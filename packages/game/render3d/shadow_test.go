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
	shadowClearColor   = core.RGB(0x182230)
	shadowLightDir     = math3d.Vec3{X: -0.55, Y: -1.0, Z: -0.35}.Norm()
	shadowLightColor   = math3d.Vec3{X: 0.82, Y: 0.80, Z: 0.76}
	shadowAmbientColor = math3d.Vec3{X: 0.18, Y: 0.20, Z: 0.24}
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
	shadowMap  *render3d.DirectionalShadowMap
	litPipe    *gpu.Pipeline
	objLayout  *gpu.BindGroupLayout
	objBuf     *gpu.Buffer
	objBG      *gpu.BindGroup
	objAlign   uint64
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

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

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

	objLayout, err := render3d.ShadowLitObjectLayout(nil, dev, true)
	if err != nil {
		t.Fatalf("ShadowLitObjectLayout: %v", err)
	}
	t.Cleanup(func() { _ = objLayout.Close() })

	align := uint64(dev.Limits().MinUniformBufferAlignment)
	if align < render3d.ShadowLitObjectUniformSize {
		align = 256
	}
	objBuf, err := dev.CreateBuffer(nil, gpu.BufferDesc{
		Label: "shadow-lit-objects",
		Size:  align * 4,
		Usage: gpu.UsageUniform | gpu.UsageCopyDst,
	})
	if err != nil {
		t.Fatalf("CreateBuffer objects: %v", err)
	}
	t.Cleanup(func() { _ = objBuf.Close() })

	objBG, err := dev.CreateBindGroupDesc(nil, gpu.BindGroupDesc{
		Label:  "shadow-lit-objects",
		Layout: objLayout,
		Entries: []gpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  objBuf,
			Size:    render3d.ShadowLitObjectUniformSize,
		}},
	})
	if err != nil {
		t.Fatalf("CreateBindGroup objects: %v", err)
	}
	t.Cleanup(func() { _ = objBG.Close() })

	litPipe, err := render3d.NewShadowLitPipeline(nil, dev, render3d.ShadowLitPipelineOptions{
		Label:        "shadow-lit",
		CameraLayout: camBuf.Layout(),
		ObjectLayout: objLayout,
		ShadowLayout: sm.Layout(),
		ColorFormat:  gpu.FormatRGBA8Unorm,
		DepthFormat:  gpu.FormatDepth24Plus,
		CullMode:     gpu.CullBack,
	})
	if err != nil {
		t.Fatalf("NewShadowLitPipeline: %v", err)
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
		shadowMap:  sm,
		litPipe:    litPipe,
		objLayout:  objLayout,
		objBuf:     objBuf,
		objBG:      objBG,
		objAlign:   align,
		floorMesh:  floorMesh,
		boxMesh:    boxMesh,
		scene:      newShadowTestScene(shadowRefWidth, shadowRefHeight),
		resolution: res,
	}
}

