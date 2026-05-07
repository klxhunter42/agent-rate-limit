# Onboarding Guide: AI Gateway สำหรับสมาชิกใหม่ของ Platform Team

> **ตัวละคร**: วรรณา (Junior Engineer, Platform Team)
> **วันเริ่มงาน**: วันจันทร์
> **บทบาท**: ดูแล Kubernetes clusters, เขียน Go microservices, ใช้ Claude Code เป็นอุปกรณ์หลัก

---

## วันที่ 1: First Login - เชื่อมต่อ Gateway ครั้งแรก

### เช้าวันจันทร์

วรรณามาถึงออฟฟิศ 9 โมงเช้า พี่เจ (Tech Lead) พาเดินผ่าน server room แล้วอธิบายสถาปัตยกรรมของระบบ

**พี่เจ**: "ระบบเรามี AI Gateway เป็นตัวกลาง รับ request จาก Claude Code แล้วส่งต่อไป provider ต่างๆ เช่น Z.AI, Anthropic, OpenAI, Gemini มี optimizer pipeline 13 ขั้นตอนที่ช่วยประหยัด token ได้มาก"

วรรณานั่งลงที่เครื่อง เปิด terminal

### Step 1: ตั้งค่า Claude Code ให้เชื่อมต่อ Gateway

```json
// ~/.claude/settings.json
{
  "ANTHROPIC_BASE_URL": "https://ai.klxhub.com",
  "ANTHROPIC_AUTH_TOKEN": "arl_wanna_dev_2026q2"
}
```

วรรณาทดสอบ connection:

```bash
curl -s https://ai.klxhub.com/health | jq
```

```json
{
  "status": "healthy",
  "version": "2.1.131",
  "uptime_seconds": 345678,
  "redis_connected": true,
  "rate_limiter_connected": true
}
```

### Step 2: คำขอแรก - ถามเรื่อง K8s

วรรณาเปิด Claude Code แล้วพิมพ์:

```
> explain how Kubernetes HPA works with custom metrics
```

**สิ่งที่เกิดขึ้นใน Gateway**:

```
Request → Handler.HandleMessages()
│
├─ Budget Level: GREEN (session เพิ่งเริ่ม, context < 50%)
│
├─ OptimizeSystemPrompt (system prompt ~577 chars)
│  ├─ F7 semantic_dedup    → 577→565 chars (-12, dedup "You are" directives)
│  ├─ F1 chunker           → reorder stable chunks for cache alignment
│  ├─ F8 delta             → SKIP (no cached baseline yet)
│  ├─ F9 sketch            → SKIP (first request, no history)
│  ├─ F17 textcomp         → balanced mode, minor filler removal
│  └─ F16 caveman          → lite tier (30% output reduction directive)
│
├─ OptimizeMessages (user message)
│  └─ whitespace + dedup   → minimal change
│
├─ privacy.MaskRequest     → no secrets detected
│
└─ Proxy → Z.AI glm-5
   └─ Response: 233 chars, clean explanation
```

**ผลลัพธ์ใน Grafana** (Dashboard: arl-gateway):

```
┌─────────────────────────────────────────────────────────┐
│  AI Gateway - Request Flow                               │
│                                                          │
│  Request Rate: 1 req/min     Budget: ● GREEN            │
│  Model: glm-5                Provider: zai              │
│                                                          │
│  ┌─── Optimizers Activated ───────────────────────────┐  │
│  │ semantic_dedup  ████████░░░░  12 chars saved       │  │
│  │ chunker         ████████████░  cache reorder        │  │
│  │ textcomp        ████░░░░░░░░  minor                │  │
│  │ caveman (lite)  █████████████  336 chars replaced  │  │
│  └─────────────────────────────────────────────────────┘  │
│                                                          │
│  Input:  439 tokens (original ~530, saved 17%)           │
│  Output: 50 tokens                                       │
│  Latency: 1.2s (upstream) + 2.1ms (optimizer overhead)   │
└─────────────────────────────────────────────────────────┘
```

### Step 3: เข้าใจ Budget Levels

พี่เจวาด diagram ให้ดู:

```
┌────────────────────────────────────────────────────────────────┐
│                    BUDGET LEVEL SYSTEM                          │
│                                                                │
│  ● GREEN    (< 50% context window)                             │
│    เปิด: semantic_dedup, chunker, delta, sketch, textcomp,   │
│          caveman lite (30% output reduction)                   │
│                                                                │
│  ● YELLOW   (50-75% context window)                            │
│    เปิด: ทุกอย่างใน GREEN +                                  │
│          packer, disclosure truncation (L2),                   │
│          caveman full (50% output reduction)                   │
│                                                                │
│  ● RED      (> 75% context window)                             │
│    เปิด: ทุกอย่างใน YELLOW +                                │
│          summarizer (50-70% truncation),                       │
│          intent_filter, caveman ultra (75% reduction)          │
│                                                                │
│  เมื่อ session ยาวนาน → context ใกล้เต็ม → budget เปลี่ยน  │
│  → optimizer ทำงานหนักขึ้นอัตโนมัติ                          │
└────────────────────────────────────────────────────────────────┘
```

**วรรณา**: "คือเหมือนเรามีระบบเศรษฐกิดนะคะ เวลา context ยังไม่เยอะ (green) optimizer ทำงานเบาๆ พอ session ยาวขึ้น (yellow/red) มันก็เร่งเครื่องให้มากขึ้นอัตโนมัติ"

