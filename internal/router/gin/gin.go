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
// r.Group("/api/v1")" then "v1.GET(...)". groupPrefix resolves that
// chain. Known, deliberate residual gaps (declined, not guessed):
//
//   - A function taking a *gin.RouterGroup parameter and registering
//     routes on it (e.g. "func registerV1(rg *gin.RouterGroup) {
//     rg.GET("/tags", ...) }") — its routes are declined, NOT walked at
//     the empty prefix. This is the OPPOSITE posture from the chi
//     plugin: gin group functions register RELATIVE paths (rg.GET("/tags")
//     means the group's own prefix + "/tags"), so walking one at the
//     empty prefix would emit "/tags" — a path gin never serves. A
//     *gin.RouterGroup parameter has no prefix a single-package Extract
//     can recover, so it's declined.
//   - A group variable assigned more than once (ambiguous prefix), a
//     non-constant Group path, a group whose receiver chain can't be
//     resolved to a constant prefix, and a "*name" catch-all path.
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

func (p *Plugin) Extract(pkg *packages.Package) ([]router.Route, error) {
	if pkg.Fset == nil {
		return nil, fmt.Errorf("gin: package %s has no Fset", pkg.PkgPath)
	}

	funcDecls := astutil.IndexFuncDecls([]*packages.Package{pkg})
	groupDefs := collectGroupDefs(pkg)

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
			routes = append(routes, extractRoute(pkg, call, sel, groupDefs, funcDecls)...)
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
func groupPrefix(pkg *packages.Package, recvExpr ast.Expr, groupDefs map[types.Object]groupDef, visiting map[types.Object]bool) (string, bool) {
	// A *gin.Engine (a var, a parameter, or an inline gin.New()/
	// gin.Default() call) is the root: empty prefix. Matched by type, so
	// it doesn't matter how the engine was obtained.
	if isGinNamedType(pkg.TypesInfo.TypeOf(recvExpr), "Engine") {
		return "", true
	}
	switch e := recvExpr.(type) {
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok || !isGinRouterMethodCall(pkg, sel) {
			return "", false
		}
		switch sel.Sel.Name {
		case "Use":
			// ".Use(middleware)" returns the same route group as
			// gin.IRoutes; it's transparent for the prefix, so recurse
			// into its receiver: g.Group("/x").Use(mw).POST(...) keeps the
			// "/x" prefix.
			return groupPrefix(pkg, sel.X, groupDefs, visiting)
		case "Group":
			// An inline chained group: X.Group("/v1").GET(...).
			if len(e.Args) < 1 {
				return "", false
			}
			path, ok := stringLiteral(e.Args[0])
			if !ok {
				return "", false
			}
			parent, ok := groupPrefix(pkg, sel.X, groupDefs, visiting)
			if !ok {
				return "", false
			}
			return joinPath(parent, path), true
		}
		return "", false
	case *ast.Ident:
		obj := pkg.TypesInfo.Uses[e]
		if obj == nil || visiting[obj] {
			return "", false
		}
		gd, ok := groupDefs[obj]
		if !ok {
			return "", false // untracked/ambiguous group var, or a *gin.RouterGroup parameter
		}
		visiting[obj] = true
		parent, ok := groupPrefix(pkg, gd.recv, groupDefs, visiting)
		if !ok {
			return "", false
		}
		return joinPath(parent, gd.path), true
	}
	return "", false
}

// extractRoute turns one recognized route call into Routes, resolving
// its prefix, path, method(s) and handler. Declines (nil) on any
// unresolvable piece.
func extractRoute(pkg *packages.Package, call *ast.CallExpr, sel *ast.SelectorExpr, groupDefs map[types.Object]groupDef, funcDecls map[types.Object]astutil.FuncDeclInfo) []router.Route {
	prefix, ok := groupPrefix(pkg, sel.X, groupDefs, map[types.Object]bool{})
	if !ok {
		return nil
	}
	switch name := sel.Sel.Name; name {
	case "Any":
		return routeAt(pkg, allMethods, call, 0, prefix, funcDecls)
	case "Handle":
		if len(call.Args) < 3 {
			return nil
		}
		method, ok := constStringArg(call.Args[0], pkg.TypesInfo)
		if !ok {
			return nil
		}
		return routeAt(pkg, []string{strings.ToUpper(method)}, call, 1, prefix, funcDecls)
	case "Match":
		if len(call.Args) < 3 {
			return nil
		}
		methods, ok := constStringSlice(call.Args[0], pkg.TypesInfo)
		if !ok {
			return nil
		}
		return routeAt(pkg, methods, call, 1, prefix, funcDecls)
	default:
		return routeAt(pkg, []string{routeMethods[name]}, call, 0, prefix, funcDecls)
	}
}

// routeAt builds one Route per emittable method: pathIdx is the argument
// holding the relative path, the handler is always the last argument
// (gin's variadic middleware precede it), and a non-emittable method
// (e.g. CONNECT) is skipped. Declines the whole call (nil) if there's no
// handler argument, the path isn't a literal, or the path has no OpenAPI
// equivalent.
func routeAt(pkg *packages.Package, methods []string, call *ast.CallExpr, pathIdx int, prefix string, funcDecls map[types.Object]astutil.FuncDeclInfo) []router.Route {
	if len(call.Args) < pathIdx+2 { // path + at least one handler
		return nil
	}
	rawPath, ok := stringLiteral(call.Args[pathIdx])
	if !ok {
		return nil
	}
	path, ok := normalizePath(joinPath(prefix, rawPath))
	if !ok {
		return nil
	}
	handlerName, decl, file, obj, ok := resolveHandler(pkg, call.Args[len(call.Args)-1], funcDecls)
	if !ok {
		return nil
	}
	var routes []router.Route
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
			Pos:         pkg.Fset.Position(call.Pos()),
		})
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
// nil, for internal/generate's cross-package backfill). Duplicated from
// nethttp/chi; gin's only difference is the call site (the handler is
// the variadic last argument), not this resolution body.
func resolveHandler(pkg *packages.Package, e ast.Expr, decls map[types.Object]astutil.FuncDeclInfo) (name string, decl *ast.FuncDecl, file *ast.File, obj types.Object, ok bool) {
	switch expr := unwrapCall(e).(type) {
	case *ast.Ident:
		identObj := pkg.TypesInfo.Uses[expr]
		rd := decls[identObj]
		return expr.Name, rd.Decl, rd.File, identObj, identObj != nil
	case *ast.SelectorExpr:
		var o types.Object
		if selection, ok := pkg.TypesInfo.Selections[expr]; ok {
			o = selection.Obj() // method value, e.g. srv.GetUser
		} else {
			o = pkg.TypesInfo.Uses[expr.Sel] // qualified identifier, e.g. handlers.GetUser
		}
		rd := decls[o]
		return expr.Sel.Name, rd.Decl, rd.File, o, o != nil
	}
	return "", nil, nil, nil, false
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
