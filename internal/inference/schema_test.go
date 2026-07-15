package inference

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/internal/parser"
	"github.com/pabloos/gota/pkg/model"
)

// loadFixture loads the fixture package checked in at
// internal/inference/testdata/<name>, with the same parser.Load path the
// real pipeline uses.
func loadFixture(t *testing.T, name string) []*packages.Package {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := parser.Load(dir)
	if err != nil {
		t.Fatalf("parser.Load: %v", err)
	}
	return pkgs
}

// docWithRef builds a minimal Document with a single GET /x operation whose
// 200 response schema is "$ref: '#/components/schemas/<name>'" — enough to
// drive ResolveSchemaRefs without needing the full route-building pipeline.
func docWithRef(name string) *model.Document {
	return &model.Document{
		Paths: model.Paths{
			"/x": &model.PathItem{
				Get: &model.Operation{
					Responses: map[string]model.Response{
						"200": {
							Description: "ok",
							Content: map[string]model.MediaType{
								"application/json": {Schema: &model.Schema{Ref: schemaRefPrefix + name}},
							},
						},
					},
				},
			},
		},
	}
}

func TestResolveSchemaRefs_PrimitivesAndOmitempty(t *testing.T) {
	pkgs := loadFixture(t, "schema_primitives")

	doc := docWithRef("User")
	if err := ResolveSchemaRefs(doc, pkgs); err != nil {
		t.Fatalf("ResolveSchemaRefs: %v", err)
	}

	s, ok := doc.Components.Schemas["User"]
	if !ok {
		t.Fatalf("User component not registered: %+v", doc.Components)
	}
	if s.Type != "object" {
		t.Errorf("Type = %q, want object", s.Type)
	}
	if len(s.Properties) != 3 {
		t.Fatalf("Properties = %+v, want exactly id/name/bio", s.Properties)
	}
	for _, name := range []string{"id", "name", "bio"} {
		if _, ok := s.Properties[name]; !ok {
			t.Errorf("missing property %q", name)
		}
	}
	if !containsAll(s.Required, "id", "name") || contains(s.Required, "bio") {
		t.Errorf("Required = %+v, want exactly [id name]", s.Required)
	}

	// The response schema itself must now be a bare $ref, not inflated inline.
	respSchema := doc.Paths["/x"].Get.Responses["200"].Content["application/json"].Schema
	if respSchema.Ref != schemaRefPrefix+"User" || respSchema.Type != "" {
		t.Errorf("response schema = %+v, want a bare $ref", respSchema)
	}
}

func TestResolveSchemaRefs_NestedStructBecomesLinkedComponent(t *testing.T) {
	pkgs := loadFixture(t, "schema_nested")

	doc := docWithRef("User")
	if err := ResolveSchemaRefs(doc, pkgs); err != nil {
		t.Fatalf("ResolveSchemaRefs: %v", err)
	}

	user := doc.Components.Schemas["User"]
	addrProp, ok := user.Properties["address"]
	if !ok {
		t.Fatalf("User.Properties = %+v, missing address", user.Properties)
	}
	if addrProp.Ref != schemaRefPrefix+"Address" {
		t.Errorf("address property = %+v, want a $ref to Address, not inlined", addrProp)
	}

	addr, ok := doc.Components.Schemas["Address"]
	if !ok {
		t.Fatalf("Address was not registered as its own component: %+v", doc.Components.Schemas)
	}
	if _, ok := addr.Properties["city"]; !ok {
		t.Errorf("Address.Properties = %+v, missing city", addr.Properties)
	}
}

