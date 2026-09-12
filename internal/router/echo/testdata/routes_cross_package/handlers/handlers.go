// Package handlers is test data for TestExtract_CrossPackage: its echo
// handlers are registered from a different package (main) than the one that
// declares them.
package handlers

import "github.com/labstack/echo/v4"

func GetUser(c echo.Context) error    { return nil }
func CreateUser(c echo.Context) error { return nil }
