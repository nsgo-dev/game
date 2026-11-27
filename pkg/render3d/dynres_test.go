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
	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// dynResQuadSceneWGSL draws a 3D mesh with vertex colors (passed via UV/normal)
// through render3d.CameraWGSL so each quadrant of the 3D view has a distinct color.
const dynResQuadSceneWGSL = render3d.CameraWGSL + `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) color: vec4<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.color = vec4<f32>(in.normal.x, in.normal.y, in.normal.z, 1.0);
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return in.color;
}
`

// dynResHUDWGSL draws clip-space 2D HUD geometry directly onto the native target.
const dynResHUDWGSL = `
struct VertexIn {
	@location(0) pos: vec2<f32>,
	@location(1) color: vec4<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) color: vec4<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = vec4<f32>(in.pos, 0.0, 1.0);
	out.color = in.color;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	return in.color;
}
`

func dynResPackFloats(fs ...float32) []byte {
	out := make([]byte, 4*len(fs))
	for i, f := range fs {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(f))
	}
	return out
}

// dynResFourQuadrantMesh builds four 3D quads on z=0 covering [-1, 1] x [-1, 1]
// under an identity/ortho camera:
//   - top-left  ([-1,0] x [0,1]):   Red   (1, 0, 0)
//   - top-right ([0,1]  x [0,1]):   Green (0, 1, 0)
//   - bot-left  ([-1,0] x [-1,0]):  Blue  (0, 0, 1)
//   - bot-right ([0,1]  x [-1,0]):  Yellow (1, 1, 0)
func dynResFourQuadrantMesh() render3d.Mesh {
	type quad struct {
		x0, y0, x1, y1 float32
		col            math3d.Vec3
	}
	quads := []quad{
		{-1, 0, 0, 1, math3d.Vec3{X: 1, Y: 0, Z: 0}},  // top-left: red
		{0, 0, 1, 1, math3d.Vec3{X: 0, Y: 1, Z: 0}},   // top-right: green
		{-1, -1, 0, 0, math3d.Vec3{X: 0, Y: 0, Z: 1}}, // bottom-left: blue
		{0, -1, 1, 0, math3d.Vec3{X: 1, Y: 1, Z: 0}},  // bottom-right: yellow
	}
	var m render3d.Mesh
	for _, q := range quads {
		base := uint32(len(m.Positions))
		m.Positions = append(m.Positions,
			math3d.Vec3{X: q.x0, Y: q.y0, Z: 0},
			math3d.Vec3{X: q.x1, Y: q.y0, Z: 0},
			math3d.Vec3{X: q.x1, Y: q.y1, Z: 0},
			math3d.Vec3{X: q.x0, Y: q.y1, Z: 0},
		)
		for range 4 {
			m.Normals = append(m.Normals, q.col)
			m.UVs = append(m.UVs, math3d.Vec2{})
		}
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	return m
}

// dynResPixelRectVerts builds 2 triangles covering exact native pixel rectangle
// [px0, px1) x [py0, py1) in top-left native pixel coordinates.
func dynResPixelRectVerts(nativeW, nativeH int, px0, py0, px1, py1 float32, r, g, b, a float32) []float32 {
	x0 := (px0/float32(nativeW))*2 - 1
	x1 := (px1/float32(nativeW))*2 - 1
	y0 := 1 - (py0/float32(nativeH))*2 // top
	y1 := 1 - (py1/float32(nativeH))*2 // bottom
	return []float32{
		x0, y1, r, g, b, a,
		x1, y1, r, g, b, a,
		x1, y0, r, g, b, a,
		x0, y1, r, g, b, a,
		x1, y0, r, g, b, a,
		x0, y0, r, g, b, a,
	}
}

func dynResAssertColorNear(t *testing.T, img *image.RGBA, x, y int, want color.RGBA, tol int, label string) {
	t.Helper()
	got := img.RGBAAt(x, y)
	diff := func(a, b uint8) int {
		d := int(a) - int(b)
		if d < 0 {
			return -d
		}
		return d
	}
	if diff(got.R, want.R) > tol || diff(got.G, want.G) > tol || diff(got.B, want.B) > tol || diff(got.A, want.A) > tol {
		t.Errorf("%s at (%d,%d) = %+v, want %+v (±%d)", label, x, y, got, want, tol)
	}
}

func TestDynamicResolutionConfigValidate(t *testing.T) {
	if err := render3d.DefaultDynamicResolutionConfig().Validate(); err != nil {
		t.Fatalf("DefaultDynamicResolutionConfig().Validate() = %v", err)
	}
	if err := (render3d.DynamicResolutionConfig{}).Validate(); err != nil {
		t.Fatalf("zero DynamicResolutionConfig.Validate() = %v", err)
	}

	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	invalid := []struct {
		name string
		cfg  render3d.DynamicResolutionConfig
	}{
		{"negative target", render3d.DynamicResolutionConfig{TargetFrameTime: -time.Millisecond}},
		{"min > max", render3d.DynamicResolutionConfig{MinScale: 0.9, MaxScale: 0.7}},
		{"min > 1", render3d.DynamicResolutionConfig{MinScale: 1.2}},
		{"max > 1", render3d.DynamicResolutionConfig{MaxScale: 1.5}},
		{"initial below min", render3d.DynamicResolutionConfig{MinScale: 0.6, MaxScale: 1.0, InitialScale: 0.5}},
		{"initial above max", render3d.DynamicResolutionConfig{MinScale: 0.5, MaxScale: 0.8, InitialScale: 0.9}},
		{"deadband >= 0.5", render3d.DynamicResolutionConfig{Deadband: 0.5}},
		{"deadband negative", render3d.DynamicResolutionConfig{Deadband: -0.05}},
		{"smoothing > 1", render3d.DynamicResolutionConfig{SmoothingAlpha: 1.5}},
		{"step down > 1", render3d.DynamicResolutionConfig{StepDown: 1.1}},
		{"step up > 1", render3d.DynamicResolutionConfig{StepUp: 1.1}},
		{"negative cooldown down", render3d.DynamicResolutionConfig{CooldownDown: -1}},
		{"negative cooldown up", render3d.DynamicResolutionConfig{CooldownUp: -1}},
		{"negative alignment", render3d.DynamicResolutionConfig{Alignment: -2}},
		{"sharpness > 1", render3d.DynamicResolutionConfig{Sharpness: 1.5}},
		{"unknown filter", render3d.DynamicResolutionConfig{Filter: render3d.DynamicResolutionFilter(99)}},
		{"hitch <= target", render3d.DynamicResolutionConfig{TargetFrameTime: 16 * time.Millisecond, HitchLimit: 10 * time.Millisecond}},
		{"nan min", render3d.DynamicResolutionConfig{MinScale: nan}},
		{"inf max", render3d.DynamicResolutionConfig{MaxScale: inf}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if !errors.Is(err, render3d.ErrInvalidDynamicResolution) {
				t.Fatalf("Validate() = %v, want error wrapping ErrInvalidDynamicResolution", err)
			}
			if _, err2 := render3d.NewDynamicResolutionScaler(tc.cfg); !errors.Is(err2, render3d.ErrInvalidDynamicResolution) {
				t.Fatalf("NewDynamicResolutionScaler() = %v, want error wrapping ErrInvalidDynamicResolution", err2)
			}
		})
	}
}

