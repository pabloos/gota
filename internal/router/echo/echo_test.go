package echo_test

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/internal/parser"
	"github.com/pabloos/gota/internal/router"
	"github.com/pabloos/gota/internal/router/echo"
)

// loadFixture loads the fixture package at internal/router/echo/testdata/<name>
// — a separate Go module (its own go.mod requiring real echo) so echo never
// becomes a dependency of gota's own root module.
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
	if got := echo.New().Name(); got != "echo" {
		t.Errorf("Name() = %q, want %q", got, "echo")
	}
}

func TestExtract(t *testing.T) {
	pkgs := loadFixture(t, "routes")
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}

	routes, err := echo.New().Extract(pkgs[0])
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := map[string]router.Route{}
	for _, r := range routes {
		got[r.Method+" "+r.Path] = r
	}

	t.Run("a method verb with a :id path, handler after the path", func(t *testing.T) {
		r, ok := got["GET /users/{id}"]
		if !ok {
			t.Fatalf("routes = %+v, missing GET /users/{id}", got)
		}
		if r.HandlerName != "GetUser" || r.HandlerDecl == nil {
			t.Errorf("route = %+v, want GetUser with a resolved decl", r)
		}
	})

	t.Run("middleware after the handler doesn't displace it", func(t *testing.T) {
		r, ok := got["POST /users"]
		if !ok || r.HandlerName != "CreateUser" {
			t.Errorf("route = %+v, want CreateUser (the middleware arg is ignored)", r)
		}
	})

	t.Run("TRACE is emitted, CONNECT is declined", func(t *testing.T) {
		if _, ok := got["TRACE /trace"]; !ok {
			t.Errorf("routes = %+v, missing TRACE /trace", got)
		}
		if _, ok := got["CONNECT /tunnel"]; ok {
			t.Errorf("routes = %+v, CONNECT has no OpenAPI slot and must be declined", got)
		}
	})

	t.Run("Any matches every emittable method", func(t *testing.T) {
		for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE"} {
			if _, ok := got[m+" /health"]; !ok {
				t.Errorf("routes = %+v, missing %s /health", got, m)
			}
		}
		if _, ok := got["CONNECT /health"]; ok {
			t.Errorf("routes = %+v, Any must not include CONNECT", got)
		}
	})

	t.Run("Add with a constant method", func(t *testing.T) {
		if _, ok := got["GET /added"]; !ok {
			t.Errorf("routes = %+v, missing GET /added", got)
		}
	})

	t.Run("Match with several methods", func(t *testing.T) {
		for _, m := range []string{"GET", "POST"} {
			if _, ok := got[m+" /matched"]; !ok {
				t.Errorf("routes = %+v, missing %s /matched", got, m)
			}
		}
	})

	t.Run("a * catch-all path is declined", func(t *testing.T) {
		for k := range got {
			if k == "GET /static/*" || k == "GET /static/{*}" {
				t.Errorf("routes = %+v, a catch-all path must be declined", got)
			}
		}
	})

	t.Run("an inline func literal handler is carried via HandlerLit", func(t *testing.T) {
		r, ok := got["GET /ping"]
		if !ok {
			t.Fatalf("routes = %+v, missing GET /ping", got)
		}
		if r.HandlerLit == nil || r.HandlerName != "" || r.HandlerDecl != nil {
			t.Errorf("route /ping = %+v, want an inline handler (HandlerLit set)", r)
		}
	})

	t.Run("a single group variable's prefix is accumulated", func(t *testing.T) {
		if _, ok := got["GET /api/v1/products"]; !ok {
			t.Errorf("routes = %+v, missing GET /api/v1/products", got)
		}
	})

	t.Run("a nested group accumulates both prefixes", func(t *testing.T) {
		if _, ok := got["GET /api/v1/admin/stats"]; !ok {
			t.Errorf("routes = %+v, missing GET /api/v1/admin/stats", got)
		}
	})

	t.Run("an inline chained group applies its prefix", func(t *testing.T) {
		if _, ok := got["GET /inline/thing"]; !ok {
			t.Errorf("routes = %+v, missing GET /inline/thing", got)
		}
	})

	t.Run("a reassigned group variable is declined", func(t *testing.T) {
		for k := range got {
			if k == "GET /second/reassigned" || k == "GET /first/reassigned" {
				t.Errorf("routes = %+v, a reassigned (ambiguous) group var must be declined", got)
			}
		}
	})

	t.Run("a non-constant Add method is declined", func(t *testing.T) {
		if _, ok := got["GET /dynamic"]; ok {
			t.Errorf("routes = %+v, a non-constant method must be declined", got)
		}
	})

	t.Run("interface-dispatch registration resolves under the group's prefix", func(t *testing.T) {
		// mountAPI loops `h.Routes(g)` on g = e.Group("/api"); every concrete
		// Routes(*echo.Group) is matched by name+position and walked at "/api".
		if _, ok := got["GET /api/widgets"]; !ok {
			t.Errorf("routes = %+v, want GET /api/widgets from interface-dispatch registration", keys(got))
		}
		if _, ok := got["POST /api/gadgets"]; !ok {
			t.Errorf("routes = %+v, want POST /api/gadgets from interface-dispatch registration", keys(got))
		}
	})

	t.Run("a group-parameter register func resolves to its call-site prefix", func(t *testing.T) {
		// registerRelative(g *echo.Group) { g.GET("/tags", ...) } is called
		// as registerRelative(e.Group("/rel")), so its relative "/tags"
		// resolves under the "/rel" prefix from the call site.
		if _, ok := got["GET /rel/tags"]; !ok {
			t.Errorf("routes = %+v, want GET /rel/tags (register func resolved via its call site)", keys(got))
		}
		if _, ok := got["GET /tags"]; ok {
			t.Errorf("routes = %+v, /tags must carry the call-site prefix, not be emitted bare", keys(got))
		}
	})
}

// keys returns the route keys of got, for readable failure messages.
func keys(got map[string]router.Route) []string {
	out := make([]string, 0, len(got))
	for k := range got {
		out = append(out, k)
	}
	return out
}

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

	routes, err := echo.New().Extract(mainPkg)
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
