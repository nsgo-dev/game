package flow

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/pkg/loaderr"
)

// VarsFileName is the conventional name of a project's shared variable declaration file.
const VarsFileName = "vars.yaml"

// VarsFormatVersion is the schema version of vars.yaml that [ParseVars] reads and [EncodeVars] writes.
const VarsFormatVersion = 1

// VarsSchemaURL is where the JSON Schema of version 1 variable declaration files (vars.yaml) is published.
const VarsSchemaURL = "https://game.nsgo.dev/schemas/vars.v1.json"

var (
	// ErrInvalidVars reports that a vars.yaml document breaks the schema: a wrong version,
	// a missing or duplicate name, an invalid kind, a mismatched default, or invalid bounds.
	ErrInvalidVars = errors.New("flow: invalid vars")
	// ErrUnknownVar reports accessing or mutating a variable name that was not declared.
	ErrUnknownVar = errors.New("flow: unknown variable")
	// ErrVarTypeMismatch reports reading or writing a variable with a Go type that does not match its declared VarKind.
	ErrVarTypeMismatch = errors.New("flow: variable type mismatch")
	// ErrVarOutOfRange reports setting a numeric variable outside its declared [Min, Max] bounds.
	ErrVarOutOfRange = errors.New("flow: variable out of range")
	// ErrUndeclaredVar reports a dialogue script <<declare>> or <<set>> naming a variable absent from the dialogue.Options.Vars store, or a [Vars.SetFromExpr] call naming an undeclared variable.
	ErrUndeclaredVar = errors.New("flow: undeclared variable")
)

// VarKind is the declared value type of a project-wide game variable or switch.
type VarKind string

const (
	// VarBool is a boolean switch (default false).
	VarBool VarKind = "bool"
	// VarInt is a signed integer variable (default 0).
	VarInt VarKind = "int"
	// VarFloat is a finite float64 variable (default 0).
	VarFloat VarKind = "float"
	// VarString is a string variable (default "").
	VarString VarKind = "string"
)

// Valid reports whether k is one of VarBool, VarInt, VarFloat, or VarString.
func (k VarKind) Valid() bool {
	switch k {
	case VarBool, VarInt, VarFloat, VarString:
		return true
	}
	return false
}

// VarsDoc is the root of a versioned vars.yaml declaration file.
type VarsDoc struct {
	// Version is the schema version; it must equal VarsFormatVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Vars declares the project's shared game variables and switches in declaration order.
	Vars []VarDecl `yaml:"vars,omitempty"`
}

// VarDecl declares one project-wide game variable or switch.
type VarDecl struct {
	// Name uniquely identifies the variable; it must start with a letter or '_' followed by letters, digits, or '_'.
	Name string `yaml:"name" schema:"required"`
	// Kind is the variable's declared type: "bool", "int", "float", or "string".
	Kind VarKind `yaml:"kind" schema:"required,enum=bool|int|float|string"`
	// Default is the variable's initial value when omitted or reset; nil uses the zero value for Kind (false, 0, 0.0, or "").
	Default any `yaml:"default,omitempty"`
	// Min is an optional inclusive lower bound for "int" and "float" variables.
	Min *float64 `yaml:"min,omitempty"`
	// Max is an optional inclusive upper bound for "int" and "float" variables.
	Max *float64 `yaml:"max,omitempty"`
	// Doc is an optional description for editors, inspectors, and designers.
	Doc string `yaml:"doc,omitempty"`
}

// DefaultValue returns the typed initial value of d (bool, int, float64, or string),
// filling in the zero value for d.Kind when d.Default is nil.
func (d VarDecl) DefaultValue() any {
	if d.Default != nil {
		switch d.Kind {
		case VarBool:
			if v, ok := d.Default.(bool); ok {
				return v
			}
		case VarInt:
			if v, ok := toExactInt(d.Default); ok {
				return v
			}
		case VarFloat:
			if v, ok := toFiniteFloat(d.Default); ok {
				return v
			}
		case VarString:
			if v, ok := d.Default.(string); ok {
				return v
			}
		}
	}
	switch d.Kind {
	case VarBool:
		return false
	case VarInt:
		return 0
	case VarFloat:
		return 0.0
	case VarString:
		return ""
	default:
		return nil
	}
}

