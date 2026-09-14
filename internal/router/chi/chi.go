// Package chi is a router plugin for github.com/go-chi/chi/v5. It
// statically recognizes:
//
//	r.Get("/users/{id}", GetUser)                    // method-specific: Get/Post/Put/Patch/Delete/Head/Options
//	r.Handle("/health", http.HandlerFunc(HealthCheck)) // all methods, unless the pattern itself has a "METHOD " prefix
//	r.Method("GET", "/users/{id}", handler)            // explicit method as a compile-time-constant string
//	r.Route("/users", func(r chi.Router) {             // nested prefix routing, arbitrarily deep
//		r.Get("/{id}", GetUser)                        // -> "/users/{id}"
//	})
//	r.Group(func(r chi.Router) { r.Get("/admin", Admin) }) // no path prefix, middleware scoping only
//	r.Mount("/products", productsRouter())             // see mountedRoutes: same-package, zero-arg constructor
//	v1 := chi.NewRouter()                              // a local router variable, configured via a same-
//	registerUserRoutes(v1)                             // package helper function taking it as a parameter,
//	r.Mount("/v1", v1)                                 // then mounted at a real prefix -- see extractRouterVarBlocks
//
// It does not import the real chi: recognition is pure go/types
// path/name matching against the analyzed target's own type-checked
// packages, the same mechanism internal/router/nethttp uses for
// *http.ServeMux — gota's own build never depends on chi. Both chi's
// current module path ("github.com/go-chi/chi/v5") and its legacy,
// unversioned one ("github.com/go-chi/chi", chi v1–v4 — still common in
// the wild) are recognized; see chiPkgPaths.
//
// Chi handlers are plain func(w http.ResponseWriter, r *http.Request)
// using ordinary encoding/json — chi introduces no response-writing
// idiom of its own — so this plugin pairs with the existing
// inference.NetHTTP() dialect unchanged.
//
// Path parameters use the same "{name}" syntax as OpenAPI path
// templates; a chi-specific regex constraint ("{id:[0-9]+}") degrades
// to the bare "{id}" (no direct OpenAPI equivalent). A pattern
// containing a bare wildcard segment ("/admin/*") has no OpenAPI
// path-template equivalent at all and is declined entirely, not
// approximated.
//
// A function taking a chi.Router/*chi.Mux parameter and registering
// routes on it — the canonical domain-driven Chi shape, e.g.
// "func RegisterHTTPEndPoints(router *chi.Mux) { router.Route("/api/v1/x",
// ...) }" called (often cross-package) as x.RegisterHTTPEndPoints(s.router)
// — is walked at the empty top-level prefix, the parameter itself acting
// as a chi receiver. This is correct for the overwhelmingly common real
// case: absolute paths on a top-level router (confirmed running this
// plugin against gmhafiz/go8, a large real Chi API, where every domain's
// register function has exactly this shape). Two same-package cases where
// an empty prefix would be wrong are filtered out (see routerParamSets):
// a function already emitted at its real prefix by extractRouterVarBlocks
// (a local router var configured then mounted, "v1 := chi.NewRouter();
// registerUserRoutes(v1); r.Mount("/v1", v1)") is not walked again here;
// and a function that receives a router var mounted at a prefix through a
// multi-argument call the prefix can't be wired through is declined
// entirely rather than emitted at a silently-wrong empty prefix.
//
// Known, deliberate v1 gaps (declined, not guessed) — found running
// this plugin against real, public Chi projects on GitHub, not
// speculated:
//   - A router-parameter register function that uses RELATIVE paths and
//     is mounted at a prefix in a DIFFERENT package — the mount site is
//     invisible to a single-package Plugin.Extract call, so its routes
//     surface at the empty prefix (unprefixed) rather than the real one.
//     The same-package version of this is detected and declined (above);
//     this cross-package sliver is the exact boundary a future
//     all-packages pass would move.
//   - Mount beyond a same-package, zero-argument constructor function or
//     a same-block tracked variable — a cross-package constructor, or a
//     variable that escapes the block it was declared in, falls outside
//     what a single Plugin.Extract call over one package/block can
//     resolve.
//   - A chi.Router reached through struct embedding rather than chi's
//     own named types directly.
//   - A Route/Group callback not written as an inline closure argument
//     (assigned to a variable first, then passed) — extracting it at the
//     wrong prefix would be silently wrong, not just incomplete, so it's
//     declined instead.
package chi

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/internal/astutil"
	"github.com/pabloos/gota/internal/router"
)

const pluginName = "chi"

