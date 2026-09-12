package generate_test

import (
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/pabloos/gota/internal/emitter"
	"github.com/pabloos/gota/internal/generate"
	"github.com/pabloos/gota/internal/inference"
	"github.com/pabloos/gota/internal/router/chi"
	"github.com/pabloos/gota/internal/router/echo"
	"github.com/pabloos/gota/internal/router/fiber"
	"github.com/pabloos/gota/internal/router/gin"
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
		Routers: []generate.Router{{Plugin: nethttp.New(), Dialect: inference.NetHTTP()}},
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

// TestRun_ChiBasic mirrors TestRun_NetHTTPBasic against a chi-routed
// fixture (testdata/chi-basic, a separate Go module -- see its own
// go.mod) -- same handler content, proving the pipeline end-to-end on
// chi's structural difference from net/http's flat ServeMux: nested
// Route accumulates a path prefix and a same-package Mount constructor
// is followed, both reaching all the way through generate.Run, not
// just the plugin's own unit tests.
func TestRun_ChiBasic(t *testing.T) {
	dir, err := filepath.Abs("../../testdata/chi-basic")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Test API",
		Version: "1.0.0",
		Routers: []generate.Router{{Plugin: chi.New(), Dialect: inference.NetHTTP()}},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v", err)
	}

	if _, ok := doc.Paths["/debug/info"]; ok {
		t.Errorf("doc.Paths contains /debug/info, want it excluded by x-gota-skip")
	}

	usersByID, ok := doc.Paths["/users/{id}"]
	if !ok {
		t.Fatalf("missing /users/{id}")
	}
	assertGetUser(t, usersByID.Get)
	assertDeleteUser(t, usersByID.Delete)

	users, ok := doc.Paths["/users"]
	if !ok {
		t.Fatalf("missing /users")
	}
	assertListUsers(t, users.Get)
	assertCreateUser(t, users.Post)

	if _, ok := doc.Paths["/orders/"]; !ok {
		t.Errorf("doc.Paths = %+v, missing /orders/ (nested Route prefix should reach the full pipeline)", doc.Paths)
	}
	if _, ok := doc.Paths["/products/"]; !ok {
		t.Errorf("doc.Paths = %+v, missing /products/ (same-package Mount should reach the full pipeline)", doc.Paths)
	}

	assertUserComponent(t, doc)
}

// TestRun_GinInline drives the whole pipeline over a gin route whose
// handler is an inline function literal (testdata/gin-inline, its own Go
// module). With no handler name to base an operationId on, generate must
// synthesize one from the method+path, and body inference must still run
// on the literal's own body — proving both the router.Route.HandlerLit
// seam and generate's synthesis reach all the way through, not just the
// gin plugin's unit test.
func TestRun_GinInline(t *testing.T) {
	dir, err := filepath.Abs("../../testdata/gin-inline")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Test API",
		Version: "1.0.0",
		Routers: []generate.Router{{Plugin: gin.New(), Dialect: inference.Gin()}},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v", err)
	}

	version, ok := doc.Paths["/version"]
	if !ok {
		t.Fatalf("doc.Paths = %+v, missing /version (inline handler must still be emitted)", doc.Paths)
	}
	op := version.Get
	if op == nil {
		t.Fatal("/version has no GET operation")
	}
	if op.OperationID != "GetVersion" {
		t.Errorf("operationId = %q, want synthesized \"GetVersion\"", op.OperationID)
	}
	resp, ok := op.Responses["200"]
	if !ok {
		t.Fatalf("Responses = %+v, missing 200 (c.JSON in the literal body)", op.Responses)
	}
	if schema := resp.Content["application/json"].Schema; schema == nil || schema.Ref != "#/components/schemas/VersionInfo" {
		t.Errorf("200 schema = %+v, want $ref to VersionInfo inferred from the literal body", schema)
	}
	if doc.Components == nil || doc.Components.Schemas["VersionInfo"] == nil {
		t.Errorf("Components = %+v, want a resolved VersionInfo component", doc.Components)
	}
}

