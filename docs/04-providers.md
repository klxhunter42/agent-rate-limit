# AI Providers

## Adding Providers

Add API keys in `.env`:

```bash
OPENAI_API_KEYS=sk-proj-xxx,sk-proj-yyy
ANTHROPIC_API_KEYS=sk-ant-xxx
GEMINI_API_KEYS=AIzaxxx
OPENROUTER_API_KEYS=sk-or-xxx
```

Then restart ai-worker:

```bash
docker-compose up -d --build arl-worker
```

## Supported Providers

### API Key Auth (add in `.env`)

| Provider ID | Env Var | Upstream |
|------------|---------|----------|
| `anthropic` | `ANTHROPIC_API_KEYS` | api.anthropic.com |
| `gemini` | `GEMINI_API_KEYS` | generativelanguage.googleapis.com |
| `openai` | `OPENAI_API_KEYS` | api.openai.com |
| `zai` | `UPSTREAM_API_KEYS` | api.z.ai/api/anthropic |
| `openrouter` | `OPENROUTER_API_KEYS` | openrouter.ai/api |
| `deepseek` | `DEEPSEEK_API_KEYS` | api.deepseek.com |
| `kimi` | `KIMI_API_KEYS` | api.moonshot.cn/v1 |
| `huggingface` | `HUGGINGFACE_API_KEYS` | api-inference.huggingface.co |
| `ollama` | `OLLAMA_API_KEYS` | localhost:11434 |
| `agy` | `AGY_API_KEYS` | antigravity.com |
| `cursor` | `CURSOR_API_KEYS` | api2.cursor.sh |
| `codebuddy` | `CODEBUDDY_API_KEYS` | api.codebuddy.io |
| `kilo` | `KILO_API_KEYS` | api.kilo.ai |

### OAuth Auth (via Dashboard UI)

| Provider ID | Name | Auth Method | Notes |
|------------|------|-------------|-------|
| `claude-oauth` | Claude (OAuth) | OAuth PKCE + Bearer token | Uses Claude Code Client ID, proxy to api.anthropic.com |
| `gemini-oauth` | Google Gemini (OAuth) | OAuth auth code | Uses Code Assist proxy (cloudcode-pa.googleapis.com) |
| `copilot` | GitHub Copilot | Device code flow | Uses GitHub device code |

## anthropic vs claude-oauth

| Provider | Auth | Use Case |
|----------|------|----------|
| `anthropic` | API Key (`x-api-key` header) | Has direct Anthropic API key |
| `claude-oauth` | OAuth Bearer token (`Authorization: Bearer`) | Uses Claude Code subscription, no API key needed |

> **Note**: Provider `claude-oauth` (originally named `claude`) uses OAuth PKCE flow through platform.claude.com with Bearer token auth sent to api.anthropic.com/v1/messages (with `anthropic-beta: oauth-2025-04-20` header)

## gemini vs gemini-oauth

| Provider | Auth | Use Case |
|----------|------|----------|
| `gemini` | API Key (query param `?key=`) | Has direct Google AI API key |
| `gemini-oauth` | OAuth Bearer token | Uses Google account + Code Assist |

> **Important**: `gemini-oauth` and `gemini` are separate providers. Gemini OAuth does NOT fallback to direct Gemini API. If you need both, register the `gemini` API key separately.

## Token Migration (Automatic)

When gateway starts, tokens are migrated automatically:

- `claude` -> `claude-oauth` (all tokens previously registered under `claude` are moved to `claude-oauth` automatically)

## Provider Fallback Order

1. **glm** (Z.ai) -- Primary
2. **openai**
3. **anthropic**
4. **gemini**
5. **openrouter**

If the first provider fails, automatically skips to the next provider that has an API key.

## Adding Provider via OAuth (Dashboard UI)

1. Open `http://localhost:8080/admin`
2. Go to Providers or Accounts page
3. Select provider (e.g., `claude-oauth`, `gemini-oauth`, `copilot`)
4. Click "Start Auth" and follow on-screen steps
5. When auth succeeds, token is stored in Dragonfly automatically

## Email Input (after OAuth)

Some providers (e.g., `claude-oauth`) don't return email from OAuth flow. The system shows a step to enter email (optional) for easier account identification. Email can be edited later from the Account List page by clicking the email field directly (inline editing) or via API:

