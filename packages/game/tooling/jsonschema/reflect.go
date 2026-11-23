package jsonschema

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Field says how a loader reads one Go struct field: the key it is written under, whether it is skipped, and whether its fields are read as if they were the parent's.
type Field struct {
	// Name is the key the field is written under.
	Name string
	// Skip leaves the field out of the schema, as a `yaml:"-"` tag does.
	Skip bool
	// Inline merges the fields of a struct-typed field into its parent, as a `yaml:",inline"` tag does.
	Inline bool
}

// Options configures Reflect.
type Options struct {
	// ID is the $id URI the schema is published at; empty omits it.
	ID string
	// Title is the root schema's title; empty omits it.
	Title string
	// Description is the root schema's description; empty uses Docs for the root type.
	Description string
	// FieldName maps a struct field to the key its loader reads; nil uses YAMLField.
	FieldName func(reflect.StructField) Field
	// Docs maps a Go type's name ("SceneDoc") or a type's field ("SceneDoc.Name") to the description written for it, as PackageDocs reads them from a package's doc comments.
	Docs map[string]string
	// Types overrides the schema of a Go type wherever it appears, for types a loader reads in a form their kind does not say, such as a time.Duration written "1.5s". Each use gets its own copy.
	Types map[reflect.Type]*Schema
	// AllowUnknown leaves additionalProperties unset on struct schemas, for loaders that ignore keys they do not know; by default unknown keys are refused, as a decoder with KnownFields(true) does.
	AllowUnknown bool
}

// YAMLField maps a struct field the way go.yaml.in/yaml/v3 does: the first part of its `yaml` tag names it, "-" skips it, ",inline" merges it, and an untagged field is named by its Go name in lower case.
func YAMLField(f reflect.StructField) Field {
	tag, ok := f.Tag.Lookup("yaml")
	if !ok {
		return Field{Name: strings.ToLower(f.Name)}
	}
	if tag == "-" {
		return Field{Skip: true}
	}
	name, opts, _ := strings.Cut(tag, ",")
	inline := false
	for opt := range strings.SplitSeq(opts, ",") {
		if opt == "inline" {
			inline = true
		}
	}
	if name == "" {
		name = strings.ToLower(f.Name)
	}
	return Field{Name: name, Inline: inline}
}

// Reflect builds the schema of values of type t as a loader with opts reads them:
//
//   - a struct is an object whose properties are its exported fields, named by opts.FieldName, with unknown keys refused unless opts.AllowUnknown; a struct other than the root is written once under definitions and referenced by $ref;
//   - a pointer is the schema of what it points to;
//   - a slice or array is an array of its elements' schema;
//   - a map with string keys is an object whose values all have the map's element schema;
//   - strings, booleans, integers (unsigned ones with minimum 0) and floats map to their JSON types, and an interface accepts any value.
//
// A field's `schema` struct tag adds what its Go type cannot say, as comma-separated entries: required, const=V, enum=A|B|C, minimum=N, maximum=N, exclusiveMinimum=N, exclusiveMaximum=N, minLength=N and minItems=N, where const and enum values are read as the field's type. Channels, functions, complex numbers and maps with keys other than strings are refused.
func Reflect(t reflect.Type, opts Options) (*Schema, error) {
	if t == nil {
		return nil, fmt.Errorf("jsonschema: nil type")
	}
	if opts.FieldName == nil {
		opts.FieldName = YAMLField
	}
	r := &reflector{opts: opts, root: deref(t), defs: map[string]*Schema{}, names: map[string]reflect.Type{}}
	var root *Schema
	var err error
	if r.root.Kind() == reflect.Struct && r.override(r.root) == nil {
		root, err = r.structSchema(r.root)
	} else {
		root, err = r.schema(t)
	}
	if err != nil {
		return nil, err
	}
	root.Schema = Draft07
	root.ID = opts.ID
	root.Title = opts.Title
	if opts.Description != "" {
		root.Description = opts.Description
	}
	if len(r.defs) > 0 {
		root.Definitions = r.defs
	}
	return root, nil
}

