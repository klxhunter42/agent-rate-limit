package secrets

import (
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
