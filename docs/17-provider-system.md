# 02 - Provider System

## Architecture Overview

```
                              +-------------------+
                              |   HTTP Request    |
                              |  POST /v1/messages |
                              +--------+----------+
                                       |
                                       v
                              +--------+----------+
                              |     Resolver      |
                              |  Model -> Provider |
                              |   + Route Table   |
                              +---+----------+----+
                                  |          |
                   +--------------+          +--------------+
                   |                         |              |
                   v                         v              v
          +--------+------+        +--------+------+  +----+----+
          |   Anthropic   |        |   OpenAI      |  | Gemini  |
          | /v1/messages  |        | /v1/chat/     |  | v1beta/ |
          | Format        |        | completions   |  | models  |
          +--------+------+        +--------+------+  +----+----+
                   |                         |              |
                   v                         v              v
          +--------+------+        +--------+------+  +----+----+
          | anthropic      |        | openai        |  | gemini  |
          | claude-oauth   |        | copilot       |  | gemini- |
          | zai            |        | openrouter    |  | oauth   |
          | agy            |        | deepseek      |  +----+----+
          | custom-*       |        | kimi          |       |
          +----------------+        | huggingface   |       |
                                    | ollama        |       |
                                    | qwen          |       |
                                    | cursor        |       |
                                    | codebuddy     |       |
                                    | kilo          |       |
                                    | lotus         |       |
                                    +---------------+       |
                                                            |
                              +-----------------------------+
                              |
                              v
                     +--------+--------+
                     |   AuthHandler   |
                     |  Auth Sessions  |
                     |  Token CRUD     |
                     |  OAuth Flows    |
                     +--------+--------+
                              |
                    +---------+---------+
                    |                   |
                    v                   v
             +------+------+   +-------+------+
             | TokenStore   |   | RefreshWorker|
             | (Redis)      |   | (30m cycle)  |
             +--------------+   +--------------+
```

The provider system has six core components:

| Component | File | Purpose |
|---|---|---|
| Registry | `provider/registry.go` | Provider catalog, CRUD, persistence |
| Resolver | `provider/resolver.go` | Model-to-provider routing, format selection |
| AuthHandler | `provider/handler.go` | HTTP endpoints for auth, tokens, accounts |
| TokenStore | `provider/token-store.go` | Redis-backed token CRUD, pools, defaults |
| RefreshWorker | `provider/token-refresh.go` | OAuth token refresh, cleanup, project resolution |
| OAuth flows | `provider/oauth_device.go`, `oauth_authcode.go` | Device code and authorization code with PKCE |

---

## 1. Provider Registry

### ProviderConfig struct

```go
type ProviderConfig struct {
    ID             string   // unique identifier, e.g. "anthropic", "claude-oauth"
    Name           string   // display name, e.g. "Anthropic", "Claude (OAuth)"
    AuthType       AuthType // "api_key", "device_code", "auth_code", "session_cookie"
    TokenURL       string   // OAuth token exchange endpoint
    AuthURL        string   // OAuth authorization endpoint
    DeviceCodeURL  string   // device code initiation endpoint
    UserInfoURL    string   // user info endpoint (email fetch)
    CallbackPort   int      // unused in current impl
    Scopes         []string // OAuth scopes
    ClientID       string   // OAuth client ID
    ClientSecret   string   // OAuth client secret (empty for PKCE-only)
    UpstreamBase   string   // base URL for API requests
}
```

### AuthType enum

```go
type AuthType string

const (
    AuthTypeAPIKey        AuthType = "api_key"         // static API key
    AuthTypeDeviceCode    AuthType = "device_code"     // GitHub-style device flow
    AuthTypeAuthCode      AuthType = "auth_code"       // OAuth2 auth code + PKCE
    AuthTypeSessionCookie AuthType = "session_cookie"  // browser session cookie
)
```

### Registry struct

```go
type Registry struct {
    providers map[string]ProviderConfig
}
```

Thread-safe via internal mutex on `AuthHandler`. The registry itself is not concurrent-safe for writes -- mutations go through `AuthHandler` which holds its own lock.

### Built-in Providers

The `NewRegistry()` constructor registers 19 providers:

| ID | Name | Auth Type | Upstream Base (default) |
|---|---|---|---|
| `anthropic` | Anthropic | `api_key` | `https://api.anthropic.com` |
| `claude-oauth` | Claude (OAuth) | `auth_code` | `https://api.anthropic.com` |
| `gemini` | Google Gemini | `api_key` | `https://generativelanguage.googleapis.com` |
| `gemini-oauth` | Google Gemini (OAuth) | `auth_code` | `https://cloudcode-pa.googleapis.com` |
| `openai` | OpenAI | `api_key` | `https://api.openai.com` |
| `copilot` | GitHub Copilot | `device_code` | `https://api.github.com/copilot` |
| `zai` | Z.AI | `api_key` | `https://api.z.ai/api/anthropic` |
| `lotus` | Lotus LLM | `api_key` | `https://api-cpxis.lotuss.com/llm` |
| `openrouter` | OpenRouter | `api_key` | `https://openrouter.ai/api` |
| `qwen` | Qwen (Aliyun) | `device_code` | `https://dashscope.aliyuncs.com` |
| `deepseek` | DeepSeek | `api_key` | `https://api.deepseek.com` |
| `kimi` | Kimi (Moonshot) | `api_key` | `https://api.moonshot.cn/v1` |
| `huggingface` | Hugging Face | `api_key` | `https://api-inference.huggingface.co/models` |
| `ollama` | Ollama | `api_key` | `http://localhost:11434` |
| `agy` | Antigravity | `api_key` | `https://antigravity.com` |
| `cursor` | Cursor | `api_key` | `https://api2.cursor.sh` |
| `codebuddy` | CodeBuddy | `api_key` | `https://api.codebuddy.io` |
| `kilo` | Kilo | `api_key` | `https://api.kilo.ai` |

