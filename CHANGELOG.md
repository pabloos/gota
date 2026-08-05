# Changelog

All notable changes to this project are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this
project versions itself with [SemVer](https://semver.org/), starting
from `0.1.0` while the tool is still pre-1.0 and its inference surface
is still growing.

## [0.3.3]

Schema resolution robustness (no more aborts) and a gorilla/mux mount fix,
each with an isolated reproducer.

### Fixed

- **An unresolvable schema `$ref` no longer aborts the whole document.** A
  reference that can't be resolved to a single Go type — a bare name that
  collides across the reachable graph, or a hand-written typo — degrades
  to a generic `{type: object}` (warning on stderr) and the rest of the
  spec is generated. The messages are origin-neutral (they no longer
  assume the reference came from a `gota:` comment).
- **A dependency / other-module response type resolves to its real
  schema.** A type declared outside the analyzed root packages is now
  package-qualified in its component name (`repository.Event`), so it
  resolves through its own package instead of colliding on a bare name
  with an unrelated same-named type elsewhere in the reachable graph — its
  fields are expanded, and an unexposed same-named type never interferes.
- **A function-local response type is inlined.** A struct declared inside
  a handler (no stable component name) now inlines its real object schema
  — fields expanded, named field types as their own `$refs` — instead of
  degrading to a bare object.
- **gorilla/mux: a same-package sub-router constructor mount applies its
  prefix.** A `func() *mux.Router` constructor mounted on another router
  now has the mount prefix applied to its routes, via
  `root.Handle("/v2", ctor())` (and mux's `"/v2/{rest:.*}"` subpath idiom,
  deduped) or `root.PathPrefix("/v2").Handler(http.StripPrefix("/v2",
  ctor()))`. Its routes appear under the mount prefix rather than
  unprefixed; a cross-package constructor remains the documented mount gap.

## [0.3.2]

Body inference through the handler-factory + middleware idiom, and a
gorilla/mux mount fix.

### Fixed

- **Inference follows a handler factory to its returned literal.** A
  handler declared as a factory — `func(...) http.Handler` returning an
  inline handler, on its own (`return http.HandlerFunc(func(w, r){...})`)
  or wrapped in a middleware chain (`return Chain(mw...)(http.HandlerFunc(
  ...))`) — produced only a bare `200`, because inference walked the
  factory's top-level statements rather than the returned handler.
  `DetectBody` now descends into that literal, unwrapping the middleware
  wrapper type-aware down to the innermost func literal, and detects its
  request body and responses.
- **Request body from a generic middleware type argument.** When the
  handler literal never decodes the body itself — it's bound by a generic
  middleware in the chain (`Chain(Bind[T])(handler)`) — the request body
  is inferred from that middleware's named-struct type argument.
- **gorilla/mux: a `*mux.Router` handler is a mount, not an endpoint.**
  `Handle(path, subrouter)` where the handler is a `*mux.Router` was
  emitted as a catch-all endpoint for every method, duplicating the
  sub-router's own routes (including mux's `Handle("/x/{rest:.*}", sub)`
  subpath idiom). It's now recognized as a mount and declined. (Applying a
  mount's prefix to a sub-router built in another package remains a
  documented cross-package gap, as with chi.)

## [0.3.1]

Fixes to the gorilla/mux plugin and to cross-module type resolution,
each with an isolated reproducer.

### Fixed

- **gorilla/mux: a factory-call handler is recognized.** A handler that
  is a call returning `http.Handler` — a free function (`Handle(p,
  made())`) or a method (`Handle(p, ct.build())`), the idiom of returning
  a handler already wrapped in middleware — was silently dropped. The
  route is now recognized and named by the factory.
- **gorilla/mux: `NewRoute().Subrouter()` inherits the prefix.** A
  middleware-only subrouter (`sec := root.NewRoute().Subrouter()`) adds no
  path segment but must inherit the parent's accumulated prefix; its
  routes were emitted at the wrong (unprefixed) path.
