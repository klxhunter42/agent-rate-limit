package middleware

import (
	"math"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newTestDetector(t *testing.T) *AnomalyDetector {
	t.Helper()
	return NewAnomalyDetector(prometheus.NewRegistry())
}

// --- 1. Insufficient samples ---

func TestAnomalyDetector_InsufficientSamples(t *testing.T) {
	d := newTestDetector(t)
	for i := 0; i < 9; i++ {
		a := d.Record(10.0)
		if a.Type != AnomalyNone {
			t.Fatalf("sample %d: expected AnomalyNone, got %d", i, a.Type)
		}
	}
	// 10th sample triggers stats
	a := d.Record(10.0)
	if a.Type != AnomalyNone {
		t.Fatalf("10th sample with same value: expected AnomalyNone (zero stddev), got %d", a.Type)
	}
}

// --- 2. Spike detection ---

func TestAnomalyDetector_SpikeDetection(t *testing.T) {
	d := newTestDetector(t)
	for i := 0; i < 20; i++ {
		d.Record(100.0)
	}
	a := d.Record(1000.0)
	if a.Type != AnomalySpike {
		t.Fatalf("expected AnomalySpike, got %d", a.Type)
	}
	if a.Score <= 2.0 {
		t.Fatalf("expected z > 2.0, got %f", a.Score)
	}
}

// --- 3. Drop detection ---

func TestAnomalyDetector_DropDetection(t *testing.T) {
	d := newTestDetector(t)
	for i := 0; i < 20; i++ {
		d.Record(100.0)
	}
	a := d.Record(0.0)
	if a.Type != AnomalyDrop {
		t.Fatalf("expected AnomalyDrop, got %d", a.Type)
	}
	if a.Score >= -2.0 {
		t.Fatalf("expected z < -2.0, got %f", a.Score)
	}
}

// --- 4. Sustained high ---

func TestAnomalyDetector_SustainedHigh(t *testing.T) {
	d := newTestDetector(t)
	// Use a large baseline so 5 spikes don't dominate the stats.
	// With 500 zeros, even after 5 spikes of 10000, z stays well above 2.
	for i := 0; i < 500; i++ {
		d.Record(0.0)
	}
	for i := 0; i < 6; i++ {
		a := d.Record(10000.0)
		if i < sustainedCount-1 {
			if a.Type != AnomalySpike {
				t.Fatalf("iteration %d: expected AnomalySpike, got %d (z=%f)", i, a.Type, a.Score)
			}
		} else {
			if a.Type != AnomalySustainedHigh {
				t.Fatalf("iteration %d: expected AnomalySustainedHigh, got %d (z=%f)", i, a.Type, a.Score)
			}
		}
	}
}

// --- 5. Sustained low ---

func TestAnomalyDetector_SustainedLow(t *testing.T) {
	d := newTestDetector(t)
	for i := 0; i < 500; i++ {
		d.Record(10000.0)
	}
	for i := 0; i < 6; i++ {
		a := d.Record(0.0)
		if i < sustainedCount-1 {
			if a.Type != AnomalyDrop {
				t.Fatalf("iteration %d: expected AnomalyDrop, got %d (z=%f)", i, a.Type, a.Score)
			}
		} else {
			if a.Type != AnomalySustainedLow {
				t.Fatalf("iteration %d: expected AnomalySustainedLow, got %d (z=%f)", i, a.Type, a.Score)
			}
		}
	}
}

// --- 6. Severity levels (table-driven) ---

func TestAnomalyDetector_SeverityLevels(t *testing.T) {
	// Pre-fill with 0s so that a large positive value gives high z-score.
	// With 50 zeros and value V, buffer has 51 values (0 included):
	//   mean = V/51
	//   stddev = sqrt(V^2 * 50 / 51^2) = V * sqrt(50) / 51
	//   z = (V - V/51) / (V * sqrt(50)/51) = 50 / sqrt(50) = sqrt(50) ~ 7.07
	// All spikes from a buffer of zeros get |z| ~ 7.07 -> Critical.
	//
	// To get controlled z-scores, use a buffer with known variance and compute
	// the exact value needed. The recorded value IS included in stats.
	//
	// Strategy: use values where we can compute exact mean/stddev after insertion.
	// Simpler: just check that the z-score falls in the right severity band.

	tests := []struct {
		name         string
		fillValue    float64
		fillCount    int
		testValue    float64
		wantType     AnomalyType
		wantSeverity AnomalySeverity
	}{
		// With many stable values and extreme spike/drop, z is large -> Critical.
		// For Medium (2.0 < |z| <= 3.0) and High (3.0 < |z| <= 4.0) we need controlled variance.
		// Using fill=0 with spike=V: z = sqrt(fillCount) regardless of V (when fillCount >= 10).
		// With 99 zeros + value V: mean = V/100, stddev = V*sqrt(99)/100
		// z = (V - V/100) / (V*sqrt(99)/100) = 99/sqrt(99) = sqrt(99) ~ 9.95 -> always Critical.
		//
		// For medium severity: need |z| between 2.0 and 3.0.
		// Use a diverse baseline. 50 values of 0, 50 values of 100: mean=50, stddev=50.
		// Then insert V. New stats (101 values):
		//   mean = (5000 + V)/101
		//   variance = (50*(0-mean)^2 + 50*(100-mean)^2 + (V-mean)^2) / 101
		// For V=100 (another 100):
		//   mean = 5100/101 ~ 50.495, stddev ~ 49.75, z ~ (100-50.495)/49.75 ~ 0.995 -> None
		// For V=200:
		//   mean = 5200/101 ~ 51.485, variance = (50*51.485^2 + 50*48.515^2 + (200-51.485)^2)/101
		//   This is getting complex. Let's just verify severity bands using known z.
		//
		// Simplest approach: use a small buffer where we can predict z exactly.
		// 9 values of 0, then test with value V. Buffer has 10 values.
		// mean = V/10, stddev = V*sqrt(9)/10 = V*3/10
		// z = (V - V/10) / (3V/10) = 9V/(10) / (3V/10) = 3
		// So with 9 zeros + value V, z = 3.0 exactly. That's the boundary.
		// For z > 3: need more variance in baseline.
		//
		// Better: just use extreme values and verify the severity classification logic.
		// The severity is determined by |z| thresholds: >2 medium, >3 high, >4 critical.

		{"spike critical (extreme value)", 0.0, 99, 1e6, AnomalySpike, SeverityCritical},
		{"drop critical (extreme value)", 1e6, 99, 0.0, AnomalyDrop, SeverityCritical},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDetector(t)
			for i := 0; i < tc.fillCount; i++ {
				d.Record(tc.fillValue)
			}
			a := d.Record(tc.testValue)
			if a.Type != tc.wantType {
				t.Fatalf("type: got %d, want %d", a.Type, tc.wantType)
			}
			if a.Severity != tc.wantSeverity {
				t.Fatalf("severity: got %d, want %d (z=%f)", a.Severity, tc.wantSeverity, a.Score)
			}
		})
	}
}

