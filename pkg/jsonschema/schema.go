// Package jsonschema builds JSON Schemas (draft-07) from Go types and checks decoded YAML or JSON values against them, so editors and AI tools can complete and validate the YAML files a game's author writes.
//
// Reflect walks a Go type the way its loader decodes it: struct fields become properties named by their yaml tags (or by Options.FieldName), structs become definitions shared through $ref, and a `schema` struct tag adds the constraints a type alone cannot say, such as `schema:"required,const=1"` or `schema:"enum=world|screen"`. Schema.Marshal writes deterministic, indented JSON, so a generated schema can be committed and checked for drift in CI, and Schema.Validate checks a value decoded from YAML or JSON against the keywords Reflect emits.
package jsonschema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Draft07 is the $schema URI of the JSON Schema dialect this package writes, the newest one editors' YAML and JSON language servers support everywhere.
const Draft07 = "http://json-schema.org/draft-07/schema#"

// Type is a JSON Schema primitive type name.
type Type string

const (
	// TypeObject is a mapping with string keys.
	TypeObject Type = "object"
	// TypeArray is a sequence.
	TypeArray Type = "array"
	// TypeString is a string.
	TypeString Type = "string"
	// TypeNumber is any number, integral or not.
	TypeNumber Type = "number"
	// TypeInteger is a number with no fractional part.
	TypeInteger Type = "integer"
	// TypeBoolean is true or false.
	TypeBoolean Type = "boolean"
	// TypeNull is null.
	TypeNull Type = "null"
)

// Property is one named entry of an object schema's properties, kept in declaration order so a generated schema reads in the same order as the Go type.
type Property struct {
	// Name is the key the property is written under.
	Name string
	// Schema constrains the property's value.
	Schema *Schema
}

// Schema is a JSON Schema draft-07 document or subschema, limited to the keywords Reflect emits and Validate checks.
//
// The zero Schema accepts every value; True and False return the boolean schemas.
type Schema struct {
	// Schema is the $schema dialect URI, set on a root schema only.
	Schema string
	// ID is the $id URI a root schema is published at.
	ID string
	// Ref is a $ref to "#" or "#/definitions/<name>" in the root schema. Validate ignores the other keywords of a schema with a Ref, as draft-07 does.
	Ref string
	// Title is a short human-readable name.
	Title string
	// Description is shown by editors on hover and completion.
	Description string
	// Types lists the allowed JSON types; empty allows every type.
	Types []Type
	// Const, when non-nil, is the only allowed value.
	Const any
	// Enum, when non-empty, lists the allowed values.
	Enum []any
	// Minimum and Maximum bound a number inclusively.
	Minimum, Maximum *float64
	// ExclusiveMinimum and ExclusiveMaximum bound a number exclusively.
	ExclusiveMinimum, ExclusiveMaximum *float64
	// MinLength is the fewest characters (Unicode code points) a string may have.
	MinLength *int
	// MinItems is the fewest elements an array may have.
	MinItems *int
	// MaxItems is the most elements an array may have.
	MaxItems *int
	// Items constrains every element of an array.
	Items *Schema
	// Properties constrains an object's named properties, in declaration order.
	Properties []Property
	// Required lists the properties an object must have.
	Required []string
	// AdditionalProperties constrains the properties not named in Properties; False refuses them and nil allows any.
	AdditionalProperties *Schema
	// AllOf lists schemas a value must match all of; Reflect uses it to give a $ref a description of its own.
	AllOf []*Schema
	// Definitions holds the named subschemas that Ref points at, set on a root schema only.
	Definitions map[string]*Schema

	boolean *bool
}

// True returns the boolean schema that accepts every value.
func True() *Schema {
	v := true
	return &Schema{boolean: &v}
}

// False returns the boolean schema that accepts no value; as AdditionalProperties it refuses unknown keys.
func False() *Schema {
	v := false
	return &Schema{boolean: &v}
}

// Boolean reports whether s is a boolean schema (True or False) and, if so, its value.
func (s *Schema) Boolean() (value, ok bool) {
	if s == nil || s.boolean == nil {
		return false, false
	}
	return *s.boolean, true
}

