package pii

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
)

func TestRegexDetector_Detect(t *testing.T) {
	d := NewRegexDetector([]string{"EMAIL_ADDRESS", "PHONE_NUMBER", "CREDIT_CARD", "SSN", "IBAN", "IP_ADDRESS", "THAI_NATIONAL_ID", "THAI_PHONE"})

	t.Run("empty text", func(t *testing.T) {
		result := d.Detect("")
		assert.False(t, result.HasPII)
	})

	t.Run("no PII", func(t *testing.T) {
		result := d.Detect("Hello, how are you today?")
		assert.False(t, result.HasPII)
	})

	t.Run("email detection", func(t *testing.T) {
		result := d.Detect("Contact me at user@example.com please")
		assert.True(t, result.HasPII)
		assert.Len(t, result.Entities, 1)
		assert.Equal(t, "EMAIL_ADDRESS", result.Entities[0].EntityType)
		assert.Equal(t, "user@example.com", "Contact me at user@example.com please"[result.Entities[0].Start:result.Entities[0].End])
	})

	t.Run("phone detection", func(t *testing.T) {
		result := d.Detect("Call me at 555-123-4567")
		assert.True(t, result.HasPII)
		found := false
		for _, e := range result.Entities {
			if e.EntityType == "PHONE_NUMBER" {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("credit card detection", func(t *testing.T) {
		result := d.Detect("Card: 4111-1111-1111-1111")
		assert.True(t, result.HasPII)
		found := false
		for _, e := range result.Entities {
			if e.EntityType == "CREDIT_CARD" {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("SSN detection", func(t *testing.T) {
		result := d.Detect("SSN: 123-45-6789")
		assert.True(t, result.HasPII)
		found := false
		for _, e := range result.Entities {
			if e.EntityType == "SSN" {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("IPv4 detection", func(t *testing.T) {
		result := d.Detect("Server at 192.168.1.1 is down")
		assert.True(t, result.HasPII)
		found := false
		for _, e := range result.Entities {
			if e.EntityType == "IP_ADDRESS" {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("Thai national ID detection", func(t *testing.T) {
		result := d.Detect("ID: 1-1001-00001-23-4")
		assert.True(t, result.HasPII)
		found := false
		for _, e := range result.Entities {
			if e.EntityType == "THAI_NATIONAL_ID" {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("Thai phone detection", func(t *testing.T) {
		result := d.Detect("Call 081-234-5678")
		assert.True(t, result.HasPII)
		found := false
		for _, e := range result.Entities {
			if e.EntityType == "THAI_PHONE" {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("multiple entities", func(t *testing.T) {
		result := d.Detect("Email user@example.com and phone 555-123-4567")
		assert.True(t, result.HasPII)
		assert.GreaterOrEqual(t, len(result.Entities), 2)
	})

	t.Run("disabled entity", func(t *testing.T) {
		d2 := NewRegexDetector([]string{"EMAIL_ADDRESS"})
		result := d2.Detect("Call 555-123-4567")
		assert.False(t, result.HasPII)
	})

	t.Run("phone in URL not detected", func(t *testing.T) {
		result := d.Detect("https://example.com/file.png?Expires=1777798284&Signature=abc123")
		for _, e := range result.Entities {
			assert.NotEqual(t, "PHONE_NUMBER", e.EntityType, "phone should not be detected inside URL")
		}
	})

	t.Run("IP in URL still detected", func(t *testing.T) {
		result := d.Detect("https://192.168.1.1:8080/api/v1/messages")
		found := false
		for _, e := range result.Entities {
			if e.EntityType == "IP_ADDRESS" {
				found = true
			}
		}
		assert.True(t, found, "IP inside URL should still be detected")
	})

	t.Run("phone outside URL still detected", func(t *testing.T) {
		result := d.Detect("Call me at 415-555-1234 or visit https://example.com")
		found := false
		for _, e := range result.Entities {
			if e.EntityType == "PHONE_NUMBER" {
				found = true
			}
		}
		assert.True(t, found, "phone outside URL should still be detected")
	})
}

func TestMaskPII(t *testing.T) {
	t.Run("no entities", func(t *testing.T) {
		result := MaskPII("hello", nil, nil)
		assert.Equal(t, "hello", result.MaskedText)
	})

	t.Run("single entity", func(t *testing.T) {
		ctx := masking.NewMaskContext()
		entities := []masking.PIIEntity{
			{EntityType: "EMAIL_ADDRESS", Start: 0, End: 16, Score: 0.9},
		}
		result := MaskPII("user@example.com is my email", entities, ctx)
		assert.Contains(t, result.MaskedText, "[[EMAIL_ADDRESS_1]]")
		assert.Equal(t, "user@example.com", ctx.Mapping["[[EMAIL_ADDRESS_1]]"])
	})

	t.Run("dedup same entity", func(t *testing.T) {
		ctx := masking.NewMaskContext()
		entities := []masking.PIIEntity{
			{EntityType: "EMAIL_ADDRESS", Start: 0, End: 16, Score: 0.9},
			{EntityType: "EMAIL_ADDRESS", Start: 22, End: 38, Score: 0.85},
		}
		text := "user@example.com xx user@example.com!!"
		result := MaskPII(text, entities, ctx)
		assert.Contains(t, result.MaskedText, "[[EMAIL_ADDRESS_1]]")
	})
}