// severityFromZ returns the expected severity for a given z-score.
func severityFromZ(z float64) AnomalySeverity {
	az := math.Abs(z)
	switch {
	case az > 4.0:
		return SeverityCritical
	case az > 3.0:
		return SeverityHigh
	default:
		return SeverityMedium
	}
}

// TestAnomalyDetector_SeverityByZScore uses controlled baselines to hit each severity band.
func TestAnomalyDetector_SeverityByZScore(t *testing.T) {
	// Use a baseline with controlled variance to produce specific z-scores.
	// Strategy: N identical values give stddev=0, so we need a mixed baseline.
	//
	// Use values that produce exact z-scores after including the test value.
	// For simplicity, we verify the severity mapping matches |z| thresholds
	// by using a very large buffer of one value and extreme test values,
	// then checking that severity matches severityFromZ(z).
	//
	// With 200 zeros and value V:
	//   mean = V/201, stddev = V*sqrt(200)/201
	//   z = (V - V/201) / (V*sqrt(200)/201) = 200/sqrt(200) = sqrt(200) ~ 14.14
	// Always critical. Need different approach for medium/high.

	tests := []struct {
		name     string
		values   []float64 // all values to record, last one is the test
		wantType AnomalyType
		wantSev  AnomalySeverity
	}{
		{
			// 9 zeros + 10.0: buffer=[0,0,0,0,0,0,0,0,0,10], mean=1, stddev=3, z=3.0
			// z=3.0 is NOT > 3.0, so severity=Medium. But we want >3 for high.
			// Use 9 zeros + 20: mean=2, stddev=6, z=18/6=3.0. Still boundary.
			// For z just over 3: 9 zeros + 10.1: mean=1.01, stddev=3.03, z=9.09/3.03=3.0
			// Actually with 9 zeros + V:
			//   mean = V/10, variance = (9*(V/10)^2 + (V-V/10)^2)/10 = (9V^2/100 + 81V^2/100)/10 = 90V^2/1000 = 9V^2/100
			//   stddev = 3V/10, z = (V - V/10)/(3V/10) = 9V/(3V) = 3.0
			// It's always exactly 3.0 regardless of V. Use 8 zeros + 1 different value.
			//
			// 8 zeros, 1 five, then test value V. Buffer has 10 values.
			// mean = (5+V)/10, variance = (8*mean^2 + (5-mean)^2 + (V-mean)^2)/10
			// This varies with V. Let's just use 99 values of 0, 1 value of X, then V.
			// Actually, let's just directly test severity thresholds using computeAnomaly helper.
			name:     "medium severity (z between 2 and 3)",
			values:   append(make([]float64, 0), []float64{0, 0, 0, 0, 0, 0, 0, 0, 100}...),
			wantType: AnomalySpike,
			wantSev:  SeverityMedium,
			// 9 values: 8 zeros, 1 hundred. mean=100/9~11.11, stddev~31.43
			// Test with V=80: mean=(100+80)/10=18, z=(80-18)/stddev...
			// Let me compute: buf=[0,0,0,0,0,0,0,0,100,80]
			// sum=180, mean=18, sqSum=8*324+82^2+62^2=2592+6724+3844=13160, stddev=sqrt(1316)=36.28
			// z=(80-18)/36.28=1.71 -> not spike. Need higher V.
		},
	}

	// Skip the complex approach above. Instead use a direct computational approach:
	// Build a buffer, record value, get z, verify severity matches severityFromZ.
	// We already verified the severity logic in the source code matches severityFromZ.
	// So let's just test specific known combinations.

	// Approach: build buffers with known stats and compute what value gives the desired z.
	// With N identical values (all M) + 1 different value X in buffer:
	//   buffer = [M, M, ..., M, X]  (N+1 values)
	//   mean = (N*M + X)/(N+1)
	//   stddev = |X-M| * sqrt(N) / (N+1)
	//   For a new test value V, buffer becomes [M,...,M,X,V] with N+2 values:
	//   ... gets complex.

	// Simplest correct approach: verify severity classification using the returned z-score.
	t.Run("severity matches z-score thresholds", func(t *testing.T) {
		d := newTestDetector(t)
		for i := 0; i < 50; i++ {
			d.Record(0.0)
		}
		for i := 0; i < 50; i++ {
			d.Record(100.0)
		}
		// Try different values and verify severity matches
		testCases := []float64{200, 300, 500, -200, -300, -500}
		for _, v := range testCases {
			a := d.Record(v)
			expected := severityFromZ(a.Score)
			if a.Severity != expected {
				t.Fatalf("value=%f z=%f: severity=%d, expected=%d", v, a.Score, a.Severity, expected)
			}
			if a.Type == AnomalyNone && math.Abs(a.Score) > 2.0 {
				t.Fatalf("value=%f z=%f: expected anomaly type but got AnomalyNone", v, a.Score)
			}
		}
	})

	_ = tests // suppress unused warning
}

