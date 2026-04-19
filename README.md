<h1 align="center">Agent Rate Limit</h1>

<p align="center">
  <strong>Multi-Agent AI Gateway with Distributed Rate Limiting</strong><br>
  Proxy + Queue for Claude Code, Batch Agents, and Multi-Provider Fallback
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.23-00ADD8?logo=go" alt="Go">
  <img src="https://img.shields.io/badge/Python-3.12-3776AB?logo=python" alt="Python">
  <img src="https://img.shields.io/badge/Java-21-ED8B00?logo=openjdk" alt="Java">
  <img src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker" alt="Docker">
</p>

---

## What It Does

A self-hosted AI gateway that sits between your agents (Claude Code, CI/CD pipelines, agent frameworks) and AI providers (Z.ai, OpenAI, Anthropic, Gemini, OpenRouter).

- **Transparent proxy** for Claude Code -- zero modification to requests/responses, SSE streaming passthrough, TTFB tracking
- **Async queue** for batch agents -- burst 100+ jobs, worker paces them automatically
- **Distributed rate limiting** -- token bucket algorithm with per-key and global limits
- **Per-model concurrency** -- 19 slots across 6 models with series-based routing and round-robin fallback
- **Multi-provider fallback** -- GLM -> OpenAI -> Anthropic -> Gemini -> OpenRouter
- **Key cooldown** -- keys temporarily disabled on 429, auto-recover after 60s (no restart needed)
- **Security middleware** -- SecurityHeaders, CorrelationID, RealIP, IPFilter
- **Anomaly detection** -- Z-score ring buffer for rate anomaly detection
- **21 Prometheus metrics** -- latency, tokens, cost, TTFB, adaptive limits, runtime stats
- **Vision auto-routing** -- detects image content, auto-selects model by size/count, routes to native Zhipu endpoint with Anthropic SSE streaming conversion
- **Content filtering** -- strips unsupported block types (server_tool_use) before forwarding

## Architecture

```
Client (Claude Code / Agent / CI)
  |
  POST /v1/messages (sync) or POST /v1/chat/completions (async)
  |
  v
arl-gateway (:8080) -- Go, chi router
  |-- SecurityHeaders, CorrelationID, RealIP middleware
  |-- Rate Limit --> arl-rate-limiter (Java/Spring) --> arl-dragonfly (Redis)
  |
  |-- Text Request:
  |     Sync:  Transparent Proxy --> Upstream Provider (api.z.ai/api/anthropic)
  |     Async: LPUSH to Queue --> arl-worker (Python, 50 coroutines)
  |
  |-- Image Request (auto-detected):
  |     Auto-select: glm-4.6v (default) or glm-4.6v-flashx (large/multi-image)
  |     Format Convert (Anthropic -> OpenAI/Zhipu)
  |     --> Native Zhipu Vision (open.bigmodel.cn/api/paas/v4/chat/completions)
  |     <-- SSE Convert (Zhipu SSE -> Anthropic SSE) if stream=true
  |     <-- JSON Convert (Zhipu -> Anthropic) if stream=false
  |
  |-- Content Filter: strip server_tool_use, convert Anthropic image -> GLM image_url
  |
  |-- Per-Model Semaphores (19 slots, global cap 9, series-based routing)
  |-- RPM Limiter (glm:5)
  |-- Key Cooldown (60s, auto-recover)
  |-- Provider Fallback Chain
  |-- Result Cache (Dragonfly, TTL 600s)
  |
  +-- Observability: 21 Prometheus metrics, TTFB, cost tracking, anomaly detection
  +-- Tracing: OpenTelemetry --> Prometheus --> Grafana
```

## Quick Start

```bash
# 1. Clone
git clone <repo-url> && cd agent-rate-limit

# 2. Configure
cp .env.example .env
# Edit .env -- at minimum set GLM_API_KEYS

# 3. Launch
docker-compose up -d --build

# 4. Verify
docker-compose ps
```

All services should show `Up (healthy)`.

## Using with Claude Code

Edit `~/.claude/settings.json`:

```json
{
  "ANTHROPIC_BASE_URL": "http://localhost:8080",
  "ANTHROPIC_AUTH_TOKEN": "your-glm-api-key"
}
```

That's it. Claude Code works exactly the same -- tools, streaming, multi-turn conversations all pass through transparently. The gateway adds rate limiting + key management on top.

## Two Modes

### Sync Mode (`POST /v1/messages`)
For **Claude Code** and interactive use. Real-time SSE streaming, tool loop compatible.

