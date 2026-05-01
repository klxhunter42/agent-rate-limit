# Claude OAuth Transparent Passthrough: The Full Story

> บันทึกการพัฒนาระบบ Claude OAuth ตั้งแต่ต้นจนใช้งานได้จริง
> รวมสาเหตุ, ปัญหาที่เจอ, การวิเคราะห์, และวิธีแก้ทุกขั้นตอน

---

## สารบัญ

1. [ที่มาและเป้าหมาย](#1-ที่มาและเป้าหมาย)
2. [ทำไมตอนแรกใช้ได้แค่ Haiku](#2-ทำไมตอนแรกใช้ได้แค่-haiku)
3. [Rate Limit Bucket: สาเหตุที่แท้จริงของ 429](#3-rate-limit-bucket-สาเหตุที่แท้จริงของ-429)
4. [การวิเคราะห์ด้วย mitmproxy](#4-การวิเคราะห์ด้วย-mitmproxy)
5. [Billing Header: Reserved Keyword Problem](#5-billing-header-reserved-keyword-problem)
6. [Sidecar Solution](#6-sidecar-solution)
7. [Bug ที่เจอระหว่างทาง](#7-bug-ที่เจอระหว่างทาง)
8. [Profile Route: ทำให้ใช้ผ่าน arl_ token ได้](#8-profile-route-ทำให้ใช้ผ่าน-arl_-token-ได้)
9. [Architecture ปัจจุบัน](#9-architecture-ปัจจุบัน)
10. [Setup สำหรับเครื่องใหม่](#10-setup-สำหรับเครื่องใหม่)
11. [Timeline](#11-timeline)

---

## 1. ที่มาและเป้าหมาย

### สิ่งที่อยากทำ

ให้ Claude Code CLI บนเครื่อง remote (192.168.5.221) ใช้ **Sonnet** และ **Opus** ผ่าน API gateway (192.168.5.62:9000) แทนการยิงตรงไป Anthropic

```
ที่อยากได้:
  CLI (192.168.5.221) → Gateway (192.168.5.62:9000) → api.anthropic.com
                         ↑
                    มี rate limit, logging,
                    multi-profile, privacy guard
```

### ทำไมต้องผ่าน gateway

- มี rate limiting ป้องกันใช้เกินโควต้า
- มี logging/metrics ดู usage ได้
- มี privacy guard (PasteGuard) ป้องกันส่ง secret/PII ไปข้างนอก
- มี profile system จัดการหลาย account จากที่เดียว
- รวมหลาย provider (Claude, Gemini, Z.AI) ไว้ที่ endpoint เดียว

### ทำไมไม่ใช้ API key ปกติ

Anthropic API key (`sk-ant-api03-*`) ต้องจ่ายเงินต่อ token ใช้ ส่วน OAuth token (`sk-ant-oat01-*`) มาจาก Claude Pro subscription ที่จ่ายรายเดือนอยู่แล้ว ไม่มีค่าใช้จ่ายเพิ่มต่อ token

---

## 2. ทำไมตอนแรกใช้ได้แค่ Haiku

### สิ่งที่เจอ

เมื่อเริ่มใช้ claude-oauth transparent passthrough ครั้งแรก (ประมาณ 2026-04-29):

| Model | ผ่าน Gateway | ผลลัพธ์ |
|-------|:-----------:|---------|
| `claude-haiku-4-5-20251001` | Yes | **200 OK** |
| `claude-sonnet-4-6-20250514` | Yes | **429 Rate Limit** |
| `claude-opus-4-7-20250514` | Yes | **429 Rate Limit** |

Haiku ใช้ได้ปกติ แต่ Sonnet และ Opus โดน 429 ทุกครั้ง

### สาเหตุที่เป็นไปได้ (ตอนแรกคิด)

1. Anthropic บล็อก OAuth token จาก third-party tool
2. Rate limit หมดจริงจากการใช้เยอะ
3. Header ผิด / ขาด field บางอย่าง

### ที่เจอจริง: 2 สาเหตุร่วมกัน

**สาเหตุที่ 1: Transparent mode bug ใน handler.go**

Gateway มี transparent mode ที่ควรจะ forward headers/body ทั้งหมดตรงไป Anthropic เหมือน CLI ตัวจริง แต่มี bug:

```
ที่ควรเป็น:
  CLI (Bearer token) → Gateway → transparent=true → forward ALL → Anthropic
  = Rate limit bucket เดียวกับ CLI ตัวจริง = 200

ที่เกิดจริง (bug):
  CLI (Bearer token) → Gateway → transparent=false (bug!)
    → ใช้ API key auth แทน
    → modify body (optimizer, masking, prompt injection)
    → เขียนทับ headers จาก ExtraHeaders
    → ต่าง rate limit bucket กับ CLI
    → 429
```

สาเหตุ: `handler.go` ตรวจ transparent mode แค่เมื่อ resolver ตอบ `claude-oauth` แต่ถ้า resolver ตอบ `anthropic` (เพราะมี API key สำรองใน Redis) transparent จะไม่ถูกเปิด

**สาเหตุที่ 2: Rate limit หมดจริงจากการ test เยอะ**

หลังแก้ bug transparent mode แล้ว ยังโดน 429 เพราะ test ไปเยอะเกินจน 7-day utilization เต็ม

---

## 3. Rate Limit Bucket: สาเหตุที่แท้จริงของ 429

### Anthropic rate limit system

Anthropic มีหลาย rate limit bucket:

```
Bucket 1: API Key (sk-ant-api03-*)
  → จ่ายตาม token usage
  → rate limit ตาม tier

Bucket 2: OAuth generic (sk-ant-oat01-* ไม่มี billing header)
  → ใช้กับ claude.ai web, third-party app
  → rate limit ต่ำกว่า Claude Code

Bucket 3: Claude Code OAuth (sk-ant-oat01-* + billing header)
  → มี x-anthropic-billing-header injected
  → rate limit สูงกว่า bucket 2
  → เฉพาะ CLI ตัวจริงที่ใช้ bucket นี้
```

### ทำไม Haiku ผ่านแต่ Sonnet/Opus ไม่ผ่าน

Rate limit แยกกันต่อ model:

```
Anthropic rate limit structure:
  ┌──────────────────────────────┐
  │ claude-haiku-4-5             │ → limit สูงสุด, ใช้เยอะก็ยังไม่เต็ม
  ├──────────────────────────────┤
  │ claude-sonnet-4-6            │ → limit ปานกลาง, test เยอะเต็มเร็ว
  ├──────────────────────────────┤
  │ claude-opus-4-7              │ → limit ต่ำสุด
  └──────────────────────────────┘

จาก mitmproxy: ตอนที่เจอ 429 sonnet:
  - 5h utilization: 0.01 (ต่ำมาก)
  - 7d utilization: 14.0 (เต็มจากการ test)
```

### ที่ CLI ตัวจริงไม่โดน 429 เพราะอะไร

CLI ตัวจริง inject `x-anthropic-billing-header` เป็น `system[0]` ซึ่งทำให้ request ไปอยู่ใน **Claude Code rate limit bucket** (bucket 3) ที่มี limit สูงกว่า

```
CLI ตัวจริง:
  system[0] = "x-anthropic-billing-header: cc_version=2.1.123.a9a; ..."
  → Anthropic จับเข้า Claude Code bucket
  → limit สูงกว่า, ไม่ค่อยโดน 429

Gateway (ตอนแรก, ไม่มี billing header):
  ไม่มี billing header
  → Anthropic จับเข้า generic OAuth bucket
  → limit ต่ำกว่า, โดน 429 เร็วกว่า
```

---

## 4. การวิเคราะห์ด้วย mitmproxy

### วิธีการ

ใช้ mitmproxy จับ traffic ของ CLI ตัวจริงเพื่อดูว่ามันส่งอะไรไปบ้าง แล้วเปรียบเทียบกับที่ gateway ส่ง

```
จำนวน HTTP flows ที่จับได้: 28 flows

Phase breakdown:
  ┌────────────────────────────────────────────────────────┐
  │ 1. Health check                                       │
  │    /api/hello, /v1/oauth/hello → 200                  │
  ├────────────────────────────────────────────────────────┤
  │ 2. OAuth PKCE                                         │
  │    /v1/oauth/token → 200 (ได้ access_token)           │
  ├────────────────────────────────────────────────────────┤
  │ 3. Profile fetch                                      │
  │    /api/oauth/profile → 200 (subscription: pro)       │
  │    /api/oauth/claude_cli/roles → 200                  │
  ├────────────────────────────────────────────────────────┤
  │ 4. Config                                             │
  │    /v1/mcp_servers → 200/404                          │
  │    /api/claude_code/settings → 200                    │
  │    /api/claude_code/policy_limits → 200               │
  ├────────────────────────────────────────────────────────┤
  │ 5. MCP Init (burst!)                                  │
  │    /v1/mcp/{11 servers} → 401/502/429/200             │
  │    ยิง 11 requests พร้อมกันใน ~150ms                 │
  ├────────────────────────────────────────────────────────┤
  │ 6. LLM Request (สำคัญที่สุด)                         │
  │    /v1/messages?beta=true → 200                       │
  │    utilization: 5h=0.01, 7d=0.09 (ต่ำมาก)            │
  └────────────────────────────────────────────────────────┘
```

### สิ่งที่ค้นพบจาก flow ที่ 6 (LLM Request)

CLI ตัวจริงส่ง headers เหล่านี้ไป `/v1/messages?beta=true`:

```
Authorization: Bearer sk-ant-oaitkn-...
Content-Type: application/json
User-Agent: claude-cli/2.1.123 (external, cli)
anthropic-beta: claude-code-20250219,oauth-2025-04-20,
                interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,
                context-management-2025-06-27,prompt-caching-scope-2026-01-05,
                advanced-tool-use-2025-11-20,effort-2025-11-24
anthropic-dangerous-direct-browser-access: true
x-app: cli
X-Stainless-Lang: js
X-Stainless-Package-Version: 0.81.0
X-Stainless-OS: MacOS
X-Stainless-Arch: arm64
X-Stainless-Runtime: node
X-Stainless-Runtime-Version: v24.3.0
```

### สิ่งที่ค้นพบใน request body

CLI inject billing header เป็น `system[0]`:

```json
{
  "system": [
    {
      "type": "text",
      "text": "x-anthropic-billing-header: cc_version=2.1.123.a9a; cc_entrypoint=cli; cch=c7d70;"
    },
    {
      "type": "text",
      "text": "You are Claude Code, Anthropic's official CLI for Claude."
    },
    {
      "type": "text",
      "text": "..." // actual system prompt
    }
  ],
  "messages": [...]
}
```

---

## 5. Billing Header: Reserved Keyword Problem

### การค้นพบ

เมื่อเห็นว่า CLI ตัวจริง inject billing header ก็ลองเลียนแบบด้วย curl:

```bash
curl -X POST https://api.anthropic.com/v1/messages \
  -H "Authorization: Bearer sk-ant-oat01-..." \
  -H "anthropic-beta: oauth-2025-04-20" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 50,
    "system": [{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.123.abc; cc_entrypoint=cli; cch=00000;"}],
    "messages": [{"role":"user","content":"hi"}]
  }'
```

**ผลลัพธ์: 400 Bad Request**

```json
{
  "type": "error",
  "error": {
    "type": "invalid_request_error",
    "message": "x-anthropic-billing-header is a reserved keyword and may not be used in the system prompt."
  }
}
```

### สรุป

```
┌──────────────────────────────────────────────────────────────┐
│ x-anthropic-billing-header เป็น RESERVED KEYWORD             │
│                                                              │
│ ✅ CLI ตัวจริง: ใช้ได้ (Anthropic allowlist ตาม TLS fingerprint) │
│ ❌ curl/Go/Python: ใช้ไม่ได้ (reserved keyword → 400)         │
│ ❌ Gateway (Go): ใช้ไม่ได้ (same 400 error)                   │
│                                                              │
│ ข้อสรุป: Anthropic ตรวจสอบ TLS fingerprint ของ caller       │
│ เฉพาะ official CLI client เท่านั้นที่สามารถ inject ได้       │
└──────────────────────────────────────────────────────────────┘
```

### ทดสอบเพิ่มเติม

| Test | Result | หมายเหตุ |
|------|--------|----------|
| curl ไม่มี billing header (sonnet) | 429 | 7-day exhausted |
| curl มี billing header (sonnet) | **400** | Reserved keyword blocked |
| CLI ตัวจริง (sonnet) | **200** | CLI ใช้ได้เพราะ TLS fingerprint |
| Gateway transparent mode (sonnet, CLI ส่งตรง) | **200** | Forward headers ตรง, billing header มาจาก CLI |

---

## 6. Sidecar Solution

### แนวคิด

เนื่องจาก Go ส่ง billing header เองไม่ได้ (reserved keyword) จึงต้องใช้ Node.js เป็น sidecar proxy:

```
ทำไมต้อง Node.js:
  1. CLI ตัวจริงเป็น Node.js app (cli.js)
  2. Node.js ใช้ OpenSSL/BoringSSL เหมือน CLI
  3. TLS fingerprint ของ Node.js ≈ TLS fingerprint ของ CLI
  4. Anthropic ตรวจ TLS fingerprint → Node.js ผ่าน
```

### Architecture

```
┌─────────────────────────────────────────────────────┐
│               arl-gateway container                  │
│                                                      │
│  ┌──────────────────┐      ┌──────────────────────┐ │
│  │  Go Gateway      │      │  Node.js Sidecar     │ │
│  │  :8080           │      │  :8081               │ │
│  │                  │      │                      │ │
│  │  - HTTP routing  │─────▶│  - Parse JSON body   │ │
│  │  - Auth          │      │  - Compute hash      │ │
│  │  - Rate limit    │      │  - Inject billing    │ │
│  │  - Privacy guard │      │  - Inject identity   │ │
│  │  - Profile       │      │  - HTTPS proxy       │ │
│  │                  │      │  - Zero dependencies  │ │
│  └──────────────────┘      └──────────┬───────────┘ │
│                                       │              │
│  entrypoint.sh starts both processes  │              │
└───────────────────────────────────────┼──────────────┘
                                        │
                                        │ HTTPS
                                        ▼
                                  api.anthropic.com
```

### Billing Header Algorithm

Reverse-engineered จาก `cli.js` v2.1.123 (functions: `Fv8()`, `h$7()`, `cq()`):

```
SALT    = "59cf53e54c78"
VERSION = "2.1.123"

Step 1: Extract first user message text
  firstMsg = messages.find(m => m.role === "user" && !m.isMeta)
  text = firstMsg.content (string or first text block)

Step 2: Compute build hash
  chars = [text[4], text[7], text[20]]
          .map(c => c || "0")     // fallback to "0" if index out of range
          .join("")
  hash  = SHA256(SALT + chars + VERSION)
          .digest("hex")
          .slice(0, 3)

Step 3: Build header string
  "cc_version=${VERSION}.${hash}; cc_entrypoint=cli; cch=00000;"

Step 4: Inject as system[0]
  system.unshift({"type":"text","text":"x-anthropic-billing-header: " + headerStr})

Step 5: Inject identity as system[1]
  system.splice(1, 0, {"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."})
```

### การทำงานของ Sidecar (index.js)

```
HTTP POST เข้ามา (จาก Go gateway)
  │
  ├── 1. Parse JSON body
  │
  ├── 2. Extract first user message text
  │     messages.find(m => m.role === "user" && !m.isMeta)
  │
  ├── 3. Compute billing header
  │     SHA256(SALT + chars + VERSION) → 3-char hash
  │     Build: "cc_version=2.1.123.{hash}; cc_entrypoint=cli; cch=00000;"
  │
  ├── 4. Inject into system array
  │     system[0] = billing header (skip if already present)
  │     system[1] = identity string (skip if already present)
  │
  ├── 5. Forward ALL headers as-is
  │     Keep Authorization: Bearer (do NOT convert to x-api-key)
  │     Skip hop-by-hop headers only (connection, keep-alive, etc.)
  │
  ├── 6. HTTPS POST to api.anthropic.com
  │     Node.js https module (same TLS stack as CLI)
  │     Stream response back via pipe()
  │
  └── 7. Health check: GET /health → {status: "ok"}
```

---

## 7. Bug ที่เจอระหว่างทาง

### Bug 1: Transparent mode detection (handler.go)

**สาเหตุ**: Gateway ตรวจ transparent mode แค่เมื่อ resolver ตอบ `claude-oauth` ถ้า resolver ตอบ `anthropic` (เพราะมี API key สำรอง) transparent จะไม่เปิด

**แก้**: เพิ่ม fallback - ถ้า client ส่ง OAuth Bearer token สำหรับ claude-* model ให้ force transparent=true เสมอ

```
ก่อน:
  transparent = true เฉพาะเมื่อ resolver ตอบ "claude-oauth"
  → ถ้า resolver ตอบ "anthropic" (มี API key) → transparent = false → โดน 429

หลัง:
  transparent = true เมื่อ:
  1. resolver ตอบ "claude-oauth" + มี Bearer token
  2. resolver ตอบอะไรก็ตาม + client ส่ง OAuth Bearer token + model เป็น claude-*
  3. apiKey เริ่มด้วย "sk-ant-oat01-" (profile route)
```

### Bug 2: Token refresh URL (registry.go)

**สาเหตุ**: Token refresh URL เป็น `platform.claude.com/v1/oauth/token` ซึ่ง return 429 เสมอ

**แก้**: เปลี่ยนเป็น `api.anthropic.com/v1/oauth/token`

### Bug 3: Empty token corruption (token-refresh.go)

**สาเหตุ**: เมื่อ refresh ได้ empty access_token (จาก error) code เขียน empty string ทับ token เดิมใน Redis

**แก้**: เพิ่ม guard - ถ้า access_token ว่าง ไม่เขียน Redis

### Bug 4: Sidecar แปลง Bearer เป็น x-api-key

**สาเหตุ**: Sidecar เดิมแปลง `Authorization: Bearer` → `x-api-key` เพราะคิดว่า Anthropic ต้องการแบบนั้น

**ผลลัพธ์**: `401 invalid x-api-key` เพราะ OAuth token ต้องส่งเป็น `Authorization: Bearer` เท่านั้น

**แก้**: ลบการแปลงออก, forward `Authorization: Bearer` เดิม

### Bug 5: ขาด oauth-2025-04-20 beta flag

**สาเหตุ**: `anthropic-beta` header ไม่มี flag `oauth-2025-04-20` → Anthropic ปฏิเสธ OAuth auth

**ผลลัพธ์**: `401 OAuth authentication is currently not supported`

**แก้**: Gateway แทรก `oauth-2025-04-20` อัตโนมัติสำหรับ OAuth requests

### Bug 6: Slice bounds panic (anthropic.go:895)

**สาเหตุ**: Log statement ทำ `header[:30]` โดยไม่เช็ค length เมื่อ header ว่าง (profile route ไม่มี Authorization) → panic

**ผลลัพธ์**: `502 Bad Gateway` (gateway crash)

**แก้**: เพิ่ม `truncate()` helper ที่เช็ค length ก่อน slice

---

## 8. Profile Route: ทำให้ใช้ผ่าน arl_ token ได้

### ปัญหา

เมื่อใช้ transparent mode ผ่าน CLI ตรง (client ส่ง Bearer token เอง) ทำงานได้ แต่เมื่อใช้ profile API token (`arl_*`) แทน:

1. Client ส่ง `x-api-key: arl_...` (ไม่ใช่ Bearer)
2. Gateway ดึง OAuth token จาก Redis ผ่าน profile system
3. แต่ `transparent=false` เพราะ `isClaudeOAuthToken(r)` เช็ค headers ของ client ไม่ใช่ apiKey
4. ถ้าไป ProxyTransparent โดยไม่ผ่าน sidecar → ไม่มี billing header → 429

### แก้ 3 จุด

**จุดที่ 1: Detect profile OAuth token**

หลังจาก profile เลือก token แล้ว ถ้า apiKey เป็น `sk-ant-oat01-*` ให้ force transparent=true:

```
apiKey จาก profile = "sk-ant-oat01-..."
→ transparent = true
→ จะได้ route ผ่าน sidecar
```

**จุดที่ 2: Fix headers ก่อนส่ง sidecar**

ก่อนเรียก ProxySidecar ต้องแก้ headers:

```
ก่อน:
  x-api-key: arl_...        (client's original, ใช้ไม่ได้กับ Anthropic)
  Authorization: (empty)

หลังแก้:
  x-api-key: (deleted)
  Authorization: Bearer sk-ant-oat01-...   (จาก Redis)
  anthropic-beta: ...oauth-2025-04-20...   (เพิ่มถ้ายังไม่มี)
```

**จุดที่ 3: ใส่ oauth-2025-04-20 ให้อัตโนมัติ**

Client ที่ใช้ `arl_` token ผ่าน curl อาจไม่ได้ส่ง `anthropic-beta` → gateway แทรก `oauth-2025-04-20` ให้

---

## 9. Architecture ปัจจุบัน

### Full Flow Diagram

```
┌──────────────────────────────────────────────────────────────────────────┐
│                    Claude Code CLI (192.168.5.221)                       │
│                                                                          │
│  ANTHROPIC_BASE_URL=http://192.168.5.62:9000                            │
│  ANTHROPIC_API_KEY=arl_2f3a72a7...                                      │
│                                                                          │
│  POST /v1/messages                                                      │
│  x-api-key: arl_2f3a72a7...                                             │
│  Body: {model:"claude-sonnet-4-20250514", messages:[...]}               │
└───────────────────────────────┬──────────────────────────────────────────┘
                                │
                                ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                     Caddy Reverse Proxy (:9000)                          │
│                     arl-proxy container                                  │
└───────────────────────────────┬──────────────────────────────────────────┘
                                │
                                ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                    API Gateway (Go, :8080)                                │
│                    arl-gateway container                                 │
│                                                                          │
│  Step 1: Resolve arl_ token                                              │
│     arl_2f3a72a7... → ResolveProfileToken() → profile "th15011880"      │
│                                                                          │
│  Step 2: Load profile                                                    │
│     profile.target = "claude-oauth"                                      │
│     profile.provider = "claude-oauth"                                    │
│                                                                          │
│  Step 3: Select token                                                    │
│     GetDefault("claude-oauth") → sk-ant-oat01-eGNq... from Redis        │
│                                                                          │
│  Step 4: Detect transparent                                              │
│     apiKey starts with "sk-ant-oat01-" → transparent = true              │
│                                                                          │
│  Step 5: Privacy scan (PasteGuard)                                       │
│     Scan body for secrets/PII → none → skip                             │
│                                                                          │
│  Step 6: Fix headers for sidecar                                         │
│     ├ Set Authorization: Bearer sk-ant-oat01-...                         │
│     ├ Del x-api-key (remove arl_ token)                                  │
│     └ Set anthropic-beta: ...oauth-2025-04-20...                         │
│                                                                          │
│  Step 7: Route to sidecar                                                │
│     transparent=true + sidecarURL → ProxySidecar()                       │
│     Fallback: ProxyTransparent() (no billing header)                     │
└───────────────────────────────┬──────────────────────────────────────────┘
                                │
                                ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                 Node.js Sidecar (:8081)                                   │
│                 Same container as Gateway                                 │
│                                                                          │
│  Step 8: Parse JSON body                                                 │
│                                                                          │
│  Step 9: Extract first user message text                                 │
│     "Say hi in 3 words"                                                  │
│                                                                          │
│  Step 10: Compute billing header                                         │
│     chars = [text[4], text[7], text[20]] = "i", "3", "r"                │
│     hash  = SHA256("59cf53e54c78" + "i3r" + "2.1.123").hex[:3] = "a9a" │
│     header = "cc_version=2.1.123.a9a; cc_entrypoint=cli; cch=00000;"   │
│                                                                          │
│  Step 11: Inject system[0] = billing header                              │
│  Step 12: Inject system[1] = identity string                             │
│                                                                          │
│  Step 13: Forward to api.anthropic.com                                   │
│     Node.js https module (same TLS fingerprint as real CLI)              │
│     Keep all headers: Authorization: Bearer, anthropic-beta, etc.        │
└───────────────────────────────┬──────────────────────────────────────────┘
                                │
                                │ HTTPS POST
                                ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                   api.anthropic.com/v1/messages                          │
│                                                                          │
│  Auth:      Bearer sk-ant-oat01-... (OAuth)                              │
│  Billing:   Claude Code bucket (system[0] = billing header)             │
│  Beta:      oauth-2025-04-20 (OAuth auth enabled)                       │
│                                                                          │
│  → 200 OK                                                                │
│  → {"content":[{"type":"text","text":"Hello there, human!"}]}           │
└───────────────────────────────┬──────────────────────────────────────────┘
                                │
                                │ 200 OK (SSE or JSON)
                                ▼
                         Back to CLI client
```

### Auth Path Comparison

```
Path A: CLI ตัวจริง (Direct)
  CLI → ANTHROPIC_BASE_URL=api.anthropic.com
  CLI inject billing header เองใน client code
  → Bearer + billing header → 200

Path B: CLI ผ่าน Gateway (apiKeyHelper)
  CLI → apiKeyHelper → ส่ง Bearer token ตรง
  Gateway → transparent=true → forward headers เดิม (มี billing header จาก CLI)
  Sidecar → inject billing header (ซ้ำ? skip เพราะ detect ว่ามีอยู่แล้ว)
  → 200

Path C: CLI ผ่าน Gateway (arl_ profile token)  ← ที่ใช้ตอนนี้
  CLI → ANTHROPIC_API_KEY=arl_...
  Gateway → profile → GetDefault → sk-ant-oat01-* from Redis
  Gateway → fix headers → Bearer + oauth-2025-04-20
  Gateway → transparent=true → sidecar
  Sidecar → inject billing header + identity
  → 200

Path D: curl (debugging)
  curl → x-api-key: arl_...
  (same as Path C from gateway perspective)
  → 200
```

### Files

| File | หน้าที่ | ขนาด |
|------|---------|------|
| `api-gateway/sidecar/index.js` | Node.js proxy, billing injection, zero dependencies | ~170 lines |
| `api-gateway/sidecar/entrypoint.sh` | เริ่ม Go + Node พร้อมกันใน container เดียว | ~13 lines |
| `api-gateway/sidecar/package.json` | No dependencies | 6 lines |
| `api-gateway/Dockerfile` | Multi-stage build, `apk add nodejs`, copy sidecar/ | - |
| `api-gateway/config/config.go` | Sidecar config: URL, enabled | - |
| `api-gateway/handler/handler.go` | Profile routing, transparent detection, header fix | - |
| `api-gateway/proxy/anthropic.go` | `ProxySidecar()`, `ProxyTransparent()`, `truncate()` | - |

### Config Env Vars

| Env Var | Default | Description |
|---------|---------|-------------|
| `CLI_SIDECAR_ENABLED` | `true` | เปิด/ปิด sidecar routing |
| `CLI_SIDECAR_URL` | `http://127.0.0.1:8081` | Sidecar URL (same container) |
| `SIDECAR_PORT` | `8081` | Node.js sidecar listen port |
| `ANTHROPIC_DIRECT_URL` | `https://api.anthropic.com` | Anthropic direct URL |

---

## 10. Setup สำหรับเครื่องใหม่

### Step 1: OAuth flow (one-time)

Gateway มี OAuth flow ของตัวเอง - เปิด browser authorize แล้ว token เก็บใน Redis:

```
1. เปิด: http://GATEWAY:9000/v1/auth/claude-oauth/start-url
2. Gateway สร้าง PKCE authorize URL → เปิด browser
3. User กด Authorize ใน claude.ai
4. Redirect กลับ gateway → /callback
5. Gateway แลก token → เก็บใน Redis
6. Token: arl:tokens:claude-oauth:claude-oauth_XXXX
```

### Step 2: สร้าง profile

```bash
curl -X POST http://GATEWAY:9000/v1/profiles \
  -H "Content-Type: application/json" \
  -d '{"name": "my-profile", "target": "claude-oauth"}'
```

### Step 3: สร้าง profile API token

ผ่าน Dashboard UI: `/admin/profiles` → profile name → Generate API Key

หรือ API:

```bash
curl -X POST http://GATEWAY:9000/v1/profiles/my-profile/tokens \
  -H "Content-Type: application/json" \
  -d '{"ttl": "720h"}'
# → {"token": "arl_XXXX..."}
```

### Step 4: ตั้งค่า CLI บนเครื่อง remote

```bash
# Option A: env vars
export ANTHROPIC_BASE_URL=http://GATEWAY:9000
export ANTHROPIC_API_KEY=arl_XXXX...
claude

# Option B: ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://GATEWAY:9000",
    "ANTHROPIC_API_KEY": "arl_XXXX..."
  }
}
```

### Step 5: ทดสอบ

```bash
# Sonnet
claude -p "Say hello" --model claude-sonnet-4-20250514

# Opus
claude -p "Say hello" --model claude-opus-4-20250514

# curl test
curl -s -X POST http://GATEWAY:9000/v1/messages \
  -H "x-api-key: arl_XXXX..." \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{"model":"claude-sonnet-4-20250514","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'
```

---

## 11. Timeline

| วันที่ | เหตุการณ์ |
|--------|-----------|
| **2026-04-23** | เริ่มใช้ claude-oauth transparent passthrough ครั้งแรก, Haiku ผ่าน |
| **2026-04-29** | พบว่า Sonnet/Opus โดน 429 ตลอด, เริ่ม investigate |
| **2026-04-30 00:00** | จับ mitmproxy flows 28 flows, CLI ตัวจริงได้ 200 |
| **2026-04-30 early** | พบ transparent mode bug ใน handler.go, แก้ |
| **2026-04-30 mid** | Token corruption: agents เขียน "null" ทับ Redis |
| **2026-04-30 mid** | Re-authenticate PKCE, token ใหม่ `claude-oauth_C8A-88LMMwAA` |
| **2026-04-30 mid** | พบ token refresh URL bug (`platform.claude.com` → `api.anthropic.com`) |
| **2026-04-30 mid** | เพิ่ม empty token guard ใน token-refresh.go |
| **2026-04-30 late** | Test remote CLI sonnet/opus ผ่าน gateway = **200 OK** |
| **2026-04-30 late** | พบ billing header = **400 reserved keyword** → ต้องใช้ sidecar |
| **2026-05-01** | สร้าง Node.js sidecar (`sidecar/index.js`) สำหรับ billing header injection |
| **2026-05-01** | Deploy sidecar, test curl → Caddy → gateway → sidecar → Anthropic = **200** |
| **2026-05-01** | พบ bug: sidecar แปลง Bearer → x-api-key → **401** → แก้ |
| **2026-05-01** | พบ bug: ขาด `oauth-2025-04-20` → **401** → แก้ |
| **2026-05-02** | Profile route: `arl_` token → profile → OAuth → sidecar → Anthropic = **200** |
| **2026-05-02** | พบ bug: slice bounds panic → แก้ด้วย `truncate()` helper |
| **2026-05-02** | พบ bug: profile route ไม่ set transparent → แก้ |
| **2026-05-02** | พบ bug: OAuth token ไปเป็น x-api-key ไม่ใช่ Bearer → แก้ header fix |
| **2026-05-02** | CLI จริงบน 192.168.5.221 ใช้ Sonnet + Opus ผ่าน gateway ได้สำเร็จ |
