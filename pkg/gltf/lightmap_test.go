package gltf_test

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/math3d"
)

func buildLightmapTestGLTF(t *testing.T, includeUV1 bool, includeUV2 bool, materials []map[string]any, numTextures int) []byte {
	t.Helper()
	// 3 vertices (1 triangle): positions (36B), uv0 (24B), optional uv1 (24B), optional uv2 (24B).
	var buf []byte
	appendFloats := func(fs ...float32) (int, int) {
		off := len(buf)
		for _, f := range fs {
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], math.Float32bits(f))
			buf = append(buf, b[:]...)
		}
		return off, len(buf) - off
	}

	posOff, posLen := appendFloats(
		0, 0, 0,
		1, 0, 0,
		0, 1, 0,
	)
	uv0Off, uv0Len := appendFloats(
		0, 0,
		2, 0,
		0, 2,
	)

	bufferViews := []map[string]any{
		{"buffer": 0, "byteOffset": posOff, "byteLength": posLen},
		{"buffer": 0, "byteOffset": uv0Off, "byteLength": uv0Len},
	}
	accessors := []map[string]any{
		{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3", "min": []float32{0, 0, 0}, "max": []float32{1, 1, 0}},
		{"bufferView": 1, "componentType": 5126, "count": 3, "type": "VEC2"},
	}
	attrs := map[string]int{
		"POSITION":   0,
		"TEXCOORD_0": 1,
	}

	if includeUV1 {
		uv1Off, uv1Len := appendFloats(
			0.1, 0.2,
			0.9, 0.2,
			0.1, 0.8,
		)
		bvIdx := len(bufferViews)
		accIdx := len(accessors)
		bufferViews = append(bufferViews, map[string]any{"buffer": 0, "byteOffset": uv1Off, "byteLength": uv1Len})
		accessors = append(accessors, map[string]any{"bufferView": bvIdx, "componentType": 5126, "count": 3, "type": "VEC2"})
		attrs["TEXCOORD_1"] = accIdx
	}

	if includeUV2 {
		uv2Off, uv2Len := appendFloats(
			0.25, 0.25,
			0.75, 0.25,
			0.25, 0.75,
		)
		bvIdx := len(bufferViews)
		accIdx := len(accessors)
		bufferViews = append(bufferViews, map[string]any{"buffer": 0, "byteOffset": uv2Off, "byteLength": uv2Len})
		accessors = append(accessors, map[string]any{"bufferView": bvIdx, "componentType": 5126, "count": 3, "type": "VEC2"})
		attrs["TEXCOORD_2"] = accIdx
	}

	textures := make([]map[string]any, numTextures)
	for i := range numTextures {
		textures[i] = map[string]any{}
	}

	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"buffers": []map[string]any{{
			"byteLength": len(buf),
			"uri":        "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(buf),
		}},
		"bufferViews": bufferViews,
		"accessors":   accessors,
		"textures":    textures,
		"materials":   materials,
		"meshes": []map[string]any{{
			"name": "baked_mesh",
			"primitives": []map[string]any{{
				"attributes": attrs,
				"material":   0,
			}},
		}},
		"nodes":  []map[string]any{{"mesh": 0}},
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"scene":  0,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal glTF: %v", err)
	}
	return raw
}

