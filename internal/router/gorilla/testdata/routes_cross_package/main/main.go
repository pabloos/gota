// Package main is test data for TestExtract_CrossPackage: it registers
// gorilla routes against handlers declared in a different package, on
// both the root router and a subrouter.
package main

import (
	"github.com/gorilla/mux"

	"github.com/pabloos/gota/internal/router/gorilla/testdata/routes_cross_package/handlers"
)

func main() {
	r := mux.NewRouter()
	r.HandleFunc("/users/{id}", handlers.GetUser).Methods("GET")
	api := r.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/users", handlers.CreateUser).Methods("POST")
	_ = r
}
