package inference

import (
	"fmt"
	"go/types"
	"os"
	"reflect"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/pabloos/gota/pkg/model"
)

const schemaRefPrefix = "#/components/schemas/"

// ResolveSchemaRefs finds every "$ref: '#/components/schemas/<Name>'"
// declared anywhere in doc (whether written by a "gota:" comment or
// produced by body inference), and generates the referenced component
// by looking up the matching Go type in pkgs and converting its
// structure into an OpenAPI Schema (fields, primitive types, omitempty
// -> required/optional, time.Time, slices, maps, embedding). Nested
// named struct types discovered along the way are registered as their
// own linked components rather than inlined, so the type graph in Go
// becomes a $ref graph in the spec.
//
// ambiguous is the set of type names declared in more than one analyzed
// package (see AmbiguousSchemaNames); it must be the SAME set the
// inference-time producers used, so that a package-qualified $ref they
// emitted (e.g. "author.UpdateRequest") is looked up and stored under
// the identical component key.
//
// A type is looked up in the analyzed root packages first, then — if not
// found there — in the full reachable import graph, so a $ref to a type
// declared in a dependency or another go.work module (a handler returning
// []*catalog.Product) resolves rather than aborting.
//
// It returns an error only if a $ref names a schema with no matching Go
// type anywhere in that graph (typically a typo in a hand-written
// comment — an inferred $ref is always to a type already in the graph);
// if a HAND-WRITTEN comment references a bare name that's declared in more
// than one package (gota can't tell which was meant — an inferred $ref for
// such a name is pre-qualified and doesn't hit this); or if two distinct
// types would collide on one component key even after qualification (see
// register). It does not attempt any inference for operations that never
// declare a $ref.
func ResolveSchemaRefs(doc *model.Document, pkgs []*packages.Package, ambiguous map[string]bool) error {
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

	// reachable is every package transitively imported from the analyzed
	// roots, for resolveByName's dependency fallback. packages.Visit walks
	// the whole import graph (roots included).
	var reachable []*packages.Package
	packages.Visit(pkgs, func(p *packages.Package) bool {
		reachable = append(reachable, p)
		return true
	}, nil)

	reg := &registry{schemas: map[string]*model.Schema{}, ambiguous: ambiguous, roots: RootPaths(pkgs), reachable: reachable}
	var unresolved []string
	for name := range names {
		if err := reg.resolveByName(name, pkgs); err != nil {
			// A name that can't be uniquely resolved must not abort the
			// whole document: warn and emit that one reference as a generic
			// object. This covers an inferred $ref to a bare
			// dependency-type name that collides across the reachable graph
			// (only the type a handler actually returns is meant, but the
			// bare name alone can't say which) and a hand-written $ref typo.
			fmt.Fprintf(os.Stderr, "gota: warning: %v — emitting it as a generic object schema; if the name came from a \"gota:\" comment, qualify it (pkg.Name) to pin it\n", err)
			unresolved = append(unresolved, name)
		}
	}
	if reg.err != nil {
		// A residual collision — two DISTINCT types that even
		// package-qualification maps to one component key (two packages
		// sharing a name at different paths) — stays fatal: it's rare and
		// genuinely actionable (rename one), unlike a bare-name ambiguity
		// that gota resolves or degrades on its own.
		return reg.err
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
	for _, name := range unresolved {
		degradeRef(doc, name)
	}
	return nil
}

// degradeRef replaces every "$ref: '#/components/schemas/<name>'" in doc's
// operation schemas with a bare {type: object}, for a name that couldn't
// be resolved to a component — keeping the document valid instead of
// leaving a dangling $ref that emitter.Validate would reject. Only
// operation-level refs need this: a nested field type is registered by
// object identity while its parent resolves, never by this name lookup.
func degradeRef(doc *model.Document, name string) {
	target := schemaRefPrefix + name
	for _, item := range doc.Paths {
		for _, op := range item.Operations() {
			walkOperationSchemas(op, func(s *model.Schema) {
				if s.Ref == target {
					s.Ref = ""
					s.Type = "object"
				}
			})
		}
	}
}

// AmbiguousSchemaNames returns the set of top-level type names declared
// in more than one of pkgs. componentName qualifies exactly these with
// their package name (e.g. "author.UpdateRequest") so two distinct Go
// types sharing a name don't collide on one OpenAPI component. It's
// computed once and threaded to every schema-name producer (body
// inference and this package's registry) so the emitted $ref string and
// the stored component key always agree.
func AmbiguousSchemaNames(pkgs []*packages.Package) map[string]bool {
	counts := map[string]int{}
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, n := range scope.Names() {
			tn, ok := scope.Lookup(n).(*types.TypeName)
			if !ok {
				continue
			}
			if _, ok := tn.Type().(*types.Named); ok {
				counts[n]++
			}
		}
	}
	ambiguous := map[string]bool{}
	for n, c := range counts {
		if c > 1 {
			ambiguous[n] = true
		}
	}
	return ambiguous
}

