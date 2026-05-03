# 04 - Infrastructure, Configuration & Operations

## 1. Startup Sequence

The gateway entry point (`api-gateway/main.go`) initializes components in this exact order:

### Phase 1: Foundation
1. **Structured logging** - `slog.NewJSONHandler(os.Stdout)` at `slog.LevelInfo`
2. **Configuration** - `config.Load()` reads all env vars with defaults
3. **OpenTelemetry** - `initTracer()` connects to OTLP gRPC collector; graceful degradation if unavailable
4. **Dragonfly/Redis** - `queue.NewDragonflyClient(cfg)` with production pool tuning (50 conns, 10 min idle, 5m max idle, 30m max lifetime); fatal exit on connection failure

### Phase 2: Core Services
5. **Prometheus metrics** - `metrics.New()` with queue depth callback and model pricing map
6. **Runtime metrics** - goroutines, heap, GC, Dragonfly health gauges; 10s collection interval
7. **Anomaly detector** - `middleware.NewAnomalyDetector()` for Z-score based anomaly detection
8. **Privacy pipeline** - PasteGuard (secrets regex + PII detection), configured via `PASTEGUARD_*` env vars

### Phase 3: Proxies & Routing
9. **Proxy handlers** - AnthropicProxy, GeminiCodeAssistProxy, OpenAIProxy, GeminiAPIProxy
10. **Adaptive limiter** - per-model concurrency with `AdaptiveLimiter`; probe multiplier for ceiling discovery
11. **Key pool** - `proxy.NewKeyPool()` for multi-key round-robin rotation with per-key RPM budgets
12. **Provider registry** - 18 built-in providers (Z.AI, Anthropic, Claude OAuth, OpenAI, Gemini, Gemini OAuth, Copilot, OpenRouter, Qwen, DeepSeek, Kimi, HuggingFace, Ollama, AGY, Cursor, CodeBuddy, Kilo, Lotus); plus custom providers loaded from Redis
13. **Token store** - Redis-backed OAuth token persistence with provider rename migration
14. **Auth handler** - device code, auth code (PKCE), API key, session cookie flows
15. **Token refresh worker** - 30-min refresh cycle, immediate refresh on startup, auto-cleanup of expired tokens

### Phase 4: Handlers & Optimizers
16. **WebSocket hub** - real-time event broadcast to dashboard
17. **Profile/Usage/Quota handlers** - per-profile and per-account usage tracking, daily quota enforcement
18. **13 token optimizers** (all off by default, activated per-request):
    - `chunker` - intelligent message chunking
    - `packer` - prompt compression
    - `disclosure` - context disclosure optimization
    - `prefetcher` - response prefetching
    - `bandit` - multi-armed bandit optimization
    - `summarizer` - conversation summarization
    - `delta` - delta encoding for repeated requests
    - `sketch` - sketch-based deduplication
    - `waste` - token waste detection
    - `filter` - request filtering
    - `cache` - response caching with eviction loop
    - `warmstart` - warm start optimization
    - `caveman` - baseline optimization
19. **Usage recording wiring** - `metrics.SetUsageRecorder()` callback persists to Redis and feeds optimizer feedback

### Phase 5: Background Workers
20. **Waste scanner** - 60s background scan loop
21. **Cache eviction loop** - continuous eviction for response cache
22. **Token refresh worker** - started in goroutine, deferred stop
23. **Session secret** - loaded/generated from `config/session_secret`, watched via fsnotify
24. **Config file watcher** - watches `.env` for changes, broadcasts via WebSocket
25. **Z.AI key sync** - syncs Z.AI tokens from TokenStore into KeyPool every 30s (GLM mode only)
26. **Adaptive metrics exporter** - exports limiter state to Prometheus gauges every 10s

### Phase 6: HTTP Server
27. **Chi router** - middleware chain: SecurityHeaders, CorrelationID, RealIP, IPFilter (optional), Logging, Metrics, RateLimiter
28. **Route registration** - all API routes, WebSocket, static SPA, Prometheus endpoints
29. **HTTP server** - `ReadTimeout` from config, `WriteTimeout: 0` (disabled for SSE streaming), `IdleTimeout: 120s`
30. **Graceful shutdown** - SIGINT/SIGTERM handler with 10s timeout

---

## 2. Configuration System

### 2.1 Config Struct (`api-gateway/config/config.go`)

All config via environment variables, parsed at startup with fallback defaults.

