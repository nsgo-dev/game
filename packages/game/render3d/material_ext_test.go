package render3d_test

import (
	"errors"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
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

func extMatLoadKhronosDoc(t *testing.T, relPath string) *gltf.Document {
	t.Helper()
	root := filepath.Join("..", "gltf", "testdata", "khronos")
	doc, err := gltf.LoadFS(os.DirFS(root), relPath)
	if err != nil {
		t.Fatalf("gltf.LoadFS(%s): %v", relPath, err)
	}
	return doc
}

func extMatTransformMesh(doc *gltf.Document, nodeIdx int, prim gltf.Primitive, uvRef *gltf.TextureRef) render3d.Mesh {
	world := doc.Nodes[nodeIdx].WorldMatrix
	nm, hasNM := math3d.NormalMatrix(world)
	positions := make([]math3d.Vec3, len(prim.Positions))
	for i, p := range prim.Positions {
		positions[i] = world.TransformPoint(p)
	}
	var normals []math3d.Vec3
	if len(prim.Normals) == len(prim.Positions) {
		normals = make([]math3d.Vec3, len(prim.Normals))
		for i, n := range prim.Normals {
			if hasNM {
				normals[i] = nm.MulVec3(n).Norm()
			} else {
				normals[i] = n.Norm()
			}
		}
	}
	var uvs []math3d.Vec2
	if uvRef != nil {
		if transformed, err := doc.TransformTexCoords(prim, uvRef, nil); err == nil {
			uvs = transformed
		}
	}
	if len(uvs) == 0 && len(prim.TexCoords0) == len(prim.Positions) {
		uvs = append([]math3d.Vec2(nil), prim.TexCoords0...)
	}
	indices := append([]uint32(nil), prim.Indices...)
	return render3d.Mesh{
		Positions: positions,
		Normals:   normals,
		UVs:       uvs,
		Indices:   indices,
	}
}

func extMatSolidNRGBA(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func extMatWaveNormalMap(size int, freq, strength float64) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		v := (float64(y) + 0.5) / float64(size)
		for x := range size {
			u := (float64(x) + 0.5) / float64(size)
			dx := math.Cos(u*freq*2*math.Pi) * strength
			dy := math.Sin(v*freq*2*math.Pi) * strength * 0.65
			n := math3d.Vec3{X: float32(dx), Y: float32(dy), Z: 1}.Norm()
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round(float64(n.X*0.5+0.5) * 255)),
				G: uint8(math.Round(float64(n.Y*0.5+0.5) * 255)),
				B: uint8(math.Round(float64(n.Z*0.5+0.5) * 255)),
				A: 255,
			})
		}
	}
	return img
}

func extMatCheckerImage(size, cells int, a, b color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		cy := (y * cells) / size
		for x := range size {
			cx := (x * cells) / size
			if (cx+cy)%2 == 0 {
				img.SetNRGBA(x, y, a)
			} else {
				img.SetNRGBA(x, y, b)
			}
		}
	}
	return img
}

func extMatClearcoatMaskImage(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			// Smooth circular coated medallion on dark uncoated surround.
			dx := (float64(x)+0.5)/float64(size) - 0.5
			dy := (float64(y)+0.5)/float64(size) - 0.5
			r := math.Sqrt(dx*dx + dy*dy)
			v := uint8(255)
			if r > 0.36 {
				v = 40
			}
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

func extMatRoughnessStripesImage(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			// Alternating glossy (low G) and satin (high G) diagonal stripes.
			stripe := ((x + y) / (size / 6)) % 2
			g := uint8(24)
			if stripe == 1 {
				g = 190
			}
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: g, B: 255, A: 255})
		}
	}
	return img
}

func extMatAbsDiffU8(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func extMatLuma(c color.RGBA) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}

func TestExtendedMaterialValidationAndUniforms(t *testing.T) {
	// 1. DefaultMaterial defaults and extension validation.
	m := render3d.DefaultMaterial()
	if m.IOR != render3d.DefaultMaterialIOR || m.ClearcoatNormalScale != 1 {
		t.Fatalf("DefaultMaterial IOR=%v ClearcoatNormalScale=%v, want 1.5 and 1", m.IOR, m.ClearcoatNormalScale)
	}
	if m.UsesExtensions() {
		t.Error("DefaultMaterial.UsesExtensions() = true, want false")
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("DefaultMaterial.Validate: %v", err)
	}

	// Zero-value IOR defaults to 1.5 via EffectiveIOR and stays valid for legacy struct literals.
	legacy := render3d.Material{
		BaseColorFactor: [4]float32{1, 1, 1, 1},
		RoughnessFactor: 0.5,
	}
	if legacy.EffectiveIOR() != render3d.DefaultMaterialIOR {
		t.Errorf("legacy EffectiveIOR = %v, want %v", legacy.EffectiveIOR(), render3d.DefaultMaterialIOR)
	}
	if err := legacy.Validate(); err != nil {
		t.Fatalf("legacy Material.Validate: %v", err)
	}

	badScalars := []struct {
		name string
		edit func(*render3d.Material)
	}{
		{"ClearcoatFactor<0", func(m *render3d.Material) { m.ClearcoatFactor = -0.1 }},
		{"ClearcoatFactor>1", func(m *render3d.Material) { m.ClearcoatFactor = 1.1 }},
		{"ClearcoatFactor NaN", func(m *render3d.Material) { m.ClearcoatFactor = float32(math.NaN()) }},
		{"ClearcoatRoughnessFactor<0", func(m *render3d.Material) { m.ClearcoatRoughnessFactor = -0.05 }},
		{"ClearcoatRoughnessFactor>1", func(m *render3d.Material) { m.ClearcoatRoughnessFactor = 1.2 }},
		{"ClearcoatNormalScale Inf", func(m *render3d.Material) { m.ClearcoatNormalScale = float32(math.Inf(1)) }},
		{"TransmissionFactor<0", func(m *render3d.Material) { m.TransmissionFactor = -0.2 }},
		{"TransmissionFactor>1", func(m *render3d.Material) { m.TransmissionFactor = 1.05 }},
		{"IOR<1 nonzero", func(m *render3d.Material) { m.IOR = 0.8 }},
		{"IOR negative", func(m *render3d.Material) { m.IOR = -1.5 }},
		{"IOR NaN", func(m *render3d.Material) { m.IOR = float32(math.NaN()) }},
	}
	for _, tc := range badScalars {
		t.Run(tc.name, func(t *testing.T) {
			bad := render3d.DefaultMaterial()
			tc.edit(&bad)
			if err := bad.Validate(); !errors.Is(err, render3d.ErrInvalidMaterial) {
				t.Fatalf("Validate(%s) = %v, want ErrInvalidMaterial", tc.name, err)
			}
		})
	}

	// 2. Zero-allocation 96-byte uniform encoding.
	extMat := render3d.DefaultMaterial()
	extMat.ClearcoatFactor = 0.9
	extMat.ClearcoatRoughnessFactor = 0.15
	extMat.ClearcoatNormalScale = -0.75
	extMat.TransmissionFactor = 0.8
	extMat.IOR = 1.45
	if !extMat.UsesExtensions() {
		t.Error("extMat.UsesExtensions() = false, want true")
	}
	u := extMat.ExtendedUniforms()
	buf := make([]byte, 0, render3d.ExtendedMaterialUniformSize)
	allocs := testing.AllocsPerRun(50, func() {
		buf = u.AppendUniformData(buf[:0])
	})
	if allocs != 0 {
		t.Errorf("ExtendedMaterialUniforms.AppendUniformData allocs = %v, want 0", allocs)
	}
	if len(buf) != render3d.ExtendedMaterialUniformSize {
		t.Fatalf("encoded len = %d, want %d", len(buf), render3d.ExtendedMaterialUniformSize)
	}

	// 3. GPU lifecycle: ExtendedMaterialDefaults, GPUExtendedMaterial, TransmissionBackground, and Close.
	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	extDefs, err := render3d.NewExtendedMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{Label: "ext-test"})
	if err != nil {
		t.Fatalf("NewExtendedMaterialDefaults: %v", err)
	}
	if !extDefs.Valid() || extDefs.Layout() == nil || extDefs.Base() == nil || extDefs.ByteSize() == 0 {
		t.Fatalf("ExtendedMaterialDefaults not valid after creation")
	}

	ccTex, err := render3d.UploadTexture(nil, dev, extMatSolidNRGBA(4, 4, color.NRGBA{R: 255, G: 255, B: 255, A: 255}), render3d.TextureUpload{Label: "cc"})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}
	t.Cleanup(func() { _ = ccTex.Close() })

	extMat.ClearcoatTexture = ccTex
	gpuExt, err := render3d.NewGPUExtendedMaterial(nil, dev, extDefs, extMat, render3d.MaterialUpload{Label: "ext-mat"})
	if err != nil {
		t.Fatalf("NewGPUExtendedMaterial: %v", err)
	}
	if !gpuExt.Valid() || !gpuExt.HasClearcoat() || !gpuExt.HasTransmission() || gpuExt.ByteSize() != render3d.ExtendedMaterialUniformSize {
		t.Fatalf("GPUExtendedMaterial invalid state: valid=%v cc=%v tr=%v bytes=%d",
			gpuExt.Valid(), gpuExt.HasClearcoat(), gpuExt.HasTransmission(), gpuExt.ByteSize())
	}

	// In-place Update succeeds for scalar changes and rejects texture swaps.
	updated := extMat
	updated.ClearcoatFactor = 0.4
	updated.TransmissionFactor = 0.25
	if err := gpuExt.Update(dev, updated); err != nil {
		t.Fatalf("GPUExtendedMaterial.Update: %v", err)
	}
	if gpuExt.Material().ClearcoatFactor != 0.4 || gpuExt.Material().TransmissionFactor != 0.25 {
		t.Errorf("Update did not record new factors: %+v", gpuExt.Material())
	}
	swapped := updated
	swapped.ClearcoatTexture = nil
	if err := gpuExt.Update(dev, swapped); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Fatalf("Update texture swap = %v, want ErrInvalidMaterial", err)
	}

	// TransmissionBackground lifecycle.
	tb, err := render3d.NewTransmissionBackground(nil, dev, render3d.TransmissionBackgroundOptions{Label: "tb-test"})
	if err != nil {
		t.Fatalf("NewTransmissionBackground: %v", err)
	}
	if !tb.Valid() || tb.Layout() == nil || tb.BindGroup() == nil {
		t.Fatal("TransmissionBackground invalid after creation")
	}
	if err := tb.SetTexture(dev, nil, true); err != nil {
		t.Fatalf("SetTexture(nil): %v", err)
	}
	if err := tb.SetViewport(dev, nil, 0, 32, 32, 32, 32, 32, false); !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Fatalf("SetViewport invalid dims = %v, want ErrInvalidMaterial", err)
	}

	// Close idempotency + nil safety.
	if err := gpuExt.Close(); err != nil {
		t.Errorf("gpuExt.Close: %v", err)
	}
	if err := gpuExt.Close(); err != nil {
		t.Errorf("gpuExt.Close second: %v", err)
	}
	if gpuExt.Valid() {
		t.Error("gpuExt.Valid() = true after Close")
	}
	if err := tb.Close(); err != nil {
		t.Errorf("tb.Close: %v", err)
	}
	if err := tb.Close(); err != nil {
		t.Errorf("tb.Close second: %v", err)
	}
	if tb.Valid() {
		t.Error("tb.Valid() = true after Close")
	}
	if err := extDefs.Close(); err != nil {
		t.Errorf("extDefs.Close: %v", err)
	}
	if extDefs.Valid() {
		t.Error("extDefs.Valid() = true after Close")
	}
	var nilExt *render3d.GPUExtendedMaterial
	var nilTB *render3d.TransmissionBackground
	var nilDefs *render3d.ExtendedMaterialDefaults
	if nilExt.Valid() || nilExt.Close() != nil || nilTB.Valid() || nilTB.Close() != nil || nilDefs.Valid() || nilDefs.Close() != nil {
		t.Error("nil receivers must be safe")
	}
}

