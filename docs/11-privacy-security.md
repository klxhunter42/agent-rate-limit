# Privacy and Security

## 1. PasteGuard PII Detection: Presidio-to-Regex Migration

### Why Presidio Was Replaced

The original PasteGuard PII pipeline used Microsoft Presidio Analyzer (NLP container) for entity detection. This caused severe latency problems:

| Issue                   | Detail                                                                                                         |
|-------------------------|----------------------------------------------------------------------------------------------------------------|
| **Slow HTTP calls**     | Each Presidio `/analyze` call took 7-14 seconds per text span                                                  |
| **Compounding latency** | Multiple spans per request = 30+ seconds total request time                                                    |
| **Overkill NLP**        | Only 2 entity types were used (`EMAIL_ADDRESS`, `PHONE_NUMBER`) - far too light to justify a 2GB NLP container |
| **Regex is faster**     | Compiled regex detection is <1ms vs 7-14s per Presidio call                                                    |
| **Container removed**   | Presidio container (2GB RAM) is no longer needed for default deployment                                        |

The replacement is `RegexDetector` -- a pure Go regex engine with zero external dependencies.

### RegexDetector Supported Entities

| Entity             | Pattern                             | Example                        |
|--------------------|-------------------------------------|--------------------------------|
| `EMAIL_ADDRESS`    | Standard email format               | `user@example.com`             |
| `PHONE_NUMBER`     | International phone numbers         | `+1-555-123-4567`              |
| `CREDIT_CARD`      | Visa / Mastercard / Amex / Discover | `4111-1111-1111-1111`          |
| `SSN`              | US Social Security Number           | `123-45-6789`                  |
| `IBAN`             | International Bank Account Number   | `GB82WEST12345698765432`       |
| `IP_ADDRESS`       | IPv4 addresses                      | `192.168.1.1`                  |
| `THAI_NATIONAL_ID` | Thai citizen ID (13 digits)         | `1-1001-00001-23-4`            |
| `THAI_PHONE`       | Thai phone format                   | `081-234-5678`, `+66812345678` |

Default: all 8 entities are enabled. Customize via `PASTEGUARD_PII_ENTITIES` env var.

### PasteGuard Secret Detection Entities

| Entity                | Pattern                                    | Example                               |
|-----------------------|--------------------------------------------|---------------------------------------|
| `OPENSSH_PRIVATE_KEY` | OpenSSH private key block                  | `-----BEGIN OPENSSH PRIVATE KEY-----` |
| `PEM_PRIVATE_KEY`     | RSA / PKCS8 / encrypted private key blocks | `-----BEGIN RSA PRIVATE KEY-----`     |
| `API_KEY_SK`          | `sk-` prefixed keys (20+ chars)            | `sk-abcdef...`                        |
| `API_KEY_AWS`         | AWS access key ID                          | `AKIAIOSFODNN7EXAMPLE`                |
| `API_KEY_GITHUB`      | GitHub PAT / token                         | `ghp_xBnf...`                         |
| `API_KEY_GITLAB`      | GitLab PAT / deploy token                  | `glpat-abcdef...`                     |
| `JWT_TOKEN`           | JWT format (3 base64 segments)             | `eyJhbG...`                           |
| `BEARER_TOKEN`        | Bearer auth header value (40+ chars)       | `Bearer abc123...`                    |
| `ENV_PASSWORD`        | `PASSWORD=` / `_PWD=` env vars             | `DB_PASSWORD=secret123`               |
| `ENV_SECRET`          | `_SECRET=` env vars                        | `API_SECRET=mysecret`                 |
| `CONNECTION_STRING`   | Database/queue connection URIs             | `postgresql://user:pass@host`         |
| `THAI_NATIONAL_ID`    | 13-digit Thai citizen ID (no dashes)       | `1100100000123`                       |