**พี่เจ**: "ถูกต้อง! และที่สำคัญ - optimizer overhead ทั้งหมด < 3ms ต่อ request เทียบกับ upstream latency ~1-2 วินาที คือแทบจะไม่รู้สึกเลยว่ามีการ optimize"

### สรุปวันที่ 1

| หัวข้อ | สิ่งที่เรียนรู้ |
|--------|-----------------|
| Connection | ตั้ง `ANTHROPIC_BASE_URL` + `ANTHROPIC_AUTH_TOKEN` เพื่อเชื่อม Gateway |
| Budget Levels | Green/Yellow/Red ควบคุม optimizer intensity อัตโนมัติ |
| Green optimizers | semantic_dedup, chunker, delta, sketch, textcomp, caveman lite |
| Savings | ~17% input tokens, 30% output influence ใน green budget |
| Overhead | < 3ms ต่อ request (เทียบกับ 1-2s upstream) |

---

## วันที่ 2: Real Task - Debugging Go Service

### เช้าวันอังคาร

วันนี้วรรณาได้ task จริง: debug Go service ที่ deploy ไปแล้ว crash loop บน K8s

### Step 1: เริ่ม debug session ด้วย Claude Code

วรรณาพิมพ์ใน Claude Code:

```
> my api-gateway pod is crash looping, help me debug
> check the logs with kubectl logs api-gateway-7d9f8b6c4-x2k1p --previous
```

Claude Code เรียก Bash tool แล้วรัน `kubectl logs` ผลลัพธ์กลับมา ~200 บรรทัด

**สิ่งที่เกิดขึ้นใน Gateway**:

```
Request → HandleMessages()
│
├─ Budget Level: GREEN (turn 1-2, context ~15%)
│
├─ OptimizeSystemPrompt
│  ├─ semantic_dedup    → 12 chars saved
│  ├─ chunker           → cache alignment
│  └─ caveman lite      → 336 chars replaced
│
├─ OptimizeMessages
│  ├─ message_textcomp  → filler removed from user messages
│  └─ F18 ToolComp      → ★ ACTIVATED ★
│
│  tool_result block: kubectl logs output (~3000 chars)
│  Format detected: LOG
│  Compression:
│    - Dedup consecutive identical lines (40 lines → 3 lines + "×37 more")
│    - Keep first/last 5 lines of each unique group
│    - Result: 3000 → 850 chars (72% reduction!)
│
├─ F19 ToolFilter       → SKIP (< 15 tools in manifest)
├─ PasteGuard           → scanning for secrets...
│
└─ Proxy → Z.AI glm-5
```

### Step 2: PasteGuard ทำงาน

ระหว่าง debug วรรณา copy-paste config จาก Slack โดยไม่ได้ตั้งใจ และในนั้นมี API key:

```
> here's the config from slack, I think the issue is in the env vars:
> API_KEY=sk-ant-oat01-xYz12345abcdef...
> REDIS_URL=redis://prod-redis:6379
```

**PasteGuard ทำงานทันที**:

```
privacy.MaskRequest()
│
├─ Secret Detection (RegexDetector, <1ms)
│  ├─ Pattern: sk-ant-oat01-*  → MATCH → __SECRET_1__
│  └─ Pattern: redis://*:6379  → no match (not in entity list)
│
├─ PII Detection
│  └─ No EMAIL_ADDRESS or PHONE_NUMBER found
│
├─ Masking applied:
│  "API_KEY=__SECRET_1__"
│  "REDIS_URL=redis://prod-redis:6379"  (unchanged)
│
└─ Proxy to upstream: masked version sent
   → Model sees __SECRET_1__ instead of real key
   → Response comes back with __SECRET_1__
   → Unmasked back to real value before returning to client
```

**พี่เจ (จาก Slack)**: "เห็นไหมว่า PasteGuard ช่วยอะไรได้บ้าง ถ้าไม่มี API key จะถูกส่งไป provider โดยตรง และอาจจะโผล่ใน log ด้วย"

**วรรณา**: "แล้วถ้าผมตั้งใจส่ง key ล่ะคะ?"

**พี่เจ**: "PasteGuard จะ mask อยู่ดี แต่ response ที่กลับมาจะ unmask ให้ก่อนส่งกลับ คุณจะได้ค่าจริงๆ แต่ provider ไม่เห็น key จริง"

### Step 3: ดู Metrics Dashboard

วรรณาเปิด Grafana:

```
URL: https://ai.klxhub.com/grafana
Username: admin
Password: (from GRAFANA_ADMIN_PASSWORD)
```

**Dashboard: API Gateway Detailed (`arl-gateway`)**

