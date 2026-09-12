# gota internals

Architecture notes for contributors. To *use* gota, see the
[top-level README](../README.md). Area-specific detail lives next to the
code it documents: [router plugins](router/) and
[schema & body inference](inference/).

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
