# Dashboards

## Grafana Dashboard

### Access

```
URL: http://localhost:3000
Username: admin
Password: from GRAFANA_ADMIN_PASSWORD in .env (default: klxhunter)
```

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
| GLM/Z.ai | glm-5 | $0.50 | $1.50 |
| OpenAI | gpt-4o | $2.50 | $10.00 |
| Anthropic | claude-sonnet-4-6 | $3.00 | $15.00 |
| Gemini | gemini-2.0-flash | $0.10 | $0.40 |
| OpenRouter | varies | varies | varies |

### Metrics in System

| Metric | Source | Description |
|--------|--------|-------------|
| `api_gateway_request_latency_seconds` | Gateway | Request latency histogram (labels: method, path, status) |
| `api_gateway_active_connections` | Gateway | Active connections |
| `api_gateway_queue_depth` | Gateway | Queue depth |
| `api_gateway_error_total` | Gateway | Errors by type (labels: type -- bad_request, validation, queue_push, cache_get, upstream) |
| `api_gateway_rate_limit_hits_total` | Gateway | Rate limit hits (labels: key) |
| `api_gateway_token_input_total` | Gateway | Input tokens by model (labels: model) |
| `api_gateway_token_output_total` | Gateway | Output tokens by model (labels: model) |
| `api_gateway_upstream_429_total` | Gateway | Upstream 429 responses |
| `api_gateway_upstream_retries_total` | Gateway | Upstream retries on 429 |
| `ai_worker_jobs_processed_total` | Worker | Jobs processed (labels: provider) |
| `ai_worker_jobs_failed_total` | Worker | Jobs failed |
| `ai_worker_jobs_retried_total` | Worker | Jobs retried |
| `ai_worker_queue_depth` | Worker | Queue depth |
| `ai_worker_active` | Worker | Active workers |
| `ai_worker_provider_latency_seconds` | Worker | Provider latency histogram (labels: provider) |
| `ai_worker_provider_errors_total` | Worker | Provider errors (labels: provider) |
| `ai_worker_rate_limit_hits_total` | Worker | Rate limit hits (labels: provider) |

### Token Optimization Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_optimizer_runs_total` | Counter | `technique` | Number of optimizer runs (system + message) |
| `api_gateway_optimizer_chars_saved_total` | Counter | `technique` | Characters saved per technique |
| `api_gateway_optimizer_duration_seconds` | Histogram | `technique` | Time taken by optimizer |
| `api_gateway_optimizer_tokens_saved_total` | Counter | - | Estimated tokens saved |
| `api_gateway_cost_savings_total` | Counter | - | Cost savings from optimization (USD) |
| `api_gateway_budget_level` | Gauge | `model` | Budget utilization level (0=green, 1=yellow, 2=red) |

**Technique labels**: `semantic_dedup`, `chunker`, `delta`, `sketch_dedup`, `summarizer`, `intent_filter`, `caveman`, `message_text` (string content), `message_block_text` (text blocks), `message_block_tool_result` (tool results)

### Claude OAuth Billing Path Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_billing_path_requests_total` | Counter | `path`, `model`, `profile` | Requests by billing path (go_direct/sidecar/direct/billing_rejected) |
| `api_gateway_billing_path_latency_seconds` | Histogram | `path`, `model` | Latency by billing path |
| `http_server_requests_seconds_*` | Rate Limiter | HTTP metrics |
| `jvm_memory_*` | Rate Limiter | JVM memory |

---

## Rate Limiter Web Dashboard

### Access

```
URL: http://localhost:8081
```

> No login required -- open access. Has nginx proxy to rate-limiter API automatically.

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
| Embedded (Go binary) | `http://localhost:8080/admin` | Works after `bun run build` in `ui/` |
| Docker Compose | `http://localhost:8082` | Standalone container, nginx proxy to gateway |
| Dev mode (hot reload) | `http://localhost:5173` | `cd ui && bun run dev` |

> Login with Gateway URL + API key (stored in sessionStorage)

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

# Docker
docker-compose up -d --build arl-dashboard
```

### Tech Stack

- React 19 + Vite 7 + TailwindCSS v4 + shadcn/ui (Radix)
- Recharts (Prometheus metrics visualization)
- Bun runtime
- Playwright E2E tests (10 tests)

---

*Back to [Manual](../MANUAL.md)*
