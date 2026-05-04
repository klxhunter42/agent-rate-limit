# Rate Limiting and Token Optimization Architecture

This document covers four subsystems that work together to control throughput, reduce token waste, and maximize upstream API efficiency:

- **Bandit** (F5) - LinUCB multi-armed bandit for adaptive model/strategy selection
- **Sketch** (F9) - SimHash-based near-duplicate detection for prompt deduplication
- **Queue** (Dragonfly) - Redis-backed job queue for async request processing and result caching
- **Caveman** (F16) - Prompt compression via system prompt injection across 4 tiers

Plus the **AdaptiveLimiter** and **distributed rate limiter** that govern concurrent upstream access.

---

## 1. Bandit Algorithm (LinUCB)

**Package:** `api-gateway/bandit/`
**File:** `bandit.go`
**Role:** Adaptive decision engine that learns which optimization strategies produce the best reward (output/input ratio) and steers future requests accordingly.

### Algorithm: LinUCB (Linear Upper Confidence Bound)

This is a contextual bandit variant. Unlike standard UCB or Thompson Sampling, LinUCB models the expected reward as a linear function of context features, allowing it to generalize across request patterns.

**Core formula** (arm selection, line 117 of `bandit.go`):

```
score = theta^T * phi + alpha * sqrt(|phi^T * A^-1 * phi|)
         ^^^^^^^^   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
         mean        uncertainty bonus (exploration)
```

Where:
- `theta = A^-1 * b` -- learned weight vector for the arm (10 dimensions)
- `phi` -- context feature vector (10 dimensions) from the current request
- `A` -- 10x10 Gram matrix (`phi * phi^T` accumulated over observations), initialized to identity
- `b` -- 10-element reward vector (`reward * phi` accumulated over observations)
- `alpha` -- exploration parameter (default 1.0, env `BANDIT_ALPHA`)
- The `A^-1` term is computed via Gauss-Jordan elimination on every `Select()` call

### Data Structures

```
armState {
    A [10][10]float64   // Design matrix (starts as identity)
    B [10]float64       // Reward vector
}
```

- Dimensionality: 10 (`const dim = 10`)
- Arms are keyed by string ID (e.g., model names like "glm-5.1")
- State is persisted per-arm in Redis at key `bandit:state:<armID>` with 24h TTL
- States are loaded lazily on `Select()` calls (no startup pre-load)

### Select/Update Flow

1. **Select(ctx, features)** -- Given a 10-element context vector:
   - Loads all arm states from Redis
   - For each arm, computes `theta = A^-1 * b`, then `mean = theta . phi`
   - Computes uncertainty as `sqrt(|phi^T * A^-1 * phi|)`
   - Scores = `mean + alpha * uncertainty`
   - Picks highest-scoring arm
   - Flags as `exploratory` when `|variance| > 1.0` (high uncertainty region)

2. **Update(ctx, armID, features, reward)** -- After response completes:
   - `A += phi * phi^T` (outer product update)
   - `B += reward * phi` (weighted feature update)
   - Persists updated state to Redis

### Reward Calculation

Defined in `PostProxyFeedback()` (optimizers.go, line 229-241):

```
reward = 0.0                     if output == 0 (empty response)
reward = output / input          otherwise (capped at 1.0)
```

This incentivizes the bandit to prefer strategies that maximize output-per-input-token (efficiency).

### Configuration (env vars)

| Variable | Default | Description |
|----------|---------|-------------|
| `BANDIT_ENABLED` | `true` | Enable/disable bandit |
| `BANDIT_ALPHA` | `1.0` | Exploration coefficient (higher = more exploration) |
| `BANDIT_DECAY` | `0.99` | Decay factor (reserved for future use) |

### Prometheus Metrics

| Metric | Type | Labels |
|--------|------|--------|
| `api_gateway_bandit_selections_total` | Counter | `arm`, `exploratory` |
| `api_gateway_bandit_reward_total` | Counter | `arm` |
| `api_gateway_bandit_selection_duration_seconds` | Histogram | - |

### Computational Notes