```
┌──────────────────────────────────────────────────────────────────┐
│  API Gateway Detailed - วรรณา's Session                         │
│                                                                  │
│  ┌─── Request Rate ───────────────┐  ┌─── Latency ─────────────┐│
│  │  ██████████░░ 2.1 req/min     │  │  p50: 1.3s              ││
│  │  sync: ██████████  100%       │  │  p95: 2.1s              ││
│  │  async: ░░░░░░░░░░   0%       │  │  p99: 3.4s              ││
│  └────────────────────────────────┘  └──────────────────────────┘│
│                                                                  │
│  ┌─── Optimizer Performance ──────────────────────────────────┐  │
│  │                                                             │  │
│  │  Technique          Runs   Chars Saved   Avg/Run           │  │
│  │  ─────────────────────────────────────────────────────     │  │
│  │  semantic_dedup       4       48          12.0             │  │
│  │  toolcomp             2     4,300       2,150.0  ★         │  │
│  │  caveman (lite)       4     1,344        336.0             │  │
│  │  message_textcomp     3       178         59.3             │  │
│  │  message_text         2         2          1.0             │  │
│  │  ─────────────────────────────────────────────────────     │  │
│  │  TOTAL                       5,872 chars (~1,468 tokens)   │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─── PasteGuard Events ─────────────────────────────────────┐  │
│  │  Secrets detected: 1  (masked: __SECRET_1__)              │  │
│  │  PII detected: 0                                          │  │
│  │  Scan time: <1ms                                          │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─── Error Rate ─────────────────────────────────────────────┐  │
│  │  2xx: ████████████████████████████  98%                    │  │
│  │  4xx: ██                            2% (rate limit)        │  │
│  │  5xx: ░                             0%                     │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

วรรณาแก้ bug ได้: config mount path ผิด ทำให้ service ไม่เจอ TLS cert แก้ helm values แล้ว redeploy ผ่าน

### สรุปวันที่ 2

| เหตุการณ์ | Optimizer/Feature | ผลลัพธ์ |
|-----------|-------------------|---------|
| kubectl logs (200 บรรทัด) | **ToolComp** (F18) | 3,000 → 850 chars (72% reduction) |
| paste config มี API key | **PasteGuard** | Secret masked เป็น `__SECRET_1__` |
| debug session 4 turns | semantic_dedup, caveman, textcomp | ~1,468 tokens ประหยัดไป |
| ดู metrics | Grafana `arl-gateway` dashboard | เห็นทุก optimizer ที่ทำงาน |

**ToolComp highlight**: นี่คือ optimizer ที่ประหยัดที่สุดตอน debug เพราะ shell output, JSON logs มักมี repeated patterns ที่ compress ได้ง่าย (40-80% savings)

---

## วันที่ 3: Long Session - Feature Development

### เช้าวันพุธ

วันนี้วรรณา implement feature ใหม่: เพิ่ม health check endpoint ใน Go service ที่ตรวจสอบ Redis connection, rate limiter status, และ worker health

Session นี้ยาว 20+ turns เพราะมีทั้งเขียน code, review, refactor, เขียน tests

### Turn 1-5: Green Budget (เริ่มเขียน code)

```
> implement a /healthz endpoint in the gateway that checks:
> 1. Redis ping
> 2. Rate limiter connectivity
> 3. Worker pool status
> Return JSON with status for each component
```

**Gateway pipeline (turn 1-5)**:

```
Budget: GREEN (< 50% context)
│
├─ semantic_dedup  → 12-23 chars saved per turn
├─ chunker         → cache hits on repeated system prompt
├─ delta           → SKIP (system prompt not changing between turns)
├─ sketch          → detects near-duplicate prompts (turn 3-5)
├─ textcomp        → removes filler from user instructions
├─ caveman lite    → 30% output reduction directive
└─ ToolFilter      → SKIP (standard tool set, < 15 tools)
```

### Turn 6-12: Yellow Budget เริ่มทำงาน

หลังจาก 12 turns context ใกล้ 60%:

```
Budget: YELLOW (60% context window)
│
├─ ทุกอย่างใน GREEN +
├─ F12 packer           → ★ ACTIVATED ★
│   คำนวณ "utility score" ของแต่ละ message
│   ตัด messages เก่าที่ score < MIN_UTILITY (0.1)
│   เช่น: "file created" confirmations, "ok" responses
│
├─ F15 disclosure       → ★ ACTIVATED ★
│   BudgetAwareEscalate(ctx, content, "yellow")
│   tool_result blocks > 2000 chars → truncate to L2Tokens*8 chars
│   ตัวอย่าง: go test output 3,500 chars → 480 chars
│
├─ caveman              → UPGRADE to FULL tier (50% output reduction)
└─ bandit               → ค่อยๆ เรียนรู้ preference ของ session นี้
```

**ตัวอย่าง: packer ตัด context เก่า**

```
Original messages (turn 12):
  [0] user:   "implement /healthz..."           (utility: 0.9) ← KEEP
  [1] assist: "here's the code..."              (utility: 0.7) ← KEEP
  [2] user:   "looks good, add tests"           (utility: 0.8) ← KEEP
  [3] assist: "file created: healthz_test.go"   (utility: 0.05) ← DROP (low value)
  [4] user:   "run the tests"                   (utility: 0.6) ← KEEP
  [5] assist: "all tests passed"                (utility: 0.1) ← DROP (confirmation only)
  [6] user:   "now add redis check"             (utility: 0.85) ← KEEP

After packer:
  [0] user:   "implement /healthz..."
  [1] assist: "here's the code..."
  [2] user:   "looks good, add tests"
  [3] user:   "run the tests"
  [4] user:   "now add redis check"

Tokens saved: ~800 tokens from dropped messages
```

### Turn 13-15: Bandit เรียนรู้

หลังจาก session ยาวนาน F5 Bandit เริ่มเห็น pattern:

```
F5: Bandit (LinUCB, 10-dim context)
│
├─ Context features: {model: glm-5, code_ratio: 0.7, tool_freq: 0.4, ...}
├─ Arms: semantic_dedup, chunker, textcomp, caveman_lite, caveman_full, ...
│
├─ Turn 10: Explore → try textcomp aggressive mode
│   Reward: 0.3 (good savings, no quality loss)
│
├─ Turn 12: Exploit → use textcomp aggressive (confidence high)
│   Reward: 0.4 (even better savings)
│
├─ Turn 15: Explore → try summarizer early
│   Reward: -0.1 (too aggressive, lost important context)
│   → Bandit learns: summarizer should wait for red budget
│
└─ Result after 15 turns:
    textcomp: θ=0.35 (high confidence, use often)
    summarizer: θ=-0.05 (negative, avoid until red)
    caveman_full: θ=0.28 (good for code sessions)