func TestExtendedMaterialCPUAndGPUParity(t *testing.T) {
	// Verify CPU EvaluateMaterial / ShadePointWithBackground match GPU extended pipeline pixel readback
	// across clearcoat, transmission over a colored backdrop, non-default IOR, and HDR emissive.
	const size = 48
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	extDefs, err := render3d.NewExtendedMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{Label: "parity"})
	if err != nil {
		t.Fatalf("NewExtendedMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = extDefs.Close() })

	tb, err := render3d.NewTransmissionBackground(nil, dev, render3d.TransmissionBackgroundOptions{Label: "parity-bg"})
	if err != nil {
		t.Fatalf("NewTransmissionBackground: %v", err)
	}
	t.Cleanup(func() { _ = tb.Close() })

	// Constant linear background texture (R=0.8, G=0.4, B=0.2 in RGBA8Unorm linear).
	bgColor := color.NRGBA{R: 204, G: 102, B: 51, A: 255}
	bgLinear := [3]float32{204.0 / 255.0, 102.0 / 255.0, 51.0 / 255.0}
	bgTex, err := render3d.UploadTexture(nil, dev, extMatSolidNRGBA(size, size, bgColor), render3d.TextureUpload{Label: "bg"})
	if err != nil {
		t.Fatalf("UploadTexture bg: %v", err)
	}
	t.Cleanup(func() { _ = bgTex.Close() })
	if err := tb.SetTexture(dev, bgTex.Texture(), false); err != nil {
		t.Fatalf("SetTexture: %v", err)
	}

	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.15, Y: 0.15, Z: 0.18}, Intensity: 1},
		Directional: []render3d.DirectionalLight{{
			Direction: math3d.Vec3{X: -0.35, Y: -0.45, Z: -0.82},
			Color:     math3d.Vec3{X: 1, Y: 0.97, Z: 0.92},
			Intensity: 2.2,
		}},
	}
	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{Label: "parity-lights"})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	defaultPipe, err := render3d.NewExtendedMaterialPipeline(nil, dev, camBuf.Layout(), extDefs.Layout(), render3d.ExtendedMaterialPipelineOptions{
		DepthFormat:        gpu.FormatDepth24Plus,
		TransmissionLayout: tb.Layout(),
	})
	if err != nil {
		t.Fatalf("NewExtendedMaterialPipeline (default+bg): %v", err)
	}
	litPipe, err := render3d.NewExtendedMaterialPipeline(nil, dev, camBuf.Layout(), extDefs.Layout(), render3d.ExtendedMaterialPipelineOptions{
		DepthFormat:        gpu.FormatDepth24Plus,
		LightLayout:        lightBuf.Layout(),
		TransmissionLayout: tb.Layout(),
	})
	if err != nil {
		t.Fatalf("NewExtendedMaterialPipeline (lit+bg): %v", err)
	}

	quad, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3{}, 2.0, 2.0), render3d.MeshUpload{Label: "parity-quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 2.2}, math3d.DegToRad(45), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	cases := []struct {
		name string
		mat  render3d.Material
	}{
		{
			name: "clearcoat-rough-base",
			mat: func() render3d.Material {
				m := render3d.DefaultMaterial()
				m.BaseColorFactor = [4]float32{0.65, 0.12, 0.10, 1}
				m.MetallicFactor = 0
				m.RoughnessFactor = 0.85
				m.ClearcoatFactor = 1.0
				m.ClearcoatRoughnessFactor = 0.08
				return m
			}(),
		},
		{
			name: "transmissive-tinted-glass",
			mat: func() render3d.Material {
				m := render3d.DefaultMaterial()
				m.BaseColorFactor = [4]float32{0.45, 0.92, 0.65, 1}
				m.MetallicFactor = 0
				m.RoughnessFactor = 0.05
				m.TransmissionFactor = 0.9
				m.IOR = 1.45
				return m
			}(),
		},
		{
			name: "combined-emissive-clearcoat-transmission",
			mat: func() render3d.Material {
				m := render3d.DefaultMaterial()
				m.BaseColorFactor = [4]float32{0.6, 0.75, 0.95, 1}
				m.MetallicFactor = 0
				m.RoughnessFactor = 0.06
				m.EmissiveFactor = [3]float32{0.18, 0.05, 0.30}
				m.ClearcoatFactor = 0.7
				m.ClearcoatRoughnessFactor = 0.12
				m.TransmissionFactor = 0.65
				m.IOR = 1.6
				return m
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gpuMat, err := render3d.NewGPUExtendedMaterial(nil, dev, extDefs, tc.mat, render3d.MaterialUpload{Label: tc.name})
			if err != nil {
				t.Fatalf("NewGPUExtendedMaterial: %v", err)
			}
			defer func() { _ = gpuMat.Close() }()

			// 1. Default-light pipeline vs EvaluateMaterial.
			enc := dev.BeginEncoder()
			pass := enc.BeginRenderPass(gpu.RenderPassDesc{
				ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
				DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
			})
			pass.SetPipeline(defaultPipe)
			pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
			gpuMat.Bind(pass)
			tb.BindAt(pass, render3d.MaterialTransmissionBackgroundGroup)
			quad.Draw(pass)
			pass.End()
			dev.Submit(enc.Finish())
			sr.WaitIdle()
			if err := sr.LastError(); err != nil {
				t.Fatalf("software renderer default: %v", err)
			}
			gotDef := sr.Image().RGBAAt(size/2, size/2)
			wantDef := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
				Material:        tc.mat,
				Normal:          math3d.Vec3{Z: 1},
				ViewDir:         math3d.Vec3{Z: 1},
				FrontFacing:     true,
				BackgroundColor: bgLinear,
			})
			wantDefPx := color.RGBA{
				R: uint8(math.Round(float64(wantDef.SRGB.X) * 255)),
				G: uint8(math.Round(float64(wantDef.SRGB.Y) * 255)),
				B: uint8(math.Round(float64(wantDef.SRGB.Z) * 255)),
				A: 255,
			}
			if extMatAbsDiffU8(gotDef.R, wantDefPx.R) > 5 || extMatAbsDiffU8(gotDef.G, wantDefPx.G) > 5 || extMatAbsDiffU8(gotDef.B, wantDefPx.B) > 5 {
				t.Errorf("default-light GPU=%v vs CPU EvaluateMaterial=%v", gotDef, wantDefPx)
			}

			// 2. LightRig pipeline vs ShadePointWithBackground.
			enc2 := dev.BeginEncoder()
			pass2 := enc2.BeginRenderPass(gpu.RenderPassDesc{
				ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
				DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
			})
			pass2.SetPipeline(litPipe)
			pass2.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
			gpuMat.Bind(pass2)
			pass2.SetBindGroup(render3d.ExtendedLightGroup, lightBuf.BindGroup())
			tb.Bind(pass2)
			quad.Draw(pass2)
			pass2.End()
			dev.Submit(enc2.Finish())
			sr.WaitIdle()
			if err := sr.LastError(); err != nil {
				t.Fatalf("software renderer lit: %v", err)
			}
			gotLit := sr.Image().RGBAAt(size/2, size/2)
			pbrMat := render3d.PBRMaterial{
				BaseColor:          math3d.Vec3{X: tc.mat.BaseColorFactor[0], Y: tc.mat.BaseColorFactor[1], Z: tc.mat.BaseColorFactor[2]},
				Metallic:           tc.mat.MetallicFactor,
				Roughness:          tc.mat.RoughnessFactor,
				Emissive:           math3d.Vec3{X: tc.mat.EmissiveFactor[0], Y: tc.mat.EmissiveFactor[1], Z: tc.mat.EmissiveFactor[2]},
				Clearcoat:          tc.mat.ClearcoatFactor,
				ClearcoatRoughness: tc.mat.ClearcoatRoughnessFactor,
				Transmission:       tc.mat.TransmissionFactor,
				IOR:                tc.mat.EffectiveIOR(),
			}
			wantLin := render3d.ShadePointWithBackground(
				math3d.Vec3Zero(),
				math3d.Vec3{Z: 1},
				math3d.Vec3{Z: 1},
				pbrMat,
				rig,
				math3d.Vec3{X: bgLinear[0], Y: bgLinear[1], Z: bgLinear[2]},
			)
			wantSRGB := render3d.LinearToSRGB(wantLin)
			wantLitPx := color.RGBA{
				R: uint8(math.Round(float64(wantSRGB.X) * 255)),
				G: uint8(math.Round(float64(wantSRGB.Y) * 255)),
				B: uint8(math.Round(float64(wantSRGB.Z) * 255)),
				A: 255,
			}
			if extMatAbsDiffU8(gotLit.R, wantLitPx.R) > 5 || extMatAbsDiffU8(gotLit.G, wantLitPx.G) > 5 || extMatAbsDiffU8(gotLit.B, wantLitPx.B) > 5 {
				t.Errorf("lit-rig GPU=%v vs CPU ShadePointWithBackground=%v", gotLit, wantLitPx)
			}
		})
	}
}

