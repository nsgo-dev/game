package animclip

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// PropertySpriteFrame is the discrete object property that steps a sprite's atlas frame name without interpolation.
const PropertySpriteFrame = "sprite.frame"

// Kind is how a keyed value is written to its property.
type Kind uint8

const (
	// KindFloat writes the number as it is, clamped to the property's range.
	KindFloat Kind = iota + 1
	// KindInt rounds the number to the nearest whole number (halves away from zero), clamped to the property's range.
	KindInt
	// KindBool writes true from 0.5 up and false below it; its keys must be 0 or 1.
	KindBool
	// KindFrame writes a discrete atlas frame name onto the object's sprite without interpolation.
	KindFrame
)

// String returns the kind's name: "float", "int", "bool" or "frame".
func (k Kind) String() string {
	switch k {
	case KindFloat:
		return "float"
	case KindInt:
		return "int"
	case KindBool:
		return "bool"
	case KindFrame:
		return "frame"
	default:
		return "Kind(" + strconv.Itoa(int(k)) + ")"
	}
}

// Property is one property a clip track can key.
type Property struct {
	// Name is the property as a track writes it: an object field such as "x" or "sprite.frame", or "component.field" for a registered component field.
	Name string
	// Component and Field name the registered component field the property is; both are empty for an object field.
	Component, Field string
	// Kind is how a keyed value is written to the property.
	Kind Kind
	// Min and Max are the property's optional inclusive bounds; nil leaves that side open.
	Min, Max *float64
	// Doc is a one-line description for tools such as the editor.
	Doc string
	// Frames lists the atlas frame names of a KindFrame track in key order while NewPlayer binds a clip, so a Binder can validate them up front and map each keyed index through Property.Frame. It is nil on properties returned by Lookup or Properties.
	Frames []string
}

// IsComponent reports whether the property is a registered component field rather than an object field.
func (p Property) IsComponent() bool { return p.Component != "" }

// IsFrame reports whether the property keys discrete atlas frame names rather than numbers.
func (p Property) IsFrame() bool { return p.Kind == KindFrame }

// Frame maps a keyed index value from NewPlayer to its frame name in p.Frames, or "" when p is not a KindFrame track or v is out of range.
func (p Property) Frame(v float64) string {
	if p.Kind != KindFrame || len(p.Frames) == 0 || math.IsNaN(v) {
		return ""
	}
	idx := int(p.Convert(v))
	if idx < 0 || idx >= len(p.Frames) {
		return ""
	}
	return p.Frames[idx]
}

// objectProperties are the ObjectDoc fields a clip can key, in the order tools list them. Z is left out: it orders the sprite batches Build groups, which playback does not regroup.
var objectProperties = []Property{
	{Name: "x", Kind: KindFloat, Doc: "Horizontal position in the layer's space."},
	{Name: "y", Kind: KindFloat, Doc: "Vertical position in the layer's space."},
	{Name: "rotation", Kind: KindFloat, Doc: "Clockwise rotation in radians about the origin."},
	{Name: "scaleX", Kind: KindFloat, Doc: "Horizontal scale about the origin; negative mirrors."},
	{Name: "scaleY", Kind: KindFloat, Doc: "Vertical scale about the origin; negative mirrors."},
	{Name: "alpha", Kind: KindFloat, Min: ptr(0), Max: ptr(1), Doc: "Opacity from 0 to 1."},
	{Name: "originX", Kind: KindFloat, Doc: "Pivot's horizontal offset inside the sprite's frame."},
	{Name: "originY", Kind: KindFloat, Doc: "Pivot's vertical offset inside the sprite's frame."},
	{Name: "flipX", Kind: KindBool, Doc: "Mirrors the sprite horizontally."},
	{Name: "flipY", Kind: KindBool, Doc: "Mirrors the sprite vertically."},
	{Name: PropertySpriteFrame, Kind: KindFrame, Doc: "Atlas frame name the object's sprite draws, stepping discretely without interpolation."},
	{Name: "posZ", Kind: KindFloat, Doc: "3D depth position."},
	{Name: "rotX", Kind: KindFloat, Doc: "3D rotation in radians about the X axis."},
	{Name: "rotY", Kind: KindFloat, Doc: "3D rotation in radians about the Y axis."},
	{Name: "rotZ", Kind: KindFloat, Doc: "3D rotation in radians about the Z axis."},
	{Name: "scaleZ", Kind: KindFloat, Doc: "3D scale along the Z axis."},
}

