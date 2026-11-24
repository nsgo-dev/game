package gltf_test

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/math3d"
)

func TestClearcoatKhronosSample(t *testing.T) {
	// Khronos TextureTransformMultiTest contains 9 KHR_materials_clearcoat materials
	// exercising clearcoatTexture, clearcoatRoughnessTexture, and clearcoatNormalTexture
	// across UV0, UV1, and untransformed reference samples.
	doc := loadKhronos(t, "TextureTransformMultiTest", "glTF", nil)

	meshByName := make(map[string]gltf.Mesh, len(doc.Nodes))
	for _, n := range doc.Nodes {
		if n.Mesh >= 0 && n.Mesh < len(doc.Meshes) {
			meshByName[n.Name] = doc.Meshes[n.Mesh]
		}
	}

	// Non-clearcoat materials keep Clearcoat == nil and default EmissiveStrength == 1.
	baseMat := doc.Materials[meshByName["BaseColorSample"].Primitives[0].Material]
	if baseMat.Clearcoat != nil {
		t.Fatalf("BaseColorSample Clearcoat = %+v, want nil", baseMat.Clearcoat)
	}
	if baseMat.EmissiveStrength != 1 {
		t.Fatalf("BaseColorSample EmissiveStrength = %v, want 1", baseMat.EmissiveStrength)
	}
	if baseMat.Transmission != nil {
		t.Fatalf("BaseColorSample Transmission = %+v, want nil", baseMat.Transmission)
	}

	slots := []struct {
		prefix       string
		wantRough    float32
		wantTexIndex int
		texRef       func(c *gltf.Clearcoat) *gltf.TextureRef
	}{
		{"Clearcoat", 0.03, 0, func(c *gltf.Clearcoat) *gltf.TextureRef { return c.Texture }},
		{"ClearcoatRough", 1.0, 3, func(c *gltf.Clearcoat) *gltf.TextureRef { return c.RoughnessTexture }},
		{"ClearcoatNormal", 0.03, 2, func(c *gltf.Clearcoat) *gltf.TextureRef { return c.NormalTexture }},
	}

	for _, slot := range slots {
		t.Run(slot.prefix, func(t *testing.T) {
			samplePrim := meshByName[slot.prefix+"Sample"].Primitives[0]
			sampleMat := doc.Materials[samplePrim.Material]
			if sampleMat.Clearcoat == nil {
				t.Fatalf("%sSample: Clearcoat is nil", slot.prefix)
			}
			if !math3d.ApproxEqual(sampleMat.Clearcoat.Factor, 1, 1e-6) {
				t.Fatalf("%sSample: Clearcoat.Factor = %v, want 1", slot.prefix, sampleMat.Clearcoat.Factor)
			}
			if !math3d.ApproxEqual(sampleMat.Clearcoat.RoughnessFactor, slot.wantRough, 1e-5) {
				t.Fatalf("%sSample: Clearcoat.RoughnessFactor = %v, want %v", slot.prefix, sampleMat.Clearcoat.RoughnessFactor, slot.wantRough)
			}
			sampleRef := slot.texRef(sampleMat.Clearcoat)
			if sampleRef == nil || sampleRef.TextureIndex != slot.wantTexIndex || sampleRef.Transform != nil {
				t.Fatalf("%sSample: unexpected texture ref %+v", slot.prefix, sampleRef)
			}
			wantUVs, err := doc.TransformTexCoords(samplePrim, sampleRef, nil)
			if err != nil {
				t.Fatalf("%sSample TransformTexCoords: %v", slot.prefix, err)
			}

			for _, uvCase := range []struct {
				suffix       string
				wantTexCoord int
			}{
				{"UV0", 0},
				{"UV1", 1},
			} {
				prim := meshByName[slot.prefix+uvCase.suffix].Primitives[0]
				mat := doc.Materials[prim.Material]
				if mat.Clearcoat == nil {
					t.Fatalf("%s%s: Clearcoat is nil", slot.prefix, uvCase.suffix)
				}
				ref := slot.texRef(mat.Clearcoat)
				if ref == nil || ref.Transform == nil {
					t.Fatalf("%s%s: missing clearcoat texture or KHR_texture_transform", slot.prefix, uvCase.suffix)
				}
				if ref.TextureIndex != slot.wantTexIndex {
					t.Fatalf("%s%s: TextureIndex = %d, want %d", slot.prefix, uvCase.suffix, ref.TextureIndex, slot.wantTexIndex)
				}
				if ref.EffectiveTexCoord() != uvCase.wantTexCoord {
					t.Fatalf("%s%s: EffectiveTexCoord = %d, want %d", slot.prefix, uvCase.suffix, ref.EffectiveTexCoord(), uvCase.wantTexCoord)
				}
				gotUVs, err := doc.TransformTexCoords(prim, ref, nil)
				if err != nil {
					t.Fatalf("%s%s TransformTexCoords: %v", slot.prefix, uvCase.suffix, err)
				}
				if len(gotUVs) != len(wantUVs) {
					t.Fatalf("%s%s: %d UVs, want %d", slot.prefix, uvCase.suffix, len(gotUVs), len(wantUVs))
				}
				for i := range wantUVs {
					if !math3d.ApproxEqual(gotUVs[i].X, wantUVs[i].X, 1e-4) || !math3d.ApproxEqual(gotUVs[i].Y, wantUVs[i].Y, 1e-2) {
						t.Fatalf("%s%s vertex %d: transformed UV=%v, want reference %v", slot.prefix, uvCase.suffix, i, gotUVs[i], wantUVs[i])
					}
				}
			}
		})
	}
}

