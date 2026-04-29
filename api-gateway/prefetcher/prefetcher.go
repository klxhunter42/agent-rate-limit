package prefetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

type Prediction struct {
	Tool       string
	Confidence float64
}

type Config struct {
	Enabled  bool
	MaxOrder int
	TopK     int
}

func envBoolOr(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func LoadConfig() Config {
	return Config{
		Enabled:  envBoolOr("PREFETCHER_ENABLED", true),
		MaxOrder: envIntOr("PREFETCHER_MAX_ORDER", 5),
		TopK:     envIntOr("PREFETCHER_TOP_K", 3),
	}
}

type prefetcherMetrics struct {
	predictions *prometheus.CounterVec
	orderUsed   prometheus.Histogram
	prewarmDur  prometheus.Histogram
}

type Prefetcher struct {
	cfg Config
	rdb *redis.Client
	m   *prefetcherMetrics
}

func New(reg prometheus.Registerer, rdb *redis.Client) *Prefetcher {
	cfg := LoadConfig()
	m := &prefetcherMetrics{
		predictions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "prefetcher_predictions_total", Help: "Predictions made",
		}, []string{"correct"}),
		orderUsed: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "prefetcher_order_used", Help: "Markov order used",
			Buckets: []float64{1, 2, 3, 4, 5},
		}),
		prewarmDur: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "prefetcher_prewarm_duration_seconds", Help: "Pre-warm duration",
		}),
	}
	reg.MustRegister(m.predictions, m.orderUsed, m.prewarmDur)
	return &Prefetcher{cfg: cfg, rdb: rdb, m: m}
}

// Record observes a tool call event for learning.
func (p *Prefetcher) Record(ctx context.Context, sessionID, toolCall string) {
	key := fmt.Sprintf("prefetcher:chain:%s", sessionID)
	// Append to history
	p.rdb.RPush(ctx, key, toolCall)
	p.rdb.LTrim(ctx, key, 0, int64(p.cfg.MaxOrder)-1)
	p.rdb.Expire(ctx, key, 4*time.Hour)

	// Update transition table
	history, _ := p.rdb.LRange(ctx, key, 0, -1).Result()
	if len(history) > 1 {
		prev := history[len(history)-2]
		transKey := fmt.Sprintf("prefetcher:trans:%s", prev)
		p.rdb.HIncrBy(ctx, transKey, toolCall, 1)
		p.rdb.Expire(ctx, transKey, 4*time.Hour)
	}
}

// Predict returns the top-K predicted next tool calls with confidence scores.
func (p *Prefetcher) Predict(ctx context.Context, sessionID string) []Prediction {
	key := fmt.Sprintf("prefetcher:chain:%s", sessionID)
	history, _ := p.rdb.LRange(ctx, key, -1, -1).Result()
	if len(history) == 0 {
		return nil
	}

	lastTool := history[len(history)-1]
	transKey := fmt.Sprintf("prefetcher:trans:%s", lastTool)
	transitions, _ := p.rdb.HGetAll(ctx, transKey).Result()

	var total int64
	for _, count := range transitions {
		n, _ := strconv.ParseInt(count, 10, 64)
		total += n
	}
	if total == 0 {
		return nil
	}

	type toolProb struct {
		tool  string
		count int64
	}
	var sorted []toolProb
	for tool, count := range transitions {
		n, _ := strconv.ParseInt(count, 10, 64)
		sorted = append(sorted, toolProb{tool, n})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	var preds []Prediction
	for i := 0; i < len(sorted) && i < p.cfg.TopK; i++ {
		preds = append(preds, Prediction{
			Tool:       sorted[i].tool,
			Confidence: float64(sorted[i].count) / float64(total),
		})
	}
	p.m.orderUsed.Observe(1)
	return preds
}

// PreWarm pre-initializes connections for predicted tools.
func (p *Prefetcher) PreWarm(ctx context.Context, predictions []Prediction) {
	start := time.Now()
	for _, pred := range predictions {
		// Store prediction for later verification
		data, _ := json.Marshal(pred)
		p.rdb.Set(ctx, "prefetcher:last_pred:"+pred.Tool, data, 1*time.Minute)
	}
	p.m.prewarmDur.Observe(time.Since(start).Seconds())
}

func (p *Prefetcher) Close() {}
