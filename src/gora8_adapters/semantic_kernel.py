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


def gora8_tools(credential: str, *, base_url: Optional[str] = None) -> Any:
    """Returns gora8's 16 economic-primitive tools (search, hire, dispute, plan, commit, execute, verify,
    quote, ...) as a Semantic Kernel `KernelPlugin` named "gora8", ready
    to pass straight into `kernel.add_plugin(gora8_tools(credential))` —
    a different shape from the other adapters' plain tool list, since
    SK's own idiom is plugin-based, not a flat list a caller assembles
    itself. Each function's name and description come from its own real
    docstring — the agent's own tool-calling loop understands what each
    one does and when to use it without any separate system-prompt
    engineering, the same way an MCP host does via `gora8-agent-mcp`.
    Per-argument descriptions are not populated (SK's own convention for
    those is `Annotated[T, "description"]` type hints, a different shape
    from the Google-style docstrings the other 6 adapters already parse
    correctly) — a real, disclosed gap in this integration specifically,
    not something silently dropped.

    Verified against semantic-kernel 1.44.1's `KernelPlugin`/`kernel_function`.
    """
    from semantic_kernel.functions import KernelPlugin, kernel_function

    from gora8_agent import Client

    from ._tool_functions import build_tool_functions

    client = Client(credential, base_url=base_url) if base_url else Client(credential)
    return KernelPlugin(name="gora8", functions=[kernel_function(fn) for fn in build_tool_functions(client)])