// TestRun_CrossModuleType is the regression guard for a handler whose
// response type is declared in a DIFFERENT module (fixture:
// testdata/cross-module — an `api` module importing a `lib` module via a
// replace). gota analyzes only `api`, so catalog.Product is a
// dependency, not an analyzed root. Before the dependency fallback,
// ResolveSchemaRefs looked up only the roots and aborted the whole
// document with a "no Go type named Product" error; now it resolves
// the type from the reachable import graph into a real component.
func TestRun_CrossModuleType(t *testing.T) {
	dir, err := filepath.Abs("testdata/cross-module/api")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Test API",
		Version: "1.0.0",
		Routers: []generate.Router{{Plugin: nethttp.New(), Dialect: inference.NetHTTP()}},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v", err)
	}

	clients, ok := doc.Paths["/clients"]
	if !ok {
		t.Fatalf("doc.Paths = %+v, missing /clients", doc.Paths)
	}
	resp, ok := clients.Get.Responses["200"]
	if !ok {
		t.Fatalf("Responses = %+v, missing 200", clients.Get.Responses)
	}
	// []*Product -> an array whose items $ref the cross-module type. Being
	// a dependency (outside the analyzed roots), it's package-qualified as
	// "catalog.Product" so it resolves by its own package, not a bare name.
	items := resp.Content["application/json"].Schema.Items
	if items == nil || items.Ref != "#/components/schemas/catalog.Product" {
		t.Errorf("200 items schema = %+v, want $ref to catalog.Product", items)
	}
	if doc.Components == nil || doc.Components.Schemas["catalog.Product"] == nil {
		t.Fatalf("Components = %+v, want catalog.Product resolved from the dependency module", doc.Components)
	}
	if doc.Components.Schemas["catalog.Product"].Properties["name"] == nil {
		t.Errorf("catalog.Product = %+v, want its fields expanded (name), not a bare object", doc.Components.Schemas["catalog.Product"])
	}
}

// TestRun_CrossModuleAmbiguityResolves is the regression guard for a
// production abort: a handler returns a dependency type (one.Thing) whose
// bare name also exists in another reachable dependency (two.Thing) that
// no handler exposes (fixture: testdata/cross-module-ambiguous). Because
// one.Thing is a dependency, it's package-qualified as "one.Thing" and
// resolves by its own package — the unexposed two.Thing never enters the
// picture, so there's no ambiguity and no abort, and the referenced type
// is fully expanded (not degraded to a bare object).
func TestRun_CrossModuleAmbiguityResolves(t *testing.T) {
	dir, err := filepath.Abs("testdata/cross-module-ambiguous/api")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Test API",
		Version: "1.0.0",
		Routers: []generate.Router{{Plugin: nethttp.New(), Dialect: inference.NetHTTP()}},
	})
	if err != nil {
		t.Fatalf("generate.Run must not abort on a cross-module type, got: %v", err)
	}
	if err := emitter.Validate(doc); err != nil {
		t.Fatalf("emitter.Validate: %v", err)
	}

	things, ok := doc.Paths["/things"]
	if !ok {
		t.Fatalf("doc.Paths = %+v, missing /things", doc.Paths)
	}
	// []*one.Thing -> array whose items resolve to the qualified component.
	items := things.Get.Responses["200"].Content["application/json"].Schema.Items
	if items == nil || items.Ref != "#/components/schemas/one.Thing" {
		t.Errorf("items schema = %+v, want $ref to one.Thing", items)
	}
	one, ok := doc.Components.Schemas["one.Thing"]
	if !ok {
		t.Fatalf("Components = %+v, want one.Thing resolved (its fields expanded)", doc.Components.Schemas)
	}
	if _, ok := one.Properties["id"]; !ok {
		t.Errorf("one.Thing = %+v, want package one's Thing (id field)", one)
	}
	// The unexposed two.Thing must never be registered.
	if _, ok := doc.Components.Schemas["two.Thing"]; ok {
		t.Errorf("Components = %+v, the unexposed two.Thing must not be registered", doc.Components.Schemas)
	}
}

