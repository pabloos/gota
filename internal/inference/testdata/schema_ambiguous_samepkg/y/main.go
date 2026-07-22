// Package config (at path .../y), same package NAME and type NAME as
// .../x's -- see x/main.go.
package config

type Settings struct {
	Port int `json:"port"`
}
