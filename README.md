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

- **Transparent proxy** for Claude Code -- zero modification to requests/responses, SSE streaming passthrough
- **Async queue** for batch agents -- burst 100+ jobs, worker paces them automatically
- **Distributed rate limiting** -- token bucket algorithm with per-key and global limits
- **Per-model concurrency** -- 9 slots across 5 models with automatic fallback
- **Multi-provider fallback** -- GLM -> OpenAI -> Anthropic -> Gemini -> OpenRouter
- **Key cooldown** -- keys temporarily disabled on 429, auto-recover after 60s (no restart needed)

## Architecture

```
Client (Claude Code / Agent / CI)
  |
  POST /v1/messages (sync) or POST /v1/chat/completions (async)
  |
  v
arl-gateway (:8080) -- Go, chi router
  |-- Rate Limit --> arl-rate-limiter (Java/Spring) --> arl-dragonfly (Redis)
  |-- Sync:  Transparent Proxy --> Upstream Provider
  |-- Async: LPUSH to Queue --> arl-worker (Python, 50 coroutines)
  |     |-- Per-Model Semaphores (9 slots)
  |     |-- RPM Limiter (glm:5)
  |     |-- Key Cooldown (60s, auto-recover)
  |     |-- Provider Fallback Chain
  |     +-- Result Cache (Dragonfly, TTL 600s)
  |
  +-- Observability: OpenTelemetry --> Prometheus --> Grafana
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
Claude Code --> Gateway --> Rate Limit Check --> Proxy --> Z.ai
                                                         |
Claude Code <-- SSE chunks <------------------------------
```

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

9 concurrent slots distributed across models. When a model is full, requests automatically fall back to models with available slots:

```
Model Slots:  glm-5.1(1)  glm-5-turbo(1)  glm-5(2)  glm-4.7(2)  glm-4.6(3)
Fallback:     glm-5.1 --> glm-5-turbo --> glm-5 --> glm-4.7 --> glm-4.6
```

Configure in `.env`:

```bash
UPSTREAM_MODEL_LIMITS=glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3
UPSTREAM_DEFAULT_LIMIT=1
UPSTREAM_GLOBAL_LIMIT=9
```

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
