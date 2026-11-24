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

// cpuRefSkinnedWGSL shades a CPU-skinned render3d.Mesh with the exact same
// fragment formula as render3d.SkinnedMeshWGSL so GPU-skinned and CPU-skinned
// renders can be compared pixel for pixel.
const cpuRefSkinnedWGSL = render3d.CameraWGSL + `
struct VertexIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

struct VertexOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) world: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}

@vertex
fn vs_main(in: VertexIn) -> VertexOut {
	var out: VertexOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	out.world = in.pos;
	out.normal = in.normal;
	out.uv = in.uv;
	return out;
}

@fragment
fn fs_main(in: VertexOut) -> @location(0) vec4<f32> {
	var n = vec3<f32>(0.0, 1.0, 0.0);
	if (dot(in.normal, in.normal) > 1e-12) {
		n = normalize(in.normal);
	}
	let v = normalize(camera.eye.xyz - in.world * camera.eye.w);
	let ndotl = max(dot(n, v), 0.0);
	let shade = 0.25 + 0.75 * ndotl;
	let base = vec3<f32>(0.55 + 0.25 * n.x, 0.55 + 0.25 * n.y, 0.6 + 0.25 * n.z);
	return vec4<f32>(base * shade, 1.0);
}
`

var skinClear = color.RGBA{R: 16, G: 20, B: 28, A: 255}

func loadGLTFSkinnedFixture(t *testing.T, relPath string) (*gltf.Document, int, render3d.SkinnedMesh) {
	t.Helper()
	fsPath := filepath.FromSlash(relPath)
	doc, err := gltf.LoadFS(os.DirFS(filepath.Dir(fsPath)), filepath.Base(fsPath))
	if err != nil {
		t.Fatalf("LoadFS(%s): %v", relPath, err)
	}
	nodeIdx := -1
	for i, n := range doc.Nodes {
		if n.Mesh >= 0 && n.Skin >= 0 {
			nodeIdx = i
			break
		}
	}
	if nodeIdx < 0 {
		t.Fatalf("%s has no skinned mesh node", relPath)
	}
	meshes, err := render3d.SkinnedMeshesFromGLTFNode(doc, nodeIdx)
	if err != nil {
		t.Fatalf("SkinnedMeshesFromGLTFNode(%s): %v", relPath, err)
	}
	if len(meshes) == 0 {
		t.Fatalf("%s yielded no skinned meshes", relPath)
	}
	return doc, doc.Nodes[nodeIdx].Skin, meshes[0]
}

// cpuSkinMesh transforms sm on the CPU via render3d.SkinVertex and returns a static Mesh and its world AABB.
func cpuSkinMesh(sm render3d.SkinnedMesh, joints []math3d.Mat4) (render3d.Mesh, math3d.AABB) {
	n := len(sm.Positions)
	pos := make([]math3d.Vec3, n)
	var nrm []math3d.Vec3
	hasNormals := len(sm.Normals) == n
	if hasNormals {
		nrm = make([]math3d.Vec3, n)
	}
	bounds := math3d.AABBEmpty()
	for i := range n {
		var inN math3d.Vec3
		if hasNormals {
			inN = sm.Normals[i]
		}
		sp, sn := render3d.SkinVertex(sm.Positions[i], inN, sm.Joints[i], sm.Weights[i], joints)
		pos[i] = sp
		if hasNormals {
			nrm[i] = sn
		}
		bounds = bounds.ExpandPoint(sp)
	}
	return render3d.Mesh{
		Positions: pos,
		Normals:   nrm,
		UVs:       sm.UVs,
		Indices:   sm.Indices,
	}, bounds
}

// frameCameraForBounds places a perspective camera looking squarely at bounds from the +Z front.
func frameCameraForBounds(bounds math3d.AABB, width, height int) *scene3d.Camera {
	center := bounds.Center()
	size := bounds.Size()
	radius := size.Len() * 0.5
	if radius < 0.1 {
		radius = 1
	}
	dist := radius * 2.1
	eye := center.Add(math3d.Vec3{X: radius * 0.25, Y: radius * 0.1, Z: dist})
	cam := scene3d.NewPerspectiveCamera(eye, math3d.DegToRad(45), float32(width)/float32(height), 0.05, dist*10)
	cam.LookAt(center, math3d.Vec3Up())
	cam.SetViewport(float32(width), float32(height))
	return cam
}

// renderCPUSkinnedReference renders a CPU-skinned render3d.Mesh with cpuRefSkinnedWGSL on a software renderer.
func renderCPUSkinnedReference(t *testing.T, width, height int, m render3d.Mesh, cam *scene3d.Camera) *image.RGBA {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	gm, err := render3d.UploadMesh(nil, dev, m, render3d.MeshUpload{Label: "cpu-ref"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = gm.Close() })
	mod, err := gpu.CompileWGSL(cpuRefSkinnedWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL cpuRefSkinnedWGSL: %v", err)
	}
	pipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Label:         "cpu-ref-skinned",
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{render3d.CameraGroup: camBuf.Layout()},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, FrontFace: gpu.FrontCCW, CullMode: gpu.CullBack},
		DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
		Targets:       []gpu.ColorTargetState{{Format: dev.SurfaceFormat(), WriteMask: gpu.ColorWriteAll}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline cpu-ref: %v", err)
	}
	t.Cleanup(func() { _ = pipe.Close() })

	camBuf.WriteCamera(cam)
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	gm.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer cpu-ref: %v", err)
	}
	return sr.Image()
}

// renderGPUSkinnedInView mounts a gpu.View, uploads sm and joints, renders one frame, and reads back pixels.
func renderGPUSkinnedInView(t *testing.T, width, height int, sm render3d.SkinnedMesh, joints []math3d.Mat4, cam *scene3d.Camera, opts ...nsgotest.Option) *image.RGBA {
	t.Helper()
	var (
		camBuf  *render3d.CameraBuffer
		palette *render3d.GPUSkinPalette
		mesh    *render3d.GPUSkinnedMesh
		pipe    *gpu.Pipeline
		frames  int
	)
	allOpts := append([]nsgotest.Option{nsgotest.Size(core.Points(width), core.Points(height)), nsgotest.ManualFrames()}, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "skin-viewport",
			Style:       core.Style{Width: core.Pt(core.Points(width)), Height: core.Pt(core.Points(height))},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, dev)
				if err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				palette, err = render3d.NewGPUSkinPalette(cx, dev, render3d.SkinPaletteOptions{Label: "test-skin"})
				if err != nil {
					t.Fatalf("NewGPUSkinPalette: %v", err)
				}
				if err := palette.Update(dev, joints); err != nil {
					t.Fatalf("palette.Update: %v", err)
				}
				mesh, err = render3d.UploadSkinnedMesh(cx, dev, sm, render3d.MeshUpload{Label: "test-mesh"})
				if err != nil {
					t.Fatalf("UploadSkinnedMesh: %v", err)
				}
				pipe, err = render3d.NewSkinnedMeshPipeline(cx, dev, camBuf.Layout(), palette.Layout(), render3d.SkinnedMeshPipelineOptions{
					Label:       "skinned",
					DepthFormat: gpu.FormatDepth24Plus,
				})
				if err != nil {
					t.Fatalf("NewSkinnedMeshPipeline: %v", err)
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				frames++
				camBuf.WriteCamera(cam)
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
				pass.SetBindGroup(render3d.SkinGroup, palette.BindGroup())
				mesh.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, allOpts...)

	before := gpuDraws(app)
	app.Advance(16 * time.Millisecond)
	if frames == 0 {
		t.Fatal("OnFrame did not run")
	}
	if gpuDraws(app) == before {
		t.Fatal("no draw call was recorded")
	}
	img, err := app.GPUReadPixels("id:skin-viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	return img
}

func countDifferingPixels(a, b *image.RGBA, tol uint8) int {
	bounds := a.Bounds()
	diff := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			ca := a.RGBAAt(x, y)
			cb := b.RGBAAt(x, y)
			if absDiffU8(ca.R, cb.R) > tol || absDiffU8(ca.G, cb.G) > tol || absDiffU8(ca.B, cb.B) > tol || absDiffU8(ca.A, cb.A) > tol {
				diff++
			}
		}
	}
	return diff
}

