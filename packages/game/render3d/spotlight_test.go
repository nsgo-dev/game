package render3d_test

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
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
	spotLightTestWidth  = 160
	spotLightTestHeight = 120
)

func spotLightTestSphereMesh(center math3d.Vec3, radius float32, stacks, slices int) render3d.Mesh {
	var m render3d.Mesh
	for iy := 0; iy <= stacks; iy++ {
		v := float32(iy) / float32(stacks)
		phi := float64(v) * math.Pi
		sinPhi, cosPhi := math.Sincos(phi)
		for ix := 0; ix <= slices; ix++ {
			u := float32(ix) / float32(slices)
			theta := float64(u) * 2 * math.Pi
			sinTheta, cosTheta := math.Sincos(theta)
			nx := float32(sinPhi * cosTheta)
			ny := float32(cosPhi)
			nz := float32(sinPhi * sinTheta)
			n := math3d.Vec3{X: nx, Y: ny, Z: nz}
			m.Positions = append(m.Positions, center.Add(n.Scale(radius)))
			m.Normals = append(m.Normals, n)
			m.UVs = append(m.UVs, math3d.Vec2{X: u, Y: v})
		}
	}
	stride := uint32(slices + 1)
	for iy := 0; iy < stacks; iy++ {
		for ix := 0; ix < slices; ix++ {
			a := uint32(iy)*stride + uint32(ix)
			b := a + stride
			c := b + 1
			d := a + 1
			if iy > 0 {
				m.Indices = append(m.Indices, a, d, b)
			}
			if iy < stacks-1 {
				m.Indices = append(m.Indices, d, c, b)
			}
		}
	}
	return m
}

