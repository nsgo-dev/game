package render3d_test

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const customMatWGSL3D = `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) normal: vec3<f32>,
	@location(1) uv: vec2<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.normal = in.normal;
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let tex = textureSample(albedo, albedo_sampler, in.uv);
	let n = normalize(in.normal);
	let l = normalize(material.light_dir);
	let ndl = max(dot(n, l), 0.0);
	let shade = (0.3 + 0.7 * ndl) * material.intensity;
	let rgb = clamp(tex.rgb * material.tint.rgb * shade, vec3<f32>(0.0), vec3<f32>(1.0));
	return vec4<f32>(rgb, tex.a * material.tint.a);
}
`

const customMatWGSLClip = `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) normal: vec3<f32>,
	@location(1) uv: vec2<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = vec4<f32>(in.pos, 1.0);
	out.normal = in.normal;
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let tex = textureSample(albedo, albedo_sampler, in.uv);
	let ndl = max(dot(normalize(in.normal), normalize(material.light_dir)), 0.0);
	let rgb = clamp(tex.rgb * material.tint.rgb * (0.2 + 0.8 * ndl) * material.intensity, vec3<f32>(0.0), vec3<f32>(1.0));
	return vec4<f32>(rgb, tex.a * material.tint.a);
}
`

const customMatKageSurface = `
//kage:unit pixels
package main

var LightDir vec3
var Tint vec4
var Intensity float
var Bands int
var Highlight bool

func quantize(v float, steps int) float {
	if steps <= 1 {
		return v
	}
	f := float(steps)
	return floor(v*f+0.5) / f
}

func Surface(uv vec2, normal vec3, worldPos vec3) vec4 {
	tex := sample(albedo, uv)
	n := normalize(normal)
	l := normalize(LightDir)
	ndl := max(dot(n, l), 0.0)
	q := quantize(ndl, Bands)
	shade := (0.25 + 0.75*q) * Intensity
	col := tex.rgb * Tint.rgb * shade
	if Highlight {
		rim := clamp(1.0-abs(n.y), 0.0, 1.0) * 0.2
		col += vec3(rim, rim, rim)
	}
	return vec4(clamp(col, vec3(0.0), vec3(1.0)), tex.a*Tint.a)
}
`

func customMatPtrF64(v float64) *float64 { return &v }

func customMatWGSLParams() []render3d.ShaderParamDesc {
	return []render3d.ShaderParamDesc{
		{Name: "intensity", Kind: render3d.ParamFloat, Default: float32(1.0), Min: customMatPtrF64(0), Max: customMatPtrF64(4)},
		{Name: "light_dir", Kind: render3d.ParamVec3, Default: math3d.Vec3{X: 0.4, Y: 0.85, Z: 0.35}},
		{Name: "tint", Kind: render3d.ParamColor, Default: core.RGB(0xf0c878)},
		{Name: "albedo", Kind: render3d.ParamTexture},
	}
}

func customMatKageParams() []render3d.ShaderParamDesc {
	return []render3d.ShaderParamDesc{
		{Name: "LightDir", Kind: render3d.ParamVec3, Default: math3d.Vec3{X: 0.45, Y: 0.75, Z: 0.5}},
		{Name: "Tint", Kind: render3d.ParamColor, Default: core.RGB(0x55d0ff)},
		{Name: "Intensity", Kind: render3d.ParamFloat, Default: float32(1.0), Min: customMatPtrF64(0), Max: customMatPtrF64(4)},
		{Name: "Bands", Kind: render3d.ParamInt, Default: int32(4), Min: customMatPtrF64(1), Max: customMatPtrF64(16)},
		{Name: "Highlight", Kind: render3d.ParamBool, Default: true},
		{Name: "albedo", Kind: render3d.ParamTexture},
	}
}

func customMatCheckerImage(size int, a, b color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	half := max(size/2, 1)
	for y := range size {
		for x := range size {
			if ((x/half)+(y/half))%2 == 0 {
				img.SetNRGBA(x, y, a)
			} else {
				img.SetNRGBA(x, y, b)
			}
		}
	}
	return img
}