func TestCesiumManBindPoseAndPosedGPUParity(t *testing.T) {
	doc, skinIdx, sm := loadGLTFSkinnedFixture(t, "../gltf/testdata/khronos/CesiumMan/glTF/CesiumMan.gltf")
	if sm.JointCount != 19 {
		t.Fatalf("CesiumMan JointCount = %d, want 19", sm.JointCount)
	}
	if sm.VertexCount() != 3273 || len(sm.Indices) != 14016 {
		t.Fatalf("CesiumMan counts = %d verts, %d indices, want 3273 and 14016", sm.VertexCount(), len(sm.Indices))
	}

	// Bind pose: initial glTF node hierarchy transforms applied through SkinJointMatrices.
	if err := doc.ComputeWorldMatrices(); err != nil {
		t.Fatalf("ComputeWorldMatrices: %v", err)
	}
	bindJoints, err := render3d.SkinJointMatrices(doc, skinIdx, nil)
	if err != nil {
		t.Fatalf("SkinJointMatrices(bind): %v", err)
	}
	cpuBindMesh, bindBounds := cpuSkinMesh(sm, bindJoints)
	size := bindBounds.Size()
	// CesiumMan stands upright in Y-up glTF world space (~1.6m tall, taller in Y than X or Z).
	if size.Y < 1.2 || size.Y <= size.X || size.Y <= size.Z {
		t.Fatalf("CesiumMan bind-pose world bounds not upright Y-up: min=%v max=%v size=%v", bindBounds.Min, bindBounds.Max, size)
	}

	const width, height = 96, 96
	cam := frameCameraForBounds(bindBounds, width, height)
	cpuBindImg := renderCPUSkinnedReference(t, width, height, cpuBindMesh, cam)
	bindCoverage := countNonClearPixels(cpuBindImg, skinClear)
	if bindCoverage < 400 {
		t.Fatalf("CesiumMan bind-pose rendered too few pixels: %d", bindCoverage)
	}

	// Verify GPU skinning matches CPU reference on both full and WebGL 2 / GLES 3.0 profiles.
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	var bindGPUImg *image.RGBA
	for _, p := range profiles {
		t.Run("bind_pose/"+p.name, func(t *testing.T) {
			gpuImg := renderGPUSkinnedInView(t, width, height, sm, bindJoints, cam, p.opts...)
			if p.name == "full" {
				bindGPUImg = gpuImg
			}
			wantPixel(t, gpuImg, 2, 2, skinClear)
			wantPixel(t, gpuImg, width-3, 2, skinClear)
			if cov := countNonClearPixels(gpuImg, skinClear); cov < 400 {
				t.Fatalf("GPU bind-pose coverage = %d, want >= 400", cov)
			}
			// Projected torso center must land on shaded geometry, not background clear.
			s, _, vis := cam.WorldToScreen(bindBounds.Center(), width, height)
			if !vis {
				t.Fatalf("bindBounds.Center() not visible")
			}
			c := gpuImg.RGBAAt(int(s.X), int(s.Y))
			if absDiffU8(c.R, skinClear.R) <= 3 && absDiffU8(c.G, skinClear.G) <= 3 && absDiffU8(c.B, skinClear.B) <= 3 {
				t.Errorf("center pixel (%d,%d) = %v, expected shaded CesiumMan torso", int(s.X), int(s.Y), c)
			}
			// Pixel-for-pixel agreement with CPU SkinVertex reference.
			diff := countDifferingPixels(gpuImg, cpuBindImg, 3)
			if maxAllowed := width * height / 200; diff > maxAllowed {
				t.Errorf("GPU vs CPU bind-pose render differed in %d pixels (limit %d)", diff, maxAllowed)
			}
		})
	}

	// Posed/animated frame: sample walk cycle mid-stride and confirm both deformation and GPU/CPU parity.
	if len(doc.Animations) == 0 {
		t.Fatal("CesiumMan missing walk animation")
	}
	doc.Animations[0].Apply(doc, 0.6)
	if err := doc.ComputeWorldMatrices(); err != nil {
		t.Fatalf("ComputeWorldMatrices(posed): %v", err)
	}
	posedJoints, err := render3d.SkinJointMatrices(doc, skinIdx, nil)
	if err != nil {
		t.Fatalf("SkinJointMatrices(posed): %v", err)
	}
	cpuPosedMesh, _ := cpuSkinMesh(sm, posedJoints)
	cpuPosedImg := renderCPUSkinnedReference(t, width, height, cpuPosedMesh, cam)

	for _, p := range profiles {
		t.Run("posed/"+p.name, func(t *testing.T) {
			gpuPosedImg := renderGPUSkinnedInView(t, width, height, sm, posedJoints, cam, p.opts...)
			diffVsCPU := countDifferingPixels(gpuPosedImg, cpuPosedImg, 3)
			if maxAllowed := width * height / 200; diffVsCPU > maxAllowed {
				t.Errorf("GPU vs CPU posed render differed in %d pixels (limit %d)", diffVsCPU, maxAllowed)
			}
			if bindGPUImg != nil {
				diffVsBind := countDifferingPixels(gpuPosedImg, bindGPUImg, 4)
				if diffVsBind < 60 {
					t.Errorf("posed frame too close to bind pose: only %d differing pixels", diffVsBind)
				}
			}
		})
	}
}

