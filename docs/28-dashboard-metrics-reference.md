# 28. Dashboard Metrics Reference

Complete reference for every Grafana dashboard panel: how values are calculated, what metrics they use, and what business decisions they support.

---

## 1. Before vs After Optimization

**All token and cost panels show POST-optimization (billed) values.**

The optimizer pipeline runs BEFORE the request reaches the upstream API. The upstream reports tokens it actually processed (the already-reduced amount). Therefore:

| Panel Type | What It Shows | Source |
|---|---|---|
| Input Tokens / Output Tokens | Tokens billed by upstream (after optimization) | `token_input_total`, `token_output_total` |
| Total Cost | Actual billed cost (after optimization) | `cost_total` |
| Cost Savings | Estimated savings from optimization | Derived: `chars_saved / 4 * blended_rate` |
| Before vs After | "Without Optimization" = tokens + estimated savings, "With Optimization" = actual billed tokens | Derived |

**Relationship:**

```
Total Cost (billed)     = what you actually pay (post-optimization)
Cost Savings            = estimated amount saved by optimization
Cost if no optimizer    = Total Cost (billed) + Cost Savings
```

**Example:**

```
Request comes in: 1000 tokens (original)
Optimizer reduces: -300 tokens
Sent to upstream: 700 tokens (post-optimization)

Upstream reports usage: input=700
  -> token_input_total += 700
  -> cost_total += 700 * price_per_token

Dashboard:
  Total Cost              = $2.10  (billed, post-optimization)
  Cost Savings            = $0.90  (estimated, from 300 tokens saved)
  Cost if no optimizer    = $3.00  ($2.10 + $0.90)
  Service Usage Detail    = $2.10  (same as Total Cost, does NOT subtract Cost Savings)
```

Service Usage Detail total = Total Cost (billed). Cost Savings is NOT subtracted from it.
If the optimizer did not exist, your actual cost would be Total Cost + Cost Savings.

---

## 2. Request Flow and Metric Recording Points

### 2.1 Text Diagram - Request Lifecycle

```
Client Request
     |
     v
[1. PasteGuard] -- mask_requests_total, pii_detected_total, secrets_detected_total, mask_duration_seconds
     |
     v
[2. Optimizer Pipeline] -- optimizer_chars_saved_total, optimizer_runs_total, optimizer_duration_seconds
     |   stages: semantic_dedup -> chunker -> delta -> sketch_dedup -> summarizer -> intent_filter -> textcomp -> caveman
     |   accumulates totalSaved (chars)
     |   at end: tokensSaved = totalSaved / 4
     |           costSavings = tokensSaved * $3/1M (flat rate in code)
     |
     v
[3. Upstream API Call] -- billing_path_requests_total, billing_path_latency_seconds
     |
     v
[4. Response Received] -- token_input_total, token_output_total, cost_total, ttfb_seconds
     |                     request_latency_seconds_count/sum/bucket
     |                     (these are POST-optimization values from upstream usage block)
     |
     v
[5. Response Trim] (non-streaming only) -- optimizer_tokens_saved_total{direction="output"}
     |
     v
[6. Profile/Account Recording] -- profile_requests_total, profile_token_input/output_total, profile_cost_total
                                   account_token_input/output_total
     |
     v
[7. Stream Unmask] -- restores PasteGuard placeholders in SSE chunks
     |
     v
Client Response
```

### 2.2 Metric Dependency Graph

```
                    optimizer_chars_saved_total
                           |
                     (chars / 4)
                           |
                    optimizer_tokens_saved_total    <-- INCOMPLETE (only input, from totalSaved)
                           |
                  (tokens * $3/1M)
                           |
                    cost_savings_total              <-- INCOMPLETE (flat rate)

                    token_input_total  ----+
                    token_output_total --+--|
                                         |  (cost_per_token from pricing map)
                    cost_total  <---------+

                    cost_total{type="input"} / token_input_total  = blended_input_rate
                    blended_input_rate * chars_saved / 4           = dashboard_cost_savings (more accurate)
```

### 2.3 Key Insight: Code vs Dashboard Calculation

| Source | Cost Savings Formula | Accuracy |
|---|---|---|
| Code (`optimizers.go`) | `totalSaved / 4 * $3.0 / 1M` | Flat rate, ignores model mix |
| Dashboard (fixed panels) | `chars_saved / 4 * cost_input / token_input` | Dynamic blended rate, reflects actual model pricing |

The dashboard formula is more accurate because it uses the actual blended rate derived from real cost/token data across all models.

---

## 3. Blended Rate Calculation

Panels that show cost savings use a dynamic blended rate:

```promql
blended_rate = sum(increase(api_gateway_cost_total{type="input"}[$__range]))
             / sum(increase(api_gateway_token_input_total[$__range]))

cost_savings = (sum(increase(api_gateway_optimizer_chars_saved_total{direction="input"}[$__range])) / 4)
             * blended_rate
```

