# Token Optimization Pipeline - Complete Reference

Date: 2026-05-06
Scope: All 14 optimizer stages in the API gateway pipeline

---

## Pipeline Overview

```
Request → Handler.HandleMessages()
  │
  ├─ Calculate budget level (green/yellow/red from session token usage)
  │
  ├─ OptimizeSystemPrompt(systemPrompt, budgetLevel, model)
  │   ├─ F7  semantic_dedup    (always)
  │   ├─ F1  chunker           (always if enabled)
  │   ├─ F8  delta             (always if enabled)
  │   ├─ F9  sketch            (always if enabled)
  │   ├─ F6  summarizer        (red budget only)
  │   ├─ F13 intent_filter     (always if enabled)
  │   ├─ F17 textcomp          (always if enabled)
  │   └─ F16 caveman           (skip if transparent mode)
  │
  ├─ OptimizeMessages(messages)
  │   ├─ whitespace + dedup    (string content blocks)
  │   ├─ textcomp              (string content blocks)
  │   └─ whitespace + dedup    (text/tool_result blocks, skip tool_use)
  │
  ├─ json.Marshal(request)
  │
  ├─ privacy.MaskRequest(body)     ← runs AFTER optimization
  │
  └─ Proxy → Provider API
      │
      └─ PostProxyFeedback(sessionID, model, input, output)
          ├─ F4  prefetcher      (record tool transitions)
          ├─ F11 waste           (detect waste patterns)
          ├─ F14 cache eviction  (ROI tracking)
          └─ F5  bandit          (reward signal)
```

### Budget Levels

| Level | Trigger | Stages Activated |
|-------|---------|-----------------|
| Green | < 50% of context window | semantic_dedup, chunker, delta, sketch, textcomp |
| Yellow | 50-75% | All green stages + packer (if enabled) |
| Red | > 75% | All stages including summarizer, intent_filter, caveman ultra |

---

## Stage Reference

### F7: Semantic Dedup (`tokenizer/`)

- **Saves**: INPUT tokens
- **Activates**: Always (all budget levels)
- **Config**: None (always on)
- **Algorithm**: Jaccard-based sentence dedup (threshold 0.7). Splits text into sentences, normalizes (lowercase, strip punctuation), removes duplicates. Skips text containing privacy placeholders (`__SECRET_*__`, `__PII_*__`).
- **Metrics**: `api_gateway_optimizer_chars_saved_total{technique="semantic_dedup"}`
- **Estimated savings**: 3-5% on system prompts with repeated instructions

### F1: Chunker (`chunker/`)

- **Saves**: INPUT tokens (indirect - improves cacheability)
- **Activates**: Always if enabled
- **Config**:
  - `CHUNKER_ENABLED` (default: true)
  - `CHUNKER_MIN_CHUNK` (default: 128)
  - `CHUNKER_MAX_CHUNK` (default: 4096)
  - `CHUNKER_WINDOW_SIZE` (default: 48)
  - `CHUNKER_STABLE_THRESHOLD` (default: 2)
- **Algorithm**: Rabin-Karp rolling hash splits content into variable-size chunks. Chunks seen >= threshold times are "stable". Output reorders stable chunks first for better cache alignment. Stability tracked in Redis with 24h TTL.
- **Metrics**: `api_gateway_chunker_chunks_total{type}`, `chunker_chars_saved_total`
- **Estimated savings**: 5-15% on repetitive conversations

### F8: Delta Encoding (`delta/`)

- **Saves**: INPUT tokens
- **Activates**: Always if enabled
- **Config**:
  - `DELTA_ENABLED` (default: true)
  - `DELTA_MIN_SAVINGS_PCT` (default: 10.0)
- **Algorithm**: Line-level LCS diff against Redis-cached baseline (keyed by `sys:{model}`). Encodes as `+`/`-`/`=` operations. Falls back to passthrough if savings < min threshold or content > 50KB/200 ops.
- **Metrics**: `api_gateway_delta_encodes_total{result}`, `delta_chars_saved_total`
- **Estimated savings**: 20-60% on iterative edit workflows

### F9: Sketch Near-Duplicate (`sketch/`)

- **Saves**: INPUT tokens (diagnostic - flags duplicates)
- **Activates**: Always if enabled
- **Config**:
  - `SKETCH_ENABLED` (default: true)
  - `SKETCH_DIMENSIONS` (default: 128)
  - `SKETCH_THRESHOLD` (default: 0.85)
