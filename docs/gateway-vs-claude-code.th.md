# Gateway vs Claude Code จริง: เปรียบเทียบ Traffic

## ภาพรวม

เอกสารนี้เปรียบเทียบว่า ARL gateway ส่ง request ไป Anthropic API ต่างกับ Claude Code CLI จริงอย่างไร เข้าใจความแตกต่างเหล่านี้สำคัญมากสำหรับการ debug ปัญหา compatibility และการทำให้ pass-through ทำงานได้ราบรื่น

---

## 1. Authentication

| ด้าน | Claude Code CLI จริง | ARL Gateway |
|---|---|---|
| วิธี auth | `Authorization: Bearer {oauth_token}` | เหมือนกัน - `AuthMode: "bearer"` สำหรับ claude-oauth |
| แหล่ง token | `~/.claude/.credentials.json` -> `claudeAiOauth.accessToken` | Redis `arl:tokens:claude-oauth:{accountID}` |
| การหมุน token | token เดียว, ต้อง re-auth เอง | Round-robin หลาย account, auto-rotate เมื่อเจอ 429 |
| การ refresh token | CLI จัดการ OAuth refresh เอง | RefreshWorker ทุก 30 นาที + refresh เมื่อเจอ 401 |
| Auth header | `Authorization: Bearer {token}` | เหมือนกัน |

**สิ่งสำคัญต่าง:** Gateway หมุน account อื่นได้เมื่อเจอ 429, ในขณะที่ CLI จริงใช้ account เดียว

---

## 2. Request Headers

### Headers ที่ส่งทั้งคู่ (เหมือนกัน)

| Header | ค่า |
|---|---|
| `Content-Type` | `application/json` |
| `Authorization` | `Bearer {token}` |
| `anthropic-version` | `2023-06-01` |
| `anthropic-dangerous-direct-browser-access` | `true` |
| `Accept` | `application/json` |
| `accept-language` | `*` |
| `sec-fetch-mode` | `cors` |

### Beta headers

| ด้าน | Claude Code จริง | ARL Gateway |
|---|---|---|
| `anthropic-beta` | `claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,effort-2025-11-24` | เซ็ตเดียวกัน, merge กับ `anthropic-beta` จาก client (dedup) |
| Beta stripping | ไม่มี (ส่ง beta ทั้งหมดเสมอ) | Strip `effort-*` และ `interleaved-thinking-*` สำหรับ haiku/3-5-sonnet |

### X-Stainless headers (fingerprint headers)

| Header | ค่า |
|---|---|
| `X-Stainless-Lang` | `js` |
| `X-Stainless-Package-Version` | `0.81.0` |
| `X-Stainless-OS` | `MacOS` |
| `X-Stainless-Arch` | `arm64` |
| `X-Stainless-Runtime` | `node` |
| `X-Stainless-Runtime-Version` | `v24.3.0` |
| `X-Stainless-Retry-Count` | `0` |
| `X-Stainless-Timeout` | `3000` |

**สถานะ:** เหมือนกันทั้งหมดระหว่าง CLI จริงกับ gateway Gateway hardcode ค่าเหล่านี้ใน `providerRouteTable`

### Headers เพิ่มเติม

| Header | CLI จริง | Gateway | หมายเหตุ |
|---|---|---|---|
| `x-client-request-id` | CLI สร้างเอง | รับจาก client หรือสุ่ม UUID | Gateway pass through หรือสร้างใหม่ |
| `X-Claude-Code-Session-Id` | CLI สร้างเอง | รับจาก client หรือสุ่ม UUID | Gateway pass through หรือสร้างใหม่ |
| `x-app` | `cli` | `cli` | เหมือนกัน |
| `User-Agent` | `claude-cli/2.1.118 (external, sdk-cli)` | เหมือนกัน | เหมือนกัน |

### URL path

| ด้าน | CLI จริง | Gateway |
|---|---|---|
| Endpoint | `/v1/messages?beta=true` | เหมือนกัน สำหรับ claude-oauth provider |

---

## 3. Request Body Transformations

### สิ่งที่ gateway แก้ไข (CLI จริงไม่ทำอะไรเลย)

| Transform | เปิดเมื่อไร | รายละเอียด |
|---|---|---|
| แทรก system prompt | เปิดอยู่เสมอ (ถ้าเปิดใช้) | แทรก `[GATEWAY RULES]` และ `[VISION]` ไปด้านหน้า system prompt |
| แทนที่ model | Profile routing | Profile สามารถบังคับ model หรือ provider default |
| แทนที่ max_tokens | Smart mode (เปิดเป็นค่า default) | ตั้ง max_tokens ตาม model ถ้า client ไม่ได้ส่งมา |
| Strip field | Per-model logic | ลบ `thinking`, `budget_tokens`, `effort` สำหรับ haiku/3-5-sonnet |
| ลบ `context_management` | Non-Anthropic upstreams | Strip ออกสำหรับ provider ที่ไม่ใช่ bearer |
| ลบ `service_tier` | เสมอ | ลบออกเสมอ |
| Privacy masking | เสมอ (ถ้าเปิดใช้) | PII/secrets แทนที่ด้วย `[[TYPE_N]]` placeholder |
| กรอง content | GLM mode เท่านั้น | ลบ `server_tool_use`, แปลง image blocks |
| 优化 system prompt | Request ที่ไม่ได้ mask | ลบ whitespace เกิน + dedup ประโยคซ้ำ |

