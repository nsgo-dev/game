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

func lightmapTestEncodePNGDataURI(t *testing.T, img image.Image) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// lightmapTestBakedSceneGLTF builds a self-contained glTF 2.0 scene containing
// a quad with distinct TEXCOORD_0 (tiled 4x2 albedo checkerboard) and
// TEXCOORD_1 (non-overlapping [0,1]x[0,1] baked lightmap chart with warm
// sunlit left half and cool shadowed right half), referenced via MOZ_lightmap.
func lightmapTestBakedSceneGLTF(t *testing.T) []byte {
	t.Helper()

	// Albedo texture: 2x2 high-contrast neutral checkerboard (0.5 and 1.0) so
	// tiled UV0 modulation is clearly distinguishable from UV1 lightmap zones.
	albedoImg := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	albedoImg.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	albedoImg.SetNRGBA(1, 0, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	albedoImg.SetNRGBA(0, 1, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	albedoImg.SetNRGBA(1, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})

	// Baked lightmap: 4x2 linear irradiance map.
	// Left half (x=0,1): warm sunlight (R=250, G=210, B=130).
	// Right half (x=2,3): cool shadow (R=45, G=65, B=130).
	lmImg := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for y := range 2 {
		lmImg.SetNRGBA(0, y, color.NRGBA{R: 250, G: 210, B: 130, A: 255})
		lmImg.SetNRGBA(1, y, color.NRGBA{R: 250, G: 210, B: 130, A: 255})
		lmImg.SetNRGBA(2, y, color.NRGBA{R: 45, G: 65, B: 130, A: 255})
		lmImg.SetNRGBA(3, y, color.NRGBA{R: 45, G: 65, B: 130, A: 255})
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

	// Facing +Z quad spanning [-1, 1] x [-1, 1] at z = 0.
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
	// TEXCOORD_0: tiled 4x across X and 2x across Y.
	uv0Off, uv0Len := appendF32(
		0, 2,
		4, 2,
		4, 0,
		0, 0,
	)
	// TEXCOORD_1: unique non-overlapping [0, 1] x [0, 1] lightmap chart.
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
		"images": []map[string]any{
			{"uri": lightmapTestEncodePNGDataURI(t, albedoImg), "mimeType": "image/png"},
			{"uri": lightmapTestEncodePNGDataURI(t, lmImg), "mimeType": "image/png"},
		},
		"textures": []map[string]any{
			{"source": 0},
			{"source": 1},
		},
		"materials": []map[string]any{{
			"name": "baked_wall",
			"pbrMetallicRoughness": map[string]any{
				"baseColorFactor":  []float32{1, 1, 1, 1},
				"baseColorTexture": map[string]any{"index": 0, "texCoord": 0},
			},
			"extensions": map[string]any{
				gltf.ExtMOZLightmap: map[string]any{
					"index":     1,
					"texCoord":  1,
					"intensity": 1.0,
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
		t.Fatalf("marshal baked glTF: %v", err)
	}
	return raw
}

func lightmapTestOrthoCamera(aspect float32) *scene3d.Camera {
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{X: 0, Y: 0, Z: 2}, 2, aspect, 0.1, 10)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	return cam
}

func lightmapTestUnitQuad(x0, y0, x1, y1 float32) render3d.LightmapMesh {
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

func lightmapTestApproxByte(t *testing.T, label string, got, want uint8, tol int) {
	t.Helper()
	diff := int(got) - int(want)
	if diff < 0 {
		diff = -diff
	}
	if diff > tol {
		t.Errorf("%s = %d, want %d (±%d)", label, got, want, tol)
	}
}

func TestLightmapBakedGLTFSceneDrawsInGPUView(t *testing.T) {
	raw := lightmapTestBakedSceneGLTF(t)
	doc, err := gltf.Load(raw, nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}
	prim := doc.Meshes[0].Primitives[0]
	lmRef, err := doc.MaterialLightmap(prim.Material)
	if err != nil || lmRef == nil {
		t.Fatalf("MaterialLightmap: ref=%+v err=%v", lmRef, err)
	}
	lmUVs, usedFallback, err := doc.LightmapTexCoords(prim, lmRef)
	if err != nil || usedFallback {
		t.Fatalf("LightmapTexCoords: fallback=%v err=%v", usedFallback, err)
	}

	mat := doc.Materials[prim.Material]
	albedoImg, err := doc.DecodeImage(doc.Textures[mat.BaseColorTexture.TextureIndex].Source)
	if err != nil {
		t.Fatalf("DecodeImage albedo: %v", err)
	}
	lmImg, err := doc.DecodeImage(doc.Textures[lmRef.Texture.TextureIndex].Source)
	if err != nil {
		t.Fatalf("DecodeImage lightmap: %v", err)
	}

	lmMeshData := render3d.LightmapMeshFromAttributes(
		prim.Positions,
		prim.Normals,
		prim.TexCoords0,
		lmUVs,
		prim.Indices,
	)

	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"no-compute-webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	for _, prof := range profiles {
		t.Run(prof.name, func(t *testing.T) {
			var (
				rendered bool
				drawErr  error
			)
			opts := append([]nsgotest.Option{nsgotest.Size(64, 64), nsgotest.ManualFrames()}, prof.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				var (
					camBuf    *render3d.CameraBuffer
					renderer  *render3d.LightmapRenderer
					gpuMesh   *render3d.GPULightmapMesh
					albedoTex *render3d.GPUTexture
					lmTex     *render3d.GPUTexture
					albedoSmp *render3d.GPUSampler
					lmSmp     *render3d.GPUSampler
				)
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "lightmap-viewport",
					Style:       core.Style{Width: core.Pt(64), Height: core.Pt(64)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						var err error
						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							drawErr = err
							return
						}
						renderer, err = render3d.NewLightmapRenderer(cx, dev, camBuf.Layout(), render3d.LightmapRendererOptions{
							DepthFormat: gpu.FormatDepth24Plus,
							Retained:    true,
						})
						if err != nil {
							drawErr = err
							return
						}
						gpuMesh, err = render3d.UploadLightmapMesh(cx, dev, lmMeshData, render3d.MeshUpload{
							Label:    "baked-wall",
							Retained: true,
						})
						if err != nil {
							drawErr = err
							return
						}
						albedoTex, err = render3d.UploadTexture(cx, dev, albedoImg, render3d.TextureUpload{
							Label:    "wall-albedo",
							Retained: true,
						})
						if err != nil {
							drawErr = err
							return
						}
						lmTex, err = render3d.UploadTexture(cx, dev, lmImg, render3d.TextureUpload{
							Label:    "wall-lightmap",
							Retained: true,
						})
						if err != nil {
							drawErr = err
							return
						}
						albedoSmp, err = render3d.NewGPUSampler(cx, dev, render3d.SamplerNearestRepeat())
						if err != nil {
							drawErr = err
							return
						}
						lmSmp, err = render3d.NewGPUSampler(cx, dev, render3d.SamplerNearestClamp())
						if err != nil {
							drawErr = err
							return
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						if renderer == nil || gpuMesh == nil {
							return
						}
						renderer.BeginFrame()
						camBuf.WriteCamera(lightmapTestOrthoCamera(1))
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x000000))
						pass.ClearDepth(1)
						lmMat := render3d.DefaultLightmapMaterial()
						lmMat.BaseColorTexture = albedoTex
						lmMat.BaseColorSampler = albedoSmp
						lmMat.Lightmap = lmTex
						lmMat.LightmapSampler = lmSmp
						lmMat.Encoding = render3d.LightmapLinear
						lmMat.Intensity = lmRef.Intensity
						lmMat.UVScale = lmRef.UVScale
						lmMat.UVOffset = lmRef.UVOffset
						if err := renderer.Draw(pass, camBuf, gpuMesh, math3d.Mat4Identity(), lmMat); err != nil {
							drawErr = err
						}
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
				t.Fatal("OnFrame did not render baked scene")
			}

			img, err := app.GPUReadPixels("id:lightmap-viewport")
			if err != nil || img == nil {
				t.Fatalf("GPUReadPixels: img=%v err=%v", img, err)
			}

			// Verify TEXCOORD_0 checkerboard tiles and TEXCOORD_1 warm-left vs cool-right baked lighting:
			// With 4x2 repeating 2x2 checker over 64x64 viewport, each checker cell is 8x16 pixels:
			// - (4, 8): left half (warm light 250,210,130) * white tile (1.0) -> ~(250, 210, 130)
			// - (12, 8): left half (warm light 250,210,130) * gray tile (0.5) -> ~(125, 105, 65)
			// - (36, 8): right half (cool shadow 45,65,130) * white tile (1.0) -> ~(45, 65, 130)
			// - (44, 8): right half (cool shadow 45,65,130) * gray tile (0.5) -> ~(22, 32, 65)
			sunWhite := img.RGBAAt(4, 8)
			sunGray := img.RGBAAt(12, 8)
			shadWhite := img.RGBAAt(36, 8)
			shadGray := img.RGBAAt(44, 8)

			lightmapTestApproxByte(t, "sunWhite.R", sunWhite.R, 250, 4)
			lightmapTestApproxByte(t, "sunWhite.G", sunWhite.G, 210, 4)
			lightmapTestApproxByte(t, "sunWhite.B", sunWhite.B, 130, 4)

			lightmapTestApproxByte(t, "sunGray.R", sunGray.R, 125, 4)
			lightmapTestApproxByte(t, "sunGray.G", sunGray.G, 105, 4)
			lightmapTestApproxByte(t, "sunGray.B", sunGray.B, 65, 4)

			lightmapTestApproxByte(t, "shadWhite.R", shadWhite.R, 45, 4)
			lightmapTestApproxByte(t, "shadWhite.G", shadWhite.G, 65, 4)
			lightmapTestApproxByte(t, "shadWhite.B", shadWhite.B, 130, 4)

			lightmapTestApproxByte(t, "shadGray.R", shadGray.R, 22, 4)
			lightmapTestApproxByte(t, "shadGray.G", shadGray.G, 32, 4)
			lightmapTestApproxByte(t, "shadGray.B", shadGray.B, 65, 4)

			if !(sunWhite.R > sunWhite.B && shadWhite.B > shadWhite.R) {
				t.Errorf("expected warm sunlit left (R>B) and cool shadowed right (B>R), got sun=%v shad=%v", sunWhite, shadWhite)
			}
		})
	}
}

func TestLightmapSharedAtlasUVTransform(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(64, 32, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	renderer, err := render3d.NewLightmapRenderer(nil, dev, camBuf.Layout(), render3d.LightmapRendererOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewLightmapRenderer: %v", err)
	}
	t.Cleanup(func() { _ = renderer.Close() })

	// One 4x2 shared lightmap atlas: left half warm red-orange, right half cool green-blue.
	atlasImg := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for y := range 2 {
		atlasImg.SetNRGBA(0, y, color.NRGBA{R: 230, G: 110, B: 30, A: 255})
		atlasImg.SetNRGBA(1, y, color.NRGBA{R: 230, G: 110, B: 30, A: 255})
		atlasImg.SetNRGBA(2, y, color.NRGBA{R: 40, G: 190, B: 220, A: 255})
		atlasImg.SetNRGBA(3, y, color.NRGBA{R: 40, G: 190, B: 220, A: 255})
	}
	atlasTex, err := render3d.UploadTexture(nil, dev, atlasImg, render3d.TextureUpload{Label: "shared-atlas"})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}
	t.Cleanup(func() { _ = atlasTex.Close() })

	nearestClamp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	t.Cleanup(func() { _ = nearestClamp.Close() })

	// Two meshes side by side, both with full [0, 1] UV1, selecting distinct sub-rectangles of atlasTex.
	leftMesh, err := render3d.UploadLightmapMesh(nil, dev, lightmapTestUnitQuad(-1, -1, 0, 1), render3d.MeshUpload{Label: "mesh-left"})
	if err != nil {
		t.Fatalf("UploadLightmapMesh left: %v", err)
	}
	t.Cleanup(func() { _ = leftMesh.Close() })

	rightMesh, err := render3d.UploadLightmapMesh(nil, dev, lightmapTestUnitQuad(0, -1, 1, 1), render3d.MeshUpload{Label: "mesh-right"})
	if err != nil {
		t.Fatalf("UploadLightmapMesh right: %v", err)
	}
	t.Cleanup(func() { _ = rightMesh.Close() })

	renderer.BeginFrame()
	// orthoHeight=2, aspect=1 spans X in [-1, 1], Y in [-1, 1] across the 64x32 viewport.
	camBuf.WriteCamera(lightmapTestOrthoCamera(1))

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

	matLeft := render3d.DefaultLightmapMaterial()
	matLeft.Lightmap = atlasTex
	matLeft.LightmapSampler = nearestClamp
	matLeft.SetUVTransform(render3d.UVTransform{Scale: [2]float32{0.5, 1.0}, Offset: [2]float32{0.0, 0.0}})

	matRight := render3d.DefaultLightmapMaterial()
	matRight.Lightmap = atlasTex
	matRight.LightmapSampler = nearestClamp
	matRight.SetUVTransform(render3d.UVTransform{Scale: [2]float32{0.5, 1.0}, Offset: [2]float32{0.5, 0.0}})

	if err := renderer.Draw(pass, camBuf, leftMesh, math3d.Mat4Identity(), matLeft); err != nil {
		t.Fatalf("Draw left: %v", err)
	}
	if err := renderer.Draw(pass, camBuf, rightMesh, math3d.Mat4Identity(), matRight); err != nil {
		t.Fatalf("Draw right: %v", err)
	}
	pass.End()
	dev.Submit(enc.Finish())
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	pxLeft := img.RGBAAt(16, 16)
	pxRight := img.RGBAAt(48, 16)

	lightmapTestApproxByte(t, "atlas left R", pxLeft.R, 230, 3)
	lightmapTestApproxByte(t, "atlas left G", pxLeft.G, 110, 3)
	lightmapTestApproxByte(t, "atlas left B", pxLeft.B, 30, 3)

	lightmapTestApproxByte(t, "atlas right R", pxRight.R, 40, 3)
	lightmapTestApproxByte(t, "atlas right G", pxRight.G, 190, 3)
	lightmapTestApproxByte(t, "atlas right B", pxRight.B, 220, 3)
}

func TestLightmapEncodingsCPUAndGPU(t *testing.T) {
	// 1. CPU RGBM / RGBD round-trip checks across LDR and HDR (> 1.0) values.
	samples := []math3d.Vec3{
		{X: 0, Y: 0, Z: 0},
		{X: 0.25, Y: 0.5, Z: 0.75},
		{X: 2.4, Y: 1.6, Z: 0.8},
		{X: 6.0, Y: 3.5, Z: 1.2},
	}
	for _, s := range samples {
		encM := render3d.EncodeRGBM(s, 8.0)
		decM := render3d.DecodeRGBM(encM, 8.0)
		if math.Abs(float64(decM.X-s.X)) > 0.06 || math.Abs(float64(decM.Y-s.Y)) > 0.06 || math.Abs(float64(decM.Z-s.Z)) > 0.06 {
			t.Errorf("RGBM round-trip(%v) = %v", s, decM)
		}
		encD := render3d.EncodeRGBD(s)
		decD := render3d.DecodeRGBD(encD)
		if math.Abs(float64(decD.X-s.X)) > 0.06 || math.Abs(float64(decD.Y-s.Y)) > 0.06 || math.Abs(float64(decD.Z-s.Z)) > 0.06 {
			t.Errorf("RGBD round-trip(%v) = %v", s, decD)
		}
	}

	// 2. GPU rendering checks for Linear, RGBA16F HDR, sRGB (software decode),
	// sRGB (hardware FormatRGBA8UnormSRGB), RGBM HDR, and RGBD HDR.
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(lightmapTestOrthoCamera(1))

	renderer, err := render3d.NewLightmapRenderer(nil, dev, camBuf.Layout(), render3d.LightmapRendererOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewLightmapRenderer: %v", err)
	}
	t.Cleanup(func() { _ = renderer.Close() })

	quad, err := render3d.UploadLightmapMesh(nil, dev, lightmapTestUnitQuad(-1, -1, 1, 1), render3d.MeshUpload{Label: "enc-quad"})
	if err != nil {
		t.Fatalf("UploadLightmapMesh: %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	nearestClamp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	t.Cleanup(func() { _ = nearestClamp.Close() })

	renderOne := func(mat render3d.LightmapMaterial) color.RGBA {
		renderer.BeginFrame()
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
		if err := renderer.Draw(pass, camBuf, quad, math3d.Mat4Identity(), mat); err != nil {
			t.Fatalf("Draw: %v", err)
		}
		pass.End()
		dev.Submit(enc.Finish())
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		return sr.Image().RGBAAt(8, 8)
	}

	// BaseColor = 0.25 so HDR irradiance > 1.0 stays cleanly inside [0, 1] output range.
	base := [4]float32{0.25, 0.25, 0.25, 1.0}

	// 2a. Linear 8-bit: irradiance (0.8, 0.4, 0.2) * 0.25 = (0.20, 0.10, 0.05) -> (51, 26, 13).
	linTex, err := render3d.UploadTexturePixels(nil, dev, render3d.PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm, Pixels: []byte{204, 102, 51, 255},
	}, render3d.TextureUpload{Label: "lm-linear"})
	if err != nil {
		t.Fatalf("UploadTexturePixels linear: %v", err)
	}
	t.Cleanup(func() { _ = linTex.Close() })
	pxLin := renderOne(render3d.LightmapMaterial{
		BaseColor: base, Lightmap: linTex, LightmapSampler: nearestClamp, Encoding: render3d.LightmapLinear, Intensity: 1,
	})
	lightmapTestApproxByte(t, "linear R", pxLin.R, 51, 3)
	lightmapTestApproxByte(t, "linear G", pxLin.G, 26, 3)
	lightmapTestApproxByte(t, "linear B", pxLin.B, 13, 3)

	// 2b. Linear HDR RGBA16F: irradiance (2.4, 1.6, 0.8) * 0.25 = (0.60, 0.40, 0.20) -> (153, 102, 51).
	hdrTex, err := render3d.UploadLightmapRGBA16F(nil, dev, []math3d.Vec3{{X: 2.4, Y: 1.6, Z: 0.8}}, 1, 1, render3d.TextureUpload{Label: "lm-hdr16f"})
	if err != nil {
		t.Fatalf("UploadLightmapRGBA16F: %v", err)
	}
	t.Cleanup(func() { _ = hdrTex.Close() })
	pxHDR := renderOne(render3d.LightmapMaterial{
		BaseColor: base, Lightmap: hdrTex, LightmapSampler: nearestClamp, Encoding: render3d.LightmapLinear, Intensity: 1,
	})
	lightmapTestApproxByte(t, "rgba16f R", pxHDR.R, 153, 3)
	lightmapTestApproxByte(t, "rgba16f G", pxHDR.G, 102, 3)
	lightmapTestApproxByte(t, "rgba16f B", pxHDR.B, 51, 3)

	// 2c. sRGB software decode: sRGB byte 188 (~0.737) -> linear ~0.50; * 0.25 = 0.125 -> ~32.
	// Contrast with raw linear 188/255 * 0.25 = 0.184 -> 47, confirming sRGB decode ran!
	srgbSoftTex, err := render3d.UploadTexturePixels(nil, dev, render3d.PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm, Pixels: []byte{188, 188, 188, 255},
	}, render3d.TextureUpload{Label: "lm-srgb-soft", SRGB: false})
	if err != nil {
		t.Fatalf("UploadTexturePixels srgb soft: %v", err)
	}
	t.Cleanup(func() { _ = srgbSoftTex.Close() })
	pxSRGBSoft := renderOne(render3d.LightmapMaterial{
		BaseColor: base, Lightmap: srgbSoftTex, LightmapSampler: nearestClamp, Encoding: render3d.LightmapSRGB, Intensity: 1,
	})
	lightmapTestApproxByte(t, "srgb-soft R", pxSRGBSoft.R, 32, 3)

	// 2d. sRGB hardware texture (SRGB: true): must match sRGB software decode (no double-decode!).
	srgbHWTex, err := render3d.UploadTexturePixels(nil, dev, render3d.PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8UnormSRGB, Pixels: []byte{188, 188, 188, 255},
	}, render3d.TextureUpload{Label: "lm-srgb-hw", SRGB: true})
	if err != nil {
		t.Fatalf("UploadTexturePixels srgb hw: %v", err)
	}
	t.Cleanup(func() { _ = srgbHWTex.Close() })
	pxSRGBHW := renderOne(render3d.LightmapMaterial{
		BaseColor: base, Lightmap: srgbHWTex, LightmapSampler: nearestClamp, Encoding: render3d.LightmapSRGB, Intensity: 1,
	})
	lightmapTestApproxByte(t, "srgb-hw R", pxSRGBHW.R, 32, 3)

	// 2e. RGBM HDR (MaxRange=8.0): HDR irradiance (3.2, 2.0, 0.8) * 0.25 = (0.80, 0.50, 0.20) -> (204, 128, 51).
	rgbmImg, err := render3d.EncodeRGBMImage([]math3d.Vec3{{X: 3.2, Y: 2.0, Z: 0.8}}, 1, 1, 8.0)
	if err != nil {
		t.Fatalf("EncodeRGBMImage: %v", err)
	}
	rgbmTex, err := render3d.UploadTexture(nil, dev, rgbmImg, render3d.TextureUpload{Label: "lm-rgbm", SRGB: false})
	if err != nil {
		t.Fatalf("UploadTexture rgbm: %v", err)
	}
	t.Cleanup(func() { _ = rgbmTex.Close() })
	pxRGBM := renderOne(render3d.LightmapMaterial{
		BaseColor: base, Lightmap: rgbmTex, LightmapSampler: nearestClamp, Encoding: render3d.LightmapRGBM, RGBMMaxRange: 8.0, Intensity: 1,
	})
	lightmapTestApproxByte(t, "rgbm R", pxRGBM.R, 204, 5)
	lightmapTestApproxByte(t, "rgbm G", pxRGBM.G, 128, 5)
	lightmapTestApproxByte(t, "rgbm B", pxRGBM.B, 51, 5)

	// 2f. RGBD HDR: HDR irradiance (3.0, 1.5, 0.6) * 0.25 = (0.75, 0.375, 0.15) -> (191, 96, 38).
	rgbdImg, err := render3d.EncodeRGBDImage([]math3d.Vec3{{X: 3.0, Y: 1.5, Z: 0.6}}, 1, 1)
	if err != nil {
		t.Fatalf("EncodeRGBDImage: %v", err)
	}
	rgbdTex, err := render3d.UploadTexture(nil, dev, rgbdImg, render3d.TextureUpload{Label: "lm-rgbd", SRGB: false})
	if err != nil {
		t.Fatalf("UploadTexture rgbd: %v", err)
	}
	t.Cleanup(func() { _ = rgbdTex.Close() })
	pxRGBD := renderOne(render3d.LightmapMaterial{
		BaseColor: base, Lightmap: rgbdTex, LightmapSampler: nearestClamp, Encoding: render3d.LightmapRGBD, Intensity: 1,
	})
	lightmapTestApproxByte(t, "rgbd R", pxRGBD.R, 191, 5)
	lightmapTestApproxByte(t, "rgbd G", pxRGBD.G, 96, 5)
	lightmapTestApproxByte(t, "rgbd B", pxRGBD.B, 38, 5)
}

func TestLightmapAmbientFallbackDirectLightAndUV0Fallback(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(32, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(lightmapTestOrthoCamera(1))

	// Start with InitialCapacity=1 to also exercise dynamic ring buffer growth in one render pass.
	renderer, err := render3d.NewLightmapRenderer(nil, dev, camBuf.Layout(), render3d.LightmapRendererOptions{
		ColorFormat:     gpu.FormatRGBA8Unorm,
		DepthFormat:     gpu.FormatDepth24Plus,
		InitialCapacity: 1,
	})
	if err != nil {
		t.Fatalf("NewLightmapRenderer: %v", err)
	}
	t.Cleanup(func() { _ = renderer.Close() })

	// Left mesh: NO UV1 provided -> gracefully falls back to UV0!
	noUV1Mesh := lightmapTestUnitQuad(-1, -1, 0, 1)
	noUV1Mesh.UV1 = nil
	gpuLeft, err := render3d.UploadLightmapMesh(nil, dev, noUV1Mesh, render3d.MeshUpload{Label: "fallback-uv0"})
	if err != nil {
		t.Fatalf("UploadLightmapMesh fallback: %v", err)
	}
	t.Cleanup(func() { _ = gpuLeft.Close() })
	if !gpuLeft.UsedUV0Fallback() {
		t.Fatal("UsedUV0Fallback() = false, want true when UV1 is absent")
	}

	// Right mesh: built from LightmapVertex slice directly.
	verts := []render3d.LightmapVertex{
		{Position: [3]float32{0, -1, 0}, Normal: [3]float32{0, 0, 1}, UV0: [2]float32{0, 1}, UV1: [2]float32{0, 1}},
		{Position: [3]float32{1, -1, 0}, Normal: [3]float32{0, 0, 1}, UV0: [2]float32{1, 1}, UV1: [2]float32{1, 1}},
		{Position: [3]float32{1, 1, 0}, Normal: [3]float32{0, 0, 1}, UV0: [2]float32{1, 0}, UV1: [2]float32{1, 0}},
		{Position: [3]float32{0, 1, 0}, Normal: [3]float32{0, 0, 1}, UV0: [2]float32{0, 0}, UV1: [2]float32{0, 0}},
	}
	gpuRight, err := render3d.UploadLightmapVertices(nil, dev, verts, []uint32{0, 1, 2, 0, 2, 3}, render3d.MeshUpload{Label: "from-verts"})
	if err != nil {
		t.Fatalf("UploadLightmapVertices: %v", err)
	}
	t.Cleanup(func() { _ = gpuRight.Close() })
	if gpuRight.UsedUV0Fallback() {
		t.Fatal("UsedUV0Fallback() = true for explicit LightmapVertex slice")
	}

	lmTex, err := render3d.UploadTexturePixels(nil, dev, render3d.PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm, Pixels: []byte{102, 51, 26, 255}, // ~0.4, 0.2, 0.1
	}, render3d.TextureUpload{Label: "baked-indirect"})
	if err != nil {
		t.Fatalf("UploadTexturePixels: %v", err)
	}
	t.Cleanup(func() { _ = lmTex.Close() })

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

	// Draw 1 (left): UV0 fallback sampling lmTex + additive directional light along +Z (N·L = 1).
	// Expected: (0.4+0.2, 0.2+0.4, 0.1+0.5) = (0.6, 0.6, 0.6) -> ~(153, 153, 153).
	matLeft := render3d.DefaultLightmapMaterial()
	matLeft.Lightmap = lmTex
	matLeft.DirectLight = render3d.LightmapDirectLight{
		Direction: math3d.Vec3{Z: 1},
		Color:     math3d.Vec3{X: 0.2, Y: 0.4, Z: 0.5},
		Intensity: 1.0,
	}
	if err := renderer.Draw(pass, camBuf, gpuLeft, math3d.Mat4Identity(), matLeft); err != nil {
		t.Fatalf("Draw left: %v", err)
	}

	// Draw 2 (right, forces ring growth from capacity 1 -> 2): nil Lightmap using AmbientFallback.
	// Expected: (0.2, 0.6, 0.8) -> ~(51, 153, 204).
	matRight := render3d.DefaultLightmapMaterial()
	matRight.AmbientFallback = [3]float32{0.2, 0.6, 0.8}
	if err := renderer.Draw(pass, camBuf, gpuRight, math3d.Mat4Identity(), matRight); err != nil {
		t.Fatalf("Draw right: %v", err)
	}

	pass.End()
	dev.Submit(enc.Finish())
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	pxL := img.RGBAAt(8, 8)
	pxR := img.RGBAAt(24, 8)
	lightmapTestApproxByte(t, "baked+direct R", pxL.R, 153, 4)
	lightmapTestApproxByte(t, "baked+direct G", pxL.G, 153, 4)
	lightmapTestApproxByte(t, "baked+direct B", pxL.B, 153, 4)

	lightmapTestApproxByte(t, "ambient fallback R", pxR.R, 51, 4)
	lightmapTestApproxByte(t, "ambient fallback G", pxR.G, 153, 4)
	lightmapTestApproxByte(t, "ambient fallback B", pxR.B, 204, 4)

	// Calling BeginFrame cleanly releases retired buffers.
	renderer.BeginFrame()
}

func TestLightmapValidationErrors(t *testing.T) {
	nan := float32(math.NaN())

	// Mesh validation errors wrap ErrInvalidMesh.
	badMeshes := []struct {
		name string
		mesh render3d.LightmapMesh
	}{
		{"empty", render3d.LightmapMesh{}},
		{
			"mismatched_uv1",
			render3d.LightmapMesh{
				Positions: []math3d.Vec3{{}, {X: 1}, {Y: 1}},
				UV1:       []math3d.Vec2{{}},
			},
		},
		{
			"nan_uv1",
			render3d.LightmapMesh{
				Positions: []math3d.Vec3{{}, {X: 1}, {Y: 1}},
				UV1:       []math3d.Vec2{{X: nan}, {}, {}},
			},
		},
	}
	for _, tc := range badMeshes {
		if err := tc.mesh.Validate(); !errors.Is(err, render3d.ErrInvalidMesh) {
			t.Errorf("LightmapMesh(%s).Validate() = %v, want ErrInvalidMesh", tc.name, err)
		}
	}

	// Material validation errors wrap ErrInvalidLightmap.
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	srgbTex, err := render3d.UploadTexturePixels(nil, dev, render3d.PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8UnormSRGB, Pixels: []byte{128, 128, 128, 255},
	}, render3d.TextureUpload{SRGB: true})
	if err != nil {
		t.Fatalf("UploadTexturePixels: %v", err)
	}
	t.Cleanup(func() { _ = srgbTex.Close() })

	closedTex, err := render3d.UploadTexturePixels(nil, dev, render3d.PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm, Pixels: []byte{255, 255, 255, 255},
	}, render3d.TextureUpload{})
	if err != nil {
		t.Fatalf("UploadTexturePixels: %v", err)
	}
	_ = closedTex.Close()

	badMats := []struct {
		name string
		mat  render3d.LightmapMaterial
	}{
		{"negative_intensity", render3d.LightmapMaterial{Intensity: -1}},
		{"nan_uv_scale", render3d.LightmapMaterial{UVScale: [2]float32{nan, 1}}},
		{"unknown_encoding", render3d.LightmapMaterial{Encoding: render3d.LightmapEncoding(99)}},
		{"rgbm_with_srgb_tex", render3d.LightmapMaterial{Encoding: render3d.LightmapRGBM, Lightmap: srgbTex}},
		{"rgbd_with_srgb_tex", render3d.LightmapMaterial{Encoding: render3d.LightmapRGBD, Lightmap: srgbTex}},
		{"closed_lightmap_tex", render3d.LightmapMaterial{Lightmap: closedTex}},
		{"negative_direct_intensity", render3d.LightmapMaterial{DirectLight: render3d.LightmapDirectLight{Intensity: -0.5}}},
	}
	for _, tc := range badMats {
		if err := tc.mat.Validate(); !errors.Is(err, render3d.ErrInvalidLightmap) {
			t.Errorf("LightmapMaterial(%s).Validate() = %v, want ErrInvalidLightmap", tc.name, err)
		}
	}

	if _, err := render3d.ParseLightmapEncoding("invalid"); !errors.Is(err, render3d.ErrInvalidLightmap) {
		t.Errorf("ParseLightmapEncoding(invalid) = %v, want ErrInvalidLightmap", err)
	}
	if _, err := render3d.EncodeRGBMImage(nil, 0, 2, 8); !errors.Is(err, render3d.ErrInvalidLightmap) {
		t.Errorf("EncodeRGBMImage(0x2) = %v, want ErrInvalidLightmap", err)
	}
	if _, err := render3d.UploadLightmapRGBA16F(nil, dev, []math3d.Vec3{{X: nan}}, 1, 1, render3d.TextureUpload{}); !errors.Is(err, render3d.ErrInvalidLightmap) {
		t.Errorf("UploadLightmapRGBA16F(NaN) = %v, want ErrInvalidLightmap", err)
	}
}

