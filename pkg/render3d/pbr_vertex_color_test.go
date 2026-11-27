package render3d_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
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

func pbrVertexColorAbsDiffU8(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

func pbrVertexColorWantPixel(t *testing.T, img *image.RGBA, x, y int, want color.RGBA, tol uint8) {
	t.Helper()
	got := img.RGBAAt(x, y)
	if pbrVertexColorAbsDiffU8(got.R, want.R) > tol ||
		pbrVertexColorAbsDiffU8(got.G, want.G) > tol ||
		pbrVertexColorAbsDiffU8(got.B, want.B) > tol ||
		pbrVertexColorAbsDiffU8(got.A, want.A) > tol {
		t.Errorf("pixel (%d, %d) = %v, want %v (tol %d)", x, y, got, want, tol)
	}
}

func pbrVertexColorVec4ToRGBA(v math3d.Vec4) color.RGBA {
	toByte := func(f float32) uint8 {
		if f <= 0 {
			return 0
		}
		if f >= 1 {
			return 255
		}
		return uint8(math.Round(float64(f) * 255))
	}
	return color.RGBA{R: toByte(v.X), G: toByte(v.Y), B: toByte(v.Z), A: toByte(v.W)}
}

func pbrVertexColorBlendOver(src math3d.Vec4, dst color.RGBA) color.RGBA {
	a := src.W
	if a < 0 {
		a = 0
	} else if a > 1 {
		a = 1
	}
	inv := 1 - a
	dr := float32(dst.R) / 255
	dg := float32(dst.G) / 255
	db := float32(dst.B) / 255
	da := float32(dst.A) / 255
	return pbrVertexColorVec4ToRGBA(math3d.Vec4{
		X: src.X*a + dr*inv,
		Y: src.Y*a + dg*inv,
		Z: src.Z*a + db*inv,
		W: a + da*inv,
	})
}

func pbrVertexColorQuad(center math3d.Vec3, hx, hy float32, colors []math3d.Vec4) render3d.ColoredMesh {
	n := math3d.Vec3{Z: 1}
	return render3d.ColoredMesh{
		Positions: []math3d.Vec3{
			{X: center.X - hx, Y: center.Y - hy, Z: center.Z},
			{X: center.X + hx, Y: center.Y - hy, Z: center.Z},
			{X: center.X + hx, Y: center.Y + hy, Z: center.Z},
			{X: center.X - hx, Y: center.Y + hy, Z: center.Z},
		},
		Normals: []math3d.Vec3{n, n, n, n},
		UVs: []math3d.Vec2{
			{X: 0, Y: 1},
			{X: 1, Y: 1},
			{X: 1, Y: 0},
			{X: 0, Y: 0},
		},
		Colors:  colors,
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

func pbrVertexColorSolidNRGBA(r, g, b, a uint8) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}
	return img
}

func pbrVertexColorSolidCubemap(c color.RGBA, size int) *render3d.Cubemap {
	var cm render3d.Cubemap
	for face := range 6 {
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		for y := range size {
			for x := range size {
				img.SetRGBA(x, y, c)
			}
		}
		cm.Faces[face] = img
	}
	return &cm
}

func pbrVertexColorSubmitAndImage(t *testing.T, sr *gpu.SoftwareRenderer, dev *gpu.Device, enc *gpu.Encoder) *image.RGBA {
	t.Helper()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	return sr.Image()
}

func pbrVertexColorLinearFromSRGBPixel(c color.RGBA) [4]float32 {
	return [4]float32{
		render3d.SRGBToLinearChannel(float32(c.R) / 255),
		render3d.SRGBToLinearChannel(float32(c.G) / 255),
		render3d.SRGBToLinearChannel(float32(c.B) / 255),
		float32(c.A) / 255,
	}
}

func pbrVertexColorBakeNodeTransform(cm *render3d.ColoredMesh, world math3d.Mat4) {
	nm, ok := math3d.NormalMatrix(world)
	for i, p := range cm.Positions {
		cm.Positions[i] = world.TransformPoint(p)
	}
	if ok {
		for i, n := range cm.Normals {
			cm.Normals[i] = math3d.Vec3{
				X: nm[0]*n.X + nm[3]*n.Y + nm[6]*n.Z,
				Y: nm[1]*n.X + nm[4]*n.Y + nm[7]*n.Z,
				Z: nm[2]*n.X + nm[5]*n.Y + nm[8]*n.Z,
			}.Norm()
		}
	}
}

func pbrVertexColorBuildGLTFBytes(t *testing.T, baseColorFactor [4]float32, metallic, roughness float32, alphaMode string, alphaCutoff *float32, colors []math3d.Vec4, colorVec3 bool, texImg image.Image) []byte {
	t.Helper()
	quad := pbrVertexColorQuad(math3d.Vec3{}, 0.8, 0.8, colors)

	var bin bytes.Buffer
	appendPad4 := func() {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
	}

	posOff := bin.Len()
	for _, p := range quad.Positions {
		_ = binary.Write(&bin, binary.LittleEndian, p.X)
		_ = binary.Write(&bin, binary.LittleEndian, p.Y)
		_ = binary.Write(&bin, binary.LittleEndian, p.Z)
	}
	posLen := bin.Len() - posOff

	normOff := bin.Len()
	for _, n := range quad.Normals {
		_ = binary.Write(&bin, binary.LittleEndian, n.X)
		_ = binary.Write(&bin, binary.LittleEndian, n.Y)
		_ = binary.Write(&bin, binary.LittleEndian, n.Z)
	}
	normLen := bin.Len() - normOff

	uvOff := bin.Len()
	for _, uv := range quad.UVs {
		_ = binary.Write(&bin, binary.LittleEndian, uv.X)
		_ = binary.Write(&bin, binary.LittleEndian, uv.Y)
	}
	uvLen := bin.Len() - uvOff

	idxOff := bin.Len()
	for _, idx := range quad.Indices {
		_ = binary.Write(&bin, binary.LittleEndian, uint16(idx))
	}
	idxLen := bin.Len() - idxOff
	appendPad4()

	colOff := bin.Len()
	colType := "VEC4"
	if colorVec3 {
		colType = "VEC3"
		for _, c := range colors {
			_ = binary.Write(&bin, binary.LittleEndian, c.X)
			_ = binary.Write(&bin, binary.LittleEndian, c.Y)
			_ = binary.Write(&bin, binary.LittleEndian, c.Z)
		}
	} else {
		for _, c := range colors {
			_ = binary.Write(&bin, binary.LittleEndian, c.X)
			_ = binary.Write(&bin, binary.LittleEndian, c.Y)
			_ = binary.Write(&bin, binary.LittleEndian, c.Z)
			_ = binary.Write(&bin, binary.LittleEndian, c.W)
		}
	}
	colLen := bin.Len() - colOff

	bufferViews := []map[string]any{
		{"buffer": 0, "byteOffset": posOff, "byteLength": posLen, "target": 34962},
		{"buffer": 0, "byteOffset": normOff, "byteLength": normLen, "target": 34962},
		{"buffer": 0, "byteOffset": uvOff, "byteLength": uvLen, "target": 34962},
		{"buffer": 0, "byteOffset": idxOff, "byteLength": idxLen, "target": 34963},
	}
	accessors := []map[string]any{
		{"bufferView": 0, "componentType": 5126, "count": len(quad.Positions), "type": "VEC3", "min": []float32{-0.8, -0.8, 0}, "max": []float32{0.8, 0.8, 0}},
		{"bufferView": 1, "componentType": 5126, "count": len(quad.Normals), "type": "VEC3"},
		{"bufferView": 2, "componentType": 5126, "count": len(quad.UVs), "type": "VEC2"},
		{"bufferView": 3, "componentType": 5123, "count": len(quad.Indices), "type": "SCALAR"},
	}
	attrs := map[string]any{
		"POSITION":   0,
		"NORMAL":     1,
		"TEXCOORD_0": 2,
	}
	if len(colors) > 0 {
		bvIdx := len(bufferViews)
		accIdx := len(accessors)
		bufferViews = append(bufferViews, map[string]any{"buffer": 0, "byteOffset": colOff, "byteLength": colLen, "target": 34962})
		accessors = append(accessors, map[string]any{"bufferView": bvIdx, "componentType": 5126, "count": len(colors), "type": colType})
		attrs["COLOR_0"] = accIdx
	}

	pbr := map[string]any{
		"baseColorFactor": baseColorFactor[:],
		"metallicFactor":  metallic,
		"roughnessFactor": roughness,
	}
	mat := map[string]any{
		"name":                 "pbr_vc_mat",
		"pbrMetallicRoughness": pbr,
	}
	if alphaMode != "" {
		mat["alphaMode"] = alphaMode
	}
	if alphaCutoff != nil {
		mat["alphaCutoff"] = *alphaCutoff
	}

	doc := map[string]any{
		"asset":       map[string]any{"version": "2.0"},
		"buffers":     []map[string]any{{"byteLength": bin.Len(), "uri": "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(bin.Bytes())}},
		"bufferViews": bufferViews,
		"accessors":   accessors,
		"materials":   []map[string]any{mat},
		"meshes": []map[string]any{{
			"name":       "pbr_vc_mesh",
			"primitives": []map[string]any{{"attributes": attrs, "indices": 3, "material": 0}},
		}},
		"nodes":  []map[string]any{{"mesh": 0}},
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"scene":  0,
	}
	if texImg != nil {
		var pngBuf bytes.Buffer
		if err := png.Encode(&pngBuf, texImg); err != nil {
			t.Fatalf("png.Encode: %v", err)
		}
		pbr["baseColorTexture"] = map[string]any{"index": 0}
		doc["images"] = []map[string]any{{"uri": "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBuf.Bytes())}}
		doc["samplers"] = []map[string]any{{"magFilter": 9728, "minFilter": 9728}}
		doc["textures"] = []map[string]any{{"source": 0, "sampler": 0}}
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

func TestPBRVertexColorEvaluateMaterialCPU(t *testing.T) {
	base := render3d.DefaultMaterial()
	base.BaseColorFactor = [4]float32{0.8, 0.9, 1.0, 0.9}
	base.MetallicFactor = 0.2
	base.RoughnessFactor = 0.6

	// Omitting HasVertexColor defaults to white [1, 1, 1, 1] even when VertexColor is zero or set.
	refDefault := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:    base,
		Normal:      math3d.Vec3{Z: 1},
		ViewDir:     math3d.Vec3{Z: 1},
		FrontFacing: true,
	})
	refIgnored := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:       base,
		VertexColor:    [4]float32{0.1, 0.2, 0.3, 0.1},
		HasVertexColor: false,
		Normal:         math3d.Vec3{Z: 1},
		ViewDir:        math3d.Vec3{Z: 1},
		FrontFacing:    true,
	})
	refExplicitWhite := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:       base,
		VertexColor:    [4]float32{1, 1, 1, 1},
		HasVertexColor: true,
		Normal:         math3d.Vec3{Z: 1},
		ViewDir:        math3d.Vec3{Z: 1},
		FrontFacing:    true,
	})
	if refDefault != refIgnored || refDefault != refExplicitWhite {
		t.Fatalf("white/unset VertexColor mismatch: default=%+v ignored=%+v white=%+v", refDefault, refIgnored, refExplicitWhite)
	}

	// Multiplying by vertex color in linear space equals folding it into BaseColorFactor.
	vc := [4]float32{0.25, 0.5, 0.75, 0.6}
	gotVC := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:           base,
		BaseColorSample:    [4]float32{0.9, 0.8, 0.7, 0.9},
		HasBaseColorSample: true,
		VertexColor:        vc,
		HasVertexColor:     true,
		Normal:             math3d.Vec3{Z: 1},
		ViewDir:            math3d.Vec3{Z: 1},
		FrontFacing:        true,
	})
	folded := base
	for i := range 4 {
		folded.BaseColorFactor[i] *= vc[i]
	}
	wantFolded := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:           folded,
		BaseColorSample:    [4]float32{0.9, 0.8, 0.7, 0.9},
		HasBaseColorSample: true,
		Normal:             math3d.Vec3{Z: 1},
		ViewDir:            math3d.Vec3{Z: 1},
		FrontFacing:        true,
	})
	if gotVC != wantFolded {
		t.Fatalf("EvaluateMaterial with VertexColor = %+v, want folded %+v", gotVC, wantFolded)
	}

	// Explicit black vertex color [0, 0, 0, 1] is respected when HasVertexColor is true.
	gotBlack := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:       base,
		VertexColor:    [4]float32{0, 0, 0, 1},
		HasVertexColor: true,
		Normal:         math3d.Vec3{Z: 1},
		ViewDir:        math3d.Vec3{Z: 1},
		FrontFacing:    true,
	})
	if gotBlack.Linear.X >= refDefault.Linear.X || gotBlack.Linear.Y >= refDefault.Linear.Y || gotBlack.Linear.Z >= refDefault.Linear.Z {
		t.Fatalf("expected black vertex color to darken linear output: black=%v default=%v", gotBlack.Linear, refDefault.Linear)
	}

	// AlphaMask discard uses BaseColorFactor.a * BaseColorSample.a * VertexColor.a.
	maskMat := render3d.DefaultMaterial()
	maskMat.AlphaMode = render3d.AlphaMask
	maskMat.AlphaCutoff = 0.5
	discarded := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:       maskMat,
		VertexColor:    [4]float32{1, 1, 1, 0.4},
		HasVertexColor: true,
		Normal:         math3d.Vec3{Z: 1},
		ViewDir:        math3d.Vec3{Z: 1},
		FrontFacing:    true,
	})
	if !discarded.Discarded {
		t.Fatalf("expected AlphaMask fragment with COLOR_0.a=0.4 < 0.5 to discard")
	}
	kept := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:       maskMat,
		VertexColor:    [4]float32{1, 1, 1, 0.6},
		HasVertexColor: true,
		Normal:         math3d.Vec3{Z: 1},
		ViewDir:        math3d.Vec3{Z: 1},
		FrontFacing:    true,
	})
	if kept.Discarded || kept.SRGB.W != 1 {
		t.Fatalf("expected AlphaMask fragment with COLOR_0.a=0.6 >= 0.5 to survive with alpha 1, got %+v", kept)
	}

	// AlphaBlend outputs BaseColorFactor.a * BaseColorSample.a * VertexColor.a.
	blendMat := render3d.DefaultMaterial()
	blendMat.BaseColorFactor = [4]float32{1, 1, 1, 0.8}
	blendMat.AlphaMode = render3d.AlphaBlend
	blended := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:           blendMat,
		BaseColorSample:    [4]float32{1, 1, 1, 0.5},
		HasBaseColorSample: true,
		VertexColor:        [4]float32{0.5, 0.25, 1, 0.5},
		HasVertexColor:     true,
		Normal:             math3d.Vec3{Z: 1},
		ViewDir:            math3d.Vec3{Z: 1},
		FrontFacing:        true,
	})
	if math.Abs(float64(blended.SRGB.W-0.2)) > 1e-5 || math.Abs(float64(blended.Linear.W-0.2)) > 1e-5 {
		t.Fatalf("AlphaBlend output alpha = (linear %v, srgb %v), want 0.2", blended.Linear.W, blended.SRGB.W)
	}

	// PBRMaterial.WithVertexColor multiplies RGB and Alpha (zero alpha defaults to 1).
	pbrMat := render3d.PBRMaterial{
		BaseColor: math3d.Vec3{X: 0.8, Y: 0.6, Z: 0.4},
		Metallic:  0.3,
		Roughness: 0.5,
	}
	tinted := pbrMat.WithVertexColor(math3d.Vec4{X: 0.5, Y: 0.25, Z: 1, W: 0.75})
	if math.Abs(float64(tinted.BaseColor.X-0.4)) > 1e-6 ||
		math.Abs(float64(tinted.BaseColor.Y-0.15)) > 1e-6 ||
		math.Abs(float64(tinted.BaseColor.Z-0.4)) > 1e-6 ||
		math.Abs(float64(tinted.Alpha-0.75)) > 1e-6 {
		t.Fatalf("WithVertexColor = %+v", tinted)
	}
}

