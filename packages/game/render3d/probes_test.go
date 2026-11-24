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

func probeSolidFace(size int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func probeSolidCubemap(size int, c color.RGBA, srgb bool) *render3d.Cubemap {
	cm := &render3d.Cubemap{SRGB: srgb}
	for f := range render3d.ProbeCubeFaceCount {
		cm.Faces[f] = probeSolidFace(size, c)
	}
	return cm
}

func probeCardinalCubemap(size int) *render3d.Cubemap {
	return &render3d.Cubemap{
		Faces: [6]*image.RGBA{
			gpu.CubeFacePosX: probeSolidFace(size, color.RGBA{R: 230, G: 30, B: 30, A: 255}),  // +X Red
			gpu.CubeFaceNegX: probeSolidFace(size, color.RGBA{R: 30, G: 230, B: 30, A: 255}),  // -X Green
			gpu.CubeFacePosY: probeSolidFace(size, color.RGBA{R: 30, G: 60, B: 235, A: 255}),  // +Y Blue
			gpu.CubeFaceNegY: probeSolidFace(size, color.RGBA{R: 230, G: 220, B: 30, A: 255}), // -Y Yellow
			gpu.CubeFacePosZ: probeSolidFace(size, color.RGBA{R: 30, G: 220, B: 230, A: 255}), // +Z Cyan
			gpu.CubeFaceNegZ: probeSolidFace(size, color.RGBA{R: 225, G: 40, B: 225, A: 255}), // -Z Magenta
		},
	}
}

func probeQuadMesh(half float32) render3d.Mesh {
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

func probeAbsDiffU8(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

func probeSRGBByte(v float32) uint8 {
	c := math3d.Clamp(v, 0, 1)
	return uint8(math.Round(float64(c) * 255))
}

func TestProbeBoxProjectDirection(t *testing.T) {
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

	// Non-finite and degenerate inputs must never produce NaN.
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	for _, tc := range []struct {
		pos, dir, ppos, bmin, bmax math3d.Vec3
	}{
		{math3d.Vec3{X: nan}, refl, probePos, boxMin, boxMax},
		{worldPos, math3d.Vec3{Y: nan}, probePos, boxMin, boxMax},
		{worldPos, math3d.Vec3Zero(), probePos, boxMin, boxMax},
		{worldPos, refl, math3d.Vec3{Z: inf}, boxMin, boxMax},
		{worldPos, refl, probePos, math3d.Vec3{X: nan}, boxMax},
	} {
		out := render3d.BoxProjectDirection(tc.pos, tc.dir, tc.ppos, tc.bmin, tc.bmax)
		if math.IsNaN(float64(out.X)) || math.IsNaN(float64(out.Y)) || math.IsNaN(float64(out.Z)) {
			t.Fatalf("BoxProjectDirection returned NaN for %+v: %v", tc, out)
		}
	}
}

func TestProbeInfluenceValidationAndSelection(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	// Validation errors wrap ErrInvalidProbe.
	var nilProbe *render3d.ReflectionProbe
	if err := nilProbe.Validate(); !errors.Is(err, render3d.ErrInvalidProbe) {
		t.Fatalf("nil Validate = %v, want ErrInvalidProbe", err)
	}
	for name, p := range map[string]*render3d.ReflectionProbe{
		"NaN position":     {Position: math3d.Vec3{X: nan}, BoxMin: math3d.Vec3{X: -1, Y: -1, Z: -1}, BoxMax: math3d.Vec3{X: 1, Y: 1, Z: 1}},
		"inverted box":     {BoxMin: math3d.Vec3{X: 2}, BoxMax: math3d.Vec3{X: 1}},
		"negative blend":   {BoxMin: math3d.Vec3{X: -1, Y: -1, Z: -1}, BoxMax: math3d.Vec3{X: 1, Y: 1, Z: 1}, BlendDistance: -0.5},
		"NaN blend":        {BoxMin: math3d.Vec3{X: -1, Y: -1, Z: -1}, BoxMax: math3d.Vec3{X: 1, Y: 1, Z: 1}, BlendDistance: nan},
		"negative intense": {BoxMin: math3d.Vec3{X: -1, Y: -1, Z: -1}, BoxMax: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: -1},
		"inf intense":      {BoxMin: math3d.Vec3{X: -1, Y: -1, Z: -1}, BoxMax: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: inf},
	} {
		if err := p.Validate(); !errors.Is(err, render3d.ErrInvalidProbe) {
			t.Errorf("%s: Validate = %v, want ErrInvalidProbe", name, err)
		}
	}

	// Influence must refuse non-finite inputs and return 0 (never NaN).
	probe := &render3d.ReflectionProbe{
		ID:            "room",
		Position:      math3d.Vec3Zero(),
		BoxMin:        math3d.Vec3{X: -4, Y: -4, Z: -4},
		BoxMax:        math3d.Vec3{X: 4, Y: 4, Z: 4},
		BlendDistance: 2,
		Baked:         true,
	}
	for _, badPos := range []math3d.Vec3{
		{X: nan, Y: 0, Z: 0},
		{X: 0, Y: inf, Z: 0},
		{X: 0, Y: 0, Z: float32(math.Inf(-1))},
	} {
		got := probe.Influence(badPos)
		if got != 0 || math.IsNaN(float64(got)) {
			t.Fatalf("Influence(%v) = %v, want 0", badPos, got)
		}
	}
	badProbe := *probe
	badProbe.BlendDistance = nan
	if got := badProbe.Influence(math3d.Vec3Zero()); got != 0 || math.IsNaN(float64(got)) {
		t.Fatalf("Influence with NaN BlendDistance = %v, want 0", got)
	}
	badBoxProbe := *probe
	badBoxProbe.BoxMax.X = nan
	if got := badBoxProbe.Influence(math3d.Vec3Zero()); got != 0 || math.IsNaN(float64(got)) {
		t.Fatalf("Influence with NaN BoxMax = %v, want 0", got)
	}

	// Smoothstep fade inside BlendDistance = 2:
	// center (dist=4 >= 2) -> 1; halfway into blend (x=3, dist=1) -> smoothstep(0.5) = 0.5; outside (x=5) -> 0.
	if got := probe.Influence(math3d.Vec3Zero()); got != 1 {
		t.Fatalf("center Influence = %v, want 1", got)
	}
	if got := probe.Influence(math3d.Vec3{X: 3}); !math3d.ApproxEqual(got, 0.5, 1e-5) {
		t.Fatalf("mid-blend Influence = %v, want 0.5", got)
	}
	if got := probe.Influence(math3d.Vec3{X: 5}); got != 0 {
		t.Fatalf("outside Influence = %v, want 0", got)
	}

	// Selection skips unbaked, dirty, and disabled probes.
	readyA := &render3d.ReflectionProbe{
		ID: "A", BoxMin: math3d.Vec3{X: -6, Y: -4, Z: -4}, BoxMax: math3d.Vec3{X: 2, Y: 4, Z: 4},
		BlendDistance: 2, Baked: true,
	}
	readyB := &render3d.ReflectionProbe{
		ID: "B", BoxMin: math3d.Vec3{X: -2, Y: -4, Z: -4}, BoxMax: math3d.Vec3{X: 6, Y: 4, Z: 4},
		BlendDistance: 2, Baked: true,
	}
	unbaked := &render3d.ReflectionProbe{
		ID: "unbaked", BoxMin: math3d.Vec3{X: -10, Y: -10, Z: -10}, BoxMax: math3d.Vec3{X: 10, Y: 10, Z: 10},
		Priority: 10, Baked: false,
	}
	dirty := &render3d.ReflectionProbe{
		ID: "dirty", BoxMin: math3d.Vec3{X: -10, Y: -10, Z: -10}, BoxMax: math3d.Vec3{X: 10, Y: 10, Z: 10},
		Priority: 10, Baked: true, Dirty: true,
	}
	disabled := &render3d.ReflectionProbe{
		ID: "disabled", BoxMin: math3d.Vec3{X: -10, Y: -10, Z: -10}, BoxMax: math3d.Vec3{X: 10, Y: 10, Z: 10},
		Priority: 10, Baked: true, Disabled: true,
	}

	var set render3d.ReflectionProbeSet
	set.Add(readyA, readyB, unbaked, dirty, disabled)

	// DirtyProbes returns unbaked and dirty, skipping ready and disabled.
	dirtyList := set.DirtyProbes()
	if len(dirtyList) != 2 || dirtyList[0] != unbaked || dirtyList[1] != dirty {
		t.Fatalf("DirtyProbes = %v, want [unbaked, dirty]", dirtyList)
	}

	// At origin (symmetric overlap of readyA and readyB): both have influence 1 -> 50/50 split, sky 0.
	mid := set.SelectReflectionProbes(math3d.Vec3Zero())
	if mid.Primary != readyA || mid.Secondary != readyB ||
		!math3d.ApproxEqual(mid.Weight0, 0.5, 1e-5) ||
		!math3d.ApproxEqual(mid.Weight1, 0.5, 1e-5) ||
		mid.SkyWeight != 0 {
		t.Fatalf("overlap selection = %+v, want A/B 0.5/0.5/0", mid)
	}

	// Outside all ready probes -> sky 1.0 even though higher-priority unbaked/dirty/disabled cover it.
	out := set.SelectReflectionProbes(math3d.Vec3{X: 8})
	if out.Primary != nil || out.Secondary != nil || out.SkyWeight != 1 {
		t.Fatalf("outside selection = %+v, want sky=1 only", out)
	}

	// Non-finite query position -> sky 1.0 safely.
	nanRes := set.SelectReflectionProbes(math3d.Vec3{X: nan})
	if nanRes.Primary != nil || nanRes.SkyWeight != 1 {
		t.Fatalf("NaN selection = %+v, want sky=1", nanRes)
	}
}

func TestProbeLightGridAndVolumeSafetyAndInterpolation(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	// Invalid constructors wrap ErrInvalidProbe.
	if _, err := render3d.NewLightProbeGrid(math3d.AABB{Min: math3d.Vec3{X: 1}, Max: math3d.Vec3{X: 0}}, 2, 2, 2); !errors.Is(err, render3d.ErrInvalidProbe) {
		t.Fatalf("empty bounds err = %v, want ErrInvalidProbe", err)
	}
	if _, err := render3d.NewLightProbeGrid(math3d.AABB{Min: math3d.Vec3Zero(), Max: math3d.Vec3One()}, 0, 2, 2); !errors.Is(err, render3d.ErrInvalidProbe) {
		t.Fatalf("zero count err = %v, want ErrInvalidProbe", err)
	}

	// Mismatched struct literals must NEVER panic in At, Set, or Sample.
	mismatched := []*render3d.LightProbeGrid{
		nil,
		{},
		{Counts: [3]int{4, 4, 4}, Probes: make([]render3d.IrradianceSH, 2)},
		{Counts: [3]int{-2, 2, 2}, Probes: make([]render3d.IrradianceSH, 8)},
		{Counts: [3]int{1 << 25, 1 << 25, 1 << 25}, Probes: make([]render3d.IrradianceSH, 1)},
	}
	for i, g := range mismatched {
		if g != nil {
			if err := g.Validate(); !errors.Is(err, render3d.ErrInvalidProbe) {
				t.Errorf("mismatched[%d].Validate = %v, want ErrInvalidProbe", i, err)
			}
		}
		g.Set(0, 0, 0, render3d.IrradianceSH{})
		if got := g.At(3, 3, 3); got != (render3d.IrradianceSH{}) {
			t.Errorf("mismatched[%d].At = %v, want zero", i, got)
		}
		if got := g.Sample(math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}); got != (render3d.IrradianceSH{}) {
			t.Errorf("mismatched[%d].Sample = %v, want zero", i, got)
		}
	}

	// Bake a 2x1x1 grid from X=-2 (pure red radiance 1,0,0) to X=+2 (pure blue radiance 0,0,1).
	grid, err := render3d.NewLightProbeGrid(math3d.AABB{
		Min: math3d.Vec3{X: -2, Y: -1, Z: -1},
		Max: math3d.Vec3{X: 2, Y: 1, Z: 1},
	}, 2, 1, 1)
	if err != nil {
		t.Fatalf("NewLightProbeGrid: %v", err)
	}
	if err := grid.Bake(8, func(pos, _ math3d.Vec3) [3]float32 {
		if pos.X < 0 {
			return [3]float32{1, 0, 0}
		}
		return [3]float32{0, 0, 1}
	}); err != nil {
		t.Fatalf("grid.Bake: %v", err)
	}

	// Uniform radiance C yields Evaluate(n) = pi * C.
	n := math3d.Vec3{Y: 1}
	leftIrr := grid.Sample(math3d.Vec3{X: -2}).Evaluate(n)
	midIrr := grid.Sample(math3d.Vec3{X: 0}).Evaluate(n)
	rightIrr := grid.Sample(math3d.Vec3{X: 2}).Evaluate(n)
	const pi = float32(math.Pi)
	if !math3d.ApproxEqual(leftIrr[0], pi, 0.02) || leftIrr[2] > 0.02 {
		t.Fatalf("left grid sample = %v, want (~pi, 0, 0)", leftIrr)
	}
	if !math3d.ApproxEqual(midIrr[0], pi/2, 0.02) || !math3d.ApproxEqual(midIrr[2], pi/2, 0.02) {
		t.Fatalf("mid grid sample = %v, want (~pi/2, 0, ~pi/2)", midIrr)
	}
	if rightIrr[0] > 0.02 || !math3d.ApproxEqual(rightIrr[2], pi, 0.02) {
		t.Fatalf("right grid sample = %v, want (0, 0, ~pi)", rightIrr)
	}

	// Non-finite worldPos returns zero for Grid and FallbackSH for Volume.
	if got := grid.Sample(math3d.Vec3{X: nan}); got != (render3d.IrradianceSH{}) {
		t.Fatalf("grid.Sample(NaN) = %v, want zero", got)
	}
	greenSH, err := render3d.ComputeIrradianceSHFromFunc(4, func(math3d.Vec3) [3]float32 {
		return [3]float32{0, 1, 0}
	})
	if err != nil {
		t.Fatalf("ComputeIrradianceSHFromFunc: %v", err)
	}
	vol := &render3d.LightProbeVolume{
		Grid:       grid,
		FallbackSH: greenSH,
	}
	vol.Add(render3d.LightProbe{
		ID:       "accent",
		Position: math3d.Vec3{X: 0, Y: 0, Z: 0},
		Radius:   1.5,
		SH:       greenSH,
		Baked:    true,
	})
	if got := vol.SampleLightProbes(math3d.Vec3{X: inf}); got != greenSH {
		t.Fatalf("vol.SampleLightProbes(Inf) != FallbackSH")
	}
	// Right on the scattered green probe: picks up greenSH.
	atAccent := vol.SampleLightProbes(math3d.Vec3Zero()).Evaluate(n)
	if !math3d.ApproxEqual(atAccent[1], pi, 0.02) || atAccent[0] > 0.02 {
		t.Fatalf("at scattered probe = %v, want (~0, ~pi, 0)", atAccent)
	}
	// Far outside scattered radius: falls back to grid (right side = blue).
	farRight := vol.SampleLightProbes(math3d.Vec3{X: 2}).Evaluate(n)
	if !math3d.ApproxEqual(farRight[2], pi, 0.02) || farRight[1] > 0.02 {
		t.Fatalf("outside scattered probe = %v, want (~0, 0, ~pi)", farRight)
	}
}

func TestProbeRebakeClosesPreviousCubemap(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(32, 32, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	probe := &render3d.ReflectionProbe{
		ID:     "rebake-room",
		BoxMin: math3d.Vec3{X: -4, Y: -4, Z: -4},
		BoxMax: math3d.Vec3{X: 4, Y: 4, Z: 4},
	}

	opts := render3d.ProbeBakeOptions{
		Size:      8,
		Prefilter: render3d.PrefilterOptions{BaseSize: 8, MipLevels: 3, SampleCount: 16},
	}

	// First CPU bake: pure red.
	if err := render3d.BakeReflectionProbe(nil, dev, probe, opts, func(_, _ math3d.Vec3) [3]float32 {
		return [3]float32{0.9, 0.1, 0.1}
	}); err != nil {
		t.Fatalf("first BakeReflectionProbe: %v", err)
	}
	t.Cleanup(func() { _ = probe.Close() })

	firstGPU := probe.GPUCubemap
	if !firstGPU.Valid() || !probe.Baked || probe.Dirty {
		t.Fatalf("unexpected state after first bake: valid=%v baked=%v dirty=%v", firstGPU.Valid(), probe.Baked, probe.Dirty)
	}

	// Re-bake with pure blue: old GPUCubemap must be closed immediately!
	probe.Dirty = true
	if err := render3d.BakeReflectionProbe(nil, dev, probe, opts, func(_, _ math3d.Vec3) [3]float32 {
		return [3]float32{0.1, 0.2, 0.9}
	}); err != nil {
		t.Fatalf("second BakeReflectionProbe: %v", err)
	}
	if firstGPU.Valid() {
		t.Fatal("previous GPUCubemap remained valid after CPU re-bake (texture leak)")
	}
	secondGPU := probe.GPUCubemap
	if secondGPU == firstGPU || !secondGPU.Valid() || !probe.Baked || probe.Dirty {
		t.Fatalf("unexpected state after second bake: valid=%v baked=%v dirty=%v", secondGPU.Valid(), probe.Baked, probe.Dirty)
	}
	s := probe.Prefiltered.Sample(math3d.Vec3{Z: 1})
	if !(s[2] > 0.8 && s[0] < 0.2) {
		t.Fatalf("re-baked cubemap sample = %v, want blue", s)
	}

	// Re-bake via BakeReflectionProbeGPU (linear clear green): secondGPU must be closed!
	var gpuDone bool
	var gpuErr error
	render3d.BakeReflectionProbeGPU(nil, dev, probe, render3d.ProbeBakeOptions{
		Size:       8,
		ClearColor: core.RGB(0x10e020),
		Prefilter:  render3d.PrefilterOptions{BaseSize: 8, MipLevels: 3, SampleCount: 16},
	}, func(_ *gpu.RenderPass, _ gpu.CubeFace, _ render3d.CameraUniform) {
		// Clear color fills the face.
	}, func(err error) {
		gpuDone = true
		gpuErr = err
	})
	if !gpuDone || gpuErr != nil {
		t.Fatalf("BakeReflectionProbeGPU: done=%v err=%v", gpuDone, gpuErr)
	}
	if secondGPU.Valid() {
		t.Fatal("previous GPUCubemap remained valid after GPU re-bake (texture leak)")
	}
	if !probe.GPUCubemap.Valid() || !probe.Baked || probe.Dirty {
		t.Fatalf("unexpected state after GPU re-bake: valid=%v baked=%v dirty=%v", probe.GPUCubemap.Valid(), probe.Baked, probe.Dirty)
	}
	gSample := probe.Prefiltered.Sample(math3d.Vec3{X: 1})
	if !(gSample[1] > 0.7 && gSample[0] < 0.2) {
		t.Fatalf("GPU re-baked cubemap sample = %v, want green", gSample)
	}
}

func TestProbeIBLUnbakedDirtyAndNilSkyBlackParity(t *testing.T) {
	const w, h = 32, 32
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(50), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	lightBuf, err := render3d.NewLightBuffer(nil, dev, render3d.LightRig{}, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	mat := render3d.PBRMaterial{
		BaseColor: math3d.Vec3One(),
		Metallic:  1,
		Roughness: 0.1,
	}
	drawBuf, err := render3d.NewPBRDrawBuffer(nil, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), mat), false)
	if err != nil {
		t.Fatalf("NewPBRDrawBuffer: %v", err)
	}
	t.Cleanup(func() { _ = drawBuf.Close() })

	probeIBL, err := render3d.NewGPUProbeIBLBuffer(nil, dev, render3d.ProbeIBLBufferOptions{Label: "parity"})
	if err != nil {
		t.Fatalf("NewGPUProbeIBLBuffer: %v", err)
	}
	t.Cleanup(func() { _ = probeIBL.Close() })

	pipe, err := render3d.NewLitPBRProbeIBLPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), probeIBL.Layout(), gpu.FormatDepth24Plus)
	if err != nil {
		t.Fatalf("NewLitPBRProbeIBLPipeline: %v", err)
	}
	quad, err := render3d.UploadMesh(nil, dev, probeQuadMesh(3), render3d.MeshUpload{Label: "quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	// Create a bright white cubemap to attach to unbaked and dirty probes to prove neither leaks white on CPU or GPU.
	brightCM := probeSolidCubemap(4, color.RGBA{R: 255, G: 255, B: 255, A: 255}, false)
	brightGPU, err := render3d.UploadCubemap(nil, dev, brightCM, render3d.CubemapUpload{Label: "bright"})
	if err != nil {
		t.Fatalf("UploadCubemap: %v", err)
	}
	t.Cleanup(func() { _ = brightGPU.Close() })

	unbakedProbe := &render3d.ReflectionProbe{
		ID: "unbaked", BoxMin: math3d.Vec3{X: -5, Y: -5, Z: -5}, BoxMax: math3d.Vec3{X: 5, Y: 5, Z: 5},
		Cubemap: brightCM, Prefiltered: brightCM, GPUCubemap: brightGPU,
		Baked: false,
	}
	dirtyProbe := &render3d.ReflectionProbe{
		ID: "dirty", BoxMin: math3d.Vec3{X: -5, Y: -5, Z: -5}, BoxMax: math3d.Vec3{X: 5, Y: 5, Z: 5},
		Cubemap: brightCM, Prefiltered: brightCM, GPUCubemap: brightGPU,
		Baked: true, Dirty: true,
	}
	readyRed := &render3d.ReflectionProbe{
		ID: "ready-red", BoxMin: math3d.Vec3{X: -5, Y: -5, Z: -5}, BoxMax: math3d.Vec3{X: 5, Y: 5, Z: 5},
	}
	if err := render3d.BakeReflectionProbe(nil, dev, readyRed, render3d.ProbeBakeOptions{
		Size: 8, SkipPrefilter: true,
	}, func(_, _ math3d.Vec3) [3]float32 {
		return [3]float32{0.8, 0.1, 0.1}
	}); err != nil {
		t.Fatalf("BakeReflectionProbe red: %v", err)
	}
	t.Cleanup(func() { _ = readyRed.Close() })

	cases := []struct {
		name      string
		blend     render3d.ProbeBlendResult
		wantBlack bool
	}{
		{"unbaked manual blend + nil sky", render3d.ProbeBlendResult{Primary: unbakedProbe, Weight0: 1}, true},
		{"dirty manual blend + nil sky", render3d.ProbeBlendResult{Primary: dirtyProbe, Weight0: 1}, true},
		{"nil sky only", render3d.ProbeBlendResult{SkyWeight: 1}, true},
		{"ready red probe + nil sky", render3d.ProbeBlendResult{Primary: readyRed, Weight0: 1}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := probeIBL.UpdateBlend(dev, render3d.IrradianceSH{}, tc.blend, nil, 1.0); err != nil {
				t.Fatalf("UpdateBlend: %v", err)
			}
			enc := dev.BeginEncoder()
			pass := enc.BeginRenderPass(gpu.RenderPassDesc{
				ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
				DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
			})
			pass.SetPipeline(pipe)
			pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
			lightBuf.Bind(pass)
			pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
			probeIBL.Bind(pass)
			quad.Draw(pass)
			pass.End()
			dev.Submit(enc.Finish())
			sr.WaitIdle()
			if err := sr.LastError(); err != nil {
				t.Fatalf("software renderer: %v", err)
			}

			got := sr.Image().RGBAAt(w/2, h/2)
			n := math3d.Vec3{Z: 1}
			v := math3d.Vec3{Z: 1}
			cpu := render3d.EvaluateProbeIBL(
				render3d.IrradianceSH{}, tc.blend, nil, 1.0,
				math3d.Vec3Zero(), n, v,
				[3]float32{mat.BaseColor.X, mat.BaseColor.Y, mat.BaseColor.Z},
				mat.Metallic, mat.Roughness, 1.0,
			)
			srgb := render3d.LinearToSRGB(math3d.Vec3{X: cpu[0], Y: cpu[1], Z: cpu[2]})
			wantR, wantG, wantB := probeSRGBByte(srgb.X), probeSRGBByte(srgb.Y), probeSRGBByte(srgb.Z)
			if tc.wantBlack {
				if cpu != ([3]float32{}) {
					t.Fatalf("CPU EvaluateProbeIBL = %v, want exact black [0,0,0]", cpu)
				}
				if got.R != 0 || got.G != 0 || got.B != 0 {
					t.Fatalf("GPU pixel = %v, want exact black (0,0,0) (never white)", got)
				}
			} else {
				if !(got.R > 150 && got.G < 110 && got.B < 110) {
					t.Fatalf("GPU pixel = %v, want reddish reflection", got)
				}
				if probeAbsDiffU8(got.R, wantR) > 5 || probeAbsDiffU8(got.G, wantG) > 5 || probeAbsDiffU8(got.B, wantB) > 5 {
					t.Fatalf("GPU pixel %v != CPU ref (%d,%d,%d)", got, wantR, wantG, wantB)
				}
			}
		})
	}
}

func TestLitPBRProbeIBLBlendingParallaxAndLightProbes(t *testing.T) {
	const w, h = 64, 64
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
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

	probeIBL, err := render3d.NewGPUProbeIBLBuffer(nil, dev, render3d.ProbeIBLBufferOptions{Label: "probe-ibl"})
	if err != nil {
		t.Fatalf("NewGPUProbeIBLBuffer: %v", err)
	}
	t.Cleanup(func() { _ = probeIBL.Close() })

	pipe, err := render3d.NewLitPBRProbeIBLPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), probeIBL.Layout(), gpu.FormatDepth24Plus)
	if err != nil {
		t.Fatalf("NewLitPBRProbeIBLPipeline: %v", err)
	}
	quad, err := render3d.UploadMesh(nil, dev, probeQuadMesh(3), render3d.MeshUpload{Label: "quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	renderFrame := func(cam *scene3d.Camera, model math3d.Mat4, mat render3d.PBRMaterial, rig render3d.LightRig) *image.RGBA {
		camBuf.WriteCamera(cam)
		if err := lightBuf.Update(dev, rig); err != nil {
			t.Fatalf("lightBuf.Update: %v", err)
		}
		drawBuf.Write(dev, render3d.NewPBRDrawUniform(model, mat))
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		lightBuf.Bind(pass)
		pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
		probeIBL.Bind(pass)
		quad.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		return sr.Image()
	}

	// --- Subtest 1: Smooth blending between Probe A (red), Probe B (blue), and Sky (green) ---
	t.Run("smooth reflection blending over sky", func(t *testing.T) {
		skyCM := probeSolidCubemap(8, color.RGBA{R: 25, G: 210, B: 40, A: 255}, false)
		skyPref, err := render3d.PrefilterEnvironment(skyCM, render3d.PrefilterOptions{BaseSize: 8, MipLevels: 3, SampleCount: 16})
		if err != nil {
			t.Fatalf("PrefilterEnvironment sky: %v", err)
		}
		skyGPU, err := render3d.UploadCubemap(nil, dev, skyPref, render3d.CubemapUpload{Label: "sky-green"})
		if err != nil {
			t.Fatalf("UploadCubemap sky: %v", err)
		}
		t.Cleanup(func() { _ = skyGPU.Close() })

		probeA := &render3d.ReflectionProbe{
			ID: "red", Position: math3d.Vec3{X: -4},
			BoxMin: math3d.Vec3{X: -8, Y: -4, Z: -4}, BoxMax: math3d.Vec3{X: 2, Y: 4, Z: 4},
			BlendDistance: 2,
		}
		probeB := &render3d.ReflectionProbe{
			ID: "blue", Position: math3d.Vec3{X: 4},
			BoxMin: math3d.Vec3{X: -2, Y: -4, Z: -4}, BoxMax: math3d.Vec3{X: 8, Y: 4, Z: 4},
			BlendDistance: 2,
		}
		bakeOpts := render3d.ProbeBakeOptions{Size: 8, Prefilter: render3d.PrefilterOptions{BaseSize: 8, MipLevels: 3, SampleCount: 16}}
		if err := render3d.BakeReflectionProbe(nil, dev, probeA, bakeOpts, func(_, _ math3d.Vec3) [3]float32 {
			return [3]float32{0.9, 0.08, 0.08}
		}); err != nil {
			t.Fatalf("bake A: %v", err)
		}
		t.Cleanup(func() { _ = probeA.Close() })
		if err := render3d.BakeReflectionProbe(nil, dev, probeB, bakeOpts, func(_, _ math3d.Vec3) [3]float32 {
			return [3]float32{0.08, 0.12, 0.9}
		}); err != nil {
			t.Fatalf("bake B: %v", err)
		}
		t.Cleanup(func() { _ = probeB.Close() })

		var set render3d.ReflectionProbeSet
		set.Add(probeA, probeB)

		mirror := render3d.PBRMaterial{BaseColor: math3d.Vec3One(), Metallic: 1, Roughness: 0.1}
		positions := []struct {
			name string
			x    float32
		}{
			{"inside red probe", -4},
			{"overlap midpoint", 0},
			{"inside blue probe", 4},
			{"outside in green sky", 12},
		}
		for _, p := range positions {
			objPos := math3d.Vec3{X: p.x}
			cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: p.x, Z: 3}, math3d.DegToRad(50), 1, 0.1, 20)
			cam.LookAt(objPos, math3d.Vec3Up())

			blend := set.SelectReflectionProbes(objPos)
			if err := probeIBL.UpdateForPoint(dev, objPos, &set, nil, skyGPU, 1.0); err != nil {
				t.Fatalf("UpdateForPoint(%s): %v", p.name, err)
			}
			img := renderFrame(cam, math3d.Translate(objPos), mirror, render3d.LightRig{})
			got := img.RGBAAt(w/2, h/2)

			cpu := render3d.EvaluateProbeIBL(
				render3d.IrradianceSH{}, blend, skyPref, 1.0,
				objPos, math3d.Vec3{Z: 1}, math3d.Vec3{Z: 1},
				[3]float32{1, 1, 1}, mirror.Metallic, mirror.Roughness, 1.0,
			)
			srgb := render3d.LinearToSRGB(math3d.Vec3{X: cpu[0], Y: cpu[1], Z: cpu[2]})
			want := [3]uint8{probeSRGBByte(srgb.X), probeSRGBByte(srgb.Y), probeSRGBByte(srgb.Z)}
			if probeAbsDiffU8(got.R, want[0]) > 5 || probeAbsDiffU8(got.G, want[1]) > 5 || probeAbsDiffU8(got.B, want[2]) > 5 {
				t.Errorf("%s: GPU=%v != CPU ref=%v", p.name, got, want)
			}
		}
	})

	// --- Subtest 2: Parallax box projection shifts reflection from +X red face to +Z cyan face ---
	t.Run("parallax box projection", func(t *testing.T) {
		cardCM := probeCardinalCubemap(16)
		cardGPU, err := render3d.UploadCubemap(nil, dev, cardCM, render3d.CubemapUpload{Label: "cardinal"})
		if err != nil {
			t.Fatalf("UploadCubemap cardinal: %v", err)
		}
		t.Cleanup(func() { _ = cardGPU.Close() })

		roomProbe := &render3d.ReflectionProbe{
			ID:            "parallax-room",
			Position:      math3d.Vec3Zero(),
			BoxMin:        math3d.Vec3{X: -5, Y: -5, Z: -5},
			BoxMax:        math3d.Vec3{X: 5, Y: 5, Z: 5},
			BoxProjection: false,
			Cubemap:       cardCM,
			Prefiltered:   cardCM,
			GPUCubemap:    cardGPU,
			Baked:         true,
		}
		// Place mirror quad at (0, 0, 4.2) facing +X (rotated +90 deg around Y) viewed from (+3, 0, 4.2).
		// Normal is +X, view is +X -> reflection ray is +X.
		// Without box projection: samples +X face (Red).
		// With box projection from (0, 0, 4.2) along +X inside [-5, 5]^3: hits (5, 0, 4.2),
		// Wait: |5| > |4.2|, so to cross into +Z face we place the wall closer to +Z at z=4.5 and box X in [-5, 3]!
		roomProbe.BoxMin = math3d.Vec3{X: -5, Y: -5, Z: -5}
		roomProbe.BoxMax = math3d.Vec3{X: 2, Y: 5, Z: 5}
		// Surface at (0, 0, 4.5) reflecting along +X hits (2, 0, 4.5): offset = (2, 0, 4.5) -> dominant axis is +Z (Cyan)!
		surfPos := math3d.Vec3{X: 0, Y: 0, Z: 4.5}
		model := math3d.Translate(surfPos).Mul(math3d.RotateY(math.Pi / 2))
		cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 2.5, Y: 0, Z: 4.5}, math3d.DegToRad(50), 1, 0.1, 20)
		cam.LookAt(surfPos, math3d.Vec3Up())
		mirror := render3d.PBRMaterial{BaseColor: math3d.Vec3One(), Metallic: 1, Roughness: 0.05}

		// Without BoxProjection -> samples +X face (Red).
		roomProbe.BoxProjection = false
		blendNoBP := render3d.ProbeBlendResult{Primary: roomProbe, Weight0: 1}
		if err := probeIBL.UpdateBlend(dev, render3d.IrradianceSH{}, blendNoBP, nil, 1.0); err != nil {
			t.Fatalf("UpdateBlend no-BP: %v", err)
		}
		pxNoBP := renderFrame(cam, model, mirror, render3d.LightRig{}).RGBAAt(w/2, h/2)
		if !(pxNoBP.R > 180 && pxNoBP.G < 110 && pxNoBP.B < 110) {
			t.Fatalf("without BoxProjection = %v, want +X Red face", pxNoBP)
		}

		// With BoxProjection -> hits (2, 0, 4.5) -> dominant +Z face (Cyan).
		roomProbe.BoxProjection = true
		blendBP := render3d.ProbeBlendResult{Primary: roomProbe, Weight0: 1}
		if err := probeIBL.UpdateBlend(dev, render3d.IrradianceSH{}, blendBP, nil, 1.0); err != nil {
			t.Fatalf("UpdateBlend BP: %v", err)
		}
		pxBP := renderFrame(cam, model, mirror, render3d.LightRig{}).RGBAAt(w/2, h/2)
		if !(pxBP.G > 180 && pxBP.B > 180 && pxBP.R < 110) {
			t.Fatalf("with BoxProjection = %v, want +Z Cyan face", pxBP)
		}
		cpuBP := render3d.EvaluateProbeIBL(
			render3d.IrradianceSH{}, blendBP, nil, 1.0,
			surfPos, math3d.Vec3{X: 1}, math3d.Vec3{X: 1},
			[3]float32{1, 1, 1}, mirror.Metallic, mirror.Roughness, 1.0,
		)
		srgbBP := render3d.LinearToSRGB(math3d.Vec3{X: cpuBP[0], Y: cpuBP[1], Z: cpuBP[2]})
		if probeAbsDiffU8(pxBP.R, probeSRGBByte(srgbBP.X)) > 5 ||
			probeAbsDiffU8(pxBP.G, probeSRGBByte(srgbBP.Y)) > 5 ||
			probeAbsDiffU8(pxBP.B, probeSRGBByte(srgbBP.Z)) > 5 {
			t.Fatalf("parallax GPU pixel %v != CPU ref (%d,%d,%d)", pxBP, probeSRGBByte(srgbBP.X), probeSRGBByte(srgbBP.Y), probeSRGBByte(srgbBP.Z))
		}
	})

	// --- Subtest 3: Dynamic object moving through LightProbeGrid + LightRig direct light + non-uniform scale normal matrix ---
	t.Run("light probe grid plus direct light and normal matrix", func(t *testing.T) {
		grid, err := render3d.NewLightProbeGrid(math3d.AABB{
			Min: math3d.Vec3{X: -4, Y: -2, Z: -2},
			Max: math3d.Vec3{X: 4, Y: 2, Z: 2},
		}, 2, 1, 1)
		if err != nil {
			t.Fatalf("NewLightProbeGrid: %v", err)
		}
		if err := grid.Bake(8, func(pos, _ math3d.Vec3) [3]float32 {
			if pos.X < 0 {
				return [3]float32{0.8, 0.25, 0.05} // warm amber room
			}
			return [3]float32{0.05, 0.3, 0.85} // cool blue room
		}); err != nil {
			t.Fatalf("grid.Bake: %v", err)
		}
		vol := &render3d.LightProbeVolume{Grid: grid}

		rig := render3d.LightRig{
			Directional: []render3d.DirectionalLight{{
				Direction: math3d.Vec3{X: 0, Y: 0, Z: -1},
				Color:     math3d.Vec3{X: 0.2, Y: 0.2, Z: 0.2},
				Intensity: 1.0,
			}},
		}
		// Rough dielectric with slight non-uniform scale to exercise the inverse-transpose normal matrix in LitPBRProbeIBLWGSL.
		mat := render3d.PBRMaterial{
			BaseColor: math3d.Vec3{X: 0.85, Y: 0.85, Z: 0.85},
			Metallic:  0,
			Roughness: 0.9,
			Emissive:  math3d.Vec3{X: 0.02, Y: 0.02, Z: 0.02},
		}

		var prevR, prevB uint8
		for i, x := range []float32{-4, 0, 4} {
			pos := math3d.Vec3{X: x}
			cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: x, Z: 3}, math3d.DegToRad(50), 1, 0.1, 20)
			cam.LookAt(pos, math3d.Vec3Up())
			model := math3d.Translate(pos).Mul(math3d.Scale(math3d.Vec3{X: 1.5, Y: 0.8, Z: 1.0}))

			if err := probeIBL.UpdateForPoint(dev, pos, nil, vol, nil, 1.0); err != nil {
				t.Fatalf("UpdateForPoint: %v", err)
			}
			px := renderFrame(cam, model, mat, rig).RGBAAt(w/2, h/2)

			sh := vol.SampleLightProbes(pos)
			direct := render3d.ShadePoint(pos, math3d.Vec3{Z: 1}, math3d.Vec3{Z: 1}, mat, rig)
			env := render3d.EvaluateProbeIBL(
				sh, render3d.ProbeBlendResult{SkyWeight: 1}, nil, 1.0,
				pos, math3d.Vec3{Z: 1}, math3d.Vec3{Z: 1},
				[3]float32{mat.BaseColor.X, mat.BaseColor.Y, mat.BaseColor.Z},
				mat.Metallic, mat.Roughness, 1.0,
			)
			srgb := render3d.LinearToSRGB(math3d.Vec3{X: direct.X + env[0], Y: direct.Y + env[1], Z: direct.Z + env[2]})
			wantR, wantG, wantB := probeSRGBByte(srgb.X), probeSRGBByte(srgb.Y), probeSRGBByte(srgb.Z)
			if probeAbsDiffU8(px.R, wantR) > 5 || probeAbsDiffU8(px.G, wantG) > 5 || probeAbsDiffU8(px.B, wantB) > 5 {
				t.Errorf("x=%v: GPU=%v != CPU ref=(%d,%d,%d)", x, px, wantR, wantG, wantB)
			}
			if i > 0 {
				if !(px.R < prevR && px.B > prevB) {
					t.Errorf("expected smooth warm-to-cool transition at x=%v: got %v after R=%d B=%d", x, px, prevR, prevB)
				}
			}
			prevR, prevB = px.R, px.B
		}
	})
}

