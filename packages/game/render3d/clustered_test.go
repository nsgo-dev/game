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
			col := palette[idx%len(palette)]
			lights = append(lights, render3d.NewPointClusterLight(
				math3d.Vec3{X: x, Y: 1.25, Z: z},
				col,
				1.8,
				2.8,
			))
		}
	}
	return lights
}

func clusterTestClampByte(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(math.Round(float64(v) * 255.0))
}

func clusterTestVec3ToRGBA(v math3d.Vec3) color.RGBA {
	return color.RGBA{
		R: clusterTestClampByte(v.X),
		G: clusterTestClampByte(v.Y),
		B: clusterTestClampByte(v.Z),
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
		t.Errorf("%s at (%d,%d): got RGBA(%d,%d,%d,%d), want RGBA(%d,%d,%d,%d) ±%d",
			label, x, y, got.R, got.G, got.B, got.A, want.R, want.G, want.B, want.A, tol)
	}
}

func clusterTestBuildPipeline(t *testing.T, cx *core.Scope, dev *gpu.Device, camLayout *gpu.BindGroupLayout, sys *render3d.ClusteredLightSystem, depth gpu.TextureFormat) *gpu.Pipeline {
	t.Helper()
	mod, err := gpu.CompileWGSL(sys.WGSL())
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	layouts := make([]*gpu.BindGroupLayout, render3d.ClusteredLightGroup+1)
	layouts[render3d.CameraGroup] = camLayout
	layouts[render3d.ClusteredLightGroup] = sys.Layout()

	desc := gpu.PipelineDesc{
		Label:         "clustered-lit",
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       layouts,
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
		Targets:       []gpu.ColorTargetState{{Format: dev.SurfaceFormat(), WriteMask: gpu.ColorWriteAll}},
	}
	if depth != gpu.FormatUndefined {
		desc.DepthStencil = &gpu.DepthStencilState{
			Format:       depth,
			DepthWrite:   true,
			DepthCompare: gpu.CompareLess,
		}
	}
	pipe, err := dev.CreatePipeline(cx, desc)
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	return pipe
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
		{"negative MaxLightsPerCluster", render3d.ClusterConfig{MaxLightsPerCluster: -1}},
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

	// Verify SliceForDepth matches exact logarithmic intervals.
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
		t.Fatalf("expected narrow spot (%d clusters) to occupy >0 and fewer clusters than equal-range point (%d clusters)",
			spStats.ActiveClusters, ptStats.ActiveClusters)
	}

	// 3. Light straddling the camera / near plane must still assign clusters (no false negative).
	nearLight := []render3d.ClusterLight{
		render3d.NewPointClusterLight(math3d.Vec3{Y: 2, Z: 10}, math3d.Vec3{X: 1}, 2, 4), // centered on camera!
	}
	nearStats := grid.AssignLights(view, proj, 0.5, 50, nearLight)
	if nearStats.VisibleLights != 1 || nearStats.ActiveClusters == 0 {
		t.Fatalf("camera-straddling light was dropped: %+v", nearStats)
	}

	// 4. Steady-state 256 lights AssignLights must perform zero heap allocations.
	lights256 := clusterTest256PointLights()
	// Warm up once (builds frustum cache & sizes lightIndices).
	grid.AssignLights(view, proj, 0.5, 50, lights256)
	allocs := testing.AllocsPerRun(50, func() {
		grid.AssignLights(view, proj, 0.5, 50, lights256)
	})
	if allocs != 0 {
		t.Fatalf("steady-state AssignLights(256 lights) allocated %.2f times/op, want 0", allocs)
	}
}