// Property returns the schema of the named property, or nil when s has none by that name.
func (s *Schema) Property(name string) *Schema {
	if s == nil {
		return nil
	}
	for _, p := range s.Properties {
		if p.Name == name {
			return p.Schema
		}
	}
	return nil
}

// Marshal returns s as JSON indented by two spaces with a trailing newline. Keys keep a fixed order (properties in declaration order, definitions sorted by name), so the same schema always yields the same bytes.
func (s *Schema) Marshal() ([]byte, error) {
	raw, err := s.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return nil, fmt.Errorf("jsonschema: indent: %w", err)
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// MarshalJSON writes s as compact JSON with the key order Marshal documents.
func (s *Schema) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := s.write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Schema) write(buf *bytes.Buffer) error {
	if s == nil {
		buf.WriteString("true")
		return nil
	}
	if s.boolean != nil {
		if *s.boolean {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	}
	w := objectWriter{buf: buf}
	buf.WriteByte('{')
	w.str("$schema", s.Schema)
	w.str("$id", s.ID)
	w.str("$ref", s.Ref)
	w.str("title", s.Title)
	w.str("description", s.Description)
	switch len(s.Types) {
	case 0:
	case 1:
		w.value("type", s.Types[0])
	default:
		w.value("type", s.Types)
	}
	if s.Const != nil {
		w.value("const", s.Const)
	}
	if len(s.Enum) > 0 {
		w.value("enum", s.Enum)
	}
	w.number("minimum", s.Minimum)
	w.number("exclusiveMinimum", s.ExclusiveMinimum)
	w.number("maximum", s.Maximum)
	w.number("exclusiveMaximum", s.ExclusiveMaximum)
	if s.MinLength != nil {
		w.value("minLength", *s.MinLength)
	}
	if s.MinItems != nil {
		w.value("minItems", *s.MinItems)
	}
	if s.MaxItems != nil {
		w.value("maxItems", *s.MaxItems)
	}
	if s.Items != nil {
		w.schema("items", s.Items)
	}
	if len(s.Properties) > 0 {
		w.key("properties")
		buf.WriteByte('{')
		for i, p := range s.Properties {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeJSON(buf, p.Name); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := p.Schema.write(buf); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	}
	if len(s.Required) > 0 {
		w.value("required", s.Required)
	}
	if s.AdditionalProperties != nil {
		w.schema("additionalProperties", s.AdditionalProperties)
	}
	if len(s.AllOf) > 0 {
		w.key("allOf")
		buf.WriteByte('[')
		for i, sub := range s.AllOf {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := sub.write(buf); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	}
	if len(s.Definitions) > 0 {
		names := make([]string, 0, len(s.Definitions))
		for name := range s.Definitions {
			names = append(names, name)
		}
		sort.Strings(names)
		w.key("definitions")
		buf.WriteByte('{')
		for i, name := range names {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeJSON(buf, name); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := s.Definitions[name].write(buf); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	}
	buf.WriteByte('}')
	return w.err
}

// objectWriter writes the members of one JSON object, remembering the first error so the caller checks once.
type objectWriter struct {
	buf   *bytes.Buffer
	count int
	err   error
}

func (w *objectWriter) key(name string) {
	if w.count > 0 {
		w.buf.WriteByte(',')
	}
	w.count++
	if err := writeJSON(w.buf, name); err != nil && w.err == nil {
		w.err = err
	}
	w.buf.WriteByte(':')
}

func (w *objectWriter) str(name, value string) {
	if value != "" {
		w.value(name, value)
	}
}

func (w *objectWriter) number(name string, value *float64) {
	if value != nil {
		w.value(name, *value)
	}
}

func (w *objectWriter) value(name string, value any) {
	w.key(name)
	if err := writeJSON(w.buf, value); err != nil && w.err == nil {
		w.err = err
	}
}

func (w *objectWriter) schema(name string, value *Schema) {
	w.key(name)
	if err := value.write(w.buf); err != nil && w.err == nil {
		w.err = err
	}
}

// writeJSON writes v without escaping <, > and &, which descriptions use and which json.Marshal would turn into < sequences.
func writeJSON(buf *bytes.Buffer, v any) error {
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("jsonschema: marshal %T: %w", v, err)
	}
	buf.Write(bytes.TrimSuffix(tmp.Bytes(), []byte{'\n'}))
	return nil
}
