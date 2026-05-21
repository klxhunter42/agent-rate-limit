# 05 - Data Models, API Contracts & State Management

Complete reference for all data structures, Redis keys, API endpoints, and state management in the agent-rate-limit gateway.

---

## 1. Configuration Data Model

### 1.1 Config struct (`config/config.go`)

| Field                    | Type                  | Env Var                      | Default                                                 |
|--------------------------|-----------------------|------------------------------|---------------------------------------------------------|
| ServerPort               | string                | SERVER_PORT                  | `:8080`                                                 |
| RedisAddr                | string                | REDIS_ADDR                   | `dragonfly:6379`                                        |
| RateLimiterAddr          | string                | RATE_LIMITER_ADDR            | `http://rate-limiter:8080`                              |
| QueueName                | string                | QUEUE_NAME                   | `ai_jobs`                                               |
| GlobalRateLimit          | int                   | GLOBAL_RATE_LIMIT            | 100                                                     |
| AgentRateLimit           | int                   | AGENT_RATE_LIMIT             | 5                                                       |
| WorkerPoolSize           | int                   | WORKER_POOL_SIZE             | 100                                                     |
| ReadTimeout              | duration              | READ_TIMEOUT                 | 30s                                                     |
| WriteTimeout             | duration              | WRITE_TIMEOUT                | 10s                                                     |
| OTLPEndpoint             | string                | OTLP_ENDPOINT                | `otel-collector:4317`                                   |
| RedisPoolSize            | int                   | REDIS_POOL_SIZE              | 50                                                      |
| RedisMinIdleConns        | int                   | REDIS_MIN_IDLE_CONNS         | 10                                                      |
| UpstreamURL              | string                | UPSTREAM_URL                 | `https://api.z.ai/api/anthropic`                        |
| AnthropicDirectURL       | string                | ANTHROPIC_DIRECT_URL         | `https://api.anthropic.com`                             |
| StreamTimeout            | duration              | STREAM_TIMEOUT               | 300s                                                    |
| ModelLimits              | map[string]int        | UPSTREAM_MODEL_LIMITS        | `glm-5.1:1,glm-5-turbo:1,...`                           |
| VisionModelLimits        | map[string]int        | UPSTREAM_VISION_MODEL_LIMITS | `glm-5.1:5,glm-4.6v:5,glm-4.5v:3`                       |
| DefaultLimit             | int                   | UPSTREAM_DEFAULT_LIMIT       | 3                                                       |
| GlobalLimit              | int                   | UPSTREAM_GLOBAL_LIMIT        | 9                                                       |
| UpstreamMaxRetries       | int                   | UPSTREAM_MAX_RETRIES         | 3                                                       |
| UpstreamRetryBaseBackoff | duration              | UPSTREAM_RETRY_BACKOFF       | 500ms                                                   |
| EnablePromptInjection    | bool                  | ENABLE_PROMPT_INJECTION      | true                                                    |
| EnableResponseTrim       | bool                  | ENABLE_RESPONSE_TRIM         | true                                                    |
| EnableSmartMaxTokens     | bool                  | ENABLE_SMART_MAX_TOKENS      | true                                                    |
| PromptInjectionText      | string                | PROMPT_INJECTION_TEXT        | (multi-line default)                                    |
| EnableAutoTruncate       | bool                  | ENABLE_AUTO_TRUNCATE         | true                                                    |
| TransientRetryMax        | int                   | TRANSIENT_RETRY_MAX          | 3                                                       |
| UpstreamAPIKeys          | []string              | ZAI_API_KEYS                 | (empty)                                                 |
| UpstreamRPMLimit         | int                   | UPSTREAM_RPM_LIMIT           | 40                                                      |
| ProbeMultiplier          | int                   | UPSTREAM_PROBE_MULTIPLIER    | 5                                                       |
| ModelPricing             | map[string]ModelPrice | MODEL_PRICING                | (see defaults)                                          |
| NativeVisionURL          | string                | NATIVE_VISION_URL            | `https://open.bigmodel.cn/api/paas/v4/chat/completions` |
| IPWhitelist              | string                | IP_WHITELIST                 | (empty)                                                 |
| IPBlacklist              | string                | IP_BLACKLIST                 | (empty)                                                 |
| QuotaCacheTTL            | duration              | QUOTA_CACHE_TTL              | 30s                                                     |
| QuotaDailyBudget         | int64                 | QUOTA_DAILY_BUDGET           | 57600                                                   |
| QuotaBlockPct            | float64               | QUOTA_BLOCK_PCT              | 95                                                      |
| QuotaRedisPoolSize       | int                   | QUOTA_REDIS_POOL_SIZE        | 5                                                       |
| QuotaRedisMinIdle        | int                   | QUOTA_REDIS_MIN_IDLE         | 2                                                       |
| ProviderModelPrefixes    | string                | PROVIDER_MODEL_PREFIXES      | `zai:glm-;anthropic:claude-;...`                        |
| MaxRequestBody           | int64                 | MAX_REQUEST_BODY             | 10485760 (10MB)                                         |
| DefaultModel             | string                | DEFAULT_MODEL                | `glm-5`                                                 |
| DefaultProvider          | string                | DEFAULT_PROVIDER             | `glm`                                                   |
| DefaultTemperature       | float64               | DEFAULT_TEMPERATURE          | 0.7                                                     |
| DefaultMaxTokens         | int                   | DEFAULT_MAX_TOKENS           | 1024                                                    |
| GeminiCodeAssistEndpoint | string                | GEMINI_CODEASSIST_ENDPOINT   | `https://cloudcode-pa.googleapis.com/v1internal`        |
| GeminiAPIEndpoint        | string                | GEMINI_API_ENDPOINT          | `https://generativelanguage.googleapis.com`             |
| GeminiDefaultModel       | string                | GEMINI_DEFAULT_MODEL         | `models/gemini-2.5-flash-preview-05-20`                 |
| AnthropicVersion         | string                | ANTHROPIC_API_VERSION        | `2023-06-01`                                            |
| ModelPriority            | string                | MODEL_PRIORITY               | `glm-5.1:100,glm-5-turbo:90,...`                        |
| AnomalyCooldownSec       | int                   | ANOMALY_COOLDOWN_SEC         | 5                                                       |
| AnomalyZThreshold        | float64               | ANOMALY_Z_THRESHOLD          | 2.0                                                     |
| GLMMode                  | bool                  | GLM_MODE                     | true                                                    |
| CLISidecarURL            | string                | CLI_SIDECAR_URL              | `http://127.0.0.1:8081`                                 |
| CLISidecarEnabled        | bool                  | CLI_SIDECAR_ENABLED          | true                                                    |
| ZAIOpenAIURL             | string                | ZAI_OPENAI_URL               | `https://api.z.ai/api/paas/v4/chat/completions`         |
| ZAIOpenAIModels          | map[string]bool       | ZAI_OPENAI_MODELS            | (empty)                                                 |