Default: 7 entities enabled (OPENSSH_PRIVATE_KEY, PEM_PRIVATE_KEY, API_KEY_SK, API_KEY_AWS, API_KEY_GITHUB, JWT_TOKEN, BEARER_TOKEN). The remaining 5 (API_KEY_GITLAB, ENV_PASSWORD, ENV_SECRET, CONNECTION_STRING, THAI_NATIONAL_ID) are available but not in the default set. Customize via `PASTEGUARD_SECRET_ENTITIES` env var.

### Mask Order

Secrets are masked first, then PII is applied on top. During unmask, secrets are restored first (innermost), then PII (outermost). This nesting prevents partial unmasking.

### Presidio Legacy (Optional)

The Presidio container is still available for legacy use but is not required:

```bash
# Start with Presidio (not recommended - use only if needed)
docker-compose --profile pii up

# Default deployment (regex only, no Presidio)
docker-compose up
```

### Files

| File                    | Purpose                                                      |
|-------------------------|--------------------------------------------------------------|
| `privacy/pii/detect.go` | `RegexDetector` -- regex pattern matching for all 8 entities |
| `privacy/pii/mask.go`   | PII masking with placeholder generation                      |
| `privacy/config.go`     | Env var loading, default config                              |
| `privacy/pipeline.go`   | Full mask/unmask pipeline (secrets + PII)                    |
| `privacy/masking/stream.go` | `StreamUnmasker` with `ProcessChunk` (text/thinking), `ProcessChunkJSON` (partial_json), undefined fallback (3-phase budget), cross-chunk undefined buffering, `SanitizeGarbledOutput` (masking-independent final guard) |
| `privacy/masking/stream_undefined_edge_test.go` | 20 edge case tests for undefined fallback |
| `privacy/masking/stream_undefined_weird_test.go` | 30+ weird edge cases (unicode, emoji, JSON, split positions) |
| `privacy/masking/stream_fuzz_test.go` | 1700 parametric fuzz tests |
| `privacy/masking/stream_unique_fuzz_test.go` | 99 unique + 900 random split tests |
| `privacy/pipeline.go` | Non-streaming undefined fallback (`replaceUndefinedNonStream`) |

---

## 2. PasteGuard Streaming Unmask -- Bug Fix Log

### Problem: `[[PERSON_N]]` leaking to client

Users saw `[[PERSON_2]]`, `[[PERSON_13]]` in responses instead of real names, because unmask step didn't work in some cases.

### Root Causes and Fixes

#### Bug #1 (HIGH) -- relayStreamWithTracking guard skipping ProcessChunk

**File:** `proxy/anthropic.go` -- `relayStreamWithTracking()`

**Cause:** Guard `strings.Contains(data, "[[")` prevented ProcessChunk when SSE line didn't have `[[`. When placeholder split across 2 chunks (e.g. chunk1=`[[PER`, chunk2=`SON_1]]`), chunk 2 didn't have `[[` so was skipped, buffer leaked placeholder.

**Fix:** Changed logic to process `content_block_delta` every chunk when unmasker is active, without checking `[[`

#### Bug #2 (HIGH) -- Flush output was discarded

**File:** `proxy/anthropic.go` -- `relayStreamWithTracking()`, `convertOpenAIStreamResponse()`

**Cause:** `unmasker.Flush()` returns buffered value but original code only logged it, didn't emit as SSE event. Placeholders stuck at end of stream disappeared (data loss).

**Fix:** Emit Flush result as `content_block_delta` SSE event before `content_block_stop`

#### Bug #3 (CRITICAL) -- ProxySidecar doesn't unmask at all

**File:** `proxy/anthropic.go` -- `ProxySidecar()`

**Cause:** Sidecar does raw byte relay, no SSE parsing, no maskResult parameter. Entire response sent directly to client including placeholders.

**Fix:** Added `maskResult` parameter + SSE line scanner + unmask logic like relayStreamWithTracking. Non-stream path: read full body, unmask, write.

#### Bug #4 (MEDIUM) -- Cross-block buffer contamination

**File:** `proxy/anthropic.go` -- `relayStreamWithTracking()`

**Cause:** ProcessChunk uses shared buffer between text/thinking blocks. If placeholder splits at block boundary, buffer leaks to next block.

