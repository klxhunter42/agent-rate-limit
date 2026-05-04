# Token Optimization - สรุปภาษาไทย

วันที่: 2026-05-04 (อัปเดต)

---

## ภาพรวม

Gateway มี token optimization pipeline ขนาดใหญ่ 13+ ขั้นตอน ประกอบด้วย module หลัก 8 ตัว + ระบบ rate limiting 3 ชั้น ทั้งหมดมีเป้าหมายลด token usage, ลดค่าใช้จ่าย API, และเพิ่มประสิทธิภาพ upstream request

### Architecture Diagram

```
Client Request
     |
     v
+=========================================+
|        13-Stage Optimizer Pipeline      |
|                                         |
|  F7  Semantic Dedup (Jaccard > 0.7)     |
|  F1  Chunker (Rabin-Karp CDC)           |
|  F8  Delta Encode (LCS diff)            |
|  F9  Sketch Dedup (SimHash >= 0.85)     |
|  F6  Summarizer (budget red only)       |
|  F13 Intent Filter                      |
|  F16 Caveman (prompt injection)         |
|                                         |
|  Pre-proxy: 7 stages                    |
|  Post-proxy feedback: 4 stages          |
+==================+======================+
                   |
                   v
+=========================================+
|        Upstream Provider Proxy           |
|  (Anthropic / OpenAI / Gemini / Z.AI)   |
+==================+======================+
                   |
                   v
+=========================================+
|        Post-Proxy Feedback Loop          |
|  F4  Prefetcher (Markov chain)          |
|  F11 Waste Detection (7 heuristics)     |
|  F14 Cache ROI tracking                 |
|  F5  Bandit reward update               |
+=========================================+
```

---

## Module ทั้งหมด

### 1. Whitespace Optimization

**Source:** `api-gateway/tokenizer/`
**แนวคิด:** ลบ whitespace ซ้ำซ้อนใน prose text แต่ preserve code block เดิม
- ลบ double spaces -> single space
- ลบ trailing whitespace ต่อบรรทัด
- ลบ blank lines ที่เกิน 2 บรรทัดติดกัน
- ไม่ยุ่งกับ code block ระหว่าง triple backticks
**ผล:** ประหยัด ~3-5% tokens ต่อ request

### 2. Content-Aware Token Estimation

**Source:** `api-gateway/tokenizer/`
**แนวคิด:** ใช้ chars-per-token ratio ต่างกันตาม content type

| Content Type | chars/token | Detection |
|---|---|---|
| Code | 2.5 | >30% lines match code indicators |
| JSON | 2.8 | Starts with `{` or `[` |
| Markdown | 3.5 | >20% lines match markdown patterns |
| Text | 4.0 | Default |

**ผล:** ประเมิน cost ต่อ request ได้ก่อนส่ง upstream

### 3. Head/Tail Truncation

**Source:** `api-gateway/tokenizer/`
**แนวคิด:** เมื่อ response เกิน limit จะ preserve 40% head + 60% tail
- แทรก marker: `[N lines truncated - showing first X + last Y lines]`
- **Privacy guard:** ข้ามถ้ามี `__SECRET_` หรือ `__PII_` placeholder
**ผล:** ไม่เสีย context สำคัญเวลา truncate

### 4. Static Model Capability Map

**Source:** `api-gateway/tokenizer/`

| Model | Context Window | Max Output | Provider |
|---|---|---|---|
| claude-opus-4-7 | 200,000 | 163,840 | anthropic |
| claude-sonnet-4-6 | 200,000 | 163,840 | anthropic |
| gpt-4o | 128,000 | 16,384 | openai |
| gemini-2.5-pro | 1,048,576 | 65,536 | google |
| glm-5.1 | 128,000 | 4,096 | zai |
| + อีก 15 models | | | |

Unknown models fallback: `{128000, 4096}` + prefix matching

### 5. Duplicate Content Deduplication

