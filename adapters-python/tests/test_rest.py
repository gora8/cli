"""Tests for gora8_adapters.rest — the OpenAPI-wrapping REST adapter.

Covers spec parsing, capability derivation, request building, auth header
construction, the shared-secret verification gate, and the handler's
end-to-end dispatch (with httpx.request monkeypatched, no real network
calls). An ElevenLabs-shaped spec is used as the fixture throughout since
it's the running example from the design conversation and exercises a
realistic path parameter + JSON body + raw (non-Bearer) API key header.
"""

import json

import httpx
import pytest
from fastapi.testclient import TestClient

from gora8_adapters import rest
from gora8_adapters.server import build_app

ELEVENLABS_SPEC = {
    "openapi": "3.0.0",
    "paths": {
        "/v1/text-to-speech/{voice_id}": {
            "post": {
                "operationId": "textToSpeech",
                "summary": "Convert text to speech in the given voice",
                "parameters": [
                    {"name": "voice_id", "in": "path", "required": True, "schema": {"type": "string"}},
                    {"name": "output_format", "in": "query", "schema": {"type": "string"}},
                ],
                "requestBody": {
                    "content": {"application/json": {"schema": {"type": "object"}}},
                },
            },
        },
        "/v1/voices": {
            "get": {
                # No operationId on purpose — exercises the method_path fallback.
                "summary": "List available voices",
            },
        },
    },
}


# ---------------------------------------------------------------------------
# load_operations / capabilities_from_spec
# ---------------------------------------------------------------------------


def test_load_operations_parses_path_and_query_params():
    ops = rest.load_operations(ELEVENLABS_SPEC)
    tts = ops["textToSpeech"]
    assert tts.method == "POST"
    assert tts.path == "/v1/text-to-speech/{voice_id}"
    assert tts.path_params == ("voice_id",)
    assert tts.query_params == ("output_format",)
    assert tts.has_body is True


def test_load_operations_falls_back_to_method_path_when_no_operation_id():
    ops = rest.load_operations(ELEVENLABS_SPEC)
    assert "get_/v1/voices" in ops
    assert ops["get_/v1/voices"].has_body is False


def test_capabilities_from_spec_defaults_to_every_operation():
    caps = rest.capabilities_from_spec(ELEVENLABS_SPEC)
    ids = {c["id"] for c in caps}
    assert ids == {"textToSpeech", "get_/v1/voices"}
    tts_cap = next(c for c in caps if c["id"] == "textToSpeech")
    assert tts_cap["description"] == "Convert text to speech in the given voice"


def test_capabilities_from_spec_respects_allowlist():
    caps = rest.capabilities_from_spec(ELEVENLABS_SPEC, operations=["textToSpeech"])
    assert [c["id"] for c in caps] == ["textToSpeech"]


def test_load_operations_skips_malformed_entries_instead_of_raising():
    spec = {
        "paths": {
            "/ok": {"get": {"operationId": "ok"}},
            "/bad": "not a dict",
        }
    }
    ops = rest.load_operations(spec)
    assert list(ops) == ["ok"]


# ---------------------------------------------------------------------------
# _build_request
# ---------------------------------------------------------------------------


def test_build_request_substitutes_path_param_and_splits_query_and_body():
    op = rest.load_operations(ELEVENLABS_SPEC)["textToSpeech"]
    request = rest._build_request(
        op,
        {"voice_id": "abc123", "output_format": "mp3", "body": {"text": "hello"}},
        base_url="https://api.elevenlabs.io",
    )
    assert request["method"] == "POST"
    assert request["url"] == "https://api.elevenlabs.io/v1/text-to-speech/abc123"
    assert request["params"] == {"output_format": "mp3"}
    assert request["json"] == {"text": "hello"}


def test_build_request_raises_on_missing_required_path_param():
    op = rest.load_operations(ELEVENLABS_SPEC)["textToSpeech"]
    with pytest.raises(ValueError, match="voice_id"):
        rest._build_request(op, {}, base_url="https://api.elevenlabs.io")


# ---------------------------------------------------------------------------
# _auth_headers
# ---------------------------------------------------------------------------


def test_auth_headers_bearer_default(monkeypatch):
    monkeypatch.setenv("MY_KEY", "secret123")
    headers = rest._auth_headers("MY_KEY", "Authorization", "Bearer")
    assert headers == {"Authorization": "Bearer secret123"}


def test_auth_headers_raw_scheme_for_apis_like_elevenlabs(monkeypatch):
    monkeypatch.setenv("XI_API_KEY", "xi-secret")
    headers = rest._auth_headers("XI_API_KEY", "xi-api-key", None)
    assert headers == {"xi-api-key": "xi-secret"}


