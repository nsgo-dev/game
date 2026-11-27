package gltf

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nsgo-dev/game/pkg/math3d"
)

const (
	// ExtMOZLightmap is the Mozilla Hubs / Blender glTF material extension name for baked lightmaps.
	ExtMOZLightmap = "MOZ_lightmap"

	// ExtLightmap is the generic EXT_lightmap glTF material extension name for baked lightmaps.
	ExtLightmap = "EXT_lightmap"
)

// LightmapRef describes a baked lightmap binding extracted from a glTF material's MOZ_lightmap extension, EXT_lightmap extension, or extras.lightmap metadata.
type LightmapRef struct {
	// Texture points to the lightmap texture in Document.Textures and its UV channel (defaulting to 1 for TEXCOORD_1). A KHR_texture_transform on the lightmap is folded into UVScale and UVOffset, so Texture.Transform carries only its texCoord override (if any) and passing Texture to [Document.TransformTexCoords] never applies the scale and offset a second time.
	Texture TextureRef
	// Intensity scales the decoded lightmap irradiance (defaults to 1).
	Intensity float32
	// Encoding is an optional encoding hint such as "linear", "srgb", "rgbm", or "rgbd", normalized to lower case. Empty when unspecified.
	Encoding string
	// RGBMMaxRange is an optional peak HDR scale hint for RGBM lightmaps (0 when unspecified, letting the renderer apply its default range).
	RGBMMaxRange float32
	// UVScale scales lightmap UVs when mapping into a shared lightmap atlas (defaults to [1, 1]). UVScale and UVOffset are the authoritative atlas transform: pass them to render3d's LightmapMaterial.SetUVTransform. They come from, in increasing precedence, the lightmap's KHR_texture_transform (scale and offset; a non-zero rotation is refused), a scaleOffset [sx, sy, ox, oy] field, and separate uvScale and uvOffset fields.
	UVScale [2]float32
	// UVOffset offsets scaled lightmap UVs when mapping into a shared lightmap atlas (defaults to [0, 0]). See UVScale for precedence.
	UVOffset [2]float32
}

// DefaultLightmapRef returns a LightmapRef referencing textureIndex on TEXCOORD_1 with unit intensity and identity atlas UV transform.
func DefaultLightmapRef(textureIndex int) LightmapRef {
	return LightmapRef{
		Texture: TextureRef{
			TextureIndex: textureIndex,
			TexCoord:     1,
			Scale:        1,
			Strength:     1,
		},
		Intensity: 1,
		UVScale:   [2]float32{1, 1},
	}
}

type rawLightmapExt struct {
	Index        *int                       `json:"index"`
	Texture      *int                       `json:"texture"`
	TexCoord     *int                       `json:"texCoord"`
	Intensity    *float32                   `json:"intensity"`
	Encoding     string                     `json:"encoding"`
	RGBMMaxRange *float32                   `json:"rgbmMaxRange"`
	UVScale      json.RawMessage            `json:"uvScale"`
	UVOffset     json.RawMessage            `json:"uvOffset"`
	ScaleOffset  json.RawMessage            `json:"scaleOffset"`
	Extensions   map[string]json.RawMessage `json:"extensions"`
}