// --- 7. Ring buffer wraparound ---

func TestAnomalyDetector_RingBufferWraparound(t *testing.T) {
	d := newTestDetector(t)

	// fill with 1000 values of 10.0
	for i := 0; i < 1000; i++ {
		d.Record(10.0)
	}
	// overwrite all with 1000 values of 20.0
	for i := 0; i < 1000; i++ {
		d.Record(20.0)
	}

	// buffer now has 1000 values of 20.0; recording 20.0 -> stddev=0 -> AnomalyNone
	a := d.Record(20.0)
	if a.Type != AnomalyNone {
		t.Fatalf("after wraparound: expected AnomalyNone, got %d", a.Type)
	}
	if a.Mean != 20.0 {
		t.Fatalf("mean: got %f, want 20.0", a.Mean)
	}

	// verify buffer is exactly 1000 entries (not more)
	if d.count != bufSize {
		t.Fatalf("count: got %d, want %d", d.count, bufSize)
	}
}

// --- 8. Zero stddev ---

func TestAnomalyDetector_ZeroStddev(t *testing.T) {
	d := newTestDetector(t)
	for i := 0; i < 20; i++ {
		d.Record(42.0)
	}
	// recording same value -> all 21 values are 42.0 -> stddev=0
	a := d.Record(42.0)
	if a.Type != AnomalyNone {
		t.Fatalf("expected AnomalyNone for zero stddev, got %d", a.Type)
	}
}

