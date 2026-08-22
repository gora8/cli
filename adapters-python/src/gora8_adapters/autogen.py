"""Wraps an AutoGen `AssistantAgent` as a gora8-deployable HTTP server.

Targets Microsoft's official `autogen-agentchat` package (the AutoGen 0.4+
architecture). Not `ag2` — as of AG2 v1.0, its classic `ConversableAgent`/
`import autogen` API split out into a separate `ag2-classic` package, so it's
a different, incompatible API today. Not legacy `pyautogen~=0.2.0` either —
current `pyautogen` on PyPI is itself just a proxy that depends on
`autogen-agentchat`, confirming that's the real current target.

`agent.run(task=task)` is async, returns a `TaskResult` whose final reply is
`messages[-1].content`.
"""

from __future__ import annotations

import asyncio
from typing import Any, Callable, Optional

from .server import serve as _serve


def _default_output_mapper(result: Any) -> Any:
    return result.messages[-1].content


def serve(
    agent,
    input_mapper: Optional[Callable[[str], str]] = None,
    output_mapper: Optional[Callable[[Any], Any]] = None,
    host: str = "0.0.0.0",
    port: int = 8000,
) -> None:
    """Serves an autogen-agentchat `AssistantAgent` instance.

    Example:
        from autogen_agentchat.agents import AssistantAgent
        from autogen_ext.models.openai import OpenAIChatCompletionClient
        from gora8_adapters.autogen import serve

        model_client = OpenAIChatCompletionClient(model="gpt-4o")
        agent = AssistantAgent(name="assistant", model_client=model_client)
        serve(agent)
    """
    in_map = input_mapper or (lambda task: task)
    out_map = output_mapper or _default_output_mapper

    def handler(task: str) -> Any:
        result = asyncio.run(agent.run(task=in_map(task)))
        return out_map(result)

    _serve(handler, host=host, port=port)
