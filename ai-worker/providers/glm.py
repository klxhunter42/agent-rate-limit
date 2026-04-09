"""Z.ai / GLM provider using the Anthropic-compatible API."""

from __future__ import annotations

from typing import Any

import anthropic
import structlog

from .base import BaseProvider, ProviderResponse

logger = structlog.get_logger(__name__)

# Map GLM model names to valid API model identifiers
GLM_MODELS = {
    "glm-5": "glm-5",
    "glm-4.5": "glm-4.5",
    "glm-4.6v": "glm-4.6v",
}


class GLMProvider(BaseProvider):
    """Z.ai GLM provider that uses the Anthropic-compatible endpoint."""

    def __init__(self, endpoint: str, api_key: str, timeout: int = 120):
        self.client = anthropic.AsyncAnthropic(
            api_key=api_key,
            base_url=endpoint,
            timeout=timeout,
        )
        self._api_key = api_key
        logger.info("glm provider initialized", endpoint=endpoint)

    def get_name(self) -> str:
        return "glm"

    async def complete(
        self,
        messages: list[dict[str, Any]],
        model: str = "glm-5",
        max_tokens: int = 1024,
        temperature: float = 0.7,
        **kwargs,
    ) -> ProviderResponse:
        api_model = GLM_MODELS.get(model, model)

        # Separate system message from user/assistant messages
        system_content = None
        chat_messages = []
        for msg in messages:
            if msg.get("role") == "system":
                system_content = msg.get("content", "")
            else:
                chat_messages.append(msg)

        params: dict[str, Any] = {
            "model": api_model,
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
            provider="glm",
            usage=usage,
            finish_reason=response.stop_reason or "stop",
        )
