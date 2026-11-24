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

func TestLightValidation(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	validDir := render3d.DirectionalLight{
		Direction: math3d.Vec3{X: 0, Y: -1, Z: 0},
		Color:     math3d.Vec3{X: 1, Y: 1, Z: 1},
		Intensity: 2,
	}
	validPoint := render3d.PointLight{
		Position:  math3d.Vec3{X: 1, Y: 2, Z: 3},
		Color:     math3d.Vec3{X: 1, Y: 0.5, Z: 0.25},
		Intensity: 5,
		Range:     10,
	}
	validAmbient := render3d.AmbientLight{
		Color:     math3d.Vec3{X: 0.2, Y: 0.3, Z: 0.4},
		Intensity: 0.1,
	}

	tests := []struct {
		name string
		rig  render3d.LightRig
		ok   bool
	}{
		{"empty rig", render3d.LightRig{}, true},
		{"valid full rig", render3d.LightRig{Ambient: validAmbient, Directional: []render3d.DirectionalLight{validDir}, Point: []render3d.PointLight{validPoint}}, true},
		{"point unbounded range zero", render3d.LightRig{Point: []render3d.PointLight{{Position: math3d.Vec3{Y: 1}, Color: math3d.Vec3One(), Intensity: 3, Range: 0}}}, true},
		{"dir zero direction", render3d.LightRig{Directional: []render3d.DirectionalLight{{Color: math3d.Vec3One(), Intensity: 1}}}, false},
		{"dir nan direction", render3d.LightRig{Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{X: nan, Y: -1}, Color: math3d.Vec3One(), Intensity: 1}}}, false},
		{"dir negative color", render3d.LightRig{Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{Y: -1}, Color: math3d.Vec3{X: -0.1, Y: 1, Z: 1}, Intensity: 1}}}, false},
		{"dir negative intensity", render3d.LightRig{Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{Y: -1}, Color: math3d.Vec3One(), Intensity: -1}}}, false},
		{"dir inf intensity", render3d.LightRig{Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{Y: -1}, Color: math3d.Vec3One(), Intensity: inf}}}, false},
		{"point inf pos", render3d.LightRig{Point: []render3d.PointLight{{Position: math3d.Vec3{X: inf}, Color: math3d.Vec3One(), Intensity: 1}}}, false},
		{"point negative color", render3d.LightRig{Point: []render3d.PointLight{{Position: math3d.Vec3Zero(), Color: math3d.Vec3{Z: -1}, Intensity: 1}}}, false},
		{"point negative intensity", render3d.LightRig{Point: []render3d.PointLight{{Position: math3d.Vec3Zero(), Color: math3d.Vec3One(), Intensity: -0.5}}}, false},
		{"point negative range", render3d.LightRig{Point: []render3d.PointLight{{Position: math3d.Vec3Zero(), Color: math3d.Vec3One(), Intensity: 1, Range: -2}}}, false},
		{"point nan range", render3d.LightRig{Point: []render3d.PointLight{{Position: math3d.Vec3Zero(), Color: math3d.Vec3One(), Intensity: 1, Range: nan}}}, false},
		{"ambient negative color", render3d.LightRig{Ambient: render3d.AmbientLight{Color: math3d.Vec3{Y: -0.1}, Intensity: 1}}, false},
		{"ambient negative intensity", render3d.LightRig{Ambient: render3d.AmbientLight{Color: math3d.Vec3One(), Intensity: -1}}, false},
		{"too many directional", render3d.LightRig{Directional: []render3d.DirectionalLight{validDir, validDir, validDir, validDir, validDir}}, false},
		{"too many point", render3d.LightRig{Point: make([]render3d.PointLight, render3d.MaxPointLights+1)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rig.Validate()
			if tt.ok && err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
			if !tt.ok && !errors.Is(err, render3d.ErrInvalidLight) {
				t.Fatalf("Validate = %v, want ErrInvalidLight", err)
			}
		})
	}
}

func TestPointLightAttenuation(t *testing.T) {
	// Unbounded (range == 0): exact inverse square 1 / max(d^2, 1e-4).
	for _, tc := range []struct {
		d    float32
		want float32
	}{
		{0, 10000},
		{1, 1},
		{2, 0.25},
		{4, 0.0625},
	} {
		got := render3d.PointLightAttenuation(tc.d*tc.d, 0)
		if math.Abs(float64(got-tc.want)) > 1e-5 {
			t.Errorf("unbounded attenuation(d=%v) = %v, want %v", tc.d, got, tc.want)
		}
	}

	// glTF KHR_lights_punctual smooth windowing with Range = 10.
	const r = float32(10)
	prev := float32(math.MaxFloat32)
	for _, d := range []float32{0.5, 1, 2.5, 5, 7.5, 9.5, 10, 15} {
		got := render3d.PointLightAttenuation(d*d, r)
		ratio := float64(d / r)
		w := math.Max(math.Min(1.0-math.Pow(ratio, 4), 1.0), 0.0)
		want := float32((w * w) / math.Max(float64(d*d), 1e-4))
		if math.Abs(float64(got-want)) > 1e-5 {
			t.Errorf("windowed attenuation(d=%v, r=%v) = %v, want %v", d, r, got, want)
		}
		if got > prev {
			t.Errorf("attenuation not monotonically decreasing at d=%v: %v > %v", d, got, prev)
		}
		prev = got
	}
	if got := render3d.PointLightAttenuation(r*r, r); got != 0 {
		t.Errorf("attenuation at d == range = %v, want 0", got)
	}
	if got := render3d.PointLightAttenuation(4*r*r, r); got != 0 {
		t.Errorf("attenuation beyond range = %v, want 0", got)
	}
}