**Fix:** Intercept `content_block_stop` event, flush buffer before relay stop event.

#### Bug #5 (LOW) -- gemini-codeassist emits empty text_delta

**File:** `proxy/gemini-codeassist.go` -- `streamResponse()`

**Cause:** No `if text == "" { continue }` after ProcessChunk. When unmasker buffers entire chunk, empty `text_delta` event is emitted.

**Fix:** Added empty text guard after ProcessChunk.

#### Bug #6 (HIGH) -- partial_json (input_json_delta) unbuffered unmask

**File:** `proxy/anthropic.go` -- `relayStreamWithTracking()`, `ProxySidecar()`

**Cause:** `input_json_delta` events (tool call arguments) used `ReplaceDirectJSON` which is unbuffered. When Claude Code calls tools (Edit, Bash, etc.) that contain masked values, the placeholder appears in the tool call's partial JSON stream. If `[[IP_ADDRESS_1]]` splits across chunks (e.g. chunk1=`[[IP_ADDR`, chunk2=`ESS_1]]`), neither chunk contains the complete placeholder, so it passes through unmodified. Users saw raw `[[IP_ADDRESS_N]]` in tool outputs.

**Fix:** Added `ProcessChunkJSON` method to `StreamUnmasker` with separate JSON-mode buffers (`piiJSONBuffer`, `secretsJSONBuffer`) that accumulate partial placeholders across chunks, same as `ProcessChunk` does for text/thinking but using `RestorePlaceholdersJSON` for JSON-safe escaping. Both `relayStreamWithTracking` and `ProxySidecar` now use `ProcessChunkJSON` for partial_json events.

#### Bug #7 (CRITICAL) -- GLM outputs "undefined" instead of preserving placeholders

**Files:** `privacy/masking/stream.go`, `privacy/pipeline.go`

**Cause:** Z.AI/GLM models sometimes output literal `undefined` instead of preserving `[[TYPE_N]]` placeholders. For example, a masked IP `10.0.0.1` replaced by `[[IP_ADDRESS_1]]` comes back as `undefined` in the response. This causes garbled output like `undefinedundefinedundefined172.18.0.9` leaking to the client.

The problem has two forms:
1. **Non-streaming:** `undefined` appears in complete response body
2. **Streaming:** `undefined` split across SSE chunks (e.g. chunk1=`undef`, chunk2=`ined`) -- existing fallback only runs per-chunk, so partial `undefined` passes through unmodified

**Fix:**

*Non-streaming* (`privacy/pipeline.go`):
- Added `replaceUndefinedNonStream` with 3-phase budget-based replacement
- Phase 1: Replace `undefined` with original values (budget = number of originals)
- Phase 2: Dedup adjacent `<original> undefined` pairs
- Phase 3: Strip remaining bare `undefined` after budget exhaustion

*Streaming* (`privacy/masking/stream.go`):
- Added `undefinedBuffer` field to `StreamUnmasker` for cross-chunk `undefined` buffering
- Added `bufferPartialUndefined(text) (safe, buffer)` -- detects text ending with a prefix of `"undefined"` (1-8 chars) and splits it for next chunk
- Added `stripPartialUndefined(text)` -- strips partial `undefined` prefixes during flush
- `ProcessChunk` / `ProcessChunkJSON` flow: prepend buffer -> run fallback -> buffer tail partial -> strip leftovers
- `Flush` drains `undefinedBuffer` with `stripPartialUndefined` + `stripStrayUndefined`
- `HasContexts()` guard ensures legitimate `undefined` in code (e.g. `typeof x === undefined`) is preserved when no masking is active

**Test coverage:** 2700+ tests across edge cases, fuzz, and random split scenarios.

#### Bug #8 (HIGH) -- Garbled "undefined" leaks when masking is inactive

**Files:** `privacy/masking/stream.go`, `proxy/anthropic.go`

**Cause:** Bugs #7's fix (3-phase undefined fallback) only runs when the privacy pipeline is active (`HasSecrets || HasPII`). When no PII/secrets are detected in a request, the unmasker is nil, and GLM's garbled `undefinedundefined...` output passes straight to the client.

