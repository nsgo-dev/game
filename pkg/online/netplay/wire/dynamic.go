package wire

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// WireBytes holds raw wire bytes in a dynamically decoded message.
// It marshals as a standard base64 string in both YAML and JSON.
type WireBytes []byte

// MarshalYAML encodes WireBytes as a standard base64 string.
func (b WireBytes) MarshalYAML() (any, error) {
	return base64.StdEncoding.EncodeToString(b), nil
}

// MarshalJSON encodes WireBytes as a standard base64 JSON string.
func (b WireBytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(base64.StdEncoding.EncodeToString(b))
}

// DecodedEnum holds a validated wire enum value together with its declared schema member name.
// It marshals as Name in both YAML and JSON while retaining Value for lossless re-encoding.
type DecodedEnum struct {
	Name  string
	Value uint64
}

// MarshalYAML encodes DecodedEnum as its member name.
func (e DecodedEnum) MarshalYAML() (any, error) {
	return e.Name, nil
}

// MarshalJSON encodes DecodedEnum as its member name.
func (e DecodedEnum) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.Name)
}

// DecodedField is one decoded wire field in exact schema declaration order.
type DecodedField struct {
	Name  string
	Value any
}

// DecodedObject is an ordered list of decoded wire fields that marshals as a YAML mapping or JSON object in wire field order.
type DecodedObject []DecodedField

// Get returns the value of the named field and whether it was present.
func (o DecodedObject) Get(name string) (any, bool) {
	for _, f := range o {
		if f.Name == name {
			return f.Value, true
		}
	}
	return nil, false
}

// Set updates an existing field's value in place or appends a new field.
func (o *DecodedObject) Set(name string, value any) {
	for i := range *o {
		if (*o)[i].Name == name {
			(*o)[i].Value = value
			return
		}
	}
	*o = append(*o, DecodedField{Name: name, Value: value})
}

// Without returns a shallow copy of o omitting the named field.
func (o DecodedObject) Without(name string) DecodedObject {
	out := make(DecodedObject, 0, len(o))
	for _, f := range o {
		if f.Name == name {
			continue
		}
		out = append(out, f)
	}
	return out
}

// MarshalYAML encodes DecodedObject as an ordered YAML mapping.
func (o DecodedObject) MarshalYAML() (any, error) {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, f := range o {
		k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: f.Name}
		var v yaml.Node
		if err := v.Encode(f.Value); err != nil {
			return nil, err
		}
		n.Content = append(n.Content, k, &v)
	}
	return n, nil
}

// MarshalJSON encodes DecodedObject as an ordered JSON object.
func (o DecodedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(f.Name)
		if err != nil {
			return nil, err
		}
		vb, err := json.Marshal(f.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// DecodedMask holds a presence-bitmask group's raw bitmask and present fields in bit order.
type DecodedMask struct {
	Mask   uint64
	Fields DecodedObject
}

// MarshalYAML encodes DecodedMask as a YAML mapping with "mask" followed by its present fields.
func (m DecodedMask) MarshalYAML() (any, error) {
	obj := make(DecodedObject, 0, 1+len(m.Fields))
	obj = append(obj, DecodedField{Name: "mask", Value: m.Mask})
	obj = append(obj, m.Fields...)
	return obj.MarshalYAML()
}

// MarshalJSON encodes DecodedMask as a JSON object with "mask" followed by its present fields.
func (m DecodedMask) MarshalJSON() ([]byte, error) {
	obj := make(DecodedObject, 0, 1+len(m.Fields))
	obj = append(obj, DecodedField{Name: "mask", Value: m.Mask})
	obj = append(obj, m.Fields...)
	return obj.MarshalJSON()
}

// DecodedPayload holds a nested bytes field (such as EntityRecord.props) that was further decoded using a custom wire schema.
type DecodedPayload struct {
	Schema  string        `yaml:"schema" json:"schema"`
	Union   string        `yaml:"union,omitempty" json:"union,omitempty"`
	Case    string        `yaml:"case,omitempty" json:"case,omitempty"`
	Tag     *uint64       `yaml:"tag,omitempty" json:"tag,omitempty"`
	Version *uint8        `yaml:"version,omitempty" json:"version,omitempty"`
	Message string        `yaml:"message" json:"message"`
	Fields  DecodedObject `yaml:"fields,omitempty" json:"fields,omitempty"`
}

// DecodedUnion holds a dynamically decoded union envelope and its selected member message.
type DecodedUnion struct {
	Schema    string
	Union     string
	HasHigh   bool
	HighName  string
	HighValue uint8
	Case      string
	Tag       uint64
	Message   string
	Fields    DecodedObject
}

type schemaEnum struct {
	schema *Schema
	def    Enum
	custom bool
}

type schemaMessage struct {
	schema *Schema
	def    Message
	custom bool
}

type schemaUnion struct {
	schema *Schema
	def    Union
	custom bool
}

// Registry indexes built-in and optional custom wire schemas for schema-driven dynamic decoding and re-encoding.
type Registry struct {
	enums    map[string]schemaEnum
	messages map[string]schemaMessage
	unions   map[string]schemaUnion

	customUnions   []string
	customMessages []string
}

// NewRegistry builds a Registry loaded with all built-in pkg/online/netplay wire schemas plus any caller-supplied custom schemas.
func NewRegistry(custom ...*Schema) (*Registry, error) {
	builtins, err := LoadBuiltins()
	if err != nil {
		return nil, err
	}
	if len(custom) > 0 {
		all := make([]*Schema, 0, len(builtins)+len(custom))
		all = append(all, builtins...)
		all = append(all, custom...)
		if err := ValidateAll(custom); err != nil {
			// Also allow custom schemas that import built-in types.
			if errAll := ValidateAll(all); errAll != nil {
				return nil, err
			}
		}
	}
	r := &Registry{
		enums:    make(map[string]schemaEnum),
		messages: make(map[string]schemaMessage),
		unions:   make(map[string]schemaUnion),
	}
	for _, s := range builtins {
		if err := r.addSchema(s, false); err != nil {
			return nil, err
		}
	}
	for _, s := range custom {
		if s == nil {
			continue
		}
		if err := r.addSchema(s, true); err != nil {
			return nil, err
		}
	}
	slices.Sort(r.customUnions)
	slices.Sort(r.customMessages)
	return r, nil
}

func (r *Registry) addSchema(s *Schema, custom bool) error {
	for _, e := range s.Enums {
		if _, dup := r.enums[e.Name]; dup {
			return fmt.Errorf("wire: duplicate enum %q", e.Name)
		}
		r.enums[e.Name] = schemaEnum{schema: s, def: e, custom: custom}
	}
	for _, m := range s.Messages {
		if _, dup := r.messages[m.Name]; dup {
			return fmt.Errorf("wire: duplicate message %q", m.Name)
		}
		r.messages[m.Name] = schemaMessage{schema: s, def: m, custom: custom}
		if custom {
			r.customMessages = append(r.customMessages, m.Name)
		}
	}
	for _, u := range s.Unions {
		if _, dup := r.unions[u.Name]; dup {
			return fmt.Errorf("wire: duplicate union %q", u.Name)
		}
		r.unions[u.Name] = schemaUnion{schema: s, def: u, custom: custom}
		if custom {
			r.customUnions = append(r.customUnions, u.Name)
		}
	}
	return nil
}

// LoadSchemaPath loads one *.wire.yaml file or every *.wire.yaml file directly under a directory in sorted lexical order.
func LoadSchemaPath(path string) ([]*Schema, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	var files []string
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".wire.yaml") {
				files = append(files, filepath.Join(path, e.Name()))
			}
		}
		slices.Sort(files)
		if len(files) == 0 {
			return nil, fmt.Errorf("wire: no *.wire.yaml files in %s", path)
		}
	} else {
		files = []string{path}
	}
	schemas := make([]*Schema, 0, len(files))
	for _, f := range files {
		s, err := LoadFile(f)
		if err != nil {
			return nil, err
		}
		schemas = append(schemas, s)
	}
	return schemas, nil
}

