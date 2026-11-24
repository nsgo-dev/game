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
	localShadowTestWidth  = 128
	localShadowTestHeight = 96
)

var (
	localShadowTestClear  = core.RGB(0x0c1018)
	localShadowTestAlbedo = math3d.Vec3{X: 0.85, Y: 0.85, Z: 0.85}
)

// localShadowTestSubdividedQuad builds a grid of subdivisions x subdivisions
// triangles spanning center +/- uAxis*halfU +/- vAxis*halfV with outward normal.
// Subdividing large floors and walls ensures triangles never straddle w <= 0 of
// a point light's cube-face frustum in the software rasterizer.
func localShadowTestSubdividedQuad(center, uAxis, vAxis, normal math3d.Vec3, halfU, halfV float32, subdiv int) render3d.Mesh {
	var m render3d.Mesh
	n := normal.Norm()
	for iy := range subdiv {
		v0 := -halfV + 2*halfV*float32(iy)/float32(subdiv)
		v1 := -halfV + 2*halfV*float32(iy+1)/float32(subdiv)
		for ix := range subdiv {
			u0 := -halfU + 2*halfU*float32(ix)/float32(subdiv)
			u1 := -halfU + 2*halfU*float32(ix+1)/float32(subdiv)
			p00 := center.Add(uAxis.Scale(u0)).Add(vAxis.Scale(v0))
			p10 := center.Add(uAxis.Scale(u1)).Add(vAxis.Scale(v0))
			p11 := center.Add(uAxis.Scale(u1)).Add(vAxis.Scale(v1))
			p01 := center.Add(uAxis.Scale(u0)).Add(vAxis.Scale(v1))
			base := uint32(len(m.Positions))
			m.Positions = append(m.Positions, p00, p10, p11, p01)
			m.Normals = append(m.Normals, n, n, n, n)
			m.UVs = append(m.UVs,
				math3d.Vec2{X: 0, Y: 0},
				math3d.Vec2{X: 1, Y: 0},
				math3d.Vec2{X: 1, Y: 1},
				math3d.Vec2{X: 0, Y: 1},
			)
			m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
		}
	}
	return m
}

// localShadowTestBoxMesh builds a CCW outward-facing box mesh centered at center
// with half extents half.
func localShadowTestBoxMesh(center, half math3d.Vec3) render3d.Mesh {
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
		c := center.Add(math3d.Vec3{X: f.n.X * half.X, Y: f.n.Y * half.Y, Z: f.n.Z * half.Z})
		u := math3d.Vec3{X: f.u.X * half.X, Y: f.u.Y * half.Y, Z: f.u.Z * half.Z}
		v := math3d.Vec3{X: f.v.X * half.X, Y: f.v.Y * half.Y, Z: f.v.Z * half.Z}
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

func localShadowTestMergeMeshes(meshes ...render3d.Mesh) render3d.Mesh {
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

func localShadowTestReadDepth(t *testing.T, dev *gpu.Device, tex *gpu.Texture, w, h uint32) []float32 {
	t.Helper()
	var raw []byte
	dev.ReadTexture(tex, gpu.TextureRegion{Width: w, Height: h}, func(b []byte, err error) {
		if err != nil {
			t.Fatalf("ReadTexture depth: %v", err)
		}
		raw = append([]byte(nil), b...)
	})
	if len(raw) != int(w*h*4) {
		t.Fatalf("depth readback len = %d, want %d", len(raw), w*h*4)
	}
	return render3d.DecodeDepth32Float(raw)
}

func localShadowTestColorToRGB8(c math3d.Vec3) [3]uint8 {
	conv := func(v float32) uint8 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint8(v*255.0 + 0.5)
	}
	return [3]uint8{conv(c.X), conv(c.Y), conv(c.Z)}
}

func localShadowTestAssertGolden(t *testing.T, name string, got *image.RGBA) {
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
		t.Fatalf("%s: bounds %v, want %v", name, got.Bounds(), want.Bounds())
	}
	const channelTol = 3
	far := 0
	b := got.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g := got.RGBAAt(x, y)
			wr, wg, wb, wa := want.At(x, y).RGBA()
			w := [4]int{int(wr >> 8), int(wg >> 8), int(wb >> 8), int(wa >> 8)}
			for i, c := range [4]int{int(g.R), int(g.G), int(g.B), int(g.A)} {
				if d := c - w[i]; d > channelTol || d < -channelTol {
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
		t.Fatalf("%s: %d pixels differ from golden (limit %d); wrote got & diff to %s", name, far, limit, dir)
	}
}

func localShadowTestSamplePixel(img *image.RGBA, cam *scene3d.Camera, world math3d.Vec3) ([3]uint8, int, int, bool) {
	b := img.Bounds()
	s, _, ok := cam.WorldToScreen(world, float32(b.Dx()), float32(b.Dy()))
	if !ok {
		return [3]uint8{}, 0, 0, false
	}
	x, y := int(s.X), int(s.Y)
	if x < 0 || x >= b.Dx() || y < 0 || y >= b.Dy() {
		return [3]uint8{}, x, y, false
	}
	c := img.RGBAAt(x, y)
	return [3]uint8{c.R, c.G, c.B}, x, y, true
}

func localShadowTestRGBDiff(a, b [3]uint8) int {
	maxD := 0
	for i := range 3 {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		if d > maxD {
			maxD = d
		}
	}
	return maxD
}

func TestSpotShadowConfigAndLightValidation(t *testing.T) {
	def := render3d.DefaultSpotShadowConfig()
	if err := def.Validate(); err != nil {
		t.Fatalf("DefaultSpotShadowConfig.Validate: %v", err)
	}
	if def.Resolution != 1024 || def.Filter != render3d.LocalShadowFilterPCF3x3 {
		t.Fatalf("unexpected default spot config: %+v", def)
	}
	if render3d.LocalShadowFilterHard.String() != "hard" || render3d.LocalShadowFilterPCF3x3.String() != "pcf3x3" {
		t.Fatalf("unexpected filter string names")
	}

	badConfigs := []render3d.SpotShadowConfig{
		{DepthBias: -0.01},
		{DepthBias: float32(math.NaN())},
		{NormalBias: -0.1},
		{NormalBias: float32(math.Inf(1))},
		{Filter: render3d.LocalShadowFilter(99)},
		{CullMode: gpu.CullMode(99)},
	}
	for i, cfg := range badConfigs {
		if err := cfg.Validate(); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
			t.Errorf("badConfigs[%d] = %v, want ErrInvalidLocalShadow", i, err)
		}
	}

	if _, err := render3d.NewSpotShadowMap(nil, nil, def); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("nil device error = %v, want gpu.ErrDeviceLost", err)
	}

	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	if _, err := render3d.NewSpotShadowMap(nil, dev, render3d.SpotShadowConfig{
		Resolution: dev.Limits().MaxTextureDimension2D + 1,
	}); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
		t.Fatalf("oversized spot resolution error = %v, want ErrInvalidLocalShadow", err)
	}

	validLight := render3d.SpotShadowLight{
		Position:       math3d.Vec3{Y: 4},
		Direction:      math3d.Vec3{Y: -1},
		Color:          math3d.Vec3{X: 1, Y: 1, Z: 1},
		Intensity:      10,
		Near:           0.1,
		Range:          12,
		InnerConeAngle: math3d.DegToRad(20),
		OuterConeAngle: math3d.DegToRad(35),
	}
	if err := validLight.Validate(); err != nil {
		t.Fatalf("valid spot light failed: %v", err)
	}

	badLights := []render3d.SpotShadowLight{
		{Position: math3d.Vec3{X: float32(math.NaN())}, Direction: math3d.Vec3{Y: -1}, Near: 0.1, Range: 10, OuterConeAngle: 0.5},
		{Direction: math3d.Vec3Zero(), Near: 0.1, Range: 10, OuterConeAngle: 0.5},
		{Direction: math3d.Vec3{Y: -1}, Color: math3d.Vec3{X: -1}, Near: 0.1, Range: 10, OuterConeAngle: 0.5},
		{Direction: math3d.Vec3{Y: -1}, Ambient: math3d.Vec3{Y: -0.1}, Near: 0.1, Range: 10, OuterConeAngle: 0.5},
		{Direction: math3d.Vec3{Y: -1}, Intensity: -1, Near: 0.1, Range: 10, OuterConeAngle: 0.5},
		{Direction: math3d.Vec3{Y: -1}, Near: 0, Range: 10, OuterConeAngle: 0.5},
		{Direction: math3d.Vec3{Y: -1}, Near: 5, Range: 5, OuterConeAngle: 0.5},
		{Direction: math3d.Vec3{Y: -1}, Near: 0.1, Range: 10, OuterConeAngle: 0},
		{Direction: math3d.Vec3{Y: -1}, Near: 0.1, Range: 10, OuterConeAngle: math.Pi / 2},
		{Direction: math3d.Vec3{Y: -1}, Near: 0.1, Range: 10, InnerConeAngle: 0.6, OuterConeAngle: 0.5},
	}
	for i, l := range badLights {
		if err := l.Validate(); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
			t.Errorf("badLights[%d] = %v, want ErrInvalidLocalShadow", i, err)
		}
	}
}

