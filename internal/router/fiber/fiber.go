// Package fiber is a router plugin for github.com/gofiber/fiber/v2. It
// statically recognizes route registrations on a *fiber.App or on a
// fiber.Router (the interface a group is typed as):
//
//	app.Get("/users/:id", GetUser)               // method-specific: Get/Head/Post/Put/Delete/Options/Patch
//	app.All("/health", HealthCheck)              // every HTTP method
//	app.Add("GET", "/users/:id", GetUser)        // explicit method as a compile-time-constant string
//	v1 := app.Group("/api/v1")                    // a group variable carries a path prefix,
//	v1.Get("/users", ListUsers)                  //   accumulated across nested groups -> "/api/v1/users"
//
// It does not import the real fiber: recognition is pure go/types path/name
// matching against the analyzed target's own type-checked packages, the same
// mechanism the other plugins use — gota's own build never depends on fiber.
//
// Fiber handlers are func(c *fiber.Ctx) error, a shape unlike net/http's, so
// this plugin pairs with the fiber inference dialect (inference.Fiber()), not
// inference.NetHTTP(). The handler is the LAST argument of a route call
// (fiber's route methods are variadic "handlers ...Handler"; earlier
// arguments are middleware), as in Gin.
//
// Path parameters use fiber's ":name" syntax (an optional "?" suffix is
// dropped), translated to OpenAPI's "{name}". A "*" wildcard or "+" greedy
// segment has no OpenAPI path-template equivalent, so a route containing one
// is declined.
//
// The prefix of a route travels through the group VARIABLE it's registered
// on (object identity). Routes registered inside a register function taking
// a fiber.Router parameter are resolved to the prefix of the group passed at
// the call site — direct call and interface-dispatch registry loop alike,
// matched by call name + argument position (see collectParamPrefixes), the
// same mechanism as the gin and echo plugins. Known, deliberate residual
// gaps (declined, not guessed):
//
//   - The fiber.Router callback form app.Route(prefix, func(r fiber.Router)
//     {...}) is not followed (v1); Group covers the common case.
//   - A register function whose only call site is in another package, a
//     group variable assigned more than once, a non-constant Group/Add
//     path or method, and a "*"/"+" wildcard path.
package fiber

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
	pluginName   = "fiber"
	fiberPkgPath = "github.com/gofiber/fiber/v2"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return pluginName }

// routeMethods maps fiber's single-method registration calls (Go method
// names, e.g. "Get") to their HTTP verb. All/Add/Group are handled
// separately.
var routeMethods = map[string]string{
	"Get":     http.MethodGet,
	"Head":    http.MethodHead,
	"Post":    http.MethodPost,
	"Put":     http.MethodPut,
	"Delete":  http.MethodDelete,
	"Options": http.MethodOptions,
	"Patch":   http.MethodPatch,
}

// allMethods is what fiber's All expands to, restricted to the HTTP methods
// OpenAPI's Path Item Object has a slot for — fiber's All also registers
// CONNECT, which is excluded (same reasoning the other plugins use).
var allMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodHead, http.MethodOptions, http.MethodTrace,
}

// httpMethods is the set of methods gota can emit (an OpenAPI slot exists).
var httpMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true,
	http.MethodDelete: true, http.MethodHead: true, http.MethodOptions: true, http.MethodTrace: true,
}

// isRouteMethodName reports whether a selector name is a route-emitting fiber
// method. Group is consumed only by the prefix machinery; Route (the
// callback form) is deliberately not recognized (see the package doc).
func isRouteMethodName(name string) bool {
	if _, ok := routeMethods[name]; ok {
		return true
	}
	return name == "All" || name == "Add"
}

func (p *Plugin) Extract(pkg *packages.Package) ([]router.Route, error) {
	if pkg.Fset == nil {
		return nil, fmt.Errorf("fiber: package %s has no Fset", pkg.PkgPath)
	}

	funcDecls := astutil.IndexFuncDecls([]*packages.Package{pkg})
	groupDefs := collectGroupDefs(pkg)
	paramPrefixes := collectParamPrefixes(pkg, groupDefs)

	var routes []router.Route
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !isRouteMethodName(sel.Sel.Name) || !isFiberRouterMethodCall(pkg, sel) {
				return true
			}
			routes = append(routes, extractRoute(pkg, call, sel, groupDefs, paramPrefixes, funcDecls)...)
			return true
		})
	}
	return routes, nil
}

// groupDef records how a group variable was created: the receiver it was
// grouped off and the constant relative path.
type groupDef struct {
	recv ast.Expr
	path string
}

