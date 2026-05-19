# Optimizer Pipeline Reference

Complete reference for all token optimization stages in the Claude OAuth request path.

---

## Architecture Overview

```
Client Request
    |
    v
[Budget Level Calculation] <-- pctUsed = tokens / context_window
    |                          green(<60%) / yellow(>=60%) / red(>=80%)
    v
[Skip Guard] --> skip if: images present OR Z.AI provider
    |
    v
[1. System Prompt Pipeline] (heavy)
    |-- semantic_dedup
    |-- chunker
    |-- delta (metrics only)
    |-- sketch (metrics only)
    |-- summarizer (red only)
    |-- textcomp
    |-- caveman_input + caveman_output
    |-- pordee (Thai only)
    v
[2. Message Pipeline] (lightweight)
    |-- whitespace optimization
    |-- sentence dedup
    |-- textcomp on message blocks
    |-- toolcomp on tool_result blocks
    v
[3. Tools Pipeline]
    |-- desctrim (description trimming)
    |-- toolfilter (unused tool removal)
    v
[4. Cache Guard]
    |-- cache breakpoint injection (preserve prompt cache)
    v
[5. Post-Response Telemetry]
    |-- prefetcher / waste / cache ROI / bandit feedback
```

Source: [optimizers.go](api-gateway/handler/optimizers.go) | [handler.go:1113-1298](api-gateway/handler/handler.go#L1113-L1298)

> **Thai examples**: Each stage below includes a `TH EXAMPLE` code block showing how it works with Thai text. Stages with `TH EXAMPLE` markers: Semantic Dedup, Chunker, Summarizer, TextComp, Caveman, Pordee, Whitespace, Sentence Dedup, ToolComp, DescTrim, ToolFilter, Cache Breakpoints, CompCache.

---

## Budget Level Calculation

Calculated before optimization starts. Determines which aggressive stages activate.

Source: [handler.go:1148-1169](api-gateway/handler/handler.go#L1148-L1169)

| Level | Condition | Threshold |
|-------|-----------|-----------|
| Green (0) | `pctUsed < 0.6` | Under 60% of context window |
| Yellow (1) | `pctUsed >= 0.6` | 60%+ of context window |
| Red (2) | `pctUsed >= 0.8` | 80%+ of context window |

Token estimation uses content-type-aware ratios:

| Content Type | Chars/Token |
|-------------|-------------|
| Code | 2.5 |
| JSON | 2.8 |
| Markdown | 3.5 |
| Plain Text | 4.0 |

Quick estimate fallback: `(len(text) + 3) / 4` (ceiling division)

---

## Stage Details

### 1. Semantic Dedup

**Source**: [tokenizer/optimizer.go](api-gateway/tokenizer/optimizer.go)
**Target**: System prompt
**Budget gate**: Always runs
**Feature ID**: F7

Removes semantically duplicate sentences by normalizing (lowercase, collapse whitespace, keep letters/digits only) and comparing with similarity threshold 0.7.

```
BEFORE:
"The function handles errors gracefully. It retries up to 3 times.
The Function Handles Errors Gracefully. After retries, it logs the failure.
It retries up to 3 times. Finally it returns nil."

AFTER:
"The function handles errors gracefully. It retries up to 3 times.
After retries, it logs the failure. Finally it returns nil."
```

- Preserves code blocks between ``` fences
- Skips if privacy placeholders (`__(SECRET|PII)_\d+__`) are present

```
TH EXAMPLE:
BEFORE:
"ฟังก์ชันนี้จัดการ error อย่างเหมาะสม มี retry สูงสุด 3 ครั้ง
ฟังก์ชันนี้จัดการ Error อย่างเหมาะสม หลัง retry จะ log การล้มเหลว
มี retry สูงสุด 3 ครั้ง สุดท้าย return nil"

AFTER:
"ฟังก์ชันนี้จัดการ error อย่างเหมาะสม มี retry สูงสุด 3 ครั้ง
หลัง retry จะ log การล้มเหลว สุดท้าย return nil"
```

---

### 2. Chunker

**Source**: [chunker/chunker.go](api-gateway/chunker/chunker.go)
**Target**: System prompt
**Budget gate**: Always runs
**Feature ID**: F1

Splits content into variable-size chunks using Rabin-Karp rolling hash, identifies "stable" chunks (previously seen in Redis >= 2 times), and reorders to place stable chunks first. This maximizes prompt cache prefix matches with previous requests.

```
BEFORE: "[Header A - seen before] [New unique content B]
         [Footer C - seen before] [New unique content D]"

AFTER:  "[Header A - seen before] [Footer C - seen before]
         [New unique content B] [New unique content D]"
```

```
TH EXAMPLE (system prompt with Thai content, stable chunks from Redis):
BEFORE: "คุณคือ Go expert. [คำสั่งที่เคยเห็น - stable] [ข้อมูลใหม่เกี่ยวกับ bug]
         [คำสั่งที่เคยเห็น - stable] [ข้อมูลใหม่เกี่ยวกับ deployment]"

AFTER:  "คุณคือ Go expert. [คำสั่งที่เคยเห็น - stable] [คำสั่งที่เคยเห็น - stable]
         [ข้อมูลใหม่เกี่ยวกับ bug] [ข้อมูลใหม่เกี่ยวกับ deployment]"
         (stable chunks moved first to maximize cache prefix match)
```

Configuration:

| Parameter | Default | Description |
|-----------|---------|-------------|
| MinChunk | 128 chars | Minimum chunk size before boundary detection |
| MaxChunk | 4096 chars | Hard split limit |
| Window | 48 chars | Rolling hash window size |
| StableThreshold | 2 | Times seen in Redis to be "stable" |
| TTL | 24 hours | Redis key expiry |

Chunking algorithm:
1. If content < MinChunk, return single chunk with SHA-256 hash
2. Slide window over content, compute rolling hash: `hash = hash*31 + uint64(byte)`
3. When `hash % 256 == 0` (content-deterministic boundary), create split point
4. Hard split at MaxChunk if no boundary found
5. Each chunk is SHA-256 hashed (first 12 bytes = 24 hex chars) for identity
6. Reorder: stable chunks first, novel chunks after

Requires Redis.

---

### 3. Delta Encoding

**Source**: [delta/delta.go](api-gateway/delta/delta.go)
**Target**: System prompt
**Budget gate**: Always runs (metrics only)
**Feature ID**: F8

Does NOT modify content. Computes delta encoding potential and records as metrics for analysis. Reports how many bytes could be saved if delta encoding were applied.

---

### 4. Sketch Dedup

**Source**: [sketch/sketch.go](api-gateway/sketch/sketch.go)
**Target**: System prompt
**Budget gate**: Always runs (metrics only)
**Feature ID**: F9

Does NOT modify content. Checks whether the system prompt has been seen before (cache fingerprint). Records `sketch_dedup` metric for duplicate detection analysis.

---

### 5. Summarizer

**Source**: [summarizer/summarizer.go](api-gateway/summarizer/summarizer.go) | [summarizer/textrank.go](api-gateway/summarizer/textrank.go)
**Target**: System prompt
**Budget gate**: Red only (`budgetLevel >= 2`)
**Feature ID**: F6

Summarizes system prompt using TextRank algorithm (graph-based sentence ranking). Does NOT use an LLM for summarization.

```
BEFORE: (2000 chars of detailed system prompt with multiple paragraphs)

AFTER:  (800 chars keeping only the highest-ranked key sentences)
```

Only activates when >= 80% of the context window is consumed.

```
TH EXAMPLE (Thai system prompt, red budget):
BEFORE: (2000 chars Thai prompt)
"คุณคือ senior Go developer ที่เชี่ยวชาญ concurrent programming
คุณต้องให้ตัวอย่าง code ที่ชัดเจนและมี documentation ดี
อธิบายเหตุผลเบื้องหลังการตัดสินใจทางสถาปัตยกรรม
พิจารณา edge cases และ error handling ในทุก solution
ปฏิบัติตาม Go best practices และ idiomatic patterns
จำไว้ว่า context ใน Go ควรถูกส่งผ่านเป็น parameter แรกเสมอ
..."

AFTER: (~800 chars, TextRank keeps highest-ranked sentences)
"คุณคือ senior Go developer ที่เชี่ยวชาญ concurrent programming
ปฏิบัติตาม Go best practices และ idiomatic patterns
พิจารณา edge cases และ error handling ในทุก solution
จำไว้ว่า context ใน Go ควรถูกส่งผ่านเป็น parameter แรกเสมอ"
```

---

### 6. TextComp (Regex Compression)

**Source**: [textcomp/textcomp.go](api-gateway/textcomp/textcomp.go)
**Target**: System prompt + message content + message blocks
**Budget gate**: Always runs
**Feature ID**: F17

Applies regex find-and-replace rules to compress text by removing filler phrases, hedge words, and verbose constructions.

**Pipeline**: Mask protected regions -> Apply rules -> Unmask -> Cleanup

**Mask phase** (protects these from modification):
- `` ```...``` `` fenced code blocks -> `__FCODE_N__`
- `` `code` `` inline code -> `__ICODE_N__`
- `https?://...` URLs -> `__URL_N__`
- `"quoted strings"` -> `__QSTR_N__`

**Apply phase** - 3 rule categories + optional aggressive:

Filler rules (10, removed entirely):
- `I would like to`, `Could you please`, `I was wondering if`, `Can you please`, `Kindly`, etc.

Hedge rules (12, removed entirely):
- `sort of`, `kind of`, `a little bit`, `somewhat`, `quite`, `rather`, `basically`, `actually`, `literally`, `just`, `really`, `very very`

Verbose-to-compact (31 replacements):
| Verbose | Compact |
|---------|---------|
| `due to the fact that` | `because` |
| `in order to` | `to` |
| `at this point in time` | `now` |
| `prior to` | `before` |
| `has the ability to` | `can` |
| `Furthermore,` | `Also` |
| `Therefore,` | `So` |
| `Nevertheless,` | `But` |
| `a large number of` | `many` |
| `take into consideration` | `consider` |
| `in the event that` | `if` |
| `with regard to` | `about` |

Aggressive-only rules (11, mode=`aggressive`):
- `It is worth noting that`, `Please note that`, `In my opinion,`, `I think that`, `It seems that`, etc.

```
BEFORE:
"I would like to ask if you could please help me out. I was wondering
if you could carry out a review of the code in order to find bugs
prior to the deployment. Furthermore, it would be great if you could
also take into consideration the performance implications. It is
important to note that the system actually has the ability to handle
quite a large number of requests."

AFTER (balanced mode):
" help me out.  you could do a review of the code to find bugs
before the deployment. Also,  you could also consider the
performance implications. It is important to note that the system
can handle  many requests."

AFTER (aggressive mode - also removes "It is important to note that"):
" help me out.  you could do a review of the code to find bugs
before the deployment. Also,  you could also consider the
performance implications.  the system can handle  many requests."
```

```
TH EXAMPLE (mixed TH/EN - removes English filler from Thai prompt):
BEFORE:
"I would like to ถามว่า Could you please ช่วยอธิบายว่า Kubernetes
sort of ทำงานยังไง basically มัน utilize cache อย่างไร
in order to optimize performance"

AFTER:
" ถามว่า  ช่วยอธิบายว่า Kubernetes ทำงานยังไง มัน use cache อย่างไร
to optimize performance"
```

Configuration:

| Env Var | Default | Description |
|--------|---------|-------------|
| `TEXTCOMP_ENABLED` | `true` | Enable/disable |
| `TEXTCOMP_MODE` | `balanced` | `balanced` or `aggressive` |

---

### 7. Caveman (Input Compression + Output Style Injection)

**Source**: [caveman/caveman.go](api-gateway/caveman/caveman.go)
**Target**: System prompt
**Budget gate**: Tier selected by budget level
**Feature ID**: F16

Two-phase operation: compress input text, then inject terse output style directives.

**Phase 1 - CompressInput** (regex-based input compression):

Mask phase (protects):
- `` ```...``` `` fenced code -> `__FCODE`
- `` `code` `` inline code -> `__ICODE`
- `https?://...` URLs -> `__URL`
- `(~/|/\w)[\w/.-]*\.\w+` file paths -> `__FPATH`

Apply phase - 3 rule categories:

Pleasantry rules (10, removed):
- `I'd be happy to`, `Sure!`, `Certainly!`, `Of course!`, `Hope this helps!`, `Let me know if...`, `Feel free to...`, etc.

Instruction fluff rules (9, removed):
- `you should always`, `make sure to`, `remember to`, `you need to`, `it is important to`, `you should`, etc.

Synonym rules (23 replacements):
| Verbose | Short |
|---------|-------|
| `utilize` | `use` |
| `approximately` | `~` |
| `initiate` | `start` |
| `terminate` | `end` |
| `endeavor` | `try` |
| `facilitate` | `help` |
| `regarding` | `about` |
| `the following` | `these` |
| `this is because` | `because` |
| `in order to` | `to` |
| `will not work properly` | `breaks` |

```
BEFORE:
"I'd be happy to help you with this! You should make sure to utilize
the extensive documentation in order to implement a solution for the error.
Feel free to reach out if you need additional help."

AFTER:
"  you with this!    use the big documentation  fix the error.  "
```

```
TH EXAMPLE (Thai prompt with English pleasantries/instructions):
BEFORE:
"I'd be happy to ช่วยคุณครับ! You should make sure to utilize namespace
in order to implement a solution for the deployment issue.
Feel free to ถามเพิ่มได้นะครับ"

AFTER:
" ช่วยคุณครับ!   use namespace  fix the deployment issue.
 ถามเพิ่มได้นะครับ"
```

**Phase 2 - Output Injection** (append terseness directives):

| Tier | Budget Level | Output Ratio | Key Directive |
|------|-------------|--------------|---------------|
| Lite | Green (0) | 0.70 | Be concise, bullet points, skip pleasantries, one-sentence answers |
| Full | Yellow (1) | 0.50 | Extremely terse, code only when asked, no explanations, tables over paragraphs |
| Ultra | Red (2) | 0.25 | Raw output only, compressed notation (&, \|, =>), zero surrounding prose |
| Wenyan | Special | 0.30 | Classical notation: facts only, `/` for "or", `&` for "and", `->` for "becomes" |

```
BEFORE: "You are a helpful coding assistant."

AFTER (TierLite):
"You are a helpful coding assistant.
[OUTPUT STYLE - lite]
Be concise. Use bullet points. Skip pleasantries and filler phrases.
Avoid: "Great question!", "Certainly!", "I'd be happy to help!", "In summary,", "Hope this helps!".
One sentence answers when possible."

AFTER (TierUltra):
"You are a helpful coding assistant.
[OUTPUT STYLE - ultra]
Raw output only. No natural language wrapper. No markdown formatting unless code.
Use compressed notation: &, |, =>, ternary.
Skip all context setting. Direct answer. No conversational framing.
Maximum compression: abbreviations, symbols, implicit context.
Output MUST be copy-paste ready with zero surrounding prose."
```

```
TH EXAMPLE (Thai system prompt with output style):
BEFORE: "คุณคือ AI assistant ที่เชี่ยวชาญด้าน Go programming"

AFTER (TierLite):
"คุณคือ AI assistant ที่เชี่ยวชาญด้าน Go programming
[OUTPUT STYLE - lite]
Be concise. Use bullet points. Skip pleasantries and filler phrases.
Avoid: "Great question!", "Certainly!", "I'd be happy to help!".
One sentence answers when possible."

AFTER (TierUltra):
"คุณคือ AI assistant ที่เชี่ยวชาญด้าน Go programming
[OUTPUT STYLE - ultra]
Raw output only. No natural language wrapper. No markdown formatting unless code.
Use compressed notation: &, |, =>, ternary.
Skip all context setting. Direct answer. No conversational framing.
Maximum compression: abbreviations, symbols, implicit context.
Output MUST be copy-paste ready with zero surrounding prose."
```

Validation: Checks code block count preserved and >= 80% identifier retention after compression.

---

### 8. Pordee (Thai Terse Output Injection)

**Source**: [pordee/pordee.go](api-gateway/pordee/pordee.go)
**Target**: System prompt (only when Thai text detected)
**Budget gate**: Always runs (level selected by budget)
**Feature ID**: F17

Detects Thai text via Unicode range `\x{0E00}-\x{0E7F}` and injects Thai-language terseness directives. The Thai equivalent of Caveman.

**Tier system:**

| Tier | Output Ratio | Behavior |
|------|-------------|----------|
| Lite | 0.80 | Drops polite particles, hedging, filler. Keeps full grammar. |
| Full | 0.27 | All of Lite + drops pleasantries, English filler, enforces short synonyms. Fragments OK. |

Lite drops:
- Polite particles: ครับ, ค่ะ, นะคะ, นะครับ, ครับผม, จ้า, จ๋า
- Hedging: อาจจะ, น่าจะ, จริงๆแล้ว, คงจะ, น่าจะเป็น
- Filler: ก็, ก็คือ, อ่ะ, อะ

Full additionally drops:
- Pleasantries: ได้เลยครับ, แน่นอน
- English filler: just, really, basically, actually, simply
- Enforces synonyms: ดู not ตรวจสอบ, แก้ not ทำการแก้ไข, เพราะ not เนื่องจาก

```
BEFORE: "You are a helpful coding assistant."

AFTER (Lite):
"You are a helpful coding assistant.
[PORDEE MODE - lite]
ตอบไทยกระชับ. Drop polite particles (ครับ, ค่ะ, นะคะ, นะครับ, ครับผม, จ้า, จ๋า).
Drop hedging (อาจจะ, น่าจะ, จริงๆแล้ว, คงจะ, น่าจะเป็น).
Drop filler (ก็, ก็คือ, อ่ะ, อะ). Use short synonyms.
Keep full grammar. Code/commits/errors: write normal English."

AFTER (Full):
"You are a helpful coding assistant.
[PORDEE MODE - full]
ตอบไทยกระชับมาก. ACTIVE EVERY RESPONSE. No drift.
Drop: polite particles, hedging, filler, pleasantries, English filler.
Short synonyms: ดู not ตรวจสอบ, แก้ not ทำการแก้ไข, เพราะ not เนื่องจาก.
Fragments OK. Pattern: [ของ] [ทำ] [เหตุผล]. [ขั้นต่ำ].
Auto-clarity: drop pordee for security warnings, irreversible actions.
Code/commits/PRs/code comments: write normal English."
```

```
TH EXAMPLE (Thai output before/after pordee injection):

WITHOUT PORDEE, model responds:
"ครับผม ผมดีใจที่ได้ช่วยครับ! ในการที่จะตรวจสอบการทำงานของ pod
ใน Kubernetes cluster คุณสามารถใช้คำสั่ง kubectl get pods ได้ครับ
ซึ่งจะแสดงรายการ pod ทั้งหมดที่ทำงานอยู่ครับผม"

WITH PORDEE (Lite - drops polite particles + hedging):
"ตรวจสอบ pod ใน Kubernetes cluster ใช้ `kubectl get pods`
แสดงรายการ pod ที่ทำงานอยู่"

WITH PORDEE (Full - drops pleasantries + enforces short synonyms):
"ใช้ `kubectl get pods` ดู pod ทั้งหมด"
```

Auto-disable conditions:
- Security warnings
- Irreversible actions
- Multi-step sequences
- User asks "อะไรนะ" / "พูดอีกที" / "อธิบายชัดๆ"

Budget red (>= 80%) forces Full level regardless of config.

---

### 9. Whitespace Optimization

**Source**: [tokenizer/optimizer.go](api-gateway/tokenizer/optimizer.go)
**Target**: Message content (string and text blocks)
**Budget gate**: Always runs

Collapses whitespace in prose while preserving code blocks.

```
BEFORE:
"Here is some   text   with  extra   spaces.


And this    has  too.



Total 3 blank lines above."

AFTER:
"Here is some text with extra spaces.

And this has too.

Total 3 blank lines above."
```

```
TH EXAMPLE:
BEFORE:
"ฟังก์ชันนี้   จัดการ   error  อย่างเหมาะสม


และ   มี   retry  mechanism



ที่   ทำงาน   ได้ดี"

AFTER:
"ฟังก์ชันนี้ จัดการ error อย่างเหมาะสม

และ มี retry mechanism

ที่ ทำงาน ได้ดี"
```

Rules:
- Consecutive spaces -> single space (prose only, not code blocks)
- Tabs preserved (carry semantic indentation)
- 3+ consecutive blank lines -> 2 blank lines
- Trailing spaces trimmed from lines (not tabs)
- Leading/trailing blank lines trimmed

---

### 10. Sentence Dedup

**Source**: [tokenizer/optimizer.go](api-gateway/tokenizer/optimizer.go)
**Target**: Message content
**Budget gate**: Always runs

Removes duplicate sentences by normalizing (lowercase, collapse whitespace, keep letters/digits only) and checking for prior occurrence.

```
BEFORE:
"The function handles errors gracefully. It retries up to 3 times.
The Function Handles Errors Gracefully. After retries, it logs the failure.
It retries up to 3 times. Finally it returns nil."

AFTER:
"The function handles errors gracefully. It retries up to 3 times.
After retries, it logs the failure. Finally it returns nil."
```

```
TH EXAMPLE:
BEFORE:
"Deployment ผ่าน CI/CD pipeline. ตรวจสอบด้วย ArgoCD.
Deployment ผ่าน CI/CD pipeline. หลัง merge จะ auto-sync.
ตรวจสอบด้วย ArgoCD. สุดท้าย deploy ไป production"

AFTER:
"Deployment ผ่าน CI/CD pipeline. ตรวจสอบด้วย ArgoCD.
หลัง merge จะ auto-sync. สุดท้าย deploy ไป production"
```

```
TH EXAMPLE (pure Thai, no English sentence boundaries):
BEFORE:
"เซิร์ฟเวอร์เริ่มทำงานแล้ว เชื่อมต่อกับ database สำเร็จ
เซิร์ฟเวอร์เริ่มทำงานแล้ว รอ request จาก client
เชื่อมต่อกับ database สำเร็จ พร้อมรับ traffic"

AFTER:
"เซิร์ฟเวอร์เริ่มทำงานแล้ว เชื่อมต่อกับ database สำเร็จ
รอ request จาก client พร้อมรับ traffic"
```

Note: sentence dedup uses `[.!?]\s+` boundary regex, so pure Thai text without English punctuation may not split correctly. The normalization still works for exact/near-duplicate removal.

- Sentence boundary: regex `[.!?]\s+`
- Skips if privacy placeholders present

---

### 11. ToolComp (Tool Result Compression)

**Source**: [toolcomp/toolcomp.go](api-gateway/toolcomp/toolcomp.go)
**Target**: `tool_result` content blocks
**Budget gate**: Always runs

Format-aware compression for tool results. Detects content format and applies targeted compression.

**Format detection** (priority order):

| Priority | Format | Detection |
|----------|--------|-----------|
| 1 | JSON | Starts with `{`/`[` and ends with `}`/`]`, passes `json.Valid()` |
| 2 | Diff | First line starts with `diff --git`, `@@`, `--- `, or `+++ ` |
| 3 | Log | >50% of first 10 lines match timestamp/level patterns |
| 4 | Table | >50% of first 10 lines have >= 2 pipe `|` characters |
| 5 | ShellLs | >50% of first 10 lines match `ls -l` format or end with file extensions |
| 6 | Prose | Fallback (no compression) |

**Compression strategies:**

| Format | Strategy |
|--------|----------|
| JSON | `json.Compact()` - remove all whitespace between tokens |
| ShellLs | Keep first N-5 lines + "... N more files ..." + last 2 lines |
| Table | Remove separator lines, keep first N rows + "... N more rows ..." |
| Diff | Keep headers + changed lines (+/-) + 1 context line after each change |
| Log | Deduplicate consecutive identical lines (ignoring timestamps) |

```
BEFORE (JSON):
{
  "name": "test",
  "items": [1, 2, 3]
}
AFTER: {"name":"test","items":[1,2,3]}

BEFORE (Log, 100 lines with duplicates):
2024-01-15 10:00:00 [INFO] Server started
2024-01-15 10:00:01 [INFO] Server started
2024-01-15 10:00:02 [INFO] Server started
2024-01-15 10:00:03 [WARN] Disk space low
2024-01-15 10:00:04 [WARN] Disk space low
... (95 more lines)

AFTER:
2024-01-15 10:00:00 [INFO] Server started
2024-01-15 10:00:03 [WARN] Disk space low
... 48 more log lines ...

BEFORE (Diff, 120 lines):
diff --git a/main.go b/main.go
@@ -10,6 +10,8 @@
 context line 1
 context line 2
 context line 3
-old line 1
-old line 2
+new line 1
+new line 2
 context line 4
... (100+ unchanged context lines)

AFTER:
diff --git a/main.go b/main.go
@@ -10,6 +10,8 @@
-old line 1
-old line 2
+new line 1
+new line 2
context line 4
... 80 unchanged lines omitted ...
```

```
TH EXAMPLE (Log with Thai messages):
BEFORE (100 lines, many duplicate):
2024-01-15 10:00:00 [INFO] pod/api-server-7d9f8 เริ่มทำงาน
2024-01-15 10:00:01 [INFO] pod/api-server-7d9f8 เริ่มทำงาน
2024-01-15 10:00:02 [INFO] pod/api-server-7d9f8 เริ่มทำงาน
2024-01-15 10:00:03 [WARN] พื้นที่ดิสก์เหลือน้อย
2024-01-15 10:00:04 [WARN] พื้นที่ดิสก์เหลือน้อย
... (95 more lines)

AFTER:
2024-01-15 10:00:00 [INFO] pod/api-server-7d9f8 เริ่มทำงาน
2024-01-15 10:00:03 [WARN] พื้นที่ดิสก์เหลือน้อย
... 48 more log lines ...
```

Configuration:

| Env Var | Default | Description |
|--------|---------|-------------|
| `TOOLCOMP_ENABLED` | `true` | Enable/disable |
| `TOOLCOMP_MAX_LINES` | `50` | Max lines to keep |

Only compresses when result is shorter. Minimum size: 256 bytes.

---

### 12. DescTrim (Tool Description Trimming)

**Source**: [desctrim/desctrim.go](api-gateway/desctrim/desctrim.go)
**Target**: Tool descriptions in the manifest
**Budget gate**: Always runs

Trims verbose tool descriptions through 3 progressive phases.

```
BEFORE (KubernetesApply, 450 chars):
"Apply configuration changes to a Kubernetes cluster. Supports applying
manifests from files or stdin, with dry-run mode for validation.

This tool handles namespace-scoped and cluster-scoped resources. It supports
strategic merge patches, JSON patches, and server-side apply. Use --force
to recreate resources when needed.

WARNING: Always review changes before applying to production clusters.
Use dry-run first to preview the effects of your changes."

AFTER (Phase 1 - first paragraph, 120 chars):
"Apply configuration changes to a Kubernetes cluster. Supports applying
manifests from files or stdin, with dry-run mode for validation."
```

```
BEFORE (MyTool, 350 chars, no paragraph break):
"This tool performs comprehensive analysis of your entire codebase structure
and provides detailed reports on code quality, complexity metrics, and
potential refactoring opportunities. It can also integrate with CI/CD
pipelines for automated checks. Additional features include..."

AFTER (Phase 2 - first sentence, ~180 chars):
"This tool performs comprehensive analysis of your entire codebase structure
and provides detailed reports on code quality, complexity metrics, and
potential refactoring opportunities."
```

```
TH EXAMPLE (tool with Thai description):
BEFORE (400 chars):
"ตรวจสอบสถานะของ Kubernetes cluster โดยแสดงข้อมูล nodes, pods, และ deployments
ทั้งหมด รวมถึง resource usage และ health status

สามารถกรองตาม namespace หรือ label selector ได้ รองรับทั้ง cluster-scope
และ namespace-scope สามารถแสดงข้อมูลในรูปแบบ JSON หรือ YAML ได้
WARNING: ควรระวังเรื่อง permissions เพราะอาจมีข้อมูล sensitive"

AFTER (Phase 1 - first paragraph, ~130 chars):
"ตรวจสอบสถานะของ Kubernetes cluster โดยแสดงข้อมูล nodes, pods, และ deployments
ทั้งหมด รวมถึง resource usage และ health status"
```

3 phases (applied in order):
1. Truncate at first `\n\n` (first paragraph)
2. If still > MaxLen, truncate at first `. ` or `.\n` boundary (first sentence)
3. If still > MaxLen, hard truncate at MaxLen + append `...`

Configuration:

| Env Var | Default | Description |
|--------|---------|-------------|
| `DESCTRIM_ENABLED` | `true` | Enable/disable |
| `DESCTRIM_MAX_LEN` | `200` | Maximum description length |
| `DESCTRIM_ALWAYS_SKIP` | `Read,Edit,Write,Bash` | Tools to never trim |

---

### 13. ToolFilter (Unused Tool Removal)

**Source**: [toolfilter/toolfilter.go](api-gateway/toolfilter/toolfilter.go)
**Target**: Tool manifest
**Budget gate**: Always runs (when tool count > MaxTools)

When tools exceed `MaxTools`, scores and ranks by relevance to recent messages, keeps only top N.

```
BEFORE (25 tools): [Read, Edit, Write, Bash, GrepTool, GlobTool, SymbolSearch,
  DeployTool, BuildTool, TestRunner, DatabaseQuery, DockerExec, KubernetesApply,
  TerraformPlan, HelmUpgrade, AnsiblePlaybook, SSHExec, ...]

User message: "search for all uses of function foo"

AFTER (15 tools): [Read, Edit, Write, Bash (always-keep),
  GrepTool (search intent +5.0 + keyword match +2.0 x2),
  GlobTool (search intent +5.0),
  SymbolSearch (search intent +5.0 + keyword match),
  ... top 7 scored tools only]
```

```
TH EXAMPLE (Thai user message):
User message: "ค้นหาที่ใช้ function foo ทั้งหมด"

AFTER (15 tools): [Read, Edit, Write, Bash (always-keep),
  GrepTool (search intent +5.0 + "ค้นหา" ~= "search" +2.0),
  GlobTool (search intent +5.0),
  SymbolSearch (search intent +5.0),
  ... top scored tools only]
```

Intent classification (keyword counting):
| Intent | Keywords |
|--------|----------|
| code | fix, implement, add, create, write, edit, modify, update, refactor |
| search | find, search, where, locate, grep, list, show |
| analysis | analyze, review, explain, understand, how, why, what |
| action | run, execute, deploy, build, test, start, restart |

Scoring:
- Intent match: +5.0
- Keyword overlap: +2.0 per keyword found in tool name/description
- Description length bonus: `len(description) / 1000.0`

Configuration:

| Env Var | Default | Description |
|--------|---------|-------------|
| `TOOLFILTER_ENABLED` | `true` | Enable/disable |
| `TOOLFILTER_MAX_TOOLS` | `15` | Max tools to keep |
| `TOOLFILTER_ALWAYS_KEEP` | `Read,Edit,Write,Bash` | Tools to never remove |

---

### 14. Cache Breakpoint Injection

**Source**: [handler.go:2416-2482](api-gateway/handler/handler.go#L2416-L2482)
**Target**: Message blocks
**Budget gate**: When message blocks > 18

Anthropic's prompt cache has a 20-block lookback window. In long conversations, the system prompt cache can be pushed beyond this window, causing full cache misses. This stage injects `cache_control: {"type": "ephemeral"}` every 18 blocks to keep the cache alive.

```json
BEFORE: [system block] [msg1] [msg2] ... [msg20] [msg21] [msg22]

AFTER:  [system block] [msg1] [msg2] ... [msg18 + cache_control] [msg19] [msg20] [msg21 + cache_control] [msg22]
```

```
TH EXAMPLE (long Thai conversation):
BEFORE: [system "คุณคือ Go expert..."] [msg1 "ช่วยแก้บั๊ก..."] [msg2 "ลองดู..."]
  ... [msg18 "ยังไม่ผ่าน..."] [msg19 "ลอง debug..."] ... [msg22 "ได้แล้ว!"]

AFTER:  [system "คุณคือ Go expert..."] [msg1] ... [msg18 + cache_control]
  [msg19] ... [msg21 + cache_control] [msg22]
  (cache_control injected every 18 blocks to preserve prompt cache)
```

- Safety margin: 2 blocks (threshold = 20 - 2 = 18)
- Only injects on `text` type blocks
- Walks messages in reverse, counting blocks since last cache_control

---

### 15. CompCache (Redis Compression Cache)

**Source**: [compcache/compcache.go](api-gateway/compcache/compcache.go)
**Target**: Redis values (infrastructure layer)
**Budget gate**: Values >= 512 bytes

Transparent zstd compression for Redis values. Not a text transformation - reduces Redis memory usage.

```
BEFORE (stored value, 2000 bytes):
{"messages":[{"role":"user","content":"...very long JSON..."}],"model":"claude-sonnet-4-6"}

AFTER (stored with "zstd:" prefix, ~600 bytes):
zstd:<binary zstd-compressed data>
```

```
TH EXAMPLE (Thai session stored in Redis):
BEFORE (2500 bytes):
{"messages":[{"role":"user","content":"ช่วยอธิบาย Kubernetes pod lifecycle..."}],"model":"claude-sonnet-4-6"}

AFTER (~750 bytes):
zstd:<binary zstd-compressed data>
```

- Write: compress with zstd if result is smaller, store with `zstd:` prefix
- Read: detect `zstd:` prefix, decompress if present
- Backward-compatible with previously stored uncompressed values

Configuration:

| Env Var | Default | Description |
|--------|---------|-------------|
| `COMPCACHE_ENABLED` | `true` | Enable/disable |
| `COMPCACHE_MIN_SIZE` | `512` | Minimum value size to compress |
| `COMPCACHE_LEVEL` | `3` | Zstd compression level (1-22) |

---

### 16. Post-Proxy Feedback (Telemetry)

**Source**: [optimizers.go:324-354](api-gateway/handler/optimizers.go#L324-L354)
**Target**: Post-response telemetry
**Budget gate**: Always runs

Records telemetry after each proxy response completes:

| Stage | What it records |
|-------|----------------|
| Prefetcher (F4) | Session/model usage patterns |
| Waste Detector (F11) | Wasted token detection |
| Cache ROI (F14) | Cache hit savings |
| Bandit (F5) | Multi-armed bandit reward for model selection |

---

## Execution Order Summary

### System Prompt Pipeline

| # | Stage | Target | Budget Gate | Modifies Content |
|---|-------|--------|-------------|------------------|
| 1 | Semantic Dedup | system | always | Yes - removes duplicate sentences |
| 2 | Chunker | system | always | Yes - reorders for cache hit |
| 3 | Delta | system | always | No - metrics only |
| 4 | Sketch | system | always | No - metrics only |
| 5 | Summarizer | system | red only | Yes - TextRank summary |
| 6 | TextComp | system | always | Yes - regex compression |
| 7 | Caveman Input | system | always | Yes - remove pleasantries/fluff |
| 8 | Caveman Output | system | tier by budget | Yes - append terse style |
| 9 | Pordee | system (Thai) | always | Yes - append Thai terse style |

### Message Pipeline

| # | Stage | Target | Budget Gate | Modifies Content |
|---|-------|--------|-------------|------------------|
| 10 | Whitespace | messages | always | Yes - collapse spaces |
| 11 | Sentence Dedup | messages | always | Yes - remove duplicate sentences |
| 12 | TextComp | messages (string content only) | always | Yes - regex compression |
| 13 | ToolComp | tool_result | always | Yes - format-aware compression |

### Tools Pipeline

| # | Stage | Target | Budget Gate | Modifies Content |
|---|-------|--------|-------------|------------------|
| 14 | DescTrim | tool descriptions | always | Yes - trim to first paragraph/sentence |
| 15 | ToolFilter | tool manifest | > 15 tools | Yes - remove unused tools |

### Post-Request

| # | Stage | Target | Budget Gate | Modifies Content |
|---|-------|--------|-------------|------------------|
| 16 | Cache Breakpoints | messages | > 18 blocks | Yes - inject cache_control |
| 17 | CompCache | Redis values | >= 512 bytes | Yes - zstd compress |

---

## Skip Conditions

The entire optimizer pipeline is skipped when:
- Request contains images (corruption risk)
- Provider is Z.AI (no prompt caching on GLM models, optimizer adds latency without benefit)

Individual stages skip when:
- Stage instance is nil (not initialized)
- Per-profile override sets stage to `false`
- Content is empty or below minimum size threshold
- Privacy placeholders detected (for dedup stages)
- Code blocks detected (for whitespace stages)

---

## Per-Profile Overrides

Source: [handler/profile.go](api-gateway/handler/profile.go)

Each profile can override optimizer stages via `OptimizerOverrides` map:

```json
{
  "optimizerOverrides": {
    "semantic_dedup": true,
    "chunker": false,
    "textcomp": true,
    "caveman": false,
    "pordee": true,
    "desctrim": false,
    "toolfilter": true,
    "toolcomp": false,
    "summarizer": false,
    "delta": true,
    "sketch": true
  }
}
```

Override logic:
- `false` -> force disable (skip)
- `true` + instance non-nil -> force enable (run)
- `true` + instance nil -> cannot enable (skip)
- absent -> use global default (instance non-nil = enabled)

---

## Environment Variables

| Env Var | Default | Stage |
|---------|---------|-------|
| `TEXTCOMP_ENABLED` | `true` | TextComp |
| `TEXTCOMP_MODE` | `balanced` | TextComp |
| `TOOLCOMP_ENABLED` | `true` | ToolComp |
| `TOOLCOMP_MAX_LINES` | `50` | ToolComp |
| `TOOLFILTER_ENABLED` | `true` | ToolFilter |
| `TOOLFILTER_MAX_TOOLS` | `15` | ToolFilter |
| `TOOLFILTER_ALWAYS_KEEP` | `Read,Edit,Write,Bash` | ToolFilter |
| `DESCTRIM_ENABLED` | `true` | DescTrim |
| `DESCTRIM_MAX_LEN` | `200` | DescTrim |
| `DESCTRIM_ALWAYS_SKIP` | `Read,Edit,Write,Bash` | DescTrim |
| `COMPCACHE_ENABLED` | `true` | CompCache |
| `COMPCACHE_MIN_SIZE` | `512` | CompCache |
| `COMPCACHE_LEVEL` | `3` | CompCache |
| `CAVEMAN_ENABLED` | `true` | Caveman |
| `CAVEMAN_MIN_SIZE` | `500` | Caveman |
| `PORDEE_ENABLED` | `true` | Pordee |
| `CHUNKER_ENABLED` | `true` | Chunker |
