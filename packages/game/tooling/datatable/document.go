package datatable

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/packages/game/loaderr"
)

// ValueKind classifies a cell value in an untyped YAML data table.
type ValueKind int

const (
	KindString ValueKind = iota
	KindNumber
	KindBool
	KindDuration
	KindList
	KindNull
)

// String returns the human-readable name of k.
func (k ValueKind) String() string {
	switch k {
	case KindString:
		return "string"
	case KindNumber:
		return "number"
	case KindBool:
		return "bool"
	case KindDuration:
		return "duration"
	case KindList:
		return "list"
	case KindNull:
		return "null"
	default:
		return fmt.Sprintf("ValueKind(%d)", int(k))
	}
}

// Value holds a single cell's value in an untyped YAML data table.
//
// Scalar values keep their exact text representation in Text (so large int64/uint64 values
// and integer-vs-float distinctions survive round-trips without float64 precision loss);
// KindList values store their elements in List.
type Value struct {
	Kind ValueKind
	Text string
	List []Value
}

// StringValue returns a KindString Value.
func StringValue(s string) Value { return Value{Kind: KindString, Text: s} }

// IntValue returns a KindNumber Value holding a signed integer.
func IntValue(n int64) Value { return Value{Kind: KindNumber, Text: strconv.FormatInt(n, 10)} }

// UintValue returns a KindNumber Value holding an unsigned integer.
func UintValue(n uint64) Value { return Value{Kind: KindNumber, Text: strconv.FormatUint(n, 10)} }

// FloatValue returns a KindNumber Value holding a float64.
func FloatValue(f float64) Value {
	return Value{Kind: KindNumber, Text: strconv.FormatFloat(f, 'g', -1, 64)}
}

// BoolValue returns a KindBool Value.
func BoolValue(b bool) Value { return Value{Kind: KindBool, Text: strconv.FormatBool(b)} }

// DurationValue returns a KindDuration Value formatted with time.Duration.String.
func DurationValue(d time.Duration) Value { return Value{Kind: KindDuration, Text: d.String()} }

// ListValue returns a KindList Value containing a copy of items.
func ListValue(items ...Value) Value {
	list := make([]Value, len(items))
	for i, it := range items {
		list[i] = it.Clone()
	}
	return Value{Kind: KindList, List: list}
}

// NullValue returns a KindNull Value.
func NullValue() Value { return Value{Kind: KindNull} }

// ZeroValue returns the deterministic zero value for kind.
func ZeroValue(kind ValueKind) Value {
	switch kind {
	case KindNumber:
		return IntValue(0)
	case KindBool:
		return BoolValue(false)
	case KindDuration:
		return DurationValue(0)
	case KindList:
		return ListValue()
	case KindNull:
		return NullValue()
	default:
		return StringValue("")
	}
}

// Clone returns a deep copy of v.
func (v Value) Clone() Value {
	if v.Kind != KindList {
		return Value{Kind: v.Kind, Text: v.Text}
	}
	return ListValue(v.List...)
}

// Equal reports whether v and other have the same kind and content.
func (v Value) Equal(other Value) bool {
	if v.Kind != other.Kind {
		return false
	}
	if v.Kind == KindList {
		if len(v.List) != len(other.List) {
			return false
		}
		for i := range v.List {
			if !v.List[i].Equal(other.List[i]) {
				return false
			}
		}
		return true
	}
	return v.Text == other.Text
}

// Int64 parses v as a signed 64-bit integer when v is KindNumber.
func (v Value) Int64() (int64, bool) {
	if v.Kind != KindNumber {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v.Text), 10, 64)
	return n, err == nil
}

// Uint64 parses v as an unsigned 64-bit integer when v is KindNumber.
func (v Value) Uint64() (uint64, bool) {
	if v.Kind != KindNumber {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimSpace(v.Text), 10, 64)
	return n, err == nil
}

// Float64 parses v as a finite float64 when v is KindNumber.
func (v Value) Float64() (float64, bool) {
	if v.Kind != KindNumber {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v.Text), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// Bool returns the boolean value when v is KindBool.
func (v Value) Bool() (bool, bool) {
	if v.Kind != KindBool {
		return false, false
	}
	b, err := parseBoolString(v.Text)
	return b, err == nil
}

// Duration returns the parsed duration when v is KindDuration.
func (v Value) Duration() (time.Duration, bool) {
	if v.Kind != KindDuration {
		return 0, false
	}
	d, err := time.ParseDuration(strings.TrimSpace(v.Text))
	return d, err == nil
}

// Display returns the human-readable grid text for v.
func (v Value) Display() string {
	switch v.Kind {
	case KindNull:
		return ""
	case KindList:
		if len(v.List) == 0 {
			return ""
		}
		// If any element contains a comma, newline, brackets, or quotes, show YAML flow syntax so editing round-trips losslessly.
		needsFlow := false
		for _, item := range v.List {
			if item.Kind == KindList || strings.ContainsAny(item.Text, ",[]\"\n\r") {
				needsFlow = true
				break
			}
		}
		if needsFlow {
			if node, err := yamlNodeFromCellValue(v); err == nil {
				if s, ok := formatInlineYAMLNode(node); ok {
					return s
				}
			}
		}
		parts := make([]string, len(v.List))
		for i, item := range v.List {
			parts[i] = item.Display()
		}
		return strings.Join(parts, ", ")
	default:
		return v.Text
	}
}

// InferValue infers a Value and its ValueKind from raw user text.
func InferValue(raw string) Value {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return StringValue(raw)
	}
	if strings.EqualFold(trimmed, "true") {
		return BoolValue(true)
	}
	if strings.EqualFold(trimmed, "false") {
		return BoolValue(false)
	}
	if isValidNumberText(trimmed) {
		return Value{Kind: KindNumber, Text: trimmed}
	}
	if isDurationString(trimmed) {
		return Value{Kind: KindDuration, Text: trimmed}
	}
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		if v, err := parseYAMLSequenceText(trimmed); err == nil {
			return v
		}
	}
	return StringValue(raw)
}

