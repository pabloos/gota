package generate_test

import (
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/pabloos/gota/internal/emitter"
	"github.com/pabloos/gota/internal/generate"
	"github.com/pabloos/gota/internal/router"
	"github.com/pabloos/gota/internal/router/nethttp"
	"github.com/pabloos/gota/pkg/model"
)

func TestRun_NetHTTPBasic(t *testing.T) {
	dir, err := filepath.Abs("../../testdata/nethttp-basic")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Test API",
		Version: "1.0.0",
		Plugins: []router.Plugin{nethttp.New()},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v", err)
	}

	if doc.OpenAPI != "3.1.0" {
		t.Errorf("OpenAPI = %q, want 3.1.0", doc.OpenAPI)
	}
	if doc.Info.Title != "Test API" || doc.Info.Version != "1.0.0" {
		t.Errorf("Info = %+v", doc.Info)
	}
	// DebugInfo (GET /debug/info) declares "x-gota-skip: true" and must be
	// excluded entirely — not just empty, absent — so the path count stays
	// at 2 even though main.go registers a third route.
	if len(doc.Paths) != 2 {
		t.Fatalf("expected 2 paths (DebugInfo's x-gota-skip route should be excluded), got %d: %+v", len(doc.Paths), doc.Paths)
	}
	if _, ok := doc.Paths["/debug/info"]; ok {
		t.Errorf("doc.Paths contains /debug/info, want it excluded by x-gota-skip")
	}

	// GET /users/{id}: fully annotated via a "gota:" comment. The comment's
	// declared parameter type (integer) must win over the inferred default
	// (string), and the declared response set must replace the inferred one.
	usersByID, ok := doc.Paths["/users/{id}"]
	if !ok {
		t.Fatalf("missing /users/{id}")
	}
	assertGetUser(t, usersByID.Get)
	assertDeleteUser(t, usersByID.Delete)

	// GET /users, POST /users
	users, ok := doc.Paths["/users"]
	if !ok {
		t.Fatalf("missing /users")
	}
	assertListUsers(t, users.Get)
	assertCreateUser(t, users.Post)

	assertUserComponent(t, doc)
}

