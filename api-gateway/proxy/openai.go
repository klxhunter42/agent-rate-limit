package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
	"github.com/klxhunter/agent-rate-limit/api-gateway/tokenizer"
)

type OpenAIProxy struct {
	cfg     *config.Config
	client  *http.Client
	metrics *metrics.Metrics
}

func NewOpenAIProxy(cfg *config.Config, m *metrics.Metrics) *OpenAIProxy {
	return &OpenAIProxy{
		cfg:     cfg,
		client:  SharedClient(0),
		metrics: m,
	}
}

func (p *OpenAIProxy) ProxyOpenAI(
	w http.ResponseWriter, r *http.Request,
	upstreamURL, apiKey string, body []byte, model string,
	isStream bool, feedback FeedbackFunc, maskResult *privacy.MaskResult,
	maxContinuations int, toolMode string,
) error {
	openaiReq, err := AnthropicToOpenAI(body, model, p.metrics, toolMode)
	if err != nil {
		return fmt.Errorf("convert to openai format: %w", err)
	}

	openaiBody, err := json.Marshal(openaiReq)
	if err != nil {
		return fmt.Errorf("marshal openai request: %w", err)
	}

	// Estimate input tokens from the actual OpenAI request body (not Anthropic body).
	// The conversion adds tools, system messages, etc. so Anthropic body underestimates.
	estInput := tokenizer.QuickEstimateTokens(string(openaiBody))
	modelCap := tokenizer.GetModelCapabilities(model)
	slog.Info("request token estimate", "model", model, "estimated_input", estInput, "context_limit", modelCap.ContextWindow, "max_continuations", maxContinuations)

	// For providers with auto-continuation (lotuss, etc.): auto-compact + max_tokens adjustment.
	// Lotus context=40000. When input exceeds threshold, truncate old messages to free space.
	if maxContinuations > 0 && estInput > 0 {
		const providerContextLimit = 40000
		const compactThreshold = 32000 // 80% of 40000

		if estInput > compactThreshold {
			compacted := compactOpenAIMessages(openaiReq, estInput, providerContextLimit)
			if compacted {
				openaiBody, _ = json.Marshal(openaiReq)
				estInput = tokenizer.QuickEstimateTokens(string(openaiBody))
				slog.Info("auto-compacted messages", "new_estimated_input", estInput)
			}
		}

		// Adjust max_tokens to fit within context limit
		const safetyBuffer = 1500
		if mt, ok := openaiReq["max_tokens"].(float64); ok {
			available := providerContextLimit - estInput - safetyBuffer
			if available < 256 {
				available = 256
			}
			if int(mt) > available {
				slog.Info("reducing max_tokens for context limit",
					"original", int(mt), "reduced", available,
					"input_tokens", estInput, "context_limit", providerContextLimit)
				openaiReq["max_tokens"] = float64(available)
				if newBody, err := json.Marshal(openaiReq); err == nil {
					openaiBody = newBody
				}
			}
		}
	}

	var lastResp *http.Response
	var lastErrBody []byte
	var lastErrStatus int
	transientAttempts := 0
	truncationAttempts := 0
	maxTransient := p.cfg.TransientRetryMax
	if maxTransient <= 0 {
		maxTransient = 2
	}
	maxAttempts := p.cfg.UpstreamMaxRetries + 1 + maxTransient
	originalBody := body

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			backoff := jitteredBackoff(p.cfg.UpstreamRetryBaseBackoff, attempt, p.cfg.RetryBackoffJitter)
			if backoff > 5*time.Minute {
				backoff = 5 * time.Minute
			}
			slog.Warn("openai upstream retry",
				"attempt", attempt,
				"backoff", backoff,
				"model", model,
				"max_attempts", maxAttempts,
			)
			p.metrics.IncRetry()
			select {
			case <-time.After(backoff):
			case <-r.Context().Done():
				return fmt.Errorf("request cancelled during retry: %w", r.Context().Err())
			}
		}

		httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(openaiBody))
		if err != nil {
			return fmt.Errorf("create openai request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		httpReq.ContentLength = int64(len(openaiBody))

		start := time.Now()
		resp, err := p.client.Do(httpReq)
		rtt := time.Since(start)
		if err != nil {
			return fmt.Errorf("openai upstream call failed: %w", err)
		}

		isLastAttempt := attempt >= maxAttempts-1
		if feedback != nil && (resp.StatusCode != 429 || isLastAttempt) {
			feedback(resp.StatusCode, rtt, resp.Header)
		}

		if resp.StatusCode == 429 && attempt < p.cfg.UpstreamMaxRetries {
			resp.Body.Close()
			p.metrics.Inc429()
			continue
		}

		if resp.StatusCode == http.StatusOK {
			lastResp = resp
			lastErrBody = nil
			break
		}

		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		resp.Body.Close()

		action := ClassifyError(resp.StatusCode, errBody)
		switch action {
		case ActionTruncateAndRetry:
			if truncationAttempts >= 1 || !p.cfg.EnableAutoTruncate {
				break
			}
			result := TruncateMessages(originalBody, model)
			if result == nil {
				break
			}
			truncationAttempts++
			openaiReq, err = AnthropicToOpenAI(result.Body, model, p.metrics, toolMode)
			if err != nil {
				break
			}
			openaiBody, err = json.Marshal(openaiReq)
			if err != nil {
				break
			}
			p.metrics.IncContextTruncation(model)
			slog.Info("auto-truncated conversation",
				"model", model,
				"dropped_messages", result.DroppedMsgs,
				"orig_tokens", result.OrigTokens,
				"new_tokens", result.NewTokens)
			lastErrBody = nil
			continue
		case ActionRetryTransient:
			if transientAttempts >= maxTransient {
				break
			}
			transientAttempts++
			p.metrics.IncTransientRetry(resp.StatusCode, model)
			slog.Warn("openai upstream retry transient error",
				"status", resp.StatusCode,
				"model", model,
				"retry", transientAttempts,
				"max_transient", maxTransient,
				"response", string(errBody[:min(200, len(errBody))]),
			)
			lastErrBody = errBody
			lastErrStatus = resp.StatusCode
			continue
		}

		lastErrBody = errBody
		lastErrStatus = resp.StatusCode
		lastResp = resp
		break
	}

	if lastResp == nil {
		if len(lastErrBody) > 0 {
			if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
				pipeline := privacy.NewPipeline(&privacy.Config{}, nil)
				lastErrBody = pipeline.UnmaskResponse(lastErrBody, maskResult)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(lastErrStatus)
			w.Write(lastErrBody)
			return nil
		}
		return fmt.Errorf("openai upstream returned no response after %d retries", maxAttempts)
	}
	defer lastResp.Body.Close()

	if lastResp.StatusCode != http.StatusOK {
		errBody := lastErrBody
		slog.Info("upstream non-200", "status", lastResp.StatusCode, "maxContinuations", maxContinuations, "body_preview", string(errBody[:min(len(errBody), 200)]))

		// If lotuss returns 400 for max_tokens too large, retry with reduced max_tokens
		// using the actual input token count from the error message.
		if lastResp.StatusCode == 400 && maxContinuations > 0 &&
			strings.Contains(string(errBody), "max_tokens") &&
			strings.Contains(string(errBody), "context length") {
			var errResp struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(errBody, &errResp) == nil {
				if idx := strings.Index(errResp.Error.Message, "has "); idx > 0 {
					rest := errResp.Error.Message[idx+4:]
					var actualInput int
					if _, serr := fmt.Sscanf(rest, "%d", &actualInput); serr == nil && actualInput > 0 {
						available := 40000 - actualInput - 500
						if available < 256 {
							available = 256
						}
						slog.Info("retrying with reduced max_tokens after 400",
							"actual_input", actualInput, "reduced_max_tokens", available)
						openaiReq["max_tokens"] = float64(available)
						if newBody, merr := json.Marshal(openaiReq); merr == nil {
							lastResp.Body.Close()
							httpReq, rerr := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(newBody))
							if rerr == nil {
								httpReq.Header.Set("Content-Type", "application/json")
								httpReq.Header.Set("Authorization", "Bearer "+apiKey)
								httpReq.ContentLength = int64(len(newBody))
								retryResp, cerr := p.client.Do(httpReq)
								if cerr == nil && retryResp.StatusCode == http.StatusOK {
									lastResp = retryResp
									goto streamOK
								}
								if cerr == nil {
									retryResp.Body.Close()
								}
							}
						}
					}
				}
			}
		}

		if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
			pipeline := privacy.NewPipeline(&privacy.Config{}, nil)
			errBody = pipeline.UnmaskResponse(errBody, maskResult)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(lastResp.StatusCode)
		w.Write(errBody)
		return nil
	}