// ParseValue parses raw user text as a Value of kind.
func ParseValue(kind ValueKind, raw string) (Value, error) {
	return parseValueWithElem(kind, KindNull, raw)
}

func parseValueWithElem(kind, elemKind ValueKind, raw string) (Value, error) {
	switch kind {
	case KindString:
		return StringValue(raw), nil
	case KindBool:
		b, err := parseBoolString(raw)
		if err != nil {
			return Value{}, err
		}
		return BoolValue(b), nil
	case KindNumber:
		s := strings.TrimSpace(raw)
		if !isValidNumberText(s) {
			return Value{}, fmt.Errorf("invalid number %q", raw)
		}
		return Value{Kind: KindNumber, Text: s}, nil
	case KindDuration:
		s := strings.TrimSpace(raw)
		if s == "" {
			return Value{}, errors.New("invalid duration: empty string")
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return DurationValue(time.Duration(n)), nil
		}
		if _, err := time.ParseDuration(s); err != nil {
			return Value{}, fmt.Errorf("invalid duration: %w", err)
		}
		return Value{Kind: KindDuration, Text: s}, nil
	case KindList:
		s := strings.TrimSpace(raw)
		if s == "" || s == "[]" {
			return ListValue(), nil
		}
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			v, err := parseYAMLSequenceText(s)
			if err != nil {
				return Value{}, err
			}
			if elemKind != KindNull {
				coerced := make([]Value, len(v.List))
				for i, item := range v.List {
					if item.Kind == elemKind {
						coerced[i] = item
						continue
					}
					pv, err := parseValueWithElem(elemKind, KindNull, item.Display())
					if err != nil {
						return Value{}, fmt.Errorf("element %d: %w", i, err)
					}
					coerced[i] = pv
				}
				return ListValue(coerced...), nil
			}
			return v, nil
		}
		parts := strings.Split(s, ",")
		items := make([]Value, 0, len(parts))
		for i, p := range parts {
			itemText := strings.TrimSpace(p)
			if elemKind != KindNull {
				iv, err := parseValueWithElem(elemKind, KindNull, itemText)
				if err != nil {
					return Value{}, fmt.Errorf("element %d (%q): %w", i, itemText, err)
				}
				items = append(items, iv)
			} else {
				items = append(items, InferValue(itemText))
			}
		}
		return ListValue(items...), nil
	case KindNull:
		s := strings.TrimSpace(raw)
		if s == "" || strings.EqualFold(s, "null") {
			return NullValue(), nil
		}
		return InferValue(raw), nil
	default:
		return Value{}, fmt.Errorf("unknown value kind %v", kind)
	}
}

func parseBoolString(raw string) (bool, error) {
	s := strings.TrimSpace(raw)
	switch strings.ToLower(s) {
	case "yes", "y", "on":
		return true, nil
	case "no", "n", "off":
		return false, nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false, fmt.Errorf("invalid bool: %w", err)
	}
	return b, nil
}

func isValidNumberText(s string) bool {
	if s == "" {
		return false
	}
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseUint(s, 10, 64); err == nil {
		return true
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return false
	}
	return true
}

func isDurationString(s string) bool {
	if s == "" || strings.TrimSpace(s) != s {
		return false
	}
	// Require at least one unit letter so bare "0" stays KindNumber or KindString rather than KindDuration.
	hasLetter := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}
	if !hasLetter {
		return false
	}
	_, err := time.ParseDuration(s)
	return err == nil
}

func parseYAMLSequenceText(s string) (Value, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		return Value{}, fmt.Errorf("invalid list: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return Value{}, errors.New("invalid list")
	}
	seq := doc.Content[0]
	if seq.Kind != yaml.SequenceNode {
		return Value{}, errors.New("expected YAML list")
	}
	return cellValueFromYAMLNode(seq, 0)
}

func cellValueFromYAMLNode(n *yaml.Node, depth int) (Value, error) {
	if depth > maxYAMLDepth {
		return Value{}, errors.New("YAML nesting limit exceeded")
	}
	n = resolveYAMLAlias(n, depth)
	if n == nil || n.Tag == "!!null" {
		return NullValue(), nil
	}
	switch n.Kind {
	case yaml.SequenceNode:
		items := make([]Value, len(n.Content))
		for i, child := range n.Content {
			v, err := cellValueFromYAMLNode(child, depth+1)
			if err != nil {
				return Value{}, fmt.Errorf("element %d: %w", i, err)
			}
			items[i] = v
		}
		return Value{Kind: KindList, List: items}, nil
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!bool":
			b, err := parseBoolString(n.Value)
			if err != nil {
				return Value{}, err
			}
			return BoolValue(b), nil
		case "!!int":
			return Value{Kind: KindNumber, Text: n.Value}, nil
		case "!!float":
			f, err := strconv.ParseFloat(n.Value, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return Value{}, fmt.Errorf("non-finite float %q", n.Value)
			}
			return Value{Kind: KindNumber, Text: n.Value}, nil
		default:
			if n.Style == 0 && isDurationString(n.Value) {
				return Value{Kind: KindDuration, Text: n.Value}, nil
			}
			return StringValue(n.Value), nil
		}
	default:
		return Value{}, errors.New("cell value cannot be a YAML mapping")
	}
}

