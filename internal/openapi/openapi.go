// Package openapi derives agent.yaml capabilities from an existing OpenAPI
// spec, for `gora8 deploy --from-openapi` — so a SaaS company with a
// multi-operation REST API doesn't hand-author `capabilities:` one entry
// at a time.
//
// This package deliberately does NOT parse parameters, request bodies, or
// build HTTP requests — that's the job of adapters-python's rest.py
// (gora8_adapters.rest), which is what actually runs the wrapper and
// calls the wrapped API. This package only needs enough of the spec to
// answer "what operations exist, and what should each be called and
// described as a capability" — the CLI never calls the wrapped API
// itself, so it never needs to know how to.
package openapi

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var httpMethods = []string{"get", "put", "post", "delete", "patch", "head", "options", "trace"}

// Operation is the subset of an OpenAPI operation this package cares
// about — just enough to name and describe a capability.
type Operation struct {
	OperationID string
	Method      string
	Path        string
	Summary     string
	Description string
}

// Capability is a plain id+description pair — deliberately not
// card.YAMLCapability, so this package stays decoupled from the CLI's
// specific manifest shape; callers convert at the call site.
type Capability struct {
	ID          string
	Description string
}

// Load reads and parses an OpenAPI spec from a local file path (JSON or
// YAML, detected by content, not extension — the same convention
// adapters-python's rest.py uses) and returns every operation it
// declares, keyed by operationId (or a "method_path" fallback for
// operations that don't declare one). A malformed path or method entry is
// skipped, not fatal — one bad operation in a large spec shouldn't block
// deploying the other 39.
func Load(path string) (map[string]Operation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var doc map[string]interface{}
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse %s as JSON: %w", path, err)
		}
	} else {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse %s as YAML: %w", path, err)
		}
	}

	paths, ok := doc["paths"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s has no top-level \"paths\" object — not a valid OpenAPI spec", path)
	}

	operations := make(map[string]Operation)
	for p, rawPathItem := range paths {
		pathItem, ok := rawPathItem.(map[string]interface{})
		if !ok {
			continue
		}
		for _, method := range httpMethods {
			rawOp, ok := pathItem[method]
			if !ok {
				continue
			}
			op, ok := rawOp.(map[string]interface{})
			if !ok {
				continue
			}
			operationID, _ := op["operationId"].(string)
			if operationID == "" {
				operationID = method + "_" + p
			}
			summary, _ := op["summary"].(string)
			description, _ := op["description"].(string)
			operations[operationID] = Operation{
				OperationID: operationID,
				Method:      strings.ToUpper(method),
				Path:        p,
				Summary:     summary,
				Description: description,
			}
		}
	}
	return operations, nil
}

// Capabilities derives agent.yaml capability entries from a spec file,
// one per operation, filtered to `allow` if non-empty — an explicit
// operationId allowlist. Deliberately doesn't default to publishing every
// operation a spec declares: account management, billing, and admin
// endpoints usually don't belong on gora8 even when an API's real
// task-shaped operations do.
func Capabilities(path string, allow []string) ([]Capability, error) {
	operations, err := Load(path)
	if err != nil {
		return nil, err
	}

	toCapability := func(op Operation) Capability {
		description := op.Summary
		if description == "" {
			description = op.Description
		}
		if description == "" {
			description = op.OperationID
		}
		return Capability{ID: op.OperationID, Description: description}
	}

	// Map iteration order is randomized in Go, so this function's output
	// must be ordered explicitly to be reproducible across runs (this
	// feeds a file `gora8 deploy` writes to disk). With an allowlist,
	// output follows the order the caller specified — the more legible
	// choice when someone hand-picked a handful of operations. Without
	// one, alphabetical by operationId is the simplest deterministic
	// order available.
	if len(allow) > 0 {
		caps := make([]Capability, 0, len(allow))
		var missing []string
		for _, id := range allow {
			id = strings.TrimSpace(id)
			op, ok := operations[id]
			if !ok {
				missing = append(missing, id)
				continue
			}
			caps = append(caps, toCapability(op))
		}
		if len(missing) > 0 {
			return caps, fmt.Errorf("operations not found in %s: %s", path, strings.Join(missing, ", "))
		}
		return caps, nil
	}

	ids := make([]string, 0, len(operations))
	for id := range operations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	caps := make([]Capability, 0, len(ids))
	for _, id := range ids {
		caps = append(caps, toCapability(operations[id]))
	}
	return caps, nil
}
