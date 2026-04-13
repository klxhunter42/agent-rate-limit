# Provider Setup Guide / คู่มือตั้งค่า Provider

ระบบรองรับ 5 providers สำหรับ async queue path (POST /v1/chat/completions -> Dragonfly -> Python Worker).
สำหรับ sync proxy path (POST /v1/messages -> Go Gateway) ใช้ `UPSTREAM_URL` ชี้ไปยัง provider ตัวใดตัวหนึ่งโดยตรง.

Provider บางตัว (Claude, Gemini) มี subscription plan หรือ free tier ที่ไม่คิดตาม token -- แต่ระบบยัง track token usage ผ่าน Prometheus เพื่อ monitoring เหมือนเดิม.

## Architecture Recap

```
Sync path:  Client -> POST /v1/messages -> Go Gateway -> UPSTREAM_URL (single provider)
Async path: Client -> POST /v1/chat/completions -> Dragonfly -> Worker -> multi-provider dispatch
```

Provider fallback order: `glm` -> `openai` -> `anthropic` -> `gemini` -> `openrouter`

---

## Quick Start: Single Provider

เริ่มจาก provider เดียวก่อน. ใส่ API key ใน `.env` แล้วระบบจะตั้งค่าอัตโนมัติ.

```bash
# ใช้แค่ GLM (default)
GLM_API_KEYS=your-glm-api-key

# หรือใช้แค่ Gemini
GEMINI_API_KEYS=your-gemini-api-key

# หรือใช้แค่ OpenAI
OPENAI_API_KEYS=your-openai-api-key
```

ระบบตรวจจับ provider ที่มี key อัตโนมัติจาก `available_providers` ใน worker config.
Provider ที่ไม่มี key จะถูกข้ามใน fallback chain.

---

## Multi-Provider with Fallback

ใส่ key หลาย provider เพื่อใช้ fallback เมื่อ provider หลักล้มเหลวหรือโดน rate limit.

```bash
# Primary: GLM
GLM_API_KEYS=glm-key-1,glm-key-2

# Fallback #1: OpenAI
OPENAI_API_KEYS=sk-openai-key-1,sk-openai-key-2

# Fallback #2: Anthropic
ANTHROPIC_API_KEYS=sk-ant-key-1

# Fallback #3: Gemini
GEMINI_API_KEYS=gemini-key-1

# Fallback #4: OpenRouter
OPENROUTER_API_KEYS=or-key-1
```

Fallback logic ใน worker (`worker.py`):
1. ลอง provider ที่ request ระบุก่อน
2. ถ้า fail (429, 5xx, timeout) -> วนตาม `PROVIDER_FALLBACK_ORDER`
3. Key ที่โดน 429 จะเข้า cooldown 60s แล้ว switch ไป key อื่นใน pool เดียวกัน
4. ถ้าทุก provider ใน pool โดน cooldown -> รอจน cooldown หมด

---

## Passthrough vs Key Pool Mode

ระบบมี 2 โหมดสำหรับ sync proxy path:

### Key Pool Mode (default for sync)

Gateway จัดการ key เองจาก pool. Client ไม่ต้องส่ง API key.

```bash
# Go Gateway sync path
UPSTREAM_URL=https://api.z.ai/api/anthropic
UPSTREAM_API_KEYS=key1,key2,key3
UPSTREAM_RPM_LIMIT=40
```

Key pool เลือก key ที่มี RPM budget เหลือมากที่สุด (weighted round-robin).
ถ้า key โดน 429 จะเข้า cooldown 10s.

### Passthrough Mode

Client ส่ง API key เองผ่าน header. Gateway ส่งต่อไปยัง upstream โดยไม่เปลี่ยนแปลง.

```bash
# ไม่ตั้ง UPSTREAM_API_KEYS จะเป็น passthrough อัตโนมัติ
UPSTREAM_URL=https://api.z.ai/api/anthropic
UPSTREAM_API_KEYS=
```

Client ต้องส่ง header:
```
x-api-key: <your-key>
# หรือ
Authorization: Bearer <your-key>
```

สำหรับ async path, worker ใช้ key pool เสมอจาก env vars ของแต่ละ provider.

---

## 1. GLM / Z.ai (Default Provider)

API ของ Z.ai เข้ากันได้กับ Anthropic SDK (Anthropic-compatible endpoint).

### Setup

```bash
# Endpoint (default value, ไม่ต้องเปลี่ยนถ้าใช้ Z.ai)
GLM_ENDPOINT=https://api.z.ai/api/anthropic

# API keys (คั่นด้วยคอมม่าสำหรับหลาย key)
GLM_API_KEYS=your-zai-key-1,your-zai-key-2
```

### How to get API key

