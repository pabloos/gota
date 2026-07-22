// Package handlers is test data for TestExtract_CrossPackage: its
// handlers (and a sub-router constructor) are registered from a
// different package (main) than the one that declares them.
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func GetUser(w http.ResponseWriter, r *http.Request) {}

func CreateUser(w http.ResponseWriter, r *http.Request) {}

type Server struct{}

func (s *Server) GetItem(w http.ResponseWriter, r *http.Request) {}

// Srv is the instance main registers GetItem as a method value on.
var Srv = &Server{}

// WidgetsRouter is a sub-router constructor declared in this package,
// mounted from main -- a cross-package Mount target, which the plugin
// deliberately can't follow (Extract only ever sees one package), so
// its own registration must not surface at all.
func WidgetsRouter() chi.Router {
	r := chi.NewRouter()
	r.Get("/widgets", ListWidgets)
	return r
}

func ListWidgets(w http.ResponseWriter, r *http.Request) {}
