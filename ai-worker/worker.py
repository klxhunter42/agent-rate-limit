"""Core worker that pulls jobs from Dragonfly and dispatches to AI providers."""

from __future__ import annotations

import asyncio
import hashlib
import json
import random
import time
from typing import Any

import redis.asyncio as aioredis
import structlog

from config import WorkerSettings, PROVIDER_FALLBACK_ORDER, MODEL_FALLBACK_ORDER
import prom_metrics as pm
from key_manager import KeyManager
from providers.base import BaseProvider, ProviderResponse
from providers.glm import GLMProvider
from providers.openai_provider import OpenAIProvider
from providers.anthropic_provider import AnthropicProvider
from providers.gemini import GeminiProvider
from providers.openrouter import OpenRouterProvider

logger = structlog.get_logger(__name__)


class ProviderRateLimiter:
    """Sliding window RPM limiter per provider. Prevents 429s by pacing requests."""

    def __init__(self):
        self._windows: dict[str, list[float]] = {}  # provider -> list of timestamps
        self._limits: dict[str, int] = {}  # provider -> max requests per minute
        self._lock = asyncio.Lock()

    def set_limit(self, provider: str, rpm: int):
        self._limits[provider] = rpm

    async def acquire(self, provider: str):
        """Wait until a request slot is available within the RPM window."""
        limit = self._limits.get(provider)
        if not limit or limit <= 0:
            return  # no limit configured

        async with self._lock:
            now = time.monotonic()
            window = self._windows.setdefault(provider, [])

            # Prune timestamps older than 60s
            self._windows[provider] = [t for t in window if now - t < 60]
            window = self._windows[provider]

            if len(window) >= limit:
                oldest = window[0]
                wait = 60 - (now - oldest) + 0.1  # small buffer
                if wait > 0:
                    logger.info("rpm limiter waiting", provider=provider, wait_seconds=round(wait, 1))
                    # Release lock during sleep to allow other providers to proceed.
                    # Re-check after wake since we're outside the lock.
                    pass
            else:
                self._windows[provider].append(time.monotonic())
                return

        # Sleep outside lock.
        await asyncio.sleep(wait)
        async with self._lock:
            self._windows.setdefault(provider, []).append(time.monotonic())

