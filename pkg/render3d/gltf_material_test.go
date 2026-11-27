package render3d_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func pngDataURI(t *testing.T, img image.Image) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func solidImage(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func transformGLTFMesh(prim gltf.Primitive, world math3d.Mat4) render3d.Mesh {
	positions := make([]math3d.Vec3, len(prim.Positions))
	for i, p := range prim.Positions {
		positions[i] = world.TransformPoint(p)
	}
	var normals []math3d.Vec3
	if len(prim.Normals) > 0 {
		// Upper-left 3x3 inverse transpose handles non-uniform scale; for rigid transforms it equals the rotation matrix.
		inv, ok := world.Inverse()
		normMat := world
		if ok {
			normMat = inv.Transpose()
		}
		normals = make([]math3d.Vec3, len(prim.Normals))
		for i, n := range prim.Normals {
			normals[i] = normMat.TransformDir(n).Norm()
		}
	}
	return render3d.Mesh{
		Positions: positions,
		Normals:   normals,
		UVs:       prim.TexCoords0,
		Indices:   prim.Indices,
	}
}

// damagedHelmetDir holds the Khronos DamagedHelmet fixture. It is a separate module because the model is CC-BY-NC-4.0, so a downloaded copy of this module lacks it (testdata/damagedhelmet/README.md).
var damagedHelmetDir = filepath.Join("testdata", "damagedhelmet")

func loadDamagedHelmet(t *testing.T) (*gltf.Document, render3d.Mesh) {
	t.Helper()
	dir := filepath.Join(damagedHelmetDir, "glTF")
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		t.Skipf("DamagedHelmet fixture %s is not in this copy of the module", damagedHelmetDir)
	}
	doc, err := gltf.LoadFS(os.DirFS(dir), "DamagedHelmet.gltf")
	if err != nil {
		t.Fatalf("gltf.LoadFS DamagedHelmet: %v", err)
	}
	if err := doc.ComputeWorldMatrices(); err != nil {
		t.Fatalf("ComputeWorldMatrices: %v", err)
	}
	if len(doc.Nodes) == 0 || len(doc.Meshes) == 0 || len(doc.Meshes[0].Primitives) == 0 {
		t.Fatal("DamagedHelmet missing expected node/mesh/primitive")
	}
	mesh := transformGLTFMesh(doc.Meshes[0].Primitives[0], doc.Nodes[0].WorldMatrix)
	return doc, mesh
}

func TestSamplerOptionsFromGLTFAndAlphaMode(t *testing.T) {
	// Default empty glTF sampler ({}): linear + linear mipmaps + repeat wrapping.
	opts, wantMips, err := render3d.SamplerOptionsFromGLTF(gltf.Sampler{})
	if err != nil {
		t.Fatalf("empty sampler: %v", err)
	}
	if !wantMips || opts.MinFilter != gpu.FilterLinear || opts.MagFilter != gpu.FilterLinear ||
		opts.MipmapFilter != gpu.FilterLinear || opts.AddressU != gpu.AddressRepeat || opts.AddressV != gpu.AddressRepeat {
		t.Errorf("empty sampler = (%+v, mips=%v), want linear-mipmap-linear repeat", opts, wantMips)
	}

	filterCases := []struct {
		name     string
		s        gltf.Sampler
		wantMin  gpu.FilterMode
		wantMag  gpu.FilterMode
		wantMip  gpu.FilterMode
		wantMips bool
		wantU    gpu.AddressMode
		wantV    gpu.AddressMode
	}{
		{
			name:     "nearest clamp",
			s:        gltf.Sampler{MagFilter: gltf.FilterNearest, MinFilter: gltf.FilterNearest, WrapS: gltf.WrapClampToEdge, WrapT: gltf.WrapClampToEdge},
			wantMin:  gpu.FilterNearest,
			wantMag:  gpu.FilterNearest,
			wantMip:  gpu.FilterNearest,
			wantMips: false,
			wantU:    gpu.AddressClampToEdge,
			wantV:    gpu.AddressClampToEdge,
		},
		{
			name:     "linear non-mip mirror",
			s:        gltf.Sampler{MagFilter: gltf.FilterLinear, MinFilter: gltf.FilterLinear, WrapS: gltf.WrapMirroredRepeat, WrapT: gltf.WrapRepeat},
			wantMin:  gpu.FilterLinear,
			wantMag:  gpu.FilterLinear,
			wantMip:  gpu.FilterNearest,
			wantMips: false,
			wantU:    gpu.AddressMirrorRepeat,
			wantV:    gpu.AddressRepeat,
		},
		{
			name:     "nearest mipmap nearest",
			s:        gltf.Sampler{MinFilter: gltf.FilterNearestMipmapNearest},
			wantMin:  gpu.FilterNearest,
			wantMag:  gpu.FilterLinear,
			wantMip:  gpu.FilterNearest,
			wantMips: true,
			wantU:    gpu.AddressRepeat,
			wantV:    gpu.AddressRepeat,
		},
		{
			name:     "linear mipmap nearest",
			s:        gltf.Sampler{MinFilter: gltf.FilterLinearMipmapNearest},
			wantMin:  gpu.FilterLinear,
			wantMag:  gpu.FilterLinear,
			wantMip:  gpu.FilterNearest,
			wantMips: true,
			wantU:    gpu.AddressRepeat,
			wantV:    gpu.AddressRepeat,
		},
		{
			name:     "nearest mipmap linear",
			s:        gltf.Sampler{MinFilter: gltf.FilterNearestMipmapLinear},
			wantMin:  gpu.FilterNearest,
			wantMag:  gpu.FilterLinear,
			wantMip:  gpu.FilterLinear,
			wantMips: true,
			wantU:    gpu.AddressRepeat,
			wantV:    gpu.AddressRepeat,
		},
		{
			name:     "linear mipmap linear",
			s:        gltf.Sampler{MagFilter: gltf.FilterLinear, MinFilter: gltf.FilterLinearMipmapLinear},
			wantMin:  gpu.FilterLinear,
			wantMag:  gpu.FilterLinear,
			wantMip:  gpu.FilterLinear,
			wantMips: true,
			wantU:    gpu.AddressRepeat,
			wantV:    gpu.AddressRepeat,
		},
	}
	for _, tc := range filterCases {
		t.Run(tc.name, func(t *testing.T) {
			got, mips, err := render3d.SamplerOptionsFromGLTF(tc.s)
			if err != nil {
				t.Fatalf("SamplerOptionsFromGLTF: %v", err)
			}
			if mips != tc.wantMips || got.MinFilter != tc.wantMin || got.MagFilter != tc.wantMag ||
				got.MipmapFilter != tc.wantMip || got.AddressU != tc.wantU || got.AddressV != tc.wantV {
				t.Errorf("got (%+v, mips=%v), want min=%v mag=%v mip=%v mips=%v u=%v v=%v",
					got, mips, tc.wantMin, tc.wantMag, tc.wantMip, tc.wantMips, tc.wantU, tc.wantV)
			}
			if err := got.Validate(); err != nil {
				t.Errorf("produced SamplerOptions failed Validate: %v", err)
			}
		})
	}

	// Unsupported filter / wrap constants wrap ErrInvalidSampler.
	badSamplers := []gltf.Sampler{
		{MagFilter: gltf.FilterMode(9999)},
		{MinFilter: gltf.FilterMode(9999)},
		{WrapS: gltf.WrapMode(12345)},
		{WrapT: gltf.WrapMode(12345)},
	}
	for i, bs := range badSamplers {
		if _, _, err := render3d.SamplerOptionsFromGLTF(bs); !errors.Is(err, render3d.ErrInvalidSampler) {
			t.Errorf("badSamplers[%d]: err = %v, want wrapping ErrInvalidSampler", i, err)
		}
	}

	// AlphaModeFromGLTF.
	alphaCases := []struct {
		in   gltf.AlphaMode
		want render3d.AlphaMode
	}{
		{"", render3d.AlphaOpaque},
		{gltf.AlphaOpaque, render3d.AlphaOpaque},
		{gltf.AlphaMask, render3d.AlphaMask},
		{gltf.AlphaBlend, render3d.AlphaBlend},
	}
	for _, ac := range alphaCases {
		got, err := render3d.AlphaModeFromGLTF(ac.in)
		if err != nil || got != ac.want {
			t.Errorf("AlphaModeFromGLTF(%q) = (%v, %v), want (%v, nil)", ac.in, got, err, ac.want)
		}
	}
	if _, err := render3d.AlphaModeFromGLTF(gltf.AlphaMode("ADDITIVE")); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Errorf("unknown alpha mode err = %v, want wrapping ErrInvalidMaterial", err)
	}
}