func TestPointShadowConfigAndLightValidation(t *testing.T) {
	def := render3d.DefaultPointShadowConfig()
	if err := def.Validate(); err != nil {
		t.Fatalf("DefaultPointShadowConfig.Validate: %v", err)
	}
	aw, ah := def.AtlasSize()
	if aw != 3*512 || ah != 2*512 {
		t.Fatalf("default atlas size = %dx%d, want %dx%d", aw, ah, 3*512, 2*512)
	}

	badConfigs := []render3d.PointShadowConfig{
		{DepthBias: -0.01},
		{NormalBias: float32(math.NaN())},
		{Filter: render3d.LocalShadowFilter(42)},
		{CullMode: gpu.CullMode(42)},
	}
	for i, cfg := range badConfigs {
		if err := cfg.Validate(); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
			t.Errorf("badConfigs[%d] = %v, want ErrInvalidLocalShadow", i, err)
		}
	}

	if _, err := render3d.NewPointShadowMap(nil, nil, def); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("nil device error = %v, want gpu.ErrDeviceLost", err)
	}

	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	if _, err := render3d.NewPointShadowMap(nil, dev, render3d.PointShadowConfig{
		FaceResolution: dev.Limits().MaxTextureDimension2D/3 + 1,
	}); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
		t.Fatalf("oversized point atlas error = %v, want ErrInvalidLocalShadow", err)
	}

	badLights := []render3d.PointShadowLight{
		{Position: math3d.Vec3{Z: float32(math.Inf(-1))}, Near: 0.1, Range: 10},
		{Color: math3d.Vec3{Z: -1}, Near: 0.1, Range: 10},
		{Ambient: math3d.Vec3{X: -0.2}, Near: 0.1, Range: 10},
		{Intensity: -2, Near: 0.1, Range: 10},
		{Near: 0, Range: 10},
		{Near: 4, Range: 2},
	}
	for i, l := range badLights {
		if err := l.Validate(); !errors.Is(err, render3d.ErrInvalidLocalShadow) {
			t.Errorf("badPointLights[%d] = %v, want ErrInvalidLocalShadow", i, err)
		}
	}
}

func TestSpotShadowUniformEncodeZeroAlloc(t *testing.T) {
	light := render3d.SpotShadowLight{
		Position:       math3d.Vec3{X: -2, Y: 4, Z: 3},
		Direction:      math3d.Vec3{X: 1, Y: -2, Z: -1},
		Color:          math3d.Vec3{X: 1, Y: 0.9, Z: 0.7},
		Intensity:      12,
		Near:           0.2,
		Range:          15,
		InnerConeAngle: math3d.DegToRad(20),
		OuterConeAngle: math3d.DegToRad(40),
		Ambient:        math3d.Vec3{X: 0.05, Y: 0.06, Z: 0.08},
	}
	cfg := render3d.SpotShadowConfig{
		Resolution: 256,
		DepthBias:  0.002,
		NormalBias: 0.03,
		Filter:     render3d.LocalShadowFilterPCF3x3,
	}
	u := render3d.NewSpotShadowUniform(light, cfg)
	var buf [render3d.SpotShadowUniformSize + 8]byte
	out := u.Encode(buf[:])
	if len(out) != render3d.SpotShadowUniformSize || render3d.SpotShadowUniformSize != 144 {
		t.Fatalf("encoded %d bytes, want 144", len(out))
	}
	f := func(i int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(out[4*i:])) }
	for i := range 16 {
		if f(i) != u.ViewProj[i] {
			t.Fatalf("ViewProj[%d] = %v, want %v", i, f(i), u.ViewProj[i])
		}
	}
	if f(16) != -2 || f(17) != 4 || f(18) != 3 || f(19) != 15 {
		t.Fatalf("position_range mismatch: %v %v %v %v", f(16), f(17), f(18), f(19))
	}
	if f(28) != 0.002 || f(29) != 0.03 || f(30) != 1.0/256.0 || f(31) != 1 {
		t.Fatalf("params mismatch: %v %v %v %v", f(28), f(29), f(30), f(31))
	}
	if f(32) != 0.05 || f(33) != 0.06 || f(34) != 0.08 || f(35) != 0.2 {
		t.Fatalf("ambient_near mismatch: %v %v %v %v", f(32), f(33), f(34), f(35))
	}

	if allocs := testing.AllocsPerRun(100, func() { u.Encode(buf[:]) }); allocs != 0 {
		t.Fatalf("SpotShadowUniform.Encode allocated %v times", allocs)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Encode with short slice did not panic")
			}
		}()
		u.Encode(make([]byte, render3d.SpotShadowUniformSize-1))
	}()
}

