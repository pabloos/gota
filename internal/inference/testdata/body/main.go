// Package fixture is test data for internal/inference's body detection
// tests (body_test.go). It is not meant to run — every function here
// exists to exercise one specific encoding/json + net/http idiom that
// DetectBody should (or deliberately should not) recognize.
package fixture

import (
	"encoding/json"
	"net/http"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type ErrorResponse struct {
	Message string `json:"message"`
}

func DecodeDirect(w http.ResponseWriter, r *http.Request) {
	var u User
	json.NewDecoder(r.Body).Decode(&u)
}

func DecodeViaVariable(w http.ResponseWriter, r *http.Request) {
	var u User
	dec := json.NewDecoder(r.Body)
	dec.Decode(&u)
}

func UnmarshalCall(w http.ResponseWriter, r *http.Request) {
	var u User
	data := []byte("{}")
	json.Unmarshal(data, &u)
}

func EncodeSingle(w http.ResponseWriter, r *http.Request) {
	u := User{}
	json.NewEncoder(w).Encode(u)
}

func EncodeErrorThenSuccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		json.NewEncoder(w).Encode(ErrorResponse{Message: "bad"})
		return
	}
	u := User{}
	json.NewEncoder(w).Encode(u)
}

func MarshalCall(w http.ResponseWriter, r *http.Request) {
	u := User{}
	b, _ := json.Marshal(u)
	w.Write(b)
}

func EncodeSlice(w http.ResponseWriter, r *http.Request) {
	users := []User{}
	json.NewEncoder(w).Encode(users)
}

func NoPattern(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(204)
}

func DecodeNonStruct(w http.ResponseWriter, r *http.Request) {
	var s string
	json.NewDecoder(r.Body).Decode(&s)
}

func EncodeWithExplicitCode(w http.ResponseWriter, r *http.Request) {
	u := User{}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(u)
}

func TwoBranchesDifferentCodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Message: "not found"})
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(User{})
}

func SiblingBranchNoLeak(w http.ResponseWriter, r *http.Request) {
	if r.Method == "DELETE" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(User{})
}

func HTTPErrorCall(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not found", http.StatusNotFound)
}

func MixedErrorAndSuccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "bad method", http.StatusMethodNotAllowed)
		return
	}
	json.NewEncoder(w).Encode(User{})
}

func computeCode() int { return 200 }

func WriteHeaderDynamicCode(w http.ResponseWriter, r *http.Request) {
	code := computeCode()
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(User{})
}

func SkippedErrorBranch(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Debug") == "1" {
		// gota:
		//   x-gota-skip: true
		http.Error(w, "debug mode not supported yet", http.StatusTeapot)
		return
	}
	json.NewEncoder(w).Encode(User{})
}

func SkippedEncodeWithCode(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusCreated)
	// gota:
	//   x-gota-skip: true
	json.NewEncoder(w).Encode(User{})
}
