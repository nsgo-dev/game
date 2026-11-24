package render3d_test

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	shadowRefWidth  = 128
	shadowRefHeight = 96
)

var (
	shadowClearColor = core.RGB(0x182230)
	shadowLightDir   = math3d.Vec3{X: -0.55, Y: -1.0, Z: -0.35}.Norm()
	// shadowRig lights the shadow scene with the shadowed sun (directional light 0), an unshadowed fill light from the other side (directional light 1), and an ambient term.
	shadowRig = render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.18, Y: 0.20, Z: 0.24}, Intensity: 1},
		Directional: []render3d.DirectionalLight{
			{Direction: shadowLightDir, Color: math3d.Vec3{X: 0.82, Y: 0.80, Z: 0.76}, Intensity: 3.5},
			{Direction: math3d.Vec3{X: 0.6, Y: -1.0, Z: 0.5}, Color: math3d.Vec3{X: 0.70, Y: 0.72, Z: 0.78}, Intensity: 0.6},
		},
	}
	shadowFloorMat  = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.78, Y: 0.80, Z: 0.82}, Roughness: 0.9}
	shadowPillarMat = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.88, Y: 0.42, Z: 0.30}, Roughness: 0.6}
	shadowBlockMat  = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.32, Y: 0.68, Z: 0.88}, Roughness: 0.6}
)

// shadowFloorMesh returns an indexed horizontal quad on y = 0 spanning [-half, half] in X and Z with +Y normals.
func shadowFloorMesh(half float32) render3d.Mesh {
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
			{X: 0, Y: 1},
			{X: 1, Y: 1},
			{X: 1, Y: 0},
			{X: 0, Y: 0},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

// shadowBoxMesh returns a unit cube centered at the origin with outward face normals and CCW winding.
func shadowBoxMesh() render3d.Mesh {
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
		c := f.n.Scale(0.5)
		u, v := f.u.Scale(0.5), f.v.Scale(0.5)
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

type shadowTestScene struct {
	floorModel  math3d.Mat4
	pillarModel math3d.Mat4
	blockModel  math3d.Mat4
	sceneBounds math3d.AABB
	cam         *scene3d.Camera
}

// drawUniforms returns the per-draw uniforms of the floor, the pillar and the floating block, in draw order.
func (sc shadowTestScene) drawUniforms() [3]render3d.PBRDrawUniform {
	return [3]render3d.PBRDrawUniform{
		render3d.NewPBRDrawUniform(sc.floorModel, shadowFloorMat),
		render3d.NewPBRDrawUniform(sc.pillarModel, shadowPillarMat),
		render3d.NewPBRDrawUniform(sc.blockModel, shadowBlockMat),
	}
}

// expectedFloor returns the sRGB 8-bit color LitPBRShadowWGSL draws for the floor at wp when the sun's shadow visibility there is vis, from the CPU references ShadePointShadowed and LinearToSRGB.
func (sc shadowTestScene) expectedFloor(wp math3d.Vec3, vis float32) [3]float64 {
	e := sc.cam.Eye()
	eye := math3d.Vec3{X: e.X, Y: e.Y, Z: e.Z}
	c := render3d.LinearToSRGB(render3d.ShadePointShadowed(wp, math3d.Vec3Up(), eye.Sub(wp), shadowFloorMat, shadowRig, vis))
	return [3]float64{float64(c.X) * 255, float64(c.Y) * 255, float64(c.Z) * 255}
}

// shadowDrawBuffers creates the floor, pillar and block draw buffers sharing one bind group layout.
func shadowDrawBuffers(t *testing.T, cx *core.Scope, dev *gpu.Device, sc shadowTestScene, retained bool) [3]*render3d.PBRDrawBuffer {
	t.Helper()
	var draws [3]*render3d.PBRDrawBuffer
	var layout *gpu.BindGroupLayout
	for i, u := range sc.drawUniforms() {
		db, err := render3d.NewPBRDrawBuffer(cx, dev, layout, u, retained)
		if err != nil {
			t.Fatalf("NewPBRDrawBuffer[%d]: %v", i, err)
		}
		if layout == nil {
			layout = db.Layout()
		}
		draws[i] = db
	}
	return draws
}

func newShadowTestScene(w, h int) shadowTestScene {
	floorModel := math3d.Mat4Identity()
	// Tall pillar standing on the floor to the right of center so its shadow stretches across the middle-left floor.
	pillarModel := math3d.Translate(math3d.Vec3{X: 1.4, Y: 1.1, Z: 0.4}).Mul(math3d.Scale(math3d.Vec3{X: 0.9, Y: 2.2, Z: 0.9}))
	// Elevated floating block casting a clean detached shadow onto the floor.
	blockModel := math3d.Translate(math3d.Vec3{X: -0.6, Y: 1.9, Z: 0.8}).Mul(math3d.Scale(math3d.Vec3{X: 1.1, Y: 0.7, Z: 1.1}))

	bounds := math3d.AABB{
		Min: math3d.Vec3{X: -5.5, Y: -0.2, Z: -5.5},
		Max: math3d.Vec3{X: 5.5, Y: 2.8, Z: 5.5},
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: -0.5, Y: 5.2, Z: 7.4}, math3d.DegToRad(46), float32(w)/float32(h), 0.1, 40)
	cam.LookAt(math3d.Vec3{X: -0.3, Y: 0.4, Z: 0.0}, math3d.Vec3Up())

	return shadowTestScene{
		floorModel:  floorModel,
		pillarModel: pillarModel,
		blockModel:  blockModel,
		sceneBounds: bounds,
		cam:         cam,
	}
}

type shadowSoftwareHarness struct {
	sr         *gpu.SoftwareRenderer
	dev        *gpu.Device
	camBuf     *render3d.CameraBuffer
	lightBuf   *render3d.GPULightBuffer
	draws      [3]*render3d.PBRDrawBuffer
	shadowMap  *render3d.DirectionalShadowMap
	litPipe    *gpu.Pipeline
	floorMesh  *render3d.GPUMesh
	boxMesh    *render3d.GPUMesh
	scene      shadowTestScene
	resolution uint32
}

func newShadowSoftwareHarness(t *testing.T, res uint32, filter render3d.ShadowFilter) *shadowSoftwareHarness {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(shadowRefWidth, shadowRefHeight, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	scene := newShadowTestScene(shadowRefWidth, shadowRefHeight)

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	lightBuf, err := render3d.NewLightBuffer(nil, dev, shadowRig, render3d.LightBufferOptions{Label: "shadow-lights"})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	draws := shadowDrawBuffers(t, nil, dev, scene, false)
	t.Cleanup(func() {
		for _, d := range draws {
			_ = d.Close()
		}
	})

	smOpts := render3d.DefaultShadowMapOptions()
	smOpts.Resolution = res
	smOpts.PCFKernel = filter
	smOpts.DepthBias = 1
	smOpts.DepthBiasSlopeScale = 1.5
	sm, err := render3d.NewDirectionalShadowMap(nil, dev, smOpts)
	if err != nil {
		t.Fatalf("NewDirectionalShadowMap: %v", err)
	}
	t.Cleanup(func() { _ = sm.Close() })

	litPipe, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), draws[0].Layout(), render3d.LitPBRPipelineOptions{
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		ShadowLayout: sm.Layout(),
	})
	if err != nil {
		t.Fatalf("NewLitPBRPipelineWithOptions(ShadowLayout): %v", err)
	}
	t.Cleanup(func() { _ = litPipe.Close() })

	floorMesh, err := render3d.UploadMesh(nil, dev, shadowFloorMesh(5.5), render3d.MeshUpload{Label: "floor"})
	if err != nil {
		t.Fatalf("UploadMesh floor: %v", err)
	}
	t.Cleanup(func() { _ = floorMesh.Close() })

	boxMesh, err := render3d.UploadMesh(nil, dev, shadowBoxMesh(), render3d.MeshUpload{Label: "box"})
	if err != nil {
		t.Fatalf("UploadMesh box: %v", err)
	}
	t.Cleanup(func() { _ = boxMesh.Close() })

	return &shadowSoftwareHarness{
		sr:         sr,
		dev:        dev,
		camBuf:     camBuf,
		lightBuf:   lightBuf,
		draws:      draws,
		shadowMap:  sm,
		litPipe:    litPipe,
		floorMesh:  floorMesh,
		boxMesh:    boxMesh,
		scene:      scene,
		resolution: res,
	}
}