| Environment Variable | Default | Type | Description |
|---|---|---|---|
| `SERVER_PORT` | `:8080` | string | HTTP listen address |
| `REDIS_ADDR` | `dragonfly:6379` | string | Dragonfly/Redis address |
| `RATE_LIMITER_ADDR` | `http://rate-limiter:8080` | string | Java rate limiter service URL |
| `QUEUE_NAME` | `ai_jobs` | string | Redis list key for job queue |
| `GLOBAL_RATE_LIMIT` | `100` | int | Global requests per second |
| `AGENT_RATE_LIMIT` | `5` | int | Per-agent requests per second |
| `WORKER_POOL_SIZE` | `100` | int | Max concurrent upstream calls |
| `READ_TIMEOUT` | `30s` | duration | HTTP read timeout |
| `WRITE_TIMEOUT` | `10s` | duration | HTTP write timeout (overridden to 0 in main) |
| `OTLP_ENDPOINT` | `otel-collector:4317` | string | OpenTelemetry gRPC endpoint |
| `REDIS_POOL_SIZE` | `50` | int | Redis connection pool size |
| `REDIS_MIN_IDLE_CONNS` | `10` | int | Minimum idle Redis connections |
| `UPSTREAM_URL` | `https://api.z.ai/api/anthropic` | string | Primary upstream API URL |
| `ANTHROPIC_DIRECT_URL` | `https://api.anthropic.com` | string | Direct Anthropic API URL |
| `STREAM_TIMEOUT` | `300s` | duration | SSE streaming timeout |
| `UPSTREAM_MODEL_LIMITS` | `glm-5.1:1,glm-5-turbo:1,...` | map | Per-model concurrency limits |
| `UPSTREAM_VISION_MODEL_LIMITS` | `glm-5.1:5,glm-4.6v:5,glm-4.5v:3` | map | Per-model vision concurrency limits |
| `UPSTREAM_DEFAULT_LIMIT` | `3` | int | Default per-model concurrency (docker-compose overrides to 1) |
| `UPSTREAM_GLOBAL_LIMIT` | `9` | int | Total concurrent upstream requests |
| `UPSTREAM_MAX_RETRIES` | `3` | int | Max retries on 429 |
| `UPSTREAM_RETRY_BACKOFF` | `500ms` | duration | Base backoff for retry |
| `UPSTREAM_API_KEYS` | (empty) | csv | API keys for rotation pool |
| `UPSTREAM_RPM_LIMIT` | `40` | int | Per-key RPM budget |
| `UPSTREAM_PROBE_MULTIPLIER` | `5` | int | Adaptive limit probe multiplier |
| `MODEL_PRICING` | `glm-5.1:1.4:4.4,glm-5-turbo:1.2:4.0,glm-5:1.0:3.2,glm-4.7:0.6:2.2,glm-4.7-flashx:0.07:0.4,glm-4.6:0.6:2.2,glm-4.5:0.6:2.2,glm-4.5-x:2.2:8.9,glm-4.5-air:0.2:1.1,glm-4.5-airx:1.1:4.5,glm-4.6v:0.3:0.9,glm-4.5v:0.6:1.8,...` | map | Per-model USD per 1M tokens (input:output) |
| `NATIVE_VISION_URL` | `https://open.bigmodel.cn/...` | string | Zhipu native vision endpoint |
| `IP_WHITELIST` | (empty) | csv | Whitelisted IPs/CIDRs |
| `IP_BLACKLIST` | (empty) | csv | Blacklisted IPs/CIDRs |
| `MAX_REQUEST_BODY` | `10485760` (10MB) | int64 | Max request body size |
| `DEFAULT_MODEL` | `glm-5` | string | Default model for chat |
| `DEFAULT_PROVIDER` | `glm` | string | Default provider |
| `DEFAULT_TEMPERATURE` | `0.7` | float | Default temperature |
| `DEFAULT_MAX_TOKENS` | `1024` | int | Default max tokens |
| `GEMINI_CODEASSIST_ENDPOINT` | `https://cloudcode-pa.googleapis.com/v1internal` | string | Gemini CodeAssist proxy |
| `GEMINI_API_ENDPOINT` | `https://generativelanguage.googleapis.com` | string | Gemini API endpoint |
| `GEMINI_DEFAULT_MODEL` | `models/gemini-2.5-flash-preview-05-20` | string | Default Gemini model |
| `ANTHROPIC_API_VERSION` | `2023-06-01` | string | Anthropic API version header |
| `MODEL_PRIORITY` | `glm-5.1:100,...` | map | Model priority for routing |
| `ANOMALY_COOLDOWN_SEC` | `5` | int | Anomaly detector cooldown |
| `ANOMALY_Z_THRESHOLD` | `2.0` | float | Z-score threshold for anomaly |
| `GLM_MODE` | `true` | bool | Enable Z.AI features (key pool, vision, model limits) |
| `CLI_SIDECAR_URL` | `http://127.0.0.1:8081` | string | Claude Code billing sidecar URL |
| `CLI_SIDECAR_ENABLED` | `true` | bool | Enable sidecar proxy |
| `QUOTA_CACHE_TTL` | `30s` | duration | Quota cache TTL |
| `QUOTA_DAILY_BUDGET` | `57600` | int64 | Daily token budget |
| `QUOTA_BLOCK_PCT` | `95` | float | Block percentage threshold |
| `QUOTA_REDIS_POOL_SIZE` | `5` | int | Quota Redis pool size |
| `QUOTA_REDIS_MIN_IDLE` | `2` | int | Quota Redis min idle |
| `PROVIDER_MODEL_PREFIXES` | `zai:glm-;anthropic:claude-;...` | string | Provider model prefix mapping |
| `DASHBOARD_API_KEY` | (empty) | string | Optional dashboard auth key |
| `ZAIOpenAIURL` | `https://api.z.ai/api/paas/v4/chat/completions` | string | Z.AI OpenAI-compatible endpoint |
| `ZAIOpenAIModels` | (empty) | map | Models to route through Z.AI OpenAI endpoint |
| `ZAIWebEnabled` | `false` | bool | Enable Z.AI web chat routing |
| `ZAIWebToken` | (empty) | string | JWT Bearer token for chat.z.ai |
| `ZAIWebModels` | (empty) | csv | Models to route through chat.z.ai |
| `ENABLE_AUTO_TRUNCATE` | `true` | bool | Auto-truncation recovery |
| `TRANSIENT_RETRY_MAX` | `3` | int | Max retries on transient errors |
| `PASTEGUARD_ENABLED` | `true` | bool | Enable PasteGuard pipeline |
| `PASTEGUARD_SECRETS_ENABLED` | `true` | bool | Enable secrets detection |
| `PASTEGUARD_PII_ENABLED` | `true` | bool | Enable PII detection |
| `PASTEGUARD_SECRET_ENTITIES` | (empty) | csv | Custom secret entity types |
| `PASTEGUARD_PII_ENTITIES` | (empty) | csv | Custom PII entity types |
| `PASTEGUARD_MAX_SCAN_CHARS` | `200000` | int | Max chars to scan for PII/secrets |
| `GEMINI_OAUTH_CLIENT_ID` | (empty) | string | Gemini OAuth client ID |
| `GEMINI_OAUTH_CLIENT_SECRET` | (empty) | string | Gemini OAuth client secret |
| `LOTUS_UPSTREAM_BASE` | `https://api-cpxis.lotuss.com/llm` | string | Lotus LLM upstream base |
| `LOTUS_API_KEYS` | (empty) | csv | Lotus API keys |
| `DASHBOARD_URL` | (empty) | string | Dashboard URL for OAuth callbacks |
| `OAUTH_CALLBACK_BASE` | (empty) | string | OAuth callback base URL |
| `SIDECAR_PORT` | `8081` | string | Sidecar listen port |