func TestLightUniformLayout(t *testing.T) {
	if render3d.LightUniformSize != 672 {
		t.Fatalf("LightUniformSize = %d, want 672", render3d.LightUniformSize)
	}
	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.2, Y: 0.4, Z: 0.6}, Intensity: 0.5},
		Directional: []render3d.DirectionalLight{
			{Direction: math3d.Vec3{X: 0, Y: -3, Z: -4}, Color: math3d.Vec3{X: 1, Y: 0.8, Z: 0.6}, Intensity: 2},
		},
		Point: []render3d.PointLight{
			{Position: math3d.Vec3{X: 1, Y: 2, Z: 3}, Color: math3d.Vec3{X: 0.25, Y: 0.5, Z: 1}, Intensity: 4, Range: 12},
		},
	}

	u, err := render3d.NewLightUniforms(rig)
	if err != nil {
		t.Fatalf("NewLightUniforms: %v", err)
	}

	// Dirty backing buffer verifies unused slots and padding are zeroed.
	dirty := make([]byte, render3d.LightUniformSize)
	for i := range dirty {
		dirty[i] = 0xFF
	}
	encoded := u.Encode(dirty)
	if len(encoded) != render3d.LightUniformSize {
		t.Fatalf("Encode len = %d, want %d", len(encoded), render3d.LightUniformSize)
	}

	appended := rig.AppendUniformData([]byte{0xAB})
	if len(appended) != 1+render3d.LightUniformSize || appended[0] != 0xAB {
		t.Fatalf("AppendUniformData len=%d prefix=%x", len(appended), appended[0])
	}
	for i := range encoded {
		if encoded[i] != appended[1+i] {
			t.Fatalf("byte %d mismatch: Encode=%02x AppendUniformData=%02x", i, encoded[i], appended[1+i])
		}
	}

	f32 := func(off int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(encoded[off:])) }
	u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(encoded[off:]) }

	// Ambient at offset 0: (0.1, 0.2, 0.3, 0.5)
	if !nearF32(f32(0), 0.1, 1e-6) || !nearF32(f32(4), 0.2, 1e-6) || !nearF32(f32(8), 0.3, 1e-6) || !nearF32(f32(12), 0.5, 1e-6) {
		t.Errorf("ambient = (%v, %v, %v, %v)", f32(0), f32(4), f32(8), f32(12))
	}
	// Counts at offset 16: (1, 1, 0, 0)
	if u32(16) != 1 || u32(20) != 1 || u32(24) != 0 || u32(28) != 0 {
		t.Errorf("counts = (%d, %d, %d, %d), want (1, 1, 0, 0)", u32(16), u32(20), u32(24), u32(28))
	}
	// Directional[0] at offset 32: to_light = -normalize(0, -3, -4) = (0, 0.6, 0.8, 0), color = (2, 1.6, 1.2, 2)
	if !nearF32(f32(32), 0, 1e-6) || !nearF32(f32(36), 0.6, 1e-6) || !nearF32(f32(40), 0.8, 1e-6) || f32(44) != 0 {
		t.Errorf("dir[0].to_light = (%v, %v, %v, %v)", f32(32), f32(36), f32(40), f32(44))
	}
	if !nearF32(f32(48), 2, 1e-6) || !nearF32(f32(52), 1.6, 1e-6) || !nearF32(f32(56), 1.2, 1e-6) || !nearF32(f32(60), 2, 1e-6) {
		t.Errorf("dir[0].color = (%v, %v, %v, %v)", f32(48), f32(52), f32(56), f32(60))
	}
	// Unused Directional[1] at offset 64 must be zeroed.
	for off := 64; off < 160; off += 4 {
		if u32(off) != 0 {
			t.Fatalf("unused directional slot at offset %d = %x, want 0", off, u32(off))
		}
	}
	// Point[0] at offset 160: pos_range = (1, 2, 3, 12), color = (1, 2, 4, 4)
	if f32(160) != 1 || f32(164) != 2 || f32(168) != 3 || f32(172) != 12 {
		t.Errorf("point[0].pos_range = (%v, %v, %v, %v)", f32(160), f32(164), f32(168), f32(172))
	}
	if !nearF32(f32(176), 1, 1e-6) || !nearF32(f32(180), 2, 1e-6) || !nearF32(f32(184), 4, 1e-6) || !nearF32(f32(188), 4, 1e-6) {
		t.Errorf("point[0].color = (%v, %v, %v, %v)", f32(176), f32(180), f32(184), f32(188))
	}
	// Unused Point[1..15] at offset 192..672 must be zeroed.
	for off := 192; off < render3d.LightUniformSize; off += 4 {
		if u32(off) != 0 {
			t.Fatalf("unused point slot at offset %d = %x, want 0", off, u32(off))
		}
	}

	// Zero-allocation checks.
	var encBuf [render3d.LightUniformSize]byte
	if allocs := testing.AllocsPerRun(50, func() { u.Encode(encBuf[:]) }); allocs != 0 {
		t.Errorf("LightUniforms.Encode allocs = %v, want 0", allocs)
	}
	appBuf := make([]byte, 0, render3d.LightUniformSize)
	if allocs := testing.AllocsPerRun(50, func() { appBuf = rig.AppendUniformData(appBuf[:0]) }); allocs != 0 {
		t.Errorf("LightRig.AppendUniformData allocs = %v, want 0", allocs)
	}
	drawU := render3d.NewPBRDrawUniform(math3d.Mat4Identity(), render3d.PBRMaterial{BaseColor: math3d.Vec3One(), Roughness: 0.5})
	var drawBuf [render3d.PBRDrawUniformSize]byte
	if allocs := testing.AllocsPerRun(50, func() { drawU.Encode(drawBuf[:]) }); allocs != 0 {
		t.Errorf("PBRDrawUniform.Encode allocs = %v, want 0", allocs)
	}

	// Short slice with sufficient capacity still panics.
	for _, fn := range []func(){
		func() { u.Encode(make([]byte, 0, render3d.LightUniformSize)) },
		func() { drawU.Encode(make([]byte, 0, render3d.PBRDrawUniformSize)) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("Encode on zero-length slice did not panic")
				}
			}()
			fn()
		}()
	}
}

