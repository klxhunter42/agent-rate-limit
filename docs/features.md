# Gateway Features Reference

> Comprehensive inventory of all implemented features in the API gateway.

---

## Architecture

```
Claude Code / Client
        |
   [HTTPS]
        |
  +-----------+     +------------------+     +-------------------+
  | Middleware | --> | Handler (Router) | --> | Proxy (Upstream)  |
  +-----------+     +------------------+     +-------------------+
        |                   |                        |
  rate limit          privacy mask            format conversion
  IP filter           token optimize          retry + recovery
  adaptive limit      quota check             streaming SSE
  anomaly detect      profile routing         key rotation
        |                   |                        |
  +-----------------------------------------------+
  |              Dragonfly (Redis)                 |
  |  profiles, tokens, usage, quota, optimizer    |
  +-----------------------------------------------+
        |
  Prometheus (/metrics) + OTLP (traces)
```

---

## 1. Multi-Provider Proxy

Routes AI requests across 17+ providers with automatic format conversion.

### Supported Providers

| Provider | Auth | Format | Proxy File |
|---|---|---|---|
| Anthropic | API Key / OAuth PKCE | Anthropic Messages API | `proxy/anthropic.go` |
| Claude OAuth | OAuth PKCE | Anthropic Messages API | `proxy/anthropic.go` |
| Claude Session | Session Cookie | Anthropic Messages API | `proxy/claude-session.go` |
| OpenAI | API Key | OpenAI Chat Completions | `proxy/openai.go` |
| Gemini API Key | API Key | Gemini GenerateContent | `proxy/gemini-apikey.go` |
| Gemini OAuth | OAuth PKCE | Gemini GenerateContent | `proxy/gemini-apikey.go` |
| Gemini Code Assist | OAuth PKCE | Code Assist envelope | `proxy/gemini-codeassist.go` |
| Z.AI (GLM) | API Key (pool) | Anthropic format | `proxy/anthropic.go` |
| OpenRouter | API Key | OpenAI format | `proxy/openai.go` |
| Qwen | API Key | OpenAI format | `proxy/openai.go` |
| DeepSeek | API Key | OpenAI format | `proxy/openai.go` |
| Kimi | API Key | OpenAI format | `proxy/openai.go` |
| HuggingFace | API Key | OpenAI format | `proxy/openai.go` |
| Ollama | API Key | OpenAI format | `proxy/openai.go` |
| Cursor | OAuth Device Code | OpenAI format | `proxy/openai.go` |
| AGY | API Key | OpenAI format | `proxy/openai.go` |
| CodeBuddy | API Key | OpenAI format | `proxy/openai.go` |

### Provider Resolution

- Model name prefix routing (e.g. `claude-` -> Anthropic, `gpt-` -> OpenAI, `gemini-` -> Gemini)
- Round-robin across active tokens in pool
- Provider cooldown on 429 (2 min)
- Per-provider rate limit header caching (5h/7d utilization)
- Configurable via `PROVIDER_MODEL_PREFIXES`

### Key Env Vars

| Variable | Default | Description |
|---|---|---|
| `UPSTREAM_URL` | `https://api.z.ai/api/anthropic` | Default upstream URL |
| `UPSTREAM_MAX_RETRIES` | `3` | Max 429 retries |
| `UPSTREAM_RETRY_BACKOFF` | `500ms` | Base backoff (exponential, cap 5 min) |
| `STREAM_TIMEOUT` | `300s` | SSE streaming timeout |
| `GLM_MODE` | `true` | Z.AI features active |
| `ANTHROPIC_API_VERSION` | `2023-06-01` | Anthropic API version header |

---

## 2. Error Recovery & Retry

Auto-recovers from upstream errors without surfacing them to clients.
All retry paths emit structured log entries for observability.

### Retry Flow

```
Request
   |
   v
Attempt 0 (fresh request)
   |
   +-- 200 OK --> return response
   |
   +-- 429 Rate Limited --> backoff (exponential, cap 5min)
   |       +-- rotate API key (if pool has alternatives)
   |       +-- increment upstream_429_total metric
   |       +-- log: "upstream retry" reason=429 rate limited
   |       +-- retry (up to UPSTREAM_MAX_RETRIES)
   |
   +-- 401 Auth Error --> refresh OAuth token
   |       +-- retry once with new token
   |       +-- log: "upstream retry with refreshed token"
   |       +-- if refresh fails --> return 401 to client
   |
   +-- 500/502/503/529 Transient --> backoff
   |       +-- log: "upstream retry transient error" + response snippet
   |       +-- increment transient_retry_total metric
   |       +-- retry (up to TRANSIENT_RETRY_MAX)
   |
   +-- 400/413/422 Context Overflow --> auto-truncate
   |       +-- drop oldest messages, keep system prompt + recent
   |       +-- log: "upstream retry after auto-truncation"
   |       +-- increment context_truncation_total metric
   |       +-- retry once
   |
   +-- Other error --> return error to client
   |
   v
All retries exhausted --> log: "upstream all retries exhausted" + last_status
```

