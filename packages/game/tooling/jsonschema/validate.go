package jsonschema

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// Issue is one place where a value breaks its schema.
type Issue struct {
	// Path is the JSON Pointer (RFC 6901) of the value within the document, "" for the document itself.
	Path string
	// Message says what is wrong with the value.
	Message string
}

// String returns the issue as "<path>: <message>", with "/" standing for the document itself.
func (i Issue) String() string {
	path := i.Path
	if path == "" {
		path = "/"
	}
	return path + ": " + i.Message
}

// ValidationError lists every place a value breaks its schema, in document order.
type ValidationError struct {
	// Issues holds one entry per broken keyword, never empty.
	Issues []Issue
}

// Error returns the issues one per line.
func (e *ValidationError) Error() string {
	lines := make([]string, len(e.Issues))
	for i, issue := range e.Issues {
		lines[i] = issue.String()
	}
	return "jsonschema: " + strings.Join(lines, "\n")
}

// maxYAMLNodes caps how many values ValidateYAML expands aliases into, so a small document of nested aliases cannot make it build an exponentially large value.
const maxYAMLNodes = 1 << 20

// maxDepth caps how deeply Validate follows nested values and $refs, so neither a deeply nested value nor a schema whose $ref points at itself overflows the stack.
const maxDepth = 512

// Validate checks v against s and returns a *ValidationError listing every issue, or nil when v conforms.
//
// v is a value as encoding/json or ValidateYAML decodes one into an interface: nil, bool, string, a number of any Go numeric kind, []any, and map[string]any. $refs resolve against s, so call Validate on the root schema.
func (s *Schema) Validate(v any) error {
	val := validator{root: s}
	val.check(s, v, "", 0)
	if len(val.issues) == 0 {
		return nil
	}
	return &ValidationError{Issues: val.issues}
}

// ValidateYAML decodes the single YAML document in data as a YAML language server reads it (mapping keys as strings, timestamps and binary as strings, merge keys merged, a key repeated within one mapping refused) and validates it with Validate. A document that is not YAML, or that holds more than one document, is an error that is not a *ValidationError.
func (s *Schema) ValidateYAML(data []byte) error {
	v, err := DecodeYAML(data)
	if err != nil {
		return err
	}
	return s.Validate(v)
}

// DecodeYAML decodes the single YAML document in data into the plain values Validate takes; empty input decodes to nil.
func DecodeYAML(data []byte) (any, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("jsonschema: decode YAML: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("jsonschema: decode YAML: more than one document")
		}
		return nil, fmt.Errorf("jsonschema: decode YAML: %w", err)
	}
	c := yamlConverter{}
	return c.convert(&doc, 0)
}

type yamlConverter struct {
	nodes int
}

func (c *yamlConverter) convert(n *yaml.Node, depth int) (any, error) {
	c.nodes++
	if c.nodes > maxYAMLNodes {
		return nil, fmt.Errorf("jsonschema: decode YAML: more than %d values once aliases are expanded", maxYAMLNodes)
	}
	if depth > maxDepth {
		return nil, fmt.Errorf("jsonschema: decode YAML: nested more than %d deep", maxDepth)
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return c.convert(n.Content[0], depth+1)
	case yaml.AliasNode:
		return c.convert(n.Alias, depth+1)
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, item := range n.Content {
			v, err := c.convert(item, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		var merged []map[string]any
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if key.Kind == yaml.ScalarNode && key.Tag == "!!merge" {
				m, err := c.mergeSources(value, depth+1)
				if err != nil {
					return nil, err
				}
				merged = append(merged, m...)
				continue
			}
			k, err := c.convert(key, depth+1)
			if err != nil {
				return nil, err
			}
			name := keyString(k)
			if _, dup := out[name]; dup {
				// yaml.v3 refuses a repeated key when it decodes into a struct or map, as Parse does, and so does a language server.
				return nil, fmt.Errorf("jsonschema: decode YAML: line %d: mapping key %q already defined", key.Line, name)
			}
			v, err := c.convert(value, depth+1)
			if err != nil {
				return nil, err
			}
			out[name] = v
		}
		// Keys written in the mapping win over merged ones, and earlier merge sources over later ones, as YAML's merge key specifies.
		for _, m := range merged {
			for k, v := range m {
				if _, ok := out[k]; !ok {
					out[k] = v
				}
			}
		}
		return out, nil
	case yaml.ScalarNode:
		return scalar(n)
	default:
		return nil, fmt.Errorf("jsonschema: decode YAML: line %d: unexpected node kind %d", n.Line, n.Kind)
	}
}

// mergeSources returns the mappings a merge key's value names: one mapping, or a sequence of them, earliest first.
func (c *yamlConverter) mergeSources(n *yaml.Node, depth int) ([]map[string]any, error) {
	v, err := c.convert(n, depth)
	if err != nil {
		return nil, err
	}
	switch v := v.(type) {
	case map[string]any:
		return []map[string]any{v}, nil
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("jsonschema: decode YAML: line %d: a merge key's sequence holds something other than mappings", n.Line)
			}
			out = append(out, m)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("jsonschema: decode YAML: line %d: a merge key's value is not a mapping", n.Line)
	}
}

