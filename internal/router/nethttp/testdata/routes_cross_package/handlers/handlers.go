// Package handlers is test data for TestExtract_CrossPackageHandler: its
// handlers are registered from a different package (main) than the one
// that declares them, exercising every shape resolveHandler recognizes —
// a bare qualified identifier, an http.HandlerFunc conversion of one, and
// a cross-package method value.
package handlers

import "net/http"

func GetUser(w http.ResponseWriter, r *http.Request) {}

func CreateUser(w http.ResponseWriter, r *http.Request) {}

type Server struct{}

func (s *Server) GetItem(w http.ResponseWriter, r *http.Request) {}

// Srv is the instance main registers GetItem as a method value on.
var Srv = &Server{}
