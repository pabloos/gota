// Package parser loads a Go module's packages with full AST and type
// information, ready for router plugins and the inference stage to walk.
package parser

import (
	"fmt"
	"os"
	"path/filepath"

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
		if hint := workspaceRootHint(dir); hint != "" {
			return nil, fmt.Errorf("parser: %s%w", hint, err)
		}
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
		// The "directory prefix . does not contain modules listed in
		// go.work" failure surfaces as a package error, not the top-level
		// load error, so the workspace-root hint is checked here too.
		if hint := workspaceRootHint(dir); hint != "" {
			return nil, fmt.Errorf("parser: %s%w", hint, errs[0])
		}
		return nil, fmt.Errorf("parser: %d error(s) loading %s, first: %w", len(errs), dir, errs[0])
	}

	return pkgs, nil
}

// workspaceRootHint returns an actionable message (ending in ": ") when
// dir is a Go workspace root — it has a go.work but no go.mod of its own,
// so "./..." matches none of the workspace's modules. gota analyzes one
// module at a time; a type declared in another workspace module is still
// resolved as a dependency (see inference.ResolveSchemaRefs), so pointing
// --dir at a specific module is sufficient. Returns "" when dir isn't a
// workspace root, so the caller falls back to the raw load error.
func workspaceRootHint(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "go.work")); err != nil {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return "" // a module that also holds a go.work: "./..." resolves normally
	}
	return fmt.Sprintf("%s is a Go workspace root (it has a go.work but no go.mod); gota analyzes one module at a time — point --dir at one of the modules listed in the go.work \"use\" block (e.g. --dir %s). A response type declared in another workspace module is still resolved as a dependency: ",
		dir, filepath.Join(dir, "<module>"))
}