func customMatCubeMesh(center math3d.Vec3, half float32) render3d.Mesh {
	type face struct{ n, u, v math3d.Vec3 }
	faces := [...]face{
		{math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3{Y: 1}},
		{math3d.Vec3{X: -1}, math3d.Vec3{Z: 1}, math3d.Vec3{Y: 1}},
		{math3d.Vec3{Y: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}},
		{math3d.Vec3{Y: -1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: 1}},
		{math3d.Vec3{Z: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Y: 1}},
		{math3d.Vec3{Z: -1}, math3d.Vec3{X: -1}, math3d.Vec3{Y: 1}},
	}
	var m render3d.Mesh
	for _, f := range faces {
		base := uint32(len(m.Positions))
		c := center.Add(f.n.Scale(half))
		u, v := f.u.Scale(half), f.v.Scale(half)
		m.Positions = append(m.Positions,
			c.Sub(u).Sub(v),
			c.Add(u).Sub(v),
			c.Add(u).Add(v),
			c.Sub(u).Add(v),
		)
		m.Normals = append(m.Normals, f.n, f.n, f.n, f.n)
		m.UVs = append(m.UVs,
			math3d.Vec2{X: 0, Y: 1},
			math3d.Vec2{X: 1, Y: 1},
			math3d.Vec2{X: 1, Y: 0},
			math3d.Vec2{X: 0, Y: 0},
		)
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	return m
}

func customMatClipQuad() render3d.Mesh {
	n := math3d.Vec3{Z: 1}
	return render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: -0.6, Y: -0.6, Z: 0.5},
			{X: 0.6, Y: -0.6, Z: 0.5},
			{X: 0.6, Y: 0.6, Z: 0.5},
			{X: -0.6, Y: 0.6, Z: 0.5},
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

func customMatNearColor(a, b color.RGBA, tol uint8) bool {
	diff := func(x, y uint8) uint8 {
		if x > y {
			return x - y
		}
		return y - x
	}
	return diff(a.R, b.R) <= tol && diff(a.G, b.G) <= tol && diff(a.B, b.B) <= tol && diff(a.A, b.A) <= tol
}

func TestCustomMaterialWGSLMatchesReference(t *testing.T) {
	const (
		width  = 128
		height = 96
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	mat, err := render3d.NewCustomMaterial(nil, dev, render3d.CustomShaderDesc{
		Label:        "wgsl-ref",
		Language:     render3d.ShaderWGSL,
		Source:       customMatWGSL3D,
		CameraLayout: camBuf.Layout(),
		Params:       customMatWGSLParams(),
		Cull:         gpu.CullBack,
		FrontFace:    gpu.FrontCCW,
		DepthFormat:  gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewCustomMaterial WGSL: %v", err)
	}
	t.Cleanup(func() { _ = mat.Close() })

	texImg := customMatCheckerImage(8, color.NRGBA{R: 245, G: 245, B: 245, A: 255}, color.NRGBA{R: 70, G: 110, B: 170, A: 255})
	tex, err := render3d.UploadTexture(nil, dev, texImg, render3d.TextureUpload{Label: "wgsl-checker"})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}
	t.Cleanup(func() { _ = tex.Close() })

	samp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestRepeat())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	t.Cleanup(func() { _ = samp.Close() })

	if err := mat.SetTexture("albedo", tex, samp); err != nil {
		t.Fatalf("SetTexture: %v", err)
	}
	if err := mat.Sync(dev); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	cube, err := render3d.UploadMesh(nil, dev, customMatCubeMesh(math3d.Vec3{Y: 0.6}, 0.8), render3d.MeshUpload{Label: "cube"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = cube.Close() })

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 2.1, Y: 1.9, Z: 2.7}, math3d.DegToRad(45), float32(width)/float32(height), 0.1, 30)
	cam.LookAt(math3d.Vec3{Y: 0.55}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "custom-wgsl-ref",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	mat.Draw(pass, cube)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	assertReference(t, "custom_material_wgsl", img)

	// Corner is clear color; top face's light checker tile is warm gold-tinted and lit.
	wantPixel(t, img, 2, 2, color.RGBA{R: 16, G: 20, B: 28, A: 255})
	if s, _, ok := cam.WorldToScreen(math3d.Vec3{X: 0.3, Y: 1.4, Z: 0.3}, width, height); !ok {
		t.Fatal("WGSL cube top face not visible")
	} else if top := img.RGBAAt(int(s.X), int(s.Y)); top.R <= top.B || top.R < 140 {
		t.Errorf("WGSL cube top light checker pixel (%d,%d) = %v, want warm gold-tinted lit pixel", int(s.X), int(s.Y), top)
	}
	if s, _, ok := cam.WorldToScreen(math3d.Vec3{X: 0.3, Y: 0.85, Z: 0.8}, width, height); !ok {
		t.Fatal("WGSL cube front dark checker not visible")
	} else if front := img.RGBAAt(int(s.X), int(s.Y)); front == (color.RGBA{R: 16, G: 20, B: 28, A: 255}) {
		t.Errorf("WGSL cube front pixel (%d,%d) = clear color, want shaded cube", int(s.X), int(s.Y))
	}
}

func TestCustomMaterialKageMatchesReference(t *testing.T) {
	const (
		width  = 128
		height = 96
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	mat, err := render3d.NewCustomMaterial(nil, dev, render3d.CustomShaderDesc{
		Label:        "kage-ref",
		Language:     render3d.ShaderKage,
		Source:       customMatKageSurface,
		CameraLayout: camBuf.Layout(),
		Params:       customMatKageParams(),
		Cull:         gpu.CullBack,
		FrontFace:    gpu.FrontCCW,
		DepthFormat:  gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewCustomMaterial Kage: %v", err)
	}
	t.Cleanup(func() { _ = mat.Close() })

	texImg := customMatCheckerImage(8, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, color.NRGBA{R: 140, G: 180, B: 220, A: 255})
	tex, err := render3d.UploadTexture(nil, dev, texImg, render3d.TextureUpload{Label: "kage-checker"})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}
	t.Cleanup(func() { _ = tex.Close() })

	samp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestRepeat())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	t.Cleanup(func() { _ = samp.Close() })

	if err := mat.SetTexture("albedo", tex, samp); err != nil {
		t.Fatalf("SetTexture: %v", err)
	}
	if err := mat.Sync(dev); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	cube, err := render3d.UploadMesh(nil, dev, customMatCubeMesh(math3d.Vec3{Y: 0.6}, 0.8), render3d.MeshUpload{Label: "cube"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = cube.Close() })

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 2.1, Y: 1.9, Z: 2.7}, math3d.DegToRad(45), float32(width)/float32(height), 0.1, 30)
	cam.LookAt(math3d.Vec3{Y: 0.55}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "custom-kage-ref",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	mat.Draw(pass, cube)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	assertReference(t, "custom_material_kage", img)

	// Corner is clear color; cube center is cyan-tinted cel-shaded pixels.
	wantPixel(t, img, 2, 2, color.RGBA{R: 16, G: 20, B: 28, A: 255})
	center := img.RGBAAt(width/2, height/2)
	if center.B <= center.R || center.B < 90 {
		t.Errorf("Kage cube center pixel = %v, want cyan-tinted lit pixel", center)
	}
}

