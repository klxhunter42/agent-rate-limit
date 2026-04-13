# System Architecture

> เอกสารอธิบายสถาปัตยกรรมของระบบทุกส่วน
> รวมถึง internal behavior ที่ไม่ได้อยู่ใน MANUAL.md

---

## สารบัญ

1. [ภาพรวม](#1-ภาพรวม)
2. [API Gateway (Go)](#2-api-gateway-go)
3. [Rate Limit Middleware](#3-rate-limit-middleware)
4. [Per-Model Upstream Limiter](#4-per-model-upstream-limiter)
5. [Transparent Proxy](#5-transparent-proxy)
6. [AI Worker (Python)](#6-ai-worker-python)
7. [Provider Fallback Chain](#7-provider-fallback-chain)
8. [Key Rotation](#8-key-rotation)
9. [Metrics & Observability](#9-metrics--observability)
10. [Data Flow: Queue & Cache](#10-data-flow-queue--cache)
11. [Network & Ports](#11-network--ports)
12. [Resource Limits](#12-resource-limits)
13. [Multi-Agent Use Cases](#13-multi-agent-use-cases)

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
│    ├─ Logging Middleware                                         │
│    ├─ Metrics Middleware                                         │
│    ├─ Rate Limit Middleware ──▶ arl-rate-limiter (:8080)        │
│    │                              │                              │
│    │                              └─▶ arl-dragonfly (:6379)     │
│    │                                                             │
│    ├─ Sync mode: Transparent Proxy ──▶ Upstream Provider        │
│    │                                    (api.z.ai/api/anthropic) │
│    │                                                             │
│    └─ Async mode: LPUSH job ──▶ arl-dragonfly (queue)          │
│                                    │                             │
│                              arl-worker (BRPOP x 50)             │
│                                ├─ Per-Model Semaphores (19 slots, global cap 9)  │
│                                ├─ RPM Limiter (glm:5)            │
│                                ├─ Provider Cache (httpx reuse)    │
│                                ├─ Provider Fallback Chain        │
│                                ├─ Key Rotation                   │
│                                └─ Result → arl-dragonfly (cache) │
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
2. Init OTel tracer (→ arl-otel:4317) — ถ้าไม่ได้จะ warn และทำงานต่อ
3. Connect Dragonfly (ping test) — ถ้าไม่ได้จะ exit
4. Init Prometheus metrics
5. Create AnthropicProxy (transparent)
6. Create Handler
7. Setup chi router with middleware stack:
   a. Logging (structured JSON)
   b. Metrics (latency + active connections)
   c. Rate Limiter (global + per-agent)
8. Start HTTP server (WriteTimeout=0 for SSE)
9. Graceful shutdown on SIGINT/SIGTERM (10s timeout)
```

### Routes

| Method | Path | Handler | Mode |
|--------|------|---------|------|
| `POST` | `/v1/messages` | `Messages` | Sync (transparent proxy) |
| `POST` | `/v1/chat/completions` | `ChatCompletions` | Async (enqueue) |
| `GET` | `/v1/result/{requestID}` | `GetResult` | Async (poll result) |
| `GET` | `/health` | `Health` | Health check |
| `GET` | `/metrics` | Prometheus | Metrics scrape |

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

## 4. Per-Model Upstream Limiter (Adaptive)

### Architecture (`middleware/adaptive_limiter.go`)

Adaptive concurrency limiter with automatic limit discovery. Starts at configured initial limits,
probes upward based on upstream feedback (success rate, latency), and backs off on 429/503 errors.

Inspired by Envoy gradient controller + Netflix concurrency limits.

### Adaptive Algorithm

```
On 429/503:
  peakBefore429 = current limit     (remember the ceiling that caused 429)
  limit_new = max(minLimit, limit * 0.5)  (multiplicative decrease x0.5)

On 200:
  gradient = (minRTT + buffer) / sampleRTT
  limit_new = min(maxLimit, gradient * limit + sqrt(limit))
  if newLimit >= peakBefore429 → cap at peakBefore429 - 1  (don't re-probe learned ceiling)

Cooldown: 5s after any 429 before increasing again
Probe: every 5 consecutive successes
```

### Fallback with Wait-then-Fallback

```
Request: { "model": "glm-5", ... }
  │
  ├─ 1. Try glm-5 (non-blocking CAS acquire)
  │     └─ Available? → use glm-5
  │
  ├─ 2. glm-5 full → wait up to 2s for slot
  │     └─ Slot freed? → use glm-5 (preferred model preserved)
  │
  ├─ 3. Timeout → try fallback models in STRICT PRIORITY order:
  │     ├─ glm-5.1 (priority 100) → skip if >2 tiers below requested
  │     ├─ glm-5-turbo (priority 90)
  │     ├─ glm-5 (priority 80)
  │     ├─ glm-4.7 (priority 70)
  │     ├─ glm-4.6 (priority 60)
  │     └─ glm-4.5 (priority 50) → only if within tier gap
  │
  └─ 4. All models full → release global slot → block-wait on requested model (30s timeout)
                                            → re-acquire global slot after model slot obtained
```

**Wait-then-fallback trade-off**: Adds 0-2s latency when series 5 is temporarily full,
but results in using higher-quality models more often. If series 5 is genuinely saturated,
falls back to series 4 after the 2s wait.

**Global slot starvation prevention**: When all models are full and `Acquire()` needs to block-wait on the requested model, it first **releases the global slot** before blocking, then **re-acquires** it after obtaining a model slot. This prevents a scenario where many requests hold global slots while waiting for a single popular model, starving other requests that could use different models.

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

# Total concurrent across all models (0 = unlimited)
UPSTREAM_GLOBAL_LIMIT=9

# Probe multiplier for adaptive limit discovery
UPSTREAM_PROBE_MULTIPLIER=5
```

### Example

```
Models configured: glm-5.1:1, glm-5-turbo:1, glm-5:2, glm-4.7:2, glm-4.6:3, glm-4.5:10
Total model capacity: 1+1+2+2+3+10 = 19
Global cap: 9 concurrent

Selection: Strict priority order (5.x always preferred before 4.x)

Low load example (5 requests for glm-5):
  req 1 → glm-5 slot (limit 2)         ✅ direct
  req 2 → glm-5 slot                    ✅ direct
  req 3 → glm-5.1 (fallback)            ✅ 5.x preferred
  req 4 → glm-5-turbo (fallback)        ✅ 5.x preferred
  req 5 → glm-4.7 (fallback)            ✅ all 5.x full, start 4.x
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
{"msg":"model limiter configured","model":"glm-5.1","limit":1}
{"msg":"model limiter configured","model":"glm-5-turbo","limit":1}
{"msg":"model limiter configured","model":"glm-5","limit":2}
{"msg":"model limiter configured","model":"glm-4.7","limit":2}
{"msg":"model limiter configured","model":"glm-4.6","limit":3}
{"msg":"model limiter configured","model":"glm-4.5","limit":10}
{"msg":"model limiter initialized","models":["glm-5.1","glm-5-turbo","glm-5","glm-4.7","glm-4.6","glm-4.5"],"global_limit":15}
{"msg":"model fallback","requested":"glm-5","selected":"glm-5-turbo"}
{"msg":"all models full, waiting","requested":"glm-5"}
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
- Headers ส่งตรง: `Content-Type`, `x-api-key`, `anthropic-version`
- Response headers ส่งตรงทั้งหมด
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

## 9. Metrics & Observability

### Gateway Metrics (Prometheus — port 8080)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_request_latency_seconds` | Histogram | method, path, status | Request latency |
| `api_gateway_queue_depth` | Gauge | | Queue depth (polled on scrape) |
| `api_gateway_error_total` | Counter | type | Error count |
| `api_gateway_rate_limit_hits_total` | Counter | key | Rate limit hits |
| `api_gateway_active_connections` | Gauge | | Active connections |
| `api_gateway_token_input_total` | Counter | model | Input tokens consumed by model |
| `api_gateway_token_output_total` | Counter | model | Output tokens generated by model |

> **หมายเหตุ**: Status label ใน latency histogram ปัจจุบัน hardcode เป็น "200" เสมอ (metrics middleware ไม่ได้ดึง status จริงจาก response writer wrapper ใน chi middleware chain)

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

## 14. Model Selection Priority (Adaptive Limiter with Wait-then-Fallback)

### Fallback Order

เมื่อ requested model เต็ม ระบบจะรอ 2s ก่อน fallback ตามลำดับ:

```
Priority: High-tier → Low-tier (strict order, 5.x always before 4.x)

glm-5.1 (1) → glm-5-turbo (1) → glm-5 (2) → glm-4.7 (2) → glm-4.6 (3) → glm-4.5 (10)
Series 5: 4 slots                          Series 4: 15 slots
Global cap: 9 concurrent

### Selection Algorithm (Wait-then-Fallback)

Request: { "model": "glm-5", ... }

Step 1: Try requested model (non-blocking CAS acquire)
        glm-5 (limit 2) → available? → use glm-5
                          → full? → Step 2

Step 2: Wait up to 2s for slot on requested model
        glm-5 → slot freed? → use glm-5 (preferred)
                              → timeout? → Step 3

Step 3: Try fallback models in strict priority order (skip >2 tier gap):
        1. glm-5.1 (limit 1)  → available? → fallback to glm-5.1
        2. glm-5-turbo (limit 1) → available? → fallback to glm-5-turbo
        3. glm-4.7 (limit 2)  → available? → fallback to glm-4.7
        4. glm-4.6 (limit 3)  → available? → fallback to glm-4.6
        5. glm-4.5 (limit 10) → available? → fallback to glm-4.5

Step 4: All models full or global cap (15) reached → block-wait on requested model

Key: Wait-then-fallback gives series 5 a 2s window before downgrading.
     This means more requests use higher-quality models at the cost of up to 2s extra latency
     when series 5 is genuinely saturated.

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

### Global Slot Starvation Prevention

**Fixed**: `AdaptiveLimiter.Acquire()` releases the global slot before blocking-waiting on a model, then re-acquires it after. Previously, many requests could hold global slots while blocked on a popular model, starving requests that could use other models.

---

*Architecture docs v1.5 — updated with global slot starvation fix, key pool RPM fix, context-aware backoff, unified docker-compose defaults*