- **Cross-module response types resolve instead of aborting.** A handler
  serializing a type declared in another module (a dependency, or another
  `go.work` module) made `ResolveSchemaRefs` abort the whole document,
  because it looked up only the analyzed root packages. It now falls back
  to the full reachable import graph, resolving the type into a real
  component with its fields expanded. The not-found message no longer
  claims a `gota:` comment was involved when the `$ref` was inferred.
- **Actionable error for a workspace-root `--dir`.** Pointing `--dir` at a
  Go workspace root (a `go.work` with no `go.mod`) failed with
  go/packages' cryptic "directory prefix . does not contain modules
  listed in go.work"; it now returns a clear error to point `--dir` at one
  of the workspace's modules.

## [0.3.0]

A fourth router and a body-inference robustness fix, both driven by
running gota against a large real gorilla/mux API (gophish).

### Routers

- **gorilla/mux router plugin** (`github.com/gorilla/mux`):
  `r.HandleFunc`/`r.Handle` registrations, with the HTTP methods taken
  from a `.Methods()` chained onto the returned `*mux.Route` (anywhere in
  the builder chain; no `.Methods()` matches every method). Subrouters
  carry a prefix by variable identity
  (`s := r.PathPrefix("/api/v1").Subrouter()`), transitively and
  inline-chained; an identity-preserving self-reconfiguration
  (`root = root.StrictSlash(true)`) is not mistaken for an ambiguous
  reassignment. A `{name:regex}` constraint and a `{rest:.*}` catch-all
  degrade to the bare `{name}`/`{rest}`. gorilla handlers are plain
  net/http, so it reuses `inference.NetHTTP()` with no dialect of its own.
- **Middleware indirection**: the handler passed to `Handle`/`HandleFunc`
  is commonly the real handler wrapped in middleware, so the gorilla
  plugin unwraps it type-aware — following the one argument at each layer
  whose type is `http.Handler`-shaped — through a single-arg wrapper, a
  multi-argument middleware (`handlers.LoggingHandler(out, H)`), and a
  curried one (`cors(opts)(H)`), down to the real handler for body
  inference.

### Inference

- A handler whose response type is a struct **declared inside the
  function** (a common real-world shape) no longer errors the entire
  document. Body inference previously emitted a `$ref` to it that
  `ResolveSchemaRefs` — which only looks up package-scope types — couldn't
  resolve; such a type now degrades to a generic `{type: object}` schema
  (a package-level type, including an instantiated generic, still gets a
  `$ref`).

## [0.2.0]

Multi-framework routing and richer handler resolution. Every router
plugin runs unconditionally against every analyzed package — a framework
that isn't present simply contributes no routes, so no flag picks one.

### Routers

- **Chi router plugin** (`github.com/go-chi/chi`): method-specific
  (`Get`/`Post`/…) and all-methods (`Handle`/`HandleFunc`) registrations,
  `Route`/`Group` nesting followed to arbitrary depth with prefix
  accumulation, and same-package zero-arg-constructor `Mount`. A
  regex-constrained param (`{id:[0-9]+}`) degrades to `{id}`; a bare
  wildcard segment is declined. Reuses the net/http body inference (chi
  handlers are plain `http.HandlerFunc`).
- **Gin router plugin + inference dialect** (`github.com/gin-gonic/gin`):
  routes off the engine and off `Group` variables, prefix accumulated by
  object identity through nested and inline-chained groups; `:name` →
  `{name}`; `Any`/`Handle`/`Match` expanded to concrete methods;
  `.Use(mw)` middleware chains kept on the group prefix. Gin's own
  request/response idioms (`c.JSON(code, obj)`, `c.ShouldBindJSON(&x)`,
  the `gin.H` envelope) are understood by a dedicated `inference.Dialect`
  that embeds the net/http one, so a stdlib fallback inside a gin handler
  still resolves. Every served path is rooted at `/` the way gin itself
  roots it. A `func(rg *gin.RouterGroup)` register function, a `*name`
  catch-all, a reassigned group variable, and a non-constant method are
  declined rather than guessed.

### Inference