func yamlNodeFromCellValue(v Value) (*yaml.Node, error) {
	return yamlNodeFromCellValueDepth(v, 0)
}

func yamlNodeFromCellValueDepth(v Value, depth int) (*yaml.Node, error) {
	if depth > maxYAMLDepth {
		return nil, errors.New("YAML nesting limit exceeded")
	}
	switch v.Kind {
	case KindNull:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case KindString:
		return yamlString(v.Text), nil
	case KindBool:
		b, err := parseBoolString(v.Text)
		if err != nil {
			return nil, err
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(b)}, nil
	case KindNumber:
		s := strings.TrimSpace(v.Text)
		if _, err := strconv.ParseInt(s, 10, 64); err == nil {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: s}, nil
		}
		if _, err := strconv.ParseUint(s, 10, 64); err == nil {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: s}, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("invalid number %q", v.Text)
		}
		// Match MarshalYAML: untagged scalar so integral values write cleanly.
		return &yaml.Node{Kind: yaml.ScalarNode, Value: s}, nil
	case KindDuration:
		s := strings.TrimSpace(v.Text)
		if _, err := time.ParseDuration(s); err != nil {
			return nil, fmt.Errorf("invalid duration %q: %w", v.Text, err)
		}
		return yamlString(s), nil
	case KindList:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
		for i, item := range v.List {
			child, err := yamlNodeFromCellValueDepth(item, depth+1)
			if err != nil {
				return nil, fmt.Errorf("element %d: %w", i, err)
			}
			seq.Content = append(seq.Content, child)
		}
		return seq, nil
	default:
		return nil, fmt.Errorf("unsupported value kind %v", v.Kind)
	}
}

// Cell is a key-value entry in an ordered Row.
type Cell struct {
	Key   string
	Value Value
}

// Row is an ordered mapping of column keys to cell values, preserving the YAML key order.
type Row struct {
	Cells []Cell
}

// Index returns the 0-based index of key in r, or -1 if absent.
func (r Row) Index(key string) int {
	for i, c := range r.Cells {
		if c.Key == key {
			return i
		}
	}
	return -1
}

// Get returns the value of key in r and whether key is present.
func (r Row) Get(key string) (Value, bool) {
	if i := r.Index(key); i >= 0 {
		return r.Cells[i].Value, true
	}
	return Value{}, false
}

// Keys returns the row's keys in their stored order.
func (r Row) Keys() []string {
	keys := make([]string, len(r.Cells))
	for i, c := range r.Cells {
		keys[i] = c.Key
	}
	return keys
}

// Clone returns a deep copy of r.
func (r Row) Clone() Row {
	cells := make([]Cell, len(r.Cells))
	for i, c := range r.Cells {
		cells[i] = Cell{Key: c.Key, Value: c.Value.Clone()}
	}
	return Row{Cells: cells}
}

// Document is an untyped, round-trip editable YAML data table.
//
// Each row keeps its own key order. Opening a table and calling Marshal without edits
// returns the exact input bytes; editing a single scalar or inline-list cell changes only
// that cell's line; structural edits (adding, deleting, or reordering rows, or adding a
// new key to a row) encode deterministically from the preserved YAML AST so untouched rows
// keep their key order, formatting, and comments.
type Document struct {
	version  int
	rows     []Row
	raw      []byte
	ast      *yaml.Node
	root     *yaml.Node
	rowsNode *yaml.Node
}

// NewDocument constructs an empty version 1 YAML data table document.
func NewDocument() *Document {
	rowsNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	root := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			yamlString(yamlVersionKey), {Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(FormatVersion)},
			yamlString(yamlRowsKey), rowsNode,
		},
	}
	ast := &yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{root},
	}
	return &Document{
		version:  FormatVersion,
		ast:      ast,
		root:     root,
		rowsNode: rowsNode,
	}
}

// LoadDocument reads path from disk and parses it as an untyped YAML data table.
func LoadDocument(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, loaderr.Locate(err, path, nil, "check that the table exists at this path and can be read")
	}
	return ParseDocument(path, data)
}