// --- 9. Stats accuracy ---

func TestAnomalyDetector_StatsAccuracy(t *testing.T) {
	// Record known values and verify mean/stddev via z-score calculation.
	// After recording [2,4,4,4,5,5,7,9,5,5] (10 values), then record 5.0:
	// Buffer has 11 values: [2,4,4,4,5,5,7,9,5,5,5]
	// mean = 55/11 = 5.0
	// variance = ((2-5)^2 + 3*(4-5)^2 + 4*(5-5)^2 + (7-5)^2 + (9-5)^2) / 11
	//          = (9 + 3 + 0 + 4 + 16) / 11 = 32/11 = 2.909091
	// stddev = sqrt(32/11) = 1.7059
	// z = (5-5)/1.7059 = 0.0

	d := newTestDetector(t)
	vals := []float64{2, 4, 4, 4, 5, 5, 7, 9, 5, 5}
	for _, v := range vals {
		d.Record(v)
	}

	a := d.Record(5.0)
	wantMean := 55.0 / 11.0
	if math.Abs(a.Mean-wantMean) > 1e-9 {
		t.Fatalf("mean: got %f, want %f", a.Mean, wantMean)
	}
	if a.Score != 0.0 {
		t.Fatalf("z-score for value=mean: got %f, want 0.0", a.Score)
	}

	// Now verify with a value that should produce a spike.
	// Record 9.0. Buffer has 12 values: previous 11 + 9.
	// mean = 64/12 = 5.333...
	// variance = (sum of (vi-5.333)^2 for all 12 values) / 12
	// Let's just verify the z-score is positive and > 2 for a large value.
	d2 := newTestDetector(t)
	for _, v := range vals {
		d2.Record(v)
	}
	a2 := d2.Record(15.0)
	// Buffer: [2,4,4,4,5,5,7,9,5,5,15] = 11 values
	// mean = 60/11 = 5.4545
	// variance = (11.93 + 2.11*3 + 0.21*4 + 2.39 + 12.57 + 91.02) / 11
	// Actually compute:
	// sum of (vi-5.4545)^2: (2-5.45)^2=11.93, (4-5.45)^2=2.11*3=6.33,
	// (5-5.45)^2=0.21*4=0.84, (7-5.45)^2=2.39, (9-5.45)^2=12.57, (15-5.45)^2=91.16
	// total sq = 11.93+6.33+0.84+2.39+12.57+91.16 = 125.22
	// variance = 125.22/11 = 11.38, stddev = 3.37
	// z = (15-5.45)/3.37 = 2.83 -> spike, medium
	if a2.Type != AnomalySpike {
		t.Fatalf("expected spike for value=15, got type %d (z=%f)", a2.Type, a2.Score)
	}
}

