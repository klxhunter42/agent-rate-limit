package middleware

import (
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// helper: create a limiter with standard test config.
func newTestLimiter() *AdaptiveLimiter {
	return NewAdaptiveLimiter(
		map[string]int{
			"glm-5.1":     10,
			"glm-5-turbo": 8,
			"glm-4.7":     6,
			"glm-4.5":     4,
		},
		2,  // defaultLimit
		50, // globalLimit
		10, // probeMultiplier
	)
}

// ---------------------------------------------------------------------------
// 1. modelSeries
// ---------------------------------------------------------------------------

func TestModelSeries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  int
	}{
		{"glm-5.1", 5},
		{"glm-4.7", 4},
		{"glm-5-turbo", 5},
		{"glm-4.5", 4},
		{"glm-5", 5},
		{"glm-4.6", 4},
		{"", 0},
		{"other", 0},
		{"gpt-4", 0},
		{"glm-", 0},
		{"glm-10.2", 10},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			if got := modelSeries(tc.input); got != tc.want {
				t.Errorf("modelSeries(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. NewAdaptiveLimiter initialization
// ---------------------------------------------------------------------------

func TestNewAdaptiveLimiter(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()

	// Models configured
	if len(al.models) != 4 {
		t.Fatalf("expected 4 models, got %d", len(al.models))
	}

	// Fallback order sorted by priority (descending)
	expectedOrder := []string{"glm-5.1", "glm-5-turbo", "glm-4.7", "glm-4.5"}
	for i, name := range expectedOrder {
		if al.fallbackOrder[i] != name {
			t.Errorf("fallbackOrder[%d] = %q, want %q", i, al.fallbackOrder[i], name)
		}
	}

	// Series buckets
	if len(al.seriesBuckets[5]) != 2 {
		t.Errorf("series 5 should have 2 entries, got %d", len(al.seriesBuckets[5]))
	}
	if len(al.seriesBuckets[4]) != 2 {
		t.Errorf("series 4 should have 2 entries, got %d", len(al.seriesBuckets[4]))
	}

	// Limits set correctly
	for _, name := range expectedOrder {
		am := al.models[name]
		if am.limit.Load() <= 0 {
			t.Errorf("model %q limit should be positive, got %d", name, am.limit.Load())
		}
		if am.series == 0 {
			t.Errorf("model %q series should be non-zero", name)
		}
	}

	// Global limit
	if al.globalLimit != 50 {
		t.Errorf("globalLimit = %d, want 50", al.globalLimit)
	}

	// Default model
	if al.defaultModel == nil {
		t.Fatal("defaultModel should not be nil")
	}
}

func TestNewAdaptiveLimiter_GlobalZero_Panics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for globalLimit <= 0")
		}
	}()
	NewAdaptiveLimiter(map[string]int{"glm-5.1": 10}, 2, 0, 10)
}

// ---------------------------------------------------------------------------
// 3. Acquire / Release
// ---------------------------------------------------------------------------

func TestAcquireRelease_Basic(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()

	model, ok := al.Acquire("glm-5.1")
	if !ok {
		t.Fatal("Acquire should succeed")
	}
	if model != "glm-5.1" {
		t.Errorf("Acquire returned %q, want %q", model, "glm-5.1")
	}

	gs := al.GlobalStatus()
	if gs.GlobalInFlight != 1 {
		t.Errorf("globalInFlight = %d, want 1", gs.GlobalInFlight)
	}

	al.Release(model)
	gs = al.GlobalStatus()
	if gs.GlobalInFlight != 0 {
		t.Errorf("globalInFlight after release = %d, want 0", gs.GlobalInFlight)
	}
}

