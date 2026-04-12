"""Google Gemini provider."""

from __future__ import annotations

from typing import Any

import google.generativeai as genai
import structlog

from .base import BaseProvider, ProviderResponse

logger = structlog.get_logger(__name__)


class GeminiProvider(BaseProvider):
    """Google Gemini API provider."""

    def __init__(self, api_key: str, timeout: int = 120):
        # Use per-instance client instead of global genai.configure() to
        # support multi-key scenarios without key collision.
        self._api_key = api_key
        self._timeout = timeout
        self._client = genai.GenerativeModel  # lazy; configure per-request
        logger.info("gemini provider initialized")

    def get_name(self) -> str:
        return "gemini"

    async def complete(
        self,
        messages: list[dict[str, Any]],
        model: str = "gemini-2.0-flash",
        max_tokens: int = 1024,
        temperature: float = 0.7,
        **kwargs,
    ) -> ProviderResponse:
        # Configure per-request to isolate keys across cached instances.
        genai.configure(api_key=self._api_key)

        # Convert messages to Gemini format
        system_instruction = None
        contents = []
        for msg in messages:
            role = msg.get("role", "user")
            content = msg.get("content", "")
            if role == "system":
                system_instruction = content
            elif role == "assistant":
                contents.append({"role": "model", "parts": [content]})
            else:
                contents.append({"role": "user", "parts": [content]})

        generation_config = genai.types.GenerationConfig(
            max_output_tokens=max_tokens,
            temperature=temperature,
        )

        gen_model = genai.GenerativeModel(
            model_name=model,
            system_instruction=system_instruction,
            generation_config=generation_config,
        )

        response = await gen_model.generate_content_async(contents)

        content = ""
        if response.text:
            content = response.text

        usage = {
            "prompt_tokens": response.usage_metadata.prompt_token_count if response.usage_metadata else 0,
            "completion_tokens": response.usage_metadata.candidates_token_count if response.usage_metadata else 0,
            "total_tokens": response.usage_metadata.total_token_count if response.usage_metadata else 0,
        }

        return ProviderResponse(
            content=content,
            model=model,
            provider="gemini",
            usage=usage,
            finish_reason="stop",
        )
