<div align="center">
  <img src="https://img.shields.io/badge/⚡_Agent_Rate_Limit-Gateway-6366f1?style=for-the-badge&labelColor=1e1b4b" alt="Agent Rate Limit Gateway">
  <br/>
  <br/>
  <h3>Enterprise-Grade Multi-Provider AI Gateway</h3>
  <p><strong>Transparent proxy · 18 providers · 5-layer rate limiting · Token optimization · Privacy masking</strong></p>
  <br/>
  <img src="https://img.shields.io/badge/Go-1.23-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/React-19-61DAFB?style=flat-square&logo=react&logoColor=black" alt="React">
  <img src="https://img.shields.io/badge/Python-3.12-3776AB?style=flat-square&logo=python&logoColor=white" alt="Python">
  <img src="https://img.shields.io/badge/Java-21-ED8B00?style=flat-square&logo=openjdk&logoColor=white" alt="Java">
  <img src="https://img.shields.io/badge/Dragonfly-Redis_Compat-00D4AA?style=flat-square" alt="Dragonfly">
  <img src="https://img.shields.io/badge/Docker-Compose-2496ED?style=flat-square&logo=docker&logoColor=white" alt="Docker">
  <br/>
  <img src="https://img.shields.io/badge/Providers-18-blue?style=flat-square" alt="Providers">
  <img src="https://img.shields.io/badge/API_Endpoints-100+-green?style=flat-square" alt="API">
  <img src="https://img.shields.io/badge/Prometheus-36+_Metrics-ff6f00?style=flat-square&logo=prometheus&logoColor=white" alt="Prometheus">
  <img src="https://img.shields.io/badge/Go_Codebase-577K_LoC-00ADD8?style=flat-square" alt="LoC">
  <br/>
  <br/>
  <a href="#quick-start"><strong>Quick Start</strong></a> ·
  <a href="#features"><strong>Features</strong></a> ·
  <a href="#architecture"><strong>Architecture</strong></a> ·
  <a href="#documentation"><strong>Docs</strong></a>
</div>

---

## What is this?

A **self-hosted AI gateway** that sits between your AI clients (Claude Code, CI/CD, agent frameworks) and 18 AI providers. One endpoint, automatic fallback, built-in rate limiting, token optimization, and privacy masking.

```
Your Agents                    Agent Rate Limit                   AI Providers
┌───────────┐                 ┌─────────────────┐                ┌───────────┐
│Claude Code│ ─── Anthropic ─▶│                 │── Anthropic ──▶│ Z.AI      │
│CI/CD      │ ─── OpenAI ────▶│  API Gateway    │── OpenAI ─────▶│ OpenAI    │
│Batch Jobs │ ─── Async ─────▶│  (Go / chi)     │── Gemini ─────▶│ Claude    │
│Web Chat   │ ─── REST ──────▶│                 │── Bearer ─────▶│ Gemini    │
└───────────┘                 │  Optimizer      │                │ OpenRouter│
                              │  PasteGuard     │                │ Qwen      │
                              │  Vision Router  │                │ DeepSeek  │
                              └─────────────────┘                │ + 10 more │
                                                                 └───────────┘
```

<br/>

## Features

<table>
<tr>
<td width="50%">

### Transparent Proxy
Drop-in for Claude Code -- zero config changes needed.
- Full SSE streaming passthrough
- Tool loop compatible (Write, Bash, Read, Edit...)
- Multi-turn conversations preserved
- TTFB tracking on every request

</td>
<td width="50%">

### 5-Layer Rate Limiting
Keep your API usage under control.
- Global RPS limiter (token bucket)
- Per-agent rate limiter (5 RPM default)
- Adaptive concurrency (gradient + EWMA RTT)
- Key pool RPM with 429 auto-cooldown
- Daily quota enforcement per account

</td>
</tr>
<tr>
<td width="50%">

### 18 Providers, Zero Lock-in
Route to any provider, fallback automatically.
- OpenRouter, Qwen, DeepSeek, Kimi, Ollama
- Custom providers via runtime API
- Round-robin token rotation
- Utilization-aware account selection

</td>
<td width="50%">

### 7-Stage Token Optimizer
Reduce upstream token usage by up to 60%.
- Chunker, Packer, Summarizer
- Delta encoding, Sketch dedup
- Intent filter, Caveman compression
- Message body whitespace + sentence dedup
- Budget-aware: more aggressive under pressure

</td>
</tr>
<tr>
<td width="50%">

### PasteGuard Privacy
Mask PII and secrets before they leave your network.
- 12 secret patterns (API keys, JWTs, AWS creds)
- 8 PII entities (email, phone, CC, SSN, IBAN)
- Real-time streaming unmask on response
- Pure Go regex -- zero external dependencies

</td>
<td width="50%">

### Vision Auto-Routing
Send images, gateway handles the rest.
- Auto-detect image content in requests
- Score-based model selection (`glm-4.6v` vs `flashx`)
- SSE streaming format conversion (Zhipu -> Anthropic)
- Base64 and URL image support

</td>
</tr>
<tr>
<td width="50%">

