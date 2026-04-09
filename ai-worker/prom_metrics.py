"""Shared Prometheus metric definitions for the AI worker.

These are process-level counters/gauges/histograms that both main.py and
worker.py import so that every code-path increments the *same* Prometheus
objects that the /metrics endpoint exports.
"""

from prometheus_client import Counter, Gauge, Histogram

# ── Job lifecycle ──────────────────────────────────────────────────────
JOBS_PROCESSED = Counter(
    "ai_worker_jobs_processed_total",
    "Total jobs processed",
    ["provider"],
)
JOBS_FAILED = Counter(
    "ai_worker_jobs_failed_total",
    "Total jobs failed",
)
JOBS_RETRIED = Counter(
    "ai_worker_jobs_retried_total",
    "Total jobs retried",
)

# ── Provider observability ─────────────────────────────────────────────
PROVIDER_LATENCY = Histogram(
    "ai_worker_provider_latency_seconds",
    "Provider request latency",
    ["provider"],
    buckets=[0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0],
)
PROVIDER_ERRORS = Counter(
    "ai_worker_provider_errors_total",
    "Total provider errors",
    ["provider"],
)
RATE_LIMIT_HITS = Counter(
    "ai_worker_rate_limit_hits_total",
    "Total rate limit hits per provider",
    ["provider"],
)

# ── Operational gauges ─────────────────────────────────────────────────
QUEUE_DEPTH = Gauge("ai_worker_queue_depth", "Current queue depth")
ACTIVE_WORKERS = Gauge("ai_worker_active", "Number of active workers")
