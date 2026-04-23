package tokenizer

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
)

// ContentType represents detected content type for token estimation.
type ContentType int

const (
	ContentCode ContentType = iota
	ContentJSON
	ContentMarkdown
	ContentText
)

var (
	codeIndicators     = regexp.MustCompile(`(?i)^\s*(import|package|from|def|class|function|const|let|var|func|type|struct|interface|module|require|return|if |for |while |switch |case |pub |fn |use |mod |go )`)
	markdownIndicators = regexp.MustCompile(`^#{1,6}\s|^\s*[-*+]\s|^\s*\d+\.\s|^\s*>\s|^` + "```" + `|^\|`)
	sentenceEnd        = regexp.MustCompile(`[.!?]\s+`)
	fencePrefix        = "```"
	// Privacy placeholders injected by privacy.MaskRequest - never modify or split these.
	privacyPlaceholder = regexp.MustCompile(`__(SECRET|PII)_\d+__`)
)

// charsPerToken ratios calibrated from tiktoken analysis.
var charsPerToken = map[ContentType]float64{
	ContentCode:     2.5,
	ContentJSON:     2.8,
	ContentMarkdown: 3.5,
	ContentText:     4.0,
}

// DetectContentType classifies text by content type.
func DetectContentType(text string) ContentType {
	lines := strings.Split(text, "\n")
	nonEmpty := 0
	codeLines := 0
	mdLines := 0

	trimmed := strings.TrimSpace(text)
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		return ContentJSON
	}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		nonEmpty++
		if codeIndicators.MatchString(line) {
			codeLines++
		}
		if markdownIndicators.MatchString(line) {
			mdLines++
		}
	}

	if nonEmpty == 0 {
		return ContentText
	}

	codeRatio := float64(codeLines) / float64(nonEmpty)
	mdRatio := float64(mdLines) / float64(nonEmpty)

	if codeRatio > 0.3 {
		return ContentCode
	}
	if mdRatio > 0.2 {
		return ContentMarkdown
	}
	return ContentText
}

// EstimateTokens estimates token count using content-aware heuristic.
func EstimateTokens(text string) int {
	ct := DetectContentType(text)
	ratio := charsPerToken[ct]
	est := float64(len(text)) / ratio
	return int(math.Ceil(est))
}

// QuickEstimateTokens uses chars/4 for ultra-fast estimation.
func QuickEstimateTokens(text string) int {
	return (len(text) + 3) / 4
}

// ModelCapabilities holds static model limits.
type ModelCapabilities struct {
	ContextWindow   int
	MaxOutputTokens int
	Provider        string
}

// KnownModels is the static model capability map.
var KnownModels = map[string]ModelCapabilities{
	// Anthropic
	"claude-opus-4-7":            {ContextWindow: 200000, MaxOutputTokens: 163840, Provider: "anthropic"},
	"claude-sonnet-4-6":          {ContextWindow: 200000, MaxOutputTokens: 163840, Provider: "anthropic"},
	"claude-haiku-4-5-20251001":  {ContextWindow: 200000, MaxOutputTokens: 8192, Provider: "anthropic"},
	"claude-3-5-sonnet-20241022": {ContextWindow: 200000, MaxOutputTokens: 8192, Provider: "anthropic"},
	"claude-3-5-haiku-20241022":  {ContextWindow: 200000, MaxOutputTokens: 8192, Provider: "anthropic"},
	// OpenAI
	"gpt-4o":      {ContextWindow: 128000, MaxOutputTokens: 16384, Provider: "openai"},
	"gpt-4o-mini": {ContextWindow: 128000, MaxOutputTokens: 16384, Provider: "openai"},
	"gpt-4-turbo": {ContextWindow: 128000, MaxOutputTokens: 4096, Provider: "openai"},
	"o1":          {ContextWindow: 200000, MaxOutputTokens: 100000, Provider: "openai"},
	"o1-mini":     {ContextWindow: 128000, MaxOutputTokens: 65536, Provider: "openai"},
	"o3-mini":     {ContextWindow: 200000, MaxOutputTokens: 100000, Provider: "openai"},
	// Gemini
	"gemini-2.5-pro":   {ContextWindow: 1048576, MaxOutputTokens: 65536, Provider: "google"},
	"gemini-2.5-flash": {ContextWindow: 1048576, MaxOutputTokens: 65536, Provider: "google"},
	"gemini-2.0-flash": {ContextWindow: 1048576, MaxOutputTokens: 8192, Provider: "google"},
	// Z.AI (GLM)
	"glm-5.1":      {ContextWindow: 128000, MaxOutputTokens: 4096, Provider: "zai"},
	"glm-5":        {ContextWindow: 128000, MaxOutputTokens: 4096, Provider: "zai"},
	"glm-4.6v":     {ContextWindow: 8192, MaxOutputTokens: 4096, Provider: "zai"},
	"glm-4-plus":   {ContextWindow: 128000, MaxOutputTokens: 4096, Provider: "zai"},
	"glm-4-flash":  {ContextWindow: 128000, MaxOutputTokens: 4096, Provider: "zai"},
	"glm-4-0520":   {ContextWindow: 128000, MaxOutputTokens: 4096, Provider: "zai"},
	"glm-4v-flash": {ContextWindow: 8192, MaxOutputTokens: 4096, Provider: "zai"},
}