Use OpenAI-compatible endpoints with Claude Code.
- Anthropic tools -> OpenAI function calling
- Streaming SSE conversion (`tool_calls` -> `tool_use`)
- 3-layer context overflow defense
- Auto-continuation (up to 3x on `length`)

</td>
<td width="50%">

### Observability Built-in
Production-ready monitoring from day one.
- 36+ Prometheus metrics
- Pre-built Grafana dashboards
- Real-time admin dashboard (React)
- OpenTelemetry distributed tracing
- Anomaly detection (Z-score ring buffer)

</td>
</tr>
</table>

<br/>

## Architecture

```
┌───────────────────────────────────────────────────────────────────────────────┐
│                              Docker Network                                   │
│                                                                               │
│   ┌─────────┐     ┌──────────────────────────────────────────────────┐       │
│   │         │     │               arl-gateway (Go)                   │       │
│   │  Caddy  │     │  ┌──────────┐ ┌──────────┐ ┌────────────────┐  │       │
│   │ :9000   │────▶│  │  Chi     │ │ Optimizer│ │  PasteGuard    │  │       │
│   │  (TLS)  │     │  │  Router  │ │ Pipeline │ │  PII + Secrets │  │       │
│   └─────────┘     │  └────┬─────┘ └────┬─────┘ └───────┬────────┘  │       │
│                   │       │            │                │            │       │
│                   │  ┌────▼─────────────▼────────────────▼────────┐  │       │
│                   │  │           Format-Aware Proxy               │  │       │
│                   │  │  Anthropic │ OpenAI │ Gemini │ ZAIWeb     │  │       │
│                   │  └────┬──────────┬──────────┬──────────┬─────┘  │       │
│                   └───────┼──────────┼──────────┼──────────┼────────┘       │
│                           │          │          │          │                 │
│                    ┌──────▼───┐ ┌────▼────┐ ┌──▼───┐ ┌───▼────┐           │
│                    │ Z.AI     │ │ OpenAI  │ │Claude│ │ Gemini │            │
│                    │ GLM-5.1  │ │ GPT-4o  │ │OAuth │ │ 2.5    │            │
│                    │ Vision   │ │ o3/o4   │ │      │ │        │            │
│                    └──────────┘ └─────────┘ └──────┘ └────────┘            │
│                                                                               │
│   ┌────────────┐  ┌────────────┐  ┌──────────┐  ┌───────────┐             │
│   │ Dragonfly  │  │ Prometheus │  │ Grafana  │  │ OTel      │              │
│   │ (Redis)    │  │ :9090      │  │ :3000    │  │ Collector │              │
│   └────────────┘  └────────────┘  └──────────┘  └───────────┘             │
└───────────────────────────────────────────────────────────────────────────────┘
```

<br/>

## Quick Start

> **Prerequisites**: Docker Desktop (or Docker Engine + Compose v2), 4GB RAM, 5GB disk

```bash
# 1. Clone
git clone <repo-url> && cd agent-rate-limit

# 2. Configure
cp .env.example .env
# Edit .env -- set at minimum one provider key:
#   GLM_API_KEYS=your-zai-key
#   or ANTHROPIC_API_KEYS=sk-ant-xxx
#   or OPENAI_API_KEYS=sk-xxx

# 3. Launch
docker-compose up -d --build

# 4. Verify
docker-compose ps    # all services should show Up (healthy)
```

### Connect Claude Code

```json
// ~/.claude/settings.json
{
  "apiKeyHelper": "echo $ANTHROPIC_API_KEY",
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:9000",
    "ANTHROPIC_API_KEY": "arl_your-profile-token"
  }
}
```

That's it. Claude Code works as-is -- tools, streaming, multi-turn conversations all pass through transparently.

### Access Dashboards

| Dashboard | URL |
|-----------|-----|
| Admin UI | `http://localhost:9000/` |
| Grafana | `http://localhost:9000/grafana` |
| Health | `http://localhost:9000/health` |
| Metrics | `http://localhost:9000/metrics` |

<br/>

## Supported Providers

| Provider | Format | Auth | Models |
|----------|--------|------|--------|
| Z.AI | Anthropic | API Key | GLM-5.1, GLM-5, GLM-4.6v (vision) |
| Claude OAuth | Anthropic | Bearer | Claude Opus 4.7, Sonnet 4.6, Haiku 4.5 |
| Anthropic | Anthropic | API Key | Claude full model family |
| OpenAI | OpenAI | Bearer | GPT-4o, o1, o3, o4 |
| Gemini | Gemini | API Key | Gemini 2.5 Pro, Flash |
| Gemini OAuth | Gemini | Bearer | Code Assist |
| OpenRouter | OpenAI | Bearer | Multi-vendor (anthropic/, openai/, meta/, google/) |
| DeepSeek | OpenAI | Bearer | DeepSeek V3/R1 |
| Qwen | OpenAI | Bearer | Qwen models |
| Kimi | OpenAI | Bearer | Moonshot models |
| HuggingFace | OpenAI | Bearer | HF Inference API |
| Ollama | OpenAI | Bearer | Local models |
| Copilot | OpenAI | Bearer | GitHub Copilot |
| Cursor | OpenAI | Bearer | Cursor AI |
| CodeBuddy | OpenAI | Bearer | CodeBuddy |
| Kilo | OpenAI | Bearer | Kilo AI |
| AGY | Anthropic | API Key | AGY models |
| Custom | Any | Any | Register via API at runtime |

