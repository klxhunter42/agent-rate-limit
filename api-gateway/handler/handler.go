package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/middleware"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy"
	"github.com/klxhunter/agent-rate-limit/api-gateway/provider"
	"github.com/klxhunter/agent-rate-limit/api-gateway/proxy"
	"github.com/klxhunter/agent-rate-limit/api-gateway/queue"
	"github.com/redis/go-redis/v9"
)

type profileCtxKey struct{}

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
	Status        string `json:"status"`
	QueueDepth    int64  `json:"queue_depth"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// ErrorLogEntry stores a single error record.
type ErrorLogEntry struct {
	Timestamp  string `json:"time"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Status     int    `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error"`
	Model      string `json:"model,omitempty"`
}

const errorLogMaxEntries = 100

var (
	errorLogMu    sync.Mutex
	errorLogBuf   []ErrorLogEntry
	errorLogTotal int
)

func pushError(entry ErrorLogEntry) {
	errorLogMu.Lock()
	defer errorLogMu.Unlock()
	errorLogTotal++
	if len(errorLogBuf) >= errorLogMaxEntries {
		errorLogBuf = errorLogBuf[1:]
	}
	errorLogBuf = append(errorLogBuf, entry)
}

// RoutingStrategyRequest is the payload for setting routing strategy.
type RoutingStrategyRequest struct {
	Strategy string `json:"strategy"`
}

// Handler holds dependencies for the HTTP handlers.
type Handler struct {
	queue           *queue.DragonflyClient
	metrics         *metrics.Metrics
	proxy           *proxy.AnthropicProxy
	codeAssistProxy *proxy.GeminiCodeAssistProxy
	openaiProxy     *proxy.OpenAIProxy
	geminiAPIProxy  *proxy.GeminiAPIProxy
	modelLimiter    *middleware.AdaptiveLimiter
	keyPool         *proxy.KeyPool
	cfg             *config.Config
	privacy         *privacy.Pipeline
	tokenStore      *provider.TokenStore
	resolver        *provider.Resolver
	anomalyDetector *middleware.AnomalyDetector
	startedAt       time.Time
	usageHandler    *UsageHandler
	quotaHandler    *QuotaHandler
	profileRedis    *redis.Client
	wsBroadcast     func(eventType string, data interface{})
	refreshWorker   *provider.RefreshWorker
}

// New creates a new Handler.
func New(q *queue.DragonflyClient, m *metrics.Metrics, p *proxy.AnthropicProxy, cap *proxy.GeminiCodeAssistProxy, oap *proxy.OpenAIProxy, gap *proxy.GeminiAPIProxy, ml *middleware.AdaptiveLimiter, kp *proxy.KeyPool, cfg *config.Config, priv *privacy.Pipeline, ts *provider.TokenStore, res *provider.Resolver, ad *middleware.AnomalyDetector, uh *UsageHandler, qh *QuotaHandler, profileRdb *redis.Client, wsFn func(string, interface{}), rw *provider.RefreshWorker) *Handler {
	return &Handler{queue: q, metrics: m, proxy: p, codeAssistProxy: cap, openaiProxy: oap, geminiAPIProxy: gap, modelLimiter: ml, keyPool: kp, cfg: cfg, privacy: priv, tokenStore: ts, resolver: res, anomalyDetector: ad, startedAt: time.Now(), usageHandler: uh, quotaHandler: qh, profileRedis: profileRdb, wsBroadcast: wsFn, refreshWorker: rw}
}

// ProfileNameFromContext extracts the profile name stored in the request context.
func ProfileNameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(profileCtxKey{}).(string)
	return v
}

