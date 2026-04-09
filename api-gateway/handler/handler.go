package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/middleware"
	"github.com/klxhunter/agent-rate-limit/api-gateway/proxy"
	"github.com/klxhunter/agent-rate-limit/api-gateway/queue"
)

// ChatRequest is the payload sent by clients to enqueue an AI inference job.
type ChatRequest struct {
	AgentID     string            `json:"agent_id"`
	Model       string            `json:"model"`
	Messages    []map[string]any  `json:"messages"`
	MaxTokens   int               `json:"max_tokens"`
	Temperature float64           `json:"temperature"`
	Provider    string            `json:"provider"`
	Stream      bool              `json:"stream"`
	Metadata    map[string]string `json:"metadata"`
}

// ChatResponse is returned to the client after the job is queued.
type ChatResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	AgentID   string `json:"agent_id"`
}

// ResultResponse wraps a cached inference result.
type ResultResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Result    any    `json:"result,omitempty"`
}

// HealthResponse is the health-check payload.
type HealthResponse struct {
	Status string `json:"status"`
}

// Handler holds dependencies for the HTTP handlers.
type Handler struct {
	queue        *queue.DragonflyClient
	metrics      *metrics.Metrics
	proxy        *proxy.AnthropicProxy
	modelLimiter *middleware.AdaptiveLimiter
	keyPool      *proxy.KeyPool
	cfg          *config.Config
}

// New creates a new Handler.
func New(q *queue.DragonflyClient, m *metrics.Metrics, p *proxy.AnthropicProxy, ml *middleware.AdaptiveLimiter, kp *proxy.KeyPool, cfg *config.Config) *Handler {
	return &Handler{queue: q, metrics: m, proxy: p, modelLimiter: ml, keyPool: kp, cfg: cfg}
}

// ChatCompletions validates the request, enqueues the job, and returns a request ID.
func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		h.metrics.IncError("bad_request")
		return
	}

	if err := validateChatRequest(&req); err != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err})
		h.metrics.IncError("validation")
		return
	}

	requestID := uuid.New().String()

	job := &queue.Job{
		RequestID:   requestID,
		AgentID:     req.AgentID,
		Model:       req.Model,
		Messages:    req.Messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Provider:    req.Provider,
		RetryCount:  0,
		Metadata:    req.Metadata,
	}

	// Push to queue asynchronously so we don't block the response.
	// Use context.Background() because r.Context() is cancelled once the
	// HTTP response is written, which races with the goroutine.
	go func() {
		if err := h.queue.PushJob(context.Background(), job); err != nil {
			slog.Error("failed to push job to queue",
				"request_id", requestID,
				"error", err,
			)
			h.metrics.IncError("queue_push")
		}
	}()

	slog.Info("job queued",
		"request_id", requestID,
		"agent_id", req.AgentID,
		"model", req.Model,
		"provider", req.Provider,
	)

	writeJSON(w, http.StatusAccepted, ChatResponse{
		RequestID: requestID,
		Status:    "queued",
		AgentID:   req.AgentID,
	})
}

// GetResult retrieves a cached result for a request ID.
func (h *Handler) GetResult(w http.ResponseWriter, r *http.Request) {
	requestID := chi.URLParam(r, "requestID")
	if requestID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request_id required"})
		return
	}

	result, err := h.queue.GetResult(r.Context(), requestID)
	if err != nil {
		slog.Error("failed to get result", "request_id", requestID, "error", err)
		h.metrics.IncError("cache_get")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if result == "" {
		writeJSON(w, http.StatusOK, ResultResponse{
			RequestID: requestID,
			Status:    "pending",
		})
		return
	}

	var parsed any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		parsed = result
	}

	writeJSON(w, http.StatusOK, ResultResponse{
		RequestID: requestID,
		Status:    "completed",
		Result:    parsed,
	})
}

