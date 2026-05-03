# Docker Operations

## Architecture

```
Client -> arl-proxy (Caddy :9000) -> arl-gateway (Go :8080)
                                    -> arl-rate-limiter (Java :8080)
                                    -> arl-dragonfly (Redis :6379)
                                    -> arl-worker (Python :9090)
       -> arl-grafana (:3000 via proxy)
       -> arl-dashboard (Vite dev :5173 via proxy)
       -> arl-prometheus (:9090 internal)
       -> arl-otel (:4317/:4318 internal)
```

External port: **9000** (Caddy proxy). All other services are internal-only.

---

## Services

| Service | Container | Image/Build | Internal Port | External |
|---------|-----------|-------------|:-------------:|:--------:|
| API Gateway | `arl-gateway` | `./api-gateway/Dockerfile` | 8080 | via proxy |
| Rate Limiter | `arl-rate-limiter` | `./distributed-rate-limiter/Dockerfile` | 8080 | - |
| Dragonfly | `arl-dragonfly` | `ghcr.io/dragonflydb/dragonfly:v1.37.2` | 6379 | - |
| AI Worker | `arl-worker` | `./ai-worker/Dockerfile` | 9090, 9091 | - |
| Prometheus | `arl-prometheus` | `prom/prometheus:v2.54.1` | 9090 | - |
| Grafana | `arl-grafana` | `grafana/grafana:11.3.0` | 3000 | via proxy |
| OTel Collector | `arl-otel` | `otel/opentelemetry-collector-contrib:0.112.0` | 4317, 4318, 8889 | - |
| Rate Limiter Dashboard | `arl-rl-dashboard` | `./distributed-rate-limiter/examples/web-dashboard/Dockerfile` | - | - |
| Dashboard UI | `arl-dashboard` | `./ui/Dockerfile.dev` | 5173 | via proxy |
| Caddy Proxy | `arl-proxy` | `caddy:2-alpine` | 9000 | **9000** |
| Presidio PII | `arl-presidio` | `mcr.microsoft.com/presidio-analyzer:2.2.362` | 3000 | - |
| Claude Code (test) | `claude-code-meow` | `./docker/Dockerfile.claude-code` | - | - |
| Claude Code (test) | `claude-code-test` | `./docker/Dockerfile.claude-code` | - | - |

### Optional Profiles

| Profile | Services | Purpose |
|---------|----------|---------|
| `pii` | `arl-presidio` | Presidio PII analyzer (legacy, replaced by regex) |
| `test-client` | `claude-code-meow`, `claude-code-test` | Claude Code test clients |

---

## Commands

```bash
# === Full System ===
docker-compose up -d --build    # Start everything
docker-compose down             # Stop everything
docker-compose restart          # Restart everything
docker-compose ps               # Check status
docker-compose logs -f          # View logs real-time

# === Single Service ===
docker-compose up -d --build arl-gateway    # Rebuild + restart gateway
docker-compose up -d --build arl-worker     # Rebuild + restart worker
docker-compose up -d --build arl-dashboard  # Rebuild dashboard UI
docker-compose logs -f arl-gateway          # View gateway logs
docker-compose restart arl-prometheus       # Restart Prometheus

# === With Optional Profiles ===
docker-compose --profile pii up -d          # Start with Presidio
docker-compose --profile test-client up -d  # Start with Claude Code test clients

# === Info ===
docker stats                              # Resource usage
docker exec -it arl-gateway sh            # Shell in container
docker exec -it arl-dragonfly redis-cli   # Dragonfly CLI

# === Cleanup ===
docker-compose down -v         # Remove containers + volumes (reset data)
docker-compose down --rmi all  # Remove images
```

---

## API Gateway Dockerfile

Multi-stage build in `api-gateway/Dockerfile`:

1. **Build stage**: `golang:1.25-alpine` - compiles Go binary with `CGO_ENABLED=0`
2. **Runtime stage**: `alpine:3.20` - minimal image with `ca-certificates`, `curl`, `nodejs`

```
Binary: /app/api-gateway
Sidecar: /app/sidecar/ (Node.js billing header injection)
Entrypoint: /app/sidecar/entrypoint.sh
User: gateway (non-root)
Port: 8080
```

The sidecar is enabled by default (`CLI_SIDECAR_ENABLED=true`) and runs a Node.js proxy at `http://127.0.0.1:8081` for billing header injection as a fallback when Go direct billing is rejected.