func TestUploadGLTFMaterialsDeduplicationAndColorSpaces(t *testing.T) {
	albedoURI := pngDataURI(t, solidImage(8, 8, color.NRGBA{R: 200, G: 120, B: 60, A: 255}))
	ormURI := pngDataURI(t, solidImage(8, 8, color.NRGBA{R: 180, G: 100, B: 230, A: 255}))
	normalURI := pngDataURI(t, solidImage(8, 8, color.NRGBA{R: 128, G: 128, B: 255, A: 255}))
	emissiveURI := pngDataURI(t, solidImage(8, 8, color.NRGBA{R: 255, G: 140, B: 40, A: 255}))

	// Material 0 uses:
	//  - image 0 as baseColor (sRGB) with sampler 0 (linear mipmap linear repeat) + KHR_texture_transform
	//  - image 1 as BOTH metallicRoughness (linear) AND occlusion (linear) -> deduplicated to 1 linear texture!
	//  - image 2 as normal (linear) with scale 0.75
	//  - image 3 as emissive (sRGB) with KHR_materials_emissive_strength = 2.5
	// Material 1 shares:
	//  - image 1 as metallicRoughness (linear) AND image 0 as occlusion (linear!) -> image 0 is now uploaded in BOTH sRGB and linear!
	//  - sampler 1 (nearest clamp, no mipmaps)
	//  - texture 4 (no image source) as emissive -> falls back gracefully
	//  - alphaMode MASK, doubleSided true, KHR_materials_unlit
	docJSON := fmt.Sprintf(`{
		"asset": {"version": "2.0"},
		"extensionsUsed": ["KHR_texture_transform", "KHR_materials_unlit", "KHR_materials_emissive_strength"],
		"samplers": [
			{"name": "trilinear", "magFilter": 9729, "minFilter": 9987, "wrapS": 10497, "wrapT": 10497},
			{"name": "pixelated", "magFilter": 9728, "minFilter": 9728, "wrapS": 33071, "wrapT": 33071},
			{"name": "unused", "magFilter": 9729, "minFilter": 9729}
		],
		"images": [
			{"name": "albedo", "uri": %q},
			{"name": "orm", "uri": %q},
			{"name": "normal", "uri": %q},
			{"name": "emissive", "uri": %q}
		],
		"textures": [
			{"sampler": 0, "source": 0},
			{"sampler": 0, "source": 1},
			{"sampler": 0, "source": 2},
			{"sampler": 0, "source": 3},
			{"sampler": 1},
			{"sampler": 1, "source": 0}
		],
		"materials": [
			{
				"name": "HeroArmor",
				"pbrMetallicRoughness": {
					"baseColorFactor": [0.9, 0.85, 0.8, 1.0],
					"baseColorTexture": {
						"index": 0,
						"texCoord": 0,
						"extensions": {
							"KHR_texture_transform": {
								"offset": [0.25, 0.5],
								"rotation": 0.5,
								"scale": [2.0, 2.0],
								"texCoord": 1
							}
						}
					},
					"metallicFactor": 0.9,
					"roughnessFactor": 0.6,
					"metallicRoughnessTexture": {"index": 1}
				},
				"normalTexture": {"index": 2, "scale": 0.75},
				"occlusionTexture": {"index": 1, "strength": 0.85},
				"emissiveFactor": [0.4, 0.2, 0.1],
				"emissiveTexture": {"index": 3},
				"extensions": {
					"KHR_materials_emissive_strength": {"emissiveStrength": 2.5}
				}
			},
			{
				"name": "CutoutFoliage",
				"pbrMetallicRoughness": {
					"baseColorFactor": [0.5, 0.9, 0.4, 1.0],
					"metallicFactor": 0.0,
					"roughnessFactor": 0.8,
					"metallicRoughnessTexture": {"index": 1}
				},
				"occlusionTexture": {"index": 5, "strength": 0.5},
				"emissiveTexture": {"index": 4},
				"alphaMode": "MASK",
				"alphaCutoff": 0.35,
				"doubleSided": true,
				"extensions": {
					"KHR_materials_unlit": {}
				}
			}
		]
	}`, albedoURI, ormURI, normalURI, emissiveURI)

	doc, err := gltf.Load([]byte(docJSON), nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}

	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	defer func() { _ = defaults.Close() }()

	set, err := render3d.UploadGLTFMaterials(nil, dev, defaults, doc, render3d.GLTFMaterialUpload{
		Label:         "dedup",
		MaxAnisotropy: 4,
	})
	if err != nil {
		t.Fatalf("UploadGLTFMaterials: %v", err)
	}

	if !set.Valid() {
		t.Fatal("set not valid after upload")
	}
	if set.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", set.Len())
	}
	// Unique (imageIndex, sRGB) pairs:
	// (0, true), (1, false), (2, false), (3, true), (0, false) => 5 unique GPU textures! ORM (1, false) is shared across Mat0.MR, Mat0.Occ, and Mat1.MR -> uploaded ONCE.
	if set.TextureCount() != 5 {
		t.Fatalf("TextureCount() = %d, want 5", set.TextureCount())
	}
	// Only samplers 0 and 1 are referenced (sampler 2 is unused).
	if set.SamplerCount() != 2 {
		t.Fatalf("SamplerCount() = %d, want 2", set.SamplerCount())
	}

	info0, ok := set.Info(0)
	if !ok {
		t.Fatal("Info(0) returned false")
	}
	if info0.Name != "HeroArmor" || info0.Unlit {
		t.Errorf("info0 header = (%q, unlit=%v), want (HeroArmor, false)", info0.Name, info0.Unlit)
	}
	if !info0.BaseColor.Present || !info0.BaseColor.SRGB || !info0.BaseColor.Texture.SRGB() {
		t.Errorf("BaseColor slot must be present and sRGB: %+v", info0.BaseColor)
	}
	if info0.BaseColor.EffectiveTexCoord() != 1 || info0.BaseColor.Transform == nil || info0.BaseColor.Transform.Scale != [2]float32{2, 2} {
		t.Errorf("BaseColor KHR_texture_transform mismatch: %+v", info0.BaseColor)
	}
	// MaterialWGSL samples every slot with untransformed TEXCOORD_0, so the transformed base color on UV set 1 is reported.
	if got := info0.Unsupported(); !slices.Equal(got, []string{"baseColorTexture UVs"}) {
		t.Errorf("info0.Unsupported() = %q, want [baseColorTexture UVs]", got)
	}
	if !info0.BaseColor.NeedsUVBake() || info0.MetallicRoughness.NeedsUVBake() || info0.Emissive.NeedsUVBake() {
		t.Error("NeedsUVBake: want only the transformed base color slot")
	}
	if info0.MetallicRoughness.Texture != info0.Occlusion.Texture {
		t.Error("shared ORM image should reuse the exact same *GPUTexture for MetallicRoughness and Occlusion")
	}
	if info0.MetallicRoughness.Texture.SRGB() || info0.Normal.Texture.SRGB() || info0.Occlusion.Texture.SRGB() {
		t.Error("data maps (MR, Normal, Occlusion) must be linear (SRGB == false)")
	}
	if !info0.Emissive.Texture.SRGB() {
		t.Error("emissive map must be sRGB (SRGB == true)")
	}
	// KHR_materials_emissive_strength = 2.5 * [0.4, 0.2, 0.1] = [1.0, 0.5, 0.25].
	if info0.Material.EmissiveFactor != [3]float32{1.0, 0.5, 0.25} {
		t.Errorf("EmissiveFactor = %v, want [1.0, 0.5, 0.25]", info0.Material.EmissiveFactor)
	}
	if info0.Material.NormalScale != 0.75 || info0.Material.OcclusionStrength != 0.85 {
		t.Errorf("NormalScale=%v OcclusionStrength=%v, want 0.75, 0.85", info0.Material.NormalScale, info0.Material.OcclusionStrength)
	}
	// Sampler 0 requested mipmaps and is all-linear, so MaxAnisotropy=4 applies and texture has mip chain (8x8 -> 4 levels).
	if s0 := set.Sampler(0); s0 == nil || s0.Options().MaxAnisotropy != 4 {
		t.Errorf("sampler 0 MaxAnisotropy = %v, want 4", s0.Options().MaxAnisotropy)
	}
	if info0.BaseColor.Texture.MipLevels() != 4 {
		t.Errorf("mipmapped 8x8 baseColor MipLevels = %d, want 4", info0.BaseColor.Texture.MipLevels())
	}

	info1, ok := set.Info(1)
	if !ok {
		t.Fatal("Info(1) returned false")
	}
	if !info1.Unlit || info1.Material.AlphaMode != render3d.AlphaMask || info1.Material.AlphaCutoff != 0.35 || !info1.Material.DoubleSided {
		t.Errorf("info1 fields mismatch: %+v", info1.Material)
	}
	if got := info1.Unsupported(); !slices.Equal(got, []string{"KHR_materials_unlit"}) {
		t.Errorf("info1.Unsupported() = %q, want [KHR_materials_unlit]", got)
	}
	if info1.MetallicRoughness.Texture != info0.MetallicRoughness.Texture {
		t.Error("Mat1 should share the same ORM *GPUTexture as Mat0")
	}
	// Image 0 used as linear Occlusion on Mat1 must be distinct from sRGB BaseColor on Mat0 and non-mipmapped (sampler 1 is FilterNearest).
	if info1.Occlusion.Texture == info0.BaseColor.Texture || info1.Occlusion.Texture.SRGB() {
		t.Error("image 0 used as linear occlusion must be a separate linear GPUTexture")
	}
	if info1.Occlusion.Texture.MipLevels() != 1 {
		t.Errorf("nearest non-mipmapped occlusion texture MipLevels = %d, want 1", info1.Occlusion.Texture.MipLevels())
	}
	// Sampler 1 is nearest -> MaxAnisotropy must stay 1 (WebGPU rule).
	if s1 := set.Sampler(1); s1 == nil || s1.Options().MaxAnisotropy != 1 {
		t.Errorf("nearest sampler MaxAnisotropy = %v, want 1", s1.Options().MaxAnisotropy)
	}
	if set.Sampler(2) != nil {
		t.Error("unused sampler 2 should not have been created")
	}
	// Texture 4 has no image source -> Emissive slot gracefully falls back to absent.
	if info1.Emissive.Present || info1.Material.EmissiveTexture != nil {
		t.Errorf("texture with source=-1 should fall back to absent: %+v", info1.Emissive)
	}

	// Default material & lookup helpers.
	if set.Material(-1) != set.DefaultMaterial() || set.DefaultMaterial() == nil {
		t.Error("Material(-1) should return non-nil DefaultMaterial()")
	}
	if set.Material(0) != info0.GPU || set.Material(2) != nil || set.Material(-2) != nil {
		t.Error("Material(index) lookup mismatch")
	}
	if set.Texture(0, true) != info0.BaseColor.Texture || set.Texture(0, false) != info1.Occlusion.Texture {
		t.Error("Texture(imageIndex, srgb) lookup mismatch")
	}
	if set.ByteSize() == 0 {
		t.Error("ByteSize() = 0, want > 0")
	}

	// DisableMipmaps suppresses mipmap generation even for mipmapped samplers.
	noMipSet, err := render3d.UploadGLTFMaterials(nil, dev, defaults, doc, render3d.GLTFMaterialUpload{
		DisableMipmaps: true,
	})
	if err != nil {
		t.Fatalf("UploadGLTFMaterials(DisableMipmaps): %v", err)
	}
	if got := noMipSet.Texture(0, true).MipLevels(); got != 1 {
		t.Errorf("DisableMipmaps MipLevels = %d, want 1", got)
	}
	if err := noMipSet.Close(); err != nil {
		t.Fatalf("noMipSet.Close: %v", err)
	}

	// Close & idempotency + nil receiver safety.
	if err := set.Close(); err != nil {
		t.Fatalf("set.Close: %v", err)
	}
	if set.Valid() {
		t.Error("set still valid after Close")
	}
	if err := set.Close(); err != nil {
		t.Errorf("second Close returned error: %v", err)
	}
	var nilSet *render3d.GLTFMaterialSet
	if nilSet.Len() != 0 || nilSet.TextureCount() != 0 || nilSet.SamplerCount() != 0 || nilSet.ByteSize() != 0 ||
		nilSet.Valid() || nilSet.DefaultMaterial() != nil || nilSet.Material(0) != nil || nilSet.Close() != nil {
		t.Error("nil GLTFMaterialSet methods should return safe zero values")
	}

	// Document with zero materials still creates a valid default material.
	emptyDoc := &gltf.Document{}
	emptySet, err := render3d.UploadGLTFMaterials(nil, dev, defaults, emptyDoc, render3d.GLTFMaterialUpload{})
	if err != nil {
		t.Fatalf("UploadGLTFMaterials(empty doc): %v", err)
	}
	defer func() { _ = emptySet.Close() }()
	if emptySet.Len() != 0 || emptySet.TextureCount() != 0 || !emptySet.DefaultMaterial().Valid() {
		t.Error("empty document should produce a valid DefaultMaterial with 0 materials and 0 textures")
	}
}