// recordProfileUsage records both Prometheus metrics and Redis usage for a profile.
func (h *Handler) recordProfileUsage(profile, model string, input, output int, cost float64) {
	h.metrics.RecordProfileUsage(profile, model, input, output, cost)
	if h.usageHandler != nil {
		h.usageHandler.RecordProfileUsage(profile, model, input, output, cost)
	}
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

// maxRequestBody moved to cfg.MaxRequestBody

// Messages handles POST /v1/messages — transparent proxy to upstream.
// Applies system prompt injection, smart max_tokens, per-model concurrency
// limiting with auto-fallback, and retries on 429.
func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	// Read and validate body before acquiring any resources.
	body, err := io.ReadAll(io.LimitReader(r.Body, h.cfg.MaxRequestBody+1))
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
	if len(body) > int(h.cfg.MaxRequestBody) {
		writeJSON(w, http.StatusRequestEntityTooLarge, proxy.ErrorResponse{
			Type: "error",
			Error: proxy.ErrorDetail{
				Type:    "invalid_request_error",
				Message: fmt.Sprintf("request body exceeds %dMB limit", h.cfg.MaxRequestBody/1024/1024),
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

	// Extract model early for provider resolution.
	requestedModel, _ := payload["model"].(string)

	// Profile-based routing: check X-Profile header or arl_* API token.
	var profileOverride *Profile
	profileName := r.Header.Get("X-Profile")
	if profileName == "" && h.profileRedis != nil {
		// Check if the auth token is a profile API token.
		authKey := r.Header.Get("x-api-key")
		if authKey == "" {
			if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
				authKey = strings.TrimPrefix(ah, "Bearer ")
			}
		}
		if strings.HasPrefix(authKey, "arl_") {
			if resolved, err := ResolveProfileToken(h.profileRedis, authKey); err == nil && resolved != "" {
				profileName = resolved
			}
		}
	}
	if profileName != "" && h.profileRedis != nil {
		if p, perr := getProfile(r.Context(), h.profileRedis, profileName); perr == nil && p != nil {
			profileOverride = p
			if p.Model != "" {
				payload["model"] = p.Model
				requestedModel = p.Model
			} else if p.Target != "" && !provider.ModelBelongsToProvider(requestedModel, p.Target) {
				mapped := mapModelForTarget(requestedModel, p.Target)
				if mapped != requestedModel {
					slog.Info("profile model mapped", "profile", profileName, "original", requestedModel, "mapped", mapped, "target", p.Target)
					payload["model"] = mapped
					requestedModel = mapped
				}
			}
			slog.Info("profile routing", "profile", profileName, "model", requestedModel, "baseUrl", p.BaseURL)
		} else {
			slog.Warn("profile not found, using default routing", "profile", profileName, "error", perr)
		}
	}

	if profileName != "" {
		*r = *r.WithContext(context.WithValue(r.Context(), profileCtxKey{}, profileName))
	}

	var apiKey string
	var decision *provider.RoutingDecision
	var selectedTokenInfo *provider.TokenInfo
	if h.resolver != nil {
		decision = h.resolver.Resolve(requestedModel)
	}

	// Account pool: if profile has accountIds, pick from pool.
	// Otherwise use profile API key, resolved token, or key pool.
	if profileOverride != nil && len(profileOverride.AccountIDs) > 0 && h.tokenStore != nil {
		providerID := ""
		if profileOverride.Provider != "" {
			providerID = profileOverride.Provider
		} else if profileOverride.Target != "" {
			providerID = profileOverride.Target
		} else if decision != nil {
			providerID = decision.ProviderID
		}
		if providerID != "" {
			if tok, err := h.tokenStore.GetFromPool(providerID, profileOverride.AccountIDs); err == nil && tok != nil {
				apiKey = tok.AccessToken
				selectedTokenInfo = tok
				slog.Info("profile account pool selected", "profile", profileOverride.Name, "provider", providerID, "account", tok.AccountID)
			}
		}
	} else if profileOverride != nil {
		pid := profileOverride.Provider
		if pid == "" {
			pid = profileOverride.Target
		}
		if pid != "" && h.tokenStore != nil {
			if tok, err := h.tokenStore.GetDefault(pid); err == nil && tok != nil {
				apiKey = tok.AccessToken
				selectedTokenInfo = tok
				slog.Info("profile default token selected", "profile", profileOverride.Name, "provider", pid, "account", tok.AccountID)
			}
		}
	} else if decision != nil && decision.APIKey != "" {
		apiKey = decision.APIKey
	} else if !h.keyPool.Passthrough() {
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

	// Quota enforcement: check before acquiring slot (fail-open on errors).
	if h.quotaHandler != nil {
		providerID := "default"
		accountID := "default"
		if decision != nil {
			providerID = decision.ProviderID
		}
		if allowed, pct, _ := h.quotaHandler.CheckQuota(providerID, accountID, requestedModel); !allowed {
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"type":  "error",
				"error": map[string]string{"type": "quota_exceeded", "message": fmt.Sprintf("quota for %s at %.1f%%", requestedModel, pct)},
			})
			h.metrics.IncError("quota_exceeded")
			return
		} else if pct >= 80 && h.wsBroadcast != nil {
			h.wsBroadcast("quota-warning", map[string]any{"provider": providerID, "accountId": accountID, "model": requestedModel, "percentage": pct})
		}
	}

	// Non-GLM mode: require a resolved provider. No Z.AI fallback.
	if !h.cfg.GLMMode && decision == nil && profileOverride == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"type":  "error",
			"error": map[string]string{"type": "no_provider", "message": fmt.Sprintf("no provider configured for model %s - authenticate via /v1/auth/claude/start or configure an API key", requestedModel)},
		})
		h.metrics.IncError("no_provider")
		return
	}
	// Acquire model slot (may fallback).
	selectedModel, ok := h.modelLimiter.Acquire(requestedModel)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, proxy.OverloadedError("all model slots busy, please retry"))
		h.metrics.IncError("overloaded")
		return
	}
	defer h.modelLimiter.Release(selectedModel)
	h.modelLimiter.RecordSeenModel(selectedModel)

	if selectedModel != requestedModel {
		payload["model"] = selectedModel
		h.metrics.RecordFallback(requestedModel, selectedModel)
		slog.Info("model fallback",
			"requested", requestedModel,
			"selected", selectedModel,
		)
		// Re-resolve provider for the fallback model.
		if h.resolver != nil {
			if fb := h.resolver.Resolve(selectedModel); fb != nil {
				decision = fb
				if profileOverride == nil || profileOverride.APIKey == "" {
					if fb.APIKey != "" {
						apiKey = fb.APIKey
					}
				}
			}
		}
	}

	// Inject system prompt for token efficiency.
	if h.cfg.EnablePromptInjection {
		injectSystemPrompt(payload, h.cfg.PromptInjectionText)
	}

	// Smart max_tokens auto-adjustment.
	if h.cfg.EnableSmartMaxTokens {
		applySmartMaxTokens(payload, selectedModel)
	}

	// Strip fields unsupported by non-Anthropic upstreams.
	// Native Anthropic (claude-oauth bearer) supports context_management — keep it.
	isNativeAnthropic := decision != nil && decision.AuthMode == "bearer" && decision.Format == provider.FormatAnthropic
	stripUnsupportedFields(payload, isNativeAnthropic, selectedModel)
	slog.Info("strip debug", "model", selectedModel, "has_effort", payload["effort"] != nil, "has_thinking", payload["thinking"] != nil, "has_budget", payload["budget_tokens"] != nil)

	// Strip content block types unsupported by upstream (only needed for Z.AI).
	if h.cfg.GLMMode {
		filterUnsupportedContent(payload)
	}

	// Detect if request contains images for native vision routing.
	hasImages := proxy.HasImageContent(payload)

	// Re-encode modified payload.
	body, err = json.Marshal(payload)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode request"})
		return
	}

	// Privacy masking: detect and mask secrets/PII before proxying.
	var maskResult *privacy.MaskResult
	if h.privacy != nil {
		maskResult, _ = h.privacy.MaskRequest(body)
		if maskResult != nil {
			body = maskResult.MaskedBody
		}
	}

	isStream, _ := payload["stream"].(bool)

	// Build profile proxy options if profile override is active.
	profileOpts := &proxy.ProxyOptions{}
	if profileOverride != nil {
		if profileOverride.BaseURL != "" {
			profileOpts.UpstreamOverride = profileOverride.BaseURL
		}
		// When profile has a target provider, override model-based routing.
		if profileOverride.Target != "" {
			providerID := profileOverride.Target
			if profileOverride.Provider != "" {
				providerID = profileOverride.Provider
			}
			if d, ok := h.resolver.ResolveByProvider(providerID); ok {
				decision = d
			}
		}
	}

	// OAuth token refresh callback: on 401, refresh the token and retry once.
	oauthRefreshFn := func(oldKey string) (string, bool) {
		pid := ""
		if profileOverride != nil && profileOverride.Provider != "" {
			pid = profileOverride.Provider
		} else if decision != nil {
			pid = decision.ProviderID
		}
		if pid == "" {
			return "", false
		}
		tokens, err := h.tokenStore.ListByProvider(pid)
		if err != nil || len(tokens) == 0 {
			return "", false
		}
		for _, t := range tokens {
			if t.AccessToken == oldKey && t.RefreshToken != "" {
				if h.refreshWorker.RefreshOne(pid, t.AccountID) == nil {
					if refreshed, err := h.tokenStore.Get(pid, t.AccountID); err == nil && refreshed != nil {
						slog.Info("token refreshed on 401", "provider", pid, "account", t.AccountID)
						return refreshed.AccessToken, true
					}
				}
			}
		}
		return "", false
	}
	profileOpts.OnAuthError = oauthRefreshFn

	// Resolve CodeAssist project ID for gemini-oauth requests.
	codeAssistProjectID := ""
	if (decision != nil && decision.ProviderID == "gemini-oauth") && selectedTokenInfo != nil {
		codeAssistProjectID = selectedTokenInfo.ProjectID
		if codeAssistProjectID == "" && h.codeAssistProxy != nil {
			if pid, err := h.codeAssistProxy.ResolveProjectID(r.Context(), apiKey); err == nil && pid != "" {
				codeAssistProjectID = pid
				selectedTokenInfo.ProjectID = pid
				if err := h.tokenStore.Store(*selectedTokenInfo); err != nil {
					slog.Warn("failed to store resolved project ID", "error", err)
				}
			} else if err != nil {
				slog.Warn("failed to resolve codeassist project", "error", err)
			}
		}
	}

	// Feedback callback for adaptive limiter + key pool + anomaly detection.
	start := time.Now()
	feedbackFn := func(statusCode int, rtt time.Duration, headers http.Header) {
		h.modelLimiter.Feedback(selectedModel, statusCode, rtt, headers)
		if statusCode == 429 || statusCode == 503 {
			h.keyPool.Report429(apiKey)
		} else if statusCode >= 200 && statusCode < 300 {
			h.keyPool.ReportSuccess(apiKey)
		}
		if h.anomalyDetector != nil {
			anomaly := h.anomalyDetector.Record(float64(rtt.Milliseconds()))
			if anomaly.Type != middleware.AnomalyNone && anomaly.Severity >= middleware.SeverityHigh {
				slog.Warn("anomaly detected",
					"type", anomaly.Type,
					"severity", anomaly.Severity,
					"z_score", anomaly.Score,
					"value_ms", anomaly.Value,
					"mean_ms", anomaly.Mean,
					"model", selectedModel,
				)
				if h.wsBroadcast != nil {
					h.wsBroadcast("anomaly-detected", map[string]any{"type": int(anomaly.Type), "severity": int(anomaly.Severity), "model": selectedModel, "rtt_ms": anomaly.Value})
				}
			}
		}
		if statusCode >= 400 {
			if h.usageHandler != nil {
				h.usageHandler.RecordError(selectedModel)
			}
			pushError(ErrorLogEntry{
				Timestamp:  time.Now().UTC().Format(time.RFC3339),
				Method:     r.Method,
				Path:       r.URL.Path,
				Status:     statusCode,
				DurationMs: time.Since(start).Milliseconds(),
				Error:      http.StatusText(statusCode),
				Model:      selectedModel,
			})
			if h.wsBroadcast != nil {
				h.wsBroadcast("request-error", map[string]any{"model": selectedModel, "statusCode": statusCode, "rtt_ms": rtt.Milliseconds()})
			}
		} else if h.wsBroadcast != nil {
			h.wsBroadcast("request-completed", map[string]any{"model": selectedModel, "statusCode": statusCode, "rtt_ms": rtt.Milliseconds()})
		}
		// Capture Anthropic unified rate limit utilization from upstream response headers.
		if statusCode >= 200 && statusCode < 300 {
			hasRL := false
			for k := range headers {
				if strings.HasPrefix(strings.ToLower(k), "anthropic-ratelimit") {
					hasRL = true
					break
				}
			}
			if hasRL {
				prov := ""
				accID := ""
				if selectedTokenInfo != nil {
					prov = selectedTokenInfo.Provider
					accID = selectedTokenInfo.AccountID
				} else if decision != nil {
					prov = decision.ProviderID
				}
				if prov != "" && accID != "" {
					go h.storeRateLimitStatus(prov, accID, headers)
				}
			}
		}
	}

	if h.cfg.GLMMode && hasImages && (decision == nil || decision.ProviderID == "zai") {
		// GLM models: use dedicated Z.AI vision endpoint (OpenAI format).
		imgBytes, imgCount := analyzeImagePayload(payload)
		visionModel := selectVisionModel(imgBytes, imgCount)
		if visionModel != selectedModel {
			slog.Info("vision model auto-selected",
				"original", selectedModel,
				"selected", visionModel,
				"imageBytes", imgBytes,
				"imageCount", imgCount,
			)
			selectedModel = visionModel
			payload["model"] = selectedModel
			body, _ = json.Marshal(payload)
		}
		slog.Info("routing to native vision endpoint", "model", selectedModel)
		if err := h.proxy.ProxyNativeVision(w, r, apiKey, body, selectedModel, isStream, feedbackFn, maskResult); err != nil {
			slog.Error("vision proxy error", "error", err)
			h.metrics.IncError("upstream")
		}
	} else if hasImages && decision != nil {
		// Non-GLM models with images: re-resolve for the vision model and use normal routing.
		visionDecision := decision
		if selectedModel != requestedModel && h.resolver != nil {
			if vd := h.resolver.Resolve(selectedModel); vd != nil && vd.APIKey != "" {
				visionDecision = vd
				apiKey = vd.APIKey
			}
		}
		switch visionDecision.Format {
		case provider.FormatOpenAI:
			if err := h.openaiProxy.ProxyOpenAI(w, r, visionDecision.UpstreamURL, apiKey, body, selectedModel, isStream, feedbackFn, maskResult); err != nil {
				slog.Error("openai vision proxy error", "error", err)
				h.metrics.IncError("upstream")
			}
		case provider.FormatGemini:
			if visionDecision.ProviderID == "gemini-oauth" && h.codeAssistProxy != nil {
				if err := h.codeAssistProxy.ProxyCodeAssist(w, r, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, oauthRefreshFn, codeAssistProjectID); err != nil {
					slog.Error("code assist vision failed", "error", err)
					h.metrics.IncError("upstream")
				}
			} else if h.geminiAPIProxy != nil {
				if err := h.geminiAPIProxy.ProxyGemini(w, r, visionDecision.UpstreamURL, apiKey, body, selectedModel, isStream, feedbackFn, maskResult); err != nil {
					slog.Error("gemini vision proxy error", "error", err)
					h.metrics.IncError("upstream")
				}
			}
		default:
			opts := &proxy.ProxyOptions{
				AuthMode:         visionDecision.AuthMode,
				UpstreamOverride: visionDecision.UpstreamURL,
				ExtraHeaders:     visionDecision.ExtraHeaders,
				OnAuthError:      oauthRefreshFn,
			}
			if err := h.proxy.ProxyTransparent(w, r, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, opts); err != nil {
				slog.Error("vision proxy error", "error", err)
				h.metrics.IncError("upstream")
			}
		}
	} else if decision != nil {
		switch decision.Format {
		case provider.FormatOpenAI:
			if err := h.openaiProxy.ProxyOpenAI(w, r, decision.UpstreamURL, apiKey, body, selectedModel, isStream, feedbackFn, maskResult); err != nil {
				slog.Error("openai proxy error", "error", err)
				h.metrics.IncError("upstream")
			}
		case provider.FormatGemini:
			if decision.ProviderID == "gemini-oauth" && h.codeAssistProxy != nil {
				if err := h.codeAssistProxy.ProxyCodeAssist(w, r, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, oauthRefreshFn, codeAssistProjectID); err != nil {
					// Don't fallback to direct Gemini API with OAuth token (requires API key).
					slog.Error("code assist failed", "error", err)
					h.metrics.IncError("upstream")
				}
			} else if h.geminiAPIProxy != nil {
				if err := h.geminiAPIProxy.ProxyGemini(w, r, decision.UpstreamURL, apiKey, body, selectedModel, isStream, feedbackFn, maskResult); err != nil {
					slog.Error("gemini proxy error", "error", err)
					h.metrics.IncError("upstream")
				}
			}
		default:
			opts := &proxy.ProxyOptions{
				AuthMode:         decision.AuthMode,
				UpstreamOverride: decision.UpstreamURL,
				ExtraHeaders:     decision.ExtraHeaders,
				OnAuthError:      oauthRefreshFn,
			}
			if err := h.proxy.ProxyTransparent(w, r, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, opts); err != nil {
				slog.Error("proxy error", "error", err)
				h.metrics.IncError("upstream")
			}
		}
	} else if err := h.proxy.ProxyTransparent(w, r, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, profileOpts); err != nil {
		slog.Error("proxy error", "error", err)
		h.metrics.IncError("upstream")
	}
}