func spotLightTestPlaneMesh(half float32) render3d.Mesh {
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

func spotLightTestSceneLights() render3d.SpotLightSet {
	warmPos := math3d.Vec3{X: -1.8, Y: 3.6, Z: 1.2}
	warmTarget := math3d.Vec3{X: -1.1, Y: 0.0, Z: 0.1}
	coolPos := math3d.Vec3{X: 2.0, Y: 3.8, Z: 1.0}
	coolTarget := math3d.Vec3{X: 1.1, Y: 0.0, Z: -0.2}
	return render3d.SpotLightSet{
		Ambient: [3]float32{0.015, 0.018, 0.024},
		Lights: []render3d.SpotLight{
			{
				Position:       warmPos,
				Direction:      warmTarget.Sub(warmPos).Norm(),
				Color:          [3]float32{1.0, 0.72, 0.38},
				Intensity:      28.0,
				Range:          9.0,
				InnerConeAngle: math3d.DegToRad(14),
				OuterConeAngle: math3d.DegToRad(26),
			},
			{
				Position:       coolPos,
				Direction:      coolTarget.Sub(coolPos).Norm(),
				Color:          [3]float32{0.38, 0.70, 1.0},
				Intensity:      26.0,
				Range:          10.0,
				InnerConeAngle: math3d.DegToRad(18),
				OuterConeAngle: math3d.DegToRad(34),
			},
		},
	}
}

func spotLightTestSceneCamera(width, height int) *scene3d.Camera {
	cam := scene3d.NewPerspectiveCamera(
		math3d.Vec3{X: 0.0, Y: 3.4, Z: 5.4},
		math3d.DegToRad(48),
		float32(width)/float32(height),
		0.1,
		40.0,
	)
	cam.LookAt(math3d.Vec3{X: 0.0, Y: 0.45, Z: 0.0}, math3d.Vec3Up())
	return cam
}

type spotLightTestScene struct {
	camBuf   *render3d.CameraBuffer
	renderer *render3d.SpotLitRenderer
	meshes   []*render3d.GPUMesh
	mats     []*render3d.GPUSpotMaterial
}

func spotLightTestBuildScene(t *testing.T, cx *core.Scope, dev *gpu.Device, depthFmt gpu.TextureFormat, retained bool) *spotLightTestScene {
	t.Helper()
	camBuf, err := render3d.NewCameraBuffer(cx, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	renderer, err := render3d.NewSpotLitRenderer(cx, dev, render3d.SpotLitRendererOptions{
		Label:        "spot-scene",
		CameraLayout: camBuf.Layout(),
		DepthFormat:  depthFmt,
		Retained:     retained,
	})
	if err != nil {
		t.Fatalf("NewSpotLitRenderer: %v", err)
	}
	if err := renderer.SetLights(spotLightTestSceneLights()); err != nil {
		t.Fatalf("SetLights: %v", err)
	}

	type objSpec struct {
		label string
		mesh  render3d.Mesh
		mat   render3d.SpotMaterial
	}
	specs := []objSpec{
		{
			label: "ground",
			mesh:  spotLightTestPlaneMesh(6.0),
			mat: render3d.SpotMaterial{
				Model:     math3d.Mat4Identity(),
				BaseColor: [3]float32{0.72, 0.72, 0.72},
				Alpha:     1,
				Metallic:  0.0,
				Roughness: 0.65,
			},
		},
		{
			label: "left-sphere",
			mesh:  spotLightTestSphereMesh(math3d.Vec3{X: -1.35, Y: 0.75, Z: 0.25}, 0.75, 14, 24),
			mat: render3d.SpotMaterial{
				Model:     math3d.Mat4Identity(),
				BaseColor: [3]float32{0.82, 0.80, 0.78},
				Alpha:     1,
				Metallic:  0.05,
				Roughness: 0.45,
			},
		},
		{
			label: "right-sphere",
			mesh:  spotLightTestSphereMesh(math3d.Vec3{X: 1.35, Y: 0.75, Z: -0.25}, 0.75, 14, 24),
			mat: render3d.SpotMaterial{
				Model:     math3d.Mat4Identity(),
				BaseColor: [3]float32{0.85, 0.88, 0.92},
				Alpha:     1,
				Metallic:  0.85,
				Roughness: 0.28,
			},
		},
		{
			label: "center-sphere",
			mesh:  spotLightTestSphereMesh(math3d.Vec3{X: 0.0, Y: 0.42, Z: 0.85}, 0.42, 12, 20),
			mat: render3d.SpotMaterial{
				Model:     math3d.Mat4Identity(),
				BaseColor: [3]float32{0.88, 0.86, 0.84},
				Alpha:     1,
				Metallic:  0.15,
				Roughness: 0.35,
			},
		},
	}

	s := &spotLightTestScene{
		camBuf:   camBuf,
		renderer: renderer,
		meshes:   make([]*render3d.GPUMesh, 0, len(specs)),
		mats:     make([]*render3d.GPUSpotMaterial, 0, len(specs)),
	}
	for _, sp := range specs {
		gm, err := render3d.UploadMesh(cx, dev, sp.mesh, render3d.MeshUpload{Label: sp.label, Retained: retained})
		if err != nil {
			t.Fatalf("UploadMesh %s: %v", sp.label, err)
		}
		mat, err := renderer.NewMaterial(sp.mat)
		if err != nil {
			t.Fatalf("NewMaterial %s: %v", sp.label, err)
		}
		s.meshes = append(s.meshes, gm)
		s.mats = append(s.mats, mat)
	}
	return s
}

func (s *spotLightTestScene) draw(pass *gpu.RenderPass) {
	s.renderer.BeginPass(pass, s.camBuf.BindGroup())
	for i := range s.meshes {
		s.renderer.Draw(pass, s.meshes[i], s.mats[i])
	}
}

func (s *spotLightTestScene) close() {
	for _, m := range s.mats {
		_ = m.Close()
	}
	for _, gm := range s.meshes {
		_ = gm.Close()
	}
	_ = s.renderer.Close()
	_ = s.camBuf.Close()
}

func spotLightTestFloatToByte(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	return uint8(math.Round(float64(v * 255)))
}

func spotLightTestAssertGolden(t *testing.T, name string, got *image.RGBA) {
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
		t.Fatalf("read reference %s: %v (UPDATE_GOLDEN=1 writes it)", path, err)
	}
	if !want.Bounds().Eq(got.Bounds()) {
		t.Fatalf("%s: size %v, reference %v", name, got.Bounds(), want.Bounds())
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
		t.Fatalf("%s: %d pixels differ from reference (limit %d); got and diff written to %s", name, far, limit, dir)
	}
}

func TestSpotLightValidation(t *testing.T) {
	valid := render3d.SpotLight{
		Position:       math3d.Vec3{Y: 3},
		Direction:      math3d.Vec3{Y: -1},
		Color:          [3]float32{1, 1, 1},
		Intensity:      10,
		Range:          8,
		InnerConeAngle: math3d.DegToRad(15),
		OuterConeAngle: math3d.DegToRad(30),
	}
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	tests := []struct {
		name string
		mod  func(*render3d.SpotLight)
		ok   bool
	}{
		{"valid default", func(*render3d.SpotLight) {}, true},
		{"infinite range zero", func(l *render3d.SpotLight) { l.Range = 0 }, true},
		{"infinite range negative", func(l *render3d.SpotLight) { l.Range = -1 }, true},
		{"equal inner and outer cone", func(l *render3d.SpotLight) { l.InnerConeAngle = l.OuterConeAngle }, true},
		{"outer cone pi/2", func(l *render3d.SpotLight) { l.OuterConeAngle = float32(math.Pi / 2) }, true},
		{"zero intensity", func(l *render3d.SpotLight) { l.Intensity = 0 }, true},
		{"non-unit direction", func(l *render3d.SpotLight) { l.Direction = math3d.Vec3{Y: -5} }, true},
		{"zero direction", func(l *render3d.SpotLight) { l.Direction = math3d.Vec3Zero() }, false},
		{"nan position", func(l *render3d.SpotLight) { l.Position.X = nan }, false},
		{"inf direction", func(l *render3d.SpotLight) { l.Direction.Y = inf }, false},
		{"negative color", func(l *render3d.SpotLight) { l.Color[1] = -0.1 }, false},
		{"nan color", func(l *render3d.SpotLight) { l.Color[0] = nan }, false},
		{"negative intensity", func(l *render3d.SpotLight) { l.Intensity = -1 }, false},
		{"inf range", func(l *render3d.SpotLight) { l.Range = inf }, false},
		{"negative inner angle", func(l *render3d.SpotLight) { l.InnerConeAngle = -0.01 }, false},
		{"negative outer angle", func(l *render3d.SpotLight) { l.OuterConeAngle = -0.01 }, false},
		{"inner exceeds outer", func(l *render3d.SpotLight) {
			l.InnerConeAngle = math3d.DegToRad(35)
			l.OuterConeAngle = math3d.DegToRad(20)
		}, false},
		{"outer exceeds pi/2", func(l *render3d.SpotLight) { l.OuterConeAngle = float32(math.Pi/2) + 0.01 }, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := valid
			tc.mod(&l)
			err := l.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
			if !tc.ok && !errors.Is(err, render3d.ErrInvalidSpotLight) {
				t.Fatalf("Validate = %v, want ErrInvalidSpotLight", err)
			}
		})
	}

	// SpotLightSet validation: too many lights & invalid ambient.
	tooMany := render3d.SpotLightSet{Lights: make([]render3d.SpotLight, render3d.MaxSpotLights+1)}
	for i := range tooMany.Lights {
		tooMany.Lights[i] = valid
	}
	if err := tooMany.Validate(); !errors.Is(err, render3d.ErrInvalidSpotLight) {
		t.Fatalf("SpotLightSet(Max+1).Validate = %v, want ErrInvalidSpotLight", err)
	}
	badAmb := render3d.SpotLightSet{Ambient: [3]float32{-0.1, 0, 0}}
	if err := badAmb.Validate(); !errors.Is(err, render3d.ErrInvalidSpotLight) {
		t.Fatalf("SpotLightSet(neg ambient).Validate = %v, want ErrInvalidSpotLight", err)
	}
}

