# Changelog

All notable changes to the Agent Rate Limit Gateway.

## [2026-05-27] - Fix Claude Code Edit Failures (P0/P1)

### Problem
Claude Code edit operations failing repeatedly through the gateway. Root causes: max_tokens capped too low (truncating extended thinking mid-tool_use), streaming errors masked as successful responses, excessive retry defaults causing cascading failures.

### Changes

#### handler/handler.go
- **Fix P0-1**: `modelMaxTokens` and `anthropicModelMaxTokens` for `claude-opus-4-7` and `claude-sonnet-4-6` raised from 64000 to 128000. Extended thinking requires higher output token limits; 64K truncated tool_use JSON mid-generation.
- **Fix P0-1**: `knownModels` ContextWindow for all Claude entries corrected from 64000 to 200000.
- **Fix P0-1**: `countCacheControlBlocks` now iterates `tools` array (previously only counted `system` + `messages`). This caused Anthropic 400 errors when client sent cache_control on tool definitions.
- **Fix P0-1**: `clampCacheControlBlocks` gained Pass 4 to strip cache_control from tools as last resort.

#### tokenizer/optimizer.go
- `KnownModels` MaxOutputTokens for Claude opus/sonnet raised from 64000 to 128000.

#### proxy/anthropic.go
- **Fix P0-2**: Scanner error now emits `event: error` SSE event before `message_stop`, instead of silently faking `end_turn`. Returns error to caller for proper logging. Prevents Claude Code from processing incomplete tool_use JSON as valid.
- **Fix P1-3**: Default `ResponseHeaderTimeout` reduced from 4 hours to 60 seconds (in shared_transport.go).

#### proxy/anthropic_test.go
- `TestGracefulStreamCloseOnScannerError` updated to expect error return and verify `event: error` is emitted.

#### config/config.go
- **Fix P1-3**: Default `UpstreamMaxRetries` reduced from 20 to 5 (max attempts 9 instead of 31).
- **Fix P1-3**: Default `TransientRetryMax` reduced from 10 to 3.

#### toolcomp/toolcomp.go
- **Fix P1-2**: Default `TOOLCOMP_MAX_LINES` raised from 50 to 200. Diff compression no longer strips critical context lines needed for accurate edits.

#### handler/handler_test.go
- Added 11 new test cases covering tools cache_control counting, Pass 4 stripping, exact bug scenario, and ensureToolsCacheControl behavior.

### Risk Assessment
- All changes have env var overrides (TOOLCOMP_MAX_LINES, UPSTREAM_MAX_RETRIES, TRANSIENT_RETRY_MAX, etc.)
- No breaking API changes
- Claude models genuinely support 128K output tokens per Anthropic docs

## [2026-05-20] - Concurrent Request Optimization for Z.AI/GLM Mode

### Problem
Multi-agent workloads via Z.AI (GLM mode) serialized to 1 concurrent request per profile. 10 agents on same profile = queued 1-by-1 despite upstream having no per-account rate limit.

### Changes

#### handler/handler.go
- **ลบ** `acquireAccountSem()` call ออกจาก default format proxy path — เดิมใช้ `make(chan struct{}, 1)` buffer=1 ต่อ profile/apiKey ทำให้ request เดียวได้ 1 concurrent. ตอนนี้ให้ adaptive limiter เป็นตัวคุม concurrency แทน

#### proxy/key_pool.go
- **Rewrite** KeyPool จาก `sync.Mutex` + `[]int64` sliding window เป็น lock-free atomic hot path:
  - `keyEntry.count` → `atomic.Int64` (RPM counter, CAS reset on window rollover)
  - `keyEntry.windowStart` → `atomic.Int64` (window boundary)
  - `keyEntry.cooldownUntil` → `atomic.Int64`
  - Round-robin cursor → `atomic.Int64`
  - `Acquire()` happy path = zero mutex contention
  - Mutex ใช้เฉพาะ cold path (รอ cooldown expiry จาก 429)

### Impact
- **ก่อนแก้**: agent 10 ตัว → serialize ผ่าน account sem (buffer=1) + KeyPool mutex → 1 concurrent/profile
- **หลังแก้**: agent 10 ตัว → concurrent ทั้งหมด, adaptive limiter คุม, KeyPool zero-lock on happy path

## [2026-05-20] - Fix cache_control 4-Block Limit Exceeded (400 Error)

### Problem
Claude Code ส่ง request มาพร้อม `cache_control: {"type": "ephemeral"}` บนหลาย block (system + messages) แล้ว gateway เพิ่ม cache breakpoint เพิ่มเติมผ่าน `injectCacheBreakpoints()` โดยไม่เช็คจำนวนรวม ทำให้ total blocks with `cache_control` เกิน Anthropic API hard limit ที่ 4 blocks:
```
API Error: 400 A maximum of 4 blocks with cache_control may be provided. Found 5.
```

### Changes

#### handler/handler.go
- **เพิ่ม** `clampCacheControlBlocks()` - นับ cache_control blocks ทั้งหมดใน payload (system + messages) และ strip ส่วนเกินจาก message block เก่าสุดก่อน (preserve system cache anchors) สำรอง strip จาก system block ท้ายสุด
- **แก้ไข** `injectCacheBreakpoints()` - เพิ่ม budget check: นับ existing cache_control blocks ก่อน inject ใหม่ จะ inject ได้สูงสุด `4 - existing` เท่านั้น
- **เพิ่ม** `countCacheControlBlocks()` - helper นับ cache_control blocks ทั้ง payload
- Pipeline order: clamp ก่อน (strip ส่วนเกินจาก client) -> แล้วค่อย inject breakpoints (ตาม budget ที่เหลือ)

#### handler/handler_test.go
- `TestClampCacheControlBlocks_NoExcess` - ไม่ strip เมื่อจำนวนไม่เกิน 4
- `TestClampCacheControlBlocks_StripsExcessFromMessages` - strip จาก oldest message block ก่อน, preserve system blocks
- `TestInjectCacheBreakpoints_RespectsBudget` - inject ได้สูงสุด 4 - existing (เช่น มีอยู่ 3 = inject ได้ 1)
- `TestInjectCacheBreakpoints_NoBudget_NoInjection` - มีอยู่ 4 = inject 0

### Impact
- **ก่อนแก้**: Claude Code ส่ง 4 cache_control blocks + gateway เพิ่ม 1 = 5 = 400 error
- **หลังแก้**: clamp เหลือ 4 ก่อน -> inject เฉพาะถ้ายังมี budget -> ไม่เกิน 4

### Affected Providers
ทุก provider ที่ใช้ FormatAnthropic (anthropic, claude-oauth) ที่ gateway ต้องจัดการ cache_control
Z.AI ไม่ได้รับผลกระทบเพราะ `filterUnsupportedContent` strip cache_control ออกหมดอยู่แล้ว

---

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
