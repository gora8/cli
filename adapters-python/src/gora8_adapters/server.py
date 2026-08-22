"""Shared HTTP server for gora8 runtime adapters.

gora8's invoke gateway enforces no envelope on an agent's endpoint — it
forwards whatever JSON body it receives and returns whatever JSON comes
back. This module honors the schema already declared for discovery
(`{"task": "string"}` in, `{"result": ...}` out) so agents built with an
adapter show correct example input/output without extra config, but that
schema is a convention here, not something gora8 enforces on the wire.
"""

from __future__ import annotations

import inspect
from contextlib import asynccontextmanager
from typing import Any, Callable

import uvicorn
from fastapi import FastAPI, Request
from starlette.concurrency import run_in_threadpool


def build_app(handler: Callable[[str], Any]) -> FastAPI:
    """Builds a FastAPI app with a single POST /invoke route.

    `handler` receives the task string and returns a JSON-serializable
    result (or a coroutine that does). Sync handlers run in a threadpool
    so a single blocking call (e.g. `graph.invoke(...)`) doesn't stall
    other requests.
    """

    @asynccontextmanager
    async def lifespan(_app: FastAPI):
        yield

    app = FastAPI(lifespan=lifespan)

    @app.post("/invoke")
    async def invoke(request: Request) -> dict:
        body = await request.json()
        task = body.get("task", "")
        if inspect.iscoroutinefunction(handler):
            result = await handler(task)
        else:
            result = await run_in_threadpool(handler, task)
        return {"result": result}

    return app


def serve(handler: Callable[[str], Any], host: str = "0.0.0.0", port: int = 8000) -> None:
    """Builds and runs the server. Call this from a `if __name__ == "__main__"` block."""
    app = build_app(handler)
    uvicorn.run(app, host=host, port=port)