// chiPkgPaths are the two import paths a real project's chi.Router
// might resolve to: the current, recommended "github.com/go-chi/chi/v5"
// and the legacy, unversioned "github.com/go-chi/chi" (chi v1–v4, still
// common in the wild — many tutorials and older templates predate the
// v5 rename, and the module system treats the two paths as entirely
// unrelated modules despite the identical API). Confirmed identical
// routing API surface (Get/Post/.../Route/Group/Mount/With, same
// signatures) directly against chi v1.5.5's own source before treating
// this as safe to unify rather than needing two separate code paths.
var chiPkgPaths = map[string]bool{
	"github.com/go-chi/chi/v5": true,
	"github.com/go-chi/chi":    true,
}

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return pluginName }

// methodCalls maps chi's method-specific registration methods to the
// HTTP method they register. Connect and Query are recognized (so a
// call to either isn't silently mistaken for something else) but never
// emitted via noEmitMethods: OpenAPI's Path Item Object has a slot for
// neither, the same reasoning allMethods excludes Connect for below.
var methodCalls = map[string]string{
	"Get":     http.MethodGet,
	"Post":    http.MethodPost,
	"Put":     http.MethodPut,
	"Patch":   http.MethodPatch,
	"Delete":  http.MethodDelete,
	"Head":    http.MethodHead,
	"Options": http.MethodOptions,
	"Trace":   http.MethodTrace,
	"Connect": http.MethodConnect,
	"Query":   "QUERY",
}

var noEmitMethods = map[string]bool{
	http.MethodConnect: true,
	"QUERY":            true,
}

// allMethods is what Handle/HandleFunc's all-methods registration
// expands to when its pattern has no "METHOD " prefix — mirrors
// nethttp.go's own allMethods list and rationale exactly (CONNECT
// excluded, no OpenAPI Path Item slot for it; QUERY excluded for the
// same reason, chi's own addition after nethttp.go's list was written).
var allMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodHead, http.MethodOptions, http.MethodTrace,
}

var httpMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true,
	http.MethodDelete: true, http.MethodHead: true, http.MethodOptions: true,
	http.MethodTrace: true, http.MethodConnect: true, "QUERY": true,
}

func isHTTPMethod(s string) bool { return httpMethods[s] }

func (p *Plugin) Extract(pkg *packages.Package, all []*packages.Package) ([]router.Route, error) {
	if pkg.Fset == nil {
		return nil, fmt.Errorf("chi: package %s has no Fset", pkg.PkgPath)
	}

	funcDecls := astutil.IndexFuncDecls([]*packages.Package{pkg})

	// mounted collects every function object used anywhere in pkg as a
	// Mount target (see resolveMountTarget) BEFORE the main walk below,
	// so a mount-constructor function like "func productsRouter()
	// chi.Router" is walked exactly once — via mountedRoutes' own
	// recursion, with the real accumulated prefix — rather than ALSO
	// independently as its own top-level entry point below, which would
	// silently double-emit its routes: once correctly prefixed, once at
	// the empty top-level prefix. This was caught by manually running
	// the plugin end-to-end, not by a unit test — see the fixture and
	// test asserting no spurious "/" routes.
	mounted := mountedFuncs(pkg)

	// excluded/declined classify same-package functions the top-level
	// walk below would otherwise mis-handle -- see routerParamSets.
	// excluded funcs are already emitted at the correct
	// prefix by extractRouterVarBlocks (walking them again here would
	// double-emit); declined funcs receive a router variable mounted at
	// a prefix this single-package pass can't compute, so they're
	// emitted nowhere rather than at a silently-wrong empty prefix.
	excluded, declined := routerParamSets(pkg)

	var routes []router.Route
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			obj := pkg.TypesInfo.Defs[fd.Name]
			if obj != nil && (mounted[obj] || excluded[obj] || declined[obj]) {
				continue
			}
			// A function taking a chi.Router/*chi.Mux parameter (e.g.
			// "func RegisterHTTPEndPoints(router *chi.Mux) {...}", the
			// canonical domain-driven Chi shape) registers routes on its
			// parameter; walked here at the empty top-level prefix, with
			// the parameter itself acting as a chi receiver. This is
			// correct for the overwhelmingly common case: absolute paths
			// on a top-level router. The two cases where an empty prefix
			// would be wrong -- the router variable is mounted at a
			// prefix same-package -- are filtered out above via
			// excluded/declined. The one residual (mounted at a prefix
			// cross-package with relative paths) is documented in this
			// package's doc comment.
			routes = append(routes, extractFromBlock(pkg, fd.Body.List, "", funcDecls)...)
		}
	}
	return routes, nil
}

