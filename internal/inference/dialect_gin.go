package inference

import (
	"go/ast"
	"go/types"
)

const ginPkgPath = "github.com/gin-gonic/gin"

// ginDialect recognizes github.com/gin-gonic/gin's request/response
// idioms, embedding netHTTPDialect so the ordinary encoding/json shapes
// (json.NewDecoder(c.Request.Body).Decode(&x), json.Marshal(v)) still
// work inside a Gin handler — c.Request is a plain *http.Request. Gin's
// own idioms are tried first, falling back to the embedded net/http
// ones:
//
//	c.JSON(201, obj)              -> response at an explicit code (also IndentedJSON/PureJSON/AsciiJSON/AbortWithStatusJSON)
//	c.ShouldBindJSON(&v)         -> request body (also BindJSON/ShouldBind/Bind)
//
// c.XML/c.String/c.Data are deliberately not recognized (v1): record()
// only emits application/json, so recording a c.XML response would
// mislabel it, and c.String/c.Data carry no schema. A "gota:" comment
// remains the explicit path for those.
type ginDialect struct{ netHTTPDialect }

// Gin returns the dialect recognizing gin-gonic/gin idioms. Pair it with
// the gin router plugin (internal/router/gin) at the generate.Options
// level. See ginDialect for the exact shapes.
func Gin() Dialect { return ginDialect{} }

// response recognizes Gin's JSON-family response calls
// "c.<M>(<constant code>, obj)". A non-constant or out-of-range status
// returns ok=false (not a partial effect), same posture as
// netHTTPDialect, so the walk can still try following the call. Anything
// unrecognized falls back to the embedded net/http dialect (json.Encode/
// Marshal inside a Gin handler).
func (d ginDialect) response(call *ast.CallExpr, ctx *evalCtx) (responseEffect, bool) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && len(call.Args) == 2 && isGinContext(sel.X, ctx.info) {
		switch sel.Sel.Name {
		case "JSON", "IndentedJSON", "PureJSON", "AsciiJSON", "AbortWithStatusJSON":
			if code, ok := statusIntArg(call.Args[0], ctx); ok {
				return responseEffect{record: true, code: code, value: call.Args[1], valueCtx: ctx}, true
			}
			return responseEffect{}, false
		}
	}
	return d.netHTTPDialect.response(call, ctx)
}

// decodeTarget recognizes Gin's request-binding calls "c.<M>(&v)" and
// returns v (via addressedType), falling back to the embedded net/http
// dialect (json.Decode/Unmarshal) for anything else.
func (d ginDialect) decodeTarget(call *ast.CallExpr, ctx *evalCtx) (ast.Expr, *evalCtx, bool) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && len(call.Args) == 1 && isGinContext(sel.X, ctx.info) {
		switch sel.Sel.Name {
		case "ShouldBindJSON", "BindJSON", "ShouldBind", "Bind":
			return addressedType(call.Args[0], ctx)
		}
	}
	return d.netHTTPDialect.decodeTarget(call, ctx)
}

// isGinContext reports whether x's static type is *gin.Context — the
// receiver of every recognized Gin method call. Mirrors isJSONStreamType.
func isGinContext(x ast.Expr, info *types.Info) bool {
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
	return obj.Pkg() != nil && obj.Pkg().Path() == ginPkgPath && obj.Name() == "Context"
}
