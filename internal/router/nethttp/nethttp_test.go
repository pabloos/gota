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

// allMethodsForTest mirrors nethttp's unexported allMethods: the 8 HTTP
// methods a method-less ServeMux pattern expands into. CONNECT is
// deliberately excluded — see allMethods' doc comment in nethttp.go.
var allMethodsForTest = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodHead, http.MethodOptions, http.MethodTrace,
}

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

	// /users/{id} and /users declare an explicit method, so they produce
	// exactly one route each. /legacy, /health and /hosted have no method
	// in their pattern, which net/http's ServeMux docs say "matches every
	// method" — each expands into 8 routes (TestExtract_NoMethodPatternMatchesAllMethods
	// checks that expansion in detail; here just the totals and the
	// explicit-method routes matter).
	wantTotal := 2 + 3*len(allMethodsForTest)
	if len(got) != wantTotal {
		t.Fatalf("got %d routes, want %d: %+v", len(got), wantTotal, got)
	}
	for _, key := range []string{
		http.MethodGet + " /users/{id}",
		http.MethodPost + " /users",
	} {
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

// TestExtract_NoMethodPatternMatchesAllMethods pins down net/http's own
// documented ServeMux behavior: "a pattern with no method matches every
// method." http.HandleFunc("/health", HealthCheck) in the fixture must
// expand into exactly the 8 methods gota's model supports, all bound to
// HealthCheck — not narrowed to GET.
func TestExtract_NoMethodPatternMatchesAllMethods(t *testing.T) {
	pkgs := loadFixture(t, "routes")

	routes, err := nethttp.New().Extract(pkgs[0])
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	gotMethods := map[string]bool{}
	for _, r := range routes {
		if r.Path != "/health" {
			continue
		}
		if r.HandlerName != "HealthCheck" {
			t.Errorf("route %+v: HandlerName = %q, want HealthCheck", r, r.HandlerName)
		}
		gotMethods[r.Method] = true
	}

	if len(gotMethods) != len(allMethodsForTest) {
		t.Fatalf("got %d methods for /health, want %d: %v", len(gotMethods), len(allMethodsForTest), gotMethods)
	}
	for _, m := range allMethodsForTest {
		if !gotMethods[m] {
			t.Errorf("missing method %s for /health", m)
		}
	}
	if gotMethods[http.MethodConnect] {
		t.Errorf("CONNECT should never be generated: OpenAPI's Path Item Object has no slot for it")
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
