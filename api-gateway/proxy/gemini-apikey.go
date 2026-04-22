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
)

type GeminiAPIProxy struct {
	cfg     *config.Config
	client  *http.Client
	metrics *metrics.Metrics
}

func NewGeminiAPIProxy(cfg *config.Config, m *metrics.Metrics) *GeminiAPIProxy {
	return &GeminiAPIProxy{
		cfg:     cfg,
		client:  SharedClient(0),
		metrics: m,
	}
}

func (p *GeminiAPIProxy) ProxyGemini(
	w http.ResponseWriter, r *http.Request,
	upstreamURL, apiKey string, body []byte, model string,
	isStream bool, feedback FeedbackFunc, maskResult *privacy.MaskResult,
) error {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("parse anthropic payload: %w", err)
	}

	geminiReq := anthropicToGemini(payload, p.cfg.GeminiDefaultModel)
	geminiBody, err := json.Marshal(geminiReq)
	if err != nil {
		return fmt.Errorf("marshal gemini request: %w", err)
	}

	geminiModel := mapModelToGemini(model)
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent", p.cfg.GeminiAPIEndpoint, geminiModel)
	if isStream {
		endpoint = fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", p.cfg.GeminiAPIEndpoint, geminiModel)
	}

	var lastResp *http.Response
	for attempt := 0; attempt <= p.cfg.UpstreamMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := p.cfg.UpstreamRetryBaseBackoff * time.Duration(attempt*attempt)
			if backoff > 5*time.Minute {
				backoff = 5 * time.Minute
			}
			slog.Warn("gemini upstream 429, retrying", "attempt", attempt, "backoff", backoff)
			p.metrics.IncRetry()
			select {
			case <-time.After(backoff):
			case <-r.Context().Done():
				return fmt.Errorf("request cancelled during retry: %w", r.Context().Err())
			}
		}

		httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(geminiBody))
		if err != nil {
			return fmt.Errorf("create gemini request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if strings.HasPrefix(apiKey, "ya29.") {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		} else {
			httpReq.Header.Set("x-goog-api-key", apiKey)
		}
		httpReq.ContentLength = int64(len(geminiBody))

		start := time.Now()
		resp, err := p.client.Do(httpReq)
		rtt := time.Since(start)
		if err != nil {
			return fmt.Errorf("gemini upstream call failed: %w", err)
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

		lastResp = resp
		break
	}

	if lastResp == nil {
		return fmt.Errorf("gemini upstream returned no response after %d retries", p.cfg.UpstreamMaxRetries)
	}
	defer lastResp.Body.Close()

	if lastResp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(lastResp.Body, maxResponseSize))
		if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
			pipeline := privacy.NewPipeline(&privacy.Config{}, nil)
			errBody = pipeline.UnmaskResponse(errBody, maskResult)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(lastResp.StatusCode)
		w.Write(errBody)
		return nil
	}

	if isStream {
		return p.relayGeminiStream(w, lastResp, model, maskResult)
	}
	return p.handleGeminiResponse(w, lastResp, model, maskResult)
}

func (p *GeminiAPIProxy) handleGeminiResponse(w http.ResponseWriter, resp *http.Response, model string, maskResult *privacy.MaskResult) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("read gemini response: %w", err)
	}

	var gResp geminiResponse
	if err := json.Unmarshal(body, &gResp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
		return nil
	}

	if usage := gResp.UsageMeta; usage != nil {
		p.metrics.RecordTokens(model, usage.PromptTokenCount, usage.CandidatesTokenCount)
	}

	anthropicResp := geminiToAnthropic(gResp, model, false)
	respBody, _ := json.Marshal(anthropicResp)

	if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
		pipeline := privacy.NewPipeline(&privacy.Config{}, nil)
		respBody = pipeline.UnmaskResponse(respBody, maskResult)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
	return nil
}

func (p *GeminiAPIProxy) relayGeminiStream(w http.ResponseWriter, resp *http.Response, model string, maskResult *privacy.MaskResult) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	const maxSSELineSize = 256 * 1024
	scanner.Buffer(make([]byte, 0, maxSSELineSize), maxSSELineSize)

	var unmasker *masking.StreamUnmasker
	if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
		unmasker = masking.NewStreamUnmasker(maskResult.PIICtx, maskResult.SecretsCtx)
	}

	msgID := fmt.Sprintf("msg_gemini_%d", time.Now().UnixNano())
	started := false
	var inputTokens, outputTokens int
	streamStart := time.Now()

	for scanner.Scan() {
		line := scanner.Text()
		if !started {
			p.metrics.RecordTTFB(model, time.Since(streamStart))
			writeSSE(w, flusher, "message_start", map[string]any{
				"type": "message_start",
				"message": map[string]any{
					"id": msgID, "type": "message", "role": "assistant",
					"content": []any{}, "model": model,
					"stop_reason": nil, "stop_sequence": nil,
					"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
				},
			})
			writeSSE(w, flusher, "content_block_start", map[string]any{
				"type": "content_block_start", "index": 0,
				"content_block": map[string]any{"type": "text", "text": ""},
			})
			started = true
		}

		if !bytes.HasPrefix([]byte(line), []byte("data: ")) {
			continue
		}
		data := line[6:]

		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if usage, ok := chunk["usageMetadata"].(map[string]any); ok {
			if pt, _ := usage["promptTokenCount"].(float64); pt > 0 {
				inputTokens = int(pt)
			}
			if ct, _ := usage["candidatesTokenCount"].(float64); ct > 0 {
				outputTokens = int(ct)
			}
		}

		candidates, _ := chunk["candidates"].([]any)
		if len(candidates) == 0 {
			continue
		}
		cand, _ := candidates[0].(map[string]any)
		content, _ := cand["content"].(map[string]any)
		parts, _ := content["parts"].([]any)
		for _, part := range parts {
			pm, _ := part.(map[string]any)
			text, _ := pm["text"].(string)
			if text == "" {
				continue
			}
			if unmasker != nil {
				text = unmasker.ProcessChunk(text)
				if text == "" {
					continue
				}
			}
			outputTokens++
			escaped, _ := json.Marshal(text)
			fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", string(escaped))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}

	if unmasker != nil {
		if remaining := unmasker.Flush(); remaining != "" {
			escaped, _ := json.Marshal(remaining)
			fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", string(escaped))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}

	if started {
		writeSSE(w, flusher, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		writeSSE(w, flusher, "message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil},
			"usage": map[string]any{"output_tokens": outputTokens},
		})
		writeSSE(w, flusher, "message_stop", map[string]any{"type": "message_stop"})
	}

	if inputTokens > 0 || outputTokens > 0 {
		p.metrics.RecordTokens(model, inputTokens, outputTokens)
	}
	return nil
}