Each upstream base is overridable via environment variable (e.g. `ANTHROPIC_UPSTREAM_BASE`, `ZAI_UPSTREAM_BASE`).

### Custom Providers

Custom providers can be created at runtime via `POST /providers/custom`. They:
- Get a generated ID with `custom-` prefix (e.g. `custom-a3f7b2`)
- Support `openai` or `anthropic` format (defaults to `anthropic`)
- Are persisted in Redis under key `arl:providers:custom:{id}`
- Are loaded on startup via `LoadCustomProviders()`
- Cascade deletion: removing a custom provider also removes its tokens and profile references

### Registry Methods

| Method | Signature | Description |
|---|---|---|
| `Get` | `(id string) (ProviderConfig, bool)` | Lookup by ID |
| `List` | `() []ProviderConfig` | All providers |
| `Register` | `(cfg ProviderConfig)` | Add to in-memory map |
| `Delete` | `(id string) bool` | Delete custom providers only |
| `IsCustom` | `(id string) bool` | Check `custom-` prefix |
| `UpdateUpstream` | `(id, upstream string) bool` | Runtime upstream override |
| `PersistCustom` | `(rdb, cfg) error` | Save to Redis |
| `RemovePersisted` | `(rdb, id) error` | Delete from Redis |
| `LoadCustomProviders` | `(rdb)` | Load all from Redis on startup |

---

## 2. Provider Route Table and Format System

### ProviderFormat

```go
type ProviderFormat string

const (
    FormatAnthropic ProviderFormat = "anthropic"  // /v1/messages, Claude API format
    FormatOpenAI    ProviderFormat = "openai"     // /v1/chat/completions, OpenAI format
    FormatGemini    ProviderFormat = "gemini"     // v1beta/models/...:streamGenerateContent
)
```

### providerRoute struct

```go
type providerRoute struct {
    format       ProviderFormat     // request/response format
    authMode     string             // "api_key" or "bearer"
    urlSuffix    string             // appended to UpstreamBase
    extraHeaders map[string]string  // injected into upstream requests
    modelOverride string            // override model name (e.g. "default" for lotus)
    maxTokens    int                // cap max_tokens in requests (0 = no cap)
}
```

### Full Route Table

| Provider ID | Format | Auth Mode | URL Suffix | Extra Headers | Special |
|---|---|---|---|---|---|
| `anthropic` | Anthropic | `api_key` | `/v1/messages` | none | - |
| `claude-oauth` | Anthropic | `api_key` | `/v1/messages?beta=true` | `anthropic-beta`, `x-app`, `User-Agent`, `X-Stainless-*` headers | Full Claude Code CLI header set |
| `claude` | Anthropic | `api_key` | `/v1/messages?beta=true` | Same as `claude-oauth` | Alias |
| `zai` | Anthropic | `api_key` | `/v1/messages` | none | Z.AI Anthropic-compatible endpoint |
| `agy` | Anthropic | `api_key` | `/v1/messages` | none | - |
| `openai` | OpenAI | `bearer` | `/v1/chat/completions` | none | - |
| `copilot` | OpenAI | `bearer` | `/v1/chat/completions` | none | - |
| `openrouter` | OpenAI | `bearer` | `/v1/chat/completions` | `HTTP-Referer: https://github.com/klxhunter/agent-rate-limit` | - |
| `qwen` | OpenAI | `bearer` | `/compatible-mode/v1/chat/completions` | none | Aliyun compatible mode |
| `deepseek` | OpenAI | `bearer` | `/v1/chat/completions` | none | - |
| `kimi` | OpenAI | `bearer` | `/v1/chat/completions` | none | - |
| `huggingface` | OpenAI | `bearer` | `/v1/chat/completions` | none | - |
| `ollama` | OpenAI | `bearer` | `/v1/chat/completions` | none | - |
| `cursor` | OpenAI | `bearer` | `/v1/chat/completions` | none | - |
| `codebuddy` | OpenAI | `bearer` | `/v1/chat/completions` | none | - |
| `kilo` | OpenAI | `bearer` | `/v1/chat/completions` | none | - |
| `lotus` | OpenAI | `bearer` | `/v1/chat/completions` | none | `modelOverride: "default"`, `maxTokens: 4096`, `MaxContinuations: 3`, `ToolMode: "native"` |
| `gemini` | Gemini | `api_key` | (dynamic) | none | URL built as `{base}/v1beta/models/{model}:streamGenerateContent?key={apiKey}` |
| `gemini-oauth` | Gemini | `bearer` | (dynamic) | none | Same URL pattern, Bearer auth |

