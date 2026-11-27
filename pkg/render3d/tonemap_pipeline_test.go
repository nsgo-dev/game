package render3d_test

import (
	"errors"
	"image"
	"image/color"
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

func uploadTestMesh(t *testing.T, dev *gpu.Device, m render3d.Mesh) *render3d.GPUMesh {
	t.Helper()
	gm, err := render3d.UploadMesh(nil, dev, m, render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = gm.Close() })
	return gm
}

// renderLitSceneThroughToneMap draws objects with the lit PBR pipeline in linear output mode into an HDRTarget and resolves it with ToneMapPass, the HDR counterpart of renderLitScene.
func renderLitSceneThroughToneMap(t *testing.T, width, height int, cam *scene3d.Camera, rig render3d.LightRig, clearCol core.Color, buildObjects func(dev *gpu.Device) []litSceneObject, opts render3d.ToneMapOptions) *image.RGBA {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	hdr, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{Width: uint32(width), Height: uint32(height), DepthFormat: gpu.FormatDepth24Plus})
	if err != nil {
		t.Fatalf("NewHDRTarget: %v", err)
	}
	t.Cleanup(func() { _ = hdr.Close() })
	if !hdr.IsHDR() {
		t.Fatalf("software device fell back to %v; the HDR path needs RGBA16Float", hdr.Format())
	}

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(cam)

	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	objects := buildObjects(dev)
	drawBufs := make([]*render3d.PBRDrawBuffer, len(objects))
	var sharedLayout *gpu.BindGroupLayout
	for i, obj := range objects {
		db, err := render3d.NewPBRDrawBuffer(nil, dev, sharedLayout, render3d.NewPBRDrawUniform(obj.model, obj.mat), false)
		if err != nil {
			t.Fatalf("NewPBRDrawBuffer[%d]: %v", i, err)
		}
		if sharedLayout == nil {
			sharedLayout = db.Layout()
		}
		drawBufs[i] = db
		t.Cleanup(func() { _ = db.Close() })
	}

	pipe, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), sharedLayout, render3d.LitPBRPipelineOptions{
		ColorFormat:  hdr.Format(),
		DepthFormat:  gpu.FormatDepth24Plus,
		LinearOutput: true,
	})
	if err != nil {
		t.Fatalf("NewLitPBRPipelineWithOptions: %v", err)
	}
	tonePass, err := render3d.NewToneMapPass(nil, dev, render3d.ToneMapPassDesc{TargetFormat: dev.SurfaceFormat()})
	if err != nil {
		t.Fatalf("NewToneMapPass: %v", err)
	}
	t.Cleanup(func() { _ = tonePass.Close() })

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(hdr.RenderPassDesc(clearCol, 1))
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	lightBuf.Bind(pass)
	for i, obj := range objects {
		pass.SetBindGroup(render3d.PBRDrawGroup, drawBufs[i].BindGroup())
		obj.mesh.Draw(pass)
	}
	pass.End()
	resolve := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
	if err := tonePass.DrawTarget(resolve, dev, hdr, opts); err != nil {
		t.Fatalf("DrawTarget: %v", err)
	}
	resolve.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	return sr.Image()
}

func vec3ToRGBA(v math3d.Vec3) color.RGBA {
	return color.RGBA{R: quantizeByte(v.X), G: quantizeByte(v.Y), B: quantizeByte(v.Z), A: 255}
}

func nearRGB(a, b color.RGBA, tol uint8) bool {
	return absDiffU8(a.R, b.R) <= tol && absDiffU8(a.G, b.G) <= tol && absDiffU8(a.B, b.B) <= tol
}

