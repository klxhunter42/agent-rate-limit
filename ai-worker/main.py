"""AI Worker entry point with async worker pool, metrics server, and graceful shutdown."""

from __future__ import annotations

import asyncio
import json
import signal
import sys

import structlog
from prometheus_client import start_http_server

from config import get_settings, PROVIDER_FALLBACK_ORDER
from key_manager import KeyManager
from worker import Worker, Metrics
import prom_metrics as pm

structlog.configure(
    processors=[
        structlog.contextvars.merge_contextvars,
        structlog.processors.add_log_level,
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.dev.ConsoleRenderer() if sys.stdout.isatty() else structlog.processors.JSONRenderer(),
    ],
    wrapper_class=structlog.make_filtering_bound_logger(20),  # INFO level
    context_class=dict,
    logger_factory=structlog.PrintLoggerFactory(),
    cache_logger_on_first_use=True,
)

logger = structlog.get_logger(__name__)


class PrometheusExporter:
    """Exports internal metrics to Prometheus counters."""

    def __init__(self, internal_metrics: Metrics):
        self.internal = internal_metrics

    def export(self):
        """Sync internal counters to Prometheus."""
        pm.QUEUE_DEPTH.set(self.internal.queue_depth)


def build_key_manager(settings) -> KeyManager:
    """Build a KeyManager from settings."""
    keys_by_provider = {}
    if settings.glm_key_list:
        keys_by_provider["glm"] = settings.glm_key_list
    if settings.openai_key_list:
        keys_by_provider["openai"] = settings.openai_key_list
    if settings.anthropic_key_list:
        keys_by_provider["anthropic"] = settings.anthropic_key_list
    if settings.gemini_key_list:
        keys_by_provider["gemini"] = settings.gemini_key_list
    if settings.openrouter_key_list:
        keys_by_provider["openrouter"] = settings.openrouter_key_list
    return KeyManager(keys_by_provider)


async def metrics_updater(worker: Worker, interval: float = 5.0):
    """Periodically update Prometheus gauge metrics."""
    while True:
        try:
            depth = await worker.get_queue_depth()
            worker.metrics.queue_depth = depth
            pm.QUEUE_DEPTH.set(depth)
        except Exception:
            pass
        await asyncio.sleep(interval)


async def run_metrics_server(worker: Worker):
    """Start a simple HTTP server for internal metrics snapshot."""
    from http.server import HTTPServer, BaseHTTPRequestHandler

    class MetricsHandler(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path == "/metrics-internal":
                snapshot = worker.metrics.snapshot()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps(snapshot).encode())
            else:
                self.send_response(404)
                self.end_headers()

        def log_message(self, format, *args):
            pass  # silence access logs

    # Run HTTP server in executor to not block event loop
    loop = asyncio.get_event_loop()
    server = HTTPServer(("0.0.0.0", 9091), MetricsHandler)
    loop.run_in_executor(None, server.serve_forever)


async def main():
    settings = get_settings()

    logger.info(
        "starting ai-worker",
        concurrency=settings.worker_concurrency,
        queue=settings.queue_name,
        providers=settings.available_providers,
    )

    # Start Prometheus metrics server
    start_http_server(settings.metrics_port)
    logger.info("prometheus metrics server started", port=settings.metrics_port)

    # Build key manager
    key_manager = build_key_manager(settings)
    logger.info("key manager initialized", key_counts=key_manager.key_counts())

    # Build worker
    internal_metrics = Metrics()
    worker = Worker(settings, key_manager, internal_metrics)
    await worker.start()

    # Setup graceful shutdown
    shutdown_event = asyncio.Event()

    def handle_signal():
        logger.info("shutdown signal received")
        shutdown_event.set()

    loop = asyncio.get_event_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, handle_signal)

    # Start worker coroutines
    tasks = []
    for i in range(settings.worker_concurrency):
        task = asyncio.create_task(worker.run_loop(i))
        tasks.append(task)

    pm.ACTIVE_WORKERS.set(settings.worker_concurrency)

    # Start metrics updater
    metrics_task = asyncio.create_task(metrics_updater(worker))

    # Start internal metrics HTTP server on port 9091
    await run_metrics_server(worker)

    # Wait for shutdown signal
    await shutdown_event.wait()

    logger.info("shutting down workers...")
    await worker.stop()

    # Cancel all tasks
    for task in tasks:
        task.cancel()
    metrics_task.cancel()

    await asyncio.gather(*tasks, metrics_task, return_exceptions=True)

    logger.info("ai-worker stopped")


if __name__ == "__main__":
    asyncio.run(main())
