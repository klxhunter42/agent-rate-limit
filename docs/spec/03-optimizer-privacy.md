# 03 - Optimizer Pipeline & Privacy Guard

## 1. Architecture Overview

```
Request Flow:
  Client -> Handler.Messages()
    -> Optimizer Pipeline (system prompt + messages)
    -> Privacy Guard (secrets + PII masking)
    -> Proxy (forward masked body)
    -> Response Unmasking (non-streaming or streaming SSE)
  Client <- Unmasked response

Components:
  tokenizer/     - Token estimation, whitespace/dedup/truncation, budget tracking
  handler/optimizers.go - 13-stage pipeline orchestrator
  chunker/       - Rabin-Karp content-addressed chunking + reorder
  packer/        - Greedy 0/1 knapsack budget packing
  disclosure/    - Progressive 3-layer disclosure (index/FTS/full)
  prefetcher/    - Markov chain tool-call prediction
  bandit/        - LinUCB contextual bandit for technique selection
  summarizer/    - Extractive summarization (first sentence per paragraph)
  delta/         - LCS-based delta encoding against cached baseline
  sketch/        - SimHash near-duplicate detection
  waste/         - 7-detector waste analysis engine
  filter/        - Intent classification + response filtering
  cache/         - ROI-based cache eviction manager
  warmstart/     - Cosine-similarity session warm-start
  caveman/       - Budget-tiered output style injection
  privacy/       - Secrets + PII detection, masking, and streaming unmask
```

---

## 2. Optimizer Pipeline - 13 Components

### 2.1 Optimizers Struct

File: `api-gateway/handler/optimizers.go`

```go
type Optimizers struct {
    Chunker     *chunker.Chunker
    Packer      *packer.Packer
    Disclosure  *disclosure.Disclosure
    Prefetcher  *prefetcher.Prefetcher
    Bandit      *bandit.Bandit
    Summarizer  *summarizer.Summarizer
    Delta       *delta.Delta
    Sketch      *sketch.Sketch
    Waste       *waste.WasteDetector
    Filter      *filter.Filter
    Cache       *cache.EvictionManager
    WarmStart   *warmstart.WarmStart
    Caveman     *caveman.CavemanPipeline
}
```

Nil fields = feature disabled. All components are optional.

### 2.2 System Prompt Pipeline Order

`OptimizeSystemPrompt(text, metrics, budgetLevel, model)` applies stages in this fixed order:

| Stage # | Name | Feature ID | Budget Gate | Input | Output | Description |
|---------|------|-----------|-------------|-------|--------|-------------|
| 1 | Semantic Dedup | F7 | Always | Raw text | Deduped text | Jaccard similarity sentence dedup (threshold 0.7) |
| 2 | Chunker | F1 | Always | Deduped text | Reordered text | Rabin-Karp chunk, reorder stable-first |
| 3 | Delta Encoding | F8 | Always | Reordered text | Delta-encoded string | LCS diff against cached baseline |
| 4 | Sketch Dedup | F9 | Always | Text | Metrics only | SimHash near-duplicate detection |
| 5 | Summarizer | F6 | Red only | Text | Summarized text | Extractive: first sentence per paragraph |
| 6 | Intent Filter | F13 | Always | Text | Filtered text | Classify intent, extract code/key lines |
| 7 | Caveman Compression | F16 | Always | Text | Text + injection | Append output-style system prompt |

After all stages: if `totalSaved > 0`, records `tokensSaved` and `costSavings` ($3/M token estimate).

### 2.3 Message Body Pipeline

`OptimizeMessages(messages, metrics)` applies lightweight optimization to message content:

For each message in the array:
1. **String content**: Apply `OptimizeWhitespace` then `DeduplicateSentences`
2. **Content block array**: For each block:
   - Skip `tool_use` blocks entirely
   - `text` blocks: optimize `text` field
   - `tool_result` blocks: optimize `content` field (string or nested array)

Metrics label: `message_text` for string content, `message_block_{type}` for block content.

### 2.4 Post-Proxy Feedback

`PostProxyFeedback(sessionID, model, input, output)` records telemetry after proxy completes:

| Component | Action |
|-----------|--------|
| Prefetcher (F4) | `Record(sessionID, model)` - learns tool call sequences |
| Waste Detector (F11) | `RecordRequest(sessionID, model, input, output)` |
| Cache ROI (F14) | `RecordHit("session:"+sessionID, input/4)` |
| Bandit Feedback (F5) | `Update(model, features, reward)` where reward = output/input (capped 1.0) |

---

## 3. Optimizer Components - Detailed Specs

### 3.1 Tokenizer (tokenizer/optimizer.go + tokenizer/similarity.go)

**Content Type Detection** (`DetectContentType`):
- JSON: starts with `{` + ends with `}` or `[` + ends with `]`
- Code: >30% of non-empty lines match code indicator regex
- Markdown: >20% of non-empty lines match markdown indicator regex
- Text: default fallback

**Token Estimation** (`charsPerToken` ratios from tiktoken calibration):

| Content Type | Ratio |
|-------------|-------|
| Code | 2.5 chars/token |
| JSON | 2.8 chars/token |
| Markdown | 3.5 chars/token |
| Text | 4.0 chars/token |

`EstimateTokens(text)` = `ceil(len(text) / ratio)`
`QuickEstimateTokens(text)` = `(len(text) + 3) / 4`

**Model Capabilities** (`KnownModels` map):

| Model | Context Window | Max Output | Provider |
|-------|---------------|------------|----------|
| claude-opus-4-7 | 200,000 | 163,840 | anthropic |
| claude-sonnet-4-6 | 200,000 | 163,840 | anthropic |
| claude-haiku-4-5-20251001 | 200,000 | 8,192 | anthropic |
| claude-3-5-sonnet-20241022 | 200,000 | 8,192 | anthropic |
| gpt-4o | 128,000 | 16,384 | openai |
| o1 | 200,000 | 100,000 | openai |
| gemini-2.5-pro | 1,048,576 | 65,536 | google |
| glm-5.1 | 128,000 | 4,096 | zai |
| glm-4.6v | 8,192 | 4,096 | zai |

Prefix matching: if exact match fails, tries `strings.HasPrefix(model, knownKey)`.
Default fallback: `{128000, 4096, "unknown"}`.

