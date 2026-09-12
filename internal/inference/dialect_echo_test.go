package inference

import (
	"testing"

	"github.com/pabloos/gota/internal/astutil"
	"github.com/pabloos/gota/pkg/model"
)

// TestDetectBody_Echo drives the real echo dialect against real echo
// handlers (fixture body_echo, a nested go.mod module requiring echo), so
// the c.JSON/c.Bind/c.NoContent recognizers and the embedded net/http
// fallback are exercised end-to-end, not via a stand-in.
func TestDetectBody_Echo(t *testing.T) {
	pkgs := loadFixture(t, "body_echo")
	funcIndex := astutil.IndexFuncDecls(pkgs)

	t.Run("c.Bind is the request body and c.JSON(201, ...) the response", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "CreateUser")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, Echo(), nil)

		if op.RequestBody == nil {
			t.Fatal("RequestBody not detected from c.Bind(&req)")
		}
		if schema := op.RequestBody.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("RequestBody schema = %+v, want $ref to User", schema)
		}
		resp, ok := op.Responses["201"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 201 (c.JSON's explicit code)", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("201 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("c.NoContent(204) is a bodyless response", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "DeleteUser")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, Echo(), nil)

		resp, ok := op.Responses["204"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 204 (c.NoContent's code)", op.Responses)
		}
		if resp.Content != nil {
			t.Errorf("204 response = %+v, want no content (bodyless)", resp)
		}
	})

	t.Run("the embedded net/http dialect still recognizes json.Decode on c.Request().Body", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "DecodeViaStdlib")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, Echo(), nil)

		if op.RequestBody == nil {
			t.Fatal("RequestBody not detected — embedded netHTTPDialect must fire inside an echo handler")
		}
		if schema := op.RequestBody.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("RequestBody schema = %+v, want $ref to User", schema)
		}
	})
}
