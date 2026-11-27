package render3d_test

import (
	"errors"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	aaTestWidth  = 96
	aaTestHeight = 96
)

// aaFlatWGSL draws clip-space colored triangles directly so flat interiors are uniform and diagonal edges are crisp high-contrast steps for AA comparison.
const aaFlatWGSL = `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) color: vec4<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) color: vec4<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = vec4<f32>(in.pos, 1.0);
	out.color = in.color;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return in.color;
}
`

// aaBlitWGSL copies a single-sampled color texture onto the swapchain for verifying resolved MSAATarget outputs through gpu.View pixel readback.
const aaBlitWGSL = `
@group(0) @binding(0) var blit_tex: texture_2d<f32>;
@group(0) @binding(1) var blit_samp: sampler;

struct BlitOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) uv: vec2<f32>,
}

@vertex
fn vs_main(@builtin(vertex_index) vid: u32) -> BlitOut {
	var out: BlitOut;
	let x = select(-1.0, 3.0, vid == 1u);
	let y = select(-1.0, 3.0, vid == 2u);
	out.clip = vec4<f32>(x, y, 0.0, 1.0);
	out.uv = vec2<f32>(x * 0.5 + 0.5, 0.5 - y * 0.5);
	return out;
}

@fragment
fn fs_main(in: BlitOut) -> @location(0) vec4<f32> {
	return textureSampleLevel(blit_tex, blit_samp, in.uv, 0.0);
}
`

var (
	aaClearColor = core.RGB(0x101824)
	aaClearRGBA  = color.RGBA{R: 0x10, G: 0x18, B: 0x24, A: 0xff}
	aaWhiteRGBA  = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	aaWarmRGBA   = color.RGBA{R: 0xf0, G: 0x60, B: 0x30, A: 0xff}
	aaCoolRGBA   = color.RGBA{R: 0x30, G: 0xd0, B: 0xf0, A: 0xff}
)

// aaSceneVertices builds a deterministic diagonal geometry scene with shallow and steep diagonal edges plus large solid-colored interiors and background:
//   - a large rotated bright white diamond/quad in the upper-left/center,
//   - a warm orange rotated wedge overlapping in depth (nearer Z),
//   - a cool cyan diagonal triangle in the lower-right.
func aaSceneVertices() []float32 {
	var v []float32
	addTri := func(x0, y0, z0, x1, y1, z1, x2, y2, z2 float32, c color.RGBA) {
		r, g, b, a := float32(c.R)/255, float32(c.G)/255, float32(c.B)/255, float32(c.A)/255
		v = append(v,
			x0, y0, z0, r, g, b, a,
			x1, y1, z1, r, g, b, a,
			x2, y2, z2, r, g, b, a,
		)
	}
	// Rotated white rectangle (two triangles) covering center with shallow ~20° edges. Interior center around (0, 0) stays completely solid white.
	addTri(-0.72, -0.18, 0.6, 0.56, -0.58, 0.6, 0.72, 0.18, 0.6, aaWhiteRGBA)
	addTri(-0.72, -0.18, 0.6, 0.72, 0.18, 0.6, -0.56, 0.58, 0.6, aaWhiteRGBA)

	// Overlapping warm diagonal triangle (closer Z=0.3 tests depth attachment too).
	addTri(-0.85, -0.82, 0.3, -0.05, -0.82, 0.3, -0.62, 0.42, 0.3, aaWarmRGBA)

	// Cool cyan diagonal triangle on the right.
	addTri(0.08, -0.85, 0.4, 0.88, -0.45, 0.4, 0.35, 0.85, 0.4, aaCoolRGBA)
	return v
}

func aaFlatVertexLayout() []gpu.VertexBufferLayout {
	return []gpu.VertexBufferLayout{{
		Stride: 7 * 4,
		Attributes: []gpu.VertexAttribute{
			{Location: 0, Offset: 0, Format: gpu.VertexFloat32x3},
			{Location: 1, Offset: 3 * 4, Format: gpu.VertexFloat32x4},
		},
	}}
}

func readTextureRGBA(t *testing.T, dev *gpu.Device, tex *gpu.Texture) *image.RGBA {
	t.Helper()
	td := tex.Desc()
	w, h := int(td.Width), int(td.Height)
	var out []byte
	var readErr error
	done := false
	dev.ReadTexture(tex, gpu.TextureRegion{Width: td.Width, Height: td.Height}, func(rgba []byte, err error) {
		out = append([]byte(nil), rgba...)
		readErr = err
		done = true
	})
	dev.WaitIdle()
	if !done {
		t.Fatal("ReadTexture callback did not run")
	}
	if readErr != nil {
		t.Fatalf("ReadTexture: %v", readErr)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if len(out) != len(img.Pix) {
		t.Fatalf("ReadTexture bytes = %d, want %d", len(out), len(img.Pix))
	}
	copy(img.Pix, out)
	return img
}

func renderAASceneOffscreen(t *testing.T, sr *gpu.SoftwareRenderer, requestedSamples int) (*image.RGBA, *render3d.MSAATarget) {
	t.Helper()
	dev := sr.Device()
	target, err := render3d.NewMSAATarget(nil, dev, render3d.MSAATargetDesc{
		Label:       "aa-scene",
		Width:       aaTestWidth,
		Height:      aaTestHeight,
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
		SampleCount: requestedSamples,
	})
	if err != nil {
		t.Fatalf("NewMSAATarget(%d): %v", requestedSamples, err)
	}
	t.Cleanup(func() { _ = target.Close() })

	mod, err := gpu.CompileWGSL(aaFlatWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	verts := aaSceneVertices()
	vb, err := dev.CreateBuffer(nil, gpu.BufferDesc{
		Label: "aa-verts",
		Usage: gpu.UsageVertex,
		Data:  packFloat32s(verts),
	})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	t.Cleanup(func() { _ = vb.Close() })

	pipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Label:         "aa-flat",
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		VertexBuffers: aaFlatVertexLayout(),
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
		DepthStencil: &gpu.DepthStencilState{
			Format:       gpu.FormatDepth24Plus,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		},
		Targets:     []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm, WriteMask: gpu.ColorWriteAll}},
		SampleCount: target.SampleCount(),
	})
	if err != nil {
		t.Fatalf("CreatePipeline(samples=%d): %v", target.SampleCount(), err)
	}
	t.Cleanup(func() { _ = pipe.Close() })

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(aaPassDesc(t, target))
	pass.SetPipeline(pipe)
	pass.SetVertexBuffer(0, vb, 0)
	pass.Draw(uint32(len(verts)/7), 0)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	return readTextureRGBA(t, dev, target.OutputTexture()), target
}

// aaPassDesc returns target's render pass descriptor cleared to aaClearColor, failing the test when the target cannot be rendered to.
func aaPassDesc(t testing.TB, target *render3d.MSAATarget) gpu.RenderPassDesc {
	t.Helper()
	desc, err := target.RenderPassDesc(aaClearColor, 1.0)
	if err != nil {
		t.Fatalf("MSAATarget.RenderPassDesc: %v", err)
	}
	return desc
}