This means:
- If most traffic is Opus ($15/1M input), blended rate is high, savings are larger
- If most traffic is Haiku ($0.8/1M input), blended rate is low, savings are smaller
- The `+ 1` in some expressions prevents division by zero when no tokens recorded

---

## 4. Metric Source Reference (43 metrics)

### 4.1 Request Lifecycle Metrics

| Metric | Type | Labels | Records At | Meaning |
|---|---|---|---|---|
| `request_latency_seconds` | histogram | method, path, status | Middleware (every request) | HTTP request duration |
| `active_connections` | gauge | - | Middleware (inc/dec) | Current in-flight connections |
| `queue_depth` | gauge | - | On Prometheus scrape | Current AI job queue depth |
| `error_total` | counter | type | `IncError()` | Errors by type |
| `rate_limit_hits_total` | counter | key | `IncRateLimit()` | Rate-limited requests by key (SHA1 hashed) |
| `upstream_429_total` | counter | - | `Inc429()` | 429 responses from upstream |
| `upstream_retries_total` | counter | - | `IncRetry()` | Retry attempts on 429 |
| `ttfb_seconds` | histogram | model | `RecordTTFB()` | Time to first byte (streaming) |
| `context_truncation_total` | counter | model | `IncContextTruncation()` | Auto-truncation recovery attempts |
| `transient_retry_total` | counter | status, model | `IncTransientRetry()` | Transient error retries |
| `model_fallback_total` | counter | requested, selected | `RecordFallback()` | Model fallback routing events |

### 4.2 Token and Cost Metrics (POST-optimization)

| Metric | Type | Labels | Records At | Meaning |
|---|---|---|---|---|
| `token_input_total` | counter | model | `RecordTokens()` | Input tokens billed by upstream |
| `token_output_total` | counter | model | `RecordTokens()` | Output tokens billed by upstream |
| `cost_total` | counter | model, type | `RecordTokens()` | Estimated cost USD (tokens * pricing) |
| `adaptive_limit` | gauge | model | `UpdateAdaptiveMetrics()` | Current adaptive concurrency limit |
| `adaptive_in_flight` | gauge | model | `UpdateAdaptiveMetrics()` | Current in-flight requests per model |
| `budget_level` | gauge | model | `SetBudgetLevel()` | Budget utilization (0=green, 1=yellow, 2=red) |

### 4.3 Profile and Account Metrics

| Metric | Type | Labels | Records At | Meaning |
|---|---|---|---|---|
| `profile_requests_total` | counter | profile, model | `RecordProfileUsage()` | Requests per profile per model |
| `profile_token_input_total` | counter | profile, model | `RecordProfileUsage()` | Input tokens per profile per model |
| `profile_token_output_total` | counter | profile, model | `RecordProfileUsage()` | Output tokens per profile per model |
| `profile_cost_total` | counter | profile, model, type | `RecordProfileUsage()` | Cost per profile per model |
| `account_token_input_total` | counter | account_id, model | `RecordAccountUsage()` | Input tokens per account per model |
| `account_token_output_total` | counter | account_id, model | `RecordAccountUsage()` | Output tokens per account per model |

### 4.4 Optimizer Metrics (savings estimation)

| Metric | Type | Labels | Records At | Meaning |
|---|---|---|---|---|
| `optimizer_chars_saved_total` | counter | technique, direction | `RecordOptimization()` | Characters saved per technique (source of truth) |
| `optimizer_runs_total` | counter | technique | `RecordOptimization()` | Number of optimization runs |
| `optimizer_duration_seconds` | histogram | technique | `RecordOptimizationDuration()` | Duration of each technique |
| `optimizer_tokens_saved_total` | counter | direction | `RecordTokensSaved()` | Estimated tokens saved (chars/4, input only) |
| `cost_savings_total` | counter | - | `RecordCostSavings()` | Estimated cost savings (flat $3/1M) |
| `profile_optimizer_chars_saved_total` | counter | profile, technique | `RecordProfileOptimization()` | Chars saved per profile |

### 4.5 Per-Technique Metrics (aliases)

These are recorded alongside `optimizer_chars_saved_total`:

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `chunker_chars_saved_total` | counter | technique | Chars saved by chunker |
| `delta_chars_saved_total` | counter | technique | Chars saved by delta encoding |
| `disclosure_chars_saved_total` | counter | technique | Chars saved by disclosure compression |
| `sketch_chars_saved_total` | counter | technique | Chars saved by sketch dedup |

### 4.6 Privacy and Security Metrics

| Metric | Type | Labels | Records At | Meaning |
|---|---|---|---|---|
| `mask_requests_total` | counter | has_secrets, has_pii | PasteGuard pipeline | Requests processed by PasteGuard |
| `pii_detected_total` | counter | type | PasteGuard pipeline | PII entities detected by type |
| `secrets_detected_total` | counter | type | PasteGuard pipeline | Secrets detected by type |
| `mask_duration_seconds` | histogram | phase | PasteGuard pipeline | Duration of masking phases |

### 4.7 Billing and OAuth Metrics