func TestCustomMaterialDrawsInGPUViewProfiles(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	langs := []struct {
		name      string
		lang      render3d.ShaderLanguage
		source    string
		params    []render3d.ShaderParamDesc
		wantColor color.RGBA
	}{
		{
			name:   "wgsl",
			lang:   render3d.ShaderWGSL,
			source: customMatWGSLClip,
			params: []render3d.ShaderParamDesc{
				{Name: "intensity", Kind: render3d.ParamFloat, Default: float32(1.0)},
				{Name: "light_dir", Kind: render3d.ParamVec3, Default: math3d.Vec3{Z: 1}},
				{Name: "tint", Kind: render3d.ParamColor, Default: core.RGB(0x40c080)},
				{Name: "albedo", Kind: render3d.ParamTexture}, // uses default 1x1 white fallback
			},
			wantColor: color.RGBA{R: 64, G: 192, B: 128, A: 255},
		},
		{
			name:   "kage",
			lang:   render3d.ShaderKage,
			source: customMatKageSurface,
			params: []render3d.ShaderParamDesc{
				{Name: "LightDir", Kind: render3d.ParamVec3, Default: math3d.Vec3{Z: 1}},
				{Name: "Tint", Kind: render3d.ParamColor, Default: core.RGB(0xc85030)},
				{Name: "Intensity", Kind: render3d.ParamFloat, Default: float32(1.0)},
				{Name: "Bands", Kind: render3d.ParamInt, Default: int32(1)},
				{Name: "Highlight", Kind: render3d.ParamBool, Default: false},
				{Name: "albedo", Kind: render3d.ParamTexture}, // uses default 1x1 white fallback
			},
			wantColor: color.RGBA{R: 200, G: 80, B: 48, A: 255},
		},
	}

	for _, p := range profiles {
		for _, tc := range langs {
			t.Run(p.name+"/"+tc.name, func(t *testing.T) {
				const size = 64
				var (
					mesh *render3d.GPUMesh
					mat  *render3d.CustomMaterial
				)
				opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, p.opts...)
				app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
					return gpu.View(cx, gpu.ViewProps{
						TestID:      "viewport",
						Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
						DepthFormat: gpu.FormatDepth24Plus,
						OnReady: func(dev *gpu.Device) {
							var err error
							mesh, err = render3d.UploadMesh(cx, dev, customMatClipQuad(), render3d.MeshUpload{Label: "quad"})
							if err != nil {
								t.Fatalf("UploadMesh: %v", err)
							}
							mat, err = render3d.NewCustomMaterial(cx, dev, render3d.CustomShaderDesc{
								Label:       tc.name,
								Language:    tc.lang,
								Source:      tc.source,
								Params:      tc.params,
								DepthFormat: gpu.FormatDepth24Plus,
							})
							if err != nil {
								t.Fatalf("NewCustomMaterial: %v", err)
							}
						},
						OnFrame: func(frame *gpu.Frame) {
							pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
							pass.ClearColor(core.RGB(0x10141c))
							pass.ClearDepth(1)
							mat.Draw(pass, mesh)
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
				wantPixel(t, img, 2, 2, color.RGBA{R: 16, G: 20, B: 28, A: 255})
				wantPixel(t, img, size/2, size/2, tc.wantColor)
			})
		}
	}
}

func TestCustomMaterialCompilesForEveryBackend(t *testing.T) {
	// Verify CompileCheck passes for both WGSL and Kage shaders, and inspect the
	// PipelineCache artifacts to confirm all 5 NSGo backend targets (MSL, HLSL,
	// GLSL 3.30, GLSL ES 3.00 / WebGL 2, and WGSL) emit non-empty sources.
	cases := []struct {
		name string
		desc render3d.CustomShaderDesc
	}{
		{
			name: "wgsl",
			desc: render3d.CustomShaderDesc{
				Label:    "backend-wgsl",
				Language: render3d.ShaderWGSL,
				Source:   customMatWGSLClip,
				Params:   customMatWGSLParams(),
			},
		},
		{
			name: "kage",
			desc: render3d.CustomShaderDesc{
				Label:    "backend-kage",
				Language: render3d.ShaderKage,
				Source:   customMatKageSurface,
				Params:   customMatKageParams(),
			},
		},
	}

	type cachedArtifacts struct {
		Artifacts struct {
			MSLSource          string `json:"msl_source"`
			HLSLVertexSource   string `json:"hlsl_vertex_source"`
			HLSLPixelSource    string `json:"hlsl_pixel_source"`
			GLSLVertexSource   string `json:"glsl_vertex_source"`
			GLSLFragmentSource string `json:"glsl_fragment_source"`
			GLESVertexSource   string `json:"gles_vertex_source"`
			GLESFragmentSource string `json:"gles_fragment_source"`
			WGSLSource         string `json:"wgsl_source"`
		} `json:"artifacts"`
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := render3d.CompileCheck(tc.desc.Source, tc.desc.Language, tc.desc.Params); err != nil {
				t.Fatalf("CompileCheck(%s): %v", tc.name, err)
			}

			cacheDir := t.TempDir()
			cache, err := gpu.NewPipelineCache(cacheDir)
			if err != nil {
				t.Fatalf("NewPipelineCache: %v", err)
			}
			sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
			t.Cleanup(sr.Close)
			dev := sr.Device()
			dev.SetPipelineCache(cache)

			mat, err := render3d.NewCustomMaterial(nil, dev, tc.desc)
			if err != nil {
				t.Fatalf("NewCustomMaterial(%s): %v", tc.name, err)
			}
			t.Cleanup(func() { _ = mat.Close() })

			entries, err := os.ReadDir(cacheDir)
			if err != nil {
				t.Fatalf("ReadDir cache: %v", err)
			}
			var jsonFiles []string
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".json") {
					jsonFiles = append(jsonFiles, filepath.Join(cacheDir, e.Name()))
				}
			}
			if len(jsonFiles) == 0 {
				t.Fatal("expected pipeline cache to emit compiled backend artifact JSON")
			}
			raw, err := os.ReadFile(jsonFiles[0])
			if err != nil {
				t.Fatalf("ReadFile artifact: %v", err)
			}
			var rec cachedArtifacts
			if err := json.Unmarshal(raw, &rec); err != nil {
				t.Fatalf("Unmarshal artifact: %v", err)
			}
			art := rec.Artifacts
			if art.MSLSource == "" {
				t.Error("missing compiled MSL source (macOS/iOS)")
			}
			if art.HLSLVertexSource == "" || art.HLSLPixelSource == "" {
				t.Error("missing compiled HLSL sources (Windows D3D11)")
			}
			if art.GLSLVertexSource == "" || art.GLSLFragmentSource == "" {
				t.Error("missing compiled GLSL 3.30 sources (Linux OpenGL)")
			}
			if art.GLESVertexSource == "" || art.GLESFragmentSource == "" {
				t.Error("missing compiled GLSL ES 3.00 sources (WebGL 2 / Android GLES 3.0)")
			}
			if art.WGSLSource == "" {
				t.Error("missing compiled WGSL source (WebGPU)")
			}
		})
	}
}

