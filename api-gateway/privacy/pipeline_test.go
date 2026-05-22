package privacy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPipeline_MaskRequest_NoSecrets(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)
	body := []byte(`{"messages":[{"role":"user","content":"Hello, how are you?"}]}`)
	result, err := p.MaskRequest(body)
	assert.NoError(t, err)
	assert.Nil(t, result) // no secrets/PII detected
}

func TestPipeline_MaskRequest_WithSecret(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = false
	p := NewPipeline(cfg, nil)

	body := []byte(`{"messages":[{"role":"user","content":"my key is sk-abc123def456ghi789jkl012mno"}]}`)
	result, err := p.MaskRequest(body)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.HasSecrets)

	// Verify masked body has placeholder (deterministic, not sequential)
	var payload map[string]any
	json.Unmarshal(result.MaskedBody, &payload)
	msgs := payload["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].(string)
	assert.Contains(t, content, "[[API_KEY_SK_")
	assert.NotContains(t, content, "sk-abc123def456ghi789jkl012mno")
}

func TestPipeline_UnmaskResponse(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = false
	p := NewPipeline(cfg, nil)

	body := []byte(`{"messages":[{"role":"user","content":"my key is sk-abc123def456ghi789jkl012mno"}]}`)
	result, _ := p.MaskRequest(body)
	assert.NotNil(t, result)

	// Extract the actual placeholder from the mapping to use in simulated response
	secret := "sk-abc123def456ghi789jkl012mno"
	ph := result.SecretsCtx.ReverseMap[secret]
	assert.NotEmpty(t, ph)

	response := []byte("Your key " + ph + " is noted.")
	unmasked := p.UnmaskResponse(response, result)
	assert.Contains(t, string(unmasked), secret)
	assert.NotContains(t, string(unmasked), ph)
}

func TestPipeline_UnmaskResponse_NilResult(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)
	body := []byte("response")
	assert.Equal(t, body, p.UnmaskResponse(body, nil))
}

func TestPipeline_InvalidJSON(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)
	_, err := p.MaskRequest([]byte("not json"))
	assert.Error(t, err)
}

func TestPipeline_Disabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	p := NewPipeline(cfg, nil)
	body := []byte(`{"messages":[{"role":"user","content":"Hello"}]}`)
	// Pipeline exists but no detectors
	result, err := p.MaskRequest(body)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestPipeline_HasPIIDetector(t *testing.T) {
	cfg := DefaultConfig()
	p := NewPipeline(cfg, nil)
	assert.True(t, p.HasPIIDetector())

	cfg2 := DefaultConfig()
	cfg2.PIIEnabled = false
	p2 := NewPipeline(cfg2, nil)
	assert.False(t, p2.HasPIIDetector())
}

func TestPipeline_NewStreamUnmasker(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)
	u := p.NewStreamUnmasker(nil)
	assert.False(t, u.HasContexts())
}

// H6: Verify StripLeftoverPlaceholders runs in UnmaskResponse as safety net.
func TestPipeline_UnmaskResponse_StripsLeftoverPlaceholders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = false
	p := NewPipeline(cfg, nil)

	body := []byte(`{"messages":[{"role":"user","content":"my key is sk-abc123def456ghi789jkl012mno"}]}`)
	result, _ := p.MaskRequest(body)
	assert.NotNil(t, result)

	secret := "sk-abc123def456ghi789jkl012mno"
	ph := result.SecretsCtx.ReverseMap[secret]

	// Response contains the known placeholder and an unknown leftover placeholder
	response := []byte("Your key " + ph + " and unknown [[UNKNOWN_TYPE_99]] here.")
	unmasked := p.UnmaskResponse(response, result)

	s := string(unmasked)
	// Known placeholder should be restored
	assert.Contains(t, s, "sk-abc123def456ghi789jkl012mno")
	// Leftover placeholder should be stripped (safety net)
	assert.NotContains(t, s, "[[UNKNOWN_TYPE_99]]")
}
