package inference

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"net/http"
	"strconv"

	"github.com/pabloos/gota/pkg/model"
)

// DetectBody is a best-effort heuristic that inspects decl's body for
// common net/http + encoding/json idioms and, when found, augments op
// with a requestBody and/or response schemas. It recognizes:
//
//	json.NewDecoder(r.Body).Decode(&x)   // or a variable holding *json.Decoder
//	json.Unmarshal(data, &x)
//	json.NewEncoder(w).Encode(x)         // or a variable holding *json.Encoder
//	json.Marshal(x)
//	w.WriteHeader(<constant code>)       // tracked per branch, see below
//	http.Error(w, msg, <constant code>)
//
// The detected schema is a bare "$ref: '#/components/schemas/<Name>'" (or
// an array of one) using the same convention a hand-written "gota:"
// comment would use, so the ResolveSchemaRefs pass expands it into a real
// component the same way regardless of which source produced it.
//
// Response detection walks the body respecting if/else branch boundaries:
// the status code in effect for a given Encode/Marshal/http.Error call is
// whatever the *nearest enclosing branch* last set via WriteHeader (or 200,
// Go's implicit default, if none did) — a WriteHeader in one branch never
// leaks into a sibling branch. Distinct status codes coexist as separate
// responses (e.g. a 200 success path and a 404 error path both get
// documented); if the *same* code is produced more than once, the last
// occurrence in source order wins.
//
// This is not a general dataflow analysis: it doesn't follow decode/encode
// or WriteHeader calls wrapped in helper functions, and a WriteHeader call
// whose argument isn't a compile-time constant is ignored (the code in
// effect is left unchanged) rather than guessed. Detecting nothing is
// silent, not an error: a "gota:" comment remains the reliable, explicit
// path, and always wins over whatever this infers.
func DetectBody(op *model.Operation, decl *ast.FuncDecl, info *types.Info) {
	if op == nil || decl == nil || decl.Body == nil || info == nil {
		return
	}

	if schema, ok := firstBodySchema(decl.Body, info, decodeCallType); ok {
		op.RequestBody = &model.RequestBody{
			Required: true,
			Content:  map[string]model.MediaType{"application/json": {Schema: schema}},
		}
	}

	rc := &responseCollector{responses: map[string]model.Response{}}
	rc.walk(decl.Body.List, http.StatusOK, info)
	if len(rc.responses) > 0 {
		if op.Responses == nil {
			op.Responses = map[string]model.Response{}
		}
		for code, resp := range rc.responses {
			op.Responses[code] = resp
		}
	}
}

// responseCollector accumulates one Response per distinct status code
// found while walking a handler body.
type responseCollector struct {
	responses map[string]model.Response
}

// walk processes stmts in source order, threading the status code in
// effect (code) through sibling statements. Recursing into a nested block
// (if/else branch, for/switch body) passes a copy of code as that block's
// starting point; changes made inside never propagate back to the caller,
// which is what keeps sibling branches from contaminating each other.
func (rc *responseCollector) walk(stmts []ast.Stmt, code int, info *types.Info) {
	for _, stmt := range stmts {
		code = rc.walkStmt(stmt, code, info)
	}
}

// walkStmt processes one statement and returns the status code in effect
// for the *next sibling* statement in the same block.
func (rc *responseCollector) walkStmt(stmt ast.Stmt, code int, info *types.Info) int {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if call, ok := s.X.(*ast.CallExpr); ok {
			code = rc.walkCall(call, code, info)
		}
	case *ast.AssignStmt:
		for _, rhs := range s.Rhs {
			if call, ok := rhs.(*ast.CallExpr); ok {
				code = rc.walkCall(call, code, info)
			}
		}
	case *ast.BlockStmt:
		rc.walk(s.List, code, info)
	case *ast.IfStmt:
		rc.walk(s.Body.List, code, info)
		if s.Else != nil {
			rc.walkStmt(s.Else, code, info)
		}
	case *ast.ForStmt:
		if s.Body != nil {
			rc.walk(s.Body.List, code, info)
		}
	case *ast.RangeStmt:
		if s.Body != nil {
			rc.walk(s.Body.List, code, info)
		}
	case *ast.SwitchStmt:
		for _, clause := range s.Body.List {
			if cc, ok := clause.(*ast.CaseClause); ok {
				rc.walk(cc.Body, code, info)
			}
		}
	}
	return code
}

// walkCall inspects a single call expression: WriteHeader updates the
// code in effect for subsequent statements in the same block; Encode,
// Marshal and http.Error record a response at the code currently in
// effect (http.Error carries its own explicit code and doesn't change
// what's "in effect" afterward).
func (rc *responseCollector) walkCall(call *ast.CallExpr, code int, info *types.Info) int {
	if newCode, ok := writeHeaderCode(call, info); ok {
		return newCode
	}
	if errCode, ok := httpErrorCode(call, info); ok {
		rc.record(errCode, nil)
		return code
	}
	if t, ok := encodeCallType(call, info); ok {
		if schema, ok := shallowRefSchema(t); ok {
			rc.record(code, schema)
		}
	}
	return code
}

// record stores (or overwrites, if code was already seen — last one in
// source order wins) the response for a status code.
func (rc *responseCollector) record(code int, schema *model.Schema) {
	desc := http.StatusText(code)
	if desc == "" {
		desc = "Response"
	}
	resp := model.Response{Description: desc}
	if schema != nil {
		resp.Content = map[string]model.MediaType{"application/json": {Schema: schema}}
	}
	rc.responses[strconv.Itoa(code)] = resp
}

