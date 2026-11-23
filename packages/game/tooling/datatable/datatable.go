// Package datatable loads typed game data tables (enemies, items, levels, quests) from CSV, JSON, and YAML with rich per-cell error reporting and live hot reload.
//
// YAML is the table format this repository defines: a mapping with a version field (FormatVersion) and a rows list, in which every key of a row names a field of the row type. Unknown fields, duplicate fields, missing or unsupported versions are refused. MarshalYAML and SaveYAML write the same format deterministically (fields in struct order, two-space indentation), so a table loaded and saved again is unchanged. CSV and JSON tables are read as their tools export them.
package datatable

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

var durationType = reflect.TypeOf(time.Duration(0))

// CellError identifies the exact file, 1-based data row, and column/field name
// where parsing or type conversion failed.
type CellError struct {
	File  string
	Row   int
	Field string
	Value string
	Err   error
}

func (e *CellError) Error() string {
	file := e.File
	if file == "" {
		file = "<input>"
	}
	if e.Field == "" {
		return fmt.Sprintf("%s: row %d: %v", file, e.Row, e.Err)
	}
	return fmt.Sprintf("%s: row %d, field %q (value %q): %v", file, e.Row, e.Field, e.Value, e.Err)
}

func (e *CellError) Unwrap() error { return e.Err }

// FormatVersion is the schema version of a YAML data table: LoadYAML accepts only this version and MarshalYAML writes it.
const FormatVersion = 1

// ErrUnknownField is the error a CellError wraps when a YAML row has a key that names no field of the row type.
var ErrUnknownField = errors.New("unknown field")

// ErrDuplicateField is the error a CellError wraps when a YAML row sets the same field twice, for example as "hp" and "HP".
var ErrDuplicateField = errors.New("duplicate field")

type fieldBinding struct {
	index []int
	name  string
	typ   reflect.Type
	// pos identifies the struct field, so two keys that resolve to the same field can be detected.
	pos int
}

type structPlan struct {
	exact map[string]fieldBinding
	lower map[string]fieldBinding
	// fields lists each column once, in struct declaration order, under the name MarshalYAML writes.
	fields []fieldBinding
	n      int
	// shadowed names the first column claimed by more than one field; CSV and JSON keep the first field, YAML refuses the type because the other field could never be saved or loaded.
	shadowed string
}

func buildStructPlan(t reflect.Type) (*structPlan, error) {
	if t == nil || t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("datatable: target type %v must be a struct", t)
	}
	plan := &structPlan{
		exact: make(map[string]fieldBinding),
		lower: make(map[string]fieldBinding),
	}
	collectFields(t, nil, plan)
	return plan, nil
}

func collectFields(t reflect.Type, prefix []int, plan *structPlan) {
	for i := range t.NumField() {
		f := t.Field(i)
		idx := append(append([]int(nil), prefix...), i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct && f.Tag.Get("table") == "" && f.Tag.Get("json") == "" {
			collectFields(f.Type, idx, plan)
			continue
		}
		if !f.IsExported() {
			continue
		}
		colName, skip := resolveColumnName(f)
		if skip {
			continue
		}
		fb := fieldBinding{
			index: idx,
			name:  colName,
			typ:   f.Type,
			pos:   plan.n,
		}
		plan.n++
		if _, exists := plan.exact[colName]; !exists {
			plan.exact[colName] = fb
			plan.fields = append(plan.fields, fb)
		} else if plan.shadowed == "" {
			plan.shadowed = colName
		}
		low := strings.ToLower(colName)
		if _, exists := plan.lower[low]; !exists {
			plan.lower[low] = fb
		}
		// Also register the Go field name case-insensitively as a fallback when no tag is present, only if no other field already claimed it.
		fieldLow := strings.ToLower(f.Name)
		if _, exists := plan.lower[fieldLow]; !exists && f.Tag.Get("table") == "" && f.Tag.Get("json") == "" {
			plan.lower[fieldLow] = fb
		}
	}
}

func resolveColumnName(f reflect.StructField) (string, bool) {
	if raw, ok := f.Tag.Lookup("table"); ok {
		name := strings.TrimSpace(strings.Split(raw, ",")[0])
		if name == "-" {
			return "", true
		}
		if name != "" {
			return name, false
		}
	}
	if raw, ok := f.Tag.Lookup("json"); ok {
		name := strings.TrimSpace(strings.Split(raw, ",")[0])
		if name == "-" {
			return "", true
		}
		if name != "" {
			return name, false
		}
	}
	return f.Name, false
}

func (p *structPlan) lookup(col string) (fieldBinding, bool) {
	trimmed := strings.TrimSpace(col)
	if fb, ok := p.exact[trimmed]; ok {
		return fb, true
	}
	fb, ok := p.lower[strings.ToLower(trimmed)]
	return fb, ok
}

// LoadFile reads path from disk and decodes it into []T based on its file extension
// (.csv, .json, .yaml, or .yml).
func LoadFile[T any](path string) ([]T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("datatable: read %q: %w", path, err)
	}
	return loadByExtension[T](path, data)
}

