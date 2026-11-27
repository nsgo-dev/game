package render3d_test

import (
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

// texturedClipWGSL draws a MeshVertexLayout mesh in clip space, sampling @group(0) texture+sampler.
const texturedClipWGSL = `
@group(0) @binding(0) var tex: texture_2d<f32>;
@group(0) @binding(1) var samp: sampler;

struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) pos: vec4<f32>,
	@location(0) uv: vec2<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.pos = vec4<f32>(in.pos, 1.0);
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return textureSample(tex, samp, in.uv);
}

@fragment
fn fs_mip1(in: VertexOut) -> @location(0) vec4<f32> {
	return textureSampleLevel(tex, samp, in.uv, 1.0);
}
`

// textured3DWGSL combines render3d.CameraWGSL at group 0 with a texture+sampler at group 1 and shades textured meshes by a simple directional/view term.
const textured3DWGSL = render3d.CameraWGSL + `
@group(1) @binding(0) var base_tex: texture_2d<f32>;
@group(1) @binding(1) var base_samp: sampler;

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
	let albedo = textureSample(base_tex, base_samp, in.uv);
	let light_dir = normalize(vec3<f32>(0.45, 0.8, 0.4));
	let ndl = max(dot(normalize(in.normal), light_dir), 0.0);
	let shade = 0.35 + 0.65 * ndl;
	return vec4<f32>(albedo.rgb * shade, albedo.a);
}
`

func texturedQuadMesh(x0, y0, x1, y1, u0, v0, u1, v1 float32) render3d.Mesh {
	n := math3d.Vec3{Z: 1}
	return render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: x0, Y: y0, Z: 0.5},
			{X: x1, Y: y0, Z: 0.5},
			{X: x1, Y: y1, Z: 0.5},
			{X: x0, Y: y1, Z: 0.5},
		},
		Normals: []math3d.Vec3{n, n, n, n},
		UVs: []math3d.Vec2{
			{X: u0, Y: v1},
			{X: u1, Y: v1},
			{X: u1, Y: v0},
			{X: u0, Y: v0},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

// fourColorImage returns a 2x2 NRGBA image with distinct corner colors:
// (0,0)=Red, (1,0)=Green, (0,1)=Blue, (1,1)=White.
func fourColorImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{R: 0, G: 255, B: 0, A: 255})
	img.SetNRGBA(0, 1, color.NRGBA{R: 0, G: 0, B: 255, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	return img
}

func checkerImage(size int, a, b color.NRGBA) *image.NRGBA {
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

func texturedCubeMesh(center math3d.Vec3, half float32, uvMax float32) render3d.Mesh {
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
			math3d.Vec2{X: 0, Y: uvMax},
			math3d.Vec2{X: uvMax, Y: uvMax},
			math3d.Vec2{X: uvMax, Y: 0},
			math3d.Vec2{X: 0, Y: 0},
		)
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	return m
}

func groundPlaneMesh(half, uvMax float32) render3d.Mesh {
	up := math3d.Vec3Up()
	return render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: -half, Y: 0, Z: half},
			{X: half, Y: 0, Z: half},
			{X: half, Y: 0, Z: -half},
			{X: -half, Y: 0, Z: -half},
		},
		Normals: []math3d.Vec3{up, up, up, up},
		UVs: []math3d.Vec2{
			{X: 0, Y: uvMax},
			{X: uvMax, Y: uvMax},
			{X: uvMax, Y: 0},
			{X: 0, Y: 0},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

func clipTexturedPipeline(t *testing.T, cx *core.Scope, dev *gpu.Device, layout *gpu.BindGroupLayout, fragEntry string, depth gpu.TextureFormat) *gpu.Pipeline {
	t.Helper()
	mod, err := gpu.CompileWGSL(texturedClipWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	desc := gpu.PipelineDesc{
		Label:         "textured-clip",
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: fragEntry},
		Layouts:       []*gpu.BindGroupLayout{layout},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
		Targets:       []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
	}
	if depth != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{Format: depth, DepthWrite: true, DepthCompare: gpu.CompareLess}
	}
	pipe, err := dev.CreatePipeline(cx, desc)
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	return pipe
}

