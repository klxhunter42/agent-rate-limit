package toolfilter

import (
	"strings"
	"testing"
)

func makeTools(n int) []Tool {
	tools := make([]Tool, n)
	for i := 0; i < n; i++ {
		tools[i] = Tool{
			Name:        "tool_" + strings.Repeat("x", 10-i%10),
			Description: "A tool that does something useful for testing purposes " + strings.Repeat("word ", i),
		}
	}
	// Ensure core tools exist
	tools[0] = Tool{Name: "Read", Description: "Read files from the filesystem"}
	tools[1] = Tool{Name: "Edit", Description: "Edit files in the codebase"}
	tools[2] = Tool{Name: "Write", Description: "Write files to disk"}
	tools[3] = Tool{Name: "Bash", Description: "Execute shell commands"}
	return tools
}

func TestFilterToolsBelowMax(t *testing.T) {
	tf := New(Config{Enabled: true, MaxTools: 15, AlwaysKeep: "Read,Edit,Write,Bash"})
	tools := makeTools(10)
	result := tf.FilterTools(tools, "find the function")
	if len(result) != 10 {
		t.Errorf("expected 10 tools (below max), got %d", len(result))
	}
}

func TestFilterToolsAboveMax(t *testing.T) {
	tf := New(Config{Enabled: true, MaxTools: 15, AlwaysKeep: "Read,Edit,Write,Bash"})
	tools := makeTools(30)
	result := tf.FilterTools(tools, "find the function")
	if len(result) != 15 {
		t.Errorf("expected 15 tools, got %d", len(result))
	}
}

func TestFilterToolsAlwaysKept(t *testing.T) {
	tf := New(Config{Enabled: true, MaxTools: 10, AlwaysKeep: "Read,Edit,Write,Bash"})
	tools := makeTools(30)
	result := tf.FilterTools(tools, "find the function")

	names := make(map[string]bool)
	for _, t := range result {
		names[t.Name] = true
	}
	for _, name := range []string{"Read", "Edit", "Write", "Bash"} {
		if !names[name] {
			t.Errorf("always-keep tool %s not in result", name)
		}
	}
}

func TestFilterToolsCodeIntent(t *testing.T) {
	tf := New(Config{Enabled: true, MaxTools: 8, AlwaysKeep: "Read,Edit,Write,Bash"})
	tools := []Tool{
		{Name: "Read", Description: "Read files"},
		{Name: "Edit", Description: "Edit files"},
		{Name: "Write", Description: "Write files"},
		{Name: "Bash", Description: "Shell commands"},
		{Name: "find_symbol", Description: "Find symbols in codebase"},
		{Name: "get_function_source", Description: "Get function source code"},
		{Name: "search_codebase", Description: "Search across project"},
		{Name: "replace_symbol_source", Description: "Replace symbol source code"},
		{Name: "move_symbol", Description: "Move symbol with import fixup"},
		{Name: "analyze_config", Description: "Analyze configuration files"},
		{Name: "get_git_status", Description: "Git status structured"},
		{Name: "detect_breaking_changes", Description: "Detect breaking changes between refs"},
	}
	result := tf.FilterTools(tools, "fix the bug in the login function")
	if len(result) > 8 {
		t.Errorf("expected <= 8 tools, got %d", len(result))
	}

	names := make(map[string]bool)
	for _, t := range result {
		names[t.Name] = true
	}
	// Code-relevant tools should be prioritized
	if !names["replace_symbol_source"] {
		t.Error("replace_symbol_source should be kept for code intent")
	}
}

func TestFilterToolsSearchIntent(t *testing.T) {
	tf := New(Config{Enabled: true, MaxTools: 8, AlwaysKeep: "Read,Edit,Write,Bash"})
	tools := []Tool{
		{Name: "Read", Description: "Read files"},
		{Name: "Edit", Description: "Edit files"},
		{Name: "Write", Description: "Write files"},
		{Name: "Bash", Description: "Shell commands"},
		{Name: "find_symbol", Description: "Find symbols in codebase"},
		{Name: "get_function_source", Description: "Get function source code"},
		{Name: "search_codebase", Description: "Search across project"},
		{Name: "get_git_status", Description: "Git status structured"},
		{Name: "replace_symbol_source", Description: "Replace symbol source code"},
		{Name: "analyze_config", Description: "Analyze configuration files"},
		{Name: "get_full_context", Description: "Get full symbol context"},
	}
	result := tf.FilterTools(tools, "find where the send_message function is defined")
	if len(result) > 8 {
		t.Errorf("expected <= 8 tools, got %d", len(result))
	}

	names := make(map[string]bool)
	for _, t := range result {
		names[t.Name] = true
	}
	if !names["find_symbol"] {
		t.Error("find_symbol should be kept for search intent")
	}
}

func TestFilterToolsDisabled(t *testing.T) {
	tf := New(Config{Enabled: false, MaxTools: 5, AlwaysKeep: "Read"})
	tools := makeTools(30)
	result := tf.FilterTools(tools, "fix bug")
	if len(result) != 30 {
		t.Errorf("disabled should return all tools, got %d", len(result))
	}
}

func TestFilterToolsEmpty(t *testing.T) {
	tf := New(Config{Enabled: true, MaxTools: 5, AlwaysKeep: ""})
	result := tf.FilterTools(nil, "test")
	if result != nil {
		t.Error("empty input should return nil")
	}
}

func TestClassifyIntent(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"fix the login bug", "code"},
		{"find all references to UserService", "search"},
		{"explain how the cache works", "analysis"},
		{"run the test suite", "action"},
		{"", "code"}, // default
	}
	for _, tt := range tests {
		got := classifyIntent(tt.input)
		if got != tt.expected {
			t.Errorf("classifyIntent(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestParseAlwaysKeep(t *testing.T) {
	result := parseAlwaysKeep("Read,Edit,Write,Bash")
	if len(result) != 4 {
		t.Errorf("expected 4, got %d", len(result))
	}
	if !result["Read"] || !result["Bash"] {
		t.Error("missing expected keys")
	}
}

func TestParseAlwaysKeepEmpty(t *testing.T) {
	result := parseAlwaysKeep("")
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}
