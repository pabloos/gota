// Package emitter builds the final OpenAPI 3.1 document from merged
// operations and serializes it to YAML or JSON.
package emitter

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/pabloos/gota/pkg/model"
)

// RouteOperation binds one merged Operation to the method + path it serves.
type RouteOperation struct {
	Method    string
	Path      string
	Operation *model.Operation
}

// Format selects the output serialization.
type Format string

const (
	YAML Format = "yaml"
	JSON Format = "json"
)

// Build assembles a Document from routeOps. info.Title/Version populate the
// required Info object.
func Build(info model.Info, routeOps []RouteOperation) (*model.Document, error) {
	doc := &model.Document{
		OpenAPI: "3.1.0",
		Info:    info,
		Paths:   model.Paths{},
	}

	for _, ro := range routeOps {
		item, ok := doc.Paths[ro.Path]
		if !ok {
			item = &model.PathItem{}
			doc.Paths[ro.Path] = item
		}
		if existing := item.ForMethod(ro.Method); existing != nil {
			return nil, fmt.Errorf("emitter: duplicate route %s %s", ro.Method, ro.Path)
		}
		if !item.Set(ro.Method, ro.Operation) {
			return nil, fmt.Errorf("emitter: HTTP method %s has no OpenAPI operation slot for route %s", ro.Method, ro.Path)
		}
	}
	return doc, nil
}

// Marshal serializes doc in the requested format. Both formats produce
// deterministic output — map-backed fields (Paths, Schemas, Responses...)
// come out with keys sorted alphabetically rather than in Go's randomized
// map iteration order.
func Marshal(doc *model.Document, format Format) ([]byte, error) {
	switch format {
	case JSON:
		// encoding/json sorts map keys alphabetically by default.
		return json.MarshalIndent(doc, "", "  ")
	case YAML:
		// gopkg.in/yaml.v3 sorts map[string]T keys alphabetically and
		// preserves struct field declaration order, so Document's field
		// order (openapi, info, paths, components) survives while
		// map-backed fields (Paths, Schemas, Responses...) still come out
		// deterministic.
		return yaml.Marshal(doc)
	default:
		return nil, fmt.Errorf("emitter: unknown format %q", format)
	}
}
