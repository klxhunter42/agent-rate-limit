# Use Case: Code Review Session ผ่าน Optimizer Gateway

> นักเขียนโปรแกรมรุ่นใหญ่ "นภัส" ทำ Code Review ให้ Junior Developer "เป็ด" บน PR #42
> โมเดล: Claude Sonnet 4 | Session: `sess_abc123` | เวลาทั้งหมด: ~8 นาที

---

## ภาพรวม Session

| รายการ | ค่า |
|---|---|
| จำนวน Turn | 20 |
| Budget Level | Green (T1-15) → Yellow (T16-20) |
| Token ก่อน Optimize | 187,420 input + 31,200 output |
| Token หลัง Optimize | 108,630 input + 19,880 output |
| ประหยัดได้ | **78,790 input + 11,320 output = 90,110 tokens** |
| ความประหยัด | **48.1%** |
| ค่าใช้จ่ายจริง | $1.42 |
| ค่าใช้จ่ายถ้าไม่มี Optimizer | $2.73 |
| ประหยัดเงิน | **$1.31 (48.0%)** |

---

## Phase 1: ตรวจ PR เบื้องต้น (Turn 1-5)

### เรื่องราว

นภัสเปิด Claude Code แล้วพิมพ์:

```
review PR #42 ใน repo api-gateway ให้หน่อย ดูว่ามีปัญหาอะไรไหม
```

Claude ตอบกลับด้วย tool calls หลายตัว:

- `Bash`: `git diff main...feature/rate-limiter-v2` → ได้ diff ออกมา 3,847 บรรทัด
- `Bash`: `git log --oneline main...feature/rate-limiter-v2` → commit log 23 commits
- `Read`: `api-gateway/internal/handler/messages.go`
- `Read`: `api-gateway/internal/middleware/ratelimit.go`
- `Bash`: `ls -la api-gateway/internal/optimizer/`

### Pipeline ทำงาน

#### Turn 1: คำขอเริ่มต้น

| Stage | ก่อน (chars) | หลัง (chars) | ประหยัด | กลไก |
|---|---:|---:|---:|---|
| F7 Semantic Dedup | 4,210 | 4,120 | 90 | ลบ "You are a helpful coding assistant" ซ้ำ 2 จาก system prompt |
| F8 Delta Encoding | 4,120 | 2,480 | 1,640 | Session ใหม่ baseline เทียบกับ default prompt ต่างกันแคบ บรรทัดเดียว |
| F17 TextComp | 2,480 | 2,310 | 170 | ตัด "please note that", "it is important to", "you should always" |
| F16 Caveman | 2,310 | 229 | 2,081 | Lite tier (Green budget) |
| F19 ToolFilter | - | - | 4,200 | จาก 27 tools เหลือ 8 (Read, Bash, Grep, Edit, Glob, Write + 2 intent-matched) |
| **รวม System Prompt** | **4,210** | **229** | **3,981** | **94.6%** |

#### Turn 2-3: Tool results กลับเข้ามา (git diff + git log)

นี่คือจุดที่เนื้อหาเยอะที่สุด `git diff` คืน 3,847 บรรทัด

| Stage | ก่อน (chars) | หลัง (chars) | ประหยัด | กลไก |
|---|---:|---:|---:|---|
| F18 ToolComp (diff format) | 142,800 | 38,200 | 104,600 | Diff format: เก็บเฉพาะ `+`/`-` lines, ลบ context lines ที่ไม่เปลี่ยน |
| F18 ToolComp (shell ls) | 4,890 | 1,920 | 2,970 | Shell ls format: head+tail+summary, ตัด intermediate entries |
| F7 Semantic Dedup | 3,890 | 3,780 | 110 | ลบประโยคซ้ำใน system prompt |
| F17 TextComp | 3,780 | 3,510 | 270 | ตัด filler จาก system prompt |
| F16 Caveman | 3,510 | 229 | 3,281 | Lite tier |
| **รวม** | **155,280** | **44,639** | **110,641** | **71.3%** |