func TestUploadGLTFMaterialsVariantsAndRendering(t *testing.T) {
	// sRGB (180, 30, 30) red vs sRGB (30, 190, 60) green + mid-gray (128, 128, 128) sRGB test.
	redURI := pngDataURI(t, solidImage(4, 4, color.NRGBA{R: 220, G: 30, B: 30, A: 255}))
	greenURI := pngDataURI(t, solidImage(4, 4, color.NRGBA{R: 30, G: 220, B: 60, A: 255}))
	cutoutURI := pngDataURI(t, cutoutCircleImage(16))
	midGrayURI := pngDataURI(t, solidImage(4, 4, color.NRGBA{R: 128, G: 128, B: 128, A: 255}))

	docJSON := fmt.Sprintf(`{
		"asset": {"version": "2.0"},
		"extensionsUsed": ["KHR_materials_variants"],
		"extensions": {
			"KHR_materials_variants": {
				"variants": [
					{"name": "Crimson"},
					{"name": "Emerald"}
				]
			}
		},
		"images": [
			{"uri": %q},
			{"uri": %q},
			{"uri": %q},
			{"uri": %q}
		],
		"textures": [
			{"source": 0},
			{"source": 1},
			{"source": 2},
			{"source": 3}
		],
		"materials": [
			{
				"name": "CrimsonPaint",
				"pbrMetallicRoughness": {
					"baseColorTexture": {"index": 0},
					"metallicFactor": 0.0,
					"roughnessFactor": 0.5
				}
			},
			{
				"name": "EmeraldPaint",
				"pbrMetallicRoughness": {
					"baseColorTexture": {"index": 1},
					"metallicFactor": 0.0,
					"roughnessFactor": 0.5
				}
			},
			{
				"name": "CutoutMask",
				"pbrMetallicRoughness": {
					"baseColorTexture": {"index": 2},
					"metallicFactor": 0.0,
					"roughnessFactor": 0.6
				},
				"alphaMode": "MASK",
				"alphaCutoff": 0.5,
				"doubleSided": true
			},
			{
				"name": "BlueGlass",
				"pbrMetallicRoughness": {
					"baseColorFactor": [0.15, 0.45, 1.0, 0.45],
					"metallicFactor": 0.0,
					"roughnessFactor": 0.2
				},
				"alphaMode": "BLEND"
			},
			{
				"name": "MidGraySRGB",
				"pbrMetallicRoughness": {
					"baseColorTexture": {"index": 3},
					"metallicFactor": 0.0,
					"roughnessFactor": 0.8
				}
			},
			{
				"name": "MidGrayLinearFactor",
				"pbrMetallicRoughness": {
					"baseColorFactor": [0.502, 0.502, 0.502, 1.0],
					"metallicFactor": 0.0,
					"roughnessFactor": 0.8
				}
			}
		]
	}`, redURI, greenURI, cutoutURI, midGrayURI)

	doc, err := gltf.Load([]byte(docJSON), nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}

	const size = 48
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()

	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	defer func() { _ = defaults.Close() }()

	matSet, err := render3d.UploadGLTFMaterials(nil, dev, defaults, doc, render3d.GLTFMaterialUpload{Label: "variants"})
	if err != nil {
		t.Fatalf("UploadGLTFMaterials: %v", err)
	}
	defer func() { _ = matSet.Close() }()

	prim := gltf.Primitive{
		Material:        0,
		VariantMappings: map[int]int{0: 0, 1: 1},
	}
	unassignedPrim := gltf.Primitive{Material: -1}

	if matSet.MaterialForVariant(prim, "Crimson") != matSet.Material(0) {
		t.Error("Crimson variant did not resolve to material 0")
	}
	if matSet.MaterialForVariant(prim, "Emerald") != matSet.Material(1) {
		t.Error("Emerald variant did not resolve to material 1")
	}
	if matSet.MaterialForVariant(prim, "NonExistent") != matSet.Material(0) {
		t.Error("unknown variant should fall back to prim.Material (0)")
	}
	if matSet.MaterialForPrimitive(unassignedPrim, -1) != matSet.DefaultMaterial() {
		t.Error("unassigned primitive should resolve to DefaultMaterial()")
	}
	if info, ok := matSet.InfoForVariant(prim, "Emerald"); !ok || info.Name != "EmeraldPaint" {
		t.Errorf("InfoForVariant(Emerald) = (%q, %v), want (EmeraldPaint, true)", info.Name, ok)
	}
	if _, ok := matSet.InfoForPrimitive(unassignedPrim, -1); ok {
		t.Error("InfoForPrimitive on unassigned primitive should return false")
	}

	opaquePipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		AlphaMode:   render3d.AlphaOpaque,
	})
	if err != nil {
		t.Fatalf("opaque pipeline: %v", err)
	}
	maskPipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		AlphaMode:   render3d.AlphaMask,
		DoubleSided: true,
	})
	if err != nil {
		t.Fatalf("mask pipeline: %v", err)
	}
	blendPipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		AlphaMode:   render3d.AlphaBlend,
	})
	if err != nil {
		t.Fatalf("blend pipeline: %v", err)
	}
	quad, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3{}, 1.4, 1.4), render3d.MeshUpload{Label: "quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	frontQuad, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3{Z: 0.4}, 1.4, 1.4), render3d.MeshUpload{Label: "front-quad"})
	if err != nil {
		t.Fatalf("UploadMesh front: %v", err)
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(45), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	renderWith := func(pipe *gpu.Pipeline, m *render3d.GPUMaterial) *image.RGBA {
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		m.Bind(pass)
		quad.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("renderer error: %v", err)
		}
		return cloneRGBA(sr.Image())
	}

	crimsonImg := renderWith(opaquePipe, matSet.MaterialForVariant(prim, "Crimson"))
	if c := crimsonImg.RGBAAt(size/2, size/2); c.R <= c.G+50 {
		t.Errorf("Crimson variant center pixel = %v, want red-dominant", c)
	}

	emeraldImg := renderWith(opaquePipe, matSet.MaterialForVariant(prim, "Emerald"))
	if c := emeraldImg.RGBAAt(size/2, size/2); c.G <= c.R+50 {
		t.Errorf("Emerald variant center pixel = %v, want green-dominant", c)
	}

	maskImg := renderWith(maskPipe, matSet.Material(2))
	// Center of cutout circle is opaque teal; corner of quad is discarded back to clear color.
	if c := maskImg.RGBAAt(size/2, size/2); c.G < 80 {
		t.Errorf("CutoutMask center pixel = %v, want lit teal inside circle", c)
	}
	wantPixel(t, maskImg, 8, 8, meshClear)

	// AlphaBlend composites translucent blue glass over opaque crimson background.
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	pass.SetPipeline(opaquePipe)
	matSet.Material(0).Bind(pass)
	quad.Draw(pass)
	pass.SetPipeline(blendPipe)
	matSet.Material(3).Bind(pass)
	frontQuad.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("blend pass error: %v", err)
	}
	redCenter := crimsonImg.RGBAAt(size/2, size/2)
	blendCenter := sr.Image().RGBAAt(size/2, size/2)
	if blendCenter.B <= redCenter.B+30 || blendCenter.R >= redCenter.R-10 {
		t.Errorf("AlphaBlend center = %v vs opaque red %v, want visibly blended blue+red", blendCenter, redCenter)
	}

	// sRGB baseColorTexture (128/255 -> ~0.215 linear) renders darker than linear baseColorFactor 0.502.
	srgbGray := renderWith(opaquePipe, matSet.Material(4)).RGBAAt(size/2, size/2)
	linGray := renderWith(opaquePipe, matSet.Material(5)).RGBAAt(size/2, size/2)
	if srgbGray.R >= linGray.R-20 {
		t.Errorf("sRGB mid-gray baseColor (%v) should decode darker than linear 0.502 (%v)", srgbGray, linGray)
	}
}

