# Getting Started

## 1. Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│ Multi-Agent AI System                                                │
│                                                                      │
│ ┌───────────┐    ┌──────────────┐    ┌──────────────┐                │
│ │ Client    │───▶│ API Gateway  │───▶│ Rate Limiter │                │
│ │ (Claude/  │    │ (Go/chi)     │    │ (Java/Spring)│                │
│ │  Agent)   │    │ :8080        │    │ :8080        │                │
│ └───────────┘    └──────┬───────┘    └──────┬───────┘                │
│                         │                    │                       │
│                  ┌──────▼──────┐      ┌──────▼──────┐                │
│                  │ Dragonfly   │◀─────│ Token       │                │
│                  │ (Redis)     │      │ Bucket      │                │
│                  │ :6379       │      │ Store       │                │
│                  └──────┬──────┘      └─────────────┘                │
│                         │                                            │
│ ┌───────────────────────▼────────────────────────────┐               │
│ │ AI Worker (Python)                                  │              │
│ │ ┌───┬───┬───┬───┬───┬───┬───┬───┐                 │                │
│ │ │W0 │W1 │W2 │.. │.. │.. │.. │W49│                 │                │
│ │ └───┴───┴───┴───┴───┴───┴───┴───┘                 │                │
│ │  WORKER_CONCURRENCY=50                              │              │
│ │ Per-Model Semaphores:                               │              │
│ │  glm-5.1(1) glm-5-turbo(1) glm-5(2)                │               │
│ │  glm-4.7(2) glm-4.6(3) glm-4.5(10)                 │               │
│ │ Vision: glm-4.6v(10) glm-4.5v(10)                  │               │
│ │ Global Limit: 9 concurrent                          │              │
│ └───────────────────────┬────────────────────────────┘               │
│                         │                                            │
│ ┌───────────────────────▼────────────────────────────┐               │
│ │ Provider Fallback Chain (18 providers)              │              │
│ │ claude-oauth → anthropic → gemini-oauth → gemini   │               │
│ │ → openai → zai → copilot → openrouter → qwen      │                │
│ │ → deepseek → kimi → huggingface → ollama          │                │
│ │ Profile: X-Profile header / arl_ API token         │               │
│ └─────────────────────────────────────────────────────┘              │
│                                                                      │
│ ┌──────────────── Observability Stack ─────────────────┐             │
│ │ OpenTelemetry → Prometheus → Grafana                  │            │
│ │ Caddy Proxy :9000 (external entry point)              │            │
│ └──────────────────────────────────────────────────────┘             │
└──────────────────────────────────────────────────────────────────────┘
```

### System Components

| Service | Technology | Port | Purpose |
|---------|-----------|------|---------|
| **arl-gateway** | Go (chi router) | 8080 (internal) | HTTP proxy, rate limit check, queue, OAuth, profiles |
| **arl-proxy** | Caddy | 9000 (external) | Reverse proxy to gateway + dashboard |
| **arl-rate-limiter** | Java / Spring Boot | 8080 (internal) | Token bucket rate limiting, admin API |
| **arl-dragonfly** | DragonflyDB v1.37 (Redis-compatible) | 6379 (internal) | Cache, queue, rate limit state, token storage |
| **arl-worker** | Python (asyncio + httpx) | 9090/9091 (internal) | AI job processing, provider fallback |
| **arl-rl-dashboard** | React + Vite + nginx | internal | Rate limiter web management UI |
| **arl-dashboard** | React + Vite + Bun | 5173 (internal) | Gateway dashboard SPA (embedded in Go binary) |
| **arl-prometheus** | Prometheus v2.54 | 9090 (internal) | Metrics collection |
| **arl-grafana** | Grafana 11.3 | internal | Dashboard & visualization |
| **arl-otel** | OTel Collector Contrib 0.112 | 4317/4318 (internal) | Trace & metric pipeline |

## 2. Traffic Flow

### Sync Mode (for Claude Code)

```
Claude Code
|
| POST /v1/messages (Anthropic API format)
| Header: x-api-key / Authorization: Bearer
| Header: anthropic-version: 2023-06-01
| Header: X-Profile: <profile-name> (optional)
|
v
API Gateway (:8080)
|
+- Rate Limit Check (per API key)
|   |
|   v
|   Rate Limiter -> Dragonfly (token bucket)
|   |
|   +- Pass: forward to upstream
|   +- Fail: return 429 Rate Limit Error (Anthropic format)
|
+- Profile detection (arl_ token / X-Profile header):
|   +- Present: load profile from Redis -> use target provider + token pool
|   +- Absent: use normal routing (resolver + key pool)
|
+- Provider resolver: match model prefix to provider route table
|   +- claude-* -> claude-oauth -> anthropic
|   +- gemini-* -> gemini-oauth -> gemini
|   +- gpt-*/o3-*/o4-* -> openai
|   +- glm-* -> zai
|   +- or-* -> openrouter
|   +- (others) -> matching provider
|
+- Claude OAuth transparent passthrough:
|   +- Client sends Bearer sk-ant-oat01-* -> transparent mode
|   +- Go billing injection -> Sidecar fallback -> Direct proxy
|
+- Content processing (non-transparent only):
|   +- System prompt injection (ENABLE_PROMPT_INJECTION)
|   +- Smart max_tokens auto-adjust
|   +- Strip unsupported fields (context_management, thinking for haiku)
|   +- Token optimization pipeline (13 optimizers)
|   +- Privacy masking (PasteGuard: secrets + PII)
|
+- No image (Text Request):
|   v
|   Upstream Provider (format-aware proxy)
|   +- FormatAnthropic -> Anthropic proxy
|   +- FormatOpenAI -> OpenAI proxy
|   +- FormatGemini -> Gemini CodeAssist / API proxy
|   |
|   v
|   SSE Streaming Response -> relay back to client chunk by chunk
|
+- Has image (Vision Request):
|   v
|   Auto-route to vision model (glm-4.6v default)
|   Convert Anthropic image format -> provider format
|
+- Response back to client
```

### Async Mode (for Batch Agents)

```
Client
│
│ POST /v1/chat/completions (OpenAI format)
│ { model, agent_id, messages }
│
▼
API Gateway (:8080)
│
├─ Rate Limit Check
├─ Push to Dragonfly queue: BRPOP ai_jobs
├─ Return 202: { request_id, status: "queued" }
│
▼ (background)
AI Worker (Python)
│
├─ BRPOP ai_jobs → pop job
├─ Resolve provider (fallback chain)
├─ Per-model semaphore (concurrency control)
├─ Call AI provider API
├─ Retry on failure (exponential backoff)
├─ Store result in Redis: SET ai_result:{id}
│
▼
Client polls: GET /v1/results/{id}
├─ 202: processing
├─ 200: { result, tokens, model, provider }
└─ 404: not found

