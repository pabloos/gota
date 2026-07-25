package chi_test

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/internal/parser"
	"github.com/pabloos/gota/internal/router"
	"github.com/pabloos/gota/internal/router/chi"
)

// loadFixture loads the fixture package checked in at
// internal/router/chi/testdata/<name> — a SEPARATE Go module (its own
// go.mod requiring the real github.com/go-chi/chi/v5) so that chi
// never becomes a dependency of gota's own root module, per this
// plugin's own package doc comment. parser.Load's underlying
// packages.Load resolves this transparently: Dir pointing inside a
// nested module makes that module the load root, the same mechanism
// scripts/sandbox/run.sh already relies on for external repos.
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

func TestName(t *testing.T) {
	if got := chi.New().Name(); got != "chi" {
		t.Errorf("Name() = %q, want %q", got, "chi")
	}
}

func TestExtract(t *testing.T) {
	pkgs := loadFixture(t, "routes")
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}

	plugin := chi.New()
	routes, err := plugin.Extract(pkgs[0])
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	got := map[string]router.Route{}
	for _, r := range routes {
		got[r.Method+" "+r.Path] = r
	}

	t.Run("method-specific calls resolve to their own HTTP method", func(t *testing.T) {
		r, ok := got["GET /users/{id}"]
		if !ok {
			t.Fatalf("routes = %+v, missing GET /users/{id}", got)
		}
		if r.HandlerName != "GetUser" || r.HandlerDecl == nil {
			t.Errorf("route = %+v, want HandlerName GetUser with a resolved decl", r)
		}
		if _, ok := got["POST /users"]; !ok {
			t.Errorf("routes = %+v, missing POST /users", got)
		}
	})

	t.Run("Handle with no space registers all methods", func(t *testing.T) {
		for _, m := range allMethodsForTest {
			if _, ok := got[m+" /health"]; !ok {
				t.Errorf("routes = %+v, missing %s /health", got, m)
			}
		}
		if _, ok := got["CONNECT /health"]; ok {
			t.Errorf("routes = %+v, should not include CONNECT (no OpenAPI Path Item slot)", got)
		}
	})

	t.Run("Handle with a \"METHOD \" prefix registers only that method", func(t *testing.T) {
		if _, ok := got["GET /legacy"]; !ok {
			t.Errorf("routes = %+v, missing GET /legacy", got)
		}
		for _, m := range allMethodsForTest {
			if m == "GET" {
				continue
			}
			if _, ok := got[m+" /legacy"]; ok {
				t.Errorf("routes = %+v, /legacy should be GET-only (chi.Handle re-parses a spaced pattern)", got)
			}
		}
	})

	t.Run("Method resolves an explicit constant method string", func(t *testing.T) {
		if _, ok := got["GET /method-get"]; !ok {
			t.Errorf("routes = %+v, missing GET /method-get", got)
		}
	})

	t.Run("Connect and Query are recognized but never emitted", func(t *testing.T) {
		for path, r := range got {
			if r.Method == "CONNECT" || r.Method == "QUERY" {
				t.Errorf("routes contains %s %s, Connect/Query must never be emitted (no OpenAPI Path Item slot)", r.Method, path)
			}
		}
	})

	t.Run("With(...) chaining is recognized like a direct call", func(t *testing.T) {
		if _, ok := got["GET /with-middleware"]; !ok {
			t.Errorf("routes = %+v, missing GET /with-middleware", got)
		}
	})

	t.Run("a regex-constrained param degrades to a bare param", func(t *testing.T) {
		if _, ok := got["GET /items/{id}"]; !ok {
			t.Errorf("routes = %+v, missing GET /items/{id} (regex constraint should be stripped)", got)
		}
	})

	t.Run("a method-value handler resolves through the receiver", func(t *testing.T) {
		r, ok := got["GET /items/{id}"]
		if !ok || r.HandlerName != "GetItem" {
			t.Errorf("route = %+v, want HandlerName GetItem", r)
		}
	})

	t.Run("nested Route accumulates the path prefix", func(t *testing.T) {
		if _, ok := got["GET /products/"]; !ok {
			t.Errorf("routes = %+v, missing GET /products/ (trailing slash preserved, matches chi's own semantics)", got)
		}
		if _, ok := got["GET /products/{id}/"]; !ok {
			t.Errorf("routes = %+v, missing GET /products/{id}/ (two levels of nesting)", got)
		}
	})

	t.Run("Group does not add a path prefix", func(t *testing.T) {
		if _, ok := got["GET /admin"]; !ok {
			t.Errorf("routes = %+v, missing GET /admin (Group must not prefix)", got)
		}
	})

	t.Run("a same-package zero-arg Mount constructor is followed", func(t *testing.T) {
		if _, ok := got["GET /orders/"]; !ok {
			t.Errorf("routes = %+v, missing GET /orders/ (Mount should recurse into ordersRouter's body)", got)
		}
		if _, ok := got["POST /orders/"]; !ok {
			t.Errorf("routes = %+v, missing POST /orders/", got)
		}
	})

	t.Run("a Mount constructor is extracted exactly once, not also as its own top-level entry point", func(t *testing.T) {
		// Caught by manually running the plugin end-to-end: without the
		// Extract-time mountedFuncs pre-pass, ordersRouter's own
		// GET "/" registration was independently walked a SECOND time as
		// a top-level function in its own right, producing a spurious
		// route at the bare "/" (empty prefix) in addition to the
		// correctly-prefixed "/orders/".
		if _, ok := got["GET /"]; ok {
			t.Errorf("routes = %+v, contains a spurious GET / -- ordersRouter must only be extracted via Mount's recursion, not also independently", got)
		}
	})

	t.Run("a bare wildcard pattern is declined, not approximated", func(t *testing.T) {
		for path := range got {
			if filepath.Base(path) == "*" || len(path) > 0 && path[len(path)-1] == '*' {
				t.Errorf("routes = %+v, a wildcard pattern should never be emitted", got)
			}
		}
	})

	t.Run("a router-configuring function called with a specific variable resolves to the real mounted prefix", func(t *testing.T) {
		// Mirrors a real pattern found running this plugin against a
		// public Chi project on GitHub: v1 := chi.NewRouter();
		// registerWidgetRoutes(v1); r.Mount("/v1", v1). Extracting
		// registerWidgetRoutes as its own top-level entry point would
		// silently produce /widgets instead of the real /v1/widgets --
		// extractRouterVarBlocks resolves the real prefix instead.
		if _, ok := got["GET /widgets"]; ok {
			t.Errorf("routes = %+v, registerWidgetRoutes must not ALSO be extracted at the empty top-level prefix", got)
		}
		if _, ok := got["GET /v1/widgets"]; !ok {
			t.Errorf("routes = %+v, missing GET /v1/widgets", got)
		}
	})

	t.Run("a Route callback passed by variable instead of inlined is declined", func(t *testing.T) {
		if _, ok := got["GET /detached/never-seen"]; ok {
			t.Errorf("routes = %+v, a non-inlined Route callback must not be extracted", got)
		}
		if _, ok := got["GET /never-seen"]; ok {
			t.Errorf("routes = %+v, a non-inlined Route callback must not be extracted at the wrong prefix either", got)
		}
	})

	t.Run("a non-call map literal is not mistaken for a registration", func(t *testing.T) {
		if len(routes) == 0 {
			t.Fatal("expected at least some routes")
		}
	})

	t.Run("a pattern given via a named constant identifier is declined, not resolved", func(t *testing.T) {
		// stringLiteral only recognizes an inline literal — the same
		// restriction nethttp.go's own stringLiteral has for patterns
		// (unlike constStringArg, used for Method's method argument,
		// which does resolve a named constant via go/constant). A
		// symmetric, pre-existing convention, not a chi-specific gap.
		if _, ok := got["GET /const-path"]; ok {
			t.Errorf("routes = %+v, a pattern given via a named constant identifier should not resolve", got)
		}
	})

	t.Run("an inline func literal handler is carried via HandlerLit", func(t *testing.T) {
		r, ok := got["GET /inline"]
		if !ok {
			t.Fatalf("routes = %+v, missing GET /inline (inline func literal)", got)
		}
		if r.HandlerLit == nil {
			t.Error("route /inline: HandlerLit is nil, want the inline *ast.FuncLit")
		}
		if r.HandlerName != "" || r.HandlerDecl != nil || r.HandlerObj != nil {
			t.Errorf("route /inline: an anonymous handler must have empty Name/Decl/Obj, got %+v", r)
		}
	})

	t.Run("an unterminated path param brace is declined", func(t *testing.T) {
		for path := range got {
			if len(path) >= 7 && path[:7] == "/broken" {
				t.Errorf("routes = %+v, an unterminated { should never be emitted", got)
			}
		}
	})

	t.Run("Method's method argument must be a compile-time constant", func(t *testing.T) {
		if _, ok := got["GET /dynamic-method"]; ok {
			t.Errorf("routes = %+v, a non-constant method argument must be declined", got)
		}
	})

	t.Run("Route's pattern must be a compile-time constant even with an inline callback", func(t *testing.T) {
		if _, ok := got["GET /computed/never-seen-computed"]; ok {
			t.Errorf("routes = %+v, a computed Route prefix must be declined", got)
		}
	})

	t.Run("a chi.Router-shaped closure assigned to a local variable is declined, not walked at the wrong prefix", func(t *testing.T) {
		if _, ok := got["GET /local-detached/never-seen-local"]; ok {
			t.Errorf("routes = %+v, a non-inlined local closure must not be extracted", got)
		}
		if _, ok := got["GET /never-seen-local"]; ok {
			t.Errorf("routes = %+v, must not be extracted at the wrong prefix either", got)
		}
	})

	t.Run("a tracked variable never mounted falls back to the block's own prefix", func(t *testing.T) {
		if _, ok := got["GET /v2-widgets"]; !ok {
			t.Errorf("routes = %+v, missing GET /v2-widgets (v2 is configured but never mounted)", got)
		}
	})

	t.Run("Mount to an untracked bare identifier is declined", func(t *testing.T) {
		if _, ok := got["GET /legacy-mux"]; ok {
			t.Errorf("routes = %+v, legacyMux isn't a tracked chi.Router variable and must not resolve", got)
		}
	})

	t.Run("a \"=\" reassigned router variable is still tracked", func(t *testing.T) {
		if _, ok := got["GET /v3/v3-widgets"]; !ok {
			t.Errorf("routes = %+v, missing GET /v3/v3-widgets (v3 is declared via var then \"=\" assigned)", got)
		}
	})

	t.Run("Mount declines a method-value call, an argument-taking call, and a non-router return type", func(t *testing.T) {
		if _, ok := got["GET /via-method/"]; ok {
			t.Errorf("routes = %+v, a Mount target reached via a method-value call must be declined", got)
		}
		if _, ok := got["GET /via-args/"]; ok {
			t.Errorf("routes = %+v, a Mount target whose call takes arguments must be declined", got)
		}
		if _, ok := got["GET /via-var-func/"]; ok {
			t.Errorf("routes = %+v, a Mount target that's a *types.Var (not a real FuncDecl) must be declined", got)
		}
		// Caught running this exact case: viaArgsRouter takes an
		// argument, so resolveMountTarget correctly declines to recurse
		// into it, but it was ALSO still being walked as its own
		// independent top-level entry point (nothing else excluded it),
		// producing a spurious unprefixed "GET /" -- the same failure
		// mode as the ordersRouter double-extraction bug above, just
		// reached through a different shape (an argument-taking mount
		// target instead of an already-mounted zero-arg one).
		if _, ok := got["GET /"]; ok {
			t.Errorf("routes = %+v, contains a spurious GET / -- an argument-taking Mount target must not be walked as its own top-level entry point either", got)
		}
	})

	t.Run("a router-parameter function with absolute paths is walked at the empty prefix (go8's canonical shape)", func(t *testing.T) {
		// RegisterAbsolute(router chi.Router) registers absolute paths and
		// is never called locally -- its real call site would be
		// cross-package. It must be walked at the empty top-level prefix,
		// the parameter acting as a chi receiver, yielding its absolute
		// paths. This is the fix for the gmhafiz/go8 zero-routes gap.
		if _, ok := got["GET /api/v1/things/"]; !ok {
			t.Errorf("routes = %+v, missing GET /api/v1/things/ (router-param function walked at empty prefix)", got)
		}
		if _, ok := got["POST /api/v1/things/{id}"]; !ok {
			t.Errorf("routes = %+v, missing POST /api/v1/things/{id}", got)
		}
	})

	t.Run("a multi-arg router-parameter function whose var is mounted same-package is declined, not emitted at empty prefix", func(t *testing.T) {
		// registerMultiArgMounted(sub, "cfg") passes a var mounted at
		// /mounted, but via a 2-arg call extractRouterVarBlocks can't wire
		// the prefix through. Declining is correct: emitting it at empty
		// prefix (/multi-arg-thing) would be a silently-wrong path, and we
		// can't produce the real /mounted/multi-arg-thing from a single
		// package's view.
		if _, ok := got["GET /multi-arg-thing"]; ok {
			t.Errorf("routes = %+v, a multi-arg register func with a mounted var must be declined, not emitted at empty prefix", got)
		}
		if _, ok := got["GET /mounted/multi-arg-thing"]; ok {
			t.Errorf("routes = %+v, the real prefix isn't computable from a multi-arg call, so this must be declined entirely", got)
		}
	})
}

