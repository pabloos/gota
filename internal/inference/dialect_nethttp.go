package inference

import (
	"go/ast"
	"go/types"
)

// netHTTPDialect recognizes stdlib net/http + encoding/json idioms:
//
//	json.NewDecoder(r.Body).Decode(&v)   -> request body (or a variable holding *json.Decoder)
//	json.Unmarshal(data, &v)             -> request body
//	json.NewEncoder(w).Encode(v)         -> response at the ambient code (or a variable holding *json.Encoder)
//	json.Marshal(v)                      -> response at the ambient code
//	w.WriteHeader(<constant code>)       -> sets the ambient code
//	http.Error(w, msg, <constant code>)  -> schema-less response at an explicit code
//
// It doubles as the base for future framework dialects (embed it, try
// the framework's own recognizers first, fall back to the embedded
// methods): the Decode/Encode/Unmarshal/Marshal shapes are
// encoding/json idioms, equally at home inside a Gin or Echo handler.
type netHTTPDialect struct{}

// decodeTarget recognizes "<x>.Decode(&v)" where x has type
// *encoding/json.Decoder, or "json.Unmarshal(data, &v)", and returns v
// (via addressedType) alongside the context it must be interpreted under.
func (netHTTPDialect) decodeTarget(call *ast.CallExpr, ctx *evalCtx) (ast.Expr, *evalCtx, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, nil, false
	}
	switch sel.Sel.Name {
	case "Decode":
		if len(call.Args) != 1 || !isJSONStreamType(sel.X, ctx.info, "Decoder") {
			return nil, nil, false
		}
		return addressedType(call.Args[0], ctx)
	case "Unmarshal":
		if len(call.Args) != 2 || !isPackageIdent(sel.X, ctx.info, "encoding/json") {
			return nil, nil, false
		}
		return addressedType(call.Args[1], ctx)
	}
	return nil, nil, false
}

// isNoBodyStatus reports whether an HTTP status must not carry a response
// body (per RFC 9110): 1xx informational, 204 No Content, and 304 Not
// Modified. A bare WriteHeader with such a status is a complete response.
func isNoBodyStatus(code int) bool {
	return code == 204 || code == 304 || (code >= 100 && code < 200)
}

// response recognizes WriteHeader (sets the ambient code, and records a
// bare no-body response when the status carries no body), http.Error
// (schema-less record at an explicit code, ambient unchanged — the
// terminal-write semantics of the real call), and Encode/Marshal
// (record at the ambient code). A shape that matches but can't be
// fully parsed (a non-constant or out-of-range status) returns
// ok=false, not a partial effect, so the walk still gets to try
// following the call as a helper.
func (netHTTPDialect) response(call *ast.CallExpr, ctx *evalCtx) (responseEffect, bool) {
	if code, ok := writeHeaderCode(call, ctx); ok {
		// A WriteHeader sets the ambient code for a following Encode. It
		// ALSO records a response on its own when the status carries no
		// body by HTTP semantics (204, 304, 1xx): there, the bare
		// WriteHeader IS the complete response (w.WriteHeader(
		// http.StatusNoContent) alone responds 204), and there's no Encode
		// to record it otherwise. For a body-bearing status (200, 201, 4xx,
		// …) the bare WriteHeader stays ambient-only, so a matching Encode
		// supplies the schema and an error-branch WriteHeader without a body
		// write isn't documented from the status alone.
		eff := responseEffect{ambient: code}
		if isNoBodyStatus(code) {
			eff.record = true
			eff.code = code
		}
		return eff, true
	}
	if code, ok := httpErrorCode(call, ctx); ok {
		return responseEffect{record: true, code: code}, true
	}
	if expr, exprCtx, ok := encodeCallArg(call, ctx); ok {
		return responseEffect{record: true, value: expr, valueCtx: exprCtx}, true
	}
	return responseEffect{}, false
}

// encodeCallArg recognizes "<x>.Encode(v)" where x has type
// *encoding/json.Encoder, or "json.Marshal(v)", and returns v
// unresolved — valueSchema is the sole place that calls resolveExpr, so
// there's exactly one resolver of record for every path into it.
func encodeCallArg(call *ast.CallExpr, ctx *evalCtx) (ast.Expr, *evalCtx, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, nil, false
	}
	switch sel.Sel.Name {
	case "Encode":
		if len(call.Args) != 1 || !isJSONStreamType(sel.X, ctx.info, "Encoder") {
			return nil, nil, false
		}
	case "Marshal":
		if len(call.Args) != 1 || !isPackageIdent(sel.X, ctx.info, "encoding/json") {
			return nil, nil, false
		}
	default:
		return nil, nil, false
	}
	return call.Args[0], ctx, true
}

// writeHeaderCode recognizes "<x>.WriteHeader(<constant code>)" where x
// has type net/http.ResponseWriter, and returns the constant code.
func writeHeaderCode(call *ast.CallExpr, ctx *evalCtx) (int, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "WriteHeader" || len(call.Args) != 1 {
		return 0, false
	}
	if !isHTTPResponseWriter(sel.X, ctx.info) {
		return 0, false
	}
	return statusIntArg(call.Args[0], ctx)
}

// httpErrorCode recognizes "http.Error(w, msg, <constant code>)" and
// returns the constant code.
func httpErrorCode(call *ast.CallExpr, ctx *evalCtx) (int, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Error" || len(call.Args) != 3 {
		return 0, false
	}
	if !isPackageIdent(sel.X, ctx.info, "net/http") {
		return 0, false
	}
	return statusIntArg(call.Args[2], ctx)
}

// statusIntArg evaluates e as a compile-time constant HTTP status,
// declining anything outside [100, 599] — net/http itself panics on
// such codes at runtime, and responseEffect reserves 0 as its
// "ambient" sentinel, so a pathological constant must never leak
// through as a real status.
func statusIntArg(e ast.Expr, ctx *evalCtx) (int, bool) {
	code, ok := constIntArg(e, ctx)
	if !ok || code < 100 || code > 599 {
		return 0, false
	}
	return code, true
}

// isHTTPResponseWriter reports whether x's static type is
// net/http.ResponseWriter. x is always the currently-walked call's own
// receiver expression, never something resolved through ctx.subst (only
// value arguments are substituted, never a selector's receiver), so this
// takes info directly rather than resolving through ctx.
func isHTTPResponseWriter(x ast.Expr, info *types.Info) bool {
	t := info.TypeOf(x)
	return t != nil && t.String() == "net/http.ResponseWriter"
}

// isJSONStreamType reports whether x's static type is *encoding/json.Decoder
// or *encoding/json.Encoder (matching typeName), regardless of whether x is
// the literal "json.NewDecoder(...)" call or a variable holding the result.
func isJSONStreamType(x ast.Expr, info *types.Info, typeName string) bool {
	t := info.TypeOf(x)
	if t == nil {
		return false
	}
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "encoding/json" && obj.Name() == typeName
}
