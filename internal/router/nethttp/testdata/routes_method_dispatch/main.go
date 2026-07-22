// Package fixture is test data for TestExtract_MethodDispatchFuncLit: it
// mirrors the hand-rolled method-dispatcher idiom found in a real,
// representative Go HTTP API (fayzan101/InventoryManagementSystem) —
// a method-less ServeMux pattern whose handler is an anonymous function
// switching or branching on r.Method, itself wrapped in a middleware
// call and an http.HandlerFunc conversion.
package fixture

import (
	"net/http"
	"strings"
)

func CreateProduct(w http.ResponseWriter, r *http.Request)  {}
func ListProducts(w http.ResponseWriter, r *http.Request)   {}
func GetWarehouse(w http.ResponseWriter, r *http.Request)   {}
func SearchProducts(w http.ResponseWriter, r *http.Request) {}
func GetProduct(w http.ResponseWriter, r *http.Request)     {}

func methodNotAllowed(w http.ResponseWriter) {}

// secure mirrors the real repo's middleware wrapper — an extra
// single-argument call layer between the ServeMux registration and the
// http.HandlerFunc conversion.
func secure(h http.Handler) http.Handler { return h }

func Setup() {
	mux := http.NewServeMux()

	// A switch-based dispatcher: two branches, each a direct delegate call.
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

	// An if/else single-method dispatcher.
	mux.Handle("/warehouses/", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			GetWarehouse(w, r)
		} else {
			methodNotAllowed(w)
		}
	})))

	// Deliberately too complex to recognize: a non-method guard
	// (strings.HasSuffix on the path) runs before the dispatch, so the
	// closure's body isn't a single top-level switch/if-else on
	// r.Method — must produce zero routes, not a partial guess.
	mux.Handle("/products/", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/search") {
			SearchProducts(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			GetProduct(w, r)
		default:
			methodNotAllowed(w)
		}
	})))

	// A "case A, B:" clause with multiple values declines the WHOLE
	// switch, not just that clause.
	mux.Handle("/multi-case", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			MultiCaseDelegate(w, r)
		default:
			methodNotAllowed(w)
		}
	})))

	// A case value that's a real string constant but not an HTTP
	// method declines the whole switch.
	mux.Handle("/non-http-method", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "FOOBAR":
			NonHTTPMethodDelegate(w, r)
		default:
			methodNotAllowed(w)
		}
	})))

	// A CONNECT case is skipped (no OpenAPI Path Item slot for it) but
	// doesn't abort its sibling GET case.
	mux.Handle("/connect-case", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodConnect:
			ConnectCaseDelegate(w, r)
		case http.MethodGet:
			ConnectCaseSibling(w, r)
		default:
			methodNotAllowed(w)
		}
	})))

	// A case body with more than one statement declines the whole
	// switch.
	mux.Handle("/multi-stmt-case", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			logDispatch(r)
			MultiStmtCaseDelegate(w, r)
		default:
			methodNotAllowed(w)
		}
	})))

	// A case value that's a variable, not a compile-time constant,
	// declines the whole switch.
	mux.Handle("/dynamic-case", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case dynamicMethod:
			DynamicCaseDelegate(w, r)
		default:
			methodNotAllowed(w)
		}
	})))

	// An if statement with an init statement declines the whole chain.
	mux.Handle("/if-init", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok := true; r.Method == http.MethodGet && ok {
			IfInitDelegate(w, r)
		} else {
			methodNotAllowed(w)
		}
	})))

	// A condition unrelated to r.Method declines the whole chain.
	mux.Handle("/if-non-method", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/if-non-method" {
			IfNonMethodDelegate(w, r)
		} else {
			methodNotAllowed(w)
		}
	})))

	// An if body with more than one statement declines the whole chain.
	mux.Handle("/if-multi-stmt", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			logDispatch(r)
			IfMultiStmtDelegate(w, r)
		} else {
			methodNotAllowed(w)
		}
	})))

	// A CONNECT branch in an if/else-if chain is skipped but doesn't
	// abort the chain — the sibling GET branch still resolves.
	mux.Handle("/if-connect", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			IfConnectDelegate(w, r)
		} else if r.Method == http.MethodGet {
			IfConnectSibling(w, r)
		} else {
			methodNotAllowed(w)
		}
	})))

	// A comparison against a selector whose name isn't "Method" at all
	// declines the whole chain.
	mux.Handle("/wrong-selector", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == wrongSelectorPath {
			WrongSelectorDelegate(w, r)
		} else {
			methodNotAllowed(w)
		}
	})))

	// A ".Method" selector on something that isn't *http.Request (here,
	// a same-named field on an unrelated local type) declines the whole
	// chain — checked by type, not by field name alone. The composite
	// literal is inlined so the closure body stays a single top-level
	// if statement (methodBranches declines outright at more than one).
	mux.Handle("/non-request-method", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (dispatchJob{Method: "GET"}).Method == http.MethodGet {
			NonRequestMethodDelegate(w, r)
		} else {
			methodNotAllowed(w)
		}
	})))

	// A branch body calling a 1-argument delegate (not the "delegate(w,
	// r)" 2-argument shape) declines the whole switch.
	mux.Handle("/wrong-arity", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			methodNotAllowed(w)
		default:
			methodNotAllowed(w)
		}
	})))

	// A branch body that isn't a call at all (an assignment) declines
	// the whole switch.
	mux.Handle("/not-a-call", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handled := true
			_ = handled
		default:
			methodNotAllowed(w)
		}
	})))

	// A single top-level statement that's neither a switch nor an
	// if/else chain on r.Method declines entirely.
	mux.Handle("/not-dispatch-shaped", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		NotDispatchShapedDelegate(w, r)
	})))

	// The comparison written with r.Method as the RIGHT operand instead
	// of the left -- methodEqualityCond checks both orders.
	mux.Handle("/reversed-operands", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if http.MethodGet == r.Method {
			ReversedOperandsDelegate(w, r)
		} else {
			methodNotAllowed(w)
		}
	})))

	// A "!=" comparison, not "==" -- methodEqualityCond only recognizes
	// equality.
	mux.Handle("/not-equal", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
		} else {
			NotEqualDelegate(w, r)
		}
	})))

	// A single "if" with no "else" at all -- still a complete,
	// recognizable one-method dispatcher.
	mux.Handle("/if-no-else", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			IfNoElseDelegate(w, r)
		}
	})))

	// An if/else comparing r.Method against a string that isn't a real
	// HTTP method -- the if-chain sibling of the switch-based
	// "/non-http-method" case above.
	mux.Handle("/if-non-http-method", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "FOOBAR" {
			IfNonHTTPMethodDelegate(w, r)
		} else {
			methodNotAllowed(w)
		}
	})))

	// A branch whose delegate call isn't a resolvable identifier or
	// selector (here, an immediately-invoked function literal) declines
	// the whole switch -- resolveHandler has no case for it.
	mux.Handle("/unresolvable-delegate", secure(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			func(w http.ResponseWriter, r *http.Request) {}(w, r)
		default:
			methodNotAllowed(w)
		}
	})))

	// A custom type's own same-named "HandleFunc" method must not be
	// mistaken for a ServeMux registration.
	var custom customHandler
	custom.HandleFunc("/not-a-real-route", CustomHandlerDelegate)
}