### 2.2 Parsing Functions

- `parseModelLimits("model:limit,...")` - parses `model:limit` pairs into `map[string]int`
- `parseModelPricing("model:in:out,...")` - parses pricing into `map[string]ModelPrice`
- `parseAPIKeys("key1,key2,...")` - splits and trims comma-separated keys
- `ParseProviderModelPrefixes("provider:prefix,...;...")` - parses provider routing rules
- `ParseModelPriority("model:priority,...")` - parses model priority weights

### 2.3 Helper Types

```go
type ModelPrice struct {
    InputPerMillion  float64 // USD per 1M input tokens
    OutputPerMillion float64 // USD per 1M output tokens
}
```

---

## 3. Docker Architecture

### 3.1 Service Overview

All services communicate over the `arl-network` bridge network. Only the Caddy proxy exposes an external port (default 9000).

```
External (port 9000)
    |
    v
arl-proxy (Caddy) -- routes to gateway, dashboard, grafana, rl-dashboard
    |
    +-- arl-gateway (Go, :8080)
    |       |
    |       +-- arl-dragonfly (:6379)
    |       +-- arl-rate-limiter (Java/Spring, :8080)
    |       +-- arl-otel (OTel Collector, :4317/:4318/:8889)
    |       +-- Node.js sidecar (localhost:8081, in-container)
    |
    +-- arl-dashboard (React/Vite, :5173 dev, embedded static for prod)
    +-- arl-grafana (:3000, served at /grafana)
    +-- arl-rl-dashboard (React, served at /rl/)
    +-- arl-prometheus (:9090)
    +-- arl-worker (Python, :9090/:9091)
    +-- arl-presidio (optional, profile: pii)
    +-- claude-code-meow/test (optional, profile: test-client)
```

### 3.2 Service Definitions

#### arl-gateway (API Gateway - Go)
- **Image**: Built from `api-gateway/Dockerfile` (multi-stage)
- **Build**: `golang:1.25-alpine` builder -> `alpine:3.20` runtime (includes `nodejs` for sidecar)
- **Binary**: `/app/api-gateway`
- **Sidecar**: `/app/sidecar/` (Node.js billing header injection)
- **Entrypoint**: `/app/sidecar/entrypoint.sh` (starts sidecar, then exec gateway)
- **Health check**: `curl -sf http://localhost:8080/health` (10s interval, 5s timeout, 3 retries)
- **Resources**: 1G limit / 256M reserved, 2 CPU limit / 0.5 reserved
- **Logging**: json-file, 20m max, 5 files
- **Depends on**: arl-dragonfly (healthy), arl-rate-limiter (healthy)

#### arl-rate-limiter (Distributed Rate Limiter - Java/Spring Boot)
- **Image**: Built from `distributed-rate-limiter/Dockerfile`
- **Port**: 8080 (internal only)
- **Health check**: `curl -f http://localhost:8080/actuator/health` (15s interval, 30s start period)
- **JVM**: G1GC, 75% RAM, String deduplication
- **Resources**: 1.5G limit / 512M reserved
- **Depends on**: arl-dragonfly (healthy)

#### arl-dragonfly (Redis-compatible in-memory store)
- **Image**: `ghcr.io/dragonflydb/dragonfly:v1.37.2`
- **Port**: 6379 (internal only)
- **Flags**: `--maxmemory=4gb --proactor_threads=4 --cache_mode=true --tcp_keepalive=60 --pipeline_squash=10`
- **Health check**: `redis-cli ping` (5s interval)
- **Resources**: 8G limit / 1G reserved
- **Volume**: `arl-dragonfly-data:/data`

