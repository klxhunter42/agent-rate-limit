package privacy

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func makeBody(content string) []byte {
	contentJSON, _ := json.Marshal(content)
	return []byte(fmt.Sprintf(`{"messages":[{"role":"user","content":%s}]}`, string(contentJSON)))
}

func roundtripTest(t *testing.T, cfg *Config, body []byte, response string) {
	p := NewPipeline(cfg, nil)

	result, err := p.MaskRequest(body)
	assert.NoError(t, err)
	if result == nil {
		t.Fatal("no secrets detected, cannot test roundtrip")
	}

	placeholders := make([]string, 0, len(result.SecretsCtx.Mapping)+len(result.PIICtx.Mapping))
	for ph := range result.SecretsCtx.Mapping {
		placeholders = append(placeholders, ph)
	}
	for ph := range result.PIICtx.Mapping {
		placeholders = append(placeholders, ph)
	}
	t.Logf("placeholders: %v", placeholders)

	resp := response
	if resp == "" {
		resp = "Found: "
		for _, ph := range placeholders {
			resp += ph + " "
		}
	}

	unmasked := p.UnmaskResponse([]byte(resp), result)
	unmaskedStr := string(unmasked)
	t.Logf("unmasked: %s", unmaskedStr)

	for _, ph := range placeholders {
		assert.NotContains(t, unmaskedStr, ph, "placeholder %s survived unmask", ph)
	}
}

func TestRoundtrip_AllNewPatterns(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = true

	tests := []struct {
		name    string
		content string
	}{
		{"API_KEY_GCP", "key=AIzaSyA1234567890abcdefghijklmnopqrstuvwxyz12345"},
		{"API_KEY_TENCENT", "key=AKID" + strings.Repeat("a", 32)},
		{"API_KEY_ALIBABA", "key=LTAI1234567890ab"},
		{"API_KEY_SLACK", "token xo" + "xb-1234567890-abcdefghijklmnop"},
		{"API_KEY_STRIPE", "key=" + "sk" + "_live_" + strings.Repeat("a", 24)},
		{"API_KEY_SENDGRID", "key=SG.aaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbcc"},
		{"ENV_TOKEN_dict", `"token": "Vaka7n3Tv9x8oKBpE"`},
		{"ENV_TOKEN_var", `API_TOKEN = "eyJhbGciOiJIUzI1NiJ9abcdef"`},
		{"ENV_CREDENTIAL_CID", `CS_MEMBER_CID = "22RlPmD49ra7IU2JKTByA1xM26iA"`},
		{"ENV_CREDENTIAL_ACCESS", `AWS_ACCESS_KEY = "AKIAIOSFODNN7EXAMPLE"`},
		{"BASIC_AUTH_URL", `https://admin:s3cretP@ss@internal.example.com/api`},
		{"VAULT_TOKEN", "VAULT_TOKEN=" + "hvs." + strings.Repeat("a", 24)},
		{"AZURE_CREDENTIAL", `AZURE_CLIENT_SECRET = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"`},
		{"CONNECTION_STRING", "DATABASE_URL=postgres://user:pass@host:5432/mydb"},
		{"ENV_PASSWORD", `SMTP_PASS = "qO4L32iZmbq54v261TGP1"`},
		{"ENV_SECRET", `CS_CLIENT_SECRET = "r8961h4L35uXad3J1RJ2Z6mbnL27"`},
		{"THAI_NATIONAL_ID", "My ID is 1100100473221 please check"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roundtripTest(t, cfg, makeBody(tt.content), "")
		})
	}
}

func TestRoundtrip_MultipleSecretsSameType(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = false

	content := `FIRST_SECRET='secretvalue111111' SECOND_SECRET='secretvalue222222' THIRD_SECRET='secretvalue333333'`
	p := NewPipeline(cfg, nil)
	result, err := p.MaskRequest(makeBody(content))
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.HasSecrets)

	t.Logf("mapping: %v", result.SecretsCtx.Mapping)

	response := "Found: "
	for ph := range result.SecretsCtx.Mapping {
		response += ph + " "
	}

	unmasked := p.UnmaskResponse([]byte(response), result)
	unmaskedStr := string(unmasked)
	t.Logf("unmasked: %s", unmaskedStr)

	for ph := range result.SecretsCtx.Mapping {
		assert.NotContains(t, unmaskedStr, ph, "placeholder %s survived", ph)
	}
	assert.Contains(t, unmaskedStr, "secretvalue111111")
	assert.Contains(t, unmaskedStr, "secretvalue222222")
	assert.Contains(t, unmaskedStr, "secretvalue333333")
}

func TestRoundtrip_MultipleTypes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = false

	script := `CS_CLIENT_ID = "qy91wQ2RR94DHMESJGgOt0xEkHlG"` + "\n" +
		`CS_CLIENT_SECRET = "r8961h4L35uXad3J1RJ2Z6mbnL27"` + "\n" +
		`SMTP_PASS = "qO4L32iZmbq54v261TGP1"` + "\n" +
		`DATABASE_URL=postgres://admin:pass123@db.example.com:5432/mydb`

	roundtripTest(t, cfg, makeBody(script), "")
}