```bash
curl -X POST http://localhost:8080/v1/auth/accounts/claude-oauth/{accountId}/email \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com"}'
```

---

## Claude OAuth Transparent Passthrough

System that enables Claude Code CLI to use Sonnet/Opus through the gateway. The gateway:

1. Receives request with profile API token (`arl_*`)
2. Retrieves OAuth token from Redis (linked to Anthropic account)
3. **Go billing injection** -- computes billing header in Go, injects as `system[0]` (primary path)
4. If Anthropic rejects billing header -> fallback to Node.js sidecar -> fallback to direct proxy
5. Privacy masking (PasteGuard) runs before proxy
6. Message body optimization (whitespace + dedup) runs before privacy masking

### Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Claude Code CLI                                                              │
│ (Remote: 192.168.5.62)                                                   │
│                                                                              │
│ ANTHROPIC_BASE_URL=http://192.168.5.62:9000                                 │
│ ANTHROPIC_API_KEY=arl_2f3a72a7...                                            │
│                                                                              │
│ POST /v1/messages                                                            │
│ Headers: x-api-key: arl_2f3a72a7...                                         │
│ Body: {model: "claude-sonnet-4-20250514", messages: [...]}                   │
└────────────────────────────┬────────────────────────────────────────────────┘
                             │
                             │ HTTP POST (arl_ token)
                             ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ Caddy Reverse Proxy (:9000)                                                  │
└────────────────────────────┬────────────────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ API Gateway (Go, :8080)                                                      │
│                                                                              │
│ 1. ResolveProfileToken() -> profile -> claude-oauth                         │
│ 2. Get OAuth token from Redis (sk-ant-oat01-*)                              │
│ 3. Transparent mode: fix headers (Bearer auth, oauth-2025-04-20)            │
│ 4. Message body optimization (whitespace + dedup)                            │
│ 5. Privacy masking (PasteGuard: secrets, PII)                                │
│ 6. 3-Path Routing:                                                           │
│    ├── Path 1 (primary): Go billing injection -> api.anthropic.com           │
│    ├── Path 2 (fallback): Sidecar (Node.js) -> api.anthropic.com             │
│    └── Path 3 (last resort): Direct proxy (no billing header)                │
└────────────────────────────┬────────────────────────────────────────────────┘
                             │
                             │ Path 1 (Go billing injection)
                             │ Inject billing header as system[0] in Go
                             │ HTTPS POST to api.anthropic.com
                             ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ api.anthropic.com/v1/messages                                                │
│                                                                              │
│ Auth: OAuth Bearer token + anthropic-beta: oauth-2025-04-20                  │
│ Billing: Claude Code rate limit bucket (more generous limits)                │
│ Response: 200 {content: [{type:"text", text:"Hello!"}]}                     │
│                                                                              │
│ If 400 "reserved keyword" (billing rejected):                                │
│ → Fallback to Path 2 (Sidecar) -> Path 3 (Direct)                           │
└────────────────────────────┬────────────────────────────────────────────────┘
                             │
                             │ 200 OK (SSE stream or JSON)
                             ▼
                        Back to CLI client
```

### Request Routing Flow (Step by Step)

```
Client sends request
│
├─ Header: x-api-key: arl_2f3a72a7...
├─ Body: {model: "claude-sonnet-4-20250514", messages: [...]}
│
▼
Handler.Messages()
│
├─ 1. Parse model from body: "claude-sonnet-4-20250514"
│
├─ 2. Profile detection:
│   x-api-key starts with "arl_" -> ResolveProfileToken()
│   -> profile target = "claude-oauth"
│   -> apiKey = "sk-ant-oat01-*" from Redis
│
├─ 3. Transparent override:
│   apiKey starts with "sk-ant-oat01-" -> transparent = true
│   Fix headers: Bearer auth, oauth-2025-04-20
│
├─ 4. Message body optimization:
│   OptimizeMessages() -> whitespace + dedup on text content
│   Skips: code blocks, tool_use, privacy placeholders
│
├─ 5. Privacy masking (PasteGuard):
│   Detect secrets/PII -> mask with placeholders
│
├─ 6. trySidecarOrDirect() -- 3-path routing:
│
│   Path 1: Go billing injection (primary)
│   ├─ InjectBillingHeader() computes billing header in Go
│   ├─ ProxyTransparent() -> api.anthropic.com
│   ├─ Record metrics: path=go_direct
│   └─ If 400 "reserved keyword" -> ErrBillingRejected -> fallback
│
│   Path 2: Sidecar fallback (if Go billing rejected)
│   ├─ ProxySidecar() -> Node.js sidecar -> api.anthropic.com
│   ├─ Record metrics: path=sidecar
│   └─ If sidecar fails -> fallback
│
│   Path 3: Direct proxy (last resort)
│   ├─ ProxyTransparent() without billing header
│   ├─ Record metrics: path=direct
│   └─ Uses generic OAuth bucket (more 429s expected)
│
└─ 7. Response 200 -> unmask PII -> relay to client
```

### Auth Mechanism: Two Paths

```
Path 1: API Key (standard)
  ANTHROPIC_API_KEY = sk-ant-api03-*
  → Sent as: x-api-key header
  → Anthropic validates via API key lookup
  → Works with any Anthropic-compatible endpoint

