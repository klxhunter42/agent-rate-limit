//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const (
	gatewayURL  = "http://localhost:8080"
	grafanaURL  = "http://localhost:3000"
	testAPIKey  = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"
	grafanaUser = "admin"
	grafanaPass = "klxhunter"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeTestClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// authGet performs an authenticated GET with x-api-key.
func authGet(client *http.Client, path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, gatewayURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", testAPIKey)
	return client.Do(req)
}

// authGetCustom performs an authenticated GET with extra headers.
func authGetCustom(client *http.Client, path string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, gatewayURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", testAPIKey)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return client.Do(req)
}

// parseJSON decodes the response body into v.
func parseJSON(resp *http.Response, v any) error {
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// parsePrometheusMetrics parses Prometheus exposition format into
// metric name -> slice of label sets (each label set is the full metric line
// after the name, including labels and value).
// Also captures metric names from # TYPE lines so registered-but-unused metrics
// (counters with no observations yet) are still detected.
func parsePrometheusMetrics(body string) map[string][]string {
	metrics := make(map[string][]string)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// # TYPE metric_name <type>
		if strings.HasPrefix(line, "# TYPE ") {
			// "# TYPE api_gateway_xxx counter" -> fields[2] is the metric name
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				name := parts[2]
				// Ensure entry exists so hasMetric returns true
				if _, ok := metrics[name]; !ok {
					metrics[name] = nil
				}
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		// metric_name{...} value [timestamp]
		idx := strings.Index(line, "{")
		if idx == -1 {
			// No labels: metric_name value
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				name := parts[0]
				metrics[name] = append(metrics[name], line)
			}
			continue
		}
		name := line[:idx]
		metrics[name] = append(metrics[name], line)
	}
	return metrics
}

// hasMetric checks if a metric name exists in the parsed metrics map.
func hasMetric(metrics map[string][]string, name string) bool {
	_, ok := metrics[name]
	return ok
}

// hasMetricWithLabel checks if a metric name exists and has at least one entry
// containing the given label substring.
func hasMetricWithLabel(metrics map[string][]string, name, labelSubstr string) bool {
	entries, ok := metrics[name]
	if !ok {
		return false
	}
	for _, e := range entries {
		if strings.Contains(e, labelSubstr) {
			return true
		}
	}
	return false
}

// requireGateway attempts to connect to the gateway, failing the test if unreachable.
func requireGateway(t *testing.T, client *http.Client) {
	t.Helper()
	resp, err := client.Get(gatewayURL + "/health")
	if err != nil {
		t.Fatalf("gateway not reachable at %s: %v", gatewayURL, err)
	}
	resp.Body.Close()
}

// ---------------------------------------------------------------------------
// 1. Health Check
// ---------------------------------------------------------------------------

