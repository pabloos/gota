// Package gorilla is a router plugin for github.com/gorilla/mux. It
// statically recognizes route registrations on a *mux.Router:
//
//	r.HandleFunc("/users/{id}", GetUser).Methods("GET")     // method(s) via a chained .Methods()
//	r.Handle("/users", http.HandlerFunc(CreateUser)).Methods("POST")
//	r.HandleFunc("/health", HealthCheck)                    // no .Methods() -> every HTTP method
//	s := r.PathPrefix("/api/v1").Subrouter()                // a subrouter variable carries a path prefix,
//	s.HandleFunc("/users", ListUsers)                       //   accumulated across nested subrouters -> "/api/v1/users"
//
// It does not import the real mux: recognition is pure go/types
// path/name matching against the analyzed target's own type-checked
// packages, the same mechanism internal/router/nethttp uses for
// *http.ServeMux — gota's own build never depends on gorilla/mux.
//
// mux handlers are plain func(w http.ResponseWriter, r *http.Request)
// using ordinary encoding/json — gorilla introduces no response-writing
// idiom of its own — so this plugin pairs with the existing
// inference.NetHTTP() dialect unchanged.
//
// # Methods
//
// A route's HTTP methods come from a .Methods("GET", ...) call chained
// onto the *mux.Route that HandleFunc/Handle returns — anywhere in the
// builder chain (.Methods(...).Name(...), .Schemes(...).Methods(...)).
// A registration with no .Methods() at all matches every method (that is
// gorilla's own semantics), so it expands to one operation per HTTP
// method, the same as a method-less net/http ServeMux pattern. A
// .Methods() whose arguments aren't compile-time-constant strings is
// declined (the route's methods are unknowable) rather than guessed.
//
// # Middleware indirection
//
// The handler passed to Handle/HandleFunc is frequently the real handler
// wrapped in middleware — the point of indirection body inference has to
// see through. resolveHandler unwraps it type-aware: it follows the one
// argument of each wrapping call whose type is http.Handler-shaped
// (http.Handler, http.HandlerFunc, or func(http.ResponseWriter,
// *http.Request)), down to the real handler. This covers a single-arg
// wrapper (http.HandlerFunc(H), authMiddleware(H)), a multi-argument
// middleware whose other arguments aren't handlers (gorilla's own
// handlers.LoggingHandler(os.Stdout, H)), and a curried one
// (handlers.CORS(opts)(H)). It stops where no single handler-shaped
// argument identifies the next hop (zero, or two-or-more). Router-level
// middleware (r.Use(mw)) doesn't wrap a specific handler and is
// correctly irrelevant to inference.
//
// The handler is also commonly produced by a factory call returning
// http.Handler — a free function (Handle(p, made())) or a method
// (Handle(p, controller.build())), the idiom of returning a
// handler already wrapped in middleware. The route is recognized via the
// factory's own name (its operationId), and the factory's body is where
// body inference then looks.
//
// A subrouter built with NewRoute().Subrouter() (a middleware-only
// subrouter, "sec := root.NewRoute().Subrouter()") adds no path segment
// but INHERITS the parent's accumulated prefix, so routes on it keep the
// parent's path.
//
// # Path parameters
//
// mux templates use "{name}" (already OpenAPI's syntax) and
// "{name:regex}" constraints, which degrade to the bare "{name}" (no
// direct OpenAPI equivalent) — including a "{rest:.*}" catch-all, which
// becomes "{rest}".
//
// # Mounts
//
// A Handle whose handler is itself a *mux.Router is a sub-router mount,
// not an endpoint: it's declined rather than emitted as a catch-all
// operation (which would just duplicate the sub-router's own routes,
// including mux's "root.Handle("/api/{rest:.*}", sub)" subpath idiom).
// The sub-router's routes are extracted where they're registered.
// Propagating a mount's prefix onto those routes when the sub-router is
// built in a DIFFERENT package (root.PathPrefix(p).Handler(
// http.StripPrefix(p, pkg.NewController().Router()))) is the cross-package
// mount gap — the mount site is invisible to a single-package Extract, so
// such routes surface at the prefix they carry themselves, the same
// boundary as chi's cross-package Mount.
//
// Known, deliberate v1 gaps (declined, not guessed):
//
//   - The builder form that splits the path and handler across the chain
//     (r.Path("/x").HandlerFunc(H), r.Methods("GET").Path("/x").Handler(h)):
//     this plugin anchors on HandleFunc/Handle, which carry both the path
//     and the handler in one call.
//   - A subrouter variable assigned more than once (ambiguous prefix), a
//     non-constant PathPrefix/Path template, and a subrouter whose
//     receiver chain can't be resolved to a constant prefix.
//   - A non-constant registration path or a non-constant .Methods()
//     argument.
package gorilla

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

