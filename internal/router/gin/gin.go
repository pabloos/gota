// Package gin is a router plugin for github.com/gin-gonic/gin. It
// statically recognizes route registrations on a *gin.Engine or
// *gin.RouterGroup:
//
//	r.GET("/users/:id", GetUser)                 // method-specific: GET/POST/PUT/DELETE/PATCH/HEAD/OPTIONS
//	r.Any("/health", HealthCheck)                // every HTTP method
//	r.Handle("GET", "/users/:id", GetUser)       // explicit method as a compile-time-constant string
//	r.Match([]string{"GET", "POST"}, "/x", H)    // several explicit methods
//	v1 := r.Group("/api/v1")                      // a group variable carries a path prefix,
//	v1.GET("/users", ListUsers)                  //   accumulated across nested groups -> "/api/v1/users"
//
// It does not import the real gin: recognition is pure go/types
// path/name matching against the analyzed target's own type-checked
// packages, the same mechanism internal/router/nethttp uses for
// *http.ServeMux — gota's own build never depends on gin.
//
// Handlers are the LAST argument of a route call (gin's route methods
// are variadic, "handlers ...HandlerFunc"; the earlier arguments are
// middleware). Gin handlers are func(c *gin.Context), a shape unlike
// net/http's, so this plugin pairs with the gin inference dialect
// (inference.Gin()), not inference.NetHTTP().
//
// Path parameters use gin's ":name" syntax, translated to OpenAPI's
// "{name}". A catch-all "*name" segment has no OpenAPI path-template
// equivalent, so a route containing one is declined.
//
// The prefix of a route travels through the group VARIABLE it's
// registered on (object identity), not lexical nesting: "v1 :=
// r.Group("/api/v1")" then "v1.GET(...)". groupPrefixes resolves that
// chain.
//
// Routes registered inside a "register function" that takes a
// *gin.RouterGroup parameter — the dominant real-world layout, e.g.
// "func (h *Users) Routes(rg *gin.RouterGroup) { rg.GET("/tags", ...) }" —
// are resolved too: collectParamPrefixes finds the call sites of such
// functions across ALL analyzed packages — a call site may live in a
// different package than the register function (the common
// handlers-package-registered-from-main split) — and gives the parameter
// the prefix of the group passed there. A call resolving to one concrete
// function/method binds its arguments to that function's parameters
// precisely (a direct "registerTags(v1)", a qualified
// "handlers.Register(v1)", or a concrete method value); an interface
// dispatch (a "for _, h := range hs { h.Routes(g) }" registry loop) matches
// by (name, argument index), so every concrete Routes(*gin.RouterGroup)
// resolves under g's prefix. Nested groups created inside the register
// function chain onto that prefix.
//
// Interface dispatch is matched by (name, argument index) rather than by
// resolved object — necessarily, since the concrete implementers aren't
// known at the call site — so across packages it's a heuristic, not the
// precise binding a direct/concrete call gets.
//
// Known, deliberate residual gaps (declined, not guessed): a group variable
// assigned more than once (ambiguous prefix), a non-constant Group path, a
// group whose receiver chain can't be resolved to a constant prefix, and a
// "*name" catch-all path.
package gin

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"net/http"
	"path"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/internal/astutil"
	"github.com/pabloos/gota/internal/router"
)

const (
	pluginName = "gin"
	ginPkgPath = "github.com/gin-gonic/gin"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return pluginName }

// routeMethods maps gin's single-method registration calls to their HTTP
// verb. Any/Handle/Match are handled separately (they don't map 1:1).
var routeMethods = map[string]string{
	"GET":     http.MethodGet,
	"POST":    http.MethodPost,
	"PUT":     http.MethodPut,
	"DELETE":  http.MethodDelete,
	"PATCH":   http.MethodPatch,
	"HEAD":    http.MethodHead,
	"OPTIONS": http.MethodOptions,
}

// allMethods is what gin's Any expands to, restricted to the HTTP
// methods OpenAPI's Path Item Object has a slot for — gin's Any also
// registers CONNECT, which is excluded (same reasoning nethttp/chi use).
var allMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodHead, http.MethodOptions, http.MethodTrace,
}