func isSolidSceneColor(c color.RGBA) bool {
	near := func(a, b color.RGBA) bool {
		return aaAbsDiff(a.R, b.R) <= 1 && aaAbsDiff(a.G, b.G) <= 1 && aaAbsDiff(a.B, b.B) <= 1 && aaAbsDiff(a.A, b.A) <= 1
	}
	return near(c, aaClearRGBA) || near(c, aaWhiteRGBA) || near(c, aaWarmRGBA) || near(c, aaCoolRGBA)
}

func aaAbsDiff(a, b uint8) int {
	if a >= b {
		return int(a - b)
	}
	return int(b - a)
}

func countAntiAliasedPixels(img *image.RGBA) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !isSolidSceneColor(img.RGBAAt(x, y)) {
				n++
			}
		}
	}
	return n
}

// assertFlatInteriorsUntouched checks known flat-interior and background pixels stay identical to the aliased baseline.
func assertFlatInteriorsUntouched(t *testing.T, label string, base, got *image.RGBA) {
	t.Helper()
	// Background corners and solid triangle interiors well away from edges.
	flatPoints := [][2]int{
		{2, 2},               // top-left background
		{aaTestWidth - 3, 2}, // top-right background
		{48, 48},             // center of white rotated quad
		{20, 76},             // interior of warm orange triangle
		{66, 56},             // interior of cool cyan triangle
	}
	for _, pt := range flatPoints {
		bc := base.RGBAAt(pt[0], pt[1])
		gc := got.RGBAAt(pt[0], pt[1])
		if aaAbsDiff(bc.R, gc.R) > 1 || aaAbsDiff(bc.G, gc.G) > 1 || aaAbsDiff(bc.B, gc.B) > 1 || aaAbsDiff(bc.A, gc.A) > 1 {
			t.Errorf("%s: flat interior pixel (%d,%d) changed from %v to %v", label, pt[0], pt[1], bc, gc)
		}
	}
}

func TestAAModeAndClampSampleCount(t *testing.T) {
	modes := []struct {
		mode    render3d.AAMode
		str     string
		samples int
		msaa    bool
		fxaa    bool
	}{
		{render3d.AANone, "none", 1, false, false},
		{render3d.AAFXAA, "fxaa", 1, false, true},
		{render3d.AAMSAA2x, "msaa2x", 2, true, false},
		{render3d.AAMSAA4x, "msaa4x", 4, true, false},
		{render3d.AAMode(99), "unknown", 1, false, false},
	}
	for _, tc := range modes {
		if got := tc.mode.String(); got != tc.str {
			t.Errorf("AAMode(%d).String() = %q, want %q", tc.mode, got, tc.str)
		}
		if got := tc.mode.SampleCount(); got != tc.samples {
			t.Errorf("AAMode(%s).SampleCount() = %d, want %d", tc.str, got, tc.samples)
		}
		if got := tc.mode.IsMSAA(); got != tc.msaa {
			t.Errorf("AAMode(%s).IsMSAA() = %v, want %v", tc.str, got, tc.msaa)
		}
		if got := tc.mode.UsesFXAA(); got != tc.fxaa {
			t.Errorf("AAMode(%s).UsesFXAA() = %v, want %v", tc.str, got, tc.fxaa)
		}
	}

	// nil device always clamps to 1.
	for _, req := range []int{-1, 0, 1, 2, 4, 8} {
		if got := render3d.ClampSampleCount(nil, req); got != 1 {
			t.Errorf("ClampSampleCount(nil, %d) = %d, want 1", req, got)
		}
	}

	// Device with MSAA4x: <=1 -> 1, >=2 -> 4.
	srFull := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer srFull.Close()
	for _, tc := range []struct{ req, want int }{
		{-4, 1}, {0, 1}, {1, 1}, {2, 4}, {3, 4}, {4, 4}, {8, 4},
	} {
		if got := render3d.ClampSampleCount(srFull.Device(), tc.req); got != tc.want {
			t.Errorf("ClampSampleCount(msaa4x, %d) = %d, want %d", tc.req, got, tc.want)
		}
	}

	// Device without MSAA4x: everything clamps to 1.
	srNoMSAA := gpu.NewSoftwareRendererWithFeatures(8, 8, gpu.FormatUndefined, gpu.Features{})
	defer srNoMSAA.Close()
	for _, req := range []int{0, 1, 2, 4, 8} {
		if got := render3d.ClampSampleCount(srNoMSAA.Device(), req); got != 1 {
			t.Errorf("ClampSampleCount(no-msaa, %d) = %d, want 1", req, got)
		}
	}
}

