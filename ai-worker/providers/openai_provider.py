"""OpenAI provider using the official SDK."""

from __future__ import annotations

from typing import Any

import openai
import structlog

from .base import BaseProvider, ProviderResponse

logger = structlog.get_logger(__name__)


class OpenAIProvider(BaseProvider):
    """OpenAI API provider."""

    def __init__(self, api_key: str, timeout: int = 120):
        self.client = openai.AsyncOpenAI(
            api_key=api_key,
            timeout=timeout,
        )
        self._api_key = api_key
        logger.info("openai provider initialized")

    def get_name(self) -> str:
        return "openai"

    async def complete(
        self,
        messages: list[dict[str, Any]],
        model: str = "gpt-4o",
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
            provider="openai",
            usage=usage,
            finish_reason=response.choices[0].finish_reason or "stop",
        )
