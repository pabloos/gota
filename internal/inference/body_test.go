package inference

import (
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/internal/astutil"
	"github.com/pabloos/gota/pkg/model"
)

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

// commentMapFor builds the ast.CommentMap for the file containing funcName,
// mirroring what internal/generate does per-file in the real pipeline.
func commentMapFor(t *testing.T, pkgs []*packages.Package, funcName string) ast.CommentMap {
	t.Helper()
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == funcName {
					return ast.NewCommentMap(pkg.Fset, file, file.Comments)
				}
			}
		}
	}
	t.Fatalf("function %s not found in fixture", funcName)
	return nil
}

func TestDetectBody(t *testing.T) {
	pkgs := loadFixture(t, "body")
	funcIndex := astutil.IndexFuncDecls(pkgs)

	t.Run("Decode via the json.NewDecoder(...).Decode(&x) chain", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "DecodeDirect")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
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
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
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
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
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
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatal("200 response not detected")
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("encoding a generic instantiation detects a $ref reflecting the instantiation", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "EncodeGenericResponse")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatal("200 response not detected")
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"Response_User" {
			t.Errorf("response schema = %+v, want $ref to Response_User (the instantiation), not the bare generic name Response", schema)
		}
	})

	t.Run("a handler factory's returned literal is followed for request and response", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "FactoryDirectHandler")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if op.RequestBody == nil {
			t.Fatal("RequestBody not detected from the returned literal's Decode(&req)")
		}
		if schema := op.RequestBody.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"CreateReq" {
			t.Errorf("RequestBody schema = %+v, want $ref to CreateReq", schema)
		}
		if _, ok := op.Responses["201"]; !ok {
			t.Fatalf("Responses = %+v, missing 201 (the literal's WriteHeader/Encode)", op.Responses)
		}
		if schema := op.Responses["201"].Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("201 schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("a middleware-wrapped factory: response from the literal, request from the generic middleware type arg", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "FactoryChainedHandler")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if op.RequestBody == nil {
			t.Fatal("RequestBody not detected from BindJSON[CreateReq] (the literal never decodes)")
		}
		if schema := op.RequestBody.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"CreateReq" {
			t.Errorf("RequestBody schema = %+v, want $ref to CreateReq from the generic middleware type arg", schema)
		}
		if _, ok := op.Responses["201"]; !ok {
			t.Fatalf("Responses = %+v, missing 201 (must unwrap the middleware chain to the literal)", op.Responses)
		}
		if schema := op.Responses["201"].Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("201 schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("a function-local response type is inlined as an object, not an unresolvable $ref", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "EncodeFunctionLocalType")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatal("200 response not detected")
		}
		schema := resp.Content["application/json"].Schema
		// A local type has no stable component name, so it's inlined — a
		// real object with its fields, not a bare {type: object} and not a
		// dangling $ref.
		if schema.Ref != "" || schema.Type != "object" {
			t.Fatalf("response schema = %+v, want an inline object (no $ref)", schema)
		}
		if success := schema.Properties["success"]; success == nil || success.Type != "boolean" {
			t.Errorf("response schema = %+v, want its fields inlined (success: boolean)", schema)
		}
	})

	t.Run("last of multiple Encode calls wins", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "EncodeErrorThenSuccess")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
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
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
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
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
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
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if op.RequestBody != nil || len(op.Responses) != 0 {
			t.Errorf("op = %+v, want untouched", op)
		}
	})

	t.Run("decoding a basic type produces an inline scalar schema", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "DecodeNonStruct")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if op.RequestBody == nil {
			t.Fatal("RequestBody not detected")
		}
		if schema := op.RequestBody.Content["application/json"].Schema; schema.Type != "string" {
			t.Errorf("RequestBody schema = %+v, want an inline {type: string}", schema)
		}
	})

	t.Run("nil decl/info is a no-op, not a panic", func(t *testing.T) {
		op := &model.Operation{}
		DetectBody(op, nil, nil, nil, nil, NetHTTP(), nil)
		if op.RequestBody != nil || len(op.Responses) != 0 {
			t.Errorf("op = %+v, want untouched", op)
		}
	})

	t.Run("WriteHeader before Encode uses the real status code, not 200", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "EncodeWithExplicitCode")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
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
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
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
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
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
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
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
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
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
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
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

	t.Run("a statement marked x-gota-skip is invisible to response detection", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "SkippedErrorBranch")
		cmap := commentMapFor(t, pkgs, "SkippedErrorBranch")
		op := &model.Operation{}
		DetectBody(op, decl, info, cmap, funcIndex, NetHTTP(), nil)
		if _, has418 := op.Responses["418"]; has418 {
			t.Errorf("Responses = %+v, the 418 branch is marked x-gota-skip and should not appear", op.Responses)
		}
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatalf("Responses = %+v, missing the 200 success path (should be unaffected by the skip)", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("200 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("x-gota-skip on the Encode statement itself suppresses that response even with an explicit code", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "SkippedEncodeWithCode")
		cmap := commentMapFor(t, pkgs, "SkippedEncodeWithCode")
		op := &model.Operation{}
		DetectBody(op, decl, info, cmap, funcIndex, NetHTTP(), nil)
		if len(op.Responses) != 0 {
			t.Errorf("Responses = %+v, want none (the only Encode call is marked x-gota-skip)", op.Responses)
		}
	})

	t.Run("a response written via a local helper is detected as if inlined", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "RespondViaHelper")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if _, has200 := op.Responses["200"]; has200 {
			t.Errorf("Responses = %+v, should not have a 200 (respond was called with http.StatusCreated)", op.Responses)
		}
		resp, ok := op.Responses["201"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 201 (respond(w, http.StatusCreated, User{...}) should be followed)", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("201 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("two calls to the same local helper in sibling branches produce two distinct responses", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "RespondViaHelperErrorThenSuccess")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if len(op.Responses) != 2 {
			t.Fatalf("Responses = %+v, want exactly 400 and 201", op.Responses)
		}
		bad, ok := op.Responses["400"]
		if !ok || bad.Content["application/json"].Schema.Ref != schemaRefPrefix+"StatusResponse" {
			t.Errorf("400 response = %+v, want $ref to StatusResponse", bad)
		}
		created, ok := op.Responses["201"]
		if !ok || created.Content["application/json"].Schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("201 response = %+v, want $ref to User", created)
		}
	})

	t.Run("a local helper called with nil data detects nothing, doesn't panic", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "RespondViaHelperNilData")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if len(op.Responses) != 0 {
			t.Errorf("Responses = %+v, want none — detection doesn't evaluate respond's \"if data != nil\" condition, it statically resolves data to a nil literal, which shallowRefSchema correctly declines the same as a direct Encode(nil) would, registering nothing (matching NoPattern's bare-WriteHeader precedent)", op.Responses)
		}
	})

	t.Run("a helper calling another helper is followed two levels deep", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "RespondViaTwoHelpers")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		resp, ok := op.Responses["201"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 201 — wrapRespond -> respond is two levels, well within maxFollowDepth", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("201 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("following stops at maxFollowDepth: a chain one level too deep detects nothing", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "RespondViaTooManyHelpers")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if len(op.Responses) != 0 {
			t.Errorf("Responses = %+v, want none — wrapRespond4 -> wrapRespond3 -> wrapRespond2 -> wrapRespond -> respond is five levels, one past maxFollowDepth", op.Responses)
		}
	})

	t.Run("a mutually-recursive helper chain is declined via cycle detection, not an infinite loop", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "RespondViaCycle")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if len(op.Responses) != 0 {
			t.Errorf("Responses = %+v, want none — cycleA/cycleB never reach a real WriteHeader/Encode call", op.Responses)
		}
	})

	t.Run("a request body decoded via a local helper is detected as if inlined", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "DecodeViaHelper")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if op.RequestBody == nil {
			t.Fatal("RequestBody not detected")
		}
		if schema := op.RequestBody.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("RequestBody schema = %+v, want $ref to User (decodeJSON's own \"v\" parameter must resolve to the call site's \"&u\" before the address-of check)", schema)
		}
	})

	t.Run("an Encode in return position inside a followed helper is detected", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "RespondViaReturnHelper")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		resp, ok := op.Responses["400"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 400 (respondErr's WriteHeader(400) then return-position Encode)", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"ErrorResponse" {
			t.Errorf("400 response schema = %+v, want $ref to ErrorResponse", schema)
		}
	})

	t.Run("a helper call in if-Init position is detected alongside the branch's own response", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "IfInitHelperCall")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if len(op.Responses) != 2 {
			t.Fatalf("Responses = %+v, want exactly 200 and 500", op.Responses)
		}
		if schema := op.Responses["200"].Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("200 response schema = %+v, want $ref to User (the if-Init call runs unconditionally)", schema)
		}
		if resp := op.Responses["500"]; resp.Content != nil {
			t.Errorf("500 response = %+v, want no content (http.Error)", resp)
		}
	})

	t.Run("a helper call in switch-Init position is detected alongside case responses", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "SwitchInitHelperCall")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if len(op.Responses) != 2 {
			t.Fatalf("Responses = %+v, want exactly 200 and 500", op.Responses)
		}
		if schema := op.Responses["200"].Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("200 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("type-switch case bodies are walked like plain switch cases", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "TypeSwitchResponses")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		if len(op.Responses) != 2 {
			t.Fatalf("Responses = %+v, want exactly 200 and 400", op.Responses)
		}
		if schema := op.Responses["200"].Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("200 response schema = %+v, want $ref to User", schema)
		}
		if resp := op.Responses["400"]; resp.Content != nil {
			t.Errorf("400 response = %+v, want no content (http.Error)", resp)
		}
	})

	t.Run("a response written via a method helper on a local struct is followed like a function", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "CreateViaMethod")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		resp, ok := op.Responses["201"]
		if !ok {
			t.Fatalf("Responses = %+v, missing 201 — s.respond resolves through info.Uses like any function reference and its declaration is in funcIndex, so it must be followed", op.Responses)
		}
		if schema := resp.Content["application/json"].Schema; schema.Ref != schemaRefPrefix+"User" {
			t.Errorf("201 response schema = %+v, want $ref to User", schema)
		}
	})

	t.Run("a map literal envelope produces an inline object schema from its actual keys", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "EncodeMapLiteralResponse")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatal("200 response not detected")
		}
		schema := resp.Content["application/json"].Schema
		if schema.Type != "object" {
			t.Fatalf("response schema = %+v, want type object", schema)
		}
		status, ok := schema.Properties["status"]
		if !ok || status.Type != "string" {
			t.Errorf("status property = %+v, want inline {type: string}", status)
		}
		data, ok := schema.Properties["data"]
		if !ok || data.Ref != schemaRefPrefix+"User" {
			t.Errorf("data property = %+v, want $ref to User", data)
		}
		if !containsAll(schema.Required, "status", "data") {
			t.Errorf("Required = %+v, want both status and data (a map literal's keys are unconditionally present)", schema.Required)
		}
	})

	t.Run("a map literal with a dynamic key degrades to a bare object schema, not zero response", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "EncodeMapLiteralDynamicKey")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatal("200 response not detected -- a dynamic key can't be enumerated, but the map itself is still a real object")
		}
		schema := resp.Content["application/json"].Schema
		if schema.Type != "object" || len(schema.Properties) != 0 {
			t.Errorf("response schema = %+v, want a bare {type: object} with no properties", schema)
		}
	})

	t.Run("a named string-keyed map variable uses shallowRefSchema's Map case", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "EncodeNamedStringMap")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatal("200 response not detected")
		}
		schema := resp.Content["application/json"].Schema
		if schema.Type != "object" || schema.AdditionalProperties == nil || schema.AdditionalProperties.Ref != schemaRefPrefix+"User" {
			t.Errorf("response schema = %+v, want {type: object, additionalProperties: $ref User}", schema)
		}
	})

	t.Run("encoding a bare string produces an inline scalar schema", func(t *testing.T) {
		decl, info := findFunc(t, pkgs, "EncodeBareString")
		op := &model.Operation{}
		DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)
		resp, ok := op.Responses["200"]
		if !ok {
			t.Fatal("200 response not detected")
		}
		if schema := resp.Content["application/json"].Schema; schema.Type != "string" {
			t.Errorf("response schema = %+v, want an inline {type: string}", schema)
		}
	})
}

