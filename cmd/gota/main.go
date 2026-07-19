// Command gota generates an OpenAPI 3.1 specification from Go source code
// by statically analyzing route registrations and merging them with
// "gota:" comment annotations.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pabloos/gota/internal/emitter"
	"github.com/pabloos/gota/internal/generate"
	"github.com/pabloos/gota/internal/inference"
	"github.com/pabloos/gota/internal/router/nethttp"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gota:", err)
		os.Exit(1)
	}
}

// config holds gota's resolved CLI parameters, ready for run to act on.
type config struct {
	Dir     string // absolute path
	Out     string
	Title   string
	Version string
	Format  emitter.Format
}

// parseArgs parses and validates gota's CLI flags. It contains all of the
// CLI-parameter handling (flags, defaults, --dir resolution, output format
// detection) in isolation from running the actual pipeline, so the two can
// be reasoned about and tested independently.
func parseArgs(args []string) (*config, error) {
	fs := flag.NewFlagSet("gota", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory of the Go project to analyze")
	out := fs.String("out", "openapi.yaml", "output file (.yaml/.yml or .json)")
	title := fs.String("title", "", "API title (defaults to the directory name)")
	version := fs.String("api-version", "0.1.0", "API version")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return nil, fmt.Errorf("resolving --dir: %w", err)
	}

	docTitle := *title
	if docTitle == "" {
		docTitle = filepath.Base(absDir)
	}

	return &config{
		Dir:     absDir,
		Out:     *out,
		Title:   docTitle,
		Version: *version,
		Format:  formatFromPath(*out),
	}, nil
}

func formatFromPath(path string) emitter.Format {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return emitter.JSON
	}
	return emitter.YAML
}

func run(args []string) error {
	cfg, err := parseArgs(args)
	if err != nil {
		return err
	}

	doc, err := generate.Run(generate.Options{
		Dir:     cfg.Dir,
		Title:   cfg.Title,
		Version: cfg.Version,
		Routers: []generate.Router{{Plugin: nethttp.New(), Dialect: inference.NetHTTP()}},
	})
	if err != nil {
		return err
	}

	if err := emitter.Validate(doc); err != nil {
		return err
	}

	data, err := emitter.Marshal(doc, cfg.Format)
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfg.Out, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", cfg.Out, err)
	}

	fmt.Printf("gota: wrote %s (%d path%s)\n", cfg.Out, len(doc.Paths), plural(len(doc.Paths)))
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
