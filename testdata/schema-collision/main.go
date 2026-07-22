// Command schema-collision is a generate-level fixture for
// TestRun_SchemaNameCollision: two handler packages each declare their
// own Widget and decode/encode it (INFERRED $refs, no "gota:"
// comments). Before package-qualified disambiguation this failed
// outright with an "ambiguous" error; now both must resolve to distinct
// author.Widget / book.Widget components and the doc must validate.
package main

import (
	"log"
	"net/http"

	"github.com/pabloos/gota/testdata/schema-collision/author"
	"github.com/pabloos/gota/testdata/schema-collision/book"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /author/widgets", author.CreateWidget)
	mux.HandleFunc("POST /book/widgets", book.CreateWidget)
	log.Fatal(http.ListenAndServe(":8080", mux))
}