// drawShadowScene records the lit main pass: the floor, the pillar and the floating block with the camera, the lights and the shadow map bound.
func drawShadowScene(pass *gpu.RenderPass, pipe *gpu.Pipeline, camBuf *render3d.CameraBuffer, lightBuf *render3d.GPULightBuffer, sm *render3d.DirectionalShadowMap, draws [3]*render3d.PBRDrawBuffer, floorMesh, boxMesh *render3d.GPUMesh) {
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	pass.SetBindGroup(render3d.LightGroup, lightBuf.BindGroup())
	pass.SetBindGroup(render3d.ShadowGroup, sm.BindGroup())
	for i, mesh := range [3]*render3d.GPUMesh{floorMesh, boxMesh, boxMesh} {
		pass.SetBindGroup(render3d.PBRDrawGroup, draws[i].BindGroup())
		mesh.Draw(pass)
	}
}

func (h *shadowSoftwareHarness) render(t *testing.T, cfg render3d.DirectionalShadowConfig) (*image.RGBA, render3d.ShadowUniforms) {
	t.Helper()
	u, err := h.shadowMap.UpdateBounds(h.dev, cfg, h.scene.sceneBounds)
	if err != nil {
		t.Fatalf("UpdateBounds: %v", err)
	}
	h.camBuf.WriteCamera(h.scene.cam)

	enc := h.dev.BeginEncoder()

	// Pass 1: directional shadow depth pass.
	sPass := h.shadowMap.BeginShadowPass(h.dev, enc)
	if sPass == nil {
		t.Fatal("BeginShadowPass returned nil")
	}
	if !h.shadowMap.DrawCaster(sPass, h.pillarMesh(), h.scene.pillarModel) {
		t.Fatal("DrawCaster pillar returned false")
	}
	if !h.shadowMap.DrawCaster(sPass, h.boxMesh, h.scene.blockModel) {
		t.Fatal("DrawCaster block returned false")
	}
	sPass.End()

	// Pass 2: lit shadowed main pass.
	mPass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label: "shadow-main",
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: shadowClearColor,
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1.0,
		},
	})
	drawShadowScene(mPass, h.litPipe, h.camBuf, h.lightBuf, h.shadowMap, h.draws, h.floorMesh, h.boxMesh)
	mPass.End()
	h.dev.Submit(enc.Finish())
	h.sr.WaitIdle()
	if err := h.sr.LastError(); err != nil {
		t.Fatalf("software renderer error: %v", err)
	}
	return h.sr.Image(), u
}

func (h *shadowSoftwareHarness) pillarMesh() *render3d.GPUMesh { return h.boxMesh }

func (h *shadowSoftwareHarness) readShadowDepth(t *testing.T) render3d.ShadowDepthGrid {
	t.Helper()
	var raw []byte
	h.dev.ReadTexture(h.shadowMap.DepthTexture(), gpu.TextureRegion{
		Width:  h.resolution,
		Height: h.resolution,
	}, func(b []byte, err error) {
		if err != nil {
			t.Fatalf("ReadTexture depth: %v", err)
		}
		raw = b
	})
	grid, err := render3d.ShadowDepthGridFromBytes(int(h.resolution), int(h.resolution), raw)
	if err != nil {
		t.Fatalf("ShadowDepthGridFromBytes: %v", err)
	}
	return grid
}

func shadowLuma(c color.RGBA) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}

func TestDirectionalShadowConfigValidation(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	base := render3d.DefaultDirectionalShadowConfig()
	if err := base.Validate(); err != nil {
		t.Fatalf("DefaultDirectionalShadowConfig().Validate() = %v", err)
	}

	cases := []struct {
		name string
		mod  func(*render3d.DirectionalShadowConfig)
	}{
		{"zero direction", func(c *render3d.DirectionalShadowConfig) { c.Direction = math3d.Vec3{} }},
		{"nan direction", func(c *render3d.DirectionalShadowConfig) { c.Direction = math3d.Vec3{X: nan, Y: -1} }},
		{"inf target", func(c *render3d.DirectionalShadowConfig) { c.Target = math3d.Vec3{Y: inf} }},
		{"negative ortho", func(c *render3d.DirectionalShadowConfig) { c.OrthoSize = -1 }},
		{"negative depth range", func(c *render3d.DirectionalShadowConfig) { c.DepthRange = -5 }},
		{"nan depth range", func(c *render3d.DirectionalShadowConfig) { c.DepthRange = nan }},
		{"non-pow2 resolution", func(c *render3d.DirectionalShadowConfig) { c.Resolution = 300 }},
		{"too small resolution", func(c *render3d.DirectionalShadowConfig) { c.Resolution = 8 }},
		{"too large resolution", func(c *render3d.DirectionalShadowConfig) { c.Resolution = 16384 }},
		{"negative depth bias", func(c *render3d.DirectionalShadowConfig) { c.DepthBias = -0.001 }},
		{"negative normal bias", func(c *render3d.DirectionalShadowConfig) { c.NormalBias = -0.01 }},
		{"invalid filter", func(c *render3d.DirectionalShadowConfig) { c.PCFKernel = 99 }},
		{"strength negative", func(c *render3d.DirectionalShadowConfig) { c.Strength = -0.1 }},
		{"strength > 1", func(c *render3d.DirectionalShadowConfig) { c.Strength = 1.5 }},
		{"negative caster padding", func(c *render3d.DirectionalShadowConfig) { c.CasterPadding = -2 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mod(&cfg)
			if err := cfg.Validate(); !errors.Is(err, render3d.ErrInvalidShadow) {
				t.Fatalf("Validate = %v, want ErrInvalidShadow", err)
			}
		})
	}

	if _, err := base.FitBounds(base.Direction, math3d.AABBEmpty()); !errors.Is(err, render3d.ErrInvalidShadow) {
		t.Fatalf("FitBounds(empty) = %v, want ErrInvalidShadow", err)
	}
	if _, err := base.FitBounds(base.Direction, math3d.AABB{Min: math3d.Vec3{X: nan}, Max: math3d.Vec3{X: 1}}); !errors.Is(err, render3d.ErrInvalidShadow) {
		t.Fatalf("FitBounds(nan) = %v, want ErrInvalidShadow", err)
	}
}