func TestTransmissionKhronosSample(t *testing.T) {
	// Khronos SunglassesKhronos uses KHR_materials_transmission on nose_pads,
	// lens_interior, and lens_exterior (all transmissionFactor = 1.0) while
	// earhooks and temples remain opaque.
	doc := loadKhronos(t, "SunglassesKhronos", "glTF", nil)
	wantTrans := map[string]bool{
		"earhooks":      false,
		"temples":       false,
		"nose_pads":     true,
		"lens_interior": true,
		"lens_exterior": true,
	}
	seen := 0
	for _, mat := range doc.Materials {
		if mat.EmissiveStrength != 1 {
			t.Fatalf("%s: EmissiveStrength = %v, want default 1", mat.Name, mat.EmissiveStrength)
		}
		expect, ok := wantTrans[mat.Name]
		if !ok {
			continue
		}
		seen++
		if !expect {
			if mat.Transmission != nil {
				t.Fatalf("%s: Transmission = %+v, want nil", mat.Name, mat.Transmission)
			}
			continue
		}
		if mat.Transmission == nil {
			t.Fatalf("%s: Transmission is nil, want factor 1", mat.Name)
		}
		if !math3d.ApproxEqual(mat.Transmission.Factor, 1, 1e-6) {
			t.Fatalf("%s: Transmission.Factor = %v, want 1", mat.Name, mat.Transmission.Factor)
		}
		if mat.Transmission.Texture != nil {
			t.Fatalf("%s: Transmission.Texture = %+v, want nil", mat.Name, mat.Transmission.Texture)
		}
	}
	if seen != len(wantTrans) {
		t.Fatalf("matched %d expected materials, want %d", seen, len(wantTrans))
	}
}

