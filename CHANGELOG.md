# Changelog

All notable changes to the Agent Rate Limit Gateway.

## [2026-05-19] - Cache-Aware Privacy Masking + 429 Fix

### Problem
Privacy masking detected secrets in user content (e.g. `client_secret=...`) and masked them to `[[ENV_SECRET_1]]`. A privacy instruction prompt was **prepended** to the system prompt so Claude would preserve `[[TYPE_N]]` placeholders. This broke Anthropic's prompt cache because:

1. Requests WITH secrets: system = `[privacy_prompt, sys_block(cache_control)]`
2. Requests WITHOUT secrets: system = `[sys_block(cache_control)]`
3. Different prefix every time -> Anthropic cache miss every time
4. Full token billing per request -> rate limit exhaustion -> **429**

### Changes

#### handler/handler.go
- **Replaced** `injectSystemPrompt()` (PREPEND) with `appendPrivacyPrompt()` (APPEND)
- Privacy instruction now appended to END of system array, after cached blocks
- Anthropic cache prefix unchanged -> cache hit preserved
- New function `appendPrivacyPrompt()` at line ~2274

#### privacy/pipeline.go
- Added mask cache lookup/store in span goroutines (lines ~130-243)
- `SkipSystemBlocks` check moved before cache lookup to prevent cached masked system prompts from bypassing skip
- Pre-mask key snapshot captures only per-span placeholder entries for cache store
- Per-request `MaskContext` via `MergeExternal()` ensures unmasking works independently of cache TTL

#### privacy/maskcache.go (NEW)
- In-memory `MaskCache` with `sync.RWMutex` + `map[string]*cacheEntry`
- Cache key: `hex(sha256(text)[:8])`, TTL 5 minutes
- Background cleanup goroutine evicts expired entries every minute
- Stores: masked text, per-span placeholder mappings, changed flag

#### privacy/masking/context.go
- Added `MergeExternal(mapping map[string]string)` method
- Injects cached placeholders into per-request `MaskContext`
- Updates counters to prevent `NextPlaceholder()` collision

#### privacy/masking/stream.go
- Changed `strayUndefinedRe` from `\s*undefined\s*` to plain `undefined`
- `stripStrayUndefined` now preserves spacing with post-cleanup

#### Tests
- Updated `stream_test.go` and `stream_audit_test.go` expectations for whitespace-preserving behavior
- All privacy tests pass (`go test ./privacy/...`)

### Impact
- **Before**: `cache_read_input_tokens = 0` on every request with secrets -> 429 after few requests
- **After**: `cache_read_input_tokens > 0` -> system prompt cache hit -> no 429
- Privacy prompt cost: ~170 input tokens (~$0.0005/request) only when secrets detected

### Deployment
Only `arl-gateway` restart required:
```bash
DOCKER_DEFAULT_PLATFORM="" docker-compose build arl-gateway
docker-compose up -d arl-gateway
```

---

## [2026-05-17] - Streaming Unmask Bug Fixes

### Changes
- Fixed 8 streaming unmask bugs (placeholders leaking to client)
- Added `ProcessChunkJSON` for partial_json (tool call) unmasking
- Added GLM "undefined" fallback (3-phase budget-based replacement)
- Added `SanitizeGarbledOutput` masking-independent guard
- 2700+ tests for edge cases, fuzz, and random split scenarios

### Files
- `privacy/masking/stream.go` - StreamUnmasker with buffered chunking
- `privacy/pipeline.go` - Non-streaming undefined fallback
- `proxy/anthropic.go` - SSE event routing, flush on block stop

---

## [2026-05-15] - Claude OAuth Integration

### Changes
- Added Claude OAuth (PKCE) flow with Bearer token + `anthropic-beta: oauth-2025-04-20` header
- Profile metrics and email migration
- Per-account serialization via `sync.Map` semaphore (buffer=1)

### Files
- `handler/handler.go` - OAuth transparent passthrough, account semaphore
- `provider/resolver.go` - Claude OAuth provider resolution

---

## [2026-05-10] - GLM Mode Isolation

### Changes
- Provider-scoped routing (Z.AI features only for Z.AI provider, not global `GLM_MODE` flag)
- Vision auto-routing fixed (only Z.AI for Z.AI models)
- `filterUnsupportedContent` is now a no-op

### Files
- `provider/resolver.go` - Z.AI fallback scope
- `handler/handler.go` - Provider-based feature gates
