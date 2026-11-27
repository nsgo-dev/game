package render3d_test

import (
	"image"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/anim3d"
	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

const cesiumManPath = "../gltf/testdata/khronos/CesiumMan/glTF/CesiumMan.gltf"

// skinnedMeshNode returns the index of the first node in doc that draws a skinned mesh.
func skinnedMeshNode(t *testing.T, doc *gltf.Document) int {
	t.Helper()
	for i, n := range doc.Nodes {
		if n.Mesh >= 0 && n.Skin >= 0 {
			return i
		}
	}
	t.Fatal("document has no skinned mesh node")
	return -1
}

// posedCesiumMan loads CesiumMan, poses it at 0.6 s with both gltf.Animation.Apply and an anim3d.Player, and returns the mesh, the world-space joints from render3d.SkinJointMatrices, the Player's mesh-local joints, and the mesh node's world matrix from the Player.
func posedCesiumMan(t *testing.T) (sm render3d.SkinnedMesh, worldJoints, localJoints []math3d.Mat4, meshWorld math3d.Mat4) {
	t.Helper()
	doc, skinIdx, sm := loadGLTFSkinnedFixture(t, cesiumManPath)
	meshNode := skinnedMeshNode(t, doc)

	player, err := anim3d.NewPlayer(doc)
	if err != nil {
		t.Fatalf("anim3d.NewPlayer: %v", err)
	}
	player.Seek(600 * time.Millisecond)
	localJoints, err = player.SkinMatricesForNode(meshNode)
	if err != nil {
		t.Fatalf("SkinMatricesForNode: %v", err)
	}
	meshWorld = player.WorldTransform(meshNode)

	doc.Animations[0].Apply(doc, 0.6)
	if err := doc.ComputeWorldMatrices(); err != nil {
		t.Fatalf("ComputeWorldMatrices: %v", err)
	}
	worldJoints, err = render3d.SkinJointMatrices(doc, skinIdx, nil)
	if err != nil {
		t.Fatalf("SkinJointMatrices: %v", err)
	}
	return sm, worldJoints, localJoints, meshWorld
}

// TestAnim3dJointsMatchSkinJointMatrices checks the two joint conventions: anim3d's mesh-local joints times the mesh node's world matrix equal render3d's world-space joints for the same pose. CesiumMan's mesh node carries a Z_UP rotation, so dropping it would fail.
func TestAnim3dJointsMatchSkinJointMatrices(t *testing.T) {
	_, worldJoints, localJoints, meshWorld := posedCesiumMan(t)
	if meshWorld.ApproxEqual(math3d.Mat4Identity(), 1e-3) {
		t.Fatal("CesiumMan mesh node world matrix is identity; the test would not catch a dropped transform")
	}
	if len(localJoints) != len(worldJoints) {
		t.Fatalf("anim3d joints = %d, render3d joints = %d", len(localJoints), len(worldJoints))
	}
	for i := range worldJoints {
		if got := meshWorld.Mul(localJoints[i]); !got.ApproxEqual(worldJoints[i], 1e-3) {
			t.Fatalf("joint %d: meshWorld * anim3d joint = %v, want render3d joint %v", i, got, worldJoints[i])
		}
	}
}

// skinRenderer draws through NSGo's software GPU device with one camera framing the posed CesiumMan.
type skinRenderer struct {
	sr     *gpu.SoftwareRenderer
	dev    *gpu.Device
	camBuf *render3d.CameraBuffer
}

func newSkinRenderer(t *testing.T, w, h int, bounds math3d.AABB) *skinRenderer {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(frameCameraForBounds(bounds, w, h))
	return &skinRenderer{sr: sr, dev: dev, camBuf: camBuf}
}

func (r *skinRenderer) render(t *testing.T, draw func(pass *gpu.RenderPass)) *image.RGBA {
	t.Helper()
	enc := r.dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetBindGroup(render3d.CameraGroup, r.camBuf.BindGroup())
	draw(pass)
	pass.End()
	r.dev.Submit(enc.Finish())
	r.sr.WaitIdle()
	if err := r.sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	return r.sr.Image()
}

func newPalette(t *testing.T, dev *gpu.Device) *render3d.GPUSkinPalette {
	t.Helper()
	palette, err := render3d.NewGPUSkinPalette(nil, dev, render3d.SkinPaletteOptions{Label: "lit-skin"})
	if err != nil {
		t.Fatalf("NewGPUSkinPalette: %v", err)
	}
	t.Cleanup(func() { _ = palette.Close() })
	return palette
}

func checkSkinnedParity(t *testing.T, name string, got, want *image.RGBA, w, h int) {
	t.Helper()
	if cov := countNonClearPixels(want, skinClear); cov < 300 {
		t.Fatalf("%s: reference covered only %d pixels", name, cov)
	}
	if d := countDifferingPixels(got, want, 3); d > w*h/200 {
		t.Errorf("%s: skinned draw differs from the CPU-skinned reference in %d pixels (limit %d)", name, d, w*h/200)
	}
}

// TestSkinnedLitPBRDrawsWithLights draws CesiumMan through the lit PBR pipeline with scene lights bound, once with render3d's world-space joints and an identity model, and once with anim3d's mesh-local joints and the mesh node's world matrix as the model, and compares both with the CPU-skinned mesh drawn by the plain lit PBR pipeline.
func TestSkinnedLitPBRDrawsWithLights(t *testing.T) {
	const w, h = 96, 96
	sm, worldJoints, localJoints, meshWorld := posedCesiumMan(t)
	cpuMesh, bounds := cpuSkinMesh(sm, worldJoints)
	r := newSkinRenderer(t, w, h, bounds)
	dev := r.dev

	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.2, Y: 0.25, Z: 0.3}, Intensity: 0.4},
		Directional: []render3d.DirectionalLight{
			{Direction: math3d.Vec3{X: -0.3, Y: -0.6, Z: -1}, Color: math3d.Vec3{X: 1, Y: 0.95, Z: 0.9}, Intensity: 2.5},
		},
		Point: []render3d.PointLight{
			{Position: bounds.Center().Add(math3d.Vec3{Z: 1.5}), Color: math3d.Vec3{X: 0.4, Y: 0.6, Z: 1}, Intensity: 3, Range: 6},
		},
	}
	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{Label: "skin-lights"})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })
	mat := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.8, Y: 0.6, Z: 0.45}, Roughness: 0.55}
	drawBuf, err := render3d.NewPBRDrawBuffer(nil, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), mat), false)
	if err != nil {
		t.Fatalf("NewPBRDrawBuffer: %v", err)
	}
	t.Cleanup(func() { _ = drawBuf.Close() })

	plainPipe, err := render3d.NewLitPBRPipeline(nil, dev, r.camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), gpu.FormatDepth24Plus)
	if err != nil {
		t.Fatalf("NewLitPBRPipeline: %v", err)
	}
	t.Cleanup(func() { _ = plainPipe.Close() })
	cpuGPU, err := render3d.UploadMesh(nil, dev, cpuMesh, render3d.MeshUpload{Label: "cpu-skinned"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = cpuGPU.Close() })

	palette := newPalette(t, dev)
	skinPipe, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, r.camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		SkinLayout:  palette.Layout(),
	})
	if err != nil {
		t.Fatalf("NewLitPBRPipelineWithOptions(SkinLayout): %v", err)
	}
	t.Cleanup(func() { _ = skinPipe.Close() })
	skinned, err := render3d.UploadSkinnedMesh(nil, dev, sm, render3d.MeshUpload{Label: "skinned"})
	if err != nil {
		t.Fatalf("UploadSkinnedMesh: %v", err)
	}
	t.Cleanup(func() { _ = skinned.Close() })

	drawBuf.Write(dev, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), mat))
	want := r.render(t, func(pass *gpu.RenderPass) {
		pass.SetPipeline(plainPipe)
		lightBuf.Bind(pass)
		pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
		cpuGPU.Draw(pass)
	})
	want = cloneRGBA(want)

	drawSkinned := func(model math3d.Mat4, joints []math3d.Mat4) *image.RGBA {
		if err := palette.Update(dev, joints); err != nil {
			t.Fatalf("palette.Update: %v", err)
		}
		drawBuf.Write(dev, render3d.NewPBRDrawUniform(model, mat))
		return r.render(t, func(pass *gpu.RenderPass) {
			pass.SetPipeline(skinPipe)
			lightBuf.Bind(pass)
			pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
			pass.SetBindGroup(render3d.LitPBRSkinGroup, palette.BindGroup())
			skinned.Draw(pass)
		})
	}
	checkSkinnedParity(t, "world-space joints", drawSkinned(math3d.Mat4Identity(), worldJoints), want, w, h)
	checkSkinnedParity(t, "anim3d joints with the mesh node as model", drawSkinned(meshWorld, localJoints), want, w, h)
}

