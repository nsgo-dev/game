package render3d_test

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// All unexported helpers in this file use the pbrExt prefix to avoid name
// collisions with parallel render3d tests.

func pbrExtQuadMesh(half float32, z float32) render3d.Mesh {
	n := math3d.Vec3{Z: 1}
	return render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: -half, Y: -half, Z: z},
			{X: half, Y: -half, Z: z},
			{X: half, Y: half, Z: z},
			{X: -half, Y: half, Z: z},
		},
		Normals: []math3d.Vec3{n, n, n, n},
		UVs: []math3d.Vec2{
			{X: 0, Y: 1},
			{X: 1, Y: 1},
			{X: 1, Y: 0},
			{X: 0, Y: 0},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

func pbrExtSphereMesh(radius float32, stacks, slices int) render3d.Mesh {
	var m render3d.Mesh
	for i := 0; i <= stacks; i++ {
		v := float64(i) / float64(stacks)
		phi := v * math.Pi
		sp, cp := math.Sin(phi), math.Cos(phi)
		for j := 0; j <= slices; j++ {
			u := float64(j) / float64(slices)
			theta := u * 2 * math.Pi
			st, ct := math.Sin(theta), math.Cos(theta)
			nx := float32(sp * ct)
			ny := float32(cp)
			nz := float32(sp * st)
			m.Positions = append(m.Positions, math3d.Vec3{X: nx * radius, Y: ny * radius, Z: nz * radius})
			m.Normals = append(m.Normals, math3d.Vec3{X: nx, Y: ny, Z: nz})
			m.UVs = append(m.UVs, math3d.Vec2{X: float32(u), Y: float32(v)})
		}
	}
	stride := uint32(slices + 1)
	for i := 0; i < stacks; i++ {
		for j := 0; j < slices; j++ {
			a := uint32(i)*stride + uint32(j)
			b := a + stride
			c := b + 1
			d := a + 1
			m.Indices = append(m.Indices, a, d, b, b, d, c)
		}
	}
	return m
}

func pbrExtOrthoCam(width, height int, orthoHeight float32) *scene3d.Camera {
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 4}, orthoHeight, float32(width)/float32(height), 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	return cam
}

