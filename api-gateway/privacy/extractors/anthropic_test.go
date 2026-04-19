package extractors

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
)

func TestExtractTextSpans_SystemString(t *testing.T) {
	payload := map[string]any{
		"system": "You are helpful.",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "Hello",
			},
		},
	}
	spans := ExtractTextSpans(payload)
	assert.Len(t, spans, 2)

	assert.Equal(t, "You are helpful.", spans[0].Text)
	assert.Equal(t, "system", spans[0].Role)
	assert.Equal(t, -1, spans[0].MessageIndex)

	assert.Equal(t, "Hello", spans[1].Text)
	assert.Equal(t, "user", spans[1].Role)
	assert.Equal(t, 0, spans[1].MessageIndex)
}

func TestExtractTextSpans_SystemArray(t *testing.T) {
	payload := map[string]any{
		"system": []any{
			map[string]any{"type": "text", "text": "Be helpful"},
			map[string]any{"type": "text", "text": "Be concise"},
		},
		"messages": []any{},
	}
	spans := ExtractTextSpans(payload)
	assert.Len(t, spans, 2)
	assert.Equal(t, "Be helpful", spans[0].Text)
	assert.Equal(t, "Be concise", spans[1].Text)
}

func TestExtractTextSpans_ContentBlocks(t *testing.T) {
	payload := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "Hello"},
					map[string]any{"type": "image", "source": "data:..."},
				},
			},
		},
	}
	spans := ExtractTextSpans(payload)
	assert.Len(t, spans, 1)
	assert.Equal(t, "Hello", spans[0].Text)
	assert.Equal(t, "user", spans[0].Role)
	assert.Equal(t, 0, spans[0].PartIndex)
}

func TestExtractTextSpans_ToolResultString(t *testing.T) {
	payload := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":    "tool_result",
						"content": "result data",
					},
				},
			},
		},
	}
	spans := ExtractTextSpans(payload)
	assert.Len(t, spans, 1)
	assert.Equal(t, "result data", spans[0].Text)
	assert.Equal(t, "tool", spans[0].Role)
}

func TestExtractTextSpans_ToolResultNested(t *testing.T) {
	payload := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "tool_result",
						"content": []any{
							map[string]any{"type": "text", "text": "nested text"},
						},
					},
				},
			},
		},
	}
	spans := ExtractTextSpans(payload)
	assert.Len(t, spans, 1)
	assert.Equal(t, "nested text", spans[0].Text)
	assert.Equal(t, 0, spans[0].NestedIndex)
}

func TestExtractTextSpans_NoMessages(t *testing.T) {
	spans := ExtractTextSpans(map[string]any{})
	assert.Len(t, spans, 0)
}

func TestExtractTextSpans_NoContent(t *testing.T) {
	payload := map[string]any{
		"messages": []any{
			map[string]any{"role": "user"},
		},
	}
	spans := ExtractTextSpans(payload)
	assert.Len(t, spans, 0)
}

func TestApplyMaskedSpans(t *testing.T) {
	payload := map[string]any{
		"system": "secret-key-here",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "my key is secret123",
			},
		},
	}

	masked := []masking.MaskedSpan{
		{Path: "system", MaskedText: "[[KEY_1]]", MessageIndex: -1, PartIndex: 0, NestedIndex: -1},
		{Path: "messages[0].content", MaskedText: "my key is [[KEY_2]]", MessageIndex: 0, PartIndex: 0, NestedIndex: -1},
	}

	ApplyMaskedSpans(payload, masked)
	assert.Equal(t, "[[KEY_1]]", payload["system"])

	msgs := payload["messages"].([]any)
	msg := msgs[0].(map[string]any)
	assert.Equal(t, "my key is [[KEY_2]]", msg["content"])
}
