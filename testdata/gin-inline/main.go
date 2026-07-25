// Package main is an end-to-end fixture for TestRun_GinInline: a gin
// route whose handler is an inline function literal (no named function,
// so no HandlerName/HandlerDecl/HandlerObj). It proves the full pipeline
// still (a) emits the route with an operationId synthesized from the
// method+path and (b) infers the response body straight from the
// literal's own c.JSON call.
package main

import "github.com/gin-gonic/gin"

// VersionInfo is the inline handler's response payload.
type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

func main() {
	r := gin.New()
	r.GET("/version", func(c *gin.Context) {
		c.JSON(200, VersionInfo{Version: "1.0.0", Commit: "abc123"})
	})
	_ = r.Run()
}
