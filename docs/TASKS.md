# ARL Feature Parity Tasks

Reference: CCS (`/repo/`) gap analysis vs ARL current state

## Status Legend
- [ ] TODO
- [~] IN PROGRESS
- [x] DONE

---

## Stream A: Go Backend

### A1. OpenRouter Provider
- [x] Add `openrouter` to `provider/registry.go` (api_key auth, https://openrouter.ai/api)
- [x] Add `openrouter` route to `provider/resolver.go` (or- prefix + vendor prefixes, OpenAI-compat, bearer auth, HTTP-Referer header)
- [x] Add OpenRouter to UI PROVIDERS list in `pages/providers/index.tsx`
- [x] No handler changes needed - existing FormatOpenAI dispatch covers it
- **Files:** edited `registry.go`, `resolver.go`, `providers/index.tsx`

### A2. Activate Dead Code
- [x] Wire `AnomalyDetector.Record()` in `handler/handler.go` (calls on every request RTT)
- [x] Mount `DashboardAuth` middleware in `main.go` for `/admin/*` routes (behind DASHBOARD_API_KEY env)
- [x] Mount `IPFilter` in `main.go` (behind IP_WHITELIST/IP_BLACKLIST env vars, fail-open)
- **Files:** edit `handler/handler.go`, `main.go`

### A3. Routing Strategy API
- [x] Add `GET /v1/routing/strategy` endpoint (returns current strategy: round-robin/fill-first)
- [x] Add `PUT /v1/routing/strategy` endpoint (updates strategy)
- [x] Store strategy in config or Dragonfly
- [x] Apply strategy in `proxy/key_pool.go` key selection
- **Files:** edit `handler/handler.go`, `proxy/key_pool.go`, `main.go`

### A4. Quota Per Account
- [x] Add `GET /v1/quota/{provider}/{accountId}` endpoint
- [x] Add `GET /v1/quota/{provider}` endpoint
- [x] Cache results in Dragonfly (30s TTL)
- **Files:** new `handler/quota.go`

### A5. Error Log API
- [x] Add `GET /v1/logs` endpoint (returns recent error entries)
- [x] Add `GET /v1/logs/config` endpoint (log config)
- [x] Store last N errors in ring buffer or Dragonfly
- **Files:** new `handler/logs.go`

### A6. Model Catalog API
- [x] Add `GET /v1/models` endpoint (returns all available models with metadata)
- [x] Include: model name, provider, series, context window, pricing, thinking support
- [x] Add `GET /v1/providers/{provider}/models` (per-provider models)
- **Files:** new `handler/models.go`

### A7. Usage Analytics API
- [x] Add `GET /v1/usage/summary` (total tokens, cost, requests, errors)
- [x] Add `GET /v1/usage/hourly` (hourly aggregated usage)
- [x] Add `GET /v1/usage/daily` (daily aggregated usage)
- [x] Add `GET /v1/usage/monthly` (monthly aggregated usage)
- [x] Add `GET /v1/usage/models` (per-model usage)
- [x] Add `GET /v1/usage/sessions` (per-session usage)
- **Files:** new `handler/usage.go`

### A8. Health Check Expansion
- [x] Add more health check groups: Dragonfly connectivity, upstream reachability, OAuth token freshness
- [x] Add `POST /v1/health/fix/{checkId}` auto-fix endpoint
- **Files:** new `handler/overview.go`

### A9. WebSocket Real-time Updates
- [x] Add WebSocket endpoint at `/ws`
- [x] Broadcast events: account-changed, model-status-changed, anomaly-detected, config-changed
- [x] File watcher for config changes (broadcasts via WS)
- **Files:** new `handler/websocket.go`, `middleware/config_watcher.go`, edit `main.go`

---

## Stream B: UI Components

### B1. Live Auth Monitor
- [x] Create `components/monitoring/auth-monitor/index.tsx` - main component
- [x] Create `components/monitoring/auth-monitor/provider-card.tsx` - per-provider card
- [x] Create `components/monitoring/auth-monitor/live-pulse.tsx` - animated pulse
- [x] Wire into Overview page below KeyFlowMonitor
- **Data source:** existing `GET /v1/auth/accounts` (poll every 5s)
- **Files:** new `components/monitoring/auth-monitor/*`

### B2. OpenRouter UI
- [x] Add OpenRouter to PROVIDERS array in `pages/providers/index.tsx`
- [x] Create `components/auth/openrouter-model-picker.tsx` (model search/select)
- [x] Fetch model catalog from `/v1/models`
- [x] Cache in localStorage (24h TTL)
- **Files:** edit `pages/providers/index.tsx`, new components

### B3. Quota Cards
- [x] Create `pages/quota/index.tsx` - shows usage vs limit per account
- [x] Add to sidebar navigation
- **Data source:** `GET /v1/quota/{provider}` (from A4)
- **Files:** new `pages/quota/*`

### B4. Routing Strategy Toggle
- [x] Create `components/routing/routing-strategy.tsx`
- [x] Wire into Controls page
- **Data source:** `GET/PUT /v1/routing/strategy` (from A3)
- **Files:** new component, add to Controls or Settings page

### B5. Error Logs Page
- [x] Create `pages/logs/index.tsx` - log viewer with filtering
- [x] Add route to App.tsx
- [x] Add to sidebar navigation
- **Data source:** `GET /v1/logs` (from A5)
- **Files:** new `pages/logs/*`, edit `App.tsx`, edit `app-sidebar.tsx`

### B6. Model Catalog Browser
- [x] Create `pages/models/index.tsx` - browse all models across providers
- [x] Add route to App.tsx
- [x] Add to sidebar navigation
- **Data source:** `GET /v1/models` (from A6)
- **Files:** new `pages/models/*`, edit `App.tsx`, edit `app-sidebar.tsx`

### B7. Usage Insights Page
- [x] Create dedicated usage analytics page or enhance existing `/analytics`
- [x] Daily/hourly breakdown charts
- [x] Per-session usage tracking
- **Data source:** `GET /v1/usage/*` (from A7)
- **Files:** new `pages/analytics/usage-api-section.tsx`, edit `pages/analytics/index.tsx`

### B8. WebSocket Live Updates
- [x] Create `hooks/use-websocket.ts` - WebSocket connection hook
- [x] Auto-reconnect with exponential backoff
- [x] Invalidate / trigger refetch on events (WSBridge + ws-events event bus + use-ws-refresh hook)
- **Data source:** `/ws` (from A9)
- **Files:** new `hooks/use-websocket.ts`, `hooks/use-ws-refresh.ts`, `lib/ws-events.ts`, edit `layout.tsx`

---

## Stream C: Cross-cutting

### C1. Provider Presets Expansion
- [x] Add more providers to registry: DeepSeek, Kimi, HuggingFace, Ollama, Llama.cpp
- [x] Add to UI PROVIDERS array
- **Files:** edit `provider/registry.go`, edit `pages/providers/index.tsx`

### C2. Thinking/Extended Context Config
- [x] Add thinking budget config per provider (Go handler + API)
- [x] UI for configuring thinking mode (Settings page Server Config section)
- **Files:** `handler/config.go`, `pages/settings/index.tsx`

### C3. Dashboard Auth Hardening
- [x] Mount DashboardAuth middleware (from A2)
- [x] Rate-limited login (5 attempts / 15 min)
- [x] Session secret persistence
- **Files:** Go middleware `login_limiter.go`, `session_secret.go`, UI login

### C4. QA / Playwright E2E Tests
- [x] Install Playwright (`@playwright/test`) and configure in `ui/`
- [x] Write E2E test: Overview page loads and shows auth monitor
- [x] Write E2E test: Analytics page renders all charts (cost, distribution, hourly, error rate, latency, anomaly)
- [x] Write E2E test: Models page lists models with search/filter
- [x] Write E2E test: Logs page shows error log entries
- [x] Write E2E test: Controls page routing strategy toggle works
- [x] Write E2E test: Providers page shows all providers including OpenRouter
- [x] Add `test:e2e` script to `package.json`
- [x] Add Playwright config (`playwright.config.ts`) pointing at dev server
- **Files:** new `ui/playwright.config.ts`, new `ui/e2e/*.spec.ts`, edit `ui/package.json`

---

## Priority Order (Recommended)

| Priority | Task | Impact | Effort |
|----------|------|--------|--------|
| P0 | A2: Activate Dead Code | High | Low |
| P0 | A1 + B2: OpenRouter | High | Medium |
| P0 | B1: Live Auth Monitor | High | Medium |
| P1 | A3 + B4: Routing Strategy | Medium | Medium |
| P1 | A4 + B3: Quota System | Medium | Medium |
| P1 | A6 + B6: Model Catalog | Medium | Medium |
| P2 | A5 + B5: Error Logs | Medium | Low |
| P2 | A7 + B7: Usage Analytics API | Medium | Medium |
| P2 | A9 + B8: WebSocket | High | High |
| P2 | C4: Playwright E2E Tests | Medium | Medium |
| P3 | C1: Provider Expansion | Low | Low |
| P3 | C2: Thinking Config | Low | Medium |
| P3 | C3: Auth Hardening | Medium | Low |