// extractCallType recognizes a call expression's shape and, if it matches,
// returns the go/types.Type of the value being decoded/encoded.
type extractCallType func(*ast.CallExpr, *types.Info) (types.Type, bool)

// firstBodySchema returns the schema for the first call in body that
// extract recognizes and whose type converts via shallowRefSchema.
func firstBodySchema(body ast.Node, info *types.Info, extract extractCallType) (*model.Schema, bool) {
	var result *model.Schema
	ast.Inspect(body, func(n ast.Node) bool {
		if result != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if t, ok := extract(call, info); ok {
			if schema, ok := shallowRefSchema(t); ok {
				result = schema
				return false
			}
		}
		return true
	})
	return result, result != nil
}

// decodeCallType recognizes "<x>.Decode(&v)" where x has type
// *encoding/json.Decoder, or "json.Unmarshal(data, &v)", and returns v's type.
func decodeCallType(call *ast.CallExpr, info *types.Info) (types.Type, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	switch sel.Sel.Name {
	case "Decode":
		if len(call.Args) != 1 || !isJSONStreamType(sel.X, info, "Decoder") {
			return nil, false
		}
		return addressedType(call.Args[0], info)
	case "Unmarshal":
		if len(call.Args) != 2 || !isPackageIdent(sel.X, info, "encoding/json") {
			return nil, false
		}
		return addressedType(call.Args[1], info)
	}
	return nil, false
}

// encodeCallType recognizes "<x>.Encode(v)" where x has type
// *encoding/json.Encoder, or "json.Marshal(v)", and returns v's type.
func encodeCallType(call *ast.CallExpr, info *types.Info) (types.Type, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	switch sel.Sel.Name {
	case "Encode":
		if len(call.Args) != 1 || !isJSONStreamType(sel.X, info, "Encoder") {
			return nil, false
		}
	case "Marshal":
		if len(call.Args) != 1 || !isPackageIdent(sel.X, info, "encoding/json") {
			return nil, false
		}
	default:
		return nil, false
	}
	t := info.TypeOf(call.Args[0])
	return t, t != nil
}

// writeHeaderCode recognizes "<x>.WriteHeader(<constant code>)" where x
// has type net/http.ResponseWriter, and returns the constant code.
func writeHeaderCode(call *ast.CallExpr, info *types.Info) (int, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "WriteHeader" || len(call.Args) != 1 {
		return 0, false
	}
	if !isHTTPResponseWriter(sel.X, info) {
		return 0, false
	}
	return constIntArg(call.Args[0], info)
}

// httpErrorCode recognizes "http.Error(w, msg, <constant code>)" and
// returns the constant code.
func httpErrorCode(call *ast.CallExpr, info *types.Info) (int, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Error" || len(call.Args) != 3 {
		return 0, false
	}
	if !isPackageIdent(sel.X, info, "net/http") {
		return 0, false
	}
	return constIntArg(call.Args[2], info)
}

// isHTTPResponseWriter reports whether x's static type is
// net/http.ResponseWriter.
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

// isPackageIdent reports whether x is a reference to the package at path
// itself (as in "json.Unmarshal" or "http.Error").
func isPackageIdent(x ast.Expr, info *types.Info, path string) bool {
	ident, ok := x.(*ast.Ident)
	if !ok {
		return false
	}
	pn, ok := info.Uses[ident].(*types.PkgName)
	return ok && pn.Imported().Path() == path
}

// addressedType returns the type of v in a "&v" expression.
func addressedType(e ast.Expr, info *types.Info) (types.Type, bool) {
	unary, ok := e.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return nil, false
	}
	t := info.TypeOf(unary.X)
	return t, t != nil
}

// constIntArg evaluates e as a compile-time integer constant (a literal
// like 404 or a named constant like http.StatusNotFound), returning false
// for anything computed at runtime.
func constIntArg(e ast.Expr, info *types.Info) (int, bool) {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil {
		return 0, false
	}
	n, ok := constant.Int64Val(tv.Value)
	if !ok {
		return 0, false
	}
	return int(n), true
}

// shallowRefSchema converts t into a Schema that references named struct
// types by $ref (and arrays of them), without expanding their fields — the
// separate ResolveSchemaRefs pass does that expansion once, however the
// $ref was produced (a "gota:" comment or this detector). Anything else
// (maps, interfaces, bare basic types with no struct shape) isn't
// representable this way and reports false.
func shallowRefSchema(t types.Type) (*model.Schema, bool) {
	switch tt := t.(type) {
	case *types.Pointer:
		return shallowRefSchema(tt.Elem())
	case *types.Named:
		if _, isStruct := tt.Underlying().(*types.Struct); !isStruct {
			return nil, false
		}
		return &model.Schema{Ref: schemaRefPrefix + tt.Obj().Name()}, true
	case *types.Slice:
		item, ok := shallowRefSchema(tt.Elem())
		if !ok {
			return nil, false
		}
		return &model.Schema{Type: "array", Items: item}, true
	case *types.Array:
		item, ok := shallowRefSchema(tt.Elem())
		if !ok {
			return nil, false
		}
		return &model.Schema{Type: "array", Items: item}, true
	}
	return nil, false
}
