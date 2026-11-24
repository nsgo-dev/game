package render3d_test

import (
	"encoding/binary"
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

// morphSceneWGSL transforms morphed mesh vertices by a model matrix and the
// camera's view_proj matrix, shading fragments from their morphed normal so
// both silhouette deformation and normal blending appear in pixel readback.
const morphSceneWGSL = render3d.CameraWGSL + `
struct Model {
	world: mat4x4<f32>,
}

@group(1) @binding(0) var<uniform> model: Model;

struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) pos: vec4<f32>,
	@location(0) normal: vec3<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	let worldPos = model.world * vec4<f32>(in.pos, 1.0);
	out.pos = camera.view_proj * worldPos;
	out.normal = normalize((model.world * vec4<f32>(in.normal, 0.0)).xyz);
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	let n = normalize(in.normal) * 0.5 + vec3<f32>(0.5, 0.5, 0.5);
	return vec4<f32>(n, 1.0);
}
`

func encodeMat4(m math3d.Mat4) []byte {
	buf := make([]byte, 64)
	for i, f := range m {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func loadAnimatedMorphCubeModel(t *testing.T) (*gltf.Document, render3d.MorphMesh, math3d.Mat4) {
	t.Helper()
	dir := filepath.Join("..", "gltf", "testdata", "khronos", "AnimatedMorphCube", "glTF")
	doc, err := gltf.LoadFS(os.DirFS(dir), "AnimatedMorphCube.gltf")
	if err != nil {
		t.Fatalf("LoadFS AnimatedMorphCube: %v", err)
	}
	if err := doc.ComputeWorldMatrices(); err != nil {
		t.Fatalf("ComputeWorldMatrices: %v", err)
	}
	prim := doc.Meshes[0].Primitives[0]
	targets := make([]render3d.MorphTarget, len(prim.Targets))
	for i, tgt := range prim.Targets {
		targets[i] = render3d.MorphTarget{
			Positions: tgt.Positions,
			Normals:   tgt.Normals,
		}
	}
	mm := render3d.MorphMesh{
		Base: render3d.Mesh{
			Positions: prim.Positions,
			Normals:   prim.Normals,
			UVs:       prim.TexCoords0,
			Indices:   prim.Indices,
		},
		Targets: targets,
		Weights: gltf.ResolveWeights(doc, 0),
	}
	return doc, mm, doc.Nodes[0].WorldMatrix
}

func countDifferentPixels(a, b *image.RGBA) int {
	n := 0
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pa, pb := a.RGBAAt(x, y), b.RGBAAt(x, y)
			if max(pa.R, pb.R)-min(pa.R, pb.R) > 2 ||
				max(pa.G, pb.G)-min(pa.G, pb.G) > 2 ||
				max(pa.B, pb.B)-min(pa.B, pb.B) > 2 {
				n++
			}
		}
	}
	return n
}

func countNonClearPixels(img *image.RGBA, clearColor color.RGBA) int {
	n := 0
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if max(p.R, clearColor.R)-min(p.R, clearColor.R) > 2 ||
				max(p.G, clearColor.G)-min(p.G, clearColor.G) > 2 ||
				max(p.B, clearColor.B)-min(p.B, clearColor.B) > 2 {
				n++
			}
		}
	}
	return n
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func TestAnimatedMorphCubeDrawsInGPUView(t *testing.T) {
	doc, morphModel, worldMat := loadAnimatedMorphCubeModel(t)
	anim := doc.Animations[0]

	// Locate peak weight timestamps for both morph targets in AnimatedMorphCube.
	ch := anim.Channels[0]
	var peak0T, peak1T float32
	var maxW0, maxW1 float32
	for k, ts := range ch.Times {
		if w0 := ch.Values[k*2]; w0 > maxW0 {
			maxW0, peak0T = w0, ts
		}
		if w1 := ch.Values[k*2+1]; w1 > maxW1 {
			maxW1, peak1T = w1, ts
		}
	}

	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			const size = 96
			var (
				dev        *gpu.Device
				morphMesh  *render3d.GPUMorphMesh
				camBuf     *render3d.CameraBuffer
				modelGroup *gpu.BindGroup
				pipe       *gpu.Pipeline
				animTime   float32
				weights    []float32
				uploads    int
			)

			cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 3.4, Y: 2.0, Z: 3.4}, 45*math.Pi/180, 1, 0.1, 50)
			cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())

			opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "viewport",
					Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(d *gpu.Device) {
						dev = d
						var err error
						morphMesh, err = render3d.UploadMorphMesh(cx, dev, morphModel, render3d.MeshUpload{Label: "AnimatedMorphCube"})
						if err != nil {
							t.Fatalf("UploadMorphMesh: %v", err)
						}
						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
						modelBuf, err := dev.CreateBuffer(cx, gpu.BufferDesc{
							Label: "model",
							Usage: gpu.UsageUniform | gpu.UsageCopyDst,
							Data:  encodeMat4(worldMat),
						})
						if err != nil {
							t.Fatalf("CreateBuffer model: %v", err)
						}
						modelLayout, err := dev.CreateBindGroupLayoutDesc(cx, gpu.BindGroupLayoutDesc{
							Label: "model",
							Entries: []gpu.BindGroupLayoutEntry{{
								Binding:    0,
								Visibility: gpu.StageVertex,
								Type:       gpu.BindingUniform,
							}},
						})
						if err != nil {
							t.Fatalf("CreateBindGroupLayoutDesc model: %v", err)
						}
						modelGroup, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
							Label:   "model",
							Layout:  modelLayout,
							Entries: []gpu.BindGroupEntry{{Binding: 0, Buffer: modelBuf, Size: 64}},
						})
						if err != nil {
							t.Fatalf("CreateBindGroupDesc model: %v", err)
						}
						mod, err := gpu.CompileWGSL(morphSceneWGSL)
						if err != nil {
							t.Fatalf("CompileWGSL: %v", err)
						}
						pipe, err = dev.CreatePipeline(cx, gpu.PipelineDesc{
							Label:         "morph-cube",
							Layouts:       []*gpu.BindGroupLayout{camBuf.Layout(), modelLayout},
							Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
							Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
							VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
							Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
							DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
							Targets:       []gpu.ColorTargetState{{Format: dev.SurfaceFormat()}},
						})
						if err != nil {
							t.Fatalf("CreatePipeline: %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						var ok bool
						weights, ok = gltf.SampleMorphWeights(anim, 0, animTime, weights)
						if !ok {
							t.Fatalf("SampleMorphWeights failed at t=%v", animTime)
						}
						if err := morphMesh.SetWeights(weights); err != nil {
							t.Fatalf("SetWeights: %v", err)
						}
						if morphMesh.Update(dev) {
							uploads++
						}
						camBuf.WriteCamera(cam)
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x10141c))
						pass.ClearDepth(1)
						pass.SetPipeline(pipe)
						pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
						pass.SetBindGroup(1, modelGroup)
						morphMesh.Draw(pass)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			renderAt := func(ts float32) *image.RGBA {
				app.Post(func() { animTime = ts })
				app.Settle()
				app.Advance(16 * time.Millisecond)
				img, err := app.GPUReadPixels("id:viewport")
				if err != nil {
					t.Fatalf("GPUReadPixels(t=%v): %v", ts, err)
				}
				return cloneRGBA(img)
			}

			// Frame 0: base cube (t = 0, weights = [0, 0]). Initial weights match upload, so 0 extra uploads.
			imgBase := renderAt(0)
			if uploads != 0 {
				t.Errorf("uploads at initial pose = %d, want 0", uploads)
			}
			baseCov := countNonClearPixels(imgBase, meshClear)
			if baseCov < 400 {
				t.Fatalf("base cube coverage = %d pixels, want >= 400", baseCov)
			}
			wantPixel(t, imgBase, 2, 2, meshClear)

			// Re-render at t=0: unchanged weights must not trigger a GPU upload.
			_ = renderAt(0)
			if uploads != 0 {
				t.Errorf("uploads after unchanged pose = %d, want 0", uploads)
			}

			// Frame at peak target 0 ("thin"): visibly distinct morphed silhouette & shading.
			imgPeak0 := renderAt(peak0T)
			if uploads != 1 {
				t.Errorf("uploads after peak0 = %d, want 1", uploads)
			}
			diff0 := countDifferentPixels(imgBase, imgPeak0)
			if diff0 < 200 {
				t.Fatalf("peak0 vs base changed only %d pixels, want >= 200", diff0)
			}

			// Frame at peak target 1 ("angle"): visibly distinct from both base and peak0.
			imgPeak1 := renderAt(peak1T)
			if uploads != 2 {
				t.Errorf("uploads after peak1 = %d, want 2", uploads)
			}
			diff1Base := countDifferentPixels(imgBase, imgPeak1)
			diff1Peak0 := countDifferentPixels(imgPeak0, imgPeak1)
			if diff1Base < 200 || diff1Peak0 < 100 {
				t.Fatalf("peak1 diffs: vs base=%d (want >=200), vs peak0=%d (want >=100)", diff1Base, diff1Peak0)
			}

			// Loop wrap back to t=0: image must match initial base pose cleanly.
			imgWrap := renderAt(float32(math.Mod(float64(anim.Duration), float64(anim.Duration))))
			if diffWrap := countDifferentPixels(imgBase, imgWrap); diffWrap != 0 {
				t.Errorf("loop wrap differed from initial pose by %d pixels, want 0", diffWrap)
			}
		})
	}
}

