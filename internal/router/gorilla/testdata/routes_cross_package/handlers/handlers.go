// Package handlers is test data for TestExtract_CrossPackage: its
// gorilla handlers are registered from a different package (main).
package handlers

import "net/http"

func GetUser(w http.ResponseWriter, r *http.Request)    {}
func CreateUser(w http.ResponseWriter, r *http.Request) {}