### 1.2 ModelPrice struct

```go
type ModelPrice struct {
    InputPerMillion  float64  // USD per 1M input tokens
    OutputPerMillion float64  // USD per 1M output tokens
}
```

Default pricing: `glm-5.1:1.4:4.4,glm-5-turbo:1.2:4.0,glm-5:1.0:3.2,glm-4.7:0.6:2.2,...`

---

## 2. Provider Data Model

### 2.1 ProviderConfig struct (`provider/registry.go`)

```go
type ProviderConfig struct {
    ID             string    `json:"id"`
    Name           string    `json:"name"`
    AuthType       AuthType  `json:"auth_type"`          // "api_key"|"device_code"|"auth_code"|"session_cookie"
    TokenURL       string    `json:"token_url,omitempty"`
    AuthURL        string    `json:"auth_url,omitempty"`
    DeviceCodeURL  string    `json:"device_code_url,omitempty"`
    UserInfoURL    string    `json:"user_info_url,omitempty"`
    CallbackPort   int       `json:"callback_port,omitempty"`
    Scopes         []string  `json:"scopes,omitempty"`
    ClientID       string    `json:"client_id,omitempty"`
    ClientSecret   string    `json:"client_secret,omitempty"`
    UpstreamBase   string    `json:"upstream_base"`
}
```

### 2.2 AuthType enum

| Value                 | String             | Description                     |
|-----------------------|--------------------|---------------------------------|
| AuthTypeAPIKey        | `"api_key"`        | Static API key header           |
| AuthTypeDeviceCode    | `"device_code"`    | OAuth device code flow          |
| AuthTypeAuthCode      | `"auth_code"`      | OAuth authorization code + PKCE |
| AuthTypeSessionCookie | `"session_cookie"` | Browser session cookie          |

### 2.3 Built-in Providers

| ID           | Name                  | AuthType    | Upstream Default                              |
|--------------|-----------------------|-------------|-----------------------------------------------|
| anthropic    | Anthropic             | api_key     | `https://api.anthropic.com`                   |
| gemini       | Google Gemini         | api_key     | `https://generativelanguage.googleapis.com`   |
| gemini-oauth | Google Gemini (OAuth) | auth_code   | `https://cloudcode-pa.googleapis.com`         |
| openai       | OpenAI                | api_key     | `https://api.openai.com`                      |
| copilot      | GitHub Copilot        | device_code | `https://api.github.com/copilot`              |
| zai          | Z.AI                  | api_key     | `https://api.z.ai/api/anthropic`              |
| openrouter   | OpenRouter            | api_key     | `https://openrouter.ai/api`                   |
| qwen         | Qwen (Aliyun)         | device_code | `https://dashscope.aliyuncs.com`              |
| claude-oauth | Claude (OAuth)        | auth_code   | `https://api.anthropic.com`                   |
| deepseek     | DeepSeek              | api_key     | `https://api.deepseek.com`                    |
| kimi         | Kimi                  | api_key     | `https://api.kimi.com/coding`                 |
| huggingface  | Hugging Face          | api_key     | `https://api-inference.huggingface.co/models` |
| ollama       | Ollama                | api_key     | `http://localhost:11434`                      |
| agy          | Antigravity           | api_key     | `https://antigravity.com`                     |
| cursor       | Cursor                | api_key     | `https://api2.cursor.sh`                      |
| codebuddy    | CodeBuddy             | api_key     | `https://api.codebuddy.io`                    |
| kilo         | Kilo                  | api_key     | `https://api.kilo.ai`                         |

Custom providers use ID prefix `custom-` (e.g. `custom-a1b2c3`).

### 2.4 ProviderFormat enum (`provider/resolver.go`)