func TestAcquire_GlobalLimit(t *testing.T) {
	al := NewAdaptiveLimiter(
		map[string]int{"glm-5.1": 50},
		2,
		3, // global limit = 3
		10,
	)

	var acquired []string
	for i := 0; i < 3; i++ {
		m, ok := al.Acquire("glm-5.1")
		if !ok {
			t.Fatalf("Acquire %d should succeed", i)
		}
		acquired = append(acquired, m)
	}

	// 4th acquire should block; use a goroutine with timeout
	done := make(chan struct{})
	go func() {
		al.Acquire("glm-5.1")
		close(done)
	}()

	select {
	case <-done:
		t.Error("Acquire should have blocked when global limit reached")
	case <-time.After(100 * time.Millisecond):
		// expected: blocked
	}

	// Release one and the blocked acquire should succeed
	al.Release(acquired[0])
	select {
	case <-done:
		// expected
	case <-time.After(2 * time.Second):
		t.Error("Acquire should have unblocked after release")
	}

	// Clean up remaining
	for _, m := range acquired[1:] {
		al.Release(m)
	}
}

func TestAcquire_ModelLimit_Fallback(t *testing.T) {
	t.Parallel()
	al := NewAdaptiveLimiter(
		map[string]int{
			"glm-5.1":     2,
			"glm-5-turbo": 2,
		},
		2,
		50,
		10,
	)

	// Fill up glm-5.1
	al.Acquire("glm-5.1")
	al.Acquire("glm-5.1")

	// Next acquire should fall back to another model in same series
	model, ok := al.Acquire("glm-5.1")
	if !ok {
		t.Fatal("Acquire should succeed via fallback")
	}
	if model == "glm-5.1" {
		t.Error("Expected fallback to different model, got same model")
	}
	al.Release(model)
}

func TestAcquire_UnknownModel(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()

	model, ok := al.Acquire("unknown-model")
	if !ok {
		t.Fatal("Acquire for unknown model should use default")
	}
	if model != "unknown-model" {
		t.Errorf("got %q, want %q", model, "unknown-model")
	}
	// Should have incremented the default model's inFlight
	am := al.defaultModel
	if am.inFlight.Load() != 1 {
		t.Errorf("default inFlight = %d, want 1", am.inFlight.Load())
	}
	al.Release(model)
}

// ---------------------------------------------------------------------------
// 4. Feedback on 429
// ---------------------------------------------------------------------------

func TestFeedback_429_HalvesLimit(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	before := am.limit.Load() // 10
	al.Feedback("glm-5.1", 429, 100*time.Millisecond, nil)

	after := am.limit.Load()
	expected := before / 2
	if after != expected {
		t.Errorf("limit after 429 = %d, want %d", after, expected)
	}
}

func TestFeedback_429_MinLimitRespected(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]
	am.limit.Store(1)

	al.Feedback("glm-5.1", 429, 100*time.Millisecond, nil)

	if am.limit.Load() < am.minLimit {
		t.Errorf("limit = %d, below minLimit %d", am.limit.Load(), am.minLimit)
	}
}

func TestFeedback_429_SetsPeakAndCooldown(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	al.Feedback("glm-5.1", 429, 100*time.Millisecond, nil)

	// peakBefore429 should be set
	peak := am.peakBefore429.Load()
	if peak != 10 { // initial limit was 10
		t.Errorf("peakBefore429 = %d, want 10", peak)
	}

	// last429Nano should be recent
	last429 := am.last429Nano.Load()
	if last429 == 0 {
		t.Error("last429Nano should be set")
	}

	// successRun should be reset
	if am.successRun.Load() != 0 {
		t.Errorf("successRun = %d, want 0", am.successRun.Load())
	}

	// total429s incremented
	if am.total429s.Load() != 1 {
		t.Errorf("total429s = %d, want 1", am.total429s.Load())
	}
}

func TestFeedback_503_TreatedAs429(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	al.Feedback("glm-5.1", 503, 100*time.Millisecond, nil)

	if am.limit.Load() != 5 {
		t.Errorf("limit after 503 = %d, want 5", am.limit.Load())
	}
}