สิ่งที่ ToolComp ทำกับ git diff output:

```
# ก่อน (142,800 chars)
diff --git a/internal/handler/messages.go b/internal/handler/messages.go
index abc1234..def5678 100644
--- a/internal/handler/messages.go
+++ b/internal/handler/messages.go
@@ -42,8 +42,12 @@ func (h *Handler) HandleMessages() {
     ctx := r.Context()
     sessionID := r.URL.Query().Get("session")
-    // old code line 1
-    // old code line 2
+    // new code line 1
+    // new code line 2
+    // new code line 3
+    // new code line 4
     // unchanged context line
     // unchanged context line
     // unchanged context line

# หลัง (38,200 chars) - เก็บเฉพาะส่วนที่เปลี่ยน
diff a/internal/handler/messages.go
-    // old code line 1
-    // old code line 2
+    // new code line 1
+    // new code line 2
+    // new code line 3
+    // new code line 4
```

#### Turn 4-5: อ่านไฟล์ที่เปลี่ยน

นภัสเห็นว่ามีปัญหาที่ `ratelimit.go` ก็เลยให้ Claude อ่านไฟล์นั้นต่อ

| Stage | ก่อน (chars) | หลัง (chars) | ประหยัด | กลไก |
|---|---:|---:|---:|---|
| F8 Delta | 4,210 | 3,420 | 790 | System prompt เปลี่ยนแค่ "reviewing ratelimit.go" แทน "reviewing messages.go" |
| F7 Semantic Dedup | 3,420 | 3,380 | 40 | ลบ "You are a code reviewer" ซ้ำ |
| F17 TextComp | 3,380 | 3,150 | 230 | ตัด filler |
| F16 Caveman | 3,150 | 229 | 2,921 | Lite tier |
| F18 ToolComp (file content) | 28,400 | 22,100 | 6,300 | Tool result จาก Read: ตัด blank lines + trailing whitespace |
| **รวม** | **32,610** | **25,659** | **6,951** | **21.3%** |

### สรุป Phase 1

| Turn | Budget | Input Tokens (ก่อน) | Input Tokens (หลัง) | ประหยัด % | Stage หลัก |
|---|---|---:|---:|---:|---|
| T1 | Green | 12,840 | 3,210 | 75.0% | ToolFilter, Caveman, Delta |
| T2 | Green | 48,200 | 14,100 | 70.7% | ToolComp (diff) |
| T3 | Green | 42,100 | 12,400 | 70.5% | ToolComp (diff) |
| T4 | Green | 11,200 | 7,800 | 30.4% | Delta, ToolComp |
| T5 | Green | 9,890 | 6,920 | 30.0% | Delta, TextComp |
| **รวม Phase 1** | | **124,230** | **44,430** | **64.2%** | |

---

## Phase 2: Deep Dive ไฟล์เฉพาะ (Turn 6-10)

### เรื่องราว

นภัสเห็นว่า rate limiter มีปัญหาหลายจุด ก็เลยให้ Claude อ่านไฟล์เพิ่ม:

```
อ่านไฟล์ internal/middleware/ratelimit.go ตั้งแต่บรรทัด 120-280 ให้หน่อย
ดู middleware/redis.go ด้วย ว่า connection pool ตั้งค่าถูกไหม
อ่าน optimizer/pipeline.go ด้วย เพราะเห็นมี call crossing
```

Claude ทำงาน:
- `Read`: `internal/middleware/ratelimit.go` (lines 120-280)
- `Read`: `internal/middleware/redis.go`
- `Read`: `internal/optimizer/pipeline.go`

### Pipeline ทำงาน

#### Turn 6-7: Read ไฟล์เดิมซ้ำ (บางส่วน)

| Stage | ก่อน (chars) | หลัง (chars) | ประหยัด | กลไก |
|---|---:|---:|---:|---|
| F8 Delta | 4,210 | 2,530 | 1,680 | System prompt ต่างจาก baseline แค่ file path → 40% savings |
| F7 Semantic Dedup | 2,530 | 2,490 | 40 | "You are a code reviewer expert in Go" ซ้ำ |
| F17 TextComp | 2,490 | 2,310 | 180 | ตัด filler |
| F16 Caveman | 2,310 | 229 | 2,081 | Lite tier |
| **รวม System Prompt** | **4,210** | **229** | **3,981** | **94.6%** |

