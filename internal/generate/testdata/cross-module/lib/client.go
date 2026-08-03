// Package catalog is a SEPARATE module from the analyzed api module —
// its Product is a dependency type, not one of the analyzed roots,
// so resolving it exercises ResolveSchemaRefs' dependency fallback.
package catalog

type Product struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