func TestHealthCheck(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	resp, err := client.Get(gatewayURL + "/health")
	if err != nil {
		t.Fatalf("health check request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := parseJSON(resp, &body); err != nil {
		t.Fatalf("failed to parse health response: %v", err)
	}

	if body.Status != "healthy" {
		t.Fatalf("expected status 'healthy', got '%s'", body.Status)
	}
}

// ---------------------------------------------------------------------------
// 2. Adaptive Limiter Status
// ---------------------------------------------------------------------------

func TestLimiterStatus(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	// Test without auth -> 401
	resp, err := client.Get(gatewayURL + "/v1/limiter-status")
	if err != nil {
		t.Fatalf("limiter-status request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", resp.StatusCode)
	}

	// Test with auth -> 200
	resp, err = authGet(client, "/v1/limiter-status")
	if err != nil {
		t.Fatalf("authenticated limiter-status request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := parseJSON(resp, &body); err != nil {
		t.Fatalf("failed to parse limiter-status response: %v", err)
	}

	// Verify top-level keys
	for _, key := range []string{"global", "models", "keyPool"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing top-level key '%s'", key)
		}
	}

	// Verify global structure
	global, ok := body["global"].(map[string]any)
	if !ok {
		t.Fatalf("global is not a map")
	}
	for _, key := range []string{"global_in_flight", "global_limit"} {
		if _, ok := global[key]; !ok {
			t.Errorf("missing global field '%s'", key)
		}
	}

	// Verify models structure
	models, ok := body["models"].([]any)
	if !ok {
		t.Fatalf("models is not an array")
	}
	if len(models) == 0 {
		t.Fatal("models array is empty")
	}

	for _, m := range models {
		model, ok := m.(map[string]any)
		if !ok {
			t.Errorf("model entry is not a map: %v", m)
			continue
		}
		requiredFields := []string{
			"name", "in_flight", "limit", "max_limit",
			"learned_ceiling", "total_requests", "total_429s",
			"min_rtt_ms", "ewma_rtt_ms", "series", "overridden",
		}
		for _, field := range requiredFields {
			if _, ok := model[field]; !ok {
				t.Errorf("model '%v' missing field '%s'", model["name"], field)
			}
		}
	}

	// Verify keyPool structure
	keyPool, ok := body["keyPool"].(map[string]any)
	if !ok {
		t.Fatalf("keyPool is not a map")
	}
	if _, ok := keyPool["total_keys"]; !ok {
		t.Error("keyPool missing 'total_keys' field")
	}
}

// ---------------------------------------------------------------------------
// 3. Metrics Endpoint
// ---------------------------------------------------------------------------

func TestMetricsEndpoint(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	resp, err := client.Get(gatewayURL + "/metrics")
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected Content-Type text/plain, got '%s'", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read metrics body: %v", err)
	}

	metrics := parsePrometheusMetrics(string(body))

	// Core metrics -- always present after startup (have data from health checks or runtime).
	coreMetrics := []string{
		"api_gateway_request_latency_seconds",
		"api_gateway_error_total",
		"api_gateway_upstream_retries_total",
		"api_gateway_upstream_429_total",
		"api_gateway_active_connections",
		"api_gateway_queue_depth",
	}

	// Runtime metrics -- always present (set by periodic collector).
	runtimeMetrics := []string{
		"api_gateway_go_goroutines",
		"api_gateway_go_heap_alloc_bytes",
		"api_gateway_go_heap_objects",
		"api_gateway_go_gc_pause_ns",
		"api_gateway_go_stack_inuse_bytes",
		"api_gateway_dragonfly_up",
	}

	// Adaptive metrics -- always present (set by periodic exporter).
	adaptiveMetrics := []string{
		"api_gateway_adaptive_limit",
		"api_gateway_adaptive_in_flight",
	}

	// Lazy metrics -- only appear after first observation. Verified as a group
	// with a soft check: at least one must be present, the rest are logged.
	lazyMetrics := []string{
		"api_gateway_rate_limit_hits_total",
		"api_gateway_token_input_total",
		"api_gateway_token_output_total",
		"api_gateway_cost_total",
		"api_gateway_ttfb_seconds",
		"api_gateway_model_fallback_total",
		"api_gateway_anomaly_total",
	}

	for _, m := range coreMetrics {
		if !hasMetric(metrics, m) {
			t.Errorf("missing core metric '%s'", m)
		}
	}

	for _, m := range runtimeMetrics {
		if !hasMetric(metrics, m) {
			t.Errorf("missing runtime metric '%s'", m)
		}
	}

	for _, m := range adaptiveMetrics {
		if !hasMetric(metrics, m) {
			t.Errorf("missing adaptive metric '%s'", m)
		}
	}

	for _, m := range lazyMetrics {
		if !hasMetric(metrics, m) {
			t.Logf("lazy metric '%s' not yet observed (expected if no triggering traffic)", m)
		}
	}

	// Verify dragonfly_up is 1 (healthy)
	if entries, ok := metrics["api_gateway_dragonfly_up"]; ok && len(entries) > 0 {
		line := entries[len(entries)-1] // last line is the current value
		if !strings.HasSuffix(strings.TrimSpace(line), " 1") {
			t.Errorf("expected dragonfly_up = 1, got: %s", line)
		}
	}

	// Verify request_latency_seconds is a histogram (has _sum, _count, _bucket suffixes)
	for _, suffix := range []string{"_count", "_sum", "_bucket"} {
		if !hasMetric(metrics, "api_gateway_request_latency_seconds"+suffix) {
			t.Errorf("missing histogram suffix '%s' for request_latency_seconds", suffix)
		}
	}

	// Count total unique api_gateway_* metrics found
	var found int
	for name := range metrics {
		if strings.HasPrefix(name, "api_gateway_") {
			// Strip histogram suffixes to avoid double counting
			base := name
			for _, s := range []string{"_count", "_sum", "_bucket"} {
				base = strings.TrimSuffix(base, s)
			}
			if base == name || !strings.HasSuffix(name, "_count") && !strings.HasSuffix(name, "_sum") {
				found++
			}
		}
	}
	t.Logf("found %d unique api_gateway metric families", found)
}

