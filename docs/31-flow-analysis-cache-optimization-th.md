# Flow Analysis & Cache Optimization Report

- วันที่วิเคราะห์: 2026-05-21
- แหล่งข้อมูล: `flows/claude flows/` (91 mitmproxy captures, flow 003-093)
- เครื่องมือ: 11 specialized agents (9 flow analyzers, 1 source code analyzer, 1 web researcher)

---

## สารบัญ

1. [ภาพรวม Session](#1-ภาพรวม-session)
2. [ปัญหาที่พบ](#2-ปัญหาที่พบ)
3. [Cache Miss Analysis](#3-cache-miss-analysis)
4. [Gap Analysis: เทียบกับ Anthropic Best Practices](#4-gap-analysis)
5. [แผนแก้ไขปัญหา](#5-แผนแก้ไขปัญหา)
6. [การประเมิน Cost Impact](#6-การประเมิน-cost-impact)

---

## 1. ภาพรวม Session

### 1.1 ข้อมูลทั่วไป

| รายการ | ค่า |
|--------|-----|
| จำนวน requests | 91 (flow 003-093) |
| Model | `claude-opus-4-7` |
| Effort | `xhigh` |
| Thinking | `adaptive` |
| Max tokens | 128,000 |
| ระยะเวลา session | ~21 นาที (12:18:49 - 12:39:22 UTC) |
| Content growth | 60KB -> 950KB |
| Input token growth | ~294K -> ~370K tokens |
| สถานะสุดท้าย | 429 Rate Limit (5h utilization: 104%) |
| Client | claude-cli/2.1.145 (claude-vscode, agent-sdk/0.3.145) |

### 1.2 Beta Features ที่ใช้งาน

```
anthropic-beta: claude-code-20250219,context-1m-2025-08-07,
  interleaved-thinking-2025-05-14,context-management-2025-06-27,
  prompt-caching-scope-2026-01-05,effort-2025-11-24,oauth-2025-04-20
```

### 1.3 Cache Breakpoints (4 จุดต่อ request)

| # | ตำแหน่ง | เนื้อหา | มี cache_control? |
|---|---------|---------|------------------|
| 1 | system[1] | "You are Claude Code..." (~94 chars) | YES |
| 2 | system[2] | Main system prompt (~30K chars) | YES |
| 3 | messages (mid-conversation) | Conversation prefix boundary | YES |
| 4 | messages (last user msg) | Latest tool_result | YES |
| - | system[0] | Billing header (cch=...) | NO (rotates) |
| - | system[3] | Reference tokens notice (~635 chars) | NO (static) |
| - | tools array | 15 tool definitions (~6-9K tokens) | **NO (static)** |

### 1.4 Cache Hit Rate ตามช่วง

| ช่วง (Flows) | Hit Rate ปกติ | Cache Miss Events |
|--------------|---------------|-------------------|
| 003-012 | 99.7% | Flow 008: breakpoint shift (277K creation) |
| 013-022 | 99.8% | ไม่มี |
| 023-032 | 99.2% | Flow 024: billing header shift |
| 033-042 | 99.7% | Flow 035: context compaction (306K creation) |
| 043-052 | 99.88% | ไม่มี |
| 053-062 | 99.5% | Flow 062: non-deterministic masking (325K creation) |
| 063-072 | 99.8% | ไม่มี |
| 073-084 | 99.7% | Flow 082-083: prefix shift (345K x2) |
| 085-093 | 99.7% | Flow 092: cache invalidation (350K) -> 429 |

---

## 2. ปัญหาที่พบ

### ปัญหาที่ 1: Tools Block ไม่ถูก Cache (CRITICAL)

**รายละเอียด:**
- Tools array มี 15 tool definitions (~6,000-9,000 tokens)
- เป็น static content ที่ไม่เปลี่ยนตลอด session
- แต่ไม่มี `cache_control` marker -> ถูก bill ที่ full input price ทุก request
- เสีย ~$0.12/request เกินควร (บน Opus 4.7)

**ผลกระทบ:**
- ตลอด 91 requests เสียเพิ่ม ~$10+ จากส่วนนี้อย่างเดียว
- ถ้า cache ได้ จะจ่ายแค่ $0.50/MTok (cache read) แทน $5.00/MTok (full input)

**สาเหตุ:**
- Claude Code client ไม่ได้ใส่ `cache_control` ที่ tools array
- Gateway (`handler.go`) มี `clampCacheControlBlocks()` และ `injectCacheBreakpoints()` แต่ไม่ได้ proactively add cache markers ให้ requests ที่ขาด

**ไฟล์ที่เกี่ยวข้อง:**
- `api-gateway/proxy/handler.go` - `clampCacheControlBlocks()`, `injectCacheBreakpoints()`

---

### ปัญหาที่ 2: Non-deterministic Data Masking ทำลาย Cache (CRITICAL BUG)

**รายละเอียด:**
- Gateway มี privacy masking pipeline ที่ mask PII/ข้อมูลสำคัญ
- Placeholder IDs เป็น **random** ต่อ request (ไม่ deterministic)
- ตัวอย่างจาก flow captures:

```
Flow 053-061: [[PHONE_NUMBER_8]][[PHONE_NUMBER_9]]
Flow 062:     [[PHONE_NUMBER_5]][[PHONE_NUMBER_4]]  <- เปลี่ยน!
```

- Anthropic prompt caching ใช้ prefix matching -> ถ้า content เปลี่ยนแม้แค่ 1 byte ก่อน breakpoint = cache invalid ทั้งก้อน
- ใน flow 062, masking เปลี่ยนที่ตำแหน่ง 12,450 (1.4% ของ messages) ทำให้ cache invalid 98.6%

**ผลกระทบ:**
- Flow 062: สร้าง 325K creation tokens แทนที่จะอ่านจาก cache
- Cost: $5.47 เพิ่มจาก flow เดียว (ปกติ $0.67)
- 8.2x แพงกว่าปกติ
- ปัญหานี้เกิดขึ้นแบบสุ่ม ไม่สามารถคาดเดาได้

**สาเหตุ:**
- Privacy pipeline ใช้ random placeholder ID generator
- ไม่มี mechanism ในการ seed ด้วย hash ของ original value

**ไฟล์ที่เกี่ยวข้อง:**
- `api-gateway/privacy/` - masking pipeline

---

### ปัญหาที่ 3: Billing Header (system[0]) หมุนเปลี่ยนทุก Request

**รายละเอียด:**
- `system[0]` เป็น billing header ที่มี `cch=XXXXX` hash
- Hash เปลี่ยนทุก request
- อยู่ก่อน cache breakpoint แรก (BP1 ที่ system[1])
- ทำให้ BP1 cache entry ถูกสร้างใหม่ทุก request แต่ไม่เคยถูกอ่านซ้ำ

**ผลกระทบ:**
- BP1 cache เป็น "dead cache" - สร้างทุก turn แต่ไม่เคย hit
- Wasted cache_creation: ~24 tokens/request (น้อย แต่เป็น structural waste)
- ทำให้การใช้ cache breakpoint ที่ system[1] ไร้ประโยชน์

**สาเหตุ:**
- Anthropic SDK อัปเดต billing hash ทุก request
- Block ordering: billing header มาก่อน cached system blocks

---

### ปัญหาที่ 4: Context Management ไม่ Trigger จริง

**รายละเอียด:**
- `context_management` config มี `clear_thinking_20251015` ตั้งไว้
- แต่ `applied_edits: []` ในทุก response delta = ไม่เคยมีการ trim/compact อะไรเลย
- Context โตจาก 294K เป็น 370K tokens โดยไม่มีการสรุป/ตัดทอน

**ผลกระทบ:**
- Context โตเกินไปจน context compaction ถูกบังคับ trigger -> massive cache miss
- เมื่อ compaction trigger (flow 035, 082): ทำให้ prefix เปลี่ยน -> cache invalid ทั้งก้อน
- Thinking signatures สะสมจนกิน 14.3% ของ input (~49K tokens ที่ turn 300+)

**สาเหตุ:**
- `clear_thinking` config ตั้ง `keep: "all"` -> ไม่มีการ clear เลย
- ไม่ได้ใช้ compaction beta (`compact-2026-01-12`) ที่ทำงานได้ดีกว่า
- ไม่ได้ใช้ `clear_tool_uses_20250919` สำหรับ auto-clear old tool results

---

### ปัญหาที่ 5: Optimizer Pipeline ถูก Bypass สำหรับ Z.AI (PRIMARY PROVIDER)

**รายละเอียด:**
- Gateway มี optimizer pipeline 13+ token optimizers
- แต่ `optimizerAllowed()` return false สำหรับ Z.AI requests
- Z.AI เป็น primary provider -> ไม่ได้รับ optimization ใดๆ

**ผลกระทบ:**
- Text compression (textcomp) ที่เป็น lossless ก็ถูก skip
- ข้อมูลที่ส่งไป Z.AI ใหญ่กว่าที่ควรเป็น

**ไฟล์ที่เกี่ยวข้อง:**
- `api-gateway/proxy/handler.go` - `optimizerAllowed()`

---

### ปัญหาที่ 6: Rate Limit ไม่ถูก Monitor ที่ฝั่ง Gateway

**รายละเอียด:**
- Anthropic ส่ง `anthropic-ratelimit-unified-5h-utilization` header ทุก response
- Gateway ไม่ได้ track/predict rate limit consumption
- Session นี้มี 5h utilization ที่ 89-90% ตั้งแต่ flow 085 แล้ว แต่ไม่มี warning

**ผลกระทบ:**
- Flow 092: cache miss ทำให้ใช้ budget 9.5x ปกติ
- Flow 093: 429 rate limit, retry-after: 16,237 วินาที (~4.5 ชั่วโมง)
- ถ้ามี monitoring จะหยุดได้ตั้งแต่ flow 085

---

### ปัญหาที่ 7: Tool Cache เป็น In-Memory Only

**รายละเอียด:**
- `ToolCache` ใน `toolcache.go` ใช้ `sync.RWMutex` + in-memory map
- TTL: 1 ชั่วโมง
- ไม่มี Redis persistence

**ผลกระทบ:**
- Gateway restart = tool cache หายทั้งหมด
- Multi-replica deployment = ไม่ share cache ข้าม instances
- Cold start = ต้องส่ง full tool definitions ใหม่ทุกครั้ง

**ไฟล์ที่เกี่ยวข้อง:**
- `api-gateway/proxy/toolcache.go`

---

### ปัญหาที่ 8: Claude Sessions ไม่มี Redis Persistence

**รายละเอียด:**
- `ClaudeSessionManager` ใช้ `sync.Map` (in-memory)
- Bootstrap เรียก 4 Anthropic OAuth endpoints
- ไม่มี session TTL หรือ periodic refresh

**ผลกระทบ:**
- Gateway restart = sessions หายทั้งหมด
- ต้อง re-bootstrap ทุก session (4 API calls/session)
- Multi-replica = sessions ไม่ share

**ไฟล์ที่เกี่ยวข้อง:**
- `api-gateway/proxy/claude-session.go`

---

### ปัญหาที่ 9: ใช้แค่ ephemeral_5m TTL (ไม่ใช้ ephemeral_1h)

**รายละเอียด:**
- Cache ทั้งหมดใช้ default 5-minute TTL
- `ephemeral_1h_input_tokens` = 0 ในทุก flow
- Static content (system prompt, tools) เหมาะกับ 1h TTL มากกว่า

**ผลกระทบ:**
- ถ้า user หยุดพัก >5 นาที = cache หายทั้งหมด
- ต้อง re-create ~370K tokens ที่ full price
- 1h TTL มีราคา cache write 2x แต่อยู่ได้นานกว่า 12x

---

### ปัญหาที่ 10: ไม่มี Cache Efficiency Metrics

**รายละเอียด:**
- Cache usage ถูก track จาก SSE events
- แต่ไม่ได้ expose เป็น Prometheus metrics
- ไม่มี visibility ว่า cache hit rate เท่าไหร่ใน production

**ผลกระทบ:**
- ไม่สามารถ monitor optimization impact ได้
- ไม่สามารถ debug cache issues ใน production ได้

---

## 3. Cache Miss Analysis

### 3.1 ภาพรวม Cache Miss Events

เกิด 5 catastrophic cache miss events ใน 91 flows:

| Flow | สาเหตุ | Creation Tokens | Cache Read | Hit Rate | Cost เพิ่ม | 5h Util Jump |
|------|--------|----------------|------------|----------|------------|--------------|
| 008 | Breakpoint shift (465->485) | 277,144 | 19,861 | 6.7% | ~$4.78 | - |
| 035 | Context compaction event | 306,470 | 19,861 | 6.1% | ~$5.82 | - |
| 062 | Non-deterministic masking | 324,947 | 19,861 | 5.8% | ~$5.47 | - |
| 082 | Prefix shift (large output) | 345,332 | 19,861 | 5.4% | ~$6.14 | 0.62->0.89 |
| 083 | Cache warming race (duplicate) | 345,332 | 19,861 | 5.4% | ~$6.14 | 0.89 |
| 092 | Cache invalidation cascade | 349,920 | 19,861 | 5.4% | ~$6.56 | 0.90->1.04 |

**Total waste: ~$35+ จาก cache misses เพียง 6 flows**

### 3.2 Root Cause Chain ของ 429 (Flow 093)

```
Long session (91 turns)
  -> Context โตถึง ~370K tokens
    -> Context management ไม่ trigger compaction
      -> Cache breakpoint shift ที่ flow 082-083
        -> 345K tokens re-created ที่ full price x2
          -> 5h utilization กระโดดจาก 0.62 เป็น 0.89
            -> อยู่ใน red zone แล้ว แต่ไม่มี warning
              -> Flow 092: cache invalidation อีกครั้ง (350K creation)
                -> 5h utilization เกิน 1.00 (1.04)
                  -> Flow 093: 429 REJECTED
                    -> retry-after: 16,237 วินาที (~4.5 ชม.)
```

**ถ้าป้องกันได้:**
- ถ้าหยุดตอง flow 085 (5h util = 89%) = ประหยัดได้
- ถ้า context compaction trigger ตอง 250K tokens = ลด per-turn cost
- ถ้า masking เป็น deterministic = ลด cache miss events
- ถ้า tools ถูก cache = ลด per-turn token budget

---

## 4. Gap Analysis: เทียบกับ Anthropic Best Practices

### 4.1 Prompt Caching Best Practices

| Best Practice | สถานะปัจจุบัน | Gap |
|---------------|---------------|-----|
| Cache breakpoint ที่ last tool | **ยังไม่ทำ** | Tools ไม่ถูก cache (~8K tokens/req) |
| Cache breakpoint ที่ last system block | ทำบางส่วน | system[3] ไม่ถูก cache |
| Max 4 breakpoints | ทำถูกต้อง | 4 breakpoints ตาม spec |
| Stable prefix ordering (tools->system->messages) | **ละเมิด** | Billing header หมุนก่อน breakpoints |
| Deterministic content สำหรับ caching | **ละเมิด** | Non-deterministic masking |
| ephemeral_1h สำหรับ static content | **ไม่ใช้** | ใช้แค่ ephemeral_5m |
| Cache pre-warming (max_tokens: 0) | **ไม่ใช้** | Cold starts เสีย tokens |

### 4.2 Context Management Best Practices

| Best Practice | สถานะปัจจุบัน | Gap |
|---------------|---------------|-----|
| Server-side compaction (`compact-2026-01-12`) | **ไม่ใช้** | ใช้แค่ context-management เดิม |
| Auto-clear tool results (`clear_tool_uses_20250919`) | **ไม่ใช้** | Old tool results สะสมไม่หยุด |
| Thinking clearing (keep < all) | **ตั้ง keep: "all"** | ไม่มีการ clear เลย |
| Trigger compaction ที่ ~250K tokens | **ไม่ trigger** | รอจน context เกินขีดจำกัด |

### 4.3 Token Optimization Best Practices

| Best Practice | สถานะปัจจุบัน | Gap |
|---------------|---------------|-----|
| Tool Search Tool (`defer_loading`) | **ไม่ใช้** | ส่ง tools ทั้งหมดทุก request |
| Programmatic Tool Calling (`allowed_callers`) | **ไม่ใช้** | - |
| Tool Use Examples (`input_examples`) | **ไม่ใช้** | - |
| Text compression (textcomp) | **Skip สำหรับ Z.AI** | Primary provider ไม่ได้รับ |
| Model routing (Sonnet สำหรับ simple ops) | **ไม่ใช้** | Opus xhigh สำหรับทุกอย่าง |

### 4.4 Rate Limiting Best Practices

| Best Practice | สถานะปัจจุบัน | Gap |
|---------------|---------------|-----|
| Monitor 5h utilization header | **ไม่ทำ** | ไม่มี warning/prediction |
| Backoff เมื่อ remaining < 20% | **ไม่ทำ** | ไม่มี proactive throttling |
| Start new session เมื่อ utilization สูง | **ไม่ทำ** | - |
| Cache reads ไม่นับ ITPM | รู้แล้ว (passive) | ไม่ได้ใช้ประโยชน์ |

---

## 5. แผนแก้ไขปัญหา

### P0 - Immediate (สัปดาห์นี้)

#### Fix 1: เพิ่ม cache_control ที่ Tools Array

**ปัญหา:** Tools definitions (~6-9K tokens) ไม่ถูก cache, เสีย full price ทุก request

**แนวทางแก้ไข:**
- แก้ไข `handler.go` ก่อนเรียก `clampCacheControlBlocks()`
- Inject `cache_control: {type: "ephemeral"}` ที่ tool definition สุดท้าย

```go
// File: api-gateway/proxy/handler.go
// ตำแหน่ง: ก่อน clampCacheControlBlocks()

if tools, ok := payload["tools"].([]interface{}); ok && len(tools) > 0 {
    lastTool, ok := tools[len(tools)-1].(map[string]interface{})
    if ok {
        if _, exists := lastTool["cache_control"]; !exists {
            lastTool["cache_control"] = map[string]string{"type": "ephemeral"}
        }
    }
}
```

**ข้อควรระวัง:**
- ต้องเช็คก่อนว่า tool definition มี cache_control อยู่แล้วหรือยัง
- ต้องทำก่อน `clampCacheControlBlocks()` เพราะ function นั้นจะ enforce max 4 breakpoints
- ถ้า request มี cache markers ครบ 4 แล้ว อาจต้องย้าย breakpoint จากที่อื่น

**Testing:**
- ส่ง request ผ่าน gateway แล้วตรวจสอบ request body ว่า tools array มี cache_control
- เปรียบเทียบ cache_creation_input_tokens ก่อนและหลัง fix

**Impact:**
- Savings: ~$0.12/request บน Opus, ~$6+/50-request session
- Risk: ต่ำ (เพิ่ม cache marker เท่านั้น ไม่เปลี่ยนเนื้อหา)

---

#### Fix 2: ทำให้ Data Masking เป็น Deterministic

**ปัญหา:** Privacy masking ใช้ random placeholder IDs -> cache invalidation

**แนวทางแก้ไข:**
- เปลี่ยนจาก random ID เป็น deterministic hash ของ original value
- Same input -> same mask -> same cache prefix

```go
// แนวทาง: hash original value เพื่อให้ได้ placeholder เดิมเสมอ
func deterministicPlaceholder(originalValue string, placeholderType string) string {
    h := fnv.New32a()
    h.Write([]byte(originalValue))
    hash := h.Sum32()
    return fmt.Sprintf("[[%s_%d]]", placeholderType, hash)
}
```

**ข้อควรระวัง:**
- Deterministic hash อาจเปิดช่องให้ brute-force original value ได้ (ถ้า value สั้น)
- พิจารณาใช้ HMAC กับ secret key แทน plain hash สำหรับข้อมูลที่ sensitive มาก
- Placeholder type ต้อง consistent กับ masking rules

**Testing:**
- ส่ง request เดียวกัน 2 ครั้งผ่าน gateway
- ตรวจสอบว่า placeholder IDs เหมือนกันทั้ง 2 ครั้ง
- ตรวจสอบว่า cache hit rate เพิ่มขึ้น

**Impact:**
- Savings: ป้องกัน random cache miss (~$5-6/ticket)
- Risk: ปานกลาง (เปลี่ยน masking behavior)

---

#### Fix 3: เปิด textcomp Optimizer สำหรับ Z.AI

**ปัญหา:** Optimizer pipeline ถูก skip สำหรับ Z.AI (primary provider)

**แนวทางแก้ไข:**
- แก้ `optimizerAllowed()` ให้ allow textcomp เสมอ
- textcomp เป็น lossless compression ที่ปลอดภัย

```go
// File: api-gateway/proxy/handler.go
// Function: optimizerAllowed()

// เพิ่ม: textcomp ทำงานได้กับทุก provider
if optimizerName == "textcomp" {
    return true
}
```

**Testing:**
- ส่ง request ไป Z.AI ผ่าน gateway
- เปรียบเทียบ token count ก่อนและหลังเปิด textcomp
- ตรวจสอบว่า response quality ไม่เปลี่ยน

**Impact:**
- Savings: ลด token count สำหรับ primary provider
- Risk: ต่ำมาก (lossless compression)

---

### P1 - Short-term (Sprint หน้า)

#### Fix 4: Rate Limit Monitoring & Proactive Throttling

**ปัญหา:** ไม่มีการ monitor 5h utilization -> โดน 429 โดยไม่คาดคิด

**แนวทางแก้ไข:**

1. **Parse response headers** จาก Anthropic:
```go
// อ่านจาก response headers
utilization := resp.Header.Get("anthropic-ratelimit-unified-5h-utilization")
status := resp.Header.Get("anthropic-ratelimit-unified-5h-status")
resetTs := resp.Header.Get("anthropic-ratelimit-unified-5h-reset")
```

2. **Threshold actions:**
   - utilization > 0.70: Log warning
   - utilization > 0.80: Add `X-RateLimit-Warning` header to client response
   - utilization > 0.90: Return 429 to client with message "Rate limit approaching. Consider starting a new session."
   - status == "allowed_warning": Force context compaction

3. **Expose Prometheus metrics:**
```go
ratelimit5hUtilization := prometheus.NewGaugeVec(
    prometheus.WithLabelValues("model", "profile"),
)
```

**Testing:**
- Simulate high utilization scenarios
- ตรวจสอบ warning headers และ Prometheus metrics

**Impact:**
- ป้องกัน 429 rate limit ที่ทำให้ session ต้องหยุด 4.5 ชม.
- Risk: ต่ำ (read-only monitoring + warning)

---

#### Fix 5: ย้าย Billing Header มาหลัง Cache Breakpoints

**ปัญหา:** system[0] billing header เปลี่ยนทุก request ทำให้ BP1 เป็น dead cache

**แนวทางแก้ไข:**

**Option A: Reorder system blocks**
- ย้าย billing header ไป system[3] (ท้ายสุด, ไม่มี cache_control)
- ผลลัพธ์: system[0] = Claude identity, system[1] = main prompt, system[2] = reference tokens, system[3] = billing header

**Option B: Move to HTTP header**
- ย้าย billing header ออกจาก system prompt เป็น custom HTTP header
- ผลลัพธ์: system prompt ทั้งหมดเป็น static content

**แนะนำ Option B** เพราะ:
- ไม่กระทบ cache structure
- แยก billing metadata ออกจาก model input
- แต่ต้องเช็คว่า downstream systems อ่าน billing header จากไหน

**Testing:**
- ตรวจสอบว่า billing header ยังถูก process ถูกต้อง
- เปรียบเทียบ cache hit rate ก่อนและหลัง

**Impact:**
- Savings: เล็กน้อย (~24 tokens/request) แต่ปรับโครงสร้างให้ถูกต้อง
- Risk: ปานกลาง (ต้องเช็ค downstream dependencies)

---

#### Fix 6: เพิ่ม cache_control ที่ system[3]

**ปัญหา:** system[3] (reference tokens notice, ~635 chars) เป็น static แต่ไม่มี cache_control

**แนวทางแก้ไข:**
- เพิ่ม `cache_control: {type: "ephemeral"}` ที่ system[3]
- หรือ merge system[3] เข้ากับ system[2] (main system prompt) เป็น block เดียว

**Testing:**
- ตรวจสอบว่า cache_creation_input_tokens ลดลง ~158 tokens

**Impact:**
- Savings: ~158 tokens/request (เล็กน้อยแต่ฟรี)
- Risk: ต่ำมาก

---

### P2 - Medium-term (เดือนหน้า)

#### Fix 7: Persist Tool Cache ไป Redis

**ปัญหา:** Tool cache เป็น in-memory only -> หายเมื่อ restart

**แนวทางแก้ไข:**
```go
// ใช้ Redis ที่มีอยู่แล้ว (Dragonfly)
func (tc *ToolCache) persistToRedis(key string, tools []interface{}) error {
    data, _ := json.Marshal(tools)
    return tc.rdb.Set(ctx, "toolcache:"+key, data, 24*time.Hour).Err()
}

func (tc *ToolCache) loadFromRedis(key string) ([]interface{}, error) {
    data, err := tc.rdb.Get(ctx, "toolcache:"+key).Bytes()
    if err != nil { return nil, err }
    var tools []interface{}
    json.Unmarshal(data, &tools)
    return tools, nil
}
```

**Testing:**
- Restart gateway แล้วตรวจสอบว่า tool cache ยังอยู่
- Load test กับ multi-replica deployment

**Impact:**
- ป้องกัน tool cache loss ตอน restart
- Share cache ข้าม replicas
- Risk: ต่ำ

---

#### Fix 8: Cache Pre-warming

**ปัญหา:** Cold start ทุก session -> สร้าง cache ทั้งก้อนที่ full price

**แนวทางแก้ไข:**
- เมื่อ session เริ่มใหม่, ส่ง request แรกด้วย `max_tokens: 0`
- ทำให้ cache เขียนเสร็จโดยไม่เสีย output token cost
- Request จริงครั้งแรกจะได้ cache read ทันที

```go
func warmCache(payload map[string]interface{}) error {
    payload["max_tokens"] = 0
    // Send request, ignore response
    // Next real request will benefit from warm cache
}
```

**Testing:**
- เปรียบเทียบ first-request latency และ cost ก่อน/หลัง

**Impact:**
- Savings: ลด first-request cost (cache write ไม่มี output token)
- Risk: ต่ำ (เพิ่ม 1 request ตอน start session)

---

#### Fix 9: ประเมิน Tool Search Tool (defer_loading)

**ปัญหา:** ส่ง tool definitions ทั้ง 15 tools (~8K tokens) ทุก request

**แนวทางแก้ไข:**
- ใช้ `defer_loading: true` บน tool definitions
- Model จะค้นหา tools แบบ on-demand แทน load ทั้งหมด
- Anthropic อ้างว่าลด token 85%

```json
{
  "name": "Bash",
  "defer_loading": true,
  "description": "Execute bash commands",
  "input_schema": { ... }
}
```

**ข้อควรระวัง:**
- เป็น beta feature (`advanced-tool-use-2025-11-20`)
- ต้องทดสอบว่า model เลือก tools ถูกต้อง
- อาจเพิ่ม latency จาก tool discovery step

**Testing:**
- A/B test: เปรียบเทียบ accuracy และ token usage ระหว่าง full loading vs defer_loading

**Impact:**
- Savings: ลด tool tokens 85% (จาก ~8K เหลือ ~1.2K)
- Risk: ปานกลาง (beta feature, ต้อง verify accuracy)

---

#### Fix 10: ใช้ Server-side Compaction Beta

**ปัญหา:** Context management เดิมไม่ trigger compaction

**แนวทางแก้ไข:**
- ใช้ beta `compact-2026-01-12` แทน `context-management-2025-06-27`
- ตั้ง trigger ที่ ~250K tokens (แทนที่จะรอถึง context limit)
- Server-side summarization ทำงานน่าเชื่อถือกว่า

```json
{
  "context_management": {
    "compaction": {
      "trigger": 250000,
      "instructions": "Preserve all tool results with errors. Keep the last 10 completed tool results. Summarize older conversation into key decisions and outcomes."
    }
  }
}
```

**Testing:**
- ทดสอบ compaction trigger ที่ 250K tokens
- ตรวจสอบว่า cache hit rate หลัง compaction กลับมาเร็ว

**Impact:**
- ลด context size -> ลด per-turn cost
- ป้องกัน context limit errors
- Risk: ปานกลาง (beta, ต้อง tune trigger threshold)

---

### P3 - Long-term

#### Fix 11: ใช้ ephemeral_1h TTL สำหรับ Static Content

**แนวทางแก้ไข:**
- System prompt และ tools ใช้ `{"type": "ephemeral", "ttl": "1h"}`
- Cache write แพงกว่า 2x แต่อยู่ได้นานกว่า 12x
- เหมาะสำหรับ content ที่ไม่เปลี่ยนตลอด session

**Impact:**
- ทนต่อ pause นานขึ้น (>5 min) โดยไม่สูญเสีย cache
- Risk: ต่ำ

---

#### Fix 12: Model Routing Optimization

**แนวทางแก้ไข:**
- ไม่ทุก request ต้องใช้ Opus 4.7 + xhigh effort
- Simple operations (file read, git status, short responses) -> Sonnet 4.6
- Complex reasoning, code generation -> Opus 4.7
- Estimated savings: ~75% สำหรับ simple operations

**Implementation:**
- Gateway-side model routing ตาม request characteristics
- หรือ client-side intent classification ก่อนส่ง

**Impact:**
- Savings: ลด cost ~50-75% สำหรับ simple operations
- Risk: ปานกลาง (ต้อง verify quality ไม่ลดลง)

---

#### Fix 13: เพิ่ม Prometheus Metrics สำหรับ Cache Efficiency

**แนวทางแก้ไข:**
```go
var (
    cacheReadTokens = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{Name: "anthropic_cache_read_tokens"},
        []string{"model", "profile"},
    )
    cacheCreationTokens = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{Name: "anthropic_cache_creation_tokens"},
        []string{"model", "profile"},
    )
    cacheHitRate = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{Name: "anthropic_cache_hit_rate"},
        []string{"model", "profile"},
    )
    rateLimit5hUtilization = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{Name: "anthropic_ratelimit_5h_utilization"},
        []string{"model", "profile"},
    )
)
```

**Impact:**
- Visibility เข้าสู่ cache performance
- Alerting เมื่อ hit rate ต่ำผิดปกติ
- Risk: ต่ำมาก

---

## 6. การประเมิน Cost Impact

### 6.1 Session นี้ (91 requests, Opus 4.7)

| สถานการณ์ | Cost ประมาณการ | ลดลง |
|-----------|---------------|------|
| ปัจจุบัน (มี 5 cache misses) | ~$85 | - |
| Fix tools + masking + billing header (P0) | ~$45 | -47% |
| + Rate limit monitoring + system[3] cache (P1) | ~$38 | -55% |
| + Tool cache Redis + pre-warming + compaction (P2) | ~$25 | -71% |
| + Model routing (P3) | ~$18 | -79% |
| Theoretical optimal (ทุกอย่าง) | ~$15 | -82% |

### 6.2 Cost Breakdown ปัจจุบัน vs Optimal

| รายการ | ปัจจุบัน | Optimal | Savings |
|--------|---------|---------|---------|
| Cache creation (normal) | ~$2 | ~$2 | 0% |
| Cache creation (misses) | ~$35 | ~$0 | 100% |
| Cache read | ~$15 | ~$10 | 33% |
| Tool tokens (uncached) | ~$10 | ~$1 | 90% |
| Output tokens | ~$3 | ~$2 | 33% |
| **Total** | **~$85** | **~$15** | **82%** |

### 6.3 ROI ของแต่ละ Fix

| Fix | Effort | Savings/Session | ROI |
|-----|--------|----------------|-----|
| Fix 1: Tools cache_control | 1 ชม. | ~$10+ | สูงมาก |
| Fix 2: Deterministic masking | 2-3 ชม. | ~$5-6/cache miss event | สูง |
| Fix 3: textcomp for Z.AI | 30 นาที | ~5-15% tokens | สูง |
| Fix 4: Rate limit monitoring | 3-4 ชม. | ป้องกัน session ขาด 4.5 ชม. | สูงมาก |
| Fix 5: Billing header reorder | 2-3 ชม. | ~$2/session | ปานกลาง |
| Fix 10: Server-side compaction | 4-6 ชม. | ~30% token reduction | สูง |

---

## Appendix A: Flow-by-Flow Rate Limit Utilization

| Flow Range | 5h Utilization | Status |
|------------|---------------|--------|
| 003-004 | 0.00-0.01 | Cold start |
| 005-020 | 0.01-0.04 | Normal |
| 021-040 | 0.04-0.30 | Growing |
| 041-060 | 0.30-0.43 | Growing |
| 061-075 | 0.43-0.60 | Elevated |
| 076-081 | 0.60-0.62 | Caution |
| 082 | **0.62->0.89** | **Cache miss spike** |
| 083-091 | 0.89-0.90 | **Red zone** |
| 092 | **0.90->1.04** | **Cache miss + breach** |
| 093 | 1.04 | **429 REJECTED** |

## Appendix B: Anthropic API Cache Pricing Reference (Opus 4.7)

| Operation | 5-min TTL | 1-hr TTL |
|-----------|-----------|----------|
| Base input | $5.00/MTok | $5.00/MTok |
| Cache write | $6.25/MTok (1.25x) | $10.00/MTok (2x) |
| Cache read | $0.50/MTok (0.1x) | $0.50/MTok (0.1x) |
| Output | $15.00/MTok | $15.00/MTok |

**Key rule:** Cache reads ไม่นับ ITPM rate limits (ยกเว้น Haiku 3.5) -> cache miss ทำให้ rate limit มาเร็วขึ้นมาก
