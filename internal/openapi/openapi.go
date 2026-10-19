// Package openapi reads the checked-in copy of the public API's OpenAPI schema
// (openapi.json). Two things come from it:
//
//   - the enum values the CLI's flags accept (generated into
//     internal/client/enums_gen.go by `go generate ./...`, so help text never
//     drifts from the server), and
//   - the response examples, which the tests serve from an httptest server.
//
// Refresh the copy from a live server with `make openapi`.
package openapi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"go/format"
	"sort"
	"strings"
)

//go:embed openapi.json
var Raw []byte

// Doc is the subset of an OpenAPI 3.x document the CLI reads.
type Doc struct {
	Paths      map[string]map[string]*Operation `json:"paths"`
	Components struct {
		Schemas map[string]*Schema `json:"schemas"`
	} `json:"components"`
}

// Operation is one method on one path.
type Operation struct {
	OperationID string               `json:"operationId"`
	RequestBody *Body                `json:"requestBody"`
	Responses   map[string]*Response `json:"responses"`
}

// Body is a request body.
type Body struct {
	Content map[string]*Media `json:"content"`
}

// Response is one status code's answer.
type Response struct {
	Content map[string]*Media `json:"content"`
}

// Media is one content type with its schema and named examples.
type Media struct {
	Schema   *Schema             `json:"schema"`
	Examples map[string]*Example `json:"examples"`
}

// Example is a named example value.
type Example struct {
	Value json.RawMessage `json:"value"`
}

// Schema is the subset of a JSON Schema the enum walk needs.
type Schema struct {
	Ref        string             `json:"$ref"`
	Type       any                `json:"type"`
	Enum       []any              `json:"enum"`
	Properties map[string]*Schema `json:"properties"`
	Items      *Schema            `json:"items"`
	OneOf      []*Schema          `json:"oneOf"`
	AnyOf      []*Schema          `json:"anyOf"`
	AllOf      []*Schema          `json:"allOf"`
}

// Parse decodes a schema document.
func Parse(raw []byte) (*Doc, error) {
	d := &Doc{}
	if err := json.Unmarshal(raw, d); err != nil {
		return nil, fmt.Errorf("openapi: %w", err)
	}
	return d, nil
}

// Load parses the embedded copy.
func Load() (*Doc, error) { return Parse(Raw) }

// Find returns the operation for method on the first path ending in suffix
// (the live schema may or may not carry the /api/v1/public prefix).
func (d *Doc) Find(method, suffix string) (string, *Operation) {
	keys := make([]string, 0, len(d.Paths))
	for k := range d.Paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, p := range keys {
		if strings.HasSuffix(p, suffix) {
			if op := d.Paths[p][strings.ToLower(method)]; op != nil {
				return p, op
			}
		}
	}
	return "", nil
}

func (d *Doc) resolve(s *Schema) *Schema {
	for i := 0; s != nil && s.Ref != "" && i < 16; i++ {
		name := strings.TrimPrefix(s.Ref, "#/components/schemas/")
		s = d.Components.Schemas[name]
	}
	return s
}

// enumsOf collects every enum under s, keyed by dotted property path
// ("options.media_type", "uploads.kind"; array items add nothing to the path).
func (d *Doc) enumsOf(s *Schema, prefix string, out map[string][]string, depth int) {
	s = d.resolve(s)
	if s == nil || depth > 12 {
		return
	}
	if len(s.Enum) > 0 && prefix != "" {
		for _, v := range s.Enum {
			if str, ok := v.(string); ok && !contains(out[prefix], str) {
				out[prefix] = append(out[prefix], str)
			}
		}
	}
	for _, sub := range append(append(append([]*Schema{}, s.OneOf...), s.AnyOf...), s.AllOf...) {
		d.enumsOf(sub, prefix, out, depth+1)
	}
	if s.Items != nil {
		d.enumsOf(s.Items, prefix, out, depth+1)
	}
	for name, p := range s.Properties {
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		d.enumsOf(p, key, out, depth+1)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Source names one request or response schema the CLI reads enums from.
type Source struct {
	Prefix string // the key prefix in the generated table ("create")
	Method string
	Suffix string // path suffix ("/videos")
	Status string // "" = the request body; else a response status
}

// Sources are the operations whose enums the CLI's flags use.
var Sources = []Source{
	{"create", "POST", "/videos", ""},
	{"video", "GET", "/videos/{task_id}", "200"},
	{"plan_edit", "POST", "/videos/{task_id}/plan/edit", ""},
	{"clone", "POST", "/video/clone", ""},
	{"studio_image", "POST", "/studio/images", ""},
}

// Enums returns "<prefix>.<property path>" → allowed values for every Source.
// A Source missing from the schema contributes nothing (the CLI then skips
// client-side checks for it and the server answers).
func (d *Doc) Enums() map[string][]string {
	out := map[string][]string{}
	for _, src := range Sources {
		_, op := d.Find(src.Method, src.Suffix)
		if op == nil {
			continue
		}
		var media *Media
		if src.Status == "" {
			if op.RequestBody != nil {
				media = op.RequestBody.Content["application/json"]
			}
		} else if r := op.Responses[src.Status]; r != nil {
			media = r.Content["application/json"]
		}
		if media == nil {
			continue
		}
		local := map[string][]string{}
		d.enumsOf(media.Schema, "", local, 0)
		for k, v := range local {
			out[src.Prefix+"."+k] = v
		}
	}
	return out
}

// Example returns a response example for method+path suffix and status.
// name "" picks the first example by name.
func (d *Doc) Example(method, suffix, status, name string) (json.RawMessage, bool) {
	_, op := d.Find(method, suffix)
	if op == nil || op.Responses[status] == nil {
		return nil, false
	}
	m := op.Responses[status].Content["application/json"]
	if m == nil || len(m.Examples) == 0 {
		return nil, false
	}
	if name == "" {
		names := make([]string, 0, len(m.Examples))
		for k := range m.Examples {
			names = append(names, k)
		}
		sort.Strings(names)
		name = names[0]
	}
	ex := m.Examples[name]
	if ex == nil {
		return nil, false
	}
	return ex.Value, true
}

// GoSource renders the enums as the Go file internal/client/enums_gen.go.
func GoSource(enums map[string][]string) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by internal/openapi/enumgen from internal/openapi/openapi.json; DO NOT EDIT.\n\n")
	b.WriteString("package client\n\n")
	b.WriteString("// Enums holds the allowed values the public API's schema declares, keyed by\n")
	b.WriteString("// \"<operation>.<property path>\". Refresh with `make openapi`.\n")
	b.WriteString("var Enums = map[string][]string{\n")
	keys := make([]string, 0, len(enums))
	for k := range enums {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "\t%q: {", k)
		for i, v := range enums[k] {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", v)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")
	return format.Source([]byte(b.String()))
}