func TestShadowMapOptionsValidation(t *testing.T) {
	base := render3d.DefaultShadowMapOptions()
	if err := base.Validate(); err != nil {
		t.Fatalf("DefaultShadowMapOptions().Validate() = %v", err)
	}

	cases := []struct {
		name string
		mod  func(*render3d.ShadowMapOptions)
	}{
		{"non-pow2 resolution", func(o *render3d.ShadowMapOptions) { o.Resolution = 200 }},
		{"too small resolution", func(o *render3d.ShadowMapOptions) { o.Resolution = 4 }},
		{"invalid filter", func(o *render3d.ShadowMapOptions) { o.PCFKernel = 7 }},
		{"nan slope scale", func(o *render3d.ShadowMapOptions) { o.DepthBiasSlopeScale = float32(math.NaN()) }},
		{"invalid cull mode", func(o *render3d.ShadowMapOptions) { o.CullMode = 42 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := base
			tc.mod(&o)
			if err := o.Validate(); !errors.Is(err, render3d.ErrInvalidShadow) {
				t.Fatalf("Validate = %v, want ErrInvalidShadow", err)
			}
		})
	}

	if _, err := render3d.NewDirectionalShadowMap(nil, nil, base); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewDirectionalShadowMap(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.ShadowBindingLayout(nil, nil); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("ShadowBindingLayout(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewLitPBRShadowPipeline(nil, nil, nil, nil, nil, nil, gpu.FormatUndefined); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewLitPBRShadowPipeline(nil dev) = %v, want ErrDeviceLost", err)
	}

	// The shadow takes bind group 3, which fog, IBL and skinning also use, so the options refuse combining them, and the dedicated constructor needs a shadow layout.
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()
	layout, err := render3d.ShadowBindingLayout(nil, dev)
	if err != nil {
		t.Fatalf("ShadowBindingLayout: %v", err)
	}
	defer func() { _ = layout.Close() }()
	for name, opts := range map[string]render3d.LitPBRPipelineOptions{
		"fog":  {ShadowLayout: layout, FogLayout: layout},
		"ibl":  {ShadowLayout: layout, IBLLayout: layout},
		"skin": {ShadowLayout: layout, SkinLayout: layout},
	} {
		if _, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, layout, layout, layout, opts); !errors.Is(err, gpu.ErrInvalidDescriptor) {
			t.Errorf("ShadowLayout with %s = %v, want ErrInvalidDescriptor", name, err)
		}
	}
	if _, err := render3d.NewLitPBRShadowPipeline(nil, dev, layout, layout, layout, nil, gpu.FormatUndefined); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("NewLitPBRShadowPipeline(nil shadow layout) = %v, want ErrInvalidDescriptor", err)
	}
}

func TestDirectionalShadowFitBoundsAndOrdering(t *testing.T) {
	dirs := []struct {
		name string
		dir  math3d.Vec3
	}{
		{"angled", math3d.Vec3{X: -0.6, Y: -1.0, Z: -0.4}},
		{"straight-down", math3d.Vec3{X: 0, Y: -1, Z: 0}},
		{"straight-up", math3d.Vec3{X: 0, Y: 1, Z: 0}},
		{"horizontal", math3d.Vec3{X: 1, Y: 0, Z: 0}},
	}
	bounds := math3d.AABB{
		Min: math3d.Vec3{X: -4, Y: -1, Z: -3},
		Max: math3d.Vec3{X: 6, Y: 5, Z: 4},
	}

	for _, d := range dirs {
		for _, snap := range []bool{false, true} {
			cfg := render3d.DefaultDirectionalShadowConfig()
			cfg.Direction = d.dir
			cfg.Resolution = 64
			cfg.TexelSnap = snap

			lvp, err := cfg.FitBounds(d.dir, bounds)
			if err != nil {
				t.Fatalf("%s snap=%v FitBounds: %v", d.name, snap, err)
			}

			// Every one of the 8 AABB corners must project strictly inside [-1, 1] x [-1, 1] x [0, 1].
			xs := [2]float32{bounds.Min.X, bounds.Max.X}
			ys := [2]float32{bounds.Min.Y, bounds.Max.Y}
			zs := [2]float32{bounds.Min.Z, bounds.Max.Z}
			for _, x := range xs {
				for _, y := range ys {
					for _, z := range zs {
						p := math3d.Vec3{X: x, Y: y, Z: z}
						c := lvp.TransformPoint(p)
						if c.X < -1 || c.X > 1 || c.Y < -1 || c.Y > 1 || c.Z < 0 || c.Z > 1 {
							t.Errorf("%s snap=%v corner %v projected outside NDC: %v", d.name, snap, p, c)
						}
					}
				}
			}

			// A point further along the light direction must have strictly greater clip Z than a point closer to the light.
			unit := d.dir.Norm()
			center := bounds.Center()
			closer := center.Sub(unit.Scale(1.5))
			farther := center.Add(unit.Scale(1.5))
			zCloser := lvp.TransformPoint(closer).Z
			zFarther := lvp.TransformPoint(farther).Z
			if !(zCloser < zFarther) {
				t.Errorf("%s snap=%v depth order inverted: closer=%v farther=%v", d.name, snap, zCloser, zFarther)
			}
		}
	}

	// CasterPadding extends near plane toward the light so an elevated occluder above bounds still lands in [0, 1] Z.
	cfg := render3d.DefaultDirectionalShadowConfig()
	cfg.Direction = math3d.Vec3{Y: -1}
	cfg.CasterPadding = 10
	lvp, err := cfg.FitBounds(cfg.Direction, bounds)
	if err != nil {
		t.Fatalf("FitBounds with CasterPadding: %v", err)
	}
	highCaster := math3d.Vec3{X: 0, Y: bounds.Max.Y + 8, Z: 0}
	ndc := lvp.TransformPoint(highCaster)
	if ndc.Z < 0 || ndc.Z > 1 {
		t.Fatalf("high caster with CasterPadding projected to Z=%v, want inside [0, 1]", ndc.Z)
	}
}

