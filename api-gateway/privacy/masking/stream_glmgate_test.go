package masking

import (
	"strings"
	"testing"
)

// Regression: the U-word noise handling must be GLM-only. Capable models
// (Claude/Gemini/OpenAI) preserve  placeholders and emit the literal
// word as real content, so with glmNoiseMode OFF it must never be touched even
// when a masking context is active.
func TestStreamUnmasker_GLMNoiseModeGating(t *testing.T) {
	U := "undef" + "ined"

	t.Run("claude mode preserves the word", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		u.SetGLMNoiseMode(false)
		got := u.ProcessChunk("typeof x === "+U) + u.Flush()
		if !strings.Contains(got, U) {
			t.Fatalf("claude mode altered real content: %q", got)
		}
	})

	t.Run("glm mode handles the word", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["undefined"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil) // default ON
		got := u.ProcessChunk("ip="+U) + u.Flush()
		if strings.Contains(got, U) {
			t.Fatalf("glm mode leaked the noise token: %q", got)
		}
	})
}
