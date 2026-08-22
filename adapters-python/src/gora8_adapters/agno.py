"""Wraps an Agno `Agent` as a gora8-deployable HTTP server.

Verified against agno 2.8.6 (the current package — `phidata` on PyPI is a
frozen legacy snapshot from Jan 2025, the project renamed to Agno).
`agent.run(input=task)` returns a `RunOutput` (not `RunResponse`, the name
used in a lot of stale docs/blogs predating this rename) with the final text
on `.content`.
"""

from __future__ import annotations

from typing import Any, Callable, Optional

from .server import serve as _serve


def _default_output_mapper(response: Any) -> Any:
    return response.content


def serve(
    agent,
    input_mapper: Optional[Callable[[str], Any]] = None,
    output_mapper: Optional[Callable[[Any], Any]] = None,
    host: str = "0.0.0.0",
    port: int = 8000,
) -> None:
    """Serves an Agno `Agent` instance.

    Example:
        from agno.agent import Agent
        from gora8_adapters.agno import serve

        agent = Agent(name="assistant")
        serve(agent)
    """
    in_map = input_mapper or (lambda task: task)
    out_map = output_mapper or _default_output_mapper

    def handler(task: str) -> Any:
        response = agent.run(input=in_map(task))
        return out_map(response)

    _serve(handler, host=host, port=port)