func pbrExtSolidRGBA(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func pbrExtHalf16ToFloat32(h uint16) float32 {
	sign := uint32(h>>15) & 1
	exp := uint32(h>>10) & 0x1f
	frac := uint32(h) & 0x3ff
	switch exp {
	case 0:
		if frac == 0 {
			return math.Float32frombits(sign << 31)
		}
		f := float32(frac) / 1024.0 * float32(math.Ldexp(1, -14))
		if sign != 0 {
			return -f
		}
		return f
	case 31:
		if frac == 0 {
			return math.Float32frombits((sign << 31) | 0x7f800000)
		}
		return float32(math.NaN())
	default:
		bits := (sign << 31) | ((exp + 112) << 23) | (frac << 13)
		return math.Float32frombits(bits)
	}
}

func pbrExtReadPixelFloat16(t *testing.T, dev *gpu.Device, tex *gpu.Texture, x, y uint32) [4]float32 {
	t.Helper()
	var out [4]float32
	called := false
	dev.ReadTexture(tex, gpu.TextureRegion{X: x, Y: y, Width: 1, Height: 1}, func(data []byte, err error) {
		called = true
		if err != nil {
			t.Fatalf("ReadTexture(%d,%d): %v", x, y, err)
		}
		if len(data) < 8 {
			t.Fatalf("ReadTexture(%d,%d): got %d bytes, want >= 8", x, y, len(data))
		}
		for i := range 4 {
			out[i] = pbrExtHalf16ToFloat32(binary.LittleEndian.Uint16(data[2*i:]))
		}
	})
	if !called {
		t.Fatal("ReadTexture callback did not run")
	}
	return out
}

func pbrExtByteFromFloat(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(math.Round(float64(v) * 255))
}

func pbrExtDiffByte(a, b uint8) int {
	d := int(a) - int(b)
	if d < 0 {
		return -d
	}
	return d
}

func pbrExtCheckRGB(t *testing.T, label string, got color.RGBA, wantVec math3d.Vec3, tol int) {
	t.Helper()
	wr := pbrExtByteFromFloat(wantVec.X)
	wg := pbrExtByteFromFloat(wantVec.Y)
	wb := pbrExtByteFromFloat(wantVec.Z)
	if pbrExtDiffByte(got.R, wr) > tol || pbrExtDiffByte(got.G, wg) > tol || pbrExtDiffByte(got.B, wb) > tol {
		t.Fatalf("%s: got RGB(%d,%d,%d), want RGB(%d,%d,%d) ±%d", label, got.R, got.G, got.B, wr, wg, wb, tol)
	}
}

func TestPBRExtEmissiveStrengthHDRAndToneMap(t *testing.T) {
	const w, h = 64, 64
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(pbrExtOrthoCam(w, h, 2))

	quad, err := render3d.UploadMesh(nil, dev, pbrExtQuadMesh(1, 0), render3d.MeshUpload{Label: "emissive-quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	// Part 1: Raw linear HDR output into FormatRGBA16Float verifies emissiveStrength
	// visibly exceeds SDR 1.0 and matches exact analytic linear radiance for 0, 1, and 5.
	hdrTarget, err := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{
		Label:       "pbr-ext-hdr",
		Width:       w,
		Height:      h,
		ColorFormat: gpu.FormatRGBA16Float,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("CreateRenderTarget(RGBA16Float): %v", err)
	}
	t.Cleanup(func() { _ = hdrTarget.Close() })

	hdrRenderer, err := render3d.NewExtendedPBRRenderer(nil, dev, camBuf.Layout(), render3d.ExtendedPBRRendererOptions{
		ColorFormat: gpu.FormatRGBA16Float,
		DepthFormat: gpu.FormatDepth24Plus,
		CullMode:    gpu.CullBack,
	})
	if err != nil {
		t.Fatalf("NewExtendedPBRRenderer(HDR): %v", err)
	}
	t.Cleanup(func() { _ = hdrRenderer.Close() })

	darkLight := render3d.PBRExtLighting{
		LightDir:       math3d.Vec3{Z: 1},
		LightColor:     math3d.Vec3Zero(), // pure emissive isolation
		AmbientSky:     math3d.Vec3Zero(),
		AmbientGround:  math3d.Vec3Zero(),
		Exposure:       1,
		ToneMap:        render3d.PBRToneMapNone,
		ViewportWidth:  w,
		ViewportHeight: h,
	}

	emissiveColor := math3d.Vec3{X: 0.8, Y: 0.4, Z: 0.2}
	for _, strength := range []float32{0, 1, 5} {
		matDesc := render3d.DefaultPBRMaterialExt()
		matDesc.BaseColorFactor = math3d.Vec4{X: 0, Y: 0, Z: 0, W: 1}
		matDesc.EmissiveFactor = emissiveColor
		matDesc.EmissiveStrength = strength

		gpuMat, err := hdrRenderer.CreateMaterial(nil, matDesc)
		if err != nil {
			t.Fatalf("CreateMaterial(strength=%g): %v", strength, err)
		}
		hdrRenderer.SetScene(math3d.Mat4Identity(), darkLight)

		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				Texture:    hdrTarget.Color,
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: core.RGB(0x000000),
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				Texture:      hdrTarget.Depth,
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreDiscard,
				ClearDepth:   1,
			},
		})
		hdrRenderer.Draw(pass, camBuf, gpuMat, quad)
		pass.End()
		dev.Submit(enc.Finish())
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer HDR (strength=%g): %v", strength, err)
		}

		got := pbrExtReadPixelFloat16(t, dev, hdrTarget.Color, w/2, h/2)
		wantHDR, _ := render3d.EvaluatePBRMaterialExt(render3d.PBRExtSample{
			BaseColor:        math3d.Vec3Zero(),
			Roughness:        1,
			Emissive:         emissiveColor,
			EmissiveStrength: strength,
			Normal:           math3d.Vec3{Z: 1},
			ViewDir:          math3d.Vec3{Z: 1},
		}, darkLight)

		if !math3d.ApproxEqual(got[0], wantHDR.X, 0.02) ||
			!math3d.ApproxEqual(got[1], wantHDR.Y, 0.02) ||
			!math3d.ApproxEqual(got[2], wantHDR.Z, 0.02) {
			t.Fatalf("HDR strength=%g: got (%v,%v,%v), want (%v,%v,%v)", strength, got[0], got[1], got[2], wantHDR.X, wantHDR.Y, wantHDR.Z)
		}
		if strength == 5 && got[0] <= 3.5 {
			t.Fatalf("HDR strength=5 red channel = %v, want > 3.5 (exceeding SDR 1.0)", got[0])
		}
		_ = gpuMat.Close()
	}

	// Part 2: ACES tone-mapped SDR RGBA8 output produces strictly increasing
	// brightness across emissiveStrength = 0, 1, and 5 matching analytic reference.
	sdrRenderer, err := render3d.NewExtendedPBRRenderer(nil, dev, camBuf.Layout(), render3d.ExtendedPBRRendererOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
		CullMode:    gpu.CullBack,
	})
	if err != nil {
		t.Fatalf("NewExtendedPBRRenderer(SDR): %v", err)
	}
	t.Cleanup(func() { _ = sdrRenderer.Close() })

	acesLight := darkLight
	acesLight.ToneMap = render3d.PBRToneMapACES
	acesLight.Exposure = 0.5 // keeps strength=5 below clip so 0 < 1 < 5 is unmistakably distinct

	var prevR uint8
	for i, strength := range []float32{0, 1, 5} {
		matDesc := render3d.DefaultPBRMaterialExt()
		matDesc.BaseColorFactor = math3d.Vec4{W: 1}
		matDesc.EmissiveFactor = emissiveColor
		matDesc.EmissiveStrength = strength

		gpuMat, err := sdrRenderer.CreateMaterial(nil, matDesc)
		if err != nil {
			t.Fatalf("CreateMaterial SDR: %v", err)
		}
		sdrRenderer.SetScene(math3d.Mat4Identity(), acesLight)

		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		sdrRenderer.Draw(pass, camBuf, gpuMat, quad)
		pass.End()
		dev.Submit(enc.Finish())
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer SDR: %v", err)
		}

		px := sr.Image().RGBAAt(w/2, h/2)
		_, wantSDR := render3d.EvaluatePBRMaterialExt(render3d.PBRExtSample{
			BaseColor:        math3d.Vec3Zero(),
			Roughness:        1,
			Emissive:         emissiveColor,
			EmissiveStrength: strength,
			Normal:           math3d.Vec3{Z: 1},
			ViewDir:          math3d.Vec3{Z: 1},
		}, acesLight)
		pbrExtCheckRGB(t, "ACES emissive", px, wantSDR, 3)
		if i > 0 && px.R <= prevR+25 {
			t.Fatalf("ACES emissive strength=%g red=%d did not rise clearly above previous=%d", strength, px.R, prevR)
		}
		prevR = px.R
		_ = gpuMat.Close()
	}
}

