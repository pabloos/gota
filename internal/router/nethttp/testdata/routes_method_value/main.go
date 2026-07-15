// Package fixture is test data for nethttp_test.go's
// TestExtract_MethodValueHandler: it exercises the common
// dependency-injection pattern where handlers are methods on a server
// struct, bound as method values (srv.GetItem) rather than referenced as
// bare package-level functions.
package fixture

import "net/http"

type Server struct{}

func Handlers(srv *Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", srv.GetItem)
	return mux
}

// gota:
//
//	summary: Get an item by ID
func (s *Server) GetItem(w http.ResponseWriter, r *http.Request) {}
