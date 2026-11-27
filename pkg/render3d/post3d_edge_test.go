package render3d_test

import (
	"errors"
	"image"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

const (
	post3dEdgeWidth  = 64
	post3dEdgeHeight = 48
)

// post3dEdgeScene is a small lit scene (floor, matte block, HDR emissive box) drawn into a caller-chosen color+depth target.
type post3dEdgeScene struct {
	sp    *post3dScenePipeline
	vb    *gpu.Buffer
	count uint32
	cam   *scene3d.Camera
}

func newPost3dEdgeScene(t *testing.T, dev *gpu.Device, cam *scene3d.Camera) *post3dEdgeScene {
	t.Helper()
	sp := newPost3dScenePipeline(t, nil, dev, gpu.FormatRGBA16Float, gpu.FormatDepth32Float)
	t.Cleanup(func() { _ = sp.cam.Close() })
	var verts []float32
	verts = post3dAppendQuad(verts,
		math3d.Vec3{X: -6, Y: 0, Z: 6}, math3d.Vec3{X: 6, Y: 0, Z: 6},
		math3d.Vec3{X: 6, Y: 0, Z: -8}, math3d.Vec3{X: -6, Y: 0, Z: -8},
		math3d.Vec3Up(), [3]float32{0.55, 0.57, 0.6}, 0.5)
	verts = post3dAppendBox(verts, math3d.Vec3{X: -0.4, Y: 0.5, Z: -0.2}, math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}, [3]float32{0.75, 0.72, 0.68}, 0.5)
	verts = post3dAppendBox(verts, math3d.Vec3{X: 1.0, Y: 0.4, Z: -0.6}, math3d.Vec3{X: 0.3, Y: 0.4, Z: 0.3}, [3]float32{2.5, 1.4, 0.4}, 1.0)
	vb, err := dev.CreateBuffer(nil, gpu.BufferDesc{Label: "post3d.edge.verts", Usage: gpu.UsageVertex, Data: post3dPackFloats(verts)})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	t.Cleanup(func() { _ = vb.Close() })
	return &post3dEdgeScene{sp: sp, vb: vb, count: uint32(len(verts) / 10), cam: cam}
}

func (s *post3dEdgeScene) draw(enc *gpu.Encoder, color, depth *gpu.Texture) {
	s.sp.cam.WriteCamera(s.cam)
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{Texture: color, LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x0c1018)}},
		DepthStencil:     &gpu.DepthStencilAttachment{Texture: depth, DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreStore, ClearDepth: 1},
	})
	s.sp.draw(pass, s.vb, s.count)
	pass.End()
}

func newPost3dEdgeTarget(t *testing.T, dev *gpu.Device, w, h uint32, color, depth gpu.TextureFormat) *gpu.RenderTarget {
	t.Helper()
	rt, err := dev.CreateRenderTarget(nil, gpu.RenderTargetDesc{Width: w, Height: h, ColorFormat: color, DepthFormat: depth})
	if err != nil {
		t.Fatalf("CreateRenderTarget %s: %v", color, err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

func post3dEdgeSubmit(t *testing.T, sr *gpu.SoftwareRenderer, enc *gpu.Encoder) {
	t.Helper()
	sr.Device().Submit(enc.Finish())
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
}

func post3dEdgeCamera() *scene3d.Camera {
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 1.6, Y: 1.8, Z: 4.2}, math3d.DegToRad(50), float32(post3dEdgeWidth)/float32(post3dEdgeHeight), 0.2, 40)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0.4, Z: -0.4}, math3d.Vec3Up())
	return cam
}

