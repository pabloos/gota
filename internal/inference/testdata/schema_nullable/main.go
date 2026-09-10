// Package fixture is test data for TestResolveSchemaRefs_NullablePointers:
// a pointer field can serialize to JSON null, so gota models it nullable —
// a scalar as an OpenAPI 3.1 type array ([T, "null"]) and a referenced
// struct as anyOf: [{$ref}, {type: null}]. A non-pointer field is not
// nullable, and nullability is independent of whether the field is required.
package fixture

import "time"

type Address struct {
	City string `json:"city"`
}

type User struct {
	Name    string     `json:"name"`              // non-pointer, required, not nullable
	Nick    *string    `json:"nick,omitempty"`    // nullable scalar, optional
	Since   *time.Time `json:"since,omitempty"`   // nullable, keeps the time format
	Address *Address   `json:"address,omitempty"` // nullable reference
}
