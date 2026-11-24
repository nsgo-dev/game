package render3d_test

import (
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

var clusterTestMat = render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.9, Y: 0.9, Z: 0.9}, Roughness: 0.6}

func clusterTestFloorMesh(half float32) render3d.Mesh {
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

func clusterTest256PointLights() []render3d.ClusterLight {
	lights := make([]render3d.ClusterLight, 0, 256)
	palette := [...]math3d.Vec3{
		{X: 1.0, Y: 0.25, Z: 0.15},
		{X: 0.15, Y: 1.0, Z: 0.25},
		{X: 0.2, Y: 0.35, Z: 1.0},
		{X: 1.0, Y: 0.85, Z: 0.2},
		{X: 0.9, Y: 0.25, Z: 0.95},
		{X: 0.2, Y: 0.95, Z: 0.95},
	}
	// 16x16 grid spanning X in [-15, 15], Z in [-15, 15] at Y = 1.25.
	for gz := range 16 {
		for gx := range 16 {
			idx := gz*16 + gx
			x := -15.0 + float32(gx)*2.0
			z := -15.0 + float32(gz)*2.0
			lights = append(lights, render3d.NewPointClusterLight(math3d.Vec3{X: x, Y: 1.25, Z: z}, palette[idx%len(palette)], 5.5, 2.8))
		}
	}
	return lights
}

// clusterTestRig is a LightRig with ambient, a dim sun and one rig point light, shaded together with the clustered lights.
func clusterTestRig() render3d.LightRig {
	return render3d.LightRig{
		Ambient:     render3d.AmbientLight{Color: math3d.Vec3{X: 0.2, Y: 0.2, Z: 0.25}, Intensity: 0.2},
		Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{X: 0.3, Y: -1, Z: -0.2}, Color: math3d.Vec3{X: 1, Y: 0.95, Z: 0.9}, Intensity: 0.3}},
		Point:       []render3d.PointLight{{Position: math3d.Vec3{X: 6, Y: 2, Z: 4}, Color: math3d.Vec3{X: 0.6, Y: 0.6, Z: 1}, Intensity: 4, Range: 6}},
	}
}

func clusterTestSRGB(lin math3d.Vec3) color.RGBA {
	srgb := render3d.LinearToSRGB(lin)
	return color.RGBA{
		R: uint8(math.Round(float64(srgb.X) * 255)),
		G: uint8(math.Round(float64(srgb.Y) * 255)),
		B: uint8(math.Round(float64(srgb.Z) * 255)),
		A: 255,
	}
}

func clusterTestAssertPixelNear(t *testing.T, img *image.RGBA, x, y int, want color.RGBA, tol int, label string) {
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
		t.Errorf("%s at (%d,%d): got RGBA(%d,%d,%d,%d), want RGBA(%d,%d,%d,%d) ±%d", label, x, y, got.R, got.G, got.B, got.A, want.R, want.G, want.B, want.A, tol)
	}
}

func clusterTestAssertVecNear(t *testing.T, got, want math3d.Vec3, label string) {
	t.Helper()
	near := func(a, b float32) bool { return math.Abs(float64(a-b)) <= 1e-4*(1+math.Abs(float64(b))) }
	if !near(got.X, want.X) || !near(got.Y, want.Y) || !near(got.Z, want.Z) {
		t.Fatalf("%s: got %v, want %v", label, got, want)
	}
}

// clusterTestGPU holds the camera, per-draw and pipeline resources a clustered draw needs besides the ClusteredLightSystem.
type clusterTestGPU struct {
	cam  *render3d.CameraBuffer
	draw *render3d.PBRDrawBuffer
	pipe *gpu.Pipeline
}

func newClusterTestGPU(t *testing.T, cx *core.Scope, dev *gpu.Device, sys *render3d.ClusteredLightSystem) clusterTestGPU {
	t.Helper()
	camBuf, err := render3d.NewCameraBuffer(cx, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	drawBuf, err := render3d.NewPBRDrawBuffer(cx, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), clusterTestMat), false)
	if err != nil {
		t.Fatalf("NewPBRDrawBuffer: %v", err)
	}
	pipe, err := render3d.NewClusteredLitPBRPipeline(cx, dev, camBuf.Layout(), sys, drawBuf.Layout(), gpu.FormatDepth24Plus)
	if err != nil {
		t.Fatalf("NewClusteredLitPBRPipeline: %v", err)
	}
	if cx == nil {
		t.Cleanup(func() {
			_ = pipe.Close()
			_ = drawBuf.Close()
			_ = camBuf.Close()
		})
	}
	return clusterTestGPU{cam: camBuf, draw: drawBuf, pipe: pipe}
}

func (g clusterTestGPU) record(pass *gpu.RenderPass, sys *render3d.ClusteredLightSystem, mesh *render3d.GPUMesh) {
	pass.SetPipeline(g.pipe)
	pass.SetBindGroup(render3d.CameraGroup, g.cam.BindGroup())
	sys.Bind(pass)
	pass.SetBindGroup(render3d.PBRDrawGroup, g.draw.BindGroup())
	mesh.Draw(pass)
}

func clusterTestRenderSoftware(t *testing.T, sr *gpu.SoftwareRenderer, g clusterTestGPU, sys *render3d.ClusteredLightSystem, mesh *render3d.GPUMesh) *image.RGBA {
	t.Helper()
	dev := sr.Device()
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	g.record(pass, sys, mesh)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	return sr.Image()
}

func clusterTestWorldPointAtPixel(cam *scene3d.Camera, px, py, w, h float32) (math3d.Vec3, bool) {
	// Intersect screen ray through pixel center (px+0.5, py+0.5) with Y=0 floor plane.
	ray := cam.ScreenRay(px+0.5, py+0.5, w, h)
	if math.Abs(float64(ray.Dir.Y)) < 1e-6 {
		return math3d.Vec3{}, false
	}
	t := -ray.Origin.Y / ray.Dir.Y
	if t <= 0 {
		return math3d.Vec3{}, false
	}
	return ray.At(t), true
}

