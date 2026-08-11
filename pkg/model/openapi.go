// Package model defines the intermediate and output representations gota
// works with: the OpenAPI 3.1 document model used for emission, and the
// route/operation model used to move data between the parser, router
// plugins, inference and merger stages.
package model

import "net/http"

// Document is the root of an OpenAPI 3.1 document.
type Document struct {
	OpenAPI    string                `yaml:"openapi" json:"openapi"`
	Info       Info                  `yaml:"info" json:"info"`
	Servers    []Server              `yaml:"servers,omitempty" json:"servers,omitempty"`
	Security   []SecurityRequirement `yaml:"security,omitempty" json:"security,omitempty"`
	Tags       []Tag                 `yaml:"tags,omitempty" json:"tags,omitempty"`
	Paths      Paths                 `yaml:"paths" json:"paths"`
	Components *Components           `yaml:"components,omitempty" json:"components,omitempty"`
}

type Info struct {
	Title       string `yaml:"title" json:"title"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Server is one entry of the document-level "servers" list.
type Server struct {
	URL         string `yaml:"url" json:"url"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Tag is one entry of the document-level "tags" list.
type Tag struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// SecurityRequirement maps a security scheme name (defined in
// components.securitySchemes) to its required scopes — empty for schemes
// that don't use them, such as HTTP bearer. A list of these objects is
// an OR of ANDs, per the OpenAPI Security Requirement Object.
type SecurityRequirement map[string][]string

// DocumentMeta carries the document-level OpenAPI fields declared in a
// "gota:doc:" comment block — the parts that belong to the whole document
// rather than any single handler: info, servers, a global security
// requirement, tags, and components (notably securitySchemes). It is
// unmarshaled directly from the YAML under "gota:doc:", so its fields use
// the OpenAPI vocabulary exactly.
type DocumentMeta struct {
	Info       *Info                 `yaml:"info,omitempty" json:"info,omitempty"`
	Servers    []Server              `yaml:"servers,omitempty" json:"servers,omitempty"`
	Security   []SecurityRequirement `yaml:"security,omitempty" json:"security,omitempty"`
	Tags       []Tag                 `yaml:"tags,omitempty" json:"tags,omitempty"`
	Components *Components           `yaml:"components,omitempty" json:"components,omitempty"`
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

// Operations returns every non-nil Operation held by pi, in the
// conventional get/post/put/patch/delete/head/options/trace order.
func (pi *PathItem) Operations() []*Operation {
	all := []*Operation{pi.Get, pi.Post, pi.Put, pi.Patch, pi.Delete, pi.Head, pi.Options, pi.Trace}
	ops := make([]*Operation, 0, len(all))
	for _, op := range all {
		if op != nil {
			ops = append(ops, op)
		}
	}
	return ops
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
	Summary     string                `yaml:"summary,omitempty" json:"summary,omitempty"`
	Description string                `yaml:"description,omitempty" json:"description,omitempty"`
	OperationID string                `yaml:"operationId,omitempty" json:"operationId,omitempty"`
	Tags        []string              `yaml:"tags,omitempty" json:"tags,omitempty"`
	Parameters  []Parameter           `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	RequestBody *RequestBody          `yaml:"requestBody,omitempty" json:"requestBody,omitempty"`
	Responses   map[string]Response   `yaml:"responses,omitempty" json:"responses,omitempty"`
	Security    []SecurityRequirement `yaml:"security,omitempty" json:"security,omitempty"`
	Deprecated  bool                  `yaml:"deprecated,omitempty" json:"deprecated,omitempty"`

	// Skip is gota's own build-time directive, declared as "x-gota-skip"
	// — a real OpenAPI Specification Extension field, not an invented
	// DSL keyword — to exclude this operation from the emitted document
	// entirely. It is consumed by the generator and never itself emitted.
	Skip bool `yaml:"x-gota-skip,omitempty" json:"-"`
}

type Parameter struct {
	Name        string              `yaml:"name" json:"name"`
	In          string              `yaml:"in" json:"in"`
	Description string              `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool                `yaml:"required,omitempty" json:"required,omitempty"`
	Schema      *Schema             `yaml:"schema,omitempty" json:"schema,omitempty"`
	Example     any                 `yaml:"example,omitempty" json:"example,omitempty"`
	Examples    map[string]*Example `yaml:"examples,omitempty" json:"examples,omitempty"`
}

type RequestBody struct {
	Description string               `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool                 `yaml:"required,omitempty" json:"required,omitempty"`
	Content     map[string]MediaType `yaml:"content,omitempty" json:"content,omitempty"`
}

type MediaType struct {
	Schema   *Schema             `yaml:"schema,omitempty" json:"schema,omitempty"`
	Example  any                 `yaml:"example,omitempty" json:"example,omitempty"`
	Examples map[string]*Example `yaml:"examples,omitempty" json:"examples,omitempty"`
}

// Example is an OpenAPI Example Object, referenced from the "examples"
// map of a media type or parameter.
type Example struct {
	Summary     string `yaml:"summary,omitempty" json:"summary,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Value       any    `yaml:"value,omitempty" json:"value,omitempty"`
}

type Response struct {
	Description string               `yaml:"description" json:"description"`
	Content     map[string]MediaType `yaml:"content,omitempty" json:"content,omitempty"`
}

type Components struct {
	Schemas         map[string]*Schema         `yaml:"schemas,omitempty" json:"schemas,omitempty"`
	SecuritySchemes map[string]*SecurityScheme `yaml:"securitySchemes,omitempty" json:"securitySchemes,omitempty"`
}

// SecurityScheme is an OpenAPI Security Scheme Object. Its fields cover
// the "http" (bearer/basic), "apiKey" and "openIdConnect" scheme types;
// only the ones relevant to a given "type" are populated, and empty
// fields are omitted on emission.
type SecurityScheme struct {
	Type             string `yaml:"type" json:"type"`
	Description      string `yaml:"description,omitempty" json:"description,omitempty"`
	Name             string `yaml:"name,omitempty" json:"name,omitempty"`
	In               string `yaml:"in,omitempty" json:"in,omitempty"`
	Scheme           string `yaml:"scheme,omitempty" json:"scheme,omitempty"`
	BearerFormat     string `yaml:"bearerFormat,omitempty" json:"bearerFormat,omitempty"`
	OpenIDConnectURL string `yaml:"openIdConnectUrl,omitempty" json:"openIdConnectUrl,omitempty"`
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
	Example              any                `yaml:"example,omitempty" json:"example,omitempty"`
	Examples             []any              `yaml:"examples,omitempty" json:"examples,omitempty"`
}
