package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// C4: Verify extractPrompt strips leftover [[TYPE_N]] placeholders.
func TestExtractPrompt_StripsLeftoverPlaceholders(t *testing.T) {
	payload := map[string]any{
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "Check [[EMAIL_ADDRESS_1]] and [[IP_ADDRESS_3]]",
			},
		},
	}

	result := extractPrompt(payload)
	// Placeholders should be stripped, not passed to claude.ai
	assert.NotContains(t, result, "[[EMAIL_ADDRESS_1]]")
	assert.NotContains(t, result, "[[IP_ADDRESS_3]]")
	// Role label should still be present
	assert.Contains(t, result, "user:")
}

func TestExtractPrompt_ContentBlocks(t *testing.T) {
	payload := map[string]any{
		"system": "You are helpful.",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Hello [[API_KEY_SK_1]]",
					},
				},
			},
		},
	}

	result := extractPrompt(payload)
	assert.NotContains(t, result, "[[API_KEY_SK_1]]")
	assert.Contains(t, result, "Hello")
}

func TestExtractPrompt_SystemPromptArray(t *testing.T) {
	payload := map[string]any{
		"system": []any{
			map[string]any{"text": "System [[PERSON_1]] info"},
			map[string]any{"text": "More instructions"},
		},
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "test",
			},
		},
	}

	result := extractPrompt(payload)
	assert.NotContains(t, result, "[[PERSON_1]]")
	assert.Contains(t, result, "System")
	assert.Contains(t, result, "More instructions")
}

func TestExtractPrompt_MultiTurn(t *testing.T) {
	payload := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "first question"},
			map[string]any{"role": "assistant", "content": "first answer"},
			map[string]any{"role": "user", "content": "second question [[PHONE_NUMBER_1]]"},
			map[string]any{"role": "assistant", "content": "second answer"},
			map[string]any{"role": "user", "content": "final question"},
		},
	}

	result := extractPrompt(payload)
	assert.NotContains(t, result, "[[PHONE_NUMBER_1]]")
	// Should include context from last few messages
	assert.Contains(t, result, "final question")
}
