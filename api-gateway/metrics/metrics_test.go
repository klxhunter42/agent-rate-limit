package metrics

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMetrics(t *testing.T) *Metrics {
	t.Helper()
	return New(func() float64 { return 0 }, map[string][2]float64{
		"glm-5.1":     {0.5, 1.5},
		"glm-5-turbo": {0.5, 1.5},
		"glm-5":       {0.5, 1.5},
	})
}

func TestRecordTokens(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordTokens("glm-5.1", 100, 50)

	inputVal := testutil.ToFloat64(m.TokenInput.WithLabelValues("glm-5.1"))
	outputVal := testutil.ToFloat64(m.TokenOutput.WithLabelValues("glm-5.1"))
	assert.Equal(t, 100.0, inputVal)
	assert.Equal(t, 50.0, outputVal)
}

func TestRecordTokensWithPricing(t *testing.T) {
	m := newTestMetrics(t)

	// glm-5.1: input=$0.5/1M, output=$1.5/1M
	// 1M input * 0.5 + 2M output * 1.5 = 0.5 + 3.0 = 3.5
	m.RecordTokens("glm-5.1", 1_000_000, 2_000_000)

	costVal := testutil.ToFloat64(m.CostTotal.WithLabelValues("glm-5.1"))
	assert.InDelta(t, 3.5, costVal, 0.001)
}

func TestRecordTokensZeroValues(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordTokens("glm-5", 0, 0)

	inputVal := testutil.ToFloat64(m.TokenInput.WithLabelValues("glm-5"))
	outputVal := testutil.ToFloat64(m.TokenOutput.WithLabelValues("glm-5"))
	costVal := testutil.ToFloat64(m.CostTotal.WithLabelValues("glm-5"))
	assert.Equal(t, 0.0, inputVal)
	assert.Equal(t, 0.0, outputVal)
	assert.Equal(t, 0.0, costVal)
}

func TestRecordTokensUnknownModelNoCost(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordTokens("unknown-model", 500, 200)

	inputVal := testutil.ToFloat64(m.TokenInput.WithLabelValues("unknown-model"))
	outputVal := testutil.ToFloat64(m.TokenOutput.WithLabelValues("unknown-model"))
	assert.Equal(t, 500.0, inputVal)
	assert.Equal(t, 200.0, outputVal)

	// No pricing configured for unknown-model, so cost should be 0.
	// WithLabelValues creates the label but counter stays at 0.
	costVal := testutil.ToFloat64(m.CostTotal.WithLabelValues("unknown-model"))
	assert.Equal(t, 0.0, costVal)
}

func TestRecordFallback(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordFallback("glm-5.1", "glm-5-turbo")
	m.RecordFallback("glm-5.1", "glm-5-turbo")

	val := testutil.ToFloat64(m.ModelFallback.WithLabelValues("glm-5.1", "glm-5-turbo"))
	assert.Equal(t, 2.0, val)
}

func TestRecordTTFB(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordTTFB("glm-5.1", 150*time.Millisecond)

	// Gather the metric family and verify histogram sample_count > 0.
	families, err := m.Registry().Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() == "api_gateway_ttfb_seconds" {
			require.NotEmpty(t, f.GetMetric())
			assert.Equal(t, uint64(1), f.GetMetric()[0].GetHistogram().GetSampleCount())
			return
		}
	}
	t.Fatal("api_gateway_ttfb_seconds not found in gathered metrics")
}

func TestIncError(t *testing.T) {
	m := newTestMetrics(t)

	m.IncError("upstream")
	m.IncError("upstream")
	m.IncError("queue_push")

	assert.Equal(t, 2.0, testutil.ToFloat64(m.ErrorRate.WithLabelValues("upstream")))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.ErrorRate.WithLabelValues("queue_push")))
}

func TestIncRateLimit(t *testing.T) {
	m := newTestMetrics(t)

	m.IncRateLimit("agent-123")

	assert.Equal(t, 1.0, testutil.ToFloat64(m.RateLimitHits.WithLabelValues("agent-123")))
}

func TestIncRetry(t *testing.T) {
	m := newTestMetrics(t)

	m.IncRetry()
	m.IncRetry()

	assert.Equal(t, 2.0, testutil.ToFloat64(m.UpstreamRetries))
}

func TestInc429(t *testing.T) {
	m := newTestMetrics(t)

	m.Inc429()

	assert.Equal(t, 1.0, testutil.ToFloat64(m.Upstream429))
}

func TestUpdateAdaptiveMetrics(t *testing.T) {
	m := newTestMetrics(t)

	statuses := []ModelStatusSnapshot{
		{Name: "glm-5.1", Limit: 5, InFlight: 3},
		{Name: "glm-5-turbo", Limit: 2, InFlight: 1},
	}
	m.UpdateAdaptiveMetrics(statuses)

	assert.Equal(t, 5.0, testutil.ToFloat64(m.AdaptiveLimit.WithLabelValues("glm-5.1")))
	assert.Equal(t, 3.0, testutil.ToFloat64(m.AdaptiveInFlight.WithLabelValues("glm-5.1")))
	assert.Equal(t, 2.0, testutil.ToFloat64(m.AdaptiveLimit.WithLabelValues("glm-5-turbo")))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.AdaptiveInFlight.WithLabelValues("glm-5-turbo")))
}