| Metric | Type | Labels | Records At | Meaning |
|---|---|---|---|---|
| `billing_path_requests_total` | counter | path, model, profile | `RecordBillingPath()` | Claude OAuth billing path routing |
| `billing_path_latency_seconds` | histogram | path, model | `RecordBillingPathLatency()` | Latency of billing path calls |

### 4.8 Vision and MCP Metrics

| Metric | Type | Labels | Records At | Meaning |
|---|---|---|---|---|
| `image_compressions_total` | counter | model | `RecordImageCompression()` | Images compressed |
| `image_bytes_saved_total` | counter | model | `RecordImageCompression()` | Bytes saved by compression |
| `image_bytes_original_total` | counter | model | `RecordImageCompression()` | Original image bytes |
| `vision_preanalysis_total` | counter | status | `RecordVisionPreAnalysis()` | Vision pre-analysis results |
| `vision_preanalysis_duration_seconds` | histogram | - | `RecordVisionPreAnalysis()` | Pre-analysis duration |
| `mcp_calls_total` | counter | server, tool, status | MCP handler | MCP proxy calls |
| `mcp_call_duration_seconds` | histogram | server, tool | MCP handler | MCP call duration |
| `mcp_cache_hits_total` | counter | server, tool | MCP handler | MCP cache hits |
| `mcp_cache_misses_total` | counter | server, tool | MCP handler | MCP cache misses |
| `mcp_quota_usage` | gauge | account_id | MCP handler | MCP quota usage per account |

### 4.9 Waste Detection Metrics

| Metric | Type | Labels | Records At | Meaning |
|---|---|---|---|---|
| `waste_findings_total` | counter | detector, severity | `IncWasteFinding()` | Waste findings detected |
| `waste_tokens_wasted_total` | counter | detector | `RecordWasteTokens()` | Tokens identified as wasted |

---

## 5. Per-Dashboard Panel Reference

### 5.1 Service Dashboard (`arl-service-dashboard`)

Executive overview of AI platform usage and costs.

| Panel | Type | Formula | Business Meaning |
|---|---|---|---|
| API Calls | stat | `sum(increase(request_latency_seconds_count[$range]))` | Total requests processed |
| Total Cost | stat | `sum(increase(cost_total[$range]))` | Actual billed cost (post-optimization) |
| Cost Savings | stat | `sum(increase(optimizer_chars_saved_total{direction="input"}[$range])) / 4 * sum(increase(cost_total{type="input"}[$range])) / (sum(increase(token_input_total[$range])) + 1)` | Estimated savings using dynamic blended rate |
| Optimization Rate | gauge | `sum(increase(optimizer_chars_saved_total{direction="input"}[$range])) / 4 / (sum(increase(token_input_total[$range])) + 1) * 100` | % of input tokens saved by optimizer |
| API Units Processed | stat | `sum(increase(token_input_total[$range]))` | Total input tokens billed |
| API Units Generated | stat | `sum(increase(token_output_total[$range]))` | Total output tokens billed |
| Before vs After (Without) | bargauge | `sum(increase(token_input_total[$range]))` | Hypothetical tokens if no optimization |
| Before vs After (With) | bargauge | `sum(increase(token_input_total[$range])) - sum(increase(optimizer_chars_saved_total{direction="input"}[$range])) / 4` | Actual billed tokens (with optimization) |
| Cost Over Time by Service | timeseries | `sum by (model) (increase(cost_total[$interval]))` | Cost trend per model |
| API Usage Trend | timeseries | `sum by (model) (increase(token_input_total[$interval]))` | Token usage trend per model |
| Top 10 Services by Usage | barchart | `sort_desc(topk(10, sum by (model) (increase(token_input_total[$range])) + sum by (model) (increase(token_output_total[$range]))))` | Which models consume most tokens |
| Top 10 Profile Usage | barchart | `sort_desc(topk(10, sum by (profile) (increase(profile_token_input_total[$range])) + sum by (profile) (increase(profile_token_output_total[$range]))))` | Which profiles consume most tokens |
| Cost by AI Model | piechart | `sum by (model) (increase(cost_total[$range]))` | Cost distribution across models |
| API Calls by Service | timeseries | `sum(increase(request_latency_seconds_count[$interval]))` | Request volume over time |
| Savings Over Time | timeseries | `sum(rate(optimizer_chars_saved_total{direction="input"}[$interval])) / 4 * sum(rate(cost_total{type="input"}[$interval])) / (sum(rate(token_input_total[$interval])) + 1)` | Rate of cost savings ($/s) |
| Service Usage Detail - Requests | table | `sum(increase(request_latency_seconds_count[$range]))` | Total requests in period |
| Service Usage Detail - Input | table | `sum by (model) (increase(token_input_total[$range]))` | Input tokens per model |
| Service Usage Detail - Output | table | `sum by (model) (increase(token_output_total[$range]))` | Output tokens per model |
| Service Usage Detail - Cost | table | `sum by (model) (increase(cost_total[$range]))` | Cost per model |

### 5.2 Token Optimization Dashboard (`arl-token-optimization`)

