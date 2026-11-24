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
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func pbrLightmapEncodePNGDataURI(t *testing.T, img image.Image) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func pbrLightmapOrthoCamera(aspect float32) *scene3d.Camera {
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{X: 0, Y: 0, Z: 2}, 2, aspect, 0.1, 10)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	return cam
}

func pbrLightmapQuad(x0, y0, x1, y1 float32) render3d.LightmapMesh {
	n := math3d.Vec3{Z: 1}
	return render3d.LightmapMesh{
		Positions: []math3d.Vec3{
			{X: x0, Y: y0, Z: 0},
			{X: x1, Y: y0, Z: 0},
			{X: x1, Y: y1, Z: 0},
			{X: x0, Y: y1, Z: 0},
		},
		Normals: []math3d.Vec3{n, n, n, n},
		UV0: []math3d.Vec2{
			{X: 0, Y: 1},
			{X: 1, Y: 1},
			{X: 1, Y: 0},
			{X: 0, Y: 0},
		},
		UV1: []math3d.Vec2{
			{X: 0, Y: 1},
			{X: 1, Y: 1},
			{X: 1, Y: 0},
			{X: 0, Y: 0},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

func pbrLightmapSolidImage(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func pbrLightmapRunPass(t *testing.T, sr *gpu.SoftwareRenderer, draw func(pass *gpu.RenderPass)) *image.RGBA {
	t.Helper()
	dev := sr.Device()
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: core.RGB(0x000000),
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1,
		},
	})
	draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	return pbrLightmapCloneRGBA(sr.Image())
}

func pbrLightmapCloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func pbrLightmapByteDiff(a, b uint8) int {
	d := int(a) - int(b)
	if d < 0 {
		return -d
	}
	return d
}

func pbrLightmapAssertPixelNear(t *testing.T, label string, got, want color.RGBA, tol int) {
	t.Helper()
	if pbrLightmapByteDiff(got.R, want.R) > tol ||
		pbrLightmapByteDiff(got.G, want.G) > tol ||
		pbrLightmapByteDiff(got.B, want.B) > tol ||
		pbrLightmapByteDiff(got.A, want.A) > tol {
		t.Errorf("%s = RGBA(%d,%d,%d,%d), want RGBA(%d,%d,%d,%d) (±%d)",
			label, got.R, got.G, got.B, got.A, want.R, want.G, want.B, want.A, tol)
	}
}

func pbrLightmapToByte(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(math.Round(float64(v) * 255))
}

func pbrLightmapAssertLinearByte(t *testing.T, label string, gotSRGB uint8, wantLinear float32, tol int) {
	t.Helper()
	gotLin := int(math.Round(float64(render3d.SRGBToLinearChannel(float32(gotSRGB)/255)) * 255))
	wantLin := int(math.Round(float64(wantLinear) * 255))
	diff := gotLin - wantLin
	if diff < 0 {
		diff = -diff
	}
	if diff > tol {
		t.Errorf("%s sRGB=%d (linear %d), want linear %d (±%d)", label, gotSRGB, gotLin, wantLin, tol)
	}
}

// pbrLightmapGLTFWithPBRAndMOZLightmap builds a self-contained glTF 2.0 scene combining:
//   - TEXCOORD_0 (tiled material UVs) and TEXCOORD_1 (non-overlapping [0,1]x[0,1] lightmap chart)
//   - baseColorTexture (neutral bright albedo)
//   - metallicRoughnessTexture (left half dielectric M=0, R=0.9; right half metallic M=1, R=0.2)
//   - normalTexture (tilted tangent-space normal)
//   - occlusionTexture (top half AO=1.0, bottom half AO=0.3)
//   - emissiveTexture + emissiveFactor (emissive bottom-right quadrant)
//   - MOZ_lightmap extension (RGBM HDR range 4, intensity 0.5, warm top-left/bottom-left vs cool top-right/bottom-right)
func pbrLightmapGLTFWithPBRAndMOZLightmap(t *testing.T, flatNormal bool) []byte {
	t.Helper()

	albedoImg := pbrLightmapSolidImage(4, 4, color.NRGBA{R: 220, G: 210, B: 200, A: 255})

	// glTF metallic-roughness: G = roughness, B = metallic.
	// Left half (x=0,1): dielectric (B=0), rough (G=230 ~= 0.90).
	// Right half (x=2,3): metallic (B=255), smooth (G=51 ~= 0.20).
	mrImg := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			if x < 2 {
				mrImg.SetNRGBA(x, y, color.NRGBA{R: 255, G: 230, B: 0, A: 255})
			} else {
				mrImg.SetNRGBA(x, y, color.NRGBA{R: 255, G: 51, B: 255, A: 255})
			}
		}
	}

	// Normal map: flat (128,128,255) or tilted strongly toward +X/+Y (230,200,160).
	normColor := color.NRGBA{R: 230, G: 200, B: 160, A: 255}
	if flatNormal {
		normColor = color.NRGBA{R: 128, G: 128, B: 255, A: 255}
	}
	normImg := pbrLightmapSolidImage(4, 4, normColor)

	// Occlusion map (R channel): top half (y=0,1) unoccluded (255), bottom half (y=2,3) occluded (77 ~= 0.30).
	occImg := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		v := uint8(255)
		if y >= 2 {
			v = 77
		}
		for x := range 4 {
			occImg.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}

	// Emissive map: glowing green-cyan only in bottom-right (x>=2, y>=2).
	emisImg := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			if x >= 2 && y >= 2 {
				emisImg.SetNRGBA(x, y, color.NRGBA{R: 40, G: 240, B: 220, A: 255})
			} else {
				emisImg.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 255})
			}
		}
	}

	// Baked lightmap encoded as RGBM (maxRange 4, scaled 2x with material intensity 0.5):
	// Left half (x=0,1): warm sunlight (0.95, 0.72, 0.35); right half (x=2,3): cool bounce (0.20, 0.35, 0.85).
	warm := math3d.Vec3{X: 2 * 0.95, Y: 2 * 0.72, Z: 2 * 0.35}
	cool := math3d.Vec3{X: 2 * 0.20, Y: 2 * 0.35, Z: 2 * 0.85}
	lmPixels := make([]math3d.Vec3, 4*4)
	for y := range 4 {
		for x := range 4 {
			if x < 2 {
				lmPixels[y*4+x] = warm
			} else {
				lmPixels[y*4+x] = cool
			}
		}
	}
	lmImg, err := render3d.EncodeRGBMImage(lmPixels, 4, 4, 4.0)
	if err != nil {
		t.Fatalf("EncodeRGBMImage: %v", err)
	}

	var bin []byte
	appendF32 := func(vals ...float32) (int, int) {
		off := len(bin)
		for _, v := range vals {
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], math.Float32bits(v))
			bin = append(bin, b[:]...)
		}
		return off, len(bin) - off
	}
	appendU16 := func(vals ...uint16) (int, int) {
		off := len(bin)
		for _, v := range vals {
			var b [2]byte
			binary.LittleEndian.PutUint16(b[:], v)
			bin = append(bin, b[:]...)
		}
		for len(bin)%4 != 0 {
			bin = append(bin, 0)
		}
		return off, len(vals) * 2
	}

	posOff, posLen := appendF32(
		-1, -1, 0,
		1, -1, 0,
		1, 1, 0,
		-1, 1, 0,
	)
	normOff, normLen := appendF32(
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
		0, 0, 1,
	)
	// TEXCOORD_0 spans [0,1]x[0,1] matching the 4x4 PBR maps.
	uv0Off, uv0Len := appendF32(
		0, 1,
		1, 1,
		1, 0,
		0, 0,
	)
	// TEXCOORD_1 spans [0,1]x[0,1] mapped into the lightmap chart.
	uv1Off, uv1Len := appendF32(
		0, 1,
		1, 1,
		1, 0,
		0, 0,
	)
	idxOff, idxLen := appendU16(0, 1, 2, 0, 2, 3)

	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"extensionsUsed": []string{
			gltf.ExtMOZLightmap,
		},
		"buffers": []map[string]any{{
			"byteLength": len(bin),
			"uri":        "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(bin),
		}},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": posOff, "byteLength": posLen},
			{"buffer": 0, "byteOffset": normOff, "byteLength": normLen},
			{"buffer": 0, "byteOffset": uv0Off, "byteLength": uv0Len},
			{"buffer": 0, "byteOffset": uv1Off, "byteLength": uv1Len},
			{"buffer": 0, "byteOffset": idxOff, "byteLength": idxLen},
		},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": 4, "type": "VEC3", "min": []float32{-1, -1, 0}, "max": []float32{1, 1, 0}},
			{"bufferView": 1, "componentType": 5126, "count": 4, "type": "VEC3"},
			{"bufferView": 2, "componentType": 5126, "count": 4, "type": "VEC2"},
			{"bufferView": 3, "componentType": 5126, "count": 4, "type": "VEC2"},
			{"bufferView": 4, "componentType": 5123, "count": 6, "type": "SCALAR"},
		},
		"samplers": []map[string]any{
			{"name": "nearest_clamp", "magFilter": 9728, "minFilter": 9728, "wrapS": 33071, "wrapT": 33071},
		},
		"images": []map[string]any{
			{"name": "albedo", "uri": pbrLightmapEncodePNGDataURI(t, albedoImg), "mimeType": "image/png"},
			{"name": "mr", "uri": pbrLightmapEncodePNGDataURI(t, mrImg), "mimeType": "image/png"},
			{"name": "normal", "uri": pbrLightmapEncodePNGDataURI(t, normImg), "mimeType": "image/png"},
			{"name": "occlusion", "uri": pbrLightmapEncodePNGDataURI(t, occImg), "mimeType": "image/png"},
			{"name": "emissive", "uri": pbrLightmapEncodePNGDataURI(t, emisImg), "mimeType": "image/png"},
			{"name": "lightmap", "uri": pbrLightmapEncodePNGDataURI(t, lmImg), "mimeType": "image/png"},
		},
		"textures": []map[string]any{
			{"sampler": 0, "source": 0},
			{"sampler": 0, "source": 1},
			{"sampler": 0, "source": 2},
			{"sampler": 0, "source": 3},
			{"sampler": 0, "source": 4},
			{"sampler": 0, "source": 5},
		},
		"materials": []map[string]any{{
			"name": "baked_pbr_wall",
			"pbrMetallicRoughness": map[string]any{
				"baseColorFactor":          []float32{1, 1, 1, 1},
				"baseColorTexture":         map[string]any{"index": 0, "texCoord": 0},
				"metallicFactor":           1.0,
				"roughnessFactor":          1.0,
				"metallicRoughnessTexture": map[string]any{"index": 1, "texCoord": 0},
			},
			"normalTexture":    map[string]any{"index": 2, "scale": 1.0},
			"occlusionTexture": map[string]any{"index": 3, "strength": 1.0},
			"emissiveFactor":   []float32{0.8, 0.8, 0.8},
			"emissiveTexture":  map[string]any{"index": 4},
			"extensions": map[string]any{
				gltf.ExtMOZLightmap: map[string]any{
					"index":        5,
					"texCoord":     1,
					"intensity":    0.5,
					"encoding":     "RGBM",
					"rgbmMaxRange": 4.0,
				},
			},
		}},
		"meshes": []map[string]any{{
			"name": "wall",
			"primitives": []map[string]any{{
				"attributes": map[string]int{
					"POSITION":   0,
					"NORMAL":     1,
					"TEXCOORD_0": 2,
					"TEXCOORD_1": 3,
				},
				"indices":  4,
				"material": 0,
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

// TestPBRLightmapMatchesLightmapRendererAndLitPBR verifies both halves of the #752 exact-parity acceptance criterion:
//  1. For a dielectric (metallic=0), rough (roughness=1), untextured material with no dynamic lights, rendered pixels from NewPBRLightmapPipeline match LightmapRenderer exactly.
//  2. Adding a LightRig directional light adds the exact same direct Cook-Torrance GGX term as an unlightmapped LitPBRWGSL draw, and LightRig.Ambient is replaced (not double-counted) when a lightmap is bound.
func TestPBRLightmapMatchesLightmapRendererAndLitPBR(t *testing.T) {
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	cam := pbrLightmapOrthoCamera(1)
	camBuf.WriteCamera(cam)

	// 4x2 linear lightmap: left half warm sunlit, right half cool shadow.
	lmImg := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for y := range 2 {
		lmImg.SetNRGBA(0, y, color.NRGBA{R: 210, G: 170, B: 110, A: 255})
		lmImg.SetNRGBA(1, y, color.NRGBA{R: 210, G: 170, B: 110, A: 255})
		lmImg.SetNRGBA(2, y, color.NRGBA{R: 40, G: 70, B: 150, A: 255})
		lmImg.SetNRGBA(3, y, color.NRGBA{R: 40, G: 70, B: 150, A: 255})
	}
	lmTex, err := render3d.UploadTexture(nil, dev, lmImg, render3d.TextureUpload{Label: "parity-lm"})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}
	t.Cleanup(func() { _ = lmTex.Close() })

	nearestClamp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	t.Cleanup(func() { _ = nearestClamp.Close() })

	quadData := pbrLightmapQuad(-1, -1, 1, 1)
	lmMesh, err := render3d.UploadLightmapMesh(nil, dev, quadData, render3d.MeshUpload{Label: "parity-lm-quad"})
	if err != nil {
		t.Fatalf("UploadLightmapMesh: %v", err)
	}
	t.Cleanup(func() { _ = lmMesh.Close() })

	stdMesh, err := render3d.UploadMesh(nil, dev, render3d.Mesh{
		Positions: quadData.Positions,
		Normals:   quadData.Normals,
		UVs:       quadData.UV0,
		Indices:   quadData.Indices,
	}, render3d.MeshUpload{Label: "parity-std-quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = stdMesh.Close() })

	baseColor := [4]float32{0.8, 0.55, 0.35, 1.0}
	lmIntensity := float32(0.75)

	// --- Render 1: LightmapRenderer baseline (no dynamic lights) ---
	lmRenderer, err := render3d.NewLightmapRenderer(nil, dev, camBuf.Layout(), render3d.LightmapRendererOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewLightmapRenderer: %v", err)
	}
	t.Cleanup(func() { _ = lmRenderer.Close() })

	lmRenderer.BeginFrame()
	lmMat := render3d.DefaultLightmapMaterial()
	lmMat.BaseColor = baseColor
	lmMat.Lightmap = lmTex
	lmMat.LightmapSampler = nearestClamp
	lmMat.Encoding = render3d.LightmapLinear
	lmMat.Intensity = lmIntensity
	lmRendererImg := pbrLightmapRunPass(t, sr, func(pass *gpu.RenderPass) {
		if err := lmRenderer.Draw(pass, camBuf, lmMesh, math3d.Mat4Identity(), lmMat); err != nil {
			t.Fatalf("LightmapRenderer.Draw: %v", err)
		}
	})

	// --- Setup PBR Lightmap Pipeline ---
	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{Label: "parity-defaults"})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = defaults.Close() })

	pbrMatDesc := render3d.DefaultMaterial()
	pbrMatDesc.BaseColorFactor = baseColor
	pbrMatDesc.MetallicFactor = 0.0
	pbrMatDesc.RoughnessFactor = 1.0
	gpuMat, err := render3d.NewGPUMaterial(nil, dev, defaults, pbrMatDesc, render3d.MaterialUpload{Label: "parity-mat"})
	if err != nil {
		t.Fatalf("NewGPUMaterial: %v", err)
	}
	t.Cleanup(func() { _ = gpuMat.Close() })

	lightBuf, err := render3d.NewLightBuffer(nil, dev, render3d.LightRig{}, render3d.LightBufferOptions{Label: "parity-lights"})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	pbrLMDesc := render3d.DefaultPBRLightmap()
	pbrLMDesc.Lightmap = lmTex
	pbrLMDesc.LightmapSampler = nearestClamp
	pbrLMDesc.Encoding = render3d.LightmapLinear
	pbrLMDesc.Intensity = lmIntensity
	gpuLM, err := render3d.NewGPUPBRLightmap(nil, dev, pbrLMDesc, render3d.PBRLightmapOptions{Label: "parity-pbr-lm"})
	if err != nil {
		t.Fatalf("NewGPUPBRLightmap: %v", err)
	}
	t.Cleanup(func() { _ = gpuLM.Close() })

	pbrLMPipe, err := render3d.NewPBRLightmapPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), defaults.Layout(), gpuLM.Layout(), render3d.PBRLightmapPipelineOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewPBRLightmapPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pbrLMPipe.Close() })

	renderPBRLightmap := func() *image.RGBA {
		return pbrLightmapRunPass(t, sr, func(p *gpu.RenderPass) {
			p.SetPipeline(pbrLMPipe)
			p.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
			lightBuf.Bind(p)
			p.SetBindGroup(render3d.PBRLightmapMaterialGroup, gpuMat.BindGroup())
			gpuLM.Bind(p)
			lmMesh.Draw(p)
		})
	}

	// --- Render 2: Lightmapped PBR with no dynamic lights ---
	pbrLMNoDirImg := renderPBRLightmap()

	samplePoints := [][2]int{{16, 32}, {24, 16}, {48, 32}, {52, 48}}
	for _, pt := range samplePoints {
		got := pbrLMNoDirImg.RGBAAt(pt[0], pt[1])
		want := lmRendererImg.RGBAAt(pt[0], pt[1])
		pbrLightmapAssertPixelNear(t, "parity-vs-LightmapRenderer", got, want, 1)
	}

	// Verify CPU EvaluatePBRLightmap matches GPU pixels for sunlit left and shadowed right.
	sunEval := render3d.EvaluatePBRLightmap(render3d.PBRLightmapEvalInput{
		Material:       pbrMatDesc,
		HasLightmap:    true,
		LightmapSample: [4]float32{210.0 / 255, 170.0 / 255, 110.0 / 255, 1},
		Encoding:       render3d.LightmapLinear,
		Intensity:      lmIntensity,
		Normal:         math3d.Vec3{Z: 1},
		ViewDir:        math3d.Vec3{Z: 1},
		FrontFacing:    true,
	})
	pbrLightmapAssertPixelNear(t, "cpu-eval-sun", pbrLMNoDirImg.RGBAAt(16, 32), color.RGBA{
		R: pbrLightmapToByte(sunEval.SRGB.X),
		G: pbrLightmapToByte(sunEval.SRGB.Y),
		B: pbrLightmapToByte(sunEval.SRGB.Z),
		A: 255,
	}, 1)

	// --- Render 3: Non-zero LightRig.Ambient must be REPLACED by baked lightmap ---
	if err := lightBuf.Update(dev, render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0, Y: 0, Z: 1}, Intensity: 2.0},
	}); err != nil {
		t.Fatalf("lightBuf.Update ambient: %v", err)
	}
	pbrLMWithRigAmbientImg := renderPBRLightmap()
	for _, pt := range samplePoints {
		pbrLightmapAssertPixelNear(t, "lightmap-replaces-rig-ambient",
			pbrLMWithRigAmbientImg.RGBAAt(pt[0], pt[1]),
			pbrLMNoDirImg.RGBAAt(pt[0], pt[1]), 0)
	}

	// --- Render 4 & 5: Adding a LightRig directional light adds the EXACT direct term of LitPBRWGSL ---
	dirLight := render3d.DirectionalLight{
		Direction: math3d.Vec3{X: -0.3, Y: -0.4, Z: -0.85}.Norm(),
		Color:     math3d.Vec3{X: 0.9, Y: 0.85, Z: 0.75},
		Intensity: 1.4,
	}
	dirRig := render3d.LightRig{
		Directional: []render3d.DirectionalLight{dirLight},
	}
	if err := lightBuf.Update(dev, dirRig); err != nil {
		t.Fatalf("lightBuf.Update dir: %v", err)
	}

	// 4a. Unlightmapped LitPBRWGSL draw with the same material and directional light (no ambient).
	drawBuf, err := render3d.NewPBRDrawBuffer(nil, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), render3d.PBRMaterial{
		BaseColor: math3d.Vec3{X: baseColor[0], Y: baseColor[1], Z: baseColor[2]},
		Alpha:     1,
		Metallic:  0,
		Roughness: 1,
	}), false)
	if err != nil {
		t.Fatalf("NewPBRDrawBuffer: %v", err)
	}
	t.Cleanup(func() { _ = drawBuf.Close() })

	litPBRPipe, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewLitPBRPipelineWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = litPBRPipe.Close() })

	litPBRDirectOnlyImg := pbrLightmapRunPass(t, sr, func(p *gpu.RenderPass) {
		p.SetPipeline(litPBRPipe)
		p.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		lightBuf.Bind(p)
		p.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
		stdMesh.Draw(p)
	})

	// 4b. Lightmapped PBR with Intensity=0 and directional light must match LitPBRWGSL pixel-for-pixel.
	zeroLM := pbrLMDesc
	zeroLM.Intensity = 0
	if err := gpuLM.Update(dev, zeroLM); err != nil {
		t.Fatalf("gpuLM.Update(zero): %v", err)
	}
	pbrLMDirectOnlyImg := renderPBRLightmap()
	for _, pt := range samplePoints {
		pbrLightmapAssertPixelNear(t, "direct-term-matches-LitPBRWGSL",
			pbrLMDirectOnlyImg.RGBAAt(pt[0], pt[1]),
			litPBRDirectOnlyImg.RGBAAt(pt[0], pt[1]), 1)
	}

	// 4c. Lightmapped PBR with Intensity=0.75 + directional light must equal linear(baked) + linear(LitPBRWGSL direct).
	if err := gpuLM.Update(dev, pbrLMDesc); err != nil {
		t.Fatalf("gpuLM.Update(restore): %v", err)
	}
	pbrLMCombinedImg := renderPBRLightmap()

	for _, pt := range samplePoints {
		bakedPx := pbrLMNoDirImg.RGBAAt(pt[0], pt[1])
		directPx := litPBRDirectOnlyImg.RGBAAt(pt[0], pt[1])
		combPx := pbrLMCombinedImg.RGBAAt(pt[0], pt[1])

		wantLinR := min(render3d.SRGBToLinearChannel(float32(bakedPx.R)/255)+render3d.SRGBToLinearChannel(float32(directPx.R)/255), 1.0)
		wantLinG := min(render3d.SRGBToLinearChannel(float32(bakedPx.G)/255)+render3d.SRGBToLinearChannel(float32(directPx.G)/255), 1.0)
		wantLinB := min(render3d.SRGBToLinearChannel(float32(bakedPx.B)/255)+render3d.SRGBToLinearChannel(float32(directPx.B)/255), 1.0)

		pbrLightmapAssertLinearByte(t, "combined.R", combPx.R, wantLinR, 2)
		pbrLightmapAssertLinearByte(t, "combined.G", combPx.G, wantLinG, 2)
		pbrLightmapAssertLinearByte(t, "combined.B", combPx.B, wantLinB, 2)
	}
}