func TestExtendedMaterialClearcoatNormalCotangentFrame(t *testing.T) {
	// Verify clearcoatNormalTexture tilts specular response via the UV-aligned cotangent frame,
	// respects ClearcoatNormalScale sign (negative flips tilt direction), and stays independent of base NormalTexture.
	const size = 48
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	extDefs, err := render3d.NewExtendedMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewExtendedMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = extDefs.Close() })

	// Key light from +X / +Z so tilting clearcoat normal toward +X increases highlight while tilting toward -X decreases it.
	rig := render3d.LightRig{
		Directional: []render3d.DirectionalLight{{
			Direction: math3d.Vec3{X: -0.75, Y: 0, Z: -0.66},
			Color:     math3d.Vec3{X: 1, Y: 1, Z: 1},
			Intensity: 3.0,
		}},
	}
	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	pipe, err := render3d.NewExtendedMaterialPipeline(nil, dev, camBuf.Layout(), extDefs.Layout(), render3d.ExtendedMaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		LightLayout: lightBuf.Layout(),
	})
	if err != nil {
		t.Fatalf("NewExtendedMaterialPipeline: %v", err)
	}

	// Tangent-space normal tilted ~24 degrees toward +X to align with the halfway vector
	// between view (0, 0, +1) and light (+0.75, 0, +0.66): (+0.41, 0, +0.91).
	tiltTex, err := render3d.UploadTexture(nil, dev, extMatSolidNRGBA(4, 4, color.NRGBA{R: 180, G: 128, B: 244, A: 255}), render3d.TextureUpload{Label: "tilt"})
	if err != nil {
		t.Fatalf("UploadTexture tilt: %v", err)
	}
	t.Cleanup(func() { _ = tiltTex.Close() })

	quad, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3{}, 2, 2), render3d.MeshUpload{Label: "cc-quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 2.2}, math3d.DegToRad(45), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	renderWithScale := func(scale float32) color.RGBA {
		m := render3d.DefaultMaterial()
		m.BaseColorFactor = [4]float32{0.02, 0.02, 0.02, 1}
		m.MetallicFactor = 0
		m.RoughnessFactor = 1.0
		m.ClearcoatFactor = 1.0
		m.ClearcoatRoughnessFactor = 0.14
		m.ClearcoatNormalTexture = tiltTex
		m.ClearcoatNormalScale = scale
		gm, err := render3d.NewGPUExtendedMaterial(nil, dev, extDefs, m, render3d.MaterialUpload{})
		if err != nil {
			t.Fatalf("NewGPUExtendedMaterial: %v", err)
		}
		defer func() { _ = gm.Close() }()

		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		gm.Bind(pass)
		pass.SetBindGroup(render3d.ExtendedLightGroup, lightBuf.BindGroup())
		quad.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		return sr.Image().RGBAAt(size/2, size/2)
	}

	posPx := renderWithScale(+1.0) // tilted toward +X (aligned with halfway vector between +Z view and +X/+Z light)
	negPx := renderWithScale(-1.0) // flipped toward -X (tilted away from light)
	if extMatLuma(posPx) <= extMatLuma(negPx)+60 {
		t.Errorf("clearcoat normal tilt did not align with cotangent +X: posScale=%v (luma=%.1f), negScale=%v (luma=%.1f)",
			posPx, extMatLuma(posPx), negPx, extMatLuma(negPx))
	}
}