- Matrix inversion is O(dim^3) = O(1000) per arm per select call -- acceptable at 10 dimensions
- No decay is applied to `A` or `B` currently (the `Decay` config field exists but is unused)
- Arms are initialized with `A = I` (identity), meaning the first prediction is purely from the uncertainty bonus

---

## 2. Sketch Data Structure (SimHash Bit Vector)

**Package:** `api-gateway/sketch/`
**File:** `sketch.go`
**Role:** Near-duplicate detection for system prompts to avoid sending semantically identical content upstream.

### Algorithm: SimHash-style Bit Sketch

This is NOT a Count-Min Sketch. It is a locality-sensitive hashing (LSH) scheme using bit vectors:

1. **Tokenize** the content into words (alphanumeric sequences)
2. **Hash each word** with FNV-1a (32-bit)
3. **Set 3 bits per word** in a fixed-size bit vector using positions derived from the hash:
   - Position `k` = `(hash >> (k*8)) % dimensions` for k=0,1,2
4. **Compare sketches** via normalized Hamming similarity (fraction of matching bits)

### Data Structure

```
Bit vector: ceil(dimensions / 8) bytes
```

- Default dimensions: 128 bits = 16 bytes per sketch (env `SKETCH_DIMENSIONS`)
- Each word sets 3 bit positions (from FNV-1a hash)
- Stored in Redis as hex-encoded strings at key `sketch:recent:<sessionID>`
- Recent sketches list capped at 100 entries per session (`LTrim -100 -1`)
- Sketch list TTL: 24 hours

### Similarity Check

`Similarity(a, b)` computes normalized Hamming similarity:

```
similarity = (total_bits - hamming_distance(a XOR b)) / total_bits
```

Range: 0.0 (completely different) to 1.0 (identical). Threshold default: 0.85 (env `SKETCH_THRESHOLD`).

### CheckAndStore Flow

```
Input: sessionID, content
  1. Compute sketch bit vector for content
  2. Load recent sketches for this session from Redis (up to 100)
  3. Compare against each stored sketch:
     - If similarity >= threshold (0.85): DUPLICATE
       - Return (true, sessionID, len(content)) -- chars saved
     - If similarity < threshold: continue checking
  4. If no duplicate found:
     - Store new sketch via RPUSH
     - Trim list to 100 entries
     - Set 24h expiry
     - Return (false, "", 0)
```

### FNV-1a Hash Implementation

```go
h = offset_basis (2166136261)
for each byte:
    h ^= byte
    h *= 16777619 (FNV prime)
return h
```

Standard FNV-1a 32-bit. Good distribution, fast computation, no cryptographic requirements.

### Configuration (env vars)

| Variable | Default | Description |
|----------|---------|-------------|
| `SKETCH_ENABLED` | `true` | Enable/disable sketch dedup |
| `SKETCH_DIMENSIONS` | `128` | Bit vector width |
| `SKETCH_THRESHOLD` | `0.85` | Similarity threshold for duplicate detection |

### Prometheus Metrics

| Metric | Type | Labels |
|--------|------|--------|
| `api_gateway_sketch_checks_total` | Counter | `result` (duplicate/unique) |
| `api_gateway_sketch_hamming_similarity` | Histogram | - (buckets: 0, 0.1, 0.3, 0.5, 0.7, 0.85, 0.9, 0.95, 1.0) |
| `api_gateway_sketch_chars_saved_total` | Counter | - |

### Accuracy and Memory

- **Per-sketch size:** 16 bytes (128 bits / 8)
- **Per-session storage:** Up to 100 * 16 bytes = 1.6 KB + Redis list overhead
- **False positive rate:** Depends on threshold. At 0.85, strings with ~85% bit overlap are flagged. Higher threshold = fewer false positives, more false negatives.
- **Collision probability:** With 128 dimensions and 3 bits per word, typical content (50-200 words) sets ~30-100 unique bits. Two random documents sharing 85%+ of bits is low probability.

---

## 3. Queue System (Dragonfly/Redis)

**Package:** `api-gateway/queue/`
**File:** `dragonfly.go`
**Role:** Redis-backed FIFO job queue for async AI inference request processing and result caching.