func TestPointShadowUniformEncodeZeroAlloc(t *testing.T) {
	light := render3d.PointShadowLight{
		Position:  math3d.Vec3{X: 1, Y: 2, Z: -3},
		Color:     math3d.Vec3{X: 1, Y: 0.8, Z: 0.6},
		Intensity: 10,
		Near:      0.15,
		Range:     14,
		Ambient:   math3d.Vec3{X: 0.04, Y: 0.05, Z: 0.06},
	}
	cfg := render3d.PointShadowConfig{
		FaceResolution: 128,
		DepthBias:      0.001,
		NormalBias:     0.02,
		Filter:         render3d.LocalShadowFilterHard,
	}
	u := render3d.NewPointShadowUniform(light, cfg)
	var buf [render3d.PointShadowUniformSize + 8]byte
	out := u.Encode(buf[:])
	if len(out) != render3d.PointShadowUniformSize || render3d.PointShadowUniformSize != 64 {
		t.Fatalf("encoded %d bytes, want 64", len(out))
	}
	f := func(i int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(out[4*i:])) }
	if f(0) != 1 || f(1) != 2 || f(2) != -3 || f(3) != 14 {
		t.Fatalf("position_range mismatch: %v %v %v %v", f(0), f(1), f(2), f(3))
	}
	if f(4) != 10 || f(5) != 8 || f(6) != 6 || f(7) != 0.15 {
		t.Fatalf("color_near mismatch: %v %v %v %v", f(4), f(5), f(6), f(7))
	}
	if f(8) != 0.001 || f(9) != 0.02 || f(10) != 1.0/128.0 || f(11) != 0 {
		t.Fatalf("params mismatch: %v %v %v %v", f(8), f(9), f(10), f(11))
	}
	if f(12) != 0.04 || f(13) != 0.05 || f(14) != 0.06 || f(15) != 0 {
		t.Fatalf("ambient_pad mismatch: %v %v %v %v", f(12), f(13), f(14), f(15))
	}

	if allocs := testing.AllocsPerRun(100, func() { u.Encode(buf[:]) }); allocs != 0 {
		t.Fatalf("PointShadowUniform.Encode allocated %v times", allocs)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Encode with short slice did not panic")
			}
		}()
		u.Encode(make([]byte, render3d.PointShadowUniformSize-1))
	}()
}

func TestPointShadowFaceProjectionConsistency(t *testing.T) {
	light := render3d.PointShadowLight{
		Position: math3d.Vec3{X: 1.5, Y: -0.75, Z: 2.25},
		Near:     0.2,
		Range:    16.0,
	}
	probes := []struct {
		offset   math3d.Vec3
		wantFace gpu.CubeFace
	}{
		{math3d.Vec3{X: 4.0, Y: 1.2, Z: -1.7}, gpu.CubeFacePosX},
		{math3d.Vec3{X: -3.5, Y: -0.8, Z: 1.4}, gpu.CubeFaceNegX},
		{math3d.Vec3{X: 0.9, Y: 5.0, Z: -1.3}, gpu.CubeFacePosY},
		{math3d.Vec3{X: -1.1, Y: -4.2, Z: 1.8}, gpu.CubeFaceNegY},
		{math3d.Vec3{X: 1.2, Y: -0.6, Z: 3.8}, gpu.CubeFacePosZ},
		{math3d.Vec3{X: -1.3, Y: 1.5, Z: -4.6}, gpu.CubeFaceNegZ},
	}

	for _, tc := range probes {
		face, u, v, axisDist := render3d.PointShadowSelectFace(tc.offset)
		if face != tc.wantFace {
			t.Fatalf("offset %v selected face %d, want %d", tc.offset, face, tc.wantFace)
		}
		world := light.Position.Add(tc.offset)
		vp := light.FaceViewProj(face)
		clip := vp.MulVec4(world.Vec4(1))
		if clip.W <= 0 {
			t.Fatalf("face %d clip.W = %v, want > 0", face, clip.W)
		}
		ndcX, ndcY, ndcZ := clip.X/clip.W, clip.Y/clip.W, clip.Z/clip.W
		gotU := ndcX*0.5 + 0.5
		gotV := 0.5 - ndcY*0.5
		wantZ := (light.Range / (light.Range - light.Near)) * (1.0 - light.Near/axisDist)
		if math.Abs(float64(u-gotU)) > 1e-5 || math.Abs(float64(v-gotV)) > 1e-5 {
			t.Errorf("face %d UV mismatch: select=(%f,%f), proj=(%f,%f)", face, u, v, gotU, gotV)
		}
		if math.Abs(float64(ndcZ-wantZ)) > 1e-5 {
			t.Errorf("face %d depth mismatch: analytic=%f, proj=%f", face, wantZ, ndcZ)
		}
	}
}