Detailed optimizer performance and savings breakdown.

| Panel | Type | Formula | Business Meaning |
|---|---|---|---|
| Input Tokens Optimized | stat | `sum(increase(optimizer_chars_saved_total{direction="input"}[$range])) / 4` | Estimated input tokens saved |
| Output Tokens Optimized | stat | `sum(increase(optimizer_chars_saved_total{direction="output"}[$range])) / 4` | Estimated output tokens saved |
| Cost Savings (USD) | stat | `sum(increase(optimizer_chars_saved_total{direction="input"}[$range])) / 4 * sum(increase(cost_total{type="input"}[$range])) / (sum(increase(token_input_total[$range])) + 1)` | Dollar savings using blended rate |
| Optimization Runs | stat | `sum(increase(optimizer_runs_total[$range]))` | Total optimizer invocations |
| Optimization Efficiency | gauge | `sum(increase(optimizer_chars_saved_total{direction="input"}[$range])) / 4 / (sum(increase(token_input_total[$range])) + 1) * 100` | % reduction achieved |
| Avg Duration (ms) | stat | `sum(rate(chunker_reorder_duration_seconds_sum[$rate])) / sum(rate(chunker_reorder_duration_seconds_count[$rate])) * 1000` | Average optimizer latency |
| Units Saved Over Time | timeseries | `sum by (technique) (rate({chunker,delta,disclosure,sketch}_chars_saved_total[$interval]))` | Savings rate per technique |
| Cost Savings Over Time | timeseries | `sum(rate(optimizer_chars_saved_total{direction="input"}[$interval])) / 4 * sum(rate(cost_total{type="input"}[$interval])) / (sum(rate(token_input_total[$interval])) + 1)` | Cost savings rate ($/s) |
| Chars Saved by Technique | timeseries | `sort_desc(topk(10, sum by (technique) (increase(optimizer_chars_saved_total{direction="input"}[$range]))))` | Which technique saves most |
| Savings by Technique | barchart | `sum by (technique) (increase(optimizer_chars_saved_total{technique!="sketch_dedup",direction="input"}[$range]))` | Technique comparison (excludes sketch_dedup internal) |
| Runs by Technique | timeseries | `sum by (technique) (rate(optimizer_runs_total[$interval]))` | How often each technique runs |
| Before vs After (Without) | bargauge | `sum(increase(token_input_total[$range]))` | Tokens without optimization |
| Before vs After (With) | bargauge | `sum(increase(token_input_total[$range])) - sum(increase(optimizer_chars_saved_total{direction="input"}[$range])) / 4` | Tokens with optimization |
| Optimization Rate Over Time | timeseries | `sum(rate(optimizer_chars_saved_total{direction="input"}[$interval])) / 4 / (sum(rate(token_input_total[$interval])) + 1) * 100` | Optimization % over time |
| Budget Level | gauge | `avg(budget_level)` | Average budget utilization (0-2) |
| Waste Detection | timeseries | `sum(rate(waste_findings_total[$interval]))` | Waste findings rate |
| Wasted Units by Detector | timeseries | `sum by (detector) (increase(waste_tokens_wasted_total[$range]))` | Wasted tokens by detector type |
| Technique Performance - Chars | table | `sum by (technique) (increase(optimizer_chars_saved_total{technique!="sketch_dedup",direction="input"}[$range]))` | Per-technique char savings |
| Technique Performance - Runs | table | `sum by (technique) (increase(optimizer_runs_total[$range]))` | Per-technique run count |

### 5.3 Gateway Overview (`arl-gw-overview`)

Technical gateway metrics: traffic, latency, concurrency, errors.