// A caller-owned destination texture is written with the pipeline built for its own format, so a stack whose ColorFormat is RGBA16Float renders into an RGBA8 texture exactly as it renders to an RGBA8 swapchain, and a destination in neither format is refused.
func TestPost3DApplyIntoCallerTexture(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(post3dEdgeWidth, post3dEdgeHeight, gpu.FormatDepth32Float)
	defer sr.Close()
	dev := sr.Device()
	cam := post3dEdgeCamera()
	scene := newPost3dEdgeScene(t, dev, cam)

	stack, err := render3d.NewPostStack3D(nil, dev, render3d.PostStack3DOptions{
		Width:        post3dEdgeWidth,
		Height:       post3dEdgeHeight,
		ColorFormat:  gpu.FormatRGBA16Float,
		OutputFormat: gpu.FormatRGBA8Unorm,
		EnableSSAO:   true,
		SSAO:         render3d.SSAOConfigFromCamera(cam),
		EnableDoF:    true,
		DoF:          render3d.DepthOfFieldConfigFromCamera(cam, 4.5, 2, 3),
		EnableBloom:  true,
		Bloom:        render3d.DefaultBloomConfig(),
	})
	if err != nil {
		t.Fatalf("NewPostStack3D: %v", err)
	}
	defer func() { _ = stack.Close() }()
	sceneRT := stack.SceneTarget()

	applyScene := func(dst *gpu.Texture) error {
		enc := dev.BeginEncoder()
		scene.draw(enc, sceneRT.Color, sceneRT.Depth)
		err := stack.ApplyScene(dev, enc, dst)
		post3dEdgeSubmit(t, sr, enc)
		return err
	}

	if err := applyScene(nil); err != nil {
		t.Fatalf("ApplyScene(swapchain): %v", err)
	}
	swap := sr.Image()

	rgba8 := newPost3dEdgeTarget(t, dev, post3dEdgeWidth, post3dEdgeHeight, gpu.FormatRGBA8Unorm, gpu.FormatUndefined)
	if err := applyScene(rgba8.Color); err != nil {
		t.Fatalf("ApplyScene(RGBA8 texture): %v", err)
	}
	if d := post3dMaxChannelDiff(swap, readTextureRGBA(t, dev, rgba8.Color)); d > 1 {
		t.Errorf("RGBA8 destination differs from the RGBA8 swapchain by %d", d)
	}

	hdr := newPost3dEdgeTarget(t, dev, post3dEdgeWidth, post3dEdgeHeight, gpu.FormatRGBA16Float, gpu.FormatUndefined)
	if err := applyScene(hdr.Color); err != nil {
		t.Errorf("ApplyScene(ColorFormat texture): %v", err)
	}

	bgra := newPost3dEdgeTarget(t, dev, post3dEdgeWidth, post3dEdgeHeight, gpu.FormatBGRA8Unorm, gpu.FormatUndefined)
	if err := applyScene(bgra.Color); !errors.Is(err, render3d.ErrInvalidPostProcess) {
		t.Errorf("ApplyScene(BGRA8 texture) = %v, want ErrInvalidPostProcess", err)
	}

	// Every stage alone, and the passthrough blit, honour the destination format too.
	for _, tc := range []struct {
		name             string
		ssao, dof, bloom bool
	}{
		{"ssao", true, false, false},
		{"dof", false, true, false},
		{"bloom", false, false, true},
		{"blit", false, false, false},
	} {
		if err := stack.SetSSAO(tc.ssao, render3d.SSAOConfig{}); err != nil {
			t.Fatalf("%s: SetSSAO: %v", tc.name, err)
		}
		if err := stack.SetDepthOfField(tc.dof, render3d.DepthOfFieldConfig{}); err != nil {
			t.Fatalf("%s: SetDepthOfField: %v", tc.name, err)
		}
		if err := stack.SetBloom(tc.bloom, render3d.BloomConfig{}); err != nil {
			t.Fatalf("%s: SetBloom: %v", tc.name, err)
		}
		if err := applyScene(nil); err != nil {
			t.Fatalf("%s: ApplyScene(swapchain): %v", tc.name, err)
		}
		want := sr.Image()
		if err := applyScene(rgba8.Color); err != nil {
			t.Fatalf("%s: ApplyScene(RGBA8 texture): %v", tc.name, err)
		}
		if d := post3dMaxChannelDiff(want, readTextureRGBA(t, dev, rgba8.Color)); d > 1 {
			t.Errorf("%s: RGBA8 destination differs from the swapchain by %d", tc.name, d)
		}
		if err := applyScene(bgra.Color); !errors.Is(err, render3d.ErrInvalidPostProcess) {
			t.Errorf("%s: ApplyScene(BGRA8 texture) = %v, want ErrInvalidPostProcess", tc.name, err)
		}
	}

	// The standalone passes pick their pipeline the same way.
	src := newPost3dEdgeTarget(t, dev, post3dEdgeWidth, post3dEdgeHeight, gpu.FormatRGBA16Float, gpu.FormatDepth32Float)
	bloom, err := render3d.NewBloomPass(nil, dev, render3d.BloomPassOptions{Width: post3dEdgeWidth, Height: post3dEdgeHeight, OutputFormat: gpu.FormatRGBA8Unorm, Config: render3d.DefaultBloomConfig()})
	if err != nil {
		t.Fatalf("NewBloomPass: %v", err)
	}
	defer func() { _ = bloom.Close() }()
	ssao, err := render3d.NewSSAOPass(nil, dev, render3d.SSAOPassOptions{Width: post3dEdgeWidth, Height: post3dEdgeHeight, OutputFormat: gpu.FormatRGBA8Unorm, Config: render3d.SSAOConfigFromCamera(cam)})
	if err != nil {
		t.Fatalf("NewSSAOPass: %v", err)
	}
	defer func() { _ = ssao.Close() }()
	dof, err := render3d.NewDepthOfFieldPass(nil, dev, render3d.DepthOfFieldPassOptions{Width: post3dEdgeWidth, Height: post3dEdgeHeight, OutputFormat: gpu.FormatRGBA8Unorm, Config: render3d.DepthOfFieldConfigFromCamera(cam, 4.5, 2, 3)})
	if err != nil {
		t.Fatalf("NewDepthOfFieldPass: %v", err)
	}
	defer func() { _ = dof.Close() }()
	passes := []struct {
		name  string
		apply func(enc *gpu.Encoder, dst *gpu.Texture) error
	}{
		{"bloom", func(enc *gpu.Encoder, dst *gpu.Texture) error { return bloom.Apply(dev, enc, src.Color, dst) }},
		{"ssao", func(enc *gpu.Encoder, dst *gpu.Texture) error { return ssao.Apply(dev, enc, src.Color, src.Depth, dst) }},
		{"dof", func(enc *gpu.Encoder, dst *gpu.Texture) error { return dof.Apply(dev, enc, src.Color, src.Depth, dst) }},
	}
	for _, p := range passes {
		run := func(dst *gpu.Texture) error {
			enc := dev.BeginEncoder()
			scene.draw(enc, src.Color, src.Depth)
			err := p.apply(enc, dst)
			post3dEdgeSubmit(t, sr, enc)
			return err
		}
		if err := run(nil); err != nil {
			t.Fatalf("%s.Apply(swapchain): %v", p.name, err)
		}
		want := sr.Image()
		if err := run(rgba8.Color); err != nil {
			t.Fatalf("%s.Apply(RGBA8 texture): %v", p.name, err)
		}
		if d := post3dMaxChannelDiff(want, readTextureRGBA(t, dev, rgba8.Color)); d > 1 {
			t.Errorf("%s: RGBA8 destination differs from the swapchain by %d", p.name, d)
		}
		if err := run(hdr.Color); err != nil {
			t.Errorf("%s.Apply(ColorFormat texture): %v", p.name, err)
		}
		if err := run(bgra.Color); !errors.Is(err, render3d.ErrInvalidPostProcess) {
			t.Errorf("%s.Apply(BGRA8 texture) = %v, want ErrInvalidPostProcess", p.name, err)
		}
	}
}

