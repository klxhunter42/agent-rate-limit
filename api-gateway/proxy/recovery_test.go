package proxy

import (
	"encoding/json"
	"testing"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   RecoveryAction
	}{
		{"anthropic prompt too long", 400, `{"error":{"type":"invalid_request_error","message":"prompt is too long: 250000 tokens"}}`, ActionTruncateAndRetry},
		{"openai context_length_exceeded", 400, `{"error":{"message":"context_length_exceeded","type":"invalid_request_error"}}`, ActionTruncateAndRetry},
		{"openai maximum context length", 400, `{"error":{"message":"This model's maximum context length is 128000 tokens"}}`, ActionTruncateAndRetry},
		{"gemini token count exceeds", 400, `{"error":{"message":"Request token count exceeds the maximum number of tokens"}}`, ActionTruncateAndRetry},
		{"gemini 413 payload too large", 413, `{"error":"too large"}`, ActionTruncateAndRetry},
		{"500 server error", 500, `{"error":"internal"}`, ActionRetryTransient},
		{"502 bad gateway", 502, `bad gateway`, ActionRetryTransient},
		{"503 service unavailable", 503, `unavailable`, ActionRetryTransient},
		{"529 overloaded", 529, `overloaded`, ActionRetryTransient},
		{"400 other error", 400, `{"error":{"message":"invalid model"}}`, ActionForward},
		{"403 forbidden", 403, `{"error":"forbidden"}`, ActionForward},
		{"401 unauthorized", 401, `{"error":"unauthorized"}`, ActionForward},
		{"400 empty body", 400, `{}`, ActionForward},
		{"context window limit 400", 400, `{"error":{"message":"The model has reached its context window limit"}}`, ActionTruncateAndRetry},
		{"context window limit 422", 422, `{"error":{"type":"invalid_request_error","message":"The model has reached its context window limit"}}`, ActionTruncateAndRetry},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyError(tt.status, []byte(tt.body))
			if got != tt.want {
				t.Errorf("ClassifyError(%d, %q) = %v, want %v", tt.status, tt.body, got, tt.want)
			}
		})
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	t.Run("string content", func(t *testing.T) {
		msg := map[string]any{"role": "user", "content": "hello world"}
		got := EstimateMessageTokens(msg)
		if got <= 0 {
			t.Errorf("expected positive token estimate, got %d", got)
		}
	})

	t.Run("array content with text blocks", func(t *testing.T) {
		msg := map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "some text here"},
			},
		}
		got := EstimateMessageTokens(msg)
		if got <= 0 {
			t.Errorf("expected positive token estimate, got %d", got)
		}
	})

	t.Run("array content with image block", func(t *testing.T) {
		msg := map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "image", "source": map[string]any{"type": "base64", "data": "abc"}},
			},
		}
		got := EstimateMessageTokens(msg)
		if got != 1000 {
			t.Errorf("image block should estimate ~1000 tokens, got %d", got)
		}
	})

	t.Run("array content with tool_use", func(t *testing.T) {
		msg := map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "tool_use", "id": "t1", "name": "read_file", "input": map[string]any{"path": "/tmp/test.go"}},
			},
		}
		got := EstimateMessageTokens(msg)
		if got <= 0 {
			t.Errorf("expected positive token estimate for tool_use, got %d", got)
		}
	})

	t.Run("tool_result with string content", func(t *testing.T) {
		msg := map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "file contents here"},
			},
		}
		got := EstimateMessageTokens(msg)
		if got <= 0 {
			t.Errorf("expected positive token estimate for tool_result, got %d", got)
		}
	})

	t.Run("nil content", func(t *testing.T) {
		msg := map[string]any{"role": "user", "content": nil}
		got := EstimateMessageTokens(msg)
		if got <= 0 {
			t.Errorf("expected positive token estimate for nil content, got %d", got)
		}
	})
}

func TestEstimateSystemTokens(t *testing.T) {
	t.Run("string system", func(t *testing.T) {
		got := estimateSystemTokens("You are a helpful assistant.")
		if got <= 0 {
			t.Errorf("expected positive estimate, got %d", got)
		}
	})

	t.Run("array system", func(t *testing.T) {
		sys := []any{
			map[string]any{"type": "text", "text": "System instructions"},
		}
		got := estimateSystemTokens(sys)
		if got <= 0 {
			t.Errorf("expected positive estimate, got %d", got)
		}
	})

	t.Run("nil system", func(t *testing.T) {
		got := estimateSystemTokens(nil)
		if got != 0 {
			t.Errorf("expected 0 for nil, got %d", got)
		}
	})
}

func makePayload(system string, msgCount, charsPerMsg int) []byte {
	msgs := make([]any, msgCount)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		text := make([]byte, charsPerMsg)
		for j := range text {
			text[j] = 'a'
		}
		msgs[i] = map[string]any{
			"role":    role,
			"content": string(text),
		}
	}
	payload := map[string]any{
		"model":      "claude-sonnet-4-6",
		"messages":   msgs,
		"max_tokens": 1024,
	}
	if system != "" {
		payload["system"] = system
	}
	b, _ := json.Marshal(payload)
	return b
}