### claude-oauth Extra Headers

These headers mimic the Claude Code CLI to enable OAuth-based inference:

```
anthropic-beta: claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,...
x-app: cli
User-Agent: claude-cli/2.1.123 (external, cli)
anthropic-dangerous-direct-browser-access: true
Accept: application/json
X-Stainless-Lang: js
X-Stainless-Package-Version: 0.81.0
X-Stainless-OS: MacOS
X-Stainless-Arch: arm64
X-Stainless-Runtime: node
X-Stainless-Runtime-Version: v24.3.0
X-Stainless-Retry-Count: 0
X-Stainless-Timeout: 3000
```

The `anthropic-beta` header includes these beta features:
- `claude-code-20250219` - Claude Code mode
- `oauth-2025-04-20` - OAuth authentication
- `interleaved-thinking-2025-05-14` - interleaved thinking
- `redact-thinking-2026-02-12` - redact thinking output
- `context-management-2025-06-27` - context window management
- `prompt-caching-scope-2026-01-05` - prompt caching
- `advanced-tool-use-2025-11-20` - advanced tool use
- `effort-2025-11-24` - effort control

### Provider-Specific Features

**Auto-continuations** (`providerContinuations`):
When a provider returns `finish_reason: "length"`, the gateway automatically sends a continuation request up to `MaxContinuations` times.

| Provider | Max Continuations |
|---|---|
| `lotus` | 3 |

**Tool mode** (`providerToolMode`):
`"native"` = use OpenAI function calling format, convert `tool_calls` to Anthropic `tool_use`.

| Provider | Tool Mode |
|---|---|
| `lotus` | `native` |

### Dynamic Route Registration

```go
func RegisterProviderRoute(providerID string, format ProviderFormat)
```

Called when creating custom providers. Adds an entry to `providerRouteTable` at runtime.

---

## 3. Resolver: Model-to-Provider Routing

### RoutingDecision struct

```go
type RoutingDecision struct {
    ProviderID       string
    ProviderCfg      ProviderConfig
    Format           ProviderFormat   // anthropic, openai, gemini
    UpstreamURL      string           // fully constructed URL
    AuthMode         string           // "api_key" or "bearer"
    ExtraHeaders     map[string]string
    APIKey           string
    AccountID        string
    ModelOverride    string           // force model name (e.g. "default")
    MaxTokens        int              // cap max_tokens (0 = no cap)
    MaxContinuations int              // auto-continuation limit
    ToolMode         string           // "", "native"
}
```

### Model Routing Rules

Models are matched by prefix, in order of priority. The first matching rule wins.

| Model Prefix | Provider Priority (ordered) |
|---|---|
| `claude-` | `claude-oauth` -> `anthropic` |
| `gpt-` | `openai` |
| `o1-` | `openai` |
| `o3-` | `openai` |
| `o4-` | `openai` |
| `gemini-` | `gemini-oauth` -> `gemini` |
| `glm-` | `zai` |
| `qwen-` | `qwen` |
| `or-` | `openrouter` |
| `anthropic/` | `anthropic` -> `openrouter` |
| `openai/` | `openrouter` |
| `google/` | `openrouter` |
| `meta/` | `openrouter` |
| `deepseek/` | `openrouter` |
| `qwen/` | `openrouter` |
| `deepseek-` | `deepseek` |
| `kimi-` | `kimi` |
| `huggingface/` | `huggingface` |
| `ollama` | `ollama` |
| `agy-` | `agy` |
| `lotus-` | `lotus` |

### Resolver struct

```go
type Resolver struct {
    registry   *Registry
    tokenStore *TokenStore
    glmMode    bool
    counters   sync.Map   // map[string]*atomic.Uint64, round-robin counters
    cooldowns  sync.Map   // map[string]time.Time, rate-limit cooldowns
}
```

### Resolution Algorithm

```
Resolve(model):
  1. For each modelRule, check if model has prefix match
  2. For each provider in the rule's priority list:
     a. Check cooldown (skip if rate-limited)
     b. For claude-oauth: use tryResolveRoundRobin()
        For others: use tryResolve()
     c. If token found and not expired, build RoutingDecision
  3. If GLM mode and no provider found:
     a. If model matches a rule with "zai", build decision with empty key
     b. If no rule matches, fall through to Z.AI with empty key
  4. Return nil if nothing found
```

### Key Resolver Methods

| Method | Purpose |
|---|---|
| `Resolve(model) *RoutingDecision` | Primary resolution, returns first available provider |
| `ResolveFallback(model, exclude) *RoutingDecision` | Skip providers in exclude list, used for retry |
| `ResolveByProvider(providerID) (*RoutingDecision, bool)` | Direct provider lookup |
| `ResolveTransparent(model) *RoutingDecision` | Claude OAuth passthrough, no token check |
| `MarkCooldown(providerID, duration, model...)` | Mark provider+model as rate-limited |
| `ModelBelongsToProvider(model, providerID) bool` | Check if model routes to a specific provider |

### Token Selection Strategies

**Single token (most providers)**:
`tryResolve()` fetches the default (or first non-paused) token via `TokenStore.GetDefault()`.