func TestEvaluatePBRPhysics(t *testing.T) {
	n := math3d.Vec3{Y: 1}
	v := math3d.Vec3{Y: 1}
	lUp := math3d.Vec3{Y: 1}
	lDown := math3d.Vec3{Y: -1}

	redPlastic := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.8, Y: 0.1, Z: 0.1}, Metallic: 0, Roughness: 0.5}
	gold := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 1.0, Y: 0.76, Z: 0.33}, Metallic: 1, Roughness: 0.5}

	// Back-facing light or view yields zero BRDF.
	if got := render3d.EvaluatePBR(n, v, lDown, redPlastic); got != math3d.Vec3Zero() {
		t.Errorf("back-facing light BRDF = %v, want zero", got)
	}
	if got := render3d.EvaluatePBR(n, lDown, lUp, redPlastic); got != math3d.Vec3Zero() {
		t.Errorf("back-facing view BRDF = %v, want zero", got)
	}

	// At normal incidence (N = V = L = H): D = 1/(pi*a^2), V = 0.25, F = F0.
	const pi = float32(3.14159265)
	a := float32(0.5 * 0.5)
	specFactor := float32(0.25) / (pi * a * a)

	// Pure conductor has zero diffuse and specular tinted by BaseColor.
	gotGold := render3d.EvaluatePBR(n, v, lUp, gold)
	wantGold := gold.BaseColor.Scale(specFactor)
	if !gotGold.ApproxEqual(wantGold, 1e-4) {
		t.Errorf("gold normal incidence BRDF = %v, want %v", gotGold, wantGold)
	}

	// Pure dielectric has F0 = 0.04 white specular + (1 - 0.04) * BaseColor / pi diffuse.
	gotPlastic := render3d.EvaluatePBR(n, v, lUp, redPlastic)
	wantPlastic := math3d.Vec3{
		X: 0.96*redPlastic.BaseColor.X/pi + 0.04*specFactor,
		Y: 0.96*redPlastic.BaseColor.Y/pi + 0.04*specFactor,
		Z: 0.96*redPlastic.BaseColor.Z/pi + 0.04*specFactor,
	}
	if !gotPlastic.ApproxEqual(wantPlastic, 1e-4) {
		t.Errorf("dielectric normal incidence BRDF = %v, want %v", gotPlastic, wantPlastic)
	}

	// Roughness sweep: smoother material produces a stronger on-axis peak than rougher material.
	smooth := render3d.EvaluatePBR(n, v, lUp, render3d.PBRMaterial{BaseColor: math3d.Vec3One(), Metallic: 0, Roughness: 0.15})
	rough := render3d.EvaluatePBR(n, v, lUp, render3d.PBRMaterial{BaseColor: math3d.Vec3One(), Metallic: 0, Roughness: 0.85})
	if smooth.X <= rough.X*5 {
		t.Errorf("smooth on-axis specular %v not dramatically stronger than rough %v", smooth.X, rough.X)
	}

	// Directional light traveling straight down (-Y) illuminates +Y surface; (+Y) travel does not.
	downRig := render3d.LightRig{Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{Y: -1}, Color: math3d.Vec3One(), Intensity: 2}}}
	upRig := render3d.LightRig{Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{Y: 1}, Color: math3d.Vec3One(), Intensity: 2}}}
	litDown := render3d.ShadePoint(math3d.Vec3Zero(), n, v, redPlastic, downRig)
	litUp := render3d.ShadePoint(math3d.Vec3Zero(), n, v, redPlastic, upRig)
	if litDown.X <= 0 || litUp != math3d.Vec3Zero() {
		t.Errorf("directional sign check failed: down=%v up=%v", litDown, litUp)
	}

	// Linear light superposition: directional + point == sum of individual evaluations.
	ptRig := render3d.LightRig{Point: []render3d.PointLight{{Position: math3d.Vec3{X: 1, Y: 2, Z: 1}, Color: math3d.Vec3{X: 0.2, Y: 0.6, Z: 1}, Intensity: 6, Range: 10}}}
	bothRig := render3d.LightRig{Directional: downRig.Directional, Point: ptRig.Point}
	sumDir := render3d.ShadePoint(math3d.Vec3Zero(), n, v, redPlastic, downRig)
	sumPt := render3d.ShadePoint(math3d.Vec3Zero(), n, v, redPlastic, ptRig)
	sumBoth := render3d.ShadePoint(math3d.Vec3Zero(), n, v, redPlastic, bothRig)
	if !sumBoth.ApproxEqual(sumDir.Add(sumPt), 1e-5) {
		t.Errorf("linear superposition failed: both=%v, dir+pt=%v", sumBoth, sumDir.Add(sumPt))
	}

	// IEC 61966-2-1 linear -> sRGB transfer checks.
	for _, tc := range []struct {
		in, want float32
	}{
		{-0.5, 0},
		{0, 0},
		{0.001, 0.01292},
		{0.0031308, 0.040449936},
		{0.18, 0.46135613},
		{0.5, 0.7353569},
		{1.0, 1.0},
		{2.5, 1.0},
	} {
		got := render3d.LinearToSRGBChannel(tc.in)
		if !nearF32(got, tc.want, 1e-4) {
			t.Errorf("LinearToSRGBChannel(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestLitPBRMatchesCPUReference renders a front-facing quad on NSGo's software GPU device using
// LitPBRWGSL and verifies that rendered pixels match CPU LinearToSRGB(ShadePoint(...)) across
// directional, point (inside and outside range), dielectric, metallic, and combined lights.
func TestLitPBRMatchesCPUReference(t *testing.T) {
	const w, h = 64, 64
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

	lightBuf, err := render3d.NewLightBuffer(nil, dev, render3d.LightRig{}, render3d.LightBufferOptions{Label: "ref"})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	drawBuf, err := render3d.NewPBRDrawBuffer(nil, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), render3d.PBRMaterial{BaseColor: math3d.Vec3One(), Roughness: 0.5}), false)
	if err != nil {
		t.Fatalf("NewPBRDrawBuffer: %v", err)
	}
	t.Cleanup(func() { _ = drawBuf.Close() })

	pipe, err := render3d.NewLitPBRPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), gpu.FormatDepth24Plus)
	if err != nil {
		t.Fatalf("NewLitPBRPipeline: %v", err)
	}

	// Large front-facing quad on Z = 0 covering the entire 64x64 viewport, normal = +Z.
	plane := render3d.Mesh{
		Positions: []math3d.Vec3{{X: -3, Y: -3, Z: 0}, {X: 3, Y: -3, Z: 0}, {X: 3, Y: 3, Z: 0}, {X: -3, Y: 3, Z: 0}},
		Normals:   []math3d.Vec3{{Z: 1}, {Z: 1}, {Z: 1}, {Z: 1}},
		UVs:       []math3d.Vec2{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1}},
		Indices:   []uint32{0, 1, 2, 0, 2, 3},
	}
	gm, err := render3d.UploadMesh(nil, dev, plane, render3d.MeshUpload{Label: "plane"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = gm.Close() })

	renderOne := func(mat render3d.PBRMaterial, rig render3d.LightRig) *image.RGBA {
		if err := lightBuf.Update(dev, rig); err != nil {
			t.Fatalf("lightBuf.Update: %v", err)
		}
		drawBuf.Write(dev, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), mat))
		enc := dev.BeginEncoder()
		pass := enc.BeginRenderPass(gpu.RenderPassDesc{
			ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
			DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
		})
		pass.SetPipeline(pipe)
		pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
		lightBuf.Bind(pass)
		pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
		gm.Draw(pass)
		pass.End()
		dev.Submit(enc.Finish())
		sr.WaitIdle()
		if err := sr.LastError(); err != nil {
			t.Fatalf("software renderer: %v", err)
		}
		return sr.Image()
	}

	cases := []struct {
		name string
		mat  render3d.PBRMaterial
		rig  render3d.LightRig
	}{
		{
			name: "directional dielectric",
			mat:  render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.75, Y: 0.25, Z: 0.15}, Metallic: 0, Roughness: 0.45},
			rig: render3d.LightRig{
				Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.1, Y: 0.15, Z: 0.25}, Intensity: 0.2},
				Directional: []render3d.DirectionalLight{
					{Direction: math3d.Vec3{X: -0.4, Y: -0.5, Z: -1}, Color: math3d.Vec3{X: 1, Y: 0.95, Z: 0.9}, Intensity: 2.2},
				},
			},
		},
		{
			name: "point inside range on metal",
			mat:  render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.95, Y: 0.72, Z: 0.30}, Metallic: 1, Roughness: 0.35},
			rig: render3d.LightRig{
				Point: []render3d.PointLight{
					{Position: math3d.Vec3{X: 0.25, Y: 0.25, Z: 1.5}, Color: math3d.Vec3{X: 0.8, Y: 0.9, Z: 1.0}, Intensity: 4.5, Range: 4.0},
				},
			},
		},
		{
			name: "point outside range leaves ambient only",
			mat:  render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}, Metallic: 0, Roughness: 0.7},
			rig: render3d.LightRig{
				Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.2, Y: 0.3, Z: 0.4}, Intensity: 0.5},
				Point: []render3d.PointLight{
					{Position: math3d.Vec3{X: 0, Y: 0, Z: 5}, Color: math3d.Vec3{X: 1, Y: 0, Z: 0}, Intensity: 50, Range: 3.0},
				},
			},
		},
		{
			name: "combined directional + two point lights + emissive",
			mat:  render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.6, Y: 0.6, Z: 0.65}, Metallic: 0.3, Roughness: 0.55, Emissive: math3d.Vec3{X: 0.02, Y: 0.01, Z: 0.0}},
			rig: render3d.LightRig{
				Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.15, Y: 0.18, Z: 0.25}, Intensity: 0.2},
				Directional: []render3d.DirectionalLight{
					{Direction: math3d.Vec3{X: 0.5, Y: -0.6, Z: -0.8}, Color: math3d.Vec3{X: 1, Y: 0.9, Z: 0.8}, Intensity: 1.5},
				},
				Point: []render3d.PointLight{
					{Position: math3d.Vec3{X: -0.6, Y: 0.3, Z: 1.2}, Color: math3d.Vec3{X: 1, Y: 0.3, Z: 0.1}, Intensity: 2.5, Range: 5},
					{Position: math3d.Vec3{X: 0.7, Y: -0.4, Z: 1.1}, Color: math3d.Vec3{X: 0.1, Y: 0.6, Z: 1}, Intensity: 2.5, Range: 5},
				},
			},
		},
	}

	// Sample 5 pixels across the quad by unprojecting pixel centers onto the Z = 0 plane.
	samplePixels := [][2]int{{16, 16}, {48, 16}, {32, 32}, {16, 48}, {48, 48}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img := renderOne(tc.mat, tc.rig)
			for _, px := range samplePixels {
				ray := cam.ScreenRay(float32(px[0])+0.5, float32(px[1])+0.5, w, h)
				// Intersect ray with plane Z = 0.
				tHit := -ray.Origin.Z / ray.Dir.Z
				worldPos := ray.At(tHit)
				viewDir := cam.Pos.Sub(worldPos).Norm()
				lin := render3d.ShadePoint(worldPos, math3d.Vec3{Z: 1}, viewDir, tc.mat, tc.rig)
				srgb := render3d.LinearToSRGB(lin)
				want := color.RGBA{
					R: uint8(math.Round(float64(srgb.X) * 255)),
					G: uint8(math.Round(float64(srgb.Y) * 255)),
					B: uint8(math.Round(float64(srgb.Z) * 255)),
					A: 255,
				}
				wantPixel(t, img, px[0], px[1], want)
			}
		})
	}
}

