"""Verifies the REST adapter's actual wiring (spec parsing, request
building, auth headers, shared-secret gate) without calling a real
third-party API. Run this file directly, then hit it with curl.

Terminal 1:
    XI_API_KEY=fake-key GORA8_SHARED_SECRET=dev-secret \
      python examples/rest_fake_test.py

Terminal 2 — rejected, no shared secret (simulates someone hitting the
public URL directly instead of going through gora8):
    curl -s -o /dev/null -w '%{http_code}\\n' -X POST localhost:8000/invoke \\
      -H 'content-type: application/json' \\
      -d '{"task": "{\\"operation\\": \\"textToSpeech\\", \\"params\\": {\\"voice_id\\": \\"abc\\", \\"body\\": {\\"text\\": \\"hi\\"}}}"}'
    # -> 401

Terminal 2 — accepted (simulates gora8's forward, which would carry this
header automatically because GORA8_SHARED_SECRET was set as an
AgentSecret):
    curl -s -X POST localhost:8000/invoke \\
      -H 'content-type: application/json' \\
      -H 'x-agent-secret-gora8_shared_secret: dev-secret' \\
      -d '{"task": "{\\"operation\\": \\"textToSpeech\\", \\"params\\": {\\"voice_id\\": \\"abc\\", \\"body\\": {\\"text\\": \\"hi\\"}}}"}'
    # -> real outbound call to https://api.elevenlabs.io, which will fail
    # with a real HTTP error since XI_API_KEY=fake-key isn't a real key —
    # that failure surfaces as {"result": {"error": true, "status": ...}},
    # confirming the request was built and dispatched correctly end to
    # end, right up to the wrapped API's own auth rejecting the fake key.
"""

ELEVENLABS_SPEC = {
    "openapi": "3.0.0",
    "paths": {
        "/v1/text-to-speech/{voice_id}": {
            "post": {
                "operationId": "textToSpeech",
                "summary": "Convert text to speech in the given voice",
                "parameters": [
                    {"name": "voice_id", "in": "path", "required": True, "schema": {"type": "string"}},
                ],
                "requestBody": {"content": {"application/json": {"schema": {"type": "object"}}}},
            },
        },
    },
}

if __name__ == "__main__":
    from gora8_adapters.rest import serve

    serve(
        ELEVENLABS_SPEC,
        base_url="https://api.elevenlabs.io",
        operations=["textToSpeech"],
        api_key_env="XI_API_KEY",
        auth_header="xi-api-key",
        auth_scheme=None,
    )
