# Privacy and Security

## 1. PasteGuard PII Detection: Presidio-to-Regex Migration

### Why Presidio Was Replaced

The original PasteGuard PII pipeline used Microsoft Presidio Analyzer (NLP container) for entity detection. This caused severe latency problems:

| Issue | Detail |
|---|---|
| **Slow HTTP calls** | Each Presidio `/analyze` call took 7-14 seconds per text span |
| **Compounding latency** | Multiple spans per request = 30+ seconds total request time |
| **Overkill NLP** | Only 2 entity types were used (`EMAIL_ADDRESS`, `PHONE_NUMBER`) - far too light to justify a 2GB NLP container |
| **Regex is faster** | Compiled regex detection is <1ms vs 7-14s per Presidio call |
| **Container removed** | Presidio container (2GB RAM) is no longer needed for default deployment |

The replacement is `RegexDetector` -- a pure Go regex engine with zero external dependencies.

### RegexDetector Supported Entities

| Entity | Pattern | Example |
|---|---|---|
| `EMAIL_ADDRESS` | Standard email format | `user@example.com` |
| `PHONE_NUMBER` | International phone numbers | `+1-555-123-4567` |
| `CREDIT_CARD` | Visa / Mastercard / Amex / Discover | `4111-1111-1111-1111` |
| `SSN` | US Social Security Number | `123-45-6789` |
| `IBAN` | International Bank Account Number | `GB82WEST12345698765432` |
| `IP_ADDRESS` | IPv4 addresses | `192.168.1.1` |
| `THAI_NATIONAL_ID` | Thai citizen ID (13 digits) | `1-1001-00001-23-4` |
| `THAI_PHONE` | Thai phone format | `081-234-5678`, `+66812345678` |

Default: all 8 entities are enabled. Customize via `PASTEGUARD_PII_ENTITIES` env var.

### Presidio Legacy (Optional)

The Presidio container is still available for legacy use but is not required:

```bash
# Start with Presidio (not recommended - use only if needed)
docker-compose --profile pii up

# Default deployment (regex only, no Presidio)
docker-compose up
```

### Files

| File | Purpose |
|---|---|
| `privacy/pii/detect.go` | `RegexDetector` -- regex pattern matching for all 8 entities |
| `privacy/pii/mask.go` | PII masking with placeholder generation |
| `privacy/config.go` | Env var loading, default config |
| `privacy/pipeline.go` | Full mask/unmask pipeline (secrets + PII) |

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

### Streaming Unmask Flow (after fix)

```
Request -> Mask PII/Secrets -> Upstream API
  |
  SSE Stream Response
  |
  +---------------------------+
  | content_block_delta?      |
  | YES -> ProcessChunk()     |
  |   (buffered, every time)  |
  |                           |
  | content_block_stop?       |
  | YES -> Flush() -> emit    |
  |   delta before stop       |
  |                           |
  | other + contains [[[      |
  | YES -> ReplaceDirectJSON  |
  |                           |
  | Relay to client           |
  +---------------------------+
  |
  End of stream -> Flush() -> emit
  |
  Unmasked Response
```

### Known Limitation

Placeholders that split across content block boundary (text -> thinking) cannot be restored because each block is a separate logical unit. However this case is very rare in practice.

---

## 3. GLM Mode Isolation Fix

### Problem: GLM_MODE affects all providers

`GLM_MODE=true` (default value) caused Z.AI features to run on all models including claude:
- Resolver fallback sends claude model to Z.AI when no Anthropic token available
- `filterUnsupportedContent` strips claude request content blocks
- Vision routing sends claude image request to Z.AI vision endpoint

`GLM_MODE=false` hides Z.AI models from listing (correct) but shouldn't have other effects.

### Principle: Provider-Scoped, Not Flag-Scoped

GLM_MODE should be an infrastructure-level toggle (key sync, model listing only). Request-path logic must decide based on **target provider**, not a global flag.

### 4 Fix Points

| File | Before | After |
|---|---|---|
| `provider/resolver.go:Resolve()` | Z.AI fallback for all models that can't find token | Z.AI fallback only for models with `zai` as intended provider or unmatched prefix rule |
| `handler/handler.go:584` | `!GLMMode && decision == nil` reject | `decision == nil` reject in all cases |
| `handler/handler.go:653` | `GLMMode` -> filterUnsupportedContent | `decision.ProviderID == "zai"` only |
| `handler/handler.go:974` | `GLMMode && hasImages && (decision==nil \|\| zai)` | `hasImages && decision != nil && zai` only |

### GLM_MODE Still Used (correct)

- `main.go:217` -- sync Z.AI keys into KeyPool
- `handler.go:1762,1822` -- hide zai models from listing when off
- `resolver.go` -- Z.AI fallback for unknown prefix and glm- model that can't find token

### Flow After Fix

```
claude-sonnet-4 request -> Resolve() -> matched "claude-" rule -> no token -> nil
  -> handler: decision == nil -> reject "no provider configured" OK

glm-5.1 request -> Resolve() -> matched "glm-" rule -> zai token
  -> yes: zai decision
  -> no token: zai decision (empty key, from pool)
  -> handler: filterUnsupportedContent OK, vision auto-route OK

unknown-model request -> Resolve() -> no rule matched -> GLM fallback -> zai decision
  -> handler: filterUnsupportedContent OK
```