// customHandler has its own HandleFunc method, unrelated to
// net/http.ServeMux -- exercises isServeMuxOrHTTPPackageCall's type
// check, not just the method-name match.
type customHandler struct{}

func (customHandler) HandleFunc(pattern string, h http.HandlerFunc) {}

func CustomHandlerDelegate(w http.ResponseWriter, r *http.Request) {}

// dispatchJob has its own "Method" field, unrelated to *http.Request —
// exercises isRequestMethodSelector's type check, not just its field
// name check.
type dispatchJob struct {
	Method string
}

var dynamicMethod = http.MethodGet

const wrongSelectorPath = "/wrong-selector"

func logDispatch(r *http.Request) {}

func MultiCaseDelegate(w http.ResponseWriter, r *http.Request)         {}
func NonHTTPMethodDelegate(w http.ResponseWriter, r *http.Request)     {}
func ConnectCaseDelegate(w http.ResponseWriter, r *http.Request)       {}
func ConnectCaseSibling(w http.ResponseWriter, r *http.Request)        {}
func MultiStmtCaseDelegate(w http.ResponseWriter, r *http.Request)     {}
func DynamicCaseDelegate(w http.ResponseWriter, r *http.Request)       {}
func IfInitDelegate(w http.ResponseWriter, r *http.Request)            {}
func IfNonMethodDelegate(w http.ResponseWriter, r *http.Request)       {}
func IfMultiStmtDelegate(w http.ResponseWriter, r *http.Request)       {}
func IfConnectDelegate(w http.ResponseWriter, r *http.Request)         {}
func IfConnectSibling(w http.ResponseWriter, r *http.Request)          {}
func WrongSelectorDelegate(w http.ResponseWriter, r *http.Request)     {}
func NonRequestMethodDelegate(w http.ResponseWriter, r *http.Request)  {}
func NotDispatchShapedDelegate(w http.ResponseWriter, r *http.Request) {}
func ReversedOperandsDelegate(w http.ResponseWriter, r *http.Request)  {}
func NotEqualDelegate(w http.ResponseWriter, r *http.Request)          {}
func IfNoElseDelegate(w http.ResponseWriter, r *http.Request)          {}
func IfNonHTTPMethodDelegate(w http.ResponseWriter, r *http.Request)   {}