func TestPBRExtClearcoatDualLobeAttenuationAndTextures(t *testing.T) {
	const w, h = 96, 96
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(pbrExtOrthoCam(w, h, 2.4))

	renderer, err := render3d.NewExtendedPBRRenderer(nil, dev, camBuf.Layout(), render3d.ExtendedPBRRendererOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
		CullMode:    gpu.CullBack,
	})
	if err != nil {
		t.Fatalf("NewExtendedPBRRenderer: %v", err)
	}
	t.Cleanup(func() { _ = renderer.Close() })

	sphere, err := render3d.UploadMesh(nil, dev, pbrExtSphereMesh(1.0, 24, 32), render3d.MeshUpload{Label: "cc-sphere"})
	if err != nil {
		t.Fatalf("UploadMesh(sphere): %v", err)
	}
	t.Cleanup(func() { _ = sphere.Close() })

	// Head-on light along +Z puts the specular highlight dead center of the sphere,
	// while the sphere silhouette rim exercises grazing Fresnel (NdotV -> 0).
	light := render3d.PBRExtLighting{
		LightDir:       math3d.Vec3{Z: 1},
		LightColor:     math3d.Vec3{X: 2.0, Y: 2.0, Z: 2.0},
		AmbientSky:     math3d.Vec3{X: 0.12, Y: 0.12, Z: 0.12},
		AmbientGround:  math3d.Vec3{X: 0.12, Y: 0.12, Z: 0.12},
		Exposure:       1,
		ToneMap:        render3d.PBRToneMapACES,
		ViewportWidth:  w,
		ViewportHeight: h,
	}

	renderMat := func(mat render3d.PBRMaterialExt) *image.RGBA {
		gpuMat, err := renderer.CreateMaterial(nil, mat)
		if err != nil {
			t.Fatalf("CreateMaterial: %v", err)
		}
		defer func() { _ = gpuMat.Close() }()
		renderer.SetScene(math3d.Mat4Identity(), light)
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		renderer.Draw(pass, camBuf, gpuMat, sphere)
		pass.End()
		dev.Submit(enc.Finish())
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		src := sr.Image()
		cp := image.NewRGBA(src.Bounds())
		copy(cp.Pix, src.Pix)
		return cp
	}

	// 1. Matte red base (roughness=0.9) without clearcoat vs with glossy clearcoat (factor=1, roughness=0.06).
	uncoatedDesc := render3d.DefaultPBRMaterialExt()
	uncoatedDesc.BaseColorFactor = math3d.Vec4{X: 0.8, Y: 0.05, Z: 0.05, W: 1}
	uncoatedDesc.RoughnessFactor = 0.9

	coatedDesc := uncoatedDesc
	coatedDesc.ClearcoatFactor = 1.0
	coatedDesc.ClearcoatRoughnessFactor = 0.06

	uncoatedImg := renderMat(uncoatedDesc)
	coatedImg := renderMat(coatedDesc)

	// Center pixel (normal = (0,0,1), V = L = (0,0,1)): coated material must have
	// a bright white specular highlight from the crisp clearcoat lobe (high G/B too!).
	cUncoated := uncoatedImg.RGBAAt(w/2, h/2)
	cCoated := coatedImg.RGBAAt(w/2, h/2)
	if cCoated.G <= cUncoated.G+60 || cCoated.B <= cUncoated.B+60 {
		t.Fatalf("center clearcoat highlight missing: uncoated=%v coated=%v", cUncoated, cCoated)
	}

	// Verify center pixels match analytic reference.
	_, wantUncoatedCenter := render3d.EvaluatePBRMaterialExt(render3d.PBRExtSample{
		BaseColor: math3d.Vec3{X: 0.8, Y: 0.05, Z: 0.05},
		Roughness: 0.9,
		Normal:    math3d.Vec3{Z: 1},
		ViewDir:   math3d.Vec3{Z: 1},
	}, light)
	_, wantCoatedCenter := render3d.EvaluatePBRMaterialExt(render3d.PBRExtSample{
		BaseColor:          math3d.Vec3{X: 0.8, Y: 0.05, Z: 0.05},
		Roughness:          0.9,
		Clearcoat:          1.0,
		ClearcoatRoughness: 0.06,
		Normal:             math3d.Vec3{Z: 1},
		ViewDir:            math3d.Vec3{Z: 1},
	}, light)
	pbrExtCheckRGB(t, "uncoated center", cUncoated, wantUncoatedCenter, 5)
	pbrExtCheckRGB(t, "coated center", cCoated, wantCoatedCenter, 5)

	// 2. Grazing angle base attenuation (Fresnel energy conservation):
	// With zero ambient and side-lit bright base, a grazing pixel sees base darkened by (1 - clearcoat * F_coat).
	quad, err := render3d.UploadMesh(nil, dev, pbrExtQuadMesh(1, 0), render3d.MeshUpload{Label: "cc-quad"})
	if err != nil {
		t.Fatalf("UploadMesh(quad): %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	// Tilt quad normal toward +X so NdotV = cos(75 deg) ~ 0.259 (grazing) while
	// light comes from the quad normal direction (no coat direct highlight because H != N).
	grazingN := math3d.Vec3{X: float32(math.Sin(math.Pi * 75 / 180)), Z: float32(math.Cos(math.Pi * 75 / 180))}.Norm()
	grazingQuad := pbrExtQuadMesh(1, 0)
	for i := range grazingQuad.Normals {
		grazingQuad.Normals[i] = grazingN
	}
	gpuGrazingQuad, err := render3d.UploadMesh(nil, dev, grazingQuad, render3d.MeshUpload{Label: "grazing-quad"})
	if err != nil {
		t.Fatalf("UploadMesh(grazing): %v", err)
	}
	t.Cleanup(func() { _ = gpuGrazingQuad.Close() })

	grazingLight := render3d.PBRExtLighting{
		LightDir:       grazingN,
		LightColor:     math3d.Vec3{X: 2.0, Y: 2.0, Z: 2.0},
		AmbientSky:     math3d.Vec3Zero(),
		AmbientGround:  math3d.Vec3Zero(),
		Exposure:       1,
		ToneMap:        render3d.PBRToneMapLinearSRGB,
		ViewportWidth:  w,
		ViewportHeight: h,
	}
	renderGrazing := func(ccFactor float32) color.RGBA {
		m := uncoatedDesc
		m.ClearcoatFactor = ccFactor
		m.ClearcoatRoughnessFactor = 0.05
		gm, err := renderer.CreateMaterial(nil, m)
		if err != nil {
			t.Fatalf("CreateMaterial(grazing): %v", err)
		}
		defer func() { _ = gm.Close() }()
		renderer.SetScene(math3d.Mat4Identity(), grazingLight)
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		renderer.Draw(pass, camBuf, gm, gpuGrazingQuad)
		pass.End()
		dev.Submit(enc.Finish())
		return sr.Image().RGBAAt(w/2, h/2)
	}
	gUncoated := renderGrazing(0)
	gCoated := renderGrazing(1)
	if gCoated.R >= gUncoated.R-10 {
		t.Fatalf("grazing clearcoat base darkening not observed: uncoated=%v coated=%v", gUncoated, gCoated)
	}

	// 3. Texture modulation: half-mask clearcoatTexture (left=0, right=255 in R)
	// and tilted clearcoatNormalTexture verify both textures affect shading.
	maskImg := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			var r uint8
			if x >= 2 {
				r = 255
			}
			maskImg.SetNRGBA(x, y, color.NRGBA{R: r, G: 255, B: 255, A: 255})
		}
	}
	ccMaskTex, err := render3d.UploadTexture(nil, dev, maskImg, render3d.TextureUpload{Label: "cc-mask"})
	if err != nil {
		t.Fatalf("UploadTexture(ccMask): %v", err)
	}
	t.Cleanup(func() { _ = ccMaskTex.Close() })

	// Normal map tilted strongly toward +X (R=245, G=128, B=160) deflects the coat highlight away from center.
	tiltedNormTex, err := render3d.UploadTexture(nil, dev, pbrExtSolidRGBA(2, 2, color.NRGBA{R: 245, G: 128, B: 160, A: 255}), render3d.TextureUpload{Label: "cc-norm"})
	if err != nil {
		t.Fatalf("UploadTexture(ccNorm): %v", err)
	}
	t.Cleanup(func() { _ = tiltedNormTex.Close() })

	texMat := coatedDesc
	texMat.ClearcoatTexture = ccMaskTex
	gpuTexMat, err := renderer.CreateMaterial(nil, texMat)
	if err != nil {
		t.Fatalf("CreateMaterial(ccMask): %v", err)
	}
	t.Cleanup(func() { _ = gpuTexMat.Close() })

	renderer.SetScene(math3d.Mat4Identity(), light)
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	renderer.Draw(pass, camBuf, gpuTexMat, quad)
	pass.End()
	dev.Submit(enc.Finish())
	leftPx := sr.Image().RGBAAt(w/4, h/2)
	rightPx := sr.Image().RGBAAt(3*w/4, h/2)
	if rightPx.G <= leftPx.G+60 {
		t.Fatalf("clearcoatTexture mask did not modulate highlight: left=%v right=%v", leftPx, rightPx)
	}

	// Clearcoat normal map shifts highlight away from head-on reflection.
	normMat := coatedDesc
	normMat.ClearcoatNormalTexture = tiltedNormTex
	normMat.ClearcoatNormalScale = 1.0
	gpuNormMat, err := renderer.CreateMaterial(nil, normMat)
	if err != nil {
		t.Fatalf("CreateMaterial(ccNorm): %v", err)
	}
	t.Cleanup(func() { _ = gpuNormMat.Close() })

	enc = dev.BeginEncoder()
	pass = enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	renderer.Draw(pass, camBuf, gpuNormMat, quad)
	pass.End()
	dev.Submit(enc.Finish())
	tiltedPx := sr.Image().RGBAAt(w/2, h/2)
	if tiltedPx.G >= rightPx.G-60 {
		t.Fatalf("clearcoatNormalTexture did not deflect head-on coat highlight: flat=%v tilted=%v", rightPx, tiltedPx)
	}
}

