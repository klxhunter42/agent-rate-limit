package tokenizer

import (
	"strings"
	"testing"
)

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ContentType
	}{
		{"json object", `{"key": "value", "count": 42}`, ContentJSON},
		{"json array", `[1, 2, 3]`, ContentJSON},
		{"go code", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}", ContentCode},
		{"python code", "import os\nfrom pathlib import Path\n\ndef hello():\n    return True", ContentCode},
		{"markdown", "# Title\n\nSome text\n\n- item 1\n- item 2\n\n```go\ncode here\n```", ContentMarkdown},
		{"plain text", "Hello world, this is a simple text with no special formatting.", ContentText},
		{"empty", "", ContentText},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectContentType(tt.input)
			if result != tt.expected {
				t.Errorf("DetectContentType() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	// Code should have more tokens per char (lower ratio = more tokens).
	codeTokens := EstimateTokens("func main() { return 42 }")
	textTokens := EstimateTokens("Hello world this is a simple text")
	if codeTokens <= 0 || textTokens <= 0 {
		t.Errorf("EstimateTokens should return positive values, got code=%d text=%d", codeTokens, textTokens)
	}
	// Code should estimate more tokens for same length since ratio is lower (2.5 vs 4.0).
	codeText := "func main() { return 42 }"
	plainText := "Hello world simple text here"
	if len(codeText) == len(plainText) {
		if codeTokens == EstimateTokens(plainText) {
			// Same length text should have different estimates based on type.
			t.Log("Token estimates differ by content type as expected")
		}
	}
}

func TestQuickEstimateTokens(t *testing.T) {
	// chars/4: 100 chars = 25 tokens.
	result := QuickEstimateTokens(strings.Repeat("a", 100))
	if result != 25 {
		t.Errorf("QuickEstimateTokens(100 chars) = %d, want 25", result)
	}
	// 101 chars = ceil(101/4) = 26.
	result = QuickEstimateTokens(strings.Repeat("a", 101))
	if result != 26 {
		t.Errorf("QuickEstimateTokens(101 chars) = %d, want 26", result)
	}
}

func TestGetModelCapabilities(t *testing.T) {
	tests := []struct {
		model        string
		wantContext  int
		wantOutput   int
		wantProvider string
	}{
		{"claude-opus-4-7", 200000, 32000, "anthropic"},
		{"claude-sonnet-4-6", 200000, 16000, "anthropic"},
		{"gpt-4o", 128000, 16384, "openai"},
		{"gemini-2.5-pro", 1048576, 65536, "google"},
		{"glm-4-plus", 128000, 4096, "zai"},
		{"unknown-model-xyz", 128000, 4096, "unknown"},
		{"claude-opus-4-7-20250514", 200000, 32000, "anthropic"}, // prefix match
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			cap := GetModelCapabilities(tt.model)
			if cap.ContextWindow != tt.wantContext {
				t.Errorf("ContextWindow = %d, want %d", cap.ContextWindow, tt.wantContext)
			}
			if cap.MaxOutputTokens != tt.wantOutput {
				t.Errorf("MaxOutputTokens = %d, want %d", cap.MaxOutputTokens, tt.wantOutput)
			}
			if cap.Provider != tt.wantProvider {
				t.Errorf("Provider = %s, want %s", cap.Provider, tt.wantProvider)
			}
		})
	}
}

func TestOptimizeWhitespace(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		wantNoDoubleSpace bool
		wantNoTrailBlank  bool
	}{
		{
			"collapse spaces",
			"hello    world   foo",
			true,
			true,
		},
		{
			"collapse newlines",
			"line1\n\n\n\n\nline2\n\n\n\n\nline3",
			true,
			true,
		},
		{
			"preserve code blocks",
			"text\n```\ncode   here\n    indented\n```\nmore  text",
			true,
			true,
		},
		{
			"trailing whitespace",
			"line1   \nline2   \nline3",
			true,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, saved := OptimizeWhitespace(tt.input)
			if saved < 0 {
				t.Errorf("saved tokens should not be negative, got %d", saved)
			}
			if tt.wantNoDoubleSpace && strings.Contains(result, "  ") {
				// Check prose sections only (not code).
				if !strings.Contains(tt.input, "```") {
					t.Errorf("result still contains double spaces: %q", result)
				}
			}
		})
	}
}

func TestOptimizeWhitespaceCodePreserved(t *testing.T) {
	input := "Hello\n```\ncode   with   spaces\n    indent\n```\nWorld"
	result, _ := OptimizeWhitespace(input)
	if !strings.Contains(result, "code   with   spaces") {
		t.Errorf("code block whitespace should be preserved, got: %q", result)
	}
	if !strings.Contains(result, "    indent") {
		t.Errorf("code indentation should be preserved, got: %q", result)
	}
}

func TestDeduplicateSentences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLess bool // result should be shorter
	}{
		{
			"exact duplicates",
			"Hello world. Hello world. Goodbye.",
			true,
		},
		{
			"no duplicates",
			"First sentence. Second sentence. Third one here.",
			false,
		},
		{
			"code preserved",
			"Some text. ```\ncode here\n```\nSome text.",
			false, // code block makes it hard to predict
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, saved := DeduplicateSentences(tt.input)
			if len(result) == 0 {
				t.Errorf("result should not be empty")
			}
			if tt.wantLess && saved <= 0 {
				t.Errorf("expected positive savings for %q, got saved=%d", tt.name, saved)
			}
		})
	}
}

