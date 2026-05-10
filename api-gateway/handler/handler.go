package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/h2non/bimg"
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
	"github.com/klxhunter/agent-rate-limit/api-gateway/tokenizer"
	"github.com/klxhunter/agent-rate-limit/api-gateway/toolfilter"
	"github.com/redis/go-redis/v9"
)

type profileCtxKey struct{}

type accountCtxKey struct{}

func AccountIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(accountCtxKey{}).(string)
	return v
}

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
	optimizers      *Optimizers
	sidecarURL      string // empty = sidecar disabled
	sessionManager  *proxy.ClaudeSessionManager
	mcpProxy        *proxy.MCPProxy
}

// New creates a new Handler.
func New(q *queue.DragonflyClient, m *metrics.Metrics, p *proxy.AnthropicProxy, cap *proxy.GeminiCodeAssistProxy, oap *proxy.OpenAIProxy, gap *proxy.GeminiAPIProxy, ml *middleware.AdaptiveLimiter, kp *proxy.KeyPool, cfg *config.Config, priv *privacy.Pipeline, ts *provider.TokenStore, res *provider.Resolver, ad *middleware.AnomalyDetector, uh *UsageHandler, qh *QuotaHandler, profileRdb *redis.Client, wsFn func(string, interface{}), rw *provider.RefreshWorker, opt *Optimizers, mcp *proxy.MCPProxy) *Handler {
	var sidecarURL string
	if cfg.CLISidecarEnabled {
		sidecarURL = cfg.CLISidecarURL
	}
	slog.Info("handler init", "sidecar_enabled", cfg.CLISidecarEnabled, "sidecar_url", sidecarURL, "glm_mode", cfg.GLMMode)
	return &Handler{queue: q, metrics: m, proxy: p, codeAssistProxy: cap, openaiProxy: oap, geminiAPIProxy: gap, modelLimiter: ml, keyPool: kp, cfg: cfg, privacy: priv, tokenStore: ts, resolver: res, anomalyDetector: ad, startedAt: time.Now(), usageHandler: uh, quotaHandler: qh, profileRedis: profileRdb, wsBroadcast: wsFn, refreshWorker: rw, optimizers: opt, sidecarURL: sidecarURL, sessionManager: proxy.NewClaudeSessionManager(), mcpProxy: mcp}
}

// ProfileNameFromContext extracts the profile name stored in the request context.
func ProfileNameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(profileCtxKey{}).(string)
	return v
}

// recordProfileUsage records both Prometheus metrics and Redis usage for a profile.

// trySidecarOrDirect routes transparent claude-oauth requests with billing header
// injection. Path priority: Go direct (fastest) -> sidecar (Node.js fallback) -> direct proxy.
func (h *Handler) trySidecarOrDirect(w http.ResponseWriter, r *http.Request, apiKey string, body []byte, model string, isStream bool, feedback proxy.FeedbackFunc, maskResult *privacy.MaskResult, opts *proxy.ProxyOptions, transparent bool) error {
	profile := ProfileNameFromContext(r.Context())
	slog.Info("trySidecarOrDirect", "transparent", transparent, "sidecarURL", h.sidecarURL, "model", model, "profile", profile)
	if !transparent {
		return h.proxy.ProxyTransparent(w, r, apiKey, body, model, isStream, feedback, maskResult, opts)
	}

	// Fix headers for profile-routed OAuth tokens.
	if strings.HasPrefix(apiKey, "sk-ant-oat01-") {
		r.Header.Set("Authorization", "Bearer "+apiKey)
		r.Header.Del("x-api-key")
		if beta := r.Header.Get("anthropic-beta"); beta != "" {
			if !strings.Contains(beta, "oauth-2025-04-20") {
				r.Header.Set("anthropic-beta", beta+",oauth-2025-04-20")
			}
		} else {
			r.Header.Set("anthropic-beta", "oauth-2025-04-20")
		}
	}

	// Path 1: Go direct billing injection (fastest, no sidecar hop).
	start := time.Now()
	billingOpts := opts
	if billingOpts == nil {
		billingOpts = &proxy.ProxyOptions{Transparent: true}
	} else {
		billingOptsCopy := *opts
		billingOpts = &billingOptsCopy
	}
	billingOpts.BillingInjected = true
	injectedBody := proxy.InjectBillingHeader(body)
	err := h.proxy.ProxyTransparent(w, r, apiKey, injectedBody, model, isStream, feedback, maskResult, billingOpts)
	if err == nil {
		h.metrics.RecordBillingPath("go_direct", model, profile)
		h.metrics.RecordBillingPathLatency("go_direct", model, time.Since(start).Seconds())
		return nil
	}
	if !errors.Is(err, proxy.ErrBillingRejected) {
		return err
	}

	// Billing rejected - record and fall back.
	h.metrics.RecordBillingPath("billing_rejected", model, profile)

	// Path 2: Sidecar fallback (Node.js TLS fingerprint).
	slog.Warn("Go billing rejected, trying sidecar fallback", "model", model)
	if h.sidecarURL != "" {
		start = time.Now()
		if err := h.proxy.ProxySidecar(w, r, h.sidecarURL, body, model, isStream, feedback, maskResult, opts); err != nil {
			slog.Warn("sidecar failed, falling back to direct", "error", err, "model", model)
			h.metrics.RecordBillingPath("direct", model, profile)
			h.metrics.RecordBillingPathLatency("direct", model, time.Since(start).Seconds())
			return h.proxy.ProxyTransparent(w, r, apiKey, body, model, isStream, feedback, maskResult, opts)
		}
		h.metrics.RecordBillingPath("sidecar", model, profile)
		h.metrics.RecordBillingPathLatency("sidecar", model, time.Since(start).Seconds())
		return nil
	}

	// Path 3: Direct proxy (no billing header).
	start = time.Now()
	err = h.proxy.ProxyTransparent(w, r, apiKey, body, model, isStream, feedback, maskResult, opts)
	h.metrics.RecordBillingPath("direct", model, profile)
	h.metrics.RecordBillingPathLatency("direct", model, time.Since(start).Seconds())
	return err
}

// handleFallbackProxy routes a request to a fallback provider's proxy.
func (h *Handler) handleFallbackProxy(w http.ResponseWriter, r *http.Request, fb *provider.RoutingDecision, body []byte, model string, isStream bool, feedbackFn proxy.FeedbackFunc, maskResult *privacy.MaskResult, oauthRefreshFn func(string) (string, bool), projectID string) {
	switch fb.Format {
	case provider.FormatOpenAI:
		if h.openaiProxy != nil {
			if err := h.openaiProxy.ProxyOpenAI(w, r, fb.UpstreamURL, fb.APIKey, body, model, isStream, feedbackFn, maskResult, fb.MaxContinuations, fb.ToolMode); err != nil {
				slog.Error("fallback openai proxy error", "error", err, "model", model)
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			}
		}
	case provider.FormatGemini:
		if h.geminiAPIProxy != nil {
			if err := h.geminiAPIProxy.ProxyGemini(w, r, fb.UpstreamURL, fb.APIKey, body, model, isStream, feedbackFn, maskResult); err != nil {
				slog.Error("fallback gemini proxy error", "error", err, "model", model)
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			}
		}
	default:
		if h.openaiProxy != nil {
			if err := h.openaiProxy.ProxyOpenAI(w, r, fb.UpstreamURL, fb.APIKey, body, model, isStream, feedbackFn, maskResult, fb.MaxContinuations, fb.ToolMode); err != nil {
				slog.Error("fallback proxy error", "error", err, "model", model)
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			}
		}
	}
}

// tryProviderFallback resolves the next provider for a model, skipping the excluded provider.
func (h *Handler) tryProviderFallback(model, excludeProvider string) *provider.RoutingDecision {
	if h.resolver == nil {
		return nil
	}
	return h.resolver.ResolveFallback(model, []string{excludeProvider})
}

// tryModelFallback returns a lighter model from the same provider that is not cooling down.
func (h *Handler) tryModelFallback(providerID, model string) (fallbackModel string, ok bool) {
	fallbacks, exists := modelFallbacks[model]
	if !exists || len(fallbacks) == 0 {
		return "", false
	}
	for _, fb := range fallbacks {
		if h.resolver != nil && !h.resolver.IsCoolingDown(providerID, fb) {
			return fb, true
		}
	}
	return "", false
}

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