func clusterTestUpdate(t *testing.T, sys *render3d.ClusteredLightSystem, dev *gpu.Device, cam *scene3d.Camera, w, h float32, rig render3d.LightRig, lights []render3d.ClusterLight) render3d.ClusterStats {
	t.Helper()
	stats, err := sys.UpdateCamera(dev, cam, w, h, rig, lights)
	if err != nil {
		t.Fatalf("UpdateCamera: %v", err)
	}
	return stats
}

func TestClusterConfigAndLightValidation(t *testing.T) {
	if err := (render3d.ClusterConfig{}).Validate(); err != nil {
		t.Fatalf("zero ClusterConfig should apply defaults and validate: %v", err)
	}
	def := render3d.DefaultClusterConfig()
	if got := def.ClusterCount(); got != 16*9*24 {
		t.Fatalf("DefaultClusterConfig().ClusterCount() = %d, want %d", got, 16*9*24)
	}

	badConfigs := []struct {
		name string
		cfg  render3d.ClusterConfig
	}{
		{"negative TilesX", render3d.ClusterConfig{TilesX: -1}},
		{"negative TilesY", render3d.ClusterConfig{TilesY: -2}},
		{"SlicesZ too large", render3d.ClusterConfig{SlicesZ: render3d.MaxClusterSlicesZ + 1}},
		{"negative SlicesZ", render3d.ClusterConfig{SlicesZ: -1}},
		{"negative MaxLights", render3d.ClusterConfig{MaxLights: -5}},
		{"MaxLights too large", render3d.ClusterConfig{MaxLights: render3d.MaxClusterLights + 1}},
		{"negative MaxLightsPerCluster", render3d.ClusterConfig{MaxLightsPerCluster: -1}},
		{"MaxLightsPerCluster above MaxLights", render3d.ClusterConfig{MaxLights: 8, MaxLightsPerCluster: 9}},
		{"index slots overflow", render3d.ClusterConfig{TilesX: 64, TilesY: 64, SlicesZ: 64, MaxLights: 4096, MaxLightsPerCluster: 4096}},
		{"FallbackMaxLights too large", render3d.ClusterConfig{FallbackMaxLights: render3d.MaxFallbackLights + 1}},
		{"negative FallbackMaxLights", render3d.ClusterConfig{FallbackMaxLights: -1}},
	}
	for _, tc := range badConfigs {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if !errors.Is(err, render3d.ErrInvalidClusterConfig) {
				t.Fatalf("Validate() = %v, want wrapping ErrInvalidClusterConfig", err)
			}
			if _, err := render3d.NewClusterGrid(tc.cfg); !errors.Is(err, render3d.ErrInvalidClusterConfig) {
				t.Fatalf("NewClusterGrid() = %v, want wrapping ErrInvalidClusterConfig", err)
			}
		})
	}

	pt := render3d.NewPointClusterLight(math3d.Vec3{Y: 2}, math3d.Vec3{X: 1, Y: 1, Z: 1}, 3, 5)
	if err := pt.Validate(); err != nil {
		t.Fatalf("valid point light failed Validate: %v", err)
	}
	sp := render3d.NewSpotClusterLight(math3d.Vec3{Y: 3}, math3d.Vec3{Y: -1}, math3d.Vec3{X: 1}, 2, 6, 0.2, 0.5)
	if err := sp.Validate(); err != nil {
		t.Fatalf("valid spot light failed Validate: %v", err)
	}

	badLights := []struct {
		name string
		l    render3d.ClusterLight
	}{
		{"unknown kind", render3d.ClusterLight{Kind: 99, Range: 1, Color: math3d.Vec3{X: 1}, Intensity: 1}},
		{"zero range", render3d.NewPointClusterLight(math3d.Vec3Zero(), math3d.Vec3{X: 1}, 1, 0)},
		{"NaN pos", render3d.NewPointClusterLight(math3d.Vec3{X: float32(math.NaN())}, math3d.Vec3{X: 1}, 1, 5)},
		{"negative color", render3d.NewPointClusterLight(math3d.Vec3Zero(), math3d.Vec3{X: -0.1}, 1, 5)},
		{"negative intensity", render3d.NewPointClusterLight(math3d.Vec3Zero(), math3d.Vec3{X: 1}, -1, 5)},
		{"inverted spot cones", render3d.ClusterLight{Kind: render3d.ClusterLightSpot, Range: 5, Color: math3d.Vec3{X: 1}, Intensity: 1, Direction: math3d.Vec3{Y: -1}, InnerConeCos: 0.5, OuterConeCos: 0.8}},
		{"out of range cone cos", render3d.ClusterLight{Kind: render3d.ClusterLightSpot, Range: 5, Color: math3d.Vec3{X: 1}, Intensity: 1, Direction: math3d.Vec3{Y: -1}, InnerConeCos: 1.5, OuterConeCos: 0.5}},
	}
	for _, tc := range badLights {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.l.Validate(); !errors.Is(err, render3d.ErrInvalidClusterLight) {
				t.Fatalf("Validate() = %v, want wrapping ErrInvalidClusterLight", err)
			}
		})
	}
}