type litSceneObject struct {
	mesh  *render3d.GPUMesh
	model math3d.Mat4
	mat   render3d.PBRMaterial
}

func renderLitScene(t *testing.T, width, height int, cam *scene3d.Camera, rig render3d.LightRig, clearCol core.Color, buildObjects func(dev *gpu.Device) []litSceneObject) *image.RGBA {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })
	camBuf.WriteCamera(cam)

	lightBuf, err := render3d.NewLightBuffer(nil, dev, rig, render3d.LightBufferOptions{Label: "scene-lights"})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	t.Cleanup(func() { _ = lightBuf.Close() })

	objects := buildObjects(dev)
	drawBufs := make([]*render3d.PBRDrawBuffer, len(objects))
	var sharedLayout *gpu.BindGroupLayout
	for i, obj := range objects {
		db, err := render3d.NewPBRDrawBuffer(nil, dev, sharedLayout, render3d.NewPBRDrawUniform(obj.model, obj.mat), false)
		if err != nil {
			t.Fatalf("NewPBRDrawBuffer[%d]: %v", i, err)
		}
		if sharedLayout == nil {
			sharedLayout = db.Layout()
		}
		drawBufs[i] = db
		t.Cleanup(func() { _ = db.Close() })
	}

	pipe, err := render3d.NewLitPBRPipeline(nil, dev, camBuf.Layout(), lightBuf.Layout(), sharedLayout, gpu.FormatDepth24Plus)
	if err != nil {
		t.Fatalf("NewLitPBRPipeline: %v", err)
	}

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "lit-pbr-reference",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: clearCol}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	lightBuf.Bind(pass)
	for i, obj := range objects {
		pass.SetBindGroup(render3d.PBRDrawGroup, drawBufs[i].BindGroup())
		obj.mesh.Draw(pass)
	}
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	return sr.Image()
}

