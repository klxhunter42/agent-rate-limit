# Optimizer Pipeline - Timeframe Flow & Activation Logic

> แสดงการทำงานของทุก Optimizer Stage ตาม Budget Level และ Request Lifecycle
> Source: `api-gateway/handler/optimizers.go`, `docs/19-optimizer-pipeline-reference.md`

---

## Budget Level Thresholds

```
Session Token Usage (cumulative input + output)
│
├── 0-50%   ──► GREEN   (budgetLevel=0)   → lightweight optimization
├── 50-75%  ──► YELLOW  (budgetLevel=1)   → moderate optimization
└── 75-100% ──► RED     (budgetLevel=2)   → aggressive optimization
```

---

## Request Lifecycle - 2 Phase Flow

### Phase 1: Pre-Proxy (Before sending to upstream LLM)

ทุก request ผ่าน 2 function หลักก่อนส่งขึ้น upstream:

```
Client Request
│
├─ 1. PasteGuard (privacy/pipeline.go)
│  └─ MaskRequest() → detect secrets + PII → replace with placeholders
│     (ทุก budget level, independent ของ optimizer)
│
├─ 2. OptimizeSystemPrompt() (optimizers.go:63)
│  └─ ทำงานเฉพาะ system prompt
│
├─ 3. OptimizeMessages() (optimizers.go:211)
│  └─ ทำงานเฉพาะ content ใน messages[]
│
├─ 4. ToolFilter (toolfilter/)
│  └─ กรอง tool manifest ให้เหลือแค่ที่เกี่ยวข้อง
│
├─ 5. Budget-Aware Disclosure (disclosure/)
│  └─ Yellow/Red: truncate tool_result blocks
│
└─ Proxy to Upstream LLM (masked + optimized)
```

### Phase 2: Post-Proxy (After receiving response)

```
Upstream Response
│
├─ 1. PasteGuard Unmask
│  └─ StreamUnmasker / UnmaskResponse → restore placeholders
│
├─ 2. PostProxyFeedback() (optimizers.go:297)
│  ├─ Prefetcher.Record()     → train Markov chain
│  ├─ Waste.RecordRequest()   → detect waste patterns
│  ├─ Cache.RecordHit()       → update ROI stats
│  └─ Bandit.Update()         → update reward signal
│
└─ Client Response (unmasked + metrics recorded)
```

---

## Phase 1 Detail: System Prompt Pipeline Stages

ตามลำดับใน `OptimizeSystemPrompt()` (optimizers.go:63-207):

### Stage Sequence Table

| Step | Stage | Saves | Budget Level | Algorithm | Trigger Condition |
|------|-------|-------|-------------|-----------|-------------------|
| 1 | **semantic_dedup** (F7) | INPUT | Always (Green+) | Jaccard sentence dedup (threshold 0.7) | `saved > 0` after dedup |
| 2 | **chunker** (F1) | INPUT | Always (Green+) | Rabin-Karp rolling hash, reorder stable chunks | `saved > 0` after reorder |
| 3 | **delta** (F8) | INPUT | Always (Green+) | LCS diff vs Redis-cached baseline | `ok && saved > 0`, content < 50KB |
| 4 | **sketch_dedup** (F9) | INPUT | Always (Green+) | 128-dim 1-bit FNV-1a sketch, Hamming similarity | `isDup && saved > 0` |
| 5 | **summarizer** (F6) | INPUT | **Red only** (>=75%) | Extractive truncation (first-sentence or TextRank) | `budgetLevel >= 2` |
| 6 | **intent_filter** (F13) | INPUT | Always | Intent classification → filter irrelevant sections | `saved > 0` after filter |
| 7 | **textcomp** (F17) | INPUT | Always | Regex: remove filler, verbose synonyms, articles | `saved > 0` after regex |
| 8a | **caveman_input** (F16) | INPUT | Always (non-transparent) | Regex: compress pleasantries, hedge words | `shouldCompress && inputSaved > 0` |
| 8b | **caveman_output** (F16) | OUTPUT | Always (non-transparent) | Append `[OUTPUT STYLE]` directive (229 chars) | `compressed != text` |

### Budget-Gated Activation Flow

