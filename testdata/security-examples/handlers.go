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
