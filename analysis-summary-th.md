# บันทึกการวิเคราะห์และแก้ไข 429 Rate Limit

**วันที่**: 2026-04-30
**เวอร์ชัน**: claude-code/2.1.123 / claude-cli/2.1.123 (external, cli)
**ปัญหา**: Gateway ส่ง sonnet/opus ผ่าน claude-oauth แล้วได้ 429 แต่ CLI ตัวจริงยิงได้ 200

---

## สาเหตุหลัก (Root Cause)

**Bug ใน handler.go:289-306** - ตรวจจับ transparent mode ไม่ครอบคลุม

เมื่อ `resolver.Resolve()` ตอบเป็น `anthropic` (เพราะ claude-oauth token หมดอายุ + มี API key สำรอง)
transparent mode ไม่ activate เลย ทำให้:

1. Request ไปใช้ API key auth แทน Bearer token ของ client
2. Body ถูก modify (optimizer, masking, prompt injection)
3. Headers ถูกเขียนทับจาก ExtraHeaders แทน forward ตรงจาก client
4. ต่าง rate limit bucket กับ CLI ตัวจริง = โดน 429

---

## Flow Diagram: Remote CLI Sonnet ผ่าน Gateway

```
Remote Mac (192.168.5.221)              Gateway (192.168.5.62)                Anthropic
========================                =======================                =========

claude CLI (2.1.123)
  |
  | apiKeyHelper -> OAuth Bearer token
  | model: claude-sonnet-4-6
  |
  | POST /v1/messages?beta=true
  | Authorization: Bearer sk-ant-oaitkn-...
  | anthropic-beta: interleaved-thinking...
  | x-app: cli
  | User-Agent: claude-cli/2.1.123
  | X-Stainless-*: ...
  |
  +-- ANTHROPIC_BASE_URL=http://192.168.5.62:9000 -->
  |
                        Caddy (:9000)
                          | reverse_proxy
                          v
                     arl-gateway (:8080)
                          |
                          | handler.go:
                          |   model = "claude-sonnet-4-6"
                          |   Bearer token != arl_*
                          |   => transparent = true
                          |   => ResolveTransparent()
                          |   => upstream: /v1/messages?beta=true
                          |
                          | ProxyTransparent:
                          |   copy ALL client headers
                          |   body = rawBody (no modify)
                          |   skip optimizer/masking
                          |
                          +-- Bearer token forwarded as-is -->
                          |
                                              api.anthropic.com
                                                |
                                                | Rate limit check
                                                | bucket: OAuth account
                                                |
                                                v
                                              200 OK
                                                |
                          <---- SSE stream ----
                          |
  <---- "Hello" -----------
  |
  v
Output: Hello
```

**Flow สั้น:**
```
Remote CLI --(Bearer+headers)--> Caddy:9000 --> Gateway:8080
  -> transparent=true
  -> forward ALL headers + body เดิม
  -> api.anthropic.com/v1/messages?beta=true
  -> 200 OK
```

---

## ข้อมูลจากการจับ traffic (mitmproxy)

### ขั้นตอนการทำงานของ CLI ตัวจริง (28 HTTP flows)

| Phase    | Endpoint                                                                         | สถานะ           | หมายเหตุ             |
|----------|----------------------------------------------------------------------------------|-----------------|----------------------|
| Health   | `/api/hello`, `/v1/oauth/hello`                                                  | 200             | ตรวจสอบการเชื่อมต่อ  |
| OAuth    | `/v1/oauth/token` (PKCE)                                                         | 200             | แลก token            |
| Profile  | `/api/oauth/profile`, `/api/oauth/claude_cli/roles`                              | 200             | ดึงข้อมูลบัญชี       |
| Config   | `/v1/mcp_servers`, `/api/claude_code/settings`, `/api/claude_code/policy_limits` | 200/404         | ตั้งค่า              |
| MCP Init | `/v1/mcp/{server_id}` (11 servers)                                               | 401/502/429/200 | Burst initialization |
| **LLM**  | `/v1/messages?beta=true`                                                         | **200**         | **Request หลัก**     |