func localShadowTestRenderSpotScene(t *testing.T, filter render3d.LocalShadowFilter) (*image.RGBA, []float32, render3d.SpotShadowUniform, *scene3d.Camera) {
	t.Helper()
	const shadowRes = 128

	sr := gpu.NewSoftwareRenderer(localShadowTestWidth, localShadowTestHeight, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	spotShadow, err := render3d.NewSpotShadowMap(nil, dev, render3d.SpotShadowConfig{
		Resolution: shadowRes,
		DepthBias:  0.002,
		NormalBias: 0.025,
		Filter:     filter,
		Label:      "test.spot",
	})
	if err != nil {
		t.Fatalf("NewSpotShadowMap: %v", err)
	}
	t.Cleanup(func() { _ = spotShadow.Close() })

	litPipe, err := render3d.NewSpotShadowLitPipeline(nil, dev, camBuf.Layout(), spotShadow.Layout(), render3d.LocalShadowLitPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		CullMode:    gpu.CullBack,
	})
	if err != nil {
		t.Fatalf("NewSpotShadowLitPipeline: %v", err)
	}
	t.Cleanup(func() { _ = litPipe.Close() })

	floorMesh := localShadowTestSubdividedQuad(
		math3d.Vec3{Y: 0},
		math3d.Vec3{X: 1},
		math3d.Vec3{Z: -1},
		math3d.Vec3Up(),
		4.5, 4.5, 6,
	)
	pillar1 := localShadowTestBoxMesh(math3d.Vec3{X: -0.2, Y: 0.9, Z: 0.1}, math3d.Vec3{X: 0.45, Y: 0.9, Z: 0.45})
	pillar2Local := localShadowTestBoxMesh(math3d.Vec3Zero(), math3d.Vec3{X: 0.35, Y: 0.65, Z: 0.35})
	pillar2Model := math3d.Translate(math3d.Vec3{X: 1.15, Y: 0.65, Z: -0.95})
	pillar2World := localShadowTestBoxMesh(math3d.Vec3{X: 1.15, Y: 0.65, Z: -0.95}, math3d.Vec3{X: 0.35, Y: 0.65, Z: 0.35})

	gpuFloor, err := render3d.UploadMesh(nil, dev, floorMesh, render3d.MeshUpload{Label: "floor"})
	if err != nil {
		t.Fatalf("UploadMesh floor: %v", err)
	}
	t.Cleanup(func() { _ = gpuFloor.Close() })
	gpuPillar1, err := render3d.UploadMesh(nil, dev, pillar1, render3d.MeshUpload{Label: "pillar1"})
	if err != nil {
		t.Fatalf("UploadMesh pillar1: %v", err)
	}
	t.Cleanup(func() { _ = gpuPillar1.Close() })
	gpuPillar2Local, err := render3d.UploadMesh(nil, dev, pillar2Local, render3d.MeshUpload{Label: "pillar2-local"})
	if err != nil {
		t.Fatalf("UploadMesh pillar2Local: %v", err)
	}
	t.Cleanup(func() { _ = gpuPillar2Local.Close() })
	gpuScene, err := render3d.UploadMesh(nil, dev, localShadowTestMergeMeshes(floorMesh, pillar1, pillar2World), render3d.MeshUpload{Label: "spot-scene"})
	if err != nil {
		t.Fatalf("UploadMesh scene: %v", err)
	}
	t.Cleanup(func() { _ = gpuScene.Close() })

	light := render3d.SpotShadowLight{
		Position:       math3d.Vec3{X: -2.4, Y: 3.6, Z: 2.3},
		Direction:      math3d.Vec3{X: 2.5, Y: -3.5, Z: -2.5}.Norm(),
		Color:          math3d.Vec3{X: 1.0, Y: 0.92, Z: 0.78},
		Intensity:      20.0,
		Near:           0.2,
		Range:          14.0,
		InnerConeAngle: math3d.DegToRad(26),
		OuterConeAngle: math3d.DegToRad(44),
		Ambient:        math3d.Vec3{X: 0.08, Y: 0.09, Z: 0.12},
	}
	if err := spotShadow.Update(dev, light); err != nil {
		t.Fatalf("spotShadow.Update: %v", err)
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0.4, Y: 4.8, Z: 5.0}, math3d.DegToRad(48), float32(localShadowTestWidth)/float32(localShadowTestHeight), 0.1, 30)
	cam.LookAt(math3d.Vec3{X: 0.35, Y: 0.3, Z: -0.35}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	// Pass 1: depth-only spot shadow pass with multiple casters using distinct model transforms.
	sp := spotShadow.BeginShadowPass(enc)
	spotShadow.DrawCaster(dev, sp, gpuPillar1, math3d.Mat4Identity())
	spotShadow.DrawCaster(dev, sp, gpuPillar2Local, pillar2Model)
	sp.End()

	// Pass 2: lit main pass sampling spot shadow map.
	mp := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label: "spot-lit-main",
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: localShadowTestClear,
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1.0,
		},
	})
	mp.SetPipeline(litPipe)
	mp.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	mp.SetBindGroup(render3d.LocalShadowGroup, spotShadow.BindGroup())
	gpuScene.Draw(mp)
	mp.End()

	dev.Submit(enc.Finish())
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer error: %v", err)
	}

	depth := localShadowTestReadDepth(t, dev, spotShadow.DepthTexture(), shadowRes, shadowRes)
	return sr.Image(), depth, spotShadow.Uniforms(), cam
}

func TestSpotShadowMatchesReferenceAndGolden(t *testing.T) {
	pcfImg, pcfDepth, pcfUniform, cam := localShadowTestRenderSpotScene(t, render3d.LocalShadowFilterPCF3x3)
	hardImg, hardDepth, hardUniform, _ := localShadowTestRenderSpotScene(t, render3d.LocalShadowFilterHard)

	localShadowTestAssertGolden(t, "local_shadow_spot", pcfImg)

	// Probe analytic visibility & GPU-vs-CPU color agreement across lit, shadowed, and out-of-cone points.
	probes := []struct {
		name      string
		world     math3d.Vec3
		normal    math3d.Vec3
		wantVisLo float32
		wantVisHi float32
	}{
		{"lit-floor-in-front", math3d.Vec3{X: -1.1, Y: 0, Z: 1.0}, math3d.Vec3Up(), 0.95, 1.0},
		{"shadowed-floor-behind-pillar1", math3d.Vec3{X: 0.7, Y: 0, Z: -0.7}, math3d.Vec3Up(), 0.0, 0.05},
		{"shadowed-floor-behind-pillar2", math3d.Vec3{X: 1.7, Y: 0, Z: -1.5}, math3d.Vec3Up(), 0.0, 0.05},
		{"lit-floor-side", math3d.Vec3{X: -1.4, Y: 0, Z: -0.9}, math3d.Vec3Up(), 0.95, 1.0},
	}

	for _, tc := range probes {
		cpuVis := render3d.EvaluateSpotShadow(pcfUniform, pcfDepth, 128, tc.world, tc.normal)
		if cpuVis < tc.wantVisLo || cpuVis > tc.wantVisHi {
			t.Errorf("%s: CPU visibility = %.3f, want in [%.2f, %.2f]", tc.name, cpuVis, tc.wantVisLo, tc.wantVisHi)
		}
		cpuCol := localShadowTestColorToRGB8(render3d.EvaluateSpotLighting(pcfUniform, pcfDepth, 128, tc.world, tc.normal, localShadowTestAlbedo))
		gpuCol, px, py, ok := localShadowTestSamplePixel(pcfImg, cam, tc.world)
		if !ok {
			t.Fatalf("%s: probe %v outside view", tc.name, tc.world)
		}
		if diff := localShadowTestRGBDiff(gpuCol, cpuCol); diff > 10 {
			t.Errorf("%s at (%d,%d): GPU=%v vs CPU=%v (max diff %d)", tc.name, px, py, gpuCol, cpuCol, diff)
		}

		hardVis := render3d.EvaluateSpotShadow(hardUniform, hardDepth, 128, tc.world, tc.normal)
		if hardVis < tc.wantVisLo || hardVis > tc.wantVisHi {
			t.Errorf("%s (hard): CPU visibility = %.3f, want in [%.2f, %.2f]", tc.name, hardVis, tc.wantVisLo, tc.wantVisHi)
		}
		hardGPU, _, _, _ := localShadowTestSamplePixel(hardImg, cam, tc.world)
		hardCPU := localShadowTestColorToRGB8(render3d.EvaluateSpotLighting(hardUniform, hardDepth, 128, tc.world, tc.normal, localShadowTestAlbedo))
		if diff := localShadowTestRGBDiff(hardGPU, hardCPU); diff > 10 {
			t.Errorf("%s (hard): GPU=%v vs CPU=%v (max diff %d)", tc.name, hardGPU, hardCPU, diff)
		}
	}

	// Verify lit floor is markedly brighter than shadowed floor behind both casters.
	litPx, _, _, _ := localShadowTestSamplePixel(pcfImg, cam, math3d.Vec3{X: -1.1, Y: 0, Z: 1.0})
	shad1Px, _, _, _ := localShadowTestSamplePixel(pcfImg, cam, math3d.Vec3{X: 0.7, Y: 0, Z: -0.7})
	shad2Px, _, _, _ := localShadowTestSamplePixel(pcfImg, cam, math3d.Vec3{X: 1.7, Y: 0, Z: -1.5})
	if int(litPx[0]) < int(shad1Px[0])+80 || int(litPx[0]) < int(shad2Px[0])+80 {
		t.Fatalf("expected strong shadow contrast: lit=%v shad1=%v shad2=%v", litPx, shad1Px, shad2Px)
	}

	// Verify Hard vs PCF3x3: scanning across the shadow edge produces intermediate
	// visibility values strictly in (0, 1) for PCF3x3 and only 0 or 1 for Hard.
	pcfPenumbra := 0
	for i := range 60 {
		x := float32(-0.5) + float32(i)*0.04
		pt := math3d.Vec3{X: x, Y: 0, Z: -0.7}
		hv := render3d.EvaluateSpotShadow(hardUniform, hardDepth, 128, pt, math3d.Vec3Up())
		if hv != 0 && hv != 1 {
			t.Fatalf("hard shadow returned non-binary visibility %v at %v", hv, pt)
		}
		pv := render3d.EvaluateSpotShadow(pcfUniform, pcfDepth, 128, pt, math3d.Vec3Up())
		if pv > 0.05 && pv < 0.95 {
			pcfPenumbra++
		}
	}
	if pcfPenumbra == 0 {
		t.Errorf("expected PCF3x3 to produce smooth intermediate penumbra taps along shadow boundary")
	}
}

