// Package main is an end-to-end fixture for TestRun_CrossModuleType: a
// handler whose response type is declared in a DIFFERENT module
// (catalog.Product). gota analyzes only this module, so that type
// is a dependency, not an analyzed root; ResolveSchemaRefs must still
// resolve it into a real component instead of aborting.
package main

import (
	"encoding/json"
	"net/http"

	catalog "github.com/pabloos/gota/internal/generate/testdata/cross-module/lib"
)

func listClients(w http.ResponseWriter, r *http.Request) {
	var clients []*catalog.Product
	json.NewEncoder(w).Encode(clients)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /clients", listClients)
	http.ListenAndServe(":8080", mux)
}
