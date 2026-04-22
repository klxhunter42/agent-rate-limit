# Claude OAuth - Research & Implementation Summary

## Protocol Overview

Claude OAuth ใช้ **Authorization Code Flow + PKCE** (RFC 7636) แบบ public client (ไม่มี client_secret)

| Parameter | Value |
|---|---|
| Client ID | `9d1c250a-e61b-44d9-88ed-5944d1962f5e` |
| Override env | `CLAUDE_CODE_OAUTH_CLIENT_ID` |
| Auth URL (claude.ai) | `https://claude.ai/oauth/authorize` |
| Auth URL (console) | `https://platform.claude.com/oauth/authorize` |
| Token URL | `https://api.anthropic.com/v1/oauth/token` (หรือ `https://platform.claude.com/v1/oauth/token`) |
| Client Metadata URL | `https://claude.ai/oauth/claude-code-client-metadata` |
| Profile URL | `https://api.anthropic.com/api/oauth/profile` |
| Token format (access) | `sk-ant-oat01-...` (108 chars) |
| Token format (refresh) | `sk-ant-ort01-...` (108 chars) |

### Scopes

| Mode | Scopes |
|---|---|
| Console OAuth (minimal) | `org:create_api_key user:profile` |
| Claude.ai OAuth (full) | `user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload` |

### Non-Standard Behaviors (RFC deviance)

- **JSON body** ใน token exchange และ refresh (ไม่ใช่ `application/x-www-form-urlencoded`)
- **`state` parameter** ต้องส่งใน POST token endpoint ด้วย (ปกติ RFC 6749 ไม่ต้องการ)
- **`code=true`** parameter ต้องแนบใน authorization URL เพื่อรับ inference scopes
- Redirect URI ต้องใช้ `localhost` ไม่ใช่ `127.0.0.1`

---

## PKCE Flow (Step-by-Step)

```
1. code_verifier  = base64url(32 random bytes)          # ~43 chars
2. code_challenge = base64url(SHA256(code_verifier))
3. state          = base64url(32 random bytes)          # CSRF token

4. GET {auth_url}?client_id=...&redirect_uri=http://localhost:{port}/callback
                 &response_type=code&scope=...&state=...
                 &code_challenge=...&code_challenge_method=S256&code=true

5. Browser -> http://localhost:{port}/callback?code=...&state=...

6. POST {token_url}
   Content-Type: application/json
   {
     "grant_type": "authorization_code",
     "code": "...",
     "redirect_uri": "http://localhost:{port}/callback",
     "client_id": "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
     "code_verifier": "...",
     "state": "..."
   }

7. POST {token_url}  [refresh]
   Content-Type: application/json
   {
     "grant_type": "refresh_token",
     "refresh_token": "sk-ant-ort01-...",
     "client_id": "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
   }
```

---

## Required Headers for Anthropic API

```
Authorization: Bearer {access_token}
anthropic-beta: oauth-2025-04-20
anthropic-version: 2023-06-01
```

Claude Code CLI เพิ่ม betas เข้าไปอีก (comma-separated) ตาม condition:

| Beta | Condition |
|---|---|
| `claude-code-20250219` | Non-haiku models (always) |
| `oauth-2025-04-20` | OAuth active |
| `interleaved-thinking-2025-05-14` | Model supports thinking |
| `context-management-2025-06-27` | Context window management |
| `fast-mode-2026-02-01` | Fast mode |
| `redact-thinking-2026-02-12` | OAuth + thinking + hide summaries |

Additional CLI headers:

| Header | Value |
|---|---|
| `x-app` | `cli` or `cli-bg` |
| `x-client-request-id` | UUID |
| `User-Agent` | `claude-code/{version}` |
| `X-Claude-Code-Session-Id` | Session UUID |

---

## Repo Comparison

| | **ProxyPilot** (Go) | **ccproxy-api** (Python) | **ccs** (TypeScript) |
|---|---|---|---|
| Auth flow | PKCE Authorization Code | PKCE Authorization Code | Delegated ให้ CLIProxyAPI binary |
| Callback port | 54545 | 35593 | 54545 |
| Token storage | `~/.auth/claude-{email}.json` | `~/.claude/.credentials.json` + keychain | `~/.ccs/cliproxy/auth/claude-*.json` |
| Auto-refresh | Yes (4h lead, 5s poll, 16 workers) | Yes (120s grace, background) | **ไม่มี** - manual re-auth เท่านั้น |
| Refresh trigger | Time-based + lazy on 401 | Time-based proactive | Expiry detected แต่ไม่ทำอะไร |
| Token URL | `api.anthropic.com/v1/oauth/token` | `console.anthropic.com/v1/oauth/token` | ผ่าน CLIProxyAPI |
| Multi-account | Yes (file-per-account) | Yes (single file + keychain fallback) | Yes (accounts.json registry) |
| Headless support | No | No | Yes (paste-callback / port-forward) |

