package textcomp

import (
	"strings"
	"testing"
)

func TestCompressBalanced(t *testing.T) {
	tc := New(Config{Enabled: true, Mode: "balanced"})

	removeTests := []struct {
		name   string
		input  string
		substr string // should NOT appear in output
	}{
		{"filler", "I would like to ask you to respond", "I would like to"},
		{"filler2", "Could you please help me with this?", "Could you please"},
		{"hedge", "This is sort of a problem", "sort of"},
		{"hedge2", "It is basically just really very very important", "basically"},
		{"verbose", "due to the fact that we need to", "due to the fact that"},
		{"verbose2", "in order to proceed with the task", "in order to"},
		{"verbose3", "with regard to the previous message", "with regard to"},
	}

	for _, tt := range removeTests {
		t.Run(tt.name, func(t *testing.T) {
			out, saved := tc.Compress(tt.input)
			if saved <= 0 {
				t.Errorf("expected savings > 0, got %d", saved)
			}
			if strings.Contains(out, tt.substr) {
				t.Errorf("output still contains %q: %s", tt.substr, out)
			}
			t.Logf("input:  %q", tt.input)
			t.Logf("output: %q", out)
			t.Logf("saved:  %d", saved)
		})
	}

	preserveTests := []struct {
		name   string
		input  string
		substr string // SHOULD appear in output (protected)
	}{
		{"preserve_code", "Use `due to the fact that` in code", "due to the fact that"},
		{"preserve_url", "See https://example.com/docs for more", "https://example.com/docs"},
	}

	for _, tt := range preserveTests {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := tc.Compress(tt.input)
			if !strings.Contains(out, tt.substr) {
				t.Errorf("protected content lost: %q not in %q", tt.substr, out)
			}
			t.Logf("output: %q", out)
		})
	}
}

func TestCompressAggressive(t *testing.T) {
	tc := New(Config{Enabled: true, Mode: "aggressive"})
	out, saved := tc.Compress("I think that we should consider this option. In my opinion, it is the best.")
	if saved <= 0 {
		t.Errorf("expected savings > 0, got %d", saved)
	}
	if strings.Contains(out, "I think that") {
		t.Errorf("aggressive should remove 'I think that': %s", out)
	}
	if strings.Contains(out, "In my opinion") {
		t.Errorf("aggressive should remove 'In my opinion': %s", out)
	}
	t.Logf("output: %q, saved: %d", out, saved)
}

func TestCompressDisabled(t *testing.T) {
	tc := New(Config{Enabled: false, Mode: "balanced"})
	input := "I would like to ask you to respond"
	out, saved := tc.Compress(input)
	if out != input {
		t.Errorf("disabled should return input unchanged")
	}
	if saved != 0 {
		t.Errorf("disabled should report 0 savings, got %d", saved)
	}
}

func TestCompressFencedCode(t *testing.T) {
	tc := New(Config{Enabled: true, Mode: "balanced"})
	input := "I would like to say:\n```\nI would like to preserve this code\n```\nand due to the fact that we need to continue"
	out, saved := tc.Compress(input)
	if strings.Contains(out, "due to the fact that") && !strings.Contains(out, "```") {
		t.Errorf("verbose phrase outside code should be compressed")
	}
	if !strings.Contains(out, "I would like to preserve this code") {
		t.Errorf("code block content should be preserved")
	}
	t.Logf("output: %q, saved: %d", out, saved)
}

func TestCompressEmpty(t *testing.T) {
	tc := New(Config{Enabled: true, Mode: "balanced"})
	out, saved := tc.Compress("")
	if out != "" {
		t.Errorf("empty input should return empty output")
	}
	if saved != 0 {
		t.Errorf("empty input should have 0 savings")
	}
}

func TestCompressURLs(t *testing.T) {
	tc := New(Config{Enabled: true, Mode: "balanced"})
	input := "I would like to visit https://example.com/path?q=test and also check out https://github.com/repo"
	out, _ := tc.Compress(input)
	if !strings.Contains(out, "https://example.com/path?q=test") {
		t.Errorf("URL should be preserved")
	}
	if !strings.Contains(out, "https://github.com/repo") {
		t.Errorf("URL should be preserved")
	}
	t.Logf("output: %q", out)
}
