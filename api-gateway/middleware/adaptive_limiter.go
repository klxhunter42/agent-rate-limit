package middleware

import (
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// AdaptiveLimiter controls concurrent upstream requests with automatic
// adjustment based on upstream feedback (429s, latency, rate-limit headers).
//
// Algorithm (inspired by Envoy gradient controller + Netflix concurrency limits):
//
//	On 429:   limit_new = max(minLimit, limit * 0.5)
//	On 200:   gradient   = (minRTT + buffer) / sampleRTT
//	          limit_new  = min(maxLimit, gradient * limit + sqrt(limit))
//	Cooldown: 5 s after any 429 before increasing again.
//
// All hot-path operations are lock-free (atomic counters).
type AdaptiveLimiter struct {
	globalInFlight atomic.Int64
	globalLimit    int64

	models        map[string]*adaptiveModel
	defaultModel  *adaptiveModel
	fallbackOrder []string
	rrEpoch       atomic.Uint64 // round-robin counter for fallback rotation

	mu sync.Mutex // serialises limit adjustments (cold path)
}

type adaptiveModel struct {
	name          string
	inFlight      atomic.Int64
	limit         atomic.Int64
	minLimit      int64
	maxLimit      int64
	minRTT        atomic.Int64 // nanoseconds
	totalReqs     atomic.Int64
	total429s     atomic.Int64
	last429Nano   atomic.Int64
	successRun    atomic.Int64 // consecutive successes since last 429
	peakBefore429 atomic.Int64 // highest limit before last 429 (learned ceiling)
	peakSetNano   atomic.Int64 // when peakBefore429 was set (for decay)
}

// ModelStatus is a snapshot for the /v1/limiter-status endpoint.
type ModelStatus struct {
	Name           string `json:"name"`
	InFlight       int64  `json:"in_flight"`
	Limit          int64  `json:"limit"`
	MaxLimit       int64  `json:"max_limit"`
	LearnedCeiling int64  `json:"learned_ceiling"`
	TotalReqs      int64  `json:"total_requests"`
	Total429s      int64  `json:"total_429s"`
	MinRTTMs       int64  `json:"min_rtt_ms"`
}

// modelPriority defines fallback order (higher = preferred).
// Newer models should be tried first before falling back to older ones.
var modelPriority = map[string]int{
	"glm-5.1":     100,
	"glm-5-turbo": 90,
	"glm-5":       80,
	"glm-4.7":     70,
	"glm-4.6":     60,
	"glm-4.5":     50,
}

// NewAdaptiveLimiter creates an adaptive concurrency limiter.
//   - limits:       model -> max concurrent (also the initial limit)
//   - defaultLimit: for unconfigured models
//   - globalLimit:  hard cap across all models (per-key upstream limit)
func NewAdaptiveLimiter(limits map[string]int, defaultLimit, globalLimit, probeMultiplier int) *AdaptiveLimiter {
	if globalLimit <= 0 {
		panic("globalLimit must be positive")
	}

	models := make(map[string]*adaptiveModel, len(limits))
	names := make([]string, 0, len(limits))

	for model, max := range limits {
		// Probe ceiling: allow adaptive limiter to discover the real
		// upstream limit by probing up to probeMultiplier x the initial limit.
		// On 429 it backs off, so it naturally converges to the true ceiling.
		if probeMultiplier <= 0 {
			probeMultiplier = 10
		}
		probeMax := int64(max) * int64(probeMultiplier)
		am := &adaptiveModel{
			name:     model,
			minLimit: 1,
			maxLimit: probeMax,
		}
		am.limit.Store(int64(max)) // start at documented limit
		models[model] = am
		names = append(names, model)

		slog.Info("adaptive model configured",
			"model", model,
			"initial_limit", max,
			"max_limit", probeMax,
		)
	}
	// Sort by priority (newer models first) instead of alphabetically.
	sort.Slice(names, func(i, j int) bool {
		pi, ok := modelPriority[names[i]]
		if !ok {
			pi = 0
		}
		pj, ok := modelPriority[names[j]]
		if !ok {
			pj = 0
		}
		return pi > pj
	})

	dm := &adaptiveModel{name: "_default", minLimit: 1, maxLimit: int64(defaultLimit)}
	dm.limit.Store(int64(defaultLimit))

	al := &AdaptiveLimiter{
		globalLimit:   int64(globalLimit),
		models:        models,
		defaultModel:  dm,
		fallbackOrder: names,
	}

	slog.Info("adaptive limiter initialized",
		"models", names,
		"global_limit", globalLimit,
	)
	return al
}

// Acquire obtains a concurrency slot. Returns the model name that was selected
// (may differ from the requested model if fallback occurred) and whether
// acquisition succeeded. On failure the caller MUST NOT call Release.
// The caller MUST call Release(selectedModel) on success when done.
func (al *AdaptiveLimiter) Acquire(requestedModel string) (string, bool) {
	// Wait for a global slot (caps total concurrent across all models).
	// Use CAS loop instead of add-then-check to avoid TOCTOU race that
	// allows globalInFlight to exceed globalLimit under high concurrency.
	for {
		cur := al.globalInFlight.Load()
		if cur >= al.globalLimit {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if al.globalInFlight.CompareAndSwap(cur, cur+1) {
			break
		}
	}

	// Try the requested model (non-blocking).
	model := al.getModel(requestedModel)
	if model.tryAcquire() {
		return requestedModel, true
	}

	// Requested model full - distribute within same series,
	// expand to nearby series when same-series is >= 40% utilized.
	reqPrio := modelPriority[requestedModel]

	type candidate struct {
		name string
		am   *adaptiveModel
	}

	var sameSeries, nearbySeries []candidate
	var sameTotal, sameUsed int64
	for _, fb := range al.fallbackOrder {
		fbPrio, ok := modelPriority[fb]
		if !ok || fbPrio < reqPrio-50 {
			continue
		}
		fm := al.models[fb]
		headroom := fm.limit.Load() - fm.inFlight.Load()
		if headroom <= 0 {
			// Still count utilization even if full.
			if fbPrio >= reqPrio-30 {
				sameTotal += fm.limit.Load()
				sameUsed += fm.inFlight.Load()
			}
			continue
		}
		c := candidate{name: fb, am: fm}
		if fbPrio >= reqPrio-30 {
			sameSeries = append(sameSeries, c)
			sameTotal += fm.limit.Load()
			sameUsed += fm.inFlight.Load()
		} else {
			nearbySeries = append(nearbySeries, c)
		}
	}

	// If same-series is >= 40% utilized, merge nearby series into pool.
	sameUtilPct := float64(0)
	if sameTotal > 0 {
		sameUtilPct = float64(sameUsed) / float64(sameTotal)
	}
	var pool []candidate
	pool = append(pool, sameSeries...)
	if sameUtilPct >= 0.4 && len(nearbySeries) > 0 {
		pool = append(pool, nearbySeries...)
	}

	tryRR := func(candidates []candidate) (string, *adaptiveModel) {
		if len(candidates) == 0 {
			return "", nil
		}
		offset := al.rrEpoch.Add(1)
		for i := range candidates {
			c := candidates[(int(offset)+i)%len(candidates)]
			if c.am.tryAcquire() {
				return c.name, c.am
			}
		}
		return "", nil
	}

	if picked, am := tryRR(pool); am != nil {
		if picked != requestedModel {
			slog.Info("model fallback (distributed)",
				"requested", requestedModel,
				"selected", picked,
				"same_series_util", sameUtilPct,
			)
		}
		return picked, true
	}

	// All models full - release global slot before blocking wait to avoid
	// starving other requests under heavy load.
	al.globalInFlight.Add(-1)
	slog.Debug("all models full, waiting", "requested", requestedModel)
	if model.acquireBlocking(30 * time.Second) {
		// Re-acquire global slot after model slot obtained.
		for {
			cur := al.globalInFlight.Load()
			if cur >= al.globalLimit {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			if al.globalInFlight.CompareAndSwap(cur, cur+1) {
				break
			}
		}
		return requestedModel, true
	}
	return "", false
}

// Release frees the concurrency slot acquired by Acquire.
func (al *AdaptiveLimiter) Release(model string) {
	am := al.getModel(model)
	am.inFlight.Add(-1)
	al.globalInFlight.Add(-1)
}

// Feedback adjusts limits based on the upstream response.
// Called by the proxy after every upstream attempt (including 429 retries).
func (al *AdaptiveLimiter) Feedback(model string, statusCode int, rtt time.Duration, headers http.Header) {
	am := al.getModel(model)
	am.totalReqs.Add(1)
	rttNano := rtt.Nanoseconds()

	if statusCode == 429 || statusCode == 503 {
		am.total429s.Add(1)
		am.last429Nano.Store(time.Now().UnixNano())
		am.successRun.Store(0)

		al.mu.Lock()
		defer al.mu.Unlock()

		old := am.limit.Load()
		am.peakBefore429.Store(old) // remember ceiling before 429
		am.peakSetNano.Store(time.Now().UnixNano())
		newLim := old * 5 / 10 // multiplicative decrease x0.5
		if newLim < am.minLimit {
			newLim = am.minLimit
		}
		if newLim != old {
			am.limit.Store(newLim)
			slog.Warn("adaptive limit decreased after 429/503",
				"model", model,
				"old", old,
				"new", newLim,
			)
		}
		return
	}

	if statusCode < 200 || statusCode >= 300 {
		return // ignore non-success non-429
	}

	// --- Success path ---
	successes := am.successRun.Add(1)

	// Update minRTT (lock-free CAS loop).
	for {
		old := am.minRTT.Load()
		if rttNano > 0 && (old == 0 || rttNano < old) {
			if am.minRTT.CompareAndSwap(old, rttNano) {
				break
			}
			continue
		}
		break
	}

	// Cooldown: don't increase within 5 s of last 429.
	last429 := am.last429Nano.Load()
	if last429 > 0 && time.Now().UnixNano()-last429 < int64(5*time.Second) {
		return
	}

	// Only consider increase every 5 consecutive successes.
	if successes%5 != 0 {
		return
	}

	al.mu.Lock()
	defer al.mu.Unlock()

	oldLimit := am.limit.Load()
	if oldLimit >= am.maxLimit {
		return
	}

	var newLimit int64
	minRTTNano := am.minRTT.Load()
	if minRTTNano > 0 && rttNano > 0 {
		// Envoy gradient controller formula.
		bufferNano := minRTTNano / 10
		gradient := float64(minRTTNano+bufferNano) / float64(rttNano)
		gradient = math.Min(2.0, math.Max(0.8, gradient)) // prevent oscillation
		additive := math.Max(1, math.Sqrt(float64(oldLimit)))
		newLimit = int64(gradient*float64(oldLimit) + additive)
	} else {
		newLimit = oldLimit + 1 // slow start
	}

	if newLimit > am.maxLimit {
		newLimit = am.maxLimit
	}
	// Decay learned ceiling: if 5 min since last 429, allow re-probing.
	if peak := am.peakBefore429.Load(); peak > 0 {
		peakAge := time.Duration(time.Now().UnixNano() - am.peakSetNano.Load())
		if peakAge > 5*time.Minute {
			am.peakBefore429.Store(0)
			slog.Info("learned ceiling decayed, allowing re-probe", "model", model, "old_peak", peak)
		} else if newLimit >= peak {
			newLimit = peak - 1
		}
	}
	if newLimit > oldLimit {
		am.limit.Store(newLimit)
		slog.Info("adaptive limit increased",
			"model", model,
			"old", oldLimit,
			"new", newLimit,
			"successes", successes,
		)
	}
}

// Status returns a snapshot of all model limits for monitoring.
func (al *AdaptiveLimiter) Status() []ModelStatus {
	var out []ModelStatus
	for _, name := range al.fallbackOrder {
		m := al.models[name]
		minRTTMs := m.minRTT.Load() / int64(time.Millisecond)
		out = append(out, ModelStatus{
			Name:           name,
			InFlight:       m.inFlight.Load(),
			Limit:          m.limit.Load(),
			MaxLimit:       m.maxLimit,
			LearnedCeiling: m.peakBefore429.Load(),
			TotalReqs:      m.totalReqs.Load(),
			Total429s:      m.total429s.Load(),
			MinRTTMs:       minRTTMs,
		})
	}
	return out
}

// GlobalStatus returns global limiter stats.
type GlobalStatus struct {
	GlobalInFlight int64 `json:"global_in_flight"`
	GlobalLimit    int64 `json:"global_limit"`
}

func (al *AdaptiveLimiter) GlobalStatus() GlobalStatus {
	return GlobalStatus{
		GlobalInFlight: al.globalInFlight.Load(),
		GlobalLimit:    al.globalLimit,
	}
}

// --- internal helpers ---

func (al *AdaptiveLimiter) getModel(name string) *adaptiveModel {
	if m, ok := al.models[name]; ok {
		return m
	}
	return al.defaultModel
}

func (am *adaptiveModel) tryAcquire() bool {
	for {
		cur := am.inFlight.Load()
		if cur >= am.limit.Load() {
			return false
		}
		if am.inFlight.CompareAndSwap(cur, cur+1) {
			return true
		}
	}
}

// tryAcquireWithTimeout waits up to timeout for a slot on this model.
// Returns false if timeout elapsed without acquiring.
func (am *adaptiveModel) tryAcquireWithTimeout(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for {
			cur := am.inFlight.Load()
			limit := am.limit.Load()
			if limit == 0 || cur >= limit {
				break
			}
			if am.inFlight.CompareAndSwap(cur, cur+1) {
				return true
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func (am *adaptiveModel) acquireBlocking(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for {
			cur := am.inFlight.Load()
			limit := am.limit.Load()
			if limit == 0 || cur >= limit {
				break
			}
			if am.inFlight.CompareAndSwap(cur, cur+1) {
				return true
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// parseRetryAfter extracts the Retry-After header value as a duration.
func parseRetryAfter(headers http.Header) time.Duration {
	v := headers.Get("Retry-After")
	if v == "" {
		return 0
	}
	if sec, err := strconv.Atoi(v); err == nil {
		return time.Duration(sec) * time.Second
	}
	// RFC 7231 also allows HTTP-date format - skip for simplicity.
	return 0
}
