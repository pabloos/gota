package inference

import (
	"go/ast"
	"go/types"
	"testing"

	"github.com/pabloos/gota/internal/astutil"
	"github.com/pabloos/gota/pkg/model"
)

// fakeDialect recognizes the Gin-shaped `c.JSON(code, obj)` call from
// the body_dialect fixture, exactly the way a real framework dialect
// would: embedding netHTTPDialect as the fallback (the encoding/json
// recognizers stay valid inside any framework's handlers) and trying
// its own recognizer first. It exists to prove the seam carries a
// combined explicit-status+payload effect — the shape the old
// four-recognizer split couldn't express at all.
type fakeDialect struct{ netHTTPDialect }

func (d fakeDialect) response(call *ast.CallExpr, ctx *evalCtx) (responseEffect, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if ok && sel.Sel.Name == "JSON" && len(call.Args) == 2 && isFakeCtx(sel.X, ctx.info) {
		code, ok := statusIntArg(call.Args[0], ctx)
		if !ok {
			return responseEffect{}, false
		}
		return responseEffect{record: true, code: code, value: call.Args[1], valueCtx: ctx}, true
	}
	return d.netHTTPDialect.response(call, ctx)
}

// isFakeCtx reports whether x's static type is *fakeCtx — the same
// pointer-to-named-type check a real dialect does against
// *gin.Context, minus the package-path comparison a local fixture type
// can't have.
func isFakeCtx(x ast.Expr, info *types.Info) bool {
	ptr, ok := info.TypeOf(x).(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	return ok && named.Obj().Name() == "fakeCtx"
}

func TestDetectBody_Dialect(t *testing.T) {
	pkgs := loadFixture(t, "body_dialect")
	funcIndex := astutil.IndexFuncDecls(pkgs)

	t.Run("a combined status+payload call records one response with both", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "Create")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, fakeDialect{}, nil)
		if len(op.Responses) != 1 {
			t.Fatalf("Responses = %+v, want exactly one 201", op.Responses)
		}
		resp, ok := op.Responses["201"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 201", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("201 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("dialect recognition fires inside a followed helper frame", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "CreateViaHelper")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, fakeDialect{}, nil)
		resp, ok := op.Responses["201"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 201 (respond's code and data params must resolve back to the call site, and the dialect must ride along on the followed frame's context)", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("201 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("the net/http dialect sees nothing in a framework-shaped handler", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "Create")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if len(op.Responses) != 0 {
			t.Errorf("Responses = %+v, want none — c.JSON isn't a net/http idiom, and JSON's own body is empty so following finds nothing either", op.Responses)
		}
	})

	t.Run("a nil dialect disables detection entirely, not a panic", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "Create")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, nil, nil)
		if op.RequestBody != nil || len(op.Responses) != 0 {
			t.Errorf("op = %+v, want untouched", op)
		}
	})
}
