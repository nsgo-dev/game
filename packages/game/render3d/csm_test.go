package render3d_test

import (
	"encoding/binary"
	"errors"
	"image"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/gametest"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	csmTestWidth  = 160
	csmTestHeight = 120
)

func csmTestBoxMesh(minPt, maxPt math3d.Vec3) render3d.Mesh {
	type face struct {
		n          math3d.Vec3
		a, b, c, d math3d.Vec3
	}
	lo, hi := minPt, maxPt
	faces := []face{
		// +Y top
		{math3d.Vec3{Y: 1},
			math3d.Vec3{X: lo.X, Y: hi.Y, Z: hi.Z},
			math3d.Vec3{X: hi.X, Y: hi.Y, Z: hi.Z},
			math3d.Vec3{X: hi.X, Y: hi.Y, Z: lo.Z},
			math3d.Vec3{X: lo.X, Y: hi.Y, Z: lo.Z}},
		// -Y bottom
		{math3d.Vec3{Y: -1},
			math3d.Vec3{X: lo.X, Y: lo.Y, Z: lo.Z},
			math3d.Vec3{X: hi.X, Y: lo.Y, Z: lo.Z},
			math3d.Vec3{X: hi.X, Y: lo.Y, Z: hi.Z},
			math3d.Vec3{X: lo.X, Y: lo.Y, Z: hi.Z}},
		// +X right
		{math3d.Vec3{X: 1},
			math3d.Vec3{X: hi.X, Y: lo.Y, Z: hi.Z},
			math3d.Vec3{X: hi.X, Y: lo.Y, Z: lo.Z},
			math3d.Vec3{X: hi.X, Y: hi.Y, Z: lo.Z},
			math3d.Vec3{X: hi.X, Y: hi.Y, Z: hi.Z}},
		// -X left
		{math3d.Vec3{X: -1},
			math3d.Vec3{X: lo.X, Y: lo.Y, Z: lo.Z},
			math3d.Vec3{X: lo.X, Y: lo.Y, Z: hi.Z},
			math3d.Vec3{X: lo.X, Y: hi.Y, Z: hi.Z},
			math3d.Vec3{X: lo.X, Y: hi.Y, Z: lo.Z}},
		// +Z front
		{math3d.Vec3{Z: 1},
			math3d.Vec3{X: lo.X, Y: lo.Y, Z: hi.Z},
			math3d.Vec3{X: hi.X, Y: lo.Y, Z: hi.Z},
			math3d.Vec3{X: hi.X, Y: hi.Y, Z: hi.Z},
			math3d.Vec3{X: lo.X, Y: hi.Y, Z: hi.Z}},
		// -Z back
		{math3d.Vec3{Z: -1},
			math3d.Vec3{X: hi.X, Y: lo.Y, Z: lo.Z},
			math3d.Vec3{X: lo.X, Y: lo.Y, Z: lo.Z},
			math3d.Vec3{X: lo.X, Y: hi.Y, Z: lo.Z},
			math3d.Vec3{X: hi.X, Y: hi.Y, Z: lo.Z}},
	}
	var m render3d.Mesh
	for _, f := range faces {
		base := uint32(len(m.Positions))
		m.Positions = append(m.Positions, f.a, f.b, f.c, f.d)
		m.Normals = append(m.Normals, f.n, f.n, f.n, f.n)
		m.UVs = append(m.UVs,
			math3d.Vec2{X: 0, Y: 0},
			math3d.Vec2{X: 1, Y: 0},
			math3d.Vec2{X: 1, Y: 1},
			math3d.Vec2{X: 0, Y: 1},
		)
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	return m
}

func csmTestGroundMesh(minX, maxX, minZ, maxZ float32) render3d.Mesh {
	up := math3d.Vec3Up()
	return render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: minX, Y: 0, Z: maxZ},
			{X: maxX, Y: 0, Z: maxZ},
			{X: maxX, Y: 0, Z: minZ},
			{X: minX, Y: 0, Z: minZ},
		},
		Normals: []math3d.Vec3{up, up, up, up},
		UVs: []math3d.Vec2{
			{X: 0, Y: 0},
			{X: 1, Y: 0},
			{X: 1, Y: 1},
			{X: 0, Y: 1},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

func csmTestMergeMeshes(meshes ...render3d.Mesh) render3d.Mesh {
	var out render3d.Mesh
	for _, m := range meshes {
		base := uint32(len(out.Positions))
		out.Positions = append(out.Positions, m.Positions...)
		out.Normals = append(out.Normals, m.Normals...)
		out.UVs = append(out.UVs, m.UVs...)
		for _, idx := range m.Indices {
			out.Indices = append(out.Indices, base+idx)
		}
	}
	return out
}

type csmTestPillar struct {
	centerX float32
	centerZ float32
	halfW   float32
	height  float32
}

type csmTestSceneSpec struct {
	cam       *scene3d.Camera
	lightDir  math3d.Vec3
	ground    render3d.Mesh
	pillars   []csmTestPillar
	rig       render3d.LightRig
	groundMat render3d.PBRMaterial
	pillarMat render3d.PBRMaterial
}

func csmTestLargeScene() csmTestSceneSpec {
	cam := scene3d.NewPerspectiveCamera(
		math3d.Vec3{X: 0, Y: 3.2, Z: 3.5},
		math3d.DegToRad(58),
		float32(csmTestWidth)/float32(csmTestHeight),
		1.0,
		60.0,
	)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0.3, Z: -15}, math3d.Vec3Up())

	// Oblique sun shining downward and toward (+X, -Y, -Z) so shadows stretch cleanly to the right and slightly away from the camera on the ground plane.
	lightDir := math3d.Vec3{X: 0.55, Y: -0.85, Z: -0.20}.Norm()

	pillars := []csmTestPillar{
		// Near pillar strictly inside cascade 0 (viewDepth ~ 5.1m)
		{centerX: -1.4, centerZ: -1.2, halfW: 0.32, height: 2.0},
		// Cascade 1 pillar (viewDepth ~ 9.5m)
		{centerX: 1.5, centerZ: -5.5, halfW: 0.45, height: 2.8},
		// Cascade 2 pillars (viewDepth ~ 17..21m)
		{centerX: -4.0, centerZ: -13.5, halfW: 0.75, height: 4.4},
		{centerX: 3.2, centerZ: -17.5, halfW: 0.85, height: 5.0},
		// Cascade 3 far pillars (viewDepth ~ 36..41m)
		{centerX: -6.8, centerZ: -32.0, halfW: 1.3, height: 7.5},
		{centerX: 5.4, centerZ: -37.0, halfW: 1.4, height: 8.0},
	}

	return csmTestSceneSpec{
		cam:       cam,
		lightDir:  lightDir,
		ground:    csmTestGroundMesh(-20, 20, -56, 2.0),
		pillars:   pillars,
		rig:       csmTestRig(lightDir),
		groundMat: render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.82, Y: 0.82, Z: 0.78}, Roughness: 0.9},
		pillarMat: render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.76, Y: 0.42, Z: 0.28}, Roughness: 0.7},
	}
}

// csmTestRig returns a LightRig whose first directional light, the one the cascades shadow, shines along lightDir, plus an unshadowed fill light and an ambient term.
func csmTestRig(lightDir math3d.Vec3) render3d.LightRig {
	return render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.18, Y: 0.20, Z: 0.24}, Intensity: 1},
		Directional: []render3d.DirectionalLight{
			{Direction: lightDir, Color: math3d.Vec3{X: 1.0, Y: 0.96, Z: 0.88}, Intensity: 2.2},
			{Direction: math3d.Vec3{X: -0.4, Y: -1, Z: 0.6}, Color: math3d.Vec3{X: 0.6, Y: 0.65, Z: 0.75}, Intensity: 0.4},
		},
	}
}

func (s csmTestSceneSpec) pillarMesh() render3d.Mesh {
	boxes := make([]render3d.Mesh, len(s.pillars))
	for i, p := range s.pillars {
		boxes[i] = csmTestBoxMesh(
			math3d.Vec3{X: p.centerX - p.halfW, Y: 0, Z: p.centerZ - p.halfW},
			math3d.Vec3{X: p.centerX + p.halfW, Y: p.height, Z: p.centerZ + p.halfW},
		)
	}
	return csmTestMergeMeshes(boxes...)
}