func TestLitPBRProbeIBLGPUViewAndAsyncBake(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{name: "full"},
		{name: "NoGPUCompute", opts: []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	for _, prof := range profiles {
		t.Run(prof.name, func(t *testing.T) {
			const size = 64
			var (
				dev        *gpu.Device
				camBuf     *render3d.CameraBuffer
				lightBuf   *render3d.GPULightBuffer
				drawBuf    *render3d.PBRDrawBuffer
				probeIBL   *render3d.GPUProbeIBLBuffer
				pipe       *gpu.Pipeline
				quad       *render3d.GPUMesh
				bakeProbe  *render3d.ReflectionProbe
				probeSet   render3d.ReflectionProbeSet
				lightVol   *render3d.LightProbeVolume
				frames     int
				bakeDone   bool
				bakeErr    error
				rebakeDone bool
				rebakeErr  error
			)

			grid, err := render3d.NewLightProbeGrid(math3d.AABB{
				Min: math3d.Vec3{X: -4, Y: -2, Z: -2},
				Max: math3d.Vec3{X: 4, Y: 2, Z: 2},
			}, 2, 1, 1)
			if err != nil {
				t.Fatalf("NewLightProbeGrid: %v", err)
			}
			if err := grid.Bake(4, func(_, _ math3d.Vec3) [3]float32 {
				return [3]float32{0.15, 0.25, 0.4}
			}); err != nil {
				t.Fatalf("grid.Bake: %v", err)
			}
			lightVol = &render3d.LightProbeVolume{Grid: grid}

			bakeProbe = &render3d.ReflectionProbe{
				ID:     "live-view-probe",
				BoxMin: math3d.Vec3{X: -5, Y: -5, Z: -5},
				BoxMax: math3d.Vec3{X: 5, Y: 5, Z: 5},
			}
			probeSet.Add(bakeProbe)

			cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(50), 1, 0.1, 20)
			cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
			mat := render3d.PBRMaterial{
				BaseColor: math3d.Vec3{X: 0.95, Y: 0.95, Z: 0.95},
				Metallic:  0.85,
				Roughness: 0.15,
			}

			opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, prof.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "probe-viewport",
					Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(d *gpu.Device) {
						dev = d
						var err error
						camBuf, err = render3d.NewCameraBuffer(cx, d)
						if err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
						lightBuf, err = render3d.NewLightBuffer(cx, d, render3d.LightRig{
							Directional: []render3d.DirectionalLight{{
								Direction: math3d.Vec3{X: -0.2, Y: -0.3, Z: -1},
								Color:     math3d.Vec3One(),
								Intensity: 0.4,
							}},
						}, render3d.LightBufferOptions{})
						if err != nil {
							t.Fatalf("NewLightBuffer: %v", err)
						}
						drawBuf, err = render3d.NewPBRDrawBuffer(cx, d, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), mat), false)
						if err != nil {
							t.Fatalf("NewPBRDrawBuffer: %v", err)
						}
						probeIBL, err = render3d.NewGPUProbeIBLBuffer(cx, d, render3d.ProbeIBLBufferOptions{Label: "live-probe-ibl"})
						if err != nil {
							t.Fatalf("NewGPUProbeIBLBuffer: %v", err)
						}
						pipe, err = render3d.NewLitPBRProbeIBLPipeline(cx, d, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), probeIBL.Layout(), gpu.FormatDepth24Plus)
						if err != nil {
							t.Fatalf("NewLitPBRProbeIBLPipeline: %v", err)
						}
						quad, err = render3d.UploadMesh(cx, d, probeQuadMesh(3), render3d.MeshUpload{Label: "quad"})
						if err != nil {
							t.Fatalf("UploadMesh: %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						frames++
						camBuf.WriteCamera(cam)
						drawBuf.Write(frame.Device(), render3d.NewPBRDrawUniform(math3d.Mat4Identity(), mat))
						if err := probeIBL.UpdateForPoint(frame.Device(), math3d.Vec3Zero(), &probeSet, lightVol, nil, 1.0); err != nil {
							t.Fatalf("UpdateForPoint: %v", err)
						}
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x000000))
						pass.ClearDepth(1)
						pass.SetPipeline(pipe)
						pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
						lightBuf.Bind(pass)
						pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
						probeIBL.Bind(pass)
						quad.Draw(pass)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			// Trigger initial frame so OnReady runs.
			app.Advance(16 * time.Millisecond)
			if dev == nil || frames == 0 {
				t.Fatal("gpu.View OnReady/OnFrame did not run")
			}

			// 1. Async GPU bake with SRGB: false (RGBA8Unorm) rendering real 3D geometry via ProbeFaceCamera on the live gpu.View engine goroutine:
			// draw a bright red wall on +Z and a bright green wall on +Y so face orientation and row flipping are verified end-to-end.
			var (
				bakePipe  *gpu.Pipeline
				wallRed   *render3d.PBRDrawBuffer
				wallGreen *render3d.PBRDrawBuffer
				bakeLight *render3d.GPULightBuffer
			)
			app.Post(func() {
				var err error
				bakeLight, err = render3d.NewLightBuffer(nil, dev, render3d.LightRig{}, render3d.LightBufferOptions{})
				if err != nil {
					t.Errorf("NewLightBuffer bake: %v", err)
					return
				}
				// Emissive quads facing inward toward the probe origin (0, 0, 0).
				// +Z wall at z=+2 facing -Z (rotated 180 deg around Y); +Y ceiling at y=+2 facing -Y (rotated +90 deg around X).
				wallRed, err = render3d.NewPBRDrawBuffer(nil, dev, drawBuf.Layout(), render3d.NewPBRDrawUniform(
					math3d.Translate(math3d.Vec3{Z: 2}).Mul(math3d.RotateY(math.Pi)),
					render3d.PBRMaterial{Emissive: math3d.Vec3{X: 0.9, Y: 0.05, Z: 0.05}},
				), false)
				if err != nil {
					t.Errorf("NewPBRDrawBuffer wallRed: %v", err)
					return
				}
				wallGreen, err = render3d.NewPBRDrawBuffer(nil, dev, drawBuf.Layout(), render3d.NewPBRDrawUniform(
					math3d.Translate(math3d.Vec3{Y: 2}).Mul(math3d.RotateX(math.Pi/2)),
					render3d.PBRMaterial{Emissive: math3d.Vec3{X: 0.05, Y: 0.9, Z: 0.05}},
				), false)
				if err != nil {
					t.Errorf("NewPBRDrawBuffer wallGreen: %v", err)
					return
				}
				bakePipe, err = render3d.NewLitPBRPipelineWithOptions(nil, dev, camBuf.Layout(), bakeLight.Layout(), drawBuf.Layout(), render3d.LitPBRPipelineOptions{
					ColorFormat:  gpu.FormatRGBA8Unorm,
					DepthFormat:  gpu.FormatDepth24Plus,
					LinearOutput: true,
				})
				if err != nil {
					t.Errorf("NewLitPBRPipelineWithOptions bake: %v", err)
					return
				}
				render3d.BakeReflectionProbeGPU(nil, dev, bakeProbe, render3d.ProbeBakeOptions{
					Size:       8,
					ClearColor: core.RGB(0x050510),
					SRGB:       false,
					Prefilter:  render3d.PrefilterOptions{BaseSize: 8, MipLevels: 3, SampleCount: 16},
				}, func(pass *gpu.RenderPass, _ gpu.CubeFace, faceCam render3d.CameraUniform) {
					camBuf.Write(faceCam)
					pass.SetPipeline(bakePipe)
					pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
					bakeLight.Bind(pass)
					pass.SetBindGroup(render3d.PBRDrawGroup, wallRed.BindGroup())
					quad.Draw(pass)
					pass.SetBindGroup(render3d.PBRDrawGroup, wallGreen.BindGroup())
					quad.Draw(pass)
				}, func(err error) {
					bakeErr = err
					bakeDone = true
				})
			})
			for range 8 {
				app.Settle()
				if bakeDone {
					break
				}
				app.Advance(16 * time.Millisecond)
			}
			app.Post(func() {
				_ = bakePipe.Close()
				_ = wallRed.Close()
				_ = wallGreen.Close()
				_ = bakeLight.Close()
			})
			app.Settle()
			if !bakeDone {
				t.Fatal("async BakeReflectionProbeGPU callback did not complete on live gpu.View")
			}
			if bakeErr != nil {
				t.Fatalf("async BakeReflectionProbeGPU (linear): %v", bakeErr)
			}
			if !bakeProbe.Baked || bakeProbe.Dirty || !bakeProbe.GPUCubemap.Valid() {
				t.Fatalf("probe not ready after async bake: baked=%v dirty=%v valid=%v", bakeProbe.Baked, bakeProbe.Dirty, bakeProbe.GPUCubemap.Valid())
			}
			if bakeProbe.GPUCubemap.Format() != gpu.FormatRGBA8Unorm {
				t.Fatalf("linear bake format = %s, want RGBA8Unorm", bakeProbe.GPUCubemap.Format())
			}
			// Verify captured cubemap faces reflect the actual 3D geometry orientation (+Z red, +Y green, -Z dark clear).
			posZSample := bakeProbe.Cubemap.Sample(math3d.Vec3{Z: 1})
			posYSample := bakeProbe.Cubemap.Sample(math3d.Vec3{Y: 1})
			negZSample := bakeProbe.Cubemap.Sample(math3d.Vec3{Z: -1})
			if !(posZSample[0] > 0.7 && posZSample[1] < 0.2) {
				t.Fatalf("captured +Z face = %v, want red wall", posZSample)
			}
			if !(posYSample[1] > 0.7 && posYSample[0] < 0.2) {
				t.Fatalf("captured +Y face = %v, want green ceiling", posYSample)
			}
			if !(negZSample[0] < 0.15 && negZSample[1] < 0.15) {
				t.Fatalf("captured -Z face = %v, want dark clear color", negZSample)
			}

			// Render a frame using the newly baked red reflection probe and verify pixels.
			app.Advance(16 * time.Millisecond)
			redImg, err := app.GPUReadPixels("id:probe-viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels after red bake: %v", err)
			}
			redCenter := redImg.RGBAAt(size/2, size/2)
			if !(redCenter.R > redCenter.B+60 && redCenter.R > 160) {
				t.Fatalf("rendered pixel with red reflection probe = %v, want strongly red-tinted reflection", redCenter)
			}

			// 2. Async GPU re-bake with SRGB: true (RGBA8UnormSRGB) -> old GPUCubemap must close and new format must be sRGB!
			firstGPU := bakeProbe.GPUCubemap
			app.Post(func() {
				render3d.BakeReflectionProbeGPU(nil, dev, bakeProbe, render3d.ProbeBakeOptions{
					Size:       8,
					ClearColor: core.RGB(0x1830e8),
					SRGB:       true,
					Prefilter:  render3d.PrefilterOptions{BaseSize: 8, MipLevels: 3, SampleCount: 16},
				}, func(_ *gpu.RenderPass, _ gpu.CubeFace, _ render3d.CameraUniform) {
					// Clear color fills every face.
				}, func(err error) {
					rebakeErr = err
					rebakeDone = true
				})
			})
			for range 8 {
				app.Settle()
				if rebakeDone {
					break
				}
				app.Advance(16 * time.Millisecond)
			}
			if !rebakeDone || rebakeErr != nil {
				t.Fatalf("async sRGB re-bake: done=%v err=%v", rebakeDone, rebakeErr)
			}
			if firstGPU.Valid() {
				t.Fatal("previous GPUCubemap remained valid after async GPU re-bake")
			}
			if bakeProbe.GPUCubemap.Format() != gpu.FormatRGBA8UnormSRGB || !bakeProbe.Cubemap.SRGB {
				t.Fatalf("sRGB bake format=%s cubemap.SRGB=%v, want RGBA8UnormSRGB / true", bakeProbe.GPUCubemap.Format(), bakeProbe.Cubemap.SRGB)
			}

			// Render again and confirm pixels shifted to blue reflection.
			app.Advance(16 * time.Millisecond)
			blueImg, err := app.GPUReadPixels("id:probe-viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels after blue re-bake: %v", err)
			}
			blueCenter := blueImg.RGBAAt(size/2, size/2)
			if !(blueCenter.B > blueCenter.R+40 && blueCenter.B > 120) {
				t.Fatalf("rendered pixel after blue re-bake = %v, want strongly blue-tinted reflection", blueCenter)
			}

			app.Post(func() { _ = bakeProbe.Close() })
			app.Settle()
		})
	}
}

