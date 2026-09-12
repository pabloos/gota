// Package main is an end-to-end fixture for TestRun_EchoBasic: an echo API
// exercising the full pipeline through the echo plugin + dialect — a group
// prefix, a :id path parameter, c.Bind request bodies, c.JSON responses at
// explicit codes, a c.NoContent 204, and an inline func-literal handler.
package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// User is the API's resource.
type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// GetUser returns a user by id.
func GetUser(c echo.Context) error {
	return c.JSON(http.StatusOK, User{})
}

// CreateUser stores a new user.
func CreateUser(c echo.Context) error {
	var u User
	if err := c.Bind(&u); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, u)
}

// DeleteUser removes a user and returns no content.
func DeleteUser(c echo.Context) error {
	return c.NoContent(http.StatusNoContent)
}

func main() {
	e := echo.New()

	v1 := e.Group("/api/v1")
	v1.GET("/users/:id", GetUser)
	v1.POST("/users", CreateUser)
	v1.DELETE("/users/:id", DeleteUser)

	e.GET("/version", func(c echo.Context) error {
		return c.JSON(http.StatusOK, User{})
	})

	e.Logger.Fatal(e.Start(":8080"))
}