// GetModelCapabilities returns capabilities for a model, with fallback defaults.
func GetModelCapabilities(model string) ModelCapabilities {
	if cap, ok := KnownModels[model]; ok {
		return cap
	}
	for k, v := range KnownModels {
		if strings.HasPrefix(model, k) {
			return v
		}
	}
	return ModelCapabilities{ContextWindow: 128000, MaxOutputTokens: 4096, Provider: "unknown"}
}

// UpdateMaxOutputTokens updates MaxOutputTokens for an existing model or adds a new entry.
func UpdateMaxOutputTokens(model string, tokens int) {
	if cap, ok := KnownModels[model]; ok {
		cap.MaxOutputTokens = tokens
		KnownModels[model] = cap
	} else {
		KnownModels[model] = ModelCapabilities{MaxOutputTokens: tokens}
	}
}

// OptimizeWhitespace collapses whitespace in prose while preserving code blocks.
func OptimizeWhitespace(text string) (string, int) {
	origTokens := QuickEstimateTokens(text)
	segments := splitCodeBlocks(text)
	var b strings.Builder
	for _, seg := range segments {
		if seg.isCode {
			b.WriteString(seg.text)
			continue
		}
		b.WriteString(optimizeProseWhitespace(seg.text))
	}
	result := b.String()
	saved := origTokens - QuickEstimateTokens(result)
	return result, saved
}

type textSegment struct {
	text   string
	isCode bool
}

