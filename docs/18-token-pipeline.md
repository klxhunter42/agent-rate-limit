# Token Pipeline - Detailed Technical Reference

This document covers the 9 subsystems that form the token processing pipeline in the API gateway. Each module intercepts, optimizes, or analyzes requests and responses to reduce token consumption, detect waste, and improve latency.

---

## 1. Chunker (`api-gateway/chunker/`)

### Purpose

Splits large content into variable-size chunks using content-defined chunking (CDC), identifies "stable" (previously seen) chunks via Redis, and reorders output so stable chunks appear first. This enables downstream caching and deduplication of repeated content within and across sessions.

### Algorithm: Rabin-Karp Rolling Hash

The chunker uses a simplified rolling hash over a sliding window to find natural content boundaries:

```
for each position i in content:
    hash = sum(content[j] * 31) over window [i-windowSize, i]
    if hash % 256 == 0 AND chunk_size >= MinChunk:
        emit boundary
```

This produces chunks at content-dependent positions rather than fixed offsets, so small edits only affect nearby chunks (not the entire file).

### Configuration

| Env Var | Default | Description |
|---|---|---|
| `CHUNKER_ENABLED` | `true` | Enable/disable chunker |
| `CHUNKER_MIN_CHUNK` | `128` | Minimum chunk size in bytes |
| `CHUNKER_MAX_CHUNK` | `4096` | Maximum chunk size in bytes (hard boundary) |
| `CHUNKER_WINDOW_SIZE` | `48` | Sliding window size for rolling hash |
| `CHUNKER_STABLE_THRESHOLD` | `2` | Number of times a chunk must be seen to be "stable" |

### Chunk Hashing

Each chunk is hashed with SHA-256 (first 12 bytes of hex digest used as the chunk ID). This provides collision-resistant identification for the stability check.

### Stability Detection

Chunks are tracked in Redis with key pattern `chunker:stable:<hash>`:
- On each encounter, the counter is incremented (TTL 24h)
- A chunk is "stable" when its count >= `StableThreshold` (default 2)
- Stable chunks are reordered to the front of the output

### Reorder Strategy

```
Output = [stable chunks] + [novel chunks]
```

Stable-first ordering lets downstream consumers (delta encoder, cache) hit on known content early.

### Metrics

| Metric | Type | Labels |
|---|---|---|
| `api_gateway_chunker_chunks_total` | Counter | `type` (stable/novel) |
| `api_gateway_chunker_reorder_duration_seconds` | Histogram | - |
| `api_gateway_chunker_cache_hit_rate` | Gauge | - |
| `api_gateway_chunker_chars_saved_total` | Counter | - |

---

## 2. Tokenizer (`api-gateway/tokenizer/`)

### Purpose

Provides token estimation (without requiring tiktoken CGo bindings), content type detection, whitespace optimization, sentence deduplication, head-tail truncation, model capability lookup, and budget tracking.

### Token Estimation

No native tiktoken library is used. Instead, content-type-aware heuristic ratios are applied:

| Content Type | chars/token Ratio | Detection Method |
|---|---|---|
| Code | 2.5 | >30% lines match code indicators |
| JSON | 2.8 | Starts with `{` or `[` |
| Markdown | 3.5 | >20% lines match markdown patterns |
| Text | 4.0 | Default/fallback |

**`EstimateTokens(text)`**: Detects content type, divides char count by the ratio, returns `ceil(result)`.

**`QuickEstimateTokens(text)`**: Ultra-fast `(len(text) + 3) / 4` -- always chars/4 regardless of content type.

### Content Type Detection

`DetectContentType()` examines lines and computes ratios:

```
codeRatio = codeLines / nonEmptyLines
mdRatio   = mdLines   / nonEmptyLines

if codeRatio > 0.3 -> ContentCode
if mdRatio   > 0.2 -> ContentMarkdown
else               -> ContentText
```

JSON is detected by structural check (starts with `{`+ends with `}` or `[`+`]`) before line scanning.