Path 2: OAuth Token (Claude Code)
  ANTHROPIC_AUTH_TOKEN = sk-ant-oat01-*
  → Sent as: Authorization: Bearer header
  → REQUIRES: anthropic-beta includes oauth-2025-04-20
  → Without oauth-2025-04-20: "OAuth authentication is currently not supported"
  → Wrong header (x-api-key instead of Bearer): "invalid x-api-key"
```

### Required Headers for OAuth on /v1/messages

| Header | Value | Required |
|--------|-------|:--------:|
| `Authorization` | `Bearer sk-ant-oat01-*` | YES |
| `anthropic-beta` | Must include `oauth-2025-04-20` | YES |
| `anthropic-version` | `2023-06-01` | YES |
| `x-app` | `cli` | YES |
| `anthropic-dangerous-direct-browser-access` | `true` | Recommended |
| `User-Agent` | `claude-cli/2.1.123 (external, cli)` | Recommended |

Full `anthropic-beta` value (from resolver route table):

```
claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,
redact-thinking-2026-02-12,context-management-2025-06-27,
prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24
```

### Billing Header Algorithm (from Claude CLI v2.1.123)

Sidecar injects a billing header into `system[0]` that routes the request to the Claude Code rate limit bucket (higher limits than generic OAuth):

```
Step 1: Extract first user message text
  firstMsg = messages.find(m => m.role === "user" && !m.isMeta)
  text = firstMsg.content (string or first text block)

Step 2: Compute build hash
  SALT = "59cf53e54c78"
  VERSION = "2.1.123"
  chars = [text[4], text[7], text[20]].map(c => c || "0").join("")
  hash = SHA256(SALT + chars + VERSION).hex.slice(0, 3)

Step 3: Build header string
  "cc_version=${VERSION}.${hash}; cc_entrypoint=cli; cch=00000;"

Step 4: Inject as system[0]
  system.unshift({"type": "text", "text": "x-anthropic-billing-header: " + headerStr})

Step 5: Inject identity as system[1]
  system.splice(1, 0, {"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."})
```

### Sidecar Architecture

```
┌─────────────────────────────────────────────────┐
│ arl-gateway container                            │
│                                                  │
│ ┌────────────────────┐ ┌────────────────────┐   │
│ │ Go Gateway (:8080) │ │ Node.js Sidecar    │   │
│ │                    │ │ (:8081)             │   │
│ │ - HTTP routing     │──▶│ - Parse JSON body  │   │
│ │ - Profile resolve  │ │ - Inject billing    │   │
│ │ - Rate limiting    │ │ - Inject identity   │   │
│ │ - Privacy masking  │ │ - Forward headers   │   │
│ │                    │ │ - HTTPS to Anthro   │   │
│ └────────────────────┘ └────────────────────┘   │
│                                                  │
│ Entrypoint: /app/sidecar/entrypoint.sh           │
│ Starts both processes, waits for either to exit  │
└─────────────────────────────────────────────────┘
```

### Files

| File | Purpose |
|------|---------|
| `api-gateway/sidecar/index.js` | Node.js proxy (~170 lines, zero dependencies) |
| `api-gateway/sidecar/entrypoint.sh` | Starts Go + Node processes |
| `api-gateway/sidecar/package.json` | No dependencies (built-in modules only) |
| `api-gateway/Dockerfile` | Multi-stage build, `apk add nodejs`, copies sidecar/ |
| `api-gateway/handler/handler.go` | Profile routing, transparent detection, header fix |
| `api-gateway/proxy/anthropic.go` | `ProxySidecar()` method, forwards to sidecar |

### Config Env Vars

| Env Var | Default | Description |
|---------|---------|-------------|
| `CLI_SIDECAR_ENABLED` | `true` | Enable/disable sidecar routing |
| `CLI_SIDECAR_URL` | `http://127.0.0.1:8081` | Sidecar URL (same container) |
| `SIDECAR_PORT` | `8081` | Node.js sidecar listen port |