func TestClustered256PointLightsParityAndBudget(t *testing.T) {
	const (
		viewW = 128
		viewH = 96
		near  = float32(0.5)
		far   = float32(60.0)
	)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 14, Z: 18}, math3d.DegToRad(50), float32(viewW)/float32(viewH), near, far)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0, Z: 0}, math3d.Vec3Up())
	ambient := math3d.Vec3{X: 0.04, Y: 0.04, Z: 0.05}
	lights := clusterTest256PointLights()

	sr := gpu.NewSoftwareRenderer(viewW, viewH, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	if !render3d.DeviceSupportsClusteredLighting(dev) {
		t.Fatal("default SoftwareRenderer must support clustered lighting")
	}

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

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

	pipe := clusterTestBuildPipeline(t, nil, dev, camBuf.Layout(), sys, gpu.FormatDepth24Plus)
	t.Cleanup(func() { _ = pipe.Close() })

	// Warm up frustum cache once, then time 256-light assignment against the 16.6ms 60fps desktop budget.
	_ = sys.UpdateCamera(dev, cam, viewW, viewH, ambient, lights)
	start := time.Now()
	stats := sys.UpdateCamera(dev, cam, viewW, viewH, ambient, lights)
	assignElapsed := time.Since(start)

	if stats.InputLights != 256 {
		t.Fatalf("InputLights = %d, want 256", stats.InputLights)
	}
	if stats.VisibleLights < 100 {
		t.Fatalf("expected >= 100 visible point lights across the 16x16 grid, got %d (stats=%+v)", stats.VisibleLights, stats)
	}
	if stats.ActiveClusters == 0 {
		t.Fatalf("expected active clusters > 0, got %+v", stats)
	}
	if assignElapsed > 16*time.Millisecond {
		t.Fatalf("256-light UpdateCamera took %v, exceeding 16ms frame budget", assignElapsed)
	}

	// Render the floor lit by all 256 clustered point lights.
	camBuf.WriteCamera(cam)
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label: "clustered-256",
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: core.RGB(0x050608),
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1,
		},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	sys.Bind(pass, render3d.ClusteredLightGroup)
	floor.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	view := cam.ViewMatrix()
	proj := cam.ProjMatrix()
	white := math3d.Vec3{X: 1, Y: 1, Z: 1}
	up := math3d.Vec3Up()

	// Sample a 7x5 grid across the visible floor and assert BOTH:
	// 1. Pure-Go clustered evaluation matches brute-force EvaluateDirectLights (zero false culls across 256 lights).
	// 2. GPU rasterized pixel matches pure-Go analytic reference within ±3/255.
	litSamples := 0
	for sy := 24; sy <= 80; sy += 14 {
		for sx := 20; sx <= 108; sx += 14 {
			worldPt, ok := clusterTestWorldPointAtPixel(cam, float32(sx), float32(sy), viewW, viewH)
			if !ok || math.Abs(float64(worldPt.X)) > 19 || math.Abs(float64(worldPt.Z)) > 19 {
				continue
			}
			direct := render3d.EvaluateDirectLights(worldPt, up, white, ambient, lights)
			clustered := render3d.EvaluateClusteredLights(worldPt, up, white, ambient, sys.Grid(), view, proj, viewW, viewH)
			if math.Abs(float64(direct.X-clustered.X)) > 1e-4 ||
				math.Abs(float64(direct.Y-clustered.Y)) > 1e-4 ||
				math.Abs(float64(direct.Z-clustered.Z)) > 1e-4 {
				t.Fatalf("CPU clustered vs direct mismatch at pixel (%d,%d) world %v: clustered=%v direct=%v",
					sx, sy, worldPt, clustered, direct)
			}
			wantRGBA := clusterTestVec3ToRGBA(direct)
			clusterTestAssertPixelNear(t, img, sx, sy, wantRGBA, 3, "clustered-256")
			if wantRGBA.R > 25 || wantRGBA.G > 25 || wantRGBA.B > 25 {
				litSamples++
			}
		}
	}
	if litSamples < 10 {
		t.Fatalf("expected at least 10 visibly lit floor sample points, got %d", litSamples)
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
	ambient := math3d.Vec3{X: 0.02, Y: 0.02, Z: 0.02}

	lights := []render3d.ClusterLight{
		// Red point light on the left (-3, 1.5, 0).
		render3d.NewPointClusterLight(math3d.Vec3{X: -3, Y: 1.5, Z: 0}, math3d.Vec3{X: 1, Y: 0.1, Z: 0.1}, 3.0, 4.5),
		// Green downward spotlight on the right (3, 3, 0) with a tight cone (15 deg inner, 25 deg outer).
		render3d.NewSpotClusterLight(
			math3d.Vec3{X: 3, Y: 3, Z: 0},
			math3d.Vec3{Y: -1},
			math3d.Vec3{X: 0.1, Y: 1, Z: 0.1},
			8.0, 7.0,
			math3d.DegToRad(15), math3d.DegToRad(25),
		),
		// Blue spotlight at center aiming UP away from the floor -> must contribute zero to the floor!
		render3d.NewSpotClusterLight(
			math3d.Vec3{X: 0, Y: 2, Z: 0},
			math3d.Vec3{Y: 1},
			math3d.Vec3{X: 0.1, Y: 0.2, Z: 1},
			10.0, 8.0,
			math3d.DegToRad(20), math3d.DegToRad(35),
		),
	}

	sr := gpu.NewSoftwareRenderer(viewW, viewH, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

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

	pipe := clusterTestBuildPipeline(t, nil, dev, camBuf.Layout(), sys, gpu.FormatDepth24Plus)
	t.Cleanup(func() { _ = pipe.Close() })

	sys.UpdateCamera(dev, cam, viewW, viewH, ambient, lights)
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	sys.Bind(pass, render3d.ClusteredLightGroup)
	floor.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	white := math3d.Vec3{X: 1, Y: 1, Z: 1}
	up := math3d.Vec3Up()

	// Project key world points to pixel coordinates and verify exact shading.
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
		want := clusterTestVec3ToRGBA(render3d.EvaluateDirectLights(exactWorld, up, white, ambient, lights))
		clusterTestAssertPixelNear(t, img, px, py, want, 3, ch.name)

		// Sanity checks on expected visual contrast.
		switch ch.name {
		case "under-red-point":
			if want.R < 150 || want.G > 40 {
				t.Fatalf("expected bright red under point light, got %+v", want)
			}
		case "under-green-spot-center":
			if want.G < 150 || want.R > 40 {
				t.Fatalf("expected bright green under spot light, got %+v", want)
			}
		case "under-upward-blue-spot":
			if want.B > 30 {
				t.Fatalf("upward spotlight should not light floor, got %+v", want)
			}
		}
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
	ambient := math3d.Vec3{X: 0.03, Y: 0.03, Z: 0.04}
	// Start with 256 point lights across the floor plus bright foreground lights near the center so both clustered and fallback modes visibly illuminate the sampled pixels.
	lights := clusterTest256PointLights()
	lights[0] = render3d.NewPointClusterLight(math3d.Vec3{X: 0, Y: 1.5, Z: 0}, math3d.Vec3{X: 1, Y: 0.3, Z: 0.2}, 6.0, 6.0)
	lights[1] = render3d.NewPointClusterLight(math3d.Vec3{X: -3, Y: 1.5, Z: 1}, math3d.Vec3{X: 0.2, Y: 1, Z: 0.3}, 6.0, 6.0)
	lights[2] = render3d.NewPointClusterLight(math3d.Vec3{X: 3, Y: 1.5, Z: 1}, math3d.Vec3{X: 0.2, Y: 0.4, Z: 1}, 6.0, 6.0)

	modes := []struct {
		name        string
		cfg         render3d.ClusterConfig
		appOpts     []nsgotest.Option
		wantCluster bool
		wantCaps    bool
	}{
		{
			name:        "full-clustered",
			cfg:         render3d.DefaultClusterConfig(),
			appOpts:     nil,
			wantCluster: true,
			wantCaps:    true,
		},
		{
			name:        "webgl2-gles3-no-compute-fallback",
			cfg:         render3d.DefaultClusterConfig(),
			appOpts:     []nsgotest.Option{nsgotest.NoGPUCompute()},
			wantCluster: false,
			wantCaps:    false,
		},
		{
			name: "force-fallback-on-full-device",
			cfg: render3d.ClusterConfig{
				ForceFallback:     true,
				FallbackMaxLights: 8,
			},
			appOpts:     nil,
			wantCluster: false,
			wantCaps:    true,
		},
	}

	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			var (
				camBuf   *render3d.CameraBuffer
				sys      *render3d.ClusteredLightSystem
				floor    *render3d.GPUMesh
				pipe     *gpu.Pipeline
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
						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
						sys, err = render3d.NewClusteredLightSystem(cx, dev, m.cfg)
						if err != nil {
							t.Fatalf("NewClusteredLightSystem: %v", err)
						}
						floor, err = render3d.UploadMesh(cx, dev, clusterTestFloorMesh(20), render3d.MeshUpload{})
						if err != nil {
							t.Fatalf("UploadMesh: %v", err)
						}
						pipe = clusterTestBuildPipeline(t, cx, dev, camBuf.Layout(), sys, gpu.FormatDepth24Plus)
					},
					OnFrame: func(frame *gpu.Frame) {
						dev := frame.Device()
						camBuf.WriteCamera(cam)
						gotStats = sys.UpdateCamera(dev, cam, viewW, viewH, ambient, lights)
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x050608))
						pass.ClearDepth(1)
						pass.SetPipeline(pipe)
						pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
						sys.Bind(pass, render3d.ClusteredLightGroup)
						floor.Draw(pass)
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

			white := math3d.Vec3{X: 1, Y: 1, Z: 1}
			up := math3d.Vec3Up()
			refLights := lights
			if sys.IsFallback() {
				wantCap := m.cfg.FallbackMaxLights
				if wantCap == 0 {
					wantCap = render3d.DefaultFallbackMaxLights
				}
				refLights = sys.FallbackLights()
				if len(refLights) != wantCap {
					t.Fatalf("fallback selected %d lights, want %d (stats=%+v)", len(refLights), wantCap, gotStats)
				}
				if gotStats.TruncatedLights != 256-wantCap {
					t.Fatalf("TruncatedLights = %d, want %d", gotStats.TruncatedLights, 256-wantCap)
				}
			}

			// Verify pixel parity against analytic reference for active light set and confirm non-trivial lighting.
			litCount := 0
			for _, pt := range [][2]int{{22, 32}, {32, 32}, {42, 32}, {32, 42}} {
				worldPt, ok := clusterTestWorldPointAtPixel(cam, float32(pt[0]), float32(pt[1]), viewW, viewH)
				if !ok {
					continue
				}
				want := clusterTestVec3ToRGBA(render3d.EvaluateDirectLights(worldPt, up, white, ambient, refLights))
				clusterTestAssertPixelNear(t, img, pt[0], pt[1], want, 3, m.name)
				if want.R > 40 || want.G > 40 || want.B > 40 {
					litCount++
				}
			}
			if litCount == 0 {
				t.Fatalf("%s: expected visibly lit pixels above ambient/clear", m.name)
			}
		})
	}
}

