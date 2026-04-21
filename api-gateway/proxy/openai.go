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

type OpenAIProxy struct {
	cfg     *config.Config
	client  *http.Client
	metrics *metrics.Metrics
}

func NewOpenAIProxy(cfg *config.Config, m *metrics.Metrics) *OpenAIProxy {
	return &OpenAIProxy{
		cfg: cfg,
		client: &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		metrics: m,
	}
}

func (p *OpenAIProxy) ProxyOpenAI(
	w http.ResponseWriter, r *http.Request,
	upstreamURL, apiKey string, body []byte, model string,
	isStream bool, feedback FeedbackFunc, maskResult *privacy.MaskResult,
) error {
	openaiReq, err := AnthropicToOpenAI(body, model)
	if err != nil {
		return fmt.Errorf("convert to openai format: %w", err)
	}

	openaiBody, err := json.Marshal(openaiReq)
	if err != nil {
		return fmt.Errorf("marshal openai request: %w", err)
	}

	var lastResp *http.Response

	for attempt := 0; attempt <= p.cfg.UpstreamMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := p.cfg.UpstreamRetryBaseBackoff * time.Duration(attempt*attempt)
			if backoff > 5*time.Minute {
				backoff = 5 * time.Minute
			}
			slog.Warn("openai upstream 429, retrying", "attempt", attempt, "backoff", backoff)
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
		return fmt.Errorf("openai upstream returned no response after %d retries", p.cfg.UpstreamMaxRetries)
	}
	defer lastResp.Body.Close()

	if lastResp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(lastResp.StatusCode)
		io.Copy(w, lastResp.Body)
		return nil
	}

	if isStream {
		var unmasker *masking.StreamUnmasker
		if maskResult != nil && (maskResult.HasSecrets || maskResult.HasPII) {
			unmasker = masking.NewStreamUnmasker(maskResult.PIICtx, maskResult.SecretsCtx)
		}
		return p.relayOpenAIStream(w, lastResp, model, unmasker)
	}
	return p.handleOpenAIResponse(w, lastResp, model, maskResult)
}

func (p *OpenAIProxy) handleOpenAIResponse(w http.ResponseWriter, resp *http.Response, model string, maskResult *privacy.MaskResult) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("read openai response: %w", err)
	}

	var openaiResp map[string]any
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return nil
	}

	if usage, ok := openaiResp["usage"].(map[string]any); ok {
		pt, _ := usage["prompt_tokens"].(float64)
		ct, _ := usage["completion_tokens"].(float64)
		p.metrics.RecordTokens(model, int(pt), int(ct))
	}

	anthropicResp := OpenAIToAnthropic(openaiResp, model)
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

func (p *OpenAIProxy) relayOpenAIStream(w http.ResponseWriter, resp *http.Response, model string, unmasker *masking.StreamUnmasker) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	const maxSSELineSize = 256 * 1024
	scanner.Buffer(make([]byte, 0, maxSSELineSize), maxSSELineSize)

	msgID := fmt.Sprintf("msg_openai_%d", time.Now().UnixNano())
	started := false
	var inputTokens, outputTokens int
	var ttfbRecorded bool
	streamStart := time.Now()

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]

		if data == "[DONE]" {
			if started {
				fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
				fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":%d}}\n\n", outputTokens)
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
		}

		outputTokens++

		if unmasker != nil {
			text = unmasker.ProcessChunk(text)
		}
		if text == "" {
			continue
		}

		escaped, _ := json.Marshal(text)
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", string(escaped))
		if flusher != nil {
			flusher.Flush()
		}
	}

	if inputTokens > 0 || outputTokens > 0 {
		p.metrics.RecordTokens(model, inputTokens, outputTokens)
	}

	return nil
}