**Whitespace Optimization** (`OptimizeWhitespace`):
1. `SplitCodeBlocks(text)` -> segments, each marked `IsCode` or prose
2. Code segments: preserved verbatim
3. Prose segments: `optimizeProseWhitespace`:
   - Collapse consecutive spaces/tabs to single space
   - Trim trailing whitespace per line
   - Collapse >2 consecutive blank lines to 2
   - `strings.TrimSpace` final result
4. Returns `(result, savedTokens)`

**Sentence Deduplication** (`DeduplicateSentences`):
1. If text contains privacy placeholders (`__SECRET_\d+__` or `__PII_\d+__`): skip entirely, return `(text, 0)`
2. Split into code/prose segments
3. For prose: `dedupProse`:
   - Split into sentences via `[.!?]\s+` regex
   - Normalize each sentence: lowercase, collapse whitespace, keep only letters/digits/spaces
   - Keep first occurrence of each normalized sentence
4. Returns `(result, savedTokens)`

**Semantic Deduplication** (`DeduplicateSemantic`):
1. Privacy placeholder guard (same as above)
2. Split into code/prose segments
3. For prose: `dedupSemanticProse`:
   - Split into sentences
   - Skip short sentences (<10 normalized chars)
   - Compare each sentence against kept list using `jaccardFast` (word-set Jaccard)
   - Threshold: 0.7 (configurable)
   - Keep only sentences not exceeding threshold with any previously kept sentence
4. Returns `(result, savedTokens)`

**Head-Tail Truncation** (`TruncateHeadTail`):
1. If `len(text) <= maxChars`: return unchanged
2. If text contains privacy placeholders: return unchanged
3. Default `headRatio = 0.4`
4. Split into lines, take head portion and tail portion
5. Insert marker: `[N lines truncated - showing first X + last Y lines]`
6. Min tail lines: 5 (when total > 10)

