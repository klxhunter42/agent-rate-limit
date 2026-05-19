# Observability

## Prometheus & OpenTelemetry

### Prometheus Scrape Targets

| Target                  | Interval   | Path                   |
|-------------------------|------------|------------------------|
| `arl-gateway:8080`      | 5s         | `/metrics`             |
| `arl-worker:9090`       | 5s         | `/metrics`             |
| `arl-rate-limiter:8080` | 10s        | `/actuator/prometheus` |
| `arl-otel:8889`         | 10s        | `/metrics`             |
| `arl-prometheus:9090`   | 15s        | `/metrics`             |

### OpenTelemetry Collector

| Protocol   | Endpoint        | Used For                   |
|------------|-----------------|----------------------------|
| gRPC       | `arl-otel:4317` | Traces & metrics ingestion |
| HTTP       | `arl-otel:4318` | Traces & metrics ingestion |
| Prometheus | `arl-otel:8889` | Metrics export             |

---

## All Gateway Prometheus Metrics

Namespace: `api_gateway`

### Core Request Metrics

| Metric                                | Type      | Labels                     | Description                                                                                                                               |
|---------------------------------------|-----------|----------------------------|-------------------------------------------------------------------------------------------------------------------------------------------|
| `api_gateway_request_latency_seconds` | Histogram | `method`, `path`, `status` | Request latency. Buckets: 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60                                                                     |
| `api_gateway_active_connections`      | Gauge     | -                          | Current active connections                                                                                                                |
| `api_gateway_queue_depth`             | GaugeFunc | -                          | Current AI job queue depth                                                                                                                |
| `api_gateway_error_total`             | Counter   | `type`                     | Errors by type. Values: `bad_request`, `validation`, `queue_push`, `cache_get`, `upstream`, `overloaded`, `no_provider`, `quota_exceeded` |
| `api_gateway_rate_limit_hits_total`   | Counter   | `key`                      | Rate-limited requests. Agent keys are SHA1-hashed to 8 chars. Values: `global`, `agent:*`                                                 |

### Token & Cost Metrics

| Metric                           | Type    | Labels          | Description                                    |
|----------------------------------|---------|-----------------|------------------------------------------------|
| `api_gateway_token_input_total`  | Counter | `model`         | Total input tokens consumed                    |
| `api_gateway_token_output_total` | Counter | `model`         | Total output tokens generated                  |
| `api_gateway_cost_total`         | Counter | `model`, `type` | Estimated cost in USD. Type: `input`, `output` |

### Upstream Metrics

| Metric                               | Type      | Labels   | Description                                                                             |
|--------------------------------------|-----------|----------|-----------------------------------------------------------------------------------------|
| `api_gateway_upstream_retries_total` | Counter   | -        | Upstream retries on 429                                                                 |
| `api_gateway_upstream_429_total`     | Counter   | -        | Upstream 429 responses received                                                         |
| `api_gateway_ttfb_seconds`           | Histogram | `model`  | Time to first byte for streaming. Buckets: 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5 |

### Adaptive Limiter Metrics

| Metric                             | Type    | Labels                  | Description                                  |
|------------------------------------|---------|-------------------------|----------------------------------------------|
| `api_gateway_adaptive_limit`       | Gauge   | `model`                 | Current adaptive concurrency limit per model |
| `api_gateway_adaptive_in_flight`   | Gauge   | `model`                 | Current in-flight requests per model         |
| `api_gateway_model_fallback_total` | Counter | `requested`, `selected` | Model fallback events                        |

### Profile Metrics

| Metric                                   | Type    | Labels                     | Description                                         |
|------------------------------------------|---------|----------------------------|-----------------------------------------------------|
| `api_gateway_profile_requests_total`     | Counter | `profile`, `model`         | Requests per profile per model                      |
| `api_gateway_profile_token_input_total`  | Counter | `profile`, `model`         | Input tokens per profile per model                  |
| `api_gateway_profile_token_output_total` | Counter | `profile`, `model`         | Output tokens per profile per model                 |
| `api_gateway_profile_cost_total`         | Counter | `profile`, `model`, `type` | Cost per profile per model. Type: `input`, `output` |

### Optimizer Metrics