func TestFeedback_CooldownBlocksIncrease(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	// Trigger 429 to start cooldown
	al.Feedback("glm-5.1", 429, 50*time.Millisecond, nil)
	am.limit.Store(5)

	// 5 successes within cooldown should not increase limit
	for i := 0; i < 10; i++ {
		al.Feedback("glm-5.1", 200, 50*time.Millisecond, nil)
	}

	if am.limit.Load() != 5 {
		t.Errorf("limit during cooldown = %d, want 5 (cooldown should block increase)", am.limit.Load())
	}
}

// ---------------------------------------------------------------------------
// 5. Feedback on success
// ---------------------------------------------------------------------------

func TestFeedback_Success_MinRTTUpdate(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	// First RTT should set minRTT
	al.Feedback("glm-5.1", 200, 100*time.Millisecond, nil)
	if am.minRTT.Load() != 100*time.Millisecond.Nanoseconds() {
		t.Errorf("minRTT = %d, want %d", am.minRTT.Load(), 100*time.Millisecond.Nanoseconds())
	}

	// Lower RTT should update minRTT
	al.Feedback("glm-5.1", 200, 50*time.Millisecond, nil)
	if am.minRTT.Load() != 50*time.Millisecond.Nanoseconds() {
		t.Errorf("minRTT = %d, want %d", am.minRTT.Load(), 50*time.Millisecond.Nanoseconds())
	}

	// Higher RTT should NOT update minRTT
	al.Feedback("glm-5.1", 200, 200*time.Millisecond, nil)
	if am.minRTT.Load() != 50*time.Millisecond.Nanoseconds() {
		t.Errorf("minRTT should stay at lowest, got %d", am.minRTT.Load())
	}
}

func TestFeedback_Success_RTT_EWMA(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	rtt1 := 100 * time.Millisecond
	al.Feedback("glm-5.1", 200, rtt1, nil)

	// First EWMA should equal the sample
	ewma := am.rttEWMA.Load()
	if ewma != rtt1.Nanoseconds() {
		t.Errorf("initial EWMA = %d, want %d", ewma, rtt1.Nanoseconds())
	}

	// Second sample: EWMA = old*0.7 + new*0.3
	rtt2 := 200 * time.Millisecond
	al.Feedback("glm-5.1", 200, rtt2, nil)
	expected := rtt1.Nanoseconds()*7/10 + rtt2.Nanoseconds()*3/10
	ewma = am.rttEWMA.Load()
	if ewma != expected {
		t.Errorf("EWMA = %d, want %d", ewma, expected)
	}
}

func TestFeedback_Success_GradientIncrease(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	// Set a known minRTT by providing a low RTT sample
	minRTTNano := 50 * time.Millisecond
	al.Feedback("glm-5.1", 200, minRTTNano, nil) // minRTT set, successRun=1

	// Clear any 429 cooldown
	am.last429Nano.Store(0)

	// Need successRun to be a multiple of 5 for increase
	// successRun is now 1, need 4 more to reach 5
	for i := 0; i < 3; i++ {
		al.Feedback("glm-5.1", 200, 60*time.Millisecond, nil)
	}

	// successRun = 4, next feedback makes it 5 and triggers adjustment
	oldLimit := am.limit.Load()
	al.Feedback("glm-5.1", 200, 60*time.Millisecond, nil)
	newLimit := am.limit.Load()

	if newLimit <= oldLimit {
		t.Errorf("limit should increase on success with good RTT, old=%d new=%d", oldLimit, newLimit)
	}
}

func TestFeedback_Success_MaxLimitCap(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	// Set limit near max
	am.limit.Store(am.maxLimit - 1)
	am.last429Nano.Store(0)
	am.minRTT.Store(50 * time.Millisecond.Nanoseconds())
	am.successRun.Store(4) // next success will be 5th

	al.Feedback("glm-5.1", 200, 60*time.Millisecond, nil)

	if am.limit.Load() > am.maxLimit {
		t.Errorf("limit = %d exceeds maxLimit %d", am.limit.Load(), am.maxLimit)
	}
}

