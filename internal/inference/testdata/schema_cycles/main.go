// Package fixture is test data for TestResolveSchemaRefs_HandlesCycles: two
// struct types that reference each other must resolve without infinite
// recursion.
package fixture

type A struct {
	B *B `json:"b,omitempty"`
}

type B struct {
	A *A `json:"a,omitempty"`
}
