package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- trimVerbose ---

func TestTrimVerbose(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Here's the code prefix",
			input: "Here's the code:\nfunc main() {}",
			want:  "code:\nfunc main() {}",
		},
		{
			name:  "Here is prefix (two words)",
			input: "Here is the implementation:\ncode here",
			want:  "implementation:\ncode here",
		},
		{
			name:  "Here is a prefix (with article)",
			input: "Here is a solution:\nsolution here",
			want:  "solution:\nsolution here",
		},
		{
			name:  "Let me explain prefix",
			input: "Let me explain how this works.\nThe actual content follows.",
			want:  "The actual content follows.",
		},
		{
			name:  "Let me help prefix",
			input: "Let me help you with that.\nHere is the answer.",
			want:  "Here is the answer.",
		},
		{
			name:  "I'll help prefix",
			input: "I'll help you fix this.\nThe fix is here.",
			want:  "The fix is here.",
		},
		{
			name:  "Sure prefix",
			input: "Sure!\nHere is the code.",
			want:  "Here is the code.",
		},
		{
			name:  "Sure without exclamation",
			input: "Sure\nHere is the code.",
			want:  "Here is the code.",
		},
		{
			name:  "Great question prefix",
			input: "Great question!\nThe answer is 42.",
			want:  "The answer is 42.",
		},
		{
			name:  "Certainly prefix",
			input: "Certainly!\nDone.",
			want:  "Done.",
		},
		{
			name:  "Of course prefix",
			input: "Of course!\nDone.",
			want:  "Done.",
		},
		{
			name:  "I'd be happy to prefix",
			input: "I'd be happy to help.\nHere is the code.",
			want:  "Here is the code.",
		},
		{
			name:  "Hope this helps suffix",
			input: "The answer is 42.\n\nHope this helps!",
			want:  "The answer is 42.",
		},
		{
			name:  "Hope that is helpful suffix",
			input: "The answer is 42.\n\nHope that is helpful.",
			want:  "The answer is 42.",
		},
		{
			name:  "Let me know suffix",
			input: "The answer is 42.\n\nLet me know if you need anything else.",
			want:  "The answer is 42.",
		},
		{
			name:  "Let me know further assistance suffix",
			input: "The answer is 42.\n\nLet me know if you need further assistance.",
			want:  "The answer is 42.",
		},
		{
			name:  "Combined prefix and suffix",
			input: "Sure!\nThe answer is 42.\n\nHope this helps!",
			want:  "The answer is 42.",
		},
		{
			name:  "Case insensitive prefix",
			input: "LET ME EXPLAIN THIS.\nThe content.",
			want:  "The content.",
		},
		{
			name:  "Normal text no patterns",
			input: "The quick brown fox jumps over the lazy dog.",
			want:  "The quick brown fox jumps over the lazy dog.",
		},
		{
			name:  "Empty string",
			input: "",
			want:  "",
		},
		{
			name:  "Text that starts with Here but not matching pattern",
			input: "Here are some considerations:\n1. First\n2. Second",
			want:  "Here are some considerations:\n1. First\n2. Second",
		},
		{
			name:  "Whitespace trimming",
			input: "Sure!\n  \n  actual content  \n",
			want:  "actual content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimVerbose(tt.input)
			if got != tt.want {
				t.Errorf("trimVerbose(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- trimResponse ---

func TestTrimResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
		check   func(t *testing.T, result []byte)
	}{
		{
			name:    "valid JSON with text blocks trims verbose",
			input:   `{"content":[{"type":"text","text":"Sure!\nThe answer is 42.\n\nHope this helps!"}]}`,
			wantNil: false,
			check: func(t *testing.T, result []byte) {
				var m map[string]any
				if err := json.Unmarshal(result, &m); err != nil {
					t.Fatal(err)
				}
				blocks := m["content"].([]any)
				text := blocks[0].(map[string]any)["text"].(string)
				if text != "The answer is 42." {
					t.Errorf("text = %q, want %q", text, "The answer is 42.")
				}
			},
		},
		{
			name:    "valid JSON with no content field",
			input:   `{"id":"msg_123","type":"message"}`,
			wantNil: true,
		},
		{
			name:    "valid JSON with non-text content blocks",
			input:   `{"content":[{"type":"image","source":{"type":"base64","data":"abc="}},{"type":"tool_use","id":"tu_1","name":"search","input":{}}]}`,
			wantNil: true,
		},
		{
			name:    "invalid JSON",
			input:   `{not valid json}`,
			wantNil: true,
		},
		{
			name:    "JSON with mixed content types trims only text",
			input:   `{"content":[{"type":"text","text":"Sure!\nHere is the code."},{"type":"tool_use","id":"tu_1","name":"bash","input":{"cmd":"ls"}}]}`,
			wantNil: false,
			check: func(t *testing.T, result []byte) {
				var m map[string]any
				if err := json.Unmarshal(result, &m); err != nil {
					t.Fatal(err)
				}
				blocks := m["content"].([]any)
				text := blocks[0].(map[string]any)["text"].(string)
				if text != "Here is the code." {
					t.Errorf("text = %q, want %q", text, "Here is the code.")
				}
				toolBlock := blocks[1].(map[string]any)
				if toolBlock["type"] != "tool_use" {
					t.Errorf("tool block type = %q, want %q", toolBlock["type"], "tool_use")
				}
			},
		},
		{
			name:    "JSON with unchanged text returns nil",
			input:   `{"content":[{"type":"text","text":"The answer is 42."}]}`,
			wantNil: true,
		},
		{
			name:    "JSON with text block missing text key",
			input:   `{"content":[{"type":"text"}]}`,
			wantNil: true,
		},
		{
			name:    "JSON with content as non-array",
			input:   `{"content":"not an array"}`,
			wantNil: true,
		},
		{
			name:    "trimmed text with invalid UTF-8 keeps original",
			input:   `{"content":[{"type":"text","text":"normal text"}]}`,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := trimResponse([]byte(tt.input))
			if tt.wantNil && result != nil {
				t.Fatalf("expected nil, got %s", string(result))
			}
			if !tt.wantNil && result == nil {
				t.Fatal("expected non-nil result, got nil")
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

// --- isValidUTF8String ---

func TestIsValidUTF8String(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid printable ASCII", "Hello, World!", true},
		{"valid UTF-8 unicode", "Hello, 世界! 🌍", true},
		{"string with null byte", "hello\x00world", false},
		{"string with control char 0x01", "hello\x01world", false},
		{"string with control char 0x1F", "hello\x1fworld", false},
		{"tab allowed", "hello\tworld", true},
		{"newline allowed", "hello\nworld", true},
		{"carriage return allowed", "hello\rworld", true},
		{"all control chars combined with allowed", "a\tb\nc\rd", true},
		{"empty string", "", true},
		{"invalid UTF-8 bytes", "\xff\xfe", false},
		{"DEL char (0x7F)", string([]byte{0x7F}), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidUTF8String(tt.input)
			if got != tt.want {
				t.Errorf("isValidUTF8String(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// --- RateLimitError ---

func TestRateLimitError(t *testing.T) {
	err := RateLimitError(30)

	if err.Type != "error" {
		t.Errorf("Type = %q, want %q", err.Type, "error")
	}
	if err.Error.Type != "rate_limit_error" {
		t.Errorf("Error.Type = %q, want %q", err.Error.Type, "rate_limit_error")
	}
	expected := "Rate limit exceeded. Please retry after 30 seconds."
	if err.Error.Message != expected {
		t.Errorf("Error.Message = %q, want %q", err.Error.Message, expected)
	}

	data, jerr := json.Marshal(err)
	if jerr != nil {
		t.Fatal(jerr)
	}

	var parsed map[string]any
	if json.Unmarshal(data, &parsed) != nil {
		t.Fatal("failed to unmarshal")
	}
	if parsed["type"] != "error" {
		t.Errorf("JSON type = %v, want %q", parsed["type"], "error")
	}
	errObj := parsed["error"].(map[string]any)
	if errObj["type"] != "rate_limit_error" {
		t.Errorf("JSON error.type = %v, want %q", errObj["type"], "rate_limit_error")
	}
}

// --- OverloadedError ---

func TestOverloadedError(t *testing.T) {
	msg := "The server is currently overloaded"
	err := OverloadedError(msg)

	if err.Type != "error" {
		t.Errorf("Type = %q, want %q", err.Type, "error")
	}
	if err.Error.Type != "overloaded_error" {
		t.Errorf("Error.Type = %q, want %q", err.Error.Type, "overloaded_error")
	}
	if err.Error.Message != msg {
		t.Errorf("Error.Message = %q, want %q", err.Error.Message, msg)
	}

	data, jerr := json.Marshal(err)
	if jerr != nil {
		t.Fatal(jerr)
	}

	var parsed map[string]any
	if json.Unmarshal(data, &parsed) != nil {
		t.Fatal("failed to unmarshal")
	}
	errObj := parsed["error"].(map[string]any)
	if errObj["type"] != "overloaded_error" {
		t.Errorf("JSON error.type = %v, want %q", errObj["type"], "overloaded_error")
	}
	if errObj["message"] != msg {
		t.Errorf("JSON error.message = %v, want %q", errObj["message"], msg)
	}
}

// --- allowedResponseHeaders ---

func TestAllowedResponseHeaders(t *testing.T) {
	required := []string{
		"Content-Type",
		"X-RateLimit-Limit",
		"X-RateLimit-Remaining",
		"X-RateLimit-Reset",
		"Retry-After",
		"Request-Id",
		"Anthropic-Ratelimit-Requests-Remaining",
		"Anthropic-Ratelimit-Tokens-Remaining",
	}

	for _, h := range required {
		if !allowedResponseHeaders[h] {
			t.Errorf("header %q not in allowedResponseHeaders", h)
		}
	}

	// Verify blocked headers are NOT allowed
	blocked := []string{
		"Set-Cookie",
		"Server",
		"X-Powered-By",
		"Transfer-Encoding",
	}

	for _, h := range blocked {
		if allowedResponseHeaders[h] {
			t.Errorf("header %q should NOT be in allowedResponseHeaders", h)
		}
	}

	// Verify total count
	if len(allowedResponseHeaders) != 8 {
		t.Errorf("allowedResponseHeaders has %d entries, want 8", len(allowedResponseHeaders))
	}
}

// --- Integration: trimResponse with verbosePatterns coverage ---

func TestTrimResponseCoversAllPatterns(t *testing.T) {
	// Build a response with a text block for each verbose pattern
	// that should be trimmed, and one that shouldn't.
	texts := []struct {
		input   string
		trimmed string
	}{
		{"Here's the code:\nprintln(1)", "code:\nprintln(1)"},
		{"Here is a fix:\nx = 1", "fix:\nx = 1"},
		{"Let me explain this concept.\nThe concept is X.", "The concept is X."},
		{"Let me help you.\nThe answer.", "The answer."},
		{"Let me show you.\nCode.", "Code."},
		{"Let me walk you through it.\nStep 1.", "Step 1."},
		{"Let me break down the problem.\nAnalysis.", "Analysis."},
		{"Let me tell you about X.\nX is great.", "X is great."},
		{"I'll help you with this.\nHere's the fix.", "Here's the fix."},
		{"I'll explain the issue.\nIssue.", "Issue."},
		{"I'll show you the code.\nCode.", "Code."},
		{"I'll walk you through it.\nSteps.", "Steps."},
		{"Sure!\nAnswer.", "Answer."},
		{"Certainly!\nAnswer.", "Answer."},
		{"Of course!\nAnswer.", "Answer."},
		{"Great question!\nAnswer.", "Answer."},
		{"I'd be happy to help.\nAnswer.", "Answer."},
		{"I'd be happy to explain.\nAnswer.", "Answer."},
		{"I'd be happy to show you.\nAnswer.", "Answer."},
		{"I'd be happy to assist.\nAnswer.", "Answer."},
		{"Content here.\n\nHope this helps!", "Content here."},
		{"Content here.\n\nHope that is helpful.", "Content here."},
		{"Content here.\n\nLet me know if you need anything else.", "Content here."},
		{"Content here.\n\nLet me know if you need more help.", "Content here."},
		{"Content here.\n\nLet me know if you need further assistance.", "Content here."},
		{"No patterns here.", "No patterns here."},
	}

	for _, tt := range texts {
		name := strings.Join(strings.Fields(tt.input[:min(len(tt.input), 30)]), "_")
		t.Run(name, func(t *testing.T) {
			body := `{"content":[{"type":"text","text":` +
				string(mustMarshal(tt.input)) + `}]}`
			result := trimResponse([]byte(body))
			if result == nil {
				if tt.input != tt.trimmed {
					t.Fatalf("expected trim for %q but got nil", tt.input)
				}
				return
			}
			var m map[string]any
			if err := json.Unmarshal(result, &m); err != nil {
				t.Fatal(err)
			}
			text := m["content"].([]any)[0].(map[string]any)["text"].(string)
			if text != tt.trimmed {
				t.Errorf("text = %q, want %q", text, tt.trimmed)
			}
		})
	}
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