func TestPBRVertexColorPipelineValidationAndVariants(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	if _, err := gpu.CompileWGSL(render3d.MaterialColoredWGSL); err != nil {
		t.Fatalf("CompileWGSL(MaterialColoredWGSL): %v", err)
	}
	if _, err := gpu.CompileWGSL(render3d.LitPBRColoredWGSL); err != nil {
		t.Fatalf("CompileWGSL(LitPBRColoredWGSL): %v", err)
	}

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defs, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	lightBuf, err := render3d.NewLightBuffer(nil, dev, render3d.LightRig{}, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	drawBuf, err := render3d.NewPBRDrawBuffer(nil, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), render3d.PBRMaterial{}), false)
	if err != nil {
		t.Fatalf("NewPBRDrawBuffer: %v", err)
	}
	skinPal, err := render3d.NewGPUSkinPalette(nil, dev, render3d.SkinPaletteOptions{})
	if err != nil {
		t.Fatalf("NewGPUSkinPalette: %v", err)
	}
	fogBuf, err := render3d.NewGPUFogBuffer(nil, dev, render3d.Fog{Mode: render3d.FogLinear, Start: 1, End: 10}, render3d.FogBufferOptions{})
	if err != nil {
		t.Fatalf("NewGPUFogBuffer: %v", err)
	}
	env := pbrVertexColorSolidCubemap(color.RGBA{R: 128, G: 128, B: 128, A: 255}, 4)
	sh, err := render3d.ComputeIrradianceSH(env)
	if err != nil {
		t.Fatalf("ComputeIrradianceSH: %v", err)
	}
	pref, err := render3d.PrefilterEnvironment(env, render3d.PrefilterOptions{BaseSize: 4, MipLevels: 2, SampleCount: 4})
	if err != nil {
		t.Fatalf("PrefilterEnvironment: %v", err)
	}
	prefGPU, err := render3d.UploadCubemap(nil, dev, pref, render3d.CubemapUpload{})
	if err != nil {
		t.Fatalf("UploadCubemap: %v", err)
	}
	iblBuf, err := render3d.NewGPUIBLBuffer(nil, dev, prefGPU, render3d.NewIBLUniform(sh, prefGPU, 1.0), render3d.IBLBufferOptions{})
	if err != nil {
		t.Fatalf("NewGPUIBLBuffer: %v", err)
	}
	dirShadowOpts := render3d.DefaultShadowMapOptions()
	dirShadowOpts.Resolution = 16
	dirShadow, err := render3d.NewDirectionalShadowMap(nil, dev, dirShadowOpts)
	if err != nil {
		t.Fatalf("NewDirectionalShadowMap: %v", err)
	}
	spotShadow, err := render3d.NewSpotShadowMap(nil, dev, render3d.SpotShadowConfig{Resolution: 16})
	if err != nil {
		t.Fatalf("NewSpotShadowMap: %v", err)
	}
	pointShadow, err := render3d.NewPointShadowMap(nil, dev, render3d.PointShadowConfig{FaceResolution: 16})
	if err != nil {
		t.Fatalf("NewPointShadowMap: %v", err)
	}
	csm, err := render3d.NewCascadedShadowMap(nil, dev, render3d.CSMConfig{CascadeCount: 2, Resolution: 16})
	if err != nil {
		t.Fatalf("NewCascadedShadowMap: %v", err)
	}

	// VertexColors + SkinLayout is rejected on both NewMaterialPipeline and NewLitPBRPipelineWithOptions.
	if _, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defs.Layout(), render3d.MaterialPipelineOptions{
		VertexColors: true,
		SkinLayout:   skinPal.Layout(),
	}); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Fatalf("NewMaterialPipeline(VertexColors+SkinLayout) err = %v, want ErrInvalidDescriptor", err)
	}
	if _, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{
		VertexColors: true,
		SkinLayout:   skinPal.Layout(),
	}); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Fatalf("NewLitPBRPipelineWithOptions(VertexColors+SkinLayout) err = %v, want ErrInvalidDescriptor", err)
	}

	// All non-skinned MaterialPipeline variants with VertexColors compile and build pipelines.
	for _, tc := range []struct {
		name string
		opts render3d.MaterialPipelineOptions
	}{
		{"opaque", render3d.MaterialPipelineOptions{VertexColors: true, AlphaMode: render3d.AlphaOpaque}},
		{"mask", render3d.MaterialPipelineOptions{VertexColors: true, AlphaMode: render3d.AlphaMask, DoubleSided: true}},
		{"blend", render3d.MaterialPipelineOptions{VertexColors: true, AlphaMode: render3d.AlphaBlend}},
		{"linear", render3d.MaterialPipelineOptions{VertexColors: true, LinearOutput: true}},
		{"fog", render3d.MaterialPipelineOptions{VertexColors: true, FogLayout: fogBuf.Layout()}},
		{"fog-linear", render3d.MaterialPipelineOptions{VertexColors: true, FogLayout: fogBuf.Layout(), LinearOutput: true}},
	} {
		pipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defs.Layout(), tc.opts)
		if err != nil {
			t.Fatalf("NewMaterialPipeline(%s): %v", tc.name, err)
		}
		_ = pipe.Close()
	}

	// All non-skinned LitPBRPipelineOptions variants with VertexColors compile and build pipelines.
	for _, tc := range []struct {
		name string
		opts render3d.LitPBRPipelineOptions
	}{
		{"plain", render3d.LitPBRPipelineOptions{VertexColors: true}},
		{"plain-linear", render3d.LitPBRPipelineOptions{VertexColors: true, LinearOutput: true}},
		{"fog", render3d.LitPBRPipelineOptions{VertexColors: true, FogLayout: fogBuf.Layout()}},
		{"fog-linear", render3d.LitPBRPipelineOptions{VertexColors: true, FogLayout: fogBuf.Layout(), LinearOutput: true}},
		{"ibl", render3d.LitPBRPipelineOptions{VertexColors: true, IBLLayout: iblBuf.Layout()}},
		{"ibl-linear", render3d.LitPBRPipelineOptions{VertexColors: true, IBLLayout: iblBuf.Layout(), LinearOutput: true}},
		{"shadow", render3d.LitPBRPipelineOptions{VertexColors: true, ShadowLayout: dirShadow.Layout()}},
		{"spot-shadow", render3d.LitPBRPipelineOptions{VertexColors: true, SpotShadowLayout: spotShadow.Layout()}},
		{"point-shadow", render3d.LitPBRPipelineOptions{VertexColors: true, PointShadowLayout: pointShadow.Layout()}},
		{"csm", render3d.LitPBRPipelineOptions{VertexColors: true, CSMLayout: csm.Layout()}},
	} {
		pipe, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), tc.opts)
		if err != nil {
			t.Fatalf("NewLitPBRPipelineWithOptions(%s): %v", tc.name, err)
		}
		_ = pipe.Close()
	}
}