streamOK:

	if isStream {
		msgID := fmt.Sprintf("msg_openai_%d", time.Now().UnixNano())
		streamStart := time.Now()

		// Write SSE headers once before the first relay
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Standard streaming path (native tool handling or no tools).
		var unmasker *masking.StreamUnmasker
		if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
			unmasker = masking.NewStreamUnmasker(maskResult.PIICtx, maskResult.SecretsCtx)
			unmasker.SetGLMNoiseMode(strings.HasPrefix(model, "glm-"))
		}

		var accumulatedText string
		var totalInput, totalOutput int

		for contIdx := 0; contIdx <= maxContinuations; contIdx++ {
			isLast := contIdx == maxContinuations

			truncated, text, inTok, outTok, err := p.relayOpenAIStreamChunk(w, lastResp, model, unmasker, msgID, contIdx > 0, isLast, streamStart, toolMode)
			if err != nil {
				slog.Warn("stream chunk error", "continuation", contIdx, "error", err)
				break
			}

			accumulatedText += text
			totalInput += inTok
			totalOutput += outTok

			if !truncated {
				break
			}

			if isLast {
				break
			}

			// Stop if context is nearly full. If input tokens are already large,
			// continuation will fail with 400.
			if inTok > 35000 {
				slog.Info("openai auto-continuation skipped, context nearly full", "input_tokens", inTok)
				break
			}

			// Build continuation request
			slog.Info("openai auto-continuation", "model", model, "continuation", contIdx+1, "accumulated_text_len", len(accumulatedText))

			messages, _ := openaiReq["messages"].([]any)
			messages = append(messages,
				map[string]any{"role": "assistant", "content": accumulatedText},
				map[string]any{"role": "user", "content": "Continue exactly from where you left off. Do not repeat or add any introductory text."},
			)

			contReq := make(map[string]any, len(openaiReq))
			for k, v := range openaiReq {
				contReq[k] = v
			}
			contReq["messages"] = messages
			contReq["stream"] = true
			contReq["stream_options"] = map[string]any{"include_usage": true}

			// Adjust max_tokens for continuation: leave room for input.
			// Lotus has ~40k context, each continuation adds input tokens.
			contMaxTokens := 4096
			if origMax, ok := openaiReq["max_tokens"].(float64); ok && int(origMax) > 0 {
				contMaxTokens = int(origMax)
			}
			// Ensure we don't exceed context: max_tokens = min(contMaxTokens, 40000 - totalInput - 2000 buffer)
			avail := 40000 - totalInput - totalOutput - 2000
			if avail < 256 {
				slog.Info("openai auto-continuation skipped, no context room", "total_input", totalInput, "total_output", totalOutput, "available", avail)
				break
			}
			if avail < contMaxTokens {
				contMaxTokens = avail
			}
			contReq["max_tokens"] = contMaxTokens

			contBody, err := json.Marshal(contReq)
			if err != nil {
				slog.Warn("continuation marshal error", "error", err)
				break
			}

			httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(contBody))
			if err != nil {
				slog.Warn("continuation request error", "error", err)
				break
			}
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
			httpReq.ContentLength = int64(len(contBody))

			contStart := time.Now()
			contResp, err := p.client.Do(httpReq)
			if err != nil {
				slog.Warn("continuation upstream error", "error", err)
				break
			}

			if contResp.StatusCode != http.StatusOK {
				errBody, _ := io.ReadAll(io.LimitReader(contResp.Body, 512))
				contResp.Body.Close()
				slog.Warn("continuation upstream non-200", "status", contResp.StatusCode, "body", string(errBody))
				break
			}

			// Close previous response, continue with new one
			lastResp.Body.Close()
			lastResp = contResp
			slog.Debug("continuation upstream response", "rtt", time.Since(contStart).Milliseconds())
		}

		if totalInput > 0 || totalOutput > 0 {
			p.metrics.RecordTokens(r.Context(), model, totalInput, totalOutput)
		}

		return nil
	}
	return p.handleOpenAIResponse(w, lastResp, model, maskResult, toolMode)
}