// TestLitPBRLinearOutputThroughToneMap renders lit PBR in linear HDR mode, tone-maps it and checks pixels against ApplyToneMap(ShadePoint). With ToneMapNone and sRGB it must equal the LDR pipeline's own output, and with a curve it must keep radiance above 1.
func TestLitPBRLinearOutputThroughToneMap(t *testing.T) {
	const w, h = 48, 48
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(50), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	rig := render3d.LightRig{
		Ambient:     render3d.AmbientLight{Color: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 0.1},
		Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{X: -0.3, Y: -0.2, Z: -1}, Color: math3d.Vec3{X: 1, Y: 0.9, Z: 0.8}, Intensity: 4}},
	}
	mat := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.9, Y: 0.6, Z: 0.3}, Roughness: 0.6, Emissive: math3d.Vec3{X: 0.5, Y: 0, Z: 1.5}}
	build := func(dev *gpu.Device) []litSceneObject {
		return []litSceneObject{{mesh: uploadTestMesh(t, dev, worldQuadMesh(math3d.Vec3{}, 2, 2)), model: math3d.Mat4Identity(), mat: mat}}
	}
	ray := cam.ScreenRay(w/2+0.5, h/2+0.5, w, h)
	hit := ray.At(-ray.Origin.Z / ray.Dir.Z)
	lin := render3d.ShadePoint(hit, math3d.Vec3{Z: 1}, cam.Pos.Sub(hit).Norm(), mat, rig)
	if max(lin.X, lin.Y, lin.Z) <= 1 {
		t.Fatalf("test scene radiance %v does not exceed 1", lin)
	}

	ldr := renderLitScene(t, w, h, cam, rig, core.RGB(0x000000), build).RGBAAt(w/2, h/2)
	for _, opts := range []render3d.ToneMapOptions{
		{Operator: render3d.ToneMapNone, Exposure: 1},
		{Operator: render3d.ToneMapACES, Exposure: 1},
		{Operator: render3d.ToneMapAgX, Exposure: render3d.ExposureFromEV(-1)},
	} {
		got := renderLitSceneThroughToneMap(t, w, h, cam, rig, core.RGB(0x000000), build, opts).RGBAAt(w/2, h/2)
		want := vec3ToRGBA(render3d.ApplyToneMap(lin, opts))
		if !nearRGB(got, want, 3) {
			t.Errorf("%s: HDR path pixel %v, want ApplyToneMap(ShadePoint) %v", opts.Operator, got, want)
		}
		if opts.Operator == render3d.ToneMapNone && !nearRGB(got, ldr, 3) {
			t.Errorf("ToneMapNone HDR path pixel %v differs from LitPBRWGSL's own output %v", got, ldr)
		}
		if opts.Operator == render3d.ToneMapACES && got.B >= 255 {
			t.Errorf("ACES clipped the HDR blue channel: %v", got)
		}
	}
}

// TestMaterialLinearOutputThroughToneMap draws a material in linear output mode into an HDRTarget: Reinhard with gamma 2.2 reproduces MaterialWGSL's built-in tone mapping, and ACES matches ApplyToneMap of EvaluateMaterial's linear radiance.
func TestMaterialLinearOutputThroughToneMap(t *testing.T) {
	const size = 48
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	hdr, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{Width: size, Height: size, DepthFormat: gpu.FormatDepth24Plus})
	if err != nil {
		t.Fatalf("NewHDRTarget: %v", err)
	}
	t.Cleanup(func() { _ = hdr.Close() })
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(50), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = defaults.Close() })
	pipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		ColorFormat:  hdr.Format(),
		DepthFormat:  gpu.FormatDepth24Plus,
		LinearOutput: true,
	})
	if err != nil {
		t.Fatalf("NewMaterialPipeline LinearOutput: %v", err)
	}
	t.Cleanup(func() { _ = pipe.Close() })
	tonePass, err := render3d.NewToneMapPass(nil, dev, render3d.ToneMapPassDesc{TargetFormat: dev.SurfaceFormat()})
	if err != nil {
		t.Fatalf("NewToneMapPass: %v", err)
	}
	t.Cleanup(func() { _ = tonePass.Close() })
	quad := uploadTestMesh(t, dev, worldQuadMesh(math3d.Vec3{}, 2, 2))

	m := render3d.DefaultMaterial()
	m.BaseColorFactor = [4]float32{0.82, 0.5, 0.2, 1}
	m.RoughnessFactor = 0.4
	m.EmissiveFactor = [3]float32{2.5, 0.3, 0.1}
	gm, err := render3d.NewGPUMaterial(nil, dev, defaults, m, render3d.MaterialUpload{})
	if err != nil {
		t.Fatalf("NewGPUMaterial: %v", err)
	}
	t.Cleanup(func() { _ = gm.Close() })
	eval := render3d.EvaluateMaterial(render3d.MaterialEvalInput{Material: m, Normal: math3d.Vec3{Z: 1}, ViewDir: math3d.Vec3{Z: 1}, FrontFacing: true})
	lin := math3d.Vec3{X: eval.Linear.X, Y: eval.Linear.Y, Z: eval.Linear.Z}
	if lin.X <= 1 {
		t.Fatalf("test material radiance %v does not exceed 1", lin)
	}

	for _, tc := range []struct {
		opts render3d.ToneMapOptions
		want color.RGBA
	}{
		{render3d.ToneMapOptions{Operator: render3d.ToneMapReinhard, Exposure: 1, Gamma: 2.2}, srgbVec4ToRGBA(eval.SRGB)},
		{render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: 1}, vec3ToRGBA(render3d.ApplyToneMap(lin, render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: 1}))},
	} {
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(hdr.RenderPassDesc(core.RGB(0x000000), 1))
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		gm.Bind(pass)
		quad.Draw(pass)
		pass.End()
		resolve := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
		if err := tonePass.DrawTarget(resolve, dev, hdr, tc.opts); err != nil {
			t.Fatalf("DrawTarget: %v", err)
		}
		resolve.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		if got := sr.Image().RGBAAt(size/2, size/2); !nearRGB(got, tc.want, 3) {
			t.Errorf("%s gamma %g: pixel %v, want %v", tc.opts.Operator, tc.opts.Gamma, got, tc.want)
		}
	}
}

