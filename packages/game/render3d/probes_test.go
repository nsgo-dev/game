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

func probeTestSphereMesh(radius float32, slices, stacks int) render3d.Mesh {
	var m render3d.Mesh
	for iy := 0; iy <= stacks; iy++ {
		v := float32(iy) / float32(stacks)
		phi := float64(v) * math.Pi
		sinPhi := float32(math.Sin(phi))
		cosPhi := float32(math.Cos(phi))
		for ix := 0; ix <= slices; ix++ {
			u := float32(ix) / float32(slices)
			theta := float64(u) * 2 * math.Pi
			nx := sinPhi * float32(math.Cos(theta))
			ny := cosPhi
			nz := sinPhi * float32(math.Sin(theta))
			n := math3d.Vec3{X: nx, Y: ny, Z: nz}.Norm()
			m.Positions = append(m.Positions, n.Scale(radius))
			m.Normals = append(m.Normals, n)
			m.UVs = append(m.UVs, math3d.Vec2{X: u, Y: v})
		}
	}
	stride := uint32(slices + 1)
	for iy := 0; iy < stacks; iy++ {
		for ix := 0; ix < slices; ix++ {
			a := uint32(iy)*stride + uint32(ix)
			b := a + 1
			c := a + stride
			d := c + 1
			m.Indices = append(m.Indices, a, b, d, a, d, c)
		}
	}
	return m
}

func probeTestQuadMesh(half float32) render3d.Mesh {
	n := math3d.Vec3{Z: 1}
	return render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: -half, Y: -half, Z: 0},
			{X: half, Y: -half, Z: 0},
			{X: half, Y: half, Z: 0},
			{X: -half, Y: half, Z: 0},
		},
		Normals: []math3d.Vec3{n, n, n, n},
		UVs: []math3d.Vec2{
			{X: 0, Y: 1},
			{X: 1, Y: 1},
			{X: 1, Y: 0},
			{X: 0, Y: 0},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

func probeTestReadCenterRGBA(t *testing.T, sr *gpu.SoftwareRenderer) [4]uint8 {
	t.Helper()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer error: %v", err)
	}
	img := sr.Image()
	b := img.Bounds()
	cx := (b.Min.X + b.Max.X) / 2
	cy := (b.Min.Y + b.Max.Y) / 2
	c := img.RGBAAt(cx, cy)
	return [4]uint8{c.R, c.G, c.B, c.A}
}

func probeTestUniformColor(c [3]float32) [6][3]float32 {
	return [6][3]float32{c, c, c, c, c, c}
}

func probeTestApprox3(a, b [3]float32, eps float32) bool {
	return math3d.ApproxEqual(a[0], b[0], eps) &&
		math3d.ApproxEqual(a[1], b[1], eps) &&
		math3d.ApproxEqual(a[2], b[2], eps)
}

func TestBoxProjectDirection(t *testing.T) {
	boxMin := math3d.Vec3{X: -5, Y: -5, Z: -5}
	boxMax := math3d.Vec3{X: 5, Y: 5, Z: 5}
	probePos := math3d.Vec3Zero()

	// At probe center, parallax-corrected direction equals input direction.
	inDir := math3d.Vec3{X: 1, Y: 0.4, Z: -0.3}.Norm()
	gotCenter := render3d.BoxProjectDirection(probePos, inDir, probePos, boxMin, boxMax)
	if !gotCenter.ApproxEqual(inDir, 1e-4) {
		t.Fatalf("at probe center: got %v, want %v", gotCenter, inDir)
	}

	// Off-center near +Z wall reflecting toward +X hits (+5, 0, +4).
	worldPos := math3d.Vec3{X: 0, Y: 0, Z: 4}
	refl := math3d.Vec3{X: 1, Y: 0, Z: 0}
	gotOff := render3d.BoxProjectDirection(worldPos, refl, probePos, boxMin, boxMax)
	wantOff := math3d.Vec3{X: 5, Y: 0, Z: 4}.Norm()
	if !gotOff.ApproxEqual(wantOff, 1e-4) {
		t.Fatalf("off-center +X reflection: got %v, want %v", gotOff, wantOff)
	}

	// Sitting directly on the +Z box face and reflecting parallel to it (+X) must not collapse.
	onWall := math3d.Vec3{X: 0, Y: 0, Z: 5}
	gotOnWall := render3d.BoxProjectDirection(onWall, refl, probePos, boxMin, boxMax)
	wantOnWall := math3d.Vec3{X: 5, Y: 0, Z: 5}.Norm()
	if !gotOnWall.ApproxEqual(wantOnWall, 1e-4) {
		t.Fatalf("on-wall parallel reflection: got %v, want %v", gotOnWall, wantOnWall)
	}

	// Non-zero probe position offset inside the box shifts the sample vector accordingly.
	shiftedProbe := math3d.Vec3{X: 2, Y: 0, Z: 0}
	gotShifted := render3d.BoxProjectDirection(worldPos, refl, shiftedProbe, boxMin, boxMax)
	wantShifted := math3d.Vec3{X: 5 - 2, Y: 0, Z: 4}.Norm()
	if !gotShifted.ApproxEqual(wantShifted, 1e-4) {
		t.Fatalf("shifted probe: got %v, want %v", gotShifted, wantShifted)
	}

	// Outside the box pointing away falls back cleanly to the unprojected reflection ray.
	outsidePos := math3d.Vec3{X: 10, Y: 0, Z: 0}
	gotOutside := render3d.BoxProjectDirection(outsidePos, refl, probePos, boxMin, boxMax)
	if !gotOutside.ApproxEqual(refl, 1e-4) {
		t.Fatalf("outside box pointing away: got %v, want %v", gotOutside, refl)
	}
}

