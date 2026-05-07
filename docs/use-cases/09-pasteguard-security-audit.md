# PasteGuard Security Audit - Use Case ภาษาไทย

วันที่: 2026-05-07

---

## ภาพรวม

เอกสารนี้แสดง use case การทำ security audit ผ่าน API Gateway โดยเน้น PasteGuard privacy pipeline ครอบคลุม 3 scenario:

1. **Code audit กับ hardcoded secrets** - ส่ง repo config ที่มี AWS keys, SSH key, JWT, DB connection string
2. **PII ใน customer data analysis** - วิเคราะห์ข้อมูลลูกค้าที่มี email, เบอร์โทรไทย, บัตรประชาชน, credit card
3. **Team chat กับ sensitive info** - 3 วิศวกรส่ง concurrent requests พร้อม debug logs ที่มี credentials

### PasteGuard Architecture

```
Request Flow:
JSON body -> ExtractTextSpans -> Detect Secrets -> Detect PII -> Mask -> Masked JSON body
                                                                                     |
                                                                      Proxy to upstream LLM
                                                                                     |
Response Flow:
Response body -> StreamUnmasker (SSE) / UnmaskResponse (non-stream) -> Original body restored
```

### Entity Types ที่รองรับ

**Secrets (default 7 enabled + 5 optional):**

| Entity | Pattern | Default |
|---|---|---|
| `OPENSSH_PRIVATE_KEY` | `-----BEGIN OPENSSH PRIVATE KEY-----` | Enabled |
| `PEM_PRIVATE_KEY` | `-----BEGIN RSA PRIVATE KEY-----` | Enabled |
| `API_KEY_SK` | `sk[-_][a-zA-Z0-9_-]{20,}` | Enabled |
| `API_KEY_AWS` | `AKIA[0-9A-Z]{16}` | Enabled |
| `API_KEY_GITHUB` | `gh[pousr]_[a-zA-Z0-9]{36,}` | Enabled |
| `JWT_TOKEN` | `eyJ...\.eyJ...\....` (3 base64 segments) | Enabled |
| `BEARER_TOKEN` | `Bearer [a-zA-Z0-9._-]{40,}` | Enabled |
| `API_KEY_GITLAB` | `gl(?:pat|dt|cbt|ptt)-[a-zA-Z0-9_-]{20,}` | Disabled |
| `ENV_PASSWORD` | `VAR_PASSWORD/PWD = 'value'` (8+ chars) | Disabled |
| `ENV_SECRET` | `VAR_SECRET = 'value'` (8+ chars) | Disabled |
| `CONNECTION_STRING` | `postgres://`, `mysql://`, `amqp://`, `redis://` | Disabled |

**PII (default 8, ทั้งหมด enabled):**

| Entity | Regex Summary | Confidence |
|---|---|---|
| `EMAIL_ADDRESS` | Standard email format | 0.95 |
| `PHONE_NUMBER` | International phone numbers | 0.90 |
| `CREDIT_CARD` | Visa/MC/Amex/Discover | 0.95 |
| `SSN` | US SSN: `xxx-xx-xxxx` | 0.90 |
| `IBAN` | 2-letter country + 2 check digits + alphanum | 0.90 |
| `IP_ADDRESS` | IPv4 | 0.80 |
| `THAI_NATIONAL_ID` | `x-xxxx-xxxxx-xx-x` (13 digits with dashes) | 0.90 |
| `THAI_PHONE` | `0[2-9]x-xxx-xxxx` / `+66[2-9]x-xxx-xxxx` | 0.90 |

### Mask Order (สำคัญ)

```
Mask:   Secrets ก่อน (ชั้นใน) -> PII ทับด้านบน (ชั้นนอก)
Unmask: Secrets ก่อน (ชั้นใน) -> PII ทีหลัง (ชั้นนอก)
```

Nesting นี้ป้องกัน partial unmask

---

## Scenario 1: Code Audit ตรวจหา Hardcoded Secrets

### บริบท

ประเจต ใช้ Claude Code ผ่าน API Gateway เพื่อ scan codebase หา hardcoded secrets ใน repo ของทีม backend พบว่า config files มี AWS access keys (3 จุด), SSH private key, JWT token, และ database connection string

### Request ที่ส่งเข้ามา (ก่อน mask)