Delta Encoding เซฟ 40% ของ system prompt เพราะ:

```
# Baseline (cached ใน Redis)
sys:claude-sonnet-4 → "You are a code reviewer expert in Go, focusing on..."

# Turn 6 incoming
"You are a code reviewer expert in Go, focusing on ratelimit.go lines 120-280"

# Delta output (ส่งไปจริง)
= = = = = = = = = = = = = = = = = =
- focusing on
+ focusing on ratelimit.go lines 120-280
```

ต่างกันแค่ 1 operation → ประหยัด ~1,680 chars

#### Turn 8-10: Read หลายไฟล์

| Stage | ก่อน (chars) | หลัง (chars) | ประหยัด | กลไก |
|---|---:|---:|---:|---|
| F8 Delta | 4,210 | 2,610 | 1,600 | เปลี่ยน file path เท่านั้น |
| F7 Semantic Dedup | 2,610 | 2,570 | 40 | ลบ "You are a code reviewer" ซ้ำ |
| F17 TextComp | 2,570 | 2,380 | 190 | ตัด "it is worth noting that", "in order to" |
| F16 Caveman | 2,380 | 229 | 2,151 | Lite tier |
| F18 ToolComp | 31,200 | 24,800 | 6,400 | File contents: strip blank lines, trailing whitespace |
| **รวม** | **35,410** | **27,379** | **8,031** | **22.7%** |

### สรุป Phase 2

| Turn | Budget | Input Tokens (ก่อน) | Input Tokens (หลัง) | ประหยัด % | Stage หลัก |
|---|---|---:|---:|---:|---|
| T6 | Green | 8,420 | 4,910 | 41.7% | Delta (40% sys prompt) |
| T7 | Green | 9,100 | 5,230 | 42.5% | Delta, ToolComp |
| T8 | Green | 10,800 | 6,400 | 40.7% | Delta, ToolComp |
| T9 | Green | 8,900 | 5,200 | 41.6% | Delta, TextComp |
| T10 | Green | 7,600 | 4,800 | 36.8% | Delta |
| **รวม Phase 2** | | **44,820** | **26,540** | **40.8%** | |

---

## Phase 3: เขียน Review Comments (Turn 11-15)

### เรื่องราว

นภัสเริ่มเขียน review comments:

```
เขียน review comments ให้ PR นี้ แบบ inline ทุกจุดที่เห็นปัญหา
```

Claude ตอบกลับด้วย review ยาวๆ พร้อม code suggestions:

```
Here are the review comments for PR #42:

## Critical Issues

### 1. Race condition in rate limiter (ratelimit.go:142-158)
The current implementation uses a map without mutex protection...
```

### Pipeline ทำงาน

#### Turn 11: ขอ review comments

| Stage | ก่อน (chars) | หลัง (chars) | ประหยัด | กลไก |
|---|---:|---:|---:|---|
| F7 Semantic Dedup | 4,210 | 4,140 | 70 | ลบ "You are a code reviewer" ซ้ำ 2 |
| F8 Delta | 4,140 | 2,520 | 1,620 | System prompt เปลี่ยน "write review" แทน "read files" |
| F17 TextComp | 2,520 | 2,310 | 210 | ตัด filler |
| F13 Intent Filter | - | - | - | ข้อมูลขาเข้าเป็น "code" intent → pass through |
| F16 Caveman | 2,310 | 229 | 2,081 | Lite tier |
| **รวม System Prompt** | **4,210** | **229** | **3,981** | **94.6%** |

#### Turn 12-13: Claude สร้าง review comments (OUTPUT path)

นี่คือจุดที่ Intent Filter ทำงานบน output:

**Output ก่อนกรอง** (Claude สร้างมา):
```
Here are the review comments for PR #42 in the api-gateway repository:

## Critical Issues

### 1. Race condition in rate limiter (ratelimit.go:142-158)
The current implementation uses a map without mutex protection when checking
rate limits across concurrent requests. This could lead to data races when
multiple goroutines access the rate limit map simultaneously. I would recommend
adding a sync.RWMutex to protect the map accesses.

Here is the suggested fix:

```go
type RateLimiter struct {
    limits map[string]*LimitEntry
    mu     sync.RWMutex  // Add this
}

func (r *RateLimiter) Check(key string) bool {
    r.mu.RLock()
    defer r.mu.RUnlock()
    // existing logic
}
```

### 2. Memory leak in Redis pool (redis.go:45)
...
```

**Output หลัง Intent Filter** (code intent → ส่งเฉพาะ code suggestions):

```
Review comments for PR #42:

## Critical Issues

### 1. Race condition (ratelimit.go:142-158)

```go
type RateLimiter struct {
    limits map[string]*LimitEntry
    mu     sync.RWMutex  // Add this
}

func (r *RateLimiter) Check(key string) bool {
    r.mu.RLock()
    defer r.mu.RUnlock()
}
```

### 2. Memory leak (redis.go:45)

```go
// fix: set MaxIdleTime
pool := &redis.Pool{
    MaxIdleTime: 5 * time.Minute,
}
```
```

| Stage | ก่อน (chars) | หลัง (chars) | ประหยัด | กลไก |
|---|---:|---:|---:|---|
| F13 Intent Filter (output) | 4,820 | 2,890 | 1,930 | code intent → เก็บเฉพาะ code blocks + file paths + เหลือประโยคสั้นๆ |
| F16 Caveman | - | - | - | Lite tier คุม output style |
| **รวม Output** | **4,820** | **2,890** | **1,930** | **40.0%** |

#### Turn 14: Review comments ที่มี API key ใน code path

นภัสเห็น comment ที่มี internal path เช่น `/home/napas/projects/api-gateway/internal/secrets/keys.go`

| Stage | ก่อน (chars) | หลัง (chars) | ประหยัด | กลไก |
|---|---:|---:|---:|---|
| PasteGuard (mask) | 2,140 | 2,080 | 60 | Mask internal path ที่มี "secrets/keys" → `__PII_PATH__` |
| F17 TextComp | 2,080 | 1,920 | 160 | ตัด filler |
| F16 Caveman | 1,920 | 229 | 1,691 | Lite tier |
| **รวม** | **2,140** | **229** | **1,911** | **89.3%** |

#### Turn 15: อ่านไฟล์อีกรอบเพื่อ verify fix

| Stage | ก่อน (chars) | หลัง (chars) | ประหยัด | กลไก |
|---|---:|---:|---:|---|
| F8 Delta | 4,210 | 2,540 | 1,670 | Delta encode เหมือนเดิม |
| F7 Semantic Dedup | 2,540 | 2,500 | 40 | ลบซ้ำ |
| F17 TextComp | 2,500 | 2,320 | 180 | ตัด filler |
| F16 Caveman | 2,320 | 229 | 2,091 | Lite tier |
| **รวม** | **4,210** | **229** | **3,981** | **94.6%** |

### สรุป Phase 3

| Turn | Budget | Input Tokens (ก่อน) | Input Tokens (หลัง) | Output Tokens (ก่อน) | Output Tokens (หลัง) | Stage หลัก |
|---|---|---:|---:|---:|---:|---|
| T11 | Green | 6,800 | 2,100 | - | - | Caveman, Delta |
| T12 | Green | 5,200 | 1,400 | 2,400 | 1,440 | Intent Filter (output) |
| T13 | Green | 4,900 | 1,300 | 2,100 | 1,260 | Intent Filter (output) |
| T14 | Green | 5,100 | 1,200 | 1,800 | 1,080 | PasteGuard, Caveman |
| T15 | Green | 6,400 | 1,800 | - | - | Delta, Caveman |
| **รวม Phase 3** | | **28,400** | **7,800** | **6,300** | **3,780** | |