func TestPBRVertexColorMaterialPipelineSoftwareParity(t *testing.T) {
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	defs, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 4}, 2, 1, 0.1, 10)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	viewDir := math3d.Vec3{Z: 1}
	clearCol := color.RGBA{R: 24, G: 36, B: 52, A: 255}
	clearCore := core.RGB(0x182434)

	renderQuad := func(pipe *gpu.Pipeline, gpuMat *render3d.GPUMaterial, gpuMesh *render3d.GPUColoredMesh) *image.RGBA {
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: clearCore,
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreDiscard,
				ClearDepth:   1,
			},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		gpuMat.Bind(pass)
		gpuMesh.Draw(pass)
		pass.End()
		return pbrVertexColorSubmitAndImage(t, sr, dev, enc)
	}

	// 1. Opaque textured metallic-roughness material multiplied by linear RGB vertex color.
	texColor := color.RGBA{R: 220, G: 190, B: 160, A: 255}
	baseTex, err := render3d.UploadTexture(nil, dev, pbrVertexColorSolidNRGBA(texColor.R, texColor.G, texColor.B, texColor.A), render3d.TextureUpload{SRGB: true})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}
	mat := render3d.DefaultMaterial()
	mat.BaseColorFactor = [4]float32{0.9, 0.95, 1.0, 1.0}
	mat.BaseColorTexture = baseTex
	mat.MetallicFactor = 0.25
	mat.RoughnessFactor = 0.55
	gpuMat, err := render3d.NewGPUMaterial(nil, dev, defs, mat, render3d.MaterialUpload{})
	if err != nil {
		t.Fatalf("NewGPUMaterial: %v", err)
	}
	pipeOpaque, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defs.Layout(), render3d.MaterialPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		AlphaMode:    render3d.AlphaOpaque,
		VertexColors: true,
	})
	if err != nil {
		t.Fatalf("NewMaterialPipeline(opaque): %v", err)
	}
	vc := math3d.Vec4{X: 0.85, Y: 0.2, Z: 0.45, W: 1.0}
	quadMesh, err := render3d.UploadColoredMesh(nil, dev, pbrVertexColorQuad(math3d.Vec3Zero(), 0.8, 0.8, []math3d.Vec4{vc, vc, vc, vc}), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadColoredMesh: %v", err)
	}
	img := renderQuad(pipeOpaque, gpuMat, quadMesh)
	wantOpaque := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:           mat,
		BaseColorSample:    pbrVertexColorLinearFromSRGBPixel(texColor),
		HasBaseColorSample: true,
		VertexColor:        [4]float32{vc.X, vc.Y, vc.Z, vc.W},
		HasVertexColor:     true,
		Normal:             math3d.Vec3{Z: 1},
		ViewDir:            viewDir,
		FrontFacing:        true,
	})
	pbrVertexColorWantPixel(t, img, size/2, size/2, pbrVertexColorVec4ToRGBA(wantOpaque.SRGB), 2)

	// 2. AlphaMask driven by COLOR_0.a (below cutoff discards to clear color; above cutoff survives).
	maskMat := render3d.DefaultMaterial()
	maskMat.BaseColorFactor = [4]float32{1, 1, 1, 1}
	maskMat.MetallicFactor = 0
	maskMat.RoughnessFactor = 0.8
	maskMat.AlphaMode = render3d.AlphaMask
	maskMat.AlphaCutoff = 0.5
	gpuMaskMat, err := render3d.NewGPUMaterial(nil, dev, defs, maskMat, render3d.MaterialUpload{})
	if err != nil {
		t.Fatalf("NewGPUMaterial(mask): %v", err)
	}
	pipeMask, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defs.Layout(), render3d.MaterialPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		AlphaMode:    render3d.AlphaMask,
		VertexColors: true,
	})
	if err != nil {
		t.Fatalf("NewMaterialPipeline(mask): %v", err)
	}
	lowA := math3d.Vec4{X: 0.9, Y: 0.3, Z: 0.2, W: 0.25}
	lowMesh, err := render3d.UploadColoredMesh(nil, dev, pbrVertexColorQuad(math3d.Vec3Zero(), 0.8, 0.8, []math3d.Vec4{lowA, lowA, lowA, lowA}), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadColoredMesh(lowA): %v", err)
	}
	imgLow := renderQuad(pipeMask, gpuMaskMat, lowMesh)
	pbrVertexColorWantPixel(t, imgLow, size/2, size/2, clearCol, 0)

	highA := math3d.Vec4{X: 0.9, Y: 0.3, Z: 0.2, W: 0.75}
	highMesh, err := render3d.UploadColoredMesh(nil, dev, pbrVertexColorQuad(math3d.Vec3Zero(), 0.8, 0.8, []math3d.Vec4{highA, highA, highA, highA}), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadColoredMesh(highA): %v", err)
	}
	imgHigh := renderQuad(pipeMask, gpuMaskMat, highMesh)
	wantHigh := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:       maskMat,
		VertexColor:    [4]float32{highA.X, highA.Y, highA.Z, highA.W},
		HasVertexColor: true,
		Normal:         math3d.Vec3{Z: 1},
		ViewDir:        viewDir,
		FrontFacing:    true,
	})
	pbrVertexColorWantPixel(t, imgHigh, size/2, size/2, pbrVertexColorVec4ToRGBA(wantHigh.SRGB), 2)

	// 3. AlphaBlend driven by COLOR_0.a composites over clearCol matching EvaluateMaterial.
	blendMat := maskMat
	blendMat.AlphaMode = render3d.AlphaBlend
	gpuBlendMat, err := render3d.NewGPUMaterial(nil, dev, defs, blendMat, render3d.MaterialUpload{})
	if err != nil {
		t.Fatalf("NewGPUMaterial(blend): %v", err)
	}
	pipeBlend, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defs.Layout(), render3d.MaterialPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		AlphaMode:    render3d.AlphaBlend,
		VertexColors: true,
	})
	if err != nil {
		t.Fatalf("NewMaterialPipeline(blend): %v", err)
	}
	halfA := math3d.Vec4{X: 0.2, Y: 0.85, Z: 0.4, W: 0.5}
	blendMesh, err := render3d.UploadColoredMesh(nil, dev, pbrVertexColorQuad(math3d.Vec3Zero(), 0.8, 0.8, []math3d.Vec4{halfA, halfA, halfA, halfA}), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadColoredMesh(blend): %v", err)
	}
	imgBlend := renderQuad(pipeBlend, gpuBlendMat, blendMesh)
	wantBlend := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:       blendMat,
		VertexColor:    [4]float32{halfA.X, halfA.Y, halfA.Z, halfA.W},
		HasVertexColor: true,
		Normal:         math3d.Vec3{Z: 1},
		ViewDir:        viewDir,
		FrontFacing:    true,
	})
	pbrVertexColorWantPixel(t, imgBlend, size/2, size/2, pbrVertexColorBlendOver(wantBlend.SRGB, clearCol), 3)

	// 4. Uncolored mesh equivalence: GPUMesh on VertexColors:false matches white GPUColoredMesh on VertexColors:true identically.
	pipePlain, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defs.Layout(), render3d.MaterialPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		AlphaMode:    render3d.AlphaOpaque,
		VertexColors: false,
	})
	if err != nil {
		t.Fatalf("NewMaterialPipeline(plain): %v", err)
	}
	uncoloredCM := pbrVertexColorQuad(math3d.Vec3Zero(), 0.8, 0.8, nil)
	plainGPU, err := render3d.UploadMesh(nil, dev, uncoloredCM.Mesh(), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	whiteColoredGPU, err := render3d.UploadColoredMesh(nil, dev, uncoloredCM, render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadColoredMesh(uncolored): %v", err)
	}
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: clearCore}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pipePlain)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	gpuMat.Bind(pass)
	plainGPU.Draw(pass)
	pass.End()
	imgPlain := pbrVertexColorSubmitAndImage(t, sr, dev, enc)
	imgWhiteColored := renderQuad(pipeOpaque, gpuMat, whiteColoredGPU)
	if !bytes.Equal(imgPlain.Pix, imgWhiteColored.Pix) {
		t.Fatalf("uncolored GPUMesh (VertexColors:false) and default-white GPUColoredMesh (VertexColors:true) rendered differently")
	}
}

