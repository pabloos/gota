// Package fixture is test data for
// TestResolveSchemaRefs_ByteSliceAndMarshaler: a []byte field serializes as
// a base64 string (encoding/json's special case), and a type with its own
// MarshalJSON is modeled as a free-form object rather than walked into its
// underlying []byte representation.
package fixture

// RawJSON carries its own MarshalJSON (like gorm.io/datatypes.JSON), so its
// underlying []byte must not be modeled as a byte array — the method emits
// whatever JSON it likes.
type RawJSON []byte

func (RawJSON) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }

type Event struct {
	ID      string  `json:"id"`
	Raw     []byte  `json:"raw"`
	Payload RawJSON `json:"payload"`
}