func TestSpotLightConeAndRangeAttenuation(t *testing.T) {
	inner := math3d.DegToRad(15)
	outer := math3d.DegToRad(35)
	light := render3d.SpotLight{
		Position:       math3d.Vec3{X: 0, Y: 4, Z: 0},
		Direction:      math3d.Vec3{X: 0, Y: -3, Z: 0}, // intentionally unnormalized
		Color:          [3]float32{1, 0.8, 0.5},
		Intensity:      20,
		Range:          10,
		InnerConeAngle: inner,
		OuterConeAngle: outer,
	}

	// Point on-axis (angle 0): cone attenuation = 1.0.
	if got := light.ConeAttenuation(math3d.Vec3{X: 0, Y: 0, Z: 0}); math.Abs(float64(got-1.0)) > 1e-6 {
		t.Errorf("on-axis ConeAttenuation = %v, want 1.0", got)
	}

	// Point strictly inside inner cone (angle 10 deg): cone attenuation = 1.0.
	pointAtAngle := func(angleRad, dist float32) math3d.Vec3 {
		s, c := math.Sincos(float64(angleRad))
		return math3d.Vec3{
			X: dist * float32(s),
			Y: light.Position.Y - dist*float32(c),
			Z: 0,
		}
	}
	if got := light.ConeAttenuation(pointAtAngle(math3d.DegToRad(10), 4)); math.Abs(float64(got-1.0)) > 1e-6 {
		t.Errorf("inside inner cone attenuation = %v, want 1.0", got)
	}

	// Halfway between inner and outer cone: smooth glTF t^2 falloff strictly in (0, 1).
	midAngle := 0.5 * (inner + outer)
	midGot := light.ConeAttenuation(pointAtAngle(midAngle, 4))
	cosI, cosO, scale, offset := light.ConeParams()
	expectedT := float32(math.Cos(float64(midAngle)))*scale + offset
	expectedMid := expectedT * expectedT
	if midGot <= 0 || midGot >= 1 || math.Abs(float64(midGot-expectedMid)) > 1e-5 {
		t.Errorf("mid-cone attenuation = %v, want %v (cosI=%v cosO=%v)", midGot, expectedMid, cosI, cosO)
	}

	// Monotonic decrease from inner to outer cone.
	prev := float32(1.0)
	for deg := float32(15); deg <= 35; deg += 2.5 {
		v := light.ConeAttenuation(pointAtAngle(math3d.DegToRad(deg), 4))
		if v > prev+1e-6 {
			t.Errorf("cone attenuation not monotonic at %v deg: %v > %v", deg, v, prev)
		}
		prev = v
	}

	// At and outside outer cone: 0.0.
	if got := light.ConeAttenuation(pointAtAngle(outer, 4)); got != 0 {
		t.Errorf("at outer cone attenuation = %v, want 0", got)
	}
	if got := light.ConeAttenuation(pointAtAngle(math3d.DegToRad(50), 4)); got != 0 {
		t.Errorf("outside outer cone attenuation = %v, want 0", got)
	}

	// Behind the light: 0.0.
	if got := light.ConeAttenuation(math3d.Vec3{X: 0, Y: 7, Z: 0}); got != 0 {
		t.Errorf("behind light attenuation = %v, want 0", got)
	}

	// Range windowing: smooth falloff to exact 0 at and beyond Range.
	if got := light.DistanceAttenuation(light.Range); got != 0 {
		t.Errorf("at range attenuation = %v, want 0", got)
	}
	if got := light.DistanceAttenuation(light.Range + 2.5); got != 0 {
		t.Errorf("beyond range attenuation = %v, want 0", got)
	}
	atHalfRange := light.DistanceAttenuation(5.0)
	wantHalfRange := float32(math.Pow(1.0-math.Pow(0.5, 4), 2)) / 25.0
	if math.Abs(float64(atHalfRange-wantHalfRange)) > 1e-6 {
		t.Errorf("half-range attenuation = %v, want %v", atHalfRange, wantHalfRange)
	}

	// Infinite range (Range <= 0): pure inverse-square even far away.
	infLight := light
	infLight.Range = 0
	if got := infLight.DistanceAttenuation(20.0); math.Abs(float64(got-1.0/400.0)) > 1e-6 {
		t.Errorf("infinite range at d=20 = %v, want %v", got, 1.0/400.0)
	}

	// EvaluateSpotLight combines distance, cone, color and intensity identically to normalized direction.
	normLight := light
	normLight.Direction = light.Direction.Norm()
	p := pointAtAngle(midAngle, 4)
	s1 := render3d.EvaluateSpotLight(light, p)
	s2 := render3d.EvaluateSpotLight(normLight, p)
	if s1 != s2 {
		t.Errorf("unnormalized vs normalized EvaluateSpotLight mismatch: %+v != %+v", s1, s2)
	}
	if math.Abs(float64(s1.Attenuation-s1.DistanceAttenuation*s1.ConeAttenuation)) > 1e-6 {
		t.Errorf("Attenuation = %v, want %v * %v", s1.Attenuation, s1.DistanceAttenuation, s1.ConeAttenuation)
	}
}

