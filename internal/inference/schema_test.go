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

// docWithTwoRefs builds a minimal Document with two GET operations
// ("/x" and "/y"), each with its own 200 response $ref — enough to check
// that two distinct $refs resolve to two distinct, non-colliding
// components.
func docWithTwoRefs(name1, name2 string) *model.Document {
	op := func(name string) *model.Operation {
		return &model.Operation{
			Responses: map[string]model.Response{
				"200": {
					Description: "ok",
					Content: map[string]model.MediaType{
						"application/json": {Schema: &model.Schema{Ref: schemaRefPrefix + name}},
					},
				},
			},
		}
	}
	return &model.Document{
		Paths: model.Paths{
			"/x": &model.PathItem{Get: op(name1)},
			"/y": &model.PathItem{Get: op(name2)},
		},
	}
}

func TestResolveSchemaRefs_PrimitivesAndOmitempty(t *testing.T) {
	pkgs := loadFixture(t, "schema_primitives")

	doc := docWithRef("User")
	if err := ResolveSchemaRefs(doc, pkgs, nil); err != nil {
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
	if err := ResolveSchemaRefs(doc, pkgs, nil); err != nil {
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
	if err := ResolveSchemaRefs(doc, pkgs, nil); err != nil {
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
	if err := ResolveSchemaRefs(doc, pkgs, nil); err != nil {
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
	if err := ResolveSchemaRefs(doc, pkgs, nil); err != nil {
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
	if err := ResolveSchemaRefs(doc, pkgs, nil); err != nil {
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
	err := ResolveSchemaRefs(doc, pkgs, nil)
	if err == nil {
		t.Fatal("expected an error for a $ref with no matching Go type")
	}
	if !strings.Contains(err.Error(), "DoesNotExist") {
		t.Errorf("error %q should mention the missing type name", err.Error())
	}
}

// TestResolveSchemaRefs_AmbiguousNameErrors pins down that a schema name
// declared in more than one analyzed package is a hard error, not a
// silent "first match wins" — gota has no $ref syntax to say which one
// was meant, so guessing would risk generating the wrong schema instead
// of failing loudly.
func TestResolveSchemaRefs_AmbiguousNameErrors(t *testing.T) {
	pkgs := loadFixture(t, "schema_ambiguous")
	if len(pkgs) != 2 {
		t.Fatalf("fixture setup: got %d packages, want 2 (a and b, each declaring User)", len(pkgs))
	}

	// A HAND-WRITTEN bare $ref to an ambiguous name still errors: gota
	// can't know which package's User the author meant. (An INFERRED
	// $ref for such a name is pre-qualified and never reaches this — see
	// TestResolveSchemaRefs_PackageQualified.) The message now suggests
	// the qualified forms the author can use instead.
	doc := docWithRef("User")
	err := ResolveSchemaRefs(doc, pkgs, AmbiguousSchemaNames(pkgs))
	if err == nil {
		t.Fatal("expected an error for a $ref matching a type declared in two packages")
	}
	if !strings.Contains(err.Error(), "User") || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error %q should mention the ambiguous name", err.Error())
	}
	if !strings.Contains(err.Error(), "a.User") || !strings.Contains(err.Error(), "b.User") {
		t.Errorf("error %q should suggest the package-qualified forms a.User / b.User", err.Error())
	}
	if doc.Components != nil {
		t.Errorf("Components = %+v, want nil — an ambiguous $ref must not partially resolve", doc.Components)
	}
}

// TestResolveSchemaRefs_PackageQualified is the inference-side path: a
// name declared in two packages, referenced by two DISTINCT
// package-qualified $refs (as body inference emits them, given the
// AmbiguousSchemaNames set), resolves to two distinct components keyed
// by the qualified names — no error, no collision.
func TestResolveSchemaRefs_PackageQualified(t *testing.T) {
	pkgs := loadFixture(t, "schema_ambiguous")

	ambiguous := AmbiguousSchemaNames(pkgs)
	if !ambiguous["User"] {
		t.Fatalf("AmbiguousSchemaNames = %+v, want it to flag User (declared in a and b)", ambiguous)
	}

	doc := docWithTwoRefs("a.User", "b.User")
	if err := ResolveSchemaRefs(doc, pkgs, ambiguous); err != nil {
		t.Fatalf("ResolveSchemaRefs: %v", err)
	}

	aUser, ok := doc.Components.Schemas["a.User"]
	if !ok {
		t.Fatalf("Components = %+v, missing a.User", doc.Components.Schemas)
	}
	if _, ok := aUser.Properties["id"]; !ok {
		t.Errorf("a.User = %+v, want package a's User with an id field", aUser)
	}
	bUser, ok := doc.Components.Schemas["b.User"]
	if !ok {
		t.Fatalf("Components = %+v, missing b.User", doc.Components.Schemas)
	}
	if _, ok := bUser.Properties["name"]; !ok {
		t.Errorf("b.User = %+v, want package b's User with a name field", bUser)
	}
}

// TestResolveSchemaRefs_QualifiedGenericBase pins that componentName
// keys the ambiguity check on the bare DECLARED name: a generic base
// ("Box") declared in two packages qualifies each instantiation to
// "<pkg>.Box_<Arg>", and the resolver's package-qualified branch scopes
// the base to that package while resolving the arg globally.
func TestResolveSchemaRefs_QualifiedGenericBase(t *testing.T) {
	pkgs := loadFixture(t, "schema_ambiguous_generic")

	ambiguous := AmbiguousSchemaNames(pkgs)
	if !ambiguous["Box"] {
		t.Fatalf("AmbiguousSchemaNames = %+v, want it to flag the generic base Box", ambiguous)
	}

	doc := docWithTwoRefs("a.Box_User", "b.Box_User")
	if err := ResolveSchemaRefs(doc, pkgs, ambiguous); err != nil {
		t.Fatalf("ResolveSchemaRefs: %v", err)
	}

	aBox, ok := doc.Components.Schemas["a.Box_User"]
	if !ok {
		t.Fatalf("Components = %+v, missing a.Box_User", doc.Components.Schemas)
	}
	// a.Box[User]: its Item field is User -> a $ref to a.User (arg is
	// package a's User, qualified because "User" isn't ambiguous here...
	// actually User is only in package a, so it stays bare "User").
	if item := aBox.Properties["item"]; item == nil || item.Ref != schemaRefPrefix+"User" {
		t.Errorf("a.Box_User.item = %+v, want a $ref to the substituted User", item)
	}
	bBox, ok := doc.Components.Schemas["b.Box_User"]
	if !ok {
		t.Fatalf("Components = %+v, missing b.Box_User", doc.Components.Schemas)
	}
	if _, ok := bBox.Properties["label"]; !ok {
		t.Errorf("b.Box_User = %+v, want package b's Box with a label field", bBox)
	}
}

// TestResolveSchemaRefs_SamePackageNameCollisionErrors pins the residual
// the qualification can't break: two packages that share a package NAME
// (both "config") at different paths, each declaring Settings, both
// qualify to "config.Settings". register's origin guard must error
// (naming both import paths), not silently overwrite one schema with the
// other.
func TestResolveSchemaRefs_SamePackageNameCollisionErrors(t *testing.T) {
	pkgs := loadFixture(t, "schema_ambiguous_samepkg")

	ambiguous := AmbiguousSchemaNames(pkgs)
	doc := docWithTwoRefs("config.Settings", "config.Settings")
	err := ResolveSchemaRefs(doc, pkgs, ambiguous)
	if err == nil {
		t.Fatal("expected an error: two distinct config.Settings collide on one component key")
	}
	if !strings.Contains(err.Error(), "config.Settings") {
		t.Errorf("error %q should name the colliding component", err.Error())
	}
}

func TestResolveSchemaRefs_NoRefsIsANoop(t *testing.T) {
	pkgs := loadFixture(t, "schema_empty")

	doc := &model.Document{Paths: model.Paths{"/x": &model.PathItem{Get: &model.Operation{}}}}
	if err := ResolveSchemaRefs(doc, pkgs, nil); err != nil {
		t.Fatalf("ResolveSchemaRefs: %v", err)
	}
	if doc.Components != nil {
		t.Errorf("Components = %+v, want nil when nothing references a schema", doc.Components)
	}
}

// TestResolveSchemaRefs_GenericInstantiation pins down that a $ref using
// componentName's "<Generic>_<Arg>" convention (the same one body.go's
// bare inference produces, and that a hand-written "gota:" comment can
// use directly) resolves to a correctly-substituted component — Data's
// field type must be User, not the unresolved type parameter T.
func TestResolveSchemaRefs_GenericInstantiation(t *testing.T) {
	pkgs := loadFixture(t, "schema_generics")

	doc := docWithRef("Response_User")
	if err := ResolveSchemaRefs(doc, pkgs, nil); err != nil {
		t.Fatalf("ResolveSchemaRefs: %v", err)
	}

	resp, ok := doc.Components.Schemas["Response_User"]
	if !ok {
		t.Fatalf("Response_User component not registered: %+v", doc.Components)
	}
	data, ok := resp.Properties["data"]
	if !ok {
		t.Fatalf("Response_User.Properties = %+v, missing data", resp.Properties)
	}
	if data.Ref != schemaRefPrefix+"User" {
		t.Errorf("data property = %+v, want a $ref to User (the substituted type, not the type parameter T)", data)
	}
	if _, ok := doc.Components.Schemas["User"]; !ok {
		t.Errorf("User was not registered as its own linked component: %+v", doc.Components.Schemas)
	}
}

// TestResolveSchemaRefs_GenericInstantiationsDontCollide is the direct
// regression test for the bug this feature fixes: Response[User] and
// Response[Product] used to collide on a single "Response" component
// with an empty (untyped) data field. Both must now resolve to their
// own distinct, correctly-typed component.
func TestResolveSchemaRefs_GenericInstantiationsDontCollide(t *testing.T) {
	pkgs := loadFixture(t, "schema_generics")

	doc := docWithTwoRefs("Response_User", "Response_Product")
	if err := ResolveSchemaRefs(doc, pkgs, nil); err != nil {
		t.Fatalf("ResolveSchemaRefs: %v", err)
	}

	respUser, ok := doc.Components.Schemas["Response_User"]
	if !ok {
		t.Fatalf("Response_User component not registered: %+v", doc.Components)
	}
	respProduct, ok := doc.Components.Schemas["Response_Product"]
	if !ok {
		t.Fatalf("Response_Product component not registered: %+v", doc.Components)
	}
	if respUser.Properties["data"].Ref != schemaRefPrefix+"User" {
		t.Errorf("Response_User.data = %+v, want a $ref to User", respUser.Properties["data"])
	}
	if respProduct.Properties["data"].Ref != schemaRefPrefix+"Product" {
		t.Errorf("Response_Product.data = %+v, want a $ref to Product", respProduct.Properties["data"])
	}
	if _, ok := doc.Components.Schemas["User"]; !ok {
		t.Errorf("User was not registered as its own linked component")
	}
	if _, ok := doc.Components.Schemas["Product"]; !ok {
		t.Errorf("Product was not registered as its own linked component")
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
