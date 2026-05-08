# AI Gateway Presentation Prompt

## Overview
Multi-provider AI proxy gateway between AI clients and upstream providers. Dark theme with cyan (#06b6d4) and purple (#8b5cf6) accents.

## Slide Structure

### Slide 1: Cover Page
```
Title: "AI Gateway"
Subtitle: "Multi-Provider Proxy with Smart Routing for AI Agents"
Chips: Go Gateway, Python Worker, Dragonfly, Multi-Provider, PasteGuard, Profile Routing, Cost Tracking
Author: Thanapat Taweerat - 2026
```

### Slide 2: Why AI Gateway?
```
Left - Problems:
- API Hammering: AI agents send rapid-fire requests exhausting rate limits
- Single Account SPOF: One account hits 429 = entire team blocked
- Zero Visibility: No usage tracking, no cost estimation, no anomaly detection

Right - Solutions:
- Transparent Proxy: Every byte passes through unchanged
- Account Pool + Auto-Rotation: Utilization-aware routing across multiple accounts
- Multi-Provider Fallback: Claude, OpenAI, Gemini - automatic failover
```

### Slide 3: System Architecture
```
Layered diagram:
- Top: Clients (Claude Code, AI Agents, CI/CD)
- Middle: Gateway (Go, :8080) - Auth, Profile Routing, PasteGuard, Account Pool, Rate Limit, Proxy
- Left: Sync path - SSE streaming to upstream provider
- Right: Async path - Dragonfly Queue -> Worker (50 coroutines)
- Bottom: Providers (Z.AI, OpenAI, Gemini, OpenRouter, DeepSeek, 12 more)
```

### Slide 4: Claude OAuth - How We Connect
```
We use PKCE flow to authenticate with Anthropic:

1. Gateway generates PKCE verifier + challenge (S256)
2. User authorizes via platform.claude.com
3. Gateway exchanges code for access_token + refresh_token
4. Tokens stored in Dragonfly, auto-refreshed when near expiry

Why PKCE: No client secret needed, prevents code interception attacks

Our setup:
- Multiple Claude accounts in pool
- Auto-refresh tokens (30min cycle)
- On 429: rotate to next account automatically
- Auto-rewrite sonnet/opus -> haiku to avoid org-level rate limits
```

### Slide 5: Multi-Provider Routing
```
Model-based routing:
- claude-* -> Claude OAuth (Bearer)
- glm-* -> Z.AI (API key)
- gpt-*/o1-*/o3-* -> OpenAI (API key)
- gemini-* -> Gemini (API key)
- deepseek-* -> DeepSeek (API key)
- Any prefix/* -> OpenRouter (API key)

Flow: Resolve provider -> Select account from pool (prefer <80% util) -> Transparent proxy -> On 429: cooldown 60s, retry next account

Key: Provider isolation (Claude keys never sent to OpenAI), OAuth support via PKCE
```

### Slide 6: Account Pool & Utilization
```
Algorithm:
1. Load accounts from Redis
2. Partition: low-util (<80%) vs high-util
3. Route to low-util first (round-robin)
4. Fallback to high-util when all low-util busy
5. On 429: cooldown 60s, auto-recover

Scaling:
- 1x Claude Pro = ~45 RPM
- 2x Claude Pro = ~90 RPM
- 1x Claude + 1x OpenAI = ~210 RPM
- All combined = 270+ RPM
```

### Slide 7: Profile-Based Routing
```
Profile = named config in Redis: model, provider, account pool, base URL

Token system:
- Create profile "team-a" -> Generate profile token
- Use as `ANTHROPIC_AUTH_TOKEN` in Claude Code settings
- Gateway intercepts `arl_*` prefix -> lookup Redis -> override routing

> **Note:** `apiKeyHelper` + `ANTHROPIC_API_KEY` is the legacy method. Use `ANTHROPIC_AUTH_TOKEN` instead.

Use cases:
- Team segmentation: juniors on Haiku, seniors on Sonnet/Opus
- Cost control: cheaper models per profile, track usage per profile
- Provider isolation: route profile A to Claude, profile B to OpenAI
- Testing/canary: compare providers without changing client config
```

### Slide 8: Client-Side Setup
```
Step 1 - Admin creates profile:
  POST /v1/profiles/ {"name":"my-team","target":"claude-oauth"}
  POST /v1/profiles/my-team/tokens {"keyName":"laptop"}
  -> Returns profile token

Step 2 - Edit ~/.claude/settings.json:
  {
    "env": {
      "ANTHROPIC_BASE_URL": "http://gateway:8080",
      "ANTHROPIC_AUTH_TOKEN": "<profile_token>"
 }
  }

> **Note:** `apiKeyHelper` + `ANTHROPIC_API_KEY` is the legacy method. Use `ANTHROPIC_AUTH_TOKEN` instead.

Step 3 - Run:
  claude          # Interactive mode
  claude -p "hi"  # Pipe mode

Done. All requests route through gateway automatically.
```

### Slide 9: Feature Summary - Monitor, Optimize, Secure
```
.col cols
Monitor                          Optimize
- Request latency & TTFB         - Adaptive model selection via profiles
- Token usage (input/output)     - Prompt caching (cache hit tracking)
- Cost per provider/model/profile- Multi-provider arbitrage (cheapest capable)
- 429 rate limit events          - Parameter stripping (auto-strip for Haiku)
- Anomaly detection (Z-score)    - Whitespace optimization (3-5% savings)
- Real-time event timeline       - Duplicate dedup (hash + Levenshtein)
- Per-account utilization        - Token budget tracking (Green/Yellow/Red)

Secure
- PasteGuard: regex + NLP masking (secrets, PII never reach upstream)
- IP filtering / CIDR whitelist
- Profile token auth (no raw keys on client machines)
- Provider isolation (keys never cross providers)
```

### Slide 10: PasteGuard - Privacy Pipeline
```
Two-phase masking before upstream:

Phase 1 - Regex (sub-ms):
- API keys (sk-ant-*, AKIA*), tokens, passwords, private keys, connection strings

Phase 2 - Presidio NLP:
- Person names, emails, phones, addresses, SSN

After response: unmask all substitutions (reversible)

Key: AI providers never see raw secrets or PII. Zero latency impact for regex path.
```

### Slide 11: Cost Tracking
```
Per-request cost estimation:
- Extract tokens from response usage
- Lookup pricing table per model
- Record to Dragonfly hourly/daily/monthly by provider/model/account/profile

| Model | Input/1M | Output/1M |
| Claude Opus 4.7 | $15 | $75 |
| Claude Sonnet 4.6 | $3 | $15 |
| Claude Haiku 4.5 | $0.80 | $4 |
| GPT-4.1 | $2 | $8 |
| Gemini 2.5 Pro | $1.25 | $10 |
| DeepSeek V3 | $0.27 | $1.10 |

Dashboard: cost per day/week/month, by provider, by profile, projected monthly
```

### Slide 12: 17 Providers & Fallback
```
Full list: Anthropic, Claude, OpenAI, Google Gemini, Gemini OAuth, OpenRouter, GitHub Copilot, DeepSeek, Qwen, Kimi, Hugging Face, Ollama, Z.AI (GLM), AGY, Cursor, CodeBuddy, Kilo

Fallback chain:
1. Try provider A (rotate accounts on 429)
2. All exhausted -> fallback to provider B with model mapping
3. Provider B down -> fallback to provider C
4. All failed -> exponential backoff (max 3), then error

| Provider | Format | Auth |
| Claude OAuth | Native Anthropic | PKCE + Bearer |
| OpenAI | OpenAI-compat | API key |
| Gemini | Google AI | API key / OAuth |
| OpenRouter | OpenAI-compat | API key |
```

### Slide 13: Claude Code Compatibility
```
Setup: ANTHROPIC_BASE_URL=gateway + ANTHROPIC_AUTH_TOKEN=profile_token

Compatibility (all PASS):
Read/Edit/Bash/Write, Streaming, Extended thinking, Image/Vision, MCP Servers, Multi-turn, Skills, Memory, NotebookEdit, TodoRead/TodoWrite

Why it works: Gateway is transparent. Skills expanded at client, memory is local files, MCP is client-side.

Auto features:
- sonnet/opus -> haiku rewrite (avoid org-level 429)
- max_tokens clamp per model (200K for claude models)
- Unsupported parameter stripping (effort, thinking for haiku)
```

### Slide 14: Dashboard & Observability
```
Dashboard pages:
- Overview: status cards, capacity bar, model utilization, event timeline
- Profiles: CRUD, account pool, token generation, setup guide
- Providers: OAuth flows, API key management, account CRUD
- Usage & Cost: time-bucket analytics, per-model cost, projections

Prometheus (21 metrics):
- request_duration_seconds, ttfb_seconds, token_input/output_total
- cost_total, adaptive_limit, upstream_429_total, anomaly_total

Anomaly detection: Z-score ring buffer (1000 samples), severity levels

Tech: React 18, TypeScript, Vite, Tailwind, shadcn/ui
```

### Slide 15: Key Differentiators
```
6 differentiators:
1. Transparent Proxy: Zero body modification, any AI client works
2. Multi-Provider: 17 providers, automatic failover, no vendor lock-in
3. PasteGuard: Privacy pipeline, secrets never reach AI providers
4. Cost Optimization: Profile routing, per-request tracking, 95%+ savings
5. Account Pool: Utilization-aware routing, auto-cooldown, horizontal scale
6. Production Ready: 21 Prometheus metrics, anomaly detection, Grafana dashboards
```

### Slide 16: Thank You
```
Title: "Thank You"
Subtitle: "AI Gateway"
Link: github.com/klxhunter/agent-rate-limit
"Questions?"
```

## Style Guide

```
- Dark background (#0a0a1a)
- Cyan (#06b6d4) and purple (#8b5cf6) accents
- Use .cols for two-column layouts
- Use .feat cards for feature descriptions
- Use .flow-box and .flow-arrow for architecture diagrams
- Use .chip tags for tech labels
- Use .stat-card for metric highlights
- Use code and pre for code/flow blocks
- Keep text minimal, let visuals and code blocks tell the story
- No bullet point walls - use structured cards and flow diagrams
```