```

### Turn 16-20: Waste Detection ทำงาน

```
F11: Waste Detection (7 detectors, every 60s)
│
├─ empty_response:     0 findings
├─ retry_churn:        1 finding (turn 14 ลอง 3 ครั้ง)
├─ loop_detection:     0 findings
├─ oversized_context:  1 finding ★
│   Severity: WARNING
│   Detail: "Context reached 65% at turn 16 with 3 redundant file reads"
│   Tokens wasted: ~1,200 tokens
│
├─ budget_exceeded:    0 findings
├─ redundant_tool_call: 2 findings ★
│   Turn 8: Read same file "handler.go" twice
│   Turn 11: Read same file "handler.go" third time
│   Recommendation: ใช้ context จาก read ครั้งแรกแทน
│
└─ low_value_response: 0 findings

Waste findings → Grafana:
  api_gateway_waste_findings_total{detector="oversized_context",severity="warning"} 1
  api_gateway_waste_findings_total{detector="redundant_tool_call",severity="info"} 2
```

### Grafana View ตอน Yellow Budget

```
┌──────────────────────────────────────────────────────────────────┐
│  Session Metrics - วรรณา's Feature Development                  │
│                                                                  │
│  ┌─── Budget Timeline ────────────────────────────────────────┐  │
│  │  Turn  1-5:  ● GREEN    ████████████████░░░░░░░░░  25%    │  │
│  │  Turn  6-12: ● YELLOW   ██████████████████████████  60%    │  │
│  │  Turn 13-20: ● YELLOW   ██████████████████████████  72%    │  │
│  │  (approaching RED...)                                      │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─── Optimizer Cumulative ───────────────────────────────────┐  │
│  │                                                             │  │
│  │  Technique          Runs   Total Saved   Contribution      │  │
│  │  ─────────────────────────────────────────────────────     │  │
│  │  toolcomp             8     12,400       42%  ████████████ │  │
│  │  caveman             20      8,200       28%  ████████     │  │
│  │  packer               5      4,000       14%  █████        │  │
│  │  disclosure           7      2,800       10%  ████         │  │
│  │  textcomp            18      1,060        4%  ███          │  │
│  │  semantic_dedup      20        240        1%  █            │  │
│  │  sketch               8        560        2%  █            │  │
│  │  ─────────────────────────────────────────────────────     │  │
│  │  TOTAL:                     29,260 chars (~7,315 tokens)   │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─── Bandit Learning ────────────────────────────────────────┐  │
│  │  Arm              Selections   Avg Reward   Exploration    │  │
│  │  textcomp_agg          12         0.38         exploit     │  │
│  │  caveman_full          15         0.31         exploit     │  │
│  │  summarizer             2        -0.05         avoid       │  │
│  │  delta                  5         0.15         explore     │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─── Waste Alerts ───────────────────────────────────────────┐  │
│  │  ⚠ oversized_context: context at 65%, 3 redundant reads   │  │
│  │  ℹ redundant_tool_call: "handler.go" read 3 times         │  │
│  │  → Tip: consolidate file reads, use Edit instead           │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

### สรุปวันที่ 3

| เหตุการณ์ | Feature | Impact |
|-----------|---------|--------|
| 20+ turn session | **Packer** เปิดที่ yellow budget | ตัด low-utility messages ประหยัด ~800 tokens |
| tool_result ใหญ่ | **Disclosure** truncate ที่ yellow | 3,500 → 480 chars (86% reduction) |
| เรียนรู้ pattern | **Bandit** explore/exploit | เลือก optimizer ที่เหมาะสมอัตโนมัติ |
| อ่านไฟล์ซ้ำ | **Waste Detection** ตรวจจับ | แจ้งเตือน redundant reads 2 ครั้ง |
| Cumulative savings | ทุก optimizer ทำงานร่วมกัน | ~7,315 tokens ประหยัดใน session เดียว |

---

## วันที่ 4: Cost Awareness

### เช้าวันพฤหัสบดี

วันนี้พี่เจสอนเรื่อง cost optimization ให้ทั้งทีม

### Step 1: เข้า Grafana Cost Dashboard

วรรณาเปิด **Cost Calculator & Savings** dashboard:

```
URL: https://ai.klxhub.com/grafana/d/arl-cost
```

