package datatable

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"strconv"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	yamlVersionKey = "version"
	yamlRowsKey    = "rows"
	yamlMergeTag   = "!!merge"
)

// errYAMLMergeKey refuses a merge key (<<) by name, rather than as an unknown field, so a designer sharing row defaults through an anchor learns that every row must spell out its own columns.
var errYAMLMergeKey = errors.New("merge keys (<<) are not supported; write each column in the row")

// buildYAMLPlan is buildStructPlan for the YAML paths, which also refuse a row type in which two fields share a column name, since only the first of them could be saved or loaded.
func buildYAMLPlan(t reflect.Type) (*structPlan, error) {
	plan, err := buildStructPlan(t)
	if err != nil {
		return nil, err
	}
	if plan.shadowed != "" {
		return nil, fmt.Errorf("datatable: %v has more than one field for column %q; give each field its own table tag or tag one of them \"-\"", t, plan.shadowed)
	}
	return plan, nil
}

// LoadYAML parses a versioned YAML data table into []T.
//
// The document is a mapping with two keys: version, which must equal FormatVersion, and rows, a list of mappings whose keys name fields of T by their table tag, json tag or Go field name (matched exactly first, then case-insensitively). A missing or empty rows key is an empty table. Any other top-level key, a missing or unsupported version, a row key that names no field (ErrUnknownField) and a field set twice in one row (ErrDuplicateField) are refused, as are a merge key (<<), a second YAML document in the same file, and a row type in which two fields share a column name; errors inside a row are a *CellError naming the 1-based row and the field.
func LoadYAML[T any](filename string, data []byte) ([]T, error) {
	var zero T
	plan, err := buildYAMLPlan(reflect.TypeOf(zero))
	if err != nil {
		return nil, err
	}

	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: parse YAML: %w", displayFile(filename), err)
		}
	} else {
		// Like LoadJSON's trailing-data check: a second document would otherwise be ignored, along with any unknown keys or version in it.
		var extra yaml.Node
		if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
			if err != nil {
				return nil, fmt.Errorf("%s: parse YAML: %w", displayFile(filename), err)
			}
			return nil, fmt.Errorf("%s: line %d: unexpected second YAML document; a data table is one document", displayFile(filename), extra.Line)
		}
	}
	var root *yaml.Node
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = resolveYAMLAlias(doc.Content[0], 0)
	}
	if root == nil || root.Tag == "!!null" {
		return nil, fmt.Errorf("%s: empty YAML data table: want a mapping with %s: %d and %s", displayFile(filename), yamlVersionKey, FormatVersion, yamlRowsKey)
	}
	if root.Kind == yaml.SequenceNode {
		return nil, fmt.Errorf("%s: line %d: a YAML data table is a mapping with %s: %d and %s, not a bare list of rows", displayFile(filename), root.Line, yamlVersionKey, FormatVersion, yamlRowsKey)
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: line %d: a YAML data table must be a mapping with %s and %s", displayFile(filename), root.Line, yamlVersionKey, yamlRowsKey)
	}

	var versionNode, rowsNode *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		kNode := resolveYAMLAlias(root.Content[i], 0)
		vNode := resolveYAMLAlias(root.Content[i+1], 0)
		if kNode == nil || kNode.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("%s: line %d: top-level keys must be scalars", displayFile(filename), root.Content[i].Line)
		}
		if kNode.Tag == yamlMergeTag {
			return nil, fmt.Errorf("%s: line %d: %v", displayFile(filename), kNode.Line, errYAMLMergeKey)
		}
		switch kNode.Value {
		case yamlVersionKey:
			if versionNode != nil {
				return nil, fmt.Errorf("%s: line %d: duplicate key %q", displayFile(filename), kNode.Line, kNode.Value)
			}
			versionNode = vNode
		case yamlRowsKey:
			if rowsNode != nil {
				return nil, fmt.Errorf("%s: line %d: duplicate key %q", displayFile(filename), kNode.Line, kNode.Value)
			}
			rowsNode = vNode
		default:
			return nil, fmt.Errorf("%s: line %d: unknown top-level key %q (want %s and %s)", displayFile(filename), kNode.Line, kNode.Value, yamlVersionKey, yamlRowsKey)
		}
	}
	if versionNode == nil {
		return nil, fmt.Errorf("%s: missing %s (want %d)", displayFile(filename), yamlVersionKey, FormatVersion)
	}
	if versionNode.Kind != yaml.ScalarNode || versionNode.Tag != "!!int" {
		return nil, fmt.Errorf("%s: line %d: %s must be an integer (want %d)", displayFile(filename), versionNode.Line, yamlVersionKey, FormatVersion)
	}
	if v, err := strconv.Atoi(versionNode.Value); err != nil || v != FormatVersion {
		return nil, fmt.Errorf("%s: line %d: unsupported %s %s (want %d)", displayFile(filename), versionNode.Line, yamlVersionKey, versionNode.Value, FormatVersion)
	}

	if rowsNode == nil || rowsNode.Tag == "!!null" {
		return nil, nil
	}
	if rowsNode.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%s: line %d: %s must be a list of mappings", displayFile(filename), rowsNode.Line, yamlRowsKey)
	}

	rows := make([]T, 0, len(rowsNode.Content))
	seen := make(map[int]string, len(plan.fields))
	for i, item := range rowsNode.Content {
		rowNum := i + 1
		node := resolveYAMLAlias(item, 0)
		if node == nil || node.Kind != yaml.MappingNode {
			return nil, &CellError{
				File:  filename,
				Row:   rowNum,
				Field: "<row>",
				Value: formatYAMLNode(node),
				Err:   errors.New("row must be a YAML mapping"),
			}
		}

		clear(seen)
		var elem T
		rv := reflect.ValueOf(&elem).Elem()
		for j := 0; j+1 < len(node.Content); j += 2 {
			kNode := resolveYAMLAlias(node.Content[j], 0)
			vNode := resolveYAMLAlias(node.Content[j+1], 0)
			if kNode == nil || kNode.Kind != yaml.ScalarNode {
				return nil, &CellError{
					File:  filename,
					Row:   rowNum,
					Field: "<key>",
					Value: formatYAMLNode(kNode),
					Err:   errors.New("row keys must be scalars"),
				}
			}
			if kNode.Tag == yamlMergeTag {
				return nil, &CellError{
					File:  filename,
					Row:   rowNum,
					Field: kNode.Value,
					Value: formatYAMLNode(vNode),
					Err:   errYAMLMergeKey,
				}
			}
			fieldName := kNode.Value
			fb, ok := plan.lookup(fieldName)
			if !ok {
				return nil, &CellError{
					File:  filename,
					Row:   rowNum,
					Field: fieldName,
					Value: formatYAMLNode(vNode),
					Err:   ErrUnknownField,
				}
			}
			if prev, dup := seen[fb.pos]; dup {
				return nil, &CellError{
					File:  filename,
					Row:   rowNum,
					Field: fieldName,
					Value: formatYAMLNode(vNode),
					Err:   fmt.Errorf("%w: also set as %q", ErrDuplicateField, prev),
				}
			}
			seen[fb.pos] = fieldName
			fv := rv.FieldByIndex(fb.index)
			if err := assignFromYAMLNode(fv, vNode, 0); err != nil {
				return nil, &CellError{
					File:  filename,
					Row:   rowNum,
					Field: fieldName,
					Value: formatYAMLNode(vNode),
					Err:   err,
				}
			}
		}
		rows = append(rows, elem)
	}
	return rows, nil
}

