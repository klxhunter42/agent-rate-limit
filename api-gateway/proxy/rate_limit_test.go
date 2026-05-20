package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/stretchr/testify/assert"
)

func newTestMetrics() *metrics.Metrics {
	return metrics.New(func() float64 { return 0 }, nil)
}

func makeMinimalBody() []byte {
	body, _ := json.Marshal(map[string]any{
		"model":      "claude-haiku-4-5-20251001",
		"max_tokens": 100,
		"stream":     false,
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	})
	return body
}

// Test429_OnRateLimitError_Callback verifies OnRateLimitError is called on 429.
func Test429_OnRateLimitError_Callback(t *testing.T) {
	var rateLimitCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rateLimitCalls.Load() < 1 {
			rateLimitCalls.Add(1)
			w.Header().Set("X-Should-Retry", "true")
			w.WriteHeader(429)
			json.NewEncoder(w).Encode(ErrorResponse{
				Type:  "error",
				Error: ErrorDetail{Type: "rate_limit_error", Message: "rate limited"},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{
			"type": "message", "id": "msg_123", "content": []any{
				map[string]any{"type": "text", "text": "ok"},
			},
		})
	}))
	defer upstream.Close()

	cfg := &config.Config{
		UpstreamURL:              upstream.URL,
		UpstreamMaxRetries:       2,
		UpstreamRetryBaseBackoff: 1 * time.Millisecond,
		TransientRetryMax:        1,
		AnthropicVersion:         "2023-06-01",
	}
	proxy := NewAnthropicProxy(cfg, newTestMetrics())

	var callbackCalled atomic.Int32
	opts := &ProxyOptions{
		AuthMode: "api_key",
		OnRateLimitError: func(oldKey string) (FallbackResult, bool) {
			callbackCalled.Add(1)
			return FallbackResult{}, false
		},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeMinimalBody()))
	err := proxy.ProxyTransparent(w, r, "test-key", makeMinimalBody(), "claude-haiku-4-5-20251001", false, nil, nil, opts)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), callbackCalled.Load(), "OnRateLimitError should be called once")
	assert.Equal(t, 200, w.Code, "should succeed after retry")
}

// Test429_Retries_Exhausted verifies 429 is returned to client immediately
// when no fallback is available (passthrough mode). Server-side retry is skipped
// because Claude CLI handles retries itself.
func Test429_Retries_Exhausted(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("X-Should-Retry", "true")
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(ErrorResponse{
			Type:  "error",
			Error: ErrorDetail{Type: "rate_limit_error", Message: "rate limited"},
		})
	}))
	defer upstream.Close()

	cfg := &config.Config{
		UpstreamURL:              upstream.URL,
		UpstreamMaxRetries:       2,
		UpstreamRetryBaseBackoff: 1 * time.Millisecond,
		TransientRetryMax:        1,
		AnthropicVersion:         "2023-06-01",
	}
	proxy := NewAnthropicProxy(cfg, newTestMetrics())

	opts := &ProxyOptions{
		AuthMode:         "api_key",
		OnRateLimitError: func(oldKey string) (FallbackResult, bool) { return FallbackResult{}, false },
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeMinimalBody()))
	err := proxy.ProxyTransparent(w, r, "test-key", makeMinimalBody(), "claude-haiku-4-5-20251001", false, nil, nil, opts)
	assert.NoError(t, err)
	assert.Equal(t, int32(1), attempts.Load(), "should attempt once (passthrough, no server-side retry)")
	assert.Equal(t, 429, w.Code, "should return 429 to client")
	assert.Equal(t, "true", w.Header().Get("X-Should-Retry"), "should set X-Should-Retry header")
}

// Test429_Fallback_WithNewKey verifies OnRateLimitError provides new key and request succeeds.
func Test429_Fallback_WithNewKey(t *testing.T) {
	var rateLimitCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-api-key")
		if key == "old-key" {
			rateLimitCalls.Add(1)
			w.WriteHeader(429)
			json.NewEncoder(w).Encode(ErrorResponse{
				Type:  "error",
				Error: ErrorDetail{Type: "rate_limit_error", Message: "rate limited"},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{
			"type": "message", "id": "msg_456", "content": []any{
				map[string]any{"type": "text", "text": "fallback success"},
			},
		})
	}))
	defer upstream.Close()

	cfg := &config.Config{
		UpstreamURL:              upstream.URL,
		UpstreamMaxRetries:       2,
		UpstreamRetryBaseBackoff: 1 * time.Millisecond,
		TransientRetryMax:        1,
		AnthropicVersion:         "2023-06-01",
	}
	proxy := NewAnthropicProxy(cfg, newTestMetrics())

	opts := &ProxyOptions{
		AuthMode: "api_key",
		OnRateLimitError: func(oldKey string) (FallbackResult, bool) {
			return FallbackResult{APIKey: "new-key"}, true
		},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeMinimalBody()))
	err := proxy.ProxyTransparent(w, r, "old-key", makeMinimalBody(), "claude-haiku-4-5-20251001", false, nil, nil, opts)
	assert.NoError(t, err)
	assert.Equal(t, 200, w.Code, "should succeed with fallback key")
}

