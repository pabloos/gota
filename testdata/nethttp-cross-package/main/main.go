// Package main is test data for TestRun_CrossPackageHandler: it wires up
// routes against handlers declared in a separate package, the common
// "handlers package separate from the package that registers routes"
// project layout.
package main

import (
	"net/http"

	"github.com/pabloos/gota/testdata/nethttp-cross-package/handlers"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", handlers.GetUser)
	mux.HandleFunc("GET /users", handlers.ListUsers)
	http.ListenAndServe(":8080", mux)
}
