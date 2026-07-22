// Command chi-basic is gota's Chi integration fixture — a separate Go
// module (see go.mod) so chi never becomes a dependency of gota's own
// root module. It exercises: a path parameter, a param-less GET, a POST
// with a request body, a fully-annotated handler, a handler with no
// "gota:" comment at all (pure inference), and nested Route/Mount so
// the pipeline is proven end-to-end on chi's structural difference
// from net/http's flat ServeMux, not just the plugin in isolation.
package main

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	r := chi.NewRouter()
	r.Get("/users/{id}", GetUser)
	r.Get("/users", ListUsers)
	r.Post("/users", CreateUser)
	r.Delete("/users/{id}", DeleteUser)
	r.Get("/debug/info", DebugInfo)

	r.Route("/orders", func(r chi.Router) {
		r.Get("/", ListOrders)
	})
	r.Mount("/products", productsRouter())

	log.Fatal(http.ListenAndServe(":8080", r))
}

func productsRouter() chi.Router {
	r := chi.NewRouter()
	r.Get("/", ListProducts)
	return r
}