// Has reports whether d declares a variable named name (optional leading '$' ignored).
func (d *VarsDoc) Has(name string) bool {
	_, ok := d.Var(name)
	return ok
}

// Var returns a copy of the declaration for name (optional leading '$' ignored), if declared.
func (d *VarsDoc) Var(name string) (VarDecl, bool) {
	if d == nil {
		return VarDecl{}, false
	}
	key := normalizeVarName(name)
	for _, decl := range d.Vars {
		if decl.Name == key {
			return cloneVarDecl(decl), true
		}
	}
	return VarDecl{}, false
}

const fixVars = "write a vars.yaml mapping with version: 1 and a vars list; docs/en/flow.md documents the format"

// ParseVars decodes and validates a versioned vars.yaml document.
// Unknown fields, duplicate keys, missing or unsupported versions, and trailing YAML documents are refused.
// Every error is a *loaderr.Error pointing to the offending field and line with a fix; a caller that read data from a file names it with loaderr.WithFile.
func ParseVars(data []byte) (*VarsDoc, error) {
	doc, err := parseVars(data)
	if err != nil {
		return nil, loaderr.Locate(err, "", data, fixVars)
	}
	return doc, nil
}

func parseVars(data []byte) (*VarsDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		e := loaderr.Errorf("", fmt.Sprintf("add version: %d and a vars list", VarsFormatVersion), "%w: empty document", ErrInvalidVars)
		e.Line = 1
		return nil, e
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc VarsDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Errorf("", "keep a single YAML document in the file; remove the trailing --- document", "%w: unexpected trailing YAML document", ErrInvalidVars)
		}
		return nil, err
	}
	if err := ValidateVars(&doc); err != nil {
		return nil, err
	}
	normalizeVarsDoc(&doc)
	return &doc, nil
}

// LoadVarsFS reads, decodes, and validates the vars.yaml file at name in fsys.
// Errors are *loaderr.Error values naming name.
func LoadVarsFS(fsys fs.FS, name string) (*VarsDoc, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, loaderr.Locate(fmt.Errorf("flow: read %s: %w", name, err), name, nil, "check that the vars.yaml file exists and is readable")
	}
	doc, err := ParseVars(data)
	if err != nil {
		return nil, loaderr.WithFile(err, name)
	}
	return doc, nil
}

// ValidateVars checks doc against the vars.yaml schema: version, unique valid identifier names,
// valid kinds, defaults matching their declared kind, valid numeric bounds, and defaults within bounds.
// Errors are *loaderr.Error values wrapping ErrInvalidVars.
func ValidateVars(doc *VarsDoc) error {
	if doc == nil {
		return loaderr.Errorf("", "pass a non-nil *VarsDoc", "%w: nil vars document", ErrInvalidVars)
	}
	if doc.Version != VarsFormatVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d", VarsFormatVersion), "%w: unsupported version %d (want %d)", ErrInvalidVars, doc.Version, VarsFormatVersion)
	}
	seen := make(map[string]struct{}, len(doc.Vars))
	for i := range doc.Vars {
		v := &doc.Vars[i]
		base := loaderr.Path("vars", i)
		if err := validateVarName(v.Name); err != nil {
			return loaderr.Errorf(loaderr.Join(base, "name"), "use a valid identifier starting with a letter or '_' followed by letters, digits, or '_'", "%w: %s", ErrInvalidVars, err)
		}
		if _, dup := seen[v.Name]; dup {
			return loaderr.Errorf(loaderr.Join(base, "name"), "give each declared variable a unique name", "%w: duplicate variable %q", ErrInvalidVars, v.Name)
		}
		seen[v.Name] = struct{}{}
		if !v.Kind.Valid() {
			return loaderr.Errorf(loaderr.Join(base, "kind"), "set kind to bool, int, float, or string", "%w: variable %q has unknown kind %q (want bool, int, float, or string)", ErrInvalidVars, v.Name, v.Kind)
		}
		if err := validateVarBounds(base, v); err != nil {
			return err
		}
		if err := validateVarDefault(base, v); err != nil {
			return err
		}
	}
	return nil
}

