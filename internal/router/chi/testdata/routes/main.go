// Package fixture is test data for chi_test.go's TestExtract: it
// exercises every route-registration shape the plugin recognizes
// (method-specific calls, Handle/HandleFunc with and without a
// "METHOD " prefix, Method/MethodFunc, nested Route, Group, a
// same-package Mount constructor, a router-configuring function called
// with a local router variable that's itself mounted at a real prefix
// (registerWidgetRoutes/v1 below — mirrors a pattern found in a real
// Chi project on GitHub), .With(...) chaining, a method-value handler,
// a regex-constrained param, Connect/Query being recognized but not
// emitted) plus the deliberately-declined shapes (a bare wildcard
// pattern, a Mount whose argument isn't a zero-arg same-package
// constructor or a tracked local variable, a Route callback passed by
// variable instead of inlined).
package fixture

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Server struct{}

func (s *Server) GetItem(w http.ResponseWriter, r *http.Request) {}

func Handlers() chi.Router {
	r := chi.NewRouter()
	srv := &Server{}

	r.Get("/users/{id}", GetUser)
	r.Post("/users", CreateUser)
	r.Handle("/health", http.HandlerFunc(HealthCheck))
	r.Handle("GET /legacy", http.HandlerFunc(LegacyHandler))
	r.Method("GET", "/method-get", http.HandlerFunc(MethodGetHandler))
	r.Connect("/connect-only", http.HandlerFunc(ConnectHandler))
	// Query() itself needs a newer chi than this fixture's go1.22.5-floor
	// pin has (added in v5.3.1, which requires go 1.23) -- Method("QUERY",
	// ...) exercises the exact same decline path without that dependency.
	r.Method("QUERY", "/query-only", http.HandlerFunc(QueryHandler))
	r.With(SomeMiddleware).Get("/with-middleware", WithMiddlewareHandler)
	r.Get("/items/{id:[0-9]+}", srv.GetItem)

	r.Route("/products", func(r chi.Router) {
		r.Get("/", ListProducts)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", GetProduct)
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(SomeMiddleware)
		r.Get("/admin", AdminHandler)
	})

	r.Mount("/orders", ordersRouter())

	r.Get("/wild/*", WildcardHandler) // declined: no OpenAPI wildcard equivalent

	r.Route("/detached", detachedFn) // declined: Route callback not inlined

	v1 := chi.NewRouter()
	registerWidgetRoutes(v1)
	r.Mount("/v1", v1)

	notARoute := map[string]string{"Get": "not a call"}
	_ = notARoute

	return r
}

func ordersRouter() chi.Router {
	r := chi.NewRouter()
	r.Get("/", ListOrders)
	r.Post("/", CreateOrder)
	return r
}

var detachedFn = func(r chi.Router) {
	r.Get("/never-seen", NeverSeenHandler)
}

// registerWidgetRoutes mirrors a real, common pattern found in a
// production Chi project on GitHub: a router-configuring function
// called with a specific router variable (registerWidgetRoutes(v1)
// above), rather than a zero-arg constructor Mount can call directly.
// Its own registrations must be declined entirely -- extracting them
// at the empty top-level prefix would silently produce the wrong path
// ("/widgets" instead of "/v1/widgets").
func registerWidgetRoutes(r chi.Router) {
	r.Get("/widgets", ListWidgets)
}

// RegisterAbsolute mirrors go8's canonical domain-driven Chi shape: a
// router-parameter function that registers ABSOLUTE paths on its
// parameter, with local handlers, and is NOT called anywhere in this
// package (its real call site would be cross-package, e.g.
// things.RegisterAbsolute(s.router)). It must be walked at the empty
// top-level prefix -- the parameter acting as a chi receiver -- and
// emit its absolute paths.
func RegisterAbsolute(router chi.Router) {
	router.Route("/api/v1/things", func(router chi.Router) {
		router.Get("/", ListThings)
		router.Post("/{id}", CreateThing)
	})
}