func csmTestAssertGolden(t *testing.T, name string, got *image.RGBA) {
	t.Helper()
	path := gametest.GoldenPath("", name)
	if gametest.ShouldUpdateGolden() {
		if err := gametest.WritePNG(path, got); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := gametest.ReadPNG(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (UPDATE_GOLDEN=1 writes it)", path, err)
	}
	if !want.Bounds().Eq(got.Bounds()) {
		t.Fatalf("%s: size %v, golden %v", name, got.Bounds(), want.Bounds())
	}
	const channelTolerance = 3
	far := 0
	b := got.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g := got.RGBAAt(x, y)
			wr, wg, wb, wa := want.At(x, y).RGBA()
			w := [4]int{int(wr >> 8), int(wg >> 8), int(wb >> 8), int(wa >> 8)}
			for i, c := range [4]int{int(g.R), int(g.G), int(g.B), int(g.A)} {
				if d := c - w[i]; d > channelTolerance || d < -channelTolerance {
					far++
					break
				}
			}
		}
	}
	if limit := b.Dx() * b.Dy() / 200; far > limit {
		dir := t.TempDir()
		diff, _ := gametest.DiffImage(got, want)
		_ = gametest.WritePNG(dir+"/"+name+".got.png", got)
		_ = gametest.WritePNG(dir+"/"+name+".diff.png", diff)
		t.Fatalf("%s: %d pixels differ from golden (limit %d); got and diff written to %s", name, far, limit, dir)
	}
}

// csmTestLit holds the camera, light and per-draw buffers and the LitPBRCSMWGSL pipeline that draw a scene shadowed by a CascadedShadowMap.
type csmTestLit struct {
	camBuf   *render3d.CameraBuffer
	lightBuf *render3d.GPULightBuffer
	pipe     *gpu.Pipeline
	draws    []*render3d.PBRDrawBuffer
}

func csmTestNewLit(t *testing.T, cx *core.Scope, dev *gpu.Device, csm *render3d.CascadedShadowMap, rig render3d.LightRig, draws []render3d.PBRDrawUniform, retained bool) csmTestLit {
	t.Helper()
	var l csmTestLit
	var err error
	l.camBuf, err = render3d.NewCameraBuffer(cx, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	l.lightBuf, err = render3d.NewLightBuffer(cx, dev, rig, render3d.LightBufferOptions{Label: "csm-lights", Retained: retained})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	var layout *gpu.BindGroupLayout
	for i, u := range draws {
		db, err := render3d.NewPBRDrawBuffer(cx, dev, layout, u, retained)
		if err != nil {
			t.Fatalf("NewPBRDrawBuffer[%d]: %v", i, err)
		}
		if layout == nil {
			layout = db.Layout()
		}
		l.draws = append(l.draws, db)
	}
	l.pipe, err = render3d.NewLitPBRCSMPipeline(cx, dev, l.camBuf.Layout(), l.lightBuf.Layout(), layout, csm.Layout(), gpu.FormatDepth24Plus)
	if err != nil {
		t.Fatalf("NewLitPBRCSMPipeline: %v", err)
	}
	if cx == nil {
		t.Cleanup(func() {
			_ = l.pipe.Close()
			for _, d := range l.draws {
				_ = d.Close()
			}
			_ = l.lightBuf.Close()
			_ = l.camBuf.Close()
		})
	}
	return l
}

// draw records the main lit pass: meshes[i] is drawn with the i-th per-draw buffer.
func (l csmTestLit) draw(pass *gpu.RenderPass, csm *render3d.CascadedShadowMap, meshes ...*render3d.GPUMesh) {
	pass.SetPipeline(l.pipe)
	pass.SetBindGroup(render3d.CameraGroup, l.camBuf.BindGroup())
	pass.SetBindGroup(render3d.LightGroup, l.lightBuf.BindGroup())
	pass.SetBindGroup(render3d.CSMGroup, csm.BindGroup())
	for i, m := range meshes {
		pass.SetBindGroup(render3d.PBRDrawGroup, l.draws[i].BindGroup())
		m.Draw(pass)
	}
}

// csmTestMainPass begins the main color + depth pass of a test frame.
func csmTestMainPass(enc render3d.RenderPassStarter, clear core.Color) *gpu.RenderPass {
	return enc.BeginRenderPass(gpu.RenderPassDesc{
		Label: "csm-main",
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: clear,
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1.0,
		},
	})
}

// csmTestExpected returns the sRGB 8-bit color LitPBRCSMWGSL draws for an up-facing ground point wp at view depth viewDepth, from the CPU references EvaluateCSMShadow, ShadePointShadowed, CSMFrame.DebugTint and LinearToSRGB.
func csmTestExpected(frame render3d.CSMFrame, atlas render3d.CSMDepthAtlas, cam *scene3d.Camera, rig render3d.LightRig, mat render3d.PBRMaterial, wp math3d.Vec3, viewDepth float32) ([3]int, float32) {
	vis := render3d.EvaluateCSMShadow(frame, atlas, wp, math3d.Vec3Up(), viewDepth)
	e := cam.Eye()
	eye := math3d.Vec3{X: e.X, Y: e.Y, Z: e.Z}
	lin := render3d.ShadePointShadowed(wp, math3d.Vec3Up(), eye.Sub(wp), mat, rig, vis)
	tint := frame.DebugTint(viewDepth)
	lin = math3d.Vec3{X: lin.X * tint.X, Y: lin.Y * tint.Y, Z: lin.Z * tint.Z}
	c := render3d.LinearToSRGB(lin)
	return [3]int{int(math.Round(float64(c.X) * 255)), int(math.Round(float64(c.Y) * 255)), int(math.Round(float64(c.Z) * 255))}, vis
}

func TestCSMSplitsPracticalScheme(t *testing.T) {
	const near, far = float32(0.5), float32(100.0)

	// lambda = 0: exact uniform splits.
	uni := render3d.ComputeCSMSplits(near, far, 4, 0)
	if len(uni) != 5 {
		t.Fatalf("uniform split len = %d, want 5", len(uni))
	}
	for i := range 5 {
		want := near + (far-near)*float32(i)/4.0
		if math.Abs(float64(uni[i]-want)) > 1e-4 {
			t.Errorf("uniform[%d] = %v, want %v", i, uni[i], want)
		}
	}

	// lambda = 1: exact logarithmic splits.
	logS := render3d.ComputeCSMSplits(near, far, 4, 1)
	if len(logS) != 5 {
		t.Fatalf("log split len = %d, want 5", len(logS))
	}
	for i := range 5 {
		want := float32(float64(near) * math.Pow(float64(far)/float64(near), float64(i)/4.0))
		if math.Abs(float64(logS[i]-want)) > 1e-4 {
			t.Errorf("log[%d] = %v, want %v", i, logS[i], want)
		}
	}

	// lambda = 0.75: strictly monotonic, endpoints exact, strictly between log and uniform.
	prac := render3d.ComputeCSMSplits(near, far, 4, 0.75)
	if len(prac) != 5 {
		t.Fatalf("practical split len = %d, want 5", len(prac))
	}
	if prac[0] != near || prac[4] != far {
		t.Errorf("endpoints = [%v, %v], want [%v, %v]", prac[0], prac[4], near, far)
	}
	for i := 1; i < 4; i++ {
		if prac[i] <= prac[i-1] || prac[i] >= prac[i+1] {
			t.Errorf("practical splits not strictly monotonic at %d: %v", i, prac)
		}
		if !(prac[i] > logS[i] && prac[i] < uni[i]) {
			t.Errorf("practical[%d] = %v, want strictly between log=%v and uniform=%v", i, prac[i], logS[i], uni[i])
		}
	}

	// Single cascade returns [near, far].
	one := render3d.ComputeCSMSplits(0.1, 50, 1, 0.75)
	if len(one) != 2 || one[0] != 0.1 || one[1] != 50 {
		t.Fatalf("1-cascade splits = %v, want [0.1 50]", one)
	}

	// Clamped / NaN lambda and invalid inputs.
	clampedLo := render3d.ComputeCSMSplits(near, far, 4, -5)
	clampedHi := render3d.ComputeCSMSplits(near, far, 4, 5)
	nanLam := render3d.ComputeCSMSplits(near, far, 4, float32(math.NaN()))
	for i := range 5 {
		if clampedLo[i] != uni[i] {
			t.Errorf("clampedLo[%d]=%v != uni=%v", i, clampedLo[i], uni[i])
		}
		if clampedHi[i] != logS[i] {
			t.Errorf("clampedHi[%d]=%v != log=%v", i, clampedHi[i], logS[i])
		}
		if nanLam[i] != prac[i] {
			t.Errorf("nanLam[%d]=%v != default(0.75)=%v", i, nanLam[i], prac[i])
		}
	}

	for _, bad := range []struct {
		near, far float32
		count     int
	}{
		{0, 10, 4},
		{-1, 10, 4},
		{10, 10, 4},
		{20, 10, 4},
		{0.1, 10, 0},
		{float32(math.NaN()), 10, 4},
		{0.1, float32(math.Inf(1)), 4},
	} {
		if got := render3d.ComputeCSMSplits(bad.near, bad.far, bad.count, 0.75); got != nil {
			t.Errorf("ComputeCSMSplits(%v, %v, %d) = %v, want nil", bad.near, bad.far, bad.count, got)
		}
	}
}