type reflector struct {
	opts  Options
	root  reflect.Type
	defs  map[string]*Schema
	names map[string]reflect.Type
}

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

var textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()

func (r *reflector) override(t reflect.Type) *Schema {
	s, ok := r.opts.Types[t]
	if !ok {
		return nil
	}
	return s.clone()
}

func (r *reflector) schema(t reflect.Type) (*Schema, error) {
	if s := r.override(t); s != nil {
		return s, nil
	}
	switch t.Kind() {
	case reflect.Pointer:
		return r.schema(t.Elem())
	case reflect.Bool:
		return &Schema{Types: []Type{TypeBoolean}}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return &Schema{Types: []Type{TypeInteger}}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		zero := 0.0
		return &Schema{Types: []Type{TypeInteger}, Minimum: &zero}, nil
	case reflect.Float32, reflect.Float64:
		return &Schema{Types: []Type{TypeNumber}}, nil
	case reflect.String:
		return &Schema{Types: []Type{TypeString}}, nil
	case reflect.Interface:
		return &Schema{}, nil
	case reflect.Slice, reflect.Array:
		items, err := r.schema(t.Elem())
		if err != nil {
			return nil, err
		}
		return &Schema{Types: []Type{TypeArray}, Items: items}, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String && !t.Key().Implements(textMarshalerType) {
			return nil, fmt.Errorf("jsonschema: map %v has keys that are not strings", t)
		}
		values, err := r.schema(t.Elem())
		if err != nil {
			return nil, err
		}
		return &Schema{Types: []Type{TypeObject}, AdditionalProperties: values}, nil
	case reflect.Struct:
		return r.structRef(t)
	default:
		return nil, fmt.Errorf("jsonschema: type %v (%v) has no JSON form", t, t.Kind())
	}
}

// structRef returns a $ref to t's definition, building the definition the first time t is seen.
func (r *reflector) structRef(t reflect.Type) (*Schema, error) {
	if t == r.root {
		return &Schema{Ref: "#"}, nil
	}
	name := t.Name()
	if name == "" {
		return r.structSchema(t)
	}
	if prev, ok := r.names[name]; ok {
		if prev != t {
			return nil, fmt.Errorf("jsonschema: types %v and %v share the definition name %q", prev, t, name)
		}
		return &Schema{Ref: "#/definitions/" + name}, nil
	}
	r.names[name] = t
	// Reserve the name before building, so a type that refers to itself gets a $ref instead of recursing.
	r.defs[name] = nil
	s, err := r.structSchema(t)
	if err != nil {
		return nil, err
	}
	r.defs[name] = s
	return &Schema{Ref: "#/definitions/" + name}, nil
}

func (r *reflector) structSchema(t reflect.Type) (*Schema, error) {
	s := &Schema{Types: []Type{TypeObject}}
	if t.Name() != "" {
		s.Description = r.opts.Docs[t.Name()]
	}
	if err := r.addFields(s, t, map[string]bool{}); err != nil {
		return nil, err
	}
	if !r.opts.AllowUnknown {
		s.AdditionalProperties = False()
	}
	return s, nil
}