// CustomUnions returns the sorted names of unions declared in custom schemas.
func (r *Registry) CustomUnions() []string { return slices.Clone(r.customUnions) }

// CustomMessages returns the sorted names of messages declared in custom schemas.
func (r *Registry) CustomMessages() []string { return slices.Clone(r.customMessages) }

// DecodeMessage dynamically decodes one named Message from src using its schema definition.
func (r *Registry) DecodeMessage(name string, src []byte) (DecodedObject, int, error) {
	sm, ok := r.messages[name]
	if !ok {
		return nil, 0, fmt.Errorf("wire: unknown message %q", name)
	}
	return r.decodeMessageDef(sm.schema, sm.def, src)
}

// EncodeMessage dynamically encodes obj back into wire bytes according to the named Message's schema definition.
func (r *Registry) EncodeMessage(name string, obj DecodedObject) ([]byte, error) {
	sm, ok := r.messages[name]
	if !ok {
		return nil, fmt.Errorf("wire: unknown message %q", name)
	}
	return r.encodeMessageDef(nil, sm.schema, sm.def, obj)
}

// DecodeUnion dynamically decodes one named Union from src using its discriminator and matched case message.
func (r *Registry) DecodeUnion(name string, src []byte) (DecodedUnion, int, error) {
	su, ok := r.unions[name]
	if !ok {
		return DecodedUnion{}, 0, fmt.Errorf("wire: unknown union %q", name)
	}
	u := su.def
	d := u.Discriminator
	off := 0
	var tag uint64
	var hasHigh bool
	var highVal uint8
	switch d.Type {
	case "u8":
		if len(src) < 1 {
			return DecodedUnion{}, 0, ErrShortBuffer
		}
		tag = uint64(src[0])
		off = 1
	case "u16":
		if len(src) < 2 {
			return DecodedUnion{}, 0, ErrShortBuffer
		}
		ord := resolveOrder(d.Order, su.schema.Order)
		tag = uint64(readUint16(src[:2], ord))
		off = 2
	case "uvarint":
		v, n := binary.Uvarint(src)
		if n == 0 {
			return DecodedUnion{}, 0, ErrShortBuffer
		}
		if n < 0 {
			return DecodedUnion{}, 0, ErrOverflow
		}
		tag = v
		off = n
	case "nibble":
		if len(src) < 1 {
			return DecodedUnion{}, 0, ErrShortBuffer
		}
		hb := nibbleHighBits(d)
		lb := 8 - hb
		b := src[0]
		highVal = b >> lb
		tag = uint64(b & ((1 << lb) - 1))
		if highVal != d.HighValue {
			return DecodedUnion{}, 0, ErrInvalidVersion
		}
		hasHigh = true
		off = 1
	default:
		return DecodedUnion{}, 0, fmt.Errorf("wire: unsupported discriminator %q", d.Type)
	}

	var matched *UnionCase
	for i := range u.Cases {
		if u.Cases[i].Tag == tag {
			matched = &u.Cases[i]
			break
		}
	}
	if matched == nil {
		return DecodedUnion{}, 0, ErrInvalidTag
	}
	fields, n, err := r.DecodeMessage(matched.Message, src[off:])
	if err != nil {
		return DecodedUnion{}, 0, err
	}
	caseName := matched.Name
	if caseName == "" {
		caseName = matched.Message
	}
	return DecodedUnion{
		Schema:    su.schema.Package,
		Union:     u.Name,
		HasHigh:   hasHigh,
		HighName:  nibbleHighName(d),
		HighValue: highVal,
		Case:      caseName,
		Tag:       tag,
		Message:   matched.Message,
		Fields:    fields,
	}, off + n, nil
}

