// Package main is a regression fixture: a handler returns a dependency
// type (one.Thing) whose bare name "Thing" also exists in another
// reachable dependency (two.Thing) that no handler exposes. Resolving the
// inferred bare "Thing" ref must not abort the whole document on a
// spurious ambiguity — it degrades to a generic object.
package main

import (
	"encoding/json"
	"net/http"

	one "github.com/pabloos/gota/internal/generate/testdata/cross-module-ambiguous/one"
	_ "github.com/pabloos/gota/internal/generate/testdata/cross-module-ambiguous/two"
)

func listThings(w http.ResponseWriter, r *http.Request) {
	var things []*one.Thing
	json.NewEncoder(w).Encode(things)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /things", listThings)
	http.ListenAndServe(":8080", mux)
}
