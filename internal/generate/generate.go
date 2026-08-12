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

	"golang.org/x/tools/go/packages"
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

	// roots is the set of analyzed root package paths. Body inference
	// package-qualifies a detected type declared OUTSIDE these (a
	// dependency or other go.work module), and ResolveSchemaRefs derives
	// the same set from the same pkgs, so a dependency type resolves by its
	// own package instead of colliding on a bare name in the reachable graph.
	roots := inference.RootPaths(pkgs)

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
				// A handler's doc-comment prose (everything but its "gota:"
				// block) becomes the operation description — the one piece of
				// per-operation documentation a Go developer already writes at
				// the source. A description declared in the comment still wins,
				// applied later in the merge.
				if route.HandlerDecl != nil {
					if prose := extractor.Prose(route.HandlerDecl.Doc); prose != "" {
						inferred.Description = prose
					}
				}
				inference.DetectBodyWithRoots(inferred, handlerDecl, info, cmap, globalIndex, rt.Dialect, ambiguous, roots)
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

	// Apply document-level "gota:doc:" declarations (securitySchemes, a
	// global security requirement, servers, tags, richer info) last, so
	// they overlay the generated paths/components without being touched by
	// route inference or ref resolution.
	docMeta, err := collectDocMeta(pkgs)
	if err != nil {
		return nil, err
	}
	applyDocMeta(doc, docMeta)

	return doc, nil
}

// collectDocMeta scans every doc comment in pkgs for "gota:doc:" blocks and
// merges them into a single document-level fragment (nil if none appear).
// A project normally declares one such block; when several appear, later
// ones — in package, then file, then comment order — override scalar Info
// fields and replace non-empty Servers/Security/Tags lists, while
// securitySchemes and manually declared schemas are unioned.
func collectDocMeta(pkgs []*packages.Package) (*model.DocumentMeta, error) {
	var merged *model.DocumentMeta
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			for _, cg := range file.Comments {
				meta, found, err := extractor.ExtractDoc(cg)
				if err != nil {
					return nil, fmt.Errorf("generate: %w", err)
				}
				if !found {
					continue
				}
				merged = mergeDocMeta(merged, meta)
			}
		}
	}
	return merged, nil
}

// mergeDocMeta folds next into base (either may be nil), returning the
// combined fragment. Scalar Info fields and non-empty Servers/Security/Tags
// lists from next win; securitySchemes and schemas are unioned with next
// taking precedence on a key clash.
func mergeDocMeta(base, next *model.DocumentMeta) *model.DocumentMeta {
	if base == nil {
		return next
	}
	if next == nil {
		return base
	}
	if next.Info != nil {
		if base.Info == nil {
			base.Info = &model.Info{}
		}
		if next.Info.Title != "" {
			base.Info.Title = next.Info.Title
		}
		if next.Info.Version != "" {
			base.Info.Version = next.Info.Version
		}
		if next.Info.Description != "" {
			base.Info.Description = next.Info.Description
		}
	}
	if len(next.Servers) > 0 {
		base.Servers = next.Servers
	}
	if len(next.Security) > 0 {
		base.Security = next.Security
	}
	if len(next.Tags) > 0 {
		base.Tags = next.Tags
	}
	if next.Components != nil {
		if base.Components == nil {
			base.Components = &model.Components{}
		}
		for k, v := range next.Components.SecuritySchemes {
			if base.Components.SecuritySchemes == nil {
				base.Components.SecuritySchemes = map[string]*model.SecurityScheme{}
			}
			base.Components.SecuritySchemes[k] = v
		}
		for k, v := range next.Components.Schemas {
			if base.Components.Schemas == nil {
				base.Components.Schemas = map[string]*model.Schema{}
			}
			base.Components.Schemas[k] = v
		}
	}
	return base
}

// applyDocMeta overlays a document-level fragment onto doc. Info fields
// declared in the fragment override the CLI-provided title/version and add
// a description; servers, a global security requirement and tags are set
// when present; securitySchemes and any manually declared schemas are
// merged into components without clobbering inferred schemas of the same
// name (an inferred schema wins, since it reflects real code).
func applyDocMeta(doc *model.Document, meta *model.DocumentMeta) {
	if meta == nil {
		return
	}
	if meta.Info != nil {
		if meta.Info.Title != "" {
			doc.Info.Title = meta.Info.Title
		}
		if meta.Info.Version != "" {
			doc.Info.Version = meta.Info.Version
		}
		if meta.Info.Description != "" {
			doc.Info.Description = meta.Info.Description
		}
	}
	if len(meta.Servers) > 0 {
		doc.Servers = meta.Servers
	}
	if len(meta.Security) > 0 {
		doc.Security = meta.Security
	}
	if len(meta.Tags) > 0 {
		doc.Tags = meta.Tags
	}
	if meta.Components == nil {
		return
	}
	if len(meta.Components.SecuritySchemes) > 0 {
		if doc.Components == nil {
			doc.Components = &model.Components{}
		}
		if doc.Components.SecuritySchemes == nil {
			doc.Components.SecuritySchemes = map[string]*model.SecurityScheme{}
		}
		for k, v := range meta.Components.SecuritySchemes {
			doc.Components.SecuritySchemes[k] = v
		}
	}
	if len(meta.Components.Schemas) > 0 {
		if doc.Components == nil {
			doc.Components = &model.Components{}
		}
		if doc.Components.Schemas == nil {
			doc.Components.Schemas = map[string]*model.Schema{}
		}
		for k, v := range meta.Components.Schemas {
			if _, exists := doc.Components.Schemas[k]; !exists {
				doc.Components.Schemas[k] = v
			}
		}
	}
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