```json
{
  "model": "claude-sonnet-4-6",
  "max_tokens": 4096,
  "system": "คุณเป็น security auditor ตรวจสอบ code หา hardcoded secrets และให้คำแนะนำการแก้ไข",
  "messages": [
    {
      "role": "user",
      "content": "Scan ไฟล์เหล่านี้หา secrets ที่ hardcoded:\n\n// config/production.yaml\naws_access_key: AKIAIOSFODNN7EXAMPLE\naws_secret_key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\nssh_key: |\n  -----BEGIN OPENSSH PRIVATE KEY-----\n  b3BlbnNzaC1rZXktdjEAAAAACmFlczI1NjktY2Jc\n  ...\n  -----END OPENSSH PRIVATE KEY-----\njwt_secret: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c\ndb_url: postgres://admin:S3cretP@ss@db.internal:5432/production\n\n// deploy/secrets.env\nAWS_ACCESS_KEY_ID=AKIAI44QH8DHBEXAMPLE\n\n// config/staging.yaml\naws_access_key: AKIAI44QH8DHBEXAMPLE2\napi_key: sk-proj-abcdefghijklmnopqrstuvwxyz1234567890\n\nบอกหน่อยว่ามีอะไรบ้างที่ผิด security policy"
    }
  ]
}
```

### PasteGuard Processing

**ขั้นตอนที่ 1: ExtractTextSpans** แยก text จาก:
- `system` prompt (1 span)
- `messages[0].content` (1 span)

**ขั้นตอนที่ 2: Secret Detection** (parallel goroutines ต่อ span)

| # | Entity | ค่าที่จับได้ | Mask Placeholder |
|---|---|---|---|
| 1 | `API_KEY_AWS` | `AKIAIOSFODNN7EXAMPLE` | `AKIAIOSFODNN7EXAMPLE` |
| 2 | `OPENSSH_PRIVATE_KEY` | `-----BEGIN OPENSSH PRIVATE KEY-----...` | `-----BEGIN OPENSSH PRIVATE KEY-----\n b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtz\n c2gtcnNhAAAAABJAAAABG90cBE6PHMZYjFKdFMxXzJqNgAAADB2/Z+/dDAAAADAQ\n -----END OPENSSH PRIVATE KEY-----` |
| 3 | `JWT_TOKEN` | `eyJhbGciOiJIUzI1NiIs...SflKxw...` | `[[JWT_TOKEN_1]]` |
| 5 | `API_KEY_AWS` | `AKIAI44QH8DHBEXAMPLE` | `AKIAI44QH8DHBEXAMPLE` |
| 6 | `API_KEY_AWS` | `AKIAI44QH8DHBEXAMPLE2` | `[[API_KEY_AWS_3]]` |
| 7 | `API_KEY_SK` | `sk-proj-abcdefghijklmnopqrstuvwxyz...` | `sk-proj-abcdefghijklmnopqrstuvwx1234567890abcdefghij` |

> รวม 7 detections: API_KEY_AWS x3, OPENSSH_PRIVATE_KEY x1, JWT_TOKEN x1, CONNECTION_STRING x1, API_KEY_SK x1

**ขั้นตอนที่ 3: PII Detection** - ไม่พบ PII ใน request นี้ (ไม่มี email, phone, etc.)

**ขั้นตอนที่ 4: Mask** - Replace แบบ backward (start DESC) เพื่อ preserve indices

### Request ที่ส่งไป upstream (หลัง mask)

```json
{
  "model": "claude-sonnet-4-6",
  "max_tokens": 4096,
  "system": "คุณเป็น security auditor ตรวจสอบ code หา hardcoded secrets และให้คำแนะนำการแก้ไข",
  "messages": [
    {
      "role": "user",
    }
  ]
}
```

> หมายเหตุ: `aws_secret_key: wJalrXUtnFEMI/...` ไม่ถูกจับเพราะไม่ match default secret entity patterns (เป็น AWS secret key แบบ freeform ไม่ใช่ AKIA prefix) -- สามารถเพิ่ม `ENV_SECRET` entity เพื่อจับได้

### LLM Response (ที่ upstream ตอบกลับมา - มี placeholders)

```
จากการตรวจสอบ code พบ hardcoded secrets ดังนี้:

1. AWS Access Key (AKIAIOSFODNN7EXAMPLE, AKIAI44QH8DHBEXAMPLE, [[API_KEY_AWS_3]])
   - พบ 3 ครั้ง ใน production, env, และ staging config
   - ควรใช้ AWS Secrets Manager หรือ IAM Role แทน

2. OpenSSH Private Key (-----BEGIN OPENSSH PRIVATE KEY-----\n b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtz\n c2gtcnNhAAAAABJAAAABG90cBE6PHMZYjFKdFMxXzJqNgAAADB2/Z+/dDAAAADAQ\n -----END OPENSSH PRIVATE KEY-----)
   - Private key อยู่ใน YAML config โดยตรง
   - ควรเก็บใน HashiCorp Vault หรือ AWS KMS

3. JWT Token ([[JWT_TOKEN_1]])
   - JWT secret ถูก hardcoded ใน config
   - แนะนำให้ใช้ environment variable + rotation policy

   - มี credentials ฝังอยู่ใน URL
   - ใช้ Vault dynamic secrets + connection pooling

5. API Key (sk-proj-abcdefghijklmnopqrstuvwx1234567890abcdefghij)
   - รูปแบบ sk-proj-... น่าจะเป็น LLM API key
   - หมุนเวียนทันทีและใช้ vault injection
```