### Architecture

Uses Dragonfly (Redis-compatible) as the queue backend via `go-redis/v9` client.

### Job Structure

```go
type Job struct {
    RequestID   string             // Unique request identifier
    AgentID     string             // Agent that submitted the request
    Model       string             // Target model (e.g., "glm-5.1")
    Messages    []map[string]any   // Chat messages
    MaxTokens   int                // Max output tokens
    Temperature float64            // Sampling temperature
    Provider    string             // Upstream provider
    RetryCount  int                // Retry attempt count
    Metadata    map[string]string  // Arbitrary metadata
}
```

### Queue Operations

| Operation | Redis Command | Timeout | Description |
|-----------|---------------|---------|-------------|
| `PushJob` | `LPUSH` | 3s | Push job JSON to head of list |
| `QueueDepth` | `LLEN` | 2s | Get current queue length |
| `GetResult` | `GET` | 2s | Retrieve cached result by request ID |
| `SetResult` | `SET` with TTL | 2s | Store result with custom TTL |
| `SetResultWithDefaultTTL` | `SET` with 10m TTL | 2s | Store result with default TTL |

### Connection Pool Configuration

```
PoolSize:         50     // High throughput: 50 connections per CPU
MinIdleConns:     10     // Keep 10 warm connections
ConnMaxIdleTime:  5 min  // Reap idle connections after 5 min
ConnMaxLifetime:  30 min // Rotate connections every 30 min
DialTimeout:      3s
ReadTimeout:      3s
WriteTimeout:     3s
PoolTimeout:      4s     // Wait for connection from pool
```

### Queue Behavior

- **Ordering:** FIFO via `LPUSH` (newest at head, consumers use `RPOP` or `BRPOP`)
- **Backpressure:** No explicit queue capacity limit. Relies on Redis memory limits and consumer throughput.
- **Overflow behavior:** `LPUSH` always succeeds (unbounded list). Overflow is handled at the consumer side or Redis memory eviction policy.
- **Async push:** `PushJob` is called in a goroutine from the handler to avoid blocking the HTTP response.
- **Result caching:** Results stored at key `result:<requestID>` with 10-minute default TTL.

### Timeout Constants

```go
pushTimeout  = 3s   // LPUSH timeout
getTimeout   = 2s   // GET/LLEN timeout
setTimeout   = 2s   // SET timeout
defaultTTL   = 10m  // Result cache TTL
```

### Key Patterns

| Redis Key | Type | TTL | Description |
|-----------|------|-----|-------------|
| `<queueName>` | List | - | Job queue (configured via `cfg.QueueName`) |
| `result:<requestID>` | String | 10m | Cached inference result |

### Integration

In the handler, jobs are pushed asynchronously after request validation:

```go
go func() {
    if err := h.queue.PushJob(context.Background(), job); err != nil {
        slog.Error("failed to push job to queue", ...)
        h.metrics.IncError("queue_push")
    }
}()
```

Result retrieval is synchronous via `GET /result/:requestID` endpoint.

---

## 4. Caveman (Prompt Compression via Injection)

**Package:** `api-gateway/caveman/`
**File:** `caveman.go`
**Role:** Token budget saver that appends compressed output-style directives to the system prompt, instructing the downstream LLM to produce shorter responses.

### What Caveman Does

Caveman is NOT traditional text compression (gzip, LZMA, etc.). It is **prompt engineering injection** that appends an `[OUTPUT STYLE]` directive to the system prompt. This instructs the LLM to produce more concise output, effectively reducing the response token count without changing the input content.

The name "Caveman" refers to the progression from verbose modern language to increasingly terse communication styles.

### Compression Tiers

| Tier | Index | Trigger | Estimated Ratio | Style |
|------|-------|---------|-----------------|-------|
| `TierLite` | 0 | Budget green (0) or content < 500 chars | 0.7 | Bullet points, skip pleasantries |
| `TierFull` | 1 | Budget yellow (1) | 0.5 | Code only, terse, one-line answers |
| `TierUltra` | 2 | Budget red (2) | 0.25 | Raw output, no markdown, compressed notation |
| `TierWenyan` | 3 | (Reserved) | 0.3 | Classical notation, minimal grammar |