| Value           | String        |
|-----------------|---------------|
| FormatAnthropic | `"anthropic"` |
| FormatOpenAI    | `"openai"`    |
| FormatGemini    | `"gemini"`    |

---

## 3. Token & Account Data Model

### 3.1 TokenInfo struct (`provider/token-store.go`)

```go
type TokenInfo struct {
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token,omitempty"`
    ExpiryDate   time.Time `json:"expiry_date"`
    Email        string    `json:"email,omitempty"`
    AccountID    string    `json:"account_id"`
    Provider     string    `json:"provider"`
    ProjectID    string    `json:"project_id,omitempty"`    // Gemini CodeAssist project
    Tier         string    `json:"tier,omitempty"`          // "api_key"|"browser_session"|"oauth"
    Paused       bool      `json:"paused"`
    IsDefault    bool      `json:"is_default"`
    CreatedAt    time.Time `json:"created_at"`
    Scopes       string    `json:"scopes,omitempty"`
}
```

### 3.2 AcctRateLimit struct

```go
type AcctRateLimit struct {
    Util5h float64 `json:"util_5h"`   // 5-hour utilization percentage (0-100)
    Status string  `json:"status"`    // "low"|"standard"|"high"|"critical"
}
```

### 3.3 RateLimitStatus struct (`handler/handler.go`)

```go
type RateLimitStatus struct {
    Provider     string    `json:"provider"`
    AccountID    string    `json:"account_id"`
    Util5h       float64   `json:"util_5h"`
    Util7d       float64   `json:"util_7d"`
    Status       string    `json:"status"`
    Status5h     string    `json:"status_5h,omitempty"`
    Status7d     string    `json:"status_7d,omitempty"`
    FallbackPct  float64   `json:"fallback_pct"`
    Reset5h      string    `json:"reset_5h,omitempty"`
    Reset7d      string    `json:"reset_7d,omitempty"`
    ResetUnified string    `json:"reset_unified,omitempty"`
    ReqRemaining string    `json:"req_remaining,omitempty"`
    TokRemaining string    `json:"tok_remaining,omitempty"`
    UpdatedAt    time.Time `json:"updated_at"`
}
```

### 3.4 AuthSession struct (`provider/handler.go`)

```go
type AuthSession struct {
    Provider     string     `json:"provider"`
    State        string     `json:"state"`
    DeviceCode   string     `json:"device_code,omitempty"`
    PKCEVerifier string     `json:"pkce_verifier,omitempty"`
    Type         string     `json:"type"`     // "device_code"|"auth_code"
    Token        *TokenInfo `json:"token,omitempty"`
    StartedAt    time.Time  `json:"started_at"`
}
```

---

## 4. Routing Decision Data Model

### 4.1 RoutingDecision struct (`provider/resolver.go`)

```go
type RoutingDecision struct {
    ProviderID        string
    ProviderCfg       ProviderConfig
    Format            ProviderFormat   // "anthropic"|"openai"|"gemini"
    UpstreamURL       string
    AuthMode          string           // "api_key"|"bearer"
    ExtraHeaders      map[string]string
    APIKey            string
    AccountID         string
    ModelOverride     string           // non-empty = override payload model
    MaxTokens         int              // 0 = no clamp, >0 = max_tokens ceiling
    MaxContinuations  int              // 0 = disabled, >0 = auto-continuation limit
    ToolMode          string           // ""|"native" (OpenAI function calling)
}
```

### 4.2 Provider Route Table

| ProviderID   | Format    | AuthMode   | URL Suffix                             | ModelOverride   | MaxTokens   | MaxContinuations  | ToolMode   |
|--------------|-----------|------------|----------------------------------------|-----------------|-------------|-------------------|------------|
| anthropic    | anthropic | api_key    | `/v1/messages`                         | -               | 0           | 0                 | -          |
| claude-oauth | anthropic | api_key    | `/v1/messages?beta=true`               | -               | 0           | 0                 | -          |
| claude       | anthropic | api_key    | `/v1/messages?beta=true`               | -               | 0           | 0                 | -          |
| zai          | anthropic | api_key    | `/v1/messages`                         | -               | 0           | 0                 | -          |
| openai       | openai    | bearer     | `/v1/chat/completions`                 | -               | 0           | 0                 | -          |
| copilot      | openai    | bearer     | `/v1/chat/completions`                 | -               | 0           | 0                 | -          |
| openrouter   | openai    | bearer     | `/v1/chat/completions`                 | -               | 0           | 0                 | -          |
| qwen         | openai    | bearer     | `/compatible-mode/v1/chat/completions` | -               | 0           | 0                 | -          |
| gemini       | gemini    | api_key    | (dynamic URL)                          | -               | 0           | 0                 | -          |
| gemini-oauth | gemini    | bearer     | (dynamic URL)                          | -               | 0           | 0                 | -          |
| deepseek     | openai    | bearer     | `/v1/chat/completions`                 | -               | 0           | 0                 | -          |
| kimi         | anthropic | api_key    | `/v1/messages`                         | -               | 0           | 0                 | -          |
| huggingface  | openai    | bearer     | `/v1/chat/completions`                 | -               | 0           | 0                 | -          |
| ollama       | openai    | bearer     | `/v1/chat/completions`                 | -               | 0           | 0                 | -          |
| agy          | anthropic | api_key    | `/v1/messages`                         | -               | 0           | 0                 | -          |
| cursor       | openai    | bearer     | `/v1/chat/completions`                 | -               | 0           | 0                 | -          |
| codebuddy    | openai    | bearer     | `/v1/chat/completions`                 | -               | 0           | 0                 | -          |
| kilo         | openai    | bearer     | `/v1/chat/completions`                 | -               | 0           | 0                 | -          |

Claude-oauth/claude extra headers (injected by route table):
- `anthropic-beta: claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24`
- `x-app: cli`, `User-Agent: claude-cli/2.1.123 (external, cli)`
- Various `X-Stainless-*` headers for API compatibility

OpenRouter extra headers:
- `HTTP-Referer: https://github.com/klxhunter/agent-rate-limit`