func TestPBRVertexColorLitPBRPipelineSoftwareParity(t *testing.T) {
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 4}, 2, 1, 0.1, 10)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.12, Y: 0.14, Z: 0.16}, Intensity: 1},
		Directional: []render3d.DirectionalLight{{
			Direction: math3d.Vec3{X: -0.3, Y: -0.5, Z: -0.8},
			Color:     math3d.Vec3{X: 1.0, Y: 0.96, Z: 0.9},
			Intensity: 2.2,
		}},
	}
	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	mat := render3d.PBRMaterial{
		BaseColor: math3d.Vec3{X: 0.9, Y: 0.85, Z: 0.8},
		Alpha:     1.0,
		Metallic:  0.15,
		Roughness: 0.55,
		Emissive:  math3d.Vec3{X: 0.02, Y: 0.01, Z: 0.0},
	}
	drawBuf, err := render3d.NewPBRDrawBuffer(nil, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), mat), false)
	if err != nil {
		t.Fatalf("NewPBRDrawBuffer: %v", err)
	}

	pipeColored, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		VertexColors: true,
	})
	if err != nil {
		t.Fatalf("NewLitPBRPipelineWithOptions(colored): %v", err)
	}
	pipePlain, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		VertexColors: false,
	})
	if err != nil {
		t.Fatalf("NewLitPBRPipelineWithOptions(plain): %v", err)
	}

	vc := math3d.Vec4{X: 0.2, Y: 0.75, Z: 0.5, W: 1.0}
	coloredMesh, err := render3d.UploadColoredMesh(nil, dev, pbrVertexColorQuad(math3d.Vec3Zero(), 0.8, 0.8, []math3d.Vec4{vc, vc, vc, vc}), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadColoredMesh: %v", err)
	}

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x080c12)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pipeColored)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	lightBuf.Bind(pass)
	pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
	coloredMesh.Draw(pass)
	pass.End()

	img := pbrVertexColorSubmitAndImage(t, sr, dev, enc)
	tintedMat := mat.WithVertexColor(vc)
	lin := render3d.ShadePoint(math3d.Vec3Zero(), math3d.Vec3{Z: 1}, math3d.Vec3{Z: 1}, tintedMat, rig)
	srgb := render3d.LinearToSRGB(lin)
	pbrVertexColorWantPixel(t, img, size/2, size/2, pbrVertexColorVec4ToRGBA(math3d.Vec4{X: srgb.X, Y: srgb.Y, Z: srgb.Z, W: tintedMat.Alpha}), 2)

	// Uncolored GPUMesh (VertexColors:false) matches default-white GPUColoredMesh (VertexColors:true).
	uncoloredCM := pbrVertexColorQuad(math3d.Vec3Zero(), 0.8, 0.8, nil)
	plainGPU, err := render3d.UploadMesh(nil, dev, uncoloredCM.Mesh(), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	whiteGPU, err := render3d.UploadColoredMesh(nil, dev, uncoloredCM, render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadColoredMesh(white): %v", err)
	}
	drawPass := func(p *gpu.Pipeline, drawFn func(*gpu.RenderPass)) *image.RGBA {
		e := dev.BeginEncoder()
		rp := e.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x080c12)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		rp.SetPipeline(p)
		rp.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		lightBuf.Bind(rp)
		rp.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
		drawFn(rp)
		rp.End()
		return pbrVertexColorSubmitAndImage(t, sr, dev, e)
	}
	imgPlain := drawPass(pipePlain, plainGPU.Draw)
	imgWhite := drawPass(pipeColored, whiteGPU.Draw)
	if !bytes.Equal(imgPlain.Pix, imgWhite.Pix) {
		t.Fatalf("LitPBR uncolored GPUMesh (VertexColors:false) and default-white GPUColoredMesh (VertexColors:true) rendered differently")
	}

	// IBL + VertexColors multiplies base color by in.color in both direct lighting and evaluate_ibl.
	env := pbrVertexColorSolidCubemap(color.RGBA{R: 180, G: 180, B: 180, A: 255}, 8)
	sh, err := render3d.ComputeIrradianceSH(env)
	if err != nil {
		t.Fatalf("ComputeIrradianceSH: %v", err)
	}
	pref, err := render3d.PrefilterEnvironment(env, render3d.PrefilterOptions{BaseSize: 8, MipLevels: 3, SampleCount: 16})
	if err != nil {
		t.Fatalf("PrefilterEnvironment: %v", err)
	}
	prefGPU, err := render3d.UploadCubemap(nil, dev, pref, render3d.CubemapUpload{})
	if err != nil {
		t.Fatalf("UploadCubemap: %v", err)
	}
	iblBuf, err := render3d.NewGPUIBLBuffer(nil, dev, prefGPU, render3d.NewIBLUniform(sh, prefGPU, 1.0), render3d.IBLBufferOptions{})
	if err != nil {
		t.Fatalf("NewGPUIBLBuffer: %v", err)
	}
	pipeIBL, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		IBLLayout:    iblBuf.Layout(),
		VertexColors: true,
	})
	if err != nil {
		t.Fatalf("NewLitPBRPipelineWithOptions(ibl+colored): %v", err)
	}
	e := dev.BeginEncoder()
	rp := e.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x080c12)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	rp.SetPipeline(pipeIBL)
	rp.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	lightBuf.Bind(rp)
	rp.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
	iblBuf.Bind(rp)
	coloredMesh.Draw(rp)
	rp.End()
	imgIBL := pbrVertexColorSubmitAndImage(t, sr, dev, e)
	iblTerm := render3d.EvaluateIBL(sh, pref, 1.0, math3d.Vec3{Z: 1}, math3d.Vec3{Z: 1}, [3]float32{tintedMat.BaseColor.X, tintedMat.BaseColor.Y, tintedMat.BaseColor.Z}, tintedMat.Metallic, tintedMat.Roughness, 1.0)
	srgbIBL := render3d.LinearToSRGB(lin.Add(math3d.Vec3{X: iblTerm[0], Y: iblTerm[1], Z: iblTerm[2]}))
	pbrVertexColorWantPixel(t, imgIBL, size/2, size/2, pbrVertexColorVec4ToRGBA(math3d.Vec4{X: srgbIBL.X, Y: srgbIBL.Y, Z: srgbIBL.Z, W: tintedMat.Alpha}), 3)
}