// TestSkinnedMaterialDrawsGLTFMaterial draws CesiumMan through the material pipeline with a GPUMaterial bound, from anim3d's mesh-local joints uploaded with UpdateWithTransform, and compares it with the CPU-skinned mesh drawn by the plain material pipeline.
func TestSkinnedMaterialDrawsGLTFMaterial(t *testing.T) {
	const w, h = 96, 96
	sm, worldJoints, localJoints, meshWorld := posedCesiumMan(t)
	cpuMesh, bounds := cpuSkinMesh(sm, worldJoints)
	r := newSkinRenderer(t, w, h, bounds)
	dev := r.dev

	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = defaults.Close() })
	m := render3d.DefaultMaterial()
	m.BaseColorFactor = [4]float32{0.7, 0.5, 0.35, 1}
	m.MetallicFactor = 0
	m.RoughnessFactor = 0.6
	gmat, err := render3d.NewGPUMaterial(nil, dev, defaults, m, render3d.MaterialUpload{Label: "skin-material"})
	if err != nil {
		t.Fatalf("NewGPUMaterial: %v", err)
	}
	t.Cleanup(func() { _ = gmat.Close() })

	plainPipe, err := render3d.NewMaterialPipeline(nil, dev, r.camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{DepthFormat: gpu.FormatDepth24Plus})
	if err != nil {
		t.Fatalf("NewMaterialPipeline: %v", err)
	}
	t.Cleanup(func() { _ = plainPipe.Close() })
	cpuGPU, err := render3d.UploadMesh(nil, dev, cpuMesh, render3d.MeshUpload{Label: "cpu-skinned"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = cpuGPU.Close() })

	palette := newPalette(t, dev)
	if err := palette.UpdateWithTransform(dev, meshWorld, localJoints); err != nil {
		t.Fatalf("UpdateWithTransform: %v", err)
	}
	if palette.JointCount() != len(localJoints) {
		t.Fatalf("JointCount = %d, want %d", palette.JointCount(), len(localJoints))
	}
	skinPipe, err := render3d.NewMaterialPipeline(nil, dev, r.camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		SkinLayout:  palette.Layout(),
	})
	if err != nil {
		t.Fatalf("NewMaterialPipeline(SkinLayout): %v", err)
	}
	t.Cleanup(func() { _ = skinPipe.Close() })
	skinned, err := render3d.UploadSkinnedMesh(nil, dev, sm, render3d.MeshUpload{Label: "skinned"})
	if err != nil {
		t.Fatalf("UploadSkinnedMesh: %v", err)
	}
	t.Cleanup(func() { _ = skinned.Close() })

	want := cloneRGBA(r.render(t, func(pass *gpu.RenderPass) {
		pass.SetPipeline(plainPipe)
		gmat.Bind(pass)
		cpuGPU.Draw(pass)
	}))
	got := r.render(t, func(pass *gpu.RenderPass) {
		pass.SetPipeline(skinPipe)
		gmat.Bind(pass)
		pass.SetBindGroup(render3d.MaterialSkinGroup, palette.BindGroup())
		skinned.Draw(pass)
	})
	checkSkinnedParity(t, "material", got, want, w, h)
}

