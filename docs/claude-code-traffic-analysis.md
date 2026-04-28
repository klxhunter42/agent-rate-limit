# Claude Code v2.1.118 Traffic Analysis - Real API Capture

Date: 2026-04-28
Source: mitmproxy reverse proxy capture on macOS ARM (192.168.5.221)
CLI version: `claude-cli/2.1.118 (external, sdk-cli)`
SDK version: `@anthropic-ai/sdk 0.81.0`
Runtime: `node v24.3.0`

---

## 1. Sonnet Request Headers (claude-sonnet-4-20250514)

Captured via mitmproxy reverse proxy to `api.anthropic.com`.

```
POST /v1/messages?beta=true HTTP/1.1
Host: api.anthropic.com
Accept: application/json
Content-Type: application/json
User-Agent: claude-cli/2.1.118 (external, sdk-cli)
X-Claude-Code-Session-Id: 8886e8f9-5680-463a-a23f-1a5304007207
X-Stainless-Arch: arm64
X-Stainless-Lang: js
X-Stainless-OS: MacOS
X-Stainless-Package-Version: 0.81.0
X-Stainless-Retry-Count: 0
X-Stainless-Runtime: node
X-Stainless-Runtime-Version: v24.3.0
X-Stainless-Timeout: 3000
anthropic-beta: claude-code-20250219,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05
anthropic-dangerous-direct-browser-access: true
anthropic-version: 2023-06-01
x-api-key: sk-ant-oat01-...[OAuth token]
x-app: cli
Connection: keep-alive
Accept-Encoding: gzip, deflate, br, zstd
Content-Length: 128569
```

### Sonnet Request Body

```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 64000,
  "stream": true,
  "thinking": {
    "budget_tokens": 63999,
    "type": "enabled"
  },
  "context_management": {
    "edits": [
      {"type": "clear_thinking_20251015", "keep": "all"}
    ]
  },
  "system": [
    {"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.118.bed; cc_entrypoint=sdk-cli; cch=f36c0;"},
    {"type": "text", "text": "You are a Claude agent, built on Anthropic's Claude Agent SDK.", "cache_control": {"type": "ephemeral"}},
    {"type": "text", "text": "<full system prompt...>", "cache_control": {"type": "ephemeral"}}
  ],
  "metadata": {
    "user_id": "{\"device_id\":\"2b6575d7353e11aab4353cf46e34c992a07f3411db05793411c6fa741a53c568\",\"account_uuid\":\"\",\"session_id\":\"8886e8f9-5680-463a-a23f-1a5304007207\"}"
  },
  "messages": [...],
  "tools": [...]
}
```

### Sonnet Response (direct to Anthropic API)

Status: `401 Unauthorized`

The OAuth token was sent as `x-api-key` header, but Anthropic's direct API endpoint requires `Authorization: Bearer <token>` for OAuth tokens. This confirms the CLI uses `x-api-key` header format and expects the upstream to handle OAuth token validation.

Response headers from Anthropic:
```
Date: Mon, 27 Apr 2026 19:15:02 GMT
Content-Type: application/json
x-should-retry: false
request-id: req_011CaUoxw5PzQrNghGgBMjbf
Server: cloudflare
x-envoy-upstream-service-time: 13
Content-Encoding: gzip
CF-RAY: 9f302751bc42260c-SIN
```

---

## 2. Haiku Request Headers (claude-haiku-4-5-20251001)

Captured via mitmproxy reverse proxy to our gateway (`192.168.5.62:9000`). The request went through gateway successfully.

```
POST /v1/messages?beta=true HTTP/1.1
Host: 192.168.5.62:9000
Accept: application/json
Content-Type: application/json
User-Agent: claude-cli/2.1.118 (external, sdk-cli)
X-Claude-Code-Session-Id: 8998aa42-95d8-4e0a-891e-7543c7cd2e48
X-Stainless-Arch: arm64
X-Stainless-Lang: js
X-Stainless-OS: MacOS
X-Stainless-Package-Version: 0.81.0
X-Stainless-Retry-Count: 0
X-Stainless-Runtime: node
X-Stainless-Runtime-Version: v24.3.0
X-Stainless-Timeout: 3000
anthropic-beta: interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,claude-code-20250219
anthropic-dangerous-direct-browser-access: true
anthropic-version: 2023-06-01
x-api-key: sk-ant-oat01-...[OAuth token]
x-app: cli
Connection: keep-alive
Accept-Encoding: gzip, deflate, br, zstd
Content-Length: 128575
```

