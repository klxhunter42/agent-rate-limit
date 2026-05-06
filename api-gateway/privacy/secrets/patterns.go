package secrets

import "regexp"

type EntityType string

const (
	EntityOpenSSHKey      EntityType = "OPENSSH_PRIVATE_KEY"
	EntityPEMKey          EntityType = "PEM_PRIVATE_KEY"
	EntityAPIKeySK        EntityType = "API_KEY_SK"
	EntityAPIKeyAWS       EntityType = "API_KEY_AWS"
	EntityAPIKeyGitHub    EntityType = "API_KEY_GITHUB"
	EntityAPIKeyGitLab    EntityType = "API_KEY_GITLAB"
	EntityJWTToken        EntityType = "JWT_TOKEN"
	EntityBearerToken     EntityType = "BEARER_TOKEN"
	EntityEnvPassword     EntityType = "ENV_PASSWORD"
	EntityEnvSecret       EntityType = "ENV_SECRET"
	EntityConnString      EntityType = "CONNECTION_STRING"
	EntityThaiID          EntityType = "THAI_NATIONAL_ID"
	EntityAPIKeyGCP       EntityType = "API_KEY_GCP"
	EntityAPIKeyTencent   EntityType = "API_KEY_TENCENT"
	EntityAPIKeyAlibaba   EntityType = "API_KEY_ALIBABA"
	EntityAPIKeySlack     EntityType = "API_KEY_SLACK"
	EntityAPIKeyStripe    EntityType = "API_KEY_STRIPE"
	EntityAPIKeySendGrid  EntityType = "API_KEY_SENDGRID"
	EntityEnvToken        EntityType = "ENV_TOKEN"
	EntityEnvCredential   EntityType = "ENV_CREDENTIAL"
	EntityBasicAuthURL    EntityType = "BASIC_AUTH_URL"
	EntityVaultToken      EntityType = "VAULT_TOKEN"
	EntityAzureCredential EntityType = "AZURE_CREDENTIAL"
	EntityCLIAuth         EntityType = "CLI_AUTH"
	EntityCurlBasicAuth   EntityType = "CURL_BASIC_AUTH"
	EntityEnvUser         EntityType = "ENV_USER"
	EntityWebhookURL      EntityType = "WEBHOOK_URL"
)

type patternSpec struct {
	entityType EntityType
	regex      *regexp.Regexp
}

