package inference

import (
	"fmt"
	"go/types"
	"reflect"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/pkg/model"
)

const schemaRefPrefix = "#/components/schemas/"

// ResolveSchemaRefs finds every "$ref: '#/components/schemas/<Name>'" a
// "gota:" comment declared anywhere in doc, and generates the referenced
// component by looking up a Go type named <Name> in pkgs and converting
// its structure into an OpenAPI Schema (fields, primitive types,
// omitempty -> required/optional, time.Time, slices, maps, embedding).
// Nested named struct types discovered along the way are registered as
// their own linked components rather than inlined, so the type graph in
// Go becomes a $ref graph in the spec.
//
// It returns an error if a comment references a schema name with no
// matching Go type anywhere in pkgs, or if that name is declared in more
// than one analyzed package (gota has no syntax to disambiguate which one
// was meant, so it refuses to guess). It does not attempt any inference
// for operations that never declare a $ref.
func ResolveSchemaRefs(doc *model.Document, pkgs []*packages.Package) error {
	names := map[string]bool{}
	for _, item := range doc.Paths {
		for _, op := range item.Operations() {
			walkOperationSchemas(op, func(s *model.Schema) {
				if name, ok := refName(s.Ref); ok {
					names[name] = true
				}
			})
		}
	}
	if len(names) == 0 {
		return nil
	}

	reg := &registry{schemas: map[string]*model.Schema{}}
	for name := range names {
		if err := reg.resolveByName(name, pkgs); err != nil {
			return err
		}
	}

	if doc.Components == nil {
		doc.Components = &model.Components{}
	}
	if doc.Components.Schemas == nil {
		doc.Components.Schemas = map[string]*model.Schema{}
	}
	for name, s := range reg.schemas {
		doc.Components.Schemas[name] = s
	}
	return nil
}

func refName(ref string) (string, bool) {
	name, ok := strings.CutPrefix(ref, schemaRefPrefix)
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

// walkOperationSchemas visits every Schema reachable from op: parameter
// schemas, the request body's schema, and every response's schema.
func walkOperationSchemas(op *model.Operation, visit func(*model.Schema)) {
	if op == nil {
		return
	}
	for _, p := range op.Parameters {
		walkSchema(p.Schema, visit)
	}
	if op.RequestBody != nil {
		for _, mt := range op.RequestBody.Content {
			walkSchema(mt.Schema, visit)
		}
	}
	for _, resp := range op.Responses {
		for _, mt := range resp.Content {
			walkSchema(mt.Schema, visit)
		}
	}
}

// walkSchema visits s and recurses into every nested schema a "gota:"
// comment could have declared inline (items, properties, additionalProperties).
func walkSchema(s *model.Schema, visit func(*model.Schema)) {
	if s == nil {
		return
	}
	visit(s)
	walkSchema(s.Items, visit)
	walkSchema(s.AdditionalProperties, visit)
	for _, prop := range s.Properties {
		walkSchema(prop, visit)
	}
}

// registry accumulates generated component schemas by name, generating
// each one at most once and guarding against infinite recursion on
// self-referential (or mutually referential) struct types.
type registry struct {
	schemas    map[string]*model.Schema
	inProgress map[string]bool
}

// resolveByName is the $ref entry point: name comes from a "$ref:
// '#/components/schemas/<name>'" string in a "gota:" comment, so the
// matching Go type must be looked up by name across pkgs.
func (r *registry) resolveByName(name string, pkgs []*packages.Package) error {
	if _, ok := r.schemas[name]; ok {
		return nil
	}
	named, found, err := lookupType(name, pkgs)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("gota: comment references schema %q but no Go type named %q was found in the analyzed packages", name, name)
	}
	r.register(named)
	return nil
}

// componentName returns named's OpenAPI component name: its own declared
// name, or — for a generic type instantiated with exactly one
// named-struct type argument (Response[User]) — a synthesized
// "<Generic>_<Arg>" name, so distinct instantiations of the same generic
// don't collide on one component. Anything else (no instantiation, 2+
// type parameters, or a non-named type argument) falls back to the bare
// declared name.
func componentName(named *types.Named) string {
	base := named.Obj().Name()
	targs := named.TypeArgs()
	if targs == nil || targs.Len() != 1 {
		return base
	}
	argNamed, ok := targs.At(0).(*types.Named)
	if !ok {
		return base
	}
	return base + "_" + argNamed.Obj().Name()
}