func TestExtendedMaterialDynamicResolutionTransmission(t *testing.T) {
	// Verify TransmissionBackground.SetDynamicResolution tracks DynamicResolutionTarget scale changes
	// across 0.5x and 0.75x sub-viewports inside a fixed 128x96 backing texture without rebuilding any material bind group.
	const (
		nativeW = 128
		nativeH = 96
	)
	sr := gpu.NewSoftwareRenderer(nativeW, nativeH, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	extDefs, err := render3d.NewExtendedMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{Label: "dynres"})
	if err != nil {
		t.Fatalf("NewExtendedMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = extDefs.Close() })

	tb, err := render3d.NewTransmissionBackground(nil, dev, render3d.TransmissionBackgroundOptions{Label: "dynres-tb"})
	if err != nil {
		t.Fatalf("NewTransmissionBackground: %v", err)
	}
	t.Cleanup(func() { _ = tb.Close() })

	opaqueTarget, err := render3d.NewDynamicResolutionTarget(nil, dev, render3d.DynamicResolutionTargetOptions{
		Label:       "dynres-opaque",
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
		Config: render3d.DynamicResolutionConfig{
			MinScale: 0.5,
			MaxScale: 1.0,
		},
	})
	if err != nil {
		t.Fatalf("NewDynamicResolutionTarget opaque: %v", err)
	}
	t.Cleanup(func() { _ = opaqueTarget.Close() })

	finalTarget, err := render3d.NewDynamicResolutionTarget(nil, dev, render3d.DynamicResolutionTargetOptions{
		Label:       "dynres-final",
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
		Scaler:      opaqueTarget.Scaler(),
	})
	if err != nil {
		t.Fatalf("NewDynamicResolutionTarget final: %v", err)
	}
	t.Cleanup(func() { _ = finalTarget.Close() })

	// Left half red opaque quad (X in [-3, 0]) and right half blue opaque quad (X in [0, 3])
	// with zero overlap at Z = -0.5 (worldQuadMesh takes half-extents hx, hy), covered by one clear transmissive glass quad.
	leftMesh, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3{X: -1.5, Z: -0.5}, 1.5, 3.0), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh left: %v", err)
	}
	t.Cleanup(func() { _ = leftMesh.Close() })
	rightMesh, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3{X: 1.5, Z: -0.5}, 1.5, 3.0), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh right: %v", err)
	}
	t.Cleanup(func() { _ = rightMesh.Close() })
	glassMesh, err := render3d.UploadMesh(nil, dev, worldQuadMesh(math3d.Vec3{Z: 0.2}, 3.6, 2.8), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh glass: %v", err)
	}
	t.Cleanup(func() { _ = glassMesh.Close() })

	redMat := render3d.DefaultMaterial()
	redMat.BaseColorFactor = [4]float32{0.95, 0.08, 0.08, 1}
	redMat.EmissiveFactor = [3]float32{0.6, 0.02, 0.02}
	redMat.MetallicFactor = 0
	redMat.RoughnessFactor = 0.8
	redGPU, err := render3d.NewGPUExtendedMaterial(nil, dev, extDefs, redMat, render3d.MaterialUpload{Label: "red"})
	if err != nil {
		t.Fatalf("redGPU: %v", err)
	}
	t.Cleanup(func() { _ = redGPU.Close() })

	blueMat := render3d.DefaultMaterial()
	blueMat.BaseColorFactor = [4]float32{0.08, 0.15, 0.95, 1}
	blueMat.EmissiveFactor = [3]float32{0.02, 0.05, 0.65}
	blueMat.MetallicFactor = 0
	blueMat.RoughnessFactor = 0.8
	blueGPU, err := render3d.NewGPUExtendedMaterial(nil, dev, extDefs, blueMat, render3d.MaterialUpload{Label: "blue"})
	if err != nil {
		t.Fatalf("blueGPU: %v", err)
	}
	t.Cleanup(func() { _ = blueGPU.Close() })

	// One transmissive glass material reused across all resolution scale changes (bind group never rebuilt!).
	glassMat := render3d.DefaultMaterial()
	glassMat.BaseColorFactor = [4]float32{0.96, 0.96, 0.96, 1}
	glassMat.MetallicFactor = 0
	glassMat.RoughnessFactor = 0.05
	glassMat.TransmissionFactor = 1.0
	glassMat.IOR = 1.5
	glassGPU, err := render3d.NewGPUExtendedMaterial(nil, dev, extDefs, glassMat, render3d.MaterialUpload{Label: "glass"})
	if err != nil {
		t.Fatalf("glassGPU: %v", err)
	}
	t.Cleanup(func() { _ = glassGPU.Close() })
	glassBG := glassGPU.BindGroup()

	opaquePipe, err := render3d.NewExtendedMaterialPipeline(nil, dev, camBuf.Layout(), extDefs.Layout(), render3d.ExtendedMaterialPipelineOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("opaquePipe: %v", err)
	}
	transPipe, err := render3d.NewExtendedMaterialPipeline(nil, dev, camBuf.Layout(), extDefs.Layout(), render3d.ExtendedMaterialPipelineOptions{
		ColorFormat:        gpu.FormatRGBA8Unorm,
		DepthFormat:        gpu.FormatDepth24Plus,
		TransmissionLayout: tb.Layout(),
	})
	if err != nil {
		t.Fatalf("transPipe: %v", err)
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3.2}, math3d.DegToRad(45), float32(nativeW)/float32(nativeH), 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	for _, scale := range []float32{0.5, 0.75, 1.0} {
		opaqueTarget.Scaler().SetFixedScale(scale)

		// Pass 1: opaque scene into opaqueTarget at active scaled sub-viewport.
		enc1 := dev.BeginEncoder()
		p1, err := opaqueTarget.Begin3D(enc1, nativeW, nativeH, core.RGB(0x000000))
		if err != nil {
			t.Fatalf("scale %.2f Begin3D opaque: %v", scale, err)
		}
		p1.SetPipeline(opaquePipe)
		p1.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		redGPU.Bind(p1)
		leftMesh.Draw(p1)
		blueGPU.Bind(p1)
		rightMesh.Draw(p1)
		p1.End()
		dev.Submit(enc1.Finish())
		sr.WaitIdle()

		// Update the single per-frame TransmissionBackground for the new scale (no material rebuild!).
		if err := tb.SetDynamicResolution(dev, opaqueTarget, nil, 0, 0, true); err != nil {
			t.Fatalf("scale %.2f SetDynamicResolution: %v", scale, err)
		}
		if glassGPU.BindGroup() != glassBG {
			t.Fatal("material bind group must not change on resolution resize")
		}

		// Pass 2: draw transmissive glass over finalTarget at the same scaled sub-viewport, then upscale to native.
		enc2 := dev.BeginEncoder()
		p2, err := finalTarget.Begin3D(enc2, nativeW, nativeH, core.RGB(0x000000))
		if err != nil {
			t.Fatalf("scale %.2f Begin3D final: %v", scale, err)
		}
		p2.SetPipeline(transPipe)
		p2.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		glassGPU.Bind(p2)
		tb.BindAt(p2, render3d.MaterialTransmissionBackgroundGroup)
		glassMesh.Draw(p2)
		p2.End()
		if err := finalTarget.Upscale(enc2, nil, nativeW, nativeH); err != nil {
			t.Fatalf("scale %.2f Upscale: %v", scale, err)
		}
		dev.Submit(enc2.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("scale %.2f software renderer: %v", scale, err)
		}

		img := sr.Image()
		leftPx := img.RGBAAt(nativeW/4, nativeH/2)
		rightPx := img.RGBAAt(3*nativeW/4, nativeH/2)
		if leftPx.R <= leftPx.B+50 {
			t.Errorf("scale %.2f left pixel = %v, want red transmitted through glass", scale, leftPx)
		}
		if rightPx.B <= rightPx.R+50 {
			t.Errorf("scale %.2f right pixel = %v, want blue transmitted through glass", scale, rightPx)
		}
	}
}

