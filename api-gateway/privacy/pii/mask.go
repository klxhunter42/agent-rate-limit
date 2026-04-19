package pii

import (
	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
)

type MaskResult struct {
	MaskedText string
	Context    *masking.MaskContext
}

// MaskPII replaces detected PII entities in text with placeholders.
func MaskPII(text string, entities []masking.PIIEntity, ctx *masking.MaskContext) MaskResult {
	if ctx == nil {
		ctx = masking.NewMaskContext()
	}
	if len(entities) == 0 {
		return MaskResult{MaskedText: text, Context: ctx}
	}

	scored := make([]masking.ScoredSpan, len(entities))
	for i, e := range entities {
		scored[i] = masking.ScoredSpan{
			Span:       masking.Span{Start: e.Start, End: e.End},
			EntityType: e.EntityType,
			Score:      e.Score,
		}
	}

	masked := masking.ReplaceWithPlaceholdersScored(text, scored, func(_ int, original string) string {
		if ph, ok := ctx.ReverseMap[original]; ok {
			return ph
		}
		ph := ctx.NextPlaceholder(entityType(entities))
		ctx.Mapping[ph] = original
		ctx.ReverseMap[original] = ph
		return ph
	})

	return MaskResult{MaskedText: masked, Context: ctx}
}

func entityType(entities []masking.PIIEntity) string {
	if len(entities) > 0 {
		return entities[0].EntityType
	}
	return "PII"
}