func TestReflectionProbeSelectionAndBlending(t *testing.T) {
	probeA := &render3d.ReflectionProbe{
		ID:            "warm-room",
		Position:      math3d.Vec3{X: -3, Y: 0, Z: 0},
		BoxMin:        math3d.Vec3{X: -6, Y: -3, Z: -3},
		BoxMax:        math3d.Vec3{X: 1, Y: 3, Z: 3},
		BlendDistance: 2,
		Priority:      1,
		BoxProjection: true,
	}
	probeB := &render3d.ReflectionProbe{
		ID:            "cool-room",
		Position:      math3d.Vec3{X: 3, Y: 0, Z: 0},
		BoxMin:        math3d.Vec3{X: -1, Y: -3, Z: -3},
		BoxMax:        math3d.Vec3{X: 6, Y: 3, Z: 3},
		BlendDistance: 2,
		Priority:      1,
		BoxProjection: true,
	}
	var set render3d.ReflectionProbeSet
	set.Add(probeA, probeB)

	// Deep inside A: 100% A, 0% sky.
	rA := set.SelectReflectionProbes(math3d.Vec3{X: -3, Y: 0, Z: 0})
	if rA.Primary != probeA || !math3d.ApproxEqual(rA.Weight0, 1, 1e-5) || rA.Weight1 != 0 || rA.SkyWeight != 0 {
		t.Fatalf("deep inside A: got %+v", rA)
	}

	// Dead center of overlap (x=0): both at distance 1 inside BlendDistance 2 -> 50/50 A & B, 0% sky.
	rMid := set.SelectReflectionProbes(math3d.Vec3{X: 0, Y: 0, Z: 0})
	if rMid.Primary == nil || rMid.Secondary == nil {
		t.Fatalf("overlap midpoint must select two probes, got %+v", rMid)
	}
	if !math3d.ApproxEqual(rMid.Weight0, 0.5, 1e-5) || !math3d.ApproxEqual(rMid.Weight1, 0.5, 1e-5) || !math3d.ApproxEqual(rMid.SkyWeight, 0, 1e-5) {
		t.Fatalf("overlap midpoint weights: got w0=%g w1=%g sky=%g", rMid.Weight0, rMid.Weight1, rMid.SkyWeight)
	}

	// In outer fade zone of A (x=-5.5, distance 0.5 inside BlendDistance 2): blends A over sky.
	rEdge := set.SelectReflectionProbes(math3d.Vec3{X: -5.5, Y: 0, Z: 0})
	if rEdge.Primary != probeA || rEdge.Weight0 <= 0 || rEdge.Weight0 >= 1 || !math3d.ApproxEqual(rEdge.Weight0+rEdge.SkyWeight, 1, 1e-5) {
		t.Fatalf("outer fade of A: got %+v", rEdge)
	}

	// Outside both boxes: 100% sky.
	rSky := set.SelectReflectionProbes(math3d.Vec3{X: 12, Y: 0, Z: 0})
	if rSky.Primary != nil || rSky.Weight0 != 0 || rSky.Weight1 != 0 || !math3d.ApproxEqual(rSky.SkyWeight, 1, 1e-5) {
		t.Fatalf("outside all probes: got %+v", rSky)
	}

	// Sweep continuously from x=-8 to x=+8: weights must always be in [0,1] and sum to 1.
	for i := 0; i <= 80; i++ {
		x := -8 + float32(i)*0.2
		res := set.SelectReflectionProbes(math3d.Vec3{X: x})
		sum := res.Weight0 + res.Weight1 + res.SkyWeight
		if !math3d.ApproxEqual(sum, 1, 1e-5) || res.Weight0 < 0 || res.Weight1 < 0 || res.SkyWeight < 0 {
			t.Fatalf("x=%g: invalid weights w0=%g w1=%g sky=%g (sum=%g)", x, res.Weight0, res.Weight1, res.SkyWeight, sum)
		}
	}

	// Nested priority probes: high-priority inner room overrides low-priority courtyard, then fades into courtyard.
	courtyard := &render3d.ReflectionProbe{
		ID:            "courtyard",
		BoxMin:        math3d.Vec3{X: -20, Y: -10, Z: -20},
		BoxMax:        math3d.Vec3{X: 20, Y: 10, Z: 20},
		BlendDistance: 4,
		Priority:      1,
	}
	alcove := &render3d.ReflectionProbe{
		ID:            "alcove",
		BoxMin:        math3d.Vec3{X: -3, Y: -3, Z: -3},
		BoxMax:        math3d.Vec3{X: 3, Y: 3, Z: 3},
		BlendDistance: 2,
		Priority:      10,
	}
	var nested render3d.ReflectionProbeSet
	nested.Add(courtyard, alcove)

	rInner := nested.SelectReflectionProbes(math3d.Vec3Zero())
	if rInner.Primary != alcove || !math3d.ApproxEqual(rInner.Weight0, 1, 1e-5) || !math3d.ApproxEqual(rInner.Weight1, 0, 1e-5) {
		t.Fatalf("nested inner core: got %+v", rInner)
	}
	rFade := nested.SelectReflectionProbes(math3d.Vec3{X: 2, Y: 0, Z: 0}) // dist=1 inside BlendDistance=2 -> inf=0.5
	if rFade.Primary != alcove || rFade.Secondary != courtyard || !math3d.ApproxEqual(rFade.Weight0, 0.5, 1e-5) || !math3d.ApproxEqual(rFade.Weight1, 0.5, 1e-5) || rFade.SkyWeight != 0 {
		t.Fatalf("nested inner fade: got %+v", rFade)
	}
}

func TestSH9BakeAndEvaluate(t *testing.T) {
	// 1. Uniform constant environment must reproduce the exact color in every normal direction
	//    thanks to exact analytical cubemap texel solid-angle integration.
	constColor := [3]float32{0.8, 0.5, 0.25}
	shConst := render3d.BakeSH9FromFunc(8, func(math3d.Vec3) [3]float32 { return constColor })
	dirs := []math3d.Vec3{
		{X: 1}, {X: -1}, {Y: 1}, {Y: -1}, {Z: 1}, {Z: -1},
		math3d.Vec3{X: 1, Y: 2, Z: -3}.Norm(),
	}
	for _, n := range dirs {
		got := render3d.EvaluateSH9(shConst, n)
		if !probeTestApprox3(got, constColor, 1e-3) {
			t.Fatalf("uniform SH9 at normal %v: got %v, want %v", n, got, constColor)
		}
	}

	// 2. Two-hemisphere room: +X is red (1,0,0), -X is blue (0,0,1).
	//    Exact cosine-convolved diffuse response at +X is (1,0,0), at -X is (0,0,1), at +Y is (0.5,0,0.5).
	shSplit := render3d.BakeSH9FromFunc(16, func(dir math3d.Vec3) [3]float32 {
		if dir.X >= 0 {
			return [3]float32{1, 0, 0}
		}
		return [3]float32{0, 0, 1}
	})
	posX := render3d.EvaluateSH9(shSplit, math3d.Vec3{X: 1})
	negX := render3d.EvaluateSH9(shSplit, math3d.Vec3{X: -1})
	posY := render3d.EvaluateSH9(shSplit, math3d.Vec3{Y: 1})
	if !probeTestApprox3(posX, [3]float32{1, 0, 0}, 0.03) {
		t.Fatalf("split SH9 at +X: got %v, want ~(1,0,0)", posX)
	}
	if !probeTestApprox3(negX, [3]float32{0, 0, 1}, 0.03) {
		t.Fatalf("split SH9 at -X: got %v, want ~(0,0,1)", negX)
	}
	if !probeTestApprox3(posY, [3]float32{0.5, 0, 0.5}, 0.03) {
		t.Fatalf("split SH9 at +Y: got %v, want ~(0.5,0,0.5)", posY)
	}

	// 3. ProjectDirectionSH9 + Add/Scale/Lerp sanity.
	dirSH := render3d.ProjectDirectionSH9(math3d.Vec3{Y: 1}, [3]float32{1, 1, 1})
	top := dirSH.EvaluateDiffuse(math3d.Vec3{Y: 1})
	bot := dirSH.EvaluateDiffuse(math3d.Vec3{Y: -1})
	if top[0] <= bot[0]+0.2 {
		t.Fatalf("directional SH9 must brighten +Y over -Y: top=%v bot=%v", top, bot)
	}
	irr := shConst.EvaluateIrradiance(math3d.Vec3{Y: 1})
	if !math3d.ApproxEqual(irr[0], constColor[0]*math.Pi, 1e-2) {
		t.Fatalf("EvaluateIrradiance = %v, want %g", irr[0], constColor[0]*math.Pi)
	}
}

