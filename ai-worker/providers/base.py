"""Abstract base class for AI providers."""

from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field


@dataclass
class ProviderResponse:
    """Standardized response from any AI provider."""

    content: str
    model: str
    provider: str
    usage: dict[str, int] = field(default_factory=lambda: {
        "prompt_tokens": 0,
        "completion_tokens": 0,
        "total_tokens": 0,
    })
    finish_reason: str = "stop"


class BaseProvider(ABC):
    """Base class for all AI providers."""

    @abstractmethod
    async def complete(
        self,
        messages: list[dict[str, Any]],
        model: str,
        max_tokens: int = 1024,
        temperature: float = 0.7,
        **kwargs,
    ) -> ProviderResponse:
        """Call the AI provider and return a standardized response."""
        ...

    @abstractmethod
    def get_name(self) -> str:
        """Return the provider name."""
        ...
