// Package main is test data for TestRun_OperationIDDisambiguation: a
// method-less pattern (net/http's ServeMux matches every method) whose
// single handler would otherwise make all 8 expanded operations infer
// the identical operationId "Login" — an invalid OpenAPI document,
// caught for real against fayzan101/InventoryManagementSystem's
// mux.HandleFunc("/auth/login", auth.Login).
package main

import "net/http"

func Login(w http.ResponseWriter, r *http.Request) {}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", Login)
	http.ListenAndServe(":8080", mux)
}
