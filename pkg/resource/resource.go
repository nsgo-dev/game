package resource

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/nsgo-dev/game/internal/yamlzero"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"go.yaml.in/yaml/v3"
)

// CurrentVersion is the resource schema version this package reads and writes.
const CurrentVersion = 1

// Kind is a resource file's type, written in its header's type field.
type Kind string

const (
	// KindMaterial is a Material: surface color, texture, opacity and blending.
	KindMaterial Kind = "material"
	// KindCurve is a Curve: a value keyed over time.
	KindCurve Kind = "curve"
	// KindGradient is a Gradient: colors keyed over offsets from 0 to 1.
	KindGradient Kind = "gradient"
	// KindAnimation is an Animation: property tracks keyed over a clip's length.
	KindAnimation Kind = "animation"
	// KindTheme is a Theme: colors, spacing and font for gameui.
	KindTheme Kind = "theme"
	// KindBusLayout is a BusLayout: sound buses and their effect chains.
	KindBusLayout Kind = "busLayout"
	// KindPhysicsMaterial is a PhysicsMaterial: friction, bounce and damping for rigid bodies.
	KindPhysicsMaterial Kind = "physicsMaterial"
)

// Kinds returns every resource type this package reads, in a fixed order.
func Kinds() []Kind {
	return []Kind{KindMaterial, KindCurve, KindGradient, KindAnimation, KindTheme, KindBusLayout, KindPhysicsMaterial}
}

// Header is the common start of every resource file: its type, schema version and stable ID. Every resource type embeds it, so Type, Version and UID are the first fields of each file.
type Header struct {
	// Type names the resource's type; it must match the Go type holding it.
	Type Kind `yaml:"type"`
	// Version is the schema version; it must equal CurrentVersion (1).
	Version int `yaml:"version"`
	// UID is the file's stable ID, which stays the same when the file is renamed or moved so the tools can find it again (see package refs); empty until a tool such as game index gives it one. When set it must be a UID as NewUID returns it.
	UID string `yaml:"uid,omitempty"`
}

// NewHeader returns the header for a resource of kind at CurrentVersion.
func NewHeader(kind Kind) Header { return Header{Type: kind, Version: CurrentVersion} }

func (h *Header) header() *Header { return h }

func (h *Header) validate(want Kind) error {
	if h.Type != want {
		return loaderr.Errorf("type", fmt.Sprintf("set type: %s at the top of the resource file", want), "resource: type %q, want %q", h.Type, want)
	}
	if h.Version != CurrentVersion {
		return loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the resource file", CurrentVersion), "resource: %s: unsupported version %d (want %d)", want, h.Version, CurrentVersion)
	}
	if h.UID != "" && !ValidUID(h.UID) {
		return loaderr.Errorf("uid", fmt.Sprintf("set uid to %d characters of 0-9 and a-z without i, l, o and u, or omit it", UIDLength), "resource: %s: uid %q must be %d characters of 0-9 and a-z without i, l, o and u", want, h.UID, UIDLength)
	}
	return nil
}

// Resource is one parsed resource file: a *Material, *Curve, *Gradient, *Animation, *Theme, *BusLayout or *PhysicsMaterial.
type Resource interface {
	// Kind returns the resource's type.
	Kind() Kind
	// Validate checks the header and every field against the schema.
	Validate() error
	header() *Header
}

// New returns an empty resource of kind with its header set, or an error for a kind this package does not know.
func New(kind Kind) (Resource, error) {
	h := NewHeader(kind)
	switch kind {
	case KindMaterial:
		return &Material{Header: h}, nil
	case KindCurve:
		return &Curve{Header: h}, nil
	case KindGradient:
		return &Gradient{Header: h}, nil
	case KindAnimation:
		return &Animation{Header: h}, nil
	case KindTheme:
		return &Theme{Header: h}, nil
	case KindBusLayout:
		return &BusLayout{Header: h}, nil
	case KindPhysicsMaterial:
		return &PhysicsMaterial{Header: h}, nil
	}
	return nil, fmt.Errorf("resource: unknown type %q", kind)
}

