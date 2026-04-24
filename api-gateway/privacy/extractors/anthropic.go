package extractors

import (
	"fmt"

	"github.com/klxhunter/agent-rate-limit/api-gateway/privacy/masking"
)

// ExtractTextSpans extracts all text content from an Anthropic-format request payload.
func ExtractTextSpans(payload map[string]any) []masking.TextSpan {
	var spans []masking.TextSpan

	// System prompt (can be string or content block array).
	if sys, ok := payload["system"]; ok {
		spans = extractFromValue(sys, -1, "system", spans)
	}

	// Messages.
	msgs, _ := payload["messages"].([]any)
	for i, m := range msgs {
		msg, _ := m.(map[string]any)
		role, _ := msg["role"].(string)

		content, ok := msg["content"]
		if !ok {
			continue
		}

		switch v := content.(type) {
		case string:
			spans = append(spans, masking.TextSpan{
				Text: v, Path: fmt.Sprintf("messages[%d].content", i),
				MessageIndex: i, PartIndex: 0, NestedIndex: -1, Role: role,
			})
		case []any:
			for j, block := range v {
				b, _ := block.(map[string]any)
				blockType, _ := b["type"].(string)
				path := fmt.Sprintf("messages[%d].content[%d]", i, j)

				switch blockType {
				case "text":
					text, _ := b["text"].(string)
					spans = append(spans, masking.TextSpan{
						Text: text, Path: path + ".text",
						MessageIndex: i, PartIndex: j, NestedIndex: -1, Role: role,
					})
				case "tool_result":
					cr := b["content"]
					switch cv := cr.(type) {
					case string:
						spans = append(spans, masking.TextSpan{
							Text: cv, Path: path + ".content",
							MessageIndex: i, PartIndex: j, NestedIndex: -1, Role: "tool",
						})
					case []any:
						for k, nested := range cv {
							nb, _ := nested.(map[string]any)
							if nb["type"] == "text" {
								text, _ := nb["text"].(string)
								spans = append(spans, masking.TextSpan{
									Text: text, Path: fmt.Sprintf("messages[%d].content[%d].content[%d].text", i, j, k),
									MessageIndex: i, PartIndex: j, NestedIndex: k, Role: "tool",
								})
							}
						}
					}
				case "tool_use":
					input, _ := b["input"].(map[string]any)
					if len(input) > 0 {
						extractInputStrings(input, path+".input", i, j, role, &spans)
					}
				}
			}
		}
	}

	return spans
}

// ApplyMaskedSpans rebuilds the payload with masked text substituted back.
func ApplyMaskedSpans(payload map[string]any, maskedSpans []masking.MaskedSpan) {
	lookup := make(map[string]string)
	for _, ms := range maskedSpans {
		if ms.MessageIndex < 0 {
			lookup["sys:"+ms.Path] = ms.MaskedText
		} else {
			key := fmt.Sprintf("%d:%d:%d", ms.MessageIndex, ms.PartIndex, ms.NestedIndex)
			lookup[key] = ms.MaskedText
		}
	}

	// Apply to system.
	if sys, ok := payload["system"]; ok {
		payload["system"] = applyToValue(sys, lookup, -1)
	}

	// Apply to messages.
	msgs, _ := payload["messages"].([]any)
	for i, m := range msgs {
		msg, _ := m.(map[string]any)
		content, ok := msg["content"]
		if !ok {
			continue
		}
		msg["content"] = applyToValue(content, lookup, i)
		msgs[i] = msg
	}
}

func extractFromValue(v any, msgIdx int, base string, spans []masking.TextSpan) []masking.TextSpan {
	switch val := v.(type) {
	case string:
		return append(spans, masking.TextSpan{
			Text: val, Path: base,
			MessageIndex: msgIdx, PartIndex: 0, NestedIndex: -1, Role: "system",
		})
	case []any:
		for j, item := range val {
			b, _ := item.(map[string]any)
			if b["type"] == "text" {
				text, _ := b["text"].(string)
				path := fmt.Sprintf("%s[%d].text", base, j)
				spans = append(spans, masking.TextSpan{
					Text: text, Path: path,
					MessageIndex: msgIdx, PartIndex: j, NestedIndex: -1, Role: "system",
				})
			}
		}
	}
	return spans
}

func applyToValue(v any, lookup map[string]string, msgIdx int) any {
	switch val := v.(type) {
	case string:
		if msgIdx < 0 {
			if masked, ok := lookup["sys:system"]; ok {
				return masked
			}
		} else {
			key := fmt.Sprintf("%d:0:-1", msgIdx)
			if masked, ok := lookup[key]; ok {
				return masked
			}
		}
		return val
	case []any:
		for j, item := range val {
			b, _ := item.(map[string]any)
			blockType, _ := b["type"].(string)

			switch blockType {
			case "text":
				key := fmt.Sprintf("%d:%d:-1", msgIdx, j)
				if masked, ok := lookup[key]; ok {
					b["text"] = masked
				}
			case "tool_result":
				cr := b["content"]
				switch cv := cr.(type) {
				case string:
					key := fmt.Sprintf("%d:%d:-1", msgIdx, j)
					if masked, ok := lookup[key]; ok {
						b["content"] = masked
					}
				case []any:
					for k, nested := range cv {
						nb, _ := nested.(map[string]any)
						if nb["type"] == "text" {
							key := fmt.Sprintf("%d:%d:%d", msgIdx, j, k)
							if masked, ok := lookup[key]; ok {
								nb["text"] = masked
							}
						}
					}
				}
			}
			val[j] = b
		}
		return val
	}
	return v
}

// extractInputStrings recursively extracts leaf string values from a tool_use input object.
func extractInputStrings(obj map[string]any, basePath string, msgIdx, partIdx int, role string, spans *[]masking.TextSpan) {
	for key, val := range obj {
		switch v := val.(type) {
		case string:
			*spans = append(*spans, masking.TextSpan{
				Text: v, Path: basePath + "." + key,
				MessageIndex: msgIdx, PartIndex: partIdx, NestedIndex: -2, Role: role,
			})
		case map[string]any:
			extractInputStrings(v, basePath+"."+key, msgIdx, partIdx, role, spans)
		case []any:
			for k, elem := range v {
				if s, ok := elem.(string); ok {
					*spans = append(*spans, masking.TextSpan{
						Text: s, Path: fmt.Sprintf("%s.%s[%d]", basePath, key, k),
						MessageIndex: msgIdx, PartIndex: partIdx, NestedIndex: -2, Role: role,
					})
				} else if m, ok := elem.(map[string]any); ok {
					extractInputStrings(m, fmt.Sprintf("%s.%s[%d]", basePath, key, k), msgIdx, partIdx, role, spans)
				}
			}
		}
	}
}