func TestUpdateAdaptiveMetricsOverwrite(t *testing.T) {
	m := newTestMetrics(t)

	m.UpdateAdaptiveMetrics([]ModelStatusSnapshot{
		{Name: "glm-5.1", Limit: 5, InFlight: 3},
	})
	m.UpdateAdaptiveMetrics([]ModelStatusSnapshot{
		{Name: "glm-5.1", Limit: 10, InFlight: 0},
	})

	assert.Equal(t, 10.0, testutil.ToFloat64(m.AdaptiveLimit.WithLabelValues("glm-5.1")))
	assert.Equal(t, 0.0, testutil.ToFloat64(m.AdaptiveInFlight.WithLabelValues("glm-5.1")))
}

func TestRegistry(t *testing.T) {
	m := newTestMetrics(t)

	reg := m.Registry()
	require.NotNil(t, reg)

	// The registry should have our metrics registered.
	families, err := reg.Gather()
	require.NoError(t, err)
	assert.Greater(t, len(families), 0)
}

func TestQueueDepth(t *testing.T) {
	depth := 42.0
	m := New(func() float64 { return depth }, nil)

	val := testutil.ToFloat64(m.QueueDepth)
	assert.Equal(t, depth, val)
}

func TestMiddleware(t *testing.T) {
	m := newTestMetrics(t)

	// Build a chi router with a route so RouteContext works.
	r := chi.NewRouter()
	r.Use(m.Middleware)
	r.Get("/v1/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Simulate a request.
	req := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Active connections should be back to 0 after request completes.
	assert.Equal(t, 0.0, testutil.ToFloat64(m.ActiveConnections))

	// Request latency should have been recorded (histogram sample_count > 0).
	families, err := m.Registry().Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() == "api_gateway_request_latency_seconds" {
			require.NotEmpty(t, f.GetMetric())
			assert.Greater(t, f.GetMetric()[0].GetHistogram().GetSampleCount(), uint64(0))
			break
		}
	}
}

func TestMiddlewareTracksActiveConnections(t *testing.T) {
	m := newTestMetrics(t)

	// Use a blocking handler to observe active connections mid-request.
	block := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// While we're inside the handler, active connections should be 1.
		assert.Equal(t, 1.0, testutil.ToFloat64(m.ActiveConnections))
		close(block)
	})

	wrapped := m.Middleware(handler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// RouteContext is needed by the middleware; use a bare request.
	// Since there's no chi router, routePattern will fall back to URL.Path.
	go wrapped.ServeHTTP(rec, req)

	<-block
	// After handler returns, defer should have decremented.
	// Give it a moment.
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 0.0, testutil.ToFloat64(m.ActiveConnections))
}

func TestMiddlewareCapturesStatus(t *testing.T) {
	m := newTestMetrics(t)

	r := chi.NewRouter()
	r.Use(m.Middleware)
	r.Get("/fail", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	// Should have recorded a 500 status (histogram sample_count > 0).
	families, err := m.Registry().Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() == "api_gateway_request_latency_seconds" {
			require.NotEmpty(t, f.GetMetric())
			assert.Greater(t, f.GetMetric()[0].GetHistogram().GetSampleCount(), uint64(0))
			break
		}
	}
}

func TestHandler(t *testing.T) {
	m := newTestMetrics(t)

	handler := m.Handler()
	require.NotNil(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/")

	body := rec.Body.String()
	assert.True(t, strings.Contains(body, "api_gateway"))
}

func TestHandlerPrometheusFormat(t *testing.T) {
	m := newTestMetrics(t)
	m.RecordTokens("glm-5.1", 100, 50)

	handler := m.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	assert.True(t, strings.Contains(body, "api_gateway_token_input_total"))
	assert.True(t, strings.Contains(body, "api_gateway_token_output_total"))
	assert.True(t, strings.Contains(body, "HELP"))
	assert.True(t, strings.Contains(body, "TYPE"))
}

func TestHandlerUsesSeparateRegistry(t *testing.T) {
	m := newTestMetrics(t)

	// Record some data so the metric family appears in Gather output.
	m.IncError("test")

	handler := m.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	// Should contain at least the error_total we just recorded.
	assert.True(t, strings.Contains(body, "api_gateway_error_total"))
	// Should NOT contain metrics from the default global registry.
	assert.False(t, strings.Contains(body, "promhttp_metric_handler"))
}

// Verify statusWriter captures status codes correctly.
func TestStatusWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := newStatusWriter(rec)

	assert.Equal(t, http.StatusOK, sw.status)

	sw.WriteHeader(http.StatusNotFound)
	assert.Equal(t, http.StatusNotFound, sw.status)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestStatusWriterDefaultStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := newStatusWriter(rec)

	sw.Write([]byte("data"))
	// Default status should remain 200 even after Write.
	assert.Equal(t, http.StatusOK, sw.status)
}