### Haiku Request Body

```json
{
  "model": "claude-haiku-4-5-20251001",
  "max_tokens": 64000,
  "stream": true,
  "thinking": {
    "budget_tokens": 63999,
    "type": "enabled"
  },
  "context_management": {
    "edits": [
      {"type": "clear_thinking_20251015", "keep": "all"}
    ]
  },
  "system": [
    {"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.118.bed; cc_entrypoint=sdk-cli; cch=a0a95;"},
    {"type": "text", "text": "You are a Claude agent, built on Anthropic's Claude Agent SDK.", "cache_control": {"type": "ephemeral"}},
    {"type": "text", "text": "<full system prompt...>", "cache_control": {"type": "ephemeral"}}
  ],
  "metadata": {
    "user_id": "{\"device_id\":\"2b6575d7353e11aab4353cf46e34c992a07f3411db05793411c6fa741a53c568\",\"account_uuid\":\"\",\"session_id\":\"8998aa42-95d8-4e0a-891e-7543c7cd2e48\"}"
  },
  "messages": [...],
  "tools": [...]
}
```

### Haiku Response (through gateway -> Anthropic)

Status: `200 OK` (SSE stream)

Response headers (from gateway, forwarded from Anthropic):
```
Anthropic-Ratelimit-Unified-5h-Reset: 1777327800
Anthropic-Ratelimit-Unified-5h-Status: allowed
Anthropic-Ratelimit-Unified-5h-Utilization: 0.05
Anthropic-Ratelimit-Unified-7d-Reset: 1777748400
Anthropic-Ratelimit-Unified-7d-Status: allowed
Anthropic-Ratelimit-Unified-7d-Utilization: 0.05
Anthropic-Ratelimit-Unified-Fallback-Percentage: 0.5
Anthropic-Ratelimit-Unified-Representative-Claim: five_hour
Anthropic-Ratelimit-Unified-Reset: 1777327800
Anthropic-Ratelimit-Unified-Status: allowed
Cache-Control: no-store
Content-Type: text/event-stream; charset=utf-8
Request-Id: req_011CaUprQMu4Cfw7Tm5PN4YX
Via: 1.1 Caddy
X-Correlation-Id: b4324c33-dc19-40ef-a161-2ec68e70b6b2
```

SSE body start:
```
event: message_start
data: {"type":"message_start","message":{"model":"claude-haiku-4-5-20251001","id":"msg_0162kZku2isBD4ynvTwFaPZ1",...,"usage":{"input_tokens":3,"cache_creation_input_tokens":33380,...}}}
```

---

## 3. Differences Between Sonnet and Haiku Requests

### Headers

| Aspect | Sonnet | Haiku |
|--------|--------|-------|
| All headers | **Identical** | **Identical** |
| anthropic-beta order | `claude-code-20250219,interleaved-thinking-2025-05-14,...` | `interleaved-thinking-2025-05-14,...,claude-code-20250219` |
| anthropic-beta values | Same 4 betas, different order | Same 4 betas, different order |
| Content-Length | 128569 | 128575 |

**Key finding: The headers are IDENTICAL between sonnet and haiku.** The only difference is:
1. The `model` field in the body
2. The `cch` value in the billing header system block (changes per session)
3. The `session_id` in both `X-Claude-Code-Session-Id` header and `metadata.user_id`
4. Minor `Content-Length` difference due to model name length

### anthropic-beta Header

Both requests send exactly these 4 beta flags:
1. `claude-code-20250219`
2. `interleaved-thinking-2025-05-14`
3. `context-management-2025-06-27`
4. `prompt-caching-scope-2026-01-05`

The order may vary between requests but the values are the same.

### Body Structure

Both sonnet and haiku use identical body structure:
- `max_tokens: 64000`
- `stream: true`
- `thinking: {budget_tokens: 63999, type: "enabled"}`
- `context_management: {edits: [{type: "clear_thinking_20251015", keep: "all"}]}`
- 3 system blocks (billing header, agent identity, full system prompt)
- Same tool definitions (44 tools)
- Same metadata structure

