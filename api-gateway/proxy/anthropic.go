package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
	"github.com/klxhunter/agent-rate-limit/api-gateway/tokenizer"
)

const maxSSELineSize = 256 * 1024 // 256KB max per SSE line

// allowedResponseHeaders lists headers safe to pass from upstream to client.
var allowedResponseHeaders = map[string]bool{
	"Content-Type":                           true,
	"X-RateLimit-Limit":                      true,
	"X-RateLimit-Remaining":                  true,
	"X-RateLimit-Reset":                      true,
	"Retry-After":                            true,
	"Request-Id":                             true,
	"Anthropic-Ratelimit-Requests-Remaining": true,
	"Anthropic-Ratelimit-Tokens-Remaining":   true,
}

// FeedbackFunc is called by the proxy after each upstream attempt.
// Defined here to avoid import cycles.
type FeedbackFunc func(statusCode int, rtt time.Duration, headers http.Header)

// Error response format matching Anthropic API.

type ErrorResponse struct {
	Type  string      `json:"type"`
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// AnthropicProxy handles transparent proxying to the Anthropic-compatible upstream.
type AnthropicProxy struct {
	cfg     *config.Config
	client  *http.Client
	metrics *metrics.Metrics
}

// NewAnthropicProxy creates a proxy with optimized HTTP client for upstream calls.
func NewAnthropicProxy(cfg *config.Config, m *metrics.Metrics) *AnthropicProxy {
	return &AnthropicProxy{
		cfg:     cfg,
		client:  SharedClient(0),
		metrics: m,
	}
}

// HasImageContent checks if the LAST user message contains image blocks.
// Only checks the most recent user turn to avoid re-routing to vision model
// when images exist in older conversation history but the current turn is text-only.
func HasImageContent(payload map[string]any) bool {
	msgs, ok := payload["messages"].([]any)
	if !ok {
		return false
	}
	// Find the last user message.
	var lastUserContent any
	for i := len(msgs) - 1; i >= 0; i-- {
		m, ok := msgs[i].(map[string]any)
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role == "user" {
			lastUserContent = m["content"]
			break
		}
	}
	if lastUserContent == nil {
		return false
	}
	content, ok := lastUserContent.([]any)
	if !ok {
		return false
	}
	for _, block := range content {
		cb, ok := block.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := cb["type"].(string); t == "image" || t == "image_url" {
			return true
		}
	}
	return false
}

// ProxyNativeVision sends a vision request to the native Zhipu API endpoint.
// It converts Anthropic format to OpenAI/Zhipu format and converts the response back.
func (p *AnthropicProxy) ProxyNativeVision(w http.ResponseWriter, r *http.Request, apiKey string, body []byte, model string, isStream bool, feedback FeedbackFunc, maskResult *privacy.MaskResult) error {
	// Convert Anthropic payload to Zhipu OpenAI format.
	zhipuReq, err := AnthropicToOpenAI(body, model, p.metrics)
	if err != nil {
		return fmt.Errorf("convert to zhipu format: %w", err)
	}

	zhipuBody, err := json.Marshal(zhipuReq)
	if err != nil {
		return fmt.Errorf("marshal zhipu request: %w", err)
	}

	// Debug: log converted payload structure (truncate base64 data).
	debugReq := make(map[string]any)
	for k, v := range zhipuReq {
		debugReq[k] = v
	}
	if msgs, ok := debugReq["messages"].([]map[string]any); ok {
		debugMsgs := make([]map[string]any, len(msgs))
		for i, m := range msgs {
			dm := map[string]any{"role": m["role"]}
			switch c := m["content"].(type) {
			case string:
				if len(c) > 200 {
					dm["content"] = c[:200] + "...(truncated)"
				} else {
					dm["content"] = c
				}
			case []map[string]any:
				parts := make([]map[string]any, len(c))
				for j, p := range c {
					dp := map[string]any{"type": p["type"]}
					if p["type"] == "text" {
						if t, ok := p["text"].(string); ok && len(t) > 100 {
							dp["text"] = t[:100] + "...(truncated)"
						} else {
							dp["text"] = p["text"]
						}
					} else if p["type"] == "image_url" {
						if iu, ok := p["image_url"].(map[string]any); ok {
							if u, ok := iu["url"].(string); ok && len(u) > 80 {
								dp["image_url"] = u[:80] + "...(truncated)"
							} else {
								dp["image_url"] = iu["url"]
							}
						}
					}
					parts[j] = dp
				}
				dm["content_type"] = fmt.Sprintf("[]map (%d parts)", len(c))
				dm["content_preview"] = parts
			default:
				dm["content_type"] = fmt.Sprintf("%T", c)
			}
			debugMsgs[i] = dm
		}
		debugReq["messages"] = debugMsgs
	}
	slog.Info("vision request payload debug", "payload", debugReq)

	upstreamURL := p.cfg.NativeVisionURL

	// Estimate input tokens for budget tracking.
	estInput := tokenizer.EstimateTokens(string(body))
	modelCap := tokenizer.GetModelCapabilities(model)
	slog.Debug("vision request token estimate", "model", model, "estimated_input", estInput, "context_limit", modelCap.ContextWindow, "provider", modelCap.Provider)

	for attempt := 0; attempt <= p.cfg.UpstreamMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := p.cfg.UpstreamRetryBaseBackoff * time.Duration(attempt*attempt)
			if backoff > 5*time.Minute {
				backoff = 5 * time.Minute
			}
			slog.Warn("vision upstream 429, retrying", "attempt", attempt, "backoff", backoff)
			select {
			case <-time.After(backoff):
			case <-r.Context().Done():
				return fmt.Errorf("request cancelled during retry: %w", r.Context().Err())
			}
		}

		httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(zhipuBody))
		if err != nil {
			return fmt.Errorf("create vision request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		httpReq.ContentLength = int64(len(zhipuBody))

		start := time.Now()
		resp, err := p.client.Do(httpReq)
		rtt := time.Since(start)
		if err != nil {
			return fmt.Errorf("vision upstream call failed: %w", err)
		}

		isLastAttempt := attempt == p.cfg.UpstreamMaxRetries
		if feedback != nil && (resp.StatusCode != 429 || isLastAttempt) {
			feedback(resp.StatusCode, rtt, resp.Header)
		}

		if resp.StatusCode == 429 && attempt < p.cfg.UpstreamMaxRetries {
			resp.Body.Close()
			p.metrics.Inc429()
			continue
		}

		// Convert Zhipu response back to Anthropic format.
		return p.convertOpenAIResponse(w, resp, model, isStream, maskResult)
	}

	return fmt.Errorf("vision upstream returned no response after %d retries", p.cfg.UpstreamMaxRetries)
}