#### arl-worker (AI Worker - Python)
- **Image**: Built from `ai-worker/Dockerfile`
- **Ports**: 9090 (Prometheus metrics), 9091 (internal metrics)
- **Health check**: `curl -f http://localhost:9091/metrics-internal` (15s interval)
- **Concurrency**: 50 workers (configurable via `WORKER_CONCURRENCY`)
- **Resources**: 2G limit / 512M reserved

#### arl-prometheus
- **Image**: `prom/prometheus:v2.54.1`
- **Port**: 9090 (internal only)
- **Flags**: 30d retention, 10GB max, WAL compression, lifecycle API enabled
- **Volume**: `arl-prometheus-data:/prometheus`, `prometheus/prometheus.yml` (ro)

#### arl-grafana
- **Image**: `grafana/grafana:11.3.0`
- **Port**: 3000 (internal, proxied at /grafana)
- **Auth**: Admin password required via `GRAFANA_ADMIN_PASSWORD`
- **Served from sub-path**: `GF_SERVER_SERVE_FROM_SUB_PATH=true`
- **Volume**: `arl-grafana-data:/var/lib/grafana`, `grafana/provisioning/` (ro)

#### arl-otel (OpenTelemetry Collector)
- **Image**: `otel/opentelemetry-collector-contrib:0.112.0`
- **Ports**: 4317 (gRPC), 4318 (HTTP), 8889 (Prometheus export)
- **Pipeline**: OTLP recv -> memory_limiter -> batch -> debug (traces), prometheus (metrics)
- **Resources**: 512M limit / 128M reserved

#### arl-dashboard (UI - React/Vite/Bun)
- **Image**: Built from `ui/Dockerfile.dev`
- **Port**: 5173 (Vite dev server)
- **Volumes**: Source mounted read-only for hot reload
- **Health check**: `curl -sf http://localhost:5173` (10s interval)
- **Resources**: 1G limit / 256M reserved

#### arl-proxy (Reverse Proxy - Caddy)
- **Image**: `caddy:2-alpine`
- **Port**: `${PROXY_PORT:-9000}` (only external port)
- **Routes**:
  - `/v1/*` -> arl-gateway:8080 (flush_interval -1 for SSE streaming)
  - `/api/*` -> arl-gateway:8080
  - `/health` -> arl-gateway:8080
  - `/metrics` -> arl-gateway:8080
  - `/callback` -> arl-gateway:8080
  - `/ws` -> arl-gateway:8080
  - `/grafana/*` -> arl-grafana:3000
  - `/rl/*` -> arl-rl-dashboard:8080 (path stripped)
  - `/*` (catch-all) -> arl-dashboard:5173

#### arl-presidio (PII Analyzer - Optional)
- **Image**: `mcr.microsoft.com/presidio-analyzer:2.2.362`
- **Profile**: `pii` (not started by default)
- **Port**: 3000

#### claude-code-meow / claude-code-test (Test Clients - Optional)
- **Profile**: `test-client`
- **TTY**: enabled
- **Volumes**: `./workspace:/workspace`

### 3.3 Shared Environment (`x-common-env`)

```yaml
REDIS_ADDR: arl-dragonfly:6379
RATE_LIMITER_ADDR: http://arl-rate-limiter:8080
QUEUE_NAME: ai_jobs
OTLP_ENDPOINT: arl-otel:4317
```

### 3.4 Volumes

| Volume | Purpose |
|---|---|
| `arl-dragonfly-data` | Dragonfly persistence |
| `arl-prometheus-data` | Prometheus TSDB (30d retention) |
| `arl-grafana-data` | Grafana dashboards, settings |

### 3.5 Network

Single bridge network `arl-network`. All services attached. Only `arl-proxy` publishes a port.

---

## 4. Sidecar Architecture

### 4.1 Node.js Billing Sidecar (`api-gateway/sidecar/`)

A lightweight Node.js HTTP proxy that injects Claude Code billing headers into Anthropic API requests.

#### Files
- `entrypoint.sh` - starts sidecar in background, then execs Go gateway
- `index.js` - the proxy server (~170 lines)
- `package.json` - minimal metadata

#### How It Works
1. Listens on `SIDECAR_PORT` (default 8081) on localhost
2. Accepts POST requests from the Go gateway
3. Parses JSON body, extracts first user message text
4. Computes `x-anthropic-billing-header` with version, build hash (SHA256 of salt + sampled chars + version)
5. Injects billing header and CLI identity prompt into `system` array
6. Forwards to `https://api.anthropic.com` with original headers (minus hop-by-hop headers)
7. Preserves `Authorization: Bearer` header (does NOT convert to `x-api-key`)
8. Pipes response back to caller

#### Billing Header Format
```
x-anthropic-billing-header: cc_version=2.1.123.{hash}; cc_entrypoint=cli; cch=00000;
```

#### Identity Prompt
```
You are Claude Code, Anthropic's official CLI for Claude.
```

#### Header Injection Logic
- If `system` is null: creates `[{billing}, {identity}]`
- If `system` is string: wraps as `[{billing}, {identity}, {original}]`
- If `system` is array: prepends billing and identity (if not already present)

#### Lifecycle
- Started by `entrypoint.sh` before Go gateway
- 0.5s delay for port binding
- HTTP keep-alive agent (10 max sockets)

---

