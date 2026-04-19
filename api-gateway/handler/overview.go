package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/provider"
	"github.com/klxhunter/agent-rate-limit/api-gateway/queue"
)

type OverviewResponse struct {
	Profiles      int    `json:"profiles"`
	Accounts      int    `json:"accounts"`
	Providers     int    `json:"providers"`
	Models        int    `json:"models"`
	ActiveKeys    int    `json:"activeKeys"`
	PausedKeys    int    `json:"pausedKeys"`
	QueueDepth    int64  `json:"queueDepth"`
	HealthStatus  string `json:"healthStatus"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
	TotalRequests int64  `json:"totalRequests"`
	TotalErrors   int64  `json:"totalErrors"`
}

type HealthCheck struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Category string `json:"category"`
}

type DetailedHealthResponse struct {
	Status    string        `json:"status"`
	Checks    []HealthCheck `json:"checks"`
	Timestamp string        `json:"timestamp"`
	Uptime    int64         `json:"uptimeSeconds"`
}

type FixResponse struct {
	CheckID string `json:"checkId"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type OverviewHandler struct {
	redis           *queue.DragonflyClient
	tokenStore      *provider.TokenStore
	cfg             *config.Config
	startedAt       time.Time
	metrics         *metrics.Metrics
	queue           *queue.DragonflyClient
	rateLimiterAddr string
}

func NewOverviewHandler(redis *queue.DragonflyClient, tokenStore *provider.TokenStore, cfg *config.Config, startedAt time.Time, m *metrics.Metrics, q *queue.DragonflyClient, rateLimiterAddr string) *OverviewHandler {
	return &OverviewHandler{
		redis:           redis,
		tokenStore:      tokenStore,
		cfg:             cfg,
		startedAt:       startedAt,
		metrics:         m,
		queue:           q,
		rateLimiterAddr: rateLimiterAddr,
	}
}

func (oh *OverviewHandler) Routes(r chi.Router) {
	r.Get("/v1/overview", oh.Overview)
	r.Get("/v1/health/detailed", oh.DetailedHealth)
	r.Post("/v1/health/fix/{checkId}", oh.FixHealthCheck)
}

func (oh *OverviewHandler) Overview(w http.ResponseWriter, r *http.Request) {
	tokens, err := oh.tokenStore.ListAll()
	if err != nil {
		slog.Error("overview: failed to list tokens", "error", err)
		tokens = nil
	}

	profiles := make(map[string]bool)
	accounts := make(map[string]bool)
	providers := make(map[string]bool)
	models := make(map[string]bool)
	activeKeys, pausedKeys := 0, 0

	for _, t := range tokens {
		providers[t.Provider] = true
		accounts[t.Provider+":"+t.AccountID] = true
		profiles[t.AccountID] = true
		if t.Paused {
			pausedKeys++
		} else {
			activeKeys++
		}
	}

	for _, km := range knownModels {
		models[km.Name] = true
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	var queueDepth int64
	if oh.queue != nil {
		queueDepth, _ = oh.queue.QueueDepth(ctx)
	}

	checks := oh.runChecks(ctx)
	healthStatus := "healthy"
	for _, c := range checks {
		if c.Status == "fail" {
			healthStatus = "unhealthy"
			break
		}
		if c.Status == "warn" {
			healthStatus = "degraded"
		}
	}

	errorLogMu.Lock()
	totalErrors := int64(errorLogTotal)
	errorLogMu.Unlock()

	writeJSON(w, http.StatusOK, OverviewResponse{
		Profiles:      len(profiles),
		Accounts:      len(accounts),
		Providers:     len(providers),
		Models:        len(models),
		ActiveKeys:    activeKeys,
		PausedKeys:    pausedKeys,
		QueueDepth:    queueDepth,
		HealthStatus:  healthStatus,
		UptimeSeconds: int64(time.Since(oh.startedAt).Seconds()),
		TotalRequests: oh.countTotalRequests(),
		TotalErrors:   totalErrors,
	})
}

func (oh *OverviewHandler) DetailedHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	checks := oh.runChecks(ctx)

	status := "healthy"
	for _, c := range checks {
		if c.Status == "fail" {
			status = "unhealthy"
			break
		}
		if c.Status == "warn" && status != "unhealthy" {
			status = "degraded"
		}
	}

	writeJSON(w, http.StatusOK, DetailedHealthResponse{
		Status:    status,
		Checks:    checks,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Uptime:    int64(time.Since(oh.startedAt).Seconds()),
	})
}

func (oh *OverviewHandler) FixHealthCheck(w http.ResponseWriter, r *http.Request) {
	checkID := chi.URLParam(r, "checkId")
	if checkID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "checkId is required"})
		return
	}

	fix := oh.applyFix(checkID)
	writeJSON(w, http.StatusOK, fix)
}

