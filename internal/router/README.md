# Router plugins

How gota reads route registrations out of each supported framework. This is
contributor reference; to *use* gota, see the [top-level README](../../README.md).
The exact, authoritative mechanics for each plugin also live in its package
doc comment (`go doc github.com/pabloos/gota/internal/router/<name>`).

`net/http`, [Chi](https://github.com/go-chi/chi),
[Gin](https://github.com/gin-gonic/gin),
[Echo](https://github.com/labstack/echo),
[Fiber](https://github.com/gofiber/fiber), and
[gorilla/mux](https://github.com/gorilla/mux) are supported routers —
every plugin runs unconditionally against every analyzed package (no flag
needed to pick one; a plugin whose framework isn't present just
contributes no routes). `net/http`, Chi, and gorilla/mux share the same
net/http-based body inference, since their handlers are plain
`func(w http.ResponseWriter, r *http.Request)` with no response-writing
idiom of their own. Gin (`c.JSON(201, obj)`, `c.ShouldBindJSON(&x)`),
Echo (`c.JSON(201, obj)`, `c.Bind(&x)`, `c.NoContent(204)`), and Fiber
(`c.Status(201).JSON(obj)`, `c.BodyParser(&x)`, `c.SendStatus(204)`),
whose handlers write through a framework context, each bring their own
`inference.Dialect` that embeds the net/http one — so a handler that falls
back to `json.Marshal`/`Decode` is still understood.

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
(`GetVersion`). A `func reg(rg *gin.RouterGroup){...}` register function is
resolved to the prefix of the group passed at its call site — direct call
and interface-dispatch registry loop alike, matched by call name + argument
position (the same mechanism as Echo's, below) — with nested groups inside
chaining onto it; only a register function called solely from another
package stays declined (the single-package boundary chi's `Mount` has). A
`*name` catch-all, a reassigned group variable, and a non-constant method
are still declined.

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
single-package boundary as chi's `Mount`. (The Gin plugin resolves its
`func(rg *gin.RouterGroup)` equivalent the same way.)

Fiber is read off `*fiber.App` and the `fiber.Router` interface a group is
typed as. Its route methods are named like Go methods (`app.Get`, `app.Post`,
… plus `app.All` for every method and `app.Add(method, …)` for an explicit
one); the handler is the last variadic argument (earlier ones are
middleware), as in Gin. Groups accumulate their prefix by object identity,
inline and nested; `:name` (an optional `?` suffix dropped) → `{name}` and a
`*`/`+` wildcard is declined. The register-function layout resolves exactly
as Gin's and Echo's — a `func(r fiber.Router)` parameter takes the prefix of
the group passed at the call site (a `*fiber.App` parameter is the root). Its
dialect is the one framework where the status isn't a response-call argument:
`c.JSON(obj)` records at the ambient status, `c.Status(code)` sets it (read
off the chain in `c.Status(201).JSON(obj)`), and `c.SendStatus(code)` is a
bodyless response. The `app.Route(prefix, func(r fiber.Router){…})` callback
form is not followed yet.

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
`{code:^[A-Z]{2}[0-9]{4}$}`, whose closing brace is found by balancing,
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

## Known gaps

No further framework plugins are planned right now. The remaining
route-extraction gaps are shared across the framework plugins: a register
function whose only call site is in another package (the single-package
boundary chi's `Mount` also has), and Fiber's `app.Route(prefix, func(r
fiber.Router){…})` callback form. A new framework plugin would also need its
own `inference.Dialect` (see [schema & body inference](../inference/)), not
just route extraction.

## Feature matrix

Router plugins — everything specific to reading routes out of a given
router's API:

| Feature                                    | `net/http` | Chi | Gin | Echo | Fiber | gorilla/mux |
|----------------------------------------------|:----------:|:---:|:---:|:---:|:---:|:---:|
| Route extraction (method + path, incl. path params) | ✅ | ✅ | ✅ (`:name` → `{name}`) | ✅ (`:name` → `{name}`) | ✅ (`:name`/`:name?` → `{name}`) | ✅ (`{name:re}` → `{name}`) |
| Method-less / multi-method pattern → expands to every HTTP method | ✅ | ✅ (`Handle`/`HandleFunc`, no space in the pattern) | ✅ (`Any`; `Match([]string{…})` per constant method) | ✅ (`Any`; `Add(const)`; `Match([]string{…})` per constant method) | ✅ (`All`; `Add(const)` per method) | ✅ (no `.Methods()`; `.Methods("GET","POST")` per constant) |
| Handler resolution: bare identifier            | ✅ | ✅ | ✅ (last variadic arg; earlier args are middleware) | ✅ (fixed arg after the path; later args are middleware) | ✅ (last variadic arg; earlier args are middleware) | ✅ |
| Handler resolution: method value (bound method) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Handler resolution: cross-package reference    | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Handler resolution: inline `func` literal (operationId synthesized from method+path) | ✅ (explicit-method pattern only; a method-less inline closure is declined as dispatcher-ambiguous) | ✅ | ✅ | ✅ | ✅ | ✅ |
| Handler resolution: through middleware wrapping | single-arg wrapper | single-arg wrapper | n/a | single-arg wrapper | single-arg wrapper | ✅ type-aware: single-arg, multi-arg (`LoggingHandler(out, H)`), curried (`cors(opts)(H)`) |
| Handler resolution: anonymous `switch`/`if-else` on `r.Method` | ✅ | n/a (chi has `Method`/`MethodFunc` instead) | n/a | n/a | n/a | n/a |
| Nested path-prefix routing                     | n/a | ✅ `Route`/`Group`, arbitrary depth | ✅ `Group` variables, by object identity, arbitrary depth | ✅ `Group` variables, by object identity, arbitrary depth | ✅ `Group` variables, by object identity, arbitrary depth | ✅ `PathPrefix(…).Subrouter()` variables, by object identity, arbitrary depth |
| Sub-router in a separate function              | n/a | `Mount`, same-package zero-arg constructor only | ✅ register function taking a `*gin.RouterGroup` param, resolved to its call-site prefix (direct call and interface-dispatch registry loop), same-package | ✅ register function taking an `*echo.Group` param, resolved to its call-site prefix (direct call and interface-dispatch registry loop), same-package | ✅ register function taking a `fiber.Router`/`*fiber.App` param, resolved to its call-site prefix (direct call and interface-dispatch registry loop), same-package | `Handle`/`PathPrefix().Handler(http.StripPrefix())` mount of a same-package zero-arg `*mux.Router` constructor, prefix applied |

Tests: `internal/router/nethttp/nethttp_test.go`,
`internal/router/chi/chi_test.go`, `internal/router/gin/gin_test.go`,
`internal/router/echo/echo_test.go`, `internal/router/fiber/fiber_test.go`,
`internal/router/gorilla/gorilla_test.go` (each framework fixture at
`internal/router/<name>/testdata/routes`, its own Go module — chi, gin,
echo, fiber, and gorilla/mux are test-only dependencies, isolated so they
never bump gota's own `go.mod` floor; same reasoning for `testdata/chi-basic`
(`TestRun_ChiBasic`), `testdata/echo-basic` (`TestRun_EchoBasic`), and
`testdata/fiber-basic` (`TestRun_FiberBasic`)). Cross-package resolution itself is plugin-agnostic
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