func ptr(v float64) *float64 { return &v }

// ObjectProperties returns the object fields a clip can key, in the order tools list them.
func ObjectProperties() []Property {
	out := make([]Property, len(objectProperties))
	for i, p := range objectProperties {
		out[i] = cloneProperty(p)
	}
	return out
}

// Properties returns every property a clip can key with reg: the object fields, then each registered component's numeric and bool fields, components by name and fields in declaration order. A nil reg returns the object fields only.
func Properties(reg *sceneformat.ComponentRegistry) []Property {
	out := ObjectProperties()
	for _, t := range reg.Types() {
		for _, f := range t.Fields {
			if p, ok := componentProperty(t.Name, f); ok {
				out = append(out, p)
			}
		}
	}
	return out
}

// Lookup resolves the property name a track writes. Built-in object properties (including "sprite.frame") resolve without a registry; any other "component.field" is a field of a component registered in reg, split at the first dot. It fails for an unknown object field, a component property without a registry, an unregistered component or field, and a string component field.
func Lookup(reg *sceneformat.ComponentRegistry, name string) (Property, error) {
	for _, p := range objectProperties {
		if p.Name == name {
			return cloneProperty(p), nil
		}
	}
	comp, field, isComp := strings.Cut(name, ".")
	if !isComp {
		names := make([]string, len(objectProperties))
		for i, p := range objectProperties {
			names[i] = p.Name
		}
		return Property{}, fmt.Errorf("animclip: property %q is not an object property (%s) or a component field written component.field", name, strings.Join(names, ", "))
	}
	if reg == nil {
		return Property{}, fmt.Errorf("animclip: property %q is a component field, which needs a component registry", name)
	}
	t, ok := reg.Lookup(comp)
	if !ok {
		return Property{}, fmt.Errorf("animclip: property %q: component type %q is not registered", name, comp)
	}
	f, ok := t.Field(field)
	if !ok {
		return Property{}, fmt.Errorf("animclip: property %q: component %q has no field %q (registered: %s)", name, comp, field, strings.Join(t.FieldNames(), ", "))
	}
	p, ok := componentProperty(comp, f)
	if !ok {
		return Property{}, fmt.Errorf("animclip: property %q is a %v field, which cannot be keyed", name, f.Kind)
	}
	return p, nil
}

func componentProperty(component string, f sceneformat.Field) (Property, bool) {
	var k Kind
	switch f.Kind {
	case sceneformat.FieldFloat:
		k = KindFloat
	case sceneformat.FieldInt:
		k = KindInt
	case sceneformat.FieldBool:
		k = KindBool
	default:
		return Property{}, false
	}
	p := Property{Name: component + "." + f.Name, Component: component, Field: f.Name, Kind: k, Doc: f.Doc}
	if f.Min != nil {
		p.Min = ptr(*f.Min)
	}
	if f.Max != nil {
		p.Max = ptr(*f.Max)
	}
	return p, true
}

func cloneProperty(p Property) Property {
	if p.Min != nil {
		p.Min = ptr(*p.Min)
	}
	if p.Max != nil {
		p.Max = ptr(*p.Max)
	}
	if p.Frames != nil {
		p.Frames = slices.Clone(p.Frames)
	}
	return p
}

// Convert returns v as the property takes it: rounded for KindInt, 0 or 1 for KindBool, clamped index for KindFrame, and clamped to the range. NaN is returned unchanged.
func (p Property) Convert(v float64) float64 {
	if math.IsNaN(v) {
		return v
	}
	switch p.Kind {
	case KindInt:
		v = math.Round(v)
	case KindBool:
		if v >= 0.5 {
			return 1
		}
		return 0
	case KindFrame:
		if len(p.Frames) == 0 {
			return 0
		}
		v = math.Round(v)
		if v < 0 {
			return 0
		}
		if last := float64(len(p.Frames) - 1); v > last {
			return last
		}
		return v
	}
	if p.Min != nil && v < *p.Min {
		v = *p.Min
	}
	if p.Max != nil && v > *p.Max {
		v = *p.Max
	}
	return v
}

