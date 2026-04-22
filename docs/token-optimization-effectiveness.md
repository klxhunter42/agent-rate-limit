# Token Optimization - Effectiveness Report

Date: 2026-04-22

---

## Measured Results (effectiveness_test.go)

All tests pass. Results from realistic system prompt (~5000 chars, simulating CLAUDE.md with intentional duplicate sections):

### Per-Technique Savings

| Technique | Before | After | Saved | % Reduction |
|-----------|--------|-------|-------|-------------|
| Whitespace Optimization | 1253 tokens | 1252 tokens | 1 token | 0.1% |
| Sentence Deduplication | 1253 tokens | 1209 tokens | 44 tokens | **3.5%** |
| Combined (WS + Dedup) | 1253 tokens | 1209 tokens | 44 tokens | **3.5%** |
| Head/Tail Truncation | 4423 tokens | 1265 tokens | 3158 tokens | **71.4%** (emergency) |

### Content-Aware Token Estimation Accuracy

| Content Type | Chars | Estimated Tokens | Ratio |
|-------------|-------|-----------------|-------|
| Code (Go) | 48 | ~20 | chars/2.5 |
| JSON | 78 | ~28 | chars/2.8 |
| Markdown | 61 | ~18 | chars/3.5 |
| Plain Text | 65 | ~17 | chars/4.0 |

### Model Capabilities Coverage

| Model | Provider | Context Window | Max Output |
|-------|----------|---------------|------------|
| claude-opus-4-7 | anthropic | 200K | 32K |
| claude-sonnet-4-6 | anthropic | 200K | 16K |
| claude-haiku-4-5 | anthropic | 200K | 8K |
| gpt-4o | openai | 128K | 16K |
| gpt-4o-mini | openai | 128K | 16K |
| gemini-2.5-pro | google | 1M | 65K |
| gemini-2.5-flash | google | 1M | 65K |
| glm-5.1 | zai | 128K | 4K |
| glm-5 | zai | 128K | 4K |
| glm-4.6v | zai | 8K | 4K |

### Budget Tracking Thresholds

| Usage | % of Context | Level | Action |
|-------|-------------|-------|--------|
| < 50% | < 100K/200K | GREEN | Normal |
| 50-75% | 100K-150K/200K | YELLOW | Optimize (ws + dedup) |
| > 75% | > 150K/200K | RED | Force truncate |

---

## Coverage: GLM Mode True vs False

Optimizer and privacy guard work on **ALL proxy paths** regardless of GLM mode:

| Proxy Path | GLM Mode True | GLM Mode False | Optimizer | Privacy Guard |
|-----------|--------------|----------------|-----------|---------------|
| ProxyNativeVision | Z.AI vision endpoint | N/A | Yes (AnthropicToOpenAI) | Yes |
| ProxyOpenAI | Text requests via Z.AI | OpenAI upstream | Yes (AnthropicToOpenAI) | Yes |
| ProxyTransparent | Anthropic passthrough | Anthropic passthrough | Yes (body JSON) | Yes |
| ProxyGemini | N/A | Gemini API key | Yes (anthropicToGemini) | Yes |
| ProxyCodeAssist | N/A | Gemini Code Assist | Yes (anthropicToGemini) | Yes |
| ProxySession | N/A | Claude.ai session | No (separate flow) | No (separate flow) |

**Privacy guard** is applied at the handler layer (handler.go:424-430) BEFORE any routing decision.
All proxy paths receive already-masked body + maskResult for unmasking.

**Token optimizer** is applied at 3 injection points:
1. `AnthropicToOpenAI()` - Anthropic/Z.AI/OpenAI text path
2. `anthropicToGemini()` - Gemini CodeAssist + Gemini API paths
3. `ProxyTransparent()` - Direct Anthropic passthrough (JSON body rewrite)

---

## Response Time Bottleneck Analysis

### HIGH Impact

