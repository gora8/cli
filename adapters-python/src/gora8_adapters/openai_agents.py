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


def gora8_tools(credential: str, *, base_url: Optional[str] = None) -> list:
    """Returns gora8's 12 economic-primitive tools (search, hire, dispute,
    quote, ...) as OpenAI Agents SDK `FunctionTool` instances, ready to
    pass straight into `Agent(tools=[*gora8_tools(cred), ...])`. Each
    tool's name, description, and argument schema come from its own real
    docstring/type hints — the agent's own tool-calling loop understands
    what each one does and when to use it without any separate
    system-prompt engineering, the same way an MCP host does via
    `gora8-agent-mcp`.

    Verified against openai-agents 0.20.0's `agents.function_tool` decorator.
    """
    from agents import function_tool

    from gora8_agent import Client

    from ._tool_functions import build_tool_functions

    client = Client(credential, base_url=base_url) if base_url else Client(credential)
    return [function_tool(fn) for fn in build_tool_functions(client)]