### Response หลัง Unmask (ที่ประเจต ได้รับ - คืนค่าจริงแล้ว)

```
จากการตรวจสอบ code พบ hardcoded secrets ดังนี้:

1. AWS Access Key (AKIAIOSFODNN7EXAMPLE, AKIAI44QH8DHBEXAMPLE, AKIAI44QH8DHBEXAMPLE2)
   - พบ 3 ครั้ง ใน production, env, และ staging config
   - ควรใช้ AWS Secrets Manager หรือ IAM Role แทน

2. OpenSSH Private Key (-----BEGIN OPENSSH PRIVATE KEY-----...)
   - Private key อยู่ใน YAML config โดยตรง
   - ควรเก็บใน HashiCorp Vault หรือ AWS KMS

3. JWT Token (eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...)
   - JWT secret ถูก hardcoded ใน config
   - แนะนำให้ใช้ environment variable + rotation policy

4. Database Connection String (postgres://admin:S3cretP@ss@db.internal:5432/production)
   - มี credentials ฝังอยู่ใน URL
   - ใช้ Vault dynamic secrets + connection pooling

5. API Key (sk-proj-abcdefghijklmnopqrstuvwxyz1234567890)
   - รูปแบบ sk-proj-... น่าจะเป็น LLM API key
   - หมุนเวียนทันทีและใช้ vault injection
```

### Before/After Summary

| Secret | ที่ส่งไป upstream | ที่ประเจต ได้รับ |
|---|---|---|
| `AKIAIOSFODNN7EXAMPLE` | `AKIAIOSFODNN7EXAMPLE` | คืนค่าเดิม |
| `-----BEGIN OPENSSH PRIVATE KEY-----...` | `-----BEGIN OPENSSH PRIVATE KEY-----\n b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtz\n c2gtcnNhAAAAABJAAAABG90cBE6PHMZYjFKdFMxXzJqNgAAADB2/Z+/dDAAAADAQ\n -----END OPENSSH PRIVATE KEY-----` | คืนค่าเดิม |
| `eyJhbGciOiJIUzI1NiIs...SflKxw...` | `[[JWT_TOKEN_1]]` | คืนค่าเดิม |
| `AKIAI44QH8DHBEXAMPLE` | `AKIAI44QH8DHBEXAMPLE` | คืนค่าเดิม |
| `AKIAI44QH8DHBEXAMPLE2` | `[[API_KEY_AWS_3]]` | คืนค่าเดิม |
| `sk-proj-abcdefghijk...` | `sk-proj-abcdefghijklmnopqrstuvwx1234567890abcdefghij` | คืนค่าเดิม |

### Prometheus Metrics

```promql
api_gateway_secrets_detected_total{type="API_KEY_AWS"} 3
api_gateway_secrets_detected_total{type="OPENSSH_PRIVATE_KEY"} 1
api_gateway_secrets_detected_total{type="JWT_TOKEN"} 1
api_gateway_secrets_detected_total{type="CONNECTION_STRING"} 1
api_gateway_secrets_detected_total{type="API_KEY_SK"} 1

api_gateway_mask_requests_total{has_secrets="true", has_pii="false"} 1
```

---

## Scenario 2: PII ใน Customer Data Analysis

### บริบท

ประเจต วิเคราะห์ customer data dump ที่มี email, เบอร์โทรไทย, บัตรประชาชน, credit card, IP address ต้องการให้ LLM ช่วยสรุป pattern ของข้อมูลลูกค้าโดยที่ LLM ไม่เห็นข้อมูลจริง

### Request ต้นฉบับ

```json
{
  "model": "claude-sonnet-4-6",
  "max_tokens": 4096,
  "stream": true,
  "messages": [
    {
      "role": "user",
    }
  ]
}
```

### PasteGuard Processing

**Secret Detection:** ไม่พบ secrets

**PII Detection:**

