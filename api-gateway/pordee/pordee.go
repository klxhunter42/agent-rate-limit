package pordee

import (
	"os"
	"regexp"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

// Level controls Thai compression intensity.
type Level int

const (
	Off  Level = iota
	Lite       // Drop politeness, keep grammar
	Full       // Aggressive: fragments OK, ~73% savings
)

func (l Level) String() string {
	return [...]string{"off", "lite", "full"}[l]
}

// Config for pordee Thai compression.
type Config struct {
	Enabled bool
	Level   Level // default level when not auto-detected
}

func envBoolOr(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envLevelOr(key string, fallback Level) Level {
	if v := os.Getenv(key); v != "" {
		switch v {
		case "lite":
			return Lite
		case "full":
			return Full
		}
	}
	return fallback
}

func LoadConfig() Config {
	return Config{
		Enabled: envBoolOr("PORDEE_ENABLED", true),
		Level:   envLevelOr("PORDEE_LEVEL", Full),
	}
}

// thaiRe detects Thai Unicode range (U+0E00 – U+0E7F).
var thaiRe = regexp.MustCompile(`[\x{0E00}-\x{0E7F}]`)

// HasThai returns true if text contains Thai characters.
func HasThai(text string) bool {
	return thaiRe.MatchString(text)
}

// tierInjections maps level to Thai compression rules injected into system prompt.
// These instruct the model to produce terse Thai output from the start (same mechanism as caveman).
var tierInjections = map[Level]string{
	Lite: `
[PORDEE MODE - lite]
ตอบไทยกระชับ. Drop polite particles (ครับ, ค่ะ, นะคะ, นะครับ, ครับผม, จ้า, จ๋า).
Drop hedging (อาจจะ, น่าจะ, จริงๆแล้ว, คงจะ, น่าจะเป็น).
Drop filler (ก็, ก็คือ, อ่ะ, อะ). Use short synonyms.
Keep full grammar. Code/commits/errors: write normal English.`,

	Full: `
[PORDEE MODE - full]
ตอบไทยกระชับมาก. ACTIVE EVERY RESPONSE. No drift.
Drop: polite particles (ครับ, ค่ะ, นะคะ, นะครับ, ครับผม, จ้า, จ๋า),
hedging (อาจจะ, น่าจะ, จริงๆแล้ว, คงจะ), filler (ก็, ก็คือ, อ่ะ),
pleasantries (ได้เลยครับ, แน่นอน, ยินดีครับ), English filler (just/really/basically/actually/simply).
Short synonyms: ดู not ตรวจสอบ, แก้ not ทำการแก้ไข, เพราะ not เนื่องจาก,
ลบ not ทำการลบ, เพิ่ม not ทำการเพิ่ม, เช็ค not ตรวจสอบ, แก้ไข not ดำเนินการแก้ไข.
Fragments OK. Pattern: [ของ] [ทำ] [เหตุผล]. [ขั้นต่อ].
Auto-clarity: drop pordee for security warnings, irreversible actions (DROP TABLE, rm -rf, git push --force),
multi-step sequences where order matters, user asks "อะไรนะ" / "พูดอีกที" / "อธิบายชัดๆ".
Code/commits/PRs/code comments: write normal English. File paths, URLs, identifiers: exact.`,
}

// estimated output ratio per level.
var tierRatios = map[Level]float64{
	Lite: 0.8,
	Full: 0.27,
}

type pordeeMetrics struct {
	injections *prometheus.CounterVec
	ratio      prometheus.Histogram
	duration   prometheus.Histogram
}

// Pipeline is the Thai compression pipeline (pordee = "พอดี" = just right / terse).
type Pipeline struct {
	cfg Config
	m   *pordeeMetrics
}

// New creates a pordee pipeline with Prometheus metrics.
func New(reg prometheus.Registerer) *Pipeline {
	cfg := LoadConfig()
	m := &pordeeMetrics{
		injections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "api_gateway", Name: "pordee_injections_total",
			Help: "Pordee Thai compression injections",
		}, []string{"level", "result"}),
		ratio: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "pordee_output_ratio",
			Help:    "Estimated output ratio after pordee injection",
			Buckets: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
		}),
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "api_gateway", Name: "pordee_duration_seconds",
			Help:    "Pordee injection duration",
			Buckets: prometheus.DefBuckets,
		}),
	}
	reg.MustRegister(m.injections, m.ratio, m.duration)
	return &Pipeline{cfg: cfg, m: m}
}

// ShouldInject returns true and the level if Thai content is detected and pordee is enabled.
// Falls back to config default level when budget-level auto-detection is used.
func (p *Pipeline) ShouldInject(content string, budgetLevel int) (bool, Level) {
	if !p.cfg.Enabled {
		p.m.injections.WithLabelValues("off", "disabled").Inc()
		return false, Off
	}
	if !HasThai(content) {
		p.m.injections.WithLabelValues("off", "no_thai").Inc()
		return false, Off
	}

	// Budget-based level selection: red=full, yellow+=config default.
	level := p.cfg.Level
	if budgetLevel >= 2 {
		level = Full
	}

	p.m.injections.WithLabelValues(level.String(), "valid").Inc()
	return true, level
}

// Inject appends Thai compression rules to the system prompt.
// Returns modified prompt and estimated output ratio.
func (p *Pipeline) Inject(systemPrompt string, level Level) (string, float64) {
	injection, ok := tierInjections[level]
	if !ok {
		injection = tierInjections[Lite]
	}
	ratio := tierRatios[level]

	result := systemPrompt + injection

	p.m.ratio.Observe(ratio)
	return result, ratio
}