// isClaudeOAuthToken checks if the request carries a claude-oauth token
// via Authorization: Bearer or x-api-key header. Returns the token and true if found.
func isClaudeOAuthToken(r *http.Request) (string, bool) {
	if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
		if tok := strings.TrimPrefix(ah, "Bearer "); tok != "" && !strings.HasPrefix(tok, "arl_") {
			return tok, true
		}
	}
	if ak := r.Header.Get("x-api-key"); strings.HasPrefix(ak, "sk-ant-oat01-") {
		return ak, true
	}
	return "", false
}

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
	// Transparent passthrough for claude-oauth: preserve exact CLI payload.
	transparent := false
		// GLM mode without claude-oauth accounts: route claude models to Z.AI instead of transparent.
		hasClaudeOAuth := false
		if h.cfg.GLMMode && h.tokenStore != nil {
			if tokens, err := h.tokenStore.ListByProvider("claude-oauth"); err == nil && len(tokens) > 0 {
				hasClaudeOAuth = true
			}
		}
	if h.resolver != nil {
		d := h.resolver.Resolve(requestedModel)
		slog.Info("resolver result", "model", requestedModel, "provider", func() string {
			if d != nil {
				return d.ProviderID
			}
			return "nil"
		}(), "has_oauth_token", func() bool {
			_, ok := isClaudeOAuthToken(r)
			return ok
		}())
		// Transparent passthrough for claude-oauth models.
		// In GLM mode without stored claude-oauth accounts, skip transparent
		// and let the model route to Z.AI instead (avoids 401 from expired client tokens).
		if !h.cfg.GLMMode || hasClaudeOAuth {
			// No stored token but client has Bearer or x-api-key OAuth: use transparent resolve.
			if d == nil && provider.ModelBelongsToProvider(requestedModel, "claude-oauth") {
				if _, ok := isClaudeOAuthToken(r); ok {
					d = h.resolver.ResolveTransparent(requestedModel)
				}
			}
			if d != nil && d.ProviderID == "claude-oauth" {
				if _, ok := isClaudeOAuthToken(r); ok {
					transparent = true
				}
			}
			// Override: client sends OAuth token for claude model - always transparent
			// regardless of what the resolver returned (may have resolved to 'anthropic'
			// via stored API key, but we want transparent for OAuth tokens).
			if !transparent && provider.ModelBelongsToProvider(requestedModel, "claude-oauth") {
				if _, ok := isClaudeOAuthToken(r); ok {
					transparent = true
					slog.Info("forced transparent for claude-oauth", "model", requestedModel)
				}
			}
		} else if provider.ModelBelongsToProvider(requestedModel, "claude-oauth") {
			slog.Info("glm mode: skipping transparent, routing to zai", "model", requestedModel)
		}
	}

	// Rewrite sonnet/opus requests to haiku (org-level rate limit workaround).
	// Disabled: testing direct sonnet routing via claude-oauth.
	// if strings.Contains(requestedModel, "sonnet") || strings.Contains(requestedModel, "opus") {
	// 	requestedModel = "claude-haiku-4-5-20251001"
	// 	payload["model"] = requestedModel
	// }
	// Clamp max_tokens to upstream model's hard limit (always, even in transparent mode,
	// to prevent 400 errors from Anthropic when max_tokens exceeds model limits).
	clampMaxTokens(payload, requestedModel)

	// Profile-based routing: arl_* token or validated X-Profile header
	var profileOverride *Profile
	var profileName string
	if h.profileRedis != nil {
		authKey := r.Header.Get("x-api-key")
		if authKey == "" {
			if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
				authKey = strings.TrimPrefix(ah, "Bearer ")
			}
		}
		// arl_ tokens resolve to a profile directly (validated by Redis lookup)
		if strings.HasPrefix(authKey, "arl_") {
			if resolved, err := ResolveProfileToken(h.profileRedis, authKey); err == nil && resolved != "" {
				profileName = resolved
			}
		}
		// X-Profile header: validate against profile's stored API key
		if profileName == "" {
			if xProfile := r.Header.Get("X-Profile"); xProfile != "" {
				if p, perr := getProfile(r.Context(), h.profileRedis, xProfile); perr == nil && p != nil {
					if p.APIKey != "" && hmac.Equal([]byte(authKey), []byte(p.APIKey)) {
						profileName = xProfile
						profileOverride = p
					} else {
						slog.Warn("profile API key mismatch", "profile", xProfile, "remote", r.RemoteAddr)
						writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid API key for profile: " + xProfile})
						return
					}
				}
			}
		}
	}
	if profileName != "" && h.profileRedis != nil && profileOverride == nil {
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
			slog.Warn("profile not found, rejecting request", "profile", profileName, "error", perr)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "profile not found: " + profileName})
			return
		}
	}

	// Require valid profile when not in GLM mode.
	// GLM mode uses ZAI_API_KEYS from env, no profile needed.
	if profileName == "" && !h.cfg.GLMMode {
		slog.Warn("no valid profile, rejecting request", "path", r.URL.Path, "remote", r.RemoteAddr)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "valid profile required"})
		return
	}

	if profileName != "" {
		*r = *r.WithContext(context.WithValue(r.Context(), profileCtxKey{}, profileName))
	}

	var apiKey string
	var decision *provider.RoutingDecision
	var selectedTokenInfo *provider.TokenInfo
	if h.resolver != nil {
		// Profile with explicit target: resolve by provider, not by model name.
		// This lets any model name (e.g. claude-sonnet-4-6) route through the
		// profile's target provider (e.g. lotuss).
		if profileOverride != nil && profileOverride.Target != "" {
			if d, ok := h.resolver.ResolveByProvider(profileOverride.Target); ok && d != nil {
				decision = d
			}
		}
		if decision == nil {
			decision = h.resolver.Resolve(requestedModel)
		}
		// Fallback: no stored token for claude-oauth, use transparent resolve
		// so client's Bearer token is used instead.
		if decision == nil && transparent {
			decision = h.resolver.ResolveTransparent(requestedModel)
		}
		// Fallback: if decision is nil (provider cooling down) and profile has a target,
		// try the next provider from model rules.
		if decision == nil && profileOverride != nil && profileOverride.Target != "" {
			if fb := h.resolver.ResolveFallback(requestedModel, []string{profileOverride.Target}); fb != nil {
				decision = fb
				slog.Info("provider cooldown fallback", "original", profileOverride.Target, "fallback", fb.ProviderID, "model", requestedModel)
			}
		}
	}

	// Resolve effective account IDs: check target-level first, then top-level.
	var effectiveAccountIDs []string
	var effectiveProviderID string
	if profileOverride != nil {
		effectiveProviderID = profileOverride.Provider
		if effectiveProviderID == "" {
			effectiveProviderID = profileOverride.Target
		}
		for _, t := range profileOverride.Targets {
			if t.Target == effectiveProviderID && len(t.AccountIDs) > 0 {
				effectiveAccountIDs = t.AccountIDs
				break
			}
		}
		if len(effectiveAccountIDs) == 0 && len(profileOverride.AccountIDs) > 0 {
			effectiveAccountIDs = profileOverride.AccountIDs
		}
	}

	// Profile must have selected accounts when targeting a provider.
	// If primary target has no accounts, try fallback to next target that has accounts.
	if profileOverride != nil && !profileOverride.PassthroughAuth && len(effectiveAccountIDs) == 0 && effectiveProviderID != "" {
		fallbackFound := false
		for _, t := range profileOverride.Targets {
			if t.Target == effectiveProviderID {
				continue
			}
			if len(t.AccountIDs) > 0 {
				effectiveProviderID = t.Target
				effectiveAccountIDs = t.AccountIDs
				if d, ok := h.resolver.ResolveByProvider(t.Target); ok && d != nil {
					decision = d
				}
				slog.Info("profile fallback to target with accounts", "profile", profileOverride.Name, "original_provider", profileOverride.Target, "fallback_provider", t.Target, "accounts", t.AccountIDs)
				fallbackFound = true
				break
			}
		}
		if !fallbackFound {
			slog.Warn("profile has no accounts selected", "profile", profileOverride.Name, "provider", effectiveProviderID)
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "profile has no accounts selected for provider: " + effectiveProviderID})
			return
		}
	}

	// Account pool: if profile has accountIds, pick from pool.
	// Otherwise use profile API key, resolved token, or key pool.
	if profileOverride != nil && len(effectiveAccountIDs) > 0 && h.tokenStore != nil {
		providerID := effectiveProviderID
		if providerID == "" && decision != nil {
			providerID = decision.ProviderID
		}
		if providerID != "" {
			poolAccountIDs := effectiveAccountIDs
			if h.resolver != nil {
				var available []string
				for _, aid := range effectiveAccountIDs {
					if !h.resolver.IsAccountCoolingDown(providerID, aid) {
						available = append(available, aid)
					}
				}
				if len(available) > 0 {
					poolAccountIDs = available
				}
			}
			if tok, err := h.tokenStore.GetFromPool(providerID, poolAccountIDs); err == nil && tok != nil {
				apiKey = tok.AccessToken
				selectedTokenInfo = tok
				slog.Info("profile account pool selected", "profile", profileOverride.Name, "provider", providerID, "account", tok.AccountID)
			}
		}
	} else if profileOverride != nil {
		if profileOverride.PassthroughAuth {
			// Passthrough: use client's own Bearer token from Authorization header.
			if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
				apiKey = strings.TrimPrefix(ah, "Bearer ")
			}
			if apiKey == "" {
				apiKey = r.Header.Get("x-api-key")
			}
			// Strip arl_ prefix if present (client sent same key for profile lookup).
			if strings.HasPrefix(apiKey, "arl_") {
				apiKey = ""
			}
			if apiKey != "" {
				slog.Info("profile passthrough auth", "profile", profileOverride.Name)
			}
		} else {
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
			// Fallback to key pool (ZAI_API_KEYS) when profile has no
			// stored token for the target provider (common in GLM mode).
			if apiKey == "" && decision != nil && decision.APIKey != "" {
				apiKey = decision.APIKey
				slog.Info("profile key pool fallback", "profile", profileOverride.Name, "provider", pid)
			}
		}
	} else if transparent {
		// Transparent passthrough: use client's own OAuth token.
		if tok, ok := isClaudeOAuthToken(r); ok {
			slog.Info("claude-oauth transparent passthrough", "token_prefix", tok[:min(20, len(tok))]+"...")
			h.sessionManager.BootstrapIfNeeded(tok)
			apiKey = tok
		}
	} else if decision != nil && decision.APIKey != "" {
		apiKey = decision.APIKey
		// claude-oauth passthrough: prefer client's own token when available,
		// since stored tokens may be stale/expired. [[PERSON_16]] Code CLI has its own valid session.
		if decision.ProviderID == "claude-oauth" {
			if tok, ok := isClaudeOAuthToken(r); ok {
				h.sessionManager.BootstrapIfNeeded(tok)
				slog.Info("claude-oauth passthrough activated", "token_prefix", tok[:min(20, len(tok))]+"...")
				apiKey = tok
			}
		}
	} else if h.cfg.GLMMode && h.keyPool != nil {
		if kpKey, ok := h.keyPool.Acquire(); ok {
			apiKey = kpKey
			slog.Info("glm mode key pool selected", "model", requestedModel)
		}
	}

	if apiKey == "" {
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


	// GLM guard: never send Anthropic OAuth token to Z.AI upstream.
	// When GLM_MODE=true, model=glm-*, and key pool is empty, the fallback
	// above picks up the client's x-api-key (Anthropic OAuth token).
	// Forwarding that to Z.AI causes 401. Reject early with a clear error.
	if h.cfg.GLMMode && decision != nil && decision.ProviderID == "zai" && strings.HasPrefix(apiKey, "sk-ant-oat01-") {
		slog.Warn("glm request with anthropic oauth token rejected", "model", requestedModel, "provider", decision.ProviderID)
		writeJSON(w, http.StatusUnauthorized, proxy.ErrorResponse{
			Type: "error",
			Error: proxy.ErrorDetail{
				Type:    "authentication_error",
				Message: "No Z.AI API key available. Configure ZAI_API_KEYS or use a profile with a Z.AI token.",
			},
		})
		return
	}

	if !transparent && strings.HasPrefix(apiKey, "sk-ant-oat01-") {
		transparent = true
		slog.Info("profile OAuth token, enabling transparent sidecar routing", "profile", func() string {
			if profileOverride != nil {
				return profileOverride.Name
			}
			return ""
		}())
	}

	// Store account ID in context for usage tracking.
	if selectedTokenInfo != nil {
		*r = *r.WithContext(context.WithValue(r.Context(), accountCtxKey{}, selectedTokenInfo.AccountID))
	} else if decision != nil && decision.AccountID != "" {
		*r = *r.WithContext(context.WithValue(r.Context(), accountCtxKey{}, decision.AccountID))
	}

	// Profile BaseURL override: if profile specifies a custom base URL, replace upstream.
	if decision != nil && profileOverride != nil && profileOverride.BaseURL != "" {
		decision.UpstreamURL = profileOverride.BaseURL
		slog.Info("profile base_url override", "profile", profileOverride.Name, "upstream", decision.UpstreamURL)
	}

	// Apply provider-level model override and max_tokens clamp (e.g., lotuss -> "default")
	if decision != nil && decision.ModelOverride != "" {
		slog.Info("model override", "original", requestedModel, "override", decision.ModelOverride, "provider", decision.ProviderID)
		payload["model"] = decision.ModelOverride
		requestedModel = decision.ModelOverride
	}
	if decision != nil && decision.MaxTokens > 0 {
		if mt, ok := payload["max_tokens"].(float64); ok && mt > float64(decision.MaxTokens) {
			slog.Info("max_tokens clamped", "original", int(mt), "clamped", decision.MaxTokens, "provider", decision.ProviderID)
			payload["max_tokens"] = decision.MaxTokens
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

	// No provider resolved and no profile override: reject.
	if decision == nil && profileOverride == nil {
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
		// Prevent cross-provider fallback (e.g. claude -> glm or glm -> claude).
		reqIsGLM := strings.HasPrefix(requestedModel, "glm-")
		selIsGLM := strings.HasPrefix(selectedModel, "glm-")
		if reqIsGLM != selIsGLM {
			slog.Warn("cross-provider fallback prevented",
				"requested", requestedModel,
				"fallback_was", selectedModel,
			)
			selectedModel = requestedModel
		} else {
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
	}

	var maskResult *privacy.MaskResult
	hasImages := false

	// Skip prompt injection for transparent claude-oauth: avoids adding input tokens.
	// Optimizer (dedup/whitespace) still runs below to reduce existing input tokens.
	if !transparent {
		// Inject system prompt for token efficiency.
		if h.cfg.EnablePromptInjection {
			injectSystemPrompt(payload, h.cfg.PromptInjectionText)
		}
	}

	// Smart max_tokens auto-adjustment.
	if h.cfg.EnableSmartMaxTokens {
		applySmartMaxTokens(payload, selectedModel)
	}

	// Strip fields unsupported by non-Anthropic upstreams.
	// Native Anthropic (claude-oauth bearer) supports context_management — keep it.

	// --- DEBUG: dump incoming request (DEBUG=true) ---
	if h.cfg.DebugMode {
		slog.Info("debug incoming request",
			"method", r.Method,
			"path", r.URL.Path,
			"content_length", r.ContentLength,
			"model_requested", payload["model"],
			"model_selected", selectedModel,
			"stream", payload["stream"],
			"has_system", payload["system"] != nil,
			"msg_count", func() int {
				if msgs, ok := payload["messages"].([]any); ok {
					return len(msgs)
				}
				return 0
			}(),
			"provider", func() string {
				if decision != nil {
					return decision.ProviderID
				}
				return ""
			}(),
			"auth_mode", func() string {
				if decision != nil {
					return decision.AuthMode
				}
				return ""
			}(),
			"headers", map[string][]string(r.Header),
		)
		// Async debug dump - don't block request processing on expensive JSON marshal.
		if rawDump, err := json.Marshal(payload); err == nil {
			const maxDumpLen = 10240
			dumpStr := string(rawDump)
			if len(dumpStr) > maxDumpLen {
				dumpStr = dumpStr[:maxDumpLen] + "... [truncated]"
			}
			go slog.Info("debug RAW PAYLOAD before strip", "payload_len", len(rawDump), "payload_json", dumpStr)
		}
	}

	// Early exit if client already disconnected (e.g. old CLI with short timeout).
	if err := r.Context().Err(); err != nil {
		slog.Info("client disconnected before strip", "error", err)
		return
	}

	isNativeAnthropic := decision != nil && decision.AuthMode == "bearer" && decision.Format == provider.FormatAnthropic
	stripUnsupportedFields(payload, isNativeAnthropic, selectedModel)

	// GLM models pass through content blocks as-is for full tool support.
	if decision != nil && decision.ProviderID == "zai" && !strings.HasPrefix(selectedModel, "glm-") {
		filterUnsupportedContent(payload)
	}

	// Async: post-strip diagnostic - capture data synchronously, log in background.
	if h.cfg.DebugMode {
		var topKeys []string
		for k := range payload {
			topKeys = append(topKeys, k)
		}
		hasTools := payload["tools"] != nil
		hasToolChoice := payload["tool_choice"] != nil
		hasThinking := payload["thinking"] != nil
		hasOutputConfig := payload["output_config"] != nil
		hasStreamOpts := payload["stream_options"] != nil
		hasMetadata := payload["metadata"] != nil
		hasServiceTier := payload["service_tier"] != nil
		hasContextMgmt := payload["context_management"] != nil
		hasEffort := payload["effort"] != nil
		hasBudgetTokens := payload["budget_tokens"] != nil

		var msgTypes []string
		if msgs, ok := payload["messages"].([]any); ok {
			for i, msg := range msgs {
				if m, ok := msg.(map[string]any); ok {
					if content, ok := m["content"].([]any); ok {
						for _, block := range content {
							if cb, ok := block.(map[string]any); ok {
								t, _ := cb["type"].(string)
								var extraKeys []string
								for k := range cb {
									if k != "type" && k != "text" && k != "source" && k != "id" && k != "name" && k != "input" && k != "tool_use_id" && k != "content" && k != "thinking" {
										extraKeys = append(extraKeys, k+":"+fmt.Sprintf("%v", cb[k]))
									}
								}
								if len(extraKeys) > 0 {
									msgTypes = append(msgTypes, fmt.Sprintf("msg[%d]:%s(%s)", i, t, strings.Join(extraKeys, ",")))
								} else {
									msgTypes = append(msgTypes, fmt.Sprintf("msg[%d]:%s", i, t))
								}
							}
						}
					} else if content, ok := m["content"].(string); ok {
						msgTypes = append(msgTypes, fmt.Sprintf("msg[%d]:string(len=%d)", i, len(content)))
					}
				}
			}
		}
		var sysTypes []string
		if sys, ok := payload["system"].([]any); ok {
			for i, block := range sys {
				if cb, ok := block.(map[string]any); ok {
					t, _ := cb["type"].(string)
					var extraKeys []string
					for k := range cb {
						if k != "type" && k != "text" {
							extraKeys = append(extraKeys, k+":"+fmt.Sprintf("%v", cb[k]))
						}
					}
					if len(extraKeys) > 0 {
						sysTypes = append(sysTypes, fmt.Sprintf("sys[%d]:%s(%s)", i, t, strings.Join(extraKeys, ",")))
					} else {
						sysTypes = append(sysTypes, fmt.Sprintf("sys[%d]:%s", i, t))
					}
				}
			}
		}

		// All data captured - safe to log in background goroutine.
		go func() {
			slog.Info("debug strip result",
				"model", selectedModel,
				"top_keys", topKeys,
				"has_tools", hasTools,
				"has_tool_choice", hasToolChoice,
				"has_thinking", hasThinking,
				"has_output_config", hasOutputConfig,
				"has_stream_options", hasStreamOpts,
				"has_metadata", hasMetadata,
				"has_service_tier", hasServiceTier,
				"has_context_mgmt", hasContextMgmt,
				"has_effort", hasEffort,
				"has_budget_tokens", hasBudgetTokens,
			)
			slog.Info("debug content analysis", "msg_blocks", msgTypes, "sys_blocks", sysTypes)
		}()
	}

	// --- Image detection runs first; optimizer and privacy skip image requests ---

	// Detect if request contains images for native vision routing
	hasImages = proxy.HasImageContent(payload)
	if hasImages {
		slog.Info("image request detected",
			"model", selectedModel,
			"body_len", len(body),
			"transparent", transparent,
			"provider", func() string {
				if decision != nil {
					return decision.ProviderID
				}
				return "none"
			}(),
		)

		// Compress large base64 images (>500KB base64 data) before dispatch.
		// Only compresses images exceeding 1024px on any dimension.
		if decision != nil && decision.ProviderID == "zai" {
			const (
				imgCompressThreshold = 1 // compress all images
				imgMaxDimension      = 1600
				imgJPEGQuality       = 75
			)
			if n, saved, orig := compressLargeImages(payload, imgCompressThreshold, imgMaxDimension, imgJPEGQuality); n > 0 {
				slog.Info("large images compressed", "count", n, "bytes_saved", saved, "model", selectedModel)
				h.metrics.RecordImageCompression(selectedModel, orig, saved, n)
				// Re-marshal body only when compression modified the payload
				var marshalErr error
				body, marshalErr = json.Marshal(payload)
				if marshalErr != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode request"})
					return
				}
			}
		}
	}

	// Re-encode payload after modifications.
	// Skip for image requests unless compression re-marshaled above.
	if !hasImages {
		body, err = json.Marshal(payload)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode request"})
			return
		}
	}

	// Early exit if client disconnected before optimizer.
	if err := r.Context().Err(); err != nil {
		slog.Info("client disconnected before optimizer", "error", err)
		return
	}

	// Optimizer + privacy masking for all modes (including transparent claude-oauth).
	// Skip only for image requests (base64/URLs get corrupted).
	if !hasImages && !transparent {
		// Extract per-profile optimizer overrides
		var optOverrides map[string]bool
		if profileOverride != nil {
			optOverrides = profileOverride.OptimizerOverrides
		}

		// Run token optimization pipeline on system prompt
		if h.optimizers != nil {
			budgetLevel := 0
			if sys, ok := payload["system"]; ok {
				var sysText string
				switch v := sys.(type) {
				case string:
					sysText = v
				case []any:
					parts := make([]string, 0, len(v))
					for _, item := range v {
						if m, ok := item.(map[string]any); ok {
							if t, ok := m["text"].(string); ok {
								parts = append(parts, t)
							}
						}
					}
					sysText = strings.Join(parts, "\n\n")
				}
				if sysText != "" {
					cap := tokenizer.GetModelCapabilities(selectedModel)
					sysTokens := tokenizer.QuickEstimateTokens(sysText)
					msgTokens := 0
					if msgs, ok := payload["messages"].([]any); ok {
						for _, msg := range msgs {
							msgTokens += proxy.EstimateMessageTokens(msg)
						}
					}
					totalTokens := sysTokens + msgTokens
					pctUsed := float64(totalTokens) / float64(cap.ContextWindow)
					if h.cfg.DebugMode {
						slog.Info("debug tokens before optimize", "model", selectedModel, "sys_tokens", sysTokens, "msg_tokens", msgTokens, "total_tokens", totalTokens, "context_limit", cap.ContextWindow, "pct_used", fmt.Sprintf("%.1f%%", pctUsed*100), "budget_level", budgetLevel)
					}
					if pctUsed >= 0.8 {
						budgetLevel = 2
						h.metrics.SetBudgetLevel(selectedModel, 2)
					} else if pctUsed >= 0.6 {
						budgetLevel = 1
						h.metrics.SetBudgetLevel(selectedModel, 1)
					} else {
						h.metrics.SetBudgetLevel(selectedModel, 0)
					}
					optimized := h.optimizers.OptimizeSystemPrompt(sysText, h.metrics, budgetLevel, selectedModel, transparent, optOverrides)
					if optimized != sysText {
						payload["system"] = optimized
						if saved := len(sysText) - len(optimized); saved > 0 {
							h.metrics.RecordProfileOptimization(profileName, "system_prompt", saved)
						}
					}
				}
			}
		}

		// Run token optimization on message content
		if h.optimizers != nil {
			if msgs, ok := payload["messages"].([]any); ok && len(msgs) > 0 {
				h.optimizers.OptimizeMessages(msgs, h.metrics, optOverrides)
			}
		}

		// Tool manifest filtering - reduce tool count when > MaxTools
		if h.optimizers != nil && optimizerAllowed(optOverrides, "toolfilter", h.optimizers.ToolFilter) {
			if tools, ok := payload["tools"].([]any); ok && len(tools) > 0 {
				parsedTools := make([]toolfilter.Tool, 0, len(tools))
				for _, t := range tools {
					tm, ok := t.(map[string]any)
					if !ok {
						continue
					}
					name, _ := tm["name"].(string)
					desc, _ := tm["description"].(string)
					parsedTools = append(parsedTools, toolfilter.Tool{Name: name, Description: desc})
				}
				// Extract intent from recent messages
				recentText := ""
				if msgs, ok := payload["messages"].([]any); ok {
					for i := len(msgs) - 1; i >= 0 && len(recentText) < 500; i-- {
						if mm, ok := msgs[i].(map[string]any); ok {
							if c, ok := mm["content"].(string); ok {
								recentText = c + " " + recentText
							}
						}
					}
				}
				filtered := h.optimizers.ToolFilter.FilterTools(parsedTools, recentText)
				if len(filtered) < len(parsedTools) {
					// Rebuild tools array from filtered results
					filteredMap := make(map[string]bool, len(filtered))
					for _, ft := range filtered {
						filteredMap[ft.Name] = true
					}
					newTools := make([]any, 0, len(filtered))
					for _, t := range tools {
						if tm, ok := t.(map[string]any); ok {
							if name, ok := tm["name"].(string); ok {
								if filteredMap[name] {
									newTools = append(newTools, t)
								}
							}
						}
					}
					saved := (len(tools) - len(newTools)) * 80
					payload["tools"] = newTools
					h.metrics.RecordOptimization("toolfilter", saved, "input")
				}
			}
		}

		// Re-encode after optimization
		body, err = json.Marshal(payload)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode request"})
			return
		}

		// Privacy masking: detect and mask secrets/PII before proxying
		if h.privacy != nil {
			maskResult, _ = h.privacy.MaskRequest(body)
			if maskResult != nil {
				body = maskResult.MaskedBody
				// Inject privacy prompt so provider treats [[TYPE_N]] as real values.
				// Must unmarshal masked body back into payload to preserve masked values.
				if pp := maskResult.PrivacyPrompt(); pp != "" {
					var maskedPayload map[string]any
					if err := json.Unmarshal(body, &maskedPayload); err == nil {
						injectSystemPrompt(maskedPayload, pp)
						if updated, err := json.Marshal(maskedPayload); err == nil {
							body = updated
							payload = maskedPayload
						}
					}
				}
				slog.Info("privacy mask applied",
					"has_secrets", maskResult.HasSecrets,
					"has_pii", maskResult.HasPII,
					"secrets_count", len(maskResult.SecretsCtx.Mapping),
					"pii_count", len(maskResult.PIICtx.Mapping),
				)
				if h.cfg.DebugMode {
					var secretTypes []string
					for t := range maskResult.SecretsCtx.Mapping {
						secretTypes = append(secretTypes, t)
					}
					var piiTypes []string
					for t := range maskResult.PIICtx.Mapping {
						piiTypes = append(piiTypes, t)
					}
					slog.Info("debug privacy detail", "secret_types", secretTypes, "pii_types", piiTypes, "body_len_before", len(string(body)), "body_len_after", len(string(maskResult.MaskedBody)))
				}
			} else {
				slog.Info("privacy mask skipped", "reason", "no_pii_or_secrets")
			}
		}
	} else {
		slog.Info("optimizer and privacy skipped", "reason", "image_request")
	}

	isStream, _ := payload["stream"].(bool)

	// Early exit if client disconnected before upstream call.
	if err := r.Context().Err(); err != nil {
		slog.Info("client disconnected before upstream", "error", err)
		return
	}

	// Build profile proxy options if profile override is active.
	profileOpts := &proxy.ProxyOptions{}
	profileOpts.Transparent = transparent
	if profileOverride != nil {
		if profileOverride.BaseURL != "" {
			profileOpts.UpstreamOverride = profileOverride.BaseURL
		}
		// When profile has a target provider, override model-based routing.
		// Use effectiveProviderID which accounts for fallback targets.
		targetPID := effectiveProviderID
		if targetPID == "" && profileOverride != nil {
			targetPID = profileOverride.Target
			if profileOverride.Provider != "" {
				targetPID = profileOverride.Provider
			}
		}
		if targetPID != "" {
			if d, ok := h.resolver.ResolveByProvider(targetPID); ok {
				decision = d
			}
		}
		// Passthrough auth: force bearer mode so client's token is sent as Authorization.
		if profileOverride.PassthroughAuth {
			profileOpts.AuthMode = "bearer"
			// Get ExtraHeaders from provider route table for anthropic-beta etc.
			if effectiveProviderID != "" {
				if d, ok := h.resolver.ResolveByProvider(effectiveProviderID); ok && d != nil {
					profileOpts.ExtraHeaders = d.ExtraHeaders
					if profileOpts.UpstreamOverride == "" {
						profileOpts.UpstreamOverride = d.UpstreamURL
					}
				}
			}
		}
	}

	// OAuth token refresh callback: on 401, refresh the token and retry once.
	// Skip for passthrough auth - client manages their own token lifecycle.
	oauthRefreshFn := func(oldKey string) (string, bool) {
		if profileOverride != nil && profileOverride.PassthroughAuth {
			return "", false
		}
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
		// Transparent mode fallback: client token not in store, try a gateway-stored token.
		for _, t := range tokens {
			if t.AccessToken != oldKey && !t.Paused && t.RefreshToken != "" {
				if h.refreshWorker.RefreshOne(pid, t.AccountID) == nil {
					if refreshed, err := h.tokenStore.Get(pid, t.AccountID); err == nil && refreshed != nil {
						slog.Info("transparent fallback: used gateway-stored token", "provider", pid, "account", t.AccountID)
						return refreshed.AccessToken, true
					}
				}
			}
		}
		return "", false
	}
	profileOpts.OnAuthError = oauthRefreshFn

	var rotateTriedKeys map[string]bool
	rotateAccountFn := func(oldKey string) (proxy.FallbackResult, bool) {
		if rotateTriedKeys == nil {
			rotateTriedKeys = make(map[string]bool)
		}
		rotateTriedKeys[oldKey] = true

		pid := effectiveProviderID
		if pid == "" {
			if profileOverride != nil {
				pid = profileOverride.Provider
				if pid == "" {
					pid = profileOverride.Target
				}
			} else if decision != nil {
				pid = decision.ProviderID
			}
		}
		if pid == "" || h.tokenStore == nil {
			return proxy.FallbackResult{}, false
		}

		// Skip account rotation for passthrough auth (client manages token)
		// but still allow profile target fallback below
		if profileOverride == nil || !profileOverride.PassthroughAuth {
			accountIDs := effectiveAccountIDs
			if len(accountIDs) == 0 && profileOverride != nil {
				accountIDs = profileOverride.AccountIDs
			}
			for _, aid := range accountIDs {
				tok, err := h.tokenStore.Get(pid, aid)
				if err != nil || tok == nil || tok.Paused {
					continue
			}
				if rotateTriedKeys[tok.AccessToken] {
					continue
			}
				if h.resolver != nil && h.resolver.IsAccountCoolingDown(pid, aid) {
					continue
			}
				rotateTriedKeys[tok.AccessToken] = true
				selectedTokenInfo = tok
				slog.Info("429: rotated profile account", "provider", pid, "account", tok.AccountID, "tried", len(rotateTriedKeys))
				return proxy.FallbackResult{APIKey: tok.AccessToken}, true
			}
			if len(accountIDs) == 0 {
				tokens, err := h.tokenStore.ListByProvider(pid)
				if err == nil && len(tokens) > 1 {
					for _, t := range tokens {
						if !t.Paused && !rotateTriedKeys[t.AccessToken] {
							rotateTriedKeys[t.AccessToken] = true
							slog.Info("429: rotated account", "provider", pid, "account", t.AccountID)
							return proxy.FallbackResult{APIKey: t.AccessToken}, true
						}
					}
				}
			}
		}

		// All accounts exhausted: try next target in profile's multi-target list
		if profileOverride != nil && len(profileOverride.Targets) > 1 {
			currentIdx := -1
			for i, t := range profileOverride.Targets {
				if t.Target == pid {
					currentIdx = i
					break
				}
			}
			for offset := 1; offset < len(profileOverride.Targets); offset++ {
				idx := (currentIdx + offset) % len(profileOverride.Targets)
				t := profileOverride.Targets[idx]
				targetPID := t.Target
				targetAccounts := t.AccountIDs
				if len(targetAccounts) == 0 {
					continue
				}
				if d, ok := h.resolver.ResolveByProvider(targetPID); ok && d != nil {
					var newKey string
					if h.tokenStore != nil {
						if tok, err := h.tokenStore.GetFromPool(targetPID, targetAccounts); err == nil && tok != nil {
							newKey = tok.AccessToken
							selectedTokenInfo = tok
						}
					}
					if newKey == "" && d.APIKey != "" {
						newKey = d.APIKey
					}
					if newKey == "" {
						continue
					}
					slog.Info("429: profile target fallback", "from", pid, "to", targetPID, "profile", profileOverride.Name)
					return proxy.FallbackResult{APIKey: newKey, UpstreamURL: d.UpstreamURL, AuthMode: d.AuthMode}, true
				}
			}
		}

		// Last resort: model rules fallback
		if fb := h.tryProviderFallback(requestedModel, pid); fb != nil {
			slog.Info("429: fallback to alternate provider", "from", pid, "to", fb.ProviderID)
			return proxy.FallbackResult{APIKey: fb.APIKey, UpstreamURL: fb.UpstreamURL, AuthMode: string(fb.AuthMode)}, true
		}
		return proxy.FallbackResult{}, false
	}

	// 403 fallback: try next target in profile's multi-target list.
	forbiddenFn := func(oldKey string) (proxy.FallbackResult, bool) {
		if profileOverride == nil || len(profileOverride.Targets) < 2 {
			return proxy.FallbackResult{}, false
		}
		currentProvider := effectiveProviderID
		currentIdx := -1
		for i, t := range profileOverride.Targets {
			if t.Target == currentProvider {
				currentIdx = i
				break
			}
		}
		for offset := 1; offset < len(profileOverride.Targets); offset++ {
			idx := (currentIdx + offset) % len(profileOverride.Targets)
			t := profileOverride.Targets[idx]
			pid := t.Target
			accounts := t.AccountIDs
			if len(accounts) == 0 {
				continue
			}
			if d, ok := h.resolver.ResolveByProvider(pid); ok && d != nil {
				var newKey string
				if h.tokenStore != nil {
					if tok, err := h.tokenStore.GetFromPool(pid, accounts); err == nil && tok != nil {
						newKey = tok.AccessToken
					}
				}
				if newKey == "" && d.APIKey != "" {
					newKey = d.APIKey
				}
				if newKey == "" {
					continue
				}
				slog.Warn("403 fallback to alternate target", "from", currentProvider, "to", pid, "profile", profileOverride.Name)
				return proxy.FallbackResult{
					APIKey:      newKey,
					UpstreamURL: d.UpstreamURL,
					AuthMode:    d.AuthMode,
				}, true
			}
		}
		return proxy.FallbackResult{}, false
	}
	profileOpts.OnForbidden = forbiddenFn

	// Profile-selected OAuth token: enable transparent mode for sidecar routing.
	if !transparent && strings.HasPrefix(apiKey, "sk-ant-oat01-") {
		transparent = true
		slog.Info("profile OAuth token, enabling transparent sidecar routing", "profile", func() string {
			if profileOverride != nil {
				return profileOverride.Name
			}
			return ""
		}())
	}

	// Store account ID in context for usage tracking.
	if selectedTokenInfo != nil {
		*r = *r.WithContext(context.WithValue(r.Context(), accountCtxKey{}, selectedTokenInfo.AccountID))
	}

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
			if decision != nil && h.resolver != nil {
				if decision.AccountID != "" {
					h.resolver.MarkAccountCooldown(decision.ProviderID, decision.AccountID, 5*time.Minute)
					slog.Info("account cooldown activated", "provider", decision.ProviderID, "account", decision.AccountID, "model", selectedModel, "duration", "5m")
				} else {
					h.resolver.MarkCooldown(decision.ProviderID, 2*time.Minute, selectedModel)
					slog.Info("provider cooldown activated", "provider", decision.ProviderID, "model", selectedModel, "duration", "2m")
				}
			}
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
		// On 2xx: store fresh rate limit data. On 429: expire stale cache to prevent stuck 100%.
		{
			hasRL := false
			for k := range headers {
				if strings.HasPrefix(strings.ToLower(k), "anthropic-ratelimit") {
					hasRL = true
					break
				}
			}
			prov := ""
			accID := ""
			if selectedTokenInfo != nil {
				prov = selectedTokenInfo.Provider
				accID = selectedTokenInfo.AccountID
			} else if decision != nil {
				prov = decision.ProviderID
			}
			if prov != "" && accID != "" {
				if hasRL && statusCode >= 200 && statusCode < 300 {
					go h.storeRateLimitStatus(prov, accID, headers)
				}
				if statusCode >= 200 && statusCode < 300 {
					go h.storeRequestUsage(prov, accID)
				} else if statusCode == 429 {
					go h.expireStaleRateLimit(prov, accID)
				}
			}
		}
	}

	// Vision pre-analysis: call Zhipu API directly with MCP-style params,
	// replace images with text descriptions, then route text-only to main model.
	// Must run before the routing if/else chain so hasImages=false skips vision blocks.
	if hasImages && decision != nil && decision.ProviderID == "zai" &&
		h.cfg.VisionPreAnalysisEnabled {
		imgBytes, imgCount := analyzeImagePayload(payload)
		newBody, mainModel, analyzed := h.preAnalyzeImages(r, payload, body, apiKey, requestedModel)
		if analyzed {
			body = newBody
			selectedModel = mainModel
			hasImages = false // payload is now text-only; routing chain will use text path
			slog.Info("vision pre-analysis: routing text-only to main model",
				"main_model", mainModel,
				"image_count", imgCount,
				"image_bytes", imgBytes,
			)
		} else {
			slog.Warn("vision pre-analysis failed, falling back to direct proxy",
				"model", selectedModel, "image_count", imgCount)
		}
	}

	if hasImages && decision != nil && decision.ProviderID == "zai" {
		h.metrics.RecordBillingPath("zai_vision", selectedModel, profileName)
		imgBytes, imgCount := analyzeImagePayload(payload)
		if !h.cfg.ZAIOpenAIModels[selectedModel] {
			visionModel := selectVisionModel(imgBytes, imgCount)
			if visionModel != selectedModel {
				slog.Info("vision model auto-selected",
					"original", selectedModel,
					"selected", visionModel,
					"imageBytes", imgBytes,
					"imageCount", imgCount,
				)
				selectedModel = visionModel
				var bodyMap map[string]any
				if json.Unmarshal(body, &bodyMap) == nil {
					bodyMap["model"] = selectedModel
					if bodyMap["stream"] == nil || bodyMap["stream"] == false {
						bodyMap["stream"] = true
					}
					// Clamp max_tokens to vision model's limit.
					if mt, ok := bodyMap["max_tokens"].(float64); ok {
						modelMaxTokensMu.RLock()
						if limit, found := modelMaxTokens[selectedModel]; found && mt > float64(limit) {
							slog.Info("vision max_tokens clamped", "original", int(mt), "clamped", limit, "model", selectedModel)
							bodyMap["max_tokens"] = limit
						}
						modelMaxTokensMu.RUnlock()
					}
					body, _ = json.Marshal(bodyMap)
				}
			}
		}
		// Vision via Z.AI proxy.
		if isNativeImageModel(selectedModel) {
			slog.Info("vision via zai proxy", "model", selectedModel, "apiKey_len", len(apiKey), "body_len", len(body))
			if err := h.proxy.ProxyTransparent(w, r, apiKey, body, selectedModel, true, feedbackFn, maskResult, &proxy.ProxyOptions{AuthMode: "bearer"}); err != nil {
				slog.Error("zai anthropic vision proxy error", "error", err, "model", selectedModel)
				h.metrics.IncError("upstream")
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "zai anthropic vision proxy error: " + err.Error()})
			}
		} else {
			slog.Info("vision via zai openai endpoint", "model", selectedModel, "apiKey_len", len(apiKey), "body_len", len(body))
			if err := h.openaiProxy.ProxyOpenAI(w, r, h.cfg.ZAIOpenAIURL, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, 0, ""); err != nil {
				slog.Error("zai openai vision proxy error", "error", err, "model", selectedModel)
				h.metrics.IncError("upstream")
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "zai openai vision proxy error: " + err.Error()})
			}
		}
		return
	} else if hasImages && decision != nil {
		h.metrics.RecordBillingPath("openai_vision", selectedModel, profileName)
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
			if err := h.openaiProxy.ProxyOpenAI(w, r, visionDecision.UpstreamURL, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, visionDecision.MaxContinuations, visionDecision.ToolMode); err != nil {
				slog.Error("openai vision proxy error", "error", err, "model", selectedModel)
				h.metrics.IncError("upstream")
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "openai vision proxy error: " + err.Error()})
			}
		case provider.FormatGemini:
			if visionDecision.ProviderID == "gemini-oauth" && h.codeAssistProxy != nil {
				if h.resolver != nil && h.resolver.IsCoolingDown("gemini-oauth", selectedModel) {
					if fb := h.tryProviderFallback(selectedModel, "gemini-oauth"); fb != nil {
						slog.Info("gemini-oauth vision cooling down, skipping to fallback", "model", selectedModel, "fallback", fb.ProviderID)
						h.handleFallbackProxy(w, r, fb, body, selectedModel, isStream, feedbackFn, maskResult, oauthRefreshFn, codeAssistProjectID)
						break
					}
				}
				err := h.codeAssistProxy.ProxyCodeAssist(w, r, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, oauthRefreshFn, codeAssistProjectID)
				if err != nil && strings.Contains(err.Error(), "429") {
					if fb := h.tryProviderFallback(selectedModel, "gemini-oauth"); fb != nil {
						slog.Info("code assist vision 429, falling back provider", "from", "gemini-oauth", "to", fb.ProviderID, "model", selectedModel)
						h.handleFallbackProxy(w, r, fb, body, selectedModel, isStream, feedbackFn, maskResult, oauthRefreshFn, codeAssistProjectID)
						break
					}
				}
				if err != nil {
					slog.Error("code assist vision failed", "error", err, "model", selectedModel)
					h.metrics.IncError("upstream")
					writeJSON(w, http.StatusBadGateway, map[string]string{"error": "code assist vision error: " + err.Error()})
				}
			} else if h.geminiAPIProxy != nil {
				if err := h.geminiAPIProxy.ProxyGemini(w, r, visionDecision.UpstreamURL, apiKey, body, selectedModel, isStream, feedbackFn, maskResult); err != nil {
					slog.Error("gemini vision proxy error", "error", err, "model", selectedModel)
					h.metrics.IncError("upstream")
					writeJSON(w, http.StatusBadGateway, map[string]string{"error": "gemini vision proxy error: " + err.Error()})
				}
			}
		default:
			opts := &proxy.ProxyOptions{
				AuthMode:         visionDecision.AuthMode,
				UpstreamOverride: visionDecision.UpstreamURL,
				ExtraHeaders:     visionDecision.ExtraHeaders,
				OnAuthError:      oauthRefreshFn,
				OnRateLimitError: rotateAccountFn,
				Transparent:      transparent,
			}
			if err := h.trySidecarOrDirect(w, r, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, opts, transparent); err != nil {
				slog.Error("vision proxy error", "error", err, "model", selectedModel)
				h.metrics.IncError("upstream")
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "vision proxy error: " + err.Error()})
			}
		}
	} else if h.cfg.GLMMode && h.cfg.ZAIOpenAIModels[selectedModel] {
		h.metrics.RecordBillingPath("zai_proxy", selectedModel, profileName)
		// GLM model that requires OpenAI-compatible endpoint.
		upstreamURL := h.cfg.ZAIOpenAIURL
		slog.Info("glm via zai openai endpoint", "model", selectedModel, "upstream", upstreamURL)
		if err := h.openaiProxy.ProxyOpenAI(w, r, upstreamURL, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, 0, ""); err != nil {
			slog.Error("zai openai proxy error", "error", err, "model", selectedModel)
			h.metrics.IncError("upstream")
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "zai openai proxy error: " + err.Error()})
		}
	} else if decision != nil {
		routePath := "anthropic_proxy"
		switch decision.Format {
		case provider.FormatOpenAI:
			routePath = "openai_proxy"
			if err := h.openaiProxy.ProxyOpenAI(w, r, decision.UpstreamURL, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, decision.MaxContinuations, decision.ToolMode); err != nil {
				slog.Error("openai proxy error", "error", err, "model", selectedModel)
				h.metrics.IncError("upstream")
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "openai proxy error: " + err.Error()})
			}
		case provider.FormatGemini:
			routePath = "gemini_proxy"
			if decision.ProviderID == "gemini-oauth" && h.codeAssistProxy != nil {
				routePath = "codeassist_proxy"
				// Check cooldown: if gemini-oauth is cooling down from 429, skip to fallback provider.
				if h.resolver != nil && h.resolver.IsCoolingDown("gemini-oauth", selectedModel) {
					if fbModel, ok := h.tryModelFallback("gemini-oauth", selectedModel); ok {
						slog.Info("gemini-oauth cooling down, falling back model", "from", selectedModel, "to", fbModel)
						selectedModel = fbModel
					} else if fb := h.tryProviderFallback(selectedModel, "gemini-oauth"); fb != nil {
						slog.Info("gemini-oauth cooling down, skipping to fallback", "model", selectedModel, "fallback", fb.ProviderID)
						h.handleFallbackProxy(w, r, fb, body, selectedModel, isStream, feedbackFn, maskResult, oauthRefreshFn, codeAssistProjectID)
						break
					}
				}
				err := h.codeAssistProxy.ProxyCodeAssist(w, r, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, oauthRefreshFn, codeAssistProjectID)
				if err != nil && strings.Contains(err.Error(), "429") {
					if fb := h.tryProviderFallback(selectedModel, "gemini-oauth"); fb != nil {
						slog.Info("code assist 429, falling back provider", "from", "gemini-oauth", "to", fb.ProviderID, "model", selectedModel)
						h.handleFallbackProxy(w, r, fb, body, selectedModel, isStream, feedbackFn, maskResult, oauthRefreshFn, codeAssistProjectID)
						break
					}
				}
				if err != nil {
					slog.Error("code assist failed", "error", err, "model", selectedModel)
					h.metrics.IncError("upstream")
					writeJSON(w, http.StatusBadGateway, map[string]string{"error": "code assist error: " + err.Error()})
				}
			} else if h.geminiAPIProxy != nil {
				if err := h.geminiAPIProxy.ProxyGemini(w, r, decision.UpstreamURL, apiKey, body, selectedModel, isStream, feedbackFn, maskResult); err != nil {
					slog.Error("gemini proxy error", "error", err, "model", selectedModel)
					h.metrics.IncError("upstream")
					writeJSON(w, http.StatusBadGateway, map[string]string{"error": "gemini proxy error: " + err.Error()})
				}
			}
		default:
			opts := &proxy.ProxyOptions{
				AuthMode:         decision.AuthMode,
				UpstreamOverride: decision.UpstreamURL,
				ExtraHeaders:     decision.ExtraHeaders,
				OnAuthError:      oauthRefreshFn,
				OnRateLimitError: rotateAccountFn,
				Transparent:      transparent,
			}
			if err := h.trySidecarOrDirect(w, r, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, opts, transparent); err != nil {
				slog.Error("proxy error", "error", err, "model", selectedModel)
				h.metrics.IncError("upstream")
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "proxy error: " + err.Error()})
			}
		}
		h.metrics.RecordBillingPath(routePath, selectedModel, profileName)
	} else if decision == nil && selectedModel != "" {
		slog.Error("no route for model, rejecting", "model", selectedModel)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no upstream provider for model: " + selectedModel})
	} else if err := h.trySidecarOrDirect(w, r, apiKey, body, selectedModel, isStream, feedbackFn, maskResult, profileOpts, transparent); err != nil {
		slog.Error("proxy error", "error", err, "model", selectedModel)
		h.metrics.IncError("upstream")
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "proxy error: " + err.Error()})
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
		req.MaxTokens = 128000
	}
	if req.Temperature <= 0 {
		req.Temperature = 0.7
	}
	if req.Model == "" {
		req.Model = "glm-5"
	}
	if req.Provider == "" {
		req.Provider = provider.ResolveProviderByModel(req.Model)
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
	ReqCount5h   int       `json:"req_count_5h,omitempty"`
	ReqCount7d   int       `json:"req_count_7d,omitempty"`
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

// expireStaleRateLimit deletes cached rate limit data when a 429 is received.
// This prevents the cache from being stuck at 100% utilization when no 2xx
// responses arrive to refresh it.
func (h *Handler) expireStaleRateLimit(provider, accountID string) {
	if h.tokenStore == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	h.tokenStore.Client().Del(ctx, rateLimitKeyPrefix+provider+":"+accountID)
	slog.Info("ratelimit cache expired on 429", "provider", provider, "account", accountID)
}

const reqUsageKeyPrefix = "arl:usage:"

// storeRequestUsage increments request counters for any provider.
func (h *Handler) storeRequestUsage(provider, accountID string) {
	if h.tokenStore == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	key := reqUsageKeyPrefix + provider + ":" + accountID
	// increment 5h window counter
	h.tokenStore.Client().Incr(ctx, key+":5h")
	h.tokenStore.Client().Expire(ctx, key+":5h", 5*time.Hour)
	// increment 7d window counter
	h.tokenStore.Client().Incr(ctx, key+":7d")
	h.tokenStore.Client().Expire(ctx, key+":7d", 7*24*time.Hour)
}

// GetRateLimits returns rate limit utilization for all accounts.
func (h *Handler) GetRateLimits(w http.ResponseWriter, r *http.Request) {
	if h.tokenStore == nil {
		writeJSON(w, http.StatusOK, []RateLimitStatus{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// 1. Load Anthropic rate limit data from cache
	rlMap := map[string]*RateLimitStatus{}
	iter := h.tokenStore.Client().Scan(ctx, 0, rateLimitKeyPrefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		data, err := h.tokenStore.Client().Get(ctx, iter.Val()).Bytes()
		if err != nil {
			continue
		}
		var s RateLimitStatus
		if json.Unmarshal(data, &s) == nil {
			rlMap[s.Provider+":"+s.AccountID] = &s
		}
	}

	// 2. Load all accounts and merge with request usage
	accounts, _ := h.tokenStore.ListAll()
	seen := map[string]bool{}
	for _, acct := range accounts {
		key := acct.Provider + ":" + acct.AccountID
		if seen[key] {
			continue
		}
		seen[key] = true

		if existing, ok := rlMap[key]; ok {
			// Already has Anthropic RL data, enrich with request count
			c5h, _ := h.tokenStore.Client().Get(ctx, reqUsageKeyPrefix+key+":5h").Int()
			c7d, _ := h.tokenStore.Client().Get(ctx, reqUsageKeyPrefix+key+":7d").Int()
			existing.ReqCount5h = c5h
			existing.ReqCount7d = c7d
			if existing.UpdatedAt.IsZero() {
				existing.UpdatedAt = time.Now().UTC()
			}
			continue
		}

		// No Anthropic RL data - create entry from request usage
		c5h, _ := h.tokenStore.Client().Get(ctx, reqUsageKeyPrefix+key+":5h").Int()
		c7d, _ := h.tokenStore.Client().Get(ctx, reqUsageKeyPrefix+key+":7d").Int()
		rlMap[key] = &RateLimitStatus{
			Provider:   acct.Provider,
			AccountID:  acct.AccountID,
			ReqCount5h: c5h,
			ReqCount7d: c7d,
			Status:     "active",
			UpdatedAt:  time.Now().UTC(),
		}
	}

	results := make([]RateLimitStatus, 0, len(rlMap))
	for _, s := range rlMap {
		results = append(results, *s)
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
	"glm-5.1":                   128000,
	"glm-5-turbo":               128000,
	"glm-5":                     128000,
	"glm-4.7":                   128000,
	"glm-4.7-flashx":            128000,
	"glm-4.6":                   128000,
	"glm-4.5":                   128000,
	"glm-4.5-x":                 128000,
	"glm-4.5-air":               128000,
	"glm-4.5-airx":              128000,
	"glm-4.6v":                  128000,
	"glm-4.5v":                  128000,
	"glm-z1-air":                128000,
	"glm-z1-airx":               128000,
	"glm-z1-flashx":             128000,
	"claude-opus-4-7":           200000,
	"claude-sonnet-4-6":         200000,
	"claude-haiku-4-5-20251001": 200000,
}

var modelMaxTokensMu sync.RWMutex

// GetModelMaxTokensDefaults returns a copy of the hardcoded defaults.
func GetModelMaxTokensDefaults() map[string]int {
	modelMaxTokensMu.RLock()
	defer modelMaxTokensMu.RUnlock()
	return copyIntMap(modelMaxTokens)
}

// ApplyMaxTokensOverrides merges overrides into the modelMaxTokens map.
func ApplyMaxTokensOverrides(overrides map[string]int) {
	modelMaxTokensMu.Lock()
	defer modelMaxTokensMu.Unlock()
	for k, v := range overrides {
		modelMaxTokens[k] = v
		tokenizer.UpdateMaxOutputTokens(k, v)
	}
}

const fallbackMaxTokens = 128000

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

// filterUnsupportedContent strips cache_control from all content blocks
// (Z.AI does not support Anthropic cache_control hints).
func filterUnsupportedContent(payload map[string]any) {
	strip := func(blocks []any) []any {
		for _, block := range blocks {
			cb, ok := block.(map[string]any)
			if !ok {
				continue
			}
			delete(cb, "cache_control")
		}
		return blocks
	}

	// Filter messages
	msgs, ok := payload["messages"].([]any)
	if ok {
		for _, msg := range msgs {
			m, ok := msg.(map[string]any)
			if !ok {
				continue
			}
			if content, ok := m["content"].([]any); ok {
				m["content"] = strip(content)
			}
		}
	}

	// Filter system blocks (Claude Code sends system as array with cache_control)
	if sys, ok := payload["system"].([]any); ok {
		payload["system"] = strip(sys)
	}
}

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

// compressLargeImages walks the payload and compresses base64 images that exceed
// compressThreshold bytes of raw base64 data. Uses bimg (libvips) for high-quality
// resize and WebP re-encoding. Compresses all images above threshold regardless
// of whether the result is smaller.
func compressLargeImages(payload map[string]any, compressThreshold int, maxDimension int, quality int) (compressed int, savedBytes int, originalBytes int) {
	const maxDimensionDefault = 1024
	if maxDimension <= 0 {
		maxDimension = maxDimensionDefault
	}
	if quality <= 0 || quality > 100 {
		quality = 85
	}

	msgs, ok := payload["messages"].([]any)
	if !ok {
		return 0, 0, 0
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
			if t != "image" {
				continue
			}
			src, ok := cb["source"].(map[string]any)
			if !ok {
				continue
			}
			data, _ := src["data"].(string)
			if data == "" || len(data) < compressThreshold {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(data)
			if err != nil {
				decoded, err = base64.RawStdEncoding.DecodeString(data)
				if err != nil {
					slog.Debug("image base64 decode failed, skip", "error", err)
					continue
				}
			}

			img := bimg.NewImage(decoded)
			meta, err := img.Metadata()
			if err != nil {
				slog.Debug("image metadata failed, skip", "error", err)
				continue
			}

			origW := meta.Size.Width
			origH := meta.Size.Height

			opts := bimg.Options{
				Quality: quality,
				Type:    bimg.JPEG,
			}
			if origW > maxDimension || origH > maxDimension {
				opts.Width = maxDimension
				opts.Height = int(float64(origH) * float64(maxDimension) / float64(origW))
				if opts.Height > maxDimension {
					opts.Width = int(float64(origW) * float64(maxDimension) / float64(origH))
					opts.Height = maxDimension
				}
			}

			newImage, err := img.Process(opts)
			if err != nil {
				slog.Debug("bimg process failed, skip", "error", err)
				continue
			}

			newData := base64.StdEncoding.EncodeToString(newImage)
			if len(newData) >= len(data) {
				slog.Debug("compression increased size, keeping original",
					"original_bytes", len(data),
					"compressed_bytes", len(newData),
					"original_size", fmt.Sprintf("%dx%d", origW, origH),
				)
				continue
			}
			src["data"] = newData
			src["media_type"] = "image/jpeg"
			compressed++
			savedBytes += len(data) - len(newData)
			originalBytes += len(data)
			newW, newH := origW, origH
			if opts.Width > 0 {
				newW, newH = opts.Width, opts.Height
			}
			slog.Info("image compressed",
				"original_bytes", len(data),
				"compressed_bytes", len(newData),
				"saved", len(data)-len(newData),
				"original_size", fmt.Sprintf("%dx%d", origW, origH),
				"new_size", fmt.Sprintf("%dx%d", newW, newH),
			)
		}
	}
	return compressed, savedBytes, originalBytes
}

// selectVisionModel chooses the best vision model based on total image payload
// size and count. Uses a score combining both factors:
//
//	score = totalBase64KB + (imageCount * 300)
//
// glm-4.6v is the default for vision payloads (best accuracy per POC testing).
func selectVisionModel(totalBytes int, imageCount int) string {
	return "glm-4.6v"
}

// isNativeImageModel returns true if the model natively supports image input
// and should not be overridden by vision auto-selection.

func isNativeImageModel(model string) bool {
	switch model {
	case "glm-5.1", "glm-4.6v", "glm-4.5v":
		return true
	}
	return false
}

// applySmartMaxTokens sets an optimal max_tokens if not already specified.
func applySmartMaxTokens(payload map[string]any, model string) {
	if _, exists := payload["max_tokens"]; exists {
		return // Respect client's explicit setting.
	}
	modelMaxTokensMu.RLock()
	limit, ok := modelMaxTokens[model]
	modelMaxTokensMu.RUnlock()
	if ok {
		payload["max_tokens"] = limit
	} else {
		payload["max_tokens"] = fallbackMaxTokens
	}
}

// anthropicModelMaxTokens are hard limits enforced by Anthropic's API.
var anthropicModelMaxTokens = map[string]int{
	"claude-haiku-4-5-20251001": 64000,
	"claude-opus-4-7":           200000,
	"claude-sonnet-4-20250514":  200000,
	"claude-sonnet-4-6":         200000,
}

// clampMaxTokens ensures max_tokens does not exceed the upstream model's limit.
func clampMaxTokens(payload map[string]any, model string) {
	mt, ok := payload["max_tokens"].(float64)
	if !ok {
		return
	}
	if limit, found := anthropicModelMaxTokens[model]; found && mt > float64(limit) {
		payload["max_tokens"] = limit
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
	{"glm-5.1", "zai", "5", "anthropic", 1.4, 4.4, 128000, "budget", false, true, false},
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
	{"glm-4.6v", "zai", "4-vision", "anthropic", 0.3, 0.9, 128000, "none", false, true, false},
	{"glm-4.5v", "zai", "4-vision", "anthropic", 0.6, 1.8, 128000, "none", false, true, false},
	{"glm-z1-air", "zai", "z1", "anthropic", 0.2, 1.1, 128000, "none", false, false, false},
	{"glm-z1-airx", "zai", "z1", "anthropic", 1.1, 4.5, 128000, "none", false, false, false},
	{"glm-z1-flashx", "zai", "z1", "anthropic", 0.07, 0.4, 128000, "none", false, false, false},
	// Anthropic
	{"claude-opus-4-7", "anthropic", "opus", "anthropic", 15, 75, 200000, "budget", false, false, false},
	{"claude-sonnet-4-6", "anthropic", "sonnet", "anthropic", 3, 15, 200000, "budget", false, false, false},
	{"claude-haiku-4-5", "anthropic", "haiku", "anthropic", 0.80, 4, 200000, "none", false, true, false},
	{"claude-3-5-sonnet-20241022", "anthropic", "sonnet-3.5", "anthropic", 3, 15, 200000, "none", false, false, false},
	{"claude-3-5-haiku-20241022", "anthropic", "haiku-3.5", "anthropic", 0.80, 4, 200000, "none", false, false, false},
	// Claude OAuth
	{"claude-opus-4-7", "claude", "opus", "anthropic", 15, 75, 200000, "budget", false, false, false},
	{"claude-sonnet-4-6", "claude", "sonnet", "anthropic", 3, 15, 200000, "budget", false, false, false},
	{"claude-sonnet-4-6", "claude-oauth", "sonnet", "anthropic", 3, 15, 200000, "budget", false, false, false},
	{"claude-sonnet-4-20250514", "claude-oauth", "sonnet", "anthropic", 3, 15, 200000, "budget", false, false, false},
	{"claude-haiku-4-5-20251001", "claude-oauth", "haiku", "anthropic", 0.80, 4, 200000, "none", false, true, false},
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
	// Kimi (Moonshot AI) - pricing from https://pricepertoken.com/pricing-page/provider/moonshotai
	{"kimi-k2.6", "kimi", "k2.6", "anthropic", 0.745, 3.50, 262144, "none", false, true, false},
	{"kimi-k2.5", "kimi", "k2.5", "anthropic", 0.44, 2.00, 262144, "none", false, true, false},
	{"kimi-k2.5-thinking", "kimi", "k2.5", "anthropic", 0.44, 2.00, 262144, "budget", false, true, false},
	{"kimi-k2.5-turbo", "kimi", "k2.5", "anthropic", 0.60, 3.00, 262144, "none", false, false, false},
	{"kimi-k2-0905", "kimi", "k2", "anthropic", 0.40, 2.00, 262144, "none", false, false, false},
	{"kimi-k2-thinking", "kimi", "k2", "anthropic", 0.60, 2.50, 131072, "budget", false, false, false},
	{"kimi-k2-0711", "kimi", "k2", "anthropic", 0.55, 2.20, 131072, "none", false, false, false},
	{"kimi-dev-72b", "kimi", "dev", "anthropic", 0, 0, 131072, "none", false, false, false},
}

// providerDefaultModels maps each provider to its default model.
var providerDefaultModels = map[string]string{
	"claude-oauth": "claude-haiku-4-5-20251001",
	"claude":       "claude-haiku-4-5-20251001",
	"anthropic":    "claude-sonnet-4-20250514",
	"gemini-oauth": "gemini-2.5-flash",
	"gemini":       "gemini-2.5-flash",
	"openai":       "gpt-4o",
	"zai":          "glm-4.5",
	"deepseek":     "deepseek-chat",
	"copilot":      "gpt-4o",
	"openrouter":   "or-openai/gpt-4o",
	"qwen":         "qwen-plus",
	"kimi":         "kimi-k2.6",
}

// modelFallbacks maps a model to lighter alternatives within the same provider.
// When a model gets 429 rate-limited, the gateway tries these fallbacks in order.
var modelFallbacks = map[string][]string{
	// Gemini
	"gemini-2.5-pro":        {"gemini-2.5-flash", "gemini-2.5-flash-lite", "gemini-2.0-flash"},
	"gemini-2.5-flash":      {"gemini-2.5-flash-lite", "gemini-2.0-flash"},
	"gemini-2.5-flash-lite": {"gemini-2.0-flash"},
	// Claude
	"claude-opus-4-7":          {"claude-opus-4-20250115", "claude-sonnet-4-6", "claude-sonnet-4-20250514", "claude-haiku-4-5-20251001"},
	"claude-opus-4-20250115":   {"claude-sonnet-4-6", "claude-sonnet-4-20250514", "claude-haiku-4-5-20251001"},
	"claude-sonnet-4-6":        {"claude-sonnet-4-20250514", "claude-haiku-4-5-20251001"},
	"claude-sonnet-4-20250514": {"claude-haiku-4-5-20251001"},
	// OpenAI / GPT
	"gpt-4.1":      {"gpt-4.1-mini", "gpt-4.1-nano"},
	"gpt-4.1-mini": {"gpt-4.1-nano"},
	"gpt-4o":       {"gpt-4o-mini"},
	"o3":           {"o4-mini"},
	"o3-pro":       {"o3", "o4-mini"},
	"o4-mini":      {"gpt-4o-mini"},
	// Z.AI (GLM)
	"glm-5.1":     {"glm-5", "glm-4.7", "glm-4.6", "glm-4.5"},
	"glm-5-turbo": {"glm-5", "glm-4.7", "glm-4.6", "glm-4.5"},
	"glm-5":       {"glm-4.7", "glm-4.6", "glm-4.5"},
	"glm-4.7":     {"glm-4.6", "glm-4.5"},
	"glm-4.6":     {"glm-4.5"},
	// Qwen
	"qwen-plus": {"qwen-turbo"},
	"qwen-max":  {"qwen-plus", "qwen-turbo"},
	// Kimi
	"kimi-k2.6":          {"kimi-k2.5", "kimi-k2-0905"},
	"kimi-k2.5":          {"kimi-k2-0905", "kimi-k2-0711"},
	"kimi-k2.5-thinking": {"kimi-k2.5", "kimi-k2-0905"},
	"kimi-k2.5-turbo":    {"kimi-k2.5", "kimi-k2-0905"},
	"kimi-k2-thinking":   {"kimi-k2-0905", "kimi-k2-0711"},
	"kimi-k2-0905":       {"kimi-k2-0711"},
}

// mapModelForTarget returns the default model for a target provider when the
// requested model does not belong to that provider.
func mapModelForTarget(model, targetProvider string) string {
	if d, ok := providerDefaultModels[targetProvider]; ok {
		return d
	}
	return model
}

func (h *Handler) CountTokens(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, h.cfg.MaxRequestBody))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, proxy.ErrorResponse{
			Type:  "error",
			Error: proxy.ErrorDetail{Type: "invalid_request_error", Message: "failed to read request body"},
		})
		return
	}

	apiKey, ok := h.keyPool.Acquire()
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, proxy.ErrorResponse{
			Type:  "error",
			Error: proxy.ErrorDetail{Type: "authentication_error", Message: "no upstream API keys available"},
		})
		return
	}

	upstreamURL := h.cfg.UpstreamURL + "/v1/messages/count_tokens"
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, proxy.ErrorResponse{
			Type:  "error",
			Error: proxy.ErrorDetail{Type: "api_error", Message: "failed to create upstream request"},
		})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if beta := r.Header.Get("anthropic-beta"); beta != "" {
		httpReq.Header.Set("anthropic-beta", beta)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, proxy.ErrorResponse{
			Type:  "error",
			Error: proxy.ErrorDetail{Type: "api_error", Message: "upstream request failed"},
		})
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
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

