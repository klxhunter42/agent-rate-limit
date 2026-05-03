package main

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	_ "go.uber.org/automaxprocs"

	"github.com/klxhunter/agent-rate-limit/api-gateway/bandit"
	"github.com/klxhunter/agent-rate-limit/api-gateway/cache"
	"github.com/klxhunter/agent-rate-limit/api-gateway/caveman"
	"github.com/klxhunter/agent-rate-limit/api-gateway/chunker"
	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/delta"
	"github.com/klxhunter/agent-rate-limit/api-gateway/disclosure"
	"github.com/klxhunter/agent-rate-limit/api-gateway/filter"
	"github.com/klxhunter/agent-rate-limit/api-gateway/handler"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/middleware"
	"github.com/klxhunter/agent-rate-limit/api-gateway/packer"
	"github.com/klxhunter/agent-rate-limit/api-gateway/prefetcher"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy"
	"github.com/klxhunter/agent-rate-limit/api-gateway/provider"
	"github.com/klxhunter/agent-rate-limit/api-gateway/proxy"
	"github.com/klxhunter/agent-rate-limit/api-gateway/queue"
	"github.com/klxhunter/agent-rate-limit/api-gateway/sketch"
	"github.com/klxhunter/agent-rate-limit/api-gateway/summarizer"
	"github.com/klxhunter/agent-rate-limit/api-gateway/warmstart"
	"github.com/klxhunter/agent-rate-limit/api-gateway/waste"
)

