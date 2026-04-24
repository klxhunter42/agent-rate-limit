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
	seriesBuckets map[int][]seriesEntry // pre-computed: series -> model entries
	rrEpoch       atomic.Uint64         // round-robin counter for fallback rotation

	// Signal-based waiting replaces spin-wait for slot availability.
	globalCond *sync.Cond
	modelConds map[string]*sync.Cond // per-model signal on Release

	overrides  map[string]int64 // model -> pinned limit (0 = no override)
	overrideMu sync.RWMutex     // protects overrides

	candPool sync.Pool // pool candidate slices to reduce GC pressure

	seenModels sync.Map // model name -> struct{} — tracks all models seen at runtime

	mu sync.Mutex // serialises limit adjustments (cold path)
}

type seriesEntry struct {
	name string
	am   *adaptiveModel
}

type adaptiveModel struct {
	name          string
	series        int // cached at init to avoid repeated string parsing
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
	rttEWMA       atomic.Int64 // exponentially weighted moving average RTT (nanoseconds)
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
	EwmaRTTMs      int64  `json:"ewma_rtt_ms"`
	Series         int    `json:"series"`
	Overridden     bool   `json:"overridden"`
}

// modelPriority defines fallback order (higher = preferred).
// Populated from config or defaults.
var modelPriority = map[string]int{
	"glm-5.1":     100,
	"glm-5-turbo": 90,
	"glm-5":       80,
	"glm-4.7":     70,
	"glm-4.6":     60,
	"glm-4.5":     50,
}

// SetModelPriority overrides the default model priority map.
func SetModelPriority(p map[string]int) {
	if len(p) > 0 {
		modelPriority = p
	}
}

// maxModelsPerSeries is the pool slice capacity.
const maxModelsPerSeries = 8

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
		if probeMultiplier <= 0 {
			probeMultiplier = 10
		}
		probeMax := int64(max) * int64(probeMultiplier)
		am := &adaptiveModel{
			name:     model,
			series:   modelSeries(model),
			minLimit: 1,
			maxLimit: probeMax,
		}
		am.limit.Store(int64(max))
		models[model] = am
		names = append(names, model)

		slog.Info("adaptive model configured",
			"model", model,
			"initial_limit", max,
			"max_limit", probeMax,
		)
	}
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

	dm := &adaptiveModel{name: "_default", series: 0, minLimit: 1, maxLimit: int64(defaultLimit)}
	dm.limit.Store(int64(defaultLimit))

	var globalMu sync.Mutex
	modelConds := make(map[string]*sync.Cond, len(models))
	for name := range models {
		modelConds[name] = sync.NewCond(&sync.Mutex{})
	}

	al := &AdaptiveLimiter{
		globalLimit:   int64(globalLimit),
		models:        models,
		defaultModel:  dm,
		fallbackOrder: names,
		seriesBuckets: buildSeriesBuckets(names, models),
		globalCond:    sync.NewCond(&globalMu),
		modelConds:    modelConds,
		overrides:     make(map[string]int64),
		candPool: sync.Pool{
			New: func() any {
				s := make([]seriesEntry, 0, maxModelsPerSeries)
				return &s
			},
		},
	}

	slog.Info("adaptive limiter initialized",
		"models", names,
		"global_limit", globalLimit,
	)
	return al
}

func buildSeriesBuckets(names []string, models map[string]*adaptiveModel) map[int][]seriesEntry {
	buckets := make(map[int][]seriesEntry)
	for _, name := range names {
		am := models[name]
		s := am.series
		buckets[s] = append(buckets[s], seriesEntry{name: name, am: am})
	}
	return buckets
}