// EncodeUnion dynamically encodes du back into wire bytes according to its Union's discriminator and selected case message.
func (r *Registry) EncodeUnion(du DecodedUnion) ([]byte, error) {
	su, ok := r.unions[du.Union]
	if !ok {
		return nil, fmt.Errorf("wire: unknown union %q", du.Union)
	}
	u := su.def
	d := u.Discriminator
	var matched *UnionCase
	for i := range u.Cases {
		c := &u.Cases[i]
		cname := c.Name
		if cname == "" {
			cname = c.Message
		}
		if (du.Case != "" && cname == du.Case) || (du.Case == "" && c.Tag == du.Tag) {
			matched = c
			break
		}
	}
	if matched == nil {
		return nil, ErrInvalidTag
	}

	var dst []byte
	switch d.Type {
	case "u8":
		dst = append(dst, byte(matched.Tag))
	case "u16":
		ord := resolveOrder(d.Order, su.schema.Order)
		dst = appendUint16(dst, uint16(matched.Tag), ord)
	case "uvarint":
		dst = binary.AppendUvarint(dst, matched.Tag)
	case "nibble":
		hb := nibbleHighBits(d)
		lb := 8 - hb
		hv := d.HighValue
		if du.HasHigh {
			hv = du.HighValue
		}
		dst = append(dst, (hv<<lb)|byte(matched.Tag))
	default:
		return nil, fmt.Errorf("wire: unsupported discriminator %q", d.Type)
	}
	sm, ok := r.messages[matched.Message]
	if !ok {
		return nil, fmt.Errorf("wire: unknown message %q", matched.Message)
	}
	return r.encodeMessageDef(dst, sm.schema, sm.def, du.Fields)
}

func resolveOrder(orders ...ByteOrder) ByteOrder {
	for _, o := range orders {
		if o != "" {
			return o
		}
	}
	return OrderBig
}

func fieldIsBitPacked(f Field, msgOrder, schemaOrder ByteOrder) bool {
	if f.Type == "bits" {
		return true
	}
	if f.Type == "qfloat" {
		ord := resolveOrder(f.Order, msgOrder, schemaOrder)
		if ord == OrderLittle && (f.QFloat.Bits == 8 || f.QFloat.Bits == 16 || f.QFloat.Bits == 32) {
			return false
		}
		return true
	}
	return false
}

func (r *Registry) decodeMessageDef(s *Schema, m Message, src []byte) (DecodedObject, int, error) {
	off := 0
	out := make(DecodedObject, 0, len(m.Fields))
	inBits := false
	var br BitReader
	flushBits := func() error {
		if !inBits {
			return nil
		}
		if err := br.Align(); err != nil {
			return err
		}
		off = br.Off
		inBits = false
		return nil
	}
	for _, f := range m.Fields {
		if fieldIsBitPacked(f, m.Order, s.Order) {
			if !inBits {
				br = BitReader{Src: src, Off: off}
				inBits = true
			}
			val, err := decodeBitField(&br, f)
			if err != nil {
				return nil, 0, err
			}
			out = append(out, DecodedField{Name: f.Name, Value: val})
			continue
		}
		if err := flushBits(); err != nil {
			return nil, 0, err
		}
		val, n, err := r.decodeField(s, m, f, src[off:])
		if err != nil {
			return nil, 0, err
		}
		off += n
		out = append(out, DecodedField{Name: f.Name, Value: val})
	}
	if err := flushBits(); err != nil {
		return nil, 0, err
	}
	return out, off, nil
}

func decodeBitField(br *BitReader, f Field) (any, error) {
	switch f.Type {
	case "bits":
		v, err := br.ReadBits(uint8(f.Bits))
		if err != nil {
			return nil, err
		}
		if f.Const != nil && v != *f.Const {
			return nil, ErrInvalidConst
		}
		return v, nil
	case "qfloat":
		qf := f.QFloat
		q, err := br.ReadBits(uint8(qf.Bits))
		if err != nil {
			return nil, err
		}
		return DequantizeFloat(uint32(q), float32(qf.Min), float32(qf.Max), qf.Bits), nil
	default:
		return nil, fmt.Errorf("wire: field %q is not bit-packed", f.Name)
	}
}