// TestSkinnedPipelinesRejectSharedGroups checks that skinning cannot be combined with the fog or IBL layouts that use the same bind group.
func TestSkinnedPipelinesRejectSharedGroups(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatDepth24Plus)
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
	drawBuf, err := render3d.NewPBRDrawBuffer(nil, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), render3d.PBRMaterial{BaseColor: math3d.Vec3One()}), false)
	if err != nil {
		t.Fatalf("NewPBRDrawBuffer: %v", err)
	}
	t.Cleanup(func() { _ = drawBuf.Close() })
	fogBuf, err := render3d.NewGPUFogBuffer(nil, dev, render3d.Fog{}, render3d.FogBufferOptions{})
	if err != nil {
		t.Fatalf("NewGPUFogBuffer: %v", err)
	}
	t.Cleanup(func() { _ = fogBuf.Close() })
	defaults, err := render3d.NewMaterialDefaults(nil, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	t.Cleanup(func() { _ = defaults.Close() })
	palette := newPalette(t, dev)

	if _, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{SkinLayout: palette.Layout(), FogLayout: fogBuf.Layout()}); err == nil {
		t.Error("lit PBR with SkinLayout and FogLayout: want an error")
	}
	if _, err := render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{SkinLayout: palette.Layout(), FogLayout: fogBuf.Layout()}); err == nil {
		t.Error("material with SkinLayout and FogLayout: want an error")
	}
	for _, tc := range []struct {
		name string
		fn   func() (*gpu.Pipeline, error)
	}{
		{"lit linear", func() (*gpu.Pipeline, error) {
			return render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{SkinLayout: palette.Layout(), LinearOutput: true})
		}},
		{"material linear", func() (*gpu.Pipeline, error) {
			return render3d.NewMaterialPipeline(nil, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{SkinLayout: palette.Layout(), LinearOutput: true})
		}},
	} {
		p, err := tc.fn()
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		_ = p.Close()
	}
}

