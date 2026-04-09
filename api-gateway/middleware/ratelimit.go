package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/proxy"
)

// rateLimitRequest matches the distributed-rate-limiter check API contract.
type rateLimitRequest struct {
	Key    string `json:"key"`
	Tokens int    `json:"tokens"`
}

// rateLimitResponse matches the distributed-rate-limiter check response.
type rateLimitResponse struct {
	Key             string `json:"key"`
	TokensRequested int    `json:"tokensRequested"`
	Allowed         bool   `json:"allowed"`
}

// RateLimiter holds the configuration and HTTP client for calling the external
// distributed-rate-limiter service.
type RateLimiter struct {
	cfg     *config.Config
	client  *http.Client
	checkURL string
}

// NewRateLimiter creates a new rate-limiting middleware helper.
func NewRateLimiter(cfg *config.Config) *RateLimiter {
	return &RateLimiter{
		cfg: cfg,
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
		checkURL: cfg.RateLimiterCheckURL(),
	}
}

// check calls the distributed-rate-limiter for a single key and returns
// whether the request is allowed. On any communication error the request is
// allowed through (fail-open) and the error is logged.
func (rl *RateLimiter) check(ctx context.Context, key string, tokens int) bool {
	body, err := json.Marshal(rateLimitRequest{Key: key, Tokens: tokens})
	if err != nil {
		slog.Error("failed to marshal rate-limit request", "error", err, "key", key)
		return true // fail-open
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rl.checkURL, bytes.NewReader(body))
	if err != nil {
		slog.Error("failed to create rate-limit request", "error", err, "key", key)
		return true
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := rl.client.Do(req)
	if err != nil {
		slog.Error("rate-limiter service unreachable, failing open", "error", err, "key", key)
		return true
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return false
	}

	var result rateLimitResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("failed to decode rate-limit response", "error", err)
		return true
	}

	return result.Allowed
}

// Middleware returns an HTTP middleware that enforces both global and per-agent
// rate limits by calling the distributed-rate-limiter service.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting for internal endpoints.
		if r.URL.Path == "/metrics" || r.URL.Path == "/health" || r.URL.Path == "/v1/limiter-status" {
			next.ServeHTTP(w, r)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		isAnthropic := strings.HasPrefix(r.URL.Path, "/v1/messages")

		// 1. Global rate-limit check.
		if !rl.check(ctx, "global", 1) {
			slog.Warn("global rate limit exceeded",
				"path", r.URL.Path,
				"method", r.Method,
				"remote_addr", r.RemoteAddr,
			)
			w.Header().Set("Retry-After", "1")
			if isAnthropic {
				writeAnthropicRateLimitError(w)
			} else {
				http.Error(w, `{"error":"global rate limit exceeded","retry_after":1}`, http.StatusTooManyRequests)
			}
			return
		}

		// 2. Per-agent rate-limit check.
		// For /v1/messages, use x-api-key as the identity.
		// For other routes, use query param or URL param.
		agentID := ""
		if isAnthropic {
			if key := r.Header.Get("x-api-key"); key != "" {
				agentID = key
			} else if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
				agentID = strings.TrimPrefix(authHeader, "Bearer ")
			}
		} else {
			agentID = r.URL.Query().Get("agent_id")
			if agentID == "" {
				agentID = chi.URLParam(r, "agentID")
			}
		}

		if agentID != "" {
			agentKey := "agent:" + agentID
			if !rl.check(ctx, agentKey, 1) {
				slog.Warn("agent rate limit exceeded",
					"agent_id", agentID,
					"path", r.URL.Path,
					"method", r.Method,
				)
				w.Header().Set("Retry-After", "1")
				if isAnthropic {
					writeAnthropicRateLimitError(w)
				} else {
					http.Error(w, `{"error":"agent rate limit exceeded","retry_after":1}`, http.StatusTooManyRequests)
				}
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// writeAnthropicRateLimitError writes an Anthropic-format rate limit error.
func writeAnthropicRateLimitError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(proxy.RateLimitError(1))
}
