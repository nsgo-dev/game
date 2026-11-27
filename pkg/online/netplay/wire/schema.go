package wire

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// CurrentVersion is the wire schema YAML format version this package reads and writes.
const CurrentVersion = 1

// SchemaURL is where the JSON Schema of version 1 wire schema files (*.wire.yaml) is published.
const SchemaURL = "https://game.nsgo.dev/schemas/wire.v1.json"

// SchemaComment is the yaml-language-server header line Encode writes at the top of a wire schema file.
const SchemaComment = "# yaml-language-server: $schema=" + SchemaURL

// ByteOrder selects multi-byte wire endianness.
type ByteOrder string

const (
	// OrderBig encodes multi-byte integers and floats most-significant byte first.
	OrderBig ByteOrder = "be"
	// OrderLittle encodes multi-byte integers and floats least-significant byte first.
	OrderLittle ByteOrder = "le"
)

// Schema is the root of a versioned *.wire.yaml document describing binary wire enums, messages, and discriminated unions.
type Schema struct {
	// Version is the schema format version; it must equal CurrentVersion (1).
	Version int `yaml:"version" schema:"required"`
	// Package is the Go package name emitted by cmd/gamewire when -pkg is not specified.
	Package string `yaml:"package" schema:"required,minLength=1"`
	// Order is the default byte order for multi-byte fields in this file ("be" or "le"; defaults to "be" when omitted).
	Order ByteOrder `yaml:"order,omitempty" schema:"enum=be|le"`
	// Imports lists relative paths to sibling *.wire.yaml files in the same package whose enums and messages this schema references.
	Imports []string `yaml:"imports,omitempty"`
	// Enums declares named integer enumerations validated on decode.
	Enums []Enum `yaml:"enums,omitempty"`
	// Messages declares binary wire message layouts.
	Messages []Message `yaml:"messages,omitempty"`
	// Unions declares tag- or nibble-discriminated message envelopes.
	Unions []Union `yaml:"unions,omitempty"`
}

// Enum defines a named integer enumeration on the wire.
type Enum struct {
	// Name is the Go type name of the enum, in PascalCase.
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Doc is an optional comment emitted on the generated Go type.
	Doc string `yaml:"doc,omitempty"`
	// Type is the underlying integer wire type ("u8", "u16", "u32", "u64", or "uvarint").
	Type string `yaml:"type" schema:"required,enum=u8|u16|u32|u64|uvarint"`
	// Order overrides the schema byte order for multi-byte enum types ("be" or "le").
	Order ByteOrder `yaml:"order,omitempty" schema:"enum=be|le"`
	// Values lists the enum's named members and their wire integers.
	Values []EnumValue `yaml:"values" schema:"required,minItems=1"`
}

// EnumValue is one named member of an Enum.
type EnumValue struct {
	// Name is the member name, appended to the enum type name to form the Go constant.
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Value is the unsigned wire integer value of the member.
	Value uint64 `yaml:"value"`
	// Doc is an optional comment emitted on the generated Go constant.
	Doc string `yaml:"doc,omitempty"`
}

// Message defines one binary wire message struct and its ordered sequence of fields.
type Message struct {
	// Name is the Go struct name of the message, in PascalCase.
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Doc is an optional comment emitted on the generated Go struct.
	Doc string `yaml:"doc,omitempty"`
	// Order overrides the schema default byte order for fields of this message ("be" or "le").
	Order ByteOrder `yaml:"order,omitempty" schema:"enum=be|le"`
	// Fields lists the wire fields in exact on-the-wire order.
	Fields []Field `yaml:"fields,omitempty"`
}

// Field defines one field of a Message or MaskSpec.
type Field struct {
	// Name is the field name (snake_case or lowerCamelCase); codegen converts it to an exported Go field name.
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Doc is an optional comment emitted on the generated Go struct field.
	Doc string `yaml:"doc,omitempty"`
	// Type is a built-in wire type ("u8", "u16", "u32", "u64", "i8", "i16", "i32", "i64", "uvarint", "svarint", "bool", "f32", "qfloat", "fixed", "bytes", "string", "array", "bits", "mask") or the name of a declared Enum or Message.
	Type string `yaml:"type" schema:"required,minLength=1"`
	// Order overrides the message/schema byte order for multi-byte primitives, length prefixes, or mask integers ("be" or "le").
	Order ByteOrder `yaml:"order,omitempty" schema:"enum=be|le"`
	// Bits specifies the bit width for "bits" fields (1..64) or the Go integer width (8, 16, 32, or 64) for "uvarint"/"svarint" fields.
	Bits int `yaml:"bits,omitempty" schema:"minimum=1,maximum=64"`
	// Const fixes an unsigned integer, bits, or enum wire field to a single constant value written on encode and verified on decode.
	Const *uint64 `yaml:"const,omitempty"`
	// QFloat configures an integer-quantized float32 field when Type is "qfloat".
	QFloat *QFloatSpec `yaml:"qfloat,omitempty"`
	// Fixed configures an integer-scaled fixed-point float64 field when Type is "fixed".
	Fixed *FixedSpec `yaml:"fixed,omitempty"`
	// Length configures the length encoding for "bytes" and "string" fields ("uvarint", "u8", "u16", "u32", or "rest"; defaults to "uvarint").
	Length string `yaml:"length,omitempty" schema:"enum=uvarint|u8|u16|u32|rest"`
	// MaxLength bounds the decoded byte length of variable-length "bytes" and "string" fields (0 means unbounded).
	MaxLength int `yaml:"maxLength,omitempty" schema:"minimum=0"`
	// Array configures a fixed-size or length-prefixed array field when Type is "array".
	Array *ArraySpec `yaml:"array,omitempty"`
	// Mask configures a presence-bitmask group of optional fields when Type is "mask".
	Mask *MaskSpec `yaml:"mask,omitempty"`
}