// registerMultiArgMounted is a MULTI-argument router-parameter function
// whose router variable is Mounted at a prefix in the same package (see
// SetupMultiArg). extractRouterVarBlocks only wires the prefix through
// single-argument calls, so gota can't compute this one's real prefix
// -- it must emit NOTHING (declined), never its route at a
// silently-wrong empty prefix.
func registerMultiArgMounted(router chi.Router, cfg string) {
	router.Get("/multi-arg-thing", MultiArgThingHandler)
}

func SetupMultiArg() chi.Router {
	r := chi.NewRouter()
	sub := chi.NewRouter()
	registerMultiArgMounted(sub, "cfg")
	r.Mount("/mounted", sub)
	return r
}

// EdgeCases exercises the plugin's less-common recognition/decline
// branches -- each one reachable via real, type-checking Go code (not
// contrived to trip a specific line), unlike an arity mismatch against
// chi's own fixed-signature methods, which the compiler would reject
// before gota ever sees it.
func EdgeCases() chi.Router {
	r := chi.NewRouter()

	// stringLiteral: the pattern is a named constant, not a literal.
	r.Get(constPath, ConstPathHandler)

	// resolveHandler: the handler is an inline func literal, not an
	// identifier or selector -- carried via Route.HandlerLit so
	// internal/generate can infer its body and synthesize an operationId.
	r.Get("/inline", func(w http.ResponseWriter, req *http.Request) {})

	// normalizePath: an unterminated "{" -- a typo that's still a valid
	// Go string literal, so gota sees it, not the compiler.
	r.Get("/broken/{id", BrokenPatternHandler)

	// constStringArg (via Method): the method argument is a plain
	// variable, not a compile-time constant -- go/constant has no value
	// recorded for a ":="-declared variable even when it's initialized
	// from a literal.
	dynamicMethod := "GET"
	r.Method(dynamicMethod, "/dynamic-method", http.HandlerFunc(DynamicMethodHandler))

	// patternAndFuncLit: Route's pattern is computed, not a literal --
	// declined even though the callback itself is a valid inline
	// closure.
	computedPrefix := "/computed"
	r.Route(computedPrefix, func(r chi.Router) {
		r.Get("/never-seen-computed", NeverSeenComputedHandler)
	})

	// isChiRouterCallback: a func(r chi.Router)-shaped closure assigned
	// to a LOCAL variable (unlike detachedFn, a package-level var) then
	// passed by identifier -- reached via the generic walk (this
	// assignment is its own statement, not a Route/Group argument), so
	// it must be blocked from descending, not walked at the wrong
	// prefix.
	localFn := func(r chi.Router) {
		r.Get("/never-seen-local", NeverSeenLocalHandler)
	}
	r.Route("/local-detached", localFn)

	// extractFromBlock's FuncLit case: an ORDINARY (non chi.Router-
	// shaped) local closure must still be walked normally, not blocked
	// -- isChiRouterCallback declines it on shape, not on being a
	// closure at all.
	logFn := func(msg string) { _ = msg }
	logFn("started")

	// extractRouterVarBlocks: a plain single-argument call whose
	// argument ISN'T a tracked router variable -- must be ignored, not
	// mistaken for a configuring function call.
	label := "not-a-router"
	noop(label)

	// extractRouterVarBlocks: a tracked variable that's never mounted
	// falls back to this block's own prefix, matching the ordinary "r
	// := chi.NewRouter(); ...; return r" case.
	v2 := chi.NewRouter()
	registerV2Routes(v2)

	// extractRouterVarBlocks: Mount to a bare identifier that ISN'T a
	// tracked chi.Router variable (a plain net/http.ServeMux here) --
	// declined, not mistaken for a tracked var.
	legacyMux := http.NewServeMux()
	r.Mount("/legacy-mux", legacyMux)

	// extractRouterVarBlocks: "=" reassigning an already-declared
	// variable, not ":=" -- go/types records this in Uses, not Defs,
	// so tracking must fall back to Uses to still catch it.
	var v3 chi.Router
	v3 = chi.NewRouter()
	registerV3Routes(v3)
	r.Mount("/v3", v3)

	// resolveMountTarget: the mounted expression calls a method value,
	// not a bare function identifier.
	srv := &legacyRouter{}
	r.Mount("/via-method", srv.Router())

	// resolveMountTarget: the mounted expression's call has arguments,
	// not zero.
	r.Mount("/via-args", viaArgsRouter("cfg"))

	// resolveMountTarget: the mounted function's return type isn't
	// chi.Router/*chi.Mux at all.
	r.Mount("/legacy-handler", legacyHandler())

	// mountedRoutes: the mounted identifier resolves to a *types.Var
	// holding a func literal (ctorFn below), not a real *ast.FuncDecl --
	// funcDecls (built from actual declarations) has no entry for it,
	// so this is declined the same as any other unresolvable target.
	r.Mount("/via-var-func", ctorFn())

	return r
}

