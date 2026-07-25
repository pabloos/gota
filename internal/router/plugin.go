// Package router defines the plugin interface framework adapters implement
// to tell gota which HTTP routes exist in a Go program. The core never
// imports a specific framework; each plugin (e.g. internal/router/nethttp)
// is a self-contained adapter that statically analyzes the user's code.
package router

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// Route is one HTTP route discovered by a plugin: an HTTP method + path
// template bound to the handler function that serves it.
type Route struct {
	Method      string
	Path        string
	HandlerName string
	HandlerDecl *ast.FuncDecl // nil if Extract's own package didn't contain the declaration (e.g. a cross-package reference); internal/generate backfills this using HandlerObj when possible
	File        *ast.File     // the file containing HandlerDecl; nil whenever HandlerDecl is nil
	HandlerObj  types.Object  // the resolved go/types object for the handler, whenever expression-shape resolution succeeded at all — independent of whether HandlerDecl was also found; nil only if the plugin couldn't identify an object (e.g. an unrecognized expression shape)
	HandlerLit  *ast.FuncLit  // set when the handler is an inline function literal (e.g. r.GET("/x", func(c *gin.Context){...})); HandlerName is then "" and HandlerDecl/HandlerObj nil, and internal/generate infers the body straight from the literal and synthesizes an operationId from the method+path
	Pos         token.Position
}

// Plugin extracts routes from a type-checked package. Implementations must
// not depend on gota's other internal packages; they only see the AST and
// type information x/tools/go/packages provides.
type Plugin interface {
	// Name identifies the plugin, e.g. "net/http".
	Name() string
	// Extract returns every route the plugin recognizes in pkg.
	Extract(pkg *packages.Package) ([]Route, error)
}
