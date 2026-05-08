# Claude Code Payload Reference: Terminal vs VSCode Panel

Gateway sees both clients as "Claude Code" -- no routing distinction. The differences are in headers and payload complexity.

## Headers

| Header                                      | Terminal (CLI)                       | VSCode Panel            |
|---------------------------------------------|--------------------------------------|-------------------------|
| `Authorization`                             | `Bearer sk-ant-oaitkn-...`           | Same                    |
| `User-Agent`                                | `claude-cli/2.1.123 (external, cli)` | Electron/Chromium-based |
| `anthropic-beta`                            | Full list (see below)                | Same                    |
| `anthropic-dangerous-direct-browser-access` | `true`                               | `true`                  |
| `x-app`                                     | `cli`                                | `cli`                   |
| `X-Stainless-Lang`                          | `js`                                 | `js`                    |
| `X-Stainless-Runtime`                       | `node`                               | `node`                  |
| `x-client-request-id`                       | UUID                                 | UUID                    |
| `X-Claude-Code-Session-Id`                  | Session UUID                         | Session UUID            |

### anthropic-beta (both clients)

```
claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,
redact-thinking-2026-02-12,context-management-2025-06-27,
prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24
```

## Request Body Structure

### Terminal (CLI) -- text-only request

```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 16384,
  "stream": true,
  "temperature": 1.0,
  "thinking": {"type": "enabled", "budget_tokens": 50000},
  "context_management": {"enabled": true},
  "tools": [
    {"name": "Bash", "description": "...", "input_schema": {...}},
    {"name": "Read", "description": "...", "input_schema": {...}},
    {"name": "Write", "description": "...", "input_schema": {...}},
    {"name": "Edit", "description": "...", "input_schema": {...}}
  ],
  "tool_choice": {"type": "auto"},
  "system": [
    {"type": "text", "text": "x-anthropic-billing-header: cc_version=...", "cache_control": {"type": "ephemeral"}},
    {"type": "text", "text": "You are Claude Code, Anthropic's official CLI...", "cache_control": {"type": "ephemeral"}},
    {"type": "text", "text": "...full system prompt...", "cache_control": {"type": "ephemeral"}}
  ],
  "messages": [
    {"role": "user", "content": "Fix the bug in main.go"},
    {"role": "assistant", "content": [
      {"type": "redacted_thinking", "data": "..."},
      {"type": "text", "text": "I'll read main.go first."},
      {"type": "tool_use", "id": "toolu_01", "name": "Read", "input": {"file_path": "main.go"}}
    ]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "toolu_01", "content": "package main\n\nfunc main() {...}"}
    ]}
  ]
}
```

### VSCode Panel -- image attachment request

```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 16384,
  "stream": true,
  "temperature": 1.0,
  "thinking": {"type": "enabled", "budget_tokens": 50000},
  "context_management": {"enabled": true},
  "output_config": {"effort": "high"},
  "stream_options": {"include_usage": true},
  "metadata": {"user_id": "..."},
  "service_tier": "auto",
  "tools": [
    {"name": "Bash", "description": "...", "input_schema": {...}},
    {"name": "Read", "description": "...", "input_schema": {...}},
    {"name": "Write", "description": "...", "input_schema": {...}},
    {"name": "Edit", "description": "...", "input_schema": {...}}
  ],
  "tool_choice": {"type": "auto"},
  "system": [
    {"type": "text", "text": "...system prompt...", "cache_control": {"type": "ephemeral"}}
  ],
  "messages": [
    {"role": "user", "content": [
      {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "<base64>"}},
      {"type": "text", "text": "Describe this image"}
    ]},
    {"role": "assistant", "content": [
      {"type": "redacted_thinking", "data": "..."},
      {"type": "text", "text": "This shows a DevOps pipeline diagram."},
      {"type": "server_tool_use", "id": "st_1", "name": "web_search", "input": {}}
    ]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "toolu_01", "content": "..."},
      {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "<base64>"}},
      {"type": "text", "text": "Also check this", "cache_control": {"type": "ephemeral"}},
      {"type": "server_tool_use", "id": "st_2", "name": "web_search", "input": {"query": "test"}}
    ]}
  ]
}
```

## Key Differences

