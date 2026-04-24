package masking

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReplaceWithPlaceholders(t *testing.T) {
	t.Run("empty spans", func(t *testing.T) {
		assert.Equal(t, "hello", ReplaceWithPlaceholders("hello", nil, nil))
	})

	t.Run("single replacement", func(t *testing.T) {
		spans := []Span{{0, 3}}
		result := ReplaceWithPlaceholders("abc world", spans, func(i int, orig string) string {
			return "[[KEY_1]]"
		})
		assert.Equal(t, "[[KEY_1]] world", result)
	})

	t.Run("multiple non-overlapping", func(t *testing.T) {
		spans := []Span{{0, 3}, {8, 11}}
		result := ReplaceWithPlaceholders("abc def ghi", spans, func(i int, orig string) string {
			return []string{"[[A_1]]", "[[B_1]]"}[i]
		})
		assert.Equal(t, "[[A_1]] def [[B_1]]", result)
	})

	t.Run("overlapping resolved", func(t *testing.T) {
		spans := []Span{{0, 10}, {3, 8}}
		result := ReplaceWithPlaceholders("0123456789end", spans, func(i int, orig string) string {
			return "[[X_1]]"
		})
		assert.Equal(t, "[[X_1]]end", result)
	})
}

func TestReplaceWithPlaceholdersScored(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		assert.Equal(t, "hello", ReplaceWithPlaceholdersScored("hello", nil, nil))
	})

	t.Run("single PII", func(t *testing.T) {
		entities := []ScoredSpan{
			{Span: Span{0, 5}, EntityType: "PERSON", Score: 0.9},
		}
		result := ReplaceWithPlaceholdersScored("Alice is here", entities, func(i int, orig string, entityType string) string {
			return "[[PERSON_1]]"
		})
		assert.Equal(t, "[[PERSON_1]] is here", result)
	})

	t.Run("conflicting higher score wins", func(t *testing.T) {
		entities := []ScoredSpan{
			{Span: Span{0, 10}, EntityType: "PERSON", Score: 0.7},
			{Span: Span{2, 8}, EntityType: "EMAIL", Score: 0.95},
		}
		result := ReplaceWithPlaceholdersScored("0123456789 end", entities, func(i int, orig string, entityType string) string {
			return "[[WIN_1]]"
		})
		// EMAIL wins (higher score), replaces 2-8, keeping 0-1 and 8-9
		assert.Equal(t, "01[[WIN_1]]89 end", result)
	})

	t.Run("non-overlapping both kept", func(t *testing.T) {
		entities := []ScoredSpan{
			{Span: Span{0, 5}, EntityType: "PERSON", Score: 0.9},
			{Span: Span{10, 15}, EntityType: "EMAIL", Score: 0.8},
		}
		result := ReplaceWithPlaceholdersScored("01234 567 89012 end", entities, func(i int, orig string, entityType string) string {
			return []string{"[[A]]", "[[B]]"}[i]
		})
		assert.Equal(t, "[[A]] 567 [[B]] end", result)
	})
}
