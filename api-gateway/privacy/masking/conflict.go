package masking

import "strings"

// Span represents a text range with start/end positions.
type Span struct {
	Start int
	End   int
}

// ScoredSpan extends Span with a confidence score and entity type (for PII).
type ScoredSpan struct {
	Span
	EntityType string
	Score      float64
}

// overlaps returns true if two spans overlap.
func overlaps(a, b Span) bool {
	return a.Start < b.End && b.Start < a.End
}

// ResolveConflicts handles PII entities with confidence scores.
// Groups by type, merges same-type overlaps, then removes cross-type conflicts
// (higher score wins, longer span wins ties).
func ResolveConflicts(entities []ScoredSpan) []ScoredSpan {
	if len(entities) == 0 {
		return nil
	}

	// Group by entity type and merge overlapping within each group.
	groups := make(map[string][]ScoredSpan)
	for _, e := range entities {
		groups[e.EntityType] = append(groups[e.EntityType], e)
	}

	var merged []ScoredSpan
	for _, group := range groups {
		merged = append(merged, mergeGroup(group)...)
	}

	// Sort by score DESC, then length DESC, then start ASC.
	sortScoredDesc(merged)

	// Greedy: keep entity only if it doesn't overlap any already-kept entity.
	return greedySelectScored(merged)
}

func mergeGroup(group []ScoredSpan) []ScoredSpan {
	if len(group) <= 1 {
		return group
	}
	sortByStart(group)

	result := []ScoredSpan{group[0]}
	for _, cur := range group[1:] {
		last := &result[len(result)-1]
		if overlaps(last.Span, cur.Span) {
			// Merge: union of boundaries, max score.
			if cur.End > last.End {
				last.End = cur.End
			}
			if cur.Start < last.Start {
				last.Start = cur.Start
			}
			if cur.Score > last.Score {
				last.Score = cur.Score
			}
		} else {
			result = append(result, cur)
		}
	}
	return result
}

func greedySelectScored(sorted []ScoredSpan) []ScoredSpan {
	var kept []ScoredSpan
	for _, e := range sorted {
		conflict := false
		for _, k := range kept {
			if overlaps(e.Span, k.Span) {
				conflict = true
				break
			}
		}
		if !conflict {
			kept = append(kept, e)
		}
	}
	return kept
}

// ResolveOverlaps handles secrets without scores.
// Sorts by start ASC, longer span wins ties on same start.
func ResolveOverlaps(spans []Span) []Span {
	if len(spans) <= 1 {
		return spans
	}

	// Sort by start ASC, then length DESC.
	sorted := make([]Span, len(spans))
	copy(sorted, spans)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0; j-- {
			a, b := sorted[j-1], sorted[j]
			if a.Start > b.Start || (a.Start == b.Start && (b.End-b.Start) > (a.End-a.Start)) {
				sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
			} else {
				break
			}
		}
	}

	result := []Span{sorted[0]}
	for _, cur := range sorted[1:] {
		last := result[len(result)-1]
		if cur.Start >= last.End {
			result = append(result, cur)
		}
		// Overlapping: shorter one is silently dropped.
	}
	return result
}

func sortByStart(s []ScoredSpan) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Start < s[j-1].Start; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func sortScoredDesc(s []ScoredSpan) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0; {
			a, b := s[j-1], s[j]
			if b.Score > a.Score {
				s[j], s[j-1] = s[j-1], s[j]
			} else if b.Score == a.Score {
				lenA := a.End - a.Start
				lenB := b.End - b.Start
				if lenB > lenA || (lenB == lenA && b.Start < a.Start) {
					s[j], s[j-1] = s[j-1], s[j]
				} else {
					break
				}
			} else {
				break
			}
			j--
		}
	}
}

// FindPartialPlaceholderStart finds the position where a partial [[... placeholder begins.
// Returns -1 if no partial placeholder is found (safe to process all text).
func FindPartialPlaceholderStart(text string) int {
	idx := strings.LastIndex(text, PlaceholderStart)
	if idx < 0 {
		return -1
	}
	afterStart := text[idx:]
	if strings.Contains(afterStart, PlaceholderEnd) {
		return -1 // Complete placeholder, safe.
	}
	return idx
}