// CheckValue reports why v cannot be a key of the property: it is not finite, a frame property keyed with a number, a bool key other than 0 or 1, a fractional int key or one a Go int cannot hold, or outside the range.
func (p Property) CheckValue(v float64) error {
	if p.Kind == KindFrame {
		return fmt.Errorf("animclip: property %q takes frame names, not numbers", p.Name)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("animclip: property %q key %v must be finite", p.Name, v)
	}
	switch p.Kind {
	case KindBool:
		if v != 0 && v != 1 {
			return fmt.Errorf("animclip: property %q is a bool and its keys must be 0 or 1, got %v", p.Name, v)
		}
	case KindInt:
		if v != math.Trunc(v) {
			return fmt.Errorf("animclip: property %q is an int and its keys must be whole numbers, got %v", p.Name, v)
		}
		// float64(math.MinInt) is exact, and -float64(math.MinInt) is the first whole number past math.MaxInt, so the key converts to a Go int the same way on every platform.
		if v < float64(math.MinInt) || v >= -float64(math.MinInt) {
			return fmt.Errorf("animclip: property %q is an int and its key %v is outside the range a Go int holds", p.Name, v)
		}
	}
	if p.Min != nil && v < *p.Min {
		return fmt.Errorf("animclip: property %q key %v is below the minimum %v", p.Name, v, *p.Min)
	}
	if p.Max != nil && v > *p.Max {
		return fmt.Errorf("animclip: property %q key %v is above the maximum %v", p.Name, v, *p.Max)
	}
	return nil
}

// CheckFrame reports why frame cannot be a key of a KindFrame property.
func (p Property) CheckFrame(frame string) error {
	if p.Kind != KindFrame {
		return fmt.Errorf("animclip: property %q takes numbers, not frame names", p.Name)
	}
	if frame == "" {
		return fmt.Errorf("animclip: property %q frame name must not be empty", p.Name)
	}
	return nil
}

// GetFrame returns the sprite frame name on o for a KindFrame property.
func (p Property) GetFrame(o *sceneformat.ObjectDoc) (string, error) {
	if o == nil {
		return "", errors.New("animclip: nil object")
	}
	if p.Kind != KindFrame {
		return "", fmt.Errorf("animclip: property %q is not a sprite frame property", p.Name)
	}
	if o.Sprite == nil {
		return "", fmt.Errorf("animclip: object %q has no sprite", o.ID)
	}
	return o.Sprite.Frame, nil
}

// SetFrame writes frame onto o's sprite when p is a KindFrame property and o has a sprite.
func (p Property) SetFrame(o *sceneformat.ObjectDoc, frame string) {
	if o == nil || p.Kind != KindFrame || o.Sprite == nil || frame == "" {
		return
	}
	o.Sprite.Frame = frame
}

// Get returns the property's value on o as a number: an object field's value (1 for an unset scale or alpha), or a component field's value with reg's default when o leaves it unset. A bool reads as 0 or 1.
func (p Property) Get(o *sceneformat.ObjectDoc, reg *sceneformat.ComponentRegistry) (float64, error) {
	if o == nil {
		return 0, errors.New("animclip: nil object")
	}
	if p.Kind == KindFrame {
		return 0, fmt.Errorf("animclip: property %q holds a frame name, not a number", p.Name)
	}
	if p.IsComponent() {
		var data map[string]any
		if raw, ok := o.Components[p.Component]; ok {
			m, ok := raw.(map[string]any)
			if !ok {
				return 0, fmt.Errorf("animclip: object %q component %q is not a mapping of fields", o.ID, p.Component)
			}
			data = m
		}
		if reg == nil {
			return 0, fmt.Errorf("animclip: property %q is a component field, which needs a component registry", p.Name)
		}
		resolved, err := reg.Resolve(p.Component, data)
		if err != nil {
			return 0, err
		}
		switch v := resolved[p.Field].(type) {
		case float64:
			return v, nil
		case int:
			return float64(v), nil
		case bool:
			return boolNumber(v), nil
		default:
			return 0, fmt.Errorf("animclip: property %q holds %T, not a number", p.Name, v)
		}
	}
	switch p.Name {
	case "x":
		return float64(o.X), nil
	case "y":
		return float64(o.Y), nil
	case "rotation":
		return float64(o.Rotation), nil
	case "scaleX":
		return float64(o.EffectiveScaleX()), nil
	case "scaleY":
		return float64(o.EffectiveScaleY()), nil
	case "alpha":
		return float64(o.EffectiveAlpha()), nil
	case "originX":
		return float64(o.OriginX), nil
	case "originY":
		return float64(o.OriginY), nil
	case "flipX":
		return boolNumber(o.FlipX), nil
	case "flipY":
		return boolNumber(o.FlipY), nil
	case "posZ":
		return float64(o.PosZ), nil
	case "rotX":
		return float64(o.RotX), nil
	case "rotY":
		return float64(o.RotY), nil
	case "rotZ":
		return float64(o.RotZ), nil
	case "scaleZ":
		return float64(o.EffectiveScaleZ()), nil
	}
	return 0, fmt.Errorf("animclip: unknown object property %q", p.Name)
}