// TestRun_GenericWrapperWithAmbiguousArg is the regression guard for a
// generic pagination wrapper instantiated with a dependency element type
// whose bare name collides across the reachable graph (fixture:
// testdata/cross-module-generic). The component name must carry the
// QUALIFIED argument (PageResponse_repository.Signature) — resolving the
// element by its bare name would be ambiguous and degrade the whole
// wrapper to an object, even though a directly-returned repository.Signature
// resolves fine.
func TestRun_GenericWrapperWithAmbiguousArg(t *testing.T) {
	dir, err := filepath.Abs("testdata/cross-module-generic/api")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Test API",
		Version: "1.0.0",
		Routers: []generate.Router{{Plugin: nethttp.New(), Dialect: inference.NetHTTP()}},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v", err)
	}
	if err := emitter.Validate(doc); err != nil {
		t.Fatalf("emitter.Validate: %v", err)
	}

	resp := doc.Paths["/signatures"].Get.Responses["200"]
	ref := resp.Content["application/json"].Schema.Ref
	if ref != "#/components/schemas/PageResponse_repository.Signature" {
		t.Fatalf("response $ref = %q, want the qualified generic wrapper PageResponse_repository.Signature (not a degraded object)", ref)
	}
	wrapper, ok := doc.Components.Schemas["PageResponse_repository.Signature"]
	if !ok {
		t.Fatalf("Components = %+v, want the wrapper component resolved", doc.Components.Schemas)
	}
	// Its list items must $ref the qualified element type, expanded.
	items := wrapper.Properties["list"].Items
	if items == nil || items.Ref != "#/components/schemas/repository.Signature" {
		t.Errorf("list items = %+v, want $ref to repository.Signature", items)
	}
	if sig := doc.Components.Schemas["repository.Signature"]; sig == nil || sig.Properties["hash"] == nil {
		t.Errorf("repository.Signature = %+v, want its fields expanded", doc.Components.Schemas["repository.Signature"])
	}
}