// Inputs and destinations whose size differs from the pass, and a destination that is also an input, are refused instead of being processed with texel steps for the wrong size.
func TestPost3DApplyRejectsMismatchedTextures(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(post3dEdgeWidth, post3dEdgeHeight, gpu.FormatDepth32Float)
	defer sr.Close()
	dev := sr.Device()
	cam := post3dEdgeCamera()

	stack, err := render3d.NewPostStack3D(nil, dev, render3d.PostStack3DOptions{
		Width: post3dEdgeWidth, Height: post3dEdgeHeight, OutputFormat: gpu.FormatRGBA8Unorm,
		EnableSSAO: true, SSAO: render3d.SSAOConfigFromCamera(cam),
		EnableBloom: true, Bloom: render3d.DefaultBloomConfig(),
	})
	if err != nil {
		t.Fatalf("NewPostStack3D: %v", err)
	}
	defer func() { _ = stack.Close() }()
	ok := newPost3dEdgeTarget(t, dev, post3dEdgeWidth, post3dEdgeHeight, gpu.FormatRGBA16Float, gpu.FormatDepth32Float)
	big := newPost3dEdgeTarget(t, dev, 2*post3dEdgeWidth, 2*post3dEdgeHeight, gpu.FormatRGBA16Float, gpu.FormatDepth32Float)
	bigOut := newPost3dEdgeTarget(t, dev, 2*post3dEdgeWidth, 2*post3dEdgeHeight, gpu.FormatRGBA8Unorm, gpu.FormatUndefined)

	bloom, err := render3d.NewBloomPass(nil, dev, render3d.BloomPassOptions{Width: post3dEdgeWidth, Height: post3dEdgeHeight, Config: render3d.DefaultBloomConfig()})
	if err != nil {
		t.Fatalf("NewBloomPass: %v", err)
	}
	defer func() { _ = bloom.Close() }()
	ssao, err := render3d.NewSSAOPass(nil, dev, render3d.SSAOPassOptions{Width: post3dEdgeWidth, Height: post3dEdgeHeight, Config: render3d.SSAOConfigFromCamera(cam)})
	if err != nil {
		t.Fatalf("NewSSAOPass: %v", err)
	}
	defer func() { _ = ssao.Close() }()
	dof, err := render3d.NewDepthOfFieldPass(nil, dev, render3d.DepthOfFieldPassOptions{Width: post3dEdgeWidth, Height: post3dEdgeHeight, Config: render3d.DepthOfFieldConfigFromCamera(cam, 4, 2, 3)})
	if err != nil {
		t.Fatalf("NewDepthOfFieldPass: %v", err)
	}
	defer func() { _ = dof.Close() }()

	enc := dev.BeginEncoder()
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"stack big source", stack.Apply(dev, enc, big.Color, ok.Depth, nil)},
		{"stack big depth", stack.Apply(dev, enc, ok.Color, big.Depth, nil)},
		{"stack big destination", stack.Apply(dev, enc, ok.Color, ok.Depth, bigOut.Color)},
		{"stack destination is source", stack.Apply(dev, enc, ok.Color, ok.Depth, ok.Color)},
		{"stack scene into itself", stack.ApplyScene(dev, enc, stack.SceneTarget().Color)},
		{"bloom big source", bloom.Apply(dev, enc, big.Color, nil)},
		{"bloom big destination", bloom.Apply(dev, enc, ok.Color, big.Color)},
		{"bloom destination is source", bloom.Apply(dev, enc, ok.Color, ok.Color)},
		{"ssao big depth", ssao.Apply(dev, enc, ok.Color, big.Depth, nil)},
		{"ssao big source", ssao.Apply(dev, enc, big.Color, ok.Depth, nil)},
		{"ssao compute big depth", func() error { _, err := ssao.ComputeAO(dev, enc, big.Depth); return err }()},
		{"dof big depth", dof.Apply(dev, enc, ok.Color, big.Depth, nil)},
		{"dof destination is source", dof.Apply(dev, enc, ok.Color, ok.Depth, ok.Color)},
	} {
		if !errors.Is(tc.err, render3d.ErrInvalidPostProcess) {
			t.Errorf("%s = %v, want ErrInvalidPostProcess", tc.name, tc.err)
		}
	}
	post3dEdgeSubmit(t, sr, enc)
}