### 4.3 Model Routing Rules

| Prefix                | Providers (priority order)       |
|-----------------------|----------------------------------|
| `claude-`             | claude-oauth, anthropic          |
| `gpt-`                | openai                           |
| `o1-` / `o3-` / `o4-` | openai                           |
| `gemini-`             | gemini-oauth, gemini             |
| `glm-`                | zai                              |
| `qwen-`               | qwen                             |
| `or-`                 | openrouter                       |
| `anthropic/`          | anthropic, openrouter (fallback) |
| `openai/`             | openrouter                       |
| `google/`             | openrouter                       |
| `meta/`               | openrouter                       |
| `deepseek/`           | openrouter                       |
| `qwen/`               | openrouter                       |
| `deepseek-`           | deepseek                         |
| `kimi-`               | kimi                             |
| `huggingface/`        | huggingface                      |
| `ollama`              | ollama                           |
| `agy-`                | agy                              |

---

## 5. Profile Data Model

### 5.1 Profile struct (`handler/profile.go`)

```go
type Profile struct {
    Name           string          `json:"name"`
    BaseURL        string          `json:"baseUrl"`
    APIKey         string          `json:"apiKey"`
    Model          string          `json:"model"`               // force model
    OpusModel      string          `json:"opusModel,omitempty"`
    SonnetModel    string          `json:"sonnetModel,omitempty"`
    HaikuModel     string          `json:"haikuModel,omitempty"`
    Target         string          `json:"target"`              // provider ID
    Provider       string          `json:"provider,omitempty"`
    AccountIDs     []string        `json:"accountIds"`          // account pool
    Targets        []ProfileTarget `json:"targets,omitempty"`   // multi-target
    PassthroughAuth bool           `json:"passthroughAuth,omitempty"`
    CreatedAt      string          `json:"createdAt"`
    UpdatedAt      string          `json:"updatedAt"`
}
```

### 5.2 ProfileTarget struct

```go
type ProfileTarget struct {
    ID              string   `json:"id,omitempty"`
    Target          string   `json:"target"`                // provider ID
    BaseURL         string   `json:"baseUrl,omitempty"`
    APIKey          string   `json:"apiKey,omitempty"`
    AccountIDs      []string `json:"accountIds,omitempty"`
    PassthroughAuth bool     `json:"passthroughAuth,omitempty"`
}
```

### 5.3 ProfileToken struct

```go
type ProfileToken struct {
    KeyName   string `json:"keyName"`
    Token     string `json:"token"`        // format: arl_<64 hex chars>
    Profile   string `json:"profile"`
    ExpiresAt string `json:"expiresAt,omitempty"`
    CreatedAt string `json:"createdAt"`
}
```