func TestClusterLightFromPointAndSpotMatchLightRigAttenuation(t *testing.T) {
	ranged := render3d.PointLight{Position: math3d.Vec3{X: 1, Y: 2, Z: -1}, Color: math3d.Vec3{X: 1, Y: 0.5, Z: 0.25}, Intensity: 3, Range: 5}
	cl, err := render3d.ClusterLightFromPoint(ranged)
	if err != nil {
		t.Fatalf("ClusterLightFromPoint: %v", err)
	}
	if cl.Kind != render3d.ClusterLightPoint || cl.Range != 5 {
		t.Fatalf("ClusterLightFromPoint = %+v, want a point light with Range 5", cl)
	}
	spot := render3d.SpotLight{Position: math3d.Vec3{Y: 4}, Direction: math3d.Vec3{X: 0.2, Y: -1}, Color: math3d.Vec3{X: 0.5, Y: 1, Z: 0.5}, Intensity: 8, Range: 9, InnerConeAngle: 0.2, OuterConeAngle: 0.6}
	cs, err := render3d.ClusterLightFromSpot(spot)
	if err != nil {
		t.Fatalf("ClusterLightFromSpot: %v", err)
	}
	for _, p := range []math3d.Vec3{{}, {X: 0.5, Z: 0.3}, {X: 1.5, Y: 0.2, Z: -1}, {X: 3}, {X: -2, Z: 2}} {
		_, got := render3d.EvaluateLightAttenuation(cl, p)
		want := render3d.PointLightAttenuation(ranged.Position.Sub(p).LenSq(), ranged.Range) * ranged.Intensity
		if math.Abs(float64(got-want)) > 1e-5*(1+float64(want)) {
			t.Fatalf("point attenuation at %v = %v, want %v", p, got, want)
		}
		_, got = render3d.EvaluateLightAttenuation(cs, p)
		want = spot.Attenuation(p) * spot.Intensity
		if math.Abs(float64(got-want)) > 1e-5*(1+float64(want)) {
			t.Fatalf("spot attenuation at %v = %v, want %v", p, got, want)
		}
	}

	// Range 0 is unbounded in a LightRig; the converters derive the distance where the radiance falls to ClusterRangeCutoff.
	unbounded := render3d.PointLight{Position: math3d.Vec3{Y: 1}, Color: math3d.Vec3{X: 0.5, Y: 1, Z: 0.25}, Intensity: 4}
	cu, err := render3d.ClusterLightFromPoint(unbounded)
	if err != nil {
		t.Fatalf("ClusterLightFromPoint(unbounded): %v", err)
	}
	if want := float32(math.Sqrt(4 * 1 / render3d.ClusterRangeCutoff)); math.Abs(float64(cu.Range-want)) > 1e-3 {
		t.Fatalf("derived range = %v, want %v", cu.Range, want)
	}
	if err := cu.Validate(); err != nil {
		t.Fatalf("converted unbounded light fails Validate: %v", err)
	}
	unboundedSpot := spot
	unboundedSpot.Range = 0
	if c, err := render3d.ClusterLightFromSpot(unboundedSpot); err != nil || c.Range <= 0 {
		t.Fatalf("ClusterLightFromSpot(unbounded) = %+v, %v; want a positive range", c, err)
	}
	dark, err := render3d.ClusterLightFromPoint(render3d.PointLight{Color: math3d.Vec3{X: 1}})
	if err != nil || dark.Validate() != nil {
		t.Fatalf("zero-intensity light converts to %+v, %v; want a valid record", dark, err)
	}

	if _, err := render3d.ClusterLightFromPoint(render3d.PointLight{Color: math3d.Vec3{X: -1}, Intensity: 1}); !errors.Is(err, render3d.ErrInvalidLight) {
		t.Fatalf("invalid point light error = %v, want ErrInvalidLight", err)
	}
	if _, err := render3d.ClusterLightFromSpot(render3d.SpotLight{Direction: math3d.Vec3{Y: -1}, Color: math3d.Vec3{X: 1}, Intensity: 1, InnerConeAngle: 0.5, OuterConeAngle: 0.4}); !errors.Is(err, render3d.ErrInvalidLight) {
		t.Fatalf("invalid spot light error = %v, want ErrInvalidLight", err)
	}
}

func TestClusterGridLogarithmicSlicesAndBinarySearch(t *testing.T) {
	grid, err := render3d.NewClusterGrid(render3d.DefaultClusterConfig())
	if err != nil {
		t.Fatalf("NewClusterGrid: %v", err)
	}
	near, far := float32(0.5), float32(100.0)
	proj := math3d.Perspective(math3d.DegToRad(60), 16.0/9.0, near, far)
	if !grid.BuildFrustum(proj, near, far) {
		t.Fatal("BuildFrustum returned false for valid perspective matrix")
	}

	depths := grid.SliceDepths()
	if len(depths) != render3d.DefaultClusterSlicesZ+1 {
		t.Fatalf("len(SliceDepths) = %d, want %d", len(depths), render3d.DefaultClusterSlicesZ+1)
	}
	for s := 0; s <= render3d.DefaultClusterSlicesZ; s++ {
		want := float32(float64(near) * math.Pow(float64(far)/float64(near), float64(s)/float64(render3d.DefaultClusterSlicesZ)))
		if math.Abs(float64(depths[s]-want)) > 1e-4*float64(want+1) {
			t.Fatalf("slice depth[%d] = %v, want %v", s, depths[s], want)
		}
	}

	if got := grid.SliceForDepth(near * 0.5); got != 0 {
		t.Fatalf("SliceForDepth(before near) = %d, want 0", got)
	}
	if got := grid.SliceForDepth(far * 2.0); got != render3d.DefaultClusterSlicesZ-1 {
		t.Fatalf("SliceForDepth(past far) = %d, want %d", got, render3d.DefaultClusterSlicesZ-1)
	}
	for s := range render3d.DefaultClusterSlicesZ {
		mid := 0.5 * (depths[s] + depths[s+1])
		if got := grid.SliceForDepth(mid); got != s {
			t.Fatalf("SliceForDepth(mid of slice %d = %v) = %d, want %d", s, mid, got, s)
		}
	}

	// Orthographic projection should also build linear depth slices cleanly.
	ortho := math3d.Ortho(-10, 10, -10, 10, 0.1, 50)
	if !grid.BuildFrustum(ortho, 0.1, 50) {
		t.Fatal("BuildFrustum returned false for orthographic matrix")
	}
	orthoDepths := grid.SliceDepths()
	if math.Abs(float64(orthoDepths[12]-25.05)) > 1e-3 {
		t.Fatalf("ortho midpoint depth = %v, want ~25.05", orthoDepths[12])
	}
}