```
┌──────────────────────────────────────────────────────────────────┐
│  Cost Calculator & Savings                                       │
│                                                                  │
│  ┌─── วรรณา's Usage (7 days) ────────────────────────────────┐  │
│  │                                                             │  │
│  │  Total Requests:        342                                 │  │
│  │  Avg Requests/Hour:     4.9                                 │  │
│  │                                                             │  │
│  │  Estimated Input Tokens:   127,400                          │  │
│  │  Estimated Output Tokens:   41,800                          │  │
│  │                                                             │  │
│  │  ─── Cost by Provider ────────────────────────────────     │  │
│  │  Z.AI (glm-5):           $0.27   ██████████████████  72%  │  │
│  │  Z.AI (glm-5-turbo):     $0.08   █████░               21% │  │
│  │  Anthropic (sonnet):     $0.02   █                     5%  │  │
│  │  Other:                  $0.01   ░                     2%  │  │
│  │  ──────────────────────────────────────────────────────    │  │
│  │  TOTAL COST:             $0.38                              │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─── Optimizer Savings ─────────────────────────────────────┐  │
│  │                                                             │  │
│  │  Without optimizer:                                        │  │
│  │    Est. input tokens:   198,300                            │  │
│  │    Est. cost:           $0.59                              │  │
│  │                                                             │  │
│  │  With optimizer:                                           │  │
│  │    Actual input tokens: 127,400                            │  │
│  │    Tokens saved:        70,900 (35.8% reduction)          │  │
│  │    Cost saved:          $0.21                              │  │
│  │    Actual cost:         $0.38                              │  │
│  │                                                             │  │
│  │  ┌─────────────────────────────────────────────────────┐  │  │
│  │  │  $0.59 → $0.38 = 35.8% cost reduction               │  │  │
│  │  │  ██████████████████████████████░░░░░░░░░░  64.2%    │  │  │
│  │  │  ████████████████████████████████████████░  100%     │  │  │
│  │  │  ^actual cost         ^original cost               │  │  │
│  │  └─────────────────────────────────────────────────────┘  │  │
│  │                                                             │  │
│  │  Top savers:                                               │  │
│  │  1. ToolComp:      28,400 tokens (40%)  ████████████      │  │
│  │  2. Caveman:       18,200 tokens (26%)  ████████          │  │
│  │  3. Packer:         9,800 tokens (14%)  █████              │  │
│  │  4. Sketch:         5,600 tokens (8%)   ███                │  │
│  │  5. Other:          8,900 tokens (12%)  ████               │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─── Rate Limit Savings ────────────────────────────────────┐  │
│  │  429 responses (rate limited): 23                          │  │
│  │  These would have been expensive API calls if not limited  │  │
│  │  Estimated cost avoided: $0.04                             │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

### Step 2: เข้าใจ Pricing Model

พี่เจอธิบาย pricing:

```
┌─────────────────────────────────────────────────────────────┐
│  Pricing Table (per 1M tokens)                              │
│                                                             │
│  Provider       Model            Input    Output            │
│  ──────────────────────────────────────────────────────    │
│  Z.AI           glm-5.1          $1.40    $4.40            │
│  Z.AI           glm-5-turbo      $1.20    $4.00            │
│  Z.AI           glm-5            $1.00    $3.20  ← default│
│  Z.AI           glm-4.7          $0.60    $2.20            │
│  Z.AI           glm-4.7-flashx   $0.07    $0.40  ← cheap  │
│  OpenAI         gpt-4o           $2.50   $10.00            │
│  Anthropic      claude-sonnet    $3.00   $15.00            │
│  Anthropic      claude-opus     $15.00   $75.00  ← premium│
│  Gemini         gemini-2.5-pro   $1.25   $10.00            │
│  Gemini         gemini-2.5-flash $0.15    $0.60  ← cheap  │
│                                                             │
│  ตัวอย่าง:                                                   │
│  1,000 requests × glm-5 × avg 400 input + 100 output       │
│  = 400K input + 100K output tokens                          │
│  = ($0.40 + $0.32) = $0.72/day                             │
│                                                             │
│  ถ้าใช้ claude-opus แทน:                                    │
│  = ($6.00 + $7.50) = $13.50/day (18x แพงกว่า!)            │
└─────────────────────────────────────────────────────────────┘
```

### Step 3: เทคนิคเขียน Prompt ให้ได้ประโยชน์จาก Optimizer

พี่เจสอนเทคนิค:

```
┌──────────────────────────────────────────────────────────────────┐
│  เทคนิคเขียน Prompt สำหรับ Optimization                           │
│                                                                  │
│  1. ToolComp ทำงานดีกับ:                                        │
│     - kubectl logs, go test output, JSON configs                 │
│     - อย่าลบ output เอง! ให้ ToolComp compress ให้               │
│     - Example: ส่ง full logs → ToolComp compress 72%            │
│                                                                  │
│  2. Semantic Dedup ชอบ:                                          │
│     - System prompt ที่มี repeated instructions                  │
│     - ไม่ต้องกังวลเรื่องซ้ำ - dedup จัดการให้                     │
│                                                                  │
│  3. Delta Encoding ดีกับ:                                        │
│     - Iterative edit workflows (แก้ไฟล์ซ้ำๆ)                     │
│     - Multi-turn refactoring sessions                            │
│                                                                  │
│  4. Caveman ทำงานได้ดีเมื่อ:                                     │
│     - ต้องการ answer กระชับ ไม่ใช่ essay                         │
│     - Code generation (output เป็น code ไม่ใช่ prose)            │
│     - หลีกเลี่ยงถ้าต้องการ explanation ยาวๆ                      │
│                                                                  │
│  5. เขียน prompt ให้ชัดเจน ไม่ verbose:                          │
│     BAD:  "I would really appreciate it if you could            │
│            please help me understand how to..."                  │
│     GOOD: "explain how to..."                                    │
│     → TextComp จะ clean ให้ แต่ถ้าเราเขียนกระชับตั้งแต่แรก        │
│       ก็ประหยัดได้มากกว่า                                        │
│                                                                  │
│  6. Session management:                                          │
│     - เริ่ม session ใหม่สำหรับ topic ใหม่ (reset context)        │
│     - อย่าใช้ session เดียวทั้งวัน (budget จะเข้า red)           │
│     - Packer จะตัด context เก่า แต่ถ้าเริ new session ได้ดีกว่า   │
└──────────────────────────────────────────────────────────────────┘
```

### Step 4: เปรียบเทียบ Usage กับเพื่อนร่วมทีม

```
┌──────────────────────────────────────────────────────────────┐
│  Team Usage Comparison (This Week)                            │
│                                                              │
│  Member      Requests   Tokens Used   Cost   Savings %      │
│  ──────────────────────────────────────────────────────      │
│  วรรณา        342       169,200      $0.38    35.8%         │
│  พี่เจ         891       485,000      $1.12    42.1%         │
│  อร (Senior)  1,204     612,000      $1.38    48.3%         │
│  ──────────────────────────────────────────────────────      │
│  Team Total   2,437     1,266,200     $2.88    43.7%         │
│                                                              │
│  Note: อร has higher savings because longer sessions          │
│  → optimizers work harder on yellow/red budget                │
└──────────────────────────────────────────────────────────────┘
```

### สรุปวันที่ 4

| หัวข้อ | สิ่งที่เรียนรู้ |
|--------|-----------------|
| Personal usage | Grafana Cost Dashboard แสดง usage ต่อ key/session |
| Cost breakdown | Z.AI glm-5 ถูกสุด ($1/$3.20 per 1M), Claude Opus แพงสุด ($15/$75) |
| Savings breakdown | ToolComp ประหยัดที่สุด (40%), ตามด้วย Caveman (26%), Packer (14%) |
| Prompt techniques | เขียนกระชับ, ใช้ session ใหม่สำหรับ topic ใหม่, ให้ ToolComp จัดการ logs |
| Team comparison | Session ยาว = optimizer ทำงานหนัก = savings % สูงขึ้น |

---

## วันที่ 5: Advanced Usage

### เช้าวันศุกร์

วันสุดท้ายของสัปดาห์ วรรณาเรียนรู้ advanced features

### Step 1: Multi-Provider Routing

วรรณาเรียนรู้ว่า Gateway รองรับ 18 providers:

```
┌──────────────────────────────────────────────────────────────────┐
│  Provider Fallback Chain                                         │
│                                                                  │
│  Request (model: claude-sonnet-4-6)                              │
│    ↓                                                             │
│  Resolver: match model prefix → provider route                   │
│    ↓                                                             │
│  1. claude-oauth  → Anthropic (OAuth token, transparent mode)   │
│    ↓ (fail)                                                      │
│  2. anthropic     → Anthropic (API key)                         │
│    ↓ (fail)                                                      │
│  3. openai        → OpenAI (format conversion)                  │
│    ↓ (fail)                                                      │
│  4. zai           → Z.AI (Anthropic-compatible format)          │
│    ↓ (fail)                                                      │
│  ... 14 more providers in chain                                 │
│                                                                  │
│  Model → Provider mapping:                                      │
│  claude-*   → claude-oauth → anthropic                          │
│  gemini-*   → gemini-oauth → gemini                             │
│  gpt-*/o3-* → openai                                            │
│  glm-*      → zai                                               │
│  or-*       → openrouter                                        │
└──────────────────────────────────────────────────────────────────┘
```

วรรณาลองใช้หลาย model:

```
> /model claude-sonnet-4-6
> review this Go code for race conditions

