package inference

import (
	"testing"

	"github.com/pabloos/gota/internal/astutil"
	"github.com/pabloos/gota/pkg/model"
)

// TestDetectBody_Fiber drives the real fiber dialect against real fiber
// handlers (fixture body_fiber, a nested go.mod module requiring fiber), so
// the c.BodyParser / c.Status(code).JSON / c.JSON / c.SendStatus recognizers
// and the embedded net/http fallback are exercised end-to-end.
func TestDetectBody_Fiber(t *testing.T) {
	pkgs := loadFixture(t, "body_fiber")
	funcIndex := astutil.IndexFuncDecls(pkgs)

	t.Run("c.BodyParser is the request body and c.Status(201).JSON the response", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "CreateUser")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, Fiber(), nil)

		if op.RequestBody == nil {
			t.Fatal("RequestBody not detected from c.BodyParser(&req)")
		}
		if schema := op.RequestBody.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("RequestBody schema = %+v, want $ref to User", schema)
		}
		resp, ok := op.Responses["201"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 201 (from the chained c.Status(201))", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("201 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("c.JSON alone responds at the default 200 status", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "GetUser")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, Fiber(), nil)

		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 200", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("200 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("c.SendStatus(204) is a bodyless response", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "DeleteUser")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, Fiber(), nil)

		resp, ok := op.Responses["204"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 204 (from c.SendStatus)", op.Responses)
		}
		if resp.Content != nil {
			t.Errorf("204 response = %+v, want no content (bodyless)", resp)
		}
	})

	t.Run("the embedded net/http dialect still recognizes json.Marshal", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "MarshalViaStdlib")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, Fiber(), nil)

		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 200 from json.Marshal", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("200 response schema = %+v, want $ref to User (embedded netHTTP dialect)", schema)
		}
	})
}
