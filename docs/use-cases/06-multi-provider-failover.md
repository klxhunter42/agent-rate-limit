# Multi-Provider Routing & Failover: End-to-End Use Case

**ทีมแพลตฟอร์ม - วันทำงานปกติของ API Gateway**

วันที่: 7 พฤษภาคม 2569 | Provider: Anthropic, Z.AI, Google Gemini

---

## ภาพรวมสถาปัตยกรรม

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Claude Code CLI / IDE                        │
│              ANTHROPIC_BASE_URL=http://gateway:9000                 │
│              ANTHROPIC_AUTH_TOKEN=arl_<profile-token>               │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ HTTP POST /v1/messages
                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      API Gateway (Go, :8080)                        │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────────┐    │
│  │ Rate Limiter│  │ 13-Stage     │  │ Profile-Based Routing    │    │
│  │ (Distributed│  │ Optimizer    │  │ claude-oauth -> Anthropic│    │
│  │  + Adaptive)│  │ Pipeline     │  │ zai          -> Z.AI     │    │
│  │             │  │              │  │ gemini-oauth -> Gemini   │    │
│  │ 60 req/min  │  │ 40-60% token │  │                          │    │
│  │ per user    │  │ savings      │  │                          │    │
│  └─────────────┘  └──────────────┘  └──────────────────────────┘    │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────────┐    │
│  │ Bandit (F5) │  │ Sketch (F9)  │  │ Cache Eviction (F14)     │    │
│  │ LinUCB      │  │ SimHash      │  │ ROI-based cleanup        │    │
│  │ adaptive    │  │ near-dup     │  │                          │    │
│  └─────────────┘  └──────────────┘  └──────────────────────────┘    │
│  ┌─────────────┐  ┌──────────────┐                                  │
│  │ Warm Start  │  │ Caveman (F16)│                                  │
│  │ (F10)       │  │ Style inject │                                  │
│  └─────────────┘  └──────────────┘                                  │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
              ┌────────────────┼────────────────┐
              ▼                ▼                ▼
     ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
     │  Anthropic   │ │    Z.AI      │ │   Gemini     │
     │ claude-oauth │ │     zai      │ │ gemini-oauth │
     │ $3/M tokens  │ │ $0.5/M tokens│ │$1.25/M tokens│
     └──────────────┘ └──────────────┘ └──────────────┘
```

---

## 08:00 - เช้า: การทำงานปกติ

### สถานการณ์

ทีมแพลตฟอร์ม 8 คน เริ่มวันทำงาน ทุกคนใช้ Claude Code CLI เชื่อมต่อผ่าน gateway

```
Engineer -> Claude Code CLI -> gateway:9000 -> claude-oauth -> api.anthropic.com
```

### การตั้งค่า Profile

แต่ละ engineer มี profile เชื่อมกับ `claude-oauth`:

```bash
# Profile: th15011880, target: claude-oauth
# Token: arl_2f3a72a7eb07b4c43ffe87d8c19776eecf62c4c64e30285eee0796198bc91be1

# Claude Code settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://gateway:9000",
    "ANTHROPIC_AUTH_TOKEN": "arl_2f3a72a7..."
  }
}
```

> **Note:** `apiKeyHelper` + `ANTHROPIC_API_KEY` is the legacy method. Use `ANTHROPIC_AUTH_TOKEN` instead.

### ข้อมูลการร้องขอ

| พารามิเตอร์ | ค่า |
|---|
| Model หลัก | `claude-sonnet-4-20250514` |
| Rate limit | 60 req/min/user |
| Optimizer pipeline | 13 stages ทำงาน |
| Provider route | `claude-oauth` -> `anthropic` (fallback) |

### Optimizer Pipeline ที่ทำงาน (08:00-10:00)

```
Request: "Write a Go HTTP handler for user registration"
│
├─ F7  semantic_dedup     -> dedup "You are expert" sentences    (-3% input)
├─ F1  chunker            -> reorder stable chunks               (-5% input)
├─ F8  delta              -> diff-encode vs cached system prompt (-20% input)
├─ F9  sketch             -> check near-duplicate prompts        (flagged)
├─ F17 textcomp           -> remove filler words                 (-8% input)
├─ F16 caveman            -> inject [OUTPUT STYLE - lite]        (-30% output)
├─ F13 intent_filter      -> classify: "code" intent             (passthrough)
│
├─ Privacy: PasteGuard    -> scan for secrets/PII                (none found)
│
└─ Route: claude-oauth -> api.anthropic.com
    Headers: Authorization: Bearer sk-ant-oat01-*
             anthropic-beta: oauth-2025-04-20
             Billing: Go billing injection (Path 1)