func TestLightProbeGridAndVolumeInterpolation(t *testing.T) {
	grid, err := render3d.NewLightProbeGrid(math3d.AABB{
		Min: math3d.Vec3{X: -4, Y: 0, Z: -4},
		Max: math3d.Vec3{X: 4, Y: 4, Z: 4},
	}, 2, 2, 2)
	if err != nil {
		t.Fatalf("NewLightProbeGrid: %v", err)
	}

	// Bake spatial color gradient: red on -X side, blue on +X side.
	grid.Bake(4, func(pos, _ math3d.Vec3) [3]float32 {
		tX := (pos.X + 4) / 8 // 0 at x=-4, 1 at x=+4
		return [3]float32{1 - tX, 0, tX}
	})

	left := grid.Sample(math3d.Vec3{X: -4, Y: 2, Z: 0}).EvaluateDiffuse(math3d.Vec3{Y: 1})
	mid := grid.Sample(math3d.Vec3{X: 0, Y: 2, Z: 0}).EvaluateDiffuse(math3d.Vec3{Y: 1})
	right := grid.Sample(math3d.Vec3{X: 4, Y: 2, Z: 0}).EvaluateDiffuse(math3d.Vec3{Y: 1})
	if !probeTestApprox3(left, [3]float32{1, 0, 0}, 0.02) {
		t.Fatalf("grid left = %v, want ~(1,0,0)", left)
	}
	if !probeTestApprox3(mid, [3]float32{0.5, 0, 0.5}, 0.02) {
		t.Fatalf("grid mid = %v, want ~(0.5,0,0.5)", mid)
	}
	if !probeTestApprox3(right, [3]float32{0, 0, 1}, 0.02) {
		t.Fatalf("grid right = %v, want ~(0,0,1)", right)
	}

	// Combine with a localized scattered green probe inside the grid.
	greenSH := render3d.BakeSH9FromFunc(4, func(math3d.Vec3) [3]float32 { return [3]float32{0, 1, 0} })
	vol := &render3d.LightProbeVolume{
		Grid: grid,
		Probes: []render3d.LightProbe{{
			Position: math3d.Vec3{X: 0, Y: 2, Z: 0},
			SH:       greenSH,
			Radius:   2,
		}},
	}
	atGreen := vol.SampleLightProbes(math3d.Vec3{X: 0, Y: 2, Z: 0}).EvaluateDiffuse(math3d.Vec3{Y: 1})
	if !probeTestApprox3(atGreen, [3]float32{0, 1, 0}, 0.02) {
		t.Fatalf("at scattered probe = %v, want ~(0,1,0)", atGreen)
	}
	farFromGreen := vol.SampleLightProbes(math3d.Vec3{X: -4, Y: 2, Z: 0}).EvaluateDiffuse(math3d.Vec3{Y: 1})
	if !probeTestApprox3(farFromGreen, [3]float32{1, 0, 0}, 0.02) {
		t.Fatalf("outside scattered radius should fall back to grid: got %v", farFromGreen)
	}
}