// AnthropicToOpenAI converts an Anthropic Messages API payload to OpenAI Chat Completions format.
// Z.AI vision API only accepts "user" and "assistant" roles, so system prompts are prepended
// to the first user message. Unsupported content types (server_tool_use, tool_use, etc.) are filtered.
func AnthropicToOpenAI(body []byte, model string, m ...*metrics.Metrics) (map[string]any, error) {
	var src map[string]any
	if err := json.Unmarshal(body, &src); err != nil {
		return nil, err
	}

	// Extract system text before converting messages.
	var systemText string
	if sys, ok := src["system"]; ok {
		switch v := sys.(type) {
		case string:
			systemText = v
		case []any:
			var parts []string
			for _, s := range v {
				if sm, ok := s.(map[string]any); ok {
					if t, _ := sm["text"].(string); t != "" {
						parts = append(parts, t)
					}
				}
			}
			systemText = strings.Join(parts, "\n\n")
		}
	}

	// Supported content types for Z.AI vision API.
	supportedTypes := map[string]bool{
		"text":      true,
		"image":     true,
		"image_url": true,
	}

	// Convert messages - only user and assistant roles, filter unsupported content types.
	srcMsgs, _ := src["messages"].([]any)
	var messages []map[string]any
	for _, msg := range srcMsgs {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)

		// Skip system/tool roles entirely.
		if role != "user" && role != "assistant" {
			continue
		}

		content := m["content"]

		switch v := content.(type) {
		case string:
			messages = append(messages, map[string]any{"role": role, "content": v})
		case []any:
			var parts []map[string]any
			for _, block := range v {
				cb, ok := block.(map[string]any)
				if !ok {
					continue
				}
				t, _ := cb["type"].(string)

				// Filter unsupported content types.
				if !supportedTypes[t] {
					continue
				}

				switch t {
				case "text":
					parts = append(parts, map[string]any{"type": "text", "text": cb["text"]})
				case "image":
					parts = append(parts, convertImageBlock(cb))
				case "image_url":
					parts = append(parts, cb)
				}
			}
			if len(parts) > 0 {
				messages = append(messages, map[string]any{"role": role, "content": parts})
			}
		default:
			messages = append(messages, map[string]any{"role": role, "content": content})
		}
	}

	// Prepend system text to first user message instead of using unsupported system role.
	if systemText != "" && len(messages) > 0 {
		// Optimize system text: whitespace cleanup + dedup to save tokens.
		optText, wsSaved := tokenizer.OptimizeWhitespace(systemText)
		dedupText, dedupSaved := tokenizer.DeduplicateSentences(optText)
		if wsSaved > 0 || dedupSaved > 0 {
			slog.Debug("system prompt optimized", "ws_saved", wsSaved, "dedup_saved", dedupSaved, "original_chars", len(systemText), "optimized_chars", len(dedupText))
		}
		if len(m) > 0 && m[0] != nil {
			m[0].RecordOptimization("whitespace", wsSaved)
			m[0].RecordOptimization("dedup", dedupSaved)
		}
		systemText = dedupText
		first := messages[0]
		if first["role"] == "user" {
			switch v := first["content"].(type) {
			case string:
				first["content"] = systemText + "\n\n" + v
			case []any:
				// Prepend a text block with system prompt.
				sysBlock := map[string]any{"type": "text", "text": systemText}
				newParts := make([]any, 0, len(v)+1)
				newParts = append(newParts, sysBlock)
				for _, p := range v {
					newParts = append(newParts, p)
				}
				first["content"] = newParts
			}
		}
	}

	// Detect Thai in user messages and append language hint.
	thaiRe := regexp.MustCompile(`[\x{0E00}-\x{0E7F}]`)
	hasThai := false
	for _, m := range messages {
		if m["role"] != "user" {
			continue
		}
		switch c := m["content"].(type) {
		case string:
			if thaiRe.MatchString(c) {
				hasThai = true
			}
		case []map[string]any:
			for _, p := range c {
				if t, ok := p["text"].(string); ok && thaiRe.MatchString(t) {
					hasThai = true
				}
			}
		}
	}
	if hasThai && len(messages) > 0 {
		first := messages[0]
		hint := "IMPORTANT: You MUST respond in the same language as the user. If the user writes in Thai, respond in Thai."
		switch v := first["content"].(type) {
		case string:
			first["content"] = hint + "\n\n" + v
		case []any:
			newParts := make([]any, 0, len(v)+1)
			newParts = append(newParts, map[string]any{"type": "text", "text": hint})
			for _, p := range v {
				newParts = append(newParts, p)
			}
			first["content"] = newParts
		}
	}

	result := map[string]any{
		"model":    model,
		"messages": messages,
	}
	// Cap max_tokens for Z.AI vision models (max output ~4096).
	const maxVisionTokens = 4096
	if mt, ok := src["max_tokens"]; ok {
		if v, ok := mt.(float64); ok && v > maxVisionTokens {
			result["max_tokens"] = maxVisionTokens
		} else {
			result["max_tokens"] = mt
		}
	} else {
		result["max_tokens"] = maxVisionTokens
	}
	if stream, ok := src["stream"].(bool); ok {
		result["stream"] = stream
		if stream {
			result["stream_options"] = map[string]any{"include_usage": true}
		}
	}

	return result, nil
}

