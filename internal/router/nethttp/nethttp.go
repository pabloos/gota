// Package nethttp is gota's Phase 1 router plugin. It statically recognizes
// route registrations against net/http's ServeMux:
//
//	mux.HandleFunc("GET /users/{id}", GetUser)
//	mux.Handle("POST /users", http.HandlerFunc(CreateUser))
//	http.HandleFunc("/health", HealthCheck)
//
// Patterns follow Go 1.22's enhanced ServeMux syntax ("METHOD /path"). Per
// net/http's own ServeMux docs, "a pattern with no method matches every
// method" — so a registration like http.HandleFunc("/health", HealthCheck)
// expands into one Route per HTTP method gota's model supports (GET, POST,
// PUT, PATCH, DELETE, HEAD, OPTIONS, TRACE), all bound to the same
// handler, rather than being narrowed to GET. Path parameters use the
// same "{name}" syntax as OpenAPI path templates, so no translation is
// needed between the two.
package nethttp

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

const pluginName = "net/http"

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return pluginName }

// registrationMethods are the ServeMux methods that bind a pattern to a handler.
var registrationMethods = map[string]bool{
	"HandleFunc": true,
	"Handle":     true,
}

func (p *Plugin) Extract(pkg *packages.Package, all []*packages.Package) ([]router.Route, error) {
	if pkg.Fset == nil {
		return nil, fmt.Errorf("nethttp: package %s has no Fset", pkg.PkgPath)
	}

	funcDecls := astutil.IndexFuncDecls([]*packages.Package{pkg})

	var routes []router.Route
	for _, file := range pkg.Syntax {
		var walkErr error
		ast.Inspect(file, func(n ast.Node) bool {
			if walkErr != nil {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !registrationMethods[sel.Sel.Name] {
				return true
			}
			if !isServeMuxOrHTTPPackageCall(pkg, sel) {
				return true
			}
			if len(call.Args) != 2 {
				return true
			}
			pattern, ok := stringLiteral(call.Args[0])
			if !ok {
				return true
			}
			methods, path := splitPattern(pattern)

			// A method-less pattern whose handler is an anonymous function
			// doing its own switch/if-else dispatch on r.Method (the
			// pre-Go-1.22 idiom for method dispatch on one path) needs a
			// completely different extraction: one Route per branch, each
			// bound to that branch's real delegate handler — not one Route
			// per allMethods entry bound to the closure itself. This must
			// run before resolveHandler, which now DOES accept a FuncLit
			// (as a plain inline handler): a dispatcher closure would
			// otherwise be misread as one inline handler for all methods
			// instead of its per-branch delegates.
			if len(methods) > 1 {
				if lit, ok := unwrapCall(call.Args[1]).(*ast.FuncLit); ok {
					if dispatched, ok := dispatchRoutes(pkg, lit, funcDecls, path, pkg.Fset.Position(call.Pos())); ok {
						routes = append(routes, dispatched...)
					}
					// A method-less inline closure is either a recognized
					// dispatcher (handled just above) or declined — never
					// treated as one all-methods catch-all handler, because a
					// hand-rolled dispatcher (the reason this branch exists)
					// can't be told apart from a real catch-all without a
					// deeper read of its body. An explicit-method inline
					// handler ("GET /x", func...) is unambiguous and IS
					// supported, via resolveHandler's FuncLit case below.
					return true
				}
			}

			handlerName, decl, declFile, handlerObj, lit, ok := resolveHandler(pkg, call.Args[1], funcDecls)
			if !ok {
				return true
			}
			for _, method := range methods {
				routes = append(routes, router.Route{
					Method:      method,
					Path:        path,
					HandlerName: handlerName,
					HandlerDecl: decl,
					File:        declFile,
					HandlerObj:  handlerObj,
					HandlerLit:  lit,
					Pos:         pkg.Fset.Position(call.Pos()),
				})
			}
			return true
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	return routes, nil
}

// isServeMuxOrHTTPPackageCall reports whether sel.X refers to a *http.ServeMux
// value or to the http package itself (for http.HandleFunc).
func isServeMuxOrHTTPPackageCall(pkg *packages.Package, sel *ast.SelectorExpr) bool {
	if pkg.TypesInfo == nil {
		return false
	}
	if ident, ok := sel.X.(*ast.Ident); ok {
		if pn, ok := pkg.TypesInfo.Uses[ident].(*types.PkgName); ok {
			return pn.Imported().Path() == "net/http"
		}
	}
	t := pkg.TypesInfo.TypeOf(sel.X)
	if t == nil {
		return false
	}
	return strings.Contains(t.String(), "net/http.ServeMux")
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

// splitPattern splits a Go 1.22+ ServeMux pattern ("METHOD /path" or
// "/path") into method (uppercased, "GET" if unspecified) and path.
// allMethods is what a ServeMux pattern with no method expands to — per
// net/http's ServeMux docs, "a pattern with no method matches every
// method." CONNECT is deliberately excluded: it's not one of the eight
// methods model.PathItem has a slot for (OpenAPI's Path Item Object has
// no "connect" field at all), so including it here would make every
// method-less pattern fail with emitter.Build's "no OpenAPI operation
// slot" error instead of gota's own model quietly reflecting the same gap.
var allMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodHead, http.MethodOptions, http.MethodTrace,
}

// splitPattern splits a Go 1.22+ ServeMux pattern ("METHOD /path" or
// "/path") into the set of HTTP methods it matches and the path. A
// pattern with an explicit method matches only that one; a pattern with
// no method matches every method in allMethods.
func splitPattern(pattern string) (methods []string, path string) {
	pattern = strings.TrimSpace(pattern)
	// Strip an optional host prefix ("example.com/path" or "METHOD example.com/path").
	if fields := strings.SplitN(pattern, " ", 2); len(fields) == 2 {
		m := strings.ToUpper(strings.TrimSpace(fields[0]))
		if isHTTPMethod(m) {
			return []string{m}, stripHost(strings.TrimSpace(fields[1]))
		}
	}
	return allMethods, stripHost(pattern)
}

func stripHost(pathPart string) string {
	if idx := strings.Index(pathPart, "/"); idx > 0 {
		return pathPart[idx:]
	}
	return pathPart
}

var httpMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true,
	http.MethodDelete: true, http.MethodHead: true, http.MethodOptions: true,
	http.MethodTrace: true, http.MethodConnect: true,
}

func isHTTPMethod(s string) bool { return httpMethods[s] }

// unwrapCall strips single-argument call layers off e — the shape of an
// http.HandlerFunc(...) conversion or a middleware(...) wrapper — down to
// whatever's inside. Shared by resolveHandler (which switches on the
// unwrapped result for an identifier/selector) and dispatchRoutes'
// FuncLit detection (which switches on it for an anonymous function).
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
// (HandleFunc(pattern, GetUser)), a method value on a receiver
// (HandleFunc(pattern, srv.GetUser)), a qualified identifier from another
// package (HandleFunc(pattern, handlers.GetUser)), or an http.HandlerFunc
// conversion of any of those, plus the go/types object it resolves to.
// decl/file are resolved via go/types object identity rather than name
// matching (so it works for methods too) against decls, which only covers
// the local package — they come back nil when the object lives outside
// it (e.g. a handler defined in a different package). obj is still
// returned in that case: internal/generate uses it to resolve the
// declaration across every loaded package, since a single plugin
// invocation only ever sees one.
// An inline handler (HandleFunc(pattern, func(w, r){...})) that isn't a
// method dispatcher has no name or object; it's returned via lit so
// internal/generate can infer its body and synthesize an operationId.
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

// methodBranch is one recognized "case http.MethodX:"/"if r.Method ==
// http.MethodX" branch of a hand-rolled dispatcher: the method it
// handles, and the single delegate call its body makes.
type methodBranch struct {
	method string
	call   *ast.CallExpr
}

// dispatchRoutes recognizes lit as a hand-rolled method dispatcher — the
// pre-Go-1.22 idiom for method dispatch on one ServeMux pattern, usually
// layered under one or more middleware-wrapping calls (e.g.
// "mux.Handle(pattern, secure(http.HandlerFunc(func(w,r){ switch
// r.Method {...} })))") — and, on a full match, returns one Route per
// branch, each bound to that branch's real delegate handler (resolved
// via resolveHandler, unchanged) rather than the anonymous closure
// itself. Declines (ok=false, not an error) unless lit's params are
// exactly (http.ResponseWriter, *http.Request)-shaped and its body
// matches methodBranches — see that function for exactly what shapes
// are and aren't recognized. This is deliberately narrow, not a general
// control-flow analysis: any registration that doesn't fit produces no
// routes at all, the same as today, rather than a partial guess.
func dispatchRoutes(pkg *packages.Package, lit *ast.FuncLit, decls map[types.Object]astutil.FuncDeclInfo, path string, pos token.Position) ([]router.Route, bool) {
	params := lit.Type.Params.List
	if len(params) != 2 || len(params[0].Names) != 1 || len(params[1].Names) != 1 {
		return nil, false
	}
	if lit.Body == nil {
		return nil, false
	}

	branches, ok := methodBranches(lit.Body.List, pkg.TypesInfo)
	if !ok {
		return nil, false
	}

	var routes []router.Route
	for _, b := range branches {
		handlerName, decl, declFile, handlerObj, lit, ok := resolveHandler(pkg, b.call.Fun, decls)
		if !ok || lit != nil {
			// One unresolvable branch aborts the whole registration (see
			// dispatchRoutes' doc comment). A branch that delegates to an
			// inline closure (an IIFE) is not a named handler and counts as
			// unresolvable here — a dispatcher's branches must each name a
			// real delegate.
			return nil, false
		}
		routes = append(routes, router.Route{
			Method:      b.method,
			Path:        path,
			HandlerName: handlerName,
			HandlerDecl: decl,
			File:        declFile,
			HandlerObj:  handlerObj,
			Pos:         pos,
		})
	}
	return routes, len(routes) > 0
}

// methodBranches recognizes stmts as exactly one top-level statement — a
// "switch r.Method { ... }" or an "if r.Method == X {...} else if ...
// else {...}" chain, the two idiomatic shapes for hand-rolled method
// dispatch — and extracts one methodBranch per case/condition. Declines
// for anything else: more than one top-level statement (rules out any
// extra guard logic before or after the dispatch, e.g. a leading
// "if strings.HasSuffix(r.URL.Path, ...)" path check), or a shape
// switchMethodBranches/ifMethodBranches themselves decline.
func methodBranches(stmts []ast.Stmt, info *types.Info) ([]methodBranch, bool) {
	if len(stmts) != 1 {
		return nil, false
	}
	switch s := stmts[0].(type) {
	case *ast.SwitchStmt:
		return switchMethodBranches(s, info)
	case *ast.IfStmt:
		return ifMethodBranches(s, info)
	}
	return nil, false
}

// switchMethodBranches recognizes s as "switch r.Method { case
// http.MethodX: delegate(w, r) ... }". Requires no init statement and a
// tag that's an r.Method selector (see isRequestMethodSelector). Each
// non-default case (a nil cc.List marks "default:", which — like any
// other branch that isn't itself a method match, e.g. a typical
// "methodNotAllowed(w)" — is simply not a branch, not an error) must
// have exactly one value (a "case A, B:" with multiple values declines
// the whole switch, not just that clause) evaluating to a real HTTP
// method, and a body of exactly one statement — an empty case body
// fails this the same as a multi-statement one.
func switchMethodBranches(s *ast.SwitchStmt, info *types.Info) ([]methodBranch, bool) {
	if s.Init != nil || !isRequestMethodSelector(s.Tag, info) {
		return nil, false
	}
	var branches []methodBranch
	for _, clause := range s.Body.List {
		cc, ok := clause.(*ast.CaseClause)
		if !ok || cc.List == nil { // nil List: "default:"
			continue
		}
		if len(cc.List) != 1 {
			return nil, false
		}
		method, ok := constStringArg(cc.List[0], info)
		if !ok || !isHTTPMethod(method) {
			return nil, false
		}
		if method == http.MethodConnect {
			continue // no OpenAPI Path Item slot for it -- see allMethods
		}
		call, ok := singleCallBody(cc.Body)
		if !ok {
			return nil, false
		}
		branches = append(branches, methodBranch{method: method, call: call})
	}
	return branches, len(branches) > 0
}

// ifMethodBranches recognizes s as "if r.Method == http.MethodX {
// delegate(w, r) } else if ... else { ... }". Each condition must be a
// plain r.Method equality check (see methodEqualityCond) and each body
// exactly one statement. A trailing plain "else { ... }" (typically
// "methodNotAllowed(w)") ends the chain successfully without being
// treated as a branch — the same "not a branch, ignored" treatment
// switchMethodBranches gives "default:". Anything else (a condition
// involving more than r.Method, an init statement, a multi-statement
// body) declines the whole chain.
func ifMethodBranches(s *ast.IfStmt, info *types.Info) ([]methodBranch, bool) {
	var branches []methodBranch
	for {
		if s.Init != nil {
			return nil, false
		}
		method, ok := methodEqualityCond(s.Cond, info)
		if !ok {
			return nil, false
		}
		call, ok := singleCallBody(s.Body.List)
		if !ok {
			return nil, false
		}
		if method != http.MethodConnect { // no OpenAPI Path Item slot for it -- see allMethods
			branches = append(branches, methodBranch{method: method, call: call})
		}

		switch e := s.Else.(type) {
		case nil:
			return branches, len(branches) > 0
		case *ast.IfStmt:
			s = e
		case *ast.BlockStmt:
			return branches, len(branches) > 0 // trailing "else {...}" ends the chain, not a branch
		default:
			return nil, false
		}
	}
}

// methodEqualityCond recognizes cond as "r.Method == http.MethodX" (in
// either operand order) and returns the method.
func methodEqualityCond(cond ast.Expr, info *types.Info) (string, bool) {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.EQL {
		return "", false
	}
	var methodExpr ast.Expr
	switch {
	case isRequestMethodSelector(bin.X, info):
		methodExpr = bin.Y
	case isRequestMethodSelector(bin.Y, info):
		methodExpr = bin.X
	default:
		return "", false
	}
	method, ok := constStringArg(methodExpr, info)
	if !ok || !isHTTPMethod(method) {
		return "", false
	}
	return method, true
}

// isRequestMethodSelector reports whether e is a ".Method" selector on
// something whose static type is *net/http.Request — checked by type,
// the same idiom isHTTPResponseWriter already uses, not by object
// identity, so it doesn't matter which *http.Request variable is in
// scope (a handler only ever has one in practice).
func isRequestMethodSelector(e ast.Expr, info *types.Info) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Method" {
		return false
	}
	t := info.TypeOf(sel.X)
	if t == nil {
		return false
	}
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "net/http" && obj.Name() == "Request"
}

// singleCallBody reports whether stmts is exactly one statement — an
// expression statement wrapping a 2-argument call, the "delegate(w, r)"
// shape every recognized branch body must have.
func singleCallBody(stmts []ast.Stmt) (*ast.CallExpr, bool) {
	if len(stmts) != 1 {
		return nil, false
	}
	exprStmt, ok := stmts[0].(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	call, ok := exprStmt.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 2 {
		return nil, false
	}
	return call, true
}

// constStringArg evaluates e as a compile-time string constant (a
// literal like "GET" or a named constant like http.MethodGet), the
// string-typed sibling of internal/inference/body.go's constIntArg.
func constStringArg(e ast.Expr, info *types.Info) (string, bool) {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}
