# Claude Code Transparent Proxy

> Gateway เป็น transparent proxy สำหรับ Anthropic API
> ประสบการณ์ผู้ใช้ผ่าน gateway ต้องเหมือนยิงตรงทุกประการ

---

## ภาพรวม

```
แบบที่ 1: ยิงตรง
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Claude Code ──▶ api.z.ai/api/anthropic
                (ANTHROPIC_BASE_URL)

แบบที่ 2: ยิงผ่าน Gateway (ต้องเหมือนแบบที่ 1 ทุกอย่าง)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Claude Code ──▶ Gateway :8080 ──▶ api.z.ai/api/anthropic
                (ANTHROPIC_BASE_URL)

Gateway ทำอะไร:
  1. รับ request
  2. เช็ค X-Profile header → profile routing (if present)
  3. เช็ค quota enforcement (>= 95% → 429, >= 80% → warning)
  4. เช็ค rate limit
  5. ส่งต่อไป upstream (ทุก byte เหมือนเดิม)
  6. รับ response กลับ
  7. Record usage metrics + usage analytics (auto via callback)
  8. Broadcast WS event (request-completed / request-error)
  9. ส่งตรงไปหา client (ทุก byte เหมือนเดิม)
```

---

## Tool Loop — หัวใจของ Claude Code

Claude Code ทำงานแบบ **tool loop** — ส่ง request, ได้ tool_use กลับ, execute ที่ local, ส่ง tool_result ไปใหม่, วนไปจนกว่าจะจบ

```
Turn 1:
━━━━━━
Client ──POST /v1/messages──▶ Gateway ──▶ Upstream
  body: {
    "messages": [{"role": "user", "content": "อ่านไฟล์ main.go"}],
    "tools": [
      {"name": "Read", "input_schema": {...}},
      {"name": "Edit", "input_schema": {...}},
      {"name": "Bash", "input_schema": {...}}
    ],
    "stream": true
  }

Client ◀──response── Gateway ◀── Upstream
  body: {
    "content": [
      {"type": "tool_use", "id": "toolu_abc", "name": "Read",
       "input": {"file_path": "/path/main.go"}}
    ],
    "stop_reason": "tool_use"
  }

Client execute Read locally (อ่านไฟล์จริงๆ)

Turn 2:
━━━━━━
Client ──POST /v1/messages──▶ Gateway ──▶ Upstream
  body: {
    "messages": [
      {"role": "user", "content": "อ่านไฟล์ main.go"},
      {"role": "assistant", "content": [
        {"type": "tool_use", "id": "toolu_abc", "name": "Read", "input": {...}}
      ]},
      {"role": "user", "content": [
        {"type": "tool_result", "tool_use_id": "toolu_abc",
         "content": "package main\n\nimport ..."}
      ]}
    ],
    "tools": [...],
    "stream": true
  }

Client ◀──response── Gateway ◀── Upstream
  body: {
    "content": [
      {"type": "text", "text": "ไฟล์ main.go เป็น Go HTTP server..."}
    ],
    "stop_reason": "end_turn"
  }

>>> จบ loop แสดงผลให้ผู้ใช้
```

---

## สิ่งที่ Gateway ต้องรักษาไว้

### Request fields (ต้องส่งต่อครบ)

| Field | ตัวอย่าง | หมายเหตุ |
|-------|---------|----------|
| `messages` | conversation history | รวม tool_use, tool_result blocks |
| `tools` | Read, Edit, Bash, Write, Grep, Glob, MCP tools | ถ้าหาย = AI ตอบเป็น text ธรรมดา ใช้ tools ไม่ได้ |
| `tool_choice` | auto, any, none | ควบคุมว่าจะใช้ tool ไหม |
| `model` | glm-5, claude-sonnet-4-6 | ต้องส่งตรงไป upstream |
| `stream` | true/false | Claude Code ใช้ streaming เป็นหลัก |
| `max_tokens` | 4096, 8192 | ต้องส่งตรงไป |
| `system` | system prompt | รวมคำสั่งต่างๆ ของ Claude Code |
| `temperature` | 0.0 - 1.0 | |
| `stop_sequences` | | |
| `metadata` | user_id | |

### Response fields (ต้องส่งตรงไป client)