```

## 3. Tech Stack

### API Gateway (Go)
- **chi** — HTTP router (lightweight, idiomatic Go)
- **go-redis** — Redis/Dragonfly client
- **net/http** — Standard library HTTP client for proxy
- **prometheus/client_golang** — Prometheus metrics

### Rate Limiter (Java)
- **Spring Boot 3** — Web framework
- **Spring Data Redis** — Redis integration (Lettuce client)
- **Spring Actuator** — Health & metrics endpoints
- **Token Bucket** — Rate limiting algorithm

### AI Worker (Python)
- **asyncio** — Async runtime (built-in, no FastAPI/Flask since worker is a background consumer)
- **httpx** — Async HTTP client for AI provider APIs
- **redis (hiredis)** — Async Redis client for queue operations
- **anthropic / openai / google-generativeai** — Provider SDKs
- **pydantic-settings** — Config management (reads from env vars)
- **structlog** — Structured JSON logging
- **prometheus-client** — Prometheus metrics export
- **OpenTelemetry SDK** — Distributed tracing

> **Why not FastAPI/Flask?**
> AI Worker is a **background job consumer** — runs `BRPOP` continuously to pull jobs from queue. No HTTP server accepting external requests (only metrics server). Uses `asyncio` event loop to manage 50 coroutines directly.

### Rate Limiter Dashboard (React)
- **React 18** — UI framework
- **Vite** — Build tool
- **Recharts** — Charts
- **TailwindCSS + shadcn/ui** — Styling & components
- **React Router** — Client-side routing
- **nginx** — Static file serving + API proxy

## 4. Installation

### Prerequisites

- Docker Desktop (or Docker Engine + Docker Compose)
- RAM minimum 4GB (recommended 8GB+)
- Disk space minimum 5GB

### Steps

```bash
# 1. Clone project
git clone <repo-url>
cd agent-rate-limit

