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

// AnomalyDetector detects rate anomalies using z-score analysis against a ring buffer baseline.
type AnomalyDetector struct {
	mu           sync.Mutex
	buf          [bufSize]float64
	idx          int
	count        int
	highStreak   int
	lowStreak    int
	anomalyTotal *prometheus.CounterVec
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

// Record adds a sample to the buffer, computes z-score, and returns any detected anomaly.
func (d *AnomalyDetector) Record(value float64) Anomaly {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.buf[d.idx] = value
	d.idx = (d.idx + 1) % bufSize
	if d.count < bufSize {
		d.count++
	}

	if d.count < 10 {
		return Anomaly{Type: AnomalyNone, Mean: value, Value: value}
	}

	mean, stddev := d.stats()
	if stddev == 0 {
		return Anomaly{Type: AnomalyNone, Mean: mean, Value: value}
	}

	z := (value - mean) / stddev

	a := Anomaly{
		Score: z,
		Value: value,
		Mean:  mean,
	}

	switch {
	case z > 2.0:
		a.Type = AnomalySpike
		d.highStreak++
		d.lowStreak = 0
	case z < -2.0:
		a.Type = AnomalyDrop
		d.lowStreak++
		d.highStreak = 0
	default:
		d.highStreak = 0
		d.lowStreak = 0
		return a
	}

	// Sustained detection overrides the initial spike/drop classification.
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

	return a
}

func (d *AnomalyDetector) stats() (mean, stddev float64) {
	n := d.count
	var sum float64
	for i := 0; i < n; i++ {
		sum += d.buf[i]
	}
	mean = sum / float64(n)

	var sqSum float64
	for i := 0; i < n; i++ {
		diff := d.buf[i] - mean
		sqSum += diff * diff
	}
	stddev = math.Sqrt(sqSum / float64(n))
	return
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
