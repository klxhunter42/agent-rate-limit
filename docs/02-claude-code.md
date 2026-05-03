# Claude Code Usage

## Setup

### GLM Mode (Z.AI)

Edit `~/.claude/settings.json`:

```json
{
  "ANTHROPIC_BASE_URL": "http://localhost:9000",
  "ANTHROPIC_AUTH_TOKEN": "your-glm-api-key"
}
```

> **ANTHROPIC_AUTH_TOKEN** is required because the gateway uses the `x-api-key` header for identity + rate limiting.
> Claude Code sends this value as the `x-api-key` header automatically.

### Claude OAuth Mode (Transparent Passthrough)

Edit `~/.claude/settings.json`:

```json
{
  "ANTHROPIC_BASE_URL": "http://localhost:9000",
  "ANTHROPIC_API_KEY": "arl_your-profile-token"
}
```

Or set environment variables:

```bash
export ANTHROPIC_BASE_URL=http://localhost:9000
export ANTHROPIC_API_KEY=arl_your-profile-token
claude
```

## Architecture

### GLM Mode (Transparent Proxy)

```
Claude Code ──POST /v1/messages──▶ Gateway :8080 ──transparent──▶ api.z.ai/api/anthropic
(ANTHROPIC_BASE_URL)
```

### Claude OAuth Mode (Profile Routing + Billing Injection)

```
Claude Code ──POST /v1/messages──▶ Gateway :8080
(ANTHROPIC_API_KEY=arl_*)         │
                                  ├─ ResolveProfileToken() -> profile -> claude-oauth
                                  ├─ Get OAuth token from Redis (sk-ant-oat01-*)
                                  ├─ Transparent mode: fix headers (Bearer, oauth-2025-04-20)
                                  ├─ Go billing injection -> api.anthropic.com
                                  │   (fallback: Sidecar -> Direct proxy)
                                  └─ Privacy masking (PasteGuard)
```

**User experience is identical** -- Gateway is a transparent proxy for GLM mode:
- Does not decode/re-encode request/response
- Forwards every byte directly
- Does not touch any fields (tools, tool_choice, messages, content, headers)

For Claude OAuth mode, the gateway:
- Injects billing header for Claude Code rate limit bucket
- Applies privacy masking on request body
- Manages OAuth token lifecycle (refresh, rotation)

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

## What Gateway Does (Messages Handler)

```
Request comes in (POST /v1/messages)
|
+- Parse body, extract model name
|
+- Provider resolver: match model prefix to route table
|   +- claude-* -> claude-oauth (round-robin) -> anthropic
|   +- glm-* -> zai
|   +- gemini-* -> gemini-oauth -> gemini
|   +- gpt-*/o3-*/o4-* -> openai
|   +- or-* -> openrouter
|
+- Transparent passthrough detection:
|   +- Client sends Bearer sk-ant-oat01-* -> transparent mode
|   +- Transparent: skip optimizer/masking, forward headers as-is
|   +- Non-transparent: apply prompt injection, smart max_tokens, strip fields
|
+- Profile detection (arl_ token / X-Profile header):
|   +- Load profile from Redis
|   +- Use target provider + token from provider pool
|   +- Override model, base URL, auth mode from profile
|
+- Per-Model Upstream Limiter
|   +- Try to acquire slot for requested model (non-blocking)
|   +- Full -> try fallback models automatically
|   +- Prevent cross-provider fallback (claude <-> glm blocked)
|   +- Adaptive limiter with probe multiplier
|
+- Token optimization pipeline (13 optimizers, text-only)
|   +- Budget level estimation (0/1/2 based on context usage)
|   +- OptimizeSystemPrompt + OptimizeMessages
|
+- Privacy masking (PasteGuard, text-only)
|   +- Detect secrets -> mask with placeholders
|   +- Detect PII (email, phone) -> mask with placeholders
|
+- Image detection:
|   +- Has images -> skip optimizer + privacy (avoid corrupting base64/URLs)
|   +- Auto-select vision model (glm-4.6v or glm-4.5v)
|
+- Format-aware proxy:
|   +- FormatAnthropic -> Anthropic proxy (with billing injection for OAuth)
|   +- FormatOpenAI -> OpenAI proxy (with continuation + tool mode support)
|   +- FormatGemini -> CodeAssist proxy or Gemini API proxy
|   +- ZAI Web chat proxy (free access models)
|
+- Feedback loop (post-response):
    +- Adaptive limiter feedback (RTT, status code)
    +- Key pool 429/success tracking
    +- Anomaly detection (z-score based)
    +- Rate limit utilization capture from response headers
    +- Usage recording (Prometheus + Redis)
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
# Non-streaming (GLM)
curl -X POST http://localhost:9000/v1/messages \
  -H "x-api-key: YOUR_GLM_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "glm-5",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# Streaming
curl -X POST http://localhost:9000/v1/messages \
  -H "x-api-key: YOUR_GLM_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "glm-5",
    "max_tokens": 100,
    "stream": true,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# Claude OAuth (transparent passthrough via profile)
curl -X POST http://localhost:9000/v1/messages \
  -H "x-api-key: arl_your-profile-token" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# With tools (same as what Claude Code sends)
curl -X POST http://localhost:9000/v1/messages \
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