// QFloatSpec configures integer-only quantization of a float32 across [Min, Max] into Bits unsigned bits.
type QFloatSpec struct {
	// Min is the finite lower bound mapped to 0.
	Min float64 `yaml:"min"`
	// Max is the finite upper bound mapped to (1<<Bits)-1; it must be strictly greater than Min.
	Max float64 `yaml:"max"`
	// Bits is the quantized bit width, from 1 to 32.
	Bits int `yaml:"bits" schema:"required,minimum=1,maximum=32"`
}

// FixedSpec configures a fixed-point number stored as an integer scaled by Scale.
type FixedSpec struct {
	// Scale is the positive integer scale factor (wire integer equals round(value * Scale)).
	Scale int64 `yaml:"scale" schema:"required,minimum=1"`
	// Type is the underlying integer wire type ("i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "uvarint", or "svarint").
	Type string `yaml:"type" schema:"required,enum=i8|i16|i32|i64|u8|u16|u32|u64|uvarint|svarint"`
	// Order overrides the byte order for multi-byte integer wire types ("be" or "le").
	Order ByteOrder `yaml:"order,omitempty" schema:"enum=be|le"`
}

// ArraySpec configures a fixed-count ([N]T) or length-prefixed ([]T) array field.
type ArraySpec struct {
	// Count fixes the array length to a compile-time constant [Count]T; omit or set 0 for a variable-length slice.
	Count int `yaml:"count,omitempty" schema:"minimum=0"`
	// Length sets the element-count prefix for variable-length arrays ("uvarint", "u8", "u16", or "u32"; defaults to "uvarint").
	Length string `yaml:"length,omitempty" schema:"enum=uvarint|u8|u16|u32"`
	// Order overrides the byte order for a multi-byte Length prefix (and as default for elements when OfOrder is omitted).
	Order ByteOrder `yaml:"order,omitempty" schema:"enum=be|le"`
	// Of is the element wire type: a primitive ("u8", "u16", "u32", "u64", "i8", "i16", "i32", "i64", "uvarint", "svarint", "bool", "f32") or the name of a declared Enum or Message.
	Of string `yaml:"of" schema:"required,minLength=1"`
	// OfOrder overrides the byte order of multi-byte primitive elements ("be" or "le").
	OfOrder ByteOrder `yaml:"ofOrder,omitempty" schema:"enum=be|le"`
	// OfBits sets the Go integer bit width (8, 16, 32, or 64) when Of is "uvarint" or "svarint".
	OfBits int `yaml:"ofBits,omitempty" schema:"minimum=1,maximum=64"`
	// MaxLength bounds the decoded element count of variable-length arrays (0 means unbounded).
	MaxLength int `yaml:"maxLength,omitempty" schema:"minimum=0"`
}

// MaskSpec configures a presence-bitmask group that gates its fields in bit order (bit 0, bit 1, ...).
type MaskSpec struct {
	// Type is the mask integer wire type ("u8", "u16", "u32", or "u64"; defaults to "u8").
	Type string `yaml:"type,omitempty" schema:"enum=u8|u16|u32|u64"`
	// Order overrides the byte order for multi-byte mask types ("be" or "le").
	Order ByteOrder `yaml:"order,omitempty" schema:"enum=be|le"`
	// Delta, when true, generates Diff<Message>(base, cur) and Apply<Message>(dst, delta) helpers comparing float bit patterns and byte slices directly.
	Delta bool `yaml:"delta,omitempty"`
	// Fields lists the gated fields in bit order (index i corresponds to bit 1<<i).
	Fields []Field `yaml:"fields" schema:"required,minItems=1"`
}

// Union defines a discriminated message envelope selecting one of Cases by a leading tag or nibble-split header byte.
type Union struct {
	// Name is the Go struct name of the union envelope, in PascalCase.
	Name string `yaml:"name" schema:"required,minLength=1"`
	// Doc is an optional comment emitted on the generated Go struct.
	Doc string `yaml:"doc,omitempty"`
	// Discriminator configures how the leading tag (and optional nibble-split version) is encoded on the wire.
	Discriminator Discriminator `yaml:"discriminator" schema:"required"`
	// Cases maps discriminator tags to member messages.
	Cases []UnionCase `yaml:"cases" schema:"required,minItems=1"`
}

// Discriminator configures the leading tag encoding of a Union.
type Discriminator struct {
	// Type selects the discriminator wire encoding: "u8", "u16", "uvarint", or "nibble" (a 1-byte high/low bit split).
	Type string `yaml:"type" schema:"required,enum=u8|u16|uvarint|nibble"`
	// Order overrides the byte order when Type is "u16" ("be" or "le").
	Order ByteOrder `yaml:"order,omitempty" schema:"enum=be|le"`
	// HighBits is the width in bits (1..7) of the high field when Type is "nibble" (defaults to 4).
	HighBits int `yaml:"highBits,omitempty" schema:"minimum=1,maximum=7"`
	// HighName is the Go field name for the high bits when Type is "nibble" (defaults to "version").
	HighName string `yaml:"highName,omitempty"`
	// HighValue is the expected constant value of the high bits when Type is "nibble" (for example, 2 for replication v2).
	HighValue uint8 `yaml:"highValue,omitempty"`
	// LowName is the logical name of the low tag bits when Type is "nibble" (defaults to "type").
	LowName string `yaml:"lowName,omitempty"`
}

// UnionCase binds one discriminator tag value to a Message inside a Union.
type UnionCase struct {
	// Name is the Go field name inside the union struct (defaults to Message when omitted).
	Name string `yaml:"name,omitempty"`
	// Tag is the discriminator tag value selecting this case.
	Tag uint64 `yaml:"tag"`
	// Message is the name of the Message encoded after the discriminator.
	Message string `yaml:"message" schema:"required,minLength=1"`
	// Doc is an optional comment emitted on the generated tag constant.
	Doc string `yaml:"doc,omitempty"`
}