func TestCSMTexelSnappingStability(t *testing.T) {
	cfgSnap := render3d.DefaultCSMConfig()
	cfgSnap.Resolution = 256
	cfgSnap.TexelSnap = true

	cfgNoSnap := cfgSnap
	cfgNoSnap.TexelSnap = false

	lightDir := math3d.Vec3{X: 0.4, Y: -1.0, Z: -0.3}.Norm()
	proj := math3d.Perspective(math3d.DegToRad(55), 16.0/9.0, 0.5, 80.0)

	baseEye := math3d.Vec3{X: 3.17, Y: 4.23, Z: 8.41}
	baseView := math3d.LookAt(baseEye, baseEye.Add(math3d.Vec3{Z: -10}), math3d.Vec3Up())
	baseFrame := render3d.FitCascades(baseView, proj, 0.5, 80.0, lightDir, cfgSnap)

	// 1. Sub-texel camera translations inside a texel cell produce bit-for-bit identical cascade matrices (zero jitter).
	for c := range baseFrame.CascadeCount {
		// Pick an offset smaller than 0.05 texel along the light's axes.
		tiny := baseFrame.Cascades[c].TexelSize * 0.02
		if tiny > 0.001 {
			tiny = 0.001
		}
		movedEye := baseEye.Add(math3d.Vec3{X: tiny, Y: tiny * 0.5, Z: -tiny * 0.7})
		movedView := math3d.LookAt(movedEye, movedEye.Add(math3d.Vec3{Z: -10}), math3d.Vec3Up())
		movedFrame := render3d.FitCascades(movedView, proj, 0.5, 80.0, lightDir, cfgSnap)

		// Even if a tiny move happens to straddle a rounding boundary for one axis, projecting any static world point must shift by an EXACT integer number of texels.
		staticPt := math3d.Vec3{X: 1.5, Y: 0.5, Z: -5.0}
		p0 := baseFrame.Cascades[c].LightViewProj.TransformPoint(staticPt)
		p1 := movedFrame.Cascades[c].LightViewProj.TransformPoint(staticPt)
		duTex := (p1.X - p0.X) * 0.5 * float32(cfgSnap.Resolution)
		dvTex := (p1.Y - p0.Y) * 0.5 * float32(cfgSnap.Resolution)
		if fracX := math.Abs(float64(duTex) - math.Round(float64(duTex))); fracX > 1e-3 {
			t.Errorf("cascade %d sub-texel move shifted U by fractional texel %v (frac=%v)", c, duTex, fracX)
		}
		if fracY := math.Abs(float64(dvTex) - math.Round(float64(dvTex))); fracY > 1e-3 {
			t.Errorf("cascade %d sub-texel move shifted V by fractional texel %v (frac=%v)", c, dvTex, fracY)
		}
	}

	// 2. Arbitrary multi-meter camera translations ALWAYS shift every static world point by an exact integer number of shadow-map texels when TexelSnap=true, whereas TexelSnap=false produces fractional texel swimming.
	staticPt := math3d.Vec3{X: 2.0, Y: 0.0, Z: -12.0}
	offsets := []math3d.Vec3{
		{X: 0.13, Y: 0.07, Z: -0.29},
		{X: 1.37, Y: -0.41, Z: -2.83},
		{X: -4.19, Y: 1.11, Z: 3.77},
	}
	noSnapFrame0 := render3d.FitCascades(baseView, proj, 0.5, 80.0, lightDir, cfgNoSnap)
	sawNoSnapSwim := false
	for _, off := range offsets {
		eye := baseEye.Add(off)
		view := math3d.LookAt(eye, eye.Add(math3d.Vec3{Z: -10}), math3d.Vec3Up())
		fSnap := render3d.FitCascades(view, proj, 0.5, 80.0, lightDir, cfgSnap)
		fNoSnap := render3d.FitCascades(view, proj, 0.5, 80.0, lightDir, cfgNoSnap)

		for c := range fSnap.CascadeCount {
			if fSnap.Cascades[c].TexelSize != baseFrame.Cascades[c].TexelSize {
				t.Errorf("cascade %d TexelSize changed on translation: %v -> %v", c, baseFrame.Cascades[c].TexelSize, fSnap.Cascades[c].TexelSize)
			}
			p0 := baseFrame.Cascades[c].LightViewProj.TransformPoint(staticPt)
			p1 := fSnap.Cascades[c].LightViewProj.TransformPoint(staticPt)
			duTex := (p1.X - p0.X) * 0.5 * float32(cfgSnap.Resolution)
			dvTex := (p1.Y - p0.Y) * 0.5 * float32(cfgSnap.Resolution)
			if fracX := math.Abs(float64(duTex) - math.Round(float64(duTex))); fracX > 2e-3 {
				t.Errorf("cascade %d offset %v: snapped U shifted by fractional texel %v (frac=%v)", c, off, duTex, fracX)
			}
			if fracY := math.Abs(float64(dvTex) - math.Round(float64(dvTex))); fracY > 2e-3 {
				t.Errorf("cascade %d offset %v: snapped V shifted by fractional texel %v (frac=%v)", c, off, dvTex, fracY)
			}

			q0 := noSnapFrame0.Cascades[c].LightViewProj.TransformPoint(staticPt)
			q1 := fNoSnap.Cascades[c].LightViewProj.TransformPoint(staticPt)
			nuTex := (q1.X - q0.X) * 0.5 * float32(cfgNoSnap.Resolution)
			if math.Abs(float64(nuTex)-math.Round(float64(nuTex))) > 0.05 {
				sawNoSnapSwim = true
			}
		}
	}
	if !sawNoSnapSwim {
		t.Error("expected TexelSnap=false to show fractional texel shift on camera translation")
	}

	// 3. Camera rotation invariance: TexelSize must stay constant as the camera yaws/pitches.
	for _, yawDeg := range []float32{15, 45, 90, 143} {
		rot := math3d.QuatFromEuler(math3d.DegToRad(-12), math3d.DegToRad(yawDeg), 0)
		fwd := rot.RotateVec3(math3d.Vec3Forward())
		view := math3d.LookAt(baseEye, baseEye.Add(fwd), math3d.Vec3Up())
		fRot := render3d.FitCascades(view, proj, 0.5, 80.0, lightDir, cfgSnap)
		for c := range fRot.CascadeCount {
			if fRot.Cascades[c].TexelSize != baseFrame.Cascades[c].TexelSize {
				t.Errorf("yaw %v cascade %d TexelSize changed: %v -> %v", yawDeg, c, baseFrame.Cascades[c].TexelSize, fRot.Cascades[c].TexelSize)
			}
		}
	}
}

