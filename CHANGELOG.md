# Changelog

All notable changes to this project are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this
project versions itself with [SemVer](https://semver.org/), starting
from `0.1.0` while the tool is still pre-1.0 and its inference surface
is still growing.

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