| Panel | Type | Formula | Business Meaning |
|---|---|---|---|
| Request Rate by Path | timeseries | `sum by (path, method, status) (rate(request_latency_seconds_count[$interval]))` | Traffic distribution across endpoints |
| Latency Percentiles (p50/p95/p99) | timeseries | `histogram_quantile(0.XX, sum by (le) (rate(request_latency_seconds_bucket[$interval])))` | Latency SLA tracking |
| Avg Latency by Path | timeseries | `sum by (path) (rate(request_latency_seconds_sum[$interval])) / sum by (path) (rate(request_latency_seconds_count[$interval]))` | Which paths are slow |
| Active Connections | timeseries | `active_connections` | Current connection load |
| Input Units by Model | timeseries | `rate(token_input_total[$interval])` | Input token rate per model |
| Output Units by Model | timeseries | `rate(token_output_total[$interval])` | Output token rate per model |
| Upstream 429 Rate | timeseries | `sum(rate(upstream_429_total[$interval]))` | Rate limiting pressure |
| Concurrency Limit per Model | timeseries | `adaptive_limit` | Current adaptive limits |
| In-Flight vs Limit per Model | timeseries | `adaptive_in_flight` vs `adaptive_limit` | How close to limits |
| Upstream 429s & Retries | timeseries | `rate(upstream_429_total[$interval])` + `rate(upstream_retries_total[$interval])` | Rate limiting + retry activity |
| Errors by Type | timeseries | `sum by (type) (rate(error_total[$interval]))` | Error breakdown |
| Error Rate % | timeseries | `sum(rate(upstream_429_total[$interval])) / sum(rate(request_latency_seconds_count[$interval])) * 100` | Error percentage |
| Rate Limit Hits by Key | timeseries | `sum by (key) (rate(rate_limit_hits_total[$interval]))` | Which agents hit limits |
| Upstream Retry Rate | timeseries | `sum(rate(upstream_retries_total[$interval]))` | Retry pressure |
| TTFB (p50/p95) | timeseries | `histogram_quantile(0.XX, sum by (le) (rate(ttfb_seconds_bucket[$interval])))` | Streaming responsiveness |
| Queue Depth | timeseries | `queue_depth` | Job queue backlog |
| Requests by Status | timeseries | `sum by (status) (rate(request_latency_seconds_count{path!~"/health\|/metrics"}[$interval]))` | HTTP status distribution |
| API Units by Model | timeseries | `sum by (model) (rate(token_input_total[$interval]))` + `sum by (model) (rate(token_output_total[$interval]))` | Token throughput per model |
| Cost by Model | timeseries | `sum by (model) (rate(cost_total[$interval]))` | Cost rate per model |
| Characters Saved by Technique | timeseries | `sum by (technique) (rate({chunker,delta,disclosure,sketch}_chars_saved_total[$interval]))` | Per-technique savings rate |
| Optimization Runs | timeseries | `sum by (technique) (rate(optimizer_runs_total[$interval]))` | Technique execution rate |
| Total Characters Saved | stat | `sum(increase(chunker_chars_saved_total[$range])) + sum(increase(delta_chars_saved_total[$range])) + sum(increase(disclosure_chars_saved_total[$range])) + sum(increase(sketch_chars_saved_total[$range]))` | Cumulative chars saved |
| Avg Savings Per Run | stat | `total_chars_saved / total_runs` | Efficiency per optimization |
| Input Tokens Total | stat | `sum by (model) (increase(token_input_total[$range]))` | Cumulative input tokens per model |
| Output Tokens Total | stat | `sum by (model) (increase(token_output_total[$range]))` | Cumulative output tokens per model |
| Model Fallbacks | timeseries | `sum by (requested, fallback, reason) (rate(model_fallback_total[$interval]))` | Model routing changes |

### 5.4 Privacy & Compliance Dashboard (`arl-pasteguard`)

PasteGuard PII/secrets detection and masking.

| Panel | Type | Formula | Business Meaning |
|---|---|---|---|
| Total Requests Protected | stat | `sum(increase(mask_requests_total[$range]))` | Total requests through PasteGuard |
| Secrets Blocked | stat | `sum(increase(pii_detected_total[$range]))` | Total secrets/PII detections |
| PII Masked | stat | `sum(increase(pii_detected_total[$range]))` | Total PII instances masked |
| Protection Coverage | stat | `(sum(mask_requests_total{has_secrets="true"}) + sum(mask_requests_total{has_pii="true"})) / sum(mask_requests_total) * 100` | % of requests containing sensitive data |
| Protection Activity | timeseries | `sum(rate(mask_requests_total[$interval]))` split by has_secrets/has_pii | Detection rate over time |
| Detection Rate by Type | timeseries | `sum by (type) (rate(pii_detected_total[$interval]))` | Which entity types detected most |
| Top 10 Secrets by Type | barchart | `topk(10, sum by (type) (increase(pii_detected_total[$range])))` | Most common secret types |
| Top 10 PII by Type | barchart | `topk(10, sum by (type) (increase(pii_detected_total[$range])))` | Most common PII types |
| Detection Distribution | piechart | `sum(increase(pii_detected_total[$range]))` | Secrets vs PII split |
| Mask Duration P95 | timeseries | `histogram_quantile(0.95, sum by (le, phase) (rate(mask_duration_seconds_bucket[$interval])))` | Masking latency |
| Processing Health | gauge | `histogram_quantile(0.95, sum by (le) (rate(mask_duration_seconds_bucket[$rate])))` | Overall P95 latency |
| Clean Requests | stat | `total - has_secrets - has_pii` | Requests with no sensitive data |
| Protected Requests | stat | `has_secrets + has_pii` | Requests that needed masking |
| Secrets Detection Trend | timeseries | `sum(rate(secrets_detected_total[$interval]))` | Secret detection rate |
| Detection Summary | table | `sum by (type) (increase(pii_detected_total[$range]))` | Per-type detection counts |

### 5.5 Cost Calculator (`arl-cost`)

Cost estimation and billing analysis.

