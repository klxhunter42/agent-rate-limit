# Cache, Privacy Guard, Filter & Disclosure

> Modules: `cache/`, `privacy/`, `filter/`, `disclosure/`

---

## Table of Contents

1. [Cache System (ROI-Based Eviction)](#1-cache-system-roi-based-eviction)
2. [Privacy Guard / PasteGuard (PII & Secrets Protection)](#2-privacy-guard--pasteguard-pii--secrets-protection)
3. [Filter System (Intent Classification)](#3-filter-system-intent-classification)
4. [Disclosure (Progressive Content Escalation)](#4-disclosure-progressive-content-escalation)
5. [Integration Pipeline Diagram](#5-integration-pipeline-diagram)

---

## 1. Cache System (ROI-Based Eviction)

**Package**: `cache/`
**Files**: `eviction.go`

### Overview

The cache module implements an ROI (Return on Investment) based eviction manager backed by Redis. It tracks per-key statistics and periodically evicts the lowest-performing cache entries. It is wired as optimizer F14 in the 13-stage optimization pipeline.

### Architecture

The cache is not a traditional LRU/LFU store. It is a **statistics overlay** on top of Redis hash keys. The actual cached content lives elsewhere (e.g., delta encoding, warm start). This module only manages metadata about cache performance and evicts underperforming keys.

```
EvictionManager
  ├── Config (enabled, eviction percentage, period)
  ├── redis.Client (Redis connection)
  └── cacheMetrics (Prometheus counters/histograms)
```

### Data Structures

#### Config

| Field         | Type            | Default | Env Var                  |
|---------------|-----------------|---------|--------------------------|
| `Enabled`     | `bool`          | `true`  | `CACHE_EVICTION_ENABLED` |
| `EvictPct`    | `float64`       | `10.0`  | `CACHE_EVICTION_PCT`     |
| `EvictPeriod` | `time.Duration` | `5m`    | (hardcoded)              |

#### Redis Key Schema

Each tracked cache key gets a Redis hash at `cache:stats:<key>`:

| Hash Field        | Type    | Description                           |
|-------------------|---------|---------------------------------------|
| `tokens_saved`    | `int64` | Cumulative tokens saved by cache hits |
| `hit_count`       | `int64` | Number of cache hits                  |
| `tokens_injected` | `int64` | Tokens injected from cache            |

TTL: 24 hours per stats hash.

### ROI Calculation

```
ROI = tokens_saved / tokens_injected
```

- If `tokens_injected` is 0, ROI defaults to 0.0.
- Higher ROI = more valuable cache entry (more tokens saved per token injected).

### Eviction Algorithm

Runs every 5 minutes in a background goroutine (`StartEvictionLoop`):

1. **SCAN** all keys matching `cache:stats:*` from Redis.
2. **Fetch** `tokens_saved` and `tokens_injected` for each key via `HGETALL`.
3. **Compute ROI** for each key.
4. **Sort** entries by ROI ascending (lowest first).
5. **Select bottom `EvictPct%`** (default 10%) for eviction. Minimum 1 key if any exist.
6. **Delete** selected keys from Redis.

The sort uses insertion sort (suitable for typical cache key counts).

### Public Methods

| Method                                      | Description                                             |
|---------------------------------------------|---------------------------------------------------------|
| `New(reg, rdb)`                             | Creates manager, registers Prometheus metrics           |
| `RecordHit(ctx, key, tokensSaved)`          | Increments `tokens_saved` and `hit_count` in a pipeline |
| `RecordInjection(ctx, key, tokensInjected)` | Sets `tokens_injected` on a key                         |
| `Evict(ctx)`                                | Runs one eviction pass, returns count of evicted keys   |
| `StartEvictionLoop(ctx)`                    | Launches background goroutine with 5m ticker            |

### Prometheus Metrics

| Metric                                             | Type      | Labels | Description                    |
|----------------------------------------------------|-----------|--------|--------------------------------|
| `api_gateway_cache_eviction_keys_evicted_total`    | Counter   | -      | Total keys evicted             |
| `api_gateway_cache_eviction_roi_score`             | Histogram | -      | ROI score at eviction time     |
| `api_gateway_cache_eviction_pass_duration_seconds` | Histogram | -      | Duration of each eviction pass |

### Integration Point

Called from `Optimizers.PostProxyFeedback()` in `handler/optimizers.go`:

```go
if o.Cache != nil && input > 0 {
    saved := input / 4
    o.Cache.RecordHit(ctx, "session:"+sessionID, saved)
}
```

---

## 2. Privacy Guard / PasteGuard (PII & Secrets Protection)

**Package**: `privacy/`
**Sub-packages**: `pii/`, `secrets/`, `masking/`, `extractors/`

### Overview

PasteGuard is a two-layer privacy pipeline that detects and masks sensitive data (secrets and PII) in outgoing API requests before they reach upstream LLM providers, then unmasks the placeholders in responses. This prevents API keys, credentials, emails, phone numbers, and other sensitive data from being exposed to third-party AI services.

### Architecture

```
Pipeline
  ├── Config (enabled flags, entity lists, scan limits)
  ├── SecretDetector (regex-based secret scanning)
  ├── RegexDetector (regex-based PII scanning)
  └── Metrics (Prometheus histograms/counters)

Request Flow:
  JSON body -> ExtractTextSpans -> Detect Secrets -> Detect PII -> Mask -> Masked JSON body

Response Flow:
  Response body -> UnmaskResponse (or StreamUnmasker) -> Original body restored
```

### Configuration

#### Pipeline Config

| Field            | Type       | Default     | Env Var                      |
|------------------|------------|-------------|------------------------------|
| `Enabled`        | `bool`     | `true`      | `PASTEGUARD_ENABLED`         |
| `SecretsEnabled` | `bool`     | `true`      | `PASTEGUARD_SECRETS_ENABLED` |
| `MaxScanChars`   | `int`      | `200000`    | `PASTEGUARD_MAX_SCAN_CHARS`  |
| `SecretEntities` | `[]string` | (see below) | `PASTEGUARD_SECRET_ENTITIES` |
| `PIIEnabled`     | `bool`     | `true`      | `PASTEGUARD_PII_ENABLED`     |
| `PIIEntities`    | `[]string` | (see below) | `PASTEGUARD_PII_ENTITIES`    |

#### Default Secret Entities

```
OPENSSH_PRIVATE_KEY, PEM_PRIVATE_KEY, API_KEY_SK, API_KEY_AWS,
API_KEY_GITHUB, JWT_TOKEN, BEARER_TOKEN
```

#### Default PII Entities

```
EMAIL_ADDRESS, PHONE_NUMBER  # Only EMAIL_ADDRESS and PHONE_NUMBER are enabled by default
IP_ADDRESS, THAI_NATIONAL_ID, THAI_PHONE
```

### Secrets Detection (`secrets/`)

#### Entity Types and Patterns

| Entity | Pattern | Example |
|---|---|---|
| `OPENSSH_PRIVATE_KEY` | OpenSSH private key header | `-----BEGIN OPENSSH PRIVATE KEY-----` |
| `PEM_PRIVATE_KEY` | PEM private key (RSA, generic, encrypted) | `-----BEGIN RSA PRIVATE KEY-----` |
| `API_KEY_SK` | `sk[-_][a-zA-Z0-9_-]{20,}` | `sk-proj-abc123...` |
| `API_KEY_AWS` | `AKIA[0-9A-Z]{16}` | `AKIAIOSFODNN7EXAMPLE` |
| `API_KEY_GITHUB` | `gh[pousr]_[a-zA-Z0-9]{36,}` | `ghp_xxxxxxxxxxxx` |
| `API_KEY_GITLAB` | `gl(?:pat\|dt\|cbt\|ptt)-[a-zA-Z0-9_-]{20,}` | `glpat-xxxxxxxxxxxx` |
| `JWT_TOKEN` | `eyJ...\.eyJ...\....` (3 segments, 20+ chars each) | `eyJhbGciOi...` |
| `BEARER_TOKEN` | `Bearer [a-zA-Z0-9._-]{40,}` | `Bearer abc123...` |
| `ENV_PASSWORD` | `VAR_PASSWORD/PWD = 'value'` (8+ chars) | `DB_PASSWORD=supersecret123` |
| `ENV_SECRET` | `VAR_SECRET = 'value'` (8+ chars) | `API_SECRET='mysecretvalue123'` |
| `CONNECTION_STRING` | `postgres://user:pass@host` | `postgres://user:pass@host:5432/mydb` |
| `THAI_NATIONAL_ID` | `[1-8]\d{12}` | `1234567890123` |

#### Detection Algorithm

1. Truncate text to `MaxScanChars` (default 200K chars).
2. For each enabled pattern, run `FindAllStringIndex` to find all matches.
3. Deduplicate by start position (first match wins at each position).
4. Sort locations by start position descending (for safe backward replacement).

#### Masking

`MaskSecrets(text, locations, ctx)` replaces each detected span with a placeholder:

- Placeholder format: `[[ENTITY_TYPE_N]]` (e.g., `sk-abc123def456ghi789jkl`)
- Same original value always gets the same placeholder (dedup via `ReverseMap`).
- Locations sorted descending before replacement to preserve indices.

### PII Detection (`pii/`)

#### Entity Types and Regex Patterns

| Entity             | Regex Summary                                         | Confidence |
|--------------------|-------------------------------------------------------|------------|
| `EMAIL_ADDRESS`    | Standard email format                                 | 0.95       |
| `PHONE_NUMBER`     | International phone numbers                           | 0.90       |
| `CREDIT_CARD`      | Visa/MC/Amex/Discover patterns                        | 0.95       | *(available but not in default set)*
| `SSN`              | US SSN: `xxx-xx-xxxx`                                 | 0.90       | *(available but not in default set)*
| `IBAN`             | 2-letter country + 2 check digits + up to 30 alphanum | 0.90       |
| `IP_ADDRESS`       | IPv4 addresses                                        | 0.80       | *(available but not in default set)*
| `THAI_NATIONAL_ID` | `x-xxxx-xxxxx-xx-x`                                   | 0.90       |
| `THAI_PHONE`       | `0[2-9]x-xxx-xxxx` or `+66[2-9]x-xxx-xxxx`            | 0.90       |

#### False-Positive Filtering

URLs are pre-detected via `urlRegex`. PHONE_NUMBER and IP_ADDRESS matches that overlap with a URL span are excluded:

```go
urlSpans := urlRegex.FindAllStringIndex(text, -1)
// Skip phone/IP matches that overlap URL spans
```

#### Detection Algorithm

1. Pre-compute URL spans for false-positive filtering.
2. For each enabled entity type, run the corresponding regex.
3. Skip phone/IP matches that overlap with URL spans.
4. Assign confidence scores per entity type.

#### PII Masking

`MaskPII(text, entities, ctx)` uses `ReplaceWithPlaceholdersScored` with conflict resolution:

- Groups entities by type.
- Merges overlapping same-type entities (union of boundaries, max score).
- Cross-type conflicts resolved by greedy selection: higher score wins, longer span wins ties.
- Placeholder format same as secrets: `user@example.com`.

### Text Extraction (`extractors/`)

`ExtractTextSpans(payload)` extracts all text content from an Anthropic-format request body:

| Location                            | Example Path                             | NestedIndex |
|-------------------------------------|------------------------------------------|-------------|
| System prompt (string)              | `system`                                 | -1          |
| System prompt (content blocks)      | `system[0].text`                         | -1          |
| Message string content              | `messages[0].content`                    | -1          |
| Message text block                  | `messages[0].content[1].text`            | -1          |
| Message tool_result (string)        | `messages[0].content[1].content`         | -1          |
| Message tool_result (nested blocks) | `messages[0].content[1].content[0].text` | >= 0        |
| Message tool_use input fields       | `messages[0].content[1].input.keyName`   | -2          |

`extractInputStrings` recursively walks nested maps and arrays in tool_use `input` objects.

### Masking Infrastructure (`masking/`)

#### MaskContext

The core state object for a mask/unmask session:

```go
type MaskContext struct {
    Mapping    map[string]string  // placeholder -> original
    ReverseMap map[string]string  // original -> placeholder (dedup)
    Counters   map[string]int     // entity_type -> sequential counter
}
```

- `NextPlaceholder(type)` generates `[[TYPE_N]]` and increments the counter.
- `RestorePlaceholders(text)` replaces all placeholders with originals.
- `RestorePlaceholdersJSON(text)` same but JSON-escapes originals (for raw JSON bodies).

#### Placeholder Format

```
[[ENTITY_TYPE_COUNTER]]
```

Examples: `sk-abc123def456ghi789jkl`, `[[EMAIL_ADDRESS_3]]`, `[[PERSON_1]]`

#### Conflict Resolution

Two algorithms:

**ResolveOverlaps** (secrets, no scores):
- Sort by start ASC, longer span wins at same start.
- Greedy scan: skip spans whose start overlaps the previous kept span's end.

**ResolveConflicts** (PII, with scores):
- Group by entity type, merge overlapping same-type spans.
- Sort all merged spans by score DESC, then length DESC, then start ASC.
- Greedy selection: keep entity only if it does not overlap any already-kept entity.

#### Replacement Algorithm

Both `ReplaceWithPlaceholders` and `ReplaceWithPlaceholdersScored`:
1. Resolve overlaps/conflicts.
2. Assign placeholders forward (start ASC).
3. Replace backward (start DESC) to preserve string indices.

### Pipeline Processing (`pipeline.go`)

#### MaskRequest

```
1. Parse JSON body into map[string]any
2. ExtractTextSpans(payload) -> []TextSpan
3. Process spans in parallel (goroutine per span):
   a. secrets.Detect(text) -> detect secrets
   b. secrets.MaskSecrets(text, locations, secretsCtx) -> mask
   c. pii.RegexDetector.Detect(text) -> detect PII
   d. pii.MaskPII(text, entities, piiCtx) -> mask
4. Apply masked text back to payload (applyMaskedToPayload)
5. Serialize to JSON
6. Return MaskResult{MaskedBody, SecretsCtx, PIICtx}
```

Spans are processed in parallel with a `sync.WaitGroup`. The `MaskContext` maps are protected by `ctxMu` mutex.

#### UnmaskResponse (Non-Streaming)

```
1. Check if any placeholder exists in response body.
2. Unmask secrets first (innermost layer).
3. Unmask PII second (outermost layer).
4. Return restored body.
```

Order matters: secrets are masked first during request processing, then PII is applied on top. Unmasking reverses this.

#### StreamUnmasker (Streaming SSE)

For streaming responses, the `StreamUnmasker` handles partial placeholder buffering:

```go
type StreamUnmasker struct {
    piiBuffer     string        // buffer for partial PII placeholders
    secretsBuffer string        // buffer for partial secret placeholders
    piiCtx        *MaskContext
    secretsCtx    *MaskContext
}
```

**ProcessChunk(chunk)** - For text/thinking deltas:
1. Concatenate buffer + chunk.
2. Check for partial placeholder (`[[` without matching `]]`).
3. If partial found: process safe portion, buffer remainder.
4. If no partial: process entire combined string, clear buffer.
5. Apply secrets unmasking first, then PII unmasking.

**ReplaceDirect(text)** - Unbuffered replacement for non-delta SSE fields.

**ReplaceDirectJSON(text)** - Same but JSON-escapes restored values.

**Flush()** - Returns any remaining buffered content with restoration.

**Integration in proxy** (`proxy/anthropic.go`):

| SSE Event Field      | Unmask Method         |
|----------------------|-----------------------|
| `delta.text`         | `ProcessChunk()`      |
| `delta.thinking`     | `ProcessChunk()`      |
| `delta.partial_json` | `ReplaceDirectJSON()` |
| Raw SSE data lines   | `ReplaceDirectJSON()` |
| Error bodies         | `UnmaskResponse()`    |
| Non-streaming bodies | `UnmaskResponse()`    |
| End of stream        | `Flush()`             |

### Prometheus Metrics

| Metric                               | Type      | Labels                   | Description                                                         |
|--------------------------------------|-----------|--------------------------|---------------------------------------------------------------------|
| `api_gateway_mask_duration_seconds`  | Histogram | `phase`                  | Duration by phase: `secrets_detect`, `pii_detect`, `mask`, `unmask` |
| `api_gateway_secrets_detected_total` | Counter   | `type`                   | Secrets detected by entity type                                     |
| `api_gateway_pii_detected_total`     | Counter   | `type`                   | PII entities detected by type                                       |
| `api_gateway_mask_requests_total`    | Counter   | `has_secrets`, `has_pii` | Requests processed by pipeline                                      |

### Handler Integration

In `handler/handler.go` (line ~830):

```go
// Privacy masking: detect and mask secrets/PII before proxying
if h.privacy != nil {
    maskResult, _ = h.privacy.MaskRequest(body)
    if maskResult != nil {
        body = maskResult.MaskedBody
    }
}
```

- Skipped for image requests (URLs/base64 get corrupted).
- Runs after optimizer pipeline but before proxying.
- `maskResult` is passed to proxy functions for response unmasking.

---

## 3. Filter System (Intent Classification)

**Package**: `filter/`
**Files**: `filter.go`

### Overview

The filter module classifies user intent from request messages and applies response filtering to reduce token output for certain intent types. It is wired as optimizer F13 in the optimization pipeline.

### Configuration

| Field     | Type   | Default | Env Var          |
|-----------|--------|---------|------------------|
| `Enabled` | `bool` | `true`  | `FILTER_ENABLED` |

### Intent Types

| Intent           | Value      | Description                             |
|------------------|------------|-----------------------------------------|
| `IntentCode`     | `code`     | Code generation/modification requests   |
| `IntentAnalysis` | `analysis` | Explanation and analysis requests       |
| `IntentSearch`   | `search`   | Finding and locating requests           |
| `IntentAction`   | `action`   | Execution and deployment requests       |
| `IntentChat`     | `chat`     | General conversation (default fallback) |

### Intent Patterns

Each intent (except `chat`) has two regex patterns:

| Intent | Pattern 1 | Pattern 2 |
|---|---|---|
| `code` | `write\|implement\|fix\|refactor\|create file\|...` | `code\|coding\|function\|method\|struct\|...` |
| `analysis` | `explain\|analyze\|why does\|how does\|compare\|...` | `meaning\|purpose\|reason\|difference between\|...` |
| `search` | `find\|search\|where is\|locate\|list all\|...` | `how many\|count\|occurrences of\|...` |
| `action` | `run\|execute\|deploy\|test\|build\|compile\|...` | `install\|uninstall\|migrate\|rollback\|...` |

### Classification Algorithm

`ClassifyIntent(messages)`:

1. Find the **last user message** in the messages array.
2. Score each intent by counting regex matches in the user message.
3. Return the intent with the highest score.
4. Default to `IntentChat` if no patterns match (score = 0).

### Response Filtering

`FilterResponse(content, intent)`:

| Intent         | Strategy                                                                         |
|----------------|----------------------------------------------------------------------------------|
| `IntentCode`   | Extract only code blocks using `tokenizer.SplitCodeBlocks()`                     |
| `IntentSearch` | Extract key lines (bullets, numbered items, file paths, short lines with colons) |
| Others         | No filtering (return as-is)                                                      |

**Search filtering criteria**: Lines kept if they start with `- ` or `* ` or contain `.go`, `.ts`, `.py`, or are under 120 chars with a colon. Appends a `[N lines filtered for relevance]` footer.

### Prometheus Metrics

| Metric                                 | Type    | Labels   | Description                            |
|----------------------------------------|---------|----------|----------------------------------------|
| `api_gateway_filter_intents_total`     | Counter | `intent` | Intent classification counts           |
| `api_gateway_filter_chars_saved_total` | Counter | `intent` | Characters saved by response filtering |

### Integration Point

Called from `Optimizers.OptimizeSystemPrompt()` in `handler/optimizers.go`:

```go
if o.Filter != nil {
    intent := o.Filter.ClassifyIntent(nil)  // nil = use internal scoring
    opt, saved := o.Filter.FilterResponse(text, intent)
}
```

Note: In the current integration, `ClassifyIntent` is called with `nil` messages (system prompt optimization context), so it falls through to `IntentChat` by default. The filter is more effective when used with actual request messages.

---

## 4. Disclosure (Progressive Content Escalation)

**Package**: `disclosure/`
**Files**: `config.go`, `disclosure.go`

### Overview

The disclosure module implements progressive content escalation: serving minimal content first and escalating to fuller content only when needed. It uses a three-layer model backed by Redis for keyword indexing.

### Configuration

| Field      | Type   | Default | Env Var                |
|------------|--------|---------|------------------------|
| `Enabled`  | `bool` | `true`  | `DISCLOSURE_ENABLED`   |
| `L1Tokens` | `int`  | `15`    | `DISCLOSURE_L1_TOKENS` |
| `L2Tokens` | `int`  | `60`    | `DISCLOSURE_L2_TOKENS` |

### Layer Model

| Layer           | Constant     | Description                                         | Budget               |
|-----------------|--------------|-----------------------------------------------------|----------------------|
| Layer 1 (Index) | `LayerIndex` | First ~L1Tokens*4 chars (heading/summary)           | `15 * 4 = 60` chars  |
| Layer 2 (FTS)   | `LayerFTS`   | Keyword-matched paragraphs within L2Tokens*4 budget | `60 * 4 = 240` chars |
| Layer 3 (Full)  | `LayerFull`  | Full content (fallback)                             | Unlimited            |

### Escalation Algorithm

`Escalate(ctx, content, query, maxTokens)`:

1. **Layer 1**: Return first `L1Tokens * 4` chars. Used when no query is provided.
2. **Layer 2**: Split content into paragraphs (`\n\n`). Match paragraphs containing query keywords (case-insensitive). Collect up to `L2Tokens * 4` chars of matched paragraphs.
3. **Layer 3**: Return full content if Layer 2 has no matches.

Returns: `(content, layer, charsSaved)`

### Keyword Index Caching

`StoreLayer(ctx, id, content)`:

1. SHA-256 hash of content (first 8 bytes as hex).
2. Extract unique words (lowercase, trimmed punctuation, length > 2).
3. Store as space-joined string in Redis at `disclosure:idx:<hash>`.
4. TTL: 1 hour.

### FTS Extraction

`ftsExtract(content, query, budget)`:

1. Split query into words.
2. Split content into paragraphs by `\n\n`.
3. For each paragraph, check if any query word appears (case-insensitive `Contains`).
4. Collect matching paragraphs up to budget.
5. Join with `\n\n`.

### Prometheus Metrics

| Metric                                     | Type    | Labels  | Description                                |
|--------------------------------------------|---------|---------|--------------------------------------------|
| `api_gateway_disclosure_escalations_total` | Counter | `layer` | Layer escalation counts (1, 2, 3)          |
| `api_gateway_disclosure_chars_saved_total` | Counter | -       | Characters saved by progressive disclosure |
| `api_gateway_disclosure_fts_hit_rate`      | Gauge   | -       | Whether FTS layer had matches (1 or 0)     |

### Integration Point

Instantiated in `main.go`:

```go
optDisclosure := disclosure.New(m.Registry(), optRdb)
```

Wired into `Optimizers.Disclosure` field. Available for use in the optimization pipeline.

---

## 5. Integration Pipeline Diagram

### Request Processing Pipeline

```
Client Request (POST /v1/messages)
  |
  v
[1] Parse JSON payload
  |
  v
[2] System prompt injection (if enabled)
  |
  v
[3] Smart max_tokens auto-adjustment
  |
  v
[4] Strip unsupported fields
  |
  v
[5] Filter unsupported content blocks (Z.AI only)
  |
  v
[6] Image detection
  |          \
  |          \-- hasImages=true --> SKIP optimizer + privacy
  |
  v (no images)
[7] Optimizer Pipeline (13 stages):
  |   F7  - Semantic dedup
  |   F1  - Chunker (reorder)
  |   F8  - Delta encoding
  |   F9  - Sketch dedup
  |   F6  - Summarizer (red budget only)
  |   F13 - Intent filter
  |   F16 - Caveman compression
  v
[8] Re-encode payload after optimization
  |
  v
[9] PasteGuard Privacy Masking:
  |   a. ExtractTextSpans (system, messages, tool inputs)
  |   b. For each span (parallel goroutines):
  |       i.   secrets.Detect() -> regex scan
  |       ii.  secrets.MaskSecrets() -> [[SECRET_N]] placeholders
  |       iii. pii.Detect() -> regex scan + URL filtering
  |       iv.  pii.MaskPII() -> [[PII_TYPE_N]] placeholders (conflict resolved)
  |   c. Apply masked text back to payload
  |   d. Serialize masked JSON body
  v
[10] Proxy to upstream (Anthropic/OpenAI/Gemini/Z.AI)
  |
  v
[11] Response handling:
  |
  +-- Non-streaming:
  |     UnmaskResponse() -> restore secrets then PII
  |
  +-- Streaming SSE:
        NewStreamUnmasker(piiCtx, secretsCtx)
        For each event:
          delta.text         -> ProcessChunk()
          delta.thinking     -> ProcessChunk()
          delta.partial_json -> ReplaceDirectJSON()
          raw data lines     -> ReplaceDirectJSON()
          error bodies       -> UnmaskResponse()
        End of stream:
          Flush() -> drain partial buffers
```

### Post-Proxy Feedback Loop

```
After proxy response completes:
  |
  v
Optimizers.PostProxyFeedback(sessionID, model, input, output):
  |   F4  - Prefetcher.Record() - record session pattern
  |   F11 - Waste.RecordRequest() - detect token waste
  |   F14 - Cache.RecordHit() - record ROI stats (input/4 tokens saved)
  |   F5  - Bandit.Update() - multi-armed bandit reward
  v
```

### Module Dependency Graph

```
handler.Optimizers
  |
  +-- cache.EvictionManager --------> Redis (cache:stats:*)
  |     RecordHit / Evict
  |
  +-- disclosure.Disclosure --------> Redis (disclosure:idx:*)
  |     Escalate / StoreLayer
  |
  +-- filter.Filter
  |     ClassifyIntent / FilterResponse -> tokenizer
  |
  +-- privacy.Pipeline
        |
        +-- secrets.SecretDetector (regex patterns)
        |     +-- masking.MaskContext (placeholder state)
        |     +-- masking.ReplaceWithPlaceholders
        |     +-- masking.ResolveOverlaps
        |
        +-- pii.RegexDetector (regex patterns + URL filter)
        |     +-- masking.MaskContext
        |     +-- masking.ReplaceWithPlaceholdersScored
        |     +-- masking.ResolveConflicts
        |
        +-- extractors (Anthropic format text extraction)
        |     ExtractTextSpans -> []TextSpan
        |
        +-- masking.StreamUnmasker (SSE streaming)
              ProcessChunk / ReplaceDirect / ReplaceDirectJSON / Flush
```

### Environment Variables Summary

| Env Var                      | Default           | Module          |
|------------------------------|-------------------|-----------------|
| `CACHE_EVICTION_ENABLED`     | `true`            | cache           |
| `CACHE_EVICTION_PCT`         | `10.0`            | cache           |
| `PASTEGUARD_ENABLED`         | `true`            | privacy         |
| `PASTEGUARD_SECRETS_ENABLED` | `true`            | privacy/secrets |
| `PASTEGUARD_MAX_SCAN_CHARS`  | `200000`          | privacy/secrets |
| `PASTEGUARD_SECRET_ENTITIES` | (comma-separated) | privacy/secrets |
| `PASTEGUARD_PII_ENABLED`     | `true`            | privacy/pii     |
| `PASTEGUARD_PII_ENTITIES`    | (comma-separated) | privacy/pii     |
| `FILTER_ENABLED`             | `true`            | filter          |
| `DISCLOSURE_ENABLED`         | `true`            | disclosure      |
| `DISCLOSURE_L1_TOKENS`       | `15`              | disclosure      |
| `DISCLOSURE_L2_TOKENS`       | `60`              | disclosure      |