Validation: `name` required, no `/`, `%`, or `\`. Default target: `"claude-oauth"`.

---

## 6. Proxy Data Model

### 6.1 ProxyOptions struct (`proxy/anthropic.go`)

```go
type ProxyOptions struct {
    AuthMode          string                              // "api_key"|"bearer"
    UpstreamOverride  string                              // override upstream URL
    ExtraHeaders      map[string]string                   // additional headers
    Transparent       bool                                // skip body/header mods
    BillingInjected   bool                                // Go billing header injected
    OnAuthError       func(oldKey string) (string, bool)  // 401 refresh callback
    OnRateLimitError  func(oldKey string) (string, bool)  // 429 rotation callback
}
```

### 6.2 Error Response structs

```go
type ErrorResponse struct {
    Type  string      `json:"type"`   // always "error"
    Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
    Type    string `json:"type"`     // "invalid_request_error"|"authentication_error"|"api_error"|"overloaded_error"
    Message string `json:"message"`
}
```

### 6.3 FeedbackFunc type

```go
type FeedbackFunc func(statusCode int, rtt time.Duration, headers http.Header)
```

---

## 7. Adaptive Limiter Data Model

### 7.1 ModelStatus struct (`middleware/adaptive_limiter.go`)

```go
type ModelStatus struct {
    Name           string `json:"name"`
    InFlight       int64  `json:"in_flight"`
    Limit          int64  `json:"limit"`
    MaxLimit       int64  `json:"max_limit"`
    LearnedCeiling int64  `json:"learned_ceiling"`
    TotalReqs      int64  `json:"total_requests"`
    Total429s      int64  `json:"total_429s"`
    MinRTTMs       int64  `json:"min_rtt_ms"`
    EwmaRTTMs      int64  `json:"ewma_rtt_ms"`
    Series         int    `json:"series"`
    Overridden     bool   `json:"overridden"`
}
```

### 7.2 GlobalStatus struct

```go
type GlobalStatus struct {
    GlobalInFlight int64 `json:"global_in_flight"`
    GlobalLimit    int64 `json:"global_limit"`
}
```

Algorithm: gradient controller + Netflix concurrency limits. On 429: `limit *= 0.5`. On success every 5th: `limit = gradient * limit + sqrt(limit)`. Cooldown 5s after 429. Learned ceiling decays after 5 minutes.

---

## 8. Usage Data Model

### 8.1 UsageRecord struct (`handler/usage.go`)

```go
type UsageRecord struct {
    Model       string  `json:"model"`
    InputTokens int64   `json:"input_tokens"`
    OutputTokens int64  `json:"output_tokens"`
    Cost        float64 `json:"cost"`
    Requests    int64   `json:"requests"`
    Errors      int64   `json:"errors"`
    Period      string  `json:"period"`
}
```

### 8.2 UsageSummary struct

```go
type UsageSummary struct {
    TotalRequests  int64   `json:"total_requests"`
    TotalErrors    int64   `json:"total_errors"`
    TotalTokensIn  int64   `json:"total_tokens_in"`
    TotalTokensOut int64   `json:"total_tokens_out"`
    TotalCost      float64 `json:"total_cost"`
    Models         int     `json:"models"`
    Period         string  `json:"period"`
}
```

---

## 9. Quota Data Model

### 9.1 ModelQuota struct (`handler/quota.go`)

```go
type ModelQuota struct {
    Name        string  `json:"name"`
    DisplayName string  `json:"displayName"`
    Percentage  float64 `json:"percentage"`
    ResetTime   *string `json:"resetTime,omitempty"`
}
```

### 9.2 QuotaResult struct

```go
type QuotaResult struct {
    Success    bool        `json:"success"`
    Models     []ModelQuota `json:"models,omitempty"`
    LastUpdated string      `json:"lastUpdated"`
    Error      string       `json:"error,omitempty"`
    AccountID  string       `json:"accountId"`
    Provider   string       `json:"provider"`
}
```

### 9.3 ProviderQuotaResult struct

```go
type ProviderQuotaResult struct {
    Provider    string       `json:"provider"`
    Accounts    []QuotaResult `json:"accounts"`
    LastUpdated string       `json:"lastUpdated"`
}
```

---

## 10. Overview & Health Data Model

### 10.1 OverviewResponse struct (`handler/overview.go`)

```go
type OverviewResponse struct {
    Profiles      int    `json:"profiles"`
    Accounts      int    `json:"accounts"`
    Providers     int    `json:"providers"`
    Models        int    `json:"models"`
    ActiveKeys    int    `json:"activeKeys"`
    PausedKeys    int    `json:"pausedKeys"`
    QueueDepth    int64  `json:"queueDepth"`
    HealthStatus  string `json:"healthStatus"`   // "healthy"|"degraded"|"unhealthy"
    UptimeSeconds int64  `json:"uptimeSeconds"`
    TotalRequests int64  `json:"totalRequests"`
    TotalErrors   int64  `json:"totalErrors"`
}
```

### 10.2 HealthCheck struct

```go
type HealthCheck struct {
    ID       string `json:"id"`       // "dragonfly"|"rate-limiter"|"prometheus"|"key-pool"|"upstream"|"memory"
    Name     string `json:"name"`
    Status   string `json:"status"`   // "pass"|"warn"|"fail"
    Message  string `json:"message"`
    Category string `json:"category"` // "connectivity"|"resources"|"config"|"upstream"
}
```

### 10.3 DetailedHealthResponse struct

```go
type DetailedHealthResponse struct {
    Status    string        `json:"status"`
    Checks    []HealthCheck `json:"checks"`
    Timestamp string        `json:"timestamp"`
    Uptime    int64         `json:"uptimeSeconds"`
}
```

### 10.4 FixResponse struct

```go
type FixResponse struct {
    CheckID string `json:"checkId"`
    Status  string `json:"status"`
    Message string `json:"message"`
}
```

---

## 11. Config API Data Model

### 11.1 ConfigResponse struct (`handler/config.go`)

```go
type ConfigResponse struct {
    UpstreamURL         string                   `json:"upstreamUrl"`
    ModelLimits         map[string]int           `json:"modelLimits"`
    DefaultLimit        int                      `json:"defaultLimit"`
    GlobalLimit         int                      `json:"globalLimit"`
    RoutingStrategy     string                   `json:"routingStrategy"`
    EnablePromptInjection bool                    `json:"enablePromptInjection"`
    EnableSmartMaxTokens bool                     `json:"enableSmartMaxTokens"`
    EnableResponseTrim  bool                     `json:"enableResponseTrim"`
    StreamTimeout       string                   `json:"streamTimeout"`
    UpstreamRPMLimit    int                      `json:"upstreamRpmLimit"`
    UpstreamMaxRetries  int                      `json:"upstreamMaxRetries"`
    ProbeMultiplier     int                      `json:"probeMultiplier"`
    PrivacyEnabled      bool                     `json:"privacyEnabled"`
    ModelPricing        map[string]ModelPrice    `json:"modelPricing"`
    NumAPIKeys          int                      `json:"numApiKeys"`
}
```

### 11.2 ThinkingConfig struct

```go
type ThinkingConfig struct {
    DefaultBudget int            `json:"defaultBudget"`
    ModelBudgets  map[string]int `json:"modelBudgets"`
    Enabled       bool           `json:"enabled"`
}
```

### 11.3 GlobalEnv struct

```go
type GlobalEnv struct {
    Enabled bool              `json:"enabled"`
    Env     map[string]string `json:"env"`
}
```

### 11.4 MaxTokensConfig struct

```go
type MaxTokensConfig struct {
    Models map[string]int `json:"models"`
}
```

---

## 12. WebSocket Data Model

### 12.1 WSEvent struct (`handler/websocket.go`)

```go
type WSEvent struct {
    Type      string      `json:"type"`
    Data      interface{} `json:"data"`
    Timestamp string      `json:"timestamp"`
}
```

Event types: `config-changed`, `request-completed`, `request-error`, `anomaly-detected`, `quota-warning`, `ratelimit-updated`.

---

## 13. Error Log Data Model

### 13.1 ErrorLogEntry struct

```go
type ErrorLogEntry struct {
    Timestamp  string `json:"time"`
    Method     string `json:"method"`
    Path       string `json:"path"`
    Status     int    `json:"status"`
    DurationMs int64  `json:"duration_ms"`
    Error      string `json:"error"`
    Model      string `json:"model,omitempty"`
}
```

Buffer: circular, max 100 entries.

---

## 14. Request/Response Schemas (Handler Level)

### 14.1 ChatRequest (POST /v1/chat/completions)

```json
{
    "agent_id": "string (required)",
    "model": "string (default: glm-5)",
    "messages": [{"role": "user", "content": "..."}],
    "max_tokens": 1024,
    "temperature": 0.7,
    "provider": "string (default: glm)",
    "stream": false,
    "metadata": {"key": "value"}
}
```

### 14.2 ChatResponse

```json
{
    "request_id": "uuid",
    "status": "queued",
    "agent_id": "string"
}
```

### 14.3 ResultResponse (GET /v1/results/{requestID})

```json
{
    "request_id": "uuid",
    "status": "pending|completed",
    "result": {}
}
```

### 14.4 HealthResponse (GET /health)

```json
{
    "status": "healthy",
    "queue_depth": 0,
    "uptime_seconds": 3600
}
```

### 14.5 LimiterOverride request (POST /v1/limiter-override)

```json
{
    "model": "string (required)",
    "limit": 0
}
```
`limit=0` clears override.

### 14.6 RoutingStrategyRequest (PUT /v1/routing/strategy)

```json
{
    "strategy": "round-robin|fill-first"
}
```

### 14.7 Model listing response (GET /v1/models)

```json
{
    "models": [{
        "name": "glm-5.1",
        "provider": "zai",
        "series": "5",
        "limit": 1,
        "format": "anthropic",
        "input_per_million": 1.4,
        "output_per_million": 4.4,
        "context_window": 128000,
        "thinking_support": "budget",
        "extended_context": false,
        "native_image_input": true,
        "deprecated": false
    }]
}
```

### 14.8 Anthropic-native models (GET /v1/models, Claude CLI user-agent)

```json
{
    "data": [{
        "id": "claude-opus-4-7",
        "object": "model",
        "created": 1746057600,
        "owned_by": "anthropic",
        "max_tokens": 200000
    }],
    "has_more": false,
    "first_id": "...",
    "last_id": "..."
}
```

---

## 15. Error Response Format

All endpoints use consistent error JSON:

### Anthropic-style (proxy endpoints)

```json
{
    "type": "error",
    "error": {
        "type": "invalid_request_error|authentication_error|api_error|overloaded_error|quota_exceeded|no_provider",
        "message": "human-readable message"
    }
}
```

### Simple-style (management endpoints)

```json
{
    "error": "human-readable message"
}
```

### HTTP Status Codes

| Code   | Type                           | Used By                            |
|--------|--------------------------------|------------------------------------|
| 400    | invalid_request_error          | Bad JSON, validation               |
| 401    | authentication_error           | Missing x-api-key, invalid profile |
| 403    | no_provider, profile_forbidden | No accounts, profile mismatch      |
| 404    | not found                      | Profile/account/provider not found |
| 409    | conflict                       | Profile already exists             |
| 413    | invalid_request_error          | Body exceeds MaxRequestBody        |
| 429    | quota_exceeded                 | Quota enforcement                  |
| 500    | api_error                      | Internal errors                    |
| 502    | api_error                      | Upstream proxy failure             |
| 503    | overloaded_error               | All model slots busy               |

---

## 16. Redis Data Model

### 16.1 Key Inventory

All keys use the `arl:` prefix (except legacy `profile:` and `usage:` patterns). Dragonfly (Redis-compatible) is the backing store.

| Key Pattern                            | Value Type                    | TTL                  | Purpose                              |
|----------------------------------------|-------------------------------|----------------------|--------------------------------------|
| `arl:tokens:{provider}:{accountID}`    | JSON (TokenInfo)              | None                 | OAuth/API key token data per account |
| `arl:tokens:{provider}:_index`         | SET of accountIDs             | None                 | Account index for provider           |
| `arl:ratelimit:{provider}:{accountID}` | JSON (RateLimitStatus)        | 6h                   | Per-account rate limit state         |
| `arl:providers:custom:{id}`            | JSON (ProviderConfig)         | None                 | Custom provider registration         |
| `profile:{name}`                       | JSON (Profile)                | None                 | Profile definition                   |
| `profile_token:{token}`                | STRING (profile name)         | Optional (token TTL) | Token-to-profile lookup              |
| `profile_tokens:{name}`                | HASH {key: ProfileToken JSON} | None                 | All tokens for a profile             |
| `usage:hourly:{YYYY-MM-DDTHH}`         | HASH {field: value}           | 48h                  | Hourly usage aggregation             |
| `usage:daily:{YYYY-MM-DD}`             | HASH {field: value}           | 35d                  | Daily usage aggregation              |
| `usage:monthly:{YYYY-MM-01}`           | HASH {field: value}           | 400d                 | Monthly usage aggregation            |
| `usage:sessions:{YYYY-MM-DD}`          | HASH {sessionID: model}       | 35d                  | Active sessions by day               |
| `usage:profile:{name}:daily:{date}`    | HASH {field: value}           | 35d                  | Per-profile daily usage              |
| `usage:profile:{name}:summary`         | HASH {field: value}           | None                 | Per-profile cumulative totals        |
| `usage:account:{id}:daily:{date}`      | HASH {field: value}           | 35d                  | Per-account daily usage              |
| `usage:account:{id}:summary`           | HASH {field: value}           | None                 | Per-account cumulative totals        |
| `config:overrides`                     | JSON                          | None                 | Runtime config overrides             |
| `config:thinking`                      | JSON (ThinkingConfig)         | None                 | Thinking/reasoning config            |
| `config:global-env`                    | JSON (GlobalEnv)              | None                 | Global environment overrides         |
| `config:max-tokens`                    | JSON (MaxTokensConfig)        | None                 | Per-model max token overrides        |
| `quota:{provider}:{accountId}`         | JSON (QuotaResult)            | 30s (QuotaCacheTTL)  | Cached quota computation             |

### 16.2 Token Storage (`arl:tokens:{provider}:{accountID}`)

**Value**: JSON-serialized `TokenInfo` struct (see Section 3.1).

```
Key:   arl:tokens:anthropic:acc_abc123
Value: {
  "access_token": "sk-ant-...",
  "refresh_token": "...",
  "expiry_date": "2026-05-03T12:00:00Z",
  "email": "user@example.com",
  "account_id": "acc_abc123",
  "provider": "anthropic",
  "project_id": "",
  "tier": "oauth",
  "paused": false,
  "is_default": false,
  "created_at": "2026-01-15T08:30:00Z",
  "scopes": "user:inference,user:profile"
}
```

**Index key**: `arl:tokens:anthropic:_index` is a Redis SET containing all accountIDs for the provider. Updated atomically when tokens are saved/deleted.

**Operations**:
- `SET arl:tokens:{provider}:{accountID} <json>` - Save token
- `SADD arl:tokens:{provider}:_index <accountID>` - Add to index
- `SMEMBERS arl:tokens:{provider}:_index` - List all accounts
- `GET arl:tokens:{provider}:{accountID}` - Get single token
- `DEL arl:tokens:{provider}:{accountID}` - Delete token
- `SREM arl:tokens:{provider}:_index <accountID>` - Remove from index

### 16.3 Rate Limit State (`arl:ratelimit:{provider}:{accountID}`)

**Value**: JSON-serialized rate limit tracking data. 6-hour TTL auto-expires stale state.

```
Key:   arl:ratelimit:anthropic:acc_abc123
Value: {
  "requestsRemaining": 35,
  "totalRequests": 100,
  "lastRequest": 1699999500,
  "consecutive429s": 0,
  "backoffUntil": 0
}
TTL:   21600s (6h)
```

**Operations**:
- `SET arl:ratelimit:{provider}:{accountID} <json> EX 21600` - Save with TTL
- `GET arl:ratelimit:{provider}:{accountID}` - Read state
- Pattern: read-modify-write cycle on each request

### 16.4 Profile Storage

Profiles use three related key patterns:

```
Key:   profile:default
Value: {"name":"default","targets":[...],"tokens":[...],"createdAt":...,"updatedAt":...}

