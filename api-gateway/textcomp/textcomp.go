package textcomp

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Config controls textcomp behavior.
type Config struct {
	Enabled bool
	Mode    string // "balanced", "aggressive"
}

func envBoolOr(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func LoadConfig() Config {
	return Config{
		Enabled: envBoolOr("TEXTCOMP_ENABLED", true),
		Mode:    envOr("TEXTCOMP_MODE", "balanced"),
	}
}

// protectedRegion is a masked placeholder for content that must not be modified.
type protectedRegion struct {
	placeholder string
	original    string
}

var (
	// Protected region patterns (applied in order).
	fencedCodeRe = regexp.MustCompile("(?s)```.*?```")
	inlineCodeRe = regexp.MustCompile("`[^`]+`")
	urlRe        = regexp.MustCompile(`https?://[^\s)"'<>]+`)
	quotedStrRe  = regexp.MustCompile(`"[^"]{3,}"`)

	// Filler phrases - remove entirely.
	fillerRules = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bI would like to\b`),
		regexp.MustCompile(`(?i)\bCould you please\b`),
		regexp.MustCompile(`(?i)\bI was wondering if\b`),
		regexp.MustCompile(`(?i)\bIf you could\b`),
		regexp.MustCompile(`(?i)\bIt would be great if\b`),
		regexp.MustCompile(`(?i)\bPlease let me know\b`),
		regexp.MustCompile(`(?i)\bI'd like to\b`),
		regexp.MustCompile(`(?i)\bI want you to\b`),
		regexp.MustCompile(`(?i)\bCan you please\b`),
		regexp.MustCompile(`(?i)\bKindly\b`),
	}

	// Hedge words - remove entirely.
	hedgeRules = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bsort of\b`),
		regexp.MustCompile(`(?i)\bkind of\b`),
		regexp.MustCompile(`(?i)\ba little bit\b`),
		regexp.MustCompile(`(?i)\bsomewhat\b`),
		regexp.MustCompile(`(?i)\bquite\b`),
		regexp.MustCompile(`(?i)\brather\b`),
		regexp.MustCompile(`(?i)\bbasically\b`),
		regexp.MustCompile(`(?i)\bactually\b`),
		regexp.MustCompile(`(?i)\bliterally\b`),
		regexp.MustCompile(`(?i)\bjust\b`),
		regexp.MustCompile(`(?i)\breally\b`),
		regexp.MustCompile(`(?i)\bvery\s+very\b`),
	}

	// Verbose-to-compact replacements.
	verboseRules = []struct {
		re   *regexp.Regexp
		repl string
	}{
		{regexp.MustCompile(`(?i)\bdue to the fact that\b`), "because"},
		{regexp.MustCompile(`(?i)\bin order to\b`), "to"},
		{regexp.MustCompile(`(?i)\bfor the purpose of\b`), "to"},
		{regexp.MustCompile(`(?i)\bat this point in time\b`), "now"},
		{regexp.MustCompile(`(?i)\bin the event that\b`), "if"},
		{regexp.MustCompile(`(?i)\bprior to\b`), "before"},
		{regexp.MustCompile(`(?i)\bsubsequent to\b`), "after"},
		{regexp.MustCompile(`(?i)\bwith regard to\b`), "about"},
		{regexp.MustCompile(`(?i)\bwith respect to\b`), "about"},
		{regexp.MustCompile(`(?i)\bin accordance with\b`), "per"},
		{regexp.MustCompile(`(?i)\bmake a decision\b`), "decide"},
		{regexp.MustCompile(`(?i)\bcarry out\b`), "do"},
		{regexp.MustCompile(`(?i)\btake into consideration\b`), "consider"},
		{regexp.MustCompile(`(?i)\bin the context of\b`), "in"},
		{regexp.MustCompile(`(?i)\bfor the reason that\b`), "because"},
		{regexp.MustCompile(`(?i)\bby means of\b`), "by"},
		{regexp.MustCompile(`(?i)\bon the basis of\b`), "based on"},
		{regexp.MustCompile(`(?i)\ba large number of\b`), "many"},
		{regexp.MustCompile(`(?i)\ba significant number of\b`), "many"},
		{regexp.MustCompile(`(?i)\bat the present time\b`), "now"},
		{regexp.MustCompile(`(?i)\bhas the ability to\b`), "can"},
		{regexp.MustCompile(`(?i)\bis able to\b`), "can"},
		{regexp.MustCompile(`(?i)\bin spite of the fact that\b`), "although"},
		{regexp.MustCompile(`(?i)\bnotwithstanding the fact that\b`), "despite"},
		{regexp.MustCompile(`(?i)\bwith the exception of\b`), "except"},
		{regexp.MustCompile(`(?i)\bIn addition,?\b`), "Also"},
		{regexp.MustCompile(`(?i)\bFurthermore,?\b`), "Also"},
		{regexp.MustCompile(`(?i)\bHowever,?\b`), "But"},
		{regexp.MustCompile(`(?i)\bTherefore,?\b`), "So"},
		{regexp.MustCompile(`(?i)\bNevertheless,?\b`), "But"},
		{regexp.MustCompile(`(?i)\bconsequently\b`), "so"},
	}

	// Aggressive-only rules.
	aggressiveRules = []struct {
		re   *regexp.Regexp
		repl string
	}{
		{regexp.MustCompile(`(?i)\bAs a matter of fact,?\b`), ""},
		{regexp.MustCompile(`(?i)\bIt is worth noting that\b`), ""},
		{regexp.MustCompile(`(?i)\bIt is important to note that\b`), ""},
		{regexp.MustCompile(`(?i)\bPlease note that\b`), ""},
		{regexp.MustCompile(`(?i)\bNeedless to say,?\b`), ""},
		{regexp.MustCompile(`(?i)\bIt goes without saying that\b`), ""},
		{regexp.MustCompile(`(?i)\bIn my opinion,?\b`), ""},
		{regexp.MustCompile(`(?i)\bI think that\b`), ""},
		{regexp.MustCompile(`(?i)\bI believe that\b`), ""},
		{regexp.MustCompile(`(?i)\bIt seems that\b`), ""},
		{regexp.MustCompile(`(?i)\bThe reason is that\b`), "Because"},
	}

	// Cleanup patterns.
	multiSpaceRe   = regexp.MustCompile(`  +`)
	multiNewlineRe = regexp.MustCompile(`\n{3,}`)
)

// TextComp performs regex-based text compression.
type TextComp struct {
	cfg Config
}

// New creates a new TextComp instance.
func New(cfg Config) *TextComp {
	return &TextComp{cfg: cfg}
}

// Compress applies the mask-apply-unmask compression pipeline.
// Returns the compressed text and estimated character savings.
func (tc *TextComp) Compress(text string) (string, int) {
	if !tc.cfg.Enabled || text == "" {
		return text, 0
	}

	original := text

	// Phase 1: Mask protected regions.
	text, regions := tc.mask(text)

	// Phase 2: Apply compression rules.
	text = tc.applyRules(text)

	// Phase 3: Unmask protected regions.
	text = tc.unmask(text, regions)

	// Phase 4: Final cleanup.
	text = cleanup(text)

	saved := len(original) - len(text)
	if saved < 0 {
		saved = 0
	}
	return text, saved
}

func (tc *TextComp) mask(text string) (string, []protectedRegion) {
	var regions []protectedRegion

	mask := func(re *regexp.Regexp, prefix string) {
		matches := re.FindAllString(text, -1)
		for i, m := range matches {
			ph := prefix + "_" + strconv.Itoa(i) + "__"
			regions = append(regions, protectedRegion{placeholder: ph, original: m})
			text = strings.Replace(text, m, ph, 1)
		}
	}

	mask(fencedCodeRe, "__FCODE")
	mask(inlineCodeRe, "__ICODE")
	mask(urlRe, "__URL")
	mask(quotedStrRe, "__QSTR")

	return text, regions
}

func (tc *TextComp) applyRules(text string) string {
	// Remove filler phrases.
	for _, rule := range fillerRules {
		text = rule.ReplaceAllString(text, "")
	}

	// Remove hedge words.
	for _, rule := range hedgeRules {
		text = rule.ReplaceAllString(text, "")
	}

	// Apply verbose-to-compact replacements.
	for _, rule := range verboseRules {
		text = rule.re.ReplaceAllString(text, rule.repl)
	}

	// Apply aggressive-only rules.
	if tc.cfg.Mode == "aggressive" {
		for _, rule := range aggressiveRules {
			text = rule.re.ReplaceAllString(text, rule.repl)
		}
	}

	return text
}

func (tc *TextComp) unmask(text string, regions []protectedRegion) string {
	for _, r := range regions {
		text = strings.Replace(text, r.placeholder, r.original, 1)
	}
	return text
}

func cleanup(text string) string {
	text = multiSpaceRe.ReplaceAllString(text, " ")
	text = multiNewlineRe.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)
	return text
}