// TestPBRVertexColorKhronosVertexColorTest loads the unmodified Khronos VertexColorTest glTF 2.0 sample
// and renders it end-to-end with NewMaterialPipeline(VertexColors: true). In VertexColorTest, the top row
// ("Test" row) multiplies a multi-channel texture containing checkmarks + complementary X marks by pure
// R/G/B COLOR_0 vertex colors so it matches the bottom row ("Sample pass" row, multiplied by white).
func TestPBRVertexColorKhronosVertexColorTest(t *testing.T) {
	const size = 160
	dir := filepath.Join("..", "gltf", "testdata", "khronos", "VertexColorTest", "glTF")
	doc, err := gltf.LoadFS(os.DirFS(dir), "VertexColorTest.gltf")
	if err != nil {
		t.Fatalf("LoadFS VertexColorTest: %v", err)
	}

	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	defs, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	matSet, err := render3d.UploadGLTFMaterials(nil, dev, defs, doc, render3d.GLTFMaterialUpload{
		Label:          "VertexColorTest",
		DisableMipmaps: true,
	})
	if err != nil {
		t.Fatalf("UploadGLTFMaterials: %v", err)
	}

	// Find VertexColorTestMesh (has COLOR_0) and LabelMesh (no COLOR_0).
	var (
		vcNodeIdx    = -1
		labelNodeIdx = -1
	)
	for i, node := range doc.Nodes {
		if node.Mesh < 0 {
			continue
		}
		cm, err := render3d.ColoredMeshFromGLTF(doc, node.Mesh, 0)
		if err != nil {
			t.Fatalf("ColoredMeshFromGLTF(node %d): %v", i, err)
		}
		if cm.HasVertexColors() {
			vcNodeIdx = i
		} else {
			labelNodeIdx = i
		}
	}
	if vcNodeIdx < 0 || labelNodeIdx < 0 {
		t.Fatalf("expected one colored node and one uncolored node in VertexColorTest, got colored=%d uncolored=%d", vcNodeIdx, labelNodeIdx)
	}

	vcNode := doc.Nodes[vcNodeIdx]
	cm, err := render3d.ColoredMeshFromGLTF(doc, vcNode.Mesh, 0)
	if err != nil {
		t.Fatalf("ColoredMeshFromGLTF: %v", err)
	}
	pbrVertexColorBakeNodeTransform(&cm, vcNode.WorldMatrix)
	gpuColoredMesh, err := render3d.UploadColoredMesh(nil, dev, cm, render3d.MeshUpload{Label: "VertexColorTestMesh"})
	if err != nil {
		t.Fatalf("UploadColoredMesh: %v", err)
	}
	gpuPlainMesh, err := render3d.UploadMesh(nil, dev, cm.Mesh(), render3d.MeshUpload{Label: "VertexColorTestMeshPlain"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}

	prim := doc.Meshes[vcNode.Mesh].Primitives[0]
	matInfo, ok := matSet.Info(prim.Material)
	if !ok {
		t.Fatalf("missing material info for primitive material %d", prim.Material)
	}
	gpuMat := matInfo.GPU

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	// Orthographic front view covering [-1.1, 1.1] x [-1.1, 1.1] so lighting is uniform across all six tiles.
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 2.2, 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	pipeColored, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defs.Layout(), render3d.MaterialPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		VertexColors: true,
	})
	if err != nil {
		t.Fatalf("NewMaterialPipeline(colored): %v", err)
	}
	pipePlain, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defs.Layout(), render3d.MaterialPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		VertexColors: false,
	})
	if err != nil {
		t.Fatalf("NewMaterialPipeline(plain): %v", err)
	}

	renderScene := func(pipe *gpu.Pipeline, drawFn func(*gpu.RenderPass)) *image.RGBA {
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		gpuMat.Bind(pass)
		drawFn(pass)
		pass.End()
		return pbrVertexColorSubmitAndImage(t, sr, dev, enc)
	}

	imgColored := renderScene(pipeColored, gpuColoredMesh.Draw)
	imgPlain := renderScene(pipePlain, gpuPlainMesh.Draw)

	// Decode VertexColorChecks.png so we can evaluate CPU EvaluateMaterial on the front-facing tiles.
	checksImg, err := doc.DecodeImage(matInfo.BaseColor.ImageIndex)
	if err != nil {
		t.Fatalf("DecodeImage(VertexColorChecks): %v", err)
	}
	checksBounds := checksImg.Bounds()
	sampleChecks := func(u, v float32) [4]float32 {
		tx := int(math.Floor(float64(u) * float64(checksBounds.Dx())))
		ty := int(math.Floor(float64(v) * float64(checksBounds.Dy())))
		tx = min(max(tx, 0), checksBounds.Dx()-1)
		ty = min(max(ty, 0), checksBounds.Dy()-1)
		r, g, b, a := checksImg.At(checksBounds.Min.X+tx, checksBounds.Min.Y+ty).RGBA()
		return pbrVertexColorLinearFromSRGBPixel(color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)})
	}

	// Find the frontmost Z of VertexColorTestMesh (the 6 foreground checkmark tiles sit in front of the grey backing plate).
	maxFrontZ := float32(-math.MaxFloat32)
	for i := 0; i+2 < len(cm.Indices); i += 3 {
		i0, i1, i2 := cm.Indices[i], cm.Indices[i+1], cm.Indices[i+2]
		n := cm.Normals[i0].Add(cm.Normals[i1]).Add(cm.Normals[i2]).Norm()
		if n.Z < 0.9 {
			continue
		}
		z := (cm.Positions[i0].Z + cm.Positions[i1].Z + cm.Positions[i2].Z) / 3
		if z > maxFrontZ {
			maxFrontZ = z
		}
	}

	vp := cam.ViewProj()
	worldToPixel := func(p math3d.Vec3) (int, int) {
		ndc := vp.TransformPoint(p)
		px := int(math.Floor((float64(ndc.X)*0.5 + 0.5) * float64(size)))
		py := int(math.Floor((0.5 - float64(ndc.Y)*0.5) * float64(size)))
		return px, py
	}

	// Collect front-most tile triangles (normal.Z > 0.9 and z near maxFrontZ) and verify interior samples match EvaluateMaterial
	// AND that the top-row colored tiles suppress the complementary X-mark channels that bleed through on pipePlain.
	var (
		checkedColoredTris int
		checkedWhiteTris   int
		checkedFlatSamples int
		maxPlainDiffOnTop  int
		topTileBox         = math3d.AABBEmpty()
		botTileBox         = math3d.AABBEmpty()
	)
	expandBox := func(b *math3d.AABB, p math3d.Vec3) {
		b.Min.X = min(b.Min.X, p.X)
		b.Min.Y = min(b.Min.Y, p.Y)
		b.Min.Z = min(b.Min.Z, p.Z)
		b.Max.X = max(b.Max.X, p.X)
		b.Max.Y = max(b.Max.Y, p.Y)
		b.Max.Z = max(b.Max.Z, p.Z)
	}
	for i := 0; i+2 < len(cm.Indices); i += 3 {
		i0, i1, i2 := cm.Indices[i], cm.Indices[i+1], cm.Indices[i+2]
		n := cm.Normals[i0].Add(cm.Normals[i1]).Add(cm.Normals[i2]).Norm()
		if n.Z < 0.9 {
			continue
		}
		triZ := (cm.Positions[i0].Z + cm.Positions[i1].Z + cm.Positions[i2].Z) / 3
		if triZ < maxFrontZ-1e-3 {
			continue
		}
		c0, c1, c2 := cm.Colors[i0], cm.Colors[i1], cm.Colors[i2]
		vcol := math3d.Vec4{
			X: (c0.X + c1.X + c2.X) / 3,
			Y: (c0.Y + c1.Y + c2.Y) / 3,
			Z: (c0.Z + c1.Z + c2.Z) / 3,
			W: (c0.W + c1.W + c2.W) / 3,
		}
		isWhite := vcol.X > 0.9 && vcol.Y > 0.9 && vcol.Z > 0.9
		if isWhite {
			checkedWhiteTris++
			expandBox(&botTileBox, cm.Positions[i0])
			expandBox(&botTileBox, cm.Positions[i1])
			expandBox(&botTileBox, cm.Positions[i2])
		} else {
			checkedColoredTris++
			expandBox(&topTileBox, cm.Positions[i0])
			expandBox(&topTileBox, cm.Positions[i1])
			expandBox(&topTileBox, cm.Positions[i2])
		}

		// Sample several interior barycentric points across the triangle.
		for _, w := range [][3]float32{
			{1.0 / 3, 1.0 / 3, 1.0 / 3},
			{0.6, 0.2, 0.2},
			{0.2, 0.6, 0.2},
			{0.2, 0.2, 0.6},
		} {
			p := cm.Positions[i0].Scale(w[0]).Add(cm.Positions[i1].Scale(w[1])).Add(cm.Positions[i2].Scale(w[2]))
			u := cm.UVs[i0].X*w[0] + cm.UVs[i1].X*w[1] + cm.UVs[i2].X*w[2]
			v := cm.UVs[i0].Y*w[0] + cm.UVs[i1].Y*w[1] + cm.UVs[i2].Y*w[2]
			px, py := worldToPixel(p)
			if px < 0 || px >= size || py < 0 || py >= size {
				continue
			}
			got := imgColored.RGBAAt(px, py)
			gotPlain := imgPlain.RGBAAt(px, py)

			if !isWhite {
				diff := int(pbrVertexColorAbsDiffU8(got.R, gotPlain.R)) +
					int(pbrVertexColorAbsDiffU8(got.G, gotPlain.G)) +
					int(pbrVertexColorAbsDiffU8(got.B, gotPlain.B))
				if diff > maxPlainDiffOnTop {
					maxPlainDiffOnTop = diff
				}
			}

			// At solid interior points (away from sharp texel transitions), compare directly against EvaluateMaterial.
			texC := sampleChecks(u, v)
			texLeft := sampleChecks(u-1.5/float32(size), v)
			texRight := sampleChecks(u+1.5/float32(size), v)
			texUp := sampleChecks(u, v-1.5/float32(size))
			texDown := sampleChecks(u, v+1.5/float32(size))
			flat := true
			for _, nb := range [][4]float32{texLeft, texRight, texUp, texDown} {
				for ch := range 3 {
					if math.Abs(float64(texC[ch]-nb[ch])) > 0.05 {
						flat = false
					}
				}
			}
			if !flat {
				continue
			}
			checkedFlatSamples++
			want := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
				Material:           matInfo.Material,
				BaseColorSample:    texC,
				HasBaseColorSample: true,
				VertexColor:        [4]float32{vcol.X, vcol.Y, vcol.Z, vcol.W},
				HasVertexColor:     true,
				Normal:             n,
				ViewDir:            math3d.Vec3{Z: 1},
				FrontFacing:        true,
			})
			wantRGBA := pbrVertexColorVec4ToRGBA(want.SRGB)
			errCh := max(
				pbrVertexColorAbsDiffU8(got.R, wantRGBA.R),
				pbrVertexColorAbsDiffU8(got.G, wantRGBA.G),
				pbrVertexColorAbsDiffU8(got.B, wantRGBA.B),
			)
			if errCh > 4 {
				t.Errorf("VertexColorTest (%d,%d) vcol=%v: got %v, want %v (EvaluateMaterial)", px, py, vcol, got, wantRGBA)
			}
		}
	}
	if checkedColoredTris == 0 || checkedWhiteTris == 0 || checkedFlatSamples == 0 {
		t.Fatalf("expected front-facing colored (%d), white (%d), and flat interior (%d) samples in VertexColorTest", checkedColoredTris, checkedWhiteTris, checkedFlatSamples)
	}
	if maxPlainDiffOnTop < 80 {
		t.Fatalf("expected colored pipeline to differ strongly from uncolored pipeline on top Test row (X-marks suppressed), max channel-sum diff = %d", maxPlainDiffOnTop)
	}

	// Verify the top "Test" row and bottom "Sample pass" row match closely when translated vertically by their
	// row offset (both rows show upright R/G/B checkmarks), whereas the uncolored render has large differences
	// because the cyan/magenta/yellow X marks bleed through on the top row.
	topMinX, topMaxY := worldToPixel(math3d.Vec3{X: topTileBox.Min.X, Y: topTileBox.Min.Y, Z: maxFrontZ})
	topMaxX, topMinY := worldToPixel(math3d.Vec3{X: topTileBox.Max.X, Y: topTileBox.Max.Y, Z: maxFrontZ})
	_, botMinY := worldToPixel(math3d.Vec3{X: botTileBox.Min.X, Y: botTileBox.Max.Y, Z: maxFrontZ})
	rowDeltaY := botMinY - topMinY
	rowDiff := func(img *image.RGBA) int64 {
		var sum int64
		for y := topMinY + 2; y < topMaxY-2; y++ {
			by := y + rowDeltaY
			if by < 0 || by >= size {
				continue
			}
			for x := topMinX + 2; x < topMaxX-2; x++ {
				top := img.RGBAAt(x, y)
				bot := img.RGBAAt(x, by)
				sum += int64(pbrVertexColorAbsDiffU8(top.R, bot.R)) +
					int64(pbrVertexColorAbsDiffU8(top.G, bot.G)) +
					int64(pbrVertexColorAbsDiffU8(top.B, bot.B))
			}
		}
		return sum
	}
	coloredRowDiff := rowDiff(imgColored)
	plainRowDiff := rowDiff(imgPlain)
	if coloredRowDiff*3 >= plainRowDiff {
		t.Fatalf("expected VertexColorTest top-vs-bottom row difference with VertexColors:true (%d) to be much smaller than without (%d)", coloredRowDiff, plainRowDiff)
	}
}