func TestExtendedMaterialKhronosEmissiveStrengthGolden(t *testing.T) {
	// Official Khronos EmissiveStrengthTest: 5 cubes with KHR_materials_emissive_strength
	// 1, 2, 4, 8, 16 over the PlainGrid backdrop, rendered through HDRTarget + BloomPass + ACES ToneMapPass.
	doc := extMatLoadKhronosDoc(t, "EmissiveStrengthTest/glTF/EmissiveStrengthTest.gltf")

	const (
		width  = 224
		height = 128
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	extDefs, err := render3d.NewExtendedMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{Label: "emissive-khronos"})
	if err != nil {
		t.Fatalf("NewExtendedMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = extDefs.Close() })

	matSet, err := render3d.UploadGLTFExtendedMaterials(nil, dev, extDefs, doc, render3d.GLTFMaterialUpload{Label: "EmissiveStrengthTest"})
	if err != nil {
		t.Fatalf("UploadGLTFExtendedMaterials: %v", err)
	}
	t.Cleanup(func() { _ = matSet.Close() })

	// Verify emissiveStrength was folded into EmissiveFactor for all 5 cubes (cyan [0.1, 0.5, 0.9] * strength).
	for i := range matSet.Len() {
		info, _ := matSet.Info(i)
		switch info.Name {
		case "Emit16":
			if info.Material.EmissiveFactor[2] < 10 {
				t.Errorf("Emit16 EmissiveFactor = %v, want HDR blue > 10", info.Material.EmissiveFactor)
			}
		}
	}

	hdr, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{
		Label:       "emissive-hdr",
		Width:       width,
		Height:      height,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewHDRTarget: %v", err)
	}
	t.Cleanup(func() { _ = hdr.Close() })

	bloomTarget, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{
		Label:  "emissive-bloom-out",
		Width:  width,
		Height: height,
	})
	if err != nil {
		t.Fatalf("NewHDRTarget bloomTarget: %v", err)
	}
	t.Cleanup(func() { _ = bloomTarget.Close() })

	bloom, err := render3d.NewBloomPass(nil, dev, render3d.BloomPassOptions{
		Label:        "emissive-bloom",
		Width:        width,
		Height:       height,
		ColorFormat:  hdr.Format(),
		OutputFormat: bloomTarget.Format(),
		Config: render3d.BloomConfig{
			Threshold: 1.0,
			SoftKnee:  0.5,
			Intensity: 0.75,
			Radius:    2.0,
		},
	})
	if err != nil {
		t.Fatalf("NewBloomPass: %v", err)
	}
	t.Cleanup(func() { _ = bloom.Close() })

	toneMap, err := render3d.NewToneMapPass(nil, dev, render3d.ToneMapPassDesc{
		Label:        "emissive-tonemap",
		TargetFormat: dev.SurfaceFormat(),
	})
	if err != nil {
		t.Fatalf("NewToneMapPass: %v", err)
	}
	t.Cleanup(func() { _ = toneMap.Close() })

	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.08, Y: 0.09, Z: 0.12}, Intensity: 1},
		Directional: []render3d.DirectionalLight{{
			Direction: math3d.Vec3{X: -0.25, Y: -0.4, Z: -0.88},
			Color:     math3d.Vec3{X: 1, Y: 0.98, Z: 0.95},
			Intensity: 0.55,
		}},
	}
	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	hdrPipe, err := render3d.NewExtendedMaterialPipeline(nil, dev, camBuf.Layout(), extDefs.Layout(), render3d.ExtendedMaterialPipelineOptions{
		ColorFormat:  hdr.Format(),
		DepthFormat:  hdr.DepthFormat(),
		LightLayout:  lightBuf.Layout(),
		LinearOutput: true,
	})
	if err != nil {
		t.Fatalf("NewExtendedMaterialPipeline HDR: %v", err)
	}

	type drawItem struct {
		mesh *render3d.GPUMesh
		mat  *render3d.GPUExtendedMaterial
	}
	var items []drawItem
	for ni, node := range doc.Nodes {
		if node.Mesh < 0 {
			continue
		}
		for _, prim := range doc.Meshes[node.Mesh].Primitives {
			cpuMesh := extMatTransformMesh(doc, ni, prim, nil)
			gm, err := render3d.UploadMesh(nil, dev, cpuMesh, render3d.MeshUpload{Label: node.Name})
			if err != nil {
				t.Fatalf("UploadMesh %s: %v", node.Name, err)
			}
			t.Cleanup(func() { _ = gm.Close() })
			items = append(items, drawItem{mesh: gm, mat: matSet.ExtendedMaterial(prim.Material)})
		}
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 0.6, Z: 13.5}, math3d.DegToRad(36), float32(width)/float32(height), 0.1, 100)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0, Z: 0}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(hdr.RenderPassDesc(core.RGB(0x090b12), 1))
	pass.SetPipeline(hdrPipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	pass.SetBindGroup(render3d.ExtendedLightGroup, lightBuf.BindGroup())
	for _, it := range items {
		it.mat.Bind(pass)
		it.mesh.Draw(pass)
	}
	pass.End()

	if err := bloom.Apply(dev, enc, hdr.ColorTexture(), bloomTarget.ColorTexture()); err != nil {
		t.Fatalf("BloomPass.Apply: %v", err)
	}

	outPass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
	})
	if err := toneMap.DrawTarget(outPass, dev, bloomTarget, render3d.ToneMapOptions{
		Operator: render3d.ToneMapACES,
		Exposure: 1.0,
	}); err != nil {
		t.Fatalf("ToneMapPass.DrawTarget: %v", err)
	}
	outPass.End()

	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	// Verify progressive HDR bloom halo growth directly above Cube1 (far left bay) vs Cube16 (far right bay).
	haloLeft := img.RGBAAt(width*11/100, height*39/100)
	haloRight := img.RGBAAt(width*89/100, height*39/100)
	if extMatLuma(haloRight) <= extMatLuma(haloLeft)+30 {
		t.Errorf("expected stronger HDR bloom halo around Emit16 (%v, luma=%.1f) than Emit1 (%v, luma=%.1f)",
			haloRight, extMatLuma(haloRight), haloLeft, extMatLuma(haloLeft))
	}

	assertReference(t, "khronos_emissive_strength", img)
}