// TestRun_SchemaNameCollision is the real-world regression guard for the
// gmhafiz/go8 gap: two handler packages each declare their own Widget
// and infer $refs to it. Before package-qualified disambiguation this
// failed outright ("schema name ... is ambiguous"); now both must
// resolve to distinct author.Widget / book.Widget components AND the
// whole document must pass emitter.Validate — proving kin-openapi
// accepts the "." component key and resolves the qualified $ref
// end-to-end.
func TestRun_SchemaNameCollision(t *testing.T) {
	dir, err := filepath.Abs("../../testdata/schema-collision")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Test API",
		Version: "1.0.0",
		Routers: []generate.Router{{Plugin: nethttp.New(), Dialect: inference.NetHTTP()}},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v (the two-package Widget collision should now disambiguate, not error)", err)
	}

	if err := emitter.Validate(doc); err != nil {
		t.Fatalf("emitter.Validate: %v — a package-qualified '.' component key must validate and its $ref resolve", err)
	}

	if doc.Components == nil {
		t.Fatal("Components is nil, want author.Widget and book.Widget")
	}
	authorW, ok := doc.Components.Schemas["author.Widget"]
	if !ok {
		t.Fatalf("Components.Schemas = %+v, missing author.Widget", doc.Components.Schemas)
	}
	if _, ok := authorW.Properties["author_field"]; !ok {
		t.Errorf("author.Widget = %+v, want package author's Widget (author_field)", authorW)
	}
	bookW, ok := doc.Components.Schemas["book.Widget"]
	if !ok {
		t.Fatalf("Components.Schemas = %+v, missing book.Widget", doc.Components.Schemas)
	}
	if _, ok := bookW.Properties["book_field"]; !ok {
		t.Errorf("book.Widget = %+v, want package book's Widget (book_field)", bookW)
	}

	// The inferred request/response $refs on the operations must point at
	// the qualified component keys, not a bare "Widget".
	post := doc.Paths["/author/widgets"].Post
	if post == nil || post.RequestBody == nil {
		t.Fatalf("POST /author/widgets = %+v, want an inferred request body", post)
	}
	if ref := post.RequestBody.Content["application/json"].Schema.Ref; ref != "#/components/schemas/author.Widget" {
		t.Errorf("request body $ref = %q, want #/components/schemas/author.Widget", ref)
	}
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
	// DeleteUser's body is w.WriteHeader(http.StatusNoContent): a 204 is a
	// complete no-body response, inferred on its own and replacing the
	// synthesized default 200.
	if _, ok := op.Responses["200"]; ok {
		t.Errorf("Responses = %+v, want no default 200 (the handler responds 204)", op.Responses)
	}
	resp, ok := op.Responses["204"]
	if !ok || resp.Description != "No Content" {
		t.Errorf("inferred 204 response missing/wrong: %+v", op.Responses)
	}
	if len(resp.Content) != 0 {
		t.Errorf("204 response = %+v, want no content", resp)
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
		Routers: []generate.Router{{Plugin: nethttp.New(), Dialect: inference.NetHTTP()}},
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
		Routers: []generate.Router{{Plugin: nethttp.New(), Dialect: inference.NetHTTP()}},
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
		Routers: []generate.Router{{Plugin: nethttp.New(), Dialect: inference.NetHTTP()}},
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

// TestRun_OperationIDDisambiguation is the regression test for a real
// bug found running gota against a large, real Go API: a method-less
// pattern (net/http's ServeMux matches every method) expands into 8
// operations all bound to the same handler, so all 8 infer the identical
// operationId — an invalid OpenAPI document (operationId must be
// globally unique) that emitter.Validate correctly rejected, blocking
// generation entirely. generate.Run must disambiguate before that point
// is ever reached.
func TestRun_OperationIDDisambiguation(t *testing.T) {
	dir, err := filepath.Abs("../../testdata/nethttp-operationid-collision")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Test API",
		Version: "1.0.0",
		Routers: []generate.Router{{Plugin: nethttp.New(), Dialect: inference.NetHTTP()}},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v (operationId collisions must be disambiguated, not left to fail validation)", err)
	}

	item, ok := doc.Paths["/login"]
	if !ok {
		t.Fatalf("missing /login: %+v", doc.Paths)
	}
	ids := map[string]bool{}
	for _, op := range item.Operations() {
		if ids[op.OperationID] {
			t.Errorf("duplicate operationId %q across /login's operations", op.OperationID)
		}
		ids[op.OperationID] = true
		if op.OperationID == "Login" {
			t.Errorf("operationId = %q, want the bare handler name disambiguated (method+path appended), since 8 operations share it", op.OperationID)
		}
	}
	if len(ids) != 8 {
		t.Fatalf("got %d distinct operationIds, want 8 (one per expanded method): %v", len(ids), ids)
	}
}