// RootPaths returns the set of import paths of the analyzed root packages
// (pkgs). componentName qualifies a type whose package is NOT in this set
// (a dependency or other-module type), so it resolves uniquely by package
// instead of by a bare name that can collide across the reachable graph.
// generate.Run computes it once and passes it to DetectBodyWithRoots;
// ResolveSchemaRefs derives it the same way from the same pkgs, so
// emission and resolution agree on which names are qualified.
func RootPaths(pkgs []*packages.Package) map[string]bool {
	roots := make(map[string]bool, len(pkgs))
	for _, pkg := range pkgs {
		roots[pkg.PkgPath] = true
	}
	return roots
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
	ambiguous  map[string]bool     // names to package-qualify, see AmbiguousSchemaNames
	roots      map[string]bool     // analyzed root package import paths; a type outside them is package-qualified (see componentName), matching what emission produced
	origin     map[string]string   // component key -> the package path that produced it, for the collision guard
	reachable  []*packages.Package // the analyzed roots plus every package transitively imported, for the dependency fallback
	err        error               // first residual-collision error (two distinct types on one key), checked by ResolveSchemaRefs
}

// resolveByName is the $ref entry point: name comes from a "$ref:
// '#/components/schemas/<name>'" string — written in a "gota:" comment or
// produced by body inference — so the matching Go type is looked up by
// name. It searches the analyzed root packages first; if the name isn't
// there it falls back to the full reachable graph (dependencies and other
// go.work modules), so a handler returning a type declared in a different
// module — e.g. []*catalog.Product — resolves to a real component
// instead of aborting. Once the *types.Named is found, register expands
// the whole type tree by object identity, no further name lookup.
func (r *registry) resolveByName(name string, pkgs []*packages.Package) error {
	if _, ok := r.schemas[name]; ok {
		return nil
	}
	named, found, err := lookupType(name, pkgs)
	if err != nil {
		return err
	}
	if !found {
		if named, found, err = lookupType(name, r.reachable); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("schema %q could not be resolved: no Go type named %q was found in the analyzed packages or their dependencies", name, name)
	}
	r.register(named)
	return nil
}