// routerParamSets scans pkg for calls that pass a local chi.Router
// variable to a same-package function, classifying that function so
// Extract's top-level walk handles it correctly rather than blindly
// walking it at the empty prefix:
//
//   - excluded: the function is called via extractRouterVarBlocks' own
//     recognized shape (a single-argument call whose sole argument is a
//     tracked local chi.Router var). extractRouterVarBlocks already
//     walks it at the variable's real prefix, so walking it again as an
//     independent top-level entry point would double-emit.
//   - declined: the function receives a local chi.Router var that is
//     Mounted at a constant prefix, via a call shape extractRouterVarBlocks
//     does NOT handle (more than one argument -- the common
//     "register(router, deps...)" shape). A non-empty prefix is known to
//     apply but this single-package pass can't wire it through the
//     multi-arg call, so the function is declined entirely rather than
//     emitted at a silently-wrong empty prefix.
//
// Object identity (a var's types.Object is unique to its declaration)
// keeps the whole-function scan precise even though it doesn't track
// block boundaries: two different vars named "v" are distinct objects.
func routerParamSets(pkg *packages.Package) (excluded, declined map[types.Object]bool) {
	excluded = map[types.Object]bool{}
	declined = map[types.Object]bool{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			local, mountedVars := routerVarsInFunc(pkg, fd.Body)
			if len(local) == 0 {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				fnIdent, ok := call.Fun.(*ast.Ident) // same-package free function
				if !ok {
					return true
				}
				fnObj := pkg.TypesInfo.Uses[fnIdent]
				if fnObj == nil {
					return true
				}
				if len(call.Args) == 1 && argVarObj(pkg, call.Args[0], local) != nil {
					excluded[fnObj] = true
					return true
				}
				for _, arg := range call.Args {
					if obj := argVarObj(pkg, arg, local); obj != nil && mountedVars[obj] {
						declined[fnObj] = true
						break
					}
				}
				return true
			})
		}
	}
	return excluded, declined
}

// routerVarsInFunc returns, for the whole function body, the local
// chi.Router variables assigned anywhere in it, and which of those are
// Mounted at a constant prefix anywhere in it. Only membership matters
// to routerParamSets, not the prefix value, so this reports booleans
// rather than the prefix strings extractRouterVarBlocks computes.
func routerVarsInFunc(pkg *packages.Package, body *ast.BlockStmt) (local, mountedVars map[types.Object]bool) {
	local = map[types.Object]bool{}
	mountedVars = map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		ident, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || !isChiRouterType(pkg.TypesInfo.TypeOf(assign.Rhs[0])) {
			return true
		}
		obj := pkg.TypesInfo.Defs[ident]
		if obj == nil {
			obj = pkg.TypesInfo.Uses[ident] // "=" reassigning an existing variable
		}
		if obj != nil {
			local[obj] = true
		}
		return true
	})
	if len(local) == 0 {
		return local, mountedVars
	}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Mount" || len(call.Args) != 2 || !isChiRouterReceiver(pkg.TypesInfo, sel.X) {
			return true
		}
		if obj := argVarObj(pkg, call.Args[1], local); obj != nil {
			if _, ok := stringLiteral(call.Args[0]); ok {
				mountedVars[obj] = true
			}
		}
		return true
	})
	return local, mountedVars
}

// argVarObj returns the types.Object of arg when arg is a bare
// identifier resolving to one of the tracked local vars, else nil.
func argVarObj(pkg *packages.Package, arg ast.Expr, local map[types.Object]bool) types.Object {
	ident, ok := arg.(*ast.Ident)
	if !ok {
		return nil
	}
	obj := pkg.TypesInfo.Uses[ident]
	if obj == nil || !local[obj] {
		return nil
	}
	return obj
}

// mountedFuncs scans every Mount call in pkg and returns the set of
// function objects called by their second argument — see Extract's own
// doc comment for why this pre-pass exists. Deliberately broader than
// resolveMountTarget's own (zero-argument, chi.Router-returning) match:
// a function is excluded from being walked as its own independent
// top-level entry point as soon as ANY Mount call names it at all, even
// one resolveMountTarget itself declines to recurse into (e.g. it takes
// arguments gota can't resolve statically) — a function called as
// Mount's argument is clearly not meant to be a standalone entry point
// on its own, whether or not this plugin can also follow into it.
// Caught the same way as the double-extraction bug this pre-pass was
// originally added for: manually running the plugin against a fixture
// where the mount target took an argument produced a spurious
// unprefixed route from walking it independently too.
func mountedFuncs(pkg *packages.Package) map[types.Object]bool {
	mounted := map[types.Object]bool{}
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Mount" || len(call.Args) != 2 || !isChiRouterReceiver(pkg.TypesInfo, sel.X) {
				return true
			}
			if obj := mountCallCallee(pkg, call.Args[1]); obj != nil {
				mounted[obj] = true
			}
			return true
		})
	}
	return mounted
}

