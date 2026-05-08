package masking

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamUnmasker_ProcessChunk(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[PERSON_1]]"] = "Alice"
	piiCtx.Counters["PERSON"] = 1

	secretsCtx := NewMaskContext()
	secretsCtx.Mapping["[[API_KEY_SK_1]]"] = "sk-abc123"
	secretsCtx.Counters["API_KEY_SK"] = 1

	t.Run("full placeholder in one chunk", func(t *testing.T) {
		u := NewStreamUnmasker(piiCtx, nil)
		result := u.ProcessChunk("Hello [[PERSON_1]]")
		assert.Equal(t, "Hello Alice", result)
	})

	t.Run("split placeholder across chunks", func(t *testing.T) {
		u := NewStreamUnmasker(piiCtx, nil)
		r1 := u.ProcessChunk("Hello [[PER")
		assert.Equal(t, "Hello ", r1)
		r2 := u.ProcessChunk("SON_1]] world")
		assert.Equal(t, "Alice world", r2)
	})

	t.Run("two-pass PII then secrets", func(t *testing.T) {
		u := NewStreamUnmasker(piiCtx, secretsCtx)
		result := u.ProcessChunk("[[PERSON_1]] key=[[API_KEY_SK_1]]")
		assert.Equal(t, "Alice key=sk-abc123", result)
	})
}

func TestStreamUnmasker_Flush(t *testing.T) {
	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[PERSON_1]]"] = "Alice"

	t.Run("empty buffer", func(t *testing.T) {
		u := NewStreamUnmasker(nil, nil)
		assert.Equal(t, "", u.Flush())
	})

	t.Run("remaining buffer", func(t *testing.T) {
		u := NewStreamUnmasker(piiCtx, nil)
		u.ProcessChunk("Hello [[PER")
		flushed := u.Flush()
		// "[[PER" is partial, not a complete placeholder, so it passes through
		assert.Equal(t, "[[PER", flushed)
	})

	t.Run("both secrets and PII buffers flushed in correct order", func(t *testing.T) {
		secCtx := NewMaskContext()
		secCtx.Mapping["[[API_KEY_SK_1]]"] = "sk-abc123"
		secCtx.Counters["API_KEY_SK"] = 1

		piCtx := NewMaskContext()
		piCtx.Mapping["[[PERSON_1]]"] = "Alice"
		piCtx.Counters["PERSON"] = 1

		u := NewStreamUnmasker(piCtx, secCtx)
		// Feed a chunk where secrets placeholder appears inside PII placeholder text
		// Both will be partially buffered, then flush must unmask secrets then PII
		u.ProcessChunk("[[PERSON_1]] key=[[API_KEY_SK_1]]")
		// After processing, both should be fully unmasked inline, buffers empty
		assert.Equal(t, "", u.Flush())

		// Now test where buffers actually have content on flush
		u2 := NewStreamUnmasker(piCtx, secCtx)
		u2.ProcessChunk("Hello [[PERSON_1]] key=[[API_")
		// "[[API_" is partial -> buffered in secretsBuffer
		// secrets unmasking produces: "Hello Alice key=[[API_" (secrets buffer holds "[[API_")
		// PII is fully resolved, no piiBuffer
		flushed := u2.Flush()
		// secretsBuffer "[[API_" is not a complete placeholder, passes through as-is
		assert.Equal(t, "[[API_", flushed)
	})

	t.Run("secrets buffer contains PII placeholder requiring two-pass flush", func(t *testing.T) {
		secCtx := NewMaskContext()
		secCtx.Mapping["[[API_KEY_SK_1]]"] = "sk-abc123"
		secCtx.Counters["API_KEY_SK"] = 1

		piCtx := NewMaskContext()
		piCtx.Mapping["[[PERSON_1]]"] = "Alice"
		piCtx.Counters["PERSON"] = 1

		// Simulate: secretsBuffer has completed text that includes a PII placeholder,
		// and piiBuffer has content too.
		u := NewStreamUnmasker(piCtx, secCtx)
		// Manually set buffers to simulate the scenario where secrets unmasking
		// produces text containing PII placeholders
		u.secretsBuffer = "key=sk-abc123 [[PERSON_1]]"
		u.piiBuffer = "Hello "

		flushed := u.Flush()
		assert.Equal(t, "Hello key=sk-abc123 Alice", flushed)
		assert.Equal(t, "", u.secretsBuffer)
		assert.Equal(t, "", u.piiBuffer)
	})
}

