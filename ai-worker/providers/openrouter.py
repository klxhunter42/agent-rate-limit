"""OpenRouter provider (OpenAI-compatible endpoint)."""

from __future__ import annotations

from typing import Any

import openai
import structlog

from .base import BaseProvider, ProviderResponse

logger = structlog.get_logger(__name__)

OPENROUTER_BASE_URL = "https://openrouter.ai/api/v1"


class OpenRouterProvider(BaseProvider):
    """OpenRouter API provider using OpenAI-compatible SDK."""

    def __init__(self, api_key: str, timeout: int = 120):
        self.client = openai.AsyncOpenAI(
            api_key=api_key,
            base_url=OPENROUTER_BASE_URL,
            timeout=timeout,
        )
        self._api_key = api_key
        logger.info("openrouter provider initialized")

    def get_name(self) -> str:
        return "openrouter"

    async def complete(
        self,
        messages: list[dict[str, Any]],
        model: str = "openai/gpt-4o",
        max_tokens: int = 1024,
        temperature: float = 0.7,
        **kwargs,
    ) -> ProviderResponse:
        response = await self.client.chat.completions.create(
            model=model,
            messages=messages,
            max_tokens=max_tokens,
            temperature=temperature,
        )

        content = response.choices[0].message.content or ""
        usage = {
            "prompt_tokens": response.usage.prompt_tokens if response.usage else 0,
            "completion_tokens": response.usage.completion_tokens if response.usage else 0,
            "total_tokens": response.usage.total_tokens if response.usage else 0,
        }

        return ProviderResponse(
            content=content,
            model=response.model,
            provider="openrouter",
            usage=usage,
            finish_reason=response.choices[0].finish_reason or "stop",
        )
