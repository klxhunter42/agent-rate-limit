# Token Optimization - สรุปภาษาไทย

วันที่: 2026-04-21

---

## ภาพรวม

Gateway ได้ integrate เทคนิค token optimization จากการวิเคราะห์โปรเจกต์ 7 ตัวใน `improvements/` directory ทั้งหมดมีเป้าหมายลด token usage และลดค่าใช้จ่าย API

---

## สิ่งที่ Integrate แล้ว

### 1. Whitespace Optimization (เพิ่มประสิทธิภาพ Whitespace)
**แหล่งที่มา:** token-optimizer-mcp
**แนวคิด:** ลบ whitespace ซ้ำซ้อนใน prose text แต่ preserve code block เดิม
- ลบ double spaces -> single space
- ลบ trailing whitespace ต่อบรรทัด
- ลบ blank lines ที่เกิน 2 บรรทัดติดกัน
- ไม่ยุ่งกับ code block ระหว่าง triple backticks
**ผลที่ได้:** ประหยัด ~3-5% tokens ต่อ request, zero-cost processing

### 2. Content-Aware Token Estimation (ประเมิน Token ตามประเภทเนื้อหา)
**แหล่งที่มา:** token-optimizer-mcp, claude-context
**แนวคิด:** ใช้ chars-per-token ratio ต่างกันตาม content type
- Code: chars / 2.5
- JSON: chars / 2.8
- Markdown: chars / 3.5
- Text: chars / 4.0
**ผลที่ได้:** ประเมิน cost ต่อ request ได้ก่อนส่ง upstream, ใช้ตัดสินใจ routing/truncation

### 3. Head/Tail Truncation (ตัดเนื้อหาแบบรักษาหัว-ท้าย)
**แหล่งที่มา:** context-mode, token-optimizer-mcp
**แนวคิด:** เมื่อ response เกิน limit จะ preserve 40% head (context เริ่มต้น) + 60% tail (ข้อความล่าสุด/errors)
**ผลที่ได้:** ไม่เสีย context สำคัญเวลา truncate, ต่างจาก naive truncation ที่ตัดแค่ tail

### 4. Static Model Capability Map (ตารางขีดความสามารถ Model)
**แหล่งที่มา:** claude-context, token-optimizer
**แนวคิด:** Hardcode context window, max output tokens ของแต่ละ model ไว้เลย ไม่ต้องเรียก API probe
- Claude: 200K context, 8K-32K output
- GPT-4o: 128K context, 16K output
- Gemini: 1M context, 65K output
- Z.AI/GLM: 8K-128K context, 4K output
**ผลที่ได้:** รู้ทันทีว่า model ไหนรับ context กี่ token, ใช้ตัดสินใจ routing และ budget

### 5. Duplicate Content Deduplication (ลบเนื้อหาซ้ำ)
**แหล่งที่มา:** token-optimizer-mcp, token-savior
**แนวคิด:** ตรวจจับประโยคซ้ำใน text แล้วลบออก, code block ไม่ถูกยุ่ง
- Sentence boundary detection
- Hash-based exact match (O(1))
- Code blocks preserved
**ผลที่ได้:** ลด token ที่เป็น repeated content โดยเฉพาะใน system prompt ที่มี boilerplate ซ้ำ

### 6. Token Budget Tracking (ติดตามงบ Token)
**แหล่งที่มา:** token-optimizer-mcp, token-optimizer, token-savior
**แนวคิด:** ติดตาม cumulative token usage ต่อ session เทียบกับ model context limit
- Green (<50%): ทำงานปกติ
- Yellow (50-75%): เริ่ม optimize whitespace + dedup
- Red (>75%): force truncation
**ผลที่ได้:** ป้องกัน context overflow, ลด error จาก upstream, ประหยัด token

---

## ผลประโยชน์สำหรับผู้ใช้ Gateway

| ประโยชน์ | รายละเอียด |
|----------|-----------|
| **ลดค่า API** | Whitespace optimization + dedup ลด input tokens ~5-15% ต่อ request |
| **ลด error** | Token estimation + model map ช่วยหลีกเลี่ยง context overflow error |
| **เร็วขึ้น** | เนื้อหาน้อยลง = upstream ตอบเร็วขึ้น |
| **เสถียรกว่า** | Budget tracking ป้องกัน session crash จาก context limit |
| **ใช้ได้ทุก provider** | Model map ครอบคลุม Anthropic, OpenAI, Google, Z.AI |

---

## Flow การทำงาน

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
    |           Secrets: API keys, private keys, JWT, bearer tokens (regex-based)
    |           PII: ชื่อ, อีเมล, เบอร์โทร (Microsoft Presidio NLP)
    |           แทนที่ด้วย placeholder -> ส่ง upstream แทนข้อมูลจริง
    |           Response จาก upstream -> restore ข้อมูลจริงกลับให้ client
    |
    v
[Whitespace Optimization] -- ลบ whitespace ซ้ำใน prose (preserve code)
    |
    v