---

## Phase 4: สรุป Review + Final Summary (Turn 16-20)

### เรื่องราว

นภัสขอสรุป:

```
สรุป review ทั้งหมดให้หน่อย พร้อม priority order และ action items
```

Session ตอนนี้มี context usage ~65% → **Budget เปลี่ยนเป็น Yellow**

### Pipeline ทำงาน

#### Turn 16: Budget เปลี่ยนเป็น Yellow

Budget Level เปลี่ยนจาก Green → Yellow (context usage 65%)

| Stage | ก่อน (chars) | หลัง (chars) | ประหยัด | กลไก |
|---|---:|---:|---:|---|
| F8 Delta | 4,210 | 2,530 | 1,680 | Delta encode เหมือนเดิม |
| F7 Semantic Dedup | 2,530 | 2,490 | 40 | ลบซ้ำ |
| F17 TextComp | 2,490 | 2,310 | 180 | ตัด filler |
| F16 Caveman | 2,310 | 229 | 2,081 | **ยัง Lite tier** (Yellow = full tier แต่ auto-detect เลือก lite) |
| F15 Disclosure (Yellow) | 28,400 | 11,360 | 17,040 | **Truncate large tool_result** เกิน 2000 chars → เหลือ L2Tokens*8 |
| **รวม** | **32,610** | **11,899** | **20,711** | **63.5%** |

สิ่งที่ Disclosure ทำ: tool_result ที่เคยอ่านไฟล์ยาวๆ ถูก truncate:

```
# ก่อน (28,400 chars - full file content from T2)
... ไฟล์ ratelimit.go ทั้งไฟล์ 28,400 chars ...

# หลัง Disclosure Yellow (< L2Tokens*8 = 480 chars)
[...content truncated... first 480 chars preserved]
```

#### Turn 17-18: Sketch ตรวจจับ duplicate

นภัสขอรายละเอียดเพิ่มเติม แต่ prompt คล้าย Turn 8-10 มาก

| Stage | ก่อน (chars) | หลัง (chars) | ประหยัด | กลไก |
|---|---:|---:|---:|---|
| F9 Sketch | 4,210 | 4,210 | 4,210* | Near-duplicate detected (similarity 0.92 vs T10) |
| F16 Caveman | 4,210 | 229 | 3,981 | Lite tier |
| **รวม** | **4,210** | **229** | **3,981** | **94.6%** |

*Sketch flags duplicate เพื่อ diagnostic ไม่ได้ตัดทิ้ง แต่ข้อมูลนี้ไปให้ Waste Detection ใช้

#### Turn 19: Claude เขียน summary

| Stage | ก่อน (chars) | หลัง (chars) | ประหยัด | กลไก |
|---|---:|---:|---:|---|
| F8 Delta | 4,210 | 2,510 | 1,700 | Delta encode |
| F16 Caveman | 2,510 | 229 | 2,281 | Lite tier |
| F13 Intent Filter (output) | 3,200 | 1,920 | 1,280 | summary intent → เก็บ bullets + action items |
| **รวม** | **4,210** | **229** | **3,981** | **94.6% (input)** |

#### Turn 20: Post-proxy feedback (ข้อมูลไป optimizer เรียนรู้)

หลังจาก request ส่งไป provider แล้ว PostProxyFeedback ทำงาน:

| Stage | ผลลัพธ์ | รายละะเอียด |
|---|---|---|
| F4 Prefetcher | `Read → Bash → Read → Edit` pattern บันทึก | ทำนาย tool ถัดไปสำหรับ session นี้ |
| F11 Waste Detection | `redundant_tool_call` flag! | ตรวจพบว่า Read `ratelimit.go` ซ้ำ 3 ครั้ง (T2, T8, T15) |
| F14 Cache Eviction | ROI recalculation | Delta baseline ROI = 4.2 (สูงมาก → ไม่ถูก evict) |
| F5 Bandit | Reward signal | Delta arm ได้ reward สูง → เพิ่ม weight สำหรับ code review sessions |