func TestGPUMorphMeshRetainedDeviceLoss(t *testing.T) {
	// A quad covering the center half of clip space; target 0 expands it to cover
	// almost the full viewport ([-0.95, 0.95]), so pixel (8, 8) distinguishes base vs morphed.
	base := quadMesh()
	expand := render3d.MorphTarget{
		Positions: []math3d.Vec3{
			{X: -0.45, Y: -0.45, Z: 0},
			{X: 0.45, Y: -0.45, Z: 0},
			{X: 0.45, Y: 0.45, Z: 0},
			{X: -0.45, Y: 0.45, Z: 0},
		},
	}
	model := render3d.MorphMesh{
		Base:    base,
		Targets: []render3d.MorphTarget{expand},
	}

	const size = 64
	var (
		dev   *gpu.Device
		morph *render3d.GPUMorphMesh
		pipe  *gpu.Pipeline
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(d *gpu.Device) {
				dev = d
				if morph == nil {
					var err error
					morph, err = render3d.UploadMorphMesh(cx, dev, model, render3d.MeshUpload{Label: "retained-morph", Retained: true})
					if err != nil {
						t.Fatalf("UploadMorphMesh: %v", err)
					}
				}
				pipe = meshPipeline(t, cx, dev, gpu.FormatDepth24Plus)
			},
			OnFrame: func(frame *gpu.Frame) {
				morph.Update(dev)
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				morph.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	magenta := color.RGBA{R: 255, G: 0, B: 255, A: 255}

	// Base pose: center covered, (8, 8) is clear.
	app.Advance(16 * time.Millisecond)
	img, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels base: %v", err)
	}
	wantPixel(t, img, 32, 32, magenta)
	wantPixel(t, img, 8, 8, meshClear)

	// Expand morph to weight 1.0 before device loss: (8, 8) becomes magenta.
	app.Post(func() {
		if err := morph.SetWeights([]float32{1}); err != nil {
			t.Fatalf("SetWeights(1): %v", err)
		}
	})
	app.Settle()
	app.Advance(16 * time.Millisecond)
	img, err = app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels expanded: %v", err)
	}
	wantPixel(t, img, 8, 8, magenta)

	// Scenario A: Device loss and restore preserves the last uploaded morphed vertices.
	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
	if morph.Valid() {
		t.Fatal("morph mesh still valid while device is lost")
	}
	app.RestoreGPUDevice("id:viewport")
	if !morph.Valid() {
		t.Fatal("retained morph mesh not valid after restore")
	}
	app.Advance(16 * time.Millisecond)
	img, err = app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	wantPixel(t, img, 8, 8, magenta)

	// Scenario B: Weight change while device is lost stays dirty until restore, then uploads.
	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
	app.Post(func() {
		if err := morph.SetWeights([]float32{0}); err != nil {
			t.Fatalf("SetWeights(0) while lost: %v", err)
		}
		if morph.Update(dev) {
			t.Error("Update returned true while device is lost")
		}
		if !morph.Dirty() {
			t.Error("morph cleared Dirty while device is lost")
		}
	})
	app.Settle()
	app.RestoreGPUDevice("id:viewport")
	app.Advance(16 * time.Millisecond)
	if morph.Dirty() {
		t.Error("morph still dirty after restore and frame render")
	}
	img, err = app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after pending update: %v", err)
	}
	wantPixel(t, img, 32, 32, magenta)
	wantPixel(t, img, 8, 8, meshClear)
}