func (r *Registry) decodeField(s *Schema, m Message, f Field, src []byte) (any, int, error) {
	ord := resolveOrder(f.Order, m.Order, s.Order)
	switch f.Type {
	case "u8", "u16", "u32", "u64", "uvarint":
		v, n, err := readUnsignedInt(src, f.Type, ord, f.Bits)
		if err != nil {
			return nil, 0, err
		}
		if f.Const != nil && v != *f.Const {
			return nil, 0, ErrInvalidConst
		}
		return v, n, nil
	case "i8", "i16", "i32", "i64", "svarint":
		v, n, err := readSignedInt(src, f.Type, ord, f.Bits)
		if err != nil {
			return nil, 0, err
		}
		return v, n, nil
	case "bool":
		if len(src) < 1 {
			return nil, 0, ErrShortBuffer
		}
		if src[0] > 1 {
			return nil, 0, ErrInvalidBool
		}
		return src[0] == 1, 1, nil
	case "f32":
		if len(src) < 4 {
			return nil, 0, ErrShortBuffer
		}
		return math.Float32frombits(readUint32(src[:4], ord)), 4, nil
	case "qfloat":
		qf := f.QFloat
		var q uint32
		var n int
		switch qf.Bits {
		case 8:
			if len(src) < 1 {
				return nil, 0, ErrShortBuffer
			}
			q, n = uint32(src[0]), 1
		case 16:
			if len(src) < 2 {
				return nil, 0, ErrShortBuffer
			}
			q, n = uint32(readUint16(src[:2], ord)), 2
		case 32:
			if len(src) < 4 {
				return nil, 0, ErrShortBuffer
			}
			q, n = readUint32(src[:4], ord), 4
		default:
			return nil, 0, fmt.Errorf("wire: unexpected byte-aligned qfloat bits %d", qf.Bits)
		}
		return DequantizeFloat(q, float32(qf.Min), float32(qf.Max), qf.Bits), n, nil
	case "fixed":
		fx := f.Fixed
		fxOrd := resolveOrder(fx.Order, f.Order, m.Order, s.Order)
		if isSignedWireInt(fx.Type) {
			v, n, err := readSignedInt(src, fx.Type, fxOrd, 0)
			if err != nil {
				return nil, 0, err
			}
			return DequantizeFixed(v, fx.Scale), n, nil
		}
		v, n, err := readUnsignedInt(src, fx.Type, fxOrd, 0)
		if err != nil {
			return nil, 0, err
		}
		return DequantizeFixedUint(v, fx.Scale), n, nil
	case "bytes", "string":
		lenKind := f.Length
		if lenKind == "" {
			lenKind = "uvarint"
		}
		var blen int
		off := 0
		if lenKind == "rest" {
			blen = len(src)
		} else {
			rawLen, n, err := readUnsignedInt(src, lenKind, ord, 0)
			if err != nil {
				return nil, 0, err
			}
			off += n
			if f.MaxLength > 0 && rawLen > uint64(f.MaxLength) {
				return nil, 0, ErrLimitExceeded
			}
			if rawLen > uint64(len(src)-off) {
				return nil, 0, ErrShortBuffer
			}
			blen = int(rawLen)
		}
		raw := make([]byte, blen)
		copy(raw, src[off:off+blen])
		off += blen
		if f.Type == "string" {
			return string(raw), off, nil
		}
		return WireBytes(raw), off, nil
	case "array":
		a := f.Array
		arrOrd := resolveOrder(a.Order, f.Order, m.Order, s.Order)
		elemOrd := resolveOrder(a.OfOrder, a.Order, f.Order, m.Order, s.Order)
		off := 0
		var count int
		if a.Count > 0 {
			count = a.Count
		} else {
			lenKind := a.Length
			if lenKind == "" {
				lenKind = "uvarint"
			}
			rawCount, n, err := readUnsignedInt(src, lenKind, arrOrd, 0)
			if err != nil {
				return nil, 0, err
			}
			off += n
			if a.MaxLength > 0 && rawCount > uint64(a.MaxLength) {
				return nil, 0, ErrLimitExceeded
			}
			if rawCount > uint64(len(src)-off) {
				// Even the smallest element is at least 1 byte (unless empty message, checked below).
				if sm, ok := r.messages[a.Of]; !ok || len(sm.def.Fields) > 0 {
					return nil, 0, ErrShortBuffer
				}
			}
			count = int(rawCount)
		}
		elems := make([]any, 0, count)
		for i := 0; i < count; i++ {
			v, n, err := r.decodeArrayElem(a.Of, elemOrd, a.OfBits, src[off:])
			if err != nil {
				return nil, 0, err
			}
			off += n
			elems = append(elems, v)
		}
		return elems, off, nil
	case "mask":
		ms := f.Mask
		maskType := ms.Type
		if maskType == "" {
			maskType = "u8"
		}
		maskOrd := resolveOrder(ms.Order, f.Order, m.Order, s.Order)
		maskVal, n, err := readUnsignedInt(src, maskType, maskOrd, 0)
		if err != nil {
			return nil, 0, err
		}
		off := n
		var validMask uint64
		if len(ms.Fields) == 64 {
			validMask = math.MaxUint64
		} else {
			validMask = (uint64(1) << len(ms.Fields)) - 1
		}
		if maskVal & ^validMask != 0 {
			return nil, 0, ErrInvalidMask
		}
		fields := make(DecodedObject, 0, len(ms.Fields))
		for i, sf := range ms.Fields {
			if maskVal&(uint64(1)<<i) == 0 {
				continue
			}
			if fieldIsBitPacked(sf, m.Order, s.Order) {
				brMask := BitReader{Src: src, Off: off}
				v, err := decodeBitField(&brMask, sf)
				if err != nil {
					return nil, 0, err
				}
				if err := brMask.Align(); err != nil {
					return nil, 0, err
				}
				off = brMask.Off
				fields = append(fields, DecodedField{Name: sf.Name, Value: v})
				continue
			}
			v, fn, err := r.decodeField(s, m, sf, src[off:])
			if err != nil {
				return nil, 0, err
			}
			off += fn
			fields = append(fields, DecodedField{Name: sf.Name, Value: v})
		}
		return DecodedMask{Mask: maskVal, Fields: fields}, off, nil
	default:
		if se, ok := r.enums[f.Type]; ok {
			enOrd := resolveOrder(se.def.Order, se.schema.Order)
			v, n, err := readUnsignedInt(src, se.def.Type, enOrd, 0)
			if err != nil {
				return nil, 0, err
			}
			if f.Const != nil && v != *f.Const {
				return nil, 0, ErrInvalidConst
			}
			for _, ev := range se.def.Values {
				if ev.Value == v {
					return DecodedEnum{Name: ev.Name, Value: v}, n, nil
				}
			}
			return nil, 0, ErrInvalidEnum
		}
		if sm, ok := r.messages[f.Type]; ok {
			return r.decodeMessageDef(sm.schema, sm.def, src)
		}
		return nil, 0, fmt.Errorf("wire: unknown field type %q", f.Type)
	}
}