| Panel | Type | Formula | Business Meaning |
|---|---|---|---|
| Actual Cost | stat | `sum(increase(cost_total[$range]))` | Billed cost from gateway pricing |
| Est. Cost from API Usage | stat | Manual per-model calculation: `tokens * price_per_1M / 1M` for each model | Independent cost verification |
| Input Units | stat | `sum(increase(token_input_total[$range]))` | Total input tokens |
| Output Units | stat | `sum(increase(token_output_total[$range]))` | Total output tokens |
| Cost Trend (Hourly) | timeseries | `sum by (model) (increase(cost_total[1h]))` | Hourly cost per model |
| Cost by Model | barchart | `sum by (model) (increase(cost_total[$range]))` | Cost distribution by model |
| API Usage by Model | barchart | `sum by (model) (increase(token_input_total[$range]))` + output | Token usage by model |
| Total Requests (Sync) | stat | `sum(increase(request_latency_seconds_count{path="/v1/messages"}[$range]))` | Synchronous requests |
| Total Requests (Async) | stat | `sum(increase(request_latency_seconds_count[$range]))` | All requests |
| Billing Path Distribution | barchart | `sum by (path) (increase(billing_path_requests_total[$range]))` | Claude OAuth path routing |

### 5.6 Claude OAuth Billing Path (`claude-oauth-billing`)

Claude OAuth billing header injection and routing.

| Panel | Type | Formula | Business Meaning |
|---|---|---|---|
| Billing Path Distribution | piechart | `sum by (path) (increase(billing_path_requests_total[$range]))` | go_direct vs sidecar vs rejected |
| Request Rate by Path | timeseries | `sum by (path) (rate(billing_path_requests_total[$interval]))` | Path routing rate |
| Go Direct | stat | `sum(increase(billing_path_requests_total{path="go_direct"}[$range]))` | Direct billing requests |
| Sidecar | stat | `sum(increase(billing_path_requests_total{path="sidecar"}[$range]))` | Sidecar proxy requests |
| Rejected | stat | `sum(increase(billing_path_requests_total{path="billing_rejected"}[$range]))` | Rejected billing requests |
| Go Direct Success Rate | gauge | `go_direct / total * 100` | % of requests using optimal path |
| Billing Path Latency (p50/p95/p99) | timeseries | `histogram_quantile(0.XX, sum by (le, path) (rate(billing_path_latency_seconds_bucket[$interval])))` | Latency per billing path |
| API Usage by Profile (input) | timeseries | `sum by (profile) (rate(profile_token_input_total[$interval]))` | Token usage per OAuth profile |
| API Usage by Profile (output) | timeseries | `sum by (profile) (rate(profile_token_output_total[$interval]))` | Output tokens per profile |
| 429 Rate Limit Hits | timeseries | `rate(upstream_429_total[$interval])` | Rate limiting on OAuth |
| Upstream Retries | timeseries | `rate(upstream_retries_total[$interval])` | Retry activity |
| TTFB by Model (p50/p95) | timeseries | `histogram_quantile(0.XX, sum by (le, model) (rate(ttfb_seconds_bucket[$interval])))` | Streaming latency per model |
| Profile Cost (USD) | timeseries | `sum by (profile, model) (rate(profile_cost_total[$interval])) * 3600` | Hourly cost per profile |
| Cost by Model | timeseries | `sum by (profile, model) (rate(profile_cost_total[$interval])) * 3600` | Hourly cost per model |

### 5.7 Gateway Runtime & Health (`arl-gw-runtime`)

Go runtime metrics and system health.

| Panel | Type | Formula | Business Meaning |
|---|---|---|---|
| Goroutines | timeseries | `go_goroutines` | Go scheduler pressure |
| Memory (heap + stack) | timeseries | `go_heap_alloc_bytes` + `go_stack_inuse_bytes` | Memory consumption |
| GC Pause | timeseries | `go_gc_pause_ns` | GC pressure indicator |
| Heap Objects | timeseries | `go_heap_objects` | Heap allocation count |
| Dragonfly Health | stat | `dragonfly_up` | Dragonfly availability (0/1) |
| Waste Detection Findings | timeseries | `sum by (detector, severity) (rate(waste_findings_total[$interval]))` | Waste findings breakdown |

### 5.8 System Overview (`arl-overview`)

High-level system architecture health.

| Panel | Type | Formula | Business Meaning |
|---|---|---|---|
| Dragonfly Health | stat | `dragonfly_up` | Cache availability |
| Connections | stat | `active_connections` | Current connections |
| Queue Depth (Gateway) | stat | `queue_depth` | Gateway job queue |
| Queue Depth (Worker) | stat | `ai_worker_queue_depth` | Worker job queue |
| Workers | stat | `ai_worker_active` | Active Python workers |
| Rate Limit Hits | stat | `sum(increase(upstream_429_total[$range]))` | Total 429s received |
| Error Rate | stat | `sum(rate(upstream_429_total[$rate])) / sum(rate(request_latency_seconds_count[$rate]))` | Error percentage |
| Request Rate | timeseries | `sum(rate(request_latency_seconds_count[$interval]))` | Request throughput |
| Job Rate | timeseries | `rate(request_latency_seconds_count[$interval])` vs `rate(ai_worker_jobs_failed_total[$interval])` | Success vs failure rate |
| Latency p95 | timeseries | `histogram_quantile(0.95, sum by (le) (rate(request_latency_seconds_bucket[$interval])))` | P95 latency |
| 429s & Retries | timeseries | `rate(upstream_429_total[$interval])` + `rate(upstream_retries_total[$interval])` | Rate limit pressure |