func TestMaterialLightmapMOZAndEXTAndExtras(t *testing.T) {
	materials := []map[string]any{
		{
			"name": "moz_mat",
			"extensions": map[string]any{
				"MOZ_lightmap": map[string]any{
					"index":     1,
					"intensity": 1.5,
					"extensions": map[string]any{
						"KHR_texture_transform": map[string]any{
							"scale":  []float32{0.5, 0.25},
							"offset": []float32{0.5, 0.75},
						},
					},
				},
			},
		},
		{
			"name": "ext_mat",
			"extensions": map[string]any{
				"EXT_lightmap": map[string]any{
					"index":        0,
					"texCoord":     2,
					"intensity":    2.0,
					"encoding":     "RGBM",
					"rgbmMaxRange": 10.0,
					"uvScale":      []float32{0.25, 0.5},
					"uvOffset":     []float32{0.1, 0.2},
				},
			},
		},
		{
			"name": "extras_mat",
			"extras": map[string]any{
				"lightmap": map[string]any{
					"texture":     1,
					"intensity":   0.75,
					"encoding":    "srgb",
					"scaleOffset": []float32{0.5, 0.5, 0.0, 0.5},
				},
			},
		},
		{
			"name": "plain_mat",
		},
	}

	raw := buildLightmapTestGLTF(t, true, true, materials, 2)
	doc, err := gltf.Load(raw, nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}

	// 0: MOZ_lightmap with KHR_texture_transform
	ref0, err := doc.MaterialLightmap(0)
	if err != nil {
		t.Fatalf("MaterialLightmap(0): %v", err)
	}
	if ref0 == nil {
		t.Fatal("MaterialLightmap(0) = nil, want non-nil")
	}
	if ref0.Texture.TextureIndex != 1 || ref0.Texture.EffectiveTexCoord() != 1 {
		t.Fatalf("ref0 texture = %+v, want index 1 texCoord 1", ref0.Texture)
	}
	if ref0.Intensity != 1.5 {
		t.Fatalf("ref0.Intensity = %v, want 1.5", ref0.Intensity)
	}
	if ref0.UVScale != [2]float32{0.5, 0.25} || ref0.UVOffset != [2]float32{0.5, 0.75} {
		t.Fatalf("ref0 UV transform = scale %v offset %v, want [0.5 0.25] [0.5 0.75]", ref0.UVScale, ref0.UVOffset)
	}

	// 1: EXT_lightmap with RGBM metadata & TEXCOORD_2
	ref1, err := doc.MaterialLightmap(1)
	if err != nil {
		t.Fatalf("MaterialLightmap(1): %v", err)
	}
	if ref1 == nil || ref1.Texture.TextureIndex != 0 || ref1.Texture.EffectiveTexCoord() != 2 {
		t.Fatalf("ref1 = %+v, want texture 0 texCoord 2", ref1)
	}
	if ref1.Encoding != "rgbm" || ref1.RGBMMaxRange != 10.0 || ref1.Intensity != 2.0 {
		t.Fatalf("ref1 encoding/range/intensity = %q/%v/%v", ref1.Encoding, ref1.RGBMMaxRange, ref1.Intensity)
	}
	if ref1.UVScale != [2]float32{0.25, 0.5} || ref1.UVOffset != [2]float32{0.1, 0.2} {
		t.Fatalf("ref1 UV = %v / %v", ref1.UVScale, ref1.UVOffset)
	}

	// 2: extras.lightmap with texture alias and scaleOffset
	ref2, err := doc.MaterialLightmap(2)
	if err != nil {
		t.Fatalf("MaterialLightmap(2): %v", err)
	}
	if ref2 == nil || ref2.Texture.TextureIndex != 1 || ref2.Texture.EffectiveTexCoord() != 1 {
		t.Fatalf("ref2 = %+v, want texture 1 texCoord 1", ref2)
	}
	if ref2.Encoding != "srgb" || ref2.Intensity != 0.75 {
		t.Fatalf("ref2 encoding/intensity = %q/%v", ref2.Encoding, ref2.Intensity)
	}
	if ref2.UVScale != [2]float32{0.5, 0.5} || ref2.UVOffset != [2]float32{0, 0.5} {
		t.Fatalf("ref2 UV = %v / %v", ref2.UVScale, ref2.UVOffset)
	}

	// 3: plain material -> nil, nil
	ref3, err := doc.MaterialLightmap(3)
	if err != nil || ref3 != nil {
		t.Fatalf("MaterialLightmap(3) = (%+v, %v), want (nil, nil)", ref3, err)
	}
}

