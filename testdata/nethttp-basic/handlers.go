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
// infers its method, path and a default 200 response purely from the route
// registration in main.go.
func ListUsers(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode([]User{})
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