- **Algorithm**: 128-dim 1-bit sketch via FNV-1a word hashing (3 bit positions per word). Hamming similarity >= threshold against last 100 sketches in session flags near-duplicates.
- **Metrics**: `api_gateway_sketch_checks_total{result}`, `sketch_chars_saved_total`
- **Estimated savings**: 5-30% in sessions with repeated prompts/retries

### F6: Summarizer (`summarizer/`)

- **Saves**: INPUT tokens
- **Activates**: Red budget only (>= 75% context usage)
- **Config**:
  - `SUMMARIZER_ENABLED` (default: true)
  - `SUMMARIZER_MODEL` (default: glm-4.7-flashx, not currently used)
  - `SUMMARIZER_MAX_RATIO` (default: 0.3)
  - `SUMMARIZER_METHOD` (default: firstsentence, candidates: textrank)
- **Algorithm**: Extractive truncation. Keeps first sentence of each paragraph within `MaxRatio` budget. Results cached in Redis with 1h TTL keyed by SHA-256 content hash.
- **Metrics**: `api_gateway_summarizer_calls_total{method}`, `summarizer_chars_saved_total{method}`
- **Estimated savings**: 50-70% on red budget (emergency truncation)

### F13: Intent Filter (`filter/`)

- **Saves**: OUTPUT tokens
- **Activates**: Always if enabled (applied to system prompt in current pipeline)
- **Config**:
  - `FILTER_ENABLED` (default: true)
- **Algorithm**: Regex intent classification (code/analysis/search/action/chat). Code intent extracts only code blocks. Search intent extracts key lines (bullets, file paths). Others pass through.
- **Metrics**: `api_gateway_filter_intents_total{intent}`, `filter_chars_saved_total{intent}`
- **Estimated savings**: 10-40% for code/search-heavy sessions

### F17: TextComp (`textcomp/`)

- **Saves**: INPUT tokens (system prompt + message text)
- **Activates**: Always if enabled
- **Config**:
  - `TEXTCOMP_ENABLED` (default: true)
  - `TEXTCOMP_MODE` (default: balanced, options: balanced/aggressive)
- **Algorithm**: 4-phase pipeline: mask protected regions (code fences, URLs, quoted strings) → apply compression rules (10 filler phrases, 12 hedge words, 30 verbose-to-compact replacements, 11 aggressive-only rules) → unmask → cleanup. Applied to system prompt AND message text content.
- **Metrics**: `api_gateway_optimizer_chars_saved_total{technique="textcomp"}`
- **Estimated savings**: 5-15% on prose-heavy prompts

### F16: Caveman (`caveman/`)

- **Saves**: OUTPUT tokens (via system prompt style injection)
- **Activates**: All levels (tier varies). Skipped if transparent mode.
- **Config**:
  - `CAVEMAN_ENABLED` (default: true)
  - `CAVEMAN_AUTO_DETECT` (default: true)
  - `CAVEMAN_MIN_SIZE` (default: 500)
- **Algorithm**: Injects `[OUTPUT STYLE]` directive into system prompt. 4 tiers: lite (30% reduction, green budget), full (50%, yellow), ultra (75%, red), wenyan (70%). Validates code blocks and identifiers preserved after compression.
- **Metrics**: `api_gateway_caveman_compressions_total{tier,result}`, `caveman_compression_ratio`
- **Estimated savings**: 30-75% output tokens depending on tier

### Message Processing (in `OptimizeMessages`)

- **Saves**: INPUT tokens
- **Activates**: Always
- **Algorithm**: Walks all messages in request. For string content: `OptimizeWhitespace` + `DeduplicateSentences` + `TextComp.Compress`. For block content: same treatment on `text` and `tool_result` blocks. Skips `tool_use` blocks entirely.
- **Metrics**: `api_gateway_optimizer_chars_saved_total{technique="message_text"}`, `{technique="message_textcomp"}`
- **Estimated savings**: 3-8% on message content

### F4: Prefetcher (`prefetcher/`)

- **Saves**: Indirect (latency reduction)
- **Activates**: Post-proxy feedback
- **Config**:
  - `PREFETCHER_ENABLED` (default: true)
  - `PREFETCHER_MAX_ORDER` (default: 5)
  - `PREFETCHER_TOP_K` (default: 3)
- **Algorithm**: 1st-order Markov chain of tool transitions per session in Redis (4h TTL). Predicts top-K next tool calls by transition frequency.
- **Metrics**: `api_gateway_prefetcher_predictions_total{correct}`
- **Estimated savings**: 50-200ms latency per correct prediction

### F5: Bandit (`bandit/`)