1. ไปที่ [z.ai](https://z.ai) แล้วสมัคร/ล็อกอิน
2. ไปที่หน้า API Keys หรือ Dashboard
3. สร้าง API key ใหม่
4. คัดลอก key มาใส่ใน `.env`

### Auth mechanism

- Header: `x-api-key`
- SDK: `anthropic` Python SDK กับ `base_url` เปลี่ยนเป็น Z.ai endpoint
- Default model: `glm-5`

### Sync proxy path

```bash
UPSTREAM_URL=https://api.z.ai/api/anthropic
UPSTREAM_API_KEYS=your-zai-key-1,your-zai-key-2
```

### Token tracking

Prometheus metrics:
- `api_gateway_token_input_total{model="glm-5"}` (sync path)
- `ai_worker_token_input_total{provider="glm",model="glm-5"}` (async path)

---

## 2. Anthropic (Claude)

Direct Anthropic API. เหมาะสำหรับผู้ที่มี Claude subscription (Pro, Team, Enterprise) หรือ API billing account.

### Setup

```bash
# API keys
ANTHROPIC_API_KEYS=sk-ant-api03-xxxx,sk-ant-api03-yyyy
```

### How to get API key

1. ไปที่ [console.anthropic.com](https://console.anthropic.com)
2. ล็อกอินด้วย account ที่มี API access
3. ไปที่ Settings -> API Keys
4. สร้าง key ใหม่

### Subscription vs API billing

Claude มี 2 รูปแบบ:
- **Claude.ai subscription** (Pro/Team): จ่ายรายเดือน, ใช้ได้ผ่าน web/UI. API key สำหรับ programmatic access มี usage limit ตาม plan
- **API billing**: จ่ายตาม token usage ผ่าน Anthropic API console

API key จากทั้ง 2 ทางใช้ได้กับระบบนี้เหมือนกัน. Token tracking เป็นประโยชน์สำหรับ monitoring ไม่ว่าจะจ่ายแบบไหน.

### Auth mechanism

- Header: `x-api-key`
- SDK: `anthropic` Python SDK (direct, no base_url override)
- Default model: `claude-sonnet-4-20250514`
- Required header: `anthropic-version: 2023-06-01` (set by SDK automatically)

### Note

Claude API keys เริ่มต้นด้วย `sk-ant-api`.

---

## 3. OpenAI

OpenAI API สำหรับ GPT series models.

### Setup

```bash
# API keys
OPENAI_API_KEYS=sk-proj-xxxx,sk-proj-yyyy
```

### How to get API key

1. ไปที่ [platform.openai.com](https://platform.openai.com)
2. ล็อกอิน หรือสมัคร
3. ไปที่ API Keys -> Create new secret key
4. คัดลอก key (จะแสดงแค่ครั้งเดียว)

### Auth mechanism

- Header: `Authorization: Bearer <key>`
- SDK: `openai` Python SDK (direct, no base_url override)
- Default model: `gpt-4o`

### Note

OpenAI คิดเงินตาม token usage เท่านั้น ไม่มี subscription plan สำหรับ API access.

---

## 4. Google Gemini

Google Generative AI API. มี free tier ที่ใช้ได้จริง.

### Setup

```bash
# API keys
GEMINI_API_KEYS=AIzaSy-xxxx,AIzaSy-yyyy
```

### How to get API key

1. ไปที่ [aistudio.google.com](https://aistudio.google.com)
2. ล็อกอินด้วย Google account
3. ไปที่ Get API Key (หรือ Settings -> API Keys)
4. สร้าง key สำหรับ Google AI Studio

### Free tier

Gemini มี free tier ที่มี rate limit:
- **Free**: 15 RPM, 1 million TPM, 1500 RPD (requests per day)
- **Paid**: สูงกว่า ตาม model และ tier

สำหรับ workload เล็กหรือ dev/testing, free tier อาจเพียงพอ.
ใช้ `PROVIDER_RPM_LIMITS=gemini:15` เพื่อป้องกัน 429.

### Auth mechanism

- ไม่ใช้ HTTP header แบบอื่น -- ใช้ `genai.configure(api_key=key)` ใน code
- SDK: `google-generativeai` Python SDK
- Default model: `gemini-2.0-flash`

### Note

Gemini API keys เริ่มต้นด้วย `AIzaSy`. ระบบ configure key per-request เพื่อรองรับ multi-key rotation.

---

## 5. OpenRouter

OpenRouter เป็น gateway ที่รวบรวมหลาย model จากหลาย provider ไว้ใน API เดียว.
ใช้ SDK แบบ OpenAI-compatible.

### Setup

```bash
# API keys
OPENROUTER_API_KEYS=sk-or-v1-xxxx,sk-or-v1-yyyy
```

### How to get API key

1. ไปที่ [openrouter.ai](https://openrouter.ai)
2. ล็อกอิน (รองรับ Google, GitHub OAuth)
3. ไปที่ Keys -> Create Key
4. คัดลอก key

### Auth mechanism

- Header: `Authorization: Bearer <key>`
- SDK: `openai` Python SDK กับ `base_url=https://openrouter.ai/api/v1`
- Default model: `openai/gpt-4o`
- Model format: `provider/model-name` (เช่น `anthropic/claude-sonnet-4`, `google/gemini-2.0-flash`)

### Pricing

OpenRouter คิดเงินตาม token usage ผ่าน credit system. ราคาแตกต่างกันตาม model.
สามารถดูราคาได้ที่ [openrouter.ai/models](https://openrouter.ai/models).

---

## Token Monitoring without Billing Concerns

ถ้าใช้ provider ที่ไม่คิดตาม token (Claude subscription, Gemini free tier), token tracking ยังมีประโยชน์:

### ดูว่าใช้งานเท่าไร

```bash
# Prometheus query: total input tokens ทั้งระบบ
sum(api_gateway_token_input_total)

# Per-model breakdown (sync path)
sum by (model) (api_gateway_token_input_total)

# Per-provider breakdown (async path)
sum by (provider) (ai_worker_token_input_total)
```

### ตรวจจับปัญหา

- Token usage แปลกปลอม = อาจมี prompt injection หรือ bug
- Input tokens สูงเกินไป = อาจต้องลด context length
- Output tokens สูงผิดปกติ = model อาจ generate ยาวเกิน

### Grafana dashboards

ระบบมี Grafana dashboard สำเร็จรูปที่ `grafana/provisioning/`.
เปิดได้ที่ `http://localhost:3000` (default password: `changeme`).

---

## Per-Provider RPM Limits

ป้องกัน 429 โดยการจำกัด request rate ต่อ provider:

```bash
# RPM limit ต่อ provider (requests per minute)
PROVIDER_RPM_LIMITS=glm:5,openai:60,anthropic:50,gemini:15,openrouter:30
```

Worker ใช้ sliding window rate limiter ที่จะรอถ้าเกิน limit ก่อนส่ง request.

---

## Per-Model Concurrency Limits

จำกัด concurrent requests ต่อ model (ป้องกัน upstream overload):

```bash
# model:limit คั่นด้วยคอมม่า
UPSTREAM_MODEL_LIMITS=glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3

# Default limit สำหรับ model ที่ไม่ได้ระบุ
UPSTREAM_DEFAULT_LIMIT=1

# Total concurrent requests ทุก model รวมกัน
UPSTREAM_GLOBAL_LIMIT=9
```

---

## Complete .env Example (All Providers)

```bash
# --- API Gateway ---
GATEWAY_PORT=8080
GLOBAL_RATE_LIMIT=100
AGENT_RATE_LIMIT=5
UPSTREAM_URL=https://api.z.ai/api/anthropic
STREAM_TIMEOUT=300s

# Key pool for sync proxy (ถ้าว่าง = passthrough mode)
UPSTREAM_API_KEYS=glm-key-1,glm-key-2
UPSTREAM_RPM_LIMIT=40

# Per-model concurrency
UPSTREAM_MODEL_LIMITS=glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3
UPSTREAM_DEFAULT_LIMIT=1
UPSTREAM_GLOBAL_LIMIT=9

# Per-provider RPM limits
PROVIDER_RPM_LIMITS=glm:5,openai:60,anthropic:50,gemini:15,openrouter:30

# --- GLM / Z.ai (Primary) ---
GLM_API_KEYS=your-glm-key
GLM_ENDPOINT=https://api.z.ai/api/anthropic

# --- Anthropic ---
ANTHROPIC_API_KEYS=sk-ant-api03-your-key

# --- OpenAI ---
OPENAI_API_KEYS=sk-proj-your-key

# --- Google Gemini ---
GEMINI_API_KEYS=AIzaSy-your-key

# --- OpenRouter ---
OPENROUTER_API_KEYS=sk-or-v1-your-key

# --- Worker ---
WORKER_CONCURRENCY=50
MAX_RETRIES=3
RESULT_TTL=600

# --- Docker ---
DOCKER_PLATFORM=linux/arm64

# --- Observability ---
GRAFANA_PORT=3000
GRAFANA_ADMIN_PASSWORD=changeme
```

---

## Troubleshooting

| ปัญหา | สาเหตุ | แก้ไข |
|---|---|---|
| `all keys in cooldown` | ส่ง request เร็วเกิน RPM limit | เพิ่ม key ใน pool หรือลด `PROVIDER_RPM_LIMITS` |
| `no available key` | ไม่ได้ตั้ง API key สำหรับ provider นั้น | เช็กว่า env var ถูกต้องและไม่มี typo |
| `authentication_error` | Sync proxy passthrough mode แต่ไม่ส่ง header | ส่ง `x-api-key` หรือ `Authorization: Bearer` header |
| 429 loop | Provider rate limit ต่ำเกินไปสำหรับ workload | ใช้หลาย key หรือเพิ่ม fallback provider |
| Provider ไม่อยู่ใน fallback chain | Env var ว่างหรือ key list ผิด format | เช็กว่า key คั่นด้วยคอมม่า ไม่มีช่องว่างเกิน |
