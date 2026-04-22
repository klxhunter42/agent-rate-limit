package middleware

import (
	"math"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// AnomalyType classifies detected anomalies.
type AnomalyType int

const (
	AnomalyNone          AnomalyType = iota
	AnomalySpike                     // sudden increase
	AnomalyDrop                      // sudden decrease
	AnomalySustainedHigh             // prolonged elevated rate
	AnomalySustainedLow              // prolonged decreased rate
)

// AnomalySeverity represents how severe an anomaly is.
type AnomalySeverity int

const (
	SeverityLow AnomalySeverity = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

// Anomaly describes a detected rate anomaly.
type Anomaly struct {
	Type     AnomalyType
	Severity AnomalySeverity
	Score    float64 // z-score value
	Value    float64 // observed value
	Mean     float64 // baseline mean
}

const (
	bufSize        = 1000
	sustainedCount = 5
)

// AnomalyDetector detects rate anomalies using Welford's online algorithm
// for incremental mean/variance. O(1) per sample with no buffer iteration.
type AnomalyDetector struct {
	mu           sync.Mutex
	highStreak   int
	lowStreak    int
	anomalyTotal *prometheus.CounterVec

	// Welford's online stats (protected by atomic, no mutex needed on hot path).
	n    int64   // sample count
	mean float64 // running mean
	m2   float64 // running sum of squared deviations

	buf   [bufSize]float64
	idx   int64
	count int64
}

// NewAnomalyDetector creates a new detector and registers its counter on the given registerer.
func NewAnomalyDetector(reg prometheus.Registerer) *AnomalyDetector {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "api_gateway",
		Name:      "anomaly_total",
		Help:      "Total number of detected anomalies by type and severity.",
	}, []string{"type", "severity"})
	reg.MustRegister(counter)

	return &AnomalyDetector{anomalyTotal: counter}
}

// Record adds a sample and returns any detected anomaly.
// Uses Welford's online algorithm for O(1) mean/stddev update.
func (d *AnomalyDetector) Record(value float64) Anomaly {
	d.mu.Lock()

	d.idx = (d.idx + 1) % bufSize
	d.buf[d.idx] = value
	d.count++

	// Welford's update (always accumulate).
	d.n++
	delta := value - d.mean
	d.mean += delta / float64(d.n)
	delta2 := value - d.mean
	d.m2 += delta * delta2

	if d.count < 10 {
		d.mu.Unlock()
		return Anomaly{Type: AnomalyNone, Mean: value, Value: value}
	}

	if d.n == 0 {
		d.mu.Unlock()
		return Anomaly{Type: AnomalyNone, Mean: value, Value: value}
	}

	variance := d.m2 / float64(d.n)
	stddev := math.Sqrt(variance)

	if stddev == 0 {
		a := Anomaly{Type: AnomalyNone, Mean: d.mean, Value: value}
		d.mu.Unlock()
		return a
	}

	z := (value - d.mean) / stddev

	a := Anomaly{
		Score: z,
		Value: value,
		Mean:  d.mean,
	}

	switch {
	case z > 2.0:
		a.Type = AnomalySpike
	case z < -2.0:
		a.Type = AnomalyDrop
	default:
		d.highStreak = 0
		d.lowStreak = 0
		d.mu.Unlock()
		return a
	}

	if a.Type == AnomalySpike {
		d.highStreak++
		d.lowStreak = 0
	} else {
		d.lowStreak++
		d.highStreak = 0
	}

	if d.highStreak >= sustainedCount {
		a.Type = AnomalySustainedHigh
	} else if d.lowStreak >= sustainedCount {
		a.Type = AnomalySustainedLow
	}

	az := math.Abs(z)
	switch {
	case az > 4.0:
		a.Severity = SeverityCritical
	case az > 3.0:
		a.Severity = SeverityHigh
	default:
		a.Severity = SeverityMedium
	}

	d.anomalyTotal.WithLabelValues(anomalyLabel(a.Type), severityLabel(a.Severity)).Inc()
	d.mu.Unlock()

	return a
}

func anomalyLabel(t AnomalyType) string {
	switch t {
	case AnomalySpike:
		return "spike"
	case AnomalyDrop:
		return "drop"
	case AnomalySustainedHigh:
		return "sustained_high"
	case AnomalySustainedLow:
		return "sustained_low"
	default:
		return "none"
	}
}

func severityLabel(s AnomalySeverity) string {
	switch s {
	case SeverityLow:
		return "low"
	case SeverityMedium:
		return "medium"
	case SeverityHigh:
		return "high"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}