// ColorFormat accepts only filterable four-channel formats: Depth of Field keeps its Circle of Confusion in alpha and every pass filter-samples the intermediate targets.
func TestPost3DColorFormatValidation(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth32Float)
	defer sr.Close()
	dev := sr.Device()
	for _, f := range []gpu.TextureFormat{gpu.FormatR8Unorm, gpu.FormatRG8Unorm, gpu.FormatRGBA32Float, gpu.FormatR32Float, gpu.FormatRG11B10Float, gpu.FormatDepth32Float, gpu.FormatBC1RGBAUnorm} {
		if _, err := render3d.NewPostStack3D(nil, dev, render3d.PostStack3DOptions{Width: 16, Height: 16, ColorFormat: f}); !errors.Is(err, render3d.ErrInvalidPostProcess) {
			t.Errorf("NewPostStack3D(ColorFormat %s) = %v, want ErrInvalidPostProcess", f, err)
		}
		if _, err := render3d.NewBloomPass(nil, dev, render3d.BloomPassOptions{Width: 16, Height: 16, ColorFormat: f}); !errors.Is(err, render3d.ErrInvalidPostProcess) {
			t.Errorf("NewBloomPass(ColorFormat %s) = %v, want ErrInvalidPostProcess", f, err)
		}
	}
	for _, f := range []gpu.TextureFormat{gpu.FormatRGBA16Float, gpu.FormatRGBA8Unorm, gpu.FormatRGBA8UnormSRGB, gpu.FormatBGRA8Unorm} {
		s, err := render3d.NewPostStack3D(nil, dev, render3d.PostStack3DOptions{Width: 16, Height: 16, ColorFormat: f})
		if err != nil {
			t.Errorf("NewPostStack3D(ColorFormat %s): %v", f, err)
			continue
		}
		_ = s.Close()
	}
}

