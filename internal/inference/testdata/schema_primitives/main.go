// Package fixture is test data for TestResolveSchemaRefs_PrimitivesAndOmitempty:
// primitive fields, an omitempty field, an unexported field, and a
// json:"-" field.
package fixture

type User struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Bio     string `json:"bio,omitempty"`
	skip    string
	Ignored string `json:"-"`
}
