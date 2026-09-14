// Package main is test data for TestExtract_CrossPackage: it registers
// gin routes against handlers declared in a different package, on both
// the engine and a group.
package main

import (
	"github.com/gin-gonic/gin"

	"github.com/pabloos/gota/internal/router/gin/testdata/routes_cross_package/handlers"
)

func main() {
	r := gin.Default()
	r.GET("/users/:id", handlers.GetUser)
	v1 := r.Group("/api/v1")
	v1.POST("/users", handlers.CreateUser)

	// A register function declared in the handlers package, given its prefix
	// here — the cross-package register-function case.
	handlers.RegisterAdmin(r.Group("/admin"))

	r.Run(":8080")
}
