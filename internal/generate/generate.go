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
	"strings"
	"unicode"

	"github.com/pabloos/gota/internal/astutil"
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
	Routers []Router
}

// Router pairs a router plugin (which framework's route registrations
// to extract) with the inference dialect recognizing that framework's
// request/response idioms inside handler bodies. The two are separate
// axes on purpose: a future Chi plugin pairs with inference.NetHTTP()
// unchanged (Chi handlers are plain net/http), while a Gin plugin
// brings its own dialect. A nil Dialect disables body inference for
// that plugin's routes — they're still documented from the route and
// any "gota:" comment.
type Router struct {
	Plugin  router.Plugin
	Dialect inference.Dialect
}

// Run executes the full pipeline against opts.Dir and returns the merged
// OpenAPI document.
func Run(opts Options) (*model.Document, error) {
	pkgs, err := parser.Load(opts.Dir)
	if err != nil {
		return nil, err
	}

	cmaps := map[*ast.File]ast.CommentMap{}

	// globalIndex resolves a handler declared in a different package than
	// the one that registered it (e.g. mux.HandleFunc("/x", handlers.GetUser))
	// — a single plugin.Extract call only ever sees its own package, so
	// this covers every loaded package instead.
	globalIndex := astutil.IndexFuncDecls(pkgs)

	// ambiguous is the set of type names declared in more than one
	// analyzed package. Body inference (below) and ResolveSchemaRefs
	// (further down) must be handed the SAME set so an inferred $ref
	// package-qualifies exactly the names its later component key does —
	// see inference.AmbiguousSchemaNames.
	ambiguous := inference.AmbiguousSchemaNames(pkgs)

	// pending holds every route's inferred (not yet merged with any
	// "gota:" comment) Operation, so operationIDs can be disambiguated
	// across the whole document — see disambiguateOperationIDs — before
	// any comment gets a chance to declare its own explicit operationId.
	var pending []pendingOperation
	for _, pkg := range pkgs {
		for _, rt := range opts.Routers {
			routes, err := rt.Plugin.Extract(pkg)
			if err != nil {
				return nil, fmt.Errorf("generate: plugin %s: %w", rt.Plugin.Name(), err)
			}
			for _, route := range routes {
				info := pkg.TypesInfo
				if route.HandlerDecl == nil && route.HandlerObj != nil {
					if fd, ok := globalIndex[route.HandlerObj]; ok {
						route.HandlerDecl = fd.Decl
						route.File = fd.File
						info = fd.Info // the declaring package's Info, not the registering one
					}
				}
				// An inline handler (r.GET("/x", func(c *Ctx){...})) has no
				// name to base an operationId on, so synthesize one from the
				// method+path — always non-empty and unique per (method,path).
				// Its body is walked straight from the literal (DetectBody
				// only reads the body block), typed by the registering
				// package's own Info.
				handlerDecl := route.HandlerDecl
				if handlerDecl == nil && route.HandlerLit != nil {
					if route.HandlerName == "" {
						route.HandlerName = syntheticHandlerName(route.Method, route.Path)
					}
					handlerDecl = &ast.FuncDecl{Type: route.HandlerLit.Type, Body: route.HandlerLit.Body}
				}
				var cmap ast.CommentMap
				if route.File != nil {
					cmap = commentMapFor(pkg.Fset, route.File, cmaps)
				}
				inferred := inference.Operation(route)
				inference.DetectBody(inferred, handlerDecl, info, cmap, globalIndex, rt.Dialect, ambiguous)
				pending = append(pending, pendingOperation{route: route, info: info, cmap: cmap, op: inferred})
			}
		}
	}

	disambiguateOperationIDs(pending)

	var routeOps []emitter.RouteOperation
	for _, p := range pending {
		op, err := mergeDeclaredComment(p.route, p.op)
		if err != nil {
			return nil, err
		}
		if op.Skip {
			continue
		}
		routeOps = append(routeOps, emitter.RouteOperation{
			Method:    p.route.Method,
			Path:      p.route.Path,
			Operation: op,
		})
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

	if err := inference.ResolveSchemaRefs(doc, pkgs, ambiguous); err != nil {
		return nil, err
	}

	return doc, nil
}

// pendingOperation is one route's inferred (pre-merge) Operation,
// carrying what's still needed to later extract and merge its "gota:"
// comment.
type pendingOperation struct {
	route router.Route
	info  *types.Info
	cmap  ast.CommentMap
	op    *model.Operation
}

// disambiguateOperationIDs appends the HTTP method and path (PascalCase,
// e.g. "GetUsersId") to every inferred OperationID in a group of 2+
// pending operations that would otherwise collide. Two ways this
// happens in practice: a method-less ServeMux pattern expands into every
// HTTP method, all bound to the same handler (see nethttp.splitPattern),
// so every one of those operations infers the identical OperationID (the
// handler's own name); or the exact same handler is registered at more
// than one path (e.g. a shared WebSocket upgrader). Method alone
// resolves the first case but not the second (same handler, same method,
// different paths) — method+path resolves both in one pass, and is
// always sufficient, since (method, path) pairs are already guaranteed
// unique by the time this runs (emitter.Build itself rejects a literal
// duplicate route). Left alone, either case violates OpenAPI's global
// operationId-uniqueness rule and emitter.Validate rejects the whole
// document. Run before any "gota:" comment is merged in, so an
// explicitly declared operationId is never touched by this — only
// gota's own inferred default is; two distinct *declared* operationIds
// that happen to collide remain a real error, caught by emitter.Validate
// same as any other structural mistake.
func disambiguateOperationIDs(pending []pendingOperation) {
	groups := map[string][]int{}
	for i, p := range pending {
		groups[p.op.OperationID] = append(groups[p.op.OperationID], i)
	}
	for _, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}
		for _, i := range idxs {
			route := pending[i].route
			method := strings.ToUpper(route.Method[:1]) + strings.ToLower(route.Method[1:])
			pending[i].op.OperationID += method + pathSuffix(route.Path)
		}
	}
}

// pathSuffix converts path into a PascalCase identifier fragment
// ("/ws/inventory" -> "WsInventory") for disambiguateOperationIDs —
// every run of letters/digits becomes a capitalized word, everything
// else (slashes, path-param braces, hyphens, ...) is just a word
// boundary.
func pathSuffix(path string) string {
	var b strings.Builder
	upperNext := true
	for _, r := range path {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			upperNext = true
			continue
		}
		if upperNext {
			b.WriteRune(unicode.ToUpper(r))
			upperNext = false
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// syntheticHandlerName builds an operationId for an inline handler that
// has no name of its own, from its method and path ("GET", "/version" ->
// "GetVersion"). (method, path) pairs are unique per document, so the
// result is unique among anonymous handlers; a clash with a same-named
// declared handler is still resolved by disambiguateOperationIDs.
func syntheticHandlerName(method, path string) string {
	m := strings.ToUpper(method[:1]) + strings.ToLower(method[1:])
	return m + pathSuffix(path)
}

// mergeDeclaredComment extracts any "gota:" comment on route's handler
// and merges it into inferred (comment wins — including a handler-level
// "x-gota-skip: true", which excludes the whole operation).
func mergeDeclaredComment(route router.Route, inferred *model.Operation) (*model.Operation, error) {
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
