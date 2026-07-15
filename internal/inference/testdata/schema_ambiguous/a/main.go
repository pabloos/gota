// Package a is test data for TestResolveSchemaRefs_AmbiguousNameErrors: it
// declares a User type with the same name as package b's, so a "gota:"
// comment referencing "User" is ambiguous between the two.
package a

type User struct {
	ID int `json:"id"`
}
