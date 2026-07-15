// Command nethttp-basic is a minimal net/http server used as gota's Phase 1
// integration fixture. It exercises: a path parameter, a param-less GET, a
// POST with a request body, a fully-annotated handler, and a handler with
// no "gota:" comment at all (pure inference).
package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", GetUser)
	mux.HandleFunc("GET /users", ListUsers)
	mux.HandleFunc("POST /users", CreateUser)
	mux.HandleFunc("DELETE /users/{id}", DeleteUser)
	mux.HandleFunc("GET /debug/info", DebugInfo)

	log.Fatal(http.ListenAndServe(":8080", mux))
}