// ParseDocument parses a versioned YAML data table into an untyped Document.
// It enforces the same envelope rules as LoadYAML (version: 1, no unknown top-level keys,
// no merge keys, no duplicate keys, no trailing second document) without requiring a Go row type.
func ParseDocument(filename string, data []byte) (*Document, error) {
	raw := bytes.Clone(data)
	decoded := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	dec := yaml.NewDecoder(bytes.NewReader(decoded))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, loaderr.Locate(err, filename, decoded, fixTable)
		}
	} else {
		var extra yaml.Node
		if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
			if err != nil {
				return nil, loaderr.Locate(err, filename, decoded, fixTable)
			}
			return nil, yamlTableError(filename, &extra, "keep one document in the file; remove the --- and what follows it", "unexpected second YAML document; a data table is one document")
		}
	}

	var root *yaml.Node
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = resolveYAMLAlias(doc.Content[0], 0)
	}
	if root == nil || root.Tag == "!!null" {
		return nil, yamlTableError(filename, nil, fixYAMLTable, "empty YAML data table: want a mapping with %s: %d and %s", yamlVersionKey, FormatVersion, yamlRowsKey)
	}
	if root.Kind == yaml.SequenceNode {
		return nil, yamlTableError(filename, root, "put version: 1 first and the list under rows:", "a YAML data table is a mapping with %s: %d and %s, not a bare list of rows", yamlVersionKey, FormatVersion, yamlRowsKey)
	}
	if root.Kind != yaml.MappingNode {
		return nil, yamlTableError(filename, root, fixYAMLTable, "a YAML data table must be a mapping with %s and %s", yamlVersionKey, yamlRowsKey)
	}

	var versionNode, rowsNode *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		kNode := resolveYAMLAlias(root.Content[i], 0)
		vNode := resolveYAMLAlias(root.Content[i+1], 0)
		if kNode == nil || kNode.Kind != yaml.ScalarNode {
			return nil, yamlTableError(filename, root.Content[i], fixYAMLTable, "top-level keys must be scalars")
		}
		if kNode.Tag == yamlMergeTag {
			return nil, yamlTableError(filename, kNode, "write version and rows out in full", "%v", errYAMLMergeKey)
		}
		switch kNode.Value {
		case yamlVersionKey:
			if versionNode != nil {
				return nil, yamlTableError(filename, kNode, "remove one of the two keys", "duplicate key %q", kNode.Value)
			}
			versionNode = vNode
		case yamlRowsKey:
			if rowsNode != nil {
				return nil, yamlTableError(filename, kNode, "remove one of the two keys", "duplicate key %q", kNode.Value)
			}
			rowsNode = vNode
		default:
			return nil, yamlTableError(filename, kNode, "remove the key; a table has only version and rows", "unknown top-level key %q (want %s and %s)", kNode.Value, yamlVersionKey, yamlRowsKey)
		}
	}

	if versionNode == nil {
		return nil, yamlTableError(filename, root, fmt.Sprintf("add %s: %d", yamlVersionKey, FormatVersion), "missing %s (want %d)", yamlVersionKey, FormatVersion)
	}
	if versionNode.Kind != yaml.ScalarNode || versionNode.Tag != "!!int" {
		return nil, yamlTableError(filename, versionNode, fmt.Sprintf("set %s: %d", yamlVersionKey, FormatVersion), "%s must be an integer (want %d)", yamlVersionKey, FormatVersion)
	}
	if v, err := strconv.Atoi(versionNode.Value); err != nil || v != FormatVersion {
		return nil, yamlTableError(filename, versionNode, fmt.Sprintf("set %s: %d", yamlVersionKey, FormatVersion), "unsupported %s %s (want %d)", yamlVersionKey, versionNode.Value, FormatVersion)
	}

	d := &Document{
		version:  FormatVersion,
		raw:      raw,
		ast:      &doc,
		root:     root,
		rowsNode: rowsNode,
	}

	if rowsNode == nil || rowsNode.Tag == "!!null" {
		return d, nil
	}
	if rowsNode.Kind != yaml.SequenceNode {
		return nil, yamlTableError(filename, rowsNode, "write rows as a list, each row starting with \"- \"", "%s must be a list of mappings", yamlRowsKey)
	}

	d.rows = make([]Row, 0, len(rowsNode.Content))
	for i, item := range rowsNode.Content {
		rowNum := i + 1
		node := resolveYAMLAlias(item, 0)
		if node == nil || node.Kind != yaml.MappingNode {
			return nil, (&CellError{
				File:  filename,
				Row:   rowNum,
				Field: "<row>",
				Value: formatYAMLNode(node),
				Err:   errors.New("row must be a YAML mapping"),
			}).located(item.Line, item.Column, loaderr.Path(yamlRowsKey, i), "make each row a mapping of column names to values")
		}
		seen := make(map[string]struct{}, len(node.Content)/2)
		row := Row{Cells: make([]Cell, 0, len(node.Content)/2)}
		for j := 0; j+1 < len(node.Content); j += 2 {
			kNode := resolveYAMLAlias(node.Content[j], 0)
			vNode := resolveYAMLAlias(node.Content[j+1], 0)
			if kNode == nil || kNode.Kind != yaml.ScalarNode {
				return nil, (&CellError{
					File:  filename,
					Row:   rowNum,
					Field: "<key>",
					Value: formatYAMLNode(kNode),
					Err:   errors.New("row keys must be scalars"),
				}).located(node.Content[j].Line, node.Content[j].Column, loaderr.Path(yamlRowsKey, i), "name each column with a plain key")
			}
			if kNode.Tag == yamlMergeTag {
				return nil, (&CellError{
					File:  filename,
					Row:   rowNum,
					Field: kNode.Value,
					Value: formatYAMLNode(vNode),
					Err:   errYAMLMergeKey,
				}).located(kNode.Line, kNode.Column, loaderr.Path(yamlRowsKey, i), "write each column in the row")
			}
			key := kNode.Value
			if _, dup := seen[key]; dup {
				return nil, (&CellError{
					File:  filename,
					Row:   rowNum,
					Field: key,
					Value: formatYAMLNode(vNode),
					Err:   ErrDuplicateField,
				}).located(kNode.Line, kNode.Column, loaderr.Path(yamlRowsKey, i, key), "set the column once in the row")
			}
			seen[key] = struct{}{}
			val, err := cellValueFromYAMLNode(vNode, 0)
			if err != nil {
				return nil, (&CellError{
					File:  filename,
					Row:   rowNum,
					Field: key,
					Value: formatYAMLNode(vNode),
					Err:   err,
				}).located(kNode.Line, kNode.Column, loaderr.Path(yamlRowsKey, i, key), "write a scalar, null, or list value in this cell")
			}
			row.Cells = append(row.Cells, Cell{Key: key, Value: val})
		}
		d.rows = append(d.rows, row)
	}
	return d, nil
}