// mountCallCallee resolves arg (Mount's second argument) to the
// go/types object of a bare-identifier function call it makes,
// regardless of argument count — used only by mountedFuncs, for the
// broader "exclude from independent top-level walking" purpose
// described there. Also resolves a method-value call (srv.Router()),
// not just a bare function identifier — a method returning chi.Router
// is exactly as unsuitable a standalone top-level entry point as a free
// function is once something calls it as a Mount target (caught the
// same way: srv.Router() used as a Mount argument was still being
// independently walked as its own top-level "FuncDecl", since a method
// declaration satisfies that same AST node type and Extract's loop
// doesn't otherwise distinguish them). resolveMountTarget is the
// narrower, separate check used to decide whether to actually recurse
// into it.
func mountCallCallee(pkg *packages.Package, arg ast.Expr) types.Object {
	call, ok := arg.(*ast.CallExpr)
	if !ok {
		return nil
	}
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return pkg.TypesInfo.Uses[fn]
	case *ast.SelectorExpr:
		if selection, ok := pkg.TypesInfo.Selections[fn]; ok {
			return selection.Obj()
		}
		return pkg.TypesInfo.Uses[fn.Sel]
	}
	return nil
}

// extractFromBlock walks stmts (a function or closure body) looking for
// chi router calls, attributing every registration found directly in
// stmts to prefix. Unlike nethttp.Extract's single flat ast.Inspect —
// safe there because every net/http registration is context-free — a
// Get/Post/etc. call's effective path here depends on which Route/Group
// closures lexically contain it, arbitrarily nested, so this recurses
// explicitly instead: whenever a Route/Group/Mount call is matched, the
// inspect callback returns false (pruning the generic walk before it
// can independently re-discover the same nested calls at the wrong,
// unprefixed level) and a fresh extractFromBlock call walks the
// closure's own body with the updated prefix.
func extractFromBlock(pkg *packages.Package, stmts []ast.Stmt, prefix string, funcDecls map[types.Object]astutil.FuncDeclInfo) []router.Route {
	// extractRouterVarBlocks is purely additive — see its own doc
	// comment for why it can't double-count against the generic walk
	// below.
	var routes []router.Route
	routes = append(routes, extractRouterVarBlocks(pkg, stmts, prefix, funcDecls)...)
	for _, stmt := range stmts {
		ast.Inspect(stmt, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncLit:
				// A func(r chi.Router)-shaped closure reached here, NOT
				// via the Route/Group/Mount handling below (which
				// intercepts and prunes its own FuncLit argument before
				// the generic walk ever reaches it as an independent
				// node), has no reliable prefix to attribute to whatever
				// it registers -- e.g. assigned to a variable and passed
				// by identifier. Declined entirely rather than extracted
				// at the wrong prefix.
				if isChiRouterCallback(pkg.TypesInfo, node) {
					return false
				}
				return true
			case *ast.CallExpr:
				sel, ok := node.Fun.(*ast.SelectorExpr)
				if !ok || !isChiRouterReceiver(pkg.TypesInfo, sel.X) {
					return true
				}
				switch sel.Sel.Name {
				case "Route":
					if pattern, lit, ok := patternAndFuncLit(node.Args); ok {
						routes = append(routes, extractFromBlock(pkg, lit.Body.List, joinPath(prefix, pattern), funcDecls)...)
					}
					return false
				case "Group":
					if len(node.Args) == 1 {
						if lit, ok := node.Args[0].(*ast.FuncLit); ok && lit.Body != nil {
							routes = append(routes, extractFromBlock(pkg, lit.Body.List, prefix, funcDecls)...)
						}
					}
					return false
				case "Mount":
					if len(node.Args) == 2 {
						if pattern, ok := stringLiteral(node.Args[0]); ok {
							if sub, ok := mountedRoutes(pkg, node.Args[1], joinPath(prefix, pattern), funcDecls); ok {
								routes = append(routes, sub...)
							}
						}
					}
					return false
				case "Handle", "HandleFunc":
					routes = append(routes, extractHandle(pkg, node, prefix, funcDecls)...)
					return false
				case "Method", "MethodFunc":
					routes = append(routes, extractMethod(pkg, node, prefix, funcDecls)...)
					return false
				default:
					if httpMethod, ok := methodCalls[sel.Sel.Name]; ok {
						if r, ok := extractMethodCall(pkg, node, httpMethod, prefix, funcDecls); ok {
							routes = append(routes, r)
						}
					}
					return false
				}
			}
			return true
		})
	}
	return routes
}

