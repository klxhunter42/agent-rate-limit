package secrets

import "regexp"

type EntityType string

const (
	EntityOpenSSHKey   EntityType = "OPENSSH_PRIVATE_KEY"
	EntityPEMKey       EntityType = "PEM_PRIVATE_KEY"
	EntityAPIKeySK     EntityType = "API_KEY_SK"
	EntityAPIKeyAWS    EntityType = "API_KEY_AWS"
	EntityAPIKeyGitHub EntityType = "API_KEY_GITHUB"
	EntityAPIKeyGitLab EntityType = "API_KEY_GITLAB"
	EntityJWTToken     EntityType = "JWT_TOKEN"
	EntityBearerToken  EntityType = "BEARER_TOKEN"
	EntityEnvPassword  EntityType = "ENV_PASSWORD"
	EntityEnvSecret    EntityType = "ENV_SECRET"
	EntityConnString   EntityType = "CONNECTION_STRING"
	EntityThaiID       EntityType = "THAI_NATIONAL_ID"
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
	{EntityEnvPassword, regexp.MustCompile(`(?i)[A-Za-z_][A-Za-z0-9_]*(?:PASSWORD|_PWD)\s*[=:]\s*['"]?[^\s'"]{8,}['"]?`)},
	{EntityEnvSecret, regexp.MustCompile(`(?i)[A-Za-z_][A-Za-z0-9_]*_SECRET\s*[=:]\s*['"]?[^\s'"]{8,}['"]?`)},

	// Connection strings
	{EntityConnString, regexp.MustCompile(`(?i)(?:postgres(?:ql)?|mysql|mariadb|mongodb(?:\+srv)?|redis|amqps?):\/\/[^:]+:[^@\s]+@[^\s'"]+`)},

	// Local PII (entities not covered by Presidio)
	{EntityThaiID, regexp.MustCompile(`\b[1-8]\d{12}\b`)},
}
