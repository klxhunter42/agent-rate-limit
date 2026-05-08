package privacy

import (
	"testing"

	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
	"github.com/stretchr/testify/assert"
)

// TestNonStreamUndefinedFix verifies the non-streaming path handles GLM's "undefined" output.
func TestNonStream_UndefinedReplacement(t *testing.T) {
	// Build a MaskResult simulating what MaskRequest would produce
	result := &MaskResult{
		HasSecrets: true,
		SecretsCtx: masking.NewMaskContext(),
		HasPII:     false,
		PIICtx:     masking.NewMaskContext(), // must be non-nil to avoid panic in log
	}
	result.SecretsCtx.Mapping["[[IP_ADDRESS_1]]"] = "192.168.5.111"
	result.SecretsCtx.Mapping["[[ENV_PASSWORD_1]]"] = "supersecret"

	p := NewPipeline(DefaultConfig(), nil)

	t.Run("concatenated undefined without placeholders", func(t *testing.T) {
		// GLM outputs "undefinedundefinedundefinedundefined" instead of [[IP_ADDRESS_1]][[ENV_PASSWORD_1]]
		body := []byte(`{"content":[{"type":"text","text":"Connect to undefinedundefinedundefinedundefined with password undefined"}]}`)
		unmasked := p.UnmaskResponse(body, result)
		s := string(unmasked)
		t.Logf("result: %s", s)
		assert.NotContains(t, s, "undefined", "all undefined must be replaced/stripped")
	})

	t.Run("mixed placeholders and undefined", func(t *testing.T) {
		// Some placeholders preserved, some output as "undefined"
		body := []byte(`{"content":[{"type":"text","text":"IP=[[IP_ADDRESS_1]] pass=undefined"}]}`)
		unmasked := p.UnmaskResponse(body, result)
		s := string(unmasked)
		t.Logf("result: %s", s)
		assert.NotContains(t, s, "undefined")
		assert.Contains(t, s, "192.168.5.111")
		assert.Contains(t, s, "supersecret")
	})

	t.Run("exact 4x undefined - the reported bug", func(t *testing.T) {
		body := []byte(`undefinedundefinedundefinedundefined`)
		unmasked := p.UnmaskResponse(body, result)
		s := string(unmasked)
		t.Logf("result: %s", s)
		assert.NotContains(t, s, "undefined")
	})
}

// TestStream_GuardLegitimateUndefined verifies streaming does NOT strip legitimate "undefined".
func TestStream_GuardLegitimateUndefined(t *testing.T) {
	t.Run("no masking context - undefined preserved", func(t *testing.T) {
		u := masking.NewStreamUnmasker(nil, nil)
		result := u.ProcessChunk(`typeof x === "undefined"`)
		assert.Equal(t, `typeof x === "undefined"`, result)
	})

	t.Run("with masking context - undefined replaced", func(t *testing.T) {
		ctx := masking.NewMaskContext()
		ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
		u := masking.NewStreamUnmasker(ctx, nil)
		result := u.ProcessChunk("connect to undefined")
		t.Logf("result: %s", result)
		assert.NotContains(t, result, "undefined")
		assert.Contains(t, result, "10.0.0.1")
	})

	t.Run("with masking context - code undefined preserved when budgets exhausted", func(t *testing.T) {
		// 1 original, but text has 2 "undefined" - the second should be stripped
		// because we can't distinguish it from GLM noise when masking is active
		ctx := masking.NewMaskContext()
		ctx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"
		u := masking.NewStreamUnmasker(ctx, nil)
		result := u.ProcessChunk("typeof x === undefined and IP=undefined")
		t.Logf("result: %s", result)
		// First undefined replaced with IP, second stripped (budget exhausted)
		assert.NotContains(t, result, "undefined")
	})
}

// TestStream_ProcessChunkJSON_GuardSameBehavior verifies JSON path has same guard.
func TestStream_ProcessChunkJSON_GuardSameBehavior(t *testing.T) {
	t.Run("no context - undefined preserved", func(t *testing.T) {
		u := masking.NewStreamUnmasker(nil, nil)
		result := u.ProcessChunkJSON(`{"check": "undefined"}`)
		assert.Contains(t, result, "undefined")
	})

	t.Run("with context - undefined replaced", func(t *testing.T) {
		ctx := masking.NewMaskContext()
		ctx.Mapping["[[API_KEY_SK_1]]"] = "sk-abc123"
		u := masking.NewStreamUnmasker(ctx, nil)
		result := u.ProcessChunkJSON(`key=undefined`)
		t.Logf("result: %s", result)
		assert.NotContains(t, result, "undefined")
		assert.Contains(t, result, "sk-abc123")
	})
}
