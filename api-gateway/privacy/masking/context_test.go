package masking

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewMaskContext(t *testing.T) {
	ctx := NewMaskContext()
	assert.NotNil(t, ctx.Mapping)
	assert.NotNil(t, ctx.ReverseMap)
	assert.NotNil(t, ctx.Counters)
}

func TestGeneratePlaceholder(t *testing.T) {
	assert.Equal(t, "[[API_KEY_SK_1]]", GeneratePlaceholder("API_KEY_SK", 1))
	assert.Equal(t, "[[PERSON_42]]", GeneratePlaceholder("PERSON", 42))
}

func TestNextPlaceholder(t *testing.T) {
	ctx := NewMaskContext()
	assert.Equal(t, "[[API_KEY_SK_1]]", ctx.NextPlaceholder("API_KEY_SK"))
	assert.Equal(t, "[[API_KEY_SK_2]]", ctx.NextPlaceholder("API_KEY_SK"))
	assert.Equal(t, "[[PERSON_1]]", ctx.NextPlaceholder("PERSON"))
}

func TestRestorePlaceholders(t *testing.T) {
	ctx := NewMaskContext()
	ph := ctx.NextPlaceholder("API_KEY_SK")
	ctx.Mapping[ph] = "sk-abc123secretkey456"

	text := "Use key [[API_KEY_SK_1]] for auth"
	restored := ctx.RestorePlaceholders(text)
	assert.Equal(t, "Use key sk-abc123secretkey456 for auth", restored)
}

func TestRestorePlaceholders_LongestFirst(t *testing.T) {
	ctx := NewMaskContext()
	ph1 := ctx.NextPlaceholder("PERSON")
	ctx.Mapping[ph1] = "Alice" // [[PERSON_1]]
	ph2 := ctx.NextPlaceholder("PERSON")
	ctx.Mapping[ph2] = "Bob" // [[PERSON_2]]

	// [[PERSON_10]] should not be partially matched by [[PERSON_1]]
	ctx.Mapping["[[PERSON_10]]"] = "Charlie"

	text := "[[PERSON_10]] and [[PERSON_1]] and [[PERSON_2]]"
	restored := ctx.RestorePlaceholders(text)
	assert.Equal(t, "Charlie and Alice and Bob", restored)
}

func TestRestorePlaceholders_Empty(t *testing.T) {
	ctx := NewMaskContext()
	assert.Equal(t, "hello", ctx.RestorePlaceholders("hello"))
}

func TestRestorePlaceholders_MultipleOccurrences(t *testing.T) {
	ctx := NewMaskContext()
	ph := ctx.NextPlaceholder("KEY")
	ctx.Mapping[ph] = "secret"
	ctx.Mapping["[[KEY_2]]"] = "other"

	text := "[[KEY_1]] appears twice: [[KEY_1]] and [[KEY_2]]"
	restored := ctx.RestorePlaceholders(text)
	assert.Equal(t, "secret appears twice: secret and other", restored)
}