**Source:** `api-gateway/tokenizer/`
- **Exact dedup:** Hash-based sentence matching (O(1))
- **Semantic dedup:** Jaccard word-set similarity, threshold 0.7
- **Privacy guard:** ข้ามทั้งหมดถ้ามี privacy placeholder

### 6. Token Budget Tracking

**Source:** `api-gateway/tokenizer/`

| Level | Utilization | Action |
|---|---|---|
| Green | < 50% | ทำงานปกติ |
| Yellow | 50-75% | เริ่ม optimize whitespace + dedup |
| Red | > 75% | force truncation + summarizer |

---

### 7. Chunker (Rabin-Karp CDC)

**Source:** `api-gateway/chunker/chunker.go`

**แนวคิด:** แบ่งเนื้อหาเป็น variable-size chunks ด้วย rolling hash แล้วตรวจ "stable" chunks (เคยเห็นแล้ว) ผ่าน Redis, reorder ให้ stable chunks อยู่ก่อน

**Algorithm:** Rabin-Karp rolling hash
```
for each position i:
    hash = sum(content[j] * 31) over window [i-windowSize, i]
    if hash % 256 == 0 AND chunk_size >= MinChunk:
        emit boundary
```

**Chunk hashing:** SHA-256 (first 12 bytes hex) เป็น chunk ID

**Stability detection:** Redis key `chunker:stable:<hash>` - นับจำนวนครั้ง, threshold=2, TTL 24h

**Reorder:** `[stable chunks] + [novel chunks]` - stable ก่อนเพื่อให้ delta/cache hit ไวขึ้น

**Config:**

| Env Var | Default | Description |
|---|---|---|
| `CHUNKER_ENABLED` | true | เปิด/ปิด |
| `CHUNKER_MIN_CHUNK` | 128 | ขนาด chunk ต่ำสุด (bytes) |
| `CHUNKER_MAX_CHUNK` | 4096 | ขนาด chunk สูงสุด |
| `CHUNKER_WINDOW_SIZE` | 48 | Sliding window size |
| `CHUNKER_STABLE_THRESHOLD` | 2 | จำนวนครั้งที่ต้องเห็นจึงจะ "stable" |

---

### 8. Packer (Greedy Knapsack)

**Source:** `api-gateway/packer/packer.go`

**แนวคิด:** เลือก context items ที่จะใส่ใน request ด้วย greedy 0/1 knapsack - เลือก items ที่มี utility/token ratio สูงสุดก่อน

**Algorithm:**
```
1. Filter items by MinUtility (default 0.1)
2. Sort by utility/token ratio (descending)
3. Greedily pack until budget exhausted
4. Excluded items' chars = "saved"
```

**Data structure:**
```go
type Item struct {
    ID       string
    Content  string
    Tokens   int
    Utility  float64  // 0.0 - 1.0+
}
```

**Config:**

| Env Var | Default | Description |
|---|---|---|
| `PACKER_ENABLED` | true | เปิด/ปิด |
| `PACKER_MIN_UTILITY` | 0.1 | Utility score ต่ำสุดที่จะพิจารณา |

---

### 9. Delta Encoding (LCS Diff)

**Source:** `api-gateway/delta/delta.go`

**แนวคิด:** คำนวณ diff ระหว่าง content ปัจจุบันกับ baseline ที่ cache ไว้ ถ้า delta เล็กกว่า full content >= 10% จะส่ง delta แทน

**Algorithm:** Longest Common Subsequence (LCS)
1. Split เป็น lines
2. Build LCS DP table O(m*n)
3. Backtrack: `=` (keep), `+` (insert), `-` (delete)
4. Compact consecutive same-type ops
5. Calculate savings

**Serialization:** `=14:Hello world!\n+6:Added\n-8:Removed!\n`