### Tier Selection Logic

`ShouldCompress(content, budgetLevel)`:

1. If disabled -> skip
2. If content length < `CAVEMAN_MIN_SIZE` (default 500) -> skip
3. If auto-detect disabled -> always `TierFull`
4. Budget-based selection:
   - Budget 0 (green) -> `TierLite`
   - Budget 1 (yellow) -> `TierFull`
   - Budget 2 (red) -> `TierUltra`

### Injection Content

Each tier appends a specific directive block. Example for `TierLite`:

```
[OUTPUT STYLE -- lite]
Be concise. Use bullet points. Skip pleasantries and filler phrases.
Avoid: "Great question!", "Certainly!", "I'd be happy to help!", ...
One sentence answers when possible.
```

### Validation

`Validate(original, compressed)` checks that compression did not destroy essential structure:

1. **Code block preservation:** Counts ```` `` ````-delimited blocks. If any are lost, validation fails.
2. **Identifier preservation:** Extracts up to 20 unique identifiers (words > 3 chars, alphanumeric + `_-`). If less than 80% of identifiers survive, validation fails.

### Configuration (env vars)

| Variable | Default | Description |
|----------|---------|-------------|
| `CAVEMAN_ENABLED` | `true` | Enable/disable compression |
| `CAVEMAN_AUTO_DETECT` | `true` | Auto-select tier based on budget |
| `CAVEMAN_MIN_SIZE` | `500` | Minimum content length to compress |

### Prometheus Metrics

| Metric | Type | Labels |
|--------|------|--------|
| `api_gateway_caveman_compressions_total` | Counter | `tier`, `result` (valid/invalid/skipped) |
| `api_gateway_caveman_compression_ratio` | Histogram | - (buckets: 0.1-1.0) |
| `api_gateway_caveman_validation_duration_seconds` | Histogram | - |

### Integration in Optimizer Pipeline

Caveman runs as stage F16 (last in the pipeline). The compressed directive is appended to the system prompt text:

```go
// In OptimizeSystemPrompt (optimizers.go)
if o.Caveman != nil {
    shouldCompress, tier := o.Caveman.ShouldCompress(text, budgetLevel)
    if shouldCompress {
        compressed, _ := o.Caveman.Compress("", tier)
        text = text + compressed  // Append directive to prompt
    }
}
```

Note: `Compress` receives an empty string as the base prompt (the injection is appended, not prepended). The actual system prompt text is concatenated with the injection in the caller.

---

## 5. Rate Limiting Architecture

### Multi-Layer Rate Control

The gateway implements rate limiting at three distinct layers:

```
Layer 1: Distributed Rate Limiter (external service)
   |-- Global rate limit (all requests)
   |-- Per-agent rate limit (per API key)
   |
Layer 2: Adaptive Concurrency Limiter (in-process)
   |-- Per-model concurrency limits
   |-- Global concurrency cap
   |-- Auto-adjustment based on 429 feedback
   |-- Cross-series model fallback
   |
Layer 3: Token Budget Optimization (optimizer pipeline)
   |-- 13+ optimization stages reduce token usage
   |-- Bandit learns best strategies
   |-- Sketch detects duplicate prompts
   |-- Caveman compresses output style
```

### Adaptive Concurrency Limiter (AdaptiveLimiter)

**Package:** `api-giddleware/adaptive_limiter.go`
**Algorithm:** Envoy gradient controller + Netflix concurrency limits

**Key behavior:**

| Event | Action |
|-------|--------|
| Upstream 429/503 | `limit = max(minLimit, limit * 0.5)` -- halve the limit |
| Upstream 200 (every 5th) | `gradient = (minRTT + buffer) / sampleRTT` then `limit = min(maxLimit, gradient * limit + sqrt(limit))` |
| Within 5s cooldown after 429 | No limit increase |
| Learned ceiling | After a 429, the pre-429 limit is stored as a ceiling. New limit cannot exceed this for 5 minutes. |
| Manual override | Pins limit to a specific value, disables auto-adjustment |

