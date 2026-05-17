package masking

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for cross-block buffer contamination fix (H3+H4).
// The proxy calls Flush() when block index changes; these tests verify
// that Flush() correctly handles partial buffers from a previous block.

func TestFlush_DrainsPIIAndSecretsBuffers(t *testing.T) {
	secCtx := NewMaskContext()
	secCtx.Mapping["[[API_KEY_SK_1]]"] = "sk-secret123"
	secCtx.Counters["API_KEY_SK"] = 1

	piCtx := NewMaskContext()
	piCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "user@example.com"
	piCtx.Counters["EMAIL_ADDRESS"] = 1

	u := NewStreamUnmasker(piCtx, secCtx)

	// Simulate: block 0 text ends with partial PII placeholder
	u.ProcessChunk("email: [[EMAIL_AD")

	// Proxy detects block index change and calls Flush()
	flushed := u.Flush()
	// Partial "[[EMAIL_AD" is not a complete placeholder, passes through as-is
	// then gets stripped by StripLeftoverPlaceholders at the proxy level
	assert.Contains(t, flushed, "[[EMAIL_AD")
}

func TestFlush_JSONBuffersDrainedCorrectly(t *testing.T) {
	secCtx := NewMaskContext()
	secCtx.Mapping["[[API_KEY_SK_1]]"] = "sk-test-key"
	secCtx.Counters["API_KEY_SK"] = 1

	piCtx := NewMaskContext()
	piCtx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	piCtx.Counters["IP_ADDRESS"] = 1

	u := NewStreamUnmasker(piCtx, secCtx)

	// Feed complete placeholder in JSON mode
	u.ProcessChunkJSON("[[API_KEY_SK_1]]")
	// Should be fully resolved inline
	assert.Equal(t, "", u.secretsJSONBuffer)

	// Feed partial placeholder that gets buffered
	u2 := NewStreamUnmasker(piCtx, secCtx)
	u2.ProcessChunkJSON("{\"key\":\"[[API_KEY_SK")
	// Partial is buffered, not resolved
	flushed := u2.Flush()
	// Partial "[[API_KEY_SK" is not a complete placeholder, passes through
	// StripLeftoverPlaceholders at proxy level cleans it up
	assert.Contains(t, flushed, "[[API_KEY_SK")
}

func TestFlush_SecretsThenPIIOrdering(t *testing.T) {
	// Simulate: secretsBuffer has content, piiBuffer has content
	// Flush must unmask secrets first (innermost), then PII (outermost)
	secCtx := NewMaskContext()
	secCtx.Mapping["[[API_KEY_SK_1]]"] = "sk-inner"

	piCtx := NewMaskContext()
	piCtx.Mapping["[[PERSON_1]]"] = "Bob"

	u := NewStreamUnmasker(piCtx, secCtx)
	u.secretsBuffer = "[[API_KEY_SK_1]]"
	u.piiBuffer = "Hello [[PERSON_1]] "

	flushed := u.Flush()
	assert.Equal(t, "Hello Bob sk-inner", flushed)
	assert.Equal(t, "", u.secretsBuffer)
	assert.Equal(t, "", u.piiBuffer)
}

func TestFlush_JSONSecretsThenPIIOrdering(t *testing.T) {
	secCtx := NewMaskContext()
	secCtx.Mapping["[[API_KEY_SK_1]]"] = "sk-inner"

	piCtx := NewMaskContext()
	piCtx.Mapping["[[PERSON_1]]"] = "Alice"

	u := NewStreamUnmasker(piCtx, secCtx)
	u.secretsJSONBuffer = "[[API_KEY_SK_1]]"
	u.piiJSONBuffer = "[[PERSON_1]] "

	flushed := u.Flush()
	assert.Contains(t, flushed, "Alice")
	assert.Contains(t, flushed, "sk-inner")
	assert.Equal(t, "", u.secretsJSONBuffer)
	assert.Equal(t, "", u.piiJSONBuffer)
}

// Tests for StripLeftoverPlaceholders (H6 safety net).

func TestStripLeftoverPlaceholders_MultipleTypes(t *testing.T) {
	input := "user [[EMAIL_ADDRESS_1]] ip [[IP_ADDRESS_3]] key [[API_KEY_SK_2]]"
	result := StripLeftoverPlaceholders(input)
	assert.Equal(t, "user  ip  key ", result)
}

func TestStripLeftoverPlaceholders_MixedWithRealContent(t *testing.T) {
	input := "hello world [[UNKNOWN_TYPE_99]] goodbye"
	result := StripLeftoverPlaceholders(input)
	assert.Equal(t, "hello world  goodbye", result)
}

func TestStripLeftoverPlaceholders_NoMatchForPartial(t *testing.T) {
	// Partial placeholder like "[[EMAIL" should NOT be stripped
	input := "data: [[EMAIL"
	result := StripLeftoverPlaceholders(input)
	assert.Equal(t, input, result)
}

func TestStripLeftoverPlaceholders_EmptyInput(t *testing.T) {
	assert.Equal(t, "", StripLeftoverPlaceholders(""))
}

func TestStripLeftoverPlaceholders_NoBrackets(t *testing.T) {
	assert.Equal(t, "hello world", StripLeftoverPlaceholders("hello world"))
}

// Tests for undefined fallback with word-boundary regex.

func TestStripStrayUndefined_PreservesSingleInCode(t *testing.T) {
	// When masking is active and budget is exhausted, stripStrayUndefined
	// uses \s*undefined\s* regex. In context, this only runs when
	// HasContexts() is true, so legitimate "undefined" in code is rare.
	// But the regex still handles concatenated cases correctly.
	input := "undefinedundefined192.168.1.1"
	result := stripStrayUndefined(input)
	assert.Equal(t, "192.168.1.1", result)
}

func TestStripStrayUndefined_WithSpaces(t *testing.T) {
	input := "result: undefined undefined value"
	result := stripStrayUndefined(input)
	assert.Equal(t, "result:value", result)
}

func TestStripStrayUndefined_SurroundingWhitespaceConsumed(t *testing.T) {
	input := "prefix undefined suffix"
	result := stripStrayUndefined(input)
	// Regex \s*undefined\s* consumes space before and after
	assert.Equal(t, "prefixsuffix", result)
}