const (
	pluginName     = "gorilla/mux"
	gorillaPkgPath = "github.com/gorilla/mux"
)

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return pluginName }

// allMethods is what a registration with no .Methods() restriction
// expands to — gorilla matches every method in that case. CONNECT is
// excluded (no OpenAPI Path Item slot), same as nethttp/chi/gin.
var allMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodHead, http.MethodOptions, http.MethodTrace,
}

// httpMethods is the set gota can emit (an OpenAPI slot exists); CONNECT
// is deliberately absent, so a .Methods("CONNECT") element is skipped.
var httpMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true,
	http.MethodDelete: true, http.MethodHead: true, http.MethodOptions: true, http.MethodTrace: true,
}

func (p *Plugin) Extract(pkg *packages.Package) ([]router.Route, error) {
	if pkg.Fset == nil {
		return nil, fmt.Errorf("gorilla: package %s has no Fset", pkg.PkgPath)
	}

	funcDecls := astutil.IndexFuncDecls([]*packages.Package{pkg})
	subrouterDefs, declined := collectSubrouterDefs(pkg)
	methodSpecs := collectMethods(pkg)

	var routes []router.Route
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") {
				return true
			}
			if !isGorillaRouterCall(pkg, sel) {
				return true
			}
			routes = append(routes, extractRoute(pkg, call, sel, subrouterDefs, declined, methodSpecs, funcDecls)...)
			return true
		})
	}
	return routes, nil
}

// methodSpec records what a .Methods() chained onto a registration
// resolved to: methods when every argument was a constant string, or
// ok=false when a .Methods() was present but its arguments weren't
// resolvable (the route's methods are then unknowable, so it's declined).
// A registration with no methodSpec at all had no .Methods() and matches
// every method.
type methodSpec struct {
	methods []string
	ok      bool
}

// extractRoute turns one HandleFunc/Handle call into Routes: its prefix
// (from the receiver subrouter), path, methods (from a chained
// .Methods(), else every method), and handler. Declines (nil) on any
// unresolvable piece.
func extractRoute(pkg *packages.Package, call *ast.CallExpr, sel *ast.SelectorExpr, subDefs map[types.Object]subrouterDef, declined map[types.Object]bool, specs map[*ast.CallExpr]methodSpec, funcDecls map[types.Object]astutil.FuncDeclInfo) []router.Route {
	if len(call.Args) < 2 {
		return nil
	}
	rawPath, ok := stringLiteral(call.Args[0])
	if !ok {
		return nil
	}
	prefix, ok := subrouterPrefix(pkg, sel.X, subDefs, declined, map[types.Object]bool{})
	if !ok {
		return nil
	}
	path, ok := normalizePath(joinPath(prefix, rawPath))
	if !ok {
		return nil
	}
	// A Handle whose handler is itself a *mux.Router is a sub-router MOUNT
	// (root.Handle("/api", sub), and mux's root.Handle("/api/{rest:.*}", sub)
	// subpath idiom), not an endpoint — a *mux.Router satisfies http.Handler
	// but serving it means dispatching into its own routes, which are
	// extracted where they're registered. Emitting it as an endpoint would
	// invent a catch-all operation for every method that just duplicates
	// those routes, so it's declined. (Applying the mount prefix to the
	// sub-router's own routes is the cross-package Mount gap, as in chi.)
	if isGorillaNamedType(pkg.TypesInfo.TypeOf(call.Args[1]), "Router") {
		return nil
	}
	handlerName, decl, file, obj, lit, ok := resolveHandler(pkg, call.Args[1], funcDecls)
	if !ok {
		return nil
	}

	methods := allMethods
	if spec, present := specs[call]; present {
		if !spec.ok {
			return nil // a .Methods() was there but unresolvable — decline rather than guess
		}
		methods = spec.methods
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
			HandlerLit:  lit,
			Pos:         pkg.Fset.Position(call.Pos()),
		})
	}
	return routes
}

// subrouterDef records how a *mux.Router subrouter variable was created:
// the parent router expression it hung off and the constant PathPrefix/
// Path template that gives its prefix.
type subrouterDef struct {
	recv   ast.Expr
	prefix string
}

