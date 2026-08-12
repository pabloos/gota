// Package fixture is test data for gorilla_test.go's TestExtract: it
// exercises every route-registration shape the plugin recognizes
// (HandleFunc/Handle, a chained .Methods() and its absence, methods
// found through an intervening builder call, single-arg / multi-arg /
// curried middleware unwrapping to the real handler, single and nested
// subrouter variables, an inline chained subrouter, a
// NewRoute().Subrouter() inheriting the parent prefix, a handler that is
// a factory call returning http.Handler (free function and method), an
// identity-preserving self-reconfiguration (r = r.StrictSlash(true)), a
// "{id:regex}" param, a "{rest:.*}" catch-all, an inline func literal
// handler, a router-level .Use) plus the deliberately-declined shapes (a
// *mux.Router-variable mount not emitted as an endpoint, a same-package
// sub-router constructor mounted with a prefix via Handle and via
// http.StripPrefix, a reassigned
// subrouter variable, a non-constant .Methods(), the split
// .Path().HandlerFunc() builder form).
package fixture

import (
	"io"
	"net/http"

	"github.com/gorilla/mux"
)

func Register() *mux.Router {
	r := mux.NewRouter()
	r.Use(logging) // router-level middleware: a no-op for route inference

	r.HandleFunc("/users/{id}", GetUser).Methods("GET")
	r.HandleFunc("/users", CreateUser).Methods("POST", "PUT")
	r.HandleFunc("/health", HealthCheck) // no .Methods() -> every method
	r.HandleFunc("/items/{id:[0-9]+}", GetItem).Methods("GET")
	r.HandleFunc("/quantifier/{id:[a-z]{3}}", QuantifierHandler).Methods("GET")            // regex with a {n} quantifier -> {id}
	r.HandleFunc("/anchored/{id:^sig_([a-zA-Z0-9]{22})$}", AnchoredHandler).Methods("GET") // full anchored pattern with braces -> {id}
	r.HandleFunc("/static/{rest:.*}", StaticHandler).Methods("GET")                        // catch-all -> {rest}
	r.HandleFunc("/ping", func(w http.ResponseWriter, req *http.Request) {}).Methods("GET")
	r.HandleFunc("/secure", SecureHandler).Schemes("https").Methods("GET") // methods through a builder

	// Middleware indirection: the real handler is wrapped and must be
	// unwrapped for body inference.
	r.Handle("/wrapped", requireAuth(http.HandlerFunc(WrappedHandler))).Methods("GET")
	r.Handle("/logged", withLogging(io.Discard, http.HandlerFunc(LoggedHandler))).Methods("GET")
	r.Handle("/cors", cors("*")(http.HandlerFunc(CorsHandler))).Methods("GET")

	// An identity-preserving self-reconfiguration (root = root.M(...))
	// must NOT make the router look reassigned/ambiguous.
	r = r.StrictSlash(true)

	// Subrouter variables carry a path prefix, accumulated across nesting.
	api := r.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/products", ListProducts).Methods("GET")

	admin := api.PathPrefix("/admin").Subrouter()
	admin.HandleFunc("/stats", AdminStats).Methods("GET")

	// A NewRoute().Subrouter() adds no path segment (a middleware-only
	// subrouter) but must INHERIT the parent's accumulated prefix.
	mw := api.NewRoute().Subrouter()
	mw.HandleFunc("/inherited", InheritedHandler).Methods("GET") // -> /api/v1/inherited

	// The handler is a factory call returning http.Handler — a free
	// function and a method — the "return a middleware-wrapped handler"
	// idiom. The route is recognized via the factory's own name.
	r.Handle("/made", makeHandler()).Methods("GET")
	ct := &controller{}
	r.Handle("/from-method", ct.build()).Methods("POST")

	r.PathPrefix("/inline").Subrouter().HandleFunc("/thing", InlineThing).Methods("GET") // inline chained

	// Mounting a subrouter VARIABLE whose routes are already extracted
	// elsewhere (api) is not an endpoint and not a followable constructor
	// mount: no /mount route is emitted, including mux's "{_dummy:.*}"
	// subpath idiom.
	r.Handle("/mount", api)
	r.Handle("/mount/{_dummy:.*}", api)

	// Mounting a same-package sub-router CONSTRUCTOR applies the mount
	// prefix to its routes: subEndpoints()'s "/widget" appears under both
	// "/mnt" (Handle with the router, the "{rest:.*}" subpath idiom
	// deduped) and "/strip" (the http.StripPrefix idiom), and NOT
	// standalone at the bare "/widget".
	mnt := subEndpoints()
	r.Handle("/mnt", mnt)
	r.Handle("/mnt/{rest:.*}", mnt)
	r.PathPrefix("/strip").Handler(http.StripPrefix("/strip", subEndpoints()))

	// Declined shapes.
	sub := r.PathPrefix("/first").Subrouter()
	sub = r.PathPrefix("/second").Subrouter() // reassigned -> ambiguous -> declined
	sub.HandleFunc("/reassigned", ReassignedHandler).Methods("GET")

	r.HandleFunc("/dynamic", DynamicHandler).Methods(methodName()) // non-constant method -> declined

	r.Path("/builder").HandlerFunc(BuilderHandler).Methods("GET") // split builder form -> declined

	return r
}