// Set writes v to the property on o after Convert: a float32 object field, a bool flip, or a component field stored as float64, int or bool, creating the component's mapping when o has none. A NaN v writes nothing.
func (p Property) Set(o *sceneformat.ObjectDoc, v float64) {
	if o == nil || math.IsNaN(v) {
		return
	}
	if p.Kind == KindFrame {
		p.SetFrame(o, p.Frame(v))
		return
	}
	v = p.Convert(v)
	if p.IsComponent() {
		if o.Components == nil {
			o.Components = make(map[string]any)
		}
		m, ok := o.Components[p.Component].(map[string]any)
		if !ok {
			m = make(map[string]any)
			o.Components[p.Component] = m
		}
		switch p.Kind {
		case KindInt:
			m[p.Field] = int(v)
		case KindBool:
			m[p.Field] = v == 1
		default:
			m[p.Field] = v
		}
		return
	}
	f := float32(v)
	switch p.Name {
	case "x":
		o.X = f
	case "y":
		o.Y = f
	case "rotation":
		o.Rotation = f
	case "scaleX":
		o.ScaleX = &f
	case "scaleY":
		o.ScaleY = &f
	case "alpha":
		o.Alpha = &f
	case "originX":
		o.OriginX = f
	case "originY":
		o.OriginY = f
	case "flipX":
		o.FlipX = v == 1
	case "flipY":
		o.FlipY = v == 1
	case "posZ":
		o.PosZ = f
	case "rotX":
		o.RotX = f
	case "rotY":
		o.RotY = f
	case "rotZ":
		o.RotZ = f
	case "scaleZ":
		o.ScaleZ = &f
	}
}

func boolNumber(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// Check checks clip against reg: every track's property must resolve with Lookup, KindFrame tracks must hold valid frame keys, and numeric tracks must hold keys whose values their property can take (CheckValue). The error names the track's target and property.
func Check(clip *resource.Animation, reg *sceneformat.ComponentRegistry) error {
	if clip == nil {
		return errors.New("animclip: nil clip")
	}
	if err := clip.Validate(); err != nil {
		return err
	}
	for i := range clip.Tracks {
		tr := &clip.Tracks[i]
		p, err := Lookup(reg, tr.Property)
		if err != nil {
			return fmt.Errorf("animclip: track %d (target %q): %s", i, tr.Target, strings.TrimPrefix(err.Error(), "animclip: "))
		}
		if p.Kind == KindFrame {
			if len(tr.Frames) == 0 {
				return fmt.Errorf("animclip: track %d (target %q): property %q requires frame keys, not numeric keys", i, tr.Target, tr.Property)
			}
			for j, fk := range tr.Frames {
				if err := p.CheckFrame(fk.Frame); err != nil {
					return fmt.Errorf("animclip: track %d (target %q) frame %d: %s", i, tr.Target, j, strings.TrimPrefix(err.Error(), "animclip: "))
				}
			}
			continue
		}
		if len(tr.Frames) > 0 {
			return fmt.Errorf("animclip: track %d (target %q): property %q requires numeric keys, not frame keys", i, tr.Target, tr.Property)
		}
		for j, k := range tr.Keys {
			if err := p.CheckValue(KeyValue(k.Value)); err != nil {
				return fmt.Errorf("animclip: track %d (target %q) key %d: %s", i, tr.Target, j, strings.TrimPrefix(err.Error(), "animclip: "))
			}
		}
	}
	return nil
}

// KeyValue widens a key's float32 value or time to float64 by its shortest decimal form, so a key written 0.1 plays as 0.1 rather than 0.10000000149011612.
func KeyValue(v float32) float64 {
	f, err := strconv.ParseFloat(strconv.FormatFloat(float64(v), 'g', -1, 32), 64)
	if err != nil {
		return float64(v)
	}
	return f
}
