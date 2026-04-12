package proxy

import (
	"bufio"
	"bytes"
	"context"
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
)

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
		cfg: cfg,
		client: &http.Client{
			Timeout: 0, // no global timeout — controlled per-request for streaming
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		metrics: m,
	}
}

// ProxyTransparent forwards the request body to upstream with automatic retry on 429.
// It tracks token usage via Prometheus and optionally trims verbose responses.
func (p *AnthropicProxy) ProxyTransparent(w http.ResponseWriter, r *http.Request, apiKey string, body []byte, model string, isStream bool, feedback FeedbackFunc) error {
	upstreamURL := p.cfg.UpstreamURL + "/v1/messages"

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
			time.Sleep(backoff)
		}

		httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create upstream request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", apiKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
		httpReq.ContentLength = int64(len(body))

		start := time.Now()
		resp, err := p.client.Do(httpReq)
		rtt := time.Since(start)
		if err != nil {
			return fmt.Errorf("upstream call failed: %w", err)
		}

		// Report feedback for adaptive limiter (every attempt, including 429s).
		if feedback != nil {
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
		return p.relayStreamWithTracking(w, lastResp, model)
	}

	return p.handleNonStreamResponse(w, lastResp, model)
}

// handleNonStreamResponse buffers the full response, tracks tokens, optionally trims, and sends.
const maxResponseSize = 100 * 1024 * 1024 // 100MB limit

func (p *AnthropicProxy) handleNonStreamResponse(w http.ResponseWriter, resp *http.Response, model string) error {
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
		p.metrics.RecordTokens(model, usage.Usage.InputTokens, usage.Usage.OutputTokens)
		slog.Debug("token usage",
			"model", model,
			"input", usage.Usage.InputTokens,
			"output", usage.Usage.OutputTokens,
		)
		tokenTracked = true
	}

	if !tokenTracked {
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
				p.metrics.RecordTokens(model, in, out)
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

	_, err = w.Write(body)
	return err
}

const streamTimeout = 10 * time.Minute

func (p *AnthropicProxy) relayStreamWithTracking(w http.ResponseWriter, resp *http.Response, model string) error {
	// Add timeout to prevent hanging streams
	ctx, cancel := context.WithTimeout(resp.Request.Context(), streamTimeout)
	defer cancel()

	// Wrap body with context-aware reader
	body := &readCloser{Reader: io.NopCloser(resp.Body), ctx: ctx}

	scanner := bufio.NewScanner(body)
	const maxSSELineSize = 256 * 1024 // 256KB max per SSE line
	scanner.Buffer(make([]byte, 0, maxSSELineSize), maxSSELineSize)

	var inputTokens, outputTokens int

	for scanner.Scan() {
		line := scanner.Text()

		// Relay to client immediately.
		fmt.Fprintln(w, line)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Parse SSE data lines for token tracking.
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]

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

	if inputTokens > 0 || outputTokens > 0 {
		p.metrics.RecordTokens(model, inputTokens, outputTokens)
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