func splitCodeBlocks(text string) []textSegment {
	var segments []textSegment
	inCode := false
	var b strings.Builder
	lines := strings.Split(text, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inCode && strings.HasPrefix(trimmed, fencePrefix) {
			if b.Len() > 0 {
				segments = append(segments, textSegment{text: b.String(), isCode: false})
				b.Reset()
			}
			inCode = true
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		if inCode {
			b.WriteString(line)
			b.WriteByte('\n')
			if strings.HasPrefix(trimmed, fencePrefix) {
				segments = append(segments, textSegment{text: b.String(), isCode: true})
				b.Reset()
				inCode = false
			}
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}

	if b.Len() > 0 {
		segments = append(segments, textSegment{text: b.String(), isCode: inCode})
	}
	return segments
}

func optimizeProseWhitespace(text string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range text {
		if r == ' ' || r == '\t' {
			if prevSpace {
				continue
			}
			prevSpace = true
			b.WriteByte(' ')
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}

	result := b.String()
	lines := strings.Split(result, "\n")
	var out strings.Builder
	blankCount := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blankCount++
			if blankCount <= 2 {
				out.WriteByte('\n')
			}
			continue
		}
		blankCount = 0
		out.WriteString(strings.TrimRight(line, " \t"))
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String())
}

// DeduplicateSentences removes duplicate sentences from text.
// Skips dedup if privacy placeholders are present to avoid corrupting them.
func DeduplicateSentences(text string) (string, int) {
	if privacyPlaceholder.MatchString(text) {
		return text, 0
	}
	origTokens := QuickEstimateTokens(text)
	segments := splitCodeBlocks(text)
	var b strings.Builder
	for _, seg := range segments {
		if seg.isCode {
			b.WriteString(seg.text)
			b.WriteByte('\n')
			continue
		}
		b.WriteString(dedupProse(seg.text))
		b.WriteByte('\n')
	}
	result := strings.TrimSpace(b.String())
	saved := origTokens - QuickEstimateTokens(result)
	return result, saved
}

func dedupProse(text string) string {
	sentences := splitSentences(text)
	seen := make(map[string]bool, len(sentences))
	var b strings.Builder
	for _, s := range sentences {
		normalized := normalizeForDedup(s)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		b.WriteString(s)
	}
	return b.String()
}

func splitSentences(text string) []string {
	// Don't split text containing privacy placeholders to avoid breaking them.
	if privacyPlaceholder.MatchString(text) {
		return []string{text}
	}
	parts := sentenceEnd.Split(text, -1)
	if len(parts) <= 1 {
		return []string{text}
	}
	var result []string
	pos := 0
	for _, part := range parts {
		if part == "" {
			continue
		}
		end := pos + len(part)
		if end > len(text) {
			end = len(text)
		}
		if end < len(text) {
			sep := text[end : end+1]
			result = append(result, part+sep)
			pos = end + 1
		} else {
			result = append(result, part)
		}
	}
	return result
}

func normalizeForDedup(s string) string {
	s = strings.ToLower(s)
	s = strings.Join(strings.Fields(s), " ")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TruncateHeadTail preserves head and tail of content with a truncation marker.
// Returns text unchanged if it contains privacy placeholders.
func TruncateHeadTail(text string, maxChars int, headRatio float64) string {
	if len(text) <= maxChars {
		return text
	}
	// Don't truncate text with privacy placeholders.
	if privacyPlaceholder.MatchString(text) {
		return text
	}
	if headRatio <= 0 || headRatio >= 1 {
		headRatio = 0.4
	}

	lines := strings.Split(text, "\n")
	totalLines := len(lines)

	headLines := int(float64(totalLines) * headRatio)
	tailLines := totalLines - headLines
	if tailLines < 5 && totalLines > 10 {
		tailLines = 5
		headLines = totalLines - tailLines
	}

	headChars := int(float64(maxChars) * headRatio)
	tailChars := maxChars - headChars

	headPart := strings.Join(lines[:headLines], "\n")
	tailPart := strings.Join(lines[totalLines-tailLines:], "\n")

	if len(headPart) > headChars {
		headPart = headPart[:headChars]
	}
	if len(tailPart) > tailChars {
		tailPart = tailPart[len(tailPart)-tailChars:]
	}

	truncatedLines := totalLines - headLines - tailLines
	marker := fmt.Sprintf("\n\n[%d lines truncated - showing first %d + last %d lines]\n\n",
		truncatedLines, headLines, tailLines)
	return headPart + marker + tailPart
}

// BudgetLevel represents token budget utilization level.
type BudgetLevel int

const (
	BudgetGreen  BudgetLevel = iota // <50%
	BudgetYellow                    // 50-75%
	BudgetRed                       // >75%
)

// TokenBudget tracks per-session token usage against model context limit.
type TokenBudget struct {
	UsedTokens   int
	ContextLimit int
	Model        string
}

// NewTokenBudget creates a budget tracker for the given model.
func NewTokenBudget(model string) *TokenBudget {
	cap := GetModelCapabilities(model)
	return &TokenBudget{
		ContextLimit: cap.ContextWindow,
		Model:        model,
	}
}

// AddTokens records token usage.
func (b *TokenBudget) AddTokens(input, output int) {
	b.UsedTokens += input + output
}

// Level returns current budget utilization level.
func (b *TokenBudget) Level() BudgetLevel {
	if b.ContextLimit == 0 {
		return BudgetGreen
	}
	pct := float64(b.UsedTokens) / float64(b.ContextLimit)
	switch {
	case pct >= 0.75:
		return BudgetRed
	case pct >= 0.50:
		return BudgetYellow
	default:
		return BudgetGreen
	}
}

// PercentUsed returns current utilization as percentage.
func (b *TokenBudget) PercentUsed() float64 {
	if b.ContextLimit == 0 {
		return 0
	}
	return float64(b.UsedTokens) / float64(b.ContextLimit) * 100
}

// ShouldOptimize returns true if budget level warrants optimization.
func (b *TokenBudget) ShouldOptimize() bool {
	return b.Level() >= BudgetYellow
}

// ShouldForceOptimize returns true if budget requires emergency optimization.
func (b *TokenBudget) ShouldForceOptimize() bool {
	return b.Level() >= BudgetRed
}