```
┌─────────────────────────────────────────────────────────────┐
│                    OPTIMIZE SYSTEM PROMPT                    │
│                                                              │
│  ┌─────────┐   ┌─────────┐   ┌─────────┐   ┌─────────┐    │
│  │ F7      │──►│ F1      │──►│ F8      │──►│ F9      │    │
│  │ dedup   │   │ chunker │   │ delta   │   │ sketch  │    │
│  └─────────┘   └─────────┘   └─────────┘   └─────────┘    │
│       │              │             │             │          │
│    ALWAYS         ALWAYS       ALWAYS        ALWAYS        │
│                                                              │
│                    ┌─────────────────────┐                   │
│                    │  budgetLevel >= 2?  │                   │
│                    └──────┬──────────────┘                   │
│                     YES   │   NO                             │
│                    ┌──────┴──────┐                           │
│                    ▼             │                           │
│              ┌─────────┐        │                           │
│              │ F6      │        │                           │
│              │summryze │        │                           │
│              └────┬────┘        │                           │
│                   │             │                           │
│                   └──────┬──────┘                           │
│                          ▼                                  │
│              ┌──────────────┐                               │
│              │ F13 intent   │──► ALWAYS                     │
│              │   filter     │                               │
│              └──────┬───────┘                               │
│                     ▼                                       │
│              ┌──────────────┐                               │
│              │ F17 textcomp │──► ALWAYS                     │
│              └──────┬───────┘                               │
│                     ▼                                       │
│              ┌──────────────────────────────┐               │
│              │  transparent mode?           │               │
│              └──────┬───────────┬───────────┘               │
│                YES  │           │  NO                       │
│               SKIP  │           ▼                           │
│                      │  ┌──────────────┐                    │
│                      │  │ F16 caveman  │──► ALWAYS          │
│                      │  │  input+output│                    │
│                      │  └──────────────┘                    │
│                      │       │                              │
│                      │       ├── tier=lite  (Green: ~30%)  │
│                      │       ├── tier=full  (Yellow: ~50%) │
│                      │       └── tier=ultra (Red: ~75%)    │
└─────────────────────────────────────────────────────────────┘
```

---

## Phase 1 Detail: Messages Pipeline Stages

ตาม `OptimizeMessages()` (optimizers.go:211-294):

### Per-Message Block Flow

```
messages[]  ──► iterate each message
│
├── content is string?
│   ├── whitespace dedup + sentence dedup (message_text)
│   └── textcomp regex compression (message_textcomp)
│
└── content is []blocks?
    └── iterate each block
        ├── block.type == "tool_use" → SKIP
        ├── block.type == "text" → whitespace + dedup (message_block_text)
        └── block.type == "tool_result" →
            ├── whitespace + dedup (message_block_tool_result)
            └── toolcomp format-aware compression (toolcomp)
```

### Stage Table

| Stage | Target | Budget | What it does |
|-------|--------|--------|-------------|
| **message_text** | `content` (string) | Always | Whitespace collapse + sentence dedup |
| **message_textcomp** | `content` (string) | Always | Regex filler/verbose removal |
| **message_block_text** | `text` blocks | Always | Whitespace + sentence dedup |
| **message_block_tool_result** | `tool_result` blocks | Always | Whitespace + sentence dedup |
| **toolcomp** | `tool_result` content | Always | Format-aware: truncate kubectl logs, compress JSON, strip ANSI |

---

## Phase 1 Detail: Additional Pre-Proxy Stages

นอกเหนือจาก system prompt + messages:

| Stage | When | Budget Gate | What |
|-------|------|-------------|------|
| **ToolFilter** (F19) | Before proxy | Always | Intent-based manifest filtering (27→4 tools) |
| **PasteGuard** | Before all optimizers | Always | Regex secrets + Presidio PII masking |
| **Budget-Aware Disclosure** (F15) | On tool_result blocks | **Yellow/Red only** | Green: passthrough, Yellow: truncate to L2*8 chars, Red: truncate to L1*4 chars |

### Budget-Aware Disclosure Logic

```
content length check:
│
├── GREEN (budgetLevel=0)
│   └── passthrough (no truncation)
│
├── YELLOW (budgetLevel=1)
│   ├── content > 2000 chars → truncate to L2Tokens * 8
│   └── content <= 2000 chars → passthrough
│
└── RED (budgetLevel=2)
    ├── content > 1000 chars → truncate to L1Tokens * 4
    ├── content 500-1000 chars → truncate to L2Tokens * 6
    └── content < 500 chars → passthrough
```

---

## Caveman Tier Mapping (F16)

Tier ขึ้นอยู่กับ budgetLevel ณ เวลานั้น:

```
func BudgetToTier(level int) CompressionTier {
    switch level {
    case 2:  → TierUltra   // red budget (>75%)  → ~75% output reduction
    case 1:  → TierFull    // yellow (50-75%)     → ~50% output reduction
    default: → TierLite    // green (<50%)        → ~30% output reduction
    }
}
```

### Condition to SKIP Caveman