### Recovery Actions

| Error | Trigger | Recovery | Log Message | Metric |
|---|---|---|---|---|
| Rate limit | 429 | Rotate key, exponential backoff (cap 5min) | `upstream retry` | `upstream_retries_total`, `upstream_429_total` |
| Auth expired | 401 (OAuth) | Refresh token, retry once | `upstream retry with refreshed token` | - |
| Transient | 500/502/503/529 | Backoff, retry up to N times | `upstream retry transient error` | `transient_retry_total` |
| Context overflow | 400/413/422 + keywords | Truncate oldest, retry once | `upstream retry after auto-truncation` | `context_truncation_total` |
| Success after retry | 200 after >0 attempts | Return response | `upstream retry success` | - |
| Exhausted | All retries failed | Return error to client | `upstream all retries exhausted` | - |

### Backoff Formula

```
backoff = UPSTREAM_RETRY_BASE_BACKOFF * attempt^2
cap at 5 minutes
```

Example with default 500ms base:
| Attempt | Backoff |
|---|---|
| 1 | 500ms |
| 2 | 2s |
| 3 | 4.5s |
| 4+ | 5min (capped) |

### Context Truncation

- Targets 75% of (contextWindow - maxOutputTokens) for messages
- Preserves system prompt + newest messages, drops oldest
- Keeps tool_use/tool_result pairs at truncation boundary
- Max 1 truncation attempt per request
- Appends note: `[Note: older conversation messages were truncated to fit context window limits.]`

### Structured Log Fields

All retry log entries include:

| Field | Description |
|---|---|
| `attempt` | Current attempt number (1-based) |
| `backoff` | Wait duration before this attempt |
| `model` | Target model name |
| `reason` | Why the retry happened (e.g. "429 rate limited") |
| `max_attempts` | Total attempts allowed |
| `status` | HTTP status that triggered retry (where applicable) |
| `response` | First 200 chars of error response body (transient only) |
| `rtt` | Round-trip time of successful attempt (success log only) |

### Key Env Vars

| Variable | Default | Description |
|---|---|---|
| `UPSTREAM_MAX_RETRIES` | `3` | Max 429 retry attempts |
| `UPSTREAM_RETRY_BASE_BACKOFF` | `500ms` | Base backoff for exponential retry |
| `ENABLE_AUTO_TRUNCATE` | `true` | Enable context window recovery |
| `TRANSIENT_RETRY_MAX` | `2` | Max transient error retries |

### Metrics

- `api_gateway_context_truncation_total{model}`
- `api_gateway_transient_retry_total{status, model}`

---

## 3. API Key Pool Rotation

Multi-key rotation for providers with per-key RPM limits.

### Features

- Per-key RPM tracking with configurable limit
- Round-robin and fill-first strategies
- 10s cooldown on 429 per key
- `sync.Cond` for efficient waiting when all keys exhausted
- `SyncFromStore()` preserves state across restarts
- Z.AI: auto-syncs provider tokens from Redis every 30s

### Key Env Vars

| Variable | Default | Description |
|---|---|---|
| `UPSTREAM_API_KEYS` | (empty) | Comma-separated API keys |
| `UPSTREAM_RPM_LIMIT` | `40` | Per-key requests per minute |

---

## 4. Token Optimization Pipeline

13-stage system prompt optimization. Each stage can be enabled/disabled independently.

### Pipeline Stages