// MarshalYAML encodes rows as a versioned YAML data table that LoadYAML reads back.
//
// The output is deterministic: version first, then rows, each row writing every field of T in struct declaration order under its table tag, json tag or Go field name, with two-space indentation. Lists are written inline ([a, b]), a nil pointer as null, and a time.Duration as its String form (such as 1.5s). A nil list and an empty list are both written as [], which loads back as an empty list. Non-finite floats, field types LoadYAML cannot read (maps, structs, interfaces) and a row type in which two fields share a column name are an error.
func MarshalYAML[T any](rows []T) ([]byte, error) {
	var zero T
	plan, err := buildYAMLPlan(reflect.TypeOf(zero))
	if err != nil {
		return nil, err
	}

	rowsNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	if len(rows) == 0 {
		rowsNode.Style = yaml.FlowStyle
	}
	for i := range rows {
		rv := reflect.ValueOf(&rows[i]).Elem()
		rowNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if len(plan.fields) == 0 {
			rowNode.Style = yaml.FlowStyle
		}
		for _, fb := range plan.fields {
			vNode, err := yamlNodeFromValue(rv.FieldByIndex(fb.index), 0)
			if err != nil {
				return nil, &CellError{
					Row:   i + 1,
					Field: fb.name,
					Value: fmt.Sprint(rv.FieldByIndex(fb.index).Interface()),
					Err:   err,
				}
			}
			rowNode.Content = append(rowNode.Content, yamlString(fb.name), vNode)
		}
		rowsNode.Content = append(rowsNode.Content, rowNode)
	}

	root := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			yamlString(yamlVersionKey), {Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(FormatVersion)},
			yamlString(yamlRowsKey), rowsNode,
		},
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("datatable: encode YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("datatable: encode YAML: %w", err)
	}
	return buf.Bytes(), nil
}

// SaveYAML writes rows to path as a versioned YAML data table in the form MarshalYAML produces.
func SaveYAML[T any](path string, rows []T) error {
	data, err := MarshalYAML(rows)
	if err != nil {
		return fmt.Errorf("datatable: %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("datatable: write %q: %w", path, err)
	}
	return nil
}

func yamlString(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func yamlNodeFromValue(fv reflect.Value, depth int) (*yaml.Node, error) {
	if depth > maxYAMLDepth {
		return nil, errors.New("YAML nesting limit exceeded")
	}
	switch fv.Kind() {
	case reflect.Pointer:
		if fv.IsNil() {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
		}
		return yamlNodeFromValue(fv.Elem(), depth+1)
	case reflect.Slice:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
		for i := range fv.Len() {
			child, err := yamlNodeFromValue(fv.Index(i), depth+1)
			if err != nil {
				return nil, fmt.Errorf("element %d: %w", i, err)
			}
			seq.Content = append(seq.Content, child)
		}
		return seq, nil
	case reflect.String:
		return yamlString(fv.String()), nil
	case reflect.Bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(fv.Bool())}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Type() == durationType {
			return yamlString(time.Duration(fv.Int()).String()), nil
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(fv.Int(), 10)}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(fv.Uint(), 10)}, nil
	case reflect.Float32, reflect.Float64:
		f := fv.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("non-finite %s value %v", fv.Type(), f)
		}
		// Untagged so an integral value such as 3 is written plain rather than as "!!float 3"; LoadYAML parses it by the field's type.
		return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.FormatFloat(f, 'g', -1, fv.Type().Bits())}, nil
	default:
		return nil, fmt.Errorf("unsupported field type %s", fv.Type())
	}
}
