// Package fixture is test data for dialect_fiber_test.go: real fiber handlers
// exercising the fiber dialect's recognizers (c.BodyParser, c.Status(code).
// JSON, c.JSON at the ambient status, c.SendStatus) plus the embedded
// net/http fallback (json.Marshal).
package fixture

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// CreateUser binds the body and responds with c.Status(201).JSON.
func CreateUser(c *fiber.Ctx) error {
	var req User
	if err := c.BodyParser(&req); err != nil {
		return err
	}
	return c.Status(201).JSON(User{ID: 1, Name: "Ada"})
}

// GetUser responds with c.JSON at the ambient (default 200) status.
func GetUser(c *fiber.Ctx) error {
	return c.JSON(User{ID: 1})
}

// DeleteUser responds with a bodyless 204 via c.SendStatus.
func DeleteUser(c *fiber.Ctx) error {
	return c.SendStatus(204)
}

// MarshalViaStdlib uses plain encoding/json — the embedded netHTTPDialect
// must still recognize this inside a fiber handler.
func MarshalViaStdlib(c *fiber.Ctx) error {
	b, _ := json.Marshal(User{ID: 2})
	return c.Send(b)
}
