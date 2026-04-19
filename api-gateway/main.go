package main

import (
	"context"
	"embed"
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

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/handler"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/middleware"
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy"
	"github.com/klxhunter/agent-rate-limit/api-gateway/provider"
	"github.com/klxhunter/agent-rate-limit/api-gateway/proxy"
	"github.com/klxhunter/agent-rate-limit/api-gateway/queue"
)

//go:embed all:static
var staticFS embed.FS

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg := config.Load()
	slog.Info("configuration loaded", "port", cfg.ServerPort, "redis", cfg.RedisAddr, "upstream", cfg.UpstreamURL)

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
	modelLimiter := middleware.NewAdaptiveLimiter(cfg.ModelLimits, cfg.DefaultLimit, cfg.GlobalLimit, cfg.ProbeMultiplier)
	middleware.SetModelPriority(config.ParseModelPriority(cfg.ModelPriority))
	keyPool := proxy.NewKeyPool(cfg.UpstreamAPIKeys, cfg.UpstreamRPMLimit)

	// --- Provider OAuth ---
	providerRegistry := provider.NewRegistry()
	tokenStore := provider.NewTokenStore(cfg.RedisAddr)
	authHandler := provider.NewAuthHandler(tokenStore, providerRegistry)
	resolver := provider.NewResolver(providerRegistry, tokenStore)

	// --- WebSocket Hub ---
	wsHub := handler.NewWebSocketHub()
	go wsHub.Run()

	// New handlers
	startedAt := time.Now()
	profileHandler := handler.NewProfileHandler(cfg.RedisAddr)
	usageHandler := handler.NewUsageHandler(cfg.RedisAddr)
	quotaHandler := handler.NewQuotaHandler(cfg.RedisAddr, tokenStore, cfg)

	// Wire usage recording: every metrics.RecordTokens call also persists to Redis.
	m.SetUsageRecorder(func(model string, input, output int, cost float64) {
		if usageHandler != nil {
			usageHandler.RecordUsage(model, input, output, cost)
		}
	})

	// Build profile Redis client for Handler to look up profiles during routing.
	var profileRdb *redis.Client
	if profileHandler != nil {
		profileRdb = profileHandler.Redis()
	}

	h := handler.New(dfClient, m, anthropicProxy, geminiCodeAssistProxy, openAIProxy, geminiAPIProxy, modelLimiter, keyPool, cfg, privacyPipeline, tokenStore, resolver, anomalyDetector, usageHandler, quotaHandler, profileRdb, wsHub.Broadcast)

	overviewHandler := handler.NewOverviewHandler(dfClient, tokenStore, cfg, startedAt, m, dfClient, cfg.RateLimiterAddr)
	configHandler := handler.NewConfigHandler(cfg, cfg.RedisAddr)
	refreshWorker := provider.NewRefreshWorker(tokenStore, providerRegistry)
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
	go func() {
		syncZAIKeys(keyPool, tokenStore)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			syncZAIKeys(keyPool, tokenStore)
		}
	}()

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
	rl := middleware.NewRateLimiter(cfg)
	r.Use(rl.Middleware)

	// Provider auth routes
	// To rate-limit login: wrap authHandler.DashboardLogin with middleware.NewLoginLimiter().
	// Example: r.With(middleware.NewLoginLimiter()).Post("/v1/auth/login", authHandler.DashboardLogin)
	r.Route("/v1", authHandler.Routes())

	// Claude OAuth loopback callback: redirect_uri is http://localhost:port/callback
	r.Get("/callback", authHandler.HandleClaudeCallback)

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
	r.Get("/v1/models", h.GetModels)

	// New handler routes
	profileHandler.Routes()(r)
	usageHandler.Routes()(r)
	quotaHandler.Routes()(r)
	overviewHandler.Routes(r)
	configHandler.Routes(r)

	// WebSocket endpoint for live dashboard updates
	r.Get("/ws", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		handler.HandleWebSocket(wsHub, w, req)
	}))

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
