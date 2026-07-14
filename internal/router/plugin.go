// Package router defines the plugin interface framework adapters implement
// to tell gota which HTTP routes exist in a Go program. The core never
// imports a specific framework; each plugin (e.g. internal/router/nethttp)
// is a self-contained adapter that statically analyzes the user's code.
package router

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/packages"
)

// Route is one HTTP route discovered by a plugin: an HTTP method + path
// template bound to the handler function that serves it.
type Route struct {
	Method      string
	Path        string
	HandlerName string
	HandlerDecl *ast.FuncDecl // nil if the handler declaration could not be resolved (e.g. it lives in another package)
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