// Version returns the table's format version (FormatVersion).
func (d *Document) Version() int {
	if d == nil {
		return 0
	}
	return d.version
}

// RowCount returns the number of rows in d.
func (d *Document) RowCount() int {
	if d == nil {
		return 0
	}
	return len(d.rows)
}

// Row returns a copy of row index.
func (d *Document) Row(index int) Row {
	return d.rows[index].Clone()
}

// Rows returns copies of all rows in document order.
func (d *Document) Rows() []Row {
	if d == nil || len(d.rows) == 0 {
		return nil
	}
	out := make([]Row, len(d.rows))
	for i := range d.rows {
		out[i] = d.rows[i].Clone()
	}
	return out
}

// Columns returns the union of all rows' keys in deterministic first-appearance order.
func (d *Document) Columns() []string {
	if d == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var cols []string
	for _, r := range d.rows {
		for _, c := range r.Cells {
			if _, ok := seen[c.Key]; !ok {
				seen[c.Key] = struct{}{}
				cols = append(cols, c.Key)
			}
		}
	}
	return cols
}

// ColumnKind infers the ValueKind of col from the non-null values present in d.
// It returns KindNull if no row has a non-null value for col, the shared kind if all
// non-null values agree, or KindString if values have mixed kinds.
func (d *Document) ColumnKind(col string) ValueKind {
	if d == nil {
		return KindNull
	}
	kind := KindNull
	for _, r := range d.rows {
		v, ok := r.Get(col)
		if !ok || v.Kind == KindNull {
			continue
		}
		if kind == KindNull {
			kind = v.Kind
			continue
		}
		if kind != v.Kind {
			return KindString
		}
	}
	return kind
}

// Cell returns the value of col in row, and whether that cell is present.
func (d *Document) Cell(row int, col string) (Value, bool) {
	if d == nil || row < 0 || row >= len(d.rows) {
		return Value{}, false
	}
	v, ok := d.rows[row].Get(col)
	if !ok {
		return Value{}, false
	}
	return v.Clone(), true
}

// RowLine returns the 1-based YAML line number where row begins in the parsed
// document, or 0 if line metadata is unavailable (for example on a newly added row).
func (d *Document) RowLine(row int) int {
	if d == nil || d.rowsNode == nil || row < 0 || row >= len(d.rowsNode.Content) {
		return 0
	}
	node := d.rowsNode.Content[row]
	if node == nil {
		return 0
	}
	if node.Line > 0 {
		return node.Line
	}
	resolved := resolveYAMLAlias(node, 0)
	if resolved != nil && resolved.Line > 0 {
		return resolved.Line
	}
	return 0
}

// CellLine returns the 1-based YAML line number of column col in row, or 0 if
// the cell or its line metadata is unavailable.
func (d *Document) CellLine(row int, col string) int {
	if d == nil || d.rowsNode == nil || row < 0 || row >= len(d.rowsNode.Content) {
		return 0
	}
	node := resolveYAMLAlias(d.rowsNode.Content[row], 0)
	if node == nil || node.Kind != yaml.MappingNode {
		return 0
	}
	for j := 0; j+1 < len(node.Content); j += 2 {
		kNode := resolveYAMLAlias(node.Content[j], 0)
		if kNode != nil && kNode.Value == col {
			if node.Content[j].Line > 0 {
				return node.Content[j].Line
			}
			return kNode.Line
		}
	}
	return 0
}

// SetCell updates or inserts col in row with val.
// Updating an existing scalar or inline-list cell on a block-style row patches only that
// line in the preserved YAML bytes; inserting a new key appends it to that row and encodes
// deterministically from the AST.
func (d *Document) SetCell(row int, col string, val Value) error {
	if d == nil {
		return errors.New("datatable: nil document")
	}
	if row < 0 || row >= len(d.rows) {
		return fmt.Errorf("datatable: row %d out of range [0, %d)", row, len(d.rows))
	}
	if col == "" {
		return errors.New("datatable: column name must not be empty")
	}
	newNode, err := yamlNodeFromCellValue(val)
	if err != nil {
		return &CellError{Row: row + 1, Field: col, Value: val.Display(), Err: err}
	}

	r := &d.rows[row]
	if idx := r.Index(col); idx >= 0 {
		if r.Cells[idx].Value.Equal(val) {
			return nil
		}
		r.Cells[idx].Value = val.Clone()
		d.updateASTCell(row, col, newNode)
		return nil
	}

	r.Cells = append(r.Cells, Cell{Key: col, Value: val.Clone()})
	d.appendASTCell(row, col, newNode)
	d.raw = nil
	return nil
}