Key:   profile_token:arl_ab12cd34ef56
Value: "default"
TTL:   (optional, set per-token)

Key:   profile_tokens:default
Value: HASH {
  "arl_ab12cd34ef56": "{\"token\":\"arl_ab12cd34ef56\",\"name\":\"API Key 1\",\"createdAt\":...}"
}
```

**Operations**:
- `GET profile:{name}` - Get profile
- `SET profile:{name} <json>` - Save profile
- `DEL profile:{name}` - Delete profile
- `GET profile_token:{token}` - Resolve token to profile name
- `SET profile_token:{token} <name> [EX <ttl>]` - Register token
- `DEL profile_token:{token}` - Revoke token
- `HSET profile_tokens:{name} <key> <json>` - Add token to profile
- `HDEL profile_tokens:{name} <key>` - Remove token from profile
- `HGETALL profile_tokens:{name}` - List all tokens

### 16.5 Usage Tracking (HASH fields)

All usage hashes use a consistent field naming convention:

```
HASH field format: {model}:{metric}
  metric = requests | input | output | cost | errors

Examples:
  glm-5:requests  = "42"
  glm-5:input     = "156789"
  glm-5:output    = "34567"
  glm-5:cost      = "1.2345"
  glm-5:errors    = "2"
  claude-3-5-sonnet:requests = "15"
