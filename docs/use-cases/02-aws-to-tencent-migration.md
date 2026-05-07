# Token Optimizer Pipeline: Use Case จริงในชีวิตวันหนึ่งของสมชาย

> **ผู้ใช้**: สมชาย — Senior DevOps Engineer, ทีม Cloud Platform  
> **งาน**: Migration AWS → Tencent Cloud ด้วย Claude Code ผ่าน API Gateway  
> **Model**: claude-sonnet-4 (200K context window)  
> **วัน**: 7 พฤษภาคม 2026

---

## Phase 1: เช้ามืด — Cold Start (Turn 1-3)

### เวลา 08:00 — เปิด Session ใหม่

สมชายเปิด Claude Code ขึ้นมาเริ่มวันใหม่ Claude สร้าง session ID: `sess-7f3a` และเริ่มต้น optimizer pipeline ทันที

#### F10: Warm Start

Pipeline ตรวจสอบใน Redis หา session เก่าที่คล้ายกัน ใช้ 32-dim feature vector เปรียบเทียบ:

```
feature_vector = [model=sonnet, tool_ratio=0.4, code_ratio=0.6, avg_budget=0.35, ...]
```

พบ session `sess-6e2b` จากเมื่อวาน — เป็นงาน Terraform migration เหมือนกัน:

```
cosine_similarity(feat_sess-7f3a, feat_sess-6e2b) = 0.78
```

ผ่านเกณฑ์ `WARMSTART_MIN_SIMILARITY=0.5` → Warm Start ดึงข้อมูล:
- Delta baseline จาก session เดิม (system prompt cached)
- Tool transition matrix (Read→Edit→Bash pattern)
- Bandit arm weights สำหรับ migration tasks

**ผลลัพธ์**: ลด cold-start waste ไปประมาณ 15% เพราะไม่ต้องเรียนรู้ pattern ใหม่

#### Turn 1: System Prompt Processing

สมชายพิมพ์คำสั่งแรก:

```
"ช่วยอ่านไฟล์ main.tf ของ AWS VPC module แล้วแปลงเป็น Tencent Cloud VPC"
```

**System Prompt** ที่ส่งมาจาก Claude Code: 1,500 ตัวอักษร ประกอบด้วย:
- CLAUDE.md instructions
- Code rules ต่างๆ
- Agent routing table
- Skill definitions

**Budget Level**: GREEN (0% — session เพิ่งเริ่ม)

| Stage | Before (chars) | After (chars) | Saved | ทำอะไร |
|-------|---------------|---------------|-------|---------|
| **F7 Semantic Dedup** | 1,500 | 1,442 | 58 | ตัดประโยคซ้ำ "Be concise" ที่ปรากฏ 3 ครั้ง, "Read files before writing" ซ้ำ 2 ครั้ง (Jaccard threshold 0.7) |
| **F1 Chunker** | 1,442 | 1,442 | 0* | ระบุ stable chunks: Code Rules block (เห็น 2+ ครั้ง), reorder ไว้หน้า → เพิ่ม cache hit rate ไม่ได้ลด chars โดยตรง |
| **F8 Delta** | 1,442 | 1,442 | 0 | Baseline ยังไม่มีใน Redis (session ใหม่) → สร้าง baseline และ cache เป็น `sys:claude-sonnet-4` |
| **F17 TextComp** | 1,442 | 1,315 | 127 | ลบ filler phrases: "You are" → ไม่ต้องการ, "Note that" → ตัด, "IMPORTANT:" → ย่อ, 12 hedge words ถูก strip |
| **F16 Caveman** | 1,315 | 229 | 1,086 | **Lite tier** (green budget): แทนที่ด้วย `[OUTPUT STYLE - lite]` directive 229 ตัวอักษร |

**รวม System Prompt**: 1,500 → 229 chars (**84.7% ลดลง**)

#### F19: ToolFilter