**Code Block Splitting** (`SplitCodeBlocks`):
- Detects ` ``` ` fenced code blocks
- Each segment is either prose (IsCode=false) or code (IsCode=true)
- Unclosed code blocks: final segment marked as code

**Similarity Functions** (tokenizer/similarity.go):
- `JaccardSimilarity(a, b, shingleSize)`: shingle-based Jaccard for long texts
- `jaccardFast(a, b)`: word-set Jaccard (no shingling) for sentence-level
- `LevenshteinSimilarity(a, b)`: normalized edit distance similarity
- `CosineSimilarity(a, b)`: float64 vector cosine similarity

### 3.2 Chunker (chunker/chunker.go)

**Algorithm**: Rabin-Karp rolling hash content-addressed chunking with Redis-backed stability tracking.

**Configuration**:

| Env Var | Default | Description |
|---------|---------|-------------|
| CHUNKER_ENABLED | true | Enable/disable |
| CHUNKER_MIN_CHUNK | 128 | Minimum chunk size in bytes |
| CHUNKER_MAX_CHUNK | 4096 | Maximum chunk size in bytes |
| CHUNKER_WINDOW_SIZE | 48 | Rolling hash window size |
| CHUNKER_STABLE_THRESHOLD | 2 | Times seen to be considered stable |

**Chunking Algorithm** (`chunk`):
1. If content < MinChunk: single chunk with SHA-256 hash (first 12 hex chars)
2. Rolling window of size `WindowSize`:
   - If distance from start >= MaxChunk: force boundary
   - If distance from start < MinChunk: skip
   - Compute rolling hash: `hash = hash*31 + char[i]` over window
   - If `hash % 256 == 0`: boundary found
3. Each chunk: `{Hash: sha256[:12], Content: string, IsStable: bool}`

**Reorder** (`ChunkAndReorder`):
1. Chunk content
2. For each chunk: check `isStable` via Redis (key `chunker:stable:{hash}`, threshold >= StableThreshold)
3. Record each chunk in Redis (increment, 24h TTL)
4. Reorder: stable chunks first, then novel chunks
5. Rebuild content string from reordered chunks

**Metrics**:
- `api_gateway_chunker_chunks_total{type="stable|novel"}`
- `api_gateway_chunker_reorder_duration_seconds`
- `api_gateway_chunker_cache_hit_rate` (gauge)
- `api_gateway_chunker_chars_saved_total`

### 3.3 Packer (packer/packer.go)

**Algorithm**: Greedy 0/1 knapsack for token budget allocation.

**Configuration**:

| Env Var | Default | Description |
|---------|---------|-------------|
| PACKER_ENABLED | true | Enable/disable |
| PACKER_MIN_UTILITY | 0.1 | Minimum utility score to include |

**Pack** (`Pack(items, tokenBudget)`):
1. Filter items by `MinUtility`
2. Sort eligible items by utility/token ratio descending
3. Greedy fill: add items while `usedTokens + item.Tokens <= tokenBudget`
4. Excluded items contribute to `excludedChars`

**Metrics**:
- `api_gateway_packer_items_packed_total{result="included|excluded"}`
- `api_gateway_packer_budget_utilization` (gauge)
- `api_gateway_packer_tokens_saved_total`

### 3.4 Disclosure (disclosure/disclosure.go)

**Algorithm**: 3-layer progressive disclosure with full-text search.

**Configuration**:

| Env Var | Default | Description |
|---------|---------|-------------|
| DISCLOSURE_ENABLED | true | Enable/disable |
| DISCLOSURE_L1_TOKENS | 15 | Layer 1 budget (tokens) |
| DISCLOSURE_L2_TOKENS | 60 | Layer 2 budget (tokens) |

**Layers**:

| Layer | Name | Strategy | Budget |
|-------|------|----------|--------|
| 1 | Index | First L1Tokens*4 chars of content | ~60 chars |
| 2 | FTS | Keyword-matched paragraphs within L2Tokens*4 | ~240 chars |
| 3 | Full | Return entire content | Unlimited |

**Escalate** (`Escalate(ctx, content, query, maxTokens)`):
1. Layer 1: if no query, return first L1Tokens*4 chars
2. Layer 2: `ftsExtract` - split content into paragraphs (on `\n\n`), match query words (lowercased), collect matching paragraphs within budget
3. Layer 3: fallback, return full content

**StoreLayer** (`StoreLayer`):
- SHA-256 hash of content -> Redis key `disclosure:idx:{hash[:8]}`
- Store unique words (>2 chars) as space-joined string, 1h TTL

**Metrics**:
- `api_gateway_disclosure_escalations_total{layer="1|2|3"}`
- `api_gateway_disclosure_chars_saved_total`
- `api_gateway_disclosure_fts_hit_rate` (gauge)

### 3.5 Prefetcher (prefetcher/prefetcher.go)

**Algorithm**: Order-1 Markov chain for tool-call prediction.

**Configuration**:

| Env Var | Default | Description |
|---------|---------|-------------|
| PREFETCHER_ENABLED | true | Enable/disable |
| PREFETCHER_MAX_ORDER | 5 | Max history length per session |
| PREFETCHER_TOP_K | 3 | Top-K predictions |

**Record** (`Record(ctx, sessionID, toolCall)`):
1. Append to Redis list `prefetcher:chain:{sessionID}` (trimmed to MaxOrder)
2. Update transition table: `prefetcher:trans:{prevTool}` field `{toolCall}` incremented
3. TTL: 4 hours

**Predict** (`Predict(ctx, sessionID)`):
1. Get last tool from session chain
2. Get all transitions from `prefetcher:trans:{lastTool}`
3. Normalize counts to confidence scores
4. Return top-K predictions sorted by count descending

**PreWarm** (`PreWarm`): Store prediction in Redis for verification (1min TTL).

**Metrics**:
- `api_gateway_prefetcher_predictions_total{correct="true|false"}`
- `api_gateway_prefetcher_order_used` (histogram)
- `api_gateway_prefetcher_prewarm_duration_seconds`

### 3.6 Bandit (bandit/bandit.go)

**Algorithm**: LinUCB (Linear Upper Confidence Bound) contextual bandit with 10-dimensional feature space.

**Configuration**:

| Env Var | Default | Description |
|---------|---------|-------------|
| BANDIT_ENABLED | true | Enable/disable |
| BANDIT_ALPHA | 1.0 | Exploration parameter |
| BANDIT_DECAY | 0.99 | Reward decay factor |

**Dimensions**: `dim = 10`

**Select** (`Select(ctx, features)`):
1. Load arm states from Redis (`bandit:state:{armID}`)
2. For each arm:
   - Compute theta = A^{-1} * b
   - Mean = theta . phi
   - Variance = phi^T * A^{-1} * phi
   - UCB score = mean + alpha * sqrt(|variance|)
3. Select arm with highest score
4. Mark exploratory if |variance| > 1.0

**Update** (`Update(ctx, armID, features, reward)`):
1. A += phi * phi^T (outer product)
2. b += reward * phi
3. Save state to Redis (24h TTL)

**State persistence**: JSON-serialized `armState{A [10][10]float64, B [10]float64}` in Redis.

**Matrix inversion**: Gauss-Jordan elimination on 10x10 augmented matrix.

**Metrics**:
- `api_gateway_bandit_selections_total{arm, exploratory}`
- `api_gateway_bandit_reward_total{arm}`
- `api_gateway_bandit_selection_duration_seconds`

### 3.7 Summarizer (summarizer/summarizer.go)

**Algorithm**: Extractive summarization - keep first sentence of each paragraph.

**Configuration**:

| Env Var | Default | Description |
|---------|---------|-------------|
| SUMMARIZER_ENABLED | true | Enable/disable |
| SUMMARIZER_MODEL | glm-4.7-flashx | Reserved for LLM mode |
| SUMMARIZER_MAX_RATIO | 0.3 | Max output length as ratio of input |

**Summarize** (`Summarize(ctx, content, budgetLevel)`):
1. Compute SHA-256 hash, check Redis cache `summarizer:cache:{hash[:8]}`
2. If cache hit: return cached result
3. Extractive: `extractiveSummarize`:
   - Split content into paragraphs (on `\n\n`)
   - For each paragraph: keep `firstSentence` (up to first `.!?` followed by space, or first 200 chars)
   - Total length capped at `len(content) * MaxRatio`
4. Cache result in Redis (1h TTL)

**Budget gate**: Only runs when `budgetLevel >= 2` (Red zone).

**Metrics**:
- `api_gateway_summarizer_calls_total{method="cached|truncation"}`
- `api_gateway_summarizer_chars_saved_total{method}`
- `api_gateway_summarizer_duration_seconds{method}`
- `api_gateway_summarizer_llm_tokens_total`

### 3.8 Delta Encoding (delta/delta.go)

**Algorithm**: Line-based LCS (Longest Common Subsequence) diff against cached baseline.

**Configuration**:

| Env Var | Default | Description |
|---------|---------|-------------|
| DELTA_ENABLED | true | Enable/disable |
| DELTA_MIN_SAVINGS_PCT | 10.0 | Minimum savings percentage to use delta |

**Constants**: `maxLCSBytes = 50000`, `maxOps = 200`

**Encode** (`Encode(ctx, cacheKey, content)`):
1. Get baseline from Redis `delta:baseline:{cacheKey}` (24h TTL)
2. If no baseline: store current as baseline, return passthrough
3. If content or baseline > maxLCSBytes: passthrough
4. Compute LCS diff: build DP table, backtrack to produce ops
5. Ops: `+` (insert), `-` (delete), `=` (keep)
6. Compact: merge consecutive same-type ops
7. If savings < MinSavingsPct: passthrough
8. Serialize: `{type}{length}:{data}` for each op
9. Update baseline to current content

**Decode** (`Decode(delta, base)`): Apply ops to base string to reconstruct.

**Metrics**:
- `api_gateway_delta_encodes_total{result="delta|passthrough"}`
- `api_gateway_delta_chars_saved_total`
- `api_gateway_delta_savings_pct` (histogram)

### 3.9 Sketch Dedup (sketch/sketch.go)

**Algorithm**: FNV-1a based 1-bit sketch (SimHash variant) for near-duplicate detection.

**Configuration**:

| Env Var | Default | Description |
|---------|---------|-------------|
| SKETCH_ENABLED | true | Enable/disable |
| SKETCH_DIMENSIONS | 128 | Bit vector dimensions |
| SKETCH_THRESHOLD | 0.85 | Hamming similarity threshold for duplicate |

**Compute** (`Compute(content)`):
1. Tokenize content into words (alphanumeric only)
2. For each word: compute FNV-1a hash
3. For each of 3 hash positions per word: set bit in sketch vector
4. Return byte array of size `(dimensions + 7) / 8`

**Similarity** (`Similarity(a, b)`): Hamming similarity = `(totalBits - popcount(a XOR b)) / totalBits`

**CheckAndStore** (`CheckAndStore(ctx, sessionID, content)`):
1. Compute sketch for content
2. Compare against recent sketches in Redis list `sketch:recent:{sessionID}` (last 100, 24h TTL)
3. If any sketch has similarity >= Threshold: mark as duplicate, return chars saved
4. Otherwise: store sketch, return unique

**Metrics**:
- `api_gateway_sketch_checks_total{result="duplicate|unique"}`
- `api_gateway_sketch_hamming_similarity` (histogram)
- `api_gateway_sketch_chars_saved_total`

### 3.10 Waste Detector (waste/waste.go)

**Algorithm**: 7 independent detectors scanning accumulated per-session request records.

**Configuration**:

| Env Var | Default | Description |
|---------|---------|-------------|
| WASTE_ENABLED | true | Enable/disable |
| WASTE_MIN_REQUESTS | 10 | Minimum requests before detection |

**Detectors**:

| Detector | Severity | Trigger |
|----------|----------|---------|
| `empty_response` | High | >10% of requests have output=0 |
| `retry_churn` | Medium | Repeated identical input with output=0, wasted >5000 tokens |
| `loop_detection` | High | N-cycle repetition in request inputs (size 2..len/2) |
| `oversized_context` | Medium | Multiple requests with input >100K tokens, wasted >100K |
| `budget_exceeded` | Medium | Session uses >3 different models |
| `redundant_tool_call` | Low | Identical request-response pairs |
| `low_value_response` | Low | >=3 requests with >5K input but <50 output |

**Session eviction**: Sessions with no activity for 30 minutes are evicted.

**Background scanner**: `StartBackgroundScanner(ctx, interval)` runs periodic `Scan()`.

**Metrics**:
- `api_gateway_waste_findings_total{detector, severity}`
- `api_gateway_waste_tokens_wasted_total{detector}`
- `api_gateway_waste_scan_duration_seconds`

### 3.11 Intent Filter (filter/filter.go)

**Algorithm**: Regex-based intent classification with response filtering.

**Intent Types**:

| Intent | Trigger Patterns |
|--------|-----------------|
| Code | write, implement, fix, refactor, create file, coding, function, struct, etc. |
| Analysis | explain, analyze, why does, compare, review, meaning, trade-off |
| Search | find, search, where is, locate, list all, how many, grep |
| Action | run, execute, deploy, test, build, install, migrate, configure |
| Chat | Default fallback (no pattern match) |

**ClassifyIntent** (`ClassifyIntent(messages)`):
1. Scan messages from end to start, find last user message
2. Match against intent patterns, accumulate scores
3. Return highest-scoring intent, default to Chat

**FilterResponse** (`FilterResponse(content, intent)`):
- `IntentCode`: Extract only code blocks from content
- `IntentSearch`: Extract lines starting with bullets, numbers, or containing file paths
- Other intents: no filtering (return as-is)

**Metrics**:
- `api_gateway_filter_intents_total{intent}`
- `api_gateway_filter_chars_saved_total{intent}`

### 3.12 Cache Eviction Manager (cache/eviction.go)

**Algorithm**: ROI-based cache eviction - remove bottom percentile by savings/injection ratio.

**Configuration**:

| Env Var | Default | Description |
|---------|---------|-------------|
| CACHE_EVICTION_ENABLED | true | Enable/disable |
| CACHE_EVICTION_PCT | 10.0 | Bottom percentile to evict per pass |
| EvictPeriod | 5 minutes | Periodic eviction interval |

**RecordHit** (`RecordHit(ctx, key, tokensSaved)`):
- Redis hash `cache:stats:{key}`: increment `tokens_saved`, `hit_count`, 24h TTL

**RecordInjection** (`RecordInjection(ctx, key, tokensInjected)`):
- Set `tokens_injected` field in same hash

**Evict** (`Evict(ctx)`):
1. SCAN all `cache:stats:*` keys
2. Compute ROI = `tokens_saved / tokens_injected` for each
3. Sort ascending by ROI
4. Delete bottom EvictPct% of keys
5. Minimum evict count: 1 (if any keys exist)

**Background loop**: `StartEvictionLoop(ctx)` runs eviction every EvictPeriod.

**Metrics**:
- `api_gateway_cache_eviction_keys_evicted_total`
- `api_gateway_cache_eviction_roi_score` (histogram)
- `api_gateway_cache_eviction_pass_duration_seconds`

### 3.13 Warm Start (warmstart/warmstart.go)

**Algorithm**: 32-dimensional session fingerprinting with cosine similarity matching.

**Configuration**:

| Env Var | Default | Description |
|---------|---------|-------------|
| WARMSTART_ENABLED | true | Enable/disable |
| WARMSTART_TOP_K | 3 | Top-K similar sessions |
| WARMSTART_MIN_SIMILARITY | 0.5 | Minimum cosine similarity for warm start |

**Signature Dimensions** (32 total):

| Dim Range | Feature | Encoding |
|-----------|---------|----------|
| 0-3 | Model type | One-hot: claude, openai, gemini, glm |
| 4-7 | Content type distribution | code_ratio, json_ratio, md_ratio, text_ratio |
| 8-15 | Tool call frequency | Top 8 tools by normalized count |
| 16-18 | Budget level distribution | green_pct, yellow_pct, red_pct |
| 19-22 | Request size buckets | avg_input/10K, avg_output/1K, total_req/100, avg_dur/10K |
| 23-27 | Intent distribution | code_pct, analysis_pct, search_pct, action_pct, chat_pct |
| 28-31 | Hash projection | project_hash, symbol_density, stream_pct, error_rate |

**WarmSession** (`WarmSession(ctx, sessionID, sessionData)`):
1. Compute signature from session data
2. `FindSimilar`: SCAN Redis keys matching `warmstart:sig:{projectRoot}:*`, compute cosine similarity with each, return best match
3. If best similarity >= MinSimilarity: hit, store current signature
4. Otherwise: miss

**Persistence**: Key `warmstart:sig:{projectRoot}:{sessionID}`, value is JSON `[32]float64`, TTL 7 days.

**Metrics**:
- `api_gateway_warmstart_sessions_warmed_total{result="hit|miss"}`
- `api_gateway_warmstart_similarity_score` (histogram)
- `api_gateway_warmstart_warmup_duration_seconds`

### 3.14 Caveman Compression (caveman/caveman.go)

**Algorithm**: Budget-tiered output style injection as system prompt suffix.

**Compression Tiers**:

| Tier | Budget Level | Estimated Ratio | Description |
|------|-------------|-----------------|-------------|
| Lite | Green (0) | 0.7 | Bullet points, skip pleasantries |
| Full | Yellow (1) | 0.5 | Terse, code-first, tables over paragraphs |
| Ultra | Red (2) | 0.25 | Raw output, compressed notation, no wrappers |
| Wenyan | Manual | 0.3 | Classical notation, facts only, minimal grammar |

**ShouldCompress** (`ShouldCompress(content, budgetLevel)`):
1. If content < MinSize (500 chars): skip
2. If auto-detect disabled: always TierFull
3. Budget-based: Green=TierLite, Yellow=TierFull, Red=TierUltra

**Compress** (`Compress(systemPrompt, tier)`):
- Appends tier injection text to system prompt
- Does NOT modify the actual content - only appends style instructions

**Validate** (`Validate(original, compressed)`):
- Check code block count preserved
- Check >=80% of key identifiers still present
- Key identifiers: alphanumeric words >3 chars, max 20 unique

**Metrics**:
- `api_gateway_caveman_compressions_total{tier, result="valid|invalid|skipped"}`
- `api_gateway_caveman_compression_ratio` (histogram)
- `api_gateway_caveman_validation_duration_seconds`

---

## 4. Budget Management

### 4.1 Budget Levels

File: `tokenizer/optimizer.go`

```go
type BudgetLevel int
const (
    BudgetGreen  BudgetLevel = iota  // <50% context used
    BudgetYellow                      // 50-75% context used
    BudgetRed                         // >75% context used
)
```

### 4.2 Budget Calculation in Handler

File: `handler/handler.go` (Messages handler)

Budget is calculated per-request, NOT cumulated across requests:

```
totalTokens = sysTokens + msgTokens
pctUsed = totalTokens / contextWindow

