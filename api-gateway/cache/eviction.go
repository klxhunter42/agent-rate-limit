package cache

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	Enabled     bool
	EvictPct    float64
	EvictPeriod time.Duration
}

func envBoolOr(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envFloatOr(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func LoadConfig() Config {
	return Config{
		Enabled:     envBoolOr("CACHE_EVICTION_ENABLED", false),
		EvictPct:    envFloatOr("CACHE_EVICTION_PCT", 10.0),
		EvictPeriod: 5 * time.Minute,
	}
}

type cacheMetrics struct {
	keysEvicted prometheus.Counter
	roiScore    prometheus.Histogram
	passDur     prometheus.Histogram
}

type EvictionManager struct {
	cfg Config
	rdb *redis.Client
	m   *cacheMetrics
}

func New(reg prometheus.Registerer, rdb *redis.Client) *EvictionManager {
	cfg := LoadConfig()
	m := &cacheMetrics{
		keysEvicted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "cache_eviction_keys_evicted_total", Help: "Cache keys evicted",
		}),
		roiScore: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "cache_eviction_roi_score", Help: "ROI score at eviction",
			Buckets: []float64{0, 0.1, 0.3, 0.5, 0.7, 0.9, 1.0, 2.0, 5.0},
		}),
		passDur: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "cache_eviction_pass_duration_seconds", Help: "Eviction pass duration",
		}),
	}
	reg.MustRegister(m.keysEvicted, m.roiScore, m.passDur)
	return &EvictionManager{cfg: cfg, rdb: rdb, m: m}
}

// RecordHit records a cache hit with token savings.
func (e *EvictionManager) RecordHit(ctx context.Context, key string, tokensSaved int) {
	if e.rdb == nil {
		return
	}
	pipe := e.rdb.Pipeline()
	pipe.HIncrBy(ctx, fmt.Sprintf("cache:stats:%s", key), "tokens_saved", int64(tokensSaved))
	pipe.HIncrBy(ctx, fmt.Sprintf("cache:stats:%s", key), "hit_count", 1)
	pipe.Expire(ctx, fmt.Sprintf("cache:stats:%s", key), 24*time.Hour)
	pipe.Exec(ctx)
}

// RecordInjection records tokens injected from cache.
func (e *EvictionManager) RecordInjection(ctx context.Context, key string, tokensInjected int) {
	if e.rdb == nil {
		return
	}
	e.rdb.HSet(ctx, fmt.Sprintf("cache:stats:%s", key), "tokens_injected", tokensInjected)
	e.rdb.Expire(ctx, fmt.Sprintf("cache:stats:%s", key), 24*time.Hour)
}

// Evict runs an eviction pass, removing bottom percentile by ROI.
func (e *EvictionManager) Evict(ctx context.Context) (int, error) {
	start := time.Now()

	// Get all cache keys
	var cursor uint64
	var allKeys []string
	for {
		keys, next, err := e.rdb.Scan(ctx, cursor, "cache:stats:*", 100).Result()
		if err != nil {
			return 0, err
		}
		allKeys = append(allKeys, keys...)
		cursor = next
		if cursor == 0 {
			break
		}
	}

	if len(allKeys) == 0 {
		e.m.passDur.Observe(time.Since(start).Seconds())
		return 0, nil
	}

	// Fetch ROI for each key
	type keyROI struct {
		key string
		roi float64
	}
	var entries []keyROI
	for _, k := range allKeys {
		stats, err := e.rdb.HGetAll(ctx, k).Result()
		if err != nil {
			continue
		}
		saved, _ := strconv.ParseFloat(stats["tokens_saved"], 64)
		injected, _ := strconv.ParseFloat(stats["tokens_injected"], 64)
		roi := 0.0
		if injected > 0 {
			roi = saved / injected
		}
		entries = append(entries, keyROI{key: k, roi: roi})
	}

	// Sort by ROI ascending
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].roi < entries[i].roi {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Evict bottom EvictPct%
	evictCount := int(float64(len(entries)) * e.cfg.EvictPct / 100.0)
	if evictCount < 1 && len(entries) > 0 {
		evictCount = 1
	}
	if evictCount > len(entries) {
		evictCount = len(entries)
	}

	evicted := 0
	for i := 0; i < evictCount; i++ {
		e.m.roiScore.Observe(entries[i].roi)
		e.rdb.Del(ctx, entries[i].key)
		evicted++
	}

	e.m.keysEvicted.Add(float64(evicted))
	e.m.passDur.Observe(time.Since(start).Seconds())
	return evicted, nil
}

// StartEvictionLoop runs periodic eviction.
func (e *EvictionManager) StartEvictionLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(e.cfg.EvictPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.Evict(ctx)
			}
		}
	}()
}