// extractRouterVarBlocks recognizes a specific, common composition
// pattern within ONE block (no nested blocks, no reassignment across
// branches): a local chi.Router variable built via chi.NewRouter() (or
// anything else whose static type is chi.Router-shaped — matched by
// type, not by constructor name), configured via same-package
// helper-function calls elsewhere in this same block, and finally
// mounted at a real prefix —
//
//	v1 := chi.NewRouter()
//	registerUserRoutes(v1)
//	r.Mount("/v1", v1)
//
// — a real, common pattern found running this plugin against a public
// Chi project on GitHub (see this package's own doc comment): a
// "register*Routes(r chi.Router)"-shaped helper configuring a router
// value that's passed around rather than built via an inline Mount
// call. A tracked variable never mounted in this block falls back to
// the block's own prefix, matching the ordinary "r :=
// chi.NewRouter(); ...; return r" case.
//
// This is purely additive to the generic walk in extractFromBlock,
// which already no-ops on both of this pattern's shapes without any
// help from here — a bare-identifier Mount argument fails
// resolveMountTarget's own *ast.CallExpr assertion, and a plain
// (non-selector) function call never matches the generic walk's
// isChiRouterReceiver check at all — so there is no double-extraction
// risk to guard against. Deliberately does NOT also handle a direct
// method call on the tracked variable (v1.Get(...) rather than a
// configuring function): the generic walk already extracts that shape
// today (v1's own static type makes isChiRouterReceiver match it
// directly), so handling it again here would double-count it.
func extractRouterVarBlocks(pkg *packages.Package, stmts []ast.Stmt, prefix string, funcDecls map[types.Object]astutil.FuncDeclInfo) []router.Route {
	localVars := map[types.Object]bool{}
	for _, stmt := range stmts {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			continue
		}
		ident, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || !isChiRouterType(pkg.TypesInfo.TypeOf(assign.Rhs[0])) {
			continue
		}
		obj := pkg.TypesInfo.Defs[ident]
		if obj == nil {
			obj = pkg.TypesInfo.Uses[ident] // "=" reassigning an existing variable
		}
		if obj != nil {
			localVars[obj] = true
		}
	}
	if len(localVars) == 0 {
		return nil
	}

	// mountPrefix[obj] is set when this variable is later Mounted at a
	// real, constant prefix in this same block.
	mountPrefix := map[types.Object]string{}
	for _, stmt := range stmts {
		ast.Inspect(stmt, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Mount" || len(call.Args) != 2 || !isChiRouterReceiver(pkg.TypesInfo, sel.X) {
				return true
			}
			ident, ok := call.Args[1].(*ast.Ident)
			if !ok {
				return true
			}
			obj := pkg.TypesInfo.Uses[ident]
			if obj == nil || !localVars[obj] {
				return true
			}
			if pattern, ok := stringLiteral(call.Args[0]); ok {
				mountPrefix[obj] = joinPath(prefix, pattern)
			}
			return true
		})
	}

	var routes []router.Route
	for _, stmt := range stmts {
		ast.Inspect(stmt, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			// registerFoo(v1) -- a plain function call taking a tracked
			// variable as its sole argument.
			fnIdent, ok := call.Fun.(*ast.Ident)
			if !ok || len(call.Args) != 1 {
				return true
			}
			argIdent, ok := call.Args[0].(*ast.Ident)
			if !ok {
				return true
			}
			argObj := pkg.TypesInfo.Uses[argIdent]
			if argObj == nil || !localVars[argObj] {
				return true
			}
			fnObj := pkg.TypesInfo.Uses[fnIdent]
			if fnObj == nil {
				return true
			}
			fd, ok := funcDecls[fnObj]
			if !ok || fd.Decl == nil || fd.Decl.Body == nil {
				return true
			}
			effectivePrefix, mounted := mountPrefix[argObj]
			if !mounted {
				effectivePrefix = prefix
			}
			routes = append(routes, extractFromBlock(pkg, fd.Decl.Body.List, effectivePrefix, funcDecls)...)
			return false
		})
	}
	return routes
}

