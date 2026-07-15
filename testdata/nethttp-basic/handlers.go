package main

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// User is the representation returned by the user endpoints.
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Bio   string `json:"bio,omitempty"`
}

// gota:
//
//	summary: Get a user by ID
//	parameters:
//	  - name: id
//	    in: path
//	    required: true
//	    schema:
//	      type: integer
//	responses:
//	  '200':
//	    description: The requested user
//	    content:
//	      application/json:
//	        schema:
//	          $ref: '#/components/schemas/User'
//	  '404':
//	    description: User not found
func GetUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	json.NewEncoder(w).Encode(User{ID: id, Name: "Ada Lovelace", Email: "ada@example.com"})
}

// ListUsers returns every registered user. It has no "gota:" comment: gota
// infers its method and path from the route registration in main.go, and
// its responses — 200 and 500 — from the encoding/json and http.Error
// calls in its own body. The debug branch below is marked "x-gota-skip"
// because it's an unpolished, undocumented escape hatch, not a real API
// response — that one response is excluded while 200 and 500 still show up.
func ListUsers(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Debug") == "1" {
		// gota:
		//   x-gota-skip: true
		http.Error(w, "debug mode not supported yet", http.StatusTeapot)
		return
	}
	users, err := fetchUsers()
	if err != nil {
		http.Error(w, "failed to list users", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(users)
}

func fetchUsers() ([]User, error) {
	return []User{}, nil
}

// gota:
//
//	requestBody:
//	  required: true
//	  content:
//	    application/json:
//	      schema:
//	        type: object
//	        required: [name, email]
//	        properties:
//	          name:
//	            type: string
//	          email:
//	            type: string
//	responses:
//	  '201':
//	    description: The created user
//	    content:
//	      application/json:
//	        schema:
//	          $ref: '#/components/schemas/User'
func CreateUser(w http.ResponseWriter, r *http.Request) {
	var u User
	json.NewDecoder(r.Body).Decode(&u)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(u)
}

// DeleteUser removes a user. No "gota:" comment: gota infers the {id} path
// parameter from the route pattern and falls back to a default response.
func DeleteUser(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// gota:
//
//	x-gota-skip: true
func DebugInfo(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "debug"})
}
