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
15. [การแก้ปัญหา (Troubleshooting)](#15-การแก้ปัญหา-troubleshooting)
16. [Profile-Based Routing](#16-profile-based-routing)
17.1. [Claude OAuth Transparent Passthrough](#171-claude-oauth-transparent-passthrough-sonnet--opus-via-gateway)
17.2. [Message Body Optimization](#172-message-body-optimization)
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
│              │   OAuth: claude-oauth, gemini-oauth   │               │
│              │   Profile: X-Profile header routing   │               │
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
  │ Header: X-Profile: <profile-name> (optional)
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
  ├─ X-Profile header (optional):
  │     ├─ มี: โหลด profile จาก Redis → ใช้ target provider + token pool
  │     └─ ไม่มี: ใช้ routing ปกติ (key pool + model fallback)
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

### PasteGuard (Privacy Pipeline)

| Variable | Default | คำอธิบาย |
|----------|---------|----------|
| `PASTEGUARD_ENABLED` | `true` | เปิด/ปิด PasteGuard ทั้งระบบ |
| `PASTEGUARD_SECRETS_ENABLED` | `true` | เปิด/ปิด secret detection (API keys, tokens, certs) |
| `PASTEGUARD_SECRET_ENTITIES` | (8 types) | Secret entity types ที่ตรวจจับ (comma-separated) |
| `PASTEGUARD_PII_ENABLED` | `true` | เปิด/ปิด PII detection |
| `PASTEGUARD_PII_ENTITIES` | `EMAIL_ADDRESS,PHONE_NUMBER,CREDIT_CARD,SSN,IBAN,IP_ADDRESS,THAI_NATIONAL_ID,THAI_PHONE` | PII entity types ที่ตรวจจับ (comma-separated, default = all 8) |
| `PASTEGUARD_MAX_SCAN_CHARS` | `200000` | จำนวนตัวอักษรสูงสุดที่ scan |

> **Note**: `PASTEGUARD_PRESIDIO_URL`, `PASTEGUARD_PII_SCORE_THRESHOLD`, and `PASTEGUARD_PII_LANGUAGE` have been removed. PII detection now uses the built-in `RegexDetector` (<1ms per call) instead of the Presidio HTTP container (7-14s per call).

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
  ├─ X-Profile header (ถ้ามี):
  │     ├─ โหลด profile จาก Redis
  │     ├─ ใช้ target provider + token จาก provider pool
  │     ├─ ข้าม key pool + model fallback logic
  │     └─ Proxy ไป provider upstream โดยตรง
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
| `GET` | `/v1/profiles` | ดู profiles ทั้งหมด |
| `POST` | `/v1/profiles` | สร้าง profile (ต้องการ name + target เท่านั้น) |
| `GET` | `/v1/profiles/{name}` | ดู profile ตามชื่อ |
| `PUT` | `/v1/profiles/{name}` | แก้ไข profile |
| `DELETE` | `/v1/profiles/{name}` | ลบ profile |
| `POST` | `/v1/profiles/{name}/copy` | คัดลอก profile |
| `POST` | `/v1/profiles/{name}/export` | ส่งออก profile (API key redacted) |
| `POST` | `/v1/profiles/import` | นำเข้า profile |
| `POST` | `/v1/auth/{provider}/start` | เริ่ม OAuth flow (device code / auth code) |
| `GET` | `/v1/auth/{provider}/callback` | OAuth callback endpoint |
| `GET` | `/v1/auth/{provider}/status` | ตรวจสอบสถานะ OAuth |
| `POST` | `/v1/auth/{provider}/register` | ลงทะเบียน API key / session cookie |
| `GET` | `/v1/auth/accounts` | ดู accounts ทั้งหมด |
| `GET` | `/v1/auth/accounts/{provider}` | ดู accounts ตาม provider |
| `DELETE` | `/v1/auth/accounts/{provider}/{accountId}` | ลบ account |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/pause` | หยุด account ชั่วคราว |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/resume` | เปิดใช้ account อีกครั้ง |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/default` | ตั้งเป็น default account |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/email` | แก้ไข email ของ account |
| `GET` | `/v1/providers` | ดู provider ทั้งหมด |

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
| **Claude OAuth Billing Path** | http://localhost:3000/d/claude-oauth-billing | Billing path distribution, latency, per-profile usage, cost |

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

### Token Optimization Metrics

| Metric | Type | Labels | คำอธิบาย |
|--------|------|--------|----------|
| `api_gateway_optimizer_runs_total` | Counter | `technique` | จำนวนครั้งที่ optimizer รัน (system + message) |
| `api_gateway_optimizer_chars_saved_total` | Counter | `technique` | อักขระที่ประหยัดได้แยกตาม technique |
| `api_gateway_optimizer_duration_seconds` | Histogram | `technique` | เวลาที่ optimizer ใช้ |
| `api_gateway_optimizer_tokens_saved_total` | Counter | - | ประมาณการ tokens ที่ประหยัดได้ |
| `api_gateway_cost_savings_total` | Counter | - | ค่าใช้จ่ายที่ประหยัดจาก optimization (USD) |
| `api_gateway_budget_level` | Gauge | `model` | ระดับ budget utilization (0=green, 1=yellow, 2=red) |

**Technique labels**: `semantic_dedup`, `chunker`, `delta`, `sketch_dedup`, `summarizer`, `intent_filter`, `caveman`, `message_text` (string content), `message_block_text` (text blocks), `message_block_tool_result` (tool results)

### Claude OAuth Billing Path Metrics

| Metric | Type | Labels | คำอธิบาย |
|--------|------|--------|----------|
| `api_gateway_billing_path_requests_total` | Counter | `path`, `model`, `profile` | Request แยกตาม billing path (go_direct/sidecar/direct/billing_rejected) |
| `api_gateway_billing_path_latency_seconds` | Histogram | `path`, `model` | Latency แยกตาม billing path |
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
| Profiles | `/profiles` | Profile CRUD management (สร้างต้องการ name + target เท่านั้น) |
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

### Provider ที่รองรับ

#### API Key Auth (ใส่ใน `.env`)

| Provider ID | Env Var | Upstream |
|------------|---------|----------|
| `anthropic` | `ANTHROPIC_API_KEYS` | api.anthropic.com |
| `gemini` | `GEMINI_API_KEYS` | generativelanguage.googleapis.com |
| `openai` | `OPENAI_API_KEYS` | api.openai.com |
| `zai` | `UPSTREAM_API_KEYS` | api.z.ai/api/anthropic |
| `openrouter` | `OPENROUTER_API_KEYS` | openrouter.ai/api |
| `deepseek` | `DEEPSEEK_API_KEYS` | api.deepseek.com |
| `kimi` | `KIMI_API_KEYS` | api.moonshot.cn/v1 |
| `huggingface` | `HUGGINGFACE_API_KEYS` | api-inference.huggingface.co |
| `ollama` | `OLLAMA_API_KEYS` | localhost:11434 |
| `agy` | `AGY_API_KEYS` | antigravity.com |
| `cursor` | `CURSOR_API_KEYS` | api2.cursor.sh |
| `codebuddy` | `CODEBUDDY_API_KEYS` | api.codebuddy.io |
| `kilo` | `KILO_API_KEYS` | api.kilo.ai |

#### OAuth Auth (ผ่าน Dashboard UI)

| Provider ID | ชื่อ | วิธี Auth | หมายเหตุ |
|------------|------|-----------|----------|
| `claude-oauth` | Claude (OAuth) | OAuth PKCE + Bearer token | ใช้ Claude Code Client ID, proxy ไป api.anthropic.com |
| `gemini-oauth` | Google Gemini (OAuth) | OAuth auth code | ใช้ Code Assist proxy (cloudcode-pa.googleapis.com) |
| `copilot` | GitHub Copilot | Device code flow | ใช้ GitHub device code |

### anthropic vs claude-oauth

| Provider | Auth | Use Case |
|----------|------|----------|
| `anthropic` | API Key (`x-api-key` header) | มี Anthropic API key ตรง |
| `claude-oauth` | OAuth Bearer token (`Authorization: Bearer`) | ใช้ Claude Code subscription, ไม่ต้องมี API key |

> **หมายเหตุ**: Provider `claude-oauth` (เดิมชื่อ `claude`) ใช้ OAuth PKCE flow ผ่าน platform.claude.com พร้อม Bearer token auth ส่งไป api.anthropic.com/v1/messages (พร้อม `anthropic-beta: oauth-2025-04-20` header)

### gemini vs gemini-oauth

| Provider | Auth | Use Case |
|----------|------|----------|
| `gemini` | API Key (query param `?key=`) | มี Google AI API key ตรง |
| `gemini-oauth` | OAuth Bearer token | ใช้ Google account + Code Assist |

> **สำคัญ**: `gemini-oauth` และ `gemini` เป็น provider คนละตัวกัน -- Gemini OAuth **ไม่ fallback** ไปใช้ direct Gemini API ถ้าต้องการใช้ทั้งสองอย่าง ต้องลงทะเบียน API key ของ `gemini` แยกต่างหาก

### Token Migration (อัตโนมัติ)

เมื่อ gateway เริ่มทำงาน ระบบจะ migrate token ของ provider เก่าอัตโนมัติ:

- `claude` -> `claude-oauth` (ทุก token ที่เคยลงทะเบียนในชื่อ `claude` จะถูกย้ายไป `claude-oauth` อัตโนมัติ ไม่ต้องทำอะไร)

### Provider Fallback Order

1. **glm** (Z.ai) -- Primary
2. **openai**
3. **anthropic**
4. **gemini**
5. **openrouter**

ถ้า provider แรกล้มเหลว จะข้ามไป provider ถัดไปที่มี API key อัตโนมัติ

### การเพิ่ม Provider ผ่าน OAuth (Dashboard UI)

1. เปิด `http://localhost:8080/admin`
2. ไปที่หน้า Providers หรือ Accounts
3. เลือก provider ที่ต้องการ (เช่น `claude-oauth`, `gemini-oauth`, `copilot`)
4. กด "Start Auth" แล้วทำตามขั้นตอนบนจอ
5. เมื่อ auth สำเร็จ token จะถูกเก็บใน Dragonfly อัตโนมัติ

### Email Input (กรอก email หลัง OAuth)

บาง provider (เช่น `claude-oauth`) ไม่ return email จาก OAuth flow เมื่อ auth สำเร็จ ระบบจะแสดง step ให้กรอก email (optional) เพื่อให้ระบุ account ได้ง่ายขึ้น

Email สามารถแก้ไขได้ทีหลังจากหน้า Account List โดยกดที่ email field โดยตรง (inline editing) หรือใช้ API:

```bash
curl -X POST http://localhost:8080/v1/auth/accounts/claude-oauth/{accountId}/email \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com"}'
```

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

## 16. Profile-Based Routing

Profile คือชุดการตั้งค่าสำหรับเชื่อมต่อ provider ที่เก็บใน Redis เมื่อส่ง `X-Profile` header พร้อม request gateway จะโหลด profile และใช้การตั้งค่านั้น route request ไปยัง provider เป้าหมาย

### การทำงาน

```
Request พร้อม X-Profile: my-profile
  |
  v
Handler.Messages()
  |-- อ่าน X-Profile header
  |-- โหลด profile:{name} จาก Redis
  |
  +-- พบ profile:
  |     |-- ใช้ target provider ของ profile
  |     |-- ดึง token จาก provider token pool
  |     |     |-- มี accountIds: เลือกจาก pool เฉพาะ accounts ที่กำหนด
  |     |     |-- ไม่มี accountIds: ใช้ default token ของ provider
  |     |-- Proxy ไปยัง provider upstream โดยตรง
  |     |-- ข้าม key pool + model fallback logic
  |
  +-- ไม่พบ profile:
        |-- Log warning
        |-- ใช้ routing ปกติ (key pool + adaptive limiter)
```

### สร้าง Profile

Profile create form ง่ายขึ้น -- ต้องการเพียง **name** + **target provider**:

```
Dashboard UI: /admin/profiles -> New
  1. Name: ชื่อ profile (จำเป็น)
  2. Target: เลือก provider จาก dropdown (จำเป็น)
  3. Account Pool: เลือก accounts ที่จะใช้ (optional, ว่าง = ใช้ทั้งหมด)
```

ไม่ต้องใส่ base URL, model, หรือ API key manually -- gateway ดึงจาก provider registry และ token pool อัตโนมัติ

### ตัวอย่างการใช้งาน

```bash
# สร้าง profile
curl -X POST http://localhost:8080/v1/profiles \
  -H "Content-Type: application/json" \
  -d '{"name": "my-claude", "target": "claude-oauth"}'

# ใช้ profile ใน request
curl -X POST http://localhost:8080/v1/messages \
  -H "X-Profile: my-claude" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[...]}'

# ใช้กับ Claude Code (apiKeyHelper)
# ~/.claude/settings.json
{
  "env": { "ANTHROPIC_BASE_URL": "http://localhost:8080" },
  "apiKeyHelper": "~/.claude/get-token.sh"
}
# ~/.claude/get-token.sh
#!/bin/bash
echo "proxy-no-key"
```

### Profile Fields

| Field | จำเป็น | คำอธิบาย |
|-------|:------:|----------|
| `name` | Yes | ชื่อ profile |
| `target` | Yes | Provider ID (เช่น `claude-oauth`, `gemini-oauth`, `anthropic`) |
| `accountIds` | No | เลือก accounts เฉพาะจาก pool (ว่าง = ใช้ default) |
| `model` | No | Override model (ว่าง = ใช้ model จาก request) |
| `provider` | Auto | กำหนดอัตโนมัติจาก target |

### Profile API Endpoints

| Method | Path | คำอธิบาย |
|--------|------|----------|
| `GET` | `/v1/profiles` | ดู profiles ทั้งหมด |
| `POST` | `/v1/profiles` | สร้าง profile (ต้องการ name + target เท่านั้น) |
| `GET` | `/v1/profiles/{name}` | ดู profile ตามชื่อ |
| `PUT` | `/v1/profiles/{name}` | แก้ไข profile |
| `DELETE` | `/v1/profiles/{name}` | ลบ profile |
| `POST` | `/v1/profiles/{name}/copy` | คัดลอก profile |
| `POST` | `/v1/profiles/{name}/export` | ส่งออก profile (API key redacted) |
| `POST` | `/v1/profiles/import` | นำเข้า profile |

---


## 17.1. Claude OAuth Transparent Passthrough (Sonnet / Opus via Gateway)

ระบบที่ทำให้ Claude Code CLI ใช้ Sonnet/Opus ผ่าน gateway ได้ โดย gateway ทำหน้าที่:
1. รับ request พร้อม profile API token (`arl_*`)
2. ดึง OAuth token จาก Redis (เชื่อมกับ Anthropic account)
3. **Go billing injection** — compute billing header ใน Go, inject เป็น `system[0]` (primary path)
4. ถ้า Anthropic reject billing header → fallback ไป Node.js sidecar → fallback ไป direct proxy
5. Privacy masking (PasteGuard) ทำงานก่อน proxy
6. Message body optimization (whitespace + dedup) ทำงานก่อน privacy masking

### Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Claude Code CLI                                     │
│                  (Remote: 192.168.5.221)                                    │
│                                                                             │
│  ANTHROPIC_BASE_URL=http://192.168.5.62:9000                               │
│  ANTHROPIC_API_KEY=arl_2f3a72a7...                                         │
│                                                                             │
│  POST /v1/messages                                                         │
│  Headers: x-api-key: arl_2f3a72a7...                                       │
│  Body: {model: "claude-sonnet-4-20250514", messages: [...]}                │
└────────────────────────────┬────────────────────────────────────────────────┘
                             │
                             │  HTTP POST (arl_ token)
                             ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                       Caddy Reverse Proxy (:9000)                           │
└────────────────────────────┬────────────────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                      API Gateway (Go, :8080)                                │
│                                                                             │
│  1. ResolveProfileToken() → profile → claude-oauth                         │
│  2. Get OAuth token from Redis (sk-ant-oat01-*)                             │
│  3. Transparent mode: fix headers (Bearer auth, oauth-2025-04-20)          │
│  4. Message body optimization (whitespace + dedup)                         │
│  5. Privacy masking (PasteGuard: secrets, PII)                             │
│  6. 3-Path Routing:                                                        │
│     ├── Path 1 (primary): Go billing injection → api.anthropic.com         │
│     ├── Path 2 (fallback): Sidecar (Node.js) → api.anthropic.com           │
│     └── Path 3 (last resort): Direct proxy (no billing header)             │
└────────────────────────────┬────────────────────────────────────────────────┘
                             │
                             │  Path 1 (Go billing injection)
                             │  Inject billing header as system[0] in Go
                             │  HTTPS POST to api.anthropic.com
                             ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                    api.anthropic.com/v1/messages                             │
│                                                                             │
│  Auth: OAuth Bearer token + anthropic-beta: oauth-2025-04-20               │
│  Billing: Claude Code rate limit bucket (more generous limits)              │
│  Response: 200 {content: [{type:"text", text:"Hello!"}]}                   │
│                                                                             │
│  If 400 "reserved keyword" (billing rejected):                             │
│  → Fallback to Path 2 (Sidecar) → Path 3 (Direct)                         │
└────────────────────────────┬────────────────────────────────────────────────┘
                             │
                             │  200 OK (SSE stream or JSON)
                             ▼
                      Back to CLI client
```

### Request Routing Flow (Step by Step)

```
Client sends request
  │
  ├─ Header: x-api-key: arl_2f3a72a7...
  ├─ Body: {model: "claude-sonnet-4-20250514", messages: [...]}
  │
  ▼
Handler.Messages()
  │
  ├─ 1. Parse model from body: "claude-sonnet-4-20250514"
  │
  ├─ 2. Profile detection:
  │     x-api-key starts with "arl_" → ResolveProfileToken()
  │     → profile target = "claude-oauth"
  │     → apiKey = "sk-ant-oat01-*" from Redis
  │
  ├─ 3. Transparent override:
  │     apiKey starts with "sk-ant-oat01-" → transparent = true
  │     Fix headers: Bearer auth, oauth-2025-04-20
  │
  ├─ 4. Message body optimization:
  │     OptimizeMessages() → whitespace + dedup on text content
  │     Skips: code blocks, tool_use, privacy placeholders
  │
  ├─ 5. Privacy masking (PasteGuard):
  │     Detect secrets/PII → mask with placeholders
  │
  ├─ 6. trySidecarOrDirect() — 3-path routing:
  │
  │     Path 1: Go billing injection (primary)
  │     ├─ InjectBillingHeader() computes billing header in Go
  │     ├─ ProxyTransparent() → api.anthropic.com
  │     ├─ Record metrics: path=go_direct
  │     └─ If 400 "reserved keyword" → ErrBillingRejected → fallback
  │
  │     Path 2: Sidecar fallback (if Go billing rejected)
  │     ├─ ProxySidecar() → Node.js sidecar → api.anthropic.com
  │     ├─ Record metrics: path=sidecar
  │     └─ If sidecar fails → fallback
  │
  │     Path 3: Direct proxy (last resort)
  │     ├─ ProxyTransparent() without billing header
  │     ├─ Record metrics: path=direct
  │     └─ Uses generic OAuth bucket (more 429s expected)
  │
  └─ 7. Response 200 → unmask PII → relay to client
```

### Auth Mechanism: Two Paths

```
Path 1: API Key (standard)
  ANTHROPIC_API_KEY = sk-ant-api03-*
  → Sent as: x-api-key header
  → Anthropic validates via API key lookup
  → Works with any Anthropic-compatible endpoint

Path 2: OAuth Token (Claude Code)
  ANTHROPIC_AUTH_TOKEN = sk-ant-oat01-*
  → Sent as: Authorization: Bearer header
  → REQUIRES: anthropic-beta includes oauth-2025-04-20
  → Without oauth-2025-04-20: "OAuth authentication is currently not supported"
  → Wrong header (x-api-key instead of Bearer): "invalid x-api-key"
```

### Required Headers for OAuth on /v1/messages

| Header | Value | Required |
|--------|-------|:--------:|
| `Authorization` | `Bearer sk-ant-oat01-*` | YES |
| `anthropic-beta` | Must include `oauth-2025-04-20` | YES |
| `anthropic-version` | `2023-06-01` | YES |
| `x-app` | `cli` | YES |
| `anthropic-dangerous-direct-browser-access` | `true` | Recommended |
| `User-Agent` | `claude-cli/2.1.123 (external, cli)` | Recommended |

Full `anthropic-beta` value (from resolver route table):
```
claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,
redact-thinking-2026-02-12,context-management-2025-06-27,
prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24
```

### Billing Header Algorithm (from Claude CLI v2.1.123)

Sidecar injects a billing header into `system[0]` that routes the request to the Claude Code rate limit bucket (higher limits than generic OAuth):

```
Step 1: Extract first user message text
  firstMsg = messages.find(m => m.role === "user" && !m.isMeta)
  text = firstMsg.content (string or first text block)

Step 2: Compute build hash
  SALT    = "59cf53e54c78"
  VERSION = "2.1.123"
  chars   = [text[4], text[7], text[20]].map(c => c || "0").join("")
  hash    = SHA256(SALT + chars + VERSION).hex.slice(0, 3)

Step 3: Build header string
  "cc_version=${VERSION}.${hash}; cc_entrypoint=cli; cch=00000;"

Step 4: Inject as system[0]
  system.unshift({"type": "text", "text": "x-anthropic-billing-header: " + headerStr})

Step 5: Inject identity as system[1]
  system.splice(1, 0, {"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."})
```

### Sidecar Architecture

```
┌─────────────────────────────────────────────────┐
│            arl-gateway container                 │
│                                                  │
│  ┌────────────────────┐  ┌────────────────────┐ │
│  │  Go Gateway (:8080)│  │ Node.js Sidecar    │ │
│  │                    │  │ (:8081)            │ │
│  │  - HTTP routing    │──▶│ - Parse JSON body  │ │
│  │  - Profile resolve │  │ - Inject billing   │ │
│  │  - Rate limiting   │  │ - Inject identity  │ │
│  │  - Privacy masking │  │ - Forward headers  │ │
│  │                    │  │ - HTTPS to Anthro. │ │
│  └────────────────────┘  └────────────────────┘ │
│                                                  │
│  Entrypoint: /app/sidecar/entrypoint.sh          │
│  Starts both processes, waits for either to exit │
└─────────────────────────────────────────────────┘
```

### Files

| File | Purpose |
|------|---------|
| `api-gateway/sidecar/index.js` | Node.js proxy (~170 lines, zero dependencies) |
| `api-gateway/sidecar/entrypoint.sh` | Starts Go + Node processes |
| `api-gateway/sidecar/package.json` | No dependencies (built-in modules only) |
| `api-gateway/Dockerfile` | Multi-stage build, `apk add nodejs`, copies sidecar/ |
| `api-gateway/handler/handler.go` | Profile routing, transparent detection, header fix |
| `api-gateway/proxy/anthropic.go` | `ProxySidecar()` method, forwards to sidecar |

### Config Env Vars

| Env Var | Default | Description |
|---------|---------|-------------|
| `CLI_SIDECAR_ENABLED` | `true` | Enable/disable sidecar routing |
| `CLI_SIDECAR_URL` | `http://127.0.0.1:8081` | Sidecar URL (same container) |
| `SIDECAR_PORT` | `8081` | Node.js sidecar listen port |

### Error Codes and Causes

| HTTP | Error Message | Cause | Fix |
|------|--------------|-------|-----|
| 401 | `invalid x-api-key` | OAuth token sent as `x-api-key` header | Use `Authorization: Bearer` instead |
| 401 | `OAuth authentication is currently not supported` | Missing `oauth-2025-04-20` in `anthropic-beta` | Add the beta flag |
| 401 | `Invalid bearer token` | Token expired or revoked | Re-auth via gateway OAuth flow |
| 400 | `reserved keyword` | Billing header rejected by Anthropic | TLS fingerprint mismatch (sidecar fixes this) |
| 404 | `not_found_error: model: X` | Wrong model name | Use `claude-sonnet-4-20250514`, not `claude-sonnet-4-6-20250514` |
| 429 | `rate_limit_error` | Rate limit exceeded (generic OAuth bucket) | Must route through sidecar for billing header |
| 502 | (empty) | Gateway panic (slice bounds) | Fixed with `truncate()` helper in proxy |

### Setup: CLI on Remote Machine

```bash
# 1. Gateway OAuth flow (one-time, via browser)
# Open: http://192.168.5.62:9000/v1/auth/claude-oauth/start-url
# Click authorize → token stored in Redis

# 2. Create profile connected to claude-oauth
curl -X POST http://192.168.5.62:9000/v1/profiles \
  -H "Content-Type: application/json" \
  -d '{"name": "th15011880", "target": "claude-oauth"}'

# 3. Generate profile API token
# Dashboard UI → Profiles → th15011880 → Generate API Key
# Returns: arl_2f3a72a7eb07b4c43ffe87d8c19776eecf62c4c64e30285eee0796198bc91be1

# 4. Configure CLI on remote machine
# Option A: Environment variables
export ANTHROPIC_BASE_URL=http://192.168.5.62:9000
export ANTHROPIC_API_KEY=arl_2f3a72a7eb07b4c43ffe87d8c19776eecf62c4c64e30285eee0796198bc91be1
claude

# Option B: settings.json
# ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://192.168.5.62:9000",
    "ANTHROPIC_API_KEY": "arl_2f3a72a7..."
  }
}

# 5. Test
claude -p "Say hello" --model claude-sonnet-4-20250514
claude -p "Say hello" --model claude-opus-4-20250514
```

### Test with curl

```bash
curl -X POST http://192.168.5.62:9000/v1/messages \
  -H "x-api-key: arl_2f3a72a7eb07b4c43ffe87d8c19776eecf62c4c64e30285eee0796198bc91be1" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 64,
    "messages": [{"role": "user", "content": "Say hi in 3 words"}]
  }'
# → 200 {"content":[{"type":"text","text":"Hello there, human!"}]}
```

### Fallback Behavior (3-Path Routing)

```
trySidecarOrDirect()
  │
  ├─ Path 1: Go billing injection (InjectBillingHeader + ProxyTransparent)
  │   ├─ Success (200) → done, metrics: go_direct
  │   └─ 400 "reserved keyword" → ErrBillingRejected → try Path 2
  │
  ├─ Path 2: Sidecar (ProxySidecar → Node.js)
  │   ├─ Success (200) → done, metrics: sidecar
  │   └─ Failure → try Path 3
  │
  └─ Path 3: Direct proxy (ProxyTransparent, no billing header)
      ├─ Success (200) → done, metrics: direct
      └─ Goes to generic OAuth bucket → more 429s expected
```

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

## 17.2. Message Body Optimization

Gateway ทำ token optimization 2 ระดับ: system prompt (13-stage pipeline) และ message content (lightweight whitespace + dedup)

### Pipeline Flow

```
Request body (JSON)
  │
  ├─ System prompt optimization (OptimizeSystemPrompt)
  │   ├── Semantic dedup (DeduplicateSemantic)
  │   ├── Chunker (F1)
  │   ├── Delta encoding (F8)
  │   ├── Sketch dedup (F9)
  │   ├── Summarizer (F6, red budget only)
  │   ├── Intent filter (F13)
  │   └── Caveman compression (F16)
  │
  ├─ Message content optimization (OptimizeMessages)
  │   ├── String content: whitespace collapse + sentence dedup
  │   ├── Text blocks: whitespace collapse + sentence dedup
  │   ├── Tool result blocks: whitespace collapse + sentence dedup
  │   └── Skip: tool_use blocks, code blocks (```...```), privacy placeholders
  │
  └─ Privacy masking (PasteGuard)
      └── Detect + mask secrets/PII
```

### Content Types Handled

| Content Type | Optimized? | How |
|---|---|---|
| `messages[].content` (string) | Yes | `OptimizeWhitespace` + `DeduplicateSentences` |
| `messages[].content[].text` blocks | Yes | Same, metric: `message_block_text` |
| `messages[].content[]` tool_result `content` field | Yes | Same, metric: `message_block_tool_result` |
| `messages[].content[]` tool_use | No | Skipped (JSON input, not prose) |
| Code blocks inside text (` ```...``` `) | No | `SplitCodeBlocks` preserves code verbatim |
| Privacy placeholders (`__SECRET_1__`, `__PII_1__`) | No | Dedup skipped when placeholders present |

### Metrics

```
api_gateway_optimizer_runs_total{technique="message_text"}          N
api_gateway_optimizer_chars_saved_total{technique="message_text"}   M
api_gateway_optimizer_runs_total{technique="message_block_text"}    N
api_gateway_optimizer_runs_total{technique="message_block_tool_result"} N
```

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

## 18.1. PasteGuard PII Detection: Presidio-to-Regex Migration

### Why Presidio Was Replaced

The original PasteGuard PII pipeline used Microsoft Presidio Analyzer (NLP container) for entity detection. This caused severe latency problems:

| Issue | Detail |
|-------|--------|
| **Slow HTTP calls** | Each Presidio `/analyze` call took 7-14 seconds per text span |
| **Compounding latency** | Multiple spans per request = 30+ seconds total request time |
| **Overkill NLP** | Only 2 entity types were used (`EMAIL_ADDRESS`, `PHONE_NUMBER`) - far too light to justify a 2GB NLP container |
| **Regex is faster** | Compiled regex detection is <1ms vs 7-14s per Presidio call |
| **Container removed** | Presidio container (2GB RAM) is no longer needed for default deployment |

The replacement is `RegexDetector` - a pure Go regex engine with zero external dependencies.

### RegexDetector Supported Entities

| Entity | Pattern | Example |
|--------|---------|---------|
| `EMAIL_ADDRESS` | Standard email format | `user@example.com` |
| `PHONE_NUMBER` | International phone numbers | `+1-555-123-4567` |
| `CREDIT_CARD` | Visa / Mastercard / Amex / Discover | `4111-1111-1111-1111` |
| `SSN` | US Social Security Number | `123-45-6789` |
| `IBAN` | International Bank Account Number | `GB82WEST12345698765432` |
| `IP_ADDRESS` | IPv4 addresses | `192.168.1.1` |
| `THAI_NATIONAL_ID` | Thai citizen ID (13 digits) | `1-1001-00001-23-4` |
| `THAI_PHONE` | Thai phone format | `081-234-5678`, `+66812345678` |

Default: all 8 entities are enabled. Customize via `PASTEGUARD_PII_ENTITIES` env var.

### Presidio Legacy (Optional)

The Presidio container is still available for legacy use but is not required:

```bash
# Start with Presidio (not recommended - use only if needed)
docker-compose --profile pii up

# Default deployment (regex only, no Presidio)
docker-compose up
```

### Files

| File | Purpose |
|------|---------|
| `privacy/pii/detect.go` | `RegexDetector` - regex pattern matching for all 8 entities |
| `privacy/pii/mask.go` | PII masking with placeholder generation |
| `privacy/config.go` | Env var loading, default config |
| `privacy/pipeline.go` | Full mask/unmask pipeline (secrets + PII) |

---

*Multi-Agent AI Rate-Limited System v1.2*

---

## 19. PasteGuard Streaming Unmask — Bug Fix Log

### ปัญหา: `[[PERSON_N]]` รั่วถึง client

ผู้ใช้เห็น `[[PERSON_2]]`, `[[PERSON_13]]` ใน response แทนชื่อจริง เพราะ unmask step ไม่ทำงานบางกรณี

### Root Causes และการแก้ไข

#### Bug #1 (HIGH) — relayStreamWithTracking guard ข้าม ProcessChunk

**ไฟล์:** `proxy/anthropic.go` — `relayStreamWithTracking()`

**สาเหตุ:** Guard `strings.Contains(data, "[[")` ป้องกัน ProcessChunk เมื่อ SSE line ไม่มี `[[`  
เมื่อ placeholder split ข้าม 2 chunks (เช่น chunk1=`[[PER`, chunk2=`SON_1]]`) chunk ที่ 2 ไม่มี `[[` จึงถูกข้าม buffer ค้าง placeholder รั่ว

**แก้:** เปลี่ยน logic เป็น process `content_block_delta` ทุก chunk เมื่อ unmasker active โดยไม่ต้อง check `[[`

#### Bug #2 (HIGH) — Flush output ถูกทิ้ง

**ไฟล์:** `proxy/anthropic.go` — `relayStreamWithTracking()`, `convertOpenAIStreamResponse()`

**สาเหตุ:** `unmasker.Flush()` return ค่าที่ค้างใน buffer แต่ code เดิม log เฉย ๆ ไม่ emit เป็น SSE event  
ทำให้ placeholder ที่ค้างอยู่ตอน stream จบหายไป (data loss)

**แก้:** Emit Flush result เป็น `content_block_delta` SSE event ก่อน `content_block_stop`

#### Bug #3 (CRITICAL) — ProxySidecar ไม่ unmask เลย

**ไฟล์:** `proxy/anthropic.go` — `ProxySidecar()`

**สาเหตุ:** Sidecar ทำ raw byte relay ไม่มี SSE parsing, ไม่มี maskResult parameter  
Response ทั้งหมดส่งตรงถึง client รวม placeholder

**แก้:** เพิ่ม `maskResult` parameter + SSE line scanner + unmask logic เหมือน relayStreamWithTracking  
Non-stream path: อ่าน full body, unmask, write

#### Bug #4 (MEDIUM) — Cross-block buffer contamination

**ไฟล์:** `proxy/anthropic.go` — `relayStreamWithTracking()`

**สาเหตุ:** ProcessChunk ใช้ buffer ร่วมกันระหว่าง text/thinking block  
ถ้า placeholder split ตรง block boundary buffer จะ leak ไป block ถัดไป

**แก้:** Intercept `content_block_stop` event, flush buffer ก่อน relay stop event

#### Bug #5 (LOW) — gemini-codeassist emit empty text_delta

**ไฟล์:** `proxy/gemini-codeassist.go` — `streamResponse()`

**สาเหตุ:** ไม่มี `if text == "" { continue }` หลัง ProcessChunk  
เมื่อ unmasker buffer ทั้ง chunk จะ emit empty `text_delta` event

**แก้:** เพิ่ม empty text guard หลัง ProcessChunk

### การทำงานของ Streaming Unmask (หลังแก้)

```
Request → Mask PII/Secrets → Upstream API
                                      ↓
                              SSE Stream Response
                                      ↓
                         ┌─────────────────────────┐
                         │  content_block_delta?    │
                         │  YES → ProcessChunk()    │
                         │    (buffered, every time)│
                         │                         │
                         │  content_block_stop?     │
                         │  YES → Flush() → emit   │
                         │    delta before stop     │
                         │                         │
                         │  other + contains [[?    │
                         │  YES → ReplaceDirectJSON │
                         │                         │
                         │  Relay to client         │
                         └─────────────────────────┘
                                      ↓
                         End of stream → Flush() → emit
                                      ↓
                              Unmasked Response
```

### Known Limitation

Placeholder ที่ split ข้าม content block boundary (text → thinking) ไม่สามารถ restore ได้  
เพราะแต่ละ block เป็นคนละ logical unit แต่กรณีนี้เกิดได้น้อยมากในทางปฏิบัติ

---

## 20. GLM Mode Isolation Fix

### ปัญหา: GLM_MODE มีผลกับทุก provider

`GLM_MODE=true` (ค่า default) ทำให้ feature ของ Z.AI รันกับทุก model รวมถึง claude:
- Resolver fallback ส่ง claude model ไป Z.AI เมื่อไม่มี Anthropic token
- `filterUnsupportedContent` strip content block ของ claude request
- Vision routing ส่ง claude image request ไป Z.AI vision endpoint

`GLM_MODE=false` ซ่อน Z.AI models จาก listing (ถูกต้อง) แต่ไม่ควรมีผลอื่น

### หลักการ: Provider-Scoped, Not Flag-Scoped

GLM_MODE ควรเป็น toggle ระดับ infrastructure (key sync, model listing) เท่านั้น
ส่วน request-path logic ต้องตัดสินใจจาก **target provider** ไม่ใช่ global flag

### แก้ 4 จุด

| ไฟล์ | เดิม | ใหม่ |
|---|---|---|
| `provider/resolver.go:Resolve()` | Z.AI fallback ทุก model ที่หา token ไม่เจอ | Z.AI fallback เฉพาะ model ที่มี `zai` เป็น intended provider หรือไม่ตรง prefix rule |
| `handler/handler.go:584` | `!GLMMode && decision == nil` reject | `decision == nil` reject ทุกกรณี |
| `handler/handler.go:653` | `GLMMode` -> filterUnsupportedContent | `decision.ProviderID == "zai"` เท่านั้น |
| `handler/handler.go:974` | `GLMMode && hasImages && (decision==nil \|\| zai)` | `hasImages && decision != nil && zai` เท่านั้น |

### GLM_MODE ที่ยังใช้ (ถูกต้อง)

- `main.go:217` -- sync Z.AI keys เข้า KeyPool
- `handler.go:1762,1822` -- ซ่อน zai models จาก listing เมื่อปิด
- `resolver.go` -- Z.AI fallback สำหรับ unknown prefix และ glm- model ที่หา token ไม่เจอ

### Flow หลังแก้

```
claude-sonnet-4 request -> Resolve() -> matched "claude-" rule -> no token -> nil
-> handler: decision == nil -> reject "no provider configured" OK

glm-5.1 request -> Resolve() -> matched "glm-" rule -> zai token? -> yes: zai decision
                                                -> no token: zai decision (empty key, from pool)
-> handler: filterUnsupportedContent OK, vision auto-route OK

unknown-model request -> Resolve() -> no rule matched -> GLM fallback -> zai decision
-> handler: filterUnsupportedContent OK
```

---


### 19.1 Overview


### 19.2 Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                                                                         │
│  ┌──────────────┐             ┌──────────────────┐      ┌───────────┐  │
│  │              │  Anthropic   │                  │ OpenAI│           │  │
│  │  sends:      │  API format  │  1. Resolve model│ fmt  │  OpenAI-  │  │
│  │  claude-*    │             │  2. Override to   │      │  endpoint │  │
│  │              │             │     "default"     │      │           │  │
│  │  tools:      │             │  3. Convert tools │◀─────│  returns  │  │
│  │  [Write,Bash,│             │     Anthropic->   │ OpenAI│  tool_calls│  │
│  │   Read,...]  │             │     OpenAI func   │ fmt  │           │  │
│  │              │             │  4. Add system    │      └───────────┘  │
│  │  receives:   │  Anthropic   │     role message  │                     │
│  │  tool_use    │◀─────────────│  5. tool_choice:  │                     │
│  │  SSE stream  │  SSE format  │     auto          │                     │
│  └──────────────┘             │  6. Convert resp  │                     │
│                               │     OpenAI->       │                     │
│                               │     Anthropic      │                     │
│                               └──────────────────┘                     │
└─────────────────────────────────────────────────────────────────────────┘
```

### 19.3 Request Flow (step-by-step)

```
    │                                  │                                │
    │  POST /v1/messages               │                                │
    │  model: claude-sonnet-4-6        │                                │
    │  tools: [Write,Bash,Read,...]    │                                │
    │  stream: true                    │                                │
    │─────────────────────────────────▶│                                │
    │                                  │                                │
    │                                  │  1. Profile "โลตัส" resolved   │
    │                                  │                                │
    │                                  │  2. Model override:            │
    │                                  │     claude-sonnet-4-6          │
    │                                  │     -> "default"               │
    │                                  │                                │
    │                                  │  3. max_tokens clamped:        │
    │                                  │     128000 -> 4096             │
    │                                  │                                │
    │                                  │  4. AnthropicToOpenAI():       │
    │                                  │     - tools: Anthropic schema  │
    │                                  │       -> OpenAI function fmt   │
    │                                  │     - system: -> "system" role │
    │                                  │       (not prepend to user)    │
    │                                  │     - tool_choice: "auto"      │
    │                                  │     - messages: tool_use       │
    │                                  │       -> tool_calls,           │
    │                                  │       tool_result -> tool role │
    │                                  │                                │
    │                                  │  POST /v1/chat/completions     │
    │                                  │───────────────────────────────▶│
    │                                  │                                │
    │                                  │                                │  with function
    │                                  │                                │  calling
    │                                  │                                │
    │                                  │  SSE stream with tool_calls    │
    │                                  │◀───────────────────────────────│
    │                                  │                                │
    │                                  │  5. Convert stream:            │
    │                                  │     OpenAI delta tool_calls    │
    │                                  │     -> Anthropic tool_use SSE  │
    │                                  │                                │
    │  SSE: message_start              │                                │
    │  SSE: content_block_start (text) │                                │
    │  SSE: content_block_delta        │                                │
    │  SSE: content_block_stop         │                                │
    │  SSE: content_block_start        │                                │
    │        (tool_use: Write)         │                                │
    │  SSE: input_json_delta           │                                │
    │  SSE: content_block_stop         │                                │
    │  SSE: message_delta              │                                │
    │       stop_reason: "tool_use"    │                                │
    │  SSE: message_stop               │                                │
    │◀─────────────────────────────────│                                │
    │                                  │                                │
    │  Claude Code executes tool       │                                │
    │  (creates file, runs cmd, etc.)  │                                │
    │                                  │                                │
    │  POST /v1/messages (2nd turn)    │                                │
    │  with tool_result                │                                │
    │─────────────────────────────────▶│                                │
    │                                  │  tool_result -> tool role      │
    │                                  │───────────────────────────────▶│
    │                                  │◀───────────────────────────────│
    │  ... more turns ...              │                                │
```

### 19.4 Problems Found and Fixes





```
File: api-gateway/provider/resolver.go
var providerToolMode = map[string]string{
}
```

#### Problem 2: Streaming SSE missing `message_start` event


**Root cause**: Bug ใน `relayOpenAIStreamChunk()` - ตัวแปร `started` กลับด้าน

```
Before (WRONG):
  started := !isContinuation
  // isContinuation=false -> started=true -> skip message_start!

After (CORRECT):
  started := isContinuation
  // isContinuation=false -> started=false -> emit message_start
```

**Fix**: `api-gateway/proxy/openai.go:397` - เปลี่ยน `!isContinuation` เป็น `isContinuation`



**Root cause**: OpenAI request ไม่มี `tool_choice` field ทำให้ model ไม่ถูก encourage ให้ใช้ tools

**Fix**: เพิ่ม `tool_choice: "auto"` เป็น default สำหรับ native mode

```
File: api-gateway/proxy/anthropic.go
// When no tool_choice specified but native mode with tools:
} else if toolMode == "native" {
    if _, hasTools := result["tools"]; hasTools {
        result["tool_choice"] = "auto"
    }
}
```




**Fix**: สำหรับ native mode ใช้ system role message จริง + inject tool-usage hint

```
File: api-gateway/proxy/anthropic.go

if toolMode == "native" {
    // Use real system role (OpenAI supports this)
    toolHint := "IMPORTANT: You have access to tools..."
    sysMsg := {"role": "system", "content": systemText + toolHint}
    messages = [sysMsg] + messages
} else {
    // Z.AI: prepend to first user message (no system role support)
    first["content"] = systemText + "\n\n" + userContent
}
```

#### Problem 5: Context overflow (400 error) when conversation gets long



**Fix**: 3-layer defense ใน `ProxyOpenAI`:

```
Layer 1: Auto-compaction (truncate old messages)
  - Estimate input tokens from OpenAI request body
  - If estInput > 32000 (80% of 40000):
    * Keep: system message + last 4 messages (2 turns)
    * Don't split mid tool_call/tool_result sequence
    * Insert compaction notice for context continuity
    * Re-estimate and continue to Layer 2

Layer 2: Dynamic max_tokens reduction
  - After compaction (or if under threshold):
    available = 40000 - estInput - 1500 buffer
    if max_tokens > available -> reduce max_tokens

Layer 3: Retry on 400 (if estimation was wrong)
  - Parse actual input token count from error message
  - Retry with: max_tokens = 40000 - actualInput - 500
```

```
File: api-gateway/proxy/openai.go

// Layer 1: auto-compact when context nearly full
if estInput > 32000 {
    compactOpenAIMessages(openaiReq, estInput, 40000)
    // Keep: system + last 4 messages + compaction notice
    // Never split tool_call/tool_result pairs
    estInput = re-estimate from compacted body
}

// Layer 2: reduce max_tokens to fit
available := 40000 - estInput - 1500
if max_tokens > available {
    openaiReq["max_tokens"] = available
}

// Layer 3: catch 400, parse actual tokens, retry
if resp.StatusCode == 400 && strings.Contains(body, "max_tokens") {
    actualInput = parse from error message
    openaiReq["max_tokens"] = 40000 - actualInput - 500
    retry request
}
```


#### Problem 6: Simulate mode was dead code



**Fix**: Removed entirely:
- Deleted `proxy/tool_sim.go`, `proxy/tool_sim_test.go`, `proxy/tool_sim_integration_test.go`
- Removed simulate streaming path from `openai.go` (buffer, emitToolSimResponse)
- Removed simulate branch from `anthropic.go` (prompt injection)
- Only `native` and `""` (no tools) modes remain

### 19.5 Configuration

```
    Format:  FormatOpenAI,
    AuthMode: "bearer",
    URL:     "/v1/chat/completions",
    ModelOverride: "default",     // overrides claude-sonnet-4-6 -> "default"
    MaxTokens: 4096,              // clamped from 128000
}


# Model routing

# Profile: "โลตัส"
accountIds: ["mLoH9s"]
```

### 19.6 Supported Tools (forwarded from Claude Code)

| Tool | Anthropic Format | OpenAI Format | Status |
|---|---|---|---|
| Bash | input_schema | function.parameters | Works |
| Write | input_schema | function.parameters | Works |
| Read | input_schema | function.parameters | Works |
| Edit | input_schema | function.parameters | Forwarded |
| MultiEdit | input_schema | function.parameters | Forwarded |
| Glob | input_schema | function.parameters | Forwarded |
| Grep | input_schema | function.parameters | Forwarded |
| LS | input_schema | function.parameters | Forwarded |
| TodoRead | input_schema | function.parameters | Forwarded |
| TodoWrite | input_schema | function.parameters | Forwarded |
| WebFetch | input_schema | function.parameters | Forwarded |
| WebSearch | input_schema | function.parameters | Forwarded |


### 19.7 Key Files

| File | Role |
|---|---|
| `api-gateway/proxy/openai.go` | Streaming SSE conversion, auto-continuation, auto-compact, max_tokens dynamic adjustment |
| `api-gateway/proxy/anthropic.go` | AnthropicToOpenAI conversion, system role, tool_choice |
| `api-gateway/handler/handler.go` | Request routing, profile resolution, optimizer, privacy masking |