func TestMSAATargetLifecycleAndResize(t *testing.T) {
	// Error paths.
	if _, err := render3d.NewMSAATarget(nil, nil, render3d.MSAATargetDesc{Width: 16, Height: 16}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewMSAATarget(nil dev) = %v, want ErrDeviceLost", err)
	}
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()
	if _, err := render3d.NewMSAATarget(nil, dev, render3d.MSAATargetDesc{Width: 0, Height: 16}); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Fatalf("NewMSAATarget(0 width) = %v, want ErrInvalidDescriptor", err)
	}

	// 2x requested clamps to 4x on MSAA4x-capable device.
	mt, err := render3d.NewMSAATarget(nil, dev, render3d.MSAATargetDesc{
		Label:       "scene",
		Width:       32,
		Height:      24,
		DepthFormat: gpu.FormatDepth24Plus,
		SampleCount: render3d.AAMSAA2x.SampleCount(),
		Retained:    true,
	})
	if err != nil {
		t.Fatalf("NewMSAATarget: %v", err)
	}
	if !mt.Valid() || mt.SampleCount() != 4 || mt.Width() != 32 || mt.Height() != 24 || !mt.Retained() {
		t.Fatalf("unexpected target state: valid=%v samples=%d size=%dx%d retained=%v",
			mt.Valid(), mt.SampleCount(), mt.Width(), mt.Height(), mt.Retained())
	}
	if mt.ColorTexture() == nil || mt.ResolveTexture() == nil || mt.DepthTexture() == nil {
		t.Fatal("expected Color, Resolve, and Depth textures for 4x MSAA target")
	}
	if mt.OutputTexture() != mt.ResolveTexture() {
		t.Fatal("OutputTexture must return ResolveTexture when multisampled")
	}
	if mt.ColorFormat() != dev.SurfaceFormat() || mt.DepthFormat() != gpu.FormatDepth24Plus {
		t.Fatalf("formats = (%v, %v)", mt.ColorFormat(), mt.DepthFormat())
	}
	ca, err := mt.ColorAttachment(aaClearColor)
	if err != nil {
		t.Fatalf("ColorAttachment: %v", err)
	}
	if ca.Texture != mt.ColorTexture() || ca.ResolveTarget != mt.ResolveTexture() || ca.StoreOp != gpu.StoreDiscard {
		t.Fatalf("unexpected 4x ColorAttachment: %+v", ca)
	}
	ds := mt.DepthStencilAttachment(1.0)
	if ds == nil || ds.Texture != mt.DepthTexture() || ds.ClearDepth != 1.0 || ds.DepthStoreOp != gpu.StoreDiscard {
		t.Fatalf("unexpected DepthStencilAttachment: %+v", ds)
	}

	// Redundant Resize is a zero-work no-op preserving the exact RenderTarget.
	origRT := mt.RenderTarget()
	if err := mt.Resize(32, 24); err != nil {
		t.Fatalf("Resize no-op: %v", err)
	}
	if mt.RenderTarget() != origRT {
		t.Fatal("redundant Resize reallocated RenderTarget")
	}

	// Invalid Resize leaves existing target valid and unchanged.
	if err := mt.Resize(0, 48); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Fatalf("Resize(0, 48) = %v, want ErrInvalidDescriptor", err)
	}
	if !mt.Valid() || mt.RenderTarget() != origRT || mt.Width() != 32 || mt.Height() != 24 {
		t.Fatal("failed Resize corrupted existing target")
	}

	// Real Resize allocates fresh textures and invalidates old target.
	if err := mt.Resize(64, 48); err != nil {
		t.Fatalf("Resize(64, 48): %v", err)
	}
	if !mt.Valid() || mt.Width() != 64 || mt.Height() != 48 || mt.SampleCount() != 4 || !mt.Retained() {
		t.Fatalf("post-resize state invalid: valid=%v size=%dx%d samples=%d retained=%v",
			mt.Valid(), mt.Width(), mt.Height(), mt.SampleCount(), mt.Retained())
	}
	if mt.RenderTarget() == origRT || origRT.Valid() {
		t.Fatal("Resize did not replace and close the previous RenderTarget")
	}

	// Idempotent Close.
	if err := mt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if mt.Valid() || mt.OutputTexture() != nil {
		t.Fatal("target still valid after Close")
	}
	// A closed target must refuse to build attachments: a nil Texture would make NSGo draw into the view's swapchain.
	if _, err := mt.ColorAttachment(aaClearColor); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Fatalf("ColorAttachment after Close = %v, want ErrInvalidDescriptor", err)
	}
	if _, err := mt.RenderPassDesc(aaClearColor, 1.0); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Fatalf("RenderPassDesc after Close = %v, want ErrInvalidDescriptor", err)
	}
	if mt.DepthStencilAttachment(1.0) != nil {
		t.Fatal("DepthStencilAttachment after Close != nil")
	}
	if err := mt.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	// Fallback to 1x on a device without MSAA4x.
	srNoMSAA := gpu.NewSoftwareRendererWithFeatures(16, 16, gpu.FormatUndefined, gpu.Features{})
	defer srNoMSAA.Close()
	mt1, err := render3d.NewMSAATarget(nil, srNoMSAA.Device(), render3d.MSAATargetDesc{
		Width:       16,
		Height:      16,
		SampleCount: render3d.AAMSAA4x.SampleCount(),
	})
	if err != nil {
		t.Fatalf("NewMSAATarget(fallback 1x): %v", err)
	}
	defer func() { _ = mt1.Close() }()
	if mt1.SampleCount() != 1 || mt1.ResolveTexture() != nil || mt1.OutputTexture() != mt1.ColorTexture() {
		t.Fatalf("1x fallback state: samples=%d resolve=%v output==color=%v",
			mt1.SampleCount(), mt1.ResolveTexture(), mt1.OutputTexture() == mt1.ColorTexture())
	}
	ca1, err := mt1.ColorAttachment(aaClearColor)
	if err != nil {
		t.Fatalf("ColorAttachment(1x): %v", err)
	}
	if ca1.ResolveTarget != nil || ca1.StoreOp != gpu.StoreStore {
		t.Fatalf("unexpected 1x ColorAttachment: %+v", ca1)
	}
	if mt1.DepthStencilAttachment(1.0) != nil {
		t.Fatal("expected nil DepthStencilAttachment when DepthFormat is undefined")
	}

	// Nil receiver safety.
	var nilTarget *render3d.MSAATarget
	if nilTarget.Valid() || nilTarget.SampleCount() != 0 || nilTarget.Width() != 0 || nilTarget.Height() != 0 ||
		nilTarget.Retained() || nilTarget.OutputTexture() != nil || nilTarget.ColorTexture() != nil ||
		nilTarget.ResolveTexture() != nil || nilTarget.DepthTexture() != nil || nilTarget.RenderTarget() != nil ||
		nilTarget.DepthStencilAttachment(1) != nil || nilTarget.Close() != nil {
		t.Fatal("nil MSAATarget methods did not return safe zero values")
	}
	if err := nilTarget.Resize(8, 8); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("nil Resize = %v, want ErrDeviceLost", err)
	}
	if _, err := nilTarget.RenderPassDesc(aaClearColor, 1.0); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Fatalf("nil RenderPassDesc = %v, want ErrInvalidDescriptor", err)
	}
}

// TestMSAATargetSampleCountFollowsDevice moves one target between devices with and without Features.MSAA4x: the requested count is kept, so Resize re-clamps it against the current device even at the same size.
func TestMSAATargetSampleCountFollowsDevice(t *testing.T) {
	srNoMSAA := gpu.NewSoftwareRendererWithFeatures(16, 16, gpu.FormatUndefined, gpu.Features{})
	defer srNoMSAA.Close()
	srMSAA := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	defer srMSAA.Close()

	mt, err := render3d.NewMSAATarget(nil, srNoMSAA.Device(), render3d.MSAATargetDesc{
		Width:       16,
		Height:      16,
		ColorFormat: gpu.FormatRGBA8Unorm,
		SampleCount: render3d.AAMSAA4x.SampleCount(),
	})
	if err != nil {
		t.Fatalf("NewMSAATarget: %v", err)
	}
	defer func() { _ = mt.Close() }()
	if mt.SampleCount() != 1 {
		t.Fatalf("SampleCount on a device without MSAA4x = %d, want 1", mt.SampleCount())
	}

	mt.SetDevice(srMSAA.Device())
	if err := mt.Resize(16, 16); err != nil {
		t.Fatalf("Resize after moving to an MSAA4x device: %v", err)
	}
	if mt.SampleCount() != 4 || mt.ResolveTexture() == nil || mt.OutputTexture() != mt.ResolveTexture() {
		t.Fatalf("after moving to an MSAA4x device: samples=%d resolve=%v", mt.SampleCount(), mt.ResolveTexture())
	}

	rt := mt.RenderTarget()
	if err := mt.Resize(16, 16); err != nil || mt.RenderTarget() != rt {
		t.Fatalf("Resize at the same size and count reallocated (err=%v)", err)
	}

	mt.SetDevice(srNoMSAA.Device())
	if err := mt.Resize(16, 16); err != nil {
		t.Fatalf("Resize after moving back: %v", err)
	}
	if mt.SampleCount() != 1 || mt.ResolveTexture() != nil {
		t.Fatalf("after moving back to a device without MSAA4x: samples=%d resolve=%v", mt.SampleCount(), mt.ResolveTexture())
	}
}

