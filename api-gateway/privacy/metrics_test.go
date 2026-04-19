package privacy

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestNewMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	assert.NotNil(t, m)
	assert.NotNil(t, m.MaskDuration)
	assert.NotNil(t, m.SecretsDetected)
	assert.NotNil(t, m.PIIDetected)
	assert.NotNil(t, m.MaskRequestsTotal)
}

func TestMetrics_ObserveMaskDuration(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.ObserveMaskDuration("secrets_detect", 5*time.Millisecond)
	m.ObserveMaskDuration("mask", time.Millisecond)
	m.ObserveMaskDuration("unmask", 2*time.Millisecond)
}

func TestMetrics_IncSecretsDetected(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.IncSecretsDetected("API_KEY_SK", 2)
	m.IncSecretsDetected("JWT_TOKEN", 1)
}

func TestMetrics_IncPIIDetected(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.IncPIIDetected("PERSON")
	m.IncPIIDetected("EMAIL_ADDRESS")
}

func TestMetrics_IncMaskRequests(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.IncMaskRequests(true, false)
	m.IncMaskRequests(false, true)
	m.IncMaskRequests(true, true)
	m.IncMaskRequests(false, false)
}