// componentName returns named's OpenAPI component name: its own declared
// name, or — for a generic type instantiated with exactly one
// named-struct type argument (Response[User]) — a synthesized
// "<Generic>_<Arg>" name, so distinct instantiations of the same generic
// don't collide on one component. When the declared name is in ambiguous
// (declared in more than one analyzed package), the result is prefixed
// with the package name and a "." separator ("author.UpdateRequest",
// "author.Response_User") so two packages' same-named types don't
// collide. "." is chosen to never clash with the generics "_": a
// qualified name has exactly one ".", split cleanly on the first one by
// the resolver (lookupPackageQualifiedType). Keying the check on the
// bare declared name (not the "_"-composed one) is what makes a
// colliding generic BASE qualify.
func componentName(named *types.Named, ambiguous, roots map[string]bool) string {
	base := named.Obj().Name()
	name := base
	if targs := named.TypeArgs(); targs != nil && targs.Len() == 1 {
		if argNamed, ok := targs.At(0).(*types.Named); ok {
			name = base + "_" + argNamed.Obj().Name()
		}
	}
	pkg := named.Obj().Pkg()
	// Qualify when the bare name is ambiguous among analyzed roots, OR when
	// the type comes from a DEPENDENCY (a package outside roots): a
	// dependency type's bare name can collide with an unrelated same-named
	// type anywhere in the reachable graph, so qualifying it by package
	// (repository.Event) makes it resolve uniquely via its own package
	// (lookupPackageQualifiedType) instead of ambiguously by bare name. A
	// nil roots (the standalone DetectBody path) qualifies only the
	// ambiguous case — every type is treated as a root.
	if pkg != nil && (ambiguous[base] || (roots != nil && !roots[pkg.Path()])) {
		return pkg.Name() + "." + name
	}
	return name
}

// pkgPathOf returns named's declaring package import path (unique across
// the whole load), or "" for a package-less type. Used as the origin
// key for register's collision guard.
func pkgPathOf(named *types.Named) string {
	if pkg := named.Obj().Pkg(); pkg != nil {
		return pkg.Path()
	}
	return ""
}

