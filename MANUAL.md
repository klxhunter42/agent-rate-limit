# Multi-Agent AI Rate-Limited System — Manual

> ระบบ Multi-Agent AI พร้อม Rate Limiting แบบ Distributed
> รองรับ Claude Code, Batch Agents, และ Multi-Provider Fallback

---

## สารบัญ

1. [ภาพรวมระบบ (Architecture)](#1-ภาพรวมระบบ-architecture)
2. [Traffic Flow](#2-traffic-flow)
3. [เทคโนโลยีที่ใช้](#3-เทคโนโลยีที่ใช้)
4. [การติดตั้งและรัน](#4-การติดตั้งและรัน)
5. [การตั้งค่า Environment Variables](#5-การตั้งค่า-environment-variables)
6. [การใช้งานกับ Claude Code](#6-การใช้งานกับ-claude-code)
7. [API Endpoints](#7-api-endpoints)
8. [Grafana Dashboard](#8-grafana-dashboard)
9. [Rate Limiter Web Dashboard](#9-rate-limiter-web-dashboard)
9.1. [Gateway Dashboard UI](#91-gateway-dashboard-ui)
10. [Distributed Rate Limiter Management API](#10-distributed-rate-limiter-management-api)
11. [Prometheus & Observability](#11-prometheus--observability)
12. [Cost Calculator](#12-cost-calculator)
13. [Docker Management Commands](#13-docker-management-commands)
14. [การเพิ่ม AI Provider](#14-การเพิ่ม-ai-provider)
16. [การแก้ปัญหา (Troubleshooting)](#15-การแก้ปัญหา-troubleshooting)
17. [Vision Auto-Routing (รูปภาพ)](#17-vision-auto-routing-รูปภาพ)
18. [Multi-Agent และการเลือกโหมด](#18-multi-agent-และการเลือกโหมด)

---

## 1. ภาพรวมระบบ (Architecture)

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Multi-Agent AI System                        │
│                                                                     │
│  ┌──────────┐    ┌──────────────┐    ┌──────────────┐              │
│  │  Client   │───▶│  API Gateway │───▶│ Rate Limiter │              │
│  │(Claude/   │    │   (Go)       │    │  (Java/Spring)│              │
│  │  Agent)   │    │  :8080       │    │  :8080       │              │
│  └──────────┘    └──────┬───────┘    └──────┬───────┘              │
│                         │                    │                      │
│                    ┌────▼────┐          ┌────▼────┐                 │
│                    │Dragonfly│◀─────────│  Token   │                │
│                    │(Redis)  │          │  Bucket  │                │
│                    │ :6379   │          │  Store   │                │
│                    └────┬────┘          └─────────┘                 │
│                         │                                           │
│                    ┌────▼────────────────────────┐                  │
│                    │     AI Worker (Python)       │                  │
│                    │  ┌──┬──┬──┬──┬──┬──┬──┬──┐  │  WORKER_CONCURRENCY=50  │
│                    │  │W0│W1│W2│..│..│..│..│W49│ │                  │
│                    │  └──┴──┴──┴──┴──┴──┴──┴──┘  │                  │
│                    │  Per-Model Semaphores:       │                  │
│                    │  glm-5.1(1) glm-5-turbo(1)   │                  │
│                    │  glm-5(2) glm-4.7(2) glm-4.6(3)│                │
│                    │  glm-4.5(10)                  │                  │
│                    │  Vision: glm-4.6v(10) glm-4.5v(10)│             │
│                    │  glm-4.6v-flashx(3) glm-4.6v-flash(1)│         │
│                    │  RPM Limiter: glm:5 req/min  │                  │
│                    │  Global Limit: 9 concurrent │                  │
│                    └─────┼────────────────────────┘                  │
│                          │       │       │                           │
│              ┌───────────▼───────▼───────▼──────────┐               │
│              │   Provider Fallback Chain             │               │
│              │   glm → openai → anthropic → gemini  │               │
│              │   → openrouter                        │               │
│              └───────────────────────────────────────┘               │
│                                                                     │
│  ┌──────────────── Observability Stack ─────────────────┐          │
│  │  OpenTelemetry → Prometheus → Grafana                │          │
│  │  Rate Limiter Dashboard (React) :8081                │          │
│  └──────────────────────────────────────────────────────┘          │
└─────────────────────────────────────────────────────────────────────┘
```

### ส่วนประกอบของระบบ

| Service | Technology | Port | หน้าที่ |
|---------|-----------|------|--------|
| **arl-gateway** | Go (chi router) | 8080 (external) | รับ request, rate limit check, proxy/queue |
| **arl-rate-limiter** | Java 21 / Spring Boot | 8080 (internal) | Token bucket rate limiting, admin API |
| **arl-dragonfly** | DragonflyDB (Redis-compatible) | 6379 (internal) | Cache, queue, rate limit state |
| **arl-worker** | Python 3.12 (asyncio + httpx) | 9090/9091 (internal) | ประมวลผล AI jobs, provider fallback |
| **arl-rl-dashboard** | React + Vite + nginx | 8081 (external) | Rate limiter web management UI |
| **arl-dashboard** | React + Vite + Bun + nginx | 8082 (external) | Gateway dashboard UI (model limits, metrics, controls) |
| **arl-prometheus** | Prometheus | 9090 (internal) | Metrics collection |
| **arl-grafana** | Grafana | 3000 (external) | Dashboard & visualization |
| **arl-otel** | OpenTelemetry | 4317/4318 (internal) | Trace & metric pipeline |

---

## 2. Traffic Flow

### โหมด Sync (สำหรับ Claude Code)

```
Claude Code
  │
  │ POST /v1/messages (Anthropic API format)
  │ Header: x-api-key: <your-key>
  │ Header: anthropic-version: 2023-06-01
  │
  ▼
API Gateway (:8080)
  │
  ├─ Rate Limit Check (per x-api-key)
  │   │
  │   ▼
  │  Rate Limiter → Dragonfly (token bucket)
  │   │
  │   ├─ ถ้าผ่าน: ส่งต่อไป upstream
  │   └─ ถ้าไม่ผ่าน: ตอบ 429 Rate Limit Error (Anthropic format)
  │
  ├─ Content Filter (strip server_tool_use/tool_use/tool_result, convert image format, prepend system to user)
  │
  ├─ ไม่มีรูป (Text Request):
  │     ▼
  │   Upstream Provider (https://api.z.ai/api/anthropic)
  │     │
  │     ▼
  │   SSE Streaming Response → ส่งกลับ Claude Code chunk by chunk
  │
  └─ มีรูป (Image Request - auto-detected):
        │
        ├─ analyzeImagePayload(): นับรูป + ขนาด base64
        ├─ selectVisionModel():
        │     score = totalKB + (imageCount * 300)
        │     <= 2000 และ < 3 รูป → glm-4.6v (10 slots)
        │     > 2000 หรือ >= 3 รูป → glm-4.6v-flashx (3 slots)
        │
        ├─ anthropicToZhipu(): แปลง format Anthropic → OpenAI/Zhipu
        │
        ▼
      Native Zhipu Vision (open.bigmodel.cn/api/paas/v4/chat/completions)
        │
        ├─ stream=true:  convertZhipuStreamResponse()
        │     Zhipu SSE → Anthropic SSE (real-time chunk by chunk)
        │
        └─ stream=false: zhipuToAnthropic()
              Zhipu JSON → Anthropic JSON
        │
        ▼
      Response กลับ Claude Code (Anthropic format, เหมือนเดิม)
```

### โหมด Async (สำหรับ Batch Agents)

```
Agent / Application
  │
  │ POST /v1/chat/completions
  │ Body: { model, messages, provider?, ... }
  │
  ▼
API Gateway (:8080)
  │
  ├─ Rate Limit Check
  │
  ├─ Push job เข้า Dragonfly queue (ai_jobs)
  │
  ▼
AI Worker (BRPOP from queue)
  │  50 workers รันพร้อมกัน
  │
  ├─ ดึง job จาก queue
  ├─ Acquire model slot (per-model semaphore)
  │   ├─ ลอง model ที่ request: non-blocking acquire
  │   ├─ เต็ม? → ลอง fallback models อัตโนมัติ (glm-5 series ก่อน)
  │   └─ ทุก model เต็ม? → รอจนกว่าจะมี slot ว่าง
  ├─ เลือก provider (fallback chain)
  ├─ ยิง API ไป provider ที่เลือก
  ├─ เก็บ result ใน Dragonfly (TTL: 600s)
  │
  ▼
Client ดึง result: GET /v1/results/{job_id}
```

---

## 3. เทคโนโลยีที่ใช้

### API Gateway (Go)
- **chi** — HTTP router (lightweight, idiomatic Go)
- **go-redis** — Redis/Dragonfly client
- **net/http** — Standard library HTTP client สำหรับ proxy
- **prometheus/client_golang** — Prometheus metrics

### Rate Limiter (Java)
- **Spring Boot 3** — Web framework
- **Spring Data Redis** — Redis integration (Lettuce client)
- **Spring Actuator** — Health & metrics endpoints
- **Token Bucket** — Rate limiting algorithm

### AI Worker (Python)
- **asyncio** — Async runtime (built-in, ไม่ใช้ FastAPI/Flask เพราะ worker เป็น background consumer ไม่รับ HTTP request)
- **httpx** — Async HTTP client สำหรับเรียก AI provider APIs
- **redis (hiredis)** — Async Redis client สำหรับ queue operations
- **anthropic / openai / google-generativeai** — Provider SDKs
- **pydantic-settings** — Config management (อ่านจาก env vars)
- **structlog** — Structured JSON logging
- **prometheus-client** — Prometheus metrics export
- **OpenTelemetry SDK** — Distributed tracing

> **ทำไมไม่ใช้ FastAPI/Flask?**
> AI Worker เป็น **background job consumer** — รัน `BRPOP` ตลอดเวลาเพื่อดึง job จาก queue ไม่มี HTTP server ที่รับ request จากภายนอก (มีแค่ metrics server) จึงไม่ต้องใช้ web framework ใช้ `asyncio` event loop จัดการ coroutine 50 ตัวได้โดยตรง

### Rate Limiter Dashboard (React)
- **React 18** — UI framework
- **Vite** — Build tool
- **Recharts** — Charts
- **TailwindCSS + shadcn/ui** — Styling & components
- **React Router** — Client-side routing
- **nginx** — Static file serving + API proxy

---

## 4. การติดตั้งและรัน

### ข้อกำหนดเบื้องต้น

- Docker Desktop (หรือ Docker Engine + Docker Compose)
- RAM ขั้นต่ำ 4GB (แนะนำ 8GB+)
- Disk space ขั้นต่ำ 5GB

### ขั้นตอนติดตั้ง

```bash
# 1. Clone โปรเจกต์
git clone <repo-url>
cd agent-rate-limit

# 2. สร้าง .env จาก template
cp .env.example .env

# 3. แก้ .env — ใส่ API keys ที่ต้องการ
# อย่างน้อยต้องใส่ GLM_API_KEYS
vim .env

# 4. รันทุกอย่าง
docker-compose up -d --build

# 5. ตรวจสอบว่าทุก service healthy
docker-compose ps
```

### การตรวจสอบ

```bash
# ดู status ทุก service
docker-compose ps

# ดู logs ทั้งหมด
docker-compose logs -f

# ดู logs เฉพาะ service
docker-compose logs -f arl-gateway
docker-compose logs -f arl-worker
docker-compose logs -f arl-rate-limiter
```

### สถานะที่ควรเห็น

```
NAME                     STATUS
arl-gateway              Up (healthy)
arl-rate-limiter         Up (healthy)
arl-dragonfly            Up (healthy)
arl-worker               Up (healthy)
arl-rl-dashboard         Up
arl-prometheus           Up
arl-grafana              Up
arl-otel                 Up
```

---

## 5. การตั้งค่า Environment Variables

ไฟล์ `.env` เก็บการตั้งค่าทั้งหมด คัดลอกจาก `.env.example`:

```bash
cp .env.example .env
```

### API Gateway

| Variable | Default | คำอธิบาย |
|----------|---------|----------|
| `GATEWAY_PORT` | `8080` | Port ที่ gateway รัน (ภายนอก container) |
| `GLOBAL_RATE_LIMIT` | `100` | Rate limit รวมทุก client (req/min) |
| `AGENT_RATE_LIMIT` | `5` | Rate limit ต่อ agent/key (req/min) |
| `WORKER_POOL_SIZE` | `100` | จำนวน goroutine pool สำหรับ async mode |
| `UPSTREAM_URL` | `https://api.z.ai/api/anthropic` | Upstream AI provider endpoint |
| `STREAM_TIMEOUT` | `300s` | Timeout สำหรับ streaming requests |
| `UPSTREAM_MODEL_LIMITS` | `glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3,glm-4.5:10` | Per-model concurrent limits (model:limit comma-separated, รวม 19 slots, global cap 9) |
| `UPSTREAM_DEFAULT_LIMIT` | `1` | Default limit สำหรับ model ที่ไม่ได้ตั้งค่า |
| `UPSTREAM_GLOBAL_LIMIT` | `9` | จำนวน concurrent request สูงสุดรวมทุก model |
| `NATIVE_VISION_URL` | `https://open.bigmodel.cn/api/paas/v4/chat/completions` | Native Zhipu endpoint for vision requests |

### Dragonfly

| Variable | Default | คำอธิบาย |
|----------|---------|----------|
| `DRAGONFLY_MAX_MEMORY` | `6gb` | Memory limit สำหรับ Dragonfly |

### Rate Limiter

| Variable | Default | คำอธิบาย |
|----------|---------|----------|
| `RATE_LIMITER_CAPACITY` | `1000` | Token bucket capacity (จำนวน token สะสมสูงสุด) |
| `RATE_LIMITER_REFILL_RATE` | `100` | Token refill rate (token/second) |

### AI Worker

| Variable | Default | คำอธิบาย | สูงสุด/ข้อจำกัด |
|----------|---------|----------|----------------|
| `WORKER_CONCURRENCY` | `50` | จำนวน worker coroutine ที่รันพร้อมกัน | ขึ้นกับ provider rate limit และ memory |
| `MAX_RETRIES` | `3` | จำนวน retry เมื่อ provider ล้มเหลว | ไม่ควรเกิน 5 (เพิ่ม latency) |
| `BASE_BACKOFF` | `1.0` | Backoff base (วินาที) สำหรับ exponential retry | 0.5-5.0 |
| `RESULT_TTL` | `600` | เวลาเก็บ result (วินาที) | 60-3600 |
| `UPSTREAM_MODEL_LIMITS` | `glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3,glm-4.5:10` | Per-model concurrent limits (เหมือน gateway) | รวมควรเท่ากับ UPSTREAM_GLOBAL_LIMIT |
| `UPSTREAM_DEFAULT_LIMIT` | `1` | Default limit สำหรับ model ที่ไม่ได้ตั้งค่า | - |
| `UPSTREAM_GLOBAL_LIMIT` | `9` | Concurrent request สูงสุดรวมทุก model (must be > 0) | - |
| `PROVIDER_RPM_LIMITS` | `glm:5` | Per-provider RPM limit ป้องกัน 429 (provider:rpm) | ขึ้นกับจำนวน key |

#### WORKER_CONCURRENCY แนะนำ

- **GLM (Z.ai)**: 20-50 (ขึ้นกับ tier ของคุณ)
- **OpenAI**: 20-50 (ถ้ามี rate limit สูง)
- **Anthropic**: 10-30
- **Multi-provider**: ตั้งตาม provider ที่เร็วที่สุด แล้ว fallback จะจัดการเอง

> **สูงสุดที่แนะนำ**: 50 workers (default) — เพียงพอสำหรับการใช้งานปกติ
> **สูงสุดที่เป็นไปได้**: ~200 (ต้องเพิ่ม memory limit ของ ai-worker container เป็น 2G+)
> **ข้อควรระวัง**: ถ้าตั้งสูงเกิน provider rate limit → จะเกิด 429 error และ retry หนัก

### AI Provider Keys

ใส่ API keys แยกด้วย comma สำหรับ key rotation:

```bash
GLM_API_KEYS=key1,key2,key3
GLM_ENDPOINT=https://api.z.ai/api/anthropic
```

| Variable | คำอธิบาย |
|----------|----------|
| `GLM_API_KEYS` | GLM/Z.ai API keys (comma-separated) |
| `GLM_ENDPOINT` | GLM API endpoint |
| `OPENAI_API_KEYS` | OpenAI API keys |
| `ANTHROPIC_API_KEYS` | Anthropic API keys |
| `GEMINI_API_KEYS` | Google Gemini API keys |
| `OPENROUTER_API_KEYS` | OpenRouter API keys |

> **สำคัญ**: ถ้าไม่ใช้ provider ไหน ให้เอาบรรทัดนั้นออกจาก `.env` หรือเว้นว่างไว้ได้เลย ระบบจะข้าม provider ที่ไม่มี key

### Observability

| Variable | Default | คำอธิบาย |
|----------|---------|----------|
| `GRAFANA_PORT` | `3000` | Grafana port (ภายนอก container) |
| `GRAFANA_ADMIN_PASSWORD` | `klxhunter` | รหัสผ่าน admin ของ Grafana |
| `DASHBOARD_PORT` | `8082` | Gateway Dashboard UI port (ภายนอก container) |

---

## 6. การใช้งานกับ Claude Code

### วิธีตั้งค่า

แก้ไข `~/.claude/settings.json`:

```json
{
  "ANTHROPIC_BASE_URL": "http://localhost:8080",
  "ANTHROPIC_AUTH_TOKEN": "your-glm-api-key"
}
```

> **ANTHROPIC_AUTH_TOKEN** ต้องใส่ เพราะ gateway ใช้ค่าจาก header `x-api-key` เพื่อระบุตัวตน + rate limit
> Claude Code จะส่งค่านี้ไปเป็น `x-api-key` header อัตโนมัติ

### สถาปัตยกรรม — ยิงตรง vs ยิงผ่าน Gateway

**ยิงตรง (ไม่ผ่าน gateway):**
```
Claude Code ──POST /v1/messages──▶ api.z.ai/api/anthropic
                                    (ANTHROPIC_BASE_URL)
```

**ยิงผ่าน Gateway:**
```
Claude Code ──POST /v1/messages──▶ Gateway :8080 ──transparent──▶ api.z.ai/api/anthropic
                                    (ANTHROPIC_BASE_URL)
```

**ประสบการณ์ผู้ใช้ต้องเหมือนกันทุกประการ** — Gateway เป็น transparent proxy:
- ไม่ decode/re-encode request/response
- ส่งตรงไปตรงมาทุก byte
- ไม่แตะ field ใดๆ (tools, tool_choice, messages, content, headers)

### วิธีทำงานของ Claude Code (Tool Loop)

```
1. Claude Code ส่ง request พร้อม tools definitions:
   POST /v1/messages
   {
     "model": "glm-5",
     "messages": [{"role": "user", "content": "อ่านไฟล์ main.go ให้หน่อย"}],
     "tools": [
       {"name": "Read", "description": "Read a file...", "input_schema": {...}},
       {"name": "Edit", "description": "Edit a file...", "input_schema": {...}},
       {"name": "Bash", "description": "Run a command...", "input_schema": {...}},
       ...
     ],
     "stream": true
   }

2. Upstream ตอบกลับพร้อม tool_use block:
   {
     "content": [
       {"type": "tool_use", "id": "toolu_xxx", "name": "Read", "input": {"file_path": "/path/main.go"}}
     ],
     "stop_reason": "tool_use"
   }

3. Claude Code execute tool ที่ local (อ่านไฟล์จริงๆ)

4. Claude Code ส่งคำขอต่อพร้อม tool_result:
   POST /v1/messages
   {
     "messages": [
       {"role": "user", "content": "อ่านไฟล์ main.go ให้หน่อย"},
       {"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_xxx", ...}]},
       {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_xxx", "content": "package main..."}]}
     ],
     "tools": [...],
     "stream": true
   }

5. วนลูปจนกว่า stop_reason = "end_turn"
```

### ความเข้ากันได้กับ Claude Code Features

| Feature | ผ่าน Gateway? | เหตุผล |
|---------|:------------:|--------|
| **Tools (Read, Edit, Bash, Write)** | ✅ | Transparent proxy ส่ง `tools` definitions และ `tool_use`/`tool_result` blocks ครบ |
| **Streaming (SSE)** | ✅ | Gateway relay SSE chunk by chunk แบบ real-time |
| **Skills (slash commands)** | ✅ | Skills ถูก expand เป็น prompt ที่ client ก่อนส่ง — gateway เห็นเป็นข้อความธรรมดา |
| **Memory** | ✅ | เก็บในไฟล์ local (`~/.claude/`) — ไม่เกี่ยวกับ API call |
| **Artifacts** | ✅ | แสดงผลจาก response content ที่ client — gateway ไม่แตะ content |
| **MCP Servers** | ✅ | Tools จาก MCP ถูก register ที่ client เหมือน built-in tools |
| **Multi-turn conversation** | ✅ | ทุก message history ส่งไปครบในแต่ละ request |
| **Extended thinking** | ✅ | เป็น content block ชนิดหนึ่ง — gateway ส่งต่อไม่แตะ |

### สิ่งที่ Gateway ทำ (Rate Limit Check)

```
Request เข้ามา
  │
  ├─ ดึง API key จาก header (x-api-key / Authorization: Bearer)
  ├─ เรียก Rate Limiter: POST /api/ratelimit/check {key: "api-key-hash"}
  │   ├─ ผ่าน: ส่ง request ต่อไป upstream แบบไม่แก้ไขอะไรเลย
  │   └─ ไม่ผ่าน: ตอบ 429 (Anthropic error format) ทันที
  │
  ├─ Per-Model Upstream Limiter (Gateway + Worker)
  │   ├─ ดึง model จาก request body
  │   ├─ ลอง acquire slot สำหรับ model ที่ขอ (non-blocking)
  │   ├─ เต็ม? → ลอง fallback models อัตโนมัติ
  │   │   Priority: glm-5.1 → glm-5-turbo → glm-5 → glm-4.7 → glm-4.6 → glm-4.5 (5.x always first)
  │   ├─ ทุก model เต็ม? → รอจนกว่าจะมี slot ว่าง
  │   ├─ RPM Limiter: ควบคุมความเร็ว req/min ต่อ provider
  │   └─ ถ้า fallback → เปลี่ยน model ใน body ก่อนส่งต่อ
  │   19 model slots (global cap 15): glm-5.1(1) + glm-5-turbo(1) + glm-5(2) + glm-4.7(2) + glm-4.6(3) + glm-4.5(10)
  │
  └─ Response กลับ: ส่งตรงไปยัง client แบบไม่แก้ไขอะไรเลย
```

### ข้อจำกัดที่อาจเกิดขึ้น

| ปัญหา | สาเหตุ | วิธีแก้ |
|-------|--------|--------|
| Timeout ระหว่าง tool loop ยาวๆ | `STREAM_TIMEOUT` เริ่มต้น 300s | เพิ่ม `STREAM_TIMEOUT=600s` ใน docker-compose |
| Latency เพิ่มขึ้น | Gateway เพิ่ม hop 1 ชั้น | ปกติเพิ่ม <5ms (เฉพาะ rate limit check) |
| Response ใหญ่ถูกตัด | Proxy buffer size | ปัจจุบันใช้ `io.Copy` ไม่มี buffer limit |
| SSE ไม่ stream แบบ real-time | Flusher ไม่ทำงาน | ตรวจสอบ nginx/reverse proxy ด้านหน้า |

### ทดสอบ

```bash
# Non-streaming
curl -X POST http://localhost:8080/v1/messages \
  -H "x-api-key: YOUR_GLM_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "glm-5",
    "max_tokens": 100,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# Streaming
curl -X POST http://localhost:8080/v1/messages \
  -H "x-api-key: YOUR_GLM_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "glm-5",
    "max_tokens": 100,
    "stream": true,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# พร้อม tools (เหมือนที่ Claude Code ส่งจริงๆ)
curl -X POST http://localhost:8080/v1/messages \
  -H "x-api-key: YOUR_GLM_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "glm-5",
    "max_tokens": 1024,
    "tools": [
      {
        "name": "Read",
        "description": "Read a file from the filesystem",
        "input_schema": {
          "type": "object",
          "properties": {"file_path": {"type": "string"}},
          "required": ["file_path"]
        }
      }
    ],
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### ทดสอบแบบ Conversation (Stress Test)

```bash
# 8-turn conversation test (ไทย + implement + cleanup)
bash scripts/conversation-test.sh

# 10 concurrent requests
bash scripts/stress-test.sh
```

---

## 7. API Endpoints

### API Gateway (`:8080`)

| Method | Path | คำอธิบาย |
|--------|------|----------|
| `POST` | `/v1/messages` | Anthropic-compatible sync proxy (Claude Code) |
| `POST` | `/v1/chat/completions` | Async queue mode (batch agents) |
| `GET` | `/v1/results/{id}` | ดึงผล async job |
| `GET` | `/health` | Health check |
| `GET` | `/metrics` | Prometheus metrics |
| `GET` | `/admin` | Management dashboard (SPA, API key auth) |
| `GET` | `/admin/*` | SPA sub-routes (fallback to index.html) |
| `GET` | `/v1/limiter-status` | Adaptive limiter state (requires x-api-key) |
| `POST` | `/v1/limiter-override` | Set/clear model concurrency limit (requires x-api-key) |

---

## 8. Grafana Dashboard

### การเข้าถึง

```
URL: http://localhost:3000
Username: admin
Password: ดูจาก GRAFANA_ADMIN_PASSWORD ใน .env (default: klxhunter)
```

### Dashboards ที่มี

เข้าได้จาก **Dashboards** → **General** หรือใช้ URL โดยตรง:

| Dashboard | URL | คำอธิบาย |
|-----------|-----|----------|
| **System Overview** | http://localhost:3000/d/arl-overview | ภาพรวมทั้งระบบ — request rate, latency, queue, jobs |
| **API Gateway Detailed** | http://localhost:3000/d/arl-gateway | Gateway metrics เช่น request rate by path, latency percentiles |
| **AI Worker Detailed** | http://localhost:3000/d/arl-worker | Worker metrics เช่น job rate, provider latency, memory |
| **Cost Calculator & Savings** | http://localhost:3000/d/arl-cost | คำนวณค่าใช้จ่าย AI, rate limit savings, cost estimation |

### สิ่งที่ดูได้ในแต่ละ Dashboard

#### System Overview (`arl-overview`)
- Architecture diagram
- Request rate รวมทุก path
- Latency p50/p95/p99
- Active connections, queue depth, active workers
- Jobs processed/failed/retried
- Rate limiter JVM memory

#### API Gateway Detailed (`arl-gateway`)
- Request rate แยกตาม path/method/status
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
- Total requests (24h) แยก sync/async
- Requests/hour average
- Estimated input/output tokens
- Request volume over time
- Estimated daily cost by provider (bar chart)
- Rate limited requests (429s) — cost savings
- Retry & failure rates
- Provider error rate
- Queue depth (backlog cost indicator)

### Pricing Table (อ้างอิงสำหรับ Cost Calculator)

| Provider | Model | Input (per 1M tokens) | Output (per 1M tokens) |
|----------|-------|----------------------|------------------------|
| GLM/Z.ai | glm-5 | $0.50 | $1.50 |
| OpenAI | gpt-4o | $2.50 | $10.00 |
| Anthropic | claude-sonnet-4-6 | $3.00 | $15.00 |
| Gemini | gemini-2.0-flash | $0.10 | $0.40 |
| OpenRouter | varies | varies | varies |

### Metrics ที่มีในระบบ

| Metric | มาจาก | คำอธิบาย |
|--------|-------|----------|
| `api_gateway_request_latency_seconds` | Gateway | Request latency histogram (labels: method, path, status) |
| `api_gateway_active_connections` | Gateway | Active connections |
| `api_gateway_queue_depth` | Gateway | Queue depth |
| `api_gateway_error_total` | Gateway | Errors by type (labels: type — bad_request, validation, queue_push, cache_get, upstream) |
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
| `http_server_requests_seconds_*` | Rate Limiter | HTTP metrics |
| `jvm_memory_*` | Rate Limiter | JVM memory |

---

## 9. Rate Limiter Web Dashboard

### การเข้าถึง

```
URL: http://localhost:8081
```

> ไม่ต้อง login — เข้าได้เลย มี nginx proxy ไปยัง rate-limiter API อัตโนมัติ

### ฟีเจอร์

- **Real-time Monitoring** — Active keys, requests/sec, success rates (อัปเดตทุก 5 วินาที)
- **Algorithm Comparison** — เปรียบเทียบ Token Bucket, Sliding Window, Fixed Window, Leaky Bucket
- **Traffic Simulation** — จำลอง traffic patterns: steady, bursty, spike, custom
- **API Key Management** — สร้าง/แก้ไข/ลบ API keys, IP whitelist/blacklist, usage stats
- **Configuration** — Global & per-key rate limiting rules, pattern-based rules
- **Load Testing** — ทดสอบด้วย constant, ramp-up, spike, step-load patterns
- **Historical Analytics** — Performance trends: 1h, 24h, 7d, 30d
- **Data Export** — CSV/JSON export

---

## 9.1 Gateway Dashboard UI

### การเข้าถึง

| วิธี | URL | หมายเหตุ |
|------|-----|---------|
| Embedded (Go binary) | `http://localhost:8080/admin` | ใช้งานได้หลัง `bun run build` ใน `ui/` |
| Docker Compose | `http://localhost:8082` | Standalone container, nginx proxy ไป gateway |
| Dev mode (hot reload) | `http://localhost:5173` | `cd ui && bun run dev` |

> Login ด้วย Gateway URL + API key (เก็บใน sessionStorage)

### Pages

| Page | Route | ฟีเจอร์ |
|------|-------|---------|
| Overview | `/` | Status, queue depth, total requests, concurrency, model utilization |
| Model Limits | `/model-limits` | ตาราง model status: in-flight, limit, max, ceiling, RTT EWMA, requests, 429s |
| Key Pool | `/key-pool` | API key rotation pool status |
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

Rate limiter รันอยู่ที่ internal port 8080 เข้าถึงได้จากภายใน Docker network:

```bash
docker exec arl-rate-limiter curl -s http://localhost:8080/...
```

หรือผ่าน Rate Limiter Dashboard proxy: `http://localhost:8081/api/...`

### Rate Limit Check

```bash
# POST /api/ratelimit/check
docker exec arl-rate-limiter curl -s -X POST http://localhost:8080/api/ratelimit/check \
  -H "Content-Type: application/json" \
  -d '{"key": "test-user"}'
```

### Rate Limit Config

| Method | Path | คำอธิบาย |
|--------|------|----------|
| `GET` | `/api/ratelimit/config` | ดู config ปัจจุบัน |
| `POST` | `/api/ratelimit/config/keys/{key}` | ตั้ง rate limit เฉพาะ key |
| `POST` | `/api/ratelimit/config/patterns/{pattern}` | ตั้ง rate limit ตาม pattern |
| `POST` | `/api/ratelimit/config/default` | ตั้ง default rate limit |
| `DELETE` | `/api/ratelimit/config/keys/{key}` | ลบ config เฉพาะ key |
| `POST` | `/api/ratelimit/config/reload` | Reload config จาก properties |
| `GET` | `/api/ratelimit/config/stats` | ดู rate limit statistics |

### Admin

| Method | Path | คำอธิบาย |
|--------|------|----------|
| `GET` | `/admin/limits/{key}` | ดู token bucket state ของ key |
| `PUT` | `/admin/limits/{key}` | แก้ไข token bucket ของ key |
| `DELETE` | `/admin/limits/{key}` | ลบ token bucket |
| `GET` | `/admin/keys` | ดู keys ทั้งหมดในระบบ |

### Adaptive Rate Limiting

| Method | Path | คำอธิบาย |
|--------|------|----------|
| `GET` | `/api/ratelimit/adaptive/{key}/status` | ดู adaptive status |
| `POST` | `/api/ratelimit/adaptive/{key}/override` | Override rate limit |
| `DELETE` | `/api/ratelimit/adaptive/{key}/override` | ลบ override |
| `GET` | `/api/ratelimit/adaptive/config` | ดู adaptive config |

### Scheduled Rate Limits

| Method | Path | คำอธิบาย |
|--------|------|----------|
| `POST` | `/api/ratelimit/schedule` | สร้าง schedule |
| `GET` | `/api/ratelimit/schedule` | ดู schedules ทั้งหมด |
| `PUT` | `/api/ratelimit/schedule/{name}` | แก้ไข schedule |
| `DELETE` | `/api/ratelimit/schedule/{name}` | ลบ schedule |
| `POST` | `/api/ratelimit/schedule/{name}/activate` | เปิดใช้ schedule |
| `POST` | `/api/ratelimit/schedule/emergency` | สร้าง emergency rate limit |

### ตัวอย่างการใช้งาน

```bash
# ดู keys ทั้งหมด
docker exec arl-rate-limiter curl -s http://localhost:8080/admin/keys

# ดู token bucket state
docker exec arl-rate-limiter curl -s http://localhost:8080/admin/limits/my-api-key

# ตั้ง rate limit เฉพาะ key
docker exec arl-rate-limiter curl -s -X POST \
  http://localhost:8080/api/ratelimit/config/keys/my-key \
  -H "Content-Type: application/json" \
  -d '{"capacity": 50, "refillRate": 10}'

# ตั้ง default rate limit
docker exec arl-rate-limiter curl -s -X POST \
  http://localhost:8080/api/ratelimit/config/default \
  -H "Content-Type: application/json" \
  -d '{"capacity": 2000, "refillRate": 200}'

# สร้าง schedule (ลด rate limit ช่วง peak)
docker exec arl-rate-limiter curl -s -X POST \
  http://localhost:8080/api/ratelimit/schedule \
  -H "Content-Type: application/json" \
  -d '{"name": "peak-hours", "cronExpression": "0 9 * * 1-5", "capacity": 500, "refillRate": 50, "active": true}'

# Health check
docker exec arl-rate-limiter curl -s http://localhost:8080/actuator/health
```

---

## 11. Prometheus & Observability

### Prometheus Scrape Targets

| Target | Interval | Path |
|--------|----------|------|
| `arl-gateway:8080` | 5s | `/metrics` |
| `arl-worker:9090` | 5s | `/metrics` |
| `arl-rate-limiter:8080` | 10s | `/actuator/prometheus` |
| `arl-otel:8889` | 10s | `/metrics` |
| `arl-prometheus:9090` | 15s | `/metrics` |

### OpenTelemetry Collector

| Protocol | Endpoint | ใช้สำหรับ |
|----------|----------|----------|
| gRPC | `arl-otel:4317` | Traces & metrics ingestion |
| HTTP | `arl-otel:4318` | Traces & metrics ingestion |
| Prometheus | `arl-otel:8889` | Metrics export |

---

## 12. Cost Calculator

Dashboard Cost Calculator อยู่ที่ http://localhost:3000/d/arl-cost

### วิธีใช้

1. เปิด Grafana → **Dashboards** → **Cost Calculator & Savings**
2. เลือกช่วงเวลา (default: 24h)
3. ดู metrics อัตโนมัติ:
   - **Total Requests** — จำนวน request 24h ย้อนหลัง
   - **Requests/hour** — ค่าเฉลี่ย request ต่อชั่วโมง
   - **Est. Tokens** — ประมาณการ tokens ที่ใช้ (input ~500, output ~200 tokens/request)
   - **Daily Cost** — คำนวณจาก tokens × pricing
   - **Rate Limited Requests** — requests ที่ถูก block = **เงินที่ประหยัดได้**

### สูตรคำนวณ

```
Daily Cost = (Input Tokens / 1M) × Input Price + (Output Tokens / 1M) × Output Price
Input Tokens ≈ Jobs Processed × 500 (default estimate)
Output Tokens ≈ Jobs Processed × 200 (default estimate)
```

### Rate Limit Savings

Requests ที่ถูก block ด้วย 429 = เงินที่ไม่ต้องจ่ายให้ provider:
```
Savings = Rate Limited Requests × Average Cost per Request
```

---

## 13. Docker Management Commands

```bash
# === ระบบทั้งหมด ===
docker-compose up -d --build       # เริ่มทั้งหมด
docker-compose down                 # หยุดทั้งหมด
docker-compose restart              # รีสตาร์ททั้งหมด
docker-compose ps                   # ดู status
docker-compose logs -f              # ดู logs real-time

# === Service เดียว ===
docker-compose up -d --build arl-worker      # Rebuild + restart
docker-compose up -d --build arl-dashboard   # Rebuild dashboard UI
docker-compose logs -f arl-gateway          # ดู logs
docker-compose restart prometheus           # Restart

# === ข้อมูล ===
docker stats                              # Resource usage
docker exec -it arl-gateway sh            # Shell ใน container
docker exec -it arl-dragonfly redis-cli       # Dragonfly CLI

# === ทำความสะอาด ===
docker-compose down -v                    # ลบ containers + volumes (reset ข้อมูล)
docker-compose down --rmi all             # ลบ images
```

### คำสั่ง Dragonfly

```bash
docker exec -it arl-dragonfly redis-cli
> INFO                 # Server info
> DBSIZE               # จำนวน keys
> LLEN ai_jobs         # ความยาว queue
> KEYS *               # ดู keys ทั้งหมด (ระวังบน production)
> MEMORY USAGE <key>   # Memory ของ key
> FLUSHALL             # ลบข้อมูลทั้งหมด (ระวัง!)
```

---

## 14. การเพิ่ม AI Provider

เพิ่ม API key ใน `.env`:

```bash
OPENAI_API_KEYS=sk-proj-xxx,sk-proj-yyy
ANTHROPIC_API_KEYS=sk-ant-xxx
GEMINI_API_KEYS=AIzaxxx
OPENROUTER_API_KEYS=sk-or-xxx
```

แล้ว restart ai-worker:

```bash
docker-compose up -d --build arl-worker
```

### Provider Fallback Order

1. **glm** (Z.ai) — Primary
2. **openai**
3. **anthropic**
4. **gemini**
5. **openrouter**

ถ้า provider แรกล้มเหลว จะข้ามไป provider ถัดไปที่มี API key อัตโนมัติ

---

## 15. การแก้ปัญหา (Troubleshooting)

### Service ไม่ healthy

```bash
docker-compose ps                                    # ดู status
docker-compose logs <service> --tail 50              # ดู logs
docker-compose up -d --build <service>               # Rebuild
```

### DOCKER_DEFAULT_PLATFORM

ถ้าเจอ error `platform (linux/amd64) does not match`:

```bash
unset DOCKER_DEFAULT_PLATFORM
# หรือเพิ่มใน ~/.zshrc / ~/.bashrc
```

### ai-worker crash (SettingsError)

```bash
# เช็ค .env ไม่มีค่าว่างที่ผิด format
cat .env | grep API_KEYS
# ถ้าไม่ใช้ provider ไหน ให้ลบบรรทัดนั้นออก หรือเว้นว่าง
```

### Rate Limiter ตอบ 403

```bash
# เช็ค docker profile active
docker exec arl-rate-limiter env | grep SPRING_PROFILES_ACTIVE
# ควรได้: SPRING_PROFILES_ACTIVE=docker
```

### Reset ระบบทั้งหมด

```bash
docker-compose down -v && docker-compose up -d --build
```

> **ระวัง**: `down -v` จะลบ volumes ทั้งหมด รวมถึง Grafana dashboards และข้อมูลใน Dragonfly

---

## สรุป Port ที่ใช้

| Port | Service | External | Protocol |
|------|---------|----------|----------|
| **8080** | API Gateway | Yes | HTTP |
| **8081** | Rate Limiter Dashboard | Yes | HTTP |
| **3000** | Grafana | Yes | HTTP |
| 8080 | Rate Limiter | No | HTTP |
| 6379 | Dragonfly | No | Redis |
| 9090 | AI Worker / Prometheus | No | HTTP |
| 9091 | AI Worker (internal) | No | HTTP |
| 4317 | OTel Collector (gRPC) | No | gRPC |
| 4318 | OTel Collector (HTTP) | No | HTTP |
| 8889 | OTel Collector (Prom) | No | HTTP |

---

## 17. Vision Auto-Routing (รูปภาพ)

Gateway ตรวจจับ image content ใน request อัตโนมัติ แล้ว route ไปยัง native Zhipu vision endpoint แทน z.ai Anthropic endpoint พร้อม **auto-select vision model** ตามขนาดภาพ และ **SSE streaming** แบบ real-time

### Flow Diagram

```
Client ส่ง request พร้อมรูปภาพ
  |
  v
arl-gateway (:8080)
  |-- HasImageContent() scan messages หา image blocks
  |
  +-- ไม่มีรูป: ProxyTransparent -> Z.ai (เหมือนเดิม)
  |
  +-- มีรูป: analyzeImagePayload()
        |-- คำนวณ totalBase64Bytes + imageCount
        |-- selectVisionModel(): score = totalBase64KB + (imageCount * 300)
        |     |-- score <= 2000 && count < 3 -> glm-4.6v (10 slots, best quality)
        |     |-- score > 2000 || count >= 3 -> glm-4.6v-flashx (3 slots, fastest)
        |
        |-- filterUnsupportedContent():
        |     strip server_tool_use blocks
        |     convert Anthropic image -> GLM image_url format
        |
        |-- AnthropicToOpenAI():
        |     Anthropic Messages format -> OpenAI/Zhipu format
        |     image blocks: {source:{type,media_type,data}} -> {image_url:{url}}
        |     system role: text prepend to first user message
        |     strip: server_tool_use, tool_use, tool_result, other unsupported
        |     only pass: user/assistant roles, text/image/image_url content types
        |
        |-- POST to NATIVE_VISION_URL
        |     Bearer auth with API key
        |
        +-- stream=true?
              |-- YES: convertZhipuStreamResponse()
              |     Zhipu SSE (OpenAI format) -> Anthropic SSE events
              |     message_start -> content_block_start -> content_block_delta...
              |     -> content_block_stop -> message_delta -> message_stop
              |
              |-- NO: zhipuToAnthropic()
                    Zhipu JSON -> Anthropic JSON response
```

### Vision Model Auto-Select

Gateway เลือก vision model อัตโนมัติตาม **scoring formula**:

```
score = totalBase64KB + (imageCount * 300)
```

| Score / Condition | Selected Model | Slots | Reason |
|---|---|---|---|
| score <= 2000 && count < 3 | `glm-4.6v` | 10 | Best quality, high capacity |
| score > 2000 or count >= 3 | `glm-4.6v-flashx` | 3 | Fast processing for heavy payloads |

**ตัวอย่าง:**

| Scenario | Total KB | Count | Score | Model |
|---|---|---|---|---|
| 1 screenshot (200KB) | 200 | 1 | 500 | glm-4.6v |
| 1 photo (1.5MB) | 1500 | 1 | 1800 | glm-4.6v |
| 2 photos (1MB each) | 2000 | 2 | 2600 | glm-4.6v-flashx |
| 5 screenshots (100KB each) | 500 | 5 | 2000 | glm-4.6v-flashx |
| 1 large photo (3MB) | 3000 | 1 | 3300 | glm-4.6v-flashx |

### SSE Streaming for Vision

Vision responses **รองรับ SSE streaming** แล้ว -- Zhipu SSE chunks ถูก convert เป็น Anthropic SSE format แบบ real-time:

```
Zhipu SSE (OpenAI format):
  data: {"choices":[{"delta":{"content":"Hello"}}]}

Converted to Anthropic SSE:
  event: content_block_delta
  data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}
```

รองรับทั้ง `delta.content` และ `delta.reasoning_content` จาก Zhipu

### Vision Models ที่รองรับ

| Model | Slots | Status | Notes |
|---|---|---|---|
| `glm-4.6v` | 10 | ✅ แนะนำ | Best quality, highest capacity |
| `glm-4.5v` | 10 | ✅ | Good quality, same capacity |
| `glm-4.6v-flashx` | 3 | ✅ | Fastest, auto-selected for heavy payloads |
| `glm-4.6v-flash` | 1 | ✅ | Fast, not auto-selected (limited slots) |

### Image Format ที่รองรับ

```json
// Anthropic base64 (แปลงอัตโนมัติ)
{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "..."}}

// Anthropic URL (แปลงอัตโนมัติ)
{"type": "image", "source": {"type": "url", "url": "https://..."}}

// แปลงเป็น GLM format ก่อนส่ง:
{"type": "image_url", "image_url": {"url": "data:image/png;base64,..."}}
```

### การตั้งค่า

```bash
# Native Zhipu vision endpoint (default)
NATIVE_VISION_URL=https://open.bigmodel.cn/api/paas/v4/chat/completions

# Vision model concurrency limits (included in UPSTREAM_MODEL_LIMITS)
UPSTREAM_MODEL_LIMITS=...,glm-4.6v:10,glm-4.5v:10,glm-4.6v-flashx:3,glm-4.6v-flash:1
```

### ข้อจำกัด

| ข้อจำกัด | รายละเอียด |
|----------|-----------|
| Privacy pipeline ข้าม | Vision path ไม่ผ่าน privacy masking |
| tool_use บน vision ถูก strip | `server_tool_use`, `tool_use`, `tool_result` content blocks ถูกกรองออกก่อนส่ง (Z.AI ไม่รองรับ) |
| ไม่มี auto-resize | รูปขนาดใหญ่อาจช้า/ล้มเหลว |

> **หมายเหตุ**: Error 1210 ("API 调用参数有误") ที่เคยเกิดจากการส่ง `system` role และ Anthropic-specific content blocks ได้รับการแก้ไขแล้ว (commit 7c08cb0) -- gateway ตอนนี้กรอง role และ content type อัตโนมัติ

---

## 18. Multi-Agent และการเลือกโหมด

### Sync vs Async — เลือกโหมดไหน

| Use Case | โหมด | Endpoint |
|----------|------|----------|
| **Claude Code (interactive)** | Sync | `POST /v1/messages` |
| **หลาย Claude Code บนเครื่องเดียว** | Sync | แต่ละ session ใช้ key ต่างกัน |
| **CI/CD pipeline** | Async | `POST /v1/chat/completions` |
| **Batch processing (100+ jobs)** | Async | ส่งแล้ว poll result |
| **Agent framework (5-50 agents)** | Async | แต่ละ agent ส่ง `agent_id` แยก quota |
| **Cron / scheduled tasks** | Async | Queue จัดการ pacing เอง |

### Sync Mode — สำหรับ Claude Code

```bash
# ตั้งค่าใน ~/.claude/settings.json
{
  "ANTHROPIC_BASE_URL": "http://localhost:8080",
  "ANTHROPIC_AUTH_TOKEN": "your-glm-key"
}
```

- Real-time SSE streaming
- Tool loop ทำงานเหมือนยิงตรง
- Per-key rate limit: `AGENT_RATE_LIMIT=5` (5 req/min ต่อ key)
- ไม่ต้องใส่ `GLM_API_KEYS` ใน `.env` (key มาจาก client)

### Async Mode — สำหรับ Batch Agents

```bash
# ส่ง job
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "glm-5",
    "agent_id": "my-agent-1",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
# Response: {"request_id": "abc-123", "status": "queued"}

# ดึงผล
curl http://localhost:8080/v1/results/abc-123
```

- ต้องใส่ `GLM_API_KEYS` ใน `.env` (worker ต้องมี key)
- Queue + worker จัดการ pacing อัตโนมัติ
- Per-agent rate limit (`agent_id` แยก quota)
- Retry + exponential backoff อัตโนมัติ
- Provider fallback chain

### การเพิ่ม Throughput

```bash
# 1 key = 5 RPM (default)
GLM_API_KEYS=key1

# 3 keys = 15 RPM
GLM_API_KEYS=key1,key2,key3
PROVIDER_RPM_LIMITS=glm:15

# เปิดหลาย provider = throughput สูงสุด
GLM_API_KEYS=k1,k2,k3
OPENAI_API_KEYS=sk1,sk2
PROVIDER_RPM_LIMITS=glm:15,openai:120
```

### แนวทางตาม Scale

| Scale | โหมด | Config |
|-------|------|--------|
| 1 developer | Sync | key เดียว |
| 2-5 developers | Sync | แต่ละคนใช้ key ต่างกัน |
| 1 team + CI/CD | Sync + Async | Dev sync, CI async |
| Agent framework (5-50) | Async | `WORKER_CONCURRENCY=50` |
| Heavy batch (100+) | Async | หลาย keys + หลาย providers |

---

## Quick Start (สรุป)

```bash
# 1. Setup
cp .env.example .env && vim .env  # ใส่ GLM_API_KEYS

# 2. Run
docker-compose up -d --build

# 3. Use with Claude Code
# เพิ่มใน ~/.claude/settings.json:
# "ANTHROPIC_BASE_URL": "http://localhost:8080"
# "ANTHROPIC_AUTH_TOKEN": "your-glm-key"

# 4. Monitor
# Grafana:         http://localhost:3000 (admin/klxhunter)
# Rate Limiter UI: http://localhost:8081
# Gateway Health:  http://localhost:8080/health
# Admin Dashboard: http://localhost:8080/admin
```

### การ Build Dashboard UI

Dashboard เป็น React + Vite + TailwindCSS แยกใน `ui/` directory:

```bash
cd ui
bun install        # ครั้งแรกเท่านั้น
bun run dev        # dev server (port 5173, proxy to :8080)
bun run build      # build production -> api-gateway/static/
```

> **สำคัญ**: หลังจาก `bun run build` จะต้อง rebuild Go binary เพื่อ embed static files ใหม่

### การ Build Gateway (Go)

```bash
cd api-gateway

# Build binary
go build -o api-gateway .

# **ทุกครั้งหลัง build**: ลบ binary artifact
rm -f api-gateway

# Run tests with race detection
go test ./... -count=1 -race

# Combined: build UI -> build Go -> cleanup binary
cd ../ui && bun run build && cd ../api-gateway && go build -o api-gateway . && rm -f api-gateway
```

---

*Multi-Agent AI Rate-Limited System v1.1*