func TestRoundtrip_VPNScript(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = false

	script := `PFUSER = "bro"
NONPROD = {
    "token": "Vaka7n3Tv9x8oKBpE",
    "url": "https://4.4.4.4",
}
PROD = {
    "token": "1e6O4Z2f6d5Ra6Ip8",
    "url": "https://5.5.5.5",
}
CS_CLIENT_ID = "qy91wQ2RR94DHMESJGgOt0xEkHlG"
CS_CLIENT_SECRET = "r8961h4L35uXad3J1RJ2Z6mbnL27"
CS_MEMBER_CID = "22RlPmD49ra7IU2JKTByA1xM26iA"
SMTP_PASS = "qO4L32iZmbq54v261TGP1"`

	roundtripTest(t, cfg, makeBody(script), "")
}

func TestRoundtrip_SecretsAndPII(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = true

	content := `SECRET_KEY='mysecretkey12345678' contact me at user@example.com`

	p := NewPipeline(cfg, nil)
	result, err := p.MaskRequest(makeBody(content))
	assert.NoError(t, err)
	assert.NotNil(t, result)

	t.Logf("secrets: %v", result.SecretsCtx.Mapping)
	t.Logf("pii: %v", result.PIICtx.Mapping)

	response := "Found: "
	for ph := range result.SecretsCtx.Mapping {
		response += ph + " "
	}
	for ph := range result.PIICtx.Mapping {
		response += ph + " "
	}

	unmasked := p.UnmaskResponse([]byte(response), result)
	unmaskedStr := string(unmasked)
	t.Logf("unmasked: %s", unmaskedStr)

	for ph := range result.SecretsCtx.Mapping {
		assert.NotContains(t, unmaskedStr, ph, "secret placeholder %s survived", ph)
	}
	for ph := range result.PIICtx.Mapping {
		assert.NotContains(t, unmaskedStr, ph, "PII placeholder %s survived", ph)
	}
}

func TestRoundtrip_Streaming_MultipleSecrets(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = false

	content := `CS_CLIENT_SECRET = "r8961h4L35uXad3J1RJ2Z6mbnL27" SMTP_PASS = "qO4L32iZmbq54v261TGP1"`

	p := NewPipeline(cfg, nil)
	result, err := p.MaskRequest(makeBody(content))
	assert.NoError(t, err)
	assert.NotNil(t, result)

	unmasker := p.NewStreamUnmasker(result)
	assert.True(t, unmasker.HasContexts())

	var allPH []string
	for ph := range result.SecretsCtx.Mapping {
		allPH = append(allPH, ph)
	}
	t.Logf("streaming placeholders: %v", allPH)
	assert.True(t, len(allPH) >= 2, "expected at least 2 placeholders")

	// Simulate streaming: first placeholder split across chunks
	ph1 := allPH[0]
	mid := len(ph1) / 2
	if mid == 0 {
		mid = 1
	}
	chunk1 := "Found " + ph1[:mid]
	out1 := unmasker.ProcessChunk(chunk1)
	t.Logf("chunk1: %q -> %q", chunk1, out1)

	chunk2 := ph1[mid:]
	if len(allPH) > 1 {
		chunk2 += " and " + allPH[1]
	}
	out2 := unmasker.ProcessChunk(chunk2)
	t.Logf("chunk2: %q -> %q", chunk2, out2)

	flushed := unmasker.Flush()
	t.Logf("flushed: %q", flushed)

	fullOutput := out1 + out2 + flushed
	t.Logf("full output: %s", fullOutput)

	for _, ph := range allPH {
		assert.NotContains(t, fullOutput, ph, "placeholder %s survived streaming unmask", ph)
	}
}

func TestRoundtrip_JSONResponse(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = false

	content := `CS_CLIENT_SECRET = "r8961h4L35uXad3J1RJ2Z6mbnL27"`
	p := NewPipeline(cfg, nil)
	result, err := p.MaskRequest(makeBody(content))
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Simulate actual JSON response body with placeholder
	var ph string
	for k := range result.SecretsCtx.Mapping {
		ph = k
		break
	}
	t.Logf("placeholder: %s", ph)

	jsonResponse := `{"id":"msg_123","content":[{"type":"text","text":"I found ` + ph + ` in your code"}]}`
	unmasked := p.UnmaskResponse([]byte(jsonResponse), result)
	unmaskedStr := string(unmasked)
	t.Logf("unmasked JSON: %s", unmaskedStr)

	assert.NotContains(t, unmaskedStr, ph)
	assert.Contains(t, unmaskedStr, `r8961h4L35uXad3J1RJ2Z6mbnL27`)
	// Verify JSON is still valid after unmask
	var parsed map[string]any
	assert.NoError(t, json.Unmarshal(unmasked, &parsed))
}