func TestLitPBRSceneMatchesReference(t *testing.T) {
	const w, h = 160, 120
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 2.4, Z: 6.2}, math3d.DegToRad(45), float32(w)/float32(h), 0.1, 40)
	cam.LookAt(math3d.Vec3{Y: 0.7}, math3d.Vec3Up())

	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{
			Color:     math3d.Vec3{X: 0.16, Y: 0.22, Z: 0.32},
			Intensity: 0.22,
		},
		Directional: []render3d.DirectionalLight{
			{
				Direction: math3d.Vec3{X: 0.55, Y: -0.8, Z: -0.45},
				Color:     math3d.Vec3{X: 1.0, Y: 0.96, Z: 0.90},
				Intensity: 2.6,
			},
		},
		Point: []render3d.PointLight{
			{
				Position:  math3d.Vec3{X: 1.6, Y: 1.15, Z: 1.5},
				Color:     math3d.Vec3{X: 0.25, Y: 0.72, Z: 1.0},
				Intensity: 9.0,
				Range:     6.5,
			},
		},
	}

	img := renderLitScene(t, w, h, cam, rig, core.RGB(0x0c1018), func(dev *gpu.Device) []litSceneObject {
		sphere, err := render3d.UploadMesh(nil, dev, uvSphereMesh(1, 24, 16), render3d.MeshUpload{Label: "sphere"})
		if err != nil {
			t.Fatalf("UploadMesh sphere: %v", err)
		}
		t.Cleanup(func() { _ = sphere.Close() })

		box, err := render3d.UploadMesh(nil, dev, unitBoxMesh(), render3d.MeshUpload{Label: "box"})
		if err != nil {
			t.Fatalf("UploadMesh box: %v", err)
		}
		t.Cleanup(func() { _ = box.Close() })

		ground, err := render3d.UploadMesh(nil, dev, lightGroundPlaneMesh(4.5), render3d.MeshUpload{Label: "ground"})
		if err != nil {
			t.Fatalf("UploadMesh ground: %v", err)
		}
		t.Cleanup(func() { _ = ground.Close() })

		return []litSceneObject{
			{
				mesh:  ground,
				model: math3d.Mat4Identity(),
				mat:   render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.42, Y: 0.44, Z: 0.48}, Metallic: 0, Roughness: 0.85},
			},
			{
				// Smooth red ceramic dielectric sphere (left).
				mesh:  sphere,
				model: math3d.Translate(math3d.Vec3{X: -2.0, Y: 0.85, Z: 0.0}).Mul(math3d.Scale(math3d.Vec3{X: 0.85, Y: 0.85, Z: 0.85})),
				mat:   render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.82, Y: 0.14, Z: 0.12}, Metallic: 0.0, Roughness: 0.22},
			},
			{
				// Polished gold metallic sphere (center).
				mesh:  sphere,
				model: math3d.Translate(math3d.Vec3{X: 0.0, Y: 0.85, Z: -0.2}).Mul(math3d.Scale(math3d.Vec3{X: 0.85, Y: 0.85, Z: 0.85})),
				mat:   render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.98, Y: 0.76, Z: 0.30}, Metallic: 1.0, Roughness: 0.25},
			},
			{
				// Rough brushed copper sphere (right, near cyan point light).
				mesh:  sphere,
				model: math3d.Translate(math3d.Vec3{X: 2.0, Y: 0.85, Z: 0.1}).Mul(math3d.Scale(math3d.Vec3{X: 0.85, Y: 0.85, Z: 0.85})),
				mat:   render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.88, Y: 0.46, Z: 0.32}, Metallic: 0.85, Roughness: 0.65},
			},
			{
				// Matte slate box behind spheres.
				mesh:  box,
				model: math3d.Translate(math3d.Vec3{X: -1.1, Y: 0.5, Z: -2.0}).Mul(math3d.RotateY(math3d.DegToRad(24))),
				mat:   render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.24, Y: 0.56, Z: 0.54}, Metallic: 0.0, Roughness: 0.9},
			},
		}
	})

	assertReference(t, "lights_pbr_scene", img)

	// Semantic shading checks on the red ceramic sphere: upper-left lit side must be clearly brighter
	// than lower-right shadowed side.
	litPt, _, ok1 := cam.WorldToScreen(math3d.Vec3{X: -2.4, Y: 1.3, Z: 0.65}, w, h)
	darkPt, _, ok2 := cam.WorldToScreen(math3d.Vec3{X: -1.5, Y: 0.35, Z: 0.5}, w, h)
	if !ok1 || !ok2 {
		t.Fatalf("sphere test points projected outside screen: %v %v", litPt, darkPt)
	}
	litPx := img.RGBAAt(int(litPt.X), int(litPt.Y))
	darkPx := img.RGBAAt(int(darkPt.X), int(darkPt.Y))
	if int(litPx.R)+int(litPx.G)+int(litPx.B) <= int(darkPx.R)+int(darkPx.G)+int(darkPx.B)+40 {
		t.Errorf("lit sphere pixel %v not significantly brighter than shadowed pixel %v", litPx, darkPx)
	}

	// Ground under the cyan point light must be brighter and bluer than distant left ground.
	nearPt, _, okNear := cam.WorldToScreen(math3d.Vec3{X: 1.6, Y: 0, Z: 1.5}, w, h)
	farPt, _, okFar := cam.WorldToScreen(math3d.Vec3{X: -2.8, Y: 0, Z: 1.5}, w, h)
	if !okNear || !okFar {
		t.Fatalf("ground test points projected outside screen: %v %v", nearPt, farPt)
	}
	nearPx := img.RGBAAt(int(nearPt.X), int(nearPt.Y))
	farPx := img.RGBAAt(int(farPt.X), int(farPt.Y))
	if int(nearPx.B) <= int(farPx.B)+25 {
		t.Errorf("ground under cyan point light %v not noticeably bluer than distant ground %v", nearPx, farPx)
	}
}