func keyString(k any) string {
	switch k := k.(type) {
	case nil:
		return "null"
	case string:
		return k
	case float64:
		return strconv.FormatFloat(k, 'g', -1, 64)
	default:
		return fmt.Sprint(k)
	}
}

func scalar(n *yaml.Node) (any, error) {
	switch n.ShortTag() {
	case "!!null":
		return nil, nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return nil, fmt.Errorf("jsonschema: decode YAML: line %d: %w", n.Line, err)
		}
		return b, nil
	case "!!int":
		var i int64
		if err := n.Decode(&i); err == nil {
			return i, nil
		}
		var u uint64
		if err := n.Decode(&u); err == nil {
			return u, nil
		}
		var f float64
		if err := n.Decode(&f); err != nil {
			return nil, fmt.Errorf("jsonschema: decode YAML: line %d: %w", n.Line, err)
		}
		return f, nil
	case "!!float":
		var f float64
		if err := n.Decode(&f); err != nil {
			return nil, fmt.Errorf("jsonschema: decode YAML: line %d: %w", n.Line, err)
		}
		return f, nil
	default:
		// Strings, and the timestamps and binary a language server also sees as strings.
		return n.Value, nil
	}
}

type validator struct {
	root   *Schema
	issues []Issue
}

func (val *validator) fail(path, format string, args ...any) {
	val.issues = append(val.issues, Issue{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (val *validator) resolve(ref string) (*Schema, bool) {
	if ref == "#" {
		return val.root, true
	}
	name, ok := strings.CutPrefix(ref, "#/definitions/")
	if !ok {
		return nil, false
	}
	def, ok := val.root.Definitions[name]
	return def, ok
}

func (val *validator) check(s *Schema, v any, path string, depth int) {
	if s == nil {
		return
	}
	if depth > maxDepth {
		val.fail(path, "nested more than %d deep", maxDepth)
		return
	}
	if b, ok := s.Boolean(); ok {
		if !b {
			val.fail(path, "no value is allowed here")
		}
		return
	}
	if s.Ref != "" {
		target, ok := val.resolve(s.Ref)
		if !ok {
			val.fail(path, "schema $ref %q does not resolve", s.Ref)
			return
		}
		val.check(target, v, path, depth+1)
		return
	}
	for _, sub := range s.AllOf {
		val.check(sub, v, path, depth+1)
	}
	v = normalize(v)
	if len(s.Types) > 0 && !slices.ContainsFunc(s.Types, func(t Type) bool { return hasType(v, t) }) {
		val.fail(path, "want %s, got %s", joinTypes(s.Types), typeOf(v))
		return
	}
	if s.Const != nil && !equal(v, s.Const) {
		val.fail(path, "%s is not the allowed value %s", describe(v), describe(s.Const))
	}
	if len(s.Enum) > 0 && !slices.ContainsFunc(s.Enum, func(e any) bool { return equal(v, e) }) {
		allowed := make([]string, len(s.Enum))
		for i, e := range s.Enum {
			allowed[i] = describe(e)
		}
		val.fail(path, "%s is not one of %s", describe(v), strings.Join(allowed, ", "))
	}
	switch v := v.(type) {
	case float64:
		val.checkNumber(s, v, path)
	case string:
		if s.MinLength != nil && utf8.RuneCountInString(v) < *s.MinLength {
			val.fail(path, "string is shorter than %d characters", *s.MinLength)
		}
	case []any:
		if s.MinItems != nil && len(v) < *s.MinItems {
			val.fail(path, "array has %d items, want at least %d", len(v), *s.MinItems)
		}
		if s.Items != nil {
			for i, item := range v {
				val.check(s.Items, item, path+"/"+strconv.Itoa(i), depth+1)
			}
		}
	case map[string]any:
		val.checkObject(s, v, path, depth)
	}
}

func (val *validator) checkNumber(s *Schema, n float64, path string) {
	if s.Minimum != nil && n < *s.Minimum {
		val.fail(path, "%s is less than the minimum %s", formatNumber(n), formatNumber(*s.Minimum))
	}
	if s.ExclusiveMinimum != nil && n <= *s.ExclusiveMinimum {
		val.fail(path, "%s is not greater than %s", formatNumber(n), formatNumber(*s.ExclusiveMinimum))
	}
	if s.Maximum != nil && n > *s.Maximum {
		val.fail(path, "%s is greater than the maximum %s", formatNumber(n), formatNumber(*s.Maximum))
	}
	if s.ExclusiveMaximum != nil && n >= *s.ExclusiveMaximum {
		val.fail(path, "%s is not less than %s", formatNumber(n), formatNumber(*s.ExclusiveMaximum))
	}
}

func (val *validator) checkObject(s *Schema, m map[string]any, path string, depth int) {
	for _, name := range s.Required {
		if _, ok := m[name]; !ok {
			val.fail(path, "missing required key %q", name)
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		child := path + "/" + escapePointer(k)
		if ps := s.Property(k); ps != nil {
			val.check(ps, m[k], child, depth+1)
			continue
		}
		if s.AdditionalProperties == nil {
			continue
		}
		if b, ok := s.AdditionalProperties.Boolean(); ok && !b {
			names := make([]string, len(s.Properties))
			for i, p := range s.Properties {
				names[i] = p.Name
			}
			if len(names) == 0 {
				val.fail(child, "unknown key %q: this object takes no keys", k)
			} else {
				val.fail(child, "unknown key %q (known keys: %s)", k, strings.Join(names, ", "))
			}
			continue
		}
		val.check(s.AdditionalProperties, m[k], child, depth+1)
	}
}

func escapePointer(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

// normalize turns every Go number into float64 (the keywords Validate checks need no more precision than that) and other maps, slices, strings and booleans into the map[string]any, []any, string and bool forms.
func normalize(v any) any {
	if f, ok := number(v); ok {
		return f
	}
	switch v.(type) {
	case nil, bool, string, []any, map[string]any:
		return v
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = rv.Index(i).Interface()
		}
		return out
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String && rv.Type().Key().Kind() != reflect.Interface {
			return v
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[keyString(iter.Key().Interface())] = iter.Value().Interface()
		}
		return out
	case reflect.String:
		return rv.String()
	case reflect.Bool:
		return rv.Bool()
	}
	return v
}

// number reports v as a float64 when it is any Go number.
func number(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

func hasType(v any, t Type) bool {
	switch t {
	case TypeNull:
		return v == nil
	case TypeBoolean:
		_, ok := v.(bool)
		return ok
	case TypeString:
		_, ok := v.(string)
		return ok
	case TypeNumber:
		f, ok := v.(float64)
		return ok && !math.IsNaN(f) && !math.IsInf(f, 0)
	case TypeInteger:
		f, ok := v.(float64)
		return ok && !math.IsInf(f, 0) && f == math.Trunc(f)
	case TypeArray:
		_, ok := v.([]any)
		return ok
	case TypeObject:
		_, ok := v.(map[string]any)
		return ok
	}
	return false
}

// typeOf names a normalized value's JSON type, with the value itself for a scalar.
func typeOf(v any) string {
	switch v := v.(type) {
	case nil:
		return "null"
	case bool:
		return "the boolean " + strconv.FormatBool(v)
	case string:
		return "the string " + strconv.Quote(v)
	case float64:
		switch {
		case math.IsNaN(v) || math.IsInf(v, 0):
			return formatNumber(v) + ", which is not a finite number"
		case v == math.Trunc(v):
			return "the integer " + formatNumber(v)
		default:
			return "the number " + formatNumber(v)
		}
	case []any:
		return "an array"
	case map[string]any:
		return "an object"
	}
	return fmt.Sprintf("a %T", v)
}

func joinTypes(types []Type) string {
	parts := make([]string, len(types))
	for i, t := range types {
		parts[i] = string(t)
	}
	return strings.Join(parts, " or ")
}

func describe(v any) string {
	if f, ok := number(v); ok {
		return formatNumber(f)
	}
	switch v := v.(type) {
	case nil:
		return "null"
	case string:
		return strconv.Quote(v)
	case bool:
		return strconv.FormatBool(v)
	case []any:
		return "the array"
	case map[string]any:
		return "the object"
	}
	return "the value"
}

// formatNumber writes an integral number below 1e21 in full, as encoding/json does, so a bound such as 4294967295 reads as written rather than as 4.294967295e+09.
func formatNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// equal compares a value with a const or enum entry, treating numbers of different Go types as equal when their values are.
func equal(v, want any) bool {
	v, want = normalize(v), normalize(want)
	switch w := want.(type) {
	case []any:
		got, ok := v.([]any)
		if !ok || len(got) != len(w) {
			return false
		}
		for i := range w {
			if !equal(got[i], w[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		got, ok := v.(map[string]any)
		if !ok || len(got) != len(w) {
			return false
		}
		for k, wv := range w {
			gv, ok := got[k]
			if !ok || !equal(gv, wv) {
				return false
			}
		}
		return true
	}
	return v == want
}