func TestDynamicResolutionScalerConvergence(t *testing.T) {
	cfg := render3d.DefaultDynamicResolutionConfig()
	scaler, err := render3d.NewDynamicResolutionScaler(cfg)
	if err != nil {
		t.Fatalf("NewDynamicResolutionScaler: %v", err)
	}

	target := cfg.TargetFrameTime // ~16.67ms
	// Workload model: GPU frame time = base + fill * scale^2.
	// At scale=1.0: 4ms + 28ms = 32ms (nearly 2x over budget).
	// Exact equilibrium: 4 + 28*s^2 = 16.67 => s ≈ 0.673.
	simFrameTime := func(baseMs, fillMs float64, scale float32) time.Duration {
		ms := baseMs + fillMs*float64(scale)*float64(scale)
		return time.Duration(math.Round(ms * float64(time.Millisecond)))
	}

	// Phase 1: converge from 1.0 under heavy fill load.
	for range 80 {
		ft := simFrameTime(4.0, 28.0, scaler.Scale())
		scaler.RecordFrameTime(ft)
	}
	settledScale := scaler.Scale()
	settledFT := simFrameTime(4.0, 28.0, settledScale)
	tol := time.Duration(float64(target) * float64(cfg.Deadband+0.02))
	if diff := settledFT - target; diff > tol || diff < -tol {
		t.Fatalf("did not converge to target: scale=%.4f frameTime=%v target=%v (tol ±%v)", settledScale, settledFT, target, tol)
	}

	// Verify stability (no thrashing/oscillation) over 40 steady frames.
	beforeDowns := scaler.Stats().ScaleDowns
	beforeUps := scaler.Stats().ScaleUps
	for range 40 {
		scaler.RecordFrameTime(simFrameTime(4.0, 28.0, scaler.Scale()))
	}
	st := scaler.Stats()
	if st.ScaleDowns != beforeDowns || st.ScaleUps != beforeUps {
		t.Fatalf("scaler oscillated at equilibrium: downs %d->%d ups %d->%d", beforeDowns, st.ScaleDowns, beforeUps, st.ScaleUps)
	}

	// Phase 2: inject a sustained load spike -> scale must drop further toward MinScale.
	for range 40 {
		scaler.RecordFrameTime(simFrameTime(5.0, 55.0, scaler.Scale()))
	}
	if got := scaler.Scale(); got >= settledScale-0.05 {
		t.Fatalf("scale did not drop under spike: got %.4f, previously %.4f", got, settledScale)
	}
	if got := scaler.Scale(); got < cfg.MinScale {
		t.Fatalf("scale %.4f dropped below MinScale %.4f", got, cfg.MinScale)
	}

	// Phase 3: load drops well below budget -> scale recovers smoothly to MaxScale.
	for range 250 {
		scaler.RecordFrameTime(simFrameTime(2.0, 8.0, scaler.Scale()))
	}
	if got := scaler.Scale(); math.Abs(float64(got-cfg.MaxScale)) > 1e-4 {
		t.Fatalf("scale did not recover to MaxScale %.2f: got %.4f", cfg.MaxScale, got)
	}
	if scaler.Stats().ScaleUps == 0 || scaler.Stats().ScaleDowns == 0 {
		t.Fatalf("expected both ScaleDowns and ScaleUps > 0, got %+v", scaler.Stats())
	}
}

