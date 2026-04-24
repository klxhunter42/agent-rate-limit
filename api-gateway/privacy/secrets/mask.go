package secrets

import (
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
)

// MaskResult holds the masked text and updated context.
type MaskResult struct {
	MaskedText string
	Context    *masking.MaskContext
}

// MaskSecrets replaces detected secret locations in text with placeholders.
func MaskSecrets(text string, locations []masking.SecretLocation, ctx *masking.MaskContext) MaskResult {
	if ctx == nil {
		ctx = masking.NewMaskContext()
	}
	if len(locations) == 0 {
		return MaskResult{MaskedText: text, Context: ctx}
	}

	// Build original text → type lookup for use after overlap resolution.
	textTypes := make(map[string]string, len(locations))
	spans := make([]masking.Span, len(locations))
	for i, loc := range locations {
		spans[i] = masking.Span{Start: loc.Start, End: loc.End}
		textTypes[text[loc.Start:loc.End]] = loc.Type
	}

	masked := masking.ReplaceWithPlaceholders(text, spans, func(_ int, original string) string {
		if ph, ok := ctx.ReverseMap[original]; ok {
			return ph
		}
		typ := textTypes[original]
		if typ == "" {
			typ = "SECRET"
		}
		ph := ctx.NextPlaceholder(typ)
		ctx.Mapping[ph] = original
		ctx.ReverseMap[original] = ph
		return ph
	})

	return MaskResult{MaskedText: masked, Context: ctx}
}
