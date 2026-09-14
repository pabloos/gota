package fiber_test

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/internal/parser"
	"github.com/pabloos/gota/internal/router"
	"github.com/pabloos/gota/internal/router/fiber"
)

// loadFixture loads the fixture package at internal/router/fiber/testdata/<name>
// — a separate Go module (its own go.mod requiring real fiber) so fiber never
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

func keys(got map[string]router.Route) []string {
	out := make([]string, 0, len(got))
	for k := range got {
		out = append(out, k)
	}
	return out
}

func TestName(t *testing.T) {
	if got := fiber.New().Name(); got != "fiber" {
		t.Errorf("Name() = %q, want %q", got, "fiber")
	}
}

func TestExtract(t *testing.T) {
	pkgs := loadFixture(t, "routes")
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}

	routes, err := fiber.New().Extract(pkgs[0], pkgs)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := map[string]router.Route{}
	for _, r := range routes {
		got[r.Method+" "+r.Path] = r
	}

	t.Run("a method verb with a :id path", func(t *testing.T) {
		r, ok := got["GET /users/{id}"]
		if !ok {
			t.Fatalf("routes = %+v, missing GET /users/{id}", keys(got))
		}
		if r.HandlerName != "GetUser" || r.HandlerDecl == nil {
			t.Errorf("route = %+v, want GetUser with a resolved decl", r)
		}
	})

	t.Run("the handler is the last arg; middleware before it is ignored", func(t *testing.T) {
		r, ok := got["POST /users"]
		if !ok || r.HandlerName != "CreateUser" {
			t.Errorf("route = %+v, want CreateUser (the middleware arg is ignored)", r)
		}
	})

	t.Run("All matches every emittable method", func(t *testing.T) {
		for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE"} {
			if _, ok := got[m+" /health"]; !ok {
				t.Errorf("routes = %+v, missing %s /health", keys(got), m)
			}
		}
	})

	t.Run("Add with a constant method", func(t *testing.T) {
		if _, ok := got["GET /added"]; !ok {
			t.Errorf("routes = %+v, missing GET /added", keys(got))
		}
	})

	t.Run("an optional :id? param becomes {id}", func(t *testing.T) {
		if _, ok := got["GET /opt/{id}"]; !ok {
			t.Errorf("routes = %+v, missing GET /opt/{id}", keys(got))
		}
	})

	t.Run("a * wildcard path is declined", func(t *testing.T) {
		for _, k := range keys(got) {
			if k == "GET /files/*" {
				t.Errorf("routes = %+v, a wildcard path must be declined", keys(got))
			}
		}
	})

	t.Run("an inline func literal handler is carried via HandlerLit", func(t *testing.T) {
		r, ok := got["GET /ping"]
		if !ok {
			t.Fatalf("routes = %+v, missing GET /ping", keys(got))
		}
		if r.HandlerLit == nil || r.HandlerName != "" || r.HandlerDecl != nil {
			t.Errorf("route /ping = %+v, want an inline handler (HandlerLit set)", r)
		}
	})

	t.Run("group variables accumulate their prefix, nested and inline", func(t *testing.T) {
		for _, want := range []string{"GET /api/v1/products", "GET /api/v1/admin/stats", "GET /inline/thing"} {
			if _, ok := got[want]; !ok {
				t.Errorf("routes = %+v, missing %s", keys(got), want)
			}
		}
	})

	t.Run("a register function resolves to its call-site prefix", func(t *testing.T) {
		if _, ok := got["GET /rel/tags"]; !ok {
			t.Errorf("routes = %+v, want GET /rel/tags (register func resolved via call site)", keys(got))
		}
		if _, ok := got["GET /tags"]; ok {
			t.Errorf("routes = %+v, /tags must carry the call-site prefix, not be bare", keys(got))
		}
	})

	t.Run("a *fiber.App register parameter is the root", func(t *testing.T) {
		if _, ok := got["GET /root"]; !ok {
			t.Errorf("routes = %+v, want GET /root (a *fiber.App param is root)", keys(got))
		}
	})

	t.Run("interface-dispatch registration resolves under the group's prefix", func(t *testing.T) {
		if _, ok := got["GET /api/widgets"]; !ok {
			t.Errorf("routes = %+v, want GET /api/widgets from interface-dispatch registration", keys(got))
		}
		if _, ok := got["POST /api/gadgets"]; !ok {
			t.Errorf("routes = %+v, want POST /api/gadgets from interface-dispatch registration", keys(got))
		}
	})

	t.Run("a reassigned group variable is declined", func(t *testing.T) {
		for _, k := range keys(got) {
			if k == "GET /second/reassigned" || k == "GET /first/reassigned" {
				t.Errorf("routes = %+v, a reassigned group var must be declined", keys(got))
			}
		}
	})

	t.Run("a non-constant Add method is declined", func(t *testing.T) {
		if _, ok := got["GET /dynamic"]; ok {
			t.Errorf("routes = %+v, a non-constant method must be declined", keys(got))
		}
	})
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

	_ = mainPkg
	got := map[string]router.Route{}
	for _, pkg := range pkgs {
		routes, err := fiber.New().Extract(pkg, pkgs)
		if err != nil {
			t.Fatalf("Extract(%s): %v", pkg.Name, err)
		}
		for _, r := range routes {
			got[r.Method+" "+r.Path] = r
		}
	}

	for key, handler := range map[string]string{
		"GET /users/{id}":    "GetUser",
		"POST /api/v1/users": "CreateUser",
	} {
		r, ok := got[key]
		if !ok {
			t.Fatalf("routes = %+v, missing %q", keys(got), key)
		}
		if r.HandlerName != handler {
			t.Errorf("route %q: HandlerName = %q, want %q", key, r.HandlerName, handler)
		}
		if r.HandlerObj == nil {
			t.Errorf("route %q: HandlerObj is nil, want the resolved cross-package object", key)
		}
	}

	// A register function in the handlers package, called as
	// handlers.RegisterAdmin(app.Group("/admin")) from main.
	admin, ok := got["GET /admin/stats"]
	if !ok {
		t.Fatalf("routes = %+v, want GET /admin/stats (cross-package register function)", keys(got))
	}
	if admin.HandlerName != "AdminStats" {
		t.Errorf("GET /admin/stats: HandlerName = %q, want AdminStats", admin.HandlerName)
	}
}
