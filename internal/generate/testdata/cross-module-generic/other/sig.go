// Package other ALSO declares a Signature (no handler exposes it), making
// the bare name "Signature" ambiguous across the reachable graph.
package other

type Signature struct {
	Other string `json:"other"`
}