func (h *shadowSoftwareHarness) render(t *testing.T, cfg render3d.DirectionalShadowConfig) (*image.RGBA, render3d.ShadowUniforms) {
	t.Helper()
	u, err := h.shadowMap.UpdateBounds(h.dev, cfg, h.scene.sceneBounds)
	if err != nil {
		t.Fatalf("UpdateBounds: %v", err)
	}
	h.camBuf.WriteCamera(h.scene.cam)

	// Upload per-object uniforms at aligned dynamic offsets.
	objs := [3]render3d.ShadowLitObjectUniform{
		render3d.NewShadowLitObjectUniform(h.scene.floorModel, math3d.Vec4{X: 0.78, Y: 0.80, Z: 0.82, W: 1}, shadowLightColor, shadowAmbientColor),
		render3d.NewShadowLitObjectUniform(h.scene.pillarModel, math3d.Vec4{X: 0.88, Y: 0.42, Z: 0.30, W: 1}, shadowLightColor, shadowAmbientColor),
		render3d.NewShadowLitObjectUniform(h.scene.blockModel, math3d.Vec4{X: 0.32, Y: 0.68, Z: 0.88, W: 1}, shadowLightColor, shadowAmbientColor),
	}
	var scratch [render3d.ShadowLitObjectUniformSize]byte
	for i, o := range objs {
		data := render3d.AppendShadowLitObjectUniform(scratch[:0], o)
		h.dev.WriteBuffer(h.objBuf, uint64(i)*h.objAlign, data)
	}

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
	mPass.SetPipeline(h.litPipe)
	mPass.SetBindGroup(render3d.CameraGroup, h.camBuf.BindGroup())
	mPass.SetBindGroup(render3d.ShadowGroup, h.shadowMap.BindGroup())

	mPass.SetBindGroup(1, h.objBG, 0)
	h.floorMesh.Draw(mPass)

	mPass.SetBindGroup(1, h.objBG, uint32(h.objAlign))
	h.boxMesh.Draw(mPass)

	mPass.SetBindGroup(1, h.objBG, uint32(2*h.objAlign))
	h.boxMesh.Draw(mPass)

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
		{"inverted near far", func(c *render3d.DirectionalShadowConfig) { c.Near, c.Far = 10, 5 }},
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
	if _, err := render3d.ShadowLitObjectLayout(nil, nil, false); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("ShadowLitObjectLayout(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewShadowLitPipeline(nil, nil, render3d.ShadowLitPipelineOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewShadowLitPipeline(nil dev) = %v, want ErrDeviceLost", err)
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

	// Zero-allocation check for AppendShadowUniforms, Encode, and AppendShadowLitObjectUniform.
	buf := make([]byte, 0, render3d.ShadowUniformSize)
	var encBuf [render3d.ShadowUniformSize]byte
	objBuf := make([]byte, 0, render3d.ShadowLitObjectUniformSize)
	obj := render3d.NewShadowLitObjectUniform(math3d.Mat4Identity(), math3d.Vec4{X: 1, Y: 1, Z: 1, W: 1}, shadowLightColor, shadowAmbientColor)
	allocs := testing.AllocsPerRun(100, func() {
		buf = render3d.AppendShadowUniforms(buf[:0], u)
		u.Encode(encBuf[:])
		objBuf = render3d.AppendShadowLitObjectUniform(objBuf[:0], obj)
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

	// Verify ShadowWGSLForGroup compiles cleanly at an alternate group slot.
	customWGSL := render3d.CameraWGSL + "\n" + render3d.ShadowWGSLForGroup(3) + `
@vertex fn vs_main(@location(0) pos: vec3<f32>) -> @builtin(position) vec4<f32> {
	return camera.view_proj * vec4<f32>(pos, 1.0);
}
@fragment fn fs_main() -> @location(0) vec4<f32> {
	let v = sample_directional_shadow(vec3<f32>(0.0, 0.0, 0.0), vec3<f32>(0.0, 1.0, 0.0));
	return vec4<f32>(v, v, v, 1.0);
}
`
	if _, err := gpu.CompileWGSL(customWGSL); err != nil {
		t.Fatalf("CompileWGSL(ShadowWGSLForGroup(3)): %v", err)
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

	// Read back the GPU depth shadow map and verify CPU EvaluateDirectionalShadow agrees with GPU pixels across the floor.
	depthGrid := hHard.readShadowDepth(t)
	depthGridPCF := hPCF.readShadowDepth(t)

	// Check a grid of floor sample points projected onto screen space:
	// - Every unoccluded floor sample must be brightly lit with zero shadow acne.
	// - Every occluded floor sample must be visibly darker (umbra).
	// - Both GPU pixel luma and CPU EvaluateDirectionalShadow must agree on lit vs shadowed points.
	litCount := 0
	umbraCount := 0
	var litLumaSum, umbraLumaSum float64
	var litLumaMin, litLumaMax float64 = 1e9, -1e9
	floorColor := math3d.Vec3{X: 0.78, Y: 0.80, Z: 0.82}
	toLight := shadowLightDir.Neg().Norm()
	ndl := max(math3d.Vec3Up().Dot(toLight), 0)

	// Sample a fine grid across the open floor and verify both lit and umbra pixels match CPU EvaluateDirectionalShadow.
	for iz := -8; iz <= 8; iz++ {
		for ix := -10; ix <= 10; ix++ {
			wp := math3d.Vec3{X: float32(ix) * 0.35, Y: 0, Z: float32(iz) * 0.35}
			scr, _, vis := hHard.scene.cam.WorldToScreen(wp, shadowRefWidth, shadowRefHeight)
			if !vis {
				continue
			}
			px, py := int(scr.X), int(scr.Y)
			if px < 4 || px >= shadowRefWidth-4 || py < 4 || py >= shadowRefHeight-4 {
				continue
			}
			got := imgHard.RGBAAt(px, py)
			// Skip background or colored box/pillar pixels (the floor is neutral gray: R ~ G ~ B and brighter than the dark blue clear color).
			if math.Abs(float64(got.R)-float64(got.B)) > 20 || (got.R < 32 && got.G < 40 && got.B < 55) {
				continue
			}
			// Guard against screen pixels right on a silhouette edge where a neighbor pixel is a colored box.
			edgeColored := false
			for _, off := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				nc := imgHard.RGBAAt(px+off[0], py+off[1])
				if math.Abs(float64(nc.R)-float64(nc.B)) > 20 {
					edgeColored = true
					break
				}
			}
			if edgeColored {
				continue
			}

			cpuVis := render3d.EvaluateDirectionalShadow(wp, math3d.Vec3Up(), uHard, depthGrid)
			wantR := float64(floorColor.X * (shadowAmbientColor.X + shadowLightColor.X*ndl*cpuVis) * 255)
			wantG := float64(floorColor.Y * (shadowAmbientColor.Y + shadowLightColor.Y*ndl*cpuVis) * 255)
			wantB := float64(floorColor.Z * (shadowAmbientColor.Z + shadowLightColor.Z*ndl*cpuVis) * 255)

			luma := shadowLuma(got)
			if cpuVis > 0.95 {
				litCount++
				litLumaSum += luma
				litLumaMin = min(litLumaMin, luma)
				litLumaMax = max(litLumaMax, luma)
				// Zero shadow acne: lit floor pixels must match full lit color tightly.
				if math.Abs(float64(got.R)-wantR) > 10 || math.Abs(float64(got.G)-wantG) > 10 || math.Abs(float64(got.B)-wantB) > 10 {
					t.Errorf("lit floor point %v px=(%d,%d) got=%v want≈(%.0f,%.0f,%.0f) — possible shadow acne", wp, px, py, got, wantR, wantG, wantB)
				}
			} else if cpuVis < 0.05 {
				// Confirm that at least one sample inside a solid 3x3 shadow neighborhood matches umbra (avoiding 1-pixel shadow-edge rounding differences).
				if render3d.EvaluateDirectionalShadow(wp.Add(math3d.Vec3{X: 0.15}), math3d.Vec3Up(), uHard, depthGrid) < 0.05 &&
					render3d.EvaluateDirectionalShadow(wp.Add(math3d.Vec3{X: -0.15}), math3d.Vec3Up(), uHard, depthGrid) < 0.05 {
					umbraCount++
					umbraLumaSum += luma
					if math.Abs(float64(got.R)-wantR) > 10 || math.Abs(float64(got.G)-wantG) > 10 || math.Abs(float64(got.B)-wantB) > 10 {
						t.Errorf("umbra floor point %v px=(%d,%d) got=%v want≈(%.0f,%.0f,%.0f)", wp, px, py, got, wantR, wantG, wantB)
					}
				}
			}
		}
	}

	if litCount < 20 {
		t.Fatalf("too few lit floor samples (%d)", litCount)
	}
	if umbraCount < 5 {
		t.Fatalf("too few umbra floor samples (%d) — shadow did not land on floor", umbraCount)
	}
	avgLit := litLumaSum / float64(litCount)
	avgUmbra := umbraLumaSum / float64(umbraCount)
	if avgLit-avgUmbra < 80 {
		t.Fatalf("shadow contrast too weak: avgLit=%.1f avgUmbra=%.1f", avgLit, avgUmbra)
	}
	// Uniform lit floor (zero acne variance).
	if litLumaMax-litLumaMin > 4 {
		t.Errorf("lit floor luma variance too high (min=%.1f max=%.1f) — possible shadow acne", litLumaMin, litLumaMax)
	}

	// Verify PCF produces strictly more intermediate penumbra floor pixels than Hard shadows!
	countPenumbra := func(img *image.RGBA) int {
		n := 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := img.RGBAAt(x, y)
				// Neutral gray floor pixel whose luma lies strictly between umbra and lit.
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

	// Verify dynamic filter switching on the same DirectionalShadowMap switches between Hard and PCF cleanly.
	bgHard := hHard.shadowMap.BindGroup()
	imgSwitched, _ := hHard.render(t, cfgPCF)
	if hHard.shadowMap.BindGroup() == bgHard {
		t.Error("switching PCFKernel did not switch BindGroup from nearest to linear comparison sampler")
	}
	assertReference(t, "shadow_directional_pcf", imgSwitched)

	// Verify CPU PCF evaluation matches lit and umbra regions on the GPU depth readback.
	if pcfLit := render3d.EvaluateDirectionalShadow(math3d.Vec3{X: 3.5, Y: 0, Z: 3.0}, math3d.Vec3Up(), uPCF, depthGridPCF); pcfLit < 0.99 {
		t.Errorf("CPU PCF far lit floor sample = %v, want 1.0", pcfLit)
	}
	// Center of the floating block projected straight along shadowLightDir onto y = 0 must be deep in umbra.
	blockCenter := math3d.Vec3{X: -0.6, Y: 1.9, Z: 0.8}
	tHit := -blockCenter.Y / shadowLightDir.Y
	blockShadowCenter := blockCenter.Add(shadowLightDir.Scale(tHit))
	if pcfUmbra := render3d.EvaluateDirectionalShadow(blockShadowCenter, math3d.Vec3Up(), uPCF, depthGridPCF); pcfUmbra > 0.05 {
		t.Errorf("CPU PCF block shadow center %v = %v, want < 0.05", blockShadowCenter, pcfUmbra)
	}
}

// shadowViewApp mounts a gpu.View rendering the directional shadow scene (retained when retained is true).
func shadowViewApp(t *testing.T, retained bool, filter render3d.ShadowFilter, opts ...nsgotest.Option) (*nsgotest.App, **render3d.DirectionalShadowMap, *int) {
	t.Helper()
	const w, h = shadowRefWidth, shadowRefHeight
	var (
		camBuf    *render3d.CameraBuffer
		shadowMap *render3d.DirectionalShadowMap
		objLayout *gpu.BindGroupLayout
		objBuf    *gpu.Buffer
		objBG     *gpu.BindGroup
		objAlign  uint64
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
				objLayout, err = render3d.ShadowLitObjectLayout(cx, dev, true)
				if err != nil {
					t.Fatalf("ShadowLitObjectLayout: %v", err)
				}
				objAlign = uint64(dev.Limits().MinUniformBufferAlignment)
				if objAlign < render3d.ShadowLitObjectUniformSize {
					objAlign = 256
				}
				objBuf, err = dev.CreateBuffer(cx, gpu.BufferDesc{
					Label: "objects",
					Size:  objAlign * 4,
					Usage: gpu.UsageUniform | gpu.UsageCopyDst,
				})
				if err != nil {
					t.Fatalf("CreateBuffer objects: %v", err)
				}
				objBG, err = dev.CreateBindGroupDesc(cx, gpu.BindGroupDesc{
					Label:  "objects",
					Layout: objLayout,
					Entries: []gpu.BindGroupEntry{{
						Binding: 0,
						Buffer:  objBuf,
						Size:    render3d.ShadowLitObjectUniformSize,
					}},
				})
				if err != nil {
					t.Fatalf("CreateBindGroup objects: %v", err)
				}
				litPipe, err = render3d.NewShadowLitPipeline(cx, dev, render3d.ShadowLitPipelineOptions{
					Label:        "shadow-lit",
					CameraLayout: camBuf.Layout(),
					ObjectLayout: objLayout,
					ShadowLayout: shadowMap.Layout(),
					ColorFormat:  dev.SurfaceFormat(),
					DepthFormat:  gpu.FormatDepth24Plus,
					CullMode:     gpu.CullBack,
				})
				if err != nil {
					t.Fatalf("NewShadowLitPipeline: %v", err)
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				frames++
				dev := frame.Device()
				if _, err := shadowMap.UpdateBounds(dev, cfg, scene.sceneBounds); err != nil {
					t.Fatalf("UpdateBounds: %v", err)
				}
				camBuf.WriteCamera(scene.cam)

				objs := [3]render3d.ShadowLitObjectUniform{
					render3d.NewShadowLitObjectUniform(scene.floorModel, math3d.Vec4{X: 0.78, Y: 0.80, Z: 0.82, W: 1}, shadowLightColor, shadowAmbientColor),
					render3d.NewShadowLitObjectUniform(scene.pillarModel, math3d.Vec4{X: 0.88, Y: 0.42, Z: 0.30, W: 1}, shadowLightColor, shadowAmbientColor),
					render3d.NewShadowLitObjectUniform(scene.blockModel, math3d.Vec4{X: 0.32, Y: 0.68, Z: 0.88, W: 1}, shadowLightColor, shadowAmbientColor),
				}
				var scratch [render3d.ShadowLitObjectUniformSize]byte
				for i, o := range objs {
					dev.WriteBuffer(objBuf, uint64(i)*objAlign, render3d.AppendShadowLitObjectUniform(scratch[:0], o))
				}

				// Shadow depth pass on frame.
				sPass := shadowMap.BeginShadowPass(dev, frame)
				shadowMap.DrawCaster(sPass, boxMesh, scene.pillarModel)
				shadowMap.DrawCaster(sPass, boxMesh, scene.blockModel)
				sPass.End()

				// Main lit pass on frame.
				mPass := frame.BeginRenderPass(gpu.RenderPassDesc{Label: "main"})
				mPass.ClearColor(shadowClearColor)
				mPass.ClearDepth(1.0)
				mPass.SetPipeline(litPipe)
				mPass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
				mPass.SetBindGroup(render3d.ShadowGroup, shadowMap.BindGroup())

				mPass.SetBindGroup(1, objBG, 0)
				floorMesh.Draw(mPass)
				mPass.SetBindGroup(1, objBG, uint32(objAlign))
				boxMesh.Draw(mPass)
				mPass.SetBindGroup(1, objBG, uint32(2*objAlign))
				boxMesh.Draw(mPass)

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