// httpMethods is the set of methods gota can emit (an OpenAPI slot
// exists) — CONNECT is deliberately absent, so a Handle("CONNECT", ...)
// or a Match element of "CONNECT" is declined rather than emitted.
var httpMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true,
	http.MethodDelete: true, http.MethodHead: true, http.MethodOptions: true, http.MethodTrace: true,
}

// isRouteMethodName reports whether a selector name is a route-emitting
// gin method. Group is deliberately excluded — it registers no handler
// and is consumed only by the prefix machinery (collectGroupDefs).
func isRouteMethodName(name string) bool {
	if _, ok := routeMethods[name]; ok {
		return true
	}
	return name == "Any" || name == "Handle" || name == "Match"
}

func (p *Plugin) Extract(pkg *packages.Package, all []*packages.Package) ([]router.Route, error) {
	if pkg.Fset == nil {
		return nil, fmt.Errorf("gin: package %s has no Fset", pkg.PkgPath)
	}

	funcDecls := astutil.IndexFuncDecls([]*packages.Package{pkg})
	groupDefs := collectGroupDefs(pkg)
	paramPrefixes := collectParamPrefixes(pkg, all)

	var routes []router.Route
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !isRouteMethodName(sel.Sel.Name) || !isGinRouterMethodCall(pkg, sel) {
				return true
			}
			routes = append(routes, extractRoute(pkg, call, sel, groupDefs, paramPrefixes, funcDecls)...)
			return true
		})
	}
	return routes, nil
}

// groupDef records how a *gin.RouterGroup variable was created: the
// receiver it was grouped off and the constant relative path.
type groupDef struct {
	recv ast.Expr
	path string
}

// collectGroupDefs maps each *gin.RouterGroup variable to its defining
// "v := X.Group(constPath)" — the input to groupPrefix's chain
// resolution. A variable assigned more than once is ambiguous (which
// prefix applies where?) and is omitted entirely, so groupPrefix
// declines any route on it rather than guessing.
func collectGroupDefs(pkg *packages.Package) map[types.Object]groupDef {
	defs := map[types.Object]groupDef{}
	assigns := map[types.Object]int{}
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				return true
			}
			ident, ok := assign.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			obj := pkg.TypesInfo.Defs[ident]
			if obj == nil {
				obj = pkg.TypesInfo.Uses[ident] // "=" reassignment
			}
			if obj == nil || !isGinNamedType(obj.Type(), "RouterGroup") {
				return true
			}
			assigns[obj]++
			call, ok := assign.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Group" || !isGinRouterMethodCall(pkg, sel) || len(call.Args) < 1 {
				return true
			}
			path, ok := stringLiteral(call.Args[0])
			if !ok {
				return true
			}
			defs[obj] = groupDef{recv: sel.X, path: path}
			return true
		})
	}
	for obj, n := range assigns {
		if n > 1 {
			delete(defs, obj) // reassigned: ambiguous, decline
		}
	}
	return defs
}

