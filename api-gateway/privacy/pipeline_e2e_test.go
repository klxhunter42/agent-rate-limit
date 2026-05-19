package privacy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildPayload constructs a Claude-like request payload.
func buildPayload(system interface{}, messages []map[string]interface{}) []byte {
	payload := map[string]interface{}{}
	if system != nil {
		payload["system"] = system
	}
	payload["messages"] = messages
	body, _ := json.Marshal(payload)
	return body
}

// TestE2E_PasswordMasked verifies password=SuperSecret123! gets masked.
func TestE2E_PasswordMasked(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)
	body := buildPayload(nil, []map[string]interface{}{
		{
			"role":    "user",
			"content": "Please use password=SuperSecret123! to connect",
		},
	})

	result, err := p.MaskRequest(body)
	require.NoError(t, err)
	require.NotNil(t, result, "expected non-nil result for password masking")
	assert.True(t, result.HasSecrets, "expected HasSecrets=true")

	// Verify masked body contains placeholder, not the secret.
	var payload map[string]any
	require.NoError(t, json.Unmarshal(result.MaskedBody, &payload))
	msgs := payload["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].(string)
	assert.Contains(t, content, "[[ENV_PASSWORD_")
	assert.NotContains(t, content, "SuperSecret123!")

	// Verify unmask restores original.
	response := []byte("I see your password is " + extractFirstPlaceholder(content))
	unmasked := p.UnmaskResponse(response, result)
	assert.Contains(t, string(unmasked), "SuperSecret123!")
	assert.NotContains(t, string(unmasked), "[[ENV_PASSWORD_")
}

// TestE2E_EmailMasked verifies test@example.com gets masked to [[EMAIL_ADDRESS_1]].
func TestE2E_EmailMasked(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)
	body := buildPayload(nil, []map[string]interface{}{
		{
			"role":    "user",
			"content": "Send a report to test@example.com please",
		},
	})

	result, err := p.MaskRequest(body)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.HasPII)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(result.MaskedBody, &payload))
	content := payload["messages"].([]any)[0].(map[string]any)["content"].(string)
	assert.Contains(t, content, "[[EMAIL_ADDRESS_")
	assert.NotContains(t, content, "test@example.com")

	// Unmask restores.
	response := []byte("Sending to " + extractFirstPlaceholder(content))
	unmasked := p.UnmaskResponse(response, result)
	assert.Contains(t, string(unmasked), "test@example.com")
}

// TestE2E_PhoneMasked verifies phone number gets masked.
func TestE2E_PhoneMasked(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)
	body := buildPayload(nil, []map[string]interface{}{
		{
			"role":    "user",
			"content": "Call me at 081-234-5678 when ready",
		},
	})

	result, err := p.MaskRequest(body)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.HasPII)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(result.MaskedBody, &payload))
	content := payload["messages"].([]any)[0].(map[string]any)["content"].(string)
	// Thai phone number format matches THAI_PHONE entity (higher score than generic PHONE_NUMBER).
	assert.NotContains(t, content, "081-234-5678")
	assert.True(t, strings.Contains(content, "[[THAI_PHONE_") || strings.Contains(content, "[[PHONE_NUMBER_"),
		"should contain a phone placeholder")
}

// TestE2E_ConnectionStringMasked verifies postgres://admin:pass@host:5432/db gets masked.
func TestE2E_ConnectionStringMasked(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)
	body := buildPayload(nil, []map[string]interface{}{
		{
			"role":    "user",
			"content": "Connect using postgres://admin:pass@host:5432/db for the database",
		},
	})

	result, err := p.MaskRequest(body)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.HasSecrets)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(result.MaskedBody, &payload))
	content := payload["messages"].([]any)[0].(map[string]any)["content"].(string)
	assert.NotContains(t, content, "postgres://admin:pass@host:5432/db")

	// Unmask restores the connection string.
	response := []byte("Use " + extractFirstPlaceholder(content) + " to connect")
	unmasked := p.UnmaskResponse(response, result)
	assert.Contains(t, string(unmasked), "postgres://admin:pass@host:5432/db")
}