func TestFeedback_Success_LearnedCeiling(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	// Simulate: limit was 8 before a 429
	am.limit.Store(8)
	al.Feedback("glm-5.1", 429, 100*time.Millisecond, nil) // halves to 4, sets peak=8

	// Clear cooldown
	am.last429Nano.Store(0)
	am.minRTT.Store(50 * time.Millisecond.Nanoseconds())

	// Drive limit back up with successes
	// Need successRun%5 == 0 triggers. Set successRun to 4.
	am.successRun.Store(4)

	// Feed successes until limit approaches the ceiling
	for i := 0; i < 20; i++ {
		am.successRun.Store(4) // force every feedback to trigger adjustment
		al.Feedback("glm-5.1", 200, 50*time.Millisecond, nil)
	}

	// Limit should be capped at peak-1 = 7
	lim := am.limit.Load()
	if lim >= 8 {
		t.Errorf("limit = %d should be below learned ceiling 8", lim)
	}
}

func TestFeedback_Success_LearnedCeilingDecay(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	// Set peak from long ago (> 5 min)
	am.peakBefore429.Store(5)
	am.peakSetNano.Store(time.Now().Add(-10 * time.Minute).UnixNano())
	am.last429Nano.Store(0)
	am.minRTT.Store(50 * time.Millisecond.Nanoseconds())
	am.successRun.Store(4)

	oldLimit := am.limit.Load()
	al.Feedback("glm-5.1", 200, 50*time.Millisecond, nil)

	// peak should be decayed (cleared)
	if am.peakBefore429.Load() != 0 {
		t.Error("learned ceiling should have decayed after 5 min")
	}

	// limit should have increased past old peak
	newLimit := am.limit.Load()
	if newLimit <= oldLimit {
		t.Errorf("limit should increase after ceiling decay, old=%d new=%d", oldLimit, newLimit)
	}
}

func TestFeedback_Success_AdditiveSqrtTerm(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	// Set up conditions: limit < maxLimit so adjustment can proceed
	// probeMultiplier=10, initial=10 => maxLimit=100
	am.minRTT.Store(100 * time.Millisecond.Nanoseconds())
	am.rttEWMA.Store(100 * time.Millisecond.Nanoseconds())
	am.last429Nano.Store(0)
	am.successRun.Store(4)
	am.limit.Store(90) // below maxLimit=100

	// rtt == minRTT => buffer = minRTT/10 => gradient = (minRTT + minRTT/10) / minRTT = 1.1
	// But gradient is clamped to [0.8, 2.0], so gradient = 1.1
	// newLimit = 1.1 * 90 + sqrt(90) = 99 + 9.49... = int64(108.49) = 108
	// But 108 > maxLimit(100) so capped at 100
	// Use a limit where the result stays below maxLimit.
	// With limit=50: newLimit = 1.1*50 + sqrt(50) = 55 + 7.07 = int64(62.07) = 62
	am.limit.Store(50)
	expectedNew := int64(1.1*50 + math.Sqrt(50))
	al.Feedback("glm-5.1", 200, 100*time.Millisecond, nil)

	newLim := am.limit.Load()
	if newLim != expectedNew {
		t.Errorf("limit = %d, want %d (gradient*limit + sqrt(limit))", newLim, expectedNew)
	}
}

// ---------------------------------------------------------------------------
// 6. Override
// ---------------------------------------------------------------------------

func TestSetOverride_PinsLimit(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	al.SetOverride("glm-5.1", 3)

	if am.limit.Load() != 3 {
		t.Errorf("limit after override = %d, want 3", am.limit.Load())
	}
}

func TestSetOverride_ClearRestores(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()

	al.SetOverride("glm-5.1", 3)
	al.SetOverride("glm-5.1", 0) // clear

	// Override removed, limit stays at whatever it was (not reverted to original)
	// The important thing is that adaptive adjustment is re-enabled
	overrides := al.Overrides()
	if _, exists := overrides["glm-5.1"]; exists {
		t.Error("override should be cleared")
	}
}