```

### เมตริกช่วงเช้า (08:00-10:30)

| เมตริก | ค่า |
|---|
| คำขอทั้งหมด | 847 คำขอ |
| Provider หลัก | `claude-oauth` (100%) |
| Input tokens | 1.2M |
| Output tokens | 420K |
| Token savings (optimizer) | ~52% |
| Latency avg | 1.8s |
| 429 errors | 0 |
| Cost (Claude $3/M input, $15/M output) | $10.05 |

---

## 10:30 - Anthropic Rate Limit ถูก Hit

### สถานการณ์

วันนี้มีการ deploy ใหญ่ ทีมทั้งหมดเร่งเขียนโค้ดพร้อมกัน คำขอพุ่งขึ้น

```
10:28  Engineer A: code gen request  -> 200 OK (sonnet-4)
10:29  Engineer B: code gen request  -> 200 OK (sonnet-4)
10:30  Engineer C: code gen request  -> 429 Rate Limit (Anthropic)
10:30  Engineer A: code gen request  -> 429 Rate Limit (Anthropic)
10:30  Engineer D: config edit req   -> 429 Rate Limit (Anthropic)
```

### Gateway Response: Adaptive Rate Limiter

```
AdaptiveLimiter ตรวจพบ 429 จาก upstream
│
├─ limit = max(1, 60 * 0.5) = 30 req/min   // ลดครึ่งหนึ่ง
├─ Record peak = 60                          // จำ limit เดิมไว้
├─ Cooldown: 5 วินาที (ห้ามเพิ่ม limit)
├─ Learned ceiling: 60, ห้ามเกินเป็นเวลา 5 นาที
│
└─ ประกาศผ่าน metrics:
   api_gateway_rate_limit_adjustments{direction="decrease"} +1
```

### Model-Level Fallback (429 Handling)

Gateway ลอง model เบากว่าก่อน จากนั้นจึง failover ข้าม provider:

```
Request: claude-sonnet-4-20250514
│
├─ Step 1: Model fallback chain
│  claude-sonnet-4-20250514 -> claude-haiku-4-5-20251001
│  Result: 429 (haiku ก็โดนด้วย)
│
├─ Step 2: Provider fallback
│  claude-oauth -> anthropic (API key route)
│  Result: 429 (Anthropic ทั้งระบบ)
│
└─ Step 3: Cross-provider failover
   │
   ├─ คำขอ code generation -> ยังรอ Claude (critical)
   ├─ คำขอ config editing  -> failover ไป Z.AI glm-5
   └─ คำขอ analysis        -> failover ไป Gemini
```

### Bandit Decision: เรียนรู้ Routing อัตโนมัติ

LinUCB bandit ตรวจพบว่ามี provider ใหม่ให้เลือก:

```
Bandit arms ที่มี:
┌────────────────────┬───────────┬─────────────┬───────────┐
│ Arm                │ Mean (θ)  │ Uncertainty │ Score     │
├────────────────────┼───────────┼─────────────┼───────────┤
│ claude-sonnet-4    │ 0.85      │ LOW (warm)  │ 0.87      │
│ glm-5              │ 0.0       │ HIGH (cold) │ 1.00 (!)  │
│ gemini-2.5-flash   │ 0.0       │ HIGH (cold) │ 1.00 (!)  │
└────────────────────┴───────────┴─────────────┴───────────┘
                                      ^-- exploration bonus
                                      สูงเพราะยังไม่มีข้อมูล