// groupPrefix resolves the accumulated path prefix of the router
// expression recvExpr a route was registered on, or ok=false to decline
// the route (an unresolvable receiver — including a *gin.RouterGroup
// parameter, see the package doc comment). It recurses through group
// chains, guarding against a self-referential reassignment cycle.
func groupPrefixes(pkg *packages.Package, recvExpr ast.Expr, groupDefs map[types.Object]groupDef, paramPrefixes map[types.Object][]string, visiting map[types.Object]bool) ([]string, bool) {
	// A *gin.Engine (a var, a parameter, or an inline gin.New()/
	// gin.Default() call) is the root: empty prefix. Matched by type, so
	// it doesn't matter how the engine was obtained.
	if isGinNamedType(pkg.TypesInfo.TypeOf(recvExpr), "Engine") {
		return []string{""}, true
	}
	switch e := recvExpr.(type) {
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok || !isGinRouterMethodCall(pkg, sel) {
			return nil, false
		}
		switch sel.Sel.Name {
		case "Use":
			// ".Use(middleware)" returns the same route group as
			// gin.IRoutes; it's transparent for the prefix, so recurse
			// into its receiver: g.Group("/x").Use(mw).POST(...) keeps the
			// "/x" prefix.
			return groupPrefixes(pkg, sel.X, groupDefs, paramPrefixes, visiting)
		case "Group":
			// An inline chained group: X.Group("/v1").GET(...).
			if len(e.Args) < 1 {
				return nil, false
			}
			path, ok := stringLiteral(e.Args[0])
			if !ok {
				return nil, false
			}
			parents, ok := groupPrefixes(pkg, sel.X, groupDefs, paramPrefixes, visiting)
			if !ok {
				return nil, false
			}
			return joinAll(parents, path), true
		}
		return nil, false
	case *ast.Ident:
		obj := pkg.TypesInfo.Uses[e]
		if obj == nil || visiting[obj] {
			return nil, false
		}
		if gd, ok := groupDefs[obj]; ok {
			visiting[obj] = true
			parents, ok := groupPrefixes(pkg, gd.recv, groupDefs, paramPrefixes, visiting)
			if !ok {
				return nil, false
			}
			return joinAll(parents, gd.path), true
		}
		// A *gin.RouterGroup parameter: its prefixes come from the call
		// sites of its enclosing register function (see collectParamPrefixes).
		if ps, ok := paramPrefixes[obj]; ok && len(ps) > 0 {
			return ps, true
		}
		return nil, false // untracked/ambiguous group var, or an unresolved parameter
	}
	return nil, false
}

// registerParam identifies a group-parameter register function by the call
// name that invokes it and the positional index of its *gin.RouterGroup
// argument — the pair a concrete method shares with the interface method it
// implements, so a dynamic dispatch (h.Routes(rg)) matches every concrete
// Routes(*gin.RouterGroup) the same way a direct call (registerUsers(rg))
// matches its one function.
type registerParam struct {
	name  string
	index int
}

// collectParamPrefixes maps each *gin.RouterGroup parameter that a route is
// registered on to the set of path prefixes the calls in this package
// invoke its function/method with — resolving the "register function"
// idiom (func (h *Users) Routes(rg *gin.RouterGroup) { rg.GET(...) },
// called as h.Routes(theGroup)), the dominant real-world layout. Matching
// is by (call name, argument index), which reaches both a direct call and
// an interface dispatch. Scope is a single package: a register function
// whose call site is in another package stays declined (the documented
// cross-package gap). Resolution iterates to a fixpoint so a register
// function that passes its own group parameter to another one resolves too.
func collectParamPrefixes(pkg *packages.Package, all []*packages.Package) map[types.Object][]string {
	// The owner's group-parameter register functions: the set of their group
	// params (for object binding), and a (name, arg index) index of them (for
	// the interface-dispatch fallback).
	ownerParams := map[types.Object]bool{}
	byNameIdx := map[registerParam][]types.Object{}
	for _, file := range pkg.Syntax {
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Type.Params == nil {
				continue
			}
			idx := 0
			for _, field := range fn.Type.Params.List {
				isGroup := isGinNamedType(pkg.TypesInfo.TypeOf(field.Type), "RouterGroup")
				names := field.Names
				if len(names) == 0 { // an unnamed parameter still occupies a position
					idx++
					continue
				}
				for _, nm := range names {
					if isGroup {
						if obj := pkg.TypesInfo.Defs[nm]; obj != nil {
							ownerParams[obj] = true
							key := registerParam{fn.Name.Name, idx}
							byNameIdx[key] = append(byNameIdx[key], obj)
						}
					}
					idx++
				}
			}
		}
	}
	if len(ownerParams) == 0 {
		return nil
	}

	// Precompute each scanned package's group definitions so a call-site
	// argument is resolved in its own package's context — the call site may
	// live in a different package than the register function it targets.
	gdByPkg := make(map[*packages.Package]map[types.Object]groupDef, len(all))
	for _, p := range all {
		gdByPkg[p] = collectGroupDefs(p)
	}

	prefixes := map[types.Object][]string{}
	add := func(p *packages.Package, gd map[types.Object]groupDef, param types.Object, arg ast.Expr, changed *bool) {
		if !ownerParams[param] {
			return
		}
		argType := p.TypesInfo.TypeOf(arg)
		if !isGinNamedType(argType, "RouterGroup") && !isGinNamedType(argType, "Engine") {
			return
		}
		if aps, ok := groupPrefixes(p, arg, gd, prefixes, map[types.Object]bool{}); ok && addPrefixes(prefixes, param, aps) {
			*changed = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, p := range all {
			gd := gdByPkg[p]
			for _, file := range p.Syntax {
				ast.Inspect(file, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					// A call resolving to one concrete function/method binds
					// its arguments to that function's parameters precisely,
					// across packages — the common cross-package case
					// (registerUsers(v1), handlers.Register(v1), h.Routes(v1)).
					if fn, ok := bindCallee(p, call); ok {
						if sig, ok := fn.Type().(*types.Signature); ok && sig.Params() != nil {
							for i := 0; i < sig.Params().Len() && i < len(call.Args); i++ {
								add(p, gd, sig.Params().At(i), call.Args[i], &changed)
							}
						}
						return true
					}
					// Interface dispatch (a registry loop calling an interface
					// method): no single concrete callee, so match every
					// owner register function by (name, arg index).
					name, ok := calleeName(call.Fun)
					if !ok {
						return true
					}
					for i, arg := range call.Args {
						for _, obj := range byNameIdx[registerParam{name, i}] {
							add(p, gd, obj, arg, &changed)
						}
					}
					return true
				})
			}
		}
	}
	return prefixes
}

