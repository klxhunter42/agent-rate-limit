# Prompt: สร้าง Marp Presentation สำหรับ AI Gateway

## บริบท

สร้าง **Marp presentation** สำหรับโปรเจกต์ **AI Gateway**
เป็น multi-provider AI proxy gateway (GLM_MODE=false) ที่ทำหน้าที่เป็นตัวกลางระหว่าง AI clients (Claude Code, AI agents, CI/CD) และ upstream AI providers (Claude OAuth, OpenAI, Gemini, OpenRouter, DeepSeek ฯลฯ)

**ข้อจำกัดหลัก: GLM_MODE=false** - Gateway ทำงานในโหมด **multi-provider** ไม่มี default provider ตัวใดตัวหนึ่ง แต่ละ model จะ route ไปยัง provider ตัวเอง

## Tech Stack

- **Gateway**: Go (chi router, atomic CAS lock-free hot path, sync.Cond waiting)
- **Worker**: Python (asyncio, 50 coroutines, BRPOP queue)
- **Queue/Cache**: Dragonfly (Redis-compatible, multi-threaded)
- **Dashboard**: React 18 + TypeScript + Vite + shadcn/ui + Recharts
- **Observability**: Prometheus (21 metrics) + Grafana + OTel Collector

## ลำดับสไลด์และเนื้อหา

### Slide 1: หน้าปก

Title: "AI Gateway"
Subtitle: "Multi-Provider Proxy อัจฉริยะสำหรับ AI Agents"
Chips: Go Gateway, Python Worker, Dragonfly, Multi-Provider, PasteGuard, Profile Routing, Cost Tracking
Author: Thanapat Taweerat - 2026

### Slide 2: ทำไมต้อง AI Gateway? (ปัญหา vs ทางออก)

ด้านปัญหา:
- API Hammering: Claude Code และ AI agents ส่ง request รัวๆ จนหมด rate limit
- Single Account SPOF: Account เดียวโดน 429 = ทีมทั้งทีมใช้งานไม่ได้
- Zero Visibility: ไม่มีการ track usage, ไม่มี cost estimation, ไม่มี anomaly detection

ด้านทางออก:
- Transparent Proxy: ส่งทุก byte ผ่านโดยไม่แก้ไขอะไรเลย
- Account Pool + Auto-Rotation: หมุนเวียน account หลายตัวตาม utilization
- Multi-Provider Fallback: Claude, OpenAI, Gemini - failover อัตโนมัติเมื่อ provider ไหนล่ม

### Slide 3: System Architecture (แผนภาพ)

แสดง layered architecture:
- **ชั้นบน**: Clients (Claude Code, AI Agents, CI/CD)
- **ชั้นกลาง**: arl-gateway:8080 (Go) - Auth, Profile Routing, PasteGuard, Account Pool, Rate Limit, Proxy
- **สายซ้าย**: Sync path - Transparent Proxy พร้อม SSE streaming ไปยัง upstream provider
- **สายขวา**: Async path - Dragonfly Queue -> arl-worker (50 coroutines)
- **ชั้นล่าง**: Providers (Claude OAuth, OpenAI, Gemini, OpenRouter, DeepSeek, และอีก 12 ตัว)

สองโหมดการทำงาน:
- Sync: `/v1/messages` - SSE streaming แบบ real-time, transparent proxy. Client ส่ง request, gateway เลือก account/provider, ส่งต่อไป upstream, stream response กลับ. ใช้กับ Claude Code และ Anthropic SDK clients. ไม่มี queue, ไม่ต้อง polling - direct passthrough.
- Async: `/v1/chat/completions` - Queue + worker + cache, poll `/v1/results/{id}`. Client ส่ง job, gateway ใส่คิว Dragonfly, Python worker รับไปทำงาน. Client poll รอผล. ใช้กับ non-streaming AI agents และ batch workloads. รองรับ multi-provider fallback พร้อม retry อัตโนมัติ.

### Slide 4: Multi-Provider Routing (GLM_MODE=false)

แนวคิดหลัก: ชื่อ model กำหนด provider routing

