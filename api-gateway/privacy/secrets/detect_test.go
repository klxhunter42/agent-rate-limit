package secrets

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPatternOpenSSHKey(t *testing.T) {
	d := DefaultDetector()
	r := d.Detect("-----BEGIN OPENSSH PRIVATE KEY-----\nabc123\n-----END OPENSSH PRIVATE KEY-----")
	assert.True(t, r.Detected)
	found := false
	for _, m := range r.Matches {
		if m.Type == EntityOpenSSHKey {
			found = true
			assert.Equal(t, 1, m.Count)
		}
	}
	assert.True(t, found)
}

func TestPatternPEMKey(t *testing.T) {
	d := DefaultDetector()
	tests := []struct {
		name  string
		input string
	}{
		{"RSA", "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----"},
		{"generic", "-----BEGIN PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----"},
		{"encrypted", "-----BEGIN ENCRYPTED PRIVATE KEY-----\nMIIE\n-----END ENCRYPTED PRIVATE KEY-----"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.Detect(tt.input)
			assert.True(t, r.Detected)
		})
	}
}

func TestPatternAPIKeySK(t *testing.T) {
	d := DefaultDetector()
	r := d.Detect("my key is sk-abc123def456ghi789jkl012")
	assert.True(t, r.Detected)
	found := false
	for _, m := range r.Matches {
		if m.Type == EntityAPIKeySK {
			found = true
		}
	}
	assert.True(t, found)
}

func TestPatternAPIKeyAWS(t *testing.T) {
	d := DefaultDetector()
	r := d.Detect("AWS key AKIAIOSFODNN7EXAMPLE")
	assert.True(t, r.Detected)
}

func TestPatternAPIKeyGitHub(t *testing.T) {
	d := DefaultDetector()
	r := d.Detect("token ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij123456")
	assert.True(t, r.Detected)
}

func TestPatternJWT(t *testing.T) {
	d := DefaultDetector()
	// Each segment needs 20+ chars after eyJ prefix.
	r := d.Detect("jwt eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0.abc123def456ghi789jkl012mno")
	assert.True(t, r.Detected)
}

func TestPatternBearerToken(t *testing.T) {
	d := DefaultDetector()
	r := d.Detect("Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789ABCD")
	assert.True(t, r.Detected)
}

func TestPatternEnvPassword(t *testing.T) {
	d := NewDetector([]string{string(EntityEnvPassword)}, 200000)
	r := d.Detect("DB_PASSWORD=supersecret123")
	assert.True(t, r.Detected)
}

func TestPatternEnvSecret(t *testing.T) {
	d := NewDetector([]string{string(EntityEnvSecret)}, 200000)
	r := d.Detect("API_SECRET='mysecretvalue123'")
	assert.True(t, r.Detected)
}

func TestPatternConnectionString(t *testing.T) {
	d := NewDetector([]string{string(EntityConnString)}, 200000)
	r := d.Detect("DATABASE_URL=postgres://user:pass@host:5432/mydb")
	assert.True(t, r.Detected)
}

func TestNoMatch(t *testing.T) {
	d := DefaultDetector()
	r := d.Detect("hello world, nothing secret here")
	assert.False(t, r.Detected)
}

func TestMaxScanChars(t *testing.T) {
	d := NewDetector([]string{"API_KEY_SK"}, 20)
	text := "key at end: sk-abcdefghijklmnopqrstuvwxyz"
	r := d.Detect(text)
	assert.False(t, r.Detected) // secret is past 20 chars
}

func TestMultipleSecrets(t *testing.T) {
	d := DefaultDetector()
	text := "key=sk-abc123def456ghi789jkl012 aws=AKIAIOSFODNN7EXAMPLE"
	r := d.Detect(text)
	assert.True(t, r.Detected)
	total := 0
	for _, m := range r.Matches {
		total += m.Count
	}
	assert.Equal(t, 2, total)
}

func TestDuplicatePositions(t *testing.T) {
	d := DefaultDetector()
	text := "key sk-abc123def456ghi789jkl012 and sk-abc123def456ghi789jkl012"
	r := d.Detect(text)
	assert.True(t, r.Detected)
}