---

## Resource Limits

| Service | Memory Limit | CPU Limit | Memory Reserve |
|---------|:-----------:|:---------:|:--------------:|
| arl-gateway | 1G | 2.0 | 256M |
| arl-rate-limiter | 1.5G | 2.0 | 512M |
| arl-dragonfly | 8G | 2.0 | 1G |
| arl-worker | 2G | 2.0 | 512M |
| arl-prometheus | 2G | 2.0 | 256M |
| arl-grafana | 1G | 2.0 | 256M |
| arl-otel | 512M | 1.0 | 128M |
| arl-proxy | 128M | 0.5 | 32M |

---

## Environment Variables

### Common (shared via `x-common-env`)

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ADDR` | `arl-dragonfly:6379` | Redis/Dragonfly address |
| `RATE_LIMITER_ADDR` | `http://arl-rate-limiter:8080` | Rate limiter service URL |
| `QUEUE_NAME` | `ai_jobs` | Job queue name |
| `OTLP_ENDPOINT` | `arl-otel:4317` | OpenTelemetry collector |

### Gateway-specific

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_PORT` | `:8080` | Listen address |
| `GLM_MODE` | `true` | Enable Z.AI features vs multi-provider proxy |
| `UPSTREAM_URL` | `https://api.z.ai/api/anthropic` | Default upstream |
| `UPSTREAM_API_KEYS` | - | Comma-separated API keys for rotation |
| `UPSTREAM_MODEL_LIMITS` | (see config.go) | Per-model concurrency limits |
| `UPSTREAM_GLOBAL_LIMIT` | `9` | Total concurrent upstream requests |
| `CLI_SIDECAR_ENABLED` | `true` | Enable Node.js billing sidecar |
| `CLI_SIDECAR_URL` | `http://127.0.0.1:8081` | Sidecar URL |
| `DASHBOARD_API_KEY` | - | Dashboard auth key (empty = no auth) |
| `GLOBAL_RATE_LIMIT` | `100` | Global rate limit |
| `AGENT_RATE_LIMIT` | `5` | Per-agent rate limit |

### Proxy (Caddy)

| Variable | Default | Description |
|----------|---------|-------------|
| `PROXY_PORT` | `9000` | External port |
| `PROXY_SCHEME` | `http` | URL scheme |
| `EXTERNAL_HOST` | `localhost` | External hostname |

---

## Health Checks

| Service | Check | Interval | Timeout | Retries |
|---------|-------|:--------:|:-------:|:-------:|
| arl-gateway | `curl -sf http://localhost:8080/health` | 10s | 5s | 3 |
| arl-rate-limiter | `curl -f http://localhost:8080/actuator/health` | 15s | 5s | 5 (start: 30s) |
| arl-dragonfly | `redis-cli ping` | 5s | 3s | 5 |
| arl-worker | `curl -f http://localhost:9091/metrics-internal` | 15s | 5s | 3 (start: 10s) |
| arl-dashboard | `curl -sf http://localhost:5173` | 10s | 5s | 10 (start: 15s) |
| arl-proxy | `wget -q --spider http://localhost:9000/health` | 10s | 5s | 3 |

---

## Dragonfly Commands

```bash
docker exec -it arl-dragonfly redis-cli
> INFO     # Server info
> DBSIZE   # Number of keys
> LLEN ai_jobs  # Queue length
> KEYS *  # View all keys (careful on production)
> MEMORY USAGE <key>  # Memory of key
> FLUSHALL  # Delete all data (careful!)
```

---

## Volumes

| Volume | Service | Purpose |
|--------|---------|---------|
| `arl-dragonfly-data` | arl-dragonfly | Persistent Redis data |
| `arl-prometheus-data` | arl-prometheus | Metrics retention (30d) |
| `arl-grafana-data` | arl-grafana | Dashboard & config persistence |

---

## Build UI for Embedded Dashboard

```bash
# Build static files into api-gateway/static/ (embedded via go:embed)
cd ui && bun run build

# Then rebuild gateway to pick up new static files
docker-compose up -d --build arl-gateway
```

The UI build produces `api-gateway/static/` which is embedded into the Go binary at build time via `//go:embed all:static`.

---

*Back to [Manual](../MANUAL.md)*
