package secrets

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
)

func TestMaskSecrets(t *testing.T) {
	t.Run("no locations", func(t *testing.T) {
		result := MaskSecrets("hello", nil, nil)
		assert.Equal(t, "hello", result.MaskedText)
	})

	t.Run("single secret", func(t *testing.T) {
		ctx := masking.NewMaskContext()
		secret := "sk-abc123def456ghi789jkl"
		locs := []masking.SecretLocation{
			{Start: 8, End: 32, Type: "API_KEY_SK"},
		}
		result := MaskSecrets("use key "+secret+" now", locs, ctx)
		ph := ctx.ReverseMap[secret]
		assert.Contains(t, result.MaskedText, ph)
		assert.NotContains(t, result.MaskedText, secret)
		assert.Equal(t, secret, ctx.Mapping[ph])
	})

	t.Run("dedup same secret", func(t *testing.T) {
		ctx := masking.NewMaskContext()
		secret := "sk-abc123def456ghi789jkl"
		text := secret + " and " + secret
		// Positions: secret at 0-24, " and " at 24-29, secret at 29-53
		locs := []masking.SecretLocation{
			{Start: 0, End: 24, Type: "API_KEY_SK"},
			{Start: 29, End: 53, Type: "API_KEY_SK"},
		}
		result := MaskSecrets(text, locs, ctx)
		ph := ctx.ReverseMap[secret]
		expected := ph + " and " + ph
		assert.Equal(t, expected, result.MaskedText)
	})

	t.Run("nil context creates new", func(t *testing.T) {
		locs := []masking.SecretLocation{
			{Start: 0, End: 5, Type: "TEST"},
		}
		result := MaskSecrets("hello world", locs, nil)
		assert.NotNil(t, result.Context)
	})
}

func BenchmarkDetectSecrets(b *testing.B) {
	d := DefaultDetector()
	text := "my key is sk-abc123def456ghi789jkl012 and AWS AKIAIOSFODNN7EXAMPLE with Bearer abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQR"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Detect(text)
	}
}