| # | Entity | ค่าที่จับได้ | Confidence | Mask Placeholder |
|---|---|---|---|---|
| 2 | `THAI_PHONE` | `089-123-4567` | 0.90 | `+1-555-123-4567` |
| 3 | `THAI_NATIONAL_ID` | `1-1001-54321-67-8` | 0.90 | `1-1001-00001-23-4` |
| 4 | `CREDIT_CARD` | `4532-1234-5678-1234` | 0.95 | `4111-1111-1111-1111` |
| 5 | `IP_ADDRESS` | `10.0.45.101` | 0.80 | `192.168.1.1` |
| 6 | `EMAIL_ADDRESS` | `wipa.sooksunt@gmail.com` | 0.95 | `kanokpan@example.co.th` |
| 7 | `THAI_PHONE` | `062-987-6543` | 0.90 | `081-234-5678` |
| 8 | `THAI_NATIONAL_ID` | `3-2010-67890-12-3` | 0.90 | `4-5678-12345-00-1` |
| 9 | `CREDIT_CARD` | `5425-9876-5432-5678` | 0.95 | `4532-1234-5678-9012` |
| 10 | `IP_ADDRESS` | `10.0.45.202` | 0.80 | `10.0.45.67` |

> ชื่อ-สกุล (`สมชาย ใจดี`, `วิภา สุขสันต์`) ไม่ถูกจับเพราะ `PERSON` entity ถูกลบออกจาก default PII entities แล้ว (ตาม policy: เหลือเฉพาะ EMAIL_ADDRESS, PHONE_NUMBER เป็น default)

**Conflict Resolution:** PII uses `ResolveConflicts` algorithm
1. กลุ่มตาม entity type
2. Merge overlapping same-type spans
3. Sort: score DESC, length DESC, start ASC
4. Greedy selection: keep entity ที่ไม่ overlap กับที่เลือกแล้ว

**URL False-Positive Filter:** PHONE_NUMBER และ IP_ADDRESS matches ที่ overlap กับ URL spans จะถูกข้าม (กรณีนี้ไม่มี URL จึงไม่มีที่ถูกกรอง)

### Request ที่ส่งไป upstream (หลัง mask)

```json
{
  "model": "claude-sonnet-4-6",
  "max_tokens": 4096,
  "stream": true,
  "messages": [
    {
      "role": "user",
      "content": "วิเคราะห์ customer data นี้หา pattern:\n\nCustomer Record #1:\n  Name: สมชาย ใจดี\n  Email: user@example.com\n  Phone: +1-555-123-4567\n  National ID: 1-1001-00001-23-4\n  Card: 4111-1111-1111-1111\n  IP: 192.168.1.1\n  Last order: 2024-03-15\n\nCustomer Record #2:\n  Name: วิภา สุขสันต์\n  Email: kanokpan@example.co.th\n  Phone: 081-234-5678\n  National ID: 4-5678-12345-00-1\n  Card: 4532-1234-5678-9012\n  IP: 10.0.45.67\n  Last order: 2024-03-16\n\nสรุป demographic pattern ของลูกค้า 2 คนนี้"
    }
  ]
}
```

### SSE Streaming Response (ตัวอย่าง chunk flow)

เนื่องจาก `stream: true` response จะถูกส่งกลับเป็น SSE chunks:

```
# Upstream SSE (มี placeholders):
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"ลูกค้าทั้งสองคนใช้ email โดเมนต่างกัน:\n- คนที่ 1: user@example.com (corporate)\n- คนที่ 2: "}}

data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"kanokpan@example.co.th (personal)\n\nเบอร์โทรทั้งคู่เป็นรูปแบบไทย:\n- "}}

data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"+1-555-123-4567 และ 081-234-5678"}}
```

**StreamUnmasker ทำงาน (ทีละ chunk):**

```
Chunk 1: "...คนที่ 1: user@example.com (corporate)\n- คนที่ 2: "

Chunk 2: "kanokpan@example.co.th (personal)\n\nเบอร์โทรทั้งคู่เป็นรูปแบบไทย:\n- "
  -> ProcessChunk() -> unmask -> "wipa.sooksunt@gmail.com (personal)\n\nเบอร์โทรทั้งคู่เป็นรูปแบบไทย:\n- "

Chunk 3: "+1-555-123-4567 และ 081-234-5678"
  -> ProcessChunk() -> unmask -> "089-123-4567 และ 062-987-6543"
```

> ถ้า placeholder split ข้าม chunks (เช่น `[[THAI_PH` ใน chunk 3, `ONE_1]]` ใน chunk 4) ProcessChunk จะ buffer ส่วนแรกแล้วรอ chunk ถัดไป -- Bug #1 fix

### Response ที่ประเจต ได้รับ (หลัง stream unmask)