func TestTexturedMeshSceneMatchesReference(t *testing.T) {
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

	texLayout, err := render3d.TextureBindingLayout(nil, dev)
	if err != nil {
		t.Fatalf("TextureBindingLayout: %v", err)
	}
	mod, err := gpu.CompileWGSL(textured3DWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	pipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Label:         "textured-3d-ref",
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{camBuf.Layout(), texLayout},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
		DepthStencil: &gpu.DepthStencilState{
			Format:       gpu.FormatDepth24Plus,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		},
		Targets: []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}

	// Ground: sRGB checkerboard uploaded from an image.NRGBA with mipmaps, sampled with linear repeat.
	floorImg := checkerImage(8, color.NRGBA{R: 225, G: 215, B: 195, A: 255}, color.NRGBA{R: 75, G: 95, B: 120, A: 255})
	floorTex, err := render3d.UploadTexture(nil, dev, floorImg, render3d.TextureUpload{Label: "floor", SRGB: true, Mipmaps: true})
	if err != nil {
		t.Fatalf("UploadTexture floor: %v", err)
	}
	floorSamp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerLinearRepeat())
	if err != nil {
		t.Fatalf("NewGPUSampler floor: %v", err)
	}
	floorBG, err := render3d.BindTexture(nil, dev, texLayout, floorTex, floorSamp)
	if err != nil {
		t.Fatalf("BindTexture floor: %v", err)
	}
	floorMesh, err := render3d.UploadMesh(nil, dev, groundPlaneMesh(4.5, 3), render3d.MeshUpload{Label: "floor"})
	if err != nil {
		t.Fatalf("UploadMesh floor: %v", err)
	}

	// Left cube: crisp 2x2 four-color image sampled with nearest clamp.
	cubeLeftTex, err := render3d.UploadTexture(nil, dev, fourColorImage(), render3d.TextureUpload{Label: "cube-left"})
	if err != nil {
		t.Fatalf("UploadTexture cube-left: %v", err)
	}
	cubeLeftSamp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler cube-left: %v", err)
	}
	cubeLeftBG, err := render3d.BindTexture(nil, dev, texLayout, cubeLeftTex, cubeLeftSamp)
	if err != nil {
		t.Fatalf("BindTexture cube-left: %v", err)
	}
	cubeLeftMesh, err := render3d.UploadMesh(nil, dev, texturedCubeMesh(math3d.Vec3{X: -1.35, Y: 0.65, Z: 0.4}, 0.65, 1), render3d.MeshUpload{Label: "cube-left"})
	if err != nil {
		t.Fatalf("UploadMesh cube-left: %v", err)
	}

	// Right cube: linear gradient uploaded from a raw PixelBuffer with row padding (stride > width*4), sampled with linear mirror repeat.
	const gradW, gradH, gradStride = 4, 4, 24 // 8 bytes of row padding
	gradPix := make([]byte, gradH*gradStride)
	for y := range gradH {
		for x := range gradW {
			off := y*gradStride + x*4
			gradPix[off+0] = byte(40 + x*65)
			gradPix[off+1] = byte(40 + y*65)
			gradPix[off+2] = 220
			gradPix[off+3] = 255
		}
	}
	cubeRightTex, err := render3d.UploadTexturePixels(nil, dev, render3d.PixelBuffer{
		Width:  gradW,
		Height: gradH,
		Stride: gradStride,
		Format: gpu.FormatRGBA8Unorm,
		Pixels: gradPix,
	}, render3d.TextureUpload{Label: "cube-right", Mipmaps: true})
	if err != nil {
		t.Fatalf("UploadTexturePixels cube-right: %v", err)
	}
	cubeRightSamp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerLinearMirror())
	if err != nil {
		t.Fatalf("NewGPUSampler cube-right: %v", err)
	}
	cubeRightBG, err := render3d.BindTexture(nil, dev, texLayout, cubeRightTex, cubeRightSamp)
	if err != nil {
		t.Fatalf("BindTexture cube-right: %v", err)
	}
	cubeRightMesh, err := render3d.UploadMesh(nil, dev, texturedCubeMesh(math3d.Vec3{X: 1.35, Y: 0.65, Z: -0.35}, 0.65, 2), render3d.MeshUpload{Label: "cube-right"})
	if err != nil {
		t.Fatalf("UploadMesh cube-right: %v", err)
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0.4, Y: 2.8, Z: 5.2}, math3d.DegToRad(48), float32(width)/float32(height), 0.1, 50)
	cam.LookAt(math3d.Vec3{Y: 0.55}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "textured-scene-reference",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())

	pass.SetBindGroup(1, floorBG)
	floorMesh.Draw(pass)

	pass.SetBindGroup(1, cubeLeftBG)
	cubeLeftMesh.Draw(pass)

	pass.SetBindGroup(1, cubeRightBG)
	cubeRightMesh.Draw(pass)

	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	assertReference(t, "textured_mesh_scene", img)

	// Analytical sanity checks in addition to the golden comparison.
	wantPixel(t, img, 2, 2, meshClear)
	// Left cube center projects to a textured face, not the background.
	if s, _, ok := cam.WorldToScreen(math3d.Vec3{X: -1.35, Y: 0.65, Z: 1.05}, width, height); !ok {
		t.Fatal("left cube front face not visible")
	} else if c := img.RGBAAt(int(s.X), int(s.Y)); c == meshClear {
		t.Errorf("left cube front pixel (%d,%d) = clear color, want textured", int(s.X), int(s.Y))
	}
	// Right cube front face mirrors the gradient around its center: bright at center (u=1, v=1), blue-dominant near its top-left corner (u~0, v~0).
	if s, _, ok := cam.WorldToScreen(math3d.Vec3{X: 1.35, Y: 0.65, Z: 0.3}, width, height); !ok {
		t.Fatal("right cube front center not visible")
	} else if c := img.RGBAAt(int(s.X), int(s.Y)); c.R < 100 || c.G < 100 || c.B < 100 {
		t.Errorf("right cube front center (%d,%d) = %v, want bright mirrored center", int(s.X), int(s.Y), c)
	}
	if s, _, ok := cam.WorldToScreen(math3d.Vec3{X: 0.85, Y: 1.15, Z: 0.3}, width, height); !ok {
		t.Fatal("right cube front corner not visible")
	} else if c := img.RGBAAt(int(s.X), int(s.Y)); c.B <= c.R || c.B <= c.G {
		t.Errorf("right cube front corner (%d,%d) = %v, want blue-dominant corner", int(s.X), int(s.Y), c)
	}
}

