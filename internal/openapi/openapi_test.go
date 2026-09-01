package openapi

import (
	"os"
	"path/filepath"
	"testing"
)

const elevenLabsSpec = `{
  "openapi": "3.0.0",
  "paths": {
    "/v1/text-to-speech/{voice_id}": {
      "post": {
        "operationId": "textToSpeech",
        "summary": "Convert text to speech in the given voice"
      }
    },
    "/v1/voices": {
      "get": {
        "summary": "List available voices"
      }
    },
    "/v1/user": {
      "get": {
        "operationId": "getUser",
        "summary": "Account details"
      }
    }
  }
}`

func writeSpec(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "openapi.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_ParsesOperationIDAndFallback(t *testing.T) {
	path := writeSpec(t, elevenLabsSpec)
	ops, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 3 {
		t.Fatalf("expected 3 operations, got %d: %v", len(ops), ops)
	}
	tts, ok := ops["textToSpeech"]
	if !ok {
		t.Fatal("expected textToSpeech operation")
	}
	if tts.Method != "POST" || tts.Path != "/v1/text-to-speech/{voice_id}" {
		t.Errorf("unexpected operation: %+v", tts)
	}

	// No operationId declared on GET /v1/voices — falls back to method_path.
	fallback, ok := ops["get_/v1/voices"]
	if !ok {
		t.Fatalf("expected get_/v1/voices fallback key, got keys: %v", keys(ops))
	}
	if fallback.Summary != "List available voices" {
		t.Errorf("unexpected fallback operation: %+v", fallback)
	}
}

func TestLoad_RejectsSpecWithNoPaths(t *testing.T) {
	path := writeSpec(t, `{"openapi": "3.0.0"}`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for a spec with no paths")
	}
}

func TestLoad_SkipsMalformedPathItemsInsteadOfFailing(t *testing.T) {
	path := writeSpec(t, `{"paths": {"/ok": {"get": {"operationId": "ok"}}, "/bad": "not an object"}}`)
	ops, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected only the well-formed operation, got %v", keys(ops))
	}
}

func TestLoad_ParsesYAMLByContentNotExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openapi.yaml")
	yamlSpec := "paths:\n  /v1/ping:\n    get:\n      operationId: ping\n      summary: Health check\n"
	if err := os.WriteFile(path, []byte(yamlSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	ops, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if ops["ping"].Summary != "Health check" {
		t.Errorf("unexpected YAML-parsed operation: %+v", ops["ping"])
	}
}

func TestCapabilities_DefaultsToEveryOperationSortedByID(t *testing.T) {
	path := writeSpec(t, elevenLabsSpec)
	caps, err := Capabilities(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) != 3 {
		t.Fatalf("expected 3 capabilities, got %d", len(caps))
	}
	// Alphabetical, byte-wise: "getUser" < "get_/v1/voices" ('U' is 0x55,
	// '_' is 0x5F) < "textToSpeech".
	got := []string{caps[0].ID, caps[1].ID, caps[2].ID}
	want := []string{"getUser", "get_/v1/voices", "textToSpeech"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("capabilities not sorted deterministically: got %v, want %v", got, want)
			break
		}
	}
}

func TestCapabilities_AllowlistFiltersAndPreservesRequestedOrder(t *testing.T) {
	path := writeSpec(t, elevenLabsSpec)
	caps, err := Capabilities(path, []string{"getUser", "textToSpeech"})
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) != 2 || caps[0].ID != "getUser" || caps[1].ID != "textToSpeech" {
		t.Fatalf("expected [getUser, textToSpeech] in that order, got %v", caps)
	}
}

func TestCapabilities_ErrorsOnUnknownOperationID(t *testing.T) {
	path := writeSpec(t, elevenLabsSpec)
	_, err := Capabilities(path, []string{"textToSpeech", "deleteEverything"})
	if err == nil {
		t.Fatal("expected an error for an operationId not in the spec")
	}
}

func TestCapabilities_DescriptionFallsBackToOperationID(t *testing.T) {
	path := writeSpec(t, `{"paths": {"/x": {"get": {"operationId": "bare"}}}}`)
	caps, err := Capabilities(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) != 1 || caps[0].Description != "bare" {
		t.Fatalf("expected description to fall back to the operationId, got %+v", caps)
	}
}

func keys(m map[string]Operation) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