Claude Code ส่งมา 27 tools ทั้งหมด (Read, Write, Edit, Bash, Glob, Grep, NotebookEdit, WebSearch, WebFetch, Skill, ...):

```
Manifest size: ~8,400 chars (~2,100 tokens)
```

Intent classification: **code** (ผู้ใช้ขออ่านและแปลงโค้ด)

ToolFilter เลือก 8 tools ที่เกี่ยวข้อง:
- `Read, Edit, Write, Bash` (always-keep list)
- `Glob, Grep` (keyword overlap: "ไฟล์", "แปลง", "module")
- `Skill` (keyword: "ช่วย")
- `NotebookEdit` (keyword overlap: ไม่เกี่ยว → ตัดออก)

```
Filtered manifest: ~2,200 chars (~550 tokens)
Savings: 6,200 chars (~1,550 tokens)
```

#### Message Processing

ข้อความของสมชาย: "ช่วยอ่านไฟล์ main.tf ของ AWS VPC module แล้วแปลงเป็น Tencent Cloud VPC"

| Stage | Before | After | Saved | ทำอะไร |
|-------|--------|-------|-------|---------|
| Whitespace + Dedup | 98 | 98 | 0 | ไม่มีประโยคซ้ำ |
| TextComp | 98 | 93 | 5 | ตัดคำเชื่อมเล็กน้อย |

#### F20: CompCache

ทุกค่าที่ cache ใน Redis ถูกบีบอัดด้วย zstd level 3:
- Delta baseline: 1,442 → 512 bytes (**64.4% ลดลง**)
- Warm start features: 128 → 89 bytes
- Tool transition matrix: 256 → 134 bytes

#### สรุป Turn 1

```
┌─────────────────────────────────────────────────┐
│  Budget: 0 / 200,000 tokens (GREEN)             │
│  Input sent:    ~850 tokens (จากเดิม ~3,800)    │
│  Output:        ~1,200 tokens                    │
│  Session total: 2,050 tokens                     │
│  Cost this turn: $0.012                          │
│  Savings this turn: ~2,950 input tokens (78%)    │
│  Running savings: 2,950 tokens                   │
└─────────────────────────────────────────────────┘
```

---

#### Turn 2-3: อ่านไฟล์และเริ่มแปลง

สมชายทำงานต่อ Claude ตอบมาพร้อม tool_use (Read) แล้วได้ tool_result เป็นเนื้อหา main.tf

**Turn 2**: Claude เรียก `Read("main.tf")` → tool_result ส่งกลับมา 3,200 chars

**Tool_result processing**:
- **F18 ToolComp**: จับรูป format = HCL/Terraform → compress: ตัด blank lines, ลบ comments ที่ซ้ำ, compact block syntax
  - 3,200 → 1,890 chars (40.9% ลดลง)

**Turn 3**: สมชายพิมพ์ "ดี แปลงต่อให้หมดทั้ง module"

**F9 Sketch**: ตรวจจับ near-duplicate — เทียบกับ prompt ก่อนหน้า:

```
hamming_similarity(sketch_turn3, sketch_turn2) = 0.88 > 0.85 threshold
→ Flagged as near-duplicate (diagnostic, ไม่ได้ตัดแต่บันทึกไว้)
```

**F8 Delta**: ตอนนี้มี baseline แล้วจาก Turn 1:

```
System prompt diff against baseline:
= = = = (17 lines unchanged)
- "Keep solutions simple" (1 line removed)
- "No abstractions for single-use operations" (1 line removed)
+ "Focus on Terraform provider tencentcloud" (1 line added)
= = = (23 lines unchanged)

Delta ops: 41 ops, savings: 22% vs full prompt
1,315 → 1,026 chars
```

#### สรุด Turn 2-3

```
┌─────────────────────────────────────────────────┐
│  Budget: 6,800 / 200,000 tokens (GREEN - 3.4%)  │
│  Cumulative input saved: 4,870 tokens            │
│  Running savings: 4,870 tokens                   │
└─────────────────────────────────────────────────┘
```

---

