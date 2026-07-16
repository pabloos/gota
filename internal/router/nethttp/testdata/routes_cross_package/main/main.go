// Package main is test data for TestExtract_CrossPackageHandler: it
// registers routes against handlers declared in a different package.
package main

import (
	"net/http"

	"github.com/pabloos/gota/internal/router/nethttp/testdata/routes_cross_package/handlers"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", handlers.GetUser)
	mux.Handle("POST /users", http.HandlerFunc(handlers.CreateUser))
	mux.HandleFunc("GET /items/{id}", handlers.Srv.GetItem)
	http.ListenAndServe(":8080", mux)
}
