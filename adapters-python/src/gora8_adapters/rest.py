"""Wraps an existing REST API — described by an OpenAPI 3.x spec — as a
gora8-deployable HTTP server. The 8th adapter, alongside the 7 agent
frameworks: for a SaaS company whose product is a REST API, not an
agent-framework object like a compiled LangGraph graph or a CrewAI crew.

What this does and doesn't change about the wrapped API:

- The SaaS's real API (`base_url`) is never modified and keeps requiring
  its own auth exactly as it does today for its existing customers. This
  adapter is a *new*, separate endpoint gora8 calls instead — the one
  privileged caller that holds the real API's own key and uses it on
  every request it proxies, the same way any other backend-to-backend
  integration would.
- `agent.endpoint` (wherever this adapter is hosted) has to be a public
  HTTPS URL — gora8 has no other way to reach it. That means anyone who
  finds that URL can hit it directly, bypassing gora8's payment/policy
  gate entirely, and get a free ride on the wrapped API's own key unless
  this adapter itself verifies the request actually came from gora8. See
  `require_shared_secret` below — on by default, because unlike the 7
  framework adapters, this one holds a real third-party credential a
  spoofed request could spend.

gora8's invoke gateway forwards a single `{"task": "<string>"}` envelope
(see server.py) and expects `{"result": ...}` back. Since a REST API
exposes many operations, `task` here is a JSON-encoded string —
`{"operation": "<operationId>", "params": {...}}` — not free text; a REST
call needs a specific operation and structured arguments, not a sentence
to re-parse. Override `input_mapper`/`output_mapper` for a different
convention, same as any of the framework adapters.
"""

from __future__ import annotations

import json
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Optional
from urllib.parse import urljoin

import httpx
from fastapi import Request

from .server import serve as _serve

_HTTP_METHODS = ("get", "put", "post", "delete", "patch", "head", "options", "trace")

DEFAULT_SHARED_SECRET_ENV = "GORA8_SHARED_SECRET"
DEFAULT_SHARED_SECRET_HEADER = "x-agent-secret-gora8_shared_secret"


@dataclass(frozen=True)
class Operation:
    operation_id: str
    method: str
    path: str
    summary: str = ""
    description: str = ""
    path_params: tuple = ()
    query_params: tuple = ()
    has_body: bool = False


def _load_spec(spec: Any) -> dict:
    """`spec` is an already-parsed dict, or a local file path (JSON or
    YAML, by extension/content sniffing). Fetching a spec from a URL isn't
    this function's job — fetch it yourself and pass the parsed dict, so
    this module never needs an opinion on how you want that request made
    (auth, retries, caching).
    """
    if isinstance(spec, dict):
        return spec
    text = Path(spec).read_text()
    stripped = text.lstrip()
    if stripped.startswith("{") or stripped.startswith("["):
        return json.loads(text)
    import yaml  # optional dep (gora8-adapters[rest]) — only needed for YAML specs

    return yaml.safe_load(text)


def load_operations(spec: Any) -> dict:
    """Parses an OpenAPI 3.x spec's `paths` into `operationId -> Operation`.

    Covers the common subset real-world specs use — path/query parameters
    and a single `application/json` requestBody. Doesn't validate the spec
    (that's a separate concern entirely); a malformed path/method entry is
    skipped rather than fatal, so one bad operation in a 40-operation spec
    doesn't block deploying the other 39.
    """
    doc = _load_spec(spec)
    operations: dict = {}
    for path, path_item in (doc.get("paths") or {}).items():
        if not isinstance(path_item, dict):
            continue
        shared_params = path_item.get("parameters", []) or []
        for method in _HTTP_METHODS:
            op = path_item.get(method)
            if not isinstance(op, dict):
                continue
            operation_id = op.get("operationId") or f"{method}_{path}"
            params = [*shared_params, *(op.get("parameters", []) or [])]
            path_params = tuple(
                p["name"] for p in params if isinstance(p, dict) and p.get("in") == "path" and "name" in p
            )
            query_params = tuple(
                p["name"] for p in params if isinstance(p, dict) and p.get("in") == "query" and "name" in p
            )
            operations[operation_id] = Operation(
                operation_id=operation_id,
                method=method.upper(),
                path=path,
                summary=op.get("summary", ""),
                description=op.get("description", ""),
                path_params=path_params,
                query_params=query_params,
                has_body="requestBody" in op,
            )
    return operations


