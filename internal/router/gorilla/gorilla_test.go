package gorilla_test

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/internal/parser"
	"github.com/pabloos/gota/internal/router"
	"github.com/pabloos/gota/internal/router/gorilla"
)

// loadFixture loads the fixture package at internal/router/gorilla/testdata/<name>
// — a separate Go module (its own go.mod requiring real gorilla/mux) so
// gorilla never becomes a dependency of gota's own root module.
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
	if got := gorilla.New().Name(); got != "gorilla/mux" {
		t.Errorf("Name() = %q, want %q", got, "gorilla/mux")
	}
}

func TestExtract(t *testing.T) {
	pkgs := loadFixture(t, "routes")
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}

	routes, err := gorilla.New().Extract(pkgs[0], pkgs)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := map[string]router.Route{}
	for _, r := range routes {
		got[r.Method+" "+r.Path] = r
	}

	t.Run("a chained .Methods() sets the method, {id} kept", func(t *testing.T) {
		r, ok := got["GET /users/{id}"]
		if !ok {
			t.Fatalf("routes = %+v, missing GET /users/{id}", got)
		}
		if r.HandlerName != "GetUser" || r.HandlerDecl == nil {
			t.Errorf("route = %+v, want GetUser with a resolved decl", r)
		}
	})

	t.Run("a multi-value .Methods() produces one route per method", func(t *testing.T) {
		for _, m := range []string{"POST", "PUT"} {
			if _, ok := got[m+" /users"]; !ok {
				t.Errorf("routes = %+v, missing %s /users", got, m)
			}
		}
		if _, ok := got["GET /users"]; ok {
			t.Errorf("routes = %+v, /users must not include an unlisted method", got)
		}
	})

	t.Run("no .Methods() matches every HTTP method", func(t *testing.T) {
		for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE"} {
			if _, ok := got[m+" /health"]; !ok {
				t.Errorf("routes = %+v, missing %s /health", got, m)
			}
		}
		if _, ok := got["CONNECT /health"]; ok {
			t.Errorf("routes = %+v, should not include CONNECT (no OpenAPI slot)", got)
		}
	})

	t.Run("a {id:regex} constraint degrades to a bare {id}", func(t *testing.T) {
		if _, ok := got["GET /items/{id}"]; !ok {
			t.Errorf("routes = %+v, missing GET /items/{id} (regex must be stripped)", got)
		}
	})

	t.Run("a {id:regex} with a {n} quantifier degrades to a bare {id}", func(t *testing.T) {
		if _, ok := got["GET /quantifier/{id}"]; !ok {
			t.Errorf("routes = %+v, missing GET /quantifier/{id} — a brace in the regex must not truncate the variable (would give /quantifier/{id}})", got)
		}
	})

	t.Run("a full anchored {id:regex} with braces degrades to a bare {id}", func(t *testing.T) {
		if _, ok := got["GET /anchored/{id}"]; !ok {
			t.Errorf("routes = %+v, missing GET /anchored/{id} — the closing brace must balance, not the first one (would give /anchored/{id})$})", got)
		}
	})

	t.Run("a {rest:.*} catch-all degrades to {rest}", func(t *testing.T) {
		if _, ok := got["GET /static/{rest}"]; !ok {
			t.Errorf("routes = %+v, missing GET /static/{rest}", got)
		}
	})

	t.Run("an inline func literal handler is carried via HandlerLit", func(t *testing.T) {
		r, ok := got["GET /ping"]
		if !ok {
			t.Fatalf("routes = %+v, missing GET /ping", got)
		}
		if r.HandlerLit == nil || r.HandlerName != "" || r.HandlerDecl != nil {
			t.Errorf("route /ping = %+v, want an inline handler (HandlerLit set, empty Name/Decl)", r)
		}
	})

	t.Run("methods are found through an intervening builder call", func(t *testing.T) {
		if _, ok := got["GET /secure"]; !ok {
			t.Errorf("routes = %+v, missing GET /secure (.Schemes().Methods() chain)", got)
		}
	})

	t.Run("single-arg middleware unwraps to the real handler", func(t *testing.T) {
		r, ok := got["GET /wrapped"]
		if !ok || r.HandlerName != "WrappedHandler" {
			t.Errorf("route = %+v, want the wrapped WrappedHandler, not the middleware", r)
		}
	})

	t.Run("multi-arg middleware unwraps by the handler-shaped argument", func(t *testing.T) {
		r, ok := got["GET /logged"]
		if !ok || r.HandlerName != "LoggedHandler" {
			t.Errorf("route = %+v, want LoggedHandler (io.Writer arg ignored)", r)
		}
	})

	t.Run("curried middleware unwraps to the real handler", func(t *testing.T) {
		r, ok := got["GET /cors"]
		if !ok || r.HandlerName != "CorsHandler" {
			t.Errorf("route = %+v, want CorsHandler through cors(origin)(H)", r)
		}
	})

	t.Run("a subrouter variable's prefix is accumulated", func(t *testing.T) {
		if _, ok := got["GET /api/v1/products"]; !ok {
			t.Errorf("routes = %+v, missing GET /api/v1/products", got)
		}
	})

	t.Run("nested subrouter variables accumulate transitively", func(t *testing.T) {
		if _, ok := got["GET /api/v1/admin/stats"]; !ok {
			t.Errorf("routes = %+v, missing GET /api/v1/admin/stats", got)
		}
	})

	t.Run("an inline chained subrouter resolves its prefix", func(t *testing.T) {
		if _, ok := got["GET /inline/thing"]; !ok {
			t.Errorf("routes = %+v, missing GET /inline/thing", got)
		}
	})

	t.Run("a NewRoute().Subrouter() inherits the parent prefix", func(t *testing.T) {
		if _, ok := got["GET /api/v1/inherited"]; !ok {
			t.Errorf("routes = %+v, missing GET /api/v1/inherited (NewRoute() must inherit, not drop, the prefix)", got)
		}
	})

	t.Run("a factory-call handler is recognized, named by the factory", func(t *testing.T) {
		r, ok := got["GET /made"]
		if !ok {
			t.Fatalf("routes = %+v, missing GET /made (Handle with a function-call handler)", got)
		}
		if r.HandlerName != "makeHandler" {
			t.Errorf("route /made: HandlerName = %q, want the factory name makeHandler", r.HandlerName)
		}
		m, ok := got["POST /from-method"]
		if !ok {
			t.Fatalf("routes = %+v, missing POST /from-method (Handle with a method-call handler)", got)
		}
		if m.HandlerName != "build" {
			t.Errorf("route /from-method: HandlerName = %q, want the factory method name build", m.HandlerName)
		}
	})

	t.Run("a reassigned subrouter variable is declined", func(t *testing.T) {
		for key := range got {
			if key == "GET /first/reassigned" || key == "GET /second/reassigned" || key == "GET /reassigned" {
				t.Errorf("routes = %+v, a reassigned subrouter var is ambiguous and must be declined", got)
			}
		}
	})

	t.Run("mounting a subrouter variable is not emitted as an endpoint", func(t *testing.T) {
		for key := range got {
			if strings.Contains(key, " /mount") {
				t.Errorf("routes = %+v, a subrouter-var mount must not become an endpoint (%s)", got, key)
			}
		}
	})

	t.Run("a same-package constructor mount applies the prefix and dedups", func(t *testing.T) {
		if _, ok := got["GET /mnt/widget"]; !ok {
			t.Errorf("routes = %+v, missing GET /mnt/widget (Handle mount of a constructor)", got)
		}
		if _, ok := got["GET /strip/widget"]; !ok {
			t.Errorf("routes = %+v, missing GET /strip/widget (StripPrefix mount of a constructor)", got)
		}
		if _, ok := got["GET /widget"]; ok {
			t.Errorf("routes = %+v, a mounted constructor's routes must not be emitted standalone at /widget", got)
		}
		var mnt int
		for _, r := range routes {
			if r.Path == "/mnt/widget" {
				mnt++
			}
		}
		if mnt != 1 {
			t.Errorf("GET /mnt/widget emitted %d times, want 1 (the two Handle mounts must dedup)", mnt)
		}
	})

	t.Run("a non-constant .Methods() is declined", func(t *testing.T) {
		if _, ok := got["GET /dynamic"]; ok {
			t.Errorf("routes = %+v, a non-constant .Methods() must be declined", got)
		}
	})

	t.Run("the split .Path().HandlerFunc() builder form is declined", func(t *testing.T) {
		if _, ok := got["GET /builder"]; ok {
			t.Errorf("routes = %+v, the split builder form is a documented v1 decline", got)
		}
	})
}