func TestTexturedFilteringAndWrappingMatchesReference(t *testing.T) {
	const size = 128
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	layout, err := render3d.TextureBindingLayout(nil, dev)
	if err != nil {
		t.Fatalf("TextureBindingLayout: %v", err)
	}
	pipe := clipTexturedPipeline(t, nil, dev, layout, "fs_main", gpu.FormatUndefined)

	tex, err := render3d.UploadTexture(nil, dev, fourColorImage(), render3d.TextureUpload{Label: "four-color"})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}

	type panel struct {
		mesh render3d.Mesh
		opts render3d.SamplerOptions
	}
	panels := []panel{
		// Top-left: nearest + clamp, UVs [-0.5, 1.5]
		{texturedQuadMesh(-1, 0, 0, 1, -0.5, -0.5, 1.5, 1.5), render3d.SamplerNearestClamp()},
		// Top-right: linear + clamp, UVs [-0.5, 1.5]
		{texturedQuadMesh(0, 0, 1, 1, -0.5, -0.5, 1.5, 1.5), render3d.SamplerLinearClamp()},
		// Bottom-left: nearest + repeat, UVs [0, 2]
		{texturedQuadMesh(-1, -1, 0, 0, 0, 0, 2, 2), render3d.SamplerNearestRepeat()},
		// Bottom-right: linear + mirror repeat, UVs [0, 2]
		{texturedQuadMesh(0, -1, 1, 0, 0, 0, 2, 2), render3d.SamplerLinearMirror()},
	}

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{
		LoadOp:     gpu.LoadClear,
		StoreOp:    gpu.StoreStore,
		ClearColor: core.RGB(0x10141c),
	}}})
	pass.SetPipeline(pipe)

	for i, p := range panels {
		gm, err := render3d.UploadMesh(nil, dev, p.mesh, render3d.MeshUpload{Label: "panel"})
		if err != nil {
			t.Fatalf("UploadMesh %d: %v", i, err)
		}
		samp, err := render3d.NewGPUSampler(nil, dev, p.opts)
		if err != nil {
			t.Fatalf("NewGPUSampler %d: %v", i, err)
		}
		bg, err := render3d.BindTexture(nil, dev, layout, tex, samp)
		if err != nil {
			t.Fatalf("BindTexture %d: %v", i, err)
		}
		pass.SetBindGroup(0, bg)
		gm.Draw(pass)
	}

	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	assertReference(t, "textured_filtering_wrapping", img)

	// Top-left (nearest + clamp): outer top-left border (u<0, v<0) clamps to Red; no blended mid-tones inside.
	wantPixel(t, img, 4, 4, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	wantPixel(t, img, 60, 60, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	// Top-right (linear + clamp): center (x=96, y=32 -> u=0.5, v=0.5) blends all 4 texels -> (128, 128, 128).
	mid := img.RGBAAt(96, 32)
	if mid.R < 110 || mid.R > 145 || mid.G < 110 || mid.G > 145 || mid.B < 110 || mid.B > 145 {
		t.Errorf("linear center pixel (96,32) = %v, want ~50%% blend of all 4 colors", mid)
	}
	// Bottom-left (nearest + repeat over [0, 2]): second tile repeats red at top-left of tile (1, 0).
	wantPixel(t, img, 4, 68, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	wantPixel(t, img, 36, 68, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	// Bottom-right (linear + mirror over [0, 2]): symmetric across u=1 seam (x=96).
	leftOfSeam := img.RGBAAt(90, 76)
	rightOfSeam := img.RGBAAt(101, 76)
	diff := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	if diff(leftOfSeam.R, rightOfSeam.R) > 12 || diff(leftOfSeam.G, rightOfSeam.G) > 12 || diff(leftOfSeam.B, rightOfSeam.B) > 12 {
		t.Errorf("mirror repeat not symmetric across seam: left=%v right=%v", leftOfSeam, rightOfSeam)
	}
}

func TestTextureSRGBAndLinearSampling(t *testing.T) {
	const size = 16
	drawSingle := func(srgb bool) color.RGBA {
		sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined)
		defer sr.Close()
		dev := sr.Device()
		layout, err := render3d.TextureBindingLayout(nil, dev)
		if err != nil {
			t.Fatalf("TextureBindingLayout: %v", err)
		}
		pipe := clipTexturedPipeline(t, nil, dev, layout, "fs_main", gpu.FormatUndefined)
		img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		c := color.NRGBA{R: 188, G: 128, B: 50, A: 255}
		for y := range 2 {
			for x := range 2 {
				img.SetNRGBA(x, y, c)
			}
		}
		tex, err := render3d.UploadTexture(nil, dev, img, render3d.TextureUpload{SRGB: srgb})
		if err != nil {
			t.Fatalf("UploadTexture(srgb=%v): %v", srgb, err)
		}
		if tex.SRGB() != srgb {
			t.Fatalf("SRGB() = %v, want %v", tex.SRGB(), srgb)
		}
		samp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
		if err != nil {
			t.Fatalf("NewGPUSampler: %v", err)
		}
		bg, err := render3d.BindTexture(nil, dev, layout, tex, samp)
		if err != nil {
			t.Fatalf("BindTexture: %v", err)
		}
		gm, err := render3d.UploadMesh(nil, dev, texturedQuadMesh(-1, -1, 1, 1, 0, 0, 1, 1), render3d.MeshUpload{})
		if err != nil {
			t.Fatalf("UploadMesh: %v", err)
		}
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(0, bg)
		gm.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		return sr.Image().RGBAAt(size/2, size/2)
	}

	lin := drawSingle(false)
	wantPixel(t, singlePixelImage(lin), 0, 0, color.RGBA{R: 188, G: 128, B: 50, A: 255})

	// sRGB (188, 128, 50) decodes on sample to linear (~0.503, ~0.216, ~0.032) -> ~(128, 55, 8) in RGBA8Unorm target.
	srgbDecoded := drawSingle(true)
	wantPixel(t, singlePixelImage(srgbDecoded), 0, 0, color.RGBA{R: 128, G: 55, B: 8, A: 255})
}

func singlePixelImage(c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, c)
	return img
}

func TestTextureMipmapsLinearAndSRGB(t *testing.T) {
	// A 2x2 black/white checkerboard has linear average 0.5.
	// When SRGB is true, CPU mip generation averages in linear light (storing sRGB byte 188 at mip 1),
	// so sampling mip 1 on the GPU decodes back to linear 0.5 (~128), NOT naive gamma average (~55).
	bw := checkerImage(2, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, color.NRGBA{R: 0, G: 0, B: 0, A: 255})

	for _, srgb := range []bool{false, true} {
		name := "linear"
		if srgb {
			name = "srgb"
		}
		t.Run(name, func(t *testing.T) {
			sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
			defer sr.Close()
			dev := sr.Device()
			layout, err := render3d.TextureBindingLayout(nil, dev)
			if err != nil {
				t.Fatalf("TextureBindingLayout: %v", err)
			}
			pipe := clipTexturedPipeline(t, nil, dev, layout, "fs_mip1", gpu.FormatUndefined)
			tex, err := render3d.UploadTexture(nil, dev, bw, render3d.TextureUpload{SRGB: srgb, Mipmaps: true})
			if err != nil {
				t.Fatalf("UploadTexture: %v", err)
			}
			if tex.MipLevels() != 2 {
				t.Fatalf("MipLevels = %d, want 2", tex.MipLevels())
			}
			// 2x2 (16 bytes) + 1x1 (4 bytes) = 20 bytes.
			if got := tex.ByteSize(); got != 20 {
				t.Errorf("ByteSize = %d, want 20", got)
			}
			samp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerLinearClamp())
			if err != nil {
				t.Fatalf("NewGPUSampler: %v", err)
			}
			bg, err := render3d.BindTexture(nil, dev, layout, tex, samp)
			if err != nil {
				t.Fatalf("BindTexture: %v", err)
			}
			gm, err := render3d.UploadMesh(nil, dev, texturedQuadMesh(-1, -1, 1, 1, 0, 0, 1, 1), render3d.MeshUpload{})
			if err != nil {
				t.Fatalf("UploadMesh: %v", err)
			}
			enc := dev.BeginEncoder()
			pass := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
			pass.SetPipeline(pipe)
			pass.SetBindGroup(0, bg)
			gm.Draw(pass)
			pass.End()
			dev.Submit(enc.Finish())
			sr.WaitIdle()
			wantPixel(t, sr.Image(), 8, 8, color.RGBA{R: 128, G: 128, B: 128, A: 255})
		})
	}

	// Verify MaxMipLevels capping and odd/non-power-of-two dimensions (5x3 -> 3 levels: 5x3, 2x1, 1x1).
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	capped, err := render3d.UploadTexture(nil, dev, checkerImage(8, color.NRGBA{A: 255}, color.NRGBA{R: 255, A: 255}), render3d.TextureUpload{
		Mipmaps:      true,
		MaxMipLevels: 2,
	})
	if err != nil {
		t.Fatalf("UploadTexture capped: %v", err)
	}
	if capped.MipLevels() != 2 {
		t.Errorf("capped MipLevels = %d, want 2", capped.MipLevels())
	}
	// 8x8*4 + 4x4*4 = 256 + 64 = 320.
	if got := capped.ByteSize(); got != 320 {
		t.Errorf("capped ByteSize = %d, want 320", got)
	}

	oddImg := image.NewNRGBA(image.Rect(0, 0, 5, 3))
	for i := 0; i < len(oddImg.Pix); i += 4 {
		oddImg.Pix[i], oddImg.Pix[i+1], oddImg.Pix[i+2], oddImg.Pix[i+3] = 200, 100, 50, 255
	}
	oddTex, err := render3d.UploadTexture(nil, dev, oddImg, render3d.TextureUpload{SRGB: true, Mipmaps: true})
	if err != nil {
		t.Fatalf("UploadTexture odd: %v", err)
	}
	if oddTex.MipLevels() != 3 {
		t.Errorf("5x3 MipLevels = %d, want 3", oddTex.MipLevels())
	}
	// 5*3*4 + 2*1*4 + 1*1*4 = 60 + 8 + 4 = 72.
	if got := oddTex.ByteSize(); got != 72 {
		t.Errorf("5x3 ByteSize = %d, want 72", got)
	}
}