func localShadowTestRenderPointScene(t *testing.T, filter render3d.LocalShadowFilter) (*image.RGBA, []float32, render3d.PointShadowUniform, *scene3d.Camera) {
	t.Helper()
	const faceRes = 64

	sr := gpu.NewSoftwareRenderer(localShadowTestWidth, localShadowTestHeight, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	pointShadow, err := render3d.NewPointShadowMap(nil, dev, render3d.PointShadowConfig{
		FaceResolution: faceRes,
		DepthBias:      0.0025,
		NormalBias:     0.03,
		Filter:         filter,
		Label:          "test.point",
	})
	if err != nil {
		t.Fatalf("NewPointShadowMap: %v", err)
	}
	t.Cleanup(func() { _ = pointShadow.Close() })

	litPipe, err := render3d.NewPointShadowLitPipeline(nil, dev, camBuf.Layout(), pointShadow.Layout(), render3d.LocalShadowLitPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		CullMode:    gpu.CullBack,
	})
	if err != nil {
		t.Fatalf("NewPointShadowLitPipeline: %v", err)
	}
	t.Cleanup(func() { _ = litPipe.Close() })

	// Open room corner: subdivided floor (y=0), back wall (z=-3.0, normal +Z), right wall (x=3.0, normal -X).
	floor := localShadowTestSubdividedQuad(
		math3d.Vec3{X: 0, Y: 0, Z: 0},
		math3d.Vec3{X: 1},
		math3d.Vec3{Z: -1},
		math3d.Vec3Up(),
		3.2, 3.2, 8,
	)
	backWall := localShadowTestSubdividedQuad(
		math3d.Vec3{X: 0, Y: 2.0, Z: -3.0},
		math3d.Vec3{X: 1},
		math3d.Vec3{Y: 1},
		math3d.Vec3{Z: 1},
		3.2, 2.0, 6,
	)
	rightWall := localShadowTestSubdividedQuad(
		math3d.Vec3{X: 3.0, Y: 2.0, Z: 0},
		math3d.Vec3{Z: 1},
		math3d.Vec3{Y: 1},
		math3d.Vec3{X: -1},
		3.2, 2.0, 6,
	)

	// Three blockers placed along distinct cube faces (+X, -Z, and -Y relative to light at (0, 1.8, 0)).
	blockerPosX := localShadowTestBoxMesh(math3d.Vec3{X: 1.45, Y: 1.45, Z: 0.1}, math3d.Vec3{X: 0.35, Y: 0.55, Z: 0.45})
	blockerNegZ := localShadowTestBoxMesh(math3d.Vec3{X: -0.1, Y: 1.45, Z: -1.45}, math3d.Vec3{X: 0.45, Y: 0.55, Z: 0.35})
	blockerFloorLocal := localShadowTestBoxMesh(math3d.Vec3Zero(), math3d.Vec3{X: 0.45, Y: 0.35, Z: 0.45})
	blockerFloorPos := math3d.Vec3{X: -0.85, Y: 0.75, Z: 0.75}
	blockerFloorWorld := localShadowTestBoxMesh(blockerFloorPos, math3d.Vec3{X: 0.45, Y: 0.35, Z: 0.45})

	gpuBlockerX, err := render3d.UploadMesh(nil, dev, blockerPosX, render3d.MeshUpload{Label: "blocker-x"})
	if err != nil {
		t.Fatalf("UploadMesh blockerX: %v", err)
	}
	t.Cleanup(func() { _ = gpuBlockerX.Close() })
	gpuBlockerZ, err := render3d.UploadMesh(nil, dev, blockerNegZ, render3d.MeshUpload{Label: "blocker-z"})
	if err != nil {
		t.Fatalf("UploadMesh blockerZ: %v", err)
	}
	t.Cleanup(func() { _ = gpuBlockerZ.Close() })
	gpuBlockerFloor, err := render3d.UploadMesh(nil, dev, blockerFloorLocal, render3d.MeshUpload{Label: "blocker-y"})
	if err != nil {
		t.Fatalf("UploadMesh blockerFloor: %v", err)
	}
	t.Cleanup(func() { _ = gpuBlockerFloor.Close() })
	gpuRoom, err := render3d.UploadMesh(nil, dev, localShadowTestMergeMeshes(
		floor, backWall, rightWall, blockerPosX, blockerNegZ, blockerFloorWorld,
	), render3d.MeshUpload{Label: "point-room"})
	if err != nil {
		t.Fatalf("UploadMesh room: %v", err)
	}
	t.Cleanup(func() { _ = gpuRoom.Close() })

	light := render3d.PointShadowLight{
		Position:  math3d.Vec3{X: 0.0, Y: 1.8, Z: 0.0},
		Color:     math3d.Vec3{X: 1.0, Y: 0.94, Z: 0.85},
		Intensity: 14.0,
		Near:      0.15,
		Range:     12.0,
		Ambient:   math3d.Vec3{X: 0.07, Y: 0.08, Z: 0.11},
	}
	if err := pointShadow.Update(dev, light); err != nil {
		t.Fatalf("pointShadow.Update: %v", err)
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: -3.8, Y: 4.3, Z: 4.4}, math3d.DegToRad(50), float32(localShadowTestWidth)/float32(localShadowTestHeight), 0.1, 30)
	cam.LookAt(math3d.Vec3{X: 0.5, Y: 1.0, Z: -0.5}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	// Render all 6 omnidirectional cube faces into the 3x2 depth atlas.
	for face := range gpu.CubeFace(render3d.PointShadowFaceCount) {
		fp := pointShadow.BeginFacePass(enc, face)
		pointShadow.DrawCaster(dev, fp, gpuBlockerX, math3d.Mat4Identity())
		pointShadow.DrawCaster(dev, fp, gpuBlockerZ, math3d.Mat4Identity())
		pointShadow.DrawCaster(dev, fp, gpuBlockerFloor, math3d.Translate(blockerFloorPos))
		fp.End()
	}

	mp := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label: "point-lit-main",
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: localShadowTestClear,
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1.0,
		},
	})
	mp.SetPipeline(litPipe)
	mp.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	mp.SetBindGroup(render3d.LocalShadowGroup, pointShadow.BindGroup())
	gpuRoom.Draw(mp)
	mp.End()

	dev.Submit(enc.Finish())
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer error: %v", err)
	}

	depth := localShadowTestReadDepth(t, dev, pointShadow.DepthTexture(), pointShadow.AtlasWidth(), pointShadow.AtlasHeight())
	return sr.Image(), depth, pointShadow.Uniforms(), cam
}