// TestE2E_SystemBlocksSkipped verifies system blocks are NOT masked when SkipSystemBlocks=true.
func TestE2E_SystemBlocksSkipped(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)

	systemBlocks := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": "Admin email is admin@corp.com and password=SystemSecret99!",
		},
	}
	body := buildPayload(systemBlocks, []map[string]interface{}{
		{
			"role":    "user",
			"content": "Hello there",
		},
	})

	result, err := p.MaskRequestWithOptions(body, MaskOptions{SkipSystemBlocks: true})
	require.NoError(t, err)
	// System block should NOT be masked. With only "Hello there" in user message,
	// no secrets/PII exist in non-system spans, so result should be nil.
	assert.Nil(t, result, "system blocks should be skipped when SkipSystemBlocks=true")
}

// TestE2E_SystemBlocksMasked verifies system blocks ARE masked when SkipSystemBlocks=false.
func TestE2E_SystemBlocksMasked(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)

	systemBlocks := []interface{}{
		map[string]interface{}{
			"type": "text",
			"text": "Admin email is admin@corp.com for notifications",
		},
	}
	body := buildPayload(systemBlocks, []map[string]interface{}{
		{
			"role":    "user",
			"content": "Hello there",
		},
	})

	result, err := p.MaskRequestWithOptions(body, MaskOptions{SkipSystemBlocks: false})
	require.NoError(t, err)
	require.NotNil(t, result, "system blocks should be masked when SkipSystemBlocks=false")
	assert.True(t, result.HasPII)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(result.MaskedBody, &payload))
	sys := payload["system"].([]any)
	sysText := sys[0].(map[string]any)["text"].(string)
	assert.Contains(t, sysText, "[[EMAIL_ADDRESS_")
	assert.NotContains(t, sysText, "admin@corp.com")
}

// TestE2E_CacheHit verifies identical text gets same masked output from cache.
func TestE2E_CacheHit(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)
	text := "My API key is sk-abc123def456ghi789jkl012mno and email is cache@test.com"
	body := buildPayload(nil, []map[string]interface{}{
		{"role": "user", "content": text},
	})

	// First call populates cache.
	result1, err := p.MaskRequest(body)
	require.NoError(t, err)
	require.NotNil(t, result1)

	var payload1 map[string]any
	require.NoError(t, json.Unmarshal(result1.MaskedBody, &payload1))
	content1 := payload1["messages"].([]any)[0].(map[string]any)["content"].(string)

	// Second call with identical text should hit cache and produce same output.
	result2, err := p.MaskRequest(body)
	require.NoError(t, err)
	require.NotNil(t, result2)

	var payload2 map[string]any
	require.NoError(t, json.Unmarshal(result2.MaskedBody, &payload2))
	content2 := payload2["messages"].([]any)[0].(map[string]any)["content"].(string)

	assert.Equal(t, content1, content2, "cache hit should produce identical masked output")

	// Second result should still unmask correctly when response contains placeholders.
	response := []byte("Your " + extractFirstPlaceholder(content2) + " and " + extractFirstPlaceholder(content2[strings.Index(content2, "[[")+2:]) + " are noted")
	unmasked := p.UnmaskResponse(response, result2)
	assert.Contains(t, string(unmasked), "sk-abc123def456ghi789jkl012mno")
	assert.Contains(t, string(unmasked), "cache@test.com")
}

// TestE2E_UnmaskResponseRestoresAll verifies UnmaskResponse correctly restores all placeholders.
func TestE2E_UnmaskResponseRestoresAll(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)
	body := buildPayload(nil, []map[string]interface{}{
		{
			"role":    "user",
			"content": "Key: sk-abc123def456ghi789jkl012mno, email: restore@test.com",
		},
	})

	result, err := p.MaskRequest(body)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Build a simulated response with all placeholders.
	var masked map[string]any
	require.NoError(t, json.Unmarshal(result.MaskedBody, &masked))
	maskedContent := masked["messages"].([]any)[0].(map[string]any)["content"].(string)

	// The response should reference all placeholders.
	response := []byte("I see key and email: " + maskedContent)
	unmasked := p.UnmaskResponse(response, result)
	s := string(unmasked)
	assert.Contains(t, s, "sk-abc123def456ghi789jkl012mno")
	assert.Contains(t, s, "restore@test.com")
	assert.NotContains(t, s, "[[API_KEY_SK_")
	assert.NotContains(t, s, "[[EMAIL_ADDRESS_")
}

// TestE2E_GLMUndefinedFallback verifies GLM "undefined" gets fallback-restored.
func TestE2E_GLMUndefinedFallback(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)
	body := buildPayload(nil, []map[string]interface{}{
		{
			"role":    "user",
			"content": "My key is sk-abc123def456ghi789jkl012mno",
		},
	})

	result, err := p.MaskRequest(body)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Simulate GLM output that replaced placeholder with "undefined".
	response := []byte("Your key is undefined and it is ready")
	unmasked := p.UnmaskResponse(response, result)
	s := string(unmasked)
	// Should have restored the original secret in place of one "undefined".
	assert.Contains(t, s, "sk-abc123def456ghi789jkl012mno")
}