// collectSubrouterDefs maps each *mux.Router subrouter variable to its
// defining "v := X.PathPrefix(p).Subrouter()" (or X.Path(p).Subrouter()).
// A variable assigned more than once is ambiguous and omitted, so
// subrouterPrefix declines any route on it rather than guessing.
func collectSubrouterDefs(pkg *packages.Package) (map[types.Object]subrouterDef, map[types.Object]bool) {
	defs := map[types.Object]subrouterDef{}
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
			if obj == nil || !isGorillaNamedType(obj.Type(), "Router") {
				return true
			}
			// An identity-preserving self-reconfiguration ("root =
			// root.StrictSlash(true)") reassigns the variable to a chained
			// config method call on ITSELF — the same underlying router, not
			// a new one. It must not count as an ambiguous reassignment, or a
			// router configured this way (idiomatic gorilla) would be wrongly
			// declined. A reassignment to a call on something else — notably
			// "root = root.PathPrefix(p).Subrouter()", whose receiver is the
			// PathPrefix route, not root — is genuinely ambiguous and still
			// counts.
			if isSelfMethodCall(pkg, assign.Rhs[0], obj) {
				return true
			}
			assigns[obj]++
			// RHS must be <route>.Subrouter().
			sub, ok := assign.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			subSel, ok := sub.Fun.(*ast.SelectorExpr)
			if !ok || subSel.Sel.Name != "Subrouter" {
				return true
			}
			prefix, parent, ok := prefixRoute(pkg, subSel.X)
			if !ok {
				return true
			}
			defs[obj] = subrouterDef{recv: parent, prefix: prefix}
			return true
		})
	}
	// A *mux.Router variable assigned more than once is ambiguous: which
	// prefix applies where is unknowable, and (unlike gin, where a group
	// and the engine are different types) a root router and a subrouter
	// share the type *mux.Router, so an untracked one can't be assumed to
	// be the root. Such variables are declined, not treated as the root.
	declined := map[types.Object]bool{}
	for obj, n := range assigns {
		if n > 1 {
			delete(defs, obj)
			declined[obj] = true
		}
	}
	return defs, declined
}

// isSelfMethodCall reports whether rhs is a method call "v.M(...)" whose
// receiver v is directly the variable obj — an identity-preserving
// self-reconfiguration like "root = root.StrictSlash(true)". A call whose
// receiver is a further expression on v (e.g. v.PathPrefix(p).Subrouter())
// is not: its receiver is that intermediate route, not v itself.
func isSelfMethodCall(pkg *packages.Package, rhs ast.Expr, obj types.Object) bool {
	call, ok := rhs.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && pkg.TypesInfo.Uses[ident] == obj
}

// prefixRoute reads the *mux.Route expression a .Subrouter() was called
// on, returning the path segment it contributes and the parent router it
// was built from. It recognizes "<router>.PathPrefix(constTpl)" and
// "<router>.Path(constTpl)" (contributing that template) and
// "<router>.NewRoute()" (contributing nothing — an empty route used to
// hang a middleware-only subrouter off the parent, which must still
// INHERIT the parent's accumulated prefix). Declines anything else.
func prefixRoute(pkg *packages.Package, e ast.Expr) (prefix string, parent ast.Expr, ok bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return "", nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", nil, false
	}
	switch sel.Sel.Name {
	case "NewRoute":
		if len(call.Args) != 0 {
			return "", nil, false
		}
		return "", sel.X, true
	case "PathPrefix", "Path":
		if len(call.Args) != 1 {
			return "", nil, false
		}
		tpl, ok := stringLiteral(call.Args[0])
		if !ok {
			return "", nil, false
		}
		return tpl, sel.X, true
	}
	return "", nil, false
}