func TestPointLightRangeSceneMatchesReference(t *testing.T) {
	const w, h = 160, 120
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 3.2, Z: 5.8}, math3d.DegToRad(45), float32(w)/float32(h), 0.1, 40)
	cam.LookAt(math3d.Vec3{Y: 0.3}, math3d.Vec3Up())

	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{
			Color:     math3d.Vec3{X: 0.08, Y: 0.09, Z: 0.12},
			Intensity: 0.2,
		},
		Point: []render3d.PointLight{
			{
				Position:  math3d.Vec3{X: -1.8, Y: 0.9, Z: 0.6},
				Color:     math3d.Vec3{X: 1.0, Y: 0.48, Z: 0.12},
				Intensity: 7.5,
				Range:     3.2,
			},
			{
				Position:  math3d.Vec3{X: 1.8, Y: 0.9, Z: 0.6},
				Color:     math3d.Vec3{X: 0.15, Y: 0.65, Z: 1.0},
				Intensity: 7.5,
				Range:     3.2,
			},
		},
	}

	img := renderLitScene(t, w, h, cam, rig, core.RGB(0x06080d), func(dev *gpu.Device) []litSceneObject {
		sphere, err := render3d.UploadMesh(nil, dev, uvSphereMesh(0.7, 24, 16), render3d.MeshUpload{Label: "sphere"})
		if err != nil {
			t.Fatalf("UploadMesh sphere: %v", err)
		}
		t.Cleanup(func() { _ = sphere.Close() })

		ground, err := render3d.UploadMesh(nil, dev, lightGroundPlaneMesh(4.5), render3d.MeshUpload{Label: "ground"})
		if err != nil {
			t.Fatalf("UploadMesh ground: %v", err)
		}
		t.Cleanup(func() { _ = ground.Close() })

		neutral := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.72, Y: 0.72, Z: 0.72}, Metallic: 0, Roughness: 0.45}
		return []litSceneObject{
			{mesh: ground, model: math3d.Mat4Identity(), mat: render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.6, Y: 0.6, Z: 0.6}, Metallic: 0, Roughness: 0.8}},
			{mesh: sphere, model: math3d.Translate(math3d.Vec3{X: -1.1, Y: 0.7, Z: -0.3}), mat: neutral},
			{mesh: sphere, model: math3d.Translate(math3d.Vec3{X: 1.1, Y: 0.7, Z: -0.3}), mat: neutral},
		}
	})

	assertReference(t, "lights_point_falloff", img)

	// Left pool is warm (R > B), right pool is cool (B > R), and distant ground beyond both ranges falls back to ambient.
	leftPt, _, okL := cam.WorldToScreen(math3d.Vec3{X: -1.8, Y: 0, Z: 0.6}, w, h)
	rightPt, _, okR := cam.WorldToScreen(math3d.Vec3{X: 1.8, Y: 0, Z: 0.6}, w, h)
	outsidePt, _, okO := cam.WorldToScreen(math3d.Vec3{X: 0, Y: 0, Z: -3.8}, w, h)
	if !okL || !okR || !okO {
		t.Fatalf("point falloff ground points outside screen: %v %v %v", leftPt, rightPt, outsidePt)
	}
	leftPx := img.RGBAAt(int(leftPt.X), int(leftPt.Y))
	rightPx := img.RGBAAt(int(rightPt.X), int(rightPt.Y))
	outsidePx := img.RGBAAt(int(outsidePt.X), int(outsidePt.Y))
	if leftPx.R <= leftPx.B+40 {
		t.Errorf("left ground pool %v not warm-tinted", leftPx)
	}
	if rightPx.B <= rightPx.R+40 {
		t.Errorf("right ground pool %v not cool-tinted", rightPx)
	}
	if outsidePx.R >= leftPx.R/2 || outsidePx.B >= rightPx.B/2 {
		t.Errorf("outside-range ground %v did not fall off compared to pools left=%v right=%v", outsidePx, leftPx, rightPx)
	}
}

func litViewApp(t *testing.T, retained bool, initialRig render3d.LightRig, opts ...nsgotest.Option) (*nsgotest.App, **render3d.GPULightBuffer, **gpu.Device) {
	t.Helper()
	const size = 64
	var (
		camBuf   *render3d.CameraBuffer
		lightBuf *render3d.GPULightBuffer
		drawBuf  *render3d.PBRDrawBuffer
		mesh     *render3d.GPUMesh
		pipe     *gpu.Pipeline
		lastDev  *gpu.Device
	)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(50), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	mat := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.85, Y: 0.85, Z: 0.85}, Metallic: 0, Roughness: 0.4}

	opts = append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				lastDev = dev
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, dev)
				if err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				camBuf.WriteCamera(cam)

				if lightBuf == nil || !retained {
					lightBuf, err = render3d.NewLightBuffer(cx, dev, initialRig, render3d.LightBufferOptions{Label: "view-lights", Retained: retained})
					if err != nil {
						t.Fatalf("NewLightBuffer: %v", err)
					}
				}
				if drawBuf == nil || !retained {
					drawBuf, err = render3d.NewPBRDrawBuffer(cx, dev, nil, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), mat), retained)
					if err != nil {
						t.Fatalf("NewPBRDrawBuffer: %v", err)
					}
				}
				if mesh == nil || !retained {
					mesh, err = render3d.UploadMesh(cx, dev, uvSphereMesh(0.95, 20, 14), render3d.MeshUpload{Label: "sphere", Retained: retained})
					if err != nil {
						t.Fatalf("UploadMesh: %v", err)
					}
				}
				pipe, err = render3d.NewLitPBRPipeline(cx, dev, camBuf.Layout(), lightBuf.Layout(), drawBuf.Layout(), gpu.FormatDepth24Plus)
				if err != nil {
					t.Fatalf("NewLitPBRPipeline: %v", err)
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				lastDev = frame.Device()
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x0a0d14))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
				lightBuf.Bind(pass)
				pass.SetBindGroup(render3d.PBRDrawGroup, drawBuf.BindGroup())
				mesh.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, opts...)
	return app, &lightBuf, &lastDev
}