```

**Hourly buckets**:
```
Key:   usage:hourly:2026-05-03T14
TTL:   48h
```

**Daily buckets**:
```
Key:   usage:daily:2026-05-03
TTL:   35d
```

**Monthly buckets**:
```
Key:   usage:monthly:2026-05-01
TTL:   400d
```

**Session tracking**:
```
Key:   usage:sessions:2026-05-03
Field: session-uuid-1234 = "glm-5"
TTL:   35d
```

**Operations**:
- `HINCRBY usage:hourly:{hour} {model}:requests 1` - Increment request count
- `HINCRBYFLOAT usage:hourly:{hour} {model}:cost 0.05` - Add cost
- `HGETALL usage:daily:{date}` - Read all daily metrics
- `EXPIRE usage:hourly:{hour} 172800` - Set TTL on first write

### 16.6 Per-Profile Usage

```
Key:   usage:profile:team-a:daily:2026-05-03
TTL:   35d
Field format: same {model}:{metric} convention

Key:   usage:profile:team-a:summary
TTL:   None (permanent cumulative totals)
```

### 16.7 Per-Account Usage

```
Key:   usage:account:acc_abc123:daily:2026-05-03
TTL:   35d
Field format: same {model}:{metric} convention

Key:   usage:account:acc_abc123:summary
TTL:   None (permanent cumulative totals)
```

### 16.8 Config Overrides

```
Key:   config:overrides
Value: JSON map of config field overrides
TTL:   None

