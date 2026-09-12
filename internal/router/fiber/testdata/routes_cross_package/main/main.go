// Package main is test data for TestExtract_CrossPackage: it registers fiber
// routes against handlers declared in a different package, on both the app
// and a group.
package main

import (
	"github.com/gofiber/fiber/v2"

	"github.com/pabloos/gota/internal/router/fiber/testdata/routes_cross_package/handlers"
)

func main() {
	app := fiber.New()
	app.Get("/users/:id", handlers.GetUser)
	v1 := app.Group("/api/v1")
	v1.Post("/users", handlers.CreateUser)
	app.Listen(":8080")
}