// register ensures named's schema is generated and stored under its
// component name, recursively registering any named struct types its
// fields reference, and returns a $ref schema pointing to it. It's safe
// to call repeatedly (idempotent) and safe against reference cycles.
//
// If a DIFFERENT type (a different declaring package) has already
// claimed the same component name — two types that even
// package-qualification can't tell apart, e.g. two packages that share
// a name AND declare the same type name — it records the first such
// collision on r.err and does NOT overwrite, so the loud error surfaces
// via ResolveSchemaRefs rather than silently dropping one type's schema.
func (r *registry) register(named *types.Named) *model.Schema {
	name := componentName(named, r.ambiguous, r.roots)
	ref := &model.Schema{Ref: schemaRefPrefix + name}

	path := pkgPathOf(named)
	if prev, seen := r.origin[name]; seen {
		if prev != path && r.err == nil {
			r.err = fmt.Errorf(
				"gota: schema component %q is produced by two distinct types (declared in %s and %s) that package-qualification can't tell apart — rename one of them",
				name, prev, path,
			)
		}
		return ref // same origin: idempotent (done or mid-cycle); different: error recorded, don't overwrite
	}
	if r.origin == nil {
		r.origin = map[string]string{}
	}
	if r.inProgress == nil {
		r.inProgress = map[string]bool{}
	}
	r.origin[name] = path
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

// lookupType resolves a $ref name to a Go type. A name containing a "."
// is a package-qualified componentName ("author.UpdateRequest", see
// lookupPackageQualifiedType) — the shape body inference emits for a
// type whose bare name collides across packages. Otherwise the bare
// name is tried first (lookupTypeByName) and, only on a miss, as a
// possible "<Generic>_<Arg>" generic instantiation (lookupInstantiatedType).
// Trying the direct lookup before the generic convention means an actual
// Go type that just happens to have an underscore in its name is never
// shadowed.
func lookupType(name string, pkgs []*packages.Package) (named *types.Named, found bool, err error) {
	if i := strings.IndexByte(name, '.'); i > 0 && i < len(name)-1 {
		return lookupPackageQualifiedType(name[:i], name[i+1:], pkgs)
	}
	named, found, err = lookupTypeByName(name, pkgs)
	if found || err != nil {
		return named, found, err
	}
	return lookupInstantiatedType(name, pkgs)
}

// lookupPackageQualifiedType resolves a "<pkgName>.<rest>" component name
// back to its type: it finds the analyzed package whose name is pkgName
// and resolves rest within that package — a bare type, or a
// "<Base>_<Arg>" generic whose base is scoped to that package (the arg
// stays looked up globally, the same package-blind limit the generics
// convention already has). More than one analyzed package can share a
// name; if rest resolves in two of them, that's a residual collision the
// qualification couldn't break, and it errors rather than guessing.
func lookupPackageQualifiedType(pkgName, rest string, pkgs []*packages.Package) (named *types.Named, found bool, err error) {
	var matches []*types.Named
	var pkgPaths []string
	for _, pkg := range pkgs {
		if pkg.Types == nil || pkg.Types.Name() != pkgName {
			continue
		}
		n, ok, err := resolveWithinPackage(pkg, rest, pkgs)
		if err != nil {
			return nil, false, err
		}
		if ok {
			matches = append(matches, n)
			pkgPaths = append(pkgPaths, pkg.PkgPath)
		}
	}
	switch len(matches) {
	case 0:
		return nil, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return nil, false, fmt.Errorf(
			"gota: schema name %q is ambiguous: %q resolves in more than one package named %q (%s) — rename one of them",
			pkgName+"."+rest, rest, pkgName, strings.Join(pkgPaths, ", "),
		)
	}
}

// resolveWithinPackage resolves rest against a single package: a bare
// top-level type, or a "<Base>_<Arg>" generic whose base is that
// package's own generic type (arg looked up globally, matching
// lookupInstantiatedType). Mirrors lookupType's bare-then-generic
// ordering so a real type literally named "<Base>_<Arg>" isn't shadowed.
func resolveWithinPackage(pkg *packages.Package, rest string, pkgs []*packages.Package) (named *types.Named, found bool, err error) {
	if n, ok := namedInScope(pkg, rest); ok {
		return n, true, nil
	}
	idx := strings.IndexByte(rest, '_')
	if idx <= 0 || idx == len(rest)-1 {
		return nil, false, nil
	}
	base, ok := namedInScope(pkg, rest[:idx])
	if !ok || base.TypeParams() == nil || base.TypeParams().Len() != 1 {
		return nil, false, nil
	}
	arg, found, err := lookupTypeByName(rest[idx+1:], pkgs)
	if err != nil || !found {
		return nil, false, err
	}
	inst, err := types.Instantiate(nil, base, []types.Type{arg}, true)
	if err != nil {
		return nil, false, fmt.Errorf("gota: instantiating generic type %q with %q: %w", rest[:idx], rest[idx+1:], err)
	}
	instNamed, ok := inst.(*types.Named)
	if !ok {
		return nil, false, nil
	}
	return instNamed, true, nil
}

// namedInScope looks up a top-level *types.Named in one package's scope.
func namedInScope(pkg *packages.Package, name string) (*types.Named, bool) {
	tn, ok := pkg.Types.Scope().Lookup(name).(*types.TypeName)
	if !ok {
		return nil, false
	}
	n, ok := tn.Type().(*types.Named)
	return n, ok
}

// lookupTypeByName searches every package in pkgs for a top-level type
// named name. err is non-nil only when name is declared in more than one
// package — found is false (with no error) when it's declared in none. An
// ambiguous name can be reached either by a hand-written "gota:" comment
// $ref or by an inferred $ref to a bare dependency-type name that happens
// to collide across the reachable graph; ResolveSchemaRefs treats the
// error as non-fatal (it degrades that one reference), so the message is
// origin-neutral and just lists the qualified candidates.
func lookupTypeByName(name string, pkgs []*packages.Package) (named *types.Named, found bool, err error) {
	var matches []*types.Named
	var qualified []string
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		n, ok := namedInScope(pkg, name)
		if !ok {
			continue
		}
		matches = append(matches, n)
		qualified = append(qualified, pkg.Types.Name()+"."+name)
	}
	switch len(matches) {
	case 0:
		return nil, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return nil, false, fmt.Errorf(
			"schema name %q is ambiguous: a type named %q is declared in more than one reachable package (candidates: %s)",
			name, name, strings.Join(qualified, ", "),
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
