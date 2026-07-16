// Package handlers is test data for TestRun_CrossPackageHandler: its
// handlers are registered from main, a different package than the one
// that declares them — proving gota resolves both a "gota:" comment and
// best-effort body inference across that package boundary, not just the
// bare route (method + path).
package handlers

import (
	"encoding/json"
	"net/http"
)

// User is the representation returned by the user endpoints.
type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// gota:
//
//	summary: Get a user by ID
//	responses:
//	  '200':
//	    description: The requested user
func GetUser(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(User{ID: 1, Name: "Ada Lovelace"})
}

// ListUsers has no "gota:" comment: its response must be inferred from
// the json.Encoder call in its own body, using *this* package's own
// type information — not main's, which is what actually exercises the
// cross-package fix (a wrong Info would silently detect nothing here).
func ListUsers(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode([]User{})
}