func TestCustomMaterialDynamicParameterUpdates(t *testing.T) {
	const size = 32
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	mat, err := render3d.NewCustomMaterial(nil, dev, render3d.CustomShaderDesc{
		Label:    "dynamic",
		Language: render3d.ShaderWGSL,
		Source:   customMatWGSLClip,
		Params: []render3d.ShaderParamDesc{
			{Name: "intensity", Kind: render3d.ParamFloat, Default: float32(1.0)},
			{Name: "light_dir", Kind: render3d.ParamVec3, Default: math3d.Vec3{Z: 1}},
			{Name: "tint", Kind: render3d.ParamColor, Default: core.RGB(0xff0000)},
			{Name: "albedo", Kind: render3d.ParamTexture},
		},
	})
	if err != nil {
		t.Fatalf("NewCustomMaterial: %v", err)
	}
	t.Cleanup(func() { _ = mat.Close() })

	quad, err := render3d.UploadMesh(nil, dev, customMatClipQuad(), render3d.MeshUpload{Label: "quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	drawFrame := func() color.RGBA {
		if err := mat.Sync(dev); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		})
		mat.Draw(pass, quad)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software device: %v", err)
		}
		return sr.Image().RGBAAt(size/2, size/2)
	}

	// Frame 1: default red tint + 1x1 white fallback texture.
	c1 := drawFrame()
	if !customMatNearColor(c1, color.RGBA{R: 255, G: 0, B: 0, A: 255}, 3) {
		t.Fatalf("frame 1 center = %v, want red", c1)
	}

	// Frame 2: change tint to green and intensity to 0.5 via typed setters.
	if err := mat.SetColor("tint", core.RGB(0x00ff00)); err != nil {
		t.Fatalf("SetColor: %v", err)
	}
	if err := mat.SetFloat("intensity", 0.5); err != nil {
		t.Fatalf("SetFloat: %v", err)
	}
	if !mat.Dirty() {
		t.Fatal("expected Dirty() == true after parameter mutation")
	}
	c2 := drawFrame()
	if !customMatNearColor(c2, color.RGBA{R: 0, G: 128, B: 0, A: 255}, 3) {
		t.Fatalf("frame 2 center = %v, want half-green", c2)
	}
	if mat.Dirty() {
		t.Fatal("expected Dirty() == false after Sync")
	}

	// Setting identical value must NOT mark dirty.
	if err := mat.SetFloat("intensity", 0.5); err != nil {
		t.Fatalf("SetFloat same: %v", err)
	}
	if mat.Dirty() {
		t.Fatal("setting unchanged value marked material dirty")
	}

	// Frame 3: swap in a solid blue texture and apply tint white via ApplyMap + Update.
	blueTex, err := render3d.UploadTexturePixels(nil, dev, render3d.PixelBuffer{
		Width:  1,
		Height: 1,
		Format: gpu.FormatRGBA8Unorm,
		Pixels: []byte{0, 0, 255, 255},
	}, render3d.TextureUpload{Label: "blue"})
	if err != nil {
		t.Fatalf("UploadTexturePixels: %v", err)
	}
	t.Cleanup(func() { _ = blueTex.Close() })

	if err := mat.ApplyMap(map[string]any{
		"intensity": 1.0,
		"tint":      "#ffffff",
		"albedo":    blueTex,
	}); err != nil {
		t.Fatalf("ApplyMap: %v", err)
	}
	if err := mat.Update(dev); err != nil {
		t.Fatalf("Update: %v", err)
	}
	c3 := drawFrame()
	if !customMatNearColor(c3, color.RGBA{R: 0, G: 0, B: 255, A: 255}, 3) {
		t.Fatalf("frame 3 center = %v, want blue", c3)
	}

	// Frame 4: reset texture back to nil (restores 1x1 white fallback).
	if err := mat.SetTexture("albedo", nil, nil); err != nil {
		t.Fatalf("SetTexture(nil): %v", err)
	}
	c4 := drawFrame()
	if !customMatNearColor(c4, color.RGBA{R: 255, G: 255, B: 255, A: 255}, 3) {
		t.Fatalf("frame 4 center = %v, want white fallback", c4)
	}
}

func TestCustomMaterialStd140LayoutAndGPUReadback(t *testing.T) {
	// Exercise exact std140 alignment rules for: float + vec3 + vec2 + vec4 + int + bool.
	params := []render3d.ShaderParamDesc{
		{Name: "p_float", Kind: render3d.ParamFloat, Default: float32(0.25)},
		{Name: "p_vec3", Kind: render3d.ParamVec3, Default: math3d.Vec3{X: 0.1, Y: 0.2, Z: 0.3}},
		{Name: "p_vec2", Kind: render3d.ParamVec2, Default: math3d.Vec2{X: 0.4, Y: 0.5}},
		{Name: "p_vec4", Kind: render3d.ParamVec4, Default: math3d.Vec4{X: 0.6, Y: 0.7, Z: 0.8, W: 1.0}},
		{Name: "p_int", Kind: render3d.ParamInt, Default: int32(2)},
		{Name: "p_bool", Kind: render3d.ParamBool, Default: true},
		{Name: "p_tex", Kind: render3d.ParamTexture},
	}

	layout, err := render3d.ComputeUniformLayout(params)
	if err != nil {
		t.Fatalf("ComputeUniformLayout: %v", err)
	}
	wantOffsets := map[string]int{
		"p_float": 0,  // align 4, size 4 -> [0..4)
		"p_vec3":  16, // align 16, size 12 -> [16..28)
		"p_vec2":  32, // align 8, size 8 -> [32..40)
		"p_vec4":  48, // align 16, size 16 -> [48..64)
		"p_int":   64, // align 4, size 4 -> [64..68)
		"p_bool":  68, // align 4, size 4 -> [68..72)
	}
	if layout.UniformSize != 80 {
		t.Fatalf("UniformSize = %d, want 80", layout.UniformSize)
	}
	for name, wantOff := range wantOffsets {
		if got := layout.Offsets[name]; got != wantOff {
			t.Errorf("offset(%s) = %d, want %d", name, got, wantOff)
		}
	}

	// Render a shader that reads every field at its std140 offset and encodes them into RGB.
	const allTypesWGSL = `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}
struct VertexOut {
	@builtin(position) clip: vec4<f32>,
}
@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = vec4<f32>(in.pos, 1.0);
	return out;
}
@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let r = material.p_float + material.p_vec3.y; // 0.25 + 0.2 = 0.45
	let g = material.p_vec2.x + f32(material.p_int) * 0.1; // 0.4 + 0.2 = 0.60
	var b = material.p_vec4.z; // 0.80
	if (material.p_bool != 0u) {
		b = b * 0.5; // 0.40
	}
	let t = textureSample(p_tex, p_tex_sampler, vec2<f32>(0.5, 0.5));
	return vec4<f32>(r * t.r, g * t.g, b * t.b, 1.0);
}
`
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	mat, err := render3d.NewCustomMaterial(nil, dev, render3d.CustomShaderDesc{
		Label:    "std140",
		Language: render3d.ShaderWGSL,
		Source:   allTypesWGSL,
		Params:   params,
	})
	if err != nil {
		t.Fatalf("NewCustomMaterial: %v", err)
	}
	t.Cleanup(func() { _ = mat.Close() })

	if mat.UniformSize() != 80 {
		t.Fatalf("mat.UniformSize() = %d, want 80", mat.UniformSize())
	}
	for name, wantOff := range wantOffsets {
		off, ok := mat.ParamOffset(name)
		if !ok || off != wantOff {
			t.Errorf("ParamOffset(%s) = (%d, %v), want (%d, true)", name, off, ok, wantOff)
		}
	}
	if tb, sb, ok := mat.ParamBinding("p_tex"); !ok || tb != 1 || sb != 2 {
		t.Errorf("ParamBinding(p_tex) = (%d, %d, %v), want (1, 2, true)", tb, sb, ok)
	}

	raw := mat.UniformBytes()
	f32At := func(off int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(raw[off:])) }
	u32At := func(off int) uint32 { return binary.LittleEndian.Uint32(raw[off:]) }
	if f32At(0) != 0.25 || f32At(16) != 0.1 || f32At(20) != 0.2 || f32At(24) != 0.3 ||
		f32At(32) != 0.4 || f32At(36) != 0.5 || f32At(48) != 0.6 || f32At(60) != 1.0 ||
		int32(u32At(64)) != 2 || u32At(68) != 1 {
		t.Fatalf("unexpected encoded std140 uniform bytes")
	}

	quad, err := render3d.UploadMesh(nil, dev, customMatClipQuad(), render3d.MeshUpload{Label: "quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0)}},
	})
	mat.Draw(pass, quad)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software device: %v", err)
	}

	// Expected: R = 0.45*255 ≈ 115, G = 0.60*255 ≈ 153, B = 0.40*255 ≈ 102
	wantPixel(t, sr.Image(), 8, 8, color.RGBA{R: 115, G: 153, B: 102, A: 255})
}