func TestTextureFormatsR8AndRG8(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	layout, err := render3d.TextureBindingLayout(nil, dev)
	if err != nil {
		t.Fatalf("TextureBindingLayout: %v", err)
	}
	pipe := clipTexturedPipeline(t, nil, dev, layout, "fs_main", gpu.FormatUndefined)
	samp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	gm, err := render3d.UploadMesh(nil, dev, texturedQuadMesh(-1, -1, 1, 1, 0, 0, 1, 1), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}

	// R8Unorm with padded stride (4 bytes per row for width 2).
	r8Buf := render3d.PixelBuffer{
		Width:  2,
		Height: 2,
		Stride: 4,
		Format: gpu.FormatR8Unorm,
		Pixels: []byte{180, 180, 0, 0, 180, 180, 0, 0},
	}
	r8Tex, err := render3d.UploadTexturePixels(nil, dev, r8Buf, render3d.TextureUpload{Mipmaps: true})
	if err != nil {
		t.Fatalf("UploadTexturePixels R8: %v", err)
	}
	if r8Tex.Format() != gpu.FormatR8Unorm || r8Tex.ByteSize() != 5 { // 2*2*1 + 1*1*1
		t.Errorf("R8 format=%v bytes=%d, want R8Unorm / 5", r8Tex.Format(), r8Tex.ByteSize())
	}
	r8BG, err := render3d.BindTexture(nil, dev, layout, r8Tex, samp)
	if err != nil {
		t.Fatalf("BindTexture R8: %v", err)
	}
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(0, r8BG)
	gm.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	wantPixel(t, sr.Image(), 8, 8, color.RGBA{R: 180, G: 0, B: 0, A: 255})

	// RG8Unorm with mipmaps.
	rg8Buf := render3d.PixelBuffer{
		Width:  2,
		Height: 2,
		Format: gpu.FormatRG8Unorm,
		Pixels: []byte{60, 210, 60, 210, 60, 210, 60, 210},
	}
	rg8Tex, err := render3d.UploadTexturePixels(nil, dev, rg8Buf, render3d.TextureUpload{Mipmaps: true})
	if err != nil {
		t.Fatalf("UploadTexturePixels RG8: %v", err)
	}
	if rg8Tex.Format() != gpu.FormatRG8Unorm || rg8Tex.ByteSize() != 10 { // 2*2*2 + 1*1*2
		t.Errorf("RG8 format=%v bytes=%d, want RG8Unorm / 10", rg8Tex.Format(), rg8Tex.ByteSize())
	}
	rg8BG, err := render3d.BindTexture(nil, dev, layout, rg8Tex, samp)
	if err != nil {
		t.Fatalf("BindTexture RG8: %v", err)
	}
	enc2 := dev.BeginEncoder()
	pass2 := enc2.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
	pass2.SetPipeline(pipe)
	pass2.SetBindGroup(0, rg8BG)
	gm.Draw(pass2)
	pass2.End()
	dev.Submit(enc2.Finish())
	sr.WaitIdle()
	wantPixel(t, sr.Image(), 8, 8, color.RGBA{R: 60, G: 210, B: 0, A: 255})
}

