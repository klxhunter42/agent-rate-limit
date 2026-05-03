# 02 - Request Handler & Routing Layer

> Generated: 2026-05-03 | Source: `api-gateway/handler/handler.go`, `api-gateway/provider/{registry,resolver,token-refresh,token-store}.go`, `api-gateway/middleware/*`, `api-gateway/config/config.go`

---

## Table of Contents

1. [Request Lifecycle](#1-request-lifecycle)
2. [Authentication Flow](#2-authentication-flow)
3. [Profile Resolution](#3-profile-resolution)
4. [Token Management](#4-token-management)
5. [Rate Limiting](#5-rate-limiting)
6. [Provider Fallback](#6-provider-fallback)
7. [Request Preprocessing](#7-request-preprocessing)
8. [Response Postprocessing](#8-response-postprocessing)
9. [Vision Routing](#9-vision-routing)
10. [Transparent Passthrough](#10-transparent-passthrough)
11. [Health/Admin Endpoints](#11-healthadmin-endpoints)

---

## 1. Request Lifecycle

The primary proxy endpoint is `POST /v1/messages` handled by `Handler.Messages()`. The full lifecycle from incoming request to upstream response:

```
Client Request
    |
    v
[Middleware Chain]
    |-- SecurityHeaders (X-Content-Type-Options, X-Frame-Options, etc.)
    |-- CorrelationID (X-Correlation-ID header)
    |-- RealIP (CF-Connecting-IP > X-Real-IP > X-Forwarded-For > RemoteAddr)
    |-- IPFilter (whitelist/blacklist)
    |-- RateLimiter (global + per-agent via distributed rate-limiter)
    |-- Logging (structured slog, duration, status)
    |
    v
[Handler.Messages()]
    |
    |-- 1. Body read & size validation (MaxRequestBody, default 10MB)
    |-- 2. JSON parse into map[string]any
    |-- 3. Model extraction from payload["model"]
    |-- 4. Resolver.Resolve(requestedModel) -> RoutingDecision
    |-- 5. Transparent detection (isClaudeOAuthToken)
    |-- 6. max_tokens clamp (Anthropic hard limits)
    |-- 7. Profile resolution (X-Profile header or arl_ token)
    |-- 8. API key resolution (priority chain below)
    |-- 9. Quota check (QuotaHandler.CheckQuota)
    |-- 10. Model slot acquisition (AdaptiveLimiter.Acquire)
    |-- 11. Preprocessing (optimizer pipeline + privacy masking)
    |-- 12. Proxy dispatch (format-specific proxy)
    |-- 13. Feedback callback (adaptive limiter + key pool + anomaly)
    |
    v
Upstream Provider
    |
    v
[Response]
    |-- Streaming: SSE relay with unmask buffer
    |-- Non-streaming: unmarshal, unmask, forward
    |
    v
Client Response
```

### 1.1 Body Read and Validation

```go
body, err := io.ReadAll(io.LimitReader(r.Body, h.cfg.MaxRequestBody+1))
```

- Reads body with a limit reader set to `MaxRequestBody + 1` bytes
- If `len(body) > MaxRequestBody`: returns `413 Request Entity Too Large`
- If JSON parse fails: returns `400 Bad Request` with `invalid_request_error`
- Default `MaxRequestBody`: 10MB (`MAX_REQUEST_BODY` env)

### 1.2 Model Extraction

```go
requestedModel, _ := payload["model"].(string)
```

Model is extracted early and used for all downstream routing decisions. The model string drives provider resolution, concurrency limiting, optimizer budget calculation, and vision routing.

---

## 2. Authentication Flow

Authentication is resolved through a priority chain with multiple fallback paths. The gateway supports three auth modalities:

### 2.1 Auth Priority Chain

```
1. Profile token (arl_ prefix) -> resolves to profile name -> profile has API key
2. Profile passthrough auth -> client's own Bearer/x-api-key forwarded
3. Profile account pool -> token from profile's selected account IDs
4. Profile default token -> GetDefault(provider) from TokenStore
5. Profile key pool fallback -> decision.APIKey from env ZAI_API_KEYS
6. Transparent OAuth token -> client's own sk-ant-oat01- or Bearer token
7. Stored OAuth token -> resolver decision's APIKey (claude-oauth passthrough)
8. Key pool -> h.keyPool.Acquire() (GLM mode, ZAI_API_KEYS from env)
9. Raw header fallback -> x-api-key or Authorization: Bearer from request
```

### 2.2 OAuth Token Detection (`isClaudeOAuthToken`)

```go
func isClaudeOAuthToken(r *http.Request) (string, bool) {
    // Path 1: Authorization: Bearer <token> (non-arl_ prefix)
    if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
        if tok := strings.TrimPrefix(ah, "Bearer "); tok != "" && !strings.HasPrefix(tok, "arl_") {
            return tok, true
        }
    }
    // Path 2: x-api-key: sk-ant-oat01-...
    if ak := r.Header.Get("x-api-key"); strings.HasPrefix(ak, "sk-ant-oat01-") {
        return ak, true
    }
    return "", false
}
```

Detection criteria:
- `Authorization: Bearer <token>` where token does NOT start with `arl_`
- `x-api-key: sk-ant-oat01-...` (Anthropic OAuth token format)

When detected, the token activates **transparent mode** which skips optimizer/masking and forwards client headers as-is.

### 2.3 Bearer vs x-api-key

The gateway supports both authentication headers. Resolution order:

1. `x-api-key` header checked first for API key auth
2. `Authorization: Bearer <token>` as fallback
3. For Claude OAuth specifically: `sk-ant-oat01-*` tokens sent as `x-api-key` are rewritten to `Authorization: Bearer` with `oauth-2025-04-20` beta flag added

```go
// In trySidecarOrDirect:
if strings.HasPrefix(apiKey, "sk-ant-oat01-") {
    r.Header.Set("Authorization", "Bearer "+apiKey)
    r.Header.Del("x-api-key")
    // Ensure oauth beta flag is present
}
```

### 2.4 Auth Failure

When no API key is resolved after exhausting all paths:

```json
{
  "type": "error",
  "error": {
    "type": "authentication_error",
    "message": "x-api-key header is required"
  }
}
```

Returns HTTP 401.

---

## 3. Profile Resolution

Profiles are stored in Redis (`profile:{name}`) and provide a routing abstraction layer.

### 3.1 Profile Lookup Priority

```
1. X-Profile header
2. arl_* token -> reverse lookup in Redis (profile_token:{token} -> profile name)
```

### 3.2 Profile Resolution Flow

```
X-Profile header present?
    |
    +-- YES --> getProfile(redis, profileName)
    |              |
    |              +-- found --> profileOverride = profile
    |              |              |
    |              |              +-- profile.Model != "" --> override payload["model"]
    |              |              +-- profile.Target != "" AND model doesn't belong to target
    |              |              |     --> mapModelForTarget (use provider's default model)
    |              |
    |              +-- not found --> 401 "profile not found"
    |
    +-- NO --> Check arl_* token
                   |
                   +-- matches --> ResolveProfileToken(redis, token)
                   |                 |
                   |                 +-- found --> profileName = resolved
                   |                 +-- not found --> no profile
                   |
                   +-- no match --> no profile
```

### 3.3 Profile Fields and Their Effects

| Field | Effect |
|-------|--------|
| `name` | Profile identifier |
| `baseUrl` | Overrides upstream URL in RoutingDecision |
| `apiKey` | Static API key (fallback when no account pool) |
| `model` | Hard override for payload["model"] |
| `opusModel` / `sonnetModel` / `haikuModel` | Per-tier model overrides |
| `target` | Provider ID to route through (e.g., "claude-oauth", "lotus") |
| `provider` | Explicit provider ID (takes precedence over target) |
| `accountIds` | Account pool for round-robin selection |
| `passthroughAuth` | Forward client's own Bearer/x-api-key instead of stored tokens |
| `targets` | Multi-target profiles for load distribution |

### 3.4 Profile Auth Modes

**Passthrough Auth** (`passthroughAuth: true`):
- Client's `Authorization: Bearer` token forwarded directly
- Strips `arl_` prefix if present (same key used for lookup and auth)
- Forces `bearer` auth mode and adds provider's extra headers (e.g., `anthropic-beta`)

**Account Pool** (`accountIds: [...]`):
- Selects token via `TokenStore.GetFromPool(provider, accountIds)`
- Prefers accounts with lowest 5h utilization
- Ties broken randomly

**Default Token** (no accountIds, no passthroughAuth):
- Uses `TokenStore.GetDefault(provider)` for the target provider
- Falls back to key pool (ZAI_API_KEYS env) if no stored token

### 3.5 GLM Mode vs Profile Requirement

```go
if profileName == "" && !h.cfg.GLMMode {
    // 401 "valid profile required"
}
```

- **GLM mode** (`GLM_MODE=true`): No profile required, uses env `ZAI_API_KEYS`
- **Non-GLM mode**: Profile is mandatory (X-Profile header or arl_ token)

### 3.6 Model Mapping for Target

When a profile has a `target` provider but the requested model doesn't belong to that provider:

```go
func mapModelForTarget(model, targetProvider string) string {
    if d, ok := providerDefaultModels[targetProvider]; ok {
        return d  // e.g., "lotus" -> "glm-4.5"
    }
    return model
}
```

Provider default models:
| Provider | Default Model |
|----------|--------------|
| claude-oauth | claude-haiku-4-5-20251001 |
| anthropic | claude-sonnet-4-20250514 |
| gemini-oauth | gemini-2.5-flash |
| openai | gpt-4o |
| zai | glm-4.5 |
| deepseek | deepseek-chat |
| lotus | (uses "default" model override from route table) |

### 3.7 Profile Token System

Tokens are generated as `arl_<64 hex chars>` and stored in Redis:
- `profile_token:{token}` -> profile name (with optional TTL)
- `profile_tokens:{profileName}` -> hash of `{keyName: json}` metadata

On request, the gateway checks if `x-api-key` or `Authorization: Bearer` starts with `arl_` and performs a reverse lookup.

---

## 4. Token Management

### 4.1 Token Store Architecture

```
Redis Keys:
  arl:tokens:{provider}:{accountId} -> TokenInfo JSON
  arl:tokens:{provider}:_index     -> SET of account IDs
```

**TokenInfo struct**:
```go
type TokenInfo struct {
    AccessToken   string
    RefreshToken  string
    ExpiryDate    time.Time
    Email         string
    AccountID     string
    Provider      string
    ProjectID     string    // gemini-oauth specific
    Tier          string
    Paused        bool
    IsDefault     bool
    CreatedAt     time.Time
    Scopes        string
}
```

### 4.2 Token Selection Algorithms

**GetDefault(provider)**:
1. Look for token with `IsDefault=true` and `!Paused`
2. Fallback: first non-paused token in provider index
3. Returns nil if no tokens exist

**GetFromPool(provider, accountIDs)**:
1. Pipeline-fetch all tokens for given account IDs
2. Filter out paused tokens
3. Single candidate: return immediately
4. Multiple candidates: select by lowest 5h utilization from cached rate limits
5. Ties broken randomly

### 4.3 Token Refresh Worker

The `RefreshWorker` runs on a 30-minute interval and performs:

```
refreshAll()
    |
    +-- For each token in TokenStore:
    |     |
    |     +-- Paused? --> skip
    |     |
    |     +-- Expired AND no refresh_token?
    |     |     --> delete token (auto-cleanup)
    |     |
    |     +-- Expired AND has refresh_token?
    |     |     --> attempt refresh (up to 3 retries with exponential backoff)
    |     |     --> on failure: skip, retry next cycle
    |     |
    |     +-- Approaching expiry (< 45 min threshold)?
    |           --> proactively refresh
    |
    +-- resolveMissingProjects() for gemini-oauth tokens
```

**Refresh request format**:
- **Claude OAuth**: JSON body `{"grant_type":"refresh_token","refresh_token":"...","client_id":"..."}`
- **All others**: `application/x-www-form-urlencoded` with same fields

**Retry on refresh failure**: 3 attempts with backoff `5s, 10s, 20s` (exponential: `1 << attempt * 5s`).

**Gemini OAuth project resolution**: For tokens without a `ProjectID`, calls `loadCodeAssist` -> `onboardUser` if needed -> polls LRO up to 3 times.

### 4.4 Token Refresh on 401 (On-Demand)

In the proxy feedback path, when upstream returns 401:

```go
oauthRefreshFn := func(oldKey string) (string, bool) {
    // Skip for passthrough auth (client manages lifecycle)
    // Find matching token by AccessToken
    // Call RefreshOne(provider, accountID)
    // Return refreshed token
}
```

This is set as `ProxyOptions.OnAuthError` and called by the proxy layer on 401 responses.

### 4.5 Token Rotation on 429

When upstream returns 429, the proxy calls `OnRateLimitError`:

```go
rotateAccountFn := func(oldKey string) (string, bool) {
    // Skip for passthrough auth
    // Profile mode: GetFromPool(provider, profile.AccountIDs) excluding oldKey
    // Non-profile mode: ListByProvider, find any non-paused token != oldKey
}
```

### 4.6 Provider ID Migration

On startup, `TokenStore.MigrateProviderRenames()` copies tokens from old provider IDs to new ones. Current migrations:

```go
var providerRenames = map[string]string{
    "claude": "claude-oauth",
}
```

---

## 5. Rate Limiting

The gateway implements a multi-layer rate limiting system.

### 5.1 Distributed Rate Limiter (Middleware Layer)

```
POST /api/ratelimit/check  (external rate-limiter service)
```

**Checks** (parallel via errgroup):
1. **Global**: key="global", tokens=1
2. **Per-agent**: key="agent:{agentID}", tokens=1

**Agent ID resolution**:
- For `/v1/messages`: extracted from `x-api-key` or `Authorization: Bearer` (last 4 chars)
- For other endpoints: from `agent_id` query param or chi URL param

**Fail-open**: If the rate-limiter service is unreachable, requests are allowed through.

**Skipped paths**: `/metrics`, `/api/metrics`, `/health`, `/ws`

**Error response format**: Anthropic-compatible for `/v1/messages`, plain JSON for others.

### 5.2 Adaptive Concurrency Limiter (Model Layer)

Per-model concurrent request limiting with automatic adjustment.

**Initialization**:
```go
NewAdaptiveLimiter(
    limits map[string]int,      // model -> initial limit (from UPSTREAM_MODEL_LIMITS)
    visionLimits map[string]int, // vision model limits
    defaultLimit int,            // UPSTREAM_DEFAULT_LIMIT (default: 3)
    globalLimit int,             // UPSTREAM_GLOBAL_LIMIT (default: 9)
    probeMultiplier int,         // UPSTREAM_PROBE_MULTIPLIER (default: 5)
)
```

Each model's `maxLimit = initialLimit * probeMultiplier`.

**Acquire algorithm**:
```
Acquire(requestedModel)
    |
    +-- acquireGlobal(60s timeout)
    |     Wait for globalInFlight < globalLimit
    |     Uses sync.Cond for efficient wake-up
    |
    +-- getModel(requestedModel)
    |
    +-- model.series > 0?
    |     |
    |     +-- YES --> tryFallbackAllSeries()
    |     |     |-- Round-robin within same series
    |     |     |-- If same-series util >= 70%: 20% cross-series distribution
    |     |     |-- If same-series full: spill to lower series
    |     |     |-- Spill triggers on: recent-429, latency pressure, or full capacity
    |     |
    |     +-- NO --> tryAcquire() on default model
    |
    +-- All full? --> acquireAnyModel(30s timeout)
          Wait with sync.Cond for Release() signal
          Try requested model first, then fallback order
```

**Series extraction**: `modelSeries("glm-5.1")` -> 5, `modelSeries("glm-4.7")` -> 4

**Feedback algorithm** (called after every upstream response):

```
On 429/503:
    limit_new = max(minLimit, limit * 0.5)
    Record peakBefore429, reset successRun

On 2xx:
    Update minRTT (CAS loop, lowest ever seen)
    Update RTT EWMA (alpha=0.3)
    Skip if manual override active
    Skip if within 5s cooldown of last 429
    Every 5th consecutive success:
        gradient = (minRTT + buffer) / sampleRTT, clamped [0.8, 2.0]
        newLimit = gradient * limit + sqrt(limit)
        Clamp to maxLimit
        If approaching learned ceiling (peakBefore429): cap at peak-1
        Decay peak after 5 minutes
```

**Manual overrides**: `SetOverride(model, limit)` pins a model's limit. Set to 0 to clear. Overridden models still track RTT/stats but don't auto-adjust.

### 5.3 Key Pool (Per-Key RPM)

```
NewKeyPool(ZAI_API_KEYS, UPSTREAM_RPM_LIMIT)
```

**Selection strategies**:
- `round-robin` (default): weighted round-robin favoring keys with most remaining budget
- `fill-first`: always pick key with most remaining budget

**RPM tracking**: Sliding 1-minute window of request timestamps per key.

**Cooldown**: On 429, key enters 10s cooldown. `Acquire()` waits via `sync.Cond` for cooldown expiry.

**Passthrough mode**: When `ZAI_API_KEYS` is empty, `Acquire()` returns `("", true)` and the caller uses the client's own key.

### 5.4 Login Rate Limiter

Per-IP rate limiting for auth endpoints:
- Max 5 attempts per 15-minute window
- Expired entries cleaned every 5 minutes
- Returns `429 Too Many Requests` with `Retry-After: 900`

### 5.5 Quota Enforcement

```go
if allowed, pct, _ := h.quotaHandler.CheckQuota(providerID, accountID, requestedModel); !allowed {
    // 429 "quota for {model} at {pct}%"
}
```

- Checked before acquiring model slot (fail-open on errors)
- At >= 80%: broadcasts `quota-warning` via WebSocket
- Config: `QUOTA_DAILY_BUDGET` (default: 57600), `QUOTA_BLOCK_PCT` (default: 95%)

---

## 6. Provider Fallback

### 6.1 Provider Registry

18 built-in providers, each with:

| Provider ID | Auth Type | Format | Upstream Base |
|-------------|-----------|--------|---------------|
| anthropic | api_key | anthropic | api.anthropic.com |
| claude-oauth | auth_code | anthropic | api.anthropic.com |
| openai | api_key | openai | api.openai.com |
| gemini | api_key | gemini | generativelanguage.googleapis.com |
| gemini-oauth | auth_code | gemini | cloudcode-pa.googleapis.com |
| copilot | device_code | openai | api.github.com/copilot |
| zai | api_key | anthropic | api.z.ai/api/anthropic |
| openrouter | api_key | openai | openrouter.ai/api |
| deepseek | api_key | openai | api.deepseek.com |
| qwen | device_code | openai | dashscope.aliyuncs.com |
| lotus | api_key | openai | api-cpxis.lotuss.com/llm |
| kimi | api_key | openai | api.moonshot.cn/v1 |
| huggingface | api_key | openai | api-inference.huggingface.co/models |
| ollama | api_key | openai | localhost:11434 |
| agy | api_key | openai | antigravity.com |
| cursor | api_key | openai | api2.cursor.sh |
| codebuddy | api_key | openai | api.codebuddy.io |
| kilo | api_key | openai | api.kilo.ai |

### 6.2 Model-to-Provider Routing Rules

```go
var modelRules = []modelRule{
    {"claude-",    []string{"claude-oauth", "anthropic"}},
    {"gpt-",       []string{"openai"}},
    {"o1-", "o3-", "o4-", []string{"openai"}},
    {"gemini-",    []string{"gemini-oauth", "gemini"}},
    {"glm-",       []string{"zai"}},
    {"qwen-",      []string{"qwen"}},
    {"or-",        []string{"openrouter"}},
    {"deepseek-",  []string{"deepseek"}},
    // ... etc
}
```

Resolution is prefix-based: first matching prefix wins. Providers in the list are tried in order.

### 6.3 Resolver Decision Flow

```
Resolve(model)
    |
    +-- For each modelRule where model has matching prefix:
    |     +-- For each providerID in rule.providers:
    |     |     +-- claude-oauth? --> tryResolveRoundRobin()
    |     |     +-- others?       --> tryResolve()
    |     |     +-- Returns decision if token found and not cooling down
    |     |
    |     +-- No provider had token?
    |           +-- GLM mode + zai in list? --> buildDecision("zai", model)
    |           +-- else? --> return nil
    |
    +-- No rule matched?
          +-- GLM mode? --> tryResolve("zai") or buildDecision("zai")
          +-- else? --> return nil
```

### 6.4 tryResolve (Single Token)

```
tryResolve(providerID, model)
    +-- isCoolingDown(providerID, model)? --> nil
    +-- GetDefault(providerID) --> token
    +-- token nil or expired? --> nil
    +-- return buildDecision(providerID, model, token.AccessToken, token.AccountID)
```

### 6.5 tryResolveRoundRobin (Multi-Token)

```
tryResolveRoundRobin(providerID, model)
    +-- isCoolingDown? --> nil
    +-- ListByProvider(providerID) --> all tokens
    +-- Filter: !Paused && not expired
    +-- Multiple active?
    |     +-- GetRateLimits(provider, accountIDs)
    |     +-- Partition: util5h < 0.8 = "low", >= 0.8 = "high"
    |     +-- Prefer low-util pool
    |     +-- Fallback to high-util if all high
    +-- Round-robin: atomic counter, index = (counter++) % len(active)
    +-- return buildDecision(providerID, model, token, accountID)
```

### 6.6 Provider Cooldown

```go
func (r *Resolver) MarkCooldown(providerID string, d time.Duration, model ...string)
```

- Key format: `providerID` or `providerID:model` if model specified
- Duration: 2 minutes (set in feedback callback on 429/503)
- Checked via `isCoolingDown()` before attempting resolution
- Cooldown is in-memory (sync.Map), not persisted

### 6.7 Cross-Provider Fallback Prevention

```go
if selectedModel != requestedModel {
    reqIsGLM := strings.HasPrefix(requestedModel, "glm-")
    selIsGLM := strings.HasPrefix(selectedModel, "glm-")
    if reqIsGLM != selIsGLM {
        // BLOCKED: cross-provider fallback prevented
    }
}
```

GLM models cannot fall back to Claude and vice versa. Same-provider fallback within the adaptive limiter is allowed.

### 6.8 Provider Route Table

Each provider has a route entry defining:

```go
type providerRoute struct {
    format       ProviderFormat   // anthropic | openai | gemini
    authMode     string           // "api_key" | "bearer"
    urlSuffix    string           // e.g., "/v1/messages"
    extraHeaders map[string]string
    modelOverride string          // e.g., lotus -> "default"
    maxTokens    int              // e.g., lotus -> 4096
}
```

Special headers for `claude-oauth`:
- `anthropic-beta`: full beta flag string with claude-code, oauth, interleaved-thinking, redact-thinking, context-management, prompt-caching, advanced-tool-use, effort flags
- `x-app: cli`
- `User-Agent: claude-cli/2.1.123 (external, cli)`
- `X-Stainless-*` headers for SDK fingerprinting

### 6.9 Dynamic Provider Registration

```go
func RegisterProviderRoute(providerID string, format ProviderFormat)
```

Custom providers (ID prefix `custom-`) can be registered at runtime. They default to OpenAI format if specified, or Anthropic format otherwise.

---

## 7. Request Preprocessing

Preprocessing runs only when `transparent == false`. The entire pipeline is skipped for transparent passthrough requests and for requests containing images.

### 7.1 System Prompt Injection

```go
if h.cfg.EnablePromptInjection {
    injectSystemPrompt(payload, h.cfg.PromptInjectionText)
}
```

Prepends gateway rules to the `system` field:
- String system: `prompt + "\n\n" + original`
- Array system: prepend `{"type":"text","text":prompt}` block

Default prompt includes token efficiency rules and vision handling rules.

### 7.2 Smart Max Tokens

```go
if h.cfg.EnableSmartMaxTokens {
    applySmartMaxTokens(payload, selectedModel)
}
```

Sets `max_tokens` only when not already specified by the client. Per-model defaults:

| Model | Max Tokens |
|-------|-----------|
| glm-5.1 | 8192 |
| glm-5-turbo | 4096 |
| glm-5 | 8192 |
| glm-4.5 | 4096 |
| claude-opus-4-7 | 200000 |
| claude-sonnet-4-6 | 200000 |
| claude-haiku-4-5-20251001 | 200000 |
| fallback | 4096 |

### 7.3 Field Stripping

```go
stripUnsupportedFields(payload, isNativeAnthropic, selectedModel)
```

**Always stripped** (non-native Anthropic): `context_management`, `service_tier`

**Native Anthropic** (bearer auth + anthropic format): `context_management` kept.

**Haiku/3.5-sonnet models**: `thinking`, `budget_tokens`, `effort` stripped. Thinking-related `context_management.edits` entries removed.

### 7.4 Content Block Filtering (Z.AI)

```go
if decision.ProviderID == "zai" {
    filterUnsupportedContent(payload)
}
```

Removes content blocks with `type: "server_tool_use"` from all messages.

### 7.5 Max Tokens Clamp

```go
func clampMaxTokens(payload map[string]any, model string)
```

Enforces Anthropic hard limits:
- claude-haiku-4-5-20251001: 64000
- claude-opus-4-7: 200000
- claude-sonnet-4-*: 200000

Only applies in non-transparent mode.

### 7.6 Token Optimization Pipeline

Runs only when `!hasImages && !transparent`.

**Budget level calculation**:
```
totalTokens = estimate(system) + estimate(messages)
pctUsed = totalTokens / contextWindow

pctUsed >= 0.8 --> budgetLevel = 2 (red)
pctUsed >= 0.6 --> budgetLevel = 1 (yellow)
else           --> budgetLevel = 0 (green)
```

**System prompt optimization** (13-stage pipeline):

| Stage | Component | Condition | Description |
|-------|-----------|-----------|-------------|
| F7 | Semantic Dedup | Always | Tokenizer-based semantic dedup (threshold 0.7) |
| F1 | Chunker | Configured | Chunk and reorder for cache locality |
| F8 | Delta Encoding | Configured | Delta compression against previous version |
| F9 | Sketch Dedup | Configured | MinHash sketch for exact/near dedup |
| F6 | Summarizer | budgetLevel >= 2 | LLM-powered summarization on red budget |
| F13 | Intent Filter | Configured | Remove content unrelated to user intent |
| F16 | Caveman | Configured | Append compression directives |

**Message optimization**:
- Whitespace optimization (tokenizer level)
- Sentence deduplication
- Skips `tool_use` blocks
- Processes both `text` and `content` fields depending on block type

### 7.7 Privacy Masking

```go
if h.privacy != nil {
    maskResult, _ = h.privacy.MaskRequest(body)
    body = maskResult.MaskedBody
}
```

Runs after optimization. Detects and masks:
- Secrets (API keys, tokens, passwords)
- PII (email addresses, phone numbers)

Replaced with placeholder tokens. The `MaskResult` is passed to the proxy for later unmasking.

### 7.8 Skipped for Images

Optimizer and privacy masking are both skipped when `hasImages == true` because URLs/base64 data would be corrupted.

---

## 8. Response Postprocessing

### 8.1 Feedback Callback

The feedback function is called after every upstream response (success or failure):

```go
feedbackFn := func(statusCode int, rtt time.Duration, headers http.Header) {
    // 1. Adaptive limiter feedback
    h.modelLimiter.Feedback(selectedModel, statusCode, rtt, headers)

    // 2. Key pool 429 reporting
    if statusCode == 429 || statusCode == 503 {
        h.keyPool.Report429(apiKey)
        h.resolver.MarkCooldown(decision.ProviderID, 2*time.Minute, selectedModel)
    } else if statusCode >= 200 && statusCode < 300 {
        h.keyPool.ReportSuccess(apiKey)
    }

    // 3. Anomaly detection
    if h.anomalyDetector != nil {
        anomaly := h.anomalyDetector.Record(float64(rtt.Milliseconds()))
        if anomaly.Severity >= SeverityHigh {
            // Log + WebSocket broadcast
        }
    }

    // 4. Error logging (status >= 400)
    if statusCode >= 400 {
        pushError(ErrorLogEntry{...})
        h.wsBroadcast("request-error", ...)
    } else {
        h.wsBroadcast("request-completed", ...)
    }

    // 5. Rate limit status caching
    // On 2xx with anthropic-ratelimit headers: store in Redis (6h TTL)
    // On 429: expire stale cache
}
```

### 8.2 Rate Limit Status Storage

Anthropic response headers parsed and cached:

```go
type RateLimitStatus struct {
    Provider     string
    AccountID    string
    Util5h       float64   // anthropic-ratelimit-unified-5h-utilization
    Util7d       float64   // anthropic-ratelimit-unified-7d-utilization
    Status       string    // anthropic-ratelimit-unified-status
    Status5h     string
    Status7d     string
    FallbackPct  float64
    Reset5h      string
    Reset7d      string
    ResetUnified string
    ReqRemaining string
    TokRemaining string
    UpdatedAt    time.Time
}
```

- Normalized: values <= 1.0 treated as fractions, converted to percentages
- Stored at `arl:ratelimit:{provider}:{accountId}` with 6-hour TTL
- On 429: cache entry deleted to prevent stale 100% data
- WebSocket event `ratelimit-updated` broadcast on successful store

### 8.3 Streaming Unmask

For streaming responses, the proxy layer manages an unmask buffer that accumulates partial placeholder tokens across SSE events and replaces them with original values when complete. This is handled within the proxy modules (not in handler.go).

### 8.4 Error Log Buffer

Circular buffer of 100 `ErrorLogEntry` records:
```go
type ErrorLogEntry struct {
    Timestamp  string
    Method     string
    Path       string
    Status     int
    DurationMs int64
    Error      string
    Model      string
}
```

Total count is monotonically increasing; buffer keeps last 100 entries.

---

## 9. Vision Routing

### 9.1 Image Detection

```go
hasImages = proxy.HasImageContent(payload)
```

Scans all message content blocks for `type: "image"` or `type: "image_url"` blocks.

### 9.2 Vision Routing Decision Tree

```
hasImages?
    |
    +-- NO --> normal proxy dispatch
    |
    +-- YES --> provider == "zai" AND !isNativeImageModel(selectedModel)?
    |     |
    |     +-- YES --> analyzeImagePayload() -> totalBytes, imageCount
    |     |            selectVisionModel(totalBytes, imageCount)
    |     |            |
    |     |            +-- score = totalBase64KB + (imageCount * 300)
    |     |            +-- score > 2000 OR imageCount >= 3 --> "glm-4.6v"
    |     |            +-- else --> "glm-4.6v"
    |     |            |
    |     |            +-- Rewrite payload["model"] to vision model
    |     |            +-- trySidecarOrDirect() (anthropic-compatible proxy)
    |     |
    |     +-- NO (already native vision model) --> proceed to format dispatch
    |
    +-- provider != "zai"?
          +-- Re-resolve for vision model if fallback occurred
          +-- Dispatch by format:
                +-- OpenAI format --> openaiProxy.ProxyOpenAI()
                +-- Gemini format + gemini-oauth --> codeAssistProxy.ProxyCodeAssist()
                +-- Gemini format + gemini API --> geminiAPIProxy.ProxyGemini()
                +-- Anthropic format --> trySidecarOrDirect()
```

### 9.3 Native Image Models

Models that natively support image input and skip auto-selection:

```go
func isNativeImageModel(model string) bool {
    switch model {
    case "glm-5.1", "glm-4.6v", "glm-4.5v":
        return true
    }
    return false
}
```

### 9.4 Image Format Conversion (Z.AI)

For Z.AI provider, Anthropic image blocks are rewritten to GLM-compatible format:

```
Anthropic: {"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}}
    --> GLM: {"type":"image_url","image_url":{"url":"data:image/png;base64,..."}}

Anthropic: {"type":"image","source":{"type":"url","url":"https://..."}}
    --> Fetch URL, convert to base64
    --> GLM: {"type":"image_url","image_url":{"url":"data:...;base64,..."}}
    --> If fetch fails: replace with "[image could not be loaded]" text block
```

### 9.5 Vision Model Selection Logic

```
selectVisionModel(totalBytes, imageCount):
    totalKB = totalBytes / 1024
    score = totalKB + imageCount * 300

    if score > 2000 OR imageCount >= 3:
        return "glm-4.6v"  // high-volume, best quality
    else:
        return "glm-4.6v"         // quality, 10 slots
```

### 9.6 Proxy Dispatch Priority

After preprocessing, the handler dispatches to the appropriate proxy in this priority order:

```
1. hasImages? --> Vision routing (section 9.2)
2. ZAIWebEnabled + IsZAIWebModel(model)? --> ZAIWebProxy.ProxyZAIWeb()
3. GLMMode + ZAIOpenAIModels[model]? --> OpenAIProxy.ProxyOpenAI(ZAIOpenAIURL)
4. decision != nil? --> Format-based dispatch:
   a. OpenAI format --> OpenAIProxy.ProxyOpenAI()
   b. Gemini format + gemini-oauth --> GeminiCodeAssistProxy.ProxyCodeAssist()
   c. Gemini format + gemini API --> GeminiAPIProxy.ProxyGemini()
   d. Anthropic format --> trySidecarOrDirect()
5. Fallback: trySidecarOrDirect() with profile options
```

The ZAIWeb path (step 2) routes requests through chat.z.ai's signed web API, providing free access without API keys. Configuration: `ZAI_WEB_ENABLED`, `ZAI_WEB_MODELS`, `ZAI_WEB_TOKEN`.

---

## 10. Transparent Passthrough

### 10.1 Activation Conditions

Transparent mode activates when ANY of these conditions are met:

1. **Resolver returned nil** AND model belongs to claude-oauth AND client has OAuth token
2. **Resolver returned claude-oauth** AND client has OAuth token
3. **Model belongs to claude-oauth** AND client has OAuth token (forced override)
4. **Profile selected token** starts with `sk-ant-oat01-` (OAuth token from account pool)

```go
// Condition 1 & 2: resolver-based detection
if d == nil && provider.ModelBelongsToProvider(requestedModel, "claude-oauth") {
    if _, ok := isClaudeOAuthToken(r); ok {
        d = h.resolver.ResolveTransparent(requestedModel)
    }
}
if d != nil && d.ProviderID == "claude-oauth" {
    if _, ok := isClaudeOAuthToken(r); ok {
        transparent = true
    }
}

// Condition 3: forced override
if !transparent && provider.ModelBelongsToProvider(requestedModel, "claude-oauth") {
    if _, ok := isClaudeOAuthToken(r); ok {
        transparent = true
    }
}

// Condition 4: profile OAuth token
if !transparent && strings.HasPrefix(apiKey, "sk-ant-oat01-") {
    transparent = true
}
```

### 10.2 What Transparent Mode Skips

When `transparent == true`:
- System prompt injection (no `GATEWAY RULES` prepended)
- Smart max_tokens (client's value preserved)
- Field stripping (upstream gets raw Claude Code CLI payload)
- Content block filtering (Z.AI-specific filtering skipped)
- Max tokens clamping

**NOT skipped** in transparent mode (only image requests skip these):
- Token optimization pipeline (all 13 stages)
- Privacy masking (secrets/PII detection)

### 10.3 Transparent Proxy Path (`trySidecarOrDirect`)

```
trySidecarOrDirect(w, r, apiKey, body, model, isStream, feedback, maskResult, opts, transparent)
    |
    +-- !transparent?
    |     +-- YES --> proxy.ProxyTransparent() directly (no billing injection)
    |
    +-- Fix OAuth headers for sk-ant-oat01- tokens:
    |     Authorization: Bearer {token}
    |     Delete x-api-key
    |     Add oauth-2025-04-20 to anthropic-beta
    |
    +-- Path 1: Go direct billing injection (fastest)
    |     Inject billing header into request body
    |     proxy.ProxyTransparent(w, r, apiKey, injectedBody, ...)
    |     +-- Success? --> record "go_direct" path metric, return
    |     +-- ErrBillingRejected? --> fall through
    |     +-- Other error? --> return error
    |
    +-- Path 2: Sidecar fallback (Node.js TLS fingerprint)
    |     proxy.ProxySidecar(w, r, sidecarURL, body, ...)
    |     +-- Success? --> record "sidecar" path metric, return
    |     +-- Error? --> log warning, fall through
    |
    +-- Path 3: Direct proxy (no billing header)
          proxy.ProxyTransparent(w, r, apiKey, body, ...)
          record "direct" path metric
```

### 10.4 Sidecar

The sidecar is a Node.js process (configurable via `CLI_SIDECAR_URL`, default `http://127.0.0.1:8081`) used when Go's HTTP client TLS fingerprint is rejected by upstream.

- Enabled when `CLISIDECAR_ENABLED=true`
- Disabled when `sidecarURL == ""`
- Acts as a TLS proxy that matches Node.js's fingerprint

### 10.5 ClaudeSessionManager

```go
h.sessionManager.BootstrapIfNeeded(tok)
```

Called when an OAuth token is detected. Initializes session management for the token (exact behavior in `proxy/claude_session.go`).

---

## 11. Health/Admin Endpoints

### 11.1 Health Check

```
GET /health
```

Response:
```json
{
  "status": "healthy",
  "queue_depth": 0,
  "uptime_seconds": 12345
}
```

2-second timeout on queue depth check.

### 11.2 Limiter Status

```
GET /v1/limiter-status
```

Returns adaptive limiter state for all models:
```json
{
  "global": {"global_in_flight": 3, "global_limit": 9},
  "models": [
    {
      "name": "glm-5.1",
      "in_flight": 1,
      "limit": 2,
      "max_limit": 5,
      "learned_ceiling": 3,
      "total_requests": 100,
      "total_429s": 2,
      "min_rtt_ms": 450,
      "ewma_rtt_ms": 520,
      "series": 5,
      "overridden": false
    }
  ],
  "seenModels": ["glm-5.1", "glm-4.7"],
  "keyPool": {"total_keys": 3, "keys": [...]},
  "glmMode": true
}
```

### 11.3 Limiter Override

```
POST /v1/limiter-override
Body: {"model": "glm-5.1", "limit": 5}
```

Sets or clears (limit=0) a manual concurrency override for a model.

### 11.4 Rate Limit Status

```
GET /v1/rate-limits
```

Returns cached Anthropic rate limit utilization for all accounts:
```json
[
  {
    "provider": "claude-oauth",
    "account_id": "user@example.com",
    "util_5h": 45.2,
    "util_7d": 12.8,
    "status": "healthy",
    "fallback_pct": 0,
    "updated_at": "2026-05-03T10:00:00Z"
  }
]
```

### 11.5 Models Catalog

```
GET /v1/models          (generic format)
GET /v1/models/native   (Anthropic-native format for Claude Code CLI)
```

The Anthropic-native endpoint returns `claudeModelsResponse` format when User-Agent starts with `claude-cli`, `Claude-Code`, or `anthropic-cli`.

### 11.6 Error Logs

```
GET /v1/error-logs       (last 100 entries)
GET /v1/error-logs/count (total + buffered count)
```

### 11.7 Routing Strategy

```
GET  /v1/routing-strategy
POST /v1/routing-strategy  Body: {"strategy": "round-robin"|"fill-first"}
```

### 11.8 Waste Findings

```
GET /v1/waste-findings
```

Returns JSON waste detection findings from the optimizer pipeline.

### 11.9 Token Counting

```
POST /v1/messages/count_tokens
```

Proxies to upstream `UPSTREAM_URL/v1/messages/count_tokens` using key pool authentication.

### 11.10 Anthropic Passthrough

```
ANY /api/claude_code/*
ANY /v1/mcp_servers
```

Full transparent proxy to `api.anthropic.com`. Forwards ALL client headers (except hop-by-hop). Converts `sk-ant-oat*` Bearer tokens to `x-api-key`.

### 11.11 Queue Endpoints (Async Job Queue)

```
POST /v1/chat/completions    (enqueue job)
GET  /v1/results/{requestID} (poll result)
```

### 11.12 ZAIWeb Endpoints

Z.AI web chat proxy endpoints (require `ZAI_WEB_ENABLED=true`):

```
GET  /v1/zaiweb/status                (token status, FE version, available models)
POST /v1/zaiweb/token                 (set/update JWT token, body: {"token": "..."})
POST /v1/zaiweb/image/generate        (proxy to image.z.ai, body forwarded as-is)
POST /v1/zaiweb/audio/tts             (proxy to audio.z.ai, SSE stream relay)
```

Routing: when `ZAIWebEnabled && IsZAIWebModel(model)`, requests to `/v1/messages` are routed through `ZAIWebProxy.ProxyZAIWeb()` instead of the standard proxy path.

### 11.13 Auth Routes (provider/handler.go)

Auth routes managed by `AuthHandler.Routes()`:

```
POST   /v1/auth/{provider}/start                  (start OAuth device/auth code flow)
POST   /v1/auth/{provider}/start-url              (start auth code flow, return URL)
POST   /v1/auth/{provider}/register               (register API key directly)
GET    /v1/auth/{provider}/callback               (OAuth callback, code exchange)
POST   /v1/auth/{provider}/callback               (OAuth callback, JSON body)
GET    /v1/auth/{provider}/status                  (poll device code token status)
POST   /v1/auth/{provider}/cancel                  (cancel pending auth session)
GET    /v1/auth/accounts                           (list all accounts across providers)
GET    /v1/auth/accounts/{provider}                (list accounts for provider)
DELETE /v1/auth/accounts/{provider}/{accountId}    (remove account)
POST   /v1/auth/accounts/{provider}/{accountId}/pause    (pause account)
POST   /v1/auth/accounts/{provider}/{accountId}/resume   (resume account)
POST   /v1/auth/accounts/{provider}/{accountId}/default  (set as default)
POST   /v1/auth/accounts/{provider}/{accountId}/email    (update email)
POST   /v1/auth/accounts/{provider}/{accountId}/refresh  (force token refresh)
POST   /v1/auth/login                              (dashboard login)
POST   /v1/auth/logout                             (dashboard logout)
GET    /v1/auth/check                              (check auth status)
GET    /v1/providers                               (list all providers)
PUT    /v1/providers/{provider}/upstream           (update provider upstream URL)
POST   /v1/providers/custom                        (create custom provider)
DELETE /v1/providers/custom/{provider}             (delete custom provider)
```

### 11.14 Profile Management

```
GET    /v1/profiles                    (list all)
POST   /v1/profiles                    (create)
GET    /v1/profiles/{name}             (get)
PUT    /v1/profiles/{name}             (update)
DELETE /v1/profiles/{name}             (delete)
POST   /v1/profiles/delete             (delete by JSON body)
POST   /v1/profiles/{name}/copy        (duplicate)
GET    /v1/profiles/{name}/export      (export bundle)
POST   /v1/profiles/{name}/export      (export bundle)
POST   /v1/profiles/import             (import bundle)
GET    /v1/profiles/recommended-models (list by provider)
GET    /v1/profiles/{name}/tokens      (list tokens)
POST   /v1/profiles/{name}/tokens      (generate arl_ token)
DELETE /v1/profiles/{name}/tokens/{keyName} (revoke token)
```

### 11.15 Middleware Endpoints

Internal endpoints skipped by rate limiter:
- `/metrics` - Prometheus metrics
- `/api/metrics` - API metrics dashboard
- `/health` - Health check
- `/ws` - WebSocket

### 11.16 Security Middleware

Applied to all requests:
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `X-XSS-Protection: 1; mode=block`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Cache-Control: no-store` on `/v1/*` paths
- `X-Correlation-ID` propagated or generated
- Real IP extraction from `CF-Connecting-IP` > `X-Real-IP` > `X-Forwarded-For` > `RemoteAddr`
- IP whitelist/blacklist filtering

### 11.17 Dashboard Auth

Dashboard endpoints protected by `DASHBOARD_API_KEY` env:
- Checks `x-api-key` header
- Falls back to `arl_session` cookie
- Disabled when key is empty

### 11.18 Session Secret

- Loaded from `config/session_secret` file
- Generated (64 random bytes, hex-encoded) if file doesn't exist
- Hot-reloaded via fsnotify
- Used for signing session cookies

### 11.19 Config Watcher

Watches `.env` file for changes via fsnotify. Debounced (500ms). Calls callback on changed values for hot-reload of configuration.

---

## Appendix A: Configuration Reference

All relevant env vars for the handler/routing layer:

| Env Var | Default | Description |
|---------|---------|-------------|
| `GLM_MODE` | `true` | Z.AI features active vs pure multi-provider proxy |
| `SERVER_PORT` | `:8080` | Listen address |
| `UPSTREAM_URL` | `https://api.z.ai/api/anthropic` | Z.AI upstream |
| `ANTHROPIC_DIRECT_URL` | `https://api.anthropic.com` | Anthropic upstream |
| `ZAI_API_KEYS` | (empty) | Comma-separated key pool |
| `UPSTREAM_RPM_LIMIT` | `40` | Per-key RPM budget |
| `UPSTREAM_MAX_RETRIES` | `3` | Retry count on 429 |
| `UPSTREAM_RETRY_BACKOFF` | `500ms` | Base backoff |
| `UPSTREAM_MODEL_LIMITS` | (see config.go) | Per-model concurrency limits |
| `UPSTREAM_DEFAULT_LIMIT` | `3` | Default model concurrency |
| `UPSTREAM_GLOBAL_LIMIT` | `9` | Total concurrent upstream requests |
| `UPSTREAM_PROBE_MULTIPLIER` | `5` | maxLimit = initial * multiplier |
| `MAX_REQUEST_BODY` | `10MB` | Request body size limit |
| `STREAM_TIMEOUT` | `300s` | Streaming response timeout |
| `ENABLE_PROMPT_INJECTION` | `true` | Prepend gateway rules |
| `ENABLE_SMART_MAX_TOKENS` | `true` | Auto-set max_tokens |
| `CLISIDECAR_ENABLED` | `true` | Enable Node.js sidecar |
| `CLI_SIDECAR_URL` | `http://127.0.0.1:8081` | Sidecar address |
| `IP_WHITELIST` | (empty) | Allowed IPs/CIDRs |
| `IP_BLACKLIST` | (empty) | Blocked IPs/CIDRs |
| `QUOTA_DAILY_BUDGET` | `57600` | Daily token budget |
| `QUOTA_BLOCK_PCT` | `95` | Block threshold percentage |
| `ANTHROPIC_API_VERSION` | `2023-06-01` | Anthropic API version header |
| `DEFAULT_MODEL` | `glm-5` | Default model |
| `DEFAULT_PROVIDER` | `glm` | Default provider |
| `ZAI_WEB_ENABLED` | `false` | Enable Z.AI web chat proxy routing |
| `ZAI_WEB_TOKEN` | (empty) | JWT token for chat.z.ai (auto-fetches anonymous if empty) |
| `ZAI_WEB_MODELS` | (empty) | Comma-separated models to route through ZAIWeb |