// Turning a stage off with a zero config keeps the stage's current settings, and SetCamera refreshes the projection SSAO and Depth of Field depend on without touching anything else.
func TestPost3DStageTogglesAndSetCamera(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(32, 24, gpu.FormatDepth32Float)
	defer sr.Close()
	dev := sr.Device()
	cam := post3dEdgeCamera()
	ssaoCfg := render3d.SSAOConfigFromCamera(cam)
	ssaoCfg.Radius = 0.9
	dofCfg := render3d.DepthOfFieldConfigFromCamera(cam, 4, 1.5, 3)
	bloomCfg := render3d.DefaultBloomConfig()
	bloomCfg.Intensity = 1.1
	stack, err := render3d.NewPostStack3D(nil, dev, render3d.PostStack3DOptions{
		Width: 32, Height: 24, EnableSSAO: true, SSAO: ssaoCfg, EnableDoF: true, DoF: dofCfg, EnableBloom: true, Bloom: bloomCfg,
	})
	if err != nil {
		t.Fatalf("NewPostStack3D: %v", err)
	}
	defer func() { _ = stack.Close() }()

	if err := stack.SetSSAO(false, render3d.SSAOConfig{}); err != nil {
		t.Errorf("SetSSAO(false, zero): %v", err)
	}
	if err := stack.SetDepthOfField(false, render3d.DepthOfFieldConfig{}); err != nil {
		t.Errorf("SetDepthOfField(false, zero): %v", err)
	}
	if err := stack.SetBloom(false, render3d.BloomConfig{}); err != nil {
		t.Errorf("SetBloom(false, zero): %v", err)
	}
	if got := stack.SSAO().Config().Radius; got != 0.9 {
		t.Errorf("SSAO Radius after toggle = %v, want 0.9", got)
	}
	if got := stack.DepthOfField().Config(); got != dofCfg {
		t.Errorf("DoF config after toggle = %+v, want %+v", got, dofCfg)
	}
	if got := stack.Bloom().Config().Intensity; got != 1.1 {
		t.Errorf("Bloom Intensity after toggle = %v, want 1.1", got)
	}
	if err := stack.SetSSAO(true, render3d.SSAOConfig{Radius: -1}); !errors.Is(err, render3d.ErrInvalidPostProcess) {
		t.Errorf("SetSSAO(invalid) = %v, want ErrInvalidPostProcess", err)
	}

	// A resize that changes the aspect ratio and a zoom change the projection; SetCamera picks both up.
	cam.SetViewport(48, 24)
	cam.Projection.FOVY = math3d.DegToRad(35)
	cam.Projection.Far = 0 // Matrix uses Near+1000.
	if err := stack.SetCamera(cam); err != nil {
		t.Fatalf("SetCamera: %v", err)
	}
	if got := stack.SSAO().Config(); got.ProjMatrix != cam.ProjMatrix() || got.Radius != 0.9 {
		t.Errorf("SSAO after SetCamera: ProjMatrix %v Radius %v, want %v and 0.9", got.ProjMatrix, got.Radius, cam.ProjMatrix())
	}
	dofGot := stack.DepthOfField().Config()
	if !post3dNear(dofGot.Near, 0.2, 1e-4) || !post3dNear(dofGot.Far, 1000.2, 0.5) || dofGot.Orthographic || dofGot.FocusDistance != 4 {
		t.Errorf("DoF after SetCamera = %+v, want Near 0.2, Far 1000.2, perspective, focus 4", dofGot)
	}
	if err := stack.SetCamera(nil); !errors.Is(err, render3d.ErrInvalidPostProcess) {
		t.Errorf("SetCamera(nil) = %v, want ErrInvalidPostProcess", err)
	}
}