- Body inference is decoupled from net/http behind a sealed
  `inference.Dialect` seam, so a framework with different
  request/response idioms brings its own recognizer instead of the core
  assuming `http.ResponseWriter`.
- Inline `func` literal handlers are supported: the route is emitted with
  an `operationId` synthesized from the method and path, and the body is
  inferred from the literal itself. All three plugins support this;
  net/http restricts it to explicit-method patterns
  (`"GET /x"`), since a method-less inline closure can't be told apart
  from the hand-rolled dispatcher idiom.
- A schema name declared in more than one analyzed package is no longer a
  hard error — it's disambiguated by package-qualifying the component key
  and every `$ref` to it (`author.Widget` / `book.Widget`). This
  supersedes 0.1.0's hard-error behavior.

### Build & tooling

- Minimum Go to build gota is now **1.23** (raised from 1.22.5), from
  adopting `golang.org/x/tools v0.36.0` so targets built with a modern Go
  toolchain (1.25/1.26 export data) can be analyzed. The module floor and
  the toolchain gota is *built* with are decoupled — see CONTRIBUTING's
  "Analyzing modern targets" for the two independent version ceilings.

## [0.1.0]

First tagged release. `gota` generates an OpenAPI 3.1 specification
from Go source via static analysis (`go/ast`, `go/types`), merged with
`gota:` YAML comments that are themselves valid OpenAPI fragments —
declared fields always win, inferred fields fill in the rest.

### Core

- `net/http` router support, including Go 1.22+'s enhanced `ServeMux`
  patterns (`"GET /users/{id}"`), with method-less patterns
  (`mux.HandleFunc("/health", h)`) expanded into one operation per HTTP
  method rather than assumed to be GET-only.
- `gota:` comment parsing and merging: comment fields always win,
  inferred fields fill in the rest.
- `x-gota-skip: true` to exclude a whole operation (on the handler's
  own comment) or a single detected response (on a comment above one
  statement inside the handler body).
- Struct-to-schema inference via `$ref` resolution: fields, primitive
  types, `omitempty` → required/optional, nested structs as linked
  components, slices, maps, `time.Time`, embedding.
- Best-effort request/response body inference for handlers with no
  `gota:` comment at all, from common `encoding/json` + `net/http`
  idioms: `Decoder.Decode`/`Encoder.Encode`, `json.Unmarshal`/`Marshal`,
  slices, maps, basic types, branch-aware status-code tracking
  (`WriteHeader`, `http.Error`, respecting if/else branch boundaries).
- A schema name declared in more than one analyzed package is a hard
  error, not a silent "first match wins". (Superseded in 0.2.0, which
  disambiguates such names by package instead.)

### Cross-package and indirection resolution

- Handlers registered from a different package than the one that
  declares them are resolved the same as a local handler, via a
  `go/types`-object index spanning every analyzed package.
- A generic type instantiated with one named-struct type argument
  (`Response[User]`) gets its own component per instantiation
  (`Response_User`) instead of every instantiation colliding on one
  untyped component.
- A response or decode call wrapped in a helper is followed as if
  inlined — same package or not, up to four calls deep, with cycle
  detection for a self- or mutually-recursive helper — resolving a
  followed helper's own parameter references back to the real
  call-site expression, transitively across as many frames as it
  takes.
- A response wrapped in an ad-hoc `map[string]any{...}` envelope with
  no named struct gets an inline object schema built from its actual
  compile-time-constant keys.

### Routing correctness

- `operationId` collisions (a method-less pattern expanding into
  several operations bound to the same handler, or the same handler
  registered at more than one path) are disambiguated automatically —
  never touching an explicitly declared `operationId`.
- The pre-Go-1.22 idiom for method dispatch — a method-less pattern
  registered against a closure that switches (or if/else branches) on
  `r.Method`, itself wrapped in middleware — is recognized, producing
  one route per branch bound to the real delegate.

### Project

- CI: build, vet, test, and coverage reporting via Codecov.
- Local git hooks, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, and
  issue/PR templates.
