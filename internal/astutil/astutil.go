// Package astutil provides small go/ast + go/types helpers shared across
// gota's router plugins and orchestration layer.
package astutil

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// FuncDeclInfo pairs a function/method declaration with its file and the
// *types.Info of the package that type-checked it. Info matters because
// go/types lookups inside that declaration's body (TypeOf, Uses, ...) are
// only valid against the Info from the same type-checking pass that
// produced them — passing the wrong package's Info silently degrades
// inference instead of erroring.
type FuncDeclInfo struct {
	Decl *ast.FuncDecl
	File *ast.File
	Info *types.Info
}

// IndexFuncDecls maps every function/method declared across pkgs to its
// types.Object, so an object resolved in one package (e.g. a cross-package
// reference like handlers.GetUser) can be traced back to its declaration
// wherever it lives. pkgs must have been loaded together (one
// packages.Load call) so object identity is shared across them —
// parser.Load already guarantees this.
//
// This only covers packages within the analyzed module: parser.Load loads
// "./..." from the target directory, which never includes stdlib or
// third-party dependencies as entries in pkgs (their type information is
// available for type-checking, but not their syntax trees). A handler
// imported from an external module resolves a types.Object fine but has
// no entry here, so it's left unresolved — the same "route exists, no
// comment or body inference" degradation as any other unresolved handler,
// not an error. Analyzing arbitrary dependency source is a materially
// different, much larger feature, not attempted here.
func IndexFuncDecls(pkgs []*packages.Package) map[types.Object]FuncDeclInfo {
	decls := make(map[types.Object]FuncDeclInfo)
	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if obj := pkg.TypesInfo.Defs[fn.Name]; obj != nil {
					decls[obj] = FuncDeclInfo{Decl: fn, File: file, Info: pkg.TypesInfo}
				}
			}
		}
	}
	return decls
}
