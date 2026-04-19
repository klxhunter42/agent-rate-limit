package privacy

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "api_gateway"

type Metrics struct {
	MaskDuration      *prometheus.HistogramVec
	SecretsDetected   *prometheus.CounterVec
	PIIDetected       *prometheus.CounterVec
	MaskRequestsTotal *prometheus.CounterVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		MaskDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "mask_duration_seconds",
			Help:      "Duration of mask/unmask operations by phase",
			Buckets:   []float64{0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		}, []string{"phase"}),

		SecretsDetected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "secrets_detected_total",
			Help:      "Total secrets detected by type",
		}, []string{"type"}),

		PIIDetected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "pii_detected_total",
			Help:      "Total PII entities detected by type",
		}, []string{"type"}),

		MaskRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "mask_requests_total",
			Help:      "Total requests processed by the masking pipeline",
		}, []string{"has_secrets", "has_pii"}),
	}

	reg.MustRegister(m.MaskDuration)
	reg.MustRegister(m.SecretsDetected)
	reg.MustRegister(m.PIIDetected)
	reg.MustRegister(m.MaskRequestsTotal)

	return m
}

func (m *Metrics) ObserveMaskDuration(phase string, d time.Duration) {
	m.MaskDuration.WithLabelValues(phase).Observe(d.Seconds())
}

func (m *Metrics) IncSecretsDetected(typ string, count int) {
	m.SecretsDetected.WithLabelValues(typ).Add(float64(count))
}

func (m *Metrics) IncPIIDetected(typ string) {
	m.PIIDetected.WithLabelValues(typ).Inc()
}

func (m *Metrics) IncMaskRequests(hasSecrets, hasPII bool) {
	s := "false"
	if hasSecrets {
		s = "true"
	}
	p := "false"
	if hasPII {
		p = "true"
	}
	m.MaskRequestsTotal.WithLabelValues(s, p).Inc()
}
