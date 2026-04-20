# Performance: Adaptive Concurrency Limiter

## Algorithm Overview

The adaptive concurrency limiter controls how many simultaneous upstream requests the gateway issues per model and globally. It combines ideas from the Envoy gradient controller and Netflix concurrency limits.

```
Request flow:
  Client -> Acquire() -> proxy to upstream -> Feedback() -> Release()
```

Three core mechanisms:

| Mechanism | Purpose |
|---|---|
| Per-model concurrent slots | Prevents overloading any single upstream model |
| Global slot pool | Caps total upstream API key concurrency |
| Gradient-based feedback | Auto-tunes per-model limits from real RTT/429 data |

---

## 3-Phase Acquire

When a request arrives at `POST /v1/messages`, the handler calls `Acquire(requestedModel)`:

```
Phase 1: acquireGlobal   (60s timeout)
   |
   +--> Wait for global slot via sync.Cond
        If globalInFlight < globalLimit: proceed
        Else: block until signal or timeout
   |
Phase 2: tryFallback
   |
   +--> Try requested model (non-blocking CAS)
   |    If slot available: done
   |
   +--> Same-series round-robin (non-blocking)
   |    e.g. glm-5.1 full -> try glm-5-turbo, glm-5
   |
   +--> Lower-series spillover (if same-series full OR latency pressure)
   |    e.g. series 5 pressured -> try series 4 models
   |
Phase 3: acquireAnyModel  (30s timeout, blocking poll)
   |
   +--> Release global slot (avoid deadlock while waiting)
   +--> Poll all models in priority order every 50ms
   +--> Re-acquire global slot when model slot found
```

If all phases fail, the handler returns `503 Overloaded`.

### Priority Ordering

Models are sorted by priority (higher = preferred):

| Model | Priority | Series |
|---|---|---|
| glm-5.1 | 100 | 5 |
| glm-5-turbo | 90 | 5 |
| glm-5 | 80 | 5 |
| glm-4.7 | 70 | 4 |
| glm-4.6 | 60 | 4 |
| glm-4.5 | 50 | 4 |

Series is extracted from the model name: `glm-5.1` -> series 5, `glm-4.7` -> series 4, `glm-5-turbo` -> series 5.

### Latency Pressure Trigger

A series is considered under latency pressure when a majority of its models have `ewmaRTT > 1.5 * minRTT`. This triggers spillover to the next lower series even if same-series models still have capacity.

```
seriesLatencyPressure(series):
  for each model in series:
    if ewmaRTT > minRTT * 1.5: pressured++
  return pressured >= majority
```

---

## Feedback Loop

After every upstream response, the handler calls `Feedback(model, statusCode, rtt, headers)`.

### On 429 or 503

```
limit_new = max(minLimit, limit * 0.5)
peakBefore429 = old limit     // remember where we were
peakSetNano = now
successRun = 0
```

The learned ceiling (`peakBefore429`) records the highest stable limit before the 429. On recovery, the limiter will not exceed `peak - 1` until the ceiling decays (5 minutes).

### On Success (every 5th consecutive success)

```
if in cooldown (5s after last 429): skip
if limit >= maxLimit: skip
if model has manual override: skip

gradient = (minRTT + minRTT/10) / sampleRTT
gradient = clamp(gradient, 0.8, 2.0)
additive = max(1, sqrt(limit))
limit_new = gradient * limit + additive

if limit_new > maxLimit: limit_new = maxLimit
if learned ceiling active and not decayed (5 min):
    limit_new = min(limit_new, peak - 1)
```

The gradient formula:
- `gradient > 1.0` means RTT is lower than minRTT (system faster than observed minimum), so increase aggressively
- `gradient < 1.0` means RTT is rising, so increase conservatively
- Clamped to `[0.8, 2.0]` to prevent wild swings

The `sqrt(limit)` additive term ensures the limit grows even when gradient is exactly 1.0, similar to additive increase in TCP AIMD.

