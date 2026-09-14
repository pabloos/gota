package gin_test

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/internal/parser"
	"github.com/pabloos/gota/internal/router"
	"github.com/pabloos/gota/internal/router/gin"
)

// loadFixture loads the fixture package at internal/router/gin/testdata/<name>
// — a separate Go module (its own go.mod requiring real gin) so gin
// never becomes a dependency of gota's own root module.
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
	if got := gin.New().Name(); got != "gin" {
		t.Errorf("Name() = %q, want %q", got, "gin")
	}
}

func TestExtract(t *testing.T) {
	pkgs := loadFixture(t, "routes")
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}

	routes, err := gin.New().Extract(pkgs[0], pkgs)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got := map[string]router.Route{}
	for _, r := range routes {
		got[r.Method+" "+r.Path] = r
	}

	t.Run("engine-direct method calls with a translated :id param", func(t *testing.T) {
		r, ok := got["GET /users/{id}"]
		if !ok {
			t.Fatalf("routes = %+v, missing GET /users/{id} (:id must translate)", got)
		}
		if r.HandlerName != "GetUser" || r.HandlerDecl == nil {
			t.Errorf("route = %+v, want GetUser with a resolved decl", r)
		}
		if _, ok := got["POST /users"]; !ok {
			t.Errorf("routes = %+v, missing POST /users", got)
		}
	})

	t.Run("Any expands to every OpenAPI method, never CONNECT", func(t *testing.T) {
		for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE"} {
			if _, ok := got[m+" /health"]; !ok {
				t.Errorf("routes = %+v, missing %s /health", got, m)
			}
		}
		if _, ok := got["CONNECT /health"]; ok {
			t.Errorf("routes = %+v, should not include CONNECT (no OpenAPI slot)", got)
		}
	})

	t.Run("Handle uses a constant method and args[1] path", func(t *testing.T) {
		if _, ok := got["GET /legacy"]; !ok {
			t.Errorf("routes = %+v, missing GET /legacy", got)
		}
	})

	t.Run("Match produces one route per constant method", func(t *testing.T) {
		if _, ok := got["GET /multi"]; !ok {
			t.Errorf("routes = %+v, missing GET /multi", got)
		}
		if _, ok := got["POST /multi"]; !ok {
			t.Errorf("routes = %+v, missing POST /multi", got)
		}
	})

	t.Run("the handler is the last variadic arg, middleware before it are ignored", func(t *testing.T) {
		r, ok := got["GET /mw"]
		if !ok || r.HandlerName != "WithMiddlewareHandler" {
			t.Errorf("route = %+v, want the LAST arg (WithMiddlewareHandler) as the handler, not the middleware", r)
		}
	})

	t.Run("a group variable's prefix is accumulated", func(t *testing.T) {
		if _, ok := got["GET /api/v1/products"]; !ok {
			t.Errorf("routes = %+v, missing GET /api/v1/products (group prefix)", got)
		}
		if _, ok := got["POST /api/v1/products"]; !ok {
			t.Errorf("routes = %+v, missing POST /api/v1/products", got)
		}
	})

	t.Run("nested group variables accumulate transitively", func(t *testing.T) {
		if _, ok := got["GET /api/v1/admin/stats"]; !ok {
			t.Errorf("routes = %+v, missing GET /api/v1/admin/stats (nested group)", got)
		}
	})

	t.Run("an inline chained Group resolves its prefix", func(t *testing.T) {
		if _, ok := got["GET /inline/thing"]; !ok {
			t.Errorf("routes = %+v, missing GET /inline/thing (chained Group)", got)
		}
	})

	t.Run("an inline func-literal handler is carried via HandlerLit", func(t *testing.T) {
		r, ok := got["GET /ping"]
		if !ok {
			t.Fatalf("routes = %+v, missing GET /ping (inline func literal)", got)
		}
		if r.HandlerLit == nil {
			t.Error("route /ping: HandlerLit is nil, want the inline *ast.FuncLit")
		}
		if r.HandlerName != "" || r.HandlerDecl != nil || r.HandlerObj != nil {
			t.Errorf("route /ping: an anonymous handler must have empty Name/Decl/Obj, got %+v", r)
		}
	})

	t.Run("a .Use(mw) chain keeps the group prefix (returns gin.IRoutes)", func(t *testing.T) {
		if _, ok := got["POST /account/settings"]; !ok {
			t.Errorf("routes = %+v, missing POST /account/settings (.Use() chain must keep the /account prefix)", got)
		}
	})

	t.Run("an empty-prefix group + no-leading-slash path is rooted at /", func(t *testing.T) {
		// gin serves every route from the engine's "/" base, so
		// noPrefix.GET("current/user") on r.Group("") is GET /current/user
		// — a naive prefix+path concat would emit "current/user", an
		// invalid OpenAPI path that fails document validation.
		if _, ok := got["GET /current/user"]; !ok {
			t.Errorf("routes = %+v, missing GET /current/user (empty group + slashless path must root at /)", got)
		}
		if _, ok := got["GET current/user"]; ok {
			t.Errorf("routes = %+v, emitted a path with no leading slash (invalid OpenAPI)", got)
		}
	})

	t.Run("a catch-all path is declined", func(t *testing.T) {
		for path := range got {
			if len(path) >= 7 && path[len(path)-5:] == "*path" {
				t.Errorf("routes = %+v, a *catch-all path must never be emitted", got)
			}
		}
		if _, ok := got["GET /files/{path}"]; ok {
			t.Errorf("routes = %+v, a catch-all must be declined, not translated to a param", got)
		}
	})

	t.Run("interface-dispatch registration resolves under the group's prefix", func(t *testing.T) {
		// mountAPI loops `h.Routes(g)` on g = r.Group("/api"); every concrete
		// Routes(*gin.RouterGroup) is matched by name+position and walked at "/api".
		if _, ok := got["GET /api/widgets"]; !ok {
			t.Errorf("routes = %+v, want GET /api/widgets from interface-dispatch registration", got)
		}
		if _, ok := got["POST /api/gadgets"]; !ok {
			t.Errorf("routes = %+v, want POST /api/gadgets from interface-dispatch registration", got)
		}
	})

	t.Run("a *gin.RouterGroup-parameter register function resolves to its call-site prefix", func(t *testing.T) {
		// registerTags(rg *gin.RouterGroup) { rg.GET("/tags", ...) } is
		// called as registerTags(v1), so its relative "/tags" resolves under
		// v1's "/api/v1" prefix — not emitted bare at "/tags".
		if _, ok := got["GET /api/v1/tags"]; !ok {
			t.Errorf("routes = %+v, want GET /api/v1/tags (register func resolved via its call site)", got)
		}
		if _, ok := got["GET /tags"]; ok {
			t.Errorf("routes = %+v, /tags must carry the call-site prefix, not be emitted bare", got)
		}
	})

	t.Run("a reassigned group variable is declined", func(t *testing.T) {
		if _, ok := got["GET /first/reassigned"]; ok {
			t.Errorf("routes = %+v, a reassigned group var is ambiguous and must be declined", got)
		}
		if _, ok := got["GET /second/reassigned"]; ok {
			t.Errorf("routes = %+v, a reassigned group var is ambiguous and must be declined", got)
		}
		if _, ok := got["GET /reassigned"]; ok {
			t.Errorf("routes = %+v, must not fall back to an empty prefix either", got)
		}
	})

	t.Run("a non-constant Handle method is declined", func(t *testing.T) {
		if _, ok := got["GET /dynamic"]; ok {
			t.Errorf("routes = %+v, a non-constant method must be declined", got)
		}
	})
}