```
System Prompt
     |
     v
1. Semantic Dedup (similarity.go) - Jaccard sentence dedup (threshold 0.7)
     |
     v
2. Exact Dedup (optimizer.go) - Identical sentence removal
     |
     v
3. Whitespace Optimize (optimizer.go) - Collapse whitespace, preserve code blocks
     |
     v
4. Split Code Blocks
     |
     v
5. Chunker - Rabin-Karp content chunking, stable-first reorder
6. Delta Encoding - LCS diff against cached baseline
7. Sketch Dedup - 1-bit sketch near-duplicate detection
     |
     v
8. Summarizer (red budget only) - Extractive summarization (max 30%)
     |
     v
9. Packer - Knapsack selection within token budget
10. Disclosure - 3-layer progressive disclosure (index/FTS/full)
     |
     v
11. Intent Filter - Classify intent, filter response format
     |
     v
12. Caveman - System prompt injection for output compression
     |
     v
13. Post-Proxy Feedback - Prefetcher, waste detection, cache ROI, bandit reward
```

### Per-Stage Config

| Stage | Env Vars | Default |
|---|---|---|
| Semantic Dedup | always on | threshold 0.7 |
| Whitespace | `ENABLE_RESPONSE_TRIM` | `true` |
| Chunker | `CHUNKER_ENABLED`, `CHUNKER_MIN_CHUNK`, `CHUNKER_MAX_CHUNK` | `true`, 128, 4096 |
| Delta | `DELTA_ENABLED`, `DELTA_MIN_SAVINGS_PCT` | `true`, 10% |
| Sketch | `SKETCH_ENABLED`, `SKETCH_DIMENSIONS`, `SKETCH_THRESHOLD` | `true`, 128, 0.85 |
| Summarizer | `SUMMARIZER_ENABLED`, `SUMMARIZER_MAX_RATIO` | `true`, 0.3 |
| Packer | `PACKER_ENABLED`, `PACKER_MIN_UTILITY` | `true`, 0.1 |
| Disclosure | `DISCLOSURE_ENABLED` | `true` |
| Intent Filter | `FILTER_ENABLED` | `true` |
| Caveman | `CAVEMAN_ENABLED`, `CAVEMAN_AUTO_DETECT` | `true`, true |
| Warmstart | `WARMSTART_ENABLED` | `true` |
| Prefetcher | `PREFETCHER_ENABLED` | `true` |
| Bandit | `BANDIT_ENABLED` | `true` |
| Cache Eviction | `CACHE_EVICTION_ENABLED` | `true` |
| Waste Detection | `WASTE_ENABLED` | `true` |

### Model Capabilities

Token estimation aware of 15 models with different context windows and max output tokens.

---

## 5. Privacy Pipeline

Detects and masks secrets and PII in requests, restores in streaming responses.

### Detection

**Secret Patterns (regex, 14 patterns):**
- OpenSSH keys, PEM keys, sk-* keys, AKIA keys
- GitHub (ghp_*, gho_*, ghr_*, ghs_*), GitLab (glpat-*)
- JWT tokens, Bearer tokens
- PASSWORD/SECRET env vars, connection strings
- Thai 13-digit national IDs

**PII Detection (external Presidio service):**
- PERSON, EMAIL_ADDRESS, PHONE_NUMBER
- Score threshold: 0.7
- Language: en

### Masking

- Replace with `[[ENTITY_N]]` placeholders (backward scan to preserve indices)
- Conflict resolution: longer span wins, same-type PII merged
- StreamUnmasker: handles placeholders split across SSE chunks
- Max scan: 200K chars

### Key Env Vars

| Variable | Default | Description |
|---|---|---|
| `PASTEGUARD_ENABLED` | `true` | Master toggle |
| `PASTEGUARD_SECRETS_ENABLED` | `true` | Secret detection |
| `PASTEGUARD_PII_ENABLED` | `true` | PII detection |
| `PASTEGUARD_PII_SCORE_THRESHOLD` | `0.7` | Minimum PII confidence |
| `PASTEGUARD_PII_ENTITIES` | `PERSON,EMAIL_ADDRESS,PHONE_NUMBER` | PII entity types |
| `PASTEGUARD_PRESIDIO_URL` | `http://arl-presidio:3000` | Presidio analyzer URL |
| `PASTEGUARD_MAX_SCAN_CHARS` | `200000` | Max chars to scan |

### Metrics

- `api_gateway_mask_duration_seconds`
- `api_gateway_secrets_detected_total`
- `api_gateway_pii_detected_total`
- `api_gateway_mask_requests_total`

---

## 6. Middleware

