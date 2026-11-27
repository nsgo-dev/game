package render3d_test

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// viewportSolidWGSL renders a MeshVertexLayout mesh transformed by CameraWGSL at @group(0) with a flat per-object color uniform at @group(1) @binding(0).
const viewportSolidWGSL = render3d.CameraWGSL + `
struct SolidColor {
	color: vec4<f32>,
}

@group(1) @binding(0) var<uniform> solid: SolidColor;

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
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return solid.color;
}
`

// viewportTexturedWGSL renders a MeshVertexLayout mesh transformed by CameraWGSL at @group(0), sampling a material texture + sampler at @group(1) (compatible with render3d.TextureBindingLayout and render3d.BindTexture).
const viewportTexturedWGSL = render3d.CameraWGSL + `
@group(1) @binding(0) var mat_tex: texture_2d<f32>;
@group(1) @binding(1) var mat_samp: sampler;

struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) uv: vec2<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return textureSample(mat_tex, mat_samp, in.uv);
}
`

func viewportBoxMesh(center math3d.Vec3, half math3d.Vec3) render3d.Mesh {
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
		c := math3d.Vec3{
			X: center.X + f.n.X*half.X,
			Y: center.Y + f.n.Y*half.Y,
			Z: center.Z + f.n.Z*half.Z,
		}
		u := math3d.Vec3{X: f.u.X * half.X, Y: f.u.Y * half.Y, Z: f.u.Z * half.Z}
		v := math3d.Vec3{X: f.v.X * half.X, Y: f.v.Y * half.Y, Z: f.v.Z * half.Z}
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

func viewportFacingQuadMesh(center math3d.Vec3, halfW, halfH float32) render3d.Mesh {
	n := math3d.Vec3{Z: 1}
	return render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: center.X - halfW, Y: center.Y - halfH, Z: center.Z},
			{X: center.X + halfW, Y: center.Y - halfH, Z: center.Z},
			{X: center.X + halfW, Y: center.Y + halfH, Z: center.Z},
			{X: center.X - halfW, Y: center.Y + halfH, Z: center.Z},
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

func viewportColorBytes(r, g, b, a float32) []byte {
	out := make([]byte, 16)
	binary.LittleEndian.PutUint32(out[0:], math.Float32bits(r))
	binary.LittleEndian.PutUint32(out[4:], math.Float32bits(g))
	binary.LittleEndian.PutUint32(out[8:], math.Float32bits(b))
	binary.LittleEndian.PutUint32(out[12:], math.Float32bits(a))
	return out
}

func viewportColorBindGroup(t *testing.T, cx *core.Scope, dev *gpu.Device, layout *gpu.BindGroupLayout, r, g, b, a float32, retained bool) *gpu.BindGroup {
	t.Helper()
	buf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
		Label:    "viewport.solid.color",
		Usage:    gpu.UsageUniform | gpu.UsageCopyDst,
		Data:     viewportColorBytes(r, g, b, a),
		Retained: retained,
	})
	if err != nil {
		t.Fatalf("CreateBuffer color: %v", err)
	}
	bg, err := dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
		Label:   "viewport.solid.color",
		Layout:  layout,
		Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: buf, Size: 16}},
	})
	if err != nil {
		t.Fatalf("CreateBindGroupDesc color: %v", err)
	}
	return bg
}

func viewportColorLayout(t *testing.T, cx *core.Scope, dev *gpu.Device) *gpu.BindGroupLayout {
	t.Helper()
	layout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
		Label: "viewport.solid.color",
		Entries: []gpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: gpu.StageFragment,
			Type:       gpu.BindingUniform,
		}},
	})
	if err != nil {
		t.Fatalf("CreateBindGroupLayoutDesc color: %v", err)
	}
	return layout
}

func viewportSolidPipeline(t *testing.T, cx *core.Scope, dev *gpu.Device, camLayout, colorLayout *gpu.BindGroupLayout, colorFmt, depthFmt gpu.TextureFormat, samples int) *gpu.Pipeline {
	t.Helper()
	mod, err := gpu.CompileWGSL(viewportSolidWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL viewportSolidWGSL: %v", err)
	}
	desc := gpu.PipelineDesc{
		Label:         "viewport.solid",
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{camLayout, colorLayout},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
		Targets:       []gpu.ColorTargetState{{Format: colorFmt}},
		SampleCount:   samples,
	}
	if depthFmt != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{Format: depthFmt, DepthWrite: true, DepthCompare: gpu.CompareLess}
	}
	pipe, err := dev.CreatePipeline(cx, desc)
	if err != nil {
		t.Fatalf("CreatePipeline solid: %v", err)
	}
	return pipe
}

func viewportTexturedPipeline(t *testing.T, cx *core.Scope, dev *gpu.Device, camLayout, texLayout *gpu.BindGroupLayout, colorFmt, depthFmt gpu.TextureFormat) *gpu.Pipeline {
	t.Helper()
	mod, err := gpu.CompileWGSL(viewportTexturedWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL viewportTexturedWGSL: %v", err)
	}
	desc := gpu.PipelineDesc{
		Label:         "viewport.textured",
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{camLayout, texLayout},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
		Targets:       []gpu.ColorTargetState{{Format: colorFmt}},
	}
	if depthFmt != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{Format: depthFmt, DepthWrite: true, DepthCompare: gpu.CompareLess}
	}
	pipe, err := dev.CreatePipeline(cx, desc)
	if err != nil {
		t.Fatalf("CreatePipeline textured: %v", err)
	}
	return pipe
}

func viewportAssertPixel(t *testing.T, img *image.RGBA, x, y int, want color.RGBA) {
	t.Helper()
	got := img.RGBAAt(x, y)
	near := func(a, b uint8) bool { return max(a, b)-min(a, b) <= 3 }
	if !near(got.R, want.R) || !near(got.G, want.G) || !near(got.B, want.B) || !near(got.A, want.A) {
		t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got, want)
	}
}

func viewportProfiles() []struct {
	name string
	opts []nsgotest.Option
} {
	return []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
}