// DeleteCell removes col from row if present.
func (d *Document) DeleteCell(row int, col string) error {
	if d == nil {
		return errors.New("datatable: nil document")
	}
	if row < 0 || row >= len(d.rows) {
		return fmt.Errorf("datatable: row %d out of range [0, %d)", row, len(d.rows))
	}
	r := &d.rows[row]
	idx := r.Index(col)
	if idx < 0 {
		return nil
	}
	r.Cells = slices.Delete(r.Cells, idx, idx+1)
	if rowNode := d.astRowNode(row); rowNode != nil {
		for j := 0; j+1 < len(rowNode.Content); j += 2 {
			kNode := resolveYAMLAlias(rowNode.Content[j], 0)
			if kNode != nil && kNode.Value == col {
				rowNode.Content = slices.Delete(rowNode.Content, j, j+2)
				break
			}
		}
		if len(rowNode.Content) == 0 {
			rowNode.Style = yaml.FlowStyle
		}
	}
	d.raw = nil
	return nil
}

// AddRow inserts a new row at index (0 <= index <= RowCount()) with the given ordered cells.
func (d *Document) AddRow(index int, cells ...Cell) error {
	if d == nil {
		return errors.New("datatable: nil document")
	}
	if index < 0 || index > len(d.rows) {
		return fmt.Errorf("datatable: insert index %d out of range [0, %d]", index, len(d.rows))
	}
	rowNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if len(cells) == 0 {
		rowNode.Style = yaml.FlowStyle
	}
	seen := make(map[string]struct{}, len(cells))
	cloned := make([]Cell, len(cells))
	for i, c := range cells {
		if c.Key == "" {
			return errors.New("datatable: column name must not be empty")
		}
		if _, dup := seen[c.Key]; dup {
			return &CellError{Row: index + 1, Field: c.Key, Value: c.Value.Display(), Err: ErrDuplicateField}
		}
		seen[c.Key] = struct{}{}
		vNode, err := yamlNodeFromCellValue(c.Value)
		if err != nil {
			return &CellError{Row: index + 1, Field: c.Key, Value: c.Value.Display(), Err: err}
		}
		rowNode.Content = append(rowNode.Content, yamlString(c.Key), vNode)
		cloned[i] = Cell{Key: c.Key, Value: c.Value.Clone()}
	}

	d.ensureRowsNode()
	d.rowsNode.Style &^= yaml.FlowStyle
	d.rowsNode.Content = slices.Insert(d.rowsNode.Content, index, rowNode)
	d.rows = slices.Insert(d.rows, index, Row{Cells: cloned})
	d.raw = nil
	return nil
}

// AppendRow appends a new row at the end of d and returns its 0-based index.
func (d *Document) AppendRow(cells ...Cell) (int, error) {
	if d == nil {
		return -1, errors.New("datatable: nil document")
	}
	idx := len(d.rows)
	if err := d.AddRow(idx, cells...); err != nil {
		return -1, err
	}
	return idx, nil
}

// DeleteRow removes the row at index.
func (d *Document) DeleteRow(index int) error {
	if d == nil {
		return errors.New("datatable: nil document")
	}
	if index < 0 || index >= len(d.rows) {
		return fmt.Errorf("datatable: row %d out of range [0, %d)", index, len(d.rows))
	}
	d.rows = slices.Delete(d.rows, index, index+1)
	if d.rowsNode != nil && index < len(d.rowsNode.Content) {
		d.rowsNode.Content = slices.Delete(d.rowsNode.Content, index, index+1)
		if len(d.rowsNode.Content) == 0 {
			d.rowsNode.Style = yaml.FlowStyle
		}
	}
	d.raw = nil
	return nil
}

// MoveRow moves the row at index from to index to, preserving its key order, comments, and formatting.
func (d *Document) MoveRow(from, to int) error {
	if d == nil {
		return errors.New("datatable: nil document")
	}
	if from < 0 || from >= len(d.rows) {
		return fmt.Errorf("datatable: row %d out of range [0, %d)", from, len(d.rows))
	}
	if to < 0 || to >= len(d.rows) {
		return fmt.Errorf("datatable: target row %d out of range [0, %d)", to, len(d.rows))
	}
	if from == to {
		return nil
	}
	r := d.rows[from]
	d.rows = slices.Delete(d.rows, from, from+1)
	d.rows = slices.Insert(d.rows, to, r)
	if d.rowsNode != nil && from < len(d.rowsNode.Content) {
		n := d.rowsNode.Content[from]
		d.rowsNode.Content = slices.Delete(d.rowsNode.Content, from, from+1)
		d.rowsNode.Content = slices.Insert(d.rowsNode.Content, to, n)
	}
	d.raw = nil
	return nil
}

// Clone returns a deep copy of d, including its preserved bytes and YAML AST.
func (d *Document) Clone() *Document {
	if d == nil {
		return nil
	}
	out := &Document{
		version: d.version,
		rows:    d.Rows(),
		raw:     bytes.Clone(d.raw),
	}
	if d.ast != nil {
		nodeMap := make(map[*yaml.Node]*yaml.Node)
		out.ast = cloneYAMLNode(d.ast, nodeMap)
		out.root = nodeMap[d.root]
		out.rowsNode = nodeMap[d.rowsNode]
	}
	return out
}