func methodName() string { return "GET" }

// subEndpoints is a same-package sub-router constructor: its routes are
// emitted under each mount prefix, not standalone.
func subEndpoints() *mux.Router {
	sr := mux.NewRouter()
	sr.HandleFunc("/widget", WidgetHandler).Methods("GET")
	return sr
}

// requireAuth is a single-argument middleware wrapper.
func requireAuth(next http.Handler) http.Handler { return next }

// withLogging is a multi-argument middleware: only the http.Handler
// argument identifies the wrapped handler; io.Writer is ignored.
func withLogging(_ io.Writer, next http.Handler) http.Handler { return next }

// cors is a curried middleware: cors(origin) returns the wrapper.
func cors(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}

// logging is a router-level mux.MiddlewareFunc.
func logging(next http.Handler) http.Handler { return next }

// makeHandler is a free factory returning an already-wrapped handler.
func makeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
}

type controller struct{}

// build is a method factory returning an already-wrapped handler.
func (c *controller) build() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
}

func GetUser(w http.ResponseWriter, r *http.Request)           {}
func CreateUser(w http.ResponseWriter, r *http.Request)        {}
func HealthCheck(w http.ResponseWriter, r *http.Request)       {}
func GetItem(w http.ResponseWriter, r *http.Request)           {}
func QuantifierHandler(w http.ResponseWriter, r *http.Request) {}
func AnchoredHandler(w http.ResponseWriter, r *http.Request)   {}
func StaticHandler(w http.ResponseWriter, r *http.Request)     {}
func SecureHandler(w http.ResponseWriter, r *http.Request)     {}
func WrappedHandler(w http.ResponseWriter, r *http.Request)    {}
func LoggedHandler(w http.ResponseWriter, r *http.Request)     {}
func CorsHandler(w http.ResponseWriter, r *http.Request)       {}
func ListProducts(w http.ResponseWriter, r *http.Request)      {}
func AdminStats(w http.ResponseWriter, r *http.Request)        {}
func InlineThing(w http.ResponseWriter, r *http.Request)       {}
func InheritedHandler(w http.ResponseWriter, r *http.Request)  {}
func WidgetHandler(w http.ResponseWriter, r *http.Request)     {}
func ReassignedHandler(w http.ResponseWriter, r *http.Request) {}
func DynamicHandler(w http.ResponseWriter, r *http.Request)    {}
func BuilderHandler(w http.ResponseWriter, r *http.Request)    {}