// TestLinearOutputPipelineVariantsCompile compiles every linear output variant (lit PBR with fog and with IBL, and fogged materials) and checks that fog and IBL together are refused.
func TestLinearOutputPipelineVariantsCompile(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	lightBuf, err := render3d.NewLightBuffer(nil, dev, render3d.LightRig{}, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })
	drawBuf, err := render3d.NewPBRDrawBuffer(nil, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), render3d.PBRMaterial{}), false)
	if err != nil {
		t.Fatalf("NewPBRDrawBuffer: %v", err)
	}
	t.Cleanup(func() { _ = drawBuf.Close() })
	fogBuf, err := render3d.NewGPUFogBuffer(nil, dev, render3d.Fog{Mode: render3d.FogLinear, Start: 1, End: 10, MaxOpacity: 1}, render3d.FogBufferOptions{})
	if err != nil {
		t.Fatalf("NewGPUFogBuffer: %v", err)
	}
	t.Cleanup(func() { _ = fogBuf.Close() })
	env, err := render3d.GradientSkyCubemap([3]float32{0.2, 0.4, 0.8}, [3]float32{0.8, 0.8, 0.9}, [3]float32{0.2, 0.2, 0.2}, 4)
	if err != nil {
		t.Fatalf("GradientSkyCubemap: %v", err)
	}
	cube, err := render3d.UploadCubemap(nil, dev, env, render3d.CubemapUpload{})
	if err != nil {
		t.Fatalf("UploadCubemap: %v", err)
	}
	t.Cleanup(func() { _ = cube.Close() })
	sh, err := render3d.ComputeIrradianceSH(env)
	if err != nil {
		t.Fatalf("ComputeIrradianceSH: %v", err)
	}
	iblBuf, err := render3d.NewGPUIBLBuffer(nil, dev, cube, render3d.NewIBLUniform(sh, cube, 1), render3d.IBLBufferOptions{})
	if err != nil {
		t.Fatalf("NewGPUIBLBuffer: %v", err)
	}
	t.Cleanup(func() { _ = iblBuf.Close() })

	for i, opts := range []render3d.LitPBRPipelineOptions{
		{ColorFormat: gpu.FormatRGBA16Float, LinearOutput: true},
		{ColorFormat: gpu.FormatRGBA16Float, FogLayout: fogBuf.Layout(), LinearOutput: true},
		{ColorFormat: gpu.FormatRGBA16Float, IBLLayout: iblBuf.Layout(), LinearOutput: true},
		{IBLLayout: iblBuf.Layout(), DepthFormat: gpu.FormatDepth24Plus},
	} {
		pipe, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), opts)
		if err != nil {
			t.Fatalf("variant %d: %v", i, err)
		}
		_ = pipe.Close()
	}
	_, err = render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{FogLayout: fogBuf.Layout(), IBLLayout: iblBuf.Layout()})
	if !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("fog and IBL together: err = %v, want ErrInvalidDescriptor", err)
	}
	if _, err := render3d.NewLitPBRPipelineWithOptions(nil, nil, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("nil device: err = %v, want ErrDeviceLost", err)
	}

	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = defaults.Close() })
	pipe, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{ColorFormat: gpu.FormatRGBA16Float, FogLayout: fogBuf.Layout(), LinearOutput: true})
	if err != nil {
		t.Fatalf("NewMaterialPipeline fog linear: %v", err)
	}
	_ = pipe.Close()
}