def capabilities_from_spec(spec: Any, operations: Optional[list] = None) -> list:
    """Derives `agent.yaml` `capabilities:` entries (id + description) from
    an OpenAPI spec — one per operationId, or a filtered subset via
    `operations` (an allowlist of operationIds). Deliberately doesn't
    default to publishing every operation a spec declares: account
    management, billing, and admin endpoints usually don't belong on
    gora8 even when the API's actual task-shaped operations do, so the
    caller picks which operationIds to expose rather than getting all of
    them by default.
    """
    ops = load_operations(spec)
    if operations is not None:
        ops = {k: v for k, v in ops.items() if k in operations}
    return [
        {"id": op.operation_id, "description": op.summary or op.description or op.operation_id}
        for op in ops.values()
    ]


def _default_input_mapper(task: str) -> dict:
    parsed = json.loads(task)
    if "operation" not in parsed:
        raise ValueError(
            'task must be JSON with an "operation" field, e.g. '
            '\'{"operation": "textToSpeech", "params": {...}}\''
        )
    return parsed


def _default_output_mapper(status_code: int, body: Any) -> Any:
    # Mirrors how the framework adapters behave: a business-logic failure
    # (the wrapped API returning 4xx/5xx) is just part of the result
    # payload, not a different HTTP status on gora8's own /invoke response
    # — the same way a LangGraph run returning an error message doesn't
    # change /invoke's status code either. gora8's settlement only cares
    # whether the forward to agent.endpoint itself succeeded.
    if status_code >= 400:
        return {"error": True, "status": status_code, "body": body}
    return body


def _build_request(op: Operation, params: dict, base_url: str) -> dict:
    path = op.path
    for name in op.path_params:
        if name not in params:
            raise ValueError(f"{op.operation_id!r} requires path parameter {name!r}")
        path = path.replace("{" + name + "}", str(params[name]))
    query = {k: v for k, v in params.items() if k in op.query_params}
    request: dict = {
        "method": op.method,
        "url": urljoin(base_url.rstrip("/") + "/", path.lstrip("/")),
        "params": query,
    }
    if op.has_body and "body" in params:
        request["json"] = params["body"]
    return request


def _auth_headers(api_key_env: Optional[str], auth_header: str, auth_scheme: Optional[str]) -> dict:
    if not api_key_env:
        return {}
    value = os.environ.get(api_key_env)
    if not value:
        raise RuntimeError(f"{api_key_env} is not set — the wrapped API's own key is required to call it")
    return {auth_header: f"{auth_scheme} {value}" if auth_scheme else value}


def _verify_shared_secret(header_name: str, expected: str) -> Callable:
    def verify(request: Request) -> bool:
        return request.headers.get(header_name) == expected

    return verify


def build_handler(
    spec: Any,
    base_url: str,
    *,
    operations: Optional[list] = None,
    input_mapper: Optional[Callable] = None,
    output_mapper: Optional[Callable] = None,
    api_key_env: Optional[str] = None,
    auth_header: str = "Authorization",
    auth_scheme: Optional[str] = "Bearer",
    timeout: float = 15.0,
) -> Callable:
    """Builds the `task -> result` handler `serve()` runs behind `/invoke`,
    without starting a server — the seam that makes the dispatch/auth/
    request-building logic unit-testable (mock `httpx.request`, call the
    returned handler directly) without spinning up uvicorn.
    """
    ops = load_operations(spec)
    if operations is not None:
        ops = {k: v for k, v in ops.items() if k in operations}
    in_map = input_mapper or _default_input_mapper
    out_map = output_mapper or _default_output_mapper
    auth = _auth_headers(api_key_env, auth_header, auth_scheme)

    def handler(task: str) -> Any:
        parsed = in_map(task)
        operation_id = parsed["operation"]
        if operation_id not in ops:
            raise ValueError(f"unknown operation {operation_id!r} — declared: {sorted(ops)}")
        request = _build_request(ops[operation_id], parsed.get("params", {}) or {}, base_url)
        response = httpx.request(headers=auth, timeout=timeout, **request)
        try:
            body = response.json()
        except ValueError:
            body = response.text
        return out_map(response.status_code, body)

    return handler