func TestCSMCascadeSelectionBlendAndCasterPadding(t *testing.T) {
	cfg := render3d.DefaultCSMConfig()
	cfg.CascadeCount = 4
	cfg.Resolution = 128
	cfg.MaxShadowDistance = 40
	cfg.CascadeBlendRange = 0.2
	cfg.CasterPadding = 30

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Y: 2, Z: 0}, math3d.DegToRad(60), 1, 0.5, 100)
	frame := render3d.FitCascadesFromCamera(cam, math3d.Vec3{Y: -1}, cfg)

	if frame.MaxShadowDistance != 40 {
		t.Fatalf("MaxShadowDistance = %v, want 40", frame.MaxShadowDistance)
	}
	if idx, next, blend := frame.SelectCascade(0); idx != -1 || next != -1 || blend != 0 {
		t.Errorf("SelectCascade(0) = (%d,%d,%v), want (-1,-1,0)", idx, next, blend)
	}
	if idx, next, blend := frame.SelectCascade(40); idx != -1 || next != -1 || blend != 0 {
		t.Errorf("SelectCascade(40) = (%d,%d,%v), want (-1,-1,0)", idx, next, blend)
	}

	// Interior of each cascade selects that cascade with blend = 0.
	for i := range 4 {
		mid := frame.Cascades[i].Near + 0.5*(frame.Cascades[i].Far-frame.Cascades[i].Near)
		idx, next, blend := frame.SelectCascade(mid)
		if idx != i || next != -1 || blend != 0 {
			t.Errorf("cascade %d mid (%v): SelectCascade = (%d,%d,%v), want (%d,-1,0)", i, mid, idx, next, blend, i)
		}
	}

	// Inside the last 20% of cascade 0 -> blends smoothly into cascade 1.
	c0Span := frame.Cascades[0].Far - frame.Cascades[0].Near
	halfwayBlendDepth := frame.Cascades[0].Far - 0.1*c0Span
	idx, next, blend := frame.SelectCascade(halfwayBlendDepth)
	if idx != 0 || next != 1 || math.Abs(float64(blend-0.5)) > 1e-3 {
		t.Errorf("cascade 0 blend midpoint = (%d,%d,%v), want (0,1,0.5)", idx, next, blend)
	}

	// Approaching MaxShadowDistance in cascade 3 -> fades out (next = -1, blend ~ 0.5).
	c3Span := frame.Cascades[3].Far - frame.Cascades[3].Near
	fadeDepth := frame.Cascades[3].Far - 0.1*c3Span
	idx, next, blend = frame.SelectCascade(fadeDepth)
	if idx != 3 || next != -1 || math.Abs(float64(blend-0.5)) > 1e-3 {
		t.Errorf("cascade 3 far fade = (%d,%d,%v), want (3,-1,0.5)", idx, next, blend)
	}

	// Verify CasterPadding captures a tall caster 20 units above the frustum along -lightDir (+Y).
	offFrustumCaster := math3d.Vec3{X: 0, Y: 20, Z: -2}
	ndc := frame.Cascades[0].LightViewProj.TransformPoint(offFrustumCaster)
	if ndc.Z < 0 || ndc.Z > 1 {
		t.Errorf("off-frustum caster at Y=20 projected outside light depth [0,1]: ndc.Z = %v", ndc.Z)
	}

	// Verify CPU EvaluateCSMShadow blends smoothly across cascade 0 -> 1 when tile 0 is shadowed (0.0) and tile 1 is lit (1.0).
	atlasW, atlasH := int(frame.AtlasWidth), int(frame.AtlasHeight)
	depth := make([]float32, atlasW*atlasH)
	for y := range atlasH {
		for x := range atlasW {
			if x < atlasW/2 && y < atlasH/2 {
				depth[y*atlasW+x] = 0.0 // tile 0: fully occluding
			} else {
				depth[y*atlasW+x] = 1.0 // tiles 1..3: fully lit
			}
		}
	}
	atlas := render3d.CSMDepthAtlas{Width: atlasW, Height: atlasH, Depth: depth}
	worldPt := math3d.Vec3{X: 0, Y: 2, Z: -halfwayBlendDepth}
	visBlend := render3d.EvaluateCSMShadow(frame, atlas, worldPt, math3d.Vec3Up(), halfwayBlendDepth)
	if math.Abs(float64(visBlend-0.5)) > 0.02 {
		t.Errorf("EvaluateCSMShadow at blend midpoint = %v, want ~0.5", visBlend)
	}

	// Beyond MaxShadowDistance, EvaluateCSMShadow returns 1.0 (unshadowed).
	if visFar := render3d.EvaluateCSMShadow(frame, atlas, worldPt, math3d.Vec3Up(), 45); visFar != 1.0 {
		t.Errorf("EvaluateCSMShadow beyond MaxShadowDistance = %v, want 1.0", visFar)
	}
}

func TestCSMConfigValidationLayoutAndZeroAlloc(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	tests := []struct {
		name string
		cfg  render3d.CSMConfig
		ok   bool
	}{
		{"zero value", render3d.CSMConfig{}, true},
		{"default", render3d.DefaultCSMConfig(), true},
		{"1 cascade", render3d.CSMConfig{CascadeCount: 1, Resolution: 64}, true},
		{"cascade count too high", render3d.CSMConfig{CascadeCount: render3d.MaxCascades + 1}, false},
		{"negative cascade count", render3d.CSMConfig{CascadeCount: -1}, false},
		{"negative resolution", render3d.CSMConfig{Resolution: -64}, false},
		{"lambda < 0", render3d.CSMConfig{SplitLambda: -0.1}, false},
		{"lambda > 1", render3d.CSMConfig{SplitLambda: 1.1}, false},
		{"lambda NaN", render3d.CSMConfig{SplitLambda: nan}, false},
		{"negative max dist", render3d.CSMConfig{MaxShadowDistance: -1}, false},
		{"inf max dist", render3d.CSMConfig{MaxShadowDistance: inf}, false},
		{"negative depth bias", render3d.CSMConfig{DepthBias: -0.001}, false},
		{"negative normal bias", render3d.CSMConfig{NormalBias: -0.01}, false},
		{"blend > 0.5", render3d.CSMConfig{CascadeBlendRange: 0.6}, false},
		{"blend < 0", render3d.CSMConfig{CascadeBlendRange: -0.05}, false},
		{"unknown filter", render3d.CSMConfig{Filter: render3d.ShadowFilter(99)}, false},
		{"negative caster padding", render3d.CSMConfig{CasterPadding: -5}, false},
		{"negative max casters", render3d.CSMConfig{MaxCasters: -1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.ok && err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
			if !tt.ok && !errors.Is(err, render3d.ErrInvalidCSM) {
				t.Fatalf("Validate = %v, want ErrInvalidCSM", err)
			}
		})
	}

	// Atlas dimensions per cascade count.
	for _, tc := range []struct {
		count int
		w, h  uint32
	}{
		{1, 128, 128},
		{2, 256, 128},
		{3, 256, 256},
		{4, 256, 256},
	} {
		w, h := (render3d.CSMConfig{CascadeCount: tc.count, Resolution: 128}).AtlasSize()
		if w != tc.w || h != tc.h {
			t.Errorf("AtlasSize(count=%d, res=128) = %dx%d, want %dx%d", tc.count, w, h, tc.w, tc.h)
		}
	}

	// Nil device error.
	if _, err := render3d.NewCascadedShadowMap(nil, nil, render3d.DefaultCSMConfig()); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewCascadedShadowMap(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewLitPBRCSMPipeline(nil, nil, nil, nil, nil, nil, gpu.FormatUndefined); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewLitPBRCSMPipeline(nil dev) = %v, want ErrDeviceLost", err)
	}
	if render3d.CSMGroup != render3d.ShadowGroup {
		t.Fatalf("CSMGroup = %d, want ShadowGroup %d", render3d.CSMGroup, render3d.ShadowGroup)
	}

	// Uniform encoding byte layout & 0-alloc verification.
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	// Exceeding MaxTextureDimension2D fails cleanly with ErrInvalidCSM.
	hugeCfg := render3d.DefaultCSMConfig()
	hugeCfg.Resolution = int(dev.Limits().MaxTextureDimension2D) + 1
	if _, err := render3d.NewCascadedShadowMap(nil, dev, hugeCfg); !errors.Is(err, render3d.ErrInvalidCSM) {
		t.Fatalf("NewCascadedShadowMap(oversized) = %v, want ErrInvalidCSM", err)
	}

	cfg := render3d.DefaultCSMConfig()
	cfg.Resolution = 64
	csm, err := render3d.NewCascadedShadowMap(nil, dev, cfg)
	if err != nil {
		t.Fatalf("NewCascadedShadowMap: %v", err)
	}
	t.Cleanup(func() { _ = csm.Close() })

	camView := math3d.LookAt(math3d.Vec3{Y: 5, Z: 5}, math3d.Vec3{Z: -10}, math3d.Vec3Up())
	camProj := math3d.Perspective(math3d.DegToRad(50), 1.33, 0.5, 60)
	lightDir := math3d.Vec3{X: 0.4, Y: -1, Z: -0.2}.Norm()
	frame := render3d.FitCascades(camView, camProj, 0.5, 60, lightDir, cfg)

	var buf [render3d.CSMUniformSize + 8]byte
	encoded := frame.EncodeUniforms(buf[:])
	if len(encoded) != render3d.CSMUniformSize || render3d.CSMUniformSize != 416 {
		t.Fatalf("encoded len = %d, CSMUniformSize = %d, want 416", len(encoded), render3d.CSMUniformSize)
	}
	readF := func(idx int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(encoded[idx*4:]))
	}
	// Check cascade 0 matrix and splits offset (4*16 + 4*4 = 80 floats = byte 320).
	for i := range 16 {
		if readF(i) != frame.Cascades[0].LightViewProj[i] {
			t.Fatalf("light_vp0[%d] = %v, want %v", i, readF(i), frame.Cascades[0].LightViewProj[i])
		}
	}
	for i := range 4 {
		if readF(80+i) != frame.Cascades[i].Far {
			t.Fatalf("splits[%d] = %v, want %v", i, readF(80+i), frame.Cascades[i].Far)
		}
	}

	if allocs := testing.AllocsPerRun(100, func() {
		f := render3d.FitCascades(camView, camProj, 0.5, 60, lightDir, cfg)
		f.EncodeUniforms(buf[:])
	}); allocs != 0 {
		t.Errorf("FitCascades + EncodeUniforms allocated %v times, want 0", allocs)
	}

	// A short slice must panic in EncodeUniforms.
	func() {
		defer func() {
			if recover() == nil {
				t.Error("EncodeUniforms on short dst did not panic")
			}
		}()
		frame.EncodeUniforms(make([]byte, 0, render3d.CSMUniformSize))
	}()

	// The lit PBR pipeline compiles with the CSM layout and refuses a second group-3 layout.
	if _, err := render3d.NewLitPBRCSMPipeline(nil, dev, csm.Layout(), csm.Layout(), csm.Layout(), nil, gpu.FormatUndefined); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("NewLitPBRCSMPipeline(nil csm layout) = %v, want ErrInvalidDescriptor", err)
	}
	if _, err := render3d.NewLitPBRPipelineWithOptions(nil, dev, csm.Layout(), csm.Layout(), csm.Layout(), render3d.LitPBRPipelineOptions{CSMLayout: csm.Layout(), ShadowLayout: csm.Layout()}); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("CSMLayout with ShadowLayout = %v, want ErrInvalidDescriptor", err)
	}
	for _, src := range []string{render3d.LitPBRCSMWGSL, render3d.CameraWGSL + render3d.CSMWGSLForGroup(1) + `
@fragment
fn fs_main(@location(0) world_pos: vec3<f32>) -> @location(0) vec4<f32> {
	return vec4<f32>(csm_debug_tint(world_pos) * sample_csm_shadow(world_pos, vec3<f32>(0.0, 1.0, 0.0)), 1.0);
}
`} {
		if _, err := gpu.CompileWGSL(src); err != nil {
			t.Errorf("CompileWGSL: %v", err)
		}
	}

	// Update refuses a frame fitted for another atlas: its tiles, viewports, texel size or filter would not match the atlas and sampler.
	if err := csm.Update(dev, frame); err != nil {
		t.Fatalf("Update(matching frame) = %v", err)
	}
	badFrame := frame
	badFrame.CascadeCount = 0
	if err := csm.Update(dev, badFrame); !errors.Is(err, render3d.ErrInvalidCSM) {
		t.Errorf("Update(0 cascades) = %v, want ErrInvalidCSM", err)
	}
	mismatched := map[string]func(*render3d.CSMConfig){
		"fewer cascades":    func(c *render3d.CSMConfig) { c.CascadeCount = 2 },
		"larger resolution": func(c *render3d.CSMConfig) { c.Resolution = 128 },
		"other filter":      func(c *render3d.CSMConfig) { c.Filter = render3d.ShadowFilterHard },
	}
	for name, mutate := range mismatched {
		other := cfg
		mutate(&other)
		f := render3d.FitCascades(camView, camProj, 0.5, 60, lightDir, other)
		if err := csm.Update(dev, f); !errors.Is(err, render3d.ErrInvalidCSM) {
			t.Errorf("Update(%s) = %v, want ErrInvalidCSM", name, err)
		}
		if got := csm.CascadeCount(); got != cfg.CascadeCount {
			t.Errorf("after Update(%s): CascadeCount() = %d, want %d", name, got, cfg.CascadeCount)
		}
		if got := csm.Frame(); got != frame {
			t.Errorf("Update(%s) replaced the uploaded frame", name)
		}
	}
	for i := range csm.CascadeCount() {
		pass := csm.BeginCascadePass(dev.BeginEncoder(), i)
		if pass == nil {
			t.Fatalf("BeginCascadePass(%d) = nil for i < CascadeCount()", i)
		}
		pass.End()
	}
	if err := csm.Update(nil, frame); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("Update(nil dev) = %v, want ErrDeviceLost", err)
	}
	if csm.ByteSize() == 0 {
		t.Error("ByteSize() = 0, want > 0")
	}
}