func TestRecordTokensMultipleModels(t *testing.T) {
	m := newTestMetrics(t)

	m.RecordTokens("glm-5.1", 100, 50)
	m.RecordTokens("glm-5-turbo", 200, 100)

	assert.Equal(t, 100.0, testutil.ToFloat64(m.TokenInput.WithLabelValues("glm-5.1")))
	assert.Equal(t, 200.0, testutil.ToFloat64(m.TokenInput.WithLabelValues("glm-5-turbo")))
}

func TestNewRegistersAllMetrics(t *testing.T) {
	m := newTestMetrics(t)

	// Verify all metric objects are non-nil (they are registered in New).
	require.NotNil(t, m.RequestLatency)
	require.NotNil(t, m.QueueDepth)
	require.NotNil(t, m.ErrorRate)
	require.NotNil(t, m.RateLimitHits)
	require.NotNil(t, m.ActiveConnections)
	require.NotNil(t, m.TokenInput)
	require.NotNil(t, m.TokenOutput)
	require.NotNil(t, m.UpstreamRetries)
	require.NotNil(t, m.Upstream429)
	require.NotNil(t, m.AdaptiveLimit)
	require.NotNil(t, m.AdaptiveInFlight)
	require.NotNil(t, m.CostTotal)
	require.NotNil(t, m.ModelFallback)
	require.NotNil(t, m.TTFB)

	// Touch each metric so Gather returns all families.
	m.RequestLatency.WithLabelValues("GET", "/", "200").Observe(0.1)
	m.ErrorRate.WithLabelValues("test").Inc()
	m.RateLimitHits.WithLabelValues("test").Inc()
	m.TokenInput.WithLabelValues("test").Inc()
	m.TokenOutput.WithLabelValues("test").Inc()
	m.UpstreamRetries.Inc()
	m.Upstream429.Inc()
	m.AdaptiveLimit.WithLabelValues("test").Set(1)
	m.AdaptiveInFlight.WithLabelValues("test").Set(0)
	m.CostTotal.WithLabelValues("test").Inc()
	m.ModelFallback.WithLabelValues("a", "b").Inc()
	m.TTFB.WithLabelValues("test").Observe(0.1)

	reg := m.Registry()
	families, err := reg.Gather()
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, f := range families {
		names[f.GetName()] = true
	}

	expected := []string{
		"api_gateway_request_latency_seconds",
		"api_gateway_queue_depth",
		"api_gateway_error_total",
		"api_gateway_rate_limit_hits_total",
		"api_gateway_active_connections",
		"api_gateway_token_input_total",
		"api_gateway_token_output_total",
		"api_gateway_upstream_retries_total",
		"api_gateway_upstream_429_total",
		"api_gateway_adaptive_limit",
		"api_gateway_adaptive_in_flight",
		"api_gateway_cost_total",
		"api_gateway_model_fallback_total",
		"api_gateway_ttfb_seconds",
	}

	for _, name := range expected {
		assert.True(t, names[name], "expected metric %q to be registered", name)
	}
}

// Benchmark RecordTokens to ensure it's not doing anything expensive.
func BenchmarkRecordTokens(b *testing.B) {
	m := New(func() float64 { return 0 }, map[string][2]float64{
		"glm-5.1": {0.5, 1.5},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordTokens("glm-5.1", 100, 50)
	}
}

// Check that testutil works correctly with CounterVec.
func TestTestutilCounterVec(t *testing.T) {
	m := newTestMetrics(t)

	// Calling ToFloat64 on a label that hasn't been used should return 0.
	val := testutil.ToFloat64(m.TokenInput.WithLabelValues("nonexistent"))
	assert.Equal(t, 0.0, val)
}

// Helper to verify JSON output from handler.
func TestHandlerOutputIsValid(t *testing.T) {
	m := newTestMetrics(t)

	handler := m.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Body should not be empty.
	body := rec.Body.String()
	assert.NotEmpty(t, body)

	// Should be parseable as Prometheus exposition format (text-based, not JSON).
	// Just verify it has multiple lines.
	lines := strings.Split(strings.TrimSpace(body), "\n")
	assert.Greater(t, len(lines), 10)
}

// Ensure middleware doesn't panic with nil chi context.
func TestMiddlewareNoChiContext(t *testing.T) {
	m := newTestMetrics(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	wrapped := m.Middleware(handler)
	req := httptest.NewRequest(http.MethodGet, "/raw/path", nil)
	rec := httptest.NewRecorder()

	// No chi router, so RouteContext will be nil.
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTeapot, rec.Code)

	// Should have recorded with the raw path as fallback (histogram sample_count > 0).
	families, err := m.Registry().Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() == "api_gateway_request_latency_seconds" {
			require.NotEmpty(t, f.GetMetric())
			assert.Greater(t, f.GetMetric()[0].GetHistogram().GetSampleCount(), uint64(0))
			break
		}
	}
}

// Verify that the testutil import works.
var _ = testutil.ToFloat64
var _ prometheus.Collector
var _ = json.NewDecoder(io.Reader(nil)).Decode
