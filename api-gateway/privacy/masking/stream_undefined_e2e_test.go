package masking

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestStreamUndefined_SplitAcrossChunks simulates real SSE streaming where
// "undefinedundefinedundefinedundefined" arrives split across multiple chunks.
func TestStreamUndefined_SplitAcrossChunks(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "192.168.5.111"
	ctx.Mapping["[[ENV_PASSWORD_1]]"] = "MyP@ss123"

	t.Run("4x undefined split into 2 chunks", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunk("connect to undefinedundef")
		r2 := u.ProcessChunk("inedundefinedundefined done")
		full := r1 + r2
		t.Logf("r1=%q r2=%q full=%q", r1, r2, full)
		assert.NotContains(t, full, "undefined")
	})

	t.Run("4x undefined in one chunk - exact user bug", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		result := u.ProcessChunk("undefinedundefinedundefinedundefined")
		t.Logf("result=%q", result)
		assert.NotContains(t, result, "undefined")
	})

	t.Run("undefined split mid-word across 5 chunks", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		chunks := []string{"undef", "in", "edundef", "ined", "undefinedundefinedundefined"}
		var full string
		for _, c := range chunks {
			full += u.ProcessChunk(c)
		}
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
	})

	t.Run("undefined interleaved with real text across chunks", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunk("The server at undefined")
		r2 := u.ProcessChunk("undefined has password undefined")
		r3 := u.ProcessChunk("undefinedundefined end")
		full := r1 + r2 + r3
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
		assert.True(t, strings.Contains(full, "192.168.5.111") || strings.Contains(full, "MyP@ss123"))
	})
}

// TestStreamUndefined_NoContext_PreservesCode verifies that when there is no
// masking context, "undefined" from code examples is NOT stripped.
func TestStreamUndefined_NoContext_PreservesCode(t *testing.T) {
	u := NewStreamUnmasker(nil, nil)

	t.Run("typeof check", func(t *testing.T) {
		result := u.ProcessChunk(`if (typeof x === "undefined") return null;`)
		assert.Equal(t, `if (typeof x === "undefined") return null;`, result)
	})

	t.Run("variable named undefined", func(t *testing.T) {
		result := u.ProcessChunk(`const undefined = "not set";`)
		assert.Equal(t, `const undefined = "not set";`, result)
	})

	t.Run("JS code across chunks", func(t *testing.T) {
		r1 := u.ProcessChunk(`if (x === un`)
		r2 := u.ProcessChunk(`defined) {`)
		full := r1 + r2
		t.Logf("full=%q", full)
		assert.Contains(t, full, "undefined")
	})
}

// TestStreamUndefined_FlushBuffer verifies Flush() cleans up remaining undefined.
func TestStreamUndefined_FlushBuffer(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"

	t.Run("undefined in buffer gets cleaned on flush", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		u.ProcessChunk("connect to undefinedun")
		remaining := u.Flush()
		t.Logf("remaining=%q", remaining)
		assert.NotContains(t, remaining, "undefined")
	})
}

// TestStreamUndefined_ExactProductionScenario simulates the exact bug:
// GLM outputs "undefinedundefinedundefinedundefined" in a content_block_delta
func TestStreamUndefined_ExactProductionScenario(t *testing.T) {
	secCtx := NewMaskContext()
	secCtx.Mapping["[[IP_ADDRESS_1]]"] = "172.18.0.9"
	secCtx.Mapping["[[ENV_PASSWORD_1]]"] = "hunter2"

	piCtx := NewMaskContext()
	piCtx.Mapping["[[EMAIL_ADDRESS_1]]"] = "admin@corp.com"
	piCtx.Mapping["[[PHONE_NUMBER_1]]"] = "+66-81-234-5678"

	t.Run("all 4 values as concatenated undefined", func(t *testing.T) {
		u := NewStreamUnmasker(piCtx, secCtx)
		result := u.ProcessChunk("Connect to undefinedundefinedundefinedundefined with credentials undefinedundefined")
		t.Logf("result=%q", result)
		assert.NotContains(t, result, "undefined")
	})

	t.Run("mix of some placeholders preserved and some undefined", func(t *testing.T) {
		u2 := NewStreamUnmasker(piCtx, secCtx)
		result := u2.ProcessChunk("Server [[IP_ADDRESS_1]], user undefined, pass undefined, phone undefined")
		t.Logf("result=%q", result)
		assert.Contains(t, result, "172.18.0.9")
		assert.NotContains(t, result, "undefined")
	})

	t.Run("split across many SSE chunks like real stream", func(t *testing.T) {
		u3 := NewStreamUnmasker(piCtx, secCtx)
		// Simulate GLM streaming: text arrives character by character
		input := "The IP is undefinedundefined and email is undefinedundefined end"
		var full string
		for i := 0; i < len(input); i += 3 {
			end := i + 3
			if end > len(input) {
				end = len(input)
			}
			full += u3.ProcessChunk(input[i:end])
		}
		full += u3.Flush()
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
	})
}