// Parse decodes and validates a *.wire.yaml document, refusing unknown fields, missing or unsupported versions, duplicate keys, and trailing YAML documents.
func Parse(data []byte) (*Schema, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", "write a versioned wire schema YAML document", "wire schema is empty"), "", data, "")
	}
	var root yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&root); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("wire: decode: %w", err), "", data, "check the YAML syntax and field names in the wire schema")
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep one YAML document per wire schema file", "unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("wire: decode: %w", err), "", data, "remove trailing YAML content after the wire schema")
	}
	if err := checkDuplicateKeys(&root, ""); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}

	var s Schema
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	if err := strict.Decode(&s); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("wire: decode: %w", err), "", data, "check the YAML syntax and field names in the wire schema")
	}
	normalizeSchema(&s)
	if err := Validate(&s); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	return &s, nil
}

func checkDuplicateKeys(n *yaml.Node, prefix string) error {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.DocumentNode:
		for _, c := range n.Content {
			if err := checkDuplicateKeys(c, prefix); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		seen := make(map[string]struct{}, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			at := loaderr.Join(prefix, k)
			if _, dup := seen[k]; dup {
				return loaderr.Errorf(at, "remove the duplicate key", "duplicate key %q", k)
			}
			seen[k] = struct{}{}
			if err := checkDuplicateKeys(n.Content[i+1], at); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			if err := checkDuplicateKeys(c, loaderr.Path(prefix, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// LoadFile reads, parses, and validates a *.wire.yaml file from disk.
func LoadFile(filePath string) (*Schema, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, loaderr.Locate(err, filePath, nil, "check that the wire schema file exists and is readable")
	}
	s, err := Parse(data)
	if err != nil {
		return nil, loaderr.WithFile(err, filePath)
	}
	return s, nil
}

// LoadFS reads, parses, and validates a *.wire.yaml file from fsys.
func LoadFS(fsys fs.FS, name string) (*Schema, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, loaderr.Locate(err, name, nil, "check that the wire schema file exists and is readable")
	}
	s, err := Parse(data)
	if err != nil {
		return nil, loaderr.WithFile(err, name)
	}
	return s, nil
}

// Encode validates s and writes it as canonical, deterministic 2-space YAML prefixed with SchemaComment.
func Encode(s *Schema) ([]byte, error) {
	if err := Validate(s); err != nil {
		return nil, err
	}
	cp := cloneSchema(s)
	normalizeSchema(cp)
	var buf bytes.Buffer
	buf.WriteString(SchemaComment)
	buf.WriteByte('\n')
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cp); err != nil {
		return nil, fmt.Errorf("wire: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("wire: encode: %w", err)
	}
	return buf.Bytes(), nil
}

func normalizeSchema(s *Schema) {
	if s == nil {
		return
	}
	for i := range s.Messages {
		normalizeFields(s.Messages[i].Fields)
	}
}

func normalizeFields(fields []Field) {
	for i := range fields {
		if fields[i].QFloat != nil {
			fields[i].QFloat.Min = cleanZero(fields[i].QFloat.Min)
			fields[i].QFloat.Max = cleanZero(fields[i].QFloat.Max)
		}
		if fields[i].Mask != nil {
			normalizeFields(fields[i].Mask.Fields)
		}
	}
}

func cleanZero(v float64) float64 {
	if v == 0 {
		return 0
	}
	return v
}

func cloneSchema(s *Schema) *Schema {
	if s == nil {
		return nil
	}
	cp := *s
	cp.Imports = slices.Clone(s.Imports)
	if len(s.Enums) > 0 {
		cp.Enums = make([]Enum, len(s.Enums))
		for i, e := range s.Enums {
			e.Values = slices.Clone(e.Values)
			cp.Enums[i] = e
		}
	}
	if len(s.Messages) > 0 {
		cp.Messages = make([]Message, len(s.Messages))
		for i, m := range s.Messages {
			m.Fields = cloneFields(m.Fields)
			cp.Messages[i] = m
		}
	}
	if len(s.Unions) > 0 {
		cp.Unions = make([]Union, len(s.Unions))
		for i, u := range s.Unions {
			u.Cases = slices.Clone(u.Cases)
			cp.Unions[i] = u
		}
	}
	return &cp
}

func cloneFields(fields []Field) []Field {
	if len(fields) == 0 {
		return nil
	}
	out := make([]Field, len(fields))
	for i, f := range fields {
		if f.Const != nil {
			v := *f.Const
			f.Const = &v
		}
		if f.QFloat != nil {
			q := *f.QFloat
			f.QFloat = &q
		}
		if f.Fixed != nil {
			fx := *f.Fixed
			f.Fixed = &fx
		}
		if f.Array != nil {
			a := *f.Array
			f.Array = &a
		}
		if f.Mask != nil {
			ms := *f.Mask
			ms.Fields = cloneFields(ms.Fields)
			f.Mask = &ms
		}
		out[i] = f
	}
	return out
}

// Validate checks a single Schema document. When s.Imports is empty, every enum and message reference must resolve within s; when s.Imports is non-empty, cross-file references are verified by ValidateAll or GenerateGo.
func Validate(s *Schema) error {
	return validateSchema(s, nil, len(s.Imports) > 0)
}

// ValidateAll checks a linked set of schemas (such as a directory of *.wire.yaml files in one package), ensuring unique top-level type names across files and that every cross-file type reference resolves.
func ValidateAll(schemas []*Schema) error {
	env := newTypeEnv()
	for i, s := range schemas {
		if s == nil {
			return loaderr.Errorf(fmt.Sprintf("schemas[%d]", i), "provide a non-nil schema", "schema is nil")
		}
		for j, e := range s.Enums {
			if err := env.addEnum(e, loaderr.Path("enums", j)); err != nil {
				return err
			}
		}
		for j, m := range s.Messages {
			if err := env.addMessage(m, loaderr.Path("messages", j)); err != nil {
				return err
			}
		}
		for j, u := range s.Unions {
			if err := env.addUnion(u, loaderr.Path("unions", j)); err != nil {
				return err
			}
		}
	}
	for _, s := range schemas {
		if err := validateSchema(s, env, false); err != nil {
			return err
		}
	}
	return nil
}

type typeEnv struct {
	enums    map[string]Enum
	messages map[string]Message
	unions   map[string]Union
	names    map[string]string
}

func newTypeEnv() *typeEnv {
	return &typeEnv{
		enums:    make(map[string]Enum),
		messages: make(map[string]Message),
		unions:   make(map[string]Union),
		names:    make(map[string]string),
	}
}

func (e *typeEnv) claim(name, kind, at string) error {
	if prev, dup := e.names[name]; dup {
		return loaderr.Errorf(loaderr.Join(at, "name"), "choose a unique type name across enums, messages, and unions", "duplicate type name %q (already declared as %s)", name, prev)
	}
	e.names[name] = kind
	return nil
}

func (e *typeEnv) addEnum(en Enum, at string) error {
	if err := e.claim(en.Name, "enum", at); err != nil {
		return err
	}
	e.enums[en.Name] = en
	return nil
}

func (e *typeEnv) addMessage(m Message, at string) error {
	if err := e.claim(m.Name, "message", at); err != nil {
		return err
	}
	e.messages[m.Name] = m
	return nil
}

func (e *typeEnv) addUnion(u Union, at string) error {
	if err := e.claim(u.Name, "union", at); err != nil {
		return err
	}
	e.unions[u.Name] = u
	return nil
}

func validateSchema(s *Schema, env *typeEnv, allowUnresolved bool) error {
	if s == nil {
		return loaderr.Errorf("", "provide a wire schema", "wire schema is nil")
	}
	if s.Version != CurrentVersion {
		return &loaderr.Error{
			Field:    "version",
			Problem:  fmt.Sprintf("unsupported version %d", s.Version),
			Expected: fmt.Sprint(CurrentVersion),
			Fix:      fmt.Sprintf("set version to %d", CurrentVersion),
		}
	}
	if !isGoIdent(s.Package) {
		return loaderr.Errorf("package", "set package to a valid Go identifier such as wire or netmsg", "invalid package name %q", s.Package)
	}
	if err := validateOrder("order", s.Order); err != nil {
		return err
	}
	seenImports := make(map[string]struct{}, len(s.Imports))
	for i, imp := range s.Imports {
		at := loaderr.Path("imports", i)
		clean := path.Clean(filepath.ToSlash(imp))
		if imp == "" || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") || clean == ".." || clean == "." {
			return loaderr.Errorf(at, "use a relative sibling *.wire.yaml path", "invalid import path %q", imp)
		}
		if _, dup := seenImports[imp]; dup {
			return loaderr.Errorf(at, "remove the duplicate import", "duplicate import %q", imp)
		}
		seenImports[imp] = struct{}{}
	}
	if len(s.Enums) == 0 && len(s.Messages) == 0 && len(s.Unions) == 0 {
		return loaderr.Errorf("", "declare at least one enum, message, or union", "wire schema defines no enums, messages, or unions")
	}

	localEnv := env
	if localEnv == nil {
		localEnv = newTypeEnv()
		for i, en := range s.Enums {
			at := loaderr.Path("enums", i)
			if !isExportedIdent(en.Name) {
				return loaderr.Errorf(loaderr.Join(at, "name"), "use a PascalCase Go identifier such as Channel", "invalid enum name %q", en.Name)
			}
			if err := localEnv.addEnum(en, at); err != nil {
				return err
			}
		}
		for i, m := range s.Messages {
			at := loaderr.Path("messages", i)
			if !isExportedIdent(m.Name) {
				return loaderr.Errorf(loaderr.Join(at, "name"), "use a PascalCase Go identifier such as TransportFrame", "invalid message name %q", m.Name)
			}
			if err := localEnv.addMessage(m, at); err != nil {
				return err
			}
		}
		for i, u := range s.Unions {
			at := loaderr.Path("unions", i)
			if !isExportedIdent(u.Name) {
				return loaderr.Errorf(loaderr.Join(at, "name"), "use a PascalCase Go identifier such as ReplicationMessage", "invalid union name %q", u.Name)
			}
			if err := localEnv.addUnion(u, at); err != nil {
				return err
			}
		}
	}

	for i, en := range s.Enums {
		if err := validateEnum(loaderr.Path("enums", i), en); err != nil {
			return err
		}
	}
	for i, m := range s.Messages {
		if err := validateMessage(loaderr.Path("messages", i), m, localEnv, allowUnresolved); err != nil {
			return err
		}
	}
	for i, u := range s.Unions {
		if err := validateUnion(loaderr.Path("unions", i), u, localEnv, allowUnresolved); err != nil {
			return err
		}
	}
	return nil
}

func validateOrder(at string, order ByteOrder) error {
	switch order {
	case "", OrderBig, OrderLittle:
		return nil
	default:
		return loaderr.Errorf(at, "use \"be\" or \"le\"", "unknown byte order %q", order)
	}
}

func validateEnum(at string, en Enum) error {
	if !isExportedIdent(en.Name) {
		return loaderr.Errorf(loaderr.Join(at, "name"), "use a PascalCase Go identifier", "invalid enum name %q", en.Name)
	}
	maxVal, ok := unsignedTypeMax(en.Type)
	if !ok {
		return loaderr.Errorf(loaderr.Join(at, "type"), "use one of u8, u16, u32, u64, or uvarint", "invalid enum type %q", en.Type)
	}
	if err := validateOrder(loaderr.Join(at, "order"), en.Order); err != nil {
		return err
	}
	if len(en.Values) == 0 {
		return loaderr.Errorf(loaderr.Join(at, "values"), "add at least one enum value", "enum %q has no values", en.Name)
	}
	seenNames := make(map[string]struct{}, len(en.Values))
	seenVals := make(map[uint64]struct{}, len(en.Values))
	for i, v := range en.Values {
		vat := loaderr.Path(loaderr.Join(at, "values"), i)
		if !isFieldIdent(v.Name) {
			return loaderr.Errorf(loaderr.Join(vat, "name"), "use a valid identifier", "invalid enum value name %q", v.Name)
		}
		goName := toExportedIdent(v.Name)
		if _, dup := seenNames[goName]; dup {
			return loaderr.Errorf(loaderr.Join(vat, "name"), "give each enum value a unique name", "duplicate enum value name %q", v.Name)
		}
		seenNames[goName] = struct{}{}
		if v.Value > maxVal {
			return loaderr.Errorf(loaderr.Join(vat, "value"), fmt.Sprintf("use a value between 0 and %d", maxVal), "enum value %d exceeds %s range", v.Value, en.Type)
		}
		if _, dup := seenVals[v.Value]; dup {
			return loaderr.Errorf(loaderr.Join(vat, "value"), "give each enum value a unique integer", "duplicate enum value %d", v.Value)
		}
		seenVals[v.Value] = struct{}{}
	}
	return nil
}

func validateMessage(at string, m Message, env *typeEnv, allowUnresolved bool) error {
	if !isExportedIdent(m.Name) {
		return loaderr.Errorf(loaderr.Join(at, "name"), "use a PascalCase Go identifier", "invalid message name %q", m.Name)
	}
	if err := validateOrder(loaderr.Join(at, "order"), m.Order); err != nil {
		return err
	}
	seenGoNames := make(map[string]struct{}, len(m.Fields))
	maskCount := 0
	for i, f := range m.Fields {
		fat := loaderr.Path(loaderr.Join(at, "fields"), i)
		isLast := i == len(m.Fields)-1
		if f.Type == "mask" {
			maskCount++
			if maskCount > 1 {
				return loaderr.Errorf(loaderr.Join(fat, "type"), "use at most one mask group per message", "message %q defines more than one mask group", m.Name)
			}
		}
		if err := validateField(fat, f, isLast, false, seenGoNames, env, allowUnresolved); err != nil {
			return err
		}
	}
	return nil
}

func validateField(at string, f Field, isLast, inMask bool, seenGoNames map[string]struct{}, env *typeEnv, allowUnresolved bool) error {
	if !isFieldIdent(f.Name) {
		return loaderr.Errorf(loaderr.Join(at, "name"), "use a valid field identifier such as tick or resume_peer", "invalid field name %q", f.Name)
	}
	if err := validateOrder(loaderr.Join(at, "order"), f.Order); err != nil {
		return err
	}
	if f.Type == "mask" {
		if inMask {
			return loaderr.Errorf(loaderr.Join(at, "type"), "remove the nested mask group", "mask groups cannot be nested")
		}
		if f.Mask == nil {
			return loaderr.Errorf(loaderr.Join(at, "mask"), "provide mask settings for a mask field", "mask field requires mask block")
		}
		if f.Const != nil || f.QFloat != nil || f.Fixed != nil || f.Array != nil || f.Bits != 0 || f.Length != "" || f.MaxLength != 0 {
			return loaderr.Errorf(at, "only set name, doc, type, and mask on a mask field", "mask field carries incompatible options")
		}
		maskGoName := toExportedIdent(f.Name) + "Mask"
		if _, dup := seenGoNames[maskGoName]; dup {
			return loaderr.Errorf(loaderr.Join(at, "name"), "choose a unique field name", "field %q conflicts with generated mask field %q", f.Name, maskGoName)
		}
		seenGoNames[maskGoName] = struct{}{}
		return validateMask(loaderr.Join(at, "mask"), f.Mask, seenGoNames, env, allowUnresolved)
	}
	if f.Mask != nil {
		return loaderr.Errorf(loaderr.Join(at, "mask"), "remove mask unless type is \"mask\"", "mask block is only valid for type \"mask\"")
	}

	goName := toExportedIdent(f.Name)
	if _, dup := seenGoNames[goName]; dup {
		return loaderr.Errorf(loaderr.Join(at, "name"), "give every field in the message a unique name", "duplicate field name %q (Go name %s)", f.Name, goName)
	}
	seenGoNames[goName] = struct{}{}

	if f.MaxLength < 0 {
		return loaderr.Errorf(loaderr.Join(at, "maxLength"), "set maxLength to 0 or a positive integer", "negative maxLength %d", f.MaxLength)
	}

	switch f.Type {
	case "u8", "u16", "u32", "u64":
		if f.QFloat != nil || f.Fixed != nil || f.Array != nil || f.Bits != 0 || f.Length != "" || f.MaxLength != 0 {
			return loaderr.Errorf(at, "remove options that do not apply to fixed-width integers", "incompatible options on %s field", f.Type)
		}
		if f.Const != nil {
			maxVal, _ := unsignedTypeMax(f.Type)
			if *f.Const > maxVal {
				return loaderr.Errorf(loaderr.Join(at, "const"), fmt.Sprintf("use a constant in 0..%d", maxVal), "const %d overflows %s", *f.Const, f.Type)
			}
		}
	case "i8", "i16", "i32", "i64":
		if f.Const != nil || f.QFloat != nil || f.Fixed != nil || f.Array != nil || f.Bits != 0 || f.Length != "" || f.MaxLength != 0 {
			return loaderr.Errorf(at, "remove options that do not apply to signed integers", "incompatible options on %s field", f.Type)
		}
	case "uvarint", "svarint":
		if f.QFloat != nil || f.Fixed != nil || f.Array != nil || f.Length != "" || f.MaxLength != 0 || f.Order != "" {
			return loaderr.Errorf(at, "remove options that do not apply to varints", "incompatible options on %s field", f.Type)
		}
		if f.Bits != 0 && f.Bits != 8 && f.Bits != 16 && f.Bits != 32 && f.Bits != 64 {
			return loaderr.Errorf(loaderr.Join(at, "bits"), "set bits to 8, 16, 32, or 64 (or omit for 64)", "invalid varint width %d", f.Bits)
		}
		if f.Const != nil && f.Type == "svarint" {
			return loaderr.Errorf(loaderr.Join(at, "const"), "const is only supported on unsigned integer, bits, and enum fields", "const is not supported on svarint")
		}
		if f.Const != nil && f.Bits > 0 && f.Bits < 64 {
			maxVal := (uint64(1) << f.Bits) - 1
			if *f.Const > maxVal {
				return loaderr.Errorf(loaderr.Join(at, "const"), fmt.Sprintf("use a constant in 0..%d", maxVal), "const %d overflows %d-bit uvarint", *f.Const, f.Bits)
			}
		}
	case "bool":
		if f.Const != nil || f.QFloat != nil || f.Fixed != nil || f.Array != nil || f.Bits != 0 || f.Length != "" || f.MaxLength != 0 || f.Order != "" {
			return loaderr.Errorf(at, "remove options that do not apply to bool", "incompatible options on bool field")
		}
	case "f32":
		if f.Const != nil || f.QFloat != nil || f.Fixed != nil || f.Array != nil || f.Bits != 0 || f.Length != "" || f.MaxLength != 0 {
			return loaderr.Errorf(at, "remove options that do not apply to f32", "incompatible options on f32 field")
		}
	case "qfloat":
		if f.QFloat == nil {
			return loaderr.Errorf(loaderr.Join(at, "qfloat"), "provide qfloat {min, max, bits}", "qfloat field requires qfloat block")
		}
		if f.Const != nil || f.Fixed != nil || f.Array != nil || f.Bits != 0 || f.Length != "" || f.MaxLength != 0 {
			return loaderr.Errorf(at, "remove options that do not apply to qfloat", "incompatible options on qfloat field")
		}
		if err := validateQFloat(loaderr.Join(at, "qfloat"), f.QFloat); err != nil {
			return err
		}
	case "fixed":
		if f.Fixed == nil {
			return loaderr.Errorf(loaderr.Join(at, "fixed"), "provide fixed {scale, type}", "fixed field requires fixed block")
		}
		if f.Const != nil || f.QFloat != nil || f.Array != nil || f.Bits != 0 || f.Length != "" || f.MaxLength != 0 {
			return loaderr.Errorf(at, "remove options that do not apply to fixed", "incompatible options on fixed field")
		}
		if err := validateFixed(loaderr.Join(at, "fixed"), f.Fixed); err != nil {
			return err
		}
	case "bytes", "string":
		if f.Const != nil || f.QFloat != nil || f.Fixed != nil || f.Array != nil || f.Bits != 0 {
			return loaderr.Errorf(at, "remove options that do not apply to "+f.Type, "incompatible options on %s field", f.Type)
		}
		switch f.Length {
		case "", "uvarint", "u8", "u16", "u32":
		case "rest":
			if inMask || !isLast {
				return loaderr.Errorf(loaderr.Join(at, "length"), "use length \"rest\" only on the last top-level field of a message", "length \"rest\" is only valid on the final field of a message")
			}
		default:
			return loaderr.Errorf(loaderr.Join(at, "length"), "use one of uvarint, u8, u16, u32, or rest", "unknown length encoding %q", f.Length)
		}
	case "array":
		if f.Array == nil {
			return loaderr.Errorf(loaderr.Join(at, "array"), "provide array {count|length, of}", "array field requires array block")
		}
		if f.Const != nil || f.QFloat != nil || f.Fixed != nil || f.Bits != 0 || f.Length != "" || f.MaxLength != 0 {
			return loaderr.Errorf(at, "put length, maxLength, and element type inside the array block", "incompatible options on array field")
		}
		if err := validateArray(loaderr.Join(at, "array"), f.Array, env, allowUnresolved); err != nil {
			return err
		}
	case "bits":
		if f.Bits < 1 || f.Bits > 64 {
			return loaderr.Errorf(loaderr.Join(at, "bits"), "set bits to an integer from 1 to 64", "bits width %d out of range [1, 64]", f.Bits)
		}
		if f.QFloat != nil || f.Fixed != nil || f.Array != nil || f.Length != "" || f.MaxLength != 0 || f.Order != "" {
			return loaderr.Errorf(at, "remove options that do not apply to bits", "incompatible options on bits field")
		}
		if f.Const != nil && f.Bits < 64 {
			maxVal := (uint64(1) << f.Bits) - 1
			if *f.Const > maxVal {
				return loaderr.Errorf(loaderr.Join(at, "const"), fmt.Sprintf("use a constant in 0..%d", maxVal), "const %d overflows %d-bit field", *f.Const, f.Bits)
			}
		}
	default:
		if f.QFloat != nil || f.Fixed != nil || f.Array != nil || f.Bits != 0 || f.Length != "" || f.MaxLength != 0 || f.Order != "" {
			return loaderr.Errorf(at, "remove primitive-only options from enum/message field", "incompatible options on %q field", f.Type)
		}
		if en, ok := env.enums[f.Type]; ok {
			if f.Const != nil {
				found := false
				for _, ev := range en.Values {
					if ev.Value == *f.Const {
						found = true
						break
					}
				}
				if !found {
					return loaderr.Errorf(loaderr.Join(at, "const"), "use a value declared by enum "+en.Name, "const %d is not a value of enum %q", *f.Const, en.Name)
				}
			}
			return nil
		}
		if _, ok := env.messages[f.Type]; ok {
			if f.Const != nil {
				return loaderr.Errorf(loaderr.Join(at, "const"), "const is not supported on message fields", "const cannot be set on message field %q", f.Type)
			}
			return nil
		}
		if !allowUnresolved {
			return loaderr.Errorf(loaderr.Join(at, "type"), "use a built-in wire type or a declared enum/message name", "unknown wire type %q", f.Type)
		}
	}
	return nil
}

func validateQFloat(at string, q *QFloatSpec) error {
	if math.IsNaN(q.Min) || math.IsInf(q.Min, 0) || math.Abs(q.Min) > math.MaxFloat32 {
		return loaderr.Errorf(loaderr.Join(at, "min"), "use a finite float32 lower bound", "invalid qfloat min %v", q.Min)
	}
	if math.IsNaN(q.Max) || math.IsInf(q.Max, 0) || math.Abs(q.Max) > math.MaxFloat32 {
		return loaderr.Errorf(loaderr.Join(at, "max"), "use a finite float32 upper bound", "invalid qfloat max %v", q.Max)
	}
	if float32(q.Min) >= float32(q.Max) {
		return loaderr.Errorf(loaderr.Join(at, "max"), "set max strictly greater than min (as float32)", "qfloat max %v is not greater than min %v", q.Max, q.Min)
	}
	if q.Bits < 1 || q.Bits > 32 {
		return loaderr.Errorf(loaderr.Join(at, "bits"), "set bits between 1 and 32", "qfloat bits %d out of range [1, 32]", q.Bits)
	}
	return nil
}

func validateFixed(at string, fx *FixedSpec) error {
	if fx.Scale <= 0 {
		return loaderr.Errorf(loaderr.Join(at, "scale"), "set scale to a positive integer such as 100 or 1000", "fixed scale %d must be positive", fx.Scale)
	}
	switch fx.Type {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "uvarint", "svarint":
	default:
		return loaderr.Errorf(loaderr.Join(at, "type"), "use an integer wire type (i8..i64, u8..u64, uvarint, svarint)", "invalid fixed wire type %q", fx.Type)
	}
	return validateOrder(loaderr.Join(at, "order"), fx.Order)
}

func validateArray(at string, a *ArraySpec, env *typeEnv, allowUnresolved bool) error {
	if a.Count < 0 {
		return loaderr.Errorf(loaderr.Join(at, "count"), "set count to 0 or a positive integer", "negative array count %d", a.Count)
	}
	if a.Count > 0 && a.Length != "" {
		return loaderr.Errorf(loaderr.Join(at, "length"), "omit length when count is set", "fixed-count array cannot also specify length")
	}
	if a.Count > 0 && a.MaxLength != 0 {
		return loaderr.Errorf(loaderr.Join(at, "maxLength"), "omit maxLength when count is set", "fixed-count array cannot specify maxLength")
	}
	if a.MaxLength < 0 {
		return loaderr.Errorf(loaderr.Join(at, "maxLength"), "set maxLength to 0 or a positive integer", "negative maxLength %d", a.MaxLength)
	}
	switch a.Length {
	case "", "uvarint", "u8", "u16", "u32":
	default:
		return loaderr.Errorf(loaderr.Join(at, "length"), "use one of uvarint, u8, u16, or u32", "invalid array length type %q", a.Length)
	}
	if err := validateOrder(loaderr.Join(at, "order"), a.Order); err != nil {
		return err
	}
	if err := validateOrder(loaderr.Join(at, "ofOrder"), a.OfOrder); err != nil {
		return err
	}
	if a.OfBits != 0 && a.Of != "uvarint" && a.Of != "svarint" {
		return loaderr.Errorf(loaderr.Join(at, "ofBits"), "only set ofBits when of is uvarint or svarint", "ofBits is not valid for element type %q", a.Of)
	}
	if a.OfBits != 0 && a.OfBits != 8 && a.OfBits != 16 && a.OfBits != 32 && a.OfBits != 64 {
		return loaderr.Errorf(loaderr.Join(at, "ofBits"), "set ofBits to 8, 16, 32, or 64", "invalid ofBits %d", a.OfBits)
	}
	switch a.Of {
	case "u8", "u16", "u32", "u64", "i8", "i16", "i32", "i64", "uvarint", "svarint", "bool", "f32":
		return nil
	default:
		if _, ok := env.enums[a.Of]; ok {
			return nil
		}
		if _, ok := env.messages[a.Of]; ok {
			return nil
		}
		if !allowUnresolved {
			return loaderr.Errorf(loaderr.Join(at, "of"), "use a scalar primitive or a declared enum/message name", "unknown array element type %q", a.Of)
		}
	}
	return nil
}

func validateMask(at string, ms *MaskSpec, seenGoNames map[string]struct{}, env *typeEnv, allowUnresolved bool) error {
	if err := validateOrder(loaderr.Join(at, "order"), ms.Order); err != nil {
		return err
	}
	maskBits, ok := maskTypeBits(ms.Type)
	if !ok {
		return loaderr.Errorf(loaderr.Join(at, "type"), "use one of u8, u16, u32, or u64", "invalid mask type %q", ms.Type)
	}
	if len(ms.Fields) == 0 {
		return loaderr.Errorf(loaderr.Join(at, "fields"), "add at least one gated field", "mask group has no fields")
	}
	if len(ms.Fields) > maskBits {
		return loaderr.Errorf(loaderr.Join(at, "fields"), fmt.Sprintf("use a wider mask type or at most %d fields", maskBits), "mask group has %d fields, exceeding %d bits", len(ms.Fields), maskBits)
	}
	for i, f := range ms.Fields {
		fat := loaderr.Path(loaderr.Join(at, "fields"), i)
		if f.Const != nil {
			return loaderr.Errorf(loaderr.Join(fat, "const"), "remove const from fields inside a mask group", "const fields are not allowed inside a mask group")
		}
		if err := validateField(fat, f, false, true, seenGoNames, env, allowUnresolved); err != nil {
			return err
		}
	}
	return nil
}

func validateUnion(at string, u Union, env *typeEnv, allowUnresolved bool) error {
	if !isExportedIdent(u.Name) {
		return loaderr.Errorf(loaderr.Join(at, "name"), "use a PascalCase Go identifier", "invalid union name %q", u.Name)
	}
	d := u.Discriminator
	dat := loaderr.Join(at, "discriminator")
	if err := validateOrder(loaderr.Join(dat, "order"), d.Order); err != nil {
		return err
	}
	var maxTag uint64
	switch d.Type {
	case "u8":
		maxTag = math.MaxUint8
	case "u16":
		maxTag = math.MaxUint16
	case "uvarint":
		maxTag = math.MaxUint64
	case "nibble":
		hb := d.HighBits
		if hb == 0 {
			hb = 4
		}
		if hb < 1 || hb > 7 {
			return loaderr.Errorf(loaderr.Join(dat, "highBits"), "set highBits between 1 and 7", "invalid nibble highBits %d", d.HighBits)
		}
		if uint64(d.HighValue) >= (uint64(1) << hb) {
			return loaderr.Errorf(loaderr.Join(dat, "highValue"), fmt.Sprintf("use a highValue below %d", 1<<hb), "highValue %d overflows %d bits", d.HighValue, hb)
		}
		if d.HighName != "" && !isFieldIdent(d.HighName) {
			return loaderr.Errorf(loaderr.Join(dat, "highName"), "use a valid field identifier", "invalid highName %q", d.HighName)
		}
		if d.LowName != "" && !isFieldIdent(d.LowName) {
			return loaderr.Errorf(loaderr.Join(dat, "lowName"), "use a valid field identifier", "invalid lowName %q", d.LowName)
		}
		maxTag = (uint64(1) << (8 - hb)) - 1
	default:
		return loaderr.Errorf(loaderr.Join(dat, "type"), "use one of u8, u16, uvarint, or nibble", "invalid discriminator type %q", d.Type)
	}
	if d.Type != "nibble" && (d.HighBits != 0 || d.HighName != "" || d.HighValue != 0 || d.LowName != "") {
		return loaderr.Errorf(dat, "remove nibble-only fields when discriminator type is not nibble", "nibble discriminator options set on %q discriminator", d.Type)
	}

	if len(u.Cases) == 0 {
		return loaderr.Errorf(loaderr.Join(at, "cases"), "add at least one union case", "union %q has no cases", u.Name)
	}
	seenTags := make(map[uint64]struct{}, len(u.Cases))
	seenNames := make(map[string]struct{}, len(u.Cases))
	for i, c := range u.Cases {
		cat := loaderr.Path(loaderr.Join(at, "cases"), i)
		if c.Tag > maxTag {
			return loaderr.Errorf(loaderr.Join(cat, "tag"), fmt.Sprintf("use a tag in 0..%d", maxTag), "case tag %d exceeds discriminator range", c.Tag)
		}
		if _, dup := seenTags[c.Tag]; dup {
			return loaderr.Errorf(loaderr.Join(cat, "tag"), "give each case a unique tag", "duplicate union case tag %d", c.Tag)
		}
		seenTags[c.Tag] = struct{}{}
		caseName := c.Name
		if caseName == "" {
			caseName = c.Message
		}
		if !isFieldIdent(caseName) {
			return loaderr.Errorf(loaderr.Join(cat, "name"), "use a valid identifier", "invalid union case name %q", caseName)
		}
		goCaseName := toExportedIdent(caseName)
		if goCaseName == "Tag" || (d.Type == "nibble" && goCaseName == toExportedIdent(nibbleHighName(d))) {
			return loaderr.Errorf(loaderr.Join(cat, "name"), "choose a case name distinct from discriminator fields", "union case name %q collides with discriminator field", caseName)
		}
		if _, dup := seenNames[goCaseName]; dup {
			return loaderr.Errorf(loaderr.Join(cat, "name"), "give each case a unique name", "duplicate union case name %q", caseName)
		}
		seenNames[goCaseName] = struct{}{}
		if _, ok := env.messages[c.Message]; !ok && !allowUnresolved {
			return loaderr.Errorf(loaderr.Join(cat, "message"), "reference a declared message name", "unknown message %q in union %q", c.Message, u.Name)
		}
	}
	return nil
}

func unsignedTypeMax(t string) (uint64, bool) {
	switch t {
	case "u8":
		return math.MaxUint8, true
	case "u16":
		return math.MaxUint16, true
	case "u32":
		return math.MaxUint32, true
	case "u64", "uvarint":
		return math.MaxUint64, true
	default:
		return 0, false
	}
}

func maskTypeBits(t string) (int, bool) {
	switch t {
	case "", "u8":
		return 8, true
	case "u16":
		return 16, true
	case "u32":
		return 32, true
	case "u64":
		return 64, true
	default:
		return 0, false
	}
}

func nibbleHighBits(d Discriminator) int {
	if d.HighBits == 0 {
		return 4
	}
	return d.HighBits
}

func nibbleHighName(d Discriminator) string {
	if d.HighName == "" {
		return "version"
	}
	return d.HighName
}

func isGoIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return false
			}
		}
	}
	return true
}

