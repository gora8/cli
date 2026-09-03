"""Wraps a compiled LangGraph graph as a gora8-deployable HTTP server.

LangGraph's state schema is per-graph, not universal — most tutorials use a
`messages`-keyed `MessagesState`, but a graph is free to define arbitrary
state. The defaults here assume the `messages` convention; pass
`input_mapper`/`output_mapper` if your graph uses a different schema.
"""

from __future__ import annotations

from typing import Any, Callable, Optional

from .server import serve as _serve


def _default_input_mapper(task: str) -> dict:
    return {"messages": [{"role": "user", "content": task}]}


def _default_output_mapper(state: dict) -> Any:
    messages = state.get("messages", [])
    if not messages:
        return state
    last = messages[-1]
    return getattr(last, "content", None) or (last.get("content") if isinstance(last, dict) else last)


def serve(
    graph,
    input_mapper: Optional[Callable[[str], dict]] = None,
    output_mapper: Optional[Callable[[dict], Any]] = None,
    host: str = "0.0.0.0",
    port: int = 8000,
) -> None:
    """Serves a compiled LangGraph graph (a `StateGraph(...).compile()` result).

    Example:
        from gora8_adapters.langgraph import serve
        serve(my_compiled_graph)
    """
    in_map = input_mapper or _default_input_mapper
    out_map = output_mapper or _default_output_mapper

    def handler(task: str) -> Any:
        result_state = graph.invoke(in_map(task))
        return out_map(result_state)

    _serve(handler, host=host, port=port)


def gora8_tools(credential: str, *, base_url: Optional[str] = None) -> list:
    """Returns gora8's 16 economic-primitive tools (search, hire, dispute, plan, commit, execute, verify,
    quote, ...) as LangChain `StructuredTool` instances, ready to pass
    straight into `create_react_agent(model, tools=[*gora8_tools(cred), ...])`
    or any other LangGraph/LangChain tool list. Each tool's name,
    description, and argument schema come from its own real docstring
    (`parse_docstring=True`) — the agent's own tool-calling loop
    understands what each one does and when to use it without any
    separate system-prompt engineering, the same way an MCP host does
    via `gora8-agent-mcp`.

    Verified against langchain-core 1.6.0's `StructuredTool.from_function`.
    """
    from langchain_core.tools import StructuredTool

    from gora8_agent import Client

    from ._tool_functions import build_tool_functions

    client = Client(credential, base_url=base_url) if base_url else Client(credential)
    return [StructuredTool.from_function(fn, parse_docstring=True) for fn in build_tool_functions(client)]