func TestClusterGridCullingSpotBoundsAndZeroAllocs(t *testing.T) {
	grid, err := render3d.NewClusterGrid(render3d.DefaultClusterConfig())
	if err != nil {
		t.Fatalf("NewClusterGrid: %v", err)
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Y: 2, Z: 10}, math3d.DegToRad(60), 16.0/9.0, 0.5, 50)
	cam.LookAt(math3d.Vec3{Y: 0, Z: 0}, math3d.Vec3Up())
	view := cam.ViewMatrix()
	proj := cam.ProjMatrix()

	// 1. Lights behind camera, beyond far plane, far off to side, or zero intensity must be culled.
	culledSet := []render3d.ClusterLight{
		render3d.NewPointClusterLight(math3d.Vec3{Y: 2, Z: 25}, math3d.Vec3{X: 1}, 2, 3),       // behind camera
		render3d.NewPointClusterLight(math3d.Vec3{Y: 2, Z: -70}, math3d.Vec3{X: 1}, 2, 3),      // beyond far
		render3d.NewPointClusterLight(math3d.Vec3{X: 90, Y: 2, Z: 0}, math3d.Vec3{X: 1}, 2, 3), // outside frustum right
		render3d.NewPointClusterLight(math3d.Vec3{Y: 1, Z: 0}, math3d.Vec3{X: 1}, 0, 3),        // zero intensity
	}
	st := grid.AssignLights(view, proj, 0.5, 50, culledSet)
	if st.VisibleLights != 0 || st.CulledLights != len(culledSet) || st.ActiveClusters != 0 {
		t.Fatalf("expected all lights culled, got stats %+v", st)
	}

	// 2. Spot light narrow cone should occupy strictly fewer clusters than an equal-range point light.
	pointAtCenter := []render3d.ClusterLight{
		render3d.NewPointClusterLight(math3d.Vec3{Y: 2, Z: 0}, math3d.Vec3{X: 1, Y: 1, Z: 1}, 2, 5),
	}
	spotAtCenter := []render3d.ClusterLight{
		render3d.NewSpotClusterLight(math3d.Vec3{Y: 2, Z: 0}, math3d.Vec3{Y: -1}, math3d.Vec3{X: 1, Y: 1, Z: 1}, 2, 5, math3d.DegToRad(10), math3d.DegToRad(18)),
	}
	ptStats := grid.AssignLights(view, proj, 0.5, 50, pointAtCenter)
	spStats := grid.AssignLights(view, proj, 0.5, 50, spotAtCenter)
	if spStats.ActiveClusters == 0 || spStats.ActiveClusters >= ptStats.ActiveClusters {
		t.Fatalf("expected narrow spot (%d clusters) to occupy >0 and fewer clusters than equal-range point (%d clusters)", spStats.ActiveClusters, ptStats.ActiveClusters)
	}

	// 3. Light straddling the camera / near plane must still assign clusters (no false negative).
	nearLight := []render3d.ClusterLight{
		render3d.NewPointClusterLight(math3d.Vec3{Y: 2, Z: 10}, math3d.Vec3{X: 1}, 2, 4), // centered on camera
	}
	nearStats := grid.AssignLights(view, proj, 0.5, 50, nearLight)
	if nearStats.VisibleLights != 1 || nearStats.ActiveClusters == 0 {
		t.Fatalf("camera-straddling light was dropped: %+v", nearStats)
	}

	// 4. Steady-state 256 lights AssignLights must perform zero heap allocations.
	lights256 := clusterTest256PointLights()
	grid.AssignLights(view, proj, 0.5, 50, lights256)
	allocs := testing.AllocsPerRun(50, func() {
		grid.AssignLights(view, proj, 0.5, 50, lights256)
	})
	if allocs != 0 {
		t.Fatalf("steady-state AssignLights(256 lights) allocated %.2f times/op, want 0", allocs)
	}
}

func TestClusterGridTruncationKeepsInputOrder(t *testing.T) {
	cfg := render3d.ClusterConfig{TilesX: 4, TilesY: 4, SlicesZ: 4, MaxLights: 4, MaxLightsPerCluster: 2}
	grid, err := render3d.NewClusterGrid(cfg)
	if err != nil {
		t.Fatalf("NewClusterGrid: %v", err)
	}
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Y: 2, Z: 10}, math3d.DegToRad(60), 1, 0.5, 50)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())

	// Ten overlapping lights in front of the camera: the grid keeps the first four (MaxLights) and every shared cell keeps the first two of those (MaxLightsPerCluster).
	lights := make([]render3d.ClusterLight, 10)
	for i := range lights {
		lights[i] = render3d.NewPointClusterLight(math3d.Vec3{X: float32(i) * 0.01, Y: 1}, math3d.Vec3{X: 1, Y: 1, Z: 1}, 1, 3)
	}
	st := grid.AssignLights(cam.ViewMatrix(), cam.ProjMatrix(), 0.5, 50, lights)
	if st.VisibleLights != 4 || st.TruncatedLights != 6 || st.CulledLights != 0 {
		t.Fatalf("stats = %+v, want 4 visible, 6 truncated, 0 culled", st)
	}
	if st.TruncatedClusters == 0 || st.MaxClusterLights != 2 {
		t.Fatalf("stats = %+v, want truncated clusters holding MaxLightsPerCluster (2) lights", st)
	}
	for i, l := range grid.ActiveLights() {
		if l.Position != lights[i].Position {
			t.Fatalf("active light %d = %v, want input light %d at %v", i, l.Position, i, lights[i].Position)
		}
	}
	for c, oc := range grid.Offsets() {
		if oc.Count > 2 {
			t.Fatalf("cluster %d holds %d lights, want at most 2", c, oc.Count)
		}
		for _, idx := range grid.LightIndices()[oc.Offset : oc.Offset+oc.Count] {
			if idx > 1 {
				t.Fatalf("cluster %d holds light %d, want only the first two lights in input order", c, idx)
			}
		}
	}
}