func TestDynamicResolutionScalerHitchesControlsAndPassTimings(t *testing.T) {
	scaler, err := render3d.NewDynamicResolutionScaler(render3d.DynamicResolutionConfig{
		TargetFrameTime: 16 * time.Millisecond,
		MinScale:        0.5,
		MaxScale:        1.0,
		HitchLimit:      200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDynamicResolutionScaler: %v", err)
	}

	// Hitch > HitchLimit must be ignored without lowering scale or corrupting EMA.
	if got := scaler.RecordFrameTime(500 * time.Millisecond); got != 1.0 {
		t.Fatalf("hitch changed scale to %v, want 1.0", got)
	}
	if st := scaler.Stats(); st.HitchesIgnored != 1 || st.FramesRecorded != 0 || st.SmoothedFrameTime != 0 {
		t.Fatalf("unexpected stats after hitch: %+v", st)
	}

	// Non-positive durations are ignored cleanly.
	if got := scaler.RecordFrameTime(0); got != 1.0 {
		t.Fatalf("zero frame time changed scale to %v", got)
	}
	if got := scaler.RecordFrameTime(-5 * time.Millisecond); got != 1.0 {
		t.Fatalf("negative frame time changed scale to %v", got)
	}

	// RecordPassTimings filters by label and adapts when overloaded.
	for range 12 {
		scaler.RecordPassTimings([]gpu.PassTiming{
			{Label: "scene-3d", Duration: 28 * time.Millisecond},
			{Label: "ui-debug", Duration: 2 * time.Millisecond},
		}, "scene-3d")
	}
	if got := scaler.Scale(); got >= 0.95 {
		t.Fatalf("RecordPassTimings did not lower scale under 28ms 3D pass: got %.4f", got)
	}

	// Fixed scale override pins output and suppresses adaptation.
	scaler.SetFixedScale(0.75)
	if got := scaler.Scale(); got != 0.75 {
		t.Fatalf("SetFixedScale(0.75) => %v", got)
	}
	if !scaler.Stats().Fixed {
		t.Fatal("Stats().Fixed = false after SetFixedScale")
	}
	scaler.RecordFrameTime(40 * time.Millisecond)
	if got := scaler.Scale(); got != 0.75 {
		t.Fatalf("fixed scale changed after overload: got %v", got)
	}
	scaler.ClearFixedScale()

	// SetEnabled(false) returns native 1.0.
	scaler.SetEnabled(false)
	if got := scaler.Scale(); got != 1.0 {
		t.Fatalf("disabled Scale() = %v, want 1.0", got)
	}
	scaler.SetEnabled(true)

	// SetScaleBounds clamps active scale cleanly (e.g. pairing with quality.Preset).
	if err := scaler.SetScaleBounds(0.5, 0.75); err != nil {
		t.Fatalf("SetScaleBounds: %v", err)
	}
	if got := scaler.Scale(); got > 0.75 {
		t.Fatalf("scale %v exceeded updated max bound 0.75", got)
	}
	if err := scaler.SetScaleBounds(0.8, 0.6); !errors.Is(err, render3d.ErrInvalidDynamicResolution) {
		t.Fatalf("invalid SetScaleBounds error = %v, want ErrInvalidDynamicResolution", err)
	}

	// Reset restores initial scale (clamped to current bounds) and zeroes counters.
	scaler.Reset()
	st := scaler.Stats()
	if st.FramesRecorded != 0 || st.HitchesIgnored != 0 || st.SmoothedFrameTime != 0 || st.Scale != 0.75 {
		t.Fatalf("unexpected stats after Reset: %+v", st)
	}
}

func TestDynamicResolutionScaledSize(t *testing.T) {
	scaler, err := render3d.NewDynamicResolutionScaler(render3d.DynamicResolutionConfig{
		MinScale:  0.25,
		MaxScale:  1.0,
		Alignment: 2,
	})
	if err != nil {
		t.Fatalf("NewDynamicResolutionScaler: %v", err)
	}

	// At scale 1.0, odd native sizes are preserved verbatim.
	if w, h := scaler.ScaledSize(127, 95); w != 127 || h != 95 {
		t.Fatalf("ScaledSize(127, 95) at 1.0 = (%d, %d), want (127, 95)", w, h)
	}
	if w, h := scaler.ScaledSize(0, 100); w != 0 || h != 0 {
		t.Fatalf("ScaledSize(0, 100) = (%d, %d), want (0, 0)", w, h)
	}

	// At scale 0.5 with even alignment.
	scaler.SetFixedScale(0.5)
	if w, h := scaler.ScaledSize(128, 72); w != 64 || h != 36 {
		t.Fatalf("ScaledSize(128, 72) at 0.5 = (%d, %d), want (64, 36)", w, h)
	}
	// Odd scaled value rounds to even multiple.
	if w, h := scaler.ScaledSize(126, 70); w%2 != 0 || h%2 != 0 {
		t.Fatalf("ScaledSize(126, 70) at 0.5 = (%d, %d), want even dimensions", w, h)
	}
	// Tiny native dimension clamps cleanly to >= 1 and <= native.
	if w, h := scaler.ScaledSize(1, 1); w != 1 || h != 1 {
		t.Fatalf("ScaledSize(1, 1) at 0.5 = (%d, %d), want (1, 1)", w, h)
	}
}

func TestDynamicResolutionTargetGPUUpscaleAndNativeHUD(t *testing.T) {
	const nativeW, nativeH = 128, 128

	sr := gpu.NewSoftwareRenderer(nativeW, nativeH, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	// Orthographic camera mapping [-1,1]x[-1,1] at z=0 directly to clip space.
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 2}, 2, 1, 0.1, 10)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	sceneMesh, err := render3d.UploadMesh(nil, dev, dynResFourQuadrantMesh(), render3d.MeshUpload{Label: "dynres-quads"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = sceneMesh.Close() })

	sceneMod, err := gpu.CompileWGSL(dynResQuadSceneWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL scene: %v", err)
	}
	scenePipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Label:         "dynres-scene",
		Vertex:        gpu.ShaderStage{Module: sceneMod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: sceneMod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{camBuf.Layout()},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
		DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
		Targets:       []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm, WriteMask: gpu.ColorWriteAll}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline scene: %v", err)
	}
	t.Cleanup(func() { _ = scenePipe.Close() })

	hudMod, err := gpu.CompileWGSL(dynResHUDWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL hud: %v", err)
	}
	hudPipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Label:    "dynres-native-hud",
		Vertex:   gpu.ShaderStage{Module: hudMod, EntryPoint: "vs_main"},
		Fragment: gpu.ShaderStage{Module: hudMod, EntryPoint: "fs_main"},
		VertexBuffers: []gpu.VertexBufferLayout{{
			Stride: 6 * 4,
			Attributes: []gpu.VertexAttribute{
				{Location: 0, Offset: 0, Format: gpu.VertexFloat32x2},
				{Location: 1, Offset: 2 * 4, Format: gpu.VertexFloat32x4},
			},
		}},
		Primitive: gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
		Targets:   []gpu.ColorTargetState{{Format: dev.SurfaceFormat(), WriteMask: gpu.ColorWriteAll}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline hud: %v", err)
	}
	t.Cleanup(func() { _ = hudPipe.Close() })

	// 1px-wide crisp white vertical crosshair at native x=20 (inside the red/blue left half,
	// well away from the quadrant seam) and a 1px-tall crisp cyan horizontal HUD bar at native y=10.
	var hudVerts []float32
	hudVerts = append(hudVerts, dynResPixelRectVerts(nativeW, nativeH, 20, 0, 21, float32(nativeH), 1, 1, 1, 1)...)
	hudVerts = append(hudVerts, dynResPixelRectVerts(nativeW, nativeH, 0, 10, float32(nativeW), 11, 0, 1, 1, 1)...)
	hudVB, err := dev.CreateBuffer(nil, gpu.BufferDesc{
		Label: "dynres-hud-vb",
		Usage: gpu.UsageVertex,
		Data:  dynResPackFloats(hudVerts...),
	})
	if err != nil {
		t.Fatalf("CreateBuffer hudVB: %v", err)
	}
	t.Cleanup(func() { _ = hudVB.Close() })

	target, err := render3d.NewDynamicResolutionTarget(nil, dev, render3d.DynamicResolutionTargetOptions{
		Label:       "test.dynres",
		TimingLabel: "scene-3d",
		Config: render3d.DynamicResolutionConfig{
			MinScale:     0.5,
			MaxScale:     1.0,
			InitialScale: 0.5, // render 3D at 64x64 inside 128x128 backing texture
			Filter:       render3d.DynamicResolutionBilinear,
		},
	})
	if err != nil {
		t.Fatalf("NewDynamicResolutionTarget: %v", err)
	}
	t.Cleanup(func() { _ = target.Close() })

	renderFrame := func(filter render3d.DynamicResolutionFilter, useSinglePassHUD bool) *image.RGBA {
		target.SetFilter(filter)
		enc := dev.BeginEncoder()

		// Pass 1: 3D scene rendered at 0.5x scale (64x64 sub-viewport).
		pass3d, err := target.Begin3D(enc, nativeW, nativeH, core.RGB(0x000000))
		if err != nil {
			t.Fatalf("Begin3D: %v", err)
		}
		pass3d.SetPipeline(scenePipe)
		pass3d.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		sceneMesh.Draw(pass3d)
		pass3d.End()

		if useSinglePassHUD {
			// Upscale + native HUD inside one native color render pass.
			passNative := enc.BeginRenderPass(gpu.RenderPassDesc{
				Label:            "upscale-and-hud",
				ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}},
			})
			if err := target.UpscaleIntoPass(passNative, nativeW, nativeH); err != nil {
				t.Fatalf("UpscaleIntoPass: %v", err)
			}
			passNative.SetPipeline(hudPipe)
			passNative.SetVertexBuffer(0, hudVB, 0)
			passNative.Draw(12, 0)
			passNative.End()
		} else {
			// Pass 2: Upscale scaled 3D target to native 128x128 surface.
			if err := target.Upscale(enc, nil, nativeW, nativeH); err != nil {
				t.Fatalf("Upscale: %v", err)
			}
			// Pass 3: Native-resolution 1px HUD composited over the upscaled 3D scene.
			passHUD := enc.BeginRenderPass(gpu.RenderPassDesc{
				Label:            "native-hud",
				ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadLoad, StoreOp: gpu.StoreStore}},
			})
			passHUD.SetViewport(0, 0, nativeW, nativeH, 0, 1)
			passHUD.SetScissor(0, 0, nativeW, nativeH)
			passHUD.SetPipeline(hudPipe)
			passHUD.SetVertexBuffer(0, hudVB, 0)
			passHUD.Draw(12, 0)
			passHUD.End()
		}

		dev.Submit(enc.Finish())
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer error: %v", err)
		}
		return sr.Image()
	}

	red := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	green := color.RGBA{R: 0, G: 255, B: 0, A: 255}
	blue := color.RGBA{R: 0, G: 0, B: 255, A: 255}
	yellow := color.RGBA{R: 255, G: 255, B: 0, A: 255}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	cyan := color.RGBA{R: 0, G: 255, B: 255, A: 255}

	for _, tc := range []struct {
		name          string
		filter        render3d.DynamicResolutionFilter
		singlePassHUD bool
	}{
		{"bilinear-separate-hud", render3d.DynamicResolutionBilinear, false},
		{"nearest-separate-hud", render3d.DynamicResolutionNearest, false},
		{"sharpen-separate-hud", render3d.DynamicResolutionSharpen, false},
		{"bilinear-single-pass-hud", render3d.DynamicResolutionBilinear, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := renderFrame(tc.filter, tc.singlePassHUD)

			if sw, sh := target.ScaledSize(); sw != 64 || sh != 64 {
				t.Fatalf("ScaledSize = (%d,%d), want (64,64)", sw, sh)
			}
			if aw, ah := target.AllocatedSize(); aw != nativeW || ah != nativeH {
				t.Fatalf("AllocatedSize = (%d,%d), want (%d,%d)", aw, ah, nativeW, nativeH)
			}

			// Verify all 4 quadrants AND extreme outer edge pixels (0,0), (127,0), (0,127), (127,127):
			// proving full viewport coverage with ZERO UV flip and ZERO black/stale border bleed!
			dynResAssertColorNear(t, img, 0, 0, red, 3, "top-left corner")
			dynResAssertColorNear(t, img, 32, 32, red, 3, "top-left interior")
			dynResAssertColorNear(t, img, nativeW-1, 0, green, 3, "top-right corner")
			dynResAssertColorNear(t, img, 96, 32, green, 3, "top-right interior")
			dynResAssertColorNear(t, img, 0, nativeH-1, blue, 3, "bottom-left corner")
			dynResAssertColorNear(t, img, 32, 96, blue, 3, "bottom-left interior")
			dynResAssertColorNear(t, img, nativeW-1, nativeH-1, yellow, 3, "bottom-right corner")
			dynResAssertColorNear(t, img, 96, 96, yellow, 3, "bottom-right interior")

			// Verify 1px native HUD crosshair at x=20 stays razor-sharp at 1:1 native pixels:
			// x=20 is pure white; immediate 1px neighbors x=19 and x=21 are untouched 3D red (y=32) and blue (y=96).
			dynResAssertColorNear(t, img, 20, 32, white, 0, "1px vertical HUD line over red")
			dynResAssertColorNear(t, img, 19, 32, red, 3, "left neighbor of 1px HUD line")
			dynResAssertColorNear(t, img, 21, 32, red, 3, "right neighbor of 1px HUD line")

			dynResAssertColorNear(t, img, 20, 96, white, 0, "1px vertical HUD line over blue")
			dynResAssertColorNear(t, img, 19, 96, blue, 3, "left neighbor over blue")
			dynResAssertColorNear(t, img, 21, 96, blue, 3, "right neighbor over blue")

			// Verify 1px native horizontal HUD bar at y=10 stays razor-sharp at 1:1 native pixels.
			dynResAssertColorNear(t, img, 96, 10, cyan, 0, "1px horizontal HUD bar over green")
			dynResAssertColorNear(t, img, 96, 9, green, 3, "above 1px HUD bar")
			dynResAssertColorNear(t, img, 96, 11, green, 3, "below 1px HUD bar")
		})
	}
}