| Metric                                     | Type      | Labels                   | Description                                                                           |
|--------------------------------------------|-----------|--------------------------|---------------------------------------------------------------------------------------|
| `api_gateway_optimizer_runs_total`         | Counter   | `technique`              | Optimization runs per technique                                                       |
| `api_gateway_optimizer_chars_saved_total`  | Counter   | `technique`, `direction` | Characters saved per technique. Direction: `input`, `output`                         |
| `api_gateway_optimizer_duration_seconds`   | Histogram | `technique`              | Optimizer execution time. Buckets: 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1 |
| `api_gateway_optimizer_tokens_saved_total` | Counter   | `direction`              | Estimated total tokens saved. Direction: `input`, `output`                            |
| `api_gateway_cost_savings_total`           | Counter   | -                        | Estimated cost savings from optimization (USD)                                        |
| `api_gateway_budget_level`                 | Gauge     | `model`                  | Budget utilization level (0=green, 1=yellow, 2=red)                                   |

Technique labels: `semantic_dedup`, `chunker`, `delta`, `sketch_dedup`, `summarizer`, `textcomp`, `caveman`, `pordee`, `toolcomp`, `toolfilter`, `desctrim`, `message_text`, `message_block_text`, `message_block_tool_result`

### Profile Optimizer Metrics

| Metric                                          | Type    | Labels                    | Description                          |
|-------------------------------------------------|---------|---------------------------|--------------------------------------|
| `api_gateway_profile_optimizer_chars_saved_total` | Counter | `profile`, `technique`  | Per-profile optimizer chars saved    |

### Waste Detection Metrics

| Metric                                  | Type    | Labels                 | Description                 |
|-----------------------------------------|---------|------------------------|-----------------------------|
| `api_gateway_waste_findings_total`      | Counter | `detector`, `severity` | Waste findings detected     |
| `api_gateway_waste_tokens_wasted_total` | Counter | `detector`             | Tokens identified as wasted |

Detector values: `empty_response`, `retry_churn`, `loop_detection`, `oversized_context`, `budget_exceeded`, `redundant_tool_call`, `low_value_response`. Severity values: `low`, `medium`, `high`.

### Billing Path Metrics

| Metric                                     | Type      | Labels                     | Description                                                                                       |
|--------------------------------------------|-----------|----------------------------|---------------------------------------------------------------------------------------------------|
| `api_gateway_billing_path_requests_total`  | Counter   | `path`, `model`, `profile` | Requests by billing path. Path values: `go_direct`, `sidecar`, `direct`, `billing_rejected`       |
| `api_gateway_billing_path_latency_seconds` | Histogram | `path`, `model`            | Latency by billing path. Buckets: 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60 |

### Error Recovery Metrics

| Metric                                 | Type    | Labels            | Description                                      |
|----------------------------------------|---------|-------------------|--------------------------------------------------|
| `api_gateway_context_truncation_total` | Counter | `model`           | Auto-truncation recovery attempts                |
| `api_gateway_transient_retry_total`    | Counter | `status`, `model` | Transient error retries by status code and model |

### Claude OAuth Metrics

| Metric                                        | Type    | Labels                          | Description                                        |
|-----------------------------------------------|---------|---------------------------------|----------------------------------------------------|
| `api_gateway_claude_profile_fetches_total`    | Counter | `status`                        | Claude profile fetch attempts                      |
| `api_gateway_claude_profile_errors_total`     | Counter | `error_type`                    | Claude profile fetch errors                        |
| `api_gateway_claude_org_tier`                 | Gauge   | `org_uuid`, `tier`              | Claude organization tier                           |
| `api_gateway_claude_subscription_status`      | Gauge   | `org_uuid`, `status`            | Claude subscription status                         |
| `api_gateway_claude_role_count`               | Gauge   | `org_uuid`, `role`              | Seat role counts                                   |

### Account Metrics

| Metric                                        | Type    | Labels                          | Description                                        |
|-----------------------------------------------|---------|---------------------------------|----------------------------------------------------|
| `api_gateway_account_token_input_total`       | Counter | `account_id`, `model`           | Input tokens per account                           |
| `api_gateway_account_token_output_total`      | Counter | `account_id`, `model`           | Output tokens per account                          |

### Cache Token Metrics

| Metric                                        | Type    | Labels                          | Description                                        |
|-----------------------------------------------|---------|---------------------------------|----------------------------------------------------|
| `api_gateway_cache_creation_tokens_total`     | Counter | `model`                         | Anthropic cache creation input tokens              |
| `api_gateway_cache_read_tokens_total`         | Counter | `model`                         | Anthropic cache read input tokens                  |
| `api_gateway_cache_savings_total`             | Counter | `model`                         | Estimated cost savings from cache hits             |