Key:   config:thinking
Value: {"enabled":true,"budgetTokens":10000,"maxTokens":16000}
TTL:   None

Key:   config:global-env
Value: {"DEFAULT_MODEL":"glm-5","DEFAULT_TEMPERATURE":"0.5"}
TTL:   None

Key:   config:max-tokens
Value: {"glm-5":8192,"claude-3-5-sonnet":4096}
TTL:   None
```

**Operations**:
- `GET config:overrides` / `SET config:overrides <json>`
- `GET config:thinking` / `SET config:thinking <json>`
- Individual config API handlers read/write these keys

### 16.9 Quota Cache

```
Key:   quota:anthropic:acc_abc123
Value: {
  "provider": "anthropic",
  "accountId": "acc_abc123",
  "dailyBudget": 57600,
  "usedTokens": 23456,
  "usedPct": 40.8,
  "blocked": false,
  "blockedUntil": 0
}
TTL:   30s (configurable via QuotaCacheTTL)
```

**Operations**:
- `GET quota:{provider}:{accountId}` - Check cache
- `SET quota:{provider}:{accountId} <json> EX 30` - Cache result
- Cache-aside pattern: compute on miss, cache on write

### 16.10 Custom Providers

```
Key:   arl:providers:custom:my-provider
Value: {
  "id": "my-provider",
  "name": "My Custom Provider",
  "baseURL": "https://api.example.com",
  "authType": "api_key",
  "apiKey": "sk-...",
  "models": ["model-a", "model-b"],
  "headers": {"X-Custom": "value"},
  "enabled": true,
  "rateLimit": 100,
  "priority": 50,
  "createdAt": 1700000000
}
TTL:   None
```

**Operations**:
- `SET arl:providers:custom:{id} <json>` - Register provider
- `GET arl:providers:custom:{id}` - Get provider
- `DEL arl:providers:custom:{id}` - Remove provider
- `KEYS arl:providers:custom:*` - List all custom providers

### 16.11 Key Lifecycle Summary

| Category         | Write Frequency                | Read Frequency              | TTL Strategy      |
|------------------|--------------------------------|-----------------------------|-------------------|
| Tokens           | On refresh (30m) / auth events | Every request (routing)     | None (persistent) |
| Rate limits      | Every request                  | Every request               | 6h auto-expire    |
| Profiles         | CRUD operations                | Every authenticated request | None (persistent) |
| Usage (hourly)   | Every request                  | Dashboard queries           | 48h expiry        |
| Usage (daily)    | Every request                  | Dashboard queries           | 35d expiry        |
| Usage (monthly)  | Every request                  | Dashboard queries           | 400d expiry       |
| Config overrides | Admin API                      | Every request               | None (persistent) |
| Quota cache      | On compute                     | Every request               | 30s short TTL     |

### 16.12 Query Patterns

**Request routing flow** (per incoming request):
1. `GET profile_token:{bearer_token}` -> resolve profile name
2. `GET profile:{name}` -> get profile targets
3. `SMEMBERS arl:tokens:{provider}:_index` -> list available accounts
4. `GET arl:tokens:{provider}:{accountID}` -> get token for selected account
5. `GET arl:ratelimit:{provider}:{accountID}` -> check rate limit state

**Usage recording** (after response):
1. `HINCRBY usage:hourly:{hour} {model}:requests 1`
2. `HINCRBY usage:hourly:{hour} {model}:input {n}`
3. `HINCRBY usage:hourly:{hour} {model}:output {n}`
4. `HINCRBYFLOAT usage:hourly:{hour} {model}:cost {cost}`
5. Same pattern for daily, monthly, profile, account buckets

**Token refresh cycle** (every 30 minutes):
1. `SMEMBERS arl:tokens:{provider}:_index` for each provider
2. `GET arl:tokens:{provider}:{id}` for each account
3. Refresh token via OAuth/provider API
4. `SET arl:tokens:{provider}:{id} <updated-json>`
