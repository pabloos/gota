// Package generate orchestrates the full gota pipeline: load packages,
// run router plugins to find routes, extract "gota:" comment blocks,
// infer defaults, merge the two, and hand the result to the emitter.
package generate

import (
	"fmt"
	"sort"

	"github.com/pabloos/gota/internal/emitter"
	"github.com/pabloos/gota/internal/extractor"
	"github.com/pabloos/gota/internal/inference"
	"github.com/pabloos/gota/internal/merger"
	"github.com/pabloos/gota/internal/parser"
	"github.com/pabloos/gota/internal/router"
	"github.com/pabloos/gota/pkg/model"
)

// Options controls document generation.
type Options struct {
	Dir     string
	Title   string
	Version string
	Plugins []router.Plugin
}

// Run executes the full pipeline against opts.Dir and returns the merged
// OpenAPI document.
func Run(opts Options) (*model.Document, error) {
	pkgs, err := parser.Load(opts.Dir)
	if err != nil {
		return nil, err
	}

	var routeOps []emitter.RouteOperation
	for _, pkg := range pkgs {
		for _, plugin := range opts.Plugins {
			routes, err := plugin.Extract(pkg)
			if err != nil {
				return nil, fmt.Errorf("generate: plugin %s: %w", plugin.Name(), err)
			}
			for _, route := range routes {
				op, err := buildOperation(route)
				if err != nil {
					return nil, err
				}
				routeOps = append(routeOps, emitter.RouteOperation{
					Method:    route.Method,
					Path:      route.Path,
					Operation: op,
				})
			}
		}
	}

	sort.Slice(routeOps, func(i, j int) bool {
		if routeOps[i].Path != routeOps[j].Path {
			return routeOps[i].Path < routeOps[j].Path
		}
		return routeOps[i].Method < routeOps[j].Method
	})

	info := model.Info{Title: opts.Title, Version: opts.Version}
	doc, err := emitter.Build(info, routeOps)
	if err != nil {
		return nil, err
	}

	if err := inference.ResolveSchemaRefs(doc, pkgs); err != nil {
		return nil, err
	}

	return doc, nil
}

// buildOperation infers a baseline Operation for route, extracts any
// "gota:" comment on its handler, and merges the two (comment wins).
func buildOperation(route router.Route) (*model.Operation, error) {
	inferred := inference.Operation(route)

	if route.HandlerDecl == nil {
		return inferred, nil
	}
	declared, found, err := extractor.Extract(route.HandlerDecl.Doc)
	if err != nil {
		return nil, fmt.Errorf("generate: handler %s: %w", route.HandlerName, err)
	}
	if !found {
		return inferred, nil
	}
	return merger.Merge(inferred, declared), nil
}