// Health returns the service health status.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	var depth int64
	if h.queue != nil {
		depth, _ = h.queue.QueueDepth(ctx)
	}
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:        "healthy",
		QueueDepth:    depth,
		UptimeSeconds: int64(time.Since(h.startedAt).Seconds()),
	})
}

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

// LimiterStatus returns current adaptive limiter state for monitoring.
func (h *Handler) LimiterStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"global":     h.modelLimiter.GlobalStatus(),
		"models":     h.modelLimiter.Status(),
		"seenModels": h.modelLimiter.SeenModels(),
		"keyPool":    h.keyPool.Status(),
		"glmMode":    h.cfg.GLMMode,
	})
}

// LimiterOverride sets or clears a manual concurrency limit for a model.
// Set limit=0 to clear an override.
func (h *Handler) LimiterOverride(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
		Limit int64  `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model is required"})
		return
	}

	h.modelLimiter.SetOverride(req.Model, req.Limit)
	action := "cleared"
	if req.Limit > 0 {
		action = "set to " + strconv.FormatInt(req.Limit, 10)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "model": req.Model, "override": action})
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

const rateLimitKeyPrefix = "arl:ratelimit:"

// RateLimitStatus holds cached Anthropic unified rate limit utilization for one account.
type RateLimitStatus struct {
	Provider     string    `json:"provider"`
	AccountID    string    `json:"account_id"`
	Util5h       float64   `json:"util_5h"`
	Util7d       float64   `json:"util_7d"`
	Status       string    `json:"status"`
	Status5h     string    `json:"status_5h,omitempty"`
	Status7d     string    `json:"status_7d,omitempty"`
	FallbackPct  float64   `json:"fallback_pct"`
	Reset5h      string    `json:"reset_5h,omitempty"`
	Reset7d      string    `json:"reset_7d,omitempty"`
	ResetUnified string    `json:"reset_unified,omitempty"`
	ReqRemaining string    `json:"req_remaining,omitempty"`
	TokRemaining string    `json:"tok_remaining,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (h *Handler) storeRateLimitStatus(provider, accountID string, headers http.Header) {
	if h.tokenStore == nil {
		return
	}
	p5h, _ := strconv.ParseFloat(headers.Get("anthropic-ratelimit-unified-5h-utilization"), 64)
	p7d, _ := strconv.ParseFloat(headers.Get("anthropic-ratelimit-unified-7d-utilization"), 64)
	pfb, _ := strconv.ParseFloat(headers.Get("anthropic-ratelimit-unified-fallback-percentage"), 64)
	// Normalize: Anthropic sends utilization as percentage (0-100).
	// If parsed value is <= 1, treat as fraction and convert to percent.
	if p5h > 0 && p5h <= 1 {
		p5h *= 100
	}
	if p7d > 0 && p7d <= 1 {
		p7d *= 100
	}
	if pfb > 0 && pfb <= 1 {
		pfb *= 100
	}
	slog.Info("ratelimit headers stored",
		"provider", provider, "account", accountID,
		"parsed_5h", p5h, "parsed_7d", p7d,
		"status", headers.Get("anthropic-ratelimit-unified-status"),
		"status_5h", headers.Get("anthropic-ratelimit-unified-5h-status"),
		"status_7d", headers.Get("anthropic-ratelimit-unified-7d-status"),
		"reset_5h", headers.Get("anthropic-ratelimit-unified-5h-reset"),
		"reset_7d", headers.Get("anthropic-ratelimit-unified-7d-reset"),
		"reset_unified", headers.Get("anthropic-ratelimit-unified-reset"),
		"req_remaining", headers.Get("anthropic-ratelimit-requests-remaining"),
		"tok_remaining", headers.Get("anthropic-ratelimit-tokens-remaining"),
	)
	rl := RateLimitStatus{
		Provider:     provider,
		AccountID:    accountID,
		Util5h:       p5h,
		Util7d:       p7d,
		Status:       headers.Get("anthropic-ratelimit-unified-status"),
		Status5h:     headers.Get("anthropic-ratelimit-unified-5h-status"),
		Status7d:     headers.Get("anthropic-ratelimit-unified-7d-status"),
		FallbackPct:  pfb,
		Reset5h:      headers.Get("anthropic-ratelimit-unified-5h-reset"),
		Reset7d:      headers.Get("anthropic-ratelimit-unified-7d-reset"),
		ResetUnified: headers.Get("anthropic-ratelimit-unified-reset"),
		ReqRemaining: headers.Get("anthropic-ratelimit-requests-remaining"),
		TokRemaining: headers.Get("anthropic-ratelimit-tokens-remaining"),
		UpdatedAt:    time.Now().UTC(),
	}
	data, err := json.Marshal(rl)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	h.tokenStore.Client().Set(ctx, rateLimitKeyPrefix+provider+":"+accountID, data, 6*time.Hour)
	if h.wsBroadcast != nil {
		h.wsBroadcast("ratelimit-updated", rl)
	}
}