Alpha=1.0, dim=10 context features
Arm states in Redis: bandit:state:glm-5, bandit:state:claude-sonnet-4
```

**ผลลัพธ์**: Bandit เลือก `glm-5` สำหรับ non-critical requests (exploration phase)

### การเปรียบเทียบต้นทุน: Claude vs GLM

| รายการ | Claude Sonnet 4 | Z.AI GLM-5 | ผลต่าง |
|---|
| Input tokens | 500K x $3/M = $1.50 | 500K x $0.5/M = $0.25 | -$1.25 |
| Output tokens | 180K x $15/M = $2.70 | 180K x $1.5/M = $0.27 | -$2.43 |
| **Subtotal** | **$4.20** | **$0.52** | **-$3.68 (-87.6%)** |

> GLM-5 ถูกกว่า 6 เท่า สำหรับ config editing tasks

### Bandit Reward Signal (หลัง response กลับมา)

```
PostProxyFeedback(sessionID, model, input, output)
│
├─ glm-5 code_gen:     output/input = 120/800  = 0.15 (reward low)
│  -> Bandit จด: "glm-5 ไม่เก่ง code gen"
│
├─ glm-5 config_edit:  output/input = 200/500  = 0.40 (reward OK)
│  -> Bandit จด: "glm-5 ทำ config edit ได้ดี"
│
├─ claude-sonnet code_gen: output/input = 600/800 = 0.75 (reward high)
│  -> Bandit จด: "claude เก่ง code gen มาก"
│
└─ Update A, b matrices in Redis (bandit:state:<arm>)
   reward = min(output/input, 1.0)
   A += phi * phi^T    // 10x10 outer product
   b += reward * phi   // 10-element reward vector
```

### สถานะช่วง 10:30-11:00

```
┌──────────────────────────────────────────────────┐
│         Traffic Distribution 10:30-14:00         │
├──────────────────────────────────────────────────┤
│                                                  │
│ Claude (claude-oauth)   ████████████░░░  65%     │
│   - code generation, complex refactoring         │
│                                                  │
│ Z.AI (glm-5)           ██████░░░░░░░░░  30%      │
│   - config editing, YAML/JSON edits              │
│                                                  │
│ Gemini (gemini-oauth)  ███░░░░░░░░░░░░░   5%     │
│   - log analysis, text summarization             │
│                                                  │
└──────────────────────────────────────────────────┘

Rate limit: 30 req/min (adaptive, ลดจาก 60)
429 errors: 0 (หลังจากปรับ limit + failover)
```

---

## 14:00 - Anthropic Outage ทั้งระบบ

### สถานการณ์

Anthropic ล่มทั้งระบบ ไม่มี Claude เลย

```
14:00:00  POST /v1/messages -> claude-oauth -> api.anthropic.com
          Response: 503 Service Unavailable

14:00:01  POST /v1/messages -> claude-oauth -> api.anthropic.com
          Response: 502 Bad Gateway

14:00:02  POST /v1/messages -> anthropic (API key fallback)
          Response: 503 Service Unavailable

14:00:03  Gateway ตัดสินใจ: full provider failover
```

### Gateway Failover Sequence

```
Handler.Messages()
│
├─ 1. ตรวจพบ Anthropic 502/503
│     MarkCooldown("anthropic", "claude-sonnet-4-20250514")
│
├─ 2. Model fallback chain (ทั้งหมด fail เพราะ outage)
│     claude-sonnet-4-20250514 -> claude-haiku-4-5-20251001 -> 503
│
├─ 3. Provider fallback (anthropic API key) -> 503
│
├─ 4. Cross-provider routing activated!
│     │
│     ├─ Bandit ตรวจ arms: glm-5 และ gemini-2.5-flash ยัง available
│     │
│     ├─ Intent classification (F13):
│     │  ├─ code_gen   -> glm-5 (bandit เคยเรียนรู้: OK สำหรับ code)
│     │  ├─ config     -> glm-5 (bandit: ดีมาก)
│     │  ├─ analysis   -> gemini-2.5-flash (ดีกว่าสำหรับ analysis)
│     │  └─ chat       -> glm-5 (ถูกสุด)
│     │
│     └─ Profile override: สลับ provider ชั่วคราว
│        target: claude-oauth -> zai (auto-remap)
│        model: claude-sonnet-4-20250514 -> glm-5 (provider default)
```

### Optimizer Adaptation: ปรับตัวสำหรับ Provider ใหม่

```
Optimizer stages ปรับ behavior สำหรับ Z.AI:
│
├─ F8  Delta:    เคลียร์ cached baseline (key: sys:claude-sonnet-4)
│                สร้าง baseline ใหม่สำหรับ glm-5
│
├─ F9  Sketch:   SimHash bit vectors ทำงานเหมือนเดิม
│                (provider-agnostic, detect ซ้ำได้ไม่ว่า model ไหน)
│
├─ F14 Cache:    Cache Eviction ทำความสะอาด Claude-specific entries
│                ROI score ต่ำเพราะ keys ไม่ถูก hit แล้ว
│                Evict bottom 10% = Claude system prompt caches
│
├─ F10 Warm Start: สร้าง session patterns ใหม่สำหรับ GLM
│                   Feature vector (32-dim) scan หา past GLM sessions
│                   Cosine similarity >= 0.5 threshold
│
├─ F16 Caveman:  [OUTPUT STYLE - lite] ทำงานเหมือนเดิม
│                GLM model ก็ตอบสั้นลงตาม directive
│
└─ F5  Bandit:   Reward signals มาจาก GLM responses แล้ว
                  A matrix, b vector สะสมใน Redis (24h TTL)
