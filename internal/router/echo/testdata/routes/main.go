// Package fixture is test data for echo_test.go's TestExtract: it exercises
// the route shapes the plugin recognizes on an *echo.Echo and *echo.Group
// (method verbs, Any/Add/Match, single and nested/inline groups, middleware
// after the handler, ":id" and "*" paths, TRACE vs CONNECT, an inline func
// literal) plus the deliberately-declined shapes (an *echo.Group-parameter
// register function, a reassigned group var, a non-constant Add method).
package fixture

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func Register() *echo.Echo {
	e := echo.New()
	e.Use(logging) // engine-level middleware: contributes no route

	e.GET("/users/:id", GetUser)
	e.POST("/users", CreateUser, authMW) // middleware AFTER the handler (fixed handler index)
	e.DELETE("/users/:id", DeleteUser)
	e.TRACE("/trace", TraceHandler)      // TRACE has an OpenAPI slot -> emitted
	e.CONNECT("/tunnel", ConnectHandler) // CONNECT has no slot -> declined
	e.Any("/health", HealthCheck)        // every method
	e.Add("GET", "/added", AddedHandler) // explicit method as a constant
	e.Match([]string{"GET", "POST"}, "/matched", MatchedHandler)
	e.GET("/static/*", StaticHandler) // catch-all -> declined
	e.GET("/ping", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

	// Group variables carry an accumulated prefix.
	api := e.Group("/api/v1")
	api.GET("/products", ListProducts)

	admin := api.Group("/admin")
	admin.GET("/stats", AdminStats)

	// Inline chained group.
	e.Group("/inline").GET("/thing", InlineThing)

	// Declined shapes.
	reg := e.Group("/first")
	reg = e.Group("/second") // reassigned -> ambiguous -> declined
	reg.GET("/reassigned", ReassignedHandler)

	e.Add(methodName(), "/dynamic", DynamicHandler) // non-constant method -> declined

	// A register function taking an *echo.Group: its "/tags" resolves under
	// the "/rel" prefix from this call site.
	registerRelative(e.Group("/rel"))

	// Interface-dispatch registration (the real-world "registry" idiom): a
	// loop calls Routes on a group with a known prefix; every concrete
	// Routes(*echo.Group) is resolved under it, matched by name + position.
	mountAPI(e, []apiHandler{widgets{}, gadgets{}})

	return e
}

// registerRelative registers on an *echo.Group parameter; its relative path
// resolves to the prefix of the group passed at the call site.
func registerRelative(g *echo.Group) {
	g.GET("/tags", TagsHandler)
}

type apiHandler interface {
	Routes(g *echo.Group)
}

func mountAPI(e *echo.Echo, hs []apiHandler) {
	g := e.Group("/api")
	for _, h := range hs {
		h.Routes(g) // dynamic dispatch; the group prefix "/api" is still static
	}
}

type widgets struct{}

func (widgets) Routes(g *echo.Group) { g.GET("/widgets", WidgetsHandler) }

type gadgets struct{}

func (gadgets) Routes(g *echo.Group) { g.POST("/gadgets", GadgetsHandler) }

func methodName() string { return "GET" }

func logging(next echo.HandlerFunc) echo.HandlerFunc { return next }
func authMW(next echo.HandlerFunc) echo.HandlerFunc  { return next }

func GetUser(c echo.Context) error           { return nil }
func CreateUser(c echo.Context) error        { return nil }
func DeleteUser(c echo.Context) error        { return nil }
func TraceHandler(c echo.Context) error      { return nil }
func ConnectHandler(c echo.Context) error    { return nil }
func HealthCheck(c echo.Context) error       { return nil }
func AddedHandler(c echo.Context) error      { return nil }
func MatchedHandler(c echo.Context) error    { return nil }
func StaticHandler(c echo.Context) error     { return nil }
func ListProducts(c echo.Context) error      { return nil }
func AdminStats(c echo.Context) error        { return nil }
func InlineThing(c echo.Context) error       { return nil }
func ReassignedHandler(c echo.Context) error { return nil }
func DynamicHandler(c echo.Context) error    { return nil }
func TagsHandler(c echo.Context) error       { return nil }
func WidgetsHandler(c echo.Context) error    { return nil }
func GadgetsHandler(c echo.Context) error    { return nil }