func TestPointShadowMatchesReferenceAndGolden(t *testing.T) {
	pcfImg, pcfDepth, pcfUniform, cam := localShadowTestRenderPointScene(t, render3d.LocalShadowFilterPCF3x3)
	hardImg, hardDepth, hardUniform, _ := localShadowTestRenderPointScene(t, render3d.LocalShadowFilterHard)

	localShadowTestAssertGolden(t, "local_shadow_point", pcfImg)

	// Verify omnidirectional shadows across +X wall, -Z wall, and -Y floor!
	probes := []struct {
		name      string
		world     math3d.Vec3
		normal    math3d.Vec3
		wantFace  gpu.CubeFace
		wantVisLo float32
		wantVisHi float32
	}{
		{"right-wall-shadow-posX", math3d.Vec3{X: 3.0, Y: 1.1, Z: 0.2}, math3d.Vec3{X: -1}, gpu.CubeFacePosX, 0.0, 0.05},
		{"right-wall-lit-posX", math3d.Vec3{X: 3.0, Y: 2.8, Z: -1.8}, math3d.Vec3{X: -1}, gpu.CubeFacePosX, 0.95, 1.0},
		{"back-wall-shadow-negZ", math3d.Vec3{X: -0.2, Y: 1.1, Z: -3.0}, math3d.Vec3{Z: 1}, gpu.CubeFaceNegZ, 0.0, 0.05},
		{"back-wall-lit-negZ", math3d.Vec3{X: -2.0, Y: 2.6, Z: -3.0}, math3d.Vec3{Z: 1}, gpu.CubeFaceNegZ, 0.95, 1.0},
		{"floor-shadow-negY", math3d.Vec3{X: -1.45, Y: 0.0, Z: 1.3}, math3d.Vec3Up(), gpu.CubeFaceNegY, 0.0, 0.05},
		{"floor-lit-center", math3d.Vec3{X: 0.0, Y: 0.0, Z: 0.0}, math3d.Vec3Up(), gpu.CubeFaceNegY, 0.95, 1.0},
	}

	for _, tc := range probes {
		gotFace, _, _, _ := render3d.PointShadowSelectFace(tc.world.Sub(pcfUniform.Position))
		if gotFace != tc.wantFace {
			t.Errorf("%s: cube face = %d, want %d", tc.name, gotFace, tc.wantFace)
		}
		cpuVis := render3d.EvaluatePointShadow(pcfUniform, pcfDepth, 64, tc.world, tc.normal)
		if cpuVis < tc.wantVisLo || cpuVis > tc.wantVisHi {
			t.Errorf("%s: CPU visibility = %.3f, want in [%.2f, %.2f]", tc.name, cpuVis, tc.wantVisLo, tc.wantVisHi)
		}
		cpuCol := localShadowTestColorToRGB8(render3d.EvaluatePointLighting(pcfUniform, pcfDepth, 64, tc.world, tc.normal, localShadowTestAlbedo))
		gpuCol, px, py, ok := localShadowTestSamplePixel(pcfImg, cam, tc.world)
		if !ok {
			t.Fatalf("%s: probe %v outside view", tc.name, tc.world)
		}
		if diff := localShadowTestRGBDiff(gpuCol, cpuCol); diff > 12 {
			t.Errorf("%s at (%d,%d): GPU=%v vs CPU=%v (max diff %d)", tc.name, px, py, gpuCol, cpuCol, diff)
		}

		hardVis := render3d.EvaluatePointShadow(hardUniform, hardDepth, 64, tc.world, tc.normal)
		if hardVis < tc.wantVisLo || hardVis > tc.wantVisHi {
			t.Errorf("%s (hard): CPU visibility = %.3f, want in [%.2f, %.2f]", tc.name, hardVis, tc.wantVisLo, tc.wantVisHi)
		}
		hardGPU, _, _, _ := localShadowTestSamplePixel(hardImg, cam, tc.world)
		hardCPU := localShadowTestColorToRGB8(render3d.EvaluatePointLighting(hardUniform, hardDepth, 64, tc.world, tc.normal, localShadowTestAlbedo))
		if diff := localShadowTestRGBDiff(hardGPU, hardCPU); diff > 12 {
			t.Errorf("%s (hard): GPU=%v vs CPU=%v (max diff %d)", tc.name, hardGPU, hardCPU, diff)
		}
	}

	// Verify strong visual shadow contrast on every shadowed face (+X wall, -Z wall, -Y floor).
	wallXShad, _, _, _ := localShadowTestSamplePixel(pcfImg, cam, math3d.Vec3{X: 3.0, Y: 1.1, Z: 0.2})
	wallXLit, _, _, _ := localShadowTestSamplePixel(pcfImg, cam, math3d.Vec3{X: 3.0, Y: 2.8, Z: -1.8})
	wallZShad, _, _, _ := localShadowTestSamplePixel(pcfImg, cam, math3d.Vec3{X: -0.2, Y: 1.1, Z: -3.0})
	wallZLit, _, _, _ := localShadowTestSamplePixel(pcfImg, cam, math3d.Vec3{X: -2.0, Y: 2.6, Z: -3.0})
	floorShad, _, _, _ := localShadowTestSamplePixel(pcfImg, cam, math3d.Vec3{X: -1.45, Y: 0.0, Z: 1.3})
	floorLit, _, _, _ := localShadowTestSamplePixel(pcfImg, cam, math3d.Vec3{X: 0.0, Y: 0.0, Z: 0.0})
	if int(wallXLit[0]) < int(wallXShad[0])+40 {
		t.Errorf("+X wall shadow contrast too low: lit=%v shad=%v", wallXLit, wallXShad)
	}
	if int(wallZLit[0]) < int(wallZShad[0])+40 {
		t.Errorf("-Z wall shadow contrast too low: lit=%v shad=%v", wallZLit, wallZShad)
	}
	if int(floorLit[0]) < int(floorShad[0])+80 {
		t.Errorf("-Y floor shadow contrast too low: lit=%v shad=%v", floorLit, floorShad)
	}
}

