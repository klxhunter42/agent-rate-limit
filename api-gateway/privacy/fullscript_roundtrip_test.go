package privacy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoundtrip_FullScript_Integration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PIIEnabled = true

	// Original script with REAL credential values (before any masking)
	script := `#!/bin/bash
random_password=$(openssl rand -base64 24)
email=$(echo "$1" | awk '{print tolower($0)}')
env=$(echo "$2" | awk '{print tolower($0)}')
role=$(echo "$3" | awk '{print tolower($0)}')
full_name=${email%@*}
elasticsearch_endpoint=$([[ ${env} == "prod" ]] && echo "https://hotpod.com" || echo "https://dev-hotpod.com")
prod_hotpod_admin_user="admin"
prod_hotpod_admin_password="123456abcdef"
dev_hotpod_admin_user="admin"
dev_hotpod_admin_password="5678inwhon"
if [[ ${env} == "prod" ]]; then
 admin_user=${prod_hotpod_admin_user}
 admin_password=${prod_hotpod_admin_password}
else
 admin_user=${dev_hotpod_admin_user}
 admin_password=${dev_hotpod_admin_password}
fi
kibana_endpoint=$([[ ${env} == "prod" ]] && echo "https://kibana.hotpod.com" || echo "https://kibana.dev.hotpod.com/")
body='{"full_name":"'${full_name}'","password":"'${random_password}'","roles":["'${role}'"],"email":"'${email}'","metadata":{}}'
response=$(curl -k -s -o /dev/null -w "%{http_code}" -X POST -u "${admin_user}:${admin_password}" "${elasticsearch_endpoint}/_security/user/${email}" -H "Content-Type: application/json" -d "${body}")
if [[ ${response} -eq 200 ]]; then
 echo "Success Create :: Username=${email}, Password=${random_password}"
 sendemail -f "team@team.com" -t "${email}" -u "Kibana ${env}" -m "Kibana URL: ${kibana_endpoint}, Username: ${email}, Password: ${random_password}" -s "mail:587" -cc "me@me.com" -xu "12345678" -xp "91011121314"
else
 echo "Failed to create user :: Username=${email}"
fi`

	p := NewPipeline(cfg, nil)
	result, err := p.MaskRequest(makeBody(script))
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Collect all placeholders
	var placeholders []string
	for ph := range result.SecretsCtx.Mapping {
		placeholders = append(placeholders, ph)
	}
	for ph := range result.PIICtx.Mapping {
		placeholders = append(placeholders, ph)
	}

	t.Logf("secrets mapping (%d):", len(result.SecretsCtx.Mapping))
	for ph, orig := range result.SecretsCtx.Mapping {
		t.Logf("  %s -> %s", ph, orig)
	}
	t.Logf("PII mapping (%d):", len(result.PIICtx.Mapping))
	for ph, orig := range result.PIICtx.Mapping {
		t.Logf("  %s -> %s", ph, orig)
	}

	// Verify key secrets detected
	foundSecrets := map[string]bool{}
	for _, orig := range result.SecretsCtx.Mapping {
		foundSecrets[orig] = true
	}
	assert.True(t, foundSecrets[`prod_hotpod_admin_password="123456abcdef"`], "prod password should be masked")
	assert.True(t, foundSecrets[`dev_hotpod_admin_password="5678inwhon"`], "dev password should be masked")
	assert.True(t, foundSecrets[`-xu "12345678"`], "SMTP username should be masked via CLI_AUTH")
	assert.True(t, foundSecrets[`-xp "91011121314"`], "SMTP password should be masked via CLI_AUTH")
	assert.True(t, foundSecrets[`prod_hotpod_admin_user="admin"`], "prod admin user should be masked via ENV_USER")
	assert.True(t, foundSecrets[`dev_hotpod_admin_user="admin"`], "dev admin user should be masked via ENV_USER")

	// Verify PII
	foundPII := map[string]bool{}
	for _, orig := range result.PIICtx.Mapping {
		foundPII[orig] = true
	}
	assert.True(t, foundPII["team@team.com"], "sender email should be masked")
	assert.True(t, foundPII["me@me.com"], "cc email should be masked")

	maskedBody := string(result.MaskedBody)

	// Verify NO sensitive values in masked body
	assert.NotContains(t, maskedBody, "123456abcdef")
	assert.NotContains(t, maskedBody, "5678inwhon")
	assert.NotContains(t, maskedBody, "12345678")
	assert.NotContains(t, maskedBody, "91011121314")
	assert.NotContains(t, maskedBody, "team@team.com")
	assert.NotContains(t, maskedBody, "me@me.com")

	// Simulate LLM echoing back placeholders
	llmResp := "Found: "
	for _, ph := range placeholders {
		llmResp += ph + " "
	}

	unmasked := p.UnmaskResponse([]byte(llmResp), result)
	unmaskedStr := string(unmasked)
	t.Logf("unmasked: %s", unmaskedStr)

	// Verify all placeholders restored (none survive)
	for _, ph := range placeholders {
		assert.NotContains(t, unmaskedStr, ph, "placeholder %s survived unmask", ph)
	}

	// Verify originals are back
	assert.Contains(t, unmaskedStr, "123456abcdef")
	assert.Contains(t, unmaskedStr, "5678inwhon")
	assert.Contains(t, unmaskedStr, "12345678")
	assert.Contains(t, unmaskedStr, "91011121314")
	assert.Contains(t, unmaskedStr, "team@team.com")
	assert.Contains(t, unmaskedStr, "me@me.com")
}