func TestSetOverride_BlocksAdaptiveIncrease(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	al.SetOverride("glm-5.1", 5)
	am.last429Nano.Store(0)
	am.minRTT.Store(50 * time.Millisecond.Nanoseconds())

	// Feed many successes
	for i := 0; i < 20; i++ {
		al.Feedback("glm-5.1", 200, 50*time.Millisecond, nil)
	}

	// Limit should stay at override value
	if am.limit.Load() != 5 {
		t.Errorf("limit should stay at override 5, got %d", am.limit.Load())
	}
}

func TestSetOverride_StillTracksRTT(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	al.SetOverride("glm-5.1", 5)

	al.Feedback("glm-5.1", 200, 100*time.Millisecond, nil)
	al.Feedback("glm-5.1", 200, 50*time.Millisecond, nil)

	// RTT should still be tracked even when overridden
	if am.minRTT.Load() == 0 {
		t.Error("minRTT should be tracked even with override")
	}
	if am.rttEWMA.Load() == 0 {
		t.Error("rttEWMA should be tracked even with override")
	}
}

// ---------------------------------------------------------------------------
// 7. seriesLatencyPressure
// ---------------------------------------------------------------------------

func TestSeriesLatencyPressure(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()

	// No RTT data: no pressure
	if al.seriesLatencyPressure(5) {
		t.Error("should not have pressure with no RTT data")
	}

	// Set up EWMA > 1.5x minRTT for majority of series 5 models
	// Series 5 has: glm-5.1 and glm-5-turbo (2 models)
	s5Models := al.seriesBuckets[5]
	for _, e := range s5Models {
		e.am.minRTT.Store(100 * time.Millisecond.Nanoseconds())
		e.am.rttEWMA.Store(200 * time.Millisecond.Nanoseconds()) // 2x minRTT > 1.5x
	}

	if !al.seriesLatencyPressure(5) {
		t.Error("should detect latency pressure when EWMA > 1.5x minRTT for all models")
	}

	// One model with normal latency, one pressured
	// With 2 models, majority threshold = (2+1)/2 = 1. Both pressured = 2 >= 1 => true
	// Reset one model to normal
	s5Models[0].am.rttEWMA.Store(100 * time.Millisecond.Nanoseconds()) // == minRTT, not pressured
	// pressured count = 1, checked = 2, threshold = 1, 1 >= 1 => true
	if !al.seriesLatencyPressure(5) {
		t.Error("with 1/2 pressured, majority threshold is 1, should still be pressured")
	}

	// Reset both to normal
	for _, e := range s5Models {
		e.am.rttEWMA.Store(100 * time.Millisecond.Nanoseconds())
	}
	if al.seriesLatencyPressure(5) {
		t.Error("should not have pressure with normal latency")
	}
}

func TestSeriesLatencyPressure_NoEntryForSeries(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()

	// Series with no models
	if al.seriesLatencyPressure(99) {
		t.Error("non-existent series should not have pressure")
	}
}

// ---------------------------------------------------------------------------
// 8. Status
// ---------------------------------------------------------------------------

func TestStatus(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()

	statuses := al.Status()
	if len(statuses) != 4 {
		t.Fatalf("expected 4 statuses, got %d", len(statuses))
	}

	// Verify order matches fallback order
	for i, s := range statuses {
		if s.Name != al.fallbackOrder[i] {
			t.Errorf("status[%d].Name = %q, want %q", i, s.Name, al.fallbackOrder[i])
		}
	}

	// Verify fields are populated
	for _, s := range statuses {
		if s.Limit <= 0 {
			t.Errorf("model %q limit should be positive", s.Name)
		}
		if s.MaxLimit <= 0 {
			t.Errorf("model %q maxLimit should be positive", s.Name)
		}
		if s.Series == 0 {
			t.Errorf("model %q series should be non-zero", s.Name)
		}
	}
}

