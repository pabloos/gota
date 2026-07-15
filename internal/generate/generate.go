// Package generate orchestrates the full gota pipeline: load packages,
// run router plugins to find routes, extract "gota:" comment blocks,
// infer defaults, merge the two, and hand the result to the emitter.
package generate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
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

	cmaps := map[*ast.File]ast.CommentMap{}

	var routeOps []emitter.RouteOperation
	for _, pkg := range pkgs {
		for _, plugin := range opts.Plugins {
			routes, err := plugin.Extract(pkg)
			if err != nil {
				return nil, fmt.Errorf("generate: plugin %s: %w", plugin.Name(), err)
			}
			for _, route := range routes {
				var cmap ast.CommentMap
				if route.File != nil {
					cmap = commentMapFor(pkg.Fset, route.File, cmaps)
				}
				op, err := buildOperation(route, pkg.TypesInfo, cmap)
				if err != nil {
					return nil, err
				}
				if op.Skip {
					continue
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

// buildOperation infers a baseline Operation for route (including a
// best-effort request/response body detected from the handler's own
// encoding/json calls, using cmap to honor any "x-gota-skip" comment
// attached to a specific statement), extracts any "gota:" comment on the
// handler itself, and merges the two (comment wins — including a
// handler-level "x-gota-skip: true", which excludes the whole operation).
func buildOperation(route router.Route, info *types.Info, cmap ast.CommentMap) (*model.Operation, error) {
	inferred := inference.Operation(route)
	inference.DetectBody(inferred, route.HandlerDecl, info, cmap)

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

// commentMapFor returns the ast.CommentMap for file, building it (via the
// stdlib go/ast, the same package used throughout gota's AST handling)
// and caching it in cache on first use — multiple handlers commonly live
// in the same file, and building a CommentMap walks the whole file.
func commentMapFor(fset *token.FileSet, file *ast.File, cache map[*ast.File]ast.CommentMap) ast.CommentMap {
	if cmap, ok := cache[file]; ok {
		return cmap
	}
	cmap := ast.NewCommentMap(fset, file, file.Comments)
	cache[file] = cmap
	return cmap
}