// TestRun_SecurityExamplesAndDocBlock verifies that document-level fields
// declared in a "gota:doc:" block (securitySchemes, a global security
// requirement, servers, tags, an info description) land in the document,
// that a per-operation "security" and a response "examples" declared in a
// "gota:" comment survive, and that the whole thing is valid OpenAPI.
func TestRun_SecurityExamplesAndDocBlock(t *testing.T) {
	dir, err := filepath.Abs("../../testdata/security-examples")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Signatures API",
		Version: "1.0.0",
		Routers: []generate.Router{{Plugin: nethttp.New(), Dialect: inference.NetHTTP()}},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v", err)
	}
	if err := emitter.Validate(doc); err != nil {
		t.Fatalf("generated document is not valid OpenAPI: %v", err)
	}

	// Document-level "gota:doc:" declarations.
	if doc.Info.Description != "A signatures API secured with a bearer token." {
		t.Errorf("Info.Description = %q", doc.Info.Description)
	}
	if len(doc.Servers) != 1 || doc.Servers[0].URL != "https://api.example.com" {
		t.Errorf("Servers = %+v", doc.Servers)
	}
	if len(doc.Tags) != 1 || doc.Tags[0].Name != "signatures" {
		t.Errorf("Tags = %+v", doc.Tags)
	}
	if len(doc.Security) != 1 {
		t.Fatalf("global Security = %+v, want one requirement", doc.Security)
	}
	if _, ok := doc.Security[0]["BearerAuth"]; !ok {
		t.Errorf("global Security[0] = %+v, want BearerAuth", doc.Security[0])
	}
	if doc.Components == nil {
		t.Fatalf("Components is nil")
	}
	scheme := doc.Components.SecuritySchemes["BearerAuth"]
	if scheme == nil {
		t.Fatalf("SecuritySchemes = %+v", doc.Components.SecuritySchemes)
	}
	if scheme.Type != "http" || scheme.Scheme != "bearer" || scheme.BearerFormat != "JWT" {
		t.Errorf("BearerAuth = %+v", scheme)
	}

	// Document-level 3.1 keys that gota can't infer: externalDocs and a
	// webhooks section whose operation is a full Operation object.
	if doc.ExternalDocs == nil || doc.ExternalDocs.URL != "https://docs.example.com" {
		t.Errorf("ExternalDocs = %+v", doc.ExternalDocs)
	}
	hook := doc.Webhooks["signatureCertified"]
	if hook == nil || hook.Post == nil {
		t.Fatalf("Webhooks = %+v, want a signatureCertified with a post operation", doc.Webhooks)
	}
	if hook.Post.Summary != "Signature certified" {
		t.Errorf("webhook post = %+v", hook.Post)
	}
	// The $ref inside the webhook resolved to the same expanded component a
	// path uses (proving webhook operations take part in ref resolution).
	hookBody := hook.Post.RequestBody
	if hookBody == nil || hookBody.Content["application/json"].Schema == nil ||
		hookBody.Content["application/json"].Schema.Ref != "#/components/schemas/Signature" {
		t.Errorf("webhook requestBody = %+v, want a $ref to Signature", hookBody)
	}

	// Per-operation security on POST /signatures.
	post := doc.Paths["/signatures"].Post
	if post == nil {
		t.Fatalf("POST /signatures missing")
	}
	if post.Security == nil || len(*post.Security) != 1 {
		t.Fatalf("POST /signatures Security = %+v, want one requirement", post.Security)
	}
	if _, ok := (*post.Security)[0]["BearerAuth"]; !ok {
		t.Errorf("POST /signatures Security[0] = %+v, want BearerAuth", (*post.Security)[0])
	}

	// Response examples on GET /signatures.
	get := doc.Paths["/signatures"].Get
	if get == nil {
		t.Fatalf("GET /signatures missing")
	}
	resp, ok := get.Responses["200"]
	if !ok {
		t.Fatalf("GET /signatures responses = %+v, want a 200", get.Responses)
	}
	mt, ok := resp.Content["application/json"]
	if !ok {
		t.Fatalf("200 content = %+v, want application/json", resp.Content)
	}
	if mt.Examples["sample"] == nil {
		t.Errorf("media type examples = %+v, want a \"sample\" example", mt.Examples)
	}

	// The $ref used by the declared example resolves to an expanded schema.
	if doc.Components.Schemas["Signature"] == nil {
		t.Errorf("Signature schema was not expanded into components: %+v", doc.Components.Schemas)
	}

	// GET /signatures/{id}: the comment declares only a response example;
	// the schema gota inferred ($ref Signature) and the inferred "OK"
	// description must survive the merge alongside it.
	byID := doc.Paths["/signatures/{id}"].Get
	if byID == nil {
		t.Fatalf("GET /signatures/{id} missing")
	}
	if byID.Description != "Fetch a single signature." {
		t.Errorf("Description = %q, want the declared operation description", byID.Description)
	}
	r200, ok := byID.Responses["200"]
	if !ok {
		t.Fatalf("GET /signatures/{id} responses = %+v, want a 200", byID.Responses)
	}
	if r200.Description == "" {
		t.Errorf("200 description was blanked, want the inferred one preserved")
	}
	idMT, ok := r200.Content["application/json"]
	if !ok {
		t.Fatalf("200 content = %+v, want application/json", r200.Content)
	}
	if idMT.Schema == nil || idMT.Schema.Ref != "#/components/schemas/Signature" {
		t.Errorf("Schema = %+v, want the inferred $ref preserved", idMT.Schema)
	}
	if idMT.Example == nil {
		t.Errorf("Example is nil, want the declared example added")
	}
	// The comment enriches the inferred {id} path parameter: the declared
	// description and constrained schema (pattern) survive, and the
	// inferred required flag is kept — not duplicated into a second param.
	if len(byID.Parameters) != 1 {
		t.Fatalf("GET /signatures/{id} parameters = %+v, want the single enriched {id}", byID.Parameters)
	}
	idParam := byID.Parameters[0]
	if idParam.Name != "id" || !idParam.Required {
		t.Errorf("id param = %+v, want name id and inferred required preserved", idParam)
	}
	if idParam.Schema == nil || idParam.Schema.Pattern != "^sig_([a-zA-Z0-9]{22})$" {
		t.Errorf("id param schema = %+v, want the declared pattern preserved", idParam.Schema)
	}

	// GET /revoked has no declared description, so its Go doc-comment prose
	// (both paragraphs, the gota: block excluded) becomes the description.
	revoked := doc.Paths["/revoked"].Get
	if revoked == nil {
		t.Fatalf("GET /revoked missing")
	}
	wantProse := "ListRevoked returns revoked signatures. It documents itself in plain Go\nprose and declares no gota: description, so this text becomes the\noperation description.\n\nThe revocation list is advisory and cached for a minute."
	if revoked.Description != wantProse {
		t.Errorf("GET /revoked description = %q, want the doc-comment prose %q", revoked.Description, wantProse)
	}

	// GetSignature's doc comment has prose AND declares a description; the
	// declared one wins (already asserted above as "Fetch a single
	// signature.", which is not its prose).

	// GET /health declares security: [] — a non-nil empty requirement that
	// marks the endpoint public, distinct from an absent one.
	health := doc.Paths["/health"].Get
	if health == nil {
		t.Fatalf("GET /health missing")
	}
	if health.Security == nil {
		t.Errorf("GET /health Security is nil, want a declared empty (public) requirement")
	} else if len(*health.Security) != 0 {
		t.Errorf("GET /health Security = %+v, want empty", *health.Security)
	}
}