| Middleware | Description | Key Config |
|---|---|---|
| Security Headers | nosniff, DENY framing, XSS block, CSP | always on |
| Correlation ID | Propagate or generate UUID | always on |
| Real IP | CF-Connecting-IP > X-Real-IP > X-Forwarded-For | always on |
| IP Filter | Whitelist/blacklist with CIDR support | `IP_WHITELIST`, `IP_BLACKLIST` |
| Rate Limit | Distributed via external rate-limiter service | `RATE_LIMITER_ADDR`, `GLOBAL_RATE_LIMIT`, `AGENT_RATE_LIMIT` |
| Adaptive Concurrency | Gradient-based per-model concurrency control | `UPSTREAM_MODEL_LIMITS`, `DEFAULT_LIMIT`, `GLOBAL_LIMIT` |
| Anomaly Detection | Welford's Z-score on latency, spike/drop/sustained | `ANOMALY_COOLDOWN_SEC`, `ANOMALY_Z_THRESHOLD` |
| Structured Logging | JSON: method, path, status, duration_ms, agent_id | always on |
| Runtime Metrics | Go goroutines, heap, GC, stack; Dragonfly health | 10s collection |
| Dashboard Auth | x-api-key or arl_session cookie | `DASHBOARD_API_KEY` |
| Login Limiter | 5 attempts per 15 min per IP | always on |
| Config Watcher | fsnotify on .env, WebSocket broadcast | always on |

### Adaptive Limiter Details

- On 429: limit *= 0.5 (min 1)
- On 200 every 5 successes: gradient formula with minRTT + buffer
- 5s cooldown after 429
- Model series extraction (glm-5 -> series 5)
- Cross-series spillover at 70% utilization
- `sync.Cond` signal-based waiting
- Manual overrides via config API

---

## 7. Profile System

Multi-target routing with per-profile configuration.

### Features

- Full CRUD: name, baseURL, APIKey, model, target, provider, accountIDs, passthroughAuth
- Copy, export (secrets redacted), import
- Named API tokens (arl_* prefix) with optional TTL
- Routing via `X-Profile` header or arl_* token
- Recommended models per provider (10 providers)
- Redis-backed persistence

### API Endpoints

| Method | Path | Description |
|---|---|---|
| GET/POST/PUT/DELETE | `/v1/profiles` | Profile CRUD |
| POST | `/v1/profiles/copy` | Copy profile |
| GET | `/v1/profiles/export` | Export (redacted) |
| POST | `/v1/profiles/import` | Import |
| POST | `/v1/profiles/delete` | Delete by name (handles special chars) |

---

## 8. OAuth Management

Multi-provider OAuth with token lifecycle management.

### Auth Flows

| Flow | Providers |
|---|---|
| Authorization Code + PKCE | Claude, Gemini, Google |
| Device Code | Cursor |
| Session Cookie | Claude (claude.ai) |

### Token Lifecycle

- RefreshWorker: 30-min cycle, refreshes tokens expiring within 45 min
- 3-attempt retry with exponential backoff
- Provider-specific refresh (Claude JSON body, others form-urlencoded)
- Account rotation on 429
- GetFromPool: selects account with lowest 5h utilization

### API Endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/v1/providers` | List all providers |
| POST | `/v1/providers/{id}/auth/start` | Start OAuth flow |
| POST | `/v1/providers/{id}/auth/poll` | Poll auth status |
| POST | `/v1/providers/{id}/auth/cancel` | Cancel auth |
| GET | `/v1/providers/{id}/accounts` | List accounts |
| DELETE | `/v1/providers/{id}/accounts/{aid}` | Remove account |
| POST | `/v1/providers/{id}/accounts/{aid}/pause` | Pause account |
| POST | `/v1/providers/{id}/accounts/{aid}/resume` | Resume account |

---

## 9. Metrics

All metrics under namespace `api_gateway`.

### Request Metrics

| Metric | Type | Labels |
|---|---|---|
| `request_latency_seconds` | histogram | method, path, status |
| `active_connections` | gauge | - |
| `error_total` | counter | type |
| `rate_limit_hits_total` | counter | key (agent keys SHA1-hashed) |

### Token & Cost Metrics

| Metric | Type | Labels |
|---|---|---|
| `token_input_total` | counter | model |
| `token_output_total` | counter | model |
| `cost_total` | counter | model, type (input/output) |
| `ttfb_seconds` | histogram | model |
| `model_fallback_total` | counter | requested, selected |

### Profile Metrics

| Metric | Type | Labels |
|---|---|---|
| `profile_requests_total` | counter | profile, model |
| `profile_token_input_total` | counter | profile, model |
| `profile_token_output_total` | counter | profile, model |
| `profile_cost_total` | counter | profile, model, type |