// TestExtract_CrossPackage mirrors nethttp's own cross-package test: a
// route's handler (or sub-router constructor) living in a different
// package than the one being analyzed. HandlerDecl/File stay nil
// (Extract only ever sees one package), but HandlerObj must still
// resolve — exercising resolveHandler's qualified-identifier branch and
// proving internal/generate can trace the declaration afterward. A
// cross-package Mount constructor, by contrast, produces no route at
// all: the plugin can't follow into another package's declaration here.
func TestExtract_CrossPackage(t *testing.T) {
	pkgs := loadFixture(t, "routes_cross_package")

	var mainPkg *packages.Package
	for _, pkg := range pkgs {
		if pkg.Name == "main" {
			mainPkg = pkg
		}
	}
	if mainPkg == nil {
		t.Fatalf("fixture setup: no package named main among: %+v", pkgs)
	}

	routes, err := chi.New().Extract(mainPkg)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	got := map[string]router.Route{}
	for _, r := range routes {
		got[r.Method+" "+r.Path] = r
	}

	wantHandler := map[string]string{
		"GET /users/{id}": "GetUser",
		"POST /users":     "CreateUser",
		"GET /items/{id}": "GetItem",
	}
	if len(got) != len(wantHandler) {
		t.Fatalf("got %d routes, want %d: %+v", len(got), len(wantHandler), got)
	}
	for key, handler := range wantHandler {
		r, ok := got[key]
		if !ok {
			t.Fatalf("missing route %q, got: %+v", key, got)
		}
		if r.HandlerName != handler {
			t.Errorf("route %q: HandlerName = %q, want %q", key, r.HandlerName, handler)
		}
		if r.HandlerDecl != nil || r.File != nil {
			t.Errorf("route %q: HandlerDecl/File should be nil (Extract only sees the main package)", key)
		}
		if r.HandlerObj == nil {
			t.Errorf("route %q: HandlerObj is nil, want the resolved cross-package object", key)
		}
	}

	if _, ok := got["GET /widgets/widgets"]; ok {
		t.Errorf("routes = %+v, a cross-package Mount constructor must not be followed", got)
	}
}

var allMethodsForTest = []string{
	"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE",
}