// collectGroupDefs maps each group variable (typed fiber.Router, the return
// of App.Group) to its defining "v := X.Group(constPath)". A variable
// assigned more than once is ambiguous and omitted.
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
			if obj == nil || !isFiberNamedType(obj.Type(), "Router") {
				return true
			}
			assigns[obj]++
			call, ok := assign.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Group" || !isFiberRouterMethodCall(pkg, sel) || len(call.Args) < 1 {
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

// groupPrefixes resolves the set of accumulated path prefixes the router
// expression recvExpr a route was registered on can have, or ok=false to
// decline. A set (not one prefix) because a register function's parameter
// can be called with several groups. Cycle-guarded.
func groupPrefixes(pkg *packages.Package, recvExpr ast.Expr, groupDefs map[types.Object]groupDef, paramPrefixes map[types.Object][]string, visiting map[types.Object]bool) ([]string, bool) {
	// A *fiber.App (a var, a parameter, or an inline fiber.New() call) is the
	// root: empty prefix. Matched by type.
	if isFiberNamedType(pkg.TypesInfo.TypeOf(recvExpr), "App") {
		return []string{""}, true
	}
	switch e := recvExpr.(type) {
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok || !isFiberRouterMethodCall(pkg, sel) {
			return nil, false
		}
		switch sel.Sel.Name {
		case "Use":
			// ".Use(mw)" returns the same router; transparent for the prefix.
			return groupPrefixes(pkg, sel.X, groupDefs, paramPrefixes, visiting)
		case "Group":
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
		// A fiber.Router parameter: its prefixes come from the call sites of
		// its enclosing register function (see collectParamPrefixes).
		if ps, ok := paramPrefixes[obj]; ok && len(ps) > 0 {
			return ps, true
		}
		return nil, false
	}
	return nil, false
}

// registerParam identifies a group-parameter register function by the call
// name that invokes it and the positional index of its fiber.Router
// argument.
type registerParam struct {
	name  string
	index int
}

// collectParamPrefixes maps each fiber.Router parameter that a route is
// registered on to the set of prefixes the calls in this package invoke its
// function/method with — resolving the "register function" idiom
// (func (h *Users) Routes(r fiber.Router) { r.Get(...) }, called as
// h.Routes(theGroup)). Matching is by (call name, argument index), reaching
// both a direct call and an interface dispatch. Single-package; resolved to
// a fixpoint. A *fiber.App parameter needs no entry here — it's always the
// root, handled directly in groupPrefixes.
func collectParamPrefixes(pkg *packages.Package, groupDefs map[types.Object]groupDef) map[types.Object][]string {
	byNameIdx := map[registerParam][]types.Object{}
	for _, file := range pkg.Syntax {
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Type.Params == nil {
				continue
			}
			idx := 0
			for _, field := range fn.Type.Params.List {
				isRouter := isFiberNamedType(pkg.TypesInfo.TypeOf(field.Type), "Router")
				names := field.Names
				if len(names) == 0 {
					idx++
					continue
				}
				for _, nm := range names {
					if isRouter {
						if obj := pkg.TypesInfo.Defs[nm]; obj != nil {
							key := registerParam{fn.Name.Name, idx}
							byNameIdx[key] = append(byNameIdx[key], obj)
						}
					}
					idx++
				}
			}
		}
	}
	if len(byNameIdx) == 0 {
		return nil
	}

	prefixes := map[types.Object][]string{}
	for changed := true; changed; {
		changed = false
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name, ok := calleeName(call.Fun)
				if !ok {
					return true
				}
				for i, arg := range call.Args {
					objs, ok := byNameIdx[registerParam{name, i}]
					if !ok {
						continue
					}
					argType := pkg.TypesInfo.TypeOf(arg)
					if !isFiberNamedType(argType, "Router") && !isFiberNamedType(argType, "App") {
						continue
					}
					argPrefixes, ok := groupPrefixes(pkg, arg, groupDefs, prefixes, map[types.Object]bool{})
					if !ok {
						continue
					}
					for _, obj := range objs {
						if addPrefixes(prefixes, obj, argPrefixes) {
							changed = true
						}
					}
				}
				return true
			})
		}
	}
	return prefixes
}

// calleeName returns the bare name a call expression invokes.
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

// joinAll joins sub onto every parent prefix, de-duplicating.
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

