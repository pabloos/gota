package inference

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/pabloos/gota/internal/router"
	"github.com/pabloos/gota/pkg/model"
)

func TestHumanize(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"", ""},
		{"GetUser", "Get user"},
		{"getUser", "Get user"},
		{"ListUsers", "List users"},
		{"CreateUser", "Create user"},
		{"GetUserByID", "Get user by ID"},
		{"DeleteUserByID", "Delete user by ID"},
		{"GetHTTPStatus", "Get HTTP status"},
		{"GetXMLData", "Get XML data"},
		{"GetUserID", "Get user ID"},
		{"A", "A"},
		// Known heuristic limitation: an acronym immediately followed by a
		// lowercase plural "s" is not recognized as its own word, so it
		// gets lowercased along with the "s" instead of staying "IDs".
		{"GetIDs", "Get ids"},
		// A name that IS the acronym is still title-cased, since the first
		// word always gets standard title-casing (there's no way to tell
		// "an acronym used alone" from "a normal capitalized word" here).
		{"ID", "Id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := humanize(c.name); got != c.want {
				t.Errorf("humanize(%q) = %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func TestPathParameters(t *testing.T) {
	t.Run("no params", func(t *testing.T) {
		if got := pathParameters("/users"); got != nil {
			t.Errorf("pathParameters(/users) = %+v, want nil", got)
		}
	})

	t.Run("single param defaults to string schema", func(t *testing.T) {
		got := pathParameters("/users/{id}")
		want := []model.Parameter{
			{Name: "id", In: "path", Required: true, Schema: &model.Schema{Type: "string"}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("pathParameters(/users/{id}) = %+v, want %+v", got, want)
		}
	})

	t.Run("multiple params preserve order", func(t *testing.T) {
		got := pathParameters("/orgs/{org}/repos/{repo}")
		if len(got) != 2 {
			t.Fatalf("got %d params, want 2: %+v", len(got), got)
		}
		if got[0].Name != "org" || got[1].Name != "repo" {
			t.Errorf("params out of order: %+v", got)
		}
	})

	t.Run("Go 1.22 wildcard suffix is stripped from the param name", func(t *testing.T) {
		got := pathParameters("/files/{path...}")
		if len(got) != 1 || got[0].Name != "path" {
			t.Fatalf("pathParameters(/files/{path...}) = %+v, want a single param named \"path\"", got)
		}
	})

	t.Run("all params are required path params, never optional", func(t *testing.T) {
		got := pathParameters("/users/{id}")
		if !got[0].Required || got[0].In != "path" {
			t.Errorf("got %+v, want In=path Required=true", got[0])
		}
	})
}

func TestOperation(t *testing.T) {
	t.Run("populates operationId and summary from the handler name", func(t *testing.T) {
		op := Operation(router.Route{Method: http.MethodGet, Path: "/users", HandlerName: "ListUsers"})
		if op.OperationID != "ListUsers" {
			t.Errorf("OperationID = %q, want ListUsers", op.OperationID)
		}
		if op.Summary != "List users" {
			t.Errorf("Summary = %q, want %q", op.Summary, "List users")
		}
	})

	t.Run("infers path parameters from the route path", func(t *testing.T) {
		op := Operation(router.Route{Method: http.MethodGet, Path: "/users/{id}", HandlerName: "GetUser"})
		if len(op.Parameters) != 1 || op.Parameters[0].Name != "id" {
			t.Fatalf("Parameters = %+v", op.Parameters)
		}
	})

	t.Run("path-less routes get no parameters", func(t *testing.T) {
		op := Operation(router.Route{Method: http.MethodGet, Path: "/users", HandlerName: "ListUsers"})
		if op.Parameters != nil {
			t.Errorf("Parameters = %+v, want nil", op.Parameters)
		}
	})

	// Current limitation: the inferred default response is always "200 OK"
	// regardless of HTTP method. A POST handler with no "gota:" comment
	// does NOT get an inferred 201, and a DELETE does not get an inferred
	// 204 — the programmer must declare those via a comment. This test
	// pins that behavior down so a future change to method-aware defaults
	// is a deliberate decision, not an accidental regression.
	t.Run("default response is always 200 OK regardless of method", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			op := Operation(router.Route{Method: method, Path: "/users", HandlerName: "Handle"})
			want := map[string]model.Response{"200": {Description: "OK"}}
			if !reflect.DeepEqual(op.Responses, want) {
				t.Errorf("method %s: Responses = %+v, want %+v", method, op.Responses, want)
			}
		}
	})
}
