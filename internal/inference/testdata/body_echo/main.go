// Package fixture is test data for dialect_echo_test.go: real echo handlers
// exercising the echo dialect's recognizers (c.Bind, c.JSON with an
// explicit code, c.NoContent) plus the embedded net/http fallback
// (json.NewDecoder(c.Request().Body).Decode).
package fixture

import (
	"encoding/json"

	"github.com/labstack/echo/v4"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// CreateUser binds the request body and responds with c.JSON at code 201.
func CreateUser(c echo.Context) error {
	var req User
	if err := c.Bind(&req); err != nil {
		return err
	}
	return c.JSON(201, User{ID: 1, Name: "Ada"})
}

// DeleteUser responds with a bodyless 204 via c.NoContent.
func DeleteUser(c echo.Context) error {
	return c.NoContent(204)
}

// DecodeViaStdlib uses plain encoding/json on c.Request().Body — the
// embedded netHTTPDialect must still recognize this inside an echo handler.
func DecodeViaStdlib(c echo.Context) error {
	var u User
	json.NewDecoder(c.Request().Body).Decode(&u)
	return c.JSON(200, u)
}
