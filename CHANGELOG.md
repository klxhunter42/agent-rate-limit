# Changelog

All notable changes to the Agent Rate Limit Gateway.

## [2026-06-24] - Fix SSE tool_use JSON corruption (GLM "undefined" noise in input_json_delta)

### Problem
Claude Code over a streaming request reported "The model's tool call could not be parsed (retry also failed)" on GLM (glm-5.1 / glm-5.2) calls that carried masked secrets/PII and used a tool. The non-streaming path was already fixed in a prior change; the streaming `input_json_delta` path still corrupted tool input.

### Root cause
GLM sometimes emits the literal token `undefined` instead of preserving a `[[TYPE_N]]` placeholder (handled by the `glmNoiseMode` fallback). That fallback substituted the **unescaped** original value. In the `input_json_delta` (tool_use `partial_json`) path the client concatenates every delta then `JSON.parse`s it, so an original containing `"`, `\`, or a newline produced invalid JSON:
```
client --concat input_json_delta--> {"email":"a"b\c"}   <- invalid JSON -> parse error
                                          ^ unescaped original
```
Only the fallback (`undefined`) path was broken; the normal placeholder path already JSON-escaped via `RestorePlaceholdersJSON`.

### Fix
- privacy/masking/stream.go: `replaceUndefinedFallback` and `dedupAdjacentUndefined` gained a `jsonSafe bool` param; originals are `jsonEscape`d before substitution. `ProcessChunkJSON` (the tool_use path) calls with `jsonSafe=true`; `ProcessChunk` and `Flush` pass `false`.
- GLM + active-masking path only (the fallback runs when `glmNoiseMode && HasContexts()`); capable models (claude/gemini/openai) set noise mode off and are unaffected.

### Verification
- `go test ./privacy/masking/` clean. 3 new regression tests cover undefined-fallback with `"`+`\`, with a newline, and `undefined` split across two SSE chunks - all assert `json.Valid`.
- Module build OK.

## [2026-06-24b] - Stop baking/committing corporate proxy CA certs; build-time TLS relax

### Problem
Corporate TLS-intercepting proxy CA certs (Zscaler / mitmproxy, `*.pem`) were sitting untracked in the repo tree and were `COPY`ed into the ai-worker and rate-limiter images at build time. A `git add -A` would have committed a corporate root CA, and the cert was baked into an image layer.

### Fix
- Deleted `ai-worker/mitm-ca.pem`, `distributed-rate-limiter/mitm-ca.pem`, `docker/mitm-ca.pem`, `docker/zscaler-ca.pem`. Added `*.pem` to `.gitignore` and the `.dockerignore` files (root, distributed-rate-limiter, new ai-worker/.dockerignore) so a local CA can never be committed or copied into an image.
- ai-worker/Dockerfile: dropped build-time cert trust; pip now uses `--trusted-host` for pypi.org / files.pythonhosted.org / pypi.python.org.
- distributed-rate-limiter/Dockerfile: dropped the `keytool` cert import; Maven forces the Wagon transport (`-Dmaven.resolver.transport=wagon`) plus the wagon SSL-insecure flags (Maven 3.9 defaults to the Resolver transport, which ignores the wagon ssl flags).
- Both mirror api-gateway's existing build-time TLS-relax pattern (`GOINSECURE GIT_SSL_NO_VERIFY`). Runtime is unaffected - these services have no proxy config in compose.

### Verification
- `docker build` of both images succeeds with no cert present, behind the corporate proxy: ai-worker pip-installs all deps; rate-limiter `mvn package` builds the jar (42 MB) into the runtime stage.

## [2026-06-22b] - Strip z.ai Thinking Config (kills 20-75s extended thinking)

### Problem
After the stripper/TextComp fixes, Claude Code requests through the gateway still took 13-75s while the gateway itself finished its work in ~ms. Root cause: Claude Code sends `thinking:{budget_tokens:50000}`; the gateway forwarded it unchanged to z.ai, and glm honored it - burning 20-75s of extended thinking even for a one-word prompt ("Thought for 20s" on "หน้า"). Both glm-5.2 and glm-5.1 were affected (not model-specific). z.ai itself does not benefit from extended thinking (docs/26: "Z.AI has no extended thinking").

### Root cause diagram
```
Claude Code ──"หน้า"──> gateway ──[thinking:budget 50000]──> z.ai/glm-5.2
                                                            (honors thinking config)
                                                            generates 20-75s of thinking
                                  <-- stream thinking + answer ~~~~~~~~~~~ 20-75s total
gateway stages: resolver -> strip-think?NO -> textcomp -> upstream req  = ~ms (not the cause)
bottleneck = z.ai generation (model latency), identical gateway vs direct
```

### Fix
- handler/handler.go: zai-only block strips/caps `payload["thinking"]` before forwarding. `ZAIThinkingBudget<=0` (default) deletes the field entirely; `>0` caps `budget_tokens`. Logs "zai thinking stripped".
- config/config.go: `ZAIThinkingBudget` + `ZAI_THINKING_BUDGET` env (default 0).
- Gated on `isZAIProvider` (provider "zai") - claude-oauth untouched.

### Impact / measurement
- Trivial prompt: 20s (thinking) -> ~1-3s expected (generation only).
- Gateway vs direct connect/TTFB: gateway **0.14s vs direct 0.32s** (gateway reuses the TCP connection to z.ai; direct pays TLS handshake each call). The remaining 13-22s on some requests is z.ai/glm generation latency - identical for gateway and direct, not fixable at the gateway.

### Verification
- Deploy 6bd0b52 on 111; logs confirm "zai thinking stripped" on every glm request.
- `go test ./handler/ -race` clean.

## [2026-06-22] - Fix GLM Gateway Slowness vs Direct z.ai (stripper + TextComp), Add 529 Model Fallback

### Problem
Clients (Claude Code, streaming) perceived the gateway as very slow while hitting z.ai directly was fast. Two independent root causes both sat on the GLM/zai path and were invisible on the short 401 reject path - that asymmetry (401 fast, 200/stream slow) was the diagnostic signature. Separately, z.ai `529 overloaded_error` made the gateway retry on the same model up to ~9x instead of falling back.

### Root Causes
| ID | Severity | Root Cause | Fix |
|----|----------|-----------|-----|
| P1 | High | `toolUseStripper` (proxy/anthropic.go) was created for every glm-* model and withheld streamed text whenever a `<` prefixed a tracked tag (`<system`/`<thinking`/`<action`/`<details`/`<tool_call`...) with no close, releasing it only at a new tag or stream end. Code/prose full of `<` froze the stream | `STRIP_GLM_TOOL_XML` env (default **false**) gates stripper creation; 8KB buffer cap (`stripperMaxBuffer`) bounds it when enabled |
| P2 | High | TextComp ran serially per text block for zai and scaled with body size: ~73ms/300 blocks, +2.6s on an ~840KB request | Worker pool parallelizes Compress across `runtime.NumCPU` (3.3x on 8 cores); `TEXTCOMP_MAX_BODY_BYTES` (default 64KB) skips TextComp for larger bodies (speed-first) |
| P3 | Medium | z.ai 529 was treated as transient and retried on the same model+key up to ~9x with growing backoff | 529 now triggers `OnRateLimitError` -> `AdaptiveLimiter.SuggestFallbackModel` switches to a same-series sibling (glm-5.2->glm-5.1->...->glm-4.x) with the same BYOK key, `skipBackoff`; limiter `Feedback` now reduces the limit on 529 |

All three are **GLM/zai-only**: P1/P2/P3 gate on `isZAIProvider` (provider "zai") and/or `strings.HasPrefix(model,"glm-")`. The **claude-oauth path is untouched**.

### Changes
#### Streaming freeze fix (P1)
- proxy/anthropic.go: gate stripper creation behind `p.cfg.StripGLMToolXML` (nil-safe); add `stripperMaxBuffer` cap in `Feed()`.
- config/config.go: `StripGLMToolXML` field + `STRIP_GLM_TOOL_XML` env (default false).
#### Request-prep perf (P2)
- handler/handler.go: collect text slots (system + messages + content blocks), run `TextComp.Compress` across a bounded worker pool, write back sequentially (payload map is not concurrency-safe). Skip when `len(body) > TextCompMaxBodyBytes`.
- config/config.go: `TextCompMaxBodyBytes` + `TEXTCOMP_MAX_BODY_BYTES` env (default 64KB).
#### 529 model fallback (P3)
- proxy/anthropic.go: 529 enters the existing `OnRateLimitError` fallback machinery (was 429-only); streaming guard blocks model switch once SSE started; no-fallback 529 keeps same-model transient retry.
- middleware/adaptive_limiter.go: `Feedback` reduces limit on 529 (was 429/503); new `SuggestFallbackModel` (read-only, no slot acquire) picks a healthy same-series then lower-series sibling, skipping recent-529 and saturated models.
- handler/handler.go: `rotateAccountFn` returns a sibling-model override in GLM mode when no account/provider to rotate; per-request `tried` set prevents cycling.
- config/config.go: `OverloadModelFallback` + `OVERLOAD_MODEL_FALLBACK` env (default true).
#### Config
- config/config.go: default `UPSTREAM_MODEL_LIMITS` -> `glm-5.2:10` (was 5) and adds `glm-4.5:10`.
#### Tests
- proxy: `TestStreamerStripperBeforeAfter` (content token 83ms->42ms), `TestStripperWithholdsUnclosedTag`, `TestStreamWithTagsNotBuffered`, `TestStreamRelayFlushesImmediately`, `Test529OverloadModelFallbackParallel`.
- handler: `TestTextCompPrepBreakdown` (serial vs pool benchmark).

### Impact
- Large request (~570KB) gateway latency: **+2.6s slower than direct -> now equal/faster than direct** (gateway 0.7-0.9s vs direct ~0.95s on 111).
- GLM streamed responses no longer freeze on `<` tags; first content byte reaches the client in ~1ms.
- z.ai 529 overload now fails over to a sibling model in ~ms instead of stalling ~9 retries.

### Verification
- go build OK; `go test ./proxy/ ./handler/ -race` clean (no data races).
- A/B on 111 (`curl -d @file` gateway vs z.ai, dummy key): small/medium/large gateway == or < direct.
- Pre-existing unrelated failures `TestTruncateMessages`, `TestAllowedResponseHeaders` confirmed failing on clean main.

## [2026-06-18a] - Add GLM-5.2 Model, Sync Z.AI Pricing, Fix Edit-Tool TextComp Bug

### Problem
GLM-5.2 (new Z.AI flagship, 1M context) was missing from the gateway catalog, defaults, pricing, and dashboards. Separately, the Edit tool failed through the gateway: gofmt column-aligned whitespace in Go source was collapsed to single spaces before the model saw it, so old_string never matched.

### Root Causes
| ID | Severity | Root Cause | Fix |
|----|----------|-----------|-----|
| P1 | High | Z.AI request path ran TextComp.Compress on tool_result content; cleanup() collapses all multi-space runs (Phase 4, post-unmask), destroying gofmt alignment | Skip tool_result (and tool_use) blocks from TextComp in handler.go ZAI block |
| P2 | Medium | GLM catalog/defaults/dashboards predated glm-5.2; cost-calculator hardcoded glm-5.1|glm-5-turbo|glm-5 bucket at stale $1.0/$3.2 | Added glm-5.2 everywhere; split cost-calculator into per-price buckets |

### Changes
#### Models / pricing (synced from https://docs.z.ai/guides/overview/pricing)
- handler/handler.go: added glm-5.2 (1M ctx, $1.4/$4.4, flagship) + glm-5v-turbo, glm-4.6v-flashx, glm-4.6v-flash, glm-ocr to knownModels; ctx 5.1->200K, 4.6->200K; modelMaxTokens, modelFallbacks, isNativeImageModel, default model.
- config/config.go: DEFAULT_MODEL=glm-5.2; ModelLimits glm-5.2:5,glm-5.1:10; VisionLimits; ModelPriority glm-5.2:100; defaultModelPricing.
- middleware/adaptive_limiter.go: modelPriority glm-5.2:100 > glm-5.1:95.
- tokenizer/optimizer.go: KnownModels glm-5.2 (1M), glm-5.1 ctx 200K.
- handler/profile.go: glm-5.2 flagship.
#### Config / env / deploy
- .env.example: DEFAULT_MODEL, MODEL_PRIORITY; corrected inverted GLM_MODE comment (true=Z.AI active).
- helm/ai-gateway/values.yaml + docker-compose.yml: UPSTREAM_MODEL_LIMITS, VISION limits, MODEL_PRICING with glm-5.2.
#### Dashboards
- grafana/ + helm cost-calculator.json: added glm-5.2 ($1.4/$4.4); corrected glm-5.1 ($1.4/$4.4), glm-5-turbo ($1.2/$4.0), glm-5 ($1.0/$3.2) into separate price buckets.
#### Bug fix (Edit tool)
- handler/handler.go (~L1432): ZAI TextComp block now skips tool_result blocks (byte-exact content for Edit matching); removed dead field branch.
#### Docs / tests
- docs/providers.md, README.md: glm-5.2 in model tables.
- handler/handler_test.go: validateChatRequest defaults -> glm-5.2.

### Impact
- Before: glm-5.2 requests unrouted/unpriced; default model was glm-5; Edit tool failed on gofmt-aligned Go files via the gateway.
- After: glm-5.2 is the default/flagship with correct pricing and 1M context; Edit tool string matching works (tool_results preserved verbatim).

### Verification
- go build ./... OK; go vet OK; gofmt clean.
- handler/middleware/textcomp/provider/config tests pass.
- tokenizer.TestGetModelCapabilities fails pre-existing (Claude max-output-token expectation, unrelated; fails on clean HEAD).
- 14 cross-check agents confirmed catalog pricing, config consistency, code-path coverage, routing, fallback integrity, YAML validity, and dashboard GLM_MODE true/false behavior.

## [2026-05-27d] - Fix Tools Not Called via Claude OAuth CLI

### Problem
Claude Code CLI through gateway did not call tools (Read, Edit, Bash, Agent, etc.) while VSCode Claude Code panel worked fine. Both used same `arl_*` profile token targeting `claude-oauth` provider.

### Root Causes

| ID | Severity | Root Cause | File | Fix |
|----|----------|-----------|------|-----|
| P0 | Critical | Tool filter dropped ~10 of 25 tools from Claude OAuth requests. Filter activated at >15 tools, scored all tools 0 (empty recentText), kept only 4 AlwaysKeep + top 11 by description length | handler.go | Skip tool filter for `transparent` requests |
| P0 | Critical | Content extraction `mm["content"].(string)` failed for Claude Code array-format content blocks `[{"type":"text","text":"..."}]`, producing empty `recentText` | handler.go | Type switch handling both string and []any |
| P1 | High | `injectCachedTools` ran on transparent requests, mutating passthrough payload | handler.go | Guard with `!transparent` |
| P1 | High | `desctrim` ran on transparent requests, modifying tool descriptions in passthrough | handler.go | Guard with `!transparent` |

### Changes

#### handler/handler.go
- **Line 1115**: Added `!transparent` guard to `injectCachedTools` - tool cache injection skipped for transparent OAuth passthrough
- **Line 1225**: Added `!transparent` guard to `desctrim` - description trimming skipped for transparent OAuth passthrough
- **Line 1259**: Added `!transparent` guard to `toolfilter` - tool filtering skipped for transparent OAuth passthrough
- **Line 1273-1289**: Fixed content extraction to handle array-format content blocks (`[]any`) in addition to string content. Previously only `mm["content"].(string)` was handled, which silently failed for Claude Code's array content format, resulting in empty `recentText` and all tools scoring 0 in the filter.

### Why Transparent Requests Should Skip Optimizers

Transparent OAuth requests are passthrough to Anthropic API using the client's own token:
- No Z.AI token budget constraint - no need to save tokens
- Client chose its own tools - gateway should not remove them
- Anthropic supports up to 128 tools - 25 is well within limits
- Tool filter's heuristic (intent classification + keyword matching) can wrongly drop tools the client needs

### Impact
- **Before**: Claude Code CLI through gateway - tools dropped, model responds with text only
- **After**: All 25 tools preserved, model can use any tool it needs

### Verification
- Build: `go build ./...` - clean
- Tests: `go test ./handler/ -count=1` - pass
- Integration: 16-tool request through gateway returns `tool_use` content block (Bash)

## [2026-05-27c] - Fix Privacy Prompt Leaking Placeholder Details to User

### Problem
Gateway response contained text about `[[ENV_PASSWORD_XXXX]]` placeholder format and internal masking system. Claude was aware of the placeholder naming convention and discussed it with the user.

Root cause: `privacyPromptInjection` explicitly taught Claude the `[[TYPE_N]]` format with examples (`[[IP_ADDRESS_1]]`, `[[ENV_USER_3]]`), what "anonymized" means, and correct/wrong usage. Despite a final instruction to "never mention privacy, masking, placeholders, or anonymization", Claude had already learned the system and referenced it in responses.

Secondary issue: `leftoverPlaceholderRe` regex `\[\[[A-Z_]+_\d+\]\]` only matched numeric suffixes. If Claude generated a non-standard variant like `[[ENV_PASSWORD_XXXX]]`, the regex would not strip it.

### Changes

#### privacy/pipeline.go
- **Fix**: Rewrote `privacyPromptInjection` to be minimal - instructs Claude to preserve `[[...]]` tokens as opaque values without explaining what they represent or showing examples. Removed all format-specific examples, "anonymized" terminology, and Correct/Wrong demonstrations.

#### privacy/masking/stream.go
- **Fix**: Broadened `leftoverPlaceholderRe` from `\[\[[A-Z_]+_\d+\]\]` to `\[\[[A-Z][A-Z0-9_]*_[A-Za-z0-9]+\]\]`. Now catches hallucinated variants with letter suffixes (e.g. `[[ENV_PASSWORD_XXXX]]`) while avoiding false positives on bash `[[ ${var} ]]`.

### Impact
- **Before**: Claude mentions `[[ENV_PASSWORD_XXXX]]`, "anonymized", "placeholder" in responses
- **After**: Claude treats `[[...]]` tokens as opaque, preserves them for unmasking, never discusses them

### Verification
- Non-streaming test with secrets: response has no placeholder/masking references
- Streaming test with secrets: response has no placeholder/masking references
- All privacy tests pass (`go test ./privacy/...`)
- Handler tests pass (`go test ./handler/`)

## [2026-05-27b] - Fix "Content block is not a text block" Error

### Problem
Claude Code through gateway threw "Content block is not a text block" error when extended thinking was enabled. Root cause: unmasker flush emitted `text_delta` events with hardcoded `index:0` into thinking blocks.

### Changes

#### proxy/anthropic.go
- **Fix P0**: Unmasker flush at `content_block_stop` (line 2438), scanner error (line 2497), and stream end (line 2534) used hardcoded `index:0`. Now uses `lastRelayBlockIdx` to target the correct content block index.
- **Fix P0**: Added `lastRelayBlockIdx >= 0` guard to prevent flush before any block is tracked.
- **Fix P1**: `SanitizeGarbledOutput` in streaming relay (lines 2388, 2395, 2400, 2405) now gated behind `stripper != nil` (GLM models only). Previously ran on all providers including claude-oauth, risking corruption of thinking `signature` fields and `partial_json` in tool_use blocks.

### Impact
- **Before**: Extended thinking + tool_use through gateway -> "Content block is not a text block"
- **After**: Correct block index tracking, no corruption of Anthropic-native responses

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
