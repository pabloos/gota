// Command security-examples is a net/http fixture exercising the
// document-level "gota:doc:" block (securitySchemes, a global security
// requirement, servers, tags, info description) alongside per-operation
// "security" and response "examples" declared in "gota:" comments — the
// OpenAPI dimensions that cannot be inferred from code.
//
// gota:doc:
//
//	info:
//	  description: A signatures API secured with a bearer token.
//	servers:
//	  - url: https://api.example.com
//	    description: Production
//	tags:
//	  - name: signatures
//	    description: Signature resources
//	security:
//	  - BearerAuth: []
//	components:
//	  securitySchemes:
//	    BearerAuth:
//	      type: http
//	      scheme: bearer
//	      bearerFormat: JWT
package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /signatures", ListSignatures)
	mux.HandleFunc("POST /signatures", CreateSignature)
	mux.HandleFunc("GET /signatures/{id}", GetSignature)
	mux.HandleFunc("GET /revoked", ListRevoked)
	mux.HandleFunc("GET /health", Health)

	log.Fatal(http.ListenAndServe(":8080", mux))
}
