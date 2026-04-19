# System Architecture

> เอกสารอธิบายสถาปัตยกรรมของระบบทุกส่วน
> รวมถึง internal behavior ที่ไม่ได้อยู่ใน MANUAL.md

---

## สารบัญ

1. [ภาพรวม](#1-ภาพรวม)
2. [API Gateway (Go)](#2-api-gateway-go)
3. [Middleware Stack](#3-middleware-stack)
4. [Rate Limit Middleware](#4-rate-limit-middleware)
5. [Security Middleware](#5-security-middleware)
6. [Anomaly Detection](#6-anomaly-detection)
7. [Runtime Metrics](#7-runtime-metrics)
8. [Per-Model Upstream Limiter (Adaptive)](#8-per-model-upstream-limiter-adaptive)
9. [Transparent Proxy](#9-transparent-proxy)
9.5. [Vision Auto-Routing](#95-vision-auto-routing)
9.6. [Profile-Based Routing](#96-profile-based-routing)
9.7. [Quota Enforcement](#97-quota-enforcement)
9.8. [Usage Recording Integration](#98-usage-recording-integration)
9.9. [WebSocket Events (Full List)](#99-websocket-events-full-list)
10. [AI Worker (Python)](#10-ai-worker-python)
11. [Provider Fallback Chain](#11-provider-fallback-chain)
12. [Key Rotation](#12-key-rotation)
13. [Metrics & Observability](#13-metrics--observability)
14. [Data Flow: Queue & Cache](#14-data-flow-queue--cache)
15. [Network & Ports](#15-network--ports)
16. [Resource Limits](#16-resource-limits)
17. [Multi-Agent Use Cases](#17-multi-agent-use-cases)

---

## 1. ภาพรวม

```
┌──────────────────────────────────────────────────────────────────┐
│                                                                  │
│  Client (Claude Code / Agent)                                    │
│    │                                                             │
│    │ POST /v1/messages หรือ POST /v1/chat/completions           │
│    ▼                                                             │
│  arl-gateway (:8080)                                             │
│    ├─ SecurityHeaders Middleware                                 │
│    ├─ CorrelationID Middleware                                   │
│    ├─ RealIP Middleware                                          │
│    ├─ IPFilter Middleware (optional)                              │
│    ├─ Logging Middleware                                         │
│    ├─ Metrics Middleware                                         │
│    ├─ Rate Limit Middleware ──▶ arl-rate-limiter (:8080)        │
│    │                              │                              │
│    │                              └─▶ arl-dragonfly (:6379)     │
│    │                                                             │
│    ├─ Login Limiter (5 attempts/15min per IP, auth endpoints)    │
│    │                                                             │
│    ├─ API Routes:                                                │
│    │   ├─ /v1/messages          (Sync proxy)                     │
│    │   │   ├─ X-Profile header → Profile-based routing           │
│    │   │   ├─ Quota check (>=95% → 429, >=80% → WS warning)     │
│    │   │   └─ Normal: key pool → model slot → proxy              │
│    │   ├─ /v1/chat/completions  (Async queue)                    │
│    │   │   └─ WS event: request-queued                           │
│    │   ├─ /v1/profiles/*        (Profile CRUD)                   │
│    │   ├─ /v1/usage/*           (Usage analytics)                │
│    │   ├─ /quota/*              (Quota tracking)                 │
│    │   ├─ /v1/overview          (Dashboard summary)              │
│    │   ├─ /v1/health/detailed   (6 health checks)                │
│    │   ├─ /v1/config/*          (Config + Thinking + GlobalEnv)  │
│    │   └─ /v1/auth/*            (OAuth + API key auth)           │
│    │                                                             │
│    ├─ Sync mode: Transparent Proxy ──▶ Upstream Provider        │
│    │                                    (17 providers)           │
│    │                                                             │
│    ├─ Async mode: LPUSH job ──▶ arl-dragonfly (queue)          │
│    │                                    │                        │
│    │                              arl-worker (BRPOP x 50)        │
│    │                                ├─ Per-Model Semaphores      │
│    │                                ├─ RPM Limiter               │
│    │                                ├─ Provider Cache             │
│    │                                ├─ Provider Fallback Chain    │
│    │                                ├─ Key Rotation               │
│    │                                └─ Result → arl-dragonfly     │
│    │                                                             │
│    ├─ WebSocket /ws (real-time dashboard updates)                │
│    │   ├─ 6 event types: config-changed, request-completed,      │
│    │   │   request-error, anomaly-detected, request-queued,      │
│    │   │   quota-warning                                         │
│    │   ├─ Config file watcher (.env changes → broadcast)         │
│    │   └─ Session secret persistence (config/session_secret)     │
│    │                                                             │
│    ├─ Static Dashboard SPA (embedded Vite build)                 │
│    │                                                             │
│    └─ Admin routes /admin/* (with optional auth)                 │
│                                                                  │
│  Observability                                                   │
│    arl-otel (:4317/4318) ──▶ arl-prometheus ──▶ arl-grafana    │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

### โหมด Sync vs Async

| | Sync (`/v1/messages`) | Async (`/v1/chat/completions`) |
|---|---|---|
| **ใช้กับ** | Claude Code (real-time) | Batch agents |
| **Flow** | Gateway → Proxy → Upstream → Response | Gateway → Queue → Worker → Cache |
| **Response** | Real-time (SSE streaming) | Request ID → Poll `GET /v1/results/{id}` |
| **Rate limit** | Global + Per API key | Global + Per agent_id |
| **Timeout** | `STREAM_TIMEOUT` (default 300s) | Worker poll timeout 5s |

---

## 2. API Gateway (Go)

### Stack

- **chi/v5** — HTTP router
- **go-redis/v9** — Dragonfly client (connection pool: 50 conns, 10 min idle)
- **prometheus/client_golang** — Metrics
- **OpenTelemetry SDK** — Distributed tracing

### Startup Sequence (`main.go`)

```
1. Load config from env vars
2. Init OTel tracer (-> arl-otel:4317) -- if unavailable, warn and continue
3. Connect Dragonfly (ping test) -- if unavailable, exit
4. Init Prometheus metrics (custom registry, 21 metrics)
5. Init RuntimeMetrics (goroutines, heap, GC, Dragonfly health -- 10s interval)
6. Init AnomalyDetector (z-score ring buffer, 1000 samples)
7. Create AnthropicProxy (transparent)
8. Create AdaptiveLimiter (series buckets, EWMA, signal-based waiting)
9. Create KeyPool
10. Create Handler
11. Init Provider Registry (17 providers, 4 auth types)
12. Init TokenStore (Dragonfly-backed OAuth token persistence)
13. Init AuthHandler (OAuth device code + auth code + API key flows)
14. Init ProfileHandler, UsageHandler, QuotaHandler, OverviewHandler, ConfigHandler
15. Init RefreshWorker (background OAuth token refresh)
16. Init WebSocketHub for real-time dashboard updates
17. Load/generate session secret (persisted to config/session_secret)
18. Start session secret file watcher (fsnotify)
19. Start config file watcher (.env changes -> WS broadcast)
20. Start Z.AI key sync from TokenStore into KeyPool (30s interval)
21. Setup chi router with middleware stack:
    a. SecurityHeaders (X-Content-Type-Options, X-Frame-Options, X-XSS-Protection, Referrer-Policy)
    b. CorrelationID (generate/propagate X-Correlation-ID)
    c. RealIP (extract from CF-Connecting-IP, X-Real-IP, X-Forwarded-For)
    d. IPFilter (optional CIDR whitelist/blacklist)
    e. Logging (structured JSON)
    f. Metrics (latency + active connections + status tracking)
    g. Rate Limiter (global + per-agent, fail-open)
    h. Login Limiter (available for auth endpoints, 5 attempts/15min per IP)
22. Register all routes (messages, profiles, usage, quota, overview, config, WebSocket)
23. Start periodic adaptive limiter metrics export (10s interval)
24. Start HTTP server (WriteTimeout=0 for SSE)
25. Graceful shutdown on SIGINT/SIGTERM (10s timeout)
```

### Routes

| Method | Path | Handler | Mode |
|--------|------|---------|------|
| `POST` | `/v1/messages` | `Messages` | Sync (transparent proxy) |
| `POST` | `/v1/chat/completions` | `ChatCompletions` | Async (enqueue) |
| `GET` | `/v1/results/{requestID}` | `GetResult` | Async (poll result) |
| `GET` | `/health` | `Health` | Health check |
| `GET` | `/v1/limiter-status` | `LimiterStatus` | Adaptive limiter state (auth required) |
| `POST` | `/v1/limiter-override` | `LimiterOverride` | Set/clear manual override |
| `GET` | `/v1/routing/strategy` | `GetRoutingStrategy` | Current routing strategy |
| `PUT` | `/v1/routing/strategy` | `SetRoutingStrategy` | Update routing strategy |
| `GET` | `/v1/logs/errors` | `GetErrorLogs` | Recent error log entries |
| `GET` | `/v1/logs/errors/count` | `GetErrorLogCount` | Error log count |
| `GET` | `/v1/models` | `GetModels` | Known models list |
| `GET` | `/metrics` | Prometheus | Metrics scrape (custom registry) |
| `GET` | `/api/metrics` | Prometheus | Metrics scrape (alias) |

#### Profile Management (`handler/profile.go`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/profiles` | List all profiles |
| `POST` | `/v1/profiles` | Create profile (409 if exists) |
| `GET` | `/v1/profiles/{name}` | Get profile by name |
| `PUT` | `/v1/profiles/{name}` | Update profile (preserves createdAt) |
| `DELETE` | `/v1/profiles/{name}` | Delete profile |
| `POST` | `/v1/profiles/{name}/copy` | Copy profile (body: `{"destination":"new-name"}`) |
| `POST` | `/v1/profiles/{name}/export` | Export profile (body: `{"includeSecrets":false}`) |
| `POST` | `/v1/profiles/import` | Import profile from bundle |

Profiles stored in Dragonfly as `profile:{name}` keys. Each profile contains: name, baseUrl, apiKey, model, target (claude/droid/codex), provider. Export redacts API keys by default (`__CCS_REDACTED__`).

#### Usage Analytics (`handler/usage.go`)

| Method | Path | Query Params | Description |
|--------|------|-------------|-------------|
| `GET` | `/v1/usage/summary` | `period=24h|7d|30d|all` | Aggregated totals |
| `GET` | `/v1/usage/hourly` | `hours=24|48` | Hourly breakdown |
| `GET` | `/v1/usage/daily` | - | Last 30 days |
| `GET` | `/v1/usage/monthly` | - | Last 12 months |
| `GET` | `/v1/usage/models` | `period=24h|7d|30d` | Per-model breakdown |
| `GET` | `/v1/usage/sessions` | `days=1-30` | Session-level (daily) usage |

Data stored in Redis hashes: `usage:hourly:YYYY-MM-DDTHH`, `usage:daily:YYYY-MM-DD`, `usage:monthly:YYYY-MM`, `usage:sessions:YYYY-MM-DD`. TTLs: hourly 48h, daily 35d, monthly 400d.

#### Quota Tracking (`handler/quota.go`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/quota/{provider}/{accountId}` | Per-account quota (30s Redis cache) |
| `GET` | `/quota/{provider}` | All accounts for a provider |

Supported providers: `claude`/`anthropic`, `gemini`/`gemini-oauth`. Returns per-model quota percentages and reset times. Falls back to stub for unsupported providers.

#### Dashboard Overview & Health (`handler/overview.go`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/overview` | Dashboard summary (profiles, accounts, providers, keys, queue, health, uptime) |
| `GET` | `/v1/health/detailed` | 6 health checks: dragonfly, rate-limiter, prometheus, key-pool, upstream, memory |
| `POST` | `/v1/health/fix/{checkId}` | Auto-fix hint for a failed check |

Health check categories: connectivity (dragonfly, rate-limiter), resources (prometheus, memory), config (key-pool), upstream. Overall status: healthy / degraded / unhealthy.

#### Server Config (`handler/config.go`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/config` | Current config (secrets redacted) |
| `GET` | `/v1/config/raw` | Config as plain text (secrets redacted) |
| `PUT` | `/v1/config` | Merge config overrides (preserve `[redacted]` values) |
| `GET` | `/v1/thinking` | Thinking budget config (defaultBudget, modelBudgets, enabled) |
| `PUT` | `/v1/thinking` | Update thinking config |
| `GET` | `/v1/global-env` | Global env vars (sensitive keys redacted) |
| `PUT` | `/v1/global-env` | Update global env vars |

Overrides stored in Redis: `config:overrides`, `config:thinking`, `config:global-env`. Sensitive key detection: keys containing "key", "secret", "token", "password".

#### WebSocket (`handler/websocket.go`)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/ws` | WebSocket endpoint for real-time dashboard updates |

Hub-based broadcast: all connected clients receive events. Ping/pong keepalive (54s ping period, 60s pong deadline). Events: `config-changed` (from .env watcher), `request-completed`, `request-error`, `anomaly-detected`, `request-queued`, `quota-warning`. Used by UI via `use-websocket.ts` hook with exponential backoff reconnect. Full event list in [Section 9.9](#99-websocket-events-full-list).

### Config (`config/config.go`)

| Env Var | Default | Description |
|---------|---------|-------------|
| `SERVER_PORT` | `:8080` | Listen address |
| `REDIS_ADDR` | `dragonfly:6379` | Dragonfly address |
| `RATE_LIMITER_ADDR` | `http://rate-limiter:8080` | Rate limiter address |
| `QUEUE_NAME` | `ai_jobs` | Dragonfly list name |
| `GLOBAL_RATE_LIMIT` | `100` | Global rate limit (req/min) |
| `AGENT_RATE_LIMIT` | `5` | Per-agent rate limit (req/min) |
| `WORKER_POOL_SIZE` | `100` | Goroutine pool for async mode |
| `UPSTREAM_URL` | `https://api.z.ai/api/anthropic` | Upstream AI provider |
| `STREAM_TIMEOUT` | `300s` | Timeout for streaming proxy |
| `UPSTREAM_MODEL_LIMITS` | `glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3,glm-4.5:10` | Per-model concurrent limits (19 slots) |
| `UPSTREAM_DEFAULT_LIMIT` | `1` | Default limit for unconfigured models |
| `UPSTREAM_GLOBAL_LIMIT` | `9` | Total concurrent across all models (0=unlimited) |
| `UPSTREAM_PROBE_MULTIPLIER` | `5` | Adaptive probe ceiling multiplier (initial * this = maxLimit) |
| `UPSTREAM_MAX_RETRIES` | `3` | Max retry attempts on upstream 429/503 |
| `UPSTREAM_RETRY_BACKOFF` | `500ms` | Base backoff between retries (quadratic: base * attempt^2, capped at 5min) |
| `UPSTREAM_API_KEYS` | | Upstream API keys (replaces `GLM_API_KEYS`/`GLM_ENDPOINT` for sync proxy) |
| `READ_TIMEOUT` | `5s` | HTTP read timeout |
| `OTLP_ENDPOINT` | `otel-collector:4317` | OTel collector |
| `REDIS_POOL_SIZE` | `50` | Connection pool size |
| `REDIS_MIN_IDLE_CONNS` | `10` | Minimum idle connections |

### Dragonfly Client (`queue/dragonfly.go`)

Connection pool tuning:
- `PoolSize: 50` — 50 connections per CPU
- `MinIdleConns: 10` — ค้างไว้ 10 connections ตลอดเวลา
- `ConnMaxIdleTime: 5m` — idle connection ค้างได้สูงสุด 5 นาที
- `ConnMaxLifetime: 30m` — connection มีอายุสูงสุด 30 นาที
- `DialTimeout: 3s`, `ReadTimeout: 3s`, `WriteTimeout: 3s`

Operations:
- `PushJob` — `LPUSH` job JSON เข้า queue
- `GetResult` — `GET result:{requestID}` จาก cache
- `SetResult` — `SET result:{requestID}` ลง cache พร้อม TTL
- `QueueDepth` — `LLEN` ดูความยาว queue

---

## 3. Rate Limit Middleware

### ที่ตั้งค่า (`middleware/ratelimit.go`)

Rate limit middleware ทำงาน 2 ระดับ:

```
Request เข้ามา
  │
  ├─ 1. Global rate limit check (key = "global")
  │     └─ POST /api/ratelimit/check {"key": "global", "tokens": 1}
  │
  ├─ 2. Per-agent rate limit check
  │     ├─ /v1/messages: ใช้ x-api-key หรือ Authorization: Bearer
  │     └─ อื่นๆ: ใช้ ?agent_id= หรือ URL param
  │     └─ POST /api/ratelimit/check {"key": "agent:<api-key>", "tokens": 1}
  │
  └─ ผ่านทั้งสองระดับ → ส่งต่อไป handler
```

### Fail-Open Behavior

**ถ้า rate limiter service ไม่ตอบหรือ error → request ผ่านเลย (fail-open)**

```go
// ทุก error case → return true (อนุญาต)
if err != nil {
    slog.Error("rate-limiter service unreachable, failing open")
    return true
}
```

เหตุผล: ไม่อยากให้ rate limiter เป็น single point of failure — ถ้า rate limiter ล่ม ระบบยังทำงานได้

### Timeout

- Rate limit check timeout: **3 วินาที** (context timeout)
- HTTP client timeout: **2 วินาที**

### Error Format

สำหรับ `/v1/messages` — ตอบเป็น Anthropic format:
```json
{
  "type": "error",
  "error": {
    "type": "rate_limit_error",
    "message": "Rate limit exceeded. Please retry after 1 seconds."
  }
}
```

สำหรับ route อื่น — ตอบเป็น plain JSON:
```json
{"error": "global rate limit exceeded", "retry_after": 1}
```

---

## 5. Security Middleware

### SecurityHeaders, CorrelationID, RealIP, IPFilter (`middleware/security.go`)

#### SecurityHeaders

เพิ่ม HTTP security headers ทุก response:

| Header | Value |
|--------|-------|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `X-XSS-Protection` | `1; mode=block` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Cache-Control` | `no-store` (เฉพาะ `/v1/*` paths) |

#### CorrelationID

สร้างหรือ propagate `X-Correlation-ID` สำหรับ distributed tracing:

```
Request เข้ามา
  ├─ มี X-Correlation-ID header? → ใช้ค่าเดิม
  └─ ไม่มี? → สร้าง UUID v4 ใหม่
  → เซ็ต header ใน response และ context
```

#### RealIP

ดึง real client IP จาก proxy headers (ใช้สำหรับ IPFilter + logging):

```
Priority: CF-Connecting-IP > X-Real-IP > X-Forwarded-For (first IP) > RemoteAddr
```

เก็บ IP ใน request context ผ่าน `GetRealIP(ctx)`.

#### IPFilter (CIDR whitelist/blacklist)

Middleware สำหรับบล็อก/อนุญาต IP:

```go
IPFilterConfig{
    Whitelist: []string{"10.0.0.0/8", "192.168.0.0/16"},
    Blacklist: []string{"203.0.113.0/24"},
}
```

- Whitelist ไม่ว่าง = เฉพาะ IP ที่ match ถึงจะผ่าน
- Blacklist = block IP ที่ match
- ใช้ `GetRealIP()` context ดึง IP (เชื่อมกับ RealIP middleware)
- Response: `403 {"error": "access denied"}`

---

## 5.5. Login Rate Limiter

### Login Limiter (`middleware/login_limiter.go`)

Per-IP rate limiter for login/auth endpoints. 5 attempts per 15-minute window.

```
Request to login endpoint
  |
  +-- Extract IP from RemoteAddr
  |
  +-- Check attempt counter for this IP:
      |
      +-- No record or expired -> allow, start new window
      +-- count < 5 -> allow, increment counter
      +-- count >= 5 -> 429 {"error":"too many login attempts"}
                       Retry-After: 900
```

Background cleanup: expired entries removed every 5 minutes.

Usage: wrap auth endpoints with `middleware.NewLoginLimiter()`.

---

## 5.6. Session Secret Persistence

### Session Secret (`middleware/session_secret.go`)

Session cookie signing secret persisted to `config/session_secret` file. Survives gateway restarts.

```
Startup:
  |-- File exists and >= 32 bytes -> load from file
  |-- File missing or too short -> generate 64-byte hex secret
  |   |-- mkdir config/ (0700)
  |   |-- write to config/session_secret (0600)
  |
  |-- Watch with fsnotify:
      |-- Write/Create event -> reload secret from file
      |-- Allows secret rotation without restart
```

File permissions: directory 0700, file 0600 (owner-only read/write).

---

## 5.7. Config File Watcher

### Config Watcher (`middleware/config_watcher.go`)

Watches `.env` file for changes using fsnotify. On change, calls callback with changed key/value. Used to broadcast `config-changed` events via WebSocket.

```
NewConfigWatcher(".env", callback)
  |
  +-- fsnotify watches parent directory
  |
  +-- On Write event to .env:
      |-- Debounce 500ms (avoid duplicate events)
      |-- Parse new .env -> compare with previous values
      |-- For each changed key: callback(key, newValue)
      |-- wsHub.Broadcast("config-changed", {key})
```

Runs in background goroutine, stops on context cancellation.

---

## 5.8. WebSocket Hub

### WebSocket (`handler/websocket.go`)

Real-time event broadcast to connected dashboard clients.

```
Client connects GET /ws
  |
  +-- Upgrade to WebSocket (gorilla/websocket, CheckOrigin: allow all)
  +-- Register in WebSocketHub
  +-- Start readPump goroutine (pong handler, read limit 512 bytes)
  +-- Start writePump goroutine (ping every 54s, write deadline 10s)
  |
  +-- On broadcast:
      |-- Hub locks, iterates all clients
      |-- Non-blocking send to client channel (buffer 256)
      |-- If channel full -> close and unregister client

Event format:
  {"type":"config-changed","data":{"key":"SOME_VAR"},"timestamp":"2026-04-19T12:00:00Z"}
```

Ping/pong: 54s ping period, 60s pong deadline, 10s write deadline.

---

## 6. Anomaly Detection

### ที่ตั้งค่า (`middleware/anomaly.go`)

Z-score anomaly detector สำหรับ detect rate anomalies ใน metrics stream:

### Algorithm

```
Ring buffer: 1000 samples
Baseline: rolling mean + stddev

On new sample:
  1. เพิ่ม sample เข้า ring buffer
  2. คำนวณ mean, stddev จาก buffer
  3. คำนวณ z-score: z = (value - mean) / stddev
  4. Classify:
     ├─ z > 2.0  → AnomalySpike (high streak counter++)
     ├─ z < -2.0 → AnomalyDrop (low streak counter++)
     └─ else     → reset streaks, return AnomalyNone

  5. Sustained detection (override spike/drop):
     ├─ highStreak >= 5  → AnomalySustainedHigh
     └─ lowStreak >= 5   → AnomalySustainedLow

  6. Severity:
     ├─ |z| > 4.0 → SeverityCritical
     ├─ |z| > 3.0 → SeverityHigh
     └─ else      → SeverityMedium
```

### Prometheus Metric

```
api_gateway_anomaly_total{type="spike|drop|sustained_high|sustained_low", severity="low|medium|high|critical"}
```

### Warm-up

ต้องมีอย่างน้อย 10 samples ก่อนจะเริ่ม detect -- ก่อนนั้น return `AnomalyNone`.

---

## 7. Runtime Metrics

### ที่ตั้งค่า (`middleware/runtime_metrics.go`)

Background collector ที่รันทุก 10 วินาที เก็บ Go runtime metrics + Dragonfly health:

### Metrics Collected

| Metric | Type | คำอธิบาย |
|--------|------|----------|
| `api_gateway_go_goroutines` | Gauge | จำนวน goroutines ปัจจุบัน |
| `api_gateway_go_heap_alloc_bytes` | Gauge | Heap allocation ปัจจุบัน (bytes) |
| `api_gateway_go_heap_objects` | Gauge | จำนวน heap objects |
| `api_gateway_go_gc_pause_ns` | Gauge | GC pause ของรอบล่าสุด (nanoseconds) |
| `api_gateway_go_stack_inuse_bytes` | Gauge | Stack ใช้งานปัจจุบัน (bytes) |
| `api_gateway_dragonfly_up` | Gauge | Dragonfly health (1=healthy, 0=down) |

### Dragonfly Health Check

```
ทุก 10 วินาที:
  ├─ Dragonfly Ping → OK → dragonfly_up = 1
  └─ Dragonfly Ping → Error → dragonfly_up = 0
```

ใช้ `go-redis` client เชื่อมต่อแยกจาก main Dragonfly client (connection pool ของ runtime metrics).

---

## 8. Per-Model Upstream Limiter (Adaptive)

### Architecture (`middleware/adaptive_limiter.go`)

Adaptive concurrency limiter with automatic limit discovery. Starts at configured initial limits,
probes upward based on upstream feedback (success rate, latency), and backs off on 429/503 errors.

Inspired by Envoy gradient controller + Netflix concurrency limits.

### Key Features

- **Series-based routing**: Models grouped by major version (glm-5.x = series 5, glm-4.x = series 4)
- **Same-series round-robin**: When requested model is full, distribute within same series first
- **Series spillover**: Only fall to lower series under latency pressure (EWMA > 1.5x minRTT)
- **Signal-based waiting**: `sync.Cond` replaces spin-wait for slot availability
- **RTT EWMA tracking**: Per-model exponentially weighted moving average (alpha=0.3)
- **sync.Pool optimization**: Pooled candidate slices reduce GC pressure
- **Manual overrides**: `SetOverride(model, limit)` pins a model's limit (0 = clear)
- **Learned ceiling**: Remembers peak limit before 429, decays after 5 minutes

### Adaptive Algorithm

```
On 429/503:
  peakBefore429 = current limit     (remember the ceiling that caused 429)
  limit_new = max(minLimit, limit * 0.5)  (multiplicative decrease x0.5)

On 200:
  Update minRTT (CAS loop — keep lowest ever)
  Update RTT EWMA (alpha=0.3, CAS loop):
    ewma_new = ewma_old * 0.7 + sampleRTT * 0.3
  Skip if model has manual override (still track RTT/stats)
  Cooldown: 5s after any 429 before increasing again
  Probe: every 5 consecutive successes
  gradient = (minRTT + buffer) / sampleRTT   (clamped 0.8-2.0)
  limit_new = min(maxLimit, gradient * limit + sqrt(limit))
  if newLimit >= peakBefore429 → cap at peakBefore429 - 1  (don't re-probe learned ceiling)
  Learned ceiling decay: peakBefore429 resets to 0 after 5 minutes
```

### Series-Based Fallback

```
Request: { "model": "glm-5", ... }
  │
  ├─ 1. Wait for global slot (sync.Cond signal-based)
  │
  ├─ 2. Try glm-5 (non-blocking CAS acquire)
  │     └─ Available? → use glm-5
  │
  ├─ 3. glm-5 full → round-robin within same series (series 5):
  │     ├─ glm-5.1, glm-5-turbo, glm-5 (pooled candidate slices)
  │     └─ Any available? → use it (same-series round-robin)
  │
  ├─ 4. Same series full → check latency pressure:
  │     ├─ EWMA > 1.5x minRTT for majority of series 5 models? → PRESSURE
  │     └─ No pressure → no spillover (stay in series 5)
  │
  ├─ 5. Spillover to lower series (series 4) under pressure:
  │     ├─ glm-4.7, glm-4.6, glm-4.5 (round-robin)
  │     └─ Any available? → use it
  │
  └─ 6. All models full → release global slot → signal-based block-wait (30s timeout)
                              → re-acquire global slot after model slot obtained
```

**Series spillover trade-off**: Only spills to lower series when the current series shows latency pressure (EWMA RTT > 1.5x minRTT for majority of models). This prevents unnecessary downgrades when a model is temporarily full but not under load.

**Global slot starvation prevention**: When all models are full and `Acquire()` needs to block-wait on the requested model, it first **releases the global slot** before blocking, then **re-acquires** it after obtaining a model slot. Signal-based waiting (`sync.Cond`) replaces spin-wait polling.

### Signal-Based Waiting

```
Old (spin-wait):  for { if tryAcquire() { break }; time.Sleep(100ms) }
New (sync.Cond):  cond.L.Lock(); for !available { cond.Wait() }; cond.L.Unlock()

Benefits:
  - Zero CPU usage while waiting (blocked in kernel, not polling)
  - Instant wake-up on Signal() (no 100ms polling delay)
  - No goroutine leak: timeout goroutine cleaned up via channel
```

### sync.Pool Optimization

```go
// Pool candidate slices to reduce GC pressure during fallback evaluation
candPool: sync.Pool{ New: func() any { s := make([]seriesEntry, 0, 8); return &s } }

// Usage: getCandidates() gets from pool, putCandidates() returns to pool
// Eliminates allocation per Acquire() call on the hot path
```

### RTT EWMA Tracking

```
Per-model RTT EWMA (alpha = 0.3):
  ewma = ewma * 0.7 + sampleRTT * 0.3

Uses:
  - Series latency pressure detection (EWMA > 1.5x minRTT)
  - Exposed in /v1/limiter-status as ewma_rtt_ms
  - Helps adaptive algorithm understand real load vs transient spikes
```

### Manual Override API

```
SetOverride(model, limit):
  ├─ limit > 0: pin model's limit to exact value (bypass adaptive)
  ├─ limit = 0: clear override (resume adaptive)
  └─ Applied immediately + logged

Overrides(): returns current override state (map[string]int64)

Feedback() still tracks RTT/stats when override is active, but doesn't change limit.
Visible in /v1/limiter-status as "overridden": true field.
```

### Probe Multiplier (Configurable)

```env
# How far above initial limit to probe (default: 5x)
# e.g. initial=1, multiplier=5 → maxLimit=5
# Set higher if real upstream limit may be higher than documented
UPSTREAM_PROBE_MULTIPLIER=5
```

The system discovers the real upstream limit automatically:
- Starts at initial limit (e.g., 1 for glm-5.1)
- Probes upward every 5 consecutive successes
- When 429 hits at limit N, remembers N as `peakBefore429`
- Future probes cap at `peakBefore429 - 1` (learned ceiling)
- Visible in `/v1/limiter-status` as `learned_ceiling` field

```
Example: real upstream limit = 4, initial = 1, probeMultiplier = 5

Step 1: limit=1 → success x5 → limit=2
Step 2: limit=2 → success x5 → limit=3
Step 3: limit=3 → success x5 → limit=4
Step 4: limit=4 → success x5 → limit=5
Step 5: limit=5 → 429! → peakBefore429=5, limit=2 (halved)
Step 6: limit=2 → success x5 → limit=3
Step 7: limit=3 → success x5 → limit=4 → cap at peak-1=4
→ Converged at 4 (the real limit)
```

### Configuration

```env
# Per-model limits (model:concurrent comma-separated)
UPSTREAM_MODEL_LIMITS=glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3,glm-4.5:10

# Default limit for models not in the list
UPSTREAM_DEFAULT_LIMIT=1

# Total concurrent across all models (must be > 0)
UPSTREAM_GLOBAL_LIMIT=9

# Probe multiplier for adaptive limit discovery
UPSTREAM_PROBE_MULTIPLIER=5
```

### Example

```
Models configured: glm-5.1:1, glm-5-turbo:1, glm-5:2, glm-4.7:2, glm-4.6:3, glm-4.5:10
Total model capacity: 1+1+2+2+3+10 = 19
Global cap: 9 concurrent

Series grouping:
  Series 5: glm-5.1(1), glm-5-turbo(1), glm-5(2) = 4 slots
  Series 4: glm-4.7(2), glm-4.6(3), glm-4.5(10) = 15 slots

Selection: Round-robin within same series, spillover to lower series under pressure

Low load example (5 requests for glm-5):
  req 1 → glm-5 slot (limit 2)         ✅ direct
  req 2 → glm-5 slot                    ✅ direct
  req 3 → glm-5.1 (same-series RR)      ✅ series 5 round-robin
  req 4 → glm-5-turbo (same-series RR)  ✅ series 5 round-robin
  req 5 → glm-4.7 (series spillover)    ✅ series 5 full + latency pressure → series 4
```

### Body Rewrite

When fallback occurs, the gateway replaces the `"model"` field in the JSON body:

```go
// Before: {"model":"glm-5.1","messages":[...],"stream":true}
// After:  {"model":"glm-5","messages":[...],"stream":true}
```

This is the only modification to the request body — all other fields (tools, messages, etc.) remain untouched.

### Log Output

```json
{"msg":"adaptive model configured","model":"glm-5.1","initial_limit":1,"max_limit":5}
{"msg":"adaptive limiter initialized","models":["glm-5.1","glm-5-turbo","glm-5","glm-4.7","glm-4.6","glm-4.5"],"global_limit":9}
{"msg":"model fallback (same-series round-robin)","requested":"glm-5","selected":"glm-5.1","series":5}
{"msg":"series spillover (latency pressure)","requested":"glm-5","selected":"glm-4.7","from_series":5,"to_series":4}
{"msg":"all models full, waiting","requested":"glm-5"}
{"msg":"adaptive limit increased","model":"glm-5","old":2,"new":3,"successes":5}
{"msg":"adaptive limit decreased after 429/503","model":"glm-5","old":3,"new":1}
{"msg":"adaptive override set","model":"glm-5","limit":5}
{"msg":"adaptive override cleared","model":"glm-5"}
{"msg":"learned ceiling decayed, allowing re-probe","model":"glm-5","old_peak":5}
{"msg":"rpm limiter waiting","provider":"glm","wait_seconds":60.1}
```

---

## 5. Transparent Proxy

ดูรายละเอียดเต็มที่ [docs/claude-code-proxy.md](claude-code-proxy.md)

### Key Points

- ไม่ decode/re-encode request body
- ไม่ decode/re-encode response body
- SSE streaming: relay chunk by chunk พร้อม flush ทุก chunk
- Token tracking: parse `usage` from response for Prometheus metrics (input/output by model)
- TTFB tracking: record time-to-first-byte for streaming responses (`api_gateway_ttfb_seconds`)
- Response header filtering: only safe headers forwarded (prevent header injection)
- Headers ส่งตรง: `Content-Type`, `x-api-key`, `anthropic-version`
- Status code ส่งตรง (429 ไม่แปลงเป็น 502)

### Key Pool (Gateway-managed upstream keys)

The gateway can manage a pool of upstream API keys (`UPSTREAM_API_KEYS`) with per-key RPM tracking and automatic cooldown on 429/overloaded errors.

**Key pool RPM leak prevention**: `keyPool.Acquire()` is called **after** body validation and JSON parsing in the Messages handler. This ensures RPM budget is not wasted on malformed or oversized requests.

```
Messages handler order (correct):
  1. Read body (with 10MB limit check)
  2. Parse JSON (validate structure)
  3. Resolve API key from pool ← only after validation
  4. Acquire model slot (may fallback)
  5. Proxy request upstream
```

### Context-Aware Retry Backoff

Upstream retry backoff uses `select` with `r.Context().Done()` instead of `time.Sleep`, so cancelled requests (e.g., client disconnect) abort immediately instead of waiting through the full backoff duration:

```go
select {
case <-time.After(backoff):
    // proceed with retry
case <-r.Context().Done():
    // client disconnected, abort
}
```

### HTTP Client Tuning (`proxy/anthropic.go`)

```go
&http.Client{
    Timeout: 0, // ไม่มี global timeout — ควบคุม per-request
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 100,
        IdleConnTimeout:     90 * time.Second,
    },
}
```

### Stream Buffer

- อ่านทีละ 8192 bytes (8KB)
- Flush ทุกครั้งที่อ่านได้ข้อมูล
- ไม่มี buffer limit — ใช้ `io.Copy` สำหรับ non-streaming

---

## 9.5. Vision Auto-Routing

### Overview

Gateway ตรวจจับ image content ใน request อัตโนมัติ แล้ว route ไป native Zhipu vision endpoint แทน z.ai Anthropic endpoint เพราะ z.ai ไม่สามารถ decode base64 image ผ่าน Anthropic-compatible format ได้ พร้อม **auto-select vision model** ตาม image payload size/count และ **SSE streaming conversion** จาก Zhipu format เป็น Anthropic format

### Dual-Path Architecture

```
Client POST /v1/messages
  |
  v
arl-gateway (:8080)
  |
  |-- parse body, resolve key, acquire model slot
  |
  |-- filterUnsupportedContent():
  |     strip server_tool_use blocks
  |     convert Anthropic image -> GLM image_url format
  |
  |-- HasImageContent() scan messages?
  |
  +-- NO images:
  |     ProxyTransparent()
  |       -> POST UPSTREAM_URL (api.z.ai/api/anthropic)
  |       <- raw response relay (SSE support)
  |
  +-- HAS images:
        |-- analyzeImagePayload() -> totalBase64Bytes, imageCount
        |-- selectVisionModel():
        |     score = totalBase64KB + (imageCount * 300)
        |     score <= 2000 && count < 3 -> glm-4.6v (10 slots)
        |     score > 2000 || count >= 3 -> glm-4.6v-flashx (3 slots)
        |
        ProxyNativeVision()
          -> anthropicToZhipu() format conversion
          -> POST NATIVE_VISION_URL (open.bigmodel.cn/api/paas/v4/chat/completions)
          <- stream=true?
             YES: convertZhipuStreamResponse()
                  Zhipu SSE -> Anthropic SSE events (real-time)
                  message_start -> content_block_start -> content_block_delta...
                  -> content_block_stop -> message_delta -> message_stop
             NO:  zhipuToAnthropic()
                  Zhipu JSON -> Anthropic JSON response
          <- response to client (Anthropic format)
```

### Format Conversion (Anthropic <-> Zhipu)

```
anthropicToZhipu():
  ┌──────────────────────────────────────────────────────────────┐
  │ Anthropic Messages API        ->    Zhipu OpenAI API         │
  │                                                              │
  │ messages[].role: "user"       ->    messages[].role: "user"  │
  │ messages[].role: "assistant"  ->    messages[].role: "assis" │
  │ messages[].content (array)    ->    messages[].content (str) │
  │                                                              │
  │ content[].type="text"         ->    text string              │
  │ content[].type="image"        ->    type="image_url"         │
  │   source.type="base64"        ->      url="data:mime;base64" │
  │   source.type="url"           ->      url=<original url>     │
  │ content[].type="tool_use"     ->    tool_calls[]             │
  │ content[].type="tool_result"  ->    text (converted)         │
  │                                                              │
  │ system (string or array)      ->    system (string)          │
  │ tools[]                       ->    tools[]                  │
  │ stream: bool                  ->    stream: bool             │
  └──────────────────────────────────────────────────────────────┘

zhipuToAnthropic():
  ┌──────────────────────────────────────────────────────────────┐
  │ Zhipu Response                ->    Anthropic Response       │
  │                                                              │
  │ choices[0].message.content    ->    content[] array          │
  │   (text string)               ->      [{type:"text",text:..}]│
  │   (tool_calls[])              ->      [{type:"tool_use",...}]│
  │ usage.prompt_tokens           ->    usage.input_tokens       │
  │ usage.completion_tokens       ->    usage.output_tokens      │
  │ finish_reason: "stop"         ->    stop_reason: "end_turn"  │
  │ finish_reason: "tool_calls"   ->    stop_reason: "tool_use"  │
  └──────────────────────────────────────────────────────────────┘
```

### Content Filtering

Before routing, `filterUnsupportedContent()` processes all messages:

1. Strip `server_tool_use` blocks (GLM does not support this type)
2. Convert Anthropic image format to GLM-compatible `image_url` format:
   ```
   Before: {"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}}
   After:  {"type":"image_url","image_url":{"url":"data:image/png;base64,..."}}
   ```

### Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `NATIVE_VISION_URL` | `https://open.bigmodel.cn/api/paas/v4/chat/completions` | Zhipu native vision endpoint |

### Vision Models

| Model | Slots | Status | Notes |
|-------|--------|--------|-------|
| glm-4.6v | 10 | Available | Recommended, default for most requests |
| glm-4.5v | 10 | Available | Good quality, same capacity |
| glm-4.6v-flashx | 3 | Available | Auto-selected for heavy payloads (score > 2000 or count >= 3) |
| glm-4.6v-flash | 1 | Available | Fast, not auto-selected (limited capacity) |

### Vision Model Auto-Select

Gateway analyzes image payload and selects optimal vision model:

```
analyzeImagePayload(payload) -> (totalBase64Bytes, imageCount)
selectVisionModel(totalBytes, imageCount):
  score = totalBase64KB + (imageCount * 300)
  if score > 2000 or imageCount >= 3:
    return "glm-4.6v-flashx"   // 3 slots, fastest for heavy payloads
  return "glm-4.6v"            // 10 slots, best quality for normal payloads
```

### SSE Streaming Conversion

Vision requests with `stream: true` get real-time SSE streaming. Zhipu SSE chunks (OpenAI format) are converted to Anthropic SSE events:

```
Zhipu SSE (input):
  data: {"choices":[{"delta":{"content":"Hello"}}]}
  data: {"choices":[{"delta":{"reasoning_content":"Let me..."}}]}

Anthropic SSE (output):
  event: content_block_delta
  data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}

  event: content_block_delta
  data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"Let me..."}}
```

Full event sequence: `message_start` -> `content_block_start` -> `content_block_delta`(x N) -> `content_block_stop` -> `message_delta` -> `message_stop`

### Limitations

- Privacy pipeline is skipped for vision path
- tool_use on vision requests may not work depending on upstream model
- No automatic image resizing

---

## 9.6. Profile-Based Routing

### Overview

The `X-Profile` header enables per-request profile-based routing. When present, the gateway loads the named profile from Redis and overrides the model, apiKey, and baseUrl for that request. If the profile is not found, the request falls through to normal routing.

### Flow

```
Request with X-Profile: my-profile
  |
  v
Handler.Messages()
  |-- Extract X-Profile header
  |-- Profile name non-empty?
  |     |
  |     +-- YES: Lookup profile:{name} from Redis
  |     |     |
  |     |     +-- Found: Override model, apiKey, baseUrl from profile
  |     |     |         Skip model fallback logic (profile model is used directly)
  |     |     |         Skip key pool acquire (profile has its own apiKey)
  |     |     |         Proxy to profile.baseUrl with profile.apiKey
  |     |     |
  |     |     +-- Not found: Log warning, fall through to normal routing
  |     |
  |     +-- NO: Normal routing (key pool + adaptive limiter)
```

### Profile Fields Used for Routing

| Field | Override Target |
|-------|----------------|
| `model` | Replaces requested model in body |
| `apiKey` | Used as upstream API key (bypasses key pool) |
| `baseUrl` | Replaces UPSTREAM_URL for this request |

### Handler Struct Expansion

The `Handler` struct now holds these additional fields:
- `usageHandler` -- usage analytics recorder
- `quotaHandler` -- quota enforcement checker
- `profileRedis` -- dedicated `redis.Client` for profile lookups
- `wsBroadcast` -- callback function for WebSocket event broadcasting

Constructor accepts 4 new parameters to inject these dependencies.

**File**: `handler/handler.go` lines ~260-275

---

## 9.7. Quota Enforcement

### Overview

Quota enforcement is now wired into the sync proxy request flow. Before acquiring a model slot in `Messages()`, the handler checks whether the requested model is at or above quota. Returns HTTP 429 if at >= 95% quota. Broadcasts a `quota-warning` via WebSocket at >= 80%.

### Flow

```
Messages() handler
  |-- Resolve model from request (or profile)
  |-- CheckQuota(provider, accountID, model)
  |     |
  |     +-- >= 95% quota → return 429 (Anthropic rate_limit_error format)
  |     +-- >= 80% quota → broadcast quota-warning via WebSocket
  |     |                  continue processing (soft warning)
  |     +-- < 80% → continue normally
  |     +-- Error → fail-open (log warning, continue processing)
  |
  |-- Acquire model slot (adaptive limiter)
  |-- Proxy upstream
```

### Behavior

- **Fail-open**: If `CheckQuota()` encounters an error (Redis down, etc.), the request proceeds normally
- **429 format**: Returns Anthropic-compatible error format for `/v1/messages`
- **Soft warning**: At 80-95% quota, a `quota-warning` WebSocket event is broadcast but the request still proceeds
- **Hard block**: At >= 95% quota, the request is rejected with 429

**File**: `handler/handler.go` lines ~314-330, `handler/quota.go`

---

## 9.8. Usage Recording Integration

### Overview

`metrics.RecordTokens()` now auto-calls `usageHandler.RecordUsage()` via a callback hook, so every request that records token metrics also populates the Redis usage buckets (hourly, daily, monthly, session) automatically.

### Wiring

```
main.go startup:
  |-- metrics.SetUsageRecorder(usageHandler.RecordUsage)
  |     // Sets callback in metrics package

Per-request flow:
  |-- proxy response received, usage parsed
  |-- metrics.RecordTokens(model, inputTokens, outputTokens)
  |     |-- Increment Prometheus counters (existing)
  |     |-- If usageRecorder callback set:
  |     |     Call usageRecorder(model, inputTokens, outputTokens)
  |     |     |-- Records to Redis:
  |     |     |     usage:hourly:YYYY-MM-DDTHH
  |     |     |     usage:daily:YYYY-MM-DD
  |     |     |     usage:monthly:YYYY-MM
  |     |     |     usage:sessions:YYYY-MM-DD
```

**File**: `metrics/metrics.go` lines ~221-237, `main.go` lines ~107-111

---

## 9.9. WebSocket Events (Full List)

### Event Types

The WebSocket hub now broadcasts 6 event types from various sources:

| Event Type | Source | Trigger | Data Fields |
|------------|--------|---------|-------------|
| `request-completed` | Handler | Successful upstream response | `model`, `statusCode`, `rtt_ms` |
| `request-error` | Handler | Failed upstream response | `model`, `statusCode`, `rtt_ms` |
| `anomaly-detected` | Handler | High-severity anomaly | `type`, `severity`, `model`, `rtt_ms` |
| `request-queued` | ChatCompletions | Async job enqueued | `requestId`, `model`, `provider` |
| `quota-warning` | Handler | Quota at >= 80% | `provider`, `accountId`, `model`, `percentage` |
| `config-changed` | Config watcher | .env file changed | `key` |

### Broadcast Wiring

Events are broadcast via the `wsBroadcast` function pointer stored in the Handler struct. This is set during handler construction from the WebSocketHub instance.

**File**: `handler/handler.go` lines ~408-432

---

## 6. AI Worker (Python)

### Architecture

```
main.py
  ├─ Prometheus HTTP server (port 9090)
  ├─ Internal metrics HTTP server (port 9091)
  │   └─ /metrics-internal → JSON snapshot
  ├─ N worker coroutines (WORKER_CONCURRENCY)
  │   └─ worker.run_loop(i)
  │       └─ BRPOP ai_jobs → _process_job() → loop forever
  └─ metrics_updater coroutine
      └─ อัปเดต queue depth ทุก 5 วินาที
```

### Worker Loop (`worker.py`)

```
BRPOP ai_jobs (poll timeout = 5s)
  │
  ├─ ได้ job → _process_job()
  │   │
  │   ├─ 1. Acquire model slot (per-model semaphore)
  │   │   ├─ Try requested model (non-blocking)
  │   │   ├─ Full? → Try fallback models in priority order:
  │   │   │   glm-5.1 → glm-5-turbo → glm-5 → glm-4.7 → glm-4.6 → glm-4.5
  │   │   └─ All full? → Block-wait on requested model
  │   │
  │   ├─ 2. Acquire global slot (if UPSTREAM_GLOBAL_LIMIT > 0)
  │   │
  │   └─ 3. _execute_job() inside model + global semaphore:
  │       ├─ Build provider fallback chain (only providers with keys)
  │       ├─ For each provider:
  │       │   ├─ Get API key from KeyManager
  │       │   ├─ Get/cached provider instance (httpx connection reuse)
  │       │   ├─ Wait for RPM slot (sliding window limiter)
  │       │   ├─ Call provider.complete()
  │       │   ├─ Success → store result → return
  │       │   └─ Fail:
  │       │       ├─ Rate limit → rotate key → retry same provider
  │       │       └─ Other → try next provider
  │       └─ All providers fail:
  │           ├─ retries < MAX_RETRIES → _retry_job()
  │           └─ retries >= MAX_RETRIES → store error result
  │
  ├─ CancelledError → clean exit
  └─ Exception → log + sleep 1s → retry BRPOP
```

### Retry Logic

- **Exponential backoff with jitter**: `base_backoff * 2^retry_count + random(0, backoff * 0.5)`
- **Max retries**: 3 (configurable)
- **Re-enqueue**: `LPUSH` กลับไป main queue (ไม่ใช่ retry queue)
- **Backoff sleep**: เกิดขึ้นใน worker coroutine — block worker slot ชั่วคราว

### Rate Limit Error Detection (`_is_rate_limit_error`)

Worker detect rate limit จาก error string:
- มี "429", "rate_limit", "rate limit"
- Z.ai specific: code "1302" (Rate limit reached), "1305" (service overloaded), "overloaded"
- HTTP status code: 429, **502**, **503**, **504**

> **หมายเหตุ**: 502/503/504 ไม่ใช่ rate limit error แต่ถูก treat ว่าเป็น เพื่อ trigger key rotation

### Per-Provider RPM Limiter (`ProviderRateLimiter`)

Sliding window rate limiter ที่ควบคุมความเร็ว request เข้าแต่ละ provider:
- Window: 60 วินาที
- Track timestamps ของ request ที่ผ่านไป
- ถ้าเต็ม → sleep จนกว่า request เก่าสุดจะหมดอายุจาก window
- Config: `PROVIDER_RPM_LIMITS=glm:5` (max 5 req/min to GLM)

```
10 requests arrive at once, RPM limit = glm:5

Batch 1 (t=0s):  5 requests → pass through → Z.ai → OK
Batch 2 (t=0s):  5 requests → window full → wait 60s
Batch 2 (t=60s): 5 requests → window reset → pass through → Z.ai → OK
Result: 10/10 OK, 0 429 errors
```

### Provider Cache

Worker cache provider instances ตาม `(provider_name, sha256(api_key)[:16])` (hash-based to prevent key collision):
- ใช้ `anthropic.AsyncAnthropic` client ซ้ำ → httpx connection pooling + cookie persistence
- ลด overhead จากการสร้าง client ใหม่ทุก request
- ทำให้ Z.ai ไม่ reject ด้วย "No cookie auth credentials"

### Internal Metrics (`Metrics` class)

- `jobs_processed`, `jobs_failed`, `jobs_retried`
- `provider_latency`: rolling window ล่าสุด 1000 samples ต่อ provider
- `provider_errors`: cumulative error count
- `rate_limit_hits`: cumulative rate limit count
- `snapshot()`: คำนวณ p50, p95, p99, avg ต่อ provider

### Config (`config.py`)

| Env Var | Default | Description |
|---------|---------|-------------|
| `REDIS_URL` | `redis://localhost:6379` | Dragonfly URL |
| `QUEUE_NAME` | `ai_jobs` | Queue name |
| `WORKER_CONCURRENCY` | `10` | Concurrent coroutines |
| `MAX_RETRIES` | `3` | Max retries per job |
| `BASE_BACKOFF` | `1.0` | Backoff base (seconds) |
| `POLL_TIMEOUT` | `5` | BRPOP timeout (seconds) |
| `RESULT_TTL` | `600` | Result cache TTL (seconds) |
| `METRICS_PORT` | `9090` | Prometheus port |
| `GLM_API_KEYS` | | Comma-separated keys |
| `OPENAI_API_KEYS` | | Comma-separated keys |
| `ANTHROPIC_API_KEYS` | | Comma-separated keys |
| `GEMINI_API_KEYS` | | Comma-separated keys |
| `OPENROUTER_API_KEYS` | | Comma-separated keys |
| `GLM_ENDPOINT` | `https://api.z.ai/api/anthropic` | GLM API endpoint |
| `UPSTREAM_MODEL_LIMITS` | `` | Per-model concurrent limits (same format as gateway) |
| `UPSTREAM_DEFAULT_LIMIT` | `1` | Default limit for unconfigured models (docker-compose default: 1) |
| `UPSTREAM_GLOBAL_LIMIT` | `0` | Total concurrent across all models (0=unlimited, docker-compose default: 9) |
| `PROVIDER_RPM_LIMITS` | `` | Per-provider RPM limit (e.g. `glm:5`) |

---

## 7. Provider Fallback Chain

### Order (hardcoded)

```
glm → openai → anthropic → gemini → openrouter
```

### Logic (`worker.py: _execute_job`)

```
1. เริ่มจาก provider ที่ job ระบุ
2. เติม providers ที่เหลือจาก fallback order — **เฉพาะที่มี API key เท่านั้น**
   (ถ้ามีแค่ GLM_API_KEYS จะวนแค่ glm, ไม่ไป openai/anthropic/gemini/openrouter)
3. ลองตามลำดับ:
   a. เรียก KeyManager.get_key() → random key
   b. Get/cached provider instance (connection reuse)
   c. Wait for RPM slot (sliding window limiter)
   d. เรียก provider.complete()
   e. สำเร็จ → เก็บ result → return
   f. ล้มเหลว:
      - Rate limit error → rotate key → retry same provider
      - อื่น → ข้ามไป provider ถัดไป
4. ทุก provider ล้ม → retry ทั้ง job (ถ้า retries เหลือ)
```

### Provider Implementations

| Provider | SDK | Endpoint | Notes |
|----------|-----|----------|-------|
| **GLM** | `anthropic` (Python SDK) | `api.z.ai/api/anthropic` | Z.ai เป็น Anthropic-compatible |
| **OpenAI** | `openai` (AsyncOpenAI) | `api.openai.com` | Standard |
| **Anthropic** | `anthropic` (AsyncAnthropic) | `api.anthropic.com` | System message extraction |
| **Gemini** | `google.generativeai` | Google AI | Convert message format |
| **OpenRouter** | `openai` (AsyncOpenAI) | `openrouter.ai/api/v1` | OpenAI-compatible |

### Model Mapping (GLM)

| Input | Sent to API |
|-------|------------|
| `glm-5` | `glm-5` |
| `glm-5.1` | `glm-5.1` (pass-through) |
| `glm-5-turbo` | `glm-5-turbo` (pass-through) |
| `glm-4.7` | `glm-4.7` (pass-through) |
| `glm-4.6` | `glm-4.6` (pass-through) |
| `glm-4.5` | `glm-4.5` (pass-through) |
| `glm-4.6v` | `glm-4.6v` |
| อื่นๆ | ส่งตรงไปเลย (pass-through) |

### Model Fallback Priority (Worker)

เมื่อ requested model เต็ม จะลอง fallback ตามลำดับ:
```
glm-5.1 → glm-5-turbo → glm-5 → glm-4.7 → glm-4.6 → glm-4.5
```

---

## 7.5. Provider Registry (Gateway)

### Provider Registry (`provider/registry.go`)

Gateway maintains a provider registry for OAuth/API key auth flows and upstream resolution. Each provider defines: ID, name, auth type, upstream base URL, and OAuth config.

#### Supported Auth Types

| Auth Type | Flow | Providers |
|-----------|------|-----------|
| `api_key` | Header-based | Anthropic, Gemini, OpenAI, Z.AI, OpenRouter, DeepSeek, Kimi, HuggingFace, Ollama, AGY, Cursor, CodeBuddy, Kilo |
| `device_code` | Device code flow | GitHub Copilot, Qwen (Aliyun) |
| `auth_code` | OAuth authorization code + PKCE | Claude (OAuth), Gemini (OAuth via Code Assist) |
| `session_cookie` | Cookie-based | (reserved) |

#### All Registered Providers

| ID | Name | Auth | Upstream Base |
|----|------|------|---------------|
| `anthropic` | Anthropic | API key | `api.anthropic.com` |
| `gemini` | Google Gemini | API key | `generativelanguage.googleapis.com` |
| `gemini-oauth` | Google Gemini (OAuth) | Auth code | `cloudcode-pa.googleapis.com` |
| `openai` | OpenAI | API key | `api.openai.com` |
| `copilot` | GitHub Copilot | Device code | `api.github.com/copilot` |
| `zai` | Z.AI | API key | `api.z.ai/api/anthropic` |
| `openrouter` | OpenRouter | API key | `openrouter.ai/api` |
| `qwen` | Qwen (Aliyun) | Device code | `dashscope.aliyuncs.com` |
| `claude` | Claude (OAuth) | Auth code (PKCE) | `api.anthropic.com` |
| `deepseek` | DeepSeek | API key | `api.deepseek.com` |
| `kimi` | Kimi (Moonshot) | API key | `api.moonshot.cn/v1` |
| `huggingface` | Hugging Face | API key | `api-inference.huggingface.co/models` |
| `ollama` | Ollama | API key | `localhost:11434` (configurable) |
| `agy` | Antigravity | API key | `antigravity.com` |
| `cursor` | Cursor | API key | `api2.cursor.sh` |
| `codebuddy` | CodeBuddy | API key | `api.codebuddy.io` |
| `kilo` | Kilo | API key | `api.kilo.ai` |

#### Token Store & Auth Flow

The `provider.TokenStore` persists OAuth tokens and API keys in Dragonfly. The `provider.AuthHandler` exposes endpoints for device code flow, auth code flow, and API key registration. The `provider.Resolver` maps incoming requests to the correct upstream based on provider and token.

The `provider.RefreshWorker` runs in background and refreshes expiring OAuth tokens automatically.

---

## 8. Key Rotation

### KeyManager (`key_manager.py`)

- **Key selection**: Random (ไม่ใช่ round-robin)
- **Key rotation**: key เข้าสู่ cooldown 60 วินาที (ไม่ถอดถาวรแล้ว)
- **Thread-safe**: ใช้ `asyncio.Lock` ต่อ provider
- **Availability**: `get_available_providers()` return providers ที่ยังมี key เหลือ

### Behavior

```
Initial: glm keys = [key1, key2, key3]

Request → key1 → 429 Rate Limit
  → cooldown_key(key1, 60s) → keys available: [key2, key3], key1 cooling down
  → retry with key2

... after 60s ...
  → key1 auto-recovers → available: [key1, key2, key3]
```

---

## 13. Metrics & Observability

### Gateway Metrics (Prometheus -- port 8080, custom registry)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_request_latency_seconds` | Histogram | method, path, status | Request latency |
| `api_gateway_queue_depth` | GaugeFunc | | Queue depth (polled on scrape) |
| `api_gateway_error_total` | Counter | type | Error count by type |
| `api_gateway_rate_limit_hits_total` | Counter | key | Rate limit hits |
| `api_gateway_active_connections` | Gauge | | Active connections |
| `api_gateway_token_input_total` | Counter | model | Input tokens consumed by model |
| `api_gateway_token_output_total` | Counter | model | Output tokens generated by model |
| `api_gateway_upstream_retries_total` | Counter | | Upstream retries on 429 |
| `api_gateway_upstream_429_total` | Counter | | Upstream 429 responses received |
| `api_gateway_adaptive_limit` | Gauge | model | Current adaptive concurrency limit per model |
| `api_gateway_adaptive_in_flight` | Gauge | model | Current in-flight requests per model |
| `api_gateway_cost_total` | Counter | model | Estimated cost (USD) from token usage x pricing |
| `api_gateway_model_fallback_total` | Counter | requested, selected | Model fallback events |
| `api_gateway_ttfb_seconds` | Histogram | model | Time to first byte for streaming |
| `api_gateway_go_goroutines` | Gauge | | Current goroutines |
| `api_gateway_go_heap_alloc_bytes` | Gauge | | Current heap allocation |
| `api_gateway_go_heap_objects` | Gauge | | Current heap objects |
| `api_gateway_go_gc_pause_ns` | Gauge | | GC pause of last cycle (ns) |
| `api_gateway_go_stack_inuse_bytes` | Gauge | | Current stack in-use |
| `api_gateway_dragonfly_up` | Gauge | | Dragonfly health (1=healthy, 0=down) |
| `api_gateway_anomaly_total` | Counter | type, severity | Detected anomalies |

**Total: 21 metrics** in `api_gateway` namespace on a custom Prometheus registry (not default).

> **หมายเหตุ**: Status label ใน latency histogram ใช้ `statusWriter` wrapper ที่จับ status code จริงจาก response writer แล้ว (แก้ไขจากเดิมที่ hardcode "200")

### Worker Metrics

**Prometheus (port 9090)** — ใช้ได้จริง:
- `ai_worker_queue_depth` (Gauge) — อัปเดตทุก 5 วินาที
- `ai_worker_active` (Gauge) — จำนวน worker ที่ active

**Internal JSON (port 9091)** — `/metrics-internal`:
```json
{
  "jobs_processed": 1234,
  "jobs_failed": 5,
  "jobs_retried": 20,
  "queue_depth": 3,
  "provider_latency": {
    "glm": {"p50": 1.2, "p95": 3.4, "p99": 5.6, "avg": 1.5},
    "openai": {"p50": 0.8, "p95": 2.1, "p99": 4.0, "avg": 1.0}
  },
  "provider_errors": {"glm": 2, "openai": 0},
  "rate_limit_hits": {"glm": 1}
}
```

### Rate Limiter Metrics (Spring Actuator — port 8080)

- `/actuator/health` — Health check
- `/actuator/prometheus` — Prometheus metrics
- `http_server_requests_seconds_*` — HTTP metrics
- `jvm_memory_*` — JVM memory

### OpenTelemetry

- Gateway: ส่ง traces ไป `arl-otel:4317` (gRPC)
- Worker: **ไม่มี OTel instrumentation** (config มีแต่ code ไม่ได้เชื่อม)
- OTel Collector: รับ traces → batch → export ไป Prometheus (`:8889`) + debug

### Prometheus Scrape Config

| Target | Interval | Path |
|--------|----------|------|
| `arl-gateway:8080` | 5s | `/metrics` |
| `arl-worker:9090` | 5s | `/metrics` |
| `arl-rate-limiter:8080` | 10s | `/actuator/prometheus` |
| `arl-dragonfly:6379` | 10s | — |
| `arl-otel:8889` | 10s | `/metrics` |
| Self | 15s | `/metrics` |

---

## 10. Data Flow: Queue & Cache

### Queue (Dragonfly List)

```
Producer (Gateway)                    Consumer (Worker)
    │                                      │
    │ LPUSH ai_jobs '{"request_id":...}'   │ BRPOP ai_jobs (timeout 5s)
    │ ─────────────────────────────────▶   │
    │                                      │ parse job JSON
    │                                      │ process job
    │                                      │ SET result:{id} (TTL 600s)
    │                                      │
    │ ◀──── GET result:{id} ────────────── │
    │ (Client polls via GET /v1/results/{id})
```

### Key Patterns

| Pattern | Type | TTL | Description |
|---------|------|-----|-------------|
| `ai_jobs` | List | — | Main job queue |
| `result:{request_id}` | String (JSON) | 600s | Cached job result |

### Retry Flow

```
Job fails all providers
  │
  ├─ retry_count < MAX_RETRIES
  │   ├─ Compute backoff: base * 2^retry + jitter
  │   ├─ Sleep (block worker slot)
  │   └─ LPUSH back to ai_jobs (head of queue → priority)
  │
  └─ retry_count >= MAX_RETRIES
      └─ Store error result in cache
```

---

## 10.5 Complete Request Flow (10 Concurrent Example)

### Full System Diagram

```
Client (10 concurrent POST /v1/chat/completions)
  │
  ▼
┌─────────────────────────────────────────────┐
│ arl-gateway (:8080)                         │
│                                             │
│  ┌────────────────┐  ┌──────────────────┐  │
│  │ Rate Limiter   │  │ Key Pool         │  │
│  │ (token bucket) │  │ (passthrough)    │  │
│  │ global: 100/m  │  │ key from client  │  │
│  │ agent: 5/m     │  └──────────────────┘  │
│  └────────────────┘                         │
│           │                                 │
│           ▼ All 10 pass rate limit          │
│  LPUSH ai_jobs x 10                         │
└─────────────┬───────────────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────┐
│ arl-dragonfly (Redis-compatible)            │
│  ai_jobs queue: [job1..job10]               │
└─────────────┬───────────────────────────────┘
              │ BRPOP x 50 worker coroutines
              ▼
┌─────────────────────────────────────────────────────────────┐
│ arl-worker (Python asyncio)                                 │
│                                                             │
│  Layer 1: Per-Model Semaphores (19 slots, global cap 9)           │
│  ┌────────┬──────────┬───────┬────────┬────────┬────────┐       │
│  │glm-5.1 │glm-5-turbo│glm-5 │glm-4.7 │glm-4.6 │glm-4.5 │       │
│  │ 1 slot │ 1 slot   │2 slots│2 slots │3 slots │10 slots│       │
│  └────────┴──────────┴───────┴────────┴────────┴────────┘       │
│  Fallback: glm-5.1 → glm-5-turbo → glm-5 → glm-4.7 → glm-4.6  │
│            → glm-4.5                                              │
│                                                                  │
│  Layer 2: Global Semaphore (9 concurrent max)                    │
│                                                             │
│  Layer 3: Per-Provider RPM Limiter                         │
│  ┌──────────────────────────────┐                          │
│  │ ProviderRateLimiter (glm:5)  │                          │
│  │ Sliding window 60s           │                          │
│  │ Batch 1: 5 req → pass now    │                          │
│  │ Batch 2: 5 req → wait 60s    │                          │
│  └──────────────────────────────┘                          │
│                                                             │
│  Layer 4: Provider Cache (httpx reuse)                      │
│                                                             │
│  Layer 5: Provider Fallback (key-gated)                     │
│  Only tries providers with API keys configured              │
│  GLM_API_KEYS only → glm only                              │
│  + OPENAI_API_KEYS → glm → openai                          │
└──────────────────────────┬──────────────────────────────────┘
                           │
                           ▼ 5 req now + 5 req after 60s
┌──────────────────────────────────────────────────────────────┐
│ Z.ai API (api.z.ai/api/anthropic)                            │
│  Per-account RPM: ~5 req/min (single key)                     │
│  Batch 1 (t=0s):  5 req → all 200 OK                        │
│  Batch 2 (t=60s): 5 req → all 200 OK                        │
└──────────────────────────────────────────────────────────────┘
```

### Per-Request Model Distribution (Real Test Data)

```
              t=0s          t=5s          t=10s         ...  t=60s         t=65s
                |             |             |                 |              |
BATCH 1 (5):  [RPM limiter: 5/5 slots used for 60s]
  req1         █████░ 1.5s   (glm-5, slot available)
  req2         ████░ 1.2s    (glm-5-turbo, fallback)
  req3         ████░ 1.1s    (glm-4.7, fallback)
  req4         ██████████████████████░ 5.6s (glm-5, slot freed)
  req5         ████████████████████████████████░ 8.7s (glm-5.1, fallback)

BATCH 2 (5):  [RPM limiter: waited 60s, window reset]
  req6                                                       █░ 0.7s (glm-4.6)
  req7                                                       █░ 0.7s (glm-4.7)
  req8                                                       █░ 0.7s (glm-4.6)
  req9                                                       ██░ 1.6s (glm-4.6)
  req10                                                      ██████████░ 4.8s (glm-5)
```

| Req | Requested | Got | Fallback? | Latency |
|-----|-----------|-----|-----------|---------|
| 1 | glm-5 | **glm-5** | No | 1.5s |
| 2 | glm-5 | **glm-5-turbo** | Yes | 1.2s |
| 3 | glm-5 | **glm-4.7** | Yes | 1.1s |
| 4 | glm-5 | **glm-5** | No | 5.6s |
| 5 | glm-5 | **glm-5.1** | Yes | 8.7s |
| 6 | glm-5 | **glm-4.6** | Yes | 0.7s |
| 7 | glm-5 | **glm-4.7** | Yes | 0.7s |
| 8 | glm-5 | **glm-4.6** | Yes | 0.7s |
| 9 | glm-5 | **glm-4.6** | Yes | 1.6s |
| 10 | glm-5 | **glm-5** | No | 4.8s |

**Result: 10/10 OK, 0 429 errors, total wall time ~75s**

---

## 10.6 Adding Additional Providers

### วิธีเปิด Provider เจ้าอื่น

เพิ่ม API key ใน `.env`:

```bash
# ตัวอย่าง: เปิด OpenAI + Anthropic
OPENAI_API_KEYS=sk-proj-xxx,sk-proj-yyy
ANTHROPIC_API_KEYS=sk-ant-xxx
```

แล้ว restart worker:

```bash
docker-compose up -d --build arl-worker
```

### ผลที่เกิดขึ้นอัตโนมัติ

```
ก่อนเพิ่ม (GLM_API_KEYS only):
  Provider fallback chain = [glm]
  ถ้า glm ล้มเหลว → retry glm

หลังเพิ่ม (GLM_API_KEYS + OPENAI_API_KEYS):
  Provider fallback chain = [glm, openai]
  ถ้า glm ล้มเหลว → ลอง openai อัตโนมัติ
  ไม่ต้องแก้ code หรือ config เพิ่ม
```

Worker ใช้ `key_manager.has_keys(provider)` ตรวจสอบก่อนเพิ่ม provider เข้า fallback chain — ถ้าไม่มี key ก็ข้าม ไม่วนไปหา provider ที่ไม่มี key

### RPM Limits สำหรับ Provider แต่ละตัว

```bash
# ตั้ง RPM limit แยกต่อ provider
PROVIDER_RPM_LIMITS=glm:5,openai:60,anthropic:50
```

ถ้าไม่ตั้ง → ไม่มี RPM limit (ยิงได้ไม่จำกัด) — แนะนำตั้งตาม tier ของ account

### Provider ที่รองรับ

| Provider | Env Var | Default RPM | Notes |
|----------|---------|-------------|-------|
| **GLM** (Z.ai) | `GLM_API_KEYS` | 5 | Primary, Anthropic-compatible endpoint |
| **OpenAI** | `OPENAI_API_KEYS` | 60 | gpt-4o, gpt-4o-mini |
| **Anthropic** | `ANTHROPIC_API_KEYS` | 50 | claude-sonnet-4-6, claude-haiku-4-5 |
| **Gemini** | `GEMINI_API_KEYS` | 60 | gemini-2.0-flash |
| **OpenRouter** | `OPENROUTER_API_KEYS` | 60 | Multi-provider aggregator |
| **DeepSeek** | `DEEPSEEK_API_KEYS` | 60 | deepseek-chat, deepseek-coder |
| **Kimi** | `KIMI_API_KEYS` | 60 | Moonshot AI |
| **HuggingFace** | `HUGGINGFACE_API_KEYS` | 60 | Open-source models |
| **Ollama** | `OLLAMA_API_KEYS` | 60 | Local models (default: localhost:11434) |
| **AGY** | `AGY_API_KEYS` | 60 | Antigravity |
| **Cursor** | `CURSOR_API_KEYS` | 60 | Cursor AI |
| **CodeBuddy** | `CODEBUDDY_API_KEYS` | 60 | CodeBuddy AI |
| **Kilo** | `KILO_API_KEYS` | 60 | Kilo AI |

### Fallback Order

```
glm → openai → anthropic → gemini → openrouter
       │          │           │          │
       └──────────┴───────────┴──────────┘
       ข้าม provider ที่ไม่มี API key อัตโนมัติ
```

### การเพิ่ม Key หลายตัว (Key Rotation)

```bash
# หลาย keys = throughput สูงขึ้น
GLM_API_KEYS=key1,key2,key3
# RPM budget = 5 x 3 keys = 15 req/min
PROVIDER_RPM_LIMITS=glm:15
```

---

## 11. Network & Ports

### External (เข้าถึงจาก host)

| Port | Service | Protocol |
|------|---------|----------|
| **8080** | API Gateway | HTTP |
| **8081** | Rate Limiter Dashboard | HTTP |
| **3000** | Grafana | HTTP |

### Internal (Docker network only)

| Port | Service | Protocol | Notes |
|------|---------|----------|-------|
| 8080 | Rate Limiter | HTTP | Spring Boot |
| 6379 | Dragonfly | Redis | Redis-compatible |
| 9090 | AI Worker (Prometheus) | HTTP | Metrics |
| 9091 | AI Worker (Internal) | HTTP | `/metrics-internal` JSON |
| 9090 | Prometheus | HTTP | Scrape target |
| 4317 | OTel Collector | gRPC | Traces |
| 4318 | OTel Collector | HTTP | Traces |
| 8889 | OTel Collector | HTTP | Prometheus export |

---

## 12. Resource Limits

| Service | Memory Limit | CPU Limit | Memory Reserved | CPU Reserved |
|---------|-------------|-----------|----------------|-------------|
| arl-gateway | 512M | 1.0 | 128M | 0.25 |
| arl-rate-limiter | 768M | 1.0 | 256M | 0.5 |
| arl-dragonfly | 6G | 2.0 | 512M | 0.5 |
| arl-worker | 1G | 2.0 | 256M | 0.5 |
| arl-prometheus | 512M | 0.5 | 128M | — |
| arl-grafana | 256M | 0.5 | 64M | — |
| arl-otel | 256M | 0.5 | 64M | — |
| arl-rl-dashboard | 128M | 0.25 | 32M | — |

### Dragonfly Tuning

```bash
--maxmemory=2gb          # Default memory limit
--proactor_threads=4      # 4 threads
--cache_mode=true         # Cache eviction
--tcp_keepalive=60        # Keep-alive
--pipeline_squash=10      # Pipeline optimization
```

### Log Rotation

ทุก service ใช้ JSON log driver:
- `max-size: 5-10MB`
- `max-file: 2-3`

---

## 13. Multi-Agent Use Cases

### โหมดไหนเหมาะกับอะไร

| Use Case | โหมด | เหตุผล |
|----------|------|--------|
| **Claude Code (interactive)** | Sync (`/v1/messages`) | ต้องการ SSE streaming real-time, tool loop หลายรอบ, latency ต่ำ |
| **1 เครื่องหลาย Claude Code** | Sync | แต่ละ session ใช้ key ต่างกัน → per-key rate limit แยกอิสระ |
| **CI/CD pipeline** | Async (`/v1/chat/completions`) | ไม่ต้องการ real-time, ส่งแล้วไปทำอย่างอื่นได้, poll result ทีหลัง |
| **Batch processing** | Async | ยิง 100 jobs พร้อมกัน → queue จัดการ pacing เอง |
| **Multi-agent framework** | Async | หลาย agent ยิงพร้อมกัน → queue + per-agent rate limit ป้องกัน overload |
| **Cron / scheduled tasks** | Async | ตั้งเวลายิง → worker process ตามลำดับ |

### Sync Mode — Multi-Agent บนเครื่องเดียว

```
Machine A (developer workstation)
  ├─ Claude Code session 1 (key: key-aaa)  ──▶ Gateway ──▶ Z.ai
  ├─ Claude Code session 2 (key: key-bbb)  ──▶ Gateway ──▶ Z.ai
  └─ Claude Code session 3 (key: key-aaa)  ──▶ Gateway ──▶ Z.ai

Gateway rate limit:
  key-aaa: 5 req/min (sessions 1+3 แชร์ quota เดียวกัน)
  key-bbb: 5 req/min (session 2 มี quota ต่างหาก)
  Global:  100 req/min (รวมทุก session)
```

**ข้อดี**: Real-time streaming, tool loop ทำงานเหมือนยิงตรง
**ข้อจำกัด**: ถ้า provider มี RPM limit ต่ำ (เช่น Z.ai 5 RPM/key) → หลาย session ใช้ key เดียวกันจะชนกัน

### Async Mode — Multi-Agent แบบ Batch

```
Agent Orchestrator
  ├─ Agent A (code reviewer)     ──POST /v1/chat/completions──▶ Gateway
  ├─ Agent B (test writer)       ──POST /v1/chat/completions──▶ Gateway
  ├─ Agent C (doc generator)     ──POST /v1/chat/completions──▶ Gateway
  └─ Agent D (security scanner)  ──POST /v1/chat/completions──▶ Gateway
                                              │
                                              ▼
                                     arl-dragonfly (queue)
                                              │
                                              ▼
                                     arl-worker (50 coroutines)
                                       ├─ RPM limiter paces requests
                                       ├─ Per-model semaphore (19 slots, global cap 9)
                                       └─ Provider fallback chain
                                              │
                                              ▼
                                        Z.ai / OpenAI / ...
                                              │
                        Agent polls GET /v1/results/{id} ←── result cache (TTL 600s)
```

**ข้อดี**:
- Queue รองรับ burst traffic (ยิง 100 jobs พร้อมกัน → worker process ตามลำดับ)
- Per-agent rate limit (agent_id แยก quota)
- Retry + backoff อัตโนมัติ
- Provider fallback ถ้า provider หลักล่ม

**ข้อจำกัด**: ไม่ real-time, ต้อง poll result

### แนวทางแนะนำตาม Scale

| Scale | โหมด | การตั้งค่า |
|-------|------|-----------|
| **1 developer** | Sync | `ANTHROPIC_BASE_URL=http://localhost:8080`, key เดียว |
| **2-5 developers** | Sync | แต่ละคนใช้ key ต่างกัน → per-key rate limit แยก |
| **1 team + CI/CD** | Sync + Async | Developer ใช้ sync, CI pipeline ใช้ async |
| **Agent framework (5-50 agents)** | Async | `WORKER_CONCURRENCY=50`, `PROVIDER_RPM_LIMITS=glm:5` |
| **Heavy batch (100+ jobs)** | Async | เพิ่ม keys: `GLM_API_KEYS=key1,key2,key3`, `PROVIDER_RPM_LIMITS=glm:15` |

### การเพิ่ม Throughput

```
Throughput = keys × RPM per key

GLM_API_KEYS=key1                    → 5 RPM
GLM_API_KEYS=key1,key2               → 10 RPM
GLM_API_KEYS=key1,key2,key3          → 15 RPM
GLM_API_KEYS=x3 + OPENAI_API_KEYS=x2 → GLM 15 + OpenAI 120 = 135 RPM

ตั้ง PROVIDER_RPM_LIMITS ให้ตรง:
GLM_API_KEYS=key1,key2,key3
PROVIDER_RPM_LIMITS=glm:15

Worker จะ rotate key อัตโนมัติ (random selection)
ถ้า key ไหนโดน 429 → cooldown 60s แล้ว auto-recover
```

---

## 14. Model Selection Priority (Adaptive Limiter with Series Routing)

### Series Grouping

Models grouped by major version for intelligent fallback:

```
Series 5 (preferred): glm-5.1(1), glm-5-turbo(1), glm-5(2)  = 4 slots
Series 4 (fallback):  glm-4.7(2), glm-4.6(3), glm-4.5(10)  = 15 slots
Vision:               glm-4.6v(10), glm-4.5v(10), glm-4.6v-flashx(3), glm-4.6v-flash(1) = 24 slots
Global cap: 9 concurrent
```

### Selection Algorithm (Series-Based)

Request: { "model": "glm-5", ... }

Step 1: Wait for global slot (sync.Cond signal-based, not spin-wait)

Step 2: Try requested model (non-blocking CAS acquire)
        glm-5 (limit 2) → available? → use glm-5
                          → full? → Step 3

Step 3: Round-robin within same series (series 5):
        glm-5.1, glm-5-turbo, glm-5 (pooled candidate slices)
        Any available? → use it

Step 4: Check series latency pressure:
        EWMA RTT > 1.5x minRTT for majority of series 5 models?
        ├─ Yes → spill to series 4 (round-robin: glm-4.7, glm-4.6, glm-4.5)
        └─ No  → no spillover, wait for series 5

Step 5: All models full or global cap reached:
        Release global slot → signal-based block-wait on requested model (30s timeout)
        → re-acquire global slot after model slot obtained

Key: Series routing keeps requests within the same quality tier when possible.
     Spillover to lower series only happens under confirmed latency pressure.
     Signal-based waiting eliminates CPU waste from polling.

### Adaptive Limit Discovery

Initial limits auto-adjust based on upstream feedback:
- Probes upward every 5 consecutive successes
- Halves limit on 429/503
- Remembers learned ceiling (peakBefore429) to prevent oscillation
- Visible via GET /v1/limiter-status as `learned_ceiling` field

### Example: 15 Concurrent Requests for glm-5

| Req | Model Selected | Fallback? | Reason |
|-----|---------------|-----------|--------|
| 1 | glm-5 | No | Slot available (1/2) |
| 2 | glm-5 | No | Slot available (2/2, full) |
| 3 | glm-5.1 | Yes | glm-5 full, next in priority |
| 4 | glm-5-turbo | Yes | glm-5.1 full, next in priority |
| 5-6 | glm-4.7 | Yes | All 5.x full, start 4.x |
| 7-9 | glm-4.6 | Yes | glm-4.7 full |
| 10-14 | glm-4.5 | Yes | glm-4.6 full, overflow buffer |
| 15 | (waits) | N/A | Global cap 9 reached |
```

---

## 15. Real-World Load Test Results

### สภาพแวดล้อมการทดสอบ

- **Config**: 1 GLM API key, RPM=5, 19 model slots (global cap 9), 50 worker coroutines
- **Endpoint**: `POST /v1/chat/completions` (async mode)
- **Method**: ยิง concurrent burst แล้ว poll จนกว่าจะเสร็จทุก request
- **Script**: `scripts/multi-agent-test.sh`

### Test 1: 3 Agents x 1 Turn (3 requests)

| Metric | Value |
|--------|-------|
| Wall time | 18.8s |
| Success rate | 3/3 (100%) |
| 429 errors | 0 |
| Fastest / P50 / Avg / Slowest | 8.3s / 10.4s / 12.5s / 18.6s |
| Throughput | 9.6 req/min |
| Model distribution | glm-5.1 x3 |
| Key survived | Yes |

### Test 2: 5 Agents x 1 Turn (5 requests)

| Metric | Value |
|--------|-------|
| Wall time | 33.5s |
| Success rate | 5/5 (100%) |
| 429 errors | 0 |
| Fastest / P50 / Avg / Slowest | 6.3s / 29.2s / 22.5s / 33.3s |
| Throughput | 9.0 req/min |
| Model distribution | glm-5.1 x3, glm-5-turbo x1, glm-4.7 x1 |
| Key survived | Yes |

### Test 3: 10 Agents x 1 Turn (10 requests)

| Metric | Value |
|--------|-------|
| Wall time | 31.7s |
| Success rate | 10/10 (100%) |
| 429 errors | 0 |
| Fastest / P50 / Avg / Slowest | 19.0s / 27.3s / 25.3s / 31.5s |
| Throughput | 18.9 req/min |
| Model distribution | glm-5.1 x3, glm-4.7 x2, others x5 |
| Key survived | Cooldown (auto-recovers) |

### Test 4: 5 Agents x 2 Turns (10 requests)

| Metric | Value |
|--------|-------|
| Wall time | 15.0s |
| Success rate | 10/10 (100%) |
| 429 errors | 0 |
| Fastest / P50 / Avg / Slowest | 8.6s / 10.7s / 10.9s / 14.8s |
| Throughput | 40.0 req/min |
| Key survived | Cooldown (auto-recovers) |

### Summary: Capacity vs Concurrent Agents

| Agents | Total Reqs | Success | Wall Time | Key Survived | Safe? |
|--------|-----------|---------|-----------|-------------|-------|
| 3 | 3 | 100% | ~19s | Yes | Safe |
| 5 | 5 | 100% | ~33s | Yes | Safe |
| 5 x2 turns | 10 | 100% | ~15s | Cooldown | Burst ok |
| 10 | 10 | 100% | ~32s | Cooldown | Burst ok |

### Conclusions

1. **Safe concurrent (key survives)**: 3-5 agents
2. **Burst capacity**: 10 agents (key cools down for 60s after burst, auto-recovers)
3. **RPM bottleneck**: With 1 key at 5 RPM, requests beyond the first batch wait ~30s for RPM window reset
4. **Model fallback works**: Automatically distributes across glm-5.1, glm-5-turbo, glm-4.7, glm-4.6, glm-4.5
5. **Key cooldown**: Keys auto-recover after 60s cooldown, no worker restart needed

### Limitations (Current Config)

```
Bottleneck hierarchy (slowest first):

1. Provider RPM limit:    5 req/min (1 GLM key) — MAIN BOTTLENECK
2. Global cap:            9 concurrent — limits burst
3. Model slots:           19 concurrent (across 6 models) — sufficient for current load
4. Worker capacity:       50 coroutines — far exceeds demand
```

### How to Scale

| Method | Effect | Config Change |
|--------|--------|---------------|
| Add GLM keys | +5 RPM per key | `GLM_API_KEYS=k1,k2,k3` + `PROVIDER_RPM_LIMITS=glm:15` |
| Add OpenAI | +120 RPM fallback | `OPENAI_API_KEYS=sk1,sk2` + `PROVIDER_RPM_LIMITS=glm:5,openai:120` |
| Add Anthropic | +50 RPM fallback | `ANTHROPIC_API_KEYS=sk1` + `PROVIDER_RPM_LIMITS=glm:5,anthropic:50` |
| Multi-provider | Maximum throughput | Combine all providers |

---

## 16. Known Issues

### Key Cooldown

**Status**: Key rotation now uses cooldown-based recovery instead of permanent removal. Keys enter a 60-second cooldown on 429 errors and auto-recover afterward.

**Previous behavior**: KeyManager removed keys permanently from the pool when a 429 rate limit error occurred. With only 1 key, the worker became unable to process any more jobs after the key was rotated out.

**Current behavior**: Keys cool down for 60 seconds, then automatically rejoin the pool. No worker restart needed.

### Rate Limiter Blocking Internal Endpoints

**Fixed**: Rate limiter middleware now skips `/metrics`, `/health`, and `/v1/limiter-status` paths. Previously, Prometheus scrapes to `/metrics` were being rate limited (429), making metrics unavailable.

### Docker-Compose Default Unification

**Fixed**: Gateway and worker now both default to `UPSTREAM_DEFAULT_LIMIT=1`, `UPSTREAM_GLOBAL_LIMIT=9` in docker-compose.yml. Previously, the gateway used different defaults than the worker, causing inconsistent behavior.

### Gateway Key Pool RPM Leak

**Fixed**: `keyPool.Acquire()` in the Messages handler is called after body read, size check, and JSON parse. Previously, acquiring a key before validation wasted RPM budget on malformed requests that would never reach upstream.

### Context-Aware Retry Backoff

**Fixed**: Retry backoff in `proxy/anthropic.go` uses `select` with `r.Context().Done()` instead of `time.Sleep`. Previously, a client disconnect during retry backoff would leave the goroutine sleeping for the full backoff duration.

### Gateway Metrics Status Tracking

**Fixed**: `metrics/metrics.go` now uses `statusWriter` wrapper that captures the actual HTTP status code from the response writer. Previously, the status label was always "200".

### Global Slot Starvation Prevention

**Fixed**: `AdaptiveLimiter.Acquire()` releases the global slot before blocking-waiting on a model, then re-acquires it after. Previously, many requests could hold global slots while blocked on a popular model, starving requests that could use other models.

---

*Architecture docs v3.1 -- updated with profile-based routing, quota enforcement, usage recording integration, WebSocket event expansion (6 types), Z.AI pricing update (19 models), UPSTREAM_API_KEYS replacing GLM_API_KEYS/GLM_ENDPOINT*