// TestPBRLightmapGLTFMOZLightmapWithPBRMapsInGPUView loads a glTF with MOZ_lightmap AND full metallic-roughness, normal, occlusion, and emissive maps through UploadGLTFMaterials + UploadGLTFPBRLightmaps + LightmapMeshFromGLTF and draws it in one lit PBR pipeline on both full and NoGPUCompute (WebGL 2 / GLES 3.0) profiles.
func TestPBRLightmapGLTFMOZLightmapWithPBRMapsInGPUView(t *testing.T) {
	tiltedDoc, err := gltf.Load(pbrLightmapGLTFWithPBRAndMOZLightmap(t, false), nil)
	if err != nil {
		t.Fatalf("gltf.Load tilted: %v", err)
	}
	flatDoc, err := gltf.Load(pbrLightmapGLTFWithPBRAndMOZLightmap(t, true), nil)
	if err != nil {
		t.Fatalf("gltf.Load flat: %v", err)
	}

	prim := tiltedDoc.Meshes[0].Primitives[0]
	meshData, err := render3d.LightmapMeshFromGLTF(tiltedDoc, prim)
	if err != nil {
		t.Fatalf("LightmapMeshFromGLTF: %v", err)
	}
	if !meshData.HasUV1() {
		t.Fatal("expected LightmapMeshFromGLTF to populate UV1 from TEXCOORD_1")
	}

	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"no-compute-webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	renderScene := func(t *testing.T, doc *gltf.Document, profOpts []nsgotest.Option) *image.RGBA {
		t.Helper()
		var (
			rendered bool
			drawErr  error
		)
		opts := append([]nsgotest.Option{nsgotest.Size(64, 64), nsgotest.ManualFrames()}, profOpts...)
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			var (
				camBuf   *render3d.CameraBuffer
				defaults *render3d.MaterialDefaults
				matSet   *render3d.GLTFMaterialSet
				lmSet    *render3d.GLTFPBRLightmapSet
				lightBuf *render3d.GPULightBuffer
				gpuMesh  *render3d.GPULightmapMesh
				pipe     *gpu.Pipeline
			)
			return gpu.View(cx, gpu.ViewProps{
				TestID:      "pbr-lm-view",
				Style:       core.Style{Width: core.Pt(64), Height: core.Pt(64)},
				DepthFormat: gpu.FormatDepth24Plus,
				OnReady: func(dev *gpu.Device) {
					var err error
					camBuf, err = render3d.NewCameraBuffer(cx, dev)
					if err != nil {
						drawErr = err
						return
					}
					defaults, err = render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{Retained: true})
					if err != nil {
						drawErr = err
						return
					}
					matSet, err = render3d.UploadGLTFMaterials(cx, dev, defaults, doc, render3d.GLTFMaterialUpload{
						Label:          "baked-pbr",
						DisableMipmaps: true,
						Retained:       true,
					})
					if err != nil {
						drawErr = err
						return
					}
					lmSet, err = render3d.UploadGLTFPBRLightmaps(cx, dev, doc, render3d.GLTFPBRLightmapUpload{
						Label:    "baked-pbr-lm",
						Retained: true,
					})
					if err != nil {
						drawErr = err
						return
					}
					lightBuf, err = render3d.NewLightBuffer(cx, dev, render3d.LightRig{
						// Non-zero ambient should be replaced by MOZ_lightmap.
						Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0, Y: 0, Z: 1}, Intensity: 1.0},
						Directional: []render3d.DirectionalLight{{
							// Light shining nearly along -Z so smooth metal on the right gets a strong GGX specular highlight while tilted normal changes shading noticeably.
							Direction: math3d.Vec3{X: -0.25, Y: -0.15, Z: -0.95}.Norm(),
							Color:     math3d.Vec3{X: 1.0, Y: 1.0, Z: 1.0},
							Intensity: 1.2,
						}},
					}, render3d.LightBufferOptions{Retained: true})
					if err != nil {
						drawErr = err
						return
					}
					gpuMesh, err = render3d.UploadLightmapMesh(cx, dev, meshData, render3d.MeshUpload{
						Label:    "baked-pbr-wall",
						Retained: true,
					})
					if err != nil {
						drawErr = err
						return
					}
					pipe, err = render3d.NewPBRLightmapPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), defaults.Layout(), lmSet.Layout(), render3d.PBRLightmapPipelineOptions{
						DepthFormat: gpu.FormatDepth24Plus,
						Retained:    true,
					})
					if err != nil {
						drawErr = err
						return
					}
				},
				OnFrame: func(frame *gpu.Frame) {
					if pipe == nil || gpuMesh == nil {
						return
					}
					camBuf.WriteCamera(pbrLightmapOrthoCamera(1))
					pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
					pass.ClearColor(core.RGB(0x000000))
					pass.ClearDepth(1)
					pass.SetPipeline(pipe)
					pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
					lightBuf.Bind(pass)
					pass.SetBindGroup(render3d.PBRLightmapMaterialGroup, matSet.Material(prim.Material).BindGroup())
					lmSet.Lightmap(prim.Material).Bind(pass)
					gpuMesh.Draw(pass)
					pass.End()
					frame.Submit()
					rendered = true
				},
			})
		}, opts...)

		app.Settle()
		app.Advance(16 * time.Millisecond)
		if drawErr != nil {
			t.Fatalf("GPUView draw error: %v", drawErr)
		}
		if !rendered {
			t.Fatal("OnFrame did not render")
		}
		img, err := app.GPUReadPixels("id:pbr-lm-view")
		if err != nil || img == nil {
			t.Fatalf("GPUReadPixels: img=%v err=%v", img, err)
		}
		return pbrLightmapCloneRGBA(img)
	}

	for _, prof := range profiles {
		t.Run(prof.name, func(t *testing.T) {
			tiltedImg := renderScene(t, tiltedDoc, prof.opts)
			flatImg := renderScene(t, flatDoc, prof.opts)

			// Quadrants on 64x64 viewport:
			//   Top-left (16, 16):     dielectric (M=0, R=0.9), AO=1.0, warm lightmap, emissive=0
			//   Bottom-left (16, 48):  dielectric (M=0, R=0.9), AO=0.3, warm lightmap, emissive=0
			//   Top-right (48, 16):    metallic (M=1, R=0.2), AO=1.0, cool lightmap (suppressed by 1-M=0), emissive=0
			//   Bottom-right (48, 48): metallic (M=1, R=0.2), AO=0.3, emissive green-cyan!
			tl := tiltedImg.RGBAAt(16, 16)
			bl := tiltedImg.RGBAAt(16, 48)
			tr := tiltedImg.RGBAAt(48, 16)
			br := tiltedImg.RGBAAt(48, 48)
			tlFlat := flatImg.RGBAAt(16, 16)
			trFlat := flatImg.RGBAAt(48, 16)

			// 1. Dielectric top-left receives warm baked lightmap (R > B).
			if !(tl.R > tl.B+30) {
				t.Errorf("dielectric top-left should be warm-lit by MOZ_lightmap (R > B+30), got %v", tl)
			}
			// 2. Occlusion map darkens bottom-left indirect light relative to top-left.
			if !(tl.R > bl.R+25 && tl.G > bl.G+20) {
				t.Errorf("occlusion map should darken bottom-left (%v) relative to top-left (%v)", bl, tl)
			}
			// 3. Metallic top-right suppresses diffuse lightmap (cool blue bounce must NOT dominate B > R) and responds to normal map perturbation on specular highlight!
			if tr.B > tr.R+10 {
				t.Errorf("metallic surface (M=1) should suppress cool blue diffuse lightmap bounce, got %v", tr)
			}
			if pbrLightmapByteDiff(tr.R, trFlat.R) < 15 && pbrLightmapByteDiff(tl.R, tlFlat.R) < 15 {
				t.Errorf("normal map did not perturb shading: tilted=(TL %v, TR %v) vs flat=(TL %v, TR %v)", tl, tr, tlFlat, trFlat)
			}
			// 4. Emissive map glows green-cyan in bottom-right relative to top-right.
			if !(br.G > tr.G+60 && br.B > tr.B+60) {
				t.Errorf("emissive map should light bottom-right green-cyan (%v) above top-right (%v)", br, tr)
			}
		})
	}
}

