// Package fixture is test data for internal/inference's dialect seam
// test (dialect_test.go): it mimics the call shape of a Gin-style
// framework — a context method combining status and payload in one
// call, on a handler signature that never touches http.ResponseWriter —
// without importing any real framework.
package fixture

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type fakeCtx struct{}

// JSON mimics gin's combined status+payload response call. Its body is
// deliberately empty: methods found in funcIndex are followable, so a
// real Encode in here would let the seam test pass via tryFollow even
// with a broken dialect — an empty body means only the dialect under
// test can produce a detection.
func (c *fakeCtx) JSON(code int, obj any) {}

func Create(c *fakeCtx) {
	c.JSON(201, User{ID: 1})
}

// respond wraps the framework call one level down, so the seam test
// also proves dialect recognition fires inside a followed helper frame
// (the dialect must ride along on the evalCtx) with the status and
// payload both resolving back to this call site's arguments.
func respond(c *fakeCtx, code int, data any) {
	c.JSON(code, data)
}

func CreateViaHelper(c *fakeCtx) {
	respond(c, 201, User{ID: 1})
}
