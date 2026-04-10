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
	name        string
	inFlight    atomic.Int64
	limit       atomic.Int64
	minLimit    int64
	maxLimit    int64
	minRTT      atomic.Int64 // nanoseconds
	totalReqs   atomic.Int64
	total429s   atomic.Int64
	last429Nano atomic.Int64
	successRun  atomic.Int64 // consecutive successes since last 429
}

// ModelStatus is a snapshot for the /v1/limiter-status endpoint.
type ModelStatus struct {
	Name      string `json:"name"`
	InFlight  int64  `json:"in_flight"`
	Limit     int64  `json:"limit"`
	MaxLimit  int64  `json:"max_limit"`
	TotalReqs int64  `json:"total_requests"`
	Total429s int64  `json:"total_429s"`
	MinRTTMs  int64  `json:"min_rtt_ms"`
}

// modelPriority defines fallback order (higher = preferred).
// Newer models should be tried first before falling back to older ones.
var modelPriority = map[string]int{
	"glm-5.1":     100,
	"glm-5-turbo": 90,
	"glm-5":       80,
	"glm-4.7":     70,
	"glm-4.6":     60,
}

// NewAdaptiveLimiter creates an adaptive concurrency limiter.
//   - limits:       model → max concurrent (also the initial limit)
//   - defaultLimit: for unconfigured models
//   - globalLimit:  hard cap across all models (per-key upstream limit)
func NewAdaptiveLimiter(limits map[string]int, defaultLimit, globalLimit int) *AdaptiveLimiter {
	models := make(map[string]*adaptiveModel, len(limits))
	names := make([]string, 0, len(limits))

	for model, max := range limits {
		am := &adaptiveModel{
			name:     model,
			minLimit: 1,
			maxLimit: int64(max),
		}
		am.limit.Store(int64(max)) // start at configured limit
		models[model] = am
		names = append(names, model)

		slog.Info("adaptive model configured",
			"model", model,
			"initial_limit", max,
			"max_limit", max,
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
// (may differ from the requested model if fallback occurred).
// The caller MUST call Release(selectedModel) when done.
func (al *AdaptiveLimiter) Acquire(requestedModel string) string {
	// Wait for a global slot (caps total concurrent across all models).
	for {
		g := al.globalInFlight.Add(1)
		if g <= al.globalLimit {
			break
		}
		al.globalInFlight.Add(-1)
		time.Sleep(5 * time.Millisecond)
	}

	// Try the requested model (non-blocking).
	model := al.getModel(requestedModel)
	if model.tryAcquire() {
		return requestedModel
	}

	// Requested model full — try fallbacks by priority (non-blocking).
	// Skip models with significantly lower priority (tier gap >= 2 levels).
	// Use round-robin rotation to distribute traffic evenly among same-tier models.
	reqPrio := modelPriority[requestedModel]
	offset := int(al.rrEpoch.Add(1))
	for i := 0; i < len(al.fallbackOrder); i++ {
		idx := (i + offset) % len(al.fallbackOrder)
		fb := al.fallbackOrder[idx]
		if fb == requestedModel {
			continue
		}
		// Only fallback within the same tier or one tier below.
		fbPrio, ok := modelPriority[fb]
		if ok && reqPrio > 0 && fbPrio < reqPrio-20 {
			continue
		}
		fm := al.models[fb]
		if fm.tryAcquire() {
			slog.Info("model fallback",
				"requested", requestedModel,
				"selected", fb,
			)
			return fb
		}
	}

	// All models full — block-wait on the originally requested model.
	slog.Debug("all models full, waiting", "requested", requestedModel)
	model.acquireBlocking()
	return requestedModel
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
		newLim := old * 5 / 10 // multiplicative decrease ×0.5
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
		additive := math.Max(1, math.Sqrt(float64(oldLimit)))
		newLimit = int64(gradient*float64(oldLimit) + additive)
	} else {
		newLimit = oldLimit + 1 // slow start
	}

	if newLimit > am.maxLimit {
		newLimit = am.maxLimit
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
			Name:      name,
			InFlight:  m.inFlight.Load(),
			Limit:     m.limit.Load(),
			MaxLimit:  m.maxLimit,
			TotalReqs: m.totalReqs.Load(),
			Total429s: m.total429s.Load(),
			MinRTTMs:  minRTTMs,
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
	cur := am.inFlight.Add(1)
	if cur <= am.limit.Load() {
		return true
	}
	am.inFlight.Add(-1)
	return false
}

func (am *adaptiveModel) acquireBlocking() {
	for {
		cur := am.inFlight.Add(1)
		if cur <= am.limit.Load() {
			return
		}
		am.inFlight.Add(-1)
		time.Sleep(5 * time.Millisecond)
	}
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
	// RFC 7231 also allows HTTP-date format — skip for simplicity.
	return 0
}
