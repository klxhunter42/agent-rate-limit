# Claude Code Usage

## Setup

Edit `~/.claude/settings.json`:

```json
{
  "ANTHROPIC_BASE_URL": "http://localhost:8080",
  "ANTHROPIC_AUTH_TOKEN": "your-glm-api-key"
}
```

> **ANTHROPIC_AUTH_TOKEN** is required because the gateway uses the `x-api-key` header for identity + rate limiting.
> Claude Code sends this value as the `x-api-key` header automatically.

## Architecture — Direct vs Gateway

**Direct (no gateway):**

```
Claude Code ──POST /v1/messages──▶ api.z.ai/api/anthropic
(ANTHROPIC_BASE_URL)
```

**Through Gateway:**

```
Claude Code ──POST /v1/messages──▶ Gateway :8080 ──transparent──▶ api.z.ai/api/anthropic
(ANTHROPIC_BASE_URL)
```

**User experience is identical** — Gateway is a transparent proxy:
- Does not decode/re-encode request/response
- Forwards every byte directly
- Does not touch any fields (tools, tool_choice, messages, content, headers)

## Claude Code Tool Loop

```
1. Claude Code sends request with tools definitions:
   POST /v1/messages
   {
     "model": "glm-5",
     "messages": [{"role": "user", "content": "Read main.go for me"}],
     "tools": [
       {"name": "Read", "description": "Read a file...", "input_schema": {...}},
       {"name": "Edit", "description": "Edit a file...", "input_schema": {...}},
       {"name": "Bash", "description": "Run a command...", "input_schema": {...}},
       ...
     ],
     "stream": true
   }

2. Upstream responds with tool_use block:
   {
     "content": [
       {"type": "tool_use", "id": "toolu_xxx", "name": "Read", "input": {"file_path": "/path/main.go"}}
     ],
     "stop_reason": "tool_use"
   }

3. Claude Code executes tool locally (actually reads the file)

4. Claude Code sends follow-up with tool_result:
   POST /v1/messages
   {
     "messages": [
       {"role": "user", "content": "Read main.go for me"},
       {"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_xxx", ...}]},
       {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_xxx", "content": "package main..."}]}
     ],
     "tools": [...],
     "stream": true
   }

5. Loop until stop_reason = "end_turn"
```

## Claude Code Feature Compatibility

| Feature | Via Gateway | Reason |
|---------|:-----------:|--------|
| **Tools (Read, Edit, Bash, Write)** | Yes | Transparent proxy forwards `tools` definitions and `tool_use`/`tool_result` blocks completely |
| **Streaming (SSE)** | Yes | Gateway relays SSE chunks in real-time |
| **Skills (slash commands)** | Yes | Skills are expanded to prompts at client side — gateway sees them as regular messages |
| **Memory** | Yes | Stored in local files (`~/.claude/`) — not related to API calls |
| **Artifacts** | Yes | Displayed from response content at client — gateway doesn't touch content |
| **MCP Servers** | Yes | MCP tools are registered at client like built-in tools |
| **Multi-turn conversation** | Yes | Full message history sent in each request |
| **Extended thinking** | Yes | It's a content block type — gateway forwards without modification |

## What Gateway Does (Rate Limit Check)

```
Request comes in
│
├─ Extract API key from header (x-api-key / Authorization: Bearer)
├─ Call Rate Limiter: POST /api/ratelimit/check {key: "api-key-hash"}
│   ├─ Pass: forward request to upstream unmodified
│   └─ Fail: return 429 (Anthropic error format) immediately
│
├─ X-Profile header (if present):
│   ├─ Load profile from Redis
│   ├─ Use target provider + token from provider pool
│   ├─ Skip key pool + model fallback logic
│   └─ Proxy directly to provider upstream
│
├─ Per-Model Upstream Limiter (Gateway + Worker)
│   ├─ Extract model from request body
│   ├─ Try to acquire slot for requested model (non-blocking)
│   ├─ Full → try fallback models automatically
│   │   Priority: glm-5.1 → glm-5-turbo → glm-5 → glm-4.7 → glm-4.6 → glm-4.5 (5.x always first)
│   ├─ All models full → wait until slot available
│   ├─ RPM Limiter: controls req/min speed per provider
│   └─ If fallback → change model in body before forwarding
│   19 model slots (global cap 15): glm-5.1(1) + glm-5-turbo(1) + glm-5(2) + glm-4.7(2) + glm-4.6(3) + glm-4.5(10)
│
└─ Response: forward directly to client unmodified
```

## Known Limitations

| Issue | Cause | Solution |
|-------|-------|----------|
| Timeout during long tool loops | `STREAM_TIMEOUT` starts at 300s | Increase `STREAM_TIMEOUT=600s` in docker-compose |
| Increased latency | Gateway adds 1 hop | Normally adds <5ms (rate limit check only) |
| Large response truncated | Proxy buffer size | Currently uses `io.Copy` with no buffer limit |
| SSE not streaming real-time | Flusher not working | Check nginx/reverse proxy in front |

## Testing

```bash
# Non-streaming
curl -X POST http://localhost:8080/v1/messages \
  -H "x-api-key: YOUR_GLM_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "glm-5",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# Streaming
curl -X POST http://localhost:8080/v1/messages \
  -H "x-api-key: YOUR_GLM_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "glm-5",
    "max_tokens": 100,
    "stream": true,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# With tools (same as what Claude Code actually sends)
curl -X POST http://localhost:8080/v1/messages \
  -H "x-api-key: YOUR_GLM_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "glm-5",
    "max_tokens": 1024,
    "tools": [
      {
        "name": "Read",
        "description": "Read a file from the filesystem",
        "input_schema": {
          "type": "object",
          "properties": {"file_path": {"type": "string"}},
          "required": ["file_path"]
        }
      }
    ],
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Conversation Stress Test

```bash
# 8-turn conversation test (Thai + implement + cleanup)
bash scripts/conversation-test.sh

# 10 concurrent requests
bash scripts/stress-test.sh
```

---

*Back to [Manual](../MANUAL.md)*