func TestSpotLightUniformLayoutAndZeroAlloc(t *testing.T) {
	set := spotLightTestSceneLights()
	buf := make([]byte, 0, render3d.SpotLightUniformSize)
	buf = render3d.AppendSpotLightUniforms(buf, set)
	if len(buf) != render3d.SpotLightUniformSize || render3d.SpotLightUniformSize != 400 {
		t.Fatalf("uniform size = %d, want 400", len(buf))
	}

	f32 := func(byteOff int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(buf[byteOff : byteOff+4]))
	}
	if f32(0) != set.Ambient[0] || f32(4) != set.Ambient[1] || f32(8) != set.Ambient[2] || f32(12) != 2 {
		t.Fatalf("header = (%v, %v, %v, %v), want ambient + count 2", f32(0), f32(4), f32(8), f32(12))
	}

	// Light 0 packed fields.
	l0 := set.Lights[0]
	_, _, wantScale, wantOffset := l0.ConeParams()
	wantDir := l0.Direction.Norm()
	if f32(16) != l0.Position.X || f32(20) != l0.Position.Y || f32(24) != l0.Position.Z || math.Abs(float64(f32(28)-1.0/l0.Range)) > 1e-6 {
		t.Fatalf("light 0 pos_range = (%v, %v, %v, %v)", f32(16), f32(20), f32(24), f32(28))
	}
	if math.Abs(float64(f32(32)-wantDir.X)) > 1e-6 || math.Abs(float64(f32(36)-wantDir.Y)) > 1e-6 || math.Abs(float64(f32(40)-wantDir.Z)) > 1e-6 || math.Abs(float64(f32(44)-wantScale)) > 1e-5 {
		t.Fatalf("light 0 dir_scale = (%v, %v, %v, %v)", f32(32), f32(36), f32(40), f32(44))
	}
	if math.Abs(float64(f32(48)-l0.Color[0]*l0.Intensity)) > 1e-4 || math.Abs(float64(f32(60)-wantOffset)) > 1e-5 {
		t.Fatalf("light 0 color_offset = (%v, %v, %v, %v)", f32(48), f32(52), f32(56), f32(60))
	}

	// Unused slot 2 must be zeroed even when reusing a dirty buffer.
	for i := range buf {
		buf[i] = 0xFF
	}
	buf = render3d.AppendSpotLightUniforms(buf[:0], set)
	slot2 := 16 + 2*render3d.SpotLightStride
	for i := slot2; i < len(buf); i++ {
		if buf[i] != 0 {
			t.Fatalf("unused uniform byte %d = 0x%02x, want 0", i, buf[i])
		}
	}

	// Zero-alloc check for AppendSpotLightUniforms and AppendSpotMaterialUniforms.
	allocs := testing.AllocsPerRun(50, func() {
		buf = render3d.AppendSpotLightUniforms(buf[:0], set)
	})
	if allocs != 0 {
		t.Errorf("AppendSpotLightUniforms allocated %v times, want 0", allocs)
	}

	mat := render3d.DefaultSpotMaterial()
	mat.Model = math3d.Scale(math3d.Vec3{X: 2, Y: 0.5, Z: 1})
	matBuf := make([]byte, 0, render3d.SpotMaterialUniformSize)
	matAllocs := testing.AllocsPerRun(50, func() {
		matBuf = render3d.AppendSpotMaterialUniforms(matBuf[:0], mat)
	})
	if matAllocs != 0 {
		t.Errorf("AppendSpotMaterialUniforms allocated %v times, want 0", matAllocs)
	}
	// Check inverse-transpose normal matrix for non-uniform scale (2, 0.5, 1) -> diag(0.5, 2, 1).
	mf := func(off int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(matBuf[off : off+4])) }
	if math.Abs(float64(mf(64)-0.5)) > 1e-6 || math.Abs(float64(mf(84)-2.0)) > 1e-6 || math.Abs(float64(mf(104)-1.0)) > 1e-6 {
		t.Errorf("normal matrix diag = (%v, %v, %v), want (0.5, 2.0, 1.0)", mf(64), mf(84), mf(104))
	}
}