| Bottleneck | Location | Impact | Fix |
|-----------|----------|--------|-----|
| No DNS caching | All proxy HTTP clients | 10-100ms per new connection | Custom Dialer with DNS cache |
| PII detection synchronous | privacy/pipeline.go:119 | Blocks request pipeline | Parallel span processing |
| 2 sequential rate-limit HTTP calls | middleware/ratelimit.go:102 | 2x network RTT before processing | Parallel with errgroup |
| Image fetch creates new HTTP client | proxy/anthropic.go:434 | No connection reuse | Shared image client |

### MEDIUM Impact

| Bottleneck | Location | Impact | Fix |
|-----------|----------|--------|-----|
| Non-stream responses fully buffered | All proxy non-stream handlers | Delayed TTFB for client | Streaming fallback |
| bufio.Scanner line-buffering | All SSE relay functions | Latency between upstream flush | Smaller buffer or raw byte relay |
| KeyPool time.Sleep on cooldown | proxy/key_pool.go:149 | Goroutine blocked up to 10s | Channel-based signaling |
| AdaptiveLimiter 50ms polling | middleware/adaptive_limiter.go:341 | Wasted CPU cycles | Channel/condvar signaling |

### LOW Impact

| Bottleneck | Location | Impact | Fix |
|-----------|----------|--------|-----|
| No TLS/ResponseHeader timeouts | All proxy transports | Risk of hung connections | Set explicit timeouts |
| AnomalyDetector mutex contention | middleware/anomaly.go:70 | Contention under load | RWMutex or lock-free |
| 5 separate Transport instances | proxy/ package | Can't share idle connections | Shared transport |

---

## Response Time Optimization - Before & After

Date: 2026-04-22

All optimizations target reducing per-request latency. Measurements are estimated under normal load (10-50 req/s).

### 1. Shared HTTP Transport + DNS Cache

**Before**: Each proxy (Anthropic, Gemini, OpenAI, Claude Session) created its own `http.Transport` with default settings. Every outbound connection required a fresh DNS lookup (10-100ms depending on resolver). No connection reuse across proxies.

**After**: Single shared transport via `SharedTransport()` with:
- DNS cache (30s TTL) - eliminates repeated lookups
- Connection pooling (200 idle, 100/host, 120s idle timeout)
- HTTP/2 enabled (`ForceAttemptHTTP2: true`)
- Explicit TLS handshake timeout (10s) and response header timeout (30s)

```
Before: New Transport per proxy, DNS lookup per connection
  Request -> [DNS 10-100ms] -> [TCP+TLS ~50ms] -> [HTTP/1.1] -> Upstream

After:  Shared transport, DNS cached, HTTP/2, connection reuse
  Request -> [DNS cache hit 0ms] -> [reuse conn 0ms] -> [HTTP/2 multiplex] -> Upstream
```

**Expected saving**: 10-100ms per cold connection, 50ms+ on connection reuse.

### 2. Parallel Rate-Limit Checks (errgroup)

**Before**: Global rate-limit HTTP call, then agent rate-limit HTTP call - sequential.
```
Global rate-limit [HTTP ~20ms] -> Agent rate-limit [HTTP ~20ms] -> Process
Total: ~40ms
```

**After**: Both HTTP calls run concurrently via `errgroup`.
```
Global rate-limit [HTTP ~20ms] ─┐
                                ├-> Process
Agent rate-limit [HTTP ~20ms]  ─┘
Total: ~20ms
```

**Expected saving**: ~20ms per request (1 RTT eliminated).

### 3. Shared Image Download Client

**Before**: `FetchImageAsBase64` created a new `http.Client{Timeout: 10s}` on every call. No connection reuse for image downloads.

**After**: Package-level `imageClient = SharedClient(15 * time.Second)` reuses connections.

**Expected saving**: ~30-50ms per image download (TCP/TLS handshake eliminated on warm connections).

### 4. KeyPool: Condvar Signaling

**Before**: `Acquire()` used `time.Sleep(500ms)` polling loop when all keys were in cooldown. Worst case: 10s of sleeping before finding an available key.

**After**: `sync.Cond` + `time.AfterFunc` for event-driven wake-up. When `ReportSuccess()` frees a key, it broadcasts to wake waiting goroutines immediately.

