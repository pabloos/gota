<p align="center">
  <img src="assets/gota-gopher.svg" alt="gota gopher mascot" width="140">
</p>

# gota

[![CI](https://github.com/pabloos/gota/actions/workflows/ci.yml/badge.svg)](https://github.com/pabloos/gota/actions/workflows/ci.yml)
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

What's still explicitly out of scope: generics (`Response[T]`),
disambiguating a schema name declared in more than one package (first
match wins, silently), and resolving a `gota:` comment on a handler that's
itself referenced from a different package than where it's registered
(e.g. `mux.HandleFunc("/x", handlers.GetUser)`) — that's a separate,
still-unresolved limitation in route extraction, not in schema resolution.

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

## License

MIT — see [LICENSE](LICENSE).
