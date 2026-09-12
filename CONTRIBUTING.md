# Contributing to gota

Thanks for considering a contribution. This project is early-stage, so
there's real room to shape it — see [Good first issues](#good-first-issues)
below for concrete, well-scoped starting points.

By participating, you're expected to follow the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Getting set up

Go 1.23+ is all you need to build and test gota — no other toolchains, no
code generation step to run first. (Analyzing targets written for a newer
Go is a separate matter — see [Analyzing modern targets](#analyzing-modern-targets).)

```sh
git clone https://github.com/pabloos/gota.git
cd gota
go build ./...
go test ./...
```

`go test ./...` will fetch `github.com/go-chi/chi/v5`,
`github.com/gin-gonic/gin`, `github.com/labstack/echo/v4`, and
`github.com/gorilla/mux` on first run (test-only fixture dependencies,
each isolated in its own nested `go.mod` so none bumps gota's own build
floor — see the `internal/router/chi`, `internal/router/gin`,
`internal/router/echo`, and `internal/router/gorilla` package doc
comments) — the one time this repo needs network access to run its normal
test suite.

Try the CLI against the bundled fixture:

```sh
go run ./cmd/gota --dir testdata/nethttp-basic --out /tmp/openapi.yaml
```

### Analyzing modern targets

gota reads a target with `go/packages` + `go/types`, and that imposes two
version ceilings that are independent of each other and of gota's own
`go.mod` floor (which only sets the minimum Go needed to *build* gota):

1. **Export-data reader.** gota decodes some of a target's dependencies
   from compiler export data via `golang.org/x/tools`. A too-old
   `x/tools` can't read export data produced by a newer Go toolchain and
   fails with `package "X" without types was imported from "Y"`. Keeping
   `x/tools` reasonably current avoids this; the pinned version reads
   Go 1.25/1.26 export data.
2. **Source type-checker.** A dependency whose own `go.mod` requires
   Go *N* can't be type-checked by a gota binary *built* with Go < *N*
   (`package requires newer Go version goN (application built with goM)`).
   This is inherent to `go/types` and is fixed only by **building gota
   with a Go toolchain ≥ the newest `go` directive among the target's
   modules** — not by anything in gota's own `go.mod`.

Practical consequence: the `go 1.23` floor keeps gota buildable on older
Go, but **release builds (and anyone analyzing a bleeding-edge target)
should use the current Go toolchain**. The floor and the build toolchain
are deliberately decoupled — a low floor does not mean gota must be built
with an old compiler.

`--dir` points at a single module. A response type declared in a
*different* module (a dependency, or another module in a `go.work`
workspace) still resolves to a real component — `ResolveSchemaRefs` falls
back from the analyzed roots to the whole reachable import graph. Pointing
`--dir` at a workspace root (a directory with a `go.work` but no `go.mod`)
is rejected with an actionable error, since `./...` matches none of the
workspace's modules; point it at one of the modules instead.

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
README's [Supported routers & inference](README.md#supported-routers--inference)
section, not secret TODOs:

- **Fiber router plugin + `inference.Dialect`.** `internal/router/nethttp`,
  `internal/router/chi`, `internal/router/gin`, `internal/router/echo`,
  and `internal/router/gorilla` are all reference `router.Plugin`
  implementations (`internal/router/plugin.go`) — a new plugin needs the
  same route (method, path, handler) extraction for a different router's
  API. Cross-package handler resolution comes for free: populate
  `Route.HandlerObj` the way they do, and `internal/generate` traces it
  via `internal/astutil` regardless of which plugin found it. Unlike Chi
  and gorilla/mux (plain net/http handlers, reuse `inference.NetHTTP()`
  unchanged), Fiber — like Gin and Echo — writes responses through a
  framework context, so it also needs its own `inference.Dialect`
  (`internal/inference/dialect.go`) — see `dialect_nethttp.go` for the
  base and `dialect_gin.go` / `dialect_echo.go` for how a real framework
  dialect embeds it and adds `c.JSON`/`c.Bind`-style recognizers.
- **Cross-package `Mount` resolution.** `internal/router/chi` only
  follows `Mount(prefix, ctor())` when `ctor` is declared in the same
  package as the `Mount` call — `Plugin.Extract` only ever sees one
  package at a time, so a constructor split into a different package
  (the common way large real Chi APIs are structured) isn't resolved.
  Would need `Extract`'s single-package scope to somehow reach another
  package's declaration, or a design change to how plugins are invoked.

Opening an issue to discuss approach before a large PR is welcome but not
required for small, well-contained changes.

## License

By contributing, you agree your contributions are licensed under this
project's [MIT license](LICENSE).