func TestSpotLightGPUParityWithCPUReference(t *testing.T) {
	// Render a flat horizontal plane viewed straight down with an orthographic camera
	// so every pixel (x, y) maps analytically to an exact world point (wx, 0, wz),
	// then verify GPU readback matches SpotToneMapSRGB(EvaluateSpotPBR(...)) across
	// inner cone, penumbra ring, outside outer cone, and beyond range.
	const size = 96
	const halfExtent = 5.0

	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	renderer, err := render3d.NewSpotLitRenderer(nil, dev, render3d.SpotLitRendererOptions{
		Label:        "spot-parity",
		CameraLayout: camBuf.Layout(),
		DepthFormat:  gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewSpotLitRenderer: %v", err)
	}
	t.Cleanup(func() { _ = renderer.Close() })

	lights := render3d.SpotLightSet{
		Ambient: [3]float32{0.02, 0.02, 0.03},
		Lights: []render3d.SpotLight{
			{
				Position:       math3d.Vec3{X: -1.0, Y: 3.0, Z: 0.0},
				Direction:      math3d.Vec3{X: 0, Y: -1, Z: 0},
				Color:          [3]float32{1.0, 0.65, 0.30},
				Intensity:      24.0,
				Range:          5.0,
				InnerConeAngle: math3d.DegToRad(15),
				OuterConeAngle: math3d.DegToRad(35),
			},
			{
				Position:       math3d.Vec3{X: 1.5, Y: 2.5, Z: 0.5},
				Direction:      math3d.Vec3{X: -0.2, Y: -1, Z: 0}.Norm(),
				Color:          [3]float32{0.35, 0.75, 1.0},
				Intensity:      20.0,
				Range:          6.0,
				InnerConeAngle: math3d.DegToRad(18),
				OuterConeAngle: math3d.DegToRad(36),
			},
		},
	}
	if err := renderer.SetLights(lights); err != nil {
		t.Fatalf("SetLights: %v", err)
	}

	planeMat := render3d.SpotMaterial{
		Model:     math3d.Mat4Identity(),
		BaseColor: [3]float32{0.8, 0.8, 0.8},
		Alpha:     1,
		Metallic:  0.1,
		Roughness: 0.4,
	}
	gpuMat, err := renderer.NewMaterial(planeMat)
	if err != nil {
		t.Fatalf("NewMaterial: %v", err)
	}
	t.Cleanup(func() { _ = gpuMat.Close() })

	planeMesh, err := render3d.UploadMesh(nil, dev, spotLightTestPlaneMesh(halfExtent), render3d.MeshUpload{Label: "plane"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = planeMesh.Close() })

	cam := scene3d.NewOrthographicCamera(math3d.Vec3{X: 0, Y: 8, Z: 0}, halfExtent*2, 1.0, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3{Z: -1})
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "spot-parity",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x000000)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	renderer.BeginPass(pass, camBuf.BindGroup())
	renderer.Draw(pass, planeMesh, gpuMat)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	eye := cam.Eye()
	probes := []struct {
		name string
		wx   float32
		wz   float32
	}{
		{"warm inner core", -1.0, 0.0},
		{"warm penumbra ring", -2.4, 0.0},
		{"overlap zone", 0.3, 0.2},
		{"cool inner core", 1.0, 0.5},
		{"outside all cones", -4.2, -4.2},
		{"beyond range corner", 4.5, 4.5},
	}

	for _, pr := range probes {
		s, _, ok := cam.WorldToScreen(math3d.Vec3{X: pr.wx, Y: 0, Z: pr.wz}, size, size)
		if !ok {
			t.Fatalf("%s: projected off-screen", pr.name)
		}
		px, py := int(s.X), int(s.Y)
		// Compute exact world position at pixel center (px+0.5, py+0.5).
		u := (float32(px)+0.5)/float32(size)*2 - 1
		v := 1 - (float32(py)+0.5)/float32(size)*2
		worldPos := math3d.Vec3{X: u * halfExtent, Y: 0, Z: -v * halfExtent}
		viewDir := math3d.Vec3{X: eye.X - worldPos.X*eye.W, Y: eye.Y - worldPos.Y*eye.W, Z: eye.Z - worldPos.Z*eye.W}.Norm()

		lin := render3d.EvaluateSpotPBR(worldPos, math3d.Vec3Up(), viewDir, planeMat, lights)
		srgb := render3d.SpotToneMapSRGB(lin)
		want := color.RGBA{
			R: spotLightTestFloatToByte(srgb[0]),
			G: spotLightTestFloatToByte(srgb[1]),
			B: spotLightTestFloatToByte(srgb[2]),
			A: 255,
		}
		got := img.RGBAAt(px, py)
		diff := func(a, b uint8) int {
			if a > b {
				return int(a - b)
			}
			return int(b - a)
		}
		if diff(got.R, want.R) > 2 || diff(got.G, want.G) > 2 || diff(got.B, want.B) > 2 || got.A != 255 {
			t.Errorf("%s at pixel (%d,%d) world %v: got %v, want %v (CPU parity mismatch)", pr.name, px, py, worldPos, got, want)
		}
	}
}

