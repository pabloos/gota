// Package fixture is test data for
// TestResolveSchemaRefs_NestedStructBecomesLinkedComponent: a struct field
// whose type is itself a named struct should become its own linked
// component instead of being inlined.
package fixture

type Address struct {
	City string `json:"city"`
}

type User struct {
	Name    string  `json:"name"`
	Address Address `json:"address"`
}
