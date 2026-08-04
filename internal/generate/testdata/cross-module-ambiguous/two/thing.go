// Package two also declares a Thing, but NO handler exposes it — it's only
// in the reachable graph, so a bare "Thing" ref must not treat it as a
// candidate that makes resolution ambiguous.
package two

type Thing struct {
	Name string `json:"name"`
}
