package summarizer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	Enabled  bool
	Model    string
	MaxRatio float64
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
		Enabled:  envBoolOr("SUMMARIZER_ENABLED", false),
		Model:    envOr("SUMMARIZER_MODEL", "glm-4.7-flashx"),
		MaxRatio: envFloatOr("SUMMARIZER_MAX_RATIO", 0.3),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type summarizerMetrics struct {
	callsTotal *prometheus.CounterVec
	charsSaved *prometheus.CounterVec
	duration   *prometheus.HistogramVec
	llmTokens  prometheus.Counter
}

type Summarizer struct {
	cfg Config
	rdb *redis.Client
	m   *summarizerMetrics
}

func New(reg prometheus.Registerer, rdb *redis.Client) *Summarizer {
	cfg := LoadConfig()
	m := &summarizerMetrics{
		callsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "summarizer_calls_total", Help: "Summarizer calls by method",
		}, []string{"method"}),
		charsSaved: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "summarizer_chars_saved_total", Help: "Chars saved by summarizer",
		}, []string{"method"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "summarizer_duration_seconds", Help: "Summarizer duration",
		}, []string{"method"}),
		llmTokens: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "summarizer_llm_tokens_total", Help: "Tokens used for LLM summarization",
		}),
	}
	reg.MustRegister(m.callsTotal, m.charsSaved, m.duration, m.llmTokens)
	return &Summarizer{cfg: cfg, rdb: rdb, m: m}
}

// Summarize compresses content. Uses extractive truncation.
func (s *Summarizer) Summarize(ctx context.Context, content string, budgetLevel int) (string, int) {
	h := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(h[:8])

	// Check cache
	if s.rdb != nil {
		cached, err := s.rdb.Get(ctx, "summarizer:cache:"+hash).Result()
		if err == nil && cached != "" {
			s.m.callsTotal.WithLabelValues("cached").Inc()
			saved := len(content) - len(cached)
			s.m.charsSaved.WithLabelValues("cached").Add(float64(saved))
			return cached, saved
		}
	}

	start := time.Now()

	// Extractive summarization: keep first sentence of each paragraph
	result := s.extractiveSummarize(content)

	s.m.callsTotal.WithLabelValues("truncation").Inc()
	s.m.duration.WithLabelValues("truncation").Observe(time.Since(start).Seconds())
	saved := len(content) - len(result)
	s.m.charsSaved.WithLabelValues("truncation").Add(float64(saved))

	// Cache result
	if s.rdb != nil && saved > 0 {
		s.rdb.Set(ctx, "summarizer:cache:"+hash, result, 1*time.Hour)
	}

	return result, saved
}

// IsSummarized checks if content has been previously summarized (cached).
func (s *Summarizer) IsSummarized(ctx context.Context, contentHash string) (string, bool) {
	if s.rdb == nil {
		return "", false
	}
	cached, err := s.rdb.Get(ctx, "summarizer:cache:"+contentHash).Result()
	if err != nil {
		return "", false
	}
	return cached, true
}

func (s *Summarizer) extractiveSummarize(content string) string {
	paragraphs := strings.Split(content, "\n\n")
	var kept []string
	totalLen := 0
	maxLen := int(float64(len(content)) * s.cfg.MaxRatio)

	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Keep first sentence of each paragraph
		sentence := firstSentence(p)
		if totalLen+len(sentence) <= maxLen || len(kept) == 0 {
			kept = append(kept, sentence)
			totalLen += len(sentence)
		}
	}

	if len(kept) == 0 {
		return content
	}
	return strings.Join(kept, "\n\n")
}

func firstSentence(text string) string {
	// Find first sentence-ending punctuation
	for i, ch := range text {
		if ch == '.' || ch == '!' || ch == '?' {
			if i+1 < len(text) && text[i+1] == ' ' {
				return text[:i+1]
			}
		}
	}
	// No sentence boundary found, take first 200 chars
	if len(text) > 200 {
		return text[:200] + "..."
	}
	return text
}