func TestUploadGLTFMaterialsErrors(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	defer func() { _ = defaults.Close() }()

	if _, err := render3d.UploadGLTFMaterials(nil, nil, defaults, &gltf.Document{}, render3d.GLTFMaterialUpload{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("nil device err = %v, want gpu.ErrDeviceLost", err)
	}
	if _, err := render3d.UploadGLTFMaterials(nil, dev, nil, &gltf.Document{}, render3d.GLTFMaterialUpload{}); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Errorf("nil defaults err = %v, want ErrInvalidMaterial", err)
	}
	if _, err := render3d.UploadGLTFMaterials(nil, dev, defaults, nil, render3d.GLTFMaterialUpload{}); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Errorf("nil doc err = %v, want ErrInvalidMaterial", err)
	}

	validBase := func() gltf.Material {
		return gltf.Material{
			BaseColorFactor: math3d.Vec4One(),
			MetallicFactor:  1,
			RoughnessFactor: 1,
			AlphaMode:       gltf.AlphaOpaque,
			AlphaCutoff:     0.5,
		}
	}

	// Out-of-range texture index.
	mBadTex := validBase()
	mBadTex.BaseColorTexture = &gltf.TextureRef{TextureIndex: 5}
	if _, err := render3d.UploadGLTFMaterials(nil, dev, defaults, &gltf.Document{Materials: []gltf.Material{mBadTex}}, render3d.GLTFMaterialUpload{}); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Errorf("bad texture index err = %v, want ErrInvalidMaterial", err)
	}

	// Out-of-range image index.
	mBadImg := validBase()
	mBadImg.BaseColorTexture = &gltf.TextureRef{TextureIndex: 0}
	docBadImg := &gltf.Document{
		Textures:  []gltf.Texture{{Sampler: -1, Source: 3}},
		Materials: []gltf.Material{mBadImg},
	}
	if _, err := render3d.UploadGLTFMaterials(nil, dev, defaults, docBadImg, render3d.GLTFMaterialUpload{}); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Errorf("bad image index err = %v, want ErrInvalidMaterial", err)
	}

	// Out-of-range sampler index.
	docBadSampIdx := &gltf.Document{
		Images:    []gltf.Image{{URI: pngDataURI(t, solidImage(2, 2, color.NRGBA{R: 255, G: 255, B: 255, A: 255}))}},
		Textures:  []gltf.Texture{{Sampler: 2, Source: 0}},
		Materials: []gltf.Material{mBadImg},
	}
	if _, err := render3d.UploadGLTFMaterials(nil, dev, defaults, docBadSampIdx, render3d.GLTFMaterialUpload{}); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Errorf("bad sampler index err = %v, want ErrInvalidMaterial", err)
	}

	// Invalid sampler filter mode.
	docBadSampFilter := &gltf.Document{
		Samplers:  []gltf.Sampler{{MagFilter: gltf.FilterMode(42)}},
		Images:    []gltf.Image{{URI: pngDataURI(t, solidImage(2, 2, color.NRGBA{R: 255, G: 255, B: 255, A: 255}))}},
		Textures:  []gltf.Texture{{Sampler: 0, Source: 0}},
		Materials: []gltf.Material{mBadImg},
	}
	if _, err := render3d.UploadGLTFMaterials(nil, dev, defaults, docBadSampFilter, render3d.GLTFMaterialUpload{}); !errors.Is(err, render3d.ErrInvalidSampler) {
		t.Errorf("bad sampler filter err = %v, want ErrInvalidSampler", err)
	}

	// Corrupt image bytes wrap ErrInvalidTexture.
	docCorruptImg := &gltf.Document{
		Images:    []gltf.Image{{URI: "data:image/png;base64,bm90LWEtcG5n"}},
		Textures:  []gltf.Texture{{Sampler: -1, Source: 0}},
		Materials: []gltf.Material{mBadImg},
	}
	if _, err := render3d.UploadGLTFMaterials(nil, dev, defaults, docCorruptImg, render3d.GLTFMaterialUpload{}); !errors.Is(err, render3d.ErrInvalidTexture) {
		t.Errorf("corrupt image err = %v, want ErrInvalidTexture", err)
	}

	// Negative emissiveStrength rejected.
	mBadEmissive := validBase()
	mBadEmissive.Extensions = map[string]json.RawMessage{
		"KHR_materials_emissive_strength": json.RawMessage(`{"emissiveStrength": -1.0}`),
	}
	if _, err := render3d.UploadGLTFMaterials(nil, dev, defaults, &gltf.Document{Materials: []gltf.Material{mBadEmissive}}, render3d.GLTFMaterialUpload{}); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Errorf("negative emissiveStrength err = %v, want ErrInvalidMaterial", err)
	}

	// Invalid metallic factor rejected.
	mBadFactor := validBase()
	mBadFactor.MetallicFactor = 2.0
	if _, err := render3d.UploadGLTFMaterials(nil, dev, defaults, &gltf.Document{Materials: []gltf.Material{mBadFactor}}, render3d.GLTFMaterialUpload{}); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Errorf("invalid metallicFactor err = %v, want ErrInvalidMaterial", err)
	}
}

