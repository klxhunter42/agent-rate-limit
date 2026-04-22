# Token Optimization Analysis

Date: 2026-04-21
Source: 7 improvement projects analyzed (`improvements/` directory)

---

## Summary of Analyzed Projects

| Project | Focus | Key Techniques |
|---------|-------|----------------|
| caveman | LLM-based markdown compression | Output style injection, validation pipeline, intensity levels |
| claude-context | Vector DB / RAG context | Hybrid search (Dense+BM25+RRF), AST chunking, ~40% token reduction validated |
| claude-token-optimizer | Methodology documentation | 10 patterns: progressive loading, dedup, token budget, semantic routing |
| context-mode | Context mode switching | Head+tail truncation, intent-driven filtering, session continuity |
| token-optimizer-mcp | MCP server with implementations | Brotli compression, whitespace opt, Levenshtein dedup, tiktoken, summarization |
| token-optimizer | Skills-based optimizer | Delta encoding, QJL near-duplicate detection, waste pattern detection, model routing |
| token-savior | Python TCA engine | LinUCB bandit, PPM prefetcher, knapsack packing, DCP chunking, community detection |

---

## Implemented in Gateway

### 1. Whitespace Optimization (from token-optimizer-mcp)
Collapses multiple spaces/newlines in prose while preserving code blocks.
- Saves ~3-5% tokens on messy input
- Zero-cost string processing

### 2. Content-Aware Token Estimation (from token-optimizer-mcp, claude-context)
Estimates tokens using content-type-specific ratios:
- Code: chars/2.5
- JSON: chars/2.8
- Markdown: chars/3.5
- Text: chars/4.0
- Quick fallback: chars/4

Used for pre-request cost estimation and budget decisions.

### 3. Head/Tail Content Truncation (from context-mode, token-optimizer-mcp)
When responses exceed limits, preserves 40% head + 60% tail instead of naive truncation.
Head keeps initial instructions/context; tail preserves recent messages and errors.

### 4. Static Model Capability Map (from claude-context, token-optimizer)
Per-model limits hardcoded to avoid API probing:
- Context windows, max output tokens, supported features
- Used for token budget calculations and routing decisions

### 5. Duplicate Content Deduplication (from token-optimizer-mcp, token-savior)
Sentence-level dedup using hash-based exact matching.
Near-duplicate detection via Levenshtein similarity (threshold 0.85).
Code blocks preserved during dedup.

### 6. Token Budget Tracking with Threshold Triggers (from token-optimizer-mcp, token-optimizer, token-savior)
Per-session token budget monitoring:
- Green (<50%): normal operation
- Yellow (50-75%): apply whitespace optimization + dedup
- Red (>75%): force aggressive truncation
- Auto-triggered at 80% context utilization

---

## Future Candidates (Not Yet Implemented)

| Priority | Technique | Impact | Complexity |
|----------|-----------|--------|------------|
| 1 | DCP Chunking + Reorder for cache prefix | Directly improves Anthropic prompt cache hit rate | High |
| 2 | Knapsack Context Packing | Maximize info density within token budget | High |
| 3 | Progressive Disclosure (3-layer) | Avoid fetching full cached bodies | Medium |
| 4 | ROI-Based Cache Eviction | Auto-clean low-value cache entries | Medium |
| 5 | PPM Prefetcher | Predict next request, pre-warm connections | Medium |
| 6 | LinUCB Bandit | Learn which injections actually save tokens | High |
| 7 | LLM-Based Summarization | Use cheap model to compress conversation history | Medium |
| 8 | Semantic Dedup (Jaccard) | Avoid caching near-duplicate completions | Medium |
| 9 | Delta Encoding | Serve only changes for cached content | High |
| 10 | QJL 1-Bit Sketch Near-Duplicate Detection | Detect near-duplicate requests across sessions | High |
| 11 | Multi-Provider Pricing Engine | Real-time cost per request, cost-based routing | Medium |
| 12 | Waste Pattern Detection (14 detectors) | Flag empty runs, loop detection, retry churn | Medium |
| 13 | Output Compression Tiers (lite/full/ultra) | 30-75% output token reduction via system prompt injection | Low |
| 14 | Intent-Driven Output Filtering | Index large responses, return only matching sections | Medium |
| 15 | Session Warm Start (Cosine Similarity) | Pre-load caches from similar past sessions | High |

---

## Technique Details

### Whitespace Optimization
Source: token-optimizer-mcp `WhitespaceOptimizationModule.ts`
- Collapse multiple spaces to single space
- Remove trailing whitespace per line
- Collapse multiple newlines to max 2
- Preserve code blocks between triple backticks
- Preserve indentation within code

### Content-Aware Token Estimation
Source: token-optimizer-mcp `heuristic-tokenizer.ts`, claude-context
- Detect content type via regex heuristics
- Code detected by: import, def, class, function, const patterns
- JSON detected by: parse validation
- Markdown detected by: heading/list patterns
- Apply calibrated chars-per-token ratios

### Head/Tail Truncation
Source: context-mode `smartTruncate()`, token-optimizer-mcp `TruncatingSummarizer`
- Preserve 40% of head (setup context, instructions)
- Preserve 60% of tail (recent messages, errors, results)
- Insert `[truncated]` marker between head and tail
- Include metadata line: `[X lines / Y KB truncated - showing first A + last B lines]`