# 2. Create .env from template
cp .env.example .env

# 3. Edit .env — add API keys
# At minimum, add GLM_API_KEYS
vim .env

# 4. Run everything
docker-compose up -d --build

# 5. Check all services healthy
docker-compose ps
```

### Verification

```bash
# Check all service status
docker-compose ps

# View all logs
docker-compose logs -f

# View specific service logs
docker-compose logs -f arl-gateway
docker-compose logs -f arl-worker
docker-compose logs -f arl-rate-limiter
```

### Expected Status

```
NAME                STATUS
arl-gateway         Up (healthy)
arl-proxy           Up (healthy)
arl-rate-limiter    Up (healthy)
arl-dragonfly       Up (healthy)
arl-worker          Up (healthy)
arl-dashboard       Up (healthy)
arl-rl-dashboard    Up
arl-prometheus      Up
arl-grafana         Up
arl-otel            Up
```

## 5. Environment Variables

File `.env` stores all configuration. Copy from `.env.example`:

```bash
cp .env.example .env
```

### API Gateway

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_PORT` | `:8080` | Gateway listen address |
| `REDIS_ADDR` | `dragonfly:6379` | Dragonfly/Redis address |
| `RATE_LIMITER_ADDR` | `http://rate-limiter:8080` | Rate limiter service URL |
| `QUEUE_NAME` | `ai_jobs` | Redis queue name for async jobs |
| `GLOBAL_RATE_LIMIT` | `100` | Global rate limit (req/min) |
| `AGENT_RATE_LIMIT` | `5` | Per-agent/key rate limit (req/min) |
| `WORKER_POOL_SIZE` | `100` | Goroutine pool size for async mode |
| `READ_TIMEOUT` | `30s` | HTTP read timeout |
| `WRITE_TIMEOUT` | `10s` | HTTP write timeout (0 for SSE) |
| `OTLP_ENDPOINT` | `otel-collector:4317` | OpenTelemetry collector endpoint |
| `REDIS_POOL_SIZE` | `50` | Redis connection pool size |
| `REDIS_MIN_IDLE_CONNS` | `10` | Redis minimum idle connections |
| `UPSTREAM_URL` | `https://api.z.ai/api/anthropic` | Upstream AI provider endpoint |
| `ANTHROPIC_DIRECT_URL` | `https://api.anthropic.com` | Direct Anthropic API URL |
| `STREAM_TIMEOUT` | `300s` | Streaming request timeout |
| `UPSTREAM_MODEL_LIMITS` | `glm-5.1:1,glm-5-turbo:1,...` | Per-model concurrent limits |
| `UPSTREAM_VISION_MODEL_LIMITS` | `glm-5.1:5,glm-4.6v:5,...` | Vision model concurrent limits |
| `UPSTREAM_DEFAULT_LIMIT` | `3` | Default limit for unlisted models |
| `UPSTREAM_GLOBAL_LIMIT` | `9` | Max concurrent requests across all models |
| `UPSTREAM_MAX_RETRIES` | `3` | Max retries on 429 errors |
| `UPSTREAM_RETRY_BACKOFF` | `500ms` | Retry backoff base duration |
| `ZAI_API_KEYS` | `` | Comma-separated API keys for key pool |
| `UPSTREAM_RPM_LIMIT` | `40` | Per-key requests-per-minute budget |
| `UPSTREAM_PROBE_MULTIPLIER` | `5` | Adaptive limiter probe ceiling multiplier |
| `ENABLE_PROMPT_INJECTION` | `true` | Inject system prompt for conciseness |
| `ENABLE_RESPONSE_TRIM` | `true` | Trim response whitespace |
| `ENABLE_SMART_MAX_TOKENS` | `true` | Auto-set max_tokens per model |
| `PROMPT_INJECTION_TEXT` | (built-in) | Custom system prompt injection text |
| `MAX_REQUEST_BODY` | `10485760` (10MB) | Max request body size in bytes |
| `MODEL_PRICING` | `glm-5.1:1.4:4.4,...` | Per-model pricing (input:output per 1M tokens) |
| `NATIVE_VISION_URL` | `https://open.bigmodel.cn/api/paas/v4/chat/completions` | Native Zhipu endpoint for vision |
| `IP_WHITELIST` | `` | Comma-separated IPs/CIDRs |
| `IP_BLACKLIST` | `` | Comma-separated IPs/CIDRs |
| `DEFAULT_MODEL` | `glm-5` | Default model for chat requests |
| `DEFAULT_PROVIDER` | `glm` | Default provider for chat requests |
| `DEFAULT_TEMPERATURE` | `0.7` | Default temperature |
| `DEFAULT_MAX_TOKENS` | `1024` | Default max_tokens |
| `ANTHROPIC_API_VERSION` | `2023-06-01` | Anthropic API version header |
| `GEMINI_CODEASSIST_ENDPOINT` | `https://cloudcode-pa.googleapis.com/v1internal` | Gemini CodeAssist endpoint |
| `GEMINI_API_ENDPOINT` | `https://generativelanguage.googleapis.com` | Gemini API endpoint |
| `GEMINI_DEFAULT_MODEL` | `models/gemini-2.5-flash-preview-05-20` | Default Gemini model |
| `MODEL_PRIORITY` | `glm-5.1:100,glm-5-turbo:90,...` | Model priority for adaptive limiter |
| `GLM_MODE` | `true` | Enable Z.AI features (key pool, model limits, vision) |
| `ZAI_OPENAI_URL` | `https://api.z.ai/api/paas/v4/chat/completions` | Z.AI OpenAI-compatible endpoint |
| `ZAI_OPENAI_MODELS` | `` | Models requiring OpenAI format (comma-separated) |
| `CLI_SIDECAR_ENABLED` | `true` | Enable Node.js sidecar for billing injection |
| `CLI_SIDECAR_URL` | `http://127.0.0.1:8081` | Sidecar URL |
| `SIDECAR_PORT` | `8081` | Sidecar listen port |
| `DASHBOARD_URL` | `https://ai.klxhub.com` | Dashboard URL for OAuth callbacks |
| `OAUTH_CALLBACK_BASE` | `https://ai.klxhub.com` | OAuth callback base URL |
| `GEMINI_OAUTH_CLIENT_ID` | `` | Google OAuth client ID |
| `GEMINI_OAUTH_CLIENT_SECRET` | `` | Google OAuth client secret |
| `DASHBOARD_PASSWORD` | `` | Dashboard auth password (empty = no auth) |

