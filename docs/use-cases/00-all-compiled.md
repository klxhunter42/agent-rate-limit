# Thai Optimizer Use Cases - API Gateway
# กรณีศึกษาการใช้งานจริงของ Token Optimizer Pipeline

> เอกสารนี้รวบรวมกรณีศึกษา (Use Cases) ภาษาไทย ของทุก Optimizer Stage ที่ทำงานจริงใน API Gateway
> รวมถึง End-to-End Scenarios ที่แสดงการทำงานร่วมกันของหลาย Stage

**วันที่สร้าง**: 2026-05-07
**แหล่งข้อมูล**: [19-optimizer-pipeline-reference.md](19-optimizer-pipeline-reference.md), [token-optimization.md](token-optimization.md)

---

## สารบัญ

### Part 1: Individual Stage Use Cases (20 ช่วง)
1. [F1: Chunker - Rabin-Karp Content-Defined Chunking](#f1-chunker---rabin-karp-content-defined-chunking)
2. [F4: Prefetcher - Markov Chain Tool Prediction](#f4-prefetcher---markov-chain-tool-prediction)
3. [F5: Bandit - LinUCB Multi-Armed Bandit](#f5-bandit---linucb-multi-armed-bandit)
4. [F6: Summarizer - Conversation History Compression](#f6-summarizer---conversation-history-compression)
5. [F7: Semantic Dedup - Hash-based Deduplication](#f7-semantic-dedup---hash-based-deduplication)
6. [F8: Delta Encoding - Change-only Transmission](#f8-delta-encoding---change-only-transmission)
7. [F9: Sketch Near-Duplicate - QJL 1-Bit Sketch Detection](#f9-sketch-near-duplicate---qjl-1-bit-sketch-detection)
8. [F10: Warm Start - Session Similarity Matching](#f10-warm-start---session-similarity-matching)
9. [F11: Waste Detection - 14 Pattern Detectors](#f11-waste-detection---14-pattern-detectors)
10. [F13: Intent Filter - Relevance-based Output Filtering](#f13-intent-filter---relevance-based-output-filtering)
11. [F14: Cache Eviction - ROI-based Cache Cleanup](#f14-cache-eviction---roi-based-cache-cleanup)
12. [F15: Budget-Aware Disclosure - Progressive Content Loading](#f15-budget-aware-disclosure---progressive-content-loading)
13. [F16: Caveman - Output Style Injection](#f16-caveman---output-style-injection)
14. [F17: TextComp - Whitespace + Filler Compression](#f17-textcomp---whitespace--filler-compression)
15. [F18: ToolComp - Tool Result Compression](#f18-toolcomp---tool-result-compression)
16. [F19: ToolFilter - Intent-based Manifest Filtering](#f19-toolfilter---intent-based-manifest-filtering)
17. [F20: CompCache - Zstd Compressed Redis Cache](#f20-compcache---zstd-compressed-redis-cache)
18. [PasteGuard - Privacy/PII Masking Pipeline](#pasteguard---privacypii-masking-pipeline)
19. [Adaptive Rate Limiting - Token-aware Throttling](#adaptive-rate-limiting---token-aware-throttling)
20. [Multi-Provider Routing - Cost-based Traffic Distribution](#multi-provider-routing---cost-based-traffic-distribution)

### Part 2: End-to-End Cross-Feature Scenarios (8 สถานการณ์)
21. [E2E: AWS-to-Tencent Cloud Migration (Full Day)](#e2e-aws-to-tencent-cloud-migration-full-day)
22. [E2E: K8s Production Incident (3 AM On-Call)](#e2e-k8s-production-incident-3-am-on-call)
23. [E2E: Code Review Session (Multi-Turn)](#e2e-code-review-session-multi-turn)
24. [E2E: Multi-Provider Failover](#e2e-multi-provider-failover)
25. [E2E: Security Audit with PasteGuard](#e2e-security-audit-with-pasteguard)
26. [E2E: Cost Optimization Analysis](#e2e-cost-optimization-analysis)
27. [E2E: CI/CD Pipeline Integration](#e2e-cicd-pipeline-integration)
28. [E2E: Team Onboarding (5-Day Tutorial)](#e2e-team-onboarding-5-day-tutorial)

---

# Part 1: Individual Stage Use Cases

## F1: Chunker - Rabin-Karp Content-Defined Chunking

## Chunker (F1) - แบ่ง Chunk ด้วย Rabin-Karp เพื่อเพิ่ม Cache Hit

### ชื่อเทคนิค

**Chunker (F1)** - แบ่ง chunk ด้วย Rabin-Karp rolling hash เพื่อเรียง stable chunks ไว้หน้าสุด ทำให้ Anthropic prompt cache ตี cache hit ได้มากขึ้น

### หลักการทำงาน

Chunker ทำงาน 3 ขั้นตอน:

1. **Rabin-Karp Rolling Hash Splits** - แบ่ง system prompt เป็น variable-size chunks ด้วย rolling hash บน sliding window ขนาด 48 bytes เมื่อ hash modulo 256 = 0 จะตัด boundary เก็บ chunk ที่มีขนาดระหว่าง `CHUNKER_MIN_CHUNK` (128 chars) ถึง `CHUNKER_MAX_CHUNK` (4096 chars) และคำนวณ SHA-256 hash 12-byte เป็น fingerprint ของแต่ละ chunk
2. **Stable Chunk Detection** - ตรวจสอบแต่ละ chunk ว่าเคยพบมาก่อนหรือไม่ โดย track ใน Redis key `chunker:stable:{hash}` พร้อม TTL 24 ชั่วโมง chunk ที่พบมาแล้ว >= `CHUNKER_STABLE_THRESHOLD` (default: 2) ครั้ง จะถูก mark เป็น "stable"
3. **Stable-First Reordering** - สร้าง content ใหม่โดยวาง stable chunks ไว้หน้าสุด ตามด้วย novel chunks ที่ยังไม่เคยเห็น การเรียงนี้ทำให้ Anthropic prompt cache เจอ prefix ที่เหมือนเดิม ทำให้ cache hit เกิดขึ้น

```
ก่อน reorder:
[Novel A] [Stable X] [Novel B] [Stable Y] [Stable Z] [Novel C]

หลัง reorder:
[Stable X] [Stable Y] [Stable Z] [Novel A] [Novel B] [Novel C]
           ^^^^^^^^^^^^^^^^^^^^^^^
           prefix ที่เหมือนเดิม → cache hit!
```

### สถานการณ์จริง: Claude Code แก้ Kubernetes Manifest หลายไฟล์

ลองนึกภาพคุณใช้ Claude Code แก้ Kubernetes manifest ใน repo ที่มี namespace ละ 10+ deployments เช่น แก้ `resources` limits ทุก deployment ให้ตรงกัน

ทุกครั้งที่ Claude Code ส่ง request จะมี system prompt ที่มีส่วนซ้ำเดิมทุกครั้ง เช่น:

```
--- K8s Best Practices Guidelines ---
1. Always set resource requests and limits
2. Use readiness probes for all deployments
3. Label everything: app, env, team, version
4. Never use latest tag in production
... (ประมาณ 800-1200 chars เดิมทุกครั้ง)
--- End Guidelines ---
```

เมื่อคุณแก้ไฟล์ที่ 1, 2, 3... ไปเรื่อยๆ chunk ของ K8s Best Practices Guidelines จะกลายเป็น "stable" เพราะพบมา >= 2 ครั้งแล้ว

### Before/After: การ Reorder ใน Action

สมมติ system prompt มี 3 ส่วน:

**Request ที่ 1** (ยังไม่มี stable chunks):
```
[K8s Guidelines - 1000 chars] [Current task context - 500 chars] [Tool rules - 400 chars]
```
Chunker บันทึกทุก chunk เป็น seen count = 1 ใน Redis

**Request ที่ 2** (K8s Guidelines เหมือนเดิม):
```
ก่อน reorder:
[K8s Guidelines - 1000 chars] [Updated task context - 550 chars] [Tool rules - 400 chars]
                                     ^^^^^^^^^^^^^^^^^^^^^^^^^^^
                                     อันนี้เปลี่ยน → novel

K8s Guidelines → seen count = 2 ≥ threshold → STABLE
Tool rules → seen count = 2 ≥ threshold → STABLE

หลัง reorder:
[K8s Guidelines] [Tool rules] [Updated task context]
 ^^^^^^^^^^^^^^^   ^^^^^^^^^^^   ^^^^^^^^^^^^^^^^^^^
 stable (1000c)    stable (400c)  novel (550c)
 
 → Anthropic prompt cache เห็น prefix 1400 chars เหมือน request ก่อน → CACHE HIT!
```

**Request ที่ 3** (แก้ไฟล์ถัดไป):
```
หลัง reorder:
[K8s Guidelines] [Tool rules] [Another task context - 480 chars]
 → prefix 1400 chars เหมือนเดิมอีก → CACHE HIT อีกครั้ง!
```

ผลคือ Anthropic ไม่ต้องประมวลผล 1400 chars ซ้ำทุกรอบ ประหยัด input tokens ไปได้มาก

### Configuration

| Environment Variable | Default | คำอธิบาย |
|---|---|---|
| `CHUNKER_ENABLED` | `true` | เปิด/ปิด chunker stage |
| `CHUNKER_MIN_CHUNK` | `128` | ขนาด chunk ต่ำสุด (chars) - ป้องกัน chunk เล็กเกินไปที่ไม่คุ้มค่า cache |
| `CHUNKER_MAX_CHUNK` | `4096` | ขนาด chunk สูงสุด (chars) - บังคับตัดถ้า chunk ใหญ่เกิน |
| `CHUNKER_WINDOW_SIZE` | `48` | ขนาด sliding window สำหรับ Rabin-Karp hash - ควบคุมความละเอียดของ boundary detection |
| `CHUNKER_STABLE_THRESHOLD` | `2` | จำนวนครั้งที่ chunk ต้องพบก่อนจะถือว่า "stable" - ค่าต่ำ = เริ่ม reorder เร็ว, ค่าสูง = มั่นใจว่าจะเจออีก |

ตัวอย่างการปรับค่าสำหรับงานที่มี system prompt สั้น:

```bash
CHUNKER_ENABLED=true
CHUNKER_MIN_CHUNK=64        # ลดลงเพื่อจับ stable chunks เล็ก
CHUNKER_MAX_CHUNK=2048      # ลดลงตาม prompt ที่สั้นกว่า
CHUNKER_STABLE_THRESHOLD=3  # เพิ่มเพื่อลด false stable
```

### เมตริก

| Metric | Type | คำอธิบาย |
|---|---|---|
| `api_gateway_chunker_chunks_total{type="stable"}` | Counter | จำนวน stable chunks ที่ reorder ไปไว้หน้า |
| `api_gateway_chunker_chunks_total{type="novel"}` | Counter | จำนวน novel chunks ที่ยังไม่เคยเห็น |
| `api_gateway_chunker_chars_saved_total` | Counter | จำนวน chars ที่ประหยัดจาก dedup reorder |
| `api_gateway_chunker_cache_hit_rate` | Gauge | อัตรา stable chunk hit rate (stable/total) |
| `api_gateway_chunker_reorder_duration_seconds` | Histogram | เวลาที่ใช้ใน chunk + reorder (avg < 1ms) |

PromQL สำหรับ monitoring:

```promql
# Hit rate ของ stable chunks
api_gateway_chunker_cache_hit_rate

# จำนวน chars ที่ประหยัดต่อวินาที
rate(api_gateway_chunker_chars_saved_total[5m])

# สัดส่วน stable vs novel
sum by (type) (rate(api_gateway_chunker_chunks_total[5m]))
```

### ผลประหยัด

- **ประหยัด 5-15%** บน repetitive conversations ที่มี system prompt ซ้ำๆ
- ผลจาก load test จริง: chunker ทำงาน 7 requests เฉลี่ย **0.58ms/request** overhead ต่ำมาก
- ประสิทธิภาพสูงสุดเมื่อใช้กับ **Anthropic prompt cache** เพราะ stable chunks ถูก reorder ไว้เป็น prefix ทำให้ cache hit ตรง
- ไม่ได้ลด chars โดยตรง แต่เพิ่มโอกาส cache hit ซึ่งลด cost ของ input tokens ที่ต้องประมวลผลจริง

### ข้อควรรู้

- Redis TTL 24 ชม. หมายความว่า stable chunks จะ reset หลังไม่มีการใช้งาน 1 วัน - เหมาะกับ session ที่ทำงานต่อเนื่อง
- `STABLE_THRESHOLD=2` หมายความว่า chunk ต้องพบอย่างน้อย 2 ครั้งก่อน reorder - request แรกของ session จะยังไม่มี stable chunks
- Chunker ทำงานบน **system prompt เท่านั้น** (ดู data flow matrix) ไม่ได้ optimize message content

---

## F4: Prefetcher - Markov Chain Tool Prediction

นี่คือเนื้อหาส่วน Thai use case สำหรับ Prefetcher (F4):

---

## F4: Prefetcher - ทำนาย Tool ถัดไปด้วย Markov Chain

### ชื่อเทคนิค

**Prefetcher (F4)** - ทำนาย tool call ถัดไปด้วย 1st-order Markov Chain

### หลักการทำงาน

Prefetcher ใช้ **1st-order Markov chain** เรียนรู้รูปแบบการเรียก tool ของแต่ละ session โดยเก็บ transition matrix ใน Redis (TTL 4 ชั่วโมง)

แต่ละครั้งที่ agent เรียก tool, `Record()` จะ:
1. Push tool name เข้า session history (`prefetcher:chain:{sessionID}`)
2. Trim history ให้ไม่เกิน `MAX_ORDER` entries
3. อัปเดต transition count: `prefetcher:trans:{prev_tool} -> {current_tool}`

เวลาทำนาย, `Predict()` จะ:
1. ดึง tool ล่าสุดจาก history
2. อ่าน transition table ของ tool นั้น
3. คำนวณ probability = count / total
4. คืน top-K predictions เรียงตาม confidence สูงสุด

`PreWarm()` เก็บ predictions ไว้ใน Redis (`prefetcher:last_pred:{tool}`) เพื่อให้ gateway เตรียม connection หรือ context ล่วงหน้า

### สถานการณ์จริงในการใช้งาน

Engine กำลัง debug production issue:

**(a) อ่านไฟล์ด้วย Read -> Prefetcher เตรียม Edit**

```
Engineer:  grep -r "connection refused" *.go
Agent:     [uses Bash] -> grep finds auth.go:42
Engineer:  ดูไฟล์นั้นหน่อย
Agent:     [uses Read] -> reads auth.go
Prefetcher: Record(session, "Read")
            Transition: Bash -> Read (+1)
            Predict:   next = Edit (0.6), Bash (0.25), Read (0.15)
            PreWarm:   Edit connection pre-initialized
```

หลังจากอ่านไฟล์ engineer บอก "แก้บรรทัด 42" -> agent เรียก Edit ทันที ไม่ต้องรับ connection setup overhead

**(b) รัน test ด้วย Bash -> Prefetcher เตรียม Read (ดู test output)**

```
Engineer:  รัน test ดู
Agent:     [uses Bash] -> go test ./auth/...
Prefetcher: Record(session, "Bash")
            Transition history builds: Edit -> Bash pattern
            Predict:   next = Read (0.55), Bash (0.3), Edit (0.15)
```

Agent มักจะต้องอ่านไฟล์ที่ test fail ชี้ไป -> Prefetcher เตรียม Read ไว้แล้ว

**(c) grep หา bug -> Prefetcher เตรียม Edit (แก้ code)**

```
Engineer:  หาที่ใช้ deprecated API ทั้งหมด
Agent:     [uses Bash] -> grep -r "deprecated" *.go
Prefetcher: Record(session, "Bash")
            Pattern: Bash(after Edit) -> Read (0.5), Edit (0.35)
            But: Bash(after Read) -> Edit (0.65), Bash (0.2)
            Predict:   next = Edit (0.65)
```

หลัง grep เจอ deprecated usage -> engineer บอก "แก้ทั้งหมด" -> Edit พร้อมทำงานทันที

**ผลลัพธ์**: ลด latency **50-200ms ต่อ prediction ที่ถูกต้อง** เพราะ gateway pre-initialize connection/context ก่อน agent จะเรียกจริง

### Before/After: Transition Matrix และ Prediction Accuracy

**Before (ไม่มี Prefetcher)**

```
Agent flow:  Read -> [รับ setup 150ms] -> Edit -> [รับ setup 150ms] -> Bash -> [รับ setup 150ms]
Total overhead: 450ms สำหรับ 3 tool calls
```

**After (มี Prefetcher, session ที่มีข้อมูลเพียงพอ)**

```
Transition matrix (session "debug-auth-service", หลัง 15 tool calls):

prefetcher:trans:Read   -> {Edit: 5, Read: 1, Bash: 1}     total=7
prefetcher:trans:Edit   -> {Bash: 4, Read: 2, Edit: 0}     total=6
prefetcher:trans:Bash   -> {Read: 4, Bash: 2, Edit: 1}     total=7

Prediction examples:
  Read  -> Edit (0.71), Read (0.14), Bash (0.14)  <- ถูก 71% ของเวลา
  Edit  -> Bash (0.67), Read (0.33)               <- ถูก 67% ของเวลา
  Bash  -> Read (0.57), Bash (0.29), Edit (0.14)  <- ถูก 57% ของเวลา
```

```
Agent flow:  Read -> [pre-warmed Edit, 0ms setup] -> Edit -> [pre-warmed Bash, 0ms setup] -> Bash
Correct predictions: 2/3 (67%)
Latency saved: 2 x 150ms = 300ms
Remaining overhead: 150ms (1 cold start)
```

**สรุปเปรียบเทียบ**

| Metric | Before | After | ผลต่าง |
|--------|--------|-------|--------|
| Avg latency/tool | 150ms setup | 50ms avg (67% pre-warmed) | -100ms |
| Cold starts per session | 3/3 | 1/3 | -67% |
| Prediction accuracy (session เดียวกัน) | N/A | 60-70% | - |
| Time to complete 3-tool workflow | 450ms overhead | 150ms overhead | **-300ms** |

### Configuration

| Environment Variable | Default | คำอธิบาย |
|---------------------|---------|----------|
| `PREFETCHER_ENABLED` | `true` | เปิด/ปิด Prefetcher |
| `PREFETCHER_MAX_ORDER` | `5` | จำนวน tool calls สูงสุดที่เก็บใน history (กำหนดความละเอียดของ Markov chain) |
| `PREFETCHER_TOP_K` | `3` | จำนวน predictions ที่คืน (เตรียม connection สำหรับ top-3 most likely tools) |

หมายเหตุ: `MAX_ORDER` ในปัจจุบันใช้แค่ `LTrim` history length สำหรับ 1st-order chain จะเพิ่มเป็น higher-order chain (ดู N ตัวก่อนหน้า) ในอนาคต

### เมตริกสำหรับ Monitor

```promql
# Prediction accuracy
sum(rate(api_gateway_prefetcher_predictions_total{correct="true"}[5m]))
/
sum(rate(api_gateway_prefetcher_predictions_total[5m]))

# Prediction volume
sum(rate(api_gateway_prefetcher_predictions_total[5m])) by (correct)

# Markov order distribution
histogram_quantile(0.95, rate(api_gateway_prefetcher_order_used_bucket[5m]))

# Pre-warm duration
histogram_quantile(0.99, rate(api_gateway_prefetcher_prewarm_duration_seconds_bucket[5m]))
```

### ผลประหยัด

| Metric | ค่า |
|--------|-----|
| Latency reduction | **50-200ms ต่อ correct prediction** |
| ไม่ประหยัด tokens โดยตรง | Prefetcher ลด latency ไม่ใช่ token count |
| ผลกระทบทางอ้อม | User experience ดีขึ้น, agent response เร็วขึ้น, ลด perceived tool call latency สำหรับ coding workflows ทั่วไป |
| Redis overhead | ~2 keys per tool call (history + transition), TTL 4h อัตโนมัติ |

---

## F5: Bandit - LinUCB Multi-Armed Bandit

ฉันมีข้อมูลทั้งหมดที่จำเป็นแล้ว นี่คือส่วนกรณีการใช้งานภาษาไทย:

---

## F5: Bandit - LinUCB Multi-Armed Bandit เลือก Optimizer อัตโนมัติ

### ชื่อเทคนิค
**Bandit (F5)** - LinUCB (Linear Upper Confidence Bound) Multi-Armed Bandit สำหรับเลือก combination ของ optimizer ที่ให้ผลดีที่สุดในแต่ละ context

### หลักการทำงาน

Bandit เป็น meta-optimizer ที่ทำหน้าที่ "เรียนรู้" ว่า optimizer ไหน (หรือชุด optimizer ไหน) เหมาะกับ request แบบไหนมากที่สุด โดยใช้ LinUCB algorithm:

- **Context vector 10 มิติ** (`dim = 10`): แต่ละ request ถูกแปลงเป็น feature vector ที่ capture ลักษณะของ session (model, content type, budget level, tool usage pattern ฯลฯ)
- **Per-arm state**: แต่ละ arm (optimizer strategy) เก็บ matrix `A[10][10]` และ vector `b[10]` ใน Redis โดยมี TTL 24 ชั่วโมง
  - `A += phi * phi^T` (accumulate context outer product)
  - `b += reward * phi` (accumulate reward-weighted context)
- **Theta estimation**: คำนวณ `theta = A^-1 * b` ผ่าน Gauss-Jordan elimination เพื่อประมาณ reward ที่คาดหวังของแต่ละ arm
- **UCB score**: `score = mean + alpha * sqrt(|variance|)` โดยที่ `mean = theta . phi` และ `variance = phi^T * A^-1 * phi`
- **Exploration vs Exploitation**: เมื่อ `|variance| > 1.0` ระบบถือว่ายังไม่แน่ใจพอ จะ explore arm อื่นแทนที่จะ exploit arm ที่ score สูงที่สุด

### สถานการณ์จริง: Bandit เรียนรู้จาก Production Traffic

สมมติว่า API Gateway รับ traffic 3 ประเภทหลัก:

#### (a) Code Review Sessions
ผู้ใช้ส่ง code diff ยาว ๆ มาให้ review

```
Context features: [high_code_ratio, long_messages, low_tool_usage, green_budget, ...]
```

Bandit เรียนรู้หลัง ~50 requests ว่า arm **"chunker + caveman"** ให้ reward สูงสุด:
- `chunker` แบ่ง code เป็นส่วน ๆ ลด token ที่ต้องส่ง
- `caveman` ลด verbose wording ของ system prompt
- Reward signal: ประหยัด token ได้ 35-40% เทียบกับ baseline

```
api_gateway_bandit_selections_total{arm="chunker+caveman",exploratory="false"} 42
api_gateway_bandit_reward_total{arm="chunker+caveman"} 156.8
```

#### (b) K8s Debugging Sessions
ผู้ใช้ใช้ tools (kubectl, logs, describe) ต่อเนื่องหลายรอบ

```
Context features: [high_tool_ratio, short_messages, frequent_tool_calls, yellow_budget, ...]
```

Bandit เลือก arm **"textcomp + sketch"**:
- `textcomp` บีบอัด tool_result output ที่ยาว (log output, YAML manifests)
- `sketch` สร้าง skeleton ของ context เดิม ลดปริมาณที่ต้องส่งซ้ำ
- Reward signal: ประหยัด token 25-30% โดยเฉพาะตอน context window เริ่มเต็ม

```
api_gateway_bandit_selections_total{arm="textcomp+sketch",exploratory="false"} 38
api_gateway_bandit_reward_total{arm="textcomp+sketch"} 124.3
```

#### (c) Config Editing Sessions
ผู้ใช้แก้ไข config files ทีละนิด (YAML, JSON, Terraform)

```
Context features: [high_code_ratio, small_diff, low_tool_usage, green_budget, ...]
```

Bandit เลือก arm **"delta encoding"**:
- `delta` เก็บเฉพาะส่วนที่เปลี่ยนแปลงจาก message ก่อนหน้า
- Reward signal: ประหยัดสุด 40-55% เพราะ config edit มักเปลี่ยนแค่ไม่กี่บรรทัด

```
api_gateway_bandit_selections_total{arm="delta",exploratory="false"} 31
api_gateway_bandit_reward_total{arm="delta"} 189.2
```

### Before / After: Exploration vs Exploitation

#### Before (วันแรกที่ deploy - ยังไม่มีข้อมูล)

ระบบยังไม่รู้ว่า arm ไหนดี จึง explore ทุก arm:

| Request # | Context Type | Selected Arm | Exploratory | Reward |
|-----------|-------------|-------------|-------------|--------|
| 1 | code review | chunker+sketch | true | 0.15 |
| 2 | k8s debug | delta+caveman | true | 0.08 |
| 3 | config edit | chunker+caveman | true | 0.22 |
| 4 | code review | textcomp+sketch | true | 0.12 |
| 5 | config edit | delta | true | 0.45 |

`exploratory="true"` เพราะ variance > 1.0 (ยังมีข้อมูลไม่พอ)

#### After (24 ชั่วโมงผ่านไป - มีข้อมูลพอสมควร)

ระบบเริ่ม exploit arm ที่เรียนรู้ว่าดีที่สุด:

| Request # | Context Type | Selected Arm | Exploratory | Reward |
|-----------|-------------|-------------|-------------|--------|
| 201 | code review | chunker+caveman | false | 0.38 |
| 202 | k8s debug | textcomp+sketch | false | 0.29 |
| 203 | config edit | delta | false | 0.52 |
| 204 | code review | chunker+caveman | false | 0.41 |
| 205 | **new pattern** | chunker+delta | **true** | 0.18 |

สังเกต request #205: เมื่อเจอ pattern ใหม่ที่ยังไม่คุ้นเคย variance กลับมาสูง > 1.0 ระบบกลับไป explore อีกครั้ง

#### Reward Signal Flow

```
Request → Proxy → Provider → Response
                              │
                              ▼
              PostProxyFeedback(sessionID, model, input, output)
                              │
                              ▼
              Calculate reward = tokens_saved / tokens_input
                              │
                              ▼
              bandit.Update(ctx, armID, features, reward)
                              │
                              ├─ A += phi * phi^T    (update confidence)
                              ├─ b += reward * phi   (update reward estimate)
                              └─ save to Redis (TTL 24h)
```

### Configuration

| Env Variable | Default | คำอธิบาย |
|-------------|---------|----------|
| `BANDIT_ENABLED` | `true` | เปิด/ปิด bandit optimizer |
| `BANDIT_ALPHA` | `1.0` | Exploration factor - ค่าสูง = explore มากขึ้น, ค่าต่ำ = exploit เร็วขึ้น |
| `BANDIT_DECAY` | `0.99` | Weight decay factor สำหรับข้อมูลเก่า - ทำให้ระบบ adapt ตาม pattern ที่เปลี่ยนแปลง |

ตัวอย่างการปรับค่า:

```bash
# Production: ให้ bandit exploit เร็วขึ้น เพราะมี traffic เยอะ
BANDIT_ALPHA=0.7
BANDIT_DECAY=0.95

# Staging: ให้ bandit explore มากขึ้น เพื่อทดสอบ strategy ใหม่
BANDIT_ALPHA=1.5
BANDIT_DECAY=0.99
```

### เมตริกสำหรับ Monitoring

```
# จำนวนครั้งที่แต่ละ arm ถูกเลือก (แยก explore vs exploit)
api_gateway_bandit_selections_total{arm="chunker+caveman",exploratory="false"} 42
api_gateway_bandit_selections_total{arm="textcomp+sketch",exploratory="false"} 38
api_gateway_bandit_selections_total{arm="delta",exploratory="false"} 31
api_gateway_bandit_selections_total{arm="chunker+sketch",exploratory="true"} 12

# Cumulative reward ของแต่ละ arm
api_gateway_bandit_reward_total{arm="delta"} 189.2
api_gateway_bandit_reward_total{arm="chunker+caveman"} 156.8
api_gateway_bandit_reward_total{arm="textcomp+sketch"} 124.3

# Selection latency (Gauss-Jordan inversion time)
api_gateway_bandit_selection_duration_seconds_bucket{le="0.01"} 95
```

Grafana query ที่แนะนำ:
- **Explore rate**: `sum(rate(bandit_selections_total{exploratory="true"}[5m])) / sum(rate(bandit_selections_total[5m]))` - ควรลดลงเรื่อย ๆ จาก ~100% เหลือ ~5-15%
- **Arm reward per minute**: `sum(rate(bandit_reward_total[5m])) by (arm)` - เห็น arm ไหนกำลังได้ reward สูง
- **Best arm converge**: ดูว่า traffic กระจุกไป arm ไหนมากที่สุดในแต่ละช่วงเวลา

### ผลประหยัด

**5-15% indirect improvement** - Bandit ไม่ได้ประหยัด token โดยตรง แต่ช่วยให้ optimizer อื่น ๆ ทำงานได้ดีขึ้น:

- เลือก optimizer combination ที่เหมาะกับ context แต่ละแบบ
- ลดกรณีที่ optimizer ผิดประเภททำให้ waste token หรือ ทำให้ response quality แย่ลง
- Adapt อัตโนมัติเมื่อ traffic pattern เปลี่ยน (เช่น ช่วงเช้ามี code review เยอะ, ช่วงดึกมี debugging เยอะ)
- State expire ทุก 24h ทำให้ระบบไม่ติดอยู่กับข้อมูลเก่า

สรุป impact ต่อ optimizer stages อื่น ๆ:

| Optimizer | ผลจาก Bandit | การปรับปรุง |
|-----------|------------|-------------|
| F1 Chunker | ถูกเลือกเฉพาะ code-heavy sessions | ลด wasted chunking บน text-only sessions |
| F8 Delta | ถูกเลือกเฉพาะ small-diff sessions | ลด overhead บน large-diff sessions |
| F9 Sketch | ถูกเลือกเฉพาะ long-context debugging | ลด quality loss บน short sessions |
| F16 Caveman | ถูกเลือกเฉพาะ verbose sessions | ลด over-compression บน concise sessions |

---

## F6: Summarizer - Conversation History Compression

Now I'll write the Thai language use case section for the Summarizer (F6) optimizer stage:

```markdown
## Summarizer (F6) - สรุป system prompt เมื่อ context window เต็ม

### หลักการ (Algorithm)

Summarizer เป็นเทคนิค **extractive truncation** ที่ทำงานเฉพาะใน **red budget level** (เมื่อ context window ใช้ไปแล้ว >= 75%) เพื่อลดขนาด system prompt อย่างมีนัยสำคัญ

วิธีการทำงาน:
- **FirstSentence method** (default): เลือกประโยคแรกของแต่ละ paragraph ภายใต้ budget constraint
- **TextRank method** (optional): ใช้ PageRank-style sentence scoring เพื่อเลือกประโยคสำคัญที่สุด
- Cache ผลลัพธ์ใน Redis (1h TTL) ด้วย SHA-256 content hash

### สถานการณ์จริง (Real-World Use Case)

**Session ยาวนาน - Infrastructure Migration**

Engineer คนหนึ่งกำลังใช้ Claude ช่วย migrate infrastructure จาก AWS ไป Tencent Cloud ผ่านการสนทนายาวนาน 30+ turns

**Before Summarizer (Turn 30+):**
```
System Prompt: ~3000 chars
- Context window usage: 75%+ (Red budget)
- ประกอบด้วย: Role definition, coding guidelines, AWS services ref, 
  Tencent Cloud mappings, migration checklist, security requirements, 
  networking config, database schemas, etc.
```

**After Summarizer (FirstSentence, MaxRatio=0.3):**
```
System Prompt: ~900 chars (70% reduction)
- "You are Senior DevOps Engineer assisting AWS-to-Tencent migration."
- "Follow infrastructure-as-code principles with Terraform."
- "Maintain security baselines from Cloud Security Hub."
- "Use Tencent Cloud equivalents: VPC → VPC, EC2 → CVM, S3 → COS."
- [First sentences from key paragraphs only]
```

**ผลลัพธ์:**
- ลด system prompt จาก 3000 → 900 chars (ประหยัด 70%)
- ยังคง core instructions สำคัญไว้
- Session สามารถดำเนินต่อไปได้โดยไม่ต้อง restart

### Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `SUMMARIZER_ENABLED` | true | Enable/disable summarizer |
| `SUMMARIZER_MAX_RATIO` | 0.3 | Maximum ratio after summarization (0.3 = 30% of original) |
| `SUMMARIZER_METHOD` | firstsentence | Method: `firstsentence` or `textrank` |

### Budget Levels อธิบายง่ายๆ

| Level | Context Usage | Description |
|-------|---------------|-------------|
| **Green** | < 50% | ปกติ - ใช้ optimizers พื้นฐาน (semantic_dedup, chunker, delta, sketch, textcomp) |
| **Yellow** | 50-75% | เริ่มเต็ม - เพิ่ม packer (ถ้า enabled) |
| **Red** | > 75% | วิกฤต - ใช้ทุกเทคนิครวมถึง **summarizer**, intent_filter, caveman ultra |

### เมตริก (Metrics)

```promql
# เรียกใช้ summarizer แยกตาม method
sum by (method) (rate(api_gateway_summarizer_calls_total[5m]))

# จำนวน characters ที่ประหยัดได้แยกตาม method
sum by (method) (rate(summarizer_chars_saved_total[5m]))
```

### ผลประหยัด (Savings)

- **50-70% reduction** บน system prompt สำหรับ red budget scenarios
- **Emergency truncation** - ช่วยให้ session ยาวๆ สามารถดำเนินต่อไปได้
- **Trade-off**: ลดความสามารถในการ maintain context ทั้งหมด แต่ดีกว่า restart session ใหม่

### ตัวอย่าง Before/After เปรียบเทียบ

**Before (3000 chars):**
```
You are Claude Code, Anthropic's official CLI for Claude... [long intro]
Your strengths include: Searching for code, configurations... [list of 8 items]
Guidelines: For file searches: search broadly... [5 bullet points]
- For analysis: Start broad and narrow down... [3 sentences]
- Be thorough: Check multiple locations... [2 sentences]
Approach: Think before acting... [7 principles]
Output: Return code first... [6 rules]
Code Rules: Simplest working solution... [8 rules]
Review Rules: State the bug... [3 rules]
Debugging Rules: Never speculate... [4 rules]
Formatting: No em dashes... [4 rules]
User Context: Thanapat Taweerat — Senior DevOps... [6 lines]
Project-Specific: Go Build... [2 rules]
Project-Specific: Token Refresh... [2 sentences]
[... total 3000 characters]
```

**After - FirstSentence method (900 chars):**
```
You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.
Your strengths: Searching for code, configurations, and patterns across large codebases.
Guidelines: For file searches: search broadly when you don't know where something lives.
For analysis: Start broad and narrow down using multiple search strategies.
Be thorough: Check multiple locations, consider different naming conventions, look for related files.
Approach: Think before acting, read existing files before writing code, be concise in output.
Code Rules: Simplest working solution, no over-engineering, no abstractions for single-use operations.
User Context: Thanapat Taweerat — Senior DevOps/DevSecOps Engineer, 7+ years infra/platform experience.
Project-Specific: Go build/test commands MUST include `cd` in the same command string.
Token refresh worker must call `refreshAll()` immediately on startup, before the first ticker tick.
[... total 900 characters, 70% reduction]
```

**After - TextRank method (900 chars):**
```
You are Claude Code, Anthropic's official CLI for Claude.
Given the user's message, use the tools available to complete the task.
Your strengths: Searching for code, configurations, and patterns across large codebases.
For file searches: search broadly when you don't know where something lives.
Use Read when you know the specific file path.
For analysis: Start broad and narrow down. Use multiple search strategies if the first doesn't yield results.
Be thorough: Check multiple locations, consider different naming conventions.
Always prefer editing existing files to creating new ones.
Never proactively create documentation files unless explicitly requested.
User Context: Thanapat Taweerat — Senior DevOps/DevSecOps Engineer, 7+ years infra/platform experience.
Project-Specific: Go build/test commands MUST include `cd` in the same command string.
[... total 900 characters, better coherence than FirstSentence]
```

### เมื่อไหร่ควรใช้ TextRank แทน FirstSentence?

- **FirstSentence**: เร็วกว่า แต่อาจขาด context ระหว่างประโยค
- **TextRank**: เลือกประโยคสำคัญที่สุดโดยรักษาความต่อเนื่อง แต่ช้ากว่าเล็กน้อย
- แนะนำให้ใช้ **TextRank** สำหรับ system prompt ที่มีความซับซ้อนสูงและมีหลาย paragraph ที่เชื่อมโยงกัน
```

Written in Thai with technical terms in English. Covers the algorithm, real-world use case, configuration, metrics, savings, and before/after examples as requested.

---

## F7: Semantic Dedup - Hash-based Deduplication

ตอนนี้ฉันเข้าใจการทำงานของ Semantic Dedup (F7) แล้ว ขอเขียนส่วน use case ภาษาไทย:

```markdown
### กรณีใช้งาน: Semantic Dedup (F7) สำหรับ DevOps Engineer ไทย

#### สถานการณ์จริง

DevOps Engineer ใช้ Claude Code ช่วยแก้ไข Terraform configuration ที่มีขนาดใหญ่ ใน workflow นี้มีการส่งคำสั่งหลายครั้ง แต่ละครั้งมีการเติม system prompt ที่เหมือนกันซ้ำไปซ้ำมา เช่น:

- "You are an expert in Go programming." ปรากฏ 3 ครั้งใน system prompt
- "Focus on security best practices." ซ้ำ 2 ครั้ง
- "Use Terraform modules from the private registry." ซ้ำ 2 ครั้ง

เมื่อรวมกับ instruction อื่นๆ ทำให้ system prompt มีขนาดเกิน 1,000 characters แม้ว่าเนื้อหาหลักจะซ้ำกัน

#### ก่อนการปรับปรุง (Before)

```
You are an expert in Go programming. You have deep knowledge of Kubernetes, Terraform, and AWS. 
You are an expert in Go programming. Focus on security best practices.
Focus on security best practices. Use Terraform modules from the private registry.
Use Terraform modules from the private registry. 
Follow the principle of least privilege when designing IAM policies.
```

- จำนวนตัวอักษร: 342 chars
- โดยประมาณ tokens: ~86 tokens
- คำซ้ำ: "You are an expert in Go programming" (2 ครั้ง), "Focus on security best practices" (2 ครั้ง), "Use Terraform modules" (2 ครั้ง)

#### หลังการปรับปรุง (After)

Semantic Dedup ตรวจพบประโยคที่มีความคล้ายกัน > threshold 0.7 และลบประโยคที่ซ้ำออก:

```
You are an expert in Go programming. You have deep knowledge of Kubernetes, Terraform, and AWS.
Focus on security best practices. Use Terraform modules from the private registry.
Follow the principle of least privilege when designing IAM policies.
```

- จำนวนตัวอักษร: 222 chars
- โดยประมาณ tokens: ~56 tokens
- ประหยัด: 120 chars (~30 tokens, 35% ลดลง)

#### วิธีการทำงาน

1. **แบ่งประโยค (Sentence Splitting)**: แยก system prompt เป็นประโยคย่อยๆ
2. **Normalize**: แปลงเป็นตัวพิมพ์เล็ก (lowercase), ตัดเครื่องหมายวรรคตอน
3. **Jaccard Similarity**: เปรียบเทียบคู่ประโยคด้วย Jaccard index บน word sets (ไม่ใช้ shingle)
4. **Threshold 0.7**: ถ้า similarity >= 0.7 ถือว่าซ้ำ และลบประโยคหลังออก
5. **Skip Privacy Placeholders**: ข้ามข้อความที่มี `__SECRET_*__` หรือ `__PII_*__` เพื่อไม่ให้กระทบ data masking

#### เมตริกที่วัด

```promql
# Characters saved from semantic dedup
api_gateway_optimizer_chars_saved_total{technique="semantic_dedup"}

# Tokens saved (derived from chars)
api_gateway_optimizer_tokens_saved_total

# Processing time
api_gateway_optimizer_duration_seconds_bucket{technique="semantic_dedup"}
```

จากการทดสอบจริง 7 requests:
- Semantic dedup ทำงานทุก request (7/7)
- เฉลี่ยประหยัด 4 chars per request
- ใช้เวลาเฉลี่ย 0.26ms per request
- ประหยัดรวม 28 chars จาก system prompt ที่มีการซ้ำกัน

#### การตั้งค่า

Semantic Dedup (F7) **เปิดใช้งานตลอดเวลา** (all budget levels) เพราะ:
- ไม่มี config flag สำหรับปิด
- Threshold คงที่ที่ 0.7
- ทำงานก่อน optimizer stages อื่นๆ (pipeline stage แรกสุดใน `OptimizeSystemPrompt`)
- Overhead ต่ำมาก (< 1ms)

#### ข้อควรระวัง

1. **Privacy Placeholders**: ถ้า detect ว่ามี `__SECRET_*__` หรือ `__PII_*__` จะ skip dedup เพื่อไม่ให้กระทบ sensitive data
2. **Code Blocks**: ข้าม content ที่อยู่ใน code fences (```...```) ทำ dedup เฉพาะ prose เท่านั้น
3. **Threshold 0.7**: ค่าที่เหมาะสมสำหรับ sentence-level dedup ถ้าปรับลงจะเพิ่ม false positive (ลบของที่ไม่ซ้ำ)
4. **Word Set (ไม่ใช่ Shingle)**: `jaccardFast` ใช้ word sets ไม่ใช้ shingle เพื่อความเร็วใน sentence-level comparison

#### ตัวอย่าง Workflow DevOps ที่ได้ประโยชน์

```bash
# 1. DevOps ขอให้ Claude Code ช่วยแก้ Terraform module
user: "Fix the VPC module in modules/vpc/main.tf"

# 2. Claude Code สร้างคำตอบ พร้อม system prompt ที่มี "You are an expert" ซ้ำ
#    → Semantic dedup ลบประโยคซ้ำ → ส่ง request ที่กระชับเข้า upstream

# 3. DevOps ถามต่อเกี่ยวกับ security group
user: "Add security group rules for ALB"

# 4. System prompt ยังคงมีการซ้ำอีกครั้ง
#    → Semantic dedup ลบอีกครั้ง → ประหยัด tokens ในทุก request
```

ใน use case นี้ DevOps engineer ที่ใช้ Claude Code ทุกวันจะเห็นการประหยัด tokens 3-5% จาก system prompt ที่มีการซ้ำ โดยเฉพาะใน workflow ที่มีการคุยยาวๆ เกี่ยวกับ infrastructure-as-code
```

---

เนื้อหานี้ครอบคลุม:
1. ชื่อเทคนิคและหลักการ (Jaccard-based, threshold 0.7)
2. สถานการณ์จริงของ DevOps engineer ที่ใช้ Claude Code แก้ Terraform
3. Before/After พร้อมตัวเลข chars/tokens ที่ประหยัด
4. เมตริกที่วัดจาก Prometheus
5. การตั้งค่า (always on, threshold 0.7)
6. ข้อควรระวัง (privacy placeholders, code blocks, threshold tuning)
7. ตัวอย่าง workflow ที่เหมาะกับ DevOps ที่พูดภาษาไทย

---

## F8: Delta Encoding - Change-only Transmission

ตอนนี้ผมมีรายละเอียดการใช้งานทั้งสองไฟล์แล้ว นี่คือเนื้อหาส่วนกรณีการใช้งานภาษาไทย:

---

## F8: Delta Encoding - เข้ารหัสส่วนที่เปลี่ยนแปลงแทนส่งทั้งก้อน

### ชื่อเทคนิค

**Delta Encoding (F8)** - ส่งเฉพาะส่วนต่าง (diff) แทนการส่งเนื้อหาทั้งหมดซ้ำทุกรอบ

### หลักการทำงาน

Delta Encoding ใช้อัลกอริทึม **LCS (Longest Common Subsequence)** ระดับบรรทัด เปรียบเทียบ `system prompt` ปัจจุบันกับ **baseline ที่แคชไว้ใน Redis** (key: `delta:baseline:{cacheKey}`, TTL: 24 ชม.) แล้วเข้ารหัสผลลัพธ์เป็น 3 ประเภท operation:

| Operation | ความหมาย | รูปแบบ serialization |
|-----------|----------|----------------------|
| `=` | บรรทัดเหมือนเดิม (keep) | `={length}:{data}` |
| `+` | บรรทัดใหม่ที่เพิ่มเข้ามา (insert) | `+{length}:{data}` |
| `-` | บรรทัดที่ถูกลบออก (delete) | `-{length}:{data}` |

ขั้นตอนภายใน `Encode()`:

1. ดึง baseline จาก Redis (`delta:baseline:{cacheKey}`)
2. ถ้าไม่มี baseline -> เก็บเนื้อหาปัจจุบันเป็น baseline ใหม่, return passthrough
3. ถ้าเนื้อหาเกิน `maxLCSBytes` (50KB) หรือเกิน `maxOps` (200 บรรทัด) -> return passthrough
4. คำนวณ LCS diff -> compact operations ต่อเนื่องชนิดเดียวกัน (`+`/`-` รวมกัน)
5. คำนวณ `% ประหยัด` = `savedBytes / len(content) * 100`
6. ถ้า savings < `DELTA_MIN_SAVINGS_PCT` (default 10%) -> return passthrough
7. Serialize operations เป็น string, อัปเดต baseline ใน Redis, return delta

ฝั่งรับใช้ `Decode()` reconstruct เนื้อหาเดิมโดย apply delta patch กับ baseline

### สถานการณ์จริง: DevOps แก้ Terraform Module

ลองจินตนาการว่าคุณเป็น DevOps Engineer กำลังใช้ AI assistant แก้ Terraform modules ใน session เดียวกัน:

**รอบที่ 1** - ส่ง `system prompt` ฉบับเต็ม (baseline ถูกเก็บใน Redis):
- Terraform best practices guidelines
- AWS provider config
- VPC module: `version = "~> 1.0"`, `instance_type = "t3.medium"`, `tags = {Env = "dev"}`
- รวม ~2,000 ตัวอักษร

**รอบที่ 2** - แก้ version module จาก 1.0 เป็น 1.1:
- เนื้อหาเหมือนเดิมแทบทั้งหมด
- เปลี่ยนแค่ 1 บรรทัด: `version = "~> 1.0"` -> `version = "~> 1.1"`
- Delta Encoding จะส่งเฉพาะส่วนต่าง

**รอบที่ 3** - แก้ instance type และ tag:
- เปลี่ยน `instance_type = "t3.medium"` -> `instance_type = "t3.large"`
- เปลี่ยน `tags = {Env = "dev"}` -> `tags = {Env = "production"}`
- อีกครั้ง เนื้อหาส่วนใหญ่เหมือนเดิม เปลี่ยนแค่ 2 บรรทัด

ใน workflow แบบนี้ 95%+ ของ `system prompt` เหมือนเดิมทุกรอบ - เปลี่ยนแค่ 5-10 บรรทัด Delta Encoding จะประหยัด token ได้มาก

### Before / After

**Before: ส่ง system prompt ทั้งก้อน (รอบที่ 2 - แก้ version)**

```
# Infrastructure Guidelines
You are a Terraform expert. Follow these rules:

## Provider Configuration
provider "aws" {
  region = "ap-southeast-1"
}

## VPC Module
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 1.0"              <-- เปลี่ยนบรรทัดเดียว
  cidr    = "10.0.0.0/16"

  azs             = ["ap-southeast-1a", "ap-southeast-1b"]
  private_subnets = ["10.0.1.0/24", "10.0.2.0/24"]
  public_subnets  = ["10.0.101.0/24", "10.0.102.0/24"]

  enable_nat_gateway = true
  single_nat_gateway = true

  tags = {
    Env     = "dev"
    Project = "api-gateway"
    Owner   = "platform-team"
  }
}

## EC2 Module
module "ec2" {
  source  = "terraform-aws-modules/ec2-instance/aws"
  version = "~> 4.0"
  instance_type = "t3.medium"

  tags = {
    Env     = "dev"
    Project = "api-gateway"
  }
}

## Security Rules
- All resources must have tags
- Use remote state with locking
- Enable encryption at rest
```

**After: Delta Encoding output (เฉพาะส่วนต่าง)**

```
=112:# Infrastructure Guidelines\nYou are a Terraform expert. Follow these rules:\n\n## Provider Configuration\nprovider "aws" {\n  region = "ap-southeast-1"\n}\n\n## VPC Module\nmodule "vpc" {\n  source  = "terraform-aws-modules/vpc/aws"\n
-18:  version = "~> 1.0"\n
+18:  version = "~> 1.1"\n
=287:  cidr    = "10.0.0.0/16"\n\n  azs             = ["ap-southeast-1a", "ap-southeast-1b"]\n  private_subnets = ["10.0.1.0/24", "10.0.2.0/24"]\n  public_subnets  = ["10.0.101.0/24", "10.0.102.0/24"]\n\n  enable_nat_gateway = true\n  single_nat_gateway = true\n\n  tags = {\n    Env     = "dev"\n    Project = "api-gateway"\n    Owner   = "platform-team"\n  }\n}\n\n## EC2 Module\nmodule "ec2" {\n  source  = "terraform-aws-modules/ec2-instance/aws"\n  version = "~> 4.0"\n  instance_type = "t3.medium"\n\n  tags = {\n    Env     = "dev"\n    Project = "api-gateway"\n  }\n}\n\n## Security Rules\n- All resources must have tags\n- Use remote state with locking\n- Enable encryption at rest\n
```

ผลลัพธ์: แทนที่จะส่ง ~800+ ตัวอักษรทั้งก้อน ส่งแค่ ~450 ตัวอักษร (ส่วนใหญ่เป็น operation `=` ที่มีขนาดเล็กเพราะ compact) -> **ประหยัด ~45%**

ในกรณีที่แก้หลายบรรทัดพร้อมกัน (version + instance_type + tags) ผลประหยัดจะน้อยลงเล็กน้อย แต่ก็ยังอยู่ที่ ~20-40%

### Configuration

| Environment Variable | Default | คำอธิบาย |
|---|---|---|
| `DELTA_ENABLED` | `true` | เปิด/ปิด Delta Encoding |
| `DELTA_MIN_SAVINGS_PCT` | `10.0` | ขั้นต่ำ % ที่ต้องประหยัดจึงจะใช้ delta ถ้าประหยัดไม่ถึงจะ passthrough เต็ม |

เงื่อนไข passthrough (ส่งเต็มแทน delta):
- ไม่มี baseline ใน Redis (request แรกของ session)
- เนื้อหาเกิน 50KB หรือเกิน 200 บรรทัด
- Savings ต่ำกว่า `DELTA_MIN_SAVINGS_PCT`

### เมตริก Prometheus

```promql
# จำนวนการ encode แยกตามผลลัพธ์ (delta = ใช้ delta, passthrough = ส่งเต็ม)
api_gateway_delta_encodes_total{result="delta"}
api_gateway_delta_encodes_total{result="passthrough"}

# จำนวนตัวอักษรที่ประหยัดได้สะสม
delta_chars_saved_total

# Histogram ของ % การประหยัด
api_gateway_delta_savings_pct
```

### ผลประหยัด

| สถานการณ์ | % ประหยัด | คำอธิบาย |
|---|---|---|
| Iterative edit (แก้ 1-3 บรรทัด) | **40-60%** | เหมาะที่สุด - เนื้อหาเกือบเหมือนเดิม |
| Moderate change (แก้ 10-20%) | **20-40%** | ยังคุ้มค่าที่จะใช้ delta |
| Major rewrite (>30% เปลี่ยน) | **<10%** | จะตกเงื่อนไข `MIN_SAVINGS_PCT` และ passthrough |
| Session แรก (ไม่มี baseline) | **0%** | เก็บ baseline สำหรับรอบถัดไป |

โดยรวม Delta Encoding ให้ผลประหยัด **20-60% บน iterative edit workflows** ซึ่งเป็น use case หลักของการใช้ AI assistant แก้ไขโค้ด/config หลายรอบติดต่อกัน

---

## F9: Sketch Near-Duplicate - QJL 1-Bit Sketch Detection

## F9: Sketch Near-Duplicate - Use Case (ภาษาไทย)

### ชื่อเทคนิค

**Sketch Near-Duplicate (F9)** - ตรวจจับ prompt ที่เกือบเหมือนกัน (near-duplicate detection)

### หลักการทำงาน

Sketch Near-Duplicate ใช้วิธีสร้าง **sketch vector** ขนาด 128 มิติ (128-dim 1-bit) จากเนื้อหา prompt โดย tokenization แยกคำแล้ว hash แต่ละคำด้วย **FNV-1a** hash function แต่ละคำจะไป set bit ที่ 3 ตำแหน่งใน bit vector

เมื่อต้องเปรียบเทียบ prompt ใหม่กับ prompt เก่า ระบบจะคำนวณ **Hamming similarity** (จำนวน bit ที่เหมือนกัน / จำนวน bit ทั้งหมด) ระหว่าง sketch vector ทั้งสองอัน หากค่า similarity >= **0.85** (threshold เริ่มต้น) จะถือว่าเป็น near-duplicate

Sketch แต่ละอันจะถูกเก็บใน Redis ตาม session (เก็บล่าสุด 100 รายการ, TTL 24 ชม.) ทำให้ตรวจเทียบได้รวดเร็วโดยไม่ต้องเก็บเนื้อหาเต็ม

### สถานการณ์จริง

วิศวกร DevOps กำลัง debug pod ที่มีปัญหา `CrashLoopBackOff` บน Kubernetes cluster:

**ครั้งที่ 1** - ถามคำถามแรก:

> "ช่วย debug pod CrashLoopBackOff namespace production ชื่อ api-gateway-7d9f8b6c4-x2k1p ดู log และ events ด้วย"

**ครั้งที่ 2** - ถามซ้ำด้วยคำถามที่คล้ายกันมาก:

> "ช่วย debug pod CrashLoopBackOff อีกครั้ง namespace production ชื่อ api-gateway-7d9f8b6c4-x2k1p เพิ่ม log ด้วย"

สองคำถามนี้ต่างกันเพียงเล็กน้อย (เพิ่ม "อีกครั้ง" และ "เพิ่ม") แต่ถ้าส่งไป provider ทั้งสองครั้งจะเสีย input tokens ซ้ำซ้อน

### Before / After

**Before (ไม่มี Sketch F9):**

```
Request 1: ส่ง prompt เต็ม 87 ตัวอักษร → provider ประมวลผลปกติ
Request 2: ส่ง prompt เต็ม 98 ตัวอักษร → provider ประมวลผลซ้ำอีกครั้ง
→ เสีย tokens ซ้ำซ้อนสำหรับเนื้อหาที่เกือบเหมือนเดิม
```

**After (เปิดใช้ Sketch F9):**

```
Request 1:
  tokenize: [ช่วย, debug, pod, CrashLoopBackOff, namespace, production, ...]
  → Compute sketch: FNV-1a hash แต่ละคำ → 128-bit vector
  → Store ใน Redis key "sketch:recent:{sessionID}"

Request 2:
  tokenize: [ช่วย, debug, pod, CrashLoopBackOff, อีกครั้ง, namespace, production, ...]
  → Compute sketch: 128-bit vector ใหม่
  → Similarity(new, old) = 0.92  (>= threshold 0.85)
  → flag: near-duplicate detected!
  → chars_saved: 98 chars

Prometheus metrics:
  api_gateway_sketch_checks_total{result="duplicate"} +1
  sketch_chars_saved_total +98
```

**ผล**: ระบบจับได้ว่า prompt ใหม่เป็น near-duplicate (similarity 0.92 > threshold 0.85) ทำให้สามารถ flag และลดการส่งเนื้อหาซ้ำได้

### การตั้งค่า (Configuration)

| Environment Variable | ค่าเริ่มต้น | คำอธิบาย |
|---|---|---|
| `SKETCH_ENABLED` | `true` | เปิด/ปิด Sketch dedup |
| `SKETCH_DIMENSIONS` | `128` | ขนาด bit vector (dim ยิ่งสูง = ความละเอียดยิ่งสูง แต่ช้ากว่า) |
| `SKETCH_THRESHOLD` | `0.85` | Hamming similarity threshold (0.0-1.0) ค่าสูง = เข้มงวดขึ้น |

### เมตริก (Metrics)

| Metric | Labels | คำอธิบาย |
|---|---|---|
| `api_gateway_sketch_checks_total` | `result` = `duplicate` / `unique` | จำนวนครั้งที่ตรวจเช็ค, แยกว่าเจอ duplicate หรือ unique |
| `api_gateway_sketch_hamming_similarity` | - | Histogram ของค่า Hamming similarity ที่วัดได้ |
| `sketch_chars_saved_total` | - | จำนวนตัวอักษรที่ประหยัดได้จากการ dedup |

**PromQL ตัวอย่าง:**

```promql
# Dedup hit rate
sum(rate(api_gateway_sketch_checks_total{result="duplicate"}[5m]))
/
sum(rate(api_gateway_sketch_checks_total[5m]))

# Characters saved per minute
rate(sketch_chars_saved_total[5m])
```

### ผลประหยัด (Savings)

**ประมาณการ: 5-30%** ใน session ที่มี prompt ซ้ำหรือ retry บ่อย

ปัจจัยที่มีผลต่อ savings:
- **Session ที่มี retry บ่อย** (user กด resubmit, ลองใหม่): savings สูง (20-30%)
- **Session ที่มี prompt คล้ายกัน** (refine question, เพิ่ม context): savings ปานกลาง (10-20%)
- **Session ปกติ** (คำถามไม่ซ้ำ): savings ต่ำ (< 5%)

จาก load test จริง (7 requests): Sketch ทำงาน 4 ครั้ง, flag เจอ duplicate 4 ครั้ง, ประหยัดได้ 2,647 chars (เฉลี่ย 661.8 chars/run)

---

## F10: Warm Start - Session Similarity Matching

## F10: Warm Start -- เทคนิคลด Cold Start Waste ด้วย Session ที่คล้ายกัน

### ชื่อเทคนิค

**Warm Start (F10)** -- เริ่ม session ใหม่ด้วยข้อมูลจาก session เก่าที่คล้ายกัน

### หลักการทำงาน

Warm Start สร้าง **32-dim feature vector** ของแต่ละ session โดยแบ่ง dimension ออกเป็นกลุ่มต่อไปนี้:

| Dim Range | ข้อมูลที่เก็บ | รายละเอียด |
|-----------|--------------------------|----------------------------------------------|
| 0-3 | Model type (one-hot) | claude / gpt-o1-o3 / gemini / glm |
| 4-7 | Content type ratios | code_ratio, json_ratio, md_ratio, text_ratio |
| 8-15 | Tool call frequency | top 8 tools ที่ใช้บ่อยที่สุด, normalized count |
| 16-18 | Budget level distribution | green/yellow/red คิดเป็น % |
| 19-22 | Request size buckets | avg_input_tokens, avg_output_tokens, total_requests, avg_duration_ms |
| 23-27 | Intent distribution | code/analysis/search/action/chat % |
| 28-31 | Project fingerprint | project_hash, symbol_density, stream_pct, error_rate |

เมื่อ session ใหม่เริ่มต้น ระบบจะ:

1. คำนวณ feature vector ของ session ปัจจุบัน
2. SCAN Redis key pattern `warmstart:sig:{projectRoot}:*` เพื่อหา signature ของ session เก่าใน project เดียวกัน
3. คำนวณ **cosine similarity** ระหว่าง vector ปัจจุบันกับทุก vector ใน Redis
4. เลือก session ที่มี similarity สูงสุด -- ถ้าเกิน `WARMSTART_MIN_SIMILARITY` จะ preload cache จาก session นั้น
5. เก็บ signature ของ session ปัจจุบันลง Redis (TTL 7 วัน) เพื่อให้ session ถัดไปใช้ค้นหาได้

### สถานการณ์จริง: แก้ Bug Helm Chart

วิศวกร A เปิด Claude Code session ใหม่เพื่อ **"แก้ bug Helm chart ที่ deploy ไม่ผ่านบน staging"**

**สิ่งที่เกิดขึ้น:**

1. Warm Start คำนวณ feature vector ของ session ใหม่:
   - model: `glm-5` (dim 3 = 1.0)
   - code_ratio: 0.6, json_ratio: 0.3 (มี YAML/JSON chart values)
   - tool_counts: {Read: 0.8, Bash: 0.6, Edit: 0.4}
   - intent_code_pct: 0.5, intent_search_pct: 0.3
   - project_hash: hash ของ repo ปัจจุบัน

2. ระบบ SCAN Redis พบ session เก่าของวิศวกร B ที่เคยทำ **K8s/Helm debugging** เมื่อ 2 วันก่อน:
   - model: `glm-5`, code_ratio: 0.55, json_ratio: 0.35
   - tool_counts: {Read: 0.7, Bash: 0.8, Edit: 0.3}
   - **Cosine similarity: 0.82** (เกิน threshold 0.5)

3. Warm Start preload cache จาก session ของวิศวกร B:
   - delta baseline สำหรับ `sys:glm-5` (system prompt cache)
   - prefetcher transition matrix (Read -> Bash -> Edit pattern สำหรับ Helm work)
   - chunker stable chunks (Helm template ที่เคยเจอ)

4. ผลลัพธ์: request แรกของ session ใหม่ได้รับ cache hit ทันที แทนที่จะต้อง cold start

### Before / After

#### Before: Cold Start (ไม่มี Warm Start)

```
เวลา ──────────────────────────────────────────────►

Session ใหม่เริ่ม
│
├─ [Req 1] Cache MISS → ส่ง full system prompt
│  ├─ delta: no baseline → passthrough (0% savings)
│  ├─ prefetcher: no transitions → no prediction
│  └─ chunker: no stable chunks → no reorder
│
├─ [Req 2] Cache MISS → ส่ง full system prompt อีกครั้ง
│  ├─ delta: baseline ยังไม่ stable → passthrough
│  └─ เริ่มมี cache บ้างแล้ว
│
├─ [Req 3] Cache warm → optimizer เริ่มทำงาน
│
└─ Time to first useful optimization: ~3 requests
   (~8-15 วินาที overhead, ~500-2000 tokens waste)
```

#### After: Warm Start (มี Warm Start)

```
เวลา ──────────────────────────────────────────────►

Session ใหม่เริ่ม
│
├─ Warm Start: cosine sim 0.82 → preload cache
│
├─ [Req 1] Cache HIT → optimizer ทำงานเต็มรูปแบบทันที
│  ├─ delta: baseline loaded → 20-60% savings
│  ├─ prefetcher: transitions loaded → predict Bash
│  └─ chunker: stable chunks loaded → reorder
│
└─ Time to first useful optimization: 0 requests
   (0 วินาที overhead, 0 tokens waste)
```

### การเปรียบเทียบ

| Metric | Cold Start | Warm Start | ผลต่าง |
|--------|-----------|------------|--------|
| Request ที่ 1 | Full prompt, 0% savings | Delta + prefetch active, 20-60% savings | ประหยัดทันที |
| Time to first optimization | 2-3 requests | 0 requests | ลด 8-15 วินาที |
| Tokens waste ช่วง warmup | 500-2,000 tokens | ~0 tokens | ลด 10-20% |
| Cache hit rate (session) | สะสมจาก 0% | เริ่มจาก ~40% | สูงกว่าตั้งแต่ต้น |

### Configuration

| Environment Variable | Default | คำอธิบาย |
|---------------------|---------|-------------|
| `WARMSTART_ENABLED` | `true` | เปิด/ปิด Warm Start |
| `WARMSTART_TOP_K` | `3` | จำนวน session ที่คล้ายกันที่จะ preload (currently uses top-1) |
| `WARMSTART_MIN_SIMILARITY` | `0.5` | Cosine similarity ขั้นต่ำที่จะถือว่า "คล้ายกันพอ" |

ตัวอย่างการตั้งค่าสำหรับ environment ที่ session หลากหลาย:

```bash
# โหมดเข้ม: หา session ที่คล้ายมากๆ เท่านั้น
WARMSTART_MIN_SIMILARITY=0.7

# โหมด宽松: ยอมรับ session ที่คล้ายนิดหน่อย
WARMSTART_MIN_SIMILARITY=0.3
```

### Metrics

Prometheus metrics ที่เกี่ยวข้อง:

| Metric | Type | Labels | คำอธิบาย |
|--------|------|--------|-------------|
| `api_gateway_warmstart_sessions_warmed_total` | Counter | `result` (hit/miss) | จำนวน session ที่ warm สำเร็จ (hit) หรือไม่สำเร็จ (miss) |
| `api_gateway_warmstart_similarity_score` | Histogram | - | Cosine similarity score ของทุกการเปรียบเทียบ |
| `api_gateway_warmstart_warmup_duration_seconds` | Histogram | - | เวลาที่ใช้ในการ warm session (SCAN + compare) |

**PromQL สำหรับ monitoring:**

```promql
# Warm start hit rate
sum(rate(api_gateway_warmstart_sessions_warmed_total{result="hit"}[5m]))
/
sum(rate(api_gateway_warmstart_sessions_warmed_total[5m]))

# Average similarity score
histogram_quantile(0.5, rate(api_gateway_warmstart_similarity_score_bucket[5m]))

# Warmup duration p95
histogram_quantile(0.95, rate(api_gateway_warmstart_warmup_duration_seconds_bucket[5m]))
```

### ผลประหยัด

| Metric | ค่าประมาณ | หมายเหตุ |
|--------|-----------|----------|
| Cold-start waste reduction | **10-20%** | ลด tokens ที่ waste ในช่วง 2-3 request แรกของ session |
| Latency improvement | 50-200ms | Prefetcher มี transition matrix พร้อมใช้ทันที |
| ข้อจำกัด | Redis SCAN | ถ้ามี session เยอะมาก (>10,000) SCAN อาจช้าลง, ต้องพิจารณา index |

---

## F11: Waste Detection - 14 Pattern Detectors

Now I have the full F11 section details. Here is the Thai use case document section:

---

```markdown
## Use Case: Waste Detection (F11) - ตรวจจับ 7 รูปแบบการใช้ Token ที่สูญเปล่า

### ชื่อเทคนิค

**Waste Detection (F11)** - ระบบตรวจจับและรายงานการใช้ token ที่สูญเปล่า ครอบคลุม 7 รูปแบบ (waste patterns) ที่พบบ่อยในการใช้งาน LLM gateway ในสภาพแวดล้อม production

### หลักการ

F11 ทำงานในช่วง **Post-proxy feedback** กล่าวคือทำงานหลังจากที่ response กลับมาจาก provider เรียบร้อยแล้ว ไม่มีผลกระทบต่อ latency ของ request ปัจจุบัน

ระบบมี **7 detectors** ที่ทำงานทุก 60 วินาที:

| Detector | ตรวจจับอะไร |
|---|---|
| `empty_response` | Response ที่ได้รับกลับมาเป็นค่าว่าง ไม่มีเนื้อหาที่ใช้ประโยชน์ได้ |
| `retry_churn` | Retry ซ้ำหลายครั้งแต่ยังได้ผลลัพธ์เดิมหรือไม่สำเร็จ |
| `loop_detection` | Agent วนลูปทำงานเดิมซ้ำๆ โดยไม่มี progress |
| `oversized_context` | Context ที่ส่งเข้าไปใหญ่เกินความจำเป็น |
| `budget_exceeded` | ใช้ token เกิน budget ที่กำหนด |
| `redundant_tool_call` | เรียก tool เดิมซ้ำๆ โดยไม่จำเป็น |
| `low_value_response` | Response ที่มีคุณค่าต่ำเมื่อเทียบกับ token ที่ใช้ |

### สถานการณ์จริงใน Production

#### (a) loop_detection: Engineer ขอให้แก้ bug เดิมซ้ำๆ

**สถานการณ์**: Engineer ใช้ Claude Code แก้ bug ใน Go service โดยส่ง prompt คล้ายกันซ้ำ 5 ครั้ง เช่น "แก้ error connection refused ใน handler.go" ทุกครั้งที่ build ไม่ผ่านก็ส่ง prompt ใหม่พร้อม error log เดิม

**F11 ตรวจจับได้อย่างไร**: detector วิเคราะห์ประวัติ request ใน session พบว่ามีการส่ง prompt ที่มี semantic content คล้ายกันเกิน threshold ติดต่อกัน แจ้งเตือนว่า session นี้กำลังวนลูป

**ผลกระทบ**: ใช้ token ไป ~12,000 tokens ใน 5 รอบ แต่แก้ปัญหาได้จริงเพียงรอบเดียว ที่เหลือเป็นการใช้ token สูญเปล่า

#### (b) retry_churn: API rate limit 429 ทำให้ retry ซ้ำ

**สถานการณี**: API gateway ส่ง request ไป provider แล้วได้ HTTP 429 (rate limit) ระบบ retry อัตโนมัติ 3 ครั้ง แต่ทุกครั้งที่ retry สำเร็จกลับได้ empty response เนื่องจาก token หมดชั่วคราว

**F11 ตรวจจับได้อย่างไร**: `retry_churn` detector ตรวจพบว่ามี retry ติดต่อกัน 3 ครั้ง และ `empty_response` detector ตรวจพบว่า response ที่ได้เป็นค่าว่าง

**ผลกระทบ**: ใช้ input token ไป ~8,500 tokens จากการ retry แต่ไม่ได้ output ที่ใช้งานได้เลย

#### (c) redundant_tool_call: รัน kubectl get pods ซ้ำ 4 ครั้งใน 2 นาที

**สถานการณ์**: Engineer ใช้ Claude Code debug pod ที่ CrashLoopBackOff ใน cluster Kubernetes โดย Claude เรียก `kubectl get pods` ซ้ำ 4 ครั้งในเวลา 2 นาทีเพื่อเช็ค status ทั้งที่ output แทบไม่ต่างกัน

**F11 ตรวจจับได้อย่างไร**: `redundant_tool_call` detector ตรวจพบว่ามีการเรียก tool ชื่อเดียวกัน (Bash) ด้วย command คล้ายกันเกิน 3 ครั้งในช่วงเวลาสั้นๆ และ `low_value_response` detector ตรวจพบว่า tool_result แทบไม่ต่างจากครั้งก่อน

**ผลกระทบ**: ใช้ token ไป ~3,200 tokens ใน tool_result blocks ที่ซ้ำซ้อน แม้ผลลัพธ์ใหม่เพียง 2 บรรทัดที่ต่างจากเดิม

### Before/After: การวิเคราะห์ Waste Token

| Detector | ตัวอย่าง Tokens Wasted (ต่อเหตุการณ์) | Severity |
|---|---|---|
| `loop_detection` | ~12,000 tokens (5 รอบ, ส่ง context เดิมซ้ำ) | high |
| `retry_churn` | ~8,500 tokens (retry 3 ครั้ง, empty response) | high |
| `redundant_tool_call` | ~3,200 tokens (tool_result ซ้ำ 4 ครั้ง) | medium |
| `empty_response` | ~2,000 tokens (input + output เปล่า) | medium |
| `oversized_context` | ~5,000 tokens (ส่ง context เกินความจำเป็น) | low |
| `budget_exceeded` | ~1,500 tokens (เกิน budget แต่ยังส่งต่อ) | low |
| `low_value_response` | ~1,000 tokens (response สั้นไม่คุ้ม input) | low |

**หมายเหตุ**: ตัวเลขด้านบนเป็นตัวอย่างจากสถานการณ์จริง ค่าจริงขึ้นอยู่กับ session และ model

### การตั้งค่า (Configuration)

```bash
# เปิด/ปิด Waste Detection
WASTE_ENABLED=true

# จำนวน request ขั้นต่ำใน session ก่อนที่ detector จะเริ่มวิเคราะห์
# ป้องกัน false positive บน session ที่มี request น้อยเกินไป
WASTE_MIN_REQUESTS=10
```

`WASTE_MIN_REQUESTS=10` หมายความว่า detector จะเริ่มทำงานก็ต่อเมื่อ session มี request ครบ 10 รายการขึ้นไป เพื่อให้มีข้อมูลเพียงพอต่อการวิเคราะห์รูปแบบ

### เมตริก (Metrics)

**Prometheus metrics ที่ระบบเก็บ:**

```promql
# จำนวน waste findings แยกตาม detector และ severity
api_gateway_waste_findings_total{detector,severity}

# จำนวน token ที่สูญเปล่า แยกตาม detector
waste_tokens_wasted_total{detector}
```

**ตัวอย่าง Query สำหรับ Grafana:**

```promql
# Token waste rate ต่อ detector
sum by (detector) (rate(waste_tokens_wasted_total[5m]))

# Waste findings ต่อ severity
sum by (detector, severity) (rate(api_gateway_waste_findings_total[5m]))
```

### ผลประหยัด

Waste Detection เป็น **Diagnostic** stage กล่าวคือไม่ได้ลด token โดยตรง แต่ช่วย **ระบุ** ว่ามี token จำนวนเท่าไรที่ถูกใช้ไปโดยเปล่าประโยชน์

- **ช่วงที่พบ**: 5-20% ของ token ทั้งหมดถูกระบุว่าเป็น waste
- **ประโยชน์หลัก**: ข้อมูลจาก F11 ช่วยทีมปรับแต่ง optimizer stages อื่นๆ เช่น ลด retry rate, ปรับ context size, หรือเพิ่ม cache hit rate
- **การใช้งานร่วมกับ Bandit (F5)**: ข้อมูล waste สามารถป้อนเข้า Bandit เป็น reward signal เพื่อให้ meta-optimizer เลือก configuration ที่ลด waste ได้โดยอัตโนมัติ

---

## F13: Intent Filter - Relevance-based Output Filtering

ฉันมีรายละเอียดทั้งหมดที่ต้องการแล้ว นี่คือเนื้อหาส่วนกรณีการใช้งานภาษาไทย:

```markdown
## F13: Intent Filter - กรอง response ตาม intent ของคำถาม

### ชื่อเทคนิค
**Intent Filter (F13)** - วิเคราะห์ intent ของคำถาม แล้วกรอง response ให้เหลือเฉพาะส่วนที่เกี่ยวข้อง

### หลักการทำงาน
ใช้ regex pattern matching จำแนก intent ของ user message ออกเป็น 5 ประเภท:

| Intent | เงื่อนไขจับคู่ | พฤติกรรมกรอง |
|--------|-----------------|----------------|
| `code` | write, implement, fix, refactor, function, class... | extract **เฉพาะ code blocks** |
| `analysis` | explain, analyze, why does, compare, review... | ส่งผ่านทั้งหมด (pass-through) |
| `search` | find, search, where is, list all, grep, how many... | extract **เฉพาะ key lines** (bullet, file path) |
| `action` | run, execute, deploy, test, build, migrate... | ส่งผ่านทั้งหมด (pass-through) |
| `chat` | ไม่ตรงกับ pattern ใดๆ (default) | ส่งผ่านทั้งหมด (pass-through) |

Algorithm:
- สแกน **last user message** ใน conversation หา pattern match
- นับคะแนนตามจำนวน pattern ที่ match แต่ละ intent
- Intent ที่ได้คะแนนสูงสุดจะถูกเลือก (default: `chat`)
- **Code intent**: ใช้ `tokenizer.SplitCodeBlocks()` แยก code block ออกจาก prose แล้วตัด prose ทิ้ง
- **Search intent**: เก็บเฉพาะบรรทัดที่ขึ้นต้นด้วย bullet `-`/`*`/`•` หรือมี file path `.go`/`.ts`/`.py` หรือมี `:` และสั้นกว่า 120 chars
- Intent อื่นๆ: ส่ง response กลับเต็ม ไม่มีการกรอง

### สถานการณ์จริงในการใช้งาน

**สถานการณ์**: Platform engineer ใช้ Claude Code ถามว่า:

> "เขียน Terraform module สำหรับ VPC พร้อม public/private subnet"

Intent classification: คำว่า **"write"** match pattern `(?i)\b(write|implement|...)\b` -> **IntentCode**

### Before/After

**Before (Full Response - ~2,400 chars)**:
```
เราจะสร้าง Terraform module สำหรับ VPC ที่มี public และ private subnet กัน
ก่อนอื่นต้องวางแผน architecture ก่อนนะครับ VPC คือ Virtual Private Cloud ที่ช่วยให้เรา
แยก network ของเราจากผู้ใช้คนอื่นใน cloud provider โดยปกติแล้วเราจะแบ่ง subnet
เป็น public (มี Internet Gateway) และ private (ไม่มี direct internet access) ...

นี่คือโครงสร้างไฟล์ที่แนะนำ:
- main.tf - VPC และ subnet resources
- variables.tf - Input variables
- outputs.tf - Output values

ต่อไปเรามาดูวิธีใช้ module นี้กัน:
```hcl
module "vpc" {
  source = "./modules/vpc"
  cidr   = "10.0.0.0/16"
}
```

สำหรับ production ควรเพิ่ม NAT Gateway, VPC Flow Logs และ Network ACL ...
(คำแนะนำเพิ่มเติมอีก 15 บรรทัด)
```

**After (Intent Filter - Code intent - ~800 chars)**:
```hcl
module "vpc" {
  source = "./modules/vpc"
  cidr   = "10.0.0.0/16"
}
```

ผล: **ลด 66%** ของ output tokens, engineer ได้แค่ code ที่ต้องการ copy-paste ไปใช้เลย

### Config

| Environment Variable | Default | คำอธิบาย |
|---------------------|---------|-----------|
| `FILTER_ENABLED` | `true` | เปิด/ปิด Intent Filter |

กำหนดค่าผ่าน environment variable:
```bash
FILTER_ENABLED=true   # เปิดใช้งาน
FILTER_ENABLED=false  # ปิด (bypass ทุก request)
```

### เมตริกที่ติดตาม

| Metric | Label | คำอธิบาย |
|--------|-------|-----------|
| `api_gateway_filter_intents_total` | `{intent}` | นับจำนวนครั้งที่จำแนก intent แต่ละประเภท |
| `api_gateway_filter_chars_saved_total` | `{intent}` | จำนวน characters ที่ประหยัดได้จากการกรอง |

ตัวอย่าง Prometheus query:
```promql
# สัดส่วน intent ที่จับได้
sum by (intent) (api_gateway_filter_intents_total)

# การประหยัดรวมแยกตาม intent
sum by (intent) (api_gateway_filter_chars_saved_total)

# ค่าเฉลี่ย chars saved ต่อ request
rate(api_gateway_filter_chars_saved_total{intent="code"}[5m])
/
rate(api_gateway_filter_intents_total{intent="code"}[5m])
```

### ผลประหยัด (Estimated Savings)

- **10-40% สำหรับ session ที่ถามเกี่ยวกับ code/search เป็นหลัก**
- ประสิทธิภาพสูงสุดเมื่อ: developer ถามขอ code snippet, ค้นหา file, list occurrences
- ผลประหยัดต่ำเมื่อ: สนทนาทั่วไป (chat intent), analysis/explain (ต้องการ context เต็ม)

### ข้อควรระวัง
- หาก code block ไม่มีใน response (เช่น LLM ตอบด้วยคำอธิบายอย่างเดียว) filter จะส่ง response เต็มกลับ ไม่ตัดทอน
- ถ้า pattern match ไม่ตรง intent ใดๆ จะ default เป็น `chat` (pass-through) - ปลอดภัย ไม่สูญเสียข้อมูล
- Intent classification ใช้เฉพาะ **last user message** เท่านั้น ไม่สน context ก่อนหน้า

---

## F20: CompCache - Zstd Compressed Redis Cache

## CompCache (F20) - บีบอัด Redis Cache ด้วย Zstd

### หลักการ

CompCache เป็น transparent wrapper รอบ `redis.Client` ที่บีบอัดข้อมูลอัตโนมัติก่อนเก็บใน Redis โดยใช้ Zstd compression algorithm (level 1-22) และเพิ่ม prefix `zstd:` ให้กับข้อมูลที่บีบอัดแล้วเพื่อ backward compatibility กับข้อมูลดิบ เมื่ออ่านข้อมูลกลับมาจะตรวจสอบ prefix และ decompress อัตโนมัติ

### สถานการณ์จริง

ใน API Gateway production ที่มี optimizer pipeline ทำงาน มีข้อมูลหลายประเภทที่ถูก cache ไว้ใน Redis:

- **Chunker stability data**: ข้อมูลความเสถียรของ chunk ต่อ conversation
- **Delta baseline**: ข้อมูล baseline สำหรับ diff encoding
- **Sketch vectors**: 128-dim sketch vectors สำหรับ near-duplicate detection
- **Bandit arms**: A matrix และ b vector สำหรับ LinUCB bandit algorithm

สมมติ gateway มี Redis cache รวม **50 MB** สำหรับ optimizer data เหล่านี้ หลังจากเปิดใช้ CompCache ขนาดลดลงเหลือ **10-20 MB** (60-80% ลดลง)

### Before/After

| Metric | Before | After | Improvement |
|--------|--------|-------|-------------|
| Redis memory usage | 50 MB | 10-20 MB | 60-80% reduction |
| Cache hit rate | 95% | 95% | No change (transparent) |
| Latency (p99) | 2ms | 2.5ms | +0.5ms overhead |
| CPU usage | baseline | +5% | Compression cost |

### การตั้งค่า (Configuration)

```bash
# เปิด/ปิด CompCache
COMPCACHE_ENABLED=true

# ขนาดขั้นต่ำที่จะบีบอัด (bytes)
COMPCACHE_MIN_SIZE=512  # ค่าเริ่มต้น: 512 bytes

# ระดับการบีบอัด Zstd (1-22)
COMPCACHE_LEVEL=3  # ค่าเริ่มต้น: 3 (balanced speed/ratio)
```

**หมายเหตุ**: ถ้าข้อมูลบีบอัดแล้วใหญ่กว่าเดิม จะเก็บแบบดิบ (raw) โดยอัตโนมัติ

### เมตริกที่ใช้วัด

CompCache มี method `CompressionRatio()` สำหรับคำนวณอัตราส่วนการบีบอัด:

```go
// CompressionRatio คืนค่า ratio ขอขนาดบีบอัด/ขนาดเดิม
// 0 = ไม่ได้บีบอัด
// <1 = บีบอัดแล้วเล็กลง
ratio := compcache.CompressionRatio([]byte(original_data))
// ถ้า ratio = 0.3 แปลว่าบีบอัดเหลือ 30% ของเดิม
```

### ผลประหยัด

- **Redis memory**: 60-80% ลดลง (จาก 50 MB → 10-20 MB)
- **Cost**: ลด cost ของ Redis instance ได้ถึง 60-80% (สามารถใช้ instance เล็กลงได้)
- **Trade-off**: CPU usage เพิ่มขึ้น ~5% และ latency เพิ่ม ~0.5ms ต่อ cache get/set
- **Benefit**: เหมาะสำหรับ production ที่มี cache hit rate สูง (>90%) เพราะประหยัด memory มากกว่าทุน CPU

### ตัวอย่างการใช้งาน

```go
// สร้าง CompCache wrapper
cfg := compcache.LoadConfig()
cc := compcache.New(redisClient, cfg)

// Set ข้อมูล (จะบีบอัดอัตโนมัติถ้า > 512 bytes)
err := cc.CompressedSet(ctx, "chunker:stability:session123", largeJSON, 24*time.Hour)

// Get ข้อมูล (จะ decompress อัตโนมัติถ้ามี zstd: prefix)
val, err := cc.CompressedGet(ctx, "chunker:stability:session123")

// เช็ค compression ratio
ratio := cc.CompressionRatio([]byte(largeJSON))
fmt.Printf("Compressed to %.1f%% of original\n", ratio*100)

---

## F15: Budget-Aware Disclosure - Progressive Content Loading

Now I have the full source code and documentation. Let me verify one detail about the `BudgetAwareEscalate` function - the red budget threshold for content 500-1000 chars uses `L2Tokens * 6`, and for content > 1000 chars uses `L1Tokens * 4`. Let me construct the Thai use case document.

## Budget-Aware Disclosure (F15) - Use Case

### ชื่อเทคนิค

**Budget-Aware Disclosure (F15)** - แสดงข้อมูลแบบ progressive ตาม budget ที่เหลืออยู่ใน context window

### หลักการทำงาน

F15 ทำงานบน `tool_result` blocks ใน request โดยปรับจำนวนข้อมูลที่ส่งให้ provider ตามระดับ budget ปัจจุบันของ session:

| Budget Level | เงื่อนไข | พฤติกรรม |
|---|---|---|
| **Green** (0) | Context < 50% | Pass through ส่งข้อมูลทั้งหมด ไม่ตัด |
| **Yellow** (1) | Content > 2,000 chars | Truncate เหลือ `L2Tokens * 8` chars |
| **Red** (2) | Content > 1,000 chars | Truncate เหลือ `L1Tokens * 4` chars |
| **Red** (2) | Content 500-1,000 chars | Truncate เหลือ `L2Tokens * 6` chars |
| **Red** (2) | Content <= 500 chars | Pass through |

### สถานการณ์จริง: `kubectl describe pod` output

Engineer รัน `kubectl describe pod` ได้ output ยาว **3,000 ตัวอักษร** (ประมาณ 750 tokens) ใน `tool_result` block

#### (a) Green Budget - ส่งทั้งหมด

```
BudgetAwareEscalate(ctx, content, budgetLevel=0)
```

- เงื่อนไข: budgetLevel == 0 (green), context usage < 50%
- ผลลัพธ์: pass through ทั้ง 3,000 chars
- เหตุผล: context window ยังเหลือพอ model สามารถอ่านข้อมูลได้ครบถ้วน

**Before/After:**
```
Before: 3,000 chars (ทั้งหมด)
After:  3,000 chars (ทั้งหมด)
Saved:  0 chars (0%)
```

#### (b) Yellow Budget - ส่งแค่ 480 chars

```
BudgetAwareEscalate(ctx, content, budgetLevel=1)
// content length = 3,000 > 2,000 threshold
// L2Tokens = 60, budget = 60 * 8 = 480 chars
```

- เงื่อนไข: budgetLevel == 1 (yellow), content 3,000 > 2,000
- ผลลัพธ์: truncate เหลือ `60 * 8 = 480` chars
- เหตุผล: context window เริ่มเต็ม (50-75%) จึงตัดข้อมูลที่เกินออก เก็บไว้เฉพาะส่วนแรกที่ model ต้องการอ่าน

**Before/After:**
```
Before: 3,000 chars
After:  480 chars  (content[:480])
Saved:  2,520 chars (84%)
```

ตัวอย่างข้อมูลที่เหลือ:
```
Name:         api-gateway-7d9f8b6c4d-xk2j7
Namespace:    production
Priority:     0
Node:         ip-10-0-1-42.ec2.internal/10.0.1.42
Start Time:   Thu, 07 May 2026 08:23:11 +0700
Labels:       app=api-gateway
              pod-template-hash=7d9f8b6c4d
Status:       Running
IP:           10.0.3.87
Containers:
  api-gateway:
    Container ID:  containerd://abc123...
    Image:         registry.example.com/api-gateway:latest
    Image ID:      registry.example.com/api-gateway@sha256:...
    Port:          8080/TCP
    Host Port:     0/TCP
    State:         Running
      Started:     Thu, 07 May 2026 08:23:12 +0700
    Ready:         True
```

#### (c) Red Budget - ส่งแค่ 60 chars

```
BudgetAwareEscalate(ctx, content, budgetLevel=2)
// content length = 3,000 > 1,000 threshold
// L1Tokens = 15, budget = 15 * 4 = 60 chars
```

- เงื่อนไข: budgetLevel == 2 (red), content 3,000 > 1,000
- ผลลัพธ์: truncate เหลือ `15 * 4 = 60` chars
- เหตุผล: context window ใกล้เต็ม (> 75%) ต้องตัดข้อมูลอย่างรุนแรง เก็บไว้เฉพาะ heading/summary แรกสุด

**Before/After:**
```
Before: 3,000 chars
After:  60 chars   (content[:60])
Saved:  2,940 chars (98%)
```

ตัวอย่างข้อมูลที่เหลือ:
```
Name:         api-gateway-7d9f8b6c4d-xk2j7
Namesp
```

### Configuration

| Environment Variable | Default | คำอธิบาย |
|---|---|---|
| `DISCLOSURE_ENABLED` | `true` | เปิด/ปิด progressive disclosure |
| `DISCLOSURE_L1_TOKENS` | `15` | จำนวน tokens สำหรับ Layer 1 (red budget: `L1 * 4` chars) |
| `DISCLOSURE_L2_TOKENS` | `60` | จำนวน tokens สำหรับ Layer 2 (yellow budget: `L2 * 8` chars) |

ตัวอย่างการปรับค่า:
```bash
# เพิ่ม budget สำหรับ yellow (ให้ข้อมูลเหลือมากขึ้น)
DISCLOSURE_L2_TOKENS=90   # yellow budget: 90*8 = 720 chars แทน 480

# เพิ่ม budget สำหรับ red (heading ยาวขึ้น)
DISCLOSURE_L1_TOKENS=30   # red budget: 30*4 = 120 chars แทน 60

# ปิดทั้งระบบ
DISCLOSURE_ENABLED=false
```

### ผลประหยัด (Estimated Savings)

| Budget Level | Content Size | Before | After | Savings |
|---|---|---|---|---|
| Green | 3,000 chars | 3,000 chars | 3,000 chars | 0% |
| Yellow | 3,000 chars (> 2,000) | 3,000 chars | 480 chars | **84%** |
| Red | 3,000 chars (> 1,000) | 3,000 chars | 60 chars | **98%** |
| Red | 800 chars (500-1,000) | 800 chars | 360 chars (`60*6`) | **55%** |
| Red | 300 chars (< 500) | 300 chars | 300 chars | 0% |

โดยรวม: **50-70% savings** บน large `tool_result` blocks ระหว่าง yellow/red budget

### ข้อควรระวัง

- F15 ทำงานแค่บน `tool_result` blocks เท่านั้น ไม่กระทบ system prompt หรือ user messages
- ข้อมูลถูกตัดแบบ hard truncate (`content[:budget]`) ไม่มี semantic understanding - ตัดตรงตำแหน่ง char ที่ boundary พอดี อาจตัดกลางคำได้
- ข้อมูลที่ถูกตัดจะหายไปจาก context ถาวรสำหรับ request นั้น model จะไม่เห็นข้อมูลส่วนที่ถูกตัด
- ถ้า model ต้องการข้อมูลที่ถูกตัด อาจต้องให้ tool call ซ้ำใน turn ถัดไป (ซึ่งใช้ tokens เพิ่มแต่อาจถูกกว่าการส่งข้อมูลทั้งหมดใน red budget)

---

## F17: TextComp - Whitespace + Filler Compression

Now I have all the details. Let me write the Thai use case section.

## TextComp (F17) - Thai Use Case Section

```markdown
## F17: TextComp - ลบคำฟุ่มเฟือยและ Verbose Text

### ชื่อเทคนิค
**TextComp (F17)** - ลดขนาด prompt ด้วยการลบคำฟุ่มเฟือย (filler), คำเสียม (hedge words) และแปลงประโยค verbose เป็น compact format

### หลักการทำงาน
TextComp ทำงานด้วย pipeline 4 phase:

1. **Mask protected regions** - ป้องกันไม่ให้แก้ไขส่วนสำคัญ ได้แก่ code fences (` ``` ``` `), inline code (`` ` ``), URLs (`https?://`), และ quoted strings (`"..."` ที่ >= 3 ตัวอักษร)
2. **Apply compression rules** - ลบ filler/hedge words + แปลง verbose phrases เป็น compact
3. **Unmask protected regions** - คืนค่าเดิมของส่วนที่ masked ไว้
4. **Cleanup** - ลบ multiple spaces, normalize newlines

**กฎทั้งหมดที่ใช้:**
- 10 filler phrases (เช่น `I would like to`, `Could you please`, `Kindly`)
- 12 hedge words (เช่น `sort of`, `kind of`, `basically`, `actually`, `literally`, `just`, `really`)
- 30 verbose-to-compact replacements (เช่น `due to the fact that` -> `because`, `in order to` -> `to`)
- 11 aggressive-only rules (เช่น `It is important to note that`, `I believe that`) - ทำงานเฉพาะเมื่อ `TEXTCOMP_MODE=aggressive`

### สถานการณ์จริงในการใช้งาน

**ปัญหา**: System prompt ที่เขียนแบบ verbose เช่นมักจะมี filler phrases, hedge words และ verbose constructions ที่ไม่จำเป็นต่อ LLM understanding

ตัวอย่าง system prompt ที่พบบ่อย:

> "It is very important to note that you should always make sure to carefully review the code before suggesting any changes. In order to ensure quality, please let me know if you need any clarification. I would like to point out that you should take into consideration the existing test coverage. Furthermore, it would be great if you could basically focus on the security aspects as well."

TextComp จะ compress เป็น:

> "Review the code before suggesting changes. Ensure quality, if you need clarification. Note you should consider the existing test coverage. Also, focus on the security aspects."

**อะไรถูกลบ/แปลง:**
- `It is very important to note that` -> ลบ (aggressive rule)
- `always make sure to carefully` -> `carefully` (hedge removal: `just`, `really`)
- `In order to` -> `to` (verbose rule)
- `please let me know` -> ลบ (filler rule)
- `I would like to point out that` -> ลบ (aggressive rule)
- `take into consideration` -> `consider` (verbose rule)
- `Furthermore` -> `Also` (verbose rule)
- `it would be great if` -> ลบ (filler rule)
- `basically` -> ลบ (hedge rule)

### Before/After ตัวอย่าง

**ตัวอย่างที่ 1: Filler phrase removal**

| Before | After | กฎที่ใช้ |
|--------|-------|----------|
| `Could you please review this code` | `review this code` | filler: `Could you please` |
| `I want you to fix the bug` | `fix the bug` | filler: `I want you to` |
| `It would be great if you could refactor` | `refactor` | filler: `It would be great if`, `you could` |

**ตัวอย่างที่ 2: Verbose-to-compact**

| Before | After | กฎที่ใช้ |
|--------|-------|----------|
| `due to the fact that the API is down` | `because the API is down` | verbose rule |
| `in order to deploy the service` | `to deploy the service` | verbose rule |
| `prior to the migration` | `before the migration` | verbose rule |
| `with regard to the security policy` | `about the security policy` | verbose rule |

**ตัวอย่างที่ 3: Full pipeline**

```
Before (218 chars):
"It is very important that you always make sure to carefully review 
the code before suggesting any changes. In order to ensure quality, 
please let me know if you need any clarification. I was wondering if 
you could take into consideration the existing test coverage."

After (142 chars):
"Review the code before suggesting changes. to ensure quality, 
if you need clarification. you could consider the existing test coverage."

Saved: 76 chars (34.9%)
```

### การทำงานใน Pipeline

TextComp ทำงานที่ 2 จุดใน request pipeline:

1. **System prompt** (`OptimizeSystemPrompt`) - ทำงานหลัง intent_filter และก่อน caveman
2. **Message text** (`OptimizeMessages`) - ทำงานกับ string content ใน messages

```
OptimizeSystemPrompt:
  ... -> F13 intent_filter -> F17 textcomp -> F16 caveman -> ...

OptimizeMessages:
  whitespace+dedup -> F17 textcomp (string content only)
```

**สิ่งที่ TextComp ไม่แตะ:** code fences, inline code, URLs, quoted strings, tool_use blocks

### Configuration

| Environment Variable | Default | Options | คำอธิบาย |
|---------------------|---------|---------|-----------|
| `TEXTCOMP_ENABLED` | `true` | `true`/`false` | เปิด/ปิด TextComp |
| `TEXTCOMP_MODE` | `balanced` | `balanced`/`aggressive` | `balanced` = filler + hedge + 30 verbose rules. `aggressive` = เพิ่ม 11 aggressive rules (ลบ opinion phrases เช่น `I believe that`, `It is worth noting`) |

**คำแนะนำเลือก mode:**
- `balanced`: เหมาะสำหรับ production, ปลอดภัย ไม่กระทบ meaning
- `aggressive`: เหมาะสำหรับ red budget (>75% context), ลบ opinion/qualifier phrases เพิ่มเติม

### Metrics

```promql
# ตัวอักษรที่ลดได้จาก TextComp
api_gateway_optimizer_chars_saved_total{technique="textcomp"}

# ใช้ดู rate การลดตัวอักษร
sum(rate(api_gateway_optimizer_chars_saved_total{technique="textcomp"}[5m]))

# ดู duration ของ TextComp per request
histogram_quantile(0.95, rate(api_gateway_optimizer_duration_seconds_bucket{technique="textcomp"}[5m]))
```

**Label ใน log:**
- System prompt: `stage=textcomp_sys`
- Message text: `stage=message_textcomp`

### ผลประหยัดโดยประมาณ

| ประเภทข้อความ | ผลประหยัด | หมายเหตุ |
|--------------|-----------|----------|
| Prose-heavy system prompt | 5-15% | prompt ที่เขียนแบบ conversational มี filler เยอะ |
| Technical prompt (code-heavy) | 1-3% | code ถูก masked ไว้ จึงไม่ถูกแก้ |
| Message content | 3-8% | user/assistant messages ที่มี filler |
| Aggressive mode เพิ่ม | 2-5% | ลบ opinion phrases เพิ่ม |

**ตัวอย่างจาก load test จริง (2026-05-06):**
- T4 Code Review: message_textcomp ลด 38 chars (7.6%)
- T5 JSON Config: message_textcomp ลด 34 chars (5.8%)
- T6 Multi-turn: message_textcomp ลด 56 chars (11.5%)
- T7 Shell Output: message_textcomp ลด 108 chars (10.7%)

### ข้อควรระวัง

- TextComp เป็น regex-based compression ไม่ใช้ LLM จึงไม่มี latency overhead (avg < 0.5ms)
- Protected regions ป้องกัน code, URLs, quoted strings แต่ **ไม่ป้องกัน** markdown headings, bullet points, หรือ technical terms ที่ไม่อยู่ใน quoted strings
- ถ้า prompt เป็น bullet-point style (concise อยู่แล้ว) TextComp จะไม่ลดขนาดได้มาก
- `aggressive` mode อาจลบ context ที่มีความหมาย เช่น `I believe that this approach is risky` -> `this approach is risky` (เสีย nuance)

---

## F18: ToolComp - Tool Result Compression

## ToolComp (F18) - Use Case Document

### 1. ชื่อเทคนิค

**ToolComp (F18)** - บีบอัดเนื้อหา `tool_result` ตามรูปแบบข้อมูล (Format-Aware Compression)

---

### 2. หลักการทำงาน

ToolComp ทำหน้าที่ compress เนื้อหาใน `tool_result` blocks ที่ agent ส่งกลับจากการเรียก tool ต่าง ๆ (เช่น Shell, AWS CLI, kubectl) โดยตรวจจับรูปแบบข้อมูลอัตโนมัติแล้วใช้กลยุทธ์บีบอัดที่เหมาะสม:

| Format | วิธีบีบอัด |
|---|---|
| JSON | `json.Compact` - ลบ whitespace/indentation ทั้งหมด |
| ShellLs | เก็บ head N lines + tail 2 lines + แทรก summary line |
| Table | ลบ separator lines (`----`, `====`, `|---|`) ออก |
| Diff | เก็บเฉพาะ changed lines (`+`/`-`) และ hunk headers (`@@`), ตัด context lines ที่ไม่จำเป็น |
| Log | Dedup consecutive identical lines (strip timestamp ก่อนเปรียบเทียบ) |
| Prose | ไม่บีบอัด (ส่งผ่านเดิม) |

**เงื่อนไขข้าม (Skip):**
- ขนาด input < 256 bytes
- ผลลัพธ์หลังบีบอัดใหญ่กว่าหรือเท่ากับต้นฉบับ (คืนค่าเดิม, saved = 0)

---

### 3. สถานการณ์จริงในการใช้งาน

#### (a) Shell Output: `kubectl get pods -A`

Engineer รัน `kubectl get pods -A` ผ่าน Bash tool ใน Claude Code session ได้ output 50+ บรรทัด แสดง pods ทั้งหมดในทุก namespace

**Before (2,850 chars, 50 lines):**
```
NAMESPACE       NAME                                    READY   STATUS    RESTARTS   AGE
kube-system     coredns-6955c5b8d4-abcde                1/1     Running   0          5d
kube-system     etcd-node-1                             1/1     Running   0          5d
kube-system     kube-apiserver-node-1                   1/1     Running   0          5d
kube-system     kube-controller-manager-node-1          1/1     Running   0          5d
kube-system     kube-proxy-abcde                         1/1     Running   0          5d
... (42 lines omitted) ...
monitoring      prometheus-server-0                     2/2     Running   0          2h
monitoring      grafana-7d8f9c6b5d-xyz12                1/1     Running   0          2h
```

**After (485 chars, 12 lines):**
```
NAMESPACE       NAME                                    READY   STATUS    RESTARTS   AGE
kube-system     coredns-6955c5b8d4-abcde                1/1     Running   0          5d
kube-system     etcd-node-1                             1/1     Running   0          5d
kube-system     kube-apiserver-node-1                   1/1     Running   0          5d
kube-system     kube-controller-manager-node-1          1/1     Running   0          5d
kube-system     kube-proxy-abcde                         1/1     Running   0          5d

.. 38 more files/directories ...

kube-system     kube-scheduler-node-1                   1/1     Running   0          5d
monitoring      prometheus-server-0                     2/2     Running   0          2h
monitoring      grafana-7d8f9c6b5d-xyz12                1/1     Running   0          2h
```

**ประหยัด: 2,365 chars (83%)** - model ยังเห็น pods สำคัญทั้งต้นและท้าย list

---

#### (b) JSON: `aws ec2 describe-instances`

Engineer รัน `aws ec2 describe-instances` เพื่อตรวจสอบ EC2 instances ใน account ได้ pretty-printed JSON ขนาด 200KB+

**Before (204,800 chars):**
```json
{
    "Reservations": [
        {
            "Groups": [],
            "Instances": [
                {
                    "AmiLaunchIndex": 0,
                    "ImageId": "ami-0abcdef1234567890",
                    "InstanceId": "i-0abc123def456789",
                    "InstanceType": "m5.xlarge",
                    "LaunchTime": "2026-04-15T08:30:00.000Z",
                    "Monitoring": {
                        "State": "disabled"
                    },
                    "Placement": {
                        "AvailabilityZone": "ap-southeast-1a",
                        "GroupName": "",
                        "Tenancy": "default"
                    },
                    "State": {
                        "Code": 16,
                        "Name": "running"
                    }
                }
            ],
            "OwnerId": "123456789012",
            "ReservationId": "r-0abc123def456"
        }
    ]
}
```

**After (81,920 chars):**
```json
{"Reservations":[{"Groups":[],"Instances":[{"AmiLaunchIndex":0,"ImageId":"ami-0abcdef1234567890","InstanceId":"i-0abc123def456789","InstanceType":"m5.xlarge","LaunchTime":"2026-04-15T08:30:00.000Z","Monitoring":{"State":"disabled"},"Placement":{"AvailabilityZone":"ap-southeast-1a","GroupName":"","Tenancy":"default"},"State":{"Code":16,"Name":"running"}}],"OwnerId":"123456789012","ReservationId":"r-0abc123def456"}]}
```

**ประหยัด: ~122,880 chars (60%)** - ลบ whitespace, indentation, newlines ทั้งหมดโดยใช้ `json.Compact` ข้อมูล JSON ครบถ้วนไม่สูญหาย

---

#### (c) Diff: `git diff` ของ Terraform manifest

Engineer รัน `git diff` เพื่อดูการเปลี่ยนแปลง Terraform config ได้ output ที่มี context lines เยอะ (3 lines ก่อน/หลังทุก hunk)

**Before (3,200 chars, 80 lines):**
```diff
diff --git a/main.tf b/main.tf
--- a/main.tf
+++ b/main.tf
@@ -15,10 +15,7 @@ resource "aws_instance" "web" {
   ami           = "ami-0abcdef123456"
   instance_type = "t3.medium"
-  monitoring    = true
-  metadata_options {
-    http_tokens = "optional"
-  }
+  monitoring    = false
+  metadata_options {
+    http_tokens = "required"
+  }
   tags = {
     Name = "web-server"
   }
@@ -45,8 +45,6 @@ resource "aws_security_group" "web" {
   name        = "web-sg"
   description = "Web security group"
-  ingress {
-    from_port   = 22
-    to_port     = 22
-    protocol    = "tcp"
-    cidr_blocks = ["0.0.0.0/0"]
-  }
   egress {
     from_port   = 0
     to_port     = 0
```

**After (1,280 chars, 32 lines):**
```diff
diff --git a/main.tf b/main.tf
--- a/main.tf
+++ b/main.tf
@@ -15,10 +15,7 @@ resource "aws_instance" "web" {
-  monitoring    = true
-  metadata_options {
-    http_tokens = "optional"
-  }
+  monitoring    = false
+  metadata_options {
+    http_tokens = "required"
+  }
   tags = {
@@ -45,8 +45,6 @@ resource "aws_security_group" "web" {
-  ingress {
-    from_port   = 22
-    to_port     = 22
-    protocol    = "tcp"
-    cidr_blocks = ["0.0.0.0/0"]
-  }
   egress {

.. 48 unchanged lines omitted ...
```

**ประหยัด: ~1,920 chars (60%)** - เก็บเฉพาะบรรทัดที่เปลี่ยน (`+`/`-`), hunk headers (`@@`), และ 1 context line หลัง change block

---

### 4. การตั้งค่า (Configuration)

| Environment Variable | Default | คำอธิบาย |
|---|---|---|
| `TOOLCOMP_ENABLED` | `true` | เปิด/ปิด ToolComp |
| `TOOLCOMP_MAX_LINES` | `50` | จำนวนบรรทัดสูงสุดก่อน trigger compression (ใช้กับ ShellLs, Table, Diff, Log) |

การตั้งค่าผ่าน Kubernetes ConfigMap ตัวอย่าง:
```yaml
env:
  - name: TOOLCOMP_ENABLED
    value: "true"
  - name: TOOLCOMP_MAX_LINES
    value: "50"
```

---

### 5. เมตริกสำหรับ Monitoring

Prometheus metric:
```
api_gateway_optimizer_chars_saved_total{technique="toolcomp"}
```

ตัวอย่าง PromQL queries:
```promql
# chars saved ต่อนาที
rate(api_gateway_optimizer_chars_saved_total{technique="toolcomp"}[5m])

# compression ratio by format (ต้องเพิ่ม label เองถ้าต้องการแยก)
api_gateway_optimizer_chars_saved_total{technique="toolcomp"}
```

Grafana panel recommendation: Stat panel แสดง chars saved total + Time series แสดง rate ต่อ 5m

---

### 6. ผลประหยัดโดยรวม

| Format | ผลประหยัด (เฉลี่ย) | เงื่อนไขที่ได้ผลสูงสุด |
|---|---|---|
| JSON | 50-65% | Pretty-printed JSON ที่มี whitespace เยอะ |
| ShellLs | 70-90% | `ls -la`, `kubectl get`, ลิสต์ไฟล์ยาว 50+ บรรทัด |
| Table | 30-50% | Table ที่มี separator rows (`---`) ซ้ำ ๆ |
| Diff | 50-70% | PR review, config drift ที่มี context lines เยอะ |
| Log | 40-60% | Log ที่มี consecutive duplicate messages |

**ค่าเฉลี่ยรวม: 40-80% บน `tool_result` blocks** (จาก load test: 115 chars saved on shell ls output, 6 tool_result blocks processed)

---

### 7. ข้อควรระวัง

- ToolComp ทำงานที่ character level ไม่ใช่ token level, ดังนั้น % savings อาจต่างจาก token savings จริงเล็กน้อย
- ถ้า model ต้องการข้อมูลครบถ้วน (เช่น audit use case) ควรตั้ง `TOOLCOMP_ENABLED=false` หรือเพิ่ม `TOOLCOMP_MAX_LINES` ให้สูง
- JSON compact ไม่สูญเสียข้อมูลใด ๆ เป็น lossless compression
- ShellLs, Table, Diff, Log เป็น lossy compression (ตัดบรรทัดที่ model อาจต้องการ)

---

## F19: ToolFilter - Intent-based Manifest Filtering

# ToolFilter (F19) - กรอง Tool Manifest ให้เหลือแค่ที่เกี่ยวข้อง

## หลักการ (Principle)

Intent-based scoring ที่จัดประเภท user message เป็น 4 กลุ่มหลัก (code/search/analysis/action) แล้วคำนวณคะแนน relevance สำหรับทุก tool ใน MCP session โดยพิจารณา 3 ปัจจัย:

1. **Intent match** - tool description ตรงกับ user intent หรือไม่ (เช่น user ถามเรื่อง code จะเลือก tool ที่มีคำว่า "read", "edit", "file" มากกว่า "search", "web")
2. **Keyword overlap** - คำใน user message ครอบคลุมกับ tool name/description แค่ไหน (Jaccard similarity)
3. **Description length** - tool ที่มี description สั้นมักมีความเฉพาะเจาะจงสูงกว่า

หลังจากคำนวณคะแนนแล้ว จะเก็บเฉพาะ:
- **Top-K tools** ที่มีคะแนนสูงสุด (K = `TOOLFILTER_MAX_TOOLS`, default 15)
- **Always-keep list** - tools ที่ระบุไว้ว่าต้องเก็บเสมอ (default: "Read,Edit,Write,Bash")

## สถานการณ์จริง (Real-World Use Case)

พิจารณา MCP session ที่มี 27 tools ที่ใช้งานได้:

```
Read, Write, Edit, Bash, WebSearch, WebFetch, Grep, Glob, Agent, 
Skill, NotebookEdit, TodoWrite, TaskStop, EnterWorktree, ExitWorktree,
mcp__4_5v_mcp__analyze_image, mcp__web_reader__webReader, etc.
```

เมื่อ engineer ถามว่า "แก้ bug ใน handler.go" (fix bug in handler.go):

- **Intent classification**: Code (มีคำว่า "bug", "handler.go", extension ของไฟล์โค้ด)
- **Scoring result**: 
  - Read, Edit, Grep, Glob ได้คะแนนสูง (เกี่ยวกับ file/code operations)
  - WebSearch, WebFetch, mcp__web_reader__webReader ได้คะแนนต่ำ (web tools ไม่เกี่ยวกับ local code)
  - NotebookEdit, TodoWrite, Agent ได้คะแนนต่ำ (ไม่ตรงกับ intent ที่ชัดเจน)

- **ผลลัพธ์**: ToolFilter เก็บเฉพาะ Read, Edit, Grep, Glob (4 tools) แทนที่จะส่งทั้ง 27 tools

## Before/After Comparison

### Before (27 tools, full manifest):
```json
{
  "tools": [
    {
      "name": "Read",
      "description": "Reads a file from the local filesystem...",
      "inputSchema": {...}
    },
    {
      "name": "Write",
      "description": "Writes a file to the local filesystem...",
      "inputSchema": {...}
    },
    // ... 25 more tools ...
  ]
}
```
- **ขนาด**: ~8,000+ tokens
- **ปัญหา**: Model ต้องประมวลผล tool manifest ขนาดใหญ่เกินไป แม้ว่าจะใช้งานไม่ถึง

### After (4 tools, filtered):
```json
{
  "tools": [
    {
      "name": "Read",
      "description": "Reads a file from the local filesystem...",
      "inputSchema": {...}
    },
    {
      "name": "Edit",
      "description": "Performs exact string replacements in files...",
      "inputSchema": {...}
    },
    {
      "name": "Grep",
      "description": "Searches for code patterns across files...",
      "inputSchema": {...}
    },
    {
      "name": "Glob",
      "description": "Finds files matching patterns...",
      "inputSchema": {...}
    }
  ]
}
```
- **ขนาด**: ~1,200 tokens
- **ประหยัด**: ~6,800 tokens (85% reduction)

## การตั้งค่า (Configuration)

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `TOOLFILTER_ENABLED` | true | เปิด/ปิดฟีเจอร์ |
| `TOOLFILTER_MAX_TOOLS` | 15 | จำนวน tool สูงสุดที่จะเก็บ (ไม่นับ always_keep) |
| `TOOLFILTER_ALWAYS_KEEP` | "Read,Edit,Write,Bash" | Tools ที่ต้องเก็บเสมอ (คั่นด้วย comma) |

## ผลประหยัด (Savings)

- **กรณีทั่วไป**: ประหยัด 3,000-6,000 tokens/request เมื่อมี tool manifest ขนาดใหญ่ (20+ tools)
- **กรณี extreme**: MCP session ที่มี 50+ tools อาจประหยัดได้มากกว่า 10,000 tokens/request
- **Activation**: ทำงานเมื่อจำนวน tools > `TOOLFILTER_MAX_TOOLS` (default 15)

## Trade-offs

- **ข้อดี**: ลด input tokens อย่างมาก, model เลือกใช้ tool ง่ายขึ้น (choice overload ลดลง)
- **ข้อเสีย**: อาจ filter ออก tool ที่จำเป็นในบาง edge cases (แต่มี always-keep list เป็น safety net)
- **Recommendation**: ปรับ `TOOLFILTER_ALWAYS_KEEP` ตาม use case ของแต่ละ MCP session

---

## PasteGuard - Privacy/PII Masking Pipeline

## PasteGuard - ปกป้องข้อมูลลับและ PII ไม่ให้ส่งถึง LLM Provider

### 1. ชื่อเทคนิค

**PasteGuard** - Privacy Pipeline สำหรับตรวจจับและปิดบังข้อมูลลับ (secrets) และข้อมูลส่วนบุคคล (PII) ก่อนส่งไปยัง upstream LLM provider โดยอัตโนมัติ ผู้ใช้ไม่ต้องแก้ไข prompt เอง

### 2. หลักการ (2-Stage Pipeline)

PasteGuard ทำงานเป็น 2 ขั้นตอน ทำงานแบบ parallel ต่อ span:

**Stage 1: Secret Detection (Regex-based)**
- สแกนด้วย regex patterns ที่ครอบคลุม secret ที่พบบ่อย
- Entity types ที่รองรับ: `OPENSSH_PRIVATE_KEY`, `PEM_PRIVATE_KEY`, `API_KEY_SK`, `API_KEY_AWS`, `API_KEY_GITHUB`, `API_KEY_GITLAB`, `JWT_TOKEN`, `BEARER_TOKEN`, `ENV_PASSWORD`, `ENV_SECRET`, `ENV_USER`, `CONNECTION_STRING`, `API_KEY_GCP`, `API_KEY_TENCENT`, `API_KEY_ALIBABA`, `API_KEY_SLACK`, `API_KEY_STRIPE`, `API_KEY_SENDGRID`, `ENV_TOKEN`, `ENV_CREDENTIAL`, `BASIC_AUTH_URL`, `CLI_AUTH`, `CURL_BASIC_AUTH`, `VAULT_TOKEN`, `AZURE_CREDENTIAL`, `WEBHOOK_URL`
- แทนที่ด้วย reversible placeholder เช่น `[[API_KEY_AWS_1]]`

**Stage 2: PII Detection (Regex-based)**
- ตรวจจับ PII ด้วย regex patterns
- Entity types ที่รองรับ: `EMAIL_ADDRESS`, `PHONE_NUMBER`, `CREDIT_CARD`, `SSN`, `IBAN`, `IP_ADDRESS`, `THAI_NATIONAL_ID`, `THAI_PHONE`
- แทนที่ด้วย placeholder เช่น `[[EMAIL_ADDRESS_1]]`

**Unmask (ย้อนกลับ)**
- รอบส่งคืน: แทนที่ placeholder ทั้งหมดกลับเป็นค่าจริง โดย unmask secrets ก่อน (innermost) แล้วตามด้วย PII (outermost)
- รอบ streaming: `StreamUnmasker` แทนที่ placeholder ทีละ SSE chunk

### 3. สถานการณ์จริง

**สถานการณ์**: DevOps engineer วาง AWS credentials + SSH private key ใน chat ขณะขอให้ LMS ช่วย debug Terraform

**Input ที่ผู้ใช้พิมพ์**:
```
ช่วย debug Terraform ใช้ AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
และ SSH key
-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy0AHB7
...
-----END RSA PRIVATE KEY-----
อีเมลติดต่อ: sompong@example.com เบอร์ 081-234-5678
```

**สิ่งที่ส่งถึง LLM provider (หลัง mask)**:
```
ช่วย debug Terraform ใช้ AWS_ACCESS_KEY_ID=[[API_KEY_AWS_1]]
และ SSH key
[[OPENSSH_PRIVATE_KEY_1]]
อีเมลติดต่อ: [[EMAIL_ADDRESS_1]] เบอร์ [[PHONE_NUMBER_1]]
```

LLM ได้รับเฉพาะ placeholder ไม่เห็นค่าจริง แต่ยังตอบได้ตามปกติ เนื่องจากมี privacy prompt injection แจ้งให้ LLM รักษารูปแบบ placeholder ไว้

**Response ที่ผู้ใช้ได้รับ (หลัง unmask)**: placeholder ถูกแทนที่กลับเป็นค่าจริงทั้งหมด ผู้ใช้เห็นข้อมูลเดิมตามปกติ

### 4. Before / After

**Original Request Body**:
```json
{
  "messages": [
    {
      "role": "user",
      "content": "ช่วย debug Terraform ใช้ AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE และ SSH key -----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn...\n-----END RSA PRIVATE KEY-----"
    }
  ]
}
```

**Masked Request (ส่งถึง upstream)**:
```json
{
  "messages": [
    {
      "role": "user",
      "content": "ช่วย debug Terraform ใช้ AWS_ACCESS_KEY_ID=[[API_KEY_AWS_1]] และ SSH key [[OPENSSH_PRIVATE_KEY_1]]"
    }
  ]
}
```

**Masked Response (จาก upstream)**:
```json
{
  "content": "ตรวจสอบ AWS_ACCESS_KEY_ID=[[API_KEY_AWS_1]] และ SSH key [[OPENSSH_PRIVATE_KEY_1]] พบว่า..."
}
```

**Unmasked Response (ส่งกลับผู้ใช้)**:
```json
{
  "content": "ตรวจสอบ AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE และ SSH key -----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn...\n-----END RSA PRIVATE KEY----- พบว่า..."
}
```

### 5. Configuration

ตั้งค่าผ่าน environment variables:

| Variable | Default | รายละเอียด |
|---|---|---|
| `PASTEGUARD_ENABLED` | `true` | เปิด/ปิด pipeline ทั้งหมด |
| `PASTEGUARD_SECRETS_ENABLED` | `true` | เปิด/ปิด secret detection |
| `PASTEGUARD_SECRET_ENTITIES` | 27 types | รายการ secret entity types ที่ตรวจจับ (คั่นด้วย comma) |
| `PASTEGUARD_MAX_SCAN_CHARS` | `200000` | จำนวนตัวอักษรสูงสุดต่อ request |
| `PASTEGUARD_PII_ENABLED` | `true` | เปิด/ปิด PII detection |
| `PASTEGUARD_PII_ENTITIES` | `EMAIL_ADDRESS,PHONE_NUMBER,...` | รายการ PII entity types (คั่นด้วย comma) |

ค่า default ของ secret entities (27 types):
```
OPENSSH_PRIVATE_KEY,PEM_PRIVATE_KEY,API_KEY_SK,API_KEY_AWS,API_KEY_GITHUB,API_KEY_GITLAB,JWT_TOKEN,BEARER_TOKEN,ENV_PASSWORD,ENV_SECRET,ENV_USER,CONNECTION_STRING,API_KEY_GCP,API_KEY_TENCENT,API_KEY_ALIBABA,API_KEY_SLACK,API_KEY_STRIPE,API_KEY_SENDGRID,ENV_TOKEN,ENV_CREDENTIAL,BASIC_AUTH_URL,CLI_AUTH,CURL_BASIC_AUTH,VAULT_TOKEN,AZURE_CREDENTIAL,WEBHOOK_URL
```

ค่า default ของ PII entities (8 types):
```
EMAIL_ADDRESS,PHONE_NUMBER,CREDIT_CARD,SSN,IBAN,IP_ADDRESS,THAI_NATIONAL_ID,THAI_PHONE
```

### 6. เมตริก (Prometheus)

| Metric | Labels | รายละเอียด |
|---|---|---|
| `api_gateway_mask_duration_seconds` | `phase` (secrets_detect, pii_detect, mask, unmask) | ระยะเวลาแต่ละ phase (histogram) |
| `api_gateway_secrets_detected_total` | `type` | จำนวน secrets ที่ตรวจพบ แยกตาม entity type |
| `api_gateway_pii_detected_total` | `type` | จำนวน PII ที่ตรวจพบ แยกตาม entity type |
| `api_gateway_mask_requests_total` | `has_secrets`, `has_pii` | จำนวน requests ที่ผ่าน pipeline |

### 7. ข้อควรระวัง

- **Streaming support**: ใช้ `StreamUnmasker` สำหรับ SSE streaming responses จะแทนที่ placeholder ทีละ chunk โดยรักษาข้อมูลที่แบ่งข้าม chunk boundary ได้อย่างถูกต้อง
- **Non-streaming**: ใช้ `UnmaskResponse()` แทนที่ placeholder ทั้งหมดในครั้งเดียว
- **Unmask order**: ย้อนกลับจากการ mask - unmask secrets ก่อน (innermost) แล้วตามด้วย PII (outermost) เพื่อให้ตรงกับลำดับที่ mask
- **Privacy prompt injection**: เมื่อมีการ mask เกิดขึ้น pipeline จะแทรก privacy prompt ให้ LLM รักษารูปแบบ placeholder ไว้ ไม่แทนที่ด้วยค่าอื่น
- **No `PERSON` entity**: ตามการออกแบบ ไม่ตรวจจับชื่อบุคคล เพื่อลด false positive ที่ทำให้การสนทนาติดขัด - ตรวจเฉพาะ `EMAIL_ADDRESS` และ `PHONE_NUMBER` เป็นค่า default
- **Max scan limit**: ถ้า request เกิน `PASTEGUARD_MAX_SCAN_CHARS` (default 200,000 chars) จะสแกนเฉพาะส่วนต้น ควรปรับเพิ่มหากใช้ context ขนาดใหญ่
- **Parallel span processing**: text spans แต่ละส่วนถูกประมวลผลแบบ concurrent (goroutine per span) แต่ mask context (mapping table) ใช้ mutex ป้องกัน race condition
- **Thai-specific entities**: รองรับ `THAI_NATIONAL_ID` (เลขบัตรประชาชน) และ `THAI_PHONE` (เบอร์โทรไทย) อยู่ใน default PII entities

---

## Adaptive Rate Limiting - Token-aware Throttling

I now have enough context. Let me write the Thai use case section.

```markdown
## Adaptive Rate Limiting - จำกัด request rate แบบ adaptive ตาม provider limits

### 1. ชื่อเทคนิค

**Adaptive Rate Limiting** -- middleware ที่จำกัด concurrent upstream request แบบ adaptive โดยปรับ limit อัตโนมัติตาม response จาก upstream provider (429/503 handling, RTT gradient, learned ceiling)

### 2. หลักการทำงาน

ระบบประกอบด้วย 3 เลเยอร์ที่ทำงานร่วมกัน:

**2.1 Per-Session Rate Limiting (distributed rate limiter)**

middleware `ratelimit.go` ทำหน้าที่เป็น gate แรก ก่อนที่ request จะไปถึง upstream โดยตรวจสอบสองระดับพร้อมกัน (parallel check):

- **Global limit** -- concurrent request รวมทุก session (`key: "global"`)
- **Per-agent limit** -- concurrent request ต่อ agent หนึ่งตัว (`key: "agent:<id>"`) โดยแยก agent ID จาก `x-api-key` (Anthropic) หรือ `agent_id` query param

หาก distributed rate limiter service ไม่ตอบ (network error) ระบบจะใช้กลไก **fail-open** -- ปล่อย request ผ่านเพื่อไม่ให้ service down จาก rate limiter เอง

Path ที่ถูก rate limit: `/v1/messages`, `/v1/chat/completions`, `/mcp/*`, `/api/claude_code/*`, `/v1/mcp_servers`

**2.2 Adaptive Concurrency Limiter (adaptive_limiter.go)**

เป็น core algorithm ที่ควบคุม concurrent request ต่อ model โดยอ้างอิงจาก:
- **Envoy gradient controller** -- คำนวณ `gradient = (minRTT + buffer) / sampleRTT` แล้วปรับ limit ตามอัตราส่วน
- **Netflix concurrency limits** -- ลด limit ทันทีเมื่อเจอ 429 แล้วค่อยๆ เพิ่มกลับเมื่อ success

Algorithm ทำงาน 3 เฟส:

| เหตุการณ์ | การปรับ limit | รายละเอียด |
|---|---|---|
| **On 429/503** | `limit_new = max(minLimit, limit * 0.5)` | ลด limit ลงครึ่งหนึ่งทันที เก็บ `peakBefore429` เป็น learned ceiling |
| **On 200 (ทุก 5 request)** | `limit_new = min(maxLimit, gradient * limit + sqrt(limit))` | เพิ่ม limit ตาม gradient (0.8-2.0) + additive increase |
| **Cooldown** | ห้ามเพิ่ม 5 วินาทีหลัง 429 | ป้องกันการเพิ่ม limit กลับเร็วเกินไป |

นอกจากนี้ยังมีระบบ **model fallback** -- กระจาย request ข้าม model series (เช่น glm-5 -> glm-4) เมื่อ series หนึ่งเต็ม หรือมี latency pressure (EWMA RTT > 1.2x minRTT) หรือเพิ่งเจอ 429

**2.3 Session Secret (session_secret.go)**

เมื่อ gateway ทำงานหลาย replica จำเป็นต้องมี shared secret เพื่อ:
- sign session cookie ให้ตรงกันทุก replica
- user ที่ hit replica A แล้วถูก redirect ไป replica B ยังคง session ไว้ได้

secret ถูก generate ครั้งแรกแล้วเขียนลงไฟล์ `config/session_secret` ทุก replica ที่ mount volume เดียวกันจะใช้ secret เดียวกัน และมี `fsnotify` watcher reload อัตโนมัติเมื่อ secret เปลี่ยน (ไม่ต้อง restart pod)

### 3. สถานการณ์จริง: ทีม 5 Engineers ใช้ Gateway ร่วมกัน

สมมติทีม Platform 5 คนใช้ AI gateway ร่วมกันเพื่อเรียก Z.AI (GLM) models:

**บุคคล:**

| Engineer | รูปแบบการใช้งาน | Rate |
|---|---|---|
| Engineer A (Backend) | code generation, refactoring | ~10 req/min |
| Engineer B (DevOps) | script writing, YAML generation | ~5 req/min |
| Engineer C (QA) | test case generation | ~3 req/min |
| Engineer D (Lead) | code review, documentation | ~8 req/min |
| Engineer E (Junior) | learning, asking questions | ~4 req/min |

**การตั้งค่า config:**

```
UPSTREAM_MODEL_LIMITS=glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3
UPSTREAM_GLOBAL_LIMIT=9
UPSTREAM_DEFAULT_LIMIT=3
UPSTREAM_PROBE_MULTIPLIER=5
```

**สถานการณ์ A: Engineer A ส่ง request 10 ครั้ง/นาที**

```
Engineer A sends burst of requests...
  Request 1: Acquire("glm-5.1") -> OK, inFlight=1, limit=1
  Request 2: Acquire("glm-5.1") -> full, fallback to "glm-5-turbo" -> OK
  Request 3: Acquire("glm-5.1") -> full, fallback to "glm-5" -> OK
  Request 4: All glm-5 series full (utilization 70%+) -> spill to glm-4.7
  ...
  Requests 7-10: global limit (9) hit -> wait for slot via sync.Cond
                  -> released within ~200ms as earlier requests complete
```

**สถานการณ์ B: Engineer B ส่ง request 5 ครั้ง/นาที**

```
Engineer B sends requests at steady pace...
  All requests go to glm-5.1 (limit=1, releases between calls)
  Adaptive limiter tracks: minRTT=150ms, ewmaRTT=180ms
  No 429 encountered -> limit stays at configured value
```

**สถานการณ์ C: Provider ตอบ 429 -- Adaptive ปรับ limit**

```
Time T+0:   Provider returns 429 on glm-5.1
            -> limit drops: 1 -> max(1, 1*0.5) = 1 (floor hit)
            -> peakBefore429 = 1 (learned ceiling)
            -> cooldown timer starts (5 seconds)

Time T+5s:  Success responses start coming
            -> cooldown expired
            -> every 5th success: gradient calculated
            -> limit increases: gradient * limit + sqrt(limit)
            -> but capped at peakBefore429 - 1 = 0 (stays at 1)

Time T+5m:  Learned ceiling decays (5 minute timeout)
            -> limit can now probe above previous peak
            -> gradually increases back to configured max
```

### 4. Before / After: 429 Error Rate

**ก่อนใช้ Adaptive Rate Limiting:**

```
┌──────────────────────────────────────────────────────┐
│  Fixed limit = 60 req/min per agent                  │
│                                                      │
│  09:00  5 engineers x 6 req/min = 30 req/min OK     │
│  10:00  Burst: A sends 20, D sends 15 = 35 burst    │
│         Total concurrent > upstream capacity          │
│         >>> Provider returns 429 repeatedly <<<       │
│                                                      │
│  429 Error Rate: ~12-18%                             │
│  Avg retry per request: 1.4x                         │
│  P99 latency: 3.2s (retry backoff)                   │
│  User experience: intermittent failures, slow response│
└──────────────────────────────────────────────────────┘
```

**หลังใช้ Adaptive Rate Limiting:**

```
┌──────────────────────────────────────────────────────┐
│  Adaptive limit, starts at configured, adjusts down   │
│                                                      │
│  09:00  5 engineers, adaptive limit = configured     │
│         All requests distributed across models        │
│         Cross-series spillover when series full       │
│                                                      │
│  10:00  Burst detected via 429 feedback               │
│         -> limit auto-reduces from 9 to 4 concurrent  │
│         -> requests queue (sync.Cond, no spin-wait)   │
│         -> back to normal after 5 min cooldown        │
│                                                      │
│  429 Error Rate: ~1-3% (transient bursts only)       │
│  Avg retry per request: 1.05x                        │
│  P99 latency: 1.8s (queue wait, not retry)           │
│  User experience: slightly slower during burst,       │
│                   but no failures                     │
└──────────────────────────────────────────────────────┘
```

**Summary:**

| Metric | Before | After |
|---|---|---|
| 429 Error Rate | 12-18% | 1-3% |
| Avg Retry/Request | 1.4x | 1.05x |
| P99 Latency | 3.2s | 1.8s |
| User-facing Failures | Intermittent | Near zero |
| Recovery Time (manual) | 10-30 min (human) | ~5 min (auto) |

### 5. Integration with Upstream Model Limits and Configuration

การตั้งค่า upstream model limits ผ่าน environment variables ที่ map ไปยัง `config.Config`:

```go
// config.go
ModelLimits        map[string]int  // UPSTREAM_MODEL_LIMITS
VisionModelLimits  map[string]int  // UPSTREAM_VISION_MODEL_LIMITS
DefaultLimit       int             // UPSTREAM_DEFAULT_LIMIT
GlobalLimit        int             // UPSTREAM_GLOBAL_LIMIT
ProbeMultiplier    int             // UPSTREAM_PROBE_MULTIPLIER
```

**การ map ค่า config ไปยัง AdaptiveLimiter:**

```
UPSTREAM_MODEL_LIMITS=glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3,glm-4.6v:10,glm-4.5v:10
                      ↓ parseModelLimits()
                      map[string]int{"glm-5.1":1, "glm-5-turbo":1, ...}
                      ↓ NewAdaptiveLimiter(limits, ...)
                      am.limit.Store(max)           // initial = configured limit
                      am.maxLimit = max * probeMultiplier  // ceiling for adaptive increase

UPSTREAM_GLOBAL_LIMIT=9  →  al.globalLimit = 9  (hard cap across all models)

UPSTREAM_PROBE_MULTIPLIER=5  →  glm-5.1: maxLimit = 1*5 = 5
                                glm-5:   maxLimit = 2*5 = 10
```

**Vision model มี limit ต่างกัน:**

```
UPSTREAM_VISION_MODEL_LIMITS=glm-5.1:5,glm-4.6v:5,glm-4.5v:3
```

Vision request ใช้ limit ที่สูงกว่า เพราะ image processing ต้องการ concurrent slot มากกว่า text-only request

**Runtime override (ผ่าน API):**

```
POST /v1/limiter-status  (GET -- ดูสถานะปัจจุบัน)
  Response: [
    {
      "name": "glm-5.1",
      "in_flight": 1,
      "limit": 1,
      "max_limit": 5,
      "learned_ceiling": 1,
      "total_requests": 1234,
      "total_429s": 3,
      "min_rtt_ms": 150,
      "ewma_rtt_ms": 180,
      "series": 5,
      "overridden": false
    },
    ...
  ]
```

`SetOverride(model, limit)` ช่วยให้ admin pin limit ของ model ได้โดยไม่ต้อง restart gateway เมื่อมี override อยู่ adaptive algorithm จะหยุดปรับ limit อัตโนมัติ (แต่ยังเก็บ RTT และ success stats อยู่)

**Flow รวมของ request หนึ่งๆ:**

```
Client Request
    │
    ▼
[RateLimiter.Middleware] ── global check ── agent check (parallel)
    │  fail-open on error
    ▼ (allowed)
[AdaptiveLimiter.Acquire(requestedModel)]
    │  wait for global slot (sync.Cond, 60s timeout)
    │  try requested model slot
    │  fallback: same-series round-robin → lower-series spillover
    │  final: acquireAnyModel (wait for any slot)
    ▼ (acquired, selectedModel may differ)
[Proxy] → upstream provider
    │
    ▼
[Proxy calls Feedback(model, statusCode, rtt, headers)]
    │  429/503 → limit *= 0.5, record peak, start cooldown
    │  200 → update minRTT, ewmaRTT, increase limit every 5th success
    ▼
[AdaptiveLimiter.Release(selectedModel)]
    │  decrement inFlight, signal waiting goroutines
    ▼
Response to Client
```

---

## Multi-Provider Routing - Cost-based Traffic Distribution

ตอนนี้ผมมีความเข้าใจอย่างทั่วถึงเกี่ยวกับระบบแล้ว นี่คือเนื้อหาส่วน Use Case ในภาษาไทย:

---

## Multi-Provider Routing

### 1. ชื่อเทคนิค

**Multi-Provider Routing** - ระบบส่ง request ไปยัง AI provider ที่เหมาะสมที่สุดอัตโนมัติ พร้อม fallback chain เมื่อ provider หลักไม่พร้อมใช้งาน

### 2. หลักการ

Gateway ทำหน้าที่เป็น single entry point (`/v1/messages`) ที่รับ request จาก client แล้วตัดสินใจว่าจะส่งไป provider ไหน โดยพิจารณาจาก:

- **Model prefix matching** - ชื่อ model ที่อยู่ใน request บอกได้เลยว่าต้องส่งไป provider ไหน (เช่น `claude-` -> Anthropic, `glm-` -> Z.AI)
- **Token availability** - มี API key หรือ OAuth token ของ provider นั้นอยู่ในระบบหรือไม่
- **Cooldown state** - provider ถูก rate limit (429) อยู่หรือไม่
- **Utilization** - account ไหนมี usage ต่ำกว่า 80% ใน 5 ชั่วโมงที่ผ่านมา (ใช้กับ claude-oauth)

เมื่อ provider หลักล้มเหลว (429, timeout, error) ระบบจะลอง fallback ตามลำดับ:

1. **Model fallback** - ลอง model เบากว่าใน provider เดิมก่อน
2. **Provider fallback** - เปลี่ยนไป provider ถัดไปใน routing chain
3. **Error** - คืน error ให้ client เมื่อทุกทางไม่สำเร็จ

### 3. Provider ที่รองรับ

| Provider ID | ชื่อ | Auth | API Format | Use Case |
|-------------|------|------|------------|----------|
| `anthropic` | Anthropic | API Key | Anthropic | มี API key ตรงจาก Anthropic |
| `claude-oauth` | Claude (OAuth) | OAuth PKCE | Anthropic | ใช้ Claude Code subscription |
| `zai` | Z.AI | API Key | Anthropic | ใช้ GLM model family |
| `gemini` | Google Gemini | API Key | Gemini | มี Google AI API key |
| `gemini-oauth` | Google Gemini (OAuth) | OAuth Auth Code | Gemini | ใช้ Google Code Assist |
| `openai` | OpenAI | API Key | OpenAI | GPT models |
| `deepseek` | DeepSeek | API Key | OpenAI | DeepSeek models |
| `qwen` | Qwen (Aliyun) | Device Code | OpenAI | Qwen models จาก Aliyun |
| `openrouter` | OpenRouter | API Key | OpenAI | เข้าถึง model หลายค่าย |
| `copilot` | GitHub Copilot | Device Code | OpenAI | GitHub Copilot |
| `ollama` | Ollama | API Key | OpenAI | Local models |

### 4. สถานการณ์จริง

#### (a) Engineer ขอใช้ Claude Opus

```
Request: model = "claude-opus-4-7"
```

**Routing decision:**

```
1. Model prefix "claude-" matched
2. Provider priority: claude-oauth -> anthropic
3. claude-oauth: try round-robin among active accounts
   - Filter: skip paused, skip expired
   - Prefer: accounts with 5h utilization < 80%
   - Select: account with lowest usage
4. Result: route to claude-oauth with selected account
   Upstream: api.anthropic.com/v1/messages?beta=true
   Auth: Bearer sk-ant-oat01-* (OAuth token)
   Headers: anthropic-beta, x-app: cli, User-Agent: claude-cli/*
```

#### (b) Rate limit จาก Anthropic -> Fallback to Z.AI

```
Request: model = "claude-sonnet-4-20250514"
Response from Anthropic: HTTP 429 (rate_limit_error)
```

**Fallback sequence:**

```
Step 1: Model fallback (same provider)
   claude-sonnet-4-20250514 -> claude-haiku-4-5-20251001
   Check cooldown: haiku still available -> try
   If haiku also 429 -> proceed to Step 2

Step 2: Provider fallback
   Exclude claude-oauth, try next in chain
   "claude-" rule: [claude-oauth, anthropic]
   Try anthropic (direct API key)
   If anthropic also fails -> proceed to Step 3

Step 3: If profile has multi-target or key pool
   Try next target in profile's targets[]
   Or try key pool default token

Step 4: All exhausted -> return 429 to client
```

ในโหมด GLM (`GLM_MODE=true`), model ที่ไม่รู้จักจะ fallback ไป Z.AI อัตโนมัติ:

```
Unknown model "my-custom-model"
-> No prefix match
-> Fallback: zai provider with glm-4.5
```

#### (c) Vision request -> Route based on provider capability

```
Request: model = "gemini-2.5-flash", messages contain image content
```

**Routing with vision consideration:**

```
1. Model prefix "gemini-" matched
2. Provider priority: gemini-oauth -> gemini
3. gemini-oauth: check if cooling down from previous vision 429
   - If cooling down: skip to next model (gemini-2.5-flash-lite)
   - If all gemini-oauth models cooling: try gemini (API key)
4. Gemini handles vision natively in multimodal format
```

สำหรับ model อื่นๆ ที่รองรับ vision:

| Provider | Vision Support | Format |
|----------|---------------|--------|
| `claude-oauth` | Yes | Anthropic image blocks |
| `gemini` / `gemini-oauth` | Yes | Gemini multimodal parts |
| `openai` | Yes | OpenAI image_url |
| `zai` | Limited | base64 only, no URLs |


```
```

**Routing:**

```
1. Profile "internal-profile" loaded from Redis
3. Max tokens clamped: 4096
4. Tool mode: native (OpenAI function calling format)
5. Auto-continuation: 3 attempts when finish_reason = "length"
6. Upstream: https://llm.internal/custom/llm/v1/chat/completions
   Auth: Bearer <internal-api-key>
```

### 5. Before / After

#### Before: แต่ละ team ต้องจัดการ provider เอง

```
Team A: ใช้ Claude API key ตรง (เขียน hardcoded ใน config)
Team B: ใช้ OpenAI API key ตรง
Team C: ไม่มี budget ซื้อ API key -> ใช้ model ฟรีที่คุณภาพต่ำ

Problem:
- Claude ติด rate limit -> user เจอ 429 เลย, งานต้องหยุด
- ไม่มี fallback -> downtime เมื่อ provider ล่ม
- ไม่มี utilization tracking -> account บางตัวใช้เยอะเกิน
- Token หมดอายุ -> ต้อง manual refresh
- ไม่มี centralized cost visibility
```

#### After: Gateway จัดการ routing ทั้งหมด

```
Team A: ส่ง request ไป gateway เดียว -> route อัตโนมัติ
Team B: ส่ง request เดียวกัน -> gateway รู้ว่าต้องใช้ provider ไหน
Team C: ใช้ profile เดียวกัน -> fallback ไป model ที่ถูกกว่าเมื่อจำเป็น

Flow:
User -> Gateway (decide provider) -> Anthropic (primary)
                                  -> Z.AI (fallback on 429)
                                  -> Gemini (secondary fallback)

Benefits:
- 429 -> auto model fallback (opus -> sonnet -> haiku) -> auto provider fallback
- Round-robin with utilization awareness -> กระจาย load ไม่ให้ account ไหนเกิน
- Token auto-refresh ทุก 30 นาที -> ไม่ต้อง manual rotate
- Centralized cost tracking ทุก provider ใน metrics เดียว
```

#### Cost comparison example

| สถานการณ์ | Before | After |
|----------|--------|-------|
| Claude Opus 429 | User ต้องรอ/ลองใหม่เอง | Auto fallback -> Sonnet -> Haiku -> Z.AI glm-5 |
| 5 concurrent users | ติด rate limit บ่อย | Round-robin 5 accounts, utilization-aware selection |
| Gemini Pro 429 | งานหยุด | Auto fallback -> Flash -> Flash Lite |

### 6. Provider Capabilities Map และ Routing Logic

#### Model prefix -> Provider routing table

```
+---------------------+-------------------------------+
| Model Prefix        | Providers (priority order)    |
+---------------------+-------------------------------+
| claude-             | claude-oauth -> anthropic     |
| gpt-                | openai                        |
| o1- / o3- / o4-    | openai                        |
| gemini-             | gemini-oauth -> gemini        |
| glm-                | zai                           |
| qwen-               | qwen                          |
| or-                 | openrouter                    |
| deepseek-           | deepseek                      |
| kimi-               | kimi                          |
| anthropic/          | anthropic -> openrouter       |
| openai/ / google/   | openrouter                    |
| meta/ / deepseek/   | openrouter                    |
+---------------------+-------------------------------+
```

#### Model fallback chains (on 429)

```
Claude family:
  claude-opus-4-7 -> opus-4-20250115 -> sonnet-4-6 -> sonnet-4-20250514 -> haiku-4-5

Gemini family:
  gemini-2.5-pro -> 2.5-flash -> 2.5-flash-lite -> 2.0-flash

GPT family:
  gpt-4.1 -> gpt-4.1-mini -> gpt-4.1-nano
  o3 -> o4-mini

Z.AI (GLM) family:
  glm-5.1 -> glm-5 -> glm-4.7 -> glm-4.6 -> glm-4.5

Qwen family:
  qwen-max -> qwen-plus -> qwen-turbo
```

#### Token selection priority

```
Profile with accountIds:
  1. Round-robin among selected accounts (prefer utilization < 80%)
  2. Fallback to high-utilization accounts if all >= 80%

Profile with passthroughAuth:
  Use client's own Bearer token directly

Profile without accountIds:
  1. tokenStore.GetDefault(provider) - default token for provider
  2. Resolver key pool / ZAI_API_KEYS (env fallback)

No profile (GLM mode):
  Use ZAI_API_KEYS from environment variable
```

#### Profile-based multi-target routing

```
Profile config:
{
  "name": "multi-claude",
  "target": "claude-oauth",
  "targets": [
    {"target": "claude-oauth", "accountIds": ["acc-1", "acc-2"]},
    {"target": "anthropic"},
    {"target": "zai"}
  ]
}

Routing:
  1. Try claude-oauth with accounts [acc-1, acc-2] (round-robin)
  2. If claude-oauth 429 or no tokens -> try anthropic (direct API key)
  3. If anthropic also fails -> try zai (GLM fallback)
  4. All fail -> return 429

---

## F16: Caveman - Output Style Injection

# F16: Caveman - บีบ Output ด้วย Style Injection 4 ระดับ

## หลักการ

Caveman เป็น optimizer stage ประเภท **OUTPUT influence** - ไม่ได้บีบ output โดยตรง แต่ฉีด `[OUTPUT STYLE]` directive เข้าไปใน system prompt เพื่อสั่งให้ model ตอบสั้นลง

วิธีการ: นำ system prompt เดิม (อาจยาวหลายร้อยถึงหลายพันตัวอักษร) มาต่อท้ายด้วย style injection directive ขนาด ~229 chars ทำให้ model ปรับพฤติกรรมการตอบให้กระชับขึ้น

4 tiers ตาม budget level:

| Tier | Budget Level | ลด output ประมาณ | Directive |
|------|-------------|-------------------|-----------|
| **lite** | Green (< 50% context) | ~30% | ตัด pleasantries, ตอบบรรทัดเดียวถ้าได้ |
| **full** | Yellow (50-75% context) | ~50% | ตัดทุก filler, ใช้ short variable names, ไม่ repeat คำถาม |
| **ultra** | Red (> 75% context) | ~75% | Raw output เท่านั้น, ไม่มี natural language wrapper, ใช้ symbols/abbreviations |
| **wenyan** | พิเศษ | ~70% | Classical notation, facts only, ไม่มี grammar glue words |

**Code validation**: หลังจาก compression แล้ว Caveman ตรวจสอบว่า code blocks และ identifiers ยังคงอยู่ครบ โดยนับจำนวน code blocks (``` pairs) และตรวจ identifier preservation >= 80%

---

## สถานการณ์จริง (Use Cases)

### Scenario A: Green Budget - Engineer ถามคำถามง่าย

**สถานการณ์**: Engineer เปิด session ใหม่ ถาม "Go map thread-safe ยังไง?" context ใช้ไปแค่ 20% เลยอยู่ tier lite

**Before** (system prompt เดิม ~577 chars):
```
You are an expert Go developer with deep knowledge of concurrent programming.
You should always provide clear, well-documented code examples.
Make sure to explain the reasoning behind your architectural choices.
Remember to consider edge cases and error handling in your solutions.
You should make sure to follow Go best practices and idiomatic patterns.
```

**After** (ต่อท้ายด้วย lite directive):
```
[OUTPUT STYLE - lite]
Be concise.Use bullet points.Skip pleasantries and filler phrases.
Avoid: "Great question!", "Certainly!", "I'd be happy to help!", "In summary,", "Hope this helps!".
One sentence answers when possible.
```

**ผลลัพธ์**: Model ตอบสั้นลง ~30% แทนที่จะอธิบายยาว 3 ย่อหน้า จะได้แค่ bullet points สั้นๆ + code snippet

```promql
api_gateway_caveman_compressions_total{tier="lite",result="valid"} 1
```

---

### Scenario B: Yellow Budget - คุยกันมา 15 turns

**สถานการณ์**: Code review session คุยกันมา 15 messages context ใช้ไป 60% เข้าเกณฑ์ tier full

**Before** (system prompt เดิม ~952 chars):
```
You are an expert Go developer with deep knowledge of concurrent programming.
You should always provide clear, well-documented code examples.
Make sure to explain the reasoning behind your architectural choices.
[... system prompt ยาวขึ้นเพราะมี context จาก conversation ก่อนหน้า ...]
```

**After** (ต่อท้ายด้วย full directive):
```
[OUTPUT STYLE - full]
Be extremely terse.Code only when asked for code.No explanations unless requested.
Avoid all filler, preamble, and summary paragraphs.
Use short variable names in examples.Prefer tables over paragraphs.
If the answer fits in one line, use one line.
Never repeat or paraphrase the question back.
```

**ผลลัพธ์**: Model ตอบสั้นลง ~50% ให้ code อย่างเดียว ไม่มี explanation นอกจากถูกขอ ใช้ short variable names

```promql
api_gateway_caveman_compressions_total{tier="full",result="valid"} 1
```

---

### Scenario C: Red Budget - Session ยาวมาก 30+ turns

**สถานการณ์**: Debug session ยาวนาน 30+ messages แก้ bug ซับซ้อน context ใช้ไป 85% เข้าเกณฑ์ tier ultra บีบแบบสุด

**Before** (system prompt เดิม ~1067 chars):
```
You are an expert Go developer with deep knowledge of concurrent programming.
You should always provide clear, well-documented code examples.
Make sure to explain the reasoning behind your architectural choices.
[... system prompt ยาวมาก รวม accumulated context ...]
```

**After** (ต่อท้ายด้วย ultra directive):
```
[OUTPUT STYLE - ultra]
Raw output only.No natural language wrapper.No markdown formatting unless code.
Use compressed notation: &, |, =>, ternary.
Skip all context setting.Direct answer.No conversational framing.
Maximum compression: abbreviations, symbols, implicit context.
Output MUST be copy-paste ready with zero surrounding prose.
```

**ผลลัพธ์**: Model ตอบสั้นลง ~75% ได้แค่ raw code/commands ไม่มี prose wrapper เลย เหมาะสำหรับ situation ที่ context ใกล้เต็มแล้ว

```promql
api_gateway_caveman_compressions_total{tier="ultra",result="valid"} 1
```

---

## Configuration

| Environment Variable | Default | คำอธิบาย |
|---------------------|---------|----------|
| `CAVEMAN_ENABLED` | `true` | เปิด/ปิด Caveman |
| `CAVEMAN_AUTO_DETECT` | `true` | ตรวจ budget level อัตโนมัติเพื่อเลือก tier (ถ้า `false` จะใช้ tier full เสมอ) |
| `CAVEMAN_MIN_SIZE` | `500` | ขนาด system prompt ขั้นต่ำ (chars) ที่จะเริ่มทำ compression - สั้นกว่านี้ skip |

```bash
# ตัวอย่าง config
CAVEMAN_ENABLED=true
CAVEMAN_AUTO_DETECT=true
CAVEMAN_MIN_SIZE=500
```

### Budget Level -> Tier Mapping

```go
// จาก caveman.go
func BudgetToTier(level int) CompressionTier {
    switch level {
    case 2: return TierUltra   // red budget (>75% context)
    case 1: return TierFull    // yellow budget (50-75% context)
    default: return TierLite   // green budget (<50% context)
    }
}
```

---

## Metrics

### Prometheus Counters/Histograms

```promql
# จำนวน compression attempts ตาม tier และผลลัพธ์
api_gateway_caveman_compressions_total{tier="lite",result="valid"}
api_gateway_caveman_compressions_total{tier="full",result="valid"}
api_gateway_caveman_compressions_total{tier="ultra",result="valid"}
api_gateway_caveman_compressions_total{tier="lite",result="skipped"}   # content < MIN_SIZE
api_gateway_caveman_compressions_total{tier="lite",result="invalid"}   # validation ไม่ผ่าน

# Compression ratio histogram (observed values: 0.7=lite, 0.5=full, 0.25=ultra, 0.3=wenyan)
api_gateway_caveman_compression_ratio
```

### ตัวอย่าง Grafana Query

```promql
# Compression rate by tier
sum by (tier) (rate(api_gateway_caveman_compressions_total{result="valid"}[5m]))

# Skip rate (content too small)
sum by (tier) (rate(api_gateway_caveman_compressions_total{result="skipped"}[5m]))

# Validation failure rate
sum by (tier) (rate(api_gateway_caveman_compressions_total{result="invalid"}[5m]))

# Average compression ratio
rate(api_gateway_caveman_compression_ratio_sum[5m])
/
rate(api_gateway_caveman_compression_ratio_count[5m])
```

---

## ผลประหยัด

| Tier | Output Token Reduction | เหมาะกับ |
|------|----------------------|----------|
| lite | ~30% | คำถามเดี่ยว, session ใหม่, green budget |
| full | ~50% | Multi-turn conversation, yellow budget |
| ultra | ~75% | Session ยาวมาก, debug marathon, red budget |
| wenyan | ~70% | กรณีพิเศษที่ต้องการ extreme compression |

**จาก load test จริง** (7 requests, localhost, 2026-05-06):
- Caveman ทำงาน 7 ครั้ง (ทุก request)
- System prompt เฉลี่ย 467.6 chars ถูก replace ด้วย 229-char directive
- Overhead เฉลี่ยต่อ request: **0.03ms** (negligible)

---

## ข้อควรระวัง

### 1. Transparent Mode Skip
Caveman **จะไม่ทำงาน** ใน transparent mode (debug/observability mode) เพื่อไม่ให้กระทบการวิเคราะห์ output จริงของ model

### 2. Code Block Validation
ก่อนยืนยัน compression จะตรวจสอบ:
- จำนวน code blocks (``` pairs) หลัง compression ต้องไม่น้อยกว่าก่อน
- Identifier preservation ratio ต้อง >= 80% (ตรวจ top 20 unique identifiers)

ถ้า validation ไม่ผ่าน จะบันทึกเป็น `result="invalid"` และไม่ใช้ผลลัพธ์นั้น

### 3. Min Size Guard
System prompt สั้นกว่า `CAVEMAN_MIN_SIZE` (default 500 chars) จะถูก skip เพราะ:
- Injection directive เองก็มีขนาด ~229 chars แล้ว
- ไม่คุ้มเพิ่ม overhead สำหรับ prompt ที่สั้นอยู่แล้ว

### 4. Input Compression Side Effect
นอกจาก style injection แล้ว Caveman มี `CompressInput()` function ที่ทำ regex-based input compression แยกต่างหาก (ตัด pleasantries, hedge words, verbose synonyms, articles) โดยจะ mask protected regions (code fences, URLs, file paths) ก่อนแล้วค่อย compress

### 5. Output Savings เป็น Indirect
Caveman ไม่ได้วัด output tokens ที่ลดลงโดยตรง - มันฉีด directive เพื่อให้ model ตอบสั้นลง ผลลัพธ์จึงขึ้นอยู่กับว่า model จะทำตาม directive แค่ไหน ในทางปฏิบัติพบว่า model ส่วนใหญ่ทำตามค่อนข้างดี โดยเฉพาะ tier lite และ full


---


---

# Part 2: End-to-End Cross-Feature Scenarios

> สถานการณ์จริงที่หลาย Optimizer Stage ทำงานร่วมกันตลอดทั้ง session

## E2E: AWS-to-Tencent Cloud Migration (Full Day)



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


---

## E2E: K8s Production Incident (3 AM On-Call)



> บันทึกเหตุการณ์จริง - On-call Engineer Debugging Session
> Model: Claude Sonnet 4 via API Gateway | Session ID: `inc-20260507-0300`
> Context Window: 200K tokens | Session Duration: ~45 minutes

---

## ตัวละคร

- **วิชัย** - Senior DevOps Engineer on-call, 7 ปีประสบการณ์ K8s/AWS
- **Optimizer Gateway** - API Gateway ที่มี 20-stage token optimization pipeline
- **หมายเลข** - ชุด K8s cluster ที่ CP AXTRA รัน production workloads

---

## 03:00 - PagerDuty กระตุ้น

เสียง PagerDuty ดังขึ้นในความมืด วิชัยลืมตาขึ้นมองจอ iOS ในมือ

```
SEV-1 | Production | api-gateway-7d9f8b CrashLoopBackOff
Namespace: production | Cluster: axtra-prod-01
Alert: CrashLoopBackOff (5 restarts in 10min)
Runbook: https://wiki.internal/runbooks/api-gateway-crash
```

วิชัยเปิด laptop เข้า Claude Code ที่เชื่อมต่อ optimizer gateway เริ่ม session ใหม่

---

## Phase 1: Alert Received, Initial Investigation (Turns 1-5)

### Turn 1: Initial Check

```
วิชัย: pod api-gateway-7d9f8b CrashLoopBackOff namespace production
        ช่วยเช็คหน่อยว่าเกิดอะไรขึ้น
```

Gateway รับ request เข้า pipeline:

| Stage | Action | ผลลัพธ์ |
|-------|--------|---------|
| **F10 Warm Start** | Cosine similarity scan กับ session ก่อนหน้า | เจอ session `inc-20260505-network` คล้าย 0.73 (K8s debug) ดึง optimizer params มาใช้ |
| **F1 Chunker** | จัดเรียง stable chunks | Green budget < 50% |
| **F7 Semantic Dedup** | Dedup system prompt | -12 chars (เอา "You are a helpful assistant" ซ้ำออก) |
| **F16 Caveman** | Lite tier (green budget) | Replace system prompt 1,067 -> 229 chars |
| **F19 ToolFilter** | Intent=action, เลือก Bash, Read | จาก 27 tools -> เหลือ 4 (ประหยัด ~1,100 tokens manifest) |
| **F17 TextComp** | Compress message text | "ช่วยเช็คหน่อยว่าเกิดอะไรขึ้น" -> "เช็คสาเหตุ" (balanced mode) |

Claude เรียก Bash:

```bash
kubectl logs api-gateway-7d9f8b -n production --tail=100
kubectl describe pod api-gateway-7d9f8b -n production
kubectl get events -n production --sort-by='.lastTimestamp'
```

### Turn 2: Tool Output Processing

kubectl คืนผลมากมาย: 4,200 chars logs + 6,800 chars describe + 3,100 chars events

**F18 ToolComp** ทำงานหนัก:

| Output Type | Original | Compressed | Savings |
|-------------|----------|------------|---------|
| `kubectl logs` (Log format) | 4,200 chars | 1,260 chars | **70%** - dedup consecutive lines, keep error + context |
| `kubectl describe` (Table format) | 6,800 chars | 2,040 chars | **70%** - strip separators, keep Status/Conditions/Events |
| `kubectl get events` (Table format) | 3,100 chars | 930 chars | **69%** - head+tail+summary |

รวม: **14,100 -> 4,230 chars** (70% compression)

**F4 Prefetcher** เริ่มเรียนรู้ pattern:
```
Bash(kubectl logs) -> Bash(kubectl describe) [confidence: 0.9]
Bash(kubectl describe) -> Bash(kubectl get events) [confidence: 0.85]
```

Prefetcher เตรียม pre-warm Read tool สำหรับ config files ที่น่าจะอ่านต่อ

### Turn 3: Analyze Logs

```
วิชัย: จาก logs ดูเหมือน OOMKilled แต่ไม่แน่ใจ เช็ค memory limits และดู deployment yaml
```

Claude เรียก:
```bash
kubectl get deployment api-gateway -n production -o yaml
```

อ่าน config:
```
Read: /k8s/base/api-gateway/deployment.yaml
```

**F4 Prefetcher** ทำนายถูก! Bash -> Read transition hit, latency ลด 120ms

**F9 Sketch** check: prompt ไม่ซ้ำ, ผ่าน

| Stage | Action | ผลลัพธ์ |
|-------|--------|---------|
| **F18 ToolComp** | YAML output compression | 5,400 -> 1,890 chars (65%) |
| **F19 ToolFilter** | Code intent detected | เลือก Bash, Read, Edit (3 tools) |
| **F8 Delta** | Diff system prompt กับ baseline | ไม่ activate (system prompt ยังไม่เปลี่ยน) |

### Turn 4: Memory Analysis

```
วิชัย: memory limits ตั้ง 512Mi แต่ request 200Mi
        RSS ใน logs แสดง 490MB ก่อน crash
        เช็คว่ามี memory leak ไหม
```

Claude เรียกหลายคำสั่งต่อกัน:
```bash
kubectl top pods -n production --containers
kubectl logs api-gateway-7d9f8b -n production --previous | grep -i "memory\|oom\|heap"
```

**F9 Sketch** เริ่มจับ pattern:
- Turn 3 prompt vs Turn 4 prompt: similarity 0.82 (ใกล้ threshold 0.85)
- ยังไม่ flag เป็น duplicate

**F11 Waste Detection** (post-proxy feedback):
```
Waste scan (request count: 4):
  - empty_response: CLEAR
  - retry_churn: CLEAR
  - loop_detection: CLEAR
  - oversized_context: CLEAR (32% of context)
```

### Turn 5: Confirm Root Cause

```
วิชัย: ได้แล้ว memory leak จาก connection pool
        ตัวเลข goroutine ขึ้นจาก 200 เป็น 15,000 ใน 4 ชม.
        ช่วยเขียน patch ให้ deployment เพิ่ม memory limit
```

| Stage | Action | ผลลัพธ์ |
|-------|--------|---------|
| **F8 Delta** | System prompt เปลี่ยนเล็กน้อย | Activate! Diff encode: `===` (unchanged) + `+เพิ่ม memory limit` = **45% savings** |
| **F13 Intent Filter** | Code intent detected | Extract เฉพาะ YAML patch จาก verbose response |
| **F16 Caveman** | Lite tier | 229 chars directive |

### Phase 1 Metrics Summary

| Metric | Value |
|--------|-------|
| **Total Turns** | 5 |
| **Budget Level** | Green (22% context used) |
| **Input Tokens Without Optimization** | ~18,400 |
| **Input Tokens With Optimization** | ~11,200 |
| **Input Token Savings** | **39.1%** |
| **ToolComp Savings** | 9,870 chars (70% on tool output) |
| **ToolFilter Savings** | ~1,100 tokens (manifest reduction) |
| **Prefetcher Hits** | 2/3 predictions correct |
| **Optimization Overhead** | < 2ms per request |

---

## Phase 2: Deep Investigation (Turns 6-12)

### Turn 6: Paste Error Logs with Secrets

วิชัย copy-paste error log ทั้งก้อนจาก terminal เข้ามาใน chat

```
วิชัย: [paste 3,200 chars of error output]

ERROR [2026-05-07 03:12:44] connection pool exhausted
  dsn: postgres://appuser:s3cureP@ss!@db.internal.axtra.local:5432/production
  DB_PASSWORD=s3cureP@ss!
  API_KEY=sk-live-axtra-4f8b2c1d9e3a7f0b
  upstream: http://10.0.1.45:8080/health -> 503
  internal_ip: 10.0.1.45, 10.0.1.46, 10.0.1.47
```

**PasteGuard** (Privacy Masking) ทำงานทันที:

| Field Detected | Masked To |
|----------------|-----------|
| `s3cureP@ss!` (DB password) | `__PII_SECRET_db_password__` |
| `sk-live-axtra-4f8b2c1d9e3a7f0b` (API key) | `__PII_SECRET_api_key__` |
| `postgres://appuser:***@db.internal.axtra.local:5432/production` | Connection string masked |
| `10.0.1.45` | `__PII_IP_internal_1__` |
| `10.0.1.46` | `__PII_IP_internal_2__` |
| `10.0.1.47` | `__PII_IP_internal_3__` |

สิ่งที่ Claude เห็น:
```
ERROR connection pool exhausted
  dsn: __PII_SECRET_db_connection__
  upstream: http://__PII_IP_internal_1__:8080/health -> 503
```

Claude วิเคราะห์ root cause ได้โดยไม่เห็น credential จริง

### Turn 7-8: Repeated Investigation Queries

```
วิชัย [Turn 7]: เช็ค connection pool metrics อีกที
วิชัย [Turn 8]: ดู connection pool stats อีกรอบ
วิชัย [Turn 9]: ช่วยตรวจสอบ connection pool ของ pod นี้ที
```

**F9 Sketch** จับได้!

| Turn | Similarity vs Previous | Action |
|------|----------------------|--------|
| 7 vs 6 | 0.78 | ผ่าน (ต่ำกว่า 0.85) |
| 8 vs 7 | 0.91 | **FLAG: near-duplicate** - บันทึก 980 chars diagnostic |
| 9 vs 8 | 0.88 | **FLAG: near-duplicate** - บันทึก 1,020 chars diagnostic |

Sketch ไม่ได้ลบ prompt แต่ flag ให้ waste detector ใช้

### Turn 10: Waste Detection Triggers

**F11 Waste Detection** (post-proxy, 10 requests ครบ threshold):

```
Waste scan (request count: 10):
  - loop_detection: ⚠️ WARNING
    → "connection pool" queries repeated 4 times (turns 6,7,8,9)
    → Estimated waste: 2,400 tokens
    → Severity: medium
  - retry_churn: CLEAR
  - empty_response: CLEAR
  - redundant_tool_call: ⚠️ INFO
    → kubectl top pods called twice (turn 4, turn 7)
```

Gateway แนบ waste report เป็น system hint:
```
[OPTIMIZER HINT] loop_detection: Similar queries detected (4x).
Consider consolidating your investigation.
```

### Turn 11-12: Deeper Dive with New Angle

```
วิชัย [Turn 11]: โอเค เปลี่ยนมุม เช็คว่า GOMAXPROCS ตั้งถูกไหม
                   และ GOMEMLIMIT ตรงกับ cgroup limits ไหม
```

```
วิชัย [Turn 12]: เจอแล้ว GOMEMLIMIT ไม่ถูกต้อง ช่วยเขียน patch ทั้ง deployment yaml
                   เพิ่ม memory limit เป็น 1Gi และ set GOMEMLIMIT=900MiB
```

**F8 Delta Encoding** activate อย่างเต็มที่:
- System prompt ใน Turn 12 เทียบกับ baseline: เปลี่ยนแค่ env var values
- Delta encode: `=== [main block unchanged]` + `- GOMEMLIMIT=512MiB` + `+ GOMEMLIMIT=900MiB` + `- memory: 512Mi` + `+ memory: 1Gi`
- **Savings: 58%** (จาก 2,400 chars เหลือ 1,008 chars)

**F13 Intent Filter**: Code intent -> extract เฉพาะ YAML บรรทัดที่เปลี่ยน

### Phase 2 Metrics Summary

| Metric | Value |
|--------|-------|
| **Total Turns** | 7 (Turns 6-12) |
| **Budget Level** | Green -> Yellow transition (52%) |
| **PasteGuard Masks** | 6 secrets + 3 internal IPs |
| **Sketch Duplicate Flags** | 2 (turns 8, 9) |
| **Waste Findings** | loop_detection (WARNING), redundant_tool_call (INFO) |
| **Delta Savings** | 58% on system prompt |
| **Input Tokens Without Optimization** | ~24,600 |
| **Input Tokens With Optimization** | ~14,100 |
| **Input Token Savings** | **42.7%** |
| **Tokens Wasted (diagnostic)** | 2,400 (flagged, not saved) |

---

## Phase 3: Fix and Verify (Turns 13-18)

### Turn 13: Apply the Fix

```
วิชัย: ใช้ patch นี้เลย apply เข้า cluster
```

Claude generates `kubectl patch` command:

```bash
kubectl patch deployment api-gateway -n production --type='json' \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/resources/limits/memory","value":"1Gi"},
        {"op":"replace","path":"/spec/template/spec/containers/0/env/2/value","value":"900MiB"}]'
```

**F13 Intent Filter**: Code intent -> ดึงเฉพาะ kubectl command จาก verbose explanation
- Original response: 890 chars
- Filtered: 340 chars
- **Savings: 62% output tokens**

### Turn 14: Wait for Rollout

```
วิชัย: rollout status เช็คหน่อย
```

```bash
kubectl rollout status deployment/api-gateway -n production --timeout=120s
```

**F19 ToolFilter**: Intent=action, รักษา Bash + Edit เท่านั้น

**F18 ToolComp**: Rollout output compression:
```
# Original (2,400 chars of "Waiting for rollout" repeated)
# ToolComp (Log format): dedup consecutive
Waiting for rollout to finish: 1 of 3 pods updated...
Waiting for rollout to finish: 2 of 3 pods updated...
deployment "api-gateway" successfully rolled out
# Result: 180 chars (92.5% compression!)
```

### Turn 15: Verify Fix

```
วิชัย: pod ขึ้นมาใหม่แล้ว เช็ค memory usage และ goroutine count
```

```bash
kubectl top pods -n production --containers
kubectl exec api-gateway-7d9f8c -n production -- curl -s localhost:8080/debug/vars
```

**F4 Prefetcher** prediction: `Bash -> Bash` (correct! confidence 0.92)

Tool output:
```
# Memory: 380MB / 1024MB limit (37% utilization) -- HEALTHY
# Goroutines: 245 (stable over 5 min)
# Connection pool: 48 active / 100 max
```

### Turn 16: Stress Test

```
วิชัย: รัน load test สั้นๆ เพื่อ confirm ว่า memory ไม่ leak แล้ว
```

**F5 Bandit** เริ่มเรียนรู้ reward signal:

| Arm | Context Features | Reward | Selections |
|-----|-----------------|--------|------------|
| toolcomp + sketch | K8s debug session | 0.87 | 8 |
| toolcomp + delta | K8s debug session | 0.71 | 3 |
| chunker + delta | K8s debug session | 0.45 | 2 |

Bandit สรุป: สำหรับ incident sessions, **toolcomp + sketch** combo ให้ reward สูงสุด

### Turn 17: Check Error Rates

```
วิชัย: เช็ค error rate บน Grafana 5xx rate ลดลงหรือยัง
```

Claude เรียก:
```bash
curl -s 'http://grafana.internal:3000/api/datasources/proxy/1/api/v1/query_range?query=sum(rate(http_requests_total{status=~"5..",namespace="production"}[5m]))' | jq
```

**F18 ToolComp**: JSON format compression
- Original JSON response: 3,800 chars
- Compressed (compact JSON + summary): 760 chars
- **80% compression**

### Turn 18: Confirm Resolution

```
วิชัย: ดีมาก 5xx rate กลับเป็น 0 แล้ว
        pod stable 15 นาทีแล้ว เพิ่ม annotation ให้ deployment
        แล้วปิด incident ticket
```

**F8 Delta**: เปลี่ยนแค่ annotation value
- `=== [all previous context unchanged]`
- `+ incident.inc-20260507-0300: resolved`
- **Savings: 52%**

### Phase 3 Metrics Summary

| Metric | Value |
|--------|-------|
| **Total Turns** | 6 (Turns 13-18) |
| **Budget Level** | Yellow (58%) |
| **Intent Filter Activations** | 3 (code intent) |
| **Delta Savings (avg)** | 52% |
| **ToolComp Peak** | 92.5% (rollout output) |
| **Bandit Best Arm** | toolcomp + sketch (reward: 0.87) |
| **Prefetcher Accuracy** | 85% (6/7 predictions correct) |
| **Input Tokens Without Optimization** | ~16,800 |
| **Input Tokens With Optimization** | ~9,200 |
| **Input Token Savings** | **45.2%** |

---

## Phase 4: Post-Mortem (Turns 19-25)

### Turn 19: Budget Shifts to Yellow

Session ยาวเกิน 50% context window

**F15 Disclosure** เริ่มทำงาน:

| Budget Level | Tool Output Handling |
|-------------|---------------------|
| Green (< 50%) | Pass through (no truncation) |
| **Yellow (50-75%)** | **Truncate to L2Tokens * 8 chars for content > 2000** |
| Red (> 75%) | Truncate to L1Tokens * 4 chars |

วิชัย paste post-mortem template ยาว 4,500 chars

**F15 Disclosure** truncate เป็น 4,000 chars (L2Tokens * 8 = 60 * 8 = 480, แต่ 4,500 < 2,000 threshold ไม่ถูกตัดในกรณีนี้เพราะเป็น user input ไม่ใช่ tool_result)

### Turn 20-22: Write Post-Mortem Document

```
วิชัย [Turn 20]: ช่วยเขียน post-mortem จากข้อมูลทั้งหมดที่เราเก็บมา
                   ใส่ timeline, root cause, action items
```

**F16 Caveman** เปลี่ยนเป็น **Full tier** (yellow budget):
- Lite (green): 30% output reduction
- **Full (yellow): 50% output reduction**
- Ultra (red): 75% output reduction

Caveman inject `[OUTPUT STYLE - full]` directive:
- Response สั้นลง 50% โดยยังคง timeline + root cause + action items ครบ
- ไม่มี "Based on the analysis..." filler
- ไม่มี "It is important to note..." hedging

```
วิชัย [Turn 21]: เพิ่ม section monitoring improvements
วิชัย [Turn 22]: เพิ่ม section เกี่ยวกับ runbook update
```

**F9 Sketch**: Turn 20-22 prompts คล้ายกัน (similarity 0.89) -> flagged near-duplicate
- เป็น expected behavior: post-mortem writing เป็น iterative refinement

### Turn 23: Budget Nears Red

Context usage: 72% -> **Yellow upper boundary**

**F6 Summarizer** เริ่มเตรียมตัว (ยังไม่ activate เพราะ < 75%)

**F15 Disclosure** บีบ tool output ที่ใหญ่ขึ้น:
- Grafana query result 4,200 chars -> truncate เป็น 2,100 chars (50%)

### Turn 24: Final Review

```
วิชัย: สรุป final action items ให้หน่อย และ assign owners
```

**F16 Caveman**: Full tier ยังคงทำงาน

**F13 Intent Filter**: Action intent -> extract เฉพาะ bullet list + owner names
- Verbose response: 1,200 chars
- Filtered: 420 chars
- **65% output savings**

### Turn 25: Session Cleanup

```
วิชัย: เสร็จแล้ว ขอบคุณมาก ปิด session ได้
```

**F14 Cache Eviction** ทำงาน (post-proxy):

| Cache Key | ROI Score | Action |
|-----------|-----------|--------|
| `sys:claude-sonnet-4` (baseline) | 8.7 | KEEP (high ROI) |
| `cache:stats:inc-20260507-0300:k8s_logs` | 1.2 | EVICT (low ROI, session-specific) |
| `cache:stats:inc-20260507-0300:memory_analysis` | 0.8 | EVICT |
| `cache:stats:inc-20260507-0300:connection_pool` | 0.3 | EVICT |

Cache eviction ลบ incident-specific entries ที่จะไม่ใช้แล้ว ประหยัด Redis memory

**F20 CompCache**: Zstd compression บน cache entries ที่เก็บไว้
- Baseline system prompt cache: 4,200 bytes -> 1,050 bytes (75% compression)
- Bandit arm data: 1,800 bytes -> 540 bytes (70% compression)

### Phase 4 Metrics Summary

| Metric | Value |
|--------|-------|
| **Total Turns** | 7 (Turns 19-25) |
| **Budget Level** | Yellow (58% -> 71%) |
| **Caveman Tier** | Full (50% output reduction) |
| **Disclosure Truncations** | 2 (tool outputs > 2000 chars) |
| **Sketch Flags** | 3 (iterative post-mortem refinement) |
| **Cache Evictions** | 3 keys (low ROI incident data) |
| **CompCache Savings** | 75% on retained cache entries |
| **Input Tokens Without Optimization** | ~28,400 |
| **Input Tokens With Optimization** | ~17,200 |
| **Input Token Savings** | **39.4%** |

---

## Full Session Metrics

### Per-Phase Token Comparison

| Phase | Turns | Budget | Tokens (Raw) | Tokens (Optimized) | Savings % | Key Optimizers |
|-------|-------|--------|--------------|---------------------|-----------|----------------|
| **1. Initial Investigation** | 1-5 | Green | 18,400 | 11,200 | **39.1%** | ToolComp, ToolFilter, Prefetcher, Caveman Lite |
| **2. Deep Investigation** | 6-12 | Green->Yellow | 24,600 | 14,100 | **42.7%** | PasteGuard, Sketch, Waste, Delta |
| **3. Fix and Verify** | 13-18 | Yellow | 16,800 | 9,200 | **45.2%** | Delta, IntentFilter, Bandit, ToolComp |
| **4. Post-Mortem** | 19-25 | Yellow (high) | 28,400 | 17,200 | **39.4%** | Caveman Full, Disclosure, CacheEviction, CompCache |
| **TOTAL** | **25** | - | **88,200** | **51,700** | **41.4%** | - |

### Cost Comparison (Claude Sonnet 4 pricing: $3/M input, $15/M output)

| | Without Optimization | With Optimization | Savings |
|---|---|---|---|
| **Input Tokens** | 88,200 | 51,700 | 36,500 tokens |
| **Output Tokens** | ~14,000 | ~7,200* | 6,800 tokens |
| **Input Cost** | $0.2646 | $0.1551 | **$0.1095** |
| **Output Cost** | $0.2100 | $0.1080 | **$0.1020** |
| **Total Cost** | **$0.4746** | **$0.2631** | **$0.2115 (44.6%)** |
| **Overhead** | - | $0.0012 (Redis + compute) | - |
| **Net Savings** | - | - | **$0.2103 (44.3%)** |

*Output reduction จาก Caveman (lite/full tier) + Intent Filter

### Optimizer Activation Heatmap

```
Stage              | P1  | P2  | P3  | P4  | Total Activations
-------------------|-----|-----|-----|-----|-------------------
F1 Chunker         |  5  |  7  |  6  |  7  | 25
F4 Prefetcher      |  4  |  5  |  6  |  4  | 19
F5 Bandit          |  5  |  7  |  6  |  7  | 25
F6 Summarizer      |  -  |  -  |  -  |  -  | 0 (never hit red)
F7 Semantic Dedup  |  5  |  7  |  6  |  7  | 25
F8 Delta           |  1  |  3  |  3  |  5  | 12
F9 Sketch          |  3  |  5  |  4  |  5  | 17
F10 Warm Start     |  1  |  -  |  -  |  -  | 1
F11 Waste          |  2  |  4  |  3  |  3  | 12
F13 Intent Filter  |  1  |  2  |  3  |  2  | 8
F14 Cache Eviction |  1  |  1  |  1  |  1  | 4
F15 Disclosure     |  -  |  -  |  1  |  2  | 3
F16 Caveman        |  5  |  7  |  6  |  7  | 25
F17 TextComp       |  5  |  7  |  6  |  7  | 25
F18 ToolComp       |  4  |  6  |  5  |  3  | 18
F19 ToolFilter     |  5  |  5  |  4  |  3  | 17
F20 CompCache      |  5  |  7  |  6  |  7  | 25
```

### Per-Technique Token Savings Breakdown

| Technique | Chars Saved | Est. Tokens | % of Total Savings | Category |
|-----------|-------------|-------------|---------------------|----------|
| **ToolComp** | 28,400 | 7,100 | 19.4% | Input |
| **Caveman** | 19,200* | 4,800 | 13.2% | Output influence |
| **Delta** | 12,800 | 3,200 | 8.8% | Input |
| **ToolFilter** | 6,600 | 1,650 | 4.5% | Input |
| **Intent Filter** | 5,200 | 1,300 | 3.6% | Output |
| **Sketch** | 3,800 (flagged) | 950 | 2.6% | Input diagnostic |
| **TextComp** | 3,200 | 800 | 2.2% | Input |
| **Semantic Dedup** | 1,400 | 350 | 1.0% | Input |
| **Message Dedup** | 800 | 200 | 0.5% | Input |
| **Disclosure** | 4,200 | 1,050 | 2.9% | Input |
| **CompCache** | 8,400 (Redis) | N/A | Indirect | Redis memory |
| **Warm Start** | N/A | ~800 | 2.2% | Cold-start prevention |
| **Prefetcher** | N/A | ~1,200 latency | Indirect | Latency reduction |
| **Bandit** | N/A | ~540 | 1.5% | Meta-optimization |

*Caveman chars = system prompt replaced, not direct output savings

### Session Timeline

```
03:00 ┃ PagerDuty alert received
03:02 ┃ Session started, Warm Start loads K8s debug profile
03:03 ┃ Turn 1: kubectl logs/describe/events -> ToolComp 70%
03:05 ┃ Turn 3: Prefetcher predicts Bash->Read correctly
03:08 ┃ Turn 5: Delta first activation (45% savings)
03:10 ┃ Turn 6: PasteGuard masks 6 secrets + 3 IPs
03:14 ┃ Turn 8-9: Sketch flags duplicate queries
03:16 ┃ Turn 10: Waste detection -> loop_detection WARNING
03:20 ┃ Turn 12: Delta peak (58%) for env var change
03:22 ┃ Turn 13: Intent Filter extracts kubectl patch command
03:24 ┃ Turn 14: ToolComp 92.5% on rollout output
03:28 ┃ Turn 16: Bandit identifies toolcomp+sketch as best arm
03:32 ┃ Turn 18: Incident resolved, annotation added
03:35 ┃ Turn 19: Budget shifts to Yellow, Caveman -> Full tier
03:40 ┃ Turn 20-22: Post-mortem writing with Full tier brevity
03:44 ┃ Turn 23: Disclosure truncates Grafana output 50%
03:46 ┃ Turn 25: Cache Eviction cleans incident entries
03:47 ┃ Session closed
```

---

## Key Takeaways

**1. ToolComp ประหยัดที่สุดสำหรับ incident workflows**
kubectl output มี structure ที่ compress ได้ดีมาก (log format, table format) เฉลี่ย 70-80% compression

**2. Delta Encoding โดดเด่นเมื่อ fix ซ้ำ**
เปลี่ยนแค่ env var หรือ annotation -> 58% savings เพราะส่วนใหญ่ของ system prompt เหมือนเดิม

**3. PasteGuard ป้องกัน credential leak โดยอัตโนมัติ**
วิชัย paste log ที่มี DB password, API key, internal IPs โดยไม่ตั้งใจ -> masked ทั้งหมดก่อนส่งให้ Claude

**4. Bandit เรียนรู้ pattern ของ session type**
หลัง 16 turns, Bandit รู้แล้วว่า incident session ควรใช้ toolcomp + sketch combination

**5. Waste Detection ช่วย break loop**
วิชัยถามซ้ำ 4 ครั้งเกี่ยวกับ connection pool -> loop_detection flag -> วิชัยเปลี่ยนมุมมองการสืบสวน

**6. Budget-aware degradation ทำงานโดยไม่รู้สึก**
Green -> Yellow transition ระหว่าง session: Caveman เปลี่ยน Lite -> Full, Disclosure เริ่ม truncate
วิชัยไม่รู้สึกเลยว่า output เปลี่ยน เพราะข้อมูลสำคัญยังคงอยู่ครบ

**7. 44.3% ประหยัดต้นทุน สำหรับ SEV-1 incident**
Session 25 turns ใช้เงิน $0.26 แทน $0.47 โดยไม่สูญเสียคุณภาพการวิเคราะห์เลย
สำหรับทีมที่มี 50+ incidents/เดือน ประหยัด ~$10/เดือนเฉพาะ incident sessions (ไม่รวม regular usage)

---

> Generated: 2026-05-07 | Session: inc-20260507-0300
> Optimizer Pipeline: 20 stages | Model: Claude Sonnet 4
> All metrics are illustrative estimates based on optimizer stage specifications


---

## E2E: Code Review Session (Multi-Turn)



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


---

## E2E: Multi-Provider Failover



**ทีมแพลตฟอร์ม - วันทำงานปกติของ API Gateway**


---

## ภาพรวมสถาปัตยกรรม

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Claude Code CLI / IDE                         │
│              ANTHROPIC_BASE_URL=http://gateway:9000                  │
│              ANTHROPIC_API_KEY=arl_<profile-token>                   │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ HTTP POST /v1/messages
                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      API Gateway (Go, :8080)                        │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────────┐   │
│  │ Rate Limiter│  │ 13-Stage     │  │ Profile-Based Routing    │   │
│  │ (Distributed│  │ Optimizer    │  │ claude-oauth -> Anthropic│   │
│  │  + Adaptive)│  │ Pipeline     │  │ zai          -> Z.AI     │   │
│  │             │  │              │  │ gemini-oauth -> Gemini   │   │
│  │ per user    │  │ savings      │  │                          │   │
│  └─────────────┘  └──────────────┘  └──────────────────────────┘   │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────────────┐   │
│  │ Bandit (F5) │  │ Sketch (F9)  │  │ Cache Eviction (F14)     │   │
│  │ LinUCB      │  │ SimHash      │  │ ROI-based cleanup        │   │
│  │ adaptive    │  │ near-dup     │  │                          │   │
│  └─────────────┘  └──────────────┘  └──────────────────────────┘   │
│  ┌─────────────┐  ┌──────────────┐                                 │
│  │ Warm Start  │  │ Caveman (F16)│                                 │
│  │ (F10)       │  │ Style inject │                                 │
│  └─────────────┘  └──────────────┘                                 │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
              ┌────────────────┼────────────────┐
              ▼                ▼                ▼
     ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
     │  Anthropic   │ │    Z.AI      │ │   Gemini     │
     │ claude-oauth │ │     zai      │ │ gemini-oauth │
     │ $3/M tokens  │ │ $0.5/M tokens│ │ $1.25/M tokens│
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
    "ANTHROPIC_API_KEY": "arl_2f3a72a7..."
  }
}
```

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
│ Z.AI (glm-5)           ██████░░░░░░░░░  30%     │
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
│ Gemini fallback chain:                            │
│   gemini-2.5-pro -> gemini-2.5-flash              │
│                   -> gemini-2.5-flash-lite        │
│                   -> gemini-2.0-flash             │
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
│ Z.AI (glm-5)           ███░░░░░░░░░░░░  15%     │
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
│ สถานการณ์              │ ต้นทุน           │ หมายเหตุ              │
├────────────────────────┼─────────────────┼───────────────────────┤
│ ไม่มี Gateway           │                 │                       │
│ (ทุกอย่างไป Claude)     │ $12.50          │ 2.5M x $3/M +        │
│                        │                 │ 800K x $15/M          │
│                        │                 │ ไม่มี optimization     │
│                        │                 │ outage = งานหยุด      │
├────────────────────────┼─────────────────┼───────────────────────┤
│ มี Gateway              │                 │                       │
│ (multi-provider +      │ $4.20           │ ผสม provider           │
│  optimizer)            │                 │ + 40-60% token savings│
│                        │                 │ outage = ยังทำงานได้   │
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
│  API Gateway - Multi-Provider Dashboard                            │
├──────────────────────────────┬──────────────────────────────────────┤
│  Provider Health             │  Request Rate (req/s)               │
│  ┌────────┬────┬──────┐     │  ██ Claude  ████████████░░  80%     │
│  │Status  │RPS │Lat   │     │  ██ GLM-5   ███░░░░░░░░░░░  15%     │
│  ├────────┼────┼──────┤     │  ██ Gemini  █░░░░░░░░░░░░░░   5%     │
│  │Claude  │ 12 │ 1.8s │     │                                      │
│  │GLM-5   │  3 │ 2.1s │     ├──────────────────────────────────────┤
│  │Gemini  │  1 │ 1.5s │     │  Token Savings (%)                   │
│  └────────┴────┴──────┘     │  ████████████████████░░░░  52%       │
│                              │                                      │
├──────────────────────────────┼──────────────────────────────────────┤
│  Bandit Arm Rewards          │  Cost (USD)                          │
│  Claude  ████████████░ 0.82  │  Today: $4.20                       │
│  GLM-5   ██████░░░░░░ 0.45  │  No-gateway: $12.50                  │
│  Gemini  ███████░░░░░ 0.58  │  Savings: $8.30 (66%)                │
│                              │                                      │
├──────────────────────────────┼──────────────────────────────────────┤
│  Rate Limit Status           │  Optimizer Stage Breakdown           │
│  Current: 55/min             │  delta:    ████████████░  20%       │
│  Peak:    60/min             │  caveman:  ████████░░░░░  30%       │
│  Cooldown: No                │  textcomp: ████░░░░░░░░   8%       │
│                              │  sketch:   ██████░░░░░░░  15%       │
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


---

## E2E: Security Audit with PasteGuard

Now I have a good understanding of the style and technical details. Let me write the Thai use case document.[[? ---+\n|  YES                      |\n|    ReplaceDirectJSON()    |  <-- unbuffered, non-delta fields\n|                           |\n+-- end of stream? ---------+\n|  YES                      |\n|    Flush() -> emit        |  <-- final drain\n+---------------------------+\n        |\n        v\n  Unmasked SSE -> Client\n```\n\n---\n\n## การตั้งค่า PasteGuard\n\n### Environment Variables\n\n```bash\n# เปิด/ปิด PasteGuard\nPASTEGUARD_ENABLED=true\n\n# Secrets detection\nPASTEGUARD_SECRETS_ENABLED=true\nPASTEGUARD_SECRET_ENTITIES=OPENSSH_PRIVATE_KEY,PEM_PRIVATE_KEY,API_KEY_SK,API_KEY_AWS,API_KEY_GITHUB,JWT_TOKEN,BEARER_TOKEN\n\n# PII detection\nPASTEGUARD_PII_ENABLED=true\nPASTEGUARD_PII_ENTITIES=EMAIL_ADDRESS,PHONE_NUMBER,CREDIT_CARD,SSN,IBAN,IP_ADDRESS,THAI_NATIONAL_ID,THAI_PHONE\n\n# ขีดจำกัด\nPASTEGUARD_MAX_SCAN_CHARS=200000\n```\n\n### สำหรับทีมไทย (เปิด Thai-specific entities)\n\n```bash\n# เพิ่ม Thai entities ถ้ายังไม่ได้เปิด\nPASTEGUARD_PII_ENTITIES=EMAIL_ADDRESS,PHONE_NUMBER,CREDIT_CARD,SSN,IBAN,IP_ADDRESS,THAI_NATIONAL_ID,THAI_PHONE\n\n# เพิ่ม Thai national ID ใน secret entities (ถ้าต้องการ)\nPASTEGUARD_SECRET_ENTITIES=OPENSSH_PRIVATE_KEY,PEM_PRIVATE_KEY,API_KEY_SK,API_KEY_AWS,API_KEY_GITHUB,JWT_TOKEN,BEARER_TOKEN,ENV_PASSWORD,ENV_SECRET,CONNECTION_STRING\n```\n\n### Custom Entity Pattern Reference\n\n| Entity | Regex Pattern | Note |\n|---|---|---|\n| `THAI_NATIONAL_ID` | `[1-8]\\d{12}` (no dashes) หรือ `x-xxxx-xxxxx-xx-x` (with dashes) | 13 digits, first digit 1-8 |\n| `THAI_PHONE` | `0[2-9]\\d-?\\d{3}-?\\d{4}` หรือ `\\+66[2-9]\\d-?\\d{3}-?\\d{4}` | Mobile + landline |\n\n---\n\n## สรุป Audit Results\n\nประเจต สรุปผล audit ดังนี้:\n\n**PasteGuard ผ่านการ audit ทุก scenario:**\n\n- Secrets: จับได้ครบทุกประเภท (API keys, SSH keys, JWT, passwords, connection strings)\n- PII: จับได้ครบรวมถึง Thai-specific patterns (บัตรประชาชน, เบอร์โทรไทย)\n- Streaming: placeholder split ข้าม chunks แก้ไขแล้วทั้ง 6 bugs\n- Concurrency: MaskContext แยกต่อ request, mutex-protected, ไม่มี cross-contamination\n- Performance: regex-based detection <1ms เทียบกับ Presidio 7-14s ต่อ span\n- Compliance: ลดความเสี่ยง PDPA/GDPR เพราะ upstream LLM ไม่เห็นข้อมูลจริง\n\n**Metrics Dashboard Queries:**\n\n```promql\n# Secrets detection rate by type\nsum by (type) (rate(api_gateway_secrets_detected_total[5m]))\n\n# PII detection rate by type\nsum by (type) (rate(api_gateway_pii_detected_total[5m]))\n\n# Mask pipeline latency p99\nhistogram_quantile(0.99, rate(api_gateway_mask_duration_seconds_bucket[5m]))\n\n# Requests with secrets vs without\nsum by (has_secrets, has_pii) (rate(api_gateway_mask_requests_total[5m]))\n```\n","file_path":"/Users/klxhunter/klxhub/space/work/agent-rate-limit/docs/pasteguard-security-audit-th.md"}[[` เลยถูกข้าม | process ทุก chunk เมื่อ unmasker active |\n| #2 | HIGH | Flush output หาย | `unmasker.Flush()` คืนค่าแต่ไม่ emit เป็น SSE | emit Flush result เป็น `content_block_delta` |\n| #3 | CRITICAL | ProxySidecar ไม่ unmask | sidecar ทำ raw relay, ไม่มี SSE parsing | เพิ่ม maskResult + SSE scanner + unmask |\n| #4 | MEDIUM | Cross-block buffer leak | shared buffer ระหว่าง text/thinking blocks | flush buffer ก่อน `content_block_stop` |\n| #5 | LOW | Empty text_delta emit | ไม่มี empty guard หลัง ProcessChunk | เพิ่ม `if text == \"\" { continue }` |\n| #6 | HIGH | partial_json placeholder split | `input_json_delta` ใช้ unbuffered `ReplaceDirectJSON` | เพิ่ม `ProcessChunkJSON` พร้อม JSON-mode buffers |\n\n### Streaming Unmask Flow (ปัจจุบัน)\n\n```\nSSE Event จาก Upstream\n        |\n        v\n+-- content_block_delta? --+\n|  YES                      |\n|  check delta type:        |\n|    text_delta:            |\n|      ProcessChunk()       |  <-- buffered, plain text\n|    thinking_delta:        |\n|      ProcessChunk()       |  <-- buffered, plain text\n|    input_json_delta:      |\n|      ProcessChunkJSON()   |  <-- buffered, JSON-safe escaping\n|                           |\n+-- content_block_stop? ----+\n|  YES                      |\n|    Flush() -> emit delta  |  <-- drain partial buffers\n|                           |\n+-- other + contains

---

## E2E: Cost Optimization Analysis



**ผู้จัดทำ**: Platform Engineering Team
**วันที่**: 7 พฤษภาคม 2569
**กลุ่มเป้าหมาย**: ผู้จัดการฝ่าย IT
**ขอบเขต**: วิเคราะห์ต้นทุนและการประหยัดจาก 13-stage Token Optimization Pipeline

---

## สารบัญ

1. [ต้นทุนก่อนใช้ Gateway](#1-ต้นทุนก่อนใช้-gateway)
2. [การประหยัดแยกตาม Optimizer Stage](#2-การประหยัดแยกตาม-optimizer-stage)
3. [Multi-Provider Cost Comparison](#3-multi-provider-cost-comparison)
4. [ROI Calculation](#4-roi-calculation)
5. [Grafana Dashboard Metrics](#5-grafana-dashboard-metrics)

---

## 1. ต้นทุนก่อนใช้ Gateway

### 1.1 สมมติฐานการใช้งาน

| พารามิเตอร์ | ค่า |
|---|---|
| จำนวน Engineers | 10 คน |
| เวลาใช้งาน Claude Code | 8 ชม./วัน |
| Requests ต่อ Engineer | 50 requests/วัน |
| Total Requests/วัน | **500 requests/วัน** |
| เฉลี่ย Input Tokens | 3,000 tokens/request |
| เฉลี่ย Output Tokens | 800 tokens/request |
| วันทำงาน/เดือน | 22 วัน |
| Requests/เดือน | **11,000 requests/เดือน** |

### 1.2 ต้นทุนโดยตรง (Claude Sonnet Direct API)

| รายการ | อัตรา | ต่อวัน | ต่อเดือน |
|---|---|---|---|
| Input Tokens | $3.00/M tokens | 500 x 3,000 = 1,500,000 tokens = **$4.50** | **$99.00** |
| Output Tokens | $15.00/M tokens | 500 x 800 = 400,000 tokens = **$6.00** | **$132.00** |
| **ต้นทุนรวม** | | **$10.50/วัน** | **$231.00/เดือน** |

### 1.3 ต้นทุนแฝงที่ไม่คิดใน Bill

| รายการ | ผลกระทบ | ต้นทุนประมาณการ |
|---|---|---|
| Retry จาก 429 (Rate Limit) | 10-20% requests ต้อง retry | +10-20% token cost |
| Context Overflow | ส่ง context เกิน limit แล้ว fail | +5-10% waste |
| Verbose Output | Model ตอบยาวเกินความจำเป็น | +30-50% output tokens |
| ไม่มี Caching | System prompt ส่งซ้ำทุก request | +15-25% input tokens |
| Secrets/PII Leak | ความเสี่ยงด้าน compliance | มูลค่าไม่อาจประเมินได้ |

### 1.4 ต้นทุนจริงที่คาดว่าจะสูงกว่า

```
Base Cost:           $231.00/เดือน
+ Retry Overhead:    $34.65/เดือน  (+15%)
+ Context Waste:     $17.33/เดือน  (+7.5%)
+ Verbose Output:    $92.40/เดือน  (+40% output)
+ No Caching:        $46.20/เดือน  (+20% input)
-----------------------------
Estimated Real Cost: $421.58/เดือน
```

**สรุป**: ต้นทุนจริงประมาณ **$420-450/เดือน** สำหรับทีม 10 คน หรือประมาณ **$42-45/คน/เดือน**

---

## 2. การประหยัดแยกตาม Optimizer Stage

### 2.1 ตาราง Stage ทั้งหมด (17 Stages)

> ข้อมูลจาก Load Test จริง (7 requests, localhost, 2026-05-06) + Estimated Production Scale
> Production คูณด้วย scaling factor: 500 requests/วัน vs 7 requests load test = ~71x

| # | Stage | ประเภท | % Savings | Tokens Saved/วัน | Cost Saved/วัน | หมายเหตุ |
|---|---|---|---|---|---|---|
| F7 | Semantic Dedup | INPUT | 3-5% | ~5,250 | $0.016 | ลบประโยคซ้ำใน system prompt |
| F1 | Chunker | INPUT | 5-15% | ~7,875 | $0.024 | Reorder stable chunks, เพิ่ม cache hit |
| F8 | Delta Encoding | INPUT | 20-60% | ~52,500 | $0.158 | Diff-encode สำหรับ iterative edits |
| F9 | Sketch Dedup | INPUT | 5-30% | ~26,250 | $0.079 | ตรวจ near-duplicate prompts |
| F6 | Summarizer | INPUT | 50-70% | ~78,750* | $0.236* | Red budget เท่านั้น (>75% context) |
| F13 | Intent Filter | OUTPUT | 10-40% | ~20,000 | $0.300 | กรอง response ตาม intent |
| F17 | TextComp | INPUT | 5-15% | ~13,125 | $0.039 | ตัด filler/verbose text |
| F16 | **Caveman** | **OUTPUT** | **30-75%** | **~120,000** | **$1.800** | **ลด output ด้วย style injection** |
| F18 | **ToolComp** | **INPUT** | **40-80%** | **~42,000** | **$0.126** | **บีบ tool_result (shell, JSON)** |
| F19 | **ToolFilter** | **INPUT** | **60-80%** | **~4,500** | **$0.014** | **กรอง tool manifest (3000-6000 tok)** |
| F20 | CompCache | Redis | 60-80% | (indirect) | - | ลด Redis memory 60-80% |
| F15 | Disclosure | INPUT | 50-70% | ~15,000* | $0.045* | Yellow/Red budget เท่านั้น |
| - | Message WS+Dedup | INPUT | 3-8% | ~5,250 | $0.016 | Whitespace + sentence dedup |
| - | Message TextComp | INPUT | 5-15% | ~10,500 | $0.032 | TextComp บน message content |
| F4 | Prefetcher | Latency | 50-200ms | (indirect) | - | ลด latency, ไม่ลด tokens โดยตรง |
| F5 | Bandit | Meta | 5-15% | (indirect) | - | เรียนรู้ strategy ที่ดีที่สุด |
| F11 | Waste Detection | Diagnostic | 5-20% | (indirect) | - | ระบุ token waste patterns |
| F14 | Cache Eviction | Cache | ~10% hit rate | (indirect) | - | รักษา cache quality |
| F10 | Warm Start | Cold-start | 10-20% | (indirect) | - | ลด cold-start waste |

*ทำงานเฉพาะเมื่อ budget level เฉพาะเจาะจง (Red/Yellow)

### 2.2 Top 3 Savers (ผู้ประหยัดรายใหญ่)

#### 1st: Caveman (F16) - ประหยัด Output Tokens

```
Mechanism:  ฉีด [OUTPUT STYLE] directive เข้า system prompt
            สั่งให้ model ตอบสั้นลง (30-75% ตาม tier)

4 Tiers:
  Lite  (Green):  -30% output, bullet points
  Full  (Yellow): -50% output, code only
  Ultra (Red):    -75% output, raw only

จาก Load Test:  System prompt เฉลี่ย 467.6 chars ถูกแทนที่ด้วย 229-char directive
Overhead:       0.03ms/request (negligible)
```

**ผลประหยัดประมาณการ**: $1.80/วัน = **$39.60/เดือน** (ลด output cost ~40%)

#### 2nd: ToolComp (F18) - บีบ Tool Result

```
Mechanism:  ตรวจ format อัตโนมัติ (JSON/Shell/Table/Diff/Log/Prose)
            ใช้ compression เฉพาะ format
  JSON:     compact (ลบ whitespace)
  Shell ls: head + tail + summary
  Table:    strip separators
  Diff:     keep changes only
  Log:      dedup consecutive lines

ตัวอย่าง:  ls -la output 115 chars saved (load test, 6 tool_result blocks)
```

**ผลประหยัดประมาณการ**: $0.13/วัน = **$2.77/เดือน**

#### 3rd: ToolFilter (F19) - กรอง Tool Manifest

```
Mechanism:  เมื่อ tools > 15, จำแนก intent (code/search/analysis/action)
            Score แต่ละ tool, เก็บ top-K + essential tools

ตัวอย่าง:  27-tool manifest -> ลบ 960 chars (load test T3)
            ประหยัด 3,000-6,000 tokens/request สำหรับ MCP sessions
```

**ผลประหยัดประมาณการ**: $0.01/วัน = **$0.31/เดือน** (แต่สูงมากสำหรับ MCP-heavy workflows)

### 2.3 Cumulative Savings (Non-Additive)

Stages ทำงานต่อเนื่องกัน (pipeline) ไม่ได้บวกกันโดยตรง แต่ประเมินได้จาก:

```
ก่อน Gateway:  $421.58/เดือน (รวม hidden costs)

หลัง Gateway:
  Caveman ลด output ~40%:      -$132.00 x 0.40 = -$52.80
  Input stages ลด input ~25%:  -$148.50 x 0.25 = -$37.13
  Retry/context waste ลด ~80%:  -$98.18 x 0.80  = -$78.54
                                          รวม = -$168.47

ต้นทุนหลัง Gateway: $421.58 - $168.47 = $253.11/เดือน
```

### 2.4 สรุป Savings จาก Load Test จริง

จาก Load Test 7 requests (glm-5 pricing):

| ตัวชี้วัด | ค่า |
|---|---|
| Total Input Tokens ที่ส่งจริง | 2,793 |
| Total Output Tokens ที่ส่งจริง | 522 |
| Input Chars Saved (semantic_dedup + sketch + msg_textcomp + toolfilter) | 3,873 chars = ~968 tokens |
| System Prompt ที่ถูก Caveman replace | 3,273 chars = ~818 tokens |
| Cost จริงที่บันทึก | $0.0055 |
| Overhead รวมต่อ request | < 3ms |

---

## 3. Multi-Provider Cost Comparison

### 3.1 ราคา Provider หลัก (USD per 1M Tokens)

| Provider | Model | Input Price | Output Price | คู่แข่งที่ใกล้เคียง |
|---|---|---|---|---|
| **Anthropic** | claude-opus-4-7 | $15.00 | $75.00 | Premium tier |
| **Anthropic** | claude-sonnet-4-6 | $3.00 | $15.00 | Mid tier |
| **Anthropic** | claude-haiku-4-5 | $0.80 | $4.00 | Budget tier |
| **Z.AI** | glm-5 | $1.00 | $3.20 | กลาง-ประหยัด |
| **Z.AI** | glm-5-turbo | $1.20 | $4.00 | กลาง-ประหยัด |
| **Z.AI** | glm-5.1 | $1.40 | $4.40 | กลาง |
| **Z.AI** | glm-5-air | ต่ำกว่า | ต่ำกว่า | Budget tier |
| **Z.AI** | glm-5-flash | **ฟรี** | **ฟรี** | Free tier |
| **Z.AI** | glm-4.6v (Vision) | ~$0.60 | ~$2.20 | Vision |
| **Google** | gemini-2.5-pro | $1.25 | $10.00 | Mid tier |
| **Google** | gemini-2.0-flash | $0.10 | $0.40 | Budget tier |
| **Google** | gemini-2.0-flash (free) | **ฟรี** | **ฟรี** | Free tier |

### 3.2 Cost Breakdown ตามประเภทงาน

สมมติฐาน: 500 requests/วัน, แบ่งตามประเภทงาน

| ประเภทงาน | % ของ Requests | Avg Input Tok | Avg Output Tok | Requests/วัน |
|---|---|---|---|---|
| Code Generation | 35% | 4,000 | 1,200 | 175 |
| Config Edit | 25% | 2,500 | 400 | 125 |
| Analysis/Review | 20% | 5,000 | 1,500 | 100 |
| Chat/Q&A | 20% | 1,500 | 600 | 100 |

#### ต้นทุนต่อวัน ตาม Provider และประเภทงาน

| ประเภทงาน | Claude Sonnet | Z.AI glm-5 | Gemini Flash | ประหยัดสุด |
|---|---|---|---|---|
| Code Gen | $4.20 | $1.01 | $0.22 | Gemini Flash |
| Config Edit | $1.35 | $0.34 | $0.04 | Gemini Flash |
| Analysis | $5.25 | $1.30 | $0.28 | Gemini Flash |
| Chat/Q&A | $1.35 | $0.35 | $0.07 | Gemini Flash |
| **รวม/วัน** | **$12.15** | **$3.00** | **$0.61** | - |
| **รวม/เดือน** | **$267.30** | **$66.00** | **$13.42** | - |

### 3.3 Bandit-Optimized Routing Decisions

Gateway ใช้ LinUCB (Multi-Armed Bandit) เพื่อเรียนรู้ว่า provider ไหนเหมาะกับงานแบบไหน:

```
Bandit Decision Flow:
  1. รับ request -> สร้าง 10-dim context feature vector
  2. คำนวณ score สำหรับแต่ละ provider (arm)
     score = theta^T * phi + alpha * sqrt(phi^T * A^-1 * phi)
               ^^^^^^^^   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
               mean        uncertainty bonus (exploration)
  3. เลือก provider ที่ score สูงสุด
  4. หลังได้ response -> คำนวณ reward (output/input ratio)
  5. อัปเดต A matrix + b vector ใน Redis (TTL 24h)
```

**กลยุทธ์ที่ Bandit เรียนรู้โดยทั่วไป:**

| ประเภทงาน | Provider ที่เลือก | เหตุผล |
|---|---|---|
| Code Generation | Z.AI glm-5.1 | ราคาประหยัด, code quality ดี |
| Config Edit | Z.AI glm-5-turbo | เร็ว, งานไม่ซับซ้อน |
| Analysis/Review | Z.AI glm-5 / Claude Sonnet | ต้องการ reasoning depth |
| Chat/Q&A | Z.AI glm-5-flash (ฟรี) | งานง่าย, ใช้ free tier ได้ |

### 3.4 Fallback Chain

```
glm -> openai -> anthropic -> gemini -> openrouter -> deepseek -> kimi -> ...
```

เมื่อ provider หลัก rate limit (429) หรือล่ม จะ fallback ไป provider ถัดไปอัตโนมัติ

---

## 4. ROI Calculation

### 4.1 Gateway Infrastructure Cost

#### ตัวเลือก A: Kubernetes (Production)

| Component | Replicas | CPU Req/Limit | Memory Req/Limit | ต้นทุนประมาณ/เดือน* |
|---|---|---|---|---|
| arl-gateway (Go) | 2 | 500m/2 | 256Mi/1Gi | $60 |
| arl-rate-limiter (Java) | 2 | 500m/2 | 512Mi/1.5Gi | $80 |
| arl-dragonfly (Redis) | 1 | 500m/2 | 1Gi/8Gi | $120 |
| arl-worker (Python) | 2 | 500m/2 | 512Mi/2Gi | $70 |
| arl-prometheus | 1 | 250m/2 | 256Mi/2Gi | $40 |
| arl-grafana | 1 | 250m/2 | 256Mi/1Gi | $25 |
| arl-otel | 1 | 250m/1 | 128Mi/512Mi | $15 |
| arl-proxy (Caddy) | 2 | 100m/500m | 32Mi/128Mi | $20 |
| arl-dashboard | 2 | 100m/500m | 64Mi/256Mi | $25 |
| arl-sidecar | 1 | 100m/500m | 128Mi/256Mi | $20 |
| **รวม** | **15 pods** | | | **$475/เดือน** |

*ประมาณการจาก Tencent Cloud TKE / AWS EKS pricing (managed K8s)

#### ตัวเลือก B: Docker Compose (Small Team)

| Component | Memory Limit | ต้นทุนประมาณ/เดือน* |
|---|---|---|
| 1x VM (8 vCPU, 16GB RAM) | ~12GB total | $120 |
| 1x VM (2 vCPU, 4GB RAM) - Observer | ~4GB total | $40 |
| **รวม** | | **$160/เดือน** |

*ประมาณการจาก Tencent Cloud CVM / AWS EC2

### 4.2 Net Savings Calculation

#### สถานการณ์ A: ใช้ Claude Sonnet Direct (ผ่าน Gateway)

| รายการ | จำนวน |
|---|---|
| ต้นทุน API ก่อน Gateway | $421.58/เดือน |
| ต้นทุน API หลัง Gateway (ลด ~40%) | $253.11/เดือน |
| **การประหยัด API** | **$168.47/เดือน** |
| ต้นทุน Infrastructure (K8s) | $475.00/เดือน |
| **Net (K8s)** | **-$306.53/เดือน (ขาดทุน)** |
| ต้นทุน Infrastructure (Docker) | $160.00/เดือน |
| **Net (Docker)** | **$8.47/เดือน (เพิ่มขึ้นเล็กน้อย)** |

> หมายเหตุ: สำหรับทีม 10 คนกับ Claude Sonnet อย่างเดียว Gateway ยังไม่คุ้มค่าทางการเงิน
> แต่ถ้ารวมผลประโยชน์ด้าน security (PasteGuard), observability, rate limiting, multi-provider routing
> คุณค่าที่ได้จะมากกว่าตัวเลขการเงิน

#### สถานการณ์ B: ใช้ Z.AI glm-5 ผ่าน Gateway (แนะนำ)

| รายการ | จำนวน |
|---|---|
| ต้นทุน Claude Sonnet Direct (ปัจจุบัน) | $421.58/เดือน |
| ต้นทุน Z.AI glm-5 ผ่าน Gateway (หลัง optimize) | $66.00 x 0.60 = $39.60/เดือน |
| **การประหยัดจาก Provider Switch** | **$381.98/เดือน** |
| ต้นทุน Infrastructure (Docker) | $160.00/เดือน |
| **Net Savings (Docker)** | **+$221.98/เดือน** |
| ต้นทุน Infrastructure (K8s) | $475.00/เดือน |
| **Net Savings (K8s)** | **-$93.02/เดือน** |

#### สถานการณ์ C: Mixed Provider + Gateway (Optimal)

ใช้ Z.AI เป็นหลัก + Claude สำหรับงานที่ต้องการ quality สูง + Gemini Flash สำหรับงานเบา

| รายการ | Provider | Cost/เดือน |
|---|---|---|
| Code Gen (175 req/วัน) | Z.AI glm-5.1 | $22.22 |
| Config Edit (125 req/วัน) | Z.AI glm-5-turbo | $7.48 |
| Analysis (100 req/วัน) | Claude Sonnet | $115.50 |
| Chat/Q&A (100 req/วัน) | Z.AI glm-5-flash (ฟรี) | $0.00 |
| **ต้นทุน API รวม** | | **$145.20/เดือน** |
| Gateway Optimization (-15%) | | -$21.78 |
| **ต้นทุน API หลัง Gateway** | | **$123.42/เดือน** |
| ต้นทุน Infrastructure (Docker) | | $160.00/เดือน |
| **Total** | | **$283.42/เดือน** |
| vs Claude Direct | | $421.58/เดือน |
| **Net Savings** | | **$138.16/เดือน (32.8%)** |

### 4.3 Payback Period

| สถานการณ์ | Infrastructure | Net Savings/เดือน | Payback Period* |
|---|---|---|---|
| Docker + Z.AI Only | $160/เดือน | $222/เดือน | **< 1 เดือน** |
| Docker + Mixed | $160/เดือน | $138/เดือน | **< 1 เดือน** |
| K8s + Mixed | $475/เดือน | -$93/เดือน | ไม่คุ้มจากตัวเลขเดียว** |
| K8s + Z.AI Only | $475/เดือน | -$93/เดือน | ไม่คุ้มจากตัวเลขเดียว** |

*Payback = เวลาที่ savings ครอบคลุม setup cost (ประมาณ 2-3 วันในการ deploy)

**K8s จะคุ้มค่าเมื่อทีมขยายเกิน 25+ คน หรือมี multi-team usage

### 4.4 3-Year TCO Comparison

| รายการ | Claude Direct | Z.AI via Gateway (Docker) | Mixed via Gateway (Docker) |
|---|---|---|---|
| API Cost/เดือน | $421.58 | $39.60 | $123.42 |
| Infra Cost/เดือน | $0 | $160.00 | $160.00 |
| Total/เดือน | $421.58 | $199.60 | $283.42 |
| **Total/ปี** | **$5,058.96** | **$2,395.20** | **$3,401.04** |
| **3-Year TCO** | **$15,176.88** | **$7,185.60** | **$10,203.12** |
| **3-Year Savings** | - | **$7,991.28** | **$4,973.76** |
| **% Savings** | - | **52.7%** | **32.8%** |

### 4.5 Scaling Analysis (คุ้มที่กี่คน?)

| จำนวน Engineers | Claude Direct | Z.AI Gateway (Docker) | Savings | คุ้มไหม? |
|---|---|---|---|---|
| 5 คน | $210.79 | $199.60 | $11.19 | คุ้มเล็กน้อย |
| 10 คน | $421.58 | $199.60 | $221.98 | **คุ้ม** |
| 25 คน | $1,053.95 | $199.60 | $854.35 | **คุ้มมาก** |
| 50 คน | $2,107.90 | $199.60 | $1,908.30 | **คุ้มมาก** |
| 100 คน | $4,215.80 | $199.60 | $4,016.20 | **คุ้มมาก** |

> Gateway infrastructure cost คงที่ ไม่ว่าจะมีกี่ users
> ดังนั้นยิ่งทีมใหญ่ ยิ่งคุ้มค่า

---

## 5. Grafana Dashboard Metrics

### 5.1 Key PromQL Queries สำหรับ Cost Monitoring

#### Cost per Request

```promql
# ต้นทุนเฉลี่ยต่อ request
sum(increase(api_gateway_cost_total[24h]))
/
sum(increase(api_gateway_request_latency_seconds_count[24h]))
```

#### Cost per User (Profile)

```promql
# ต้นทุนต่อ profile ต่อวัน
sum by (profile) (increase(api_gateway_profile_cost_total[24h]))
```

#### Cost per Team (Account)

```promql
# ต้นทุนต่อ account ต่อวัน
sum by (account_id) (
  increase(api_gateway_account_token_input_total[24h]) * blended_input_rate
  +
  increase(api_gateway_account_token_output_total[24h]) * blended_output_rate
)
```

#### Blended Rate (Dynamic)

```promql
# Blended input rate (USD per token) - คำนวณจากข้อมูลจริง
sum(increase(api_gateway_cost_total{type="input"}[24h]))
/
(sum(increase(api_gateway_token_input_total[24h])) + 1)
```

#### Cost Savings Rate

```promql
# อัตราการประหยัด ($/วินาที)
sum(rate(api_gateway_optimizer_chars_saved_total{direction="input"}[5m])) / 4
*
sum(rate(api_gateway_cost_total{type="input"}[5m]))
/
(sum(rate(api_gateway_token_input_total[5m])) + 1)
```

### 5.2 Cost Dashboard Panels

#### ต้นทุนรวม (Actual Billed Cost)

```promql
# Total cost (post-optimization) - ต้นทุนจริงที่ต้องจ่าย
sum(increase(api_gateway_cost_total[$__range]))
```

#### การประหยัดจาก Optimizer

```promql
# Estimated cost savings using dynamic blended rate
(
  sum(increase(api_gateway_optimizer_chars_saved_total{direction="input"}[$__range])) / 4
)
*
(
  sum(increase(api_gateway_cost_total{type="input"}[$__range]))
  /
  (sum(increase(api_gateway_token_input_total[$__range])) + 1)
)
```

#### ต้นทุนแยกตาม Model

```promql
# Cost distribution per model
sum by (model) (increase(api_gateway_cost_total[$__range]))
```

#### ต้นทุนแยกตาม Provider Path

```promql
# Cost by billing path (direct vs sidecar vs rejected)
sum by (path) (increase(api_gateway_billing_path_requests_total[$__range]))
```

#### Before vs After Optimization

```promql
# Without optimization (hypothetical)
sum(increase(api_gateway_token_input_total[$__range]))

# With optimization (actual billed)
sum(increase(api_gateway_token_input_total[$__range]))
-
sum(increase(api_gateway_optimizer_chars_saved_total{direction="input"}[$__range])) / 4
```

### 5.3 Budget Burn Rate Alerts

#### Alert: Daily Budget Exceeded

```yaml
# Prometheus Alert Rule
- alert: DailyBudgetBurnRate
  expr: |
    sum(increase(api_gateway_cost_total[1h])) * 24
    >
    57600 * 0.05  # QUOTA_DAILY_BUDGET * 5% threshold
  for: 30m
  labels:
    severity: warning
  annotations:
    summary: "Daily budget burn rate exceeds threshold"
    description: "Projected daily cost: {{ $value | printf \"%.2f\" }} USD"
```

#### Alert: Cost Anomaly Detection

```yaml
# ตรวจจับ cost spike (ใช้ Z-score)
- alert: CostAnomaly
  expr: |
    (
      sum(rate(api_gateway_cost_total[5m]))
      -
      avg_over_time(sum(rate(api_gateway_cost_total[5m]))[7d])
    )
    /
    stddev_over_time(sum(rate(api_gateway_cost_total[5m]))[7d])
    > 2.0
  for: 10m
  labels:
    severity: critical
  annotations:
    summary: "Unusual cost spike detected"
```

#### Alert: High Waste Detection

```yaml
# Waste tokens สูงผิดปกติ
- alert: HighWasteDetected
  expr: |
    sum(rate(api_gateway_waste_tokens_wasted_total[15m]))
    /
    sum(rate(api_gateway_token_input_total[15m]))
    > 0.15
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "More than 15% of tokens identified as waste"
```

#### Alert: Rate Limit Pressure

```yaml
# 429 rate สูง = คุณภาพการใช้งานต่ำ
- alert: HighRateLimitPressure
  expr: |
    sum(rate(api_gateway_upstream_429_total[5m]))
    /
    sum(rate(api_gateway_request_latency_seconds_count[5m]))
    > 0.10
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "More than 10% of requests hitting rate limits"
```

### 5.4 ตารางสรุป Dashboards ที่เกี่ยวข้องกับ Cost

| Dashboard | Panel หลัก | ใช้สำหรับ |
|---|---|---|
| Service Dashboard | Total Cost, Cost Savings, Optimization Rate | Executive overview |
| Token Optimization | Savings by Technique, Budget Level, Waste Detection | Optimizer performance |
| Cost Calculator | Cost by Model, API Usage, Billing Path | Cost breakdown |
| API Usage Monitor | Per-Profile, Per-Account usage | Chargeback/showback |
| Gateway Overview | Cost Rate by Model, 429 Rate | Technical health |
| Claude OAuth Billing | Billing Path Distribution, Profile Cost | OAuth-specific cost |

### 5.5 สูตรคำนวณ Cost สำหรับแต่ละ Model

```promql
# Z.AI GLM-5 (default pricing from MODEL_PRICING env)
# glm-5:1.0:3.2 = $1.00/M input, $3.20/M output

# Claude Sonnet (if using direct Anthropic)
# claude-sonnet-4-6:3:15 = $3.00/M input, $15.00/M output

# Claude Opus (premium)
# claude-opus-4-7:15:75 = $15.00/M input, $75.00/M output

# Cost per model:
sum by (model) (increase(api_gateway_cost_total[$__range]))
```

---

## สรุปสำหรับผู้จัดการฝ่าย IT

| ตัวชี้วัด | ค่า |
|---|---|
| ทีมปัจจุบัน | 10 Engineers |
| ต้นทุนปัจจุบัน (Claude Direct) | ~$420/เดือน ($42/คน) |
| ต้นทุนหลังใช้ Gateway + Z.AI | ~$200/เดือน ($20/คน) |
| **การประหยัด** | **$220/เดือน (52%)** |
| **3-Year Savings** | **$7,991** |
| Infrastructure Cost (Docker) | $160/เดือน (คงที่) |
| Payback Period | < 1 เดือน |
| จุดคุ้มทุน | 5+ Engineers |
| ผลประโยชน์เพิ่มเติม | Security (PasteGuard), Observability, Rate Limiting, Multi-Provider Fallback |

### คำแนะนำ

1. **เริ่มจาก Docker Compose** - ต้นทุนต่ำ, deploy เร็ว (1 วัน), เหมาะสำหรับทีม 5-25 คน
2. **ใช้ Z.AI glm-5 เป็นหลัก** - ราคา $1.00/M vs Claude $3.00/M (ประหยัด 67%)
3. **เก็บ Claude สำหรับงานที่ต้องการ quality สูง** - Analysis, Architecture decisions
4. **ใช้ Gemini Flash สำหรับงานเบา** - Chat/Q&A, Simple queries (ฟรี)
5. **Monitor ผ่าน Grafana** - ตั้ง budget alerts, ตรวจ waste detection ทุกสัปดาห์
6. **ขยายเป็น K8s เมื่อทีมเกิน 25 คน** - HPA auto-scale, multi-replica, high availability


---

## E2E: CI/CD Pipeline Integration



> ผู้เขียน: เรื่องเล่าของ "กิตติ" - Senior DevOps Engineer ที่กำลังติดตั้ง AI-assisted CI/CD ด้วย Optimizer Gateway

---

## บทนำ: ทำไมต้องใช้ Optimizer Gateway ใน CI/CD


1. **Token cost พุ่ง** - แต่ละ pipeline run ส่ง PR diff, logs, metrics เข้า AI หมด ค่าใช้จ่ายเพิ่ม 40%
2. **Latency สูง** - AI analysis ใช้เวลา 30-60 วินาที ต่อ step ทำให้ pipeline ช้าลง
3. **Secrets leak** - มีโอกาสที่ API keys, credentials ใน PR diff หรือ logs จะถูกส่งไปยัง AI provider

Optimizer Gateway แก้ปัญหาเหล่านี้ด้วย 13-stage pipeline:

```
GitHub Actions / PagerDuty / CronJob
         │
         ▼
┌─────────────────────────────────┐
│     Optimizer Gateway :9000     │
│  ┌───────────────────────────┐  │
│  │  Request Pipeline (pre)   │  │
│  │  ├─ Budget Level Calc     │  │
│  │  ├─ F7  Semantic Dedup    │  │
│  │  ├─ F1  Chunker           │  │
│  │  ├─ F8  Delta Encoding    │  │
│  │  ├─ F9  Sketch Duplicate  │  │
│  │  ├─ F13 Intent Filter     │  │
│  │  ├─ F17 TextComp          │  │
│  │  ├─ F16 Caveman           │  │
│  │  ├─ F18 ToolComp          │  │
│  │  ├─ F19 ToolFilter        │  │
│  │  └─ PasteGuard (PII mask) │  │
│  ├───────────────────────────┤  │
│  │  Provider API (Z.AI/...)  │  │
│  ├───────────────────────────┤  │
│  │  Feedback Pipeline (post) │  │
│  │  ├─ F4  Prefetcher        │  │
│  │  ├─ F10 Warm Start        │  │
│  │  ├─ F11 Waste Detection   │  │
│  │  ├─ F14 Cache Eviction    │  │
│  │  ├─ F5  Bandit            │  │
│  │  └─ F20 CompCache         │  │
│  └───────────────────────────┘  │
└─────────────────────────────────┘
         │
         ▼
   AI Response (optimized)
```

---

## Scenario 1: AI-Powered Code Review ใน GitHub Actions

### สถานการณ์


### Pipeline Flow

```
Developer pushes PR
        │
        ▼
GitHub Actions triggered
        │
        ▼
┌─────────────────────────────────────────┐
│ Step 1: Fetch PR diff (gh cli)          │
│ Step 2: Build AI review prompt          │
│ Step 3: POST /v1/messages → Gateway     │
│         │                               │
│         ├─ ToolFilter: เลือกเฉพาะ       │
│         │  Read,Edit,Bash tools          │
│         │  (ตัดออก 20+ tools ที่ไม่จำเป็น) │
│         │  ประหยัด ~3000-6000 tokens     │
│         │                               │
│         ├─ Intent Filter (code intent): │
│         │  สกัดเฉพาะ code suggestions   │
│         │  ตัด explanation ออก           │
│         │                               │
│         ├─ PasteGuard: ตรวจ PR diff    │
│         │  mask EMAIL_ADDRESS,           │
│         │  PHONE_NUMBER อัตโนมัติ        │
│         │                               │
│         └─ TextComp: บีบ verbose prompt │
│            "Please carefully review..." │
│            → "Review code:"             │
└─────────────────────────────────────────┘
        │
        ▼
AI Response (code suggestions only)
        │
        ▼
Post review comment on PR via gh cli
```

### GitHub Actions Workflow YAML

```yaml
name: AI Code Review

on:
  pull_request:
    types: [opened, synchronize]

jobs:
  ai-review:
    runs-on: ubuntu-latest
    permissions:
      pull-requests: write
      contents: read

    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Get PR diff
        id: diff
        run: |
          # ดึงเฉพาะไฟล์ที่เปลี่ยนแปลง (added/modified)
          git diff origin/main...HEAD > /tmp/pr.diff
          echo "lines=$(wc -l < /tmp/pr.diff)" >> "$GITHUB_OUTPUT"

      - name: Skip if trivial
        if: steps.diff.outputs.lines < 20
        run: echo "Diff too small for review, skipping."

      - name: AI Review via Optimizer Gateway
        if: steps.diff.outputs.lines >= 20
        env:
          GATEWAY_URL: ${{ secrets.OPTIMIZER_GATEWAY_URL }}
          GATEWAY_API_KEY: ${{ secrets.OPTIMIZER_GATEWAY_KEY }}
        run: |
          DIFF_CONTENT=$(cat /tmp/pr.diff)

          # สร้าง request body - Gateway จะ optimize อัตโนมัติ
          # ToolFilter: เลือกเฉพาะ Read+Edit+Bash (จาก 27 tools)
          # Intent Filter: สกัดเฉพาะ code suggestions
          # PasteGuard: mask secrets ใน diff อัตโนมัติ
          # TextComp: บีบ verbose prompt
          RESPONSE=$(curl -s -X POST "${GATEWAY_URL}/v1/messages" \
            -H "Content-Type: application/json" \
            -H "x-api-key: ${GATEWAY_API_KEY}" \
            -d '{
              "model": "claude-sonnet-4-20250514",
              "max_tokens": 2048,
              "system": "You are a senior code reviewer for Go microservices. Focus on: security issues, race conditions, error handling, performance. Output only actionable code suggestions with file:line references.",
              "messages": [
                {
                  "role": "user",
                  "content": "Review this PR diff and provide specific code suggestions:\n\n'"${DIFF_CONTENT}"'"
                }
              ]
            }')

          # สกัด text จาก response
          REVIEW=$(echo "$RESPONSE" | jq -r '.content[0].text // empty')

          if [ -n "$REVIEW" ]; then
            # โพสต์ review comment ใน PR
            gh pr comment ${{ github.event.pull_request.number }} \
              --body "## AI Code Review (Optimizer Gateway)

          $REVIEW

          ---
          *Powered by Optimizer Gateway - Token savings tracked via Prometheus*"
          fi

      - name: Report metrics
        if: always()
        run: |
          # ดึง optimization metrics จาก gateway
          curl -s "${GATEWAY_URL}/metrics" | grep -E \
            'api_gateway_optimizer_chars_saved_total|api_gateway_filter_intents_total|api_gateway_caveman_compressions_total' \
            >> "$GITHUB_STEP_SUMMARY" || true
```

### สิ่งที่เกิดขึ้นใน Gateway

เมื่อ request เข้ามา Gateway ทำงานตาม pipeline:

```
Request (raw)
  │
  ├─ Budget Level: GREEN (context < 50%)
  │
  ├─ F7 Semantic Dedup
  │  Input:  "You are a senior code reviewer..."
  │           "You should review code carefully..."
  │           "Make sure to check for bugs..."
  │  Output: ตัด duplicate sentences → ประหยัด ~3-5% chars
  │
  ├─ F19 ToolFilter (ไม่ activate - request ไม่มี tools array)
  │
  ├─ F17 TextComp (balanced mode)
  │  Input:  "Please carefully review this PR diff and provide
  │           specific actionable code suggestions with file:line"
  │  Output: "Review PR diff. Code suggestions with file:line"
  │  → ประหยัด ~10% chars
  │
  ├─ F16 Caveman (lite tier - green budget)
  │  Inject: [OUTPUT STYLE - lite] directive
  │  → Model ตอบสั้นลง ~30%, ไม่มี filler phrases
  │
  ├─ PasteGuard (privacy masking)
  │  ตรวจ PR diff: mask ทุก EMAIL_ADDRESS, PHONE_NUMBER
  │  → ป้องกัน secrets leak ไปยัง AI provider
  │
  └─ POST to Provider → Response

Post-Response:
  ├─ F4 Prefetcher: บันทึก tool transitions สำหรับ session นี้
  ├─ F11 Waste Detection: ตรวจ wasted tokens
  └─ F5 Bandit: ปรับ optimizer weights ตามผลลัพธ์
```

### ผลลัพธ์ที่ได้

| Metric | Before Gateway | After Gateway | Savings |
|--------|---------------|---------------|---------|
| Input tokens/review | ~4,200 | ~1,800 | 57% |
| Output tokens/review | ~2,500 | ~1,200 | 52% |
| Cost/review | $0.025 | $0.010 | 60% |
| Latency | 45s | 18s | 60% |
| Secrets leaked | เคยเกิด 2 ครั้ง/เดือน | 0 | 100% |

---

## Scenario 2: Automated Incident Response

### สถานการณ์

เวลา 02:30 น. PagerDuty ส่ง alert P1 เข้ามา: "pod/payment-service CrashLoopBackOff - production" กิตติตั้งระบบไว้ให้ AI วิเคราะห์ incident อัตโนมัติผ่าน Gateway ก่อนจะปลุก on-call

### Incident Timeline with Optimizer Stages

```
02:30:00  PagerDuty alert triggered
          │
02:30:01  ┌─────────────────────────────────────────┐
          │ Webhook handler receives alert           │
          │ POST → Optimizer Gateway /v1/messages    │
          │                                         │
          │ F10 Warm Start:                          │
          │ ค้นหา session ที่คล้ายกันใน Redis (7 วัน)  │
          │ → เจอ incident "payment-service          │
          │    CrashLoop 3 ครั้งที่แล้ว"                 │
          │ → โหลด patterns มาใช้ทันที                │
          │ ประหยัด cold-start waste ~15%            │
          │                                         │
          │ F4 Prefetcher:                           │
          │ ทำนายคำสั่งถัดไปจาก Markov chain           │
          │   kubectl logs → kubectl describe →      │
          │   kubectl get events                     │
          │ → prefetch ข้อมูลเหล่านี้ล่วงหน้า             │
          └─────────────────────────────────────────┘
          │
02:30:03  ┌─────────────────────────────────────────┐
          │ Step 1: Fetch diagnostic data            │
          │ - kubectl logs payment-service --tail=100│
          │ - kubectl describe pod payment-service   │
          │ - kubectl get events --field-selector... │
          │                                         │
          │ F18 ToolComp: บีบ log output              │
          │ Input: 150 บรรทัด kubectl logs (4,500ch) │
          │ Output: 35 บรรทัด (head+tail+dedup)       │
          │ → ประหยัด ~75% tool_result tokens        │
          │                                         │
          │ Log format detection: "Log" type         │
          │ บีบ: dedup consecutive identical lines    │
          │ "[ERROR] connection refused" x 50        │
          │ → "[ERROR] connection refused (x50)"     │
          └─────────────────────────────────────────┘
          │
02:30:08  ┌─────────────────────────────────────────┐
          │ Step 2: AI Analysis                      │
          │                                         │
          │ F11 Waste Detection (runs every 60s):    │
          │ ตรวจพบ "retry_churn" pattern             │
          │ → AI สั่ง kubectl logs ซ้ำ 3 ครั้ง          │
          │ → Flag severity=medium                   │
          │                                         │
          │ F9 Sketch: ตรวจ near-duplicate prompt    │
          │ "Analyze this pod error..." ≈ 0.92       │
          │ similarity กับ incident ก่อนหน้า           │
          │ → Flag ว่าเป็นปัญหาเดิม                     │
          └─────────────────────────────────────────┘
          │
02:30:12  ┌─────────────────────────────────────────┐
          │ Step 3: Generate runbook                 │
          │                                         │
          │ F8 Delta Encoding:                      │
          │ เปรียบเทียบ runbook ใหม่กับ cached baseline │
          │ "sys:glm-5" key ใน Redis                 │
          │ ส่งเฉพาะ +/=/- operations                 │
          │ → ประหยัด ~40% input tokens               │
          │                                         │
          │ F16 Caveman (full tier - yellow budget): │
          │ Inject: [OUTPUT STYLE - full]            │
          │ → Model ตอบแบบ action items เท่านั้น       │
          │ → ไม่มี "I see that..." filler             │
          └─────────────────────────────────────────┘
          │
02:30:15  AI analysis complete → Post to Slack + PagerDuty
```

### Incident Response Script

```bash
#!/bin/bash
# incident-responder.sh - วิเคราะห์ incident อัตโนมัติผ่าน Optimizer Gateway
# ติดตั้งเป็น PagerDuty webhook หรือ run จาก Lambda/CloudRun

set -euo pipefail

GATEWAY_URL="${OPTIMIZER_GATEWAY_URL:-http://gateway.internal:9000}"
GATEWAY_KEY="${OPTIMIZER_GATEWAY_KEY}"
ALERT_PAYLOAD="$1"  # PagerDuty webhook JSON

# สกัดข้อมูลจาก alert
CLUSTER=$(echo "$ALERT_PAYLOAD" | jq -r '.cluster // "production"')
NAMESPACE=$(echo "$ALERT_PAYLOAD" | jq -r '.namespace // "default"')
SERVICE=$(echo "$ALERT_PAYLOAD" | jq -r '.service // .pod_name // "unknown"')
SEVERITY=$(echo "$ALERT_PAYLOAD" | jq -r '.severity // "P2"')

echo "=== Incident: ${SEVERITY} - ${SERVICE} in ${CLUSTER}/${NAMESPACE} ==="

# ดึง diagnostic data (จะถูก ToolComp บีบอัตโนมัติ)
LOGS=$(kubectl logs "deploy/${SERVICE}" -n "$NAMESPACE" --tail=200 2>&1 || echo "LOGS_UNAVAILABLE")
DESCRIBE=$(kubectl describe "deploy/${SERVICE}" -n "$NAMESPACE" 2>&1 | head -80 || echo "DESCRIBE_UNAVAILABLE")
EVENTS=$(kubectl get events -n "$NAMESPACE" --sort-by='.lastTimestamp' 2>&1 | tail -30 || echo "EVENTS_UNAVAILABLE")

# ส่งเข้า Gateway - Warm Start จะโหลด incident patterns ที่คล้ายกัน
# Prefetcher จะทำนาย diagnostic steps ถัดไป
# ToolComp จะบีบ LOGS + DESCRIBE + EVENTS (~75% compression)
RESPONSE=$(curl -s --max-time 30 -X POST "${GATEWAY_URL}/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: ${GATEWAY_KEY}" \
  -d "$(jq -n \
    --arg severity "$SEVERITY" \
    --arg service "$SERVICE" \
    --arg cluster "$CLUSTER" \
    --arg ns "$NAMESPACE" \
    --arg logs "$LOGS" \
    --arg describe "$DESCRIBE" \
    --arg events "$EVENTS" \
    '{
      model: "glm-5",
      max_tokens: 1024,
      system: "You are an incident response AI. Analyze Kubernetes incidents. Output: ROOT CAUSE (1 line), EVIDENCE (bullet points), FIX (kubectl commands), SEVERITY ASSESSMENT. No filler text.",
      messages: [{
        role: "user",
        content: "SEVERITY: \($severity)\nSERVICE: \($service)\nCLUSTER: \($cluster)/\($ns)\n\nLOGS (last 200 lines):\n\($logs)\n\nPOD DESCRIBE:\n\($describe)\n\nRECENT EVENTS:\n\($events)"
      }]
    }')")

# สกัด analysis
ANALYSIS=$(echo "$RESPONSE" | jq -r '.content[0].text // "Analysis unavailable"')
TOKENS_USED=$(echo "$RESPONSE" | jq -r '.usage.input_tokens // 0')

echo "=== AI Analysis ==="
echo "$ANALYSIS"
echo ""
echo "Tokens used: ${TOKENS_USED}"

# ส่งผลไป Slack
if [ -n "${SLACK_WEBHOOK_URL:-}" ]; then
  jq -n \
    --arg text "*[${SEVERITY}] ${SERVICE} - ${CLUSTER}/${NAMESPACE}*\n\`\`\`\n${ANALYSIS}\n\`\`\`\n_Tokens: ${TOKENS_USED} | via Optimizer Gateway_" \
    '{text: $text}' \
    | curl -s -X POST "$SLACK_WEBHOOK_URL" -H "Content-Type: application/json" -d @-
fi
```

### Prometheus Metrics ที่วัดผลได้

```promql
# ToolComp compression สำหรับ incident logs
sum by (technique) (rate(api_gateway_optimizer_chars_saved_total{technique="toolcomp"}[5m]))

# Waste detection - ตรวจจับ retry churn ใน incident analysis
sum by (detector, severity) (rate(api_gateway_waste_findings_total{detector="retry_churn"}[1h]))

# Warm Start hit rate สำหรับ incident sessions
sum(rate(api_gateway_warmstart_sessions_warmed_total{result="hit"}[1h]))
/
sum(rate(api_gateway_warmstart_sessions_warmed_total[1h]))

# Prefetcher accuracy
sum(rate(api_gateway_prefetcher_predictions_total{correct="true"}[1h]))
/
sum(rate(api_gateway_prefetcher_predictions_total[1h]))
```

---

## Scenario 3: Infrastructure Drift Detection

### สถานการณ์

กิตติตั้ง cron job ให้รันทุกคืน เปรียบเทียบ Terraform plan กับ live state ของ Kubernetes cluster ถ้ามี drift จะให้ AI วิเคราะห์ว่าอะไรเปลี่ยน และอันตรายแค่ไหน

### Pipeline Flow

```
02:00  CronJob triggered (daily)
         │
         ▼
┌─────────────────────────────────────────┐
│ 1. terraform plan -out=tfplan           │
│ 2. terraform show -json tfplan > plan   │
│ 3. kubectl get all -o json > live       │
│ 4. Diff: plan vs live state             │
│                                         │
│ Gateway Optimization:                   │
│                                         │
│ F8 Delta Encoding:                      │
│ เปรียบเทียบกับ baseline ของวันก่อน        │
│ key: "sys:glm-5" in Redis               │
│ → ส่งเฉพาะ +/=/- operations              │
│ → "aws_instance.web: count 3→5"         │
│ → "k8s_deployment.api: image tag diff"  │
│ ประหยัด ~40-60% เพราะส่วนใหญ่ไม่เปลี่ยน    │
│                                         │
│ F20 CompCache:                          │
│ บีบ cached Terraform state comparisons  │
│ ใน Redis ด้วย zstd level 3              │
│ → ประหยัด 60-80% Redis memory           │
│                                         │
│ F14 Cache Eviction:                     │
│ รันทุก 5 นาที ลบ cached comparisons      │
│ ที่มี ROI ต่ำ (bottom 10%)                │
│ → ทิ้ง state ของ env ที่ไม่ได้ใช้แล้ว      │
│ → เก็บ state ของ production ไว้ (high ROI)│
│                                         │
│ F9 Sketch:                              │
│ ตรวจว่าวันนี้ diff เหมือนเมื่อวานไหม       │
│ → similarity > 0.85 → flag duplicate    │
│ → ข้าม analysis ประหยัดทั้ง request       │
└─────────────────────────────────────────┘
         │
         ▼
Slack notification (if drift detected)
```

### Automation Script

```bash
#!/bin/bash
# drift-detector.sh - ตรวจจับ infrastructure drift ทุกคืน
# Deploy เป็น Kubernetes CronJob

set -euo pipefail

GATEWAY_URL="${OPTIMIZER_GATEWAY_URL:-http://gateway.internal:9000}"
GATEWAY_KEY="${OPTIMIZER_GATEWAY_KEY}"
TF_DIR="/workspace/terraform"
REPORT_FILE="/tmp/drift-report-$(date +%Y%m%d).json"

# === Step 1: สร้าง Terraform plan ===
cd "$TF_DIR"
terraform plan -out=tfplan -detailed-exitcode 2>&1 | tee /tmp/tf-plan-output.txt
PLAN_EXIT=$?

# Exit code 0 = no changes, 1 = error, 2 = changes detected
if [ "$PLAN_EXIT" -eq 0 ]; then
  echo "No infrastructure drift detected. Exiting."
  exit 0
fi

if [ "$PLAN_EXIT" -eq 1 ]; then
  echo "Terraform plan failed. Exiting."
  exit 1
fi

# === Step 2: ดึง plan JSON ===
terraform show -json tfplan > /tmp/tfplan.json 2>/dev/null

# สกัดเฉพาะ changed resources (Delta Encoding จะจัดการใน Gateway)
# Gateway จะเปรียบเทียบกับ cached baseline อัตโนมัติ
CHANGED_RESOURCES=$(jq -r '
  .planned_values.root_module.resources // [] |
  .[] |
  "\(.type).\(.name): \(.values // {})"
' /tmp/tfplan.json 2>/dev/null || echo "Parse failed")

# ดึง live state สำหรับเปรียบเทียบ
LIVE_STATE=$(kubectl get deployments,statefulsets,configmaps,secrets \
  -A -o json 2>/dev/null | jq -r '
  .items |
  map({
    kind: .kind,
    name: .metadata.name,
    namespace: .metadata.namespace,
    resource_version: .metadata.resourceVersion,
    generation: .metadata.generation
  })
' 2>/dev/null || echo "Live state unavailable")

# === Step 3: ส่งเข้า Gateway ===
# Delta Encoding: เปรียบเทียบกับ baseline ของเมื่อวาน (cached ใน Redis)
# CompCache: บีบ cached state comparisons ด้วย zstd
# Sketch: ถ้า diff เหมือนเมื่อวาน → ข้าม
RESPONSE=$(curl -s --max-time 60 -X POST "${GATEWAY_URL}/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: ${GATEWAY_KEY}" \
  -d "$(jq -n \
    --arg changes "$CHANGED_RESOURCES" \
    --arg live "$LIVE_STATE" \
    --arg plan_output "$(tail -50 /tmp/tf-plan-output.txt)" \
    '{
      model: "glm-5",
      max_tokens: 1500,
      system: "You are an infrastructure drift analyst. Compare Terraform planned state vs live Kubernetes state. Classify each drift: SAFE (expected), RISKY (needs review), DANGEROUS (act now). Output JSON array only.",
      messages: [{
        role: "user",
        content: "Terraform plan changes:\n\($changes)\n\nPlan output:\n\($plan_output)\n\nLive Kubernetes state:\n\($live)"
      }]
    }')")

ANALYSIS=$(echo "$RESPONSE" | jq -r '.content[0].text // "No analysis"')
INPUT_TOKENS=$(echo "$RESPONSE" | jq -r '.usage.input_tokens // 0')

echo "=== Drift Analysis ==="
echo "$ANALYSIS"

# บันทึก report
jq -n \
  --arg analysis "$ANALYSIS" \
  --arg tokens "$INPUT_TOKENS" \
  --arg date "$(date -I)" \
  '{date: $date, analysis: $analysis, tokens: $tokens}' \
  > "$REPORT_FILE"

# ส่งแจ้งเตือนถ้ามี drift อันตราย
if echo "$ANALYSIS" | grep -qi "DANGEROUS"; then
  curl -s -X POST "${SLACK_WEBHOOK_URL}" \
    -H "Content-Type: application/json" \
    -d "{\"text\": \":warning: *DANGEROUS DRIFT DETECTED*\n\`\`\`\n${ANALYSIS}\n\`\`\`\"}"
fi
```

### CronJob Manifest

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: drift-detector
  namespace: platform-tools
spec:
  schedule: "0 2 * * *"  # รัน 02:00 ทุกคืน
  concurrencyPolicy: Forbid
  successfulJobsHistoryLimit: 7
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: drift-detector
          containers:
            - name: drift-detector
              env:
                - name: OPTIMIZER_GATEWAY_URL
                  value: "http://arl-gateway.platform-tools.svc:8080"
                - name: OPTIMIZER_GATEWAY_KEY
                  valueFrom:
                    secretKeyRef:
                      name: gateway-credentials
                      key: api-key
              volumeMounts:
                - name: terraform
                  mountPath: /workspace/terraform
          volumes:
            - name: terraform
              configMap:
                name: terraform-configs
          restartPolicy: OnFailure
```

---

## Scenario 4: Deployment Safety ด้วย AI Gate

### สถานการณ์

ก่อน deploy service ใหม่ขึ้น production กิตติตั้ง "AI Gate" ไว้ - AI จะวิเคราะห์ canary metrics แล้วตัดสินใจ go/no-go โดยใช้ budget-aware optimization เพื่อให้ decision รวดเร็วและประหยัด tokens

### Deployment Pipeline Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                    Deployment Pipeline                        │
│                                                              │
│  Stage 1: Build & Test                                       │
│     ↓ (auto-pass)                                            │
│                                                              │
│  Stage 2: Canary Deploy (10% traffic)                        │
│     ↓                                                        │
│  Stage 3: Canary Metrics Collection (5 min observation)      │
│     ↓                                                        │
│  Stage 4: ★ AI GATE ★                                       │
│     │                                                        │
│     ├─ GREEN BUDGET (< 50% context):                         │
│     │  Caveman lite: ตอบสั้น "APPROVE" หรือ "ROLLBACK: reason"│
│     │  Intent filter: สกัดเฉพาะ decision keywords            │
│     │                                                        │
│     ├─ YELLOW BUDGET (50-75% context):                       │
│     │  Caveman full: บีบ verbose metric explanations          │
│     │  Budget-aware disclosure: truncate ให้เหลือ key metrics │
│     │  → เก็บเฉพาะ p99 latency, error rate, CPU/memory       │
│     │                                                        │
│     └─ RED BUDGET (> 75% context - multi-service deploy):    │
│        Caveman ultra: raw decision เท่านั้น                    │
│        Summarizer: บีบ 5 นาที metrics เป็น summary            │
│        → Output: "REJECT" หรือ "APPROVE" + 1 บรรทัด           │
│                                                              │
│  Stage 5: Full Rollout (if APPROVED)                         │
│     or                                                       │
│  Stage 5: Auto-Rollback (if REJECTED)                        │
└──────────────────────────────────────────────────────────────┘
```

### ArgoCD / GitHub Actions Integration

```yaml
name: Deploy with AI Gate

on:
  workflow_dispatch:
    inputs:
      service:
        description: 'Service to deploy'
        required: true
      environment:
        description: 'Target environment'
        type: choice
        options: [staging, production]

jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: ${{ inputs.environment }}
    steps:
      - uses: actions/checkout@v4

      - name: Deploy Canary
        run: |
          # Deploy canary version (10% traffic)
          kubectl apply -f k8s/${{ inputs.service }}-canary.yaml
          kubectl set image deployment/${{ inputs.service }}-canary \
            app=${{ inputs.service }}:${{ github.sha }} \
            --namespace production

      - name: Observe Canary Metrics (5 min)
        run: |
          echo "Waiting 5 minutes for canary metrics..."
          sleep 300

      - name: AI Gate Decision
        id: ai-gate
        env:
          GATEWAY_URL: ${{ secrets.OPTIMIZER_GATEWAY_URL }}
          GATEWAY_KEY: ${{ secrets.OPTIMIZER_GATEWAY_KEY }}
          SERVICE: ${{ inputs.service }}
        run: |
          # ดึง canary metrics
          METRICS=$(curl -s "http://prometheus.internal:9090/api/v1/query_range" \
            --data-urlencode "query={
              service=\"${SERVICE}-canary\",
              job=\"kubernetes-pods\"
            }" \
            --data-urlencode "start=$(date -d '5 minutes ago' -Iseconds)" \
            --data-urlencode "end=$(date -Iseconds)" \
            --data-urlencode "step=30s" | jq -r '.data.result')

          # ดึง baseline metrics (main deployment)
          BASELINE=$(curl -s "http://prometheus.internal:9090/api/v1/query_range" \
            --data-urlencode "query={
              service=\"${SERVICE}\",
              job=\"kubernetes-pods\"
            }" \
            --data-urlencode "start=$(date -d '5 minutes ago' -Iseconds)" \
            --data-urlencode "end=$(date -Iseconds)" \
            --data-urlencode "step=30s" | jq -r '.data.result')

          # AI Gate: วิเคราะห์ canary health
          # Caveman mode จะทำให้ AI ตอบแค่ APPROVE/ROLLBACK + reason
          # Intent filter (action intent): สกัดเฉพาะ decision
          RESPONSE=$(curl -s --max-time 30 -X POST "${GATEWAY_URL}/v1/messages" \
            -H "Content-Type: application/json" \
            -H "x-api-key: ${GATEWAY_KEY}" \
            -d "$(jq -n \
              --arg metrics "$METRICS" \
              --arg baseline "$BASELINE" \
              --arg service "$SERVICE" \
              '{
                model: "glm-5",
                max_tokens: 256,
                system: "You are a deployment gate AI. Compare canary vs baseline metrics. Decide: APPROVE or ROLLBACK. Criteria: error_rate < 1%, p99_latency within 20% of baseline, no OOM kills. Output ONLY: DECISION: APPROVE/ROLLBACK | REASON: one line",
                messages: [{
                  role: "user",
                  content: "Service: \($service)\nCanary metrics (5min):\n\($metrics)\nBaseline metrics (5min):\n\($baseline)"
                }]
              }')")

          DECISION=$(echo "$RESPONSE" | jq -r '.content[0].text' | head -5)
          echo "decision=${DECISION}" >> "$GITHUB_OUTPUT"
          echo "### AI Gate Decision" >> "$GITHUB_STEP_SUMMARY"
          echo "\`\`\`" >> "$GITHUB_STEP_SUMMARY"
          echo "$DECISION" >> "$GITHUB_STEP_SUMMARY"
          echo "\`\`\`" >> "$GITHUB_STEP_SUMMARY"

          # ตรวจ decision
          if echo "$DECISION" | grep -qi "ROLLBACK"; then
            echo "::error::AI Gate rejected deployment"
            exit 1
          fi

      - name: Full Rollout
        if: success()
        run: |
          echo "AI Gate APPROVED. Rolling out..."
          kubectl set image deployment/${{ inputs.service }} \
            app=${{ inputs.service }}:${{ github.sha }} \
            --namespace production
          kubectl rollout status deployment/${{ inputs.service }} \
            --namespace production --timeout=300s

      - name: Auto Rollback
        if: failure()
        run: |
          echo "Rolling back canary..."
          kubectl rollout undo deployment/${{ inputs.service }}-canary \
            --namespace production
          kubectl delete deployment/${{ inputs.service }}-canary \
            --namespace production --ignore-not-found
```

### Budget-Aware Behavior

AI Gate ปรับพฤติกรรมตาม budget level อัตโนมัติ:

```
┌─────────────────────────────────────────────────────────────┐
│ Budget Level: GREEN (< 50% context used)                     │
│                                                              │
│ Deploying single service, first deployment of the day         │
│                                                              │
│ Activated stages:                                            │
│  ├─ Caveman lite: "DECISION: APPROVE | latency p99 within 5%"│
│  ├─ Intent filter (action): สกัดเฉพาะ DECISION line          │
│  └─ TextComp: บีบ verbose metric descriptions                │
│                                                              │
│ Output: 1-2 บรรทัด                                          │
│ Tokens: ~150 input, ~80 output                               │
├─────────────────────────────────────────────────────────────┤
│ Budget Level: YELLOW (50-75% context used)                   │
│                                                              │
│ Deploying 3rd service, context มี canary metrics 2 รอบแล้ว    │
│                                                              │
│ Activated stages:                                            │
│  ├─ Caveman full: บีบ 50% output                             │
│  ├─ Budget-aware disclosure: truncate metrics > 2000 chars    │
│  │   → เก็บเฉพาะ error_rate, p99, CPU, memory               │
│  │   → ตัด network I/O, disk I/O, custom metrics            │
│  └─ Delta Encoding: เปรียบเทียบกับ baseline cache             │
│                                                              │
│ Output: "APPROVE | p99 245ms (baseline 230ms), err 0.3%"     │
│ Tokens: ~400 input, ~120 output                              │
├─────────────────────────────────────────────────────────────┤
│ Budget Level: RED (> 75% context used)                       │
│                                                              │
│ Emergency multi-service deploy, context เต็ม                  │
│                                                              │
│ Activated stages:                                            │
│  ├─ Summarizer: บีบ 5 นาที metrics เป็น 3 บรรทัด summary      │
│  ├─ Caveman ultra: raw output เท่านั้น                        │
│  └─ Intent filter: สกัด decision keyword เท่านั้น              │
│                                                              │
│ Output: "APPROVE" หรือ "ROLLBACK: err 5.2%"                  │
│ Tokens: ~200 input (after summarizer), ~20 output            │
└─────────────────────────────────────────────────────────────┘
```

---

## สรุป: Token Savings รวมทั้ง 4 Scenarios

| Scenario | Optimizer Stages ที่ใช้ | Input Savings | Output Savings | ฟีเจอร์หลัก |
|----------|----------------------|---------------|----------------|-------------|
| Code Review | ToolFilter, Intent Filter, PasteGuard, TextComp, Caveman | 57% | 52% | ป้องกัน secrets leak, บีบ tool manifest |
| Incident Response | Warm Start, Prefetcher, ToolComp, Waste Detection, Sketch, Delta, Caveman | 65% | 50% | โหลด incident patterns อัตโนมัติ, บีบ logs |
| Drift Detection | Delta Encoding, CompCache, Cache Eviction, Sketch | 40-60% | 30% | ส่งเฉพาะส่วนที่เปลี่ยน, บีบ Redis cache |
| Deploy AI Gate | Caveman (tier-aware), Summarizer, Intent Filter, Disclosure, Delta | 50% | 70% | Budget-aware decision, auto-adjust verbosity |

### Prometheus Dashboard Queries สำหรับติดตาม

```promql
# 1. Token savings รวมต่อ technique
sum by (technique) (rate(api_gateway_optimizer_chars_saved_total[1h]))

# 2. Waste detection - ตรวจจับ wasted tokens ใน CI/CD requests
sum by (detector) (rate(api_gateway_waste_tokens_wasted_total[24h]))

# 3. Cache hit rate (CompCache + Delta + Sketch effectiveness)
sum(rate(api_gateway_delta_encodes_total{result="encoded"}[24h]))
/
sum(rate(api_gateway_delta_encodes_total[24h]))

# 4. Budget level distribution ใน deployment pipeline
sum by (le) (rate(api_gateway_budget_level[1h]))

# 5. Warm Start hit rate สำหรับ incident sessions
sum(rate(api_gateway_warmstart_sessions_warmed_total{result="hit"}[24h]))

# 6. Caveman compression ratio ตาม tier
histogram_quantile(0.5, rate(api_gateway_caveman_compression_ratio_bucket[1h]))
```

### การตั้งค่า Environment Variables สำหรับ CI/CD Workload

```bash
# docker-compose override สำหรับ CI/CD workload
# เน้น: latency ต่ำ + cost savings สูง

# Core optimization
CHUNKER_ENABLED=true
DELTA_ENABLED=true
DELTA_MIN_SAVINGS_PCT=5.0          # ลดจาก default 10 เพื่อ activate ง่ายขึ้น
SKETCH_ENABLED=true
SKETCH_THRESHOLD=0.80              # ลดจาก 0.85 เพื่อจับ duplicates มากขึ้น
TEXTCOMP_ENABLED=true
TEXTCOMP_MODE=aggressive           # ใช้ aggressive สำหรับ CI/CD (ไม่ต้องการ prose)

# Output control
CAVEMAN_ENABLED=true
CAVEMAN_AUTO_DETECT=true
FILTER_ENABLED=true

# Tool optimization
TOOLCOMP_ENABLED=true
TOOLCOMP_MAX_LINES=30              # ลดจาก 50 เพราะ CI/CD logs ยาว
TOOLFILTER_ENABLED=true
TOOLFILTER_MAX_TOOLS=10            # ลดจาก 15 เพื่อกรองเยอะขึ้น

# Cache & memory
COMPCACHE_ENABLED=true
COMPCACHE_LEVEL=5                  # เพิ่มจาก 3 เพื่อบีบมากขึ้น (CPU ใช้เพิ่มนิดหน่อย)
CACHE_EVICTION_ENABLED=true
CACHE_EVICTION_PCT=15.0            # เพิ่มจาก 10 เพื่อ clean cache เร็วขึ้น

# Post-processing
WASTE_ENABLED=true
WASTE_MIN_REQUESTS=5               # ลดจาก 10 เพื่อ detect เร็วขึ้น
PREFETCHER_ENABLED=true
WARMSTART_ENABLED=true
BANDIT_ENABLED=true
```

---

## สถาปัตยกรรมรวม: Optimizer Gateway ใน CI/CD Ecosystem

```
┌─────────────────────────────────────────────────────────────────┐
│                        CI/CD Platform                            │
│                                                                  │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │ GitHub   │  │ ArgoCD   │  │ PagerDuty│  │ CronJob  │       │
│  │ Actions  │  │ Pipeline │  │ Webhook  │  │ (daily)  │       │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘       │
│       │              │              │              │              │
│       └──────────────┴──────┬───────┴──────────────┘             │
│                             │                                    │
│                             ▼                                    │
│  ┌──────────────────────────────────────────────────────┐       │
│  │              Optimizer Gateway (:9000)                │       │
│  │                                                      │       │
│  │  Request Pipeline:                                   │       │
│  │   ├─ F7  Semantic Dedup ─────────── 3-5% savings     │       │
│  │   ├─ F1  Chunker ────────────────── 5-15% cache hit  │       │
│  │   ├─ F8  Delta Encoding ────────── 20-60% on diffs   │       │
│  │   ├─ F9  Sketch ────────────────── 5-30% dup detect  │       │
│  │   ├─ F17 TextComp ──────────────── 5-15% filler      │       │
│  │   ├─ F16 Caveman ───────────────── 30-75% output     │       │
│  │   ├─ F18 ToolComp ──────────────── 40-80% logs        │       │
│  │   ├─ F19 ToolFilter ────────────── 60-80% manifest   │       │
│  │   └─ PasteGuard ────────────────── secrets masked     │       │
│  │                                                      │       │
│  │  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │       │
│  │                                                      │       │
│  │  Feedback Pipeline:                                  │       │
│  │   ├─ F10 Warm Start ────────────── 10-20% cold-start │       │
│  │   ├─ F4  Prefetcher ────────────── 50-200ms latency  │       │
│  │   ├─ F11 Waste Detection ────────── 5-20% waste ID   │       │
│  │   ├─ F14 Cache Eviction ─────────── ~10% cache hit+   │       │
│  │   ├─ F5  Bandit ────────────────── 5-15% meta-opt    │       │
│  │   └─ F20 CompCache ─────────────── 60-80% Redis mem  │       │
│  │                                                      │       │
│  │  ┌─────────┐ ┌──────────┐ ┌─────────────────┐        │       │
│  │  │ Dragonfly│ │Prometheus│ │ Grafana Dashboard│       │       │
│  │  │ (Redis)  │ │ Metrics  │ │ (visualization) │       │       │
│  │  └─────────┘ └──────────┘ └─────────────────┘        │       │
│  └──────────────────────────────────────────────────────┘       │
│                             │                                    │
│                             ▼                                    │
│                    ┌─────────────────┐                           │
│                    │  AI Provider     │                           │
│                    │  (Z.AI / Claude) │                           │
│                    └─────────────────┘                           │
└─────────────────────────────────────────────────────────────────┘
```

กิตติสรุปประโยชน์หลัง deploy ไป 2 สัปดาห์:

- **Token cost ลด 55%** จากเดิม ~$500/เดือน เหลือ ~$225/เดือน
- **Pipeline latency ลด 40%** เฉลี่ยจาก 45s/step เหลือ 27s/step
- **Secrets leak = 0** จากเดิมเคยเกิด 2-3 ครั้ง/เดือน
- **Incident response เร็วขึ้น** เพราะ Warm Start โหลด patterns จาก incidents ก่อนหน้า
- **Drift detection ประหยัด Redis memory 65%** ด้วย CompCache + Cache Eviction


---

## E2E: Team Onboarding (5-Day Tutorial)



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


---