### Error Codes and Causes

| HTTP | Error Message | Cause | Fix |
|------|--------------|-------|-----|
| 401 | `invalid x-api-key` | OAuth token sent as `x-api-key` header | Use `Authorization: Bearer` instead |
| 401 | `OAuth authentication is currently not supported` | Missing `oauth-2025-04-20` in `anthropic-beta` | Add the beta flag |
| 401 | `Invalid bearer token` | Token expired or revoked | Re-auth via gateway OAuth flow |
| 400 | `reserved keyword` | Billing header rejected by Anthropic | TLS fingerprint mismatch (sidecar fixes this) |
| 404 | `not_found_error: model: X` | Wrong model name | Use `claude-sonnet-4-20250514`, not `claude-sonnet-4-6-20250514` |
| 429 | `rate_limit_error` | Rate limit exceeded (generic OAuth bucket) | Must route through sidecar for billing header |
| 502 | (empty) | Gateway panic (slice bounds) | Fixed with `truncate()` helper in proxy |

### Setup: CLI on Remote Machine

```bash
# 1. Gateway OAuth flow (one-time, via browser)
# Open: http://192.168.5.62:9000/v1/auth/claude-oauth/start-url
# Click authorize -> token stored in Redis

# 2. Create profile connected to claude-oauth
curl -X POST http://192.168.5.62:9000/v1/profiles \
  -H "Content-Type: application/json" \
  -d '{"name": "th15011880", "target": "claude-oauth"}'

# 3. Generate profile API token
# Dashboard UI -> Profiles -> th15011880 -> Generate API Key
# Returns: arl_2f3a72a7eb07b4c43ffe87d8c19776eecf62c4c64e30285eee0796198bc91be1

# 4. Configure CLI on remote machine
# Option A: Environment variables
export ANTHROPIC_BASE_URL=http://192.168.5.62:9000
export ANTHROPIC_API_KEY=arl_2f3a72a7eb07b4c43ffe87d8c19776eecf62c4c64e30285eee0796198bc91be1
claude

# Option B: settings.json
# ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://192.168.5.62:9000",
    "ANTHROPIC_API_KEY": "arl_2f3a72a7..."
  }
}

# 5. Test
claude -p "Say hello" --model claude-sonnet-4-20250514
claude -p "Say hello" --model claude-opus-4-20250514
```

### Test with curl

```bash
curl -X POST http://192.168.5.62:9000/v1/messages \
  -H "x-api-key: arl_2f3a72a7eb07b4c43ffe87d8c19776eecf62c4c64e30285eee0796198bc91be1" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 64,
    "messages": [{"role": "user", "content": "Say hi in 3 words"}]
  }'
# -> 200 {"content":[{"type":"text","text":"Hello there, human!"}]}
```

### Fallback Behavior (3-Path Routing)

```
trySidecarOrDirect()
│
├─ Path 1: Go billing injection (InjectBillingHeader + ProxyTransparent)
│   ├─ Success (200) -> done, metrics: go_direct
│   └─ 400 "reserved keyword" -> ErrBillingRejected -> try Path 2
│
├─ Path 2: Sidecar (ProxySidecar -> Node.js)
│   ├─ Success (200) -> done, metrics: sidecar
│   └─ Failure -> try Path 3
│
└─ Path 3: Direct proxy (ProxyTransparent, no billing header)
    ├─ Success (200) -> done, metrics: direct
    └─ Goes to generic OAuth bucket -> more 429s expected
```

---

*Back to [Manual](../MANUAL.md)*