func isExportedIdent(s string) bool {
	if !isGoIdent(s) {
		return false
	}
	r := []rune(s)[0]
	return unicode.IsUpper(r)
}

func isFieldIdent(s string) bool {
	return isGoIdent(s)
}

// toExportedIdent converts snake_case or lowerCamelCase schema names into exported PascalCase Go identifiers, preserving common acronyms like ID, UDP, VX, VY.
func toExportedIdent(s string) string {
	if s == "" {
		return ""
	}
	if !strings.ContainsRune(s, '_') {
		// Preserve already-PascalCase names like "UDPSealedDatagram", "VX", "ID".
		runes := []rune(s)
		if unicode.IsUpper(runes[0]) {
			return s
		}
		if up, ok := commonAcronyms[strings.ToLower(s)]; ok {
			return up
		}
		runes[0] = unicode.ToUpper(runes[0])
		return string(runes)
	}
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		if up, ok := commonAcronyms[strings.ToLower(p)]; ok {
			b.WriteString(up)
			continue
		}
		runes := []rune(p)
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	return b.String()
}

var commonAcronyms = map[string]string{
	"id":  "ID",
	"ids": "IDs",
	"udp": "UDP",
	"tcp": "TCP",
	"ws":  "WS",
	"vx":  "VX",
	"vy":  "VY",
	"vz":  "VZ",
}
