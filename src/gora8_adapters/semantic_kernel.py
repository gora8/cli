"""Wraps a Semantic Kernel `ChatCompletionAgent` as a gora8-deployable HTTP
server.

Verified against current Microsoft Learn docs (semantic-kernel, Python):
`agent.get_response(messages=task)` is async and manages an ephemeral thread
internally for a single-turn call — no explicit thread object required.
"""

from __future__ import annotations

import asyncio
from typing import Any, Callable, Optional

from .server import serve as _serve


def _default_output_mapper(response: Any) -> Any:
    return str(response)


def serve(
    agent,
    input_mapper: Optional[Callable[[str], str]] = None,
    output_mapper: Optional[Callable[[Any], Any]] = None,
    host: str = "0.0.0.0",
    port: int = 8000,
) -> None:
    """Serves a Semantic Kernel `ChatCompletionAgent` instance.

    Example:
        from semantic_kernel.agents import ChatCompletionAgent
        from semantic_kernel.connectors.ai.open_ai import OpenAIChatCompletion
        from gora8_adapters.semantic_kernel import serve

        agent = ChatCompletionAgent(service=OpenAIChatCompletion(), name="Assistant", instructions="You are helpful.")
        serve(agent)
    """
    in_map = input_mapper or (lambda task: task)
    out_map = output_mapper or _default_output_mapper

    def handler(task: str) -> Any:
        response = asyncio.run(agent.get_response(messages=in_map(task)))
        return out_map(response)

    _serve(handler, host=host, port=port)
