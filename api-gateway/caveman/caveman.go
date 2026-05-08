package caveman

import (
	"os"
	"regexp"
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
		Enabled:    envBoolOr("CAVEMAN_ENABLED", true),
		AutoDetect: envBoolOr("CAVEMAN_AUTO_DETECT", true),
		MinSize:    envIntOr("CAVEMAN_MIN_SIZE", 500),
	}
}

var tierInjections = map[CompressionTier]string{
	TierLite: `
[OUTPUT STYLE - lite]
Be concise. Use bullet points. Skip pleasantries and filler phrases.
Avoid: "Great question!", "Certainly!", "I'd be happy to help!", "In summary,", "Hope this helps!".
One sentence answers when possible.`,
	TierFull: `
[OUTPUT STYLE - full]
Be extremely terse. Code only when asked for code. No explanations unless requested.
Avoid all filler, preamble, and summary paragraphs.
Use short variable names in examples. Prefer tables over paragraphs.
If the answer fits in one line, use one line.
Never repeat or paraphrase the question back.`,
	TierUltra: `
[OUTPUT STYLE - ultra]
Raw output only. No natural language wrapper. No markdown formatting unless code.
Use compressed notation: &, |, =>, ternary.
Skip all context setting. Direct answer. No conversational framing.
Maximum compression: abbreviations, symbols, implicit context.
Output MUST be copy-paste ready with zero surrounding prose.`,
	TierWenyan: `
[OUTPUT STYLE - wenyan]
Extreme compression using classical notation style.
Facts only. No grammar glue words. Subject-verb-object minimal.
Use: / for "or", & for "and", -> for "becomes/returns", ? for "if", ! for "not".
Numerical data in compact form. Code in fewest possible lines.`,
}

type cavemanMetrics struct {
	compressions *prometheus.CounterVec
	ratio        prometheus.Histogram
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
	}
	reg.MustRegister(m.compressions, m.ratio)
	return &CavemanPipeline{
		cfg: cfg,
		m:   m,
	}
}

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

	switch budgetLevel {
	case 2:
		return true, TierUltra
	case 1:
		return true, TierFull
	default:
		return true, TierLite
	}
}

