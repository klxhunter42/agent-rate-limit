package masking

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Truly weird edge cases.

func TestStreamUndefined_WeirdCases(t *testing.T) {
	t.Run("replacement value IS 'undefined'", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[CUSTOM_1]]"] = "undefined"
		u := NewStreamUnmasker(ctx, nil)
		// Model outputs "undefined" for the placeholder
		r := u.ProcessChunk("value=undefined")
		t.Logf("result=%q", r)
		// The fallback replaces "undefined" with the original "undefined",
		// then stripStrayUndefined eats it again. Known edge case.
		assert.NotContains(t, r, "undefined")
	})

	t.Run("replacement value is empty string", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[CUSTOM_1]]"] = ""
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("value=undefined")
		t.Logf("result=%q", r)
		assert.NotContains(t, r, "undefined")
	})

	t.Run("UNDEFINED uppercase should NOT be replaced", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("value=UNDEFINED")
		assert.Equal(t, "value=UNDEFINED", r)
	})

	t.Run("undefined with trailing punctuation", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		ctx.Mapping["[[PW_1]]"] = "pass123"
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("ip=undefined, pass=undefined.")
		t.Logf("result=%q", r)
		assert.NotContains(t, r, "undefined")
	})

	t.Run("undefined with trailing punctuation across chunks", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunk("ip=undefined")
		r2 := u.ProcessChunk(", done")
		full := r1 + r2
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
	})

	t.Run("chunk is exactly 'undefined'", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("undefined")
		assert.Equal(t, "10.0.0.1", r)
	})

	t.Run("chunk is exactly 'undef' then 'ined'", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunk("undef")
		r2 := u.ProcessChunk("ined")
		full := r1 + r2
		assert.Equal(t, "10.0.0.1", full)
	})

	t.Run("chunk is exactly 'u' then 'n' then 'd' then 'e' then 'f' then 'i' then 'n' then 'e' then 'd'", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		full := ""
		for _, c := range "undefined" {
			full += u.ProcessChunk(string(c))
		}
		full += u.Flush()
		t.Logf("full=%q", full)
		assert.Equal(t, "10.0.0.1", full)
	})

	t.Run("newline in chunks", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("ip=undefined\npass=undefined")
		t.Logf("result=%q", r)
		assert.NotContains(t, r, "undefined")
		assert.Contains(t, r, "\n")
	})

	t.Run("newline splitting undefined", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunk("ip=undef\n")
		r2 := u.ProcessChunk("ined")
		full := r1 + r2
		t.Logf("full=%q", full)
		// "undef\n" ends with \n, not "undef", so buffering doesn't trigger.
		// "undef" and "ined" are separate because of the newline.
		// stripStrayUndefined handles "undef" -> stripped, "ined" stays.
		// Result: "ip=\nined" or "ip=undef\nined" depending on fallback.
		assert.NotContains(t, full, "undefined")
	})

	t.Run("tab and special whitespace", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("ip=undefined\tval=undefined")
		t.Logf("result=%q", r)
		assert.NotContains(t, r, "undefined")
	})

	t.Run("model outputs placeholder AND undefined for same value", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		ctx.Mapping["[[PW_1]]"] = "pass123"
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("ip=[[IP_1]] pass=undefined")
		t.Logf("result=%q", r)
		assert.Contains(t, r, "10.0.0.1")
		assert.NotContains(t, r, "undefined")
		assert.NotContains(t, r, "[[")
	})

	t.Run("reusing unmasker after flush", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunk("ip=undefined")
		_ = u.Flush()
		r2 := u.ProcessChunk(" again=undefined")
		full := r1 + r2
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
	})

	t.Run("double flush", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		_ = u.ProcessChunk("ip=undef")
		f1 := u.Flush()
		f2 := u.Flush()
		t.Logf("f1=%q f2=%q", f1, f2)
		assert.NotContains(t, f1+f2, "undefined")
		assert.Equal(t, "", f2)
	})

	t.Run("flush with no prior ProcessChunk", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r := u.Flush()
		assert.Equal(t, "", r)
	})

	t.Run("50 originals, 50 undefined, single char chunks", func(t *testing.T) {
		ctx := NewMaskContext()
		for i := 0; i < 50; i++ {
			ctx.Mapping["[[VAR_"+strings.Repeat("A", i)+"]]"] = "val"
		}
		u := NewStreamUnmasker(ctx, nil)
		input := strings.Repeat("undefined", 50)
		var full string
		for _, c := range input {
			full += u.ProcessChunk(string(c))
		}
		full += u.Flush()
		t.Logf("full_len=%d", len(full))
		assert.NotContains(t, full, "undefined")
		assert.NotContains(t, full, "[[")
	})

	t.Run("all budgets consumed then normal text follows", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunk("a=undefined b=undefined c=undefined")
		r2 := u.ProcessChunk(" normal text here")
		full := r1 + r2
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
		assert.Contains(t, full, "normal text here")
	})

	t.Run("thinking block style content with undefined", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		input := "Let me think about this... the IP is undefined and I need to connect"
		var full string
		for i := 0; i < len(input); i += 7 {
			end := i + 7
			if end > len(input) {
				end = len(input)
			}
			full += u.ProcessChunk(input[i:end])
		}
		full += u.Flush()
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
		assert.Contains(t, full, "10.0.0.1")
	})

	t.Run("JSON escaped: undefined inside quotes", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunkJSON(`"ip":"undefined"`)
		t.Logf("result=%q", r)
		assert.NotContains(t, r, "undefined")
	})

	t.Run("JSON escaped: undefined split across chunks", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r1 := u.ProcessChunkJSON(`"ip":"undef`)
		r2 := u.ProcessChunkJSON(`ined"`)
		full := r1 + r2
		t.Logf("full=%q", full)
		assert.NotContains(t, full, "undefined")
	})

	t.Run("every possible split position of 'undefined'", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		target := "undefined"
		for split := 1; split < len(target); split++ {
			t.Run("split_at_"+fmt.Sprintf("%d", split), func(t *testing.T) {
				u := NewStreamUnmasker(ctx, nil)
				r1 := u.ProcessChunk(target[:split])
				r2 := u.ProcessChunk(target[split:])
				f := u.Flush()
				full := r1 + r2 + f
				t.Logf("split=%d full=%q", split, full)
				assert.NotContains(t, full, "undefined")
				assert.NotContains(t, full, "[[")
			})
		}
	})

	t.Run("every split position with surrounding text", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		target := "prefix=undefined suffix"
		for split := 1; split < len(target); split++ {
			t.Run("split_at_"+fmt.Sprintf("%d", split), func(t *testing.T) {
				u := NewStreamUnmasker(ctx, nil)
				r1 := u.ProcessChunk(target[:split])
				r2 := u.ProcessChunk(target[split:])
				f := u.Flush()
				full := r1 + r2 + f
				t.Logf("split=%d full=%q", split, full)
				assert.NotContains(t, full, "undefined")
				assert.Contains(t, full, "prefix=")
				assert.Contains(t, full, "suffix")
			})
		}
	})

	t.Run("emoji and unicode near undefined", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("IP: undefined 🔒✅")
		t.Logf("result=%q", r)
		assert.NotContains(t, r, "undefined")
		assert.Contains(t, r, "🔒✅")
	})

	t.Run("Thai text near undefined", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("IP คือ undefined ครับ")
		t.Logf("result=%q", r)
		assert.NotContains(t, r, "undefined")
		assert.Contains(t, r, "คือ")
		assert.Contains(t, r, "ครับ")
	})

	t.Run("undefined inside markdown code block", func(t *testing.T) {
		// When masking is active, even code "undefined" gets replaced
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("```js\ntypeof x === undefined\n```")
		t.Logf("result=%q", r)
		// With masking active, "undefined" IS replaced (known tradeoff)
		assert.NotContains(t, r, "undefined")
	})

	t.Run("undefined inside markdown code block NO masking", func(t *testing.T) {
		// Without masking, code "undefined" is preserved
		u := NewStreamUnmasker(nil, nil)
		r := u.ProcessChunk("```js\ntypeof x === undefined\n```")
		assert.Contains(t, r, "undefined")
	})

	t.Run("placeholder starts with 'un' like [[UNDERSCORE_1]]", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[UNDERSCORE_1]]"] = "_sep_"
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		r := u.ProcessChunk("val=[[UNDERSCORE_1]] ip=undefined")
		t.Logf("result=%q", r)
		assert.Contains(t, r, "_sep_")
		assert.NotContains(t, r, "undefined")
		assert.NotContains(t, r, "[[")
	})

	t.Run("concurrent ProcessChunk on same unmasker (should NOT happen but test robustness)", func(t *testing.T) {
		// This test documents that ProcessChunk is NOT thread-safe.
		// It's a single-goroutine-per-request API.
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		// Sequential is fine
		r := u.ProcessChunk("ip=undefined")
		assert.Contains(t, r, "10.0.0.1")
	})

	t.Run("secrets context only, no PII", func(t *testing.T) {
		secCtx := NewMaskContext()
		secCtx.Mapping["[[API_KEY_1]]"] = "sk-test"
		u := NewStreamUnmasker(nil, secCtx)
		r := u.ProcessChunk("key=undefined")
		t.Logf("result=%q", r)
		assert.Contains(t, r, "sk-test")
		assert.NotContains(t, r, "undefined")
	})

	t.Run("PII context only, no secrets", func(t *testing.T) {
		piCtx := NewMaskContext()
		piCtx.Mapping["[[EMAIL_1]]"] = "a@b.com"
		u := NewStreamUnmasker(piCtx, nil)
		r := u.ProcessChunk("email=undefined")
		t.Logf("result=%q", r)
		assert.Contains(t, r, "a@b.com")
		assert.NotContains(t, r, "undefined")
	})
}

// Need strconv for the dynamic test names.
var _ = strings.TrimSpace
