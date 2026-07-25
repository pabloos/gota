// Package fixture is test data for gin_test.go's TestExtract: it
// exercises every route-registration shape the plugin recognizes
// (engine-direct routes, gin.Default()/gin.New(), single and nested
// Group variables, inline chained Group, Any/Handle/Match, a ":id"
// param, middleware-before-handler, an empty-prefix group whose route
// path has no leading slash, a ".Use(mw)" chain, an inline func-literal
// handler) plus the deliberately-declined
// shapes (a "*catchall" path, a *gin.RouterGroup-parameter register
// function, a reassigned group variable, a non-constant Handle method,
// a handler-less call).
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

	registerTags(v1) // declined: *gin.RouterGroup param -> relative paths, no recoverable prefix

	rg := r.Group("/first")
	rg = r.Group("/second") // reassigned -> ambiguous -> declined
	rg.GET("/reassigned", ReassignedHandler)

	dyn := methodName()
	r.Handle(dyn, "/dynamic", DynamicHandler) // declined: non-constant method
}

func registerTags(rg *gin.RouterGroup) {
	rg.GET("/tags", ListTags) // relative to rg's prefix, unknowable here
}

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
func ReassignedHandler(c *gin.Context)     {}
func DynamicHandler(c *gin.Context)        {}
