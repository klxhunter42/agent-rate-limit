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

func TestNextPlaceholderFor_Deterministic(t *testing.T) {
	ctx1 := NewMaskContext()
	ph1 := ctx1.NextPlaceholderFor("PHONE_NUMBER", "+66812345678")
	ctx1.Mapping[ph1] = "+66812345678"
	ctx1.ReverseMap["+66812345678"] = ph1

	ctx2 := NewMaskContext()
	ph2 := ctx2.NextPlaceholderFor("PHONE_NUMBER", "+66812345678")

	assert.Equal(t, ph1, ph2, "same original should get same placeholder across contexts")
}

func TestNextPlaceholderFor_DifferentValues(t *testing.T) {
	ctx := NewMaskContext()
	ph1 := ctx.NextPlaceholderFor("PHONE_NUMBER", "+66811111111")
	ph2 := ctx.NextPlaceholderFor("PHONE_NUMBER", "+66822222222")

	assert.NotEqual(t, ph1, ph2, "different originals should get different placeholders")
}

func TestNextPlaceholderFor_NoCollisionBetweenTypes(t *testing.T) {
	ctx := NewMaskContext()
	ph1 := ctx.NextPlaceholderFor("EMAIL", "user@example.com")
	ctx2 := NewMaskContext()
	ph2 := ctx2.NextPlaceholderFor("PHONE_NUMBER", "user@example.com")

	assert.NotEqual(t, ph1, ph2, "different entity types should produce different placeholders")
}

func TestNextPlaceholderFor_CollisionAvoidance(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[PHONE_NUMBER_1234]]"] = "existing"

	ph := ctx.NextPlaceholderFor("PHONE_NUMBER", "test-value")
	assert.NotEqual(t, "[[PHONE_NUMBER_1234]]", ph)
}