func TestLitSceneDrawsInGPUView(t *testing.T) {
	redRig := render3d.LightRig{
		Directional: []render3d.DirectionalLight{
			{Direction: math3d.Vec3{X: 0, Y: 0, Z: -1}, Color: math3d.Vec3{X: 1, Y: 0.1, Z: 0.1}, Intensity: 2.5},
		},
	}
	blueRig := render3d.LightRig{
		Point: []render3d.PointLight{
			{Position: math3d.Vec3{X: 0, Y: 0, Z: 2.2}, Color: math3d.Vec3{X: 0.1, Y: 0.2, Z: 1}, Intensity: 6.0, Range: 6.0},
		},
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
			app, lightsp, devp := litViewApp(t, false, redRig, p.opts...)
			app.Advance(16 * time.Millisecond)
			img1, err := app.GPUReadPixels("id:viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels frame 1: %v", err)
			}
			center1 := img1.RGBAAt(32, 32)
			if center1.R <= center1.B+80 {
				t.Fatalf("red directional light center pixel = %v, want strongly red", center1)
			}
			// Update lights dynamically to a blue point light and verify the next frame reflects it.
			app.Post(func() {
				if err := (*lightsp).Update(*devp, blueRig); err != nil {
					t.Fatalf("Update(blueRig): %v", err)
				}
			})
			app.Advance(16 * time.Millisecond)
			img2, err := app.GPUReadPixels("id:viewport")
			if err != nil {
				t.Fatalf("GPUReadPixels frame 2: %v", err)
			}
			center2 := img2.RGBAAt(32, 32)
			if center2.B <= center2.R+80 {
				t.Fatalf("blue point light center pixel = %v, want strongly blue", center2)
			}
		})
	}
}

func TestRetainedLightBufferSurvivesDeviceLoss(t *testing.T) {
	greenRig := render3d.LightRig{
		Directional: []render3d.DirectionalLight{
			{Direction: math3d.Vec3{Z: -1}, Color: math3d.Vec3{X: 0.1, Y: 1.0, Z: 0.15}, Intensity: 2.2},
		},
	}
	warmRig := render3d.LightRig{
		Directional: []render3d.DirectionalLight{
			{Direction: math3d.Vec3{Z: -1}, Color: math3d.Vec3{X: 1.0, Y: 0.2, Z: 0.05}, Intensity: 2.2},
		},
	}

	app, lightsp, devp := litViewApp(t, true, greenRig)
	app.Advance(16 * time.Millisecond)
	first := *lightsp
	imgBefore, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	beforePx := imgBefore.RGBAAt(32, 32)
	if beforePx.G <= beforePx.R+80 {
		t.Fatalf("initial green lit pixel = %v, want strongly green", beforePx)
	}

	app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
	if first.Valid() {
		t.Fatal("light buffer still valid while device is lost")
	}

	app.RestoreGPUDevice("id:viewport")
	if *lightsp != first || !first.Valid() {
		t.Fatalf("retained light buffer not restored: same=%v valid=%v", *lightsp == first, first.Valid())
	}

	// Draw without re-uploading lights: shadow bytes must restore the exact green light.
	app.Advance(16 * time.Millisecond)
	imgRestored, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	wantPixel(t, imgRestored, 32, 32, beforePx)

	// Now update the restored retained buffer using the new device and verify the new light renders.
	app.Post(func() {
		if err := first.Update(*devp, warmRig); err != nil {
			t.Fatalf("Update after restore: %v", err)
		}
	})
	app.Advance(16 * time.Millisecond)
	imgUpdated, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels after post-restore update: %v", err)
	}
	updatedPx := imgUpdated.RGBAAt(32, 32)
	if updatedPx.R <= updatedPx.G+80 {
		t.Fatalf("post-restore updated pixel = %v, want strongly red", updatedPx)
	}
}