func TestDirectionalShadowTexelSnapping(t *testing.T) {
	const res uint32 = 64
	const orthoSize float32 = 16.0
	texelWorld := orthoSize / float32(res) // 0.25 world units per texel

	cfg := render3d.DefaultDirectionalShadowConfig()
	cfg.Direction = math3d.Vec3{X: -0.4, Y: -1.0, Z: -0.3}.Norm()
	cfg.OrthoSize = orthoSize
	cfg.Resolution = res
	cfg.TexelSnap = true
	cfg.Target = math3d.Vec3{X: 0, Y: 0, Z: 0}

	baseLVP, err := cfg.LightViewProj()
	if err != nil {
		t.Fatalf("base LightViewProj: %v", err)
	}

	// Sub-half-texel perturbation around origin must produce bit-for-bit identical LightViewProj (zero shadow swimming).
	cfgSub := cfg
	cfgSub.Target = math3d.Vec3{X: texelWorld * 0.15, Y: 0, Z: texelWorld * 0.1}
	subLVP, err := cfgSub.LightViewProj()
	if err != nil {
		t.Fatalf("sub-texel LightViewProj: %v", err)
	}
	// Check XY rows (columns 0,1,3 rows 0,1) are identical when target shifts less than half a texel.
	staticPoint := math3d.Vec3{X: 1.25, Y: 0.5, Z: -0.75}
	p0 := baseLVP.TransformPoint(staticPoint)
	p1 := subLVP.TransformPoint(staticPoint)
	if math.Abs(float64(p0.X-p1.X)) > 1e-6 || math.Abs(float64(p0.Y-p1.Y)) > 1e-6 {
		t.Fatalf("sub-texel target move shifted static point XY: %v -> %v", p0, p1)
	}

	// Large target translation must shift every static world point in shadow-map texel coordinates by an exact integer number of texels.
	cfgMoved := cfg
	cfgMoved.Target = math3d.Vec3{X: 3.17, Y: 0.41, Z: -2.63}
	movedLVP, err := cfgMoved.LightViewProj()
	if err != nil {
		t.Fatalf("moved LightViewProj: %v", err)
	}
	p2 := movedLVP.TransformPoint(staticPoint)
	duTexels := float64((p2.X-p0.X)*0.5) * float64(res)
	dvTexels := float64((p0.Y-p2.Y)*0.5) * float64(res)
	if math.Abs(duTexels-math.Round(duTexels)) > 1e-4 || math.Abs(dvTexels-math.Round(dvTexels)) > 1e-4 {
		t.Fatalf("texel-snapped move produced fractional texel shift: du=%v dv=%v", duTexels, dvTexels)
	}
}

// TestDirectionalShadowFitBoundsTexelStable moves the fitted bounds, as UpdateBounds does when the framing follows moving casters or the camera, and checks that static world points move by whole texels (zero for a sub-texel move), so shadow edges do not shimmer.
func TestDirectionalShadowFitBoundsTexelStable(t *testing.T) {
	const res uint32 = 256
	cfg := render3d.DefaultDirectionalShadowConfig()
	cfg.Direction = math3d.Vec3{X: -0.4, Y: -1.0, Z: -0.3}
	cfg.Resolution = res
	cfg.TexelSnap = true
	bounds := math3d.AABB{Min: math3d.Vec3{X: -5.5, Y: -0.2, Z: -5.5}, Max: math3d.Vec3{X: 5.5, Y: 2.8, Z: 5.5}}
	base, err := cfg.FitBounds(cfg.Direction, bounds)
	if err != nil {
		t.Fatalf("FitBounds: %v", err)
	}
	statics := []math3d.Vec3{{X: 1.25, Y: 0.5, Z: -0.75}, {X: -3.1, Y: 0, Z: 2.2}, {X: 4.4, Y: 2.1, Z: 4.9}}
	texelShift := func(a, b math3d.Mat4, p math3d.Vec3) (du, dv float64) {
		pa, pb := a.TransformPoint(p), b.TransformPoint(p)
		return float64(pb.X-pa.X) * 0.5 * float64(res), float64(pa.Y-pb.Y) * 0.5 * float64(res)
	}
	moves := []struct {
		name     string
		delta    math3d.Vec3
		wantZero bool
	}{
		{"sub-texel", math3d.Vec3{X: 0.004, Y: 0.002, Z: -0.003}, true},
		{"multi-texel", math3d.Vec3{X: 1.37, Y: 0.21, Z: -0.83}, false},
		{"far", math3d.Vec3{X: -12.9, Y: 3.3, Z: 7.1}, false},
	}
	for _, m := range moves {
		moved := math3d.AABB{Min: bounds.Min.Add(m.delta), Max: bounds.Max.Add(m.delta)}
		lvp, err := cfg.FitBounds(cfg.Direction, moved)
		if err != nil {
			t.Fatalf("%s FitBounds: %v", m.name, err)
		}
		for _, p := range statics {
			du, dv := texelShift(base, lvp, p)
			if math.Abs(du-math.Round(du)) > 2e-3 || math.Abs(dv-math.Round(dv)) > 2e-3 {
				t.Errorf("%s: static point %v moved by a fractional number of texels (%v, %v)", m.name, p, du, dv)
			}
			if m.wantZero && (math.Round(du) != 0 || math.Round(dv) != 0) {
				t.Errorf("%s: static point %v moved by (%v, %v) texels, want 0", m.name, p, du, dv)
			}
		}
	}

	// Turning the light keeps the frustum width (the bounding sphere does not depend on the direction), so the texel size, and with it the penumbra width, stays the same.
	// Row 0 of the matrix is (2/width) times the light basis' unit right vector.
	width := func(lvp math3d.Mat4) float32 {
		return 2 / math3d.Vec3{X: lvp[0], Y: lvp[4], Z: lvp[8]}.Len()
	}
	turned, err := cfg.FitBounds(math3d.Vec3{X: 0.7, Y: -1.0, Z: 0.2}, bounds)
	if err != nil {
		t.Fatalf("turned FitBounds: %v", err)
	}
	if w0, w1 := width(base), width(turned); math.Abs(float64(w0-w1)) > 1e-4*float64(w0) {
		t.Errorf("frustum width changed with light direction: %v -> %v", w0, w1)
	}
}

// TestDirectionalShadowLightViewProjDepthRange pins DepthRange: the slab is centred on Target, with Target at depth 0.5 and the slab's faces DepthRange/2 along the light axis at depths 0 and 1.
func TestDirectionalShadowLightViewProjDepthRange(t *testing.T) {
	cfg := render3d.DefaultDirectionalShadowConfig()
	cfg.Direction = math3d.Vec3{X: -0.4, Y: -1.0, Z: -0.3}
	cfg.Target = math3d.Vec3{X: 2, Y: 1, Z: -3}
	cfg.DepthRange = 40
	cfg.TexelSnap = false
	lvp, err := cfg.LightViewProj()
	if err != nil {
		t.Fatalf("LightViewProj: %v", err)
	}
	dir := cfg.Direction.Norm()
	for _, tc := range []struct {
		along, want float32
	}{{0, 0.5}, {-20, 0}, {20, 1}, {-10, 0.25}} {
		p := cfg.Target.Add(dir.Scale(tc.along))
		if got := lvp.TransformPoint(p).Z; !math3d.ApproxEqual(got, tc.want, 1e-4) {
			t.Errorf("depth %v along the light from Target = %v, want %v", tc.along, got, tc.want)
		}
	}
	cfg.DepthRange = 0
	lvp, err = cfg.LightViewProj()
	if err != nil {
		t.Fatalf("LightViewProj default: %v", err)
	}
	// The default range is max(OrthoSize*2, 100) = 100 for OrthoSize 20.
	if got := lvp.TransformPoint(cfg.Target.Add(dir.Scale(50))).Z; !math3d.ApproxEqual(got, 1, 1e-4) {
		t.Errorf("default depth range: point 50 past Target at depth %v, want 1", got)
	}
}

