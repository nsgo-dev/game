package gltf_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/math3d"
)

// sampleClamp samples img at normalized glTF UV coordinates (origin top-left,
// +U right, +V down) using CLAMP_TO_EDGE addressing.
func sampleClamp(img image.Image, uv math3d.Vec2) color.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	u := math3d.Clamp(uv.X, 0, 1)
	v := math3d.Clamp(uv.Y, 0, 1)
	x := min(int(math.Floor(float64(u)*float64(w))), w-1)
	y := min(int(math.Floor(float64(v)*float64(h))), h-1)
	r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8)}
}

func colorLuma(c color.RGBA) int {
	return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
}

func TestTextureTransformKhronosSample(t *testing.T) {
	doc := loadKhronos(t, "TextureTransformTest", "glTF", nil)

	uvImg, err := doc.DecodeImage(0) // UV.png
	if err != nil {
		t.Fatalf("DecodeImage(UV.png): %v", err)
	}
	arrowImg, err := doc.DecodeImage(1) // Arrow.png
	if err != nil {
		t.Fatalf("DecodeImage(Arrow.png): %v", err)
	}

	// Verify every panel's TransformTexCoords matches Matrix().MulVec3 and reuses scratch storage.
	var scratch []math3d.Vec2
	for mi := range 6 {
		mesh := doc.Meshes[mi]
		prim := mesh.Primitives[0]
		ref := doc.Materials[prim.Material].BaseColorTexture
		if ref == nil || ref.Transform == nil {
			t.Fatalf("mesh %d (%s): missing BaseColorTexture transform", mi, mesh.Name)
		}
		uvs, err := doc.TransformTexCoords(prim, ref, scratch)
		if err != nil {
			t.Fatalf("mesh %d (%s): TransformTexCoords: %v", mi, mesh.Name, err)
		}
		scratch = uvs[:0]
		mat := ref.Transform.Matrix()
		for i, raw := range prim.TexCoords0 {
			mUV := mat.MulVec3(math3d.Vec3{X: raw.X, Y: raw.Y, Z: 1})
			want := math3d.Vec2{X: mUV.X, Y: mUV.Y}
			if !uvs[i].ApproxEqual(want, 1e-5) {
				t.Fatalf("mesh %d (%s) vert %d: TransformTexCoords=%v, Matrix=%v", mi, mesh.Name, i, uvs[i], want)
			}
		}
	}

	// Offset panels: UV1 in TextureTransformTest.bin covers [0, 0.5] x [0, 0.5];
	// Offset U, V, and UV shift it into the green (top-right), blue (bottom-left),
	// and cyan (bottom-right) quadrants of UV.png instead of the yellow top-left.
	offsetCases := []struct {
		meshIdx   int
		name      string
		wantMin   math3d.Vec2
		wantMax   math3d.Vec2
		wantColor func(c color.RGBA) bool
		colorDesc string
	}{
		{0, "Offset U", math3d.Vec2{X: 0.5, Y: 0.0}, math3d.Vec2{X: 1.0, Y: 0.5}, func(c color.RGBA) bool { return c.R < 50 && c.G > 150 && c.B < 50 }, "green"},
		{1, "Offset V", math3d.Vec2{X: 0.0, Y: 0.5}, math3d.Vec2{X: 0.5, Y: 1.0}, func(c color.RGBA) bool { return c.R < 50 && c.G < 50 && c.B > 150 }, "blue"},
		{2, "Offset UV", math3d.Vec2{X: 0.5, Y: 0.5}, math3d.Vec2{X: 1.0, Y: 1.0}, func(c color.RGBA) bool { return c.R < 50 && c.G > 150 && c.B > 150 }, "cyan"},
	}
	untransformedBg := sampleClamp(uvImg, math3d.Vec2{X: 0.1, Y: 0.1})
	if untransformedBg.R < 150 || untransformedBg.G < 150 || untransformedBg.B > 50 {
		t.Fatalf("UV.png untransformed top-left background = %v, want yellow", untransformedBg)
	}
	for _, tc := range offsetCases {
		prim := doc.Meshes[tc.meshIdx].Primitives[0]
		ref := doc.Materials[prim.Material].BaseColorTexture
		uvs, err := doc.TransformTexCoords(prim, ref, nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		lo, hi := uvs[0], uvs[0]
		for _, uv := range uvs[1:] {
			lo.X, lo.Y = min(lo.X, uv.X), min(lo.Y, uv.Y)
			hi.X, hi.Y = max(hi.X, uv.X), max(hi.Y, uv.Y)
		}
		if !lo.ApproxEqual(tc.wantMin, 1e-5) || !hi.ApproxEqual(tc.wantMax, 1e-5) {
			t.Fatalf("%s: UV bounds [%v, %v], want [%v, %v]", tc.name, lo, hi, tc.wantMin, tc.wantMax)
		}
		gotPx := sampleClamp(uvImg, ref.ApplyUV(math3d.Vec2{X: 0.1, Y: 0.1}))
		if !tc.wantColor(gotPx) {
			t.Fatalf("%s: sampled pixel %v, want %s quadrant", tc.name, gotPx, tc.colorDesc)
		}
	}

	// Arrow panels (Rotation, Scale, All): each quad spans [-0.5, 0.5]^2 with
	// UV0 (u, v) = (x + 0.5, 0.5 - y). In Arrow.png the black arrow runs along
	// the diagonal toward (0.68, 0.68), and the Khronos marker nodes sit just
	// past the arrowhead at untransformed UV (0.7778, 0.7778) so the arrow
	// points cleanly at the green "Correct" marker without covering it.
	localToUV := func(pos math3d.Vec3) math3d.Vec2 {
		return math3d.Vec2{X: pos.X + 0.5, Y: 0.5 - pos.Y}
	}
	nodeByName := func(name string) gltf.Node {
		for _, n := range doc.Nodes {
			if n.Name == name {
				return n
			}
		}
		t.Fatalf("node %q not found", name)
		return gltf.Node{}
	}

	// Verify the marker reference UV sits just ahead of the black arrowhead in Arrow.png.
	markerUVRef := localToUV(nodeByName("Rotation - Not Supported").Translation)
	if !markerUVRef.ApproxEqual(math3d.Vec2{X: 0.77781746, Y: 0.77781746}, 1e-5) {
		t.Fatalf("unexpected markerUVRef = %v", markerUVRef)
	}
	// Sampling along the arrow diagonal inside the arrowhead (0.60, 0.60) hits black; corner background hits white.
	if bg := colorLuma(sampleClamp(arrowImg, math3d.Vec2{X: 0.95, Y: 0.05})); bg < 240 {
		t.Fatalf("Arrow.png corner luma = %d, want white background", bg)
	}
	if head := colorLuma(sampleClamp(arrowImg, math3d.Vec2{X: 0.60, Y: 0.60})); head > 40 {
		t.Fatalf("Arrow.png arrowhead luma = %d, want black arrow", head)
	}
	// For any panel with transform T, stepping 77% of the way from UV origin (0,0) toward the Correct marker
	// (accounting for offset) maps right onto the black arrowhead at ~(0.60, 0.60) in Arrow.png.
	checkArrowPanel := func(name string, meshIdx int, correctMarker string, tol float32, wrongMarkers ...string) {
		t.Helper()
		ref := doc.Materials[doc.Meshes[meshIdx].Primitives[0].Material].BaseColorTexture
		correctUV := ref.ApplyUV(localToUV(nodeByName(correctMarker).Translation))
		if d := correctUV.Sub(markerUVRef).Len(); d > tol {
			t.Fatalf("%s (%s): transformed UV = %v, distance %v > %v from reference %v", name, correctMarker, correctUV, d, tol, markerUVRef)
		}
		// Compute the point on the quad that maps 0.77 of the way to the marker in texture space, and verify it hits the black arrow.
		arrowTarget := markerUVRef.Scale(0.77)
		if l := colorLuma(sampleClamp(arrowImg, arrowTarget)); l > 40 {
			t.Fatalf("%s: expected black arrow at %v, got luma=%d", name, arrowTarget, l)
		}
		for _, wrong := range wrongMarkers {
			wrongUV := ref.ApplyUV(localToUV(nodeByName(wrong).Translation))
			if wrongUV.Sub(markerUVRef).Len() < 0.15 {
				t.Fatalf("%s (%s): wrong marker unexpectedly landed near reference (%v vs %v)", name, wrong, wrongUV, markerUVRef)
			}
		}
	}

	checkArrowPanel("Rotation", 3, "Rotation - Correct", 1e-4, "Rotation - Not Supported", "Rotation - Error")
	checkArrowPanel("Scale", 4, "Scale - Correct", 1e-4, "Scale - Not Supported")
	checkArrowPanel("All", 5, "All - Correct", 0.04)
}

func TestTextureTransformMultiKhronosSample(t *testing.T) {
	// TextureTransformMultiTest declares KHR_texture_transform in extensionsRequired
	// and applies transforms across baseColor, emissive, normal, metallicRoughness,
	// occlusion, and unlit materials on both TEXCOORD_0 and TEXCOORD_1.
	doc := loadKhronos(t, "TextureTransformMultiTest", "glTF", nil)

	meshByName := make(map[string]gltf.Mesh, len(doc.Meshes))
	for _, m := range doc.Meshes {
		meshByName[m.Name] = m
	}

	slots := []struct {
		prefix string
		texRef func(m gltf.Material) *gltf.TextureRef
	}{
		{"BaseColor", func(m gltf.Material) *gltf.TextureRef { return m.BaseColorTexture }},
		{"Emission", func(m gltf.Material) *gltf.TextureRef { return m.EmissiveTexture }},
		{"Normal", func(m gltf.Material) *gltf.TextureRef { return m.NormalTexture }},
		{"MetalRough", func(m gltf.Material) *gltf.TextureRef { return m.MetallicRoughnessTexture }},
		{"Occlusion", func(m gltf.Material) *gltf.TextureRef { return m.OcclusionTexture }},
		{"Unlit", func(m gltf.Material) *gltf.TextureRef { return m.BaseColorTexture }},
	}

	for _, slot := range slots {
		t.Run(slot.prefix, func(t *testing.T) {
			samplePrim := meshByName[slot.prefix+"Sample"].Primitives[0]
			sampleRef := slot.texRef(doc.Materials[samplePrim.Material])
			if sampleRef == nil || sampleRef.Transform != nil {
				t.Fatalf("%sSample: expected untransformed texture reference", slot.prefix)
			}
			wantUVs, err := doc.TransformTexCoords(samplePrim, sampleRef, nil)
			if err != nil {
				t.Fatalf("%sSample TransformTexCoords: %v", slot.prefix, err)
			}

			var uv0Transformed []math3d.Vec2
			for _, uvCase := range []struct {
				suffix       string
				wantTexCoord int
			}{
				{"UV0", 0},
				{"UV1", 1},
			} {
				prim := meshByName[slot.prefix+uvCase.suffix].Primitives[0]
				ref := slot.texRef(doc.Materials[prim.Material])
				if ref == nil || ref.Transform == nil {
					t.Fatalf("%s%s: missing KHR_texture_transform", slot.prefix, uvCase.suffix)
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
				// UV0 and UV1 transform to identical coordinates, matching the Sample control mesh
				// (whose authored Blender V coordinate differs by 0.006 from the transformed tiles).
				if uvCase.wantTexCoord == 0 {
					uv0Transformed = gotUVs
				} else {
					for i := range gotUVs {
						if !gotUVs[i].ApproxEqual(uv0Transformed[i], 1e-5) {
							t.Fatalf("%sUV1 vertex %d: transformed UV=%v, want exact UV0 match %v", slot.prefix, i, gotUVs[i], uv0Transformed[i])
						}
					}
				}
				for i := range wantUVs {
					if !math3d.ApproxEqual(gotUVs[i].X, wantUVs[i].X, 1e-4) || !math3d.ApproxEqual(gotUVs[i].Y, wantUVs[i].Y, 1e-2) {
						t.Fatalf("%s%s vertex %d: transformed UV=%v, want reference %v", slot.prefix, uvCase.suffix, i, gotUVs[i], wantUVs[i])
					}
				}
				// Verify untransformed UVs do not match the reference (confirming transform was applied).
				rawUVs, err := doc.PrimitiveTexCoords(prim, ref.EffectiveTexCoord())
				if err != nil {
					t.Fatalf("%s%s PrimitiveTexCoords: %v", slot.prefix, uvCase.suffix, err)
				}
				if rawUVs[0].ApproxEqual(wantUVs[0], 0.1) {
					t.Fatalf("%s%s: untransformed UV %v unexpectedly close to reference %v", slot.prefix, uvCase.suffix, rawUVs[0], wantUVs[0])
				}
				// For UV1, verify TEXCOORD_0 differs substantially from TEXCOORD_1 on the tile face
				// (confirming TEXCOORD_1 was selected rather than falling back to TEXCOORD_0).
				if uvCase.wantTexCoord == 1 {
					var maxDiff float32
					for i := range prim.TexCoords0 {
						if d := ref.ApplyUV(prim.TexCoords0[i]).Sub(wantUVs[i]).Len(); d > maxDiff {
							maxDiff = d
						}
					}
					if maxDiff < 0.15 {
						t.Fatalf("%sUV1: transforming TEXCOORD_0 stayed within %v of reference, expected divergence", slot.prefix, maxDiff)
					}
				}
			}
		})
	}
}

func TestMaterialsVariantsKhronosSample(t *testing.T) {
	// GlamVelvetSofa combines KHR_materials_variants (5 variants across mapped
	// and unmapped meshes) with required KHR_texture_transform on normal maps.
	doc := loadKhronos(t, "GlamVelvetSofa", "glTF", nil)

	wantVariants := []string{"Champagne", "Navy", "Gray", "Black", "Pale Pink"}
	if len(doc.Variants) != len(wantVariants) {
		t.Fatalf("len(Variants) = %d, want %d", len(doc.Variants), len(wantVariants))
	}
	for i, name := range wantVariants {
		if doc.Variants[i].Name != name {
			t.Fatalf("Variants[%d].Name = %q, want %q", i, doc.Variants[i].Name, name)
		}
		if got := doc.VariantIndex(name); got != i {
			t.Fatalf("VariantIndex(%q) = %d, want %d", name, got, i)
		}
	}
	if got := doc.VariantIndex("NonExistent"); got != -1 {
		t.Fatalf("VariantIndex(NonExistent) = %d, want -1", got)
	}

	// Expected material indices per mesh [legs, fabric, feet] for default and each variant.
	// Legs (mesh 0) and feet (mesh 2) have no variant mappings and keep their defaults (0, 1).
	// Fabric (mesh 1) defaults to Navy (material 3) and maps variants 0..4 to materials 2..6.
	cases := []struct {
		label      string
		variantIdx int
		variantStr string
		wantMats   [3]int
		wantRot    float32 // fabric normalTexture KHR_texture_transform rotation
	}{
		{"default", -1, "", [3]int{0, 3, 1}, 0.36},
		{"Champagne", 0, "Champagne", [3]int{0, 2, 1}, 0},
		{"Navy", 1, "Navy", [3]int{0, 3, 1}, 0.36},
		{"Gray", 2, "Gray", [3]int{0, 4, 1}, 1.5},
		{"Black", 3, "Black", [3]int{0, 5, 1}, 2.0},
		{"Pale Pink", 4, "Pale Pink", [3]int{0, 6, 1}, 2.5},
		{"unknown name fallback", 99, "DoesNotExist", [3]int{0, 3, 1}, 0.36},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			resolved := doc.ResolveVariantMaterials(tc.variantIdx)
			if len(resolved) != 3 {
				t.Fatalf("ResolveVariantMaterials len = %d, want 3", len(resolved))
			}
			for mi := range 3 {
				meshMats, err := doc.MeshMaterials(mi, tc.variantIdx)
				if err != nil {
					t.Fatalf("MeshMaterials(%d, %d): %v", mi, tc.variantIdx, err)
				}
				if len(meshMats) != 1 || meshMats[0] != tc.wantMats[mi] {
					t.Fatalf("mesh %d MeshMaterials = %v, want [%d]", mi, meshMats, tc.wantMats[mi])
				}
				if len(resolved[mi]) != 1 || resolved[mi][0] != tc.wantMats[mi] {
					t.Fatalf("mesh %d ResolveVariantMaterials = %v, want [%d]", mi, resolved[mi], tc.wantMats[mi])
				}
				prim := doc.Meshes[mi].Primitives[0]
				if got := doc.MaterialForPrimitive(prim, tc.variantIdx); got != tc.wantMats[mi] {
					t.Fatalf("mesh %d MaterialForPrimitive = %d, want %d", mi, got, tc.wantMats[mi])
				}
				if got := doc.MaterialForVariant(prim, tc.variantStr); got != tc.wantMats[mi] {
					t.Fatalf("mesh %d MaterialForVariant(%q) = %d, want %d", mi, tc.variantStr, got, tc.wantMats[mi])
				}
			}
			// Check the active fabric material's KHR_texture_transform on normalTexture.
			fabricMat := doc.Materials[tc.wantMats[1]]
			if fabricMat.NormalTexture == nil || fabricMat.NormalTexture.Transform == nil {
				t.Fatalf("fabric material %d (%s) missing NormalTexture transform", tc.wantMats[1], fabricMat.Name)
			}
			tr := fabricMat.NormalTexture.Transform
			if tr.Scale != [2]float32{5, 5} || !math3d.ApproxEqual(tr.Rotation, tc.wantRot, 1e-6) {
				t.Fatalf("fabric material %d transform = %+v, want scale=[5,5] rot=%v", tc.wantMats[1], *tr, tc.wantRot)
			}
			uvs, err := doc.TransformTexCoords(doc.Meshes[1].Primitives[0], fabricMat.NormalTexture, nil)
			if err != nil || len(uvs) != len(doc.Meshes[1].Primitives[0].TexCoords0) {
				t.Fatalf("fabric TransformTexCoords: len=%d err=%v", len(uvs), err)
			}
		})
	}

	if _, err := doc.MeshMaterials(-1, 0); err == nil {
		t.Fatal("MeshMaterials(-1) succeeded, want out-of-range error")
	}
	if _, err := doc.MeshMaterials(len(doc.Meshes), 0); err == nil {
		t.Fatal("MeshMaterials(len) succeeded, want out-of-range error")
	}
}