func TestRiggedSimpleBindPoseAndAnimatedBend(t *testing.T) {
	doc, skinIdx, sm := loadGLTFSkinnedFixture(t, "../gltf/testdata/khronos/RiggedSimple/glTF/RiggedSimple.gltf")
	if sm.JointCount != 2 {
		t.Fatalf("RiggedSimple JointCount = %d, want 2", sm.JointCount)
	}
	if err := doc.ComputeWorldMatrices(); err != nil {
		t.Fatalf("ComputeWorldMatrices: %v", err)
	}
	bindJoints, err := render3d.SkinJointMatrices(doc, skinIdx, nil)
	if err != nil {
		t.Fatalf("SkinJointMatrices: %v", err)
	}
	cpuBindMesh, bindBounds := cpuSkinMesh(sm, bindJoints)
	const width, height = 64, 64
	cam := frameCameraForBounds(bindBounds, width, height)
	cpuBindImg := renderCPUSkinnedReference(t, width, height, cpuBindMesh, cam)

	// Pose RiggedSimple at t=1.0 where Bone.001 bends the cylinder.
	doc.Animations[0].Apply(doc, 1.0)
	if err := doc.ComputeWorldMatrices(); err != nil {
		t.Fatalf("ComputeWorldMatrices(bent): %v", err)
	}
	bentJoints, err := render3d.SkinJointMatrices(doc, skinIdx, nil)
	if err != nil {
		t.Fatalf("SkinJointMatrices(bent): %v", err)
	}
	cpuBentMesh, _ := cpuSkinMesh(sm, bentJoints)
	cpuBentImg := renderCPUSkinnedReference(t, width, height, cpuBentMesh, cam)

	for _, p := range []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	} {
		t.Run(p.name, func(t *testing.T) {
			gpuBind := renderGPUSkinnedInView(t, width, height, sm, bindJoints, cam, p.opts...)
			gpuBent := renderGPUSkinnedInView(t, width, height, sm, bentJoints, cam, p.opts...)

			if d := countDifferingPixels(gpuBind, cpuBindImg, 3); d > width*height/200 {
				t.Errorf("RiggedSimple bind GPU vs CPU differed in %d pixels", d)
			}
			if d := countDifferingPixels(gpuBent, cpuBentImg, 3); d > width*height/200 {
				t.Errorf("RiggedSimple bent GPU vs CPU differed in %d pixels", d)
			}
			if d := countDifferingPixels(gpuBind, gpuBent, 4); d < 40 {
				t.Errorf("RiggedSimple bent pose did not visibly deform: %d differing pixels", d)
			}
		})
	}
}

