// Package fixture is test data for TestResolveSchemaRefs_TimeTimeField:
// time.Time is special-cased to {type: string, format: date-time} instead
// of being walked as a struct.
package fixture

import "time"

type User struct {
	CreatedAt time.Time `json:"created_at"`
}
