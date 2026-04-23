# Prompt: Generate Marp Presentation for AI Rate Limit Gateway

## Context

Generate a **Marp presentation** for the **AI Gateway** project.
This is a multi-provider AI proxy gateway (GLM_MODE=false) that sits between AI clients (Claude Code, AI agents, CI/CD) and upstream AI providers (Claude OAuth, OpenAI, Gemini, OpenRouter, DeepSeek, etc.).

**Key constraint: GLM_MODE=false** - This means the gateway operates in **multi-provider mode**, not Z.AI default mode. There is no single default provider. Each model routes to its native provider.

## Tech Stack

- **Gateway**: Go (chi router, atomic CAS lock-free hot path, sync.Cond waiting)
- **Worker**: Python (asyncio, 50 coroutines, BRPOP queue)
- **Queue/Cache**: Dragonfly (Redis-compatible, multi-threaded)
- **Dashboard**: React 18 + TypeScript + Vite + shadcn/ui + Recharts
- **Observability**: Prometheus (21 metrics) + Grafana + OTel Collector

## Slide Order & Content Guide

### Slide 1: Cover Page

Title: "AI Gateway"
Subtitle: "Multi-Provider Proxy with Smart Routing for AI Agents"
Chips: Go Gateway, Python Worker, Dragonfly, Multi-Provider, PasteGuard, Profile Routing, Cost Tracking
Author: Thanapat Taweerat - 2026

### Slide 2: Why AI Gateway? (Problem vs Solution)

Problem side:
- API Hammering: Claude Code and AI agents send rapid-fire requests exhausting rate limits
- Single Account SPOF: One account hits 429 = entire team blocked
- Zero Visibility: No usage tracking, no cost estimation, no anomaly detection

Solution side:
- Transparent Proxy: Every byte passes through unchanged
- Account Pool + Auto-Rotation: Multiple accounts with utilization-aware routing
- Multi-Provider Fallback: Claude, OpenAI, Gemini - automatic failover when one provider is down

### Slide 3: System Architecture (Diagram)

Show layered architecture:
- **Top**: Clients (Claude Code, AI Agents, CI/CD)
- **Middle**: arl-gateway:8080 (Go) - Auth, Profile Routing, PasteGuard, Account Pool, Rate Limit, Proxy
- **Left branch**: Sync path - Transparent Proxy with SSE streaming to upstream provider
- **Right branch**: Async path - Dragonfly Queue -> arl-worker (50 coroutines)
- **Bottom**: Providers (Claude OAuth, OpenAI, Gemini, OpenRouter, DeepSeek, 12 more)

Two modes:
- Sync: `/v1/messages` - Real-time SSE streaming, transparent proxy. Client sends request, gateway selects account/provider, forwards bytes to upstream, streams response back. Used by Claude Code and other Anthropic SDK clients. No queue, no polling - direct passthrough.
- Async: `/v1/chat/completions` - Queue + worker + cache, poll `/v1/results/{id}`. Client submits job, gateway enqueues to Dragonfly, Python worker picks up and processes. Client polls for result. Used by non-streaming AI agents and batch workloads. Supports multi-provider fallback with automatic retry.

### Slide 4: Multi-Provider Routing (GLM_MODE=false)

Core concept: model name determines provider routing.

- `claude-*` -> Claude OAuth provider (Bearer token auth)
- `gpt-*` -> OpenAI provider (API key)
- `gemini-*` -> Gemini provider (API key)
- `deepseek-*` -> DeepSeek provider (API key)
- `openrouter-*` -> OpenRouter provider (API key)

Flow: Resolve provider from model -> Select account from pool (utilization-aware round-robin, prefer <80%) -> Transparent proxy (rewrite model field only) -> On 429: cooldown 60s, retry next account

Key design: No single default provider. Provider isolation (Claude keys never sent to OpenAI). OAuth support via PKCE/device code.

### Slide 5: Account Pool & Utilization Routing

Algorithm:
1. Load accounts from Redis for provider
2. Partition: low-util (<80%) vs high-util (>=80%)
3. Route to low-util first (round-robin within)
4. Fallback to high-util only when all low-util busy
5. On 429: cooldown 60s, auto-recover after

