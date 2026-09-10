// Package main is a regression fixture: a handler returns a generic
// pagination wrapper instantiated with a DEPENDENCY element type
// (repository.Signature) whose bare name "Signature" is ambiguous across
// the reachable graph (see the other module). The wrapper's component name
// must carry the qualified argument (PageResponse_repository.Signature) so
// it resolves instead of degrading to a bare object.
package main

import (
	"encoding/json"
	"net/http"

	_ "github.com/pabloos/gota/internal/generate/testdata/cross-module-generic/other"
	"github.com/pabloos/gota/internal/generate/testdata/cross-module-generic/repository"
)

type PageResponse[T any] struct {
	Total int `json:"total"`
	List  []T `json:"list"`
}

func listSignatures(w http.ResponseWriter, r *http.Request) {
	var list []repository.Signature
	json.NewEncoder(w).Encode(PageResponse[repository.Signature]{List: list})
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /signatures", listSignatures)
	http.ListenAndServe(":8080", mux)
}
