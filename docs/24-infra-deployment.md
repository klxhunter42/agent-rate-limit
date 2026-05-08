# Infrastructure & Deployment Reference

Complete infrastructure documentation covering Docker Compose, Dockerfiles, Helm chart, observability stack, CI/CD pipeline, scripts, environment variables, sidecar architecture, and architecture diagrams.

---

## Table of Contents

1. [Architecture Diagram](#1-architecture-diagram)
2. [Docker Compose](#2-docker-compose)
3. [Dockerfiles](#3-dockerfiles)
4. [Helm Chart (Kubernetes)](#4-helm-chart-kubernetes)
5. [Observability Stack](#5-observability-stack)
6. [CI/CD Pipeline](#6-cicd-pipeline)
7. [Scripts](#7-scripts)
8. [Environment Variables](#8-environment-variables)
9. [Sidecar Architecture](#9-sidecar-architecture)

---

## 1. Architecture Diagram

### 1.1 Full System Architecture (Docker Compose)

```
                              EXTERNAL CLIENTS
                                    |
                                    v
                        +-----------+-----------+
                        |   arl-proxy (Caddy)   |
                        |     port 9000 (ext)   |
                        +-----------+-----------+
                                    |
             +----------------------+----------------------+
|                      |                      |
             v                      v                      v
   /v1/*, /api/*,         /grafana, /grafana/*       /*
   /ws, /health,          (Grafana UI)               (Dashboard UI)
   /metrics, /callback
|                      |                      |
             v                      v                      v
   +---------+--------+  +---------+--------+  +---------+--------+
|   arl-gateway     |  |   arl-grafana    |  |   arl-dashboard  |
|   (Go, :8080)     |  |   (:3000)        |  |   (Vite, :5173)  |
   +---------+--------+  +------------------+  +------------------+
             |
             +-- sidecar (Node.js, :8081) --> api.anthropic.com
             |     (billing + identity injection
             |      for Claude Code OAuth passthrough)
             |
    +--------+--------+
    |                 |
    v                 v
+---+------+  +-------+------+
| arl-rate- |  | arl-dragonfly|
| limiter   |  |   (:6379)    |
| (Java,    |  | (Redis-compat|
|  :8080)   |  |  in-memory)  |
+---+------+  +------+-------+
    |                 |
    +--------+--------+
             |
             v
   +---------+--------+
   |   arl-worker      |
   |   (Python,        |
   |   :9090 metrics,  |
   |   :9091 internal) |
   +---------+--------+
             |
             +-----------> Z.AI (api.z.ai/api/anthropic)
             +-----------> Anthropic (api.anthropic.com)
             +-----------> OpenAI (api.openai.com)
             +-----------> Gemini (generativelanguage.googleapis.com)
             +-----------> OpenRouter (openrouter.ai/api)
             +-----------> DeepSeek, Kimi, Qwen, etc.
```

### 1.2 Observability Data Flow

```
+--------------+     OTLP gRPC:4317     +------------------+
| arl-gateway  |----------------------->|   arl-otel       |
| arl-worker   |                        |   (OTel Collector|
+--------------+                        |    :4317 gRPC     |
                                        |    :4318 HTTP     |
                                        |    :8889 metrics) |
                                        +--------+---------+
                                                 |
                                                 | prometheus exporter
                                                 v
                                        +--------+---------+
+--------------+  scrape :9090          |                  |
| arl-gateway  |---(metrics /metrics)-->| arl-prometheus   |
| arl-worker   |---(metrics :9090)----->| (:9090)          |
| arl-rate-    |---(/actuator/          |                  |
|  limiter     |    prometheus)-------->| retention: 30d   |
| arl-otel     |---(:8889)------------->| 10GB             |
+--------------+                        +--------+---------+
                                                 |
                                                 | data source
                                                 v
                                        +--------+---------+
                                        |   arl-grafana    |
                                        |   (:3000)        |
                                        |   10 dashboards  |
                                        +------------------+
```

### 1.3 Request Flow (Sync Proxy)

```
Client (Claude Code)
  |
  v
arl-proxy (Caddy, :9000)
  |  POST /v1/messages
  v
arl-gateway (Go, :8080)
  |-- rate limit check --> arl-rate-limiter (Java, :8080)
  |     (token bucket via Redis)
  |-- quota check ------> arl-dragonfly (Redis, :6379)
  |-- PasteGuard --------> regex PII/secrets masking
  |
  +-- if CLI_SIDECAR_ENABLED:
  |     |-- forward to sidecar (Node.js, :8081)
  |     |     |-- inject billing header + identity into system[]
  |     |     |-- forward to api.anthropic.com
  |     |     |-- pipe response back (supports streaming)
  |     +--
  |
  +-- else (direct upstream):
        |-- route to configured provider (Z.AI, Anthropic, etc.)
        |-- model slot semaphore (per-model concurrency)
        |-- retry on 429 with backoff
        |-- stream response to client
```

### 1.4 Request Flow (Async Queue)

```
Client
  |
  POST /v1/chat/completions
  v
arl-gateway (Go)
  |-- enqueue job to arl-dragonfly (queue: ai_jobs)
  |-- return {"request_id": "...", "status": "queued"}
  |
  (client polls GET /v1/result/{request_id})
        |
        v
arl-worker (Python, asyncio)
  |-- dequeue from ai_jobs
  |-- rate limit (per-provider RPM)
  |-- model slot semaphore
  |-- forward to AI provider
  |-- store result in Redis (TTL: 600s)
  |
Client polls until status=completed or error
```

---

## 2. Docker Compose

**File**: `docker-compose.yml`

### 2.1 Network

| Name          | Driver   | Scope        |
|---------------|----------|--------------|
| `arl-network` | bridge   | All services |

### 2.2 Volumes

| Name                  | Used By          | Purpose                     |
|-----------------------|------------------|-----------------------------|
| `arl-dragonfly-data`  | `arl-dragonfly`  | Dragonfly persistence       |
| `arl-prometheus-data` | `arl-prometheus` | TSDB retention (30d, 10GB)  |
| `arl-grafana-data`    | `arl-grafana`    | Grafana dashboards/settings |

### 2.3 Services Overview

| Service            | Image / Build                                                  | Container Port   | External Port   | Profile        | Health Check                                     |
|--------------------|----------------------------------------------------------------|------------------|-----------------|----------------|--------------------------------------------------|
| `arl-gateway`      | `./api-gateway/Dockerfile`                                     | 8080             | -               | default        | `curl -sf http://localhost:8080/health`          |
| `arl-rate-limiter` | `./distributed-rate-limiter/Dockerfile`                        | 8080             | -               | default        | `curl -f http://localhost:8080/actuator/health`  |
| `arl-dragonfly`    | `ghcr.io/dragonflydb/dragonfly:v1.37.2`                        | 6379             | -               | default        | `redis-cli ping`                                 |
| `arl-worker`       | `./ai-worker/Dockerfile`                                       | 9090, 9091       | -               | default        | `curl -f http://localhost:9091/metrics-internal` |
| `arl-prometheus`   | `prom/prometheus:v2.54.1`                                      | 9090             | -               | default        | N/A (uses image default)                         |
| `arl-grafana`      | `grafana/grafana:11.3.0`                                       | 3000             | -               | default        | N/A (uses image default)                         |
| `arl-otel`         | `otel/opentelemetry-collector-contrib:0.112.0`                 | 4317, 4318, 8889 | -               | default        | N/A                                              |
| `arl-proxy`        | `caddy:2-alpine`                                               | 9000             | **9000**        | default        | `wget -q --spider http://localhost:9000/health`  |
| `arl-dashboard`    | `./ui/Dockerfile.dev`                                          | 5173             | -               | default        | `curl -sf http://localhost:5173`                 |
| `arl-rl-dashboard` | `./distributed-rate-limiter/examples/web-dashboard/Dockerfile` | 8080             | -               | `rl-dashboard` | N/A                                              |
| `arl-presidio`     | `mcr.microsoft.com/presidio-analyzer:2.2.362`                  | 3000             | -               | `pii`          | N/A                                              |
| `claude-code-meow` | `./docker/Dockerfile.claude-code`                              | -                | -               | `test-client`  | N/A                                              |
| `claude-code-test` | `./docker/Dockerfile.claude-code`                              | -                | -               | `test-client`  | N/A                                              |

### 2.4 Shared Environment (`x-common-env`)

```yaml
REDIS_ADDR: arl-dragonfly:6379
RATE_LIMITER_ADDR: http://arl-rate-limiter:8080
QUEUE_NAME: ai_jobs
OTLP_ENDPOINT: arl-otel:4317
```

### 2.5 Service Dependencies

```
arl-gateway
  depends_on: arl-dragonfly (healthy), arl-rate-limiter (healthy)

arl-rate-limiter
  depends_on: arl-dragonfly (healthy)

arl-worker
  depends_on: arl-dragonfly (healthy)

arl-grafana
  depends_on: arl-prometheus

arl-proxy
  depends_on: arl-gateway (healthy), arl-dashboard (healthy)

arl-rl-dashboard
  depends_on: arl-rate-limiter (healthy)
  profiles: ["rl-dashboard"]
```

### 2.6 Resource Limits

| Service            | Memory Limit  | CPU Limit   | Memory Reserve   | CPU Reserve   |
|--------------------|---------------|-------------|------------------|---------------|
| `arl-gateway`      | 1G            | 2.0         | 256M             | 0.5           |
| `arl-rate-limiter` | 1.5G          | 2.0         | 512M             | 0.5           |
| `arl-dragonfly`    | 8G            | 2.0         | 1G               | 0.5           |
| `arl-worker`       | 2G            | 2.0         | 512M             | 0.5           |
| `arl-prometheus`   | 2G            | 2.0         | 256M             | -             |
| `arl-grafana`      | 1G            | 2.0         | 256M             | -             |
| `arl-otel`         | 512M          | 1.0         | 128M             | -             |
| `arl-proxy`        | 128M          | 0.5         | 32M              | -             |
| `arl-dashboard`    | 1G            | 2.0         | 256M             | -             |
| `arl-rl-dashboard` | 256M          | 0.5         | 64M              | -             |
| `arl-presidio`     | 2G            | 2.0         | 512M             | -             |

### 2.7 Docker Compose Profiles

| Profile        | Services                               | Purpose                                               |
|----------------|----------------------------------------|-------------------------------------------------------|
| (default)      | All core services                      | Production/staging stack                              |
| `rl-dashboard` | `arl-rl-dashboard`                     | Rate limiter web dashboard (optional)                 |
| `pii`          | `arl-presidio`                         | Presidio PII analyzer (legacy, replaced by regex)     |
| `test-client`  | `claude-code-meow`, `claude-code-test` | Claude Code test clients with auto-token provisioning |

### 2.8 Caddy Proxy Routes

**File**: `docker/proxy/Caddyfile`

| Route Pattern            | Upstream                | Notes                            |
|--------------------------|-------------------------|----------------------------------|
| `/ws`                    | `arl-gateway:8080`      | WebSocket, flush immediately     |
| `/hmr`                   | `arl-dashboard:5173`    | Vite HMR (dev mode)              |
| `/v1/*`                  | `arl-gateway:8080`      | API messages endpoint, streaming |
| `/health`                | `arl-gateway:8080`      | Health check                     |
| `/api/*`                 | `arl-gateway:8080`      | Internal API, streaming          |
| `/metrics`               | `arl-gateway:8080`      | Prometheus metrics               |
| `/callback`              | `arl-gateway:8080`      | OAuth callback                   |
| `/grafana`, `/grafana/*` | `arl-grafana:3000`      | Grafana UI                       |
| `/rl/*`                  | `arl-rl-dashboard:8080` | Rate limiter dashboard           |
| `*` (default)            | `arl-dashboard:5173`    | Dashboard UI (Vite dev server)   |

### 2.9 Logging Configuration

All services use `json-file` log driver with rotation:
- Core services: 20MB max, 5 files
- Observability: 10MB max, 3 files
- Proxy/test: 5MB max, 2 files

---

## 3. Dockerfiles

### 3.1 API Gateway (`api-gateway/Dockerfile`)

**Multi-stage build** (Go production):

| Stage   | Base Image           | Purpose           |
|---------|----------------------|-------------------|
| Builder | `golang:1.24-alpine` | Compile Go binary |
| Runtime | `alpine:3.20`        | Minimal runtime   |

**Build optimizations**:
- `CGO_ENABLED=0` - Static binary, no C dependencies
- `-trimpath` - Removes build machine paths from binary
- `-ldflags="-s -w"` - Strip debug info and DWARF symbols
- `go mod download` in separate layer (cached unless go.mod/go.sum change)
- Version injected via `git describe --tags` at build time

**Runtime**:
- Installs `ca-certificates`, `curl`, `nodejs`
- Non-root user `gateway:gateway`
- Sidecar files copied from `sidecar/` directory
- Entrypoint: `/app/sidecar/entrypoint.sh` (starts sidecar + gateway)

### 3.2 Sidecar (`api-gateway/sidecar/Dockerfile`)

**Standalone sidecar image** (for K8s separate deployment):

| Property       | Value                                      |
|----------------|--------------------------------------------|
| Base           | `node:20-alpine`                           |
| Extra packages | `curl`                                     |
| Install        | `npm install --omit=dev` (production only) |
| User           | `1000` (non-root)                          |
| Port           | 8081                                       |
| CMD            | `node index.js`                            |

### 3.3 Claude Code Test Client (`docker/Dockerfile.claude-code`)

| Property       | Value                       |
|----------------|-----------------------------|
| Base           | `node:22-slim`              |
| Extra packages | `git`, `curl`, `python3`    |
| Global install | `@anthropic-ai/claude-code` |
| Entrypoint     | `entrypoint-claude.sh`      |

**Entrypoint behavior** (`docker/entrypoint-claude.sh`):
1. Wait for gateway health (`/health` on arl-gateway:8080)
2. Auto-provision token via `POST /v1/profiles/{PROFILE_NAME}/tokens`
3. Configure `~/.claude/settings.json` with:
   - `ANTHROPIC_BASE_URL` pointing to arl-proxy:9000
   - `ANTHROPIC_AUTH_TOKEN` with provisioned token

   > **Note:** `apiKeyHelper` + `ANTHROPIC_API_KEY` is the legacy method. Use `ANTHROPIC_AUTH_TOKEN` instead.

   - Model and thinking settings from previous config
4. Set `CLAUDE_CODE_SIMPLE=1` when no TTY attached
5. Exec `claude` with arguments

---

## 4. Helm Chart (Kubernetes)

**Path**: `helm/ai-gateway/`

### 4.1 Chart Metadata

| Property   | Value         |
|------------|---------------|
| Name       | `ai-gateway`  |
| Version    | `0.2.0`       |
| AppVersion | `1.1.0`       |
| Type       | `application` |

### 4.2 Values Structure (Global)

```yaml
global:
  namespace: ai-gateway
  securityContext:
    runAsNonRoot: true
    readOnlyRootFilesystem: true
    allowPrivilegeEscalation: false
    capabilities:
      drop: ["ALL"]

istio:
  enabled: true
  gateway: "ai-gateway-gateway"
  hosts: ["*"]
```

### 4.3 Components

Each component follows a consistent pattern with:
- `enabled` flag (conditional rendering)
- `replicaCount`
- `image` (repository, tag, pullPolicy)
- `service` (port definitions)
- `resources` (requests/limits)
- Pod anti-affinity (preferred, weight 100)
- Security context (non-root, read-only filesystem, drop ALL capabilities)
- `emptyDir` volume for `/tmp`

#### 4.3.1 Gateway

| Property        | Value                                                                                    |
|-----------------|------------------------------------------------------------------------------------------|
| Replicas        | 2                                                                                        |
| Image           | `earth4242/ai-gateway`                                                                   |
| Port            | 8080                                                                                     |
| Resources       | 256Mi-1Gi RAM, 500m-2 CPU                                                                |
| HPA             | Optional (2-10 replicas, CPU 70%, Memory 80%)                                            |
| Probes          | Readiness: `/health` (5s delay), Liveness: `/health` (10s delay)                         |
| Config checksum | Secrets template hash triggers rollout on change                                         |

**Gateway HPA** (optional, controlled by `gateway.hpa.enabled`):
- Min replicas: 2
- Max replicas: 10
- Target CPU: 70%
- Target Memory: 80%

#### 4.3.2 Worker

| Property        | Value                                                                                                               |
|-----------------|---------------------------------------------------------------------------------------------------------------------|
| Replicas        | 2                                                                                                                   |
| Image           | `earth4242/ai-worker`                                                                                               |
| Ports           | 9090 (metrics), 9091 (internal)                                                                                     |
| Resources       | 512Mi-2Gi RAM, 500m-2 CPU                                                                                           |
| Probes          | `/metrics-internal` on port 9091                                                                                    |

#### 4.3.3 Rate Limiter

| Property   | Value                                                                                                                     |
|------------|---------------------------------------------------------------------------------------------------------------------------|
| Replicas   | 2                                                                                                                         |
| Image      | `earth4242/distributed-rate-limiter`                                                                                      |
| Port       | 8080                                                                                                                      |
| Resources  | 512Mi-1536Mi RAM, 500m-2 CPU                                                                                              |
| Probes     | Startup: `/actuator/health` (30s initial), Readiness: `/actuator/health/readiness`, Liveness: `/actuator/health/liveness` |
| JVM opts   | G1GC, 75% RAM, string deduplication                                                                                       |

#### 4.3.4 Dragonfly (StatefulSet)

| Property    | Value                                                                                              |
|-------------|----------------------------------------------------------------------------------------------------|
| Replicas    | 1                                                                                                  |
| Image       | `ghcr.io/dragonflydb/dragonfly:v1.37.2`                                                            |
| Port        | 6379                                                                                               |
| Resources   | 1Gi-8Gi RAM, 500m-2 CPU                                                                            |
| Persistence | PVC (20Gi, configurable storageClass)                                                              |
| Services    | ClusterIP + Headless service                                                                       |
| Probes      | `redis-cli ping`                                                                                   |
| Args        | `maxmemory=4gb`, `proactor_threads=4`, `cache_mode=true`, `tcp_keepalive=60`, `pipeline_squash=10` |

#### 4.3.5 Prometheus

| Property    | Value                                         |
|-------------|-----------------------------------------------|
| Replicas    | 1                                             |
| Image       | `prom/prometheus:v2.54.1`                     |
| Port        | 9090                                          |
| Resources   | 256Mi-2Gi RAM, 250m-2 CPU                     |
| Persistence | PVC (50Gi, configurable storageClass)         |
| Retention   | 30d, 10GB max                                 |
| Config      | ConfigMap with scrape targets                 |
| Probes      | Readiness: `/-/ready`, Liveness: `/-/healthy` |
| fsGroup     | 65534                                         |

#### 4.3.6 Grafana

| Property       | Value                                                         |
|----------------|---------------------------------------------------------------|
| Replicas       | 1                                                             |
| Image          | `grafana/grafana:11.3.0`                                      |
| Port           | 3000                                                          |
| Resources      | 256Mi-1Gi RAM, 250m-2 CPU                                     |
| Persistence    | PVC (10Gi, configurable storageClass)                         |
| Admin password | From secret `ai-gateway-secrets` key `grafana-admin-password` |
| Provisioning   | ConfigMap with datasources + dashboards                       |
| fsGroup        | 472                                                           |
| Root URL       | `%(protocol)s://%(domain)s/grafana` (sub-path)                |

#### 4.3.7 OpenTelemetry Collector

| Property   | Value                                          |
|------------|------------------------------------------------|
| Replicas   | 1                                              |
| Image      | `otel/opentelemetry-collector-contrib:0.112.0` |
| Ports      | 4317 (gRPC), 4318 (HTTP), 8889 (metrics)       |
| Resources  | 128Mi-512Mi RAM, 250m-1 CPU                    |
| Config     | ConfigMap (receivers, processors, exporters)   |

#### 4.3.8 Proxy (Caddy)

| Property   | Value                         |
|------------|-------------------------------|
| Replicas   | 2                             |
| Image      | `caddy:2-alpine`              |
| Port       | 9000                          |
| Resources  | 32Mi-128Mi RAM, 100m-500m CPU |
| Config     | ConfigMap with Caddyfile      |
| Probes     | `/health` on port 9000        |

#### 4.3.9 Dashboard

| Property   | Value                         |
|------------|-------------------------------|
| Replicas   | 2                             |
| Image      | `earth4242/ai-dashboard`      |
| Port       | 80                            |
| Resources  | 64Mi-256Mi RAM, 100m-500m CPU |
| Probes     | `/` on port 80                |

#### 4.3.10 RL Dashboard

| Property   | Value                         |
|------------|-------------------------------|
| Replicas   | 1                             |
| Image      | `earth4242/ai-rl-dashboard`   |
| Port       | 8080                          |
| Resources  | 64Mi-256Mi RAM, 100m-500m CPU |
| Probes     | `/` on port 8080              |

#### 4.3.11 Sidecar

| Property        | Value                          |
|-----------------|--------------------------------|
| Replicas        | 1                              |
| Image           | `earth4242/ai-sidecar`         |
| Port            | 8081                           |
| Resources       | 128Mi-256Mi RAM, 100m-500m CPU |
| Probes          | `/health` on port 8081         |
| Secrets mounted | `ZAI_API_KEYS` (optional)      |

#### 4.3.12 Presidio (Disabled by Default)

| Property   | Value                                         |
|------------|-----------------------------------------------|
| Replicas   | 1                                             |
| Image      | `mcr.microsoft.com/presidio-analyzer:2.2.362` |
| Port       | 3000                                          |
| Resources  | 512Mi-2Gi RAM, 250m-2 CPU                     |
| Probes     | `/healthcheck` on port 3000                   |

### 4.4 Secrets

Managed via `templates/secrets.yaml`, creating `ai-gateway-secrets`:

| Key                          | Source Value                                           |
|------------------------------|--------------------------------------------------------|
| `upstream-api-keys`          | `secrets.upstreamApiKeys`                              |
| `glm-api-keys`               | `secrets.glmApiKeys`                                   |
| `openai-api-keys`            | `secrets.openaiApiKeys`                                |
| `anthropic-api-keys`         | `secrets.anthropicApiKeys`                             |
| `gemini-api-keys`            | `secrets.geminiApiKeys`                                |
| `openrouter-api-keys`        | `secrets.openrouterApiKeys`                            |
| `gemini-oauth-client-id`     | `secrets.geminiOAuthClientId`                          |
| `gemini-oauth-client-secret` | `secrets.geminiOAuthClientSecret`                      |
| `grafana-admin-password`     | `secrets.grafanaAdminPassword` (default: `devopscore`) |
| `dashboard-api-key`          | `secrets.dashboardApiKey`                              |

### 4.5 Istio VirtualService

When `istio.enabled=true`, creates a VirtualService routing to the proxy service on port 9000 with 300s timeout.

### 4.6 Dashboard ConfigMaps (Helm)

Grafana provisioning ConfigMap embeds all JSON dashboard files from `files/dashboards/`:

| Dashboard            | File                                         |
|----------------------|----------------------------------------------|
| AI Worker            | `files/dashboards/ai-worker.json`            |
| API Gateway Overview | `files/dashboards/api-gateway-overview.json` |
| API Gateway Runtime  | `files/dashboards/api-gateway-runtime.json`  |
| Cost Calculator      | `files/dashboards/cost-calculator.json`      |
| PasteGuard           | `files/dashboards/pasteguard.json`           |
| System Overview      | `files/dashboards/system-overview.json`      |

---

## 5. Observability Stack

### 5.1 Prometheus

**File**: `prometheus/prometheus.yml`

**Global config**:
- Scrape interval: 15s (global), 10s (per-job override)
- Evaluation interval: 15s
- External labels: `cluster=agent-rate-limit`, `replica=1`

**Scrape Targets**:

| Job Name         | Target                  | Metrics Path           | Interval   |
|------------------|-------------------------|------------------------|------------|
| `api-gateway`    | `arl-gateway:8080`      | `/metrics`             | 10s        |
| `ai-worker`      | `arl-worker:9090`       | `/metrics`             | 10s        |
| `rate-limiter`   | `arl-rate-limiter:8080` | `/actuator/prometheus` | 10s        |
| `otel-collector` | `arl-otel:8889`         | `/metrics`             | 10s        |
| `prometheus`     | `localhost:9090`        | `/metrics`             | default    |

Dragonfly is commented out (Redis protocol on port 6379 does not expose Prometheus metrics; requires `--metrics` flag or `redis_exporter` sidecar).

**Storage**:
- Retention: 30 days or 10GB
- WAL compression enabled
- Lifecycle API enabled (`--web.enable-lifecycle`)

### 5.2 OpenTelemetry Collector

**File**: `otel/otel-collector-config.yml`

**Receivers**:
- OTLP gRPC on `0.0.0.0:4317`
- OTLP HTTP on `0.0.0.0:4318`

**Processors**:
- `batch`: 5s timeout, 1024 batch size
- `memory_limiter`: 200MiB limit, 1s check interval

**Exporters**:
- `prometheus`: Exports to `0.0.0.0:8889` with namespace `agent_rate_limit`
- `debug`: Basic verbosity (Docker Compose only; K8s Helm removes this)

**Pipelines**:
| Pipeline   | Receivers   | Processors            | Exporters                   |
|------------|-------------|-----------------------|-----------------------------|
| Traces     | otlp        | memory_limiter, batch | debug (Docker) / none (K8s) |
| Metrics    | otlp        | memory_limiter, batch | prometheus                  |

### 5.3 Grafana

**Datasources** (`grafana/provisioning/datasources/datasources.yml`):
- Prometheus at `http://arl-prometheus:9090` (proxy mode, non-editable, default)

**Dashboard Provisioning** (`grafana/provisioning/dashboards/dashboards.yml`):
- File-based provider from `/etc/grafana/provisioning/dashboards`

**Dashboards** (10 total in Docker Compose, 6 in Helm):

| Dashboard            | Docker Compose  | Helm Chart   |
|----------------------|-----------------|--------------|
| API Gateway Overview | Yes             | Yes          |
| API Gateway Runtime  | Yes             | Yes          |
| System Overview      | Yes             | Yes          |
| AI Worker            | Yes             | Yes          |
| Cost Calculator      | Yes             | Yes          |
| PasteGuard           | Yes             | Yes          |
| Claude OAuth Billing | Yes             | No           |
| Token Optimization   | Yes             | No           |
| Token Usage          | Yes             | No           |

**Grafana Configuration**:
- Anonymous access: disabled
- Sub-path: `/grafana`
- Admin password: from `GRAFANA_ADMIN_PASSWORD` env var
- Log level: `warn`

---

## 6. CI/CD Pipeline

**File**: `.github/workflows/build.yml`

**Trigger**: Push to `main` branch or manual dispatch

**Strategy**: Matrix build (6 images, parallel)

| Matrix Name   | Context                                             | Dockerfile       | Image Name                 |
|---------------|-----------------------------------------------------|------------------|----------------------------|
| gateway       | `./api-gateway`                                     | `Dockerfile`     | `ai-gateway`               |
| worker        | `./ai-worker`                                       | `Dockerfile`     | `ai-worker`                |
| rate-limiter  | `./distributed-rate-limiter`                        | `Dockerfile`     | `distributed-rate-limiter` |
| dashboard     | `./ui`                                              | `Dockerfile.dev` | `ai-dashboard`             |
| rl-dashboard  | `./distributed-rate-limiter/examples/web-dashboard` | `Dockerfile`     | `ai-rl-dashboard`          |
| sidecar       | `./api-gateway/sidecar`                             | `Dockerfile`     | `ai-sidecar`               |

**Per-job steps**:
1. Checkout (`actions/checkout@v4`)
2. Setup Buildx (`docker/setup-buildx-action@v3`)
3. Login to GHCR (`docker/login-action@v3` with `GITHUB_TOKEN`)
4. Build & Push (`docker/build-push-action@v6`)

**Build details**:
- Registry: `ghcr.io`
- Platforms: `linux/amd64`, `linux/arm64`
- Tags: `latest` + `<git-sha>`
- Cache: GitHub Actions cache (`type=gha`)
- `fail-fast: false` (all images build independently)

---

## 7. Scripts

**Path**: `scripts/`

### 7.1 `anthropic-openai-proxy.py`

Lightweight Anthropic-to-OpenAI format translator proxy.

- Listens on port 8999
- Accepts Anthropic `/v1/messages` POST requests
- Converts to OpenAI chat completions format
- Converts response back to Anthropic format
- Non-streaming only
- Health check: `GET /health`



- Listens on port 8999
- Patches `message_start` SSE events by injecting missing `usage`, `role`, `type`, `stop_reason` fields
- Supports both streaming and non-streaming
- Forwards `anthropic-version` and `anthropic-beta` headers

### 7.3 `concurrent-test.sh`

Concurrent load tester for finding optimal concurrency thresholds.

- Default levels: 3, 5, 10, 15, 20 concurrent requests
- Targets `/v1/chat/completions` (async queue mode)
- Measures wall time, per-request latency, 429 errors
- Reports p50, p95 latency percentiles
- 5s cooldown between levels

### 7.4 `conversation-test.sh`

Multi-turn conversation test (Thai + English).

- 8-turn test: 4 Thai questions, 2 implementation tasks, 2 cleanup
- Targets `/v1/messages` (sync proxy mode)
- Checks for artifact leakage (tool_call, tool_result, thinking tags)
- Logs raw responses for inspection
- Reports artifact count

### 7.5 `multi-agent-test.sh`

Realistic multi-agent simulation.

- Default: 5 agents x 2 turns = 10 requests
- Simulates different agent personalities (code reviewer, test writer, doc generator, etc.)
- Uses async queue: POST to `/v1/chat/completions`, poll `/v1/result/{id}`
- Measures per-agent latency, throughput (req/min)
- Reports model distribution

### 7.6 `stress-test.sh`

Simple concurrent stress test for sync endpoint.

- 10 concurrent requests to `/v1/messages`
- Different question per agent
- Reports success/fail/rate-limited counts and average response time

---

## 8. Environment Variables

### 8.1 API Gateway (arl-gateway)

#### Core Server

| Variable               | Default                        | Description                            |
|------------------------|--------------------------------|----------------------------------------|
| `SERVER_PORT`          | `:8080`                        | Gateway listen address                 |
| `DASHBOARD_URL`        | `https://ai.klxhub.com`        | Dashboard base URL for OAuth callbacks |
| `OAUTH_CALLBACK_BASE`  | `https://ai.klxhub.com`        | OAuth callback base URL                |
| `GLOBAL_RATE_LIMIT`    | `100`                          | Global requests per second             |
| `AGENT_RATE_LIMIT`     | `5`                            | Per-agent requests per second          |
| `WORKER_POOL_SIZE`     | `100`                          | Max concurrent worker goroutines       |
| `REDIS_ADDR`           | `arl-dragonfly:6379`           | Redis/Dragonfly address                |
| `REDIS_POOL_SIZE`      | `50`                           | Redis connection pool size             |
| `REDIS_MIN_IDLE_CONNS` | `10`                           | Minimum idle Redis connections         |
| `RATE_LIMITER_ADDR`    | `http://arl-rate-limiter:8080` | Rate limiter service address           |
| `QUEUE_NAME`           | `ai_jobs`                      | Redis queue name for async jobs        |
| `OTLP_ENDPOINT`        | `arl-otel:4317`                | OpenTelemetry OTLP endpoint            |
| `MAX_REQUEST_BODY`     | `10485760`                     | Max request body size (10MB)           |

#### Upstream Provider

| Variable                       | Default                          | Description                                |
|--------------------------------|----------------------------------|--------------------------------------------|
| `UPSTREAM_URL`                 | `https://api.z.ai/api/anthropic` | Primary upstream URL                       |
| `STREAM_TIMEOUT`               | `300s`                           | Stream response timeout                    |
| `UPSTREAM_MODEL_LIMITS`        | (see .env.example)               | Per-model concurrency limits (model:limit) |
| `UPSTREAM_VISION_MODEL_LIMITS` | (see .env.example)               | Per-vision-model concurrency limits        |
| `UPSTREAM_DEFAULT_LIMIT`       | `1`                              | Default per-model concurrency limit        |
| `UPSTREAM_GLOBAL_LIMIT`        | `9`                              | Total concurrent upstream requests         |
| `UPSTREAM_MAX_RETRIES`         | `3`                              | Max retries on 429 errors                  |
| `UPSTREAM_RETRY_BACKOFF`       | `500ms`                          | Retry backoff duration                     |
| `UPSTREAM_RPM_LIMIT`           | `40`                             | Upstream requests per minute               |
| `UPSTREAM_PROBE_MULTIPLIER`    | `5`                              | RPM probe multiplier                       |

#### API Keys

| Variable                     | Description                                  |
|------------------------------|----------------------------------------------|
| `ZAI_API_KEYS`               | Z.AI API keys (comma-separated for rotation) |
| `GEMINI_OAUTH_CLIENT_ID`     | Google OAuth client ID                       |
| `GEMINI_OAUTH_CLIENT_SECRET` | Google OAuth client secret                   |

#### Token Optimization

| Variable                  | Default   | Description                          |
|---------------------------|-----------|--------------------------------------|
| `ENABLE_PROMPT_INJECTION` | `true`    | Enable prompt injection/optimization |
| `ENABLE_RESPONSE_TRIM`    | `true`    | Trim response whitespace             |
| `ENABLE_SMART_MAX_TOKENS` | `true`    | Smart max_tokens estimation          |
| `PROMPT_INJECTION_TEXT`   | (empty)   | Custom prompt injection text         |

#### PasteGuard

| Variable                         | Default                    | Description                       |
|----------------------------------|----------------------------|-----------------------------------|
| `PASTEGUARD_ENABLED`             | `true`                     | Enable PasteGuard privacy masking |
| `PASTEGUARD_SECRETS_ENABLED`     | `true`                     | Enable secret detection           |
| `PASTEGUARD_SECRET_ENTITIES`     | (empty, uses defaults)     | Custom secret entity types        |
| `PASTEGUARD_MAX_SCAN_CHARS`      | `200000`                   | Max characters to scan            |
| `PASTEGUARD_PII_ENABLED`         | `true`                     | Enable PII detection              |
| `PASTEGUARD_PII_ENTITIES`        | (empty, uses defaults)     | Custom PII entity types           |
| `PASTEGUARD_PRESIDIO_URL`        | `http://arl-presidio:3000` | Presidio analyzer URL             |
| `PASTEGUARD_PII_SCORE_THRESHOLD` | `0.7`                      | PII confidence threshold          |
| `PASTEGUARD_PII_LANGUAGE`        | `en`                       | PII detection language            |

#### Provider Routing

| Variable              | Default                           | Description                   |
|-----------------------|-----------------------------------|-------------------------------|
| `GLM_MODE`            | `true`                            | Enable Z.AI/GLM provider mode |
| `CLI_SIDECAR_ENABLED` | `true`                            | Enable Claude Code sidecar    |
| `CLI_SIDECAR_URL`     | `http://127.0.0.1:8081`           | Sidecar service URL           |
| `SIDECAR_PORT`        | `8081`                            | Sidecar listen port           |

#### Model Pricing

| Variable        | Default                            | Description                      |
|-----------------|------------------------------------|----------------------------------|
| `MODEL_PRICING` | (model:input:output per 1M tokens) | Pricing config for cost tracking |

#### Quota

| Variable                | Default   | Description                          |
|-------------------------|-----------|--------------------------------------|
| `QUOTA_CACHE_TTL`       | `30s`     | Quota cache TTL                      |
| `QUOTA_DAILY_BUDGET`    | `57600`   | Daily token budget                   |
| `QUOTA_BLOCK_PCT`       | `95`      | Block percentage threshold           |
| `QUOTA_REDIS_POOL_SIZE` | `5`       | Quota Redis pool size                |
| `QUOTA_REDIS_MIN_IDLE`  | `2`       | Minimum idle quota Redis connections |

#### Default Request Values

| Variable              | Default   | Description                |
|-----------------------|-----------|----------------------------|
| `DEFAULT_MODEL`       | `glm-5`   | Default model for requests |
| `DEFAULT_PROVIDER`    | `glm`     | Default provider           |
| `DEFAULT_TEMPERATURE` | `0.7`     | Default temperature        |
| `DEFAULT_MAX_TOKENS`  | `1024`    | Default max tokens         |

#### Adaptive Limiter

| Variable               | Default   | Description                             |
|------------------------|-----------|-----------------------------------------|
| `ANOMALY_COOLDOWN_SEC` | `5`       | Anomaly detection cooldown              |
| `ANOMALY_Z_THRESHOLD`  | `2.0`     | Z-score threshold for anomaly detection |

### 8.2 Rate Limiter (arl-rate-limiter)

| Variable                                | Default                       | Description              |
|-----------------------------------------|-------------------------------|--------------------------|
| `SPRING_DATA_REDIS_HOST`                | `arl-dragonfly`               | Redis host               |
| `SPRING_DATA_REDIS_PORT`                | `6379`                        | Redis port               |
| `RATELIMITER_CAPACITY`                  | `1000`                        | Token bucket capacity    |
| `RATELIMITER_REFILL_RATE`               | `100`                         | Token refill rate        |
| `RATELIMITER_REFILL_PERIOD_SECONDS`     | `1`                           | Refill period in seconds |
| `RATELIMITER_SECURITY_IP_WHITELIST`     | (empty)                       | IP whitelist             |
| `RATELIMITER_SECURITY_API_KEYS_ENABLED` | `false`                       | API key auth             |
| `RATELIMITER_ADAPTIVE_ENABLED`          | `true`                        | Adaptive rate limiting   |
| `RATELIMITER_GEOGRAPHIC_ENABLED`        | `false`                       | Geographic rate limiting |
| `SPRING_PROFILES_ACTIVE`                | `docker`                      | Spring profile           |
| `SERVER_PORT`                           | `8080`                        | Server port              |
| `JAVA_OPTS`                             | (G1GC, 75% RAM, string dedup) | JVM options              |

### 8.3 AI Worker (arl-worker)

| Variable                       | Default                          | Description                   |
|--------------------------------|----------------------------------|-------------------------------|
| `REDIS_URL`                    | `redis://arl-dragonfly:6379`     | Redis connection URL          |
| `WORKER_CONCURRENCY`           | `50`                             | Concurrent coroutine count    |
| `MAX_RETRIES`                  | `3`                              | Max job retries               |
| `BASE_BACKOFF`                 | `1.0`                            | Retry backoff base (seconds)  |
| `RESULT_TTL`                   | `600`                            | Result TTL in Redis (seconds) |
| `METRICS_PORT`                 | `9090`                           | Prometheus metrics port       |
| `GLM_API_KEYS`                 | (empty)                          | Z.AI API keys                 |
| `GLM_ENDPOINT`                 | `https://api.z.ai/api/anthropic` | Z.AI endpoint                 |
| `OPENAI_API_KEYS`              | (empty)                          | OpenAI API keys               |
| `ANTHROPIC_API_KEYS`           | (empty)                          | Anthropic API keys            |
| `GEMINI_API_KEYS`              | (empty)                          | Gemini API keys               |
| `OPENROUTER_API_KEYS`          | (empty)                          | OpenRouter API keys           |
| `UPSTREAM_MODEL_LIMITS`        | (same as gateway)                | Per-model concurrency limits  |
| `UPSTREAM_VISION_MODEL_LIMITS` | (same as gateway)                | Vision model limits           |
| `UPSTREAM_DEFAULT_LIMIT`       | `1`                              | Default per-model limit       |
| `UPSTREAM_GLOBAL_LIMIT`        | `9`                              | Global concurrent limit       |
| `PROVIDER_RPM_LIMITS`          | `glm:5`                          | Per-provider RPM limits       |

### 8.4 Grafana (arl-grafana)

| Variable                        | Default                         | Description                  |
|---------------------------------|---------------------------------|------------------------------|
| `GF_SECURITY_ADMIN_PASSWORD`    | (required)                      | Admin password (from `.env`) |
| `GF_AUTH_ANONYMOUS_ENABLED`     | `false`                         | Anonymous access             |
| `GF_LOG_LEVEL`                  | `warn`                          | Log level                    |
| `GF_SERVER_ROOT_URL`            | `http://localhost:9000/grafana` | Root URL                     |
| `GF_SERVER_SERVE_FROM_SUB_PATH` | `true`                          | Sub-path serving             |

### 8.5 Dashboard UI (arl-dashboard)

| Variable             | Default     | Description        |
|----------------------|-------------|--------------------|
| `VITE_ALLOWED_HOSTS` | `localhost` | CORS allowed hosts |
| `VITE_HMR_HOST`      | (empty)     | Vite HMR host      |
| `VITE_HMR_PORT`      | `443`       | Vite HMR port      |

### 8.6 Caddy Proxy (arl-proxy)

| Variable     | Default      | Description     |
|--------------|--------------|-----------------|
| `PROXY_HOST` | (IP address) | Proxy bind host |
| `PROXY_PORT` | `9000`       | Proxy port      |

### 8.7 Dragonfly (arl-dragonfly)

| Argument             | Default   | Description                           |
|----------------------|-----------|---------------------------------------|
| `--maxmemory`        | `4gb`     | Max memory                            |
| `--proactor_threads` | `4`       | Thread count                          |
| `--cache_mode`       | `true`    | Cache mode (evict on memory pressure) |
| `--tcp_keepalive`    | `60`      | TCP keepalive seconds                 |
| `--pipeline_squash`  | `10`      | Pipeline squashing                    |

### 8.8 Sidecar

| Variable       | Default   | Description   |
|----------------|-----------|---------------|
| `SIDECAR_PORT` | `8081`    | Listen port   |

### 8.9 Claude Code Test Clients

| Variable                                   | Default                    | Description                    |
|--------------------------------------------|----------------------------|--------------------------------|
| `PROFILE_NAME`                             | `meow` / `test`            | Profile for token provisioning |
| `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` | `1`                        | Disable telemetry              |
| `CLAUDE_CODE_DISABLE_ANALYTICS`            | `1`                        | Disable analytics              |
| `CLAUDE_CODE_SIMPLE`                       | `1` (auto-set when no TTY) | Simple mode for Docker         |
| `CLAUDE_OAUTH_TOKEN`                       | (optional)                 | OAuth token for passthrough    |

### 8.10 Docker Platform

| Variable          | Default       | Description                |
|-------------------|---------------|----------------------------|
| `DOCKER_PLATFORM` | `linux/arm64` | Target platform for builds |

### 8.11 Upstream Provider URLs (from .env.example)

| Variable                    | Default                                             | Description            |
|-----------------------------|-----------------------------------------------------|------------------------|
| `ANTHROPIC_UPSTREAM_BASE`   | `https://api.anthropic.com`                         | Anthropic API base     |
| `CLAUDE_UPSTREAM_BASE`      | `https://claude.ai`                                 | Claude.ai base         |
| `GEMINI_UPSTREAM_BASE`      | `https://generativelanguage.googleapis.com`         | Gemini API base        |
| `GEMINI_CODEASSIST_BASE`    | `https://cloudcode-pa.googleapis.com/v1internal`    | CodeAssist base        |
| `OPENAI_UPSTREAM_BASE`      | `https://api.openai.com`                            | OpenAI API base        |
| `OPENROUTER_UPSTREAM_BASE`  | `https://openrouter.ai/api`                         | OpenRouter API base    |
| `DEEPSEEK_UPSTREAM_BASE`    | `https://api.deepseek.com`                          | DeepSeek API base      |
| `KIMI_UPSTREAM_BASE`        | `https://api.kimi.com/coding`                       | Kimi API base          |
| `HUGGINGFACE_UPSTREAM_BASE` | `https://api-inference.huggingface.co/models`       | HuggingFace base       |
| `OLLAMA_UPSTREAM_BASE`      | `http://localhost:11434`                            | Ollama local base      |
| `QWEN_UPSTREAM_BASE`        | `https://dashscope.aliyuncs.com/compatible-mode/v1` | Qwen API base          |

---

## 9. Sidecar Architecture

### 9.1 Overview

The sidecar is a Node.js HTTP proxy that injects Claude Code billing headers and identity prompts into requests before forwarding to `api.anthropic.com`. It enables Claude Code OAuth passthrough authentication.

### 9.2 Deployment Modes

**Mode 1: Embedded in Gateway Container (Docker Compose)**

```
+-----------------------------------+
|        arl-gateway container       |
|                                    |
|  +-----------+    +-------------+ |
|  | Go gateway |    | Node.js     | |
|  |  :8080     |--->| sidecar     | |
|  |            |    |  :8081      | |
|  +-----------+    +------+------+ |
|                          |        |
+--------------------------|--------+
                           |
                           v
                    api.anthropic.com
```

The gateway Dockerfile copies `sidecar/` into `/app/sidecar/`. The entrypoint script starts the sidecar Node.js process in the background, then starts the Go gateway in the foreground.

**Mode 2: Standalone Pod (Kubernetes)**

```
+-------------------+     +-------------------+
| gateway pod       |     | sidecar pod       |
| (Go, :8080)       |---->| (Node.js, :8081)  |
|                   |     |                   |
+-------------------+     +---------+---------+
                                    |
                                    v
                             api.anthropic.com
```

The sidecar has its own Deployment and Service in the Helm chart, running as a separate pod.

### 9.3 Sidecar Logic (index.js)

**Request flow**:

1. Accepts POST requests on port 8081
2. Parses JSON body
3. Calls `injectBillingAndIdentity(body)`:
   - Extracts first user message text
   - Computes a build hash from characters at positions [4, 7, 20] of the text
   - Creates billing header: `x-anthropic-billing-header: cc_version={VERSION}.{hash}; cc_entrypoint=cli; cch=00000;`
   - Injects identity prompt: "You are Claude Code, Anthropic's official CLI for Claude."
   - Prepends both as `system[]` text blocks
4. Forwards all headers (except hop-by-hop headers) to `https://api.anthropic.com`
5. Preserves `Authorization: Bearer` header for OAuth passthrough (does NOT convert to `x-api-key`)
6. Pipes response back to caller (supports streaming via `res.pipe()`)

**Health check**: `GET /health` returns `{"status":"ok"}`

**Security**:
- HTTPS agent with keepAlive (max 10 sockets)
- Hop-by-hop headers stripped (connection, keep-alive, transfer-encoding, te, upgrade, host)
- Non-root user (UID 1000 in standalone mode)

### 9.4 Entrypoint Script (entrypoint.sh)

```sh
node /app/sidecar/index.js &     # Start sidecar in background
sleep 0.5                         # Wait for port bind
exec /app/api-gateway             # Start Go gateway (PID 1)
```

The sidecar runs as a background process; the Go gateway is PID 1 (receives signals).

### 9.5 Configuration

| Variable              | Default                 | Description                     |
|-----------------------|-------------------------|---------------------------------|
| `SIDECAR_PORT`        | `8081`                  | Listen port                     |
| `CLI_SIDECAR_ENABLED` | `true`                  | Enable/disable sidecar routing  |
| `CLI_SIDECAR_URL`     | `http://127.0.0.1:8081` | Sidecar URL (gateway uses this) |

When `CLI_SIDECAR_ENABLED=true`, the gateway routes Claude Code OAuth requests through the sidecar. When disabled, requests go directly to the configured upstream provider.

---

## Appendix: Port Summary

| Port   | Service               | Protocol   | External                |
|--------|-----------------------|------------|-------------------------|
| 9000   | arl-proxy (Caddy)     | HTTP       | Yes (only exposed port) |
| 8080   | arl-gateway           | HTTP       | No                      |
| 8080   | arl-rate-limiter      | HTTP       | No                      |
| 8080   | arl-rl-dashboard      | HTTP       | No                      |
| 8081   | sidecar               | HTTP       | No                      |
| 6379   | arl-dragonfly         | Redis      | No                      |
| 9090   | arl-worker (metrics)  | HTTP       | No                      |
| 9090   | arl-prometheus        | HTTP       | No                      |
| 9091   | arl-worker (internal) | HTTP       | No                      |
| 3000   | arl-grafana           | HTTP       | No                      |
| 3000   | arl-presidio          | HTTP       | No                      |
| 4317   | arl-otel (gRPC)       | gRPC       | No                      |
| 4318   | arl-otel (HTTP)       | HTTP       | No                      |
| 8889   | arl-otel (metrics)    | HTTP       | No                      |
| 5173   | arl-dashboard (Vite)  | HTTP       | No                      |
| 8999   | Scripts (proxy)       | HTTP       | No (local only)         |

## Appendix: Image Registry

| Image                                          | Registry   | Notes                                                          |
|------------------------------------------------|------------|----------------------------------------------------------------|
| `earth4242/ai-gateway`                         | GHCR       | Built from `./api-gateway`                                     |
| `earth4242/ai-worker`                          | GHCR       | Built from `./ai-worker`                                       |
| `earth4242/distributed-rate-limiter`           | GHCR       | Built from `./distributed-rate-limiter`                        |
| `earth4242/ai-dashboard`                       | GHCR       | Built from `./ui`                                              |
| `earth4242/ai-rl-dashboard`                    | GHCR       | Built from `./distributed-rate-limiter/examples/web-dashboard` |
| `earth4242/ai-sidecar`                         | GHCR       | Built from `./api-gateway/sidecar`                             |
| `ghcr.io/dragonflydb/dragonfly:v1.37.2`        | GHCR       | External                                                       |
| `prom/prometheus:v2.54.1`                      | Docker Hub | External                                                       |
| `grafana/grafana:11.3.0`                       | Docker Hub | External                                                       |
| `otel/opentelemetry-collector-contrib:0.112.0` | Docker Hub | External                                                       |
| `caddy:2-alpine`                               | Docker Hub | External                                                       |
| `mcr.microsoft.com/presidio-analyzer:2.2.362`  | MCR        | External                                                       |