func TestCustomMaterialKageClassicFragmentAndAutoParams(t *testing.T) {
	// Kage classic Fragment(dst, pos) signature with omitted Params (auto-inferred from var declarations).
	const kageClassic = `
var Color vec4
var Gain float

func Fragment(dst vec4, pos vec2) vec4 {
	return vec4(Color.rgb * Gain, Color.a)
}
`
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	mat, err := render3d.NewCustomMaterial(nil, dev, render3d.CustomShaderDesc{
		Label:    "kage-classic",
		Language: render3d.ShaderKage,
		Source:   kageClassic,
	})
	if err != nil {
		t.Fatalf("NewCustomMaterial kage classic: %v", err)
	}
	t.Cleanup(func() { _ = mat.Close() })

	if err := mat.SetVec4("Color", math3d.Vec4{X: 0.4, Y: 0.8, Z: 0.2, W: 1.0}); err != nil {
		t.Fatalf("SetVec4 Color: %v", err)
	}
	if err := mat.SetFloat("Gain", 0.5); err != nil {
		t.Fatalf("SetFloat Gain: %v", err)
	}
	if err := mat.Sync(dev); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	quad, err := render3d.UploadMesh(nil, dev, customMatClipQuad(), render3d.MeshUpload{Label: "quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0)}},
	})
	mat.Draw(pass, quad)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()

	// 0.4*0.5=0.2 (51), 0.8*0.5=0.4 (102), 0.2*0.5=0.1 (26)
	wantPixel(t, sr.Image(), 8, 8, color.RGBA{R: 51, G: 102, B: 26, A: 255})
}

