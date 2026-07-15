package nethttp_test

import (
	"net/http"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/internal/parser"
	"github.com/pabloos/gota/internal/router"
	"github.com/pabloos/gota/internal/router/nethttp"
)

// loadFixture loads the fixture package checked in at
// internal/router/nethttp/testdata/<name>, with the same parser.Load path
// the real pipeline uses.
func loadFixture(t *testing.T, name string) []*packages.Package {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := parser.Load(dir)
	if err != nil {
		t.Fatalf("parser.Load: %v", err)
	}
	return pkgs
}

func TestExtract(t *testing.T) {
	pkgs := loadFixture(t, "routes")
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}

	plugin := nethttp.New()
	routes, err := plugin.Extract(pkgs[0])
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	got := map[string]router.Route{}
	for _, r := range routes {
		got[r.Method+" "+r.Path] = r
	}

	want := []string{
		http.MethodGet + " /users/{id}",
		http.MethodGet + " /legacy", // no method in pattern -> defaults to GET
		http.MethodPost + " /users",
		http.MethodGet + " /health", // http.HandleFunc (package-level call)
		http.MethodGet + " /hosted", // host-prefixed pattern, host stripped
	}
	if len(got) != len(want) {
		t.Fatalf("got %d routes, want %d: %+v", len(got), len(want), got)
	}
	for _, key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("missing route %q, got: %+v", key, got)
		}
	}

	getUser, ok := got[http.MethodGet+" /users/{id}"]
	if !ok {
		t.Fatalf("missing GET /users/{id}")
	}
	if getUser.HandlerName != "GetUser" {
		t.Errorf("HandlerName = %q, want GetUser", getUser.HandlerName)
	}
	if getUser.HandlerDecl == nil {
		t.Errorf("HandlerDecl is nil, want resolved FuncDecl")
	}
	if getUser.File == nil {
		t.Errorf("File is nil, want the file containing GetUser's declaration")
	}

	createUser, ok := got[http.MethodPost+" /users"]
	if !ok {
		t.Fatalf("missing POST /users")
	}
	if createUser.HandlerName != "CreateUser" {
		t.Errorf("HandlerName = %q, want CreateUser", createUser.HandlerName)
	}
}

func TestName(t *testing.T) {
	if nethttp.New().Name() != "net/http" {
		t.Errorf("Name() = %q, want net/http", nethttp.New().Name())
	}
}

// TestExtract_MethodValueHandler exercises the common dependency-injection
// pattern where handlers are methods on a server struct, bound as method
// values (srv.GetItem) rather than referenced as bare package-level
// functions. Fixture: testdata/routes_method_value.
func TestExtract_MethodValueHandler(t *testing.T) {
	pkgs := loadFixture(t, "routes_method_value")

	routes, err := nethttp.New().Extract(pkgs[0])
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1: %+v", len(routes), routes)
	}
	r := routes[0]
	if r.Method != http.MethodGet || r.Path != "/items/{id}" {
		t.Fatalf("route = %+v", r)
	}
	if r.HandlerName != "GetItem" {
		t.Errorf("HandlerName = %q, want GetItem", r.HandlerName)
	}
	if r.HandlerDecl == nil {
		t.Fatalf("HandlerDecl is nil: a method value handler must still resolve back to its *ast.FuncDecl so its \"gota:\" comment can be extracted")
	}
	if r.HandlerDecl.Recv == nil {
		t.Errorf("resolved decl has no receiver; resolved the wrong FuncDecl")
	}
	if r.HandlerDecl.Doc == nil {
		t.Errorf("resolved decl has no doc comment, want the \"gota:\" block above (s *Server) GetItem")
	}
	if r.File == nil {
		t.Errorf("File is nil, want the file containing (s *Server) GetItem")
	}
}