### EWMA RTT Tracking

```
alpha = 0.3
ewmaRTT_new = ewmaRTT_old * 0.7 + sampleRTT * 0.3
```

Uses lock-free CAS loop. The EWMA provides a smoothed latency signal for the gradient calculation and latency pressure detection.

### minRTT Discovery

The minimum observed RTT is tracked via lock-free CAS. This serves as the baseline "fastest possible" response time for the gradient calculation. Updated on every success:

```
if sampleRTT < minRTT: minRTT = sampleRTT
```

### Learned Ceiling Decay

```
if time since peakSetNano > 5 minutes:
    clear peakBefore429
    allow re-probing above the old ceiling
```

This prevents the limiter from being permanently stuck below the true capacity. After 5 minutes of stability, it is allowed to probe higher again.

### Cooldown Period

5 seconds after any 429, the limiter will not increase any model's limit. This gives upstream systems time to recover from rate-limit pressure before the gateway starts ramping up again.

---

## Model Fallback Strategy

When a model slot is unavailable, fallback follows this order:

```
1. Requested model (exact match, non-blocking)
2. Same-series round-robin (e.g. glm-5.1 -> glm-5-turbo -> glm-5)
3. Lower-series spillover (series 5 -> series 4, if pressured)
4. Blocking poll all models in priority order (50ms interval)
```

### Provider Re-Resolution on Fallback

When fallback selects a different model, the handler re-resolves the provider:

```go
if selectedModel != requestedModel {
    if fb := resolver.Resolve(selectedModel); fb != nil {
        decision = fb    // new upstream URL + apiKey
    }
}
```

This ensures the request is routed to the correct upstream endpoint and uses the right API key for the fallback model.

---

## Configuration Defaults

| Environment Variable | Default | Description |
|---|---|---|
| `UPSTREAM_MODEL_LIMITS` | `glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3,glm-4.5:10` | Per-model initial concurrent request limits |
| `UPSTREAM_GLOBAL_LIMIT` | `9` | Hard cap on total concurrent upstream requests |
| `UPSTREAM_PROBE_MULTIPLIER` | `5` | maxLimit = initialLimit * probeMultiplier |
| `MODEL_PRIORITY` | (built-in map) | Model priority ordering for fallback |

