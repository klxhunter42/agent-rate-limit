package masking

import (
	"regexp"
	"sort"
	"strings"
)

// leftoverPlaceholderRe matches [[TYPE_N]] placeholder tokens that survived
// unmasking (e.g. when GLM mangles them beyond recognition).
var leftoverPlaceholderRe = regexp.MustCompile(`\[\[[A-Z_]+_\d+\]\]`)

type StreamUnmasker struct {
	piiBuffer     string
	secretsBuffer string
	// JSON-mode buffers for partial_json (input_json_delta) where replaced
	// values must survive JSON string encoding.
	piiJSONBuffer     string
	secretsJSONBuffer string
	piiCtx            *MaskContext
	secretsCtx        *MaskContext
	// Fallback sanitizer state for models that output "undefined" instead of
	// preserving [[TYPE_N]] placeholders. Lazily populated.
	fallbackOriginals   []string
	fallbackConsumedIdx int
	// undefinedBuffer holds a partial "undefined" prefix split across SSE chunks.
	undefinedBuffer string
}

func NewStreamUnmasker(piiCtx, secretsCtx *MaskContext) *StreamUnmasker {
	return &StreamUnmasker{
		piiCtx:     piiCtx,
		secretsCtx: secretsCtx,
	}
}

// ProcessChunk unmasks a single SSE text chunk with buffering for partial placeholders.
// Only use for text/thinking deltas within the same content block.
func (u *StreamUnmasker) ProcessChunk(chunk string) string {
	processed := chunk
	if u.secretsCtx != nil && len(u.secretsCtx.Mapping) > 0 {
		processed, u.secretsBuffer = processStreamChunk(u.secretsBuffer, processed, u.secretsCtx)
	}
	if u.piiCtx != nil && len(u.piiCtx.Mapping) > 0 {
		processed, u.piiBuffer = processStreamChunk(u.piiBuffer, processed, u.piiCtx)
	}
	// Prepend previously buffered partial "undefined" prefix.
	if u.undefinedBuffer != "" {
		processed = u.undefinedBuffer + processed
		u.undefinedBuffer = ""
	}
	// Fallback: models like GLM may output "undefined" instead of preserving placeholders.
	// Only run when masking is active to avoid stripping legitimate "undefined" from
	// code examples (e.g. typeof x === "undefined").
	if u.HasContexts() && strings.Contains(processed, "undefined") {
		processed = u.replaceUndefinedFallback(processed)
	}
	// Buffer tail that might be a partial "undefined" split across SSE chunks.
	// Must run AFTER fallback so the partial is preceded by the replacement value
	// (non-alpha), not by "d" from a previous "undefined" (alpha).
	if u.HasContexts() {
		var buf string
		processed, buf = bufferPartialUndefined(processed)
		u.undefinedBuffer = buf
	}
	// Safety: strip any [[TYPE_N]] placeholder tokens that survived unmasking.
	processed = StripLeftoverPlaceholders(processed)
	return processed
}

// ProcessChunkJSON does buffered JSON-safe unmasking for partial_json deltas.
// Use for input_json_delta events where placeholders may be split across chunks
// and replaced values must survive JSON string encoding.
func (u *StreamUnmasker) ProcessChunkJSON(chunk string) string {
	processed := chunk
	if u.secretsCtx != nil && len(u.secretsCtx.Mapping) > 0 {
		processed, u.secretsJSONBuffer = processStreamChunkJSON(u.secretsJSONBuffer, processed, u.secretsCtx)
	}
	if u.piiCtx != nil && len(u.piiCtx.Mapping) > 0 {
		processed, u.piiJSONBuffer = processStreamChunkJSON(u.piiJSONBuffer, processed, u.piiCtx)
	}
	// Prepend previously buffered partial "undefined" prefix.
	if u.undefinedBuffer != "" {
		processed = u.undefinedBuffer + processed
		u.undefinedBuffer = ""
	}
	// Same undefined fallback as ProcessChunk - only when masking is active.
	if u.HasContexts() && strings.Contains(processed, "undefined") {
		processed = u.replaceUndefinedFallback(processed)
	}
	// Buffer tail that might be a partial "undefined" split across SSE chunks.
	if u.HasContexts() {
		var buf string
		processed, buf = bufferPartialUndefined(processed)
		u.undefinedBuffer = buf
	}
	// Safety: strip any [[TYPE_N]] placeholder tokens that survived unmasking.
	processed = StripLeftoverPlaceholders(processed)
	return processed
}