func TestPBRExtTransmissionOpaqueTransmissiveAndSpecular(t *testing.T) {
	const w, h = 64, 64
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(pbrExtOrthoCam(w, h, 2))

	quad, err := render3d.UploadMesh(nil, dev, pbrExtQuadMesh(1, 0), render3d.MeshUpload{Label: "trans-quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	// Create an opaque scene background texture (bright green left half, bright blue right half)
	// simulating Pass 1's rendered opaque scene target.
	bgImg := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if x < w/2 {
				bgImg.SetNRGBA(x, y, color.NRGBA{R: 0, G: 255, B: 0, A: 255})
			} else {
				bgImg.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 255, A: 255})
			}
		}
	}
	bgTex, err := render3d.UploadTexture(nil, dev, bgImg, render3d.TextureUpload{Label: "scene-bg"})
	if err != nil {
		t.Fatalf("UploadTexture(bg): %v", err)
	}
	t.Cleanup(func() { _ = bgTex.Close() })

	renderer, err := render3d.NewExtendedPBRRenderer(nil, dev, camBuf.Layout(), render3d.ExtendedPBRRendererOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
		CullMode:    gpu.CullBack,
	})
	if err != nil {
		t.Fatalf("NewExtendedPBRRenderer: %v", err)
	}
	t.Cleanup(func() { _ = renderer.Close() })

	// Light angled slightly off-normal so specular is small in Case A, then head-on in Case B.
	offAxisLight := render3d.PBRExtLighting{
		LightDir:       math3d.Vec3{X: 0.7, Y: 0.2, Z: 0.68}.Norm(),
		LightColor:     math3d.Vec3{X: 1.5, Y: 1.5, Z: 1.5},
		AmbientSky:     math3d.Vec3{X: 0.05, Y: 0.05, Z: 0.05},
		AmbientGround:  math3d.Vec3{X: 0.05, Y: 0.05, Z: 0.05},
		Exposure:       1,
		ToneMap:        render3d.PBRToneMapLinearSRGB,
		ViewportWidth:  w,
		ViewportHeight: h,
	}

	renderTrans := func(mat render3d.PBRMaterialExt, light render3d.PBRExtLighting) *image.RGBA {
		gm, err := renderer.CreateMaterial(nil, mat)
		if err != nil {
			t.Fatalf("CreateMaterial: %v", err)
		}
		defer func() { _ = gm.Close() }()
		renderer.SetScene(math3d.Mat4Identity(), light)
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		renderer.Draw(pass, camBuf, gm, quad)
		pass.End()
		dev.Submit(enc.Finish())
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		src := sr.Image()
		cp := image.NewRGBA(src.Bounds())
		copy(cp.Pix, src.Pix)
		return cp
	}

	// Base tint (0.9, 0.5, 0.8) transmits green on left (0, 0.5, 0) and blue on right (0, 0, 0.8).
	tint := math3d.Vec3{X: 0.9, Y: 0.5, Z: 0.8}

	// Case 1: transmissionFactor = 0 -> completely opaque (background NOT visible).
	opaqueMat := render3d.DefaultPBRMaterialExt()
	opaqueMat.BaseColorFactor = math3d.Vec4{X: tint.X, Y: tint.Y, Z: tint.Z, W: 1}
	opaqueMat.RoughnessFactor = 0.05
	opaqueMat.TransmissionFactor = 0
	opaqueMat.TransmissionBackground = bgTex.Texture()
	opaqueImg := renderTrans(opaqueMat, offAxisLight)

	// Left and right halves must be identical on an opaque surface.
	opLeft := opaqueImg.RGBAAt(w/4, h/2)
	opRight := opaqueImg.RGBAAt(3*w/4, h/2)
	if pbrExtDiffByte(opLeft.R, opRight.R) > 2 || pbrExtDiffByte(opLeft.G, opRight.G) > 2 || pbrExtDiffByte(opLeft.B, opRight.B) > 2 {
		t.Fatalf("opaque transmission=0 leaked background: left=%v right=%v", opLeft, opRight)
	}

	// Case 2: transmissionFactor = 1 -> transmits tinted background (green left, blue right) matching analytic reference.
	glassMat := opaqueMat
	glassMat.TransmissionFactor = 1
	glassImg := renderTrans(glassMat, offAxisLight)

	glLeft := glassImg.RGBAAt(w/4, h/2)
	glRight := glassImg.RGBAAt(3*w/4, h/2)
	_, wantLeft := render3d.EvaluatePBRMaterialExt(render3d.PBRExtSample{
		BaseColor:       tint,
		Roughness:       0.05,
		Transmission:    1,
		BackgroundColor: math3d.Vec3{Y: 1},
		Normal:          math3d.Vec3{Z: 1},
		ViewDir:         math3d.Vec3{Z: 1},
	}, offAxisLight)
	_, wantRight := render3d.EvaluatePBRMaterialExt(render3d.PBRExtSample{
		BaseColor:       tint,
		Roughness:       0.05,
		Transmission:    1,
		BackgroundColor: math3d.Vec3{Z: 1},
		Normal:          math3d.Vec3{Z: 1},
		ViewDir:         math3d.Vec3{Z: 1},
	}, offAxisLight)
	pbrExtCheckRGB(t, "transmission=1 left (green bg * tint)", glLeft, wantLeft, 4)
	pbrExtCheckRGB(t, "transmission=1 right (blue bg * tint)", glRight, wantRight, 4)

	// Case 3: transmissionFactor = 1 with head-on directional light preserves specular highlight!
	headOnLight := offAxisLight
	headOnLight.LightDir = math3d.Vec3{Z: 1}
	headOnLight.LightColor = math3d.Vec3{X: 3, Y: 3, Z: 3}
	headOnLight.ToneMap = render3d.PBRToneMapACES
	specGlassImg := renderTrans(glassMat, headOnLight)
	specPx := specGlassImg.RGBAAt(w/4, h/2)
	_, wantSpec := render3d.EvaluatePBRMaterialExt(render3d.PBRExtSample{
		BaseColor:       tint,
		Roughness:       0.05,
		Transmission:    1,
		BackgroundColor: math3d.Vec3{Y: 1},
		Normal:          math3d.Vec3{Z: 1},
		ViewDir:         math3d.Vec3{Z: 1},
	}, headOnLight)
	pbrExtCheckRGB(t, "transmission=1 head-on specular", specPx, wantSpec, 4)
	// Specular reflection adds strong white R+B even though background is pure green (R=0, B=0).
	if specPx.R < 120 || specPx.B < 120 {
		t.Fatalf("transmission=1 lost specular reflection under head-on light: got %v", specPx)
	}

	// Case 4: Frosted glass (roughness=1.0) blurs the center seam between green and blue halves.
	frostedMat := glassMat
	frostedMat.RoughnessFactor = 1.0
	frostedMat.TransmissionBlur = 0.05
	frostedImg := renderTrans(frostedMat, offAxisLight)
	seamSharp := glassImg.RGBAAt(w/2-2, h/2)
	seamFrosted := frostedImg.RGBAAt(w/2-2, h/2)
	if seamFrosted.B <= seamSharp.B+30 {
		t.Fatalf("frosted transmission did not blur across seam: sharp=%v frosted=%v", seamSharp, seamFrosted)
	}
}

