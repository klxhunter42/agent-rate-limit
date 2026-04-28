# Lotus LLM Direct Connection - Integration Notes

> Setup, issues encountered, and gateway integration plan for Claude Code + Lotus LLM endpoint

---

## Overview

Lotus LLM endpoint (`api-cpxis.lotuss.com/llm`) provides an internal LLM service that supports both OpenAI and Anthropic API formats. This doc covers using it directly with Claude Code and the gaps to fix when integrating into our gateway.

---

## Endpoint Details

| Item | Value |
|---|---|
| Base URL | `https://api-cpxis.lotuss.com/llm` |
| Anthropic format | `/v1/messages` |
| OpenAI format | `/v1/chat/completions` |
| Auth | `Authorization: Bearer devops.lotuss.Db90t8pjZE2kdn0uI8os9jldmLoH9s` |
| Model name | `default` (NOT `glm-4.5` or `glm-5.1` - returns 404) |
| Context limit | ~40K tokens total (system prompt ~26K, output ~14K max) |
| Streaming | Supported (SSE) |

---

## Claude Code Setup (Remote 10.11.11.88)

### settings.json

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://api-cpxis.lotuss.com/llm",
    "ANTHROPIC_API_KEY": "devops.lotuss.Db90t8pjZE2kdn0uI8os9jldmLoH9s",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "default",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "default",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "default",
    "CLAUDE_CODE_MAX_OUTPUT_TOKENS": "14000",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
    "CLAUDE_CODE_DISABLE_ANALYTICS": "1"
  },
  "apiKeyHelper": "echo $ANTHROPIC_API_KEY"
}
```

### Key settings explained

| Key | Why |
|---|---|
| `ANTHROPIC_DEFAULT_*_MODEL: "default"` | Claude Code sends model names like `claude-sonnet-4-6`. Lotus only accepts `default`. These env vars override the model name Claude Code sends. |
| `CLAUDE_CODE_MAX_OUTPUT_TOKENS: "14000"` | Lotus context ~40K. System prompt ~26K. 40K - 26K = 14K for output. Higher values cause "max_tokens too large" error. |
| `apiKeyHelper` | Required for interactive mode. Without it, Claude Code shows "Not logged in" even when `ANTHROPIC_API_KEY` is set in env. |

### Verify

```bash
# Pipe mode test
echo "say ok" | claude -p

# Direct curl test
curl -s -X POST https://api-cpxis.lotuss.com/llm/v1/messages \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer devops.lotuss.Db90t8pjZE2kdn0uI8os9jldmLoH9s" \
  -H 'anthropic-version: 2023-06-01' \
  -d '{"model":"default","max_tokens":64,"messages":[{"role":"user","content":"say hi"}]}'