func loadByExtension[T any](filename string, data []byte) ([]T, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".csv":
		return LoadCSV[T](filename, data)
	case ".json":
		return LoadJSON[T](filename, data)
	case ".yaml", ".yml":
		return LoadYAML[T](filename, data)
	default:
		return nil, fmt.Errorf("datatable: unsupported file extension %q for %q (want .csv, .json, .yaml, or .yml)", filepath.Ext(filename), filename)
	}
}

// LoadCSV parses UTF-8 CSV data with a header row into []T.
// Slice fields in CSV cells may be delimited by ';', '|', or ','.
func LoadCSV[T any](filename string, data []byte) ([]T, error) {
	var zero T
	plan, err := buildStructPlan(reflect.TypeOf(zero))
	if err != nil {
		return nil, err
	}

	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // strip UTF-8 BOM
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}

	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1

	header, err := r.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: parse CSV header: %w", displayFile(filename), err)
	}

	bindings := make([]*fieldBinding, len(header))
	for i, col := range header {
		if fb, ok := plan.lookup(col); ok {
			copyFB := fb
			bindings[i] = &copyFB
		}
	}

	var rows []T
	rowNum := 0
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		rowNum++
		if err != nil {
			return nil, &CellError{
				File: filename,
				Row:  rowNum,
				Err:  err,
			}
		}
		if len(rec) != len(header) {
			return nil, &CellError{
				File:  filename,
				Row:   rowNum,
				Field: "<row>",
				Value: strings.Join(rec, ","),
				Err:   fmt.Errorf("expected %d columns, got %d", len(header), len(rec)),
			}
		}
		var elem T
		rv := reflect.ValueOf(&elem).Elem()
		for c, val := range rec {
			fb := bindings[c]
			if fb == nil {
				continue
			}
			fv := rv.FieldByIndex(fb.index)
			if err := assignFromString(fv, val); err != nil {
				return nil, &CellError{
					File:  filename,
					Row:   rowNum,
					Field: strings.TrimSpace(header[c]),
					Value: val,
					Err:   err,
				}
			}
		}
		rows = append(rows, elem)
	}
	return rows, nil
}

// LoadJSON parses a JSON array of objects into []T.
func LoadJSON[T any](filename string, data []byte) ([]T, error) {
	var zero T
	plan, err := buildStructPlan(reflect.TypeOf(zero))
	if err != nil {
		return nil, err
	}

	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var rawRows []json.RawMessage
	if err := dec.Decode(&rawRows); err != nil {
		return nil, fmt.Errorf("%s: parse JSON array: %w", displayFile(filename), err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%s: unexpected trailing JSON data", displayFile(filename))
	}

	rows := make([]T, 0, len(rawRows))
	for i, rawRow := range rawRows {
		rowNum := i + 1
		rowDec := json.NewDecoder(bytes.NewReader(rawRow))
		rowDec.UseNumber()
		var obj map[string]any
		if err := rowDec.Decode(&obj); err != nil || obj == nil {
			errMsg := err
			if errMsg == nil {
				errMsg = errors.New("row must be a JSON object")
			}
			return nil, &CellError{
				File:  filename,
				Row:   rowNum,
				Field: "<row>",
				Value: strings.TrimSpace(string(rawRow)),
				Err:   errMsg,
			}
		}

		var elem T
		rv := reflect.ValueOf(&elem).Elem()
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			fb, ok := plan.lookup(k)
			if !ok {
				continue
			}
			val := obj[k]
			fv := rv.FieldByIndex(fb.index)
			if err := assignFromAny(fv, val); err != nil {
				return nil, &CellError{
					File:  filename,
					Row:   rowNum,
					Field: k,
					Value: formatAny(val),
					Err:   err,
				}
			}
		}
		rows = append(rows, elem)
	}
	return rows, nil
}

const maxYAMLDepth = 32