## 5. Claude Code Integration

### 5.1 OAuth Flow (PKCE)

Provider: `claude-oauth` in registry.

- **Auth URL**: `https://claude.com/cai/oauth/authorize`
- **Token URL**: `https://api.anthropic.com/v1/oauth/token` (JSON body, not form-urlencoded)
- **Client ID**: `9d1c250a-e61b-44d9-88ed-5944d1962f5e` (bundled, from Claude Code CLI)
- **PKCE**: No client secret - uses code_verifier/code_challenge
- **Scopes**: `user:inference`, `user:profile`, `user:sessions:claude_code`, `user:mcp_servers`, `user:file_upload`, `org:create_api_key`
- **Token format**: `sk-ant-oat-*` prefix
- **Authorization header**: `Bearer {token}` with `anthropic-beta: oauth-2025-04-20`

### 5.2 Session Bootstrap (`claude_session.go`)

On first request with `sk-ant-oat-*` token, the gateway bootstraps a session by fetching:

1. **Profile**: `GET https://api.anthropic.com/api/oauth/profile`
2. **Roles**: `GET https://api.anthropic.com/api/oauth/claude_cli/roles` (required)
3. **Settings**: `GET https://api.anthropic.com/api/claude_code/settings`
4. **Policy limits**: `GET https://api.anthropic.com/api/claude_code/policy_limits`

Sessions cached in-memory via `sync.Map`. Bootstrapped session includes all four responses.

### 5.3 Passthrough Routes

These routes proxy directly to Anthropic API without optimizer/masking:
- `/api/claude_code/policy_limits`
- `/api/claude_code/settings`
- `/v1/mcp_servers`

### 5.4 Docker Test Clients

Two Claude Code test containers (`claude-code-meow`, `claude-code-test`):
- Built from `docker/Dockerfile.claude-code`
- Profile: `test-client` (not started by default)
- Environment: `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`, `CLAUDE_CODE_DISABLE_ANALYTICS=1`
- Interactive: `stdin_open: true`, `tty: true`

### 5.5 Claude Code Config (`CLAUDE_CODE_SIMPLE`)

When running in Docker, use `CLAUDE_CODE_SIMPLE=1` environment variable instead of `--bare` flag for non-interactive mode.

---

## 6. UI Components

### 6.1 Tech Stack

- **Framework**: React 19 + TypeScript 5.9
- **Build**: Vite 7 + Bun
- **Routing**: react-router-dom 7
- **UI**: Radix UI primitives + Tailwind CSS 4 + class-variance-authority
- **Charts**: Recharts 2.14
- **Icons**: Lucide React
- **Notifications**: Sonner (toast)

### 6.2 Pages

| Page | Directory | Description |
|---|---|---|
| **Overview** | `pages/overview/` | Status cards (health, queue, requests, concurrency), global capacity gauge, model utilization bars, event timeline |
| **Analytics** | `pages/analytics/` | Cost by model, token breakdown, usage trends, latency charts, hourly breakdown, model distribution, anomaly insights |
| **Models** | `pages/models/` | Model listing and status |
| **Model Limits** | `pages/model-limits/` | Adaptive limiter status, per-model concurrency, override controls |
| **Key Pool** | `pages/key-pool/` | API key health indicators, pool summary, per-key RPM tracking |
| **Providers** | `pages/providers/` | 18+ provider cards with connect dialogs (API Key, OAuth, Device Code, Session Cookie), account management, custom providers |
| **Profiles** | `pages/profiles/` | Per-profile usage tracking |
| **Quota** | `pages/quota/` | Daily budget, utilization percentage |
| **Privacy** | `pages/privacy/` | PasteGuard metrics: masked requests, secrets by type, PII by type, mask duration P95 |
| **Health** | `pages/health/` | Health check groups (gateway, queue, models, key pool, infrastructure), health gauge, summary bar |
| **Metrics** | `pages/metrics/` | Raw Prometheus metrics viewer |
| **Logs** | `pages/logs/` | Error log viewer |
| **Settings** | `pages/settings/` | Language, polling interval, theme, notification preferences, history retention |
| **Controls** | `pages/controls/` | Manual controls for limiter, mock data |
| **Debug** | `pages/debug/` | Debug/mock data controls |
| **Login** | `pages/login/` | Dashboard auth (DASHBOARD_API_KEY) |

### 6.3 API Layer (`src/lib/`)

| File | Purpose |
|---|---|
| `api.ts` | Core API: limiter status, health, metrics, overrides, profile usage, account usage, waste findings, Prometheus text parser |
| `auth-api.ts` | Auth flows: device code, auth code (PKCE), API key registration, session cookie, account CRUD (pause/resume/default/email), rate limit status, dashboard login/logout |
| `providers.ts` | Provider info, color coding, custom provider CRUD, upstream URL update |
| `privacy-api.ts` | Privacy metrics extraction from Prometheus data |
| `privacy.ts` | CSS blur class for sensitive data display |
| `metrics-helpers.ts` | Extract model tokens, costs, errors, latency, infra metrics from parsed Prometheus data |
| `health-checks.ts` | Derive health check groups from system state (gateway, queue, models, key pool, infra) |
| `ws-events.ts` | WebSocket event bus with typed listeners and wildcard support |
| `format.ts` | Number/time formatting utilities |
| `i18n.ts` | Internationalization support |
| `polling.ts` | Configurable polling intervals |
| `clipboard.ts` | Clipboard utilities |
| `utils.ts` | General utilities (cn class merge) |