func TestUploadGLTFMaterialsSamplerSemantics(t *testing.T) {
	pixelURI := pngDataURI(t, solidImage(16, 16, color.NRGBA{R: 90, G: 160, B: 220, A: 255}))
	normalURI := pngDataURI(t, solidImage(4, 4, color.NRGBA{R: 128, G: 128, B: 255, A: 255}))
	// Image 0 is the base color of material 0 through a NEAREST sampler and of material 1 through a mipmapped sampler, so it is uploaded once with mips. Texture 2 has no sampler, and material 1's normal map has a negative scale.
	docJSON := fmt.Sprintf(`{
		"asset": {"version": "2.0"},
		"samplers": [
			{"magFilter": 9728, "minFilter": 9728},
			{"magFilter": 9729, "minFilter": 9987},
			{}
		],
		"images": [{"uri": %q}, {"uri": %q}],
		"textures": [
			{"sampler": 0, "source": 0},
			{"sampler": 1, "source": 0},
			{"source": 1}
		],
		"materials": [
			{"pbrMetallicRoughness": {"baseColorTexture": {"index": 0}}},
			{"pbrMetallicRoughness": {"baseColorTexture": {"index": 1}}, "normalTexture": {"index": 2, "scale": -1}}
		]
	}`, pixelURI, normalURI)
	doc, err := gltf.Load([]byte(docJSON), nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}

	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()
	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	defer func() { _ = defaults.Close() }()

	set, err := render3d.UploadGLTFMaterials(nil, dev, defaults, doc, render3d.GLTFMaterialUpload{MaxAnisotropy: 8})
	if err != nil {
		t.Fatalf("UploadGLTFMaterials: %v", err)
	}
	defer func() { _ = set.Close() }()

	nearest, _ := set.Info(0)
	mipmapped, _ := set.Info(1)
	if nearest.BaseColor.Texture != mipmapped.BaseColor.Texture || nearest.BaseColor.Texture.MipLevels() != 5 {
		t.Fatalf("shared image: textures %p/%p with %d mip levels, want one 16x16 texture with 5 levels", nearest.BaseColor.Texture, mipmapped.BaseColor.Texture, nearest.BaseColor.Texture.MipLevels())
	}
	// The NEAREST sampler must stay on the base level although the texture it samples has mips.
	if got := nearest.BaseColor.Sampler.Options(); got.MaxLOD >= 0.5 || got.MipmapFilter != gpu.FilterNearest {
		t.Errorf("NEAREST sampler options = %+v, want MaxLOD < 0.5 with nearest mip selection", got)
	}
	if got := mipmapped.BaseColor.Sampler.Options(); got.MaxLOD != 32 || got.MaxAnisotropy != 8 {
		t.Errorf("mipmapped sampler options = %+v, want MaxLOD 32 and MaxAnisotropy 8", got)
	}
	// A texture without a sampler uses the glTF default sampler, which gets the same anisotropy as an empty sampler object.
	if got := mipmapped.Normal.Sampler; got == nil || got == defaults.Sampler() || got.Options().MaxAnisotropy != 8 || got.Options().AddressU != gpu.AddressRepeat {
		t.Errorf("sampler-less normal texture sampler = %+v, want a set-owned repeat sampler with MaxAnisotropy 8", got)
	}
	if mipmapped.Material.NormalSampler != mipmapped.Normal.Sampler {
		t.Error("Material.NormalSampler differs from the slot's sampler")
	}
	if mipmapped.Material.NormalScale != -1 {
		t.Errorf("NormalScale = %v, want -1", mipmapped.Material.NormalScale)
	}
	// Samplers 0 and 1 plus the glTF default sampler; sampler 2 is unused.
	if set.SamplerCount() != 3 || set.Sampler(2) != nil {
		t.Errorf("SamplerCount() = %d, Sampler(2) = %v, want 3 and nil", set.SamplerCount(), set.Sampler(2))
	}
}