**Round-robin with utilization awareness (claude-oauth)**:
`tryResolveRoundRobin()` cycles through all active tokens for a provider:
1. List all non-paused, non-expired tokens
2. If multiple accounts exist, check 5h utilization from Redis (`arl:ratelimit:{provider}:{accountID}`)
3. Partition into low-util (<0.8) and high-util pools
4. Prefer low-util pool; fall back to high-util if all are high
5. Atomic counter-based round-robin within the selected pool

### Cooldown System

When a provider returns 429, `MarkCooldown()` is called with a duration. Subsequent `Resolve()` calls skip that provider+model combination until the cooldown expires. Cooldown keys can be provider-only or provider+model for granular backoff.

---

## 4. Token Store

### TokenInfo struct

```go
type TokenInfo struct {
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token,omitempty"`
    ExpiryDate   time.Time `json:"expiry_date"`
    Email        string    `json:"email,omitempty"`
    AccountID    string    `json:"account_id"`
    Provider     string    `json:"provider"`
    ProjectID    string    `json:"project_id,omitempty"`
    Tier         string    `json:"tier,omitempty"`
    Paused       bool      `json:"paused"`
    IsDefault    bool      `json:"is_default"`
    CreatedAt    time.Time `json:"created_at"`
    Scopes       string    `json:"scopes,omitempty"`
}
```

### Redis Key Schema

| Pattern | Description |
|---|---|
| `arl:tokens:{provider}:{accountID}` | Serialized TokenInfo JSON |
| `arl:tokens:{provider}:_index` | Redis SET of account IDs for the provider |
| `arl:ratelimit:{provider}:{accountID}` | Cached rate limit status (`AcctRateLimit`) |
| `arl:providers:custom:{id}` | Serialized custom ProviderConfig |

### Token Store Methods

| Method | Signature | Description |
|---|---|---|
| `Store` | `(token TokenInfo) error` | Upsert token + add to provider index |
| `Get` | `(provider, accountID) (*TokenInfo, error)` | Single token lookup |
| `Delete` | `(provider, accountID) error` | Remove token + update index |
| `DeleteByProvider` | `(provider) error` | Remove all tokens for a provider |
| `ListByProvider` | `(provider) ([]TokenInfo, error)` | Pipeline GET for all tokens in provider |
| `ListAll` | `() ([]TokenInfo, error)` | SCAN all tokens across providers |
| `GetDefault` | `(provider) (*TokenInfo, error)` | Default token, fallback to first non-paused |
| `SetDefault` | `(provider, accountID) error` | Toggle default (clear all if already default) |
| `Pause` | `(provider, accountID) error` | Set paused=true |
| `Resume` | `(provider, accountID) error` | Set paused=false |
| `UpdateEmail` | `(provider, accountID, email) error` | Update email field |
| `GetFromPool` | `(provider, accountIDs) (*TokenInfo, error)` | Select from given account IDs with utilization-based selection |
| `GetRateLimits` | `(provider, accountIDs) map[string]AcctRateLimit` | Pipeline GET for rate limit cache |

### Provider Rename Migration

On startup, `MigrateProviderRenames()` copies tokens from old provider IDs to new ones. Currently maps:

```
"claude" -> "claude-oauth"
```

### Account Pool Selection

`GetFromPool()` implements smart account selection for multi-account profiles:
1. Pipeline GET all candidate tokens
2. Filter out paused accounts
3. Fetch `AcctRateLimit` for all candidates via pipeline
4. Pick account with lowest `Util5h` (5-hour utilization)
5. Among ties, pick randomly
6. If no rate limit data, pick randomly among all candidates

---

## 5. OAuth Flows

### Device Code Flow

Used by: `copilot`, `qwen`

```
Client                 Gateway                    Provider
  |                       |                          |
  | POST /auth/{p}/start |                          |
  |---------------------->|                          |
  |                       | POST device_code_url     |
  |                       |------------------------->|
  |                       | {device_code, user_code} |
  |                       |<-------------------------|
  | {user_code, url,      |                          |
  |  device_code, state}  |                          |
  |<----------------------|                          |
  |                       |                          |
  | (User visits URL,     |                          |
  |  enters code)         |                          |
  |                       |                          |
  | GET /auth/{p}/status?state=...                   |
  |---------------------->|                          |
  |                       | POST token_url (poll)    |
  |                       |------------------------->|
  |                       | {pending} or {token}     |
  |                       |<-------------------------|
  | {status: "pending"}   |                          |
  |<----------------------|                          |
  |                       |                          |
  | (repeat poll)         |                          |
  |---------------------->|                          |
  |                       | POST token_url (poll)    |
  |                       |------------------------->|
  |                       | {access_token}           |
  |                       |<-------------------------|
  | {status: "complete"}  |                          |
  |<----------------------|                          |