func TestCSMLargeSceneReferenceRender(t *testing.T) {
	scene := csmTestLargeScene()
	pillarCPU := scene.pillarMesh()

	cfg := render3d.DefaultCSMConfig()
	cfg.CascadeCount = 4
	cfg.Resolution = 128
	cfg.SplitLambda = 0.75
	cfg.MaxShadowDistance = 56.0
	cfg.DepthBias = 0.0025
	cfg.NormalBias = 0.03
	cfg.CascadeBlendRange = 0.1
	cfg.TexelSnap = true
	cfg.Filter = render3d.ShadowFilterPCF3x3
	cfg.CasterPadding = 25.0

	// Compare near cascade texel density vs single-map equivalent at same resolution.
	csmFrame := render3d.FitCascadesFromCamera(scene.cam, scene.lightDir, cfg)
	singleCfg := cfg
	singleCfg.CascadeCount = 1
	singleFrame := render3d.FitCascadesFromCamera(scene.cam, scene.lightDir, singleCfg)
	densityGain := singleFrame.Cascades[0].TexelSize / csmFrame.Cascades[0].TexelSize
	if densityGain < 3.0 {
		t.Fatalf("near cascade texel density gain = %.2fx (csm=%v, single=%v), want >= 3.0x",
			densityGain, csmFrame.Cascades[0].TexelSize, singleFrame.Cascades[0].TexelSize)
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
			var (
				dev        *gpu.Device
				lit        csmTestLit
				csm        *render3d.CascadedShadowMap
				groundMesh *render3d.GPUMesh
				pillarMesh *render3d.GPUMesh
				activeCfg  = cfg
				lastFrame  render3d.CSMFrame
				depthAtlas render3d.CSMDepthAtlas
			)

			opts := append([]nsgotest.Option{nsgotest.Size(csmTestWidth, csmTestHeight), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "csm-viewport",
					Style:       core.Style{Width: core.Pt(csmTestWidth), Height: core.Pt(csmTestHeight)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(d *gpu.Device) {
						dev = d
						var err error
						csm, err = render3d.NewCascadedShadowMap(cx, dev, cfg)
						if err != nil {
							t.Fatalf("NewCascadedShadowMap: %v", err)
						}
						lit = csmTestNewLit(t, cx, dev, csm, scene.rig, []render3d.PBRDrawUniform{
							render3d.NewPBRDrawUniform(math3d.Mat4Identity(), scene.groundMat),
							render3d.NewPBRDrawUniform(math3d.Mat4Identity(), scene.pillarMat),
						}, false)
						groundMesh, err = render3d.UploadMesh(cx, dev, scene.ground, render3d.MeshUpload{Label: "csm-ground"})
						if err != nil {
							t.Fatalf("UploadMesh ground: %v", err)
						}
						pillarMesh, err = render3d.UploadMesh(cx, dev, pillarCPU, render3d.MeshUpload{Label: "csm-pillars"})
						if err != nil {
							t.Fatalf("UploadMesh pillars: %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						lastFrame = render3d.FitCascadesFromCamera(scene.cam, scene.rig.Directional[0].Direction, activeCfg)
						if err := csm.Update(dev, lastFrame); err != nil {
							t.Fatalf("csm.Update: %v", err)
						}
						for i := range csm.CascadeCount() {
							sp := csm.BeginCascadePass(frame, i)
							csm.DrawCaster(sp, pillarMesh, math3d.Mat4Identity())
							sp.End()
						}

						lit.camBuf.WriteCamera(scene.cam)
						pass := csmTestMainPass(frame, core.RGB(0x5278a8))
						lit.draw(pass, csm, groundMesh, pillarMesh)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)
			img, err := app.GPUReadPixels("id:csm-viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			got := csmTestCloneRGBA(img)
			csmTestAssertGolden(t, "csm_large_scene", got)

			// Read back the GPU depth atlas asynchronously on the engine goroutine and verify CPU reference parity.
			var readDone bool
			app.Post(func() {
				render3d.ReadCSMDepthAtlasAsync(dev, csm, func(a render3d.CSMDepthAtlas, rerr error) {
					if rerr != nil {
						t.Errorf("ReadCSMDepthAtlasAsync: %v", rerr)
						return
					}
					depthAtlas = a
					readDone = true
				})
			})
			app.Settle()
			app.Advance(16 * time.Millisecond)
			app.Settle()
			if !readDone {
				t.Fatal("ReadCSMDepthAtlasAsync callback did not complete")
			}

			// Sample world points on the ground plane near both a near pillar (cascade 0) and a far pillar (cascade 3): each pillar casts its shadow along (+lightDir.X / -lightDir.Y, +lightDir.Z / -lightDir.Y) on Y = 0.
			dxPerH := scene.lightDir.X / (-scene.lightDir.Y)
			dzPerH := scene.lightDir.Z / (-scene.lightDir.Y)

			type probe struct {
				name       string
				world      math3d.Vec3
				wantShadow bool
				wantCasc   int
			}
			nearP := scene.pillars[0]
			c1P := scene.pillars[1]
			midP := scene.pillars[2]
			farP := scene.pillars[4]
			probes := []probe{
				{
					name:       "near pillar shadow (cascade 0)",
					world:      math3d.Vec3{X: nearP.centerX + dxPerH*0.8, Y: 0, Z: nearP.centerZ + dzPerH*0.8},
					wantShadow: true,
					wantCasc:   0,
				},
				{
					name:       "near sunlit ground (cascade 0)",
					world:      math3d.Vec3{X: nearP.centerX + 2.0, Y: 0, Z: nearP.centerZ},
					wantShadow: false,
					wantCasc:   0,
				},
				{
					name:       "cascade 1 pillar shadow",
					world:      math3d.Vec3{X: c1P.centerX + dxPerH*1.3, Y: 0, Z: c1P.centerZ + dzPerH*1.3},
					wantShadow: true,
					wantCasc:   1,
				},
				{
					name:       "mid pillar shadow (cascade 2)",
					world:      math3d.Vec3{X: midP.centerX + dxPerH*2.0, Y: 0, Z: midP.centerZ + dzPerH*2.0},
					wantShadow: true,
					wantCasc:   2,
				},
				{
					name:       "far pillar shadow (cascade 3)",
					world:      math3d.Vec3{X: farP.centerX + dxPerH*3.2, Y: 0, Z: farP.centerZ + dzPerH*3.2},
					wantShadow: true,
					wantCasc:   3,
				},
				{
					name:       "far sunlit ground (cascade 3)",
					world:      math3d.Vec3{X: 1.5, Y: 0, Z: farP.centerZ},
					wantShadow: false,
					wantCasc:   3,
				},
			}

			camView := scene.cam.ViewMatrix()
			for _, pr := range probes {
				scr, _, visible := scene.cam.WorldToScreen(pr.world, csmTestWidth, csmTestHeight)
				if !visible {
					t.Fatalf("probe %q at %v is not visible in viewport", pr.name, pr.world)
				}
				px, py := int(scr.X), int(scr.Y)
				// Intersect the exact GPU pixel-center ray with Y = 0 so CPU EvaluateCSM samples the exact world point rasterized at pixel (px, py).
				ray := scene.cam.ScreenRay(float32(px)+0.5, float32(py)+0.5, csmTestWidth, csmTestHeight)
				tHit := -ray.Origin.Y / ray.Dir.Y
				exactWorld := ray.Origin.Add(ray.Dir.Scale(tHit))

				viewDepth := -camView.TransformPoint(exactWorld).Z
				gotCasc, _, _ := lastFrame.SelectCascade(viewDepth)
				if pr.wantCasc >= 0 && gotCasc != pr.wantCasc {
					t.Errorf("probe %q (viewDepth=%.2f): cascade = %d, want %d", pr.name, viewDepth, gotCasc, pr.wantCasc)
				}

				// Compare the CPU reference shadow factor and shaded color with the GPU pixel.
				want, cpuShadow := csmTestExpected(lastFrame, depthAtlas, scene.cam, scene.rig, scene.groundMat, exactWorld, viewDepth)
				if pr.wantShadow && cpuShadow > 0.35 {
					t.Errorf("probe %q: CPU shadow factor = %.3f, want <= 0.35 (in shadow)", pr.name, cpuShadow)
				}
				if !pr.wantShadow && cpuShadow < 0.85 {
					t.Errorf("probe %q: CPU shadow factor = %.3f, want >= 0.85 (lit)", pr.name, cpuShadow)
				}

				gpuPix := got.RGBAAt(px, py)
				const tol = 4
				if csmTestAbsInt(int(gpuPix.R)-want[0]) > tol || csmTestAbsInt(int(gpuPix.G)-want[1]) > tol || csmTestAbsInt(int(gpuPix.B)-want[2]) > tol {
					t.Errorf("probe %q at (%d,%d): GPU RGB=(%d,%d,%d), CPU reference=%v (shadow=%.2f)", pr.name, px, py, gpuPix.R, gpuPix.G, gpuPix.B, want, cpuShadow)
				}
			}

			// Verify debug cascade tint mode visibly distinguishes cascade 0 (red-tinted) vs cascade 3 (yellow-tinted).
			if p.name == "full" {
				app.Post(func() {
					activeCfg.DebugCascades = true
				})
				app.Settle()
				app.Advance(16 * time.Millisecond)
				dbgImg, err := app.GPUReadPixels("id:csm-viewport")
				if err != nil {
					t.Fatalf("GPUReadPixels debug: %v", err)
				}
				nearScr, _, _ := scene.cam.WorldToScreen(probes[1].world, csmTestWidth, csmTestHeight)
				farScr, _, _ := scene.cam.WorldToScreen(probes[5].world, csmTestWidth, csmTestHeight)
				nearC := dbgImg.RGBAAt(int(nearScr.X), int(nearScr.Y))
				farC := dbgImg.RGBAAt(int(farScr.X), int(farScr.Y))
				if nearC.R <= nearC.G+20 {
					t.Errorf("debug cascade 0 pixel %+v expected strong red tint", nearC)
				}
				if farC.R <= farC.B+20 || farC.G <= farC.B+20 {
					t.Errorf("debug cascade 3 pixel %+v expected yellow tint (R,G > B)", farC)
				}
				if !lastFrame.Config.DebugCascades {
					t.Fatal("debug frame was not fitted with DebugCascades")
				}
				for _, pr := range []probe{probes[1], probes[5]} {
					scr, _, _ := scene.cam.WorldToScreen(pr.world, csmTestWidth, csmTestHeight)
					px, py := int(scr.X), int(scr.Y)
					ray := scene.cam.ScreenRay(float32(px)+0.5, float32(py)+0.5, csmTestWidth, csmTestHeight)
					wp := ray.Origin.Add(ray.Dir.Scale(-ray.Origin.Y / ray.Dir.Y))
					want, _ := csmTestExpected(lastFrame, depthAtlas, scene.cam, scene.rig, scene.groundMat, wp, -camView.TransformPoint(wp).Z)
					c := dbgImg.RGBAAt(px, py)
					if csmTestAbsInt(int(c.R)-want[0]) > 4 || csmTestAbsInt(int(c.G)-want[1]) > 4 || csmTestAbsInt(int(c.B)-want[2]) > 4 {
						t.Errorf("debug probe %q at (%d,%d): GPU RGB=(%d,%d,%d), CPU reference with DebugTint=%v", pr.name, px, py, c.R, c.G, c.B, want)
					}
				}
			}
		})
	}
}

// csmTestSmallScene renders a box placed by a non-identity model matrix over a ground plane into a 2-cascade map, drawing the casters into the cascades in the given order.
type csmTestSmallScene struct {
	cam         *scene3d.Camera
	rig         render3d.LightRig
	groundMat   render3d.PBRMaterial
	casterModel math3d.Mat4
}

func newCSMTestSmallScene() csmTestSmallScene {
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 6.0, Z: 2.5}, math3d.DegToRad(50), 1, 0.5, 30)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0, Z: -4.5}, math3d.Vec3Up())
	return csmTestSmallScene{
		cam: cam,
		// A white sun and gray ambient keep the gray ground neutral, so penumbra pixels are easy to tell from the red caster.
		rig: render3d.LightRig{
			Ambient:     render3d.AmbientLight{Color: math3d.Vec3{X: 0.15, Y: 0.15, Z: 0.15}, Intensity: 1},
			Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{X: 0.55, Y: -0.85, Z: 0.0}, Color: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 2.5}},
		},
		groundMat:   render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.85, Y: 0.85, Z: 0.85}, Roughness: 1},
		casterModel: math3d.Translate(math3d.Vec3{X: -0.8, Y: 0, Z: -4.0}),
	}
}