<br/>

## Scaling

```
Throughput = Keys x RPM per key

1 GLM key              -->   5 RPM
3 GLM keys             -->  15 RPM
3 GLM + 2 OpenAI keys  --> 135 RPM
All providers           --> 200+ RPM
```

| Scale | Mode | Config |
|-------|------|--------|
| Solo developer | Sync | Single key |
| 2-5 developers | Sync | 1 key per person |
| Team + CI/CD | Sync + Async | Dev sync, CI async |
| Agent framework (5-50) | Async | 50 workers, multi-key |
| Heavy batch (100+) | Async | Multi-key + multi-provider |

<br/>

## Tech Stack

<p align="center">
  <img src="https://img.shields.io/badge/Go_1.23-API_Gateway-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Java_21-Rate_Limiter-ED8B00?style=for-the-badge&logo=openjdk&logoColor=white" alt="Java">
  <img src="https://img.shields.io/badge/Python_3.12-Job_Worker-3776AB?style=for-the-badge&logo=python&logoColor=white" alt="Python">
  <img src="https://img.shields.io/badge/React_19-Dashboard-61DAFB?style=for-the-badge&logo=react&logoColor=black" alt="React">
</p>
<p align="center">
  <img src="https://img.shields.io/badge/DragonflyDB-Cache_&_Queue-00D4AA?style=for-the-badge" alt="Dragonfly">
  <img src="https://img.shields.io/badge/Caddy-Reverse_Proxy-1F8C3A?style=for-the-badge&logo=caddy&logoColor=white" alt="Caddy">
  <img src="https://img.shields.io/badge/Prometheus-Metrics-ff6f00?style=for-the-badge&logo=prometheus&logoColor=white" alt="Prometheus">
  <img src="https://img.shields.io/badge/Grafana-Dashboards-F46800?style=for-the-badge&logo=grafana&logoColor=white" alt="Grafana">
  <img src="https://img.shields.io/badge/OpenTelemetry-Tracing-000000?style=for-the-badge&logo=opentelemetry&logoColor=white" alt="OTel">
</p>

<br/>

## Documentation

### User Guides

| # | Document | Description |
|---|----------|-------------|
| 01 | [Getting Started](docs/01-getting-started.md) | Architecture, install, env vars, ports |
| 02 | [Claude Code Guide](docs/02-claude-code.md) | Setup, tool loop, compatibility, rate limits |
| 03 | [API Reference](docs/03-api-reference.md) | All 100+ API endpoints |
| 04 | [Providers](docs/04-providers.md) | 18 providers, OAuth flows, token management |
| 05 | [Routing](docs/05-routing.md) | Profile-based routing, model mapping |
| 06 | [Dashboards](docs/06-dashboards.md) | Grafana, admin UI, pricing |
| 07 | [Observability](docs/07-observability.md) | 36+ Prometheus metrics reference |
| 08 | [Docker Ops](docs/08-docker-ops.md) | Build, deploy, service management |
| 09 | [Features](docs/09-features.md) | Vision, multi-agent, message optimization |
| 11 | [Privacy & Security](docs/11-privacy-security.md) | PasteGuard, streaming unmask, GLM isolation |
| 12 | [Z.AI Vision](docs/12-zai.md) | Image format routing fix |
| 13 | [Troubleshooting](docs/13-troubleshooting.md) | Common issues, reset, port reference |

### Implementation Specs

> Detailed enough to re-implement the entire system from scratch.

| # | Spec | Description |
|---|------|-------------|
| S1 | [Proxy Layer](docs/spec/01-proxy-layer.md) | All proxy types, SSE state machines, format conversion, retry matrix |
| S2 | [Handler & Routing](docs/spec/02-handler-routing.md) | Request lifecycle, auth flow, rate limiting, provider fallback |
| S3 | [Optimizer & Privacy](docs/spec/03-optimizer-privacy.md) | 13 components, PII patterns, streaming unmask algorithms |
| S4 | [Infra & Config](docs/spec/04-infra-config.md) | 30-step startup, 50+ env vars, 14 Docker services |
| S5 | [Data Models & API](docs/spec/05-data-models-api.md) | All structs, API contracts, 22 Redis key patterns |

<br/>

## Requirements

| Requirement | Minimum | Recommended |
|-------------|---------|-------------|
| Docker | Engine + Compose v2 | Docker Desktop |
| RAM | 4 GB | 8 GB+ |
| Disk | 5 GB | 10 GB+ |
| Network | 1 provider API access | Multi-provider for fallback |

<br/>

## License

Private project. All rights reserved.

---

<div align="center">
  <sub>Built with Go, Coffee, and Too Many 429s.</sub>
</div>