// subrouterPrefix resolves the accumulated path prefix of the router
// expression a route was registered on, or ok=false to decline. A root
// router (mux.NewRouter(), a *mux.Router variable or parameter not
// tracked as a subrouter) has the empty prefix; a subrouter variable or
// an inline "X.PathPrefix(p).Subrouter()" chain accumulates. Recurses
// through subrouter chains, guarding against a reassignment cycle.
func subrouterPrefix(pkg *packages.Package, recvExpr ast.Expr, subDefs map[types.Object]subrouterDef, declined map[types.Object]bool, visiting map[types.Object]bool) (string, bool) {
	switch e := recvExpr.(type) {
	case *ast.Ident:
		obj := pkg.TypesInfo.Uses[e]
		if obj == nil {
			return "", false
		}
		if declined[obj] {
			return "", false // reassigned/ambiguous subrouter variable
		}
		sd, tracked := subDefs[obj]
		if !tracked {
			return "", true // a root router variable or parameter — empty prefix
		}
		if visiting[obj] {
			return "", false
		}
		visiting[obj] = true
		parent, ok := subrouterPrefix(pkg, sd.recv, subDefs, declined, visiting)
		if !ok {
			return "", false
		}
		return joinPath(parent, sd.prefix), true
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		if sel.Sel.Name == "Subrouter" {
			// An inline "X.PathPrefix(p).Subrouter().HandleFunc(...)".
			prefix, parent, ok := prefixRoute(pkg, sel.X)
			if !ok {
				return "", false
			}
			parentPrefix, ok := subrouterPrefix(pkg, parent, subDefs, declined, visiting)
			if !ok {
				return "", false
			}
			return joinPath(parentPrefix, prefix), true
		}
		// Any other call returning a *mux.Router (mux.NewRouter(), a
		// helper that builds one) is a root — empty prefix.
		if isGorillaNamedType(pkg.TypesInfo.TypeOf(recvExpr), "Router") {
			return "", true
		}
		return "", false
	}
	// A selector/field or other *mux.Router expression: treat a plain
	// router as root, decline anything untyped.
	if isGorillaNamedType(pkg.TypesInfo.TypeOf(recvExpr), "Router") {
		return "", true
	}
	return "", false
}

// collectMethods maps each HandleFunc/Handle registration call to the
// methods a chained .Methods() restricts it to. A .Methods() with a
// non-constant argument records an unresolved spec (ok=false) so the
// route is declined rather than emitted with guessed methods.
func collectMethods(pkg *packages.Package) map[*ast.CallExpr]methodSpec {
	out := map[*ast.CallExpr]methodSpec{}
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Methods" {
				return true
			}
			if !isGorillaRouteOrRouterCall(pkg, sel) {
				return true
			}
			reg := registrationOf(sel.X)
			if reg == nil {
				return true
			}
			methods, ok := constMethodList(call.Args, pkg.TypesInfo)
			out[reg] = methodSpec{methods: methods, ok: ok}
			return true
		})
	}
	return out
}

// registrationOf walks a *mux.Route builder chain back to the
// HandleFunc/Handle call that started it, stepping through intermediate
// builder methods (.Schemes, .Host, .Name, .Queries, .Methods, ...).
// Returns nil if the chain doesn't bottom out at a HandleFunc/Handle.
func registrationOf(routeExpr ast.Expr) *ast.CallExpr {
	for {
		call, ok := routeExpr.(*ast.CallExpr)
		if !ok {
			return nil
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return nil
		}
		if sel.Sel.Name == "HandleFunc" || sel.Sel.Name == "Handle" {
			return call
		}
		routeExpr = sel.X
	}
}

// constMethodList evaluates args as compile-time-constant, uppercased
// HTTP method strings (gorilla's .Methods variadic). ok=false if any
// argument isn't a constant string, or there are none.
func constMethodList(args []ast.Expr, info *types.Info) ([]string, bool) {
	if len(args) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		s, ok := constStringArg(a, info)
		if !ok {
			return nil, false
		}
		out = append(out, strings.ToUpper(s))
	}
	return out, true
}

// isGorillaRouterCall reports whether sel is a method call on a
// *mux.Router (where HandleFunc/Handle live).
func isGorillaRouterCall(pkg *packages.Package, sel *ast.SelectorExpr) bool {
	if seln, ok := pkg.TypesInfo.Selections[sel]; ok {
		if fn, ok := seln.Obj().(*types.Func); ok {
			if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
				return isGorillaNamedType(sig.Recv().Type(), "Router")
			}
		}
	}
	return isGorillaNamedType(pkg.TypesInfo.TypeOf(sel.X), "Router")
}

// isGorillaRouteOrRouterCall reports whether sel is a method call on a
// *mux.Route or *mux.Router — .Methods() exists on both.
func isGorillaRouteOrRouterCall(pkg *packages.Package, sel *ast.SelectorExpr) bool {
	if seln, ok := pkg.TypesInfo.Selections[sel]; ok {
		if fn, ok := seln.Obj().(*types.Func); ok {
			if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
				t := sig.Recv().Type()
				return isGorillaNamedType(t, "Route") || isGorillaNamedType(t, "Router")
			}
		}
	}
	t := pkg.TypesInfo.TypeOf(sel.X)
	return isGorillaNamedType(t, "Route") || isGorillaNamedType(t, "Router")
}

// isGorillaNamedType reports whether t is the gorilla/mux package's named
// type `name`, with or without a leading pointer.
func isGorillaNamedType(t types.Type, name string) bool {
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
	return named.Obj().Pkg().Path() == gorillaPkgPath && named.Obj().Name() == name
}

