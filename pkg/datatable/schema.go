package datatable

import (
	"reflect"

	"github.com/nsgo-dev/game/pkg/jsonschema"
)

// Schema returns the JSON Schema of a YAML data table of T rows, the format LoadYAML reads and MarshalYAML writes, for an editor or AI tool to validate and complete a table file while it is written.
//
// The schema is a mapping with a required version, which must equal FormatVersion, and rows, a list of T's rows that may be left out or null for a table with no rows; any other top-level key is refused. Each row is an object keyed by the column names LoadYAML matches, from a field's `table` tag, else its `json` tag, else its Go name, spelled as MarshalYAML writes them, with embedded structs that have neither tag merged in, as the loaders do. A row type in which two fields share a column name is an error, as it is for LoadYAML. A time.Duration is a string such as "1.5s" or an integer of nanoseconds.
//
// The schema describes a table's canonical spelling, which is as strict as LoadYAML about unknown columns and stricter about the rest: it refuses other capitalizations of a column name (LoadYAML also matches a column name in another case), and wants each cell as its own type rather than a string LoadYAML would convert, and a list as a YAML sequence rather than a delimited string. LoadYAML also refuses merge keys (<<), which a YAML language server resolves before validating, so the schema cannot flag them.
//
// opts sets the $id, title and descriptions; Schema fills in FieldName and the time.Duration form unless opts sets them, so a game can pass jsonschema.PackageDocs of its own package as opts.Docs to describe each column. The row schema is at #/definitions/<T's name>.
func Schema[T any](opts jsonschema.Options) (*jsonschema.Schema, error) {
	if _, err := buildYAMLPlan(reflect.TypeFor[T]()); err != nil {
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
	rows, err := jsonschema.Reflect(reflect.TypeFor[[]T](), opts)
	if err != nil {
		return nil, err
	}
	table := &jsonschema.Schema{
		Schema:      rows.Schema,
		ID:          rows.ID,
		Title:       rows.Title,
		Description: rows.Description,
		Types:       []jsonschema.Type{jsonschema.TypeObject},
		Properties: []jsonschema.Property{
			{Name: yamlVersionKey, Schema: &jsonschema.Schema{
				Description: "The data table format version.",
				Types:       []jsonschema.Type{jsonschema.TypeInteger},
				Const:       FormatVersion,
			}},
			// LoadYAML reads a missing or null rows key as a table with no rows.
			{Name: yamlRowsKey, Schema: &jsonschema.Schema{
				Description: "The table's rows.",
				Types:       []jsonschema.Type{jsonschema.TypeArray, jsonschema.TypeNull},
				Items:       rows.Items,
			}},
		},
		Required:             []string{yamlVersionKey},
		AdditionalProperties: jsonschema.False(),
		Definitions:          rows.Definitions,
	}
	return table, nil
}

// columnField names a struct field the way collectFields and resolveColumnName match it to a column, spelled as MarshalYAML writes it.
func columnField(f reflect.StructField) jsonschema.Field {
	if f.Anonymous && f.Type.Kind() == reflect.Struct && f.Tag.Get("table") == "" && f.Tag.Get("json") == "" {
		return jsonschema.Field{Inline: true}
	}
	name, skip := resolveColumnName(f)
	return jsonschema.Field{Name: name, Skip: skip}
}