**Fix:**

Added `SanitizeGarbledOutput(text string) string` -- a masking-independent final guard:
- Regex `(?:undefined[\s]*){2,}` matches 2+ consecutive "undefined"
- Single "undefined" preserved (e.g. code: `typeof x === "undefined"`)
- Runs at 4 response write points regardless of masking state:
  1. `relayStreamWithTracking` -- text/thinking deltas (always)
  2. `handleNonStreamResponse` -- body before JSON validation
  3. `ProxySidecar` stream -- content_block_delta when unmasker is nil
  4. `ProxySidecar` non-stream -- body before `w.Write`

**Before vs After:**

| Input | Before (no masking) | After |
|---|---|---|
| `undefinedundefinedundefined` | Leaked to client | Stripped |
| `http://undefinedundefined192.168.5.111` | Leaked to client | `http://192.168.5.111` |
| `undefined undefined undefined` | Leaked to client | Stripped |
| `typeof x === "undefined"` | Passed through | Passed through (single) |
| `if (x === undefined && y === undefined)` | Passed through | Passed through (non-consecutive) |

### Streaming Unmask Flow (after fix)

```
Request -> Mask PII/Secrets -> Upstream API
  |
  SSE Stream Response
  |
  +-------------------------------------------+
  | content_block_delta?                      |
  | YES -> check delta type:                  |
  |   text_delta:                             |
  |     unmasker active?                      |
  |       YES -> ProcessChunk() (buffered)    |
  |     SanitizeGarbledOutput() (always)      |
  |   thinking_delta:                         |
  |     unmasker active?                      |
  |       YES -> ProcessChunk() (buffered)    |
  |     SanitizeGarbledOutput() (always)      |
  |   input_json_delta:                       |
  |     unmasker active?                      |
  |       YES -> ProcessChunkJSON() (buffered)|
  |                                           |
  | content_block_stop?                       |
  | YES -> Flush() -> emit delta before stop  |
  |                                           |
  | other + contains [[ + unmasker active?    |
  | YES -> ReplaceDirectJSON                  |
  |                                           |
  | Relay to client                           |
  +-------------------------------------------+
  |
  End of stream -> Flush() -> emit
  |
  Unmasked Response

Non-stream path:
  Body -> UnmaskResponse() (if masking active)
       -> SanitizeGarbledOutput() (always)
       -> JSON validation -> w.Write()
```

### Known Limitation

Placeholders that split across content block boundary (text -> thinking) cannot be restored because each block is a separate logical unit. However this case is very rare in practice.

---

## 3. GLM Mode Isolation Fix

### Problem: GLM_MODE affects all providers

`GLM_MODE=true` (default value) caused Z.AI features to run on all models including claude:
- Resolver fallback sends claude model to Z.AI when no Anthropic token available
- `filterUnsupportedContent` is now a no-op (content blocks pass through as-is)
- Vision routing sends claude image request to Z.AI vision endpoint

`GLM_MODE=false` hides Z.AI models from listing (correct) but shouldn't have other effects.

### Principle: Provider-Scoped, Not Flag-Scoped

GLM_MODE should be an infrastructure-level toggle (key sync, model listing only). Request-path logic must decide based on **target provider**, not a global flag.

### 4 Fix Points

| File | Before | After |
|---|---|---|
| `provider/resolver.go:Resolve()` | Z.AI fallback for all models that can't find token | Z.AI fallback only for models with `zai` as intended provider or unmatched prefix rule |
| `handler/handler.go:666` | `!GLMMode && decision == nil` reject | `decision == nil && profileOverride == nil` reject in all cases |
| `handler/handler.go:735` | `GLMMode` -> filterUnsupportedContent | `filterUnsupportedContent()` is now a no-op |
| `handler/handler.go:1072` | `GLMMode && hasImages && (decision==nil \|\| zai)` | `hasImages && decision != nil && decision.ProviderID == "zai"` only |