// Test429_OAuth_NoTokenRefresh verifies 429 does NOT trigger OnAuthError.
// Token renewal is handled by the background refresh worker based on expiry,
// not by the 429 retry path. 429 is a rate-limit issue (per-organization),
// refreshing the token is useless since it belongs to the same org.
func Test429_OAuth_NoTokenRefresh(t *testing.T) {
	var authErrorCalled atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(ErrorResponse{
			Type:  "error",
			Error: ErrorDetail{Type: "rate_limit_error", Message: "rate limited"},
		})
	}))
	defer upstream.Close()

	cfg := &config.Config{
		UpstreamURL:              upstream.URL,
		UpstreamMaxRetries:       2,
		UpstreamRetryBaseBackoff: 1 * time.Millisecond,
		TransientRetryMax:        1,
		AnthropicVersion:         "2023-06-01",
	}
	proxy := NewAnthropicProxy(cfg, newTestMetrics())

	opts := &ProxyOptions{
		AuthMode:         "bearer",
		OnRateLimitError: func(oldKey string) (FallbackResult, bool) { return FallbackResult{}, false },
		OnAuthError: func(oldKey string) (string, bool) {
			authErrorCalled.Add(1)
			return "sk-ant-oat01-new-refreshed-token", true
		},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeMinimalBody()))
	r.Header.Set("Authorization", "Bearer sk-ant-oat01-old-token")
	err := proxy.ProxyTransparent(w, r, "sk-ant-oat01-old-token", makeMinimalBody(), "claude-sonnet-4-6", false, nil, nil, opts)
	assert.NoError(t, err)
	assert.Equal(t, int32(0), authErrorCalled.Load(), "OnAuthError should NOT be called on 429")
	assert.Equal(t, 429, w.Code, "should return 429 to client after retries exhausted")
}

// Test429_Feedback_OnLastAttempt verifies feedback callback fires only on last attempt.
func Test429_Feedback_OnLastAttempt(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(ErrorResponse{
			Type:  "error",
			Error: ErrorDetail{Type: "rate_limit_error", Message: "limited"},
		})
	}))
	defer upstream.Close()

	cfg := &config.Config{
		UpstreamURL:              upstream.URL,
		UpstreamMaxRetries:       1,
		UpstreamRetryBaseBackoff: 1 * time.Millisecond,
		TransientRetryMax:        0,
		AnthropicVersion:         "2023-06-01",
	}
	proxy := NewAnthropicProxy(cfg, newTestMetrics())

	var feedbackStatuses []int
	opts := &ProxyOptions{
		AuthMode:         "api_key",
		OnRateLimitError: func(oldKey string) (FallbackResult, bool) { return FallbackResult{}, false },
	}

	feedback := func(statusCode int, rtt time.Duration, headers http.Header) {
		feedbackStatuses = append(feedbackStatuses, statusCode)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeMinimalBody()))
	proxy.ProxyTransparent(w, r, "test-key", makeMinimalBody(), "claude-haiku-4-5-20251001", false, feedback, nil, opts)

	assert.Empty(t, feedbackStatuses, "feedback should not fire for early-loop-break 429")
	assert.Equal(t, 429, w.Code, "client should still receive 429")
}

// Test429_Fallback_ChangesUpstream verifies OnRateLimitError can change upstream URL.
func Test429_Fallback_ChangesUpstream(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(ErrorResponse{
			Type:  "error",
			Error: ErrorDetail{Type: "rate_limit_error", Message: "limited"},
		})
	}))
	defer primary.Close()

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{
			"type": "message", "id": "msg_fb", "content": []any{
				map[string]any{"type": "text", "text": "from fallback"},
			},
		})
	}))
	defer fallback.Close()

	cfg := &config.Config{
		UpstreamURL:              primary.URL,
		UpstreamMaxRetries:       2,
		UpstreamRetryBaseBackoff: 1 * time.Millisecond,
		TransientRetryMax:        1,
		AnthropicVersion:         "2023-06-01",
	}
	proxy := NewAnthropicProxy(cfg, newTestMetrics())

	opts := &ProxyOptions{
		AuthMode: "api_key",
		OnRateLimitError: func(oldKey string) (FallbackResult, bool) {
			return FallbackResult{
				APIKey:      "fallback-key",
				UpstreamURL: fallback.URL + "/v1/messages",
			}, true
		},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeMinimalBody()))
	err := proxy.ProxyTransparent(w, r, "primary-key", makeMinimalBody(), "claude-haiku-4-5-20251001", false, nil, nil, opts)
	assert.NoError(t, err)
	assert.Equal(t, 200, w.Code, "should succeed on fallback upstream")
	body, _ := io.ReadAll(w.Body)
	assert.Contains(t, string(body), "from fallback")
}