## Phase 2: กลางเช้า — เพลินงาน (Turn 4-15)

### เวลา 09:30-11:00 — วนลูป Terraform แก้ไข

สมชายเข้าสู่ช่วง workflow ที่ทำซ้ำ: อ่านไฟล์ → แก้ไข → รัน `terraform plan` → แก้ error → รันใหม่

#### F4: Prefetcher เรียนรู้ Pattern

หลังจาก Turn 5, Prefetcher เริ่มจับ pattern ใน Redis (key: `prefetch:sess-7f3a`):

```
Transition matrix (Top-3):
  Read → Edit:    0.42
  Read → Bash:    0.25
  Edit → Bash:    0.67
  Bash → Read:    0.55  (terraform plan fail → อ่านไฟล์ใหม่)
  Bash → Edit:    0.30
```

ตั้งแต่ Turn 8 เป็นต้นไป Prefetcher ทำนายถูก 4 ครั้งจาก 6 → ลด latency เฉลี่ย 120ms/turn

#### Turn 7: Terraform Plan ผิดพลาด — Retry

สมชายรัน `terraform plan` แล้ว error:

```bash
$ terraform plan
│ Error: Invalid attribute on resource "tencentcloud_vpc"
│   on main.tf line 42: cidr_block is not a valid attribute
```

สมชายพิมพ์: "error นี้แก้ยังไง" — **prompt คล้าย Turn 6 มาก**

**F9 Sketch**: ตรวจพบ near-duplicate:

```
hamming_similarity = 0.91 > 0.85
→ ใช้ cached response hint, ลด re-processing ของ system prompt
```

**F8 Delta**: System prompt เปลี่ยนนิดหน่อย (เพิ่ม context จาก tool_result):

```
Delta: 23 unchanged ops + 2 add ops + 0 remove ops
Savings: 31% vs full re-send
1,315 → 907 chars
```

#### Turn 9: ผลลัพธ์ terraform plan ยาวมาก

สมชายรัน `terraform plan` สำเร็จ ผลลัพธ์ยาว 5,000 chars:

```
Terraform will perform the following actions:

  # tencentcloud_vpc.main will be created
  + resource "tencentcloud_vpc" "main" {
      + cidr_block   = "10.0.0.0/16"
      + name         = "production-vpc"
      + ...
    }

  # tencentcloud_subnet.web will be created
  + resource "tencentcloud_subnet" "web" {
      + cidr_block   = "10.0.1.0/24"
      + availability_zone = "ap-bangkok-1"
      + ...
    }
  ... (15 more resources)

Plan: 17 to add, 0 to change, 0 to destroy.
```

**F18 ToolComp**: จับรูป format = Diff/Terraform plan:

| ขั้นตอน | Before | After | ทำอะไร |
|---------|--------|-------|---------|
| รวบรวม changes-only | 5,000 | 2,100 | ตัดส่วน "will be created" ที่ซ้ำ, เก็บเฉพาะ attribute ที่เปลี่ยน |
| Compact format | 2,100 | 800 | ลบ decorative chars, strip separators, head+tail summary |

**5,000 → 800 chars (84% ลดลง)** — ประหยัด ~1,050 tokens

#### Turn 10: วาง AWS Credentials โดยไม่ตั้งใจ

สมชายก๊อปปี้ output จาก terminal แล้ววางใน chat โดยมี AWS credentials ติดมาด้วย:

```
ช่วยตรวจสอบ config นี้:
AWS_ACCESS_KEY_ID=AKIA3EXAMPLE7KEY
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
```

**PasteGuard** (Privacy Mask) ทำงานก่อน optimization:

```
Masked output:
AWS_ACCESS_KEY_ID=__PII_CRED_1__
AWS_SECRET_ACCESS_KEY=__PII_CRED_2__
```

F7 Semantic Dedup ข้ามบรรทัดที่มี `__PII_*__` placeholder ไม่ตัดทิ้ง
F17 TextComp ไม่ย่อบรรทัด masked → ส่งต่อไปให้ Claude เตือนสมชาย