func TestDynamicResolutionTargetWithOverlay3DNativeHUD(t *testing.T) {
	// Verify DynamicResolutionTarget integrates directly with gpu.View (*gpu.Frame)
	// and scene.Overlay3D (native-resolution 2D HUD canvas over scaled 3D view)
	// across both full capabilities and the WebGL 2 / GLES 3.0 profile (NoGPUCompute).
	for _, tc := range []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"no-compute-webgl2", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				target      *render3d.DynamicResolutionTarget
				camBuf      *render3d.CameraBuffer
				mesh        *render3d.GPUMesh
				pipe        *gpu.Pipeline
				framesDrawn int
				hudSize     core.Size
			)
			cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 2}, 2, 1, 0.1, 10)
			cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())

			overlay := scene.NewOverlay3D(nil)
			overlay.TestID = "native-hud-overlay"
			overlay.OnDraw = func(d *core.Drawing, size core.Size) {
				hudSize = size
				// Draw a 1pt native HUD bar across the top of the native overlay canvas.
				d.SetFillColor(core.RGB(0xffffff))
				d.FillRect(0, 0, float32(size.Width), 1)
			}

			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return overlay.View(cx, gpu.ViewProps{
					TestID:      "dynres-gpu-view",
					DepthFormat: gpu.FormatDepth24Plus,
					Style:       core.Style{Width: core.Pt(128), Height: core.Pt(96)},
					OnReady: func(dev *gpu.Device) {
						var err error
						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
						mesh, err = render3d.UploadMesh(cx, dev, dynResFourQuadrantMesh(), render3d.MeshUpload{
							Label:    "overlay-quads",
							Retained: true,
						})
						if err != nil {
							t.Fatalf("UploadMesh: %v", err)
						}
						mod, err := gpu.CompileWGSL(dynResQuadSceneWGSL)
						if err != nil {
							t.Fatalf("CompileWGSL: %v", err)
						}
						pipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
							Label:         "overlay-scene",
							Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
							Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
							Layouts:       []*gpu.BindGroupLayout{camBuf.Layout()},
							VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
							Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
							DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
							Targets:       []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm, WriteMask: gpu.ColorWriteAll}},
						})
						if err != nil {
							t.Fatalf("CreatePipeline: %v", err)
						}
						target, err = render3d.NewDynamicResolutionTarget(cx, dev, render3d.DynamicResolutionTargetOptions{
							TimingLabel: "world-3d",
							Config: render3d.DynamicResolutionConfig{
								MinScale:     0.5,
								MaxScale:     1.0,
								InitialScale: 0.5,
							},
						})
						if err != nil {
							t.Fatalf("NewDynamicResolutionTarget: %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						w, h := frame.Size()
						if w <= 0 || h <= 0 {
							return
						}
						camBuf.WriteCamera(cam)
						pass3d, err := target.Begin3D(frame, w, h, core.RGB(0x000000))
						if err != nil {
							t.Fatalf("Begin3D: %v", err)
						}
						pass3d.SetPipeline(pipe)
						pass3d.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
						mesh.Draw(pass3d)
						pass3d.End()

						if err := target.Upscale(frame, nil, w, h); err != nil {
							t.Fatalf("Upscale: %v", err)
						}
						frame.Submit()
						target.RecordPassTimings(frame.PassTimings())
						framesDrawn++
					},
				})
			}, append([]nsgotest.Option{nsgotest.Size(128, 96)}, tc.opts...)...)

			app.Advance(34 * time.Millisecond)

			if framesDrawn == 0 {
				t.Fatal("expected at least one GPU frame to render")
			}
			nw, nh := target.NativeSize()
			sw, sh := target.ScaledSize()
			if sw >= nw || sh >= nh {
				t.Fatalf("expected 3D pass to run at scaled resolution < native (%dx%d), got %dx%d", nw, nh, sw, sh)
			}
			// Overlay3D HUD canvas stays at full native size (128x96) regardless of 0.5x 3D scale.
			if hudSize.Width != core.Points(128) || hudSize.Height != core.Points(96) {
				t.Fatalf("Overlay3D HUD canvas size = %v, want 128x96 native", hudSize)
			}
		})
	}
}