// TestToneMapDrawDynamicResolution renders a half-resolution HDR scene into a DynamicResolutionTarget's scaled sub-viewport and checks that DrawDynamicResolution tone-maps and upscales only that region over the whole native output.
func TestToneMapDrawDynamicResolution(t *testing.T) {
	const nativeW, nativeH = 32, 16
	sr := gpu.NewSoftwareRenderer(nativeW, nativeH, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	dr, err := render3d.NewDynamicResolutionTarget(nil, dev, render3d.DynamicResolutionTargetOptions{
		ColorFormat:  render3d.PreferredHDRFormat(dev),
		DisableDepth: true,
	})
	if err != nil {
		t.Fatalf("NewDynamicResolutionTarget: %v", err)
	}
	t.Cleanup(func() { _ = dr.Close() })
	dr.Scaler().SetFixedScale(0.5)

	tonePass, err := render3d.NewToneMapPass(nil, dev, render3d.ToneMapPassDesc{TargetFormat: dev.SurfaceFormat()})
	if err != nil {
		t.Fatalf("NewToneMapPass: %v", err)
	}
	t.Cleanup(func() { _ = tonePass.Close() })
	opts := render3d.ToneMapOptions{Operator: render3d.ToneMapACES, Exposure: 1}

	enc := dev.BeginEncoder()
	resolve := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
	if err := tonePass.DrawDynamicResolution(resolve, dev, dr, opts); !errors.Is(err, render3d.ErrInvalidDynamicResolution) {
		t.Errorf("DrawDynamicResolution before Begin3D: err = %v, want ErrInvalidDynamicResolution", err)
	}
	if err := tonePass.DrawDynamicResolution(resolve, dev, nil, opts); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("DrawDynamicResolution(nil): err = %v, want ErrInvalidDescriptor", err)
	}
	resolve.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()

	// The scene fills the scaled viewport with one HDR color; the unused texels keep the clear color, which must not reach the output.
	sceneColor := math3d.Vec3{X: 4, Y: 1, Z: 0.25}
	mod, err := gpu.CompileWGSL(hdrPatchWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	bgl, err := dev.CreateBindGroupLayout(nil, []gpu.BindGroupLayoutEntry{{Binding: 0, Visibility: gpu.StageFragment, Type: gpu.BindingUniform}})
	if err != nil {
		t.Fatalf("CreateBindGroupLayout: %v", err)
	}
	pipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{bgl},
		VertexBuffers: []gpu.VertexBufferLayout{{Stride: 3 * 4, Attributes: []gpu.VertexAttribute{{Location: 0, Format: gpu.VertexFloat32x3}}}},
		Targets:       []gpu.ColorTargetState{{Format: dr.ColorFormat()}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	vb, err := dev.CreateBuffer(nil, gpu.BufferDesc{Usage: gpu.UsageVertex, Data: packFloat32s([]float32{-1, -1, 0.5, 3, -1, 0.5, -1, 3, 0.5})})
	if err != nil {
		t.Fatalf("CreateBuffer vb: %v", err)
	}
	ub, err := dev.CreateBuffer(nil, gpu.BufferDesc{Usage: gpu.UsageUniform, Data: packFloat32s([]float32{sceneColor.X, sceneColor.Y, sceneColor.Z, 1})})
	if err != nil {
		t.Fatalf("CreateBuffer ub: %v", err)
	}
	bg, err := dev.CreateBindGroup(nil, bgl, []gpu.BindGroupEntry{{Binding: 0, Buffer: ub}})
	if err != nil {
		t.Fatalf("CreateBindGroup: %v", err)
	}

	enc = dev.BeginEncoder()
	scenePass, err := dr.Begin3D(enc, nativeW, nativeH, core.RGB(0x000000))
	if err != nil {
		t.Fatalf("Begin3D: %v", err)
	}
	scenePass.SetPipeline(pipe)
	scenePass.SetBindGroup(0, bg)
	scenePass.SetVertexBuffer(0, vb, 0)
	scenePass.Draw(3, 0)
	scenePass.End()
	if w, h := dr.ScaledSize(); w != nativeW/2 || h != nativeH/2 {
		t.Fatalf("ScaledSize = %dx%d, want %dx%d", w, h, nativeW/2, nativeH/2)
	}
	resolve = enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
	if err := tonePass.DrawDynamicResolution(resolve, dev, dr, opts); err != nil {
		t.Fatalf("DrawDynamicResolution: %v", err)
	}
	resolve.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	want := vec3ToRGBA(render3d.ApplyToneMap(sceneColor, opts))
	img := sr.Image()
	for _, p := range [][2]int{{0, 0}, {nativeW - 1, 0}, {0, nativeH - 1}, {nativeW - 1, nativeH - 1}, {nativeW / 2, nativeH / 2}} {
		if got := img.RGBAAt(p[0], p[1]); !nearRGB(got, want, 3) {
			t.Errorf("pixel %v = %v, want the tone-mapped scene color %v everywhere", p, got, want)
		}
	}
}

// TestHDRTargetMSAAResolve creates a 4x multisampled HDR target on a device that supports MSAA4x and checks the resolve wiring and memory accounting.
func TestHDRTargetMSAAResolve(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 8, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	if !dev.Features().MSAA4x {
		t.Skip("device has no MSAA4x")
	}
	hdr, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{Width: 16, Height: 8, SampleCount: 4, DepthFormat: gpu.FormatDepth24Plus})
	if err != nil {
		t.Fatalf("NewHDRTarget: %v", err)
	}
	t.Cleanup(func() { _ = hdr.Close() })
	rt := hdr.RenderTarget()
	if hdr.SampleCount() != 4 || rt.Resolve == nil {
		t.Fatalf("SampleCount = %d, Resolve = %v, want 4 and a resolve texture", hdr.SampleCount(), rt.Resolve)
	}
	if hdr.ColorTexture() != rt.Resolve || hdr.ColorAttachmentTexture() != rt.Color {
		t.Error("ColorTexture must be the resolve texture and ColorAttachmentTexture the multisampled one")
	}
	if a := hdr.ColorAttachment(core.RGB(0)); a.Texture != rt.Color || a.ResolveTarget != rt.Resolve {
		t.Errorf("ColorAttachment = %+v, want the color texture resolving into Resolve", a)
	}
	f := hdr.Format()
	want := f.ByteSize(16, 8)*4 + f.ByteSize(16, 8) + gpu.FormatDepth24Plus.ByteSize(16, 8)*4
	if got := hdr.ByteSize(); got != want {
		t.Errorf("ByteSize = %d, want %d (4x color + resolve + 4x depth)", got, want)
	}

	// The resolved texture feeds the tone-map pass like a single-sampled one.
	tonePass, err := render3d.NewToneMapPass(nil, dev, render3d.ToneMapPassDesc{TargetFormat: dev.SurfaceFormat()})
	if err != nil {
		t.Fatalf("NewToneMapPass: %v", err)
	}
	t.Cleanup(func() { _ = tonePass.Close() })
	enc := dev.BeginEncoder()
	enc.BeginRenderPass(hdr.RenderPassDesc(core.RGB(0x808080), 1)).End()
	resolve := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
	opts := render3d.ToneMapOptions{Operator: render3d.ToneMapNone, Exposure: 1, LinearOutput: true}
	if err := tonePass.DrawTarget(resolve, dev, hdr, opts); err != nil {
		t.Fatalf("DrawTarget: %v", err)
	}
	resolve.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	if got := sr.Image().RGBAAt(8, 4); absDiffU8(got.R, 0x80) > 2 {
		t.Errorf("resolved clear color through the tone map = %v, want ~0x80", got)
	}
}

// TestToneMapPassDrawErrors checks Draw's and DrawTarget's argument errors.
func TestToneMapPassDrawErrors(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	tonePass, err := render3d.NewToneMapPass(nil, dev, render3d.ToneMapPassDesc{})
	if err != nil {
		t.Fatalf("NewToneMapPass: %v", err)
	}
	t.Cleanup(func() { _ = tonePass.Close() })
	hdr, err := render3d.NewHDRTarget(nil, dev, render3d.HDRTargetDesc{Width: 8, Height: 8})
	if err != nil {
		t.Fatalf("NewHDRTarget: %v", err)
	}
	opts := render3d.DefaultToneMapOptions()

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
	if err := tonePass.Draw(nil, dev, hdr.ColorTexture(), opts); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("Draw(nil pass): err = %v, want ErrInvalidDescriptor", err)
	}
	if err := tonePass.Draw(pass, dev, nil, opts); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("Draw(nil texture): err = %v, want ErrInvalidDescriptor", err)
	}
	if err := tonePass.DrawTarget(pass, dev, nil, opts); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("DrawTarget(nil): err = %v, want ErrInvalidDescriptor", err)
	}
	closed := hdr.ColorTexture()
	_ = hdr.Close()
	if err := tonePass.Draw(pass, dev, closed, opts); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("Draw(closed texture): err = %v, want ErrInvalidDescriptor", err)
	}
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()

	_ = tonePass.Close()
	if err := tonePass.Draw(pass, dev, closed, opts); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("Draw on a closed pass: err = %v, want ErrDeviceLost", err)
	}
}