func localShadowTestStartGPUView(t *testing.T, retained bool, usePoint bool, extraOpts ...nsgotest.Option) (
	*nsgotest.App,
	func() bool,
	*int,
	*scene3d.Camera,
) {
	t.Helper()
	var (
		dev         *gpu.Device
		camBuf      *render3d.CameraBuffer
		spotShadow  *render3d.SpotShadowMap
		pointShadow *render3d.PointShadowMap
		litPipe     *gpu.Pipeline
		gpuCaster   *render3d.GPUMesh
		gpuScene    *render3d.GPUMesh
		frames      int
	)

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 4.5, Z: 4.5}, math3d.DegToRad(50), 1.0, 0.1, 30)
	cam.LookAt(math3d.Vec3{X: 0, Y: 0, Z: 0}, math3d.Vec3Up())

	floorMesh := localShadowTestSubdividedQuad(
		math3d.Vec3{Y: 0},
		math3d.Vec3{X: 1},
		math3d.Vec3{Z: -1},
		math3d.Vec3Up(),
		4.0, 4.0, 6,
	)
	pillarMesh := localShadowTestBoxMesh(math3d.Vec3{X: 0, Y: 0.9, Z: 0}, math3d.Vec3{X: 0.45, Y: 0.9, Z: 0.45})
	sceneMesh := localShadowTestMergeMeshes(floorMesh, pillarMesh)

	spotLight := render3d.SpotShadowLight{
		Position:       math3d.Vec3{X: -2.0, Y: 3.2, Z: 0.0},
		Direction:      math3d.Vec3{X: 2.0, Y: -3.2, Z: 0.0}.Norm(),
		Color:          math3d.Vec3{X: 1, Y: 1, Z: 1},
		Intensity:      18,
		Near:           0.2,
		Range:          12,
		InnerConeAngle: math3d.DegToRad(30),
		OuterConeAngle: math3d.DegToRad(45),
		Ambient:        math3d.Vec3{X: 0.08, Y: 0.08, Z: 0.08},
	}
	pointLight := render3d.PointShadowLight{
		Position:  math3d.Vec3{X: -2.0, Y: 2.0, Z: 0.0},
		Color:     math3d.Vec3{X: 1, Y: 1, Z: 1},
		Intensity: 18,
		Near:      0.15,
		Range:     12,
		Ambient:   math3d.Vec3{X: 0.08, Y: 0.08, Z: 0.08},
	}

	opts := append([]nsgotest.Option{nsgotest.Size(64, 64), nsgotest.ManualFrames()}, extraOpts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "viewport",
			Style:       core.Style{Width: core.Pt(64), Height: core.Pt(64)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(d *gpu.Device) {
				dev = d
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, d)
				if err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				if !retained || gpuCaster == nil {
					gpuCaster, err = render3d.UploadMesh(cx, d, pillarMesh, render3d.MeshUpload{Label: "caster", Retained: retained})
					if err != nil {
						t.Fatalf("UploadMesh caster: %v", err)
					}
					gpuScene, err = render3d.UploadMesh(cx, d, sceneMesh, render3d.MeshUpload{Label: "scene", Retained: retained})
					if err != nil {
						t.Fatalf("UploadMesh scene: %v", err)
					}
				}
				if usePoint {
					if !retained || pointShadow == nil {
						pointShadow, err = render3d.NewPointShadowMap(cx, d, render3d.PointShadowConfig{
							FaceResolution: 32,
							DepthBias:      0.002,
							NormalBias:     0.02,
							Filter:         render3d.LocalShadowFilterPCF3x3,
							Retained:       retained,
						})
						if err != nil {
							t.Fatalf("NewPointShadowMap: %v", err)
						}
					}
					litPipe, err = render3d.NewPointShadowLitPipeline(cx, d, camBuf.Layout(), pointShadow.Layout(), render3d.LocalShadowLitPipelineOptions{
						DepthFormat: gpu.FormatDepth24Plus,
						CullMode:    gpu.CullBack,
					})
					if err != nil {
						t.Fatalf("NewPointShadowLitPipeline: %v", err)
					}
				} else {
					if !retained || spotShadow == nil {
						spotShadow, err = render3d.NewSpotShadowMap(cx, d, render3d.SpotShadowConfig{
							Resolution: 64,
							DepthBias:  0.002,
							NormalBias: 0.02,
							Filter:     render3d.LocalShadowFilterPCF3x3,
							Retained:   retained,
						})
						if err != nil {
							t.Fatalf("NewSpotShadowMap: %v", err)
						}
					}
					litPipe, err = render3d.NewSpotShadowLitPipeline(cx, d, camBuf.Layout(), spotShadow.Layout(), render3d.LocalShadowLitPipelineOptions{
						DepthFormat: gpu.FormatDepth24Plus,
						CullMode:    gpu.CullBack,
					})
					if err != nil {
						t.Fatalf("NewSpotShadowLitPipeline: %v", err)
					}
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				camBuf.WriteCamera(cam)
				if usePoint {
					if err := pointShadow.Update(dev, pointLight); err != nil {
						t.Fatalf("pointShadow.Update: %v", err)
					}
					for face := range gpu.CubeFace(render3d.PointShadowFaceCount) {
						fp := pointShadow.BeginFacePass(frame, face)
						pointShadow.DrawCaster(dev, fp, gpuCaster, math3d.Mat4Identity())
						fp.End()
					}
				} else {
					if err := spotShadow.Update(dev, spotLight); err != nil {
						t.Fatalf("spotShadow.Update: %v", err)
					}
					sp := spotShadow.BeginShadowPass(frame)
					spotShadow.DrawCaster(dev, sp, gpuCaster, math3d.Mat4Identity())
					sp.End()
				}
				mp := frame.BeginRenderPass(gpu.RenderPassDesc{
					ColorAttachments: []gpu.ColorAttachment{{
						LoadOp:     gpu.LoadClear,
						StoreOp:    gpu.StoreStore,
						ClearColor: localShadowTestClear,
					}},
					DepthStencil: &gpu.DepthStencilAttachment{
						DepthLoadOp:  gpu.LoadClear,
						DepthStoreOp: gpu.StoreDiscard,
						ClearDepth:   1.0,
					},
				})
				mp.SetPipeline(litPipe)
				mp.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
				if usePoint {
					mp.SetBindGroup(render3d.LocalShadowGroup, pointShadow.BindGroup())
				} else {
					mp.SetBindGroup(render3d.LocalShadowGroup, spotShadow.BindGroup())
				}
				gpuScene.Draw(mp)
				mp.End()
				frame.Submit()
				frames++
			},
		})
	}, opts...)

	isValid := func() bool {
		if usePoint {
			return pointShadow != nil && pointShadow.Valid()
		}
		return spotShadow != nil && spotShadow.Valid()
	}
	return app, isValid, &frames, cam
}