// TestClustered256PointLightsParity renders a floor lit by a LightRig plus 256 clustered point lights through NewClusteredLitPBRPipeline and checks that the grid lookup misses no light and that the pixels match the CPU reference.
func TestClustered256PointLightsParity(t *testing.T) {
	const (
		viewW = 128
		viewH = 96
		near  = float32(0.5)
		far   = float32(60.0)
	)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 14, Z: 18}, math3d.DegToRad(50), float32(viewW)/float32(viewH), near, far)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0, Z: 0}, math3d.Vec3Up())
	rig := clusterTestRig()
	lights := clusterTest256PointLights()

	sr := gpu.NewSoftwareRenderer(viewW, viewH, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	if !render3d.DeviceSupportsClusteredLighting(dev) {
		t.Fatal("default SoftwareRenderer must support clustered lighting")
	}
	sys, err := render3d.NewClusteredLightSystem(nil, dev, render3d.DefaultClusterConfig())
	if err != nil {
		t.Fatalf("NewClusteredLightSystem: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close() })
	if !sys.IsClustered() || sys.IsFallback() {
		t.Fatalf("expected clustered mode on full device, got clustered=%v fallback=%v", sys.IsClustered(), sys.IsFallback())
	}
	if sys.ByteSize() == 0 {
		t.Fatal("expected positive GPU ByteSize")
	}

	floor, err := render3d.UploadMesh(nil, dev, clusterTestFloorMesh(20), render3d.MeshUpload{Label: "floor"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = floor.Close() })
	g := newClusterTestGPU(t, nil, dev, sys)

	stats := clusterTestUpdate(t, sys, dev, cam, viewW, viewH, rig, lights)
	if stats.InputLights != 256 {
		t.Fatalf("InputLights = %d, want 256", stats.InputLights)
	}
	if stats.VisibleLights < 100 || stats.ActiveClusters == 0 || stats.TruncatedLights != 0 || stats.TruncatedClusters != 0 {
		t.Fatalf("expected >= 100 visible lights in active clusters with no truncation, got %+v", stats)
	}

	g.cam.WriteCamera(cam)
	img := clusterTestRenderSoftware(t, sr, g, sys, floor)

	view := cam.ViewMatrix()
	proj := cam.ProjMatrix()
	up := math3d.Vec3Up()
	litSamples := 0
	for sy := 24; sy <= 80; sy += 14 {
		for sx := 20; sx <= 108; sx += 14 {
			worldPt, ok := clusterTestWorldPointAtPixel(cam, float32(sx), float32(sy), viewW, viewH)
			if !ok || math.Abs(float64(worldPt.X)) > 19 || math.Abs(float64(worldPt.Z)) > 19 {
				continue
			}
			viewDir := cam.Pos.Sub(worldPt)
			direct := render3d.ShadeClusteredPoint(worldPt, up, viewDir, clusterTestMat, rig, lights)
			clustered := render3d.ShadeClusteredGridPoint(worldPt, up, viewDir, clusterTestMat, rig, sys.Grid(), view, proj, viewW, viewH)
			clusterTestAssertVecNear(t, clustered, direct, "grid lookup vs brute force")
			want := clusterTestSRGB(direct)
			clusterTestAssertPixelNear(t, img, sx, sy, want, 3, "clustered-256")
			if base := clusterTestSRGB(render3d.ShadePoint(worldPt, up, viewDir, clusterTestMat, rig)); int(want.R)+int(want.G)+int(want.B) > int(base.R)+int(base.G)+int(base.B)+30 {
				litSamples++
			}
		}
	}
	if litSamples < 10 {
		t.Fatalf("expected at least 10 samples visibly lit by clustered lights, got %d", litSamples)
	}
}

func TestClusteredMixedPointAndSpotLightsGPU(t *testing.T) {
	const (
		viewW = 96
		viewH = 96
		near  = float32(0.5)
		far   = float32(40.0)
	)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 10, Z: 10}, math3d.DegToRad(50), 1, near, far)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	rig := render3d.LightRig{Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 0.02}}

	spot, err := render3d.ClusterLightFromSpot(render3d.SpotLight{
		Position: math3d.Vec3{X: 3, Y: 3}, Direction: math3d.Vec3{Y: -1}, Color: math3d.Vec3{X: 0.1, Y: 1, Z: 0.1},
		Intensity: 24, Range: 7, InnerConeAngle: math3d.DegToRad(15), OuterConeAngle: math3d.DegToRad(25),
	})
	if err != nil {
		t.Fatalf("ClusterLightFromSpot: %v", err)
	}
	lights := []render3d.ClusterLight{
		// Red point light on the left.
		render3d.NewPointClusterLight(math3d.Vec3{X: -3, Y: 1.5, Z: 0}, math3d.Vec3{X: 1, Y: 0.1, Z: 0.1}, 5.0, 4.5),
		// Green downward spot light on the right with a 15 to 25 degree cone.
		spot,
		// Blue spot light at the center aiming up, away from the floor: it must add nothing.
		render3d.NewSpotClusterLight(math3d.Vec3{X: 0, Y: 2, Z: 0}, math3d.Vec3{Y: 1}, math3d.Vec3{X: 0.1, Y: 0.2, Z: 1}, 30.0, 8.0, math3d.DegToRad(20), math3d.DegToRad(35)),
	}

	sr := gpu.NewSoftwareRenderer(viewW, viewH, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	sys, err := render3d.NewClusteredLightSystem(nil, dev, render3d.DefaultClusterConfig())
	if err != nil {
		t.Fatalf("NewClusteredLightSystem: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close() })
	floor, err := render3d.UploadMesh(nil, dev, clusterTestFloorMesh(10), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = floor.Close() })
	g := newClusterTestGPU(t, nil, dev, sys)

	clusterTestUpdate(t, sys, dev, cam, viewW, viewH, rig, lights)
	g.cam.WriteCamera(cam)
	img := clusterTestRenderSoftware(t, sr, g, sys, floor)

	up := math3d.Vec3Up()
	checks := []struct {
		name  string
		world math3d.Vec3
	}{
		{"under-red-point", math3d.Vec3{X: -3, Y: 0, Z: 0}},
		{"under-green-spot-center", math3d.Vec3{X: 3, Y: 0, Z: 0}},
		{"green-spot-penumbra", math3d.Vec3{X: 3 + 1.05, Y: 0, Z: 0}},
		{"outside-green-spot-cone", math3d.Vec3{X: 3 + 2.5, Y: 0, Z: 0}},
		{"under-upward-blue-spot", math3d.Vec3{X: 0, Y: 0, Z: 0}},
	}
	for _, ch := range checks {
		scr, _, vis := cam.WorldToScreen(ch.world, viewW, viewH)
		if !vis {
			t.Fatalf("%s not visible on screen", ch.name)
		}
		px, py := int(scr.X), int(scr.Y)
		exactWorld, ok := clusterTestWorldPointAtPixel(cam, float32(px), float32(py), viewW, viewH)
		if !ok {
			t.Fatalf("%s failed raycast", ch.name)
		}
		want := clusterTestSRGB(render3d.ShadeClusteredPoint(exactWorld, up, cam.Pos.Sub(exactWorld), clusterTestMat, rig, lights))
		clusterTestAssertPixelNear(t, img, px, py, want, 3, ch.name)

		switch ch.name {
		case "under-red-point":
			if want.R < 150 || 2*int(want.G) > int(want.R) {
				t.Fatalf("expected bright red under point light, got %+v", want)
			}
		case "under-green-spot-center":
			if want.G < 150 || 2*int(want.R) > int(want.G) {
				t.Fatalf("expected bright green under spot light, got %+v", want)
			}
		case "under-upward-blue-spot":
			if want.B > 50 {
				t.Fatalf("upward spot light should not light the floor, got %+v", want)
			}
		}
	}
}

func TestClusteredUpdateRejectsInvalidLights(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Y: 4, Z: 6}, math3d.DegToRad(50), 1, 0.5, 40)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	for _, force := range []bool{false, true} {
		sys, err := render3d.NewClusteredLightSystem(nil, dev, render3d.ClusterConfig{ForceFallback: force})
		if err != nil {
			t.Fatalf("NewClusteredLightSystem: %v", err)
		}
		// Range 0 is unbounded in a LightRig, so the clustered system refuses it instead of dropping it silently.
		unbounded := render3d.NewPointClusterLight(math3d.Vec3{Y: 1}, math3d.Vec3{X: 1, Y: 1, Z: 1}, 2, 0)
		if _, err := sys.UpdateCamera(dev, cam, 16, 16, render3d.LightRig{}, []render3d.ClusterLight{unbounded}); !errors.Is(err, render3d.ErrInvalidClusterLight) {
			t.Fatalf("fallback=%v: Update with a Range 0 light = %v, want ErrInvalidClusterLight", force, err)
		}
		badRig := render3d.LightRig{Ambient: render3d.AmbientLight{Intensity: -1}}
		if _, err := sys.UpdateCamera(dev, cam, 16, 16, badRig, nil); !errors.Is(err, render3d.ErrInvalidLight) {
			t.Fatalf("fallback=%v: Update with a malformed rig = %v, want ErrInvalidLight", force, err)
		}
		converted, err := render3d.ClusterLightFromPoint(render3d.PointLight{Position: math3d.Vec3{Y: 1}, Color: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 2})
		if err != nil {
			t.Fatalf("ClusterLightFromPoint: %v", err)
		}
		st, err := sys.UpdateCamera(dev, cam, 16, 16, render3d.LightRig{}, []render3d.ClusterLight{converted})
		if err != nil || st.VisibleLights != 1 {
			t.Fatalf("fallback=%v: Update with a converted unbounded light = %+v, %v; want 1 visible light", force, st, err)
		}
		_ = sys.Close()
	}
}