if pctUsed >= 0.8:  budgetLevel = 2 (Red)
if pctUsed >= 0.6:  budgetLevel = 1 (Yellow)
else:               budgetLevel = 0 (Green)
```

### 4.3 Technique Activation by Budget Level

| Technique | Green (0) | Yellow (1) | Red (2) |
|-----------|-----------|------------|---------|
| Semantic Dedup | Yes | Yes | Yes |
| Chunker | Yes | Yes | Yes |
| Delta Encoding | Yes | Yes | Yes |
| Sketch Dedup | Yes | Yes | Yes |
| Summarizer | No | No | **Yes** |
| Intent Filter | Yes | Yes | Yes |
| Caveman Tier | Lite | Full | Ultra |
| Whitespace Opt | Yes | Yes | Yes |
| Sentence Dedup | Yes | Yes | Yes |

### 4.4 TokenBudget Tracker

`NewTokenBudget(model)` creates a per-session tracker:
- `AddTokens(input, output)`: accumulate usage
- `Level()`: returns Green/Yellow/Red based on UsedTokens/ContextLimit
- `ShouldOptimize()`: true at Yellow+
- `ShouldForceOptimize()`: true at Red+

Thresholds: Yellow at 50%, Red at 75%.

---

## 5. Privacy Guard (PasteGuard)

### 5.1 Pipeline Architecture

File: `api-gateway/privacy/pipeline.go`

```
Mask Flow:
  MaskRequest(body)
    -> JSON unmarshal
    -> ExtractTextSpans(payload)          -- extract all text locations
    -> For each span (parallel goroutines):
       -> SecretDetector.Detect(text)     -- regex scan for secrets
       -> secrets.MaskSecrets(text, locs, ctx)
       -> pii.RegexDetector.Detect(text)  -- regex scan for PII
       -> pii.MaskPII(text, entities, ctx)
    -> Apply masked results back to payload
    -> JSON marshal -> MaskedBody