func (r *Registry) decodeArrayElem(elemType string, ord ByteOrder, bits int, src []byte) (any, int, error) {
	switch elemType {
	case "u8", "u16", "u32", "u64", "uvarint":
		return readUnsignedInt(src, elemType, ord, bits)
	case "i8", "i16", "i32", "i64", "svarint":
		return readSignedInt(src, elemType, ord, bits)
	case "bool":
		if len(src) < 1 {
			return nil, 0, ErrShortBuffer
		}
		if src[0] > 1 {
			return nil, 0, ErrInvalidBool
		}
		return src[0] == 1, 1, nil
	case "f32":
		if len(src) < 4 {
			return nil, 0, ErrShortBuffer
		}
		return math.Float32frombits(readUint32(src[:4], ord)), 4, nil
	default:
		if se, ok := r.enums[elemType]; ok {
			enOrd := resolveOrder(se.def.Order, se.schema.Order)
			v, n, err := readUnsignedInt(src, se.def.Type, enOrd, 0)
			if err != nil {
				return nil, 0, err
			}
			for _, ev := range se.def.Values {
				if ev.Value == v {
					return DecodedEnum{Name: ev.Name, Value: v}, n, nil
				}
			}
			return nil, 0, ErrInvalidEnum
		}
		if sm, ok := r.messages[elemType]; ok {
			return r.decodeMessageDef(sm.schema, sm.def, src)
		}
		return nil, 0, fmt.Errorf("wire: unknown array element type %q", elemType)
	}
}

func (r *Registry) encodeMessageDef(dst []byte, s *Schema, m Message, obj DecodedObject) ([]byte, error) {
	inBits := false
	var bw BitWriter
	flushBits := func() {
		if inBits {
			dst = bw.Flush()
			inBits = false
		}
	}
	for _, f := range m.Fields {
		val, ok := obj.Get(f.Name)
		if !ok && f.Const == nil {
			return nil, fmt.Errorf("wire: %s missing field %q", m.Name, f.Name)
		}
		if fieldIsBitPacked(f, m.Order, s.Order) {
			if !inBits {
				bw = BitWriter{Dst: dst}
				inBits = true
			}
			if err := encodeBitField(&bw, f, val); err != nil {
				return nil, err
			}
			continue
		}
		flushBits()
		var err error
		dst, err = r.encodeField(dst, s, m, f, val)
		if err != nil {
			return nil, err
		}
	}
	flushBits()
	return dst, nil
}

func encodeBitField(bw *BitWriter, f Field, val any) error {
	switch f.Type {
	case "bits":
		var u uint64
		if f.Const != nil {
			u = *f.Const
		} else {
			var err error
			u, err = asUint64(val)
			if err != nil {
				return fmt.Errorf("wire: field %q: %w", f.Name, err)
			}
		}
		bw.WriteBits(u, uint8(f.Bits))
		return nil
	case "qfloat":
		fv, err := asFloat32(val)
		if err != nil {
			return fmt.Errorf("wire: field %q: %w", f.Name, err)
		}
		qf := f.QFloat
		bw.WriteBits(uint64(QuantizeFloat(fv, float32(qf.Min), float32(qf.Max), qf.Bits)), uint8(qf.Bits))
		return nil
	default:
		return fmt.Errorf("wire: field %q is not bit-packed", f.Name)
	}
}