// convertImageBlock converts Anthropic image to Zhipu image_url format.
// Z.AI vision API only supports base64-encoded images, not external URLs.
// URL images are downloaded and converted to base64 data URIs.
func convertImageBlock(cb map[string]any) map[string]any {
	src, ok := cb["source"].(map[string]any)
	if !ok {
		return cb
	}
	srcType, _ := src["type"].(string)
	var url string
	switch srcType {
	case "base64":
		mediaType, _ := src["media_type"].(string)
		data, _ := src["data"].(string)
		url = fmt.Sprintf("data:%s;base64,%s", mediaType, data)
	case "url":
		imgURL, _ := src["url"].(string)
		if base64URI := FetchImageAsBase64(imgURL); base64URI != "" {
			url = base64URI
		} else {
			// Z.AI vision API doesn't support external URLs - skip this image.
			slog.Warn("skipping URL image that couldn't be fetched for base64 conversion", "url", imgURL)
			return map[string]any{"type": "text", "text": "[image could not be loaded]"}
		}
	}
	return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}
}

// FetchImageAsBase64 downloads an image URL and converts it to a base64 data URI.
func FetchImageAsBase64(imgURL string) string {
	resp, err := imageClient.Get(imgURL)
	if err != nil {
		slog.Warn("failed to fetch image URL for base64 conversion", "url", imgURL, "error", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 || resp.ContentLength > 20*1024*1024 {
		slog.Warn("image URL fetch failed or too large", "url", imgURL, "status", resp.StatusCode, "size", resp.ContentLength)
		return ""
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024))
	if err != nil {
		slog.Warn("failed to read image data", "url", imgURL, "error", err)
		return ""
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}

	return fmt.Sprintf("data:%s;base64,%s", contentType, base64.StdEncoding.EncodeToString(data))
}

// convertToolResultBlock converts Anthropic tool_result to OpenAI tool message format.
func convertToolResultBlock(cb map[string]any) map[string]any {
	toolUseID, _ := cb["tool_use_id"].(string)
	content := cb["content"]
	return map[string]any{
		"type":        "tool_result",
		"tool_use_id": toolUseID,
		"content":     content,
	}
}

// convertOpenAIResponse reads an OpenAI response and converts back to Anthropic format.
func (p *AnthropicProxy) convertOpenAIResponse(w http.ResponseWriter, resp *http.Response, model string, isStream bool, maskResult *privacy.MaskResult) error {
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
			pipeline := privacy.NewPipeline(&privacy.Config{}, nil)
			errBody = pipeline.UnmaskResponse(errBody, maskResult)
		}
		slog.Warn("vision upstream error", "status", resp.StatusCode, "body", string(errBody))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(errBody)
		return nil
	}

	if isStream {
		return p.convertOpenAIStreamResponse(w, resp, model, maskResult)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("read vision response: %w", err)
	}

	var zhipuResp map[string]any
	if err := json.Unmarshal(body, &zhipuResp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return nil
	}

	// Track tokens from Zhipu usage.
	if usage, ok := zhipuResp["usage"].(map[string]any); ok {
		pt, _ := usage["prompt_tokens"].(float64)
		ct, _ := usage["completion_tokens"].(float64)
		p.metrics.RecordTokens(resp.Request.Context(), model, int(pt), int(ct))
		slog.Info("token usage", "model", model, "input", int(pt), "output", int(ct), "format", "zhipu")
	}

	anthropicResp := OpenAIToAnthropic(zhipuResp, model)
	respBody, _ := json.Marshal(anthropicResp)

	// Unmask secrets/PII placeholders before sending to client.
	if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
		pipeline := privacy.NewPipeline(&privacy.Config{}, nil)
		respBody = pipeline.UnmaskResponse(respBody, maskResult)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
	return nil
}