def serve(
    spec: Any,
    base_url: str,
    *,
    operations: Optional[list] = None,
    input_mapper: Optional[Callable] = None,
    output_mapper: Optional[Callable] = None,
    api_key_env: Optional[str] = None,
    auth_header: str = "Authorization",
    auth_scheme: Optional[str] = "Bearer",
    shared_secret_env: str = DEFAULT_SHARED_SECRET_ENV,
    shared_secret_header: str = DEFAULT_SHARED_SECRET_HEADER,
    require_shared_secret: bool = True,
    timeout: float = 15.0,
    host: str = "0.0.0.0",
    port: int = 8000,
) -> None:
    """Serves an OpenAPI-described REST API as a gora8-deployable HTTP
    server. `base_url` is the wrapped API's real base URL — never gora8's.

    Example (ElevenLabs-shaped):
        from gora8_adapters.rest import serve

        serve(
            "./elevenlabs-openapi.json",
            base_url="https://api.elevenlabs.io",
            operations=["textToSpeech"],  # curate, don't expose the whole API
            api_key_env="XI_API_KEY",     # ElevenLabs' own key, never gora8's
            auth_header="xi-api-key",
            auth_scheme=None,             # raw key, no "Bearer " prefix
        )

    `require_shared_secret=True` (the default) fails fast at startup if
    `shared_secret_env` isn't set — set the same random value as an
    `AgentSecret` named `GORA8_SHARED_SECRET` in the gora8 dashboard and as
    this env var here, so every genuine forward from gora8 carries it
    (gora8 injects `x-agent-secret-<key>` for every configured secret) and
    this adapter can tell a real gora8-routed call from someone hitting
    its public URL directly. Pass `require_shared_secret=False` only for
    local testing against a stubbed base_url with no real credential
    behind it.
    """
    handler = build_handler(
        spec,
        base_url,
        operations=operations,
        input_mapper=input_mapper,
        output_mapper=output_mapper,
        api_key_env=api_key_env,
        auth_header=auth_header,
        auth_scheme=auth_scheme,
        timeout=timeout,
    )

    verify_request = None
    if require_shared_secret:
        expected = os.environ.get(shared_secret_env)
        if not expected:
            raise RuntimeError(
                f"{shared_secret_env} is not set. This adapter holds a real credential for the wrapped "
                "API, so a request must prove it actually came through gora8's payment/policy gate before "
                f"this adapter will spend that credential on it — set an AgentSecret named "
                f"{shared_secret_env!r} in the gora8 dashboard, set the same value as this env var here, "
                "or pass require_shared_secret=False to accept any request (local testing only)."
            )
        verify_request = _verify_shared_secret(shared_secret_header, expected)

    _serve(handler, host=host, port=port, verify_request=verify_request)


def _main() -> None:
    import argparse

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--spec", required=True, help="Path to an OpenAPI spec (JSON or YAML)")
    parser.add_argument(
        "--operations",
        nargs="*",
        default=None,
        help="operationIds to expose (default: every operation the spec declares)",
    )
    args = parser.parse_args()
    caps = capabilities_from_spec(args.spec, operations=args.operations)
    print("capabilities:")
    for cap in caps:
        print(f"  - id: {cap['id']}")
        print(f"    description: {cap['description']!r}")


if __name__ == "__main__":
    _main()