func TestDynamicResolutionTargetResizeAndScaleChurn(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(128, 128, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	target, err := render3d.NewDynamicResolutionTarget(nil, dev, render3d.DynamicResolutionTargetOptions{
		Config: render3d.DynamicResolutionConfig{
			MinScale: 0.25,
			MaxScale: 1.0,
		},
	})
	if err != nil {
		t.Fatalf("NewDynamicResolutionTarget: %v", err)
	}

	// First Begin3D allocates backing offscreen texture at native 128x128.
	enc := dev.BeginEncoder()
	p, err := target.Begin3D(enc, 128, 128, core.RGB(0x102030))
	if err != nil {
		t.Fatalf("Begin3D initial: %v", err)
	}
	p.End()
	if err := target.Upscale(enc, nil, 128, 128); err != nil {
		t.Fatalf("Upscale initial: %v", err)
	}
	dev.Submit(enc.Finish())

	initialTexID := target.ColorTexture().ID()
	if initialTexID == 0 {
		t.Fatal("expected non-zero offscreen color texture ID")
	}
	if wantBytes := uint64(128 * 128 * (4 + 4)); target.ByteSize() != wantBytes {
		t.Fatalf("ByteSize() = %d, want %d", target.ByteSize(), wantBytes)
	}

	// Rapid scale churn at constant native size must NOT reallocate the offscreen texture.
	for _, sc := range []float32{0.9, 0.75, 0.62, 0.5, 0.35, 0.5, 0.8, 1.0} {
		target.Scaler().SetFixedScale(sc)
		enc := dev.BeginEncoder()
		p, err := target.Begin3D(enc, 128, 128, core.RGB(0x102030))
		if err != nil {
			t.Fatalf("Begin3D scale=%.2f: %v", sc, err)
		}
		p.End()
		if err := target.Upscale(enc, nil, 128, 128); err != nil {
			t.Fatalf("Upscale scale=%.2f: %v", sc, err)
		}
		dev.Submit(enc.Finish())
		if gotID := target.ColorTexture().ID(); gotID != initialTexID {
			t.Fatalf("offscreen texture reallocated on scale=%.2f (id %d != %d)", sc, gotID, initialTexID)
		}
	}

	// Growing native size reallocates backing target to fit.
	if err := target.EnsureCapacity(160, 120); err != nil {
		t.Fatalf("EnsureCapacity grow: %v", err)
	}
	if aw, ah := target.AllocatedSize(); aw != 160 || ah != 120 {
		t.Fatalf("AllocatedSize after grow = (%d,%d), want (160,120)", aw, ah)
	}
	grownTexID := target.ColorTexture().ID()
	if grownTexID == initialTexID {
		t.Fatal("expected new texture allocation on grow")
	}

	// Modest shrink (< 2x) reuses backing texture without thrashing.
	if err := target.EnsureCapacity(140, 110); err != nil {
		t.Fatalf("EnsureCapacity small shrink: %v", err)
	}
	if target.ColorTexture().ID() != grownTexID {
		t.Fatal("small shrink unexpectedly reallocated backing texture")
	}

	// Large shrink (> 2x) releases oversized VRAM and reallocates smaller target.
	if err := target.EnsureCapacity(64, 48); err != nil {
		t.Fatalf("EnsureCapacity large shrink: %v", err)
	}
	if aw, ah := target.AllocatedSize(); aw != 64 || ah != 48 {
		t.Fatalf("AllocatedSize after large shrink = (%d,%d), want (64,48)", aw, ah)
	}
	if target.ColorTexture().ID() == grownTexID {
		t.Fatal("large shrink did not reallocate smaller backing texture")
	}

	// Close is idempotent and marks target invalid.
	if err := target.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := target.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if target.Valid() {
		t.Fatal("Valid() = true after Close()")
	}
	if target.ByteSize() != 0 {
		t.Fatalf("ByteSize() = %d after Close(), want 0", target.ByteSize())
	}
	if _, err := target.Begin3D(dev.BeginEncoder(), 64, 64, core.RGB(0)); !errors.Is(err, render3d.ErrInvalidDynamicResolution) {
		t.Fatalf("Begin3D after Close() = %v, want ErrInvalidDynamicResolution", err)
	}
}