```
if transparent mode → SKIP (preserve full output for debugging)
if len(systemPrompt) < CAVEMAN_MIN_SIZE (default 500 chars) → SKIP
```

---

## Timeframe Simulation: 25-Turn Session

ตัวอย่างการเปลี่ยนแปลง optimizer ตามเวลาผ่านไปใน session จริง:

### Timeline Overview

```
Turn    Context%    Budget    New Stages Activated               Cumulative Savings
────    ─────────   ──────    ───────────────────────            ──────────────────
T1      5%          GREEN     semantic_dedup, textcomp, caveman-lite    ~15% input
T2      10%         GREEN     + chunker, delta (baseline set)          ~20% input
T3      15%         GREEN     + sketch (no dup yet), toolfilter        ~22% input
T4      20%         GREEN     + toolcomp (first tool_result)           ~25% input
T5      25%         GREEN     sketch flags T4≈T5 (0.92 similarity)     ~27% input
...
T10     48%         GREEN     caveman still lite, all green stages     ~30% input
T11     52%         YELLOW    ⚡ budget shift!                          ──────
                              + disclosure truncates tool_results      ~38% input
                              + caveman upgrades to tier=full          +50% output
T12     55%         YELLOW    delta hits (baseline cached)             ~40% input
...
T18     73%         YELLOW    waste_detection flags loop (3x reads)    ~42% input
T19     76%         RED       ⚡ budget shift!                          ──────
                              + summarizer activates (extractive)      ~55% input
                              + caveman upgrades to tier=ultra         +75% output
T20     80%         RED       cache_eviction cleans low-ROI entries    ~58% input
T21     83%         RED       bandit reward: toolcomp+sketch best arm  ~60% input
T22     87%         RED       intent_filter aggressive mode            ~62% input
T23     90%         RED       summarizer (TextRank) on long history    ~65% input
T24     93%         RED       prefetcher predicts next tool correctly  latency saved
T25     95%         RED       session approaching limit                ~67% input
```

### Detailed Per-Turn Activation Map

```
Stage              T1  T2  T3  T4  T5  T6  T7  T8  T9  T10 T11 T12 T13 T14 T15 T16 T17 T18 T19 T20 T21 T22 T23 T24 T25
────────────────── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ───
Budget Level       G   G   G   G   G   G   G   G   G   G   Y   Y   Y   Y   Y   Y   Y   Y   R   R   R   R   R   R   R
                   (0) (0) (0) (0) (0) (0) (0) (0) (0) (0) (1) (1) (1) (1) (1) (1) (1) (1) (2) (2) (2) (2) (2) (2) (2)
────────────────── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ───
PasteGuard         ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
semantic_dedup     ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
chunker            ·   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
delta              ·   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
sketch_dedup       ·   ·   ●   ·   ●   ·   ●   ·   ●   ·   ●   ·   ●   ·   ●   ·   ●   ·   ●   ·   ●   ·   ●   ·   ●
summarizer         ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ●   ●   ●   ●   ●   ●   ●
intent_filter      ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
textcomp           ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
caveman_input      ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
caveman_output     L   L   L   L   L   L   L   L   L   L   F   F   F   F   F   F   F   F   U   U   U   U   U   U   U
toolfilter         ·   ·   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
toolcomp           ·   ·   ·   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
disclosure         ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
────────────────── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ───
Post-Proxy:
────────────────── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ───
prefetcher         ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
waste_detect       ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
bandit             ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
cache_roi          ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●   ●
────────────────── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ─── ───
● = activated   L=lite  F=full  U=ultra   · = not activated / no savings
```

---

## Complete Data Flow Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         CLIENT REQUEST                                   │
│  POST /v1/messages                                                      │
│  { model, system, messages[], tools[], stream }                         │
└───────────────────────────┬─────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  PasteGuard: MaskRequest()                                              │
│  ├── Extract text spans from: system, messages, tool_result content    │
│  ├── Secret Detection (regex): SSH key, PEM, API keys, JWT, Bearer    │
│  ├── Secret Masking: AKIAIOSFODNN7EXAMPLE → [[API_KEY_AWS_1]]        │
│  ├── PII Detection (Presidio): EMAIL, PHONE, PERSON                   │
│  └── PII Masking: user@example.com → [[EMAIL_ADDRESS_1]]              │
│  Result: maskedBody + MaskContext (placeholder→original map)           │
└───────────────────────────┬─────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  Token Estimation + Budget Level Calculation                            │
│  ├── Content-type aware estimation (code/JSON/markdown/text)          │
│  ├── Sum input + output tokens from session history                    │
│  └── budgetLevel = 0 (green<50%), 1 (yellow 50-75%), 2 (red>75%)      │
└───────────────────────────┬─────────────────────────────────────────────┘
                            │
              ┌─────────────┼─────────────┐
              │             │             │
              ▼             ▼             ▼
