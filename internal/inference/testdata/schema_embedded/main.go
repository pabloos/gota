// Package fixture is test data for
// TestResolveSchemaRefs_EmbeddedStructPromotesFields: an embedded struct's
// fields should be flattened into the parent, not registered as their own
// separate component.
package fixture

type Base struct {
	ID int `json:"id"`
}

type User struct {
	Base
	Name string `json:"name"`
}
