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

Phase 1: `net/http` only, using Go 1.22+'s enhanced `ServeMux` patterns
(`"GET /users/{id}"`). Struct-to-schema type inference (Phase 2) and other
router plugins — Chi, Gin (Phase 3) — are not implemented yet.

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
                                   Static inference → baseline Operation
                                             │
                                             ▼
                              Merge (comment wins, inferred fills gaps)
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