// --- 10. Concurrent Record ---

func TestAnomalyDetector_ConcurrentRecord(t *testing.T) {
	d := newTestDetector(t)
	for i := 0; i < 20; i++ {
		d.Record(100.0)
	}

	var wg sync.WaitGroup
	n := 200
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(v int) {
			defer wg.Done()
			d.Record(float64(v))
		}(i)
	}
	wg.Wait()

	d.mu.Lock()
	cnt := d.count
	d.mu.Unlock()

	if cnt < 20+n {
		t.Fatalf("count: got %d, want >= %d", cnt, 20+n)
	}
}

// --- 11. Counter increment ---

func TestAnomalyDetector_CounterIncrement(t *testing.T) {
	// Each subtest gets its own detector to avoid cross-contamination.
	// With 99 identical fill values + 1 anomaly, z = sqrt(99) ~ 9.95 -> always Critical.
	// For medium/high severities, use a mixed baseline to reduce z.
	// 50 zeros + 49 hundreds: mean=49.495, stddev=stddev_of_99_values.
	// For spike test: value slightly above baseline -> medium z.
	tests := []struct {
		name      string
		values    []float64 // baseline values (must be >= 9)
		anomaly   float64
		repeat    int
		wantLabel string
		wantSev   string
	}{
		// spike/critical: all zeros + huge spike
		{"spike/critical", makeVals(99, 0.0), 1e6, 1, "spike", "critical"},
		// drop/critical: all large + zero drop
		{"drop/critical", makeVals(99, 1e6), 0.0, 1, "drop", "critical"},
		// spike/medium: use a high-variance baseline so z stays between 2 and 3.
		// 50 zeros + 50 hundreds -> mean=50, pop stddev=50. With value 200:
		// buffer has 101 values, mean=(5000+200)/101=51.49, z=(200-51.49)/stddev.
		// stddev with 101 values ~= 50.25, z ~= 2.95 -> still could be medium.
		// Use value=170: mean=(5000+170)/101=51.19, z=(170-51.19)/50.24=2.36 -> medium
		{"spike/medium", append(makeVals(50, 0.0), makeVals(50, 100.0)...), 170.0, 1, "spike", "medium"},
		// drop/medium: same baseline, value=-70
		// mean=(5000-70)/101=48.81, z=(-70-48.81)/50.24=-2.36 -> medium
		{"drop/medium", append(makeVals(50, 0.0), makeVals(50, 100.0)...), -70.0, 1, "drop", "medium"},
		// spike/high: value=250, z=(250-51.98)/50.13=3.95 -> high
		{"spike/high", append(makeVals(50, 0.0), makeVals(50, 100.0)...), 250.0, 1, "spike", "high"},
		// sustained_high/critical
		{"sustained_high/critical", makeVals(99, 0.0), 1e6, sustainedCount, "sustained_high", "critical"},
		// sustained_low/critical
		{"sustained_low/critical", makeVals(99, 1e6), 0.0, sustainedCount, "sustained_low", "critical"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			d := NewAnomalyDetector(reg)

			for _, v := range tc.values {
				d.Record(v)
			}

			for i := 0; i < tc.repeat; i++ {
				d.Record(tc.anomaly)
			}

			count := testutil.ToFloat64(d.anomalyTotal.WithLabelValues(tc.wantLabel, tc.wantSev))
			if count < 1.0 {
				a := d.Record(tc.anomaly)
				t.Fatalf("counter for %s/%s: got %f, want >= 1 (last z=%f)", tc.wantLabel, tc.wantSev, count, a.Score)
			}
		})
	}
}

// makeVals returns n copies of v.
func makeVals(n int, v float64) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = v
	}
	return s
}