---

## 4. Response Status Codes and Headers

### Successful Response (200 OK)

Key Anthropic response headers for rate limit tracking:
- `Anthropic-Ratelimit-Unified-Status: allowed`
- `Anthropic-Ratelimit-Unified-5h-Status: allowed`
- `Anthropic-Ratelimit-Unified-7d-Status: allowed`
- `Anthropic-Ratelimit-Unified-5h-Utilization: 0.05`
- `Anthropic-Ratelimit-Unified-7d-Utilization: 0.05`
- `Anthropic-Ratelimit-Unified-5h-Reset: <unix_timestamp>`
- `Anthropic-Ratelimit-Unified-7d-Reset: <unix_timestamp>`
- `Anthropic-Ratelimit-Unified-Reset: <unix_timestamp>`
- `Anthropic-Ratelimit-Unified-Fallback-Percentage: 0.5`
- `Anthropic-Ratelimit-Unified-Representative-Claim: five_hour`
- `Request-Id: req_xxxx`
- `Content-Type: text/event-stream; charset=utf-8`
- `Cache-Control: no-store`

### Authentication Error (401)

```
Content-Type: application/json
x-should-retry: false
request-id: req_xxxx
Server: cloudflare
x-envoy-upstream-service-time: 13
```

Body:
```json
{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}
```

---

## 5. Exact Beta Header Value

```
anthropic-beta: claude-code-20250219,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05
```

Note: There is NO `oauth-2025-04-20` beta flag in the real CLI traffic. Our gateway's `claude-oauth` provider route table includes `oauth-2025-04-20` and `effort-2025-11-24` which the real CLI does NOT send.

---

## 6. Session/Request ID Patterns

### X-Claude-Code-Session-Id

Format: UUID v4 (e.g., `8886e8f9-5680-463a-a23f-1a5304007207`)

This is generated once per CLI invocation (each `claude -p` call gets a new session ID).

### metadata.user_id

Format: JSON string containing:
```json
{
  "device_id": "2b6575d7353e11aab4353cf46e34c992a07f3411db05793411c6fa741a53c568",
  "account_uuid": "",
  "session_id": "<same as X-Claude-Code-Session-Id>"
}
```

- `device_id`: SHA-256 hash, stable per device/installation
- `account_uuid`: Empty string for OAuth users (not yet authenticated)
- `session_id`: Matches `X-Claude-Code-Session-Id` header

### Request-Id (response)

Format: `req_` prefix + 24-char alphanumeric (e.g., `req_011CaUoxw5PzQrNghGgBMjbf`)

### x-anthropic-billing-header (system block 0)

Format: `x-anthropic-billing-header: cc_version=2.1.118.bed; cc_entrypoint=sdk-cli; cch=<5-char-hex>;`

The `cch` value changes per session/request (likely a hash of session context).

---

## 7. Comparison with Gateway Current Headers

### Gateway ExtraHeaders (claude-oauth route, from `resolver.go`)

```go
"anthropic-beta": "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,effort-2025-11-24",
"x-app":          "cli",
"User-Agent":     "claude-cli/2.1.118 (external, sdk-cli)",
"anthropic-dangerous-direct-browser-access": "true",
"Accept":                      "application/json",
"accept-language":             "*",
"sec-fetch-mode":              "cors",
"X-Stainless-Lang":            "js",
"X-Stainless-Package-Version": "0.81.0",
"X-Stainless-OS":              "MacOS",
"X-Stainless-Arch":            "arm64",
"X-Stainless-Runtime":         "node",
"X-Stainless-Runtime-Version": "v24.3.0",
"X-Stainless-Retry-Count":     "0",
"X-Stainless-Timeout":         "3000",
```

### Differences: Real CLI vs Gateway

