# 10 - OAuth, Metrics & Improvement Ecosystem

Comprehensive reference for the OAuth integration layer, Prometheus metrics system, token optimization improvements, and their integration points within the Agent Rate Limit Gateway.

---

## Table of Contents

1. [OAuth System](#1-oauth-system)
2. [Improvements & Experiments](#2-improvements--experiments)
3. [Metrics System](#3-metrics-system)
4. [Static Assets](#4-static-assets)
5. [Integration Points](#5-integration-points)
6. [Architecture Diagrams](#6-architecture-diagrams)

---

## 1. OAuth System

### 1.1 Overview

The gateway supports Claude OAuth (PKCE flow) as a first-class auth method alongside API key auth. Claude Code 2.1.123+ uses OAuth Bearer tokens obtained via `platform.claude.com` for all API calls, including LLM completions, MCP server initialization, and account management.

### 1.2 Directory Structure

```
oauth/
  ProxyPilot/       (placeholder - empty)
  ccproxy-api/      (placeholder - empty)
  ccs/              (placeholder - empty)
```

The `oauth/` directory contains placeholder subdirectories for planned proxy tooling. Actual OAuth implementation lives in the gateway's handler layer (see [docs/spec/02-handler-routing.md](../spec/02-handler-routing.md)).

### 1.3 Claude Code OAuth PKCE Flow

Claude Code uses a 4-phase startup sequence observed via mitmproxy capture (session ID: `04fa8597-8c70-4a57-b32a-2d73d27310e7`, version 2.1.123).

#### Phase 1: Health Checks

| # | Method | Host | Path | Status |
|---|--------|------|------|--------|
| 0 | GET | api.anthropic.com | /api/hello | 200 |
| 1 | GET | platform.claude.com | /v1/oauth/hello | 200 |

#### Phase 2: OAuth Token Exchange

| # | Method | Host | Path | Status |
|---|--------|------|------|--------|
| 2 | POST | platform.claude.com | /v1/oauth/token | 200 |
| 3 | GET | api.anthropic.com | /api/oauth/profile | 200 |
| 4 | GET | api.anthropic.com | /api/oauth/claude_cli/roles | 200 |
| 5 | GET | api.anthropic.com | /v1/mcp_servers?limit=1000 | 404 |
| 6 | GET | api.anthropic.com | /api/claude_code/settings | 200/404 |
| 7 | GET | api.anthropic.com | /api/claude_code/policy_limits | 200 |

#### Phase 3: MCP Server Initialization

| # | Method | Host | Path | Status |
|---|--------|------|------|--------|
| 8-24 | POST | mcp-proxy.anthropic.com | /v1/mcp/{server_id} | Mixed |

11 MCP servers initialized. Most return 401 (unauthorized) or 502 (bad gateway). Mermaid Chart MCP server succeeds (200).

#### Phase 4: LLM Completion

| # | Method | Host | Path | Status |
|---|--------|------|------|--------|
| 26 | POST | api.anthropic.com | /v1/messages?beta=true | 200 |
| 27 | POST | api.anthropic.com | /v1/messages?beta=true | 200 (streaming) |

### 1.4 Token Exchange Details

**Request** (POST `/v1/oauth/token`):

```json
{
  "grant_type": "authorization_code",
  "code": "<authorization_code>",
  "redirect_uri": "http://localhost:53718/callback",
  "client_id": "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
  "code_verifier": "<PKCE_code_verifier>",
  "state": "<state_param>"
}
```

**Response** (expected):

```json
{
  "access_token": "sk-ant-oatk-...",
  "refresh_token": "rt-...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "scope": "openid profile email"
}
```

**Key constants**:
- Client ID: `9d1c250a-e61b-44d9-88ed-5944d1962f5e` (Claude Code desktop)
- Token prefix: `sk-ant-oatk-` (OAuth access token)
- Token TTL: 3600 seconds (1 hour)
- Redirect: localhost ephemeral port

### 1.5 Authentication Headers

Three distinct User-Agent strings are used across the flow:

| Phase | User-Agent | Additional Headers |
|-------|-----------|-------------------|
| Auth (profile, roles) | `axios/1.13.6` | None |
| Settings/policy | `claude-code/2.1.123` | `anthropic-beta: oauth-2025-04-20` |
| Messages | `claude-cli/2.1.123 (external, cli)` | Extensive beta list |

**Messages beta header**:
```
anthropic-beta: claude-code-20250219,interleaved-thinking-2025-05-14,
  redact-thinking-2026-02-12,context-management-2025-06-27,
  prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,
  effort-2025-11-24
```

**SDK metadata** (X-Stainless headers):
```
X-Stainless-Package-Version: 0.81.0
X-Stainless-Runtime: node
X-Stainless-Runtime-Version: v24.3.0
X-Stainless-OS: MacOS
X-Stainless-Arch: arm64
```

### 1.6 Bearer Token Usage

The same OAuth Bearer token is used across all authenticated endpoints:

- `/api/oauth/profile` - account info, subscription tier
- `/api/oauth/claude_cli/roles` - model access, permissions
- `/v1/mcp_servers` - MCP server list
- `/api/claude_code/settings` - client settings
- `/api/claude_code/policy_limits` - rate limits, quota
- MCP server calls - includes `X-Mcp-Client-Session-Id`
- `/v1/messages` - LLM completions

### 1.7 MCP Server Protocol

MCP servers use JSON-RPC 2.0 over SSE:

**Initialize request**:
```json
{
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-11-25",
    "capabilities": { "roots": {}, "elicitation": {} },
    "clientInfo": {
      "name": "claude-code",
      "title": "Claude Code",
      "version": "2.1.123",
      "description": "Anthropic's agentic coding tool",
      "websiteUrl": "https://claude.com/claude-code"
    }
  },
  "jsonrpc": "2.0",
  "id": 0
}
```

**Error response** (401):
```json
{
  "type": "error",
  "error": {
    "type": "authentication_error",
    "message": "MCP server requires authentication but no OAuth token is configured.",
    "details": { "error_code": "mcp_unauthorized_no_token" }
  }
}
```

### 1.8 Gateway Proxy Requirements

Endpoints the gateway must proxy for Claude Code compatibility:

| Endpoint | Priority | Failure Impact |
|----------|----------|---------------|
| `/v1/oauth/token` | Critical | No Bearer token = all calls fail |
| `/api/oauth/profile` | High | Claude Code may refuse to proceed |
| `/api/oauth/claude_cli/roles` | High | Model access unknown |
| `/api/claude_code/policy_limits` | Medium | Client-side quota awareness lost |
| `/api/claude_code/settings` | Low | Returns 404, client uses defaults |
| `/v1/messages?beta=true` | Critical | No LLM completions |

**Gateway implications**:
1. Token refresh must work through the gateway (tokens expire in 1 hour)
2. `Authorization: Bearer` header must be forwarded unchanged
3. All `anthropic-beta` headers must be forwarded unchanged
4. `policy_limits` absence means client cannot self-throttle; gateway must enforce
5. MCP server auth errors (401) are non-blocking; Claude Code continues

### 1.9 Billing Path Routing (Claude OAuth)

The gateway routes Claude OAuth requests through distinct billing paths:

| Path | Description |
|------|-------------|
| `go_direct` | Direct proxy to Anthropic API |
| `sidecar` | Via Claude OAuth sidecar service |
| `direct` | Direct connection without billing intermediary |
| `billing_rejected` | Billing check failed; request blocked |

Each billing path is tracked via `api_gateway_billing_path_requests_total{path}` and `api_gateway_billing_path_latency_seconds{path}` metrics.

---

## 2. Improvements & Experiments

The `improvements/` directory contains 7 independent projects exploring different approaches to token optimization and context management for AI coding agents. These are reference implementations and research tools, not integrated into the gateway runtime.

### 2.1 context-mode (Node.js MCP Server)

**Path**: `improvements/context-mode/`
**License**: ELv2 (source-available)
**Approach**: Context window protection via sandboxed execution

**Core concept**: Raw tool output floods context. Context-mode intercepts tool calls and executes them in sandboxed subprocesses, returning only compact results.

**6 core tools**:
| Tool | Purpose | Context Saved |
|------|---------|---------------|
| `ctx_batch_execute` | Run multiple commands + queries in ONE call | 986 KB -> 62 KB |
| `ctx_execute` | Run code in 11 languages; only stdout enters context | 56 KB -> 299 B |
| `ctx_execute_file` | Process files in sandbox; raw content never leaves | 45 KB -> 155 B |
| `ctx_index` | Chunk markdown into FTS5 with BM25 ranking | 60 KB -> 40 B |
| `ctx_search` | Query indexed content; multiple queries per call | On-demand |
| `ctx_fetch_and_index` | Fetch URL, chunk, index; 24h TTL cache | 60 KB -> 40 B |

**Session continuity**: 5 hook types track all tool calls, user decisions, git ops, errors, and tasks in SQLite. On compaction, a priority-tiered XML snapshot (<=2 KB) is built. After compaction, a Session Guide with 15 categories is injected.

**Platform support**: 12+ platforms (Claude Code, Gemini CLI, VS Code Copilot, Cursor, OpenCode, KiloCode, OpenClaw, Codex CLI, Antigravity, Kiro, Zed, Pi Coding Agent).

**Search**: Dual-strategy RRF (Reciprocal Rank Fusion) with Porter stemming + trigram substring matching, proximity reranking, and Levenshtein fuzzy correction.

**SQLite backend**: Auto-selected at runtime: `bun:sqlite` on Bun, `node:sqlite` on Node.js >= 22.13, `better-sqlite3` elsewhere.

### 2.2 token-savior (Python MCP Server)

**Path**: `improvements/token-savior/`
**Version**: 2.6.0
**Tools**: 105
**Tests**: 1318/1318
**Approach**: Structural code navigation + persistent memory engine

**Core concept**: Index codebase by symbol (functions, classes, imports, call graph) so the model navigates by pointer instead of reading whole files.

**Memory engine**: SQLite WAL + FTS5 + sqlite-vec for vector embeddings. 3-layer progressive disclosure:
1. `memory_index` - shortlist (~15 tokens/result)
2. `memory_search` - filtered search
3. `memory_get` - full retrieval

**Features**:
- Bayesian validity scoring
- Contradiction detection at save time
- Observation decay with explicit TTLs
- ROI tracking per observation
- MDL distillation for knowledge compression

**Benchmark**: 100% score (180/180) on tsbench vs 78.3% baseline. 48% active token reduction, 79% wall time reduction.

**Token savings**:
| Operation | Reduction |
|-----------|-----------|
| `find_symbol("send_message")` | -99.9% (41M chars -> 67 chars) |
| `get_backward_slice(var, line)` | -92% (130 lines -> 12 lines) |
| 60-task tsbench | -85% (1.43M chars -> 216K chars) |

**Profiles**: full, core, nav, lean, ultra.

### 2.3 token-optimizer (Python Claude Code Plugin)

**Path**: `improvements/token-optimizer/`
**Version**: 5.5.0
**Dependencies**: Zero
**License**: PolyForm Noncommercial
**Approach**: Quality scoring + compaction survival + active compression

**Core concept**: Covers the full token problem (85% of context waste), not just command output. Tracks quality degradation through compaction and provides 5 active compression features.

**7-signal quality scoring**:
1. Token count trend
2. Task completion rate
3. Error frequency
4. Context utilization
5. Response coherence
6. File modification accuracy
7. User correction rate

**5 active compression features**:
1. Quality nudges - proactive suggestions to maintain quality
2. Loop detection - identify and break repetitive tool-call patterns
3. Delta mode - only send changes, not full content
4. Structure map - replace verbose descriptions with compact tree structures
5. Bash compression - compress shell output before context injection

**Smart compaction**: Progressive checkpoints before compaction. Live HTML dashboard at `localhost:24842` showing tokens, dollars, and turns per session.

**Additional tools**: Fleet auditor, memory health review.

### 2.4 token-optimizer-mcp (TypeScript MCP Server)

**Path**: `improvements/token-optimizer-mcp/`
**Version**: 5.0.1
**Tools**: 65
**Approach**: Smart tool replacements with caching + compression

**Core concept**: Replace standard tools (Read, Grep, Glob) with optimized alternatives that compress content via Brotli and cache in SQLite.

**7 tool categories**:
1. Smart file operations
2. API caching
3. Build/test helpers
4. Advanced caching
5. Monitoring
6. System operations
7. Token analytics

**Compression**: Brotli (2-4x typical, up to 82x for repetitive content).

**Persistence**: SQLite database with tiktoken for precise token counting.

**Hooks**: 7-phase global hooks system. Auto-detects and configures Claude Desktop, Cursor, Cline, etc.

**Production results**: 60-90% token reduction across 38,000+ operations.

### 2.5 claude-token-optimizer (Bash Script)

**Path**: `improvements/claude-token-optimizer/`
**Version**: 1.4.0
**License**: MIT
**Approach**: Document restructuring for minimal startup token usage

**Core concept**: Restructure project docs so Claude only loads essentials at startup, making everything else available on demand.

**Structure created**:
```
project/
  CLAUDE.md                    # 4 essential files (~800 tokens vs 11K)
  .claudeignore                # Prevent old docs from auto-loading
  .claude/
    COMMON_MISTAKES.md         # Top 5 critical bugs
    QUICK_START.md             # Common commands
    ARCHITECTURE_MAP.md        # Where things are
    completions/               # Task history (0 tokens)
    sessions/                  # Old work (0 tokens)
  docs/
    INDEX.md
    learnings/                 # Topic-based, load as needed
    archive/                   # Old docs (0 tokens)
```

**Framework patterns**: Supports 9 frameworks (React, Next.js, Vue, Svelte, Angular, Express, FastAPI, Django, Rails).

### 2.6 claude-context (Zilliz MCP Plugin)

**Path**: `improvements/claude-context/`
**License**: MIT
**Approach**: Semantic code search via vector database

**Core concept**: Index codebase into Zilliz Cloud (Milvus) using AST-based code chunking. Hybrid search (BM25 + dense vectors) retrieves relevant code from millions of lines.

**Embedding providers**: OpenAI, VoyageAI, Ollama, Gemini.

**Indexing**: AST-based code chunking with Merkle trees for incremental indexing. Only changed files re-indexed.

**Token reduction**: ~40% by loading only relevant code instead of entire directories.

**Components**:
- `@zilliz/claude-context-core` - Core indexing and search library
- `@zilliz/claude-context-mcp` - MCP server integration
- VS Code extension available on marketplace

### 2.7 caveman (Claude Code Skill/Plugin)

**Path**: `improvements/caveman/`
**License**: MIT
**Approach**: Compressed output style (~75% fewer output tokens)

**Core concept**: Make the agent respond in terse, caveman-style prose. Same technical accuracy, dramatically fewer output tokens.

**6 intensity levels**:
| Level | Description |
|-------|-------------|
| `lite` | Mild compression, mostly natural language |
| `full` | Default; ~65-75% output token reduction |
| `ultra` | Maximum compression |
| `wenyan-lite` | Classical Chinese lite |
| `wenyan-full` | Classical Chinese full |
| `wenyan-ultra` | Classical Chinese maximum |

**Sub-skills**:
- `caveman-commit` - Conventional Commits, <=50 char subject
- `caveman-review` - One-line comments: `L<line>: <severity> <problem>. <fix>.`
- `caveman-compress` - Compress existing prose to caveman style (~46% input reduction)

**Auto-clarity rule**: Drops to normal prose for security warnings, irreversible action confirmations, and when user appears confused.

**Hook system**: Three hooks (SessionStart, UserPromptSubmit, statusline) communicate via flag file at `$CLAUDE_CONFIG_DIR/.caveman-active`. SessionStart injects ruleset as hidden stdout.

**Agent distribution**: Claude Code (plugin + hooks), Codex (plugin), Gemini CLI (extension), Cursor/Windsurf (always-on rules), Cline (.clinerules), Copilot (.github), 40+ others via `npx skills`.

### 2.8 Comparison Matrix

| Project | Language | Mechanism | Input Savings | Output Savings | Persistence | Session Continuity |
|---------|----------|-----------|:---:|:---:|:---:|:---:|
| context-mode | Node.js | Sandbox execution | 94-99% | N/A | SQLite FTS5 | Full (5 hook types) |
| token-savior | Python | Symbol navigation + memory | 85-99% | N/A | SQLite WAL + vec | 3-layer disclosure |
| token-optimizer | Python | Quality scoring + compression | 60-85% | N/A | Local | Progressive checkpoints |
| token-optimizer-mcp | TypeScript | Tool replacement + Brotli | 60-90% | N/A | SQLite | 7-phase hooks |
| claude-token-optimizer | Bash | Doc restructuring | 88% | N/A | File system | N/A |
| claude-context | Node.js | Vector semantic search | ~40% | N/A | Zilliz Cloud | N/A |
| caveman | JS/Markdown | Output style compression | N/A | ~75% | N/A | N/A |

---

## 3. Metrics System

### 3.1 Overview

The gateway exposes 42+ Prometheus metrics via a dedicated registry (not the default global registry). Metrics are defined in `api-gateway/metrics/metrics.go` and validated against Grafana dashboards in `dashboard_test.go`.

### 3.2 Metrics Registry

```go
type Metrics struct {
    registry prometheus.Registerer
    // ... 37+ instrument fields
}

func New(pricing map[string]ModelPricing) *Metrics
```

The `New()` constructor accepts a model pricing map and registers all instruments with a custom `prometheus.Registerer`. A separate handler at `/metrics` serves the custom registry.

### 3.3 Complete Metric Catalog

#### Request & Latency

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_request_latency_seconds` | Histogram | method, path, status | Per-request latency (seconds) |
| `api_gateway_ttfb_seconds` | Histogram | model | Time-to-first-byte per model |
| `api_gateway_active_connections` | Gauge | (none) | Currently active proxy connections |
| `api_gateway_queue_depth` | GaugeFunc | (none) | Queue depth (callback on scrape) |

#### Errors & Rate Limiting

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_error_total` | Counter | type | Error count by type |
| `api_gateway_rate_limit_hits_total` | Counter | key | Rate limit hits by key |
| `api_gateway_upstream_retries_total` | Counter | (none) | Upstream retry count |
| `api_gateway_upstream_429_total` | Counter | (none) | Upstream 429 (rate limited) count |
| `api_gateway_transient_retry_total` | Counter | status, model | Transient error retries |
| `api_gateway_model_fallback_total` | Counter | requested, selected | Model fallback count |

#### Tokens & Cost

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_token_input_total` | Counter | model | Input tokens consumed |
| `api_gateway_token_output_total` | Counter | model | Output tokens consumed |
| `api_gateway_cost_total` | Counter | model, type | Estimated cost (USD) |
| `api_gateway_cost_savings_total` | Counter | (none) | Cost savings from optimization |

#### Adaptive Concurrency

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_adaptive_limit` | Gauge | model | Current adaptive concurrency limit |
| `api_gateway_adaptive_in_flight` | Gauge | model | Currently in-flight requests per model |

#### Profile & Account

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_profile_requests_total` | Counter | profile, model | Requests per routing profile |
| `api_gateway_profile_token_input_total` | Counter | profile, model | Input tokens per profile |
| `api_gateway_profile_token_output_total` | Counter | profile, model | Output tokens per profile |
| `api_gateway_profile_cost_total` | Counter | profile, model, type | Cost per profile |
| `api_gateway_account_token_input_total` | Counter | account_id, model | Input tokens per account |
| `api_gateway_account_token_output_total` | Counter | account_id, model | Output tokens per account |

#### Billing Path

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_billing_path_requests_total` | Counter | path | Requests per billing path |
| `api_gateway_billing_path_latency_seconds` | Histogram | path | Latency per billing path |

#### Optimizer

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_optimizer_chars_saved_total` | Counter | technique | Characters saved per technique |
| `api_gateway_optimizer_runs_total` | Counter | technique | Optimizer runs per technique |
| `api_gateway_optimizer_duration_seconds` | Histogram | technique | Optimizer duration per technique |
| `api_gateway_optimizer_tokens_saved_total` | Counter | (none) | Total tokens saved |
| `api_gateway_budget_level` | Gauge | model | Current budget level (0-1) |

#### PasteGuard (Privacy)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_mask_duration_seconds` | Histogram | phase | Masking duration per phase |
| `api_gateway_secrets_detected_total` | Counter | type | Secrets detected by type |
| `api_gateway_pii_detected_total` | Counter | type | PII detected by type |
| `api_gateway_mask_requests_total` | Counter | has_secrets, has_pii | Requests with masking applied |

#### Waste Detection

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_waste_findings_total` | Counter | detector, severity | Waste findings |
| `api_gateway_waste_tokens_wasted_total` | Counter | detector | Tokens wasted |

#### Anomaly Detection

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_anomaly_total` | Counter | type, severity | Anomaly events |
| `api_gateway_context_truncation_total` | Counter | model | Context truncation events |

#### Go Runtime

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_go_goroutines` | Gauge | (none) | Goroutine count |
| `api_gateway_go_heap_alloc_bytes` | Gauge | (none) | Heap allocation |
| `api_gateway_go_heap_objects` | Gauge | (none) | Heap object count |
| `api_gateway_go_gc_pause_ns` | Histogram | (none) | GC pause duration |
| `api_gateway_go_stack_inuse_bytes` | Gauge | (none) | Stack in-use bytes |

#### Infrastructure

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_dragonfly_up` | Gauge | (none) | Dragonfly (Redis) health (0/1) |

### 3.4 Helper Methods

```go
func (m *Metrics) RecordTokens(model string, inputTokens, outputTokens int)
func (m *Metrics) RecordOptimization(technique string, charsSaved int, duration time.Duration)
func (m *Metrics) RecordBillingPath(path string, duration time.Duration)
func (m *Metrics) RecordTTFB(model string, duration time.Duration)
func (m *Metrics) RecordFallback(requested, selected string)
```

`RecordTokens` calculates cost using the pricing map. Unknown models log a warning and skip cost recording.

### 3.5 Agent Key Hashing

Agent keys are hashed to prevent Prometheus label cardinality explosion:

```go
// SHA1 first 8 hex chars, prefixed with "agent:"
hash := sha1.Sum([]byte(agentKey))
rateLimitKey := "agent:" + hex.EncodeToString(hash[:])[:8]
```

### 3.6 Middleware

The metrics package provides HTTP middleware for chi routers:

```go
func (m *Metrics) Middleware(next http.Handler) http.Handler
```

Tracks per-request latency, status codes, and active connection gauge. Active connections use atomic increment/decrement.

### 3.7 Mock Data Seeding

The `Metrics` struct includes a `SeedMockData()` method that populates all counters with realistic values for dashboard development and testing.

### 3.8 Dashboard Test Suite

`dashboard_test.go` validates the complete metrics-to-dashboard pipeline:

**4 test functions**:
1. `TestDashboardJSONValid` - Validates JSON structure of all dashboard files
2. `TestDashboardPromQLValidation` - Every `api_gateway_*` metric in PromQL is registered
3. `TestLabelValidation` - Label keys in PromQL match registered label sets
4. `TestNoMissingMetrics` - Every registered metric appears in at least one dashboard panel
5. `TestRegisteredMetricsComplete` - Canonical metric list matches `registeredMetrics` map

**Skip list** (metrics excluded from dashboard coverage check):
- `api_gateway_go_heap_objects` - internal GC diagnostic
- `api_gateway_go_stack_inuse_bytes` - internal runtime diagnostic
- `api_gateway_context_truncation_total` - recovery metric, no dedicated panel
- `api_gateway_transient_retry_total` - recovery metric, no dedicated panel

### 3.9 Additional MCP Metrics (registered in code, not in dashboard_test.go)

These metrics are registered in `metrics.go` but not in the canonical `registeredMetrics` map (they serve MCP-related subsystems):

| Metric | Description |
|--------|-------------|
| `api_gateway_mcp_calls_total` | MCP tool call count |
| `api_gateway_mcp_call_duration_seconds` | MCP call latency |
| `api_gateway_mcp_cache_hits_total` | MCP cache hit count |
| `api_gateway_mcp_cache_misses_total` | MCP cache miss count |
| `api_gateway_mcp_quota_usage` | MCP quota utilization |

---

## 4. Static Assets

### 4.1 Dashboard SPA

**Path**: `api-gateway/static/index.html`

React 19 Single Page Application served as the admin dashboard.

**Entry point**: Mounts on `<div id="root">`

**Bundle structure** (4 JS chunks + 1 CSS):
```html
<script src="/static/js/react-vendor.[hash].js"></script>   <!-- React 19 + deps -->
<script src="/static/js/radix-ui.[hash].js"></script>       <!-- Radix UI components -->
<script src="/static/js/charts.[hash].js"></script>         <!-- Chart library -->
<script src="/static/js/icons.[hash].js"></script>          <!-- Icon library -->
<link rel="stylesheet" href="/static/css/[hash].css">       <!-- Styles -->
<script src="/static/js/main.[hash].js"></script>           <!-- App entry -->
```

**Title**: "Agent Rate Limit - Dashboard"

### 4.2 Favicon

**Path**: `api-gateway/static/favicon.svg`

64x64 SVG icon. Purple gradient gauge design (indigo gradient, `#6366f1` to `#4f46e5`).

### 4.3 Serving

Static files are served by the Go gateway at `/static/*`. The dashboard SPA handles client-side routing. All bundles use content-hash filenames for cache busting.

---

## 5. Integration Points

### 5.1 Gateway <-> OAuth

```
Gateway Handler Layer
  |
  +-- Auth middleware detects Bearer token (sk-ant-oatk-...)
  |     |
  |     +-- Routes to Claude OAuth profile
  |     +-- Validates subscription tier from /api/oauth/profile
  |     +-- Enforces per-model rate limits from /api/claude_code/policy_limits
  |
  +-- Token refresh worker
  |     |
  |     +-- Calls refreshAll() on startup (before first ticker)
  |     +-- Periodic refresh via ticker (before token expiry)
  |     +-- Handles grant_type=refresh_token requests
  |
  +-- Billing path router
        |
        +-- go_direct: Direct to Anthropic API
        +-- sidecar: Via Claude OAuth sidecar
        +-- direct: No billing intermediary
        +-- billing_rejected: Block request
```

### 5.2 Gateway <-> Metrics

```
Request Lifecycle
  |
  +-- Middleware (metrics.Middleware)
  |     +-- Records request latency (histogram)
  |     +-- Tracks active connections (gauge)
  |     +-- Records status codes
  |
  +-- Handler
  |     +-- RecordTokens(model, input, output) -> cost calculation
  |     +-- RecordTTFB(model, duration)
  |     +-- RecordFallback(requested, selected)
  |     +-- RecordBillingPath(path, duration)
  |
  +-- Optimizer pipeline
  |     +-- RecordOptimization(technique, charsSaved, duration)
  |     +-- Update budget_level gauge
  |
  +-- PasteGuard
  |     +-- Record mask_duration_seconds
  |     +-- Increment secrets/pii counters
  |
  +-- Rate limiter
        +-- Increment rate_limit_hits
        +-- Update adaptive_limit / adaptive_in_flight
```

### 5.3 Gateway <-> Improvements

The improvements in `improvements/` are standalone projects, not integrated into the gateway runtime. However, the gateway's **13-stage optimizer pipeline** (implemented in Go) incorporates concepts from several:

| Gateway Stage | Improvement Concept | Origin |
|---------------|--------------------|--------|
| Chunker | Code chunking | claude-context (AST chunking) |
| Summarizer | Content summarization | context-mode (sandbox processing) |
| Delta encoding | Delta mode | token-optimizer (delta mode) |
| Sketch dedup | Deduplication | token-savior (contradiction detection) |
| Caveman compression | Prose compression | caveman (output style) |
| Intent filter | Intent-based filtering | context-mode (intent-driven filtering) |

### 5.4 Metrics <-> Grafana Dashboards

```
metrics.go (Go)
  |
  +-- Defines 42+ api_gateway_* metrics
  +-- Custom prometheus.Registerer
  +-- Served at /metrics endpoint
  |
  v
dashboard_test.go (Go)
  |
  +-- registeredMetrics map (canonical list)
  +-- Validates all dashboard JSON files
  +-- Checks PromQL expressions reference valid metrics
  +-- Verifies label keys match registered labels
  +-- Ensures every metric has dashboard coverage
  |
  v
grafana/provisioning/dashboards/*.json
  |
  +-- Pre-built Grafana dashboard definitions
  +-- PromQL queries referencing api_gateway_* metrics
  +-- Panels: latency, tokens, cost, rate limits, optimizer, etc.
```

### 5.5 Billing Path Metrics Flow

```
Request arrives with Bearer token
  |
  v
Billing path determination
  |
  +-- Record billing path start time
  |
  v
Proxy to selected path
  |
  v
Record billing_path_requests_total{path="go_direct|sidecar|direct|billing_rejected"}
Record billing_path_latency_seconds{path}
```

---

## 6. Architecture Diagrams

### 6.1 Claude Code OAuth Flow Through Gateway

```
Claude Code (Client)                    ARL Gateway                      Anthropic
====================                    ==========                      =========
                                       
User launches Claude Code               
  |                                    
  +-- GET /api/hello ------>|           |                             
  +-- GET /v1/oauth/hello -->|           |----> platform.claude.com    
  |                           |           |----> api.anthropic.com      
  |                           |<-- 200 ---|<---- 200                   
  |                           |                                       
  +-- POST /v1/oauth/token -->|           |----> platform.claude.com    
  |   (PKCE exchange)         |           |    grant_type=authorization_code
  |                           |<-- 200 ---|<---- {access_token, refresh_token}
  |                           |                                       
  +-- GET /api/oauth/profile->|           |----> api.anthropic.com      
  |   (Bearer token)          |<-- 200 ---|<---- {subscription, capabilities}
  |                           |                                       
  +-- GET /api/oauth/roles -->|           |----> api.anthropic.com      
  |                           |<-- 200 ---|<---- {roles, models, permissions}
  |                           |                                       
  +-- GET /api/claude_code/   |           |----> api.anthropic.com      
  |   policy_limits --------->|<-- 200 ---|<---- {rate_limits, quota}
  |                           |                                       
  +-- POST /v1/messages ------>|           |----> api.anthropic.com      
  |   (SSE streaming)         |<-- SSE ----|<---- SSE chunks             
  |   anthropic-beta: ...     |           |    (Claude Code beta flags) 
  |   x-app: cli              |           |                              
  |                           |                                       
  [session continues...]      |                                       
  |                           |                                       
  [1 hour later]              |                                       
  |                           |                                       
  Token expired               |                                       
  +-- POST /v1/oauth/token -->|           |----> platform.claude.com    
  |   (refresh_token)         |<-- 200 ---|<---- {new_access_token}     
  |                           |                                       
```

### 6.2 Metrics Collection Pipeline

```
                          Request
                             |
                             v
              +--------------------------+
              |   Metrics Middleware      |
              |   (chi HTTP handler)      |
              +--------------------------+
              | record start time         |
              | increment active_conn     |
              +-------------+------------+
                            |
                            v
              +--------------------------+
              |   Gateway Handler         |
              |                           |
              | 1. Auth (Bearer/API key)  |
              | 2. Profile resolution     |
              | 3. Rate limit check       |
              | 4. Billing path route     |
              | 5. Optimizer pipeline     |
              | 6. PasteGuard masking     |
              | 7. Proxy to provider      |
              +--------------------------+
                            |
              +-------------+------------+
              | record metrics:           |
              | - request_latency_seconds |
              | - token_input/output_total|
              | - cost_total              |
              | - ttfb_seconds            |
              | - billing_path_*          |
              | - optimizer_*             |
              | - mask_*                  |
              +-------------+------------+
                            |
                            v
              +--------------------------+
              |   Prometheus Registry     |
              |   (custom, not default)   |
              +--------------------------+
                            |
              +-------------+------------+
              |                           |
              v                           v
    /metrics endpoint            Grafana Dashboards
    (Prometheus scrape)          (via provisioning)
```

### 6.3 Improvement Ecosystem Integration

```
                    +-----------------------------------+
                    |     Agent Rate Limit Gateway       |
                    |     (Go, 13-stage optimizer)       |
                    +-----------------------------------+
                    |                                   |
  Token input  ---> |  Chunker -> Packer -> Summarizer  |
                    |  -> Delta -> Sketch -> Semantic    |
                    |  -> Disclosure -> Bandit -> etc.   |
                    |                                   |
                    +-----------------------------------+
                               | concepts from
          +--------------------+--------------------+
          |                    |                    |
          v                    v                    v
  +---------------+   +---------------+   +---------------+
  | context-mode  |   | token-savior  |   | caveman       |
  | (sandbox exec)|   | (symbol nav)  |   | (output style)|
  +---------------+   +---------------+   +---------------+
  | ctx_execute   |   | find_symbol   |   | lite/ultra    |
  | ctx_batch_exec|   | memory_search |   | wenyan modes  |
  | ctx_index     |   | backward_slice|   | auto-clarity  |
  | ctx_search    |   | memory_get    |   |               |
  +---------------+   +---------------+   +---------------+
          |                    |                    |
          +--------------------+--------------------+
                               |
          +--------------------+--------------------+
          |                    |                    |
          v                    v                    v
  +---------------+   +---------------+   +---------------+
  | token-optimizer|  | t-optimizer-mcp| | claude-context |
  | (quality score)|  | (Brotli cache) | | (vector search)|
  +---------------+   +---------------+   +---------------+
  | 7-signal QA   |   | 65 tools      |   | AST chunking  |
  | delta mode    |   | tiktoken      |   | BM25+dense    |
  | loop detect   |   | SQLite persist|   | Merkle index  |
  +---------------+   +---------------+   +---------------+
          |
          v
  +---------------+
  | claude-token- |
  | optimizer     |
  | (doc restruct)|
  +---------------+
  | 800 vs 11K    |
  | startup tokens|
  +---------------+
```

### 6.4 Billing Path Routing

```
                     Incoming Request
                     (Bearer token)
                            |
                            v
                  +-------------------+
                  |  Auth Middleware  |
                  |  Detect: sk-ant-  |
                  |  oatk- prefix?    |
                  +-------------------+
                     |            |
                   Yes           No
                     |            |
                     v            v
            +--------------+  +--------------+
            | OAuth Path   |  | API Key Path |
            +--------------+  +--------------+
            |              |
            v              |
   +------------------+    |
   | Billing Decision |    |
   +------------------+    |
     |    |    |    |     |
     v    v    v    v     |
   go_  side direct  bill  |
   dir  car  (no    ing   |
             bill)  _rej  |
                     ected |
                            |
                            v
                  +-------------------+
                  |  Provider Proxy   |
                  |  (upstream call)  |
                  +-------------------+
                            |
                            v
                  Record billing_path_*
                  requests_total
                  latency_seconds
```

### 6.5 Dashboard Test Validation Pipeline

```
  metrics.go                    dashboard_test.go              grafana/*.json
  ===========                    ==================              ============
  |                              |                               |
  | Registered metrics           | registeredMetrics map         |
  | with labels                  | (canonical list of 42)        |
  |                              |                               |
  +----------+                   +----------+                    |
             |                              |                    |
             +--------- both must match ----+                    |
                                            |                    |
                                            v                    |
                                  +----------------------+       |
                                  | Test #1: JSON Valid  |       |
                                  | panels/rows exist    |       |
                                  +----------------------+       |
                                            |                    |
                                            v                    |
                                  +----------------------+       |
                                  | Test #2: PromQL      |<------+
                                  | all api_gateway_*    |
                                  | metrics are known    |
                                  +----------------------+
                                            |
                                            v
                                  +----------------------+
                                  | Test #3: Labels      |
                                  | label keys match     |
                                  | registered label set |
                                  +----------------------+
                                            |
                                            v
                                  +----------------------+
                                  | Test #4: Coverage    |
                                  | every metric in at   |
                                  | least one dashboard  |
                                  +----------------------+
                                            |
                                            v
                                  +----------------------+
                                  | Test #5: Complete    |
                                  | canonical ==         |
                                  | registeredMetrics    |
                                  +----------------------+
```