### Request ที่สำคัญ: `/v1/messages?beta=true`

CLI ตัวจริงส่ง headers:
```
Authorization: Bearer sk-ant-oaitkn-...
Content-Type: application/json
User-Agent: claude-cli/2.1.123 (external, cli)
anthropic-beta: interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,...
anthropic-dangerous-direct-browser-access: true
x-app: cli
X-Stainless-Lang: js
X-Stainless-Package-Version: 0.81.0
X-Stainless-OS: MacOS
X-Stainless-Arch: arm64
X-Stainless-Runtime: node
X-Stainless-Runtime-Version: v24.3.0
```

**ผลลัพธ์**: 200 OK, utilization ต่ำมาก (5h: 0.01, 7d: 0.09)

### 429 ที่เกิดขึ้นใน flows

429 ที่จับได้ **ไม่ใช่จาก `/v1/messages`** แต่จาก MCP proxy burst:
- Claude Code ยิง MCP initialize 11 requests พร้อมกันใน ~150ms
- Request ที่ ~10th ถูก rate limit
- สาเหตุ: concurrent connection limit ของ mcp-proxy.anthropic.com

---

## การวิเคราะห์สาเหตุ

### เส้นทางปกติ (ที่ CLI ตัวจริงใช้)

```
Client -> api.anthropic.com/v1/messages?beta=true
Headers: Bearer token + anthropic-beta + X-Stainless-*
= 200 OK (OAuth rate limit bucket)
```

### เส้นทางที่ Gateway ใช้ (ก่อนแก้)

```
Client -> Gateway -> resolver.Resolve("claude-sonnet-4-6")
  -> ตอบ "anthropic" (เพราะ claude-oauth token หมดอายุ)
  -> transparent = false (bug: เช็คแค่ claude-oauth และ nil)
  -> ใช้ API key auth, modify body, เขียนทับ headers
  -> ต่าง rate limit bucket -> 429
```

### เส้นทางที่ถูกต้อง (หลังแก้)

```
Client -> Gateway -> ตรวจ claude-* model + Bearer token (ไม่ใช่ arl_)
  -> transparent = true
  -> force ResolveTransparent() = claude-oauth decision
  -> forward ALL client headers + body เดิม
  -> upstream: api.anthropic.com/v1/messages?beta=true
  = 200 OK (OAuth rate limit bucket เดียวกับ CLI)
```

---

## การแก้ไข (4 จุด)

### 1. handler.go:289-296 - ตรวจจับ transparent mode

**ก่อน**: เช็คแค่เมื่อ resolver ตอบ `claude-oauth` หรือ `nil`
```go
if d != nil && d.ProviderID == "claude-oauth" {
    // check bearer token
} else if d == nil && strings.HasPrefix(requestedModel, "claude-") {
    // check bearer token
}
```

**หลัง**: เช็คทุกกรณีที่เป็น claude-* model + มี OAuth Bearer token
```go
if h.resolver != nil && strings.HasPrefix(requestedModel, "claude-") {
    if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
        if tok := strings.TrimPrefix(ah, "Bearer "); tok != "" && !strings.HasPrefix(tok, "arl_") {
            transparent = true
        }
    }
}
```

### 2. handler.go:356-360 - Force claude-oauth decision

**ก่อน**: สร้าง transparent decision เฉพาะเมื่อ `decision == nil`
```go
if transparent && decision == nil && strings.HasPrefix(requestedModel, "claude-") {
    decision = h.resolver.ResolveTransparent(requestedModel)
}
```

**หลัง**: สร้าง transparent decision ทุกครั้งที่ transparent = true
```go
if transparent && strings.HasPrefix(requestedModel, "claude-") {
    decision = h.resolver.ResolveTransparent(requestedModel)
}
```

### 3. registry.go:131 - Token refresh URL

**ก่อน**: `platform.claude.com/v1/oauth/token` (return 429 เสมอ)
**หลัง**: `api.anthropic.com/v1/oauth/token` (ทำงานปกติ)