### สรุป Phase 4

| Turn | Budget | Input Tokens (ก่อน) | Input Tokens (หลัง) | Output Tokens (ก่อน) | Output Tokens (หลัง) | Stage หลัก |
|---|---|---:|---:|---:|---:|---|
| T16 | **Yellow** | 14,200 | 4,900 | - | - | Disclosure, Delta |
| T17 | Yellow | 6,100 | 1,200 | 1,900 | 1,140 | Sketch, Caveman |
| T18 | Yellow | 5,800 | 1,100 | 2,200 | 1,320 | Sketch, Intent Filter |
| T19 | Yellow | 7,100 | 1,800 | 3,200 | 1,920 | Delta, Intent Filter |
| T20 | Yellow | 6,760 | 2,860 | 1,800 | 1,080 | Post-proxy (Waste, Bandit) |
| **รวม Phase 4** | | **39,960** | **11,860** | **9,100** | **5,460** | |

---

## สรุปทั้ง Session: Per-Stage Metrics

### Stage ที่ทำงานทุก Turn

| Stage | จำนวนครั้ง | Chars ประหยัด (รวม) | Avg chars/run | % ของ Total Savings | ประเภท |
|---|---:|---:|---:|---:|---|
| F7 Semantic Dedup | 20 | 1,420 | 71 | 0.4% | INPUT |
| F16 Caveman | 20 | 42,180 | 2,109 | 12.7% | OUTPUT (indirect) |
| F1 Chunker | 20 | (cache) | - | indirect | INPUT (cache) |
| F17 TextComp | 18 | 3,780 | 210 | 1.1% | INPUT |

### Stage ที่ทำงานบาง Turn

| Stage | จำนวนครั้ง | Chars ประหยัด (รวม) | Avg chars/run | % ของ Total Savings | ประเภท | เงื่อนไข |
|---|---:|---:|---:|---:|---|---|
| F8 Delta | 16 | 24,640 | 1,540 | 7.4% | INPUT | System prompt เปลี่ยนนิดหน่อย |
| F18 ToolComp | 8 | 119,900 | 14,988 | 36.1% | INPUT | tool_result มีเนื้อหาเยอะ |
| F13 Intent Filter | 6 | 8,820 | 1,470 | 2.7% | OUTPUT | Code/search intent |
| F19 ToolFilter | 2 | 8,400 | 4,200 | 2.5% | INPUT | Tools > 15 |
| F15 Disclosure | 4 | 17,040 | 4,260 | 5.1% | INPUT | Yellow budget |
| F9 Sketch | 4 | 16,840* | 4,210 | diagnostic | INPUT | Near-duplicate |
| PasteGuard | 2 | 120 | 60 | < 0.1% | INPUT | PII detected |

### Stage ที่ไม่ทำงาน

| Stage | เหตุผล |
|---|---|
| F6 Summarizer | Budget ไม่ถึง Red |
| F6 TextRank | ไม่ activate (Green/Yellow) |
| F4 Prefetcher | ทำงาน post-proxy เท่านั้น |
| F11 Waste Detection | ทำงาน post-proxy เท่านั้น |
| F5 Bandit | ทำงาน post-proxy เท่านั้น |
| F14 Cache Eviction | ทำงาน periodic |
| F10 Warm Start | ทำงานตอน session init เท่านั้น |
| F20 CompCache | Transparent wrapper ทำงานเสมอ |

---

## Waste Detection Report (Turn 20)

Post-proxy feedback สร้าง report:

```
WASTE FINDINGS for sess_abc123:
===============================

[redundant_tool_call] MEDIUM
  Tool: Read
  File: internal/middleware/ratelimit.go
  Called 3 times (T2, T8, T15) with overlapping line ranges
  Tokens wasted: ~2,400 input tokens
  Suggestion: Cache file contents for same path within session

[oversized_context] LOW
  Turn 2: git diff returned 3,847 lines but only ~800 relevant
  Could use git diff --stat first, then targeted reads
  Tokens wasted: ~4,800 input tokens

Total waste identified: 7,200 tokens (3.8% of session)
```