// TestSkinnedBoundsContainPosedMesh checks that SkinnedBounds contains every CPU-skinned vertex of the posed CesiumMan while the unskinned Bounds does not.
func TestSkinnedBoundsContainPosedMesh(t *testing.T) {
	sm, worldJoints, _, _ := posedCesiumMan(t)
	_, posed := cpuSkinMesh(sm, worldJoints)
	bind := sm.Bounds()
	got := render3d.SkinnedBounds(bind, worldJoints)
	const eps = 1e-4
	if got.Min.X > posed.Min.X+eps || got.Min.Y > posed.Min.Y+eps || got.Min.Z > posed.Min.Z+eps ||
		got.Max.X < posed.Max.X-eps || got.Max.Y < posed.Max.Y-eps || got.Max.Z < posed.Max.Z-eps {
		t.Fatalf("SkinnedBounds = %v, does not contain posed mesh bounds %v", got, posed)
	}
	if bind.Min.Y <= posed.Min.Y && bind.Max.Y >= posed.Max.Y && bind.Min.Z <= posed.Min.Z && bind.Max.Z >= posed.Max.Z {
		t.Fatalf("unskinned bounds %v already contain the posed mesh %v; the fixture does not exercise SkinnedBounds", bind, posed)
	}
	if b := render3d.SkinnedBounds(bind, nil); b != bind {
		t.Errorf("SkinnedBounds(bind, nil) = %v, want %v", b, bind)
	}
	allocs := testing.AllocsPerRun(10, func() { _ = render3d.SkinnedBounds(bind, worldJoints) })
	if allocs != 0 {
		t.Errorf("SkinnedBounds allocated %v times, want 0", allocs)
	}
}