func TestCustomMaterialValidationErrors(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	t.Run("nil device", func(t *testing.T) {
		_, err := render3d.NewCustomMaterial(nil, nil, render3d.CustomShaderDesc{
			Language: render3d.ShaderWGSL,
			Source:   customMatWGSLClip,
			Params:   customMatWGSLParams(),
		})
		if !errors.Is(err, gpu.ErrDeviceLost) {
			t.Fatalf("err = %v, want gpu.ErrDeviceLost", err)
		}
	})

	t.Run("descriptor and compile errors", func(t *testing.T) {
		nan := math.NaN()
		badDescs := []struct {
			name string
			desc render3d.CustomShaderDesc
		}{
			{"empty source", render3d.CustomShaderDesc{Language: render3d.ShaderWGSL}},
			{"invalid language", render3d.CustomShaderDesc{Language: render3d.ShaderLanguage(99), Source: customMatWGSLClip}},
			{"duplicate param", render3d.CustomShaderDesc{
				Language: render3d.ShaderWGSL,
				Source:   customMatWGSLClip,
				Params: []render3d.ShaderParamDesc{
					{Name: "x", Kind: render3d.ParamFloat},
					{Name: "x", Kind: render3d.ParamFloat},
				},
			}},
			{"invalid kind", render3d.CustomShaderDesc{
				Language: render3d.ShaderWGSL,
				Source:   customMatWGSLClip,
				Params:   []render3d.ShaderParamDesc{{Name: "x"}},
			}},
			{"reserved ident", render3d.CustomShaderDesc{
				Language: render3d.ShaderWGSL,
				Source:   customMatWGSLClip,
				Params:   []render3d.ShaderParamDesc{{Name: "material", Kind: render3d.ParamFloat}},
			}},
			{"sampler name collision", render3d.CustomShaderDesc{
				Language: render3d.ShaderWGSL,
				Source:   customMatWGSLClip,
				Params: []render3d.ShaderParamDesc{
					{Name: "albedo", Kind: render3d.ParamTexture},
					{Name: "albedo_sampler", Kind: render3d.ParamFloat},
				},
			}},
			{"nan default", render3d.CustomShaderDesc{
				Language: render3d.ShaderWGSL,
				Source:   customMatWGSLClip,
				Params:   []render3d.ShaderParamDesc{{Name: "x", Kind: render3d.ParamFloat, Default: nan}},
			}},
			{"inverted min max", render3d.CustomShaderDesc{
				Language: render3d.ShaderWGSL,
				Source:   customMatWGSLClip,
				Params:   []render3d.ShaderParamDesc{{Name: "x", Kind: render3d.ParamFloat, Min: customMatPtrF64(5), Max: customMatPtrF64(1)}},
			}},
			{"wrong default type", render3d.CustomShaderDesc{
				Language: render3d.ShaderWGSL,
				Source:   customMatWGSLClip,
				Params:   []render3d.ShaderParamDesc{{Name: "x", Kind: render3d.ParamFloat, Default: "bad"}},
			}},
			{"wgsl syntax error", render3d.CustomShaderDesc{
				Language: render3d.ShaderWGSL,
				Source:   "@vertex fn vs_main( -> ???",
			}},
			{"wgsl missing fragment entry", render3d.CustomShaderDesc{
				Language: render3d.ShaderWGSL,
				Source:   "@vertex fn vs_main(@location(0) p: vec3<f32>) -> @builtin(position) vec4<f32> { return vec4<f32>(p, 1.0); }",
			}},
			{"kage syntax error", render3d.CustomShaderDesc{
				Language: render3d.ShaderKage,
				Source:   "package main\nfunc Surface(uv vec2 vec4 { return vec4(1.0) }",
			}},
			{"kage missing entry", render3d.CustomShaderDesc{
				Language: render3d.ShaderKage,
				Source:   "package main\nfunc Helper(x float) float { return x }",
			}},
			{"kage undeclared var", render3d.CustomShaderDesc{
				Language: render3d.ShaderKage,
				Source:   "package main\nvar Mystery float\nfunc Surface(uv vec2) vec4 { return vec4(Mystery) }",
				Params:   []render3d.ShaderParamDesc{{Name: "Other", Kind: render3d.ParamFloat}},
			}},
			{"kage var kind mismatch", render3d.CustomShaderDesc{
				Language: render3d.ShaderKage,
				Source:   "package main\nvar Gain vec3\nfunc Surface(uv vec2) vec4 { return vec4(Gain, 1.0) }",
				Params:   []render3d.ShaderParamDesc{{Name: "Gain", Kind: render3d.ParamFloat}},
			}},
			{"kage out of range imageAt", render3d.CustomShaderDesc{
				Language: render3d.ShaderKage,
				Source:   "package main\nfunc Surface(uv vec2) vec4 { return image1At(uv) }",
				Params:   []render3d.ShaderParamDesc{{Name: "albedo", Kind: render3d.ParamTexture}},
			}},
		}

		for _, tc := range badDescs {
			t.Run(tc.name, func(t *testing.T) {
				_, err := render3d.NewCustomMaterial(nil, dev, tc.desc)
				if !errors.Is(err, render3d.ErrInvalidCustomMaterial) {
					t.Fatalf("NewCustomMaterial(%s) = %v, want ErrInvalidCustomMaterial", tc.name, err)
				}
				var cme *render3d.CustomMaterialError
				if !errors.As(err, &cme) {
					t.Fatalf("expected *CustomMaterialError, got %T", err)
				}
			})
		}
	})

	t.Run("setter validation and atomic ApplyMap", func(t *testing.T) {
		mat, err := render3d.NewCustomMaterial(nil, dev, render3d.CustomShaderDesc{
			Label:    "validate-setters",
			Language: render3d.ShaderWGSL,
			Source:   customMatWGSLClip,
			Params:   customMatWGSLParams(),
		})
		if err != nil {
			t.Fatalf("NewCustomMaterial: %v", err)
		}
		t.Cleanup(func() { _ = mat.Close() })

		if err := mat.SetFloat("missing", 1); !errors.Is(err, render3d.ErrInvalidCustomMaterial) {
			t.Errorf("unknown param err = %v", err)
		}
		if err := mat.SetVec3("intensity", math3d.Vec3{X: 1}); !errors.Is(err, render3d.ErrInvalidCustomMaterial) {
			t.Errorf("type mismatch err = %v", err)
		}
		if err := mat.SetFloat("intensity", float32(math.NaN())); !errors.Is(err, render3d.ErrInvalidCustomMaterial) {
			t.Errorf("NaN err = %v", err)
		}
		if err := mat.SetFloat("intensity", float32(math.Inf(1))); !errors.Is(err, render3d.ErrInvalidCustomMaterial) {
			t.Errorf("Inf err = %v", err)
		}
		if err := mat.SetFloat("intensity", 9.0); !errors.Is(err, render3d.ErrInvalidCustomMaterial) {
			t.Errorf("above max err = %v", err)
		}
		if err := mat.SetFloat("intensity", -1.0); !errors.Is(err, render3d.ErrInvalidCustomMaterial) {
			t.Errorf("below min err = %v", err)
		}

		// Atomic ApplyMap: if one key fails, no other key is mutated.
		before := mat.UniformBytes()
		err = mat.ApplyMap(map[string]any{
			"intensity": float32(2.0),
			"tint":      "not-a-color",
		})
		if !errors.Is(err, render3d.ErrInvalidCustomMaterial) {
			t.Fatalf("ApplyMap bad color = %v, want ErrInvalidCustomMaterial", err)
		}
		after := mat.UniformBytes()
		if string(before) != string(after) || mat.Dirty() {
			t.Fatal("failed ApplyMap mutated uniform bytes")
		}
	})
}