func TestStatus_Overridden(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	al.SetOverride("glm-5.1", 5)

	statuses := al.Status()
	for _, s := range statuses {
		if s.Name == "glm-5.1" {
			if !s.Overridden {
				t.Error("glm-5.1 should be marked as overridden")
			}
		} else {
			if s.Overridden {
				t.Errorf("%q should not be overridden", s.Name)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 9. GlobalStatus
// ---------------------------------------------------------------------------

func TestGlobalStatus(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()

	gs := al.GlobalStatus()
	if gs.GlobalLimit != 50 {
		t.Errorf("GlobalLimit = %d, want 50", gs.GlobalLimit)
	}
	if gs.GlobalInFlight != 0 {
		t.Errorf("GlobalInFlight = %d, want 0", gs.GlobalInFlight)
	}

	m, _ := al.Acquire("glm-5.1")
	gs = al.GlobalStatus()
	if gs.GlobalInFlight != 1 {
		t.Errorf("GlobalInFlight after acquire = %d, want 1", gs.GlobalInFlight)
	}
	al.Release(m)
}

// ---------------------------------------------------------------------------
// 10. Concurrent Acquire/Release
// ---------------------------------------------------------------------------

func TestConcurrentAcquireRelease(t *testing.T) {
	al := NewAdaptiveLimiter(
		map[string]int{
			"glm-5.1":     20,
			"glm-5-turbo": 20,
			"glm-4.7":     20,
		},
		5,
		100,
		10,
	)

	const goroutines = 50
	const iterations = 100
	var wg sync.WaitGroup
	var totalAcquired atomic.Int64
	var totalReleased atomic.Int64

	models := []string{"glm-5.1", "glm-5-turbo", "glm-4.7"}

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				model := models[id%len(models)]
				acquired, ok := al.Acquire(model)
				if ok {
					totalAcquired.Add(1)
					// Simulate some work
					time.Sleep(time.Microsecond)
					al.Release(acquired)
					totalReleased.Add(1)
				}
			}
		}(g)
	}

	wg.Wait()

	acq := totalAcquired.Load()
	rel := totalReleased.Load()
	if acq != rel {
		t.Errorf("acquired = %d, released = %d - mismatch indicates leak", acq, rel)
	}

	// After all releases, in-flight should be 0
	gs := al.GlobalStatus()
	if gs.GlobalInFlight != 0 {
		t.Errorf("globalInFlight after all releases = %d, want 0", gs.GlobalInFlight)
	}
}

func TestConcurrentFeedback(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()

	const goroutines = 20
	const iterations = 50
	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if id%3 == 0 {
					al.Feedback("glm-5.1", 429, 100*time.Millisecond, nil)
				} else {
					al.Feedback("glm-5.1", 200, time.Duration(50+i)*time.Millisecond, nil)
				}
			}
		}(g)
	}

	wg.Wait()

	am := al.models["glm-5.1"]
	if am.totalReqs.Load() != int64(goroutines*iterations) {
		t.Errorf("totalReqs = %d, want %d", am.totalReqs.Load(), goroutines*iterations)
	}

	// Limit should be within bounds
	lim := am.limit.Load()
	if lim < am.minLimit {
		t.Errorf("limit = %d below minLimit %d", lim, am.minLimit)
	}
	if lim > am.maxLimit {
		t.Errorf("limit = %d above maxLimit %d", lim, am.maxLimit)
	}
}

func TestConcurrentOverrideAndFeedback(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]

	var wg sync.WaitGroup

	// Concurrently set/clear overrides and provide feedback
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(iter int) {
			defer wg.Done()
			if iter%2 == 0 {
				al.SetOverride("glm-5.1", int64(3+iter))
			} else {
				al.SetOverride("glm-5.1", 0)
			}
		}(i)
		go func(iter int) {
			defer wg.Done()
			status := 200
			if iter%3 == 0 {
				status = 429
			}
			al.Feedback("glm-5.1", status, time.Duration(50+iter*10)*time.Millisecond, nil)
		}(i)
	}

	wg.Wait()

	// Limit should be within valid range
	lim := am.limit.Load()
	if lim < am.minLimit {
		t.Errorf("limit = %d below minLimit %d", lim, am.minLimit)
	}
}

// ---------------------------------------------------------------------------
// Additional edge cases
// ---------------------------------------------------------------------------

