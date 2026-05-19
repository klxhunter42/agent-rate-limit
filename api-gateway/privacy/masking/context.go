package masking

import (
	"fmt"
	"strconv"
	"strings"
)

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
	return restoreSorted(text, ctx.Mapping, false)
}

// RestorePlaceholdersJSON replaces placeholders with JSON-escaped originals.
// Use when unmasking raw JSON response bodies to preserve JSON structure.
func (ctx *MaskContext) RestorePlaceholdersJSON(text string) string {
	return restoreSorted(text, ctx.Mapping, true)
}

func restoreSorted(text string, mapping map[string]string, jsonSafe bool) string {
	if len(mapping) == 0 {
		return text
	}
	placeholders := make([]string, 0, len(mapping))
	for p := range mapping {
		placeholders = append(placeholders, p)
	}
	sortByLenDesc(placeholders)

	result := text
	for _, p := range placeholders {
		orig := mapping[p]
		if jsonSafe {
			orig = jsonEscape(orig)
		}
		result = replaceAll(result, p, orig)
	}
	return result
}

func jsonEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\b':
			b.WriteString(`\b`)
		case c == '\f':
			b.WriteString(`\f`)
		case c < 0x20:
			fmt.Fprintf(&b, `\u%04x`, c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
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

func (ctx *MaskContext) MergeExternal(mapping map[string]string) {
	for placeholder, original := range mapping {
		if _, exists := ctx.Mapping[placeholder]; !exists {
			ctx.Mapping[placeholder] = original
			ctx.ReverseMap[original] = placeholder
			// Update counter to prevent NextPlaceholder collision.
			// Placeholder format: [[TYPE_N]] -> extract TYPE and N.
			inner := strings.TrimPrefix(strings.TrimSuffix(placeholder, "]]"), "[[")
			if idx := strings.LastIndex(inner, "_"); idx > 0 {
				entityType := inner[:idx]
				if n, err := strconv.Atoi(inner[idx+1:]); err == nil && n > ctx.Counters[entityType] {
					ctx.Counters[entityType] = n
				}
			}
		}
	}
}

func sortByLenDesc(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && len(ss[j]) > len(ss[j-1]); j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}