func (oh *OverviewHandler) runChecks(ctx context.Context) []HealthCheck {
	var checks []HealthCheck

	checks = append(checks, oh.checkDragonfly(ctx))
	checks = append(checks, oh.checkRateLimiter(ctx))
	checks = append(checks, oh.checkPrometheus())
	checks = append(checks, oh.checkKeyPool())
	checks = append(checks, oh.checkUpstream(ctx))
	checks = append(checks, oh.checkMemory())

	return checks
}

func (oh *OverviewHandler) checkDragonfly(ctx context.Context) HealthCheck {
	c := HealthCheck{ID: "dragonfly", Name: "Dragonfly / Redis", Category: "connectivity"}

	if oh.redis == nil {
		c.Status = "fail"
		c.Message = "redis client not configured"
		return c
	}

	// Use the queue client's underlying connection.
	// The DragonflyClient doesn't expose ping, so we use a timeout on QueueDepth.
	dCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_, err := oh.redis.QueueDepth(dCtx)
	if err != nil {
		c.Status = "fail"
		c.Message = fmt.Sprintf("ping failed: %s", err.Error())
		return c
	}

	c.Status = "pass"
	c.Message = "connected"
	return c
}

func (oh *OverviewHandler) checkRateLimiter(ctx context.Context) HealthCheck {
	c := HealthCheck{ID: "rate-limiter", Name: "Rate Limiter", Category: "connectivity"}

	if oh.rateLimiterAddr == "" {
		c.Status = "warn"
		c.Message = "rate limiter address not configured"
		return c
	}

	hCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(hCtx, http.MethodGet, oh.rateLimiterAddr+"/health", nil)
	if err != nil {
		c.Status = "fail"
		c.Message = fmt.Sprintf("request creation failed: %s", err.Error())
		return c
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Status = "fail"
		c.Message = fmt.Sprintf("connection failed: %s", err.Error())
		return c
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.Status = "pass"
		c.Message = fmt.Sprintf("healthy (HTTP %d)", resp.StatusCode)
	} else {
		c.Status = "warn"
		c.Message = fmt.Sprintf("unexpected status: HTTP %d", resp.StatusCode)
	}
	return c
}

func (oh *OverviewHandler) checkPrometheus() HealthCheck {
	c := HealthCheck{ID: "prometheus", Name: "Prometheus Metrics", Category: "resources"}

	if oh.metrics == nil {
		c.Status = "fail"
		c.Message = "metrics collector not configured"
		return c
	}

	c.Status = "pass"
	c.Message = "metrics endpoint active"
	return c
}

func (oh *OverviewHandler) checkKeyPool() HealthCheck {
	c := HealthCheck{ID: "key-pool", Name: "API Key Pool", Category: "config"}

	tokens, err := oh.tokenStore.ListAll()
	if err != nil {
		c.Status = "warn"
		c.Message = fmt.Sprintf("failed to list tokens: %s", err.Error())
		return c
	}

	activeCount := 0
	for _, t := range tokens {
		if !t.Paused && t.AccessToken != "" {
			activeCount++
		}
	}

	if activeCount == 0 {
		c.Status = "warn"
		c.Message = "no active API keys configured"
		return c
	}

	c.Status = "pass"
	c.Message = fmt.Sprintf("%d active key(s)", activeCount)
	return c
}