// joinPath concatenates a subrouter prefix and a relative path the way
// mux does: the prefix's own trailing slash (if any) is trimmed, then
// sub is appended keeping its leading slash.
func joinPath(prefix, sub string) string {
	return strings.TrimSuffix(prefix, "/") + sub
}

// normalizePath converts a mux path template into an OpenAPI path
// template: a "{name}" is kept, a "{name:regex}" constraint degrades to
// the bare "{name}" (including a "{rest:.*}" catch-all -> "{rest}"). An
// unterminated "{" declines the path.
func normalizePath(pattern string) (string, bool) {
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

// resolveHandler unwraps any middleware wrapping the handler (see
// unwrapToHandler) and extracts its name from a bare identifier, a method
// value, a qualified identifier from another package, plus the go/types
// object it resolves to (populated even when decl is nil, for
// internal/generate's cross-package backfill). An inline handler closure
// has no name or object and is returned via lit. Duplicated from
// nethttp/chi (with gorilla's middleware-aware unwrap).
func resolveHandler(pkg *packages.Package, e ast.Expr, decls map[types.Object]astutil.FuncDeclInfo) (name string, decl *ast.FuncDecl, file *ast.File, obj types.Object, lit *ast.FuncLit, ok bool) {
	switch expr := unwrapToHandler(pkg, e).(type) {
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
		return "", nil, nil, nil, expr, true
	case *ast.CallExpr:
		// The handler is produced by a factory call returning http.Handler
		// — a free function (made()) or a method
		// (controller.build()), the idiom of returning a handler
		// already wrapped in middleware. Recognize the route via the
		// callee's own name and object; the factory's body is where body
		// inference then looks. (A call that instead wraps a handler
		// argument was already unwrapped by unwrapToHandler above.)
		if isHTTPHandlerShaped(pkg.TypesInfo.TypeOf(expr)) {
			return resolveHandler(pkg, expr.Fun, decls)
		}
	}
	return "", nil, nil, nil, nil, false
}

// unwrapToHandler follows middleware/conversion layers wrapping the real
// handler down to it: at each call it steps into the single argument
// whose type is http.Handler-shaped, covering a single-arg wrapper
// (http.HandlerFunc(H), authMiddleware(H)), a multi-argument middleware
// whose other arguments aren't handlers (handlers.LoggingHandler(out, H)),
// and a curried one (handlers.CORS(opts)(H)). It stops when no single
// handler-shaped argument identifies the next hop — zero, or two-or-more
// (ambiguous), or the expression isn't a call.
func unwrapToHandler(pkg *packages.Package, e ast.Expr) ast.Expr {
	for {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return e
		}
		var handlerArg ast.Expr
		count := 0
		for _, arg := range call.Args {
			if isHTTPHandlerShaped(pkg.TypesInfo.TypeOf(arg)) {
				handlerArg = arg
				count++
			}
		}
		if count != 1 {
			return e
		}
		e = handlerArg
	}
}

// isHTTPHandlerShaped reports whether t is an http.Handler, an
// http.HandlerFunc, or a bare func(http.ResponseWriter, *http.Request) —
// the shapes a middleware layer passes the wrapped handler as.
func isHTTPHandlerShaped(t types.Type) bool {
	if t == nil {
		return false
	}
	if named, ok := t.(*types.Named); ok {
		if o := named.Obj(); o.Pkg() != nil && o.Pkg().Path() == "net/http" &&
			(o.Name() == "Handler" || o.Name() == "HandlerFunc") {
			return true
		}
	}
	sig, ok := t.Underlying().(*types.Signature)
	return ok && isHandlerSignature(sig)
}

// isHandlerSignature reports whether sig is func(http.ResponseWriter,
// *http.Request) with no results.
func isHandlerSignature(sig *types.Signature) bool {
	if sig.Params().Len() != 2 || sig.Results().Len() != 0 {
		return false
	}
	p0, ok := sig.Params().At(0).Type().(*types.Named)
	if !ok || p0.Obj().Pkg() == nil || p0.Obj().Pkg().Path() != "net/http" || p0.Obj().Name() != "ResponseWriter" {
		return false
	}
	p1, ok := sig.Params().At(1).Type().(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := p1.Elem().(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "net/http" && named.Obj().Name() == "Request"
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

// constStringArg evaluates e as a compile-time string constant.
// Duplicated from nethttp/chi.
func constStringArg(e ast.Expr, info *types.Info) (string, bool) {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}
