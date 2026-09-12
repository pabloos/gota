# Schema & body inference

How gota derives request/response schemas, types and operation bodies from
Go types and `encoding/json` usage. Contributor reference; to *use* gota,
see the [top-level README](../../README.md).

A `gota:` comment can declare `schema: {$ref: '#/components/schemas/User'}`,
and gota generates that component automatically from the matching Go
struct — fields, primitive types, `omitempty` → required/optional, nested
structs as their own linked components, slices, maps, `time.Time`,
embedding. A `[]byte` field is a base64 `string` (`format: byte`), matching
how `encoding/json` encodes it. A type with its own `MarshalJSON` (a
`json.Marshaler`, e.g. `json.RawMessage` or `gorm.io/datatypes.JSON`)
controls its own wire format, so gota models it as a free-form `object`
rather than describing its underlying Go representation — `time.Time` is
the one such shape gota knows exactly. A **pointer** field can serialize to
JSON `null`, so it's modeled nullable (OpenAPI 3.1): a scalar becomes a type
array (`*string` → `type: [string, "null"]`), a referenced struct becomes
`anyOf: [{$ref}, {type: "null"}]`. This is independent of `required`, which
`omitempty` still governs. The type lookup searches every package under
`--dir`, not just the one containing the comment, so `User` can live in a
different package than the handler that references it.

Handlers with **no** `gota:` comment at all also get a best-effort
request/response schema, detected from the handler's own `encoding/json`
calls (`Decoder.Decode`, `Encoder.Encode`, `json.Unmarshal`/`Marshal`,
including slices, maps, and basic types like a bare string or int). A
map literal wrapping the real payload in an ad-hoc envelope — e.g.
`json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": user})`,
a common way to avoid a named response-envelope struct — gets an inline
`object` schema built from its actual keys (only when every key is a
compile-time constant; a dynamic key still produces a response, just a
bare `{type: object}` with no enumerated properties, rather than nothing
at all). Response detection tracks the *real* status code — it
walks the body respecting if/else branch boundaries, pairing each
`Encode`/`Marshal`/`http.Error` call with whatever `w.WriteHeader(<code>)`
was last called in its own branch (or 200, Go's implicit default, if none
was), so a handler with a 404 error branch and a 201 success branch gets
**both** documented as distinct responses, correctly numbered — not
everything collapsed into a generic `200`.

Detection follows a chain of calls — same package or not, up to four
calls deep — to a helper that itself calls
Decode/Encode/WriteHeader/http.Error, or delegates further to another
helper, treated as if it had all been inlined at the original call site.
A parameter reference inside a followed helper resolves back to whatever
expression was actually passed at its own call site, transitively
through as many levels as it takes — e.g. a shared response library
where `Success(w, status, data)` wraps `data` in a map-literal envelope
and delegates the actual write to `JSON(w, status, payload)` in the same
package, called from a handler in a completely different package,
resolves all the way through to the handler's own value. This was tested
against two real, representative Go HTTP APIs on GitHub: one (stdlib
`net/http` only) where every handler routed its output through a single
same-package `respond(w, code, data)` helper — before following, every
response came out as a generic `200` with no schema; after, error and
success responses alike get their real status codes and schemas. The
other (13 resource modules) mixed the map-literal-envelope case above
with exactly the cross-package, two-level `Success`/`JSON` shape — before
cross-package/multi-level following, four of its POST endpoints still
had no response schema at all despite the map-literal fix; after, all
four resolve to the real payload type three frames up.

All of this is a heuristic over common idioms, not a dataflow analysis:
following stops after four calls, a helper already earlier in the
current chain is declined immediately rather than followed into a cycle,
a helper with a variadic parameter or an argument-count mismatch isn't
followed at all, a `WriteHeader` call whose code isn't a compile-time
constant is ignored, and if the *same* status code is produced more than
once the last occurrence in source order wins (covers the common "if err
!= nil {...; return }; ..." shape). A `gota:` comment always overrides
whatever this detects.

Schema component names are disambiguated automatically. A type name
declared in more than one analyzed package is package-qualified
(`author.Widget` / `book.Widget`), and so is a type from a **dependency**
or another `go.work` module (`repository.Event`) — so it resolves by its
own package and its real fields are expanded, rather than colliding on a
bare name with an unrelated same-named type elsewhere in the reachable
graph (a hand-written `$ref` can qualify a name the same way). A reference
that genuinely can't be resolved to a single type — a `$ref` typo, or a
qualified name that's still ambiguous — never aborts the document: gota
emits that one reference as a generic `{type: object}` (warning on stderr)
and generates the rest of the spec.

A handler can be registered from a different package than the one that
declares it (`mux.HandleFunc("/x", handlers.GetUser)`) — gota resolves
the `gota:` comment and best-effort body inference across that boundary
the same as if it were local, via a `go/types`-object index spanning
every analyzed package. This only reaches packages within the module
being analyzed (`--dir`), not external dependencies: a handler imported
from a third-party module is left unresolved the same way any other
unresolvable handler is — the route is still documented, just without a
comment or inferred body — rather than gota reading source code outside
the project it was pointed at.

A generic type instantiated with exactly one named-struct type argument
— the common "response envelope" idiom, `Response[User]` — gets its own
component per instantiation, named `<Generic>_<Arg>` (`Response_User`),
instead of every instantiation colliding on one `Response` component
with an untyped field. This is inferred automatically from the handler's
own `Encode`/`Marshal` call, the same as any other best-effort body
detection, and the same `<Generic>_<Arg>` name works in a hand-written
`gota:` comment's `$ref` too — it's a real, resolvable name, not
gota-internal syntax. Scoped deliberately to one type parameter
instantiated with a named type: a basic-type argument (`Response[string]`),
a 2+-type-parameter generic, or referencing the bare unparameterized
generic name by itself (`$ref: '#/components/schemas/Response'`, with no
instantiation specified) all fall back to today's behavior rather than
being newly resolved.