// register ensures named's schema is generated and stored under its own
// name, recursively registering any named struct types its fields
// reference, and returns a $ref schema pointing to it. It's safe to call
// repeatedly (idempotent) and safe against reference cycles.
func (r *registry) register(named *types.Named) *model.Schema {
	name := componentName(named)
	ref := &model.Schema{Ref: schemaRefPrefix + name}

	if _, done := r.schemas[name]; done {
		return ref
	}
	if r.inProgress[name] {
		return ref
	}
	if r.inProgress == nil {
		r.inProgress = map[string]bool{}
	}

	r.inProgress[name] = true
	s := r.schemaForType(named.Underlying())
	delete(r.inProgress, name)
	r.schemas[name] = s

	return ref
}

// schemaForType converts a go/types.Type into an OpenAPI Schema. Named
// struct types are registered as their own component and returned as a
// $ref rather than inlined, so the Go type graph becomes a $ref graph.
func (r *registry) schemaForType(t types.Type) *model.Schema {
	switch tt := t.(type) {
	case *types.Named:
		if isTimeTime(tt) {
			return &model.Schema{Type: "string", Format: "date-time"}
		}
		if _, isStruct := tt.Underlying().(*types.Struct); isStruct {
			return r.register(tt)
		}
		// A named non-struct type (e.g. "type UserID string") has no
		// object shape of its own worth a separate component; resolve it
		// to its underlying representation directly.
		return r.schemaForType(tt.Underlying())
	case *types.Pointer:
		// Nullability isn't modeled yet (known simplification) — a
		// pointer field just resolves to its pointee's schema.
		return r.schemaForType(tt.Elem())
	case *types.Slice:
		return &model.Schema{Type: "array", Items: r.schemaForType(tt.Elem())}
	case *types.Array:
		return &model.Schema{Type: "array", Items: r.schemaForType(tt.Elem())}
	case *types.Map:
		if basic, ok := tt.Key().Underlying().(*types.Basic); !ok || basic.Kind() != types.String {
			// A non-string-keyed map has no direct JSON object
			// representation; degrade to an untyped object rather than
			// erroring on something that isn't fully representable.
			return &model.Schema{Type: "object"}
		}
		return &model.Schema{Type: "object", AdditionalProperties: r.schemaForType(tt.Elem())}
	case *types.Struct:
		return r.schemaForStruct(tt)
	case *types.Basic:
		return schemaForBasic(tt)
	default:
		// chan, func, interface, unsafe.Pointer, etc. have no JSON Schema
		// equivalent; degrade to an untyped schema ("any value") instead
		// of erroring.
		return &model.Schema{}
	}
}

// schemaForStruct builds an "object" schema from a struct's exported
// fields, honoring "json" struct tags: a "-" tag skips the field, a name
// override renames the property, and the presence of ",omitempty"
// determines whether the field is required. Embedded struct fields have
// their properties promoted into the parent, matching how encoding/json
// flattens anonymous fields by default.
func (r *registry) schemaForStruct(s *types.Struct) *model.Schema {
	out := &model.Schema{Type: "object", Properties: map[string]*model.Schema{}}

	for i := 0; i < s.NumFields(); i++ {
		field := s.Field(i)
		if !field.Exported() {
			continue
		}

		if field.Embedded() {
			if embedded, ok := embeddedStruct(field.Type()); ok {
				es := r.schemaForStruct(embedded)
				for k, v := range es.Properties {
					out.Properties[k] = v
				}
				out.Required = append(out.Required, es.Required...)
				continue
			}
			// An embedded field we don't know how to flatten (e.g. an
			// embedded interface or basic type): treat it like an
			// ordinary named field instead of silently dropping it.
		}

		name, skip, omitempty := parseJSONTag(reflect.StructTag(s.Tag(i)).Get("json"), field.Name())
		if skip {
			continue
		}
		out.Properties[name] = r.schemaForType(field.Type())
		if !omitempty {
			out.Required = append(out.Required, name)
		}
	}

	if len(out.Required) == 0 {
		out.Required = nil
	}
	return out
}

// embeddedStruct unwraps an embedded field's type (following one pointer
// indirection, e.g. an embedded "*Base") down to its *types.Struct, if any.
func embeddedStruct(t types.Type) (*types.Struct, bool) {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return nil, false
	}
	st, ok := named.Underlying().(*types.Struct)
	return st, ok
}