func TestShadowUniformsLayoutAndZeroAlloc(t *testing.T) {
	cfg := render3d.DefaultDirectionalShadowConfig()
	cfg.Direction = math3d.Vec3{X: 0, Y: -3, Z: -4} // length 5 -> unit (0, -0.6, -0.8) -> to_light (0, 0.6, 0.8)
	cfg.Resolution = 512
	cfg.DepthBias = 0.002
	cfg.NormalBias = 0.03
	cfg.Strength = 0.85
	cfg.PCFKernel = render3d.ShadowFilterPCF3x3

	lvp, err := cfg.LightViewProj()
	if err != nil {
		t.Fatalf("LightViewProj: %v", err)
	}
	u := cfg.Uniforms(lvp)

	prefix := []byte{0x7F}
	encoded := render3d.AppendShadowUniforms(prefix, u)
	if len(encoded) != 1+render3d.ShadowUniformSize || render3d.ShadowUniformSize != 96 {
		t.Fatalf("encoded len=%d ShadowUniformSize=%d, want 97 and 96", len(encoded), render3d.ShadowUniformSize)
	}
	if encoded[0] != 0x7F {
		t.Fatal("prefix byte overwritten")
	}
	body := encoded[1:]
	readF := func(i int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(body[i*4:]))
	}
	for i := range 16 {
		if readF(i) != lvp[i] {
			t.Fatalf("LightViewProj[%d] = %v, want %v", i, readF(i), lvp[i])
		}
	}
	if !math3d.ApproxEqual(readF(16), 0, 1e-6) || !math3d.ApproxEqual(readF(17), 0.6, 1e-6) || !math3d.ApproxEqual(readF(18), 0.8, 1e-6) {
		t.Fatalf("to_light = (%v, %v, %v), want (0, 0.6, 0.8)", readF(16), readF(17), readF(18))
	}
	if readF(19) != 0.85 || readF(20) != 0.002 || readF(21) != 0.03 || readF(22) != 1.0/512.0 || readF(23) != 1.0 {
		t.Fatalf("params = (%v, %v, %v, %v, %v)", readF(19), readF(20), readF(21), readF(22), readF(23))
	}

	// Zero-allocation check for AppendShadowUniforms and Encode.
	buf := make([]byte, 0, render3d.ShadowUniformSize)
	var encBuf [render3d.ShadowUniformSize]byte
	allocs := testing.AllocsPerRun(100, func() {
		buf = render3d.AppendShadowUniforms(buf[:0], u)
		u.Encode(encBuf[:])
	})
	if allocs != 0 {
		t.Fatalf("uniform packing allocated %v times, want 0", allocs)
	}

	// Encode panics when len(dst) < ShadowUniformSize.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Encode on short slice did not panic")
			}
		}()
		u.Encode(make([]byte, 0, render3d.ShadowUniformSize))
	}()

	// Uniforms built by hand skip DirectionalShadowConfig.Validate, so encoding must keep NaN and infinity away from the shader: a broken matrix or strength disables the shadow, and broken biases and texel size become 0.
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	for name, bad := range map[string]render3d.ShadowUniforms{
		"nan strength":   {LightViewProj: lvp, LightDirection: cfg.Direction, Strength: nan, DepthBias: nan, NormalBias: inf, TexelSize: nan},
		"inf strength":   {LightViewProj: lvp, LightDirection: math3d.Vec3{X: nan}, Strength: inf, DepthBias: -inf, NormalBias: nan, TexelSize: -inf},
		"nan matrix":     {LightViewProj: math3d.Mat4{nan}, LightDirection: cfg.Direction, Strength: 1},
		"unknown filter": {LightViewProj: lvp, LightDirection: cfg.Direction, Strength: 1, PCFKernel: 2},
	} {
		body := render3d.AppendShadowUniforms(nil, bad)
		for i := 16; i < 24; i++ {
			if f := math.Float32frombits(binary.LittleEndian.Uint32(body[i*4:])); !(f == f) || math.IsInf(float64(f), 0) {
				t.Errorf("%s: encoded float %d = %v, want finite", name, i, f)
			}
		}
		strength := math.Float32frombits(binary.LittleEndian.Uint32(body[19*4:]))
		filter := math.Float32frombits(binary.LittleEndian.Uint32(body[23*4:]))
		switch name {
		case "unknown filter":
			if strength != 1 || filter != 0 {
				t.Errorf("%s: strength %v filter %v, want 1 and 0 (hard)", name, strength, filter)
			}
		default:
			if strength != 0 {
				t.Errorf("%s: strength %v, want 0 (shadow disabled)", name, strength)
			}
		}
	}

	// ShadowWGSL is ShadowWGSLForGroup at ShadowGroup, the slot after the lit PBR pipeline's camera, lights and per-draw groups.
	if render3d.ShadowWGSLForGroup(render3d.ShadowGroup) != render3d.ShadowWGSL {
		t.Fatal("ShadowWGSLForGroup(ShadowGroup) differs from ShadowWGSL")
	}
	if _, err := gpu.CompileWGSL(render3d.LitPBRShadowWGSL); err != nil {
		t.Fatalf("CompileWGSL(LitPBRShadowWGSL): %v", err)
	}

	// Verify ShadowWGSLForGroup compiles cleanly at an alternate group slot.
	customWGSL := render3d.CameraWGSL + "\n" + render3d.ShadowWGSLForGroup(2) + `
@vertex fn vs_main(@location(0) pos: vec3<f32>) -> @builtin(position) vec4<f32> {
	return camera.view_proj * vec4<f32>(pos, 1.0);
}
@fragment fn fs_main() -> @location(0) vec4<f32> {
	let v = sample_directional_shadow(vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(0.0, 1.0, 0.0));
	return vec4<f32>(v, v, v, 1.0);
}
`
	if _, err := gpu.CompileWGSL(customWGSL); err != nil {
		t.Fatalf("CompileWGSL(ShadowWGSLForGroup(2)): %v", err)
	}
}

