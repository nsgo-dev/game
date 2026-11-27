package datatable

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"strings"
)

// ColumnSpec describes one column of a Go struct row type.
type ColumnSpec struct {
	Name     string
	Kind     ValueKind
	ElemKind ValueKind
	Nullable bool
	Type     reflect.Type
}

// RowType describes a Go struct row type so an untyped Document or editor grid can
// validate cells and rows with the exact same rules LoadYAML applies.
type RowType struct {
	typ   reflect.Type
	plan  *structPlan
	specs []ColumnSpec
}

// RowTypeFor builds a RowType for Go struct type T.
// It applies the same structural checks as LoadYAML[T] (must be a struct with no two
// fields sharing a column name).
func RowTypeFor[T any]() (*RowType, error) {
	return RowTypeOf(reflect.TypeFor[T]())
}

// RowTypeOf builds a RowType for Go struct type t.
func RowTypeOf(t reflect.Type) (*RowType, error) {
	plan, err := buildYAMLPlan(t)
	if err != nil {
		return nil, err
	}
	specs := make([]ColumnSpec, len(plan.fields))
	for i, fb := range plan.fields {
		kind, elemKind, nullable, err := classifyGoType(fb.typ)
		if err != nil {
			return nil, fmt.Errorf("datatable: %v field %q: %w", t, fb.name, err)
		}
		specs[i] = ColumnSpec{
			Name:     fb.name,
			Kind:     kind,
			ElemKind: elemKind,
			Nullable: nullable,
			Type:     fb.typ,
		}
	}
	return &RowType{
		typ:   t,
		plan:  plan,
		specs: specs,
	}, nil
}

func classifyGoType(t reflect.Type) (kind, elemKind ValueKind, nullable bool, err error) {
	if t.Kind() == reflect.Pointer {
		k, ek, _, err := classifyGoType(t.Elem())
		return k, ek, true, err
	}
	if t.Kind() == reflect.Slice {
		ek, _, _, err := classifyGoType(t.Elem())
		if err != nil {
			return KindList, KindNull, false, err
		}
		return KindList, ek, false, nil
	}
	switch t.Kind() {
	case reflect.String:
		return KindString, KindNull, false, nil
	case reflect.Bool:
		return KindBool, KindNull, false, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if t == durationType {
			return KindDuration, KindNull, false, nil
		}
		return KindNumber, KindNull, false, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return KindNumber, KindNull, false, nil
	case reflect.Float32, reflect.Float64:
		return KindNumber, KindNull, false, nil
	default:
		return KindString, KindNull, false, fmt.Errorf("unsupported field type %s", t)
	}
}

// Name returns the Go type name of rt.
func (rt *RowType) Name() string {
	if rt == nil || rt.typ == nil {
		return ""
	}
	if name := rt.typ.Name(); name != "" {
		return name
	}
	return rt.typ.String()
}

// Type returns the underlying Go reflect.Type of rt.
func (rt *RowType) Type() reflect.Type {
	if rt == nil {
		return nil
	}
	return rt.typ
}

// Columns returns the row type's columns in struct declaration order.
func (rt *RowType) Columns() []ColumnSpec {
	if rt == nil {
		return nil
	}
	out := make([]ColumnSpec, len(rt.specs))
	copy(out, rt.specs)
	return out
}

// Column looks up col by exact name first and then case-insensitively, as LoadYAML does.
func (rt *RowType) Column(col string) (ColumnSpec, bool) {
	if rt == nil {
		return ColumnSpec{}, false
	}
	fb, ok := rt.plan.lookup(col)
	if !ok {
		return ColumnSpec{}, false
	}
	for _, s := range rt.specs {
		if s.Name == fb.name {
			return s, true
		}
	}
	return ColumnSpec{}, false
}

