// Package fixture is test data for TestResolveSchemaRefs_Slices: a slice
// of a named struct type (items should be a $ref) and a slice of a
// primitive type (items should be inline).
package fixture

type Tag struct {
	Name string `json:"name"`
}

type User struct {
	Tags  []Tag    `json:"tags"`
	Roles []string `json:"roles"`
}