func (p *OpenAIProxy) handleOpenAIResponse(w http.ResponseWriter, resp *http.Response, model string, maskResult *privacy.MaskResult, toolMode string) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("read openai response: %w", err)
	}

	if !json.Valid(body) {
		slog.Warn("openai upstream returned invalid JSON, wrapping as error",
			"model", model,
			"status", resp.StatusCode,
			"body_len", len(body),
			"body_preview", string(body[:min(200, len(body))]),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    "api_error",
				"message": "upstream returned malformed response",
			},
		})
		return nil
	}

	var openaiResp map[string]any
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
			pipeline := privacy.NewPipeline(&privacy.Config{}, nil)
			body = pipeline.UnmaskResponse(body, maskResult)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return nil
	}

	if usage, ok := openaiResp["usage"].(map[string]any); ok {
		pt, _ := usage["prompt_tokens"].(float64)
		ct, _ := usage["completion_tokens"].(float64)
		p.metrics.RecordTokens(resp.Request.Context(), model, int(pt), int(ct))
	}

	anthropicResp := OpenAIToAnthropic(openaiResp, model, toolMode)

	// Strip <tool_use> XML blocks from GLM model text responses.
	if strings.HasPrefix(model, "glm-") {
		if contentArr, ok := anthropicResp["content"].([]any); ok {
			for i, block := range contentArr {
				if cb, ok := block.(map[string]any); ok {
					if t, _ := cb["type"].(string); t == "text" {
						if text, _ := cb["text"].(string); text != "" {
							cb["text"] = toolUseBlockRe.ReplaceAllString(text, "")
						}
					}
				}
				contentArr[i] = block
			}
		}
	}

	respBody, _ := json.Marshal(anthropicResp)

	// Sanitize garbled output BEFORE unmask to avoid stripping restored values.
	respBody = []byte(masking.SanitizeGarbledOutput(string(respBody)))

	if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
		pipeline := privacy.NewPipeline(&privacy.Config{}, nil)
		respBody = pipeline.UnmaskResponse(respBody, maskResult)
	}

	if p.cfg.EnableResponseTrim {
		if trimmed, charsSaved := trimResponse(respBody); trimmed != nil {
			respBody = trimmed
			if charsSaved > 0 {
				p.metrics.RecordOptimization("response_trim", charsSaved, "output")
				tokensSaved := int(float64(charsSaved) / 4.0)
				p.metrics.RecordTokensSaved(tokensSaved, "output")
				p.metrics.RecordCostSavings(model, float64(tokensSaved)*p.metrics.GetInputPrice(model)/1_000_000)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
	return nil
}

// streamChunkResult holds the result of relaying one stream chunk.
type streamChunkResult struct {
	truncated       bool
	accumulatedText string
	inputTokens     int
	outputTokens    int
}

// relayOpenAIStreamChunk relays one streaming response from upstream.
// isContinuation=true means skip message_start/content_block_start (already emitted).
// isFinal=true means emit closing events even if truncated.
func (p *OpenAIProxy) relayOpenAIStreamChunk(
	w http.ResponseWriter, resp *http.Response, model string, unmasker *masking.StreamUnmasker,
	msgID string, isContinuation bool, isFinal bool, streamStart time.Time, toolMode string,
) (truncated bool, accumulatedText string, inputTokens int, outputTokens int, err error) {

	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	const maxSSELineSize = 8 * 1024 * 1024
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineSize)

	started := isContinuation
	doneReceived := false
	stopReason := "end_turn"
	var ttfbRecorded bool
	contentBlockIdx := 0   // current Anthropic content block index
	var textBlockOpen bool // true while a text content block is open
	var toolBlockOpen bool // true while a tool_use content block is open
	var stripper *toolUseStripper
	if strings.HasPrefix(model, "glm-") {
		stripper = &toolUseStripper{}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]

		if data == "[DONE]" {
			slog.Info("openai stream completed", "model", model, "output_tokens", outputTokens, "input_tokens", inputTokens, "stop_reason", stopReason, "continuation", isContinuation)

			if started {
				// Flush remaining tool_use stripper buffer.
				if stripper != nil && textBlockOpen {
					if remaining := stripper.Flush(); remaining != "" {
						if unmasker != nil {
							remaining = unmasker.ProcessChunk(remaining)
						}
						remaining = masking.SanitizeGarbledOutput(remaining)
						if remaining != "" {
							escaped, _ := json.Marshal(remaining)
							fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", contentBlockIdx, string(escaped))
							accumulatedText += remaining

						}
					}
				}
				// Flush remaining unmasker buffer before closing events.
				if unmasker != nil && (textBlockOpen || toolBlockOpen) {
					if remaining := unmasker.Flush(); remaining != "" {
						remaining = masking.SanitizeGarbledOutput(remaining)
						if remaining != "" {
							escaped, _ := json.Marshal(remaining)
							if toolBlockOpen {
								fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", contentBlockIdx, string(escaped))
							} else {
								fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", contentBlockIdx, string(escaped))
							}

						}
					}
				}
				if textBlockOpen || toolBlockOpen {
					fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", contentBlockIdx)
				}
				fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"%s\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":%d}}\n\n", stopReason, outputTokens)
				fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			}
			doneReceived = true
			if flusher != nil {
				flusher.Flush()
			}
			break
		}

		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if usage, ok := chunk["usage"].(map[string]any); ok {
			if pt, _ := usage["prompt_tokens"].(float64); pt > 0 {
				inputTokens = int(pt)
			}
			if ct, _ := usage["completion_tokens"].(float64); ct > 0 {
				outputTokens = int(ct)
			}
		}

		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)

		text, _ := delta["content"].(string)
		finishReason, _ := choice["finish_reason"].(string)
		if finishReason == "length" {
			stopReason = "max_tokens"
		}
		if finishReason == "stop" {
			continue
		}
		if finishReason == "tool_calls" {
			stopReason = "tool_use"
			continue
		}

		// Handle tool_calls in delta (OpenAI function calling)
		if toolMode == "native" {
			if toolCalls, ok := delta["tool_calls"].([]any); ok && len(toolCalls) > 0 {
				for _, tc := range toolCalls {
					tcMap, ok := tc.(map[string]any)
					if !ok {
						continue
					}
					fn, _ := tcMap["function"].(map[string]any)

					// New tool call: has id and function.name
					if id, _ := tcMap["id"].(string); id != "" {
						name, _ := fn["name"].(string)
						if !started {
							fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"%s\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"%s\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":%d,\"output_tokens\":0}}}\n\n", msgID, model, inputTokens)
							started = true
						}
						// Close previous content block if open
						if textBlockOpen || toolBlockOpen {
							if unmasker != nil {
								if remaining := unmasker.Flush(); remaining != "" {
									remaining = masking.SanitizeGarbledOutput(remaining)
									if remaining != "" {
										escaped, _ := json.Marshal(remaining)
										if toolBlockOpen {
											fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", contentBlockIdx, string(escaped))
										} else {
											fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", contentBlockIdx, string(escaped))
										}

									}
								}
							}
							fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", contentBlockIdx)
							textBlockOpen = false
							toolBlockOpen = false
							contentBlockIdx++
						}
						// Start new tool_use content block
						// JSON-escape id and name to prevent injection via special characters.
						safeID, _ := json.Marshal(id)
						safeName, _ := json.Marshal(name)
						fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"tool_use\",\"id\":%s,\"name\":%s,\"input\":{}}}\n\n", contentBlockIdx, string(safeID), string(safeName))
						toolBlockOpen = true
						if !ttfbRecorded {
							p.metrics.RecordTTFB(model, time.Since(streamStart))
							ttfbRecorded = true
						}
					}

					// Arguments delta - unmask placeholders in tool call arguments
					if args, _ := fn["arguments"].(string); args != "" && toolBlockOpen {
						if unmasker != nil {
							args = unmasker.ProcessChunkJSON(args)
						} else if strings.Contains(args, "[[") {
							args = masking.StripLeftoverPlaceholders(args)
						}
						escaped, _ := json.Marshal(args)
						fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", contentBlockIdx, string(escaped))
					}
				}
				if flusher != nil {
					flusher.Flush()
				}
				continue
			}
		}

		if text == "" {
			continue
		}

		if !ttfbRecorded {
			p.metrics.RecordTTFB(model, time.Since(streamStart))
			ttfbRecorded = true
		}

		if !started {
			fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"%s\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"%s\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":%d,\"output_tokens\":0}}}\n\n", msgID, model, inputTokens)
			fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
			started = true
			textBlockOpen = true
		} else if !textBlockOpen && !toolBlockOpen {
			// Continuation: message already started but need new content block
			contentBlockIdx++
			fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n", contentBlockIdx)
			textBlockOpen = true
		}

		accumulatedText += text
		outputTokens++

		if stripper != nil {
			text = stripper.Feed(text)
			if text == "" {
				continue
			}
		}

		if unmasker != nil {
			text = unmasker.ProcessChunk(text)
		} else if strings.Contains(text, "[[") {
			text = masking.StripLeftoverPlaceholders(text)
		}
		text = masking.SanitizeGarbledOutput(text)
		if text == "" {
			continue
		}

		escaped, _ := json.Marshal(text)
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", contentBlockIdx, string(escaped))
		if flusher != nil {
			flusher.Flush()
		}
	}

	// If stream ended without [DONE] (upstream disconnect/error), emit closing events.
	if started && !doneReceived {
		if stripper != nil && textBlockOpen {
			if remaining := stripper.Flush(); remaining != "" {
				if unmasker != nil {
					remaining = unmasker.ProcessChunk(remaining)
				}
				remaining = masking.SanitizeGarbledOutput(remaining)
				if remaining != "" {
					escaped, _ := json.Marshal(remaining)
					fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", contentBlockIdx, string(escaped))
					accumulatedText += remaining

				}
			}
		}
		if unmasker != nil && (textBlockOpen || toolBlockOpen) {
			if remaining := unmasker.Flush(); remaining != "" {
				remaining = masking.SanitizeGarbledOutput(remaining)
				if remaining != "" {
					escaped, _ := json.Marshal(remaining)
					if toolBlockOpen {
						fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", contentBlockIdx, string(escaped))
					} else {
						fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", contentBlockIdx, string(escaped))
					}

				}
			}
		}
		if err := scanner.Err(); err != nil {
			slog.Warn("openai stream scanner error, closing incomplete", "error", err)
		}
		if textBlockOpen || toolBlockOpen {
			fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", contentBlockIdx)
		}
		fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"%s\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":%d}}\n\n", stopReason, outputTokens)
		fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	} else {
		// Flush remaining unmasker buffer if nothing started.
		if !started && unmasker != nil {
			if remaining := unmasker.Flush(); remaining != "" {
				remaining = masking.SanitizeGarbledOutput(remaining)
				if remaining != "" {
					escaped, _ := json.Marshal(remaining)
					fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", string(escaped))

				}
			}
		}
	}

	return stopReason == "max_tokens", accumulatedText, inputTokens, outputTokens, nil
}