// MaterialLightmap extracts baked lightmap metadata from d.Materials[materialIndex], checking MOZ_lightmap, EXT_lightmap, and extras.lightmap in that order. It returns (nil, nil) when the material carries no lightmap metadata.
func (d *Document) MaterialLightmap(materialIndex int) (*LightmapRef, error) {
	if d == nil {
		return nil, fmt.Errorf("gltf: nil document")
	}
	if materialIndex < 0 || materialIndex >= len(d.Materials) {
		return nil, fmt.Errorf("gltf: material index %d out of range [0, %d)", materialIndex, len(d.Materials))
	}
	mat := d.Materials[materialIndex]

	var (
		source  string
		payload json.RawMessage
	)
	if raw, ok := mat.Extensions[ExtMOZLightmap]; ok {
		source = ExtMOZLightmap
		payload = raw
	} else if raw, ok := mat.Extensions[ExtLightmap]; ok {
		source = ExtLightmap
		payload = raw
	} else if len(mat.Extras) > 0 && isJSONObject(mat.Extras) {
		var extras map[string]json.RawMessage
		if err := json.Unmarshal(mat.Extras, &extras); err == nil {
			if raw, ok := extras["lightmap"]; ok && !isJSONNull(raw) {
				source = "extras.lightmap"
				payload = raw
			}
		}
	}
	if len(payload) == 0 {
		return nil, nil
	}
	if !isJSONObject(payload) {
		return nil, fmt.Errorf("gltf: material %d %s must be a JSON object", materialIndex, source)
	}

	var raw rawLightmapExt
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("gltf: material %d %s: %w", materialIndex, source, err)
	}

	texIdxPtr := raw.Index
	if texIdxPtr == nil && source == "extras.lightmap" {
		texIdxPtr = raw.Texture
	}
	if texIdxPtr == nil {
		return nil, fmt.Errorf("gltf: material %d %s missing required texture index", materialIndex, source)
	}
	texIdx := *texIdxPtr
	if texIdx < 0 || texIdx >= len(d.Textures) {
		return nil, fmt.Errorf("gltf: material %d %s texture index %d out of range [0, %d)", materialIndex, source, texIdx, len(d.Textures))
	}

	ref := DefaultLightmapRef(texIdx)
	if raw.TexCoord != nil {
		if *raw.TexCoord < 0 {
			return nil, fmt.Errorf("gltf: material %d %s has negative texCoord %d", materialIndex, source, *raw.TexCoord)
		}
		ref.Texture.TexCoord = *raw.TexCoord
	}
	if raw.Intensity != nil {
		if !finite32(*raw.Intensity) || *raw.Intensity < 0 {
			return nil, fmt.Errorf("gltf: material %d %s intensity %v must be finite and non-negative", materialIndex, source, *raw.Intensity)
		}
		ref.Intensity = *raw.Intensity
	}
	if raw.Encoding != "" {
		ref.Encoding = strings.ToLower(strings.TrimSpace(raw.Encoding))
	}
	if raw.RGBMMaxRange != nil {
		if !finite32(*raw.RGBMMaxRange) || *raw.RGBMMaxRange <= 0 {
			return nil, fmt.Errorf("gltf: material %d %s rgbmMaxRange %v must be finite and positive", materialIndex, source, *raw.RGBMMaxRange)
		}
		ref.RGBMMaxRange = *raw.RGBMMaxRange
	}

	if extData, ok := raw.Extensions[extTextureTransform]; ok {
		tt, err := parseTextureTransform(extData)
		if err != nil {
			return nil, fmt.Errorf("gltf: material %d %s: %w", materialIndex, source, err)
		}
		if tt.Rotation != 0 {
			return nil, fmt.Errorf("gltf: material %d %s %s rotation %v is not supported for lightmaps; bake the atlas without rotated charts", materialIndex, source, extTextureTransform, tt.Rotation)
		}
		ref.UVScale = tt.Scale
		ref.UVOffset = tt.Offset
		if tt.TexCoord != nil {
			keep := DefaultTextureTransform()
			keep.TexCoord = tt.TexCoord
			ref.Texture.Transform = &keep
		}
	}

	if len(raw.ScaleOffset) > 0 {
		so, err := parseLightmapVec4(source, "scaleOffset", raw.ScaleOffset)
		if err != nil {
			return nil, fmt.Errorf("gltf: material %d %w", materialIndex, err)
		}
		ref.UVScale = [2]float32{so[0], so[1]}
		ref.UVOffset = [2]float32{so[2], so[3]}
	}
	if len(raw.UVScale) > 0 {
		sc, err := parseLightmapVec2(source, "uvScale", raw.UVScale)
		if err != nil {
			return nil, fmt.Errorf("gltf: material %d %w", materialIndex, err)
		}
		ref.UVScale = sc
	}
	if len(raw.UVOffset) > 0 {
		off, err := parseLightmapVec2(source, "uvOffset", raw.UVOffset)
		if err != nil {
			return nil, fmt.Errorf("gltf: material %d %w", materialIndex, err)
		}
		ref.UVOffset = off
	}

	return &ref, nil
}