```

### Traffic Distribution ช่วง Outage (14:00-16:00)

```
┌──────────────────────────────────────────────────┐
│         Traffic Distribution 14:00-16:00         │
│         (Anthropic Outage)                       │
├──────────────────────────────────────────────────┤
│                                                  │
│ Claude (claude-oauth)   ░░░░░░░░░░░░░░░   0%     │
│   [OFFLINE - Anthropic outage]                   │
│                                                  │
│ Z.AI (glm-5)           ██████████████  75%       │
│   - code gen, config editing, general chat       │
│   - bandit reward: 0.45 avg (acceptable)         │
│                                                  │
│ Gemini (gemini-2.5-flash) ██████████░░░  25%     │
│   - log analysis, doc review, summarization      │
│   - bandit reward: 0.55 avg (good for analysis)  │
│                                                  │
│ Gemini fallback chain:                           │
│   gemini-2.5-pro -> gemini-2.5-flash             │
│                   -> gemini-2.5-flash-lite       │
│                   -> gemini-2.0-flash            │
│                                                  │
└──────────────────────────────────────────────────┘
```

### คำขอที่อยู่ระหว่างดำเนินการ (In-flight Request Handling)

```
Engineer A: ส่งคำขอ code gen ตอน 14:00:00 พอดี
│
├─ Request ไป claude-oauth -> 503
├─ Model fallback (haiku) -> 503
├─ Provider fallback (anthropic) -> 503
├─ Queue: push to Dragonfly job queue (async retry)
│  LPUSH job JSON -> queue:pending
│  Result key: result:<requestID> TTL 10m
│
├─ Cross-provider retry: glm-5
│  Model: claude-sonnet-4 -> glm-5 (auto-map)
│  Route: zai -> api.z.ai/api/anthropic
│  Format: Anthropic (compatible)
│  Auth: x-api-key from ZAI_API_KEYS
│
└─ Response 200 OK จาก Z.AI
   Latency: 2.1s (vs 1.8s Claude avg)
```

---

## 16:00 - Anthropic กลับมาแล้ว (Canary Failback)

### สถานการณ์

Anthropic ฟื้นจาก outage Gateway เริ่มส่ง traffic กลับแบบค่อยเป็นค่อยไป

### Canary Failback Process

```
16:00  Anthropic health check: 200 OK
│
├─ Phase 1: Canary 10% (16:00-16:15)
│  - ส่ง 10% ของ code gen requests ไป Claude
│  - 90% ยังไป Z.AI + Gemini
│  - ตรวจ success rate, latency, reward
│
├─ Phase 2: 30% (16:15-16:30)
│  - Bandit reward signals confirm:
│    claude-sonnet-4 code_gen: reward = 0.82 (ดีมาก)
│    glm-5 code_gen:          reward = 0.38 (พอใช้)
│  - เพิ่มเป็น 30%
│
├─ Phase 3: 60% (16:30-16:45)
│  - ทุกอย่างปกติ, ไม่มี 429
│  - Adaptive rate limiter เพิ่ม limit กลับ
│    gradient = (minRTT + buffer) / sampleRTT
│    limit = min(maxLimit, gradient * 30 + sqrt(30))
│    -> limit = 45 req/min
│
└─ Phase 4: 100% (16:45+)
   - Traffic กลับมาปกติ
   - Rate limit: 55 req/min (ยังไม่ถึง 60, adaptive เพิ่มทีละนิด)
   - Z.AI ยังรับ non-critical requests บางส่วน (bandit แนะนำ)