Scaling example:
- 1x Claude Pro = ~45 RPM
- 2x Claude Pro = ~90 RPM
- 1x Claude + 1x OpenAI = ~210 RPM
- All combined = 270+ RPM

### Slide 6: Profile-Based Routing

Profile = named config in Redis overriding model, provider, account pool, base URL.

Token system:
- Create profile "meow" (Haiku) -> Generate `arl_meow_x7Kp9mNx...`
- Use as `ANTHROPIC_API_KEY` in Claude Code settings
- Gateway intercepts `arl_*` prefix -> lookup Redis -> override routing

Use cases:
- Team segmentation: juniors on Haiku, seniors on Sonnet/Opus
- Cost control: cheaper models per profile, track usage per profile
- Provider isolation: route profile A to Claude, profile B to OpenAI
- Testing/canary: compare providers without changing client config

### Slide 7: PasteGuard - Privacy Pipeline

Two-phase masking before upstream:

Phase 1 - Regex Masking (fast, sub-ms):
- API keys (sk-ant-*, AKIA*), tokens, passwords
- AWS keys, private keys, connection strings, credit cards
- Replace with `[REDACTED_TYPE_N]`

Phase 2 - Presidio NLP (deep, configurable):
- Person names, emails, phones, addresses, SSN, org names
- Replace with `[PII_TYPE_N]`

After upstream response: unmask all substitutions (reversible).

Key point: AI providers never see raw secrets or PII. Zero latency impact for regex path.

### Slide 8: Cost Calculator & Tracking

Per-request cost estimation:
- Extract input_tokens, output_tokens, cache_read, cache_creation from response usage
- Lookup pricing table per model
- Calculate: cost = (input * price.in) + (output * price.out) + (cache adjustments)
- Record to Dragonfly buckets: hourly/daily/monthly, dimensions: provider/model/account/profile
- Emit Prometheus counter: `cost_total{provider, model, account}`

Pricing examples:
| Model | Input/1M | Output/1M |
| Claude Opus 4.7 | $15 | $75 |
| Claude Sonnet 4.6 | $3 | $15 |
| Claude Haiku 4.5 | $0.80 | $4 |
| GPT-4.1 | $2 | $8 |
| Gemini 2.5 Pro | $1.25 | $10 |
| DeepSeek V3 | $0.27 | $1.10 |

Dashboard views: cost per day/week/month, by provider, by profile, by model, projected monthly.

### Slide 9: Token Optimization

Strategies:
- Adaptive model selection via profiles (Haiku for simple, Sonnet for complex -> 5-10x savings)
- Prompt caching (Claude API cache hit tracking)
- Parameter stripping (auto-strip effort/thinking for Haiku, prevent 400 errors)
- Multi-provider arbitrage (route to cheapest capable provider)
- Whitespace optimization (3-5% savings, zero cost)
- Head/tail truncation (40% head + 60% tail with marker)
- Duplicate content deduplication (hash + Levenshtein similarity 0.85)
- Token budget tracking (Green <50%, Yellow 50-75%, Red >75%)

Cost comparison example:
- Code review task: 50K input, 5K output
- Opus: $1.125, Sonnet: $0.225, Haiku: $0.060, DeepSeek: $0.019
- Savings: Opus -> Haiku = 95%, Opus -> DeepSeek = 98%

### Slide 10: Rate Limit Handling

Adaptive concurrency limiter (Envoy gradient + Netflix inspired):
- On 429: halve limit, remember peakBefore429
- On success (every 5th): gradient = (minRTT + buffer) / sampleRTT, limit = gradient * limit + sqrt(limit)
- Learned ceiling decays after 5 minutes

Multi-layer protection:
| Layer | Scope | Mechanism |
| Global | All requests | Adaptive gradient limit |
| Per-provider | Provider-level | RPM limit |
| Per-account | Account-level | 60s cooldown on 429 |
| Per-model | Model-level | Slot-based concurrency |
| Per-agent | Client-level | 5 RPM per agent_id |
| Per-IP | IP-level | Login rate limiting |