func TestEvaluateDirectionalShadowCPUReference(t *testing.T) {
	// Synthetic 8x8 depth grid: left half (x < 4) has an occluder at depth 0.25, right half is clear (1.0).
	const size = 8
	depths := make([]float32, size*size)
	for y := range size {
		for x := range size {
			if x < 4 {
				depths[y*size+x] = 0.25
			} else {
				depths[y*size+x] = 1.0
			}
		}
	}
	grid := render3d.ShadowDepthGrid{Width: size, Height: size, Depths: depths}

	// Identity LightViewProj maps world (x, y, z) directly to NDC (x, y, z):
	// u = x*0.5 + 0.5, v = 0.5 - y*0.5, depth = z.
	u := render3d.ShadowUniforms{
		LightViewProj:  math3d.Mat4Identity(),
		LightDirection: math3d.Vec3{Y: -1},
		Strength:       1.0,
		DepthBias:      0.002,
		NormalBias:     0,
		TexelSize:      1.0 / float32(size),
		PCFKernel:      render3d.ShadowFilterHard,
	}

	// Deep inside occluded left half -> 0.0.
	if got := render3d.EvaluateDirectionalShadow(math3d.Vec3{X: -0.6, Y: 0, Z: 0.6}, math3d.Vec3Up(), u, grid); got != 0 {
		t.Errorf("hard umbra visibility = %v, want 0", got)
	}
	// Deep inside lit right half -> 1.0.
	if got := render3d.EvaluateDirectionalShadow(math3d.Vec3{X: 0.6, Y: 0, Z: 0.6}, math3d.Vec3Up(), u, grid); got != 1 {
		t.Errorf("hard lit visibility = %v, want 1", got)
	}
	// Outside frustum -> always 1.0.
	if got := render3d.EvaluateDirectionalShadow(math3d.Vec3{X: 1.5, Y: 0, Z: 0.6}, math3d.Vec3Up(), u, grid); got != 1 {
		t.Errorf("outside frustum visibility = %v, want 1", got)
	}
	// Strength 0.5 blends umbra to 0.5.
	uHalf := u
	uHalf.Strength = 0.5
	if got := render3d.EvaluateDirectionalShadow(math3d.Vec3{X: -0.6, Y: 0, Z: 0.6}, math3d.Vec3Up(), uHalf, grid); !math3d.ApproxEqual(got, 0.5, 1e-6) {
		t.Errorf("half strength umbra = %v, want 0.5", got)
	}
	// Strength 0 disables shadow completely.
	uZero := u
	uZero.Strength = 0
	if got := render3d.EvaluateDirectionalShadow(math3d.Vec3{X: -0.6, Y: 0, Z: 0.6}, math3d.Vec3Up(), uZero, grid); got != 1 {
		t.Errorf("zero strength = %v, want 1", got)
	}
	// Zero normal falls back cleanly without NaN.
	if got := render3d.EvaluateDirectionalShadow(math3d.Vec3{X: 0.6, Y: 0, Z: 0.6}, math3d.Vec3Zero(), u, grid); got != 1 {
		t.Errorf("zero normal lit = %v, want 1", got)
	}
	// PCF 3x3 right on the shadow boundary (x = 0 -> u = 0.5) must produce an intermediate penumbra value in (0, 1).
	uPCF := u
	uPCF.PCFKernel = render3d.ShadowFilterPCF3x3
	penumbra := render3d.EvaluateDirectionalShadow(math3d.Vec3{X: 0, Y: 0, Z: 0.6}, math3d.Vec3Up(), uPCF, grid)
	if !(penumbra > 0.1 && penumbra < 0.9) {
		t.Errorf("PCF boundary visibility = %v, want intermediate value in (0.1, 0.9)", penumbra)
	}
	// An unknown filter is encoded as hard, and the CPU reference samples it as hard too.
	uUnknown := u
	uUnknown.PCFKernel = 2
	boundary := math3d.Vec3{X: 0, Y: 0, Z: 0.6}
	if got, want := render3d.EvaluateDirectionalShadow(boundary, math3d.Vec3Up(), uUnknown, grid), render3d.EvaluateDirectionalShadow(boundary, math3d.Vec3Up(), u, grid); got != want {
		t.Errorf("unknown filter visibility = %v, want the hard result %v", got, want)
	}
	// NaN strength disables the shadow, as the encoded uniforms do.
	uNaN := u
	uNaN.Strength = float32(math.NaN())
	if got := render3d.EvaluateDirectionalShadow(math3d.Vec3{X: -0.6, Y: 0, Z: 0.6}, math3d.Vec3Up(), uNaN, grid); got != 1 {
		t.Errorf("NaN strength visibility = %v, want 1", got)
	}
}