- `claude-*` -> Claude OAuth provider (Bearer token auth)
- `gpt-*` -> OpenAI provider (API key)
- `gemini-*` -> Gemini provider (API key)
- `deepseek-*` -> DeepSeek provider (API key)
- `openrouter-*` -> OpenRouter provider (API key)

Flow: หา provider จาก model -> เลือก account จาก pool (utilization-aware round-robin, ชอบ <80%) -> Transparent proxy (แก้เฉพาะ model field) -> เจอ 429: cooldown 60s, retry account ถัดไป

Key design: ไม่มี default provider ตัวเดียว. Provider isolation (key ของ Claude ไม่ส่งไป OpenAI). OAuth support ผ่าน PKCE/device code.

### Slide 5: Account Pool & Utilization Routing

อัลกอริทึม:
1. โหลด accounts จาก Redis ตาม provider
2. แบ่ง partition: low-util (<80%) vs high-util (>=80%)
3. Route ไป low-util ก่อน (round-robin ในกลุ่ม)
4. Fallback ไป high-util เฉพาะเมื่อ low-util ไม่ว่างทั้งหมด
5. เมื่อเจอ 429: cooldown 60s, auto-recover หลังหมดเวลา

ตัวอย่างการ scale:
- 1x Claude Pro = ~45 RPM
- 2x Claude Pro = ~90 RPM
- 1x Claude + 1x OpenAI = ~210 RPM
- รวมทั้งหมด = 270+ RPM

### Slide 6: Profile-Based Routing

Profile = config ชื่อใน Redis ที่ override model, provider, account pool, base URL

ระบบ Token:
- สร้าง profile "meow" (Haiku) -> ได้ token: `arl_meow_x7Kp9mNx...`
- ใส่เป็น `ANTHROPIC_API_KEY` ใน Claude Code settings
- Gateway ตรวจจับ prefix `arl_*` -> lookup Redis -> override routing

Use cases:
- แบ่งทีม: junior ใช้ Haiku, senior ใช้ Sonnet/Opus
- ควบคุมต้นทุน: model ถูกกว่าตาม profile, track usage ตาม profile
- Provider isolation: profile A ไป Claude, profile B ไป OpenAI
- Testing/canary: เปรียบเทียบ providers โดยไม่ต้องแก้ client config

### Slide 7: PasteGuard - Privacy Pipeline

Masking สองเฟสก่อนส่ง upstream:

เฟส 1 - Regex Masking (เร็ว, sub-ms):
- API keys (sk-ant-*, AKIA*), tokens, passwords
- AWS keys, private keys, connection strings, credit cards
- แทนที่ด้วย `[REDACTED_TYPE_N]`

เฟส 2 - Presidio NLP (ลึก, configurable):
- ชื่อบุคคล, emails, เบอร์โทร, ที่อยู่, SSN, ชื่อองค์กร
- แทนที่ด้วย `[PII_TYPE_N]`

หลังได้ response จาก upstream: unmask คืนทั้งหมด (reversible)

จุดสำคัญ: AI providers ไม่เคยเห็น secrets หรือ PII ตัวจริง. Regex path ไม่มีผลต่อ latency.

### Slide 8: Cost Calculator & Tracking

การประเมินต้นทุนต่อ request:
- ดึง input_tokens, output_tokens, cache_read, cache_creation จาก response usage
- ค้นหา pricing table ตาม model
- คำนวณ: cost = (input * price.in) + (output * price.out) + (cache adjustments)
- บันทึกลง Dragonfly buckets: hourly/daily/monthly, dimensions: provider/model/account/profile
- ส่ง Prometheus counter: `cost_total{provider, model, account}`

ตัวอย่าง pricing:
| Model | Input/1M | Output/1M |
| Claude Opus 4.7 | $15 | $75 |
| Claude Sonnet 4.6 | $3 | $15 |
| Claude Haiku 4.5 | $0.80 | $4 |
| GPT-4.1 | $2 | $8 |
| Gemini 2.5 Pro | $1.25 | $10 |
| DeepSeek V3 | $0.27 | $1.10 |

มุมมอง Dashboard: ต้นทุนต่อวัน/สัปดาห์/เดือน, ตาม provider, ตาม profile, ตาม model, ประมาณการรายเดือน