### ProxyPilot specifics
- Background refresh: min-heap scheduler, 16 concurrent workers, checks ทุก 5s
- Refresh lead: 4 hours ก่อน expiry
- 401 handling: `onAuthError` callback → lazy refresh → retry request
- Tool name remapping (anti-fingerprinting): `bash → Bash`, `read → Read` ฯลฯ
- CCH signing: auto-enabled สำหรับ OAuth tokens

### ccproxy-api specifics
- Plugin system: `oauth_claude` plugin, inject ผ่าน ServiceContainer
- In-memory cache TTL: 30s (ลด disk I/O)
- Profile cache: `~/.claude/.account.json` (subscription type, email)
- Atomic writes: temp file + rename (crash-safe)
- File permissions: `0o600`
- Bearer token inject ตอน build request ใน `ClaudeAPIAdapter._resolve_access_token()`

### ccs specifics
- OAuth ทำผ่าน CLIProxyAPI binary ทั้งหมด (ไม่ได้ implement เอง)
- Flow: `GET /v0/management/anthropic-auth-url` → browser → poll token file
- Token file polling timeout: 10 minutes, grace 15s
- Quota endpoint: `GET https://api.anthropic.com/api/oauth/usage` + `anthropic-beta: oauth-2025-04-20`
- Expiry ตรวจจับได้แต่ไม่ refresh อัตโนมัติ → user ต้อง re-auth เอง

---

## Our Project (agent-rate-limit gateway)

### Routing (provider/resolver.go:45)

```go
"claude-oauth": {FormatAnthropic, "bearer", "/v1/messages",
    map[string]string{"anthropic-beta": "oauth-2025-04-20"}},
"claude": // alias for claude-oauth
```

Model prefix `claude-*` route ไปที่ `claude-oauth` ก่อน fallback เป็น `anthropic` (API key).

### Request Headers Sent Upstream (proxy/anthropic.go:756)

```
Authorization: Bearer {access_token}    // authMode = "bearer"
anthropic-beta: oauth-2025-04-20        // ExtraHeaders from route table
anthropic-version: {cfg.AnthropicVersion}
Content-Type: application/json
```

On 401: `opts.OnAuthError` callback → refresh token → retry request once (proxy/anthropic.go:790)

### Initial OAuth (provider/oauth_authcode.go)

- PKCE: 32 bytes → base64url verifier, SHA256 → base64url challenge
- State: 32 bytes → base64url
- Redirect URI: `{redirectBase}/callback` (force `localhost`, ไม่ใช่ `127.0.0.1`)
- Claude-specific: `code=true` param ใน auth URL (oauth_authcode.go:111)
- Token exchange: JSON body สำหรับ `claude-oauth`, form-urlencoded สำหรับ provider อื่น
- Email resolution: UserInfo endpoint ก่อน, fallback เป็น JWT id_token claims

### Token Refresh Worker (provider/token-refresh.go)

```
interval:   30 minutes
threshold:  refresh if ExpiryDate < now + 45 minutes
retry:      3 attempts, exponential backoff (5s → 10s → 20s)
startup:    refreshAll() ทันทีก่อน ticker แรก (สำคัญ: ป้องกัน expired token หลัง restart)
body:       JSON สำหรับ claude-oauth, form-urlencoded สำหรับ provider อื่น
```

---

## Rate Limit Notes

- Rate limit ผูกกับ **account** ไม่ใช่ token (OAuth tokens หลายตัวจาก account เดียวกัน share limit)
- Per-model limits: haiku > sonnet > opus (haiku allowance สูงสุด)
- Rate limit headers บน **200** responses:
  - `anthropic-ratelimit-unified-5h-utilization`
  - `anthropic-ratelimit-unified-7d-utilization`
  - `anthropic-ratelimit-unified-status: allowed/limited`
- **429 ไม่มี** rate limit headers (generic `rate_limit_error` เท่านั้น)
- Beta headers และ `x-app` ไม่มีผลต่อ rate limit
- Sonnet 429 บน Pro tier: เป็น Anthropic per-model limit ปกติ ไม่ใช่ gateway issue

## Auth Sources (Claude Code CLI priority)

1. `ANTHROPIC_API_KEY` env var
2. `apiKeyHelper` script (settings.json)
3. `ANTHROPIC_AUTH_TOKEN` env var
4. `CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR`
5. OAuth token จาก macOS Keychain (`Claude Code-credentials`)
6. `CCR_OAUTH_TOKEN_FILE`
