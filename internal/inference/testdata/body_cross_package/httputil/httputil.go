// Package httputil is test data for TestDetectBody_CrossPackage: a
// shared response-helper library, mirroring the real-world shape found
// in a production Go HTTP API — Success wraps a payload in an ad-hoc
// envelope via a map literal and delegates the actual write to JSON, one
// package and one level away from where it's called.
package httputil

import (
	"encoding/json"
	"net/http"
)

func JSON(w http.ResponseWriter, status int, payload interface{}) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func Success(w http.ResponseWriter, status int, data interface{}) {
	JSON(w, status, map[string]interface{}{"status": "success", "data": data})
}