func (sc csmTestSmallScene) render(t *testing.T, filter render3d.ShadowFilter, order []int) (*image.RGBA, render3d.CSMFrame, render3d.CSMDepthAtlas) {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(96, 96, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	cfg := render3d.DefaultCSMConfig()
	cfg.CascadeCount = 2
	cfg.Resolution = 128
	cfg.MaxShadowDistance = 30
	cfg.DepthBias = 0.002
	cfg.NormalBias = 0.01
	cfg.Filter = filter
	cfg.CascadeBlendRange = 0

	csm, err := render3d.NewCascadedShadowMap(nil, dev, cfg)
	if err != nil {
		t.Fatalf("NewCascadedShadowMap: %v", err)
	}
	t.Cleanup(func() { _ = csm.Close() })
	casterMat := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.8, Y: 0.3, Z: 0.2}, Roughness: 1}
	lit := csmTestNewLit(t, nil, dev, csm, sc.rig, []render3d.PBRDrawUniform{
		render3d.NewPBRDrawUniform(math3d.Mat4Identity(), sc.groundMat),
		render3d.NewPBRDrawUniform(sc.casterModel, casterMat),
	}, false)

	groundMesh, err := render3d.UploadMesh(nil, dev, csmTestGroundMesh(-8, 8, -14, 4), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh ground: %v", err)
	}
	t.Cleanup(func() { _ = groundMesh.Close() })
	// A box at the origin, placed in the scene only by its model matrix.
	box, err := render3d.UploadMesh(nil, dev, csmTestBoxMesh(math3d.Vec3{X: -0.75, Y: 0, Z: -0.75}, math3d.Vec3{X: 0.75, Y: 2.4, Z: 0.75}), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh box: %v", err)
	}
	t.Cleanup(func() { _ = box.Close() })

	frame := render3d.FitCascadesFromCamera(sc.cam, sc.rig.Directional[0].Direction, cfg)
	if err := csm.Update(dev, frame); err != nil {
		t.Fatalf("csm.Update: %v", err)
	}
	enc := dev.BeginEncoder()
	for _, i := range order {
		sp := csm.BeginCascadePass(enc, i)
		if !csm.DrawCaster(sp, box, sc.casterModel) {
			t.Fatalf("DrawCaster into cascade %d returned false", i)
		}
		sp.End()
	}
	lit.camBuf.WriteCamera(sc.cam)
	mp := csmTestMainPass(enc, core.RGB(0x101820))
	lit.draw(mp, csm, groundMesh, box)
	mp.End()
	dev.Submit(enc.Finish())
	if sr.LastError() != nil {
		t.Fatalf("SoftwareRenderer: %v", sr.LastError())
	}
	atlas, err := render3d.ReadCSMDepthAtlas(dev, csm)
	if err != nil {
		t.Fatalf("ReadCSMDepthAtlas: %v", err)
	}
	return csmTestCloneRGBA(sr.Image()), frame, atlas
}

