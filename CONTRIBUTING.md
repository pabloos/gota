# Contributing to gota

Thanks for considering a contribution. This project is early-stage, so
there's real room to shape it — see [Good first issues](#good-first-issues)
below for concrete, well-scoped starting points.

By participating, you're expected to follow the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Getting set up

Go 1.22.5+ is all you need — no other toolchains, no code generation step
to run first.

```sh
git clone https://github.com/pabloos/gota.git
cd gota
go build ./...
go test ./...
```

Try the CLI against the bundled fixture:

```sh
go run ./cmd/gota --dir testdata/nethttp-basic --out /tmp/openapi.yaml
```

### Local git hooks (optional)

`.githooks/` mirrors what CI checks, so problems show up before you push
instead of in the Actions run:

```sh
git config core.hooksPath .githooks
```

`pre-commit` runs `gofmt` only (fast, every commit); `pre-push` runs
`gofmt`/`vet`/`build`/`test` (slower, every push). Neither runs `-race` —
that's CI's job, since forcing cgo for `-race` hits linker bugs on some
local toolchains unrelated to the code.

## Before opening a PR

```sh
gofmt -l .          # must print nothing
go vet ./...
go build ./...
go test ./...
```

This is exactly what CI runs, so a clean run here means a clean run there.

## Code conventions

These aren't arbitrary — they're what the existing code already does, so
matching them keeps a PR reviewable as a diff rather than a rewrite:

- **No comments that restate the code.** Only comment a genuinely
  non-obvious *why* — a hidden constraint, a workaround, a subtlety a
  reader would trip on. If you'd delete the comment and nothing would be
  lost, don't write it. The existing `internal/` packages are full of
  examples of the level that's expected.
- **Test fixtures are real `.go` files under `testdata/`,** not strings
  embedded in the test source — they need to be readable and (where
  applicable) valid Go a human can open directly, not just something a
  test happens to parse.
- **A `gota:` comment is OpenAPI, not a DSL.** If you're touching the
  extractor or merger, don't invent new comment syntax — express new
  behavior in real OpenAPI vocabulary (including Specification
  Extensions, `x-*` fields) the way `x-gota-skip` does.
- **Don't add abstractions the current scope doesn't need.** No
  speculative interfaces, no config knobs for hypothetical future
  requirements. Three similar lines beats a premature helper.
- Keep commit messages short and imperative (`git log --oneline` shows
  the style this project uses); explain *why* in the PR description if
  it's not obvious from the diff.

## Good first issues

These are real, currently-unaddressed gaps — each documented in the
README's [Status](README.md#status) section, not secret TODOs:

- **Chi or Gin router plugin.** `internal/router/nethttp` is the
  reference implementation of the `router.Plugin` interface
  (`internal/router/plugin.go`) — a new plugin needs the same route
  (method, path, handler) extraction for a different router's API.
- **Generics support** (`Response[T]`-shaped handlers) — currently
  explicitly out of scope for schema inference.
- **Cross-package handler resolution** — resolving a `gota:` comment on
  a handler that's referenced from a different package than where it's
  registered (e.g. `mux.HandleFunc("/x", handlers.GetUser)`). This is a
  route-extraction limitation, distinct from schema `$ref` resolution
  (which already searches every analyzed package).

Opening an issue to discuss approach before a large PR is welcome but not
required for small, well-contained changes.

## License

By contributing, you agree your contributions are licensed under this
project's [MIT license](LICENSE).