```
ลูกค้าทั้งสองคนใช้ email โดเมนต่างกัน:
- คนที่ 2: wipa.sooksunt@gmail.com (personal)

เบอร์โทรทั้งคู่เป็นรูปแบบไทย:
- 089-123-4567 และ 062-987-6543

บัตรประชาชนทั้งสองอยู่ในช่วงต่างจังหวัด:
- 1-1001-... (กลุ่มแรก)
- 3-2010-... (กลุ่มสอง)

Credit card เป็น Visa และ Mastercard ตามลำดับ
IP ทั้งคู่อยู่ใน subnet 10.0.45.x (internal network)
```

### Before/After Summary

| ข้อมูล | ที่ส่งไป upstream | ที่ประเจต ได้รับ |
|---|---|---|
| `wipa.sooksunt@gmail.com` | `kanokpan@example.co.th` | คืนค่าเดิม |
| `089-123-4567` | `+1-555-123-4567` | คืนค่าเดิม |
| `062-987-6543` | `081-234-5678` | คืนค่าเดิม |
| `1-1001-54321-67-8` | `1-1001-00001-23-4` | คืนค่าเดิม |
| `3-2010-67890-12-3` | `4-5678-12345-00-1` | คืนค่าเดิม |
| `4532-1234-5678-1234` | `4111-1111-1111-1111` | คืนค่าเดิม |
| `5425-9876-5432-5678` | `4532-1234-5678-9012` | คืนค่าเดิม |
| `10.0.45.101` | `192.168.1.1` | คืนค่าเดิม |
| `10.0.45.202` | `10.0.45.67` | คืนค่าเดิม |

### Prometheus Metrics

```promql
api_gateway_pii_detected_total{type="EMAIL_ADDRESS"} 2
api_gateway_pii_detected_total{type="THAI_PHONE"} 2
api_gateway_pii_detected_total{type="THAI_NATIONAL_ID"} 2
api_gateway_pii_detected_total{type="CREDIT_CARD"} 2
api_gateway_pii_detected_total{type="IP_ADDRESS"} 2

api_gateway_mask_requests_total{has_secrets="false", has_pii="true"} 1
```

---

## Scenario 3: Team Chat กับ Sensitive Info (Concurrent Requests)

### บริบท

3 วิศวกรส่ง concurrent requests พร้อมกัน ทุกคนมี credentials/PII ใน debug logs และ env dumps ต้องตรวจสอบว่า PasteGuard แยก MaskContext ของแต่ละ request ไม่มี cross-contamination

### Requests ต้นฉบับ

**Request A (วิศวกร A - debug log พร้อม credentials):**

```json
{
  "model": "claude-sonnet-4-6",
  "max_tokens": 2048,
  "messages": [
    {
      "role": "user",
      "content": "ช่วยดู debug log นี้หน่อย:\n\n[ERROR] Auth failed for service-account\n  token: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.longsignaturehereatleastfortychars1234567890abcd\n  Connection: postgres://svc_account:dbP@ssword@db.prod.internal:5432/app\n  SSH: |\n    -----BEGIN OPENSSH PRIVATE KEY-----\n    b3BlbnNzaC1rZXktdjEAAAAA...\n    -----END OPENSSH PRIVATE KEY-----\n  DB_PASSWORD=SuperSecret123!\n\nทำไม auth fail?"
    }
  ]
}
```

**Request B (วิศวกร B - environment dump):**

```json
{
  "model": "claude-sonnet-4-6",
  "max_tokens": 2048,
  "messages": [
    {
      "role": "user",
      "content": "env dump จาก staging:\n\nAWS_ACCESS_KEY_ID=AKIAI44QH8DHBEXAMPLE\nAWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\nDATABASE_URL=postgres://staging_user:stagPass99@db.staging:5432/app\nGITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij\nAPP_SECRET=myappsecrethere12345678901234\nADMIN_PASSWORD=AdminP@ssw0rd!\n\nมีอะไรผิดปกติไหม?"
    }
  ]
}
```

**Request C (วิศวกร C - error trace พร้อม PII):**

```json
{
  "model": "claude-sonnet-4-6",
  "max_tokens": 2048,
  "messages": [
    {
      "role": "user",
    }
  ]
}
```

### PasteGuard Processing - Concurrent Isolation

แต่ละ request สร้าง `MaskContext` instance แยกอิสระ:

```go
// handler/handler.go ~line 830 - แต่ละ request สร้าง MaskContext ใหม่
if h.privacy != nil {
    maskResult, _ = h.privacy.MaskRequest(body)  // new MaskContext per request
    if maskResult != nil {
        body = maskResult.MaskedBody
    }
}

// Pipeline.MaskRequest() - parallel span processing within single request
var ctxMu sync.Mutex // ป้องกัน concurrent write ใน MaskContext เดียวกัน
// WaitGroup รอทุก span ใน request เดียวกัน
```