func resolveYAMLAlias(n *yaml.Node, depth int) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		if depth >= maxYAMLDepth {
			return nil
		}
		n = n.Alias
		depth++
	}
	return n
}

func assignFromYAMLNode(fv reflect.Value, n *yaml.Node, depth int) error {
	if depth > maxYAMLDepth {
		return errors.New("YAML nesting limit exceeded")
	}
	n = resolveYAMLAlias(n, depth)
	if n == nil || n.Tag == "!!null" {
		return nil
	}
	if fv.Kind() == reflect.Pointer {
		elem := reflect.New(fv.Type().Elem())
		if err := assignFromYAMLNode(elem.Elem(), n, depth+1); err != nil {
			return err
		}
		fv.Set(elem)
		return nil
	}
	if fv.Kind() == reflect.Slice {
		switch n.Kind {
		case yaml.SequenceNode:
			slice := reflect.MakeSlice(fv.Type(), len(n.Content), len(n.Content))
			for i, child := range n.Content {
				if err := assignFromYAMLNode(slice.Index(i), child, depth+1); err != nil {
					return fmt.Errorf("element %d: %w", i, err)
				}
			}
			fv.Set(slice)
			return nil
		case yaml.ScalarNode:
			return assignSliceFromString(fv, n.Value)
		default:
			return fmt.Errorf("expected sequence for %s, got YAML mapping", fv.Type())
		}
	}
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("expected scalar for %s, got non-scalar YAML node", fv.Type())
	}
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(n.Value)
		return nil
	case reflect.Bool:
		if n.Tag == "!!str" {
			return assignBoolFromString(fv, n.Value)
		}
		if n.Tag != "!!bool" && n.Tag != "" {
			return fmt.Errorf("cannot convert YAML %s %q to bool", n.Tag, n.Value)
		}
		return assignBoolFromString(fv, n.Value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Type() == durationType {
			return assignDurationFromString(fv, n.Value)
		}
		if n.Tag == "!!bool" {
			return fmt.Errorf("cannot convert bool %q to %s", n.Value, fv.Type())
		}
		return assignIntFromString(fv, n.Value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n.Tag == "!!bool" {
			return fmt.Errorf("cannot convert bool %q to %s", n.Value, fv.Type())
		}
		return assignUintFromString(fv, n.Value)
	case reflect.Float32, reflect.Float64:
		if n.Tag == "!!bool" {
			return fmt.Errorf("cannot convert bool %q to %s", n.Value, fv.Type())
		}
		return assignFloatFromString(fv, n.Value)
	default:
		return fmt.Errorf("unsupported field type %s", fv.Type())
	}
}

func assignFromAny(fv reflect.Value, val any) error {
	if val == nil {
		return nil
	}
	if fv.Kind() == reflect.Pointer {
		elem := reflect.New(fv.Type().Elem())
		if err := assignFromAny(elem.Elem(), val); err != nil {
			return err
		}
		fv.Set(elem)
		return nil
	}
	if fv.Kind() == reflect.Slice {
		switch s := val.(type) {
		case []any:
			slice := reflect.MakeSlice(fv.Type(), len(s), len(s))
			for i, item := range s {
				if err := assignFromAny(slice.Index(i), item); err != nil {
					return fmt.Errorf("element %d: %w", i, err)
				}
			}
			fv.Set(slice)
			return nil
		case string:
			return assignSliceFromString(fv, s)
		default:
			return fmt.Errorf("expected array for %s, got %T", fv.Type(), val)
		}
	}

	switch fv.Kind() {
	case reflect.String:
		s, ok := val.(string)
		if !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
		fv.SetString(s)
		return nil
	case reflect.Bool:
		switch b := val.(type) {
		case bool:
			fv.SetBool(b)
			return nil
		case string:
			return assignBoolFromString(fv, b)
		default:
			return fmt.Errorf("expected bool, got %T", val)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if fv.Type() == durationType {
			switch v := val.(type) {
			case string:
				return assignDurationFromString(fv, v)
			case json.Number:
				return assignDurationFromString(fv, v.String())
			default:
				return fmt.Errorf("expected duration string or integer, got %T", val)
			}
		}
		switch v := val.(type) {
		case json.Number:
			return assignIntFromString(fv, v.String())
		case float64:
			if math.Trunc(v) != v || math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("non-integer float %v for %s", v, fv.Type())
			}
			return assignIntFromString(fv, strconv.FormatInt(int64(v), 10))
		default:
			return fmt.Errorf("expected integer for %s, got %T", fv.Type(), val)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		switch v := val.(type) {
		case json.Number:
			return assignUintFromString(fv, v.String())
		case float64:
			if v < 0 || math.Trunc(v) != v || math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("invalid unsigned integer %v for %s", v, fv.Type())
			}
			return assignUintFromString(fv, strconv.FormatUint(uint64(v), 10))
		default:
			return fmt.Errorf("expected unsigned integer for %s, got %T", fv.Type(), val)
		}
	case reflect.Float32, reflect.Float64:
		switch v := val.(type) {
		case json.Number:
			return assignFloatFromString(fv, v.String())
		case float64:
			if fv.OverflowFloat(v) {
				return fmt.Errorf("float %v overflows %s", v, fv.Type())
			}
			fv.SetFloat(v)
			return nil
		default:
			return fmt.Errorf("expected number for %s, got %T", fv.Type(), val)
		}
	default:
		return fmt.Errorf("unsupported field type %s", fv.Type())
	}
}