### Concurrency Metrics

| Metric | Type | Labels |
|---|---|---|
| `adaptive_limit` | gauge | model |
| `adaptive_in_flight` | gauge | model |
| `upstream_retries_total` | counter | - |
| `upstream_429_total` | counter | - |
| `queue_depth` | gauge | - |

### Optimizer Metrics

| Metric | Type | Labels |
|---|---|---|
| `optimizer_chars_saved_total` | counter | technique |
| `optimizer_runs_total` | counter | technique |
| `optimizer_duration_seconds` | histogram | technique |
| `optimizer_tokens_saved_total` | counter | - |

### Recovery Metrics

| Metric | Type | Labels |
|---|---|---|
| `context_truncation_total` | counter | model |
| `transient_retry_total` | counter | status, model |

### Retry Log Messages

| Log Level | Message | When |
|---|---|---|
| WARN | `upstream retry` | 429 received, about to backoff and retry |
| INFO | `upstream retry key rotation` | 429 triggered key rotation in pool |
| WARN | `upstream retry with refreshed token` | 401 triggered OAuth token refresh |
| WARN | `upstream retry transient error` | 500/502/503/529, retrying |
| WARN | `upstream retry after auto-truncation` | Context overflow, truncated messages |
| INFO | `upstream retry success` | Request succeeded after retry (attempt > 0) |
| ERROR | `upstream all retries exhausted` | All retry attempts failed |

### Budget Metrics

| Metric | Type | Labels |
|---|---|---|
| `budget_level` | gauge | model (0=green, 1=yellow, 2=red) |
| `cost_savings_total` | counter | - |

### Runtime Metrics

| Metric | Type | Labels |
|---|---|---|
| `go_goroutines` | gauge | - |
| `go_heap_alloc_bytes` | gauge | - |
| `go_heap_objects` | gauge | - |
| `go_gc_pause_ns` | gauge | - |
| `go_stack_inuse_bytes` | gauge | - |
| `dragonfly_up` | gauge | - |
| `anomaly_total` | counter | severity (medium/high/critical) |

---

## 10. Dashboard & APIs

### API Endpoints

| Method | Path | Description |
|---|---|---|
| POST | `/v1/messages` | Main proxy endpoint |
| GET | `/v1/models` | Model catalog (45+ models) |
| GET | `/metrics` | Prometheus metrics |
| GET/PUT | `/v1/config` | Runtime config |
| GET/PUT | `/v1/thinking` | Thinking budget per model |
| GET/PUT | `/v1/global-env` | Env overrides |
| GET/PUT | `/v1/max-tokens` | Per-model max tokens override |
| GET | `/v1/usage/*` | Usage analytics (hourly/daily/monthly/summary) |
| GET | `/v1/quota` | Quota check |
| GET | `/v1/overview` | Dashboard overview |
| GET | `/health` | Health checks |
| GET | `/v1/waste/findings` | Waste detection findings |
| WS | `/ws` | WebSocket event hub |

### Static Dashboard

- Embedded Vite SPA via `//go:embed all:static`
- SPA fallback for `/admin/*` sub-routes
- Real-time config changes via WebSocket

---

## 11. Format Conversion

Bidirectional conversion between API formats:

- **Anthropic <-> OpenAI**: Full message format, streaming SSE chunk conversion
- **Anthropic <-> Gemini**: Complete format conversion including tool use
- **Anthropic <-> Code Assist**: Wraps in Code Assist envelope with model mapping

---

## 12. Streaming

- SSE streaming with 10-min default timeout
- SSE buffer pool (`sync.Pool`, 512KB max cap)
- Response header allowlist (security: no header injection)
- Thai language detection triggers prompt optimization
- Smart max_tokens clamping per model

---

## 13. Infrastructure

### Shared Transport

- DNS cache (30s TTL)
- 200 max idle connections
- HTTP/2 enabled
- 30s TLS handshake timeout

### Graceful Shutdown

- SIGINT/SIGTERM with 10s timeout
- Server WriteTimeout=0 for SSE streaming

### OpenTelemetry

- OTLP gRPC exporter
- Batched span processing
- Gracefully disabled if endpoint unreachable
- Env: `OTLP_ENDPOINT`

### Quota System

- Per-provider/account daily budget tracking
- Redis caching (30s TTL)
- Fail-open on errors
- Env: `QUOTA_DAILY_BUDGET` (57600), `QUOTA_BLOCK_PCT` (95)