// ConvertOpenAIStreamResponse converts OpenAI SSE chunks to Anthropic SSE format on-the-fly.
func (p *AnthropicProxy) convertOpenAIStreamResponse(w http.ResponseWriter, resp *http.Response, model string, maskResult *privacy.MaskResult) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, maxSSELineSize), maxSSELineSize)

	msgID := fmt.Sprintf("msg_vision_%d", time.Now().UnixNano())
	started := false
	var inputTokens, outputTokens int
	var ttfbRecorded bool
	streamStart := time.Now()

	var unmasker *masking.StreamUnmasker
	if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
		unmasker = masking.NewStreamUnmasker(maskResult.PIICtx, maskResult.SecretsCtx)
	}

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]

		if data == "[DONE]" {
			if started {
				// Flush remaining unmasker buffer before closing events.
				if unmasker != nil {
					if remaining := unmasker.Flush(); remaining != "" {
						escaped, _ := json.Marshal(remaining)
						fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", string(escaped))
						if flusher != nil {
							flusher.Flush()
						}
					}
				}
				// content_block_stop
				fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
				// message_delta with stop_reason
				fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":%d}}\n\n", outputTokens)
				// message_stop
				fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			}
			if flusher != nil {
				flusher.Flush()
			}
			break
		}

		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// Extract token usage from chunks.
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

		// Get text content from delta.
		text, _ := delta["content"].(string)
		// Also check reasoning_content (some Zhipu models use this).
		if text == "" {
			text, _ = delta["reasoning_content"].(string)
		}
		if text == "" {
			continue
		}

		if !ttfbRecorded {
			p.metrics.RecordTTFB(model, time.Since(streamStart))
			ttfbRecorded = true
		}

		if !started {
			// message_start
			fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"%s\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"%s\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":%d,\"output_tokens\":0}}}\n\n", msgID, model, inputTokens)
			// content_block_start
			fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
			started = true
		}

		outputTokens++

		// Unmask text chunk if privacy masking is active.
		if unmasker != nil {
			text = unmasker.ProcessChunk(text)
		}
		// content_block_delta
		escaped, _ := json.Marshal(text)
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", string(escaped))
		if flusher != nil {
			flusher.Flush()
		}
	}

	// Flush remaining unmasker buffer.
	if inputTokens > 0 || outputTokens > 0 {
		p.metrics.RecordTokens(resp.Request.Context(), model, inputTokens, outputTokens)
		slog.Info("vision stream token usage", "model", model, "input", inputTokens, "output", outputTokens)
	}

	return nil
}