### สิ่งที่ส่งผ่านโดยไม่แก้ไข

- `messages` array (ยกเว้น privacy masking)
- `tools` definitions
- `temperature`, `top_p`, `stop_sequences`
- `stream` flag
- `metadata`

---

## 4. Streaming SSE Behavior

### Event relay (Anthropic native path)

Gateway ทำหน้าที่เป็น SSE relay โปร่งใสสำหรับ response แบบ Anthropic:

| Event Type | การจัดการ |
|---|---|
| `message_start` | ส่งผ่าน + ดึง `input_tokens` สำหรับ metrics |
| `content_block_start` | ส่งผ่านโดยไม่แก้ไข |
| `content_block_delta` | ส่งผ่าน + unmask field `text`/`thinking` ถ้าเปิด privacy masking |
| `content_block_stop` | ส่งผ่านโดยไม่แก้ไข |
| `message_delta` | ส่งผ่าน + ดึง `output_tokens` สำหรับ metrics |
| `message_stop` | ส่งผ่านโดยไม่แก้ไข |
| `ping` | ส่งผ่านโดยไม่แก้ไข |
| `error` | ส่งผ่านโดยไม่แก้ไข |

### ความต่างจาก API ตรง

| ด้าน | API ตรง | Gateway |
|---|---|---|
| ลำดับ event | ไม่แก้ไข | เหมือนกัน (ไม่มีการ reorder) |
| ประเภท event | ส่งผ่านทั้งหมด | เหมือนกัน |
| Privacy unmasking | ไม่มี | `[[TYPE_N]]` placeholder แทนที่กลับเป็นค่าจริง |
| Stream timeout | ไม่มี (client-side เท่านั้น) | จำกัด 10 นาที |
| ขนาดบรรทัดสูงสุด | ไม่ระบุ | 256KB ต่อ SSE line |
| TTFB measurement | วัดที่ client-side | Gateway วัดสำหรับ Prometheus metrics |
| Response headers | headers ทั้งหมดจาก Anthropic | กรองเฉพาะ allowlist (rate limit headers, Request-Id, Content-Type) |

### Rate limit response headers ที่ส่งผ่าน

Gateway ส่งต่อ headers เหล่านี้จาก Anthropic ไปยัง client:

```
Anthropic-Ratelimit-Requests-Remaining
Anthropic-Ratelimit-Tokens-Remaining
Anthropic-Ratelimit-Unified-Status
Anthropic-Ratelimit-Unified-5h-Status
Anthropic-Ratelimit-Unified-5h-Utilization
Anthropic-Ratelimit-Unified-5h-Reset
Anthropic-Ratelimit-Unified-7d-Status
Anthropic-Ratelimit-Unified-7d-Utilization
Anthropic-Ratelimit-Unified-7d-Reset
Anthropic-Ratelimit-Unified-Fallback-Percentage
Anthropic-Ratelimit-Unified-Fallback
Anthropic-Ratelimit-Unified-Reset
X-RateLimit-Limit
X-RateLimit-Remaining
X-RateLimit-Reset
Retry-After
```

---

## 5. Error Handling & Retry

| ด้าน | API ตรง (CLI) | Gateway |
|---|---|---|
| จัดการ 429 | CLI แสดง error, user รอเอง | Retry สูงสุด 3 ครั้ง ด้วย quadratic backoff, หมุน account, cooldown 2 นาที |
| จัดการ 401 | CLI refresh token | Gateway refresh token + retry 1 ครั้ง |
| จัดการ 503 | CLI แสดง error | Retry + cooldown |
| Backoff strategy | ไม่มี (user retry เอง) | `500ms * attempt^2`, สูงสุด 5 นาที |
| การหมุน account | ไม่มี (account เดียว) | หมุนไป account ถัดไปใน pool เมื่อเจอ 429 |
| Per-model cooldown | ไม่มี | Cooldown 2 นาทีต่อ provider+model |

### ผังการ Retry

```
Request -> ProxyTransparent
  |
  +-- attempt 0: ส่งไป upstream
  |     +-- 429 -> rotateAccountFn -> ลองใหม่
  |     +-- 401 -> oauthRefreshFn -> retry ด้วย token ใหม่
  |     +-- 2xx -> unmask + ส่งกลับ
  |
  +-- attempt 1..N: เหมือนกัน พร้อม backoff
  |
  +-- retry หมด -> ส่ง error ล่าสุดกลับไป client
```

