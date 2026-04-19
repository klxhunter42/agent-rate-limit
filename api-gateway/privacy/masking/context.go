package masking

import "fmt"

const (
	PlaceholderStart = "[["
	PlaceholderEnd   = "]]"
)

type MaskContext struct {
	Mapping    map[string]string // placeholder -> original
	ReverseMap map[string]string // original -> placeholder (dedup)
	Counters   map[string]int    // entity type -> sequential counter
}

func NewMaskContext() *MaskContext {
	return &MaskContext{
		Mapping:    make(map[string]string),
		ReverseMap: make(map[string]string),
		Counters:   make(map[string]int),
	}
}

func GeneratePlaceholder(entityType string, counter int) string {
	return fmt.Sprintf("[[%s_%d]]", entityType, counter)
}

func (ctx *MaskContext) NextPlaceholder(entityType string) string {
	ctx.Counters[entityType]++
	return GeneratePlaceholder(entityType, ctx.Counters[entityType])
}

func (ctx *MaskContext) RestorePlaceholders(text string) string {
	return restoreSorted(text, ctx.Mapping)
}

func restoreSorted(text string, mapping map[string]string) string {
	if len(mapping) == 0 {
		return text
	}
	placeholders := make([]string, 0, len(mapping))
	for p := range mapping {
		placeholders = append(placeholders, p)
	}
	// Sort longest first to prevent partial matches ([[PERSON_10]] before [[PERSON_1]]).
	sortByLenDesc(placeholders)

	result := text
	for _, p := range placeholders {
		orig := mapping[p]
		result = replaceAll(result, p, orig)
	}
	return result
}

func replaceAll(s, old, new string) string {
	for {
		idx := indexOf(s, old)
		if idx < 0 {
			return s
		}
		s = s[:idx] + new + s[idx+len(old):]
	}
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func sortByLenDesc(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && len(ss[j]) > len(ss[j-1]); j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}