func (oh *OverviewHandler) checkUpstream(ctx context.Context) HealthCheck {
	c := HealthCheck{ID: "upstream", Name: "Upstream Connectivity", Category: "upstream"}

	if oh.cfg.UpstreamURL == "" {
		c.Status = "fail"
		c.Message = "upstream URL not configured"
		return c
	}

	hCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(hCtx, http.MethodGet, oh.cfg.UpstreamURL, nil)
	if err != nil {
		c.Status = "fail"
		c.Message = fmt.Sprintf("request creation failed: %s", err.Error())
		return c
	}
	req.Header.Set("User-Agent", "agent-rate-limit/health-check")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Status = "fail"
		c.Message = fmt.Sprintf("connection failed: %s", err.Error())
		return c
	}
	defer resp.Body.Close()

	// Upstream may return auth errors (401/403) which still proves connectivity.
	if resp.StatusCode < 500 {
		c.Status = "pass"
		c.Message = fmt.Sprintf("reachable (HTTP %d)", resp.StatusCode)
	} else {
		c.Status = "fail"
		c.Message = fmt.Sprintf("server error: HTTP %d", resp.StatusCode)
	}
	return c
}

func (oh *OverviewHandler) checkMemory() HealthCheck {
	c := HealthCheck{ID: "memory", Name: "Memory Usage", Category: "resources"}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	allocMB := float64(ms.HeapAlloc) / (1024 * 1024)
	sysMB := float64(ms.Sys) / (1024 * 1024)
	usagePct := (allocMB / sysMB) * 100

	if usagePct > 90 {
		c.Status = "fail"
		c.Message = fmt.Sprintf("heap %.1f MB / %.1f MB (%.0f%%)", allocMB, sysMB, usagePct)
	} else if usagePct > 75 {
		c.Status = "warn"
		c.Message = fmt.Sprintf("heap %.1f MB / %.1f MB (%.0f%%)", allocMB, sysMB, usagePct)
	} else {
		c.Status = "pass"
		c.Message = fmt.Sprintf("heap %.1f MB / %.1f MB (%.0f%%)", allocMB, sysMB, usagePct)
	}
	return c
}

func (oh *OverviewHandler) applyFix(checkID string) FixResponse {
	switch checkID {
	case "dragonfly":
		return FixResponse{
			CheckID: checkID,
			Status:  "info",
			Message: "reconnect hint: restart the gateway or check Dragonfly service. Connection pools are managed automatically by go-redis.",
		}
	case "rate-limiter":
		return FixResponse{
			CheckID: checkID,
			Status:  "info",
			Message: fmt.Sprintf("verify rate-limiter is running at %s. The gateway rate-limits locally as fallback.", oh.rateLimiterAddr),
		}
	case "prometheus":
		return FixResponse{
			CheckID: checkID,
			Status:  "pass",
			Message: "prometheus metrics are always active when the gateway is running. No fix needed.",
		}
	case "key-pool":
		return FixResponse{
			CheckID: checkID,
			Status:  "info",
			Message: "no automated fix. Configure API keys via UPSTREAM_API_KEYS env var or provider OAuth endpoints.",
		}
	case "upstream":
		return FixResponse{
			CheckID: checkID,
			Status:  "info",
			Message: fmt.Sprintf("verify upstream is reachable at %s. Check DNS, firewall, and upstream service health.", oh.cfg.UpstreamURL),
		}
	case "memory":
		return FixResponse{
			CheckID: checkID,
			Status:  "info",
			Message: "high memory detected. Consider increasing container memory limit or restarting the gateway.",
		}
	default:
		return FixResponse{
			CheckID: checkID,
			Status:  "error",
			Message: fmt.Sprintf("unknown check ID: %s", checkID),
		}
	}
}

func (oh *OverviewHandler) countTotalRequests() int64 {
	errorLogMu.Lock()
	count := int64(errorLogTotal)
	errorLogMu.Unlock()
	return count
}
