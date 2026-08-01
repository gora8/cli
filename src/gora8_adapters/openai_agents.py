"""Wraps an OpenAI Agents SDK `Agent` as a gora8-deployable HTTP server.

Verified against openai-agents 0.19.1: `Runner.run_sync(agent, input)` takes
a plain string and returns a `RunResult` whose `.final_output` holds the
final text — stable across recent 0.x releases.
"""

from __future__ import annotations

from typing import Any, Callable, Optional

from .server import serve as _serve


def _default_output_mapper(result: Any) -> Any:
    return result.final_output


def serve(
    agent,
    input_mapper: Optional[Callable[[str], Any]] = None,
    output_mapper: Optional[Callable[[Any], Any]] = None,
    host: str = "0.0.0.0",
    port: int = 8000,
) -> None:
    """Serves an OpenAI Agents SDK `Agent` instance.

    Example:
        from agents import Agent
        from gora8_adapters.openai_agents import serve

        agent = Agent(name="assistant", instructions="You are helpful.")
        serve(agent)
    """
    from agents import Runner

    in_map = input_mapper or (lambda task: task)
    out_map = output_mapper or _default_output_mapper

    def handler(task: str) -> Any:
        result = Runner.run_sync(agent, in_map(task))
        return out_map(result)

    _serve(handler, host=host, port=port)