func TestLightBufferLifecycleAndScopeDisposal(t *testing.T) {
	// Error guards on nil device and malformed rigs.
	if _, err := render3d.NewLightBuffer(nil, nil, render3d.LightRig{}, render3d.LightBufferOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewLightBuffer(nil dev) = %v, want ErrDeviceLost", err)
	}
	badRig := render3d.LightRig{Directional: []render3d.DirectionalLight{{Intensity: 1}}}
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	if _, err := render3d.NewLightBuffer(nil, sr.Device(), badRig, render3d.LightBufferOptions{}); !errors.Is(err, render3d.ErrInvalidLight) {
		t.Fatalf("NewLightBuffer(bad rig) = %v, want ErrInvalidLight", err)
	}

	lb, err := render3d.NewLightBuffer(nil, sr.Device(), render3d.LightRig{}, render3d.LightBufferOptions{})
	if err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	if !lb.Valid() || lb.ByteSize() != render3d.LightUniformSize {
		t.Fatalf("valid=%v byteSize=%d, want true and %d", lb.Valid(), lb.ByteSize(), render3d.LightUniformSize)
	}
	if err := lb.Update(sr.Device(), badRig); !errors.Is(err, render3d.ErrInvalidLight) {
		t.Fatalf("Update(bad rig) = %v, want ErrInvalidLight", err)
	}
	if err := lb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := lb.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if lb.Valid() {
		t.Error("closed light buffer still valid")
	}
	if err := lb.Update(sr.Device(), render3d.LightRig{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("Update(closed) = %v, want ErrDeviceLost", err)
	}

	var nilBuf *render3d.GPULightBuffer
	if nilBuf.Valid() || nilBuf.ByteSize() != 0 || nilBuf.Buffer() != nil || nilBuf.Layout() != nil || nilBuf.BindGroup() != nil || nilBuf.Close() != nil {
		t.Error("nil GPULightBuffer methods must be safe")
	}

	// Scope disposal automatically releases the buffer, layout, and bind group.
	var (
		scoped *render3d.GPULightBuffer
		show   *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(dev *gpu.Device) {
					var err error
					scoped, err = render3d.NewLightBuffer(cx, dev, render3d.LightRig{}, render3d.LightBufferOptions{})
					if err != nil {
						t.Fatalf("scoped NewLightBuffer: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())
	if !scoped.Valid() {
		t.Fatal("scoped light buffer not valid while mounted")
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if scoped.Valid() {
		t.Error("scoped light buffer still valid after scope disposal")
	}
}

func BenchmarkLightRigAppendUniformData(b *testing.B) {
	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.2, Y: 0.25, Z: 0.35}, Intensity: 0.2},
		Directional: []render3d.DirectionalLight{
			{Direction: math3d.Vec3{X: 0.5, Y: -1, Z: -0.5}, Color: math3d.Vec3One(), Intensity: 2.5},
		},
		Point: []render3d.PointLight{
			{Position: math3d.Vec3{X: 2, Y: 1, Z: 2}, Color: math3d.Vec3{X: 1, Y: 0.5, Z: 0.2}, Intensity: 8, Range: 10},
			{Position: math3d.Vec3{X: -2, Y: 1, Z: 2}, Color: math3d.Vec3{X: 0.2, Y: 0.5, Z: 1}, Intensity: 8, Range: 10},
		},
	}
	buf := make([]byte, 0, render3d.LightUniformSize)
	b.SetBytes(render3d.LightUniformSize)
	b.ReportAllocs()
	for b.Loop() {
		buf = rig.AppendUniformData(buf[:0])
	}
}

func BenchmarkLightUniformsEncode(b *testing.B) {
	u, _ := render3d.NewLightUniforms(render3d.LightRig{
		Directional: []render3d.DirectionalLight{{Direction: math3d.Vec3{Y: -1}, Color: math3d.Vec3One(), Intensity: 2}},
	})
	var buf [render3d.LightUniformSize]byte
	b.SetBytes(render3d.LightUniformSize)
	b.ReportAllocs()
	for b.Loop() {
		u.Encode(buf[:])
	}
}

func BenchmarkShadePoint(b *testing.B) {
	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.15, Y: 0.2, Z: 0.3}, Intensity: 0.2},
		Directional: []render3d.DirectionalLight{
			{Direction: math3d.Vec3{X: 0.4, Y: -0.8, Z: -0.4}, Color: math3d.Vec3One(), Intensity: 2.5},
		},
		Point: []render3d.PointLight{
			{Position: math3d.Vec3{X: 2, Y: 2, Z: 2}, Color: math3d.Vec3{X: 1, Y: 0.6, Z: 0.3}, Intensity: 10, Range: 8},
			{Position: math3d.Vec3{X: -2, Y: 1, Z: 2}, Color: math3d.Vec3{X: 0.3, Y: 0.7, Z: 1}, Intensity: 10, Range: 8},
		},
	}
	mat := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.7, Y: 0.2, Z: 0.15}, Metallic: 0.2, Roughness: 0.35}
	p := math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}
	n := math3d.Vec3{X: 0.2, Y: 0.9, Z: 0.35}.Norm()
	v := math3d.Vec3{X: 0, Y: 0.3, Z: 0.95}.Norm()
	var sink math3d.Vec3
	b.ReportAllocs()
	for b.Loop() {
		sink = render3d.ShadePoint(p, n, v, mat, rig)
	}
	_ = sink
}

func nearF32(a, b, eps float32) bool {
	return float32(math.Abs(float64(a-b))) <= eps
}

// uvSphereMesh builds an indexed counter-clockwise UV sphere of radius r with outward unit normals.
func uvSphereMesh(r float32, slices, stacks int) render3d.Mesh {
	var m render3d.Mesh
	for y := 0; y <= stacks; y++ {
		v := float64(y) / float64(stacks)
		theta := v * math.Pi
		sinT, cosT := math.Sin(theta), math.Cos(theta)
		for x := 0; x <= slices; x++ {
			u := float64(x) / float64(slices)
			phi := u * 2 * math.Pi
			nx := float32(sinT * math.Cos(phi))
			ny := float32(cosT)
			nz := float32(sinT * math.Sin(phi))
			n := math3d.Vec3{X: nx, Y: ny, Z: nz}
			m.Positions = append(m.Positions, n.Scale(r))
			m.Normals = append(m.Normals, n)
			m.UVs = append(m.UVs, math3d.Vec2{X: float32(u), Y: float32(v)})
		}
	}
	row := uint32(slices + 1)
	for y := uint32(0); y < uint32(stacks); y++ {
		for x := uint32(0); x < uint32(slices); x++ {
			i0 := y*row + x
			i1 := i0 + 1
			i2 := (y+1)*row + x
			i3 := i2 + 1
			if y > 0 {
				m.Indices = append(m.Indices, i0, i1, i2)
			}
			if y < uint32(stacks-1) {
				m.Indices = append(m.Indices, i1, i3, i2)
			}
		}
	}
	return m
}

// unitBoxMesh builds a counter-clockwise 1x1x1 cube centered at the origin with per-face flat normals.
func unitBoxMesh() render3d.Mesh {
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
		corners := [4]math3d.Vec3{
			c.Sub(u).Sub(v),
			c.Add(u).Sub(v),
			c.Add(u).Add(v),
			c.Sub(u).Add(v),
		}
		uvs := [4]math3d.Vec2{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1}}
		for i := range 4 {
			m.Positions = append(m.Positions, corners[i])
			m.Normals = append(m.Normals, f.n)
			m.UVs = append(m.UVs, uvs[i])
		}
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	return m
}

// lightGroundPlaneMesh builds a subdivided counter-clockwise +Y horizontal plane centered at the origin.
func lightGroundPlaneMesh(halfExtent float32) render3d.Mesh {
	const segs = 8
	n := math3d.Vec3{Y: 1}
	var m render3d.Mesh
	for z := 0; z <= segs; z++ {
		fz := float32(z) / float32(segs)
		pz := halfExtent - 2*halfExtent*fz
		for x := 0; x <= segs; x++ {
			fx := float32(x) / float32(segs)
			px := -halfExtent + 2*halfExtent*fx
			m.Positions = append(m.Positions, math3d.Vec3{X: px, Z: pz})
			m.Normals = append(m.Normals, n)
			m.UVs = append(m.UVs, math3d.Vec2{X: fx, Y: fz})
		}
	}
	row := uint32(segs + 1)
	for z := uint32(0); z < segs; z++ {
		for x := uint32(0); x < segs; x++ {
			i := z*row + x
			m.Indices = append(m.Indices, i, i+1, i+row+1, i, i+row+1, i+row)
		}
	}
	return m
}
