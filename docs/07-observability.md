# Observability

## Prometheus & OpenTelemetry

### Prometheus Scrape Targets

| Target | Interval | Path |
|--------|----------|------|
| `arl-gateway:8080` | 5s | `/metrics` |
| `arl-worker:9090` | 5s | `/metrics` |
| `arl-rate-limiter:8080` | 10s | `/actuator/prometheus` |
| `arl-otel:8889` | 10s | `/metrics` |
| `arl-prometheus:9090` | 15s | `/metrics` |

### OpenTelemetry Collector

| Protocol | Endpoint | Used For |
|----------|----------|----------|
| gRPC | `arl-otel:4317` | Traces & metrics ingestion |
| HTTP | `arl-otel:4318` | Traces & metrics ingestion |
| Prometheus | `arl-otel:8889` | Metrics export |

---

## Cost Calculator

Dashboard Cost Calculator is at http://localhost:3000/d/arl-cost

### How to Use

1. Open Grafana → **Dashboards** → **Cost Calculator & Savings**
2. Select time range (default: 24h)
3. View automatic metrics:
 - **Total Requests** — request count past 24h
 - **Requests/hour** — average requests per hour
 - **Est. Tokens** — estimated tokens used (input ~500, output ~200 tokens/request)
 - **Daily Cost** — calculated from tokens x pricing
 - **Rate Limited Requests** — requests blocked = **money saved**

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