```

### Bandit Reward Comparison (หลัง failback)

```
LinUCB scores หลังจาก 1 วันของการเรียนรู้:
┌─────────────────────┬──────────────┬───────────────┬──────────────┐
│ Arm (Model)         │ Mean Reward  │ Task Strength │ Bandit Score │
├─────────────────────┼──────────────┼───────────────┼──────────────┤
│ claude-sonnet-4     │ 0.82         │ code_gen: +++ │ 0.85         │
│ glm-5               │ 0.45         │ config: +++   │ 0.55         │
│ gemini-2.5-flash    │ 0.58         │ analysis: ++  │ 0.60         │
└─────────────────────┴──────────────┴───────────────┴──────────────┘

สรุปการเรียนรู้ของ Bandit:
- code_gen + claude   = reward สูงสุด -> เลือก Claude สำหรับ code
- config  + glm-5     = reward ดี      -> เลือก GLM สำหรับ config
- analysis + gemini   = reward ดี      -> เลือก Gemini สำหรับ analysis
```

### Traffic Distribution หลัง Failback (16:45-18:00)

```
┌──────────────────────────────────────────────────┐
│         Traffic Distribution 16:45-18:00         │
│         (Post-Failback, Normal)                  │
├──────────────────────────────────────────────────┤
│                                                  │
│ Claude (claude-oauth)   ██████████████░  80%     │
│   - code generation, refactoring, debugging      │
│   - bandit reward: 0.82 (verified)               │
│                                                  │
│ Z.AI (glm-5)           ███░░░░░░░░░░░░  15%      │
│   - config editing, YAML/JSON, quick edits       │
│   - bandit reward: 0.45 (acceptable)             │
│                                                  │
│ Gemini (gemini-2.5-flash) █░░░░░░░░░░░░   5%     │
│   - log analysis, documentation                  │
│   - bandit reward: 0.58                          │
│                                                  │
└──────────────────────────────────────────────────┘
```

---

## 18:00 - เย็น: สรุป Cost Report

### Token Usage ทั้งวัน

| ช่วงเวลา | Input Tokens | Output Tokens | Provider หลัก | คำขอ |
|---|
| 08:00-10:30 | 1,200,000 | 420,000 | Claude (100%) | 847 |
| 10:30-14:00 | 800,000 | 280,000 | Claude 65%, GLM 30%, Gemini 5% | 623 |
| 14:00-16:00 | 400,000 | 150,000 | GLM 75%, Gemini 25% | 312 |
| 16:00-18:00 | 100,000 | -50,000* | Claude 80%, GLM 15%, Gemini 5% | 198 |
| **รวม** | **2,500,000** | **800,000** | | **1,980** |

*output ติดลบเพราะยังไม่ครบช่วง

### การกระจายต้นทุนตาม Provider

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        Cost Breakdown by Provider                       │
├──────────────┬─────────────┬──────────────┬──────────────┬──────────────┤
│ Provider     │ Input Cost  │ Output Cost  │ Total        │ % of Total   │
├──────────────┼─────────────┼──────────────┼──────────────┼──────────────┤
│ Claude       │             │              │              │              │
│  1.6M input  │ $4.80       │              │              │              │
│  520K output │             │ $7.80        │ $12.60       │ -            │
│  @ $3/$15    │             │              │              │              │
├──────────────┼─────────────┼──────────────┼──────────────┼──────────────┤
│ GLM-5        │             │              │              │              │
│  720K input  │ $0.36       │              │              │              │
│  210K output │             │ $0.32        │ $0.68        │ -            │
│  @ $0.5/$1.5 │             │              │              │              │
├──────────────┼─────────────┼──────────────┼──────────────┼──────────────┤
│ Gemini       │             │              │              │              │
│  180K input  │ $0.23       │              │              │              │
│  70K output  │             │ $0.35        │ $0.58        │ -            │
│  @ $1.25/$5  │             │              │              │              │
├──────────────┼─────────────┼──────────────┼──────────────┼──────────────┤
│ **Actual**   │ **$5.39**   │ **$8.47**    │ **$13.86**   │              │
│ + Optimizer  │ -$1.35      │ -$8.31       │ -$9.66       │              │
│  savings     │ (25% input) │ (50% output) │              │              │
├──────────────┼─────────────┼──────────────┼──────────────┼──────────────┤
│ **NET COST** │ **$4.04**   │ **$0.16**    │ **$4.20**    │ **100%**     │
└──────────────┴─────────────┴──────────────┴──────────────┴──────────────┘
```