// TestE2E_MultipleSecretsAndPII verifies multiple secrets + PII in same message all get masked and unmasked.
func TestE2E_MultipleSecretsAndPII(t *testing.T) {
	p := NewPipeline(DefaultConfig(), nil)

	// Build a request with system blocks, user message, tool_use, and tool_result.
	payload := map[string]interface{}{
		"system": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "System context here",
			},
		},
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "text",
						"text": "Connect to postgres://dbadmin:dbpass123@db.internal:5432/prod using email ops@company.com and phone 081-234-5678",
					},
				},
			},
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{
						"type": "tool_use",
						"id":   "toolu_01",
						"name": "execute_query",
						"input": map[string]interface{}{
							"query":  "SELECT * FROM users WHERE email='admin@root.org'",
							"db_url": "mysql://root:r00tpwd@mysql.internal:3306/main",
						},
					},
				},
			},
			map[string]interface{}{
				"role":        "user",
				"content":     "Result data here",
				"tool_use_id": "toolu_01",
			},
		},
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	result, err := p.MaskRequest(body)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify masked body has no raw secrets or PII.
	maskedStr := string(result.MaskedBody)
	assert.NotContains(t, maskedStr, "postgres://dbadmin:dbpass123@db.internal:5432/prod")
	assert.NotContains(t, maskedStr, "ops@company.com")
	assert.NotContains(t, maskedStr, "081-234-5678")
	assert.NotContains(t, maskedStr, "mysql://root:r00tpwd@mysql.internal:3306/main")
	assert.NotContains(t, maskedStr, "admin@root.org")

	// Verify placeholders are present.
	assert.Contains(t, maskedStr, "[[CONNECTION_STRING_")
	assert.Contains(t, maskedStr, "[[EMAIL_ADDRESS_")
	assert.Contains(t, maskedStr, "[[PHONE_NUMBER_")

	// Build a response that references all masked values.
	var maskedPayload map[string]any
	require.NoError(t, json.Unmarshal(result.MaskedBody, &maskedPayload))

	response := []byte(`{"content":[{"type":"text","text":"Connected using the provided credentials. All systems operational."}]}`)
	unmasked := p.UnmaskResponse(response, result)

	// The response text itself doesn't contain placeholders, so unmask should return unchanged.
	assert.Contains(t, string(unmasked), "Connected using the provided credentials")

	// Now test a response that does contain placeholders to verify round-trip.
	// Extract placeholders from masked body.
	placeholders := extractPlaceholders(maskedStr)
	require.NotEmpty(t, placeholders, "should have extracted placeholders from masked body")

	// Build response referencing all placeholders.
	respText := "Found connections: " + strings.Join(placeholders, ", ")
	unmasked = p.UnmaskResponse([]byte(respText), result)
	s := string(unmasked)

	// All originals should be restored.
	assert.Contains(t, s, "postgres://dbadmin:dbpass123@db.internal:5432/prod")
	assert.Contains(t, s, "ops@company.com")
	assert.Contains(t, s, "mysql://root:r00tpwd@mysql.internal:3306/main")
	assert.Contains(t, s, "admin@root.org")
	// No placeholders should remain.
	assert.NotContains(t, s, "[[CONNECTION_STRING_")
	assert.NotContains(t, s, "[[EMAIL_ADDRESS_")
	assert.NotContains(t, s, "[[PHONE_NUMBER_")
}

// extractFirstPlaceholder returns the first [[TYPE_N]] placeholder found in text.
func extractFirstPlaceholder(text string) string {
	start := strings.Index(text, "[[")
	if start < 0 {
		return ""
	}
	end := strings.Index(text[start:], "]]")
	if end < 0 {
		return ""
	}
	return text[start : start+end+2]
}

// extractPlaceholders returns all [[TYPE_N]] placeholders found in text.
func extractPlaceholders(text string) []string {
	var result []string
	for {
		idx := strings.Index(text, "[[")
		if idx < 0 {
			break
		}
		end := strings.Index(text[idx:], "]]")
		if end < 0 {
			break
		}
		ph := text[idx : idx+end+2]
		result = append(result, ph)
		text = text[idx+end+2:]
	}
	return result
}
