# Claude OAuth: 429 Prevention Guide

> สรุปสาเหตุและวิธีแก้ที่ทำให้ Claude CLI (claude-oauth) request ผ่าน gateway แล้วไม่โดน 429 จาก Anthropic API
> อ้างอิงจาก debugging session วันที่ 2026-05-17

---

## ภาพรวม

Claude CLI ส่ง request ผ่าน gateway ไป Anthropic API ด้วย `user-agent: claude-cli/2.1.143 (external, cli)` และใช้ sidecar proxy (`http://127.0.0.1:8081`) พร้อม `?beta=true` ใน URL

429 ที่เกิดขึ้น **ไม่ใช่ rate limit จริง** แต่เกิดจาก gateway แก้ไข request body ทำให้:
1. Anthropic หา cached content ไม่เจอ -> cache miss ทุกรอบ
2. Input tokens สูงเกิน plan limit -> 429

---

## องค์ประกอบหลัก 5 ข้อที่ทำให้ 429

### 1. `context_management` ถูก strip (CRITICAL)

**ไฟล์**: `handler/handler.go` ~line 958

**ปัญหา**: `isNativeAnthropic` check ใช้ `decision.AuthMode == "bearer"` แต่ claude-oauth provider config ใน `provider/resolver.go` มี `AuthMode = "api_key"` (ไม่ใช่ "bearer") -> เงื่อนไขเป็น false เสมอ -> `context_management` ถูก strip ทุกรอบ

```go
// เก่า (พัง):
isNativeAnthropic := decision != nil && decision.AuthMode == "bearer" && decision.Format == provider.FormatAnthropic

// ใหม่ (ถูก):
isNativeAnthropic := decision != nil && decision.Format == provider.FormatAnthropic && decision.ProviderID != "zai"
```

**ผลกระทบถ้า strip**: Claude CLI ใช้ `context_management` เพื่อ clear thinking จาก previous turns ถ้าถูก strip:
- Thinking จาก turn ก่อนๆ ไม่ถูก clear
- Request body ใหญ่ขึ้นมาก (เพิ่มหลาย KB ต่อ turn)
- ยิ่งยาวยิ่งพัง

**ตรวจสอบ**: log ต้องมี `has_context_mgmt: true`

---

### 2. Privacy masking ทำลาย cache_control markers (CRITICAL)

**ไฟล์**: `handler/handler.go` ~line 1275, `privacy/pipeline.go`, `privacy/extractors/anthropic.go`

**ปัญหา**: Privacy masking แทนที่ secrets ด้วย `[[ENV_USER_N]]` placeholders ใน blocks ที่มี `cache_control: {"type": "ephemeral"}` -> เนื้อหาใน block เปลี่ยน -> Anthropic หา cached content ไม่เจอ -> cache miss ทุกรอบ -> input tokens เต็ม -> 429

**แก้**: Cache-aware masking - skip blocks ที่มี `cache_control`

```go
// handler.go
maskOpts := privacy.MaskOptions{SkipCachedBlocks: isClaudeCLIReq}
maskResult, _ = h.privacy.MaskRequestWithOptions(body, maskOpts)
```

**วิธีทำงาน**:
1. `TextSpan` struct มี field `HasCacheControl bool` (`privacy/masking/types.go`)
2. Extractor (`privacy/extractors/anthropic.go`) check `block["cache_control"]` บนทุก block
3. `MaskRequestWithOptions` skip spans ที่ `HasCacheControl == true`
4. Block ที่ไม่มี cache_control ยัง mask ปกติ

**ตาราง mask behavior**:

| Content | `cache_control`? | Claude CLI | Non-Claude CLI |
|---------|-----------------|------------|----------------|
| System prompt (cached) | Yes | **Skip** | Mask |
| System prompt (no cache) | No | Mask | Mask |
| User/assistant messages | No | Mask | Mask |
| Tool results (cached) | Yes | **Skip** | Mask |
| Tool results (no cache) | No | Mask | Mask |
| Tool use input | No | Mask | Mask |

---

### 3. System prompt optimizer แก้ไข cached blocks

**ไฟล์**: `handler/handler.go` ~line 1169

**ปัญหา**: System prompt optimizer (caveman, dedup, chunker, textcomp) แก้ไข text ใน blocks ที่มี `cache_control` -> cache miss

**แก้**: Optimizer มี guard `if _, hasCC := elem["cache_control"]; hasCC { continue }` อยู่แล้วที่ line ~1176 - ข้าม block ที่มี cache_control โดยอัตโนมัติ

**สำคัญ**: Guard นี้ทำงานเฉพาะ **array format** system prompt (`[{type: "text", text: "...", cache_control: ...}]`) ถ้า system prompt เป็น **string format** (`"system": "plain text"`) จะไม่มี cache_control check เพราะ string format ไม่มี cache_control markers

---

### 4. 429 passthrough ไม่มี Retry-After

**ไฟล์**: `proxy/anthropic.go` ~line 1329