**Guards:**
- ข้ามถ้า content > 50KB หรือ > 200 lines
- ข้ามถ้า savings < 10%
- Baseline เก็บใน Redis `delta:baseline:<cacheKey>` TTL 24h

**Config:**

| Env Var | Default | Description |
|---|---|---|
| `DELTA_ENABLED` | true | เปิด/ปิด |
| `DELTA_MIN_SAVINGS_PCT` | 10.0 | ขั้นต่ำ % ที่ต้องประหยัด |

---

### 10. Summarizer (Extractive)

**Source:** `api-gateway/summarizer/summarizer.go`

**แนวคิด:** สรุป context ด้วย extractive summarization - ดึง first sentence ของแต่ละ paragraph, cap ที่ 30% ของต้นฉบับ

**Algorithm:**
1. Split content เป็น paragraphs (`\n\n`)
2. ดึง first sentence ของแต่ละ paragraph
3. ถ้าไม่เจอ sentence boundary เอา 200 chars + `...`
4. สะสมจนถึง MaxRatio (30%) ของ original length

**Caching:** Redis `summarizer:cache:<hash>` TTL 1hr

**Trigger:** ทำงานเฉพาะเมื่อ budget level = Red (>75%)

**Config:**

| Env Var | Default | Description |
|---|---|---|
| `SUMMARIZER_ENABLED` | true | เปิด/ปิด |
| `SUMMARIZER_MODEL` | glm-4.7-flashx | Model สำหรับ LLM summarization (อนาคต) |
| `SUMMARIZER_MAX_RATIO` | 0.3 | อัตราส่วน summary/original สูงสุด |

---

### 11. Caveman (Prompt Compression via Injection)

**Source:** `api-gateway/caveman/caveman.go`

**แนวคิด:** ไม่ใช่ compression แบบ gzip แต่เป็น **prompt engineering injection** - แทรก `[OUTPUT STYLE]` directive ลงไปใน system prompt เพื่อสั่งให้ LLM ตอบสั้นลง

**Compression Tiers:**

| Tier | Trigger | Estimated Ratio | Style |
|---|---|---|---|
| Lite | Budget green / content < 500 chars | 0.7 | Bullet points, skip pleasantries |
| Full | Budget yellow | 0.5 | Code only, terse, one-line answers |
| Ultra | Budget red | 0.25 | Raw output, no markdown |
| Wenyan | (Reserved) | 0.3 | Classical notation |

**ตัวอย่าง Lite injection:**
```
[OUTPUT STYLE -- lite]
Be concise. Use bullet points. Skip pleasantries and filler phrases.
One sentence answers when possible.
```

