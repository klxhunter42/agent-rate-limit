package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// registeredMetrics is the canonical set of api_gateway_* metrics registered
// in metrics.go. Update this map when new metrics are added.
var registeredMetrics = map[string][]string{
	"api_gateway_request_latency_seconds":     {"method", "path", "status"},
	"api_gateway_error_total":                 {"type"},
	"api_gateway_rate_limit_hits_total":       {"key"},
	"api_gateway_active_connections":          {},
	"api_gateway_queue_depth":                 {},
	"api_gateway_token_input_total":           {"model"},
	"api_gateway_token_output_total":          {"model"},
	"api_gateway_upstream_retries_total":      {},
	"api_gateway_upstream_429_total":          {},
	"api_gateway_adaptive_limit":              {"model"},
	"api_gateway_adaptive_in_flight":          {"model"},
	"api_gateway_cost_total":                  {"model"},
	"api_gateway_model_fallback_total":        {"requested", "selected"},
	"api_gateway_ttfb_seconds":                {"model"},
	"api_gateway_go_goroutines":               {},
	"api_gateway_go_heap_alloc_bytes":         {},
	"api_gateway_go_heap_objects":             {},
	"api_gateway_go_gc_pause_ns":              {},
	"api_gateway_go_stack_inuse_bytes":        {},
	"api_gateway_dragonfly_up":                {},
	"api_gateway_anomaly_total":               {"type", "severity"},
	"api_gateway_mask_duration_seconds":       {"phase"},
	"api_gateway_secrets_detected_total":      {"type"},
	"api_gateway_pii_detected_total":          {"type"},
	"api_gateway_mask_requests_total":         {"has_secrets", "has_pii"},
	"api_gateway_profile_requests_total":      {"profile", "model"},
	"api_gateway_profile_token_input_total":   {"profile", "model"},
	"api_gateway_profile_token_output_total":  {"profile", "model"},
	"api_gateway_profile_cost_total":          {"profile", "model"},
	"api_gateway_optimizer_chars_saved_total": {"technique"},
	"api_gateway_optimizer_runs_total":        {"technique"},
}

// skipMetricsFromCoverage are registered metrics excluded from the
// "every metric must appear in a dashboard" check. These are internal
// runtime/diagnostic metrics that may not warrant a dedicated panel.
var skipMetricsFromCoverage = map[string]string{
	"api_gateway_go_heap_objects":      "internal GC diagnostic, covered by heap_alloc_bytes panel",
	"api_gateway_go_stack_inuse_bytes": "internal runtime diagnostic, rarely actionable alone",
}

var (
	metricNameRe = regexp.MustCompile(`api_gateway_[a-z_]+\d*[a-z_]*`)
	labelKeyRe   = regexp.MustCompile(`\{([^}]*)\}`)
)

func dashboardsDir() string {
	return filepath.Join("..", "..", "grafana", "provisioning", "dashboards")
}

// loadDashboards reads all .json files from the dashboards directory recursively.
func loadDashboards(t *testing.T) map[string]map[string]any {
	t.Helper()
	dir := dashboardsDir()
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("glob dashboards: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no dashboard JSON files found in ", dir)
	}

	dashboards := make(map[string]map[string]any, len(files))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var db map[string]any
		if err := json.Unmarshal(raw, &db); err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		dashboards[f] = db
	}
	return dashboards
}

// extractPanels recursively collects all panels, expanding row panels.
func extractPanels(obj map[string]any) []map[string]any {
	var out []map[string]any
	raw, ok := obj["panels"]
	if !ok {
		return out
	}
	panels, ok := raw.([]any)
	if !ok {
		return out
	}
	for _, p := range panels {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, pm)
		// Recurse into nested panels (rows with collapsed sub-panels).
		if sub := extractPanels(pm); len(sub) > 0 {
			out = append(out, sub...)
		}
	}
	return out
}

// extractExprs returns all "expr" string values from a panel's targets.
func extractExprs(panel map[string]any) []string {
	var exprs []string
	raw, ok := panel["targets"]
	if !ok {
		return exprs
	}
	targets, ok := raw.([]any)
	if !ok {
		return exprs
	}
	for _, t := range targets {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if expr, _ := tm["expr"].(string); expr != "" {
			exprs = append(exprs, expr)
		}
	}
	return exprs
}