// patternAndFuncLit reports whether args is Route's own (pattern
// string, func(r chi.Router) {...}) shape, returning the pattern and
// the literal directly -- Route is declined (ok=false) when its second
// argument isn't an inline closure, same reasoning as the FuncLit case
// in extractFromBlock above.
func patternAndFuncLit(args []ast.Expr) (pattern string, lit *ast.FuncLit, ok bool) {
	if len(args) != 2 {
		return "", nil, false
	}
	pattern, ok = stringLiteral(args[0])
	if !ok {
		return "", nil, false
	}
	lit, ok = args[1].(*ast.FuncLit)
	if !ok || lit.Body == nil {
		return "", nil, false
	}
	return pattern, lit, true
}

// mountedRoutes recognizes arg as a call to a zero-argument function
// declared in pkg -- the SAME package as the Mount call, since
// Plugin.Extract only ever sees one package at a time, so a
// constructor declared elsewhere can't be resolved here -- that
// returns a chi.Router/*chi.Mux, and recurses into its body with
// prefix already joined in. This is the common "func productsRouter()
// chi.Router { r := chi.NewRouter(); r.Get(...); return r }" /
// "mx.Mount(\"/products\", productsRouter())" idiom. Everything else
// (a router built across multiple statements, passed in as a
// parameter, constructed in a different package, returned from a
// method call) declines this one Mount call entirely -- the same
// all-or-nothing posture nethttp.dispatchRoutes uses for method-switch
// closures.
func mountedRoutes(pkg *packages.Package, arg ast.Expr, prefix string, funcDecls map[types.Object]astutil.FuncDeclInfo) ([]router.Route, bool) {
	obj := resolveMountTarget(pkg, arg)
	if obj == nil {
		return nil, false
	}
	fd, ok := funcDecls[obj]
	if !ok || fd.Decl == nil || fd.Decl.Body == nil {
		return nil, false
	}
	return extractFromBlock(pkg, fd.Decl.Body.List, prefix, funcDecls), true
}