**Validation:**
- Code block preservation: นับ ``` blocks - ถ้าหาย validation fail
- Identifier preservation: ดึง identifiers สูงสุด 20 ตัว - ต้องเหลือ >= 80%

**Config:**

| Env Var | Default | Description |
|---|---|---|
| `CAVEMAN_ENABLED` | true | เปิด/ปิด |
| `CAVEMAN_AUTO_DETECT` | true | เลือก tier อัตโนมัติตาม budget |
| `CAVEMAN_MIN_SIZE` | 500 | ขนาด content ต่ำสุดที่จะ compress |

---

### 12. Waste Detector (7 Heuristics)

**Source:** `api-gateway/waste/waste.go`

**แนวคิด:** ตรวจจับ token waste ข้าม sessions ด้วย background scanner ทุก 60s

**7 Heuristics:**

| # | Detector | Severity | Trigger |
|---|---|---|---|
| 1 | Empty Response | High | >10% requests return 0 output |
| 2 | Retry Churn | Medium | Consecutive identical input + zero output, wasted > 5000 |
| 3 | Loop Detection | High | Cycle 2+ requests ซ้ำกัน |
| 4 | Oversized Context | Medium | Multiple requests >100K tokens, excess >100K |
| 5 | Budget Exceeded | Medium | ใช้ >3 models ใน session เดียว |
| 6 | Redundant Tool Call | Low | Consecutive identical request-response pairs |
| 7 | Low Value Response | Low | >=3 requests ที่ input >5000 แต่ output <50 |

**Session management:** In-memory, evict หลัง 30 min idle, ต้องมี >=10 requests ก่อน scan

---

### 13. Prefetcher (Markov Chain)

**Source:** `api-gateway/prefetcher/prefetcher.go`

**แนวคิด:** ทำนาย tool call ถัดไปด้วย first-order Markov chain - เรียนรู้จาก tool call sequences แล้ว pre-warm connections

**Algorithm:**
1. **Learning:** บันทึก tool call sequence per session + transition counts `P(tool_B | tool_A)`
2. **Prediction:** ดู transition table จาก last tool, return top-K predictions + confidence
3. **Pre-warming:** Store predictions ใน Redis ให้ downstream ใช้

**Confidence:** `count(transition to tool) / sum(all transition counts from last tool)`

**Redis keys:**

| Key | Purpose | TTL |
|---|---|---|
| `prefetcher:chain:<sessionID>` | Tool call history | 4hr |
| `prefetcher:trans:<toolName>` | Transition counts | 4hr |
| `prefetcher:last_pred:<tool>` | Last prediction | 1min |

**Config:**

| Env Var | Default | Description |
|---|---|---|
| `PREFETCHER_ENABLED` | true | เปิด/ปิด |
| `PREFETCHER_MAX_ORDER` | 5 | History length สูงสุด |
| `PREFETCHER_TOP_K` | 3 | จำนวน predictions |

---

### 14. Warm Start (Cosine Similarity)

**Source:** `api-gateway/warmstart/warmstart.go`

**แนวคิด:** หา sessions ที่คล้ายกันในอดีตด้วย cosine similarity บน 32-dim feature vectors เพื่อ preload optimizer state

**32-Dimension Feature Vector:**

| Dims | Feature | Encoding |
|---|---|---|
| 0-3 | Model type | One-hot (claude, gpt, gemini, glm) |
| 4-7 | Content type distribution | Ratio (0.0-1.0) |
| 8-15 | Tool call frequency | Normalized counts (top 8 tools) |
| 16-18 | Budget level distribution | Percentage |
| 19-22 | Request size buckets | Normalized tokens/requests |
| 23-27 | Intent distribution | Percentage |
| 28-31 | Project fingerprint | Hash, density, stream%, error% |

**Algorithm:**
1. Compute 32-dim signature จาก session metadata
2. Scan Redis หา signatures ที่ match project root
3. Cosine similarity: `sim(A,B) = dot(A,B) / (||A|| * ||B||)`
4. ถ้า best match >= 0.5 -> ใช้ warm start
5. Store signature ปัจจุบัน Redis `warmstart:sig:<project>:<session>` TTL 7d

---

### 15. Bandit (LinUCB)

**Source:** `api-gateway/bandit/bandit.go`

**แนวคิด:** Multi-Armed Bandit แบบ contextual เลือก optimization strategy ที่ให้ reward (output/input ratio) ดีที่สุด

**Algorithm:** LinUCB (Linear Upper Confidence Bound)
```
score = theta^T * phi + alpha * sqrt(|phi^T * A^-1 * phi|)
        ^^^^^^^^   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
        mean       uncertainty bonus (exploration)