### 5.9 AI Worker Detailed (`arl-worker`)

Python worker job processing metrics.

| Panel | Type | Formula | Business Meaning |
|---|---|---|---|
| Jobs Processed/Failed/Retried | timeseries | Rate of each job type | Worker throughput |
| Total Jobs | stat | `sum(increase(...))` for each type | Cumulative job counts |
| Queue Depth Over Time | timeseries | `ai_worker_queue_depth` | Job backlog |
| Active Workers | gauge | `ai_worker_active` | Current active workers |
| Error Rate % | gauge | `rate(failed) / rate(processed) * 100` | Worker failure percentage |
| Rate Limit Hits | stat | `sum(increase(upstream_429_total[$range]))` | Upstream rate limiting |
| Total Provider Errors | stat | `sum(increase(ai_worker_jobs_failed_total[$range]))` | Total provider errors |
| API Usage Input by Model | timeseries | `sum by (model) (rate(token_input_total[$interval]))` | Input tokens per model |
| API Usage Output by Model | timeseries | `sum by (model) (rate(token_output_total[$interval]))` | Output tokens per model |
| Provider Latency (p50/p95/p99) | timeseries | `histogram_quantile(0.XX, sum by (le, path) (rate(request_latency_seconds_bucket[$interval])))` | Provider response latency |
| Worker Memory (RSS/Virtual) | timeseries | `process_resident/virtual_memory_bytes{job="ai-worker"}` | Worker memory usage |
| Worker CPU | timeseries | `rate(process_cpu_seconds_total{job="ai-worker"}[$interval])` | Worker CPU usage |
| Provider Errors by Provider | timeseries | `rate(ai_worker_jobs_failed_total[$interval])` + `rate(upstream_429_total[$interval])` | Error rate by source |
| Account Input Tokens | timeseries | `sum by (account_id, model) (rate(account_token_input_total[$interval]))` | Per-account input usage |
| Account Output Tokens | timeseries | `sum by (account_id, model) (rate(account_token_output_total[$interval]))` | Per-account output usage |

### 5.10 API Usage Monitor (`arl-token-usage`)

Detailed token usage by model, profile, and account.

| Panel | Type | Formula | Business Meaning |
|---|---|---|---|
| Total Input Units | stat | `sum(increase(token_input_total[$range]))` | Cumulative input tokens |
| Total Output Units | stat | `sum(increase(token_output_total[$range]))` | Cumulative output tokens |
| Total Requests | stat | `sum(increase(request_latency_seconds_count[$range]))` | Total requests |
| Total Cost (USD) | stat | `sum(increase(cost_total[$range]))` | Total billed cost |
| Input Units by Model | timeseries | `sum by (model) (increase(token_input_total[$interval]))` | Token trend per model |
| Output Units by Model | timeseries | `sum by (model) (increase(token_output_total[$interval]))` | Output trend per model |
| Usage by Model (table) | table | `sum by (model) (increase(token_input/output_total[$range]))` | Cumulative per-model breakdown |
| Input Units by Profile | timeseries | `sum by (profile) (increase(profile_token_input_total[$interval]))` | Per-profile input trend |
| Output Units by Profile | timeseries | `sum by (profile) (increase(profile_token_output_total[$interval]))` | Per-profile output trend |
| Usage by Profile+Model (table) | table | `sum by (profile, model) (increase(profile_token_input/output_total[$range]))` | Cross-reference table |
| Input Units by Account | timeseries | `sum by (account_id) (increase(account_token_input_total[$interval]))` | Per-account input trend |
| Output Units by Account | timeseries | `sum by (account_id) (increase(account_token_output_total[$interval]))` | Per-account output trend |
| Usage by Account+Model (table) | table | `sum by (account_id, model) (increase(account_token_input/output_total[$range]))` | Account-level breakdown |
| Input Unit Rate by Model | timeseries | `sum by (model) (rate(token_input_total[$interval]))` | Token throughput per model |
| Output Unit Rate by Model | timeseries | `sum by (model) (rate(token_output_total[$interval]))` | Output throughput per model |
| API Unit Rate by Account | timeseries | `sum by (account_id) (rate(account_token_input/output_total[$interval]))` | Account-level throughput |

---

## 6. Cost Calculation Details

### 6.1 How `cost_total` is Calculated

In `metrics.go`, `RecordTokens()` computes cost from the configured pricing map:

```go
// Simplified logic:
inputCost  = inputTokens * modelInputPrice  / 1_000_000
outputCost = outputTokens * modelOutputPrice / 1_000_000
cost_total{type="input"}  += inputCost
cost_total{type="output"} += outputCost
```

