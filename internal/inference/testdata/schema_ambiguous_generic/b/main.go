// Package b declares its own generic Box[T], colliding on the bare name
// "Box" with package a's. See a/main.go.
package b

type Box[T any] struct {
	Label string `json:"label"`
	Item  T      `json:"item"`
}