func texturedViewApp(t *testing.T, retained bool, opts ...nsgotest.Option) (*nsgotest.App, **render3d.GPUTexture, **render3d.GPUSampler, *int) {
	t.Helper()
	const size = 64
	var (
		mesh   *render3d.GPUMesh
		tex    *render3d.GPUTexture
		samp   *render3d.GPUSampler
		bg     *gpu.BindGroup
		pipe   *gpu.Pipeline
		frames int
	)
	opts = append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				var err error
				layout, err := render3d.TextureBindingLayout(cx, dev)
				if err != nil {
					t.Fatalf("TextureBindingLayout: %v", err)
				}
				if mesh == nil || !retained {
					mesh, err = render3d.UploadMesh(cx, dev, texturedQuadMesh(-0.5, -0.5, 0.5, 0.5, 0, 0, 1, 1), render3d.MeshUpload{
						Label:    "quad",
						Retained: retained,
					})
					if err != nil {
						t.Fatalf("UploadMesh: %v", err)
					}
				}
				if tex == nil || !retained {
					tex, err = render3d.UploadTexture(cx, dev, fourColorImage(), render3d.TextureUpload{
						Label:    "four",
						Mipmaps:  true,
						Retained: retained,
					})
					if err != nil {
						t.Fatalf("UploadTexture: %v", err)
					}
				}
				if samp == nil || !retained {
					sOpts := render3d.SamplerNearestClamp()
					sOpts.Label = "nearest"
					sOpts.Retained = retained
					samp, err = render3d.NewGPUSampler(cx, dev, sOpts)
					if err != nil {
						t.Fatalf("NewGPUSampler: %v", err)
					}
				}
				bg, err = render3d.BindTexture(cx, dev, layout, tex, samp)
				if err != nil {
					t.Fatalf("BindTexture: %v", err)
				}
				pipe = clipTexturedPipeline(t, cx, dev, layout, "fs_main", gpu.FormatDepth24Plus)
			},
			OnFrame: func(frame *gpu.Frame) {
				frames++
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				pass.SetBindGroup(0, bg)
				mesh.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, opts...)
	return app, &tex, &samp, &frames
}

func wantFourColorQuadDrawn(t *testing.T, app *nsgotest.App, frames *int) {
	t.Helper()
	before := *frames
	app.Advance(16 * time.Millisecond)
	if *frames == before {
		t.Fatal("OnFrame did not run")
	}
	img, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	wantPixel(t, img, 2, 2, meshClear)
	// Quad covers [-0.5, 0.5] -> pixels [16..48]; UV (0,0)=Red at top-left, (1,0)=Green top-right, (0,1)=Blue bottom-left, (1,1)=White bottom-right.
	wantPixel(t, img, 24, 24, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	wantPixel(t, img, 40, 24, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	wantPixel(t, img, 24, 40, color.RGBA{R: 0, G: 0, B: 255, A: 255})
	wantPixel(t, img, 40, 40, color.RGBA{R: 255, G: 255, B: 255, A: 255})
}

func TestTexturedMeshDrawsInGPUView(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			app, texp, sampp, frames := texturedViewApp(t, false, p.opts...)
			wantFourColorQuadDrawn(t, app, frames)
			if !(*texp).Valid() || !(*sampp).Valid() {
				t.Fatalf("texture or sampler not valid: tex=%v samp=%v", (*texp).Valid(), (*sampp).Valid())
			}
		})
	}
}

func TestRetainedTextureAndSamplerDrawAfterDeviceLoss(t *testing.T) {
	app, texp, sampp, frames := texturedViewApp(t, true)
	wantFourColorQuadDrawn(t, app, frames)
	firstTex, firstSamp := *texp, *sampp

	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
	if firstTex.Valid() || firstSamp.Valid() {
		t.Fatalf("texture or sampler still valid while device is lost: tex=%v samp=%v", firstTex.Valid(), firstSamp.Valid())
	}

	app.RestoreGPUDevice("id:viewport")
	if *texp != firstTex || !firstTex.Valid() {
		t.Fatalf("retained texture not restored: same=%v valid=%v", *texp == firstTex, firstTex.Valid())
	}
	if *sampp != firstSamp || !firstSamp.Valid() {
		t.Fatalf("retained sampler not restored: same=%v valid=%v", *sampp == firstSamp, firstSamp.Valid())
	}
	wantFourColorQuadDrawn(t, app, frames)
}

