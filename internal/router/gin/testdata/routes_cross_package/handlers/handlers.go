// Package handlers is test data for TestExtract_CrossPackage: its gin
// handlers are registered from a different package (main) than the one
// that declares them.
package handlers

import "github.com/gin-gonic/gin"

func GetUser(c *gin.Context)    {}
func CreateUser(c *gin.Context) {}