// bindCallee returns the single concrete function or method a call invokes,
// or ok=false when the call goes through an interface (no one concrete
// callee — the caller then falls back to name+index matching). It resolves a
// bare function, a package-qualified function, and a concrete method value,
// across packages.
func bindCallee(callPkg *packages.Package, call *ast.CallExpr) (*types.Func, bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if fn, ok := callPkg.TypesInfo.Uses[fun].(*types.Func); ok {
			return fn, true
		}
	case *ast.SelectorExpr:
		if seln, ok := callPkg.TypesInfo.Selections[fun]; ok {
			fn, ok := seln.Obj().(*types.Func)
			if !ok {
				return nil, false
			}
			sig, ok := fn.Type().(*types.Signature)
			if ok && sig.Recv() != nil {
				if _, iface := sig.Recv().Type().Underlying().(*types.Interface); iface {
					return nil, false // interface method: dispatch, not one callee
				}
			}
			return fn, true
		}
		if fn, ok := callPkg.TypesInfo.Uses[fun.Sel].(*types.Func); ok {
			return fn, true // qualified identifier: pkg.Func
		}
	}
	return nil, false
}

// calleeName returns the bare name a call expression invokes — the
// identifier for a plain call, or the selected method name for a method
// call — used to match a call site to a register function by name.
func calleeName(fun ast.Expr) (string, bool) {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name, true
	case *ast.SelectorExpr:
		return f.Sel.Name, true
	}
	return "", false
}

// addPrefixes unions add into prefixes[obj], reporting whether it grew.
func addPrefixes(prefixes map[types.Object][]string, obj types.Object, add []string) bool {
	existing := prefixes[obj]
	grew := false
	for _, p := range add {
		found := false
		for _, e := range existing {
			if e == p {
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, p)
			grew = true
		}
	}
	if grew {
		prefixes[obj] = existing
	}
	return grew
}

// joinAll joins sub onto every parent prefix, de-duplicating the result.
func joinAll(parents []string, sub string) []string {
	out := make([]string, 0, len(parents))
	seen := map[string]bool{}
	for _, p := range parents {
		j := joinPath(p, sub)
		if !seen[j] {
			seen[j] = true
			out = append(out, j)
		}
	}
	return out
}

