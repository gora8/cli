"""Wraps a CrewAI Crew as a gora8-deployable HTTP server.

CrewAI's `kickoff(inputs=...)` interpolates `inputs` into task/agent prompt
templates by variable name — the default here passes `{"task": task}`, so
your crew's task descriptions should reference `{task}`. Pass
`input_mapper`/`output_mapper` if your crew uses different variable names.
"""

from __future__ import annotations

from typing import Any, Callable, Optional

from .server import serve as _serve


def _default_input_mapper(task: str) -> dict:
    return {"task": task}


def _default_output_mapper(crew_output: Any) -> Any:
    # `.raw` is always populated per CrewAI's CrewOutput model, regardless of
    # whether the crew used structured JSON/Pydantic output.
    return getattr(crew_output, "raw", None) or str(crew_output)


def serve(
    crew,
    input_mapper: Optional[Callable[[str], dict]] = None,
    output_mapper: Optional[Callable[[Any], Any]] = None,
    host: str = "0.0.0.0",
    port: int = 8000,
) -> None:
    """Serves a CrewAI `Crew` instance.

    Example:
        from gora8_adapters.crewai import serve
        serve(my_crew)
    """
    in_map = input_mapper or _default_input_mapper
    out_map = output_mapper or _default_output_mapper

    def handler(task: str) -> Any:
        crew_output = crew.kickoff(inputs=in_map(task))
        return out_map(crew_output)

    _serve(handler, host=host, port=port)


def gora8_tools(credential: str, *, base_url: Optional[str] = None) -> list:
    """Returns gora8's 16 economic-primitive tools (search, hire, dispute, plan, commit, execute, verify,
    quote, ...) as CrewAI `Tool` instances (via `crewai.tools.tool`),
    ready to pass straight into an `Agent(tools=[*gora8_tools(cred), ...])`.
    Each tool's name, description, and argument schema come from its own
    real docstring/type hints — the agent's own tool-calling loop
    understands what each one does and when to use it without any
    separate system-prompt engineering, the same way an MCP host does
    via `gora8-agent-mcp`.

    Verified against crewai 1.15.17's `crewai.tools.tool` decorator —
    note this module's tool functions are deliberately NOT declared
    under `from __future__ import annotations` (see
    `_tool_functions.py`'s own comment): CrewAI's pydantic-based schema
    builder needs live type objects, not deferred string annotations.
    """
    from crewai.tools import tool as crewai_tool

    from gora8_agent import Client

    from ._tool_functions import build_tool_functions

    client = Client(credential, base_url=base_url) if base_url else Client(credential)
    return [crewai_tool(fn) for fn in build_tool_functions(client)]