### 6.4 WebSocket Events

Real-time events pushed from gateway to dashboard:
- `config-changed` - .env file modified (broadcast by config watcher)
- `request-completed` - request finished processing
- `request-error` - request failed
- `anomaly-detected` - Z-score anomaly detected
- `quota-warning` - quota threshold exceeded
- `ratelimit-updated` - rate limit state changed

### 6.5 Dashboard Auth

Optional auth via `DASHBOARD_API_KEY` env var:
- Login endpoint: `POST /v1/auth/login` with `{api_key}`
- Auth middleware wraps `/admin/*` routes
- Session managed via signed cookies (session secret in `config/session_secret`)

---

## 7. Metrics & Monitoring

### 7.1 Prometheus Metrics

All metrics use namespace `api_gateway`. Exposed at `/metrics` and `/api/metrics`.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `api_gateway_request_latency_seconds` | histogram | method, path, status | Request latency (buckets: 0.05-60s) |
| `api_gateway_queue_depth` | gauge | - | Current job queue length |
| `api_gateway_error_total` | counter | type | Errors by category |
| `api_gateway_rate_limit_hits_total` | counter | key | Rate-limited requests (agent keys SHA1-hashed) |
| `api_gateway_active_connections` | gauge | - | Current active HTTP connections |
| `api_gateway_token_input_total` | counter | model | Input tokens consumed |
| `api_gateway_token_output_total` | counter | model | Output tokens generated |
| `api_gateway_upstream_retries_total` | counter | - | 429 retry count |
| `api_gateway_upstream_429_total` | counter | - | Upstream 429 responses |
| `api_gateway_adaptive_limit` | gauge | model | Current adaptive concurrency limit |
| `api_gateway_adaptive_in_flight` | gauge | model | Current in-flight per model |
| `api_gateway_cost_total` | counter | model, type | Estimated cost USD (input/output) |
| `api_gateway_model_fallback_total` | counter | requested, selected | Model fallback events |
| `api_gateway_ttfb_seconds` | histogram | model | Time to first byte (streaming) |
| `api_gateway_profile_requests_total` | counter | profile, model | Per-profile request count |
| `api_gateway_profile_token_input_total` | counter | profile, model | Per-profile input tokens |
| `api_gateway_profile_token_output_total` | counter | profile, model | Per-profile output tokens |
| `api_gateway_profile_cost_total` | counter | profile, model, type | Per-profile cost |
| `api_gateway_optimizer_chars_saved_total` | counter | technique | Characters saved by optimizer |
| `api_gateway_optimizer_runs_total` | counter | technique | Optimizer execution count |
| `api_gateway_optimizer_duration_seconds` | histogram | technique | Optimizer execution time |
| `api_gateway_optimizer_tokens_saved_total` | counter | - | Total estimated tokens saved |
| `api_gateway_context_truncation_total` | counter | model | Auto-truncation recovery count |
| `api_gateway_transient_retry_total` | counter | status, model | Transient error retries |
| `api_gateway_billing_path_requests_total` | counter | path, model, profile | Billing routing events |
| `api_gateway_billing_path_latency_seconds` | histogram | path, model | Billing path latency |
| `api_gateway_budget_level` | gauge | model | Budget utilization (0=green, 1=yellow, 2=red) |
| `api_gateway_cost_savings_total` | counter | - | Total cost savings USD |
| `api_gateway_waste_findings_total` | counter | detector, severity | Waste detection events |
| `api_gateway_waste_tokens_wasted_total` | counter | detector | Tokens identified as waste |
| `api_gateway_go_goroutines` | gauge | - | Goroutine count |
| `api_gateway_go_heap_alloc_bytes` | gauge | - | Heap allocation |
| `api_gateway_go_heap_objects` | gauge | - | Heap object count |
| `api_gateway_go_gc_pause_ns` | gauge | - | Last GC pause |
| `api_gateway_go_stack_inuse_bytes` | gauge | - | Stack in-use |
| `api_gateway_dragonfly_up` | gauge | - | Dragonfly health (1/0) |
| `api_gateway_mask_requests_total` | counter | - | PasteGuard masked requests |
| `api_gateway_secrets_detected_total` | counter | type | Secrets detected by type |
| `api_gateway_pii_detected_total` | counter | type | PII detected by type |
| `api_gateway_mask_duration_seconds` | histogram | phase | PasteGuard masking latency |

### 7.2 Prometheus Scrape Config

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s
  external_labels:
    cluster: agent-rate-limit
    replica: '1'

scrape_configs:
  - job_name: api-gateway
    metrics_path: /metrics
    static_configs:
      - targets: ['arl-gateway:8080']
    scrape_interval: 10s

  - job_name: ai-worker
    static_configs:
      - targets: ['arl-worker:9090']
    scrape_interval: 10s

  - job_name: rate-limiter
    metrics_path: /actuator/prometheus
    static_configs:
      - targets: ['arl-rate-limiter:8080']
    scrape_interval: 10s

  # Dragonfly does not expose Prometheus metrics on port 6379 (Redis protocol).
  # Enable Dragonfly's --metrics flag or use redis_exporter as a sidecar.
  # - job_name: dragonfly
  #   static_configs:
  #     - targets: ['arl-dragonfly:6379']
  #   scrape_interval: 10s

  - job_name: otel-collector
    static_configs:
      - targets: ['arl-otel:8889']
    scrape_interval: 10s

  - job_name: prometheus
    static_configs:
      - targets: ['localhost:9090']
