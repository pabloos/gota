// Package fixture is test data for the generic-instantiation
// ResolveSchemaRefs tests: Response[T] is a single-type-parameter
// generic type, instantiated with two different named structs (User,
// Product) so distinct instantiations can be checked not to collide on
// one component.
package fixture

type Response[T any] struct {
	Data T `json:"data"`
}

type User struct {
	ID int `json:"id"`
}

type Product struct {
	SKU string `json:"sku"`
}
