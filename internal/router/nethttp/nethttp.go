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
	"go/token"
	"go/types"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

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

func (p *Plugin) Extract(pkg *packages.Package) ([]router.Route, error) {
	if pkg.Fset == nil {
		return nil, fmt.Errorf("nethttp: package %s has no Fset", pkg.PkgPath)
	}

	funcDecls := indexFuncDecls(pkg)

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
			handlerName, decl, declFile, ok := resolveHandler(pkg, call.Args[1], funcDecls)
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

// resolvedDecl pairs a function/method declaration with the file that
// contains it, since callers need both (the file to build an
// ast.CommentMap over, for instance).
type resolvedDecl struct {
	Decl *ast.FuncDecl
	File *ast.File
}

// indexFuncDecls maps every function and method declared in pkg to its
// *types.Object, so handlers can be resolved back to their doc comments
// whether they're referenced as a bare function (GetUser), a method value
// on a receiver (srv.GetUser), or an http.HandlerFunc conversion of either.
func indexFuncDecls(pkg *packages.Package) map[types.Object]resolvedDecl {
	decls := make(map[types.Object]resolvedDecl)
	if pkg.TypesInfo == nil {
		return decls
	}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if obj := pkg.TypesInfo.Defs[fn.Name]; obj != nil {
				decls[obj] = resolvedDecl{Decl: fn, File: file}
			}
		}
	}
	return decls
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

// resolveHandler extracts the handler name from a bare identifier
// (HandleFunc(pattern, GetUser)), a method value on a receiver
// (HandleFunc(pattern, srv.GetUser)), a qualified identifier from another
// package (HandleFunc(pattern, handlers.GetUser)), or an http.HandlerFunc
// conversion of any of those. The declaration (and its file) is resolved
// via go/types object identity rather than name matching, so it works for
// methods too; both come back nil when the object lives outside pkg (e.g.
// a handler defined in a different package), since cross-package
// resolution isn't supported yet.
func resolveHandler(pkg *packages.Package, e ast.Expr, decls map[types.Object]resolvedDecl) (name string, decl *ast.FuncDecl, file *ast.File, ok bool) {
	switch expr := e.(type) {
	case *ast.Ident:
		rd := decls[pkg.TypesInfo.Uses[expr]]
		return expr.Name, rd.Decl, rd.File, true
	case *ast.CallExpr:
		if len(expr.Args) != 1 {
			return "", nil, nil, false
		}
		return resolveHandler(pkg, expr.Args[0], decls)
	case *ast.SelectorExpr:
		var obj types.Object
		if selection, ok := pkg.TypesInfo.Selections[expr]; ok {
			obj = selection.Obj() // method value, e.g. srv.GetUser
		} else {
			obj = pkg.TypesInfo.Uses[expr.Sel] // qualified identifier, e.g. handlers.GetUser
		}
		rd := decls[obj]
		return expr.Sel.Name, rd.Decl, rd.File, true
	}
	return "", nil, nil, false
}