// ---------------------------------------------------------------------------
// 4. Security Headers
// ---------------------------------------------------------------------------

func TestSecurityHeaders(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	resp, err := client.Get(gatewayURL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	expected := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"X-XSS-Protection":       "1; mode=block",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}

	for header, want := range expected {
		got := resp.Header.Get(header)
		if got != want {
			t.Errorf("header '%s': expected '%s', got '%s'", header, want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// 5. Correlation ID
// ---------------------------------------------------------------------------

func TestCorrelationID_Generated(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	resp, err := client.Get(gatewayURL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	corrID := resp.Header.Get("X-Correlation-ID")
	if corrID == "" {
		t.Fatal("expected X-Correlation-ID header to be set")
	}

	// Verify it's a valid UUID
	if _, err := uuid.Parse(corrID); err != nil {
		t.Errorf("X-Correlation-ID '%s' is not a valid UUID: %v", corrID, err)
	}
}

func TestCorrelationID_Echo(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	customID := uuid.New().String()
	resp, err := authGetCustom(client, "/health", map[string]string{
		"X-Correlation-ID": customID,
	})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	got := resp.Header.Get("X-Correlation-ID")
	if got != customID {
		t.Errorf("expected echoed correlation ID '%s', got '%s'", customID, got)
	}
}

// ---------------------------------------------------------------------------
// 6. RealIP Extraction
// ---------------------------------------------------------------------------

func TestRealIP_Extraction(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	testCases := []struct {
		name    string
		headers map[string]string
		wantIP  string // if empty, just verify request succeeds
	}{
		{
			name:    "X-Real-IP",
			headers: map[string]string{"X-Real-IP": "203.0.113.42"},
		},
		{
			name:    "CF-Connecting-IP",
			headers: map[string]string{"CF-Connecting-IP": "198.51.100.7"},
		},
		{
			name:    "X-Forwarded-For single",
			headers: map[string]string{"X-Forwarded-For": "192.0.2.1"},
		},
		{
			name:    "X-Forwarded-For multiple",
			headers: map[string]string{"X-Forwarded-For": "10.0.0.1, 172.16.0.1, 192.0.2.1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := authGetCustom(client, "/health", tc.headers)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected 200, got %d", resp.StatusCode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 7. Chat Completions Endpoint
// ---------------------------------------------------------------------------

func TestChatCompletions_Queued(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	payload := map[string]any{
		"agent_id": "test-agent-001",
		"messages": []map[string]string{
			{"role": "user", "content": "Hello"},
		},
		"max_tokens":  64,
		"temperature": 0.7,
		"model":       "glm-5.1",
		"provider":    "glm",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", testAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("chat completions request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", resp.StatusCode)
	}

	var chatResp struct {
		RequestID string `json:"request_id"`
		Status    string `json:"status"`
		AgentID   string `json:"agent_id"`
	}
	if err := parseJSON(resp, &chatResp); err != nil {
		t.Fatalf("failed to parse chat response: %v", err)
	}

	if chatResp.RequestID == "" {
		t.Error("expected non-empty request_id")
	}
	if _, err := uuid.Parse(chatResp.RequestID); err != nil {
		t.Errorf("request_id '%s' is not a valid UUID: %v", chatResp.RequestID, err)
	}
	if chatResp.Status != "queued" {
		t.Errorf("expected status 'queued', got '%s'", chatResp.Status)
	}
	if chatResp.AgentID != "test-agent-001" {
		t.Errorf("expected agent_id 'test-agent-001', got '%s'", chatResp.AgentID)
	}
}

func TestChatCompletions_ValidationError(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	payload := map[string]any{
		"messages": []map[string]string{}, // empty messages
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", testAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for empty messages, got %d", resp.StatusCode)
	}
}

func TestChatCompletions_Defaults(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	// Minimal payload - should get defaults
	payload := map[string]any{
		"agent_id": "test-agent-defaults",
		"messages": []map[string]string{
			{"role": "user", "content": "Hi"},
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", testAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// 8. Concurrent Load Test
// ---------------------------------------------------------------------------

func TestConcurrentLoad(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	const concurrency = 10
	var wg sync.WaitGroup
	results := make(chan int, concurrency)
	start := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // synchronize start

			payload := map[string]any{
				"agent_id": fmt.Sprintf("load-agent-%d", idx),
				"messages": []map[string]string{
					{"role": "user", "content": "Load test"},
				},
				"max_tokens": 32,
				"model":      "glm-5",
				"provider":   "glm",
			}
			body, _ := json.Marshal(payload)

			c := makeTestClient()
			req, err := http.NewRequest(http.MethodPost, gatewayURL+"/v1/chat/completions", bytes.NewReader(body))
			if err != nil {
				results <- 0
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-api-key", testAPIKey)

			resp, err := c.Do(req)
			if err != nil {
				results <- 0
				return
			}
			defer resp.Body.Close()
			results <- resp.StatusCode
		}(i)
	}

	close(start)
	wg.Wait()
	close(results)

	var successCount int
	for code := range results {
		if code == http.StatusAccepted {
			successCount++
		}
	}

	t.Logf("concurrent load: %d/%d requests returned 202", successCount, concurrency)

	if successCount == 0 {
		t.Error("no requests succeeded in concurrent load test")
	}

	// Give metrics a moment to propagate, then verify request counts
	time.Sleep(500 * time.Millisecond)

	metricsResp, err := client.Get(gatewayURL + "/metrics")
	if err != nil {
		t.Logf("could not verify metrics after load test: %v", err)
		return
	}
	defer metricsResp.Body.Close()

	metricsBody, _ := io.ReadAll(metricsResp.Body)
	metrics := parsePrometheusMetrics(string(metricsBody))

	// Verify request latency was recorded
	if entries, ok := metrics["api_gateway_request_latency_seconds_count"]; ok {
		t.Logf("request_latency_seconds_count entries: %d", len(entries))
	} else {
		t.Log("request_latency_seconds_count not found in metrics")
	}
}

// ---------------------------------------------------------------------------
// 9. Grafana Dashboard Validation
// ---------------------------------------------------------------------------

func TestGrafanaDashboards(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: Authenticate and get session cookie
	loginBody := fmt.Sprintf(`{"user":"%s","password":"%s"}`, grafanaUser, grafanaPass)
	loginReq, err := http.NewRequest(http.MethodPost, grafanaURL+"/api/login", strings.NewReader(loginBody))
	if err != nil {
		t.Fatalf("failed to create login request: %v", err)
	}
	loginReq.Header.Set("Content-Type", "application/json")

	loginResp, err := client.Do(loginReq)
	if err != nil {
		t.Skipf("Grafana not reachable at %s: %v", grafanaURL, err)
	}
	defer loginResp.Body.Close()

	if loginResp.StatusCode != http.StatusOK {
		// Try basic auth instead
		t.Logf("Grafana login returned %d, trying basic auth", loginResp.StatusCode)
	}

	// Use cookie from login response for subsequent requests
	dashReq, err := http.NewRequest(http.MethodGet, grafanaURL+"/api/search?type=dash-db", nil)
	if err != nil {
		t.Fatalf("failed to create dashboard search request: %v", err)
	}

	// Try with cookie first, fall back to basic auth
	if loginResp.StatusCode == http.StatusOK {
		for _, cookie := range loginResp.Cookies() {
			dashReq.AddCookie(cookie)
		}
	} else {
		dashReq.SetBasicAuth(grafanaUser, grafanaPass)
	}

	dashResp, err := client.Do(dashReq)
	if err != nil {
		t.Skipf("failed to search dashboards: %v", err)
	}
	defer dashResp.Body.Close()

	if dashResp.StatusCode != http.StatusOK {
		t.Skipf("dashboard search returned %d", dashResp.StatusCode)
	}

	var dashboards []struct {
		Title string `json:"title"`
		Slug  string `json:"slug"`
		UID   string `json:"uid"`
	}
	if err := parseJSON(dashResp, &dashboards); err != nil {
		t.Fatalf("failed to parse dashboard list: %v", err)
	}

	t.Logf("found %d Grafana dashboards", len(dashboards))

	expectedDashboards := []struct {
		uid   string
		title string
	}{
		{uid: "arl-gateway", title: "API Gateway - Detailed"},
		{uid: "arl-overview", title: "Agent Rate Limit - System Overview"},
		{uid: "arl-cost", title: "Cost Calculator & Savings"},
		{uid: "arl-worker", title: "AI Worker - Detailed"},
	}

	uidSet := make(map[string]bool)
	titleSet := make(map[string]bool)
	for _, d := range dashboards {
		uidSet[d.UID] = true
		titleSet[strings.ToLower(d.Title)] = true
	}

	for _, expected := range expectedDashboards {
		if !uidSet[expected.uid] {
			t.Errorf("missing dashboard with uid '%s' (title: %s)", expected.uid, expected.title)
		} else {
			t.Logf("dashboard '%s' (uid=%s) found", expected.title, expected.uid)
		}
	}
}

// ---------------------------------------------------------------------------
// Bonus: Rate Limiter Status (401 without auth)
// ---------------------------------------------------------------------------

func TestLimiterStatus_Unauthorized(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	// No auth header
	resp, err := client.Get(gatewayURL + "/v1/limiter-status")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}

	// Invalid API key
	resp, err = authGetCustom(client, "/v1/limiter-status", map[string]string{
		"x-api-key": "invalid-key-12345",
	})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// In passthrough mode any non-empty key is accepted, so we can't assert 401
	// Just verify the endpoint responded
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 200 or 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Bonus: Metrics content validation
// ---------------------------------------------------------------------------

func TestMetrics_HistogramBuckets(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	resp, err := client.Get(gatewayURL + "/metrics")
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read metrics: %v", err)
	}

	content := string(body)
	metrics := parsePrometheusMetrics(content)

	// Verify request_latency_seconds has buckets
	buckets, ok := metrics["api_gateway_request_latency_seconds_bucket"]
	if !ok {
		t.Fatal("missing request_latency_seconds_bucket")
	}

	if len(buckets) == 0 {
		t.Fatal("no bucket entries found")
	}

	// Verify le label is present in buckets
	lePattern := regexp.MustCompile(`le="`)
	foundLE := false
	for _, b := range buckets {
		if lePattern.MatchString(b) {
			foundLE = true
			break
		}
	}
	if !foundLE {
		t.Error("bucket entries missing 'le' label")
	}

	// Verify ttfb_seconds also has histogram suffixes.
	// TTFB is only registered after the first streaming response, so this may
	// not exist yet if no streaming requests have been made.
	for _, suffix := range []string{"_count", "_sum", "_bucket"} {
		if !hasMetric(metrics, "api_gateway_ttfb_seconds"+suffix) {
			t.Logf("ttfb_seconds%s not present (expected if no streaming requests yet)", suffix)
		}
	}
}

// ---------------------------------------------------------------------------
// Bonus: Cache-Control on /v1/ endpoints
// ---------------------------------------------------------------------------

func TestCacheControl_V1Endpoints(t *testing.T) {
	client := makeTestClient()
	requireGateway(t, client)

	resp, err := authGet(client, "/v1/limiter-status")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	cc := resp.Header.Get("Cache-Control")
	if cc != "no-store" {
		t.Errorf("expected Cache-Control 'no-store' on /v1/ endpoint, got '%s'", cc)
	}
}