// ReplaceDirect does unbuffered replacement on a standalone string.
// Use for partial_json and other independent SSE data to avoid
// cross-block buffer contamination.
func (u *StreamUnmasker) ReplaceDirect(text string) string {
	result := text
	if u.secretsCtx != nil && len(u.secretsCtx.Mapping) > 0 {
		result = u.secretsCtx.RestorePlaceholders(result)
	}
	if u.piiCtx != nil && len(u.piiCtx.Mapping) > 0 {
		result = u.piiCtx.RestorePlaceholders(result)
	}
	return StripLeftoverPlaceholders(result)
}

// ReplaceDirectJSON does unbuffered JSON-safe replacement.
// Use for partial_json and raw SSE data lines where the replaced
// values must survive JSON string encoding.
func (u *StreamUnmasker) ReplaceDirectJSON(text string) string {
	result := text
	if u.secretsCtx != nil && len(u.secretsCtx.Mapping) > 0 {
		result = u.secretsCtx.RestorePlaceholdersJSON(result)
	}
	if u.piiCtx != nil && len(u.piiCtx.Mapping) > 0 {
		result = u.piiCtx.RestorePlaceholdersJSON(result)
	}
	return StripLeftoverPlaceholders(result)
}

func (u *StreamUnmasker) Flush() string {
	// Process secrets first (innermost layer), then PII (outermost layer)
	// This matches the layering order in ProcessChunk/ReplaceDirect.
	result := ""
	if u.secretsCtx != nil && u.secretsBuffer != "" {
		result += u.secretsCtx.RestorePlaceholders(u.secretsBuffer)
		u.secretsBuffer = ""
	}
	combined := u.piiBuffer + result
	if u.piiCtx != nil && combined != "" {
		result = u.piiCtx.RestorePlaceholders(combined)
		u.piiBuffer = ""
	} else if u.piiBuffer != "" {
		result = u.piiBuffer + result
		u.piiBuffer = ""
	}

	// Also flush JSON-mode buffers (partial_json path).
	// Same layering as text mode: secrets first (innermost), then PII (outermost).
	jsonResult := ""
	if u.secretsCtx != nil && u.secretsJSONBuffer != "" {
		jsonResult += u.secretsCtx.RestorePlaceholdersJSON(u.secretsJSONBuffer)
		u.secretsJSONBuffer = ""
	}
	combinedJSON := u.piiJSONBuffer + jsonResult
	if u.piiCtx != nil && combinedJSON != "" {
		jsonResult = u.piiCtx.RestorePlaceholdersJSON(combinedJSON)
		u.piiJSONBuffer = ""
	} else if u.piiJSONBuffer != "" {
		jsonResult = u.piiJSONBuffer + jsonResult
		u.piiJSONBuffer = ""
	}
	result += jsonResult

	// Flush undefined buffer: run fallback then strip leftovers.
	if u.undefinedBuffer != "" {
		ub := u.undefinedBuffer
		u.undefinedBuffer = ""
		if u.HasContexts() {
			if strings.Contains(ub, "undefined") {
				ub = u.replaceUndefinedFallback(ub)
			}
			// Strip partial "undefined" prefix at stream end (e.g. "undefi").
			ub = stripPartialUndefined(ub)
			ub = stripStrayUndefined(ub)
		}
		result += ub
	}

	return StripLeftoverPlaceholders(result)
}