// Compress appends output-style injection to system prompt.
func (c *CavemanPipeline) Compress(systemPrompt string, tier CompressionTier) (string, float64) {
	injection, ok := tierInjections[tier]
	if !ok {
		injection = tierInjections[TierLite]
	}

	result := systemPrompt + injection

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
	origBlocks := countCodeBlocks(original)
	compBlocks := countCodeBlocks(compressed)
	if origBlocks > 0 && compBlocks < origBlocks {
		c.m.compressions.WithLabelValues("lite", "invalid").Inc()
		return false
	}

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

func stripOuterFence(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	idx := strings.Index(text, "\n")
	if idx < 0 {
		return text
	}
	content := text[idx+1:]
	if strings.HasSuffix(content, "```") {
		return strings.TrimSuffix(content, "```")
	}
	if strings.HasSuffix(content, "```\n") {
		return strings.TrimSuffix(content, "```\n")
	}
	return text
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

// --- Input compression (regex-based) ---

type protectedRegion struct {
	placeholder string
	original    string
}

var (
	maskFencedCode = regexp.MustCompile("(?s)```.*?```")
	maskInlineCode = regexp.MustCompile("`[^`]+`")
	maskURL        = regexp.MustCompile(`https?://[^\s)"'<>]+`)
	maskFilePath   = regexp.MustCompile(`(?:~/|/\w)[\w/.-]*\.\w+`)

	pleasantryRules = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bI'd be happy to\b`),
		regexp.MustCompile(`(?i)\bI'd recommend\b`),
		regexp.MustCompile(`(?i)\bI would recommend\b`),
		regexp.MustCompile(`(?i)\bSure!?\s*`),
		regexp.MustCompile(`(?i)\bCertainly!?\s*`),
		regexp.MustCompile(`(?i)\bOf course!?\s*`),
		regexp.MustCompile(`(?i)\bHappy to help!?\s*`),
		regexp.MustCompile(`(?i)\bHope this helps!?\s*`),
		regexp.MustCompile(`(?i)\bLet me know if\b[^.]*\.`),
		regexp.MustCompile(`(?i)\bFeel free to\b[^.]*\.`),
	}

	instructionFluffRules = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\byou should always\b`),
		regexp.MustCompile(`(?i)\byou should make sure to\b`),
		regexp.MustCompile(`(?i)\bmake sure to\b`),
		regexp.MustCompile(`(?i)\bmake sure you\b`),
		regexp.MustCompile(`(?i)\bremember to\b`),
		regexp.MustCompile(`(?i)\byou need to\b`),
		regexp.MustCompile(`(?i)\bit is important to\b`),
		regexp.MustCompile(`(?i)\bit is important that\b`),
		regexp.MustCompile(`(?i)\byou should\b`),
	}

	synonymRules = []struct {
		re   *regexp.Regexp
		repl string
	}{
		{regexp.MustCompile(`(?i)\bimplement a solution for\b`), "fix"},
		{regexp.MustCompile(`(?i)\bimplement a solution to\b`), "fix"},
		{regexp.MustCompile(`(?i)\butilize\b`), "use"},
		{regexp.MustCompile(`(?i)\butilization\b`), "use"},
		{regexp.MustCompile(`(?i)\bextensive\b`), "big"},
		{regexp.MustCompile(`(?i)\bnumerous\b`), "many"},
		{regexp.MustCompile(`(?i)\bapproximately\b`), "~"},
		{regexp.MustCompile(`(?i)\bsufficient\b`), "enough"},
		{regexp.MustCompile(`(?i)\binitiate\b`), "start"},
		{regexp.MustCompile(`(?i)\bterminate\b`), "end"},
		{regexp.MustCompile(`(?i)\bendeavor\b`), "try"},
		{regexp.MustCompile(`(?i)\bfacilitate\b`), "help"},
		{regexp.MustCompile(`(?i)\bindividuals\b`), "people"},
		{regexp.MustCompile(`(?i)\bregarding\b`), "about"},
		{regexp.MustCompile(`(?i)\btherefore\b`), "so"},
		{regexp.MustCompile(`(?i)\bin addition\b`), "also"},
		{regexp.MustCompile(`(?i)\bthe following\b`), "these"},
		{regexp.MustCompile(`(?i)\bthis is because\b`), "because"},
		{regexp.MustCompile(`(?i)\bthe reason is because\b`), "because"},
		{regexp.MustCompile(`(?i)\bthe reason is that\b`), "because"},
		{regexp.MustCompile(`(?i)\bin order to\b`), "to"},
		{regexp.MustCompile(`(?i)\bfor the purpose of\b`), "to"},
		{regexp.MustCompile(`(?i)\bwill not work properly\b`), "breaks"},
		// "helps" and "prevents" rules removed: corrupts meaning in technical context.
	}

	// Article rules removed: deleting "the"/"a"/"an" corrupts semantics for minimal savings.

	multiSpaceRe   = regexp.MustCompile(`  +`)
	multiNewlineRe = regexp.MustCompile(`\n{3,}`)
)

// CompressInput applies regex-based input text compression.
func (c *CavemanPipeline) CompressInput(text string, tier CompressionTier) (string, int) {
	if text == "" {
		return text, 0
	}

	original := text

	text, regions := maskProtected(text)

	for _, rule := range pleasantryRules {
		text = rule.ReplaceAllString(text, "")
	}

	for _, rule := range instructionFluffRules {
		text = rule.ReplaceAllString(text, "")
	}

	for _, rule := range synonymRules {
		text = rule.re.ReplaceAllString(text, rule.repl)
	}

		// Article removal removed: corrupts semantics in technical text.

	text = unmaskProtected(text, regions)

	text = multiSpaceRe.ReplaceAllString(text, " ")
	text = multiNewlineRe.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)

	saved := len(original) - len(text)
	if saved < 0 {
		saved = 0
	}
	return text, saved
}

func maskProtected(text string) (string, []protectedRegion) {
	var regions []protectedRegion

	mask := func(re *regexp.Regexp, prefix string) {
		matches := re.FindAllString(text, -1)
		for i, m := range matches {
			ph := prefix + "_" + strconv.Itoa(i) + "__"
			regions = append(regions, protectedRegion{placeholder: ph, original: m})
			text = strings.Replace(text, m, ph, 1)
		}
	}

	mask(maskFencedCode, "__FCODE")
	mask(maskInlineCode, "__ICODE")
	mask(maskURL, "__URL")
	mask(maskFilePath, "__FPATH")

	return text, regions
}

func unmaskProtected(text string, regions []protectedRegion) string {
	for _, r := range regions {
		text = strings.Replace(text, r.placeholder, r.original, 1)
	}
	return text
}
