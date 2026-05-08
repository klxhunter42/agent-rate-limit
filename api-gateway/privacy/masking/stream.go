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
	// Fallback: models like GLM may output "undefined" instead of preserving placeholders.
	if strings.Contains(processed, "undefined") {
		processed = u.replaceUndefinedFallback(processed)
	}
	// Safety: strip any [[TYPE_N]] placeholder tokens that survived unmasking.
	processed = stripLeftoverPlaceholders(processed)
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
	// Same undefined fallback as ProcessChunk - GLM models may output "undefined"
	// in JSON deltas (tool_use input) instead of preserving placeholders.
	if strings.Contains(processed, "undefined") {
		processed = u.replaceUndefinedFallback(processed)
	}
	// Safety: strip any [[TYPE_N]] placeholder tokens that survived unmasking.
	processed = stripLeftoverPlaceholders(processed)
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
	return stripLeftoverPlaceholders(result)
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
	return stripLeftoverPlaceholders(result)
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
	if u.secretsCtx != nil && u.secretsJSONBuffer != "" {
		result += u.secretsCtx.RestorePlaceholdersJSON(u.secretsJSONBuffer)
		u.secretsJSONBuffer = ""
	}
	if u.piiCtx != nil && u.piiJSONBuffer != "" {
		result += u.piiCtx.RestorePlaceholdersJSON(u.piiJSONBuffer)
		u.piiJSONBuffer = ""
	} else if u.piiJSONBuffer != "" {
		result += u.piiJSONBuffer
		u.piiJSONBuffer = ""
	}

	return stripLeftoverPlaceholders(result)
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
// exhaustion. Handles concatenated forms like "undefinedundefinedundefinedVALUE"
// by stripping leading/trailing "undefined" runs while preserving actual content.
func stripStrayUndefined(text string) string {
	if !strings.Contains(text, "undefined") {
		return text
	}
	// Remove standalone "undefined" words (surrounded by non-alpha or at boundaries).
	// This avoids stripping "undefined" that appears as part of a real word.
	result := text
	for {
		replaced := result
		// "undefined " at word boundary
		replaced = strings.Replace(replaced, "undefined ", " ", -1)
		// " undefined" at word boundary
		replaced = strings.Replace(replaced, " undefined", "", -1)
		// Bare "undefined" with no space (concatenated by model)
		replaced = strings.Replace(replaced, "undefined", "", -1)
		if replaced == result {
			break
		}
		result = replaced
	}
	return result
}

// stripLeftoverPlaceholders removes any [[TYPE_N]] placeholder tokens that
// survived the unmasking pipeline. These appear when GLM mangles placeholders
// or when unmapped placeholders leak through.
func stripLeftoverPlaceholders(text string) string {
	if !strings.Contains(text, "[[") {
		return text
	}
	return leftoverPlaceholderRe.ReplaceAllString(text, "")
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
