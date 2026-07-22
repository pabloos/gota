// Package book declares its own Widget, colliding by bare name with
// package author's -- see author/author.go.
package book

import (
	"encoding/json"
	"net/http"
)

type Widget struct {
	BookField int `json:"book_field"`
}

func CreateWidget(w http.ResponseWriter, r *http.Request) {
	var body Widget
	json.NewDecoder(r.Body).Decode(&body)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(body)
}