| Field | หมายเหตุ |
|-------|----------|
| `content[].type = "text"` | ข้อความตอบกลับ |
| `content[].type = "tool_use"` | คำขอเรียกใช้ tool — ถ้าหาย = tools ไม่ทำงาน |
| `stop_reason` | "end_turn", "tool_use", "max_tokens" |
| `usage` | token usage stats |
| `model` | model ที่ใช้จริง |

### Headers (ต้องส่งตรงไป)

| Header | ทิศทาง | หมายเหตุ |
|--------|--------|----------|
| `x-api-key` | client → upstream | ส่งต่อไป upstream |
| `anthropic-version` | client → upstream | |
| `content-type` | ทั้งสองทิศทาง | |
| `content-type: text/event-stream` | upstream → client | สำหรับ SSE streaming |

---

## สถาปัตยกรรม Gateway (Transparent Proxy)

### Rate Limit Middleware (ก่อนถึง proxy)

ทุก request ผ่าน `/v1/messages` จะผ่าน rate limit middleware ก่อน:

```
Request → Logging → Metrics → Rate Limit → Proxy
                                    │
                              ┌─────▼──────┐
                              │ Global RL  │ (key = "global")
                              │ Per-key RL │ (key = "agent:<api-key>")
                              └─────┬──────┘
                                    │
                           ┌────────▼────────┐
                           │ Rate Limiter     │
                           │ POST /api/       │
                           │ ratelimit/check  │
                           └────────┬─────────┘
                                    │
                        ┌───────────▼──────────┐
                        │ Fail-open:           │
                        │ ถ้า rate limiter     │
                        │ ไม่ตอบ → ผ่านเลย    │
                        └───────────┬──────────┘
                                    │
                        ┌───────────▼──────────┐
                        │ Handler:             │
                        │ 1. Read+validate body│
                        │ 2. Parse JSON        │
                        │ 3. X-Profile header? │
                        │    ├─ YES: Load from │
                        │    │  Redis, override │
                        │    │  model/apiKey/   │
                        │    │  baseUrl          │
                        │    └─ NO: Normal flow │
                        │ 4. Check quota       │
                        │    (>= 95% → 429)    │
                        │    (>= 80% → WS warn)│
                        │ 5. Resolve key pool  │
                        │ 6. Acquire model slot│
                        │    (may fallback)    │
                        │ 7. Proxy upstream    │
                        │ 8. Record usage      │
                        │    (metrics + Redis  │
                        │     via callback)    │
                        │ 9. Broadcast WS event│
                        └───────────┬──────────┘
                                    │
                        ┌───────────▼──────────┐
                        │ Adaptive Limiter     │
                        │ glm-5.1: limit 1     │
                        │ glm-5-turbo: limit 1 │
                        │ glm-5: limit 2       │
                        │ glm-4.7: limit 2     │
                        │ glm-4.6: limit 3     │
                        │ glm-4.5: limit 10    │
                        │ Total: 19 slots      │
                        │ Global cap: 9        │
                        │ Probe: 5x initial    │
                        │                      │
                        │ เต็ม? → รอ 2s        │
                        │ → fallback ตาม       │
                        │ priority order       │
                        │ (5.x ก่อนเสมอ)       │
                        └──────────────────────┘
```

**Fail-open behavior**: ถ้า rate limiter service ไม่ตอบหรือ error → request ผ่านโดยไม่ถูก block (เพื่อไม่ให้ rate limiter เป็น SPOF)

**Identity extraction**:
- `/v1/messages`: ใช้ `x-api-key` header หรือ `Authorization: Bearer <token>`
- อื่นๆ: ใช้ `?agent_id=` query param

**Additional gateway routes** (not proxied, served directly):
- `/v1/messages` supports `X-Profile` header for profile-based routing (overrides model, apiKey, baseUrl)
- `/v1/messages` enforces quota: 429 at >= 95%, WebSocket `quota-warning` at >= 80%
- `/v1/messages` auto-records usage to Redis buckets via metrics callback on every request
- `/v1/messages` broadcasts `request-completed`/`request-error` WebSocket events per request
- `/v1/chat/completions` broadcasts `request-queued` WebSocket event on enqueue
- `/v1/profiles/*` - Profile CRUD management
- `/v1/usage/*` - Usage analytics (summary, hourly, daily, monthly, models, sessions)
- `/quota/*` - Per-provider/account quota tracking
- `/v1/overview` - Dashboard summary
- `/v1/health/detailed` - 6 automated health checks with auto-fix hints
- `/v1/config`, `/v1/config/raw`, `/v1/config` (PUT) - Server config management
- `/v1/thinking` (GET/PUT) - Thinking budget configuration
- `/v1/global-env` (GET/PUT) - Global environment variable overrides
- `/ws` - WebSocket endpoint for 6 real-time event types (config-changed, request-completed, request-error, anomaly-detected, request-queued, quota-warning)
- `/v1/auth/*` - Provider OAuth/API key authentication flows
- `/admin/*` - Dashboard SPA (embedded Vite build)

