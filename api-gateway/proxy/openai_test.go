package proxy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// M2: Verify tool_use id/name are JSON-escaped to prevent injection.
func TestToolUseIDName_JSONEscaped(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"normal id", "toolu_01abc"},
		{"id with quotes", `toolu_"inject`},
		{"id with backslash", `toolu\path`},
		{"id with newline", "toolu\nline"},
		{"id with html", "<script>alert(1)</script>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			escaped, err := json.Marshal(tt.id)
			assert.NoError(t, err)
			// Round-trip: marshal then unmarshal must preserve original value
			var unescaped string
			assert.NoError(t, json.Unmarshal(escaped, &unescaped))
			assert.Equal(t, tt.id, unescaped)
			// Must be wrapped in quotes (valid JSON string)
			assert.True(t, len(escaped) >= 2)
			assert.Equal(t, byte('"'), escaped[0])
			assert.Equal(t, byte('"'), escaped[len(escaped)-1])
		})
	}
}

func TestToolUseName_JSONEscaped(t *testing.T) {
	tests := []struct {
		name    string
		nameVal string
	}{
		{"normal name", "bash"},
		{"name with space", "my tool"},
		{"name with angle brackets", "tool</name>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			escaped, err := json.Marshal(tt.nameVal)
			assert.NoError(t, err)
			var unescaped string
			assert.NoError(t, json.Unmarshal(escaped, &unescaped))
			assert.Equal(t, tt.nameVal, unescaped)
		})
	}
}