func TestLightmapTexCoordsAndFallback(t *testing.T) {
	// With TEXCOORD_0, TEXCOORD_1, TEXCOORD_2.
	rawWithUV1 := buildLightmapTestGLTF(t, true, true, []map[string]any{{
		"extensions": map[string]any{
			"EXT_lightmap": map[string]any{"index": 0, "texCoord": 2},
		},
	}}, 1)
	doc, err := gltf.Load(rawWithUV1, nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}
	prim := doc.Meshes[0].Primitives[0]

	uvs, fallback := gltf.PrimitiveLightmapUVs(prim)
	if fallback {
		t.Fatal("PrimitiveLightmapUVs used fallback when TEXCOORD_1 is present")
	}
	if len(uvs) != 3 || uvs[0] != (math3d.Vec2{X: 0.1, Y: 0.2}) {
		t.Fatalf("PrimitiveLightmapUVs = %v, want TEXCOORD_1", uvs)
	}

	ref, err := doc.MaterialLightmap(0)
	if err != nil {
		t.Fatalf("MaterialLightmap: %v", err)
	}
	uv2, fallback, err := doc.LightmapTexCoords(prim, ref)
	if err != nil || fallback {
		t.Fatalf("LightmapTexCoords(texCoord=2) = (fallback=%v, err=%v)", fallback, err)
	}
	if len(uv2) != 3 || uv2[0] != (math3d.Vec2{X: 0.25, Y: 0.25}) {
		t.Fatalf("LightmapTexCoords(texCoord=2) = %v, want TEXCOORD_2", uv2)
	}

	// Without TEXCOORD_1: falls back cleanly to TEXCOORD_0.
	rawNoUV1 := buildLightmapTestGLTF(t, false, false, []map[string]any{{}}, 1)
	docNoUV1, err := gltf.Load(rawNoUV1, nil)
	if err != nil {
		t.Fatalf("gltf.Load no UV1: %v", err)
	}
	primNoUV1 := docNoUV1.Meshes[0].Primitives[0]

	fbUVs, usedFB := gltf.PrimitiveLightmapUVs(primNoUV1)
	if !usedFB {
		t.Fatal("PrimitiveLightmapUVs should report fallback when TEXCOORD_1 is missing")
	}
	if len(fbUVs) != 3 || fbUVs[1] != (math3d.Vec2{X: 2, Y: 0}) {
		t.Fatalf("fallback UVs = %v, want TEXCOORD_0", fbUVs)
	}

	fbUVs2, usedFB2, err := docNoUV1.LightmapTexCoords(primNoUV1, nil)
	if err != nil || !usedFB2 {
		t.Fatalf("LightmapTexCoords fallback = (fallback=%v, err=%v)", usedFB2, err)
	}
	if len(fbUVs2) != 3 || fbUVs2[1] != (math3d.Vec2{X: 2, Y: 0}) {
		t.Fatalf("LightmapTexCoords fallback UVs = %v", fbUVs2)
	}

	// Neither TEXCOORD_1 nor TEXCOORD_0: an error instead of every vertex sampling texel (0, 0).
	bare := gltf.Primitive{Positions: []math3d.Vec3{{}, {X: 1}, {Y: 1}}}
	if uvs, _, err := docNoUV1.LightmapTexCoords(bare, nil); err == nil {
		t.Fatalf("LightmapTexCoords without any UV set = %v, want error", uvs)
	}
}