func (u *StreamUnmasker) HasContexts() bool {
	if u.piiCtx != nil && len(u.piiCtx.Mapping) > 0 {
		return true
	}
	if u.secretsCtx != nil && len(u.secretsCtx.Mapping) > 0 {
		return true
	}
	return false
}

func processStreamChunk(buffer, chunk string, ctx *MaskContext) (output, remaining string) {
	combined := buffer + chunk
	partialStart := FindPartialPlaceholderStart(combined)
	if partialStart < 0 {
		return ctx.RestorePlaceholders(combined), ""
	}
	safeToProcess := combined[:partialStart]
	toBuffer := combined[partialStart:]
	return ctx.RestorePlaceholders(safeToProcess), toBuffer
}

func processStreamChunkJSON(buffer, chunk string, ctx *MaskContext) (output, remaining string) {
	combined := buffer + chunk
	partialStart := FindPartialPlaceholderStart(combined)
	if partialStart < 0 {
		return ctx.RestorePlaceholdersJSON(combined), ""
	}
	safeToProcess := combined[:partialStart]
	toBuffer := combined[partialStart:]
	return ctx.RestorePlaceholdersJSON(safeToProcess), toBuffer
}

// replaceUndefinedFallback replaces "undefined" strings with original values from
// the mapping. Some models (e.g. GLM) output "undefined" instead of preserving
// [[TYPE_N]] placeholders. This is a best-effort fallback to recover the original text.
//
// Handles GLM failure modes:
//  1. Model outputs "undefined" instead of placeholder -> replace with original
//  2. Model outputs both placeholder AND "undefined" (e.g. "192.168.5.111 undefined")
//     -> dedup by removing the trailing "undefined"
//  3. Budget exhausted (more "undefined" than originals) -> strip remaining "undefined"
func (u *StreamUnmasker) replaceUndefinedFallback(text string) string {
	if u.fallbackOriginals == nil {
		u.fallbackOriginals = u.collectFallbackOriginals()
	}

	// Phase 1: Replace "undefined" with originals (budget-limited by available originals).
	for u.fallbackConsumedIdx < len(u.fallbackOriginals) {
		if !strings.Contains(text, "undefined") {
			break
		}
		orig := u.fallbackOriginals[u.fallbackConsumedIdx]
		u.fallbackConsumedIdx++
		text = strings.Replace(text, "undefined", orig, 1)
	}

	// Phase 2: Remove leftover "undefined" that appear adjacent to an already-restored
	// original value. Pattern: "<original> undefined" -> "<original>"
	text = u.dedupAdjacentUndefined(text)

	// Phase 3: Budget exhausted - strip any remaining bare "undefined" to prevent
	// garbled output like "undefinedundefinedundefined172.18.0.9" leaking to client.
	if u.fallbackConsumedIdx >= len(u.fallbackOriginals) {
		text = stripStrayUndefined(text)
	}

	return text
}

// dedupAdjacentUndefined removes "undefined" that appears right after a restored
// original value. Example: "192.168.5.111 undefined" -> "192.168.5.111"
func (u *StreamUnmasker) dedupAdjacentUndefined(text string) string {
	if len(u.fallbackOriginals) == 0 {
		return text
	}
	for _, orig := range u.fallbackOriginals {
		// Match "<original> undefined" with a space separator
		pattern := orig + " undefined"
		for strings.Contains(text, pattern) {
			text = strings.Replace(text, pattern, orig, 1)
		}
		// Match "undefined <original>" with a space separator
		pattern = "undefined " + orig
		for strings.Contains(text, pattern) {
			text = strings.Replace(text, pattern, orig, 1)
		}
	}
	return text
}