---

## Cost Comparison

### ราคาอ้างอิง: Claude Sonnet 4

| รายการ | Rate |
|---|---|
| Input tokens | $3.00 / 1M tokens |
| Output tokens | $15.00 / 1M tokens |

### เปรียบเทียบค่าใช้จ่าย

| รายการ | ไม่มี Optimizer | มี Optimizer | ประหยัด |
|---|---:|---:|---:|
| Input tokens | 187,420 | 108,630 | 78,790 (42.0%) |
| Output tokens | 31,200 | 19,880 | 11,320 (36.3%) |
| **ค่า Input** | $0.562 | $0.326 | $0.236 |
| **ค่า Output** | $0.468 | $0.298 | $0.170 |
| **รวม** | **$1.030** | **$0.624** | **$0.406 (39.4%)** |

### ถ้าเป็น Code Review Session 50 ครั้ง/วัน

| รายการ | ต่อวัน | ต่อเดือน |
|---|---:|---:|
| ประหยัดได้ | $20.30 | **$609.00** |
| ประหยัด tokens | 4,505,500 | 135,165,000 |

---

## Data Flow Visualization

```
Turn 1: "review PR #42"
  │
  ├─ System Prompt (4,210 chars)
  │   ├─ F7 Semantic Dedup → 4,120 (-90)
  │   ├─ F8 Delta → 2,480 (-1,640)
  │   ├─ F17 TextComp → 2,310 (-170)
  │   ├─ F1 Chunker → (cache reorder)
  │   └─ F16 Caveman → 229 (-2,081)
  │
  ├─ Tools Manifest (27 tools, ~6,000 chars)
  │   └─ F19 ToolFilter → 8 tools (~1,800 chars) (-4,200)
  │
  ├─ User Message (90 chars) → passthrough
  │
  ├─ Privacy.MaskRequest → no PII found
  │
  └─ → Provider API: 2,119 chars input

Turn 2: git diff returns (142,800 chars in tool_result)
  │
  ├─ System Prompt → same pipeline → 229 chars
  │
  ├─ tool_result (git diff output)
  │   └─ F18 ToolComp (diff format) → 38,200 chars (-104,600)
  │
  └─ → Provider API: 38,429 chars input

Turn 12: Claude writes review (output path)
  │
  └─ Provider Response (4,820 chars)
      └─ F13 Intent Filter (code intent) → 2,890 chars (-1,930)
          └─ → Client: 2,890 chars

Turn 16: Budget Yellow, large file contents
  │
  ├─ System Prompt → Caveman → 229 chars
  │
  ├─ tool_result (file content, 28,400 chars)
  │   └─ F15 Disclosure (Yellow) → 11,360 chars (-17,040)
  │
  └─ → Provider API: 11,589 chars input

Turn 20: Post-proxy feedback
  │
  ├─ F4 Prefetcher → record Read→Edit transition
  ├─ F11 Waste → flag redundant_tool_call
  ├─ F14 Cache → recalculate Delta ROI
  └─ F5 Bandit → reward Delta arm
```

---

## บทเรียนสำหรับนภัส

1. **ToolComp ประหยัดสุด** สำหรับ code review: git diff output ลด 73% เพราะ format-aware compression เข้าใจ diff syntax
2. **Delta Encoding** ประหยัด ~40% system prompt ทุก turn เพราะ system prompt เปลี่ยนนิดเดียว (file path)
3. **Intent Filter** ประหยัด 40% output เวลา Claude ชอบเล่ายาวๆ แต่เราต้องการแค่ code suggestions
4. **Waste Detection** จับได้ว่า Read ไฟล์ซ้ำ 3 ครั้ง → บอกว่าควร cache ไว้ใน session
5. **Budget Yellow** ทำให้ Disclosure เข้ามาช่วย ตัด tool_result เก่าที่ไม่จำเป็นแล้ว
6. **Caveman Lite** ประหยัด 94% ของ system prompt ทุก turn โดยแทนที่ด้วย 229-char style directive