// parseJSONTag mirrors encoding/json's tag semantics closely enough for
// schema generation: name is the JSON property name and skip reports a
// "json:\"-\"" tag (a literal "-" name is "json:\"-,\"" and is not treated
// as skip, matching encoding/json).
func parseJSONTag(tag, fieldName string) (name string, skip bool, omitempty bool) {
	if tag == "" {
		return fieldName, false, false
	}
	if tag == "-" {
		return "", true, false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = fieldName
	}
	for _, opt := range parts[1:] {
		if opt == "omitempty" {
			omitempty = true
		}
	}
	return name, false, omitempty
}

func schemaForBasic(b *types.Basic) *model.Schema {
	switch b.Kind() {
	case types.String:
		return &model.Schema{Type: "string"}
	case types.Bool:
		return &model.Schema{Type: "boolean"}
	case types.Int8, types.Int16, types.Int32, types.Uint8, types.Uint16, types.Uint32:
		return &model.Schema{Type: "integer", Format: "int32"}
	case types.Int, types.Int64, types.Uint, types.Uint64:
		return &model.Schema{Type: "integer", Format: "int64"}
	case types.Float32:
		return &model.Schema{Type: "number", Format: "float"}
	case types.Float64:
		return &model.Schema{Type: "number", Format: "double"}
	default:
		// complex64/128, uintptr: rare in JSON APIs, no clean mapping.
		return &model.Schema{Type: "string"}
	}
}

func isTimeTime(named *types.Named) bool {
	obj := named.Obj()
	pkg := obj.Pkg()
	return pkg != nil && pkg.Path() == "time" && obj.Name() == "Time"
}

// lookupType resolves a $ref name to a Go type: name itself first
// (lookupTypeByName), and — only on a miss — as a possible
// componentName-convention generic instantiation ("<Generic>_<Arg>", see
// lookupInstantiatedType). Trying the direct lookup first means an
// actual Go type that just happens to have an underscore in its name is
// never shadowed by the generics convention.
func lookupType(name string, pkgs []*packages.Package) (named *types.Named, found bool, err error) {
	named, found, err = lookupTypeByName(name, pkgs)
	if found || err != nil {
		return named, found, err
	}
	return lookupInstantiatedType(name, pkgs)
}

// lookupTypeByName searches every package in pkgs for a top-level type
// named name. err is non-nil only when name is declared in more than one
// package — found is false (with no error) when it's declared in none.
func lookupTypeByName(name string, pkgs []*packages.Package) (named *types.Named, found bool, err error) {
	var matches []*types.Named
	var pkgPaths []string
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		obj := pkg.Types.Scope().Lookup(name)
		if obj == nil {
			continue
		}
		tn, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		n, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		matches = append(matches, n)
		pkgPaths = append(pkgPaths, pkg.PkgPath)
	}
	switch len(matches) {
	case 0:
		return nil, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return nil, false, fmt.Errorf(
			"gota: schema name %q is ambiguous: a type named %q is declared in more than one analyzed package (%s) — rename one of them so the $ref is unambiguous",
			name, name, strings.Join(pkgPaths, ", "),
		)
	}
}

// lookupInstantiatedType parses name as "<Generic>_<Arg>"
// (componentName's convention for a single-type-parameter generic
// instantiation, e.g. "Response_User" for Response[User]) and, if
// Generic really is a single-type-parameter generic type and Arg a real
// type, returns the properly instantiated (fields substituted)
// *types.Named — the same object schemaForType/register would build
// from a live Response[User] expression. found is false, with no error,
// whenever name doesn't fit this shape — most names, including any type
// that just happens to contain an underscore — so callers fall through
// to their normal "not found" handling, not treat this as authoritative.
func lookupInstantiatedType(name string, pkgs []*packages.Package) (named *types.Named, found bool, err error) {
	idx := strings.IndexByte(name, '_')
	if idx <= 0 || idx == len(name)-1 {
		return nil, false, nil
	}
	baseName, argName := name[:idx], name[idx+1:]

	base, found, err := lookupTypeByName(baseName, pkgs)
	if err != nil {
		return nil, false, err
	}
	if !found || base.TypeParams() == nil || base.TypeParams().Len() != 1 {
		return nil, false, nil
	}

	arg, found, err := lookupTypeByName(argName, pkgs)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}

	inst, err := types.Instantiate(nil, base, []types.Type{arg}, true)
	if err != nil {
		return nil, false, fmt.Errorf("gota: instantiating generic type %q with %q: %w", baseName, argName, err)
	}
	instNamed, ok := inst.(*types.Named)
	if !ok {
		return nil, false, nil
	}
	return instNamed, true, nil
}
