// Package fixture is test data for nethttp_test.go's TestExtract: it
// exercises every route-registration shape the plugin recognizes (method +
// path pattern, method-less pattern, http.HandlerFunc conversion, the
// package-level http.HandleFunc, a host-prefixed pattern, an
// explicit-method inline func literal) plus the shapes that must NOT be
// mistaken for a route (a non-route call, a method-less inline closure).
package fixture

import "net/http"

func Handlers() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", GetUser)
	mux.HandleFunc("/legacy", LegacyHandler)
	mux.Handle("POST /users", http.HandlerFunc(CreateUser))
	http.HandleFunc("/health", HealthCheck)
	mux.HandleFunc("example.com/hosted", HostedHandler)

	// An explicit-method inline handler is unambiguous and supported:
	// carried via Route.HandlerLit, named by method+path downstream.
	mux.HandleFunc("POST /inline", func(w http.ResponseWriter, r *http.Request) {})
	// A method-less inline closure is declined: it can't be told apart
	// from a hand-rolled all-methods dispatcher without reading its body.
	mux.HandleFunc("/inline-methodless", func(w http.ResponseWriter, r *http.Request) {})

	notARoute := map[string]string{"HandleFunc": "not a call"}
	_ = notARoute
	return mux
}

// gota:
//
//	summary: Get a user by ID
func GetUser(w http.ResponseWriter, r *http.Request) {}

func LegacyHandler(w http.ResponseWriter, r *http.Request) {}

func CreateUser(w http.ResponseWriter, r *http.Request) {}

func HealthCheck(w http.ResponseWriter, r *http.Request) {}

func HostedHandler(w http.ResponseWriter, r *http.Request) {}