```

- `theta = A^-1 * b` - learned weights (10 dims)
- `A` = 10x10 Gram matrix per arm (start as identity)
- `alpha` = 1.0 (exploration coefficient)
- Matrix inversion: Gauss-Jordan elimination

**Reward:** `output / input` (capped at 1.0)

**Redis:** `bandit:state:<armID>` TTL 24h

---

### 16. Sketch (SimHash Near-Duplicate)

**Source:** `api-gateway/sketch/sketch.go`

**แนวคิด:** ตรวจจับ near-duplicate prompts ด้วย SimHash bit vector - ถ้า similarity >= 0.85 ถือว่าซ้ำ ข้ามการส่ง upstream

**Algorithm:**
1. Tokenize content เป็น words
2. Hash แต่ละ word ด้วย FNV-1a (32-bit)
3. Set 3 bits per word ใน 128-bit vector
4. Compare ด้วย normalized Hamming similarity

**Config:**

| Env Var | Default | Description |
|---|---|---|
| `SKETCH_ENABLED` | true | เปิด/ปิด |
| `SKETCH_DIMENSIONS` | 128 | Bit vector width |
| `SKETCH_THRESHOLD` | 0.85 | Similarity threshold |

---

## 3-Layer Rate Limiting Architecture

```
Layer 1: Distributed Rate Limiter (external Java service)
  |- Global rate limit (all requests)
  |- Per-agent rate limit (per API key)
  |- Fail-open: ถ้า service ล่ม requests ผ่านได้
  |
Layer 2: Adaptive Concurrency Limiter (in-process)
  |- Per-model concurrency limits
  |- Envoy gradient + Netflix concurrency algorithm
  |- Auto-adjust: 429 -> halve limit, 200 -> gradient increase
  |- Cross-series fallback: >=70% utilization -> spillover 20% to lower
  |- Learned ceiling: 5-min decay after 429
  |
Layer 3: Token Budget Optimization (13-stage pipeline)
  |- Bandit learns best strategies
  |- Sketch detects duplicate prompts
  |- Caveman compresses output style
  |- All modules above
```

---

## Flow การทำงานแบบเต็ม

```
Client Request
     |
     v
[Token Estimation] -- ประเมิน token ก่อนส่ง (chars/ratio)
     |
     v
[Model Lookup] -- ดู context limit + max output จาก static map
     |
     v
[PasteGuard] -- ตรวจจับและแทนที่ secrets/PII ก่อนส่ง upstream
     | Secrets: API keys, private keys, JWT, bearer tokens (regex)
     | PII: EMAIL_ADDRESS, PHONE_NUMBER (Microsoft Presidio)
     | แทนที่ด้วย placeholder -> ส่ง upstream
     |
     v
[Whitespace Optimization] -- ลบ whitespace ซ้ำใน prose (preserve code)
     |
     v
[Sentence Dedup] -- ลบประโยคซ้ำ exact + semantic (Jaccard > 0.7)
     |
     v
[Chunker] -- Rabin-Karp CDC, reorder stable chunks first
     |
     v
[Delta Encode] -- LCS diff, ส่ง delta ถ้า savings >= 10%
     |
     v
[Sketch Dedup] -- SimHash near-duplicate, ข้ามถ้า similarity >= 0.85
     |
     v
[Packer] -- Greedy knapsack, เลือก context items ที่คุ้มที่สุด
     |
     v
[Budget Check] -- Green/Yellow/Red drives tier selection
     | Yellow (>50%): Summarizer + Caveman Lite
     | Red (>75%): Force truncate + Summarizer + Caveman Ultra
     v
[Summarizer] -- (Red only) Extractive: first sentence per paragraph, cap 30%
     |
     v
[Caveman] -- Append [OUTPUT STYLE] directive to system prompt
     | Lite: bullet points (70%)
     | Full: code only (50%)
     | Ultra: raw output (25%)
     v
[Intent Filter] -- Classify intent, filter irrelevant content
     |
     v
[Upstream API] -- ส่ง request ที่ optimize แล้ว
     |
     v
[Unmask] -- restore secrets/PII ใน response
     | Non-stream: UnmaskResponse()
     | Stream: StreamUnmasker แทนที่ placeholder ทีละ chunk
     |
     v
[Post-Proxy Feedback]
     |- Bandit: update reward (output/input)
     |- Prefetcher: record tool call, predict next via Markov
     |- Waste: check 7 heuristics, background scan 60s
     |- Cache ROI: estimate savings from cache hit
     |
     v