Claude ตอบ: "⚠️ ตรวจพบ credentials ในข้อความ — กรุณาหมุน key ทันที"

**Turn 11**: สมชายขอบคุณและรัน `aws iam delete-access-key` ผ่าน Bash tool

#### Turn 12-15: ทำงานต่อเนื่อง

**Budget status** at Turn 15:

```
┌──────────────────────────────────────────────────────┐
│  Budget: 38,200 / 200,000 tokens (GREEN - 19.1%)     │
│  Prefetcher hit rate: 66% (4/6 correct predictions)   │
│  Delta avg savings: 28% per turn                      │
│  ToolComp total saved: 12,400 chars (~3,100 tokens)   │
│  Cumulative input saved: 18,450 tokens                │
│  Caveman: Lite tier active (30% output reduction)     │
└──────────────────────────────────────────────────────┘
```

**ข้อมูลเพิ่มเติมตอนนี้**:
- **F5 Bandit** เริ่มมีข้อมูลเพียงพอ (10+ requests): พบว่า arm "delta+toolcomp" ให้ reward สูงสุดสำหรับ migration tasks (0.82 avg reward)
- **F14 Cache Eviction**: ยังไม่ทำงาน (5-min interval, ROI ทุก entry ยังสูง)
- **F11 Waste Detection**: วิเคราะห์ทุก 60s — พบ "retry_churn" severity=low (สมชาย retry 3 ครั้งติด), แต่ไม่ถึงเกณฑ์แจ้งเตือน

---

## Phase 3: บ่าย — ขึ้นเขียวเหลือง (Turn 16-25)

### เวลา 13:00-15:00 — งานเริ่มหนัก

สมชายกลับมาจากกินข้าวเที่ยง ทำงานต่อ แต่ session มี context สะสมมากขึ้น:

#### Turn 18: Budget เปลี่ยนเป็น YELLOW

```
Token usage: 104,500 / 200,000 (52.3%) → YELLOW
```

**F16 Caveman อัปเกรด**: Lite → **Full tier** (50% output reduction)

System prompt directive เปลี่ยนจาก:

```
[OUTPUT STYLE - lite] Be concise. Bullet points. Skip filler.
```

เป็น:

```
[OUTPUT STYLE] Be concise. Use bullet points. Skip filler. No explanation unless code is non-obvious. 
Return code first. No inline prose. No boilerplate.
```

ยัง 229 ตัวอักษร แต่ directive เข้มขึ้น → Claude ตอบสั้นลง ~50%

#### Turn 20: kubectl describe ยาวมาก

สมชายรัน `kubectl describe deployment api-gateway -n production` ผลลัพธ์ 8,500 chars:

**F15 Budget-Aware Disclosure** (YELLOW level):

```
Content > 2,000 chars → truncate to L2_TOKENS * 8 = 480 chars

Before: 8,500 chars
After:  480 chars (ตัดเหลือ name, image, replicas, conditions, events ล่าสุด 3 รายการ)

Savings: 94.4%
```

#### Turn 22: เริ่มทำ Kubernetes manifests

สมชายสลับมาทำ K8s manifests สำหรับ deployment บน Tencent Cloud:

**F19 ToolFilter** reclassify:
- Intent: **code** + **action** (terraform apply + kubectl)
- ครั้งนี้เลือก: Read, Edit, Write, Bash + Glob, Grep (หา template files)
- ตัดออก: NotebookEdit, WebSearch, WebFetch (ไม่จำเป็นสำหรับ K8s work)

```
27 tools → 6 tools
Manifest: 8,400 → 1,600 chars (~400 tokens saved)
```

#### Turn 23: อ่านไฟล์ Helm values ยาว

สมชายขออ่าน `values.yaml` ขนาด 12,000 chars:

**F18 ToolComp**: จับรูป format = YAML config:

```
12,000 chars → 4,200 chars (65% ลดลง)
กลยุทธ์: compact YAML (ลบ comments, inline short values, strip defaults)
```

**F15 Budget-Aware Disclosure** (YELLOW):

```
4,200 chars > 2,000 threshold → truncate to 480 chars
เหลือเฉพาะ: image tags, replica counts, resource limits, environment variables
```

Combined: **12,000 → 480 chars (96% ลดลง)**

#### Turn 25: สถานะก่อนเข้า Red

```
┌───────────────────────────────────────────────────────┐
│  Budget: 147,200 / 200,000 tokens (YELLOW - 73.6%)    │
│  Stages active:                                       │
│    F7  Semantic Dedup: 58-127 chars/turn              │
│    F1  Chunker: cache hit rate 34% (stable chunks)    │
│    F8  Delta: 22-35% savings per turn                 │
│    F9  Sketch: 4 near-duplicates flagged              │
│    F17 TextComp: 80-140 chars/turn                    │
│    F16 Caveman: Full tier (50% output reduction)      │
│    F18 ToolComp: avg 65% savings on tool_results      │
│    F19 ToolFilter: avg 1,200 tokens saved/turn        │
│    F15 Disclosure: truncating large outputs            │
│  Cumulative input saved: 42,180 tokens                │
│  Estimated cost saved: $0.38                          │
└───────────────────────────────────────────────────────┘
```

---

## Phase 4: บ่ายโมงยาว — เข้าสีแดง (Turn 26-35)

### เวลา 15:30-17:00 — บ้าระห่ำปิดงาน

#### Turn 26: Budget เปลี่ยนเป็น RED

```
Token usage: 152,000 / 200,000 (76.0%) → RED
```

**Pipeline เปิดใช้ทุก stage ที่มี:**

#### F6: Summarizer — TextRank Extractive Summary

System prompt สะสมมาเป็น 3,800 chars แล้ว (original + context จาก tool_results):

```
SUMMARIZER_METHOD=textrank (default)
SUMMARIZER_MAX_RATIO=0.3
```

TextRank algorithm:
1. แบ่งเป็น 24 ประโยค
2. สร้าง Jaccard similarity graph (24x24 matrix)
3. รัน PageRank 10 iterations, damping=0.85
4. เลือก top-N sentences ใน budget 30% → เลือก 7 ประโยค
5. เรียงลำดับตามตำแหน่งเดิม

```
Before: 3,800 chars (24 sentences)
After:  1,140 chars (7 key sentences)
Saved:  2,660 chars (70% ลดลง)
```

ประโยคที่เลือกเก็บ:
- "You are Claude Code, Anthropic's official CLI for Claude"
- "Read existing files before writing code"
- "Prefer editing over rewriting whole files"
- "Return code first, explanation after only if non-obvious"
- "No error handling for scenarios that cannot happen"
- "Use absolute file paths"
- "Token refresh worker must call refreshAll() immediately on startup"

ประโยคที่ตัด (low PageRank score):
- "Be concise in output but thorough in reasoning" (ซ้ำซ้อนกับอันอื่น)
- "Keep solutions simple and direct" (ซ้ำ)
- "No sycophantic openers or closing fluff" (ไม่สำคัญเท่า)
- "Three similar lines is better than a premature abstraction" (low connectivity)

#### F13: Intent Filter — เอาแค่โค้ด

สมชายถาม: "เขียน Terraform module สำหรับ Tencent Cloud Security Group"

Intent classification: **code** → IntentFilter ตัดส่วนที่ไม่ใช่โค้ดออกจาก response:

```
Before (Claude response): 2,800 chars
  - คำอธิบาย: "Security Group ของ Tencent Cloud จะใช้ tencentcloud_security_group..."
  - โค้ด: ```hcl ... ``` (1,400 chars)
  - หมายเหตุ: "Note that..." (400 chars)

After (IntentFilter): 1,400 chars
  - เหลือเฉพาะ code block

Savings: 50%
```

#### F16: Caveman Ultra Tier

