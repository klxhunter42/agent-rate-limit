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
	"api_gateway_request_latency_seconds":      {"method", "path", "status"},
	"api_gateway_error_total":                  {"type"},
	"api_gateway_rate_limit_hits_total":        {"key"},
	"api_gateway_active_connections":           {},
	"api_gateway_queue_depth":                  {},
	"api_gateway_token_input_total":            {"model"},
	"api_gateway_token_output_total":           {"model"},
	"api_gateway_upstream_retries_total":       {},
	"api_gateway_upstream_429_total":           {},
	"api_gateway_adaptive_limit":               {"model"},
	"api_gateway_adaptive_in_flight":           {"model"},
	"api_gateway_cost_total":                   {"model", "type"},
	"api_gateway_model_fallback_total":         {"requested", "selected"},
	"api_gateway_ttfb_seconds":                 {"model"},
	"api_gateway_go_goroutines":                {},
	"api_gateway_go_heap_alloc_bytes":          {},
	"api_gateway_go_heap_objects":              {},
	"api_gateway_go_gc_pause_ns":               {},
	"api_gateway_go_stack_inuse_bytes":         {},
	"api_gateway_dragonfly_up":                 {},
	"api_gateway_anomaly_total":                {"type", "severity"},
	"api_gateway_mask_duration_seconds":        {"phase"},
	"api_gateway_secrets_detected_total":       {"type"},
	"api_gateway_pii_detected_total":           {"type"},
	"api_gateway_mask_requests_total":          {"has_secrets", "has_pii"},
	"api_gateway_profile_requests_total":       {"profile", "model"},
	"api_gateway_profile_token_input_total":    {"profile", "model"},
	"api_gateway_profile_token_output_total":   {"profile", "model"},
	"api_gateway_profile_cost_total":           {"profile", "model", "type"},
	"api_gateway_optimizer_chars_saved_total":  {"technique", "direction"},
	"api_gateway_optimizer_runs_total":         {"technique"},
	"api_gateway_optimizer_duration_seconds":   {"technique"},
	"api_gateway_optimizer_tokens_saved_total": {"direction"},
	"api_gateway_cost_savings_total":           {},
	"api_gateway_budget_level":                 {"model"},
	"api_gateway_context_truncation_total":     {"model"},
	"api_gateway_transient_retry_total":        {"status", "model"},
	"api_gateway_waste_findings_total":         {"detector", "severity"},
	"api_gateway_waste_tokens_wasted_total":    {"detector"},
	"api_gateway_account_token_input_total":    {"account_id", "model"},
	"api_gateway_account_token_output_total":   {"account_id", "model"},
	"api_gateway_billing_path_requests_total":  {"path"},
	"api_gateway_billing_path_latency_seconds": {"path"},
	// Per-technique metrics from optimizer sub-packages
	"api_gateway_chunker_chars_saved_total":        {},
	"api_gateway_delta_chars_saved_total":          {},
	"api_gateway_disclosure_chars_saved_total":     {},
	"api_gateway_sketch_chars_saved_total":         {},
	"api_gateway_chunker_reorder_duration_seconds": {},
	"api_gateway_delta_encodes_total":              {},
	"api_gateway_sketch_checks_total":              {},
	"api_gateway_waste_scan_duration_seconds":      {},
	// Pordee Thai compression
	"api_gateway_pordee_injections_total":    {"level", "result"},
	"api_gateway_pordee_output_ratio":        {},
	"api_gateway_pordee_duration_seconds":    {},
	// Bandit selection
	"api_gateway_bandit_reward_total":                  {},
	"api_gateway_bandit_selection_duration_seconds":    {},
	// Cache eviction
	"api_gateway_cache_eviction_keys_evicted_total":         {},
	"api_gateway_cache_eviction_pass_duration_seconds":      {},
	"api_gateway_cache_eviction_roi_score":                   {},
	// Caveman compression
	"api_gateway_caveman_compressions_total":            {},
	"api_gateway_caveman_compression_ratio":             {},
	// Chunker
	"api_gateway_chunker_chunks_total":                  {},
	"api_gateway_chunker_cache_hit_rate":                {},
	// Delta
	"api_gateway_delta_savings_pct":                    {},
	// Disclosure
	"api_gateway_disclosure_fts_hit_rate":              {},
	// Image compression
	"api_gateway_image_compressions_total":              {},
	"api_gateway_image_bytes_original_total":            {},
	"api_gateway_image_bytes_saved_total":               {},
	// Packer
	"api_gateway_packer_budget_utilization":             {},
	"api_gateway_packer_tokens_saved_total":             {},
	// Prefetcher
	"api_gateway_prefetcher_order_used":                 {},
	"api_gateway_prefetcher_prewarm_duration_seconds":   {},
	// Sketch
	"api_gateway_sketch_hamming_similarity":             {},
	// Summarizer
	"api_gateway_summarizer_llm_tokens_total":           {},
	// Vision pre-analysis
	"api_gateway_vision_preanalysis_total":              {},
	"api_gateway_vision_preanalysis_duration_seconds":   {},
	// Warmstart
	"api_gateway_warmstart_similarity_score":            {},
	"api_gateway_warmstart_warmup_duration_seconds":     {},
}

