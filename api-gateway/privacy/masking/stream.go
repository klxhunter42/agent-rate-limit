package masking

import (
	"regexp"
	"sort"
	"strings"
)

// leftoverPlaceholderRe matches [[TYPE_N]] placeholder tokens that survived
// unmasking (e.g. when GLM mangles them beyond recognition).
var leftoverPlaceholderRe = regexp.MustCompile(`\[\[[A-Z][A-Z0-9_]*_[A-Za-z0-9]+\]\]`)

type StreamUnmasker struct {
	// glmNoiseMode enables the GLM-specific U-word noise handling. It defaults
	// ON in the constructor for backward-compat; production proxies call
	// SetGLMNoiseMode(model) to turn it OFF for capable models (Claude/Gemini/
	// OpenAI) that preserve placeholders and never emit the failure token, so
	// the fallback must NOT run for them.
	glmNoiseMode  bool
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
		piiCtx:       piiCtx,
		secretsCtx:   secretsCtx,
		glmNoiseMode: true,
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
	if u.glmNoiseMode && u.HasContexts() && strings.Contains(processed, "undefined") {
		processed = u.replaceUndefinedFallback(processed, false)
	}
	// Buffer tail that might be a partial "undefined" split across SSE chunks.
	// Must run AFTER fallback so the partial is preceded by the replacement value
	// (non-alpha), not by "d" from a previous "undefined" (alpha).
	if u.glmNoiseMode && u.HasContexts() {
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
	// JSON mode: originals are escaped so tool_use input_json_delta stays valid.
	if u.glmNoiseMode && u.HasContexts() && strings.Contains(processed, "undefined") {
		processed = u.replaceUndefinedFallback(processed, true)
	}
	// Buffer tail that might be a partial "undefined" split across SSE chunks.
	if u.glmNoiseMode && u.HasContexts() {
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
		if u.glmNoiseMode && u.HasContexts() {
			if strings.Contains(ub, "undefined") {
				ub = u.replaceUndefinedFallback(ub, false)
			}
			// Strip partial "undefined" prefix at stream end (e.g. "undefi").
			ub = stripPartialUndefined(ub)
			ub = stripStrayUndefined(ub)
		}
		result += ub
	}

	return StripLeftoverPlaceholders(result)
}

func (u *StreamUnmasker) SetGLMNoiseMode(enabled bool) {
	u.glmNoiseMode = enabled
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
//
// jsonSafe must be true when the text is a JSON fragment (tool_use input_json_delta):
// originals are JSON-escaped before substitution so characters like " , \ , newline
// do not corrupt the JSON the client concatenates and JSON.parses.
func (u *StreamUnmasker) replaceUndefinedFallback(text string, jsonSafe bool) string {
	if u.fallbackOriginals == nil {
		u.fallbackOriginals = u.collectFallbackOriginals()
	}

	// Pre-Phase: remove "undefined" already adjacent to leaked original values (no-space or spaced),
	// and strip "undefined" embedded within word chars (e.g. "PGundefinedUSER" -> "PGUSER").
	// Must run before Phase 1 so these don't consume budget slots incorrectly.
	text = u.dedupAdjacentUndefined(text, jsonSafe)
	text = StripEmbeddedUndefined(text)

	// Phase 1: Replace "undefined" with originals (budget-limited by available originals).
	for u.fallbackConsumedIdx < len(u.fallbackOriginals) {
		if !strings.Contains(text, "undefined") {
			break
		}
		orig := u.fallbackOriginals[u.fallbackConsumedIdx]
		if jsonSafe {
			orig = jsonEscape(orig)
		}
		u.fallbackConsumedIdx++
		text = strings.Replace(text, "undefined", orig, 1)
	}

	// Phase 2: Remove leftover "undefined" that appear adjacent to an already-restored
	// original value. Pattern: "<original> undefined" -> "<original>"
	text = u.dedupAdjacentUndefined(text, jsonSafe)

	// Phase 3: Budget exhausted - strip any remaining bare "undefined" to prevent
	// garbled output like "undefinedundefinedundefined172.18.0.9" leaking to client.
	if u.fallbackConsumedIdx >= len(u.fallbackOriginals) {
		text = stripStrayUndefined(text)
	}

	return text
}

// dedupAdjacentUndefined removes "undefined" that appears right after a restored
// original value. Example: "192.168.5.111 undefined" -> "192.168.5.111".
// jsonSafe must match the mode of the caller so patterns match the escaped form
// substituted in Phase 1.
func (u *StreamUnmasker) dedupAdjacentUndefined(text string, jsonSafe bool) string {
	if len(u.fallbackOriginals) == 0 {
		return text
	}
	for _, raw := range u.fallbackOriginals {
		if raw == "" {
			continue
		}
		orig := raw
		if jsonSafe {
			orig = jsonEscape(orig)
		}
		for strings.Contains(text, orig+" undefined") {
			text = strings.Replace(text, orig+" undefined", orig, 1)
		}
		for strings.Contains(text, "undefined "+orig) {
			text = strings.Replace(text, "undefined "+orig, orig, 1)
		}
		// No-space variants: GLM can concatenate directly (e.g. "undefined10.228.26.203")
		for strings.Contains(text, orig+"undefined") {
			text = strings.Replace(text, orig+"undefined", orig, 1)
		}
		for strings.Contains(text, "undefined"+orig) {
			text = strings.Replace(text, "undefined"+orig, orig, 1)
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
	result := strayUndefinedRe.ReplaceAllString(text, "")
	// Clean up extra spaces left after removal.
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}
	result = strings.TrimRight(result, " ")
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

// strayUndefinedRe matches "undefined" anywhere in the string (including concatenated).
var strayUndefinedRe = regexp.MustCompile(`undefined`)

// embeddedUndefinedRe matches "undefined" surrounded by word chars on both sides.
// e.g. "PGundefinedUSER" - the "undefined" is embedded and cannot be a legitimate replacement.
var embeddedUndefinedRe = regexp.MustCompile(`([A-Za-z0-9_])undefined([A-Za-z0-9_])`)

// StripEmbeddedUndefined removes "undefined" tokens surrounded by word chars on both sides.
// Handles GLM output like "PGundefinedUSER" -> "PGUSER" before Phase 1 replacement.
func StripEmbeddedUndefined(text string) string {
	if !strings.Contains(text, "undefined") {
		return text
	}
	// Loop: ReplaceAll consumes boundary chars, adjacent cases need a second pass.
	for embeddedUndefinedRe.MatchString(text) {
		text = embeddedUndefinedRe.ReplaceAllString(text, "${1}${2}")
	}
	return text
}

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

func SanitizeGarbledForModel(model, text string) string {
	if strings.HasPrefix(model, "glm-") {
		return SanitizeGarbledOutput(text)
	}
	return text
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
	var result []string
	// PII first (outer masking layer): host/IP appear before passwords in connection strings.
	result = AppendOrderedOriginals(result, u.piiCtx)
	// Secrets second (inner masking layer): passwords/API keys appear after host.
	result = AppendOrderedOriginals(result, u.secretsCtx)
	return result
}

// AppendOrderedOriginals appends originals from ctx in insertion order when available,
// falling back to alphabetical sort for contexts populated without AddMapping.
func AppendOrderedOriginals(result []string, ctx *MaskContext) []string {
	if ctx == nil {
		return result
	}
	if len(ctx.Order) > 0 {
		for _, ph := range ctx.Order {
			if orig, ok := ctx.Mapping[ph]; ok {
				result = append(result, orig)
			}
		}
		return result
	}
	// Fallback: Order not populated (e.g. tests or legacy paths); use sorted placeholder names.
	phs := make([]string, 0, len(ctx.Mapping))
	for p := range ctx.Mapping {
		phs = append(phs, p)
	}
	sort.Strings(phs)
	for _, p := range phs {
		result = append(result, ctx.Mapping[p])
	}
	return result
}
