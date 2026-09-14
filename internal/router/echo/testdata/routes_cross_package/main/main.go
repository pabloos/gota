// Package main is test data for TestExtract_CrossPackage: it registers echo
// routes against handlers declared in a different package, on both the
// engine and a group.
package main

import (
	"github.com/labstack/echo/v4"

	"github.com/pabloos/gota/internal/router/echo/testdata/routes_cross_package/handlers"
)

func main() {
	e := echo.New()
	e.GET("/users/:id", handlers.GetUser)
	v1 := e.Group("/api/v1")
	v1.POST("/users", handlers.CreateUser)
	handlers.RegisterAdmin(e.Group("/admin"))
	e.Logger.Fatal(e.Start(":8080"))
}