// TestMSAATargetKeepDepth checks the depth attachment's store ops: transient by default, stored with KeepDepth, and with explicit stencil ops for a depth-stencil format.
func TestMSAATargetKeepDepth(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	defer sr.Close()
	for _, tc := range []struct {
		name    string
		format  gpu.TextureFormat
		keep    bool
		store   gpu.StoreOp
		stencil bool
	}{
		{"depth-transient", gpu.FormatDepth24Plus, false, gpu.StoreDiscard, false},
		{"depth-kept", gpu.FormatDepth24Plus, true, gpu.StoreStore, false},
		{"depth-stencil-transient", gpu.FormatDepth24PlusStencil8, false, gpu.StoreDiscard, true},
		{"depth-stencil-kept", gpu.FormatDepth24PlusStencil8, true, gpu.StoreStore, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mt, err := render3d.NewMSAATarget(nil, sr.Device(), render3d.MSAATargetDesc{
				Width:       16,
				Height:      16,
				ColorFormat: gpu.FormatRGBA8Unorm,
				DepthFormat: tc.format,
				SampleCount: render3d.AAMSAA4x.SampleCount(),
				KeepDepth:   tc.keep,
			})
			if err != nil {
				t.Fatalf("NewMSAATarget: %v", err)
			}
			defer func() { _ = mt.Close() }()
			ds := mt.DepthStencilAttachment(1.0)
			if ds == nil || ds.DepthLoadOp != gpu.LoadClear || ds.DepthStoreOp != tc.store {
				t.Fatalf("depth attachment = %+v, want LoadClear and store op %v", ds, tc.store)
			}
			if tc.stencil && (ds.StencilLoadOp != gpu.LoadClear || ds.StencilStoreOp != tc.store || ds.ClearStencil != 0) {
				t.Fatalf("stencil ops = (%v, %v, %d), want (LoadClear, %v, 0)", ds.StencilLoadOp, ds.StencilStoreOp, ds.ClearStencil, tc.store)
			}
			if desc := aaPassDesc(t, mt); desc.DepthStencil == nil || desc.DepthStencil.DepthStoreOp != tc.store {
				t.Fatalf("RenderPassDesc depth attachment = %+v", desc.DepthStencil)
			}
		})
	}
}

func TestMSAAResolveSmoothsDiagonalEdges(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(aaTestWidth, aaTestHeight, gpu.FormatUndefined)
	defer sr.Close()

	baseImg, _ := renderAASceneOffscreen(t, sr, render3d.AANone.SampleCount())
	msaa4Img, mt4 := renderAASceneOffscreen(t, sr, render3d.AAMSAA4x.SampleCount())
	msaa2Img, mt2 := renderAASceneOffscreen(t, sr, render3d.AAMSAA2x.SampleCount())

	if mt4.SampleCount() != 4 || mt2.SampleCount() != 4 {
		t.Fatalf("sample counts = (%d, %d), want (4, 4)", mt4.SampleCount(), mt2.SampleCount())
	}

	baseAA := countAntiAliasedPixels(baseImg)
	msaa4AA := countAntiAliasedPixels(msaa4Img)
	if baseAA != 0 {
		t.Fatalf("1x aliased baseline had %d intermediate edge pixels, want 0", baseAA)
	}
	if msaa4AA < 80 {
		t.Fatalf("4x MSAA produced only %d anti-aliased edge pixels, want >= 80", msaa4AA)
	}
	assertFlatInteriorsUntouched(t, "msaa4x", baseImg, msaa4Img)

	// AAMSAA2x promotes to 4x on MSAA4x devices and must produce identical pixels.
	if _, diff := diffMaxChannel(msaa4Img, msaa2Img); diff != 0 {
		t.Fatalf("AAMSAA2x (clamped to 4x) differed from AAMSAA4x by max channel delta %d", diff)
	}

	assertReference(t, "aa_baseline", baseImg)
	assertReference(t, "aa_msaa4x", msaa4Img)
}

func TestFXAAWGSLCompilesAndPassLifecycle(t *testing.T) {
	if _, err := gpu.CompileWGSL(render3d.FXAAWGSL); err != nil {
		t.Fatalf("CompileWGSL(FXAAWGSL): %v", err)
	}

	// FXAAOptions sanitization.
	def := (render3d.FXAAOptions{}).Sanitize()
	if def != render3d.DefaultFXAAOptions() {
		t.Fatalf("zero FXAAOptions.Sanitize() = %+v, want %+v", def, render3d.DefaultFXAAOptions())
	}
	noSub := (render3d.FXAAOptions{DisableSubpixel: true}).Sanitize()
	if noSub.SubpixelQuality != 0 || noSub.EdgeThreshold != def.EdgeThreshold || noSub.EdgeThresholdMin != def.EdgeThresholdMin {
		t.Fatalf("DisableSubpixel sanitize = %+v", noSub)
	}
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	clamped := (render3d.FXAAOptions{SubpixelQuality: 2.5, EdgeThreshold: nan, EdgeThresholdMin: inf}).Sanitize()
	if clamped.SubpixelQuality != 1 || clamped.EdgeThreshold != def.EdgeThreshold || clamped.EdgeThresholdMin != def.EdgeThresholdMin {
		t.Fatalf("out-of-range sanitize = %+v", clamped)
	}
	negSub := (render3d.FXAAOptions{SubpixelQuality: -0.5, EdgeThreshold: 2.0, EdgeThresholdMin: 2.0}).Sanitize()
	if negSub.SubpixelQuality != 0 || negSub.EdgeThreshold != 1 || negSub.EdgeThresholdMin != 1 {
		t.Fatalf("negative/high sanitize = %+v", negSub)
	}

	// Pass creation and error handling.
	if _, err := render3d.NewFXAAPass(nil, nil, render3d.FXAAPassDesc{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewFXAAPass(nil dev) = %v, want ErrDeviceLost", err)
	}

	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	fxaa, err := render3d.NewFXAAPass(nil, dev, render3d.FXAAPassDesc{
		Label:    "fxaa-test",
		Retained: true,
	})
	if err != nil {
		t.Fatalf("NewFXAAPass: %v", err)
	}
	if !fxaa.Valid() || !fxaa.Retained() || fxaa.Pipeline() == nil || fxaa.Layout() == nil || fxaa.Sampler() == nil {
		t.Fatal("fresh FXAAPass missing valid resources")
	}
	if fxaa.TargetFormat() != dev.SurfaceFormat() {
		t.Fatalf("TargetFormat = %v, want %v", fxaa.TargetFormat(), dev.SurfaceFormat())
	}

	// Reject multisampled source texture.
	msaaTex, err := dev.CreateTexture(nil, gpu.TextureDesc{
		Width: 16, Height: 16, SampleCount: 4, Format: gpu.FormatRGBA8Unorm, Usage: gpu.TextureRenderAttachment,
	})
	if err != nil {
		t.Fatalf("CreateTexture 4x: %v", err)
	}
	defer func() { _ = msaaTex.Close() }()

	enc := dev.BeginEncoder()
	if err := fxaa.Encode(enc, msaaTex, nil); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Fatalf("Encode(multisampled src) = %v, want ErrInvalidDescriptor", err)
	}
	if err := fxaa.Encode(enc, nil, nil); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Fatalf("Encode(nil src) = %v, want ErrInvalidDescriptor", err)
	}
	if err := fxaa.Encode(nil, msaaTex, nil); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Fatalf("Encode(nil enc) = %v, want ErrInvalidDescriptor", err)
	}
	if err := fxaa.Draw(nil, msaaTex); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Fatalf("Draw(nil pass) = %v, want ErrInvalidDescriptor", err)
	}

	// SetOptions and Close idempotency.
	fxaa.SetOptions(render3d.FXAAOptions{SubpixelQuality: 0.5, EdgeThreshold: 0.125, EdgeThresholdMin: 0.0625})
	if got := fxaa.Options(); got.SubpixelQuality != 0.5 || got.EdgeThreshold != 0.125 || got.EdgeThresholdMin != 0.0625 {
		t.Fatalf("Options() = %+v", got)
	}
	if err := fxaa.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if fxaa.Valid() {
		t.Fatal("FXAAPass still valid after Close")
	}
	if err := fxaa.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	var nilPass *render3d.FXAAPass
	if nilPass.Valid() || nilPass.Retained() || nilPass.Pipeline() != nil || nilPass.Layout() != nil || nilPass.Sampler() != nil || nilPass.Close() != nil {
		t.Fatal("nil FXAAPass methods did not return safe zero values")
	}
}

