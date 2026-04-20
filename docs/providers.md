# Provider Setup Guide / คู่มือตั้งค่า Provider

ระบบรองรับ 5 providers สำหรับ async queue path (POST /v1/chat/completions -> Dragonfly -> Python Worker) และ 17 providers ผ่าน gateway registry (sync path + OAuth/API key auth flows).
สำหรับ sync proxy path (POST /v1/messages -> Go Gateway) ใช้ `UPSTREAM_URL` ชี้ไปยัง provider ตัวใดตัวหนึ่งโดยตรง.

Provider บางตัว (Claude, Gemini) มี subscription plan หรือ free tier ที่ไม่คิดตาม token -- แต่ระบบยัง track token usage ผ่าน Prometheus เพื่อ monitoring เหมือนเดิม.

## Architecture Recap

```
Sync path:  Client -> POST /v1/messages -> Go Gateway -> UPSTREAM_URL (single provider)
Async path: Client -> POST /v1/chat/completions -> Dragonfly -> Worker -> multi-provider dispatch
```

Provider fallback order: `glm` -> `openai` -> `anthropic` -> `gemini` -> `openrouter` -> `deepseek` -> `kimi` -> `huggingface` -> `ollama` -> `agy` -> `cursor` -> `codebuddy` -> `kilo`

Async path (Python worker) uses the first 5 providers with configured keys. Gateway registry supports all 17 providers with OAuth/API key auth flows.

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
# Sync proxy path (Gateway): use UPSTREAM_API_KEYS + UPSTREAM_URL
UPSTREAM_URL=https://api.z.ai/api/anthropic
UPSTREAM_API_KEYS=your-zai-key-1,your-zai-key-2

