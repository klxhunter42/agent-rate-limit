package masking

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJsonEscape_ControlChars(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"double quote", `"hello"`, `\"hello\"`},
		{"backslash", `a\b`, `a\\b`},
		{"newline", "a\nb", `a\nb`},
		{"carriage return", "a\rb", `a\rb`},
		{"tab", "a\tb", `a\tb`},
		{"backspace", "a\bb", `a\bb`},
		{"form feed", "a\x0cb", `a\fb`},
		{"bell", "a\x07b", `ab`},
		{"vertical tab", "a\x0bb", `ab`},
		{"soh", "a\x01b", `ab`},
		{"unit separator", "a\x1fb", `ab`},
		{"normal", "hello world", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jsonEscape(tt.input)
			// Verify result is valid in JSON context
			wrapped := `{"v":"` + got + `"}`
			parsed := map[string]string{}
			assert.NoError(t, json.Unmarshal([]byte(wrapped), &parsed),
				"jsonEscape(%q) produced invalid JSON: %s", tt.name, got)
			assert.Equal(t, tt.input, parsed["v"],
				"jsonEscape(%q) did not round-trip", tt.name)
		})
	}
}

func TestRestorePlaceholdersJSON_ProducesValidJSON(t *testing.T) {
	// Values with control chars that would break JSON if not escaped
	email := "test\x08" + "user@example.com"
	phone := "+1\x0c" + "555-0100"
	secret := "sk\x01\x02" + "secret"
	quoted := "has\"quote\\and\x08bs"

	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = email
	ctx.Mapping["[[PHONE_NUMBER_1]]"] = phone
	ctx.Mapping["[[KEY_1]]"] = secret
	ctx.Mapping["[[KEY_2]]"] = quoted

	input := `{"email":"[[EMAIL_ADDRESS_1]]","phone":"[[PHONE_NUMBER_1]]","key":"[[KEY_1]]","q":"[[KEY_2]]"}`
	result := ctx.RestorePlaceholdersJSON(input)

	// Must be valid JSON
	parsed := map[string]string{}
	err := json.Unmarshal([]byte(result), &parsed)
	assert.NoError(t, err, "Result must be valid JSON: %s", result)

	// Values must round-trip correctly
	assert.Equal(t, email, parsed["email"])
	assert.Equal(t, phone, parsed["phone"])
	assert.Equal(t, secret, parsed["key"])
	assert.Equal(t, quoted, parsed["q"])
}

func TestRestorePlaceholders_NoEscape(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[KEY_1]]"] = "sk\x01secret"

	result := ctx.RestorePlaceholders("key=[[KEY_1]]")
	assert.Equal(t, "key=sk\x01secret", result)
}

func TestRestorePlaceholdersJSON_NormalValue(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[EMAIL_ADDRESS_1]]"] = "user@example.com"

	result := ctx.RestorePlaceholdersJSON(`{"to":"[[EMAIL_ADDRESS_1]]"}`)
	assert.Equal(t, `{"to":"user@example.com"}`, result)
}

func TestRestorePlaceholdersJSON_MultipleSamePlaceholder(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[KEY_1]]"] = "sk\x07" + "abc"

	result := ctx.RestorePlaceholdersJSON(`{"a":"[[KEY_1]]","b":"[[KEY_1]]"}`)
	parsed := map[string]string{}
	assert.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "sk\x07abc", parsed["a"])
	assert.Equal(t, "sk\x07abc", parsed["b"])
}

func TestStreamUnmasker_ControlCharRoundTrip(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[KEY_1]]"] = "sk\x01\x02" + "secret"

	u := NewStreamUnmasker(ctx, nil)

	chunks := []string{"Use [[KE", "Y_1]] now"}
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(u.ProcessChunk(c))
	}
	out.WriteString(u.Flush())

	assert.Equal(t, "Use sk\x01\x02secret now", out.String())
}

func TestProcessChunkJSON_BufferedSplit(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "192.168.1.100"
	ctx.Mapping["[[SECRET_1]]"] = "has\"quote"

	u := NewStreamUnmasker(ctx, nil)

	// Placeholder split across chunks
	chunk1 := `{"server":"[[IP_ADDR`
	chunk2 := `ESS_1]]"}`
	out1 := u.ProcessChunkJSON(chunk1)
	out2 := u.ProcessChunkJSON(chunk2)
	flushed := u.Flush()

	full := out1 + out2 + flushed
	assert.Contains(t, full, "192.168.1.100")
	assert.NotContains(t, full, "[[IP_ADDRESS_1]]")

	// Verify result is valid JSON
	assert.NoError(t, json.Unmarshal([]byte(full), &map[string]any{}))
}

func TestProcessChunkJSON_ValueWithQuotes(t *testing.T) {
	secretsCtx := NewMaskContext()
	secretsCtx.Mapping["[[SECRET_1]]"] = `has "quotes" and \backslash`

	piiCtx := NewMaskContext()
	piiCtx.Mapping["[[IP_ADDRESS_1]]"] = "10.0.0.1"

	u := NewStreamUnmasker(piiCtx, secretsCtx)

	input := `{"a":"[[SECRET_1]]","b":"[[IP_ADDRESS_1]]"}`
	out := u.ProcessChunkJSON(input)
	flushed := u.Flush()
	full := out + flushed

	assert.NotContains(t, full, "[[SECRET_1]]")
	assert.NotContains(t, full, "[[IP_ADDRESS_1]]")
	assert.Contains(t, full, `has \"quotes\" and \\backslash`)
	assert.Contains(t, full, "10.0.0.1")

	parsed := map[string]string{}
	assert.NoError(t, json.Unmarshal([]byte(full), &parsed))
	assert.Equal(t, `has "quotes" and \backslash`, parsed["a"])
	assert.Equal(t, "10.0.0.1", parsed["b"])
}

func TestProcessChunkJSON_MultipleSplits(t *testing.T) {
	ctx := NewMaskContext()
	ctx.Mapping["[[IP_ADDRESS_1]]"] = "192.168.1.1"
	ctx.Mapping["[[IP_ADDRESS_2]]"] = "10.0.0.5"

	u := NewStreamUnmasker(ctx, nil)

	chunks := []string{
		`{"from":"[[IP_ADDRESS_`,
		`1]]","to":"[[`,
		`IP_ADDRESS_2]]"}`,
	}
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(u.ProcessChunkJSON(c))
	}
	out.WriteString(u.Flush())

	full := out.String()
	assert.Contains(t, full, "192.168.1.1")
	assert.Contains(t, full, "10.0.0.5")
	assert.NotContains(t, full, "[[IP_ADDRESS_")
	assert.NoError(t, json.Unmarshal([]byte(full), &map[string]any{}))
}

func TestRestorePlaceholdersJSON_AllC0ControlChars(t *testing.T) {
	// Verify every C0 control char (0x00-0x1F except already-handled ones) is escaped
	for b := byte(0x00); b < 0x20; b++ {
		val := "a" + string([]byte{b}) + "z"
		ctx := NewMaskContext()
		ctx.Mapping["[[X_1]]"] = val

		result := ctx.RestorePlaceholdersJSON(`{"v":"[[X_1]]"}`)
		parsed := map[string]string{}
		unErr := json.Unmarshal([]byte(result), &parsed)
		assert.NoError(t, unErr, "byte 0x%02x produced invalid JSON: %s", b, result)
		assert.Equal(t, val, parsed["v"], "byte 0x%02x round-trip failed", b)
	}
}