func TestTextureTransformTexCoordOverrideAndHighSet(t *testing.T) {
	// Primitive with TEXCOORD_0 (sentinel bad UVs), TEXCOORD_1, and TEXCOORD_2.
	// BaseColorTexture has texCoord: 0 overridden to 1 by KHR_texture_transform.
	// EmissiveTexture has texCoord: 0 overridden to 2 by KHR_texture_transform.
	data := triangleDoc(t, unitTri, [3]uint16{0, 1, 2}, func(b *bytes.Buffer) {
		// TEXCOORD_0: sentinel (-9, -9)
		appendFloat32s(b, -9, -9, -9, -9, -9, -9)
		// TEXCOORD_1: unit triangle UVs
		appendFloat32s(b, 0, 0, 1, 0, 0, 1)
		// TEXCOORD_2: half-scaled UVs
		appendFloat32s(b, 0.25, 0.5, 0.75, 0.5, 0.25, 1.0)
	}, func(doc map[string]any) {
		views := doc["bufferViews"].([]any)
		ev := views[2].(map[string]any)
		baseOff := ev["byteOffset"].(int)
		views[2] = map[string]any{"buffer": 0, "byteOffset": baseOff, "byteLength": 24}
		views = append(views,
			map[string]any{"buffer": 0, "byteOffset": baseOff + 24, "byteLength": 24},
			map[string]any{"buffer": 0, "byteOffset": baseOff + 48, "byteLength": 24},
		)
		doc["bufferViews"] = views
		doc["accessors"] = append(doc["accessors"].([]any),
			map[string]any{"bufferView": 2, "componentType": 5126, "count": 3, "type": "VEC2"},
			map[string]any{"bufferView": 3, "componentType": 5126, "count": 3, "type": "VEC2"},
			map[string]any{"bufferView": 4, "componentType": 5126, "count": 3, "type": "VEC2"},
		)
		doc["extensionsUsed"] = []string{"KHR_texture_transform"}
		doc["extensionsRequired"] = []string{"KHR_texture_transform"}
		doc["textures"] = []any{map[string]any{}}
		doc["materials"] = []any{
			map[string]any{
				"pbrMetallicRoughness": map[string]any{
					"baseColorTexture": map[string]any{
						"index":    0,
						"texCoord": 0,
						"extensions": map[string]any{
							"KHR_texture_transform": map[string]any{
								"offset":   []float32{0.5, 0.25},
								"rotation": float32(math.Pi / 2),
								"scale":    []float32{2, 4},
								"texCoord": 1,
							},
						},
					},
				},
				"emissiveTexture": map[string]any{
					"index":    0,
					"texCoord": 0,
					"extensions": map[string]any{
						"KHR_texture_transform": map[string]any{
							"offset":   []float32{0.1, -0.2},
							"scale":    []float32{2, 2},
							"texCoord": 2,
						},
					},
				},
				"occlusionTexture": map[string]any{
					"index":    0,
					"texCoord": 1,
				},
			},
		}
		prim := doc["meshes"].([]any)[0].(map[string]any)["primitives"].([]any)[0].(map[string]any)
		attrs := prim["attributes"].(map[string]any)
		attrs["TEXCOORD_0"] = 2
		attrs["TEXCOORD_1"] = 3
		attrs["TEXCOORD_2"] = 4
		prim["material"] = 0
	})

	doc, err := gltf.Load(data, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prim := doc.Meshes[0].Primitives[0]
	mat := doc.Materials[0]

	if !gltf.DefaultTextureTransform().IsIdentity() {
		t.Fatal("DefaultTextureTransform().IsIdentity() = false, want true")
	}
	if mat.BaseColorTexture.EffectiveTexCoord() != 1 {
		t.Fatalf("BaseColorTexture.EffectiveTexCoord = %d, want 1", mat.BaseColorTexture.EffectiveTexCoord())
	}
	if mat.EmissiveTexture.EffectiveTexCoord() != 2 {
		t.Fatalf("EmissiveTexture.EffectiveTexCoord = %d, want 2", mat.EmissiveTexture.EffectiveTexCoord())
	}

	// BaseColorTexture: TEXCOORD_1 [(0,0), (1,0), (0,1)] -> scale(2,4) -> rot(+pi/2) -> offset(0.5, 0.25)
	// (0,0) -> (0.5, 0.25)
	// (1,0) -> (2,0) -> (0, -2) -> (0.5, -1.75)
	// (0,1) -> (0,4) -> (4, 0)  -> (4.5, 0.25)
	wantBC := []math3d.Vec2{{X: 0.5, Y: 0.25}, {X: 0.5, Y: -1.75}, {X: 4.5, Y: 0.25}}
	buf := make([]math3d.Vec2, 0, 3)
	gotBC, err := doc.TransformTexCoords(prim, mat.BaseColorTexture, buf)
	if err != nil {
		t.Fatalf("TransformTexCoords(baseColor): %v", err)
	}
	if &gotBC[0] != &buf[:1][0] {
		t.Fatal("TransformTexCoords did not reuse caller buffer")
	}
	for i := range wantBC {
		if !gotBC[i].ApproxEqual(wantBC[i], 1e-5) {
			t.Fatalf("baseColor UV[%d] = %v, want %v", i, gotBC[i], wantBC[i])
		}
	}

	// EmissiveTexture: TEXCOORD_2 [(0.25,0.5), (0.75,0.5), (0.25,1.0)] -> scale(2,2) + offset(0.1,-0.2)
	wantEm := []math3d.Vec2{{X: 0.6, Y: 0.8}, {X: 1.6, Y: 0.8}, {X: 0.6, Y: 1.8}}
	gotEm, err := doc.TransformTexCoords(prim, mat.EmissiveTexture, nil)
	if err != nil {
		t.Fatalf("TransformTexCoords(emissive): %v", err)
	}
	for i := range wantEm {
		if !gotEm[i].ApproxEqual(wantEm[i], 1e-5) {
			t.Fatalf("emissive UV[%d] = %v, want %v", i, gotEm[i], wantEm[i])
		}
	}

	// Untransformed texture ref copies TEXCOORD_1 unchanged.
	gotOcc, err := doc.TransformTexCoords(prim, mat.OcclusionTexture, nil)
	if err != nil {
		t.Fatalf("TransformTexCoords(occlusion): %v", err)
	}
	for i := range prim.TexCoords1 {
		if gotOcc[i] != prim.TexCoords1[i] {
			t.Fatalf("occlusion UV[%d] = %v, want %v", i, gotOcc[i], prim.TexCoords1[i])
		}
		if mat.OcclusionTexture.ApplyUV(prim.TexCoords1[i]) != prim.TexCoords1[i] {
			t.Fatalf("OcclusionTexture.ApplyUV changed untransformed UV")
		}
	}

	// Error cases for TransformTexCoords / PrimitiveTexCoords.
	if _, err := doc.TransformTexCoords(prim, nil, nil); err == nil {
		t.Fatal("TransformTexCoords(nil) succeeded, want error")
	}
	if _, err := doc.PrimitiveTexCoords(prim, -1); err == nil {
		t.Fatal("PrimitiveTexCoords(-1) succeeded, want error")
	}
	if _, err := doc.PrimitiveTexCoords(prim, 3); err == nil || !strings.Contains(err.Error(), "TEXCOORD_3") {
		t.Fatalf("PrimitiveTexCoords(3) = %v, want error naming TEXCOORD_3", err)
	}
}

func TestMaterialsVariantsRequiredAndMultiPrimitiveReference(t *testing.T) {
	// GLB with KHR_materials_variants in extensionsRequired, 3 variants,
	// multi-variant mappings ([0, 2] -> mat 2), a primitive without a default
	// material (-1), and an unmapped primitive.
	var bin bytes.Buffer
	posOff := appendFloat32s(&bin, 0, 0, 0, 1, 0, 0, 0, 1, 0)
	posLen := bin.Len() - posOff

	raw := map[string]any{
		"asset":              map[string]any{"version": "2.0"},
		"extensionsUsed":     []string{"KHR_materials_variants"},
		"extensionsRequired": []string{"KHR_materials_variants"},
		"extensions": map[string]any{
			"KHR_materials_variants": map[string]any{
				"variants": []any{
					map[string]any{"name": "Day"},
					map[string]any{"name": "Night"},
					map[string]any{"name": "Retro"},
				},
			},
		},
		"buffers":     []any{map[string]any{"byteLength": bin.Len()}},
		"bufferViews": []any{map[string]any{"buffer": 0, "byteOffset": posOff, "byteLength": posLen}},
		"accessors":   []any{map[string]any{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3"}},
		"materials": []any{
			map[string]any{"name": "DefaultWhite", "pbrMetallicRoughness": map[string]any{"baseColorFactor": []float32{1, 1, 1, 1}}},
			map[string]any{"name": "NightBlue", "pbrMetallicRoughness": map[string]any{"baseColorFactor": []float32{0.1, 0.2, 0.8, 1}}},
			map[string]any{"name": "WarmGold", "pbrMetallicRoughness": map[string]any{"baseColorFactor": []float32{0.9, 0.7, 0.1, 1}}},
		},
		"meshes": []any{
			map[string]any{
				"name": "MultiPrim",
				"primitives": []any{
					// Prim 0: default=0, Day & Retro -> 2, Night -> 1
					map[string]any{
						"attributes": map[string]int{"POSITION": 0},
						"material":   0,
						"extensions": map[string]any{
							"KHR_materials_variants": map[string]any{
								"mappings": []any{
									map[string]any{"material": 2, "variants": []int{0, 2}},
									map[string]any{"material": 1, "variants": []int{1}},
								},
							},
						},
					},
					// Prim 1: no default material (-1), only Night -> 1
					map[string]any{
						"attributes": map[string]int{"POSITION": 0},
						"extensions": map[string]any{
							"KHR_materials_variants": map[string]any{
								"mappings": []any{
									map[string]any{"material": 1, "variants": []int{1}},
								},
							},
						},
					},
					// Prim 2: default=0, no variant mappings
					map[string]any{
						"attributes": map[string]int{"POSITION": 0},
						"material":   0,
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
		t.Fatalf("Load GLB with required KHR_materials_variants: %v", err)
	}

	wantByVariant := []struct {
		name string
		idx  int
		mats []int
	}{
		{"default", -1, []int{0, -1, 0}},
		{"Day", 0, []int{2, -1, 0}},
		{"Night", 1, []int{1, 1, 0}},
		{"Retro", 2, []int{2, -1, 0}},
	}
	for _, tc := range wantByVariant {
		got, err := doc.MeshMaterials(0, tc.idx)
		if err != nil {
			t.Fatalf("%s: MeshMaterials: %v", tc.name, err)
		}
		for pi, wantMat := range tc.mats {
			if got[pi] != wantMat {
				t.Fatalf("%s prim %d: got material %d, want %d", tc.name, pi, got[pi], wantMat)
			}
			if tc.idx >= 0 {
				if byName := doc.MaterialForVariant(doc.Meshes[0].Primitives[pi], tc.name); byName != wantMat {
					t.Fatalf("%s prim %d by name: got %d, want %d", tc.name, pi, byName, wantMat)
				}
			}
		}
	}
}

func TestTextureTransformRejectsMalformed(t *testing.T) {
	makeDoc := func(slot string, ttJSON string) []byte {
		texInfo := `{"index":0,"extensions":{"KHR_texture_transform":` + ttJSON + `}}`
		var matBody string
		switch slot {
		case "baseColorTexture":
			matBody = `{"pbrMetallicRoughness":{"baseColorTexture":` + texInfo + `}}`
		case "metallicRoughnessTexture":
			matBody = `{"pbrMetallicRoughness":{"metallicRoughnessTexture":` + texInfo + `}}`
		default:
			matBody = `{` + `"` + slot + `":` + texInfo + `}`
		}
		return []byte(`{
			"asset": {"version": "2.0"},
			"textures": [{}],
			"materials": [` + matBody + `]
		}`)
	}

	cases := []struct {
		name string
		slot string
		tt   string
		want string
	}{
		{"null extension", "baseColorTexture", `null`, "JSON object"},
		{"array extension", "baseColorTexture", `[]`, "JSON object"},
		{"offset null", "baseColorTexture", `{"offset":null}`, "2-element"},
		{"offset empty", "baseColorTexture", `{"offset":[]}`, "must have 2 elements"},
		{"offset len 1", "baseColorTexture", `{"offset":[0.5]}`, "must have 2 elements"},
		{"offset len 3", "baseColorTexture", `{"offset":[0.1,0.2,0.3]}`, "must have 2 elements"},
		{"offset null elem", "baseColorTexture", `{"offset":[null,0]}`, "finite numbers"},
		{"offset overflow", "baseColorTexture", `{"offset":[1e40,0]}`, "parse KHR_texture_transform offset"},
		{"offset string", "baseColorTexture", `{"offset":["a","b"]}`, "parse KHR_texture_transform offset"},
		{"scale null", "normalTexture", `{"scale":null}`, "2-element"},
		{"scale len 1", "normalTexture", `{"scale":[2]}`, "must have 2 elements"},
		{"scale len 3", "metallicRoughnessTexture", `{"scale":[1,1,1]}`, "must have 2 elements"},
		{"scale null elem", "occlusionTexture", `{"scale":[1,null]}`, "finite numbers"},
		{"scale overflow", "emissiveTexture", `{"scale":[1,-1e40]}`, "parse KHR_texture_transform scale"},
		{"rotation null", "baseColorTexture", `{"rotation":null}`, "finite number"},
		{"rotation string", "baseColorTexture", `{"rotation":"half"}`, "parse KHR_texture_transform rotation"},
		{"rotation overflow", "baseColorTexture", `{"rotation":1e40}`, "parse KHR_texture_transform rotation"},
		{"texCoord null", "baseColorTexture", `{"texCoord":null}`, "non-negative integer"},
		{"texCoord negative", "baseColorTexture", `{"texCoord":-1}`, "negative texCoord"},
		{"texCoord float", "baseColorTexture", `{"texCoord":1.5}`, "parse KHR_texture_transform texCoord"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gltf.Load(makeDoc(tc.slot, tc.tt), nil)
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

func TestMaterialsVariantsRejectsMalformed(t *testing.T) {
	posBin := base64.StdEncoding.EncodeToString(floats(0, 0, 0, 1, 0, 0, 0, 1, 0))
	baseDoc := func(topExt, primExt string) []byte {
		top := ""
		if topExt != "" {
			top = `"extensions":{"KHR_materials_variants":` + topExt + `},`
		}
		prim := `{"attributes":{"POSITION":0},"material":0`
		if primExt != "" {
			prim += `,"extensions":{"KHR_materials_variants":` + primExt + `}`
		}
		prim += `}`
		return []byte(`{
			"asset": {"version": "2.0"},
			` + top + `
			"buffers": [{"byteLength": 36, "uri": "data:application/octet-stream;base64,` + posBin + `"}],
			"bufferViews": [{"buffer": 0, "byteLength": 36}],
			"accessors": [{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3"}],
			"materials": [{"name": "A"}, {"name": "B"}],
			"meshes": [{"primitives": [` + prim + `]}]
		}`)
	}

	validTop := `{"variants":[{"name":"V0"},{"name":"V1"}]}`
	cases := []struct {
		name      string
		topExt    string
		primExt   string
		want      string
		wantField string
	}{
		{"top null", `null`, "", "JSON object", "extensions.KHR_materials_variants"},
		{"top array", `[]`, "", "JSON object", "extensions.KHR_materials_variants"},
		{"top missing variants", `{}`, "", "non-empty variants", "extensions.KHR_materials_variants"},
		{"top empty variants", `{"variants":[]}`, "", "non-empty variants", "extensions.KHR_materials_variants"},
		{"top null variant entry", `{"variants":[null]}`, "", "JSON object", "extensions.KHR_materials_variants"},
		{"top missing variant name", `{"variants":[{}]}`, "", "missing required name", "extensions.KHR_materials_variants"},
		{"top non-string variant name", `{"variants":[{"name":42}]}`, "", "parse KHR_materials_variants variant 0", "extensions.KHR_materials_variants"},

		{"prim null", validTop, `null`, "JSON object", "meshes[0]"},
		{"prim array", validTop, `[]`, "JSON object", "meshes[0]"},
		{"prim missing mappings", validTop, `{}`, "non-empty mappings", "meshes[0]"},
		{"prim empty mappings", validTop, `{"mappings":[]}`, "non-empty mappings", "meshes[0]"},
		{"prim null mapping", validTop, `{"mappings":[null]}`, "JSON object", "meshes[0]"},
		{"prim missing material", validTop, `{"mappings":[{"variants":[0]}]}`, "invalid material", "meshes[0]"},
		{"prim negative material", validTop, `{"mappings":[{"material":-1,"variants":[0]}]}`, "invalid material", "meshes[0]"},
		{"prim oob material", validTop, `{"mappings":[{"material":2,"variants":[0]}]}`, "invalid material", "meshes[0]"},
		{"prim empty variants", validTop, `{"mappings":[{"material":1,"variants":[]}]}`, "non-empty variants", "meshes[0]"},
		{"prim null variant index", validTop, `{"mappings":[{"material":1,"variants":[null]}]}`, "null variant index", "meshes[0]"},
		{"prim negative variant index", validTop, `{"mappings":[{"material":1,"variants":[-1]}]}`, "invalid variant -1", "meshes[0]"},
		{"prim oob variant index", validTop, `{"mappings":[{"material":1,"variants":[2]}]}`, "invalid variant 2", "meshes[0]"},
		{"prim variant without top-level variants", "", `{"mappings":[{"material":1,"variants":[0]}]}`, "invalid variant 0", "meshes[0]"},
		{"prim duplicate variant in same mapping", validTop, `{"mappings":[{"material":1,"variants":[0,0]}]}`, "mapped more than once", "meshes[0]"},
		{"prim duplicate variant across mappings", validTop, `{"mappings":[{"material":0,"variants":[0]},{"material":1,"variants":[0]}]}`, "mapped more than once", "meshes[0]"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gltf.Load(baseDoc(tc.topExt, tc.primExt), nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want one containing %q", err, tc.want)
			}
			le, ok := loaderr.As(err)
			if !ok || le.Field != tc.wantField {
				t.Fatalf("expected *loaderr.Error at %q, got %T (%v)", tc.wantField, err, err)
			}
		})
	}
}

func TestMaterialEmissiveStrengthRequired(t *testing.T) {
	doc, err := gltf.Load([]byte(`{
		"asset": {"version": "2.0"},
		"extensionsUsed": ["KHR_materials_emissive_strength"],
		"extensionsRequired": ["KHR_materials_emissive_strength"],
		"materials": [{"emissiveFactor": [1, 0.5, 0], "extensions": {"KHR_materials_emissive_strength": {"emissiveStrength": 4}}}]
	}`), nil)
	if err != nil {
		t.Fatalf("Load with required KHR_materials_emissive_strength: %v", err)
	}
	if got, err := doc.Materials[0].EmissiveStrength(); err != nil || got != 4 {
		t.Fatalf("EmissiveStrength() = (%v, %v), want (4, nil)", got, err)
	}
}

func TestMaterialEmissiveStrength(t *testing.T) {
	validCases := []struct {
		name string
		ext  json.RawMessage
		want float32
	}{
		{"absent", nil, 1},
		{"empty object", json.RawMessage(`{}`), 1},
		{"zero", json.RawMessage(`{"emissiveStrength": 0}`), 0},
		{"positive", json.RawMessage(`{"emissiveStrength": 3.5}`), 3.5},
	}
	for _, tc := range validCases {
		t.Run(tc.name, func(t *testing.T) {
			var m gltf.Material
			if tc.ext != nil {
				m.Extensions = map[string]json.RawMessage{"KHR_materials_emissive_strength": tc.ext}
			}
			got, err := m.EmissiveStrength()
			if err != nil {
				t.Fatalf("EmissiveStrength() err = %v, want nil", err)
			}
			if got != tc.want {
				t.Fatalf("EmissiveStrength() = %v, want %v", got, tc.want)
			}
		})
	}

	badCases := []struct {
		name string
		ext  json.RawMessage
	}{
		{"null extension", json.RawMessage(`null`)},
		{"array extension", json.RawMessage(`[]`)},
		{"null property", json.RawMessage(`{"emissiveStrength": null}`)},
		{"string property", json.RawMessage(`{"emissiveStrength": "bright"}`)},
		{"negative", json.RawMessage(`{"emissiveStrength": -0.5}`)},
		{"overflow", json.RawMessage(`{"emissiveStrength": 1e40}`)},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			m := gltf.Material{
				Extensions: map[string]json.RawMessage{"KHR_materials_emissive_strength": tc.ext},
			}
			if got, err := m.EmissiveStrength(); err == nil {
				t.Fatalf("EmissiveStrength() = (%v, nil), want error", got)
			}
		})
	}
}