// allExprs walks all dashboards and returns a flat list of (file, expr) pairs.
func allExprs(t *testing.T) []struct {
	file string
	expr string
} {
	t.Helper()
	var out []struct {
		file string
		expr string
	}
	for f, db := range loadDashboards(t) {
		for _, panel := range extractPanels(db) {
			for _, expr := range extractExprs(panel) {
				out = append(out, struct {
					file string
					expr string
				}{f, expr})
			}
		}
	}
	return out
}

// TestDashboardJSONValid verifies each .json file is valid JSON with expected
// Grafana dashboard structure (has "panels" or is a provisioning config).
func TestDashboardJSONValid(t *testing.T) {
	for f, db := range loadDashboards(t) {
		t.Run(filepath.Base(f), func(t *testing.T) {
			if _, hasPanels := db["panels"]; !hasPanels {
				if _, hasRows := db["rows"]; !hasRows {
					t.Errorf("%s: dashboard has neither 'panels' nor 'rows' field", f)
				}
			}
			if title, _ := db["title"].(string); title == "" {
				t.Errorf("%s: dashboard missing 'title'", f)
			}
		})
	}
}

// TestDashboardPromQLValidation checks that every api_gateway_* metric name
// used in dashboard PromQL expressions is registered in the metrics package.
func TestDashboardPromQLValidation(t *testing.T) {
	registered := make(map[string]bool, len(registeredMetrics))
	for m := range registeredMetrics {
		registered[m] = true
	}

	for _, pair := range allExprs(t) {
		matches := metricNameRe.FindAllString(pair.expr, -1)
		for _, m := range matches {
			// Strip common histogram suffixes to get base metric name.
			base := strings.TrimSuffix(m, "_bucket")
			base = strings.TrimSuffix(base, "_count")
			base = strings.TrimSuffix(base, "_sum")
			base = strings.TrimSuffix(base, "_created")

			if !registered[base] && !registered[m] {
				t.Errorf("%s: unknown metric %q in expr: %s", filepath.Base(pair.file), m, pair.expr)
			}
		}
	}
}

// TestLabelValidation verifies that label keys used in PromQL selectors match
// the registered labels for each metric.
func TestLabelValidation(t *testing.T) {
	for _, pair := range allExprs(t) {
		matches := metricNameRe.FindAllString(pair.expr, -1)
		for _, m := range matches {
			base := strings.TrimSuffix(m, "_bucket")
			base = strings.TrimSuffix(base, "_count")
			base = strings.TrimSuffix(base, "_sum")
			base = strings.TrimSuffix(base, "_created")

			labels, ok := registeredMetrics[base]
			if !ok {
				labels, ok = registeredMetrics[m]
			}
			if !ok {
				continue // Unknown metric caught by TestDashboardPromQLValidation.
			}

			// Extract label selectors from the full expression around this metric.
			// Find all label clauses in the expression.
			labelMatches := labelKeyRe.FindAllString(pair.expr, -1)
			for _, lm := range labelMatches {
				inner := lm[1 : len(lm)-1] // strip braces
				parts := strings.Split(inner, ",")
				for _, part := range parts {
					kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
					if len(kv) != 2 {
						continue
					}
					key := strings.TrimSpace(kv[0])
					if !allowedLabel(key, labels) {
						t.Errorf("%s: metric %q uses unknown label %q in expr: %s",
							filepath.Base(pair.file), m, key, pair.expr)
					}
				}
			}
		}
	}
}

func allowedLabel(key string, registered []string) bool {
	// PromQL internal / aggregation labels that are always valid.
	switch key {
	case "le", "instance", "job", "group", "group_left", "group_right":
		return true
	}
	for _, r := range registered {
		if key == r {
			return true
		}
	}
	return false
}

// TestNoMissingMetrics verifies every registered metric appears in at least
// one dashboard panel. Skips internal/diagnostic metrics documented in
// skipMetricsFromCoverage.
func TestNoMissingMetrics(t *testing.T) {
	// Collect all metric names used across all dashboards.
	used := make(map[string]bool)
	for _, pair := range allExprs(t) {
		for _, m := range metricNameRe.FindAllString(pair.expr, -1) {
			base := strings.TrimSuffix(m, "_bucket")
			base = strings.TrimSuffix(base, "_count")
			base = strings.TrimSuffix(base, "_sum")
			base = strings.TrimSuffix(base, "_created")
			used[base] = true
			used[m] = true
		}
	}

	var missing []string
	for m := range registeredMetrics {
		if skipMetricsFromCoverage[m] != "" {
			t.Logf("SKIP %s: %s", m, skipMetricsFromCoverage[m])
			continue
		}
		if !used[m] {
			missing = append(missing, m)
		}
	}

	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("registered metric %q is not used in any dashboard", m)
	}
}