func TestExtendedMaterialKhronosClearcoatGolden(t *testing.T) {
	// Official Khronos TextureTransformMultiTest clearcoat panels (Clearcoat, ClearcoatRough, ClearcoatNormal
	// with KHR_texture_transform UVs baked from doc.TransformTexCoords) rendered alongside a glossy clearcoat
	// car-paint sphere under a LightRig.
	doc := extMatLoadKhronosDoc(t, "TextureTransformMultiTest/glTF/TextureTransformMultiTest.gltf")

	const (
		width  = 224
		height = 160
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	extDefs, err := render3d.NewExtendedMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{Label: "clearcoat-khronos"})
	if err != nil {
		t.Fatalf("NewExtendedMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = extDefs.Close() })

	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.16, Y: 0.18, Z: 0.24}, Intensity: 1},
		Directional: []render3d.DirectionalLight{
			{
				Direction: math3d.Vec3{X: -0.25, Y: -0.35, Z: -0.90},
				Color:     math3d.Vec3{X: 1.0, Y: 0.97, Z: 0.92},
				Intensity: 3.0,
			},
			{
				Direction: math3d.Vec3{X: 0.45, Y: -0.15, Z: -0.88},
				Color:     math3d.Vec3{X: 0.65, Y: 0.80, Z: 1.0},
				Intensity: 1.5,
			},
		},
		Point: []render3d.PointLight{
			{Position: math3d.Vec3{X: -0.82, Y: -0.36, Z: 1.3}, Color: math3d.Vec3{X: 1, Y: 0.98, Z: 0.95}, Intensity: 4.0, Range: 4.5},
			{Position: math3d.Vec3{X: 0.0, Y: -0.36, Z: 1.3}, Color: math3d.Vec3{X: 1, Y: 0.98, Z: 0.95}, Intensity: 4.0, Range: 4.5},
			{Position: math3d.Vec3{X: 0.82, Y: -0.36, Z: 1.3}, Color: math3d.Vec3{X: 1, Y: 0.98, Z: 0.95}, Intensity: 4.0, Range: 4.5},
		},
	}
	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	pipe, err := render3d.NewExtendedMaterialPipeline(nil, dev, camBuf.Layout(), extDefs.Layout(), render3d.ExtendedMaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		LightLayout: lightBuf.Layout(),
	})
	if err != nil {
		t.Fatalf("NewExtendedMaterialPipeline: %v", err)
	}

	ccMaskTex, err := render3d.UploadTexture(nil, dev, extMatClearcoatMaskImage(64), render3d.TextureUpload{Label: "cc-mask"})
	if err != nil {
		t.Fatalf("ccMaskTex: %v", err)
	}
	t.Cleanup(func() { _ = ccMaskTex.Close() })
	ccRoughTex, err := render3d.UploadTexture(nil, dev, extMatRoughnessStripesImage(64), render3d.TextureUpload{Label: "cc-rough"})
	if err != nil {
		t.Fatalf("ccRoughTex: %v", err)
	}
	t.Cleanup(func() { _ = ccRoughTex.Close() })
	ccWaveNormTex, err := render3d.UploadTexture(nil, dev, extMatWaveNormalMap(64, 2.5, 0.45), render3d.TextureUpload{Label: "cc-wave-norm"})
	if err != nil {
		t.Fatalf("ccWaveNormTex: %v", err)
	}
	t.Cleanup(func() { _ = ccWaveNormTex.Close() })

	// Extract the 3 Khronos Clearcoat sample panels (ClearcoatUV0, ClearcoatRoughUV0, ClearcoatNormalUV0)
	// with their KHR_materials_clearcoat scalars & KHR_texture_transform UVs directly from the Khronos glTF asset.
	panelSpecs := []struct {
		nodeName string
		offsetX  float32
		texKind  string
	}{
		{"ClearcoatUV0", -0.82, "mask"},
		{"ClearcoatRoughUV0", 0.0, "rough"},
		{"ClearcoatNormalUV0", 0.82, "normal"},
	}

	type drawItem struct {
		mesh *render3d.GPUMesh
		mat  *render3d.GPUExtendedMaterial
	}
	var items []drawItem

	nodeIndex := make(map[string]int, len(doc.Nodes))
	for ni, n := range doc.Nodes {
		nodeIndex[n.Name] = ni
	}
	for _, ps := range panelSpecs {
		ni, ok := nodeIndex[ps.nodeName]
		if !ok {
			t.Fatalf("TextureTransformMultiTest missing node %q", ps.nodeName)
		}
		prim := doc.Meshes[doc.Nodes[ni].Mesh].Primitives[0]
		gm := doc.Materials[prim.Material]
		if gm.Clearcoat == nil {
			t.Fatalf("node %q material has nil Clearcoat", ps.nodeName)
		}
		var uvRef *gltf.TextureRef
		switch ps.texKind {
		case "mask":
			uvRef = gm.Clearcoat.Texture
		case "rough":
			uvRef = gm.Clearcoat.RoughnessTexture
		case "normal":
			uvRef = gm.Clearcoat.NormalTexture
		}
		// Build mesh in local quad space scaled to 0.68 width and placed in a front row.
		uvs, err := doc.TransformTexCoords(prim, uvRef, nil)
		if err != nil {
			t.Fatalf("TransformTexCoords %s: %v", ps.nodeName, err)
		}
		pos := make([]math3d.Vec3, len(prim.Positions))
		for i, p := range prim.Positions {
			pos[i] = math3d.Vec3{X: p.X*3.2 + ps.offsetX, Y: p.Y*3.2 - 0.36, Z: 0.0}
		}
		cpuMesh := render3d.Mesh{
			Positions: pos,
			Normals:   append([]math3d.Vec3(nil), prim.Normals...),
			UVs:       uvs,
			Indices:   append([]uint32(nil), prim.Indices...),
		}
		gpuMesh, err := render3d.UploadMesh(nil, dev, cpuMesh, render3d.MeshUpload{Label: ps.nodeName})
		if err != nil {
			t.Fatalf("UploadMesh %s: %v", ps.nodeName, err)
		}
		t.Cleanup(func() { _ = gpuMesh.Close() })

		mat := render3d.DefaultMaterial()
		mat.BaseColorFactor = [4]float32{0.05, 0.14, 0.42, 1}
		mat.MetallicFactor = 0.0
		mat.RoughnessFactor = 0.72
		mat.ClearcoatFactor = gm.Clearcoat.Factor
		mat.ClearcoatRoughnessFactor = gm.Clearcoat.RoughnessFactor
		switch ps.texKind {
		case "mask":
			mat.ClearcoatTexture = ccMaskTex
			mat.ClearcoatRoughnessFactor = 0.16
		case "rough":
			mat.ClearcoatRoughnessTexture = ccRoughTex
		case "normal":
			mat.ClearcoatNormalTexture = ccWaveNormTex
			mat.ClearcoatNormalScale = gm.Clearcoat.NormalTexture.Scale
			mat.ClearcoatRoughnessFactor = 0.18
		}
		gpuMat, err := render3d.NewGPUExtendedMaterial(nil, dev, extDefs, mat, render3d.MaterialUpload{Label: ps.nodeName})
		if err != nil {
			t.Fatalf("NewGPUExtendedMaterial %s: %v", ps.nodeName, err)
		}
		t.Cleanup(func() { _ = gpuMat.Close() })
		items = append(items, drawItem{mesh: gpuMesh, mat: gpuMat})
	}

	// Top pair of spheres comparing uncoated rough crimson vs clearcoat-lacquered crimson with ribbed clearcoat normal map.
	uncoatedMat := render3d.DefaultMaterial()
	uncoatedMat.BaseColorFactor = [4]float32{0.72, 0.08, 0.09, 1}
	uncoatedMat.MetallicFactor = 0
	uncoatedMat.RoughnessFactor = 0.75
	uncoatedGPU, err := render3d.NewGPUExtendedMaterial(nil, dev, extDefs, uncoatedMat, render3d.MaterialUpload{Label: "uncoated-sphere"})
	if err != nil {
		t.Fatalf("uncoatedGPU: %v", err)
	}
	t.Cleanup(func() { _ = uncoatedGPU.Close() })
	uncoatedMesh, err := render3d.UploadMesh(nil, dev, centeredUVSphereMesh(math3d.Vec3{X: -0.52, Y: 0.46, Z: 0.0}, 0.38, 22, 32), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("uncoatedMesh: %v", err)
	}
	t.Cleanup(func() { _ = uncoatedMesh.Close() })
	items = append(items, drawItem{mesh: uncoatedMesh, mat: uncoatedGPU})

	coatedMat := uncoatedMat
	coatedMat.ClearcoatFactor = 1.0
	coatedMat.ClearcoatRoughnessFactor = 0.16
	coatedMat.ClearcoatNormalTexture = ccWaveNormTex
	coatedMat.ClearcoatNormalScale = 0.35
	coatedGPU, err := render3d.NewGPUExtendedMaterial(nil, dev, extDefs, coatedMat, render3d.MaterialUpload{Label: "coated-sphere"})
	if err != nil {
		t.Fatalf("coatedGPU: %v", err)
	}
	t.Cleanup(func() { _ = coatedGPU.Close() })
	coatedMesh, err := render3d.UploadMesh(nil, dev, centeredUVSphereMesh(math3d.Vec3{X: 0.52, Y: 0.46, Z: 0.0}, 0.38, 22, 32), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("coatedMesh: %v", err)
	}
	t.Cleanup(func() { _ = coatedMesh.Close() })
	items = append(items, drawItem{mesh: coatedMesh, mat: coatedGPU})

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 0.12, Z: 2.55}, math3d.DegToRad(40), float32(width)/float32(height), 0.1, 20)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0.06, Z: 0}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x111520)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	pass.SetBindGroup(render3d.ExtendedLightGroup, lightBuf.BindGroup())
	for _, it := range items {
		it.mat.Bind(pass)
		it.mesh.Draw(pass)
	}
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	// Peak specular brightness on the clearcoated right sphere must exceed the uncoated rough left sphere.
	var maxLeft, maxRight float64
	for y := 10; y < height/2; y++ {
		for x := width / 6; x < width/2-4; x++ {
			if l := extMatLuma(img.RGBAAt(x, y)); l > maxLeft {
				maxLeft = l
			}
		}
		for x := width/2 + 4; x < 5*width/6; x++ {
			if l := extMatLuma(img.RGBAAt(x, y)); l > maxRight {
				maxRight = l
			}
		}
	}
	if maxRight <= maxLeft+25 {
		t.Errorf("clearcoat sphere peak luma %.1f did not exceed uncoated sphere %.1f by >25", maxRight, maxLeft)
	}

	assertReference(t, "khronos_clearcoat", img)
}