### 4. token-refresh.go:178 - Empty token guard

**ก่อน**: `doRefresh()` เขียน access_token ว่างเปล่าลง Redis ทับ token เดิม
**หลัง**: เพิ่ม guard ป้องกัน
```go
if tokResp.AccessToken == "" {
    return fmt.Errorf("refresh returned empty access_token")
}
```

---

## x-anthropic-billing-header (Reserved Keyword)

### การค้นพบ

จาก mitmproxy flows พบว่า CLI ตัวจริงส่ง billing header เป็น system prompt ตัวแรก:
```json
{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.123.a9a; cc_entry=cli; cch=c7d70;"}
```

### ผลการทดสอบ

| Test                               | Result   | หมายเหตุ                                                                                       |
|------------------------------------|----------|------------------------------------------------------------------------------------------------|
| curl ไม่มี billing header (sonnet) | 429      | Rate limit 7-day exhausted                                                                     |
| curl มี billing header (sonnet)    | **400**  | `"x-anthropic-billing-header is a reserved keyword and may not be used in the system prompt."` |

### สรุป

- `x-anthropic-billing-header` เป็น reserved keyword ของ Anthropic
- CLI ตัวจริง (official client) มีสิทธิ์ใช้ - API call ภายนอกถูกบล็อก (400)
- ไม่สามารถเลียนแบบจาก curl/gateway ได้
- วิธีเดียวคือเรียกผ่าน CLI ตัวจริง (ซึ่ง gateway รองรับแล้วใน transparent mode)

---

## Rate Limit: Consumer OAuth (Team Plan)

### ระบบ Rate Limit

Consumer OAuth ใช้ rolling window:
- **5-hour window**: utilization ต่ำ (0.0 หลัง reset)
- **7-day window**: utilization สูง (14.0 จากการ test หนัก)

### Per-Model Rate Limits

