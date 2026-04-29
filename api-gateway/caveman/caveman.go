package caveman

import (
	"os"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

type CompressionTier int

const (
	TierLite CompressionTier = iota
	TierFull
	TierUltra
	TierWenyan
)

func (t CompressionTier) String() string {
	return [...]string{"lite", "full", "ultra", "wenyan"}[t]
}

type Config struct {
	Enabled    bool
	AutoDetect bool
	MinSize    int
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
		Enabled:    envBoolOr("CAVEMAN_ENABLED", false),
		AutoDetect: envBoolOr("CAVEMAN_AUTO_DETECT", true),
		MinSize:    envIntOr("CAVEMAN_MIN_SIZE", 500),
	}
}

var tierInjections = map[CompressionTier]string{
	TierLite: `
[OUTPUT STYLE — lite]
Be concise. Use bullet points. Skip pleasantries and filler phrases.
Avoid: "Great question!", "Certainly!", "I'd be happy to help!", "In summary,", "Hope this helps!".
One sentence answers when possible.`,
	TierFull: `
[OUTPUT STYLE — full]
Be extremely terse. Code only when asked for code. No explanations unless requested.
Avoid all filler, preamble, and summary paragraphs.
Use short variable names in examples. Prefer tables over paragraphs.
If the answer fits in one line, use one line.
Never repeat or paraphrase the question back.`,
	TierUltra: `
[OUTPUT STYLE — ultra]
Raw output only. No natural language wrapper. No markdown formatting unless code.
Use compressed notation: &, |, =>, ternary.
Skip all context setting. Direct answer. No conversational framing.
Maximum compression: abbreviations, symbols, implicit context.
Output MUST be copy-paste ready with zero surrounding prose.`,
	TierWenyan: `
[OUTPUT STYLE — wenyan]
Extreme compression using classical notation style.
Facts only. No grammar glue words. Subject-verb-object minimal.
Use: / for "or", & for "and", -> for "becomes/returns", ? for "if", ! for "not".
Numerical data in compact form. Code in fewest possible lines.`,
}

type cavemanMetrics struct {
	compressions *prometheus.CounterVec
	ratio        prometheus.Histogram
	validateDur  prometheus.Histogram
}

type CavemanPipeline struct {
	cfg Config
	m   *cavemanMetrics
}

func New(reg prometheus.Registerer) *CavemanPipeline {
	cfg := LoadConfig()
	m := &cavemanMetrics{
		compressions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "caveman_compressions_total", Help: "Caveman compression attempts",
		}, []string{"tier", "result"}),
		ratio: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "caveman_compression_ratio", Help: "Estimated compression ratio",
			Buckets: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
		}),
		validateDur: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "caveman_validation_duration_seconds", Help: "Validation duration",
		}),
	}
	reg.MustRegister(m.compressions, m.ratio, m.validateDur)
	return &CavemanPipeline{cfg: cfg, m: m}
}

// ShouldCompress auto-detects if compression is appropriate.
func (c *CavemanPipeline) ShouldCompress(content string, budgetLevel int) (bool, CompressionTier) {
	if !c.cfg.Enabled {
		return false, TierLite
	}
	if len(content) < c.cfg.MinSize {
		c.m.compressions.WithLabelValues("lite", "skipped").Inc()
		return false, TierLite
	}

	if !c.cfg.AutoDetect {
		return true, TierFull
	}

	// Budget-based tier selection
	switch budgetLevel {
	case 2: // Red
		return true, TierUltra
	case 1: // Yellow
		return true, TierFull
	default: // Green
		return true, TierLite
	}
}

// Compress applies the specified compression tier as system prompt injection.
func (c *CavemanPipeline) Compress(systemPrompt string, tier CompressionTier) (string, float64) {
	injection, ok := tierInjections[tier]
	if !ok {
		injection = tierInjections[TierLite]
	}

	result := systemPrompt + injection

	// Estimate compression ratio based on tier
	ratios := map[CompressionTier]float64{
		TierLite:   0.7,
		TierFull:   0.5,
		TierUltra:  0.25,
		TierWenyan: 0.3,
	}
	ratio := ratios[tier]

	c.m.compressions.WithLabelValues(tier.String(), "valid").Inc()
	c.m.ratio.Observe(ratio)
	return result, ratio
}

// Validate checks that compressed output preserves essential structure.
func (c *CavemanPipeline) Validate(original, compressed string) bool {
	// Check that code blocks are preserved
	origBlocks := countCodeBlocks(original)
	compBlocks := countCodeBlocks(compressed)
	if origBlocks > 0 && compBlocks < origBlocks {
		c.m.compressions.WithLabelValues("lite", "invalid").Inc()
		return false
	}

	// Check that key identifiers are still present
	origWords := extractIdentifiers(original)
	compLower := strings.ToLower(compressed)
	preserved := 0
	for _, w := range origWords {
		if strings.Contains(compLower, strings.ToLower(w)) {
			preserved++
		}
	}
	if len(origWords) > 0 && float64(preserved)/float64(len(origWords)) < 0.8 {
		c.m.compressions.WithLabelValues("lite", "invalid").Inc()
		return false
	}

	return true
}

func countCodeBlocks(text string) int {
	return strings.Count(text, "```") / 2
}

func extractIdentifiers(text string) []string {
	words := strings.Fields(text)
	var ids []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?'\"()[]{}<>/\\")
		if len(w) > 3 && isIdentifier(w) {
			ids = append(ids, w)
		}
	}
	// Keep only unique, max 20
	seen := make(map[string]bool, 20)
	var unique []string
	for _, id := range ids {
		if !seen[id] && len(unique) < 20 {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	return unique
}

func isIdentifier(s string) bool {
	hasLetter := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			hasLetter = true
		} else if !((r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return hasLetter
}

// BudgetToTier maps budget level (0=green, 1=yellow, 2=red) to compression tier.
func BudgetToTier(level int) CompressionTier {
	switch level {
	case 2:
		return TierUltra
	case 1:
		return TierFull
	default:
		return TierLite
	}
}