### MCP Server Metrics

| Metric                                        | Type      | Labels                  | Description                          |
|-----------------------------------------------|-----------|-------------------------|--------------------------------------|
| `api_gateway_mcp_calls_total`                 | Counter   | `server`, `tool`, `status` | MCP tool call count               |
| `api_gateway_mcp_call_duration_seconds`       | Histogram | `server`, `tool`        | MCP tool call latency                |
| `api_gateway_mcp_cache_hits_total`            | Counter   | `server`, `tool`        | MCP cache hits                       |
| `api_gateway_mcp_cache_misses_total`          | Counter   | `server`, `tool`        | MCP cache misses                     |
| `api_gateway_mcp_quota_usage`                 | Gauge     | `account_id`            | MCP quota usage                      |

### Image Compression Metrics

| Metric                                        | Type    | Labels                  | Description                          |
|-----------------------------------------------|---------|-------------------------|--------------------------------------|
| `api_gateway_image_compressions_total`        | Counter | `model`                 | Compressed image count               |
| `api_gateway_image_bytes_saved_total`         | Counter | `model`                 | Bytes saved by compression           |
| `api_gateway_image_bytes_original_total`      | Counter | `model`                 | Original bytes before compression    |

### Vision Pre-Analysis Metrics

| Metric                                            | Type      | Labels  | Description                       |
|---------------------------------------------------|-----------|---------|-----------------------------------|
| `api_gateway_vision_preanalysis_total`            | Counter   | `status` | Vision pre-analysis call count   |
| `api_gateway_vision_preanalysis_duration_seconds` | Histogram | -       | Vision pre-analysis latency       |

### Runtime Metrics (collected every 10s)

| Metric                             | Type   | Labels   | Description                                |
|------------------------------------|--------|----------|--------------------------------------------|
| `api_gateway_go_goroutines`        | Gauge  | -        | Current goroutine count                    |
| `api_gateway_go_heap_alloc_bytes`  | Gauge  | -        | Current heap allocation                    |
| `api_gateway_go_heap_objects`      | Gauge  | -        | Current heap object count                  |
| `api_gateway_go_gc_pause_ns`       | Gauge  | -        | Last GC pause duration (ns)                |
| `api_gateway_go_stack_inuse_bytes` | Gauge  | -        | Current stack in-use                       |
| `api_gateway_dragonfly_up`         | Gauge  | -        | Dragonfly/Redis health (1=healthy, 0=down) |

---

## Middleware Stack

The following middleware runs on every request (in order):

| Middleware                            | Source                         |                  Registers Metrics                   |
|---------------------------------------|--------------------------------|:----------------------------------------------------:|
| `SecurityHeaders`                     | `middleware/security.go`       |                          No                          |
| `CorrelationID`                       | `middleware/security.go`       |                          No                          |
| `RealIP`                              | `middleware/security.go`       |                          No                          |
| `IPFilter` (if configured)            | `middleware/security.go`       |                          No                          |
| `Logging`                             | `middleware/logging.go`        |                          No                          |
| `Metrics.Middleware`                  | `metrics/metrics.go`           | Yes: `request_latency_seconds`, `active_connections` |
| `RateLimiter.Middleware`              | `middleware/ratelimit.go`      |             Yes: `rate_limit_hits_total`             |
| `DashboardAuth` (on dashboard routes) | `middleware/dashboard-auth.go` |                          No                          |

---

## Cost Calculator

Dashboard Cost Calculator is at http://localhost:3000/d/ai-cost

### How to Use

1. Open Grafana -> **Dashboards** -> **Cost Calculator & Savings**
2. Select time range (default: 24h)
3. View automatic metrics:
 - **Total Requests** -- request count past 24h
 - **Requests/hour** -- average requests per hour
 - **Est. Tokens** -- estimated tokens used (input ~500, output ~200 tokens/request)
 - **Daily Cost** -- calculated from tokens x pricing
 - **Rate Limited Requests** -- requests blocked = **money saved**

### Calculation Formula

```
Daily Cost = (Input Tokens / 1M) x Input Price + (Output Tokens / 1M) x Output Price
Input Tokens ~= Jobs Processed x 500 (default estimate)
Output Tokens ~= Jobs Processed x 200 (default estimate)
```

### Rate Limit Savings

Requests blocked by 429 = money not paid to provider:

```
Savings = Rate Limited Requests x Average Cost per Request
```

---

*Back to [Manual](../MANUAL.md)*