Key points:
- These are **concurrent request limits**, not RPM
- `initialLimit` is also the starting value for the adaptive algorithm
- `maxLimit` bounds how high the adaptive algorithm can probe
- `globalLimit` is the total concurrency cap across all models (typically the upstream API key's concurrent request limit)

### Per-Model Limit Range

| Model | Initial | Max (x5) |
|---|---|---|
| glm-5.1 | 1 | 5 |
| glm-5-turbo | 1 | 5 |
| glm-5 | 2 | 10 |
| glm-4.7 | 2 | 10 |
| glm-4.6 | 3 | 15 |
| glm-4.5 | 10 | 50 |

Sum of initial limits (19) exceeds global limit (9), so contention is expected and fallback is normal under load.

---

## Tuning Guidelines

### Probe Multiplier

Controls how aggressively the limiter probes for higher concurrency.

| Scenario | Action |
|---|---|
| Frequent 429s even at low concurrency | Decrease to 3 (more conservative) |
| Limits stuck well below upstream capacity | Increase to 8-10 (more aggressive probing) |
| Default (5) works for most upstreams | No change needed |

### Global Limit

Sets the total concurrent request cap. This should match or slightly exceed the upstream API key's true concurrent request capacity.

| Scenario | Action |
|---|---|
| Upstream returns 429 even when individual model limits are low | Global limit is too high; decrease |
| Global limit blocks requests when individual models have capacity | Increase global limit or add more API keys |
| Multiple API keys in rotation | Set global limit = sum of per-key limits |

### Cooldown Period

The 5-second cooldown after any 429 prevents rapid oscillation.

| Scenario | Action |
|---|---|
| Recovery too slow after transient 429 | Reduce to 2-3s (faster ramp-up) |
| Oscillating limits (up/down/up) | Increase to 8-10s (more damping) |

Cooldown is hardcoded. To change it, modify the constant in `Feedback()`.

### Reading `/v1/limiter-status`

```json
GET /v1/limiter-status

{
  "global": {
    "global_in_flight": 7,
    "global_limit": 9
  },
  "models": [
    {
      "name": "glm-5.1",
      "in_flight": 1,
      "limit": 3,
      "max_limit": 5,
      "learned_ceiling": 0,
      "total_requests": 1234,
      "total_429s": 3,
      "min_rtt_ms": 450,
      "ewma_rtt_ms": 520,
      "series": 5,
      "overridden": false
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `limit` | Current adaptive concurrency limit |
| `max_limit` | Upper bound (initial * probeMultiplier) |
| `learned_ceiling` | Limit value before last 429 (0 if decayed or never hit) |
| `in_flight` | Currently active requests for this model |
| `min_rtt_ms` | Lowest observed RTT (baseline for gradient) |
| `ewma_rtt_ms` | Smoothed RTT (for pressure detection) |
| `total_429s` | Cumulative 429/503 responses received |
| `overridden` | Manual override active (limit is pinned) |

**Diagnostic patterns:**

- `limit` stuck at `learned_ceiling - 1`: the limiter is respecting a recently learned ceiling. Wait 5 minutes for decay or clear via `/v1/limiter-override`.
- `ewma_rtt_ms >> min_rtt_ms`: upstream is slow, expect spillover to lower series.
- `in_flight == limit` and high `total_429s`: limit may be too high, algorithm should self-correct.
- `global_in_flight == global_limit`: total concurrency saturated, requests will queue or fail.

### Manual Override

Pin a model's limit (bypasses adaptive algorithm):

```json
POST /v1/limiter-override
{"model": "glm-5.1", "limit": 3}
```

Clear override:

```json
POST /v1/limiter-override
{"model": "glm-5.1", "limit": 0}
```

When overridden, the feedback loop still tracks RTT and success stats but does not change the limit.

---

## Performance Characteristics

### Lock-Free Hot Path

The per-model `tryAcquire` uses atomic CAS:

```go
func (am *adaptiveModel) tryAcquire() bool {
    for {
        cur := am.inFlight.Load()
        if cur >= am.limit.Load() { return false }
        if am.inFlight.CompareAndSwap(cur, cur+1) { return true }
    }
}
```

No mutex contention on the request path. The `sync.Mutex` in `AdaptiveLimiter.mu` only serializes limit adjustments (cold path, every 5th success).

### Signal-Based Waiting

`acquireGlobal` uses `sync.Cond.Wait()` instead of spin-waiting. Goroutines block on a condition variable and are woken by `Signal()` on `Release()`. This avoids CPU waste when all slots are occupied.

### sync.Pool for Candidate Slices

`tryFallback` uses a `sync.Pool` to reuse candidate slices:

```go
candPool: sync.Pool{
    New: func() any {
        s := make([]seriesEntry, 0, maxModelsPerSeries)
        return &s
    },
}
```

Each fallback attempt borrows a slice, populates it with eligible models, and returns it. This eliminates per-request heap allocations in the fallback path.

### Global Limit as Upstream Concurrency Cap

The global limit is not just a safety net. It represents the upstream API key's maximum concurrent request capacity. When `globalInFlight == globalLimit`:

- All new requests block at `acquireGlobal`
- Existing requests complete and `Release()` signals waiting goroutines
- This creates a natural backpressure mechanism that matches the gateway's output to the upstream's actual capacity

### Metrics Export

Limiter state is exported to Prometheus every 10 seconds via `UpdateAdaptiveMetrics`. The `LimiterStatus` endpoint provides real-time JSON snapshots for the dashboard and debugging.