func TestExtendedMaterialKhronosTransmissionGolden(t *testing.T) {
	// Official Khronos SunglassesKhronos model (KHR_materials_transmission + KHR_materials_ior on
	// LensesInterior, LensesExterior, Nosepads) rendered over a colorful checkerboard backdrop via the
	// two-pass TransmissionBackground pipeline, alongside a tinted transmissive glass orb.
	doc := extMatLoadKhronosDoc(t, "SunglassesKhronos/glTF/SunglassesKhronos.gltf")

	const (
		width  = 224
		height = 144
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	extDefs, err := render3d.NewExtendedMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{Label: "trans-khronos"})
	if err != nil {
		t.Fatalf("NewExtendedMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = extDefs.Close() })

	tb, err := render3d.NewTransmissionBackground(nil, dev, render3d.TransmissionBackgroundOptions{Label: "trans-khronos-bg", SRGB: true})
	if err != nil {
		t.Fatalf("NewTransmissionBackground: %v", err)
	}
	t.Cleanup(func() { _ = tb.Close() })

	// Strip external image URI on earhooks (omitted in testdata/khronos per README.md) so UploadGLTFExtendedMaterials
	// exercises the exact glTF document upload path cleanly.
	docCopy := *doc
	docCopy.Textures = append([]gltf.Texture(nil), doc.Textures...)
	for i := range docCopy.Textures {
		docCopy.Textures[i].Source = -1
	}
	matSet, err := render3d.UploadGLTFExtendedMaterials(nil, dev, extDefs, &docCopy, render3d.GLTFMaterialUpload{Label: "SunglassesKhronos"})
	if err != nil {
		t.Fatalf("UploadGLTFExtendedMaterials(SunglassesKhronos): %v", err)
	}
	t.Cleanup(func() { _ = matSet.Close() })

	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.28, Y: 0.30, Z: 0.34}, Intensity: 1},
		Directional: []render3d.DirectionalLight{
			{
				Direction: math3d.Vec3{X: -0.35, Y: -0.55, Z: -0.75},
				Color:     math3d.Vec3{X: 1, Y: 0.98, Z: 0.94},
				Intensity: 2.8,
			},
			{
				Direction: math3d.Vec3{X: 0.55, Y: -0.35, Z: -0.75},
				Color:     math3d.Vec3{X: 0.85, Y: 0.92, Z: 1.0},
				Intensity: 1.5,
			},
		},
	}
	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	opaquePipe, err := render3d.NewExtendedMaterialPipeline(nil, dev, camBuf.Layout(), extDefs.Layout(), render3d.ExtendedMaterialPipelineOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
		LightLayout: lightBuf.Layout(),
		DoubleSided: true,
	})
	if err != nil {
		t.Fatalf("opaquePipe: %v", err)
	}
	transPipe, err := render3d.NewExtendedMaterialPipeline(nil, dev, camBuf.Layout(), extDefs.Layout(), render3d.ExtendedMaterialPipelineOptions{
		ColorFormat:        gpu.FormatRGBA8Unorm,
		DepthFormat:        gpu.FormatDepth24Plus,
		LightLayout:        lightBuf.Layout(),
		TransmissionLayout: tb.Layout(),
		DoubleSided:        true,
	})
	if err != nil {
		t.Fatalf("transPipe: %v", err)
	}

	type drawItem struct {
		mesh *render3d.GPUMesh
		mat  *render3d.GPUExtendedMaterial
	}
	var opaqueItems, transItems []drawItem
	bounds := math3d.AABBEmpty()

	for ni, node := range docCopy.Nodes {
		if node.Mesh < 0 {
			continue
		}
		for _, prim := range docCopy.Meshes[node.Mesh].Primitives {
			cpuMesh := extMatTransformMesh(&docCopy, ni, prim, nil)
			bounds = bounds.Union(cpuMesh.Bounds())
			gm, err := render3d.UploadMesh(nil, dev, cpuMesh, render3d.MeshUpload{Label: node.Name})
			if err != nil {
				t.Fatalf("UploadMesh %s: %v", node.Name, err)
			}
			t.Cleanup(func() { _ = gm.Close() })
			mat := matSet.ExtendedMaterial(prim.Material)
			if mat.HasTransmission() {
				// Boost very dark sunglasses tint slightly only if needed? No — keep authentic Khronos material,
				// and add an emerald-tinted transmissive lens sphere right beside it to showcase bright tinted transmission too!
				transItems = append(transItems, drawItem{mesh: gm, mat: mat})
			} else {
				opaqueItems = append(opaqueItems, drawItem{mesh: gm, mat: mat})
			}
		}
	}
	if len(transItems) == 0 {
		t.Fatal("SunglassesKhronos produced zero transmissive items")
	}

	center := bounds.Center()
	sizeVec := bounds.Size()
	maxDim := max(sizeVec.X, max(sizeVec.Y, sizeVec.Z))

	// Vibrant warm/cyan checkerboard backdrop placed behind the sunglasses so transmitted background is unmistakable.
	checkerTex, err := render3d.UploadTexture(nil, dev, extMatCheckerImage(64, 8, color.NRGBA{R: 255, G: 210, B: 60, A: 255}, color.NRGBA{R: 45, G: 195, B: 235, A: 255}), render3d.TextureUpload{Label: "checker", SRGB: true})
	if err != nil {
		t.Fatalf("checkerTex: %v", err)
	}
	t.Cleanup(func() { _ = checkerTex.Close() })

	bgQuadMat := render3d.DefaultMaterial()
	bgQuadMat.BaseColorTexture = checkerTex
	bgQuadMat.MetallicFactor = 0
	bgQuadMat.RoughnessFactor = 0.9
	bgQuadMat.EmissiveTexture = checkerTex
	bgQuadMat.EmissiveFactor = [3]float32{0.65, 0.65, 0.65}
	bgQuadGPU, err := render3d.NewGPUExtendedMaterial(nil, dev, extDefs, bgQuadMat, render3d.MaterialUpload{Label: "backdrop"})
	if err != nil {
		t.Fatalf("bgQuadGPU: %v", err)
	}
	t.Cleanup(func() { _ = bgQuadGPU.Close() })
	bgQuadMesh, err := render3d.UploadMesh(nil, dev, worldQuadMesh(
		math3d.Vec3{X: center.X, Y: center.Y, Z: bounds.Min.Z - maxDim*0.25},
		maxDim*3.0, maxDim*2.0,
	), render3d.MeshUpload{Label: "backdrop"})
	if err != nil {
		t.Fatalf("bgQuadMesh: %v", err)
	}
	t.Cleanup(func() { _ = bgQuadMesh.Close() })
	opaqueItems = append([]drawItem{{mesh: bgQuadMesh, mat: bgQuadGPU}}, opaqueItems...)

	// Tinted clear glass orb in front of the checkerboard (right lower side) to showcase colored Fresnel transmission + specular highlight.
	orbMat := render3d.DefaultMaterial()
	orbMat.BaseColorFactor = [4]float32{0.55, 0.98, 0.72, 1}
	orbMat.MetallicFactor = 0
	orbMat.RoughnessFactor = 0.06
	orbMat.TransmissionFactor = 0.92
	orbMat.ClearcoatFactor = 0.5
	orbMat.ClearcoatRoughnessFactor = 0.05
	orbMat.IOR = 1.5
	orbGPU, err := render3d.NewGPUExtendedMaterial(nil, dev, extDefs, orbMat, render3d.MaterialUpload{Label: "glass-orb"})
	if err != nil {
		t.Fatalf("orbGPU: %v", err)
	}
	t.Cleanup(func() { _ = orbGPU.Close() })
	orbMesh, err := render3d.UploadMesh(nil, dev, centeredUVSphereMesh(
		math3d.Vec3{X: center.X + maxDim*0.44, Y: center.Y - maxDim*0.24, Z: bounds.Max.Z + maxDim*0.02},
		maxDim*0.15, 20, 28,
	), render3d.MeshUpload{Label: "glass-orb"})
	if err != nil {
		t.Fatalf("orbMesh: %v", err)
	}
	t.Cleanup(func() { _ = orbMesh.Close() })
	transItems = append(transItems, drawItem{mesh: orbMesh, mat: orbGPU})

	// Offscreen opaque scene capture target.
	opaqueRT, err := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{
		Label:       "trans-opaque-rt",
		Width:       width,
		Height:      height,
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("CreateRenderTarget: %v", err)
	}
	t.Cleanup(func() { _ = opaqueRT.Close() })

	cam := scene3d.NewPerspectiveCamera(
		center.Add(math3d.Vec3{X: maxDim * 0.22, Y: maxDim * 0.18, Z: maxDim * 1.45}),
		math3d.DegToRad(38),
		float32(width)/float32(height),
		maxDim*0.05,
		maxDim*20,
	)
	cam.LookAt(center, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	// Pass 1: render opaque backdrop + opaque sunglasses frame into opaqueRT.
	enc1 := dev.BeginEncoder()
	p1 := enc1.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{
			Texture:    opaqueRT.Color,
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: core.RGB(0x10141c),
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			Texture:      opaqueRT.Depth,
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1,
		},
	})
	p1.SetPipeline(opaquePipe)
	p1.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	p1.SetBindGroup(render3d.ExtendedLightGroup, lightBuf.BindGroup())
	for _, it := range opaqueItems {
		it.mat.Bind(p1)
		it.mesh.Draw(p1)
	}
	p1.End()
	dev.Submit(enc1.Finish())
	sr.WaitIdle()

	// Bind opaqueRT.Color ONCE at frame/pass level.
	if err := tb.SetTexture(dev, opaqueRT.Color, true); err != nil {
		t.Fatalf("tb.SetTexture: %v", err)
	}

	// Pass 2: render full scene (opaque + transmissive sunglasses lenses & tinted glass orb) to output.
	enc2 := dev.BeginEncoder()
	p2 := enc2.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	p2.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	p2.SetBindGroup(render3d.ExtendedLightGroup, lightBuf.BindGroup())

	p2.SetPipeline(opaquePipe)
	for _, it := range opaqueItems {
		it.mat.Bind(p2)
		it.mesh.Draw(p2)
	}
	p2.SetPipeline(transPipe)
	tb.Bind(p2)
	for _, it := range transItems {
		it.mat.Bind(p2)
		it.mesh.Draw(p2)
	}
	p2.End()
	dev.Submit(enc2.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	assertReference(t, "khronos_transmission", img)
}