// OpenAIToAnthropic converts an OpenAI Chat Completions response to Anthropic Messages response format.
func OpenAIToAnthropic(zhipu map[string]any, model string) map[string]any {
	content := []any{}
	var stopReason string

	if choices, ok := zhipu["choices"].([]any); ok && len(choices) > 0 {
		choice, _ := choices[0].(map[string]any)
		if msg, ok := choice["message"].(map[string]any); ok {
			if text, _ := msg["content"].(string); text != "" {
				content = append(content, map[string]any{"type": "text", "text": text})
			}
		}
		if fr, _ := choice["finish_reason"].(string); fr == "stop" {
			stopReason = "end_turn"
		} else {
			stopReason = fr
		}
	}

	var inputTokens, outputTokens int
	if usage, ok := zhipu["usage"].(map[string]any); ok {
		inputTokens = int(usage["prompt_tokens"].(float64))
		outputTokens = int(usage["completion_tokens"].(float64))
	}

	return map[string]any{
		"id":            zhipu["id"],
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       content,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	}
}

// ProxyOptions configures proxy behavior for non-default upstream/auth scenarios.
type ProxyOptions struct {
	AuthMode         string                                       // "api_key" (default) or "bearer"
	UpstreamOverride string                                       // if non-empty, use this instead of cfg.UpstreamURL
	ExtraHeaders     map[string]string                            // additional headers to set
	OnAuthError      func(oldKey string) (newKey string, ok bool) // called on 401 to refresh token
}