// stripStrayUndefined removes bare "undefined" tokens that remain after budget
// exhaustion. Uses word-boundary regex to avoid stripping "undefined" embedded
// in legitimate words (e.g. "isUndefined", "typeof_undefined_var").
func stripStrayUndefined(text string) string {
	if !strings.Contains(text, "undefined") {
		return text
	}
	// Word-boundary regex: matches "undefined" not adjacent to [a-zA-Z0-9_]
	result := strayUndefinedRe.ReplaceAllString(text, "")
	// Also handle concatenated model output like "undefinedundefinedVALUE"
	for strings.Contains(result, "undefinedundefined") {
		result = strings.ReplaceAll(result, "undefinedundefined", "")
	}
	return result
}

// stripPartialUndefined removes text that is a prefix of "undefined" (1-8 chars).
// Used during flush to clean up partial "undefined" at stream end.
func stripPartialUndefined(text string) string {
	target := "undefined"
	for i := len(target) - 1; i >= 1; i-- {
		if text == target[:i] {
			return ""
		}
		if strings.HasSuffix(text, target[:i]) {
			return text[:len(text)-i]
		}
	}
	return text
}

// strayUndefinedRe matches "undefined" with optional surrounding whitespace.
// Uses negative lookahead/behind to avoid stripping "undefined" inside legitimate
// identifiers (e.g. "isUndefined", "typeof_undefined_var"). In Go regex (RE2),
// we use word boundaries with a fallback for concatenated cases.
var strayUndefinedRe = regexp.MustCompile(`\s*undefined\s*`)

// garbledUndefinedRe matches any occurrence of "undefined" with optional whitespace.
// GLM models emit this as garbled noise in both single and repeated form.
// Strips all occurrences since "undefined" in model output is never legitimate content.
var garbledUndefinedRe = regexp.MustCompile(`(?:undefined[\s]*){2,}`)

// SanitizeGarbledOutput strips "undefined" tokens from model output.
// GLM models emit this as garbled noise regardless of masking state.
func SanitizeGarbledOutput(text string) string {
	if !strings.Contains(text, "undefined") {
		return text
	}
	return garbledUndefinedRe.ReplaceAllString(text, "")
}

// StripLeftoverPlaceholders removes any [[TYPE_N]] placeholder tokens that
// survived the unmasking pipeline. These appear when GLM mangles placeholders
// or when unmapped placeholders leak through.
func StripLeftoverPlaceholders(text string) string {
	if !strings.Contains(text, "[[") {
		return text
	}
	return leftoverPlaceholderRe.ReplaceAllString(text, "")
}

// bufferPartialUndefined checks if text ends with a prefix of "undefined" and
// splits it so the partial part can be buffered for the next SSE chunk.
// Runs after fallback replacement, so any partial prefix at the end is very likely
// the start of a split "undefined" from the model.
func bufferPartialUndefined(text string) (safe, buffer string) {
	target := "undefined"
	for i := len(target) - 1; i >= 1; i-- {
		prefix := target[:i]
		if strings.HasSuffix(text, prefix) {
			return text[:len(text)-len(prefix)], prefix
		}
	}
	return text, ""
}

// collectFallbackOriginals returns original values from both contexts, sorted by
// placeholder counter (numeric) for correct substitution order matching the
// original masking sequence.
func (u *StreamUnmasker) collectFallbackOriginals() []string {
	type entry struct {
		placeholder string
		original    string
	}
	var entries []entry
	if u.secretsCtx != nil {
		for p, orig := range u.secretsCtx.Mapping {
			entries = append(entries, entry{p, orig})
		}
	}
	if u.piiCtx != nil {
		for p, orig := range u.piiCtx.Mapping {
			entries = append(entries, entry{p, orig})
		}
	}
	// Sort by placeholder name (e.g. [[EMAIL_ADDRESS_1]] before [[IP_ADDRESS_2]])
	// This gives deterministic ordering matching the masking sequence.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].placeholder < entries[j].placeholder
	})
	result := make([]string, len(entries))
	for i, e := range entries {
		result[i] = e.original
	}
	return result
}