Fail-open design: Dragonfly down / rate limiter down -> requests pass through.

### Slide 11: 17 Providers

Full list: Anthropic, Claude OAuth, OpenAI, Google Gemini, Gemini OAuth, OpenRouter, GitHub Copilot, DeepSeek, Qwen (Aliyun), Kimi, Hugging Face, Ollama, Z.AI (GLM), AGY, Cursor, CodeBuddy, Kilo

Auth types: 13 API key providers, 4 OAuth/device code providers (Claude, Gemini, Copilot, Qwen)
Fallback: Automatic for API key providers, manual via dashboard for OAuth.

### Slide 12: Provider Fallback Chain

Show failover flow:
1. Try provider A (e.g., claude-oauth) -> rotate through accounts on 429
2. All accounts exhausted -> fallback to provider B (e.g., OpenAI) with model mapping
3. Provider B also down -> fallback to provider C (e.g., Gemini)
4. All failed -> retry with exponential backoff (max 3), then return error

Throughput scaling formula: sum of all provider RPMs.

Per-provider handling table:
| Provider | Format | Streaming | Special |
| Claude OAuth | Native Anthropic | SSE | PKCE + Bearer |
| OpenAI | OpenAI-compat | SSE | Native |
| Gemini | Google AI | SSE | Native |
| OpenRouter | OpenAI-compat | SSE | Model aliasing |

### Slide 13: Claude Code Integration

Three setup modes:
1. Direct: ANTHROPIC_BASE_URL=http://localhost:8080, ANTHROPIC_AUTH_TOKEN=your-key
2. Profile: ANTHROPIC_API_KEY=arl_meow_x7Kp9mNx... (routes to profile config)
3. Docker: docker-compose with profile token, `claude --bare` for interactive mode

Compatibility matrix (all PASS):
Read/Edit/Bash/Write, Streaming SSE, Extended thinking, Image/Vision, MCP Servers, Multi-turn, Skills, Memory, NotebookEdit, TodoRead/TodoWrite

Why it works: Gateway is transparent pass-through. Skills expanded at client, memory is local files, MCP is client-side. Gateway only proxies.

### Slide 14: Haiku Support & Parameter Stripping

Problem: Claude Code sends parameters Haiku doesn't support (effort in output_config, thinking/budget_tokens, anthropic-beta effort-* headers, context_management clear_thinking edits). Each causes 400 errors.

Solution - auto-stripping at two layers:
- Body stripping (handler): delete thinking, budget_tokens, effort from output_config, filter thinking-dependent context_management edits
- Header stripping (proxy): remove effort-* and interleaved-thinking-* beta flags from anthropic-beta header

Result: zero 400 errors for Haiku profiles. No client-side changes needed.

### Slide 15: Dashboard

Pages:
- Overview: status cards, capacity bar, model utilization, key flow monitor (live SVG), event timeline
- Profiles: CRUD, account pool management, token generation, Docker setup guide
- Providers: OAuth flows, API key management, account CRUD, per-provider health
- Usage & Cost: time-bucket analytics, per-model cost, per-account quota, cost projections
- Health & Limiter: 6 health checks, adaptive limit override, thinking budget, live config editing

Tech: React 18, TypeScript, Vite, Tailwind CSS, shadcn/ui, Recharts
Served at `/admin` with cookie-based auth via DASHBOARD_API_KEY.

Key flow monitor: SVG visualization showing accounts -> gateway -> providers with hover-highlight.

### Slide 16: Observability

Prometheus metrics (21 total):
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

Anomaly detection: Z-score ring buffer (1000 samples), severity: Critical (>4.0), High (>3.0), Medium (>2.0), Sustained (5+ consecutive).

WebSocket events: request-completed, request-error, anomaly-detected, request-queued, quota-warning, config-changed.

### Slide 17: Middleware Stack

