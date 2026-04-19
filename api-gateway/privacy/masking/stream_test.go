package masking

import (
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