func TestProbeIBLUniformValidationAndLifecycle(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	valid := render3d.ProbeIBLUniform{Intensity: 1, SkyWeight: 1}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid ProbeIBLUniform: %v", err)
	}
	for name, mutate := range map[string]func(*render3d.ProbeIBLUniform){
		"NaN SH":          func(u *render3d.ProbeIBLUniform) { u.SH.Coeff[3][2] = nan },
		"Inf probe0 pos":  func(u *render3d.ProbeIBLUniform) { u.Probe0Pos.X = inf },
		"NaN probe1 min":  func(u *render3d.ProbeIBLUniform) { u.Probe1BoxMin.Y = nan },
		"negative w0":     func(u *render3d.ProbeIBLUniform) { u.Probe0Weight = -0.1 },
		"negative lod1":   func(u *render3d.ProbeIBLUniform) { u.Probe1MaxLOD = -1 },
		"negative sky w":  func(u *render3d.ProbeIBLUniform) { u.SkyWeight = -0.2 },
		"NaN intensity":   func(u *render3d.ProbeIBLUniform) { u.Intensity = nan },
		"negative skyLOD": func(u *render3d.ProbeIBLUniform) { u.SkyMaxLOD = -1 },
	} {
		u := valid
		mutate(&u)
		if err := u.Validate(); !errors.Is(err, render3d.ErrInvalidProbe) {
			t.Errorf("%s: Validate = %v, want ErrInvalidProbe", name, err)
		}
	}

	// Zero-allocation Encode and panic on short buffer.
	var dst [render3d.ProbeIBLUniformSize]byte
	allocs := testing.AllocsPerRun(50, func() {
		_ = valid.Encode(dst[:])
	})
	if allocs != 0 {
		t.Fatalf("ProbeIBLUniform.Encode allocs = %v, want 0", allocs)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Encode on short buffer did not panic")
			}
		}()
		var short [render3d.ProbeIBLUniformSize - 1]byte
		valid.Encode(short[:])
	}()

	// Custom group WGSL helper and LinearOutput HDR pipeline compile cleanly.
	customSrc := render3d.ProbeIBLWGSLForGroup(2) + `
@vertex fn vs(@builtin(vertex_index) i: u32) -> @builtin(position) vec4<f32> { return vec4<f32>(0.0, 0.0, 0.0, 1.0); }
@fragment fn fs() -> @location(0) vec4<f32> {
	let c = evaluate_probe_ibl(vec3<f32>(0.0), vec3<f32>(0.0, 1.0, 0.0), vec3<f32>(0.0, 0.0, 1.0), vec3<f32>(1.0), 0.5, 0.5, 1.0);
	return vec4<f32>(c, 1.0);
}
`
	if _, err := gpu.CompileWGSL(customSrc); err != nil {
		t.Fatalf("CompileWGSL ProbeIBLWGSLForGroup(2): %v", err)
	}

	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	sharedLayout, err := render3d.ProbeIBLBindGroupLayout(nil, dev)
	if err != nil {
		t.Fatalf("ProbeIBLBindGroupLayout: %v", err)
	}
	t.Cleanup(func() { _ = sharedLayout.Close() })

	buf, err := render3d.NewGPUProbeIBLBuffer(nil, dev, render3d.ProbeIBLBufferOptions{
		Label:  "shared",
		Layout: sharedLayout,
	})
	if err != nil {
		t.Fatalf("NewGPUProbeIBLBuffer: %v", err)
	}
	if !buf.Valid() || buf.ByteSize() == 0 {
		t.Fatalf("unexpected GPUProbeIBLBuffer state: valid=%v bytes=%d", buf.Valid(), buf.ByteSize())
	}

	// Updating uniform with identical cubemaps reuses the same BindGroup without recreating it.
	bgBefore := buf.BindGroup()
	if err := buf.Update(dev, render3d.ProbeIBLUniform{Intensity: 0.5}, nil, nil, nil); err != nil {
		t.Fatalf("Update same textures: %v", err)
	}
	if buf.BindGroup() != bgBefore {
		t.Fatal("Update recreated BindGroup even though bound cubemaps did not change")
	}

	// Switching to a new cubemap creates a new BindGroup and closes the old one.
	skyCube, err := render3d.UploadCubemap(nil, dev, probeSolidCubemap(4, color.RGBA{R: 100, G: 150, B: 200, A: 255}, false), render3d.CubemapUpload{})
	if err != nil {
		t.Fatalf("UploadCubemap: %v", err)
	}
	t.Cleanup(func() { _ = skyCube.Close() })
	if err := buf.Update(dev, render3d.ProbeIBLUniform{Intensity: 1, SkyWeight: 1}, nil, nil, skyCube); err != nil {
		t.Fatalf("Update new sky: %v", err)
	}
	if buf.BindGroup() == bgBefore || bgBefore.Valid() {
		t.Fatal("expected old BindGroup to be replaced and closed when cubemaps changed")
	}

	// LinearOutput HDR pipeline builds cleanly.
	camBuf, _ := render3d.NewCameraBuffer(nil, dev)
	t.Cleanup(func() { _ = camBuf.Close() })
	lightBuf, _ := render3d.NewLightBuffer(nil, dev, render3d.LightRig{}, render3d.LightBufferOptions{})
	t.Cleanup(func() { _ = lightBuf.Close() })
	drawBuf, _ := render3d.NewPBRDrawBuffer(nil, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), render3d.PBRMaterial{}), false)
	t.Cleanup(func() { _ = drawBuf.Close() })
	hdrPipe, err := render3d.NewLitPBRProbeIBLPipelineWithOptions(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), sharedLayout, render3d.ProbeIBLPipelineOptions{
		ColorFormat:  gpu.FormatRGBA16Float,
		DepthFormat:  gpu.FormatDepth24Plus,
		LinearOutput: true,
	})
	if err != nil {
		t.Fatalf("NewLitPBRProbeIBLPipelineWithOptions LinearOutput: %v", err)
	}
	_ = hdrPipe.Close()

	// Close does not close a shared layout.
	if err := buf.Close(); err != nil {
		t.Fatalf("buf.Close: %v", err)
	}
	if !sharedLayout.Valid() {
		t.Fatal("Close closed a shared ProbeIBLBindGroupLayout")
	}
	if err := buf.Close(); err != nil {
		t.Fatalf("second buf.Close: %v", err)
	}

	// Nil receivers are safe.
	var nilBuf *render3d.GPUProbeIBLBuffer
	if nilBuf.Valid() || nilBuf.Close() != nil || nilBuf.ByteSize() != 0 {
		t.Fatal("nil GPUProbeIBLBuffer should be safe and invalid")
	}
}
