package inference

import (
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/pkg/model"
)

const bodyFixtureSrc = "package fixture\n\n" +
	"import (\n" +
	"\t\"encoding/json\"\n" +
	"\t\"net/http\"\n" +
	")\n\n" +
	"type User struct {\n" +
	"\tID   int    `json:\"id\"`\n" +
	"\tName string `json:\"name\"`\n" +
	"}\n\n" +
	"type ErrorResponse struct {\n" +
	"\tMessage string `json:\"message\"`\n" +
	"}\n\n" +
	"func DecodeDirect(w http.ResponseWriter, r *http.Request) {\n" +
	"\tvar u User\n" +
	"\tjson.NewDecoder(r.Body).Decode(&u)\n" +
	"}\n\n" +
	"func DecodeViaVariable(w http.ResponseWriter, r *http.Request) {\n" +
	"\tvar u User\n" +
	"\tdec := json.NewDecoder(r.Body)\n" +
	"\tdec.Decode(&u)\n" +
	"}\n\n" +
	"func UnmarshalCall(w http.ResponseWriter, r *http.Request) {\n" +
	"\tvar u User\n" +
	"\tdata := []byte(\"{}\")\n" +
	"\tjson.Unmarshal(data, &u)\n" +
	"}\n\n" +
	"func EncodeSingle(w http.ResponseWriter, r *http.Request) {\n" +
	"\tu := User{}\n" +
	"\tjson.NewEncoder(w).Encode(u)\n" +
	"}\n\n" +
	"func EncodeErrorThenSuccess(w http.ResponseWriter, r *http.Request) {\n" +
	"\tif r.Method != \"GET\" {\n" +
	"\t\tjson.NewEncoder(w).Encode(ErrorResponse{Message: \"bad\"})\n" +
	"\t\treturn\n" +
	"\t}\n" +
	"\tu := User{}\n" +
	"\tjson.NewEncoder(w).Encode(u)\n" +
	"}\n\n" +
	"func MarshalCall(w http.ResponseWriter, r *http.Request) {\n" +
	"\tu := User{}\n" +
	"\tb, _ := json.Marshal(u)\n" +
	"\tw.Write(b)\n" +
	"}\n\n" +
	"func EncodeSlice(w http.ResponseWriter, r *http.Request) {\n" +
	"\tusers := []User{}\n" +
	"\tjson.NewEncoder(w).Encode(users)\n" +
	"}\n\n" +
	"func NoPattern(w http.ResponseWriter, r *http.Request) {\n" +
	"\tw.WriteHeader(204)\n" +
	"}\n\n" +
	"func DecodeNonStruct(w http.ResponseWriter, r *http.Request) {\n" +
	"\tvar s string\n" +
	"\tjson.NewDecoder(r.Body).Decode(&s)\n" +
	"}\n\n" +
	"func EncodeWithExplicitCode(w http.ResponseWriter, r *http.Request) {\n" +
	"\tu := User{}\n" +
	"\tw.WriteHeader(http.StatusCreated)\n" +
	"\tjson.NewEncoder(w).Encode(u)\n" +
	"}\n\n" +
	"func TwoBranchesDifferentCodes(w http.ResponseWriter, r *http.Request) {\n" +
	"\tif r.Method != \"POST\" {\n" +
	"\t\tw.WriteHeader(http.StatusNotFound)\n" +
	"\t\tjson.NewEncoder(w).Encode(ErrorResponse{Message: \"not found\"})\n" +
	"\t\treturn\n" +
	"\t}\n" +
	"\tw.WriteHeader(http.StatusCreated)\n" +
	"\tjson.NewEncoder(w).Encode(User{})\n" +
	"}\n\n" +
	"func SiblingBranchNoLeak(w http.ResponseWriter, r *http.Request) {\n" +
	"\tif r.Method == \"DELETE\" {\n" +
	"\t\tw.WriteHeader(http.StatusNotFound)\n" +
	"\t\treturn\n" +
	"\t}\n" +
	"\tjson.NewEncoder(w).Encode(User{})\n" +
	"}\n\n" +
	"func HTTPErrorCall(w http.ResponseWriter, r *http.Request) {\n" +
	"\thttp.Error(w, \"not found\", http.StatusNotFound)\n" +
	"}\n\n" +
	"func MixedErrorAndSuccess(w http.ResponseWriter, r *http.Request) {\n" +
	"\tif r.Method != \"GET\" {\n" +
	"\t\thttp.Error(w, \"bad method\", http.StatusMethodNotAllowed)\n" +
	"\t\treturn\n" +
	"\t}\n" +
	"\tjson.NewEncoder(w).Encode(User{})\n" +
	"}\n\n" +
	"func computeCode() int { return 200 }\n\n" +
	"func WriteHeaderDynamicCode(w http.ResponseWriter, r *http.Request) {\n" +
	"\tcode := computeCode()\n" +
	"\tw.WriteHeader(code)\n" +
	"\tjson.NewEncoder(w).Encode(User{})\n" +
	"}\n"

// findFunc locates the *ast.FuncDecl named name across pkgs, returning it
// alongside the TypesInfo of the package that declared it.
func findFunc(t *testing.T, pkgs []*packages.Package, name string) (*ast.FuncDecl, *types.Info) {
	t.Helper()
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name {
					return fn, pkg.TypesInfo
				}
			}
		}
	}
	t.Fatalf("function %s not found in fixture", name)
	return nil, nil
}