// ValidateCell validates a single cell value against rt using LoadYAML's conversion rules.
// row is 0-based; CellError reports 1-based Row.
func (rt *RowType) ValidateCell(filename string, row int, field string, val Value) error {
	if rt == nil {
		return nil
	}
	fb, ok := rt.plan.lookup(field)
	if !ok {
		return &CellError{
			File:  filename,
			Row:   row + 1,
			Field: field,
			Value: val.Display(),
			Err:   ErrUnknownField,
		}
	}
	node, err := yamlNodeFromCellValue(val)
	if err != nil {
		return &CellError{
			File:  filename,
			Row:   row + 1,
			Field: field,
			Value: val.Display(),
			Err:   err,
		}
	}
	fv := reflect.New(fb.typ).Elem()
	if err := assignFromYAMLNode(fv, node, 0); err != nil {
		return &CellError{
			File:  filename,
			Row:   row + 1,
			Field: field,
			Value: formatYAMLNode(node),
			Err:   err,
		}
	}
	return nil
}

// ParseCellText parses raw grid text for field according to rt and validates it with
// LoadYAML's rules, returning the typed Value or a *CellError.
func (rt *RowType) ParseCellText(filename string, row int, field, raw string) (Value, error) {
	if rt == nil {
		return InferValue(raw), nil
	}
	spec, ok := rt.Column(field)
	if !ok {
		return Value{}, &CellError{
			File:  filename,
			Row:   row + 1,
			Field: field,
			Value: raw,
			Err:   ErrUnknownField,
		}
	}
	if spec.Nullable {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.EqualFold(trimmed, "null") {
			return NullValue(), nil
		}
	}
	val, err := parseValueWithElem(spec.Kind, spec.ElemKind, raw)
	if err != nil {
		// Run through ValidateCell on the raw string so the returned error matches LoadYAML's *CellError.
		if verr := rt.ValidateCell(filename, row, field, StringValue(raw)); verr != nil {
			return Value{}, verr
		}
		return Value{}, &CellError{
			File:  filename,
			Row:   row + 1,
			Field: field,
			Value: raw,
			Err:   err,
		}
	}
	if err := rt.ValidateCell(filename, row, field, val); err != nil {
		return Value{}, err
	}
	return val, nil
}

// ValidateRow checks every cell in r against rt, refusing unknown fields, duplicate fields
// (including case-insensitive duplicates such as "hp" and "HP"), and values LoadYAML refuses.
func (rt *RowType) ValidateRow(filename string, row int, r Row) error {
	if rt == nil {
		return nil
	}
	seen := make(map[int]string, len(r.Cells))
	for _, c := range r.Cells {
		fb, ok := rt.plan.lookup(c.Key)
		if !ok {
			return &CellError{
				File:  filename,
				Row:   row + 1,
				Field: c.Key,
				Value: c.Value.Display(),
				Err:   ErrUnknownField,
			}
		}
		if prev, dup := seen[fb.pos]; dup {
			return &CellError{
				File:  filename,
				Row:   row + 1,
				Field: c.Key,
				Value: c.Value.Display(),
				Err:   fmt.Errorf("%w: also set as %q", ErrDuplicateField, prev),
			}
		}
		seen[fb.pos] = c.Key
		if err := rt.ValidateCell(filename, row, c.Key, c.Value); err != nil {
			return err
		}
	}
	return nil
}

// ValidateDocument validates every row of doc against rt.
func (rt *RowType) ValidateDocument(filename string, doc *Document) error {
	if rt == nil || doc == nil {
		return nil
	}
	for i, r := range doc.rows {
		if err := rt.ValidateRow(filename, i, r); err != nil {
			return err
		}
	}
	return nil
}

// Registry maps slash-separated file patterns (or basename patterns without '/') to Go
// row types so editors and tools can validate untyped YAML data tables with LoadYAML's rules.
type Registry struct {
	entries []registryEntry
}

