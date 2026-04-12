"""API key pool manager with cooldown-based rotation."""

from __future__ import annotations

import asyncio
import random
import time
from typing import Any

import structlog

logger = structlog.get_logger(__name__)

# Max cooldown when a key hits rate limit (seconds).
MAX_COOLDOWN = 60


class KeyManager:
    """Thread-safe API key pool with random selection and cooldown rotation."""

    def __init__(self, keys_by_provider: dict[str, list[str]]):
        self._pools: dict[str, list[str]] = {}
        self._cooldowns: dict[str, dict[str, float]] = {}  # provider → {key: cooldown_until}
        self._lock = asyncio.Lock()

        for provider, keys in keys_by_provider.items():
            if keys:
                self._pools[provider] = list(keys)
                self._cooldowns[provider] = {}
                logger.info("key pool initialized", provider=provider, key_count=len(keys))

    def _available_keys(self, provider: str) -> list[str]:
        """Return keys not currently in cooldown."""
        pool = self._pools.get(provider, [])
        cooldowns = self._cooldowns.get(provider, {})
        now = time.monotonic()
        return [k for k in pool if cooldowns.get(k, 0) <= now]

    async def get_key(self, provider: str) -> str | None:
        """Get a random available (non-cooldown) API key."""
        async with self._lock:
            available = self._available_keys(provider)
            if not available:
                return None
            return random.choice(available)

    async def cooldown_key(self, provider: str, key: str, duration: float = MAX_COOLDOWN) -> str | None:
        """Put key on cooldown for `duration` seconds (capped at MAX_COOLDOWN).
        Returns the next available key, or None if all cooling down."""
        async with self._lock:
            cooldowns = self._cooldowns.setdefault(provider, {})
            actual = min(duration, MAX_COOLDOWN)
            cooldowns[key] = time.monotonic() + actual
            logger.warning("key on cooldown",
                           provider=provider,
                           cooldown_seconds=actual,
                           key_prefix=key[:8])

            # Return next available
            available = self._available_keys(provider)
            if not available:
                logger.warning("all keys in cooldown", provider=provider)
                return None
            return random.choice(available)

    async def rotate_key(self, provider: str, failed_key: str) -> str | None:
        """Alias for cooldown_key — keeps backward compatibility with worker.py."""
        return await self.cooldown_key(provider, failed_key)

    def get_available_providers(self) -> list[str]:
        """Return providers that have keys (even if some are cooling down)."""
        return [p for p, keys in self._pools.items() if keys]

    def has_keys(self, provider: str) -> bool:
        """Check if provider has any keys registered (regardless of cooldown)."""
        pool = self._pools.get(provider)
        return bool(pool)

    def has_available_keys(self, provider: str) -> bool:
        """Check if provider has any keys NOT in cooldown right now."""
        return len(self._available_keys(provider)) > 0

    def key_counts(self) -> dict[str, dict[str, int]]:
        """Return key stats per provider."""
        result = {}
        for p, keys in self._pools.items():
            available = self._available_keys(p)
            result[p] = {
                "total": len(keys),
                "available": len(available),
                "cooling_down": len(keys) - len(available),
            }
        return result

    async def shortest_cooldown(self, provider: str) -> float:
        """Return seconds until the shortest cooldown expires, or 5.0 if unknown."""
        async with self._lock:
            cooldowns = self._cooldowns.get(provider, {})
            now = time.monotonic()
            waits = [v - now for v in cooldowns.values() if v > now]
            if waits:
                return min(waits) + 0.5
            return 5.0