```
[OUTPUT STYLE - ultra]
Return only code. No prose. No comments. No explanation.
If asked for code: return ONLY the code block.
If asked for analysis: return ONLY bullet points with data.
Maximum response: 500 chars unless code requires more.
```

ผล: Claude ตอบสั้นลง ~75% — ให้แค่ code block ไม่มีคำอธิบาย

#### Turn 28: Waste Detection ตรวจจับ

**F11 Waste Detection** รันตรวจทุก 60 วินาที หลัง 28 requests:

```
╔══════════════════════════════════════════════════════════╗
║  WASTE DETECTION REPORT — sess-7f3a                     ║
╠══════════════════════════════════════════════════════════╣
║  retry_churn:    3 retries on terraform plan (Turn 7)    ║
║                  → 2,400 tokens wasted                   ║
║                  severity: MEDIUM                        ║
║                                                          ║
║  redundant_tool_call: 2x Read same file (values.yaml)    ║
║                  → 1,800 tokens wasted                   ║
║                  severity: LOW                           ║
║                                                          ║
║  oversized_context: 152K/200K (76%)                      ║
║                  → recommendation: start new session     ║
║                  severity: HIGH                          ║
╠══════════════════════════════════════════════════════════╣
║  Total waste identified: 4,200 tokens (2.8% of total)   ║
╚══════════════════════════════════════════════════════════╝
```

#### Turn 30: Cache Eviction ทำความสะอาด

**F14 Cache Eviction** รันทุก 5 นาที:

```
Scanning cache:stats:* keys in Redis...

Entry: delta:sys:claude-sonnet-4
  ROI: 8.2 (high) → KEEP

Entry: chunker:stable:code_rules
  ROI: 5.1 (high) → KEEP

Entry: sketch:sess-7f3a:turn_3
  ROI: 0.3 (low - old turn, rarely matched) → EVICT

Entry: prefetcher:sess-6e2b (old session)
  ROI: 0.05 (very low - expired session) → EVICT

Entry: summarizer:cache:a3f2b8...
  ROI: 1.8 (medium) → KEEP

Evicted: 2 entries (bottom 10% by ROI)
Freed: 3.2 KB compressed → ~8 KB uncompressed
Effective cache hit rate improved: 34% → 37%
```

#### Turn 32: F5 Bandit เรียนรู้ Optimal Arms

หลัง 32 requests, LinUCB bandit มีข้อมูลเพียงพอ:

```
BANDIT ARM PERFORMANCE (Top 5):

Arm                | Avg Reward | Pulls | Selection %
-------------------|------------|-------|----------
delta+toolcomp     | 0.82       | 12    | 37.5%
delta+toolcomp+disc| 0.79       | 8     | 25.0%
sketch+delta       | 0.71       | 5     | 15.6%
full_compression   | 0.65       | 4     | 12.5%
passthrough        | 0.20       | 3     | 9.4%

Context features: [budget=0.76, tool_heavy=0.8, code_ratio=0.6, ...]
Best arm for migration tasks: delta+toolcomp (confidence: 0.92)
```

Bandit แนะนำให้ใช้ delta+toolcomp เป็นหลักสำหรับ session แบบนี้ → ปรับ optimization strategy อัตโนมัติ

#### Turn 34: ปิดท้ายด้วย Terraform Apply

สมชายรัน `terraform apply` สำเร็จ:

```bash
Apply complete! Resources: 17 added, 0 changed, 0 destroyed.
Outputs:
  vpc_id = "vpc-xxxxxx"
  subnet_ids = [...]
```

**F18 ToolComp**: จับรูป format = Shell output:

```
Before: 1,200 chars
After:  180 chars (head+tail+summary: "17 added, 0 changed, 0 destroyed" + vpc_id)
Savings: 85%
```

#### Turn 35: สมชายขอสรุปงานที่ทำ

สมชายพิมพ์: "สรุปสิ่งที่ทำวันนี้ให้หน่อย"