// TestRenderTextureSampledAsMaterial proves acceptance criterion 1: an offscreen camera renders 3D geometry into a RenderTexture and the main camera samples rt.GPUTexture() as a material texture via render3d.BindTexture on a 3D quad (mirror / minimap / portal pattern), on both full and WebGL 2 / GLES 3.0 (NoGPUCompute) profiles.
func TestRenderTextureSampledAsMaterial(t *testing.T) {
	const (
		layerWorld  uint32 = 1 << 0
		layerMirror uint32 = 1 << 1
		surfW              = 128
		surfH              = 128
		rtSize             = 64
	)

	offClear := core.RGB(0x203050)
	mainClear := core.RGB(0x0c1018)

	for _, p := range viewportProfiles() {
		t.Run(p.name, func(t *testing.T) {
			var (
				renderer   *render3d.ViewportRenderer
				rt         *render3d.RenderTexture
				solidPipe  *gpu.Pipeline
				texPipe    *gpu.Pipeline
				redBox     *render3d.GPUMesh
				greenBox   *render3d.GPUMesh
				mirrorQuad *render3d.GPUMesh
				redBG      *gpu.BindGroup
				greenBG    *gpu.BindGroup
				mirrorBG   *gpu.BindGroup
				frames     int
			)

			offCam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 1, 0.1, 20)
			mainCam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 1, 0.1, 20)

			opts := append([]nsgotest.Option{nsgotest.Size(surfW, surfH), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "rtt-view",
					Style:       core.Style{Width: core.Pt(surfW), Height: core.Pt(surfH)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						var err error
						renderer, err = render3d.NewViewportRenderer(cx, dev)
						if err != nil {
							t.Fatalf("NewViewportRenderer: %v", err)
						}
						rt, err = render3d.NewRenderTexture(cx, dev, render3d.RenderTextureDesc{
							Label:       "mirror-rt",
							Width:       rtSize,
							Height:      rtSize,
							ColorFormat: gpu.FormatRGBA8Unorm,
							DepthFormat: gpu.FormatDepth24Plus,
						})
						if err != nil {
							t.Fatalf("NewRenderTexture: %v", err)
						}
						colorLayout := viewportColorLayout(t, cx, dev)
						texLayout, err := render3d.TextureBindingLayout(cx, dev)
						if err != nil {
							t.Fatalf("TextureBindingLayout: %v", err)
						}
						solidPipe = viewportSolidPipeline(t, cx, dev, renderer.CameraLayout(), colorLayout, rt.ColorFormat(), rt.DepthFormat(), 1)
						texPipe = viewportTexturedPipeline(t, cx, dev, renderer.CameraLayout(), texLayout, dev.SurfaceFormat(), gpu.FormatDepth24Plus)

						redBox, err = render3d.UploadMesh(cx, dev, viewportBoxMesh(math3d.Vec3{X: -1.1, Y: 0, Z: 0}, math3d.Vec3{X: 0.7, Y: 1.2, Z: 0.5}), render3d.MeshUpload{Label: "red-box"})
						if err != nil {
							t.Fatalf("UploadMesh redBox: %v", err)
						}
						greenBox, err = render3d.UploadMesh(cx, dev, viewportBoxMesh(math3d.Vec3{X: 1.1, Y: 0, Z: 0}, math3d.Vec3{X: 0.7, Y: 1.2, Z: 0.5}), render3d.MeshUpload{Label: "green-box"})
						if err != nil {
							t.Fatalf("UploadMesh greenBox: %v", err)
						}
						mirrorQuad, err = render3d.UploadMesh(cx, dev, viewportFacingQuadMesh(math3d.Vec3{Z: 0}, 1.4, 1.4), render3d.MeshUpload{Label: "mirror-quad"})
						if err != nil {
							t.Fatalf("UploadMesh mirrorQuad: %v", err)
						}

						redBG = viewportColorBindGroup(t, cx, dev, colorLayout, 1, 0, 0, 1, false)
						greenBG = viewportColorBindGroup(t, cx, dev, colorLayout, 0, 1, 0, 1, false)

						samp, err := render3d.NewGPUSampler(cx, dev, render3d.SamplerNearestClamp())
						if err != nil {
							t.Fatalf("NewGPUSampler: %v", err)
						}
						// Directly bind rt.GPUTexture() as a material texture!
						mirrorBG, err = render3d.BindTexture(cx, dev, texLayout, rt.GPUTexture(), samp)
						if err != nil {
							t.Fatalf("BindTexture(rt.GPUTexture()): %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						frames++
						// Deliberately list main view BEFORE offscreen view to prove ViewportRenderer always renders offscreen RenderTextures first.
						views := []render3d.CameraView{
							{
								Name:       "main",
								Camera:     mainCam,
								LayerMask:  layerMirror,
								ClearColor: mainClear,
							},
							{
								Name:       "offscreen-mirror",
								Camera:     offCam,
								Target:     rt,
								LayerMask:  layerWorld,
								ClearColor: offClear,
							},
						}
						err := renderer.RenderFrame(frame, render3d.SurfaceDesc{HasDepth: true}, views, func(ctx render3d.ViewportContext) {
							if render3d.LayerMatches(layerWorld, ctx.LayerMask) {
								ctx.Pass.SetPipeline(solidPipe)
								ctx.Pass.SetBindGroup(render3d.CameraGroup, ctx.CameraBindGroup)
								ctx.Pass.SetBindGroup(1, redBG)
								redBox.Draw(ctx.Pass)
								ctx.Pass.SetBindGroup(1, greenBG)
								greenBox.Draw(ctx.Pass)
							}
							if render3d.LayerMatches(layerMirror, ctx.LayerMask) {
								ctx.Pass.SetPipeline(texPipe)
								ctx.Pass.SetBindGroup(render3d.CameraGroup, ctx.CameraBindGroup)
								ctx.Pass.SetBindGroup(1, mirrorBG)
								mirrorQuad.Draw(ctx.Pass)
							}
						})
						if err != nil {
							t.Fatalf("RenderFrame: %v", err)
						}
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)
			if frames == 0 {
				t.Fatal("OnFrame did not execute")
			}
			img, err := app.GPUReadPixels("id:rtt-view")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}

			// Corner outside the mirror quad shows main clear color.
			viewportAssertPixel(t, img, 4, 4, color.RGBA{R: 0x0c, G: 0x10, B: 0x18, A: 255})
			// Center of the mirror quad shows the offscreen camera's clear color (between the two boxes).
			viewportAssertPixel(t, img, surfW/2, surfH/2, color.RGBA{R: 0x20, G: 0x30, B: 0x50, A: 255})
			// Left side of the mirror quad shows the offscreen red box.
			viewportAssertPixel(t, img, surfW/2-26, surfH/2, color.RGBA{R: 255, G: 0, B: 0, A: 255})
			// Right side of the mirror quad shows the offscreen green box.
			viewportAssertPixel(t, img, surfW/2+26, surfH/2, color.RGBA{R: 0, G: 255, B: 0, A: 255})
		})
	}
}

// TestRenderTextureMSAA4xSampledAsMaterial verifies that a 4x MSAA RenderTexture automatically resolves into its single-sampled Resolve texture and is directly sampleable via rt.GPUTexture() and BindTexture.
func TestRenderTextureMSAA4xSampledAsMaterial(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(64, 64, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	renderer, err := render3d.NewViewportRenderer(nil, dev)
	if err != nil {
		t.Fatalf("NewViewportRenderer: %v", err)
	}
	t.Cleanup(func() { _ = renderer.Close() })

	rt, err := render3d.NewRenderTexture(nil, dev, render3d.RenderTextureDesc{
		Label:       "msaa-rt",
		Width:       32,
		Height:      32,
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
		SampleCount: 4,
	})
	if err != nil {
		t.Fatalf("NewRenderTexture 4x: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	if rt.SampleCount() != 4 || rt.Target().Resolve == nil || rt.Texture() != rt.Target().Resolve {
		t.Fatalf("4x MSAA RenderTexture must expose Resolve texture for sampling")
	}
	// 32*32*4*(4 samples + 1 resolve) + 32*32*4*4(depth)
	if rt.ByteSize() == 0 {
		t.Fatal("ByteSize must be non-zero for 4x MSAA RenderTexture")
	}

	colorLayout := viewportColorLayout(t, nil, dev)
	texLayout, err := render3d.TextureBindingLayout(nil, dev)
	if err != nil {
		t.Fatalf("TextureBindingLayout: %v", err)
	}
	solidPipe := viewportSolidPipeline(t, nil, dev, renderer.CameraLayout(), colorLayout, rt.ColorFormat(), rt.DepthFormat(), 4)
	texPipe := viewportTexturedPipeline(t, nil, dev, renderer.CameraLayout(), texLayout, gpu.FormatRGBA8Unorm, gpu.FormatDepth24Plus)

	quad, err := render3d.UploadMesh(nil, dev, viewportFacingQuadMesh(math3d.Vec3{}, 1.5, 1.5), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	yellowBG := viewportColorBindGroup(t, nil, dev, colorLayout, 1, 1, 0, 1, false)
	samp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	rtBG, err := render3d.BindTexture(nil, dev, texLayout, rt.GPUTexture(), samp)
	if err != nil {
		t.Fatalf("BindTexture MSAA: %v", err)
	}

	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 1, 0.1, 20)
	enc := dev.BeginEncoder()
	views := []render3d.CameraView{
		{Name: "off", Camera: cam, Target: rt, LayerMask: 1, ClearColor: core.RGB(0x101010)},
		{Name: "main", Camera: cam, LayerMask: 2, ClearColor: core.RGB(0x000000)},
	}
	err = renderer.RenderEncoder(dev, enc, render3d.SurfaceDesc{Width: 64, Height: 64, HasDepth: true}, views, func(ctx render3d.ViewportContext) {
		if ctx.LayerMask == 1 {
			ctx.Pass.SetPipeline(solidPipe)
			ctx.Pass.SetBindGroup(1, yellowBG)
			quad.Draw(ctx.Pass)
		} else {
			ctx.Pass.SetPipeline(texPipe)
			ctx.Pass.SetBindGroup(1, rtBG)
			quad.Draw(ctx.Pass)
		}
	})
	if err != nil {
		t.Fatalf("RenderEncoder: %v", err)
	}
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	viewportAssertPixel(t, sr.Image(), 32, 32, color.RGBA{R: 255, G: 255, B: 0, A: 255})
}

// TestPerCameraLayerMasks proves acceptance criterion 2: three cameras looking at the same 3D scene with different LayerMasks render only Layer 1 (red), only Layer 2 (blue), and both Layer 1|2 respectively, verified via pixel readback on both full and NoGPUCompute profiles.
func TestPerCameraLayerMasks(t *testing.T) {
	const (
		layerRed  uint32 = 1 << 0
		layerBlue uint32 = 1 << 1
		surfW            = 192 // 3 equal 64x64 viewports side by side!
		surfH            = 64
	)

	clearCol := core.RGB(0x10141c)
	wantClear := color.RGBA{R: 0x10, G: 0x14, B: 0x1c, A: 255}
	wantRed := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	wantBlue := color.RGBA{R: 0, G: 0, B: 255, A: 255}

	type sceneObj struct {
		mesh *render3d.GPUMesh
		bg   *gpu.BindGroup
	}

	for _, p := range viewportProfiles() {
		t.Run(p.name, func(t *testing.T) {
			var (
				renderer  *render3d.ViewportRenderer
				solidPipe *gpu.Pipeline
				objects   map[uint64]sceneObj
				layered   []render3d.LayeredDrawItem
				scratch   []render3d.DrawItem
				queue     render3d.Queue
				frames    int
			)

			cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 1, 0.1, 20)
			thirds := render3d.SplitScreenSideBySide(3)

			opts := append([]nsgotest.Option{nsgotest.Size(surfW, surfH), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "layer-view",
					Style:       core.Style{Width: core.Pt(surfW), Height: core.Pt(surfH)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						var err error
						renderer, err = render3d.NewViewportRenderer(cx, dev)
						if err != nil {
							t.Fatalf("NewViewportRenderer: %v", err)
						}
						colorLayout := viewportColorLayout(t, cx, dev)
						solidPipe = viewportSolidPipeline(t, cx, dev, renderer.CameraLayout(), colorLayout, dev.SurfaceFormat(), gpu.FormatDepth24Plus, 1)

						redM := viewportBoxMesh(math3d.Vec3{X: -1.0, Y: 0, Z: 0}, math3d.Vec3{X: 0.65, Y: 1.2, Z: 0.5})
						blueM := viewportBoxMesh(math3d.Vec3{X: 1.0, Y: 0, Z: 0}, math3d.Vec3{X: 0.65, Y: 1.2, Z: 0.5})
						redGPU, err := render3d.UploadMesh(cx, dev, redM, render3d.MeshUpload{Label: "red"})
						if err != nil {
							t.Fatalf("UploadMesh red: %v", err)
						}
						blueGPU, err := render3d.UploadMesh(cx, dev, blueM, render3d.MeshUpload{Label: "blue"})
						if err != nil {
							t.Fatalf("UploadMesh blue: %v", err)
						}

						// Build scene3d.Nodes with distinct Layer bits.
						redNode := scene3d.NewNode("red-box")
						redNode.Layer = layerRed
						redNode.SetLocalBounds(redGPU.Bounds())

						blueNode := scene3d.NewNode("blue-box")
						blueNode.Layer = layerBlue
						blueNode.SetLocalBounds(blueGPU.Bounds())

						objects = map[uint64]sceneObj{
							1: {mesh: redGPU, bg: viewportColorBindGroup(t, cx, dev, colorLayout, 1, 0, 0, 1, false)},
							2: {mesh: blueGPU, bg: viewportColorBindGroup(t, cx, dev, colorLayout, 0, 0, 1, 1, false)},
						}
						layered = []render3d.LayeredDrawItem{
							{
								Layer: redNode.Layer,
								Item:  render3d.DrawItem{ID: 1, MeshID: 1, MaterialID: 1, PipelineID: 1, WorldBounds: redNode.WorldBounds()},
							},
							{
								Layer: blueNode.Layer,
								Item:  render3d.DrawItem{ID: 2, MeshID: 2, MaterialID: 2, PipelineID: 1, WorldBounds: blueNode.WorldBounds()},
							},
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						frames++
						views := []render3d.CameraView{
							{Name: "only-red", Camera: cam, Viewport: thirds[0], LayerMask: layerRed, ClearColor: clearCol},
							{Name: "only-blue", Camera: cam, Viewport: thirds[1], LayerMask: layerBlue},
							{Name: "both", Camera: cam, Viewport: thirds[2], LayerMask: layerRed | layerBlue},
						}
						err := renderer.RenderFrame(frame, render3d.SurfaceDesc{HasDepth: true}, views, func(ctx render3d.ViewportContext) {
							scratch = render3d.FilterLayeredItems(layered, ctx.LayerMask, scratch[:0])
							plan := queue.Build(ctx.Uniform.ViewProj, ctx.Camera.Pos, scratch)
							ctx.Pass.SetPipeline(solidPipe)
							ctx.Pass.SetBindGroup(render3d.CameraGroup, ctx.CameraBindGroup)
							for _, d := range plan.Opaque {
								obj := objects[d.ID]
								ctx.Pass.SetBindGroup(1, obj.bg)
								obj.mesh.Draw(ctx.Pass)
							}
						})
						if err != nil {
							t.Fatalf("RenderFrame: %v", err)
						}
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)
			if frames == 0 {
				t.Fatal("OnFrame did not execute")
			}
			img, err := app.GPUReadPixels("id:layer-view")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}

			// Viewport 0 (x in [0, 64)): Layer 1 ONLY -> red at x=16, clear at x=48.
			viewportAssertPixel(t, img, 16, 32, wantRed)
			viewportAssertPixel(t, img, 48, 32, wantClear)

			// Viewport 1 (x in [64, 128)): Layer 2 ONLY -> clear at x=80, blue at x=112.
			viewportAssertPixel(t, img, 80, 32, wantClear)
			viewportAssertPixel(t, img, 112, 32, wantBlue)

			// Viewport 2 (x in [128, 192)): Layer 1 | Layer 2 -> red at x=144, blue at x=176.
			viewportAssertPixel(t, img, 144, 32, wantRed)
			viewportAssertPixel(t, img, 176, 32, wantBlue)
		})
	}
}

// TestSplitScreenSideBySideViewports proves acceptance criterion 3: two 3D viewports render side by side in a single frame with independent cameras, automatic per-viewport aspect adjustment (without mutating caller cameras), and strict scissor clipping with zero bleed across the split seam, on both full and WebGL 2 / GLES 3.0 (NoGPUCompute) profiles.
func TestSplitScreenSideBySideViewports(t *testing.T) {
	const (
		surfW = 128
		surfH = 64
	)

	for _, p := range viewportProfiles() {
		t.Run(p.name, func(t *testing.T) {
			var (
				renderer      *render3d.ViewportRenderer
				solidPipe     *gpu.Pipeline
				redQuad       *render3d.GPUMesh
				blueQuad      *render3d.GPUMesh
				redBG, blueBG *gpu.BindGroup
				seenAspects   [2]float32
				frames        int
			)

			// Give caller cameras a distinct initial aspect (2.5) to verify ViewportRenderer adjusts each per-view copy without mutating them.
			camLeft := scene3d.NewPerspectiveCamera(math3d.Vec3{X: -10, Z: 4}, math3d.DegToRad(60), 2.5, 0.1, 50)
			camLeft.LookAt(math3d.Vec3{X: -10, Z: 0}, math3d.Vec3Up())
			camRight := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 10, Z: 4}, math3d.DegToRad(60), 2.5, 0.1, 50)
			camRight.LookAt(math3d.Vec3{X: 10, Z: 0}, math3d.Vec3Up())

			halves := render3d.SplitScreenHorizontal(2)

			opts := append([]nsgotest.Option{nsgotest.Size(surfW, surfH), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "split-view",
					Style:       core.Style{Width: core.Pt(surfW), Height: core.Pt(surfH)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						var err error
						renderer, err = render3d.NewViewportRenderer(cx, dev)
						if err != nil {
							t.Fatalf("NewViewportRenderer: %v", err)
						}
						colorLayout := viewportColorLayout(t, cx, dev)
						solidPipe = viewportSolidPipeline(t, cx, dev, renderer.CameraLayout(), colorLayout, dev.SurfaceFormat(), gpu.FormatDepth24Plus, 1)

						// Oversized quads centered in front of each camera so they fill past the viewport boundary — proving scissor prevents any bleed!
						redQuad, err = render3d.UploadMesh(cx, dev, viewportFacingQuadMesh(math3d.Vec3{X: -10, Z: 0}, 8, 8), render3d.MeshUpload{Label: "left-quad"})
						if err != nil {
							t.Fatalf("UploadMesh left: %v", err)
						}
						blueQuad, err = render3d.UploadMesh(cx, dev, viewportFacingQuadMesh(math3d.Vec3{X: 10, Z: 0}, 8, 8), render3d.MeshUpload{Label: "right-quad"})
						if err != nil {
							t.Fatalf("UploadMesh right: %v", err)
						}
						redBG = viewportColorBindGroup(t, cx, dev, colorLayout, 1, 0, 0, 1, false)
						blueBG = viewportColorBindGroup(t, cx, dev, colorLayout, 0, 0, 1, 1, false)
					},
					OnFrame: func(frame *gpu.Frame) {
						frames++
						views := []render3d.CameraView{
							{Name: "player1", Camera: camLeft, Viewport: halves[0], ClearColor: core.RGB(0x10141c)},
							{Name: "player2", Camera: camRight, Viewport: halves[1]},
						}
						err := renderer.RenderFrame(frame, render3d.SurfaceDesc{HasDepth: true}, views, func(ctx render3d.ViewportContext) {
							seenAspects[ctx.ViewIndex] = ctx.Camera.Projection.Aspect
							ctx.Pass.SetPipeline(solidPipe)
							ctx.Pass.SetBindGroup(render3d.CameraGroup, ctx.CameraBindGroup)
							ctx.Pass.SetBindGroup(1, redBG)
							redQuad.Draw(ctx.Pass)
							ctx.Pass.SetBindGroup(1, blueBG)
							blueQuad.Draw(ctx.Pass)
						})
						if err != nil {
							t.Fatalf("RenderFrame: %v", err)
						}
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)
			if frames == 0 {
				t.Fatal("OnFrame did not execute")
			}
			// Original cameras must not have been mutated.
			if camLeft.Projection.Aspect != 2.5 || camRight.Projection.Aspect != 2.5 {
				t.Fatalf("caller cameras mutated: left=%g right=%g, want 2.5", camLeft.Projection.Aspect, camRight.Projection.Aspect)
			}
			// Each 64x64 half has pixel aspect 1.0.
			if seenAspects[0] != 1.0 || seenAspects[1] != 1.0 {
				t.Fatalf("resolved aspects = %v, want [1 1]", seenAspects)
			}

			img, err := app.GPUReadPixels("id:split-view")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}

			// Verify entire left half [0..63] is red and entire right half [64..127] is blue, including right on the split seam (x=63 vs x=64).
			for _, y := range []int{2, 32, 61} {
				viewportAssertPixel(t, img, 1, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
				viewportAssertPixel(t, img, 32, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
				viewportAssertPixel(t, img, 63, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
				viewportAssertPixel(t, img, 64, y, color.RGBA{R: 0, G: 0, B: 255, A: 255})
				viewportAssertPixel(t, img, 96, y, color.RGBA{R: 0, G: 0, B: 255, A: 255})
				viewportAssertPixel(t, img, 126, y, color.RGBA{R: 0, G: 0, B: 255, A: 255})
			}
		})
	}
}

// TestPictureInPictureResetDepth verifies that an overlapping secondary viewport (such as a corner 3D minimap) with ResetDepth: true preserves the main view's color outside its scissor rect while clearing depth so its own farther 3D geometry is not occluded by the main view's closer depth values.
func TestPictureInPictureResetDepth(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(64, 64, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	renderer, err := render3d.NewViewportRenderer(nil, dev)
	if err != nil {
		t.Fatalf("NewViewportRenderer: %v", err)
	}
	t.Cleanup(func() { _ = renderer.Close() })

	colorLayout := viewportColorLayout(t, nil, dev)
	pipe := viewportSolidPipeline(t, nil, dev, renderer.CameraLayout(), colorLayout, gpu.FormatRGBA8Unorm, gpu.FormatDepth24Plus, 1)

	// Main view draws a VERY NEAR red quad (z = 4.0 -> small depth near 0). PiP overlay draws a FARTHER blue quad (z = -2.0 -> larger depth) that would be depth-rejected unless ResetDepth: true clears the depth buffer!
	nearQuad, err := render3d.UploadMesh(nil, dev, viewportFacingQuadMesh(math3d.Vec3{Z: 4.0}, 3, 3), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh nearQuad: %v", err)
	}
	farQuad, err := render3d.UploadMesh(nil, dev, viewportFacingQuadMesh(math3d.Vec3{Z: -2.0}, 3, 3), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh farQuad: %v", err)
	}
	redBG := viewportColorBindGroup(t, nil, dev, colorLayout, 1, 0, 0, 1, false)
	blueBG := viewportColorBindGroup(t, nil, dev, colorLayout, 0, 0, 1, 1, false)

	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 1, 0.1, 20)
	pipRect := render3d.ViewportRect{X: 0.5, Y: 0, Width: 0.5, Height: 0.5}

	enc := dev.BeginEncoder()
	views := []render3d.CameraView{
		{Name: "main", Camera: cam, Viewport: render3d.FullViewport(), LayerMask: 1, Priority: 0, ClearColor: core.RGB(0x000000)},
		{Name: "pip", Camera: cam, Viewport: pipRect, LayerMask: 2, Priority: 10, ResetDepth: true},
	}
	err = renderer.RenderEncoder(dev, enc, render3d.SurfaceDesc{Width: 64, Height: 64, HasDepth: true}, views, func(ctx render3d.ViewportContext) {
		ctx.Pass.SetPipeline(pipe)
		if ctx.LayerMask == 1 {
			ctx.Pass.SetBindGroup(1, redBG)
			nearQuad.Draw(ctx.Pass)
		} else {
			ctx.Pass.SetBindGroup(1, blueBG)
			farQuad.Draw(ctx.Pass)
		}
	})
	if err != nil {
		t.Fatalf("RenderEncoder: %v", err)
	}
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	// Top-right quadrant [32..63, 0..31] has the PiP blue quad despite being farther in Z.
	viewportAssertPixel(t, img, 48, 16, color.RGBA{R: 0, G: 0, B: 255, A: 255})
	// Other three quadrants preserve the main view's red quad.
	viewportAssertPixel(t, img, 16, 16, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	viewportAssertPixel(t, img, 16, 48, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	viewportAssertPixel(t, img, 48, 48, color.RGBA{R: 255, G: 0, B: 0, A: 255})
}

// TestInterleavedOffscreenTargetsKeepEarlierViews renders the left half of an atlas, then another render texture, then the right half of the atlas (ordered by Priority), and checks that the atlas keeps the left half drawn before the other target.
func TestInterleavedOffscreenTargetsKeepEarlierViews(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(64, 64, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	renderer, err := render3d.NewViewportRenderer(nil, dev)
	if err != nil {
		t.Fatalf("NewViewportRenderer: %v", err)
	}
	t.Cleanup(func() { _ = renderer.Close() })
	newRT := func(label string) *render3d.RenderTexture {
		rt, err := render3d.NewRenderTexture(nil, dev, render3d.RenderTextureDesc{Label: label, Width: 32, Height: 32, DepthFormat: gpu.FormatDepth24Plus})
		if err != nil {
			t.Fatalf("NewRenderTexture %s: %v", label, err)
		}
		t.Cleanup(func() { _ = rt.Close() })
		return rt
	}
	atlas, other := newRT("atlas"), newRT("other")

	colorLayout := viewportColorLayout(t, nil, dev)
	solidPipe := viewportSolidPipeline(t, nil, dev, renderer.CameraLayout(), colorLayout, atlas.ColorFormat(), atlas.DepthFormat(), 1)
	texLayout, err := render3d.TextureBindingLayout(nil, dev)
	if err != nil {
		t.Fatalf("TextureBindingLayout: %v", err)
	}
	texPipe := viewportTexturedPipeline(t, nil, dev, renderer.CameraLayout(), texLayout, gpu.FormatRGBA8Unorm, gpu.FormatDepth24Plus)
	samp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	atlasBG, err := render3d.BindTexture(nil, dev, texLayout, atlas.GPUTexture(), samp)
	if err != nil {
		t.Fatalf("BindTexture: %v", err)
	}
	bigQuad, err := render3d.UploadMesh(nil, dev, viewportFacingQuadMesh(math3d.Vec3{}, 8, 8), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh bigQuad: %v", err)
	}
	screenQuad, err := render3d.UploadMesh(nil, dev, viewportFacingQuadMesh(math3d.Vec3{}, 2, 2), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh screenQuad: %v", err)
	}
	redBG := viewportColorBindGroup(t, nil, dev, colorLayout, 1, 0, 0, 1, false)
	greenBG := viewportColorBindGroup(t, nil, dev, colorLayout, 0, 1, 0, 1, false)
	blueBG := viewportColorBindGroup(t, nil, dev, colorLayout, 0, 0, 1, 1, false)

	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 1, 0.1, 20)
	halves := render3d.SplitScreenHorizontal(2)
	views := []render3d.CameraView{
		{Name: "atlas-left", Camera: cam, Target: atlas, Viewport: halves[0], Priority: 0},
		{Name: "other", Camera: cam, Target: other, Priority: 1},
		{Name: "atlas-right", Camera: cam, Target: atlas, Viewport: halves[1], Priority: 2},
		{Name: "main", Camera: cam, PreserveAspect: true},
	}
	colors := []*gpu.BindGroup{redBG, greenBG, blueBG}

	enc := dev.BeginEncoder()
	err = renderer.RenderEncoder(dev, enc, render3d.SurfaceDesc{Width: 64, Height: 64, HasDepth: true}, views, func(ctx render3d.ViewportContext) {
		if ctx.View.Target == nil {
			ctx.Pass.SetPipeline(texPipe)
			ctx.Pass.SetBindGroup(1, atlasBG)
			screenQuad.Draw(ctx.Pass)
			return
		}
		ctx.Pass.SetPipeline(solidPipe)
		ctx.Pass.SetBindGroup(1, colors[ctx.ViewIndex])
		bigQuad.Draw(ctx.Pass)
	})
	if err != nil {
		t.Fatalf("RenderEncoder: %v", err)
	}
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	for _, y := range []int{8, 32, 56} {
		viewportAssertPixel(t, img, 8, y, color.RGBA{R: 255, A: 255})
		viewportAssertPixel(t, img, 28, y, color.RGBA{R: 255, A: 255})
		viewportAssertPixel(t, img, 36, y, color.RGBA{B: 255, A: 255})
		viewportAssertPixel(t, img, 56, y, color.RGBA{B: 255, A: 255})
	}
}

// TestRenderTextureResizeAndRebind verifies Resize no-op, reallocation, generation increment, old GPUTexture invalidation, failed-resize preservation, and sampling the resized render texture.
func TestRenderTextureResizeAndRebind(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(64, 64, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	rt, err := render3d.NewRenderTexture(nil, dev, render3d.RenderTextureDesc{
		Label:       "resizable",
		Width:       16,
		Height:      16,
		DepthFormat: gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewRenderTexture: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	if rt.Width() != 16 || rt.Height() != 16 || rt.Aspect() != 1 || rt.Generation() != 1 {
		t.Fatalf("initial metadata: %dx%d aspect=%g gen=%d", rt.Width(), rt.Height(), rt.Aspect(), rt.Generation())
	}

	// Same dimensions: no-op.
	resized, err := rt.Resize(dev, 16, 16)
	if err != nil || resized || rt.Generation() != 1 {
		t.Fatalf("same-size Resize = (%v, %v) gen=%d, want (false, nil) gen=1", resized, err, rt.Generation())
	}

	// Invalid dimensions: returns ErrInvalidRenderTexture and preserves current target.
	if _, err := rt.Resize(dev, 0, 32); !errors.Is(err, render3d.ErrInvalidRenderTexture) {
		t.Fatalf("Resize(0,32) = %v, want ErrInvalidRenderTexture", err)
	}
	if _, err := rt.Resize(nil, 32, 32); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("Resize(nil dev) = %v, want ErrDeviceLost", err)
	}
	if !rt.Valid() || rt.Width() != 16 || rt.Generation() != 1 {
		t.Fatal("failed Resize corrupted existing RenderTexture")
	}

	oldGPUTex := rt.GPUTexture()
	resized, err = rt.Resize(dev, 64, 32)
	if err != nil || !resized {
		t.Fatalf("Resize(64,32) = (%v, %v), want (true, nil)", resized, err)
	}
	if rt.Width() != 64 || rt.Height() != 32 || rt.Aspect() != 2 || rt.Generation() != 2 {
		t.Fatalf("resized metadata: %dx%d aspect=%g gen=%d", rt.Width(), rt.Height(), rt.Aspect(), rt.Generation())
	}
	if oldGPUTex.Valid() {
		t.Error("previous GPUTexture handle must become invalid after Resize")
	}
	if !rt.Valid() || !rt.GPUTexture().Valid() || rt.GPUTexture().Width() != 64 || rt.GPUTexture().Height() != 32 {
		t.Fatal("resized GPUTexture not valid with updated dimensions")
	}

	// Render cyan into resized target via BeginPass and sample it onto the main surface.
	enc := dev.BeginEncoder()
	pass := rt.BeginPass(enc, core.RGB(0x00ffff))
	if pass == nil {
		t.Fatal("BeginPass returned nil")
	}
	pass.End()

	texLayout, err := render3d.TextureBindingLayout(nil, dev)
	if err != nil {
		t.Fatalf("TextureBindingLayout: %v", err)
	}
	samp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	bg, err := render3d.BindTexture(nil, dev, texLayout, rt.GPUTexture(), samp)
	if err != nil {
		t.Fatalf("BindTexture after resize: %v", err)
	}
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 1, 0.1, 20)
	camBuf.WriteCamera(cam)

	texPipe := viewportTexturedPipeline(t, nil, dev, camBuf.Layout(), texLayout, gpu.FormatRGBA8Unorm, gpu.FormatDepth24Plus)
	quad, err := render3d.UploadMesh(nil, dev, viewportFacingQuadMesh(math3d.Vec3{}, 2, 2), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	mainPass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	mainPass.SetPipeline(texPipe)
	mainPass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	mainPass.SetBindGroup(1, bg)
	quad.Draw(mainPass)
	mainPass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	viewportAssertPixel(t, sr.Image(), 32, 32, color.RGBA{R: 0, G: 255, B: 255, A: 255})
}

// TestRetainedRenderTextureAndViewportRendererAfterDeviceLoss verifies that Retained: true on RenderTexture + ViewportRenderer survives GPU device loss and restore without recreating the RenderTexture, its BindTexture bind group, or the ViewportRenderer.
func TestRetainedRenderTextureAndViewportRendererAfterDeviceLoss(t *testing.T) {
	const size = 64
	var (
		renderer  *render3d.ViewportRenderer
		rt        *render3d.RenderTexture
		mesh      *render3d.GPUMesh
		samp      *render3d.GPUSampler
		rtBG      *gpu.BindGroup
		greenBG   *gpu.BindGroup
		solidPipe *gpu.Pipeline
		texPipe   *gpu.Pipeline
		frames    int
	)
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 1, 0.1, 20)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "retained-rtt",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				var err error
				colorLayout := viewportColorLayout(t, cx, dev)
				texLayout, err := render3d.TextureBindingLayout(cx, dev)
				if err != nil {
					t.Fatalf("TextureBindingLayout: %v", err)
				}
				if renderer == nil {
					renderer, err = render3d.NewViewportRenderer(cx, dev)
					if err != nil {
						t.Fatalf("NewViewportRenderer: %v", err)
					}
				}
				if rt == nil {
					rt, err = render3d.NewRenderTexture(cx, dev, render3d.RenderTextureDesc{
						Label:       "retained-rt",
						Width:       32,
						Height:      32,
						ColorFormat: gpu.FormatRGBA8Unorm,
						DepthFormat: gpu.FormatDepth24Plus,
						Retained:    true,
					})
					if err != nil {
						t.Fatalf("NewRenderTexture: %v", err)
					}
				}
				if mesh == nil {
					mesh, err = render3d.UploadMesh(cx, dev, viewportFacingQuadMesh(math3d.Vec3{}, 1.5, 1.5), render3d.MeshUpload{Label: "quad", Retained: true})
					if err != nil {
						t.Fatalf("UploadMesh: %v", err)
					}
				}
				if samp == nil {
					sOpts := render3d.SamplerNearestClamp()
					sOpts.Retained = true
					samp, err = render3d.NewGPUSampler(cx, dev, sOpts)
					if err != nil {
						t.Fatalf("NewGPUSampler: %v", err)
					}
				}
				if rtBG == nil {
					rtBG, err = render3d.BindTexture(cx, dev, texLayout, rt.GPUTexture(), samp)
					if err != nil {
						t.Fatalf("BindTexture: %v", err)
					}
				}
				if greenBG == nil {
					greenBG = viewportColorBindGroup(t, cx, dev, colorLayout, 0, 1, 0, 1, true)
				}
				solidPipe = viewportSolidPipeline(t, cx, dev, renderer.CameraLayout(), colorLayout, rt.ColorFormat(), rt.DepthFormat(), 1)
				texPipe = viewportTexturedPipeline(t, cx, dev, renderer.CameraLayout(), texLayout, dev.SurfaceFormat(), gpu.FormatDepth24Plus)
			},
			OnFrame: func(frame *gpu.Frame) {
				frames++
				views := []render3d.CameraView{
					{Name: "off", Camera: cam, Target: rt, LayerMask: 1, ClearColor: core.RGB(0x101010)},
					{Name: "main", Camera: cam, LayerMask: 2, ClearColor: core.RGB(0x050505)},
				}
				err := renderer.RenderFrame(frame, render3d.SurfaceDesc{HasDepth: true}, views, func(ctx render3d.ViewportContext) {
					if ctx.LayerMask == 1 {
						ctx.Pass.SetPipeline(solidPipe)
						ctx.Pass.SetBindGroup(1, greenBG)
						mesh.Draw(ctx.Pass)
					} else {
						ctx.Pass.SetPipeline(texPipe)
						ctx.Pass.SetBindGroup(1, rtBG)
						mesh.Draw(ctx.Pass)
					}
				})
				if err != nil {
					t.Fatalf("RenderFrame: %v", err)
				}
				frame.Submit()
			},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	checkFrame := func() {
		t.Helper()
		before := frames
		app.Advance(16 * time.Millisecond)
		if frames == before {
			t.Fatal("OnFrame did not run")
		}
		img, err := app.GPUReadPixels("id:retained-rtt")
		if err != nil {
			t.Fatalf("GPUReadPixels: %v", err)
		}
		viewportAssertPixel(t, img, size/2, size/2, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	}

	checkFrame()
	firstRT, firstRenderer := rt, renderer

	app.TriggerGPUDeviceLost("id:retained-rtt", nsgotest.GPULostContextLost, "context lost")
	if firstRT.Valid() || firstRenderer.Valid() {
		t.Fatalf("resources still valid while device lost: rt=%v renderer=%v", firstRT.Valid(), firstRenderer.Valid())
	}

	app.RestoreGPUDevice("id:retained-rtt")
	if rt != firstRT || !firstRT.Valid() || !firstRT.GPUTexture().Valid() {
		t.Fatalf("retained RenderTexture not restored: same=%v valid=%v", rt == firstRT, firstRT.Valid())
	}
	if renderer != firstRenderer || !firstRenderer.Valid() {
		t.Fatalf("retained ViewportRenderer not restored: same=%v valid=%v", renderer == firstRenderer, firstRenderer.Valid())
	}
	checkFrame()
}

func TestRenderTextureScopeDisposal(t *testing.T) {
	var (
		rt       *render3d.RenderTexture
		renderer *render3d.ViewportRenderer
		show     *core.Signal[bool]
		device   *gpu.Device
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(dev *gpu.Device) {
					var err error
					device = dev
					rt, err = render3d.NewRenderTexture(cx, dev, render3d.RenderTextureDesc{Width: 16, Height: 16, DepthFormat: gpu.FormatDepth24Plus})
					if err != nil {
						t.Fatalf("NewRenderTexture: %v", err)
					}
					renderer, err = render3d.NewViewportRenderer(cx, dev)
					if err != nil {
						t.Fatalf("NewViewportRenderer: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())

	if !rt.Valid() || !renderer.Valid() {
		t.Fatal("RenderTexture and ViewportRenderer must be valid while mounted")
	}
	// Each Resize moves the target into a fresh child scope and disposes the previous one; the latest target still belongs to the view's scope.
	app.Post(func() {
		for i := range 4 {
			old := rt.GPUTexture()
			if resized, err := rt.Resize(device, uint32(17+i), 16); err != nil || !resized {
				t.Errorf("Resize %d = (%v, %v), want (true, nil)", i, resized, err)
			}
			if old.Valid() {
				t.Errorf("Resize %d left the previous texture valid", i)
			}
		}
	})
	app.Settle()
	if !rt.Valid() || rt.Width() != 20 || rt.Generation() != 5 {
		t.Fatalf("after resizes: valid=%v width=%d generation=%d, want valid 20 5", rt.Valid(), rt.Width(), rt.Generation())
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if rt.Valid() || renderer.Valid() {
		t.Errorf("resources still valid after scope disposal: rt=%v renderer=%v", rt.Valid(), renderer.Valid())
	}
	app.Post(func() {
		if _, err := rt.Resize(device, 32, 32); !errors.Is(err, render3d.ErrInvalidRenderTexture) {
			t.Errorf("Resize after scope disposal = %v, want ErrInvalidRenderTexture", err)
		}
		if err := rt.Close(); err != nil {
			t.Errorf("Close after scope disposal: %v", err)
		}
	})
	app.Settle()
}

func TestViewportRectSplitHelpersAndSeamTiling(t *testing.T) {
	// 3-way split on a non-divisible width (100px) must tile [0, 100) with zero gaps and zero overlap.
	for _, count := range []int{1, 2, 3, 4, 5} {
		rects := render3d.SplitScreenHorizontal(count)
		if len(rects) != count {
			t.Fatalf("SplitScreenHorizontal(%d) len = %d", count, len(rects))
		}
		var cursor int32
		for i, r := range rects {
			pv, ok := r.Resolve(100, 75)
			if !ok {
				t.Fatalf("count=%d idx=%d failed Resolve", count, i)
			}
			if pv.X != cursor || pv.Y != 0 || pv.Height != 75 {
				t.Fatalf("count=%d idx=%d seam mismatch: got X=%d (want %d) Y=%d H=%d", count, i, pv.X, cursor, pv.Y, pv.Height)
			}
			cursor += pv.Width
		}
		if cursor != 100 {
			t.Fatalf("count=%d total width = %d, want 100", count, cursor)
		}
	}

	// Vertical stacked tiling on odd height (75px).
	vRects := render3d.SplitScreenVertical(3)
	var yCursor int32
	for i, r := range vRects {
		pv, ok := r.Resolve(80, 75)
		if !ok || pv.Y != yCursor || pv.X != 0 || pv.Width != 80 {
			t.Fatalf("vertical idx=%d mismatch: %+v ok=%v want Y=%d", i, pv, ok, yCursor)
		}
		yCursor += pv.Height
	}
	if yCursor != 75 {
		t.Fatalf("vertical total height = %d, want 75", yCursor)
	}

	// 4-player quad split covers all 4 quadrants.
	quad := render3d.SplitScreenQuad()
	wantQuad := [4][4]int32{
		{0, 0, 50, 40},
		{50, 0, 50, 40},
		{0, 40, 50, 40},
		{50, 40, 50, 40},
	}
	for i, r := range quad {
		pv, ok := r.Resolve(100, 80)
		if !ok || [4]int32{pv.X, pv.Y, pv.Width, pv.Height} != wantQuad[i] {
			t.Fatalf("quad[%d] = %+v ok=%v, want %v", i, pv, ok, wantQuad[i])
		}
		if pv.Bounds(1) != (core.Bounds{Origin: core.Point{X: core.Points(pv.X), Y: core.Points(pv.Y)}, Size: core.Size{Width: core.Points(pv.Width), Height: core.Points(pv.Height)}}) {
			t.Fatalf("quad[%d] Bounds(1) mismatch", i)
		}
	}

	// Bounds divides physical pixels by the content scale to get logical points.
	retina := render3d.PixelViewport{X: 100, Y: 40, Width: 200, Height: 120}
	wantPts := core.Bounds{Origin: core.Point{X: 50, Y: 20}, Size: core.Size{Width: 100, Height: 60}}
	if got := retina.Bounds(2); got != wantPts {
		t.Fatalf("Bounds(2) = %+v, want %+v", got, wantPts)
	}
	for _, bad := range []float32{0, -1, float32(math.NaN()), float32(math.Inf(1))} {
		if got := retina.Bounds(bad); got != (core.Bounds{Origin: core.Point{X: 100, Y: 40}, Size: core.Size{Width: 200, Height: 120}}) {
			t.Fatalf("Bounds(%g) = %+v, want the pixel rect unchanged", bad, got)
		}
	}

	// ViewportFromPixels round-trips cleanly.
	fromPx := render3d.ViewportFromPixels(20, 10, 60, 30, 100, 50)
	pv, ok := fromPx.Resolve(100, 50)
	if !ok || pv.X != 20 || pv.Y != 10 || pv.Width != 60 || pv.Height != 30 {
		t.Fatalf("ViewportFromPixels resolve = %+v ok=%v", pv, ok)
	}
	if math.Abs(float64(fromPx.Aspect(100, 50)-2.0)) > 1e-6 {
		t.Fatalf("Aspect = %g, want 2.0", fromPx.Aspect(100, 50))
	}

	if render3d.SplitScreenHorizontal(0) != nil || render3d.SplitScreenVertical(-1) != nil {
		t.Error("non-positive split counts must return nil")
	}
}

func TestLayerFilteringAndOrderCameraViews(t *testing.T) {
	items := []render3d.LayeredDrawItem{
		{Layer: 1 << 0, Item: render3d.DrawItem{ID: 1}},
		{Layer: 1 << 1, Item: render3d.DrawItem{ID: 2}},
		{Layer: (1 << 0) | (1 << 1), Item: render3d.DrawItem{ID: 3}},
		{Layer: 0, Item: render3d.DrawItem{ID: 4}},
	}
	dst := make([]render3d.DrawItem, 0, len(items))

	got1 := render3d.FilterLayeredItems(items, 1<<0, dst[:0])
	if len(got1) != 2 || got1[0].ID != 1 || got1[1].ID != 3 {
		t.Fatalf("mask 1<<0 = %+v, want IDs [1, 3]", got1)
	}
	if got0 := render3d.FilterLayeredItems(items, 0, dst[:0]); len(got0) != 0 {
		t.Fatalf("mask 0 = %+v, want empty", got0)
	}

	// Zero steady-state allocations when reusing dst.
	if allocs := testing.AllocsPerRun(50, func() {
		dst = render3d.FilterLayeredItems(items, scene3d.LayerAll, dst[:0])
	}); allocs != 0 {
		t.Fatalf("FilterLayeredItems allocated %v times", allocs)
	}

	n1 := scene3d.NewNode("n1")
	n1.Layer = 1 << 0
	n2 := scene3d.NewNode("n2")
	n2.Layer = 1 << 2
	nodes := []*scene3d.Node{n1, nil, n2}
	nodeDst := make([]*scene3d.Node, 0, 4)
	filteredNodes := render3d.FilterNodesByLayer(nodes, 1<<2, nodeDst[:0])
	if len(filteredNodes) != 1 || filteredNodes[0] != n2 {
		t.Fatalf("FilterNodesByLayer = %v, want [n2]", filteredNodes)
	}
	if allocs := testing.AllocsPerRun(50, func() {
		nodeDst = render3d.FilterNodesByLayer(nodes, scene3d.LayerAll, nodeDst[:0])
	}); allocs != 0 {
		t.Fatalf("FilterNodesByLayer allocated %v times", allocs)
	}

	// OrderCameraViews places offscreen targets first, sorted stably by Priority, skipping Disabled.
	fakeRT := &render3d.RenderTexture{}
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 5}, math3d.DegToRad(60), 1, 0.1, 50)
	views := []render3d.CameraView{
		{Name: "main-hi", Camera: cam, Priority: 10},
		{Name: "disabled", Camera: cam, Target: fakeRT, Priority: -5, Disabled: true},
		{Name: "off-b", Camera: cam, Target: fakeRT, Priority: 5},
		{Name: "main-lo", Camera: cam, Priority: 1},
		{Name: "off-a", Camera: cam, Target: fakeRT, Priority: 2},
		{Name: "off-b2", Camera: cam, Target: fakeRT, Priority: 5},
	}
	order := make([]int, 0, len(views))
	order = render3d.OrderCameraViews(views, order[:0])
	wantOrder := []int{4, 2, 5, 3, 0}
	if len(order) != len(wantOrder) {
		t.Fatalf("order = %v, want %v", order, wantOrder)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("order = %v, want %v", order, wantOrder)
		}
	}
	if allocs := testing.AllocsPerRun(50, func() {
		order = render3d.OrderCameraViews(views, order[:0])
	}); allocs != 0 {
		t.Fatalf("OrderCameraViews allocated %v times", allocs)
	}

	// CameraView zero LayerMask defaults to scene3d.LayerAll.
	cv := render3d.CameraView{Camera: cam}
	if cv.EffectiveLayerMask() != scene3d.LayerAll {
		t.Fatalf("EffectiveLayerMask zero = %#x, want LayerAll", cv.EffectiveLayerMask())
	}
	cv2 := render3d.NewCameraView(cam)
	if cv2.LayerMask != scene3d.LayerAll || cv2.Viewport != render3d.FullViewport() || cv2.ClearDepth != 1 {
		t.Fatalf("NewCameraView defaults = %+v", cv2)
	}
}

func TestRenderTextureAndViewportValidationAndNilSafety(t *testing.T) {
	// RenderTextureDesc validation errors wrap ErrInvalidRenderTexture.
	badDescs := []struct {
		name string
		desc render3d.RenderTextureDesc
	}{
		{"zero width", render3d.RenderTextureDesc{Width: 0, Height: 16}},
		{"zero height", render3d.RenderTextureDesc{Width: 16, Height: 0}},
		{"depth as color", render3d.RenderTextureDesc{Width: 16, Height: 16, ColorFormat: gpu.FormatDepth24Plus}},
		{"color as depth", render3d.RenderTextureDesc{Width: 16, Height: 16, DepthFormat: gpu.FormatRGBA8Unorm}},
		{"negative samples", render3d.RenderTextureDesc{Width: 16, Height: 16, SampleCount: -1}},
		{"unsupported 2x samples", render3d.RenderTextureDesc{Width: 16, Height: 16, SampleCount: 2}},
		{"unsupported 8x samples", render3d.RenderTextureDesc{Width: 16, Height: 16, SampleCount: 8}},
	}
	for _, tc := range badDescs {
		t.Run("desc/"+tc.name, func(t *testing.T) {
			if err := tc.desc.Validate(); !errors.Is(err, render3d.ErrInvalidRenderTexture) {
				t.Fatalf("Validate = %v, want ErrInvalidRenderTexture", err)
			}
		})
	}
	if _, err := render3d.NewRenderTexture(nil, nil, render3d.RenderTextureDesc{Width: 16, Height: 16}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewRenderTexture(nil dev) = %v, want ErrDeviceLost", err)
	}

	// ViewportRect validation errors wrap ErrInvalidViewport.
	nan := float32(math.NaN())
	badRects := []struct {
		name string
		rect render3d.ViewportRect
	}{
		{"nan x", render3d.ViewportRect{X: nan, Width: 1, Height: 1}},
		{"negative x", render3d.ViewportRect{X: -0.1, Width: 0.5, Height: 0.5}},
		{"zero width with nonzero height", render3d.ViewportRect{Width: 0, Height: 0.5}},
		{"exceeds right", render3d.ViewportRect{X: 0.6, Width: 0.5, Height: 1}},
		{"exceeds bottom", render3d.ViewportRect{Y: 0.6, Width: 1, Height: 0.5}},
		{"min depth > max depth", render3d.ViewportRect{Width: 1, Height: 1, MinDepth: 0.8, MaxDepth: 0.2}},
		{"depth > 1", render3d.ViewportRect{Width: 1, Height: 1, MaxDepth: 1.5}},
	}
	for _, tc := range badRects {
		t.Run("rect/"+tc.name, func(t *testing.T) {
			if err := tc.rect.Validate(); !errors.Is(err, render3d.ErrInvalidViewport) {
				t.Fatalf("Validate = %v, want ErrInvalidViewport", err)
			}
		})
	}

	// Nil safety on RenderTexture and ViewportRenderer.
	var nilRT *render3d.RenderTexture
	if nilRT.Valid() || nilRT.Target() != nil || nilRT.Texture() != nil || nilRT.ColorTexture() != nil ||
		nilRT.DepthTexture() != nil || nilRT.GPUTexture() != nil || nilRT.Width() != 0 || nilRT.Height() != 0 ||
		nilRT.Aspect() != 0 || nilRT.ColorFormat() != gpu.FormatUndefined || nilRT.DepthFormat() != gpu.FormatUndefined ||
		nilRT.SampleCount() != 0 || nilRT.Generation() != 0 || nilRT.ByteSize() != 0 ||
		nilRT.BeginPass(nil, 0) != nil || nilRT.BeginFramePass(nil, 0) != nil || nilRT.Close() != nil {
		t.Error("nil RenderTexture methods returned unexpected values")
	}
	if _, err := nilRT.Resize(nil, 16, 16); !errors.Is(err, render3d.ErrInvalidRenderTexture) {
		t.Errorf("nilRT.Resize = %v, want ErrInvalidRenderTexture", err)
	}

	var nilRenderer *render3d.ViewportRenderer
	if nilRenderer.Valid() || nilRenderer.CameraLayout() != nil || nilRenderer.Close() != nil {
		t.Error("nil ViewportRenderer methods returned unexpected values")
	}

	// Renderer validation errors before recording commands.
	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	if _, err := render3d.NewViewportRenderer(nil, nil); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewViewportRenderer(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewViewportRendererWithLayout(nil, dev, nil); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("NewViewportRendererWithLayout(nil layout) = %v, want ErrInvalidDescriptor", err)
	}

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()
	rWithLayout, err := render3d.NewViewportRendererWithLayout(nil, dev, camBuf.Layout())
	if err != nil {
		t.Fatalf("NewViewportRendererWithLayout: %v", err)
	}
	// Closing rWithLayout must NOT close the borrowed camBuf.Layout().
	if err := rWithLayout.Close(); err != nil {
		t.Fatalf("rWithLayout.Close: %v", err)
	}
	if !camBuf.Layout().Valid() {
		t.Error("closing ViewportRendererWithLayout closed caller's camera layout")
	}

	r, err := render3d.NewViewportRenderer(nil, dev)
	if err != nil {
		t.Fatalf("NewViewportRenderer: %v", err)
	}
	enc := dev.BeginEncoder()
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 5}, math3d.DegToRad(60), 1, 0.1, 50)
	if err := r.RenderEncoder(dev, enc, render3d.SurfaceDesc{Width: 32, Height: 32}, []render3d.CameraView{{Name: "nil-cam"}}, nil); !errors.Is(err, render3d.ErrInvalidViewport) {
		t.Errorf("nil camera = %v, want ErrInvalidViewport", err)
	}
	closedRT, err := render3d.NewRenderTexture(nil, dev, render3d.RenderTextureDesc{Width: 16, Height: 16})
	if err != nil {
		t.Fatalf("NewRenderTexture: %v", err)
	}
	_ = closedRT.Close()
	if err := r.RenderEncoder(dev, enc, render3d.SurfaceDesc{Width: 32, Height: 32}, []render3d.CameraView{{Camera: cam, Target: closedRT}}, nil); !errors.Is(err, render3d.ErrInvalidRenderTexture) {
		t.Errorf("closed target = %v, want ErrInvalidRenderTexture", err)
	}
	if err := r.RenderEncoder(dev, enc, render3d.SurfaceDesc{Width: 0, Height: 32}, []render3d.CameraView{{Camera: cam}}, nil); !errors.Is(err, render3d.ErrInvalidViewport) {
		t.Errorf("zero surface = %v, want ErrInvalidViewport", err)
	}
	if err := r.RenderEncoder(nil, enc, render3d.SurfaceDesc{Width: 32, Height: 32}, []render3d.CameraView{{Camera: cam}}, nil); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("nil device = %v, want ErrDeviceLost", err)
	}
	// Empty / all-disabled views succeed as no-op.
	if err := r.RenderEncoder(dev, enc, render3d.SurfaceDesc{Width: 32, Height: 32}, []render3d.CameraView{{Disabled: true}}, nil); err != nil {
		t.Errorf("disabled view = %v, want nil", err)
	}
	_ = r.Close()
	if err := r.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
	if err := r.RenderEncoder(dev, enc, render3d.SurfaceDesc{Width: 32, Height: 32}, []render3d.CameraView{{Camera: cam}}, nil); !errors.Is(err, render3d.ErrInvalidViewport) {
		t.Errorf("closed renderer = %v, want ErrInvalidViewport", err)
	}
}