func TestPBRVertexColorGLTFVec3AndVec4Fixtures(t *testing.T) {
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	defs, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 4}, 2, 1, 0.1, 10)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	clearCol := color.RGBA{R: 18, G: 22, B: 30, A: 255}
	clearCore := core.RGB(0x12161e)
	cutoff := float32(0.5)

	cases := []struct {
		name            string
		baseColorFactor [4]float32
		alphaMode       string
		alphaCutoff     *float32
		color           math3d.Vec4
		vec3            bool
		tex             *image.NRGBA
		expectDiscard   bool
	}{
		{
			name:            "vec3-opaque-tinted",
			baseColorFactor: [4]float32{0.9, 0.85, 0.8, 1.0},
			alphaMode:       "OPAQUE",
			color:           math3d.Vec4{X: 0.3, Y: 0.9, Z: 0.4, W: 1.0},
			vec3:            true,
			tex:             pbrVertexColorSolidNRGBA(240, 220, 200, 255),
		},
		{
			name:            "vec4-mask-discard",
			baseColorFactor: [4]float32{1, 1, 1, 1},
			alphaMode:       "MASK",
			alphaCutoff:     &cutoff,
			color:           math3d.Vec4{X: 1, Y: 0.4, Z: 0.2, W: 0.3},
			vec3:            false,
		},
		{
			name:            "vec4-mask-keep",
			baseColorFactor: [4]float32{1, 1, 1, 1},
			alphaMode:       "MASK",
			alphaCutoff:     &cutoff,
			color:           math3d.Vec4{X: 0.4, Y: 0.7, Z: 0.9, W: 0.8},
			vec3:            false,
		},
		{
			name:            "vec4-blend",
			baseColorFactor: [4]float32{1, 1, 1, 0.8},
			alphaMode:       "BLEND",
			color:           math3d.Vec4{X: 0.9, Y: 0.5, Z: 0.2, W: 0.5},
			vec3:            false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			colors := []math3d.Vec4{tc.color, tc.color, tc.color, tc.color}
			var texImg image.Image
			if tc.tex != nil {
				texImg = tc.tex
			}
			raw := pbrVertexColorBuildGLTFBytes(t, tc.baseColorFactor, 0.1, 0.7, tc.alphaMode, tc.alphaCutoff, colors, tc.vec3, texImg)
			doc, err := gltf.Load(raw, nil)
			if err != nil {
				t.Fatalf("gltf.Load: %v", err)
			}
			cm, err := render3d.ColoredMeshFromGLTF(doc, 0, 0)
			if err != nil {
				t.Fatalf("ColoredMeshFromGLTF: %v", err)
			}
			if !cm.HasVertexColors() {
				t.Fatalf("expected ColoredMeshFromGLTF to preserve COLOR_0")
			}
			gpuMesh, err := render3d.UploadColoredMesh(nil, dev, cm, render3d.MeshUpload{Label: tc.name})
			if err != nil {
				t.Fatalf("UploadColoredMesh: %v", err)
			}
			defer gpuMesh.Close()
			matSet, err := render3d.UploadGLTFMaterials(nil, dev, defs, doc, render3d.GLTFMaterialUpload{
				Label:          tc.name,
				DisableMipmaps: true,
			})
			if err != nil {
				t.Fatalf("UploadGLTFMaterials: %v", err)
			}
			defer matSet.Close()
			info, ok := matSet.Info(0)
			if !ok {
				t.Fatalf("missing material 0")
			}
			pipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defs.Layout(), render3d.MaterialPipelineOptions{
				ColorFormat:  gpu.FormatRGBA8Unorm,
				DepthFormat:  gpu.FormatDepth24Plus,
				AlphaMode:    info.Material.AlphaMode,
				VertexColors: true,
			})
			if err != nil {
				t.Fatalf("NewMaterialPipeline: %v", err)
			}
			defer pipe.Close()

			enc := dev.BeginEncoder()
			pass := enc.BeginRenderPass(gpu.RenderPassDesc{
				ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: clearCore}},
				DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
			})
			pass.SetPipeline(pipe)
			pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
			info.GPU.Bind(pass)
			gpuMesh.Draw(pass)
			pass.End()

			img := pbrVertexColorSubmitAndImage(t, sr, dev, enc)

			evalIn := render3d.MaterialEvalInput{
				Material:       info.Material,
				VertexColor:    [4]float32{tc.color.X, tc.color.Y, tc.color.Z, tc.color.W},
				HasVertexColor: true,
				Normal:         math3d.Vec3{Z: 1},
				ViewDir:        math3d.Vec3{Z: 1},
				FrontFacing:    true,
			}
			if tc.tex != nil {
				c := tc.tex.NRGBAAt(0, 0)
				evalIn.BaseColorSample = pbrVertexColorLinearFromSRGBPixel(color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A})
				evalIn.HasBaseColorSample = true
			}
			want := render3d.EvaluateMaterial(evalIn)
			if tc.expectDiscard || want.Discarded {
				if !want.Discarded {
					t.Fatalf("expected EvaluateMaterial to discard")
				}
				pbrVertexColorWantPixel(t, img, size/2, size/2, clearCol, 0)
				return
			}
			wantCol := pbrVertexColorVec4ToRGBA(want.SRGB)
			if info.Material.AlphaMode == render3d.AlphaBlend {
				wantCol = pbrVertexColorBlendOver(want.SRGB, clearCol)
			}
			pbrVertexColorWantPixel(t, img, size/2, size/2, wantCol, 3)
		})
	}
}

