# F16: Caveman - บีบ Output ด้วย Style Injection 4 ระดับ

## หลักการ

Caveman เป็น optimizer stage ประเภท **OUTPUT influence** - ไม่ได้บีบ output โดยตรง แต่ฉีด `[OUTPUT STYLE]` directive เข้าไปใน system prompt เพื่อสั่งให้ model ตอบสั้นลง

วิธีการ: นำ system prompt เดิม (อาจยาวหลายร้อยถึงหลายพันตัวอักษร) มาต่อท้ายด้วย style injection directive ขนาด ~229 chars ทำให้ model ปรับพฤติกรรมการตอบให้กระชับขึ้น

4 tiers ตาม budget level:

| Tier | Budget Level | ลด output ประมาณ | Directive |
|------|-------------|-------------------|-----------|
| **lite** | Green (< 60% context) | ~30% | ตัด pleasantries, ตอบบรรทัดเดียวถ้าได้ |
| **full** | Yellow (>= 60% context) | ~50% | ตัดทุก filler, ใช้ short variable names, ไม่ repeat คำถาม |
| **ultra** | Red (>= 80% context) | ~75% | Raw output เท่านั้น, ไม่มี natural language wrapper, ใช้ symbols/abbreviations |
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
    case 2: return TierUltra   // red budget (>=80% context)
    case 1: return TierFull    // yellow budget (>=60% context)
    default: return TierLite   // green budget (<60% context)
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

### 1. Skip Conditions
Caveman **จะไม่ทำงาน** เมื่อ:
- **Transparent mode** (debug/observability) เพื่อไม่ให้กระทบการวิเคราะห์ output จริงของ model
- **Images present** ใน request (corruption risk)
- **Z.AI provider** (GLM models ไม่มี prompt caching, optimizer เพิ่ม latency โดยไม่มี benefit)

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