func TestDamagedHelmetMatchesReference(t *testing.T) {
	doc, helmetMesh := loadDamagedHelmet(t)

	const (
		width  = 192
		height = 192
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = defaults.Close() })

	matSet, err := render3d.UploadGLTFMaterials(nil, dev, defaults, doc, render3d.GLTFMaterialUpload{
		Label: "DamagedHelmet",
	})
	if err != nil {
		t.Fatalf("UploadGLTFMaterials(DamagedHelmet): %v", err)
	}
	t.Cleanup(func() { _ = matSet.Close() })

	// Verify exact glTF material & texture mapping for Khronos DamagedHelmet.
	if matSet.Len() != 1 || matSet.TextureCount() != 5 || matSet.SamplerCount() != 1 {
		t.Fatalf("DamagedHelmet counts = (mats=%d, tex=%d, samp=%d), want (1, 5, 1)",
			matSet.Len(), matSet.TextureCount(), matSet.SamplerCount())
	}
	info, ok := matSet.Info(0)
	if !ok {
		t.Fatal("Info(0) returned false")
	}
	if info.Name != "Material_MR" {
		t.Errorf("material name = %q, want Material_MR", info.Name)
	}
	if !info.BaseColor.Present || !info.BaseColor.Texture.SRGB() {
		t.Error("BaseColor must be present and sRGB")
	}
	if !info.Emissive.Present || !info.Emissive.Texture.SRGB() {
		t.Error("Emissive must be present and sRGB")
	}
	if !info.MetallicRoughness.Present || info.MetallicRoughness.Texture.SRGB() {
		t.Error("MetallicRoughness must be present and linear")
	}
	if !info.Normal.Present || info.Normal.Texture.SRGB() {
		t.Error("Normal must be present and linear")
	}
	if !info.Occlusion.Present || info.Occlusion.Texture.SRGB() {
		t.Error("Occlusion must be present and linear")
	}
	if got := info.Unsupported(); got != nil {
		t.Errorf("DamagedHelmet Unsupported() = %q, want nil", got)
	}
	if info.Material.EmissiveFactor != [3]float32{1, 1, 1} {
		t.Errorf("EmissiveFactor = %v, want [1, 1, 1]", info.Material.EmissiveFactor)
	}
	// Empty glTF sampler {} defaults to repeat + mipmaps so V in [1.0, 2.0] wraps cleanly.
	if s0 := matSet.Sampler(0); s0 == nil || s0.Options().AddressU != gpu.AddressRepeat || s0.Options().AddressV != gpu.AddressRepeat {
		t.Errorf("DamagedHelmet sampler 0 = %+v, want AddressRepeat", s0.Options())
	}
	if info.BaseColor.Texture.MipLevels() <= 1 {
		t.Errorf("DamagedHelmet BaseColor MipLevels = %d, want > 1", info.BaseColor.Texture.MipLevels())
	}

	gpuMesh, err := render3d.UploadMesh(nil, dev, helmetMesh, render3d.MeshUpload{Label: "DamagedHelmet"})
	if err != nil {
		t.Fatalf("UploadMesh(DamagedHelmet): %v", err)
	}
	t.Cleanup(func() { _ = gpuMesh.Close() })

	pipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		AlphaMode:   info.Material.AlphaMode,
		DoubleSided: info.Material.DoubleSided,
	})
	if err != nil {
		t.Fatalf("NewMaterialPipeline: %v", err)
	}

	// Front-3/4 portrait camera showing full helmet silhouette, cyan HUD visor emissive, metallic crown, scratches, and AO crevices.
	bounds := gpuMesh.Bounds()
	center := bounds.Center()
	cam := scene3d.NewPerspectiveCamera(
		center.Add(math3d.Vec3{X: 0.80, Y: 0.12, Z: 3.10}),
		math3d.DegToRad(42),
		float32(width)/float32(height),
		0.1,
		50,
	)
	cam.LookAt(center, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	renderHelmet := func(mat *render3d.GPUMaterial) *image.RGBA {
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			Label:            "damaged-helmet",
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		mat.Bind(pass)
		gpuMesh.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		return cloneRGBA(sr.Image())
	}

	helmetMat := matSet.MaterialForPrimitive(doc.Meshes[0].Primitives[0], -1)
	img := renderHelmet(helmetMat)
	assertReferenceAt(t, filepath.Join(damagedHelmetDir, "reference.png"), img)

	// Analytical sanity checks on the rendered DamagedHelmet:
	// 1. All four background corners stay clear color (entire helmet fits inside frame).
	wantPixel(t, img, 2, 2, meshClear)
	wantPixel(t, img, width-3, 2, meshClear)
	wantPixel(t, img, 2, height-3, meshClear)
	wantPixel(t, img, width-3, height-3, meshClear)

	// 2. Helmet covers a substantial portion of the viewport and renders its glowing cyan visor HUD.
	litPixels := countNonClearPixels(img, meshClear)
	if litPixels < width*height/4 {
		t.Errorf("DamagedHelmet covered only %d pixels, want at least %d", litPixels, width*height/4)
	}
	cyanVisorPixels := 0
	for y := range height {
		for x := range width {
			c := img.RGBAAt(x, y)
			if c.G >= 120 && int(c.G) > int(c.R)+20 && int(c.B) > int(c.R)+10 {
				cyanVisorPixels++
			}
		}
	}
	if cyanVisorPixels < 50 {
		t.Errorf("DamagedHelmet glowing cyan visor HUD pixels = %d, want >= 50", cyanVisorPixels)
	}

	// 3. Disabling emissive visibly removes the glowing HUD reticle from the visor.
	noEmisMat := info.Material
	noEmisMat.EmissiveFactor = [3]float32{0, 0, 0}
	noEmisGPU, err := render3d.NewGPUMaterial(nil, dev, defaults, noEmisMat, render3d.MaterialUpload{Label: "no-emissive"})
	if err != nil {
		t.Fatalf("NewGPUMaterial no-emissive: %v", err)
	}
	defer func() { _ = noEmisGPU.Close() }()
	if emisDiff := countDifferentPixels(img, renderHelmet(noEmisGPU)); emisDiff < 150 {
		t.Errorf("emissive vs non-emissive DamagedHelmet differed by only %d pixels, want >= 150", emisDiff)
	}

	// 4. Textured DamagedHelmet differs markedly from untextured default material on the same mesh.
	untextured := renderHelmet(matSet.DefaultMaterial())
	diff := countDifferentPixels(img, untextured)
	if diff < litPixels/2 {
		t.Errorf("textured vs untextured DamagedHelmet differed by only %d pixels (lit=%d)", diff, litPixels)
	}
}

