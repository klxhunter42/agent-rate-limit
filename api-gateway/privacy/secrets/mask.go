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

	spans := make([]masking.Span, len(locations))
	for i, loc := range locations {
		spans[i] = masking.Span{Start: loc.Start, End: loc.End}
	}

	masked := masking.ReplaceWithPlaceholders(text, spans, func(i int, original string) string {
		if ph, ok := ctx.ReverseMap[original]; ok {
			return ph
		}
		ph := ctx.NextPlaceholder(locations[i].Type)
		ctx.Mapping[ph] = original
		ctx.ReverseMap[original] = ph
		return ph
	})

	return MaskResult{MaskedText: masked, Context: ctx}
}

func locType(locations []masking.SecretLocation, original string) string {
	for _, loc := range locations {
		if loc.Type != "" {
			return loc.Type
		}
	}
	return "SECRET"
}