func TestSpotLightsSceneMatchesReference(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(spotLightTestWidth, spotLightTestHeight, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	scene := spotLightTestBuildScene(t, nil, dev, gpu.FormatDepth24Plus, false)
	t.Cleanup(scene.close)

	cam := spotLightTestSceneCamera(spotLightTestWidth, spotLightTestHeight)
	scene.camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "spot-lights-scene-reference",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x0b0e14)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	scene.draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	spotLightTestAssertGolden(t, "spot_lights_scene", img)

	// Verify semantic lighting invariants directly on the rendered scene:
	sampleWorld := func(p math3d.Vec3) color.RGBA {
		s, _, visible := cam.WorldToScreen(p, spotLightTestWidth, spotLightTestHeight)
		if !visible {
			t.Fatalf("probe %v not visible", p)
		}
		return img.RGBAAt(int(s.X), int(s.Y))
	}

	// Ground under warm spotlight is bright and warm (R > B).
	warmGround := sampleWorld(math3d.Vec3{X: -1.1, Y: 0, Z: 1.4})
	if warmGround.R <= warmGround.B+25 || warmGround.R < 90 {
		t.Errorf("warm spotlight ground pool = %v, want bright warm (R > B+25 and R >= 90)", warmGround)
	}

	// Ground under cool spotlight is bright and cool (B > R).
	coolGround := sampleWorld(math3d.Vec3{X: 1.5, Y: 0, Z: 1.1})
	if coolGround.B <= coolGround.R+25 || coolGround.B < 90 {
		t.Errorf("cool spotlight ground pool = %v, want bright cool (B > R+25 and B >= 90)", coolGround)
	}

	// Far dark corner outside both cones stays near ambient.
	darkCorner := sampleWorld(math3d.Vec3{X: -4.2, Y: 0, Z: -3.2})
	if darkCorner.R > 65 || darkCorner.G > 65 || darkCorner.B > 65 {
		t.Errorf("outside-cone ground corner = %v, want dim ambient (< 65)", darkCorner)
	}
	if warmGround.R <= darkCorner.R+50 || coolGround.B <= darkCorner.B+50 {
		t.Errorf("spot pools not clearly brighter than outside-cone ground: warm=%v cool=%v dark=%v", warmGround, coolGround, darkCorner)
	}
}

func TestSpotLightsDrawInGPUViewProfiles(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	var firstImg *image.RGBA
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			const w, h = 96, 72
			var (
				scene  *spotLightTestScene
				frames int
			)
			cam := spotLightTestSceneCamera(w, h)
			opts := append([]nsgotest.Option{nsgotest.Size(w, h), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "spot-view",
					Style:       core.Style{Width: core.Pt(w), Height: core.Pt(h)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						scene = spotLightTestBuildScene(t, cx, dev, gpu.FormatDepth24Plus, false)
					},
					OnFrame: func(frame *gpu.Frame) {
						frames++
						scene.camBuf.WriteCamera(cam)
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x0b0e14))
						pass.ClearDepth(1)
						scene.draw(pass)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)
			if frames == 0 {
				t.Fatal("OnFrame did not run")
			}
			img, err := app.GPUReadPixels("id:spot-view")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}
			// Confirm warm & cool light pools rendered.
			sWarm, _, _ := cam.WorldToScreen(math3d.Vec3{X: -1.1, Y: 0, Z: 1.4}, w, h)
			sCool, _, _ := cam.WorldToScreen(math3d.Vec3{X: 1.5, Y: 0, Z: 1.1}, w, h)
			wc := img.RGBAAt(int(sWarm.X), int(sWarm.Y))
			cc := img.RGBAAt(int(sCool.X), int(sCool.Y))
			if wc.R <= wc.B+20 || cc.B <= cc.R+20 {
				t.Fatalf("%s profile unexpected spot colors: warm=%v cool=%v", p.name, wc, cc)
			}
			if firstImg == nil {
				firstImg = img
			} else if _, mismatched := gametest.DiffImage(firstImg, img); mismatched != 0 {
				t.Fatalf("NoGPUCompute profile differed from full profile in %d pixels", mismatched)
			}
		})
	}
}