> หมายเหตุ: ตัวเลข optimizer savings คำนวณจาก input savings (semantic_dedup, delta, sketch, textcomp, chunker) + output savings (caveman style injection) ที่ทำงานตลอดทั้งวัน

### เปรียบเทียบ: มี Gateway vs ไม่มี Gateway

```
┌──────────────────────────────────────────────────────────────────┐
│                    Cost Comparison                               │
├────────────────────────┬─────────────────┬───────────────────────┤
│ สถานการณ์              │ ต้นทุน           │ หมายเหตุ             │
├────────────────────────┼─────────────────┼───────────────────────┤
│ ไม่มี Gateway           │                 │                      │
│ (ทุกอย่างไป Claude)     │ $12.50          │ 2.5M x $3/M +        │
│                        │                 │ 800K x $15/M          │
│                        │                 │ ไม่มี optimization    │
│                        │                 │ outage = งานหยุด      │
├────────────────────────┼─────────────────┼───────────────────────┤
│ มี Gateway              │                 │                      │
│ (multi-provider +      │ $4.20           │ ผสม provider          │
│  optimizer)            │                 │ + 40-60% token savings│
│                        │                 │ outage = ยังทำงานได้  │
├────────────────────────┼─────────────────┼───────────────────────┤
│ **ประหยัด**            │ **$8.30**       │ **66.4%**             │
└────────────────────────┴─────────────────┴───────────────────────┘
```

### สาเหตุหลักของการประหยัด

| แหล่งที่มา | ประหยัด | กลไก |
|---|
| Provider switching | $4.80 (38%) | GLM ถูกกว่า Claude 6x, รับ non-critical tasks |
| Input optimization | $2.15 (17%) | delta (-20%), textcomp (-8%), semantic_dedup (-3%) |
| Output optimization | $1.35 (11%) | caveman style injection (-30-50% output) |
| **รวม** | **$8.30 (66%)** | |

---

## Timeline สรุปทั้งวัน

```
เวลา     สถานะ              Provider           Rate Limit    เหตุการณ์
─────────────────────────────────────────────────────────────────────────────
08:00    🟢 ปกติ             Claude 100%        60/min        เริ่มวันทำงาน
         │                   │                   │             Optimizer ทำงาน
         │                   │                   │             ~52% token savings
         ▼                   ▼                   ▼
10:30    🟡 Rate Limit Hit   Claude 65%         30/min        Anthropic 429
         │                   GLM 30%            (ลดลง)        Adaptive limiter ลด
         │                   Gemini 5%                        Bandit เริ่มเรียนรู้ GLM
         ▼                   ▼                   ▼
14:00    🔴 Outage           GLM 75%            30/min        Anthropic ล่ม
         │                   Gemini 25%                       Full failover
         │                                                    Cache eviction ทำความสะอาด
         │                                                    Warm start สร้าง GLM patterns
         ▼                   ▼                   ▼
16:00    🟡 Canary Failback  Claude 10%         30/min        Anthropic ฟื้น
         │                   GLM 75%                          เริ่ม 10% canary
         │                   Gemini 15%                       Bandit confirm Claude > code
         ▼                   ▼                   ▼
16:45    🟢 กลับปกติ         Claude 80%         55/min        Failback เสร็จ
         │                   GLM 15%                          Bandit route อัตโนมัติ
         │                   Gemini 5%                        Rate limit ปรับขึ้น
         ▼                   ▼                   ▼
18:00    🟢 End of Day       Claude 80%         55/min        Cost: $4.20
                              GLM 15%                          vs $12.50 (ไม่มี gateway)
                              Gemini 5%                        ประหยัด 66%
```

