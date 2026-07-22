// Package config (at path .../x) declares Settings, the same package
// NAME and type NAME as .../y's. componentName qualifies both to
// "config.Settings" -- package-name qualification can't tell them
// apart, so register's origin guard must error rather than silently
// overwrite one with the other.
package config

type Settings struct {
	Host string `json:"host"`
}