### Slide 9: Token Optimization

กลยุทธ์:
- Adaptive model selection ผ่าน profiles (Haiku สำหรับงานง่าย, Sonnet สำหรับงานซับซ้อน -> ประหยัด 5-10x)
- Prompt caching (track cache hit rate ของ Claude API)
- Parameter stripping (strip effort/thinking อัตโนมัติสำหรับ Haiku, ป้องกัน 400 errors)
- Multi-provider arbitrage (route ไป provider ที่ถูกที่สุด)
- Whitespace optimization (ประหยัด 3-5%, ไม่มีค่าใช้จ่าย)
- Head/tail truncation (40% หัว + 60% ท้าย พร้อม marker)
- Duplicate content deduplication (hash + Levenshtein similarity 0.85)
- Token budget tracking (เขียว <50%, เหลือง 50-75%, แดง >75%)

ตัวอย่างเปรียบเทียบต้นทุน:
- งาน code review: 50K input, 5K output
- Opus: $1.125, Sonnet: $0.225, Haiku: $0.060, DeepSeek: $0.019
- ประหยัด: Opus -> Haiku = 95%, Opus -> DeepSeek = 98%

### Slide 10: Rate Limit Handling

Adaptive concurrency limiter (ได้แรงบันดาลใจจาก Envoy gradient + Netflix):
- เมื่อเจอ 429: ลด limit ครึ่งหนึ่ง, จำ peakBefore429
- เมื่อสำเร็จ (ทุก 5 request): gradient = (minRTT + buffer) / sampleRTT, limit = gradient * limit + sqrt(limit)
- Learned ceiling ลดลงหลัง 5 นาที

Multi-layer protection:
| Layer | Scope | กลไก |
| Global | ทุก request | Adaptive gradient limit |
| Per-provider | ระดับ provider | RPM limit |
| Per-account | ระดับ account | 60s cooldown เมื่อเจอ 429 |
| Per-model | ระดับ model | Slot-based concurrency |
| Per-agent | ระดับ client | 5 RPM ต่อ agent_id |
| Per-IP | ระดับ IP | Login rate limiting |

Fail-open: Dragonfly ล่ม / rate limiter ล่ม -> requests ผ่านได้ปกติ

### Slide 11: 17 Providers

รายการทั้งหมด: Anthropic, Claude OAuth, OpenAI, Google Gemini, Gemini OAuth, OpenRouter, GitHub Copilot, DeepSeek, Qwen (Aliyun), Kimi, Hugging Face, Ollama, Z.AI (GLM), AGY, Cursor, CodeBuddy, Kilo

ประเภท Auth: 13 providers ใช้ API key, 4 providers ใช้ OAuth/device code (Claude, Gemini, Copilot, Qwen)
Fallback: อัตโนมัติสำหรับ API key providers, manual ผ่าน dashboard สำหรับ OAuth

### Slide 12: Provider Fallback Chain

แสดง failover flow:
1. ลอง provider A (เช่น claude-oauth) -> หมุน accounts เมื่อเจอ 429
2. Accounts หมด -> fallback ไป provider B (เช่น OpenAI) พร้อม model mapping
3. Provider B ล่มด้วย -> fallback ไป provider C (เช่น Gemini)
4. ทั้งหมดล้มเหลว -> retry ด้วย exponential backoff (สูงสุด 3 ครั้ง), คืน error

สูตร scale throughput: ผลรวม RPM ของทุก provider

ตาราง handling ต่อ provider:
| Provider | Format | Streaming | Special |
| Claude OAuth | Native Anthropic | SSE | PKCE + Bearer |
| OpenAI | OpenAI-compat | SSE | Native |
| Gemini | Google AI | SSE | Native |
| OpenRouter | OpenAI-compat | SSE | Model aliasing |

### Slide 13: Claude Code Integration

สามโหมดการตั้งค่า:
1. Direct: ANTHROPIC_BASE_URL=http://localhost:8080, ANTHROPIC_AUTH_TOKEN=your-key
2. Profile: ANTHROPIC_API_KEY=arl_meow_x7Kp9mNx... (route ตาม profile config)
3. Docker: docker-compose พร้อม profile token, `claude --bare` สำหรับ interactive mode