// TestExtract_CrossPackage pins two cross-package behaviors, extracting
// every package the way generate does: a handler declared in a different
// package than the one registering it resolves HandlerObj (with
// HandlerDecl/File nil for the registering package, backfilled later by
// generate); and a register function declared in the handlers package,
// given its group prefix from main, resolves to that prefix.
func TestExtract_CrossPackage(t *testing.T) {
	pkgs := loadFixture(t, "routes_cross_package")

	got := map[string]router.Route{}
	for _, pkg := range pkgs {
		routes, err := gin.New().Extract(pkg, pkgs)
		if err != nil {
			t.Fatalf("Extract(%s): %v", pkg.Name, err)
		}
		for _, r := range routes {
			got[r.Method+" "+r.Path] = r
		}
	}

	// Cross-package handlers registered from main.
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
	// handlers.RegisterAdmin(r.Group("/admin")) from main: its "/stats"
	// resolves under the "/admin" prefix from the cross-package call site.
	admin, ok := got["GET /admin/stats"]
	if !ok {
		t.Fatalf("routes = %+v, want GET /admin/stats (cross-package register function)", keys(got))
	}
	if admin.HandlerName != "AdminStats" {
		t.Errorf("GET /admin/stats: HandlerName = %q, want AdminStats", admin.HandlerName)
	}
}

// keys returns the route keys of got, for readable failure messages.
func keys(got map[string]router.Route) []string {
	out := make([]string, 0, len(got))
	for k := range got {
		out = append(out, k)
	}
	return out
}