```
Claude Code --> Gateway --> Rate Limit Check --> Content Filter
                                                         |
                    +-- Text Request ---------------------+--> Proxy --> Z.ai
                    |                                                        |
                    +-- Image Request (auto-detected) ----+--> Format Convert
                                                             --> Native Zhipu Vision
                                                             <-- Format Convert Response
Claude Code <-- SSE chunks <-----------------------------------
```

### Vision Auto-Routing

When a request contains image content, the gateway automatically routes to the native Zhipu vision endpoint instead of the Anthropic-compatible endpoint:

```
Request with image content
  |
  v
Gateway detects image blocks
  |-- strip server_tool_use blocks
  |-- convert Anthropic image format -> GLM image_url format
  |-- analyze image payload: total base64 size + image count
  |-- auto-select vision model:
  |     score = totalKB + (imageCount * 300)
  |     score <= 2000 and < 3 images -> glm-4.6v (10 slots, best quality)
  |     score > 2000 or >= 3 images -> glm-4.6v-flashx (3 slots, fastest)
  |
  v
anthropicToZhipu():
  |-- messages: Anthropic format -> OpenAI format
  |-- system: string/array -> string
  |-- image blocks: source{type,media_type,data} -> image_url{url}
  |-- tool_result blocks -> text content
  |
  v
POST to Native Zhipu Vision (open.bigmodel.cn/api/paas/v4/chat/completions)
  |
  v
Response conversion:
  |-- Zhipu SSE response (stream=true):
  |     Zhipu chunk (delta.content) -> Anthropic content_block_delta (text_delta)
  |     Emit: message_start -> content_block_start -> deltas -> content_block_stop -> message_stop
  |
  |-- Zhipu JSON response (stream=false):
  |     choices -> content array (text)
  |     usage -> token tracking
  |
  v
Response to client (Anthropic format)
```

Supported vision models: `glm-4.6v`, `glm-4.5v`, `glm-4.6v-flash`, `glm-4.6v-flashx`

### Async Mode (`POST /v1/chat/completions`)
For **batch agents**, CI/CD, scheduled tasks. Queue + worker handles pacing.

```
Agent --> Gateway --> Queue (Dragonfly)
                        |
                    Worker (BRPOP)
                        |-- RPM Limiter
                        |-- Model Semaphore
                        |-- Provider Fallback
                        +-- Result Cache
                             |
Agent <-- Poll GET /v1/result/{id} <--+
```

## Multi-Provider Fallback

Add API keys in `.env` to enable providers:

```bash
GLM_API_KEYS=key1,key2,key3        # Primary (Z.ai)
OPENAI_API_KEYS=sk-xxx             # Optional fallback
ANTHROPIC_API_KEYS=sk-ant-xxx     # Optional fallback
GEMINI_API_KEYS=AIza-xxx          # Optional fallback
OPENROUTER_API_KEYS=or-xxx        # Optional fallback
```

Fallback order: `glm -> openai -> anthropic -> gemini -> openrouter`

Only providers with configured keys are tried. If GLM returns 429, the system automatically tries OpenAI, then Anthropic, etc.

```bash
# Set per-provider RPM limits to match your account tier
PROVIDER_RPM_LIMITS=glm:5,openai:60,anthropic:50
```

## Per-Model Concurrency

19 concurrent slots distributed across 6 models. When a model is full, requests first round-robin within the same series, then spill to lower series under latency pressure:

```
Series 5 (preferred):  glm-5.1(1) → glm-5-turbo(1) → glm-5(2)  = 4 slots
Series 4 (fallback):   glm-4.7(2) → glm-4.6(3) → glm-4.5(10)   = 15 slots
Vision:                glm-4.6v(10) → glm-4.5v(10) → glm-4.6v-flashx(3) → glm-4.6v-flash(1)

Global cap: 9 concurrent across all models
```

Signal-based waiting (sync.Cond) replaces spin-wait for slot availability. RTT EWMA per model drives latency pressure detection for series spillover.

### Selection Examples

**Low load (3 concurrent):**
| Req | Requested | Selected | Why |
|-----|-----------|----------|-----|
| 1 | glm-5 | glm-5 | Slot available |
| 2 | glm-5 | glm-5 | 2nd slot available |
| 3 | glm-5 | glm-5.1 | glm-5 full, fallback to next 5.x |

