package openapiimport

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Spec is the subset of an OpenAPI 3.0/3.1 document the importer reads.
// Unknown fields are ignored on purpose: a spec is a large, evolving document
// and the importer must not fail because a vendor added an extension.
type Spec struct {
	OpenAPI    string                 `json:"openapi"`
	Info       Info                   `json:"info"`
	Paths      map[string]PathItem    `json:"paths"`
	Components Components             `json:"components"`
	Security   []map[string][]string  `json:"security"`
	Servers    []Server               `json:"servers"`
	Extensions map[string]interface{} `json:"-"`
}

// Info is the document metadata; Version feeds the manifest pin when the
// caller does not supply one.
type Info struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// Server is one base URL entry. Only the first is recorded as provenance.
type Server struct {
	URL string `json:"url"`
}

// PathItem holds the operations supported for one path. Only the methods the
// Tool Manifest can express are modelled.
type PathItem struct {
	Get     *Operation `json:"get"`
	Put     *Operation `json:"put"`
	Post    *Operation `json:"post"`
	Delete  *Operation `json:"delete"`
	Patch   *Operation `json:"patch"`
	Head    *Operation `json:"head"`
	Options *Operation `json:"options"`
}

// Operation is one HTTP operation.
type Operation struct {
	OperationID string                `json:"operationId"`
	Summary     string                `json:"summary"`
	Description string                `json:"description"`
	Tags        []string              `json:"tags"`
	Deprecated  bool                  `json:"deprecated"`
	Parameters  []Parameter           `json:"parameters"`
	RequestBody *RequestBody          `json:"requestBody"`
	Responses   map[string]Response   `json:"responses"`
	Security    []map[string][]string `json:"security"`
}

// Parameter is a path, query, header or cookie parameter.
type Parameter struct {
	Name        string                 `json:"name"`
	In          string                 `json:"in"`
	Description string                 `json:"description"`
	Required    bool                   `json:"required"`
	Schema      map[string]interface{} `json:"schema"`
}

// RequestBody describes the request payload.
type RequestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]MediaType `json:"content"`
}

// MediaType wraps one media-type schema.
type MediaType struct {
	Schema map[string]interface{} `json:"schema"`
}

// Response is one declared response.
type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content"`
}

// Components holds the reusable schemas and security schemes.
type Components struct {
	Schemas         map[string]interface{} `json:"schemas"`
	SecuritySchemes map[string]Security    `json:"securitySchemes"`
}

// Security is one security scheme definition.
type Security struct {
	Type   string `json:"type"`
	Scheme string `json:"scheme"`
	Name   string `json:"name"`
	In     string `json:"in"`
}

// Load parses an OpenAPI document. JSON is read directly; YAML is converted to
// JSON first so one struct and one set of `json` tags describe both formats.
func Load(doc []byte) (*Spec, error) {
	if strings.TrimSpace(string(doc)) == "" {
		return nil, fmt.Errorf("openapiimport: document is empty")
	}
	data := doc
	if !json.Valid(data) {
		var generic interface{}
		if err := yaml.Unmarshal(doc, &generic); err != nil {
			return nil, fmt.Errorf("openapiimport: document is neither JSON nor YAML: %w", err)
		}
		converted, err := json.Marshal(generic)
		if err != nil {
			return nil, fmt.Errorf("openapiimport: YAML document is not representable as JSON: %w", err)
		}
		data = converted
	}
	var spec Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("openapiimport: cannot read the document: %w", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.") {
		return nil, fmt.Errorf("openapiimport: openapi field %q is not 3.x (only OpenAPI 3.0/3.1 is supported)", spec.OpenAPI)
	}
	if len(spec.Paths) == 0 {
		return nil, fmt.Errorf("openapiimport: document declares no paths")
	}
	return &spec, nil
}

// methods returns the operations of a path item in a fixed order, so import
// output never depends on map iteration.
func (p PathItem) methods() []struct {
	name string
	op   *Operation
} {
	return []struct {
		name string
		op   *Operation
	}{
		{"get", p.Get},
		{"post", p.Post},
		{"put", p.Put},
		{"patch", p.Patch},
		{"delete", p.Delete},
		{"head", p.Head},
		{"options", p.Options},
	}
}