```

**Key details**:
- `StartDeviceCode()` POSTs `client_id` + `scope` to `DeviceCodeURL`
- `PollDeviceToken()` uses `urn:ietf:params:oauth:grant-type:device_code` grant type
- Returns `authorization_pending` or `slow_down` while waiting (mapped to nil, nil)
- Device tokens get a 90-day expiry
- Account ID derived from last 12 chars of access token: `{provider}_{suffix}`

### Authorization Code Flow with PKCE

Used by: `claude-oauth`, `gemini-oauth`

```
Client                 Gateway                    Provider
  |                       |                          |
  | POST /auth/{p}/start |                          |
  |---------------------->|                          |
  |                       | Generate state + PKCE    |
  |                       | Build auth_url           |
  | {auth_url, state,     |                          |
  |  client_id, redirect} |                          |
  |<----------------------|                          |
  |                       |                          |
  | (User opens auth_url, |                          |
  |  authorizes, callback)|                          |
  |                       |                          |
  | GET /auth/{p}/callback?code=...&state=...        |
  |---------------------->|                          |
  |                       | POST token_url           |
  |                       | {code, verifier, id}     |
  |                       |------------------------->|
  |                       | {access, refresh, id_token}
  |                       |<-------------------------|
  |                       | GET user_info_url        |
  |                       | (if configured)          |
  |                       | Store TokenInfo          |
  | Redirect to dashboard |                          |
  |<----------------------|                          |
```

**PKCE generation**:
```go
// code_verifier: 32 random bytes, base64url-encoded (43 chars)
// code_challenge: BASE64URL(SHA256(code_verifier))
```

**Redirect URI logic**:
- Google (`gemini-oauth`): `{OAUTH_CALLBACK_BASE}/v1/auth/gemini-oauth/callback`
- Claude (`claude-oauth`): `http://localhost:8765/callback` (PKCE-only, no client_secret, matches Claude Code CLI registration)
- For remote servers: user manually pastes callback URL via `POST /auth/{p}/callback` with JSON body

**Token exchange differences**:

| Provider | Content-Type | Body Format | Special |
|---|---|---|---|
| `claude-oauth` | `application/json` | JSON `{grant_type, code, redirect_uri, client_id, code_verifier, state}` | PKCE-only, no client_secret |
| `gemini-oauth` | `application/x-www-form-urlencoded` | Form-encoded + `code_verifier` + `client_secret` | `access_type=offline`, `prompt=consent` |

**Email resolution**:
1. If `UserInfoURL` is set, fetch email via `GET {UserInfoURL}` with Bearer token
2. Fallback: extract email from `id_token` JWT payload (base64 decode, no signature verification)

---

## 6. Token Refresh Worker

### RefreshWorker struct

```go
type RefreshWorker struct {
    store    *TokenStore
    registry *Registry
    interval time.Duration  // 30 minutes
    stop     chan struct{}
}
```

### Refresh Cycle

```
Start():
  refreshAll() immediately    <-- critical for first request after restart
  ticker every 30 minutes:
    refreshAll()

refreshAll():
  ListAll() tokens
  threshold = now + 45 minutes

  For each token:
    Skip if paused

    If expired AND no refresh_token:
      Delete (cleanup stale token)

    If expired AND provider removed:
      Delete (cleanup orphaned token)

    If expired AND has refresh_token AND provider is auth_code:
      refreshToken() with 3 retries, exponential backoff (5s, 10s, 20s)

    If not expired AND approaching threshold (<45 min):
      AND is auth_code type:
        refreshToken()

  resolveMissingProjects()
```

### Per-provider Refresh Protocol

**Claude OAuth**:
```
POST https://api.anthropic.com/v1/oauth/token
Content-Type: application/json

{
  "grant_type": "refresh_token",
  "refresh_token": "...",
  "client_id": "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
}
```

**Google Gemini OAuth** (and other standard OAuth2):
```
POST https://oauth2.googleapis.com/token
Content-Type: application/x-www-form-urlencoded

grant_type=refresh_token&refresh_token=...&client_id=...&client_secret=...
```

### Retry Logic

3 attempts with exponential backoff:
- Attempt 1: fail immediately
- Attempt 2: wait 5s
- Attempt 3: wait 10s
- Attempt 4 (if reached): wait 20s, then return error

### Google CodeAssist Project Resolution

After each refresh cycle, `resolveMissingProjects()` runs:

1. List all `gemini-oauth` tokens missing `ProjectID`
2. For each: call `loadCodeAssistProject()`
   - `POST {UpstreamBase}/v1internal:loadCodeAssist` with `Authorization: Bearer {token}`
   - Extract `cloudaicompanionProject` from response
3. If empty: call `onboardAndLoad()`
   - `POST {UpstreamBase}/v1internal:onboardUser` with tier `"free-tier"`
   - Handles Long Running Operations (LRO), polls up to 3 times with 5s interval
4. Store resolved `ProjectID` in TokenInfo

### On-Demand Refresh

`RefreshOne(providerID, accountID)` allows manual refresh via `POST /auth/accounts/{provider}/{accountId}/refresh`.

---

## 7. Auth Handler HTTP Routes

### Route Table

