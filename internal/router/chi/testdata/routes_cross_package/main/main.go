// Package main is test data for TestExtract_CrossPackage: it registers
// chi routes against handlers declared in a different package.
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pabloos/gota/internal/router/chi/testdata/routes_cross_package/handlers"
)

func main() {
	r := chi.NewRouter()
	r.Get("/users/{id}", handlers.GetUser)                  // qualified identifier
	r.Post("/users", http.HandlerFunc(handlers.CreateUser)) // http.HandlerFunc conversion of one
	r.Get("/items/{id}", handlers.Srv.GetItem)              // cross-package method value
	r.Mount("/widgets", handlers.WidgetsRouter())           // cross-package Mount target (declined)
	http.ListenAndServe(":8080", r)
}