func TestTextureAndSamplerScopeDisposal(t *testing.T) {
	var (
		tex  *render3d.GPUTexture
		samp *render3d.GPUSampler
		show *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(dev *gpu.Device) {
					var err error
					tex, err = render3d.UploadTexture(cx, dev, fourColorImage(), render3d.TextureUpload{})
					if err != nil {
						t.Fatalf("UploadTexture: %v", err)
					}
					samp, err = render3d.NewGPUSampler(cx, dev, render3d.SamplerLinearRepeat())
					if err != nil {
						t.Fatalf("NewGPUSampler: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())

	if !tex.Valid() || !samp.Valid() {
		t.Fatal("texture and sampler must be valid while mounted")
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if tex.Valid() || samp.Valid() {
		t.Errorf("resources still valid after scope disposal: tex=%v samp=%v", tex.Valid(), samp.Valid())
	}
}

func TestTextureUpdateImageAndPixels(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	layout, err := render3d.TextureBindingLayout(nil, dev)
	if err != nil {
		t.Fatalf("TextureBindingLayout: %v", err)
	}
	pipeMip0 := clipTexturedPipeline(t, nil, dev, layout, "fs_main", gpu.FormatUndefined)
	pipeMip1 := clipTexturedPipeline(t, nil, dev, layout, "fs_mip1", gpu.FormatUndefined)
	samp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	gm, err := render3d.UploadMesh(nil, dev, texturedQuadMesh(-1, -1, 1, 1, 0, 0, 1, 1), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}

	solid := func(c color.NRGBA) *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		for y := range 2 {
			for x := range 2 {
				img.SetNRGBA(x, y, c)
			}
		}
		return img
	}

	tex, err := render3d.UploadTexture(nil, dev, solid(color.NRGBA{R: 255, A: 255}), render3d.TextureUpload{Mipmaps: true})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}
	bg, err := render3d.BindTexture(nil, dev, layout, tex, samp)
	if err != nil {
		t.Fatalf("BindTexture: %v", err)
	}

	renderWith := func(p *gpu.Pipeline) color.RGBA {
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
		pass.SetPipeline(p)
		pass.SetBindGroup(0, bg)
		gm.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		return sr.Image().RGBAAt(8, 8)
	}

	wantPixel(t, singlePixelImage(renderWith(pipeMip0)), 0, 0, color.RGBA{R: 255, A: 255})
	wantPixel(t, singlePixelImage(renderWith(pipeMip1)), 0, 0, color.RGBA{R: 255, A: 255})

	// UpdateImage updates both level 0 and regenerated level 1.
	if err := tex.UpdateImage(dev, solid(color.NRGBA{G: 200, A: 255})); err != nil {
		t.Fatalf("UpdateImage: %v", err)
	}
	wantPixel(t, singlePixelImage(renderWith(pipeMip0)), 0, 0, color.RGBA{G: 200, A: 255})
	wantPixel(t, singlePixelImage(renderWith(pipeMip1)), 0, 0, color.RGBA{G: 200, A: 255})

	// UpdatePixels with custom stride updates both levels too.
	blueBuf := render3d.PixelBuffer{
		Width:  2,
		Height: 2,
		Stride: 12,
		Pixels: []byte{
			0, 0, 240, 255, 0, 0, 240, 255, 99, 99, 99, 99,
			0, 0, 240, 255, 0, 0, 240, 255, 99, 99, 99, 99,
		},
	}
	if err := tex.UpdatePixels(dev, blueBuf); err != nil {
		t.Fatalf("UpdatePixels: %v", err)
	}
	wantPixel(t, singlePixelImage(renderWith(pipeMip0)), 0, 0, color.RGBA{B: 240, A: 255})
	wantPixel(t, singlePixelImage(renderWith(pipeMip1)), 0, 0, color.RGBA{B: 240, A: 255})

	// Error paths: size mismatch, format mismatch, nil device, closed texture.
	if err := tex.UpdateImage(dev, image.NewNRGBA(image.Rect(0, 0, 4, 2))); !errors.Is(err, render3d.ErrInvalidTexture) {
		t.Errorf("UpdateImage wrong size = %v, want ErrInvalidTexture", err)
	}
	if err := tex.UpdatePixels(dev, render3d.PixelBuffer{Width: 2, Height: 2, Format: gpu.FormatR8Unorm, Pixels: make([]byte, 4)}); !errors.Is(err, render3d.ErrInvalidTexture) {
		t.Errorf("UpdatePixels wrong format = %v, want ErrInvalidTexture", err)
	}
	if err := tex.UpdatePixels(nil, blueBuf); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("UpdatePixels nil device = %v, want ErrDeviceLost", err)
	}
	_ = tex.Close()
	if err := tex.UpdatePixels(dev, blueBuf); !errors.Is(err, render3d.ErrInvalidTexture) {
		t.Errorf("UpdatePixels closed texture = %v, want ErrInvalidTexture", err)
	}
}

func TestPixelBufferFromImageConversions(t *testing.T) {
	// 1. Sub-image of *image.NRGBA with zero alpha preserves raw RGB channels verbatim.
	nrgba := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	nrgba.SetNRGBA(1, 2, color.NRGBA{R: 12, G: 34, B: 56, A: 0})
	nrgba.SetNRGBA(2, 2, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	sub := nrgba.SubImage(image.Rect(1, 2, 3, 3))
	buf, err := render3d.PixelBufferFromImage(sub)
	if err != nil {
		t.Fatalf("PixelBufferFromImage(NRGBA sub): %v", err)
	}
	if buf.Width != 2 || buf.Height != 1 || buf.Stride != 8 || buf.TightSize() != 8 {
		t.Fatalf("buf = %dx%d stride %d tight %d", buf.Width, buf.Height, buf.Stride, buf.TightSize())
	}
	want := []byte{12, 34, 56, 0, 200, 100, 50, 128}
	for i, w := range want {
		if buf.Pixels[i] != w {
			t.Fatalf("NRGBA sub byte %d = %d, want %d", i, buf.Pixels[i], w)
		}
	}

	// 2. *image.RGBA with premultiplied translucent pixel un-premultiplies to straight RGBA.
	rgba := image.NewRGBA(image.Rect(0, 0, 1, 1))
	rgba.SetRGBA(0, 0, color.RGBA{R: 128, G: 64, B: 0, A: 128}) // premultiplied (0.5, 0.25, 0, 0.5) -> straight (1.0, 0.5, 0, 0.5)
	bufRGBA, err := render3d.PixelBufferFromImage(rgba)
	if err != nil {
		t.Fatalf("PixelBufferFromImage(RGBA): %v", err)
	}
	if bufRGBA.Pixels[0] < 253 || bufRGBA.Pixels[1] < 126 || bufRGBA.Pixels[1] > 129 || bufRGBA.Pixels[2] != 0 || bufRGBA.Pixels[3] != 128 {
		t.Errorf("RGBA un-premultiplied = %v, want ~[255 128 0 128]", bufRGBA.Pixels)
	}

	// 3. *image.Gray expands to R=G=B=Y, A=255.
	gray := image.NewGray(image.Rect(0, 0, 1, 1))
	gray.SetGray(0, 0, color.Gray{Y: 77})
	bufGray, err := render3d.PixelBufferFromImage(gray)
	if err != nil {
		t.Fatalf("PixelBufferFromImage(Gray): %v", err)
	}
	if bufGray.Pixels[0] != 77 || bufGray.Pixels[1] != 77 || bufGray.Pixels[2] != 77 || bufGray.Pixels[3] != 255 {
		t.Errorf("Gray pixels = %v, want [77 77 77 255]", bufGray.Pixels)
	}

	// 4. Nil and empty bounds rejected.
	if _, err := render3d.PixelBufferFromImage(nil); !errors.Is(err, render3d.ErrInvalidTexture) {
		t.Errorf("nil image = %v, want ErrInvalidTexture", err)
	}
	if _, err := render3d.PixelBufferFromImage(image.NewNRGBA(image.Rect(0, 0, 0, 4))); !errors.Is(err, render3d.ErrInvalidTexture) {
		t.Errorf("empty image = %v, want ErrInvalidTexture", err)
	}
}

func TestPixelBufferValidateAndUploadErrors(t *testing.T) {
	tests := []struct {
		name string
		buf  render3d.PixelBuffer
		ok   bool
	}{
		{"valid rgba tight", render3d.PixelBuffer{Width: 2, Height: 2, Pixels: make([]byte, 16)}, true},
		{"valid rgba padded", render3d.PixelBuffer{Width: 2, Height: 2, Stride: 12, Pixels: make([]byte, 20)}, true}, // last row only needs 8 bytes: 12 + 8 = 20
		{"valid rg8", render3d.PixelBuffer{Width: 3, Height: 2, Format: gpu.FormatRG8Unorm, Pixels: make([]byte, 12)}, true},
		{"valid r8", render3d.PixelBuffer{Width: 3, Height: 2, Format: gpu.FormatR8Unorm, Pixels: make([]byte, 6)}, true},
		{"zero width", render3d.PixelBuffer{Width: 0, Height: 2, Pixels: make([]byte, 8)}, false},
		{"negative height", render3d.PixelBuffer{Width: 2, Height: -1, Pixels: make([]byte, 8)}, false},
		{"negative stride", render3d.PixelBuffer{Width: 2, Height: 2, Stride: -4, Pixels: make([]byte, 16)}, false},
		{"stride too small", render3d.PixelBuffer{Width: 2, Height: 2, Stride: 4, Pixels: make([]byte, 16)}, false},
		{"short pixels", render3d.PixelBuffer{Width: 2, Height: 2, Pixels: make([]byte, 15)}, false},
		{"short padded pixels", render3d.PixelBuffer{Width: 2, Height: 2, Stride: 12, Pixels: make([]byte, 19)}, false},
		{"unsupported depth format", render3d.PixelBuffer{Width: 2, Height: 2, Format: gpu.FormatDepth24Plus, Pixels: make([]byte, 16)}, false},
		{"unsupported float format", render3d.PixelBuffer{Width: 2, Height: 2, Format: gpu.FormatRGBA16Float, Pixels: make([]byte, 32)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.buf.Validate()
			if tt.ok && err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
			if !tt.ok && !errors.Is(err, render3d.ErrInvalidTexture) {
				t.Fatalf("Validate = %v, want ErrInvalidTexture", err)
			}
		})
	}

	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	// SRGB with R8/RG8 rejected.
	if _, err := render3d.UploadTexturePixels(nil, dev, render3d.PixelBuffer{Width: 2, Height: 2, Format: gpu.FormatR8Unorm, Pixels: make([]byte, 4)}, render3d.TextureUpload{SRGB: true}); !errors.Is(err, render3d.ErrInvalidTexture) {
		t.Errorf("R8 + SRGB = %v, want ErrInvalidTexture", err)
	}
	// Nil device rejected.
	if _, err := render3d.UploadTexture(nil, nil, fourColorImage(), render3d.TextureUpload{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("UploadTexture(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.TextureBindingLayout(nil, nil); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("TextureBindingLayout(nil dev) = %v, want ErrDeviceLost", err)
	}
}

func TestSamplerOptionsValidateAndPresets(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	presets := []struct {
		name string
		opts render3d.SamplerOptions
	}{
		{"zero", render3d.SamplerOptions{}},
		{"linear-repeat", render3d.SamplerLinearRepeat()},
		{"linear-clamp", render3d.SamplerLinearClamp()},
		{"linear-mirror", render3d.SamplerLinearMirror()},
		{"nearest-repeat", render3d.SamplerNearestRepeat()},
		{"nearest-clamp", render3d.SamplerNearestClamp()},
	}
	for _, p := range presets {
		if err := p.opts.Validate(); err != nil {
			t.Errorf("preset %s invalid: %v", p.name, err)
		}
	}

	aniso := render3d.SamplerLinearRepeat()
	aniso.MaxAnisotropy = 8
	if err := aniso.Validate(); err != nil {
		t.Errorf("anisotropic linear sampler invalid: %v", err)
	}

	invalid := []struct {
		name string
		opts render3d.SamplerOptions
	}{
		{"bad min filter", render3d.SamplerOptions{MinFilter: gpu.FilterMode(99)}},
		{"bad mag filter", render3d.SamplerOptions{MagFilter: gpu.FilterMode(99)}},
		{"bad mip filter", render3d.SamplerOptions{MipmapFilter: gpu.FilterMode(99)}},
		{"bad addr u", render3d.SamplerOptions{AddressU: gpu.AddressMode(99)}},
		{"bad addr v", render3d.SamplerOptions{AddressV: gpu.AddressMode(99)}},
		{"bad addr w", render3d.SamplerOptions{AddressW: gpu.AddressMode(99)}},
		{"neg min lod", render3d.SamplerOptions{MinLOD: -1}},
		{"neg max lod", render3d.SamplerOptions{MaxLOD: -1}},
		{"nan min lod", render3d.SamplerOptions{MinLOD: nan}},
		{"inf max lod", render3d.SamplerOptions{MaxLOD: inf}},
		{"max < min", render3d.SamplerOptions{MinLOD: 4, MaxLOD: 2}},
		{"min > default max", render3d.SamplerOptions{MinLOD: 40}},
		{"aniso with nearest", render3d.SamplerOptions{MinFilter: gpu.FilterNearest, MagFilter: gpu.FilterLinear, MipmapFilter: gpu.FilterLinear, MaxAnisotropy: 4}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.opts.Validate(); !errors.Is(err, render3d.ErrInvalidSampler) {
				t.Fatalf("Validate = %v, want ErrInvalidSampler", err)
			}
		})
	}

	if _, err := render3d.NewGPUSampler(nil, nil, render3d.SamplerLinearRepeat()); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewGPUSampler(nil dev) = %v, want ErrDeviceLost", err)
	}
}

func TestTextureAndSamplerLifecycleAndNilSafety(t *testing.T) {
	var nilTex *render3d.GPUTexture
	if nilTex.Valid() || nilTex.Texture() != nil || nilTex.Width() != 0 || nilTex.Height() != 0 ||
		nilTex.MipLevels() != 0 || nilTex.Format() != gpu.FormatUndefined || nilTex.SRGB() ||
		nilTex.ByteSize() != 0 || nilTex.Close() != nil {
		t.Error("nil GPUTexture methods returned unexpected values")
	}
	var nilSamp *render3d.GPUSampler
	if nilSamp.Valid() || nilSamp.Sampler() != nil || nilSamp.Options() != (render3d.SamplerOptions{}) || nilSamp.Close() != nil {
		t.Error("nil GPUSampler methods returned unexpected values")
	}

	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	layout, err := render3d.TextureBindingLayout(nil, dev)
	if err != nil {
		t.Fatalf("TextureBindingLayout: %v", err)
	}
	tex, err := render3d.UploadTexture(nil, dev, fourColorImage(), render3d.TextureUpload{})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}
	samp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerLinearRepeat())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	if tex.Width() != 2 || tex.Height() != 2 || tex.MipLevels() != 1 || tex.ByteSize() != 16 {
		t.Errorf("tex metadata = %dx%d mips=%d bytes=%d", tex.Width(), tex.Height(), tex.MipLevels(), tex.ByteSize())
	}
	if samp.Options().MaxLOD != 32 || samp.Options().MaxAnisotropy != 1 {
		t.Errorf("normalized sampler opts = %+v", samp.Options())
	}

	if err := tex.Close(); err != nil {
		t.Fatalf("tex.Close: %v", err)
	}
	if err := tex.Close(); err != nil {
		t.Fatalf("second tex.Close: %v", err)
	}
	if tex.Valid() {
		t.Error("closed texture still valid")
	}
	if _, err := render3d.BindTexture(nil, dev, layout, tex, samp); !errors.Is(err, render3d.ErrInvalidTexture) {
		t.Errorf("BindTexture closed tex = %v, want ErrInvalidTexture", err)
	}

	tex2, err := render3d.UploadTexture(nil, dev, fourColorImage(), render3d.TextureUpload{})
	if err != nil {
		t.Fatalf("UploadTexture 2: %v", err)
	}
	if err := samp.Close(); err != nil {
		t.Fatalf("samp.Close: %v", err)
	}
	if err := samp.Close(); err != nil {
		t.Fatalf("second samp.Close: %v", err)
	}
	if samp.Valid() {
		t.Error("closed sampler still valid")
	}
	if _, err := render3d.BindTexture(nil, dev, layout, tex2, samp); !errors.Is(err, render3d.ErrInvalidSampler) {
		t.Errorf("BindTexture closed samp = %v, want ErrInvalidSampler", err)
	}
	if _, err := render3d.BindTexture(nil, dev, nil, tex2, samp); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("BindTexture nil layout = %v, want ErrInvalidDescriptor", err)
	}
}

