package main

import (
	"encoding/json"
	"net/http"
)

// Signature is a stored signature.
type Signature struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

// ListSignatures returns every signature. Its response carries an example,
// declared in the gota: comment since examples cannot be inferred.
//
// gota:
//
//	summary: List signatures
//	tags: [signatures]
//	responses:
//	  '200':
//	    description: The signatures.
//	    content:
//	      application/json:
//	        schema:
//	          type: array
//	          items:
//	            $ref: '#/components/schemas/Signature'
//	        examples:
//	          sample:
//	            summary: One signature
//	            value:
//	              - id: sig_1
//	                hash: abc123
func ListSignatures(w http.ResponseWriter, r *http.Request) {
	var sigs []Signature
	json.NewEncoder(w).Encode(sigs)
}

// CreateSignature stores a new signature. It declares an explicit
// per-operation security requirement to show it round-trips independently
// of the document-level default.
//
// gota:
//
//	summary: Create a signature
//	tags: [signatures]
//	security:
//	  - BearerAuth: []
func CreateSignature(w http.ResponseWriter, r *http.Request) {
	var sig Signature
	json.NewDecoder(r.Body).Decode(&sig)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sig)
}

// GetSignature returns one signature. Its gota: comment adds a description
// and a response example but declares no schema, so the schema gota infers
// from the Encode below ($ref Signature) must survive the merge alongside
// the declared example and the inferred "OK" description.
//
// gota:
//
//	description: Fetch a single signature.
//	responses:
//	  '200':
//	    content:
//	      application/json:
//	        example:
//	          id: sig_2f8a
//	          hash: abc123
func GetSignature(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(Signature{})
}

// Health is a public liveness probe. An explicit empty security requirement
// overrides the document-level Bearer default, marking it unauthenticated.
//
// gota:
//
//	summary: Liveness probe
//	security: []
func Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
