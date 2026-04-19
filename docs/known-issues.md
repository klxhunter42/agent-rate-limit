# Known Issues & Limitations

> สิ่งที่ยังไม่สมบูรณ์, dead code, และข้อจำกัดของระบบ
> รวบรวมจากการ audit codebase ทั้งหมด

---

## สารบัญ

1. [Dead Code & Unused Config](#1-dead-code--unused-config)
2. [Prometheus Metrics ไม่ทำงานเต็มที่](#2-prometheus-metrics-ไม่ทำงานเต็มที่)
3. [OpenTelemetry เป็น No-op ใน Worker](#3-opentelemetry-เป็น-no-op-ใน-worker)
4. [Key Rotation เป็น Destructive](#4-key-rotation-เป็น-destructive)
5. [Retry Logic Issues](#5-retry-logic-issues)
6. [Rate Limit Detection กว้างเกินไป](#6-rate-limit-detection-กว้างเกินไป)
7. [Gateway Metrics Status Hardcode](#7-gateway-metrics-status-hardcode)
8. [Single Redis Connection ใน Worker](#8-single-redis-connection-ใน-worker)
9. [Security Considerations](#9-security-considerations)
10. [Vision Routing Limitations](#10-vision-routing-limitations)
11. [Quota Placeholders](#11-quota-placeholders)
12. [Resolved Issues (Recent Session)](#12-resolved-issues-recent-session)

---

## 1. Dead Code & Unused Config

### Worker: `retry_queue_name` (never used)

```python
# config.py
retry_queue_name: str = "ai_jobs_retry"  # ← ประกาศแต่ไม่มี code ใช้
```

Retry jobs ถูก `LPUSH` กลับไป main queue (`ai_jobs`) ไม่ได้ใช้ retry queue แยก

### Worker: `short_cache_ttl` (never used)

```python
# config.py
short_cache_ttl: int = 60  # ← ประกาศแต่ไม่มี code ใช้
```

### Worker: `PrometheusExporter` class (never called)

```python
# main.py
class PrometheusExporter:
    def export(self):  # ← ไม่เคยถูกเรียก
```

Class ถูกสร้างแต่ method `export()` ไม่เคยถูกเรียก — Prometheus counters/histograms ไม่เคยถูก increment

### Worker: `KeyManager._index` dict (never read)

```python
# key_manager.py
self._index = {provider: 0 for provider in pools}  # ← ไม่เคยถูกอ่าน/เขียน
```

เป็น leftover จากแผนที่จะใช้ round-robin key selection แต่เปลี่ยนเป็น random แทน

---

## 2. Prometheus Metrics ไม่ทำงานเต็มที่

### ปัญหา

Worker declare Prometheus metrics แต่ **ไม่เคย increment/observe**:

```python
# main.py — ประกาศแต่ไม่ใช้
JOBS_PROCESSED = Counter(...)      # ← ไม่เคย .inc()
JOBS_FAILED = Counter(...)         # ← ไม่เคย .inc()
JOBS_RETRIED = Counter(...)        # ← ไม่เคย .inc()
PROVIDER_LATENCY = Histogram(...)  # ← ไม่เคย .observe()
```

Metrics จริงอยู่ใน in-process `Metrics` class (port 9091 `/metrics-internal`) ไม่ใช่ Prometheus

**Token metrics** (`ai_worker_token_input_total`, `ai_worker_token_output_total`) ถูก increment แล้ว:
```python
# worker.py — token tracking ทำงานแล้ว
pm.TOKEN_INPUT.labels(provider=response.provider, model=response.model).inc(prompt_tokens)
pm.TOKEN_OUTPUT.labels(provider=response.provider, model=response.model).inc(completion_tokens)
```

### ผลกระทบ

- Grafana dashboard ที่ query `ai_worker_jobs_processed_total` จะได้ค่าว่างเสมอ
- ต้องใช้ internal metrics (port 9091) แทน หรือแก้ code ให้ increment Prometheus counters

### แก้ไข (ถ้าต้องการ)

```python
# worker.py — ใน _process_job() หลังสำเร็จ:
from main import JOBS_PROCESSED, PROVIDER_LATENCY
JOBS_PROCESSED.labels(provider=provider_name).inc()
PROVIDER_LATENCY.labels(provider=provider_name).observe(elapsed)
```

---

## 3. OpenTelemetry เป็น No-op ใน Worker

### ปัญหา

```python
# config.py
otel_endpoint: str = "http://otel-collector:4317"  # ← มี config
```

แต่ไม่มี tracing code ใดๆ ใน worker:
- ไม่มี `tracer.start_span()`
- ไม่มี `trace.get_tracer()`
- Dependencies ติดตั้งแล้ว (`opentelemetry-api`, `opentelemetry-sdk`, `opentelemetry-exporter-otlp-proto-grpc`) แต่ไม่ import ใช้

### ผลกระทบ

- Worker traces ไม่ปรากฏใน OTel collector
- มีแค่ Gateway traces (Go SDK)

### Gateway OTel

Gateway มี OTel tracing ทำงานจริง:
```go
// main.go
func initTracer(endpoint string) func() {
    exp, err := otlptracegrpc.New(ctx, ...)
    tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp))
    otel.SetTracerProvider(tp)
}
```

---

## 4. Key Rotation เป็น Destructive

### ปัญหา

เมื่อ key ถูก rotate (เพราะ rate limit error) → key ถูก **ลบถาวร** จาก pool:

```python
# key_manager.py
async def rotate_key(self, provider: str, failed_key: str) -> str | None:
    pool = self._pools[provider]
    pool.remove(failed_key)  # ← ลบถาวร ไม่มีการคืน
```

### ผลกระทบ

```
Initial keys: [key1, key2, key3]

key1 → 429 → rotate → keys = [key2, key3]
key2 → 429 → rotate → keys = [key3]
key3 → 429 → rotate → keys = []
→ provider "glm" DEAD จนกว่าจะ restart worker
```

### ทางเลือกแก้

- ใช้ cooldown timer แทนการลบ (เช่น ห้ามใช้ key นั้น 60 วินาที)
- ใช้ circuit breaker pattern
- Reset pool ทุก N นาที

---

## 5. Retry Logic Issues

### Retry ได้ priority กว่า job ใหม่

```python
# worker.py
async def _retry_job(self, job_data: dict):
    await self.redis.lpush(self.queue_name, job_json)  # LPUSH → head of queue
```

`LPUSH` ใส่ที่ head, `BRPOP` ดึงจาก tail → retried job ถูก process ก่อน job เก่า:

```
Queue: [job5, job4, job3, job2, job1]   ← BRPOP ดึง job1 ก่อน
LPUSH retry_job: [retry_job, job5, job4, job3, job2, job1]
                            ↑ BRPOP ยังดึง job1 ก่อน... แต่ถ้ามีหลาย retry
```

> **หมายเหตุ**: จริงๆ แล้ว BRPOP ดึงจาก tail (job1) ก่อน LPUSH ใส่ที่ head ดังนั้น retry job จะอยู่ท้ายสุดของ queue และถูกดึงเป็นลำดับสุดท้าย ไม่ได้มี priority กว่า — **นี่เป็น behavior ที่ถูกต้อง**

### Backoff บล็อก worker slot

```python
# worker.py
await asyncio.sleep(backoff)  # ← worker ตัวนี้ idle ชั่ว backoff duration
```

ถ้า `base_backoff=1.0`, `max_retries=3`:
- Retry 1: sleep 1-1.5s
- Retry 2: sleep 2-3s
- Retry 3: sleep 4-6s

Worker slot ถูก block รวม ~7-10 วินาทีต่อ retry cycle

---

## 6. Rate Limit Detection กว้างเกินไป

### ปัญหา

```python
# worker.py
def _is_rate_limit_error(self, error: Exception) -> bool:
    error_str = str(error).lower()
    if "429" in error_str or "rate_limit" in error_str or "rate limit" in error_str:
        return True
    status_code = getattr(error, "status_code", None)
    if status_code in (429, 502, 503, 504):
        return True
    return False
```

**502, 503, 504 ไม่ใช่ rate limit error** — เป็น server error แต่ถูก treat เหมือน rate limit → trigger key rotation ที่ไม่จำเป็น

### ผลกระทบ

- Provider มีปัญหาชั่วคราว (502/503) → key ถูก rotate ทิ้ง
- ถ้ามี transient error รุนแรง → keys หมดเร็วกว่าที่ควรจะเป็น

### แก้ไข

```python
# แยก rate limit error จาก server error
if status_code == 429:
    return True  # rate limit → key rotation
if status_code in (502, 503, 504):
    return False  # server error → fallback to next provider (ไม่ rotate key)
```

---

## 7. Gateway Metrics Status Hardcode

### ปัญหา

```go
// metrics/metrics.go — Middleware()
status := "200"  // ← hardcode ไม่ได้อ่านจาก response
m.RequestLatency.WithLabelValues(r.Method, routePattern, status).Observe(duration)
```

`responseWriter` wrapper มีอยู่ใน `middleware/logging.go` แต่ metrics middleware ไม่ได้ใช้ → status เป็น "200" เสมอ

### ผลกระทบ

- Latency histogram ไม่แยกตาม status code (4xx, 5xx ทั้งหมดเป็น "200")
- Error rate ใน Grafana dashboard อาจไม่ถูกต้อง

### แก้ไข

ใช้ `middleware/logging.go:responseWriter` ใน metrics middleware ด้วย:

```go
func (m *Metrics) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        wrapped := newResponseWriter(w)
        next.ServeHTTP(wrapped, r)
        status := strconv.Itoa(wrapped.status)
        m.RequestLatency.WithLabelValues(r.Method, routePattern, status).Observe(duration)
    })
}
```## 8. Single Redis Connection ใน Worker

### ปัญหา

```python
# worker.py
async def start(self):
    self.redis = aioredis.from_url(self.redis_url)  # ← 1 connection shared by N coroutines
```

ทุก worker coroutine ใช้ Redis connection เดียวกัน — ไม่มี connection pooling

### ผลกระทบ

- ถ้า WORKER_CONCURRENCY=50 → 50 coroutines แย่ง connection เดียว
- อาจเกิด bottleneck ตรง Redis operations

### แก้ไข

```python
self.redis = aioredis.from_url(self.redis_url, max_connections=worker_concurrency)
```

---

## 9. Security Considerations

### API Key ใน Test Scripts

ถูกเอาออกแล้ว — script ใช้ `${API_KEY:-}` แทน hardcoded key
ต้อง export `API_KEY` ก่อนรัน:

```bash
export API_KEY="your-key-here"
bash scripts/conversation-test.sh
```

### Rate Limiter Admin API ไม่มี Auth

Rate limiter admin endpoints (`/admin/*`, `/api/ratelimit/config/*`) ไม่มี authentication:
- อยู่ใน internal network เท่านั้น (ไม่ expose port)
- แต่ถ้า container อื่นถูก compromise → สามารถแก้ rate limit config ได้

### Dragonfly ไม่มี Password

```yaml
# docker-compose.yml
arl-dragonfly:
  command:
    - "--maxmemory=2gb"
    # ไม่มี --requirepass
```

ไม่มี password protection — แต่อยู่ใน internal network

---

## 10. Vision Routing Limitations

### SSE streaming รองรับแล้วสำหรับ vision

Vision requests ขณะนี้รองรับ SSE streaming แล้ว -- Zhipu SSE chunks ถูก convert เป็น Anthropic SSE format แบบ real-time (stream=true)

### Privacy pipeline ข้าม vision path

`privacy.MaskRequest()` ไม่ทำงานบน vision requests

**Files**: `api-gateway/handler/handler.go`

### tool_use บน vision requests อาจไม่ทำงาน

การแปลง format ระหว่าง Anthropic <-> Zhipu อาจทำให้ tool definitions ไม่สมบูรณ์

**Workaround**: ใช้ text model สำหรับ tool loop, ใช้ vision model เฉพาะวิเคราะห์รูป

### ไม่มี auto-resize รูปภาพ

รูปขนาดใหญ่ (>=10MB) จะถูกปฏิเสติ รูปขนาดกลางอาจช้า

**Workaround**: ย่อรูปก่อนส่ง, หรือใช้ URL image แทน base64

### server_tool_use blocks ถูก strip

Content blocks ประเภท `server_tool_use` จะถูกลบออกก่อนส่งไป upstream

**เหตุผล**: GLM ไม่รองรับ content type นี้, ส่งไปจะได้ error

---

## 11. Quota Enforcement (Now Wired)

### Status: Enforced (with placeholder data)

Quota enforcement is now wired into the `Messages()` handler:
- >= 95% quota: returns 429
- >= 80% quota: broadcasts `quota-warning` via WebSocket
- Fail-open on errors

The `CheckQuota(provider, accountID, model)` method is implemented in `handler/quota.go`.

**Remaining limitation**: Quota percentages still use placeholder data for some providers (Claude, Gemini) rather than real API quota values. Z.AI models use accurate pricing from docs.z.ai. The enforcement mechanism is real and functional -- the data source just needs to be connected to real provider APIs.

### แก้ไข (ถ้าต้องการ real quota data)

Integrate with Anthropic usage API and Google AI Studio quota API to fetch real data instead of placeholders.

**Files**: `api-gateway/handler/quota.go`, `api-gateway/handler/handler.go` lines ~314-330

---

## 12. Resolved Issues (Recent Session)

### Global Slot Starvation Under Heavy Load (FIXED)

**Problem**: When all model slots were full, `AdaptiveLimiter.Acquire()` would hold the global slot while blocking on a model semaphore. Under heavy load, many requests could hold global slots while waiting for the same popular model, starving requests that could use other models.

**Fix**: `Acquire()` now releases the global slot before entering the blocking wait on the requested model, then re-acquires it after obtaining a model slot. This prevents global slot hoarding.

**Files**: `api-gateway/middleware/adaptive_limiter.go`

### Key Pool RPM Wasted on Bad Requests (FIXED)

**Problem**: `keyPool.Acquire()` was called early in the Messages handler, before request body validation. Malformed or oversized requests would consume RPM budget from a key even though the request would never reach upstream.

**Fix**: `keyPool.Acquire()` is now called after body read, size check, and JSON parse. Only valid requests consume RPM budget.

**Files**: `api-gateway/handler/handler.go`

### Retry Backoff Ignores Context Cancellation (FIXED)

**Problem**: Upstream retry backoff used `time.Sleep(backoff)`, which ignored client disconnect. A cancelled request would still occupy a goroutine for the full backoff duration.

**Fix**: Retry backoff now uses `select` with `r.Context().Done()`:
```go
select {
case <-time.After(backoff):
    // proceed with retry
case <-r.Context().Done():
    return fmt.Errorf("request cancelled during retry backoff: %w", r.Context().Err())
}
```

**Files**: `api-gateway/proxy/anthropic.go`

### Docker-Compose Default Mismatch (FIXED)

**Problem**: Gateway and worker had different default values for `UPSTREAM_DEFAULT_LIMIT` and `UPSTREAM_GLOBAL_LIMIT` in docker-compose.yml, causing inconsistent behavior.

**Fix**: Both gateway and worker now default to `UPSTREAM_DEFAULT_LIMIT=1`, `UPSTREAM_GLOBAL_LIMIT=9`.

**Files**: `docker-compose.yml`

### Vision SSE Streaming (FIXED)

**Problem**: Vision requests did not support SSE streaming -- response arrived in one shot.

**Fix**: Implemented `convertZhipuStreamResponse()` that converts Zhipu SSE chunks (OpenAI format) to Anthropic SSE events in real-time. Supports both `delta.content` and `delta.reasoning_content`.

**Files**: `api-gateway/proxy/anthropic.go`

### Vision Model Auto-Select (FIXED)

**Problem**: All vision requests used whatever model the client requested, no intelligence around model selection.

**Fix**: Gateway now analyzes image payload (total base64 bytes + count) and auto-selects optimal vision model: `glm-4.6v` for normal payloads, `glm-4.6v-flashx` for heavy payloads.

**Files**: `api-gateway/handler/handler.go`

---

## Summary

| # | Issue | Severity | Status |
|---|-------|----------|--------|
| 1 | Dead code (retry queue, PrometheusExporter, _index) | Low | Known |
| 2 | Prometheus metrics ไม่ increment | Medium | Known |
| 3 | OTel tracing no-op in worker | Low | Known |
| 4 | Key rotation destructive | Medium | Known |
| 5 | Retry LPUSH behavior | Low | Known (correct) |
| 6 | 502/503/504 trigger key rotation | Medium | Known |
| 7 | Metrics status hardcode "200" | Medium | Known |
| 8 | Single Redis connection | Low | Known |
| 9 | No auth on admin APIs | Low | Known (internal only) |
| 10 | Vision routing limitations | Low | Known |
| 11 | Quota enforcement wired, placeholder data | Medium | Partial Fix |
| 12a | Global slot starvation | High | **Fixed** |
| 12b | Key pool RPM wasted on bad requests | Medium | **Fixed** |
| 12c | Retry backoff ignores context cancellation | Medium | **Fixed** |
| 12d | Docker-compose default mismatch | Low | **Fixed** |

---

*Known Issues v1.3 -- updated: quota enforcement now wired (placeholder data remains)*