### Duplicate Content Deduplication
Source: token-optimizer-mcp `DeduplicationModule.ts`, token-savior `dedup.py`
- Sentence boundary detection (handle abbreviations, decimals, ellipses)
- Hash-based exact match for O(1) lookup
- Levenshtein similarity for near-duplicate detection (threshold 0.85-0.9)
- Code blocks between backticks preserved
- Support "preserve first" and "preserve last" modes

### Token Budget Tracking
Source: token-optimizer-mcp `Handle-ContextGuard`, token-savior `budget.py`
- Track cumulative input+output tokens per session
- 80% threshold: proactive optimization every N operations
- 90% threshold: force emergency optimization
- Budget configurable per model (200K for Claude, 128K for GPT-4, 1M for Gemini)

---

## PasteGuard - Data Privacy Masking

PasteGuard is a privacy pipeline that intercepts requests before they reach upstream LLM providers, masking secrets and PII with reversible placeholders. The original values are restored in the response before returning to the client.

### Purpose
Prevent sensitive data (API keys, private keys, personal information) from being sent to upstream LLM providers, without requiring any changes to user prompts.

### Request Flow

```
Client Request
    |
    v
[Extract Text Spans] -- Parse JSON payload: system prompt, messages, tool_result content
    |
    v
[Secret Detection] -- Regex-based scanning for known secret patterns
    |   Entities: OPENSSH_PRIVATE_KEY, PEM_PRIVATE_KEY, API_KEY_SK,
    |            API_KEY_AWS, API_KEY_GITHUB, JWT_TOKEN, BEARER_TOKEN
    v
[Secret Masking] -- Replace detected secrets with placeholders (e.g. [[API_KEY_AWS_1]])
    |
    v
[PII Detection] -- Send secrets-masked text to Microsoft Presidio (NLP analyzer)
    |   Entities: PERSON, EMAIL_ADDRESS, PHONE_NUMBER
    |   Confidence threshold: 0.7 (configurable)
    v
[PII Masking] -- Replace PII with placeholders
    |
    v
[Proxy to Upstream] -- Send masked body (LLM never sees real secrets/PII)
    |
    v
[Unmask Response] -- Restore original values in response
    |   Non-stream: UnmaskResponse() replaces all placeholders at once
    |   Stream: StreamUnmasker replaces placeholders per SSE chunk
    v
Client Response
```

### Architecture

- `api-gateway/privacy/pipeline.go` - Main pipeline orchestrator: MaskRequest, UnmaskResponse, StreamUnmasker
- `api-gateway/privacy/config.go` - Environment-based configuration (all PASTEGUARD_* env vars)
- `api-gateway/privacy/metrics.go` - Prometheus metrics for monitoring
- `api-gateway/privacy/secrets/` - Regex-based secret detection and masking
- `api-gateway/privacy/pii/` - Presidio client for NLP-based PII detection
- `api-gateway/privacy/masking/` - Placeholder context management and stream unmasking
- `api-gateway/privacy/extractors/` - Text span extraction from LLM request payloads
- `grafana/provisioning/dashboards/pasteguard.json` - Grafana dashboard

### Configuration (Environment Variables)

| Variable | Default | Description |
|----------|---------|-------------|
| `PASTEGUARD_ENABLED` | `true` | Enable/disable the entire pipeline |
| `PASTEGUARD_SECRETS_ENABLED` | `true` | Enable regex-based secret detection |
| `PASTEGUARD_SECRET_ENTITIES` | All 7 types | Comma-separated list of secret entity types to detect |
| `PASTEGUARD_MAX_SCAN_CHARS` | `200000` | Max characters to scan per request |
| `PASTEGUARD_PII_ENABLED` | `true` | Enable Presidio-based PII detection |
| `PASTEGUARD_PRESIDIO_URL` | `http://arl-presidio:3000` | Presidio analyzer service URL |
| `PASTEGUARD_PII_SCORE_THRESHOLD` | `0.7` | Minimum confidence score for PII detection |
| `PASTEGUARD_PII_ENTITIES` | `PERSON,EMAIL_ADDRESS,PHONE_NUMBER` | Comma-separated PII entity types |
| `PASTEGUARD_PII_LANGUAGE` | `en` | Language for Presidio NLP analysis |

### Prometheus Metrics

| Metric | Labels | Description |
|--------|--------|-------------|
| `api_gateway_mask_duration_seconds` | `phase` (secrets_detect, pii_detect, mask, unmask) | Duration histogram per phase |
| `api_gateway_secrets_detected_total` | `type` | Secrets detected by entity type |
| `api_gateway_pii_detected_total` | `type` | PII entities detected by type |
| `api_gateway_mask_requests_total` | `has_secrets`, `has_pii` | Requests processed by the pipeline |

### Integration Point

PasteGuard runs in `handler.go` after token estimation and model lookup, before the request is proxied upstream:

```go
// Privacy masking: detect and mask secrets/PII before proxying.
var maskResult *privacy.MaskResult
if h.privacy != nil {
    maskResult, _ = h.privacy.MaskRequest(body)
    if maskResult != nil {
        body = maskResult.MaskedBody
    }
}
```

The `maskResult` is passed to proxy functions which use it to unmask responses before returning to the client. Supports both non-streaming (`UnmaskResponse`) and streaming (`NewStreamUnmasker`) responses.