func (r *reflector) addFields(s *Schema, t reflect.Type, seen map[string]bool) error {
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() && !(f.Anonymous && deref(f.Type).Kind() == reflect.Struct) {
			continue
		}
		info := r.opts.FieldName(f)
		if info.Skip {
			continue
		}
		if info.Inline {
			inner := deref(f.Type)
			if inner.Kind() != reflect.Struct {
				return fmt.Errorf("jsonschema: %v.%s is inline but is not a struct", t, f.Name)
			}
			if err := r.addFields(s, inner, seen); err != nil {
				return err
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		if seen[info.Name] {
			return fmt.Errorf("jsonschema: %v has two fields named %q", t, info.Name)
		}
		seen[info.Name] = true
		fs, err := r.schema(f.Type)
		if err != nil {
			return fmt.Errorf("jsonschema: %v.%s: %w", t, f.Name, err)
		}
		required, err := applyTag(fs, f)
		if err != nil {
			return fmt.Errorf("jsonschema: %v.%s: %w", t, f.Name, err)
		}
		if doc := r.opts.Docs[t.Name()+"."+f.Name]; doc != "" && t.Name() != "" {
			if fs.Ref != "" {
				// draft-07 ignores keywords beside $ref, so the field's description wraps the reference.
				fs = &Schema{Description: doc, AllOf: []*Schema{fs}}
			} else {
				fs.Description = doc
			}
		}
		s.Properties = append(s.Properties, Property{Name: info.Name, Schema: fs})
		if required {
			s.Required = append(s.Required, info.Name)
		}
	}
	return nil
}

// applyTag adds the constraints of f's `schema` tag to s and reports whether the tag marks f required.
func applyTag(s *Schema, f reflect.StructField) (required bool, err error) {
	tag, ok := f.Tag.Lookup("schema")
	if !ok || tag == "" {
		return false, nil
	}
	base := deref(f.Type)
	for entry := range strings.SplitSeq(tag, ",") {
		key, value, hasValue := strings.Cut(strings.TrimSpace(entry), "=")
		switch key {
		case "required":
			if hasValue {
				return false, fmt.Errorf("schema tag %q takes no value", key)
			}
			required = true
		case "const":
			v, err := parseTagValue(base, value)
			if err != nil {
				return false, fmt.Errorf("schema tag const: %w", err)
			}
			s.Const = v
		case "enum":
			for item := range strings.SplitSeq(value, "|") {
				v, err := parseTagValue(base, item)
				if err != nil {
					return false, fmt.Errorf("schema tag enum: %w", err)
				}
				s.Enum = append(s.Enum, v)
			}
		case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum":
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return false, fmt.Errorf("schema tag %s: %w", key, err)
			}
			switch key {
			case "minimum":
				s.Minimum = &n
			case "maximum":
				s.Maximum = &n
			case "exclusiveMinimum":
				s.ExclusiveMinimum = &n
			default:
				s.ExclusiveMaximum = &n
			}
		case "minLength", "minItems":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return false, fmt.Errorf("schema tag %s=%q is not a non-negative integer", key, value)
			}
			if key == "minLength" {
				s.MinLength = &n
			} else {
				s.MinItems = &n
			}
		default:
			return false, fmt.Errorf("unknown schema tag entry %q", entry)
		}
	}
	return required, nil
}

// parseTagValue reads a const or enum value from a schema tag as a value of type t.
func parseTagValue(t reflect.Type, raw string) (any, error) {
	switch t.Kind() {
	case reflect.String:
		return raw, nil
	case reflect.Bool:
		return strconv.ParseBool(raw)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.ParseInt(raw, 10, 64)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.ParseUint(raw, 10, 64)
	case reflect.Float32, reflect.Float64:
		return strconv.ParseFloat(raw, 64)
	default:
		return nil, fmt.Errorf("type %v takes no const or enum value", t)
	}
}

// clone returns a deep copy of s, so an override in Options.Types can be given a description per use without changing the others.
func (s *Schema) clone() *Schema {
	if s == nil {
		return nil
	}
	c := *s
	c.Types = append([]Type(nil), s.Types...)
	c.Enum = append([]any(nil), s.Enum...)
	c.Items = s.Items.clone()
	c.AdditionalProperties = s.AdditionalProperties.clone()
	c.Required = append([]string(nil), s.Required...)
	if s.Properties != nil {
		c.Properties = make([]Property, len(s.Properties))
		for i, p := range s.Properties {
			c.Properties[i] = Property{Name: p.Name, Schema: p.Schema.clone()}
		}
	}
	if s.AllOf != nil {
		c.AllOf = make([]*Schema, len(s.AllOf))
		for i, sub := range s.AllOf {
			c.AllOf[i] = sub.clone()
		}
	}
	if s.Definitions != nil {
		c.Definitions = make(map[string]*Schema, len(s.Definitions))
		for k, v := range s.Definitions {
			c.Definitions[k] = v.clone()
		}
	}
	return &c
}