func TestTruncateMessages(t *testing.T) {
	t.Run("normal truncation", func(t *testing.T) {
		body := makePayload("You are helpful.", 20, 20000)
		result := TruncateMessages(body, "claude-sonnet-4-6")
		if result == nil {
			t.Fatal("expected truncation result, got nil")
		}
		if result.DroppedMsgs <= 0 {
			t.Errorf("expected messages to be dropped, got %d", result.DroppedMsgs)
		}
		if result.NewTokens >= result.OrigTokens {
			t.Errorf("new tokens %d should be less than original %d", result.NewTokens, result.OrigTokens)
		}

		var parsed map[string]any
		json.Unmarshal(result.Body, &parsed)
		msgs, _ := parsed["messages"].([]any)
		if len(msgs) != 20-result.DroppedMsgs {
			t.Errorf("expected %d messages, got %d", 20-result.DroppedMsgs, len(msgs))
		}

		sys, _ := parsed["system"].(string)
		if sys == "" {
			t.Error("system prompt should still exist")
		}
	})

	t.Run("too few messages", func(t *testing.T) {
		body := makePayload("", 3, 50000)
		result := TruncateMessages(body, "claude-sonnet-4-6")
		if result != nil {
			t.Error("expected nil for too few messages")
		}
	})

	t.Run("already fits", func(t *testing.T) {
		body := makePayload("", 4, 100)
		result := TruncateMessages(body, "claude-sonnet-4-6")
		if result != nil {
			t.Error("expected nil when already fits")
		}
	})

	t.Run("system prompt preserved as array", func(t *testing.T) {
		msgs := make([]any, 10)
		for i := range msgs {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			text := make([]byte, 20000)
			for j := range text {
				text[j] = 'b'
			}
			msgs[i] = map[string]any{"role": role, "content": string(text)}
		}
		payload := map[string]any{
			"model":      "claude-sonnet-4-6",
			"messages":   msgs,
			"max_tokens": 1024,
			"system": []any{
				map[string]any{"type": "text", "text": "Instructions"},
			},
		}
		body, _ := json.Marshal(payload)
		result := TruncateMessages(body, "claude-sonnet-4-6")
		if result == nil {
			t.Fatal("expected truncation, got nil")
		}

		var parsed map[string]any
		json.Unmarshal(result.Body, &parsed)
		sys, ok := parsed["system"].([]any)
		if !ok || len(sys) == 0 {
			t.Fatal("system should still be array")
		}
		text, _ := sys[0].(map[string]any)["text"].(string)
		if text == "" {
			t.Error("system text should not be empty")
		}
	})

	t.Run("tool pair boundary preserved", func(t *testing.T) {
		msgs := make([]any, 10)
		for i := range msgs {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			text := make([]byte, 20000)
			for j := range text {
				text[j] = 'c'
			}
			msgs[i] = map[string]any{"role": role, "content": string(text)}
		}
		msgs[8] = map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "tool_use", "id": "t1", "name": "read", "input": map[string]any{}},
			},
		}
		msgs[9] = map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "result data"},
			},
		}
		payload := map[string]any{
			"model":      "claude-sonnet-4-6",
			"messages":   msgs,
			"max_tokens": 1024,
		}
		body, _ := json.Marshal(payload)
		result := TruncateMessages(body, "claude-sonnet-4-6")
		if result == nil {
			t.Fatal("expected truncation, got nil")
		}

		var parsed map[string]any
		json.Unmarshal(result.Body, &parsed)
		kept, _ := parsed["messages"].([]any)

		firstKept, _ := kept[0].(map[string]any)
		firstRole, _ := firstKept["role"].(string)
		if firstRole == "user" {
			firstContent, ok := firstKept["content"].([]any)
			if ok && len(firstContent) > 0 {
				if ct, _ := firstContent[0].(map[string]any)["type"].(string); ct == "tool_result" {
					if len(kept) < 2 {
						t.Error("tool_result should be paired with preceding tool_use")
					}
				}
			}
		}
	})

	t.Run("invalid JSON returns nil", func(t *testing.T) {
		result := TruncateMessages([]byte("not json"), "claude-sonnet-4-6")
		if result != nil {
			t.Error("expected nil for invalid JSON")
		}
	})

	t.Run("truncation note appended", func(t *testing.T) {
		body := makePayload("System instructions", 20, 20000)
		result := TruncateMessages(body, "claude-sonnet-4-6")
		if result == nil {
			t.Fatal("expected truncation, got nil")
		}
		var parsed map[string]any
		json.Unmarshal(result.Body, &parsed)
		sys, _ := parsed["system"].(string)
		if sys == "" {
			t.Fatal("system should exist")
		}
		if !contains(sys, "truncated to fit context window") {
			t.Errorf("system prompt should contain truncation note, got: %s", sys)
		}
	})
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestFixToolPairBoundary(t *testing.T) {
	t.Run("tool_result without preceding tool_use", func(t *testing.T) {
		msgs := []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "result"},
			}},
			map[string]any{"role": "assistant", "content": "response"},
		}
		got := fixToolPairBoundary(msgs, 2)
		if got != 2 {
			t.Errorf("expected 2 (no extra keep possible), got %d", got)
		}
	})

	t.Run("tool_result with preceding tool_use", func(t *testing.T) {
		msgs := []any{
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "tool_use", "id": "t1", "name": "read", "input": map[string]any{}},
			}},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "result"},
			}},
			map[string]any{"role": "assistant", "content": "response"},
		}
		got := fixToolPairBoundary(msgs, 2)
		if got != 3 {
			t.Errorf("expected 3 (include preceding tool_use), got %d", got)
		}
	})

	t.Run("keepCount >= len(msgs)", func(t *testing.T) {
		msgs := []any{map[string]any{"role": "user", "content": "hi"}}
		got := fixToolPairBoundary(msgs, 2)
		if got != 2 {
			t.Errorf("expected unchanged, got %d", got)
		}
	})
}
