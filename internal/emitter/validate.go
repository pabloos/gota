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
	data, err := json.Marshal(doc)
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