// TestPBRLightmapEncodingsAtlasAndDynamicLights checks RGBM, RGBD, sRGB (software & hardware), RGBA16F HDR, shared atlas UVTransform, point/spot dynamic lights, LinearOutput, AlphaMask/AlphaBlend, and Lightmap==nil fallbacks.
func TestPBRLightmapEncodingsAtlasAndDynamicLights(t *testing.T) {
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(pbrLightmapOrthoCamera(1))

	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = defaults.Close() })

	lmLayout, err := render3d.PBRLightmapBindGroupLayout(nil, dev)
	if err != nil {
		t.Fatalf("PBRLightmapBindGroupLayout: %v", err)
	}
	t.Cleanup(func() { _ = lmLayout.Close() })

	lightBuf, err := render3d.NewLightBuffer(nil, dev, render3d.LightRig{}, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	pipe, err := render3d.NewPBRLightmapPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), defaults.Layout(), lmLayout, render3d.PBRLightmapPipelineOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewPBRLightmapPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipe.Close() })

	whiteMatDesc := render3d.DefaultMaterial()
	whiteMatDesc.MetallicFactor = 0
	whiteMatDesc.RoughnessFactor = 1
	whiteMat, err := render3d.NewGPUMaterial(nil, dev, defaults, whiteMatDesc, render3d.MaterialUpload{})
	if err != nil {
		t.Fatalf("NewGPUMaterial: %v", err)
	}
	t.Cleanup(func() { _ = whiteMat.Close() })

	nearestClamp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	t.Cleanup(func() { _ = nearestClamp.Close() })

	fullQuad, err := render3d.UploadLightmapMesh(nil, dev, pbrLightmapQuad(-1, -1, 1, 1), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadLightmapMesh: %v", err)
	}
	t.Cleanup(func() { _ = fullQuad.Close() })

	renderOne := func(mat *render3d.GPUMaterial, lm *render3d.GPUPBRLightmap, mesh *render3d.GPULightmapMesh) color.RGBA {
		img := pbrLightmapRunPass(t, sr, func(p *gpu.RenderPass) {
			p.SetPipeline(pipe)
			p.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
			lightBuf.Bind(p)
			p.SetBindGroup(render3d.PBRLightmapMaterialGroup, mat.BindGroup())
			lm.Bind(p)
			mesh.Draw(p)
		})
		return img.RGBAAt(size/2, size/2)
	}

	// 1. All 4 LightmapEncoding modes (HDR 2.4, 1.2, 0.4 scaled by intensity 0.25 -> linear 0.60, 0.30, 0.10).
	hdrVal := math3d.Vec3{X: 2.4, Y: 1.2, Z: 0.4}
	rgbmImg, err := render3d.EncodeRGBMImage([]math3d.Vec3{hdrVal}, 1, 1, 8.0)
	if err != nil {
		t.Fatalf("EncodeRGBMImage: %v", err)
	}
	rgbmTex, err := render3d.UploadTexture(nil, dev, rgbmImg, render3d.TextureUpload{SRGB: false})
	if err != nil {
		t.Fatalf("UploadTexture RGBM: %v", err)
	}
	t.Cleanup(func() { _ = rgbmTex.Close() })

	rgbdImg, err := render3d.EncodeRGBDImage([]math3d.Vec3{hdrVal}, 1, 1)
	if err != nil {
		t.Fatalf("EncodeRGBDImage: %v", err)
	}
	rgbdTex, err := render3d.UploadTexture(nil, dev, rgbdImg, render3d.TextureUpload{SRGB: false})
	if err != nil {
		t.Fatalf("UploadTexture RGBD: %v", err)
	}
	t.Cleanup(func() { _ = rgbdTex.Close() })

	f16Tex, err := render3d.UploadLightmapRGBA16F(nil, dev, []math3d.Vec3{hdrVal}, 1, 1, render3d.TextureUpload{})
	if err != nil {
		t.Fatalf("UploadLightmapRGBA16F: %v", err)
	}
	t.Cleanup(func() { _ = f16Tex.Close() })

	// sRGB value encoding linear (0.60, 0.30, 0.10).
	srgbPx := color.NRGBA{
		R: pbrLightmapToByte(render3d.LinearToSRGBChannel(0.60)),
		G: pbrLightmapToByte(render3d.LinearToSRGBChannel(0.30)),
		B: pbrLightmapToByte(render3d.LinearToSRGBChannel(0.10)),
		A: 255,
	}
	srgbSoftTex, err := render3d.UploadTexture(nil, dev, pbrLightmapSolidImage(1, 1, srgbPx), render3d.TextureUpload{SRGB: false})
	if err != nil {
		t.Fatalf("UploadTexture sRGB-soft: %v", err)
	}
	t.Cleanup(func() { _ = srgbSoftTex.Close() })
	srgbHwTex, err := render3d.UploadTexture(nil, dev, pbrLightmapSolidImage(1, 1, srgbPx), render3d.TextureUpload{SRGB: true})
	if err != nil {
		t.Fatalf("UploadTexture sRGB-hw: %v", err)
	}
	t.Cleanup(func() { _ = srgbHwTex.Close() })

	encCases := []struct {
		name      string
		tex       *render3d.GPUTexture
		enc       render3d.LightmapEncoding
		intensity float32
	}{
		{"RGBM", rgbmTex, render3d.LightmapRGBM, 0.25},
		{"RGBD", rgbdTex, render3d.LightmapRGBD, 0.25},
		{"RGBA16F", f16Tex, render3d.LightmapLinear, 0.25},
		{"sRGB-software", srgbSoftTex, render3d.LightmapSRGB, 1.0},
		{"sRGB-hardware", srgbHwTex, render3d.LightmapSRGB, 1.0},
	}
	for _, tc := range encCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := render3d.DefaultPBRLightmap()
			cfg.Lightmap = tc.tex
			cfg.LightmapSampler = nearestClamp
			cfg.Encoding = tc.enc
			cfg.Intensity = tc.intensity
			glm, err := render3d.NewGPUPBRLightmap(nil, dev, cfg, render3d.PBRLightmapOptions{SharedLayout: lmLayout})
			if err != nil {
				t.Fatalf("NewGPUPBRLightmap: %v", err)
			}
			defer func() { _ = glm.Close() }()
			px := renderOne(whiteMat, glm, fullQuad)
			pbrLightmapAssertLinearByte(t, tc.name+".R", px.R, 0.60, 4)
			pbrLightmapAssertLinearByte(t, tc.name+".G", px.G, 0.30, 4)
			pbrLightmapAssertLinearByte(t, tc.name+".B", px.B, 0.10, 4)
		})
	}

	// 2. Shared atlas UVTransform + SetModel drawing two objects in one render pass.
	atlasImg := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for y := range 2 {
		atlasImg.SetNRGBA(0, y, color.NRGBA{R: 220, G: 80, B: 30, A: 255})
		atlasImg.SetNRGBA(1, y, color.NRGBA{R: 220, G: 80, B: 30, A: 255})
		atlasImg.SetNRGBA(2, y, color.NRGBA{R: 30, G: 180, B: 230, A: 255})
		atlasImg.SetNRGBA(3, y, color.NRGBA{R: 30, G: 180, B: 230, A: 255})
	}
	atlasTex, err := render3d.UploadTexture(nil, dev, atlasImg, render3d.TextureUpload{})
	if err != nil {
		t.Fatalf("UploadTexture atlas: %v", err)
	}
	t.Cleanup(func() { _ = atlasTex.Close() })

	unitHalfQuad, err := render3d.UploadLightmapMesh(nil, dev, pbrLightmapQuad(-0.5, -1, 0.5, 1), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadLightmapMesh half: %v", err)
	}
	t.Cleanup(func() { _ = unitHalfQuad.Close() })

	leftCfg := render3d.DefaultPBRLightmap()
	leftCfg.Lightmap = atlasTex
	leftCfg.LightmapSampler = nearestClamp
	leftCfg.UVTransform = render3d.UVTransform{Scale: [2]float32{0.5, 1}, Offset: [2]float32{0, 0}}
	leftLM, err := render3d.NewGPUPBRLightmap(nil, dev, leftCfg, render3d.PBRLightmapOptions{SharedLayout: lmLayout})
	if err != nil {
		t.Fatalf("NewGPUPBRLightmap left: %v", err)
	}
	t.Cleanup(func() { _ = leftLM.Close() })
	if err := leftLM.SetModel(dev, math3d.Translate(math3d.Vec3{X: -0.5})); err != nil {
		t.Fatalf("leftLM.SetModel: %v", err)
	}

	rightCfg := render3d.DefaultPBRLightmap()
	rightCfg.Lightmap = atlasTex
	rightCfg.LightmapSampler = nearestClamp
	rightCfg.UVTransform = render3d.UVTransform{Scale: [2]float32{0.5, 1}, Offset: [2]float32{0.5, 0}}
	rightLM, err := render3d.NewGPUPBRLightmap(nil, dev, rightCfg, render3d.PBRLightmapOptions{SharedLayout: lmLayout})
	if err != nil {
		t.Fatalf("NewGPUPBRLightmap right: %v", err)
	}
	t.Cleanup(func() { _ = rightLM.Close() })
	if err := rightLM.SetModel(dev, math3d.Translate(math3d.Vec3{X: 0.5})); err != nil {
		t.Fatalf("rightLM.SetModel: %v", err)
	}

	atlasOut := pbrLightmapRunPass(t, sr, func(p *gpu.RenderPass) {
		p.SetPipeline(pipe)
		p.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		lightBuf.Bind(p)
		p.SetBindGroup(render3d.PBRLightmapMaterialGroup, whiteMat.BindGroup())
		leftLM.Bind(p)
		unitHalfQuad.Draw(p)
		rightLM.Bind(p)
		unitHalfQuad.Draw(p)
	})
	leftPx := atlasOut.RGBAAt(16, 32)
	rightPx := atlasOut.RGBAAt(48, 32)
	pbrLightmapAssertLinearByte(t, "atlas.left.R", leftPx.R, 220.0/255, 3)
	pbrLightmapAssertLinearByte(t, "atlas.left.B", leftPx.B, 30.0/255, 3)
	pbrLightmapAssertLinearByte(t, "atlas.right.R", rightPx.R, 30.0/255, 3)
	pbrLightmapAssertLinearByte(t, "atlas.right.B", rightPx.B, 230.0/255, 3)

	// 3. Dynamic point + spot lights combined with baked lightmap, verified against EvaluatePBRLightmap.
	dynRig := render3d.LightRig{
		Point: []render3d.PointLight{{
			Position:  math3d.Vec3{X: 0, Y: 0, Z: 1.5},
			Color:     math3d.Vec3{X: 0.4, Y: 0.8, Z: 0.3},
			Intensity: 2.0,
			Range:     6.0,
		}},
		Spot: []render3d.SpotLight{{
			Position:       math3d.Vec3{X: 0, Y: 0, Z: 2.0},
			Direction:      math3d.Vec3{X: 0, Y: 0, Z: -1},
			Color:          math3d.Vec3{X: 0.2, Y: 0.3, Z: 0.9},
			Intensity:      2.5,
			Range:          8.0,
			InnerConeAngle: math3d.DegToRad(20),
			OuterConeAngle: math3d.DegToRad(35),
		}},
	}
	if err := lightBuf.Update(dev, dynRig); err != nil {
		t.Fatalf("lightBuf.Update point+spot: %v", err)
	}
	ptSpotLM, err := render3d.NewGPUPBRLightmap(nil, dev, render3d.PBRLightmap{
		Model:           math3d.Mat4Identity(),
		Lightmap:        srgbHwTex,
		LightmapSampler: nearestClamp,
		Encoding:        render3d.LightmapSRGB,
		Intensity:       0.4,
	}, render3d.PBRLightmapOptions{SharedLayout: lmLayout})
	if err != nil {
		t.Fatalf("NewGPUPBRLightmap pt+spot: %v", err)
	}
	t.Cleanup(func() { _ = ptSpotLM.Close() })

	gotPtSpot := renderOne(whiteMat, ptSpotLM, fullQuad)
	wantPtSpot := render3d.EvaluatePBRLightmap(render3d.PBRLightmapEvalInput{
		Material:       whiteMatDesc,
		HasLightmap:    true,
		LightmapSample: [4]float32{0.60, 0.30, 0.10, 1},
		LightmapSRGB:   true,
		Encoding:       render3d.LightmapSRGB,
		Intensity:      0.4,
		WorldPos:       math3d.Vec3Zero(),
		Normal:         math3d.Vec3{Z: 1},
		ViewDir:        math3d.Vec3{Z: 1},
		FrontFacing:    true,
		Rig:            dynRig,
	})
	pbrLightmapAssertPixelNear(t, "point+spot+lightmap", gotPtSpot, color.RGBA{
		R: pbrLightmapToByte(wantPtSpot.SRGB.X),
		G: pbrLightmapToByte(wantPtSpot.SRGB.Y),
		B: pbrLightmapToByte(wantPtSpot.SRGB.Z),
		A: 255,
	}, 4)

	// 4. Lightmap == nil fallback: uses Rig.Ambient by default, or AmbientFallback when set.
	if err := lightBuf.Update(dev, render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.2, Y: 0.5, Z: 0.7}, Intensity: 1.0},
	}); err != nil {
		t.Fatalf("lightBuf.Update fallback: %v", err)
	}
	noLM, err := render3d.NewGPUPBRLightmap(nil, dev, render3d.DefaultPBRLightmap(), render3d.PBRLightmapOptions{SharedLayout: lmLayout})
	if err != nil {
		t.Fatalf("NewGPUPBRLightmap nil-lm: %v", err)
	}
	t.Cleanup(func() { _ = noLM.Close() })

	rigAmbPx := renderOne(whiteMat, noLM, fullQuad)
	pbrLightmapAssertLinearByte(t, "noLM.rigAmbient.R", rigAmbPx.R, 0.2, 3)
	pbrLightmapAssertLinearByte(t, "noLM.rigAmbient.G", rigAmbPx.G, 0.5, 3)
	pbrLightmapAssertLinearByte(t, "noLM.rigAmbient.B", rigAmbPx.B, 0.7, 3)

	fbCfg := render3d.DefaultPBRLightmap()
	fbCfg.AmbientFallback = [3]float32{0.7, 0.2, 0.1}
	if err := noLM.Update(dev, fbCfg); err != nil {
		t.Fatalf("noLM.Update fallback: %v", err)
	}
	fbPx := renderOne(whiteMat, noLM, fullQuad)
	pbrLightmapAssertLinearByte(t, "noLM.AmbientFallback.R", fbPx.R, 0.7, 3)
	pbrLightmapAssertLinearByte(t, "noLM.AmbientFallback.G", fbPx.G, 0.2, 3)
	pbrLightmapAssertLinearByte(t, "noLM.AmbientFallback.B", fbPx.B, 0.1, 3)

	// 5. LinearOutput skips sRGB encoding.
	linPipe, err := render3d.NewPBRLightmapPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), defaults.Layout(), lmLayout, render3d.PBRLightmapPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		LinearOutput: true,
	})
	if err != nil {
		t.Fatalf("NewPBRLightmapPipeline linear: %v", err)
	}
	t.Cleanup(func() { _ = linPipe.Close() })
	linPx := pbrLightmapRunPass(t, sr, func(p *gpu.RenderPass) {
		p.SetPipeline(linPipe)
		p.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		lightBuf.Bind(p)
		p.SetBindGroup(render3d.PBRLightmapMaterialGroup, whiteMat.BindGroup())
		noLM.Bind(p)
		fullQuad.Draw(p)
	}).RGBAAt(size/2, size/2)
	pbrLightmapAssertPixelNear(t, "LinearOutput", linPx, color.RGBA{
		R: pbrLightmapToByte(0.7),
		G: pbrLightmapToByte(0.2),
		B: pbrLightmapToByte(0.1),
		A: 255,
	}, 2)
}