func TestRetainedSkinDrawsAfterDeviceLoss(t *testing.T) {
	// A simple skinned quad moved to the right (+X) by joint 0, then after restore moved to the left (-X).
	sm := skinnedQuadMesh()
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 5}, 4, 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	cam.SetViewport(64, 64)

	rightPose := []math3d.Mat4{math3d.Translate(math3d.Vec3{X: 1.0})}
	leftPose := []math3d.Mat4{math3d.Translate(math3d.Vec3{X: -1.0})}

	var (
		dev     *gpu.Device
		camBuf  *render3d.CameraBuffer
		palette *render3d.GPUSkinPalette
		mesh    *render3d.GPUSkinnedMesh
		pipe    *gpu.Pipeline
		frames  int
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "skin-loss",
			Style:       core.Style{Width: core.Pt(64), Height: core.Pt(64)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(d *gpu.Device) {
				dev = d
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, d)
				if err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				if palette == nil {
					palette, err = render3d.NewGPUSkinPalette(cx, d, render3d.SkinPaletteOptions{Label: "retained-skin", Retained: true})
					if err != nil {
						t.Fatalf("NewGPUSkinPalette: %v", err)
					}
					if err := palette.Update(d, rightPose); err != nil {
						t.Fatalf("initial palette.Update: %v", err)
					}
				}
				if mesh == nil {
					mesh, err = render3d.UploadSkinnedMesh(cx, d, sm, render3d.MeshUpload{Label: "retained-quad", Retained: true})
					if err != nil {
						t.Fatalf("UploadSkinnedMesh: %v", err)
					}
				}
				pipe, err = render3d.NewSkinnedMeshPipeline(cx, d, camBuf.Layout(), palette.Layout(), render3d.SkinnedMeshPipelineOptions{
					Label:       "retained-pipe",
					DepthFormat: gpu.FormatDepth24Plus,
				})
				if err != nil {
					t.Fatalf("NewSkinnedMeshPipeline: %v", err)
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				frames++
				camBuf.WriteCamera(cam)
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
				pass.SetBindGroup(render3d.SkinGroup, palette.BindGroup())
				mesh.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(64, 64), nsgotest.ManualFrames())

	// Frame 1: quad is on the right side (x ~ 48), left side (x ~ 16) is clear.
	app.Advance(16 * time.Millisecond)
	img1, err := app.GPUReadPixels("id:skin-loss")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	wantPixel(t, img1, 16, 32, skinClear)
	if c := img1.RGBAAt(48, 32); c == skinClear {
		t.Fatalf("before loss: right side (48,32) = %v, want shaded quad", c)
	}

	firstMesh, firstPalette := mesh, palette
	app.TriggerGPUDeviceLost("id:skin-loss", nsgotest.GPULostContextLost, "context lost")
	if firstMesh.Valid() || firstPalette.Valid() {
		t.Fatalf("retained resources still valid while device lost: mesh=%v palette=%v", firstMesh.Valid(), firstPalette.Valid())
	}

	app.RestoreGPUDevice("id:skin-loss")
	if mesh != firstMesh || !firstMesh.Valid() {
		t.Fatalf("retained skinned mesh not restored: same=%v valid=%v", mesh == firstMesh, firstMesh.Valid())
	}
	if palette != firstPalette || !firstPalette.Valid() {
		t.Fatalf("retained skin palette not restored: same=%v valid=%v", palette == firstPalette, firstPalette.Valid())
	}

	// Frame 2: restored buffer still holds rightPose automatically.
	app.Advance(16 * time.Millisecond)
	img2, err := app.GPUReadPixels("id:skin-loss")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	wantPixel(t, img2, 16, 32, skinClear)
	if c := img2.RGBAAt(48, 32); c == skinClear {
		t.Fatalf("after restore: right side (48,32) = %v, want restored rightPose quad", c)
	}

	// Frame 3: updating palette after restore moves quad to the left side.
	app.Post(func() {
		if err := palette.Update(dev, leftPose); err != nil {
			t.Errorf("post-restore palette.Update: %v", err)
		}
	})
	app.Settle()
	app.Advance(16 * time.Millisecond)
	img3, err := app.GPUReadPixels("id:skin-loss")
	if err != nil {
		t.Fatalf("GPUReadPixels after post-restore update: %v", err)
	}
	wantPixel(t, img3, 48, 32, skinClear)
	if c := img3.RGBAAt(16, 32); c == skinClear {
		t.Fatalf("after post-restore update: left side (16,32) = %v, want shaded quad", c)
	}
}

func TestSkinScopeDisposalReleasesResources(t *testing.T) {
	var (
		mesh    *render3d.GPUSkinnedMesh
		palette *render3d.GPUSkinPalette
		show    *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(dev *gpu.Device) {
					var err error
					mesh, err = render3d.UploadSkinnedMesh(cx, dev, skinnedQuadMesh(), render3d.MeshUpload{})
					if err != nil {
						t.Fatalf("UploadSkinnedMesh: %v", err)
					}
					palette, err = render3d.NewGPUSkinPalette(cx, dev, render3d.SkinPaletteOptions{})
					if err != nil {
						t.Fatalf("NewGPUSkinPalette: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())
	if !mesh.Valid() || !palette.Valid() {
		t.Fatal("skinned mesh or palette not valid while mounted")
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if mesh.Valid() || palette.Valid() {
		t.Errorf("resources still valid after scope disposal: mesh=%v palette=%v", mesh.Valid(), palette.Valid())
	}
}

func TestSkinVertexMultiInfluenceAndNormalization(t *testing.T) {
	m0 := math3d.Translate(math3d.Vec3{X: 2, Y: 0, Z: 0})
	m1 := math3d.Translate(math3d.Vec3{X: 0, Y: 4, Z: 0}).Mul(math3d.RotateZ(math.Pi / 2))
	m2 := math3d.Translate(math3d.Vec3{X: 0, Y: 0, Z: 6})
	m3 := math3d.Translate(math3d.Vec3{X: -2, Y: -2, Z: -2})
	mats := []math3d.Mat4{m0, m1, m2, m3}

	pos := math3d.Vec3{X: 1, Y: 0, Z: 0}
	nrm := math3d.Vec3{X: 1, Y: 0, Z: 0}
	joints := [4]uint16{0, 1, 2, 3}
	// Unnormalized weights (4, 3, 2, 1) -> normalized (0.4, 0.3, 0.2, 0.1).
	sp, sn := render3d.SkinVertex(pos, nrm, joints, math3d.Vec4{X: 4, Y: 3, Z: 2, W: 1}, mats)

	// Hand-calculated expected position:
	// m0*p = (3, 0, 0) * 0.4 = (1.2, 0, 0)
	// m1*p = (0, 5, 0) * 0.3 = (0, 1.5, 0)
	// m2*p = (1, 0, 6) * 0.2 = (0.2, 0, 1.2)
	// m3*p = (-1, -2, -2) * 0.1 = (-0.1, -0.2, -0.2)
	// sum  = (1.3, 1.3, 1.0)
	wantP := math3d.Vec3{X: 1.3, Y: 1.3, Z: 1.0}
	if !sp.ApproxEqual(wantP, 1e-5) {
		t.Errorf("SkinVertex pos = %v, want %v", sp, wantP)
	}
	// Hand-calculated expected normal:
	// m0*n = (1,0,0)*0.4, m1*n = (0,1,0)*0.3, m2*n = (1,0,0)*0.2, m3*n = (1,0,0)*0.1 -> (0.7, 0.3, 0) normalized.
	wantN := math3d.Vec3{X: 0.7, Y: 0.3, Z: 0}.Norm()
	if !sn.ApproxEqual(wantN, 1e-5) {
		t.Errorf("SkinVertex normal = %v, want %v", sn, wantN)
	}

	// Zero-sum weights fall back to (1, 0, 0, 0) attached rigidly to joints[0].
	spZero, _ := render3d.SkinVertex(pos, nrm, [4]uint16{2, 0, 0, 0}, math3d.Vec4{}, mats)
	if want := m2.TransformPoint(pos); !spZero.ApproxEqual(want, 1e-6) {
		t.Errorf("zero-weight fallback pos = %v, want %v", spZero, want)
	}

	// Out-of-palette joint index falls back to identity matrix.
	spPad, _ := render3d.SkinVertex(pos, nrm, [4]uint16{99, 0, 0, 0}, math3d.Vec4{X: 1}, mats)
	if !spPad.ApproxEqual(pos, 1e-6) {
		t.Errorf("padded joint index pos = %v, want %v", spPad, pos)
	}

	// Zero normal stays zero (no NaN).
	_, snZero := render3d.SkinVertex(pos, math3d.Vec3Zero(), joints, math3d.Vec4{X: 1}, mats)
	if snZero != math3d.Vec3Zero() {
		t.Errorf("zero normal became %v, want zero", snZero)
	}
}

func TestSkinnedMeshValidateAndErrors(t *testing.T) {
	valid := skinnedQuadMesh()
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	tests := []struct {
		name    string
		mutate  func(*render3d.SkinnedMesh)
		wantErr error
	}{
		{"valid", func(*render3d.SkinnedMesh) {}, nil},
		{"empty positions", func(m *render3d.SkinnedMesh) { m.Positions = nil }, render3d.ErrInvalidMesh},
		{"mismatched normals", func(m *render3d.SkinnedMesh) { m.Normals = m.Normals[:1] }, render3d.ErrInvalidMesh},
		{"index out of range", func(m *render3d.SkinnedMesh) { m.Indices = []uint32{0, 1, 9} }, render3d.ErrInvalidMesh},
		{"mismatched joints", func(m *render3d.SkinnedMesh) { m.Joints = m.Joints[:2] }, render3d.ErrInvalidSkin},
		{"mismatched weights", func(m *render3d.SkinnedMesh) { m.Weights = m.Weights[:2] }, render3d.ErrInvalidSkin},
		{"negative joint count", func(m *render3d.SkinnedMesh) { m.JointCount = -1 }, render3d.ErrInvalidSkin},
		{"joint count exceeds max", func(m *render3d.SkinnedMesh) { m.JointCount = render3d.MaxSkinJoints + 1 }, render3d.ErrInvalidSkin},
		{"joint index >= JointCount", func(m *render3d.SkinnedMesh) {
			m.JointCount = 2
			m.Joints[0] = [4]uint16{0, 2, 0, 0}
		}, render3d.ErrInvalidSkin},
		{"joint index >= MaxSkinJoints when JointCount==0", func(m *render3d.SkinnedMesh) {
			m.JointCount = 0
			m.Joints[0] = [4]uint16{render3d.MaxSkinJoints, 0, 0, 0}
		}, render3d.ErrInvalidSkin},
		{"negative weight", func(m *render3d.SkinnedMesh) {
			m.Weights[1] = math3d.Vec4{X: 1, Y: -0.1}
		}, render3d.ErrInvalidSkin},
		{"nan weight", func(m *render3d.SkinnedMesh) {
			m.Weights[0] = math3d.Vec4{X: nan}
		}, render3d.ErrInvalidSkin},
		{"inf weight", func(m *render3d.SkinnedMesh) {
			m.Weights[0] = math3d.Vec4{X: inf}
		}, render3d.ErrInvalidSkin},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := cloneSkinnedMesh(valid)
			tt.mutate(&m)
			err := m.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestSkinnedMeshFromGLTFValidation(t *testing.T) {
	prim := gltf.Primitive{
		Mode:      gltf.ModeTriangles,
		Positions: []math3d.Vec3{{}, {X: 1}, {Y: 1}},
		Joints0:   [][4]uint16{{0, 0, 0, 0}, {0, 1, 0, 0}, {1, 0, 0, 0}},
		Weights0:  []math3d.Vec4{{X: 2}, {X: 3, Y: 1}, {X: 0, Y: 0}},
	}
	skin := gltf.Skin{Joints: []int{0, 1}}

	sm, err := render3d.SkinnedMeshFromGLTF(prim, skin)
	if err != nil {
		t.Fatalf("SkinnedMeshFromGLTF: %v", err)
	}
	if !sm.Weights[0].ApproxEqual(math3d.Vec4{X: 1}, 1e-6) ||
		!sm.Weights[1].ApproxEqual(math3d.Vec4{X: 0.75, Y: 0.25}, 1e-6) ||
		!sm.Weights[2].ApproxEqual(math3d.Vec4{X: 1}, 1e-6) {
		t.Errorf("normalized weights = %v", sm.Weights)
	}
	if sm.JointCount != 2 {
		t.Errorf("JointCount = %d, want 2", sm.JointCount)
	}

	// Non-triangle primitive rejected as ErrInvalidMesh.
	linePrim := prim
	linePrim.Mode = gltf.ModeLines
	if _, err := render3d.SkinnedMeshFromGLTF(linePrim, skin); !errors.Is(err, render3d.ErrInvalidMesh) {
		t.Errorf("line mode err = %v, want ErrInvalidMesh", err)
	}
	// Empty skin or oversized skin rejected as ErrInvalidSkin.
	if _, err := render3d.SkinnedMeshFromGLTF(prim, gltf.Skin{}); !errors.Is(err, render3d.ErrInvalidSkin) {
		t.Errorf("empty skin err = %v, want ErrInvalidSkin", err)
	}
	bigSkin := gltf.Skin{Joints: make([]int, render3d.MaxSkinJoints+1)}
	if _, err := render3d.SkinnedMeshFromGLTF(prim, bigSkin); !errors.Is(err, render3d.ErrInvalidSkin) {
		t.Errorf("oversized skin err = %v, want ErrInvalidSkin", err)
	}
	// Out-of-bounds joint in primitive rejected.
	badJointPrim := prim
	badJointPrim.Joints0 = [][4]uint16{{0, 0, 0, 0}, {2, 0, 0, 0}, {0, 0, 0, 0}}
	if _, err := render3d.SkinnedMeshFromGLTF(badJointPrim, skin); !errors.Is(err, render3d.ErrInvalidSkin) {
		t.Errorf("out-of-range joint err = %v, want ErrInvalidSkin", err)
	}
	// Nil document and invalid node indices.
	if _, err := render3d.SkinnedMeshesFromGLTFNode(nil, 0); !errors.Is(err, render3d.ErrInvalidSkin) {
		t.Errorf("nil doc err = %v, want ErrInvalidSkin", err)
	}
}

func TestSkinnedVertexLayoutAndEncoding(t *testing.T) {
	l := render3d.SkinnedVertexLayout()
	if l.Stride != render3d.SkinnedVertexStride || render3d.SkinnedVertexStride != 64 || l.StepMode != gpu.StepVertex || len(l.Attributes) != 5 {
		t.Fatalf("layout = %+v", l)
	}
	want := []gpu.VertexAttribute{
		{Format: gpu.VertexFloat32x3, Offset: 0, Location: 0},
		{Format: gpu.VertexFloat32x3, Offset: 12, Location: 1},
		{Format: gpu.VertexFloat32x2, Offset: 24, Location: 2},
		{Format: gpu.VertexUint32x4, Offset: 32, Location: 3},
		{Format: gpu.VertexFloat32x4, Offset: 48, Location: 4},
	}
	for i := range want {
		if l.Attributes[i] != want[i] {
			t.Errorf("attribute %d = %+v, want %+v", i, l.Attributes[i], want[i])
		}
	}
	l.Attributes[0].Location = 9
	if render3d.SkinnedVertexLayout().Attributes[0].Location != 0 {
		t.Error("SkinnedVertexLayout shares Attributes slice across calls")
	}

	m := render3d.SkinnedMesh{
		Positions: []math3d.Vec3{{X: 1, Y: 2, Z: 3}, {X: -1, Y: -2, Z: -3}, {X: 0, Y: 1, Z: 0}},
		Normals:   []math3d.Vec3{{Z: 1}, {Y: 1}, {X: 1}},
		UVs:       []math3d.Vec2{{X: 0.25, Y: 0.75}, {X: 1, Y: 0}, {X: 0.5, Y: 0.5}},
		Joints:    [][4]uint16{{0, 3, 7, 127}, {1, 2, 0, 0}, {5, 6, 7, 8}},
		Weights:   []math3d.Vec4{{X: 2, Y: 2, Z: 0, W: 0}, {X: 0, Y: 0, Z: 0, W: 0}, {X: 0.25, Y: 0.25, Z: 0.25, W: 0.25}},
	}
	raw := m.AppendVertexData([]byte{0x5A})
	if len(raw) != 1+m.VertexDataSize() || raw[0] != 0x5A {
		t.Fatalf("encoded len = %d, want %d", len(raw), 1+m.VertexDataSize())
	}
	v0 := raw[1 : 1+64]
	if got := binary.LittleEndian.Uint32(v0[32:]); got != 0 {
		t.Errorf("v0 joint.x = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(v0[36:]); got != 3 {
		t.Errorf("v0 joint.y = %d, want 3", got)
	}
	if got := binary.LittleEndian.Uint32(v0[44:]); got != 127 {
		t.Errorf("v0 joint.w = %d, want 127", got)
	}
	w0x := math.Float32frombits(binary.LittleEndian.Uint32(v0[48:]))
	w0y := math.Float32frombits(binary.LittleEndian.Uint32(v0[52:]))
	if !math3d.ApproxEqual(w0x, 0.5, 1e-6) || !math3d.ApproxEqual(w0y, 0.5, 1e-6) {
		t.Errorf("v0 normalized weights = (%v, %v), want (0.5, 0.5)", w0x, w0y)
	}
	// Vertex 1 had all-zero weights -> normalized to (1, 0, 0, 0).
	v1 := raw[1+64 : 1+128]
	w1x := math.Float32frombits(binary.LittleEndian.Uint32(v1[48:]))
	if w1x != 1 {
		t.Errorf("v1 zero-weight fallback w.x = %v, want 1", w1x)
	}
}

func TestAppendJointMatricesLayoutAndErrors(t *testing.T) {
	if render3d.SkinUniformSize != 8192 || render3d.SkinUniformSize > 16384 {
		t.Fatalf("SkinUniformSize = %d, want 8192 (<= 16 KiB WebGL 2 / GLES 3 limit)", render3d.SkinUniformSize)
	}
	m0 := math3d.Translate(math3d.Vec3{X: 3, Y: 4, Z: 5})
	m1 := math3d.Scale(math3d.Vec3{X: 2, Y: 2, Z: 2})
	data, err := render3d.AppendJointMatrices(nil, []math3d.Mat4{m0, m1})
	if err != nil {
		t.Fatalf("AppendJointMatrices: %v", err)
	}
	if len(data) != render3d.SkinUniformSize {
		t.Fatalf("len = %d, want %d", len(data), render3d.SkinUniformSize)
	}
	readMat := func(slot int) math3d.Mat4 {
		var out math3d.Mat4
		off := slot * 64
		for i := range 16 {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[off+i*4:]))
		}
		return out
	}
	if got := readMat(0); got != m0 {
		t.Errorf("slot 0 = %v, want %v", got, m0)
	}
	if got := readMat(1); got != m1 {
		t.Errorf("slot 1 = %v, want %v", got, m1)
	}
	// Remaining slots padded with identity.
	if got := readMat(2); got != math3d.Mat4Identity() {
		t.Errorf("slot 2 = %v, want identity", got)
	}
	if got := readMat(render3d.MaxSkinJoints - 1); got != math3d.Mat4Identity() {
		t.Errorf("last slot = %v, want identity", got)
	}

	// Over-limit joints rejected.
	tooMany := make([]math3d.Mat4, render3d.MaxSkinJoints+1)
	if _, err := render3d.AppendJointMatrices(nil, tooMany); !errors.Is(err, render3d.ErrInvalidSkin) {
		t.Errorf("tooMany err = %v, want ErrInvalidSkin", err)
	}
	// Non-finite matrix entry rejected.
	bad := math3d.Mat4Identity()
	bad[12] = float32(math.NaN())
	if _, err := render3d.AppendJointMatrices(nil, []math3d.Mat4{bad}); !errors.Is(err, render3d.ErrInvalidSkin) {
		t.Errorf("NaN matrix err = %v, want ErrInvalidSkin", err)
	}
}

func TestSkinnedMeshAndPaletteZeroAllocs(t *testing.T) {
	doc, skinIdx, sm := loadGLTFSkinnedFixture(t, "../gltf/testdata/khronos/CesiumMan/glTF/CesiumMan.gltf")
	if err := doc.ComputeWorldMatrices(); err != nil {
		t.Fatalf("ComputeWorldMatrices: %v", err)
	}
	vbuf := make([]byte, 0, sm.VertexDataSize())
	ibuf := make([]byte, 0, sm.IndexDataSize())
	pbuf := make([]byte, 0, render3d.SkinUniformSize)
	mbuf := make([]math3d.Mat4, 0, sm.JointCount)

	var err error
	allocs := testing.AllocsPerRun(10, func() {
		vbuf = sm.AppendVertexData(vbuf[:0])
		ibuf = sm.AppendIndexData(ibuf[:0])
		mbuf, err = render3d.SkinJointMatrices(doc, skinIdx, mbuf[:0])
		if err != nil {
			t.Fatal(err)
		}
		pbuf, err = render3d.AppendJointMatrices(pbuf[:0], mbuf)
		if err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Errorf("steady-state append/skinning allocs = %v, want 0", allocs)
	}
}

func TestSkinnedMeshPaletteAndPipelineLifecycle(t *testing.T) {
	// Nil device / nil receiver safety.
	if _, err := render3d.UploadSkinnedMesh(nil, nil, skinnedQuadMesh(), render3d.MeshUpload{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("UploadSkinnedMesh(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewGPUSkinPalette(nil, nil, render3d.SkinPaletteOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewGPUSkinPalette(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewSkinnedMeshPipeline(nil, nil, nil, nil, render3d.SkinnedMeshPipelineOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewSkinnedMeshPipeline(nil dev) = %v, want ErrDeviceLost", err)
	}

	var nilMesh *render3d.GPUSkinnedMesh
	if nilMesh.Valid() || nilMesh.ByteSize() != 0 || nilMesh.Close() != nil {
		t.Error("nil GPUSkinnedMesh must be invalid, 0 bytes, and close cleanly")
	}
	var nilPalette *render3d.GPUSkinPalette
	if nilPalette.Valid() || nilPalette.ByteSize() != 0 || nilPalette.JointCount() != 0 || nilPalette.Buffer() != nil || nilPalette.Layout() != nil || nilPalette.BindGroup() != nil || nilPalette.Close() != nil {
		t.Error("nil GPUSkinPalette must be invalid, 0 bytes, and close cleanly")
	}

	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()

	palette, err := render3d.NewGPUSkinPalette(nil, dev, render3d.SkinPaletteOptions{Label: "life"})
	if err != nil {
		t.Fatalf("NewGPUSkinPalette: %v", err)
	}
	if !palette.Valid() || palette.ByteSize() != render3d.SkinUniformSize || palette.Buffer() == nil {
		t.Fatalf("palette invalid or wrong size: valid=%v size=%d", palette.Valid(), palette.ByteSize())
	}

	// Invalid layouts rejected by NewSkinnedMeshPipeline.
	if _, err := render3d.NewSkinnedMeshPipeline(nil, dev, nil, palette.Layout(), render3d.SkinnedMeshPipelineOptions{}); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("nil camLayout = %v, want ErrInvalidDescriptor", err)
	}
	if _, err := render3d.NewSkinnedMeshPipeline(nil, dev, camBuf.Layout(), nil, render3d.SkinnedMeshPipelineOptions{}); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("nil skinLayout = %v, want ErrInvalidDescriptor", err)
	}

	gm, err := render3d.UploadSkinnedMesh(nil, dev, skinnedQuadMesh(), render3d.MeshUpload{Label: "life"})
	if err != nil {
		t.Fatalf("UploadSkinnedMesh: %v", err)
	}
	if !gm.Valid() || !gm.Indexed() || gm.VertexCount() != 4 || gm.IndexCount() != 6 || gm.Triangles() != 2 || gm.JointCount() != 2 {
		t.Fatalf("unexpected GPUSkinnedMesh state: valid=%v indexed=%v verts=%d idx=%d tris=%d joints=%d",
			gm.Valid(), gm.Indexed(), gm.VertexCount(), gm.IndexCount(), gm.Triangles(), gm.JointCount())
	}
	if want := uint64(4*render3d.SkinnedVertexStride + 12); gm.ByteSize() != want {
		t.Errorf("ByteSize = %d, want %d", gm.ByteSize(), want)
	}

	// Test UpdateFromGLTFWithTransform on RiggedSimple.
	doc, skinIdx, _ := loadGLTFSkinnedFixture(t, "../gltf/testdata/khronos/RiggedSimple/glTF/RiggedSimple.gltf")
	if err := doc.ComputeWorldMatrices(); err != nil {
		t.Fatal(err)
	}
	if err := palette.UpdateFromGLTFWithTransform(dev, doc, skinIdx, math3d.Translate(math3d.Vec3{Y: 2})); err != nil {
		t.Fatalf("UpdateFromGLTFWithTransform: %v", err)
	}
	if palette.JointCount() != 2 {
		t.Errorf("palette.JointCount = %d, want 2", palette.JointCount())
	}

	// Double Close is safe and invalidates resources.
	if err := gm.Close(); err != nil {
		t.Fatalf("gm.Close: %v", err)
	}
	if gm.Valid() || gm.Close() != nil {
		t.Error("closed GPUSkinnedMesh still valid or second Close failed")
	}
	if err := palette.Close(); err != nil {
		t.Fatalf("palette.Close: %v", err)
	}
	if palette.Valid() || palette.Close() != nil {
		t.Error("closed GPUSkinPalette still valid or second Close failed")
	}
	if err := palette.Update(dev, nil); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("closed palette.Update = %v, want ErrDeviceLost", err)
	}
}

func TestSkinnedMeshNonIndexedAndDoubleSided(t *testing.T) {
	// Reversed-winding non-indexed triangle without normals or UVs.
	tri := render3d.SkinnedMesh{
		Positions: []math3d.Vec3{{X: -0.6, Y: -0.6, Z: 0}, {X: 0, Y: 0.6, Z: 0}, {X: 0.6, Y: -0.6, Z: 0}},
		Joints:    [][4]uint16{{0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}},
		Weights:   []math3d.Vec4{{X: 1}, {X: 1}, {X: 1}},
	}
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 3}, 2, 1, 0.1, 10)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	cam.SetViewport(32, 32)

	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()
	camBuf, _ := render3d.NewCameraBuffer(nil, dev)
	defer func() { _ = camBuf.Close() }()
	palette, _ := render3d.NewGPUSkinPalette(nil, dev, render3d.SkinPaletteOptions{})
	defer func() { _ = palette.Close() }()
	gm, err := render3d.UploadSkinnedMesh(nil, dev, tri, render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadSkinnedMesh: %v", err)
	}
	defer func() { _ = gm.Close() }()
	if gm.Indexed() || gm.IndexBuffer() != nil || gm.Triangles() != 1 {
		t.Fatalf("non-indexed mesh counts: indexed=%v tris=%d", gm.Indexed(), gm.Triangles())
	}

	drawWith := func(doubleSided bool) *image.RGBA {
		pipe, err := render3d.NewSkinnedMeshPipeline(nil, dev, camBuf.Layout(), palette.Layout(), render3d.SkinnedMeshPipelineOptions{
			DepthFormat: gpu.FormatDepth24Plus,
			DoubleSided: doubleSided,
		})
		if err != nil {
			t.Fatalf("NewSkinnedMeshPipeline: %v", err)
		}
		defer func() { _ = pipe.Close() }()
		camBuf.WriteCamera(cam)
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		pass.SetBindGroup(render3d.SkinGroup, palette.BindGroup())
		gm.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		return sr.Image()
	}

	// Culled when DoubleSided is false; visible when DoubleSided is true.
	if cov := countNonClearPixels(drawWith(false), skinClear); cov != 0 {
		t.Errorf("back-facing triangle drew %d pixels with CullBack, want 0", cov)
	}
	if cov := countNonClearPixels(drawWith(true), skinClear); cov < 40 {
		t.Errorf("double-sided triangle drew %d pixels, want >= 40", cov)
	}
}

func TestInvalidSkinnedMeshDrawsNothing(t *testing.T) {
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Z: 3}, 2, 1, 0.1, 10)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	cam.SetViewport(32, 32)

	var (
		camBuf         *render3d.CameraBuffer
		palette        *render3d.GPUSkinPalette
		good, goClosed *render3d.GPUSkinnedMesh
		pipe           *gpu.Pipeline
		frames         int
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "skin-invalid",
			Style:       core.Style{Width: core.Pt(32), Height: core.Pt(32)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, dev)
				if err != nil {
					t.Fatal(err)
				}
				palette, err = render3d.NewGPUSkinPalette(cx, dev, render3d.SkinPaletteOptions{})
				if err != nil {
					t.Fatal(err)
				}
				good, err = render3d.UploadSkinnedMesh(cx, dev, skinnedQuadMesh(), render3d.MeshUpload{Label: "good"})
				if err != nil {
					t.Fatal(err)
				}
				goClosed, err = render3d.UploadSkinnedMesh(cx, dev, skinnedQuadMesh(), render3d.MeshUpload{Label: "closed"})
				if err != nil {
					t.Fatal(err)
				}
				_ = goClosed.Close()
				pipe, err = render3d.NewSkinnedMeshPipeline(cx, dev, camBuf.Layout(), palette.Layout(), render3d.SkinnedMeshPipelineOptions{
					DepthFormat: gpu.FormatDepth24Plus,
				})
				if err != nil {
					t.Fatal(err)
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				frames++
				camBuf.WriteCamera(cam)
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
				pass.SetBindGroup(render3d.SkinGroup, palette.BindGroup())
				good.Draw(pass)
				goClosed.Bind(pass)
				goClosed.Draw(pass)
				var nilSkinned *render3d.GPUSkinnedMesh
				nilSkinned.Bind(pass)
				nilSkinned.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(32, 32), nsgotest.ManualFrames())

	before := gpuDraws(app)
	app.Advance(16 * time.Millisecond)
	if frames == 0 {
		t.Fatal("OnFrame did not run")
	}
	if got := gpuDraws(app) - before; got != 1 {
		t.Fatalf("recorded %d draws, want 1", got)
	}
}

func TestSkinJointMatricesValidation(t *testing.T) {
	if _, err := render3d.SkinJointMatrices(nil, 0, nil); !errors.Is(err, render3d.ErrInvalidSkin) {
		t.Errorf("nil doc err = %v, want ErrInvalidSkin", err)
	}
	emptyDoc := &gltf.Document{}
	if _, err := render3d.SkinJointMatrices(emptyDoc, 0, nil); !errors.Is(err, render3d.ErrInvalidSkin) {
		t.Errorf("missing skin err = %v, want ErrInvalidSkin", err)
	}
	noJoints := &gltf.Document{Skins: []gltf.Skin{{}}}
	if _, err := render3d.SkinJointMatrices(noJoints, 0, nil); !errors.Is(err, render3d.ErrInvalidSkin) {
		t.Errorf("empty joints err = %v, want ErrInvalidSkin", err)
	}
	tooMany := &gltf.Document{Skins: []gltf.Skin{{Joints: make([]int, render3d.MaxSkinJoints+1)}}}
	if _, err := render3d.SkinJointMatrices(tooMany, 0, nil); !errors.Is(err, render3d.ErrInvalidSkin) {
		t.Errorf("oversized skin err = %v, want ErrInvalidSkin", err)
	}
	badNode := &gltf.Document{
		Nodes: []gltf.Node{{WorldMatrix: math3d.Mat4Identity()}},
		Skins: []gltf.Skin{{Joints: []int{0, 5}}},
	}
	if _, err := render3d.SkinJointMatrices(badNode, 0, nil); !errors.Is(err, render3d.ErrInvalidSkin) {
		t.Errorf("out-of-range joint node err = %v, want ErrInvalidSkin", err)
	}
	// Missing InverseBindMatrices defaults to identity matrices.
	noIBM := &gltf.Document{
		Nodes: []gltf.Node{{WorldMatrix: math3d.Translate(math3d.Vec3{X: 7})}},
		Skins: []gltf.Skin{{Joints: []int{0}}},
	}
	mats, err := render3d.SkinJointMatrices(noIBM, 0, nil)
	if err != nil {
		t.Fatalf("SkinJointMatrices(no IBM): %v", err)
	}
	if len(mats) != 1 || mats[0] != math3d.Translate(math3d.Vec3{X: 7}) {
		t.Errorf("no-IBM matrix = %v, want translate(7,0,0)", mats)
	}
}

func TestSkinnedMeshUint32IndexFormat(t *testing.T) {
	const n = 65536
	pos := make([]math3d.Vec3, n)
	jts := make([][4]uint16, n)
	wts := make([]math3d.Vec4, n)
	for i := range n {
		wts[i] = math3d.Vec4{X: 1}
	}
	sm := render3d.SkinnedMesh{
		Positions: pos,
		Joints:    jts,
		Weights:   wts,
		Indices:   []uint32{n - 3, n - 2, n - 1},
	}
	if sm.IndexFormat() != gpu.IndexUint32 || sm.IndexDataSize() != 12 {
		t.Fatalf("IndexFormat=%v size=%d, want uint32 and 12", sm.IndexFormat(), sm.IndexDataSize())
	}
	if err := sm.Validate(); err != nil {
		t.Fatalf("Validate(65536 verts): %v", err)
	}
}

func FuzzSkinnedMeshData(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5}, uint8(3), uint8(4))
	f.Add([]byte{10, 20, 30}, uint8(0), uint8(1))
	f.Fuzz(func(t *testing.T, raw []byte, flags uint8, jointCount uint8) {
		n := len(raw)
		if n == 0 || n > 256 {
			return
		}
		jc := int(jointCount)%render3d.MaxSkinJoints + 1
		m := render3d.SkinnedMesh{
			Positions:  make([]math3d.Vec3, n),
			Joints:     make([][4]uint16, n),
			Weights:    make([]math3d.Vec4, n),
			JointCount: jc,
		}
		for i, b := range raw {
			m.Positions[i] = math3d.Vec3{X: float32(b), Y: -float32(b), Z: float32(i)}
			j := uint16(int(b) % jc)
			m.Joints[i] = [4]uint16{j, uint16((int(j) + 1) % jc), 0, 0}
			m.Weights[i] = math3d.Vec4{X: float32(b), Y: float32(255 - b)}
		}
		if flags&1 != 0 {
			m.Normals = make([]math3d.Vec3, n)
			for i := range m.Normals {
				m.Normals[i] = math3d.Vec3{Z: 1}
			}
		}
		if flags&2 != 0 {
			for i := range n {
				m.Indices = append(m.Indices, uint32(i))
			}
			for len(m.Indices)%3 != 0 {
				m.Indices = append(m.Indices, 0)
			}
		}
		if err := m.Validate(); err != nil {
			if !errors.Is(err, render3d.ErrInvalidMesh) && !errors.Is(err, render3d.ErrInvalidSkin) {
				t.Fatalf("unexpected Validate error: %v", err)
			}
			return
		}
		vd := m.AppendVertexData(nil)
		id := m.AppendIndexData(nil)
		if len(vd) != m.VertexDataSize() || len(id) != m.IndexDataSize() || len(id)%4 != 0 {
			t.Fatalf("bad encoded sizes: v=%d/%d i=%d/%d", len(vd), m.VertexDataSize(), len(id), m.IndexDataSize())
		}
		for i := range n {
			off := i * render3d.SkinnedVertexStride
			var sum float32
			for k := range 4 {
				j := binary.LittleEndian.Uint32(vd[off+render3d.SkinnedJointsOffset+k*4:])
				if int(j) >= jc {
					t.Fatalf("vertex %d joint %d = %d >= %d", i, k, j, jc)
				}
				w := math.Float32frombits(binary.LittleEndian.Uint32(vd[off+render3d.SkinnedWeightsOffset+k*4:]))
				sum += w
			}
			if !math3d.ApproxEqual(sum, 1, 1e-5) {
				t.Fatalf("vertex %d encoded weight sum = %v, want 1", i, sum)
			}
		}
	})
}

func BenchmarkSkinnedMeshAppendVertexData(b *testing.B) {
	fsPath := filepath.FromSlash("../gltf/testdata/khronos/CesiumMan/glTF/CesiumMan.gltf")
	doc, err := gltf.LoadFS(os.DirFS(filepath.Dir(fsPath)), filepath.Base(fsPath))
	if err != nil {
		b.Fatal(err)
	}
	meshes, err := render3d.SkinnedMeshesFromGLTFNode(doc, 2)
	if err != nil {
		b.Fatal(err)
	}
	sm := meshes[0]
	buf := make([]byte, 0, sm.VertexDataSize())
	b.SetBytes(int64(sm.VertexDataSize()))
	b.ReportAllocs()
	for b.Loop() {
		buf = sm.AppendVertexData(buf[:0])
	}
}

func BenchmarkAppendJointMatrices(b *testing.B) {
	mats := make([]math3d.Mat4, 64)
	for i := range mats {
		mats[i] = math3d.Mat4Identity()
	}
	buf := make([]byte, 0, render3d.SkinUniformSize)
	b.SetBytes(render3d.SkinUniformSize)
	b.ReportAllocs()
	for b.Loop() {
		var err error
		buf, err = render3d.AppendJointMatrices(buf[:0], mats)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func skinnedQuadMesh() render3d.SkinnedMesh {
	n := math3d.Vec3{Z: 1}
	uv := math3d.Vec2{X: 1}
	j := [4]uint16{0, 1, 0, 0}
	w := math3d.Vec4{X: 1}
	return render3d.SkinnedMesh{
		Positions:  []math3d.Vec3{{X: -0.6, Y: -0.6, Z: 0}, {X: 0.6, Y: -0.6, Z: 0}, {X: 0.6, Y: 0.6, Z: 0}, {X: -0.6, Y: 0.6, Z: 0}},
		Normals:    []math3d.Vec3{n, n, n, n},
		UVs:        []math3d.Vec2{uv, uv, uv, uv},
		Joints:     [][4]uint16{j, j, j, j},
		Weights:    []math3d.Vec4{w, w, w, w},
		Indices:    []uint32{0, 1, 2, 0, 2, 3},
		JointCount: 2,
	}
}

func cloneSkinnedMesh(m render3d.SkinnedMesh) render3d.SkinnedMesh {
	out := m
	out.Positions = append([]math3d.Vec3(nil), m.Positions...)
	out.Normals = append([]math3d.Vec3(nil), m.Normals...)
	out.UVs = append([]math3d.Vec2(nil), m.UVs...)
	out.Joints = append([][4]uint16(nil), m.Joints...)
	out.Weights = append([]math3d.Vec4(nil), m.Weights...)
	out.Indices = append([]uint32(nil), m.Indices...)
	return out
}