func TestClusteredEmptyLightsResizeRetainedAndClose(t *testing.T) {
	// 1. Nil device error & nil receiver safety.
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

	// 2. Empty lights + viewport resize + Retained device-loss recovery on both full and NoGPUCompute profiles.
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
			ambient := math3d.Vec3{X: 0.2, Y: 0.3, Z: 0.4}
			activeLights := []render3d.ClusterLight{} // start empty, then populate

			var (
				camBuf *render3d.CameraBuffer
				sys    *render3d.ClusteredLightSystem
				floor  *render3d.GPUMesh
				pipe   *gpu.Pipeline
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
						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
						pipe = clusterTestBuildPipeline(t, cx, dev, camBuf.Layout(), sys, gpu.FormatDepth24Plus)
					},
					OnFrame: func(frame *gpu.Frame) {
						dev := frame.Device()
						w, h := dev.SurfaceSize()
						cam.SetViewport(float32(w), float32(h))
						camBuf.WriteCamera(cam)
						sys.UpdateCamera(dev, cam, float32(w), float32(h), ambient, activeLights)
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x000000))
						pass.ClearDepth(1)
						pass.SetPipeline(pipe)
						pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
						sys.Bind(pass, render3d.ClusteredLightGroup)
						floor.Draw(pass)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			// Frame 1: 0 lights -> pure ambient color.
			app.Advance(16 * time.Millisecond)
			img, err := app.GPUReadPixels("id:ret-view")
			if err != nil {
				t.Fatalf("GPUReadPixels empty: %v", err)
			}
			clusterTestAssertPixelNear(t, img, size/2, size/2, clusterTestVec3ToRGBA(ambient), 3, "empty-ambient")

			// Frame 2: add bright yellow light at origin.
			activeLights = []render3d.ClusterLight{
				render3d.NewPointClusterLight(math3d.Vec3{Y: 1.5}, math3d.Vec3{X: 1, Y: 0.9, Z: 0.1}, 4.0, 6.0),
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

			// Trigger device loss and restore: retained ClusteredLightSystem must recover cleanly.
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

			// Close is idempotent.
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
	camPos := math3d.Vec3{Z: 10}
	camFwd := math3d.Vec3Forward() // (0, 0, -1)
	lights := []render3d.ClusterLight{
		render3d.NewPointClusterLight(math3d.Vec3{Z: 20}, math3d.Vec3{X: 1}, 2, 3),  // behind camera
		render3d.NewPointClusterLight(math3d.Vec3{Z: 5}, math3d.Vec3{Y: 1}, 5, 6),   // closest & bright in front
		render3d.NewPointClusterLight(math3d.Vec3{Z: -30}, math3d.Vec3{Z: 1}, 1, 3), // far in front
		render3d.NewPointClusterLight(math3d.Vec3{Z: 4}, math3d.Vec3{X: 1}, 0, 5),   // inactive (zero intensity)
		render3d.NewPointClusterLight(math3d.Vec3{Z: 6}, math3d.Vec3{X: 1}, 3, 6),   // second best in front
	}
	picked := render3d.SelectFallbackLights(nil, camPos, camFwd, lights, 2)
	if len(picked) != 2 {
		t.Fatalf("picked %d lights, want 2", len(picked))
	}
	// Light index 1 (Z=5, intensity=5, green) and index 4 (Z=6, intensity=3, red) must be the top two.
	if picked[0].Position.Z != 5 && picked[1].Position.Z != 5 {
		t.Fatalf("expected closest bright front light (Z=5) in top 2, got %+v", picked)
	}
	if picked[0].Position.Z != 6 && picked[1].Position.Z != 6 {
		t.Fatalf("expected second front light (Z=6) in top 2, got %+v", picked)
	}
}

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

	// Warm up frustum cache and scratch capacity.
	grid.AssignLights(view, proj, 0.5, 60, lights)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		grid.AssignLights(view, proj, 0.5, 60, lights)
	}
}

func BenchmarkClusterFallbackSelect256Lights(b *testing.B) {
	camPos := math3d.Vec3{X: 0, Y: 14, Z: 18}
	camFwd := math3d.Vec3{X: 0, Y: -14, Z: -18}.Norm()
	lights := clusterTest256PointLights()
	dst := make([]render3d.ClusterLight, 0, render3d.DefaultFallbackMaxLights)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		dst = render3d.SelectFallbackLights(dst, camPos, camFwd, lights, render3d.DefaultFallbackMaxLights)
	}
}