type registryEntry struct {
	pattern string
	rowType *RowType
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// RegisterRowType registers Go struct row type T for files matching pattern in r.
func RegisterRowType[T any](r *Registry, pattern string) error {
	if r == nil {
		return errors.New("datatable: nil registry")
	}
	rt, err := RowTypeFor[T]()
	if err != nil {
		return err
	}
	return r.Register(pattern, rt)
}

// Register binds pattern to rt. If pattern is already registered, its binding is replaced.
// Patterns are matched in registration order; a pattern without '/' matches the file's
// basename, and "**" matches zero or more path segments.
func (r *Registry) Register(pattern string, rt *RowType) error {
	if r == nil {
		return errors.New("datatable: nil registry")
	}
	if rt == nil {
		return errors.New("datatable: nil row type")
	}
	norm := normalizePattern(pattern)
	if norm == "" || norm == "." {
		return errors.New("datatable: file pattern must not be empty")
	}
	if err := validatePattern(norm); err != nil {
		return fmt.Errorf("datatable: invalid file pattern %q: %w", pattern, err)
	}
	for i := range r.entries {
		if r.entries[i].pattern == norm {
			r.entries[i].rowType = rt
			return nil
		}
	}
	r.entries = append(r.entries, registryEntry{pattern: norm, rowType: rt})
	return nil
}

// Lookup returns the RowType registered for pattern (after path normalization) and reports whether pattern is registered.
func (r *Registry) Lookup(pattern string) (*RowType, bool) {
	if r == nil {
		return nil, false
	}
	norm := normalizePattern(pattern)
	if norm == "" {
		return nil, false
	}
	for _, e := range r.entries {
		if e.pattern == norm {
			return e.rowType, true
		}
	}
	return nil, false
}

// Match returns the first registered RowType whose pattern matches filePath, or nil.
func (r *Registry) Match(filePath string) *RowType {
	if r == nil || filePath == "" {
		return nil
	}
	for _, e := range r.entries {
		ok, err := MatchPattern(e.pattern, filePath)
		if err != nil {
			panic(fmt.Sprintf("datatable: registered pattern %q failed to match %q: %v", e.pattern, filePath, err))
		}
		if ok {
			return e.rowType
		}
	}
	return nil
}

// MatchPattern reports whether filePath matches pattern.
// Both paths are normalized to forward slashes with leading "./" removed. A pattern with no
// '/' matches path.Base(filePath). A "**" path segment matches zero or more segments.
func MatchPattern(pattern, filePath string) (bool, error) {
	pat := normalizePattern(pattern)
	if pat == "" {
		return false, errors.New("empty pattern")
	}
	if err := validatePattern(pat); err != nil {
		return false, err
	}
	target := normalizePattern(filePath)
	if !strings.Contains(pat, "/") {
		return path.Match(pat, path.Base(target))
	}
	patParts := strings.Split(pat, "/")
	targetParts := strings.Split(target, "/")
	return matchSegments(patParts, targetParts)
}

func validatePattern(norm string) error {
	for _, seg := range strings.Split(norm, "/") {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			return fmt.Errorf("segment %q: %w", seg, err)
		}
	}
	return nil
}

func normalizePattern(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = filepath.ToSlash(p)
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimSpace(p)
	if p == "." || p == "" {
		return ""
	}
	return p
}

func matchSegments(pat, target []string) (bool, error) {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// Skip consecutive "**" segments.
			for len(pat) > 1 && pat[1] == "**" {
				pat = pat[1:]
			}
			if len(pat) == 1 {
				return true, nil
			}
			// Try matching remaining pattern at every suffix of target.
			for i := 0; i <= len(target); i++ {
				ok, err := matchSegments(pat[1:], target[i:])
				if err != nil {
					return false, err
				}
				if ok {
					return true, nil
				}
			}
			return false, nil
		}
		if len(target) == 0 {
			return false, nil
		}
		ok, err := path.Match(pat[0], target[0])
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		pat = pat[1:]
		target = target[1:]
	}
	return len(target) == 0, nil
}