// skipMetricsFromCoverage are registered metrics excluded from the
// "every metric must appear in a dashboard" check. These are internal
// runtime/diagnostic metrics that may not warrant a dedicated panel.
var skipMetricsFromCoverage = map[string]string{
	"api_gateway_go_heap_objects":            "internal GC diagnostic, covered by heap_alloc_bytes panel",
	"api_gateway_go_stack_inuse_bytes":       "internal runtime diagnostic, rarely actionable alone",
	"api_gateway_context_truncation_total":   "recovery metric, no dedicated panel yet",
	"api_gateway_transient_retry_total":      "recovery metric, no dedicated panel yet",
	"api_gateway_profile_requests_total":     "GLM mode has no profile concept, metric never populated",
	"api_gateway_profile_token_input_total":  "GLM mode has no profile concept, metric never populated",
	"api_gateway_profile_token_output_total": "GLM mode has no profile concept, metric never populated",
	"api_gateway_profile_cost_total":         "GLM mode has no profile concept, metric never populated",
	"api_gateway_waste_findings_total":       "registered but not incremented until waste detector runs",
	"api_gateway_waste_tokens_wasted_total":  "registered but not incremented until waste detector runs",
	"api_gateway_billing_path_latency_seconds": "covered by billing_path_requests panel",
	"api_gateway_chunker_chars_saved_total":        "covered by aggregate optimizer_chars_saved panel",
	"api_gateway_delta_chars_saved_total":          "covered by aggregate optimizer_chars_saved panel",
	"api_gateway_disclosure_chars_saved_total":     "covered by aggregate optimizer_chars_saved panel",
	"api_gateway_sketch_chars_saved_total":         "covered by aggregate optimizer_chars_saved panel",
	"api_gateway_chunker_reorder_duration_seconds": "covered by optimizer_duration panel",
	"api_gateway_delta_encodes_total":              "covered by optimizer_runs panel",
	"api_gateway_sketch_checks_total":              "covered by optimizer_runs panel",
	"api_gateway_waste_scan_duration_seconds":      "covered by waste detection panel",
	"api_gateway_anomaly_total":               "registered but not incremented until anomaly detector runs",
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
	// metricLabelRe matches a metric name followed by optional {labels}
	metricLabelRe := regexp.MustCompile(`(api_gateway_[a-z_]+\d*[a-z_]*)(\{[^}]*\})?`)

	for _, pair := range allExprs(t) {
		mlMatches := metricLabelRe.FindAllStringSubmatch(pair.expr, -1)
		for _, mlm := range mlMatches {
			m := mlm[1]
			labelClause := mlm[2]

			base := strings.TrimSuffix(m, "_bucket")
			base = strings.TrimSuffix(base, "_count")
			base = strings.TrimSuffix(base, "_sum")
			base = strings.TrimSuffix(base, "_created")

			labels, ok := registeredMetrics[base]
			if !ok {
				labels, ok = registeredMetrics[m]
			}
			if !ok {
				continue // Unknown metric caught by TestDashboardPromQLValidation
			}

			if labelClause == "" {
				continue
			}
			inner := labelClause[1 : len(labelClause)-1] // strip braces
			parts := strings.Split(inner, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				sep := "="
				if strings.Contains(part, "!=") {
					sep = "!="
				}
				kv := strings.SplitN(part, sep, 2)
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
		"api_gateway_optimizer_duration_seconds",
		"api_gateway_optimizer_tokens_saved_total",
		"api_gateway_cost_savings_total",
		"api_gateway_budget_level",
		"api_gateway_context_truncation_total",
		"api_gateway_transient_retry_total",
		"api_gateway_waste_findings_total",
		"api_gateway_waste_tokens_wasted_total",
		"api_gateway_account_token_input_total",
		"api_gateway_account_token_output_total",
		"api_gateway_billing_path_requests_total",
		"api_gateway_billing_path_latency_seconds",
		"api_gateway_chunker_chars_saved_total",
		"api_gateway_delta_chars_saved_total",
		"api_gateway_disclosure_chars_saved_total",
		"api_gateway_sketch_chars_saved_total",
		"api_gateway_chunker_reorder_duration_seconds",
		"api_gateway_delta_encodes_total",
		"api_gateway_sketch_checks_total",
		"api_gateway_waste_scan_duration_seconds",
	// Sub-package metrics
	"api_gateway_bandit_reward_total",
	"api_gateway_bandit_selection_duration_seconds",
	"api_gateway_cache_eviction_keys_evicted_total",
	"api_gateway_cache_eviction_pass_duration_seconds",
	"api_gateway_cache_eviction_roi_score",
	"api_gateway_caveman_compressions_total",
	"api_gateway_caveman_compression_ratio",
	"api_gateway_chunker_chunks_total",
	"api_gateway_chunker_cache_hit_rate",
	"api_gateway_delta_savings_pct",
	"api_gateway_disclosure_fts_hit_rate",
	"api_gateway_image_compressions_total",
	"api_gateway_image_bytes_original_total",
	"api_gateway_image_bytes_saved_total",
	"api_gateway_packer_budget_utilization",
	"api_gateway_packer_tokens_saved_total",
	"api_gateway_prefetcher_order_used",
	"api_gateway_prefetcher_prewarm_duration_seconds",
	"api_gateway_sketch_hamming_similarity",
	"api_gateway_summarizer_llm_tokens_total",
	"api_gateway_vision_preanalysis_total",
	"api_gateway_vision_preanalysis_duration_seconds",
	"api_gateway_warmstart_similarity_score",
	"api_gateway_warmstart_warmup_duration_seconds",
	"api_gateway_pordee_injections_total",
	"api_gateway_pordee_output_ratio",
	"api_gateway_pordee_duration_seconds",
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
		"api_gateway_cost_total":                 {"model", "type"},
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
		"api_gateway_profile_cost_total":         {"profile", "model", "type"},
		"api_gateway_optimizer_chars_saved_total":  {"technique", "direction"},
		"api_gateway_optimizer_runs_total":         {"technique"},
		"api_gateway_optimizer_tokens_saved_total": {"direction"},
		"api_gateway_cost_savings_total":           {},
		"api_gateway_waste_findings_total":         {"detector", "severity"},
		"api_gateway_waste_tokens_wasted_total":    {"detector"},
	}
	for m, wantLabels := range labelChecks {
		gotLabels := registeredMetrics[m]
		if fmt.Sprintf("%v", gotLabels) != fmt.Sprintf("%v", wantLabels) {
			t.Errorf("metric %q labels: got %v, want %v", m, gotLabels, wantLabels)
		}
	}
}
