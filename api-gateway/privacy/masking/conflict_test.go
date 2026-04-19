package masking

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOverlaps(t *testing.T) {
	tests := []struct {
		name string
		a, b Span
		want bool
	}{
		{"no overlap", Span{0, 5}, Span{5, 10}, false},
		{"overlap", Span{0, 5}, Span{3, 8}, true},
		{"contained", Span{0, 10}, Span{2, 5}, true},
		{"same", Span{0, 5}, Span{0, 5}, true},
		{"adjacent no overlap", Span{0, 5}, Span{5, 10}, false},
		{"reversed", Span{3, 8}, Span{0, 5}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, overlaps(tt.a, tt.b))
		})
	}
}

func TestResolveOverlaps(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		assert.Nil(t, ResolveOverlaps(nil))
	})

	t.Run("single span", func(t *testing.T) {
		result := ResolveOverlaps([]Span{{0, 5}})
		assert.Len(t, result, 1)
		assert.Equal(t, Span{0, 5}, result[0])
	})

	t.Run("non-overlapping", func(t *testing.T) {
		spans := []Span{{0, 5}, {10, 15}, {20, 25}}
		result := ResolveOverlaps(spans)
		assert.Len(t, result, 3)
	})

	t.Run("overlapping longer wins", func(t *testing.T) {
		spans := []Span{{0, 10}, {0, 5}, {3, 8}}
		result := ResolveOverlaps(spans)
		assert.Len(t, result, 1)
		assert.Equal(t, Span{0, 10}, result[0])
	})

	t.Run("partial overlap drops shorter", func(t *testing.T) {
		spans := []Span{{0, 10}, {8, 20}}
		result := ResolveOverlaps(spans)
		assert.Len(t, result, 1)
		assert.Equal(t, Span{0, 10}, result[0])
	})
}

func TestResolveConflicts(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		assert.Nil(t, ResolveConflicts(nil))
	})

	t.Run("single entity", func(t *testing.T) {
		result := ResolveConflicts([]ScoredSpan{
			{Span: Span{0, 5}, EntityType: "PERSON", Score: 0.9},
		})
		assert.Len(t, result, 1)
	})

	t.Run("same type merged", func(t *testing.T) {
		result := ResolveConflicts([]ScoredSpan{
			{Span: Span{0, 5}, EntityType: "PERSON", Score: 0.8},
			{Span: Span{3, 8}, EntityType: "PERSON", Score: 0.9},
		})
		assert.Len(t, result, 1)
		assert.Equal(t, 0, result[0].Start)
		assert.Equal(t, 8, result[0].End)
		assert.Equal(t, 0.9, result[0].Score)
	})

	t.Run("cross-type higher score wins", func(t *testing.T) {
		result := ResolveConflicts([]ScoredSpan{
			{Span: Span{0, 5}, EntityType: "PERSON", Score: 0.9},
			{Span: Span{2, 7}, EntityType: "EMAIL", Score: 0.6},
		})
		assert.Len(t, result, 1)
		assert.Equal(t, "PERSON", result[0].EntityType)
	})

	t.Run("non-overlapping kept", func(t *testing.T) {
		result := ResolveConflicts([]ScoredSpan{
			{Span: Span{0, 5}, EntityType: "PERSON", Score: 0.9},
			{Span: Span{10, 15}, EntityType: "EMAIL", Score: 0.8},
		})
		assert.Len(t, result, 2)
	})
}

func TestFindPartialPlaceholderStart(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"no placeholder", "hello world", -1},
		{"complete placeholder", "[[PERSON_1]]", -1},
		{"partial at end", "hello [[PER", 6},
		{"partial after complete", "[[PERSON_1]] hello [[PER", 19},
		{"just brackets", "text [[", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FindPartialPlaceholderStart(tt.text))
		})
	}
}