// TestPBRLightmapRetainedDeviceLossRecovery verifies that retained MaterialDefaults, GLTFMaterialSet, GLTFPBRLightmapSet, GPULightBuffer, GPULightmapMesh, and NewPBRLightmapPipeline survive TriggerGPUDeviceLost / RestoreGPUDevice and keep drawing identical pixels (and allow SetModel / Update post-restore).
func TestPBRLightmapRetainedDeviceLossRecovery(t *testing.T) {
	doc, err := gltf.Load(pbrLightmapGLTFWithPBRAndMOZLightmap(t, false), nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}
	prim := doc.Meshes[0].Primitives[0]
	meshData, err := render3d.LightmapMeshFromGLTF(doc, prim)
	if err != nil {
		t.Fatalf("LightmapMeshFromGLTF: %v", err)
	}

	const size = 64
	var (
		camBuf   *render3d.CameraBuffer
		defaults *render3d.MaterialDefaults
		matSet   *render3d.GLTFMaterialSet
		lmSet    *render3d.GLTFPBRLightmapSet
		lightBuf *render3d.GPULightBuffer
		gpuMesh  *render3d.GPULightmapMesh
		pipe     *gpu.Pipeline
		lastDev  *gpu.Device
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "pbr-lm-ret",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				lastDev = dev
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, dev)
				if err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				if defaults == nil {
					defaults, err = render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{
						Label:    "ret-defaults",
						Retained: true,
					})
					if err != nil {
						t.Fatalf("NewMaterialDefaults: %v", err)
					}
				}
				if matSet == nil {
					matSet, err = render3d.UploadGLTFMaterials(cx, dev, defaults, doc, render3d.GLTFMaterialUpload{
						Label:          "ret-mats",
						DisableMipmaps: true,
						Retained:       true,
					})
					if err != nil {
						t.Fatalf("UploadGLTFMaterials: %v", err)
					}
				}
				if lmSet == nil {
					lmSet, err = render3d.UploadGLTFPBRLightmaps(cx, dev, doc, render3d.GLTFPBRLightmapUpload{
						Label:    "ret-lms",
						Retained: true,
					})
					if err != nil {
						t.Fatalf("UploadGLTFPBRLightmaps: %v", err)
					}
				}
				if lightBuf == nil {
					lightBuf, err = render3d.NewLightBuffer(cx, dev, render3d.LightRig{
						Directional: []render3d.DirectionalLight{{
							Direction: math3d.Vec3{X: -0.2, Y: -0.2, Z: -0.95}.Norm(),
							Color:     math3d.Vec3{X: 1, Y: 1, Z: 1},
							Intensity: 1.0,
						}},
					}, render3d.LightBufferOptions{Label: "ret-lights", Retained: true})
					if err != nil {
						t.Fatalf("NewLightBuffer: %v", err)
					}
				}
				if gpuMesh == nil {
					gpuMesh, err = render3d.UploadLightmapMesh(cx, dev, meshData, render3d.MeshUpload{
						Label:    "ret-mesh",
						Retained: true,
					})
					if err != nil {
						t.Fatalf("UploadLightmapMesh: %v", err)
					}
				}
				pipe, err = render3d.NewPBRLightmapPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), defaults.Layout(), lmSet.Layout(), render3d.PBRLightmapPipelineOptions{
					DepthFormat: gpu.FormatDepth24Plus,
					Retained:    true,
				})
				if err != nil {
					t.Fatalf("NewPBRLightmapPipeline: %v", err)
				}
				camBuf.WriteCamera(pbrLightmapOrthoCamera(1))
			},
			OnFrame: func(frame *gpu.Frame) {
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x000000))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
				lightBuf.Bind(pass)
				pass.SetBindGroup(render3d.PBRLightmapMaterialGroup, matSet.Material(prim.Material).BindGroup())
				lmSet.Lightmap(prim.Material).Bind(pass)
				gpuMesh.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	app.Settle()
	app.Advance(16 * time.Millisecond)
	before, err := app.GPUReadPixels("id:pbr-lm-ret")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	beforeCopy := pbrLightmapCloneRGBA(before)
	if px := beforeCopy.RGBAAt(16, 16); px.R < 50 {
		t.Fatalf("expected lit pixel before loss, got %v", px)
	}

	firstLMSet := lmSet
	app.TriggerGPUDeviceLost("id:pbr-lm-ret", nsgotest.GPULostContextLost, "context lost")
	if firstLMSet.Valid() {
		t.Fatal("GLTFPBRLightmapSet should be invalid while device is lost")
	}

	app.RestoreGPUDevice("id:pbr-lm-ret")
	if lmSet != firstLMSet || !firstLMSet.Valid() {
		t.Fatalf("retained GLTFPBRLightmapSet not restored: same=%v valid=%v", lmSet == firstLMSet, firstLMSet.Valid())
	}
	// Updating post-restore with the new device must succeed.
	if err := lmSet.Lightmap(0).SetModel(lastDev, math3d.Mat4Identity()); err != nil {
		t.Fatalf("SetModel after restore: %v", err)
	}

	app.Advance(16 * time.Millisecond)
	after, err := app.GPUReadPixels("id:pbr-lm-ret")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	for y := range size {
		for x := range size {
			pbrLightmapAssertPixelNear(t, "post-restore", after.RGBAAt(x, y), beforeCopy.RGBAAt(x, y), 0)
		}
	}
}