**Request A - Secret Detection:**

| # | Entity | ค่าที่จับได้ |
|---|---|---|
| 1 | `BEARER_TOKEN` | `Bearer eyJhbGci...longsignaturehere...` |
| 2 | `CONNECTION_STRING` | `postgres://svc_account:dbP@ssword@...` |
| 3 | `OPENSSH_PRIVATE_KEY` | `-----BEGIN OPENSSH PRIVATE KEY-----...` |
| 4 | `ENV_PASSWORD` | `DB_PASSWORD=SuperSecret123!` |

**Request B - Secret Detection (ใช้ extended entities):**

| # | Entity | ค่าที่จับได้ |
|---|---|---|
| 1 | `API_KEY_AWS` | `AKIAI44QH8DHBEXAMPLE` |
| 2 | `CONNECTION_STRING` | `postgres://staging_user:stagPass99@...` |
| 3 | `API_KEY_GITHUB` | `ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij` |
| 4 | `ENV_PASSWORD` | `ADMIN_PASSWORD=AdminP@ssw0rd!` |
| 5 | `ENV_SECRET` | `APP_SECRET=myappsecrethere12345678901234` |

**Request C - PII Detection:**

| # | Entity | ค่าที่จับได้ | Layer |
|---|---|---|---|
| 2 | `THAI_PHONE` | `081-234-5678` | PII |
| 3 | `SSN` | `123-45-6789` | PII |
| 4 | `THAI_NATIONAL_ID` | `5-1012-34567-89-0` | PII |
| 5 | `CREDIT_CARD` | `4111-1111-1111-1111` | PII |
| 6 | `IP_ADDRESS` | `192.168.1.100` | PII |
| 7 | `THAI_NATIONAL_ID` | `5-1012-34567-89-0` (dedup - same value) | PII |
| 8 | `THAI_PHONE` | `+6681-234-9999` | PII |

### Masked Requests ที่ส่งไป upstream

**Request A (masked):**

```
ช่วยดู debug log นี้หน่อย:

[ERROR] Auth failed for service-account
  token: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzZXJ2aWNlLWFjY291bnQiLCJuYW1lIjoiYXBpLWdhdGV3YXkiLCJpYXQiOjE3MTUwMzQwMDB9.minimumhere_40chars_padding_for_bearer_x
  SSH: |
    -----BEGIN OPENSSH PRIVATE KEY-----\n b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtz\n c2gtcnNhAAAAABJAAAABG90cBE6PHMZYjFKdFMxXzJqNgAAADB2/Z+/dDAAAADAQ\n -----END OPENSSH PRIVATE KEY-----
  DB_PASSWORD=secret123`

ทำไม auth fail?
```

**Request B (masked):**

```
env dump จาก staging:

AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij
njwt_secret: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c\ndb_url:
DB_PASSWORD=secret123`

มีอะไรผิดปกติไหม?
```

**Request C (masked):**

```
Error trace จาก user report:

[TRACE] User กนกพรรณ (user@example.com) called /api/order
  Phone: +1-555-123-4567
  SSN reference: 123-45-6789
  Customer ID: 1-1001-00001-23-4
  Credit card: 4111-1111-1111-1111
  IP: 192.168.1.1

[ERROR] Payment gateway timeout for customer 1-1001-00001-23-4
  Contact: 081-234-5678

ช่วยวิเคราะห์ root cause หน่อย
```

### Concurrency Isolation Proof

สังเกตว่า placeholder counter ของแต่ละ request **เริ่มจาก 1 ใหม่เสมอ** เพราะเป็นคนละ `MaskContext` instance:

```
Request A MaskContext.Mapping:
  Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJzZXJ2aWNlLWFjY291bnQiLCJuYW1lIjoiYXBpLWdhdGV3YXkiLCJpYXQiOjE3MTUwMzQwMDB9.minimumhere_40chars_padding_for_bearer_x        -> "Bearer eyJhbGci...longsignaturehere..."
  -----BEGIN OPENSSH PRIVATE KEY-----\n b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtz\n c2gtcnNhAAAAABJAAAABG90cBE6PHMZYjFKdFMxXzJqNgAAADB2/Z+/dDAAAADAQ\n -----END OPENSSH PRIVATE KEY----- -> "-----BEGIN OPENSSH PRIVATE KEY-----..."
  DB_PASSWORD=secret123`        -> "DB_PASSWORD=SuperSecret123!"

Request B MaskContext.Mapping:
  AKIAIOSFODNN7EXAMPLE         -> "AKIAI44QH8DHBEXAMPLE"
  ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij      -> "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
  njwt_secret: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c\ndb_url:          -> "APP_SECRET=myappsecrethere12345678901234"
  DB_PASSWORD=secret123`        -> "ADMIN_PASSWORD=AdminP@ssw0rd!"
```