// TestExtract_CrossPackage pins that a handler declared in a different
// package than the one being analyzed still resolves HandlerObj (with
// HandlerDecl/File nil, since Extract sees one package) so
// internal/generate can backfill the declaration — on both a root-router
// route and a subrouter route.
func TestExtract_CrossPackage(t *testing.T) {
	pkgs := loadFixture(t, "routes_cross_package")

	var mainPkg *packages.Package
	for _, pkg := range pkgs {
		if pkg.Name == "main" {
			mainPkg = pkg
		}
	}
	if mainPkg == nil {
		t.Fatalf("fixture setup: no package named main among %+v", pkgs)
	}

	routes, err := gorilla.New().Extract(mainPkg, pkgs)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := map[string]router.Route{}
	for _, r := range routes {
		got[r.Method+" "+r.Path] = r
	}

	for key, handler := range map[string]string{
		"GET /users/{id}":    "GetUser",
		"POST /api/v1/users": "CreateUser",
	} {
		r, ok := got[key]
		if !ok {
			t.Fatalf("routes = %+v, missing %q", got, key)
		}
		if r.HandlerName != handler {
			t.Errorf("route %q: HandlerName = %q, want %q", key, r.HandlerName, handler)
		}
		if r.HandlerDecl != nil || r.File != nil {
			t.Errorf("route %q: HandlerDecl/File should be nil (cross-package)", key)
		}
		if r.HandlerObj == nil {
			t.Errorf("route %q: HandlerObj is nil, want the resolved cross-package object", key)
		}
	}
}
