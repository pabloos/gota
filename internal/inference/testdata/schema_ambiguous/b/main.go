// Package b is test data for TestResolveSchemaRefs_AmbiguousNameErrors: it
// declares a User type with the same name as package a's, so a "gota:"
// comment referencing "User" is ambiguous between the two.
package b

type User struct {
	Name string `json:"name"`
}