// Parse decodes one YAML resource file and validates it. It reads the header first, refuses an unknown type or unsupported version, then decodes the whole document into the matching type, refusing unknown fields and trailing documents.
func Parse(data []byte) (Resource, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, loaderr.Locate(loaderr.Errorf("", fmt.Sprintf("add a resource file starting with type and version: %d", CurrentVersion), "resource: empty document"), "", data, "")
	}
	var h Header
	if err := yaml.Unmarshal(data, &h); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("resource: decode header: %w", err), "", data, "check the YAML syntax and header fields (type, version) in the resource file")
	}
	if h.Type == "" {
		return nil, loaderr.Locate(loaderr.Errorf("type", "set type to material, curve, gradient, animation, theme, busLayout, or physicsMaterial", "resource: missing type"), "", data, "")
	}
	r, err := New(h.Type)
	if err != nil {
		return nil, loaderr.Locate(loaderr.Errorf("type", "set type to material, curve, gradient, animation, theme, busLayout, or physicsMaterial", "%s", err.Error()), "", data, "")
	}
	if h.Version != CurrentVersion {
		return nil, loaderr.Locate(loaderr.Errorf("version", fmt.Sprintf("set version: %d at the top of the resource file", CurrentVersion), "resource: %s: unsupported version %d (want %d)", h.Type, h.Version, CurrentVersion), "", data, "")
	}
	// Decode over a fresh value so the header fields come from the document itself.
	*r.header() = Header{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(r); err != nil {
		return nil, loaderr.Locate(fmt.Errorf("resource: decode %s: %w", h.Type, err), "", data, fmt.Sprintf("check the YAML syntax and field names for a %s resource", h.Type))
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, loaderr.Locate(loaderr.Errorf("", "keep a single YAML document in the resource file and remove any --- separator", "resource: unexpected trailing YAML document"), "", data, "")
		}
		return nil, loaderr.Locate(fmt.Errorf("resource: decode: %w", err), "", data, "remove trailing YAML content after the resource file")
	}
	if err := r.Validate(); err != nil {
		return nil, loaderr.Locate(err, "", data, "")
	}
	yamlzero.Normalize(r)
	return r, nil
}

// ParseAs parses data and returns it as T, failing when the file holds another type.
func ParseAs[T Resource](data []byte) (T, error) {
	var zero T
	r, err := Parse(data)
	if err != nil {
		return zero, err
	}
	t, ok := r.(T)
	if !ok {
		return zero, loaderr.Locate(loaderr.Errorf("type", fmt.Sprintf("load a %s resource file instead of %s", kindOf[T](), r.Kind()), "resource: file is a %s, want %s", r.Kind(), kindOf[T]()), "", data, "")
	}
	return t, nil
}

// kindOf returns the kind of the resource type T, for error messages.
func kindOf[T Resource]() Kind {
	var zero T
	switch any(zero).(type) {
	case *Material:
		return KindMaterial
	case *Curve:
		return KindCurve
	case *Gradient:
		return KindGradient
	case *Animation:
		return KindAnimation
	case *Theme:
		return KindTheme
	case *BusLayout:
		return KindBusLayout
	case *PhysicsMaterial:
		return KindPhysicsMaterial
	}
	return ""
}

// Encode validates r and writes it as deterministic 2-space YAML: the header first, then the type's fields in declaration order, omitting fields left at their defaults.
func Encode(r Resource) ([]byte, error) {
	if r == nil {
		return nil, errors.New("resource: nil resource")
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(yamlzero.Clean(r)); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("resource: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("resource: encode: %w", err)
	}
	return buf.Bytes(), nil
}

func isFinite(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// checkUnit reports an error unless v is finite and in [0, 1].
func checkUnit(field, where string, v float32) error {
	if !isFinite(v) || v < 0 || v > 1 {
		return loaderr.Errorf(field, fmt.Sprintf("set %s to a finite number in [0, 1]", field), "resource: %s %v must be in [0, 1]", where, v)
	}
	return nil
}

// checkNonNegative reports an error unless v is finite and not negative.
func checkNonNegative(field, where string, v float32) error {
	if !isFinite(v) || v < 0 {
		return loaderr.Errorf(field, fmt.Sprintf("set %s to a finite number >= 0", field), "resource: %s %v must be finite and non-negative", where, v)
	}
	return nil
}