func TestApplyFXAAAndGPUParity(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(aaTestWidth, aaTestHeight, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	baseImg, baseTarget := renderAASceneOffscreen(t, sr, render3d.AANone.SampleCount())

	fxaa, err := render3d.NewFXAAPass(nil, dev, render3d.FXAAPassDesc{
		TargetFormat: gpu.FormatRGBA8Unorm,
		Options:      render3d.DefaultFXAAOptions(),
	})
	if err != nil {
		t.Fatalf("NewFXAAPass: %v", err)
	}
	defer func() { _ = fxaa.Close() }()

	dstRT, err := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{
		Label:       "fxaa-out",
		Width:       aaTestWidth,
		Height:      aaTestHeight,
		ColorFormat: gpu.FormatRGBA8Unorm,
		SampleCount: 1,
	})
	if err != nil {
		t.Fatalf("CreateRenderTarget: %v", err)
	}
	defer func() { _ = dstRT.Close() }()

	enc := dev.BeginEncoder()
	if err := fxaa.Encode(enc, baseTarget.OutputTexture(), dstRT.Color); err != nil {
		t.Fatalf("FXAAPass.Encode: %v", err)
	}
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	gpuFXAA := readTextureRGBA(t, dev, dstRT.Color)
	cpuFXAA := render3d.ApplyFXAA(baseImg, render3d.DefaultFXAAOptions())

	// 1. GPU FXAAPass and CPU ApplyFXAA must agree closely across the whole image.
	assertFXAAParity(t, "software-fxaa", gpuFXAA, cpuFXAA)

	// 2. Diagonal edges must be noticeably smoothed vs aliased 1x baseline.
	baseAA := countAntiAliasedPixels(baseImg)
	fxaaAA := countAntiAliasedPixels(gpuFXAA)
	if baseAA != 0 {
		t.Fatalf("aliased baseline had %d intermediate pixels, want 0", baseAA)
	}
	if fxaaAA < 80 {
		t.Fatalf("FXAA produced only %d anti-aliased edge pixels, want >= 80", fxaaAA)
	}

	// 3. Flat interiors and background remain untouched.
	assertFlatInteriorsUntouched(t, "gpu-fxaa", baseImg, gpuFXAA)
	assertFlatInteriorsUntouched(t, "cpu-fxaa", baseImg, cpuFXAA)

	// 4. Alpha channel stays opaque (255) everywhere.
	for y := range aaTestHeight {
		for x := range aaTestWidth {
			if a := gpuFXAA.RGBAAt(x, y).A; a != 255 {
				t.Fatalf("gpuFXAA alpha at (%d,%d) = %d, want 255", x, y, a)
			}
			if a := cpuFXAA.RGBAAt(x, y).A; a != 255 {
				t.Fatalf("cpuFXAA alpha at (%d,%d) = %d, want 255", x, y, a)
			}
		}
	}

	// 5. Golden render match.
	assertReference(t, "aa_fxaa", gpuFXAA)
}