```

### 7.3 Grafana Dashboards

9 dashboards in `grafana/provisioning/dashboards/`:

| Dashboard | File |
|---|---|
| API Gateway Overview | `api-gateway-overview.json` |
| API Gateway Runtime | `api-gateway-runtime.json` |
| Token Optimization | `token-optimization.json` |
| PasteGuard Privacy | `pasteguard.json` |
| Cost Calculator | `cost-calculator.json` |
| System Overview | `system-overview.json` |
| Claude OAuth Billing | `claude-oauth-billing.json` |
| AI Worker | `ai-worker.json` |
| (duplicate) API Gateway Overview | `api-gateway/` subdirectory |

### 7.4 OpenTelemetry Pipeline

```yaml
receivers: [otlp (grpc:4317, http:4318)]
processors: [memory_limiter (200MiB), batch (5s, 1024)]
exporters: [debug (traces), prometheus (metrics, :8889)]
```

### 7.5 Mock Data System

For testing Grafana dashboards without live traffic:
- `POST /v1/mock/seed?category=all|optimizer|waste|budget` - seed initial data
- `POST /v1/mock/loop/start` - start continuous mock data (5s interval)
- `POST /v1/mock/loop/stop` - stop loop
- `GET /v1/mock/status` - check loop state

---

## 8. Build Pipeline

### 8.1 UI Build -> Go Embed

1. **UI build**: `cd ui && bun run build` (Vite + TypeScript)
2. **Output**: `ui/dist/` directory
3. **Go embed**: `api-gateway/main.go` contains `//go:embed all:static` directive
4. **Static serving**: Embedded FS served at `/admin`, `/admin/*` (SPA fallback), `/assets/*`

### 8.2 Docker Build (Multi-stage)

```dockerfile
# Stage 1: Build Go binary
FROM golang:1.25-alpine AS builder
RUN apk add git ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/api-gateway .

# Stage 2: Runtime
FROM alpine:3.20
RUN apk add ca-certificates curl nodejs
COPY --from=builder /bin/api-gateway /app/api-gateway
COPY sidecar/ /app/sidecar/
RUN chmod +x /app/sidecar/entrypoint.sh
USER gateway:gateway
EXPOSE 8080
ENTRYPOINT ["/app/sidecar/entrypoint.sh"]
```

### 8.3 Entrypoint Sequence

```bash
#!/bin/sh
set -e
node /app/sidecar/index.js &    # Start billing sidecar
sleep 0.5                        # Wait for port binding
exec /app/api-gateway            # Start Go gateway (PID 1)
```

---

## 9. Redis/Dragonfly Usage

### 9.1 Key Patterns

| Key Pattern | Type | TTL | Purpose |
|---|---|---|---|
| `ai_jobs` | LIST | - | Job queue (LPUSH/BRPOP) |
| `result:{requestID}` | STRING | 10 min | Cached async job results |
| `provider:token:{provider}:{accountID}` | HASH | - | OAuth token storage |
| `profile:{name}` | HASH | - | Profile configuration |
| `usage:profile:{name}` | HASH | - | Per-profile usage counters |
| `usage:account:{accountID}` | HASH | - | Per-account usage counters |
| `usage:model:{model}` | HASH | - | Per-model usage counters |
| `quota:{date}` | HASH | 48h | Daily quota tracking |
| `arl:providers:custom:{id}` | STRING (JSON) | - | Custom provider configs |
| `config:max_tokens:*` | STRING | - | Persisted max-tokens overrides |
| `optimizer:*` | Various | Various | Optimizer state (cache, delta, etc.) |
| `config/session_secret` | FILE | - | Session signing secret (filesystem, not Redis) |

### 9.2 Connection Pool

- **Gateway main pool**: 50 connections, 10 min idle, 5m max idle time, 30m max lifetime
- **Optimizer shared pool**: 50 connections (separate `redis.NewClient`)
- **Quota pool**: 5 connections, 2 min idle
- **Runtime metrics**: single connection for health ping

### 9.3 Dragonfly Configuration

```bash
--maxmemory=4gb           # Memory limit
--proactor_threads=4      # IO threads
--cache_mode=true         # LRU eviction
--tcp_keepalive=60        # Keep-alive
--pipeline_squash=10      # Pipeline optimization
```

---

## 10. Worker/Async System

### 10.1 Job Queue

- **Queue key**: `ai_jobs` (Redis LIST)
- **Producer**: Go gateway (`DragonflyClient.PushJob`)
- **Consumer**: Python worker (BRPOP)
- **Job format**:
  ```json
  {
    "request_id": "uuid",
    "agent_id": "agent-123",
    "model": "glm-5",
    "messages": [...],
    "max_tokens": 1024,
    "temperature": 0.7,
    "provider": "glm",
    "retry_count": 0,
    "metadata": {}
  }
  ```

### 10.2 Result Polling