// TestRegisteredMetricsComplete is a safety net that verifies the
// registeredMetrics map in this test file includes every metric name
// listed in the canonical set from the spec.
func TestRegisteredMetricsComplete(t *testing.T) {
	canonical := []string{
		"api_gateway_request_latency_seconds",
		"api_gateway_error_total",
		"api_gateway_rate_limit_hits_total",
		"api_gateway_active_connections",
		"api_gateway_queue_depth",
		"api_gateway_token_input_total",
		"api_gateway_token_output_total",
		"api_gateway_upstream_retries_total",
		"api_gateway_upstream_429_total",
		"api_gateway_adaptive_limit",
		"api_gateway_adaptive_in_flight",
		"api_gateway_cost_total",
		"api_gateway_model_fallback_total",
		"api_gateway_ttfb_seconds",
		"api_gateway_go_goroutines",
		"api_gateway_go_heap_alloc_bytes",
		"api_gateway_go_heap_objects",
		"api_gateway_go_gc_pause_ns",
		"api_gateway_go_stack_inuse_bytes",
		"api_gateway_dragonfly_up",
		"api_gateway_anomaly_total",
		"api_gateway_mask_duration_seconds",
		"api_gateway_secrets_detected_total",
		"api_gateway_pii_detected_total",
		"api_gateway_mask_requests_total",
		"api_gateway_profile_requests_total",
		"api_gateway_profile_token_input_total",
		"api_gateway_profile_token_output_total",
		"api_gateway_profile_cost_total",
		"api_gateway_optimizer_chars_saved_total",
		"api_gateway_optimizer_runs_total",
	}

	for _, m := range canonical {
		if _, ok := registeredMetrics[m]; !ok {
			t.Errorf("canonical metric %q missing from registeredMetrics map", m)
		}
	}
	if len(registeredMetrics) != len(canonical) {
		// Find extras.
		canonicalSet := make(map[string]bool, len(canonical))
		for _, m := range canonical {
			canonicalSet[m] = true
		}
		for m := range registeredMetrics {
			if !canonicalSet[m] {
				t.Errorf("extra metric %q in registeredMetrics not in canonical list", m)
			}
		}
	}

	// Also verify label sets match what's registered in metrics.go.
	labelChecks := map[string][]string{
		"api_gateway_request_latency_seconds":    {"method", "path", "status"},
		"api_gateway_error_total":                {"type"},
		"api_gateway_rate_limit_hits_total":      {"key"},
		"api_gateway_token_input_total":          {"model"},
		"api_gateway_token_output_total":         {"model"},
		"api_gateway_adaptive_limit":             {"model"},
		"api_gateway_adaptive_in_flight":         {"model"},
		"api_gateway_cost_total":                 {"model"},
		"api_gateway_model_fallback_total":       {"requested", "selected"},
		"api_gateway_ttfb_seconds":               {"model"},
		"api_gateway_anomaly_total":              {"type", "severity"},
		"api_gateway_mask_duration_seconds":      {"phase"},
		"api_gateway_secrets_detected_total":     {"type"},
		"api_gateway_pii_detected_total":         {"type"},
		"api_gateway_mask_requests_total":        {"has_secrets", "has_pii"},
		"api_gateway_profile_requests_total":     {"profile", "model"},
		"api_gateway_profile_token_input_total":  {"profile", "model"},
		"api_gateway_profile_token_output_total": {"profile", "model"},
		"api_gateway_profile_cost_total":         {"profile", "model"},
	}
	for m, wantLabels := range labelChecks {
		gotLabels := registeredMetrics[m]
		if fmt.Sprintf("%v", gotLabels) != fmt.Sprintf("%v", wantLabels) {
			t.Errorf("metric %q labels: got %v, want %v", m, gotLabels, wantLabels)
		}
	}
}