[Deduplication] -- ลบประโยคซ้ำใน system prompt
    |
    v
[Budget Check] -- เช็คว่าใช้ไปกี่ % ของ context limit แล้ว
    |           Yellow (>50%): optimize เพิ่ม
    |           Red (>75%): force truncate
    v
[Upstream API] -- ส่ง request ที่ optimize แล้ว (secrets/PII ถูก mask)
    |
    v
[Unmask] -- restore secrets/PII ใน response กลับเป็นค่าจริง (non-stream)
    |       สำหรับ streaming: ใช้ StreamUnmasker แทนที่ placeholder ทีละ chunk
    |
    v
[Token Tracking] -- บันทึก actual token usage จาก response
```

### PasteGuard - Data Privacy Masking ละเอียด

**จุดประสงค์:** ป้องกันส่ง secrets และ PII ไปยัง upstream LLM provider โดยไม่ต้องไปแก้ prompt ของผู้ใช้

**Flow:**
1. **Extract** - แยก text spans จาก JSON payload (system prompt, messages, tool_result)
2. **Secret Detection** - scan ด้วย regex-based detector (OpenSSH key, PEM, AWS key, GitHub PAT, JWT, Bearer token)
3. **Secret Masking** - แทนที่ secrets ด้วย placeholder เช่น `[[API_KEY_AWS_1]]`
4. **PII Detection** - ส่ง text ที่ mask secrets แล้วไปยัง Microsoft Presidio (NLP-based)
5. **PII Masking** - แทนที่ PII (ชื่อ, อีเมล, เบอร์โทร) ด้วย placeholder
6. **Proxy** - ส่ง masked body ไป upstream (LLM ไม่เห็นข้อมูลจริง)
7. **Unmask** - restore ค่าจริงกลับใน response ก่อนส่งให้ client
   - Non-stream: `UnmaskResponse()` แทนที่ placeholder ทั้งหมด
   - Stream: `StreamUnmasker` แทนที่ placeholder ทีละ SSE chunk

**Config (env vars):**
- `PASTEGUARD_ENABLED` - เปิด/ปิด pipeline ทั้งหมด
- `PASTEGUARD_SECRETS_ENABLED` - เปิด/ปิด secret detection
- `PASTEGUARD_SECRET_ENTITIES` - ระบุ entity types (default: OpenSSH, PEM, AWS, GitHub, JWT, Bearer)
- `PASTEGUARD_MAX_SCAN_CHARS` - ขีดจำกัดตัวอักษรต่อ scan (default: 200K)
- `PASTEGUARD_PII_ENABLED` - เปิด/ปิด PII detection
- `PASTEGUARD_PRESIDIO_URL` - URL ของ Presidio analyzer service
- `PASTEGUARD_PII_SCORE_THRESHOLD` - confidence threshold (default: 0.7)
- `PASTEGUARD_PII_ENTITIES` - ระบุ PII entity types (default: PERSON, EMAIL, PHONE)
- `PASTEGUARD_PII_LANGUAGE` - ภาษาสำหรับ PII detection

**Monitoring:** Grafana dashboard `arl-pasteguard` แสดง metrics:
- `mask_duration_seconds` - เวลาในแต่ละ phase (secrets_detect, pii_detect, mask, unmask)
- `secrets_detected_total` - จำนวน secrets ที่ตรวจพบตาม type
- `pii_detected_total` - จำนวน PII ที่ตรวจพบตาม type
- `mask_requests_total` - จำนวน requests ที่ผ่าน pipeline

---

## เทคนิคที่ยังไม่ได้ Implement (Future)

| Priority | เทคนิค | ผลกระทบ | ความซับซ้อน |
|----------|--------|---------|-----------|
| 1 | DCP Chunking + Reorder | เพิ่ม prompt cache hit rate 90% | สูง |
| 2 | Knapsack Context Packing | ใส่ข้อมูลได้เยอะขึ้นใน budget เดียวกัน | สูง |
| 3 | LLM Summarization | ใช้ model เล็ก (Haiku) สรุป context | กลาง |
| 4 | PPM Prefetcher | ทำนาย request ถัดไป เตรียม cache | กลาง |
| 5 | Semantic Dedup | ตรวจ near-duplicate ด้วย embedding similarity | กลาง |
| 6 | Output Compression Tiers | ลด output tokens 30-75% ด้วย system prompt injection | ต่ำ |
| 7 | Session Warm Start | preload cache จาก session ที่คล้ายกัน | สูง |

---

## โครงสร้างไฟล์

- `api-gateway/tokenizer/optimizer.go` - Module หลัก: estimation, whitespace, dedup, truncation, model map, budget
- `api-gateway/tokenizer/optimizer_test.go` - Tests: 16 tests + 4 benchmarks
- `api-gateway/proxy/anthropic.go` - Integration: optimize system prompt + token estimation logging
- `docs/token-optimization.md` - Technical analysis (English)