**Budget**: 178,500 / 200,000 (89.3%) — RED สุดๆ

Pipeline ทำงานหนักที่สุด:

| Stage | Action | Result |
|-------|--------|--------|
| **F6 Summarizer** | TextRank บีบ system prompt อีกครั้ง | 1,140 → 684 chars (40% ลดลง) |
| **F8 Delta** | Diff encode (เปลี่ยนน้อยมากจาก turn ก่อน) | 31% savings |
| **F13 Intent Filter** | Intent = "analysis" → เก็บ bullet points + file paths | ลบ prose ออก 40% |
| **F16 Caveman** | Ultra tier (75% reduction) | Claude ตอบแค่ bullet list |
| **F15 Disclosure** | Red budget: truncate to L1_TOKENS * 4 = 60 chars สำหรับ tool_result > 1000 chars | บังคับให้ใช้ cached summaries |

---

## Phase 5: เย็น — ปิด Session

### เวลา 17:15 — สมชายปิด Claude Code

#### Final Session Stats

```
╔══════════════════════════════════════════════════════════════════════╗
║                  SESSION SUMMARY — sess-7f3a                       ║
║                  สมชาย: AWS → Tencent Cloud Migration              ║
╠══════════════════════════════════════════════════════════════════════╣
║                                                                    ║
║  Duration:              9 hours 15 minutes                         ║
║  Total turns:           35                                         ║
║  Model:                 claude-sonnet-4 (200K context)              ║
║                                                                    ║
║  ── Token Usage ──────────────────────────────────────────────────  ║
║  Total input tokens:    142,300 (actual sent to provider)           ║
║  Total output tokens:   38,700                                      ║
║  Total tokens:          181,000                                     ║
║  Context peak:          178,500 / 200,000 (89.3%)                   ║
║                                                                    ║
║  ── Without Optimizer (estimated) ────────────────────────────────  ║
║  Estimated raw input:   268,500 tokens                              ║
║  Estimated raw output:  87,400 tokens                               ║
║  Estimated total:       355,900 tokens                              ║
║                                                                    ║
║  ── Optimizer Savings ────────────────────────────────────────────  ║
║  Input tokens saved:    126,200 tokens (47.0%)                      ║
║  Output tokens saved:   48,700 tokens (55.7%)                       ║
║  Total tokens saved:    174,900 tokens (49.1%)                      ║
║                                                                    ║
║  ── Cost Analysis ────────────────────────────────────────────────  ║
║  Actual cost:           $4.72                                       ║
║  Cost without optimizer: $9.28                                      ║
║  Cost saved:             $4.56 (49.1%)                              ║
║                                                                    ║
║  ── Cache State ──────────────────────────────────────────────────  ║
║  Delta baseline:        Cached (1,442 chars, zstd compressed)       ║
║  Chunker stable chunks: 4 entries (Code Rules, Approach, Debug)     ║
║  Prefetcher matrix:     12 transitions learned                      ║
║  Bandit arms:           5 arms, best = delta+toolcomp (0.82)        ║
║  Summarizer cache:      3 TextRank summaries cached                 ║
║                                                                    ║
╚══════════════════════════════════════════════════════════════════════╝
```

---

## ตารางสรุป Savings ตาม Stage