Show ordered middleware chain:
1. SecurityHeaders (X-Content-Type-Options, X-Frame-Options)
2. CorrelationID (generate/propagate X-Correlation-ID)
3. RealIP (CF-Connecting-IP > X-Real-IP > X-Forwarded-For)
4. IPFilter (CIDR whitelist/blacklist)
5. Logging (structured JSON)
6. Metrics (latency + connections + status)
7. Rate Limiter (global 100/min + per-agent 5/min)
8. Login Limiter (5 attempts / 15min per IP)
-> Route Handler

Fail-open: rate limiter unreachable = requests pass through.
Identity: x-api-key or Authorization header for /v1/messages, arl_* prefix auto-detected.

### Slide 18: Docker Deployment

Show docker-compose.yml structure:
- arl-gateway (Go, port 8080, env: REDIS_ADDR, GLM_MODE=false, DASHBOARD_API_KEY)
- dragonfly (6G, 4 threads, cache mode, pipeline squash)
- arl-worker (Python, 50 coroutines)
- prometheus (scrape gateway metrics)
- grafana (port 3000, dashboards)

Quick start:
```bash
git clone <repo> && cd agent-rate-limit
docker compose up -d
open http://localhost:8080/admin  # Configure providers
# Add Claude OAuth account, Add OpenAI key, Create profile
# Edit ~/.claude/settings.json -> Done!
```

Resource table: gateway 512M, worker 1G, Dragonfly 6G, rate-limiter 768M, prometheus 512M, grafana 256M.

### Slide 19: Security Model

5 layers:
1. Network: IP filtering, CIDR whitelist/blacklist, Cloudflare integration
2. Authentication: API key validation, arl_* profile tokens, dashboard cookie auth
3. PasteGuard: Secrets/PII masking before upstream, reversible unmasking
4. Rate Limiting: Multi-layer (global, per-provider, per-account, per-agent, per-IP)
5. Headers: Security headers, HSTS, no-sniff, CORS

PasteGuard detail: AI providers train on your data. PasteGuard ensures they never see secrets/PII.

### Slide 20: Key Differentiators

6 differentiators in 2x3 grid:
1. Transparent Proxy: Zero body modification, any AI client works
2. Multi-Provider: 17 providers, automatic failover, no vendor lock-in
3. PasteGuard: Privacy pipeline, secrets never reach AI providers
4. Cost Optimization: Profile routing, per-request tracking, 95%+ savings
5. Account Pool: Utilization-aware routing, auto-cooldown, scale by adding accounts
6. Production Ready: Adaptive rate limiting, 21 metrics, anomaly detection, Docker Compose

### Slide 21: Performance Highlights

Stat cards:
- 100% Success Rate
- 0 429 Errors
- 17 Providers
- 21 Metrics

Bottom row:
- Transparent (Zero Body Modification)
- Multi-Provider (Auto Failover)
- PasteGuard (Privacy Pipeline)

### Slide 22: Thank You

Title: "Thank You"
Subtitle: "AI Gateway"
Link: github.com/klxhunter/agent-rate-limit
"Questions?"

## Style Guide

- Use the existing Marp frontmatter style from `marp.md` (dark theme, gradient backgrounds, custom CSS classes)
- Use `.cols` for two-column layouts
- Use `.feat` cards for feature descriptions
- Use `.flow-box` and `.flow-arrow` for architecture diagrams
- Use `.chip` tags for tech labels
- Use `.stat-card` for metric highlights
- Use `code` and `pre` for code/flow blocks
- Keep text minimal, let visuals and code blocks tell the story
- No bullet point walls - use structured cards and flow diagrams
- Dark background (#0a0a1a), cyan (#06b6d4) and purple (#8b5cf6) accents

## Important Notes

- This is GLM_MODE=false presentation. Do NOT focus on Z.AI as default provider.
- Multi-provider is the core theme: Claude OAuth, OpenAI, Gemini, OpenRouter, DeepSeek are all first-class.
- Profile-based routing is a key feature - emphasize team segmentation and cost control.
- PasteGuard is a unique differentiator - give it a dedicated slide.
- Cost tracking is built-in, not an afterthought.
- Haiku parameter stripping enables cheap Claude Code sessions via profiles.
- Transparent proxy design is why Claude Code works perfectly - explain this clearly.