func TestDirectionalShadowMatchesReferenceHardAndPCF(t *testing.T) {
	baseCfg := render3d.DefaultDirectionalShadowConfig()
	baseCfg.Direction = shadowLightDir
	baseCfg.Resolution = 128
	baseCfg.DepthBias = 0.002
	baseCfg.NormalBias = 0.03
	baseCfg.Strength = 1.0
	baseCfg.TexelSnap = true

	// Render Hard shadow reference.
	cfgHard := baseCfg
	cfgHard.PCFKernel = render3d.ShadowFilterHard
	hHard := newShadowSoftwareHarness(t, 128, render3d.ShadowFilterHard)
	imgHard, uHard := hHard.render(t, cfgHard)
	assertReference(t, "shadow_directional_hard", imgHard)

	// Render 3x3 PCF shadow reference.
	cfgPCF := baseCfg
	cfgPCF.PCFKernel = render3d.ShadowFilterPCF3x3
	hPCF := newShadowSoftwareHarness(t, 128, render3d.ShadowFilterPCF3x3)
	imgPCF, uPCF := hPCF.render(t, cfgPCF)
	assertReference(t, "shadow_directional_pcf", imgPCF)

	// Read back the GPU depth shadow maps: the CPU reference must reproduce every floor pixel from them.
	depthGrid := hHard.readShadowDepth(t)
	depthGridPCF := hPCF.readShadowDepth(t)

	// floorPixel returns the floor point seen through the center of pixel (px, py) and the GPU color there, or ok=false when the pixel does not show the open floor: off the floor, or on or next to the red pillar or the blue block (the floor is neutral gray, R close to B).
	cam := hHard.scene.cam
	floorPixel := func(img *image.RGBA, px, py int) (wp math3d.Vec3, got color.RGBA, ok bool) {
		if px < 1 || px >= shadowRefWidth-1 || py < 1 || py >= shadowRefHeight-1 {
			return wp, got, false
		}
		ray := cam.ScreenRay(float32(px)+0.5, float32(py)+0.5, shadowRefWidth, shadowRefHeight)
		if ray.Dir.Y >= 0 {
			return wp, got, false
		}
		wp = ray.At(-ray.Origin.Y / ray.Dir.Y)
		if math.Abs(float64(wp.X)) > 5.4 || math.Abs(float64(wp.Z)) > 5.4 {
			return wp, got, false
		}
		for _, off := range [5][2]int{{0, 0}, {-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nc := img.RGBAAt(px+off[0], py+off[1])
			if math.Abs(float64(nc.R)-float64(nc.B)) > 20 {
				return wp, got, false
			}
		}
		return wp, img.RGBAAt(px, py), true
	}
	near := func(got color.RGBA, want [3]float64, tol float64) bool {
		return math.Abs(float64(got.R)-want[0]) <= tol && math.Abs(float64(got.G)-want[1]) <= tol && math.Abs(float64(got.B)-want[2]) <= tol
	}

	// Check every open floor pixel against the CPU reference at the floor point under the pixel's center. With hard shadows, every pixel the reference calls lit must match the fully lit color (no shadow acne) and every pixel in umbra the shadowed color, in which the fill light (directional light 1) still shines; pixels within a texel of the shadow edge may round either way and are skipped. With PCF every pixel, penumbra included, must match the reference's filtered visibility.
	litCount, umbraCount, pcfPenumbraCount := 0, 0, 0
	var litLumaSum, umbraLumaSum float64
	edgeStep := float32(0.5) * hHard.scene.sceneBounds.Max.Sub(hHard.scene.sceneBounds.Min).Len() / 128
	for py := 0; py < shadowRefHeight; py++ {
		for px := 0; px < shadowRefWidth; px++ {
			if wp, got, ok := floorPixel(imgHard, px, py); ok {
				cpuVis := render3d.EvaluateDirectionalShadow(wp, math3d.Vec3Up(), uHard, depthGrid)
				stable := true
				for _, d := range [4]math3d.Vec3{{X: edgeStep}, {X: -edgeStep}, {Z: edgeStep}, {Z: -edgeStep}} {
					if render3d.EvaluateDirectionalShadow(wp.Add(d), math3d.Vec3Up(), uHard, depthGrid) != cpuVis {
						stable = false
					}
				}
				if stable {
					want := hHard.scene.expectedFloor(wp, cpuVis)
					if cpuVis == 1 {
						litCount++
						litLumaSum += shadowLuma(got)
					} else {
						umbraCount++
						umbraLumaSum += shadowLuma(got)
					}
					if !near(got, want, 8) {
						t.Errorf("hard: floor point %v px=(%d,%d) vis=%v got=%v want≈(%.0f,%.0f,%.0f)", wp, px, py, cpuVis, got, want[0], want[1], want[2])
					}
				}
			}
			if wp, got, ok := floorPixel(imgPCF, px, py); ok {
				cpuVis := render3d.EvaluateDirectionalShadow(wp, math3d.Vec3Up(), uPCF, depthGridPCF)
				if cpuVis > 0.05 && cpuVis < 0.95 {
					pcfPenumbraCount++
				}
				if want := hPCF.scene.expectedFloor(wp, cpuVis); !near(got, want, 8) {
					t.Errorf("pcf: floor point %v px=(%d,%d) vis=%.3f got=%v want≈(%.0f,%.0f,%.0f)", wp, px, py, cpuVis, got, want[0], want[1], want[2])
				}
			}
		}
	}

	if litCount < 200 {
		t.Fatalf("too few lit floor samples (%d)", litCount)
	}
	if umbraCount < 50 {
		t.Fatalf("too few umbra floor samples (%d) — shadow did not land on floor", umbraCount)
	}
	if pcfPenumbraCount < 20 {
		t.Errorf("too few PCF penumbra floor samples (%d) to compare the filtered edge", pcfPenumbraCount)
	}
	avgLit := litLumaSum / float64(litCount)
	avgUmbra := umbraLumaSum / float64(umbraCount)
	if avgLit-avgUmbra < 60 {
		t.Fatalf("shadow contrast too weak: avgLit=%.1f avgUmbra=%.1f", avgLit, avgUmbra)
	}

	// Independently of the depth readback, the block's shadow must fall where its center projects along the light onto the floor, and a floor point far from both casters must be fully lit.
	blockCenter := math3d.Vec3{X: -0.6, Y: 1.9, Z: 0.8}
	blockShadowCenter := blockCenter.Add(shadowLightDir.Scale(-blockCenter.Y / shadowLightDir.Y))
	for name, img := range map[string]*image.RGBA{"hard": imgHard, "pcf": imgPCF} {
		for _, tc := range []struct {
			what string
			at   math3d.Vec3
			vis  float32
		}{
			{"block shadow center", blockShadowCenter, 0},
			{"far floor", math3d.Vec3{X: 2.6, Y: 0, Z: 2.4}, 1},
		} {
			scr, _, visible := cam.WorldToScreen(tc.at, shadowRefWidth, shadowRefHeight)
			if !visible {
				t.Fatalf("%s: %s %v is off screen", name, tc.what, tc.at)
			}
			wp, got, ok := floorPixel(img, int(scr.X), int(scr.Y))
			if !ok {
				t.Fatalf("%s: %s %v is not open floor on screen", name, tc.what, tc.at)
			}
			if want := hHard.scene.expectedFloor(wp, tc.vis); !near(got, want, 8) {
				t.Errorf("%s: %s px=(%d,%d) got=%v, want ≈(%.0f,%.0f,%.0f) for visibility %v", name, tc.what, int(scr.X), int(scr.Y), got, want[0], want[1], want[2], tc.vis)
			}
		}
	}

	// PCF must produce clearly more intermediate penumbra floor pixels than hard shadows.
	countPenumbra := func(img *image.RGBA) int {
		n := 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := img.RGBAAt(x, y)
				if math.Abs(float64(c.R)-float64(c.B)) <= 20 {
					l := shadowLuma(c)
					if l > avgUmbra+15 && l < avgLit-15 {
						n++
					}
				}
			}
		}
		return n
	}
	hardPenumbra := countPenumbra(imgHard)
	pcfPenumbra := countPenumbra(imgPCF)
	if pcfPenumbra <= hardPenumbra+15 {
		t.Errorf("PCF penumbra pixel count (%d) should clearly exceed Hard (%d)", pcfPenumbra, hardPenumbra)
	}

	// Switching PCFKernel on the same DirectionalShadowMap switches between the nearest and linear comparison samplers.
	bgHard := hHard.shadowMap.BindGroup()
	imgSwitched, _ := hHard.render(t, cfgPCF)
	if hHard.shadowMap.BindGroup() == bgHard {
		t.Error("switching PCFKernel did not switch BindGroup from nearest to linear comparison sampler")
	}
	assertReference(t, "shadow_directional_pcf", imgSwitched)
}

// shadowViewApp mounts a gpu.View rendering the directional shadow scene (retained when retained is true).
func shadowViewApp(t *testing.T, retained bool, filter render3d.ShadowFilter, opts ...nsgotest.Option) (*nsgotest.App, **render3d.DirectionalShadowMap, *int) {
	t.Helper()
	const w, h = shadowRefWidth, shadowRefHeight
	var (
		camBuf    *render3d.CameraBuffer
		lightBuf  *render3d.GPULightBuffer
		draws     [3]*render3d.PBRDrawBuffer
		shadowMap *render3d.DirectionalShadowMap
		litPipe   *gpu.Pipeline
		floorMesh *render3d.GPUMesh
		boxMesh   *render3d.GPUMesh
		frames    int
		gpuErr    error
	)
	scene := newShadowTestScene(w, h)
	cfg := render3d.DefaultDirectionalShadowConfig()
	cfg.Direction = shadowLightDir
	cfg.Resolution = 128
	cfg.DepthBias = 0.002
	cfg.NormalBias = 0.03
	cfg.PCFKernel = filter
	cfg.Strength = 1.0
	cfg.TexelSnap = true

	opts = append([]nsgotest.Option{nsgotest.Size(w, h), nsgotest.ManualFrames()}, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "shadow-viewport",
			Style:       core.Style{Width: core.Pt(w), Height: core.Pt(h)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnError: func(err gpu.Error) {
				gpuErr = err
			},
			OnReady: func(dev *gpu.Device) {
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, dev)
				if err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				if shadowMap == nil || !retained {
					smOpts := render3d.DefaultShadowMapOptions()
					smOpts.Resolution = 128
					smOpts.PCFKernel = filter
					smOpts.DepthBias = 1
					smOpts.DepthBiasSlopeScale = 1.5
					smOpts.Retained = retained
					shadowMap, err = render3d.NewDirectionalShadowMap(cx, dev, smOpts)
					if err != nil {
						t.Fatalf("NewDirectionalShadowMap: %v", err)
					}
				}
				if floorMesh == nil || !retained {
					floorMesh, err = render3d.UploadMesh(cx, dev, shadowFloorMesh(5.5), render3d.MeshUpload{Label: "floor", Retained: retained})
					if err != nil {
						t.Fatalf("UploadMesh floor: %v", err)
					}
					boxMesh, err = render3d.UploadMesh(cx, dev, shadowBoxMesh(), render3d.MeshUpload{Label: "box", Retained: retained})
					if err != nil {
						t.Fatalf("UploadMesh box: %v", err)
					}
				}
				lightBuf, err = render3d.NewLightBuffer(cx, dev, shadowRig, render3d.LightBufferOptions{Label: "shadow-lights"})
				if err != nil {
					t.Fatalf("NewLightBuffer: %v", err)
				}
				draws = shadowDrawBuffers(t, cx, dev, scene, false)
				litPipe, err = render3d.NewLitPBRShadowPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), draws[0].Layout(), shadowMap.Layout(), gpu.FormatDepth24Plus)
				if err != nil {
					t.Fatalf("NewLitPBRShadowPipeline: %v", err)
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				frames++
				dev := frame.Device()
				if _, err := shadowMap.UpdateBounds(dev, cfg, scene.sceneBounds); err != nil {
					t.Fatalf("UpdateBounds: %v", err)
				}
				camBuf.WriteCamera(scene.cam)

				// Shadow depth pass on frame.
				if sPass := shadowMap.BeginShadowPass(dev, frame); sPass != nil {
					shadowMap.DrawCaster(sPass, boxMesh, scene.pillarModel)
					shadowMap.DrawCaster(sPass, boxMesh, scene.blockModel)
					sPass.End()
				}

				// Main lit pass on frame.
				mPass := frame.BeginRenderPass(gpu.RenderPassDesc{Label: "main"})
				mPass.ClearColor(shadowClearColor)
				mPass.ClearDepth(1.0)
				drawShadowScene(mPass, litPipe, camBuf, lightBuf, shadowMap, draws, floorMesh, boxMesh)
				mPass.End()
				frame.Submit()
			},
		})
	}, opts...)
	t.Cleanup(func() {
		if gpuErr != nil {
			t.Errorf("unexpected GPU error: %v", gpuErr)
		}
	})
	return app, &shadowMap, &frames
}

