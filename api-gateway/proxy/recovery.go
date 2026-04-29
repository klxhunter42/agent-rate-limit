package proxy

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"

	"github.com/klxhunter/agent-rate-limit/api-gateway/tokenizer"
)

type RecoveryAction int

const (
	ActionForward RecoveryAction = iota
	ActionRetryTransient
	ActionTruncateAndRetry
)

var transientStatusCodes = map[int]bool{
	500: true,
	502: true,
	503: true,
	529: true,
}

var contextWindowPatterns = []string{
	"prompt is too long",
	"prompt exceeds maximum length",
	"context_length_exceeded",
	"maximum context length",
	"token count exceeds",
	"context window",
	"context limit exceeded",
	"too many tokens",
	"reduced context window",
}

func ClassifyError(statusCode int, body []byte) RecoveryAction {
	if transientStatusCodes[statusCode] {
		return ActionRetryTransient
	}
	if statusCode == 413 {
		return ActionTruncateAndRetry
	}
	if statusCode == 400 || statusCode == 422 {
		lower := strings.ToLower(string(body))
		for _, p := range contextWindowPatterns {
			if strings.Contains(lower, p) {
				return ActionTruncateAndRetry
			}
		}
	}
	return ActionForward
}

type TruncationResult struct {
	Body        []byte
	DroppedMsgs int
	OrigTokens  int
	NewTokens   int
}

const (
	minMessagesForTruncation = 4
	truncationTargetRatio    = 0.75
)

func TruncateMessages(body []byte, model string) *TruncationResult {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	msgsRaw, ok := payload["messages"].([]any)
	if !ok || len(msgsRaw) < minMessagesForTruncation {
		return nil
	}

	cap := tokenizer.GetModelCapabilities(model)
	targetTokens := int(float64(cap.ContextWindow-cap.MaxOutputTokens) * truncationTargetRatio)

	systemTokens := estimateSystemTokens(payload["system"])

	type msgInfo struct {
		tokens int
	}
	msgInfos := make([]msgInfo, len(msgsRaw))
	for i, m := range msgsRaw {
		msgInfos[i].tokens = EstimateMessageTokens(m)
	}

	origTotal := systemTokens
	for _, mi := range msgInfos {
		origTotal += mi.tokens
	}
	if origTotal <= targetTokens {
		return nil
	}

	budget := targetTokens - systemTokens
	keepCount := 0
	for i := len(msgInfos) - 1; i >= 0; i-- {
		if budget >= msgInfos[i].tokens {
			budget -= msgInfos[i].tokens
			keepCount++
		} else {
			break
		}
	}
	if keepCount < 2 {
		keepCount = 2
	}

	keepCount = fixToolPairBoundary(msgsRaw, keepCount)

	dropped := len(msgsRaw) - keepCount
	if dropped <= 0 {
		return nil
	}

	keptMsgs := msgsRaw[len(msgsRaw)-keepCount:]
	payload["messages"] = keptMsgs

	note := "\n\n[Note: older conversation messages were truncated to fit context window limits.]\n"
	if sys, ok := payload["system"]; ok {
		switch v := sys.(type) {
		case string:
			payload["system"] = v + note
		case []any:
			if len(v) > 0 {
				if last, ok := v[len(v)-1].(map[string]any); ok {
					if t, ok := last["text"].(string); ok {
						last["text"] = t + note
					}
				}
			}
		}
	}

	newBody, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	newTotal := systemTokens + tokenizer.QuickEstimateTokens(note)
	for i := len(msgInfos) - keepCount; i < len(msgInfos); i++ {
		newTotal += msgInfos[i].tokens
	}

	return &TruncationResult{
		Body:        newBody,
		DroppedMsgs: dropped,
		OrigTokens:  origTotal,
		NewTokens:   newTotal,
	}
}

func fixToolPairBoundary(msgs []any, keepCount int) int {
	if keepCount <= 0 || keepCount >= len(msgs) {
		return keepCount
	}
	firstIdx := len(msgs) - keepCount
	first, ok := msgs[firstIdx].(map[string]any)
	if !ok {
		return keepCount
	}
	role, _ := first["role"].(string)
	content := first["content"]

	if role == "user" && hasToolResult(content) && firstIdx > 0 {
		keepCount++
	} else if role == "assistant" && hasToolUse(content) && firstIdx > 0 {
		keepCount++
	}
	return keepCount
}

func hasToolUse(content any) bool {
	blocks, ok := content.([]any)
	if !ok {
		return false
	}
	for _, b := range blocks {
		if m, ok := b.(map[string]any); ok {
			if t, _ := m["type"].(string); t == "tool_use" {
				return true
			}
		}
	}
	return false
}

func hasToolResult(content any) bool {
	blocks, ok := content.([]any)
	if !ok {
		return false
	}
	for _, b := range blocks {
		if m, ok := b.(map[string]any); ok {
			if t, _ := m["type"].(string); t == "tool_result" {
				return true
			}
		}
	}
	return false
}

func estimateSystemTokens(sys any) int {
	if sys == nil {
		return 0
	}
	switch v := sys.(type) {
	case string:
		return tokenizer.QuickEstimateTokens(v)
	case []any:
		total := 0
		for _, s := range v {
			if sm, ok := s.(map[string]any); ok {
				if t, ok := sm["text"].(string); ok {
					total += tokenizer.QuickEstimateTokens(t)
				}
			}
		}
		return total
	}
	return tokenizer.QuickEstimateTokens(jsonStr(sys))
}

func EstimateMessageTokens(msg any) int {
	m, ok := msg.(map[string]any)
	if !ok {
		return 0
	}
	content := m["content"]
	switch v := content.(type) {
	case string:
		return tokenizer.QuickEstimateTokens(v)
	case []any:
		total := 0
		for _, block := range v {
			cb, ok := block.(map[string]any)
			if !ok {
				continue
			}
			switch t, _ := cb["type"].(string); t {
			case "text":
				if text, ok := cb["text"].(string); ok {
					total += tokenizer.QuickEstimateTokens(text)
				}
			case "image":
				total += 1000
			case "tool_use":
				if inp, ok := cb["input"]; ok {
					total += tokenizer.QuickEstimateTokens(jsonStr(inp))
				}
			case "tool_result":
				if c := cb["content"]; c != nil {
					switch cv := c.(type) {
					case string:
						total += tokenizer.QuickEstimateTokens(cv)
					default:
						total += tokenizer.QuickEstimateTokens(jsonStr(cv))
					}
				}
			}
		}
		return total
	default:
		return tokenizer.QuickEstimateTokens(jsonStr(content))
	}
}

func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func init() {
	slog.Debug("context recovery loaded",
		"transient_codes", mapKeys(transientStatusCodes),
		"min_messages", minMessagesForTruncation,
		"target_ratio", truncationTargetRatio,
	)
}

func mapKeys(m map[int]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, strconv.Itoa(k))
	}
	return "[" + strings.Join(keys, ",") + "]"
}