// resolveMountTarget resolves arg (Mount's second argument) to the
// go/types object of a zero-argument, chi.Router/*chi.Mux-returning
// function it calls — nil for anything else (a router built across
// multiple statements, passed in as a parameter, constructed in a
// different package, returned from a method call). Shared by
// mountedFuncs (the Extract-time pre-pass) and mountedRoutes (the
// actual recursion).
func resolveMountTarget(pkg *packages.Package, arg ast.Expr) types.Object {
	call, ok := arg.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok {
		return nil
	}
	obj := pkg.TypesInfo.Uses[ident]
	if obj == nil {
		return nil
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Results() == nil || sig.Results().Len() != 1 || !isChiRouterType(sig.Results().At(0).Type()) {
		return nil
	}
	return obj
}

// extractHandle recognizes Handle/HandleFunc(pattern, handler). Chi's
// own Handle re-parses a "METHOD /path"-style pattern (splitting on
// the first space/tab) and delegates to Method — so "GET /users" here
// registers only GET, not all methods; only a pattern with no space
// expands to allMethods, verified against chi's actual source rather
// than assumed from the "all methods" doc comment alone.
func extractHandle(pkg *packages.Package, call *ast.CallExpr, prefix string, funcDecls map[types.Object]astutil.FuncDeclInfo) []router.Route {
	if len(call.Args) != 2 {
		return nil
	}
	pattern, ok := stringLiteral(call.Args[0])
	if !ok {
		return nil
	}
	if i := strings.IndexAny(pattern, " \t"); i >= 0 {
		method := strings.ToUpper(strings.TrimSpace(pattern[:i]))
		rest := strings.TrimSpace(pattern[i+1:])
		return methodRoute(pkg, method, rest, call.Args[1], prefix, funcDecls, call.Pos())
	}
	return allMethodsRoutes(pkg, pattern, call.Args[1], prefix, funcDecls, call.Pos())
}

// extractMethod recognizes Method/MethodFunc(method, pattern, handler)
// — method is only recognized when it's a compile-time constant
// string (go/constant-based, matching internal/inference/body.go's
// constIntArg/constStringArg posture of declining rather than guessing
// a dynamic value); real chi panics at registration time on an
// unsupported method string, so a dynamic method argument wired to
// something other than a constant is rare in practice.
func extractMethod(pkg *packages.Package, call *ast.CallExpr, prefix string, funcDecls map[types.Object]astutil.FuncDeclInfo) []router.Route {
	if len(call.Args) != 3 {
		return nil
	}
	method, ok := constStringArg(call.Args[0], pkg.TypesInfo)
	if !ok {
		return nil
	}
	method = strings.ToUpper(method)
	pattern, ok := stringLiteral(call.Args[1])
	if !ok {
		return nil
	}
	return methodRoute(pkg, method, pattern, call.Args[2], prefix, funcDecls, call.Pos())
}

// extractMethodCall handles the method-specific calls (Get/Post/...)
// via methodCalls' static name->verb map.
func extractMethodCall(pkg *packages.Package, call *ast.CallExpr, method string, prefix string, funcDecls map[types.Object]astutil.FuncDeclInfo) (router.Route, bool) {
	if len(call.Args) != 2 || noEmitMethods[method] {
		return router.Route{}, false
	}
	pattern, ok := stringLiteral(call.Args[0])
	if !ok {
		return router.Route{}, false
	}
	r := methodRoute(pkg, method, pattern, call.Args[1], prefix, funcDecls, call.Pos())
	if len(r) != 1 {
		return router.Route{}, false
	}
	return r[0], true
}

// methodRoute builds a single Route for method+pattern (already joined
// with prefix), declining (nil) for an unrecognized or non-emittable
// method, an unresolvable path, or an unresolvable handler.
func methodRoute(pkg *packages.Package, method, pattern string, handlerExpr ast.Expr, prefix string, funcDecls map[types.Object]astutil.FuncDeclInfo, pos token.Pos) []router.Route {
	if !isHTTPMethod(method) || noEmitMethods[method] {
		return nil
	}
	path, ok := normalizePath(joinPath(prefix, pattern))
	if !ok {
		return nil
	}
	handlerName, decl, declFile, handlerObj, lit, ok := resolveHandler(pkg, handlerExpr, funcDecls)
	if !ok {
		return nil
	}
	return []router.Route{{
		Method:      method,
		Path:        path,
		HandlerName: handlerName,
		HandlerDecl: decl,
		File:        declFile,
		HandlerObj:  handlerObj,
		HandlerLit:  lit,
		Pos:         pkg.Fset.Position(pos),
	}}
}

// allMethodsRoutes builds one Route per allMethods entry, all bound to
// the same handler -- Handle/HandleFunc's own all-methods semantics.
func allMethodsRoutes(pkg *packages.Package, pattern string, handlerExpr ast.Expr, prefix string, funcDecls map[types.Object]astutil.FuncDeclInfo, pos token.Pos) []router.Route {
	path, ok := normalizePath(joinPath(prefix, pattern))
	if !ok {
		return nil
	}
	handlerName, decl, declFile, handlerObj, lit, ok := resolveHandler(pkg, handlerExpr, funcDecls)
	if !ok {
		return nil
	}
	routes := make([]router.Route, 0, len(allMethods))
	for _, m := range allMethods {
		routes = append(routes, router.Route{
			Method:      m,
			Path:        path,
			HandlerName: handlerName,
			HandlerDecl: decl,
			File:        declFile,
			HandlerObj:  handlerObj,
			HandlerLit:  lit,
			Pos:         pkg.Fset.Position(pos),
		})
	}
	return routes
}

// joinPath concatenates an accumulated prefix and a pattern the way
// chi's own Mount does: prefix's own trailing slash (if any) is
// trimmed, then sub is appended as-is, keeping sub's own leading slash
// — so a sub-pattern of exactly "/" is preserved as a real trailing
// slash on the result ("/users" + "/" = "/users/"), not collapsed
// away. Verified against chi's actual mount/tree behavior, not
// guessed: a naive "no double slashes" join that strips a resulting
// trailing slash would silently produce a path chi itself would never
// match.
func joinPath(prefix, sub string) string {
	return strings.TrimSuffix(prefix, "/") + sub
}

// normalizePath converts a chi pattern into an OpenAPI path template. A
// regex-constrained param ("{id:[0-9]+}") degrades to a bare "{id}" —
// no direct OpenAPI equivalent, and dropping the constraint is the
// honest best-effort. A bare wildcard segment ("*", e.g. "/admin/*" —
// chi's actual, documented wildcard syntax) has no OpenAPI
// path-template equivalent at all, so a pattern containing one is
// declined (ok=false) entirely rather than approximated.
func normalizePath(pattern string) (string, bool) {
	if strings.Contains(pattern, "*") {
		return "", false
	}
	var b strings.Builder
	for i := 0; i < len(pattern); {
		if pattern[i] != '{' {
			b.WriteByte(pattern[i])
			i++
			continue
		}
		end := strings.IndexByte(pattern[i:], '}')
		if end < 0 {
			return "", false
		}
		end += i
		name := pattern[i+1 : end]
		if colon := strings.IndexByte(name, ':'); colon >= 0 {
			name = name[:colon]
		}
		b.WriteByte('{')
		b.WriteString(name)
		b.WriteByte('}')
		i = end + 1
	}
	return b.String(), true
}

// isChiRouterReceiver reports whether e's static type is chi.Router or
// *chi.Mux — the two shapes a chi routing-method receiver actually
// takes (verified against go/types: an interface-typed value like a
// func(r chi.Router) parameter is a *types.Named directly, no pointer
// to strip, while r := chi.NewRouter() is *types.Pointer wrapping
// *types.Named — checking both, not assuming one, is load-bearing:
// Route's and Mount's own callback parameter is ALWAYS the interface
// shape, so it's the common case here, not an edge case). A
// .With(mw).Get(...) chain is recognized for free, since With's own
// declared return type is chi.Router too.
func isChiRouterReceiver(info *types.Info, e ast.Expr) bool {
	if info == nil {
		return false
	}
	return isChiRouterType(info.TypeOf(e))
}

func isChiRouterType(t types.Type) bool {
	if t == nil {
		return false
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return chiPkgPaths[named.Obj().Pkg().Path()] &&
		(named.Obj().Name() == "Mux" || named.Obj().Name() == "Router")
}

// isChiRouterCallback reports whether lit has the func(r chi.Router)
// shape Route/Group/Mount's own callback parameter always declares.
func isChiRouterCallback(info *types.Info, lit *ast.FuncLit) bool {
	if info == nil {
		return false
	}
	params := lit.Type.Params.List
	if len(params) != 1 || len(params[0].Names) != 1 {
		return false
	}
	obj := info.Defs[params[0].Names[0]]
	if obj == nil {
		return false
	}
	return isChiRouterType(obj.Type())
}

// stringLiteral extracts the value of a string literal expression.
func stringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return v, true
}

// unwrapCall strips single-argument call layers off e — the shape of
// an http.HandlerFunc(...) conversion or a middleware(...) wrapper —
// down to whatever's inside. Identical to nethttp.go's own unwrapCall;
// duplicated rather than shared, see this package's own doc comment
// and internal/router/plugin.go's plugin-isolation contract.
func unwrapCall(e ast.Expr) ast.Expr {
	for {
		call, ok := e.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return e
		}
		e = call.Args[0]
	}
}