func TestDirectionalShadowDrawsInGPUViewProfiles(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			app, _, frames := shadowViewApp(t, false, render3d.ShadowFilterPCF3x3, p.opts...)
			before := *frames
			app.Advance(16 * time.Millisecond)
			if *frames == before {
				t.Fatal("OnFrame did not run")
			}
			img, err := app.GPUReadPixels("id:shadow-viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			assertReference(t, "shadow_directional_pcf", img)
		})
	}
}

func TestRetainedDirectionalShadowSurvivesDeviceLoss(t *testing.T) {
	app, smp, frames := shadowViewApp(t, true, render3d.ShadowFilterHard)
	app.Advance(16 * time.Millisecond)
	if *frames == 0 {
		t.Fatal("initial frame did not run")
	}
	first := *smp
	if !first.Valid() {
		t.Fatal("shadow map not valid before device loss")
	}
	imgBefore, err := app.GPUReadPixels("id:shadow-viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	assertReference(t, "shadow_directional_hard", imgBefore)

	app.TriggerGPUDeviceLost("id:shadow-viewport", nsgotest.GPULostContextLost, "context lost")
	if first.Valid() {
		t.Fatal("shadow map still valid while device is lost")
	}

	app.RestoreGPUDevice("id:shadow-viewport")
	if *smp != first || !first.Valid() {
		t.Fatalf("retained shadow map not restored: same=%v valid=%v", *smp == first, first.Valid())
	}

	before := *frames
	app.Advance(16 * time.Millisecond)
	if *frames == before {
		t.Fatal("OnFrame did not run after device restore")
	}
	imgAfter, err := app.GPUReadPixels("id:shadow-viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	assertReference(t, "shadow_directional_hard", imgAfter)
}

func TestDirectionalShadowLifecycleAndScopeDisposal(t *testing.T) {
	var (
		sm   *render3d.DirectionalShadowMap
		show *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(16), Height: core.Pt(16)},
				OnReady: func(dev *gpu.Device) {
					var err error
					sm, err = render3d.NewDirectionalShadowMap(cx, dev, render3d.ShadowMapOptions{
						Resolution: 64,
						MaxCasters: 4,
					})
					if err != nil {
						t.Fatalf("NewDirectionalShadowMap: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(16, 16), nsgotest.ManualFrames())

	if !sm.Valid() || sm.Resolution() != 64 || sm.MaxCasters() != 4 {
		t.Fatalf("mounted shadow map: valid=%v res=%d maxCasters=%d", sm.Valid(), sm.Resolution(), sm.MaxCasters())
	}
	if sm.ByteSize() < 64*64*4+render3d.ShadowUniformSize {
		t.Errorf("ByteSize = %d, unexpectedly small", sm.ByteSize())
	}

	app.Post(func() { show.Set(false) })
	app.Settle()
	if sm.Valid() {
		t.Error("shadow map still valid after its scope was disposed")
	}

	// Explicit Close idempotency, nil safety, and MaxCasters exhaustion.
	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()
	sm2, err := render3d.NewDirectionalShadowMap(nil, dev, render3d.ShadowMapOptions{Resolution: 32, MaxCasters: 1})
	if err != nil {
		t.Fatalf("NewDirectionalShadowMap: %v", err)
	}
	box, err := render3d.UploadMesh(nil, dev, shadowBoxMesh(), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	defer func() { _ = box.Close() }()

	enc := dev.BeginEncoder()
	pass := sm2.BeginShadowPass(dev, enc)
	if !sm2.DrawCaster(pass, box, math3d.Mat4Identity()) {
		t.Fatal("first DrawCaster failed")
	}
	if sm2.DrawCaster(pass, box, math3d.Mat4Identity()) {
		t.Fatal("second DrawCaster succeeded beyond MaxCasters=1")
	}
	pass.End()
	dev.Submit(enc.Finish())

	if err := sm2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if sm2.Valid() {
		t.Fatal("closed shadow map still valid")
	}
	if err := sm2.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	var nilSM *render3d.DirectionalShadowMap
	if nilSM.Valid() || nilSM.Resolution() != 0 || nilSM.MaxCasters() != 0 || nilSM.ByteSize() != 0 || nilSM.Close() != nil {
		t.Error("nil DirectionalShadowMap must be safe and report zero/invalid")
	}
}

func BenchmarkAppendShadowUniforms(b *testing.B) {
	cfg := render3d.DefaultDirectionalShadowConfig()
	lvp, _ := cfg.LightViewProj()
	u := cfg.Uniforms(lvp)
	buf := make([]byte, 0, render3d.ShadowUniformSize)
	b.SetBytes(render3d.ShadowUniformSize)
	b.ReportAllocs()
	for b.Loop() {
		buf = render3d.AppendShadowUniforms(buf[:0], u)
	}
}

func BenchmarkEvaluateDirectionalShadow(b *testing.B) {
	const size = 64
	depths := make([]float32, size*size)
	for i := range depths {
		depths[i] = 0.5
	}
	grid := render3d.ShadowDepthGrid{Width: size, Height: size, Depths: depths}
	u := render3d.ShadowUniforms{
		LightViewProj:  math3d.Mat4Identity(),
		LightDirection: math3d.Vec3{Y: -1},
		Strength:       1.0,
		DepthBias:      0.002,
		NormalBias:     0.02,
		TexelSize:      1.0 / float32(size),
		PCFKernel:      render3d.ShadowFilterPCF3x3,
	}
	p := math3d.Vec3{X: 0.1, Y: 0.2, Z: 0.4}
	n := math3d.Vec3Up()
	b.ReportAllocs()
	var sink float32
	for b.Loop() {
		sink += render3d.EvaluateDirectionalShadow(p, n, u, grid)
	}
	_ = sink
}
