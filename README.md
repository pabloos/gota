<p align="center">
  <img src="assets/gota-gopher.svg" alt="gota gopher mascot" width="140">
</p>

# gota

[![CI](https://github.com/pabloos/gota/actions/workflows/ci.yml/badge.svg)](https://github.com/pabloos/gota/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/pabloos/gota/graph/badge.svg)](https://codecov.io/gh/pabloos/gota)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

`gota` generates an OpenAPI 3.1 specification from Go source code. It's
code-first with real static inference, not annotation-only:

1. **AST inference** — routes, path parameters, handler names, and a
   handler's doc-comment prose as the operation description — everything
   deducible without programmer help, via `go/ast`, `go/types` and
   `golang.org/x/tools`.
2. **`gota:` comments** — structured YAML fragments written directly in
   OpenAPI vocabulary, placed above a handler. The comment *is* OpenAPI,
   not an intermediate DSL, which makes it easy for both humans and AI
   agents to write or edit.

Declared (comment) fields always win; inferred fields fill in the rest.

```go
// gota:
//   summary: Get a user by ID
//   parameters:
//     - name: id
//       in: path
//       required: true
//       schema:
//         type: integer
//   responses:
//     '200':
//       description: The requested user
func GetUser(w http.ResponseWriter, r *http.Request) { ... }
```

## Status

`net/http`, [Chi](https://github.com/go-chi/chi),
[Gin](https://github.com/gin-gonic/gin),
[Echo](https://github.com/labstack/echo), and
[gorilla/mux](https://github.com/gorilla/mux) are supported routers —
every plugin runs unconditionally against every analyzed package (no flag
needed to pick one; a plugin whose framework isn't present just
contributes no routes). `net/http`, Chi, and gorilla/mux share the same
net/http-based body inference, since their handlers are plain
`func(w http.ResponseWriter, r *http.Request)` with no response-writing
idiom of their own. Gin (`c.JSON(201, obj)`, `c.ShouldBindJSON(&x)`) and
Echo (`c.JSON(201, obj)`, `c.Bind(&x)`, `c.NoContent(204)`), whose
handlers write through a framework context, each bring their own
`inference.Dialect` that embeds the net/http one — so a handler that falls
back to `json.NewDecoder(...).Decode(&x)` is still understood.

Gin routes are read off the engine and off `Group` variables, whose
prefix is accumulated by object identity (`v1 := r.Group("/api/v1")` then
`v1.GET("/users/:id", H)` → `/api/v1/users/{id}`), transitively through
nested groups and inline-chained `r.Group("/v1").GET(...)`.
`Any`/`Handle`/`Match` expand to their concrete methods; a `:name` param
becomes `{name}`; a `.Use(mw)` middleware chain
(`g.Group("/x").Use(mw).POST(...)`) keeps the group's prefix. An inline
handler — `r.GET("/version", func(c *gin.Context){ c.JSON(200, v) })` —
is supported too: its body is inferred like any other, and since it has
no name, its operationId is synthesized from the method and path
(`GetVersion`). Because Gin group functions register *relative* paths,
a `func reg(rg *gin.RouterGroup){...}` register function has no
locally-recoverable prefix and its routes are declined (the opposite of
Chi's absolute-path constructors) — as are `*name` catch-alls, reassigned
group variables, and non-constant methods.

Echo works the same way, off `*echo.Echo` and `*echo.Group` — the one
structural difference from Gin is that Echo's handler sits at a *fixed*
argument position (`e.GET(path, handler, mw...)`, the handler right after
the path) rather than being the variadic last argument. `Any`/`Add`/
`Match` expand to their concrete methods (`TRACE` included, `CONNECT`
declined for want of an OpenAPI slot); groups accumulate their prefix by
object identity, inline and nested; `:name` → `{name}` and a `*` catch-all
is declined; an inline `func(c echo.Context) error` handler is supported.
Its dialect adds `c.NoContent(204)` as a bodyless response on top of the
shared `c.JSON`/`c.Bind` recognizers.

The Echo plugin also resolves the **register-function** layout that most
real Echo apps use — routes registered inside a function that takes an
`*echo.Group` parameter, e.g. `func (h *Users) Routes(g *echo.Group) {
g.GET("/users", …) }`. gota finds where such a function is called and gives
its group parameter the prefix of the group passed there, matched by call
name + argument position — so it resolves both a direct call
(`registerUsers(v1)`) and the interface-dispatch registry loop
(`for _, h := range hs { h.Routes(g) }`) that frameworks like
[pagoda](https://github.com/mikestefanello/pagoda) use, with nested groups
inside the function chaining onto the resolved prefix. A register function
whose only call site is in another package stays declined — the same
single-package boundary as chi's `Mount`. (This resolution is Echo-only for
now; the Gin plugin still declines its `func(rg *gin.RouterGroup)`
equivalent.)

gorilla/mux routes are read off `r.HandleFunc(path, h)` / `r.Handle(path,
h)`, with the HTTP methods taken from a `.Methods("GET", …)` chained onto
the returned `*mux.Route` — anywhere in the builder chain
(`.Schemes(…).Methods(…)`); a registration with no `.Methods()` matches
every method, like a method-less `net/http` pattern. Subrouters carry a
prefix by variable identity (`s := r.PathPrefix("/api/v1").Subrouter()`
then `s.HandleFunc("/users", H)` → `/api/v1/users`), transitively and
inline-chained; a `NewRoute().Subrouter()` (a middleware-only subrouter)
adds no segment but inherits the parent's prefix. A `{id:[0-9]+}`
constraint (and a `{rest:.*}` catch-all) degrades to the bare `{id}` /
`{rest}` — including a regex that itself contains braces, such as a
quantifier `{id:[a-z]{3}}` or a full anchored pattern
`{id:^sig_([a-zA-Z0-9]{22})$}`, whose closing brace is found by balancing,
not by stopping at the first `}`. The handler passed to `Handle`/`HandleFunc` is commonly the real
handler wrapped in middleware — `r.Handle("/x",
handlers.LoggingHandler(os.Stdout, H))`, `cors(opts)(H)`,
`authMiddleware(H)` — and body inference sees through it by following the
one argument at each layer whose type is `http.Handler`-shaped, down to
the real handler. A handler produced by a factory call returning
`http.Handler` — `r.Handle("/x", controller.build())` — is
recognized too, named by the factory. Router-level `r.Use(mw)` doesn't
wrap a specific handler and is irrelevant to inference. A same-package
sub-router constructor (a zero-arg `func() *mux.Router`) mounted on
another router has the mount prefix applied to its routes — via
`root.Handle("/v2", ctor())` (and mux's `"/v2/{rest:.*}"` subpath idiom,
deduped) or `root.PathPrefix("/v2").Handler(http.StripPrefix("/v2",
ctor()))`. The split builder form (`r.Path("/x").HandlerFunc(H)`), a
reassigned subrouter variable, and a non-constant method or path are
declined.

Chi's `Route`/`Group` nesting is followed to arbitrary depth,
accumulating the real path prefix (`r.Route("/users", func(r
chi.Router) { r.Get("/{id}", H) })` → `/users/{id}`). `Mount` is
followed for the common same-package, zero-argument constructor
function idiom (`func productsRouter() chi.Router {...}` /
`mx.Mount("/products", productsRouter())`) — a constructor declared in
a *different* package can't be resolved (route extraction only ever
sees one package at a time), and is left undetected rather than
guessed. A chi-specific regex-constrained param (`{id:[0-9]+}`)
degrades to a bare `{id}` (no direct OpenAPI equivalent); a bare
wildcard segment (`/admin/*`) has no OpenAPI path-template equivalent
at all and that registration is declined entirely.

`net/http` is the reference router, using Go 1.22+'s enhanced
`ServeMux` patterns (`"GET /users/{id}"`). A pattern with no method
(`mux.HandleFunc("/health", HealthCheck)`) matches every method per
net/http's own `ServeMux` docs, so gota expands it into one operation per
HTTP method it supports (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS,
TRACE — not CONNECT, which OpenAPI's Path Item Object has no slot for)
rather than assuming GET. If that expansion would make two or more
operations infer the same `operationId` (the guaranteed case: the
handler is the same for all of them), gota disambiguates by appending
the method and path rather than emitting an invalid document.

A method-less pattern registered against an anonymous function that
itself dispatches on `r.Method` — the common pre-Go-1.22 idiom for
method routing on one path — is also recognized, *not* expanded to every
method:
```go
mux.Handle("/products", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		CreateProduct(w, r)
	case http.MethodGet:
		ListProducts(w, r)
	default:
		methodNotAllowed(w)
	}
})))
```
splits into one operation per `case` (or `if`/`else if` branch), each
correctly bound to its real delegate handler — not the anonymous
closure — including that handler's own `gota:` comment, if any. This is
narrow, not a general control-flow analysis: the closure's entire body
must be exactly that one switch/if-else chain, and each branch exactly
one call. Any extra logic mixed into the dispatch (e.g. a path-substring
check alongside the method check) falls outside this and the
registration is left undetected, same as before this existed — not a
partial or incorrect guess.

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

Since gota documents every registered route by default (unlike swaggo,
which requires an annotation to appear at all), there's also
`x-gota-skip: true` — a real OpenAPI Specification Extension field, not an
invented directive — to opt back out. On a handler's own `gota:` comment it
excludes the whole operation from the emitted document:

```go
// gota:
//   x-gota-skip: true
func DebugInfo(w http.ResponseWriter, r *http.Request) { ... }
```

Placed on a `// gota:\n//   x-gota-skip: true` comment directly above a
specific statement inside a handler's body, it excludes just that one
detected response (e.g. an unpolished error path) without affecting the
rest of the handler's inferred responses — this only applies to response
detection, not request body detection.

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

### Document-level declarations (`gota:doc:`)

Some OpenAPI belongs to the whole document, not any single handler:
authentication (`components.securitySchemes` plus a global `security`
requirement), `servers`, top-level `tags`, a richer `info` block,
`externalDocs`, and `webhooks` (events the backend fires at
client-registered URLs — not routes any router exposes). None of it is
inferable from code. Declare it once in a `gota:doc:` block — raw
document-level OpenAPI, in any doc comment in the analyzed source (a
package comment is the natural home):

```go
// gota:doc:
//   info:
//     description: A signatures API secured with a bearer token.
//   security:
//     - BearerAuth: []
//   externalDocs:
//     description: Guides and tutorials
//     url: https://docs.example.com
//   webhooks:
//     signatureCertified:
//       post:
//         summary: Signature certified
//         requestBody:
//           content:
//             application/json:
//               schema:
//                 $ref: '#/components/schemas/Signature'
//         responses:
//           '200':
//             description: Acknowledged.
//   components:
//     securitySchemes:
//       BearerAuth:
//         type: http
//         scheme: bearer
//         bearerFormat: JWT
package main
```

`gota:doc:` overlays the generated document: its `info` fields override
the CLI-provided `--title`/`--version` and add a description; `servers`,
`security`, `tags`, `externalDocs`, `webhooks` and `jsonSchemaDialect` are
set when present; `securitySchemes` (and any manually declared `schemas`)
merge into `components` without clobbering schemas gota inferred from real
code. Every top-level field of the OpenAPI 3.1 root object is accepted; a
`$ref` inside a webhook operation resolves to its Go type exactly like one
in a path, and webhook operations are validated the same as paths. A key
gota doesn't recognize (a typo, or `paths`/`openapi`, which gota owns) is
reported on stderr rather than dropped in silence. A project normally has
one such block; if several appear they're merged in a deterministic order.
The marker is distinct from the per-handler `gota:` — a `gota:doc:` block
is never mistaken for an operation, and vice versa.

Per-operation `security` and per-response `example`/`examples` are
ordinary OpenAPI too, so they're written in a handler's own `gota:`
comment. Responses and the request body merge **field by field**: adding
an `example` under a `200` keeps the `schema` and `description` gota
inferred for that same response, and a response code the comment doesn't
mention is left in place — declared fields win only where they appear,
they never wipe an inferred sibling. The same field-level merge applies to
`parameters` (matched by their `in`+`name` identity): declaring a
`description` or a constrained `schema` — a `pattern`, `minLength`,
`minimum`, … — for the `{id}` path parameter gota already inferred
enriches it rather than replacing it, and the inferred `required` flag it
doesn't mention is kept. A `security:` requirement overrides the
document-level default for that one operation, and its scheme name
resolves against the `securitySchemes` declared in `gota:doc:` (the
generated document is validated as a whole, so a requirement naming an
undefined scheme is caught). An explicit empty `security: []` is honored
as written — it marks that operation public, overriding the global
default, and is emitted rather than dropped (distinct from omitting
`security` entirely, which inherits the default).

A handler's own doc comment is its description. gota already derives the
`summary` from the function name, so the prose a Go developer writes above
the handler — everything except the `gota:` block, paragraph breaks
preserved — becomes the operation `description`. A `description:` declared
in the `gota:` block still wins; the prose only fills the gap. A `gota:`
or `gota:doc:` block ends at the first blank comment line (or where the
prose returns to the left margin), so ordinary package/handler
documentation can sit in the same comment without being parsed as YAML.

Other router plugins — Fiber, for one — are not implemented yet; like Gin
and Echo, one would also need its own `inference.Dialect` (see the body
inference paragraph above), not just a router plugin.

## Usage

```sh
go run ./cmd/gota --dir ./path/to/project --out openapi.yaml
```

Flags:

| Flag            | Default        | Description                          |
|-----------------|----------------|---------------------------------------|
| `--dir`         | `.`            | Directory of the Go project to analyze |
| `--out`         | `openapi.yaml` | Output file (`.yaml`/`.yml` or `.json`) |
| `--title`       | directory name | API title                             |
| `--api-version` | `0.1.0`        | API version                           |

Try it against the bundled fixture:

```sh
go run ./cmd/gota --dir testdata/nethttp-basic --out /tmp/openapi.yaml
```

## Using gota in CI

Since the spec is derived from the code, the natural place to enforce
"the committed spec actually matches the code" is CI — regenerate it and
fail the build if that produces a diff, the same drift check you'd use
for any other generated file:

```yaml
name: openapi

on:
  push:
    branches: [main]
  pull_request:

jobs:
  spec:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: generate openapi.yaml
        run: go run github.com/pabloos/gota/cmd/gota@latest --dir . --out openapi.yaml

      - name: fail if the committed spec is stale
        run: git diff --exit-code openapi.yaml

      - uses: actions/upload-artifact@v4
        with:
          name: openapi
          path: openapi.yaml
```

`go run ./cmd/gota` exits non-zero on anything that would make the spec
wrong or incomplete — an ambiguous `$ref` (two packages declaring the
same schema name), an HTTP method OpenAPI has no operation slot for, or
a structural validation failure (`emitter.Validate`) — so this job also
doubles as a correctness gate, not just a formatting one.

A fresh, validated `openapi.yaml` artifact in CI is a natural input for
whatever comes after doc generation: publish it as a build artifact (as
above) for downstream jobs or other repos to consume, diff it against a
previous release to catch breaking API changes before they ship, or feed
it straight into a client generator — see below.

### Example: generating a Go client with oapi-codegen

[oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) reads the
spec gota writes and generates a typed Go client from it — a natural
next step once the spec exists, and something gota deliberately doesn't
do itself. Note that `oapi-codegen` itself needs a Go 1.25+ toolchain to
build — independent of whatever version your own project targets;
`go generate`/`go run` will fetch that automatically as long as CI has
network access.

`client/cfg.yaml`:

```yaml
package: client
output: client.gen.go
generate:
  models: true
  client: true
```

`client/generate.go`:

```go
package client

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config cfg.yaml ../openapi.yaml
```

Then, right after gota regenerates `openapi.yaml` in the CI job above:

```yaml
      - name: generate Go client from the spec
        run: go generate ./client/...
```

`client.gen.go` can be committed like any other generated file (with its
own drift check, the same pattern as `openapi.yaml` above) or uploaded as
a build artifact, depending on how your project already handles generated
code.

## Pipeline

```
Go source
   │
   ▼
Parsing (go/ast + go/types via golang.org/x/tools/go/packages)
   │
   ├── Extract "gota:" comment blocks  → declared Operation fragments
   └── Router plugin (net/http)        → routes (method, path, handler)
                                             │
                                             ▼
                       Static inference → baseline Operation (params,
                       default response, best-effort body from json calls)
                                             │
                                             ▼
                              Merge (comment wins, inferred fills gaps)
                                             │
                                             ▼
                    Resolve $ref schemas → components.schemas (go/types)
                                             │
                                             ▼
                          Validate (kin-openapi, independent of our model)
                                             │
                                             ▼
                            Emit OpenAPI 3.1 (YAML or JSON)
```

Validation is a structural sanity check, not a full OpenAPI 3.1 conformance
guarantee — see the doc comment on `emitter.Validate` for the known gap
(kin-openapi v0.135.0 models the Schema Object per OpenAPI 3.0 internally).
Because of that 3.0 model, the validation pass downgrades the 3.1-only
constructs gota emits — nullable type arrays / `type: null`, and top-level
`webhooks` — to a form kin accepts, so the check still runs over the rest;
the emitted document keeps the 3.1 form. It still catches real mistakes
nothing upstream prevents, like a `gota:` comment declaring a path parameter
without `required: true`.

## Testing

```sh
go test ./...
```

Aggregate coverage is tracked by the codecov badge above; the tables
below are about *what* each part's tests actually pin down, not how much.

Core engine — everything router-agnostic:

| Feature                          | What's tested                                                                                                                                              | Where |
|------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------|-------|
| `gota:` comment extraction        | Full blocks, gofmt-reformatted comments (tab-indent, inserted blank line), inline form, no block present, invalid YAML, prose preceding *and following* the block (the block is bounded, not run to the comment's end), doc-comment prose extracted as the description, `gota:doc:` document-level blocks (webhooks/externalDocs/jsonSchemaDialect captured, an unknown key surfaced for a warning) kept disjoint from per-handler `gota:` blocks in both directions | `internal/extractor/extractor_test.go` |
| Merge semantics                   | Declared fields always win, inferred fields fill gaps, inputs aren't mutated, `Deprecated` can't be unset once set, responses/request body/parameters merge field-by-field (a declared example keeps the inferred schema/description; a declared parameter schema/description enriches the inferred one by `in`+`name`), a declared `security` requirement is taken over an operation with none inferred, an explicit `security: []` round-trips | `internal/merger/merger_test.go` |
| Schema `$ref` resolution          | Primitives + `omitempty`→required, nested structs as linked components, slices, `time.Time`, `[]byte`→base64 string, `json.Marshaler` types→free-form object, pointer fields→nullable (`[T,"null"]` for scalars, `anyOf` for refs), embedded-field promotion, reference cycles, unknown/ambiguous type names | `internal/inference/schema_test.go` |
| Generic type instantiation         | A single-type-parameter generic (`Response[T]`) instantiated with a named struct resolves to its own component with substituted fields; two distinct instantiations don't collide on one component | `internal/inference/schema_test.go`, `internal/inference/body_test.go` |
| Request/response body inference   | Decode/Unmarshal, Encode/Marshal (single call and "last wins"), slices, maps, basic types, branch-aware status codes, `http.Error`, non-constant `WriteHeader` args, `x-gota-skip` at operation and single-statement granularity, following a chain of same- or cross-package helpers up to four calls deep (both directions, cycle detection, nil-argument and variadic/arity-mismatch non-follow cases), map-literal envelopes (constant keys → inline object, dynamic key → bare object) | `internal/inference/body_test.go` |
| Path parameter & operation ID inference | Turning a `{id}`-style path template into an OpenAPI `parameters` entry, handler-name humanization                                                  | `internal/inference/inference_test.go` |
| Document assembly & validation    | Path/method assembly, duplicate-route errors, unrepresentable-method errors, YAML/JSON marshaling, structural validation catching bad `gota:` input      | `internal/emitter/emitter_test.go` |
| CLI                                | Flag defaults, `--dir` resolution to an absolute path, title override, format detection from `--out`'s extension                                        | `cmd/gota/main_test.go` |
| End-to-end pipeline                | Full `nethttp-basic` fixture through generate → emit → marshal, round-tripped; `chi-basic` fixture (a separate Go module, see below) proving the same pipeline on Chi's nested `Route`/`Mount`; `nethttp-generics` fixture proving two generic instantiations resolve distinctly; `nethttp-operationid-collision` fixture proving a method-less pattern's 8 expanded operations get distinct operationIds instead of failing validation; `gin-inline` fixture proving an inline `func` literal handler still emits a route with a method+path-synthesized operationId and a body inferred from the literal; `echo-basic` fixture proving the full pipeline over the Echo plugin + dialect (group prefix, `:id` param, `c.Bind`, `c.JSON`, a `c.NoContent` 204, an inline handler) validates end-to-end; `security-examples` fixture proving a `gota:doc:` block (securitySchemes, global security, servers, tags, info description, `externalDocs`, and a `webhooks` section whose operation `$ref`s a component and is validated like a path), a per-operation `security`, and response `examples` all land in a document that validates; an unrecognized `gota:doc:` key warns on stderr instead of vanishing | `internal/generate/generate_test.go` |

Router plugins — everything specific to reading routes out of a given
router's API:

| Feature                                    | `net/http` | Chi | Gin | Echo | gorilla/mux |
|----------------------------------------------|:----------:|:---:|:---:|:---:|:---:|
| Route extraction (method + path, incl. path params) | ✅ | ✅ | ✅ (`:name` → `{name}`) | ✅ (`:name` → `{name}`) | ✅ (`{name:re}` → `{name}`) |
| Method-less / multi-method pattern → expands to every HTTP method | ✅ | ✅ (`Handle`/`HandleFunc`, no space in the pattern) | ✅ (`Any`; `Match([]string{…})` per constant method) | ✅ (`Any`; `Add(const)`; `Match([]string{…})` per constant method) | ✅ (no `.Methods()`; `.Methods("GET","POST")` per constant) |
| Handler resolution: bare identifier            | ✅ | ✅ | ✅ (last variadic arg; earlier args are middleware) | ✅ (fixed arg after the path; later args are middleware) | ✅ |
| Handler resolution: method value (bound method) | ✅ | ✅ | ✅ | ✅ | ✅ |
| Handler resolution: cross-package reference    | ✅ | ✅ | ✅ | ✅ | ✅ |
| Handler resolution: inline `func` literal (operationId synthesized from method+path) | ✅ (explicit-method pattern only; a method-less inline closure is declined as dispatcher-ambiguous) | ✅ | ✅ | ✅ | ✅ |
| Handler resolution: through middleware wrapping | single-arg wrapper | single-arg wrapper | n/a | single-arg wrapper | ✅ type-aware: single-arg, multi-arg (`LoggingHandler(out, H)`), curried (`cors(opts)(H)`) |
| Handler resolution: anonymous `switch`/`if-else` on `r.Method` | ✅ | n/a (chi has `Method`/`MethodFunc` instead) | n/a | n/a | n/a |
| Nested path-prefix routing                     | n/a | ✅ `Route`/`Group`, arbitrary depth | ✅ `Group` variables, by object identity, arbitrary depth | ✅ `Group` variables, by object identity, arbitrary depth | ✅ `PathPrefix(…).Subrouter()` variables, by object identity, arbitrary depth |
| Sub-router in a separate function              | n/a | `Mount`, same-package zero-arg constructor only | declined: group functions use relative paths, no recoverable prefix | ✅ register function taking an `*echo.Group` param, resolved to its call-site prefix (direct call and interface-dispatch registry loop), same-package | `Handle`/`PathPrefix().Handler(http.StripPrefix())` mount of a same-package zero-arg `*mux.Router` constructor, prefix applied |

Tests: `internal/router/nethttp/nethttp_test.go`,
`internal/router/chi/chi_test.go`, `internal/router/gin/gin_test.go`,
`internal/router/echo/echo_test.go`,
`internal/router/gorilla/gorilla_test.go` (each framework fixture at
`internal/router/<name>/testdata/routes`, its own Go module — chi, gin,
echo, and gorilla/mux are test-only dependencies, isolated so they never
bump gota's own `go.mod` floor; same reasoning for `testdata/chi-basic`,
used by `TestRun_ChiBasic`, and `testdata/echo-basic`, used by
`TestRun_EchoBasic`). Cross-package resolution itself is plugin-agnostic
(`internal/astutil`, exercised end-to-end in
`internal/generate/generate_test.go`, and per-plugin in each
`TestExtract_CrossPackage`) — any future plugin gets it for free by
populating `Route.HandlerObj` the same way.

Optional local git hooks (`.githooks/`) mirror the CI checks so failures
show up before you even push: `pre-commit` runs `gofmt` only (fast, every
commit), `pre-push` runs the same `gofmt`/`vet`/`build`/`test` steps CI
does (slower, every push). They're not enabled by default — turn them on
with:

```sh
git config core.hooksPath .githooks
```

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for
dev setup and a list of concrete, well-scoped starting points (a Fiber
router plugin + dialect, cross-package `Mount` resolution).
Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

MIT — see [LICENSE](LICENSE).