---

## เมตริกสำคัญที่ Grafana Dashboard

### Prometheus Queries

```promql
# Token usage ต่อ provider
sum by (provider) (rate(api_gateway_tokens_total[5m]))

# Rate limit adjustments
sum by (direction) (rate(api_gateway_rate_limit_adjustments[5m]))

# Bandit arm selections
sum by (arm, exploratory) (rate(api_gateway_bandit_selections_total[5m]))

# Bandit reward per arm
sum by (arm) (rate(api_gateway_bandit_reward_total[5m]))

# Optimizer savings by technique
sum by (technique) (rate(api_gateway_optimizer_chars_saved_total[5m]))

# Cache eviction activity
rate(api_gateway_cache_eviction_keys_evicted_total[5m])

# 429 errors per provider
sum by (provider) (rate(api_gateway_upstream_errors_total{code="429"}[5m]))
```

### Key Dashboard Panels

```
┌─────────────────────────────────────────────────────────────────────┐
│  API Gateway - Multi-Provider Dashboard                             │
├──────────────────────────────┬──────────────────────────────────────┤
│  Provider Health             │  Request Rate (req/s)                │
│  ┌────────┬────┬──────┐     │  ██ Claude  ████████████░░  80%       │
│  │Status  │RPS │Lat   │     │  ██ GLM-5   ███░░░░░░░░░░░  15%       │
│  ├────────┼────┼──────┤     │  ██ Gemini  █░░░░░░░░░░░░░░   5%      │
│  │Claude  │ 12 │ 1.8s │     │                                       │
│  │GLM-5   │  3 │ 2.1s │     ├──────────────────────────────────────┤
│  │Gemini  │  1 │ 1.5s │     │  Token Savings (%)                    │
│  └────────┴────┴──────┘     │  ████████████████████░░░░  52%        │
│                              │                                      │
├──────────────────────────────┼──────────────────────────────────────┤
│  Bandit Arm Rewards          │  Cost (USD)                          │
│  Claude  ████████████░ 0.82  │  Today: $4.20                        │
│  GLM-5   ██████░░░░░░ 0.45  │  No-gateway: $12.50                   │
│  Gemini  ███████░░░░░ 0.58  │  Savings: $8.30 (66%)                 │
│                              │                                      │
├──────────────────────────────┼──────────────────────────────────────┤
│  Rate Limit Status           │  Optimizer Stage Breakdown           │
│  Current: 55/min             │  delta:    ████████████░  20%        │
│  Peak:    60/min             │  caveman:  ████████░░░░░  30%        │
│  Cooldown: No                │  textcomp: ████░░░░░░░░   8%         │
│                              │  sketch:   ██████░░░░░░░  15%        │
└──────────────────────────────┴──────────────────────────────────────┘
```

---

## บทสรุป

| เมตริก | ค่า |
|---|
| คำขอทั้งหมด | 1,980 |
| Uptime | 100% (failover ช่วยไม่ให้งานหยุด) |
| Token savings | ~52% (input + output) |
| Cost savings | 66% ($12.50 -> $4.20) |
| Outage impact | 0 downtime (แม้ Anthropic ล่ม 2 ชม.) |
| Bandit accuracy | เลือก provider ได้ถูกต้อง 85%+ หลังเรียนรู้ |

สิ่งสำคัญที่ทำให้ระบบทำงานได้:

1. **Adaptive Rate Limiter** - ปรับ limit อัตโนมัติตาม upstream 429 feedback
2. **Model Fallback Chain** - ลอง model เบาก่อน แล้วค่อยข้าม provider
3. **Bandit (LinUCB)** - เรียนรู้ว่า model ไหนเก่งที่ task ไหน
4. **13-Stage Optimizer** - บีบ token ทั้ง input และ output
5. **Cache Eviction** - ทำความสะอาด cache ที่ไม่จำเป็นตอน provider เปลี่ยน
6. **Warm Start** - ใช้ session patterns จาก provider ใหม่ลด cold-start waste
7. **Canary Failback** - ค่อยๆ ส่ง traffic กลับ ไม่กระทบ provider ที่เพิ่งฟื้น