| Method | Path | Handler | Purpose |
|---|---|---|---|
| POST | `/auth/{provider}/start` | `StartAuth` | Initiate OAuth flow |
| POST | `/auth/{provider}/start-url` | `StartAuthURL` | Same as start |
| POST | `/auth/{provider}/register` | `RegisterAPIKey` | Register API key or session cookie |
| GET | `/auth/{provider}/callback` | `HandleCallback` | OAuth callback redirect |
| POST | `/auth/{provider}/callback` | `HandleCallbackPost` | Manual code exchange (remote) |
| GET | `/auth/{provider}/status` | `PollStatus` | Poll device code / auth status |
| POST | `/auth/{provider}/cancel` | `CancelAuth` | Cancel pending auth session |
| GET | `/auth/accounts` | `ListAccounts` | List all tokens |
| GET | `/auth/accounts/{provider}` | `ListAccounts` | List tokens for provider |
| DELETE | `/auth/accounts/{provider}/{accountId}` | `RemoveAccount` | Delete token + cascade from profiles |
| POST | `/auth/accounts/{provider}/{accountId}/pause` | `PauseAccount` | Pause account |
| POST | `/auth/accounts/{provider}/{accountId}/resume` | `ResumeAccount` | Resume account |
| POST | `/auth/accounts/{provider}/{accountId}/default` | `SetDefaultAccount` | Set default token |
| POST | `/auth/accounts/{provider}/{accountId}/email` | `UpdateAccountEmail` | Update email |
| POST | `/auth/accounts/{provider}/{accountId}/refresh` | `RefreshAccount` | On-demand token refresh |
| POST | `/auth/login` | `DashboardLogin` | Dashboard auth (API key -> cookie) |
| POST | `/auth/logout` | `DashboardLogout` | Clear session cookie |
| GET | `/auth/check` | `CheckAuth` | Check auth status |
| GET | `/providers` | `ListProviders` | List all providers |
| PUT | `/providers/{provider}/upstream` | `UpdateProviderUpstream` | Runtime upstream change |
| POST | `/providers/custom` | `CreateCustomProvider` | Create custom provider |
| DELETE | `/providers/custom/{provider}` | `DeleteCustomProvider` | Delete custom provider + cascade |

### Auth Session Management

```go
type AuthSession struct {
    Provider     string     // provider ID
    State        string     // OAuth state parameter
    DeviceCode   string     // device code (device_code flow)
    PKCEVerifier string     // PKCE code_verifier (auth_code flow)
    Type         string     // "device_code" or "auth_code"
    Token        *TokenInfo // populated when auth completes
    StartedAt    time.Time
}
```

Sessions are stored in-memory (`map[string]*AuthSession`) protected by `sync.Mutex`. State strings serve as map keys.

### Dashboard Authentication

If `DASHBOARD_PASSWORD` env var is set:
- Login via `POST /auth/login` with `{api_key: "..."}` sets an `arl_session` cookie
- Auth check supports both `x-api-key` header and `arl_session` cookie
- Cookie: HttpOnly, SameSite=Lax, 30-day max age
- If `DASHBOARD_PASSWORD` is empty, all requests are treated as authenticated

### Account Cascade on Deletion

When an account is removed:
1. Delete from TokenStore
2. Scan all `profile:*` keys in Redis
3. Remove the accountID from `accountIds` array in each profile

When a custom provider is deleted:
1. Remove from registry
2. Remove from route table
3. Delete all provider tokens
4. Scan profiles, clear `target`/`provider` fields and remove matching targets from `targets` array
5. Remove from Redis persistence

---

## 8. Provider-Specific Details

### Anthropic (Direct API Key)

| Aspect | Value |
|---|---|
| Auth | `x-api-key` header |
| Endpoint | `POST /v1/messages` |
| Format | Anthropic native |
| Token counting | Native Anthropic token counting |
| Streaming | SSE with `event: message_start`, `content_block_delta`, etc. |
| Models | `claude-*` prefix |

### Claude (OAuth)

| Aspect | Value |
|---|---|
| Auth | `x-api-key: Bearer {access_token}` + `anthropic-beta: oauth-2025-04-20` |
| Endpoint | `POST /v1/messages?beta=true` |
| Format | Anthropic native with beta features |
| Client ID | `9d1c250a-e61b-44d9-88ed-5944d1962f5e` (extracted from Claude Code CLI) |
| Auth URL | `https://claude.com/cai/oauth/authorize` |
| Token URL | `https://api.anthropic.com/v1/oauth/token` |
| Redirect URI | `http://localhost:8765/callback` |
| PKCE | Yes (S256), no client_secret |
| Token refresh | JSON body, every 30 min, 45 min threshold |
| Round-robin | Yes, with utilization-aware account selection |
| Models | `claude-*` prefix, highest priority |

### Google Gemini (API Key)

| Aspect | Value |
|---|---|
| Auth | Query param `?key={apiKey}` |
| Endpoint | `GET /v1beta/models/{model}:streamGenerateContent?key={apiKey}` |
| Format | Gemini native |
| Models | `gemini-*` prefix, lower priority than gemini-oauth |

### Google Gemini (OAuth / CodeAssist)

| Aspect | Value |
|---|---|
| Auth | `Authorization: Bearer {token}` |
| Endpoint | Same Gemini format, Bearer auth |
| Auth URL | `https://accounts.google.com/o/oauth2/v2/auth` |
| Token URL | `https://oauth2.googleapis.com/token` |
| Redirect URI | `{OAUTH_CALLBACK_BASE}/v1/auth/gemini-oauth/callback` |
| PKCE | Yes (S256) + client_secret (installed app) |
| Scopes | `cloud-platform`, `userinfo.email`, `userinfo.profile` |
| Upstream | `https://cloudcode-pa.googleapis.com` (not generativelanguage.googleapis.com) |
| Project resolution | `loadCodeAssist` -> `onboardUser` -> LRO poll |
| Models | `gemini-*` prefix, higher priority than API key |

