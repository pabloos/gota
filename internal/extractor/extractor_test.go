package extractor_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/pabloos/gota/internal/extractor"
)

// docOf parses src (a single function declaration with a doc comment) and
// returns its *ast.CommentGroup.
func docOf(t *testing.T, src string) *ast.CommentGroup {
	t.Helper()
	fset := token.NewFileSet()
	full := "package p\n" + src
	f, err := parser.ParseFile(fset, "test.go", full, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return fn.Doc
		}
	}
	t.Fatalf("no func decl found in fixture")
	return nil
}

func TestExtract_FullBlock(t *testing.T) {
	doc := docOf(t, `
// gota:
//   summary: Get a user by ID
//   parameters:
//     - name: id
//       in: path
//       required: true
//       schema:
//         type: integer
//   responses:
//     '200':
//       description: OK
func GetUser() {}
`)
	op, found, err := extractor.Extract(doc)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !found {
		t.Fatalf("expected a gota block to be found")
	}
	if op.Summary != "Get a user by ID" {
		t.Errorf("Summary = %q, want %q", op.Summary, "Get a user by ID")
	}
	if len(op.Parameters) != 1 || op.Parameters[0].Name != "id" {
		t.Fatalf("Parameters = %+v", op.Parameters)
	}
	if op.Parameters[0].Schema == nil || op.Parameters[0].Schema.Type != "integer" {
		t.Errorf("Parameters[0].Schema = %+v", op.Parameters[0].Schema)
	}
	resp, ok := op.Responses["200"]
	if !ok || resp.Description != "OK" {
		t.Errorf("Responses[200] = %+v, ok=%v", resp, ok)
	}
}

// TestExtract_GofmtStyle mirrors what `gofmt` actually produces for a
// "gota:" doc comment: gofmt's doc-comment reformatter (Go 1.19+) treats
// the indented YAML as a preformatted block, inserting a blank comment
// line after "gota:" and re-indenting the block with a single tab. Since
// virtually all real-world Go source is gofmt'd, the extractor must handle
// this shape, not just the hand-written one.
func TestExtract_GofmtStyle(t *testing.T) {
	doc := docOf(t, "// gota:\n"+
		"//\n"+
		"//\tsummary: Get a user by ID\n"+
		"//\tparameters:\n"+
		"//\t  - name: id\n"+
		"//\t    in: path\n"+
		"//\t    required: true\n"+
		"//\t    schema:\n"+
		"//\t      type: integer\n"+
		"func GetUser() {}\n")
	op, found, err := extractor.Extract(doc)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !found {
		t.Fatalf("expected a gota block to be found")
	}
	if op.Summary != "Get a user by ID" {
		t.Errorf("Summary = %q, want %q", op.Summary, "Get a user by ID")
	}
	if len(op.Parameters) != 1 || op.Parameters[0].Name != "id" {
		t.Fatalf("Parameters = %+v", op.Parameters)
	}
	if op.Parameters[0].Schema == nil || op.Parameters[0].Schema.Type != "integer" {
		t.Errorf("Parameters[0].Schema = %+v", op.Parameters[0].Schema)
	}
}

func TestExtract_NoBlock(t *testing.T) {
	doc := docOf(t, `
// ListUsers returns every user. Just a normal doc comment, no gota block.
func ListUsers() {}
`)
	op, found, err := extractor.Extract(doc)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if found {
		t.Fatalf("expected no gota block, got %+v", op)
	}
}

func TestExtract_NilDoc(t *testing.T) {
	op, found, err := extractor.Extract(nil)
	if err != nil || found || op != nil {
		t.Fatalf("Extract(nil) = %+v, %v, %v, want nil, false, nil", op, found, err)
	}
}

func TestExtract_PrecedingProseThenBlock(t *testing.T) {
	doc := docOf(t, `
// CreateUser creates a new user.
//
// gota:
//   summary: Create user
func CreateUser() {}
`)
	op, found, err := extractor.Extract(doc)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !found {
		t.Fatalf("expected a gota block to be found")
	}
	if op.Summary != "Create user" {
		t.Errorf("Summary = %q, want %q", op.Summary, "Create user")
	}
}

func TestExtract_InvalidYAML(t *testing.T) {
	doc := docOf(t, `
// gota:
//   summary: [unterminated
func Broken() {}
`)
	_, _, err := extractor.Extract(doc)
	if err == nil {
		t.Fatalf("expected an error for invalid YAML")
	}
}

func TestExtract_IgnoresDocBlock(t *testing.T) {
	// A "gota:doc:" block is document-level, not a per-handler operation:
	// Extract must skip it rather than mis-parse "doc:" as an inline block.
	doc := docOf(t, `
// gota:doc:
//   info:
//     description: The whole API.
func setup() {}
`)
	op, found, err := extractor.Extract(doc)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if found {
		t.Fatalf("Extract should ignore a gota:doc: block, got %+v", op)
	}
}

func TestExtractDoc_FullBlock(t *testing.T) {
	doc := docOf(t, `
// gota:doc:
//   info:
//     description: A signatures API.
//   security:
//     - BearerAuth: []
//   components:
//     securitySchemes:
//       BearerAuth:
//         type: http
//         scheme: bearer
//         bearerFormat: JWT
func setup() {}
`)
	meta, found, err := extractor.ExtractDoc(doc)
	if err != nil {
		t.Fatalf("ExtractDoc: %v", err)
	}
	if !found {
		t.Fatalf("expected a gota:doc: block to be found")
	}
	if meta.Info == nil || meta.Info.Description != "A signatures API." {
		t.Errorf("Info = %+v", meta.Info)
	}
	if len(meta.Security) != 1 {
		t.Fatalf("Security = %+v", meta.Security)
	}
	if _, ok := meta.Security[0]["BearerAuth"]; !ok {
		t.Errorf("Security[0] = %+v, want a BearerAuth key", meta.Security[0])
	}
	if meta.Components == nil {
		t.Fatalf("Components is nil")
	}
	scheme := meta.Components.SecuritySchemes["BearerAuth"]
	if scheme == nil {
		t.Fatalf("SecuritySchemes = %+v", meta.Components.SecuritySchemes)
	}
	if scheme.Type != "http" || scheme.Scheme != "bearer" || scheme.BearerFormat != "JWT" {
		t.Errorf("BearerAuth scheme = %+v", scheme)
	}
}

func TestExtractDoc_IgnoresOperationBlock(t *testing.T) {
	// A plain "gota:" operation block is not document-level.
	doc := docOf(t, `
// gota:
//   summary: Get a user
func GetUser() {}
`)
	_, found, err := extractor.ExtractDoc(doc)
	if err != nil {
		t.Fatalf("ExtractDoc: %v", err)
	}
	if found {
		t.Fatalf("ExtractDoc should ignore a plain gota: block")
	}
}
