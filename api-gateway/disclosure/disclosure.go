package disclosure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

type Layer int

const (
	LayerIndex Layer = iota
	LayerFTS
	LayerFull
)

type Config struct {
	Enabled  bool
	L1Tokens int
	L2Tokens int
}

func LoadConfig() Config {
	return Config{
		Enabled:  envBoolOr("DISCLOSURE_ENABLED", true),
		L1Tokens: envIntOr("DISCLOSURE_L1_TOKENS", 15),
		L2Tokens: envIntOr("DISCLOSURE_L2_TOKENS", 60),
	}
}

type disclosureMetrics struct {
	escalations *prometheus.CounterVec
	charsSaved  prometheus.Counter
	ftsHitRate  prometheus.Gauge
}

type Disclosure struct {
	cfg Config
	rdb *redis.Client
	m   *disclosureMetrics
}

func New(reg prometheus.Registerer, rdb *redis.Client) *Disclosure {
	cfg := LoadConfig()
	m := &disclosureMetrics{
		escalations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "disclosure_escalations_total", Help: "Disclosure layer escalations",
		}, []string{"layer"}),
		charsSaved: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "disclosure_chars_saved_total", Help: "Chars saved by progressive disclosure",
		}),
		ftsHitRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "api_gateway", Name: "disclosure_fts_hit_rate", Help: "FTS layer hit rate",
		}),
	}
	reg.MustRegister(m.escalations, m.charsSaved, m.ftsHitRate)
	return &Disclosure{cfg: cfg, rdb: rdb, m: m}
}

// Escalate returns content at the minimum layer that matches the query.
// Layer 1: first ~L1Tokens worth of content (heading/summary).
// Layer 2: keyword-matched excerpts within L2Tokens budget.
// Layer 3: full content (fallback).
func (d *Disclosure) Escalate(ctx context.Context, content, query string, maxTokens int) (string, Layer, int) {
	fullLen := len(content)

	// Layer 1: index (first ~L1Tokens chars, ~4 chars/token)
	l1Budget := d.cfg.L1Tokens * 4
	if l1Budget > fullLen {
		l1Budget = fullLen
	}
	l1Content := content
	if fullLen > l1Budget {
		l1Content = content[:l1Budget]
	}

	if query == "" {
		d.m.escalations.WithLabelValues("1").Inc()
		saved := fullLen - len(l1Content)
		d.m.charsSaved.Add(float64(saved))
		return l1Content, LayerIndex, saved
	}

	// Layer 2: FTS match within budget
	l2Budget := d.cfg.L2Tokens * 4
	matched := d.ftsExtract(content, query, l2Budget)
	if matched != "" {
		d.m.escalations.WithLabelValues("2").Inc()
		d.m.ftsHitRate.Set(1)
		saved := fullLen - len(matched)
		d.m.charsSaved.Add(float64(saved))
		return matched, LayerFTS, saved
	}

	d.m.ftsHitRate.Set(0)
	d.m.escalations.WithLabelValues("3").Inc()
	return content, LayerFull, 0
}

// StoreLayer pre-computes and caches a keyword index for content.
func (d *Disclosure) StoreLayer(ctx context.Context, id, content string) error {
	h := sha256.Sum256([]byte(content))
	key := "disclosure:idx:" + hex.EncodeToString(h[:8])

	words := strings.Fields(strings.ToLower(content))
	seen := make(map[string]bool, len(words))
	var unique []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?'\"()[]{}")
		if len(w) > 2 && !seen[w] {
			seen[w] = true
			unique = append(unique, w)
		}
	}
	if len(unique) > 0 {
		return d.rdb.Set(ctx, key, strings.Join(unique, " "), 1*time.Hour).Err()
	}
	return nil
}

func (d *Disclosure) ftsExtract(content, query string, budget int) string {
	queryWords := strings.Fields(strings.ToLower(query))
	paragraphs := strings.Split(content, "\n\n")
	var matched []string
	totalLen := 0

	for _, p := range paragraphs {
		if totalLen >= budget {
			break
		}
		lower := strings.ToLower(p)
		for _, qw := range queryWords {
			if strings.Contains(lower, qw) {
				if totalLen+len(p) <= budget {
					matched = append(matched, p)
					totalLen += len(p)
				}
				break
			}
		}
	}
	return strings.Join(matched, "\n\n")
}

// BudgetAwareEscalate applies progressive disclosure based on budget level.
// Green: pass through. Yellow: L2 for large content (>2000 chars). Red: L1 for >1000, L2 for 500-1000.
func (d *Disclosure) BudgetAwareEscalate(ctx context.Context, content string, budgetLevel int) (string, int) {
	fullLen := len(content)
	if fullLen == 0 || !d.cfg.Enabled {
		return content, 0
	}

	switch budgetLevel {
	case 0: // green - pass through
		return content, 0
	case 1: // yellow - L2 for large content
		if fullLen > 2000 {
			l2Budget := d.cfg.L2Tokens * 8
			if l2Budget > fullLen {
				l2Budget = fullLen
			}
			truncated := content[:l2Budget]
			saved := fullLen - len(truncated)
			return truncated, saved
		}
		return content, 0
	case 2: // red - aggressive truncation
		if fullLen > 1000 {
			l1Budget := d.cfg.L1Tokens * 4
			if l1Budget > fullLen {
				l1Budget = fullLen
			}
			truncated := content[:l1Budget]
			saved := fullLen - len(truncated)
			return truncated, saved
		}
		if fullLen > 500 {
			l2Budget := d.cfg.L2Tokens * 6
			if l2Budget > fullLen {
				l2Budget = fullLen
			}
			truncated := content[:l2Budget]
			saved := fullLen - len(truncated)
			return truncated, saved
		}
		return content, 0
	default:
		return content, 0
	}
}