# Async path (Worker): use GLM_API_KEYS + GLM_ENDPOINT (unchanged)
GLM_ENDPOINT=https://api.z.ai/api/anthropic
GLM_API_KEYS=your-zai-key-1,your-zai-key-2
```

> **Note**: `GLM_API_KEYS` and `GLM_ENDPOINT` have been removed from the sync proxy path. The gateway now uses `UPSTREAM_API_KEYS` and `UPSTREAM_URL` exclusively for the sync proxy. The worker async path still uses `GLM_API_KEYS`/`GLM_ENDPOINT` independently.

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

### Dashboard registration

เพิ่ม Z.AI API key ผ่าน dashboard ที่หน้า Providers:
1. ไปที่ /providers
2. กด Connect ที่การ์ด Z.AI
3. ใส่ API key ในช่อง input (กด icon ตาเพื่อ show/hide)
4. กด Register

รองรับหลาย key ต่อ provider -- เพิ่มได้เรื่อย ๆ ผ่านปุ่ม Add.
Key แรกจะถูกตั้งเป็น default อัตโนมัติ.
Account management (pause/resume/remove/set default) ทำได้จาก account list ใต้การ์ด provider.

### Token tracking

Prometheus metrics:
- `api_gateway_token_input_total{model="glm-5"}` (sync path)
- `ai_worker_token_input_total{provider="glm",model="glm-5"}` (async path)
- `api_gateway_cost_total{model="..."}` (estimated cost from token usage x pricing)

Usage recording (sync path): Token counts automatically populate Redis hourly/daily/monthly/session buckets via `metrics.RecordTokens()` callback. Query via `/v1/usage/*` endpoints.

### Z.AI Pricing

19 Z.AI models have accurate pricing from https://docs.z.ai/guides/overview/pricing. Includes flash (free tier), air, and turbo variants. The `api_gateway_cost_total` metric reflects real pricing.

| Tier | Models | Notes |
|------|--------|-------|
| Flash | glm-5-flash, glm-4.6-flash, glm-4.6v-flash | Free tier available |
| Air | glm-5-air, glm-5-air-turbo | Budget tier |
| Standard | glm-5, glm-5-turbo, glm-5.1 | General purpose |
| Plus | glm-4.7, glm-4.6, glm-4.5 | Older generation |
| Vision | glm-4.6v, glm-4.5v, glm-4.6v-flashx | Image analysis |

### Vision Models (Native Zhipu Endpoint)

GLM vision models ใช้ native Zhipu API endpoint แยกจาก z.ai Anthropic-compatible endpoint:

```bash
# Vision endpoint (auto-configured, ไม่ต้องตั้งเอง)
NATIVE_VISION_URL=https://open.bigmodel.cn/api/paas/v4/chat/completions
```

Gateway ตรวจจับ image content อัตโนมัติแล้ว route ไป native endpoint:

```
Text request -> api.z.ai/api/anthropic (Anthropic-compatible)
Image request -> open.bigmodel.cn/api/paas/v4/chat/completions (OpenAI-compatible)
```

Z.AI vision API รองรับเฉพาะ `user` and `assistant` roles และ `text`, `image`, `image_url` content types.
Gateway ทำการแปลงอัตโนมัติ: system prompt text ถูก prepend เข้าไปใน first user message,
และ Anthropic-specific content blocks (`server_tool_use`, `tool_use`, `tool_result`) ถูก strip ออกก่อนส่ง

Vision models ที่รองรับ:

| Model | Slots | Input Price | Notes |
|-------|-------|-----------|-------|
| glm-4.6v | 10 | Same as glm-4.6 | แนะนำ, default for most vision requests |
| glm-4.5v | 10 | Same as glm-4.5 | Works well |
| glm-4.6v-flashx | 3 | Lower | Auto-selected for heavy payloads |
| glm-4.6v-flash | 1 | Lower | Fast, not auto-selected |

Gateway เลือก vision model อัตโนมัติ:
- `score = totalBase64KB + (imageCount * 300)`
- score <= 2000 and count < 3 -> glm-4.6v (best quality)
- score > 2000 or count >= 3 -> glm-4.6v-flashx (fastest)

SSE streaming รองรับแล้ว: Zhipu SSE chunks ถูก convert เป็น Anthropic SSE format แบบ real-time

**การแปลง format อัตโนมัติ** (ไม่ต้องตั้งค่าเพิ่มเติม):
- `role: "system"` → ถูกกรองออก, text นำหน้าไปที่ user message แรก
- `server_tool_use`, `tool_use`, `tool_result` → ถูกกรองออก (Z.AI ไม่รองรับ)
- `type: "image"` (Anthropic) → `type: "image_url"` (Z.AI format)
- ส่งผ่านเฉพาะ role `user`/`assistant` และ content type `text`/`image`/`image_url`

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

## 6. DeepSeek

DeepSeek API สำหรับ DeepSeek Chat และ DeepSeek Coder models.

### Setup

```bash
# API keys
DEEPSEEK_API_KEYS=sk-xxxx,sk-yyyy
```

### How to get API key

1. ไปที่ [platform.deepseek.com](https://platform.deepseek.com)
2. ล็อกอิน หรือสมัคร
3. ไปที่ API Keys -> Create new key
4. คัดลอก key

### Auth mechanism

- Header: `Authorization: Bearer <key>`
- SDK: OpenAI-compatible API
- Upstream: `https://api.deepseek.com`
- Default model: `deepseek-chat`

### Pricing

DeepSeek คิดเงินตาม token usage. ราคาประหยัดกว่า provider อื่นอย่างมาก.

---

## 7. Kimi (Moonshot)

Kimi AI โดย Moonshot AI. รองรับ context window ยาว.

### Setup

```bash
# API keys
KIMI_API_KEYS=sk-xxxx,sk-yyyy
```

### How to get API key

1. ไปที่ [platform.moonshot.cn](https://platform.moonshot.cn)
2. ล็อกอิน หรือสมัคร
3. ไปที่ API Keys -> Create new key
4. คัดลอก key

### Auth mechanism

- Header: `Authorization: Bearer <key>`
- SDK: OpenAI-compatible API
- Upstream: `https://api.moonshot.cn/v1`
- Default model: `moonshot-v1-8k`

---

## 8. Hugging Face

Hugging Face Inference API สำหรับ open-source models.

### Setup

```bash
# API keys
HUGGINGFACE_API_KEYS=hf_xxxx,hf_yyyy
```

### How to get API key

1. ไปที่ [huggingface.co](https://huggingface.co)
2. ล็อกอิน หรือสมัคร
3. ไปที่ Settings -> Access Tokens -> New token
4. คัดลอก token (ขึ้นต้นด้วย `hf_`)

### Auth mechanism

- Header: `Authorization: Bearer <key>`
- Upstream: `https://api-inference.huggingface.co/models`
- Model format: model repository ID (เช่น `meta-llama/Llama-3-70b-chat-hf`)

### Free tier

Hugging Face มี free inference API แต่ rate limit ต่ำ. Pro plan ให้ rate limit สูงขึ้น.

---

## 9. Ollama

Ollama สำหรับรัน models ที่ local. เหมาะสำหรับ development และ air-gapped environments.

### Setup

```bash
# ไม่จำเป็นต้องใช้ API key (local)
# แต่ต้องติดตั้ง Ollama และรัน model ก่อน
OLLAMA_UPSTREAM_BASE=http://localhost:11434
```

### Prerequisites

1. ติดตั้ง Ollama: [ollama.com](https://ollama.com)
2. Pull model: `ollama pull llama3`
3. รัน server: `ollama serve` (default port 11434)

### Auth mechanism

- ไม่มี auth (local)
- Upstream: `http://localhost:11434` (configurable via `OLLAMA_UPSTREAM_BASE`)
- Default model: depends on what is pulled locally

### Note

Ollama เหมาะสำหรับ dev/testing. Production ควรใช้ cloud providers.

---

## 10. AGY (Antigravity)

Antigravity AI platform.

### Setup

```bash
AGY_API_KEYS=agy-xxxx
```

### Auth mechanism

- Header: `Authorization: Bearer <key>`
- Upstream: `https://antigravity.com`

---

## 11. Cursor

Cursor AI coding assistant API.

### Setup

```bash
CURSOR_API_KEYS=cursor-xxxx
```

### Auth mechanism

- Header: `Authorization: Bearer <key>`
- Upstream: `https://api2.cursor.sh`

---

## 12. CodeBuddy

CodeBuddy AI coding assistant.

### Setup

```bash
CODEBUDDY_API_KEYS=cb-xxxx
```

### Auth mechanism

- Header: `Authorization: Bearer <key>`
- Upstream: `https://api.codebuddy.io`

---

## 13. Kilo

Kilo AI platform.

### Setup

```bash
KILO_API_KEYS=kilo-xxxx
```

### Auth mechanism

- Header: `Authorization: Bearer <key>`
- Upstream: `https://api.kilo.ai`

---

## OAuth Providers (Gateway Auth Flow)

นอกจาก API key authentication ข้างต้น gateway ยังรองรับ OAuth flows สำหรับ providers เหล่านี้:

### Claude (OAuth)

PKCE-based OAuth flow ผ่าน `platform.claude.com`:
- Auth URL: `https://platform.claude.com/oauth/authorize`
- Token URL: `https://platform.claude.com/v1/oauth/token`
- Client ID: embedded (from Claude Code CLI pattern)
- Scopes: org:create_api_key, user:profile, user:inference, user:sessions:claude_code, user:mcp_servers, user:file_upload
- Callback: `http://localhost:{port}/callback` (loopback redirect)

### Google Gemini (OAuth via Code Assist)

OAuth ผ่าน Gemini Code Assist proxy:
- Auth URL: `https://accounts.google.com/o/oauth2/v2/auth`
- Token URL: `https://oauth2.googleapis.com/token`
- Upstream: `cloudcode-pa.googleapis.com` (not generativelanguage.googleapis.com)
- Scopes: cloud-platform, userinfo.email, userinfo.profile

### GitHub Copilot

Device code flow:
- Device code URL: `https://github.com/login/device/code`
- Token URL: `https://github.com/login/oauth/access_token`
- Upstream: `https://api.github.com/copilot`

### Qwen (Aliyun)

Device code flow (configurable):
- Device code URL: `QWEN_DEVICE_CODE_URL` env var
- Token URL: `QWEN_TOKEN_URL` env var
- Upstream: `https://dashscope.aliyuncs.com`

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
UPSTREAM_API_KEYS=zai-key-1,zai-key-2
UPSTREAM_RPM_LIMIT=40

# Per-model concurrency
UPSTREAM_MODEL_LIMITS=glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3
UPSTREAM_DEFAULT_LIMIT=1
UPSTREAM_GLOBAL_LIMIT=9

# Per-provider RPM limits
PROVIDER_RPM_LIMITS=glm:5,openai:60,anthropic:50,gemini:15,openrouter:30

# --- GLM / Z.ai (Primary - async/worker path) ---
# NOTE: Sync proxy uses UPSTREAM_API_KEYS + UPSTREAM_URL above
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

# --- DeepSeek ---
DEEPSEEK_API_KEYS=sk-your-key

# --- Kimi (Moonshot) ---
KIMI_API_KEYS=sk-your-key

# --- Hugging Face ---
HUGGINGFACE_API_KEYS=hf_your-key

# --- Ollama (local) ---
OLLAMA_UPSTREAM_BASE=http://localhost:11434

# --- AGY ---
AGY_API_KEYS=agy-your-key

# --- Cursor ---
CURSOR_API_KEYS=cursor-your-key

# --- CodeBuddy ---
CODEBUDDY_API_KEYS=cb-your-key

# --- Kilo ---
KILO_API_KEYS=kilo-your-key

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
