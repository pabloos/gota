package inference

import (
	"testing"

	"github.com/pabloos/gota/internal/astutil"
	"github.com/pabloos/gota/pkg/model"
)

// TestDetectBody_Gin drives the real gin dialect against real gin
// handlers (fixture body_gin, a nested go.mod module requiring gin), so
// the c.JSON/c.ShouldBindJSON recognizers, the gin.H envelope, and the
// embedded net/http fallback are all exercised end-to-end, not via a
// stand-in.
func TestDetectBody_Gin(t *testing.T) {
	pkgs := loadFixture(t, "body_gin")
	funcIndex := astutil.IndexFuncDecls(pkgs)

	t.Run("c.ShouldBindJSON is the request body and c.JSON(201, ...) the response", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "CreateUser")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, Gin(), nil)

		if op.RequestBody == nil {
			t.Fatal("RequestBody not detected from c.ShouldBindJSON(&req)")
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

	t.Run("a gin.H envelope builds an inline object schema from its keys", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "GetUserEnvelope")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, Gin(), nil)

		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatal("200 response not detected")
		}
		schema := resp.Content["application/json"].Schema
		if schema.Type != "object" {
			t.Fatalf("response schema = %+v, want an inline object (gin.H is a named map type)", schema)
		}
		if status := schema.Properties["status"]; status == nil || status.Type != "string" {
			t.Errorf("status property = %+v, want inline {type: string}", status)
		}
		if data := schema.Properties["data"]; data == nil || data.Ref != schemaRefPrefix+"User" {
			t.Errorf("data property = %+v, want $ref to User", data)
		}
	})

	t.Run("the embedded net/http dialect still recognizes json.Decode on c.Request.Body", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "DecodeViaStdlib")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, Gin(), nil)

		if op.RequestBody == nil {
			t.Fatal("RequestBody not detected — embedded netHTTPDialect must fire inside a gin handler")
		}
		if schema := op.RequestBody.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("RequestBody schema = %+v, want $ref to User", schema)
		}
	})
}