แต่ละ model มี rate limit แยก:
- `claude-sonnet-4-6`: จำกัดสูง, หมดเร็วจากการ test
- `claude-haiku-4-5`: ยังใช้ได้ (test #4 ได้ 200)
- `claude-opus-4-7`: จำกัดสูงกว่า sonnet

### Reset Time

- 7-day window reset: **2026-05-03 02:00 BKK**
- หลัง reset: sonnet/opus จะกลับมาใช้ได้ปกติ

---

## ผลการทดสอบ

| #   | Test                                | ผ่าน Gateway  | Result     | ความหมาย                                                          |
|-----|-------------------------------------|---------------|------------|-------------------------------------------------------------------|
| 1   | curl + dummy Bearer token (sonnet)  | ใช่           | 401        | transparent mode ทำงาน - ส่งตรง Anthropic, token ไม่ valid ก็ 401 |
| 2   | curl + stored OAuth token (sonnet)  | ใช่           | 429        | token valid แต่บัญชีโดน rate limit จริงจากการ test เยอะ           |
| 3   | curl ตรง api.anthropic.com (sonnet) | ไม่           | 429        | confirm: 429 มาจาก Anthropic จริง ไม่ใช่ gateway bug              |
| 4   | curl + stored OAuth token (haiku)   | ใช่           | **200 OK** | transparent mode ทำงานถูก, model ที่ยังไม่เต็มได้ 200             |
| 5   | remote CLI sonnet ตรง Anthropic     | ไม่           | **200 OK** | apiKeyHelper token ใช้ได้, sonnet ยังไม่เต็มสำหรับบัญชีนี้        |
| 6   | **remote CLI sonnet ผ่าน gateway**  | **ใช่**       | **200 OK** | **สำเร็จ! sonnet ผ่าน gateway ได้ 200**                           |
| 7   | **remote CLI opus ผ่าน gateway**    | **ใช่**       | **200 OK** | **สำเร็จ! opus ผ่าน gateway ได้ 200**                             |
| 8   | curl ไม่มี billing header (sonnet)  | ไม่ (direct)  | 429        | Rate limit 7-day exhausted                                        |
| 9   | curl มี billing header (sonnet)     | ไม่ (direct)  | **400**    | Reserved keyword ถูกบล็อก                                         |

### วิธีทดสอบแบบต่างๆ

**1. curl ผ่าน gateway (ด่วน verify)**
```bash
curl -X POST http://localhost:9000/v1/messages \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -H "anthropic-beta: claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14" \
  -H "x-app: cli" \
  -H "anthropic-dangerous-direct-browser-access: true" \
  -d '{"model":"claude-sonnet-4-6","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
```

**2. curl ยิงตรง Anthropic (เทียบ rate limit)**
```bash
curl -X POST 'https://api.anthropic.com/v1/messages?beta=true' \
  -H "Authorization: Bearer $TOKEN" \
  -H "anthropic-beta: claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14" \
  -H "anthropic-dangerous-direct-browser-access: true" \
  -d '{"model":"claude-sonnet-4-6","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
```

**3. Claude Code CLI ผ่าน gateway**
```bash
ANTHROPIC_BASE_URL=http://192.168.5.62:9000 claude --model claude-sonnet-4-6 --print "say hi"
```

**4. Remote CLI ผ่าน gateway (test ข้ามเครื่อง)**
```bash
# สร้าง apiKeyHelper script (ใช้ token จาก Redis)
echo '#!/bin/bash\necho "YOUR_TOKEN"' > ~/.claude/scripts/apikey-helper.sh
chmod +x ~/.claude/scripts/apikey-helper.sh

# ตั้งค่า settings.local.json
# "apiKeyHelper": "~/.claude/scripts/apikey-helper.sh"

# ยิงผ่าน gateway
ssh user@remote "ANTHROPIC_BASE_URL=http://192.168.5.62:9000 /opt/homebrew/bin/claude --model claude-sonnet-4-6 --print 'say hi'"
```

**5. ดู token จาก Redis**
```bash
docker exec arl-dragonfly redis-cli --raw GET 'arl:tokens:claude-oauth:claude-oauth_cIg-v3wo5QAA'
```

**6. ดู gateway logs**
```bash
docker logs arl-gateway -f 2>&1 | grep -E 'POST.*messages|upstream req|transparent'
```

---

## ผลกระทบ

- ทุก request ที่ client ส่ง OAuth Bearer token สำหรับ claude-* models จะ transparent passthrough
- Headers, body, auth token ถูก forward ตรงเหมือน CLI ตัวจริง
- Rate limit bucket เดียวกันกับ CLI = ไม่โดน 429 ผิด
- `arl_` keys (gateway API keys) จะไม่ trigger transparent mode
- `x-anthropic-billing-header` เป็น reserved keyword - CLI เท่านั้นที่ใช้ได้
- Token refresh URL แก้แล้ว (`api.anthropic.com` แทน `platform.claude.com`)
- Empty token guard ป้องกัน Redis corruption

---

## Timeline

| เวลา                 | เหตุการณ์                                                               |
|----------------------|-------------------------------------------------------------------------|
| 2026-04-29           | เริ่มจับ mitmproxy flows, CLI ตัวจริงได้ 200                            |
| 2026-04-30 00:00     | เริ่ม spawn 20 agents เพื่อหาสาเหตุ 429                                 |
| 2026-04-30 early     | พบ transparent mode bug, แก้ handler.go                                 |
| 2026-04-30 mid       | Token corruption: agents เขียน "null" ทับ Redis                         |
| 2026-04-30 mid       | Re-authenticate PKCE, token ใหม่ `claude-oauth_cIg-v3wo5QAA`            |
| 2026-04-30 mid       | พบ token refresh URL bug (`platform.claude.com` -> `api.anthropic.com`) |
| 2026-04-30 mid       | เพิ่ม empty token guard ใน token-refresh.go                             |
| 2026-04-30 late      | Test remote CLI sonnet/opus ผ่าน gateway = **200 OK**                   |
| 2026-04-30 late      | Test billing header = **400 reserved keyword**                          |
| 2026-05-03 02:00 BKK | 7-day rate limit reset (sonnet/opus กลับมาใช้ได้)                       |