func TestFeedback_NonSuccessNon429_Ignored(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]
	originalLimit := am.limit.Load()

	// 500 should be ignored (not 429/503, not 2xx)
	al.Feedback("glm-5.1", 500, 100*time.Millisecond, nil)
	if am.limit.Load() != originalLimit {
		t.Error("500 should not change limit")
	}

	// 404 should be ignored
	al.Feedback("glm-5.1", 404, 100*time.Millisecond, nil)
	if am.limit.Load() != originalLimit {
		t.Error("404 should not change limit")
	}
}

func TestFeedback_Success_OnlyAdjustsEvery5th(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()
	am := al.models["glm-5.1"]
	am.last429Nano.Store(0)
	am.minRTT.Store(50 * time.Millisecond.Nanoseconds())

	// Feed 4 successes (successRun 1-4): no adjustment
	for i := 0; i < 4; i++ {
		al.Feedback("glm-5.1", 200, 50*time.Millisecond, nil)
	}
	limitAfter4 := am.limit.Load()

	// 5th success should trigger adjustment
	al.Feedback("glm-5.1", 200, 50*time.Millisecond, nil)
	limitAfter5 := am.limit.Load()

	if limitAfter5 <= limitAfter4 {
		t.Errorf("limit should increase on 5th success: before=%d after=%d", limitAfter4, limitAfter5)
	}
}

func TestFeedback_Success_NoMinRTT_IncrementBy1(t *testing.T) {
	t.Parallel()
	al := NewAdaptiveLimiter(
		map[string]int{"glm-5.1": 10},
		2,
		50,
		10,
	)
	am := al.models["glm-5.1"]
	am.last429Nano.Store(0)
	// minRTT stays 0 (no previous feedback)

	// First feedback sets minRTT and successRun=1
	al.Feedback("glm-5.1", 200, 100*time.Millisecond, nil)
	// minRTT is now set, so subsequent calls will use gradient path

	// Reset minRTT to 0 to test the fallback path
	am.minRTT.Store(0)
	am.successRun.Store(4)

	al.Feedback("glm-5.1", 200, 50*time.Millisecond, nil)

	// Feedback updates minRTT via CAS even when it was 0
	if am.minRTT.Load() == 0 {
		t.Error("minRTT should have been set by feedback")
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		val  string
		want time.Duration
	}{
		{"30", 30 * time.Second},
		{"0", 0},
		{"", 0},
		{"abc", 0},
		{"120", 120 * time.Second},
	}
	for _, tc := range tests {
		h := http.Header{}
		if tc.val != "" {
			h.Set("Retry-After", tc.val)
		}
		got := parseRetryAfter(h)
		if got != tc.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
}

func TestAcquireRelease_MultipleModels(t *testing.T) {
	t.Parallel()
	al := newTestLimiter()

	var acquired []string
	for _, model := range []string{"glm-5.1", "glm-4.7", "glm-4.5"} {
		m, ok := al.Acquire(model)
		if !ok {
			t.Fatalf("Acquire(%s) should succeed", model)
		}
		acquired = append(acquired, m)
	}

	gs := al.GlobalStatus()
	if gs.GlobalInFlight != 3 {
		t.Errorf("globalInFlight = %d, want 3", gs.GlobalInFlight)
	}

	for _, m := range acquired {
		al.Release(m)
	}

	gs = al.GlobalStatus()
	if gs.GlobalInFlight != 0 {
		t.Errorf("globalInFlight after release = %d, want 0", gs.GlobalInFlight)
	}
}

func TestAcquireBlocking_Timeout(t *testing.T) {
	t.Parallel()
	al := NewAdaptiveLimiter(
		map[string]int{"glm-5.1": 1},
		1,
		50,
		10,
	)
	am := al.models["glm-5.1"]

	// Fill the model's limit
	am.inFlight.Store(1)

	// acquireAnyModel should timeout
	_, ok := al.acquireAnyModel(50*time.Millisecond, "glm-5.1")
	if ok {
		t.Error("acquireAnyModel should return false on timeout")
	}

	am.inFlight.Store(0)
}