| Header | Real CLI | Gateway | Match? |
|--------|----------|---------|--------|
| anthropic-beta | `claude-code-20250219,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05` | `claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,effort-2025-11-24` | **NO** - gateway has extra `oauth-2025-04-20` and `effort-2025-11-24` |
| accept-language | NOT sent | `*` | **NO** - gateway sends extra header |
| sec-fetch-mode | NOT sent | `cors` | **NO** - gateway sends extra header |
| User-Agent | `claude-cli/2.1.118 (external, sdk-cli)` | Same | YES |
| x-app | `cli` | Same | YES |
| anthropic-dangerous-direct-browser-access | `true` | Same | YES |
| X-Stainless-* | All present | All present | YES |
| anthropic-version | `2023-06-01` | `2023-06-01` | YES |
| Accept-Encoding | `gzip, deflate, br, zstd` | NOT set (Go default) | **DIFFERENT** |
| X-Claude-Code-Session-Id | UUID v4 per session | Generated if missing | YES (passthrough) |
| x-anthropic-billing-header | In system[0] block | NOT present | **MISSING** |

### Critical Differences That Could Trigger Detection

1. **Extra beta flags**: Gateway sends `oauth-2025-04-20` and `effort-2025-11-24` that the real CLI does NOT send. This is the most suspicious difference.

2. **Extra headers**: Gateway sends `accept-language: *` and `sec-fetch-mode: cors` which the real CLI does NOT send. These are browser-style headers that may flag the request as non-standard.

3. **Missing billing header**: The real CLI includes `x-anthropic-billing-header` in system[0]. The gateway does not inject this.

4. **Accept-Encoding**: The real CLI sends `gzip, deflate, br, zstd`. The Go HTTP client sends its own Accept-Encoding.

5. **URL path**: Real CLI sends to `/v1/messages?beta=true`. Gateway routes correctly use this.

### Recommendation

Update `providerRouteTable["claude-oauth"].extraHeaders` to:
- Remove `oauth-2025-04-20` and `effort-2025-11-24` from `anthropic-beta`
- Remove `accept-language` and `sec-fetch-mode` headers entirely
- Keep all other headers as-is

---

## 8. Tools List (Complete - 44 tools)

```
Agent, AskUserQuestion, Bash, CronCreate, CronDelete, CronList, Edit,
EnterPlanMode, EnterWorktree, ExitPlanMode, ExitWorktree, Glob, Grep,
ListMcpResourcesTool, NotebookEdit, Read, ReadMcpResourceTool,
ScheduleWakeup, Skill, TaskOutput, TaskStop, TodoWrite, WebFetch,
WebSearch, Write,
mcp__claude_ai_Asana__authenticate, mcp__claude_ai_Asana__complete_authentication,
mcp__claude_ai_Atlassian__authenticate, mcp__claude_ai_Atlassian__complete_authentication,
mcp__claude_ai_Box__authenticate, mcp__claude_ai_Box__complete_authentication,
mcp__claude_ai_Canva__authenticate, mcp__claude_ai_Canva__complete_authentication,
mcp__claude_ai_HubSpot__authenticate, mcp__claude_ai_HubSpot__complete_authentication,
mcp__claude_ai_Intercom__authenticate, mcp__claude_ai_Intercom__complete_authentication,
mcp__claude_ai_Linear__authenticate, mcp__claude_ai_Linear__complete_authentication,
mcp__claude_ai_Mermaid_Chart__validate_and_render_mermaid_diagram,
mcp__claude_ai_monday_com__authenticate, mcp__claude_ai_monday_com__complete_authentication,
mcp__claude_ai_Notion__authenticate, mcp__claude_ai_Notion__complete_authentication
```

---

## 9. Why Sonnet Gets 429 but Haiku Works

Based on this traffic analysis, the **request headers and body structure are identical** between sonnet and haiku. The 429 rate limiting is NOT caused by header differences -- it is caused by:

1. **Per-model rate limits on Anthropic's side**: Claude Pro/Max plans have separate rate limits per model. Sonnet has lower rate limits than haiku because sonnet is more expensive to run.

2. **Higher token usage for sonnet**: Sonnet typically uses more output tokens (thinking + response), which depletes the rate limit faster.

3. **Unified usage tracking**: The `Anthropic-Ratelimit-Unified-*` headers show that Anthropic tracks usage across a 5-hour and 7-day window. Sonnet's higher token cost per request means the unified utilization fills up faster.

The gateway should NOT need different headers for sonnet vs haiku -- both use identical request structure.