func TestClusteredFallbackNoGPUComputeAndForceFallback(t *testing.T) {
	const (
		viewW = 64
		viewH = 64
		near  = float32(0.5)
		far   = float32(60.0)
	)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 14, Z: 18}, math3d.DegToRad(50), 1, near, far)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	rig := clusterTestRig()
	// 256 point lights across the floor plus bright foreground lights near the center so every mode visibly lights the sampled pixels.
	lights := clusterTest256PointLights()
	lights[0] = render3d.NewPointClusterLight(math3d.Vec3{X: 0, Y: 1.5, Z: 0}, math3d.Vec3{X: 1, Y: 0.3, Z: 0.2}, 18.0, 6.0)
	lights[1] = render3d.NewPointClusterLight(math3d.Vec3{X: -3, Y: 1.5, Z: 1}, math3d.Vec3{X: 0.2, Y: 1, Z: 0.3}, 18.0, 6.0)
	lights[2] = render3d.NewPointClusterLight(math3d.Vec3{X: 3, Y: 1.5, Z: 1}, math3d.Vec3{X: 0.2, Y: 0.4, Z: 1}, 18.0, 6.0)

	modes := []struct {
		name        string
		cfg         render3d.ClusterConfig
		appOpts     []nsgotest.Option
		wantCluster bool
		wantCaps    bool
	}{
		{name: "full-clustered", cfg: render3d.DefaultClusterConfig(), wantCluster: true, wantCaps: true},
		{name: "webgl2-gles3-no-compute-fallback", cfg: render3d.DefaultClusterConfig(), appOpts: []nsgotest.Option{nsgotest.NoGPUCompute()}},
		{name: "force-fallback-on-full-device", cfg: render3d.ClusterConfig{ForceFallback: true, FallbackMaxLights: 8}, wantCaps: true},
	}

	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			var (
				sys      *render3d.ClusteredLightSystem
				floor    *render3d.GPUMesh
				g        clusterTestGPU
				gotCaps  bool
				gotStats render3d.ClusterStats
			)
			opts := append([]nsgotest.Option{nsgotest.Size(viewW, viewH), nsgotest.ManualFrames()}, m.appOpts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "cluster-view",
					Style:       core.Style{Width: core.Pt(viewW), Height: core.Pt(viewH)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						gotCaps = render3d.DeviceSupportsClusteredLighting(dev)
						var err error
						sys, err = render3d.NewClusteredLightSystem(cx, dev, m.cfg)
						if err != nil {
							t.Fatalf("NewClusteredLightSystem: %v", err)
						}
						floor, err = render3d.UploadMesh(cx, dev, clusterTestFloorMesh(20), render3d.MeshUpload{})
						if err != nil {
							t.Fatalf("UploadMesh: %v", err)
						}
						g = newClusterTestGPU(t, cx, dev, sys)
					},
					OnFrame: func(frame *gpu.Frame) {
						dev := frame.Device()
						g.cam.WriteCamera(cam)
						gotStats = clusterTestUpdate(t, sys, dev, cam, viewW, viewH, rig, lights)
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x050608))
						pass.ClearDepth(1)
						g.record(pass, sys, floor)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)

			if gotCaps != m.wantCaps {
				t.Fatalf("DeviceSupportsClusteredLighting = %v, want %v", gotCaps, m.wantCaps)
			}
			if sys.IsClustered() != m.wantCluster || sys.IsFallback() == m.wantCluster {
				t.Fatalf("mode mismatch: IsClustered=%v IsFallback=%v, wantCluster=%v", sys.IsClustered(), sys.IsFallback(), m.wantCluster)
			}

			img, err := app.GPUReadPixels("id:cluster-view")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}

			refLights := lights
			if sys.IsFallback() {
				wantCap := m.cfg.FallbackMaxLights
				if wantCap == 0 {
					wantCap = render3d.DefaultFallbackMaxLights
				}
				refLights = sys.FallbackLights()
				if len(refLights) != wantCap || gotStats.VisibleLights != wantCap {
					t.Fatalf("fallback selected %d lights, want %d (stats=%+v)", len(refLights), wantCap, gotStats)
				}
				if gotStats.TruncatedLights == 0 || gotStats.CulledLights+gotStats.TruncatedLights+gotStats.VisibleLights != 256 {
					t.Fatalf("fallback stats %+v must split 256 lights into culled, truncated and visible with some truncated", gotStats)
				}
			}

			up := math3d.Vec3Up()
			litCount := 0
			for _, pt := range [][2]int{{22, 32}, {32, 32}, {42, 32}, {32, 42}} {
				worldPt, ok := clusterTestWorldPointAtPixel(cam, float32(pt[0]), float32(pt[1]), viewW, viewH)
				if !ok {
					continue
				}
				want := clusterTestSRGB(render3d.ShadeClusteredPoint(worldPt, up, cam.Pos.Sub(worldPt), clusterTestMat, rig, refLights))
				clusterTestAssertPixelNear(t, img, pt[0], pt[1], want, 3, m.name)
				if want.R > 120 || want.G > 120 || want.B > 120 {
					litCount++
				}
			}
			if litCount == 0 {
				t.Fatalf("%s: expected visibly lit pixels above ambient/clear", m.name)
			}
		})
	}
}

