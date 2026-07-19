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

type Response[T any] struct {
	Data T `json:"data"`
}

type StatusResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// respond mirrors the response-writing helper found in a real,
// representative Go HTTP API (leeprovoost/go-rest-api-template) where
// every single handler routes its output through exactly this shape —
// the motivating case for following one level into a local helper.
func respond(w http.ResponseWriter, code int, data any) {
	w.WriteHeader(code)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
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

// EncodeGenericResponse must produce a $ref reflecting the instantiation
// (Response_User), not the bare generic name (Response) — otherwise it
// would collide with every other instantiation of Response[T].
func EncodeGenericResponse(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(Response[User]{Data: User{ID: 1}})
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

// RespondViaHelper mirrors the real repo's actual shape: a struct literal
// passed directly as data any, no address-of.
func RespondViaHelper(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusCreated, User{ID: 1})
}

// RespondViaHelperErrorThenSuccess mirrors handleCreateUser's real
// validation-then-success shape: two respond(...) calls in sibling
// branches, at two different explicit codes.
func RespondViaHelperErrorThenSuccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		respond(w, http.StatusBadRequest, StatusResponse{Status: "error", Message: "bad request"})
		return
	}
	respond(w, http.StatusCreated, User{ID: 1})
}

// RespondViaHelperNilData mirrors a delete-style handler. Detection is a
// static AST walk, not a real interpreter — it doesn't evaluate respond's
// own "if data != nil" condition, so it still statically finds the
// Encode(data) call inside that branch and resolves data to this
// nil literal, an untyped-nil type shallowRefSchema correctly declines
// (same as it would for a direct "json.NewEncoder(w).Encode(nil)" call,
// with no helper involved at all). Since nothing else in respond
// registers a response on its own (a bare WriteHeader doesn't — see
// NoPattern above), this handler must detect nothing, not a schema-less
// 204 — a real behavior worth pinning down, not a bug in following.
func RespondViaHelperNilData(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusNoContent, nil)
}

// wrapRespond calls respond itself — a second level of helper
// indirection, still well within maxFollowDepth.
func wrapRespond(w http.ResponseWriter, code int, data any) {
	respond(w, code, data)
}

// RespondViaTwoHelpers must be followed all the way through: two levels
// of local-helper indirection is no longer a special "capped at one
// level" case, just a shorter instance of the same general mechanism
// exercised by RespondViaTooManyHelpers below.
func RespondViaTwoHelpers(w http.ResponseWriter, r *http.Request) {
	wrapRespond(w, http.StatusCreated, User{ID: 1})
}

// wrapRespond2/3/4 stack three more levels on top of wrapRespond/respond,
// making RespondViaTooManyHelpers below five calls away from its own
// WriteHeader/Encode — one more than maxFollowDepth allows.
func wrapRespond2(w http.ResponseWriter, code int, data any) {
	wrapRespond(w, code, data)
}

func wrapRespond3(w http.ResponseWriter, code int, data any) {
	wrapRespond2(w, code, data)
}

func wrapRespond4(w http.ResponseWriter, code int, data any) {
	wrapRespond3(w, code, data)
}

// RespondViaTooManyHelpers must detect NOTHING: the chain down to
// respond's own WriteHeader/Encode calls is five levels deep, one past
// maxFollowDepth.
func RespondViaTooManyHelpers(w http.ResponseWriter, r *http.Request) {
	wrapRespond4(w, http.StatusCreated, User{ID: 1})
}

// cycleA/cycleB call each other and never reach a real WriteHeader/Encode
// call — the direct regression fixture for cycle detection actually
// terminating a mutually-recursive chain, not just the depth cap
// eventually bailing (five levels, comfortably inside maxFollowDepth,
// would otherwise recurse forever without it).
func cycleA() {
	cycleB()
}

func cycleB() {
	cycleA()
}

// RespondViaCycle must detect nothing and, more importantly, must
// terminate at all.
func RespondViaCycle(w http.ResponseWriter, r *http.Request) {
	cycleA()
}