// TestFXAAPassSourcesOfDifferentSizesInOneSubmission draws two sources of different sizes through one FXAAPass in a single submission, alternating A, B, A, and checks each output against ApplyFXAA: each draw needs its own texel size and a bind group that is still alive when the pass runs.
func TestFXAAPassSourcesOfDifferentSizesInOneSubmission(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(aaTestWidth, aaTestHeight, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	imgA, targetA := renderAASceneOffscreen(t, sr, render3d.AANone.SampleCount())

	const bw, bh = 64, 40
	imgB := image.NewRGBA(image.Rect(0, 0, bw, bh))
	for y := range bh {
		for x := range bw {
			if y*5 < x*3+2 {
				imgB.SetRGBA(x, y, aaWarmRGBA)
			} else {
				imgB.SetRGBA(x, y, aaClearRGBA)
			}
		}
	}
	texB, err := dev.CreateTexture(nil, gpu.TextureDesc{
		Label:  "fxaa-src-b",
		Width:  bw,
		Height: bh,
		Format: gpu.FormatRGBA8Unorm,
		Usage:  gpu.TextureSampled | gpu.TextureCopyDst,
	})
	if err != nil {
		t.Fatalf("CreateTexture: %v", err)
	}
	defer func() { _ = texB.Close() }()
	dev.WriteTextureRGBA(texB, 0, 0, imgB)

	newDst := func(label string, w, h uint32) *gpu.RenderTarget {
		rt, err := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{Label: label, Width: w, Height: h, ColorFormat: gpu.FormatRGBA8Unorm, SampleCount: 1})
		if err != nil {
			t.Fatalf("CreateRenderTarget(%s): %v", label, err)
		}
		t.Cleanup(func() { _ = rt.Close() })
		return rt
	}
	dstA1 := newDst("fxaa-a1", aaTestWidth, aaTestHeight)
	dstB := newDst("fxaa-b", bw, bh)
	dstA2 := newDst("fxaa-a2", aaTestWidth, aaTestHeight)

	fxaa, err := render3d.NewFXAAPass(nil, dev, render3d.FXAAPassDesc{TargetFormat: gpu.FormatRGBA8Unorm})
	if err != nil {
		t.Fatalf("NewFXAAPass: %v", err)
	}
	defer func() { _ = fxaa.Close() }()

	enc := dev.BeginEncoder()
	for _, d := range []struct {
		src, dst *gpu.Texture
	}{
		{targetA.OutputTexture(), dstA1.Color},
		{texB, dstB.Color},
		{targetA.OutputTexture(), dstA2.Color},
	} {
		if err := fxaa.Encode(enc, d.src, d.dst); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	refA := render3d.ApplyFXAA(imgA, render3d.DefaultFXAAOptions())
	refB := render3d.ApplyFXAA(imgB, render3d.DefaultFXAAOptions())
	if _, d := diffMaxChannel(imgB, refB); d == 0 {
		t.Fatal("ApplyFXAA left source B unchanged; the test pattern has no edge to smooth")
	}
	assertFXAAParity(t, "source-a-first", readTextureRGBA(t, dev, dstA1.Color), refA)
	assertFXAAParity(t, "source-b", readTextureRGBA(t, dev, dstB.Color), refB)
	assertFXAAParity(t, "source-a-again", readTextureRGBA(t, dev, dstA2.Color), refA)
}

func TestApplyFXAAEdgeCases(t *testing.T) {
	if render3d.ApplyFXAA(nil, render3d.DefaultFXAAOptions()) != nil {
		t.Fatal("ApplyFXAA(nil) != nil")
	}

	// Empty and tiny images must not panic.
	for _, sz := range [][2]int{{0, 0}, {1, 1}, {2, 2}, {3, 1}} {
		src := image.NewRGBA(image.Rect(0, 0, sz[0], sz[1]))
		for i := 0; i < len(src.Pix); i += 4 {
			src.Pix[i+0], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 80, 120, 200, 255
		}
		got := render3d.ApplyFXAA(src, render3d.DefaultFXAAOptions())
		if !got.Bounds().Eq(src.Bounds()) {
			t.Fatalf("size %v bounds mismatch: %v", sz, got.Bounds())
		}
		for i := range src.Pix {
			if got.Pix[i] != src.Pix[i] {
				t.Fatalf("uniform %v image modified at byte %d: %d -> %d", sz, i, src.Pix[i], got.Pix[i])
			}
		}
	}

	// Non-zero origin sub-image preserves bounds and produces identical pixels.
	full := image.NewRGBA(image.Rect(0, 0, 24, 24))
	for y := range 24 {
		for x := range 24 {
			if x*2 > y*3 {
				full.SetRGBA(x, y, color.RGBA{R: 240, G: 240, B: 240, A: 255})
			} else {
				full.SetRGBA(x, y, color.RGBA{R: 20, G: 20, B: 30, A: 255})
			}
		}
	}
	sub := full.SubImage(image.Rect(4, 4, 20, 20)).(*image.RGBA)
	// Build equivalent zero-origin copy.
	zeroOrigin := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			zeroOrigin.SetRGBA(x, y, sub.RGBAAt(4+x, 4+y))
		}
	}
	gotSub := render3d.ApplyFXAA(sub, render3d.DefaultFXAAOptions())
	gotZero := render3d.ApplyFXAA(zeroOrigin, render3d.DefaultFXAAOptions())
	if !gotSub.Bounds().Eq(sub.Bounds()) {
		t.Fatalf("sub-image bounds = %v, want %v", gotSub.Bounds(), sub.Bounds())
	}
	for y := range 16 {
		for x := range 16 {
			if gotSub.RGBAAt(4+x, 4+y) != gotZero.RGBAAt(x, y) {
				t.Fatalf("sub-image pixel (%d,%d) = %v, want %v", x, y, gotSub.RGBAAt(4+x, 4+y), gotZero.RGBAAt(x, y))
			}
		}
	}

	// DisableSubpixel preserves 1px isolated pixel dot more strongly than default SubpixelQuality=0.75.
	dot := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			dot.SetRGBA(x, y, color.RGBA{R: 20, G: 20, B: 20, A: 255})
		}
	}
	dot.SetRGBA(8, 8, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	withSub := render3d.ApplyFXAA(dot, render3d.DefaultFXAAOptions())
	withoutSub := render3d.ApplyFXAA(dot, render3d.FXAAOptions{DisableSubpixel: true})
	if withSub.RGBAAt(8, 8).R >= withoutSub.RGBAAt(8, 8).R {
		t.Fatalf("expected SubpixelQuality=0.75 to soften isolated dot more than DisableSubpixel: with=%d without=%d",
			withSub.RGBAAt(8, 8).R, withoutSub.RGBAAt(8, 8).R)
	}
}