func TestGPUMorphMeshDirtyBoundsAndZeroAlloc(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	base := quadMesh()
	deltas := []math3d.Vec3{{X: -1}, {X: 1}, {X: 1}, {X: -1}}
	normDeltas := []math3d.Vec3{{X: 1}, {X: 1}, {X: 1}, {X: 1}}
	model := render3d.MorphMesh{
		Base:    base,
		Targets: []render3d.MorphTarget{{Positions: deltas, Normals: normDeltas}},
		Weights: []float32{0.5},
	}

	gm, err := render3d.UploadMorphMesh(nil, dev, model, render3d.MeshUpload{Label: "dirty-test"})
	if err != nil {
		t.Fatalf("UploadMorphMesh: %v", err)
	}
	defer gm.Close()

	// Mutating caller slices after upload must not affect gm.
	deltas[0].X = 999
	base.Positions[0].X = 999

	if gm.Dirty() {
		t.Fatal("freshly uploaded morph mesh must not be dirty")
	}
	if gm.Update(dev) {
		t.Fatal("Update on clean morph mesh must return false")
	}
	if gm.TargetCount() != 1 || gm.VertexCount() != 4 || gm.IndexCount() != 6 || gm.Triangles() != 2 || !gm.Indexed() {
		t.Fatalf("unexpected counts targets=%d v=%d i=%d tri=%d indexed=%v",
			gm.TargetCount(), gm.VertexCount(), gm.IndexCount(), gm.Triangles(), gm.Indexed())
	}

	// Initial weights [0.5] should have expanded X from [-0.5, 0.5] to [-1.0, 1.0].
	wantInitBounds := math3d.AABB{Min: math3d.Vec3{X: -1, Y: -0.5, Z: 0.5}, Max: math3d.Vec3{X: 1, Y: 0.5, Z: 0.5}}
	if gm.Bounds() != wantInitBounds {
		t.Fatalf("initial Bounds = %+v, want %+v", gm.Bounds(), wantInitBounds)
	}

	// Setting identical weights keeps it clean.
	if err := gm.SetWeights([]float32{0.5}); err != nil {
		t.Fatalf("SetWeights same: %v", err)
	}
	if gm.Dirty() || gm.Update(dev) {
		t.Fatal("unchanged weights must not mark dirty or upload")
	}

	// Changing weights updates Bounds immediately AND still uploads on Update.
	if err := gm.SetWeights([]float32{1.0}); err != nil {
		t.Fatalf("SetWeights 1.0: %v", err)
	}
	if !gm.Dirty() {
		t.Fatal("expected dirty after weight change")
	}
	wantExpanded := math3d.AABB{Min: math3d.Vec3{X: -1.5, Y: -0.5, Z: 0.5}, Max: math3d.Vec3{X: 1.5, Y: 0.5, Z: 0.5}}
	if gm.Bounds() != wantExpanded || gm.Mesh().Bounds() != wantExpanded {
		t.Fatalf("expanded Bounds = %+v (mesh=%+v), want %+v", gm.Bounds(), gm.Mesh().Bounds(), wantExpanded)
	}
	if !gm.Update(dev) {
		t.Fatal("Update must still upload after Bounds() evaluated CPU blend")
	}
	if gm.Dirty() || gm.Update(dev) {
		t.Fatal("second Update after clean must return false")
	}

	// Resetting with nil clears weights to 0.
	if err := gm.SetWeights(nil); err != nil {
		t.Fatalf("SetWeights(nil): %v", err)
	}
	gotW := gm.Weights(nil)
	if len(gotW) != 1 || gotW[0] != 0 {
		t.Fatalf("Weights after nil reset = %v, want [0]", gotW)
	}
	if !gm.Update(dev) {
		t.Fatal("Update after reset must upload")
	}

	// Zero-alloc check for SetWeights + Weights + Bounds CPU blending.
	wIn := []float32{0.25}
	wOut := make([]float32, 0, 1)
	allocs := testing.AllocsPerRun(50, func() {
		wIn[0] = 0.75 - wIn[0]
		_ = gm.SetWeights(wIn)
		wOut = gm.Weights(wOut)
		_ = gm.Bounds()
	})
	if allocs != 0 {
		t.Errorf("steady-state SetWeights+Weights+Bounds allocated %v times, want 0", allocs)
	}
}