func TestMaterialLightmapTransformPrecedence(t *testing.T) {
	materials := []map[string]any{
		{
			// KHR_texture_transform, then scaleOffset, then uvScale: uvScale wins for scale, scaleOffset for offset.
			"extensions": map[string]any{
				"EXT_lightmap": map[string]any{
					"index":       0,
					"scaleOffset": []float32{0.5, 0.5, 0.25, 0.25},
					"uvScale":     []float32{0.125, 0.25},
					"extensions": map[string]any{
						"KHR_texture_transform": map[string]any{"scale": []float32{2, 2}, "offset": []float32{0.9, 0.9}},
					},
				},
			},
		},
		{
			// KHR_texture_transform alone, with a texCoord override.
			"extensions": map[string]any{
				"MOZ_lightmap": map[string]any{
					"index": 0,
					"extensions": map[string]any{
						"KHR_texture_transform": map[string]any{"scale": []float32{0.5, 0.5}, "offset": []float32{0.5, 0}, "texCoord": 2},
					},
				},
			},
		},
	}
	doc, err := gltf.Load(buildLightmapTestGLTF(t, true, true, materials, 1), nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}
	ref0, err := doc.MaterialLightmap(0)
	if err != nil {
		t.Fatalf("MaterialLightmap(0): %v", err)
	}
	if ref0.UVScale != [2]float32{0.125, 0.25} || ref0.UVOffset != [2]float32{0.25, 0.25} {
		t.Fatalf("precedence: scale %v offset %v, want [0.125 0.25] [0.25 0.25]", ref0.UVScale, ref0.UVOffset)
	}

	ref1, err := doc.MaterialLightmap(1)
	if err != nil {
		t.Fatalf("MaterialLightmap(1): %v", err)
	}
	if ref1.UVScale != [2]float32{0.5, 0.5} || ref1.UVOffset != [2]float32{0.5, 0} {
		t.Fatalf("KHR_texture_transform: scale %v offset %v", ref1.UVScale, ref1.UVOffset)
	}
	if ref1.Texture.EffectiveTexCoord() != 2 {
		t.Fatalf("texCoord override = %d, want 2", ref1.Texture.EffectiveTexCoord())
	}
	// UVScale/UVOffset carry the atlas transform, so TransformTexCoords on ref.Texture must return the raw channel rather than apply it again.
	prim := doc.Meshes[0].Primitives[0]
	got, err := doc.TransformTexCoords(prim, &ref1.Texture, nil)
	if err != nil {
		t.Fatalf("TransformTexCoords: %v", err)
	}
	raw, err := doc.PrimitiveTexCoords(prim, 2)
	if err != nil {
		t.Fatalf("PrimitiveTexCoords: %v", err)
	}
	for i := range raw {
		if got[i] != raw[i] {
			t.Fatalf("TransformTexCoords[%d] = %v, want untransformed %v", i, got[i], raw[i])
		}
	}
}

func TestLightmapExtensionsAllowedAsRequired(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(buildLightmapTestGLTF(t, true, false, []map[string]any{{
		"extensions": map[string]any{"MOZ_lightmap": map[string]any{"index": 0}},
	}}, 1), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	doc["extensionsUsed"] = []string{gltf.ExtMOZLightmap, gltf.ExtLightmap}
	doc["extensionsRequired"] = []string{gltf.ExtMOZLightmap, gltf.ExtLightmap}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	loaded, err := gltf.Load(raw, nil)
	if err != nil {
		t.Fatalf("gltf.Load with lightmap extensions required: %v", err)
	}
	if ref, err := loaded.MaterialLightmap(0); err != nil || ref == nil {
		t.Fatalf("MaterialLightmap = %+v, %v", ref, err)
	}
}

func TestMaterialLightmapValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		mat  map[string]any
	}{
		{"non_object_ext", map[string]any{"extensions": map[string]any{"MOZ_lightmap": 123}}},
		{"missing_index", map[string]any{"extensions": map[string]any{"MOZ_lightmap": map[string]any{"intensity": 1.0}}}},
		{"index_out_of_range", map[string]any{"extensions": map[string]any{"MOZ_lightmap": map[string]any{"index": 5}}}},
		{"negative_texcoord", map[string]any{"extensions": map[string]any{"MOZ_lightmap": map[string]any{"index": 0, "texCoord": -1}}}},
		{"negative_intensity", map[string]any{"extensions": map[string]any{"MOZ_lightmap": map[string]any{"index": 0, "intensity": -0.5}}}},
		{"non_positive_rgbm_range", map[string]any{"extensions": map[string]any{"EXT_lightmap": map[string]any{"index": 0, "rgbmMaxRange": 0}}}},
		{"bad_uv_scale", map[string]any{"extensions": map[string]any{"EXT_lightmap": map[string]any{"index": 0, "uvScale": []float32{1}}}}},
		{"bad_scale_offset", map[string]any{"extras": map[string]any{"lightmap": map[string]any{"index": 0, "scaleOffset": []float32{1, 1, 0}}}}},
		{"rotated_transform", map[string]any{"extensions": map[string]any{"MOZ_lightmap": map[string]any{"index": 0, "extensions": map[string]any{"KHR_texture_transform": map[string]any{"rotation": 0.5}}}}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildLightmapTestGLTF(t, true, false, []map[string]any{tc.mat}, 1)
			doc, err := gltf.Load(raw, nil)
			if err != nil {
				t.Fatalf("gltf.Load: %v", err)
			}
			if _, err := doc.MaterialLightmap(0); err == nil {
				t.Fatalf("MaterialLightmap succeeded for invalid case %s, want error", tc.name)
			}
		})
	}
}
