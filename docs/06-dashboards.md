# Dashboards

## Grafana Dashboard

### Access

```
URL: http://localhost:3000
Username: admin
Password: from GRAFANA_ADMIN_PASSWORD in .env (required)
```

Grafana is exposed via Caddy reverse proxy at `${PROXY_SCHEME}://${EXTERNAL_HOST}:${PROXY_PORT}/grafana`. No external port is mapped directly.

### Available Dashboards

Access from **Dashboards** -> **General** or use URL directly:

| Dashboard | URL | Description |
|-----------|-----|-------------|
| **System Overview** | http://localhost:3000/d/arl-overview | Full system overview -- request rate, latency, queue, jobs |
| **API Gateway Detailed** | http://localhost:3000/d/arl-gateway | Gateway metrics: request rate by path, latency percentiles |
| **AI Worker Detailed** | http://localhost:3000/d/arl-worker | Worker metrics: job rate, provider latency, memory |
| **Cost Calculator & Savings** | http://localhost:3000/d/arl-cost | AI cost calculation, rate limit savings, cost estimation |
| **Claude OAuth Billing Path** | http://localhost:3000/d/claude-oauth-billing | Billing path distribution, latency, per-profile usage, cost |

### Dashboard Details

#### System Overview (`arl-overview`)
- Architecture diagram
- Total request rate across all paths
- Latency p50/p95/p99
- Active connections, queue depth, active workers
- Jobs processed/failed/retried
- Rate limiter JVM memory

#### API Gateway Detailed (`arl-gateway`)
- Request rate by path/method/status
- Latency percentiles (p50/p90/p95/p99)
- Average latency by path
- Error rate (4xx/5xx)
- Active connections & queue depth timeline

#### AI Worker Detailed (`arl-worker`)
- Job processing rate (processed/failed/retried per second)
- Total job counts
- Queue depth over time
- Active workers gauge
- Error rate percentage gauge
- Provider latency by provider (p50/p95/p99)
- Process memory (RSS/Virtual) & CPU usage

#### Cost Calculator & Savings (`arl-cost`)
- Total requests (24h) by sync/async
- Requests/hour average
- Estimated input/output tokens
- Request volume over time
- Estimated daily cost by provider (bar chart)
- Rate limited requests (429s) -- cost savings
- Retry & failure rates
- Provider error rate
- Queue depth (backlog cost indicator)

### Pricing Table (reference for Cost Calculator)

| Provider | Model | Input (per 1M tokens) | Output (per 1M tokens) |
|----------|-------|----------------------|------------------------|
| Z.AI | glm-5.1 | $1.40 | $4.40 |
| Z.AI | glm-5-turbo | $1.20 | $4.00 |
| Z.AI | glm-5 | $1.00 | $3.20 |
| Z.AI | glm-4.7 | $0.60 | $2.20 |
| Z.AI | glm-4.7-flashx | $0.07 | $0.40 |
| Z.AI | glm-4.6v | $0.30 | $0.90 |
| Z.AI | glm-4.5v | $0.60 | $1.80 |
| Z.AI | glm-4.5v | $0.60 | $1.80 |
| OpenAI | gpt-4o | $2.50 | $10.00 |
| Anthropic | claude-opus-4-7 | $15.00 | $75.00 |
| Anthropic | claude-sonnet-4-6 | $3.00 | $15.00 |
| Anthropic | claude-haiku-4-5 | $0.80 | $4.00 |
| Gemini | gemini-2.5-pro | $1.25 | $10.00 |
| Gemini | gemini-2.5-flash | $0.15 | $0.60 |
| OpenRouter | varies | varies | varies |

### Prometheus Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /metrics` | Prometheus metrics (gateway internal) |
| `GET /api/metrics` | Alias for `/metrics` |

Both endpoints are served by the `promhttp.Handler` on the gateway's main server. The Prometheus scraper targets `arl-gateway:8080/metrics`.

### Mock Data Endpoints (for dashboard testing)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/mock/seed?category=all` | Seed mock data (categories: `all`, `optimizer`, `waste`, `budget`) |
| `POST` | `/v1/mock/loop/start` | Start continuous mock data feed (5s interval) |
| `POST` | `/v1/mock/loop/stop` | Stop continuous mock data feed |
| `GET` | `/v1/mock/status` | Check if mock loop is running |

---

## Rate Limiter Web Dashboard

### Access

```
Container: arl-rl-dashboard (internal only, no external port)
Build: ./distributed-rate-limiter/examples/web-dashboard
```

Accessed via Caddy reverse proxy. No external port mapped.

### Features

- **Real-time Monitoring** -- Active keys, requests/sec, success rates (updates every 5s)
- **Algorithm Comparison** -- Compare Token Bucket, Sliding Window, Fixed Window, Leaky Bucket
- **Traffic Simulation** -- Simulate traffic patterns: steady, bursty, spike, custom
- **API Key Management** -- Create/edit/delete API keys, IP whitelist/blacklist, usage stats
- **Configuration** -- Global & per-key rate limiting rules, pattern-based rules
- **Load Testing** -- Test with constant, ramp-up, spike, step-load patterns
- **Historical Analytics** -- Performance trends: 1h, 24h, 7d, 30d
- **Data Export** -- CSV/JSON export

---

## Gateway Dashboard UI

### Access

| Method | URL | Notes |
|--------|-----|-------|
| Embedded (Go binary) | `http://localhost:8080/` | Static files embedded via `go:embed` from `api-gateway/static/` |
| Docker Compose | `http://localhost:9000/` | Via Caddy reverse proxy |
| Standalone container | `http://arl-dashboard:5173` | Hot-reload dev container (internal) |
| Dev mode (hot reload) | `http://localhost:5173` | `cd ui && bun run dev` |

### Authentication

Dashboard auth is controlled by `DASHBOARD_PASSWORD` env var:
- **Empty**: No auth required (open access)
- **Set**: Requires `x-api-key` header or `arl_session` cookie matching the key

### Pages

| Page | Route | Features |
|------|-------|----------|
| Overview | `/` | Status, queue depth, total requests, concurrency, model utilization |
| Model Limits | `/model-limits` | Model status table: in-flight, limit, max, ceiling, RTT EWMA, requests, 429s |
| Key Pool | `/key-pool` | API key rotation pool status |
| Profiles | `/profiles` | Profile CRUD management (create needs name + target only) |
| Accounts | `/accounts` | OAuth/API key accounts, inline email editing, pause/resume, default selection |
| Metrics | `/metrics` | Recharts time-series: request rate, token usage, errors (auto-poll 5s) |
| Controls | `/controls` | Manual override model limits, active overrides table |

### Build & Deploy

```bash
# Dev (hot reload)
cd ui && bun run dev

# Build static files -> api-gateway/static/ (embedded in Go binary)
cd ui && bun run build

# Docker (standalone dev container)
docker-compose up -d --build arl-dashboard
```

### Tech Stack

- React 19 + Vite 7 + TailwindCSS v4 + shadcn/ui (Radix)
- Recharts (Prometheus metrics visualization)
- Bun runtime
- Playwright E2E tests

---

*Back to [Manual](../MANUAL.md)*
