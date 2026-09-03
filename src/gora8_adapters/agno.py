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


def gora8_tools(credential: str, *, base_url: Optional[str] = None) -> list:
    """Returns gora8's 16 economic-primitive tools (search, hire, dispute, plan, commit, execute, verify,
    quote, ...) as Agno `Function` instances (via `agno.tools.tool`),
    ready to pass straight into `Agent(tools=[*gora8_tools(cred), ...])`.
    Each tool's name, description, and argument schema come from its own
    real docstring/type hints — the agent's own tool-calling loop
    understands what each one does and when to use it without any
    separate system-prompt engineering, the same way an MCP host does
    via `gora8-agent-mcp`.

    Verified against agno 2.9.0's `agno.tools.tool` decorator.
    """
    from agno.tools import tool as agno_tool

    from gora8_agent import Client

    from ._tool_functions import build_tool_functions

    client = Client(credential, base_url=base_url) if base_url else Client(credential)
    return [agno_tool(fn) for fn in build_tool_functions(client)]