| Stage | ชื่อ | Phase ที่ทำงาน | Total Saved | % ของ Savings | หมายเหตุ |
|-------|------|---------------|-------------|--------------|----------|
| **F10** | Warm Start | Phase 1 | ~2,800 tokens | 1.6% | ใช้ข้อมูล session เดือนเมื่อวาน |
| **F7** | Semantic Dedup | ทุก Phase | 8,400 tokens | 4.8% | ลดประโยคซ้ำใน system prompt |
| **F1** | Chunker | ทุก Phase | 6,200 tokens | 3.5% | Reorder stable chunks → cache hit rate 37% |
| **F8** | Delta Encoding | Phase 2-4 | 22,500 tokens | 12.9% | ใหญ่สุดตอน system prompt คงที่ |
| **F9** | Sketch Near-Dup | Phase 2-3 | 5,100 tokens | 2.9% | Flag 8 near-duplicate prompts |
| **F17** | TextComp | ทุก Phase | 9,800 tokens | 5.6% | Filler + verbose text removal |
| **F19** | ToolFilter | Phase 1-3 | 18,200 tokens | 10.4% | 27→6-8 tools, ~1,200 tokens/turn |
| **F18** | ToolComp | Phase 2-4 | 15,600 tokens | 8.9% | terraform plan/kubectl output |
| **F15** | Disclosure | Phase 3-4 | 12,800 tokens | 7.3% | Truncate จาก yellow/red budget |
| **F6** | Summarizer | Phase 4 | 9,400 tokens | 5.4% | TextRank บีบ system prompt 70% |
| **F13** | Intent Filter | Phase 4 | 7,200 tokens | 4.1% | เอาเฉพาะ code/analysis |
| **F16** | Caveman | ทุก Phase | 48,700 tokens* | 27.8%* | Lite→Full→Ultra tier ลด output |
| **F4** | Prefetcher | Phase 2-4 | (latency) | — | 66% hit rate, ~120ms saved/turn |
| **F5** | Bandit | Phase 2-4 | (indirect) | — | เรียนรู้ delta+toolcomp ดีสุด |
| **F11** | Waste Detection | Phase 4 | (diagnostic) | — | พบ 4,200 tokens waste (2.8%) |
| **F14** | Cache Eviction | Phase 4 | (indirect) | — | Evict 2 low-ROI entries, hit rate +3% |
| **F20** | CompCache | ทุก Phase | (Redis) | — | 60-80% Redis memory saved |
| **F12** | Packer | Phase 3-4 | 4,200 tokens | 2.4% | Activate ตอน yellow budget |

*Caveman savings คือ OUTPUT tokens ที่ลดลง (indirect — model ตอบสั้นลงตาม directive)

---

## ไทม์ไลน์ Budget ตลอดวัน

```
Tokens (K)
200 ┤──────────────────────────────────────────────────────── RED
    │                                              ████
175 ┤                                          ████    ████
    │                                      ████
150 ┤                                  ████             YELLOW
    │                              ████
125 ┤                          ████
    │                      ████
100 ┤                  ████
    │              ████
 75 ┤          ████
    │      ████                         GREEN
 50 ┤  ████
    │██
 25 ┤
    └──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──
      T1 T3 T5 T7 T9 T11 T13 T15 T17 T19 T21 T23 T25 T27 T29 T31 T33 T35
      8:00                10:30       13:00       15:00       17:00

      ▲ Cold Start     ▲ Prefetcher   ▲ YELLOW    ▲ RED      ▲ Peak
        Warm Start       learning       Caveman     Summarizer  89.3%
                                         Full       +Intent
                                                     +Ultra
```

---

## สรุป

สมชายทำงาน migration ตลอดทั้งวัน 35 turns ผ่าน API Gateway pipeline ที่มี 20 optimizer stages:

- **49.1% ของ tokens ถูกประหยัดไป** (174,900 tokens จาก 355,900 ที่ควรใช้)
- **ค่าใช้จ่ายลดลง $4.56** (จาก $9.28 → $4.72)
- **Stage ที่ประหยัดที่สุด**: Caveman (output reduction 48,700 tokens), Delta Encoding (22,500 tokens), ToolFilter (18,200 tokens)
- **Pipeline ปรับตัวอัตโนมัติ**: จาก Lite → Full → Ultra ตาม budget level โดยไม่ต้อง config เพิ่ม
- **Bandit เรียนรู้**: หลัง 10+ requests เลือก delta+toolcomp arm อัตโนมัติสำหรับ migration tasks
- **ความเร็ว**: optimization overhead < 3ms/request — สมชายไม่รู้สึกเลยว่ามี pipeline ทำงานอยู่