// It tracks token usage via Prometheus and optionally trims verbose responses.
func (p *AnthropicProxy) ProxyTransparent(w http.ResponseWriter, r *http.Request, apiKey string, body []byte, model string, isStream bool, feedback FeedbackFunc, maskResult *privacy.MaskResult, opts *ProxyOptions) error {
	upstreamURL := p.cfg.UpstreamURL + "/v1/messages"
	if opts != nil && opts.UpstreamOverride != "" {
		upstreamURL = opts.UpstreamOverride
	}

	// Estimate input tokens and log model capabilities.
	estInput := tokenizer.QuickEstimateTokens(string(body))
	modelCap := tokenizer.GetModelCapabilities(model)
	slog.Debug("request token estimate", "model", model, "estimated_input", estInput, "context_limit", modelCap.ContextWindow, "max_output", modelCap.MaxOutputTokens)

	// Optimize system prompt in body if present.
	if bodyMap := make(map[string]any); json.Unmarshal(body, &bodyMap) == nil {
		if sys, ok := bodyMap["system"].(string); ok && sys != "" {
			optSys, wsSaved := tokenizer.OptimizeWhitespace(sys)
			optSys, dedupSaved := tokenizer.DeduplicateSentences(optSys)
			if wsSaved > 0 || dedupSaved > 0 {
				bodyMap["system"] = optSys
				if newBody, err := json.Marshal(bodyMap); err == nil {
					body = newBody
					slog.Debug("transparent prompt optimized", "ws_saved", wsSaved, "dedup_saved", dedupSaved)
					p.metrics.RecordOptimization("whitespace", wsSaved)
					p.metrics.RecordOptimization("dedup", dedupSaved)
				}
			}
		}
	}

	var lastResp *http.Response

	for attempt := 0; attempt <= p.cfg.UpstreamMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := p.cfg.UpstreamRetryBaseBackoff * time.Duration(attempt*attempt)
			// Cap backoff at 5 minutes to prevent excessive waits
			if backoff > 5*time.Minute {
				backoff = 5 * time.Minute
			}
			slog.Warn("upstream 429, retrying",
				"attempt", attempt,
				"backoff", backoff,
				"model", model,
			)
			p.metrics.IncRetry()
			select {
			case <-time.After(backoff):
			case <-r.Context().Done():
				return fmt.Errorf("request cancelled during retry backoff: %w", r.Context().Err())
			}
		}

		httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create upstream request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if opts != nil && opts.AuthMode == "bearer" {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
			httpReq.Header.Set("x-client-request-id", newRequestID())
		} else {
			httpReq.Header.Set("x-api-key", apiKey)
		}
		httpReq.Header.Set("anthropic-version", p.cfg.AnthropicVersion)
		if opts != nil {
			for k, v := range opts.ExtraHeaders {
				httpReq.Header.Set(k, v)
			}
		}
		httpReq.ContentLength = int64(len(body))

		start := time.Now()
		resp, err := p.client.Do(httpReq)
		rtt := time.Since(start)
		if err != nil {
			return fmt.Errorf("upstream call failed: %w", err)
		}

		isLastAttempt := attempt == p.cfg.UpstreamMaxRetries
		// Report feedback for adaptive limiter only on final attempt
		// to prevent hammering the limit down on retries.
		if feedback != nil && (resp.StatusCode != 429 || isLastAttempt) {
			feedback(resp.StatusCode, rtt, resp.Header)
		}

		if resp.StatusCode == 429 && attempt < p.cfg.UpstreamMaxRetries {
			resp.Body.Close()
			p.metrics.Inc429()
			continue
		}

		// On 401 with OAuth bearer, try refreshing the token once.
		if resp.StatusCode == 401 && opts != nil && opts.AuthMode == "bearer" && opts.OnAuthError != nil {
			resp.Body.Close()
			if newKey, ok := opts.OnAuthError(apiKey); ok {
				slog.Info("retrying with refreshed token", "model", model)
				apiKey = newKey
				httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
				if err != nil {
					return fmt.Errorf("create retry request: %w", err)
				}
				httpReq.Header.Set("Content-Type", "application/json")
				httpReq.Header.Set("Authorization", "Bearer "+apiKey)
				httpReq.Header.Set("anthropic-version", p.cfg.AnthropicVersion)
				for k, v := range opts.ExtraHeaders {
					httpReq.Header.Set(k, v)
				}
				httpReq.ContentLength = int64(len(body))

				start2 := time.Now()
				resp2, err2 := p.client.Do(httpReq)
				rtt2 := time.Since(start2)
				if err2 != nil {
					return fmt.Errorf("retry after refresh failed: %w", err2)
				}
				if feedback != nil {
					feedback(resp2.StatusCode, rtt2, resp2.Header)
				}
				lastResp = resp2
				break
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(ErrorResponse{
				Type: "error",
				Error: ErrorDetail{
					Type:    "authentication_error",
					Message: "OAuth token expired and refresh failed. Please re-authenticate.",
				},
			})
			return fmt.Errorf("upstream 401: token refresh failed")
		}

		lastResp = resp
		break
	}

	if lastResp == nil {
		return fmt.Errorf("upstream returned no response after %d retries", p.cfg.UpstreamMaxRetries)
	}
	defer lastResp.Body.Close()

	// Copy only allowed response headers (prevent header injection).
	for k, vs := range lastResp.Header {
		if _, ok := allowedResponseHeaders[k]; !ok {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(lastResp.StatusCode)

	if isStream {
		var unmasker *masking.StreamUnmasker
		if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
			unmasker = masking.NewStreamUnmasker(maskResult.PIICtx, maskResult.SecretsCtx)
		}
		return p.relayStreamWithTracking(w, lastResp, model, unmasker)
	}

	return p.handleNonStreamResponse(w, lastResp, model, maskResult)
}

// handleNonStreamResponse buffers the full response, tracks tokens, optionally trims, and sends.
const maxResponseSize = 100 * 1024 * 1024 // 100MB limit

