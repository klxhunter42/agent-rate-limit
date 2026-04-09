"""Anthropic provider using the official SDK."""

from __future__ import annotations

from typing import Any

import anthropic
import structlog

from .base import BaseProvider, ProviderResponse

logger = structlog.get_logger(__name__)


class AnthropicProvider(BaseProvider):
    """Anthropic API provider."""

    def __init__(self, api_key: str, timeout: int = 120):
        self.client = anthropic.AsyncAnthropic(
            api_key=api_key,
            timeout=timeout,
        )
        self._api_key = api_key
        logger.info("anthropic provider initialized")

    def get_name(self) -> str:
        return "anthropic"

    async def complete(
        self,
        messages: list[dict[str, Any]],
        model: str = "claude-sonnet-4-20250514",
        max_tokens: int = 1024,
        temperature: float = 0.7,
        **kwargs,
    ) -> ProviderResponse:
        system_content = None
        chat_messages = []
        for msg in messages:
            if msg.get("role") == "system":
                system_content = msg.get("content", "")
            else:
                chat_messages.append(msg)

        params: dict[str, Any] = {
            "model": model,
            "messages": chat_messages,
            "max_tokens": max_tokens,
            "temperature": temperature,
        }
        if system_content:
            params["system"] = system_content

        response = await self.client.messages.create(**params)

        content = ""
        if response.content:
            for block in response.content:
                if hasattr(block, "text"):
                    content += block.text

        usage = {
            "prompt_tokens": response.usage.input_tokens if response.usage else 0,
            "completion_tokens": response.usage.output_tokens if response.usage else 0,
            "total_tokens": (response.usage.input_tokens + response.usage.output_tokens) if response.usage else 0,
        }

        return ProviderResponse(
            content=content,
            model=response.model,
            provider="anthropic",
            usage=usage,
            finish_reason=response.stop_reason or "stop",
        )