**Model series and fallback:**
- Models are grouped by series (e.g., "glm-5.1" and "glm-5-turbo" are both series 5)
- When a series is >= 70% utilized, 20% of traffic is proactively distributed to lower series
- On same-series exhaustion, spillover to lower series is triggered by:
  - Same-series full (all models at limit)
  - Recent 429 on the series (within 30s)
  - Latency pressure (EWMA RTT > 1.2x minRTT for majority of models in series)

**Concurrency primitives:**
- All hot-path operations use atomic CAS (no locks)
- `sync.Cond` for slot-waiting (no spin-wait)
- `sync.Pool` for candidate slice reuse (reduce GC pressure)
- Global timeout: 60s for initial slot, 30s for fallback

**Model priority (default):**

| Model | Priority |
|-------|----------|
| glm-5.1 | 100 |
| glm-5-turbo | 90 |
| glm-5 | 80 |
| glm-4.7 | 70 |
| glm-4.6 | 60 |
| glm-4.5 | 50 |

### Distributed Rate Limiter Middleware

**Package:** `api-gateway/middleware/ratelimit.go`

Calls an external rate limiter service via HTTP POST:

```
POST <rateLimiterCheckURL>
{"key": "global", "tokens": 1}
{"key": "agent:<agentID>", "tokens": 1}
```

- Global and agent checks run in parallel (errgroup)
- Fail-open: if the rate limiter service is unreachable, requests are allowed through
- Returns Anthropic-format 429 errors for `/v1/messages` paths
- Internal paths (`/metrics`, `/health`, `/ws`) bypass rate limiting
- 3-second timeout per check

### 13-Stage Optimizer Pipeline

The `Optimizers` struct wires all optimization stages together. Applied to every system prompt in this order:

| Stage | Component | Trigger | Effect |
|-------|-----------|---------|--------|
| F7 | Semantic Dedup | Always | Remove duplicate sentences (>0.7 similarity) |
| F1 | Chunker | Always | Chunk and reorder content |
| F8 | Delta Encoding | Always | Encode diffs from previous prompts |
| F9 | Sketch Dedup | Always | Skip near-duplicate prompts (>=0.85 similarity) |
| F6 | Summarizer | Budget red (2) only | Summarize long system prompts |
| F13 | Intent Filter | Always | Classify intent, filter irrelevant content |
| F16 | Caveman | Content >= 500 chars | Append output-style compression directive |

Post-proxy feedback loop:

| Stage | Component | Trigger | Data |
|-------|-----------|---------|------|
| F4 | Prefetcher | After response | Record session/model for warm cache |
| F11 | Waste Detection | After response | Track input/output ratio |
| F14 | Cache ROI | After response | Estimate cache hit savings |
| F5 | Bandit Feedback | After response | Update arm rewards (output/input ratio) |

---

## 6. Architecture Flow Diagram