func TestAAProfilesFullAndNoGPUCompute(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"no-gpu-compute", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	// Render a reference 1x aliased image + CPU FXAA reference for parity verification.
	srRef := gpu.NewSoftwareRenderer(aaTestWidth, aaTestHeight, gpu.FormatUndefined)
	defer srRef.Close()
	baseRef, _ := renderAASceneOffscreen(t, srRef, render3d.AANone.SampleCount())
	msaaRef, _ := renderAASceneOffscreen(t, srRef, render3d.AAMSAA4x.SampleCount())
	cpuFXAARef := render3d.ApplyFXAA(baseRef, render3d.DefaultFXAAOptions())

	for _, p := range profiles {
		t.Run(p.name+"/fxaa", func(t *testing.T) {
			var (
				sceneTarget *render3d.MSAATarget
				scenePipe   *gpu.Pipeline
				vb          *gpu.Buffer
				fxaa        *render3d.FXAAPass
				vertsCount  uint32
			)
			opts := append([]nsgotest.Option{nsgotest.Size(aaTestWidth, aaTestHeight), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID: "viewport",
					Style:  core.Style{Width: core.Pt(aaTestWidth), Height: core.Pt(aaTestHeight)},
					OnReady: func(dev *gpu.Device) {
						var err error
						sceneTarget, err = render3d.NewMSAATarget(cx, dev, render3d.MSAATargetDesc{
							Label:       "scene-1x",
							Width:       aaTestWidth,
							Height:      aaTestHeight,
							ColorFormat: gpu.FormatRGBA8Unorm,
							DepthFormat: gpu.FormatDepth24Plus,
							SampleCount: render3d.AAFXAA.SampleCount(),
						})
						if err != nil {
							t.Fatalf("NewMSAATarget: %v", err)
						}
						mod, err := gpu.CompileWGSL(aaFlatWGSL)
						if err != nil {
							t.Fatalf("CompileWGSL: %v", err)
						}
						verts := aaSceneVertices()
						vertsCount = uint32(len(verts) / 7)
						vb, err = dev.CreateBuffer(cx, gpu.BufferDesc{Usage: gpu.UsageVertex, Data: packFloat32s(verts)})
						if err != nil {
							t.Fatalf("CreateBuffer: %v", err)
						}
						scenePipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
							Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
							Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
							VertexBuffers: aaFlatVertexLayout(),
							Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
							DepthStencil: &gpu.DepthStencilState{
								Format:       gpu.FormatDepth24Plus,
								DepthWrite:   true,
								DepthCompare: gpu.CompareLess,
							},
							Targets:     []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm, WriteMask: gpu.ColorWriteAll}},
							SampleCount: sceneTarget.SampleCount(),
						})
						if err != nil {
							t.Fatalf("CreatePipeline: %v", err)
						}
						fxaa, err = render3d.NewFXAAPass(cx, dev, render3d.FXAAPassDesc{
							TargetFormat: dev.SurfaceFormat(),
							Options:      render3d.DefaultFXAAOptions(),
						})
						if err != nil {
							t.Fatalf("NewFXAAPass: %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						pass1 := frame.BeginRenderPass(aaPassDesc(t, sceneTarget))
						pass1.SetPipeline(scenePipe)
						pass1.SetVertexBuffer(0, vb, 0)
						pass1.Draw(vertsCount, 0)
						pass1.End()

						pass2 := frame.BeginRenderPass(gpu.RenderPassDesc{
							ColorAttachments: []gpu.ColorAttachment{{
								LoadOp:  gpu.LoadClear,
								StoreOp: gpu.StoreStore,
							}},
						})
						if err := fxaa.Draw(pass2, sceneTarget.OutputTexture()); err != nil {
							t.Fatalf("fxaa.Draw: %v", err)
						}
						pass2.End()
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)
			img, err := app.GPUReadPixels("id:viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			assertFXAAParity(t, p.name+"-fxaa", img, cpuFXAARef)
			assertFlatInteriorsUntouched(t, p.name+"-fxaa", baseRef, img)
		})

		t.Run(p.name+"/msaa4x", func(t *testing.T) {
			var (
				msaaTarget *render3d.MSAATarget
				scenePipe  *gpu.Pipeline
				vb         *gpu.Buffer
				blitPipe   *gpu.Pipeline
				blitBG     *gpu.BindGroup
				vertsCount uint32
			)
			opts := append([]nsgotest.Option{nsgotest.Size(aaTestWidth, aaTestHeight), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID: "viewport",
					Style:  core.Style{Width: core.Pt(aaTestWidth), Height: core.Pt(aaTestHeight)},
					OnReady: func(dev *gpu.Device) {
						var err error
						msaaTarget, err = render3d.NewMSAATarget(cx, dev, render3d.MSAATargetDesc{
							Label:       "scene-msaa4x",
							Width:       aaTestWidth,
							Height:      aaTestHeight,
							ColorFormat: gpu.FormatRGBA8Unorm,
							DepthFormat: gpu.FormatDepth24Plus,
							SampleCount: render3d.AAMSAA4x.SampleCount(),
						})
						if err != nil {
							t.Fatalf("NewMSAATarget: %v", err)
						}
						mod, err := gpu.CompileWGSL(aaFlatWGSL)
						if err != nil {
							t.Fatalf("CompileWGSL: %v", err)
						}
						verts := aaSceneVertices()
						vertsCount = uint32(len(verts) / 7)
						vb, err = dev.CreateBuffer(cx, gpu.BufferDesc{Usage: gpu.UsageVertex, Data: packFloat32s(verts)})
						if err != nil {
							t.Fatalf("CreateBuffer: %v", err)
						}
						scenePipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
							Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
							Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
							VertexBuffers: aaFlatVertexLayout(),
							Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
							DepthStencil: &gpu.DepthStencilState{
								Format:       gpu.FormatDepth24Plus,
								DepthWrite:   true,
								DepthCompare: gpu.CompareLess,
							},
							Targets:     []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm, WriteMask: gpu.ColorWriteAll}},
							SampleCount: msaaTarget.SampleCount(),
						})
						if err != nil {
							t.Fatalf("CreatePipeline: %v", err)
						}

						blitMod, err := gpu.CompileWGSL(aaBlitWGSL)
						if err != nil {
							t.Fatalf("CompileWGSL(blit): %v", err)
						}
						samp, err := dev.CreateSampler(cx, gpu.SamplerDesc{
							MinFilter: gpu.FilterNearest,
							MagFilter: gpu.FilterNearest,
							AddressU:  gpu.AddressClampToEdge,
							AddressV:  gpu.AddressClampToEdge,
						})
						if err != nil {
							t.Fatalf("CreateSampler: %v", err)
						}
						bgl, err := dev.CreateBindGroupLayout(cx, []gpu.BindGroupLayoutEntry{
							{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingTexture},
							{Binding: 1, Visibility: gpu.StageFragment, Type: gpu.BindingSampler},
						})
						if err != nil {
							t.Fatalf("CreateBindGroupLayout: %v", err)
						}
						blitBG, err = dev.CreateBindGroup(cx, bgl, []gpu.BindGroupEntry{
							{Binding: 0, Texture: msaaTarget.OutputTexture()},
							{Binding: 1, Sampler: samp},
						})
						if err != nil {
							t.Fatalf("CreateBindGroup: %v", err)
						}
						blitPipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
							Vertex:    gpu.ShaderStage{Module: blitMod, EntryPoint: "vs_main"},
							Fragment:  gpu.ShaderStage{Module: blitMod, EntryPoint: "fs_main"},
							Layouts:   []*gpu.BindGroupLayout{bgl},
							Primitive: gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
							Targets:   []gpu.ColorTargetState{{Format: dev.SurfaceFormat(), WriteMask: gpu.ColorWriteAll}},
						})
						if err != nil {
							t.Fatalf("CreatePipeline(blit): %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						pass1 := frame.BeginRenderPass(aaPassDesc(t, msaaTarget))
						pass1.SetPipeline(scenePipe)
						pass1.SetVertexBuffer(0, vb, 0)
						pass1.Draw(vertsCount, 0)
						pass1.End()

						pass2 := frame.BeginRenderPass(gpu.RenderPassDesc{
							ColorAttachments: []gpu.ColorAttachment{{
								LoadOp:  gpu.LoadClear,
								StoreOp: gpu.StoreStore,
							}},
						})
						pass2.SetPipeline(blitPipe)
						pass2.SetBindGroup(0, blitBG)
						pass2.Draw(3, 0)
						pass2.End()
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)
			img, err := app.GPUReadPixels("id:viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			if _, maxDelta := diffMaxChannel(img, msaaRef); maxDelta > 2 {
				t.Fatalf("profile %s MSAA4x mismatch vs reference: maxDelta=%d", p.name, maxDelta)
			}
			assertFlatInteriorsUntouched(t, p.name+"-msaa4x", baseRef, img)
		})
	}
}