func (p *AnthropicProxy) handleNonStreamResponse(w http.ResponseWriter, resp *http.Response, model string, maskResult *privacy.MaskResult) error {
	// Limit response size to prevent OOM
	limitedReader := io.LimitReader(resp.Body, maxResponseSize+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxResponseSize {
		return fmt.Errorf("response exceeds maximum size of %d bytes", maxResponseSize)
	}

	// Track token usage.
	tokenTracked := false
	var usage struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &usage) == nil && (usage.Usage.InputTokens > 0 || usage.Usage.OutputTokens > 0) {
		p.metrics.RecordTokens(resp.Request.Context(), model, usage.Usage.InputTokens, usage.Usage.OutputTokens)
		slog.Info("token usage",
			"model", model,
			"input", usage.Usage.InputTokens,
			"output", usage.Usage.OutputTokens,
			"format", "anthropic",
		)
		tokenTracked = true
	}

	if !tokenTracked {
		// Log raw response for debugging when token tracking fails.
		preview := string(body)
		if len(preview) > 500 {
			preview = preview[:500]
		}
		slog.Warn("token tracking failed for non-stream response",
			"model", model,
			"status", resp.StatusCode,
			"body_preview", preview,
		)
		// Fallback: try parsing without nested "usage" wrapper (OpenAI-style)
		var altUsage struct {
			InputTokens      int `json:"input_tokens"`
			OutputTokens     int `json:"output_tokens"`
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		}
		if json.Unmarshal(body, &altUsage) == nil {
			in := altUsage.InputTokens
			out := altUsage.OutputTokens
			if in == 0 && altUsage.PromptTokens > 0 {
				in = altUsage.PromptTokens
			}
			if out == 0 && altUsage.CompletionTokens > 0 {
				out = altUsage.CompletionTokens
			}
			if in > 0 || out > 0 {
				p.metrics.RecordTokens(resp.Request.Context(), model, in, out)
				slog.Info("token usage (fallback format)",
					"model", model,
					"input", in,
					"output", out,
				)
			}
		}
		slog.Debug("token usage not found in response",
			"model", model,
			"response_preview", string(body[:min(500, len(body))]),
		)
	}

	// Trim verbose patterns if enabled.
	if p.cfg.EnableResponseTrim {
		if trimmed := trimResponse(body); trimmed != nil {
			body = trimmed
		}
	}

	// Unmask secrets/PII placeholders before sending to client.
	if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
		pipeline := privacy.NewPipeline(&privacy.Config{}, nil)
		body = pipeline.UnmaskResponse(body, maskResult)
	}

	_, err = w.Write(body)
	return err
}

const streamTimeout = 10 * time.Minute