func validateVarName(name string) error {
	if name == "" {
		return errors.New("empty variable name")
	}
	if strings.HasPrefix(name, "$") {
		return fmt.Errorf("variable name %q must not include leading '$'", name)
	}
	if !isValidVarName(name) {
		return fmt.Errorf("invalid variable name %q", name)
	}
	return nil
}

func isValidVarName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); {
		r, size := utf8.DecodeRuneInString(name[i:])
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		} else if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
		i += size
	}
	return true
}

func validateVarBounds(base string, v *VarDecl) error {
	if v.Kind != VarInt && v.Kind != VarFloat {
		if v.Min != nil {
			return loaderr.Errorf(loaderr.Join(base, "min"), "remove min; only int and float variables support range bounds", "%w: variable %q of kind %s cannot set min", ErrInvalidVars, v.Name, v.Kind)
		}
		if v.Max != nil {
			return loaderr.Errorf(loaderr.Join(base, "max"), "remove max; only int and float variables support range bounds", "%w: variable %q of kind %s cannot set max", ErrInvalidVars, v.Name, v.Kind)
		}
		return nil
	}
	if v.Min != nil {
		if err := validateBoundNumber(*v.Min, v.Kind); err != nil {
			return loaderr.Errorf(loaderr.Join(base, "min"), "set min to a valid finite number for this variable's kind", "%w: variable %q min: %s", ErrInvalidVars, v.Name, err)
		}
	}
	if v.Max != nil {
		if err := validateBoundNumber(*v.Max, v.Kind); err != nil {
			return loaderr.Errorf(loaderr.Join(base, "max"), "set max to a valid finite number for this variable's kind", "%w: variable %q max: %s", ErrInvalidVars, v.Name, err)
		}
	}
	if v.Min != nil && v.Max != nil && *v.Min > *v.Max {
		return loaderr.Errorf(loaderr.Join(base, "max"), "set max to a value greater than or equal to min", "%w: variable %q has min (%g) > max (%g)", ErrInvalidVars, v.Name, *v.Min, *v.Max)
	}
	return nil
}

func validateBoundNumber(n float64, kind VarKind) error {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return fmt.Errorf("must be finite, got %v", n)
	}
	if kind == VarInt {
		if math.Trunc(n) != n {
			return fmt.Errorf("int bound must be a whole number, got %g", n)
		}
		if n < float64(math.MinInt) || n > float64(math.MaxInt) {
			return fmt.Errorf("int bound %g overflows int", n)
		}
	}
	return nil
}

func validateVarDefault(base string, v *VarDecl) error {
	defField := loaderr.Join(base, "default")
	switch v.Kind {
	case VarBool:
		if v.Default != nil {
			if _, ok := v.Default.(bool); !ok {
				return loaderr.Errorf(defField, "set default to true or false", "%w: variable %q default must be bool, got %T", ErrInvalidVars, v.Name, v.Default)
			}
		}
	case VarString:
		if v.Default != nil {
			if _, ok := v.Default.(string); !ok {
				return loaderr.Errorf(defField, "set default to a string", "%w: variable %q default must be string, got %T", ErrInvalidVars, v.Name, v.Default)
			}
		}
	case VarInt:
		eff := 0
		if v.Default != nil {
			iv, ok := toExactInt(v.Default)
			if !ok {
				return loaderr.Errorf(defField, "set default to a whole integer", "%w: variable %q default must be int, got %T (%v)", ErrInvalidVars, v.Name, v.Default, v.Default)
			}
			eff = iv
		}
		if err := checkNumericBounds(v.Name, float64(eff), v.Min, v.Max); err != nil {
			target := defField
			if v.Default == nil {
				target = base
			}
			return loaderr.Errorf(target, "set default within [min, max] or widen the bounds", "%w: %s", ErrInvalidVars, err)
		}
	case VarFloat:
		eff := 0.0
		if v.Default != nil {
			fv, ok := toFiniteFloat(v.Default)
			if !ok {
				return loaderr.Errorf(defField, "set default to a finite number", "%w: variable %q default must be a finite float, got %T (%v)", ErrInvalidVars, v.Name, v.Default, v.Default)
			}
			eff = fv
		}
		if err := checkNumericBounds(v.Name, eff, v.Min, v.Max); err != nil {
			target := defField
			if v.Default == nil {
				target = base
			}
			return loaderr.Errorf(target, "set default within [min, max] or widen the bounds", "%w: %s", ErrInvalidVars, err)
		}
	}
	return nil
}