Compatibility matrix (ทั้งหมด PASS):
Read/Edit/Bash/Write, Streaming SSE, Extended thinking, Image/Vision, MCP Servers, Multi-turn, Skills, Memory, NotebookEdit, TodoRead/TodoWrite

ทำมันถึงใช้ได้: Gateway เป็น transparent pass-through. Skills ขยายที่ client, memory เป็น local files, MCP ทำงาน client-side. Gateway ทำหน้าที่ proxy อย่างเดียว.

### Slide 14: Haiku Support & Parameter Stripping

ปัญหา: Claude Code ส่ง parameters ที่ Haiku ไม่รองรับ (effort ใน output_config, thinking/budget_tokens, anthropic-beta effort-* headers, context_management clear_thinking edits) แต่ละตัวทำให้เกิด 400 errors

ทางออก - auto-stripping สองชั้น:
- Body stripping (handler): ลบ thinking, budget_tokens, effort จาก output_config, filter thinking-dependent context_management edits
- Header stripping (proxy): ลบ effort-* และ interleaved-thinking-* beta flags จาก anthropic-beta header

ผลลัพธ์: ไม่มี 400 errors สำหรับ Haiku profiles. ไม่ต้องแก้ไขอะไรที่ client.

### Slide 15: Dashboard

หน้าต่างๆ:
- Overview: status cards, capacity bar, model utilization, key flow monitor (live SVG), event timeline
- Profiles: CRUD, จัดการ account pool, generate token, คู่มือ setup Docker
- Providers: OAuth flows, จัดการ API key, account CRUD, สถานะแยกตาม provider
- Usage & Cost: time-bucket analytics, ต้นทุนตาม model, quota ตาม account, cost projections
- Health & Limiter: 6 health checks, override adaptive limit, thinking budget, live config editing

Tech: React 18, TypeScript, Vite, Tailwind CSS, shadcn/ui, Recharts
ให้บริการที่ `/admin` พร้อม cookie-based auth ผ่าน DASHBOARD_API_KEY

Key flow monitor: SVG visualization แสดง accounts -> gateway -> providers พร้อม hover-highlight

### Slide 16: Observability

Prometheus metrics (21 ตัว):
- request_duration_seconds (Histogram)
- token_input/output_total (Counter)
- cost_total (Counter)
- adaptive_limit (Gauge)
- ttfb_seconds (Histogram)
- model_fallback_total (Counter)
- anomaly_total (Counter)
- upstream_429_total (Counter)
- go_goroutines (Gauge)
- dragonfly_up (Gauge)
- Scrape interval: 5s

Anomaly detection: Z-score ring buffer (1000 samples), severity: Critical (>4.0), High (>3.0), Medium (>2.0), Sustained (ติดต่อกัน 5+ ครั้ง)

WebSocket events: request-completed, request-error, anomaly-detected, request-queued, quota-warning, config-changed

### Slide 17: Middleware Stack

แสดงลำดับ middleware chain:
1. SecurityHeaders (X-Content-Type-Options, X-Frame-Options)
2. CorrelationID (generate/propagate X-Correlation-ID)
3. RealIP (CF-Connecting-IP > X-Real-IP > X-Forwarded-For)
4. IPFilter (CIDR whitelist/blacklist)
5. Logging (structured JSON)
6. Metrics (latency + connections + status)
7. Rate Limiter (global 100/min + per-agent 5/min)
8. Login Limiter (5 attempts / 15min ต่อ IP)
-> Route Handler

Fail-open: rate limiter ไม่ตอบ = requests ผ่านได้
Identity: x-api-key หรือ Authorization header สำหรับ /v1/messages, prefix arl_* ตรวจจับอัตโนมัติ

### Slide 18: Docker Deployment

แสดงโครงสร้าง docker-compose.yml:
- arl-gateway (Go, port 8080, env: REDIS_ADDR, GLM_MODE=false, DASHBOARD_API_KEY)
- dragonfly (6G, 4 threads, cache mode, pipeline squash)
- arl-worker (Python, 50 coroutines)
- prometheus (scrape gateway metrics)
- grafana (port 3000, dashboards)