[Warm Start] -- (on next session start) หา similar session via cosine sim
     |- Compute 32-dim feature vector
     |- Match >= 0.5 -> preload optimizer state
     |- Store signature for future lookups
```

---

## PasteGuard - Data Privacy Masking

**จุดประสงค์:** ป้องกันส่ง secrets และ PII ไปยัง upstream LLM provider

**Flow:**
1. **Extract** - แยก text spans จาก JSON payload
2. **Secret Detection** - regex-based (OpenSSH key, PEM, AWS key, GitHub PAT, JWT, Bearer)
3. **Secret Masking** - แทนที่ด้วย placeholder `[[TYPE_N]]`
4. **PII Detection** - Microsoft Presidio (NLP-based)
5. **PII Masking** - แทนที่ PII ด้วย placeholder
6. **Proxy** - ส่ง masked body ไป upstream
7. **Unmask** - restore ค่าจริงกลับใน response

**Config:**

| Env Var | Default | Description |
|---|---|---|
| `PASTEGUARD_ENABLED` | - | เปิด/ปิด pipeline ทั้งหมด |
| `PASTEGUARD_SECRETS_ENABLED` | - | เปิด/ปิด secret detection |
| `PASTEGUARD_PII_ENABLED` | - | เปิด/ปิด PII detection |
| `PASTEGUARD_PRESIDIO_URL` | - | URL ของ Presidio analyzer |
| `PASTEGUARD_PII_SCORE_THRESHOLD` | 0.7 | Confidence threshold |
| `PASTEGUARD_PII_ENTITIES` | EMAIL_ADDRESS,PHONE_NUMBER | PII entity types |
| `PASTEGUARD_MAX_SCAN_CHARS` | 200K | ขีดจำกัด chars ต่อ scan |

---

## ผลประโยชน์สำหรับผู้ใช้ Gateway

| ประโยชน์ | รายละเอียด |
|----------|-----------|
| **ลดค่า API** | Whitespace + dedup + packer ลด input tokens ~5-25% |
| **ลด error** | Token estimation + model map หลีกเลี่ยง context overflow |
| **เร็วขึ้น** | เนื้อหาน้อยลง = upstream ตอบเร็วขึ้น + delta ลด bandwidth |
| **เสถียรกว่า** | Budget tracking + caveman ป้องกัน session crash |
| **ใช้ได้ทุก provider** | Model map ครอบคลุม Anthropic, OpenAI, Google, Z.AI |
| **เรียนรู้อัตโนมัติ** | Bandit เรียนรู้ strategy ที่ดีที่สุด, prefetcher ทำนาย request ถัดไป |
| **ปลอดภัย** | PasteGuard mask secrets/PII ก่อนส่ง upstream |
| **ตรวจจับ waste** | 7 heuristics ตรวจ loop, retry churn, oversized context |

---

## โครงสร้างไฟล์

| Module | Path |
|---|---|
| Tokenizer (estimation, whitespace, dedup, budget) | `api-gateway/tokenizer/` |
| Chunker (Rabin-Karp CDC) | `api-gateway/chunker/` |
| Packer (Greedy knapsack) | `api-gateway/packer/` |
| Delta (LCS diff) | `api-gateway/delta/` |
| Summarizer (extractive) | `api-gateway/summarizer/` |
| Caveman (prompt injection) | `api-gateway/caveman/` |
| Waste (7 heuristics) | `api-gateway/waste/` |
| Prefetcher (Markov chain) | `api-gateway/prefetcher/` |
| Warm Start (cosine similarity) | `api-gateway/warmstart/` |
| Bandit (LinUCB) | `api-gateway/bandit/` |
| Sketch (SimHash) | `api-gateway/sketch/` |
| Privacy/PasteGuard | `api-gateway/privacy/` |
| Filter (Intent) | `api-gateway/filter/` |