# Gateway routes to claude-oauth → transparent mode (passthrough)
# No optimization applied (transparent preserves original payload)
# Prompt cache works correctly (cache_control preserved)

> /model glm-5
> อธิบาย Terraform module นี้ให้หน่อย

# Gateway routes to Z.AI
# Full optimization pipeline active
# Budget: green, all green optimizers running
```

**พี่เจ**: "Transparent mode สำคัญมากกับ Claude เพราะถ้า gateway แก้ payload (json.Marshal, system prompt injection) prompt cache จะพัง ทุก request จะเสีย cache_creation_input_tokens ใหม่ทั้งหมด"

### Step 2: ToolFilter Customization

วรรณาใช้ MCP tools หลายตัวจน manifest เกิน 15 tools:

```
┌──────────────────────────────────────────────────────────────┐
│  ToolFilter in Action                                         │
│                                                              │
│  Session has 27 tools loaded:                                │
│  Read, Edit, Write, Bash, WebSearch, analyze_image,          │
│  NotebookEdit, TodoWrite, Skill, mcp_k8s_apply,              │
│  mcp_k8s_logs, mcp_helm_upgrade, mcp_terraform_plan,        │
│  mcp_vault_read, mcp_grafana_query, ...                     │
│                                                              │
│  User message: "debug the pod crash in namespace prod"       │
│                                                              │
│  Intent classification: ACTION (k8s debug task)              │
│                                                              │
│  Tool scoring:                                               │
│  Bash              0.95  ★ always-keep                       │
│  Read              0.90  ★ always-keep                       │
│  mcp_k8s_logs      0.88  ★ intent match: k8s, debug         │
│  Edit              0.75  ★ always-keep                       │
│  Write             0.70  ★ always-keep                       │
│  mcp_k8s_apply     0.65  ★ intent match: k8s                │
│  WebSearch         0.30  ✗ low relevance                     │
│  analyze_image     0.10  ✗ not relevant                      │
│  NotebookEdit      0.05  ✗ not relevant                      │
│  TodoWrite         0.05  ✗ not relevant                      │
│  ...                                                         │
│                                                              │
│  Result: 27 tools → 8 tools kept                             │
│  Tokens saved: ~4,200 tokens (manifest compressed from        │
│  ~8,000 to ~3,800 tokens)                                    │
│                                                              │
│  ALWAYS_KEEP list: Read, Edit, Write, Bash                   │
│  These are NEVER removed regardless of intent                │
└──────────────────────────────────────────────────────────────┘
```

วรรณาลองปรับ ALWAYS_KEEP:

```bash
# ใน .env ของ gateway
TOOLFILTER_ALWAYS_KEEP=Read,Edit,Write,Bash,mcp_k8s_logs
```

```
ตอนนี้ mcp_k8s_logs จะอยู่ใน always-keep list เสมอ
ถึงแม้ intent จะไม่ match ก็ตาม
```

### Step 3: Transparent Mode vs Normal Mode

```
┌──────────────────────────────────────────────────────────────────┐
│  When to Use Each Mode                                           │
│                                                                  │
│  ┌─── Transparent Mode ───────────────────────────────────────┐  │
│  │ Provider: claude-oauth                                      │  │
│  │ Detection: Bearer token + claude model                     │  │
│  │ Pipeline: SKIPPED (raw bytes forwarded)                    │  │
│  │ Benefits: Prompt cache works, beta flags preserved         │  │
│  │ Trade-off: No optimization, no PasteGuard                  │  │
│  │ Use when: Code review, long sessions needing cache         │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─── Normal Mode ────────────────────────────────────────────┐  │
│  │ Provider: zai, openai, gemini, etc.                        │  │
│  │ Pipeline: Full 13-stage optimization + PasteGuard          │  │
│  │ Benefits: 35-75% token savings, secret masking             │  │
│  │ Trade-off: Prompt cache destroyed (payload modified)       │  │
│  │ Use when: Z.AI tasks, cost-sensitive workloads             │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  Decision tree:                                                  │
│                                                                  │
│  ใช้ claude-* model?                                             │
│    YES → Transparent (cache important, cost covered by plan)    │
│    NO → Normal mode (optimize everything, save tokens)          │
│                                                                  │
│  ข้อยกเว้น: ถ้าใช้ arl_ API key → ไม่เข้า transparent            │
│  (เพราะ arl_ key = profile token, ไม่ใช่ OAuth)                 │
└──────────────────────────────────────────────────────────────────┘
```

### Step 4: ให้ Feedback ปรับปรุง Optimizer

วรรณาเจอกรณีที่ optimizer ทำงานไม่ดี:

```
เคส: ส่ง Go struct definition ไป review
ผล: semantic_dedup ลบ field "ID" กับ "Id" เพราะ normalize แล้วเหมือนกัน
     → struct พัง, code ผิด