func TestSpotLightsRetainedSurvivesDeviceLoss(t *testing.T) {
	const w, h = 96, 72
	var (
		curDev   *gpu.Device
		camBuf   *render3d.CameraBuffer
		lights   *render3d.GPUSpotLights
		matLay   *gpu.BindGroupLayout
		planeMat *render3d.GPUSpotMaterial
		mesh     *render3d.GPUMesh
		pipe     *gpu.Pipeline
		frames   int
	)
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{Y: 8}, 10, float32(w)/float32(h), 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3{Z: -1})

	set := render3d.SpotLightSet{
		Ambient: [3]float32{0.01, 0.01, 0.01},
		Lights: []render3d.SpotLight{{
			Position:       math3d.Vec3{Y: 3},
			Direction:      math3d.Vec3{Y: -1},
			Color:          [3]float32{1.0, 0.5, 0.2},
			Intensity:      24,
			Range:          8,
			InnerConeAngle: math3d.DegToRad(20),
			OuterConeAngle: math3d.DegToRad(35),
		}},
	}
	mat := render3d.SpotMaterial{
		Model:     math3d.Mat4Identity(),
		BaseColor: [3]float32{0.9, 0.9, 0.9},
		Alpha:     1,
		Metallic:  0,
		Roughness: 0.5,
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "spot-retained",
			Style:       core.Style{Width: core.Pt(w), Height: core.Pt(h)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				curDev = dev
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, dev)
				if err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				// Retained buffers survive device loss, so only create them once.
				if lights == nil {
					lights, err = render3d.NewGPUSpotLights(cx, dev, render3d.GPUSpotLightsOptions{Label: "retained-lights", Retained: true})
					if err != nil {
						t.Fatalf("NewGPUSpotLights: %v", err)
					}
					if err := lights.Update(dev, set); err != nil {
						t.Fatalf("lights.Update: %v", err)
					}
					matLay, err = render3d.SpotMaterialBindingLayout(cx, dev)
					if err != nil {
						t.Fatalf("SpotMaterialBindingLayout: %v", err)
					}
					planeMat, err = render3d.NewGPUSpotMaterial(cx, dev, matLay, mat, render3d.GPUSpotLightsOptions{Label: "retained-mat", Retained: true})
					if err != nil {
						t.Fatalf("NewGPUSpotMaterial: %v", err)
					}
					mesh, err = render3d.UploadMesh(cx, dev, spotLightTestPlaneMesh(5), render3d.MeshUpload{Label: "retained-plane", Retained: true})
					if err != nil {
						t.Fatalf("UploadMesh: %v", err)
					}
				}
				pipe, err = render3d.NewSpotLitPipeline(cx, dev, render3d.SpotLitPipelineOptions{
					Label:          "retained-pipe",
					CameraLayout:   camBuf.Layout(),
					LightsLayout:   lights.Layout(),
					MaterialLayout: matLay,
					DepthFormat:    gpu.FormatDepth24Plus,
				})
				if err != nil {
					t.Fatalf("NewSpotLitPipeline: %v", err)
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				frames++
				camBuf.WriteCamera(cam)
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x000000))
				pass.ClearDepth(1)
				pass.SetPipeline(pipe)
				pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
				pass.SetBindGroup(render3d.SpotLightsGroup, lights.BindGroup())
				pass.SetBindGroup(render3d.SpotMaterialGroup, planeMat.BindGroup())
				mesh.Draw(pass)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(w, h), nsgotest.ManualFrames())

	app.Advance(16 * time.Millisecond)
	if frames == 0 {
		t.Fatal("initial OnFrame did not run")
	}
	before, err := app.GPUReadPixels("id:spot-retained")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	centerBefore := before.RGBAAt(w/2, h/2)
	if centerBefore.R < 100 || centerBefore.R <= centerBefore.B+30 {
		t.Fatalf("center before device loss = %v, want bright warm spot", centerBefore)
	}

	app.TriggerGPUDeviceLost("id:spot-retained", nsgotest.GPULostContextLost, "simulated loss")
	if lights.Valid() || planeMat.Valid() {
		t.Fatal("retained spot light resources still valid while device is lost")
	}

	app.RestoreGPUDevice("id:spot-retained")
	if !lights.Valid() || !planeMat.Valid() {
		t.Fatal("retained spot light resources did not restore after device restore")
	}

	app.Advance(16 * time.Millisecond)
	after, err := app.GPUReadPixels("id:spot-retained")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	if _, mismatched := gametest.DiffImage(before, after); mismatched != 0 {
		t.Fatalf("frame after retained device-loss restore differed in %d pixels", mismatched)
	}

	// Updating the retained light buffer on the restored device must take effect on the next frame.
	coolSet := set
	coolSet.Lights = append([]render3d.SpotLight(nil), set.Lights...)
	coolSet.Lights[0].Color = [3]float32{0.2, 0.5, 1.0}
	app.Post(func() {
		if err := lights.Update(curDev, coolSet); err != nil {
			t.Errorf("post-restore lights.Update: %v", err)
		}
	})
	app.Settle()
	app.Advance(16 * time.Millisecond)
	updated, err := app.GPUReadPixels("id:spot-retained")
	if err != nil {
		t.Fatalf("GPUReadPixels after post-restore update: %v", err)
	}
	centerAfter := updated.RGBAAt(w/2, h/2)
	if centerAfter.B < 100 || centerAfter.B <= centerAfter.R+30 {
		t.Fatalf("center after post-restore Update = %v, want bright cool spot", centerAfter)
	}
}

