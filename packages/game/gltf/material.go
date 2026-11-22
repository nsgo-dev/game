package gltf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// AlphaMode controls how a material's alpha channel is interpreted.
type AlphaMode string

const (
	AlphaOpaque AlphaMode = "OPAQUE"
	AlphaMask   AlphaMode = "MASK"
	AlphaBlend  AlphaMode = "BLEND"
)

// FilterMode defines glTF texture minification/magnification filtering modes.
type FilterMode int

const (
	FilterNearest              FilterMode = 9728
	FilterLinear               FilterMode = 9729
	FilterNearestMipmapNearest FilterMode = 9984
	FilterLinearMipmapNearest  FilterMode = 9985
	FilterNearestMipmapLinear  FilterMode = 9986
	FilterLinearMipmapLinear   FilterMode = 9987
)

// WrapMode defines glTF texture coordinate wrapping modes.
type WrapMode int

const (
	WrapClampToEdge    WrapMode = 33071
	WrapMirroredRepeat WrapMode = 33648
	WrapRepeat         WrapMode = 10497
)

// Sampler describes filtering and wrapping properties for a texture.
type Sampler struct {
	Name      string
	MagFilter FilterMode
	MinFilter FilterMode
	WrapS     WrapMode
	WrapT     WrapMode
	Extras    json.RawMessage
}

// Texture binds an Image source with an optional Sampler.
// Sampler and Source are -1 when unset.
type Texture struct {
	Name       string
	Sampler    int
	Source     int
	Extras     json.RawMessage
	Extensions map[string]json.RawMessage
}

// Image represents an embedded or external texture image.
// BufferView is -1 when the image is referenced via URI.
type Image struct {
	Name       string
	URI        string
	MimeType   string
	BufferView int
	Extras     json.RawMessage
}

// TextureTransform represents the Khronos KHR_texture_transform extension.
// Transforms are applied in exact spec order: T(Offset) * R(Rotation) * S(Scale)
// in glTF UV space (origin top-left, +U right, +V down, positive rotation
// counter-clockwise).
type TextureTransform struct {
	Offset   [2]float32
	Rotation float32
	Scale    [2]float32
	TexCoord *int
}

// ApplyUV transforms texture coordinates (u, v) using the exact
// KHR_texture_transform matrix order M = T * R * S:
//
//	su = u * Scale[0]
//	sv = v * Scale[1]
//	u' =  cos(Rotation)*su + sin(Rotation)*sv + Offset[0]
//	v' = -sin(Rotation)*su + cos(Rotation)*sv + Offset[1]
func (t TextureTransform) ApplyUV(u, v float32) (float32, float32) {
	su := u * t.Scale[0]
	sv := v * t.Scale[1]
	s, c := math.Sincos(float64(t.Rotation))
	sf, cf := float32(s), float32(c)
	return cf*su + sf*sv + t.Offset[0], -sf*su + cf*sv + t.Offset[1]
}

// Matrix returns the column-major 3x3 homogeneous UV transform matrix T * R * S
// matching the Khronos KHR_texture_transform specification.
func (t TextureTransform) Matrix() math3d.Mat3 {
	s, c := math.Sincos(float64(t.Rotation))
	sf, cf := float32(s), float32(c)
	sx, sy := t.Scale[0], t.Scale[1]
	return math3d.Mat3{
		cf * sx, -sf * sx, 0,
		sf * sy, cf * sy, 0,
		t.Offset[0], t.Offset[1], 1,
	}
}

// TextureRef references a Texture in Document.Textures along with UV channel,
// normal scale / occlusion strength, and optional KHR_texture_transform.
type TextureRef struct {
	TextureIndex int
	TexCoord     int
	Scale        float32
	Strength     float32
	Transform    *TextureTransform
}

// EffectiveTexCoord returns Transform.TexCoord if KHR_texture_transform overrides
// the UV set, otherwise TexCoord.
func (r TextureRef) EffectiveTexCoord() int {
	if r.Transform != nil && r.Transform.TexCoord != nil {
		return *r.Transform.TexCoord
	}
	return r.TexCoord
}

// Material defines a glTF 2.0 PBR metallic-roughness material.
type Material struct {
	Name                     string
	BaseColorFactor          math3d.Vec4
	BaseColorTexture         *TextureRef
	MetallicFactor           float32
	RoughnessFactor          float32
	MetallicRoughnessTexture *TextureRef
	NormalTexture            *TextureRef
	OcclusionTexture         *TextureRef
	EmissiveFactor           math3d.Vec3
	EmissiveTexture          *TextureRef
	AlphaMode                AlphaMode
	AlphaCutoff              float32
	DoubleSided              bool
	Unlit                    bool // KHR_materials_unlit
	Extras                   json.RawMessage
	// Extensions keeps unsupported optional extension payloads by name.
	Extensions map[string]json.RawMessage
}

// MaterialVariant represents an entry in KHR_materials_variants.
type MaterialVariant struct {
	Name   string
	Extras json.RawMessage
}

// MaterialForPrimitive returns the material index to use for prim under the
// specified KHR_materials_variants variantIndex. If variantIndex is negative,
// out of range, or unmapped on prim, it falls back to prim.Material.
func (d *Document) MaterialForPrimitive(prim Primitive, variantIndex int) int {
	if variantIndex >= 0 && variantIndex < len(d.Variants) && prim.VariantMappings != nil {
		if matIdx, ok := prim.VariantMappings[variantIndex]; ok {
			return matIdx
		}
	}
	return prim.Material
}

// ImageBytes returns the raw encoded bytes (e.g. PNG or JPEG) for image index.
func (d *Document) ImageBytes(index int) ([]byte, error) {
	if index < 0 || index >= len(d.Images) {
		return nil, fmt.Errorf("gltf: image index %d out of range [0, %d)", index, len(d.Images))
	}
	img := d.Images[index]
	if img.BufferView >= 0 {
		if img.BufferView >= len(d.BufferViews) {
			return nil, fmt.Errorf("gltf: image %d bufferView %d out of range", index, img.BufferView)
		}
		bv := d.BufferViews[img.BufferView]
		if bv.Buffer < 0 || bv.Buffer >= len(d.Buffers) {
			return nil, fmt.Errorf("gltf: image %d buffer %d out of range", index, bv.Buffer)
		}
		buf := d.Buffers[bv.Buffer].Data
		end := uint64(bv.ByteOffset) + uint64(bv.ByteLength)
		if bv.ByteOffset < 0 || bv.ByteLength < 0 || end > uint64(len(buf)) {
			return nil, fmt.Errorf("gltf: image %d bufferView out of buffer bounds", index)
		}
		return buf[bv.ByteOffset : bv.ByteOffset+bv.ByteLength], nil
	}
	if strings.HasPrefix(img.URI, "data:") {
		return decodeDataURI(img.URI)
	}
	if img.URI != "" {
		if d.resolver == nil {
			return nil, fmt.Errorf("gltf: image %d external URI %q requires a BufferResolver", index, img.URI)
		}
		return d.resolver(img.URI)
	}
	return nil, errors.New("gltf: image has neither bufferView nor URI")
}

// DecodeImage decodes the PNG or JPEG image at index from its bufferView,
// data URI, or external URI (resolved via the loader's BufferResolver / fs.FS).
func (d *Document) DecodeImage(index int) (image.Image, error) {
	raw, err := d.ImageBytes(index)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("gltf: decode image %d: %w", index, err)
	}
	return img, nil
}