func noop(s string) {}

const constPath = "/const-path"

type legacyRouter struct{}

func (l *legacyRouter) Router() chi.Router {
	r := chi.NewRouter()
	r.Get("/", NeverSeenViaMethodHandler)
	return r
}

func viaArgsRouter(cfg string) chi.Router {
	r := chi.NewRouter()
	r.Get("/", NeverSeenViaArgsHandler)
	return r
}

func legacyHandler() http.Handler {
	return http.NotFoundHandler()
}

var ctorFn = func() chi.Router {
	r := chi.NewRouter()
	r.Get("/", NeverSeenVarFuncHandler)
	return r
}

func registerV2Routes(r chi.Router) {
	r.Get("/v2-widgets", V2WidgetsHandler)
}

func registerV3Routes(r chi.Router) {
	r.Get("/v3-widgets", V3WidgetsHandler)
}

func SomeMiddleware(next http.Handler) http.Handler { return next }

// gota:
//
//	summary: Get a user by ID
func GetUser(w http.ResponseWriter, r *http.Request)                   {}
func CreateUser(w http.ResponseWriter, r *http.Request)                {}
func HealthCheck(w http.ResponseWriter, r *http.Request)               {}
func LegacyHandler(w http.ResponseWriter, r *http.Request)             {}
func MethodGetHandler(w http.ResponseWriter, r *http.Request)          {}
func ConnectHandler(w http.ResponseWriter, r *http.Request)            {}
func QueryHandler(w http.ResponseWriter, r *http.Request)              {}
func WithMiddlewareHandler(w http.ResponseWriter, r *http.Request)     {}
func ListProducts(w http.ResponseWriter, r *http.Request)              {}
func GetProduct(w http.ResponseWriter, r *http.Request)                {}
func AdminHandler(w http.ResponseWriter, r *http.Request)              {}
func ListOrders(w http.ResponseWriter, r *http.Request)                {}
func CreateOrder(w http.ResponseWriter, r *http.Request)               {}
func WildcardHandler(w http.ResponseWriter, r *http.Request)           {}
func NeverSeenHandler(w http.ResponseWriter, r *http.Request)          {}
func ListWidgets(w http.ResponseWriter, r *http.Request)               {}
func ConstPathHandler(w http.ResponseWriter, r *http.Request)          {}
func BrokenPatternHandler(w http.ResponseWriter, r *http.Request)      {}
func DynamicMethodHandler(w http.ResponseWriter, r *http.Request)      {}
func NeverSeenComputedHandler(w http.ResponseWriter, r *http.Request)  {}
func NeverSeenLocalHandler(w http.ResponseWriter, r *http.Request)     {}
func NeverSeenViaMethodHandler(w http.ResponseWriter, r *http.Request) {}
func NeverSeenViaArgsHandler(w http.ResponseWriter, r *http.Request)   {}
func NeverSeenVarFuncHandler(w http.ResponseWriter, r *http.Request)   {}
func V2WidgetsHandler(w http.ResponseWriter, r *http.Request)          {}
func V3WidgetsHandler(w http.ResponseWriter, r *http.Request)          {}
func ListThings(w http.ResponseWriter, r *http.Request)                {}
func CreateThing(w http.ResponseWriter, r *http.Request)               {}
func MultiArgThingHandler(w http.ResponseWriter, r *http.Request)      {}