func FuzzPixelBufferValidate(f *testing.F) {
	f.Add(4, 4, 0, uint8(gpu.FormatRGBA8Unorm), 64)
	f.Add(3, 5, 16, uint8(gpu.FormatRG8Unorm), 70)
	f.Add(0, 4, 0, uint8(gpu.FormatR8Unorm), 0)
	f.Fuzz(func(t *testing.T, w, h, stride int, fmtCode uint8, pixLen int) {
		if pixLen < 0 {
			pixLen = 0
		}
		if pixLen > 1<<16 {
			pixLen = 1 << 16
		}
		b := render3d.PixelBuffer{
			Width:  w,
			Height: h,
			Stride: stride,
			Format: gpu.TextureFormat(fmtCode),
			Pixels: make([]byte, pixLen),
		}
		err := b.Validate()
		if err != nil && !errors.Is(err, render3d.ErrInvalidTexture) {
			t.Fatalf("Validate error does not wrap ErrInvalidTexture: %v", err)
		}
	})
}

func BenchmarkPixelBufferFromImage(b *testing.B) {
	img := checkerImage(256, color.NRGBA{R: 220, G: 180, B: 140, A: 255}, color.NRGBA{R: 40, G: 60, B: 90, A: 255})
	b.SetBytes(256 * 256 * 4)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := render3d.PixelBufferFromImage(img); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUploadTextureWithMipmaps(b *testing.B) {
	img := checkerImage(128, color.NRGBA{R: 220, G: 180, B: 140, A: 255}, color.NRGBA{R: 40, G: 60, B: 90, A: 255})
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()
	b.ReportAllocs()
	for b.Loop() {
		tex, err := render3d.UploadTexture(nil, dev, img, render3d.TextureUpload{SRGB: true, Mipmaps: true})
		if err != nil {
			b.Fatal(err)
		}
		_ = tex.Close()
	}
}