// GetRateLimits returns cached Anthropic rate limit utilization for all accounts.
func (h *Handler) GetRateLimits(w http.ResponseWriter, r *http.Request) {
	if h.tokenStore == nil {
		writeJSON(w, http.StatusOK, []RateLimitStatus{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var results []RateLimitStatus
	iter := h.tokenStore.Client().Scan(ctx, 0, rateLimitKeyPrefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		data, err := h.tokenStore.Client().Get(ctx, iter.Val()).Bytes()
		if err != nil {
			continue
		}
		var s RateLimitStatus
		if json.Unmarshal(data, &s) == nil {
			results = append(results, s)
		}
	}
	if results == nil {
		results = []RateLimitStatus{}
	}
	writeJSON(w, http.StatusOK, results)
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
	"glm-4.5":     4096,
}

const fallbackMaxTokens = 4096

// unsupportedContentTypes are Anthropic-specific block types that GLM does not handle.
var unsupportedContentTypes = map[string]bool{
	"server_tool_use": true,
}

// unsupportedTopLevelFields are request fields Claude Code sends that non-Anthropic upstreams reject.
var unsupportedTopLevelFields = []string{
	"context_management",
	"service_tier",
}

// stripUnsupportedFields removes top-level request fields that upstream APIs reject.
// nativeAnthropic: if true, keep context_management (Anthropic supports it natively).
func stripUnsupportedFields(payload map[string]any, nativeAnthropic bool, model string) {
	for _, f := range unsupportedTopLevelFields {
		if f == "context_management" && nativeAnthropic {
			continue
		}
		delete(payload, f)
	}
	// Strip thinking params for models that don't support extended thinking.
	if strings.Contains(model, "haiku") || strings.Contains(model, "3-5-sonnet") {
		delete(payload, "thinking")
		delete(payload, "budget_tokens")
		delete(payload, "effort")
		// Strip effort from nested output_config.
		if oc, ok := payload["output_config"].(map[string]any); ok {
			delete(oc, "effort")
		}
		// Strip context_management edits that require thinking.
		if cm, ok := payload["context_management"].(map[string]any); ok {
			if edits, ok := cm["edits"].([]any); ok {
				var kept []any
				for _, e := range edits {
					if m, ok := e.(map[string]any); ok {
						if t, _ := m["type"].(string); strings.Contains(t, "thinking") {
							continue
						}
					}
					kept = append(kept, e)
				}
				if len(kept) == 0 {
					delete(payload, "context_management")
				} else {
					cm["edits"] = kept
				}
			}
		}
	}
}

// filterUnsupportedContent removes unsupported content block types from messages
// and rewrites Anthropic image format to GLM-compatible format.
func filterUnsupportedContent(payload map[string]any) {
	msgs, ok := payload["messages"].([]any)
	if !ok {
		return
	}
	for _, msg := range msgs {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		content, ok := m["content"].([]any)
		if !ok {
			continue
		}
		filtered := make([]any, 0, len(content))
		for _, block := range content {
			cb, ok := block.(map[string]any)
			if !ok {
				filtered = append(filtered, block)
				continue
			}
			t, _ := cb["type"].(string)
			if unsupportedContentTypes[t] {
				continue
			}
			if t == "image" {
				rewriteImageToGLMFormat(cb)
			}
			filtered = append(filtered, cb)
		}
		m["content"] = filtered
	}
}

// rewriteImageToGLMFormat converts Anthropic image blocks to GLM-compatible format.
// Anthropic: {"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}}
// Anthropic: {"type":"image","source":{"type":"url","url":"https://..."}}
// GLM native: {"type":"image_url","image_url":{"url":"data:image/png;base64,..."}}
func rewriteImageToGLMFormat(cb map[string]any) {
	src, ok := cb["source"].(map[string]any)
	if !ok {
		return
	}
	srcType, _ := src["type"].(string)

	var url string
	switch srcType {
	case "base64":
		mediaType, _ := src["media_type"].(string)
		data, _ := src["data"].(string)
		if mediaType == "" || data == "" {
			return
		}
		url = fmt.Sprintf("data:%s;base64,%s", mediaType, data)
	case "url":
		imgURL, _ := src["url"].(string)
		if base64URI := proxy.FetchImageAsBase64(imgURL); base64URI != "" {
			url = base64URI
		} else {
			// Z.AI vision API doesn't support external URLs - skip this image.
			slog.Warn("skipping unfetchable URL image", "url", imgURL)
			delete(cb, "source")
			cb["type"] = "text"
			cb["text"] = "[image could not be loaded]"
			return
		}
	default:
		return
	}

	// Rewrite to GLM image_url format.
	cb["type"] = "image_url"
	cb["image_url"] = map[string]any{"url": url}
	delete(cb, "source")
}

// analyzeImagePayload walks all messages and returns total base64 image data size
// (bytes) and image block count. Only counts image/image_url blocks with base64 data.
func analyzeImagePayload(payload map[string]any) (totalBytes int, imageCount int) {
	msgs, ok := payload["messages"].([]any)
	if !ok {
		return 0, 0
	}
	for _, msg := range msgs {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		content, ok := m["content"].([]any)
		if !ok {
			continue
		}
		for _, block := range content {
			cb, ok := block.(map[string]any)
			if !ok {
				continue
			}
			t, _ := cb["type"].(string)
			if t != "image" && t != "image_url" {
				continue
			}
			imageCount++
			// Anthropic image block: source.data (base64)
			if src, ok := cb["source"].(map[string]any); ok {
				if data, ok := src["data"].(string); ok {
					totalBytes += len(data)
				}
			}
			// GLM image_url block: image_url.url (data:...;base64,... or https://)
			if iu, ok := cb["image_url"].(map[string]any); ok {
				if url, ok := iu["url"].(string); ok {
					if idx := strings.Index(url, ";base64,"); idx >= 0 {
						totalBytes += len(url[idx+8:]) // skip ";base64,"
					}
				}
			}
		}
	}
	return totalBytes, imageCount
}

// selectVisionModel chooses the best vision model based on total image payload
// size and count. Uses a score combining both factors:
//
//	score = totalBase64KB + (imageCount * 300)
//
// glm-4.6v is the default (10 slots, best quality). Only upgrades to
// glm-4.6v-flashx for heavy payloads. glm-4.6v-flash (1 slot) is not
// auto-selected.
func selectVisionModel(totalBytes int, imageCount int) string {
	totalKB := totalBytes / 1024
	score := totalKB + imageCount*300

	if score > 2000 || imageCount >= 3 {
		return "glm-4.6v-flashx"
	}
	return "glm-4.6v"
}

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

func (h *Handler) GetRoutingStrategy(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"strategy": proxy.GetStrategy()})
}

func (h *Handler) SetRoutingStrategy(w http.ResponseWriter, r *http.Request) {
	var req RoutingStrategyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.Strategy != "round-robin" && req.Strategy != "fill-first" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "strategy must be 'round-robin' or 'fill-first'"})
		return
	}
	proxy.SetStrategy(req.Strategy)
	writeJSON(w, http.StatusOK, map[string]string{"strategy": proxy.GetStrategy()})
}

