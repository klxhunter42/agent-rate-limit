package warmstart

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

const dimensions = 32

type Config struct {
	Enabled    bool
	TopK       int
	MinSimilar float64
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
		Enabled:    envBoolOr("WARMSTART_ENABLED", false),
		TopK:       envIntOr("WARMSTART_TOP_K", 3),
		MinSimilar: envFloatOr("WARMSTART_MIN_SIMILARITY", 0.5),
	}
}

type warmstartMetrics struct {
	sessions   *prometheus.CounterVec
	similarity prometheus.Histogram
	warmupDur  prometheus.Histogram
}

type WarmStart struct {
	cfg Config
	rdb *redis.Client
	m   *warmstartMetrics
}

func New(reg prometheus.Registerer, rdb *redis.Client) *WarmStart {
	cfg := LoadConfig()
	m := &warmstartMetrics{
		sessions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "warmstart_sessions_warmed_total", Help: "Warm start attempts",
		}, []string{"result"}),
		similarity: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "warmstart_similarity_score", Help: "Cosine similarity score",
			Buckets: []float64{0, 0.1, 0.3, 0.5, 0.7, 0.8, 0.9, 0.95, 1.0},
		}),
		warmupDur: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "warmstart_warmup_duration_seconds", Help: "Warmup duration",
		}),
	}
	reg.MustRegister(m.sessions, m.similarity, m.warmupDur)
	return &WarmStart{cfg: cfg, rdb: rdb, m: m}
}

// ComputeSignature creates a 32-dim feature vector for a session.
func (w *WarmStart) ComputeSignature(sessionData map[string]any) [dimensions]float64 {
	var sig [dimensions]float64

	// Dims 0-3: model type one-hot
	model, _ := sessionData["model"].(string)
	switch {
	case containsAny(model, "claude"):
		sig[0] = 1
	case containsAny(model, "gpt", "o1", "o3"):
		sig[1] = 1
	case containsAny(model, "gemini"):
		sig[2] = 1
	case containsAny(model, "glm"):
		sig[3] = 1
	}

	// Dims 4-7: content type distribution
	if v, ok := sessionData["code_ratio"].(float64); ok {
		sig[4] = v
	}
	if v, ok := sessionData["json_ratio"].(float64); ok {
		sig[5] = v
	}
	if v, ok := sessionData["md_ratio"].(float64); ok {
		sig[6] = v
	}
	if v, ok := sessionData["text_ratio"].(float64); ok {
		sig[7] = v
	}

	// Dims 8-15: tool call frequency (top 8 tools by normalized count)
	if tools, ok := sessionData["tool_counts"].(map[string]float64); ok {
		i := 8
		for _, count := range tools {
			if i >= 16 {
				break
			}
			sig[i] = count
			i++
		}
	}

	// Dims 16-18: budget level distribution
	if v, ok := sessionData["budget_green_pct"].(float64); ok {
		sig[16] = v
	}
	if v, ok := sessionData["budget_yellow_pct"].(float64); ok {
		sig[17] = v
	}
	if v, ok := sessionData["budget_red_pct"].(float64); ok {
		sig[18] = v
	}

	// Dims 19-22: request size buckets
	if v, ok := sessionData["avg_input_tokens"].(float64); ok {
		sig[19] = v / 10000
	}
	if v, ok := sessionData["avg_output_tokens"].(float64); ok {
		sig[20] = v / 1000
	}
	if v, ok := sessionData["total_requests"].(float64); ok {
		sig[21] = v / 100
	}
	if v, ok := sessionData["avg_duration_ms"].(float64); ok {
		sig[22] = v / 10000
	}

	// Dims 23-27: intent distribution
	if v, ok := sessionData["intent_code_pct"].(float64); ok {
		sig[23] = v
	}
	if v, ok := sessionData["intent_analysis_pct"].(float64); ok {
		sig[24] = v
	}
	if v, ok := sessionData["intent_search_pct"].(float64); ok {
		sig[25] = v
	}
	if v, ok := sessionData["intent_action_pct"].(float64); ok {
		sig[26] = v
	}
	if v, ok := sessionData["intent_chat_pct"].(float64); ok {
		sig[27] = v
	}

	// Dims 28-31: hash projection of project/touched symbols
	if v, ok := sessionData["project_hash"].(float64); ok {
		sig[28] = v
	}
	if v, ok := sessionData["symbol_density"].(float64); ok {
		sig[29] = v
	}
	if v, ok := sessionData["stream_pct"].(float64); ok {
		sig[30] = v
	}
	if v, ok := sessionData["error_rate"].(float64); ok {
		sig[31] = v
	}

	return sig
}

// FindSimilar finds the most similar past session.
func (w *WarmStart) FindSimilar(ctx context.Context, signature [dimensions]float64, projectRoot string) (string, float64, error) {
	if w.rdb == nil {
		return "", 0, nil
	}

	pattern := "warmstart:sig:*"
	if projectRoot != "" {
		pattern = "warmstart:sig:" + projectRoot + ":*"
	}

	var cursor uint64
	var bestID string
	bestSim := 0.0

	for {
		keys, next, err := w.rdb.Scan(ctx, cursor, pattern, 50).Result()
		if err != nil {
			return "", 0, err
		}
		for _, key := range keys {
			data, err := w.rdb.Get(ctx, key).Bytes()
			if err != nil {
				continue
			}
			var otherSig [dimensions]float64
			if json.Unmarshal(data, &otherSig) != nil {
				continue
			}
			sim := cosineSimilarity(signature, otherSig)
			w.m.similarity.Observe(sim)
			if sim > bestSim {
				bestSim = sim
				bestID = key
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}

	return bestID, bestSim, nil
}

// WarmSession pre-populates optimizer state from a similar past session.
func (w *WarmStart) WarmSession(ctx context.Context, sessionID string, sessionData map[string]any) bool {
	start := time.Now()
	sig := w.ComputeSignature(sessionData)

	projectRoot, _ := sessionData["project_root"].(string)
	bestID, sim, err := w.FindSimilar(ctx, sig, projectRoot)
	if err != nil || bestID == "" || sim < w.cfg.MinSimilar {
		w.m.sessions.WithLabelValues("miss").Inc()
		w.m.warmupDur.Observe(time.Since(start).Seconds())
		return false
	}

	// Store current session signature for future lookups
	w.StoreSignature(ctx, sessionID, sig, projectRoot)

	w.m.sessions.WithLabelValues("hit").Inc()
	w.m.warmupDur.Observe(time.Since(start).Seconds())
	return true
}

// StoreSignature persists a session signature for future lookups.
func (w *WarmStart) StoreSignature(ctx context.Context, sessionID string, signature [dimensions]float64, projectRoot string) error {
	if w.rdb == nil {
		return nil
	}
	key := fmt.Sprintf("warmstart:sig:%s:%s", projectRoot, sessionID)
	data, _ := json.Marshal(signature)
	return w.rdb.Set(ctx, key, data, 7*24*time.Hour).Err()
}

func cosineSimilarity(a, b [dimensions]float64) float64 {
	var dot, magA, magB float64
	for i := 0; i < dimensions; i++ {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
