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
from typing import Any, Callable, Optional

import uvicorn
from fastapi import FastAPI, HTTPException, Request
from starlette.concurrency import run_in_threadpool


def build_app(
    handler: Callable[[str], Any],
    verify_request: Optional[Callable[[Request], bool]] = None,
) -> FastAPI:
    """Builds a FastAPI app with a single POST /invoke route.

    `handler` receives the task string and returns a JSON-serializable
    result (or a coroutine that does). Sync handlers run in a threadpool
    so a single blocking call (e.g. `graph.invoke(...)`) doesn't stall
    other requests.

    `verify_request`, if given, runs before `handler` on every call and
    must return `True` to let the request through — a 401 is returned
    otherwise. None of the 7 framework adapters need this today (their
    handler doesn't hold a third-party credential a spoofed request could
    spend), but `rest.py` does: `agent.endpoint` must be a public HTTPS URL
    (gora8 has no other way to reach it), so without a check like this,
    anyone who finds that URL directly can spend the wrapped API's own key
    for free, bypassing gora8's payment/policy gate entirely.
    """

    @asynccontextmanager
    async def lifespan(_app: FastAPI):
        yield

    app = FastAPI(lifespan=lifespan)

    @app.post("/invoke")
    async def invoke(request: Request) -> dict:
        if verify_request is not None and not verify_request(request):
            raise HTTPException(status_code=401, detail="unauthorized")
        body = await request.json()
        task = body.get("task", "")
        if inspect.iscoroutinefunction(handler):
            result = await handler(task)
        else:
            result = await run_in_threadpool(handler, task)
        return {"result": result}

    return app


def serve(
    handler: Callable[[str], Any],
    host: str = "0.0.0.0",
    port: int = 8000,
    verify_request: Optional[Callable[[Request], bool]] = None,
) -> None:
    """Builds and runs the server. Call this from a `if __name__ == "__main__"` block."""
    app = build_app(handler, verify_request=verify_request)
    uvicorn.run(app, host=host, port=port)