func post3dNear(a, b, tol float32) bool {
	return float32(math.Abs(float64(a-b))) <= tol
}

// DepthOfFieldConfigFromCamera reads the clip planes from the projection matrix, so defaults the projection fills in and an orthographic camera whose near plane is behind it both linearize depth the way the depth buffer was written.
func TestPost3DDepthOfFieldCameraClipPlanes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		proj      scene3d.Projection
		near, far float32
		ortho     bool
	}{
		{"perspective far defaulted", scene3d.PerspectiveProjection(math3d.DegToRad(60), 1, 0.5, 0), 0.5, 1000.5, false},
		{"perspective near defaulted", scene3d.PerspectiveProjection(math3d.DegToRad(60), 1, 0, 50), 0.1, 50, false},
		{"orthographic both zero", scene3d.OrthographicProjection(4, 1, 0, 0), 0.1, 1000, true},
		{"orthographic negative near", scene3d.OrthographicProjection(4, 1, -2, 30), -2, 30, true},
	} {
		cam := &scene3d.Camera{Rot: math3d.QuatIdentity(), Projection: tc.proj}
		cfg := render3d.DepthOfFieldConfigFromCamera(cam, 3, 1, 4)
		tol := float32(1e-3) * max(1, float32(math.Abs(float64(tc.far))))
		if !post3dNear(cfg.Near, tc.near, 1e-3) || !post3dNear(cfg.Far, tc.far, tol) || cfg.Orthographic != tc.ortho {
			t.Errorf("%s: Near %v Far %v Orthographic %v, want %v %v %v", tc.name, cfg.Near, cfg.Far, cfg.Orthographic, tc.near, tc.far, tc.ortho)
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("%s: Validate: %v", tc.name, err)
		}
	}
	persp := render3d.DefaultDepthOfFieldConfig()
	persp.Near = -1
	if err := persp.Validate(); !errors.Is(err, render3d.ErrInvalidPostProcess) {
		t.Errorf("perspective Near -1 Validate = %v, want ErrInvalidPostProcess", err)
	}
}

