// Package handlers is test data for TestDetectBody_CrossPackage: its
// handler calls a response helper declared in a different package
// (httputil), which itself delegates one more level to a second helper
// in that same package — exercising cross-package following and
// multi-level following at once, the real-world shape that motivated
// both.
package handlers

import (
	"net/http"

	"github.com/pabloos/gota/internal/inference/testdata/body_cross_package/httputil"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func CreateUser(w http.ResponseWriter, r *http.Request) {
	user := User{ID: 1, Name: "Ada"}
	httputil.Success(w, http.StatusCreated, user)
}
