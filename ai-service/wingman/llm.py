"""Language-model access for the wingman, behind a one-method interface.

The service never depends on a particular provider: `LLM.complete` takes a
system prompt and one user message and returns text. `AnthropicLLM` is the
shipped implementation (Anthropic Messages API over HTTPS). Nothing here logs
prompts or answers; callers log outcomes only.
"""
from __future__ import annotations

import os
from typing import Protocol

import httpx


class LLM(Protocol):
    def complete(self, system: str, user: str, *, max_tokens: int = 700) -> str: ...


class LLMError(Exception):
    """Any failure to get an answer: network, status, malformed envelope."""


class AnthropicLLM:
    def __init__(self, api_key: str, model: str, base_url: str = "https://api.anthropic.com",
                 timeout_s: float = 8.0, transport: httpx.BaseTransport | None = None) -> None:
        self._key = api_key
        self._model = model
        self._base = base_url.rstrip("/")
        self._client = httpx.Client(timeout=timeout_s, transport=transport)

    def complete(self, system: str, user: str, *, max_tokens: int = 700) -> str:
        try:
            r = self._client.post(
                f"{self._base}/v1/messages",
                headers={"x-api-key": self._key, "anthropic-version": "2023-06-01", "content-type": "application/json"},
                json={"model": self._model, "max_tokens": max_tokens, "temperature": 0.7, "system": system,
                      "messages": [{"role": "user", "content": user}]},
            )
        except httpx.HTTPError as e:  # timeouts, connection errors
            raise LLMError(f"request failed: {type(e).__name__}") from None
        if r.status_code != 200:
            raise LLMError(f"status {r.status_code}")
        try:
            blocks = r.json()["content"]
            text = "".join(b["text"] for b in blocks if b.get("type") == "text")
        except (ValueError, KeyError, TypeError):
            raise LLMError("unexpected response shape") from None
        if not text.strip():
            raise LLMError("empty answer")
        return text


def from_env() -> LLM | None:
    """The configured model, or None (template suggestions) when the provider
    is not `llm` or no key is set."""
    if os.environ.get("WINGMAN_PROVIDER", "template") != "llm":
        return None
    key = os.environ.get("ANTHROPIC_API_KEY", "")
    if not key:
        return None
    return AnthropicLLM(
        key,
        os.environ.get("WINGMAN_MODEL", "claude-sonnet-5-5"),
        os.environ.get("WINGMAN_API_BASE", "https://api.anthropic.com"),
        float(os.environ.get("WINGMAN_TIMEOUT_S", "8")),
    )
