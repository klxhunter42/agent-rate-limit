"""Configuration for the AI Worker service."""

from __future__ import annotations

from pydantic_settings import BaseSettings
from pydantic import Field


class WorkerSettings(BaseSettings):
    """Worker settings loaded from environment variables."""

    # --- Redis / Dragonfly ---
    redis_url: str = "redis://dragonfly:6379"
    queue_name: str = "ai_jobs"
    retry_queue_name: str = "ai_jobs_retry"
    result_ttl: int = 600  # seconds
    short_cache_ttl: int = 60

    # --- Worker ---
    worker_concurrency: int = 10
    max_retries: int = 3
    base_backoff: float = 1.0
    poll_timeout: int = 5  # BRPOP timeout in seconds

    # --- Observability ---
    otel_endpoint: str = "otel-collector:4317"
    metrics_port: int = 9090

    # --- GLM / Z.ai (Primary) ---
    glm_endpoint: str = "https://api.z.ai/api/anthropic"
    glm_api_keys: str = ""
    glm_default_model: str = "glm-5"

    # --- OpenAI ---
    openai_api_keys: str = ""

    # --- Anthropic ---
    anthropic_api_keys: str = ""

    # --- Google Gemini ---
    gemini_api_keys: str = ""

    # --- OpenRouter ---
    openrouter_api_keys: str = ""

    # --- Upstream concurrency limits ---
    upstream_model_limits: str = ""
    upstream_default_limit: int = 1
    upstream_global_limit: int = 0  # 0 = unlimited

    # --- Provider defaults ---
    default_provider: str = "glm"
    default_model: str = "glm-5"
    request_timeout: int = 120

    # --- Per-provider RPM limits ---
    provider_rpm_limits: str = ""  # "glm:5,openai:60"

    model_config = {
        "env_file": ".env",
        "env_file_encoding": "utf-8",
    }

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        # Parse comma-separated key strings into lists
        self._glm_keys = _parse_keys(self.glm_api_keys)
        self._openai_keys = _parse_keys(self.openai_api_keys)
        self._anthropic_keys = _parse_keys(self.anthropic_api_keys)
        self._gemini_keys = _parse_keys(self.gemini_api_keys)
        self._openrouter_keys = _parse_keys(self.openrouter_api_keys)
        # Parse per-model concurrency limits
        self._model_limits = _parse_model_limits(self.upstream_model_limits)
        # Parse per-provider RPM limits
        self._rpm_limits = _parse_rpm_limits(self.provider_rpm_limits)

    @property
    def glm_key_list(self) -> list[str]:
        return self._glm_keys

    @property
    def openai_key_list(self) -> list[str]:
        return self._openai_keys

    @property
    def anthropic_key_list(self) -> list[str]:
        return self._anthropic_keys

    @property
    def gemini_key_list(self) -> list[str]:
        return self._gemini_keys

    @property
    def openrouter_key_list(self) -> list[str]:
        return self._openrouter_keys

    @property
    def model_limits(self) -> dict[str, int]:
        return self._model_limits

    @property
    def rpm_limits(self) -> dict[str, int]:
        return self._rpm_limits

    @property
    def available_providers(self) -> list[str]:
        """Return list of providers that have at least one API key configured."""
        providers = []
        if self._glm_keys:
            providers.append("glm")
        if self._openai_keys:
            providers.append("openai")
        if self._anthropic_keys:
            providers.append("anthropic")
        if self._gemini_keys:
            providers.append("gemini")
        if self._openrouter_keys:
            providers.append("openrouter")
        return providers


def _parse_keys(value: str) -> list[str]:
    """Parse comma-separated API key string into a list."""
    if not value or not value.strip():
        return []
    return [k.strip() for k in value.split(",") if k.strip()]


def _parse_model_limits(value: str) -> dict[str, int]:
    """Parse 'model1:limit1,model2:limit2' into a dict."""
    if not value or not value.strip():
        return {}
    result = {}
    for pair in value.split(","):
        parts = pair.strip().split(":")
        if len(parts) == 2:
            try:
                n = int(parts[1])
                if n > 0:
                    result[parts[0].strip()] = n
            except ValueError:
                pass
    return result


def _parse_rpm_limits(value: str) -> dict[str, int]:
    """Parse 'provider1:rpm1,provider2:rpm2' into a dict."""
    return _parse_model_limits(value)  # same format, reuse parser


# Provider fallback order
PROVIDER_FALLBACK_ORDER = ["glm", "openai", "anthropic", "gemini", "openrouter"]

# Model fallback order: glm-5 series prioritized, then older models
MODEL_FALLBACK_ORDER = [
    "glm-5.1", "glm-5-turbo", "glm-5",
    "glm-4.7", "glm-4.6",
]


def get_settings() -> WorkerSettings:
    """Load and return worker settings."""
    return WorkerSettings()