**High load (15 concurrent):**
| Req | Requested | Selected | Why |
|-----|-----------|----------|-----|
| 1-2 | glm-5 | glm-5 | 2 slots filled |
| 3 | glm-5 | glm-5.1 | Fallback (5.x preferred) |
| 4 | glm-5 | glm-5-turbo | Fallback (5.x preferred) |
| 5-6 | glm-5 | glm-4.7 | All 5.x full, start 4.x |
| 7-9 | glm-5 | glm-4.6 | 4.7 full |
| 10-14 | glm-5 | glm-4.5 | 4.6 full, last resort |
| 15 | glm-5 | (waits) | Global cap reached |

```bash
UPSTREAM_MODEL_LIMITS=glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3,glm-4.5:10
UPSTREAM_DEFAULT_LIMIT=1
UPSTREAM_GLOBAL_LIMIT=9
```

## Vision Model Auto-Select

| Payload | Images | Score | Model | Reason |
|---------|--------|-------|-------|--------|
| < 500KB | 1 | < 800 | glm-4.6v | Best quality |
| 500KB - 2MB | 1 | 800-2300 | glm-4.6v | Still manageable |
| > 2MB | 1 | > 2300 | glm-4.6v-flashx | Large, use fastest |
| any | >= 3 | any | glm-4.6v-flashx | Multiple images, use fastest |

Score = `totalKB + (imageCount * 300)`. Threshold: 2000 with < 3 images selects glm-4.6v, otherwise glm-4.6v-flashx.

## Scaling Throughput

```
Throughput = Keys x RPM per key

1 GLM key              -> 5 RPM
3 GLM keys             -> 15 RPM
3 GLM + 2 OpenAI keys  -> 15 + 120 = 135 RPM
```

| Scale | Mode | Config |
|-------|------|--------|
| 1 developer | Sync | 1 key |
| 2-5 developers | Sync | 1 key per person |
| Team + CI/CD | Sync + Async | Dev sync, CI async |
| Agent framework (5-50) | Async | 50 workers, multi-key |
| Heavy batch (100+) | Async | Multi-key + multi-provider |

## Key Cooldown

When a key hits a 429 rate limit, it enters a **60-second cooldown** instead of being permanently removed. After cooldown expires, the key automatically becomes available again -- no worker restart needed.

```
Key hits 429 --> Cooldown 60s --> Auto-recover --> Available again
```

With multiple keys, the system rotates to the next available key while the cooldown key recovers.

## Observability

| Service | URL | Credentials |
|---------|-----|-------------|
| **Grafana** | http://localhost:3000 | admin / (see GRAFANA_ADMIN_PASSWORD) |
| **Rate Limiter Dashboard** | http://localhost:8081 | No login |
| **Gateway Health** | http://localhost:8080/health | -- |
| **Prometheus Metrics** | http://localhost:8080/metrics | -- |

Pre-built Grafana dashboards: System Overview, API Gateway, AI Worker, Cost Calculator.

## Services

| Service | Tech | Port | Purpose |
|---------|------|------|---------|
| arl-gateway | Go (chi) | **8080** (external) | HTTP gateway, rate limit, proxy/queue |
| arl-rate-limiter | Java 21 / Spring Boot | 8080 (internal) | Token bucket rate limiting |
| arl-dragonfly | DragonflyDB | 6379 (internal) | Cache, queue, state store |
| arl-worker | Python 3.12 (asyncio) | 9090 (internal) | Job processing, provider calls |
| arl-rl-dashboard | React + Vite | **8081** (external) | Rate limiter management UI |
| arl-prometheus | Prometheus | 9090 (internal) | Metrics collection |
| arl-grafana | Grafana | **3000** (external) | Dashboards |
| arl-otel | OpenTelemetry | 4317/4318 (internal) | Trace pipeline |

## Test Scripts

```bash
# Multi-agent simulation (default: 5 agents x 2 turns)
bash scripts/multi-agent-test.sh [agents] [turns]

# Examples:
bash scripts/multi-agent-test.sh 5 1      # 5 agents, 1 turn each
bash scripts/multi-agent-test.sh 10 5     # 10 agents, 5 turns each
```

## Documentation

| Document | Description |
|----------|-------------|
| [MANUAL.md](MANUAL.md) | Full user manual -- setup, config, troubleshooting |
| [docs/architecture.md](docs/architecture.md) | Internal architecture, data flows, load test results |
| [CLAUDE.md](CLAUDE.md) | AI coding assistant configuration |

## Requirements

- Docker Desktop (or Docker Engine + Docker Compose)
- RAM: 4GB minimum, 8GB+ recommended
- Disk: 5GB minimum

## License

Private project.