### GLM_MODE Still Used (correct)

- `main.go` -- sync Z.AI keys into KeyPool
- `handler.go` -- hide zai models from listing when GLM mode is off
- `resolver.go` -- Z.AI fallback for unknown prefix and glm- model that can't find token

### Flow After Fix

```
claude-sonnet-4 request -> Resolve() -> matched "claude-" rule -> no token -> nil
  -> handler: decision == nil -> reject "no provider configured" OK

glm-5.1 request -> Resolve() -> matched "glm-" rule -> zai token
  -> yes: zai decision
  -> no token: zai decision (empty key, from pool)
  -> handler: content blocks pass through as-is, vision auto-route OK

unknown-model request -> Resolve() -> no rule matched -> GLM fallback -> zai decision
  -> handler: content blocks pass through as-is
```

---

## 4. Cache-Aware Privacy Masking

### Problem: Privacy prompt injection breaks Anthropic prompt cache -> 429

When user content contains secrets (e.g. `client_secret=abcdefghijklmnopqrstuvwxyz123456`), the privacy pipeline masks them to `[[ENV_SECRET_1]]` before sending to Anthropic. A privacy instruction prompt was injected into the **beginning** of the system prompt so Claude preserves `[[TYPE_N]]` placeholders in its response.

This caused Anthropic's prompt cache to miss on every request:

```
BEFORE (broken):

Request 1:
  system = [PRIVACY_PROMPT, sys_block(cache_control)]
                     ^--- PREPENDED --- shifts everything down
  Anthropic sees: "PRIVACY_PROMPT\n\n<sys_block>" -> cache CREATE

Request 2:
  system = [PRIVACY_PROMPT, sys_block(cache_control)]
  Anthropic cache hit? YES (same prefix) ...BUT only if masking active

Request 3 (no secrets in user message):
  system = [sys_block(cache_control)]     <-- no privacy prompt
  Anthropic cache hit? NO (different prefix from request 1 & 2)
  cache_read_input_tokens = 0 -> full token billing -> 429
```

The root cause: `injectSystemPrompt` **prepends** the privacy prompt to the system array. This shifts the cache prefix, so:

1. Requests WITH secrets: system = `[privacy, sys(cache)]`
2. Requests WITHOUT secrets: system = `[sys(cache)]`
3. Different prefix every time -> Anthropic cache never stabilizes
4. Full token billing on every request -> rate limit exhaustion -> **429**

### Fix: Append privacy prompt to END of system array

```
AFTER (fixed):

Request 1 (has secrets):
  system = [sys_block(cache_control), PRIVACY_PROMPT]
                                            ^--- APPENDED at end
  Anthropic caches: sys_block (prefix with cache_control)
  cache_create = 50K tokens (sys_block), privacy_prompt = uncached ~170 tokens

Request 2 (has secrets):
  system = [sys_block(cache_control), PRIVACY_PROMPT]
  Anthropic cache hit: sys_block (identical prefix)
  cache_read = 50K tokens -> only ~170 new tokens billed

Request 3 (no secrets):
  system = [sys_block(cache_control)]
  Anthropic cache hit: sys_block (identical prefix)
  cache_read = 50K tokens -> no privacy prompt overhead
```

Key insight: Anthropic's prompt cache is **prefix-based**. Blocks with `cache_control: {"type": "ephemeral"}` are cached. Adding blocks AFTER the cached prefix does not invalidate it.

### Request Flow Diagram

```
                    BEFORE (prepend)                           AFTER (append)
                ========================                 ========================

User: "secret=abc123"                     User: "secret=abc123"
  |                                         |
  v                                         v
Mask: "secret=[[ENV_SECRET_1]]"           Mask: "secret=[[ENV_SECRET_1]]"
  |                                         |
  v                                         v
+---------------------------+             +---------------------------+
| injectSystemPrompt()      |             | appendPrivacyPrompt()     |
|                           |             |                           |
| system[0] = PRIVACY_PROMPT|             | system[0] = sys_block     |
| system[1] = sys_block     |             |   (cache_control)         |
|   (cache_control)         |             | system[1] = PRIVACY_PROMPT|
|                           |             |                           |
| Prefix CHANGED!           |             | Prefix UNCHANGED!         |
| Cache MISS                |             | Cache HIT                 |
+---------------------------+             +---------------------------+
  |                                         |
  v                                         v
Anthropic receives:                       Anthropic receives:
  [privacy, sys(cache)]                     [sys(cache), privacy]
  cache_read = 0                            cache_read = 50000+
  Full token billing                        Minimal billing
  -> 429 after a few requests               -> No 429
```