Pricing comes from the `MODEL_PRICING` env var (see `docker-compose.yml`):
```
glm-5.1:1.4:4.4,glm-5-turbo:1.2:4.0,glm-5:1.0:3.2,
glm-4.7:0.6:2.2,glm-4.6:0.6:2.2,glm-4.5:0.6:2.2,
claude-opus-4-7:15:75,claude-sonnet-4-6:3:15,
claude-haiku-4-5-20251001:0.8:4
```

Format: `model:input_price:output_price` (per million tokens, USD).

### 6.2 How Cost Savings is Calculated

**In code (optimizers.go):**
```go
tokensSaved := float64(totalSaved) / 4.0    // chars to estimated tokens
costSavings := tokensSaved * 3.0 / 1_000_000 // flat $3/1M rate
```

**In dashboard (more accurate):**
```promql
blended_rate = sum(cost_total{type="input"}) / sum(token_input_total)
cost_savings = (chars_saved / 4) * blended_rate
```

### 6.3 Optimizer Pipeline Stages

| Stage | Order | Condition | Records |
|---|---|---|---|
| `semantic_dedup` | 1 | Always | chars saved if dedup found |
| `chunker` | 2 | Always | chars saved by chunk+reorder |
| `delta` | 3 | Always | chars saved by delta encoding |
| `sketch_dedup` | 4 | Always | chars saved by sketch dedup |
| `summarizer` | 5 | budgetLevel >= 2 (red) only | chars saved by summarization |
| `intent_filter` | 6 | Always | chars saved by intent filtering |
| `textcomp_sys` | 7 | Always (if TEXTCOMP_ENABLED) | chars saved by text compression |
| `caveman_input` | 8 | Always | chars saved by caveman compression |
| `caveman_output` | 8b | Always | chars ADDED (not saved, not in totalSaved) |

After all stages:
- `totalSaved` = sum of chars saved across all stages (excluding caveman_output)
- `RecordTokensSaved(totalSaved / 4, "input")`
- `RecordCostSavings(totalSaved / 4 * $3 / 1M)`

---

## 7. Common Patterns and Gotchas

### 7.1 `increase()` vs `rate()`

- `increase(metric[$__range])` = total growth over the selected time range (stat panels)
- `rate(metric[$__interval])` = per-second rate (timeseries panels)
- `$__range` = full dashboard time range (e.g., "Last 24 hours")
- `$__interval` = auto-calculated per-point interval for timeseries resolution

### 7.2 Counter Reset Behavior

Go in-memory counters reset to 0 on process restart. `increase()` and `rate()` handle this correctly (Prometheus detects counter resets). However, the first data point after restart may show a spike or dip.

### 7.3 `or on() vector(0)` Pattern

Many expressions use `... or on() vector(0)` to show 0 instead of "No data" when the metric doesn't exist yet. This is standard practice for dashboards that need clean zeros.

### 7.4 `+ 1` in Denominators

Expressions like `sum(increase(token_input_total[$range])) + 1` prevent division by zero. The `+ 1` has negligible impact on large numbers but prevents NaN display when no traffic has occurred.

### 7.5 Per-Technique vs Aggregate Metrics

- `optimizer_chars_saved_total{technique="chunker",direction="input"}` = per-technique, always accurate
- `optimizer_tokens_saved_total{direction="input"}` = aggregate from totalSaved, only records at end of pipeline
- `cost_savings_total` = uses flat $3/1M rate, less accurate than dashboard blended rate

Always prefer `optimizer_chars_saved_total` as the source of truth for savings calculations.

---

## 8. Answering Business Questions

### Q: "How much are we spending on AI?"
**A:** Service Dashboard -> "Total Cost" panel. This is actual billed cost (post-optimization).

### Q: "How much are we saving with the optimizer?"
**A:** Service Dashboard -> "Cost Savings" panel. Uses dynamic blended rate for accuracy.

### Q: "Which model is most expensive?"
**A:** Service Dashboard -> "Cost by AI Model" pie chart. Shows cost distribution.

### Q: "Which optimization technique is most effective?"
**A:** Token Optimization Dashboard -> "Savings by Technique" bar chart.

### Q: "Are we hitting rate limits?"
**A:** Gateway Overview -> "Upstream 429 Rate" and "Upstream Retry Rate" panels.

### Q: "Is the optimizer making things faster or slower?"
**A:** Token Optimization Dashboard -> "Avg Duration (ms)" for optimizer overhead vs token savings ratio.

### Q: "Which profile/account is consuming the most?"
**A:** API Usage Monitor -> per-profile and per-account tables. Also Service Dashboard -> "Top 10 Profile Usage".

### Q: "Are we leaking secrets or PII?"
**A:** Privacy Dashboard -> "Secrets Blocked" and "PII Masked" panels. "Protection Coverage" shows % of requests with sensitive data.

### Q: "Service Usage Detail total = Total Cost - Cost Savings?"
**A:** No. Service Usage Detail total = Total Cost (post-optimization, what was actually billed). Cost Savings is an additional estimated value. Relationship:
```
Total Cost (billed)          = Service Usage Detail total
Total Cost (without optimize) = Total Cost (billed) + Cost Savings
```