// TestDamagedHelmetSamplesTexturesAtGLTFUVs checks the DamagedHelmet render against the asset's own base color image rather than against a render: it reads each pixel's UV back from a probe texture and predicts the pixel from the decoded JPEG under glTF's conventions (UV origin at the image's top-left, sRGB color, RGB channel order). A V flip, a linear upload or swapped channels would match the prediction far worse than the real mapping, even if a golden image had been written from that wrong output.
func TestDamagedHelmetSamplesTexturesAtGLTFUVs(t *testing.T) {
	doc, helmetMesh := loadDamagedHelmet(t)

	const size = 160
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = defaults.Close() })
	matSet, err := render3d.UploadGLTFMaterials(nil, dev, defaults, doc, render3d.GLTFMaterialUpload{Label: "DamagedHelmet"})
	if err != nil {
		t.Fatalf("UploadGLTFMaterials: %v", err)
	}
	t.Cleanup(func() { _ = matSet.Close() })
	info, _ := matSet.Info(0)
	albedo, err := doc.DecodeImage(info.BaseColor.ImageIndex)
	if err != nil {
		t.Fatalf("DecodeImage(base color): %v", err)
	}
	const texSize = 256
	if b := albedo.Bounds(); b.Dx() != texSize || b.Dy() != texSize {
		t.Fatalf("base color image is %v, want %dx%d", b, texSize, texSize)
	}

	// The probe texture stores each texel's column in R and row in G, so an emissive-only render of it shows which texel every pixel samples.
	probe := image.NewNRGBA(image.Rect(0, 0, texSize, texSize))
	for y := range texSize {
		for x := range texSize {
			probe.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), A: 255})
		}
	}
	probeTex, err := render3d.UploadTexture(nil, dev, probe, render3d.TextureUpload{Label: "uv-probe"})
	if err != nil {
		t.Fatalf("UploadTexture(probe): %v", err)
	}
	t.Cleanup(func() { _ = probeTex.Close() })
	// Nearest filtering on the base level, so each pixel shows exactly one texel of the uploaded base color texture (which has mips).
	samplerOpts := render3d.SamplerNearestRepeat()
	samplerOpts.MaxLOD = 0.25
	nearest, err := render3d.NewGPUSampler(nil, dev, samplerOpts)
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	t.Cleanup(func() { _ = nearest.Close() })

	// Black metallic base color removes diffuse and ambient light, so a pixel is its emissive texel plus the same small grazing specular term in every pass.
	emissiveOnly := func(tex *render3d.GPUTexture, factor float32) *render3d.GPUMaterial {
		m := render3d.DefaultMaterial()
		m.BaseColorFactor = [4]float32{0, 0, 0, 1}
		m.EmissiveFactor = [3]float32{factor, factor, factor}
		m.EmissiveTexture = tex
		m.EmissiveSampler = nearest
		gm, err := render3d.NewGPUMaterial(nil, dev, defaults, m, render3d.MaterialUpload{Label: "emissive-only"})
		if err != nil {
			t.Fatalf("NewGPUMaterial: %v", err)
		}
		t.Cleanup(func() { _ = gm.Close() })
		return gm
	}

	gpuMesh, err := render3d.UploadMesh(nil, dev, helmetMesh, render3d.MeshUpload{Label: "DamagedHelmet"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = gpuMesh.Close() })
	pipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{DepthFormat: gpu.FormatDepth24Plus})
	if err != nil {
		t.Fatalf("NewMaterialPipeline: %v", err)
	}
	center := gpuMesh.Bounds().Center()
	cam := scene3d.NewPerspectiveCamera(center.Add(math3d.Vec3{X: 0.80, Y: 0.12, Z: 3.10}), math3d.DegToRad(42), 1, 0.1, 50)
	cam.LookAt(center, math3d.Vec3Up())
	camBuf.WriteCamera(cam)
	render := func(mat *render3d.GPUMaterial) *image.RGBA {
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		mat.Bind(pass)
		gpuMesh.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		return cloneRGBA(sr.Image())
	}
	probeImg := render(emissiveOnly(probeTex, 1))
	albedoImg := render(emissiveOnly(info.BaseColor.Texture, 1))
	leakImg := render(emissiveOnly(probeTex, 0))

	// MaterialWGSL's output is pow(c / (1 + c), 1 / 2.2) of the linear color c; untonemap inverts it.
	untonemap := func(v uint8) float64 {
		m := math.Pow(float64(v)/255, 2.2)
		return m / (1 - min(m, 0.999))
	}
	tonemap := func(c float64) float64 { return 255 * math.Pow(c/(1+c), 1/2.2) }
	srgbToLinear := func(v uint8) float64 {
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	texel := func(c float64) int { return min(max(int(math.Round(c*255)), 0), texSize-1) }

	hypotheses := []struct {
		name   string
		sample func(x, y int) [3]float64
	}{
		{"glTF", func(x, y int) [3]float64 {
			c := color.NRGBAModel.Convert(albedo.At(x, y)).(color.NRGBA)
			return [3]float64{srgbToLinear(c.R), srgbToLinear(c.G), srgbToLinear(c.B)}
		}},
		{"V flipped", func(x, y int) [3]float64 {
			c := color.NRGBAModel.Convert(albedo.At(x, texSize-1-y)).(color.NRGBA)
			return [3]float64{srgbToLinear(c.R), srgbToLinear(c.G), srgbToLinear(c.B)}
		}},
		{"uploaded as linear", func(x, y int) [3]float64 {
			c := color.NRGBAModel.Convert(albedo.At(x, y)).(color.NRGBA)
			return [3]float64{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255}
		}},
		{"BGR", func(x, y int) [3]float64 {
			c := color.NRGBAModel.Convert(albedo.At(x, y)).(color.NRGBA)
			return [3]float64{srgbToLinear(c.B), srgbToLinear(c.G), srgbToLinear(c.R)}
		}},
	}
	errSum := make([]float64, len(hypotheses))
	pixels := 0
	for py := 1; py < size-1; py++ {
		for px := 1; px < size-1; px++ {
			// Skip silhouette pixels, whose neighbours can be background.
			covered := true
			for dy := -1; dy <= 1 && covered; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if probeImg.RGBAAt(px+dx, py+dy) == meshClear {
						covered = false
						break
					}
				}
			}
			if !covered {
				continue
			}
			p, a, l := probeImg.RGBAAt(px, py), albedoImg.RGBAAt(px, py), leakImg.RGBAAt(px, py)
			leak := [3]float64{untonemap(l.R), untonemap(l.G), untonemap(l.B)}
			tx := texel(untonemap(p.R) - leak[0])
			ty := texel(untonemap(p.G) - leak[1])
			got := [3]float64{float64(a.R), float64(a.G), float64(a.B)}
			// The 8-bit render quantizes the probe to a few texels near u or v = 1, so each hypothesis is scored by its best texel within 2 of the decoded one.
			for h, hyp := range hypotheses {
				best := math.Inf(1)
				for dy := -2; dy <= 2; dy++ {
					for dx := -2; dx <= 2; dx++ {
						want := hyp.sample((tx+dx+texSize)%texSize, (ty+dy+texSize)%texSize)
						e := 0.0
						for ch := range 3 {
							e += math.Abs(tonemap(want[ch]+leak[ch]) - got[ch])
						}
						best = min(best, e)
					}
				}
				errSum[h] += best
			}
			pixels++
		}
	}
	if pixels < size*size/5 {
		t.Fatalf("only %d interior helmet pixels, want at least %d", pixels, size*size/5)
	}
	meanErr := make([]float64, len(hypotheses))
	for h := range hypotheses {
		meanErr[h] = errSum[h] / float64(pixels*3)
		t.Logf("%s: mean error %.2f of 255 over %d pixels", hypotheses[h].name, meanErr[h], pixels)
	}
	// Measured with the software renderer: 0.3 for glTF's convention, 9 to 30 for the others.
	if meanErr[0] > 1.5 {
		t.Errorf("render differs from the base color image by %.2f of 255 on average, want <= 1.5", meanErr[0])
	}
	for h := 1; h < len(hypotheses); h++ {
		if meanErr[h] < max(4, 5*meanErr[0]) {
			t.Errorf("%s predicts the render about as well as glTF's convention (%.2f vs %.2f): the check cannot tell them apart", hypotheses[h].name, meanErr[h], meanErr[0])
		}
	}
}