func TestClusteredFallbackUpdateZeroAllocs(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	sys, err := render3d.NewClusteredLightSystem(nil, sr.Device(), render3d.ClusterConfig{ForceFallback: true})
	if err != nil {
		t.Fatalf("NewClusteredLightSystem: %v", err)
	}
	t.Cleanup(func() { _ = sys.Close() })
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Y: 14, Z: 18}, math3d.DegToRad(50), 16.0/9.0, 0.5, 60)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	view, proj := cam.ViewMatrix(), cam.ProjMatrix()
	rig := clusterTestRig()
	lights := clusterTest256PointLights()
	update := func() {
		if _, err := sys.Update(nil, view, proj, 0.5, 60, 1280, 720, rig, lights); err != nil {
			t.Fatalf("Update: %v", err)
		}
	}
	update()
	if allocs := testing.AllocsPerRun(50, update); allocs != 0 {
		t.Fatalf("steady-state fallback Update(256 lights) allocated %.2f times/op, want 0", allocs)
	}
}

func TestClusteredEmptyLightsResizeRetainedAndClose(t *testing.T) {
	if _, err := render3d.NewClusteredLightSystem(nil, nil, render3d.DefaultClusterConfig()); err == nil {
		t.Fatal("expected error for nil device")
	}
	var nilSys *render3d.ClusteredLightSystem
	if nilSys.Valid() || nilSys.IsClustered() || nilSys.IsFallback() || nilSys.ByteSize() != 0 {
		t.Fatal("nil ClusteredLightSystem returned unexpected non-zero state")
	}
	if err := nilSys.Close(); err != nil {
		t.Fatalf("nil Close: %v", err)
	}

	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	for _, prof := range profiles {
		t.Run(prof.name, func(t *testing.T) {
			const size = 48
			cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 6, Z: 6}, math3d.DegToRad(50), 1, 0.5, 40)
			cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
			rig := render3d.LightRig{Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.2, Y: 0.3, Z: 0.4}, Intensity: 1}}
			activeLights := []render3d.ClusterLight{}

			var (
				sys   *render3d.ClusteredLightSystem
				floor *render3d.GPUMesh
				g     clusterTestGPU
			)
			opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, prof.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "ret-view",
					Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						var err error
						if sys == nil {
							cfg := render3d.DefaultClusterConfig()
							cfg.Retained = true
							sys, err = render3d.NewClusteredLightSystem(cx, dev, cfg)
							if err != nil {
								t.Fatalf("NewClusteredLightSystem: %v", err)
							}
							floor, err = render3d.UploadMesh(cx, dev, clusterTestFloorMesh(10), render3d.MeshUpload{Retained: true})
							if err != nil {
								t.Fatalf("UploadMesh: %v", err)
							}
						}
						g = newClusterTestGPU(t, cx, dev, sys)
					},
					OnFrame: func(frame *gpu.Frame) {
						dev := frame.Device()
						w, h := dev.SurfaceSize()
						cam.SetViewport(float32(w), float32(h))
						g.cam.WriteCamera(cam)
						clusterTestUpdate(t, sys, dev, cam, float32(w), float32(h), rig, activeLights)
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x000000))
						pass.ClearDepth(1)
						g.record(pass, sys, floor)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			// Frame 1: no local lights, so only the rig's ambient term lights the floor.
			app.Advance(16 * time.Millisecond)
			img, err := app.GPUReadPixels("id:ret-view")
			if err != nil {
				t.Fatalf("GPUReadPixels empty: %v", err)
			}
			ambientOnly := render3d.ShadeClusteredPoint(math3d.Vec3Zero(), math3d.Vec3Up(), cam.Pos, clusterTestMat, rig, nil)
			clusterTestAssertPixelNear(t, img, size/2, size/2, clusterTestSRGB(ambientOnly), 3, "empty-ambient")

			// Frame 2: add a bright yellow light above the origin.
			activeLights = []render3d.ClusterLight{
				render3d.NewPointClusterLight(math3d.Vec3{Y: 1.5}, math3d.Vec3{X: 1, Y: 0.9, Z: 0.1}, 12.0, 6.0),
			}
			app.Advance(16 * time.Millisecond)
			beforeLoss, err := app.GPUReadPixels("id:ret-view")
			if err != nil {
				t.Fatalf("GPUReadPixels before loss: %v", err)
			}
			centerBefore := beforeLoss.RGBAAt(size/2, size/2)
			if centerBefore.R < 150 || centerBefore.G < 150 {
				t.Fatalf("expected bright yellow center before loss, got %+v", centerBefore)
			}

			// Lose and restore the device: the retained ClusteredLightSystem must recover.
			firstSys := sys
			app.TriggerGPUDeviceLost("id:ret-view", nsgotest.GPULostContextLost, "context lost")
			if firstSys.Valid() {
				t.Fatal("ClusteredLightSystem still Valid while device is lost")
			}
			app.RestoreGPUDevice("id:ret-view")
			if sys != firstSys || !firstSys.Valid() {
				t.Fatalf("retained ClusteredLightSystem not restored: same=%v valid=%v", sys == firstSys, firstSys.Valid())
			}

			app.Advance(16 * time.Millisecond)
			afterRestore, err := app.GPUReadPixels("id:ret-view")
			if err != nil {
				t.Fatalf("GPUReadPixels after restore: %v", err)
			}
			clusterTestAssertPixelNear(t, afterRestore, size/2, size/2, centerBefore, 2, "after-restore")

			if err := sys.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if sys.Valid() {
				t.Fatal("ClusteredLightSystem still Valid after Close")
			}
			if err := sys.Close(); err != nil {
				t.Fatalf("second Close: %v", err)
			}
		})
	}
}

