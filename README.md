<p align="center">
  <img src="assets/gota-gopher.svg" alt="gota gopher mascot" width="140">
</p>

# gota

[![CI](https://github.com/pabloos/gota/actions/workflows/ci.yml/badge.svg)](https://github.com/pabloos/gota/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/pabloos/gota/graph/badge.svg)](https://codecov.io/gh/pabloos/gota)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

`gota` generates an OpenAPI 3.1 specification from Go source code. It's
code-first with real static inference, not annotation-only:

1. **AST inference** — routes, path parameters, handler names — everything
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

`net/http` is the only supported router, using Go 1.22+'s enhanced
`ServeMux` patterns (`"GET /users/{id}"`). A pattern with no method
(`mux.HandleFunc("/health", HealthCheck)`) matches every method per
net/http's own `ServeMux` docs, so gota expands it into one operation per
HTTP method it supports (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS,
TRACE — not CONNECT, which OpenAPI's Path Item Object has no slot for)
rather than assuming GET.

A `gota:` comment can declare `schema: {$ref: '#/components/schemas/User'}`,
and gota generates that component automatically from the matching Go
struct — fields, primitive types, `omitempty` → required/optional, nested
structs as their own linked components, slices, maps, `time.Time`,
embedding. The type lookup searches every package under `--dir`, not just
the one containing the comment, so `User` can live in a different package
than the handler that references it.

Handlers with **no** `gota:` comment at all also get a best-effort
request/response schema, detected from the handler's own `encoding/json`
calls (`Decoder.Decode`, `Encoder.Encode`, `json.Unmarshal`/`Marshal`,
including slices). Response detection tracks the *real* status code — it
walks the body respecting if/else branch boundaries, pairing each
`Encode`/`Marshal`/`http.Error` call with whatever `w.WriteHeader(<code>)`
was last called in its own branch (or 200, Go's implicit default, if none
was), so a handler with a 404 error branch and a 201 success branch gets
**both** documented as distinct responses, correctly numbered — not
everything collapsed into a generic `200`.

All of this is a heuristic over common idioms, not a dataflow analysis: it
doesn't follow decode/encode/WriteHeader calls wrapped in helper
functions, a `WriteHeader` call whose code isn't a compile-time constant is
ignored, and if the *same* status code is produced more than once the last
occurrence in source order wins (covers the common
"if err != nil {...; return }; ..." shape). A `gota:` comment always
overrides whatever this detects.

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

A schema name declared in more than one analyzed package is a hard error,
not a silent "first match wins" — gota has no `$ref` syntax to say which
one was meant, so it refuses to guess.

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

What's still explicitly out of scope: generics (`Response[T]`).

Other router plugins — Chi, Gin — are not implemented yet.

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
It still catches real mistakes nothing upstream prevents, like a `gota:`
comment declaring a path parameter without `required: true`.

## Testing

```sh
go test ./...
```

Aggregate coverage is tracked by the codecov badge above; the tables
below are about *what* each part's tests actually pin down, not how much.

Core engine — everything router-agnostic:

| Feature                          | What's tested                                                                                                                                              | Where |
|------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------|-------|
| `gota:` comment extraction        | Full blocks, gofmt-reformatted comments (tab-indent, inserted blank line), no block present, invalid YAML, prose preceding the block                     | `internal/extractor/extractor_test.go` |
| Merge semantics                   | Declared fields always win, inferred fields fill gaps, inputs aren't mutated, `Deprecated` can't be unset once set                                       | `internal/merger/merger_test.go` |
| Schema `$ref` resolution          | Primitives + `omitempty`→required, nested structs as linked components, slices, `time.Time`, embedded-field promotion, reference cycles, unknown/ambiguous type names | `internal/inference/schema_test.go` |
| Request/response body inference   | Decode/Unmarshal, Encode/Marshal (single call and "last wins"), slices, branch-aware status codes, `http.Error`, non-constant `WriteHeader` args, `x-gota-skip` at operation and single-statement granularity | `internal/inference/body_test.go` |
| Path parameter & operation ID inference | Turning a `{id}`-style path template into an OpenAPI `parameters` entry, handler-name humanization                                                  | `internal/inference/inference_test.go` |
| Document assembly & validation    | Path/method assembly, duplicate-route errors, unrepresentable-method errors, YAML/JSON marshaling, structural validation catching bad `gota:` input      | `internal/emitter/emitter_test.go` |
| CLI                                | Flag defaults, `--dir` resolution to an absolute path, title override, format detection from `--out`'s extension                                        | `cmd/gota/main_test.go` |
| End-to-end pipeline                | Full `nethttp-basic` fixture through generate → emit → marshal, round-tripped                                                                            | `internal/generate/generate_test.go` |

Router plugins — everything specific to reading routes out of a given
router's API (only `net/http` exists today; this table's shape is meant
to scale as Chi/Gin plugins get added):

| Feature                                    | `net/http` |
|----------------------------------------------|:----------:|
| Route extraction (method + path, incl. path params) | ✅ |
| Method-less pattern → expands to every HTTP method | ✅ |
| Handler resolution: bare identifier            | ✅ |
| Handler resolution: method value (bound method) | ✅ |
| Handler resolution: cross-package reference    | ✅ |

Tests: `internal/router/nethttp/nethttp_test.go`. Cross-package
resolution itself is plugin-agnostic (`internal/astutil`, exercised
end-to-end in `internal/generate/generate_test.go`) — any future plugin
gets it for free by populating `Route.HandlerObj` the same way.

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
dev setup and a list of concrete, well-scoped starting points (a Chi/Gin
router plugin, generics support, cross-package handler resolution).
Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

MIT — see [LICENSE](LICENSE).
