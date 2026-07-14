// Package model defines the intermediate and output representations gota
// works with: the OpenAPI 3.1 document model used for emission, and the
// route/operation model used to move data between the parser, router
// plugins, inference and merger stages.
package model

import "net/http"

// Document is the root of an OpenAPI 3.1 document.
type Document struct {
	OpenAPI    string      `yaml:"openapi" json:"openapi"`
	Info       Info        `yaml:"info" json:"info"`
	Paths      Paths       `yaml:"paths" json:"paths"`
	Components *Components `yaml:"components,omitempty" json:"components,omitempty"`
}

type Info struct {
	Title   string `yaml:"title" json:"title"`
	Version string `yaml:"version" json:"version"`
}

// Paths maps a URL path template (e.g. "/users/{id}") to its PathItem.
type Paths map[string]*PathItem

// PathItem holds one Operation per HTTP method. Fields are explicit
// (rather than a map) so YAML/JSON emission has a stable, conventional
// key order (get, post, put, ...) without needing custom sorting logic.
type PathItem struct {
	Get     *Operation `yaml:"get,omitempty" json:"get,omitempty"`
	Post    *Operation `yaml:"post,omitempty" json:"post,omitempty"`
	Put     *Operation `yaml:"put,omitempty" json:"put,omitempty"`
	Patch   *Operation `yaml:"patch,omitempty" json:"patch,omitempty"`
	Delete  *Operation `yaml:"delete,omitempty" json:"delete,omitempty"`
	Head    *Operation `yaml:"head,omitempty" json:"head,omitempty"`
	Options *Operation `yaml:"options,omitempty" json:"options,omitempty"`
	Trace   *Operation `yaml:"trace,omitempty" json:"trace,omitempty"`
}

// Set assigns op to the field matching method (an uppercase HTTP verb).
// It reports false without modifying pi if method has no corresponding
// OpenAPI Path Item field — this is the case for CONNECT, which OpenAPI's
// spec deliberately has no operation slot for. Callers must check the
// return value rather than assume the operation was recorded.
func (pi *PathItem) Set(method string, op *Operation) bool {
	switch method {
	case http.MethodGet:
		pi.Get = op
	case http.MethodPost:
		pi.Post = op
	case http.MethodPut:
		pi.Put = op
	case http.MethodPatch:
		pi.Patch = op
	case http.MethodDelete:
		pi.Delete = op
	case http.MethodHead:
		pi.Head = op
	case http.MethodOptions:
		pi.Options = op
	case http.MethodTrace:
		pi.Trace = op
	default:
		return false
	}
	return true
}

// ForMethod returns the operation registered for method, or nil.
func (pi *PathItem) ForMethod(method string) *Operation {
	switch method {
	case http.MethodGet:
		return pi.Get
	case http.MethodPost:
		return pi.Post
	case http.MethodPut:
		return pi.Put
	case http.MethodPatch:
		return pi.Patch
	case http.MethodDelete:
		return pi.Delete
	case http.MethodHead:
		return pi.Head
	case http.MethodOptions:
		return pi.Options
	case http.MethodTrace:
		return pi.Trace
	}
	return nil
}

// Operation is a fragment of the OpenAPI Operation Object. Its YAML/JSON
// tags match the OpenAPI vocabulary exactly, since "gota:" comment blocks
// are unmarshaled directly into this type: the comment IS OpenAPI, not an
// intermediate DSL.
type Operation struct {
	Summary     string              `yaml:"summary,omitempty" json:"summary,omitempty"`
	Description string              `yaml:"description,omitempty" json:"description,omitempty"`
	OperationID string              `yaml:"operationId,omitempty" json:"operationId,omitempty"`
	Tags        []string            `yaml:"tags,omitempty" json:"tags,omitempty"`
	Parameters  []Parameter         `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	RequestBody *RequestBody        `yaml:"requestBody,omitempty" json:"requestBody,omitempty"`
	Responses   map[string]Response `yaml:"responses,omitempty" json:"responses,omitempty"`
	Deprecated  bool                `yaml:"deprecated,omitempty" json:"deprecated,omitempty"`
}

type Parameter struct {
	Name        string  `yaml:"name" json:"name"`
	In          string  `yaml:"in" json:"in"`
	Description string  `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool    `yaml:"required,omitempty" json:"required,omitempty"`
	Schema      *Schema `yaml:"schema,omitempty" json:"schema,omitempty"`
}

type RequestBody struct {
	Description string               `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool                 `yaml:"required,omitempty" json:"required,omitempty"`
	Content     map[string]MediaType `yaml:"content,omitempty" json:"content,omitempty"`
}

type MediaType struct {
	Schema *Schema `yaml:"schema,omitempty" json:"schema,omitempty"`
}

type Response struct {
	Description string               `yaml:"description" json:"description"`
	Content     map[string]MediaType `yaml:"content,omitempty" json:"content,omitempty"`
}

type Components struct {
	Schemas map[string]*Schema `yaml:"schemas,omitempty" json:"schemas,omitempty"`
}

// Schema is a (partial) JSON Schema / OpenAPI Schema Object.
type Schema struct {
	Ref                  string             `yaml:"$ref,omitempty" json:"$ref,omitempty"`
	Type                 string             `yaml:"type,omitempty" json:"type,omitempty"`
	Format               string             `yaml:"format,omitempty" json:"format,omitempty"`
	Description          string             `yaml:"description,omitempty" json:"description,omitempty"`
	Items                *Schema            `yaml:"items,omitempty" json:"items,omitempty"`
	Properties           map[string]*Schema `yaml:"properties,omitempty" json:"properties,omitempty"`
	Required             []string           `yaml:"required,omitempty" json:"required,omitempty"`
	Enum                 []any              `yaml:"enum,omitempty" json:"enum,omitempty"`
	AdditionalProperties *Schema            `yaml:"additionalProperties,omitempty" json:"additionalProperties,omitempty"`
	Default              any                `yaml:"default,omitempty" json:"default,omitempty"`
	Nullable             bool               `yaml:"nullable,omitempty" json:"nullable,omitempty"`
}