### Dragonfly

| Variable | Default | Description |
|----------|---------|-------------|
| `DRAGONFLY_MAX_MEMORY` | `4gb` | Dragonfly memory limit |

### Rate Limiter

| Variable | Default | Description |
|----------|---------|-------------|
| `RATE_LIMITER_CAPACITY` | `1000` | Token bucket capacity |
| `RATE_LIMITER_REFILL_RATE` | `100` | Token refill rate (token/second) |

### AI Worker

| Variable | Default | Description | Max/Limit |
|----------|---------|-------------|-----------|
| `WORKER_CONCURRENCY` | `50` | Concurrent worker coroutines | Depends on provider rate limit and memory |
| `MAX_RETRIES` | `3` | Retry count on provider failure | Should not exceed 5 |
| `BASE_BACKOFF` | `1.0` | Exponential retry backoff base (seconds) | 0.5-5.0 |
| `RESULT_TTL` | `600` | Result storage time (seconds) | 60-3600 |
| `UPSTREAM_MODEL_LIMITS` | `glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3,glm-4.5:10` | Per-model concurrent limits | Should sum to UPSTREAM_GLOBAL_LIMIT |
| `UPSTREAM_DEFAULT_LIMIT` | `1` | Default limit for unlisted models | - |
| `UPSTREAM_GLOBAL_LIMIT` | `9` | Max concurrent requests (must be > 0) | - |
| `PROVIDER_RPM_LIMITS` | `glm:5` | Per-provider RPM limit to prevent 429 | Depends on key count |

#### WORKER_CONCURRENCY Recommendations

- **GLM (Z.ai)**: 20-50 (depends on your tier)
- **OpenAI**: 20-50 (if high rate limit)
- **Anthropic**: 10-30
- **Multi-provider**: Set based on fastest provider, fallback handles the rest

> **Recommended max**: 50 workers (default) — sufficient for normal usage
> **Absolute max**: ~200 (increase ai-worker container memory to 2G+)
> **Warning**: Setting too high beyond provider rate limit → 429 errors and heavy retries

### AI Provider Keys

Add API keys separated by comma for key rotation:

