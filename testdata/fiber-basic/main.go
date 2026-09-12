// Package main is an end-to-end fixture for TestRun_FiberBasic: a fiber API
// through the fiber plugin + dialect — a group prefix, a :id path parameter,
// c.BodyParser request bodies, c.Status(code).JSON and c.JSON responses, a
// c.SendStatus 204, and a register function resolved via its call site.
package main

import "github.com/gofiber/fiber/v2"

// User is the API's resource.
type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// GetUser returns a user by id.
func GetUser(c *fiber.Ctx) error {
	return c.JSON(User{})
}

// CreateUser stores a new user.
func CreateUser(c *fiber.Ctx) error {
	var u User
	if err := c.BodyParser(&u); err != nil {
		return err
	}
	return c.Status(201).JSON(u)
}

// DeleteUser removes a user and returns no content.
func DeleteUser(c *fiber.Ctx) error {
	return c.SendStatus(204)
}

// registerUsers registers the user routes on a group passed by the caller.
func registerUsers(r fiber.Router) {
	r.Get("/users/:id", GetUser)
	r.Post("/users", CreateUser)
	r.Delete("/users/:id", DeleteUser)
}

func main() {
	app := fiber.New()
	registerUsers(app.Group("/api/v1"))
	app.Listen(":8080")
}