//go:embed all:static
var staticFS embed.FS

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg := config.Load()
	slog.Info("configuration loaded", "port", cfg.ServerPort, "redis", cfg.RedisAddr, "upstream", cfg.UpstreamURL)
	slog.Info("GLM mode", "enabled", cfg.GLMMode)

	// --- OpenTelemetry ---
	shutdown := initTracer(cfg.OTLPEndpoint)
	defer shutdown()

	// --- Dragonfly / Redis ---
	dfClient, err := queue.NewDragonflyClient(cfg)
	if err != nil {
		slog.Error("failed to connect to dragonfly", "error", err)
		os.Exit(1)
	}
	defer dfClient.Close()

	// --- Metrics ---
	pricingMap := make(map[string][2]float64, len(cfg.ModelPricing))
	for model, p := range cfg.ModelPricing {
		pricingMap[model] = [2]float64{p.InputPerMillion, p.OutputPerMillion}
	}
	m := metrics.New(func() float64 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		n, err := dfClient.QueueDepth(ctx)
		if err != nil {
			return 0
		}
		return float64(n)
	}, pricingMap)

	// --- Runtime metrics (goroutines, heap, GC, Dragonfly health) ---
	rtMetrics := middleware.NewRuntimeMetrics(m.Registry())
	rtMetrics.MustRegister(m.Registry())
	ctx, cancelRT := context.WithCancel(context.Background())
	defer cancelRT()
	go rtMetrics.Start(ctx, cfg.RedisAddr)

	// --- Anomaly detector ---
	anomalyDetector := middleware.NewAnomalyDetector(m.Registry())

	// --- Privacy pipeline ---
	privacyCfg := privacy.LoadConfig()
	slog.Info("privacy pipeline", "enabled", privacyCfg.Enabled, "secrets", privacyCfg.SecretsEnabled, "pii", privacyCfg.PIIEnabled)
	privacyPipeline := privacy.NewPipeline(privacyCfg, privacy.NewMetrics(m.Registry()))

	// --- Handlers ---
	anthropicProxy := proxy.NewAnthropicProxy(cfg, m)
	geminiCodeAssistProxy := proxy.NewGeminiCodeAssistProxy(m, cfg.GeminiCodeAssistEndpoint, cfg.GeminiDefaultModel)
	openAIProxy := proxy.NewOpenAIProxy(cfg, m)
	geminiAPIProxy := proxy.NewGeminiAPIProxy(cfg, m)
	modelLimiter := middleware.NewAdaptiveLimiter(cfg.ModelLimits, cfg.VisionModelLimits, cfg.DefaultLimit, cfg.GlobalLimit, cfg.ProbeMultiplier)
	middleware.SetModelPriority(config.ParseModelPriority(cfg.ModelPriority))
	keyPool := proxy.NewKeyPool(cfg.UpstreamAPIKeys, cfg.UpstreamRPMLimit)

	// Z.AI web chat proxy (free access via chat.z.ai signed API).
	var zaiWebProxy *proxy.ZAIWebProxy
	if cfg.ZAIWebEnabled {
		zaiWebProxy = proxy.NewZAIWebProxy(cfg, m)
		slog.Info("zaiweb proxy enabled", "token_configured", cfg.ZAIWebToken != "", "models", cfg.ZAIWebModels)
	}

	// --- Provider OAuth ---
	providerRegistry := provider.NewRegistry()
	tokenStore := provider.NewTokenStore(cfg.RedisAddr)
	tokenStore.MigrateProviderRenames()
	authHandler := provider.NewAuthHandler(tokenStore, providerRegistry)
	resolver := provider.NewResolver(providerRegistry, tokenStore, cfg.GLMMode)
	refreshWorker := provider.NewRefreshWorker(tokenStore, providerRegistry)
	authHandler.SetRefreshWorker(refreshWorker)

	// --- WebSocket Hub ---
	wsHub := handler.NewWebSocketHub()
	go wsHub.Run()

	// New handlers
	startedAt := time.Now()
	profileHandler := handler.NewProfileHandler(cfg.RedisAddr)
	if profileHandler != nil {
		authHandler.SetProfileRedis(profileHandler.Redis())
		providerRegistry.LoadCustomProviders(profileHandler.Redis())
	}
	usageHandler := handler.NewUsageHandler(cfg.RedisAddr)
	quotaHandler := handler.NewQuotaHandler(cfg.RedisAddr, tokenStore, cfg)

	// --- Token Optimizers (13 packages, all off by default) ---
	// Shared Redis client for optimizer packages.
	optRdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	// MCP proxy for Z.AI remote MCP servers (GLM mode only).
	var mcpProxy *proxy.MCPProxy
	if cfg.GLMMode && cfg.MCPEnabled {
		mcpProxy = proxy.NewMCPProxy(cfg, m, keyPool, optRdb)
		slog.Info("mcp proxy enabled", "servers", len(proxy.MCPServers), "cache_ttl", cfg.MCPCacheTTL, "rate_limit_per_min", cfg.MCPRateLimitPerMin)
	}


	optChunker := chunker.New(m.Registry(), optRdb)
	optPacker := packer.New(m.Registry())
	optDisclosure := disclosure.New(m.Registry(), optRdb)
	optPrefetcher := prefetcher.New(m.Registry(), optRdb)
	optBandit := bandit.New(m.Registry(), optRdb, nil)
	optSummarizer := summarizer.New(m.Registry(), optRdb)
	optDelta := delta.New(m.Registry(), optRdb)
	optSketch := sketch.New(m.Registry(), optRdb)
	optWaste := waste.New(m.Registry())
	optFilter := filter.New(m.Registry())
	optCache := cache.New(m.Registry(), optRdb)
	optWarmStart := warmstart.New(m.Registry(), optRdb)
	optCaveman := caveman.New(m.Registry())

	optimizers := &handler.Optimizers{
		Chunker:    optChunker,
		Packer:     optPacker,
		Disclosure: optDisclosure,
		Prefetcher: optPrefetcher,
		Bandit:     optBandit,
		Summarizer: optSummarizer,
		Delta:      optDelta,
		Sketch:     optSketch,
		Waste:      optWaste,
		Filter:     optFilter,
		Cache:      optCache,
		WarmStart:  optWarmStart,
		Caveman:    optCaveman,
	}

	// Background optimizer goroutines.
	bgCtx, cancelBg := context.WithCancel(context.Background())
	defer cancelBg()

	// Wire usage recording: every metrics.RecordTokens call also persists to Redis + optimizer feedback.
	m.SetUsageRecorder(func(ctx context.Context, model string, input, output int, cost float64) {
		if usageHandler != nil {
			usageHandler.RecordUsage(model, input, output, cost)
		}
		if pn := handler.ProfileNameFromContext(ctx); pn != "" {
			usageHandler.RecordProfileUsage(pn, model, input, output, cost)
			m.RecordProfileUsage(pn, model, input, output, cost)
		}
		if aid := handler.AccountIDFromContext(ctx); aid != "" {
			usageHandler.RecordAccountUsage(aid, model, input, output, cost)
		}
		sessionID := "default"
		if pn := handler.ProfileNameFromContext(ctx); pn != "" {
			sessionID = pn
		}
		if optimizers != nil {
			optimizers.PostProxyFeedback(sessionID, model, input, output)
		}
	})

	// Build profile Redis client for Handler to look up profiles during routing.
	var profileRdb *redis.Client
	if profileHandler != nil {
		profileRdb = profileHandler.Redis()
	}

	if optWaste != nil {
		optWaste.StartBackgroundScanner(bgCtx, 60*time.Second)
	}
	if optCache != nil {
		optCache.StartEvictionLoop(bgCtx)
	}

	h := handler.New(dfClient, m, anthropicProxy, geminiCodeAssistProxy, openAIProxy, geminiAPIProxy, modelLimiter, keyPool, cfg, privacyPipeline, tokenStore, resolver, anomalyDetector, usageHandler, quotaHandler, profileRdb, wsHub.Broadcast, refreshWorker, optimizers, zaiWebProxy, mcpProxy)

	overviewHandler := handler.NewOverviewHandler(dfClient, tokenStore, cfg, startedAt, m, dfClient, cfg.RateLimiterAddr)
	configHandler := handler.NewConfigHandler(cfg, cfg.RedisAddr)
	go refreshWorker.Start(context.Background())
	defer refreshWorker.Stop()

	// --- Session secret persistence ---
	_ = middleware.LoadOrGenerateSessionSecret()
	go middleware.WatchSessionSecret(context.Background())

	// --- Config file watcher (broadcasts changes via WS) ---
	cfgWatcher := middleware.NewConfigWatcher(".env", func(key, value string) {
		wsHub.Broadcast("config-changed", map[string]string{"key": key})
	})
	go cfgWatcher.Start(context.Background())

	// Sync Z.AI keys from token store into KeyPool for rotation.
	if cfg.GLMMode {
		go func() {
			syncZAIKeys(keyPool, tokenStore)
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				syncZAIKeys(keyPool, tokenStore)
			}
		}()
	}

	// Periodically export adaptive limiter state to Prometheus.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			var snapshots []metrics.ModelStatusSnapshot
			for _, ms := range modelLimiter.Status() {
				snapshots = append(snapshots, metrics.ModelStatusSnapshot{
					Name:     ms.Name,
					Limit:    float64(ms.Limit),
					InFlight: float64(ms.InFlight),
				})
			}
			m.UpdateAdaptiveMetrics(snapshots)
		}
	}()

	// --- Router ---
	r := chi.NewRouter()
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.CorrelationID)
	r.Use(middleware.RealIP)

	// IP filter (fail-open: skip if not configured)
	if cfg.IPWhitelist != "" || cfg.IPBlacklist != "" {
		ipFilter := middleware.NewIPFilter(middleware.IPFilterConfig{
			Whitelist: parseCommaList(cfg.IPWhitelist),
			Blacklist: parseCommaList(cfg.IPBlacklist),
		})
		r.Use(ipFilter)
		slog.Info("ip filter enabled", "whitelist", cfg.IPWhitelist, "blacklist", cfg.IPBlacklist)
	}

	r.Use(middleware.Logging)
	r.Use(m.Middleware)

	// Rate limiting
	rl := middleware.NewRateLimiter(cfg, m)
	r.Use(rl.Middleware)

	// WebSocket endpoint - rate limiter skips /ws internally to avoid upgrade failures
	r.Get("/ws", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		handler.HandleWebSocket(wsHub, w, req)
	}))

	// Provider auth routes (ratelimits registered first inside /v1 group to beat {provider} wildcard).
	// To rate-limit login: wrap authHandler.DashboardLogin with middleware.NewLoginLimiter().
	// Example: r.With(middleware.NewLoginLimiter()).Post("/v1/auth/login", authHandler.DashboardLogin)
	authRoutesFn := authHandler.Routes()
	r.Route("/v1", func(sub chi.Router) {
		sub.Get("/auth/accounts/ratelimits", h.GetRateLimits)
		authRoutesFn(sub)
	})

	// Claude OAuth loopback callback: redirect_uri is http://localhost:port/callback
	r.Get("/callback", authHandler.HandleClaudeCallback)
	r.Post("/callback", authHandler.HandleClaudeCallbackPost)

	// Dashboard auth middleware (for /admin/* routes)
	dashAPIKey := os.Getenv("DASHBOARD_API_KEY")
	if dashAPIKey != "" {
		slog.Info("dashboard auth enabled")
	}

	// Routes
	r.Post("/v1/chat/completions", h.ChatCompletions)
	r.Post("/v1/messages", h.Messages)
	r.Get("/v1/results/{requestID}", h.GetResult)
	r.Get("/health", h.Health)
	r.Get("/v1/limiter-status", h.LimiterStatus)
	r.Post("/v1/limiter-override", h.LimiterOverride)
	r.Get("/v1/routing/strategy", h.GetRoutingStrategy)
	r.Put("/v1/routing/strategy", h.SetRoutingStrategy)
	r.Get("/v1/logs/errors", h.GetErrorLogs)
	r.Get("/v1/logs/errors/count", h.GetErrorLogCount)

	// ZAI web chat token management + media generation.
	if cfg.ZAIWebEnabled && zaiWebProxy != nil {
		r.Get("/v1/zaiweb/status", h.ZAIWebStatus)
		r.Post("/v1/zaiweb/token", h.ZAIWebSetToken)
		r.Post("/v1/images/generations", h.ZAIWebImageGenerate)
		r.Post("/v1/audio/tts", h.ZAIWebAudioTTS)
	}
	r.Get("/v1/models", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.UserAgent()
		if strings.HasPrefix(ua, "claude-cli") || strings.HasPrefix(ua, "Claude-Code") || strings.HasPrefix(ua, "anthropic-cli") {
			h.GetModelsAnthropic(w, r)
			return
		}
		h.GetModels(w, r)
	}))
	r.Post("/v1/messages/count_tokens", h.CountTokens)
	r.Get("/v1/waste/findings", h.GetWasteFindings)

	// Mock data endpoints (manual control for Grafana dashboard testing).
	r.Post("/v1/mock/seed", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		category := r.URL.Query().Get("category")
		if category == "" {
			category = "all"
		}
		m.SeedMockCategory(category)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "seeded"})
	}))
	r.Post("/v1/mock/loop/start", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ok := m.StartMockLoop()
		w.Header().Set("Content-Type", "application/json")
		if ok {
			json.NewEncoder(w).Encode(map[string]string{"status": "started"})
		} else {
			json.NewEncoder(w).Encode(map[string]string{"status": "already_running"})
		}
	}))
	r.Post("/v1/mock/loop/stop", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ok := m.StopMockLoop()
		w.Header().Set("Content-Type", "application/json")
		if ok {
			json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
		} else {
			json.NewEncoder(w).Encode(map[string]string{"status": "not_running"})
		}
	}))
	r.Get("/v1/mock/status", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"running": m.MockLoopRunning()})
	}))

	// Claude Code CLI passthrough routes (proxy to Anthropic API)
	r.HandleFunc("/api/claude_code/policy_limits", h.AnthropicPassthrough)
	r.HandleFunc("/api/claude_code/settings", h.AnthropicPassthrough)
	r.HandleFunc("/v1/mcp_servers", h.AnthropicPassthrough)

	// MCP proxy routes (GLM mode only).
	if cfg.GLMMode && cfg.MCPEnabled {
		r.Post("/mcp/{server}", h.MCPProxyHandle)
		r.Get("/mcp", h.MCPListServers)
		slog.Info("mcp routes registered")
	}

	// New handler routes
	profileHandler.Routes()(r)
	usageHandler.Routes()(r)
	quotaHandler.Routes()(r)
	overviewHandler.Routes(r)
	configHandler.Routes(r)

	// Apply persisted max-tokens overrides from Redis.
	configHandler.LoadAndApplyMaxTokens()
	// Static dashboard SPA (Vite build output)
	staticSub, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(staticSub))

	// Admin routes with optional auth
	adminGroup := r.With(middleware.DashboardAuth(dashAPIKey))
	adminGroup.Get("/admin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}))
	adminGroup.Get("/admin/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SPA fallback: serve index.html for any sub-route
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}))
	r.Handle("/assets/*", fileServer)
	r.Handle("/metrics", m.Handler())
	r.Handle("/api/metrics", m.Handler())

	// --- Server ---
	// WriteTimeout is 0 to allow long-lived SSE streaming connections.
	srv := &http.Server{
		Addr:         cfg.ServerPort,
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: 0, // disabled for SSE streaming
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("api-gateway starting", "addr", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}

	slog.Info("server stopped")
}

func syncZAIKeys(kp *proxy.KeyPool, ts *provider.TokenStore) {
	tokens, err := ts.ListByProvider("zai")
	if err != nil || len(tokens) == 0 {
		return
	}
	var keys []string
	for _, t := range tokens {
		if !t.Paused && t.AccessToken != "" {
			keys = append(keys, t.AccessToken)
		}
	}
	kp.SyncFromStore(keys)
}

func initTracer(endpoint string) func() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		slog.Warn("failed to create OTLP exporter, tracing disabled", "error", err)
		cancel()
		return func() {}
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
	)
	otel.SetTracerProvider(tp)
	cancel()

	return func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := tp.Shutdown(shutdownCtx); err != nil {
			slog.Warn("tracer shutdown error", "error", err)
		}
	}
}

func parseCommaList(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, v := range strings.Split(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			result = append(result, v)
		}
	}
	return result
}