ดูรายละเอียดเพิ่มเติมที่ [docs/architecture.md](architecture.md#3-rate-limit-middleware)

### Code Design

```go
// handler.go — Messages endpoint
func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
    // 1. อ่าน body ทั้งก้อน + validate (size limit 10MB, JSON parse)
    body, _ := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
    // ... validate size + parse JSON ...

    // 2. Profile-based routing (X-Profile header)
    profileName := r.Header.Get("X-Profile")
    if profileName != "" {
        profile := loadProfileFromRedis(profileName)
        if profile != nil {
            // Override model, apiKey, baseUrl from profile
            // Skip key pool + model fallback
        }
    }

    // 3. Quota enforcement (before model slot)
    quota := h.quotaHandler.CheckQuota(provider, accountID, model)
    if quota >= 95% { return 429 }
    if quota >= 80% { h.wsBroadcast("quota-warning", ...) }

    // 4. Resolve API key (หลัง validation — ไม่เสีย RPM บน bad request)
    apiKey := h.keyPool.Acquire() // หรือ extract จาก header (passthrough mode)

    // 5. Acquire model slot (อาจ fallback ไป model อื่น)
    selectedModel, _ := h.modelLimiter.Acquire(requestedModel)

    // 6. ส่งตรงไป upstream (body rewrite เฉพาะ model field)
    h.proxy.ProxyTransparent(w, r, apiKey, isStream)

    // 7. Usage recording + WS broadcast done via metrics callback + wsBroadcast
}

// anthropic.go — Transparent proxy
func (p *AnthropicProxy) ProxyTransparent(...) error {
    // ส่ง raw body ไป upstream เลย ไม่แตะ
    // รับ response มา ส่งตรงไป client เลย ไม่แตะ
    // Retry backoff ใช้ select + ctx.Done() (ไม่ block เมื่อ client disconnect)
}
```

### สิ่งที่ **ไม่ทำ** (เพื่อความ transparent)

- ❌ ไม่ decode request body เป็น struct
- ❌ ไม่ re-encode request body
- ❌ ไม่แก้ response body
- ❌ ไม่ลบ/เพิ่ม fields
- ❌ ไม่แปลง format
- ❌ ไม่แตะ content blocks
- ❌ ไม่แยก text/tool_use/thinking

---

## Claude Code Features Compatibility

### ทำงานได้ปกติ (ผ่าน gateway)

| Feature | ทำงานไหม | เหตุผล |
|---------|:--------:|--------|
| **Read** | ✅ | tool_use block ส่งผ่าน gateway ไม่ถูกแตะ |
| **Edit** | ✅ | เหมือน Read |
| **Bash** | ✅ | เหมือน Read |
| **Write** | ✅ | เหมือน Read |
| **Grep / Glob** | ✅ | เหมือน Read |
| **Streaming (SSE)** | ✅ | Gateway relay chunk by chunk แบบ real-time |
| **Skills (slash commands)** | ✅ | ถูก expand เป็น prompt ที่ client ก่อนส่ง API |
| **Memory** | ✅ | เก็บในไฟล์ local `~/.claude/` ไม่เกี่ยวกับ API |
| **Artifacts** | ✅ | Client-side rendering จาก response content |
| **MCP Servers** | ✅ | Tools จาก MCP register ที่ client เหมือน built-in tools |
| **Multi-turn conversation** | ✅ | History ส่งไปครบในแต่ละ request |
| **Extended thinking** | ✅ | เป็น content block อีกชนิด — gateway ไม่แตะ |
| **NotebookEdit** | ✅ | เหมือน tools อื่นๆ |
| **TodoRead / TodoWrite** | ✅ | เหมือน tools อื่นๆ |
| **Image / Vision** | ✅ | Auto-routed to native Zhipu endpoint with format conversion |

### ทำงานที่ client (ไม่เกี่ยวกับ gateway)

```
Skills     → expand prompt ที่ client ก่อนส่ง
Memory     → อ่าน/เขียนไฟล์ ~/.claude/ ที่ local
Artifacts  → render จาก response ที่ client
MCP        → register tools ที่ client
TodoWrite  → จัดการ todo list ที่ client
```

### ทำงานที่ gateway (transparent)

```
Profile routing    → X-Profile header → load profile from Redis → override model/apiKey/baseUrl
Quota enforcement  → CheckQuota() before model slot → 429 at >= 95%, WS warning at >= 80%
Rate limit check   → ดึง API key → เรียก rate limiter → ผ่าน/ไม่ผ่าน
Vision routing     -> detect image content -> convert format -> route to native Zhipu endpoint
Content filter     -> strip server_tool_use -> convert image format to GLM-compatible
Usage recording    -> metrics.RecordTokens() auto-calls usageHandler → Redis buckets
WS broadcasting    -> request-completed, request-error, anomaly-detected, quota-warning
Proxy request      → ส่ง raw body ไป upstream
Proxy response     → ส่ง raw body กลับ client
Proxy SSE stream   → relay chunk by chunk
```

### ทำงานที่ gateway (management APIs)

```
Profile CRUD       → /v1/profiles/* → Dragonfly storage
Profile routing    → X-Profile header → profile lookup → override request config
Usage analytics    → /v1/usage/* → Redis time-bucket aggregation (auto-recorded via metrics callback)
Quota tracking     → /quota/* → per-provider quota with Redis cache
Quota enforcement  → CheckQuota() in Messages handler → 429 at >= 95%, WS warning at >= 80%
Health checks      → /v1/health/detailed → 6 automated checks
Config management  → /v1/config, /v1/thinking, /v1/global-env → Redis overrides
WebSocket          → /ws → 6 real-time event types to dashboard (request-completed, request-error,
                      anomaly-detected, request-queued, quota-warning, config-changed)
Provider auth      → /v1/auth/* → OAuth device code + auth code + API key flows
```

---

## การตั้งค่า

### Image Handling

Gateway จัดการ image requests อัตโนมัติ:

```
Claude Code ส่ง request พร้อม image block
  |
  v
Gateway detects image content
  |-- analyzeImagePayload(): count images + total base64 bytes
  |-- selectVisionModel():
  |     score = totalBase64KB + (imageCount * 300)
  |     glm-4.6v (10 slots) or glm-4.6v-flashx (3 slots)
  |-- filterUnsupportedContent(): strip server_tool_use, convert image format
  |-- ProxyNativeVision(): convert Anthropic -> Zhipu format
  |-- Send to native Zhipu vision endpoint
  |-- stream=true? -> convertZhipuStreamResponse() (real-time SSE)
  |-- stream=false? -> zhipuToAnthropic() (JSON response)
  |
  v
Claude Code ได้ response (เหมือนเดิม, Anthropic format)
```

Client ไม่ต้องทำอะไรเพิ่ม -- gateway จัดการทุกอย่าง transparently

Supported image formats:
- Anthropic base64: `{"type":"image","source":{"type":"base64","media_type":"...","data":"..."}}`
- Anthropic URL: `{"type":"image","source":{"type":"url","url":"https://..."}}`

### Claude Code → Gateway

```json
// ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_AUTH_TOKEN": "your-api-key"
  }
}
```

### Claude Code → Gateway (Docker container)

```json
// ~/.claude/settings.json (ใน container)
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://192.168.5.62:8080",
    "ANTHROPIC_AUTH_TOKEN": "your-api-key"
  }
}
```

### Gateway → Upstream

```yaml
# docker-compose.yml
arl-gateway:
  environment:
    - UPSTREAM_URL=https://api.z.ai/api/anthropic
    - STREAM_TIMEOUT=300s   # timeout สำหรับ streaming (ตั้งสูงไว้)
```

---

## Troubleshooting

### อาการ: Claude Code ไม่ใช้ tools (ตอบเป็น text ธรรมดา)

**สาเหตุ:** Gateway decode request เป็น struct ที่ไม่มี field `tools` ทำให้ tool definitions หาย

**แก้:** ใช้ transparent proxy — ไม่ decode/re-encode body

### อาการ: Response มี artifact แปลกๆ (`<tool_call督查>`, `<tool_result>`)

**สาเหตุ:** Gateway decode request เป็น Go struct ที่ไม่มี field `tools` → tool definitions หาย → upstream model ไม่รู้จัก structured tool format → เลย output tool calls เป็น text/XML tags แทน

**Root cause chain:**

```
1. Claude Code ส่ง request พร้อม tools definitions:
   { "tools": [{"name":"Read",...}, {"name":"Edit",...}], "messages":[...], "stream":true }

2. Gateway decode เป็น Go struct (เดิม):
   type MessagesRequest struct {
       Model     string    `json:"model"`
       MaxTokens int       `json:"max_tokens"`
       Messages  []Message `json:"messages"`
       Stream    bool      `json:"stream"`
       // ❌ ไม่มี field tools → หาย!
   }

3. Gateway re-encode ส่งไป upstream:
   { "model":"glm-5", "messages":[...], "stream":true }
   // ❌ "tools" หายไปแล้ว

4. GLM model ได้ request ไม่มี tool definitions
   → ไม่รู้จัก structured tool_use format
   → เลย "เรียก tool" เป็น text ธรรมดา:

   content: [{"type":"text", "text":"<tool_call督查>\n{\"name\":\"Read\",...}\n</tool_call督查>"}]

5. → เห็น <tool_call督查> ใน response เป็นข้อความธรรมดา
```

**แก้:** ใช้ transparent proxy — ไม่ decode/re-encode body:

```go
// แบบเก่า (มีปัญหา):
body → json.Decode(&struct{ไม่มีtools}) → json.Encode() → upstream  // tools หาย

// แบบใหม่ (transparent):
body → io.ReadAll() → ส่ง raw bytes ตรงไป upstream                // tools ครบ
```

### อาการ: Streaming ไม่ทำงาน (รอนานแล้วค่อยแสดงทีเดียว)

**สาเหตุ:** Gateway รอ response ครบแล้วค่อยส่ง แทนที่จะ relay chunk by chunk

**แก้:** ตรวจสอบว่า `Flusher` interface ทำงาน — gateway ต้อง flush ทุก chunk

### อาการ: Timeout ระหว่าง tool loop ยาวๆ

**สาเหตุ:** `STREAM_TIMEOUT` ตั้งไว้ต่ำเกินไป

**แก้:** เพิ่ม `STREAM_TIMEOUT=600s` ใน docker-compose

### อาการ: 429 กลับมาเป็น 502

**สาเหตุ:** Gateway แปลง upstream error เป็น 502 (Bad Gateway) แทนที่จะส่ง status code เดิม

**แก้:** Transparent proxy ส่ง status code + headers + body ตรงไป client

### อาการ: "Unsupported content type: server_tool_use" เมื่อส่งรูป

**สาเหตุ:** GLM ผ่าน z.ai Anthropic endpoint ไม่รองรับ image content และ server_tool_use block type

**แก้:** Gateway ตอนนี้ filter server_tool_use blocks ออกอัตโนมัติ และ route image requests ไป native Zhipu endpoint แทน

### อาการ: Vision model ตอบไม่แม่น

**สาเหตุ:** System prompt ไม่มี vision-specific instructions

**แก้:** Gateway มี vision prompt injection อัตโนมัติ (ENABLE_PROMPT_INJECTION=true)

### อาการ: glm-4.6v-flash overload ตอนส่ง base64

**สาเหตุ:** flash model มี payload limit ต่ำกว่า

**แก้:** ใช้ URL image แทน base64, หรือใช้ glm-4.6v แทน

---

## Test Scripts

```bash
# Conversation test (8 turns: ไทย + implement HTML + cleanup)
bash scripts/conversation-test.sh

# Stress test (10 concurrent requests)
bash scripts/stress-test.sh

# ทดสอบ tools โดยตรง
curl -X POST http://localhost:8080/v1/messages \
  -H "x-api-key: $API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "glm-5",
    "max_tokens": 512,
    "tools": [{"name": "get_weather", "description": "Get weather", "input_schema": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}}],
    "messages": [{"role": "user", "content": "อากาศกรุงเทพเป็นยังไง"}]
  }'
# ควรได้ response ที่มี content: [{"type": "tool_use", "name": "get_weather", ...}]
```

---

*Transparent Proxy v2.7 -- profile-based routing, quota enforcement, usage recording integration, 6 WebSocket event types, Z.AI pricing update (19 models), UPSTREAM_API_KEYS replacing GLM_API_KEYS/GLM_ENDPOINT*