func TestPBRVertexColorGPUViewProfiles(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	vcMat := math3d.Vec4{X: 0.75, Y: 0.25, Z: 0.6, W: 1.0}
	vcLit := math3d.Vec4{X: 0.2, Y: 0.8, Z: 0.45, W: 1.0}
	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.1, Y: 0.12, Z: 0.15}, Intensity: 1},
		Directional: []render3d.DirectionalLight{{
			Direction: math3d.Vec3{X: -0.3, Y: -0.5, Z: -0.8},
			Color:     math3d.Vec3{X: 1, Y: 0.95, Z: 0.9},
			Intensity: 2.0,
		}},
	}
	pbrMat := render3d.PBRMaterial{
		BaseColor: math3d.Vec3{X: 0.95, Y: 0.9, Z: 0.85},
		Alpha:     1,
		Metallic:  0.1,
		Roughness: 0.6,
	}

	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			const size = 64
			var (
				camBuf     *render3d.CameraBuffer
				defs       *render3d.MaterialDefaults
				matPipe    *gpu.Pipeline
				litPipe    *gpu.Pipeline
				gpuMat     *render3d.GPUMaterial
				lightBuf   *render3d.GPULightBuffer
				drawBuf    *render3d.PBRDrawBuffer
				leftMesh   *render3d.GPUColoredMesh
				rightMesh  *render3d.GPUColoredMesh
				matCPU     render3d.Material
				renderedOK bool
			)
			opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "pbr-vc-view",
					Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						var err error
						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
						cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 4}, 2, 1, 0.1, 10)
						cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
						camBuf.WriteCamera(cam)

						defs, err = render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{})
						if err != nil {
							t.Fatalf("NewMaterialDefaults: %v", err)
						}
						matPipe, err = render3d.NewMaterialPipeline(cx, dev, camBuf.Layout(), defs.Layout(), render3d.MaterialPipelineOptions{
							DepthFormat:  gpu.FormatDepth24Plus,
							VertexColors: true,
						})
						if err != nil {
							t.Fatalf("NewMaterialPipeline: %v", err)
						}
						matCPU = render3d.DefaultMaterial()
						matCPU.BaseColorFactor = [4]float32{0.9, 0.9, 0.9, 1}
						matCPU.MetallicFactor = 0.2
						matCPU.RoughnessFactor = 0.65
						gpuMat, err = render3d.NewGPUMaterial(cx, dev, defs, matCPU, render3d.MaterialUpload{})
						if err != nil {
							t.Fatalf("NewGPUMaterial: %v", err)
						}

						lightBuf, err = render3d.NewLightBuffer(cx, dev, rig, render3d.LightBufferOptions{})
						if err != nil {
							t.Fatalf("NewLightBuffer: %v", err)
						}
						drawBuf, err = render3d.NewPBRDrawBuffer(cx, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), pbrMat), false)
						if err != nil {
							t.Fatalf("NewPBRDrawBuffer: %v", err)
						}
						litPipe, err = render3d.NewLitPBRPipelineWithOptions(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{
							DepthFormat:  gpu.FormatDepth24Plus,
							VertexColors: true,
						})
						if err != nil {
							t.Fatalf("NewLitPBRPipelineWithOptions: %v", err)
						}

						leftMesh, err = render3d.UploadColoredMesh(cx, dev, pbrVertexColorQuad(math3d.Vec3{X: -0.5}, 0.45, 0.8, []math3d.Vec4{vcMat, vcMat, vcMat, vcMat}), render3d.MeshUpload{Label: "left"})
						if err != nil {
							t.Fatalf("UploadColoredMesh(left): %v", err)
						}
						rightMesh, err = render3d.UploadColoredMesh(cx, dev, pbrVertexColorQuad(math3d.Vec3{X: 0.5}, 0.45, 0.8, []math3d.Vec4{vcLit, vcLit, vcLit, vcLit}), render3d.MeshUpload{Label: "right"})
						if err != nil {
							t.Fatalf("UploadColoredMesh(right): %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x10141c))
						pass.ClearDepth(1)
						pass.SetPipeline(matPipe)
						pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
						gpuMat.Bind(pass)
						leftMesh.Draw(pass)

						pass.SetPipeline(litPipe)
						pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
						lightBuf.Bind(pass)
						pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
						rightMesh.Draw(pass)

						pass.End()
						frame.Submit()
						renderedOK = true
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)
			if !renderedOK {
				t.Fatalf("OnFrame did not run on profile %s", p.name)
			}
			shot, err := app.GPUReadPixels("id:pbr-vc-view")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			wantLeft := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
				Material:       matCPU,
				VertexColor:    [4]float32{vcMat.X, vcMat.Y, vcMat.Z, vcMat.W},
				HasVertexColor: true,
				Normal:         math3d.Vec3{Z: 1},
				ViewDir:        math3d.Vec3{Z: 1},
				FrontFacing:    true,
			})
			pbrVertexColorWantPixel(t, shot, size/4, size/2, pbrVertexColorVec4ToRGBA(wantLeft.SRGB), 2)

			tinted := pbrMat.WithVertexColor(vcLit)
			linRight := render3d.ShadePoint(math3d.Vec3{X: 0.5}, math3d.Vec3{Z: 1}, math3d.Vec3{Z: 1}, tinted, rig)
			srgbRight := render3d.LinearToSRGB(linRight)
			pbrVertexColorWantPixel(t, shot, (3*size)/4, size/2, pbrVertexColorVec4ToRGBA(math3d.Vec4{X: srgbRight.X, Y: srgbRight.Y, Z: srgbRight.Z, W: tinted.Alpha}), 2)
		})
	}
}