- **Result key**: `result:{requestID}`
- **TTL**: 10 minutes
- **Flow**: Client polls `GET /v1/results/{requestID}` -> gateway returns cached result or empty (202 pending)

### 10.3 Worker Configuration (Python)

| Env Var | Default | Description |
|---|---|---|
| `WORKER_CONCURRENCY` | `50` | Max concurrent tasks |
| `MAX_RETRIES` | `3` | Max retry attempts per job |
| `BASE_BACKOFF` | `1.0` | Exponential backoff base (seconds) |
| `RESULT_TTL` | `600` | Result cache TTL (seconds) |

### 10.4 Worker Multi-provider Support

Workers accept keys for multiple providers:
- `GLM_API_KEYS` / `GLM_ENDPOINT`
- `OPENAI_API_KEYS`
- `ANTHROPIC_API_KEYS`
- `GEMINI_API_KEYS`
- `OPENROUTER_API_KEYS`

---

## 11. Provider Ecosystem

### 11.1 Built-in Providers (18)

| ID | Name | Auth Type | Upstream |
|---|---|---|---|
| `zai` | Z.AI | API Key | `api.z.ai/api/anthropic` |
| `anthropic` | Anthropic | API Key | `api.anthropic.com` |
| `claude-oauth` | Claude (OAuth) | OAuth/PKCE | `api.anthropic.com` |
| `openai` | OpenAI | API Key | `api.openai.com` |
| `gemini` | Gemini | API Key | `generativelanguage.googleapis.com` |
| `gemini-oauth` | Gemini (OAuth) | OAuth | `cloudcode-pa.googleapis.com` |
| `copilot` | GitHub Copilot | Device Code | `api.github.com/copilot` |
| `openrouter` | OpenRouter | API Key | `openrouter.ai/api` |
| `qwen` | Qwen (Aliyun) | Device Code | `dashscope.aliyuncs.com` |
| `deepseek` | DeepSeek | API Key | `api.deepseek.com` |
| `kimi` | Kimi (Moonshot) | API Key | `api.moonshot.cn/v1` |
| `huggingface` | Hugging Face | API Key | `api-inference.huggingface.co/models` |
| `ollama` | Ollama | API Key | `localhost:11434` |
| `agy` | Antigravity | API Key | `antigravity.com` |
| `cursor` | Cursor | API Key | `api2.cursor.sh` |
| `codebuddy` | CodeBuddy | API Key | `api.codebuddy.io` |
| `kilo` | Kilo | API Key | `api.kilo.ai` |
| `lotus` | Lotus LLM | API Key | `api-cpxis.lotuss.com/llm` |

### 11.2 Custom Providers

Users can register custom providers via dashboard UI:
- `POST /v1/providers/custom` - register with name, format, upstream, optional API key
- `DELETE /v1/providers/custom/{id}` - delete custom provider
- Stored in Redis as `arl:providers:custom:{id}` (JSON)
- Loaded on startup via `Registry.LoadCustomProviders()`

### 11.3 Provider Upstream Override

- `PUT /v1/providers/{providerId}/upstream` - change upstream URL at runtime
- Applies immediately to registry without restart

### 11.4 Provider Model Prefix Routing

Configured via `PROVIDER_MODEL_PREFIXES`:
```
zai:glm-;anthropic:claude-;claude:claude-;openai:gpt-,o3,o4-;gemini:gemini-;gemini-oauth:gemini-;openrouter:or-;qwen:qwen-
```

Additional model routing rules (not in prefix config, handled in resolver):
- `anthropic/` -> anthropic, openrouter (fallback)
- `openai/` -> openrouter
- `google/` -> openrouter
- `meta/` -> openrouter
- `deepseek/` -> openrouter
- `qwen/` -> openrouter
- `huggingface/` -> huggingface
- `ollama` -> ollama
- `agy-` -> agy
- `lotus-` -> lotus

Routes requests to the correct provider based on model name prefix.

---

## 12. Token Refresh System

### 12.1 Refresh Worker

- **Interval**: 30 minutes (configurable)
- **Startup**: Immediate refresh on start (before first tick)
- **Threshold**: Refresh tokens expiring within 45 minutes
- **Auto-cleanup**: Delete expired tokens with no refresh_token or removed provider
- **Retry**: 3 attempts with exponential backoff (5s, 10s, 20s)

### 12.2 Provider-specific Refresh

- **Claude OAuth**: JSON body to `api.anthropic.com/v1/oauth/token`
- **Gemini OAuth**: Form-urlencoded to `oauth2.googleapis.com/token`
- **Others**: Form-urlencoded with client_id + client_secret

### 12.3 Gemini Project Resolution

For `gemini-oauth` tokens without a `projectID`:
1. Call `loadCodeAssist` endpoint
2. If empty, call `onboardUser` (creates free-tier project)
3. Poll Long Running Operation up to 3 times (5s interval)
4. Fallback: retry `loadCodeAssist`

---

## 13. Graceful Shutdown

1. Signal handler catches `SIGINT` and `SIGTERM`
2. Context cancellation propagated to:
   - Background goroutines (runtime metrics, waste scanner, cache eviction, config watcher, session secret watcher)
   - Token refresh worker (`Stop()` closes channel)
3. HTTP server shutdown with 10s timeout
4. Dragonfly/Redis connections closed via `defer`
5. OpenTelemetry tracer shutdown with 5s timeout
