package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/handler"
	"github.com/klxhunter/agent-rate-limit/api-gateway/metrics"
	"github.com/klxhunter/agent-rate-limit/api-gateway/middleware"
	"github.com/klxhunter/agent-rate-limit/api-gateway/proxy"
	"github.com/klxhunter/agent-rate-limit/api-gateway/queue"
)

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
	m := metrics.New(func() float64 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		n, err := dfClient.QueueDepth(ctx)
		if err != nil {
			return 0
		}
		return float64(n)
	})

	// --- Handlers ---
	anthropicProxy := proxy.NewAnthropicProxy(cfg, m)
	modelLimiter := middleware.NewAdaptiveLimiter(cfg.ModelLimits, cfg.DefaultLimit, cfg.GlobalLimit)
	keyPool := proxy.NewKeyPool(cfg.UpstreamAPIKeys, cfg.UpstreamRPMLimit)
	h := handler.New(dfClient, m, anthropicProxy, modelLimiter, keyPool, cfg)

	// --- Router ---
	r := chi.NewRouter()
	r.Use(middleware.Logging)
	r.Use(m.Middleware)

	// Rate limiting
	rl := middleware.NewRateLimiter(cfg)
	r.Use(rl.Middleware)

	// Routes
	r.Post("/v1/chat/completions", h.ChatCompletions)
	r.Post("/v1/messages", h.Messages)
	r.Get("/v1/result/{requestID}", h.GetResult)
	r.Get("/health", h.Health)
	r.Get("/v1/limiter-status", h.LimiterStatus)
	r.Handle("/metrics", m.Handler())

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
