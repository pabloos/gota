// Package author declares its own Widget type, colliding by bare name
// with package book's — the shape a real domain-driven API produces
// (see gmhafiz/go8, where author and book each declare UpdateRequest).
package author

import (
	"encoding/json"
	"net/http"
)

type Widget struct {
	AuthorField string `json:"author_field"`
}

func CreateWidget(w http.ResponseWriter, r *http.Request) {
	var body Widget
	json.NewDecoder(r.Body).Decode(&body)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(body)
}