// An orthographic Depth of Field keeps the panel at the focus distance sharp and blurs the one behind it, with a near plane behind the camera that the old Near > 0 clamp would have replaced.
func TestPost3DDepthOfFieldOrthographic(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(post3dEdgeWidth, post3dEdgeHeight, gpu.FormatDepth32Float)
	defer sr.Close()
	dev := sr.Device()
	sp := newPost3dScenePipeline(t, nil, dev, gpu.FormatRGBA16Float, gpu.FormatDepth32Float)
	defer func() { _ = sp.cam.Close() }()
	cam := &scene3d.Camera{Rot: math3d.QuatIdentity(), Projection: scene3d.OrthographicProjection(4, float32(post3dEdgeWidth)/float32(post3dEdgeHeight), -2, 30)}

	var verts []float32
	verts = post3dAppendCheckerPanel(verts, math3d.Vec3{X: -1.3, Z: -3}, 2.2, 3, 6, 8, [3]float32{0.95, 0.2, 0.2}, [3]float32{0.95, 0.95, 0.95})
	verts = post3dAppendCheckerPanel(verts, math3d.Vec3{X: 1.3, Z: -12}, 2.2, 3, 6, 8, [3]float32{0.2, 0.4, 0.95}, [3]float32{0.95, 0.95, 0.95})
	vb, err := dev.CreateBuffer(nil, gpu.BufferDesc{Usage: gpu.UsageVertex, Data: post3dPackFloats(verts)})
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	defer func() { _ = vb.Close() }()
	scene := newPost3dEdgeTarget(t, dev, post3dEdgeWidth, post3dEdgeHeight, gpu.FormatRGBA16Float, gpu.FormatDepth32Float)

	cfg := render3d.DepthOfFieldConfigFromCamera(cam, 3, 1.5, 4)
	if !cfg.Orthographic || !post3dNear(cfg.Near, -2, 1e-3) {
		t.Fatalf("config from orthographic camera = %+v", cfg)
	}
	dof, err := render3d.NewDepthOfFieldPass(nil, dev, render3d.DepthOfFieldPassOptions{Width: post3dEdgeWidth, Height: post3dEdgeHeight, OutputFormat: gpu.FormatRGBA8Unorm, Config: cfg})
	if err != nil {
		t.Fatalf("NewDepthOfFieldPass: %v", err)
	}
	defer func() { _ = dof.Close() }()

	render := func(c render3d.DepthOfFieldConfig) *image.RGBA {
		if err := dof.SetConfig(c); err != nil {
			t.Fatalf("SetConfig: %v", err)
		}
		sp.cam.WriteCamera(cam)
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{Texture: scene.Color, LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x141820)}},
			DepthStencil:     &gpu.DepthStencilAttachment{Texture: scene.Depth, DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreStore, ClearDepth: 1},
		})
		sp.draw(pass, vb, uint32(len(verts)/10))
		pass.End()
		if err := dof.Apply(dev, enc, scene.Color, scene.Depth, nil); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		post3dEdgeSubmit(t, sr, enc)
		return sr.Image()
	}
	blurred := render(cfg)
	sharpCfg := cfg
	sharpCfg.MaxBlurRadius = 0
	sharp := render(sharpCfg)

	focusRaw := post3dRegionContrast(sharp, 6, 10, 26, 38)
	focusDoF := post3dRegionContrast(blurred, 6, 10, 26, 38)
	farRaw := post3dRegionContrast(sharp, 38, 10, 58, 38)
	farDoF := post3dRegionContrast(blurred, 38, 10, 58, 38)
	if focusRaw < 0.05 || focusDoF < focusRaw*0.9 {
		t.Errorf("in-focus orthographic panel blurred: %.4f vs %.4f", focusDoF, focusRaw)
	}
	if farRaw < 0.05 || farDoF > farRaw*0.6 {
		t.Errorf("far orthographic panel not blurred: %.4f vs %.4f", farDoF, farRaw)
	}
}

