package emitter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/pabloos/gota/pkg/model"
)

// Validate checks that doc is structurally valid OpenAPI, using kin-openapi
// as an independent authority rather than trusting our own struct
// definitions. It marshals doc to JSON internally — a format kin-openapi's
// loader handles unambiguously — so the check doesn't depend on which
// format the CLI eventually writes to disk.
//
// Known limitation: kin-openapi v0.135.0 models the Schema Object per
// OpenAPI 3.0 (e.g. "nullable" and "exclusiveMinimum"/"exclusiveMaximum"
// as booleans, no "webhooks"/"jsonSchemaDialect" support). It still catches
// real structural mistakes — missing required fields, malformed $ref,
// wrong parameter/response shapes — but it is not a strict OpenAPI 3.1
// conformance check. A newer kin-openapi (>= v0.140.0) fixes this properly
// but requires Go >= 1.25; revisit this once that's no longer a cost.
func Validate(doc *model.Document) error {
	data, err := json.Marshal(validatable(doc))
	if err != nil {
		return fmt.Errorf("emitter: marshaling document for validation: %w", err)
	}
	data, err = downgradeNullTypes(data)
	if err != nil {
		return fmt.Errorf("emitter: preparing document for validation: %w", err)
	}

	loaded, err := openapi3.NewLoader().LoadFromData(data)
	if err != nil {
		return fmt.Errorf("emitter: generated document is not valid OpenAPI: %w", err)
	}
	if err := loaded.Validate(context.Background()); err != nil {
		return fmt.Errorf("emitter: generated document is not valid OpenAPI: %w", err)
	}
	return nil
}

// downgradeNullTypes rewrites the OpenAPI 3.1 way of expressing null —
// `type: [T, "null"]` and an `anyOf` member `{type: "null"}` — into a form
// kin-openapi v0.135's 3.0 model accepts, for the validation pass only.
// kin loads a type array but rejects the "null" value at validation time,
// so drop it: a `[T, "null"]` array collapses to `T`, and a `{type:"null"}`
// anyOf member is removed. The real emitted document keeps the 3.1 form;
// only these bytes, fed to the validator, are downgraded. Structural
// validation (types, refs, required, examples) is unaffected.
func downgradeNullTypes(data []byte) ([]byte, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	stripNull(root)
	return json.Marshal(root)
}

func stripNull(v any) {
	switch node := v.(type) {
	case map[string]any:
		if arr, ok := node["type"].([]any); ok {
			kept := arr[:0:0]
			for _, e := range arr {
				if e != "null" {
					kept = append(kept, e)
				}
			}
			switch len(kept) {
			case 0:
				delete(node, "type")
			case 1:
				node["type"] = kept[0]
			default:
				node["type"] = kept
			}
		}
		if arr, ok := node["anyOf"].([]any); ok {
			kept := arr[:0:0]
			for _, m := range arr {
				if mm, ok := m.(map[string]any); ok && len(mm) == 1 && mm["type"] == "null" {
					continue
				}
				kept = append(kept, m)
			}
			if len(kept) == 0 {
				delete(node, "anyOf")
			} else {
				node["anyOf"] = kept
			}
		}
		for _, child := range node {
			stripNull(child)
		}
	case []any:
		for _, e := range node {
			stripNull(e)
		}
	}
}

// webhookValidationPrefix is the synthetic path prefix under which webhook
// operations are validated. It's an internal detail of validation only —
// never emitted — so it just needs to be a valid, collision-proof path.
const webhookValidationPrefix = "/x-gota-webhook/"

// validatable returns a copy of doc adjusted for kin-openapi v0.135, which
// models OpenAPI 3.0 and rejects the 3.1-only top-level "webhooks" and
// "jsonSchemaDialect" keys outright. Webhook values are Path Item Objects,
// so they're folded into paths under a synthetic prefix — kin then validates
// each webhook operation (its parameters, request body, responses, and
// $refs) exactly as it validates a path operation — and the 3.1-only keys
// kin can't model are cleared. The real emitted document keeps webhooks
// where they belong; only this validation copy is rearranged.
func validatable(doc *model.Document) *model.Document {
	if len(doc.Webhooks) == 0 && doc.JSONSchemaDialect == "" {
		return doc
	}
	check := *doc
	check.JSONSchemaDialect = ""
	if len(doc.Webhooks) > 0 {
		paths := make(model.Paths, len(doc.Paths)+len(doc.Webhooks))
		for k, v := range doc.Paths {
			paths[k] = v
		}
		for name, item := range doc.Webhooks {
			paths[webhookValidationPrefix+name] = item
		}
		check.Paths = paths
		check.Webhooks = nil
	}
	return &check
}
