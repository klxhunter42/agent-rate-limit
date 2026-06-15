package masking

import (
	"strings"
	"testing"
)

// Regression: the U-word noise handling must be provider-aware.
//   - GLM models and OAuth providers (Claude/Gemini OAuth) emit "undefined" instead
//     of preserving [[TYPE_N]] placeholders, so glmNoiseMode=ON enables fallback.
//   - Standard API providers (Anthropic/Gemini API keys) preserve placeholders correctly
//     and can output legitimate "undefined" in code, so glmNoiseMode=OFF preserves it.
func TestStreamUnmasker_GLMNoiseModeGating(t *testing.T) {
	U := "undef" + "ined"

	t.Run("api-key mode preserves the word", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["[[IP_1]]"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil)
		u.SetGLMNoiseMode(false) // Standard API: preserve "undefined"
		got := u.ProcessChunk("typeof x === "+U) + u.Flush()
		if !strings.Contains(got, U) {
			t.Fatalf("api-key mode altered real content: %q", got)
		}
	})

	t.Run("oauth/glm mode handles the word", func(t *testing.T) {
		ctx := NewMaskContext()
		ctx.Mapping["undefined"] = "10.0.0.1"
		u := NewStreamUnmasker(ctx, nil) // default ON for OAuth/GLM
		got := u.ProcessChunk("ip="+U) + u.Flush()
		if strings.Contains(got, U) {
			t.Fatalf("oauth/glm mode leaked the noise token: %q", got)
		}
	})
}
