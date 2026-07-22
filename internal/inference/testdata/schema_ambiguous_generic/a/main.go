// Package a declares a generic Box[T] AND the User type used as its
// argument. Package b declares its own Box[T]. The bare name "Box" is
// thus ambiguous across a and b, so componentName qualifies each
// instantiation's base ("a.Box_User", "b.Box_User") — the collision is
// on the generic BASE, pinning that componentName keys on the bare
// declared name, not the composed "Box_User".
package a

type Box[T any] struct {
	Item T `json:"item"`
}

type User struct {
	ID int `json:"id"`
}