**ปัญหา**: เวลา Anthropic return 429, gateway ส่งต่อให้ client แต่ไม่มี `Retry-After` header -> Claude CLI ไม่รู้จะ retry เมื่อไหร่

**แก้**: เพิ่ม default `Retry-After: 5` seconds

```go
w.Header().Set("X-Should-Retry", "true")
retrySeconds := int(retryAfterOverride.Seconds())
if retrySeconds <= 0 {
    retrySeconds = 5
}
w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
w.WriteHeader(429)
```

---

### 5. Missing closing brace ใน pool refresh block

**ไฟล์**: `handler/handler.go` ~line 676-687

**ปัญหา**: `if err := h.refreshWorker.RefreshOne(...)` block ขาด closing brace -> depth เพิ่มขึ้นเรื่อยๆ -> compile error หรือ runtime weirdness

**แก้**: เพิ่ม tab5 `}` สำหรับ `if err := RefreshOne` block

---

## โครงสร้าง Request Pipeline สำหรับ Claude CLI

```
Request เข้า (user-agent: claude-cli/*)
  |
  v
[1] isNativeAnthropic check (Format + ProviderID)
  |   -> context_management ถูก PRESERVED
  v
[2] stripUnsupportedFields (isNativeAnthropic=true)
  |   -> ไม่ strip context_management, service_tier
  v
[3] Thinking budget clamp (ถ้า profile กำหนด maxThinkingTokens)
  v
[4] System prompt optimizer (array format only)
  |   -> ข้าม blocks ที่มี cache_control
  |   -> ข้าม blocks ที่สั้นกว่า 500 chars
  v
[5] Message optimizer (whitespace/dedup/TextComp)
  |   -> ลด token count โดยไม่เปลี่ยน meaning
  v
[6] Tool filter
  |   -> ลบ tools ที่ไม่จำเป็น
  v
[7] Cache breakpoint guard (ทุก 18 blocks)
  |   -> inject cache_control สำหรับ non-Z.AI providers
  v
[8] Privacy masking (cache-aware)
  |   -> mask secrets/PII บน blocks ที่ไม่มี cache_control
  |   -> skip blocks ที่มี cache_control (preserve cache)
  v
[9] Proxy to Anthropic API (sidecar: ?beta=true)
  |
  v
Response (200 OK with cache hits)
```

---

## ตรวจสอบว่าทุกอย่างทำงานถูกต้อง

### Log markers ที่ต้องเห็น

```bash
# context_management preserved
grep "has_context_mgmt" <gateway-logs>
# ต้องเห็น: has_context_mgmt: true

# Privacy masking cache-aware
grep "privacy mask" <gateway-logs>
# Claude CLI: ต้องเห็น "privacy mask applied" แต่ secrets_count < total spans
# (cached blocks skipped)

# Cache hits
grep "cache_read" <gateway-logs>
# ต้องเห็น cache_read_input_tokens > 0 (cache hit!)

# Request success
grep "claude-cli" <gateway-logs> | grep "status"
# ต้องเห็น status=200
```

### curl test

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -X POST http://localhost:9000/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: <your-api-key>" \
  -H "anthropic-version: 2023-06-01" \
  -H "anthropic-beta: context-management-2025-06-27" \
  -H "User-Agent: claude-cli/2.1.143 (external, cli)" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 100,
    "stream": true,
    "system": [{"type":"text","text":"You are a helpful assistant.","cache_control":{"type":"ephemeral"}}],
    "messages": [{"role":"user","content":"say hi"}]
  }'
# Expected: 200
```

---

## สิ่งที่ต้องระวัง

| ห้ามทำ | เหตุผล |
|---------|---------|
| เปลี่ยน `isNativeAnthropic` ให้ check `AuthMode` | claude-oauth ใช้ `api_key` mode, ไม่ใช่ `bearer` |
| Mask secrets ใน blocks ที่มี `cache_control` | cache miss -> input tokens เต็ม -> 429 |
| Strip `context_management` field | thinking ไม่ถูก clear -> body ใหญ่เกิน -> 429 |
| ลบ `cache_control` check ใน optimizer array loop | cached blocks จะถูกแก้ -> cache miss |
| ลบ `Retry-After` header สำหรับ 429 | Claude CLI จะไม่ retry |

---

## Optimizer Pipeline Status สำหรับ Claude CLI

| Stage | Claude CLI | เหตุผล |
|-------|-----------|---------|
| Cache Metrics | RUN | metrics only, ไม่แก้ content |
| Thinking Budget Clamp | RUN | guard only, default off (0=unlimited) |
| Cache Breakpoint Guard | RUN | inject cache_control, ช่วย cache hit rate |
| System Prompt Optimizer | RUN | ข้าม cached blocks อัตโนมัติ |
| Message Optimizer | RUN | whitespace/dedup/TextComp, ลด tokens |
| Tool Filter | RUN | ลบ tools ไม่จำเป็น |
| Privacy Masking | Cache-aware | mask non-cached blocks, skip cached |
| 429 Retry-After | RUN | default 5s |