func TestMSAATargetAndFXAARetainedAcrossDeviceLoss(t *testing.T) {
	var (
		msaaTarget *render3d.MSAATarget
		scenePipe  *gpu.Pipeline
		vb         *gpu.Buffer
		fxaa       *render3d.FXAAPass
		vertsCount uint32
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID: "viewport",
			Style:  core.Style{Width: core.Pt(aaTestWidth), Height: core.Pt(aaTestHeight)},
			OnReady: func(dev *gpu.Device) {
				if msaaTarget != nil {
					msaaTarget.SetDevice(dev)
					fxaa.SetDevice(dev)
					return
				}
				var err error
				msaaTarget, err = render3d.NewMSAATarget(cx, dev, render3d.MSAATargetDesc{
					Label:       "retained-msaa",
					Width:       aaTestWidth,
					Height:      aaTestHeight,
					ColorFormat: gpu.FormatRGBA8Unorm,
					DepthFormat: gpu.FormatDepth24Plus,
					SampleCount: render3d.AAMSAA4x.SampleCount(),
					Retained:    true,
				})
				if err != nil {
					t.Fatalf("NewMSAATarget: %v", err)
				}
				mod, err := gpu.CompileWGSL(aaFlatWGSL)
				if err != nil {
					t.Fatalf("CompileWGSL: %v", err)
				}
				verts := aaSceneVertices()
				vertsCount = uint32(len(verts) / 7)
				vb, err = dev.CreateBuffer(cx, gpu.BufferDesc{
					Usage:    gpu.UsageVertex,
					Data:     packFloat32s(verts),
					Retained: true,
				})
				if err != nil {
					t.Fatalf("CreateBuffer: %v", err)
				}
				scenePipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
					Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
					Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
					VertexBuffers: aaFlatVertexLayout(),
					Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
					DepthStencil: &gpu.DepthStencilState{
						Format:       gpu.FormatDepth24Plus,
						DepthWrite:   true,
						DepthCompare: gpu.CompareLess,
					},
					Targets:     []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm, WriteMask: gpu.ColorWriteAll}},
					SampleCount: msaaTarget.SampleCount(),
					Retained:    true,
				})
				if err != nil {
					t.Fatalf("CreatePipeline: %v", err)
				}
				fxaa, err = render3d.NewFXAAPass(cx, dev, render3d.FXAAPassDesc{
					TargetFormat: dev.SurfaceFormat(),
					Retained:     true,
				})
				if err != nil {
					t.Fatalf("NewFXAAPass: %v", err)
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				pass1 := frame.BeginRenderPass(aaPassDesc(t, msaaTarget))
				pass1.SetPipeline(scenePipe)
				pass1.SetVertexBuffer(0, vb, 0)
				pass1.Draw(vertsCount, 0)
				pass1.End()

				pass2 := frame.BeginRenderPass(gpu.RenderPassDesc{
					ColorAttachments: []gpu.ColorAttachment{{
						LoadOp:  gpu.LoadClear,
						StoreOp: gpu.StoreStore,
					}},
				})
				if err := fxaa.Draw(pass2, msaaTarget.OutputTexture()); err != nil {
					t.Fatalf("fxaa.Draw: %v", err)
				}
				pass2.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(aaTestWidth, aaTestHeight), nsgotest.ManualFrames())

	app.Advance(16 * time.Millisecond)
	before, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	if n := countAntiAliasedPixels(before); n < 80 {
		t.Fatalf("before loss anti-aliased pixels = %d, want >= 80", n)
	}

	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
	if msaaTarget.Valid() || fxaa.Valid() {
		t.Fatalf("retained resources still valid during device loss: msaa=%v fxaa=%v", msaaTarget.Valid(), fxaa.Valid())
	}

	app.RestoreGPUDevice("id:viewport")
	if !msaaTarget.Valid() || !fxaa.Valid() {
		t.Fatalf("retained resources not restored: msaa=%v fxaa=%v", msaaTarget.Valid(), fxaa.Valid())
	}

	app.Advance(16 * time.Millisecond)
	after, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	if _, maxDelta := diffMaxChannel(before, after); maxDelta != 0 {
		t.Fatalf("frame after device restore differed by max channel delta %d", maxDelta)
	}
}

func diffMaxChannel(a, b *image.RGBA) (mismatched, maxDelta int) {
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			ca := a.RGBAAt(x, y)
			cb := b.RGBAAt(x, y)
			d := max(aaAbsDiff(ca.R, cb.R), max(aaAbsDiff(ca.G, cb.G), max(aaAbsDiff(ca.B, cb.B), aaAbsDiff(ca.A, cb.A))))
			if d > 0 {
				mismatched++
				if d > maxDelta {
					maxDelta = d
				}
			}
		}
	}
	return mismatched, maxDelta
}

// assertFXAAParity verifies GPU FXAAPass output matches CPU ApplyFXAA closely: at least 99.5% of pixels agree within 2 channel levels, and the rare half-texel edge-search tie pixels (where 1 ULP of fullscreen-triangle UV interpolation flips abs(lumaEnd) >= 0.25*grad) stay within a tight bound.
func assertFXAAParity(t *testing.T, label string, got, ref *image.RGBA) {
	t.Helper()
	b := got.Bounds()
	if !b.Eq(ref.Bounds()) {
		t.Fatalf("%s: bounds %v != %v", label, b, ref.Bounds())
	}
	const channelTol = 2
	far := 0
	maxDelta := 0
	sumDelta := 0
	total := b.Dx() * b.Dy()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g := got.RGBAAt(x, y)
			r := ref.RGBAAt(x, y)
			d := max(aaAbsDiff(g.R, r.R), max(aaAbsDiff(g.G, r.G), max(aaAbsDiff(g.B, r.B), aaAbsDiff(g.A, r.A))))
			if d > channelTol {
				far++
			}
			if d > maxDelta {
				maxDelta = d
			}
			sumDelta += d
		}
	}
	limit := total / 200
	meanDelta := float64(sumDelta) / float64(total)
	if far > limit || maxDelta > 20 || meanDelta > 0.1 {
		t.Fatalf("%s: GPU vs CPU FXAA mismatch: far=%d (limit %d), maxDelta=%d, meanDelta=%.4f", label, far, limit, maxDelta, meanDelta)
	}
}

func BenchmarkApplyFXAA(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 128, 96))
	for y := range 96 {
		for x := range 128 {
			if x*3+y*2 > 220 && x*2-y*3 < 90 {
				img.SetRGBA(x, y, aaWhiteRGBA)
			} else {
				img.SetRGBA(x, y, aaClearRGBA)
			}
		}
	}
	opts := render3d.DefaultFXAAOptions()
	b.ReportAllocs()
	for b.Loop() {
		_ = render3d.ApplyFXAA(img, opts)
	}
}

func BenchmarkMSAATargetResizeNoop(b *testing.B) {
	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatUndefined)
	defer sr.Close()
	mt, err := render3d.NewMSAATarget(nil, sr.Device(), render3d.MSAATargetDesc{
		Width:       64,
		Height:      64,
		ColorFormat: gpu.FormatRGBA8Unorm,
		DepthFormat: gpu.FormatDepth24Plus,
		SampleCount: render3d.AAMSAA4x.SampleCount(),
	})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = mt.Close() }()
	b.ReportAllocs()
	for b.Loop() {
		if err := mt.Resize(64, 64); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFXAAPassRecordSteadyState(b *testing.B) {
	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()
	srcRT, err := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{
		Width: 32, Height: 32, ColorFormat: gpu.FormatRGBA8Unorm, SampleCount: 1,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = srcRT.Close() }()
	fxaa, err := render3d.NewFXAAPass(nil, dev, render3d.FXAAPassDesc{TargetFormat: gpu.FormatRGBA8Unorm})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = fxaa.Close() }()

	// Warm up cached bind group and uniform buffer.
	enc := dev.BeginEncoder()
	if err := fxaa.Encode(enc, srcRT.Color, nil); err != nil {
		b.Fatal(err)
	}
	_ = enc.Finish()

	b.ReportAllocs()
	for b.Loop() {
		enc := dev.BeginEncoder()
		if err := fxaa.Encode(enc, srcRT.Color, nil); err != nil {
			b.Fatal(err)
		}
		_ = enc.Finish()
	}
}