```bash
GLM_API_KEYS=key1,key2,key3
GLM_ENDPOINT=https://api.z.ai/api/anthropic
```

| Variable | Description |
|----------|-------------|
| `GLM_API_KEYS` | GLM/Z.ai API keys (comma-separated) |
| `GLM_ENDPOINT` | GLM API endpoint |
| `OPENAI_API_KEYS` | OpenAI API keys |
| `ANTHROPIC_API_KEYS` | Anthropic API keys |
| `GEMINI_API_KEYS` | Google Gemini API keys |
| `OPENROUTER_API_KEYS` | OpenRouter API keys |

> **Important**: If not using a provider, remove that line from `.env` or leave empty. System will skip providers without keys.

### Observability

| Variable | Default | Description |
|----------|---------|-------------|
| `GRAFANA_ADMIN_PASSWORD` | (required) | Grafana admin password |
| `PROXY_PORT` | `9000` | Caddy external proxy port |
| `EXTERNAL_HOST` | `localhost` | External hostname for proxy |
| `PROXY_SCHEME` | `http` | Proxy URL scheme |

### PasteGuard (Privacy Pipeline)

| Variable | Default | Description |
|----------|---------|-------------|
| `PASTEGUARD_ENABLED` | `true` | Enable/disable PasteGuard system-wide |
| `PASTEGUARD_SECRETS_ENABLED` | `true` | Enable/disable secret detection |
| `PASTEGUARD_SECRET_ENTITIES` | (8 types) | Secret entity types to detect (comma-separated) |
| `PASTEGUARD_PII_ENABLED` | `true` | Enable/disable PII detection |
| `PASTEGUARD_PII_ENTITIES` | `EMAIL_ADDRESS,PHONE_NUMBER` | PII entity types (default = email + phone only) |
| `PASTEGUARD_MAX_SCAN_CHARS` | `200000` | Max characters to scan (200K) |

> **Note**: `PASTEGUARD_PRESIDIO_URL`, `PASTEGUARD_PII_SCORE_THRESHOLD`, and `PASTEGUARD_PII_LANGUAGE` have been removed. PII detection now uses built-in `RegexDetector` (<1ms per call) instead of Presidio HTTP container (7-14s per call).

## 6. Quick Start

```bash
# 1. Setup
cp .env.example .env && vim .env   # Set GRAFANA_ADMIN_PASSWORD, add API keys

# 2. Run
docker-compose up -d --build

# 3. Use with Claude Code
# Add to ~/.claude/settings.json:
# "ANTHROPIC_BASE_URL": "http://localhost:9000"
# "ANTHROPIC_AUTH_TOKEN": "your-key"

# 4. Monitor
# Gateway Health: http://localhost:9000/health
# Admin Dashboard: http://localhost:9000/
# Prometheus: http://localhost:9000/metrics
```

### Build Dashboard UI

Dashboard is React + Vite + TailwindCSS in `ui/` directory:

```bash
cd ui
bun install    # First time only
bun run dev    # Dev server (port 5173, proxy to :8080)
bun run build  # Build production -> api-gateway/static/
```

> **Important**: After `bun run build`, rebuild Go binary to embed new static files.

### Build Gateway (Go)

```bash
cd api-gateway

# Build binary
go build -o api-gateway .

# **Every time after build**: delete binary artifact
rm -f api-gateway

# Run tests with race detection
go test ./... -count=1 -race

# Combined: build UI -> build Go -> cleanup binary
cd ../ui && bun run build && cd ../api-gateway && go build -o api-gateway . && rm -f api-gateway
```

## 7. Port Summary

| Port | Service | External | Protocol |
|------|---------|----------|----------|
| **9000** | Caddy Reverse Proxy | Yes | HTTP |
| 8080 | API Gateway | No | HTTP |
| 8080 | Rate Limiter | No | HTTP |
| 8081 | Node.js Sidecar | No | HTTP |
| 5173 | Dashboard UI (dev) | No | HTTP |
| 6379 | Dragonfly | No | Redis |
| 9090 | AI Worker / Prometheus | No | HTTP |
| 9091 | AI Worker (internal metrics) | No | HTTP |
| 4317 | OTel Collector (gRPC) | No | gRPC |
| 4318 | OTel Collector (HTTP) | No | HTTP |
| 8889 | OTel Collector (Prom) | No | HTTP |

---

*Back to [Manual](../MANUAL.md)*