// extractRoute turns one recognized route call into Routes, resolving
// its prefix, path, method(s) and handler. Declines (nil) on any
// unresolvable piece.
func extractRoute(pkg *packages.Package, call *ast.CallExpr, sel *ast.SelectorExpr, groupDefs map[types.Object]groupDef, paramPrefixes map[types.Object][]string, funcDecls map[types.Object]astutil.FuncDeclInfo) []router.Route {
	prefixes, ok := groupPrefixes(pkg, sel.X, groupDefs, paramPrefixes, map[types.Object]bool{})
	if !ok {
		return nil
	}
	switch name := sel.Sel.Name; name {
	case "Any":
		return routeAt(pkg, allMethods, call, 0, prefixes, funcDecls)
	case "Handle":
		if len(call.Args) < 3 {
			return nil
		}
		method, ok := constStringArg(call.Args[0], pkg.TypesInfo)
		if !ok {
			return nil
		}
		return routeAt(pkg, []string{strings.ToUpper(method)}, call, 1, prefixes, funcDecls)
	case "Match":
		if len(call.Args) < 3 {
			return nil
		}
		methods, ok := constStringSlice(call.Args[0], pkg.TypesInfo)
		if !ok {
			return nil
		}
		return routeAt(pkg, methods, call, 1, prefixes, funcDecls)
	default:
		return routeAt(pkg, []string{routeMethods[name]}, call, 0, prefixes, funcDecls)
	}
}

// routeAt builds one Route per emittable method: pathIdx is the argument
// holding the relative path, the handler is always the last argument
// (gin's variadic middleware precede it), and a non-emittable method
// (e.g. CONNECT) is skipped. Declines the whole call (nil) if there's no
// handler argument, the path isn't a literal, or the path has no OpenAPI
// equivalent.
func routeAt(pkg *packages.Package, methods []string, call *ast.CallExpr, pathIdx int, prefixes []string, funcDecls map[types.Object]astutil.FuncDeclInfo) []router.Route {
	if len(call.Args) < pathIdx+2 { // path + at least one handler
		return nil
	}
	rawPath, ok := stringLiteral(call.Args[pathIdx])
	if !ok {
		return nil
	}
	handlerName, decl, file, obj, lit, ok := resolveHandler(pkg, call.Args[len(call.Args)-1], funcDecls)
	if !ok {
		return nil
	}
	var routes []router.Route
	for _, prefix := range prefixes {
		path, ok := normalizePath(joinPath(prefix, rawPath))
		if !ok {
			continue
		}
		for _, m := range methods {
			if !httpMethods[m] {
				continue
			}
			routes = append(routes, router.Route{
				Method:      m,
				Path:        path,
				HandlerName: handlerName,
				HandlerDecl: decl,
				File:        file,
				HandlerObj:  obj,
				HandlerLit:  lit,
				Pos:         pkg.Fset.Position(call.Pos()),
			})
		}
	}
	return routes
}

// isGinRouterMethodCall reports whether sel is a method call on a gin
// *Engine/*RouterGroup — checked via the selected method's own declared
// receiver (always *gin.RouterGroup, whether the call is on an Engine by
// promotion or on a group directly), falling back to the receiver
// expression's static type.
func isGinRouterMethodCall(pkg *packages.Package, sel *ast.SelectorExpr) bool {
	if seln, ok := pkg.TypesInfo.Selections[sel]; ok {
		if fn, ok := seln.Obj().(*types.Func); ok {
			if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
				return isGinRouterType(sig.Recv().Type())
			}
		}
	}
	return isGinRouterType(pkg.TypesInfo.TypeOf(sel.X))
}

// isGinRouterType reports whether t is a gin type a route can be
// registered on: the concrete *gin.Engine / *gin.RouterGroup, or the
// gin.IRoutes / gin.IRouter interfaces that a ".Use(mw)" chain returns
// (g.Group("/x").Use(mw).POST(...) — the route method's receiver is the
// interface, not the concrete group).
func isGinRouterType(t types.Type) bool {
	return isGinNamedType(t, "Engine") || isGinNamedType(t, "RouterGroup") ||
		isGinNamedType(t, "IRoutes") || isGinNamedType(t, "IRouter")
}

// isGinNamedType reports whether t is the gin package's named type
// `name`, with or without a leading pointer.
func isGinNamedType(t types.Type, name string) bool {
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
	return named.Obj().Pkg().Path() == ginPkgPath && named.Obj().Name() == name
}