```
                              Incoming Request
                                    |
                                    v
                        +-----------------------+
                        |  HTTP Middleware Chain |
                        +-----------------------+
                                    |
                          +---------+---------+
                          |                   |
                   +------+------+    +-------+-------+
                   | Distributed |    |   Internal    |
                   | Rate Limiter |    |  path bypass  |
                   | (global +    |    | (/health,     |
                   |  per-agent)  |    |  /metrics)    |
                   +------+------+    +---------------+
                          |
                     [allowed?]
                    /          \
                  no            yes
                   |             |
              +----+----+       v
              | 429     |  +------------------------+
              | Response|  | Adaptive Concurrency    |
              +---------+  | Limiter (Acquire slot)  |
                           |                          |
                           | Per-model limits,        |
                           | series fallback,         |
                           | cross-series spillover   |
                           +-----------+------------+
                                       |
                                  [slot acquired?]
                                 /                \
                               no                  yes
                                |                   |
                        +-------+-----+            v
                        | 503 Timeout |  +-------------------------+
                        | Response    |  | 13-Stage Optimizer      |
                        +-------------+  | Pipeline                |
                                         |                         |
                                         | 1. Semantic Dedup (F7)  |
                                         | 2. Chunker (F1)         |
                                         | 3. Delta Encode (F8)    |
                                         | 4. Sketch Dedup (F9)    |
                                         |    - SimHash bit vector  |
                                         |    - Hamming similarity  |
                                         |    - Redis-backed cache  |
                                         | 5. Summarizer (F6, red) |
                                         | 6. Intent Filter (F13)  |
                                         | 7. Caveman (F16)        |
                                         |    - Budget-based tier  |
                                         |    - Prompt injection   |
                                         +----------+--------------+
                                                    |
                                                    v
                                         +---------------------+
                                         |  Upstream Proxy     |
                                         |  (Anthropic/OpenAI/ |
                                         |   Gemini/Z.AI)      |
                                         +----------+----------+
                                                    |
                                            [upstream response]
                                                    |
                                         +----------+----------+
                                         |                     |
                                    [429/503]             [200 OK]
                                         |                     |
                                         v                     v
                                   +-----+------+    +---------+--------+
                                   | Adaptive   |    | Post-Proxy       |
                                   | Feedback   |    | Feedback Loop    |
                                   |            |    |                  |
                                   | - Halve    |    | - Bandit Update  |
                                   |   limit    |    |   (reward =      |
                                   | - Record   |    |    output/input) |
                                   |   peak     |    | - Prefetcher     |
                                   | - 5s       |    |   Record         |
                                   |   cooldown |    | - Waste Detect   |
                                   +------------+    | - Cache ROI      |
                                                     +------------------+
                                                            |
                                                            v
                                                  +-------------------+
                                                  | Release Concurrency|
                                                  | Slot (AdaptiveLimiter)
                                                  |                   |
                                                  | - Signal waiters  |
                                                  | - Update RTT EWMA |
                                                  +-------------------+
                                                            |
                                                            v
                                                    +-------+------+
                                                    |              |
                                              [async path]   [sync path]
                                                    |              |
                                                    v              v
                                           +--------+---+  +------+------+
                                           | Dragonfly   |  | HTTP Response|
                                           | Job Queue   |  | to client    |
                                           |             |  +-------------+
                                           | LPUSH job   |
                                           | Result cache |
                                           | (10m TTL)    |
                                           +-------------+

    +--------------------------------------------------------------+
    |                    Redis/Dragonfly State                      |
    |                                                               |
    |  bandit:state:<armID>    Arm state (A, b matrices)  24h TTL  |
    |  sketch:recent:<session> Sketch bit vectors (100)   24h TTL  |
    |  result:<requestID>      Cached inference results   10m TTL  |
    |  <queueName>             Job queue (FIFO list)      no TTL   |
    +--------------------------------------------------------------+
```

---

## Key Parameters Summary

### Bandit
- **dim** = 10 (feature vector dimensionality)
- **alpha** = 1.0 (exploration coefficient)
- **decay** = 0.99 (reserved, unused)
- **TTL** = 24h per arm state in Redis

### Sketch
- **dimensions** = 128 bits (16 bytes per sketch)
- **bits per word** = 3 (from FNV-1a hash)
- **threshold** = 0.85 Hamming similarity
- **max recent sketches** = 100 per session
- **TTL** = 24h per session sketch list

### Queue (Dragonfly)
- **pool size** = 50 connections
- **push timeout** = 3s
- **result TTL** = 10 minutes
- **queue type** = unbounded FIFO (Redis list)

### Caveman
- **min size** = 500 characters
- **tiers** = 4 (lite/full/ultra/wenyan)
- **estimated savings** = 30-75% response tokens depending on tier
- **validation threshold** = 80% identifier preservation

### Adaptive Limiter
- **min limit** = 1 (per model)
- **max limit** = initial * probeMultiplier (default 10x)
- **global timeout** = 60s (initial), 30s (fallback)
- **cooldown** = 5s after 429
- **learned ceiling decay** = 5 minutes
- **adjustment frequency** = every 5th success
- **RTT EWMA alpha** = 0.3
- **gradient clamp** = [0.8, 2.0]