- **Saves**: Indirect (meta-optimizer)
- **Activates**: Post-proxy feedback
- **Config**:
  - `BANDIT_ENABLED` (default: true)
  - `BANDIT_ALPHA` (default: 1.0)
  - `BANDIT_DECAY` (default: 0.99)
- **Algorithm**: LinUCB with 10-dim context features. Per-arm A matrix + b vector in Redis (24h TTL). Gauss-Jordan inversion for theta estimation. Explores when variance > 1.0.
- **Metrics**: `api_gateway_bandit_selections_total{arm,exploratory}`, `bandit_reward_total{arm}`
- **Estimated savings**: 5-15% indirect improvement on other optimizers

### F11: Waste Detection (`waste/`)

- **Saves**: Indirect (diagnostic)
- **Activates**: Post-proxy feedback
- **Config**:
  - `WASTE_ENABLED` (default: true)
  - `WASTE_MIN_REQUESTS` (default: 10)
- **Algorithm**: 7 detectors run every 60s: empty_response, retry_churn, loop_detection, oversized_context, budget_exceeded, redundant_tool_call, low_value_response.
- **Metrics**: `api_gateway_waste_findings_total{detector,severity}`, `waste_tokens_wasted_total{detector}`
- **Estimated savings**: Diagnostic - identifies 5-20% of tokens as waste

### F14: Cache Eviction (`cache/`)

- **Saves**: Indirect (cache quality)
- **Activates**: Post-proxy feedback
- **Config**:
  - `CACHE_EVICTION_ENABLED` (default: true)
  - `CACHE_EVICTION_PCT` (default: 10.0)
- **Algorithm**: Periodic (5min) eviction: scans Redis `cache:stats:*` keys, computes ROI = tokens_saved/tokens_injected, evicts bottom 10%.
- **Metrics**: `api_gateway_cache_eviction_keys_evicted_total`, `cache_eviction_roi_score`
- **Estimated savings**: Indirect - maintains ~10% higher effective cache hit rate

### F10: Warm Start (`warmstart/`)

- **Saves**: Indirect (cold-start mitigation)
- **Activates**: Session initialization
- **Config**:
  - `WARMSTART_ENABLED` (default: true)
  - `WARMSTART_TOP_K` (default: 3)
  - `WARMSTART_MIN_SIMILARITY` (default: 0.5)
- **Algorithm**: 32-dim feature vector per session (model, content ratios, tool frequencies, budget levels). Cosine similarity scan against past sessions in Redis (7d TTL).
- **Metrics**: `api_gateway_warmstart_sessions_warmed_total{result}`
- **Estimated savings**: 10-20% reduction in cold-start waste

---

## Data Flow Matrix

| Stage | System Prompt | Message Text | tool_result | tool_use | Output Response |
|-------|:---:|:---:|:---:|:---:|:---:|
| semantic_dedup | Y | - | - | - | - |
| chunker | Y | - | - | - | - |
| delta | Y | - | - | - | - |
| sketch | Y | - | - | - | - |
| summarizer | Y (red) | - | - | - | - |
| intent_filter | Y | - | - | - | - |
| textcomp | Y | Y | - | - | - |
| caveman | Y | - | - | - | Y* |
| whitespace+dedup | - | Y | Y | - | - |
| message_textcomp | - | Y | - | - | - |

*Y* = caveman injects style into system prompt to affect output, does not modify response directly

---

## Input vs Output Token Savings

| Category | Stages | Mechanism |
|----------|--------|-----------|
| **Input savings** | semantic_dedup, chunker, delta, sketch, summarizer, textcomp, message ws+dedup, message_textcomp | Compress or truncate content before sending to provider |
| **Output savings** | caveman, intent_filter | Style injection or response filtering after receiving from provider |
| **Indirect/meta** | prefetcher, bandit, waste, cache eviction, warmstart, packer | Improve other stages' effectiveness or reduce latency |

---

## Per-Technique Savings Summary

| Stage | Input Saved | Output Saved | Typical % Reduction | Activation |
|-------|:-----------:|:------------:|:-------------------:|:----------:|
| semantic_dedup | 3-5% | - | 3-5% chars | always |
| chunker | 5-15% | - | 5-15% cache hit improvement | always |
| delta | 20-60% | - | 20-60% on edits | always |
| sketch | 5-30% | - | 5-30% on retries | always |
| summarizer | 50-70% | - | 50-70% emergency | red only |
| intent_filter | - | 10-40% | 10-40% output | always |
| textcomp | 5-15% | - | 5-15% chars | always |
| caveman | - | 30-75% | 30-75% output | all tiers |
| message ws+dedup | 3-8% | - | 3-8% chars | always |