```

---

## Issues Encountered

### 1. Model name 404

**Symptom**: `"The model glm-4.5 does not exist"`
**Cause**: Lotus model name is `default`, not `glm-*`
**Fix**: Set `ANTHROPIC_DEFAULT_*_MODEL=default` to override whatever model Claude Code tries to send

### 2. max_tokens too large

**Symptom**: Error about max_tokens exceeding context
**Cause**: Lotus context ~40K total. Claude Code sends max_tokens=16000+ by default. Combined with ~26K system prompt, exceeds 40K.
**Fix**: `CLAUDE_CODE_MAX_OUTPUT_TOKENS=14000` (40000 - 26000 buffer)

### 3. VSCode Claude panel crash: "Cannot set properties of undefined (setting 'output_tokens')"

**Symptom**: Error in VSCode Claude Code extension panel during streaming
**Root cause**: Lotus SSE `message_start` event missing `usage` field in `message` object

Lotus returns:
```json
event: message_start
data: {"type":"message_start","message":{"id":"chatcmpl-xxx","content":[],"model":"default"}}
```

Claude Code expects:
```json
event: message_start
data: {"type":"message_start","message":{"id":"msg_xxx","type":"message","role":"assistant","content":[],"model":"...","stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}}
```

The extension does `message.usage.output_tokens` on `undefined` -> crash.

**Current status**: Only affects VSCode panel (streaming). Pipe mode (`claude -p`) works fine because non-streaming response includes `usage`.

**Fix for gateway integration**: When proxying to Lotus, gateway must inject missing fields into `message_start` SSE event before relaying to client.

### 4. "Not logged in" in interactive mode

**Symptom**: `claude` (no flags) shows "Not logged in"
**Cause**: Claude Code interactive mode requires `apiKeyHelper` to resolve the API key
**Fix**: Add `"apiKeyHelper": "echo $ANTHROPIC_API_KEY"` to settings.json

---

## Non-Streaming Response Format (Working)

```json
{
  "id": "chatcmpl-xxx",
  "type": "message",
  "role": "assistant",
  "content": [{"type": "text", "text": "response"}],
  "model": "default",
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 7, "output_tokens": 43}
}
```

## Streaming Response Format (Needs Patching)

```
event: message_start
data: {"type":"message_start","message":{"id":"...","content":[],"model":"default"}}
                                                      ^^^ MISSING: role, type, stop_reason, usage

event: content_block_start
data: {"type":"content_block_start","content_block":{"type":"text","text":""},"index":0}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"..."},"index":0}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":7,"output_tokens":64}}

event: message_stop
data: {"type":"message_stop"}

data: [DONE]
```

---

## Gateway Integration Plan

When integrating Lotus as a provider in the gateway, handle these in the proxy layer:

### SSE Event Patching

In the streaming proxy path, intercept `message_start` and inject:

```go
// In streaming response handler for Lotus provider
func patchLotusSSE(eventType string, data []byte) []byte {
    if eventType == "message_start" {
        var msg struct {
            Type    string          `json:"type"`
            Message json.RawMessage `json:"message"`
        }
        json.Unmarshal(data, &msg)

        // Check if message has usage field
        if !bytes.Contains(msg.Message, []byte(`"usage"`)) {
            // Inject missing fields
            patched := strings.Replace(
                string(msg.Message),
                `"content":[]`,
                `"type":"message","role":"assistant","content":[],"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}`,
                1,
            )
            msg.Message = json.RawMessage(patched)
        }

        result, _ := json.Marshal(msg)
        return result
    }
    return data
}
```

### Model Name Mapping

```go
// In provider resolver for Lotus
case "lotus":
    // All claude-* model names -> "default"
    payload["model"] = "default"
```

### max_tokens Clamp

```go
// Context limit ~40K, system prompt ~26K
const lotusMaxTokens = 14000
if mt, ok := payload["max_tokens"].(float64); ok && mt > lotusMaxTokens {
    payload["max_tokens"] = lotusMaxTokens
}
```

### Provider Config (Redis)

```json
{
  "name": "lotus",
  "type": "anthropic-compat",
  "baseUrl": "https://api-cpxis.lotuss.com/llm",
  "apiKey": "devops.lotuss.Db90t8pjZE2kdn0uI8os9jldmLoH9s",
  "models": ["default"],
  "maxTokens": 14000,
  "streamPatch": true
}
```

---

## Files

| File | Purpose |
|---|---|
| `scripts/lotus-sse-patch-proxy.py` | Standalone SSE-patching proxy (for testing, not deployed to 88) |
| `scripts/anthropic-openai-proxy.py` | Anthropic-to-OpenAI translator (unused, Lotus now supports Anthropic format) |

---

## Timeline

| Date | Event |
|---|---|
| 2026-04-28 | Initial setup on 10.11.11.88, direct connection working (pipe mode) |
| 2026-04-28 | Discovered VSCode panel SSE bug, created SSE-patch proxy script |
| 2026-04-28 | Decided to integrate SSE patching into gateway instead of standalone proxy |