// Messages handles POST /v1/messages — transparent proxy to upstream.
// Applies system prompt injection, smart max_tokens, per-model concurrency
// limiting with auto-fallback, and retries on 429.
func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	// Resolve API key: pool key > client key.
	var apiKey string
	if !h.keyPool.Passthrough() {
		poolKey, ok := h.keyPool.Acquire()
		if !ok {
			writeJSON(w, http.StatusTooManyRequests, proxy.RateLimitError(10))
			return
		}
		apiKey = poolKey
	} else {
		apiKey = r.Header.Get("x-api-key")
		if apiKey == "" {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				apiKey = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}
		if apiKey == "" {
			writeJSON(w, http.StatusUnauthorized, proxy.ErrorResponse{
				Type: "error",
				Error: proxy.ErrorDetail{
					Type:    "authentication_error",
					Message: "x-api-key header is required",
				},
			})
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, proxy.ErrorResponse{
			Type: "error",
			Error: proxy.ErrorDetail{
				Type:    "invalid_request_error",
				Message: "failed to read request body",
			},
		})
		h.metrics.IncError("bad_request")
		return
	}

	// Parse body into a map for all modifications.
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, proxy.ErrorResponse{
			Type: "error",
			Error: proxy.ErrorDetail{
				Type:    "invalid_request_error",
				Message: "invalid JSON payload",
			},
		})
		h.metrics.IncError("bad_request")
		return
	}

	// Extract and acquire model slot (may fallback).
	requestedModel, _ := payload["model"].(string)
	selectedModel := h.modelLimiter.Acquire(requestedModel)
	defer h.modelLimiter.Release(selectedModel)

	if selectedModel != requestedModel {
		payload["model"] = selectedModel
		slog.Info("model fallback",
			"requested", requestedModel,
			"selected", selectedModel,
		)
	}

	// Inject system prompt for token efficiency.
	if h.cfg.EnablePromptInjection {
		injectSystemPrompt(payload, h.cfg.PromptInjectionText)
	}

	// Smart max_tokens auto-adjustment.
	if h.cfg.EnableSmartMaxTokens {
		applySmartMaxTokens(payload, selectedModel)
	}

	// Re-encode modified payload.
	body, err = json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode request"})
		return
	}

	isStream, _ := payload["stream"].(bool)

	// Feedback callback for adaptive limiter + key pool.
	feedbackFn := func(statusCode int, rtt time.Duration, headers http.Header) {
		h.modelLimiter.Feedback(selectedModel, statusCode, rtt, headers)
		if statusCode == 429 || statusCode == 503 {
			h.keyPool.Report429(apiKey)
		} else if statusCode >= 200 && statusCode < 300 {
			h.keyPool.ReportSuccess(apiKey)
		}
	}

	if err := h.proxy.ProxyTransparent(w, r, apiKey, body, selectedModel, isStream, feedbackFn); err != nil {
		slog.Error("proxy error", "error", err)
		h.metrics.IncError("upstream")
	}
}

// Health returns the service health status.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "healthy"})
}

// LimiterStatus returns current adaptive limiter state for monitoring.
func (h *Handler) LimiterStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"global":  h.modelLimiter.GlobalStatus(),
		"models":  h.modelLimiter.Status(),
		"keyPool": h.keyPool.Status(),
	})
}

func validateChatRequest(req *ChatRequest) string {
	if req.AgentID == "" {
		return "agent_id is required"
	}
	if len(req.Messages) == 0 {
		return "messages must be non-empty"
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 1024
	}
	if req.Temperature <= 0 {
		req.Temperature = 0.7
	}
	if req.Model == "" {
		req.Model = "glm-5"
	}
	if req.Provider == "" {
		req.Provider = "glm"
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		json.NewEncoder(w).Encode(v)
		return
	}
	w.Write(pretty)
	w.Write([]byte("\n"))
}

// injectSystemPrompt prepends the terse rules into the system field.
// Handles both string and array system formats from the Anthropic API.
func injectSystemPrompt(payload map[string]any, prompt string) {
	if prompt == "" {
		return
	}
	if sys, ok := payload["system"]; ok {
		switch v := sys.(type) {
		case string:
			payload["system"] = prompt + "\n\n" + v
		case []any:
			payload["system"] = append([]any{
				map[string]any{"type": "text", "text": prompt},
			}, v...)
		}
	} else {
		payload["system"] = prompt
	}
}

// modelMaxTokens defines optimal max_tokens defaults per model.
var modelMaxTokens = map[string]int{
	"glm-5.1":     8192,
	"glm-5-turbo": 4096,
	"glm-5":       8192,
}

const fallbackMaxTokens = 4096

// applySmartMaxTokens sets an optimal max_tokens if not already specified.
func applySmartMaxTokens(payload map[string]any, model string) {
	if _, exists := payload["max_tokens"]; exists {
		return // Respect client's explicit setting.
	}
	if limit, ok := modelMaxTokens[model]; ok {
		payload["max_tokens"] = limit
	} else {
		payload["max_tokens"] = fallbackMaxTokens
	}
}
