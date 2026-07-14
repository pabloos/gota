package emitter_test

import (
	"net/http"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/pabloos/gota/internal/emitter"
	"github.com/pabloos/gota/pkg/model"
)

func TestBuild_AssemblesPathsByMethod(t *testing.T) {
	doc, err := emitter.Build(model.Info{Title: "T", Version: "1.0.0"}, []emitter.RouteOperation{
		{Method: http.MethodGet, Path: "/users", Operation: &model.Operation{OperationID: "ListUsers"}},
		{Method: http.MethodPost, Path: "/users", Operation: &model.Operation{OperationID: "CreateUser"}},
		{Method: http.MethodGet, Path: "/users/{id}", Operation: &model.Operation{OperationID: "GetUser"}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Errorf("OpenAPI = %q", doc.OpenAPI)
	}
	if len(doc.Paths) != 2 {
		t.Fatalf("got %d paths, want 2: %+v", len(doc.Paths), doc.Paths)
	}
	users := doc.Paths["/users"]
	if users == nil || users.Get == nil || users.Get.OperationID != "ListUsers" {
		t.Errorf("/users GET = %+v", users)
	}
	if users == nil || users.Post == nil || users.Post.OperationID != "CreateUser" {
		t.Errorf("/users POST = %+v", users)
	}
	usersByID := doc.Paths["/users/{id}"]
	if usersByID == nil || usersByID.Get == nil || usersByID.Get.OperationID != "GetUser" {
		t.Errorf("/users/{id} GET = %+v", usersByID)
	}
}

func TestBuild_DuplicateRouteErrors(t *testing.T) {
	_, err := emitter.Build(model.Info{}, []emitter.RouteOperation{
		{Method: http.MethodGet, Path: "/users", Operation: &model.Operation{OperationID: "A"}},
		{Method: http.MethodGet, Path: "/users", Operation: &model.Operation{OperationID: "B"}},
	})
	if err == nil {
		t.Fatal("expected an error for a duplicate GET /users route")
	}
}

// TestBuild_UnrepresentableMethodErrors guards against silently dropping a
// route. CONNECT is a method net/http can route but OpenAPI's Path Item
// Object has no operation field for; Build must surface that as an error
// instead of quietly discarding the operation.
func TestBuild_UnrepresentableMethodErrors(t *testing.T) {
	_, err := emitter.Build(model.Info{}, []emitter.RouteOperation{
		{Method: http.MethodConnect, Path: "/tunnel", Operation: &model.Operation{OperationID: "Tunnel"}},
	})
	if err == nil {
		t.Fatal("expected an error for an unrepresentable HTTP method (CONNECT), got nil")
	}
	if !strings.Contains(err.Error(), http.MethodConnect) {
		t.Errorf("error %q should mention the offending method", err.Error())
	}
}

func TestMarshal_YAML(t *testing.T) {
	doc, err := emitter.Build(model.Info{Title: "T", Version: "1.0.0"}, []emitter.RouteOperation{
		{Method: http.MethodGet, Path: "/users", Operation: &model.Operation{
			OperationID: "ListUsers",
			Responses:   map[string]model.Response{"200": {Description: "OK"}},
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	out, err := emitter.Marshal(doc, emitter.YAML)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("emitted YAML is not valid: %v\n%s", err, out)
	}
	if got["openapi"] != "3.1.0" {
		t.Errorf("openapi = %v", got["openapi"])
	}
}

func TestMarshal_JSON(t *testing.T) {
	doc, err := emitter.Build(model.Info{Title: "T", Version: "1.0.0"}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	out, err := emitter.Marshal(doc, emitter.JSON)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(out), `"openapi": "3.1.0"`) {
		t.Errorf("JSON output missing openapi field:\n%s", out)
	}
}

func TestValidate_ValidDocumentPasses(t *testing.T) {
	doc, err := emitter.Build(model.Info{Title: "T", Version: "1.0.0"}, []emitter.RouteOperation{
		{Method: http.MethodGet, Path: "/users/{id}", Operation: &model.Operation{
			OperationID: "GetUser",
			Parameters: []model.Parameter{
				{Name: "id", In: "path", Required: true, Schema: &model.Schema{Type: "integer"}},
			},
			Responses: map[string]model.Response{"200": {Description: "OK"}},
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := emitter.Validate(doc); err != nil {
		t.Errorf("Validate() = %v, want nil for a well-formed document", err)
	}
}

// TestValidate_CatchesPathParamNotRequired pins down that Validate catches
// a real mistake our own struct definitions don't prevent: OpenAPI requires
// that a path parameter always have required: true, but nothing in
// model.Parameter enforces that — a "gota:" comment declaring "in: path"
// without "required: true" would otherwise sail through untouched, since
// bool's zero value (false) is a perfectly ordinary Go value.
func TestValidate_CatchesPathParamNotRequired(t *testing.T) {
	doc, err := emitter.Build(model.Info{Title: "T", Version: "1.0.0"}, []emitter.RouteOperation{
		{Method: http.MethodGet, Path: "/users/{id}", Operation: &model.Operation{
			OperationID: "GetUser",
			Parameters: []model.Parameter{
				{Name: "id", In: "path", Required: false, Schema: &model.Schema{Type: "integer"}},
			},
			Responses: map[string]model.Response{"200": {Description: "OK"}},
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := emitter.Validate(doc); err == nil {
		t.Fatal("expected Validate to reject a path parameter that isn't marked required")
	}
}

func TestMarshal_UnknownFormat(t *testing.T) {
	doc, err := emitter.Build(model.Info{}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := emitter.Marshal(doc, emitter.Format("toml")); err == nil {
		t.Fatal("expected an error for an unknown format")
	}
}