func TestExtendedMaterialDrawsInGPUViewAndRetainedDeviceLoss(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			const size = 64
			var (
				activeDev *gpu.Device
				camBuf    *render3d.CameraBuffer
				extDefs   *render3d.ExtendedMaterialDefaults
				lightBuf  *render3d.GPULightBuffer
				tb        *render3d.TransmissionBackground
				bgTex     *render3d.GPUTexture
				mesh      *render3d.GPUMesh
				mat       *render3d.GPUExtendedMaterial
				pipe      *gpu.Pipeline
			)
			baseMat := render3d.DefaultMaterial()
			baseMat.BaseColorFactor = [4]float32{0.45, 0.88, 0.62, 1}
			baseMat.MetallicFactor = 0
			baseMat.RoughnessFactor = 0.08
			baseMat.ClearcoatFactor = 0.85
			baseMat.ClearcoatRoughnessFactor = 0.1
			baseMat.TransmissionFactor = 0.75
			baseMat.IOR = 1.5

			rig := render3d.LightRig{
				Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.12, Y: 0.12, Z: 0.14}, Intensity: 1},
				Directional: []render3d.DirectionalLight{{
					Direction: math3d.Vec3{X: -0.3, Y: -0.4, Z: -0.85},
					Color:     math3d.Vec3{X: 1, Y: 1, Z: 1},
					Intensity: 2.0,
				}},
			}

			opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "ext-viewport",
					Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						activeDev = dev
						var err error
						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
						if extDefs == nil {
							extDefs, err = render3d.NewExtendedMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{Label: "ret-ext", Retained: true})
							if err != nil {
								t.Fatalf("NewExtendedMaterialDefaults: %v", err)
							}
						}
						if lightBuf == nil {
							lightBuf, err = render3d.NewLightBuffer(cx, dev, rig, render3d.LightBufferOptions{Label: "ret-light", Retained: true})
							if err != nil {
								t.Fatalf("NewLightBuffer: %v", err)
							}
						}
						if tb == nil {
							tb, err = render3d.NewTransmissionBackground(cx, dev, render3d.TransmissionBackgroundOptions{Label: "ret-tb", Retained: true})
							if err != nil {
								t.Fatalf("NewTransmissionBackground: %v", err)
							}
						}
						if bgTex == nil {
							bgTex, err = render3d.UploadTexture(cx, dev, extMatSolidNRGBA(size, size, color.NRGBA{R: 220, G: 180, B: 60, A: 255}), render3d.TextureUpload{Label: "ret-bg", Retained: true})
							if err != nil {
								t.Fatalf("UploadTexture bg: %v", err)
							}
						}
						if err := tb.SetTexture(dev, bgTex.Texture(), false); err != nil {
							t.Fatalf("tb.SetTexture: %v", err)
						}
						if mesh == nil {
							mesh, err = render3d.UploadMesh(cx, dev, worldQuadMesh(math3d.Vec3{}, 1.8, 1.8), render3d.MeshUpload{Label: "ret-quad", Retained: true})
							if err != nil {
								t.Fatalf("UploadMesh: %v", err)
							}
						}
						if mat == nil {
							mat, err = render3d.NewGPUExtendedMaterial(cx, dev, extDefs, baseMat, render3d.MaterialUpload{Label: "ret-mat", Retained: true})
							if err != nil {
								t.Fatalf("NewGPUExtendedMaterial: %v", err)
							}
						}
						pipe, err = render3d.NewExtendedMaterialPipeline(cx, dev, camBuf.Layout(), extDefs.Layout(), render3d.ExtendedMaterialPipelineOptions{
							DepthFormat:        gpu.FormatDepth24Plus,
							LightLayout:        lightBuf.Layout(),
							TransmissionLayout: tb.Layout(),
						})
						if err != nil {
							t.Fatalf("NewExtendedMaterialPipeline: %v", err)
						}
						cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 2.2}, math3d.DegToRad(45), 1, 0.1, 20)
						cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
						camBuf.WriteCamera(cam)
					},
					OnFrame: func(frame *gpu.Frame) {
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x000000))
						pass.ClearDepth(1)
						pass.SetPipeline(pipe)
						pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
						mat.Bind(pass)
						pass.SetBindGroup(render3d.ExtendedLightGroup, lightBuf.BindGroup())
						tb.Bind(pass)
						mesh.Draw(pass)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)
			beforeImg, err := app.GPUReadPixels("id:ext-viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels before loss: %v", err)
			}
			before := beforeImg.RGBAAt(size/2, size/2)
			if before.G < 80 {
				t.Fatalf("before device loss center = %v, want bright green-tinted transmissive quad", before)
			}

			// Simulate GPU device loss & restore on full profile (verifying Retained restore + Update + SetTexture).
			if p.name == "full" {
				firstMat := mat
				app.TriggerGPUDeviceLost("id:ext-viewport", nsgotest.GPULostContextLost, "context lost")
				if firstMat.Valid() {
					t.Fatal("GPUExtendedMaterial still valid while device is lost")
				}
				app.RestoreGPUDevice("id:ext-viewport")
				if mat != firstMat || !extDefs.Valid() || !mat.Valid() || !lightBuf.Valid() || !bgTex.Valid() || !mesh.Valid() || !tb.Valid() {
					t.Fatalf("retained resources not valid after restore: same=%v defs=%v mat=%v light=%v bg=%v mesh=%v tb=%v",
						mat == firstMat, extDefs.Valid(), mat.Valid(), lightBuf.Valid(), bgTex.Valid(), mesh.Valid(), tb.Valid())
				}
				updated := baseMat
				updated.EmissiveFactor = [3]float32{0.6, 0.05, 0.05}
				if err := mat.Update(activeDev, updated); err != nil {
					t.Fatalf("mat.Update after restore: %v", err)
				}
				app.Advance(16 * time.Millisecond)
				afterImg, err := app.GPUReadPixels("id:ext-viewport")
				if err != nil {
					t.Fatalf("GPUReadPixels after restore: %v", err)
				}
				after := afterImg.RGBAAt(size/2, size/2)
				if after.R <= before.R+35 {
					t.Errorf("after restore Update(emissive) center = %v, want noticeably redder than before %v", after, before)
				}
			}
		})
	}
}