| Aspect                            | Terminal                    | VSCode Panel                        |
|-----------------------------------|-----------------------------|-------------------------------------|
| Image content blocks              | Rare (paste from clipboard) | Common (drag & drop, paste)         |
| `server_tool_use` blocks          | No                          | Yes (web search, etc.)              |
| `cache_control` on message blocks | Rare                        | Common (on text/image blocks)       |
| `output_config`                   | Not sent                    | Sent (`{"effort": "high"}`)         |
| `stream_options`                  | Not sent                    | Sent (`{"include_usage": true}`)    |
| `metadata`                        | Not sent                    | Sent (`{"user_id": "..."}`)         |
| `service_tier`                    | Not sent                    | Sent (`"auto"`)                     |
| `redacted_thinking` in history    | Yes                         | Yes                                 |
| Multi-image payloads              | Rare                        | Common (multiple turns with images) |
| Payload size                      | ~10-50 KB                   | ~100 KB - 5 MB (with images)        |

## Gateway Processing for Z.AI (GLM models)

When these payloads route to Z.AI, the gateway filters unsupported content blocks (server_tool_use only). Fields like tools/tool_choice are preserved:

### Fields stripped by `stripUnsupportedFields()`

| Field                | Reason                        |
|----------------------|-------------------------------|
| `tools`              | Z.AI has no tool support      |
| `tool_choice`        | Requires tools                |
| `thinking`           | Z.AI has no extended thinking |
| `budget_tokens`      | Part of thinking              |
| `effort`             | Z.AI has no effort control    |
| `output_config`      | Contains effort               |
| `stream_options`     | Z.AI does not support         |
| `metadata`           | Not supported                 |
| `service_tier`       | Not supported                 |
| `context_management` | Not supported                 |

### Content blocks stripped by `filterUnsupportedContent()`

| Block/Key                                        | Action                 |
|--------------------------------------------------|------------------------|
| `server_tool_use` type blocks                    | Removed entirely       |
| `cache_control` on any block (messages + system) | Key removed from block |

### Vision routing

When image content is detected with a GLM model:
- `glm-5.1` -> `glm-4.6v` (auto-selected)
- `glm-4.6v` routed via Anthropic-compatible endpoint (same billing pool)
- Non-native image models routed via Z.AI OpenAI endpoint

## Testing

### Terminal test (curl via Docker network)

```bash
# Small payload - inline
IMG_B64=$(base64 -i ~/Pictures/test.png | tr -d '\n')
ssh klxhunter@192.168.5.111 "docker exec arl-proxy curl -s -X POST \
  http://arl-gateway:8080/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'x-api-key: YOUR_ZAI_API_KEY' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{\"model\":\"glm-5.1\",\"max_tokens\":100,\"stream\":true,
       \"messages\":[{\"role\":\"user\",\"content\":[
         {\"type\":\"image\",\"source\":{\"type\":\"base64\",\"media_type\":\"image/png\",\"data\":\"$IMG_B64\"}},
         {\"type\":\"text\",\"text\":\"What do you see?\"}
       ]}]}'"

# Large payload - file-based
python3 -c "
import json, base64
img = base64.b64encode(open('test.png','rb').read()).decode()
json.dump({'model':'glm-5.1','max_tokens':100,'stream':True,
  'messages':[{'role':'user','content':[
    {'type':'image','source':{'type':'base64','media_type':'image/png','data':img}},
    {'type':'text','text':'What do you see?'}
  ]}]
}, open('/tmp/test_payload.json','w'))
"
scp /tmp/test_payload.json klxhunter@192.168.5.111:/tmp/test_payload.json
ssh klxhunter@192.168.5.111 "docker cp /tmp/test_payload.json arl-proxy:/tmp/ && \
  docker exec arl-proxy curl -s -X POST http://arl-gateway:8080/v1/messages \
  -H 'Content-Type: application/json' -H 'x-api-key: YOUR_KEY' \
  -H 'anthropic-version: 2023-06-01' -d @/tmp/test_payload.json"
```

### VSCode Panel test (manual)

1. Open VSCode with Claude Code extension pointed at the gateway
2. Open Claude Code panel (sidebar)
3. Drag & drop or paste an image
4. Send a prompt like "Describe this image"
5. Check gateway logs:

```bash
ssh klxhunter@192.168.5.111 "docker logs arl-gateway --tail 30 2>&1 | grep -v health\|metrics"
```

### Direct Z.AI test (bypass gateway)

```bash
curl -s -X POST https://api.z.ai/api/anthropic/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'x-api-key: YOUR_ZAI_API_KEY' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{"model":"glm-4.6v","max_tokens":50,"stream":true,
       "messages":[{"role":"user","content":"Say hi"}]}'
```