func TestGPUReflectionProbeBlendingOverSky(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(64, 64, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()

	pr, err := render3d.NewProbeRenderer(nil, dev, render3d.ProbeRendererOptions{
		CameraLayout: camBuf.Layout(),
		ColorFormat:  dev.SurfaceFormat(),
		DepthFormat:  gpu.FormatDepth24Plus,
		CullMode:     gpu.CullNone,
	})
	if err != nil {
		t.Fatalf("NewProbeRenderer: %v", err)
	}
	defer func() { _ = pr.Close() }()

	quad, err := render3d.UploadMesh(nil, dev, probeTestQuadMesh(1.2), render3d.MeshUpload{Label: "mirror"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	defer func() { _ = quad.Close() }()

	// Build two overlapping probes (warm red room A, cool blue room B) over a green sky cubemap.
	warmCube, err := render3d.NewSolidProbeCubemap(nil, dev, probeTestUniformColor([3]float32{1, 0, 0}), 4, render3d.ProbeCubemapUpload{Label: "warm"})
	if err != nil {
		t.Fatalf("warm cubemap: %v", err)
	}
	defer func() { _ = warmCube.Close() }()

	coolCube, err := render3d.NewSolidProbeCubemap(nil, dev, probeTestUniformColor([3]float32{0, 0, 1}), 4, render3d.ProbeCubemapUpload{Label: "cool"})
	if err != nil {
		t.Fatalf("cool cubemap: %v", err)
	}
	defer func() { _ = coolCube.Close() }()

	skyCube, err := render3d.NewSolidProbeCubemap(nil, dev, probeTestUniformColor([3]float32{0, 1, 0}), 4, render3d.ProbeCubemapUpload{Label: "sky"})
	if err != nil {
		t.Fatalf("sky cubemap: %v", err)
	}
	defer func() { _ = skyCube.Close() }()

	probeA := &render3d.ReflectionProbe{
		ID:            "A",
		Position:      math3d.Vec3{X: -3},
		BoxMin:        math3d.Vec3{X: -6, Y: -3, Z: -3},
		BoxMax:        math3d.Vec3{X: 1, Y: 3, Z: 3},
		BlendDistance: 2,
		Priority:      1,
		BoxProjection: true,
		Cubemap:       warmCube,
		Baked:         true,
	}
	probeB := &render3d.ReflectionProbe{
		ID:            "B",
		Position:      math3d.Vec3{X: 3},
		BoxMin:        math3d.Vec3{X: -1, Y: -3, Z: -3},
		BoxMax:        math3d.Vec3{X: 6, Y: 3, Z: 3},
		BlendDistance: 2,
		Priority:      1,
		BoxProjection: true,
		Cubemap:       coolCube,
		Baked:         true,
	}
	var probeSet render3d.ReflectionProbeSet
	probeSet.Add(probeA, probeB)

	binding, err := pr.NewBinding(nil)
	if err != nil {
		t.Fatalf("NewBinding: %v", err)
	}
	defer func() { _ = binding.Close() }()

	renderAt := func(pos math3d.Vec3) ([4]uint8, [3]float32) {
		cam := scene3d.NewPerspectiveCamera(pos.Add(math3d.Vec3{Z: 3}), math3d.DegToRad(60), 1, 0.1, 100)
		cam.LookAt(pos, math3d.Vec3Up())
		camBuf.WriteCamera(cam)

		blend := probeSet.SelectReflectionProbes(pos)
		u := render3d.ProbeUniform{
			Model: math3d.Translate(pos),
			Material: render3d.ProbeMaterial{
				Albedo:    [3]float32{1, 1, 1},
				Roughness: 0,
				Metallic:  1, // pure mirror reflection
				F0:        1,
			},
			Blend: blend,
		}
		if err := binding.Update(u, skyCube); err != nil {
			t.Fatalf("binding.Update: %v", err)
		}
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: core.RGB(0x000000),
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreDiscard,
				ClearDepth:   1,
			},
		})
		pr.Draw(pass, camBuf, binding, quad)
		pass.End()
		dev.Submit(enc.Finish())

		cpuExpected := blend.EvaluateCPU(pos, math3d.Vec3{Z: 1}, skyCube, [3]float32{1, 1, 1})
		return probeTestReadCenterRGBA(t, sr), cpuExpected
	}

	// 1. Inside probe A -> warm red.
	pxA, cpuA := renderAt(math3d.Vec3{X: -3})
	if pxA[0] < 240 || pxA[1] > 10 || pxA[2] > 10 {
		t.Fatalf("inside probe A: got RGBA %v, want pure red (cpu=%v)", pxA, cpuA)
	}

	// 2. Overlap center (x=0) -> 50/50 purple blend of A and B.
	pxMid, cpuMid := renderAt(math3d.Vec3{X: 0})
	if pxMid[0] < 115 || pxMid[0] > 140 || pxMid[2] < 115 || pxMid[2] > 140 || pxMid[1] > 10 {
		t.Fatalf("overlap midpoint: got RGBA %v, want ~50/50 red+blue (cpu=%v)", pxMid, cpuMid)
	}

	// 3. Inside probe B -> cool blue.
	pxB, cpuB := renderAt(math3d.Vec3{X: 3})
	if pxB[2] < 240 || pxB[0] > 10 || pxB[1] > 10 {
		t.Fatalf("inside probe B: got RGBA %v, want pure blue (cpu=%v)", pxB, cpuB)
	}

	// 4. Outer fade of probe B (x=5.0, dist=1 inside BlendDistance=2 -> 50% probe B + 50% green sky).
	pxFade, cpuFade := renderAt(math3d.Vec3{X: 5})
	if pxFade[1] < 115 || pxFade[1] > 140 || pxFade[2] < 115 || pxFade[2] > 140 || pxFade[0] > 10 {
		t.Fatalf("probe-to-sky fade: got RGBA %v, want ~50/50 green+blue (cpu=%v)", pxFade, cpuFade)
	}

	// 5. Outside all probes -> pure green sky environment.
	pxSky, cpuSky := renderAt(math3d.Vec3{X: 12})
	if pxSky[1] < 240 || pxSky[0] > 10 || pxSky[2] > 10 {
		t.Fatalf("outside all probes: got RGBA %v, want pure green sky (cpu=%v)", pxSky, cpuSky)
	}
}

