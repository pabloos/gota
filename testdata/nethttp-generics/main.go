// Package main is test data for TestRun_GenericResponses: two handlers
// encode different instantiations of the same generic Response[T] type,
// proving they resolve to distinct, correctly-typed components instead
// of colliding on one.
package main

import (
	"encoding/json"
	"net/http"
)

type Response[T any] struct {
	Data T `json:"data"`
}

type User struct {
	ID int `json:"id"`
}

type Product struct {
	SKU string `json:"sku"`
}

func GetUser(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(Response[User]{Data: User{ID: 1}})
}

func GetProduct(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(Response[Product]{Data: Product{SKU: "abc"}})
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", GetUser)
	mux.HandleFunc("GET /products/{id}", GetProduct)
	http.ListenAndServe(":8080", mux)
}
