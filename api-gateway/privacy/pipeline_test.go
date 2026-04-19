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

	// Verify masked body has placeholder
	var payload map[string]any
	json.Unmarshal(result.MaskedBody, &payload)
	msgs := payload["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].(string)
	assert.Contains(t, content, "[[API_KEY_SK_1]]")
	assert.NotContains(t, content, "sk-abc123def456ghi789jkl012mno")
}

func TestPipeline_UnmaskResponse(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = false
	p := NewPipeline(cfg, nil)

	body := []byte(`{"messages":[{"role":"user","content":"my key is sk-abc123def456ghi789jkl012mno"}]}`)
	result, _ := p.MaskRequest(body)
	assert.NotNil(t, result)

	// Simulate response containing placeholders
	response := []byte("Your key [[API_KEY_SK_1]] is noted.")
	unmasked := p.UnmaskResponse(response, result)
	assert.Contains(t, string(unmasked), "sk-abc123def456ghi789jkl012mno")
	assert.NotContains(t, string(unmasked), "[[API_KEY_SK_1]]")
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

func TestPipeline_HasPresidio(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = false
	p := NewPipeline(cfg, nil)
	assert.False(t, p.HasPresidio())
}

func TestPipeline_NewStreamUnmasker(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)
	u := p.NewStreamUnmasker(nil)
	assert.False(t, u.HasContexts())
}