func TestPatternThaiID(t *testing.T) {
	d := DefaultDetector()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid ID", "My ID is 1100100473221 please check", true},
		{"valid start 2", "2509800345678", true},
		{"valid start 8", "8901234567890", true},
		{"invalid start 0", "0100100473221", false},
		{"invalid start 9", "9100100473221", false},
		{"too short", "110010047322", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.Detect(tt.input)
			assert.Equal(t, tt.want, r.Detected)
			if tt.want {
				found := false
				for _, m := range r.Matches {
					if m.Type == EntityThaiID {
						found = true
					}
				}
				assert.True(t, found, "expected THAI_NATIONAL_ID in matches")
			}
		})
	}
}

func TestPatternGitLabToken(t *testing.T) {
	d := DefaultDetector()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"PAT", "token glpat-xxxxxxxxxxxxxxxxxxxx", true},
		{"deploy token", "token gldt-xxxxxxxxxxxxxxxxxxxx", true},
		{"CI build trigger", "token glcbt-xxxxxxxxxxxxxxxxxxxx", true},
		{"pipeline trigger", "token glptt-xxxxxxxxxxxxxxxxxxxx", true},
		{"too short", "glpat-short", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.Detect(tt.input)
			assert.Equal(t, tt.want, r.Detected)
		})
	}
}

func TestLocationsSortedDesc(t *testing.T) {
	d := DefaultDetector()
	text := "first=AKIAIOSFODNN7EXAMPL second=sk-abc123def456ghi789jkl012"
	r := d.Detect(text)
	assert.True(t, r.Detected)
	for i := 1; i < len(r.Locations); i++ {
		assert.Greater(t, r.Locations[i-1].Start, r.Locations[i].Start)
	}
}

func TestPatternEnvPasswordPass(t *testing.T) {
	d := NewDetector([]string{string(EntityEnvPassword)}, 200000)
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"_PASS", `SMTP_PASS = "qO4L32iZmbq54v261TGP1"`, true},
		{"_PWD", "DB_PWD=supersecret123", true},
		{"PASSWORD", `DB_PASSWORD = 'mydbpass123'`, true},
		{"PASSWD", "DB_PASSWD=secretpass", true},
		{"too short", `SMTP_PASS = "abc"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.Detect(tt.input)
			assert.Equal(t, tt.want, r.Detected)
		})
	}
}

func TestPatternAPIKeyGCP(t *testing.T) {
	d := DefaultDetector()
	r := d.Detect("key=AIzaSyA1234567890abcdefghijklmnopqrstuvwx")
	assert.True(t, r.Detected)
	found := false
	for _, m := range r.Matches {
		if m.Type == EntityAPIKeyGCP {
			found = true
		}
	}
	assert.True(t, found)
}

func TestPatternAPIKeyTencent(t *testing.T) {
	d := DefaultDetector()
	r := d.Detect("TENCENTCLOUD_SECRET_ID=AKID1234567890abcdefghijklmnopqrstuvwx")
	assert.True(t, r.Detected)
	found := false
	for _, m := range r.Matches {
		if m.Type == EntityAPIKeyTencent {
			found = true
		}
	}
	assert.True(t, found)
}

func TestPatternAPIKeyAlibaba(t *testing.T) {
	d := DefaultDetector()
	r := d.Detect("access_key=LTAI1234567890ab")
	assert.True(t, r.Detected)
	found := false
	for _, m := range r.Matches {
		if m.Type == EntityAPIKeyAlibaba {
			found = true
		}
	}
	assert.True(t, found)
}

func TestPatternAPIKeySlack(t *testing.T) {
	d := DefaultDetector()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"bot token", "xoxb-1234567890-abcdefghij", true},
		{"user token", "xoxp-1234567890-abcdefghij", true},
		{"app token", "xoxa-1234567890-abcdefghij", true},
		{"too short", "xoxb-short", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.Detect(tt.input)
			assert.Equal(t, tt.want, r.Detected)
		})
	}
}

func TestPatternAPIKeyStripe(t *testing.T) {
	d := NewDetector([]string{string(EntityAPIKeyStripe)}, 200000)
	// Construct key to avoid push protection false positive on literal
	key := "sk" + "_live_" + strings.Repeat("a", 24)
	r := d.Detect("key=" + key)
	assert.True(t, r.Detected)
}

func TestPatternAPIKeySendGrid(t *testing.T) {
	d := NewDetector([]string{string(EntityAPIKeySendGrid)}, 200000)
	// SG. + 22 chars + . + 43 chars
	r := d.Detect("SG.aaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	assert.True(t, r.Detected)
}

func TestPatternEnvToken(t *testing.T) {
	d := NewDetector([]string{string(EntityEnvToken)}, 200000)
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"dict key", `"token": "Vaka7n3Tv9x8oKBpE"`, true},
		{"variable", `API_TOKEN = "eyJhbGciOiJIUzI1NiJ9"`, true},
		{"assignment", `token = "abcdef1234567890xyz"`, true},
		{"too short", `"token": "abc123"`, false},
		{"token_type no match", `"token_type": "Bearer"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.Detect(tt.input)
			assert.Equal(t, tt.want, r.Detected, "input: %s, detected: %v", tt.input, r.Detected)
		})
	}
}