func (h *Handler) GetErrorLogs(w http.ResponseWriter, r *http.Request) {
	errorLogMu.Lock()
	entries := make([]ErrorLogEntry, len(errorLogBuf))
	copy(entries, errorLogBuf)
	errorLogMu.Unlock()
	writeJSON(w, http.StatusOK, entries)
}

func (h *Handler) GetErrorLogCount(w http.ResponseWriter, r *http.Request) {
	errorLogMu.Lock()
	total := errorLogTotal
	current := len(errorLogBuf)
	errorLogMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]int{"total": total, "buffered": current})
}

// knownModels is a static catalog of models across all supported providers.
// Limit=0 means "use config default". Pricing=0 means "not priced".
var knownModels = []struct {
	Name             string
	Provider         string
	Series           string
	Format           string
	InputPerMillion  float64
	OutputPerMillion float64
	ContextWindow    int
	ThinkingSupport  string
	ExtendedContext  bool
	NativeImageInput bool
	Deprecated       bool
}{
	// Z.AI / GLM — pricing from https://docs.z.ai/guides/overview/pricing
	{"glm-5.1", "zai", "5", "anthropic", 1.4, 4.4, 128000, "budget", false, false, false},
	{"glm-5", "zai", "5", "anthropic", 1.0, 3.2, 128000, "budget", false, false, false},
	{"glm-5-turbo", "zai", "5", "anthropic", 1.2, 4.0, 128000, "budget", false, false, false},
	{"glm-4.7", "zai", "4", "anthropic", 0.6, 2.2, 128000, "none", false, false, false},
	{"glm-4.7-flashx", "zai", "4", "anthropic", 0.07, 0.4, 128000, "none", false, false, false},
	{"glm-4.7-flash", "zai", "4", "anthropic", 0, 0, 128000, "none", false, false, false},
	{"glm-4.6", "zai", "4", "anthropic", 0.6, 2.2, 128000, "none", false, false, false},
	{"glm-4.5", "zai", "4", "anthropic", 0.6, 2.2, 128000, "none", false, false, false},
	{"glm-4.5-x", "zai", "4", "anthropic", 2.2, 8.9, 128000, "none", false, false, false},
	{"glm-4.5-air", "zai", "4", "anthropic", 0.2, 1.1, 128000, "none", false, false, false},
	{"glm-4.5-airx", "zai", "4", "anthropic", 1.1, 4.5, 128000, "none", false, false, false},
	{"glm-4.5-flash", "zai", "4", "anthropic", 0, 0, 128000, "none", false, false, false},
	{"glm-4-32b-0414-128k", "zai", "4", "anthropic", 0.1, 0.1, 128000, "none", false, false, false},
	{"glm-5v-turbo", "zai", "5-vision", "anthropic", 1.2, 4.0, 128000, "budget", false, true, false},
	{"glm-4.6v", "zai", "4-vision", "anthropic", 0.3, 0.9, 128000, "none", false, true, false},
	{"glm-4.6v-flashx", "zai", "4-vision", "anthropic", 0.04, 0.4, 128000, "none", false, true, false},
	{"glm-4.6v-flash", "zai", "4-vision", "anthropic", 0, 0, 128000, "none", false, true, false},
	{"glm-4.5v", "zai", "4-vision", "anthropic", 0.6, 1.8, 128000, "none", false, true, false},
	{"glm-ocr", "zai", "ocr", "anthropic", 0.03, 0.03, 128000, "none", false, true, false},
	// Anthropic
	{"claude-opus-4-7", "anthropic", "opus", "anthropic", 15, 75, 200000, "budget", false, false, false},
	{"claude-sonnet-4-6", "anthropic", "sonnet", "anthropic", 3, 15, 200000, "budget", false, false, false},
	{"claude-haiku-4-5", "anthropic", "haiku", "anthropic", 0.80, 4, 200000, "none", false, true, false},
	{"claude-3-5-sonnet-20241022", "anthropic", "sonnet-3.5", "anthropic", 3, 15, 200000, "none", false, false, false},
	{"claude-3-5-haiku-20241022", "anthropic", "haiku-3.5", "anthropic", 0.80, 4, 200000, "none", false, false, false},
	// Claude OAuth
	{"claude-opus-4-7", "claude", "opus", "anthropic", 15, 75, 200000, "budget", false, false, false},
	{"claude-sonnet-4-6", "claude", "sonnet", "anthropic", 3, 15, 200000, "budget", false, false, false},
	// OpenAI
	{"gpt-4o", "openai", "gpt-4", "openai", 2.50, 10, 128000, "none", false, false, false},
	{"gpt-4o-mini", "openai", "gpt-4", "openai", 0.15, 0.60, 128000, "none", false, false, false},
	{"o3", "openai", "o", "openai", 10, 40, 200000, "levels", false, false, false},
	{"o4-mini", "openai", "o", "openai", 1.50, 6, 200000, "levels", false, false, false},
	// Gemini
	{"gemini-2.5-pro", "gemini", "2.5", "gemini", 1.25, 10, 1048576, "budget", true, false, false},
	{"gemini-2.5-flash", "gemini", "2.5", "gemini", 0.15, 0.60, 1048576, "budget", true, false, false},
	{"gemini-2.0-flash", "gemini", "2.0", "gemini", 0.10, 0.40, 1048576, "none", true, false, false},
	// Gemini OAuth
	{"gemini-2.5-pro", "gemini-oauth", "2.5", "gemini", 1.25, 10, 1048576, "budget", true, false, false},
	{"gemini-2.5-flash", "gemini-oauth", "2.5", "gemini", 0.15, 0.60, 1048576, "budget", true, false, false},
	// GitHub Copilot
	{"gpt-4o", "copilot", "gpt-4", "openai", 0, 0, 128000, "none", false, false, false},
	{"claude-sonnet-4-6", "copilot", "sonnet", "anthropic", 0, 0, 128000, "none", false, false, false},
	// OpenRouter
	{"or-anthropic/claude-sonnet-4-6", "openrouter", "sonnet", "openai", 3, 15, 200000, "budget", false, false, false},
	{"or-openai/gpt-4o", "openrouter", "gpt-4", "openai", 2.50, 10, 128000, "none", false, false, false},
	{"or-google/gemini-2.5-pro", "openrouter", "gemini", "openai", 1.25, 10, 1048576, "budget", true, false, false},
	{"or-meta/llama-4-maverick", "openrouter", "llama", "openai", 0.20, 0.80, 1048576, "none", true, false, false},
	{"or-deepseek/deepseek-r1", "openrouter", "deepseek", "openai", 0.55, 2.19, 131072, "none", false, false, false},
	{"or-qwen/qwen3-235b-a22b", "openrouter", "qwen", "openai", 0.10, 0.40, 131072, "none", false, false, false},
	// Qwen
	{"qwen-max", "qwen", "max", "openai", 0.40, 1.20, 32768, "none", false, false, false},
	{"qwen-plus", "qwen", "plus", "openai", 0.08, 0.24, 131072, "none", false, false, false},
	{"qwen-turbo", "qwen", "turbo", "openai", 0.03, 0.09, 1048576, "none", true, false, false},
}