func TestMorphMeshValidationAndErrors(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	base := quadMesh()
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	tests := []struct {
		name string
		mesh render3d.MorphMesh
		ok   bool
	}{
		{"valid zero targets", render3d.MorphMesh{Base: base}, true},
		{"valid position-only target", render3d.MorphMesh{Base: base, Targets: []render3d.MorphTarget{{Positions: make([]math3d.Vec3, 4)}}}, true},
		{"valid normal-only target", render3d.MorphMesh{Base: base, Targets: []render3d.MorphTarget{{Normals: make([]math3d.Vec3, 4)}}}, true},
		{"invalid base", render3d.MorphMesh{}, false},
		{"wrong target position count", render3d.MorphMesh{Base: base, Targets: []render3d.MorphTarget{{Positions: make([]math3d.Vec3, 3)}}}, false},
		{"wrong target normal count", render3d.MorphMesh{Base: base, Targets: []render3d.MorphTarget{{Normals: make([]math3d.Vec3, 2)}}}, false},
		{"nan target position", render3d.MorphMesh{Base: base, Targets: []render3d.MorphTarget{{Positions: []math3d.Vec3{{X: nan}, {}, {}, {}}}}}, false},
		{"inf target normal", render3d.MorphMesh{Base: base, Targets: []render3d.MorphTarget{{Normals: []math3d.Vec3{{}, {Y: inf}, {}, {}}}}}, false},
		{"wrong initial weight count", render3d.MorphMesh{Base: base, Targets: []render3d.MorphTarget{{}}, Weights: []float32{0.5, 0.5}}, false},
		{"nan initial weight", render3d.MorphMesh{Base: base, Targets: []render3d.MorphTarget{{}}, Weights: []float32{nan}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mesh.Validate()
			if tt.ok && err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
			if !tt.ok && !errors.Is(err, render3d.ErrInvalidMesh) {
				t.Fatalf("Validate = %v, want ErrInvalidMesh", err)
			}
		})
	}

	if _, err := render3d.UploadMorphMesh(nil, nil, render3d.MorphMesh{Base: base}, render3d.MeshUpload{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("UploadMorphMesh(nil dev) = %v, want ErrDeviceLost", err)
	}

	gm, err := render3d.UploadMorphMesh(nil, dev, render3d.MorphMesh{
		Base:    base,
		Targets: []render3d.MorphTarget{{Positions: make([]math3d.Vec3, 4)}},
		Weights: []float32{0.25},
	}, render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMorphMesh: %v", err)
	}

	// Invalid SetWeights calls must wrap ErrInvalidMesh and leave previous state intact.
	if err := gm.SetWeights([]float32{0.1, 0.2}); !errors.Is(err, render3d.ErrInvalidMesh) {
		t.Fatalf("SetWeights(too many) = %v, want ErrInvalidMesh", err)
	}
	if err := gm.SetWeights([]float32{nan}); !errors.Is(err, render3d.ErrInvalidMesh) {
		t.Fatalf("SetWeights(NaN) = %v, want ErrInvalidMesh", err)
	}
	if err := gm.SetWeights([]float32{inf}); !errors.Is(err, render3d.ErrInvalidMesh) {
		t.Fatalf("SetWeights(Inf) = %v, want ErrInvalidMesh", err)
	}
	if gm.Dirty() {
		t.Error("invalid SetWeights marked mesh dirty")
	}
	if w := gm.Weights(nil); len(w) != 1 || w[0] != 0.25 {
		t.Fatalf("weights after invalid SetWeights = %v, want [0.25]", w)
	}

	// Close & nil receiver safety.
	if err := gm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if gm.Valid() || gm.Update(dev) {
		t.Error("closed morph mesh must be invalid and skip Update")
	}
	if err := gm.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	var nilMorph *render3d.GPUMorphMesh
	if !errors.Is(nilMorph.SetWeights(nil), render3d.ErrInvalidMesh) {
		t.Error("nil SetWeights must wrap ErrInvalidMesh")
	}
	if nilMorph.Valid() || nilMorph.Dirty() || nilMorph.Update(dev) || nilMorph.ByteSize() != 0 || nilMorph.TargetCount() != 0 || !nilMorph.Bounds().Empty() || nilMorph.Close() != nil {
		t.Error("nil morph mesh accessors violated zero-value safety")
	}
}