func TestStreamUnmasker_HasContexts(t *testing.T) {
	t.Run("no contexts", func(t *testing.T) {
		u := NewStreamUnmasker(nil, nil)
		assert.False(t, u.HasContexts())
	})

	t.Run("with contexts", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[X_1]]"] = "val"
		u := NewStreamUnmasker(ctx, nil)
		assert.True(t, u.HasContexts())
	})
}

func TestStripLeftoverPlaceholders(t *testing.T) {
	t.Run("no placeholders", func(t *testing.T) {
		assert.Equal(t, "hello world", stripLeftoverPlaceholders("hello world"))
	})

	t.Run("single leftover", func(t *testing.T) {
		assert.Equal(t, "hello  world", stripLeftoverPlaceholders("hello [[IP_ADDRESS_1]] world"))
	})

	t.Run("multiple leftovers", func(t *testing.T) {
		assert.Equal(t, "user: admin, ip: ",
			stripLeftoverPlaceholders("user: [[EMAIL_ADDRESS_1]]admin, ip: [[IP_ADDRESS_3]]"))
	})
}

func TestStripStrayUndefined(t *testing.T) {
	t.Run("no undefined", func(t *testing.T) {
		assert.Equal(t, "hello world", stripStrayUndefined("hello world"))
	})

	t.Run("budget exhausted - concatenated undefined", func(t *testing.T) {
		assert.Equal(t, "172.18.0.9", stripStrayUndefined("undefinedundefinedundefinedundefined172.18.0.9"))
	})

	t.Run("mixed undefined and content", func(t *testing.T) {
		assert.Equal(t, "IP:172.18.0.9", stripStrayUndefined("IP: undefinedundefined172.18.0.9"))
	})

	t.Run("trailing undefined", func(t *testing.T) {
		assert.Equal(t, "value", stripStrayUndefined("value undefined"))
	})
}

func TestStreamUnmasker_UndefinedFallbackBudgetExhaustion(t *testing.T) {
	// Simulate: 2 originals but GLM outputs 5 "undefined" strings.
	piCtx := NewMaskContext()
	piCtx.Mapping["[[IP_ADDRESS_1]]"] = "172.18.0.9"
	piCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "admin@example.com"

	u := NewStreamUnmasker(piCtx, nil)

	// GLM outputs: "undefinedundefinedundefinedundefinedundefined172.18.0.9"
	// Budget has 2 originals. After consuming 2 "undefined", 3 remain.
	// stripStrayUndefined must clean up the rest.
	result := u.ProcessChunk("undefinedundefinedundefinedundefinedundefined172.18.0.9")

	// Should contain the two originals but NOT "undefined"
	assert.NotContains(t, result, "undefined")
	// Should contain at least one of the original values
	assert.True(t, strings.Contains(result, "172.18.0.9") || strings.Contains(result, "admin@example.com"))
}

func TestStreamUnmasker_LeftoverPlaceholderStripped(t *testing.T) {
	piCtx := NewMaskContext()
	piCtx.Mapping["[[IP_ADDRESS_1]]"] = "172.18.0.9"

	u := NewStreamUnmasker(piCtx, nil)

	// If GLM somehow leaves a partial placeholder that wasn't in the mapping
	result := u.ProcessChunk("user IP is [[IP_ADDRESS_1]] and [[UNKNOWN_TYPE_9]]")
	assert.Equal(t, "user IP is 172.18.0.9 and ", result)
	assert.NotContains(t, result, "[[")
}

func TestStreamUnmasker_GLMConcatPattern(t *testing.T) {
	// Reproduce the real pattern: "undefinedundefinedundefinedundefinedundefined172.18.0.9"
	piCtx := NewMaskContext()
	piCtx.Mapping["[[IP_ADDRESS_1]]"] = "172.18.0.9"

	u := NewStreamUnmasker(piCtx, nil)

	result := u.ProcessChunk("undefinedundefinedundefinedundefinedundefined172.18.0.9")
	assert.NotContains(t, result, "undefined")
	assert.Contains(t, result, "172.18.0.9")
}

func TestProcessStreamChunk(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[KEY_1]]"] = "secret"

	t.Run("no partial", func(t *testing.T) {
		out, remaining := processStreamChunk("", "use [[KEY_1]] now", ctx)
		assert.Equal(t, "use secret now", out)
		assert.Equal(t, "", remaining)
	})

	t.Run("partial buffered", func(t *testing.T) {
		out, remaining := processStreamChunk("", "use [[KE", ctx)
		assert.Equal(t, "use ", out)
		assert.Equal(t, "[[KE", remaining)
	})
}