func TestDetectBody(t *testing.T) {
	pkgs := loadFixture(t, bodyFixtureSrc)

	t.Run("Decode via the json.NewDecoder(...).Decode(&x) chain", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "DecodeDirect")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		if op.RequestBody == nil {
			t.Fatal("RequestBody not detected")
		}
		if schema := op.RequestBody.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("RequestBody schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("Decode via an intermediate *json.Decoder variable", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "DecodeViaVariable")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		if op.RequestBody == nil {
			t.Fatal("RequestBody not detected")
		}
		if schema := op.RequestBody.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("RequestBody schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("json.Unmarshal", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "UnmarshalCall")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		if op.RequestBody == nil {
			t.Fatal("RequestBody not detected")
		}
		if schema := op.RequestBody.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("RequestBody schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("single Encode call", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "EncodeSingle")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatal("200 response not detected")
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("last of multiple Encode calls wins", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "EncodeErrorThenSuccess")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatal("200 response not detected")
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("response schema = %+v, want the LAST Encode call (User), not the earlier error branch (ErrorResponse)", schema)
		}
	})

	t.Run("json.Marshal", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "MarshalCall")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatal("200 response not detected")
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("encoding a slice detects an array of $ref", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "EncodeSlice")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatal("200 response not detected")
		}
		schema := resp.Content["application/json"].Schema
		if schema.Type != "array" || schema.Items == nil || schema.Items.Ref != schemaRefPrefix+"User" {
			t.Errorf("response schema = %+v, want an array with items $ref User", schema)
		}
	})

	t.Run("no recognizable pattern leaves the operation untouched", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "NoPattern")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		if op.RequestBody != nil || len(op.Responses) != 0 {
			t.Errorf("op = %+v, want untouched", op)
		}
	})

	t.Run("decoding a non-struct type detects nothing, doesn't panic", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "DecodeNonStruct")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		if op.RequestBody != nil {
			t.Errorf("RequestBody = %+v, want nil (a bare string isn't $ref-able)", op.RequestBody)
		}
	})

	t.Run("nil decl/info is a no-op, not a panic", func(t *testing.T) {
		op := &model.Operation{}
		DetectBody(op, nil, nil)
		if op.RequestBody != nil || len(op.Responses) != 0 {
			t.Errorf("op = %+v, want untouched", op)
		}
	})

	t.Run("WriteHeader before Encode uses the real status code, not 200", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "EncodeWithExplicitCode")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		if _, has200 := op.Responses["200"]; has200 {
			t.Errorf("Responses = %+v, should not have a 200 (WriteHeader(201) was called)", op.Responses)
		}
		resp, ok := op.Responses["201"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 201", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("201 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("two branches with different explicit codes produce two distinct responses", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "TwoBranchesDifferentCodes")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		if len(op.Responses) != 2 {
			t.Fatalf("Responses = %+v, want exactly 404 and 201", op.Responses)
		}
		notFound, ok := op.Responses["404"]
		if !ok || notFound.Content["application/json"].Schema.Ref != schemaRefPrefix+"ErrorResponse" {
			t.Errorf("404 response = %+v, want $ref to ErrorResponse", notFound)
		}
		created, ok := op.Responses["201"]
		if !ok || created.Content["application/json"].Schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("201 response = %+v, want $ref to User", created)
		}
	})

	t.Run("a sibling branch's WriteHeader does not leak into this branch's implicit 200", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "SiblingBranchNoLeak")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		if _, has404 := op.Responses["404"]; has404 {
			t.Errorf("Responses = %+v, the 404 branch never calls Encode so it shouldn't register a response at all", op.Responses)
		}
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatalf("Responses = %+v, missing the implicit 200 for the Encode-only branch", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("200 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("http.Error registers a response with no schema", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "HTTPErrorCall")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		resp, ok := op.Responses["404"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 404", op.Responses)
		}
		if resp.Description != "Not Found" {
			t.Errorf("Description = %q, want %q", resp.Description, "Not Found")
		}
		if resp.Content != nil {
			t.Errorf("Content = %+v, want nil (http.Error carries no JSON schema)", resp.Content)
		}
	})

	t.Run("http.Error in one branch and Encode in another both survive", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "MixedErrorAndSuccess")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		if len(op.Responses) != 2 {
			t.Fatalf("Responses = %+v, want exactly 405 and 200", op.Responses)
		}
		if resp := op.Responses["405"]; resp.Content != nil {
			t.Errorf("405 response = %+v, want no content", resp)
		}
		if schema := op.Responses["200"].Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("200 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("a non-constant WriteHeader argument is ignored, falls back to the inherited code", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "WriteHeaderDynamicCode")
		op := &model.Operation{}
		DetectBody(op, decl, info)
		if len(op.Responses) != 1 {
			t.Fatalf("Responses = %+v, want exactly one entry", op.Responses)
		}
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatalf("Responses = %+v, want the dynamic WriteHeader to be ignored and fall back to 200", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("200 response schema = %+v, want $ref to User", schema)
		}
	})
}