func TestLightmapIntensityNonIndexedAndLifecycle(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(lightmapTestOrthoCamera(1))

	renderer, err := render3d.NewLightmapRenderer(nil, dev, camBuf.Layout(), render3d.LightmapRendererOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewLightmapRenderer: %v", err)
	}

	// Non-indexed two-triangle quad covering [-1, 1]^2.
	n := math3d.Vec3{Z: 1}
	nonIndexed := render3d.LightmapMesh{
		Positions: []math3d.Vec3{
			{X: -1, Y: -1, Z: 0}, {X: 1, Y: -1, Z: 0}, {X: 1, Y: 1, Z: 0},
			{X: -1, Y: -1, Z: 0}, {X: 1, Y: 1, Z: 0}, {X: -1, Y: 1, Z: 0},
		},
		Normals: []math3d.Vec3{n, n, n, n, n, n},
		UV0:     []math3d.Vec2{{X: 0, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 0}, {X: 0, Y: 0}},
		UV1:     []math3d.Vec2{{X: 0, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: 0}, {X: 0, Y: 1}, {X: 1, Y: 0}, {X: 0, Y: 0}},
	}
	if nonIndexed.Indexed() || nonIndexed.VertexCount() != 6 || len(nonIndexed.Vertices()) != 6 {
		t.Fatalf("unexpected non-indexed mesh stats: indexed=%v count=%d", nonIndexed.Indexed(), nonIndexed.VertexCount())
	}
	gpuMesh, err := render3d.UploadLightmapMesh(nil, dev, nonIndexed, render3d.MeshUpload{Label: "non-indexed"})
	if err != nil {
		t.Fatalf("UploadLightmapMesh: %v", err)
	}
	if gpuMesh.Indexed() || gpuMesh.Triangles() != 2 || gpuMesh.ByteSize() != 6*render3d.LightmapVertexStride {
		t.Fatalf("unexpected GPULightmapMesh stats: indexed=%v tris=%d bytes=%d", gpuMesh.Indexed(), gpuMesh.Triangles(), gpuMesh.ByteSize())
	}

	lmTex, err := render3d.UploadTexturePixels(nil, dev, render3d.PixelBuffer{
		Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm, Pixels: []byte{102, 102, 102, 255}, // ~0.4
	}, render3d.TextureUpload{})
	if err != nil {
		t.Fatalf("UploadTexturePixels: %v", err)
	}
	t.Cleanup(func() { _ = lmTex.Close() })

	// Intensity = 2.0 doubles 0.4 -> 0.8 (~204).
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
	mat := render3d.DefaultLightmapMaterial()
	mat.Lightmap = lmTex
	mat.Intensity = 2.0
	if err := renderer.Draw(pass, camBuf, gpuMesh, math3d.Mat4Identity(), mat); err != nil {
		t.Fatalf("Draw intensity=2: %v", err)
	}
	pass.End()
	dev.Submit(enc.Finish())
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	px := sr.Image().RGBAAt(8, 8)
	lightmapTestApproxByte(t, "intensity=2 R", px.R, 204, 4)

	// Closing mesh & renderer is idempotent and marks them invalid.
	if err := gpuMesh.Close(); err != nil {
		t.Fatalf("gpuMesh.Close: %v", err)
	}
	if gpuMesh.Valid() {
		t.Fatal("gpuMesh.Valid() = true after Close")
	}
	if err := gpuMesh.Close(); err != nil {
		t.Fatalf("gpuMesh second Close: %v", err)
	}
	if err := renderer.Close(); err != nil {
		t.Fatalf("renderer.Close: %v", err)
	}
	if renderer.Valid() {
		t.Fatal("renderer.Valid() = true after Close")
	}
	if err := renderer.Close(); err != nil {
		t.Fatalf("renderer second Close: %v", err)
	}
}
