package inference

import "go/ast"

// Dialect is the set of framework-specific call-shape recognizers
// DetectBody consults while walking a handler body. The shared walk
// machinery — branch-aware status tracking, helper-chain following,
// schema building — is framework-agnostic; a Dialect tells it which
// calls mean "decode the request body into X", "write a response", or
// "set the status code in effect".
//
// The interface is deliberately sealed (unexported methods): dialects
// must live in this package, because the walk hands them unexported
// state (*evalCtx). Callers select one via the exported constructors
// (NetHTTP today; future frameworks add their own, paired with their
// router plugin at the generate.Options level).
//
// A future framework dialect should embed netHTTPDialect and try its
// own recognizers first, falling back to the embedded ones: the
// encode/decode recognizers netHTTPDialect carries are encoding/json
// idioms, not net/http ones, and stay perfectly ordinary inside e.g. a
// Gin handler (json.NewDecoder(c.Request.Body).Decode(&x) — Gin's
// c.Request is a plain *http.Request).
type Dialect interface {
	// decodeTarget reports that call decodes the request body into the
	// returned expression, interpreted under the returned context.
	decodeTarget(call *ast.CallExpr, ctx *evalCtx) (ast.Expr, *evalCtx, bool)
	// response reports that call writes a response and/or changes the
	// status code in effect. Returning ok=false must mean "not this
	// dialect's idiom at all" — a call whose shape matches but whose
	// meaning can't be fully parsed (e.g. a non-constant status
	// argument) must also return false, so the walk still gets to try
	// following the call as a helper. Recognized means fully parsed.
	response(call *ast.CallExpr, ctx *evalCtx) (responseEffect, bool)
}

// NetHTTP returns the dialect recognizing stdlib net/http +
// encoding/json idioms — the one every plain-net/http (and future Chi)
// router plugin pairs with. See netHTTPDialect for the exact shapes.
func NetHTTP() Dialect { return netHTTPDialect{} }

// responseEffect describes what a recognized call does to the response
// state of the handler being walked.
//
// Invariants: value is only meaningful when record is set. code == 0
// means "the ambient code in effect at the call site" — a real status
// can never be 0 because dialects decline codes outside [100, 599]
// (net/http itself panics on them at runtime, so nothing is lost).
type responseEffect struct {
	// ambient, if >0, becomes the code in effect for subsequent
	// statements in the same block (WriteHeader-like). A call can set
	// this without recording anything, and vice versa.
	ambient int
	// record registers a response now.
	record bool
	// code, if >0, is the explicit status for the recorded response
	// (http.Error-like, or Gin's c.JSON(201, obj)); 0 records at the
	// ambient code in effect.
	code int
	// value is the encoded expression whose schema the response
	// carries, interpreted under valueCtx; nil means deliberately
	// schema-less (http.Error writes plain text, not JSON).
	value    ast.Expr
	valueCtx *evalCtx
	// mediaType is reserved for future non-JSON dialects (Gin's c.XML,
	// Echo's c.String); "" means application/json, and record() does
	// not consume anything else yet.
	mediaType string
}