### OpenAI

| Aspect | Value |
|---|---|
| Auth | `Authorization: Bearer {apiKey}` |
| Endpoint | `POST /v1/chat/completions` |
| Format | OpenAI Chat Completions |
| Models | `gpt-*`, `o1-*`, `o3-*`, `o4-*` |

### Z.AI / GLM

| Aspect | Value |
|---|---|
| Auth | `x-api-key` header |
| Endpoint | `POST /v1/messages` (Anthropic-compatible) |
| Format | Anthropic native |
| Models | `glm-*` prefix |
| Fallback | In GLM mode, all unmatched models route to Z.AI |
| OpenAI compat | Separate `ZAI_OPENAI_URL` for models in `ZAI_OPENAI_MODELS` set |

### OpenRouter

| Aspect | Value |
|---|---|
| Auth | `Authorization: Bearer {apiKey}` |
| Endpoint | `POST /v1/chat/completions` |
| Format | OpenAI Chat Completions |
| Extra Header | `HTTP-Referer: https://github.com/klxhunter/agent-rate-limit` |
| Models | `or-*` prefix, also `anthropic/*`, `openai/*`, `google/*`, `meta/*`, `deepseek/*`, `qwen/*` |

### GitHub Copilot

| Aspect | Value |
|---|---|
| Auth | Device code flow -> Bearer token |
| Device Code URL | `https://github.com/login/device/code` |
| Token URL | `https://github.com/login/oauth/access_token` |
| Client ID | `Iv1.b507a08c87ecfe98` |
| Endpoint | `POST /v1/chat/completions` |
| Format | OpenAI Chat Completions |

### Lotus LLM

| Aspect | Value |
|---|---|
| Auth | `Authorization: Bearer {apiKey}` |
| Endpoint | `POST /v1/chat/completions` |
| Format | OpenAI Chat Completions |
| Model Override | All models mapped to `"default"` |
| Max Tokens | Capped at 4096 |
| Auto-continuations | Up to 3 on `finish_reason: "length"` |
| Tool Mode | `native` (OpenAI function calling -> Anthropic tool_use conversion) |

### Qwen (Aliyun)

| Aspect | Value |
|---|---|
| Auth | Device code flow -> Bearer token |
| Endpoint | `POST /compatible-mode/v1/chat/completions` |
| Format | OpenAI Chat Completions |
| Models | `qwen-*` prefix |

### DeepSeek

| Aspect | Value |
|---|---|
| Auth | API key -> Bearer |
| Endpoint | `POST /v1/chat/completions` |
| Format | OpenAI Chat Completions |
| Models | `deepseek-*` prefix |

### Ollama

| Aspect | Value |
|---|---|
| Auth | API key (usually empty for local) |
| Endpoint | `POST /v1/chat/completions` |
| Format | OpenAI Chat Completions |
| Default | `http://localhost:11434` |

---

## 9. Configuration Reference

### Provider-Related Environment Variables

| Variable | Default | Description |
|---|---|---|
| `ANTHROPIC_UPSTREAM_BASE` | `https://api.anthropic.com` | Anthropic API base |
| `CLAUDE_UPSTREAM_BASE` | `https://api.anthropic.com` | Claude OAuth API base |
| `CLAUDE_OAUTH_CLIENT_ID` | `9d1c250a-...` | Claude OAuth client ID |
| `GEMINI_UPSTREAM_BASE` | `https://generativelanguage.googleapis.com` | Gemini API base |
| `GEMINI_CODEASSIST_BASE` | `https://cloudcode-pa.googleapis.com` | CodeAssist API base |
| `GEMINI_OAUTH_CLIENT_ID` | (empty) | Google OAuth client ID |
| `GEMINI_OAUTH_CLIENT_SECRET` | (empty) | Google OAuth client secret |
| `OPENAI_UPSTREAM_BASE` | `https://api.openai.com` | OpenAI API base |
| `ZAI_UPSTREAM_BASE` | `https://api.z.ai/api/anthropic` | Z.AI Anthropic endpoint |
| `LOTUS_UPSTREAM_BASE` | `https://api-cpxis.lotuss.com/llm` | Lotus API base |
| `OPENROUTER_UPSTREAM_BASE` | `https://openrouter.ai/api` | OpenRouter API base |
| `QWEN_UPSTREAM_BASE` | `https://dashscope.aliyuncs.com` | Qwen API base |
| `QWEN_DEVICE_CODE_URL` | (empty) | Qwen device code URL |
| `QWEN_TOKEN_URL` | (empty) | Qwen token URL |
| `QWEN_CLIENT_ID` | (empty) | Qwen OAuth client ID |
| `QWEN_CLIENT_SECRET` | (empty) | Qwen OAuth client secret |
| `DEEPSEEK_UPSTREAM_BASE` | `https://api.deepseek.com` | DeepSeek API base |
| `KIMI_UPSTREAM_BASE` | `https://api.moonshot.cn/v1` | Kimi API base |
| `HUGGINGFACE_UPSTREAM_BASE` | `https://api-inference.huggingface.co/models` | HF API base |
| `OLLAMA_UPSTREAM_BASE` | `http://localhost:11434` | Ollama API base |
| `AGY_UPSTREAM_BASE` | `https://antigravity.com` | Antigravity API base |
| `CURSOR_UPSTREAM_BASE` | `https://api2.cursor.sh` | Cursor API base |
| `CODEBUDDY_UPSTREAM_BASE` | `https://api.codebuddy.io` | CodeBuddy API base |
| `KILO_UPSTREAM_BASE` | `https://api.kilo.ai` | Kilo API base |
| `COPILOT_CLIENT_ID` | `Iv1.b507a08c87ecfe98` | GitHub Copilot client ID |
| `OAUTH_CALLBACK_BASE` | `http://127.0.0.1:9000` | OAuth redirect URI base |
| `DASHBOARD_URL` | `http://localhost:8082` | Dashboard URL for redirects |
| `DASHBOARD_PASSWORD` | (empty) | Dashboard auth key |
| `REDIS_ADDR` | `dragonfly:6379` | Redis connection |
| `GLM_MODE` | `true` | Enable Z.AI features |