// Marshal encodes d back to YAML. If no edits have invalidated the preserved bytes,
// Marshal returns the exact preserved bytes (unchanged for an unedited file, or patched
// on only the edited lines); otherwise it encodes deterministically from the AST with
// two-space indentation.
func (d *Document) Marshal() ([]byte, error) {
	if d == nil {
		return nil, errors.New("datatable: nil document")
	}
	if d.raw != nil {
		return bytes.Clone(d.raw), nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(d.ast); err != nil {
		return nil, fmt.Errorf("datatable: encode YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("datatable: encode YAML: %w", err)
	}
	return buf.Bytes(), nil
}

// Save marshals d and writes it to path.
func (d *Document) Save(path string) error {
	data, err := d.Marshal()
	if err != nil {
		return fmt.Errorf("datatable: %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("datatable: write %q: %w", path, err)
	}
	return nil
}

func (d *Document) ensureRowsNode() {
	if d.rowsNode != nil && d.rowsNode.Kind == yaml.SequenceNode {
		return
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	if d.rowsNode != nil {
		// Replace an existing null rows: node in place.
		for i := 0; i+1 < len(d.root.Content); i += 2 {
			if k := resolveYAMLAlias(d.root.Content[i], 0); k != nil && k.Value == yamlRowsKey {
				seq.HeadComment = d.root.Content[i+1].HeadComment
				seq.LineComment = d.root.Content[i+1].LineComment
				seq.FootComment = d.root.Content[i+1].FootComment
				d.root.Content[i+1] = seq
				d.rowsNode = seq
				return
			}
		}
	}
	d.root.Content = append(d.root.Content, yamlString(yamlRowsKey), seq)
	d.rowsNode = seq
}

func (d *Document) astRowNode(row int) *yaml.Node {
	if d.rowsNode == nil || row < 0 || row >= len(d.rowsNode.Content) {
		return nil
	}
	item := d.rowsNode.Content[row]
	if item.Kind == yaml.AliasNode {
		// Detach alias into an independent mapping copy before mutating it.
		resolved := resolveYAMLAlias(item, 0)
		if resolved == nil || resolved.Kind != yaml.MappingNode {
			return nil
		}
		cloned := cloneYAMLNode(resolved, make(map[*yaml.Node]*yaml.Node))
		cloned.Anchor = ""
		d.rowsNode.Content[row] = cloned
		d.raw = nil
		return cloned
	}
	return item
}

func (d *Document) updateASTCell(row int, col string, newNode *yaml.Node) {
	rowNode := d.astRowNode(row)
	if rowNode == nil {
		d.raw = nil
		return
	}
	for j := 0; j+1 < len(rowNode.Content); j += 2 {
		kNode := rowNode.Content[j]
		resolvedK := resolveYAMLAlias(kNode, 0)
		if resolvedK == nil || resolvedK.Value != col {
			continue
		}
		oldV := rowNode.Content[j+1]

		patched := false
		if d.raw != nil && rowNode.Style&yaml.FlowStyle == 0 && rowNode.Anchor == "" &&
			kNode.Kind == yaml.ScalarNode && kNode.Anchor == "" &&
			oldV.Kind != yaml.AliasNode && oldV.Anchor == "" {
			if nextRaw, ok := patchRawCellLine(d.raw, kNode, oldV, newNode); ok {
				d.raw = nextRaw
				patched = true
			}
		}
		if !patched {
			d.raw = nil
		}

		// Preserve comments and line/column metadata on the replaced AST node.
		newNode.HeadComment = oldV.HeadComment
		newNode.LineComment = oldV.LineComment
		newNode.FootComment = oldV.FootComment
		if patched {
			newNode.Line = oldV.Line
			newNode.Column = oldV.Column
		}
		rowNode.Content[j+1] = newNode
		return
	}
	d.raw = nil
}

func (d *Document) appendASTCell(row int, col string, newNode *yaml.Node) {
	rowNode := d.astRowNode(row)
	if rowNode == nil {
		return
	}
	rowNode.Style &^= yaml.FlowStyle
	rowNode.Content = append(rowNode.Content, yamlString(col), newNode)
}

func formatInlineYAMLNode(n *yaml.Node) (string, bool) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return "", false
	}
	if err := enc.Close(); err != nil {
		return "", false
	}
	s := strings.TrimSuffix(buf.String(), "\n")
	if s == "" || strings.ContainsAny(s, "\r\n") {
		return "", false
	}
	return s, true
}

func isSingleLineNode(n *yaml.Node) bool {
	if n == nil || n.Line <= 0 {
		return false
	}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			return false
		}
		return !strings.ContainsAny(n.Value, "\r\n")
	case yaml.SequenceNode:
		if n.Style&yaml.FlowStyle == 0 {
			return false
		}
		for _, c := range n.Content {
			if c.Line != n.Line || !isSingleLineNode(c) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// patchRawCellLine replaces only the value token on kNode.Line in raw when both the old
// and new values fit cleanly on a single line, keeping indentation, comments, blank lines,
// and every other line byte-for-byte identical.
func patchRawCellLine(raw []byte, kNode, oldV, newNode *yaml.Node) ([]byte, bool) {
	if !isSingleLineNode(oldV) || kNode.Line <= 0 || oldV.Line != kNode.Line || oldV.Column <= kNode.Column {
		return nil, false
	}
	formatted, ok := formatInlineYAMLNode(newNode)
	if !ok {
		return nil, false
	}
	// Strip UTF-8 BOM offset for line/column indexing while preserving the BOM in the output.
	bomLen := 0
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		bomLen = 3
	}
	body := raw[bomLen:]
	lineStart, lineEnd, ok := findLineBounds(body, oldV.Line)
	if !ok {
		return nil, false
	}
	line := body[lineStart:lineEnd]
	valStart, ok := runeColumnByteOffset(line, oldV.Column)
	if !ok || valStart >= len(line) {
		return nil, false
	}
	// Implicit null (e.g. "notes:") has no value token on the line.
	if oldV.Tag == "!!null" && oldV.Value == "" && (line[valStart] == '#' || line[valStart] == ' ' || line[valStart] == '\t') {
		return nil, false
	}
	valEnd, ok := scanYAMLInlineValueEnd(line, valStart)
	if !ok || valEnd <= valStart {
		return nil, false
	}
	absStart := bomLen + lineStart + valStart
	absEnd := bomLen + lineStart + valEnd
	out := make([]byte, 0, len(raw)-(absEnd-absStart)+len(formatted))
	out = append(out, raw[:absStart]...)
	out = append(out, formatted...)
	out = append(out, raw[absEnd:]...)
	return out, true
}

func findLineBounds(data []byte, lineNum int) (start, end int, ok bool) {
	if lineNum < 1 {
		return 0, 0, false
	}
	cur := 1
	pos := 0
	for cur < lineNum {
		nl := bytes.IndexByte(data[pos:], '\n')
		if nl < 0 {
			return 0, 0, false
		}
		pos += nl + 1
		cur++
	}
	if pos > len(data) {
		return 0, 0, false
	}
	end = len(data)
	if nl := bytes.IndexByte(data[pos:], '\n'); nl >= 0 {
		end = pos + nl
	}
	if end > pos && data[end-1] == '\r' {
		end--
	}
	return pos, end, true
}

func runeColumnByteOffset(line []byte, col int) (int, bool) {
	if col < 1 {
		return 0, false
	}
	offset := 0
	for i := 1; i < col; i++ {
		if offset >= len(line) {
			return 0, false
		}
		_, size := utf8.DecodeRune(line[offset:])
		offset += size
	}
	return offset, true
}

// scanYAMLInlineValueEnd finds the byte end of a single-line YAML scalar or flow sequence
// starting at start in line, verifying that only optional whitespace and an optional inline
// comment follow it on the line.
func scanYAMLInlineValueEnd(line []byte, start int) (int, bool) {
	if start >= len(line) {
		return 0, false
	}
	var end int
	switch line[start] {
	case '"':
		i := start + 1
		for i < len(line) {
			if line[i] == '\\' {
				i += 2
				continue
			}
			if line[i] == '"' {
				end = i + 1
				break
			}
			i++
		}
		if end == 0 {
			return 0, false
		}
	case '\'':
		i := start + 1
		for i < len(line) {
			if line[i] == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i += 2
					continue
				}
				end = i + 1
				break
			}
			i++
		}
		if end == 0 {
			return 0, false
		}
	case '[':
		depth := 0
		inSingle := false
		inDouble := false
		for i := start; i < len(line); i++ {
			c := line[i]
			switch {
			case inDouble:
				if c == '\\' {
					i++
				} else if c == '"' {
					inDouble = false
				}
			case inSingle:
				if c == '\'' {
					if i+1 < len(line) && line[i+1] == '\'' {
						i++
					} else {
						inSingle = false
					}
				}
			default:
				switch c {
				case '"':
					inDouble = true
				case '\'':
					inSingle = true
				case '[':
					depth++
				case ']':
					depth--
					if depth == 0 {
						end = i + 1
					}
				}
			}
			if end != 0 {
				break
			}
		}
		if end == 0 {
			return 0, false
		}
	default:
		// Plain scalar: ends before a comment (" #" or "\t#") or EOL, trimming trailing spaces/tabs.
		end = len(line)
		for i := start; i < len(line); i++ {
			if line[i] == '#' && i > start && (line[i-1] == ' ' || line[i-1] == '\t') {
				end = i
				break
			}
		}
		for end > start && (line[end-1] == ' ' || line[end-1] == '\t') {
			end--
		}
	}

	// Verify nothing unexpected trails the value on this line.
	rest := line[end:]
	i := 0
	for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t') {
		i++
	}
	if i < len(rest) && rest[i] != '#' {
		return 0, false
	}
	return end, true
}

func cloneYAMLNode(n *yaml.Node, seen map[*yaml.Node]*yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if existing, ok := seen[n]; ok {
		return existing
	}
	cp := *n
	seen[n] = &cp
	if len(n.Content) > 0 {
		cp.Content = make([]*yaml.Node, len(n.Content))
		for i, child := range n.Content {
			cp.Content[i] = cloneYAMLNode(child, seen)
		}
	}
	if n.Alias != nil {
		cp.Alias = cloneYAMLNode(n.Alias, seen)
	}
	return &cp
}