**Note**: Savings are cumulative but not additive - stages operate on the output of previous stages.

---

## Configuration Quick Reference

| Env Var | Default | Stage |
|---------|---------|-------|
| `CHUNKER_ENABLED` | true | F1 |
| `CHUNKER_MIN_CHUNK` | 128 | F1 |
| `CHUNKER_MAX_CHUNK` | 4096 | F1 |
| `CHUNKER_WINDOW_SIZE` | 48 | F1 |
| `CHUNKER_STABLE_THRESHOLD` | 2 | F1 |
| `DELTA_ENABLED` | true | F8 |
| `DELTA_MIN_SAVINGS_PCT` | 10.0 | F8 |
| `SKETCH_ENABLED` | true | F9 |
| `SKETCH_DIMENSIONS` | 128 | F9 |
| `SKETCH_THRESHOLD` | 0.85 | F9 |
| `SUMMARIZER_ENABLED` | true | F6 |
| `SUMMARIZER_MAX_RATIO` | 0.3 | F6 |
| `SUMMARIZER_METHOD` | firstsentence | F6 |
| `FILTER_ENABLED` | true | F13 |
| `TEXTCOMP_ENABLED` | true | F17 |
| `TEXTCOMP_MODE` | balanced | F17 |
| `CAVEMAN_ENABLED` | true | F16 |
| `CAVEMAN_AUTO_DETECT` | true | F16 |
| `CAVEMAN_MIN_SIZE` | 500 | F16 |
| `PACKER_ENABLED` | true | F12 |
| `PACKER_MIN_UTILITY` | 0.1 | F12 |
| `PREFETCHER_ENABLED` | true | F4 |
| `PREFETCHER_MAX_ORDER` | 5 | F4 |
| `PREFETCHER_TOP_K` | 3 | F4 |
| `BANDIT_ENABLED` | true | F5 |
| `BANDIT_ALPHA` | 1.0 | F5 |
| `BANDIT_DECAY` | 0.99 | F5 |
| `WASTE_ENABLED` | true | F11 |
| `WASTE_MIN_REQUESTS` | 10 | F11 |
| `CACHE_EVICTION_ENABLED` | true | F14 |
| `CACHE_EVICTION_PCT` | 10.0 | F14 |
| `WARMSTART_ENABLED` | true | F10 |
| `WARMSTART_TOP_K` | 3 | F10 |
| `WARMSTART_MIN_SIMILARITY` | 0.5 | F10 |
| `DISCLOSURE_ENABLED` | true | F15 |
| `DISCLOSURE_L1_TOKENS` | 15 | F15 |
| `DISCLOSURE_L2_TOKENS` | 60 | F15 |

---

## Gap Analysis: Not Yet Implemented

Techniques from 7 improvement repos (`improvements/`) not yet in the gateway:

| Technique | Source | Impact | Feasibility | Status |
|-----------|--------|--------|-------------|--------|
| TextRank summarization | token-reducer | High | High | Planned |
| Tool result format compression | context-mode, trs | High | High | Planned |
| Budget-aware progressive disclosure | token-savior | Medium | High | Planned |
| Tool manifest filtering | token-savior (ts_search) | Very High | Medium | Planned |
| Brotli Redis cache compression | token-optimizer-mcp | Medium | High | Planned |
| AST code graph navigation | code-review-graph, token-savior | Very High | Low (requires MCP) | Future |
| Persistent memory engine | token-savior | High | Low (requires MCP) | Future |
| BM25+vector hybrid search | token-reducer, token-savior | High | Low (requires embeddings) | Future |

---

## Production Measurement

All per-technique metrics are currently **mock/seed data** from `seedOptimizers()` in `metrics/metrics.go`. To get real numbers:

```promql
# Per-technique chars saved (real)
sum by (technique) (rate(api_gateway_optimizer_chars_saved_total[5m]))

# Total tokens saved
rate(api_gateway_optimizer_tokens_saved_total[5m])

# Per-technique duration
histogram_quantile(0.95, rate(api_gateway_optimizer_duration_seconds_bucket[5m]))

# Budget level distribution
sum by (model) (rate(api_gateway_budget_level[5m]))

# Waste findings by detector
sum by (detector, severity) (rate(api_gateway_waste_findings_total[5m]))
```

To disable mock data: remove the `go seedOptimizers()` call in `metrics/metrics.go` or gate behind `SEED_MOCK_DATA=false`.