func TestSpotLightsScopeDisposalReleasesResources(t *testing.T) {
	var (
		lights *render3d.GPUSpotLights
		mat    *render3d.GPUSpotMaterial
		show   *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(dev *gpu.Device) {
					var err error
					lights, err = render3d.NewGPUSpotLights(cx, dev, render3d.GPUSpotLightsOptions{})
					if err != nil {
						t.Fatalf("NewGPUSpotLights: %v", err)
					}
					lay, err := render3d.SpotMaterialBindingLayout(cx, dev)
					if err != nil {
						t.Fatalf("SpotMaterialBindingLayout: %v", err)
					}
					mat, err = render3d.NewGPUSpotMaterial(cx, dev, lay, render3d.DefaultSpotMaterial(), render3d.GPUSpotLightsOptions{})
					if err != nil {
						t.Fatalf("NewGPUSpotMaterial: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())

	if !lights.Valid() || !mat.Valid() {
		t.Fatal("spot light resources not valid while mounted")
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if lights.Valid() || mat.Valid() {
		t.Fatal("spot light resources still valid after scope disposal")
	}
}

func TestSpotLightsLifecycleAndCleanup(t *testing.T) {
	// SpotMaterial validation rejections.
	nan := float32(math.NaN())
	badMats := []render3d.SpotMaterial{
		{Model: math3d.Mat4{nan}},
		{BaseColor: [3]float32{-0.1, 0, 0}},
		{Alpha: 1.5},
		{Metallic: -0.1},
		{Roughness: 1.2},
		{Emissive: [3]float32{0, -1, 0}},
	}
	for i, bm := range badMats {
		if err := bm.Validate(); !errors.Is(err, render3d.ErrInvalidSpotLight) {
			t.Fatalf("badMats[%d].Validate() = %v, want ErrInvalidSpotLight", i, err)
		}
	}

	// Nil device rejections.
	if _, err := render3d.NewGPUSpotLights(nil, nil, render3d.GPUSpotLightsOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewGPUSpotLights(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.SpotMaterialBindingLayout(nil, nil); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("SpotMaterialBindingLayout(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewSpotLitRenderer(nil, nil, render3d.SpotLitRendererOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewSpotLitRenderer(nil dev) = %v, want ErrDeviceLost", err)
	}

	// Nil receiver safety.
	var nilLights *render3d.GPUSpotLights
	var nilMat *render3d.GPUSpotMaterial
	var nilRend *render3d.SpotLitRenderer
	if nilLights.Valid() || nilLights.Count() != 0 || nilLights.ByteSize() != 0 || nilLights.Close() != nil {
		t.Error("nil GPUSpotLights unsafe")
	}
	if nilMat.Valid() || nilMat.ByteSize() != 0 || nilMat.Close() != nil {
		t.Error("nil GPUSpotMaterial unsafe")
	}
	if nilRend.Valid() || nilRend.Close() != nil {
		t.Error("nil SpotLitRenderer unsafe")
	}

	// Live lifecycle, invalid update rejection, and double Close.
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatUndefined)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	rend, err := render3d.NewSpotLitRenderer(nil, dev, render3d.SpotLitRendererOptions{
		CameraLayout: camBuf.Layout(),
	})
	if err != nil {
		t.Fatalf("NewSpotLitRenderer: %v", err)
	}
	if !rend.Valid() || rend.Lights().ByteSize() != render3d.SpotLightUniformSize {
		t.Fatalf("renderer valid=%v lights bytes=%d", rend.Valid(), rend.Lights().ByteSize())
	}
	if err := rend.SetLights(render3d.SpotLightSet{Lights: []render3d.SpotLight{{}}}); !errors.Is(err, render3d.ErrInvalidSpotLight) {
		t.Fatalf("SetLights(zero direction) = %v, want ErrInvalidSpotLight", err)
	}
	if err := rend.SetLights(spotLightTestSceneLights()); err != nil {
		t.Fatalf("SetLights valid: %v", err)
	}
	if rend.Lights().Count() != 2 {
		t.Fatalf("Lights().Count() = %d, want 2", rend.Lights().Count())
	}

	mat, err := rend.NewMaterial(render3d.DefaultSpotMaterial())
	if err != nil {
		t.Fatalf("NewMaterial: %v", err)
	}
	if !mat.Valid() || mat.ByteSize() != render3d.SpotMaterialUniformSize {
		t.Fatalf("mat valid=%v bytes=%d", mat.Valid(), mat.ByteSize())
	}
	if err := mat.Close(); err != nil {
		t.Fatalf("mat.Close: %v", err)
	}
	if err := mat.Close(); err != nil {
		t.Fatalf("second mat.Close: %v", err)
	}
	if mat.Valid() {
		t.Fatal("mat still valid after Close")
	}
	if err := mat.Update(dev, render3d.DefaultSpotMaterial()); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("closed mat.Update = %v, want ErrDeviceLost", err)
	}

	if err := rend.Close(); err != nil {
		t.Fatalf("rend.Close: %v", err)
	}
	if err := rend.Close(); err != nil {
		t.Fatalf("second rend.Close: %v", err)
	}
	if rend.Valid() || rend.Lights().Valid() {
		t.Fatal("renderer still valid after Close")
	}
}
