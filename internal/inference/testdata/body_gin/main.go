// Package fixture is test data for dialect_gin_test.go: real gin
// handlers exercising the gin dialect's recognizers (c.JSON with an
// explicit code, a gin.H envelope, c.ShouldBindJSON) plus the embedded
// net/http fallback (json.NewDecoder(c.Request.Body).Decode).
package fixture

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func CreateUser(c *gin.Context) {
	var req User
	c.ShouldBindJSON(&req)
	c.JSON(201, User{ID: 1, Name: "Ada"})
}

func GetUserEnvelope(c *gin.Context) {
	user := User{ID: 1}
	c.JSON(200, gin.H{"status": "ok", "data": user})
}

// DecodeViaStdlib uses plain encoding/json on c.Request.Body — the
// embedded netHTTPDialect must still recognize this inside a gin handler.
func DecodeViaStdlib(c *gin.Context) {
	var u User
	json.NewDecoder(c.Request.Body).Decode(&u)
	c.JSON(200, u)
}
