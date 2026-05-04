# API Gateway Core Reference

Auto-generated documentation from source analysis. Covers entry point, handler, proxy, middleware, configuration, and request flow.

---

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [Main Entry Point](#2-main-entry-point)
3. [HTTP Handler Layer](#3-http-handler-layer)
4. [Proxy Layer](#4-proxy-layer)
5. [Middleware Chain](#5-middleware-chain)
6. [Configuration](#6-configuration)
7. [Supporting Packages](#7-supporting-packages)
8. [Request Flow Diagram](#8-request-flow-diagram)
9. [Docker Compose Services](#9-docker-compose-services)

---

## 1. Architecture Overview

The API gateway is a multi-provider AI proxy written in Go. It sits between AI clients (Claude Code CLI, custom agents, web apps) and upstream AI providers (Anthropic, OpenAI, Gemini, Z.AI, OpenRouter, Copilot, Qwen, DeepSeek, Kimi, Lotus, and more).

Key capabilities:
- Multi-provider routing with automatic format conversion (Anthropic/OpenAI/Gemini)
- Adaptive concurrency limiting per model with cross-series fallback
- Profile-based routing with account pool rotation and round-robin
- Transparent OAuth passthrough for Claude Code CLI
- 13-stage token optimization pipeline (chunker, packer, delta, sketch, bandit, etc.)
- Privacy masking with PII/secret detection and SSE stream unmasking
- Billing header injection (Go direct vs Node.js sidecar fallback)
- HMAC-SHA256 signed Z.AI web chat API proxy
- MCP (Model Context Protocol) JSON-RPC proxy with caching
- WebSocket hub for real-time dashboard events
- Redis/Dragonfly-backed config, usage tracking, quota enforcement
- Context window recovery (auto-truncation on overflow)

```
+------------------+     +-----------------+     +-------------------+
|  Claude Code CLI |     |  Custom Agents  |     |  Web Dashboard    |
+--------+---------+     +--------+--------+     +--------+----------+
         |                        |                       |
         v                        v                       v
+------------------------------------------------------------------+
|                     Caddy Reverse Proxy (:9000)                   |
+-------------------------------+----------------------------------+
                                |
                                v
+------------------------------------------------------------------+
|                      API Gateway (:8080)                          |
|  +------------+  +------------+  +------------+  +-------------+  |
|  | Middleware |->| Handler    |->| Resolver   |->| Proxy Layer |  |
|  | Chain      |  | (Routes)   |  | (Routing)  |  | (Upstream)  |  |
|  +------------+  +------------+  +------------+  +-------------+  |
|  +------------+  +------------+  +------------+  +-------------+  |
|  | Privacy    |  | Optimizer  |  | Adaptive   |  | Key Pool    |  |
|  | Pipeline   |  | Pipeline   |  | Limiter    |  | (Rotation)  |  |
|  +------------+  +------------+  +------------+  +-------------+  |
+--------+-----------+-----------+-----------+-----------+----------+
         |           |           |           |           |
         v           v           v           v           v
+------------------------------------------------------------------+
|  Dragonfly/Redis  |  Rate Limiter  |  Prometheus  |  OTel Collector|
+------------------------------------------------------------------+
         |           |           |
         v           v           v
+------------------------------------------------------------------+
|  Z.AI  |  Anthropic  |  OpenAI  |  Gemini  |  OpenRouter  | ...  |
+------------------------------------------------------------------+
```

---

## 2. Main Entry Point

**File:** `main.go` (~503 lines)

### Startup Sequence

```
main()
  -> slog.NewJSONHandler (structured JSON logging)
  -> config.Load() (all env vars with defaults)
  -> initTracer() (OpenTelemetry OTLP gRPC exporter)
  -> queue.NewDragonflyClient() (Redis connection pool, ping test)
  -> metrics.New() (Prometheus registry with pricing map)
  -> middleware.NewRuntimeMetrics() (goroutine, heap, GC gauges)
  -> middleware.NewAnomalyDetector() (Welford's online algorithm)
  -> privacy.NewPipeline() (secrets + PII detection)
  -> proxy.New*Proxy() (anthropic, gemini-codeassist, openai, gemini-api, zaiweb)
  -> middleware.NewAdaptiveLimiter() (per-model concurrency slots)
  -> proxy.NewKeyPool() (multi-key rotation with RPM tracking)
  -> provider.New*() (registry, token store, auth handler, resolver, refresh worker)
  -> handler.New*() (handler, overview, config, profile, usage, quota)
  -> Optimizer init (13 packages: chunker, packer, disclosure, prefetcher, bandit, summarizer, delta, sketch, waste, filter, cache, warmstart, caveman)
  -> chi.NewRouter() with middleware chain
  -> Route registration (all endpoints)
  -> http.Server (WriteTimeout=0 for SSE streaming)
  -> signal.Notify for graceful shutdown (10s timeout)
```

### Background Goroutines

| Goroutine | Source | Purpose |
|-----------|--------|---------|
| `rtMetrics.Start()` | `main.go:88` | Runtime metrics collection every 10s |
| `wsHub.Run()` | `main.go:125` | WebSocket hub event loop |
| `refreshWorker.Start()` | `main.go:221` | OAuth token refresh loop |
| `WatchSessionSecret()` | `main.go:226` | Session secret file watcher |
| `cfgWatcher.Start()` | `main.go:232` | .env file change detection |
| `syncZAIKeys()` + ticker | `main.go:236-243` | Key pool sync from token store every 30s |
| Adaptive metrics export | `main.go:247-261` | Limiter state to Prometheus every 10s |
| `optWaste.StartBackgroundScanner()` | `main.go:210` | Waste detection scan every 60s |
| `optCache.StartEvictionLoop()` | `main.go:213` | Cache eviction loop |

### Server Configuration

```go
srv := &http.Server{
    Addr:         cfg.ServerPort,     // default ":8080"
    Handler:      r,                  // chi router
    ReadTimeout:  cfg.ReadTimeout,    // default 30s
    WriteTimeout: 0,                  // disabled for SSE streaming
    IdleTimeout:  120 * time.Second,
}
```

### Dependencies (`go.mod`)

| Dependency | Version | Purpose |
|------------|---------|---------|
| `go-chi/chi/v5` | 5.2.1 | HTTP router |
| `redis/go-redis/v9` | 9.7.3 | Dragonfly/Redis client |
| `prometheus/client_golang` | 1.21.1 | Metrics |
| `gorilla/websocket` | 1.5.3 | WebSocket hub |
| `fsnotify/fsnotify` | 1.9.0 | Config file watching |
| `google/uuid` | 1.6.0 | Correlation IDs |
| `go.opentelemetry/otel` | 1.33.0 | Distributed tracing |
| `golang.org/x/sync` | 0.20.0 | errgroup for parallel rate limit checks |
| `go.uber.org/automaxprocs` | 1.6.0 | Auto GOMAXPROCS in containers |

---

## 3. HTTP Handler Layer

### 3.1 Handler Struct

**File:** `handler/handler.go` (~2293 lines)

```go
type Handler struct {
    queue              *queue.DragonflyClient
    metrics            *metrics.Metrics
    anthropicProxy     *proxy.AnthropicProxy
    geminiCodeAssist   *proxy.GeminiCodeAssistProxy
    openAIProxy        *proxy.OpenAIProxy
    geminiAPIProxy     *proxy.GeminiAPIProxy
    zaiWebProxy        *proxy.ZAIWebProxy
    mcpProxy           *proxy.MCPProxy
    modelLimiter       *middleware.AdaptiveLimiter
    keyPool            *proxy.KeyPool
    cfg                *config.Config
    privacyPipeline    *privacy.Pipeline
    tokenStore         *provider.TokenStore
    resolver           *provider.Resolver
    anomalyDetector    *middleware.AnomalyDetector
    usageHandler       *UsageHandler
    quotaHandler       *QuotaHandler
    profileRedis       *redis.Client
    wsBroadcast        handler.BroadcastFunc
    refreshWorker      *provider.RefreshWorker
    optimizers         *Optimizers
    sessionManager     *proxy.SessionManager
}
```

### 3.2 Route Table

| Method | Path | Handler | Purpose |
|--------|------|---------|---------|
| POST | `/v1/messages` | `Messages()` | Primary AI chat endpoint |
| POST | `/v1/chat/completions` | `ChatCompletions()` | OpenAI-compatible async endpoint |
| GET | `/v1/results/{requestID}` | `GetResult()` | Async result retrieval |
| GET | `/health` | `Health()` | Health check (queue depth + uptime) |
| GET | `/v1/limiter-status` | `LimiterStatus()` | Adaptive limiter state |
| POST | `/v1/limiter-override` | `LimiterOverride()` | Pin model concurrency limit |
| GET | `/v1/routing/strategy` | `GetRoutingStrategy()` | Current routing config |
| PUT | `/v1/routing/strategy` | `SetRoutingStrategy()` | Update routing config |
| GET | `/v1/models` | `GetModels()` or `GetModelsAnthropic()` | Model catalog (UA-based dispatch) |
| POST | `/v1/messages/count_tokens` | `CountTokens()` | Token counting |
| GET | `/v1/logs/errors` | `GetErrorLogs()` | Error log retrieval |
| GET | `/v1/logs/errors/count` | `GetErrorLogCount()` | Error log count |
| GET | `/ws` | `HandleWebSocket()` | WebSocket real-time events |
| GET | `/v1/waste/findings` | `GetWasteFindings()` | Waste detection results |
| GET | `/v1/auth/accounts/ratelimits` | `GetRateLimits()` | Per-account rate limit status |
| POST | `/v1/zaiweb/token` | `ZAIWebSetToken()` | Set Z.AI web chat token |
| GET | `/v1/zaiweb/status` | `ZAIWebStatus()` | Z.AI web chat status |
| POST | `/v1/images/generations` | `ZAIWebImageGenerate()` | Image generation via Z.AI |
| POST | `/v1/audio/tts` | `ZAIWebAudioTTS()` | TTS via Z.AI |
| POST | `/mcp/{server}` | `MCPProxyHandle()` | MCP JSON-RPC proxy |
| GET | `/mcp` | `MCPListServers()` | List available MCP servers |
| * | `/api/claude_code/*` | `AnthropicPassthrough()` | Claude Code transparent passthrough |
| * | `/v1/mcp_servers` | `AnthropicPassthrough()` | Claude Code transparent passthrough |
| GET | `/v1/overview` | `Overview()` | System overview |
| GET | `/v1/health/detailed` | `DetailedHealth()` | Detailed health checks |
| POST | `/v1/health/fix/{checkId}` | `FixHealthCheck()` | Apply health fix |
| GET/PUT | `/v1/config` | Config CRUD | Redis-backed config overrides |
| * | `/v1/profiles/*` | Profile CRUD | Profile management |
| * | `/v1/usage/*` | Usage analytics | Usage tracking endpoints |
| * | `/v1/quota/*` | Quota enforcement | Quota check/endpoints |
| GET | `/`, `/*` | Static SPA | Dashboard UI (Vite build) |
| GET | `/metrics`, `/api/metrics` | Prometheus | Metrics scrape endpoint |

### 3.3 Messages() - Primary Endpoint

**File:** `handler/handler.go:L338`

The `Messages()` handler is the primary endpoint (`POST /v1/messages`). Its request lifecycle:

1. **Body validation**: Read and parse request body
2. **Provider resolution**: `h.resolver.Resolve(model)` determines provider based on model name prefix rules
3. **Transparent OAuth**: Detect Claude Code CLI Bearer token, switch to transparent passthrough mode
4. **Profile override**: Check profile-based routing (account pool rotation)
5. **Model fallback**: If provider has no token, try next provider in chain (cross-provider prevention for Z.AI models)
6. **System prompt injection**: Optional `PROMPT_INJECTION_TEXT` prepended to system messages
7. **Smart max_tokens**: Auto-calculate max_tokens based on model context window
8. **Privacy masking**: `h.privacyPipeline.MaskRequest(body)` masks secrets and PII
9. **Vision model auto-selection**: Switch to vision model when images detected in content blocks
10. **Provider-specific dispatch**:
    - Anthropic format -> `anthropicProxy.ProxyTransparent()` or `ProxySidecar()`
    - OpenAI format -> `openAIProxy.ProxyOpenAI()`
    - Gemini format -> `geminiCodeAssistProxy.Proxy()` or `geminiAPIProxy.Proxy()`
    - Z.AI web -> `zaiWebProxy.Proxy()`
    - MCP -> `mcpProxy.ProxyMCP()`
11. **Privacy unmasking**: Restore original values in response (buffered or SSE stream)
12. **Metrics recording**: Token counts, cost, usage, profile, account
13. **Optimizer feedback**: Post-proxy feedback for prefetcher, waste detection, cache ROI, bandit

### 3.4 trySidecarOrDirect()

**File:** `handler/handler.go:L154`

3-path billing header injection:
1. **Go direct** (fastest): Inject billing header as system prompt entry, proxy directly
2. **Sidecar fallback** (Node.js proxy): Route through CLI sidecar at `CLI_SIDECAR_URL`
3. **Direct proxy**: No billing header, just forward

### 3.5 ChatCompletions()

**File:** `handler/handler.go:L228`

Queue-based async job submission:
1. Parse request, generate UUID
2. Enqueue job to Dragonfly (`LPUSH ai_jobs`)
3. Return `requestID` immediately
4. Worker picks up job, processes, stores result with TTL

### 3.6 GetResult()

**File:** `handler/handler.go:L284`

Cached result retrieval from Dragonfly by `requestID`.

### 3.7 AnthropicPassthrough()

**File:** `handler/handler.go:L2197`

Transparent proxy to `api.anthropic.com` for Claude Code CLI routes (`/api/claude_code/*`, `/v1/mcp_servers`). Streams SSE directly.

### 3.8 Model Catalog

**File:** `handler/handler.go:L1899-L1969`

`knownModels` is a static catalog of ~40 models across all providers with pricing:

| Provider | Models |
|----------|--------|
| Z.AI | glm-5.1, glm-5-turbo, glm-5, glm-4.7, glm-4.6, glm-4.5, glm-4.5-x, glm-4.5-air, glm-4.5-airx, glm-4.6v, glm-4.5v |
| Anthropic | claude-sonnet-4-20250514, claude-opus-4-20250115, claude-3.5-sonnet, claude-3-haiku |
| OpenAI | gpt-4o, gpt-4-turbo, o3, o4-mini |
| Gemini | gemini-2.5-flash-preview-05-20 |
| OpenRouter | Various via `or-` prefix |
| Lotus | lotus-default |

### 3.9 Profile Handler

**File:** `handler/profile.go` (~768 lines)

```go
type Profile struct {
    Name            string
    BaseURL         string
    APIKey          string
    Model           string
    Target          string   // provider ID
    Provider        string
    AccountIDs      []string
    Targets         []string // provider ID aliases
    PassthroughAuth bool
}
```

Profile tokens use `arl_` prefix with TTL support. Routes include full CRUD, copy, export/import, token generate/revoke. Redis key: `profiles:{name}`.

`ResolveProfileToken()` performs token -> profile name lookup for routing.

### 3.10 Config Handler

**File:** `handler/config.go` (~422 lines)

Redis-backed config management with redacted responses. Redis keys:
- `config:overrides` - General config overrides
- `config:thinking` - Thinking/reasoning settings
- `config:global-env` - Global environment overrides
- `config:max-tokens` - Per-model max_tokens overrides (loaded on startup via `LoadAndApplyMaxTokens()`)

### 3.11 Usage Handler

**File:** `handler/usage.go` (~895 lines)

Multi-granularity usage tracking in Redis:

| Granularity | Redis Key Pattern | TTL |
|-------------|-------------------|-----|
| Hourly | `usage:hourly:{timestamp}` | 48h |
| Daily | `usage:daily:{date}` | 35d |
| Monthly | `usage:monthly:{month}` | 400d |
| Session | `usage:sessions:{sessionID}` | - |
| Profile | `usage:profile:{profile}:{date}` | 35d |
| Account | `usage:account:{accountID}:{date}` | 35d |

Uses `SCAN`-based key enumeration (production-safe, avoids `KEYS`).

### 3.12 Quota Handler

**File:** `handler/quota.go` (~331 lines)

Quota enforcement with Redis caching and usage-based computation. `CheckQuota()` fails open on errors, blocks at `QuotaBlockPct` threshold (default 95%). Daily budget default: 57,600 requests.

### 3.13 Overview Handler

**File:** `handler/overview.go` (~416 lines)

6 health checks:
1. **Dragonfly/Redis** - QueueDepth ping (2s timeout)
2. **Rate Limiter** - HTTP GET /health (2s timeout)
3. **Prometheus** - Metrics endpoint active
4. **Key Pool** - Active API key count
5. **Upstream** - HTTP GET to upstream URL (3s timeout, <500 = pass)
6. **Memory** - Heap allocation percentage (>90% fail, >75% warn)

### 3.14 WebSocket Handler

**File:** `handler/websocket.go` (~171 lines)

WebSocket hub with Register/Unregister/Broadcast channels. Ping/pong keepalive: 60s pong wait, 54s ping period. Broadcasts events: `config-changed`, usage updates, etc.

### 3.15 Optimizers Handler

**File:** `handler/optimizers.go` (~261 lines)

```go
type Optimizers struct {
    Chunker    *chunker.Chunker
    Packer     *packer.Packer
    Disclosure *disclosure.Disclosure
    Prefetcher *prefetcher.Prefetcher
    Bandit     *bandit.Bandit
    Summarizer *summarizer.Summarizer
    Delta      *delta.Delta
    Sketch     *sketch.Sketch
    Waste      *waste.Waste
    Filter     *filter.Filter
    Cache      *cache.Cache
    WarmStart  *warmstart.WarmStart
    Caveman    *caveman.Caveman
}
```

Optimization stages:
- `OptimizeSystemPrompt()`: Semantic dedup -> Chunker -> Delta -> Sketch -> Summarizer (red budget) -> Intent filter -> Caveman compression
- `OptimizeMessages()`: Whitespace + sentence dedup on message content
- `PostProxyFeedback()`: Prefetcher record, waste detection, cache ROI, bandit feedback

---

## 4. Proxy Layer

### 4.1 Anthropic Proxy

**File:** `proxy/anthropic.go` (~2244 lines)

```go
type AnthropicProxy struct {
    cfg     *config.Config
    metrics *metrics.Metrics
    client  *http.Client
}

type ProxyOptions struct {
    AuthMode          string
    UpstreamOverride  string
    ExtraHeaders      map[string]string
    Transparent       bool
    BillingInjected   bool
    OnAuthError       func()
    OnRateLimitError  func()
}
```

Key methods:
- `ProxyTransparent()`: Full passthrough with optional billing header injection
- `ProxySidecar()`: Route through Node.js sidecar for billing
- `ProxyNativeVision()`: Z.AI native vision endpoint (base64 images)

`InjectBillingHeader()`: Injects billing system prompt entry with build hash into the messages array.

`relayStreamWithTracking()`: SSE relay with:
- Real-time privacy unmasking (stream unmasker)
- Server tool_use event filtering
- Token counting (input + output)
- Usage recording via metrics callback

`handleNonStreamResponse()`: Buffered response with:
- Trimming of whitespace
- Server tool_use filtering
- Token counting

Retry logic:
- **429**: Key rotation via KeyPool, retry with different key
- **401**: OAuth token refresh via `onAuthError` callback
- **Transient** (500/502/503/529): Exponential backoff with jitter
- **Context overflow**: Auto-truncation via `TruncateMessages()` in `recovery.go`
- **Empty/malformed 200**: Retried as transient error

### 4.2 Key Pool

**File:** `proxy/key_pool.go` (~363 lines)

```go
type KeyPool struct {
    keys     []keyEntry
    strategy string // "round-robin" or "fill-first"
    rpmLimit int
    cond     *sync.Cond
}
```

Per-key state:
- RPM tracking (sliding window, 1-minute buckets)
- Cooldown on 429 (10s)
- `sync.Cond` for efficient goroutine waiting when all keys exhausted

Methods:
- `Acquire()`: Get next available key (respects RPM limit and cooldown)
- `ReportSuccess(key)`: Mark key as healthy
- `Report429(key)`: Put key in 10s cooldown
- `SyncFromStore(keys)`: Live key rotation preserving RPM state

### 4.3 OpenAI Proxy

**File:** `proxy/openai.go` (~729 lines)

Format conversion between Anthropic and OpenAI APIs:
- `AnthropicToOpenAI()`: Convert Anthropic messages to OpenAI format
- Tool support: `tool_use` <-> `tool_calls` bidirectional conversion
- Auto-continuation for providers with context limits (Lotus ~40k, up to 3 continuations)
- `compactOpenAIMessages()`: Message truncation keeping last 2 turns

Supports: OpenAI, Copilot, OpenRouter, Qwen, DeepSeek, Kimi, Lotus, HuggingFace, Ollama, Cursor, CodeBuddy, Kilo

### 4.4 Gemini Code Assist Proxy

**File:** `proxy/gemini-codeassist.go` (~725 lines)

- `anthropicToGemini()`: Anthropic to Gemini format conversion
- Code Assist envelope wrapping (model, project, request)
- `ResolveProjectID()`: loadCodeAssist endpoint for project ID resolution

### 4.5 Gemini API Proxy

**File:** `proxy/gemini-apikey.go` (~322 lines)

Direct Gemini API integration with streaming SSE conversion from Gemini to Anthropic format.

### 4.6 Z.AI Web Proxy

**File:** `proxy/zaiweb.go` (~765 lines)

HMAC-SHA256 request signing with 5-minute time periods:
```
signature = HMAC-SHA256(timestamp, token)
```

Features:
- FE version scraping from `chat.z.ai`
- Model name mapping
- Feature flags: `web_search`, `thinking`, `image_generation`
- Image generation proxy (`image.z.ai`)
- TTS proxy (`audio.z.ai`)

### 4.7 Claude Session Proxy

**File:** `proxy/claude-session.go` (~386 lines) + `proxy/claude_session.go` (~139 lines)

Claude web session proxy and OAuth session manager. Handles profile, roles, settings, and policy bootstrap.

### 4.8 MCP Proxy

**File:** `proxy/mcp.go` (~321 lines)

```go
type MCPProxy struct {
    cfg     *config.Config
    client  *http.Client
    metrics *metrics.Metrics
    keyPool *KeyPool
    rdb     *redis.Client
}
```

3 MCP servers:
- `web_reader` - Web Reader (working)
- `web_search_prime` - Web Search Prime (broken upstream)
- `zread` - Zread (working)

Per-account rate limiting via Redis (1-minute windows). Response caching with SHA256 key derivation. Retry with key rotation on 429/5xx (quadratic backoff, max 5min).

### 4.9 Shared Transport

**File:** `proxy/shared_transport.go` (~108 lines)

Singleton HTTP transport with DNS caching (30s TTL), connection pooling, and keep-alive.

### 4.10 Recovery

**File:** `proxy/recovery.go` (~299 lines)

Error classification:
- `ClassifyError()`: transient (500/502/503/529), context overflow (400/413/422 patterns)
- `TruncateMessages()`: Keeps system + last N messages, fixes tool pair boundaries

### 4.11 SSE Writer

**File:** `proxy/sse_writer.go` (~74 lines)

Buffer pool utilities for SSE event writing.

---

## 5. Middleware Chain

The middleware chain is applied in this exact order in `main.go:L265-284`:

```
Request -> SecurityHeaders -> CorrelationID -> RealIP -> [IPFilter] -> Logging -> Metrics -> RateLimiter -> Handler
```

### 5.1 SecurityHeaders

**File:** `middleware/security.go:L21`

Adds standard security headers:
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `X-XSS-Protection: 1; mode=block`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Cache-Control: no-store` (for `/v1/` paths only)

### 5.2 CorrelationID

**File:** `middleware/security.go:L37`

Propagates or generates a UUID correlation ID:
1. Check `X-Correlation-ID` header
2. If absent, generate new UUID
3. Set `X-Correlation-ID` response header
4. Store in request context

### 5.3 RealIP

**File:** `middleware/security.go:L51`

Extracts real client IP from proxy headers in priority order:
1. `CF-Connecting-IP` (Cloudflare)
2. `X-Real-IP`
3. `X-Forwarded-For` (first entry)
4. `r.RemoteAddr` (fallback)

### 5.4 IPFilter (conditional)

**File:** `middleware/security.go:L94`

Only applied when `IP_WHITELIST` or `IP_BLACKLIST` env vars are set. Supports CIDR notation. Whitelist is checked first (if non-empty), then blacklist.

### 5.5 Logging

**File:** `middleware/logging.go:L37`

Structured logging middleware using `log/slog`. Logs every request with:
- method, path, status code, duration (ms), remote_addr
- agent_id (from query string, if present)

Wraps `http.ResponseWriter` to capture status code. Supports `http.Hijacker` interface for WebSocket upgrades.

### 5.6 Metrics Middleware

**File:** `metrics/metrics.go` (applied via `m.Middleware`)

Prometheus middleware recording request duration and count. Exposed at `/metrics` and `/api/metrics`.

### 5.7 RateLimiter

**File:** `middleware/ratelimit.go:L94`

Calls distributed rate-limiter service (Java/Spring Boot) for:
1. **Global rate limit**: Single `global` key check
2. **Per-agent rate limit**: Keyed by API key suffix or `agent_id` param

Checks run in parallel via `errgroup`. **Fail-open** on any communication error (allows request through). Skips internal endpoints (`/metrics`, `/health`, `/ws`).

Anthropic-format rate limit errors use `proxy.RateLimitError()` for consistent response format.

### 5.8 AdaptiveLimiter

**File:** `middleware/adaptive_limiter.go` (~788 lines)

**Not HTTP middleware** -- called programmatically by the handler before proxying.

```go
type AdaptiveLimiter struct {
    globalInFlight atomic.Int64
    globalLimit    int64
    models         map[string]*adaptiveModel
    fallbackOrder  []string
    seriesBuckets  map[int][]seriesEntry
    rrEpoch        atomic.Uint64
    globalCond     *sync.Cond
    modelConds     map[string]*sync.Cond
    overrides      map[string]int64
    candPool       sync.Pool
}
```

Algorithm (inspired by Envoy gradient controller + Netflix concurrency limits):

**On 429/503:**
```
limit_new = max(minLimit, limit * 0.5)
peakBefore429 = old_limit
```

**On success (every 5th):**
```
gradient = min(2.0, max(0.8, (minRTT + buffer) / sampleRTT))
limit_new = min(maxLimit, gradient * limit + sqrt(limit))
```

**Cooldown:** 5s after any 429 before increasing.

**Learned ceiling:** `peakBefore429` is stored on 429. New limit cannot exceed `peak-1` for 5 minutes (then decays, allowing re-probe).

**Cross-series fallback:**
- Models grouped by major version series (glm-5 -> series 5, glm-4 -> series 4)
- Same-series round-robin first
- Cross-series proactive distribution: 20% of traffic to lower series when utilization >= 70%
- Spillover triggers: same-series full, recent 429, latency pressure

**Lock-free hot path:** All in-flight tracking uses `atomic.Int64` with CAS loops. `sync.Cond` for efficient goroutine wake on Release.

**Signal-based waiting:** `acquireAnyModel()` uses temporary `sync.Cond` per waiter, woken by `Release()`.

### 5.9 AnomalyDetector

**File:** `middleware/anomaly.go` (~188 lines)

```go
type AnomalyDetector struct {
    n    int64   // sample count
    mean float64 // running mean
    m2   float64 // running sum of squared deviations
}
```

Uses Welford's online algorithm for O(1) mean/stddev update. Anomaly types:
- `AnomalySpike` (z > 2.0)
- `AnomalyDrop` (z < -2.0)
- `AnomalySustainedHigh` (5+ consecutive spikes)
- `AnomalySustainedLow` (5+ consecutive drops)

Severity levels by |z-score|:
- Medium: |z| > 2.0
- High: |z| > 3.0
- Critical: |z| > 4.0

Prometheus counter: `api_gateway_anomaly_total{type, severity}`

### 5.10 ConfigWatcher

**File:** `middleware/config_watcher.go` (~119 lines)

Watches `.env` file via `fsnotify`. Debounced (500ms). Calls callback on key change. Broadcasts `config-changed` WebSocket event.

### 5.11 DashboardAuth

**File:** `middleware/dashboard-auth.go` (~38 lines)

Checks `x-api-key` header or `arl_session` cookie. If `DASHBOARD_PASSWORD` env var is empty, all requests pass through (auth disabled). Applied to dashboard routes.

### 5.12 LoginLimiter

**File:** `middleware/login_limiter.go` (~83 lines)

Per-IP login rate limiting: max 5 attempts per 15-minute window. Background cleanup every 5 minutes. Available but not applied by default (commented out in `main.go:L293`).

### 5.13 RuntimeMetrics

**File:** `middleware/runtime_metrics.go` (~106 lines)

Background goroutine collecting every 10s:

| Metric | Prometheus Name |
|--------|----------------|
| Goroutines | `api_gateway_go_goroutines` |
| Heap allocation | `api_gateway_go_heap_alloc_bytes` |
| Heap objects | `api_gateway_go_heap_objects` |
| GC pause | `api_gateway_go_gc_pause_ns` |
| Stack in-use | `api_gateway_go_stack_inuse_bytes` |
| Dragonfly health | `api_gateway_dragonfly_up` |

### 5.14 SessionSecret

**File:** `middleware/session_secret.go` (~106 lines)

Load or generate 64-byte hex session secret. Persisted to `config/session_secret`. Auto-reloaded on file change via `fsnotify`.

---

## 6. Configuration

**File:** `config/config.go` (~429 lines)

All config loaded from environment variables with sensible defaults for containerized deployment.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_PORT` | `:8080` | HTTP server listen address |
| `REDIS_ADDR` | `dragonfly:6379` | Dragonfly/Redis address |
| `RATE_LIMITER_ADDR` | `http://rate-limiter:8080` | Distributed rate limiter URL |
| `QUEUE_NAME` | `ai_jobs` | Dragonfly queue name |
| `GLOBAL_RATE_LIMIT` | `100` | Global requests per second |
| `AGENT_RATE_LIMIT` | `5` | Per-agent requests per second |
| `WORKER_POOL_SIZE` | `100` | Async worker pool size |
| `READ_TIMEOUT` | `30s` | HTTP read timeout |
| `WRITE_TIMEOUT` | `10s` | HTTP write timeout (overridden to 0 for SSE) |
| `OTLP_ENDPOINT` | `otel-collector:4317` | OpenTelemetry collector endpoint |
| `REDIS_POOL_SIZE` | `50` | Redis connection pool size |
| `REDIS_MIN_IDLE_CONNS` | `10` | Minimum idle Redis connections |
| `UPSTREAM_URL` | `https://api.z.ai/api/anthropic` | Primary upstream URL |
| `ANTHROPIC_DIRECT_URL` | `https://api.anthropic.com` | Direct Anthropic API URL |
| `STREAM_TIMEOUT` | `300s` | SSE stream timeout |
| `UPSTREAM_MODEL_LIMITS` | `glm-5.1:1,glm-5-turbo:1,...` | Per-model concurrency limits |
| `UPSTREAM_VISION_MODEL_LIMITS` | `glm-5.1:5,glm-4.6v:5,...` | Per-vision-model concurrency limits |
| `UPSTREAM_DEFAULT_LIMIT` | `3` | Default concurrency limit for unconfigured models |
| `UPSTREAM_GLOBAL_LIMIT` | `9` | Hard cap across all models |
| `UPSTREAM_MAX_RETRIES` | `3` | Max retries on upstream errors |
| `UPSTREAM_RETRY_BACKOFF` | `500ms` | Base retry backoff |
| `UPSTREAM_PROBE_MULTIPLIER` | `5` | Adaptive probe ceiling multiplier |
| `UPSTREAM_RPM_LIMIT` | `40` | Per-key requests-per-minute |
| `ENABLE_PROMPT_INJECTION` | `true` | System prompt injection toggle |
| `ENABLE_RESPONSE_TRIM` | `true` | Response trimming toggle |
| `ENABLE_SMART_MAX_TOKENS` | `true` | Auto max_tokens calculation |
| `PROMPT_INJECTION_TEXT` | (default rules) | Custom system prompt injection text |
| `ENABLE_AUTO_TRUNCATE` | `true` | Auto-truncate on context overflow |
| `TRANSIENT_RETRY_MAX` | `3` | Max transient error retries |
| `ZAI_API_KEYS` | (empty) | Comma-separated Z.AI API keys |
| `MODEL_PRICING` | (default pricing) | Per-model pricing `model:input:output` |
| `NATIVE_VISION_URL` | `https://open.bigmodel.cn/...` | Z.AI native vision endpoint |
| `IP_WHITELIST` | (empty) | Comma-separated IPs/CIDRs |
| `IP_BLACKLIST` | (empty) | Comma-separated IPs/CIDRs |
| `QUOTA_CACHE_TTL` | `30s` | Quota cache TTL |
| `QUOTA_DAILY_BUDGET` | `57600` | Daily request budget |
| `QUOTA_BLOCK_PCT` | `95` | Block threshold percentage |
| `MAX_REQUEST_BODY` | `10485760` (10MB) | Max request body size |
| `DEFAULT_MODEL` | `glm-5` | Default model for requests |
| `DEFAULT_PROVIDER` | `glm` | Default provider |
| `DEFAULT_TEMPERATURE` | `0.7` | Default temperature |
| `DEFAULT_MAX_TOKENS` | `1024` | Default max tokens |
| `GEMINI_CODEASSIST_ENDPOINT` | `https://cloudcode-pa.googleapis.com/...` | Gemini Code Assist URL |
| `GEMINI_API_ENDPOINT` | `https://generativelanguage.googleapis.com` | Gemini API URL |
| `GEMINI_DEFAULT_MODEL` | `models/gemini-2.5-flash-preview-05-20` | Default Gemini model |
| `ANTHROPIC_API_VERSION` | `2023-06-01` | Anthropic API version header |
| `MODEL_PRIORITY` | `glm-5.1:100,...` | Model fallback priority |
| `ANOMALY_COOLDOWN_SEC` | `5` | Anomaly cooldown seconds |
| `ANOMALY_Z_THRESHOLD` | `2.0` | Z-score threshold |
| `GLM_MODE` | `true` | Z.AI features toggle |
| `ZAI_OPENAI_URL` | `https://api.z.ai/api/paas/v4/...` | Z.AI OpenAI-compatible endpoint |
| `ZAI_WEB_ENABLED` | `false` | Z.AI web chat proxy toggle |
| `ZAI_WEB_TOKEN` | (empty) | Z.AI web chat JWT token |
| `ZAI_WEB_MODELS` | (empty) | Models to route via chat.z.ai |
| `CLI_SIDECAR_ENABLED` | `true` | Node.js sidecar toggle |
| `CLI_SIDECAR_URL` | `http://127.0.0.1:8081` | Sidecar URL |
| `MCP_ENABLED` | `true` | MCP proxy toggle |
| `MCP_CACHE_TTL` | `1h` | MCP response cache TTL |
| `MCP_MAX_RETRIES` | `2` | MCP max retries |
| `MCP_RATE_LIMIT_PER_MIN` | `30` | MCP per-account rate limit |
| `DASHBOARD_PASSWORD` | (empty) | Dashboard auth API key |
| `DASHBOARD_URL` | `https://ai.klxhub.com` | Dashboard public URL |
| `OAUTH_CALLBACK_BASE` | `https://ai.klxhub.com` | OAuth callback base URL |
| `PASTEGUARD_ENABLED` | `true` | Privacy masking toggle |
| `PASTEGUARD_SECRETS_ENABLED` | `true` | Secret detection toggle |
| `PASTEGUARD_PII_ENABLED` | `true` | PII detection toggle |
| `PASTEGUARD_MAX_SCAN_CHARS` | `200000` | Max chars to scan for PII |
| `GEMINI_OAUTH_CLIENT_ID` | (empty) | Gemini OAuth client ID |
| `GEMINI_OAUTH_CLIENT_SECRET` | (empty) | Gemini OAuth client secret |
| `LOTUS_UPSTREAM_BASE` | `https://api-cpxis.lotuss.com/llm` | Lotus upstream base URL |
| `LOTUS_API_KEYS` | (empty) | Lotus API keys |
| `PROVIDER_MODEL_PREFIXES` | `zai:glm-;anthropic:claude-;...` | Model-to-provider prefix mapping |

### Model Pricing (default)

```
glm-5.1:          $1.40 / $4.40 per 1M tokens (input/output)
glm-5-turbo:      $1.20 / $4.00
glm-5:            $1.00 / $3.20
glm-4.7:          $0.60 / $2.20
glm-4.7-flashx:   $0.07 / $0.40
glm-4.6:          $0.60 / $2.20
glm-4.5:          $0.60 / $2.20
glm-4.5-x:        $2.20 / $8.90
glm-4.5-air:      $0.20 / $1.10
glm-4.5-airx:     $1.10 / $4.50
glm-4.6v:         $0.30 / $0.90
glm-4.5v:         $0.60 / $1.80
```

### Provider Routing Rules

**File:** `provider/resolver.go`

Model prefix -> provider mapping:

| Model Prefix | Provider Order |
|-------------|---------------|
| `claude-` | claude-oauth, anthropic |
| `gpt-`, `o1-`, `o3-`, `o4-` | openai |
| `gemini-` | gemini-oauth, gemini |
| `glm-` | zai |
| `qwen-` | qwen |
| `or-` | openrouter |
| `deepseek-` | deepseek |
| `kimi-` | kimi |
| `lotus-` | lotus |

API format per provider:
- **Anthropic format**: anthropic, claude-oauth, claude, zai, agy
- **OpenAI format**: openai, copilot, openrouter, qwen, deepseek, kimi, huggingface, ollama, lotus, cursor, codebuddy, kilo
- **Gemini format**: gemini, gemini-oauth

---

## 7. Supporting Packages

### 7.1 Queue (Dragonfly)

**File:** `queue/dragonfly.go` (~156 lines)

Connection pool: 50 connections, 10 min idle, 5min idle timeout, 30min lifetime. Methods:
- `PushJob()`: LPUSH to queue
- `GetResult()`: GET from `result:{id}`
- `SetResult()`: SET with TTL (default 10min)
- `QueueDepth()`: LLEN for metrics

### 7.2 Privacy Pipeline

**File:** `privacy/pipeline.go` (~376 lines)

Two-phase masking:
1. **MaskRequest()**: Extract text spans -> parallel secret/PII detection -> mask -> return MaskContext
2. **UnmaskResponse()**: Restore original values in buffered response
3. **NewStreamUnmasker()**: Create SSE stream unmasker for real-time restoration

Secret detection entities: OPENSSH_PRIVATE_KEY, PEM_PRIVATE_KEY, API_KEY_SK, API_KEY_AWS, API_KEY_GITHUB, JWT_TOKEN, BEARER_TOKEN

PII detection entities: EMAIL_ADDRESS, PHONE_NUMBER (default)

### 7.3 Provider System

**Files:** `provider/*.go`

- `Registry`: Provider configuration lookup
- `TokenStore`: Redis-backed token storage with CRUD
- `Resolver`: Model-to-provider routing with round-robin, cooldown, fallback
- `RefreshWorker`: Background OAuth token refresh
- `AuthHandler`: OAuth auth code + device flow endpoints

---

## 8. Request Flow Diagram

### 8.1 Primary Request Path (POST /v1/messages)

```
Client
  |
  v
[Caddy Reverse Proxy] :9000
  |
  v
[API Gateway] :8080
  |
  +-> SecurityHeaders (X-Content-Type-Options, X-Frame-Options, etc.)
  +-> CorrelationID (propagate or generate UUID)
  +-> RealIP (CF-Connecting-IP > X-Real-IP > X-Forwarded-For)
  +-> [IPFilter] (conditional, whitelist/blacklist)
  +-> Logging (slog: method, path, status, duration)
  +-> Metrics (Prometheus request counter + duration)
  +-> RateLimiter (parallel global + agent check, fail-open)
  |
  v
Handler.Messages()
  |
  +-> 1. Parse request body (model, messages, stream flag)
  +-> 2. Resolve provider (model prefix -> provider rules)
  |      |
  |      +-> Transparent OAuth? (Bearer token present, claude-oauth route)
  |      +-> Profile override? (profile token -> profile name -> provider)
  |      +-> Account pool? (round-robin through active tokens)
  |      +-> GLM key pool? (key rotation for Z.AI models)
  |      +-> Fallback? (next provider in chain)
  |
  +-> 3. Check quota (Redis usage vs daily budget)
  +-> 4. System prompt injection (optional)
  +-> 5. Smart max_tokens (model context window based)
  +-> 6. Privacy masking (secrets + PII detection, placeholder replacement)
  +-> 7. Vision model auto-selection (switch to *v model if images present)
  +-> 8. Acquire concurrency slot (AdaptiveLimiter)
  |      |
  |      +-> Try same-series round-robin
  |      +-> Try cross-series spillover (if same-series full or 429)
  |      +-> Wait for any slot (sync.Cond signal)
  |
  +-> 9. Dispatch to provider proxy
  |      |
  |      +-> [Anthropic format] -> AnthropicProxy
  |      |      +-> trySidecarOrDirect() (Go direct > Sidecar > Direct)
  |      |      +-> Billing header injection (system prompt entry)
  |      |      +-> SSE relay with unmasking + token tracking
  |      |      +-> Retry: 429 (key rotation), 401 (refresh), transient (backoff)
  |      |
  |      +-> [OpenAI format] -> OpenAIProxy
  |      |      +-> AnthropicToOpenAI() format conversion
  |      |      +-> Tool use <-> tool_calls conversion
  |      |      +-> Auto-continuation for limited-context providers
  |      |
  |      +-> [Gemini format] -> GeminiCodeAssistProxy or GeminiAPIProxy
  |      |      +-> anthropicToGemini() format conversion
  |      |      +-> SSE stream conversion (Gemini -> Anthropic format)
  |      |
  |      +-> [Z.AI Web] -> ZAIWebProxy
  |      |      +-> HMAC-SHA256 request signing
  |      |      +-> chat.z.ai API call
  |      |
  |      +-> [MCP] -> MCPProxy
  |             +-> Per-account rate limit (Redis)
  |             +-> Cache check (SHA256 key)
  |             +-> Retry with key rotation
  |
  +-> 10. Privacy unmasking (restore originals in response)
  +-> 11. Release concurrency slot (AdaptiveLimiter.Release)
  +-> 12. Record metrics (tokens, cost, duration, provider, model)
  +-> 13. Record usage (hourly, daily, monthly, session, profile, account)
  +-> 14. Optimizer feedback (prefetcher, waste, cache, bandit)
  +-> 15. Adaptive limiter feedback (adjust limit based on response)
  |
  v
Client receives response (streaming SSE or buffered JSON)
```

### 8.2 Async Request Path (POST /v1/chat/completions)

```
Client -> POST /v1/chat/completions
  |
  +-> Parse request, generate UUID
  +-> Enqueue job to Dragonfly (LPUSH ai_jobs)
  +-> Return {"request_id": "uuid", "status": "queued"}
  |
  [Worker picks up job from queue]
  +-> Process via provider proxy
  +-> Store result in Dragonfly (result:{id}, TTL 10min)
  |
Client -> GET /v1/results/{requestID}
  +-> Fetch cached result from Dragonfly
  +-> Return result or {"status": "pending"}
```

### 8.3 Provider Resolution Flow

```
Model name: "claude-sonnet-4-20250514"
  |
  +-> Prefix match: "claude-" -> ["claude-oauth", "anthropic"]
  |
  +-> Try "claude-oauth":
  |      +-> Check cooldown (skip if recently rate-limited)
  |      +-> Get tokens from TokenStore (round-robin)
  |      +-> Skip expired tokens
  |      +-> Prefer low-utilization accounts (<0.8 util5h)
  |      +-> Return RoutingDecision with Bearer auth + beta headers
  |
  +-> Try "anthropic" (fallback):
         +-> Get default token from TokenStore
         +-> Return RoutingDecision with API key auth

Model name: "glm-5.1"
  |
  +-> Prefix match: "glm-" -> ["zai"]
  +-> Try "zai": Get token or use KeyPool
  +-> GLM mode fallback: build Z.AI decision even without token
```

### 8.4 Adaptive Limiter Flow

```
Acquire("glm-5.1")
  |
  +-> acquireGlobal() (wait for global slot, 60s timeout)
  |      +-> sync.Cond.Wait() until globalInFlight < globalLimit
  |
  +-> getModel("glm-5.1") -> series=5
  |
  +-> tryFallbackAllSeries():
         +-> Same-series (5): round-robin through glm-5.x models
         +-> If utilization >= 70%: 20% chance to try lower series (4)
         +-> If same-series full: spill to lower series
         +-> Triggers: same-series full, recent 429, latency pressure
  |
  +-> (fallback) acquireAnyModel():
         +-> Try requested model
         +-> Try any model in fallback order
         +-> sync.Cond.Wait() for Release signal
  |
  v
[Request processed]
  |
  +-> Release(selectedModel):
         +-> Decrement model inFlight
         +-> Decrement global inFlight
         +-> Signal model cond + global cond
  |
  +-> Feedback(model, statusCode, rtt, headers):
         +-> On 429/503: limit *= 0.5, store peak
         +-> On success (every 5th): gradient calculation
         +-> Update minRTT (CAS), RTT EWMA (alpha=0.3)
         +-> Respect learned ceiling (peak-1) with 5min decay
```

---

## 9. Docker Compose Services

**File:** `docker-compose.yml`

| Service | Image/Build | Port | Purpose |
|---------|-------------|------|---------|
| `arl-gateway` | `./api-gateway/Dockerfile` | 8080 (internal) | API Gateway (Go) |
| `arl-rate-limiter` | `./distributed-rate-limiter/Dockerfile` | 8080 (internal) | Rate Limiter (Java/Spring Boot) |
| `arl-dragonfly` | `dragonfly:v1.37.2` | 6379 (internal) | Redis-compatible store |
| `arl-worker` | `./ai-worker/Dockerfile` | 9090/9091 (internal) | AI Worker (Python) |
| `arl-prometheus` | `prometheus:v2.54.1` | 9090 (internal) | Metrics |
| `arl-grafana` | `grafana:11.3.0` | (internal) | Dashboards |
| `arl-otel` | `otel-collector-contrib:0.112.0` | 4317/4318 (internal) | Tracing |
| `arl-proxy` | `caddy:2-alpine` | 9000 (external) | Reverse proxy |
| `arl-dashboard` | `./ui/Dockerfile.dev` | 5173 (internal) | Dashboard UI (React/Vite) |
| `arl-rl-dashboard` | (compose profile: rl-dashboard) | - | Rate limiter dashboard |
| `arl-presidio` | (compose profile: pii) | 3000 (internal) | PII analyzer (legacy) |
| `claude-code-meow` | (compose profile: test-client) | - | Claude Code test client |
| `claude-code-test` | (compose profile: test-client) | - | Claude Code test client |

Resource limits: Gateway (1G/2CPU), Rate Limiter (1.5G/2CPU), Dragonfly (8G/2CPU), Worker (2G/2CPU).

Network: `arl-network` (bridge). Volumes: `arl-dragonfly-data`, `arl-prometheus-data`, `arl-grafana-data`.