func TestPatternEnvCredential(t *testing.T) {
	d := NewDetector([]string{string(EntityEnvCredential)}, 200000)
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"CLIENT_ID", `CS_CLIENT_ID = "qy91wQ2RR94DHMESJGgOt0xEkHlG"`, true},
		{"_CID", `CS_MEMBER_CID = "22RlPmD49ra7IU2JKTByA1xM26iA"`, true},
		{"_ACCESS_KEY", `AWS_ACCESS_KEY = "AKIAIOSFODNN7EXAMPLE"`, true},
		{"too short", `CS_CLIENT_ID = "abc"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.Detect(tt.input)
			assert.Equal(t, tt.want, r.Detected, "input: %s, detected: %v", tt.input, r.Detected)
		})
	}
}

func TestPatternBasicAuthURL(t *testing.T) {
	d := NewDetector([]string{string(EntityBasicAuthURL)}, 200000)
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"https basic auth", `https://admin:s3cretP@ss@internal.example.com/api`, true},
		{"http basic auth", `http://user:password123@10.0.0.1:8080/health`, true},
		{"ftp creds", `ftp://deploy:d3pl0y@ftp.example.com/files`, true},
		{"no password", `https://example.com/page`, false},
		{"short password", `https://a:ab@host.com`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.Detect(tt.input)
			assert.Equal(t, tt.want, r.Detected)
		})
	}
}

func TestPatternVaultToken(t *testing.T) {
	d := NewDetector([]string{string(EntityVaultToken)}, 200000)
	vaultPrefix := "hvs" + "."
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"standard", "VAULT_TOKEN=" + vaultPrefix + strings.Repeat("a", 24), true},
		{"long", `vault_token = "` + vaultPrefix + `CAEaAaAAAAEAAAASAaAAAAAa"`, true},
		{"too short", `hvs.short`, false},
		{"wrong prefix", `s.hvs_aaaaaaaaaaaaaaaaaaaaaaaa`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.Detect(tt.input)
			assert.Equal(t, tt.want, r.Detected)
		})
	}
}

func TestPatternAzureCredential(t *testing.T) {
	d := NewDetector([]string{string(EntityAzureCredential)}, 200000)
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"client secret", `AZURE_CLIENT_SECRET = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"`, true},
		{"tenant id", `AZURE_TENANT_ID="a1b2c3d4-e5f6-7890-abcd-ef1234567890"`, true},
		{"aad token", `AAD_TOKEN='a1b2c3d4-e5f6-7890-abcd-ef1234567890'`, true},
		{"random uuid", `id="a1b2c3d4-e5f6-7890-abcd-ef1234567890"`, false},
		{"non-azure context", `REQUEST_ID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.Detect(tt.input)
			assert.Equal(t, tt.want, r.Detected)
		})
	}
}

func TestRealWorldVPNScript(t *testing.T) {
	d := DefaultDetector()
	script := `
PFUSER = "bro"
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
SMTP_PASS = "qO4L32iZmbq54v261TGP1"
CC_EMAIL="team@me.com"
FROM_EMAIL="me@me.com"
`
	r := d.Detect(script)
	assert.True(t, r.Detected)

	found := map[EntityType]bool{}
	for _, m := range r.Matches {
		found[m.Type] = true
	}

	// These must be detected
	assert.True(t, found[EntityEnvToken], "should detect token dict assignments")
	assert.True(t, found[EntityEnvSecret], "should detect CS_CLIENT_SECRET")
	assert.True(t, found[EntityEnvCredential], "should detect CS_CLIENT_ID and CS_MEMBER_CID")
	assert.True(t, found[EntityEnvPassword], "should detect SMTP_PASS")
}