Quick start:
```bash
git clone <repo> && cd agent-rate-limit
docker compose up -d
open http://localhost:8080/admin  # ตั้งค่า providers
# เพิ่ม Claude OAuth account, เพิ่ม OpenAI key, สร้าง profile
# แก้ ~/.claude/settings.json -> เสร็จ!
```

Resource table: gateway 512M, worker 1G, Dragonfly 6G, rate-limiter 768M, prometheus 512M, grafana 256M

### Slide 19: Security Model

5 ชั้นความปลอดภัย:
1. Network: IP filtering, CIDR whitelist/blacklist, Cloudflare integration
2. Authentication: API key validation, arl_* profile tokens, dashboard cookie auth
3. PasteGuard: Secrets/PII masking ก่อนส่ง upstream, reversible unmasking
4. Rate Limiting: Multi-layer (global, per-provider, per-account, per-agent, per-IP)
5. Headers: Security headers, HSTS, no-sniff, CORS

รายละเอียด PasteGuard: AI providers เทรนจากข้อมูลของคุณ. PasteGuard ทำให้พวกเขาไม่เห็น secrets/PII ตั้งแต่แรก

### Slide 20: Key Differentiators

6 จุดเด่น จัดเป็น grid 2x3:
1. Transparent Proxy: ไม่แก้ไข body เลย, AI client ตัวไหนก็ใช้ได้
2. Multi-Provider: 17 providers, failover อัตโนมัติ, ไม่ติด vendor
3. PasteGuard: Privacy pipeline, secrets ไม่มีวันไปถึง AI providers
4. Cost Optimization: Profile routing, track ต้นทุนต่อ request, ประหยัด 95%+
5. Account Pool: Utilization-aware routing, auto-cooldown, scale ด้วยการเพิ่ม accounts
6. Production Ready: Adaptive rate limiting, 21 metrics, anomaly detection, Docker Compose

### Slide 21: Performance Highlights

Stat cards:
- 100% Success Rate
- 0 429 Errors
- 17 Providers
- 21 Metrics

แถวล่าง:
- Transparent (Zero Body Modification)
- Multi-Provider (Auto Failover)
- PasteGuard (Privacy Pipeline)

### Slide 22: Thank You

Title: "Thank You"
Subtitle: "AI Gateway"
Link: github.com/klxhunter/agent-rate-limit
"Questions?"

## Style Guide

- ใช้ Marp frontmatter style เดิมจาก `marp.md` (dark theme, gradient backgrounds, custom CSS classes)
- ใช้ `.cols` สำหรับ layout สองคอลัมน์
- ใช้ `.feat` cards สำหรับ feature descriptions
- ใช้ `.flow-box` และ `.flow-arrow` สำหรับ architecture diagrams
- ใช้ `.chip` tags สำหรับ tech labels
- ใช้ `.stat-card` สำหรับ metric highlights
- ใช้ `code` และ `pre` สำหรับ code/flow blocks
- เก็บข้อความให้น้อย, ให้ visuals และ code blocks เล่าเรื่อง
- ห้ามกำแพง bullet points - ใช้ structured cards และ flow diagrams
- พื้นหลังสีดำ (#0a0a1a), สี cyan (#06b6d4) และ purple (#8b5cf6) เป็น accent

## หมายเหตุสำคัญ

- Presentation นี้เป็น GLM_MODE=false. อย่าเน้น Z.AI เป็น default provider
- Multi-provider เป็นธีมหลัก: Claude OAuth, OpenAI, Gemini, OpenRouter, DeepSeek ทั้งหมดเท่ากัน
- Profile-based routing เป็น feature สำคัญ - เน้นการแบ่งทีมและควบคุมต้นทุน
- PasteGuard เป็นจุดเด่นที่ไม่เหมือนใคร - ให้ slide ของมัน
- Cost tracking เป็น built-in, ไม่ใช่ feature เสริม
- Haiku parameter stripping ทำให้ Claude Code sessions ถูกลงได้ผ่าน profiles
- Transparent proxy design เป็นเหตุผลที่ Claude Code ใช้ได้สมบูรณ์ - อธิบายให้ชัด