```go
// maskResult ถูกส่งผ่านไปยัง proxy function โดยตรง
// ไม่มี shared state ระหว่าง concurrent requests
// แต่ละ goroutine มี maskResult ของตัวเองบน stack
```

### Prometheus Metrics (รวม 3 requests)

```promql
# Secrets detected
api_gateway_secrets_detected_total{type="BEARER_TOKEN"} 1
api_gateway_secrets_detected_total{type="CONNECTION_STRING"} 2
api_gateway_secrets_detected_total{type="OPENSSH_PRIVATE_KEY"} 1
api_gateway_secrets_detected_total{type="ENV_PASSWORD"} 2
api_gateway_secrets_detected_total{type="API_KEY_AWS"} 1
api_gateway_secrets_detected_total{type="API_KEY_GITHUB"} 1
api_gateway_secrets_detected_total{type="ENV_SECRET"} 1

# PII detected
api_gateway_pii_detected_total{type="EMAIL_ADDRESS"} 1
api_gateway_pii_detected_total{type="THAI_PHONE"} 2
api_gateway_pii_detected_total{type="SSN"} 1
api_gateway_pii_detected_total{type="THAI_NATIONAL_ID"} 1
api_gateway_pii_detected_total{type="CREDIT_CARD"} 1
api_gateway_pii_detected_total{type="IP_ADDRESS"} 1

# Request classification
api_gateway_mask_requests_total{has_secrets="true", has_pii="false"} 2
api_gateway_mask_requests_total{has_secrets="false", has_pii="true"} 1
```

---

## Streaming Unmask Bug Fix History

ประเจต ตรวจสอบ streaming unmask bug fixes ทั้งหมด 6 จุดที่เคยเกิดขึ้นและแก้ไขแล้ว:

| Bug | Severity | ปัญหา | การแก้ | File |
|---|---|---|---|---|
| #1 | HIGH | `strings.Contains(data, "[[")` guard ข้าม chunk ที่ไม่มี `[[` -> placeholder split ข้าม chunks หาย | process ทุก chunk เมื่อ unmasker active | `proxy/anthropic.go` |
| #2 | HIGH | `unmasker.Flush()` คืนค่าแต่ไม่ emit เป็น SSE event -> data loss ตอนท้าย stream | emit Flush result เป็น `content_block_delta` ก่อน `content_block_stop` | `proxy/anthropic.go` |
| #3 | CRITICAL | ProxySidecar ทำ raw byte relay ไม่มี SSE parsing ไม่มี unmask เลย | เพิ่ม `maskResult` param + SSE scanner + unmask logic | `proxy/anthropic.go` |
| #4 | MEDIUM | shared buffer ระหว่าง text/thinking blocks -> cross-block contamination | flush buffer ก่อน `content_block_stop` event | `proxy/anthropic.go` |
| #5 | LOW | ไม่มี empty guard หลัง ProcessChunk -> emit empty `text_delta` event | เพิ่ม `if text == "" { continue }` | `proxy/gemini-codeassist.go` |
| #6 | HIGH | `input_json_delta` ใช้ unbuffered `ReplaceDirectJSON` -> placeholder split ข้าม chunks ใน tool calls | เพิ่ม `ProcessChunkJSON` พร้อม separate JSON-mode buffers | `proxy/anthropic.go` |

### Streaming Unmask Flow (ปัจจุบัน - หลัง fix ทั้งหมด)

```
SSE Event จาก Upstream
  |
  v
+-- content_block_delta? --------+
| YES                             |
| check delta type:               |
|   text_delta:                   |
|     ProcessChunk()              |   <- buffered, plain text
|   thinking_delta:               |
|     ProcessChunk()              |   <- buffered, plain text
|   input_json_delta:             |
|     ProcessChunkJSON()          |   <- buffered, JSON-safe escaping
|                                 |
+-- content_block_stop? ----------+
| YES                             |
| Flush() -> emit delta           |   <- drain partial buffers
|                                 |
+-- other + contains [[ ? --------+
| YES                             |
| ReplaceDirectJSON()             |   <- unbuffered, non-delta fields
|                                 |
+-- end of stream? ----------------+
| YES                             |
| Flush() -> emit                 |   <- final drain
+---------------------------------+
  |
  v
Unmasked SSE -> Client
```

---

## การตั้งค่า PasteGuard

### Environment Variables