func TestMaterialExtensionsSpecAndRequired(t *testing.T) {
	// Build a GLB declaring all three extensions in extensionsRequired and
	// testing defaults, non-default factors, textures, normal scale, and
	// KHR_texture_transform on extension textures.
	var bin bytes.Buffer
	posOff := appendFloat32s(&bin, 0, 0, 0, 1, 0, 0, 0, 1, 0)
	posLen := bin.Len() - posOff
	uvOff := appendFloat32s(&bin, 0, 0, 1, 0, 0, 1)
	uvLen := bin.Len() - uvOff

	raw := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"extensionsUsed": []string{
			"KHR_materials_emissive_strength",
			"KHR_materials_clearcoat",
			"KHR_materials_transmission",
			"KHR_texture_transform",
		},
		"extensionsRequired": []string{
			"KHR_materials_emissive_strength",
			"KHR_materials_clearcoat",
			"KHR_materials_transmission",
			"KHR_texture_transform",
		},
		"buffers": []any{map[string]any{"byteLength": bin.Len()}},
		"bufferViews": []any{
			map[string]any{"buffer": 0, "byteOffset": posOff, "byteLength": posLen},
			map[string]any{"buffer": 0, "byteOffset": uvOff, "byteLength": uvLen},
		},
		"accessors": []any{
			map[string]any{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3"},
			map[string]any{"bufferView": 1, "componentType": 5126, "count": 3, "type": "VEC2"},
		},
		"textures": []any{
			map[string]any{"name": "tex0"},
			map[string]any{"name": "tex1"},
			map[string]any{"name": "tex2"},
			map[string]any{"name": "tex3"},
		},
		"materials": []any{
			// 0: EmissiveStrength default ({} -> 1.0) and explicit 0.0 / 5.0 cases
			map[string]any{
				"name":           "EmissiveDefault",
				"emissiveFactor": []float32{0.5, 0.25, 0.1},
				"extensions": map[string]any{
					"KHR_materials_emissive_strength": map[string]any{},
				},
			},
			// 1: EmissiveStrength = 5.0
			map[string]any{
				"name":           "EmissiveHDR",
				"emissiveFactor": []float32{0.8, 0.4, 0.2},
				"extensions": map[string]any{
					"KHR_materials_emissive_strength": map[string]any{
						"emissiveStrength": 5.0,
					},
				},
			},
			// 2: EmissiveStrength = 0.0 (valid zero)
			map[string]any{
				"name":           "EmissiveZero",
				"emissiveFactor": []float32{1, 1, 1},
				"extensions": map[string]any{
					"KHR_materials_emissive_strength": map[string]any{
						"emissiveStrength": 0.0,
					},
				},
			},
			// 3: Clearcoat defaults ({} -> factor 0, roughness 0, nil textures)
			map[string]any{
				"name": "ClearcoatDefault",
				"extensions": map[string]any{
					"KHR_materials_clearcoat": map[string]any{},
				},
			},
			// 4: Full clearcoat with textures, normal scale, and KHR_texture_transform
			map[string]any{
				"name": "ClearcoatFull",
				"extensions": map[string]any{
					"KHR_materials_clearcoat": map[string]any{
						"clearcoatFactor":          0.85,
						"clearcoatRoughnessFactor": 0.12,
						"clearcoatTexture": map[string]any{
							"index": 0,
							"extensions": map[string]any{
								"KHR_texture_transform": map[string]any{
									"offset": []float32{0.25, 0.5},
									"scale":  []float32{2, 2},
								},
							},
						},
						"clearcoatRoughnessTexture": map[string]any{"index": 1, "texCoord": 0},
						"clearcoatNormalTexture":    map[string]any{"index": 2, "scale": 0.6},
					},
				},
			},
			// 5: Transmission defaults ({} -> factor 0, nil texture)
			map[string]any{
				"name": "TransmissionDefault",
				"extensions": map[string]any{
					"KHR_materials_transmission": map[string]any{},
				},
			},
			// 6: Full transmission with factor, texture, and KHR_texture_transform
			map[string]any{
				"name": "TransmissionFull",
				"extensions": map[string]any{
					"KHR_materials_transmission": map[string]any{
						"transmissionFactor": 0.9,
						"transmissionTexture": map[string]any{
							"index": 3,
							"extensions": map[string]any{
								"KHR_texture_transform": map[string]any{
									"rotation": float32(math.Pi / 2),
								},
							},
						},
					},
				},
			},
		},
		"meshes": []any{
			map[string]any{
				"primitives": []any{
					map[string]any{
						"attributes": map[string]int{"POSITION": 0, "TEXCOORD_0": 1},
						"material":   4,
					},
				},
			},
		},
	}

	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := gltf.Load(buildGLB(jsonBytes, bin.Bytes()), nil)
	if err != nil {
		t.Fatalf("Load GLB with required material extensions: %v", err)
	}

	// EmissiveDefault
	if doc.Materials[0].EmissiveStrength != 1 {
		t.Fatalf("mat[0] EmissiveStrength = %v, want 1", doc.Materials[0].EmissiveStrength)
	}
	if !doc.Materials[0].EffectiveEmissive().ApproxEqual(math3d.Vec3{X: 0.5, Y: 0.25, Z: 0.1}, 1e-6) {
		t.Fatalf("mat[0] EffectiveEmissive = %v", doc.Materials[0].EffectiveEmissive())
	}

	// EmissiveHDR
	if doc.Materials[1].EmissiveStrength != 5 {
		t.Fatalf("mat[1] EmissiveStrength = %v, want 5", doc.Materials[1].EmissiveStrength)
	}
	if !doc.Materials[1].EffectiveEmissive().ApproxEqual(math3d.Vec3{X: 4.0, Y: 2.0, Z: 1.0}, 1e-5) {
		t.Fatalf("mat[1] EffectiveEmissive = %v, want (4, 2, 1)", doc.Materials[1].EffectiveEmissive())
	}

	// EmissiveZero
	if doc.Materials[2].EmissiveStrength != 0 {
		t.Fatalf("mat[2] EmissiveStrength = %v, want 0", doc.Materials[2].EmissiveStrength)
	}
	if !doc.Materials[2].EffectiveEmissive().ApproxEqual(math3d.Vec3Zero(), 1e-6) {
		t.Fatalf("mat[2] EffectiveEmissive = %v, want (0, 0, 0)", doc.Materials[2].EffectiveEmissive())
	}

	// ClearcoatDefault
	ccDef := doc.Materials[3].Clearcoat
	if ccDef == nil || ccDef.Factor != 0 || ccDef.RoughnessFactor != 0 || ccDef.Texture != nil || ccDef.RoughnessTexture != nil || ccDef.NormalTexture != nil {
		t.Fatalf("mat[3] Clearcoat = %+v, want zero-valued Clearcoat", ccDef)
	}

	// ClearcoatFull
	cc := doc.Materials[4].Clearcoat
	if cc == nil {
		t.Fatal("mat[4] Clearcoat is nil")
	}
	if !math3d.ApproxEqual(cc.Factor, 0.85, 1e-6) || !math3d.ApproxEqual(cc.RoughnessFactor, 0.12, 1e-6) {
		t.Fatalf("mat[4] Clearcoat factors = (%v, %v), want (0.85, 0.12)", cc.Factor, cc.RoughnessFactor)
	}
	if cc.Texture == nil || cc.Texture.TextureIndex != 0 || cc.Texture.Transform == nil {
		t.Fatalf("mat[4] Clearcoat.Texture = %+v", cc.Texture)
	}
	if got := cc.Texture.ApplyUV(math3d.Vec2{X: 0.5, Y: 0.25}); !got.ApproxEqual(math3d.Vec2{X: 1.25, Y: 1.0}, 1e-5) {
		t.Fatalf("mat[4] Clearcoat.Texture.ApplyUV = %v, want (1.25, 1.0)", got)
	}
	if cc.RoughnessTexture == nil || cc.RoughnessTexture.TextureIndex != 1 {
		t.Fatalf("mat[4] Clearcoat.RoughnessTexture = %+v", cc.RoughnessTexture)
	}
	if cc.NormalTexture == nil || cc.NormalTexture.TextureIndex != 2 || !math3d.ApproxEqual(cc.NormalTexture.Scale, 0.6, 1e-6) {
		t.Fatalf("mat[4] Clearcoat.NormalTexture = %+v", cc.NormalTexture)
	}

	// TransmissionDefault
	trDef := doc.Materials[5].Transmission
	if trDef == nil || trDef.Factor != 0 || trDef.Texture != nil {
		t.Fatalf("mat[5] Transmission = %+v, want zero-valued Transmission", trDef)
	}

	// TransmissionFull
	tr := doc.Materials[6].Transmission
	if tr == nil || !math3d.ApproxEqual(tr.Factor, 0.9, 1e-6) {
		t.Fatalf("mat[6] Transmission = %+v, want factor 0.9", tr)
	}
	if tr.Texture == nil || tr.Texture.TextureIndex != 3 || tr.Texture.Transform == nil {
		t.Fatalf("mat[6] Transmission.Texture = %+v", tr.Texture)
	}
	if got := tr.Texture.ApplyUV(math3d.Vec2{X: 1, Y: 0}); !got.ApproxEqual(math3d.Vec2{X: 0, Y: -1}, 1e-5) {
		t.Fatalf("mat[6] Transmission.Texture.ApplyUV = %v, want (0, -1)", got)
	}
}

