package datatable

import (
	"reflect"
	"strings"

	"github.com/nsgo-dev/game/packages/game/tooling/jsonschema"
)

// Schema returns the JSON Schema of a YAML or JSON table of T rows, for an editor or AI tool to validate and complete a table file while it is written.
//
// The schema is an array whose items are T's rows. Each row is an object keyed by the column names LoadYAML and LoadJSON match, from a field's `table` tag, else its `json` tag, else its Go name in lower case, with embedded structs that have neither tag merged in, as the loaders do. A time.Duration is a string such as "1.5s" or an integer of nanoseconds.
//
// The schema describes a table's canonical spelling, which is stricter than the loaders: it refuses keys that match no column (the loaders ignore them, so a misspelt column would silently load as zero) and other capitalizations of a column name (the loaders match an untagged field's Go name in any case), and wants each cell as its own type rather than a string the loaders would convert, and a list as a YAML sequence rather than a delimited string.
//
// opts sets the $id, title and descriptions; Schema fills in FieldName and the time.Duration form unless opts sets them, so a game can pass jsonschema.PackageDocs of its own package as opts.Docs to describe each column.
func Schema[T any](opts jsonschema.Options) (*jsonschema.Schema, error) {
	if _, err := buildStructPlan(reflect.TypeFor[T]()); err != nil {
		return nil, err
	}
	if opts.FieldName == nil {
		opts.FieldName = columnField
	}
	if _, ok := opts.Types[durationType]; !ok {
		types := make(map[reflect.Type]*jsonschema.Schema, len(opts.Types)+1)
		for t, s := range opts.Types {
			types[t] = s
		}
		types[durationType] = &jsonschema.Schema{Types: []jsonschema.Type{jsonschema.TypeString, jsonschema.TypeInteger}}
		opts.Types = types
	}
	return jsonschema.Reflect(reflect.TypeFor[[]T](), opts)
}

// columnField names a struct field the way collectFields and resolveColumnName match it to a column.
func columnField(f reflect.StructField) jsonschema.Field {
	if f.Anonymous && f.Type.Kind() == reflect.Struct && f.Tag.Get("table") == "" && f.Tag.Get("json") == "" {
		return jsonschema.Field{Inline: true}
	}
	name, skip := resolveColumnName(f)
	if !skip && !tagNamed(f, "table") && !tagNamed(f, "json") {
		// The loaders match a field's Go name in any case; the schema spells it in lower case, as YAML keys usually are.
		name = strings.ToLower(name)
	}
	return jsonschema.Field{Name: name, Skip: skip}
}

// tagNamed reports whether f's tag key names a column.
func tagNamed(f reflect.StructField, key string) bool {
	raw, ok := f.Tag.Lookup(key)
	if !ok {
		return false
	}
	name := strings.TrimSpace(strings.Split(raw, ",")[0])
	return name != "" && name != "-"
}