// assertUserComponent checks that the $ref declared in GetUser/CreateUser's
// "gota:" comments resolved to a real components.schemas.User entry
// generated from the testdata/nethttp-basic User struct — not just parsed
// as an opaque string.
func assertUserComponent(t *testing.T, doc *model.Document) {
	t.Helper()
	if doc.Components == nil {
		t.Fatal("Components is nil, want a resolved User schema")
	}
	user, ok := doc.Components.Schemas["User"]
	if !ok {
		t.Fatalf("Components.Schemas = %+v, missing User", doc.Components.Schemas)
	}
	if user.Type != "object" {
		t.Errorf("User.Type = %q, want object", user.Type)
	}
	for _, name := range []string{"id", "name", "email", "bio"} {
		if _, ok := user.Properties[name]; !ok {
			t.Errorf("User.Properties = %+v, missing %q", user.Properties, name)
		}
	}
	if !containsStr(user.Required, "id") || !containsStr(user.Required, "name") || !containsStr(user.Required, "email") {
		t.Errorf("User.Required = %+v, want id/name/email (no omitempty)", user.Required)
	}
	if containsStr(user.Required, "bio") {
		t.Errorf("User.Required = %+v, bio has omitempty and should not be required", user.Required)
	}
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func assertGetUser(t *testing.T, op *model.Operation) {
	t.Helper()
	if op == nil {
		t.Fatal("GET /users/{id} operation is nil")
	}
	if op.Summary != "Get a user by ID" {
		t.Errorf("Summary = %q", op.Summary)
	}
	if op.OperationID != "GetUser" {
		t.Errorf("OperationID = %q, want GetUser (inferred, not overridden by comment)", op.OperationID)
	}
	if len(op.Parameters) != 1 {
		t.Fatalf("Parameters = %+v", op.Parameters)
	}
	p := op.Parameters[0]
	if p.Name != "id" || p.In != "path" || !p.Required {
		t.Errorf("Parameters[0] = %+v", p)
	}
	if p.Schema == nil || p.Schema.Type != "integer" {
		t.Errorf("declared comment schema (integer) did not win over inferred default (string): %+v", p.Schema)
	}
	resp200, ok := op.Responses["200"]
	if !ok {
		t.Fatalf("missing 200 response: %+v", op.Responses)
	}
	if schema := resp200.Content["application/json"].Schema; schema == nil || schema.Ref != "#/components/schemas/User" {
		t.Errorf("200 response schema = %+v, want a bare $ref to User", schema)
	}
	if _, ok := op.Responses["404"]; !ok {
		t.Errorf("missing declared 404 response: %+v", op.Responses)
	}
}

func assertDeleteUser(t *testing.T, op *model.Operation) {
	t.Helper()
	if op == nil {
		t.Fatal("DELETE /users/{id} operation is nil")
	}
	if op.OperationID != "DeleteUser" {
		t.Errorf("OperationID = %q, want DeleteUser", op.OperationID)
	}
	// No "gota:" comment at all: purely inferred.
	if len(op.Parameters) != 1 || op.Parameters[0].Name != "id" {
		t.Fatalf("Parameters = %+v", op.Parameters)
	}
	if op.Parameters[0].Schema == nil || op.Parameters[0].Schema.Type != "string" {
		t.Errorf("inferred path param default should be string, got %+v", op.Parameters[0].Schema)
	}
	if resp, ok := op.Responses["200"]; !ok || resp.Description != "OK" {
		t.Errorf("inferred default response missing/wrong: %+v", op.Responses)
	}
}

func assertListUsers(t *testing.T, op *model.Operation) {
	t.Helper()
	if op == nil {
		t.Fatal("GET /users operation is nil")
	}
	if op.OperationID != "ListUsers" {
		t.Errorf("OperationID = %q, want ListUsers", op.OperationID)
	}
	if len(op.Parameters) != 0 {
		t.Errorf("expected no parameters, got %+v", op.Parameters)
	}
	resp, ok := op.Responses["200"]
	if !ok || resp.Description != "OK" {
		t.Fatalf("inferred default response missing/wrong: %+v", op.Responses)
	}
	// ListUsers has no "gota:" comment at all: its response schema comes
	// purely from DetectBody noticing "json.NewEncoder(w).Encode([]User{})"
	// in the handler body — this is the "pure inference" case actually
	// inferring something real, not just the generic 200 default.
	schema := resp.Content["application/json"].Schema
	if schema == nil || schema.Type != "array" || schema.Items == nil || schema.Items.Ref != "#/components/schemas/User" {
		t.Errorf("response schema = %+v, want an array with items $ref to User (detected from the handler body)", schema)
	}

	// ListUsers also has an error branch — http.Error(w, ..., http.StatusInternalServerError)
	// — which DetectBody must register as its own 500 response, distinct
	// from and without leaking into the 200 success path above.
	errResp, ok := op.Responses["500"]
	if !ok {
		t.Fatalf("Responses = %+v, missing the 500 detected from the http.Error branch", op.Responses)
	}
	if errResp.Description != "Internal Server Error" {
		t.Errorf("500 Description = %q, want %q", errResp.Description, "Internal Server Error")
	}
	if errResp.Content != nil {
		t.Errorf("500 Content = %+v, want nil (http.Error carries no JSON schema)", errResp.Content)
	}

	// ListUsers also has a debug branch marked "x-gota-skip: true" over
	// its http.Error(..., http.StatusTeapot) call — that response must
	// not appear at all, while 200 and 500 above are unaffected.
	if _, has418 := op.Responses["418"]; has418 {
		t.Errorf("Responses = %+v, the 418 debug branch is marked x-gota-skip and should not appear", op.Responses)
	}
}

func assertCreateUser(t *testing.T, op *model.Operation) {
	t.Helper()
	if op == nil {
		t.Fatal("POST /users operation is nil")
	}
	// Summary/operationId come from inference (comment doesn't declare them);
	// requestBody/responses come from the comment.
	if op.OperationID != "CreateUser" {
		t.Errorf("OperationID = %q, want CreateUser", op.OperationID)
	}
	if op.Summary == "" {
		t.Errorf("expected an inferred summary to survive the merge")
	}
	if op.RequestBody == nil || !op.RequestBody.Required {
		t.Fatalf("RequestBody = %+v", op.RequestBody)
	}
	mt, ok := op.RequestBody.Content["application/json"]
	if !ok || mt.Schema == nil || mt.Schema.Type != "object" {
		t.Errorf("RequestBody.Content = %+v", op.RequestBody.Content)
	}
	if _, ok := op.Responses["200"]; ok {
		t.Errorf("inferred default 200 response should have been replaced by the declared 201")
	}
	resp201, ok := op.Responses["201"]
	if !ok {
		t.Fatalf("missing declared 201 response: %+v", op.Responses)
	}
	if schema := resp201.Content["application/json"].Schema; schema == nil || schema.Ref != "#/components/schemas/User" {
		t.Errorf("201 response schema = %+v, want a bare $ref to the same User component", schema)
	}
}

func TestRun_MarshalRoundTrip(t *testing.T) {
	dir, err := filepath.Abs("../../testdata/nethttp-basic")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := generate.Run(generate.Options{
		Dir: dir, Title: "T", Version: "1.0.0",
		Plugins: []router.Plugin{nethttp.New()},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v", err)
	}

	yamlBytes, err := emitter.Marshal(doc, emitter.YAML)
	if err != nil {
		t.Fatalf("Marshal YAML: %v", err)
	}
	var roundTripped map[string]any
	if err := yaml.Unmarshal(yamlBytes, &roundTripped); err != nil {
		t.Fatalf("emitted YAML is not valid YAML: %v\n%s", err, yamlBytes)
	}
	if roundTripped["openapi"] != "3.1.0" {
		t.Errorf("round-tripped openapi field = %v", roundTripped["openapi"])
	}

	jsonBytes, err := emitter.Marshal(doc, emitter.JSON)
	if err != nil {
		t.Fatalf("Marshal JSON: %v", err)
	}
	if len(jsonBytes) == 0 {
		t.Fatal("empty JSON output")
	}
}

// TestRun_CrossPackageHandler proves the full pipeline resolves a handler
// registered from a different package than the one that declares it —
// fixture: testdata/nethttp-cross-package. GetUser has a "gota:" comment
// (proves comment extraction survives the package boundary); ListUsers
// has none, only a json.Encoder call in its body (proves best-effort
// body inference does too, using the *declaring* package's type info,
// not the *registering* one — the actual risk this fix has to get right).
func TestRun_CrossPackageHandler(t *testing.T) {
	dir, err := filepath.Abs("../../testdata/nethttp-cross-package")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Test API",
		Version: "1.0.0",
		Plugins: []router.Plugin{nethttp.New()},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v", err)
	}

	usersByID, ok := doc.Paths["/users/{id}"]
	if !ok || usersByID.Get == nil {
		t.Fatalf("missing GET /users/{id}: %+v", doc.Paths)
	}
	if usersByID.Get.Summary != "Get a user by ID" {
		t.Errorf("GetUser's \"gota:\" comment wasn't picked up across the package boundary: Summary = %q", usersByID.Get.Summary)
	}

	users, ok := doc.Paths["/users"]
	if !ok || users.Get == nil {
		t.Fatalf("missing GET /users: %+v", doc.Paths)
	}
	resp200, ok := users.Get.Responses["200"]
	if !ok {
		t.Fatalf("ListUsers has no inferred 200 response — cross-package body detection didn't run: %+v", users.Get.Responses)
	}
	schema := resp200.Content["application/json"].Schema
	if schema == nil || schema.Type != "array" {
		t.Errorf("ListUsers 200 response schema = %+v, want an inferred array schema", schema)
	}
}

// TestRun_GenericResponses is the end-to-end regression test for the bug
// this feature fixes: two handlers encoding different instantiations of
// the same generic Response[T] type (fixture: testdata/nethttp-generics)
// used to collide on a single "Response" component with an untyped data
// field. Both must now get their own distinct, correctly-typed component.
func TestRun_GenericResponses(t *testing.T) {
	dir, err := filepath.Abs("../../testdata/nethttp-generics")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Test API",
		Version: "1.0.0",
		Plugins: []router.Plugin{nethttp.New()},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v", err)
	}

	userRef := doc.Paths["/users/{id}"].Get.Responses["200"].Content["application/json"].Schema
	productRef := doc.Paths["/products/{id}"].Get.Responses["200"].Content["application/json"].Schema
	if userRef == nil || userRef.Ref != "#/components/schemas/Response_User" {
		t.Fatalf("GetUser response schema = %+v, want $ref to Response_User", userRef)
	}
	if productRef == nil || productRef.Ref != "#/components/schemas/Response_Product" {
		t.Fatalf("GetProduct response schema = %+v, want $ref to Response_Product", productRef)
	}

	respUser, ok := doc.Components.Schemas["Response_User"]
	if !ok {
		t.Fatalf("Response_User component not registered: %+v", doc.Components.Schemas)
	}
	respProduct, ok := doc.Components.Schemas["Response_Product"]
	if !ok {
		t.Fatalf("Response_Product component not registered: %+v", doc.Components.Schemas)
	}
	if respUser.Properties["data"].Ref != "#/components/schemas/User" {
		t.Errorf("Response_User.data = %+v, want $ref to User", respUser.Properties["data"])
	}
	if respProduct.Properties["data"].Ref != "#/components/schemas/Product" {
		t.Errorf("Response_Product.data = %+v, want $ref to Product", respProduct.Properties["data"])
	}
}
