package inference

import (
	"go/ast"
	"go/types"
)

const fiberPkgPath = "github.com/gofiber/fiber/v2"

// fiberDialect recognizes github.com/gofiber/fiber/v2's request/response
// idioms, embedding netHTTPDialect so plain encoding/json shapes
// (json.Marshal(v), json.Unmarshal(c.Body(), &x)) still work. Fiber differs
// from Gin/Echo in that the status code is not an argument to the response
// call — it is set separately, and chained:
//
//	c.JSON(obj)                  -> response at the ambient status (default 200)
//	c.Status(201)                -> sets the ambient status for later writes
//	c.Status(201).JSON(obj)      -> response at 201 (status read off the chain)
//	c.SendStatus(204)            -> a bodyless response at an explicit code
//	c.BodyParser(&v)             -> request body
//
// c.SendString/c.Send/c.JSONP are not recognized (v1): they carry no JSON
// schema, or (JSONP) aren't application/json.
type fiberDialect struct{ netHTTPDialect }

// Fiber returns the dialect recognizing gofiber/fiber/v2 idioms. Pair it
// with the fiber router plugin (internal/router/fiber) at the
// generate.Options level. See fiberDialect for the exact shapes.
func Fiber() Dialect { return fiberDialect{} }

// response recognizes Fiber's response calls. c.JSON records at the ambient
// status unless the receiver is a chained c.Status(<const>), in which case
// that explicit code wins; a bare c.Status(<const>) sets the ambient status;
// c.SendStatus records a bodyless response. Anything unrecognized falls back
// to the embedded net/http dialect (json.Marshal inside a Fiber handler).
func (d fiberDialect) response(call *ast.CallExpr, ctx *evalCtx) (responseEffect, bool) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && isFiberCtx(sel.X, ctx.info) {
		switch sel.Sel.Name {
		case "JSON":
			if len(call.Args) >= 1 {
				eff := responseEffect{record: true, value: call.Args[0], valueCtx: ctx}
				if code, ok := fiberChainedStatus(sel.X, ctx); ok {
					eff.code = code
				}
				return eff, true
			}
		case "Status":
			// A bare c.Status(code) sets the ambient status for a following
			// write (a chained c.Status(code).JSON(...) is handled above,
			// through the JSON call's receiver, so this only fires when
			// Status is the statement's own outermost call).
			if len(call.Args) == 1 {
				if code, ok := statusIntArg(call.Args[0], ctx); ok {
					return responseEffect{ambient: code}, true
				}
				return responseEffect{}, false
			}
		case "SendStatus":
			if len(call.Args) == 1 {
				if code, ok := statusIntArg(call.Args[0], ctx); ok {
					return responseEffect{record: true, code: code}, true
				}
				return responseEffect{}, false
			}
		}
	}
	return d.netHTTPDialect.response(call, ctx)
}

// decodeTarget recognizes Fiber's request-binding call "c.BodyParser(&v)"
// and returns v (via addressedType), falling back to the embedded net/http
// dialect (json.Decode/Unmarshal) for anything else.
func (d fiberDialect) decodeTarget(call *ast.CallExpr, ctx *evalCtx) (ast.Expr, *evalCtx, bool) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && len(call.Args) == 1 &&
		sel.Sel.Name == "BodyParser" && isFiberCtx(sel.X, ctx.info) {
		return addressedType(call.Args[0], ctx)
	}
	return d.netHTTPDialect.decodeTarget(call, ctx)
}

// fiberChainedStatus reads the constant status from a "c.Status(<const>)"
// receiver of a chained call (c.Status(201).JSON(obj)), or ok=false when the
// receiver isn't such a chain.
func fiberChainedStatus(recv ast.Expr, ctx *evalCtx) (int, bool) {
	call, ok := recv.(*ast.CallExpr)
	if !ok {
		return 0, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Status" || len(call.Args) != 1 || !isFiberCtx(sel.X, ctx.info) {
		return 0, false
	}
	return statusIntArg(call.Args[0], ctx)
}

// isFiberCtx reports whether x's static type is *fiber.Ctx — the receiver of
// every recognized Fiber method call (a Fiber handler is func(*fiber.Ctx) error).
func isFiberCtx(x ast.Expr, info *types.Info) bool {
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
	return obj.Pkg() != nil && obj.Pkg().Path() == fiberPkgPath && obj.Name() == "Ctx"
}