// resolveHandler extracts the handler name from a bare identifier
// (Get(pattern, GetUser)), a method value on a receiver
// (Get(pattern, srv.GetUser)), a qualified identifier from another
// package (Get(pattern, handlers.GetUser)), or an http.HandlerFunc
// conversion (or middleware wrapper) of any of those, plus the
// go/types object it resolves to. An inline handler closure
// (Get(pattern, func(w, r){...})) has no name or object and is returned
// via lit for internal/generate to infer and name. Identical in shape to
// nethttp.go's own resolveHandler — see that function's doc comment for
// the full rationale (decl/file via object identity, obj always populated
// for internal/generate's cross-package backfill even when decl isn't).
func resolveHandler(pkg *packages.Package, e ast.Expr, decls map[types.Object]astutil.FuncDeclInfo) (name string, decl *ast.FuncDecl, file *ast.File, obj types.Object, lit *ast.FuncLit, ok bool) {
	switch expr := unwrapCall(e).(type) {
	case *ast.Ident:
		identObj := pkg.TypesInfo.Uses[expr]
		rd := decls[identObj]
		return expr.Name, rd.Decl, rd.File, identObj, nil, true
	case *ast.SelectorExpr:
		var obj types.Object
		if selection, ok := pkg.TypesInfo.Selections[expr]; ok {
			obj = selection.Obj() // method value, e.g. srv.GetUser
		} else {
			obj = pkg.TypesInfo.Uses[expr.Sel] // qualified identifier, e.g. handlers.GetUser
		}
		rd := decls[obj]
		return expr.Sel.Name, rd.Decl, rd.File, obj, nil, true
	case *ast.FuncLit:
		return "", nil, nil, nil, expr, true
	}
	return "", nil, nil, nil, nil, false
}

// constStringArg evaluates e as a compile-time string constant.
// Identical to nethttp.go's own constStringArg; duplicated rather than
// shared, see this package's own doc comment.
func constStringArg(e ast.Expr, info *types.Info) (string, bool) {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}