// With ProjMatrix left zero, SSAO falls back to a 60-degree perspective at the pass aspect ratio and the configured Near and Far, which matches a camera built that way.
func TestPost3DSSAOProjectionFallback(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(post3dEdgeWidth, post3dEdgeHeight, gpu.FormatDepth32Float)
	defer sr.Close()
	dev := sr.Device()
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 1.6, Y: 1.8, Z: 4.2}, math3d.DegToRad(60), float32(post3dEdgeWidth)/float32(post3dEdgeHeight), 0.2, 40)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0.4, Z: -0.4}, math3d.Vec3Up())
	scene := newPost3dEdgeScene(t, dev, cam)
	rt := newPost3dEdgeTarget(t, dev, post3dEdgeWidth, post3dEdgeHeight, gpu.FormatRGBA16Float, gpu.FormatDepth32Float)

	withMatrix := render3d.SSAOConfigFromCamera(cam)
	fallback := withMatrix
	fallback.ProjMatrix = math3d.Mat4{}
	fallback.Near, fallback.Far = 0.2, 40

	ao := func(cfg render3d.SSAOConfig) *image.RGBA {
		p, err := render3d.NewSSAOPass(nil, dev, render3d.SSAOPassOptions{Width: post3dEdgeWidth, Height: post3dEdgeHeight, Config: cfg})
		if err != nil {
			t.Fatalf("NewSSAOPass: %v", err)
		}
		defer func() { _ = p.Close() }()
		enc := dev.BeginEncoder()
		scene.draw(enc, rt.Color, rt.Depth)
		tex, err := p.ComputeAO(dev, enc, rt.Depth)
		if err != nil {
			t.Fatalf("ComputeAO: %v", err)
		}
		post3dEdgeSubmit(t, sr, enc)
		return readTextureRGBA(t, dev, tex)
	}
	a, b := ao(withMatrix), ao(fallback)
	if d := post3dMaxChannelDiff(a, b); d > 2 {
		t.Errorf("fallback projection AO differs from the 60-degree camera's by %d", d)
	}
	occluded := 0
	for y := range post3dEdgeHeight {
		for x := range post3dEdgeWidth {
			if a.RGBAAt(x, y).R < 230 {
				occluded++
			}
		}
	}
	if occluded == 0 {
		t.Error("AO map is blank; the scene produced no occlusion to compare")
	}
}

// Toggling BloomConfig.HalfRes through SetConfig reallocates the blur targets inside Apply, and switching back reproduces the first image.
func TestPost3DBloomHalfResToggle(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(post3dEdgeWidth, post3dEdgeHeight, gpu.FormatDepth32Float)
	defer sr.Close()
	dev := sr.Device()
	scene := newPost3dEdgeScene(t, dev, post3dEdgeCamera())
	rt := newPost3dEdgeTarget(t, dev, post3dEdgeWidth, post3dEdgeHeight, gpu.FormatRGBA16Float, gpu.FormatDepth32Float)

	full := render3d.DefaultBloomConfig()
	full.HalfRes = false
	bloom, err := render3d.NewBloomPass(nil, dev, render3d.BloomPassOptions{Width: post3dEdgeWidth, Height: post3dEdgeHeight, OutputFormat: gpu.FormatRGBA8Unorm, Config: full})
	if err != nil {
		t.Fatalf("NewBloomPass: %v", err)
	}
	defer func() { _ = bloom.Close() }()
	render := func(cfg render3d.BloomConfig) *image.RGBA {
		if err := bloom.SetConfig(cfg); err != nil {
			t.Fatalf("SetConfig: %v", err)
		}
		enc := dev.BeginEncoder()
		scene.draw(enc, rt.Color, rt.Depth)
		if err := bloom.Apply(dev, enc, rt.Color, nil); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		post3dEdgeSubmit(t, sr, enc)
		return sr.Image()
	}
	half := full
	half.HalfRes = true
	first := render(full)
	halfImg := render(half)
	again := render(full)
	if d := post3dMaxChannelDiff(first, halfImg); d == 0 {
		t.Error("half-resolution bloom rendered the same image as full resolution")
	}
	if d := post3dMaxChannelDiff(first, again); d > 1 {
		t.Errorf("full-resolution bloom after a HalfRes round trip differs by %d", d)
	}
}
