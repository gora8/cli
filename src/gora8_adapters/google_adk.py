"""Wraps a Google Agent Development Kit (ADK) `Agent` as a gora8-deployable
HTTP server.

Verified against google-adk 2.6.1 (ADK 2.0+ — note this SDK had a breaking
API change vs 1.x; anything showing `Runner.run()` used synchronously or
`create_session()` called without `await` is pre-2.0 and stale). ADK is
async-only and session-based; this adapter creates one throwaway in-memory
session per request since a stateless "task in, result out" HTTP call has no
use for multi-turn history.
"""

from __future__ import annotations

import asyncio
import uuid
from typing import Any, Callable, Optional

from .server import serve as _serve

_APP_NAME = "gora8-adapter"


def _default_output_mapper(text: str) -> Any:
    return text


def serve(
    agent,
    input_mapper: Optional[Callable[[str], str]] = None,
    output_mapper: Optional[Callable[[str], Any]] = None,
    host: str = "0.0.0.0",
    port: int = 8000,
) -> None:
    """Serves a Google ADK `Agent` instance.

    Example:
        from google.adk.agents import Agent
        from gora8_adapters.google_adk import serve

        agent = Agent(name="assistant", model="gemini-2.0-flash", instruction="You are helpful.")
        serve(agent)
    """
    from google.adk.runners import Runner
    from google.adk.sessions import InMemorySessionService
    from google.genai import types

    session_service = InMemorySessionService()
    runner = Runner(agent=agent, app_name=_APP_NAME, session_service=session_service)
    in_map = input_mapper or (lambda task: task)
    out_map = output_mapper or _default_output_mapper

    async def run_once(task: str) -> str:
        user_id = "gora8"
        session_id = str(uuid.uuid4())
        await session_service.create_session(app_name=_APP_NAME, user_id=user_id, session_id=session_id)
        content = types.Content(role="user", parts=[types.Part(text=in_map(task))])
        final_text = ""
        async for event in runner.run_async(user_id=user_id, session_id=session_id, new_message=content):
            if event.is_final_response() and event.content and event.content.parts:
                final_text = "".join(p.text or "" for p in event.content.parts)
        return final_text

    def handler(task: str) -> Any:
        return out_map(asyncio.run(run_once(task)))

    _serve(handler, host=host, port=port)