// Acquire obtains a concurrency slot. Returns the model name that was selected
// (may differ from the requested model if fallback occurred) and whether
// acquisition succeeded. On failure the caller MUST NOT call Release.
// The caller MUST call Release(selectedModel) on success when done.
func (al *AdaptiveLimiter) Acquire(requestedModel string) (string, bool) {
	// Wait for a global slot with timeout (60s) to prevent goroutine leaks.
	if !al.acquireGlobal(60 * time.Second) {
		return "", false
	}

	model := al.getModel(requestedModel)

	// Proactive distribution: round-robin across same-series models when
	// multiple models share the same capability tier. This prevents a single
	// model from absorbing all traffic while its peers sit idle.
	// Under high load (same-series near capacity or recent 429s), also
	// consider lower-series models for cross-series distribution.
	if model.series > 0 && len(al.seriesBuckets[model.series]) > 1 {
		selected, ok := al.tryFallback(requestedModel, model)
		if ok {
			return selected, true
		}
	} else if model.series > 0 && al.shouldDistributeToLower(model.series) {
		selected, ok := al.tryCrossSeries(requestedModel, model)
		if ok {
			return selected, true
		}
	} else if model.tryAcquire() {
		return requestedModel, true
	}

	// All immediate candidates full - release global slot and retry with backoff.
	al.globalInFlight.Add(-1)
	al.globalCond.Signal()
	return al.acquireAnyModel(30*time.Second, requestedModel)
}

// acquireGlobal waits for a global concurrency slot with a timeout.
func (al *AdaptiveLimiter) acquireGlobal(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Goroutine to broadcast on timeout so we unblock from Wait().
	go func() {
		select {
		case <-timer.C:
			al.globalCond.Broadcast()
		case <-time.After(timeout + time.Second):
		}
	}()

	al.globalCond.L.Lock()
	for al.globalInFlight.Load() >= al.globalLimit {
		if time.Now().After(deadline) {
			al.globalCond.L.Unlock()
			return false
		}
		al.globalCond.Wait()
	}
	al.globalInFlight.Add(1)
	al.globalCond.L.Unlock()
	return true
}

// tryFallback attempts same-series round-robin then lower-series spillover.
func (al *AdaptiveLimiter) tryFallback(requestedModel string, model *adaptiveModel) (string, bool) {
	reqSeries := model.series

	sameSeries := al.getCandidates(al.seriesBuckets[reqSeries])
	lowerSeries := al.getCandidates(al.seriesBuckets[reqSeries-1])
	defer al.putCandidates(sameSeries)
	defer al.putCandidates(lowerSeries)

	tryRR := func(candidates *[]seriesEntry) (string, *adaptiveModel) {
		if len(*candidates) == 0 {
			return "", nil
		}
		offset := al.rrEpoch.Add(1)
		for i := range *candidates {
			c := (*candidates)[(int(offset)+i)%len(*candidates)]
			if c.am.tryAcquire() {
				return c.name, c.am
			}
		}
		return "", nil
	}

	// Phase 1: round-robin within same series.
	if picked, _ := tryRR(sameSeries); picked != "" {
		if picked != requestedModel {
			slog.Debug("proactive model distribution",
				"requested", requestedModel,
				"selected", picked,
				"series", reqSeries,
			)
		}
		return picked, true
	}

	// Phase 2: spill to lower series when same-series is full, under latency pressure,
	// or recently hit 429 on this series (immediate spill reduces cascading retries).
	sameSeriesFull := len(*sameSeries) == 0
	recent429 := al.seriesRecent429(reqSeries)
	if (sameSeriesFull || recent429 || al.seriesLatencyPressure(reqSeries)) && len(*lowerSeries) > 0 {
		if picked, _ := tryRR(lowerSeries); picked != "" {
			reason := "same-series full"
			if recent429 {
				reason = "recent-429 spill"
			} else if !sameSeriesFull {
				reason = "latency pressure"
			}
			slog.Info("series spillover ("+reason+")",
				"requested", requestedModel,
				"selected", picked,
				"from_series", reqSeries,
				"to_series", al.models[picked].series,
			)
			return picked, true
		}
	}

	return "", false
}

// acquireAnyModel waits for ANY available model slot with a timeout.
// Uses sync.Cond to wake immediately on Release instead of polling.
func (al *AdaptiveLimiter) acquireAnyModel(timeout time.Duration, requestedModel string) (string, bool) {
	deadline := time.Now().Add(timeout)

	// Create a temporary condvar for this wait.
	var waitMu sync.Mutex
	waitCond := sync.NewCond(&waitMu)

	// Timer to broadcast on timeout so we unblock from Wait().
	timer := time.AfterFunc(timeout, func() {
		waitCond.Broadcast()
	})
	defer timer.Stop()

	for {
		// Try the requested model first.
		model := al.getModel(requestedModel)
		if model.tryAcquire() {
			if al.acquireGlobal(time.Until(deadline)) {
				return requestedModel, true
			}
			model.inFlight.Add(-1)
			return "", false
		}

		// Try any model in fallback order.
		for _, name := range al.fallbackOrder {
			am := al.models[name]
			if am.tryAcquire() {
				if al.acquireGlobal(time.Until(deadline)) {
					slog.Info("model fallback (cond-wait)",
						"requested", requestedModel,
						"selected", name,
					)
					return name, true
				}
				am.inFlight.Add(-1)
				return "", false
			}
		}

		if time.Now().After(deadline) {
			return "", false
		}

		// Wait for signal from Release() or timeout.
		waitMu.Lock()
		remaining := time.Until(deadline)
		if remaining > 100*time.Millisecond {
			waitCond.Wait()
		} else {
			waitMu.Unlock()
			time.Sleep(remaining)
			continue
		}
		waitMu.Unlock()
	}
}