func TestPBRExtGLTFEndToEndAndMultiPassScene(t *testing.T) {
	// End-to-end glTF load -> render3d pipeline verifying all three extensions
	// rendered into one scene (two-pass transmission over a real RenderTarget
	// plus emissiveStrength HDR panel and clearcoat panel) against analytic references.
	const w, h = 96, 64

	// Build a minimal glTF document with 3 materials exercising all 3 extensions.
	gltfJSON, err := json.Marshal(map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"extensionsUsed": []string{
			"KHR_materials_emissive_strength",
			"KHR_materials_clearcoat",
			"KHR_materials_transmission",
		},
		"extensionsRequired": []string{
			"KHR_materials_emissive_strength",
			"KHR_materials_clearcoat",
			"KHR_materials_transmission",
		},
		"materials": []any{
			map[string]any{
				"name":           "NeonSign",
				"emissiveFactor": []float32{1.0, 0.5, 0.1},
				"pbrMetallicRoughness": map[string]any{
					"baseColorFactor": []float32{0.05, 0.05, 0.05, 1},
					"metallicFactor":  0,
					"roughnessFactor": 0.9,
				},
				"extensions": map[string]any{
					"KHR_materials_emissive_strength": map[string]any{"emissiveStrength": 6.0},
				},
			},
			map[string]any{
				"name": "CarPaint",
				"pbrMetallicRoughness": map[string]any{
					"baseColorFactor": []float32{0.05, 0.1, 0.7, 1},
					"metallicFactor":  0,
					"roughnessFactor": 0.85,
				},
				"extensions": map[string]any{
					"KHR_materials_clearcoat": map[string]any{
						"clearcoatFactor":          1.0,
						"clearcoatRoughnessFactor": 0.05,
					},
				},
			},
			map[string]any{
				"name": "TintedGlass",
				"pbrMetallicRoughness": map[string]any{
					"baseColorFactor": []float32{0.9, 0.95, 0.4, 1},
					"metallicFactor":  0,
					"roughnessFactor": 0.05,
				},
				"extensions": map[string]any{
					"KHR_materials_transmission": map[string]any{"transmissionFactor": 1.0},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := gltf.Load(gltfJSON, nil)
	if err != nil {
		t.Fatalf("gltf.Load: %v", err)
	}

	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(pbrExtOrthoCam(w, h, 2))

	renderer, err := render3d.NewExtendedPBRRenderer(nil, dev, camBuf.Layout(), render3d.ExtendedPBRRendererOptions{
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
		CullMode:    gpu.CullBack,
	})
	if err != nil {
		t.Fatalf("NewExtendedPBRRenderer: %v", err)
	}
	t.Cleanup(func() { _ = renderer.Close() })

	quad, err := render3d.UploadMesh(nil, dev, pbrExtQuadMesh(0.45, 0), render3d.MeshUpload{Label: "panel"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	// Pass 1: Render an opaque cyan backdrop into an offscreen gpu.RenderTarget
	// (using PBRToneMapNone so stored values are linear radiance).
	bgTarget, err := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{
		Label:       "opaque-backdrop",
		Width:       w,
		Height:      h,
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("CreateRenderTarget: %v", err)
	}
	t.Cleanup(func() { _ = bgTarget.Close() })

	enc := dev.BeginEncoder()
	bgPass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{
			Texture:    bgTarget.Color,
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: core.RGB(0x00ffff), // linear (0, 1, 1)
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			Texture:      bgTarget.Depth,
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1,
		},
	})
	bgPass.End()
	dev.Submit(enc.Finish())

	// Convert all 3 glTF materials into render3d.PBRMaterialExt.
	neonDesc := render3d.PBRMaterialExtFromGLTF(doc.Materials[0])
	paintDesc := render3d.PBRMaterialExtFromGLTF(doc.Materials[1])
	glassDesc := render3d.PBRMaterialExtFromGLTF(doc.Materials[2])
	glassDesc.TransmissionBackground = bgTarget.Color

	neonMat, err := renderer.CreateMaterial(nil, neonDesc)
	if err != nil {
		t.Fatalf("CreateMaterial(neon): %v", err)
	}
	t.Cleanup(func() { _ = neonMat.Close() })

	paintMat, err := renderer.CreateMaterial(nil, paintDesc)
	if err != nil {
		t.Fatalf("CreateMaterial(paint): %v", err)
	}
	t.Cleanup(func() { _ = paintMat.Close() })

	glassMat, err := renderer.CreateMaterial(nil, glassDesc)
	if err != nil {
		t.Fatalf("CreateMaterial(glass): %v", err)
	}
	t.Cleanup(func() { _ = glassMat.Close() })

	light := render3d.PBRExtLighting{
		LightDir:       math3d.Vec3{Z: 1},
		LightColor:     math3d.Vec3{X: 1.8, Y: 1.8, Z: 1.8},
		AmbientSky:     math3d.Vec3{X: 0.08, Y: 0.08, Z: 0.08},
		AmbientGround:  math3d.Vec3{X: 0.08, Y: 0.08, Z: 0.08},
		Exposure:       1,
		ToneMap:        render3d.PBRToneMapACES,
		ViewportWidth:  w,
		ViewportHeight: h,
	}

	// Place three panels side by side at X = -1, 0, +1 in one render pass using CreateScene.
	sceneLeft, err := renderer.CreateScene(nil, math3d.Translate(math3d.Vec3{X: -1}), light)
	if err != nil {
		t.Fatalf("CreateScene(left): %v", err)
	}
	t.Cleanup(func() { _ = sceneLeft.Close() })

	sceneMid, err := renderer.CreateScene(nil, math3d.Mat4Identity(), light)
	if err != nil {
		t.Fatalf("CreateScene(mid): %v", err)
	}
	t.Cleanup(func() { _ = sceneMid.Close() })

	sceneRight, err := renderer.CreateScene(nil, math3d.Translate(math3d.Vec3{X: 1}), light)
	if err != nil {
		t.Fatalf("CreateScene(right): %v", err)
	}
	t.Cleanup(func() { _ = sceneRight.Close() })

	// Pass 2: Draw all three panels in one pass.
	enc = dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	renderer.DrawWithScene(pass, camBuf, sceneLeft, neonMat, quad)
	renderer.DrawWithScene(pass, camBuf, sceneMid, paintMat, quad)
	renderer.DrawWithScene(pass, camBuf, sceneRight, glassMat, quad)
	pass.End()
	dev.Submit(enc.Finish())
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	// Sample centers of the three panels: X=-1 -> px 16, X=0 -> px 48, X=+1 -> px 80 (for ortho width=3 on 96px).
	_, wantNeon := render3d.EvaluatePBRMaterialExt(render3d.PBRExtSample{
		BaseColor:        math3d.Vec3{X: 0.05, Y: 0.05, Z: 0.05},
		Roughness:        0.9,
		Emissive:         math3d.Vec3{X: 1.0, Y: 0.5, Z: 0.1},
		EmissiveStrength: 6.0,
		Normal:           math3d.Vec3{Z: 1},
		ViewDir:          math3d.Vec3{Z: 1},
	}, light)
	_, wantPaint := render3d.EvaluatePBRMaterialExt(render3d.PBRExtSample{
		BaseColor:          math3d.Vec3{X: 0.05, Y: 0.1, Z: 0.7},
		Roughness:          0.85,
		Clearcoat:          1.0,
		ClearcoatRoughness: 0.05,
		Normal:             math3d.Vec3{Z: 1},
		ViewDir:            math3d.Vec3{Z: 1},
	}, light)
	_, wantGlass := render3d.EvaluatePBRMaterialExt(render3d.PBRExtSample{
		BaseColor:       math3d.Vec3{X: 0.9, Y: 0.95, Z: 0.4},
		Roughness:       0.05,
		Transmission:    1.0,
		BackgroundColor: math3d.Vec3{Y: 1, Z: 1},
		Normal:          math3d.Vec3{Z: 1},
		ViewDir:         math3d.Vec3{Z: 1},
	}, light)

	pbrExtCheckRGB(t, "glTF NeonSign", img.RGBAAt(16, h/2), wantNeon, 4)
	pbrExtCheckRGB(t, "glTF CarPaint", img.RGBAAt(48, h/2), wantPaint, 4)
	pbrExtCheckRGB(t, "glTF TintedGlass", img.RGBAAt(80, h/2), wantGlass, 4)
}

func TestPBRExtValidationAndUniformEncoding(t *testing.T) {
	def := render3d.DefaultPBRMaterialExt()
	if err := def.Validate(); err != nil {
		t.Fatalf("DefaultPBRMaterialExt().Validate(): %v", err)
	}

	var matBuf [render3d.PBRExtMaterialUniformSize]byte
	if allocs := testing.AllocsPerRun(100, func() { def.EncodeUniform(matBuf[:]) }); allocs != 0 {
		t.Fatalf("EncodeUniform allocated %v times", allocs)
	}

	var sceneBuf [render3d.PBRExtSceneUniformSize]byte
	light := render3d.DefaultPBRExtLighting(128, 96)
	if allocs := testing.AllocsPerRun(100, func() { light.EncodeSceneUniform(math3d.Mat4Identity(), sceneBuf[:]) }); allocs != 0 {
		t.Fatalf("EncodeSceneUniform allocated %v times", allocs)
	}

	badCases := []struct {
		name string
		edit func(m *render3d.PBRMaterialExt)
	}{
		{"negative emissive strength", func(m *render3d.PBRMaterialExt) { m.EmissiveStrength = -1 }},
		{"NaN emissive strength", func(m *render3d.PBRMaterialExt) { m.EmissiveStrength = float32(math.NaN()) }},
		{"clearcoat > 1", func(m *render3d.PBRMaterialExt) { m.ClearcoatFactor = 1.2 }},
		{"clearcoat roughness < 0", func(m *render3d.PBRMaterialExt) { m.ClearcoatRoughnessFactor = -0.1 }},
		{"transmission > 1", func(m *render3d.PBRMaterialExt) { m.TransmissionFactor = 1.5 }},
		{"negative transmission blur", func(m *render3d.PBRMaterialExt) { m.TransmissionBlur = -0.01 }},
		{"metallic > 1", func(m *render3d.PBRMaterialExt) { m.MetallicFactor = 2 }},
		{"roughness < 0", func(m *render3d.PBRMaterialExt) { m.RoughnessFactor = -0.5 }},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			m := render3d.DefaultPBRMaterialExt()
			tc.edit(&m)
			err := m.Validate()
			if !errors.Is(err, render3d.ErrInvalidPBRExtMaterial) {
				t.Fatalf("Validate() = %v, want ErrInvalidPBRExtMaterial", err)
			}
		})
	}
}