// PrimitiveLightmapUVs returns the lightmap UV channel for prim: prim.TexCoords1 (TEXCOORD_1) when present, falling back to prim.TexCoords0 (TEXCOORD_0) when TEXCOORD_1 is absent. The second return value reports whether the fallback path was used.
func PrimitiveLightmapUVs(prim Primitive) (uvs []math3d.Vec2, usedFallback bool) {
	if len(prim.TexCoords1) > 0 {
		return prim.TexCoords1, false
	}
	if len(prim.TexCoords0) > 0 {
		return prim.TexCoords0, true
	}
	return nil, true
}

// LightmapTexCoords resolves the baked lightmap UVs for prim using ref (or TEXCOORD_1 when ref is nil). When TEXCOORD_1 is requested but absent on prim, it falls back to prim.TexCoords0 and reports usedFallback = true. A primitive with neither TEXCOORD_1 nor TEXCOORD_0 is an error, since every vertex would sample the same lightmap texel.
func (d *Document) LightmapTexCoords(prim Primitive, ref *LightmapRef) (uvs []math3d.Vec2, usedFallback bool, err error) {
	if d == nil {
		return nil, false, fmt.Errorf("gltf: nil document")
	}
	tc := 1
	if ref != nil {
		tc = ref.Texture.EffectiveTexCoord()
	}
	if tc < 0 {
		return nil, false, fmt.Errorf("gltf: negative lightmap texCoord %d", tc)
	}
	if tc == 1 && len(prim.TexCoords1) == 0 {
		if _, hasAttr := prim.Attributes["TEXCOORD_1"]; !hasAttr {
			if len(prim.TexCoords0) == 0 {
				return nil, false, fmt.Errorf("gltf: primitive has neither TEXCOORD_1 nor TEXCOORD_0 for its lightmap")
			}
			return prim.TexCoords0, true, nil
		}
	}
	coords, err := d.PrimitiveTexCoords(prim, tc)
	if err != nil {
		return nil, false, err
	}
	return coords, false, nil
}

func parseLightmapVec2(source, field string, raw json.RawMessage) ([2]float32, error) {
	if isJSONNull(raw) {
		return [2]float32{}, fmt.Errorf("%s %s must be a 2-element number array", source, field)
	}
	var elems []*float32
	if err := json.Unmarshal(raw, &elems); err != nil {
		return [2]float32{}, fmt.Errorf("parse %s %s: %w", source, field, err)
	}
	if len(elems) != 2 || elems[0] == nil || elems[1] == nil {
		return [2]float32{}, fmt.Errorf("%s %s must have 2 finite numbers", source, field)
	}
	v0, v1 := *elems[0], *elems[1]
	if !finite32(v0) || !finite32(v1) {
		return [2]float32{}, fmt.Errorf("%s %s elements must be finite numbers", source, field)
	}
	return [2]float32{v0, v1}, nil
}

func parseLightmapVec4(source, field string, raw json.RawMessage) ([4]float32, error) {
	if isJSONNull(raw) {
		return [4]float32{}, fmt.Errorf("%s %s must be a 4-element number array", source, field)
	}
	var elems []*float32
	if err := json.Unmarshal(raw, &elems); err != nil {
		return [4]float32{}, fmt.Errorf("parse %s %s: %w", source, field, err)
	}
	if len(elems) != 4 {
		return [4]float32{}, fmt.Errorf("%s %s must have 4 finite numbers", source, field)
	}
	var out [4]float32
	for i, p := range elems {
		if p == nil || !finite32(*p) {
			return [4]float32{}, fmt.Errorf("%s %s element %d must be a finite number", source, field, i)
		}
		out[i] = *p
	}
	return out, nil
}