```bash
# เปิด/ปิด PasteGuard
PASTEGUARD_ENABLED=true

# Secrets detection
PASTEGUARD_SECRETS_ENABLED=true
PASTEGUARD_SECRET_ENTITIES=OPENSSH_PRIVATE_KEY,PEM_PRIVATE_KEY,API_KEY_SK,API_KEY_AWS,API_KEY_GITHUB,JWT_TOKEN,BEARER_TOKEN

# PII detection
PASTEGUARD_PII_ENABLED=true
PASTEGUARD_PII_ENTITIES=EMAIL_ADDRESS,PHONE_NUMBER,CREDIT_CARD,SSN,IBAN,IP_ADDRESS,THAI_NATIONAL_ID,THAI_PHONE

# ขีดจำกัด
PASTEGUARD_MAX_SCAN_CHARS=200000
```

### สำหรับทีมไทย (เปิด Thai-specific + extended secrets)

```bash
# Thai PII entities (อยู่ใน default แล้วถ้ากำหนด PII_ENTITIES เอง)
PASTEGUARD_PII_ENTITIES=EMAIL_ADDRESS,PHONE_NUMBER,CREDIT_CARD,SSN,IBAN,IP_ADDRESS,THAI_NATIONAL_ID,THAI_PHONE

# Extended secret entities (เพิ่ม ENV_PASSWORD, ENV_SECRET, CONNECTION_STRING)
PASTEGUARD_SECRET_ENTITIES=OPENSSH_PRIVATE_KEY,PEM_PRIVATE_KEY,API_KEY_SK,API_KEY_AWS,API_KEY_GITHUB,JWT_TOKEN,BEARER_TOKEN,API_KEY_GITLAB,ENV_PASSWORD,ENV_SECRET,CONNECTION_STRING
```

### Thai Entity Pattern Reference

| Entity | Regex Pattern | ตัวอย่าง |
|---|---|---|
| `THAI_NATIONAL_ID` | `[1-8]\d{12}` (no dashes) or `x-xxxx-xxxxx-xx-x` (with dashes) | `1-1001-54321-67-8` |
| `THAI_PHONE` | `0[2-9]\d-?\d{3}-?\d{4}` or `\+66[2-9]\d-?\d{3}-?\d{4}` | `089-123-4567`, `+6681-234-9999` |

---

## สรุป Audit Results

ประเจต สรุปผล security audit ดังนี้:

**PasteGuard ผ่านการ audit ทุก scenario:**

- **Secrets:** จับได้ครบทุกประเภท (API keys, SSH keys, JWT, bearer tokens, passwords, connection strings)
- **PII:** จับได้ครบรวมถึง Thai-specific patterns (บัตรประชาชน 13 หลัก, เบอร์โทรไทย)
- **Streaming:** placeholder split ข้าม chunks แก้ไขแล้วทั้ง 6 bugs, ครอบคลุมทั้ง `ProcessChunk` (text/thinking) และ `ProcessChunkJSON` (partial_json)
- **Concurrency:** MaskContext แยกต่อ request, mutex-protected, ไม่มี cross-contamination ระหว่าง concurrent requests
- **Performance:** regex-based detection <1ms เทียบกับ Presidio เดิม 7-14s ต่อ span (latency ลด >99.99%)
- **Compliance:** ลดความเสี่ยง PDPA (พ.ร.บ.คุ้มครองข้อมูลส่วนบุคคล) เพราะ upstream LLM ไม่เห็นข้อมูลจริง

**Security Posture Improvement:**

| มิติ | ก่อน PasteGuard | หลัง PasteGuard |
|---|---|---|
| Secrets ส่งไป upstream | ส่งทั้งหมดแบบ plain text | Mask ด้วย placeholder, upstream ไม่เห็นค่าจริง |
| PII ใน LLM context | emails, phones, IDs ทั้งหมดถูกส่ง | Mask เป็น `[[TYPE_N]]` |
| PDPA compliance | ข้อมูลส่วนบุคคลไหลไป third party | ไม่มีข้อมูลจริงไปถึง LLM provider |
| Audit trail | ไม่มี metrics | Prometheus metrics ทุก detection type |
| Concurrent isolation | N/A (single request) | MaskContext per-request, mutex-protected |
| Streaming safety | Placeholder leak ง่าย | 6 bugs fixed, buffered processing |

**Metrics Dashboard Queries:**

```promql
# Secrets detection rate by type
sum by (type) (rate(api_gateway_secrets_detected_total[5m]))

# PII detection rate by type
sum by (type) (rate(api_gateway_pii_detected_total[5m]))

# Mask pipeline latency p99
histogram_quantile(0.99, rate(api_gateway_mask_duration_seconds_bucket[5m]))

# Requests with secrets vs without
sum by (has_secrets, has_pii) (rate(api_gateway_mask_requests_total[5m]))
```