func TestCSMHardVsPCFAndNonIdentityCasterTransform(t *testing.T) {
	// Under ShadowFilterHard every ground pixel is either fully shadowed or fully lit; ShadowFilterPCF3x3 adds a penumbra along the shadow edge. The box is placed only by its model matrix, so a shadow at all shows DrawCaster applied it.
	sc := newCSMTestSmallScene()
	camView := sc.cam.ViewMatrix()
	e := sc.cam.Eye()
	eye := math3d.Vec3{X: e.X, Y: e.Y, Z: e.Z}

	classify := func(img *image.RGBA, frame render3d.CSMFrame, atlas render3d.CSMDepthAtlas) (shadowed, penumbra, lit, mismatched int) {
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := img.RGBAAt(x, y)
				// Ground pixels are neutral gray; the caster is red and the clear color is blue-black.
				if csmTestAbsInt(int(c.R)-int(c.G)) > 2 || csmTestAbsInt(int(c.R)-int(c.B)) > 2 || c.R < 25 {
					continue
				}
				ray := sc.cam.ScreenRay(float32(x)+0.5, float32(y)+0.5, 96, 96)
				if ray.Dir.Y >= 0 {
					continue
				}
				wp := ray.Origin.Add(ray.Dir.Scale(-ray.Origin.Y / ray.Dir.Y))
				dark := render3d.LinearToSRGB(render3d.ShadePointShadowed(wp, math3d.Vec3Up(), eye.Sub(wp), sc.groundMat, sc.rig, 0)).X * 255
				bright := render3d.LinearToSRGB(render3d.ShadePointShadowed(wp, math3d.Vec3Up(), eye.Sub(wp), sc.groundMat, sc.rig, 1)).X * 255
				f := (float32(c.R) - dark) / (bright - dark)
				switch {
				case f < 0.1:
					shadowed++
				case f > 0.9:
					lit++
				default:
					penumbra++
				}
				want, _ := csmTestExpected(frame, atlas, sc.cam, sc.rig, sc.groundMat, wp, -camView.TransformPoint(wp).Z)
				if csmTestAbsInt(int(c.R)-want[0]) > 4 {
					mismatched++
				}
			}
		}
		return shadowed, penumbra, lit, mismatched
	}

	hardImg, hardFrame, hardAtlas := sc.render(t, render3d.ShadowFilterHard, []int{0, 1})
	pcfImg, pcfFrame, pcfAtlas := sc.render(t, render3d.ShadowFilterPCF3x3, []int{0, 1})
	hShad, hPen, hLit, hBad := classify(hardImg, hardFrame, hardAtlas)
	pShad, pPen, pLit, pBad := classify(pcfImg, pcfFrame, pcfAtlas)
	if hShad < 50 || hLit < 200 {
		t.Fatalf("hard shadow render missing shadow/lit ground: shadowed=%d lit=%d", hShad, hLit)
	}
	if pShad < 20 || pLit < 200 {
		t.Fatalf("PCF shadow render missing shadow/lit ground: shadowed=%d lit=%d", pShad, pLit)
	}
	if hPen != 0 {
		t.Errorf("ShadowFilterHard produced %d penumbra pixels, want 0", hPen)
	}
	if pPen < 20 {
		t.Errorf("ShadowFilterPCF3x3 produced only %d penumbra pixels, want >= 20", pPen)
	}
	// A pixel right on a shadow edge can land on the other side of a depth comparison on the CPU, so allow a handful.
	if hBad > 4 || pBad > 4 {
		t.Errorf("ground pixels off the CPU reference by more than 4: hard %d, PCF %d", hBad, pBad)
	}

	// The first pass after Update clears the atlas whichever cascade it draws, so drawing cascade 1 before cascade 0 gives the same atlas and image.
	revImg, _, revAtlas := sc.render(t, render3d.ShadowFilterPCF3x3, []int{1, 0})
	if d := csmTestDiffPixels(pcfImg, revImg); d != 0 {
		t.Errorf("rendering cascades in reverse order changed %d pixels", d)
	}
	for i := range pcfAtlas.Depth {
		if pcfAtlas.Depth[i] != revAtlas.Depth[i] {
			t.Fatalf("rendering cascades in reverse order changed atlas texel %d: %v, want %v", i, revAtlas.Depth[i], pcfAtlas.Depth[i])
		}
	}
}

func TestCSMDrawCasterSlots(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	cfg := render3d.DefaultCSMConfig()
	cfg.Resolution = 32
	cfg.MaxCasters = 4 // slot 0 is the identity, so three distinct models fit
	csm, err := render3d.NewCascadedShadowMap(nil, dev, cfg)
	if err != nil {
		t.Fatalf("NewCascadedShadowMap: %v", err)
	}
	t.Cleanup(func() { _ = csm.Close() })
	mesh, err := render3d.UploadMesh(nil, dev, csmTestBoxMesh(math3d.Vec3{X: -0.5, Y: 0, Z: -0.5}, math3d.Vec3{X: 0.5, Y: 1, Z: 0.5}), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = mesh.Close() })
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Y: 4, Z: 4}, math3d.DegToRad(60), 1, 0.5, 40)
	cam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
	frame := render3d.FitCascadesFromCamera(cam, math3d.Vec3{X: 0.3, Y: -1, Z: -0.2}, csm.Config())

	models := []math3d.Mat4{
		math3d.Translate(math3d.Vec3{X: 1}),
		math3d.Translate(math3d.Vec3{X: 2}),
		math3d.Translate(math3d.Vec3{X: 3}),
	}
	extra := math3d.Translate(math3d.Vec3{X: 4})
	for round := range 2 {
		if err := csm.Update(dev, frame); err != nil {
			t.Fatalf("Update: %v", err)
		}
		enc := dev.BeginEncoder()
		// Each of the three models is drawn into all four cascades: twelve draws, three slots.
		for i := range csm.CascadeCount() {
			pass := csm.BeginCascadePass(enc, i)
			for _, m := range models {
				if !csm.DrawCaster(pass, mesh, m) {
					t.Fatalf("round %d cascade %d: DrawCaster(%v) = false with a slot per model", round, i, m[12])
				}
			}
			if !csm.DrawCaster(pass, mesh, math3d.Mat4Identity()) {
				t.Fatalf("round %d cascade %d: DrawCaster(identity) = false", round, i)
			}
			if csm.DrawCaster(pass, mesh, extra) {
				t.Fatalf("round %d cascade %d: DrawCaster(fourth model) = true past MaxCasters", round, i)
			}
			pass.End()
		}
		dev.Submit(enc.Finish())
		if sr.LastError() != nil {
			t.Fatalf("SoftwareRenderer: %v", sr.LastError())
		}
	}
	if csm.DrawCaster(nil, mesh, extra) {
		t.Error("DrawCaster(nil pass) = true")
	}
}

