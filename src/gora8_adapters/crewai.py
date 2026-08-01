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