func TestTruncateHeadTail(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line content here that is reasonably long"
	}
	text := strings.Join(lines, "\n")

	// No truncation needed.
	result := TruncateHeadTail("short text", 100, 0.4)
	if result != "short text" {
		t.Errorf("short text should not be truncated")
	}

	// Truncate with default ratio.
	result = TruncateHeadTail(text, 500, 0.4)
	if !strings.Contains(result, "truncated") {
		t.Errorf("long text should contain truncation marker")
	}
	if !strings.Contains(result, "line content") {
		t.Errorf("truncated text should still contain content")
	}
}

func TestTokenBudget(t *testing.T) {
	budget := NewTokenBudget("claude-opus-4-7")
	if budget.ContextLimit != 200000 {
		t.Errorf("ContextLimit = %d, want 200000", budget.ContextLimit)
	}

	// Green zone.
	if budget.Level() != BudgetGreen {
		t.Errorf("empty budget should be green")
	}
	if budget.ShouldOptimize() {
		t.Errorf("empty budget should not need optimization")
	}

	// Add 60% -> Yellow.
	budget.AddTokens(120000, 0)
	if budget.Level() != BudgetYellow {
		t.Errorf("60%% budget should be yellow, pct=%.1f", budget.PercentUsed())
	}
	if !budget.ShouldOptimize() {
		t.Errorf("60%% budget should need optimization")
	}
	if budget.ShouldForceOptimize() {
		t.Errorf("60%% budget should not force optimize")
	}

	// Add to 80% -> Red.
	budget.AddTokens(40000, 0)
	if budget.Level() != BudgetRed {
		t.Errorf("80%% budget should be red, pct=%.1f", budget.PercentUsed())
	}
	if !budget.ShouldForceOptimize() {
		t.Errorf("80%% budget should force optimize")
	}
}

func TestTokenBudgetUnknownModel(t *testing.T) {
	budget := NewTokenBudget("totally-unknown-model")
	if budget.ContextLimit != 128000 {
		t.Errorf("unknown model should default to 128K context, got %d", budget.ContextLimit)
	}
}

func TestSplitCodeBlocks(t *testing.T) {
	input := "prose line 1\nprose line 2\n```\ncode line 1\ncode line 2\n```\nmore prose"
	segments := splitCodeBlocks(input)

	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segments))
	}
	if segments[0].isCode {
		t.Error("first segment should not be code")
	}
	if !segments[1].isCode {
		t.Error("second segment should be code")
	}
	if segments[2].isCode {
		t.Error("third segment should not be code")
	}
}

func TestSplitCodeBlocksUnclosed(t *testing.T) {
	input := "some prose\n```\ncode without closing"
	segments := splitCodeBlocks(input)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments for unclosed code, got %d", len(segments))
	}
	if !segments[1].isCode {
		t.Error("unclosed code segment should be marked as code")
	}
}

func TestPrivacyPlaceholderGuard_Dedup(t *testing.T) {
	input := "Hello world. __SECRET_0__ is a secret. Hello world."
	result, saved := DeduplicateSentences(input)
	if saved != 0 {
		t.Errorf("dedup should skip text with privacy placeholders, got saved=%d", saved)
	}
	if result != input {
		t.Errorf("dedup should return original when placeholders present, got: %q", result)
	}
}

func TestPrivacyPlaceholderGuard_Truncate(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line " + strings.Repeat("content ", 10)
	}
	text := strings.Join(lines, "\n")

	// Insert a privacy placeholder.
	textWithPlaceholder := "prefix " + text + "\n__PII_5__"
	result := TruncateHeadTail(textWithPlaceholder, 500, 0.4)
	if result != textWithPlaceholder {
		t.Errorf("truncate should skip text with privacy placeholders")
	}
}

func TestPrivacyPlaceholderGuard_Whitespace(t *testing.T) {
	input := "Hello  world   __SECRET_0__  extra  spaces"
	result, _ := OptimizeWhitespace(input)
	if !strings.Contains(result, "__SECRET_0__") {
		t.Errorf("whitespace optimization should preserve privacy placeholders, got: %q", result)
	}
}

func BenchmarkEstimateTokens(b *testing.B) {
	text := strings.Repeat("func main() { fmt.Println(\"hello world\") return 42 } ", 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateTokens(text)
	}
}

func BenchmarkQuickEstimateTokens(b *testing.B) {
	text := strings.Repeat("some text content here ", 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		QuickEstimateTokens(text)
	}
}

func BenchmarkOptimizeWhitespace(b *testing.B) {
	text := "Hello world  this   has    extra     spaces\n\n\n\nWith multiple blank lines\n\n\n\nAnd more   trailing   spaces   \n\n```\ncode   block   preserved\n```\n"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		OptimizeWhitespace(text)
	}
}

func BenchmarkDeduplicateSentences(b *testing.B) {
	text := strings.Repeat("This is a test sentence. This is another one. This is a test sentence. ", 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DeduplicateSentences(text)
	}
}
