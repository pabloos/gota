// Package parser loads a Go module's packages with full AST and type
// information, ready for router plugins and the inference stage to walk.
package parser

import (
	"fmt"

	"golang.org/x/tools/go/packages"
)

// loadMode requests everything router plugins and type inference need:
// syntax trees, type-checked info, and import resolution.
const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo

// Load type-checks every package under dir (recursively, "./...") and
// returns them. It returns an error if any package failed to load or had
// type errors, since gota's inference relies on accurate type information.
func Load(dir string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode: loadMode,
		Dir:  dir,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("parser: loading packages in %s: %w", dir, err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("parser: no Go packages found in %s", dir)
	}

	var errs []error
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		for _, e := range pkg.Errors {
			errs = append(errs, fmt.Errorf("%s: %w", pkg.PkgPath, e))
		}
	})
	if len(errs) > 0 {
		return nil, fmt.Errorf("parser: %d error(s) loading %s, first: %w", len(errs), dir, errs[0])
	}

	return pkgs, nil
}
