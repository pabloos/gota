// Package fixture is test data for TestExtract_MethodDispatchFuncLit: it
// mirrors the hand-rolled method-dispatcher idiom found in a real,
// representative Go HTTP API (fayzan101/InventoryManagementSystem) —
// a method-less ServeMux pattern whose handler is an anonymous function
// switching or branching on r.Method, itself wrapped in a middleware
// call and an http.HandlerFunc conversion.
package fixture

import (
	"net/http"
	"strings"
)

func CreateProduct(w http.ResponseWriter, r *http.Request)  {}
func ListProducts(w http.ResponseWriter, r *http.Request)   {}
func GetWarehouse(w http.ResponseWriter, r *http.Request)   {}
func SearchProducts(w http.ResponseWriter, r *http.Request) {}
func GetProduct(w http.ResponseWriter, r *http.Request)     {}

func methodNotAllowed(w http.ResponseWriter) {}

// secure mirrors the real repo's middleware wrapper — an extra
// single-argument call layer between the ServeMux registration and the
// http.HandlerFunc conversion.
func secure(h http.Handler) http.Handler { return h }

func Setup() {
	mux := http.NewServeMux()

	// A switch-based dispatcher: two branches, each a direct delegate call.
	mux.Handle("/products", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			CreateProduct(w, r)
		case http.MethodGet:
			ListProducts(w, r)
		default:
			methodNotAllowed(w)
		}
	})))

	// An if/else single-method dispatcher.
	mux.Handle("/warehouses/", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			GetWarehouse(w, r)
		} else {
			methodNotAllowed(w)
		}
	})))

	// Deliberately too complex to recognize: a non-method guard
	// (strings.HasSuffix on the path) runs before the dispatch, so the
	// closure's body isn't a single top-level switch/if-else on
	// r.Method — must produce zero routes, not a partial guess.
	mux.Handle("/products/", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/search") {
			SearchProducts(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			GetProduct(w, r)
		default:
			methodNotAllowed(w)
		}
	})))
}