var allPatterns = []patternSpec{
	// Private keys
	{EntityOpenSSHKey, regexp.MustCompile(`-----BEGIN OPENSSH PRIVATE KEY-----[\s\S]*?-----END OPENSSH PRIVATE KEY-----`)},
	{EntityPEMKey, regexp.MustCompile(`-----BEGIN RSA PRIVATE KEY-----[\s\S]*?-----END RSA PRIVATE KEY-----`)},
	{EntityPEMKey, regexp.MustCompile(`-----BEGIN PRIVATE KEY-----[\s\S]*?-----END PRIVATE KEY-----`)},
	{EntityPEMKey, regexp.MustCompile(`-----BEGIN ENCRYPTED PRIVATE KEY-----[\s\S]*?-----END ENCRYPTED PRIVATE KEY-----`)},

	// API keys
	{EntityAPIKeySK, regexp.MustCompile(`sk[-_][a-zA-Z0-9_-]{20,}`)},
	{EntityAPIKeyAWS, regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{EntityAPIKeyGitHub, regexp.MustCompile(`gh[pousr]_[a-zA-Z0-9]{36,}`)},
	{EntityAPIKeyGitLab, regexp.MustCompile(`gl(?:pat|dt|cbt|ptt)-[a-zA-Z0-9_-]{20,}`)},

	// Tokens
	{EntityJWTToken, regexp.MustCompile(`eyJ[a-zA-Z0-9_-]{20,}\.eyJ[a-zA-Z0-9_-]{20,}\.[a-zA-Z0-9_-]{20,}`)},
	{EntityBearerToken, regexp.MustCompile(`(?i)Bearer\s+[a-zA-Z0-9._-]{40,}`)},

	// Environment variables
	{EntityEnvPassword, regexp.MustCompile(`(?i)(?:[A-Za-z_]\w*(?:PASSWORD|PASSWD|_PWD|_PASS)|(?:PASSWORD|PASSWD|_PWD|_PASS))\s*[=:]\s*['"]?[^\s'"]{8,}['"]?`)},
	{EntityEnvSecret, regexp.MustCompile(`(?i)(?:[A-Za-z_]\w*_SECRET|(?:SECRET))\s*[=:]\s*['"]?[^\s'"]{8,}['"]?`)},
	{EntityEnvUser, regexp.MustCompile(`(?i)(?:[A-Za-z_]\w*(?:_USER|_USERNAME|_LOGIN)|(?:USER|USERNAME|LOGIN))\s*[=:]\s*['"]?[a-zA-Z0-9._@+-]{2,}['"]?`)},

	// Connection strings
	{EntityConnString, regexp.MustCompile(`(?i)(?:postgres(?:ql)?|mysql|mariadb|mongodb(?:\+srv)?|redis|amqps?):\/\/[^:]+:[^@\s]+@[^\s'"]+`)},

	// Cloud provider keys (prefixed, near-zero false positive)
	{EntityAPIKeyGCP, regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)},
	{EntityAPIKeyTencent, regexp.MustCompile(`AKID[A-Za-z0-9]{12,}`)},
	// Tencent/Alibaba SecretKey assignment (context-aware)
	{EntityAPIKeyTencent, regexp.MustCompile(`(?i)(?:TENCENT|ALIBABA|ALIYUN|ACS)[A-Za-z_ ]*SECRET_?KEY\s*[=:]\s*['"]?[A-Za-z0-9/+=]{16,}['"]?`)},
	{EntityAPIKeyAlibaba, regexp.MustCompile(`LTAI[A-Za-z0-9]{12,20}`)},

	// SaaS platform tokens (prefixed)
	{EntityAPIKeySlack, regexp.MustCompile(`xox[bapors]-[a-zA-Z0-9-]{10,}`)},
	{EntityAPIKeyStripe, regexp.MustCompile(`(?:sk|rk)_live_[a-zA-Z0-9]{24,}`)},
	{EntityAPIKeySendGrid, regexp.MustCompile(`SG\.[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9_-]{10,}`)},

	// Generic webhook URLs (Slack, Discord, Teams, PagerDuty, Zapier, etc.)
	{EntityWebhookURL, regexp.MustCompile(`https://(?:hooks\.slack\.com/services/|discord(?:app)?\.com/api/webhooks/|outlook\.office\.com/webhook/|events\.pagerduty\.com/[a-zA-Z0-9]+/[a-zA-Z0-9]+|hooks\.zapier\.com/hooks/[a-zA-Z0-9/]+|hooks\.slack\.com/workflows/[a-zA-Z0-9/]+|[^/]*\.webhook\.[a-z]+(?:\.[a-z]+)?(?:/[^\s'"]*)?)[a-zA-Z0-9_/.-]+`)},

	// Context-aware: token/key assignments (variable name or dict key + long value)
	{EntityEnvToken, regexp.MustCompile(`(?i)(?:\btoken\b|[A-Za-z_][A-Za-z0-9_]*_TOKEN)['"]?\s*[=:]\s*['"]?[a-zA-Z0-9_.+/=-]{16,}['"]?`)},
	// Context-aware: client ID / CID / access key assignments
	{EntityEnvCredential, regexp.MustCompile(`(?i)[A-Za-z_][A-Za-z0-9_]*(?:CLIENT_ID|_CID|_ACCESS_KEY)['"]?\s*[=:]\s*['"]?[a-zA-Z0-9_.-]{10,}['"]?`)},

	// HTTP Basic Auth in URLs (user:pass@host)
	{EntityBasicAuthURL, regexp.MustCompile(`(?i)[a-z][a-z0-9+\-.]*://[^\s:/'"]{2,}:[^\s@/'"]{4,}@[^\s'"]+`)},
	// CLI authentication flags (-u/-p/-U/-W/-xu/-xp, --password, --token, --secret[-key], --api-key, --access-key, --auth-token, etc.)
	{EntityCLIAuth, regexp.MustCompile(`(?i)(?:\B-[uUpW]|-x[up]|--password|--passwd|--secret(?:-key)?|--access(?:-key(?:-id)?)?|--token|--api-?key|--auth(?:-token|-pass|-user)?)(?:\s+['"]?|=['"]?)[^\s'"]{3,}['"]?`)},
	// mysql/psql password shorthand: -pPASSWORD (no space between -p and value)
	{EntityCLIAuth, regexp.MustCompile(`(?:^|\s)-p([^\s'"]{4,})`)},
	// curl/wget basic auth (-u user:pass or --user user:pass)
	{EntityCurlBasicAuth, regexp.MustCompile(`(?:-u|--user|--username)\s+['"]?[^\s'":]+:[^\s'"]{2,}['"]?`)},
	// HashiCorp Vault token (hvs. or s. prefix)
	{EntityVaultToken, regexp.MustCompile(`hvs\.[a-zA-Z0-9_-]{24,}`)},
	{EntityVaultToken, regexp.MustCompile(`(?i)\bs\.([a-zA-Z0-9]{8,})\b`)},
	// Azure client secret / tenant ID (UUID with Azure keyword context)
	{EntityAzureCredential, regexp.MustCompile(`(?i)(?:AZURE|TENANT|AAD)[A-Za-z_]*(?:SECRET|KEY|PASSWORD|TOKEN|ID)\s*[=:]\s*['"][a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}['"]`)},

	// Local PII (entities not covered by Presidio)
	{EntityThaiID, regexp.MustCompile(`\b[1-8]\d{12}\b`)},
}
