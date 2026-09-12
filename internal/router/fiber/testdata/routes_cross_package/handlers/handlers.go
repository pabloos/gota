// Package handlers is test data for TestExtract_CrossPackage: its fiber
// handlers are registered from a different package (main) than the one that
// declares them.
package handlers

import "github.com/gofiber/fiber/v2"

func GetUser(c *fiber.Ctx) error    { return nil }
func CreateUser(c *fiber.Ctx) error { return nil }