func TestCSMOrthographicCamera(t *testing.T) {
	cfg := render3d.DefaultCSMConfig()
	cfg.CascadeCount = 2
	cfg.Resolution = 64
	lightDir := math3d.Vec3{X: 0.3, Y: -1, Z: -0.2}
	eye := math3d.Vec3{Y: 10, Z: 10}
	target := math3d.Vec3{}

	// An orthographic camera whose near plane is behind it: cascades start at 0.1 and still cover the stretch of the view frustum they claim.
	cam := scene3d.NewOrthographicCamera(eye, 20, 1, -10, 50)
	cam.LookAt(target, math3d.Vec3Up())
	frame := render3d.FitCascadesFromCamera(cam, lightDir, cfg)
	if frame.Splits[0] != 0.1 || frame.Splits[2] != 50 {
		t.Fatalf("splits = %v, want them to span [0.1, 50]", frame.Splits[:3])
	}
	view := cam.ViewMatrix()
	inv, ok := view.Inverse()
	if !ok {
		t.Fatal("camera view matrix is singular")
	}
	for i := range frame.CascadeCount {
		c := frame.Cascades[i]
		for _, d := range []float32{c.Near, c.Far} {
			for _, xy := range [][2]float32{{-10, -10}, {10, -10}, {10, 10}, {-10, 10}} {
				// The view-space corner of the camera's box at depth d, in world space, must project inside the cascade.
				wp := inv.TransformPoint(math3d.Vec3{X: xy[0], Y: xy[1], Z: -d})
				ndc := c.LightViewProj.TransformPoint(wp)
				const eps = 1e-3
				if ndc.X < -1-eps || ndc.X > 1+eps || ndc.Y < -1-eps || ndc.Y > 1+eps || ndc.Z < -eps || ndc.Z > 1+eps {
					t.Errorf("cascade %d: frustum corner %v at depth %v projects to %v, outside the cascade", i, xy, d, ndc)
				}
			}
		}
	}

	// Near and Far both zero take the 0.1 and 1000 defaults ProjMatrix uses.
	def := scene3d.NewOrthographicCamera(eye, 20, 1, 0, 0)
	def.LookAt(target, math3d.Vec3Up())
	if f := render3d.FitCascadesFromCamera(def, lightDir, cfg); f.Splits[0] != 0.1 || f.Splits[2] != 1000 {
		t.Errorf("default orthographic splits = %v, want them to span [0.1, 1000]", f.Splits[:3])
	}
}

func TestCSMRetainedDeviceLossAndClose(t *testing.T) {
	scene := csmTestLargeScene()
	pillarCPU := scene.pillarMesh()

	cfg := render3d.DefaultCSMConfig()
	cfg.CascadeCount = 2
	cfg.Resolution = 64
	cfg.MaxShadowDistance = 30
	cfg.Filter = render3d.ShadowFilterHard
	cfg.Retained = true

	const size = 64
	var (
		dev        *gpu.Device
		lit        csmTestLit
		csm        *render3d.CascadedShadowMap
		groundMesh *render3d.GPUMesh
		pillarMesh *render3d.GPUMesh
	)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "csm-retained",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(d *gpu.Device) {
				dev = d
				var err error
				if csm == nil {
					csm, err = render3d.NewCascadedShadowMap(cx, dev, cfg)
					if err != nil {
						t.Fatalf("NewCascadedShadowMap: %v", err)
					}
				}
				lit = csmTestNewLit(t, cx, dev, csm, scene.rig, []render3d.PBRDrawUniform{
					render3d.NewPBRDrawUniform(math3d.Mat4Identity(), scene.groundMat),
					render3d.NewPBRDrawUniform(math3d.Mat4Identity(), scene.pillarMat),
				}, true)
				if groundMesh == nil {
					groundMesh, err = render3d.UploadMesh(cx, dev, scene.ground, render3d.MeshUpload{Label: "g", Retained: true})
					if err != nil {
						t.Fatalf("UploadMesh ground: %v", err)
					}
					pillarMesh, err = render3d.UploadMesh(cx, dev, pillarCPU, render3d.MeshUpload{Label: "p", Retained: true})
					if err != nil {
						t.Fatalf("UploadMesh pillars: %v", err)
					}
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				f := render3d.FitCascadesFromCamera(scene.cam, scene.lightDir, cfg)
				if err := csm.Update(dev, f); err != nil {
					t.Fatalf("csm.Update: %v", err)
				}
				for i := range csm.CascadeCount() {
					sp := csm.BeginCascadePass(frame, i)
					csm.DrawCaster(sp, pillarMesh, math3d.Mat4Identity())
					sp.End()
				}
				lit.camBuf.WriteCamera(scene.cam)
				pass := csmTestMainPass(frame, core.RGB(0x5278a8))
				lit.draw(pass, csm, groundMesh, pillarMesh)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	app.Advance(16 * time.Millisecond)
	before, err := app.GPUReadPixels("id:csm-retained")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	beforeCopy := csmTestCloneRGBA(before)
	firstCSM := csm
	if !firstCSM.Valid() {
		t.Fatal("csm not valid before device loss")
	}

	app.TriggerGPUDeviceLost("id:csm-retained", nsgotest.GPULostContextLost, "context lost")
	if firstCSM.Valid() {
		t.Fatal("csm still valid while device is lost")
	}

	app.RestoreGPUDevice("id:csm-retained")
	if csm != firstCSM || !firstCSM.Valid() {
		t.Fatalf("retained csm not restored: same=%v valid=%v", csm == firstCSM, firstCSM.Valid())
	}

	app.Advance(16 * time.Millisecond)
	after, err := app.GPUReadPixels("id:csm-retained")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	if diff := csmTestDiffPixels(beforeCopy, after); diff != 0 {
		t.Errorf("frame after device restore differed by %d pixels, want 0", diff)
	}

	// Test Close idempotency and nil receiver safety.
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	closeable, err := render3d.NewCascadedShadowMap(nil, sr.Device(), render3d.CSMConfig{CascadeCount: 2, Resolution: 32})
	if err != nil {
		t.Fatalf("NewCascadedShadowMap: %v", err)
	}
	if err := closeable.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closeable.Valid() {
		t.Fatal("csm still valid after Close")
	}
	if err := closeable.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if closeable.BeginCascadePass(sr.Device().BeginEncoder(), 0) != nil {
		t.Error("BeginCascadePass on closed csm returned non-nil")
	}

	var nilCSM *render3d.CascadedShadowMap
	if nilCSM.Valid() || nilCSM.CascadeCount() != 0 || nilCSM.AtlasWidth() != 0 || nilCSM.AtlasHeight() != 0 ||
		nilCSM.DepthTexture() != nil || nilCSM.Sampler() != nil || nilCSM.UniformBuffer() != nil ||
		nilCSM.Layout() != nil || nilCSM.BindGroup() != nil || nilCSM.DepthPipeline() != nil ||
		nilCSM.CascadeBindGroup(0) != nil || nilCSM.CasterModelLayout() != nil || nilCSM.ByteSize() != 0 ||
		nilCSM.Close() != nil {
		t.Error("nil CascadedShadowMap accessors violated zero-value safety")
	}
}

func csmTestAbsInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func csmTestCloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func csmTestDiffPixels(a, b *image.RGBA) int {
	n := 0
	bounds := a.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pa, pb := a.RGBAAt(x, y), b.RGBAAt(x, y)
			if csmTestAbsInt(int(pa.R)-int(pb.R)) > 2 ||
				csmTestAbsInt(int(pa.G)-int(pb.G)) > 2 ||
				csmTestAbsInt(int(pa.B)-int(pb.B)) > 2 {
				n++
			}
		}
	}
	return n
}