func checkNumericBounds(name string, val float64, minVal, maxVal *float64) error {
	if minVal != nil && val < *minVal {
		return fmt.Errorf("%w: variable %q value %g is less than min %g", ErrVarOutOfRange, name, val, *minVal)
	}
	if maxVal != nil && val > *maxVal {
		return fmt.Errorf("%w: variable %q value %g is greater than max %g", ErrVarOutOfRange, name, val, *maxVal)
	}
	return nil
}

// EncodeVars validates doc and writes it in canonical deterministic YAML form:
// 2-space indentation, fields in declaration order, zero defaults and empty lists omitted.
func EncodeVars(doc *VarsDoc) ([]byte, error) {
	if err := ValidateVars(doc); err != nil {
		return nil, err
	}
	out := cloneVarsDoc(doc)
	normalizeVarsDoc(out)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("flow: encode vars: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("flow: encode vars: %w", err)
	}
	return buf.Bytes(), nil
}

func cloneVarsDoc(doc *VarsDoc) *VarsDoc {
	if doc == nil {
		return nil
	}
	cp := &VarsDoc{Version: doc.Version}
	if len(doc.Vars) > 0 {
		cp.Vars = make([]VarDecl, len(doc.Vars))
		for i, d := range doc.Vars {
			cp.Vars[i] = cloneVarDecl(d)
		}
	}
	return cp
}

func cloneVarDecl(d VarDecl) VarDecl {
	cp := d
	if d.Min != nil {
		m := *d.Min
		cp.Min = &m
	}
	if d.Max != nil {
		m := *d.Max
		cp.Max = &m
	}
	return cp
}

// normalizeVarsDoc turns empty lists and zero defaults into nil so ParseVars(EncodeVars(doc)) equals doc.
func normalizeVarsDoc(doc *VarsDoc) {
	if len(doc.Vars) == 0 {
		doc.Vars = nil
		return
	}
	for i := range doc.Vars {
		v := &doc.Vars[i]
		if v.Min != nil && *v.Min == 0 {
			z := 0.0
			v.Min = &z
		}
		if v.Max != nil && *v.Max == 0 {
			z := 0.0
			v.Max = &z
		}
		if v.Default == nil {
			continue
		}
		switch v.Kind {
		case VarBool:
			if b, ok := v.Default.(bool); ok && !b {
				v.Default = nil
			}
		case VarInt:
			if iv, ok := toExactInt(v.Default); ok {
				if iv == 0 {
					v.Default = nil
				} else {
					v.Default = iv
				}
			}
		case VarFloat:
			if fv, ok := toFiniteFloat(v.Default); ok {
				if fv == 0 {
					v.Default = nil
				} else {
					v.Default = fv
				}
			}
		case VarString:
			if s, ok := v.Default.(string); ok && s == "" {
				v.Default = nil
			}
		}
	}
}

func toExactInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int8:
		return int(x), true
	case int16:
		return int(x), true
	case int32:
		return int(x), true
	case int64:
		if x < int64(math.MinInt) || x > int64(math.MaxInt) {
			return 0, false
		}
		return int(x), true
	case uint:
		if x > uint(math.MaxInt) {
			return 0, false
		}
		return int(x), true
	case uint8:
		return int(x), true
	case uint16:
		return int(x), true
	case uint32:
		if uint64(x) > uint64(math.MaxInt) {
			return 0, false
		}
		return int(x), true
	case uint64:
		if x > uint64(math.MaxInt) {
			return 0, false
		}
		return int(x), true
	default:
		return 0, false
	}
}

func toFiniteFloat(v any) (float64, bool) {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case float32:
		f = float64(x)
	case int:
		f = float64(x)
	case int8:
		f = float64(x)
	case int16:
		f = float64(x)
	case int32:
		f = float64(x)
	case int64:
		f = float64(x)
	case uint:
		f = float64(x)
	case uint8:
		f = float64(x)
	case uint16:
		f = float64(x)
	case uint32:
		f = float64(x)
	case uint64:
		f = float64(x)
	default:
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	if f == 0 {
		f = 0
	}
	return f, true
}
