package resource

import (
	"fmt"
	"io/fs"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// Material describes how a surface draws: its color and texture, how opaque it is and how it blends with what is beneath. 2D sprites use Opacity and Blend; 3D renderers also read the PBR fields.
type Material struct {
	Header `yaml:",inline"`
	// Name is an optional human-readable name.
	Name string `yaml:"name,omitempty"`
	// BaseColor is a CSS color parsed with core.ParseColor; empty is opaque white.
	BaseColor string `yaml:"baseColor,omitempty"`
	// Texture is an optional slash-separated path to the surface's image, relative to the project root.
	Texture string `yaml:"texture,omitempty"`
	// Opacity multiplies the alpha of everything drawn with the material, from 0 to 1; nil is 1.
	Opacity *float32 `yaml:"opacity,omitempty"`
	// Blend is how the surface mixes with what is beneath (see BlendModes); empty is "sourceOver".
	Blend string `yaml:"blend,omitempty"`
	// Metallic is the PBR metalness from 0 to 1.
	Metallic float32 `yaml:"metallic,omitempty"`
	// Roughness is the PBR roughness from 0 to 1; nil is 1.
	Roughness *float32 `yaml:"roughness,omitempty"`
	// Emissive is an optional CSS color the surface gives off; empty is none.
	Emissive string `yaml:"emissive,omitempty"`
	// DoubleSided draws a 3D surface's back faces too.
	DoubleSided bool `yaml:"doubleSided,omitempty"`
	// Unlit draws a 3D surface in its base color without lighting.
	Unlit bool `yaml:"unlit,omitempty"`
}

var blendModes = map[string]core.CompositeOp{
	"sourceOver":      core.CompositeSourceOver,
	"sourceIn":        core.CompositeSourceIn,
	"sourceOut":       core.CompositeSourceOut,
	"sourceAtop":      core.CompositeSourceAtop,
	"destinationOver": core.CompositeDestinationOver,
	"destinationIn":   core.CompositeDestinationIn,
	"destinationOut":  core.CompositeDestinationOut,
	"destinationAtop": core.CompositeDestinationAtop,
	"lighter":         core.CompositeLighter,
	"copy":            core.CompositeCopy,
	"xor":             core.CompositeXor,
	"multiply":        core.CompositeMultiply,
	"screen":          core.CompositeScreen,
	"overlay":         core.CompositeOverlay,
	"darken":          core.CompositeDarken,
	"lighten":         core.CompositeLighten,
	"colorDodge":      core.CompositeColorDodge,
	"colorBurn":       core.CompositeColorBurn,
	"hardLight":       core.CompositeHardLight,
	"softLight":       core.CompositeSoftLight,
	"difference":      core.CompositeDifference,
	"exclusion":       core.CompositeExclusion,
	"hue":             core.CompositeHue,
	"saturation":      core.CompositeSaturation,
	"color":           core.CompositeColor,
	"luminosity":      core.CompositeLuminosity,
}

// BlendModes returns the names Material.Blend accepts, sorted: the canvas composite operations in camel case, such as "sourceOver", "multiply" and "lighter".
func BlendModes() []string {
	names := make([]string, 0, len(blendModes))
	for name := range blendModes {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Kind returns KindMaterial.
func (m *Material) Kind() Kind { return KindMaterial }

// Validate checks the header, colors, ranges and blend mode.
func (m *Material) Validate() error {
	if m == nil {
		return fmt.Errorf("resource: nil material")
	}
	if err := m.Header.validate(KindMaterial); err != nil {
		return err
	}
	if m.BaseColor != "" {
		if _, err := core.ParseColor(m.BaseColor); err != nil {
			return fmt.Errorf("resource: material baseColor %q: %w", m.BaseColor, err)
		}
	}
	if m.Emissive != "" {
		if _, err := core.ParseColor(m.Emissive); err != nil {
			return fmt.Errorf("resource: material emissive %q: %w", m.Emissive, err)
		}
	}
	if m.Texture != "" && (!fs.ValidPath(m.Texture) || m.Texture == ".") {
		return fmt.Errorf("resource: material texture %q must be a slash-separated path relative to the project root", m.Texture)
	}
	if m.Opacity != nil {
		if err := checkUnit("material opacity", *m.Opacity); err != nil {
			return err
		}
	}
	if m.Blend != "" {
		if _, ok := blendModes[m.Blend]; !ok {
			return fmt.Errorf("resource: material blend %q is not one of %v", m.Blend, BlendModes())
		}
	}
	if err := checkUnit("material metallic", m.Metallic); err != nil {
		return err
	}
	if m.Roughness != nil {
		if err := checkUnit("material roughness", *m.Roughness); err != nil {
			return err
		}
	}
	return nil
}

// Color returns the parsed base color, opaque white when BaseColor is empty or invalid.
func (m *Material) Color() core.Color {
	if m == nil || m.BaseColor == "" {
		return core.RGB(0xffffff)
	}
	c, err := core.ParseColor(m.BaseColor)
	if err != nil {
		return core.RGB(0xffffff)
	}
	return c
}

// EmissiveColor returns the parsed emissive color and whether the material has one.
func (m *Material) EmissiveColor() (core.Color, bool) {
	if m == nil || m.Emissive == "" {
		return 0, false
	}
	c, err := core.ParseColor(m.Emissive)
	if err != nil {
		return 0, false
	}
	return c, true
}

// EffectiveOpacity returns Opacity, 1 when it is nil.
func (m *Material) EffectiveOpacity() float32 {
	if m == nil || m.Opacity == nil {
		return 1
	}
	return *m.Opacity
}

// EffectiveRoughness returns Roughness, 1 when it is nil.
func (m *Material) EffectiveRoughness() float32 {
	if m == nil || m.Roughness == nil {
		return 1
	}
	return *m.Roughness
}

// BlendOp returns the composite operation Blend names, core.CompositeSourceOver when it is empty or unknown.
func (m *Material) BlendOp() core.CompositeOp {
	if m == nil {
		return core.CompositeSourceOver
	}
	if op, ok := blendModes[m.Blend]; ok {
		return op
	}
	return core.CompositeSourceOver
}