```
Before: Sleep 500ms -> check -> Sleep 500ms -> check -> ... -> Key available
        Worst case: up to 10s wasted sleeping

After:  Wait on condvar -> Key freed -> Broadcast -> Immediate wake -> Key available
        Worst case: <1ms latency after key becomes available
```

**Expected saving**: Up to 10s during cooldown periods, ~250ms average (half of poll interval).

### 5. AdaptiveLimiter: Condvar Instead of Polling

**Before**: `acquireAnyModel()` polled every 50ms checking if capacity was available.

**After**: `sync.Cond` with `time.AfterFunc` timeout. Wakes immediately when capacity frees.

```
Before: Poll 50ms -> check -> Poll 50ms -> check -> ... -> Capacity free
        Average waste: 25ms per wait

After:  Wait on condvar -> Capacity freed -> Broadcast -> Immediate wake
        Average waste: 0ms
```

**Expected saving**: ~25ms average per rate-limited request.

### 6. Parallel PII Span Processing

**Before**: `MaskRequest()` processed text spans sequentially: secrets detection, then PII detection, one span at a time.

**After**: Each span runs in a goroutine with `sync.WaitGroup`. Secrets + PII detection run concurrently per span.

```
Before: Span1[secrets 5ms + PII 5ms] -> Span2[secrets 5ms + PII 5ms] -> ...
        10 spans: ~100ms

After:  Span1[secrets 5ms + PII 5ms] ─┐
        Span2[secrets 5ms + PII 5ms] ─┤
        Span3[...] ─────────────────────┤-> Collect results
        ...                            ─┘
        10 spans: ~10ms (limited by slowest span)
```

**Expected saving**: ~80-90% reduction in masking time for multi-span payloads.

### 7. AnomalyDetector: Welford's Online Algorithm

**Before**: `Record()` iterated over a 1000-element ring buffer on every call to compute mean/stddev. O(n) per sample.

**After**: Welford's online algorithm maintains running mean and variance in O(1) per sample. No buffer iteration needed.

```
Before: Record(value) -> iterate 1000 elements -> compute mean/stddev -> O(1000) ops
After:  Record(value) -> update mean, m2 in 3 arithmetic ops -> O(1)
```

**Expected saving**: ~1000x reduction in computation per anomaly check (microseconds, but compounds under high request rates).

### Summary: Total Expected Improvement

| Optimization | Before | After | Saving |
|-------------|--------|-------|--------|
| DNS + shared transport | 10-100ms per cold conn | 0ms (cached) | 10-100ms |
| Parallel rate-limit | ~40ms sequential | ~20ms concurrent | ~20ms |
| Shared image client | 30-50ms per image | 0-5ms (reuse) | 25-45ms |
| KeyPool condvar | Up to 10s during cooldown | <1ms wake | Up to 10s |
| AdaptiveLimiter condvar | 25ms avg polling waste | 0ms event-driven | ~25ms |
| Parallel PII | ~100ms for 10 spans | ~10ms | ~90ms |
| Welford's anomaly | O(1000) per check | O(1) per check | ~microseconds |

**Estimated per-request improvement** (normal path, no cooldown): **55-175ms** faster
**Worst-case improvement** (all keys in cooldown): **up to 10 seconds** faster

---

## File Structure

- `api-gateway/tokenizer/optimizer.go` - Core module: estimation, whitespace, dedup, truncation, model map, budget
- `api-gateway/tokenizer/optimizer_test.go` - 19 tests + 4 benchmarks
- `api-gateway/tokenizer/effectiveness_test.go` - Real-world effectiveness measurement
- `api-gateway/proxy/anthropic.go` - Integration: ProxyTransparent + ProxyNativeVision
- `api-gateway/proxy/gemini-codeassist.go` - Integration: anthropicToGemini optimizer
- `api-gateway/proxy/gemini-apikey.go` - Uses shared anthropicToGemini
- `docs/token-optimization.md` - Technical analysis (English)
- `docs/token-optimization-th.md` - Thai summary
