package sketch

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	Enabled    bool
	Dimensions int
	Threshold  float64
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
		Enabled:    envBoolOr("SKETCH_ENABLED", false),
		Dimensions: envIntOr("SKETCH_DIMENSIONS", 128),
		Threshold:  envFloatOr("SKETCH_THRESHOLD", 0.85),
	}
}

type sketchMetrics struct {
	checksTotal    *prometheus.CounterVec
	hammingSimilar prometheus.Histogram
	charsSaved     prometheus.Counter
}

type Sketch struct {
	cfg Config
	rdb *redis.Client
	m   *sketchMetrics
}

func New(reg prometheus.Registerer, rdb *redis.Client) *Sketch {
	cfg := LoadConfig()
	m := &sketchMetrics{
		checksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "sketch_checks_total", Help: "Sketch duplicate checks",
		}, []string{"result"}),
		hammingSimilar: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "sketch_hamming_similarity", Help: "Hamming similarity between sketches",
			Buckets: []float64{0, 0.1, 0.3, 0.5, 0.7, 0.85, 0.9, 0.95, 1.0},
		}),
		charsSaved: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "sketch_chars_saved_total", Help: "Chars saved by dedup",
		}),
	}
	reg.MustRegister(m.checksTotal, m.hammingSimilar, m.charsSaved)
	return &Sketch{cfg: cfg, rdb: rdb, m: m}
}

// Compute generates a 1-bit sketch for content.
func (s *Sketch) Compute(content string) []byte {
	dims := s.cfg.Dimensions
	words := tokenizeWords(content)
	if len(words) == 0 {
		return make([]byte, (dims+7)/8)
	}

	// FNV-1a hash per word, accumulate into bit vector
	sketch := make([]byte, (dims+7)/8)
	for _, w := range words {
		h := fnv1a(w)
		// Use hash to set bits at multiple positions
		for k := 0; k < 3; k++ {
			pos := int((h >> uint(k*8)) % uint32(dims))
			byteIdx := pos / 8
			bitIdx := uint(pos % 8)
			if int(byteIdx) < len(sketch) {
				sketch[byteIdx] |= 1 << bitIdx
			}
		}
	}
	return sketch
}

// Similarity computes Hamming similarity between two sketches (0-1).
func (s *Sketch) Similarity(a, b []byte) float64 {
	if len(a) != len(b) {
		return 0
	}
	totalBits := len(a) * 8
	if totalBits == 0 {
		return 1
	}
	sameBits := 0
	for i := range a {
		xor := a[i] ^ b[i]
		sameBits += 8 - popcount(xor)
	}
	return float64(sameBits) / float64(totalBits)
}

// CheckAndStore computes sketch for content, checks for near-duplicates,
// and stores it. Returns (isDuplicate, similarSessionID, charsSaved).
func (s *Sketch) CheckAndStore(ctx context.Context, sessionID, content string) (bool, string, int) {
	sketch := s.Compute(content)
	sketchHex := hex.EncodeToString(sketch)

	// Check against recent sketches
	key := fmt.Sprintf("sketch:recent:%s", sessionID)
	recent, _ := s.rdb.LRange(ctx, key, 0, -1).Result()

	for _, prevSketchHex := range recent {
		prevSketch, err := hex.DecodeString(prevSketchHex)
		if err != nil || len(prevSketch) != len(sketch) {
			continue
		}
		sim := s.Similarity(sketch, prevSketch)
		s.m.hammingSimilar.Observe(sim)
		if sim >= s.cfg.Threshold {
			s.m.checksTotal.WithLabelValues("duplicate").Inc()
			saved := len(content)
			s.m.charsSaved.Add(float64(saved))
			return true, sessionID, saved
		}
	}

	// Store sketch
	s.rdb.RPush(ctx, key, sketchHex)
	s.rdb.LTrim(ctx, key, -100, -1)
	s.rdb.Expire(ctx, key, 24*time.Hour)

	s.m.checksTotal.WithLabelValues("unique").Inc()
	return false, "", 0
}

func tokenizeWords(text string) []string {
	var words []string
	var b [64]byte
	bi := 0
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			if bi < len(b) {
				b[bi] = byte(r)
				bi++
			}
		} else {
			if bi > 0 {
				words = append(words, string(b[:bi]))
				bi = 0
			}
		}
	}
	if bi > 0 {
		words = append(words, string(b[:bi]))
	}
	return words
}

func fnv1a(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func popcount(b byte) int {
	count := 0
	for b != 0 {
		count += int(b & 1)
		b >>= 1
	}
	return count
}