// claudeModelsResponse is the Anthropic-native format returned by GET /v1/models
// when the client User-Agent indicates Claude Code CLI.
type claudeModelsResponse struct {
	Data    []claudeModelEntry `json:"data"`
	HasMore bool               `json:"has_more"`
	FirstID string             `json:"first_id"`
	LastID  string             `json:"last_id"`
}

type claudeModelEntry struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Created   int64  `json:"created"`
	OwnedBy   string `json:"owned_by"`
	MaxTokens int    `json:"max_tokens"`
}

func isClaudeCLI(ua string) bool {
	return strings.HasPrefix(ua, "claude-cli") || strings.HasPrefix(ua, "Claude-Code") || strings.HasPrefix(ua, "anthropic-cli")
}

func (h *Handler) GetModelsAnthropic(w http.ResponseWriter, r *http.Request) {
	type modelEntry struct {
		Name             string
		Provider         string
		Limit            int
		InputPerMillion  float64
		OutputPerMillion float64
		ContextWindow    int
		ThinkingSupport  string
		Deprecated       bool
	}

	entries := make([]modelEntry, 0, len(knownModels))
	for _, km := range knownModels {
		if !h.cfg.GLMMode && km.Provider == "zai" {
			continue
		}
		if km.Deprecated {
			continue
		}
		limit := h.cfg.DefaultLimit
		if l, ok := h.cfg.ModelLimits[km.Name]; ok {
			limit = l
		}
		entries = append(entries, modelEntry{
			Name:             km.Name,
			Provider:         km.Provider,
			Limit:            limit,
			InputPerMillion:  km.InputPerMillion,
			OutputPerMillion: km.OutputPerMillion,
			ContextWindow:    km.ContextWindow,
			ThinkingSupport:  km.ThinkingSupport,
			Deprecated:       km.Deprecated,
		})
	}

	data := make([]claudeModelEntry, 0, len(entries))
	for _, e := range entries {
		owner := e.Provider
		if e.Provider == "claude-oauth" {
			owner = "anthropic"
		}
		data = append(data, claudeModelEntry{
			ID:        e.Name,
			Object:    "model",
			Created:   1700000000,
			OwnedBy:   owner,
			MaxTokens: e.ContextWindow,
		})
	}

	firstID, lastID := "", ""
	if len(data) > 0 {
		firstID = data[0].ID
		lastID = data[len(data)-1].ID
	}
	writeJSON(w, http.StatusOK, claudeModelsResponse{Data: data, HasMore: false, FirstID: firstID, LastID: lastID})
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

// GetWasteFindings returns waste detection findings.
func (h *Handler) GetWasteFindings(w http.ResponseWriter, r *http.Request) {
	if h.optimizers == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(h.optimizers.GetWasteFindings()))
}