func localShadowTestAssertViewShadowContrast(t *testing.T, app *nsgotest.App, frames *int, cam *scene3d.Camera) {
	t.Helper()
	before := *frames
	app.Advance(16 * time.Millisecond)
	if *frames <= before {
		t.Fatalf("expected rendered frame, got frames=%d", *frames)
	}
	img, err := app.GPUReadPixels("id:viewport")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	litPx, _, _, okLit := localShadowTestSamplePixel(img, cam, math3d.Vec3{X: -1.2, Y: 0, Z: 0})
	shadPx, _, _, okShad := localShadowTestSamplePixel(img, cam, math3d.Vec3{X: 1.0, Y: 0, Z: 0})
	if !okLit || !okShad {
		t.Fatalf("probes outside view: lit=%v shad=%v", okLit, okShad)
	}
	if int(litPx[0]) < int(shadPx[0])+60 {
		t.Fatalf("expected strong shadow contrast in gpu.View: lit=%v shad=%v", litPx, shadPx)
	}
}

func TestLocalShadowsInGPUViewProfiles(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	for _, p := range profiles {
		for _, kind := range []struct {
			name     string
			usePoint bool
		}{
			{"spot", false},
			{"point", true},
		} {
			t.Run(p.name+"/"+kind.name, func(t *testing.T) {
				app, valid, frames, cam := localShadowTestStartGPUView(t, false, kind.usePoint, p.opts...)
				if !valid() {
					t.Fatal("shadow map not valid after OnReady")
				}
				localShadowTestAssertViewShadowContrast(t, app, frames, cam)
			})
		}
	}
}

func TestLocalShadowsRetainedDeviceLossRestore(t *testing.T) {
	for _, kind := range []struct {
		name     string
		usePoint bool
	}{
		{"spot", false},
		{"point", true},
	} {
		t.Run(kind.name, func(t *testing.T) {
			app, valid, frames, cam := localShadowTestStartGPUView(t, true, kind.usePoint)
			localShadowTestAssertViewShadowContrast(t, app, frames, cam)

			app.TriggerGPUDeviceLost("id:viewport", nsgotest.GPULostContextLost, "context lost")
			if valid() {
				t.Fatalf("retained %s shadow map still valid while device is lost", kind.name)
			}

			app.RestoreGPUDevice("id:viewport")
			if !valid() {
				t.Fatalf("retained %s shadow map not restored after RestoreGPUDevice", kind.name)
			}
			localShadowTestAssertViewShadowContrast(t, app, frames, cam)
		})
	}
}

func TestLocalShadowsCloseAndScopeDisposal(t *testing.T) {
	// Nil safety.
	var nilSpot *render3d.SpotShadowMap
	var nilPoint *render3d.PointShadowMap
	if nilSpot.Valid() || nilPoint.Valid() || nilSpot.ByteSize() != 0 || nilPoint.ByteSize() != 0 {
		t.Fatal("nil shadow map should report invalid and zero byte size")
	}
	if err := nilSpot.Close(); err != nil {
		t.Fatalf("nil spot Close: %v", err)
	}
	if err := nilPoint.Close(); err != nil {
		t.Fatalf("nil point Close: %v", err)
	}

	// Explicit Close idempotency & ByteSize accounting.
	sr := gpu.NewSoftwareRenderer(16, 16, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	spot, err := render3d.NewSpotShadowMap(nil, dev, render3d.SpotShadowConfig{Resolution: 64, MaxCasters: 16})
	if err != nil {
		t.Fatalf("NewSpotShadowMap: %v", err)
	}
	if want := uint64(64*64*4) + render3d.SpotShadowUniformSize + 16*256; spot.ByteSize() != want {
		t.Fatalf("spot ByteSize = %d, want %d", spot.ByteSize(), want)
	}
	if err := spot.Close(); err != nil {
		t.Fatalf("spot Close: %v", err)
	}
	if err := spot.Close(); err != nil {
		t.Fatalf("spot second Close: %v", err)
	}
	if spot.Valid() {
		t.Fatal("spot still valid after Close")
	}

	point, err := render3d.NewPointShadowMap(nil, dev, render3d.PointShadowConfig{FaceResolution: 32, MaxCasters: 24})
	if err != nil {
		t.Fatalf("NewPointShadowMap: %v", err)
	}
	if want := uint64(96*64*4) + render3d.PointShadowUniformSize + 24*256; point.ByteSize() != want {
		t.Fatalf("point ByteSize = %d, want %d", point.ByteSize(), want)
	}
	if err := point.Close(); err != nil {
		t.Fatalf("point Close: %v", err)
	}
	if err := point.Close(); err != nil {
		t.Fatalf("point second Close: %v", err)
	}
	if point.Valid() {
		t.Fatal("point still valid after Close")
	}

	// Scope disposal releases both maps automatically.
	var (
		scopedSpot  *render3d.SpotShadowMap
		scopedPoint *render3d.PointShadowMap
		show        *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(d *gpu.Device) {
					var err error
					scopedSpot, err = render3d.NewSpotShadowMap(cx, d, render3d.SpotShadowConfig{Resolution: 32})
					if err != nil {
						t.Fatalf("NewSpotShadowMap: %v", err)
					}
					scopedPoint, err = render3d.NewPointShadowMap(cx, d, render3d.PointShadowConfig{FaceResolution: 16})
					if err != nil {
						t.Fatalf("NewPointShadowMap: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())

	if !scopedSpot.Valid() || !scopedPoint.Valid() {
		t.Fatal("scoped shadow maps must be valid while mounted")
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if scopedSpot.Valid() || scopedPoint.Valid() {
		t.Fatalf("scoped shadow maps still valid after disposal: spot=%v point=%v", scopedSpot.Valid(), scopedPoint.Valid())
	}
}