// providerDefaultModels maps each provider to its default model.
var providerDefaultModels = map[string]string{
	"claude-oauth": "claude-sonnet-4-20250514",
	"claude":       "claude-sonnet-4-20250514",
	"anthropic":    "claude-sonnet-4-20250514",
	"gemini-oauth": "gemini-2.5-flash",
	"gemini":       "gemini-2.5-flash",
	"openai":       "gpt-4o",
	"zai":          "glm-4.5",
	"deepseek":     "deepseek-chat",
	"copilot":      "gpt-4o",
	"openrouter":   "or-openai/gpt-4o",
	"qwen":         "qwen-plus",
}

// mapModelForTarget returns the default model for a target provider when the
// requested model does not belong to that provider.
func mapModelForTarget(model, targetProvider string) string {
	if d, ok := providerDefaultModels[targetProvider]; ok {
		return d
	}
	return model
}

func (h *Handler) GetModels(w http.ResponseWriter, r *http.Request) {
	type modelEntry struct {
		Name             string  `json:"name"`
		Provider         string  `json:"provider"`
		Series           string  `json:"series"`
		Limit            int     `json:"limit"`
		Format           string  `json:"format"`
		InputPerMillion  float64 `json:"input_per_million"`
		OutputPerMillion float64 `json:"output_per_million"`
		ContextWindow    int     `json:"context_window"`
		ThinkingSupport  string  `json:"thinking_support"`
		ExtendedContext  bool    `json:"extended_context"`
		NativeImageInput bool    `json:"native_image_input"`
		Deprecated       bool    `json:"deprecated"`
	}

	models := make([]modelEntry, 0, len(knownModels))
	for _, km := range knownModels {
		if !h.cfg.GLMMode && km.Provider == "zai" {
			continue
		}
		limit := h.cfg.DefaultLimit
		if l, ok := h.cfg.ModelLimits[km.Name]; ok {
			limit = l
		}
		models = append(models, modelEntry{
			Name:             km.Name,
			Provider:         km.Provider,
			Series:           km.Series,
			Limit:            limit,
			Format:           km.Format,
			InputPerMillion:  km.InputPerMillion,
			OutputPerMillion: km.OutputPerMillion,
			ContextWindow:    km.ContextWindow,
			ThinkingSupport:  km.ThinkingSupport,
			ExtendedContext:  km.ExtendedContext,
			NativeImageInput: km.NativeImageInput,
			Deprecated:       km.Deprecated,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func extractSeries(model string) string {
	if len(model) >= 5 && model[:5] == "glm-4" {
		return "4"
	}
	if len(model) >= 5 && model[:5] == "glm-5" {
		return "5"
	}
	return "unknown"
}
