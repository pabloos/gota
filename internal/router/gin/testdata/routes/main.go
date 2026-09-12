// Package fixture is test data for gin_test.go's TestExtract: it
// exercises every route-registration shape the plugin recognizes
// (engine-direct routes, gin.Default()/gin.New(), single and nested
// Group variables, inline chained Group, Any/Handle/Match, a ":id"
// param, middleware-before-handler, an empty-prefix group whose route
// path has no leading slash, a ".Use(mw)" chain, an inline func-literal
// handler, a *gin.RouterGroup-parameter register function resolved to its
// call-site prefix — direct call and interface-dispatch registry loop)
// plus the deliberately-declined shapes (a "*catchall" path, a reassigned
// group variable, a non-constant Handle method, a handler-less call).
package fixture

import (
	"github.com/gin-gonic/gin"
)

func Register() {
	r := gin.Default()

	r.GET("/users/:id", GetUser)
	r.POST("/users", CreateUser)
	r.Any("/health", HealthCheck)
	r.Handle("GET", "/legacy", LegacyHandler)
	r.Match([]string{"GET", "POST"}, "/multi", MultiHandler)
	r.GET("/mw", authMiddleware, WithMiddlewareHandler) // handler is the LAST arg
	r.GET("/ping", func(c *gin.Context) {})             // inline func-literal handler

	v1 := r.Group("/api/v1")
	v1.GET("/products", ListProducts)
	v1.POST("/products", CreateProduct)

	admin := v1.Group("/admin")
	admin.GET("/stats", AdminStats)

	r.Group("/inline").GET("/thing", InlineThing) // chained, no variable

	// An empty-prefix group whose route path has no leading slash: gin
	// roots every served path at "/", so this is GET /current/user, NOT
	// the invalid "current/user" a naive prefix+path concat would emit.
	noPrefix := r.Group("")
	noPrefix.GET("current/user", CurrentUser)

	// A ".Use(mw)" chain returns gin.IRoutes; the route method still
	// registers on the underlying group's prefix -> POST /account/settings.
	r.Group("/account").Use(authMiddleware).POST("/settings", UpdateSettings)

	r.GET("/files/*path", CatchAllHandler) // declined: catch-all has no OpenAPI equivalent

	// A register function taking a *gin.RouterGroup: its "/tags" resolves
	// under v1's "/api/v1" prefix from this call site.
	registerTags(v1)

	// Interface-dispatch registration (the registry idiom): a loop calls
	// Routes on a group with a known prefix; every concrete
	// Routes(*gin.RouterGroup) is resolved under it, matched by name+position.
	mountAPI(r, []apiRegistrar{widgets{}, gadgets{}})

	rg := r.Group("/first")
	rg = r.Group("/second") // reassigned -> ambiguous -> declined
	rg.GET("/reassigned", ReassignedHandler)

	dyn := methodName()
	r.Handle(dyn, "/dynamic", DynamicHandler) // declined: non-constant method
}

func registerTags(rg *gin.RouterGroup) {
	rg.GET("/tags", ListTags) // relative to the group passed at the call site
}

type apiRegistrar interface {
	Routes(rg *gin.RouterGroup)
}

func mountAPI(r *gin.Engine, hs []apiRegistrar) {
	g := r.Group("/api")
	for _, h := range hs {
		h.Routes(g) // dynamic dispatch; the group prefix "/api" is still static
	}
}

type widgets struct{}

func (widgets) Routes(rg *gin.RouterGroup) { rg.GET("/widgets", WidgetsHandler) }

type gadgets struct{}

func (gadgets) Routes(rg *gin.RouterGroup) { rg.POST("/gadgets", GadgetsHandler) }

func methodName() string { return "GET" }

func authMiddleware(c *gin.Context) {}

func GetUser(c *gin.Context)               {}
func CreateUser(c *gin.Context)            {}
func HealthCheck(c *gin.Context)           {}
func LegacyHandler(c *gin.Context)         {}
func MultiHandler(c *gin.Context)          {}
func WithMiddlewareHandler(c *gin.Context) {}
func ListProducts(c *gin.Context)          {}
func CreateProduct(c *gin.Context)         {}
func AdminStats(c *gin.Context)            {}
func InlineThing(c *gin.Context)           {}
func CurrentUser(c *gin.Context)           {}
func UpdateSettings(c *gin.Context)        {}
func CatchAllHandler(c *gin.Context)       {}
func ListTags(c *gin.Context)              {}
func WidgetsHandler(c *gin.Context)        {}
func GadgetsHandler(c *gin.Context)        {}
func ReassignedHandler(c *gin.Context)     {}
func DynamicHandler(c *gin.Context)        {}
