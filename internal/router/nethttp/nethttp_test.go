package nethttp_test

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/internal/astutil"
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

	// /users/{id}, /users and the inline POST /inline declare an explicit
	// method, so they produce exactly one route each. /legacy, /health and
	// /hosted have no method in their pattern, which net/http's ServeMux
	// docs say "matches every method" — each expands into 8 routes
	// (TestExtract_NoMethodPatternMatchesAllMethods checks that expansion
	// in detail; here just the totals and the explicit-method routes
	// matter). The method-less inline closure /inline-methodless is
	// declined, contributing nothing.
	wantTotal := 3 + 3*len(allMethodsForTest)
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

	t.Run("an explicit-method inline func literal is carried via HandlerLit", func(t *testing.T) {
		r, ok := got[http.MethodPost+" /inline"]
		if !ok {
			t.Fatalf("missing POST /inline (explicit-method inline handler)")
		}
		if r.HandlerLit == nil {
			t.Error("HandlerLit is nil, want the inline *ast.FuncLit")
		}
		if r.HandlerName != "" || r.HandlerDecl != nil || r.HandlerObj != nil {
			t.Errorf("an anonymous handler must have empty Name/Decl/Obj, got %+v", r)
		}
	})

	t.Run("a method-less inline closure is declined", func(t *testing.T) {
		for key := range got {
			if strings.HasSuffix(key, " /inline-methodless") {
				t.Errorf("routes contain %q, a method-less inline closure must be declined", key)
			}
		}
	})
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

// TestExtract_CrossPackageHandler pins down what a single Extract call
// can and can't do when a route's handler lives in a different package
// than the one being analyzed: HandlerDecl/File stay nil (Extract only
// ever sees one package), but HandlerObj must still resolve — that's
// what lets internal/generate trace the declaration across every loaded
// package afterward. Covers all three shapes resolveHandler recognizes:
// a bare qualified identifier, an http.HandlerFunc conversion of one,
// and a cross-package method value.
func TestExtract_CrossPackageHandler(t *testing.T) {
	pkgs := loadFixture(t, "routes_cross_package")
	if len(pkgs) != 2 {
		t.Fatalf("fixture setup: got %d packages, want 2 (main and handlers)", len(pkgs))
	}

	var mainPkg *packages.Package
	for _, pkg := range pkgs {
		if pkg.Name == "main" {
			mainPkg = pkg
		}
	}
	if mainPkg == nil {
		t.Fatalf("fixture setup: no package named main among: %+v", pkgs)
	}

	routes, err := nethttp.New().Extract(mainPkg)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(routes) != 3 {
		t.Fatalf("got %d routes, want 3: %+v", len(routes), routes)
	}

	// The global index (spanning both packages) is what internal/generate
	// uses to trace HandlerObj back to its declaration; reusing it here
	// proves the identity resolveHandler captured is actually traceable,
	// not just non-nil.
	globalIndex := astutil.IndexFuncDecls(pkgs)

	wantHandlerName := map[string]string{
		"/users/{id}": "GetUser",
		"/users":      "CreateUser",
		"/items/{id}": "GetItem",
	}
	for _, r := range routes {
		if r.HandlerDecl != nil {
			t.Errorf("route %s %s: HandlerDecl = %+v, want nil (Extract only sees the main package)", r.Method, r.Path, r.HandlerDecl)
		}
		if r.File != nil {
			t.Errorf("route %s %s: File = %+v, want nil (Extract only sees the main package)", r.Method, r.Path, r.File)
		}
		if r.HandlerObj == nil {
			t.Fatalf("route %s %s: HandlerObj is nil, want the resolved go/types object even though the decl lives elsewhere", r.Method, r.Path)
		}

		wantName, ok := wantHandlerName[r.Path]
		if !ok {
			t.Fatalf("unexpected route path %q", r.Path)
		}
		if r.HandlerName != wantName {
			t.Errorf("route %s %s: HandlerName = %q, want %q", r.Method, r.Path, r.HandlerName, wantName)
		}

		fd, ok := globalIndex[r.HandlerObj]
		if !ok {
			t.Fatalf("route %s %s: HandlerObj not found in the global index built from the same pkgs — go/types identity isn't shared across packages as expected", r.Method, r.Path)
		}
		if fd.Decl.Name.Name != wantName {
			t.Errorf("route %s %s: global index resolved to %q, want %q", r.Method, r.Path, fd.Decl.Name.Name, wantName)
		}
		if fd.File == nil || fd.Info == nil {
			t.Errorf("route %s %s: resolved FuncDeclInfo = %+v, want non-nil File and Info", r.Method, r.Path, fd)
		}

		// GetItem specifically must resolve to a method (a receiver-bound
		// FuncDecl), not some unrelated function that happens to share a name.
		if r.Path == "/items/{id}" && fd.Decl.Recv == nil {
			t.Errorf("GetItem's resolved decl has no receiver; resolved the wrong FuncDecl")
		}
	}
}

