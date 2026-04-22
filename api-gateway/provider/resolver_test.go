package provider

import (
	"testing"
)

func TestModelResolution(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"claude-haiku-4-5-20251001", "claude-oauth"},
		{"claude-sonnet-4-6", "claude-oauth"},
		{"claude-opus-4-7", "claude-oauth"},
		{"gemini-2.5-flash", "gemini-oauth"},
		{"gpt-4", "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if !ModelBelongsToProvider(tt.model, tt.expected) {
				t.Errorf("model %s should belong to provider %s", tt.model, tt.expected)
			}
		})
	}
}

func TestRouteTable(t *testing.T) {
	routes := []string{"claude-oauth", "anthropic", "openai", "gemini-oauth"}
	for _, route := range routes {
		if _, ok := providerRouteTable[route]; !ok {
			t.Errorf("route %s not found in provider route table", route)
		}
	}
}