func (r *Registry) encodeField(dst []byte, s *Schema, m Message, f Field, val any) ([]byte, error) {
	ord := resolveOrder(f.Order, m.Order, s.Order)
	switch f.Type {
	case "u8", "u16", "u32", "u64", "uvarint":
		var u uint64
		if f.Const != nil {
			u = *f.Const
		} else {
			var err error
			u, err = asUint64(val)
			if err != nil {
				return nil, fmt.Errorf("wire: %s.%s: %w", m.Name, f.Name, err)
			}
		}
		return appendUnsignedInt(dst, f.Type, ord, u)
	case "i8", "i16", "i32", "i64", "svarint":
		v, err := asInt64(val)
		if err != nil {
			return nil, fmt.Errorf("wire: %s.%s: %w", m.Name, f.Name, err)
		}
		return appendSignedInt(dst, f.Type, ord, v)
	case "bool":
		b, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("wire: %s.%s: expected bool, got %T", m.Name, f.Name, val)
		}
		if b {
			return append(dst, 1), nil
		}
		return append(dst, 0), nil
	case "f32":
		fv, err := asFloat32(val)
		if err != nil {
			return nil, fmt.Errorf("wire: %s.%s: %w", m.Name, f.Name, err)
		}
		return appendUint32(dst, math.Float32bits(fv), ord), nil
	case "qfloat":
		fv, err := asFloat32(val)
		if err != nil {
			return nil, fmt.Errorf("wire: %s.%s: %w", m.Name, f.Name, err)
		}
		qf := f.QFloat
		q := QuantizeFloat(fv, float32(qf.Min), float32(qf.Max), qf.Bits)
		switch qf.Bits {
		case 8:
			return append(dst, byte(q)), nil
		case 16:
			return appendUint16(dst, uint16(q), ord), nil
		case 32:
			return appendUint32(dst, q, ord), nil
		default:
			return nil, fmt.Errorf("wire: unexpected byte-aligned qfloat bits %d", qf.Bits)
		}
	case "fixed":
		fv, err := asFloat64(val)
		if err != nil {
			return nil, fmt.Errorf("wire: %s.%s: %w", m.Name, f.Name, err)
		}
		fx := f.Fixed
		fxOrd := resolveOrder(fx.Order, f.Order, m.Order, s.Order)
		if isSignedWireInt(fx.Type) {
			minInt, maxInt := signedIntBounds(fx.Type)
			q := QuantizeFixed(fv, fx.Scale, minInt, maxInt)
			return appendSignedInt(dst, fx.Type, fxOrd, q)
		}
		maxU := unsignedIntMax(fx.Type)
		q := QuantizeFixedUint(fv, fx.Scale, maxU)
		return appendUnsignedInt(dst, fx.Type, fxOrd, q)
	case "bytes":
		raw, err := r.encodeBytesValue(val)
		if err != nil {
			return nil, fmt.Errorf("wire: %s.%s: %w", m.Name, f.Name, err)
		}
		lenKind := f.Length
		if lenKind == "" {
			lenKind = "uvarint"
		}
		if lenKind != "rest" {
			dst, err = appendUnsignedInt(dst, lenKind, ord, uint64(len(raw)))
			if err != nil {
				return nil, err
			}
		}
		return append(dst, raw...), nil
	case "string":
		str, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("wire: %s.%s: expected string, got %T", m.Name, f.Name, val)
		}
		lenKind := f.Length
		if lenKind == "" {
			lenKind = "uvarint"
		}
		if lenKind != "rest" {
			var err error
			dst, err = appendUnsignedInt(dst, lenKind, ord, uint64(len(str)))
			if err != nil {
				return nil, err
			}
		}
		return append(dst, str...), nil
	case "array":
		a := f.Array
		arrOrd := resolveOrder(a.Order, f.Order, m.Order, s.Order)
		elemOrd := resolveOrder(a.OfOrder, a.Order, f.Order, m.Order, s.Order)
		elems, ok := val.([]any)
		if !ok {
			return nil, fmt.Errorf("wire: %s.%s: expected array slice, got %T", m.Name, f.Name, val)
		}
		if a.Count > 0 {
			if len(elems) != a.Count {
				return nil, fmt.Errorf("wire: %s.%s: expected %d elements, got %d", m.Name, f.Name, a.Count, len(elems))
			}
		} else {
			lenKind := a.Length
			if lenKind == "" {
				lenKind = "uvarint"
			}
			var err error
			dst, err = appendUnsignedInt(dst, lenKind, arrOrd, uint64(len(elems)))
			if err != nil {
				return nil, err
			}
		}
		for _, el := range elems {
			var err error
			dst, err = r.encodeArrayElem(dst, a.Of, elemOrd, el)
			if err != nil {
				return nil, fmt.Errorf("wire: %s.%s: %w", m.Name, f.Name, err)
			}
		}
		return dst, nil
	case "mask":
		dm, ok := val.(DecodedMask)
		if !ok {
			return nil, fmt.Errorf("wire: %s.%s: expected DecodedMask, got %T", m.Name, f.Name, val)
		}
		ms := f.Mask
		maskType := ms.Type
		if maskType == "" {
			maskType = "u8"
		}
		maskOrd := resolveOrder(ms.Order, f.Order, m.Order, s.Order)
		var err error
		dst, err = appendUnsignedInt(dst, maskType, maskOrd, dm.Mask)
		if err != nil {
			return nil, err
		}
		for i, sf := range ms.Fields {
			if dm.Mask&(uint64(1)<<i) == 0 {
				continue
			}
			sv, present := dm.Fields.Get(sf.Name)
			if !present {
				return nil, fmt.Errorf("wire: %s.%s: mask bit %d (%s) set but value missing", m.Name, f.Name, i, sf.Name)
			}
			if fieldIsBitPacked(sf, m.Order, s.Order) {
				bwMask := BitWriter{Dst: dst}
				if err := encodeBitField(&bwMask, sf, sv); err != nil {
					return nil, err
				}
				dst = bwMask.Flush()
				continue
			}
			dst, err = r.encodeField(dst, s, m, sf, sv)
			if err != nil {
				return nil, err
			}
		}
		return dst, nil
	default:
		if se, ok := r.enums[f.Type]; ok {
			enOrd := resolveOrder(se.def.Order, se.schema.Order)
			var u uint64
			if f.Const != nil {
				u = *f.Const
			} else {
				var err error
				u, err = resolveEnumValue(se.def, val)
				if err != nil {
					return nil, fmt.Errorf("wire: %s.%s: %w", m.Name, f.Name, err)
				}
			}
			return appendUnsignedInt(dst, se.def.Type, enOrd, u)
		}
		if sm, ok := r.messages[f.Type]; ok {
			sub, ok := val.(DecodedObject)
			if !ok {
				return nil, fmt.Errorf("wire: %s.%s: expected DecodedObject, got %T", m.Name, f.Name, val)
			}
			return r.encodeMessageDef(dst, sm.schema, sm.def, sub)
		}
		return nil, fmt.Errorf("wire: unknown field type %q", f.Type)
	}
}

