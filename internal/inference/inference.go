// Package inference derives a baseline OpenAPI Operation from what can be
// deduced statically from a route and its handler, with no help from the
// programmer: path parameters, a default response, and a few conventional
// defaults (operationId, summary). It never overrides anything the
// programmer wrote in a "gota:" comment — see internal/merger for that.
package inference

import (
	"regexp"
	"strings"

	"github.com/pabloos/gota/internal/router"
	"github.com/pabloos/gota/pkg/model"
)

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// Operation builds the inferred baseline Operation for route.
func Operation(route router.Route) *model.Operation {
	op := &model.Operation{
		OperationID: route.HandlerName,
		Summary:     humanize(route.HandlerName),
		Parameters:  pathParameters(route.Path),
		Responses: map[string]model.Response{
			"200": {Description: "OK"},
		},
	}
	return op
}

// pathParameters infers a path Parameter (typed as string, since Phase 1
// has no struct/type inference yet) for every "{name}" segment in path.
func pathParameters(path string) []model.Parameter {
	matches := pathParamRe.FindAllStringSubmatch(path, -1)
	if len(matches) == 0 {
		return nil
	}
	params := make([]model.Parameter, 0, len(matches))
	for _, m := range matches {
		name := strings.TrimSuffix(m[1], "...") // Go 1.22 wildcard suffix, e.g. {path...}
		params = append(params, model.Parameter{
			Name:     name,
			In:       "path",
			Required: true,
			Schema:   &model.Schema{Type: "string"},
		})
	}
	return params
}

// camelBoundaryRe matches the boundary between camelCase words, splitting
// both "aB" (lower-to-upper) and "ABc" (acronym-to-word, e.g. "IDFor") transitions.
var camelBoundaryRe = regexp.MustCompile(`([a-z0-9])([A-Z])|([A-Z])([A-Z][a-z])`)

// humanize turns a Go identifier like "GetUserByID" into "Get user by ID"
// as a readable default summary. It's a best-effort heuristic, not a
// substitute for a "gota:" comment.
func humanize(name string) string {
	if name == "" {
		return ""
	}
	spaced := camelBoundaryRe.ReplaceAllString(name, "$1$3 $2$4")
	words := strings.Fields(spaced)
	for i, w := range words {
		if i == 0 {
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		} else if !isAllUpper(w) {
			words[i] = strings.ToLower(w)
		}
	}
	return strings.Join(words, " ")
}

func isAllUpper(s string) bool { return s == strings.ToUpper(s) }