func TestMaterialExtensionsRejectMalformed(t *testing.T) {
	makeDoc := func(extName, payload string) []byte {
		return []byte(`{
			"asset": {"version": "2.0"},
			"textures": [{}],
			"materials": [{
				"extensions": {"` + extName + `": ` + payload + `}
			}]
		}`)
	}

	cases := []struct {
		name    string
		extName string
		payload string
		want    string
	}{
		// KHR_materials_emissive_strength
		{"emissive null payload", "KHR_materials_emissive_strength", `null`, "JSON object"},
		{"emissive array payload", "KHR_materials_emissive_strength", `[]`, "JSON object"},
		{"emissive null strength", "KHR_materials_emissive_strength", `{"emissiveStrength":null}`, "finite number"},
		{"emissive string strength", "KHR_materials_emissive_strength", `{"emissiveStrength":"bright"}`, "parse KHR_materials_emissive_strength emissiveStrength"},
		{"emissive overflow", "KHR_materials_emissive_strength", `{"emissiveStrength":1e40}`, "parse KHR_materials_emissive_strength emissiveStrength"},
		{"emissive negative", "KHR_materials_emissive_strength", `{"emissiveStrength":-0.1}`, "must be >= 0"},

		// KHR_materials_clearcoat
		{"clearcoat null payload", "KHR_materials_clearcoat", `null`, "JSON object"},
		{"clearcoat array payload", "KHR_materials_clearcoat", `[]`, "JSON object"},
		{"clearcoat factor null", "KHR_materials_clearcoat", `{"clearcoatFactor":null}`, "finite number"},
		{"clearcoat factor negative", "KHR_materials_clearcoat", `{"clearcoatFactor":-0.01}`, "must be in [0, 1]"},
		{"clearcoat factor > 1", "KHR_materials_clearcoat", `{"clearcoatFactor":1.5}`, "must be in [0, 1]"},
		{"clearcoat roughness null", "KHR_materials_clearcoat", `{"clearcoatRoughnessFactor":null}`, "finite number"},
		{"clearcoat roughness negative", "KHR_materials_clearcoat", `{"clearcoatRoughnessFactor":-0.2}`, "must be in [0, 1]"},
		{"clearcoat roughness > 1", "KHR_materials_clearcoat", `{"clearcoatRoughnessFactor":1.01}`, "must be in [0, 1]"},
		{"clearcoat texture null", "KHR_materials_clearcoat", `{"clearcoatTexture":null}`, "JSON object"},
		{"clearcoat texture oob", "KHR_materials_clearcoat", `{"clearcoatTexture":{"index":1}}`, "invalid texture index"},
		{"clearcoat texture neg texCoord", "KHR_materials_clearcoat", `{"clearcoatTexture":{"index":0,"texCoord":-1}}`, "negative texCoord"},
		{"clearcoat texture bad transform", "KHR_materials_clearcoat", `{"clearcoatTexture":{"index":0,"extensions":{"KHR_texture_transform":{"scale":[1]}}}}`, "must have 2 elements"},
		{"clearcoat roughness texture oob", "KHR_materials_clearcoat", `{"clearcoatRoughnessTexture":{"index":5}}`, "invalid texture index"},
		{"clearcoat normal texture null", "KHR_materials_clearcoat", `{"clearcoatNormalTexture":null}`, "JSON object"},
		{"clearcoat normal texture oob", "KHR_materials_clearcoat", `{"clearcoatNormalTexture":{"index":-1}}`, "invalid texture index"},

		// KHR_materials_transmission
		{"transmission null payload", "KHR_materials_transmission", `null`, "JSON object"},
		{"transmission array payload", "KHR_materials_transmission", `[]`, "JSON object"},
		{"transmission factor null", "KHR_materials_transmission", `{"transmissionFactor":null}`, "finite number"},
		{"transmission factor negative", "KHR_materials_transmission", `{"transmissionFactor":-0.5}`, "must be in [0, 1]"},
		{"transmission factor > 1", "KHR_materials_transmission", `{"transmissionFactor":1.2}`, "must be in [0, 1]"},
		{"transmission texture null", "KHR_materials_transmission", `{"transmissionTexture":null}`, "JSON object"},
		{"transmission texture oob", "KHR_materials_transmission", `{"transmissionTexture":{"index":2}}`, "invalid texture index"},
		{"transmission texture bad transform", "KHR_materials_transmission", `{"transmissionTexture":{"index":0,"extensions":{"KHR_texture_transform":{"texCoord":-2}}}}`, "negative texCoord"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gltf.Load(makeDoc(tc.extName, tc.payload), nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want one containing %q", err, tc.want)
			}
			le, ok := loaderr.As(err)
			if !ok || le.Field != "materials[0]" {
				t.Fatalf("expected *loaderr.Error at materials[0], got %T (%v)", err, err)
			}
		})
	}
}
