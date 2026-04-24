package masking

// ReplaceWithPlaceholders replaces detected spans in text with placeholders.
// assignFn is called for each span (sorted by start ASC) to get a placeholder.
// The replacement is done backward (end-to-start) to preserve indices.
func ReplaceWithPlaceholders(text string, spans []Span, assignFn func(i int, original string) string) string {
	if len(spans) == 0 {
		return text
	}

	// Resolve overlaps first.
	resolved := ResolveOverlaps(spans)
	if len(resolved) == 0 {
		return text
	}

	// Sort by start ASC for deterministic placeholder assignment.
	sortSpansByStart(resolved)

	// Assign placeholders.
	placeholders := make([]string, len(resolved))
	for i, sp := range resolved {
		original := text[sp.Start:sp.End]
		placeholders[i] = assignFn(i, original)
	}

	// Replace backward (start DESC) to preserve indices.
	sorted := make([]spanPlaceholder, len(resolved))
	for i, sp := range resolved {
		sorted[i] = spanPlaceholder{Span: sp, Placeholder: placeholders[i]}
	}
	sortSpanPlaceholdersByStartDesc(sorted)

	result := text
	for _, sp := range sorted {
		result = result[:sp.Start] + sp.Placeholder + result[sp.End:]
	}
	return result
}

// ReplaceWithPlaceholdersScored replaces scored spans (PII) in text.
// assignFn receives the resolved span's entity type.
func ReplaceWithPlaceholdersScored(text string, entities []ScoredSpan, assignFn func(i int, original string, entityType string) string) string {
	if len(entities) == 0 {
		return text
	}

	resolved := ResolveConflicts(entities)
	if len(resolved) == 0 {
		return text
	}

	sortScoredByStart(resolved)

	placeholders := make([]string, len(resolved))
	for i, sp := range resolved {
		original := text[sp.Start:sp.End]
		placeholders[i] = assignFn(i, original, sp.EntityType)
	}

	sorted := make([]scoredSpanPlaceholder, len(resolved))
	for i, sp := range resolved {
		sorted[i] = scoredSpanPlaceholder{ScoredSpan: sp, Placeholder: placeholders[i]}
	}
	sortScoredSpanPlaceholdersByStartDesc(sorted)

	result := text
	for _, sp := range sorted {
		result = result[:sp.Start] + sp.Placeholder + result[sp.End:]
	}
	return result
}

type spanPlaceholder struct {
	Span
	Placeholder string
}

type scoredSpanPlaceholder struct {
	ScoredSpan
	Placeholder string
}

func sortSpansByStart(s []Span) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Start < s[j-1].Start; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func sortSpanPlaceholdersByStartDesc(s []spanPlaceholder) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Start > s[j-1].Start; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func sortScoredByStart(s []ScoredSpan) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Start < s[j-1].Start; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func sortScoredSpanPlaceholdersByStartDesc(s []scoredSpanPlaceholder) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Start > s[j-1].Start; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
