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

	loaded, err := openapi3.NewLoader().LoadFromData(data)
	if err != nil {
		return fmt.Errorf("emitter: generated document is not valid OpenAPI: %w", err)
	}
	if err := loaded.Validate(context.Background()); err != nil {
		return fmt.Errorf("emitter: generated document is not valid OpenAPI: %w", err)
	}
	return nil
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
