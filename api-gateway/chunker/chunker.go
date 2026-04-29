package chunker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	Enabled         bool
	MinChunk        int
	MaxChunk        int
	WindowSize      int
	StableThreshold int
}

func LoadConfig() Config {
	return Config{
		Enabled:         envBoolOr("CHUNKER_ENABLED", true),
		MinChunk:        envIntOr("CHUNKER_MIN_CHUNK", 128),
		MaxChunk:        envIntOr("CHUNKER_MAX_CHUNK", 4096),
		WindowSize:      envIntOr("CHUNKER_WINDOW_SIZE", 48),
		StableThreshold: envIntOr("CHUNKER_STABLE_THRESHOLD", 2),
	}
}

type Chunk struct {
	Hash     string
	Content  string
	IsStable bool
}

type chunkerMetrics struct {
	chunksTotal     *prometheus.CounterVec
	reorderDuration prometheus.Histogram
	cacheHitRate    prometheus.Gauge
	charsSaved      prometheus.Counter
}

type Chunker struct {
	cfg Config
	rdb *redis.Client
	m   *chunkerMetrics
}

func New(reg prometheus.Registerer, rdb *redis.Client) *Chunker {
	cfg := LoadConfig()
	m := &chunkerMetrics{
		chunksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "chunker_chunks_total", Help: "Chunks created by type",
		}, []string{"type"}),
		reorderDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "chunker_reorder_duration_seconds", Help: "Reorder duration",
		}),
		cacheHitRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "api_gateway", Name: "chunker_cache_hit_rate", Help: "Stable chunk hit rate",
		}),
		charsSaved: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "chunker_chars_saved_total", Help: "Chars saved by dedup reorder",
		}),
	}
	reg.MustRegister(m.chunksTotal, m.reorderDuration, m.cacheHitRate, m.charsSaved)
	return &Chunker{cfg: cfg, rdb: rdb, m: m}
}

// ChunkAndReorder splits content into chunks using Rabin-Karp rolling hash,
// reorders stable (previously seen) chunks first, returns reordered content and chars saved.
func (c *Chunker) ChunkAndReorder(ctx context.Context, content string) (string, int) {
	start := time.Now()
	chunks := c.chunk(content)

	stableCount := 0
	for i := range chunks {
		if c.isStable(ctx, chunks[i].Hash) {
			chunks[i].IsStable = true
			stableCount++
		}
		c.recordChunk(ctx, chunks[i].Hash)
	}

	// Reorder: stable first, novel last
	reordered := make([]Chunk, 0, len(chunks))
	for _, ch := range chunks {
		if ch.IsStable {
			reordered = append(reordered, ch)
		}
	}
	for _, ch := range chunks {
		if !ch.IsStable {
			reordered = append(reordered, ch)
		}
	}

	c.m.chunksTotal.WithLabelValues("stable").Add(float64(stableCount))
	c.m.chunksTotal.WithLabelValues("novel").Add(float64(len(chunks) - stableCount))
	if len(chunks) > 0 {
		c.m.cacheHitRate.Set(float64(stableCount) / float64(len(chunks)))
	}
	c.m.reorderDuration.Observe(time.Since(start).Seconds())

	// Rebuild content
	var result string
	for _, ch := range reordered {
		result += ch.Content
	}
	saved := len(content) - len(result)
	if saved < 0 {
		saved = 0
	}
	c.m.charsSaved.Add(float64(saved))
	return result, saved
}

func (c *Chunker) chunk(content string) []Chunk {
	if len(content) < c.cfg.MinChunk {
		h := sha256.Sum256([]byte(content))
		return []Chunk{{Hash: hex.EncodeToString(h[:12]), Content: content}}
	}

	var chunks []Chunk
	start := 0
	window := c.cfg.WindowSize
	boundaryMod := uint64(256)

	for i := window; i < len(content); i++ {
		if i-start >= c.cfg.MaxChunk {
			boundary := content[start:i]
			h := sha256.Sum256([]byte(boundary))
			chunks = append(chunks, Chunk{Hash: hex.EncodeToString(h[:12]), Content: boundary})
			start = i
			continue
		}
		if i-start < c.cfg.MinChunk {
			continue
		}

		// Simple rolling hash over window
		var hash uint64
		for j := i - window; j < i; j++ {
			hash = hash*31 + uint64(content[j])
		}
		if hash%boundaryMod == 0 {
			boundary := content[start:i]
			h := sha256.Sum256([]byte(boundary))
			chunks = append(chunks, Chunk{Hash: hex.EncodeToString(h[:12]), Content: boundary})
			start = i
		}
	}

	if start < len(content) {
		rem := content[start:]
		h := sha256.Sum256([]byte(rem))
		chunks = append(chunks, Chunk{Hash: hex.EncodeToString(h[:12]), Content: rem})
	}
	return chunks
}

func (c *Chunker) isStable(ctx context.Context, hash string) bool {
	key := fmt.Sprintf("chunker:stable:%s", hash)
	n, err := c.rdb.Get(ctx, key).Int()
	if err != nil {
		return false
	}
	return n >= c.cfg.StableThreshold
}

func (c *Chunker) recordChunk(ctx context.Context, hash string) {
	key := fmt.Sprintf("chunker:stable:%s", hash)
	c.rdb.Incr(ctx, key)
	c.rdb.Expire(ctx, key, 24*time.Hour)
}

func (c *Chunker) Close() {}