วรรณาแจ้ง issue:
  "semantic_dedup incorrectly deduplicates Go struct fields
   that differ only in case (ID vs Id). Fields within code
   blocks should be preserved."

พี่เจ check:
  → code blocks (```...```) มี protection อยู่แล้วใน SplitCodeBlocks
  → แต่ถ้า code อยู่ใน plain text (ไม่มี ``` wrapper) จะโดน dedup

Fix:
  → เพิ่ม detection สำหรับ text ที่มี struct/interface pattern
  → หรือ skip dedup ถ้า text มี Go syntax patterns

บันทึกเป็น GitHub issue เพื่อ track
```

### Grafana Dashboard: Weekly Summary

```
┌──────────────────────────────────────────────────────────────────┐
│  วรรณา's First Week Summary                                      │
│                                                                  │
│  ┌─── Activity ───────────────────────────────────────────────┐  │
│  │  Sessions: 12                                              │  │
│  │  Total Requests: 478                                       │  │
│  │  Models Used: glm-5 (68%), glm-5-turbo (18%),             │  │
│  │               claude-sonnet (10%), glm-5.1 (4%)            │  │
│  │  Avg Session Length: 8.3 turns                             │  │
│  │  Longest Session: 22 turns (Wednesday feature dev)        │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─── Optimization Summary ──────────────────────────────────┐  │
│  │                                                             │  │
│  │  Total tokens consumed:    245,600                         │  │
│  │  Tokens saved by optimizer: 98,200 (40.0%)                │  │
│  │                                                             │  │
│  │  Breakdown:                                                │  │
│  │  ToolComp         42,800  (44%)  ████████████████████      │  │
│  │  Caveman          24,500  (25%)  ████████████              │  │
│  │  Packer           12,400  (13%)  ██████                    │  │
│  │  Disclosure        8,900   (9%)  ████                      │  │
│  │  Sketch            5,200   (5%)  ███                       │  │
│  │  Other             4,400   (4%)  ██                        │  │
│  │                                                             │  │
│  │  Waste detected:                                            │  │
│  │  redundant_tool_call:  5 instances                         │  │
│  │  oversized_context:    2 instances                          │  │
│  │  → วรรณา improved prompt habits after seeing waste data    │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─── Cost ───────────────────────────────────────────────────┐  │
│  │  Total cost this week:   $0.54                             │  │
│  │  Without optimizer:      $0.89                             │  │
│  │  Money saved:            $0.35 (39.3%)                     │  │
│  │                                                             │  │
│  │  Cost by day:                                              │  │
│  │  Mon: $0.06  █████ (simple questions)                      │  │
│  │  Tue: $0.12  ██████████ (debugging with toolcomp)         │  │
│  │  Wed: $0.28  ████████████████████████ (long feature dev)  │  │
│  │  Thu: $0.05  ████ (light usage + dashboard review)        │  │
│  │  Fri: $0.03  ███ (learning advanced features)             │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─── PasteGuard ─────────────────────────────────────────────┐  │
│  │  Secrets masked: 3 (API keys accidentally pasted)          │  │
│  │  PII masked: 0                                            │  │
│  │  Scan overhead: <1ms per request                          │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌─── Bandit Learning Progress ───────────────────────────────┐  │
│  │  Arms explored: 8/10                                       │  │
│  │  Best arm for code tasks: textcomp_aggressive (θ=0.35)    │  │
│  │  Best arm for debug: toolcomp (θ=0.42)                    │  │
│  │  Worst arm: summarizer_early (θ=-0.05)                    │  │
│  │  Exploration rate: 15% (still learning)                    │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

### สรุปวันที่ 5

| หัวข้อ | สิ่งที่เรียนรู้ |
|--------|-----------------|
| Multi-provider | 18 providers, resolver match model prefix, fallback chain |
| Transparent mode | Claude OAuth = raw passthrough, preserve cache, skip optimization |
| Normal mode | Z.AI/OpenAI/Gemini = full pipeline, 35-75% savings |
| ToolFilter | ตัด tool manifest จาก 27 → 8 tools, ประหยัด ~4,200 tokens |
| ALWAYS_KEEP | ปรับ list เองได้ (default: Read, Edit, Write, Bash) |
| Feedback loop | แจ้ง issue เมื่อ optimizer ทำงานผิด, track ใน GitHub |

---

## สรุปสัปดาห์แรก: สิ่งที่วรรณาเรียนรู้

```
┌──────────────────────────────────────────────────────────────────┐
│                    WEEK 1 LEARNING PATH                          │
│                                                                  │
│  Day 1: Connection & Basics                                      │
│  ├── Setup Claude Code → Gateway connection                     │
│  ├── Understanding budget levels: green/yellow/red              │
│  ├── First request, green optimizers activated                  │
│  └── Savings: ~17% input, 30% output influence                  │
│                                                                  │
│  Day 2: Real Debugging Task                                      │
│  ├── ToolComp compressed kubectl logs (72% reduction)           │
│  ├── PasteGuard protected accidentally pasted API key           │
│  ├── Grafana dashboard showed per-optimizer metrics             │
│  └── Real savings: 1,468 tokens in a debug session              │
│                                                                  │
│  Day 3: Long Feature Development Session                         │
│  ├── Experienced yellow budget: packer + disclosure activated   │
│  ├── Bandit learned preferences across 20 turns                 │
│  ├── Waste Detection caught redundant file reads                │
│  └── Cumulative: 7,315 tokens saved in one session              │
│                                                                  │
│  Day 4: Cost Awareness                                           │
│  ├── Personal usage on Grafana: $0.38/week                      │
│  ├── Understanding pricing per provider/model                   │
│  ├── Prompt writing tips for better optimization                │
│  └── Team comparison and benchmarks                             │
│                                                                  │
│  Day 5: Advanced Usage                                           │
│  ├── Multi-provider routing and transparent mode                │
│  ├── ToolFilter customization (ALWAYS_KEEP list)                │
│  ├── When to use transparent vs normal mode                     │
│  └── Contributing optimizer feedback                            │
│                                                                  │
│  ──────────────────────────────────────────────────────         │
│  WEEKLY TOTALS:                                                  │
│  Requests: 478 | Cost: $0.54 | Saved: $0.35 (39.3%)           │
│  Tokens saved: 98,200 | Secrets protected: 3                    │
│  Waste detected: 7 instances | Bandit arms explored: 8          │
└──────────────────────────────────────────────────────────────────┘
```

### Quick Reference Card

```
┌─────────────────────────────────────────────────────────────────┐
│  AI Gateway Quick Reference                                      │
│                                                                  │
│  Connection:                                                     │
│    ANTHROPIC_BASE_URL=https://ai.klxhub.com                     │
│    ANTHROPIC_AUTH_TOKEN=arl_<your-key>                           │
│                                                                  │
│  Dashboards:                                                     │
│    Gateway:    https://ai.klxhub.com/                            │
│    Grafana:    https://ai.klxhub.com/grafana                     │
│    Health:     https://ai.klxhub.com/health                      │
│                                                                  │
│  Budget Levels:                                                  │
│    GREEN  (< 50%)  → basic optimizers, caveman lite (30%)       │
│    YELLOW (50-75%) → + packer, disclosure, caveman full (50%)   │
│    RED   (> 75%)   → + summarizer, caveman ultra (75%)          │
│                                                                  │
│  Key Optimizers:                                                 │
│    ToolComp   → compress shell/JSON/log output (40-80%)         │
│    Caveman    → reduce output verbosity (30-75%)                 │
│    Packer     → drop low-utility messages in long sessions      │
│    PasteGuard → mask secrets/PII before sending to provider     │
│    ToolFilter → trim tool manifest (60-80% manifest size)       │
│                                                                  │
│  Tips:                                                           │
│    - New session for new topic (reset context)                   │
│    - Let ToolComp handle large outputs (don't trim yourself)     │
│    - Write concise prompts (TextComp helps but less is more)     │
│    - Check Grafana weekly for usage and waste patterns            │
│    - Claude models → transparent mode (cache preserved)          │
│    - Z.AI models → normal mode (full optimization)               │
└─────────────────────────────────────────────────────────────────┘
```

---

*เอกสารนี้เขียนสำหรับ onboarding สมาชิกใหม่ของ Platform Team - อ้างอิงจาก architecture จริงของ AI Gateway (arl-gateway)*
*Optimizer pipeline reference: `docs/19-optimizer-pipeline-reference.md`*
*Getting started: `docs/01-getting-started.md`*