func (r *Registry) encodeBytesValue(val any) ([]byte, error) {
	switch v := val.(type) {
	case WireBytes:
		return []byte(v), nil
	case []byte:
		return v, nil
	case DecodedPayload:
		if v.Union != "" {
			du := DecodedUnion{
				Schema:  v.Schema,
				Union:   v.Union,
				Case:    v.Case,
				Message: v.Message,
				Fields:  v.Fields,
			}
			if v.Tag != nil {
				du.Tag = *v.Tag
			}
			if v.Version != nil {
				du.HasHigh = true
				du.HighValue = *v.Version
			}
			return r.EncodeUnion(du)
		}
		return r.EncodeMessage(v.Message, v.Fields)
	case *DecodedPayload:
		if v == nil {
			return nil, errors.New("nil DecodedPayload")
		}
		return r.encodeBytesValue(*v)
	default:
		return nil, fmt.Errorf("expected bytes or DecodedPayload, got %T", val)
	}
}

func (r *Registry) encodeArrayElem(dst []byte, elemType string, ord ByteOrder, val any) ([]byte, error) {
	switch elemType {
	case "u8", "u16", "u32", "u64", "uvarint":
		u, err := asUint64(val)
		if err != nil {
			return nil, err
		}
		return appendUnsignedInt(dst, elemType, ord, u)
	case "i8", "i16", "i32", "i64", "svarint":
		v, err := asInt64(val)
		if err != nil {
			return nil, err
		}
		return appendSignedInt(dst, elemType, ord, v)
	case "bool":
		b, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("expected bool, got %T", val)
		}
		if b {
			return append(dst, 1), nil
		}
		return append(dst, 0), nil
	case "f32":
		fv, err := asFloat32(val)
		if err != nil {
			return nil, err
		}
		return appendUint32(dst, math.Float32bits(fv), ord), nil
	default:
		if se, ok := r.enums[elemType]; ok {
			enOrd := resolveOrder(se.def.Order, se.schema.Order)
			u, err := resolveEnumValue(se.def, val)
			if err != nil {
				return nil, err
			}
			return appendUnsignedInt(dst, se.def.Type, enOrd, u)
		}
		if sm, ok := r.messages[elemType]; ok {
			sub, ok := val.(DecodedObject)
			if !ok {
				return nil, fmt.Errorf("expected DecodedObject, got %T", val)
			}
			return r.encodeMessageDef(dst, sm.schema, sm.def, sub)
		}
		return nil, fmt.Errorf("unknown array element type %q", elemType)
	}
}

func resolveEnumValue(e Enum, val any) (uint64, error) {
	switch v := val.(type) {
	case DecodedEnum:
		return v.Value, nil
	case string:
		for _, ev := range e.Values {
			if ev.Name == v {
				return ev.Value, nil
			}
		}
		return 0, ErrInvalidEnum
	default:
		return asUint64(val)
	}
}

func readUint16(b []byte, ord ByteOrder) uint16 {
	if ord == OrderLittle {
		return binary.LittleEndian.Uint16(b)
	}
	return binary.BigEndian.Uint16(b)
}

func readUint32(b []byte, ord ByteOrder) uint32 {
	if ord == OrderLittle {
		return binary.LittleEndian.Uint32(b)
	}
	return binary.BigEndian.Uint32(b)
}

func readUint64(b []byte, ord ByteOrder) uint64 {
	if ord == OrderLittle {
		return binary.LittleEndian.Uint64(b)
	}
	return binary.BigEndian.Uint64(b)
}

func appendUint16(dst []byte, v uint16, ord ByteOrder) []byte {
	if ord == OrderLittle {
		return binary.LittleEndian.AppendUint16(dst, v)
	}
	return binary.BigEndian.AppendUint16(dst, v)
}

func appendUint32(dst []byte, v uint32, ord ByteOrder) []byte {
	if ord == OrderLittle {
		return binary.LittleEndian.AppendUint32(dst, v)
	}
	return binary.BigEndian.AppendUint32(dst, v)
}

func appendUint64(dst []byte, v uint64, ord ByteOrder) []byte {
	if ord == OrderLittle {
		return binary.LittleEndian.AppendUint64(dst, v)
	}
	return binary.BigEndian.AppendUint64(dst, v)
}