// TestExtract_MethodDispatchFuncLit pins down the hand-rolled
// method-dispatcher idiom found in a real, representative Go HTTP API:
// a method-less pattern whose handler is an anonymous function that
// itself switches or branches on r.Method, wrapped in a middleware call
// and an http.HandlerFunc conversion. Fixture: testdata/routes_method_dispatch.
func TestExtract_MethodDispatchFuncLit(t *testing.T) {
	pkgs := loadFixture(t, "routes_method_dispatch")

	routes, err := nethttp.New().Extract(pkgs[0])
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	got := map[string]router.Route{}
	for _, r := range routes {
		got[r.Method+" "+r.Path] = r
	}

	// Every one of these path prefixes is deliberately too complex, or
	// outright unrecognizable, to the narrow dispatch-detection this
	// plugin does -- each must produce zero routes, not a partial or
	// wrong guess. See the fixture's own comments for exactly which
	// shape each one declines on.
	declinedPrefixes := []string{
		"/products/",             // a non-method guard runs before the dispatch
		"/multi-case",            // "case A, B:" -- a clause with multiple values
		"/non-http-method",       // a case value that isn't a real HTTP method
		"/multi-stmt-case",       // a case body with more than one statement
		"/dynamic-case",          // a case value that's a variable, not a constant
		"/if-init",               // an if statement with an init statement
		"/if-non-method",         // a condition unrelated to r.Method
		"/if-multi-stmt",         // an if body with more than one statement
		"/wrong-selector",        // a comparison against a non-"Method" selector
		"/non-request-method",    // a ".Method" selector on a non-*http.Request type
		"/wrong-arity",           // a branch body calling a 1-argument delegate
		"/not-a-call",            // a branch body that isn't a call at all
		"/not-dispatch-shaped",   // a single statement that's neither switch nor if
		"/not-equal",             // a "!=" comparison, not "=="
		"/if-non-http-method",    // an if comparing r.Method to a non-HTTP-method string
		"/unresolvable-delegate", // a delegate call that isn't a resolvable ident/selector
		"/not-a-real-route",      // a custom type's own HandleFunc, not a ServeMux
	}
	for key := range got {
		for _, prefix := range declinedPrefixes {
			if strings.Contains(key, prefix) {
				t.Errorf("route %q: %s is deliberately too complex/unrecognizable, want no routes from it at all", key, prefix)
			}
		}
	}

	wantRoutes := map[string]string{
		"POST /products":         "CreateProduct",
		"GET /products":          "ListProducts",
		"GET /warehouses/":       "GetWarehouse",
		"GET /connect-case":      "ConnectCaseSibling",
		"GET /if-connect":        "IfConnectSibling",
		"GET /reversed-operands": "ReversedOperandsDelegate", // r.Method as the right operand
		"GET /if-no-else":        "IfNoElseDelegate",         // a bare "if" with no else
	}
	if len(got) != len(wantRoutes) {
		t.Fatalf("got %d routes, want %d: %+v", len(got), len(wantRoutes), got)
	}
	for key, wantHandler := range wantRoutes {
		r, ok := got[key]
		if !ok {
			t.Fatalf("missing route %q, got: %+v", key, got)
		}
		if r.HandlerName != wantHandler {
			t.Errorf("route %q: HandlerName = %q, want %q (the real delegate, not the anonymous closure)", key, r.HandlerName, wantHandler)
		}
		if r.HandlerDecl == nil {
			t.Errorf("route %q: HandlerDecl is nil, want the delegate's own *ast.FuncDecl (so its \"gota:\" comment can be extracted)", key)
		}
	}

	t.Run("a CONNECT branch is skipped, not extracted, in either dispatch shape", func(t *testing.T) {
		for key := range got {
			if strings.HasPrefix(key, "CONNECT ") {
				t.Errorf("routes = %+v, CONNECT should never be emitted (no OpenAPI Path Item slot)", got)
			}
		}
	})
}