// decodeJSON's own decode-target parameter (v) is a bare identifier, not
// literally "&v" — the call site's actual argument ("&u" below) is what
// needs to surface before decodeCallType's "is this &x" check.
func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func DecodeViaHelper(w http.ResponseWriter, r *http.Request) {
	var u User
	decodeJSON(r, &u)
}

// respondErr writes the whole response in return position — the shape
// of every error-returning response helper, and (once inside a real
// framework) the entire idiom of e.g. Echo. The WriteHeader before it
// must still apply: the ambient code is frame-local state, and the
// return-position Encode is just the last statement to see it.
func respondErr(w http.ResponseWriter, v any) error {
	w.WriteHeader(http.StatusBadRequest)
	return json.NewEncoder(w).Encode(v)
}

func RespondViaReturnHelper(w http.ResponseWriter, r *http.Request) {
	respondErr(w, ErrorResponse{Message: "bad"})
}

// encodeJSON returns its Encode error, so callers can use it in an
// if-Init position.
func encodeJSON(w http.ResponseWriter, v any) error {
	return json.NewEncoder(w).Encode(v)
}

// IfInitHelperCall exercises the "if err := helper(...); err != nil"
// shape: the Init call runs unconditionally, so its 200 User response
// must be detected alongside the error branch's 500.
func IfInitHelperCall(w http.ResponseWriter, r *http.Request) {
	if err := encodeJSON(w, User{ID: 1}); err != nil {
		http.Error(w, "boom", http.StatusInternalServerError)
	}
}

// SwitchInitHelperCall is the switch-flavored sibling of
// IfInitHelperCall.
func SwitchInitHelperCall(w http.ResponseWriter, r *http.Request) {
	switch err := encodeJSON(w, User{ID: 1}); err {
	case nil:
	default:
		http.Error(w, "boom", http.StatusInternalServerError)
	}
}

// TypeSwitchResponses writes different responses per dynamic type —
// same CaseClause walking as a plain switch, previously invisible.
func TypeSwitchResponses(w http.ResponseWriter, r *http.Request) {
	var v any = User{}
	switch v.(type) {
	case User:
		json.NewEncoder(w).Encode(User{})
	default:
		http.Error(w, "unknown", http.StatusBadRequest)
	}
}

// server carries a response helper as a method — a very common
// real-world shape (handlers hang off a server/handler struct and
// share its respond method). Method calls resolve through info.Uses
// exactly like function calls (go/types records the selected method
// object there too, not only in Selections), so s.respond is followed
// the same as a free function: the receiver isn't a parameter, and the
// plain arguments bind positionally.
type server struct{}

func (s *server) respond(w http.ResponseWriter, code int, data any) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func (s *server) CreateViaMethod(w http.ResponseWriter, r *http.Request) {
	s.respond(w, http.StatusCreated, User{ID: 1})
}

// EncodeMapLiteralResponse mirrors a real, common pattern found in
// production Go APIs: wrapping the real payload in an ad-hoc envelope
// via a map literal directly at the call site, instead of a named
// struct.
func EncodeMapLiteralResponse(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": User{ID: 1}})
}

func computeKey() string { return "status" }

// EncodeMapLiteralDynamicKey's key isn't a compile-time constant, so
// mapLiteralSchema can't enumerate it — this must still produce a
// response (an honest, lesser degrade via the type-based path: a bare
// {type: object} with no properties), not zero response.
func EncodeMapLiteralDynamicKey(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{computeKey(): "ok"})
}

// EncodeNamedStringMap exercises shallowRefSchema's own *types.Map case
// directly (a variable, not a literal) rather than mapLiteralSchema.
func EncodeNamedStringMap(w http.ResponseWriter, r *http.Request) {
	m := map[string]User{"a": {ID: 1}}
	json.NewEncoder(w).Encode(m)
}

// EncodeBareString exercises the new top-level *types.Basic case with
// no wrapping at all.
func EncodeBareString(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode("ok")
}