### Multi-turn Conversation Cache Behavior

```
Turn 1: user sends "my password=meow123"
  maskResult != nil (has ENV_SECRET)
  system = [sys(cache), privacy_prompt]
  -> cache CREATE for sys block

Turn 2: user sends "how do I change it?"
  maskResult == nil (no secrets)
  system = [sys(cache)]
  -> cache READ for sys block (identical prefix)

Turn 3: user sends "the password is hunter2 now"
  maskResult != nil (has ENV_SECRET)
  system = [sys(cache), privacy_prompt]
  -> cache READ for sys block (identical prefix)

Result: All 3 turns hit cache for the system block prefix.
The privacy_prompt block (170 tokens) is only billed when present.
```

### Token Cost Analysis

| Scenario | Input tokens | Cache savings | Cost impact |
|----------|-------------|---------------|-------------|
| Before (prepend, cache miss) | ~50,000 | 0% | Full price every request |
| After (append, cache hit) | ~170 (privacy only) | ~99.7% | ~$0.0005/request |
| No secrets (no privacy) | 0 | 100% | Zero overhead |

### Files Changed

| File | Change | Purpose |
|------|--------|---------|
| `handler/handler.go:1313-1324` | Replace `injectSystemPrompt` with `appendPrivacyPrompt` | Append privacy prompt to END of system array |
| `handler/handler.go:2274-2295` | New `appendPrivacyPrompt()` function | APPEND instead of PREPEND to preserve cache prefix |
| `privacy/pipeline.go:130-153` | Cache lookup/store in span goroutine | Identical text produces identical masked output (mask cache) |
| `privacy/pipeline.go:136-141` | `SkipSystemBlocks` check before cache lookup | Never mask system blocks for Claude requests |
| `privacy/maskcache.go` | New `MaskCache` with SHA-256 key, 5min TTL | Local mask cache for dedup |
| `privacy/masking/context.go:116-132` | `MergeExternal()` with counter update | Inject cached placeholders into per-request context |

### Mask Cache (Local, In-Memory)

In addition to the Anthropic prompt cache fix, a local mask cache avoids re-masking identical text spans:

```
Span text: "client_secret=abcdefghijklmnopqrstuvwxyz123456"
  |
  v
Cache lookup (SHA-256 of span text, 5min TTL)
  |
  +-- HIT: return cached masked text + inject placeholders into MaskContext
  |
  +-- MISS: run detection + masking -> store result in cache
```

The mask cache ensures identical text produces identical masked output, which helps Anthropic's cache even when the same secret appears in different requests.

### Verification

```bash
# Test: 5 rapid requests with large system prompt + secret in user message
# Expected: cache_read > 0 on requests 2-5, no 429s

Conv 1: input=201 cache_read=0    cache_create=1616   # First request creates cache
Conv 2: input=3   cache_read=1616 cache_create=198    # Cache HIT!
Conv 3: input=3   cache_read=1616 cache_create=198    # Cache HIT!
Conv 4: input=3   cache_read=1616 cache_create=198    # Cache HIT!
Conv 5: input=3   cache_read=1616 cache_create=198    # Cache HIT!
```

### Deployment

Only `arl-gateway` needs restart. The change is in the Go binary only:
- No database changes
- No Redis/Dragonfly changes (tokens unaffected)
- No config changes

```bash
DOCKER_DEFAULT_PLATFORM="" docker-compose build arl-gateway
docker-compose up -d arl-gateway
```