// TestRun_EchoBasic drives the whole pipeline through the echo plugin +
// dialect against a real echo API (fixture testdata/echo-basic, its own
// module): a group prefix, a :id path parameter, c.Bind request bodies,
// c.JSON at explicit codes, a c.NoContent 204, and an inline func literal —
// all landing in a document that validates.
func TestRun_EchoBasic(t *testing.T) {
	dir, err := filepath.Abs("../../testdata/echo-basic")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Echo API",
		Version: "1.0.0",
		Routers: []generate.Router{{Plugin: echo.New(), Dialect: inference.Echo()}},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v", err)
	}
	if err := emitter.Validate(doc); err != nil {
		t.Fatalf("generated document is not valid OpenAPI: %v", err)
	}

	// GET /api/v1/users/{id}: group prefix + :id param, prose description.
	get := doc.Paths["/api/v1/users/{id}"]
	if get == nil || get.Get == nil {
		t.Fatalf("doc.Paths = %+v, missing GET /api/v1/users/{id}", doc.Paths)
	}
	if get.Get.Description != "GetUser returns a user by id." {
		t.Errorf("description = %q, want the doc-comment prose", get.Get.Description)
	}
	if len(get.Get.Parameters) != 1 || get.Get.Parameters[0].Name != "id" {
		t.Errorf("parameters = %+v, want a single {id} path param", get.Get.Parameters)
	}
	if s := get.Get.Responses["200"].Content["application/json"].Schema; s == nil || s.Ref != "#/components/schemas/User" {
		t.Errorf("200 schema = %+v, want $ref to User", s)
	}

	// POST /api/v1/users: c.Bind request body + c.JSON(201).
	post := doc.Paths["/api/v1/users"].Post
	if post == nil {
		t.Fatalf("missing POST /api/v1/users")
	}
	if post.RequestBody == nil || post.RequestBody.Content["application/json"].Schema.Ref != "#/components/schemas/User" {
		t.Errorf("requestBody = %+v, want $ref to User from c.Bind", post.RequestBody)
	}
	if _, ok := post.Responses["201"]; !ok {
		t.Errorf("responses = %+v, want 201 from c.JSON", post.Responses)
	}

	// DELETE /api/v1/users/{id}: c.NoContent -> a bodyless 204, not 200.
	del := doc.Paths["/api/v1/users/{id}"].Delete
	if del == nil {
		t.Fatalf("missing DELETE /api/v1/users/{id}")
	}
	if _, ok := del.Responses["200"]; ok {
		t.Errorf("responses = %+v, want no default 200 (c.NoContent is a 204)", del.Responses)
	}
	if _, ok := del.Responses["204"]; !ok {
		t.Errorf("responses = %+v, want a 204 from c.NoContent", del.Responses)
	}

	// The inline func literal still emits a route with a synthesized id.
	version := doc.Paths["/version"]
	if version == nil || version.Get == nil {
		t.Fatalf("missing GET /version (inline handler)")
	}
	if version.Get.OperationID != "GetVersion" {
		t.Errorf("operationId = %q, want synthesized \"GetVersion\"", version.Get.OperationID)
	}

	if doc.Components == nil || doc.Components.Schemas["User"] == nil {
		t.Errorf("Components = %+v, want a resolved User component", doc.Components)
	}
}

