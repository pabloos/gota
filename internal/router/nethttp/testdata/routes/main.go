// Package fixture is test data for nethttp_test.go's TestExtract: it
// exercises every route-registration shape the plugin recognizes (method +
// path pattern, method-less pattern, http.HandlerFunc conversion, the
// package-level http.HandleFunc, a host-prefixed pattern) plus one
// non-route call that must NOT be mistaken for a registration.
package fixture

import "net/http"

func Handlers() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", GetUser)
	mux.HandleFunc("/legacy", LegacyHandler)
	mux.Handle("POST /users", http.HandlerFunc(CreateUser))
	http.HandleFunc("/health", HealthCheck)
	mux.HandleFunc("example.com/hosted", HostedHandler)

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