// TestDetectBody_CrossPackage exercises the real-world shape that
// motivated cross-package, multi-level following: handlers.CreateUser
// calls httputil.Success (a different package), which itself delegates
// to httputil.JSON (a second level) for the actual WriteHeader/Encode
// calls — and the envelope Success builds is a map literal referencing
// its own "data" parameter, which must resolve two frames back up to
// CreateUser's own "user" variable.
func TestDetectBody_CrossPackage(t *testing.T) {
	pkgs := loadFixture(t, "body_cross_package")
	funcIndex := astutil.IndexFuncDecls(pkgs)

	decl, info := findFunc(t, pkgs, "CreateUser")
	op := &model.Operation{}
	DetectBody(op, decl, info, nil, funcIndex, NetHTTP(), nil)

	resp, ok := op.Responses["201"]
	if !ok {
		t.Fatalf("Responses = %+v, missing 201 (httputil.Success -> httputil.JSON should be followed cross-package)", op.Responses)
	}
	schema := resp.Content["application/json"].Schema
	if schema.Type != "object" {
		t.Fatalf("response schema = %+v, want type object", schema)
	}
	status, ok := schema.Properties["status"]
	if !ok || status.Type != "string" {
		t.Errorf("status property = %+v, want inline {type: string}", status)
	}
	data, ok := schema.Properties["data"]
	if !ok || data.Ref != schemaRefPrefix+"User" {
		t.Errorf("data property = %+v, want $ref to User (resolved three frames up: JSON's payload -> Success's map literal -> Success's data param -> CreateUser's user variable)", data)
	}
}