// TestRun_FiberBasic drives the whole pipeline through the fiber plugin +
// dialect against a real fiber API (fixture testdata/fiber-basic): routes
// registered via a register function on a group, c.BodyParser bodies,
// c.Status(code).JSON and c.SendStatus responses — all validating.
func TestRun_FiberBasic(t *testing.T) {
	dir, err := filepath.Abs("../../testdata/fiber-basic")
	if err != nil {
		t.Fatal(err)
	}

	doc, err := generate.Run(generate.Options{
		Dir:     dir,
		Title:   "Fiber API",
		Version: "1.0.0",
		Routers: []generate.Router{{Plugin: fiber.New(), Dialect: inference.Fiber()}},
	})
	if err != nil {
		t.Fatalf("generate.Run: %v", err)
	}
	if err := emitter.Validate(doc); err != nil {
		t.Fatalf("generated document is not valid OpenAPI: %v", err)
	}

	// The register function's routes resolve under the group's "/api/v1".
	get := doc.Paths["/api/v1/users/{id}"]
	if get == nil || get.Get == nil {
		t.Fatalf("doc.Paths = %+v, missing GET /api/v1/users/{id}", doc.Paths)
	}
	if get.Get.Description != "GetUser returns a user by id." {
		t.Errorf("description = %q, want the doc-comment prose", get.Get.Description)
	}
	if len(get.Get.Parameters) != 1 || get.Get.Parameters[0].Name != "id" {
		t.Errorf("parameters = %+v, want a single {id} path param", get.Get.Parameters)
	}
	if s := get.Get.Responses["200"].Content["application/json"].Schema; s == nil || s.Ref != "#/components/schemas/User" {
		t.Errorf("200 schema = %+v, want $ref to User", s)
	}

	post := doc.Paths["/api/v1/users"].Post
	if post == nil {
		t.Fatalf("missing POST /api/v1/users")
	}
	if post.RequestBody == nil || post.RequestBody.Content["application/json"].Schema.Ref != "#/components/schemas/User" {
		t.Errorf("requestBody = %+v, want $ref to User from c.BodyParser", post.RequestBody)
	}
	if _, ok := post.Responses["201"]; !ok {
		t.Errorf("responses = %+v, want 201 from c.Status(201).JSON", post.Responses)
	}

	del := doc.Paths["/api/v1/users/{id}"].Delete
	if del == nil {
		t.Fatalf("missing DELETE /api/v1/users/{id}")
	}
	if _, ok := del.Responses["200"]; ok {
		t.Errorf("responses = %+v, want no default 200 (c.SendStatus is 204)", del.Responses)
	}
	if _, ok := del.Responses["204"]; !ok {
		t.Errorf("responses = %+v, want a 204 from c.SendStatus", del.Responses)
	}

	if doc.Components == nil || doc.Components.Schemas["User"] == nil {
		t.Errorf("Components = %+v, want a resolved User component", doc.Components)
	}
}
