// Package fixture is test data for fiber_test.go's TestExtract: it exercises
// the route shapes the plugin recognizes on a *fiber.App and a fiber.Router
// group (method verbs, All/Add, single/nested/inline groups, middleware
// before the handler, ":id"/":id?"/"*" paths, a register function resolved
// via its call site — direct and interface-dispatch — and a *fiber.App
// register parameter as root) plus the declined shapes (a reassigned group
// var, a non-constant Add method).
package fixture

import "github.com/gofiber/fiber/v2"

func Register() *fiber.App {
	app := fiber.New()
	app.Use(logging) // app-level middleware: contributes no route

	app.Get("/users/:id", GetUser)
	app.Post("/users", authMW, CreateUser) // middleware BEFORE the handler (handler is last)
	app.Delete("/users/:id", DeleteUser)
	app.All("/health", HealthCheck) // every method
	app.Add("GET", "/added", AddedHandler)
	app.Get("/opt/:id?", OptHandler)   // optional param -> {id}
	app.Get("/files/*", StaticHandler) // wildcard -> declined
	app.Get("/ping", func(c *fiber.Ctx) error { return c.SendStatus(204) })

	// Group variables carry an accumulated prefix.
	api := app.Group("/api/v1")
	api.Get("/products", ListProducts)

	admin := api.Group("/admin")
	admin.Get("/stats", AdminStats)

	// Inline chained group.
	app.Group("/inline").Get("/thing", InlineThing)

	// A register function taking a fiber.Router: "/tags" resolves under "/rel".
	registerRelative(app.Group("/rel"))

	// A register function taking *fiber.App is the root.
	registerRoot(app)

	// Interface-dispatch registration (the registry idiom).
	mountAPI(app, []apiRegistrar{widgets{}, gadgets{}})

	// Declined shapes.
	reg := app.Group("/first")
	reg = app.Group("/second") // reassigned -> ambiguous -> declined
	reg.Get("/reassigned", ReassignedHandler)

	app.Add(methodName(), "/dynamic", DynamicHandler) // non-constant method -> declined

	return app
}

func registerRelative(r fiber.Router) { r.Get("/tags", TagsHandler) }
func registerRoot(app *fiber.App)     { app.Get("/root", RootHandler) }

type apiRegistrar interface {
	Routes(r fiber.Router)
}

func mountAPI(app *fiber.App, hs []apiRegistrar) {
	g := app.Group("/api")
	for _, h := range hs {
		h.Routes(g)
	}
}

type widgets struct{}

func (widgets) Routes(r fiber.Router) { r.Get("/widgets", WidgetsHandler) }

type gadgets struct{}

func (gadgets) Routes(r fiber.Router) { r.Post("/gadgets", GadgetsHandler) }

func methodName() string { return "GET" }

func logging(c *fiber.Ctx) error { return c.Next() }
func authMW(c *fiber.Ctx) error  { return c.Next() }

func GetUser(c *fiber.Ctx) error           { return nil }
func CreateUser(c *fiber.Ctx) error        { return nil }
func DeleteUser(c *fiber.Ctx) error        { return nil }
func HealthCheck(c *fiber.Ctx) error       { return nil }
func AddedHandler(c *fiber.Ctx) error      { return nil }
func OptHandler(c *fiber.Ctx) error        { return nil }
func StaticHandler(c *fiber.Ctx) error     { return nil }
func ListProducts(c *fiber.Ctx) error      { return nil }
func AdminStats(c *fiber.Ctx) error        { return nil }
func InlineThing(c *fiber.Ctx) error       { return nil }
func TagsHandler(c *fiber.Ctx) error       { return nil }
func RootHandler(c *fiber.Ctx) error       { return nil }
func WidgetsHandler(c *fiber.Ctx) error    { return nil }
func GadgetsHandler(c *fiber.Ctx) error    { return nil }
func ReassignedHandler(c *fiber.Ctx) error { return nil }
func DynamicHandler(c *fiber.Ctx) error    { return nil }