┌──────────────────┐ ┌──────────────┐ ┌─────────────────┐
│ OptimizeSystem   │ │ Optimize     │ │ ToolFilter      │
│ Prompt()         │ │ Messages()   │ │ (F19)           │
│                  │ │              │ │                 │
│ F7  dedup        │ │ whitespace   │ │ Classify intent │
│ F1  chunker      │ │ dedup        │ │ → filter 27→N   │
│ F8  delta        │ │ textcomp     │ │ tools           │
│ F9  sketch       │ │ toolcomp     │ │                 │
│ F6  summarizer   │ │              │ │                 │
│ F13 intent_flt   │ │              │ │                 │
│ F17 textcomp     │ │              │ │                 │
│ F16 caveman      │ │              │ │                 │
└────────┬─────────┘ └──────┬───────┘ └────────┬────────┘
         │                  │                  │
         └──────────────────┼──────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  Budget-Aware Disclosure (F15)                                          │
│  ├── Green: passthrough                                                 │
│  ├── Yellow: truncate tool_result > 2000 chars to L2*8                 │
│  └── Red: truncate tool_result > 1000 chars to L1*4                    │
└───────────────────────────┬─────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    PROXY TO UPSTREAM LLM                                │
│  Provider selection via Bandit / Multi-Provider routing                 │
│  Rate limiting check (adaptive token-aware)                             │
└───────────────────────────┬─────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  PasteGuard: Unmask Response                                            │
│  ├── Non-stream: UnmaskResponse() → replace all placeholders at once   │
│  └── Stream: StreamUnmasker → ProcessChunk() per SSE event             │
│      ├── text_delta → buffered text unmask                              │
│      ├── thinking_delta → buffered text unmask                          │
│      └── input_json_delta → JSON-safe unmask                            │
└───────────────────────────┬─────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  PostProxyFeedback() - Record telemetry                                 │
│  ├── F4  prefetcher.Record()  → Markov chain training                  │
│  ├── F11 waste.RecordRequest() → 14 waste pattern detectors            │
│  ├── F14 cache.RecordHit()    → ROI tracking for eviction              │
│  └── F5  bandit.Update()      → LinUCB reward signal                   │
│      reward = output_tokens / input_tokens (capped at 1.0)             │
│      reward = 0.0 if output == 0                                        │
└───────────────────────────┬─────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    CLIENT RESPONSE                                       │
│  Unmasked + all metrics recorded                                        │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Background Processes (ไม่ได้ทำงานต่อ request)

| Process | Interval | What |
|---------|----------|------|
| **Waste Detection scan** | 60s | 7 detectors scan recent requests |
| **Cache Eviction** | 5min | ROI-based cleanup of bottom 10% cache entries |
| **Warm Start** | On new session | Cosine similarity match against past sessions |
| **Token Refresh** | 30min ticker | OAuth token refresh (immediate on startup) |
| **CompCache** | Every Redis op | Transparent Zstd compression for values > 512 bytes |

---

## Savings Summary by Budget Level

| Budget | Active Stages | Input Savings | Output Savings | Typical Scenario |
|--------|--------------|---------------|----------------|------------------|
| **Green** (<50%) | dedup, chunker, delta, sketch, textcomp, caveman-lite, toolcomp, toolfilter | 20-30% | ~30% | New session, simple Q&A |
| **Yellow** (50-75%) | All Green + disclosure, caveman-full | 35-50% | ~50% | Multi-turn code review, debugging |
| **Red** (>75%) | All Yellow + summarizer, caveman-ultra, aggressive disclosure | 55-70% | ~75% | Long debug marathon, complex feature |

---

## Prometheus Queries for Timeframe Analysis

```promql
# Budget level distribution over time
sum by (le) (rate(api_gateway_budget_level[5m]))

# Stages activating per budget tier
sum by (stage) (rate(api_gateway_optimizer_savings_total{stage="summarizer"}[5m]))
# → non-zero only during red budget

# Caveman tier distribution
sum by (tier) (rate(api_gateway_caveman_compressions_total{result="valid"}[5m]))

# Disclosure truncation rate by budget
sum by (budget_level) (rate(api_gateway_disclosure_truncations_total[5m]))

# Session token usage percentile over time
histogram_quantile(0.5, rate(api_gateway_session_tokens_bucket[5m]))
histogram_quantile(0.95, rate(api_gateway_session_tokens_bucket[5m]))
```