// TestPBRLightmapValidationAndClose exercises validation errors, Close idempotence, GLTF helpers (EXT_lightmap, extras.lightmap, KHR_texture_transform, NewInstance, deduplication), and CPU evaluator edge cases.
func TestPBRLightmapValidationAndClose(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	// Validation errors wrap ErrInvalidLightmap.
	badCases := []struct {
		name string
		cfg  render3d.PBRLightmap
	}{
		{"nan-model", render3d.PBRLightmap{Model: math3d.Mat4{float32(math.NaN())}}},
		{"bad-encoding", render3d.PBRLightmap{Encoding: render3d.LightmapEncoding(99)}},
		{"neg-intensity", render3d.PBRLightmap{Intensity: -1}},
		{"neg-rgbm-range", render3d.PBRLightmap{RGBMMaxRange: -2}},
		{"nan-uv-scale", render3d.PBRLightmap{UVTransform: render3d.UVTransform{Scale: [2]float32{float32(math.NaN()), 1}}}},
		{"neg-ambient", render3d.PBRLightmap{AmbientFallback: [3]float32{0, -0.1, 0}}},
	}
	for _, tc := range badCases {
		if err := tc.cfg.Validate(); !errors.Is(err, render3d.ErrInvalidLightmap) {
			t.Errorf("%s: Validate err = %v, want ErrInvalidLightmap", tc.name, err)
		}
	}

	// RGBM/RGBD with SRGB texture must fail validation.
	srgbTex, err := render3d.UploadTexture(nil, dev, pbrLightmapSolidImage(2, 2, color.NRGBA{R: 128, G: 128, B: 128, A: 255}), render3d.TextureUpload{SRGB: true})
	if err != nil {
		t.Fatalf("UploadTexture srgb: %v", err)
	}
	t.Cleanup(func() { _ = srgbTex.Close() })
	if err := (render3d.PBRLightmap{Lightmap: srgbTex, Encoding: render3d.LightmapRGBM}).Validate(); !errors.Is(err, render3d.ErrInvalidLightmap) {
		t.Errorf("RGBM on sRGB texture err = %v, want ErrInvalidLightmap", err)
	}

	// GPUPBRLightmap Update rejects changing Lightmap pointer, and Close is idempotent.
	glm, err := render3d.NewGPUPBRLightmap(nil, dev, render3d.DefaultPBRLightmap(), render3d.PBRLightmapOptions{Label: "val-lm"})
	if err != nil {
		t.Fatalf("NewGPUPBRLightmap: %v", err)
	}
	if !glm.Valid() || glm.ByteSize() < render3d.PBRLightmapUniformSize {
		t.Fatalf("unexpected GPUPBRLightmap state: valid=%v bytes=%d", glm.Valid(), glm.ByteSize())
	}
	changed := render3d.DefaultPBRLightmap()
	changed.Lightmap = srgbTex
	if err := glm.Update(dev, changed); !errors.Is(err, render3d.ErrInvalidLightmap) {
		t.Errorf("Update changing Lightmap err = %v, want ErrInvalidLightmap", err)
	}
	if err := glm.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	if glm.Valid() {
		t.Fatal("GPUPBRLightmap still valid after Close")
	}
	if err := glm.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}

	// GLTFPBRLightmapSet: EXT_lightmap + extras.lightmap + KHR_texture_transform + deduplication + NewInstance + Close.
	lmURI := pbrLightmapEncodePNGDataURI(t, pbrLightmapSolidImage(4, 4, color.NRGBA{R: 180, G: 140, B: 100, A: 255}))
	docJSON, err := json.Marshal(map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"images": []map[string]any{
			{"name": "shared_lm", "uri": lmURI, "mimeType": "image/png"},
		},
		"textures": []map[string]any{
			{"source": 0},
		},
		"materials": []map[string]any{
			{
				"name": "mat_ext",
				"extensions": map[string]any{
					gltf.ExtLightmap: map[string]any{
						"index":     0,
						"intensity": 1.25,
						"extensions": map[string]any{
							"KHR_texture_transform": map[string]any{
								"scale":  []float32{0.5, 0.5},
								"offset": []float32{0.25, 0.0},
							},
						},
					},
				},
			},
			{
				"name": "mat_extras",
				"extras": map[string]any{
					"lightmap": map[string]any{
						"texture":     0,
						"scaleOffset": []float32{0.5, 0.5, 0.5, 0.5},
						"encoding":    "srgb",
					},
				},
			},
			{
				"name": "mat_plain",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	doc, err := gltf.Load(docJSON, nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}
	set, err := render3d.UploadGLTFPBRLightmaps(nil, dev, doc, render3d.GLTFPBRLightmapUpload{Label: "ext-test"})
	if err != nil {
		t.Fatalf("UploadGLTFPBRLightmaps: %v", err)
	}
	if !set.Valid() || set.Len() != 3 {
		t.Fatalf("set valid=%v len=%d, want true, 3", set.Valid(), set.Len())
	}
	// Image 0 uploaded once linear (mat 0) and once sRGB (mat 1) => 2 unique GPU textures.
	if set.TextureCount() != 2 {
		t.Errorf("TextureCount = %d, want 2", set.TextureCount())
	}
	info0, ok := set.Info(0)
	if !ok || !info0.HasLightmap || info0.Config.UVTransform.Scale != [2]float32{0.5, 0.5} || info0.Config.UVTransform.Offset != [2]float32{0.25, 0} || info0.Config.Intensity != 1.25 {
		t.Errorf("info0 = %+v (ok=%v)", info0, ok)
	}
	info1, ok := set.Info(1)
	if !ok || !info1.HasLightmap || info1.Encoding != render3d.LightmapSRGB || info1.Config.UVTransform.Offset != [2]float32{0.5, 0.5} {
		t.Errorf("info1 = %+v (ok=%v)", info1, ok)
	}
	info2, ok := set.Info(2)
	if !ok || info2.HasLightmap || set.Lightmap(2) == nil || set.Lightmap(-1) != set.DefaultLightmap() {
		t.Errorf("info2 = %+v (ok=%v)", info2, ok)
	}

	inst, err := set.NewInstance(nil, dev, 0, math3d.Translate(math3d.Vec3{X: 3}), render3d.PBRLightmapOptions{Label: "inst"})
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	if !inst.Valid() || inst.Config().Model != math3d.Translate(math3d.Vec3{X: 3}) {
		t.Fatalf("unexpected instance state: valid=%v model=%v", inst.Valid(), inst.Config().Model)
	}
	if err := inst.Close(); err != nil {
		t.Fatalf("inst.Close: %v", err)
	}
	if err := set.Close(); err != nil {
		t.Fatalf("set.Close 1: %v", err)
	}
	if set.Valid() {
		t.Fatal("GLTFPBRLightmapSet still valid after Close")
	}
	if err := set.Close(); err != nil {
		t.Fatalf("set.Close 2: %v", err)
	}

	// CPU evaluator AlphaMask discard + AlphaBlend alpha passthrough + DoubleSided normal flip.
	maskMat := render3d.DefaultMaterial()
	maskMat.AlphaMode = render3d.AlphaMask
	maskMat.AlphaCutoff = 0.5
	maskMat.BaseColorFactor = [4]float32{1, 1, 1, 0.25}
	if res := render3d.EvaluatePBRLightmap(render3d.PBRLightmapEvalInput{Material: maskMat}); !res.Discarded {
		t.Error("expected AlphaMask fragment below cutoff to be discarded")
	}

	blendMat := render3d.DefaultMaterial()
	blendMat.AlphaMode = render3d.AlphaBlend
	blendMat.DoubleSided = true
	blendMat.BaseColorFactor = [4]float32{1, 1, 1, 0.4}
	blendRes := render3d.EvaluatePBRLightmap(render3d.PBRLightmapEvalInput{
		Material:    blendMat,
		Normal:      math3d.Vec3{Z: 1},
		ViewDir:     math3d.Vec3{Z: -1},
		FrontFacing: false,
	})
	if blendRes.Discarded || math.Abs(float64(blendRes.SRGB.W-0.4)) > 1e-4 || blendRes.Normal.Z > -0.99 {
		t.Errorf("unexpected blend/double-sided eval: %+v", blendRes)
	}
}