Code indicator regex matches: `import`, `package`, `from`, `def`, `class`, `function`, `const`, `let`, `var`, `func`, `type`, `struct`, `interface`, `module`, `require`, `return`, `if`, `for`, `while`, `switch`, `case`, `pub`, `fn`, `use`, `mod`, `go`.

Markdown indicators: `#{1,6}\s`, list markers, blockquotes, code fences, table pipes.

### Whitespace Optimization

`OptimizeWhitespace(text)`:
1. Splits text into code blocks (fenced with `` ``` ``) and prose segments
2. Code blocks are preserved verbatim
3. Prose sections get: consecutive spaces collapsed to one, trailing whitespace trimmed, blank lines capped at 2
4. Returns optimized text and estimated tokens saved

### Sentence Deduplication

`DeduplicateSentences(text)`:
1. Splits prose into sentences at `[.!?]\s+` boundaries
2. Normalizes each sentence (lowercase, collapse whitespace, strip non-alphanumeric)
3. Keeps only first occurrence of each unique normalized sentence
4. **Privacy guard**: skips entirely if text contains `__SECRET_\d+__` or `__PII_\d+__` placeholders

### Semantic Deduplication

`DeduplicateSemantic(text, threshold)` (in `similarity.go`):
1. Uses Jaccard word-set similarity between sentences
2. Default threshold 0.7 -- sentences above this similarity are considered duplicates
3. Also respects privacy placeholder guard

### Similarity Functions

| Function | Algorithm | Use Case |
|---|---|---|
| `JaccardSimilarity(a, b, shingleSize)` | Shingle-set Jaccard | General text similarity |
| `DeduplicateSemantic(text, threshold)` | Word-set Jaccard per sentence | Near-duplicate sentence removal |
| `LevenshteinSimilarity(a, b)` | Edit distance, normalized | Short string comparison |
| `CosineSimilarity(a, b []float64)` | Dot product / magnitude | Vector comparison (used by warmstart) |

### Head-Tail Truncation

`TruncateHeadTail(text, maxChars, headRatio)`:
- Preserves `headRatio` (default 0.4) of lines from the start
- Preserves the remainder from the end
- Inserts marker: `[N lines truncated - showing first X + last Y lines]`
- **Privacy guard**: returns text unchanged if it contains privacy placeholders

### Model Capabilities

Static map `KnownModels` with context window and max output tokens:

| Model | Context Window | Max Output | Provider |
|---|---|---|---|
| `claude-opus-4-7` | 200,000 | 163,840 | anthropic |
| `claude-sonnet-4-6` | 200,000 | 163,840 | anthropic |
| `claude-haiku-4-5-20251001` | 200,000 | 8,192 | anthropic |
| `claude-3-5-sonnet-20241022` | 200,000 | 8,192 | anthropic |
| `claude-3-5-haiku-20241022` | 200,000 | 8,192 | anthropic |
| `gpt-4o` | 128,000 | 16,384 | openai |
| `gpt-4o-mini` | 128,000 | 16,384 | openai |
| `gpt-4-turbo` | 128,000 | 4,096 | openai |
| `o1` | 200,000 | 100,000 | openai |
| `o1-mini` | 128,000 | 65,536 | openai |
| `o3-mini` | 200,000 | 100,000 | openai |
| `gemini-2.5-pro` | 1,048,576 | 65,536 | google |
| `gemini-2.5-flash` | 1,048,576 | 65,536 | google |
| `gemini-2.0-flash` | 1,048,576 | 8,192 | google |
| `glm-5.1` | 128,000 | 4,096 | zai |
| `glm-5` | 128,000 | 4,096 | zai |
| `glm-4.6v` | 8,192 | 4,096 | zai |
| `glm-4-plus` | 128,000 | 4,096 | zai |
| `glm-4-flash` | 128,000 | 4,096 | zai |
| `glm-4-0520` | 128,000 | 4,096 | zai |
| `glm-4v-flash` | 8,192 | 4,096 | zai |

Unknown models fall back to `{128000, 4096, "unknown"}`. Prefix matching is attempted (e.g., `claude-opus-4-7-20250514` matches `claude-opus-4-7`).

### Token Budget Tracking

`TokenBudget` tracks cumulative token usage per session:

| Budget Level | Utilization | Behavior |
|---|---|---|
| `BudgetGreen` | < 50% | No optimization needed |
| `BudgetYellow` | 50-75% | `ShouldOptimize()` returns true |
| `BudgetRed` | > 75% | `ShouldForceOptimize()` returns true |

The budget level drives which optimization stages are activated in the pipeline.

---

## 3. Packer (`api-gateway/packer/`)

### Purpose

Implements a greedy 0/1 knapsack algorithm to select which context items to include in a request, given a token budget. Items below a minimum utility threshold are excluded.

### Algorithm: Greedy 0/1 Knapsack

```
1. Filter items by MinUtility (default 0.1)
2. Sort eligible items by utility/token ratio (descending)
3. Greedily pack items until budget exhausted
4. Excluded items' chars counted as "saved"
```

This is an approximation of the NP-complete 0/1 knapsack problem. The greedy approach prioritizes items with the highest value-per-token-cost ratio.

### Data Structures

```go
type Item struct {
    ID      string   // Unique identifier
    Content string   // Text content
    Tokens  int      // Token cost
    Utility float64  // Value score (0.0 - 1.0+)
}
```

### Configuration

| Env Var | Default | Description |
|---|---|---|
| `PACKER_ENABLED` | `true` | Enable/disable packer |
| `PACKER_MIN_UTILITY` | `0.1` | Minimum utility score for an item to be considered |

### Metrics

| Metric | Type | Labels |
|---|---|---|
| `api_gateway_packer_items_packed_total` | Counter | `result` (included/excluded) |
| `api_gateway_packer_budget_utilization` | Gauge | - |
| `api_gateway_packer_tokens_saved_total` | Counter | - |

---

## 4. Delta (`api-gateway/delta/`)

### Purpose

Computes line-based diffs between current and cached (baseline) content using Longest Common Subsequence (LCS). When the delta encoding is smaller than the full content by a minimum threshold, the delta is sent instead, reducing bandwidth.

### Algorithm: LCS-based Diff

1. Split old and new content into lines
2. Build full LCS dynamic programming table `O(m*n)`
3. Backtrack to produce edit operations: `=` (keep), `+` (insert), `-` (delete)
4. Reverse ops (backtrack produces reversed order)
5. Compact consecutive same-type ops (merge adjacent `+` or `-`)
6. Calculate savings: `len(new) - serializedSize(ops)`

### Serialization Format

```
<type_byte><length>:<data><type_byte><length>:<data>...
```

Example: `=14:Hello world!\n+6:Added\n-8:Removed!\n`

### Limits and Guards

| Constant | Value | Purpose |
|---|---|---|
| `maxLCSBytes` | 50,000 | Skip delta for content exceeding this size |
| `maxOps` | 200 | Skip if either input has more than 200 lines |
| `MinSavingsPct` | 10.0% | Don't use delta unless it saves at least this much |

### Delta Operations

| Op | Byte | Meaning |
|---|---|---|
| Keep | `=` | Copy N bytes from baseline |
| Insert | `+` | Add new data |
| Delete | `-` | Skip N bytes from baseline |

### Decode (Patch Application)

`Decode(delta, base)` reconstructs original content by applying ops to the baseline:
- `=`: Read N bytes from base pointer, advance
- `+`: Append data directly
- `-`: Skip N bytes in base pointer

### Baseline Storage

Redis key pattern: `delta:baseline:<cacheKey>`, TTL 24 hours.

### Configuration

| Env Var | Default | Description |
|---|---|---|
| `DELTA_ENABLED` | `true` | Enable/disable delta encoding |
| `DELTA_MIN_SAVINGS_PCT` | `10.0` | Minimum savings percentage to use delta |

### Metrics

| Metric | Type | Labels |
|---|---|---|
| `api_gateway_delta_encodes_total` | Counter | `result` (delta/passthrough) |
| `api_gateway_delta_chars_saved_total` | Counter | - |
| `api_gateway_delta_savings_pct` | Histogram | Buckets: 5, 10, 20, 30, 50, 70, 90 |

---

## 5. Summarizer (`api-gateway/summarizer/`)

### Purpose

Compresses conversation context using extractive summarization. Operates when the token budget reaches yellow/red levels.

### Summarization Strategy: Extractive Truncation

The current implementation uses extractive (not abstractive) summarization:

1. Split content into paragraphs (on `\n\n`)
2. For each paragraph, extract the first sentence (up to `.`, `!`, or `?` followed by space)
3. If no sentence boundary found, take first 200 chars + `...`
4. Accumulate sentences until reaching `MaxRatio` of original length (default 30%)
5. Return joined sentences as summary

### Configuration

| Env Var | Default | Description |
|---|---|---|
| `SUMMARIZER_ENABLED` | `true` | Enable/disable summarizer |
| `SUMMARIZER_MODEL` | `glm-4.7-flashx` | Target model for future LLM-based summarization |
| `SUMMARIZER_MAX_RATIO` | `0.3` | Maximum ratio of summary to original content |

### Caching

Summaries are cached in Redis by SHA-256 hash (first 8 bytes) of input content:
- Key pattern: `summarizer:cache:<hash>`
- TTL: 1 hour
- Cache lookup happens before any summarization work

### API

```go
Summarize(ctx, content, budgetLevel) -> (string, int)  // Returns summarized text and chars saved
IsSummarized(ctx, contentHash) -> (string, bool)        // Check if summary exists in cache
```

### Metrics

| Metric | Type | Labels |
|---|---|---|
| `api_gateway_summarizer_calls_total` | Counter | `method` (cached/truncation) |
| `api_gateway_summarizer_chars_saved_total` | Counter | `method` |
| `api_gateway_summarizer_duration_seconds` | Histogram | `method` |
| `api_gateway_summarizer_llm_tokens_total` | Counter | - |

---

## 6. Waste Detector (`api-gateway/waste/`)

### Purpose

Detects patterns of token waste across sessions by accumulating request records and running 7 detection heuristics. Runs as a background scanner.

### Detection Heuristics

#### 1. Empty Response (`empty_response`) - Severity: High

Triggers when >10% of requests in a session return zero output tokens.

```
if emptyCount / totalRequests > 0.1 -> alert
waste = totalInputTokens * emptyCount
```

**Suggestion**: Check upstream API health or model availability.

#### 2. Retry Churn (`retry_churn`) - Severity: Medium

Triggers when consecutive requests have identical input tokens but zero output, indicating failed retries.

```
if consecutive identical input + zero output AND totalWasted > 5000 -> alert
```

**Suggestion**: Investigate error handling and retry logic.

#### 3. Loop Detection (`loop_detection`) - Severity: High

Triggers when a cycle of 2+ requests repeats identically.

```
for cycle_size in 2..N/2:
    check if records[i] == records[i - cycle_size] for all i >= cycle_size
```

Detects the smallest repeating cycle and flags all iterations after the first cycle.

**Suggestion**: Add loop detection guard in agent logic.

#### 4. Oversized Context (`oversized_context`) - Severity: Medium

Triggers when multiple requests have input >100,000 tokens and cumulative excess exceeds 100,000.

```
for each request:
    if input > 100000:
        excess += input - 100000
if excess > 100000 -> alert
```

**Suggestion**: Enable context truncation or progressive disclosure.

#### 5. Budget Exceeded (`budget_exceeded`) - Severity: Medium

Triggers when a session uses more than 3 different models (budget sprawl).

```
if uniqueModelCount > 3 -> alert
waste = estimated as sum(input/2) / 2
```

**Suggestion**: Consolidate model usage for cost efficiency.

#### 6. Redundant Tool Call (`redundant_tool_call`) - Severity: Low

Triggers on consecutive identical request-response pairs.

```
if records[i].Input == records[i-1].Input && records[i].Output == records[i-1].Output -> redundant
```

**Suggestion**: Add caching or dedup for repeated tool calls.

#### 7. Low Value Response (`low_value_response`) - Severity: Low

Triggers when >=3 requests have >5,000 input tokens but <50 output tokens.

```
if input > 5000 && output < 50 -> low value
if lowValueCount >= 3 -> alert
```

**Suggestion**: Review prompts for unnecessary context injection.

### Session Management

- Records are stored in-memory per session ID
- Sessions with no activity for 30 minutes are evicted
- Scanning requires minimum `MinRequests` (default 10) per session

### Configuration

| Env Var | Default | Description |
|---|---|---|
| `WASTE_ENABLED` | `true` | Enable/disable waste detection |
| `WASTE_MIN_REQUESTS` | `10` | Minimum requests per session before scanning |

### Background Scanner

`StartBackgroundScanner(ctx, interval)` runs the scan on a ticker. The scan interval is configurable (default 60s from `ScanInterval`).

### Finding Output

```json
{
  "detector": "retry_churn",
  "severity": "medium",
  "message": "Repeated identical requests with no output - retry churn detected.",
  "tokens_wasted": 15000,
  "suggestion": "Investigate error handling and retry logic."
}
```

### Metrics

| Metric | Type | Labels |
|---|---|---|
| `api_gateway_waste_findings_total` | Counter | `detector`, `severity` |
| `api_gateway_waste_tokens_wasted_total` | Counter | `detector` |
| `api_gateway_waste_scan_duration_seconds` | Histogram | - |

---

## 7. Prefetcher (`api-gateway/prefetcher/`)

### Purpose

Predicts the next tool call in a session using a first-order Markov chain learned from observed tool call sequences. Pre-warms connections for predicted tools.

### Algorithm: First-Order Markov Chain

1. **Learning**: Each tool call is appended to a per-session history in Redis. Transition counts are recorded: `P(tool_B | tool_A)`.
2. **Prediction**: Given the last tool call, look up the transition table, sort by count descending, return top-K predictions with confidence scores.
3. **Pre-warming**: Store predictions in Redis for downstream consumers to use.

### Data Structures

```go
type Prediction struct {
    Tool       string   // Predicted tool name
    Confidence float64  // Probability score (0.0 - 1.0)
}
```

### Redis Key Patterns

| Key | Purpose | TTL |
|---|---|---|
| `prefetcher:chain:<sessionID>` | Tool call history (list) | 4 hours |
| `prefetcher:trans:<toolName>` | Transition counts (hash) | 4 hours |
| `prefetcher:last_pred:<tool>` | Last prediction for tool | 1 minute |

### Configuration

| Env Var | Default | Description |
|---|---|---|
| `PREFETCHER_ENABLED` | `true` | Enable/disable prefetcher |
| `PREFETCHER_MAX_ORDER` | `5` | Maximum history length per session |
| `PREFETCHER_TOP_K` | `3` | Number of predictions to return |

### API

```go
Record(ctx, sessionID, toolCall)                      // Learn from observed tool call
Predict(ctx, sessionID) -> []Prediction                // Predict next K tool calls
PreWarm(ctx, predictions)                              // Pre-initialize predicted tools
```

### Confidence Calculation

```
confidence(tool) = count(transition to tool) / sum(all transition counts from last tool)
```

### Metrics

| Metric | Type | Labels |
|---|---|---|
| `api_gateway_prefetcher_predictions_total` | Counter | `correct` |
| `api_gateway_prefetcher_order_used` | Histogram | Buckets: 1, 2, 3, 4, 5 |
| `api_gateway_prefetcher_prewarm_duration_seconds` | Histogram | - |

---

## 8. Warm Start (`api-gateway/warmstart/`)

### Purpose

Finds similar past sessions using cosine similarity on 32-dimensional feature vectors, enabling pre-population of optimizer state for new sessions.

### Feature Vector (32 Dimensions)

| Dims | Feature | Encoding | Source Key |
|---|---|---|---|
| 0-3 | Model type | One-hot: claude, gpt/o1/o3, gemini, glm | `model` |
| 4-7 | Content type distribution | Ratio (0.0-1.0) | `code_ratio`, `json_ratio`, `md_ratio`, `text_ratio` |
| 8-15 | Tool call frequency | Normalized counts (top 8 tools) | `tool_counts` |
| 16-18 | Budget level distribution | Percentage | `budget_green_pct`, `budget_yellow_pct`, `budget_red_pct` |
| 19-22 | Request size buckets | Normalized: tokens/10000, tokens/1000, count/100, ms/10000 | `avg_input_tokens`, `avg_output_tokens`, `total_requests`, `avg_duration_ms` |
| 23-27 | Intent distribution | Percentage (0.0-1.0) | `intent_code_pct`, `intent_analysis_pct`, `intent_search_pct`, `intent_action_pct`, `intent_chat_pct` |
| 28-31 | Project/context fingerprint | Hash, density, stream%, error% | `project_hash`, `symbol_density`, `stream_pct`, `error_rate` |

### Algorithm

1. Compute 32-dim signature from session metadata
2. Scan Redis for all stored signatures matching project root
3. Compute cosine similarity against each stored signature
4. If best match >= `MinSimilar` (default 0.5), use it for warm start
5. Store current signature for future lookups

### Cosine Similarity

```
sim(A, B) = dot(A, B) / (||A|| * ||B||)
```

### Redis Storage

| Key | Value | TTL |
|---|---|---|
| `warmstart:sig:<projectRoot>:<sessionID>` | JSON-encoded `[32]float64` | 7 days |

### Configuration

| Env Var | Default | Description |
|---|---|---|
| `WARMSTART_ENABLED` | `true` | Enable/disable warm start |
| `WARMSTART_TOP_K` | `3` | Number of similar sessions to find |
| `WARMSTART_MIN_SIMILARITY` | `0.5` | Minimum cosine similarity threshold |

### Metrics

| Metric | Type | Labels |
|---|---|---|
| `api_gateway_warmstart_sessions_warmed_total` | Counter | `result` (hit/miss) |
| `api_gateway_warmstart_similarity_score` | Histogram | Buckets: 0, 0.1, 0.3, 0.5, 0.7, 0.8, 0.9, 0.95, 1.0 |
| `api_gateway_warmstart_warmup_duration_seconds` | Histogram | - |

---

## 9. Token Pipeline Flow Diagram

```
                              INCOMING REQUEST
                                    |
                                    v
                        +-----------------------+
                        |    1. TOKENIZER       |
                        |                       |
                        |  - DetectContentType  |
                        |  - EstimateTokens     |
                        |  - ComputeTokenBudget |
                        |  - BudgetLevel check  |
                        +----------+------------+
                                   |
                          BudgetLevel?
                          /         \
                   GREEN /           \ YELLOW/RED
                        /             \
                       v               v
                [Pass through]   +------------------+
                       |         | 2. OPTIMIZER      |
                       |         |                    |
                       |         | Pipeline stages:   |
                       |         |  a. WhitespaceOpt  |
                       |         |  b. DedupSentences  |
                       |         |  c. SemanticDedup   |
                       |         |  d. TruncateHeadTail|
                       |         +--------+-----------+
                       |                  |
                       |                  v
                       |         +------------------+
                       |         | 3. PACKER         |
                       |         |                    |
                       |         | Greedy knapsack:   |
                       |         | - Filter by utility|
                       |         | - Sort by ratio    |
                       |         | - Pack in budget   |
                       |         +--------+-----------+
                       |                  |
                       +--------+---------+
                                |
                                v
                    +-----------------------+
                    |    4. CHUNKER         |
                    |                       |
                    |  - Rabin-Karp CDC     |
                    |  - SHA-256 per chunk  |
                    |  - Redis stability    |
                    |  - Reorder stable 1st |
                    +----------+------------+
                               |
                               v
                    +-----------------------+
                    |    5. DELTA           |
                    |                       |
                    |  - Fetch baseline     |
                    |  - LCS diff compute   |
                    |  - Savings >= 10%?    |
                    |  - Yes: send delta    |
                    |  - No:  send full     |
                    |  - Store new baseline |
                    +----------+------------+
                               |
                               v
                    +-----------------------+
                    |    6. SUMMARIZER      |
                    |  (if budget > 50%)    |
                    |                       |
                    |  - Check Redis cache  |
                    |  - Extractive: first  |
                    |    sentence/paragraph |
                    |  - Cap at 30% of orig |
                    |  - Cache result 1hr   |
                    +----------+------------+
                               |
                               v
                    +-----------------------+
                    |    7. PREFETCHER      |
                    |                       |
                    |  - Record tool call   |
                    |  - Predict next K     |
                    |    via Markov chain   |
                    |  - Pre-warm predicted |
                    +----------+------------+
                               |
                               v
                        +-------------+
                        |   REQUEST    |
                        |   TO MODEL   |
                        +------+------+
                               |
                               v
                        +-------------+
                        |   RESPONSE   |
                        +------+------+
                               |
                               v
                    +-----------------------+
                    |    8. WASTE DETECTOR  |
                    |                       |
                    |  Record per session:  |
                    |  - empty_response     |
                    |  - retry_churn        |
                    |  - loop_detection     |
                    |  - oversized_context  |
                    |  - budget_exceeded    |
                    |  - redundant_tool     |
                    |  - low_value_response |
                    |                       |
                    | Background scanner    |
                    | at 60s intervals      |
                    +----------+------------+
                               |
                               v
                    +-----------------------+
                    |    9. WARM START      |
                    |  (on session start)   |
                    |                       |
                    |  - Compute 32-dim     |
                    |    feature vector     |
                    |  - Find similar past  |
                    |    session via cosine |
                    |  - Pre-populate       |
                    |    optimizer state    |
                    |  - Store signature    |
                    +-----------------------+


BACKGROUNDS:
  - WasteDetector: scans sessions every 60s
  - Prefetcher: learns Markov transitions per tool call
  - WarmStart: signature lookup on session init
  - Chunker: stable chunk counter (Redis, 24h TTL)

DATA STORES:
  - Redis: chunk stability, delta baselines, summary cache,
           prefetcher transitions, warm start signatures
  - In-memory: waste detector session records (30min eviction)
```

---

## 10. TextComp (`api-gateway/textcomp/`)

### Purpose

Regex-based text compression that removes filler phrases, hedge words, and verbose constructs from prompts. Inspired by [tokenshrink](https://github.com/voxel-hub/tokenshrink) - reduces input token count without changing meaning.

### Algorithm: Mask-Apply-Unmask

```
Input text
  -> Phase 1: Mask protected regions (code blocks, URLs, quoted strings)
  -> Phase 2: Apply regex compression rules (filler/hedge/verbose removal)
  -> Phase 3: Unmask protected regions (restore originals)
  -> Phase 4: Final cleanup (collapse multi-spaces, trim)
  -> Compressed text + char savings count
```

Protected regions ensure code, URLs, and quoted content are never modified.

### Compression Rules

| Category | Count | Examples |
|---|---|---|
| Filler removal | 10 | "I would like to", "Could you please", "Kindly" |
| Hedge removal | 12 | "sort of", "basically", "just", "really" |
| Verbose-to-compact | 30 | "due to the fact that" -> "because", "in order to" -> "to" |
| Aggressive-only | 11 | "I think that", "It seems that" (mode=aggressive only) |

### Modes

| Mode | Rules Applied | Use Case |
|---|---|---|
| `balanced` | Filler + hedge + verbose (52 rules) | Default, safe for all prompts |
| `aggressive` | All 63 rules | Maximum compression, may alter tone |

### Configuration

| Env Var | Default | Description |
|---|---|---|
| `TEXTCOMP_ENABLED` | `true` | Enable/disable TextComp |
| `TEXTCOMP_MODE` | `balanced` | Compression mode: `balanced` or `aggressive` |

### Integration Points

TextComp runs as stage F17 in the optimizer pipeline:
- **OptimizeSystemPrompt**: compresses system prompt text (all modes including transparent)
- **OptimizeMessages**: compresses string content in user/assistant messages

Unlike Caveman (F16), TextComp does NOT add input tokens - it only removes or shortens existing text. Safe for transparent claude-oauth passthrough.

### Prometheus Metrics

- `api_gateway_optimizer_runs_total{technique="textcomp"}`
- `api_gateway_optimizer_chars_saved_total{technique="textcomp"}`
- `api_gateway_optimizer_duration_seconds{technique="textcomp"}`
- `api_gateway_optimizer_runs_total{technique="message_textcomp"}`
- `api_gateway_optimizer_chars_saved_total{technique="message_textcomp"}`
- `api_gateway_profile_optimizer_chars_saved_total{profile, technique}` - per-profile character savings from optimization
- `api_gateway_optimizer_tokens_saved_total` - total tokens saved (previously always 0 due to budgetLevel bug, now fixed)
- `api_gateway_cost_savings_total` - estimated cost savings from token optimization

### Grafana Dashboard

The "AI Gateway - Service Dashboard" (uid: `arl-service-dashboard`, file: `grafana/provisioning/dashboards/service-dashboard.json`) provides TextComp and optimizer visibility:

- Before/after optimization comparison
- Top 5 profile usage by requests/tokens/cost
- Profile usage over time
- Model distribution
- Per-profile optimization savings

---

## Cross-Cutting Concerns

### Privacy Placeholder Protection

Multiple modules check for privacy placeholders (`__SECRET_\d+__`, `__PII_\d+__`) injected by the privacy guard and skip destructive optimizations to avoid corrupting them. Affected modules:
- `DeduplicateSentences` -- skips entirely
- `DeduplicateSemantic` -- skips entirely
- `TruncateHeadTail` -- returns text unchanged
- `OptimizeWhitespace` -- preserves placeholders within prose

### Redis Dependency

The following modules require a Redis connection:
- **Chunker**: stable chunk tracking (24h TTL)
- **Delta**: baseline storage (24h TTL)
- **Summarizer**: summary cache (1h TTL)
- **Prefetcher**: Markov chain state (4h TTL)
- **WarmStart**: session signatures (7d TTL)

Modules gracefully degrade when Redis is unavailable (pass-through behavior).

### Prometheus Metrics

All 8 modules expose Prometheus metrics under the `api_gateway_` namespace with consistent naming:
- `api_gateway_chunker_*` (4 metrics)
- `api_gateway_packer_*` (3 metrics)
- `api_gateway_delta_*` (3 metrics)
- `api_gateway_summarizer_*` (4 metrics)
- `api_gateway_waste_*` (3 metrics)
- `api_gateway_prefetcher_*` (3 metrics)
- `api_gateway_warmstart_*` (3 metrics)

#### Profile-Level Optimization Metrics

- `api_gateway_profile_optimizer_chars_saved_total{profile, technique}` - per-profile character savings from optimization
- `api_gateway_optimizer_tokens_saved_total` - total tokens saved across all requests (was previously always 0 due to budgetLevel propagation bug, now fixed)
- `api_gateway_cost_savings_total` - estimated dollar cost savings from token optimization

### Configuration Pattern

All modules follow the same configuration pattern:
- Environment variables for all tunables
- `LoadConfig()` function with sensible defaults
- `Enabled` flag for feature toggle
- `New(reg, rdb)` constructor with Prometheus registerer and optional Redis client
