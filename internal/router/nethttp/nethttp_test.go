package nethttp_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/pabloos/gota/internal/parser"
	"github.com/pabloos/gota/internal/router"
	"github.com/pabloos/gota/internal/router/nethttp"
)

const fixtureSrc = `package fixture

import "net/http"

func Handlers() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", GetUser)
	mux.HandleFunc("/legacy", LegacyHandler)
	mux.Handle("POST /users", http.HandlerFunc(CreateUser))
	http.HandleFunc("/health", HealthCheck)
	mux.HandleFunc("example.com/hosted", HostedHandler)

	notARoute := map[string]string{"HandleFunc": "not a call"}
	_ = notARoute
	return mux
}

// gota:
//   summary: Get a user by ID
func GetUser(w http.ResponseWriter, r *http.Request) {}

func LegacyHandler(w http.ResponseWriter, r *http.Request) {}

func CreateUser(w http.ResponseWriter, r *http.Request) {}

func HealthCheck(w http.ResponseWriter, r *http.Request) {}

func HostedHandler(w http.ResponseWriter, r *http.Request) {}
`

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.22.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(fixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgs, err := parser.Load(dir)
	if err != nil {
		t.Fatalf("parser.Load: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}

	plugin := nethttp.New()
	routes, err := plugin.Extract(pkgs[0])
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	got := map[string]router.Route{}
	for _, r := range routes {
		got[r.Method+" "+r.Path] = r
	}

	want := []string{
		http.MethodGet + " /users/{id}",
		http.MethodGet + " /legacy", // no method in pattern -> defaults to GET
		http.MethodPost + " /users",
		http.MethodGet + " /health", // http.HandleFunc (package-level call)
		http.MethodGet + " /hosted", // host-prefixed pattern, host stripped
	}
	if len(got) != len(want) {
		t.Fatalf("got %d routes, want %d: %+v", len(got), len(want), got)
	}
	for _, key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("missing route %q, got: %+v", key, got)
		}
	}

	getUser, ok := got[http.MethodGet+" /users/{id}"]
	if !ok {
		t.Fatalf("missing GET /users/{id}")
	}
	if getUser.HandlerName != "GetUser" {
		t.Errorf("HandlerName = %q, want GetUser", getUser.HandlerName)
	}
	if getUser.HandlerDecl == nil {
		t.Errorf("HandlerDecl is nil, want resolved FuncDecl")
	}

	createUser, ok := got[http.MethodPost+" /users"]
	if !ok {
		t.Fatalf("missing POST /users")
	}
	if createUser.HandlerName != "CreateUser" {
		t.Errorf("HandlerName = %q, want CreateUser", createUser.HandlerName)
	}
}

func TestName(t *testing.T) {
	if nethttp.New().Name() != "net/http" {
		t.Errorf("Name() = %q, want net/http", nethttp.New().Name())
	}
}

// methodHandlerFixtureSrc exercises the common dependency-injection pattern
// where handlers are methods on a server struct, bound as method values
// (srv.GetItem) rather than referenced as bare package-level functions.
const methodHandlerFixtureSrc = `package fixture

import "net/http"

type Server struct{}

func Handlers(srv *Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", srv.GetItem)
	return mux
}

// gota:
//   summary: Get an item by ID
func (s *Server) GetItem(w http.ResponseWriter, r *http.Request) {}
`

func TestExtract_MethodValueHandler(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.22.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(methodHandlerFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgs, err := parser.Load(dir)
	if err != nil {
		t.Fatalf("parser.Load: %v", err)
	}

	routes, err := nethttp.New().Extract(pkgs[0])
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1: %+v", len(routes), routes)
	}
	r := routes[0]
	if r.Method != http.MethodGet || r.Path != "/items/{id}" {
		t.Fatalf("route = %+v", r)
	}
	if r.HandlerName != "GetItem" {
		t.Errorf("HandlerName = %q, want GetItem", r.HandlerName)
	}
	if r.HandlerDecl == nil {
		t.Fatalf("HandlerDecl is nil: a method value handler must still resolve back to its *ast.FuncDecl so its \"gota:\" comment can be extracted")
	}
	if r.HandlerDecl.Recv == nil {
		t.Errorf("resolved decl has no receiver; resolved the wrong FuncDecl")
	}
	if r.HandlerDecl.Doc == nil {
		t.Errorf("resolved decl has no doc comment, want the \"gota:\" block above (s *Server) GetItem")
	}
}
