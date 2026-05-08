package masking

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Edge cases for undefined streaming fix.

func TestStreamUndefined_EdgeCases(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
	ctx.Mapping["[[API_KEY_SK_1]]"] = "sk-secret123"

	t.Run("empty chunks", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("")
		assert.Equal(t, "", r)
		r = u.ProcessChunk("hello")
		assert.Equal(t, "hello", r)
		assert.Equal(t, "", u.Flush())
	})

	t.Run("single char chunks - extreme split", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		input := "ip=undefined key=undefined"
		var full string
		for _, ch := range input {
			full += u.ProcessChunk(string(ch))
		}
		full += u.Flush()
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
		assert.Contains(t, full, "10.0.0.1")
	})

	t.Run("whitespace-only chunks between undefined", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunk("ip=undefined")
		r2 := u.ProcessChunk(" ")
		r3 := u.ProcessChunk("key=undefined")
		full := r1 + r2 + r3
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
	})

	t.Run("undefined at very start of stream", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("undefined is the IP")
		t.Logf("result=%q", r)
		assert.NotContains(t, r, "undefined")
		assert.True(t, strings.Contains(r, "10.0.0.1") || strings.Contains(r, "sk-secret123"))
	})

	t.Run("undefined at very end of stream then flush", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunk("IP is ")
		r2 := u.ProcessChunk("undefi")
		f := u.Flush()
		full := r1 + r2 + f
		t.Logf("full=%q", full)
		// "undefi" is buffered, flush should clean it
		assert.NotContains(t, full, "undefined")
	})

	t.Run("undefine (8 chars) partial at chunk boundary", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunk("ip=undefine")
		r2 := u.ProcessChunk("d done")
		full := r1 + r2
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
		assert.True(t, strings.Contains(full, "10.0.0.1") || strings.Contains(full, "sk-secret123"))
	})

	t.Run("budget exactly matches undefined count", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		// 2 originals, 2 undefined
		r := u.ProcessChunk("a=undefined b=undefined")
		t.Logf("result=%q", r)
		assert.NotContains(t, r, "undefined")
	})

	t.Run("more undefined than originals - budget exhaustion across chunks", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		// 2 originals, 5 undefined split across chunks
		r1 := u.ProcessChunk("a=undefined b=undefined")
		r2 := u.ProcessChunk(" c=undefined d=undefined")
		r3 := u.ProcessChunk(" e=undefined end")
		full := r1 + r2 + r3
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
	})

	t.Run("replacement value contains 'un' prefix", func(t *testing.T) {
		specialCtx := NewMaskContext()
		specialCtx.Mapping["[[CUSTOM_1]]"] = "val_under_score"
		specialCtx.Mapping["[[CUSTOM_2]]"] = "another_undefined_val"
		u := NewStreamUnmasker(specialCtx, nil)
		// 2 originals, 2 undefined in response
		r := u.ProcessChunk("x=undefined y=undefined done")
		t.Logf("result=%q", r)
		assert.NotContains(t, r, "undefined")
	})

	t.Run("split u-n-d-e-f-i-n-e-d one char at a time", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		chars := []string{"u", "n", "d", "e", "f", "i", "n", "e", "d"}
		var full string
		for _, c := range chars {
			full += u.ProcessChunk(c)
		}
		full += u.Flush()
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
		assert.True(t, strings.Contains(full, "10.0.0.1") || strings.Contains(full, "sk-secret123"))
	})

	t.Run("chunk ends with 'u' that is not start of undefined", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		// "you" - the "u" at end should be buffered and prepended
		r1 := u.ProcessChunk("yo")
		r2 := u.ProcessChunk("u are")
		r3 := u.ProcessChunk(" cool")
		full := r1 + r2 + r3
		t.Logf("full=%q", full)
		// Should reconstruct "you are cool" correctly
		assert.Contains(t, full, "you are cool")
	})

	t.Run("real code 'typeof x === undefined' preserved when no masking", func(t *testing.T) {
		u := NewStreamUnmasker(nil, nil)
		r := u.ProcessChunk("if (typeof x === undefined) return;")
		assert.Equal(t, "if (typeof x === undefined) return;", r)
	})

	t.Run("real code 'typeof x === undefined' preserved across chunks no masking", func(t *testing.T) {
		u := NewStreamUnmasker(nil, nil)
		r1 := u.ProcessChunk("if (typeof x === un")
		r2 := u.ProcessChunk("defined) return;")
		full := r1 + r2
		t.Logf("full=%q", full)
		assert.Contains(t, full, "undefined")
	})

	t.Run("mixed: placeholder + undefined + real undefined-like text", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		// First undefined is GLM artifact, second is real code
		// But budget will consume both - this tests ordering
		r1 := u.ProcessChunk("IP=undefined ")
		r2 := u.ProcessChunk("and typeof y === undefined end")
		full := r1 + r2
		t.Logf("full=%q", full)
		// Budget has 2 originals, 2 undefined in text
		assert.NotContains(t, full, "undefined")
	})

	t.Run("ProcessChunkJSON same split behavior", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunkJSON("ip=undef")
		r2 := u.ProcessChunkJSON("ined done")
		full := r1 + r2
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
		assert.True(t, strings.Contains(full, "10.0.0.1") || strings.Contains(full, "sk-secret123"))
	})

	t.Run("placeholder unmasking then undefined fallback", func(t *testing.T) {
		// Simulate: some placeholders preserved by model, some output as "undefined"
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("IP=[[IP_ADDRESS_1]] key=undefined")
		t.Logf("result=%q", r)
		assert.Contains(t, r, "10.0.0.1")
		assert.Contains(t, r, "sk-secret123")
		assert.NotContains(t, r, "undefined")
		assert.NotContains(t, r, "[[")
	})

	t.Run("placeholder split across chunks then undefined", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunk("IP=[[IP_AD")
		r2 := u.ProcessChunk("DRESS_1]] key=undefined")
		full := r1 + r2
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
		assert.NotContains(t, full, "[[")
	})

	t.Run("10 concatenated undefined split into 2-char chunks", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		// 2 originals, 10 "undefined" = massive budget exhaustion
		input := strings.Repeat("undefined", 10)
		var full string
		for i := 0; i < len(input); i += 2 {
			end := i + 2
			if end > len(input) {
				end = len(input)
			}
			full += u.ProcessChunk(input[i:end])
		}
		full += u.Flush()
		t.Logf("full=%q len=%d", full, len(full))
		assert.NotContains(t, full, "undefined")
		// Should contain the 2 originals
		assert.True(t, strings.Contains(full, "10.0.0.1") || strings.Contains(full, "sk-secret123"))
	})

	t.Run("ReplaceDirect does not use undefined buffer", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		// ReplaceDirect is unbuffered - should work without buffering
		r := u.ReplaceDirect("ip=[[IP_ADDRESS_1]] key=undefined")
		t.Logf("result=%q", r)
		assert.Contains(t, r, "10.0.0.1")
		assert.NotContains(t, r, "[[")
		// ReplaceDirect doesn't run undefined fallback - "undefined" remains
		// This is expected: ReplaceDirect only does placeholder restoration
	})

	t.Run("consecutive ProcessChunk and ProcessChunkJSON on same unmasker", func(t *testing.T) {
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunk("ip=undef")
		r2 := u.ProcessChunkJSON("ined ") // different path but shares undefinedBuffer
		r3 := u.ProcessChunk("done")
		full := r1 + r2 + r3
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
	})
}