func TestGPUBoxProjectionParallax(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(64, 64, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()

	pr, err := render3d.NewProbeRenderer(nil, dev, render3d.ProbeRendererOptions{
		CameraLayout: camBuf.Layout(),
		ColorFormat:  dev.SurfaceFormat(),
		DepthFormat:  gpu.FormatDepth24Plus,
		CullMode:     gpu.CullNone,
	})
	if err != nil {
		t.Fatalf("NewProbeRenderer: %v", err)
	}
	defer func() { _ = pr.Close() }()

	quad, err := render3d.UploadMesh(nil, dev, probeTestQuadMesh(0.8), render3d.MeshUpload{Label: "quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	defer func() { _ = quad.Close() }()

	// Bake a probe cubemap whose +Z face is split vertically:
	//   y > 0 (upper half) = bright yellow (1, 1, 0)
	//   y < 0 (lower half) = deep cyan (0, 0, 1)
	probe := &render3d.ReflectionProbe{
		ID:            "parallax-room",
		Position:      math3d.Vec3Zero(),
		BoxMin:        math3d.Vec3{X: -4, Y: -4, Z: -4},
		BoxMax:        math3d.Vec3{X: 4, Y: 4, Z: 4},
		BoxProjection: true,
	}
	err = render3d.BakeReflectionProbe(nil, dev, probe, 16, render3d.ProbeCubemapUpload{Label: "split-z"}, func(_, dir math3d.Vec3) [3]float32 {
		if dir.Y > 0.15 {
			return [3]float32{1, 1, 0} // upper half yellow
		}
		if dir.Y < -0.15 {
			return [3]float32{0, 0, 1} // lower half blue
		}
		return [3]float32{0.5, 0.5, 0.5}
	})
	if err != nil {
		t.Fatalf("BakeReflectionProbe: %v", err)
	}
	defer func() { _ = probe.Cubemap.Close() }()
	if !probe.Baked || probe.Dirty {
		t.Fatalf("expected Baked=true Dirty=false after BakeReflectionProbe")
	}

	binding, err := pr.NewBinding(nil)
	if err != nil {
		t.Fatalf("NewBinding: %v", err)
	}
	defer func() { _ = binding.Close() }()

	// Place a +Z-facing mirror near the top of the room (y=+3) and look straight at it from (0, 3, 5).
	// Unprojected reflection direction is purely (0, 0, 1) -> hits y=0 gray band!
	// Box-projected reflection hits the +Z wall at (0, +3, +4) -> direction from probe (0,0,0) has y=+3/5 = +0.6 -> hits yellow upper band!
	mirrorPos := math3d.Vec3{X: 0, Y: 3, Z: 0}
	cam := scene3d.NewPerspectiveCamera(mirrorPos.Add(math3d.Vec3{Z: 2.5}), math3d.DegToRad(50), 1, 0.1, 100)
	cam.LookAt(mirrorPos, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	renderWithBoxProj := func(boxProj bool) [4]uint8 {
		probe.BoxProjection = boxProj
		u := render3d.ProbeUniform{
			Model: math3d.Translate(mirrorPos),
			Material: render3d.ProbeMaterial{
				Albedo:    [3]float32{1, 1, 1},
				Roughness: 0,
				Metallic:  1,
				F0:        1,
			},
			Blend: render3d.ProbeBlendResult{Primary: probe, Weight0: 1},
		}
		if err := binding.Update(u, nil); err != nil {
			t.Fatalf("binding.Update: %v", err)
		}
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: core.RGB(0x000000),
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreDiscard,
				ClearDepth:   1,
			},
		})
		pr.Draw(pass, camBuf, binding, quad)
		pass.End()
		dev.Submit(enc.Finish())
		return probeTestReadCenterRGBA(t, sr)
	}

	pxUnproj := renderWithBoxProj(false)
	pxProj := renderWithBoxProj(true)

	// Unprojected hits the gray middle band (~128,128,128); box-projected shifts upward into yellow (~255,255,0).
	if pxUnproj[2] < 90 {
		t.Fatalf("unprojected reflection should hit gray mid band, got %v", pxUnproj)
	}
	if pxProj[0] < 230 || pxProj[1] < 230 || pxProj[2] > 25 {
		t.Fatalf("box-projected reflection should shift into upper yellow wall, got %v (unprojected=%v)", pxProj, pxUnproj)
	}
}

func TestGPUDynamicObjectPicksUpBakedLightProbes(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(96, 96, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()

	pr, err := render3d.NewProbeRenderer(nil, dev, render3d.ProbeRendererOptions{
		CameraLayout: camBuf.Layout(),
		ColorFormat:  dev.SurfaceFormat(),
		DepthFormat:  gpu.FormatDepth24Plus,
		CullMode:     gpu.CullBack,
	})
	if err != nil {
		t.Fatalf("NewProbeRenderer: %v", err)
	}
	defer func() { _ = pr.Close() }()

	sphere, err := render3d.UploadMesh(nil, dev, probeTestSphereMesh(1.0, 20, 14), render3d.MeshUpload{Label: "dynamic-sphere"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	defer func() { _ = sphere.Close() }()

	// Build a light probe volume spanning x in [-4, +4]:
	//   Left room (x=-4): warm directional light (bright red from +Z/+Y, faint dark elsewhere)
	//   Right room (x=+4): cool directional light (bright blue from +Z/+Y, faint dark elsewhere)
	grid, err := render3d.NewLightProbeGrid(math3d.AABB{
		Min: math3d.Vec3{X: -4, Y: -2, Z: -2},
		Max: math3d.Vec3{X: 4, Y: 2, Z: 2},
	}, 2, 1, 1)
	if err != nil {
		t.Fatalf("NewLightProbeGrid: %v", err)
	}
	grid.Bake(8, func(pos, dir math3d.Vec3) [3]float32 {
		// Also encode directional shading: front (+Z) bright, back (-Z) dark.
		front := float32(0.1)
		if dir.Z > 0 {
			front = 1.0
		}
		if pos.X < 0 {
			return [3]float32{front, 0.05 * front, 0} // warm room
		}
		return [3]float32{0, 0.05 * front, front} // cool room
	})
	vol := &render3d.LightProbeVolume{Grid: grid}

	binding, err := pr.NewBinding(nil)
	if err != nil {
		t.Fatalf("NewBinding: %v", err)
	}
	defer func() { _ = binding.Close() }()

	renderSphereAt := func(pos math3d.Vec3) ([4]uint8, [3]float32) {
		cam := scene3d.NewPerspectiveCamera(pos.Add(math3d.Vec3{Z: 3.5}), math3d.DegToRad(50), 1, 0.1, 100)
		cam.LookAt(pos, math3d.Vec3Up())
		camBuf.WriteCamera(cam)

		sh := vol.SampleLightProbes(pos)
		u := render3d.ProbeUniform{
			Model: math3d.Translate(pos),
			Material: render3d.ProbeMaterial{
				Albedo:    [3]float32{1, 1, 1},
				Roughness: 1,
				Metallic:  0,
				F0:        0, // pure diffuse to isolate SH9 light-probe contribution
			},
			LightSH: sh,
		}
		if err := binding.Update(u, nil); err != nil {
			t.Fatalf("binding.Update: %v", err)
		}
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{
				LoadOp:     gpu.LoadClear,
				StoreOp:    gpu.StoreStore,
				ClearColor: core.RGB(0x000000),
			}},
			DepthStencil: &gpu.DepthStencilAttachment{
				DepthLoadOp:  gpu.LoadClear,
				DepthStoreOp: gpu.StoreDiscard,
				ClearDepth:   1,
			},
		})
		pr.Draw(pass, camBuf, binding, sphere)
		pass.End()
		dev.Submit(enc.Finish())

		// Center pixel of the sphere has normal +Z facing the camera.
		return probeTestReadCenterRGBA(t, sr), sh.EvaluateDiffuse(math3d.Vec3{Z: 1})
	}

	pxLeft, expLeft := renderSphereAt(math3d.Vec3{X: -4})
	pxMid, expMid := renderSphereAt(math3d.Vec3{X: 0})
	pxRight, expRight := renderSphereAt(math3d.Vec3{X: 4})

	if pxLeft[0] < 200 || pxLeft[2] > 20 {
		t.Fatalf("dynamic sphere in warm room: got RGBA %v (expected ~%v)", pxLeft, expLeft)
	}
	if pxMid[0] < 95 || pxMid[0] > 160 || pxMid[2] < 95 || pxMid[2] > 160 {
		t.Fatalf("dynamic sphere in corridor midpoint: got RGBA %v (expected ~%v)", pxMid, expMid)
	}
	if pxRight[2] < 200 || pxRight[0] > 20 {
		t.Fatalf("dynamic sphere in cool room: got RGBA %v (expected ~%v)", pxRight, expRight)
	}
}

func TestBakeReflectionProbeGPUOnRequest(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()

	// Build a simple colored-wall room shader so BakeReflectionProbeGPU renders a
	// real 3D room with 6 distinct wall colors around the probe position.
	const roomWGSL = render3d.CameraWGSL + `
struct RoomVertIn {
	@location(0) pos: vec3<f32>,
	@location(1) normal: vec3<f32>,
	@location(2) uv: vec2<f32>,
}
struct RoomVertOut {
	@builtin(position) clip: vec4<f32>,
	@location(0) color: vec3<f32>,
}
@vertex
fn vs_main(in: RoomVertIn) -> RoomVertOut {
	var out: RoomVertOut;
	out.clip = camera.view_proj * vec4<f32>(in.pos, 1.0);
	// Encode wall color in vertex normal slot for this test fixture.
	out.color = in.normal;
	return out;
}
@fragment
fn fs_main(in: RoomVertOut) -> @location(0) vec4<f32> {
	return vec4<f32>(in.color, 1.0);
}
`
	mod, err := gpu.CompileWGSL(roomWGSL)
	if err != nil {
		t.Fatalf("CompileWGSL: %v", err)
	}
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()

	pipe, err := dev.CreatePipeline(nil, gpu.PipelineDesc{
		Label:         "room-bake",
		Vertex:        gpu.ShaderStage{Module: mod, EntryPoint: "vs_main"},
		Fragment:      gpu.ShaderStage{Module: mod, EntryPoint: "fs_main"},
		Layouts:       []*gpu.BindGroupLayout{render3d.CameraGroup: camBuf.Layout()},
		VertexBuffers: []gpu.VertexBufferLayout{render3d.MeshVertexLayout()},
		Primitive:     gpu.PrimitiveState{Topology: gpu.TopologyTriangleList, CullMode: gpu.CullNone},
		DepthStencil:  &gpu.DepthStencilState{Format: gpu.FormatDepth24Plus, DepthWrite: true, DepthCompare: gpu.CompareLess},
		Targets:       []gpu.ColorTargetState{{Format: gpu.FormatRGBA8Unorm}},
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	defer func() { _ = pipe.Close() }()

	// Build an enclosing cube centered at probePos with 6 distinct wall colors.
	probePos := math3d.Vec3{X: 2, Y: 1, Z: -3}
	wallColors := [6][3]float32{
		gpu.CubeFacePosX: {1, 0, 0}, // +X Red
		gpu.CubeFaceNegX: {0, 1, 0}, // -X Green
		gpu.CubeFacePosY: {0, 0, 1}, // +Y Blue
		gpu.CubeFaceNegY: {1, 1, 0}, // -Y Yellow
		gpu.CubeFacePosZ: {0, 1, 1}, // +Z Cyan
		gpu.CubeFaceNegZ: {1, 0, 1}, // -Z Magenta
	}
	type wall struct {
		dir, u, v math3d.Vec3
		color     [3]float32
	}
	walls := []wall{
		{math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3{Y: 1}, wallColors[gpu.CubeFacePosX]},
		{math3d.Vec3{X: -1}, math3d.Vec3{Z: 1}, math3d.Vec3{Y: 1}, wallColors[gpu.CubeFaceNegX]},
		{math3d.Vec3{Y: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, wallColors[gpu.CubeFacePosY]},
		{math3d.Vec3{Y: -1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: 1}, wallColors[gpu.CubeFaceNegY]},
		{math3d.Vec3{Z: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Y: 1}, wallColors[gpu.CubeFacePosZ]},
		{math3d.Vec3{Z: -1}, math3d.Vec3{X: -1}, math3d.Vec3{Y: 1}, wallColors[gpu.CubeFaceNegZ]},
	}
	var roomMesh render3d.Mesh
	const half = float32(4)
	for _, w := range walls {
		base := uint32(len(roomMesh.Positions))
		c := probePos.Add(w.dir.Scale(half))
		u, v := w.u.Scale(half), w.v.Scale(half)
		col := math3d.Vec3{X: w.color[0], Y: w.color[1], Z: w.color[2]}
		roomMesh.Positions = append(roomMesh.Positions,
			c.Sub(u).Sub(v), c.Add(u).Sub(v), c.Add(u).Add(v), c.Sub(u).Add(v),
		)
		roomMesh.Normals = append(roomMesh.Normals, col, col, col, col)
		roomMesh.UVs = append(roomMesh.UVs, math3d.Vec2{}, math3d.Vec2{}, math3d.Vec2{}, math3d.Vec2{})
		roomMesh.Indices = append(roomMesh.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	gpuRoom, err := render3d.UploadMesh(nil, dev, roomMesh, render3d.MeshUpload{Label: "room"})
	if err != nil {
		t.Fatalf("UploadMesh room: %v", err)
	}
	defer func() { _ = gpuRoom.Close() }()

	probe := &render3d.ReflectionProbe{
		ID:       "baked-room",
		Position: probePos,
		BoxMin:   probePos.Sub(math3d.Vec3{X: half, Y: half, Z: half}),
		BoxMax:   probePos.Add(math3d.Vec3{X: half, Y: half, Z: half}),
		Dirty:    true,
	}

	err = render3d.BakeReflectionProbeGPU(nil, dev, probe, 8, 0.1, 20, core.RGB(0x000000), render3d.ProbeCubemapUpload{
		Label:   "gpu-baked",
		Mipmaps: true,
	}, func(pass *gpu.RenderPass, _ gpu.CubeFace, cam render3d.CameraUniform) {
		camBuf.Write(cam)
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		gpuRoom.Draw(pass)
	})
	if err != nil {
		t.Fatalf("BakeReflectionProbeGPU: %v", err)
	}
	defer func() { _ = probe.Cubemap.Close() }()

	if !probe.Baked || probe.Dirty || !probe.Cubemap.Valid() {
		t.Fatalf("probe state after GPU bake: Baked=%v Dirty=%v Valid=%v", probe.Baked, probe.Dirty, probe.Cubemap.Valid())
	}
	if probe.Cubemap.MipLevels() != 4 { // 8 -> 4 -> 2 -> 1
		t.Fatalf("MipLevels = %d, want 4", probe.Cubemap.MipLevels())
	}

	// Verify all 6 cardinal directions match the 6 rendered room walls.
	cardinals := [6]math3d.Vec3{
		gpu.CubeFacePosX: {X: 1},
		gpu.CubeFaceNegX: {X: -1},
		gpu.CubeFacePosY: {Y: 1},
		gpu.CubeFaceNegY: {Y: -1},
		gpu.CubeFacePosZ: {Z: 1},
		gpu.CubeFaceNegZ: {Z: -1},
	}
	for f, dir := range cardinals {
		got := probe.Cubemap.SampleCPU(dir)
		if !probeTestApprox3(got, wallColors[f], 0.05) {
			t.Fatalf("GPU-baked face %d (dir %v): got %v, want %v", f, dir, got, wallColors[f])
		}
	}

	// Baking SH9 directly from the GPU-baked cubemap should reflect the directional wall colors.
	sh := probe.Cubemap.BakeSH9()
	diffPosX := sh.EvaluateDiffuse(math3d.Vec3{X: 1})
	diffNegX := sh.EvaluateDiffuse(math3d.Vec3{X: -1})
	if diffPosX[0] <= diffPosX[1] || diffNegX[1] <= diffNegX[0] {
		t.Fatalf("SH9 from GPU-baked cubemap did not preserve +X red / -X green: +X=%v -X=%v", diffPosX, diffNegX)
	}
}

func TestProbeRendererHeadlessNoComputeProfile(t *testing.T) {
	// Verify ProbeRenderer works on the restricted WebGL 2 / OpenGL ES 3.0 profile
	// (no compute shaders) through a headless gpu.View driven by nsgotest.
	var (
		camBuf  *render3d.CameraBuffer
		pr      *render3d.ProbeRenderer
		mesh    *render3d.GPUMesh
		binding *render3d.ProbeBinding
		skyCube *render3d.ProbeCubemap
		initErr error
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "probe-viewport",
			Style:       core.Style{Width: core.Pt(64), Height: core.Pt(64)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, dev)
				if err != nil {
					initErr = err
					return
				}
				pr, err = render3d.NewProbeRenderer(cx, dev, render3d.ProbeRendererOptions{
					CameraLayout: camBuf.Layout(),
					ColorFormat:  dev.SurfaceFormat(),
					DepthFormat:  gpu.FormatDepth24Plus,
					CullMode:     gpu.CullNone,
					Retained:     true,
				})
				if err != nil {
					initErr = err
					return
				}
				mesh, err = render3d.UploadMesh(cx, dev, probeTestQuadMesh(1.5), render3d.MeshUpload{Label: "quad", Retained: true})
				if err != nil {
					initErr = err
					return
				}
				skyCube, err = render3d.NewSolidProbeCubemap(cx, dev, probeTestUniformColor([3]float32{0.2, 0.8, 1.0}), 4, render3d.ProbeCubemapUpload{
					Label:    "sky",
					Mipmaps:  true,
					Retained: true,
				})
				if err != nil {
					initErr = err
					return
				}
				binding, err = pr.NewBinding(cx)
				if err != nil {
					initErr = err
					return
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				if initErr != nil || pr == nil || binding == nil {
					return
				}
				cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(60), 1, 0.1, 100)
				cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
				camBuf.WriteCamera(cam)
				_ = binding.Update(render3d.ProbeUniform{
					Model: math3d.Mat4Identity(),
					Material: render3d.ProbeMaterial{
						Albedo:    [3]float32{1, 1, 1},
						Roughness: 0.25,
						Metallic:  1,
						F0:        1,
					},
					Blend: render3d.ProbeBlendResult{SkyWeight: 1},
				}, skyCube)
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{
					ColorAttachments: []gpu.ColorAttachment{{
						LoadOp:     gpu.LoadClear,
						StoreOp:    gpu.StoreStore,
						ClearColor: core.RGB(0x000000),
					}},
					DepthStencil: &gpu.DepthStencilAttachment{
						DepthLoadOp:  gpu.LoadClear,
						DepthStoreOp: gpu.StoreDiscard,
						ClearDepth:   1,
					},
				})
				pr.Draw(pass, camBuf, binding, mesh)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(64, 64), nsgotest.ManualFrames(), nsgotest.NoGPUCompute())

	app.Advance(16 * time.Millisecond)
	if initErr != nil {
		t.Fatalf("headless gpu.View probe init failed: %v", initErr)
	}
	img, err := app.GPUReadPixels("id:probe-viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	if img == nil {
		t.Fatal("GPUReadPixels returned nil")
	}
	b := img.Bounds()
	c := img.RGBAAt((b.Min.X+b.Max.X)/2, (b.Min.Y+b.Max.Y)/2)
	if c.R < 35 || c.R > 70 || c.G < 185 || c.G > 220 || c.B < 235 {
		t.Fatalf("NoGPUCompute sky reflection pixel = %v, want ~(51, 204, 255)", c)
	}
}

func TestProbeValidationAndLifecycle(t *testing.T) {
	// Invalid ReflectionProbe cases.
	badProbes := []*render3d.ReflectionProbe{
		nil,
		{ID: "nan-pos", Position: math3d.Vec3{X: float32(math.NaN())}},
		{ID: "inverted-box", BoxMin: math3d.Vec3{X: 5}, BoxMax: math3d.Vec3{X: 1}},
		{ID: "neg-blend", BlendDistance: -1},
		{ID: "neg-intensity", Intensity: -0.5},
	}
	for _, p := range badProbes {
		if err := p.Validate(); !errors.Is(err, render3d.ErrInvalidProbe) {
			t.Fatalf("probe %+v: Validate = %v, want ErrInvalidProbe", p, err)
		}
	}

	// DirtyProbes tracking.
	p1 := &render3d.ReflectionProbe{ID: "unbaked", Baked: false}
	p2 := &render3d.ReflectionProbe{ID: "dirty", Baked: true, Dirty: true}
	p3 := &render3d.ReflectionProbe{ID: "clean", Baked: true, Dirty: false}
	p4 := &render3d.ReflectionProbe{ID: "disabled", Baked: false, Disabled: true}
	var set render3d.ReflectionProbeSet
	set.Add(p1, p2, p3, p4)
	dirty := set.DirtyProbes()
	if len(dirty) != 2 || dirty[0] != p1 || dirty[1] != p2 {
		t.Fatalf("DirtyProbes = %v, want [p1, p2]", dirty)
	}

	// Invalid LightProbeGrid cases.
	if _, err := render3d.NewLightProbeGrid(math3d.AABBEmpty(), 2, 2, 2); !errors.Is(err, render3d.ErrInvalidProbe) {
		t.Fatalf("empty grid bounds: got %v, want ErrInvalidProbe", err)
	}
	if _, err := render3d.NewLightProbeGrid(math3d.AABB{Min: math3d.Vec3Zero(), Max: math3d.Vec3One()}, 0, 2, 2); !errors.Is(err, render3d.ErrInvalidProbe) {
		t.Fatalf("zero grid count: got %v, want ErrInvalidProbe", err)
	}

	// Cubemap upload validation & UpdateFaceRGBA.
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()

	nonSquare := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	var badFaces [6]image.Image
	for i := range 6 {
		badFaces[i] = nonSquare
	}
	if _, err := render3d.UploadProbeCubemap(nil, dev, badFaces, render3d.ProbeCubemapUpload{}); !errors.Is(err, render3d.ErrInvalidProbe) {
		t.Fatalf("non-square cubemap: got %v, want ErrInvalidProbe", err)
	}

	solid := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			solid.SetNRGBA(x, y, color.NRGBA{R: 255, G: 128, B: 0, A: 255})
		}
	}
	var goodFaces [6]image.Image
	for i := range 6 {
		goodFaces[i] = solid
	}
	cm, err := render3d.UploadProbeCubemap(nil, dev, goodFaces, render3d.ProbeCubemapUpload{Mipmaps: true})
	if err != nil {
		t.Fatalf("UploadProbeCubemap: %v", err)
	}
	// 4x4 + 2x2 + 1x1 = 21 texels * 4 bytes * 6 faces = 504 bytes.
	if got := cm.ByteSize(); got != 504 {
		t.Fatalf("ByteSize = %d, want 504", got)
	}
	// Update +X face to pure cyan.
	cyan := make([]byte, 4*4*4)
	for i := 0; i < len(cyan); i += 4 {
		cyan[i+0] = 0
		cyan[i+1] = 255
		cyan[i+2] = 255
		cyan[i+3] = 255
	}
	if err := cm.UpdateFaceRGBA(dev, gpu.CubeFacePosX, cyan); err != nil {
		t.Fatalf("UpdateFaceRGBA: %v", err)
	}
	gotCyan := cm.SampleCPU(math3d.Vec3{X: 1})
	if !probeTestApprox3(gotCyan, [3]float32{0, 1, 1}, 0.02) {
		t.Fatalf("after UpdateFaceRGBA(+X): got %v, want (0,1,1)", gotCyan)
	}
	if err := cm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := cm.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if cm.Valid() {
		t.Fatal("closed cubemap reported Valid() == true")
	}
}

func TestProbeRendererMultiObjectAndRoughnessMip(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(80, 40, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()

	pr, err := render3d.NewProbeRenderer(nil, dev, render3d.ProbeRendererOptions{
		CameraLayout: camBuf.Layout(),
		ColorFormat:  dev.SurfaceFormat(),
		DepthFormat:  gpu.FormatDepth24Plus,
		CullMode:     gpu.CullNone,
	})
	if err != nil {
		t.Fatalf("NewProbeRenderer: %v", err)
	}
	defer func() { _ = pr.Close() }()

	quad, err := render3d.UploadMesh(nil, dev, probeTestQuadMesh(0.7), render3d.MeshUpload{Label: "quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	defer func() { _ = quad.Close() }()

	// Build a cubemap whose +Z face is a red/green checkerboard so mip 0 is crisp
	// and the coarsest mip averages to muted yellow (~0.5, 0.5, 0).
	checker := make([]float32, 4*4*3)
	for y := range 4 {
		for x := range 4 {
			i := (y*4 + x) * 3
			if (x+y)%2 == 0 {
				checker[i+0] = 1
			} else {
				checker[i+1] = 1
			}
		}
	}
	var faces [6][]float32
	for f := range 6 {
		faces[f] = checker
	}
	checkCube, err := render3d.UploadProbeCubemapLinear(nil, dev, 4, faces, render3d.ProbeCubemapUpload{
		Label:   "checker",
		Mipmaps: true,
	})
	if err != nil {
		t.Fatalf("UploadProbeCubemapLinear: %v", err)
	}
	defer func() { _ = checkCube.Close() }()

	warmCube, err := render3d.NewSolidProbeCubemap(nil, dev, probeTestUniformColor([3]float32{1, 0, 0}), 4, render3d.ProbeCubemapUpload{Label: "warm"})
	if err != nil {
		t.Fatalf("warmCube: %v", err)
	}
	defer func() { _ = warmCube.Close() }()

	pLeft := &render3d.ReflectionProbe{ID: "left", BoxMin: math3d.Vec3{X: -5, Y: -5, Z: -5}, BoxMax: math3d.Vec3{X: 0, Y: 5, Z: 5}, Cubemap: warmCube, Baked: true}
	pRight := &render3d.ReflectionProbe{ID: "right", BoxMin: math3d.Vec3{X: 0, Y: -5, Z: -5}, BoxMax: math3d.Vec3{X: 5, Y: 5, Z: 5}, Cubemap: checkCube, Baked: true}

	bLeft, err := pr.NewBinding(nil)
	if err != nil {
		t.Fatalf("NewBinding left: %v", err)
	}
	defer func() { _ = bLeft.Close() }()
	bRight, err := pr.NewBinding(nil)
	if err != nil {
		t.Fatalf("NewBinding right: %v", err)
	}
	defer func() { _ = bRight.Close() }()

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 4}, math3d.DegToRad(50), 2, 0.1, 100)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	if err := bLeft.Update(render3d.ProbeUniform{
		Model:    math3d.Translate(math3d.Vec3{X: -1.4}),
		Material: render3d.ProbeMaterial{Albedo: [3]float32{1, 1, 1}, Roughness: 0, Metallic: 1, F0: 1},
		Blend:    render3d.ProbeBlendResult{Primary: pLeft, Weight0: 1},
	}, nil); err != nil {
		t.Fatalf("bLeft.Update: %v", err)
	}
	if err := bRight.Update(render3d.ProbeUniform{
		Model:    math3d.Translate(math3d.Vec3{X: 1.4}),
		Material: render3d.ProbeMaterial{Albedo: [3]float32{1, 1, 1}, Roughness: 1, Metallic: 1, F0: 1},
		Blend:    render3d.ProbeBlendResult{Primary: pRight, Weight0: 1},
	}, nil); err != nil {
		t.Fatalf("bRight.Update: %v", err)
	}

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: core.RGB(0x000000),
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1,
		},
	})
	pr.Draw(pass, camBuf, bLeft, quad)
	pr.Draw(pass, camBuf, bRight, quad)
	pass.End()
	dev.Submit(enc.Finish())

	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer error: %v", err)
	}
	img := sr.Image()
	leftPx := img.RGBAAt(20, 20)
	rightPx := img.RGBAAt(60, 20)
	// Left object samples solid red warmCube.
	if leftPx.R < 240 || leftPx.G > 10 || leftPx.B > 10 {
		t.Fatalf("left object pixel = %v, want pure red", leftPx)
	}
	// Right object has roughness=1 -> samples coarsest mip of red/green checker -> muted yellow (~128, 128, 0).
	if rightPx.R < 105 || rightPx.R > 150 || rightPx.G < 105 || rightPx.G > 150 || rightPx.B > 15 {
		t.Fatalf("right rough object pixel = %v, want mip-averaged yellow (~128, 128, 0)", rightPx)
	}
}