func (p *AnthropicProxy) relayStreamWithTracking(w http.ResponseWriter, resp *http.Response, model string, unmasker *masking.StreamUnmasker) error {
	// Add timeout to prevent hanging streams
	ctx, cancel := context.WithTimeout(resp.Request.Context(), streamTimeout)
	defer cancel()

	// Wrap body with context-aware reader
	body := &readCloser{Reader: io.NopCloser(resp.Body), ctx: ctx}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, maxSSELineSize), maxSSELineSize)

	var ttfbRecorded bool
	var inputTokens, outputTokens int
	var streamStart = time.Now()

	for scanner.Scan() {
		if !ttfbRecorded {
			p.metrics.RecordTTFB(model, time.Since(streamStart))
			ttfbRecorded = true
		}
		line := scanner.Text()

		// Parse SSE data lines for unmasking and token tracking.
		if !strings.HasPrefix(line, "data: ") {
			fmt.Fprintln(w, line)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			continue
		}
		data := line[6:]

		// Unmask content_block_delta text before relaying.
		if unmasker != nil && strings.Contains(data, `"content_block_delta"`) {
			var evt struct {
				Type  string `json:"type"`
				Index int    `json:"index"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal([]byte(data), &evt) == nil && evt.Delta.Text != "" {
				unmasked := unmasker.ProcessChunk(evt.Delta.Text)
				if unmasked != evt.Delta.Text {
					evt.Delta.Text = unmasked
					if newData, err := json.Marshal(evt); err == nil {
						line = "data: " + string(newData)
					}
				}
			}
		}

		// Relay to client.
		fmt.Fprintln(w, line)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		if strings.Contains(data, `"message_start"`) {
			var msg struct {
				Message struct {
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(data), &msg) == nil && msg.Message.Usage.InputTokens > 0 {
				inputTokens = msg.Message.Usage.InputTokens
			}
		} else if strings.Contains(data, `"message_delta"`) {
			var msg struct {
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal([]byte(data), &msg) == nil && msg.Usage.OutputTokens > 0 {
				outputTokens = msg.Usage.OutputTokens
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stream read error: %w", err)
	}

	// Flush remaining unmasker buffer.
	if unmasker != nil {
		if remaining := unmasker.Flush(); remaining != "" {
			escaped, _ := json.Marshal(remaining)
			fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", string(escaped))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}

	if inputTokens > 0 || outputTokens > 0 {
		p.metrics.RecordTokens(ctx, model, inputTokens, outputTokens)
		slog.Debug("stream token usage",
			"model", model,
			"input", inputTokens,
			"output", outputTokens,
		)
	}

	return nil
}

// trimResponse strips verbose patterns from text content blocks in a non-stream response.
// Returns nil if trimming was skipped (invalid JSON or no changes).
func trimResponse(body []byte) []byte {
	var resp map[string]any
	if json.Unmarshal(body, &resp) != nil {
		return nil
	}

	content, ok := resp["content"].([]any)
	if !ok {
		return nil
	}

	modified := false
	for i, block := range content {
		cb, ok := block.(map[string]any)
		if !ok || cb["type"] != "text" {
			continue
		}
		text, ok := cb["text"].(string)
		if !ok {
			continue
		}
		trimmed := trimVerbose(text)
		if trimmed != text {
			// Validate trimmed text is still valid printable UTF-8
			if !isValidUTF8String(trimmed) {
				slog.Warn("trimmed text contains invalid UTF-8, skipping trim")
				continue
			}
			cb["text"] = trimmed
			content[i] = cb
			modified = true
		}
	}

	if !modified {
		return nil
	}

	resp["content"] = content
	result, err := json.Marshal(resp)
	if err != nil {
		return nil
	}
	return result
}

// trimVerbose removes common verbose prefixes and suffixes from AI response text.
func trimVerbose(text string) string {
	for _, re := range verbosePatterns {
		text = re.ReplaceAllString(text, "")
	}
	return strings.TrimSpace(text)
}

var verbosePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^here's (the |a )?`),
	regexp.MustCompile(`(?i)^here is (the |a )?`),
	regexp.MustCompile(`(?i)^let me (explain|help|show|walk you through|break down|tell you about)[^\n]*\n`),
	regexp.MustCompile(`(?i)^i'll (help you|explain|show|walk you through)[^\n]*\n`),
	regexp.MustCompile(`(?i)^sure!?\s*\n`),
	regexp.MustCompile(`(?i)^certainly!?\s*\n`),
	regexp.MustCompile(`(?i)^of course!?\s*\n`),
	regexp.MustCompile(`(?i)^great question!?\s*\n`),
	regexp.MustCompile(`(?i)^i'd be happy to (help|explain|show|assist)[^\n]*\n`),
	regexp.MustCompile(`(?i)\n\nhope (this|that) (helps|is helpful)!?\.?\s*$`),
	regexp.MustCompile(`(?i)\n\nlet me know if you need (anything else|more help|further assistance)\.?\s*$`),
}

// readCloser wraps an io.Reader with context cancellation support.
type readCloser struct {
	io.Reader
	ctx context.Context
}

func (rc *readCloser) Read(p []byte) (n int, err error) {
	if err := rc.ctx.Err(); err != nil {
		return 0, err
	}
	return rc.Reader.Read(p)
}

// isValidUTF8String checks if a string contains only valid printable UTF-8.
func isValidUTF8String(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

// newRequestID generates a random UUID-like request ID for x-client-request-id.
func newRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// RateLimitError returns an Anthropic-format rate limit error.
func RateLimitError(retryAfterSec int) ErrorResponse {
	return ErrorResponse{
		Type: "error",
		Error: ErrorDetail{
			Type:    "rate_limit_error",
			Message: fmt.Sprintf("Rate limit exceeded. Please retry after %d seconds.", retryAfterSec),
		},
	}
}

// OverloadedError returns an Anthropic-format overloaded error.
func OverloadedError(msg string) ErrorResponse {
	return ErrorResponse{
		Type: "error",
		Error: ErrorDetail{
			Type:    "overloaded_error",
			Message: msg,
		},
	}
}