func readUnsignedInt(src []byte, typ string, ord ByteOrder, bits int) (uint64, int, error) {
	switch typ {
	case "u8":
		if len(src) < 1 {
			return 0, 0, ErrShortBuffer
		}
		return uint64(src[0]), 1, nil
	case "u16":
		if len(src) < 2 {
			return 0, 0, ErrShortBuffer
		}
		return uint64(readUint16(src[:2], ord)), 2, nil
	case "u32":
		if len(src) < 4 {
			return 0, 0, ErrShortBuffer
		}
		return uint64(readUint32(src[:4], ord)), 4, nil
	case "u64":
		if len(src) < 8 {
			return 0, 0, ErrShortBuffer
		}
		return readUint64(src[:8], ord), 8, nil
	case "uvarint":
		v, n := binary.Uvarint(src)
		if n == 0 {
			return 0, 0, ErrShortBuffer
		}
		if n < 0 {
			return 0, 0, ErrOverflow
		}
		if bits > 0 && bits < 64 && v > ((uint64(1)<<bits)-1) {
			return 0, 0, ErrOverflow
		}
		return v, n, nil
	default:
		return 0, 0, fmt.Errorf("wire: unsupported unsigned type %q", typ)
	}
}

func readSignedInt(src []byte, typ string, ord ByteOrder, bits int) (int64, int, error) {
	switch typ {
	case "i8":
		if len(src) < 1 {
			return 0, 0, ErrShortBuffer
		}
		return int64(int8(src[0])), 1, nil
	case "i16":
		if len(src) < 2 {
			return 0, 0, ErrShortBuffer
		}
		return int64(int16(readUint16(src[:2], ord))), 2, nil
	case "i32":
		if len(src) < 4 {
			return 0, 0, ErrShortBuffer
		}
		return int64(int32(readUint32(src[:4], ord))), 4, nil
	case "i64":
		if len(src) < 8 {
			return 0, 0, ErrShortBuffer
		}
		return int64(readUint64(src[:8], ord)), 8, nil
	case "svarint":
		v, n := binary.Varint(src)
		if n == 0 {
			return 0, 0, ErrShortBuffer
		}
		if n < 0 {
			return 0, 0, ErrOverflow
		}
		if bits > 0 && bits < 64 {
			minV := -(int64(1) << (bits - 1))
			maxV := (int64(1) << (bits - 1)) - 1
			if v < minV || v > maxV {
				return 0, 0, ErrOverflow
			}
		}
		return v, n, nil
	default:
		return 0, 0, fmt.Errorf("wire: unsupported signed type %q", typ)
	}
}

func appendUnsignedInt(dst []byte, typ string, ord ByteOrder, v uint64) ([]byte, error) {
	switch typ {
	case "u8":
		return append(dst, byte(v)), nil
	case "u16":
		return appendUint16(dst, uint16(v), ord), nil
	case "u32":
		return appendUint32(dst, uint32(v), ord), nil
	case "u64":
		return appendUint64(dst, v, ord), nil
	case "uvarint":
		return binary.AppendUvarint(dst, v), nil
	default:
		return nil, fmt.Errorf("wire: unsupported unsigned type %q", typ)
	}
}

func appendSignedInt(dst []byte, typ string, ord ByteOrder, v int64) ([]byte, error) {
	switch typ {
	case "i8":
		return append(dst, byte(int8(v))), nil
	case "i16":
		return appendUint16(dst, uint16(int16(v)), ord), nil
	case "i32":
		return appendUint32(dst, uint32(int32(v)), ord), nil
	case "i64":
		return appendUint64(dst, uint64(v), ord), nil
	case "svarint":
		return binary.AppendVarint(dst, v), nil
	default:
		return nil, fmt.Errorf("wire: unsupported signed type %q", typ)
	}
}

func signedIntBounds(typ string) (int64, int64) {
	switch typ {
	case "i8":
		return math.MinInt8, math.MaxInt8
	case "i16":
		return math.MinInt16, math.MaxInt16
	case "i32":
		return math.MinInt32, math.MaxInt32
	default:
		return math.MinInt64, math.MaxInt64
	}
}

func unsignedIntMax(typ string) uint64 {
	switch typ {
	case "u8":
		return math.MaxUint8
	case "u16":
		return math.MaxUint16
	case "u32":
		return math.MaxUint32
	default:
		return math.MaxUint64
	}
}

func asUint64(val any) (uint64, error) {
	switch v := val.(type) {
	case uint64:
		return v, nil
	case uint32:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case int:
		if v < 0 {
			return 0, fmt.Errorf("negative integer %d for unsigned field", v)
		}
		return uint64(v), nil
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("negative integer %d for unsigned field", v)
		}
		return uint64(v), nil
	default:
		return 0, fmt.Errorf("expected unsigned integer, got %T", val)
	}
}

func asInt64(val any) (int64, error) {
	switch v := val.(type) {
	case int64:
		return v, nil
	case int32:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int:
		return int64(v), nil
	default:
		return 0, fmt.Errorf("expected signed integer, got %T", val)
	}
}

func asFloat32(val any) (float32, error) {
	switch v := val.(type) {
	case float32:
		return v, nil
	case float64:
		return float32(v), nil
	default:
		return 0, fmt.Errorf("expected float32, got %T", val)
	}
}

func asFloat64(val any) (float64, error) {
	switch v := val.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	default:
		return 0, fmt.Errorf("expected float64, got %T", val)
	}
}