# Prometheus-style counters (simple in-process counters for metrics endpoint)
class Metrics:
    def __init__(self):
        self.jobs_processed = 0
        self.jobs_failed = 0
        self.jobs_retried = 0
        self.provider_latency: dict[str, list[float]] = {}
        self.provider_errors: dict[str, int] = {}
        self.rate_limit_hits: dict[str, int] = {}
        self.queue_depth = 0

    def record_latency(self, provider: str, latency: float):
        if provider not in self.provider_latency:
            self.provider_latency[provider] = []
        self.provider_latency[provider].append(latency)
        # Keep last 1000 samples
        if len(self.provider_latency[provider]) > 1000:
            self.provider_latency[provider] = self.provider_latency[provider][-1000:]

    def record_error(self, provider: str):
        self.provider_errors[provider] = self.provider_errors.get(provider, 0) + 1

    def record_rate_limit(self, provider: str):
        self.rate_limit_hits[provider] = self.rate_limit_hits.get(provider, 0) + 1

    def snapshot(self) -> dict[str, Any]:
        latencies = {}
        for provider, samples in self.provider_latency.items():
            if samples:
                sorted_s = sorted(samples)
                latencies[provider] = {
                    "p50": sorted_s[len(sorted_s) // 2],
                    "p95": sorted_s[int(len(sorted_s) * 0.95)],
                    "p99": sorted_s[int(len(sorted_s) * 0.99)],
                    "avg": sum(sorted_s) / len(sorted_s),
                    "count": len(sorted_s),
                }
        return {
            "jobs_processed": self.jobs_processed,
            "jobs_failed": self.jobs_failed,
            "jobs_retried": self.jobs_retried,
            "queue_depth": self.queue_depth,
            "provider_latency": latencies,
            "provider_errors": self.provider_errors,
            "rate_limit_hits": self.rate_limit_hits,
        }


def _create_provider(
    provider_name: str,
    settings: WorkerSettings,
    key_manager: KeyManager,
    api_key: str | None = None,
) -> BaseProvider:
    """Factory to create a provider instance with a specific API key."""
    match provider_name:
        case "glm":
            return GLMProvider(
                endpoint=settings.glm_endpoint,
                api_key=api_key or "",
                timeout=settings.request_timeout,
            )
        case "openai":
            return OpenAIProvider(
                api_key=api_key or "",
                timeout=settings.request_timeout,
            )
        case "anthropic":
            return AnthropicProvider(
                api_key=api_key or "",
                timeout=settings.request_timeout,
            )
        case "gemini":
            return GeminiProvider(
                api_key=api_key or "",
                timeout=settings.request_timeout,
            )
        case "openrouter":
            return OpenRouterProvider(
                api_key=api_key or "",
                timeout=settings.request_timeout,
            )
        case _:
            raise ValueError(f"Unknown provider: {provider_name}")


class Worker:
    """Async worker that polls the Dragonfly queue and processes AI jobs."""

    def __init__(
        self,
        settings: WorkerSettings,
        key_manager: KeyManager,
        metrics: Metrics,
    ):
        self.settings = settings
        self.key_manager = key_manager
        self.metrics = metrics
        self.redis: aioredis.Redis | None = None
        self._running = False
        self._provider_cache: dict[str, BaseProvider] = {}

        # Per-provider RPM limiter
        self._rpm_limiter = ProviderRateLimiter()
        for provider, rpm in settings.rpm_limits.items():
            self._rpm_limiter.set_limit(provider, rpm)
            logger.info("provider rpm limit set", provider=provider, rpm=rpm)

        # Per-model concurrency semaphores
        self._model_sems: dict[str, asyncio.Semaphore] = {}
        for model, limit in settings.model_limits.items():
            self._model_sems[model] = asyncio.Semaphore(limit)
            logger.info("model semaphore", model=model, limit=limit)

        # Global semaphore (0 = unlimited)
        self._global_sem: asyncio.Semaphore | None = None
        if settings.upstream_global_limit > 0:
            self._global_sem = asyncio.Semaphore(settings.upstream_global_limit)
            logger.info("global semaphore", limit=settings.upstream_global_limit)

        # Default semaphore for models not in config
        self._default_sem = asyncio.Semaphore(settings.upstream_default_limit)

    async def start(self):
        """Connect to Dragonfly and start the worker loop."""
        self.redis = aioredis.from_url(
            self.settings.redis_url,
            decode_responses=True,
        )
        # Test connection
        await self.redis.ping()
        logger.info("worker connected to dragonfly", url=self.settings.redis_url)
        self._running = True

    async def stop(self):
        """Gracefully stop the worker."""
        self._running = False
        if self.redis:
            await self.redis.close()

    async def run_loop(self, worker_id: int):
        """Main polling loop for a single worker coroutine."""
        logger.info("worker loop started", worker_id=worker_id)

        while self._running:
            try:
                # BRPOP with timeout
                result = await self.redis.brpop(
                    self.settings.queue_name,
                    timeout=self.settings.poll_timeout,
                )

                if result is None:
                    continue  # timeout, loop again

                _, job_json = result
                await self._process_job(job_json, worker_id)

            except asyncio.CancelledError:
                logger.info("worker cancelled", worker_id=worker_id)
                break
            except Exception as e:
                logger.error("worker loop error", worker_id=worker_id, error=str(e))
                await asyncio.sleep(1)

        logger.info("worker loop stopped", worker_id=worker_id)

    def _try_acquire_model(self, requested_model: str) -> tuple[str, asyncio.Semaphore] | None:
        """Try non-blocking acquire on requested model, then fallback chain.
        Returns (selected_model, semaphore) or None if all full."""
        # Build candidate list: requested first, then fallbacks
        candidates = [requested_model]
        for m in MODEL_FALLBACK_ORDER:
            if m not in candidates:
                candidates.append(m)

        for m in candidates:
            sem = self._model_sems.get(m, self._default_sem)
            if sem._value > 0:  # non-blocking check
                return m, sem
        return None

    async def _process_job(self, job_json: str, worker_id: int):
        """Parse, execute, and store the result for a single job."""
        try:
            job = json.loads(job_json)
        except json.JSONDecodeError as e:
            logger.error("invalid job JSON", error=str(e))
            self.metrics.jobs_failed += 1
            pm.JOBS_FAILED.inc()
            return

        request_id = job.get("request_id", "unknown")
        agent_id = job.get("agent_id", "unknown")
        provider_name = job.get("provider", self.settings.default_provider)
        model = job.get("model", self.settings.default_model)
        messages = job.get("messages", [])
        max_tokens = job.get("max_tokens", 1024)
        temperature = job.get("temperature", 0.7)
        retry_count = job.get("retry_count", 0)

        # Try non-blocking acquire with model fallback
        result = self._try_acquire_model(model)
        if result:
            selected_model, model_sem = result
        else:
            # All full — blocking wait on requested model
            selected_model = model
            model_sem = self._model_sems.get(model, self._default_sem)

        if selected_model != model:
            logger.info(
                "model fallback",
                worker_id=worker_id,
                request_id=request_id,
                requested=model,
                selected=selected_model,
            )
            job["model"] = selected_model
            model = selected_model

        async with model_sem:
            if self._global_sem:
                async with self._global_sem:
                    logger.info(
                        "slot acquired",
                        worker_id=worker_id,
                        request_id=request_id,
                        model=model,
                    )
                    await self._execute_job(
                        job, worker_id, request_id, agent_id,
                        provider_name, model, messages,
                        max_tokens, temperature, retry_count,
                    )
            else:
                logger.info(
                    "slot acquired",
                    worker_id=worker_id,
                    request_id=request_id,
                    model=model,
                )
                await self._execute_job(
                    job, worker_id, request_id, agent_id,
                    provider_name, model, messages,
                    max_tokens, temperature, retry_count,
                )

    def _get_provider(self, provider_name: str, api_key: str) -> BaseProvider:
        """Get or create a cached provider instance for connection reuse."""
        key_hash = hashlib.sha256(api_key.encode()).hexdigest()[:16]
        cache_key = f"{provider_name}:{key_hash}"
        if cache_key in self._provider_cache:
            return self._provider_cache[cache_key]
        provider = _create_provider(provider_name, self.settings, self.key_manager, api_key)
        self._provider_cache[cache_key] = provider
        logger.info("provider cached", provider=provider_name, cache_key=cache_key)
        return provider

    async def _execute_job(
        self, job, worker_id, request_id, agent_id,
        provider_name, model, messages,
        max_tokens, temperature, retry_count,
    ):

        # Try primary provider, then fallback chain (only providers with keys)
        providers_to_try = [provider_name]
        for p in PROVIDER_FALLBACK_ORDER:
            if p not in providers_to_try and self.key_manager.has_keys(p):
                providers_to_try.append(p)

        last_error = None
        for current_provider in providers_to_try:
            try:
                api_key = await self.key_manager.get_key(current_provider)

                # All keys in cooldown — wait for shortest cooldown to expire
                if api_key is None and self.key_manager.has_keys(current_provider):
                    wait = await self.key_manager.shortest_cooldown(current_provider)
                    if wait > 0:
                        logger.info("all keys cooling down, waiting",
                                    provider=current_provider,
                                    wait_seconds=round(wait, 1))
                        await asyncio.sleep(wait)
                    api_key = await self.key_manager.get_key(current_provider)

                if api_key is None:
                    logger.warning("no available key", provider=current_provider)
                    continue

                logger.info(
                    "provider key resolved",
                    provider=current_provider,
                    has_key=True,
                    key_prefix=api_key[:8],
                )
                provider = self._get_provider(current_provider, api_key)

                # Wait for RPM slot before calling provider
                await self._rpm_limiter.acquire(current_provider)

                start = time.monotonic()
                response = await provider.complete(
                    messages=messages,
                    model=model,
                    max_tokens=max_tokens,
                    temperature=temperature,
                )
                latency = time.monotonic() - start

                self.metrics.record_latency(current_provider, latency)
                self.metrics.jobs_processed += 1
                pm.JOBS_PROCESSED.labels(provider=current_provider).inc()
                pm.PROVIDER_LATENCY.labels(provider=current_provider).observe(latency)

                # Store result
                result = {
                    "request_id": request_id,
                    "agent_id": agent_id,
                    "status": "completed",
                    "provider": response.provider,
                    "model": response.model,
                    "content": response.content,
                    "usage": response.usage,
                    "finish_reason": response.finish_reason,
                    "latency_seconds": round(latency, 3),
                }

                await self._store_result(request_id, result)
                logger.info(
                    "job completed",
                    request_id=request_id,
                    provider=response.provider,
                    latency_ms=int(latency * 1000),
                )
                return

            except Exception as e:
                error_str = str(e)
                last_error = e
                self.metrics.record_error(current_provider)
                pm.PROVIDER_ERRORS.labels(provider=current_provider).inc()
                logger.warning(
                    "provider failed",
                    provider=current_provider,
                    request_id=request_id,
                    error=error_str,
                )

                # Check if it's a rate limit error
                is_rate_limit = _is_rate_limit_error(e)

                if is_rate_limit:
                    self.metrics.record_rate_limit(current_provider)
                    pm.RATE_LIMIT_HITS.labels(provider=current_provider).inc()
                    # Put key on cooldown (60s max)
                    if api_key:
                        await self.key_manager.cooldown_key(current_provider, api_key)

                # Move to next provider in fallback chain
                continue

        # All providers failed
        self.metrics.jobs_failed += 1
        pm.JOBS_FAILED.inc()

        if retry_count < self.settings.max_retries:
            self.metrics.jobs_retried += 1
            pm.JOBS_RETRIED.inc()
            await self._retry_job(job, retry_count)
        else:
            # Store error result
            error_result = {
                "request_id": request_id,
                "agent_id": agent_id,
                "status": "error",
                "error": str(last_error) if last_error else "all providers failed",
                "retry_count": retry_count,
            }
            await self._store_result(request_id, error_result)
            logger.error(
                "job failed after all retries",
                request_id=request_id,
                retry_count=retry_count,
            )

    async def _store_result(self, request_id: str, result: dict):
        """Store the result in Dragonfly cache."""
        key = f"result:{request_id}"
        value = json.dumps(result)
        await self.redis.set(key, value, ex=self.settings.result_ttl)

    async def _retry_job(self, job: dict, current_retry: int):
        """Push the job back to the retry queue with exponential backoff."""
        job["retry_count"] = current_retry + 1

        backoff = self.settings.base_backoff * (2 ** current_retry)
        backoff = min(backoff, 60.0)  # cap at 60s
        backoff_with_jitter = backoff + random.uniform(0, backoff * 0.5)

        logger.info(
            "retrying job",
            request_id=job.get("request_id"),
            retry_count=job["retry_count"],
            backoff_seconds=round(backoff_with_jitter, 2),
        )

        # Wait with backoff before re-queueing
        await asyncio.sleep(backoff_with_jitter)

        await self.redis.lpush(
            self.settings.queue_name,
            json.dumps(job),
        )

    async def get_queue_depth(self) -> int:
        """Get current queue depth for metrics."""
        return await self.redis.llen(self.settings.queue_name)


def _is_rate_limit_error(exc: Exception) -> bool:
    """Check if the exception indicates a rate limit (429) or server error."""
    error_str = str(exc).lower()
    if "429" in error_str or "rate_limit" in error_str or "rate limit" in error_str:
        return True
    # Z.ai specific: code 1305 = overloaded
    if "1305" in error_str or "overloaded" in error_str:
        return True
    # Check for httpx status codes
    if hasattr(exc, "status_code"):
        return exc.status_code in (429, 502, 503, 504)
    return False