func assignFromString(fv reflect.Value, raw string) error {
	if fv.Kind() == reflect.Pointer {
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		elem := reflect.New(fv.Type().Elem())
		if err := assignFromString(elem.Elem(), raw); err != nil {
			return err
		}
		fv.Set(elem)
		return nil
	}
	if fv.Kind() == reflect.Slice {
		return assignSliceFromString(fv, raw)
	}
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
		return nil
	case reflect.Bool:
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		return assignBoolFromString(fv, raw)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		if fv.Type() == durationType {
			return assignDurationFromString(fv, raw)
		}
		return assignIntFromString(fv, raw)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		return assignUintFromString(fv, raw)
	case reflect.Float32, reflect.Float64:
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		return assignFloatFromString(fv, raw)
	default:
		return fmt.Errorf("unsupported field type %s", fv.Type())
	}
}

func assignSliceFromString(fv reflect.Value, raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	sep := ","
	switch {
	case strings.Contains(trimmed, ";"):
		sep = ";"
	case strings.Contains(trimmed, "|"):
		sep = "|"
	}
	parts := strings.Split(trimmed, sep)
	slice := reflect.MakeSlice(fv.Type(), len(parts), len(parts))
	for i, part := range parts {
		item := strings.TrimSpace(part)
		if err := assignFromString(slice.Index(i), item); err != nil {
			return fmt.Errorf("element %d (%q): %w", i, item, err)
		}
	}
	fv.Set(slice)
	return nil
}

func assignIntFromString(fv reflect.Value, raw string) error {
	s := strings.TrimSpace(raw)
	n, err := strconv.ParseInt(s, 10, fv.Type().Bits())
	if err != nil {
		return fmt.Errorf("invalid %s: %w", fv.Type(), err)
	}
	fv.SetInt(n)
	return nil
}

func assignUintFromString(fv reflect.Value, raw string) error {
	s := strings.TrimSpace(raw)
	n, err := strconv.ParseUint(s, 10, fv.Type().Bits())
	if err != nil {
		return fmt.Errorf("invalid %s: %w", fv.Type(), err)
	}
	fv.SetUint(n)
	return nil
}

func assignFloatFromString(fv reflect.Value, raw string) error {
	s := strings.TrimSpace(raw)
	f, err := strconv.ParseFloat(s, fv.Type().Bits())
	if err != nil {
		return fmt.Errorf("invalid %s: %w", fv.Type(), err)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("non-finite %s value %q", fv.Type(), s)
	}
	fv.SetFloat(f)
	return nil
}

func assignBoolFromString(fv reflect.Value, raw string) error {
	s := strings.TrimSpace(raw)
	switch strings.ToLower(s) {
	case "yes", "y", "on":
		fv.SetBool(true)
		return nil
	case "no", "n", "off":
		fv.SetBool(false)
		return nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return fmt.Errorf("invalid bool: %w", err)
	}
	fv.SetBool(b)
	return nil
}

func assignDurationFromString(fv reflect.Value, raw string) error {
	s := strings.TrimSpace(raw)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		fv.SetInt(n)
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}
	fv.SetInt(int64(d))
	return nil
}

func displayFile(filename string) string {
	if filename == "" {
		return "<input>"
	}
	return filename
}

func formatAny(v any) string {
	if v == nil {
		return "null"
	}
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(b)
	}
}

func formatYAMLNode(n *yaml.Node) string {
	if n == nil {
		return "null"
	}
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	b, err := yaml.Marshal(n)
	if err != nil {
		return n.Value
	}
	return strings.TrimSpace(string(b))
}