### Config struct (provider-relevant fields)

```go
type Config struct {
    // GLM mode toggle
    GLMMode           bool

    // Z.AI routing
    ZAIOpenAIURL      string            // OpenAI-compatible endpoint
    ZAIOpenAIModels   map[string]bool   // models to route via OpenAI format

    // Z.AI web chat
    ZAIWebEnabled     bool
    ZAIWebToken       string
    ZAIWebModels      []string

    // Provider routing
    ProviderModelPrefixes string

    // Gemini
    GeminiCodeAssistEndpoint string
    GeminiAPIEndpoint        string
    GeminiDefaultModel       string

    // Anthropic
    AnthropicVersion         string
    AnthropicDirectURL       string

    // Multi-key pool
    UpstreamAPIKeys     []string
    UpstreamRPMLimit    int

    // CLI sidecar
    CLISidecarURL       string
    CLISidecarEnabled   bool
}
```

---

## 10. Provider Selection Flow Diagram

```
                    Incoming Request
                    model: "claude-sonnet-4-6"
                          |
                          v
                    +-----+------+
                    |  Resolver  |
                    | .Resolve() |
                    +-----+------+
                          |
                    Match prefix "claude-"
                          |
                    Priority: [claude-oauth, anthropic]
                          |
                    +-----+-----+
                    |           |
                    v           v
            claude-oauth    anthropic
            (round-robin)   (single key)
                    |           |
             Token store     Token store
             has token?      has token?
                    |           |
              YES / \ NO   YES / \ NO
                 /   \         /   \
                v     v       v     v
           Build    Skip   Build   Skip
           Decision         Decision
                |               |
                +-------+-------+
                        |
                        v
                 RoutingDecision
                 {
                   ProviderID: "claude-oauth",
                   Format: "anthropic",
                   UpstreamURL: "https://api.anthropic.com/v1/messages?beta=true",
                   AuthMode: "api_key",
                   APIKey: "Bearer ey...",
                   ExtraHeaders: {...claude-beta headers...}
                 }
                        |
                        v
                 Upstream Request
                 POST /v1/messages?beta=true
                 x-api-key: Bearer ey...
                 anthropic-beta: oauth-2025-04-20,...
                 Body: Anthropic Messages API format
```

### Fallback Example

```
Request: model="claude-haiku-4-5"
  1st attempt: claude-oauth -> 429 rate limit
     -> MarkCooldown("claude-oauth", 30s)
     -> ResolveFallback("claude-haiku-4-5", ["claude-oauth"])
  2nd attempt: anthropic -> 200 OK
```

### GLM Mode Fallback

```
Request: model="unknown-model-xyz"
  No prefix match in modelRules
  GLM mode = true:
    -> tryResolve("zai", "unknown-model-xyz")
    -> No token? buildDecision("zai", ..., "", "")
    -> Uses ZAI_API_KEYS pool or empty
```

---

## 11. Error Handling

### Provider-Level Errors

| Error | Response | Action |
|---|---|---|
| 429 Too Many Requests | Rate limited | `MarkCooldown()` on provider+model, retry with next provider |
| 401 Unauthorized | Token expired | Handled by RefreshWorker proactively (45min threshold) |
| 500 Internal Server Error | Provider error | Retry with exponential backoff (configurable) |
| Token not found | No credential | Resolver returns nil, caller handles "no provider available" |
| Token expired + no refresh_token | Stale token | Auto-deleted by RefreshWorker cleanup |

### Refresh Worker Error Handling

- Failed refresh: log warning, retry next cycle (does not delete token)
- 3 consecutive failures with backoff: return error, keep token for next cycle
- Expired + no refresh_token: auto-delete
- Expired + provider removed: auto-delete
- Network errors: counted as failed, retried next cycle

### OAuth Flow Errors

- Device code `authorization_pending`: return `{status: "pending"}`
- Device code `slow_down`: return `{status: "pending"}` (client should increase poll interval)
- Token exchange failure: redirect to dashboard with `?auth_error=callback_failed`
- Missing client ID: return 400 with env var hint