Unmask Flow (non-streaming):
  UnmaskResponse(body, maskResult)
    -> SecretsCtx.RestorePlaceholdersJSON(text)  -- secrets first (inner)
    -> PIICtx.RestorePlaceholdersJSON(text)       -- PII second (outer)

Unmask Flow (streaming):
  NewStreamUnmasker(result)
    -> ProcessChunk(chunk) per SSE text delta
    -> Flush() at stream end
```

### 5.2 Configuration

File: `api-gateway/privacy/config.go`

| Env Var | Default | Description |
|---------|---------|-------------|
| PASTEGUARD_ENABLED | true | Master switch |
| PASTEGUARD_SECRETS_ENABLED | true | Enable secret detection |
| PASTEGUARD_MAX_SCAN_CHARS | 200000 | Max characters to scan per text span |
| PASTEGUARD_SECRET_ENTITIES | (all types) | Comma-separated entity types to detect |
| PASTEGUARD_PII_ENABLED | true | Enable PII detection |
| PASTEGUARD_PII_ENTITIES | EMAIL_ADDRESS,PHONE_NUMBER,CREDIT_CARD,SSN,IBAN,IP_ADDRESS,THAI_NATIONAL_ID,THAI_PHONE | Comma-separated PII types |

**Default Config** (when env vars not set):
- Secret entities: `OPENSSH_PRIVATE_KEY,PEM_PRIVATE_KEY,API_KEY_SK,API_KEY_AWS,API_KEY_GITHUB,JWT_TOKEN,BEARER_TOKEN`
- PII entities: `EMAIL_ADDRESS,PHONE_NUMBER,CREDIT_CARD,SSN,IBAN,IP_ADDRESS,THAI_NATIONAL_ID,THAI_PHONE`
- When `PIIEntities` is empty in code (but non-default): defaults to `EMAIL_ADDRESS,PHONE_NUMBER`

### 5.3 Text Span Extraction

File: `api-gateway/privacy/extractors/anthropic.go`

`ExtractTextSpans(payload)` extracts all text content from an Anthropic-format request:

| Location | Path Pattern | Example |
|----------|-------------|---------|
| System (string) | `system` | `"system": "You are helpful"` |
| System (array) | `system[N].text` | Content block arrays |
| Message string | `messages[N].content` | `"content": "Hello"` |
| Text block | `messages[N].content[M].text` | `{"type":"text","text":"..."}` |
| Tool result (string) | `messages[N].content[M].content` | `{"type":"tool_result","content":"..."}` |
| Tool result (nested) | `messages[N].content[M].content[K].text` | Nested content array |
| Tool use input | `messages[N].content[M].input.{keyPath}` | Recursive leaf string extraction |

**TextSpan struct**:
```go
type TextSpan struct {
    Text         string
    Path         string  // JSON path
    MessageIndex int     // -1 for system prompt
    PartIndex    int
    NestedIndex  int     // -1 unused, -2 for tool_use input
    Role         string  // "system", "user", "assistant", "tool"
}
```

**Tool use input extraction**: `extractInputStrings` recursively navigates nested maps and arrays to extract leaf string values. Supports dot-separated key paths and `[N]` array index notation.

### 5.4 Secret Detection

File: `api-gateway/privacy/secrets/detect.go`, `secrets/patterns.go`

**Entity Types and Patterns**:

| Entity | Pattern | Example |
|--------|---------|---------|
| OPENSSH_PRIVATE_KEY | `-----BEGIN OPENSSH PRIVATE KEY-----` | SSH private key |
| PEM_PRIVATE_KEY | `-----BEGIN (RSA \|ENCRYPTED )?PRIVATE KEY-----` (3 patterns: RSA, plain, encrypted) | PEM keys |
| API_KEY_SK | `sk[-_][a-zA-Z0-9_-]{20,}` | OpenAI-style API keys |
| API_KEY_AWS | `AKIA[0-9A-Z]{16}` | AWS access key ID |
| API_KEY_GITHUB | `gh[pousr]_[a-zA-Z0-9]{36,}` | GitHub tokens |
| API_KEY_GITLAB | `gl(pat|dt|cbt|ptt)-[a-zA-Z0-9_-]{20,}` | GitLab tokens |
| JWT_TOKEN | `eyJ[a-zA-Z0-9_-]{20,}\.eyJ[a-zA-Z0-9_-]{20,}\.[a-zA-Z0-9_-]{20,}` | JWT tokens |
| BEARER_TOKEN | `(?i)Bearer\s+[a-zA-Z0-9._-]{40,}` | Bearer auth headers |
| ENV_PASSWORD | `(?i)[A-Za-z_][A-Za-z0-9_]*(PASSWORD|_PWD)\s*[=:]\s*['"]?[^\s'"]{8,}['"]?` | Password env vars |
| ENV_SECRET | `(?i)[A-Za-z_][A-Za-z0-9_]*_SECRET\s*[=:]\s*['"]?[^\s'"]{8,}['"]?` | Secret env vars |
| CONNECTION_STRING | `(?i)(?:postgres(?:ql)?|mysql|mariadb|mongodb(?:\+srv)?|redis|amqps?):\/\/[^:]+:[^@\s]+@[^\s'"]+` | DB connection strings |
| THAI_NATIONAL_ID | `\b[1-8]\d{12}\b` | 13-digit Thai ID |

**Detection Process**:
1. Truncate text to `maxScanChars` if configured
2. Run each enabled pattern's regex against text
3. Deduplicate matches at same start position
4. Collect `SecretLocation{Start, End, Type}` for each match
5. Sort locations by Start DESC for safe backward replacement

**DefaultDetector** (fallback when SecretEntities is empty) enables: OpenSSHKey, PEMKey, APIKeySK, APIKeyAWS, APIKeyGitHub, APIKeyGitLab, JWTToken, BearerToken, ThaiID.

**Note:** The normal code path uses `DefaultConfig().SecretEntities` (7 types, no GitLab or ThaiID) since `DefaultConfig()` always provides a non-empty list. `DefaultDetector()` is only reached if `SecretEntities` is explicitly set to empty.

### 5.5 PII Detection

File: `api-gateway/privacy/pii/detect.go`

**RegexDetector** replaces the slow Presidio HTTP container (7-14s per call) with <1ms regex.

**Entity Types, Patterns, and Scores**:

| Entity | Pattern | Score | Notes |
|--------|---------|-------|-------|
| EMAIL_ADDRESS | `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}` | 0.95 | Standard email |
| PHONE_NUMBER | `(\+?\d{1,3}[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}` | 0.90 | International format |
| CREDIT_CARD | Visa/MC/Amex/Discover prefixes + digit groups | 0.95 | 4xxx, 5xxx, 34/37, 6011, 65 |
| SSN | `\d{3}[ -]\d{2}[ -]\d{4}` | 0.90 | US Social Security |
| IBAN | `[A-Z]{2}\d{2}[A-Z0-9]{4}[A-Z0-9]{0,26}` | 0.90 | International bank account |
| IP_ADDRESS | IPv4 regex with proper octet validation | 0.80 | 0-255 per octet |
| THAI_NATIONAL_ID | `\d{1}[- ]?\d{4}[- ]?\d{5}[- ]?\d{2}[- ]?\d{1}` | 0.90 | Thai citizen ID |
| THAI_PHONE | `(\+66|0)[2-9]\d{1}[- ]?\d{3}[- ]?\d{4}` | 0.90 | Thai mobile/landline |

**URL False-Positive Filtering**:
- Before PII detection: extract all URL spans via `https?://...` regex
- PHONE_NUMBER, IP_ADDRESS, THAI_PHONE: skip if span overlaps with any URL span
- `overlapsURL(urlSpans, start, end)`: checks if `[start, end)` intersects any URL range

### 5.6 Placeholder System

File: `api-gateway/privacy/masking/context.go`

**Format**: `[[{ENTITY_TYPE}_{COUNTER}]]`
- Examples: `sk-abc123def456ghi789jkl012mno`, `[[EMAIL_ADDRESS_3]]`, `[[PERSON_1]]`

**MaskContext struct**:
```go
type MaskContext struct {
    Mapping    map[string]string  // placeholder -> original value
    ReverseMap map[string]string  // original value -> placeholder (dedup)
    Counters   map[string]int    // entity type -> sequential counter
}
```

**Placeholder generation**: `NextPlaceholder(entityType)` increments per-type counter, returns `[[{TYPE}_{N}]]`.

**Deduplication**: If the same original value appears multiple times, `ReverseMap` ensures the same placeholder is reused.

**Restoration**:
- `RestorePlaceholders(text)`: Replace placeholders with raw original values
- `RestorePlaceholdersJSON(text)`: Replace with JSON-escaped originals (escapes `"`, `\`, `\n`, `\r`, `\t`)
- Restoration order: sorted by placeholder length descending (longest first) to prevent partial matches (e.g., `[[PERSON_10]]` vs `[[PERSON_1]]`)

### 5.7 Overlap Resolution

File: `api-gateway/privacy/masking/conflict.go`

**Secrets** (`ResolveOverlaps`):
1. Sort by Start ASC, longer span wins ties on same start
2. Greedy scan: if next span overlaps with previous, shorter is silently dropped
3. Result: non-overlapping spans covering maximum area

**PII** (`ResolveConflicts`):
1. Group entities by type
2. Within each group: merge overlapping spans (union of boundaries, max score)
3. Sort all merged spans by Score DESC, then length DESC, then start ASC
4. Greedy selection: keep entity only if it doesn't overlap any already-kept entity
5. Result: non-overlapping entities, highest-confidence wins

**Span types**:
- `Span{Start, End}` - for secrets (no score)
- `ScoredSpan{Start, End, EntityType, Score}` - for PII (with confidence)

### 5.8 Replacement Algorithm

File: `api-gateway/privacy/masking/replace.go`

**ReplaceWithPlaceholders** (secrets):
1. Resolve overlaps
2. Sort spans by Start ASC
3. Assign placeholders sequentially
4. Replace backward (Start DESC) to preserve indices
5. Each replacement: `text[:start] + placeholder + text[end:]`

**ReplaceWithPlaceholdersScored** (PII):
1. Resolve conflicts
2. Sort by Start ASC
3. Assign placeholders with entity type
4. Replace backward (Start DESC)

### 5.9 Streaming Unmask

File: `api-gateway/privacy/masking/stream.go`

**StreamUnmasker struct**:
```go
type StreamUnmasker struct {
    piiBuffer     string
    secretsBuffer string
    piiCtx        *MaskContext
    secretsCtx    *MaskContext
}
```

**ProcessChunk** (`ProcessChunk(chunk)`):
1. Apply secrets unmask first: `processStreamChunk(secretsBuffer, chunk, secretsCtx)`
2. Apply PII unmask second: `processStreamChunk(piiBuffer, result, piiCtx)`
3. Return processed output

**processStreamChunk** algorithm:
1. Combine buffer + chunk: `combined = buffer + chunk`
2. Find partial placeholder start: `FindPartialPlaceholderStart(combined)`
   - Search for last `[[` in combined
   - If `[[` found but no closing `]]` after it: partial placeholder detected
3. If no partial: restore all placeholders in `combined`, return with empty buffer
4. If partial at position P:
   - Safe portion: `combined[:P]` -> restore placeholders -> output
   - Buffered portion: `combined[P:]` -> keep in buffer for next chunk

**FindPartialPlaceholderStart**:
- `LastIndex(text, "[[")`
- If `]]` found after that position: not partial, return -1
- Otherwise: return position of `[[`

**ReplaceDirect** (`ReplaceDirect(text)`): Unbuffered replacement for standalone strings (e.g., partial_json blocks). Avoids cross-block buffer contamination.

**ReplaceDirectJSON** (`ReplaceDirectJSON(text)`): Same as ReplaceDirect but with JSON-escaped restoration.

**Flush** (`Flush()`): Called at stream end. Restores any remaining buffered text as-is (even partial placeholders pass through).

**HasContexts**: Returns true if either context has non-empty mapping.

### 5.10 Unmask Order

For both streaming and non-streaming:
1. **Secrets first** (innermost layer)
2. **PII second** (outermost layer)

This matches the mask order: secrets are detected and masked first, then PII is applied on top of the already-masked text.

### 5.11 Apply Masked Results to Payload

File: `api-gateway/privacy/pipeline.go` (`applyMaskedToPayload`)

Handles all content block types:

| Block Type | Field Updated | Special Handling |
|-----------|---------------|------------------|
| System (string) | `payload["system"]` | Direct assignment |
| System (array) | `block["text"]` | PartIndex-based |
| Message string content | `msg["content"]` | Direct assignment |
| text block | `block["text"]` | Standard |
| tool_result (string content) | `block["content"]` | Direct |
| tool_result (nested array) | `nestedBlock["text"]` | NestedIndex-based |
| tool_use input | Leaf value via `setInputLeaf` | Dot-path + array index navigation |

`setInputLeaf` supports: `key`, `key.sub`, `key[0]` notation for navigating nested input objects.

---

## 6. Metrics Reference

### 6.1 Privacy Metrics

File: `api-gateway/privacy/metrics.go`

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_mask_duration_seconds` | Histogram | `phase` | Duration of mask/unmask ops by phase |
| `api_gateway_secrets_detected_total` | Counter | `type` | Secrets found by entity type |
| `api_gateway_pii_detected_total` | Counter | `type` | PII entities found by type |
| `api_gateway_mask_requests_total` | Counter | `has_secrets`, `has_pii` | Requests processed by masking pipeline |

**Phase labels for mask_duration_seconds**:
- `secrets_detect` - Secret detection duration
- `pii_detect` - PII detection duration
- `mask` - Masking (replacement) duration
- `unmask` - Unmasking duration

**Histogram buckets**: `[0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1]`

### 6.2 Optimizer Metrics (Global)

File: `api-gateway/metrics/metrics.go`

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `api_gateway_optimizer_chars_saved_total` | Counter | `technique` | Characters saved by technique |
| `api_gateway_optimizer_runs_total` | Counter | `technique` | Optimization runs by technique |
| `api_gateway_optimizer_duration_seconds` | Histogram | `technique` | Execution time by technique |
| `api_gateway_optimizer_tokens_saved_total` | Counter | - | Total estimated tokens saved |
| `api_gateway_budget_level` | Gauge | `model` | Current budget level (0/1/2) |
| `api_gateway_cost_savings_total` | Counter | - | Estimated cost savings in USD |

**Technique labels**: `semantic_dedup`, `chunker`, `delta`, `sketch_dedup`, `summarizer`, `intent_filter`, `caveman`, `message_text`, `message_block_text`, `message_block_tool_result`

### 6.3 Per-Component Metrics

| Component | Metrics (prefixed with `api_gateway_`) |
|-----------|---------------------------------------|
| Chunker | `chunker_chunks_total{type}`, `chunker_reorder_duration_seconds`, `chunker_cache_hit_rate`, `chunker_chars_saved_total` |
| Packer | `packer_items_packed_total{result}`, `packer_budget_utilization`, `packer_tokens_saved_total` |
| Disclosure | `disclosure_escalations_total{layer}`, `disclosure_chars_saved_total`, `disclosure_fts_hit_rate` |
| Prefetcher | `prefetcher_predictions_total{correct}`, `prefetcher_order_used`, `prefetcher_prewarm_duration_seconds` |
| Bandit | `bandit_selections_total{arm,exploratory}`, `bandit_reward_total{arm}`, `bandit_selection_duration_seconds` |
| Summarizer | `summarizer_calls_total{method}`, `summarizer_chars_saved_total{method}`, `summarizer_duration_seconds{method}`, `summarizer_llm_tokens_total` |
| Delta | `delta_encodes_total{result}`, `delta_chars_saved_total`, `delta_savings_pct` |
| Sketch | `sketch_checks_total{result}`, `sketch_hamming_similarity`, `sketch_chars_saved_total` |
| Waste | `waste_findings_total{detector,severity}`, `waste_tokens_wasted_total{detector}`, `waste_scan_duration_seconds` |
| Filter | `filter_intents_total{intent}`, `filter_chars_saved_total{intent}` |
| Cache | `cache_eviction_keys_evicted_total`, `cache_eviction_roi_score`, `cache_eviction_pass_duration_seconds` |
| WarmStart | `warmstart_sessions_warmed_total{result}`, `warmstart_similarity_score`, `warmstart_warmup_duration_seconds` |
| Caveman | `caveman_compressions_total{tier,result}`, `caveman_compression_ratio`, `caveman_validation_duration_seconds` |

---

## 7. Integration Points

### 7.1 Request Path (handler.go)

```
1. Parse body, resolve provider/model
2. If NOT transparent AND NOT image request:
   a. Inject system prompt (if enabled)
   b. Smart max_tokens adjustment
   c. Strip unsupported fields
3. If image request: skip optimizer + privacy entirely
4. If NOT image request:
   a. Calculate budget level (sysTokens + msgTokens vs contextWindow)
   b. Run OptimizeSystemPrompt()
   c. Run OptimizeMessages()
   d. Re-encode payload
   e. Run Privacy MaskRequest()
5. Proxy to upstream with masked body
6. Unmask response (streaming or non-streaming)
7. Run PostProxyFeedback()
```

### 7.2 Privacy Placeholder Guards in Optimizer

The tokenizer respects privacy placeholders at every stage:
- `DeduplicateSentences`: skips if `__SECRET_\d+__` or `__PII_\d+__` detected
- `DeduplicateSemantic`: same guard
- `TruncateHeadTail`: returns unchanged if placeholders present
- `OptimizeWhitespace`: preserves placeholders (code block preservation)

Note: The actual placeholder format from privacy is `[[TYPE_N]]`, not `__TYPE_N__`. The tokenizer guards check for `__SECRET_\d+__` / `__PII_\d+__` which is a separate format. These two systems use different placeholder formats and are independent.

### 7.3 Image Request Bypass

When `HasImageContent(payload)` returns true:
- Optimizer pipeline is completely skipped
- Privacy masking is completely skipped
- Reason: image URLs and base64 data would be corrupted by text transformations

### 7.4 Transparent Mode Bypass

When `transparent = true` (claude-oauth passthrough):
- System prompt injection is skipped
- Smart max_tokens is skipped
- Strip unsupported fields is skipped
- Optimizer and privacy masking still run (unless image request)

---

## 8. Edge Cases and Error Handling

### 8.1 Privacy Edge Cases

| Case | Behavior |
|------|----------|
| Invalid JSON body | `MaskRequest` returns error, no masking |
| No text spans found | Returns `nil, nil` (no masking needed) |
| No secrets/PII detected | Returns `nil, nil` |
| Same secret appears twice | Deduplicated via `ReverseMap`, single placeholder |
| Overlapping secrets | Longer span wins, shorter dropped |
| Overlapping PII of different types | Higher score wins; same score -> longer span wins |
| Same PII value at different positions | Single placeholder reused via `ReverseMap` |
| Text exceeds maxScanChars | Only first N chars scanned |
| Streaming: placeholder split across chunks | Buffered until complete, then restored |
| Streaming: stream ends with partial placeholder | `Flush()` passes through buffer as-is |
| Nil MaskResult passed to UnmaskResponse | Returns body unchanged |
| Tool_use input with nested arrays | Recursive extraction via `extractInputStrings` |

### 8.2 Optimizer Edge Cases

| Case | Behavior |
|------|----------|
| Empty system prompt | Returns unchanged (no stages run) |
| Text shorter than min chunk size | Single chunk, no reordering |
| Delta: no baseline exists | Stores current as baseline, returns passthrough |
| Delta: content > 50KB | Passthrough (too large for LCS) |
| Delta: savings < 10% | Passthrough (not worth delta encoding) |
| Sketch: no previous sketches | Stored as unique, no dedup |
| Waste: session < 10 requests | Skipped (not enough data) |
| Waste: session idle > 30min | Evicted from memory |
| Budget: unknown model | Defaults to 128K context |
| Caveman: content < 500 chars | Skipped |
| Filter: no user messages found | Default to IntentChat |
| Bandit: no previous state | Identity matrix initialization |

### 8.3 Streaming Unmask Edge Cases

| Case | Behavior |
|------|----------|
| Placeholder `[[PER` in one chunk, `SON_1]]` in next | Buffered correctly, restored on second chunk |
| Two different placeholder types in same chunk | Secrets restored first, then PII |
| `[[` at end of stream with no `]]` | `Flush()` outputs `[[` literally |
| Complete placeholder `[[X_1]]` in single chunk | Restored immediately, no buffering |
| `[[PERSON_1]]` followed by `[[PER` in same chunk | First restored, second buffered |
| Empty mapping contexts | `HasContexts()` returns false, no processing |
| Cross-block buffer contamination | Use `ReplaceDirect`/`ReplaceDirectJSON` for independent SSE data |