func TestResolveSchemaRefs_Slices(t *testing.T) {
	pkgs := loadFixture(t, "schema_slices")

	doc := docWithRef("User")
	if err := ResolveSchemaRefs(doc, pkgs); err != nil {
		t.Fatalf("ResolveSchemaRefs: %v", err)
	}

	user := doc.Components.Schemas["User"]
	tags := user.Properties["tags"]
	if tags.Type != "array" || tags.Items == nil || tags.Items.Ref != schemaRefPrefix+"Tag" {
		t.Errorf("tags property = %+v, want array with items $ref Tag", tags)
	}
	roles := user.Properties["roles"]
	if roles.Type != "array" || roles.Items == nil || roles.Items.Type != "string" {
		t.Errorf("roles property = %+v, want array with items type string", roles)
	}
	if _, ok := doc.Components.Schemas["Tag"]; !ok {
		t.Errorf("Tag was not registered as its own component")
	}
}

func TestResolveSchemaRefs_TimeTimeField(t *testing.T) {
	pkgs := loadFixture(t, "schema_time")

	doc := docWithRef("User")
	if err := ResolveSchemaRefs(doc, pkgs); err != nil {
		t.Fatalf("ResolveSchemaRefs: %v", err)
	}

	created := doc.Components.Schemas["User"].Properties["created_at"]
	if created.Type != "string" || created.Format != "date-time" {
		t.Errorf("created_at property = %+v, want {type: string, format: date-time}", created)
	}
}

func TestResolveSchemaRefs_EmbeddedStructPromotesFields(t *testing.T) {
	pkgs := loadFixture(t, "schema_embedded")

	doc := docWithRef("User")
	if err := ResolveSchemaRefs(doc, pkgs); err != nil {
		t.Fatalf("ResolveSchemaRefs: %v", err)
	}

	user := doc.Components.Schemas["User"]
	if _, ok := user.Properties["id"]; !ok {
		t.Errorf("Properties = %+v, want the embedded Base's \"id\" promoted into User", user.Properties)
	}
	if _, ok := user.Properties["name"]; !ok {
		t.Errorf("Properties = %+v, missing name", user.Properties)
	}
	if _, ok := doc.Components.Schemas["Base"]; ok {
		t.Errorf("Base should not be registered as its own component when embedded (fields are flattened)")
	}
}

func TestResolveSchemaRefs_HandlesCycles(t *testing.T) {
	pkgs := loadFixture(t, "schema_cycles")

	doc := docWithRef("A")
	if err := ResolveSchemaRefs(doc, pkgs); err != nil {
		t.Fatalf("ResolveSchemaRefs: %v", err)
	}

	a, ok := doc.Components.Schemas["A"]
	if !ok {
		t.Fatalf("A was not registered")
	}
	b, ok := doc.Components.Schemas["B"]
	if !ok {
		t.Fatalf("B was not registered")
	}
	if a.Properties["b"].Ref != schemaRefPrefix+"B" {
		t.Errorf("A.b = %+v, want $ref to B", a.Properties["b"])
	}
	if b.Properties["a"].Ref != schemaRefPrefix+"A" {
		t.Errorf("B.a = %+v, want $ref to A", b.Properties["a"])
	}
}

func TestResolveSchemaRefs_UnknownTypeErrors(t *testing.T) {
	pkgs := loadFixture(t, "schema_empty")

	doc := docWithRef("DoesNotExist")
	err := ResolveSchemaRefs(doc, pkgs)
	if err == nil {
		t.Fatal("expected an error for a $ref with no matching Go type")
	}
	if !strings.Contains(err.Error(), "DoesNotExist") {
		t.Errorf("error %q should mention the missing type name", err.Error())
	}
}

func TestResolveSchemaRefs_NoRefsIsANoop(t *testing.T) {
	pkgs := loadFixture(t, "schema_empty")

	doc := &model.Document{Paths: model.Paths{"/x": &model.PathItem{Get: &model.Operation{}}}}
	if err := ResolveSchemaRefs(doc, pkgs); err != nil {
		t.Fatalf("ResolveSchemaRefs: %v", err)
	}
	if doc.Components != nil {
		t.Errorf("Components = %+v, want nil when nothing references a schema", doc.Components)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func containsAll(list []string, want ...string) bool {
	for _, w := range want {
		if !contains(list, w) {
			return false
		}
	}
	return true
}
