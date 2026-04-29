package packer

import (
	"sort"

	"github.com/prometheus/client_golang/prometheus"
)

type Item struct {
	ID      string
	Content string
	Tokens  int
	Utility float64
}

type Config struct {
	Enabled    bool
	MinUtility float64
}

func LoadConfig() Config {
	return Config{
		Enabled:    envBoolOr("PACKER_ENABLED", true),
		MinUtility: envFloatOr("PACKER_MIN_UTILITY", 0.1),
	}
}

type packerMetrics struct {
	itemsPacked *prometheus.CounterVec
	budgetUtil  prometheus.Gauge
	tokensSaved prometheus.Counter
}

type Packer struct {
	cfg Config
	m   *packerMetrics
}

func New(reg prometheus.Registerer) *Packer {
	cfg := LoadConfig()
	m := &packerMetrics{
		itemsPacked: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "packer_items_packed_total", Help: "Items packed or excluded",
		}, []string{"result"}),
		budgetUtil: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "api_gateway", Name: "packer_budget_utilization", Help: "Token budget utilization",
		}),
		tokensSaved: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "packer_tokens_saved_total", Help: "Tokens saved by exclusion",
		}),
	}
	reg.MustRegister(m.itemsPacked, m.budgetUtil, m.tokensSaved)
	return &Packer{cfg: cfg, m: m}
}

// Pack selects items within tokenBudget using greedy 0/1 knapsack.
// Items below MinUtility are excluded. Returns selected items and chars saved from excluded items.
func (p *Packer) Pack(items []Item, tokenBudget int) ([]Item, int) {
	// Filter by minimum utility
	var eligible []Item
	for _, it := range items {
		if it.Utility >= p.cfg.MinUtility && it.Tokens > 0 {
			eligible = append(eligible, it)
		}
	}

	// Sort by utility/token ratio descending
	sort.Slice(eligible, func(i, j int) bool {
		ri := eligible[i].Utility / float64(eligible[i].Tokens)
		rj := eligible[j].Utility / float64(eligible[j].Tokens)
		return ri > rj
	})

	var selected []Item
	usedTokens := 0
	excludedChars := 0

	for _, it := range eligible {
		if usedTokens+it.Tokens <= tokenBudget {
			selected = append(selected, it)
			usedTokens += it.Tokens
		} else {
			excludedChars += len(it.Content)
		}
	}

	// Count items excluded from original list
	excludedByUtility := len(items) - len(eligible)
	p.m.itemsPacked.WithLabelValues("included").Add(float64(len(selected)))
	p.m.itemsPacked.WithLabelValues("excluded").Add(float64(len(eligible) - len(selected) + excludedByUtility))
	if tokenBudget > 0 {
		p.m.budgetUtil.Set(float64(usedTokens) / float64(tokenBudget))
	}
	p.m.tokensSaved.Add(float64(excludedChars))

	return selected, excludedChars
}