// AnthropicPassthrough proxies requests to the upstream Anthropic API.
// Used for Claude Code CLI endpoints like /api/claude_code/* and /v1/mcp_servers.
func (h *Handler) AnthropicPassthrough(w http.ResponseWriter, r *http.Request) {

	upstreamURL := "https://api.anthropic.com" + r.URL.RequestURI()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, h.cfg.MaxRequestBody))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, proxy.ErrorResponse{
				Type:  "error",
				Error: proxy.ErrorDetail{Type: "invalid_request_error", Message: "failed to read request body"},
			})
			return
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL, bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, proxy.ErrorResponse{
			Type:  "error",
			Error: proxy.ErrorDetail{Type: "api_error", Message: "failed to create upstream request"},
		})
		return
	}

	// Forward ALL client headers transparently (preserves x-api-key, x-app,
	// X-Stainless-*, User-Agent fingerprint from Claude Code CLI).
	for k, vv := range r.Header {
		httpReq.Header[k] = vv
	}
	for _, hdr := range []string{"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailers", "Transfer-Encoding", "Upgrade", "Host"} {
		httpReq.Header.Del(hdr)
	}

	// Resolve arl_ tokens before forwarding to Anthropic.
	// arl_ tokens are gateway-internal and must not be sent to api.anthropic.com.
	apiKey := httpReq.Header.Get("x-api-key")
	if apiKey == "" {
		if auth := httpReq.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			apiKey = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if strings.HasPrefix(apiKey, "arl_") {
		resolved := false
		if h.profileRedis != nil {
			if profileName, rerr := ResolveProfileToken(h.profileRedis, apiKey); rerr == nil && profileName != "" {
				if p, perr := getProfile(ctx, h.profileRedis, profileName); perr == nil && p != nil {
					if p.APIKey != "" {
						httpReq.Header.Set("x-api-key", p.APIKey)
						httpReq.Header.Del("Authorization")
						resolved = true
					} else if p.PassthroughAuth {
						resolved = true
					} else if h.keyPool != nil {
						if poolKey, ok := h.keyPool.Acquire(); ok {
							httpReq.Header.Set("x-api-key", poolKey)
							httpReq.Header.Del("Authorization")
							resolved = true
						}
					}
				}
				slog.Info("passthrough arl_ token resolved", "profile", profileName, "resolved", resolved)
			}
		}
		if !resolved {
			slog.Warn("passthrough rejected: unresolved arl_ token", "path", r.URL.Path)
			writeJSON(w, http.StatusUnauthorized, proxy.ErrorResponse{
				Type:  "error",
				Error: proxy.ErrorDetail{Type: "authentication_error", Message: "invalid or expired profile token"},
			})
			return
		}
	}

	// Claude OAuth tokens must be sent as x-api-key, not Authorization: Bearer.
	if auth := httpReq.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		tok := strings.TrimPrefix(auth, "Bearer ")
		if strings.HasPrefix(tok, "sk-ant-oat") {
			httpReq.Header.Set("x-api-key", tok)
			httpReq.Header.Del("Authorization")
		}
	}
	if httpReq.Header.Get("anthropic-version") == "" {
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	}
	if httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, proxy.ErrorResponse{
			Type:  "error",
			Error: proxy.ErrorDetail{Type: "api_error", Message: "upstream request failed"},
		})
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// MCPProxyHandle proxies MCP JSON-RPC requests to Z.AI MCP servers.
func (h *Handler) MCPProxyHandle(w http.ResponseWriter, r *http.Request) {
	if h.mcpProxy == nil {
		http.Error(w, "MCP proxy not enabled", http.StatusServiceUnavailable)
		return
	}
	serverName := chi.URLParam(r, "server")
	if serverName == "" {
		http.Error(w, "missing server name", http.StatusBadRequest)
		return
	}
	h.mcpProxy.ProxyMCP(w, r, serverName)
}

// MCPListServers returns the list of available MCP servers.
func (h *Handler) MCPListServers(w http.ResponseWriter, r *http.Request) {
	if h.mcpProxy == nil {
		http.Error(w, "MCP proxy not enabled", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"servers": h.mcpProxy.ListServers(),
	})
}