// compactOpenAIMessages truncates old conversation messages when input exceeds context threshold.
// Strategy: always keep system message + last 2 turns (4 messages) + compaction notice.
// Returns true if messages were modified.
func compactOpenAIMessages(req map[string]any, estInput int, contextLimit int) bool {
	// Messages can be []map[string]any (from AnthropicToOpenAI) or []any (from JSON unmarshal)
	var messagesRaw []map[string]any
	switch m := req["messages"].(type) {
	case []map[string]any:
		messagesRaw = m
	case []any:
		for _, v := range m {
			if msg, ok := v.(map[string]any); ok {
				messagesRaw = append(messagesRaw, msg)
			}
		}
	default:
		slog.Info("compact: unexpected messages type", "type", fmt.Sprintf("%T", req["messages"]))
		return false
	}

	if len(messagesRaw) < 6 {
		slog.Info("compact: too few messages", "count", len(messagesRaw))
		return false
	}

	// Separate system message from conversation
	var systemMsg map[string]any
	var convMsgs []map[string]any
	for _, msg := range messagesRaw {
		role, _ := msg["role"].(string)
		if role == "system" {
			systemMsg = msg
		} else {
			convMsgs = append(convMsgs, msg)
		}
	}

	if len(convMsgs) < 6 {
		slog.Info("compact: too few conv messages", "count", len(convMsgs))
		return false
	}

	// Keep last 4 messages (2 full turns: assistant+user pairs)
	keepCount := 4
	// Walk backwards to include assistant that owns tool results at boundary.
	// Cap walkback at 8 to avoid pulling in entire history.
	keepFrom := len(convMsgs) - keepCount
	maxWalkback := 8
	for keepFrom > 0 && maxWalkback > 0 {
		role, _ := convMsgs[keepFrom]["role"].(string)
		if role == "tool" {
			keepFrom--
			maxWalkback--
			continue
		}
		if _, hasTC := convMsgs[keepFrom]["tool_calls"]; hasTC {
			keepFrom--
			break
		}
		break
	}

	if keepFrom <= 0 {
		slog.Info("compact: keepFrom <= 0 after tool fixup", "keepFrom", keepFrom)
		return false
	}

	removed := keepFrom
	slog.Info("compacting messages", "total_conv", len(convMsgs), "keeping", len(convMsgs)-keepFrom, "removed", removed)

	// Build compacted messages
	var result []any
	if systemMsg != nil {
		result = append(result, systemMsg)
	}
	result = append(result, map[string]any{
		"role":    "user",
		"content": "[System: Previous conversation was auto-compacted to fit context window. Earlier messages were removed. Continue based on the remaining context and the current task.]",
	})
	result = append(result, map[string]any{
		"role":    "assistant",
		"content": "Understood. I'll continue based on the available context.",
	})
	for i := keepFrom; i < len(convMsgs); i++ {
		result = append(result, convMsgs[i])
	}

	req["messages"] = result
	return true
}
