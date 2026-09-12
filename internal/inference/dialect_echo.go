package inference

import (
	"go/ast"
	"go/types"
)

const echoPkgPath = "github.com/labstack/echo/v4"

// echoDialect recognizes github.com/labstack/echo/v4's request/response
// idioms, embedding netHTTPDialect so the ordinary encoding/json shapes
// still work inside an Echo handler (its handlers can reach the raw
// *http.Request via c.Request()). Echo's own idioms are tried first,
// falling back to the embedded net/http ones:
//
//	c.JSON(201, obj)             -> response at an explicit code (also JSONPretty)
//	c.NoContent(204)             -> a bodyless response at an explicit code
//	c.Bind(&v)                   -> request body
//
// c.String/c.XML/c.HTML/c.Blob are deliberately not recognized (v1):
// record() only emits application/json, so recording them would mislabel
// the media type or carry no schema. A "gota:" comment covers those.
type echoDialect struct{ netHTTPDialect }

// Echo returns the dialect recognizing labstack/echo idioms. Pair it with
// the echo router plugin (internal/router/echo) at the generate.Options
// level. See echoDialect for the exact shapes.
func Echo() Dialect { return echoDialect{} }

// response recognizes Echo's JSON-family and NoContent response calls. A
// matched shape whose status isn't a constant in range returns ok=false
// (not a partial effect), same posture as netHTTPDialect, so the walk can
// still try following the call. Anything unrecognized falls back to the
// embedded net/http dialect (json.Encode/Marshal inside an Echo handler).
func (d echoDialect) response(call *ast.CallExpr, ctx *evalCtx) (responseEffect, bool) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && isEchoContext(sel.X, ctx.info) {
		switch {
		case sel.Sel.Name == "JSON" && len(call.Args) == 2,
			sel.Sel.Name == "JSONPretty" && len(call.Args) == 3:
			if code, ok := statusIntArg(call.Args[0], ctx); ok {
				return responseEffect{record: true, code: code, value: call.Args[1], valueCtx: ctx}, true
			}
			return responseEffect{}, false
		case sel.Sel.Name == "NoContent" && len(call.Args) == 1:
			// A bodyless response at an explicit code — the value is
			// deliberately nil (no content), like a net/http no-body status.
			if code, ok := statusIntArg(call.Args[0], ctx); ok {
				return responseEffect{record: true, code: code}, true
			}
			return responseEffect{}, false
		}
	}
	return d.netHTTPDialect.response(call, ctx)
}

// decodeTarget recognizes Echo's request-binding call "c.Bind(&v)" and
// returns v (via addressedType), falling back to the embedded net/http
// dialect (json.Decode/Unmarshal) for anything else.
func (d echoDialect) decodeTarget(call *ast.CallExpr, ctx *evalCtx) (ast.Expr, *evalCtx, bool) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && len(call.Args) == 1 &&
		sel.Sel.Name == "Bind" && isEchoContext(sel.X, ctx.info) {
		return addressedType(call.Args[0], ctx)
	}
	return d.netHTTPDialect.decodeTarget(call, ctx)
}

// isEchoContext reports whether x's static type is echo.Context — the
// receiver of every recognized Echo method call. Unlike *gin.Context,
// echo.Context is an interface, so it's used by value (no pointer).
func isEchoContext(x ast.Expr, info *types.Info) bool {
	t := info.TypeOf(x)
	if t == nil {
		return false
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == echoPkgPath && obj.Name() == "Context"
}