func TestCustomMaterialRetainedSurvivesDeviceLoss(t *testing.T) {
	const size = 64
	var (
		mesh  *render3d.GPUMesh
		mat   *render3d.CustomMaterial
		curDv *gpu.Device
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				curDv = dev
				if mesh == nil {
					var err error
					mesh, err = render3d.UploadMesh(cx, dev, customMatClipQuad(), render3d.MeshUpload{Label: "quad", Retained: true})
					if err != nil {
						t.Fatalf("UploadMesh: %v", err)
					}
				}
				if mat == nil {
					var err error
					mat, err = render3d.NewCustomMaterial(cx, dev, render3d.CustomShaderDesc{
						Label:       "retained",
						Language:    render3d.ShaderWGSL,
						Source:      customMatWGSLClip,
						DepthFormat: gpu.FormatDepth24Plus,
						Retained:    true,
						Params: []render3d.ShaderParamDesc{
							{Name: "intensity", Kind: render3d.ParamFloat, Default: float32(1.0)},
							{Name: "light_dir", Kind: render3d.ParamVec3, Default: math3d.Vec3{Z: 1}},
							{Name: "tint", Kind: render3d.ParamColor, Default: core.RGB(0x20a0ff)},
							{Name: "albedo", Kind: render3d.ParamTexture},
						},
					})
					if err != nil {
						t.Fatalf("NewCustomMaterial: %v", err)
					}
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				mat.Draw(pass, mesh)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	app.Advance(16 * time.Millisecond)
	img, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	wantPixel(t, img, size/2, size/2, color.RGBA{R: 32, G: 160, B: 255, A: 255})

	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
	if mat.Valid() {
		t.Fatal("material still valid while device is lost")
	}

	app.RestoreGPUDevice("id:viewport")
	if !mat.Valid() {
		t.Fatal("retained material not valid after device restore")
	}
	app.Advance(16 * time.Millisecond)
	img, err = app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	wantPixel(t, img, size/2, size/2, color.RGBA{R: 32, G: 160, B: 255, A: 255})

	// Updating uniforms on the restored device must also work without stale device pointers.
	app.Post(func() {
		if err := mat.SetColor("tint", core.RGB(0xff4080)); err != nil {
			t.Fatalf("SetColor after restore: %v", err)
		}
		if err := mat.Sync(curDv); err != nil {
			t.Fatalf("Sync after restore: %v", err)
		}
	})
	app.Settle()
	app.Advance(16 * time.Millisecond)
	img, err = app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after update: %v", err)
	}
	wantPixel(t, img, size/2, size/2, color.RGBA{R: 255, G: 64, B: 128, A: 255})
}

func TestCustomMaterialScopeDisposalAndClose(t *testing.T) {
	var (
		mat  *render3d.CustomMaterial
		show *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(dev *gpu.Device) {
					var err error
					mat, err = render3d.NewCustomMaterial(cx, dev, render3d.CustomShaderDesc{
						Label:    "scoped",
						Language: render3d.ShaderWGSL,
						Source:   customMatWGSLClip,
						Params:   customMatWGSLParams(),
					})
					if err != nil {
						t.Fatalf("NewCustomMaterial: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())

	if !mat.Valid() {
		t.Fatal("material not valid while mounted")
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if mat.Valid() {
		t.Error("material still valid after scope disposal")
	}
	if err := mat.Close(); err != nil {
		t.Errorf("Close after disposal: %v", err)
	}
	var nilMat *render3d.CustomMaterial
	if nilMat.Valid() || nilMat.Close() != nil || nilMat.UniformSize() != 0 {
		t.Error("nil CustomMaterial methods did not behave cleanly")
	}
}

func TestCustomMaterialSteadyStateZeroAllocs(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	mat, err := render3d.NewCustomMaterial(nil, dev, render3d.CustomShaderDesc{
		Label:    "zero-alloc",
		Language: render3d.ShaderKage,
		Source:   customMatKageSurface,
		Params:   customMatKageParams(),
	})
	if err != nil {
		t.Fatalf("NewCustomMaterial: %v", err)
	}
	t.Cleanup(func() { _ = mat.Close() })

	// Typed uniform setters mutate the pre-allocated buffer in place with 0 allocations.
	setterAllocs := testing.AllocsPerRun(50, func() {
		_ = mat.SetFloat("Intensity", 1.25)
		_ = mat.SetVec3("LightDir", math3d.Vec3{X: 0.3, Y: 0.8, Z: 0.5})
		_ = mat.SetColor("Tint", core.RGB(0x88ccff))
		_ = mat.SetInt("Bands", 3)
		_ = mat.SetBool("Highlight", true)
	})
	if setterAllocs != 0 {
		t.Errorf("typed uniform setters allocated %v times, want 0", setterAllocs)
	}

	// Sync once to clear dirty, then steady-state Sync with unchanged values allocates 0.
	if err := mat.Sync(dev); err != nil {
		t.Fatalf("initial Sync: %v", err)
	}
	syncAllocs := testing.AllocsPerRun(50, func() {
		_ = mat.SetFloat("Intensity", 1.25)
		_ = mat.Sync(dev)
	})
	if syncAllocs != 0 {
		t.Errorf("steady-state unchanged Sync allocated %v times, want 0", syncAllocs)
	}
}

func BenchmarkCustomMaterialUniformUpdate(b *testing.B) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	mat, err := render3d.NewCustomMaterial(nil, dev, render3d.CustomShaderDesc{
		Label:    "bench",
		Language: render3d.ShaderKage,
		Source:   customMatKageSurface,
		Params:   customMatKageParams(),
	})
	if err != nil {
		b.Fatalf("NewCustomMaterial: %v", err)
	}
	defer func() { _ = mat.Close() }()

	if err := mat.Sync(dev); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = mat.SetFloat("Intensity", 1.5)
		_ = mat.SetVec3("LightDir", math3d.Vec3{X: 0.2, Y: 0.9, Z: 0.4})
		_ = mat.SetColor("Tint", core.RGB(0xaabbcc))
		_ = mat.SetInt("Bands", 4)
		_ = mat.SetBool("Highlight", true)
		_ = mat.Sync(dev)
	}
}

func TestCustomMaterialMatchesMaterialAPI(t *testing.T) {
	if render3d.CustomMaterialGroup != render3d.MaterialGroup {
		t.Fatalf("CustomMaterialGroup = %d, want MaterialGroup %d", render3d.CustomMaterialGroup, render3d.MaterialGroup)
	}
	_, err := render3d.ComputeUniformLayout([]render3d.ShaderParamDesc{{Name: "bad name", Kind: render3d.ParamFloat}})
	if !errors.Is(err, render3d.ErrInvalidCustomMaterial) || !errors.Is(err, render3d.ErrInvalidMaterial) {
		t.Fatalf("error %v should match ErrInvalidCustomMaterial and ErrInvalidMaterial", err)
	}

	sr := gpu.NewSoftwareRenderer(4, 4, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	mat, err := render3d.NewCustomMaterial(nil, dev, render3d.CustomShaderDesc{
		Label:    "bytesize",
		Language: render3d.ShaderWGSL,
		Source:   customMatWGSLClip,
		Params: []render3d.ShaderParamDesc{
			{Name: "intensity", Kind: render3d.ParamFloat},
			{Name: "light_dir", Kind: render3d.ParamVec3},
			{Name: "tint", Kind: render3d.ParamColor},
			{Name: "albedo", Kind: render3d.ParamTexture},
		},
	})
	if err != nil {
		t.Fatalf("NewCustomMaterial: %v", err)
	}
	// 48-byte uniform block plus one 1x1 RGBA8 fallback texture.
	if got := mat.ByteSize(); got != 48+4 {
		t.Fatalf("ByteSize = %d, want 52", got)
	}

	// ApplyMap samples a new texture with the default sampler, as SetTexture with a nil sampler does.
	tex, err := render3d.UploadTexturePixels(nil, dev, render3d.PixelBuffer{Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm, Pixels: []byte{1, 2, 3, 255}}, render3d.TextureUpload{Label: "t"})
	if err != nil {
		t.Fatalf("UploadTexturePixels: %v", err)
	}
	t.Cleanup(func() { _ = tex.Close() })
	nearest, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	t.Cleanup(func() { _ = nearest.Close() })
	if err := mat.SetTexture("albedo", tex, nearest); err != nil {
		t.Fatalf("SetTexture: %v", err)
	}
	if err := mat.Sync(dev); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	withNearest := mat.BindGroup()
	if err := mat.ApplyMap(map[string]any{"albedo": tex}); err != nil {
		t.Fatalf("ApplyMap: %v", err)
	}
	if !mat.Dirty() {
		t.Fatal("ApplyMap with the same texture but a custom sampler should switch to the default sampler")
	}
	if err := mat.Sync(dev); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if mat.BindGroup() == withNearest {
		t.Fatal("ApplyMap kept the custom sampler's bind group")
	}
	if err := mat.SetTexture("albedo", tex, nil); err != nil {
		t.Fatalf("SetTexture: %v", err)
	}
	if mat.Dirty() {
		t.Fatal("SetTexture with a nil sampler after ApplyMap should be a no-op")
	}
	if err := mat.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := mat.ByteSize(); got != 0 {
		t.Fatalf("ByteSize after Close = %d, want 0", got)
	}
}

func TestCustomMaterialRejectsScalarAfterVec3(t *testing.T) {
	for _, kind := range []render3d.ShaderParamKind{render3d.ParamFloat, render3d.ParamInt, render3d.ParamBool} {
		_, err := render3d.ComputeUniformLayout([]render3d.ShaderParamDesc{
			{Name: "dir", Kind: render3d.ParamVec3},
			{Name: "s", Kind: kind},
		})
		var cerr *render3d.CustomMaterialError
		if !errors.As(err, &cerr) || cerr.Param != "s" || !strings.Contains(cerr.Message, "vec3") {
			t.Fatalf("[vec3, %s] error = %v, want a CustomMaterialError on param s", kind, err)
		}
	}

	// A texture between the vec3 and the scalar does not separate them in the uniform block.
	if _, err := render3d.ComputeUniformLayout([]render3d.ShaderParamDesc{
		{Name: "dir", Kind: render3d.ParamVec3},
		{Name: "tex", Kind: render3d.ParamTexture},
		{Name: "s", Kind: render3d.ParamFloat},
	}); !errors.Is(err, render3d.ErrInvalidCustomMaterial) {
		t.Fatalf("[vec3, texture, float] error = %v, want ErrInvalidCustomMaterial", err)
	}

	// Kage var declarations go through the same layout.
	err := render3d.CompileCheck(`package main

var Dir vec3
var Gain float

func Surface() vec4 {
	return vec4(Dir*Gain, 1.0)
}
`, render3d.ShaderKage, nil)
	if !errors.Is(err, render3d.ErrInvalidCustomMaterial) || !strings.Contains(err.Error(), "Gain") {
		t.Fatalf("Kage [vec3, float] error = %v, want an error on Gain", err)
	}

	// Scalars before a vec3, and 16-byte or 8-byte members after it, keep the WGSL and Metal layouts identical.
	layout, err := render3d.ComputeUniformLayout([]render3d.ShaderParamDesc{
		{Name: "a", Kind: render3d.ParamFloat},
		{Name: "dir", Kind: render3d.ParamVec3},
		{Name: "uv", Kind: render3d.ParamVec2},
		{Name: "b", Kind: render3d.ParamFloat},
		{Name: "dir2", Kind: render3d.ParamVec3},
		{Name: "tint", Kind: render3d.ParamVec4},
	})
	if err != nil {
		t.Fatalf("ComputeUniformLayout: %v", err)
	}
	want := map[string]int{"a": 0, "dir": 16, "uv": 32, "b": 40, "dir2": 48, "tint": 64}
	for name, off := range want {
		if layout.Offsets[name] != off {
			t.Fatalf("offset of %s = %d, want %d", name, layout.Offsets[name], off)
		}
	}
	if layout.UniformSize != 80 {
		t.Fatalf("UniformSize = %d, want 80", layout.UniformSize)
	}
}

func TestCustomMaterialTextureSwapsReuseBindGroups(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(4, 4, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	mat, err := render3d.NewCustomMaterial(nil, dev, render3d.CustomShaderDesc{
		Label:    "flipbook",
		Language: render3d.ShaderWGSL,
		Source:   customMatWGSLClip,
		Params:   customMatWGSLParams(),
	})
	if err != nil {
		t.Fatalf("NewCustomMaterial: %v", err)
	}
	t.Cleanup(func() { _ = mat.Close() })
	frames := make([]*render3d.GPUTexture, 3)
	for i := range frames {
		frames[i], err = render3d.UploadTexturePixels(nil, dev, render3d.PixelBuffer{Width: 1, Height: 1, Format: gpu.FormatRGBA8Unorm, Pixels: []byte{byte(i * 80), 0, 0, 255}}, render3d.TextureUpload{Label: "frame"})
		if err != nil {
			t.Fatalf("UploadTexturePixels: %v", err)
		}
		t.Cleanup(func() { _ = frames[i].Close() })
	}

	groups := make(map[*gpu.BindGroup]bool)
	swap := func(i int) {
		t.Helper()
		if err := mat.SetTexture("albedo", frames[i%len(frames)], nil); err != nil {
			t.Fatalf("SetTexture: %v", err)
		}
		if err := mat.Sync(dev); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		groups[mat.BindGroup()] = true
	}
	for i := range len(frames) {
		swap(i)
	}
	first := mat.BindGroup()
	for i := range 300 {
		swap(i)
	}
	if len(groups) != len(frames) {
		t.Fatalf("cycling %d textures 300 times made %d bind groups, want %d", len(frames), len(groups), len(frames))
	}
	if !first.Valid() {
		t.Fatal("a cached bind group was closed while still in use")
	}
	allocs := testing.AllocsPerRun(100, func() {
		_ = mat.SetTexture("albedo", frames[0], nil)
		_ = mat.Sync(dev)
		_ = mat.SetTexture("albedo", frames[1], nil)
		_ = mat.Sync(dev)
	})
	if allocs != 0 {
		t.Fatalf("swapping between cached textures allocated %.1f times per run, want 0", allocs)
	}

	// A closed texture's bind group is dropped, and binding it again is refused.
	_ = frames[2].Close()
	if err := mat.SetTexture("albedo", frames[2], nil); !errors.Is(err, render3d.ErrInvalidTexture) {
		t.Fatalf("SetTexture(closed) error = %v, want ErrInvalidTexture", err)
	}
	if err := mat.SetTexture("albedo", nil, nil); err != nil {
		t.Fatalf("SetTexture(nil): %v", err)
	}
	if err := mat.Sync(dev); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !mat.Valid() {
		t.Fatal("material invalid after dropping a closed texture's bind group")
	}
}