func TestSelectFallbackLightsPrioritization(t *testing.T) {
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 10}, math3d.DegToRad(60), 1, 0.5, 100)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	lights := []render3d.ClusterLight{
		render3d.NewPointClusterLight(math3d.Vec3{Z: 20}, math3d.Vec3{X: 1}, 2, 3),         // behind the camera
		render3d.NewPointClusterLight(math3d.Vec3{Z: 5}, math3d.Vec3{Y: 1}, 5, 6),          // closest and bright in front
		render3d.NewPointClusterLight(math3d.Vec3{Z: -30}, math3d.Vec3{Z: 1}, 1, 3),        // far in front
		render3d.NewPointClusterLight(math3d.Vec3{Z: 4}, math3d.Vec3{X: 1}, 0, 5),          // no power
		render3d.NewPointClusterLight(math3d.Vec3{Z: 6}, math3d.Vec3{X: 1}, 3, 6),          // second best in front
		render3d.NewPointClusterLight(math3d.Vec3{X: 60, Z: 8}, math3d.Vec3{X: 1}, 500, 6), // brightest of all, but off screen to the side
	}
	picked := render3d.SelectFallbackLights(nil, cam.ViewMatrix(), cam.ProjMatrix(), lights, 2)
	if len(picked) != 2 {
		t.Fatalf("picked %d lights, want 2", len(picked))
	}
	if picked[0].Position.Z != 5 || picked[1].Position.Z != 6 {
		t.Fatalf("picked %+v, want the visible lights at Z=5 then Z=6", picked)
	}
	all := render3d.SelectFallbackLights(nil, cam.ViewMatrix(), cam.ProjMatrix(), lights, 16)
	if len(all) != 3 {
		t.Fatalf("picked %d lights with room for 16, want the 3 visible powered lights: %+v", len(all), all)
	}
}

// BenchmarkClusterAssign256Lights measures the CPU side of clustering 256 lights per frame, which a 60 FPS desktop frame must fit in well under 16 ms.
func BenchmarkClusterAssign256Lights(b *testing.B) {
	grid, err := render3d.NewClusterGrid(render3d.DefaultClusterConfig())
	if err != nil {
		b.Fatalf("NewClusterGrid: %v", err)
	}
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 14, Z: 18}, math3d.DegToRad(50), 16.0/9.0, 0.5, 60)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	view := cam.ViewMatrix()
	proj := cam.ProjMatrix()
	lights := clusterTest256PointLights()

	grid.AssignLights(view, proj, 0.5, 60, lights)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		grid.AssignLights(view, proj, 0.5, 60, lights)
	}
}

func BenchmarkClusterFallbackSelect256Lights(b *testing.B) {
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 14, Z: 18}, math3d.DegToRad(50), 16.0/9.0, 0.5, 60)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	view, proj := cam.ViewMatrix(), cam.ProjMatrix()
	lights := clusterTest256PointLights()
	dst := make([]render3d.ClusterLight, 0, render3d.DefaultFallbackMaxLights)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		dst = render3d.SelectFallbackLights(dst, view, proj, lights, render3d.DefaultFallbackMaxLights)
	}
}