// joinPath joins a group prefix and a relative path the way gin's own
// router does, so the result matches the path gin actually serves. Gin
// roots every route at the engine's "/" base and joins with path.Join
// (gin.joinPaths), so the served path is ALWAYS absolute — even when the
// group prefix is empty ("g.Group(\"\")") and the relative path has no
// leading slash ("current/user" -> "/current/user"). A naive prefix+sub
// concatenation would emit "current/user", which is not a valid OpenAPI
// path and fails document validation for the whole spec. path.Join drops
// a trailing slash, which gin preserves when the relative path ended in
// one, so that case is restored explicitly.
func joinPath(prefix, sub string) string {
	if prefix == "" {
		prefix = "/"
	}
	joined := path.Join(prefix, sub)
	if strings.HasSuffix(sub, "/") && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined
}

// normalizePath converts a gin path into an OpenAPI path template:
// ":name" segments become "{name}". A "*name" catch-all has no OpenAPI
// path-template equivalent, so a path containing one is declined
// (ok=false) rather than approximated.
func normalizePath(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		switch {
		case strings.HasPrefix(seg, ":"):
			segments[i] = "{" + seg[1:] + "}"
		case strings.HasPrefix(seg, "*"):
			return "", false
		}
	}
	return strings.Join(segments, "/"), true
}

// constStringSlice evaluates e as a "[]string{a, b, ...}" literal of
// compile-time string constants (gin's Match first argument), declining
// unless e is exactly that shape.
func constStringSlice(e ast.Expr, info *types.Info) ([]string, bool) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok || len(lit.Elts) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		s, ok := constStringArg(elt, info)
		if !ok {
			return nil, false
		}
		out = append(out, strings.ToUpper(s))
	}
	return out, true
}

// stringLiteral extracts the value of a string literal expression.
// Duplicated from nethttp/chi, see internal/router/plugin.go's
// plugin-isolation contract.
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

// unwrapCall strips single-argument call layers off e (a HandlerFunc
// conversion or middleware wrapper). Duplicated from nethttp/chi.
func unwrapCall(e ast.Expr) ast.Expr {
	for {
		call, ok := e.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return e
		}
		e = call.Args[0]
	}
}

// resolveHandler extracts the handler name from a bare identifier, a
// method value on a receiver, a qualified identifier from another
// package, or a single-arg-call conversion/wrapper of any of those,
// plus the go/types object it resolves to (populated even when decl is
// nil, for internal/generate's cross-package backfill). An inline
// function literal (r.GET("/x", func(c *gin.Context){...})) has no name
// or object; it's returned via lit so internal/generate can infer its
// body and synthesize an operationId. Duplicated from nethttp/chi (with
// the added func-literal case); gin's only structural difference is the
// call site (the handler is the variadic last argument), not this body.
func resolveHandler(pkg *packages.Package, e ast.Expr, decls map[types.Object]astutil.FuncDeclInfo) (name string, decl *ast.FuncDecl, file *ast.File, obj types.Object, lit *ast.FuncLit, ok bool) {
	switch expr := unwrapCall(e).(type) {
	case *ast.Ident:
		identObj := pkg.TypesInfo.Uses[expr]
		rd := decls[identObj]
		return expr.Name, rd.Decl, rd.File, identObj, nil, identObj != nil
	case *ast.SelectorExpr:
		var o types.Object
		if selection, ok := pkg.TypesInfo.Selections[expr]; ok {
			o = selection.Obj() // method value, e.g. srv.GetUser
		} else {
			o = pkg.TypesInfo.Uses[expr.Sel] // qualified identifier, e.g. handlers.GetUser
		}
		rd := decls[o]
		return expr.Sel.Name, rd.Decl, rd.File, o, nil, o != nil
	case *ast.FuncLit:
		// An inline handler: no name/decl/object to resolve — the body
		// itself is the handler, carried through for body inference.
		return "", nil, nil, nil, expr, true
	}
	return "", nil, nil, nil, nil, false
}

// constStringArg evaluates e as a compile-time string constant.
// Duplicated from nethttp/chi.
func constStringArg(e ast.Expr, info *types.Info) (string, bool) {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}