func (al *AdaptiveLimiter) Release(model string) {
	am := al.getModel(model)
	am.inFlight.Add(-1)
	al.globalInFlight.Add(-1)

	if c, ok := al.modelConds[model]; ok {
		c.Signal()
	}
	al.globalCond.Signal()
}

// SetOverride pins a model's concurrency limit to a specific value.
// Set to 0 to clear the override.
func (al *AdaptiveLimiter) SetOverride(model string, limit int64) {
	al.overrideMu.Lock()
	defer al.overrideMu.Unlock()
	if limit <= 0 {
		delete(al.overrides, model)
		slog.Info("adaptive override cleared", "model", model)
		return
	}
	al.overrides[model] = limit
	// Apply immediately.
	if am, ok := al.models[model]; ok {
		am.limit.Store(limit)
	}
	slog.Info("adaptive override set", "model", model, "limit", limit)
}

// Overrides returns current override state.
func (al *AdaptiveLimiter) Overrides() map[string]int64 {
	al.overrideMu.RLock()
	defer al.overrideMu.RUnlock()
	out := make(map[string]int64, len(al.overrides))
	for k, v := range al.overrides {
		out[k] = v
	}
	return out
}

// getCandidates returns a pooled slice populated with entries that have headroom.
func (al *AdaptiveLimiter) getCandidates(entries []seriesEntry) *[]seriesEntry {
	buf := al.candPool.Get().(*[]seriesEntry)
	*buf = (*buf)[:0]
	for _, e := range entries {
		if e.am.limit.Load()-e.am.inFlight.Load() > 0 {
			*buf = append(*buf, e)
		}
	}
	return buf
}