func TestDamagedHelmetDrawsInGPUView(t *testing.T) {
	doc, helmetMesh := loadDamagedHelmet(t)

	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	var rendered [2]*image.RGBA
	for idx, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			const size = 96
			var (
				camBuf   *render3d.CameraBuffer
				defaults *render3d.MaterialDefaults
				matSet   *render3d.GLTFMaterialSet
				gpuMesh  *render3d.GPUMesh
				pipe     *gpu.Pipeline
			)
			opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "viewport",
					Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						var err error
						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
						defaults, err = render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{})
						if err != nil {
							t.Fatalf("NewMaterialDefaults: %v", err)
						}
						matSet, err = render3d.UploadGLTFMaterials(cx, dev, defaults, doc, render3d.GLTFMaterialUpload{
							Label: "DamagedHelmet",
						})
						if err != nil {
							t.Fatalf("UploadGLTFMaterials: %v", err)
						}
						gpuMesh, err = render3d.UploadMesh(cx, dev, helmetMesh, render3d.MeshUpload{Label: "DamagedHelmet"})
						if err != nil {
							t.Fatalf("UploadMesh: %v", err)
						}
						pipe, err = render3d.NewMaterialPipeline(cx, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
							DepthFormat: gpu.FormatDepth24Plus,
						})
						if err != nil {
							t.Fatalf("NewMaterialPipeline: %v", err)
						}
						center := gpuMesh.Bounds().Center()
						cam := scene3d.NewPerspectiveCamera(
							center.Add(math3d.Vec3{X: 0.75, Y: 0.18, Z: 2.65}),
							math3d.DegToRad(40),
							1,
							0.1,
							50,
						)
						cam.LookAt(center, math3d.Vec3Up())
						camBuf.WriteCamera(cam)
					},
					OnFrame: func(frame *gpu.Frame) {
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x10141c))
						pass.ClearDepth(1)
						pass.SetPipeline(pipe)
						pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
						matSet.MaterialForPrimitive(doc.Meshes[0].Primitives[0], -1).Bind(pass)
						gpuMesh.Draw(pass)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)
			img, err := app.GPUReadPixels("id:viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			wantPixel(t, img, 2, 2, meshClear)
			if n := countNonClearPixels(img, meshClear); n < size*size/4 {
				t.Errorf("%s: DamagedHelmet covered only %d pixels", p.name, n)
			}
			rendered[idx] = cloneRGBA(img)
		})
	}

	if rendered[0] != nil && rendered[1] != nil {
		if diff := countDifferentPixels(rendered[0], rendered[1]); diff != 0 {
			t.Errorf("full vs webgl2-gles3 DamagedHelmet frames differed by %d pixels, want 0", diff)
		}
	}
}

func TestGLTFMaterialsRetainedSurvivesDeviceLoss(t *testing.T) {
	albedoURI := pngDataURI(t, solidImage(8, 8, color.NRGBA{R: 230, G: 150, B: 45, A: 255}))
	emissiveURI := pngDataURI(t, solidImage(8, 8, color.NRGBA{R: 20, G: 90, B: 220, A: 255}))
	docJSON := fmt.Sprintf(`{
		"asset": {"version": "2.0"},
		"samplers": [{"magFilter": 9729, "minFilter": 9987}],
		"images": [{"uri": %q}, {"uri": %q}],
		"textures": [{"sampler": 0, "source": 0}, {"sampler": 0, "source": 1}],
		"materials": [{
			"name": "RetainedTest",
			"pbrMetallicRoughness": {
				"baseColorTexture": {"index": 0},
				"metallicFactor": 0.2,
				"roughnessFactor": 0.5
			},
			"emissiveFactor": [0.25, 0.25, 0.25],
			"emissiveTexture": {"index": 1}
		}]
	}`, albedoURI, emissiveURI)
	doc, err := gltf.Load([]byte(docJSON), nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}

	const size = 64
	var (
		camBuf   *render3d.CameraBuffer
		defaults *render3d.MaterialDefaults
		matSet   *render3d.GLTFMaterialSet
		mesh     *render3d.GPUMesh
		pipe     *gpu.Pipeline
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, dev)
				if err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				if defaults == nil {
					defaults, err = render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{
						Label:    "gltf-ret",
						Retained: true,
					})
					if err != nil {
						t.Fatalf("NewMaterialDefaults: %v", err)
					}
				}
				if matSet == nil {
					matSet, err = render3d.UploadGLTFMaterials(cx, dev, defaults, doc, render3d.GLTFMaterialUpload{
						Label:    "gltf-ret",
						Retained: true,
					})
					if err != nil {
						t.Fatalf("UploadGLTFMaterials: %v", err)
					}
				}
				if mesh == nil {
					mesh, err = render3d.UploadMesh(cx, dev, worldQuadMesh(math3d.Vec3{}, 1.5, 1.5), render3d.MeshUpload{
						Label:    "quad",
						Retained: true,
					})
					if err != nil {
						t.Fatalf("UploadMesh: %v", err)
					}
				}
				pipe, err = render3d.NewMaterialPipeline(cx, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
					DepthFormat: gpu.FormatDepth24Plus,
				})
				if err != nil {
					t.Fatalf("NewMaterialPipeline: %v", err)
				}
				cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(45), 1, 0.1, 20)
				cam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
				camBuf.WriteCamera(cam)
			},
			OnFrame: func(frame *gpu.Frame) {
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
				matSet.Material(0).Bind(pass)
				mesh.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	app.Advance(16 * time.Millisecond)
	before, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	beforeCopy := cloneRGBA(before)
	if c := beforeCopy.RGBAAt(size/2, size/2); c == meshClear || c.R < 100 {
		t.Fatalf("before loss center pixel = %v, want lit textured quad", c)
	}

	firstSet := matSet
	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
	if firstSet.Valid() {
		t.Fatal("GLTFMaterialSet still valid while device is lost")
	}

	app.RestoreGPUDevice("id:viewport")
	if matSet != firstSet || !firstSet.Valid() {
		t.Fatalf("retained GLTFMaterialSet not restored: same=%v valid=%v", matSet == firstSet, firstSet.Valid())
	}

	app.Advance(16 * time.Millisecond)
	after, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	if diff := countDifferentPixels(beforeCopy, after); diff != 0 {
		t.Errorf("post-restore frame differed from pre-loss frame by %d pixels, want 0", diff)
	}
}
