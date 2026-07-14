package main

import (
	"path/filepath"
	"testing"

	"github.com/pabloos/gota/internal/emitter"
)

func TestParseArgs_Defaults(t *testing.T) {
	cfg, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if cfg.Out != "openapi.yaml" {
		t.Errorf("Out = %q, want openapi.yaml", cfg.Out)
	}
	if cfg.Version != "0.1.0" {
		t.Errorf("Version = %q, want 0.1.0", cfg.Version)
	}
	if cfg.Format != emitter.YAML {
		t.Errorf("Format = %v, want YAML", cfg.Format)
	}
	wantDir, _ := filepath.Abs(".")
	if cfg.Dir != wantDir {
		t.Errorf("Dir = %q, want %q", cfg.Dir, wantDir)
	}
	if cfg.Title != filepath.Base(wantDir) {
		t.Errorf("Title = %q, want the current directory's base name %q", cfg.Title, filepath.Base(wantDir))
	}
}

func TestParseArgs_DirIsResolvedToAbsolute(t *testing.T) {
	cfg, err := parseArgs([]string{"--dir", "."})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !filepath.IsAbs(cfg.Dir) {
		t.Errorf("Dir = %q, want an absolute path", cfg.Dir)
	}
}

func TestParseArgs_ExplicitTitleWins(t *testing.T) {
	cfg, err := parseArgs([]string{"--dir", "testdata", "--title", "My API"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if cfg.Title != "My API" {
		t.Errorf("Title = %q, want %q", cfg.Title, "My API")
	}
}

func TestParseArgs_FormatFromOutExtension(t *testing.T) {
	cases := []struct {
		out  string
		want emitter.Format
	}{
		{"openapi.yaml", emitter.YAML},
		{"openapi.yml", emitter.YAML},
		{"openapi.json", emitter.JSON},
		{"openapi.JSON", emitter.JSON},
		{"openapi", emitter.YAML},
	}
	for _, c := range cases {
		cfg, err := parseArgs([]string{"--out", c.out})
		if err != nil {
			t.Fatalf("parseArgs(--out %s): %v", c.out, err)
		}
		if cfg.Format != c.want {
			t.Errorf("--out %s: Format = %v, want %v", c.out, cfg.Format, c.want)
		}
	}
}

func TestParseArgs_UnknownFlagErrors(t *testing.T) {
	if _, err := parseArgs([]string{"--not-a-flag"}); err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
}