// extractRoute turns one recognized route call into Routes.
func extractRoute(pkg *packages.Package, call *ast.CallExpr, sel *ast.SelectorExpr, groupDefs map[types.Object]groupDef, paramPrefixes map[types.Object][]string, funcDecls map[types.Object]astutil.FuncDeclInfo) []router.Route {
	prefixes, ok := groupPrefixes(pkg, sel.X, groupDefs, paramPrefixes, map[types.Object]bool{})
	if !ok {
		return nil
	}
	switch name := sel.Sel.Name; name {
	case "All":
		return routeAt(pkg, allMethods, call, 0, prefixes, funcDecls)
	case "Add":
		if len(call.Args) < 3 {
			return nil
		}
		method, ok := constStringArg(call.Args[0], pkg.TypesInfo)
		if !ok {
			return nil
		}
		return routeAt(pkg, []string{strings.ToUpper(method)}, call, 1, prefixes, funcDecls)
	default:
		return routeAt(pkg, []string{routeMethods[name]}, call, 0, prefixes, funcDecls)
	}
}

// routeAt builds one Route per emittable method and prefix: pathIdx is the
// argument holding the relative path, and the handler is the last argument
// (fiber's variadic middleware precede it). Declines the whole call on any
// unresolvable piece.
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

// isFiberRouterMethodCall reports whether sel is a method call on a fiber
// router — checked via the selected method's declared receiver (the
// fiber.Router interface for a group, *fiber.App for the app), falling back
// to the receiver expression's static type.
func isFiberRouterMethodCall(pkg *packages.Package, sel *ast.SelectorExpr) bool {
	if seln, ok := pkg.TypesInfo.Selections[sel]; ok {
		if fn, ok := seln.Obj().(*types.Func); ok {
			if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
				return isFiberRouterType(sig.Recv().Type())
			}
		}
	}
	return isFiberRouterType(pkg.TypesInfo.TypeOf(sel.X))
}

// isFiberRouterType reports whether t is a fiber type a route can be
// registered on: *fiber.App, the fiber.Router interface, or *fiber.Group.
func isFiberRouterType(t types.Type) bool {
	return isFiberNamedType(t, "App") || isFiberNamedType(t, "Router") || isFiberNamedType(t, "Group")
}

// isFiberNamedType reports whether t is the fiber package's named type
// `name`, with or without a leading pointer.
func isFiberNamedType(t types.Type, name string) bool {
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
	return named.Obj().Pkg().Path() == fiberPkgPath && named.Obj().Name() == name
}

// joinPath joins a group prefix and a relative path into an absolute path,
// the way fiber composes a group prefix with a route's path. An empty prefix
// roots at "/"; a trailing slash on the relative path is preserved.
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

// normalizePath converts a fiber path into an OpenAPI path template:
// ":name" (with an optional "?" suffix dropped) becomes "{name}". A "*"
// wildcard or "+" greedy segment has no OpenAPI equivalent, so a path
// containing one is declined.
func normalizePath(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		switch {
		case strings.HasPrefix(seg, ":"):
			name := strings.TrimSuffix(seg[1:], "?")
			segments[i] = "{" + name + "}"
		case strings.HasPrefix(seg, "*"), strings.HasPrefix(seg, "+"):
			return "", false
		}
	}
	return strings.Join(segments, "/"), true
}

// stringLiteral extracts the value of a string literal expression.
// Duplicated per plugin-isolation contract (see internal/router/plugin.go).
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

// unwrapCall strips single-argument call layers off e.
func unwrapCall(e ast.Expr) ast.Expr {
	for {
		call, ok := e.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return e
		}
		e = call.Args[0]
	}
}

// resolveHandler extracts the handler name/decl/object from a bare
// identifier, a method value, a qualified identifier, a single-arg wrapper
// of any of those, or an inline func literal.
func resolveHandler(pkg *packages.Package, e ast.Expr, decls map[types.Object]astutil.FuncDeclInfo) (name string, decl *ast.FuncDecl, file *ast.File, obj types.Object, lit *ast.FuncLit, ok bool) {
	switch expr := unwrapCall(e).(type) {
	case *ast.Ident:
		identObj := pkg.TypesInfo.Uses[expr]
		rd := decls[identObj]
		return expr.Name, rd.Decl, rd.File, identObj, nil, identObj != nil
	case *ast.SelectorExpr:
		var o types.Object
		if selection, ok := pkg.TypesInfo.Selections[expr]; ok {
			o = selection.Obj()
		} else {
			o = pkg.TypesInfo.Uses[expr.Sel]
		}
		rd := decls[o]
		return expr.Sel.Name, rd.Decl, rd.File, o, nil, o != nil
	case *ast.FuncLit:
		return "", nil, nil, nil, expr, true
	}
	return "", nil, nil, nil, nil, false
}

// constStringArg evaluates e as a compile-time string constant.
func constStringArg(e ast.Expr, info *types.Info) (string, bool) {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}