---

## 6. Privacy Pipeline (PasteGuard)

มีเฉพาะใน gateway CLI จริงส่ง content ดิบไป Anthropic

### Masking (request -> upstream)

1. ดึง text spans จาก: system prompt, message content, tool inputs
2. ตรวจจับ secrets (regex): SSH keys, API keys (sk-, AKIA-, ghp-, glpat-), JWTs, connection strings
3. ตรวจจับ PII (Presidio): PERSON, EMAIL_ADDRESS, PHONE_NUMBER
4. แทนที่ด้วย `[[TYPE_N]]` placeholder
5. ส่ง payload ที่ mask แล้วไป upstream

### Unmasking (upstream -> client)

1. Non-stream: แทนที่ข้อความทั้งก้อนใน response body
2. Stream: ประมวลผลทีละ chunk ผ่าน `StreamUnmasker`
   - บัฟเฟอร์ partial `[[...` placeholder ที่ข้ามขอบเขต SSE chunk
   - ลำดับ unmask: secrets ก่อน (ชั้นในสุด), แล้วค่อย PII (ชั้นนอก)
   - จัดการ field `content_block_delta.text` และ `.thinking`
   - Raw replacement สำหรับ event types อื่นที่มี `[[`

---

## 7. Response Trimming

ฟีเจอร์เฉพาะ gateway (configurable, เปิดเป็น default):

ลบ pattern คำพูดที่ซ้ำซ้อนออกจาก text content blocks:
- "Here's the/a...", "Let me explain/show...", "I'll help you..."
- "Sure!", "Certainly!", "Of course!", "Great question!"
- "Hope this helps!", "Let me know if you need anything else..."

ใช้เฉพาะ non-stream responses, หลัง unmask

---

## 8. พฤติกรรมเฉพาะ Model

| Model | การจัดการเฉพาะของ Gateway |
|---|---|
| `claude-haiku-*` | Strip thinking/effort betas และ fields, max_tokens=8192, ไม่รองรับ thinking |
| `claude-3-5-sonnet` | Strip thinking/effort betas และ fields |
| `claude-sonnet-4-*` | รองรับ beta เต็ม, max_tokens=163840 |
| `claude-opus-4-7` | รองรับ beta เต็ม, max_tokens=163840 |

---

## 9. Configuration Endpoints (เฉพาะ gateway)

| Endpoint | วัตถุประสงค์ |
|---|---|
| `GET/PUT /v1/thinking` | ค่า thinking budget ต่อ model |
| `GET/PUT /v1/max-tokens` | แทนที่ max_tokens ต่อ model |
| `GET /v1/limiter-override` | แทนที่ adaptive limiter |

---

## 10. Compatibility Matrix

| ฟีเจอร์ | CLI ตรง | ผ่าน Gateway | หมายเหตุ |
|---|---|---|---|
| OAuth Bearer auth | ได้ | ได้ | Headers เหมือนกัน |
| Streaming SSE | ได้ | ได้ | Relay โปร่งใส |
| Thinking blocks | ได้ | ได้ | ส่งผ่าน + unmask |
| Tool use | ได้ | ได้ | ส่งผ่าน |
| Context management | ได้ | บางส่วน | Strip สำหรับ non-Anthropic upstreams |
| Prompt caching | ได้ | ได้ | Beta header ถูกเก็บไว้ |
| Image inputs | ได้ | ได้ | ส่งผ่านสำหรับ Anthropic format |
| หมุนหลาย account | ไม่ได้ | ได้ | ฟีเจอร์ของ gateway |
| Privacy masking | ไม่ได้ | ได้ | PasteGuard pipeline |
| Rate limit headers | ได้ | บางส่วน | กรองเฉพาะ allowlist |
| Retry อัตโนมัติเมื่อ 429 | เอง | อัตโนมัติ (3x) | ฟีเจอร์ของ gateway |
| แทรก system prompt | ไม่ได้ | ได้ | Gateway เพิ่ม rules |
| ตัด response ยาว | ไม่ได้ | ได้ | ลบ pattern ซ้ำซ้อน |

---

## ปัญหาที่ทราบ

1. **Sonnet 429 ระดับ org**: ทุก claude-oauth accounts แชร์ org ID เดียวกัน, การหมุน account ไม่ข้าม org-level limits ได้ เปลี่ยน default model เป็น haiku เพื่อหลีกเลี่ยง

2. **SSH ไป 192.168.5.221**: MITM capture ต้องมี SSH key authorization key ปัจจุบันยังไม่ authorized บนเครื่อง remote

3. **Privacy filter ใน tool I/O**: `[[PERSON_X]]` marker ปรากฏใน tool output ของ Claude Code เอง, ไม่ใช่ใน source files จริง ไฟล์บน disk สะอาด (ตรวจสอบแล้วด้วย `git diff`)