def test_auth_headers_raises_if_env_var_unset(monkeypatch):
    monkeypatch.delenv("MISSING_KEY", raising=False)
    with pytest.raises(RuntimeError, match="MISSING_KEY"):
        rest._auth_headers("MISSING_KEY", "Authorization", "Bearer")


def test_auth_headers_empty_when_no_api_key_env():
    assert rest._auth_headers(None, "Authorization", "Bearer") == {}


# ---------------------------------------------------------------------------
# build_handler — end-to-end dispatch, httpx.request mocked
# ---------------------------------------------------------------------------


def test_handler_dispatches_to_the_right_operation_and_returns_json_body(monkeypatch):
    captured = {}

    def fake_request(*, method, url, params, timeout, headers, json=None):
        captured.update(method=method, url=url, params=params, headers=headers, json=json)
        return httpx.Response(200, json={"audio_url": "https://cdn.example/out.mp3"})

    monkeypatch.setattr(httpx, "request", fake_request)
    monkeypatch.setenv("XI_API_KEY", "xi-secret")

    handler = rest.build_handler(
        ELEVENLABS_SPEC,
        base_url="https://api.elevenlabs.io",
        api_key_env="XI_API_KEY",
        auth_header="xi-api-key",
        auth_scheme=None,
    )
    task = json.dumps({"operation": "textToSpeech", "params": {"voice_id": "abc123", "body": {"text": "hi"}}})
    result = handler(task)

    assert result == {"audio_url": "https://cdn.example/out.mp3"}
    assert captured["method"] == "POST"
    assert captured["url"] == "https://api.elevenlabs.io/v1/text-to-speech/abc123"
    assert captured["headers"] == {"xi-api-key": "xi-secret"}
    assert captured["json"] == {"text": "hi"}


def test_handler_raises_on_unknown_operation():
    handler = rest.build_handler(ELEVENLABS_SPEC, base_url="https://api.elevenlabs.io", operations=["textToSpeech"])
    task = json.dumps({"operation": "deleteEverything", "params": {}})
    with pytest.raises(ValueError, match="unknown operation"):
        handler(task)


def test_handler_default_output_mapper_wraps_error_status(monkeypatch):
    def fake_request(*, method, url, params, timeout, headers, json=None):
        return httpx.Response(429, json={"message": "rate limited"})

    monkeypatch.setattr(httpx, "request", fake_request)
    handler = rest.build_handler(ELEVENLABS_SPEC, base_url="https://api.elevenlabs.io", operations=["textToSpeech"])
    task = json.dumps({"operation": "textToSpeech", "params": {"voice_id": "v1", "body": {"text": "hi"}}})
    result = handler(task)
    assert result == {"error": True, "status": 429, "body": {"message": "rate limited"}}


def test_handler_input_mapper_must_include_operation_field():
    handler = rest.build_handler(ELEVENLABS_SPEC, base_url="https://api.elevenlabs.io")
    with pytest.raises(ValueError, match="operation"):
        handler(json.dumps({"params": {}}))


# ---------------------------------------------------------------------------
# serve() fail-fast on a missing shared secret — must never reach uvicorn
# ---------------------------------------------------------------------------


def test_serve_fails_fast_when_shared_secret_env_unset(monkeypatch):
    monkeypatch.delenv(rest.DEFAULT_SHARED_SECRET_ENV, raising=False)
    with pytest.raises(RuntimeError, match=rest.DEFAULT_SHARED_SECRET_ENV):
        rest.serve(ELEVENLABS_SPEC, base_url="https://api.elevenlabs.io", require_shared_secret=True)


# ---------------------------------------------------------------------------
# server.py's verify_request gate, exercised through a real ASGI request
# ---------------------------------------------------------------------------


def test_build_app_rejects_request_missing_shared_secret_header():
    verify = rest._verify_shared_secret(rest.DEFAULT_SHARED_SECRET_HEADER, "expected-value")
    app = build_app(lambda task: {"echo": task}, verify_request=verify)
    client = TestClient(app)

    resp = client.post("/invoke", json={"task": "hi"})
    assert resp.status_code == 401

    resp = client.post(
        "/invoke",
        json={"task": "hi"},
        headers={rest.DEFAULT_SHARED_SECRET_HEADER: "wrong-value"},
    )
    assert resp.status_code == 401

    resp = client.post(
        "/invoke",
        json={"task": "hi"},
        headers={rest.DEFAULT_SHARED_SECRET_HEADER: "expected-value"},
    )
    assert resp.status_code == 200
    assert resp.json() == {"result": {"echo": "hi"}}


def test_build_app_with_no_verify_request_is_unchanged():
    """Existing framework adapters pass no verify_request — confirms this
    change is backward compatible."""
    app = build_app(lambda task: {"echo": task})
    client = TestClient(app)
    resp = client.post("/invoke", json={"task": "hi"})
    assert resp.status_code == 200
    assert resp.json() == {"result": {"echo": "hi"}}