func (al *AdaptiveLimiter) putCandidates(buf *[]seriesEntry) {
	if buf == nil {
		return
	}
	*buf = (*buf)[:0]
	al.candPool.Put(buf)
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
		am.peakBefore429.Store(old)
		am.peakSetNano.Store(time.Now().UnixNano())
		newLim := old * 5 / 10
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
		return
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

	// Update RTT EWMA (lock-free CAS loop, alpha=0.3).
	for {
		old := am.rttEWMA.Load()
		var newVal int64
		if old == 0 {
			newVal = rttNano
		} else {
			newVal = old*7/10 + rttNano*3/10
		}
		if am.rttEWMA.CompareAndSwap(old, newVal) {
			break
		}
	}

	// Skip adaptive adjustment if model has a manual override.
	al.overrideMu.RLock()
	_, overridden := al.overrides[model]
	al.overrideMu.RUnlock()
	if overridden {
		// Still track RTT and success stats, but don't change the limit.
		return
	}

	// Cooldown: don't increase within 5 s of last 429.
	last429 := am.last429Nano.Load()
	if last429 > 0 && time.Now().UnixNano()-last429 < int64(5*time.Second) {
		return
	}

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
		bufferNano := minRTTNano / 10
		gradient := float64(minRTTNano+bufferNano) / float64(rttNano)
		gradient = math.Min(2.0, math.Max(0.8, gradient))
		additive := math.Max(1, math.Sqrt(float64(oldLimit)))
		newLimit = int64(gradient*float64(oldLimit) + additive)
	} else {
		newLimit = oldLimit + 1
	}

	if newLimit > am.maxLimit {
		newLimit = am.maxLimit
	}
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
		ewmaRTTMs := m.rttEWMA.Load() / int64(time.Millisecond)
		al.overrideMu.RLock()
		_, overridden := al.overrides[name]
		al.overrideMu.RUnlock()

		out = append(out, ModelStatus{
			Name:           name,
			InFlight:       m.inFlight.Load(),
			Limit:          m.limit.Load(),
			MaxLimit:       m.maxLimit,
			LearnedCeiling: m.peakBefore429.Load(),
			TotalReqs:      m.totalReqs.Load(),
			Total429s:      m.total429s.Load(),
			MinRTTMs:       minRTTMs,
			EwmaRTTMs:      ewmaRTTMs,
			Series:         m.series,
			Overridden:     overridden,
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

func (al *AdaptiveLimiter) RecordSeenModel(model string) {
	al.seenModels.Store(model, struct{}{})
}

func (al *AdaptiveLimiter) SeenModels() []string {
	names := make([]string, 0)
	al.seenModels.Range(func(key, _ any) bool {
		names = append(names, key.(string))
		return true
	})
	sort.Strings(names)
	return names
}

// --- internal helpers ---

// modelSeries extracts the major version from a model name.
// "glm-5.1" -> 5, "glm-4.7" -> 4, "glm-5-turbo" -> 5.
func modelSeries(name string) int {
	if len(name) < 5 || name[:4] != "glm-" {
		return 0
	}
	n := 0
	for i := 4; i < len(name); i++ {
		if name[i] >= '0' && name[i] <= '9' {
			n = n*10 + int(name[i]-'0')
		} else {
			break
		}
	}
	return n
}

// seriesLatencyPressure returns true when a majority of models in the given
// series show elevated latency (EWMA RTT > 1.2x minRTT).
func (al *AdaptiveLimiter) seriesLatencyPressure(series int) bool {
	var checked, pressured int
	for _, e := range al.seriesBuckets[series] {
		ewma := e.am.rttEWMA.Load()
		minRTT := e.am.minRTT.Load()
		if minRTT == 0 || ewma == 0 {
			continue
		}
		checked++
		if ewma > minRTT*12/10 {
			pressured++
		}
	}
	return checked > 0 && pressured >= (checked+1)/2
}

// seriesRecent429 returns true if any model in the series was rate-limited
// within the last 30 seconds, indicating upstream congestion.
func (al *AdaptiveLimiter) seriesRecent429(series int) bool {
	now := time.Now().UnixNano()
	for _, e := range al.seriesBuckets[series] {
		last := e.am.last429Nano.Load()
		if last > 0 && now-last < int64(30*time.Second) {
			return true
		}
	}
	return false
}

// shouldDistributeToLower returns true when same-series models are near capacity
// (>= 80% utilization) and a lower series has available headroom.
func (al *AdaptiveLimiter) shouldDistributeToLower(series int) bool {
	var totalLimit, totalInFlight int64
	for _, e := range al.seriesBuckets[series] {
		totalLimit += e.am.limit.Load()
		totalInFlight += e.am.inFlight.Load()
	}
	if totalLimit == 0 || totalInFlight < totalLimit*8/10 {
		return false
	}
	lower := al.seriesBuckets[series-1]
	for _, e := range lower {
		if e.am.limit.Load()-e.am.inFlight.Load() > 0 {
			return true
		}
	}
	return false
}

// tryCrossSeries routes a portion of traffic to a lower series when the
// current series is near capacity. Uses round-robin epoch to distribute
// roughly 1 in 5 requests to the lower series.
func (al *AdaptiveLimiter) tryCrossSeries(requestedModel string, model *adaptiveModel) (string, bool) {
	offset := al.rrEpoch.Add(1)
	if offset%5 != 0 {
		return "", false
	}
	lower := al.getCandidates(al.seriesBuckets[model.series-1])
	defer al.putCandidates(lower)
	if len(*lower) == 0 {
		return "", false
	}
	for i := range *lower {
		c := (*lower)[(int(offset)+i)%len(*lower)]
		if c.am.tryAcquire() {
			slog.Info("cross-series proactive distribution",
				"requested", requestedModel,
				"selected", c.name,
				"from_series", model.series,
				"to_series", c.am.series,
			)
			return c.name, true
		}
	}
	return "", false
}

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

// parseRetryAfter extracts the Retry-After header value as a duration.
func parseRetryAfter(headers http.Header) time.Duration {
	v := headers.Get("Retry-After")
	if v == "" {
		return 0
	}
	if sec, err := strconv.Atoi(v); err == nil {
		return time.Duration(sec) * time.Second
	}
	return 0
}
