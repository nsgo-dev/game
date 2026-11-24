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

func fogApprox(t *testing.T, label string, got, want, tol float32) {
	t.Helper()
	if math.IsNaN(float64(got)) || math.Abs(float64(got-want)) > float64(tol) {
		t.Errorf("%s = %v, want %v (±%v)", label, got, want, tol)
	}
}

// fogSRGB returns the 8-bit pixel a fogged pipeline writes for the linear color c: sRGB-encoded with [render3d.LinearToSRGB] and rounded.
func fogSRGB(c math3d.Vec3) color.RGBA {
	e := render3d.LinearToSRGB(c)
	toU8 := func(v float32) uint8 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint8(math.Round(float64(v) * 255))
	}
	return color.RGBA{R: toU8(e.X), G: toU8(e.Y), B: toU8(e.Z), A: 255}
}

func fogAssertPixelNear(t *testing.T, img *image.RGBA, x, y int, want color.RGBA, tol uint8) {
	t.Helper()
	got := img.RGBAAt(x, y)
	diff := func(a, b uint8) uint8 {
		if a > b {
			return a - b
		}
		return b - a
	}
	if diff(got.R, want.R) > tol || diff(got.G, want.G) > tol || diff(got.B, want.B) > tol || diff(got.A, want.A) > tol {
		t.Errorf("pixel (%d,%d) = %v, want %v (±%d)", x, y, got, want, tol)
	}
}

func fogAssertGolden(t *testing.T, name string, got *image.RGBA) {
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
	const channelTolerance = 2
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

// fogMakeUnitQuadMesh builds a 2x2 unit quad centered at the origin in the XY plane facing +Z (CCW).
func fogMakeUnitQuadMesh() render3d.Mesh {
	n := math3d.Vec3{Z: 1}
	return render3d.Mesh{
		Positions: []math3d.Vec3{
			{X: -1, Y: -1, Z: 0},
			{X: 1, Y: -1, Z: 0},
			{X: 1, Y: 1, Z: 0},
			{X: -1, Y: 1, Z: 0},
		},
		Normals: []math3d.Vec3{n, n, n, n},
		UVs: []math3d.Vec2{
			{X: 0, Y: 0},
			{X: 1, Y: 0},
			{X: 1, Y: 1},
			{X: 0, Y: 1},
		},
		Indices: []uint32{0, 1, 2, 0, 2, 3},
	}
}

// fogMakeUnitCubeMesh builds an indexed unit cube centered at origin with outward normals.
func fogMakeUnitCubeMesh() render3d.Mesh {
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
			math3d.Vec2{X: 0, Y: 0},
			math3d.Vec2{X: 1, Y: 0},
			math3d.Vec2{X: 1, Y: 1},
			math3d.Vec2{X: 0, Y: 1},
		)
		m.Indices = append(m.Indices, base, base+1, base+2, base, base+2, base+3)
	}
	return m
}

func TestFogModeString(t *testing.T) {
	tests := []struct {
		mode render3d.FogMode
		want string
	}{
		{render3d.FogOff, "off"},
		{render3d.FogLinear, "linear"},
		{render3d.FogExp, "exp"},
		{render3d.FogExp2, "exp2"},
		{render3d.FogMode(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("FogMode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestFogAnalyticalDistanceModes(t *testing.T) {
	cam := math3d.Vec3{X: 0, Y: 2, Z: 0}
	src := math3d.Vec3{X: 0.8, Y: 0.2, Z: 0.1}
	fogCol := math3d.Vec3{X: 0.6, Y: 0.7, Z: 0.9}

	t.Run("off", func(t *testing.T) {
		f := render3d.Fog{Mode: render3d.FogOff, Color: fogCol, Start: 1, End: 10, Density: 0.5}
		for _, d := range []float32{0, 5, 10, 100} {
			p := math3d.Vec3{X: 0, Y: 2, Z: -d}
			fogApprox(t, "factor", f.Factor(cam, p), 0, 1e-6)
			got := f.Apply(src, cam, p)
			if got != src {
				t.Errorf("d=%v Apply = %v, want %v", d, got, src)
			}
		}
	})

	t.Run("linear", func(t *testing.T) {
		f := render3d.Fog{Mode: render3d.FogLinear, Color: fogCol, Start: 10, End: 50}
		cases := []struct {
			d    float32
			want float32
		}{
			{0, 0},
			{5, 0},
			{10, 0},
			{20, 0.25},
			{30, 0.5},
			{40, 0.75},
			{50, 1},
			{100, 1},
		}
		for _, tc := range cases {
			p := math3d.Vec3{X: 0, Y: 2, Z: -tc.d}
			fogApprox(t, "linear", f.Factor(cam, p), tc.want, 1e-6)
			got := f.Apply(src, cam, p)
			want := src.Scale(1 - tc.want).Add(fogCol.Scale(tc.want))
			fogApprox(t, "linear color r", got.X, want.X, 1e-6)
			fogApprox(t, "linear color g", got.Y, want.Y, 1e-6)
			fogApprox(t, "linear color b", got.Z, want.Z, 1e-6)
		}
	})

	t.Run("linear-max-opacity", func(t *testing.T) {
		f := render3d.Fog{Mode: render3d.FogLinear, Color: fogCol, Start: 10, End: 50, MaxOpacity: 0.6}
		fogApprox(t, "before cap", f.Factor(cam, math3d.Vec3{Y: 2, Z: -30}), 0.5, 1e-6)
		fogApprox(t, "at cap", f.Factor(cam, math3d.Vec3{Y: 2, Z: -34}), 0.6, 1e-6)
		fogApprox(t, "past end capped", f.Factor(cam, math3d.Vec3{Y: 2, Z: -90}), 0.6, 1e-6)
	})

	t.Run("exp", func(t *testing.T) {
		const density = 0.08
		f := render3d.Fog{Mode: render3d.FogExp, Color: fogCol, Density: density}
		for _, d := range []float32{0, 2, 5, 10, 25, 60} {
			p := math3d.Vec3{X: 0, Y: 2, Z: -d}
			want := float32(1 - math.Exp(-float64(d)*density))
			fogApprox(t, "exp", f.Factor(cam, p), want, 1e-6)
		}
	})

	t.Run("exp2", func(t *testing.T) {
		const density = 0.05
		f := render3d.Fog{Mode: render3d.FogExp2, Color: fogCol, Density: density}
		for _, d := range []float32{0, 2, 5, 10, 20, 50} {
			p := math3d.Vec3{X: 0, Y: 2, Z: -d}
			x := float64(d) * density
			want := float32(1 - math.Exp(-x*x))
			fogApprox(t, "exp2", f.Factor(cam, p), want, 1e-6)
		}
	})
}

func TestFogAnalyticalHeightAndCombined(t *testing.T) {
	fogCol := math3d.Vec3{X: 0.7, Y: 0.75, Z: 0.85}

	t.Run("horizontal-altitudes", func(t *testing.T) {
		f := render3d.Fog{
			Mode:          render3d.FogOff,
			Color:         fogCol,
			HeightEnabled: true,
			HeightBase:    5,
			HeightFalloff: 0.25,
			HeightDensity: 0.1,
		}
		const dist = 10.0
		// At HeightBase (y=5): sigma = HeightDensity, tau = dist * HeightDensity.
		atBase := f.Factor(math3d.Vec3{Y: 5}, math3d.Vec3{Y: 5, Z: -dist})
		wantBase := float32(1 - math.Exp(-dist*0.1))
		fogApprox(t, "at base", atBase, wantBase, 1e-6)

		// Below HeightBase (y=1): sigma = 0.1 * exp(-0.25*(1-5)) = 0.1 * exp(1).
		below := f.Factor(math3d.Vec3{Y: 1}, math3d.Vec3{Y: 1, Z: -dist})
		wantBelow := float32(1 - math.Exp(-dist*0.1*math.Exp(1)))
		fogApprox(t, "below base", below, wantBelow, 1e-6)

		// Above HeightBase (y=13): sigma = 0.1 * exp(-0.25*(13-5)) = 0.1 * exp(-2).
		above := f.Factor(math3d.Vec3{Y: 13}, math3d.Vec3{Y: 13, Z: -dist})
		wantAbove := float32(1 - math.Exp(-dist*0.1*math.Exp(-2)))
		fogApprox(t, "above base", above, wantAbove, 1e-6)

		if !(below > atBase && atBase > above) {
			t.Fatalf("expected below (%v) > atBase (%v) > above (%v)", below, atBase, above)
		}
	})

	t.Run("vertical-ray", func(t *testing.T) {
		const (
			base    = 2.0
			falloff = 0.5
			density = 0.2
		)
		f := render3d.Fog{
			Color:         fogCol,
			HeightEnabled: true,
			HeightBase:    base,
			HeightFalloff: falloff,
			HeightDensity: density,
		}
		// Exact vertical integral from y=0 to y=10:
		// tau = (density/falloff) * (exp(-falloff*(0-base)) - exp(-falloff*(10-base)))
		tau := (density / falloff) * (math.Exp(-falloff*(0-base)) - math.Exp(-falloff*(10-base)))
		want := float32(1 - math.Exp(-tau))
		up := f.Factor(math3d.Vec3{Y: 0}, math3d.Vec3{Y: 10})
		down := f.Factor(math3d.Vec3{Y: 10}, math3d.Vec3{Y: 0})
		fogApprox(t, "vertical up", up, want, 1e-5)
		fogApprox(t, "vertical down (reciprocity)", down, want, 1e-5)
	})

	t.Run("grazing-ray-continuity", func(t *testing.T) {
		f := render3d.Fog{
			Color:         fogCol,
			HeightEnabled: true,
			HeightBase:    0,
			HeightFalloff: 0.5,
			HeightDensity: 0.12,
		}
		cam := math3d.Vec3{Y: 1}
		for _, dy := range []float64{0, 1e-7, 1e-5, 1.99e-4, 2.01e-4, 1e-3, 1e-2} {
			p := math3d.Vec3{Y: float32(1 + dy), Z: -20}
			d := math.Hypot(20, dy)
			// Exact float64 integral along ray.
			var integral float64
			u := 0.5 * dy
			if u < 1e-8 {
				integral = 1 - 0.5*u + u*u/6
			} else {
				integral = (1 - math.Exp(-u)) / u
			}
			tau := 0.12 * d * math.Exp(-0.5*1.0) * integral
			want := float32(1 - math.Exp(-tau))
			fogApprox(t, "grazing", f.Factor(cam, p), want, 1e-5)
		}
	})

	t.Run("high-camera-looking-down-no-nan", func(t *testing.T) {
		f := render3d.Fog{
			Color:         fogCol,
			HeightEnabled: true,
			HeightBase:    0,
			HeightFalloff: 0.5,
			HeightDensity: 0.1,
		}
		// Previously naive exp(-k*h0)*(1-exp(-k*dy))/(k*dy) produced 0*Inf = NaN here!
		got := f.Factor(math3d.Vec3{Y: 500}, math3d.Vec3{Y: 0, Z: -50})
		if math.IsNaN(float64(got)) || math.IsInf(float64(got), 0) || got <= 0 || got > 1 {
			t.Fatalf("high camera factor = %v, want finite in (0, 1]", got)
		}
		rev := f.Factor(math3d.Vec3{Y: 0, Z: -50}, math3d.Vec3{Y: 500})
		fogApprox(t, "reciprocity", got, rev, 1e-6)
	})

	t.Run("zero-falloff-uniform-height", func(t *testing.T) {
		f := render3d.Fog{
			Color:         fogCol,
			HeightEnabled: true,
			HeightBase:    10,
			HeightFalloff: 0,
			HeightDensity: 0.05,
		}
		want := float32(1 - math.Exp(-15*0.05))
		fogApprox(t, "zero falloff", f.Factor(math3d.Vec3{Y: 0}, math3d.Vec3{Y: 9, Z: -12}), want, 1e-6)
	})

	t.Run("combined-distance-and-height", func(t *testing.T) {
		distOnly := render3d.Fog{Mode: render3d.FogLinear, Color: fogCol, Start: 5, End: 25}
		heightOnly := render3d.Fog{
			Color:         fogCol,
			HeightEnabled: true,
			HeightBase:    2,
			HeightFalloff: 0.3,
			HeightDensity: 0.08,
		}
		combined := render3d.Fog{
			Mode:          render3d.FogLinear,
			Color:         fogCol,
			Start:         5,
			End:           25,
			MaxOpacity:    0.85,
			HeightEnabled: true,
			HeightBase:    2,
			HeightFalloff: 0.3,
			HeightDensity: 0.08,
		}
		cam := math3d.Vec3{Y: 4}
		p := math3d.Vec3{Y: 0, Z: -15}
		fd := distOnly.Factor(cam, p)
		fh := heightOnly.Factor(cam, p)
		want := 1 - (1-fd)*(1-fh)
		if want > 0.85 {
			want = 0.85
		}
		fogApprox(t, "combined", combined.Factor(cam, p), want, 1e-6)
	})
}

func TestFogValidateAndSanitize(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	tests := []struct {
		name string
		fog  render3d.Fog
		ok   bool
	}{
		{"zero off", render3d.Fog{}, true},
		{"linear", render3d.Fog{Mode: render3d.FogLinear, Color: math3d.Vec3{X: 0.5, Y: 0.6, Z: 0.7}, Start: 10, End: 50}, true},
		{"exp", render3d.Fog{Mode: render3d.FogExp, Density: 0.04, MaxOpacity: 0.8}, true},
		{"exp2", render3d.Fog{Mode: render3d.FogExp2, Density: 0.02, MaxOpacity: 1}, true},
		{"height negative base", render3d.Fog{HeightEnabled: true, HeightBase: -25, HeightFalloff: 0.1, HeightDensity: 0.05}, true},
		{"unknown mode", render3d.Fog{Mode: render3d.FogMode(9)}, false},
		{"negative color", render3d.Fog{Color: math3d.Vec3{X: -0.1, Y: 0, Z: 0}}, false},
		{"nan color", render3d.Fog{Color: math3d.Vec3{X: 0, Y: nan, Z: 0}}, false},
		{"negative start", render3d.Fog{Mode: render3d.FogLinear, Start: -1, End: 10}, false},
		{"linear end <= start", render3d.Fog{Mode: render3d.FogLinear, Start: 10, End: 10}, false},
		{"linear end < start", render3d.Fog{Mode: render3d.FogLinear, Start: 20, End: 10}, false},
		{"negative density", render3d.Fog{Mode: render3d.FogExp, Density: -0.01}, false},
		{"inf density", render3d.Fog{Mode: render3d.FogExp, Density: inf}, false},
		{"max opacity > 1", render3d.Fog{MaxOpacity: 1.1}, false},
		{"max opacity < 0", render3d.Fog{MaxOpacity: -0.1}, false},
		{"nan height base", render3d.Fog{HeightEnabled: true, HeightBase: nan}, false},
		{"negative height falloff", render3d.Fog{HeightEnabled: true, HeightFalloff: -0.2}, false},
		{"negative height density", render3d.Fog{HeightEnabled: true, HeightDensity: -0.2}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fog.Validate()
			if tt.ok && err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
			if !tt.ok && !errors.Is(err, render3d.ErrInvalidFog) {
				t.Fatalf("Validate = %v, want ErrInvalidFog", err)
			}
			// Factor and Apply must never return NaN/Inf even on invalid configs.
			f := tt.fog.Factor(math3d.Vec3{}, math3d.Vec3{Z: -10})
			if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) || f < 0 || f > 1 {
				t.Fatalf("Factor on %s = %v, want finite in [0, 1]", tt.name, f)
			}
		})
	}

	// Non-finite positions return 0 safely.
	valid := render3d.Fog{Mode: render3d.FogExp, Density: 0.1}
	if got := valid.Factor(math3d.Vec3{X: nan}, math3d.Vec3{}); got != 0 {
		t.Errorf("Factor(NaN cam) = %v, want 0", got)
	}
	if got := valid.Factor(math3d.Vec3{}, math3d.Vec3{Z: inf}); got != 0 {
		t.Errorf("Factor(Inf pos) = %v, want 0", got)
	}

}

func TestFogUniformLayoutAndZeroAlloc(t *testing.T) {
	if render3d.FogUniformSize != 48 {
		t.Fatalf("FogUniformSize = %d, want 48", render3d.FogUniformSize)
	}
	fog := render3d.Fog{
		Mode:          render3d.FogExp2,
		Color:         math3d.Vec3{X: 0.25, Y: 0.5, Z: 0.75},
		Start:         5,
		End:           40,
		Density:       0.03,
		MaxOpacity:    0.9,
		HeightEnabled: true,
		HeightBase:    -4,
		HeightFalloff: 0.15,
		HeightDensity: 0.06,
	}
	u := fog.Uniforms()
	prefix := []byte{0xAB, 0xCD}
	raw := render3d.AppendFogUniforms(prefix, u)
	if len(raw) != 2+render3d.FogUniformSize || raw[0] != 0xAB || raw[1] != 0xCD {
		t.Fatalf("unexpected encoded length %d or corrupted prefix", len(raw))
	}
	f := func(i int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(raw[2+4*i:]))
	}
	want := [12]float32{
		0.25, 0.5, 0.75, 0.9,
		float32(render3d.FogExp2), 5, 40, 0.03,
		1, -4, 0.15, 0.06,
	}
	for i, w := range want {
		if got := f(i); got != w {
			t.Errorf("float[%d] = %v, want %v", i, got, w)
		}
	}

	// Encode matches AppendFogUniforms byte-for-byte and panics on short slice.
	var encBuf [render3d.FogUniformSize]byte
	enc := u.Encode(encBuf[:])
	for i := range render3d.FogUniformSize {
		if enc[i] != raw[2+i] {
			t.Fatalf("Encode differs from AppendFogUniforms at byte %d", i)
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Encode on short slice did not panic")
			}
		}()
		u.Encode(make([]byte, 0, render3d.FogUniformSize))
	}()

	// Zero-allocation AppendFogUniforms into reusable slice.
	reuse := make([]byte, 0, render3d.FogUniformSize)
	allocs := testing.AllocsPerRun(100, func() {
		reuse = render3d.AppendFogUniforms(reuse[:0], fog.Uniforms())
	})
	if allocs != 0 {
		t.Errorf("AppendFogUniforms allocs = %v, want 0", allocs)
	}
}

func FuzzFogUniformsAndFactor(f *testing.F) {
	f.Add(uint8(1), float32(0.5), float32(0.6), float32(0.7), float32(5), float32(25), float32(0.1), float32(0.9), true, float32(0), float32(0.2), float32(0.05), float32(10), float32(3))
	f.Add(uint8(2), float32(0), float32(0), float32(0), float32(-1), float32(0), float32(0.5), float32(0), false, float32(-50), float32(1), float32(0.2), float32(100), float32(-20))
	f.Fuzz(func(t *testing.T, mode uint8, r, g, b, start, end, density, maxOp float32, hOn bool, hBase, hFall, hDens, dist, y float32) {
		cfg := render3d.Fog{
			Mode:          render3d.FogMode(mode),
			Color:         math3d.Vec3{X: r, Y: g, Z: b},
			Start:         start,
			End:           end,
			Density:       density,
			MaxOpacity:    maxOp,
			HeightEnabled: hOn,
			HeightBase:    hBase,
			HeightFalloff: hFall,
			HeightDensity: hDens,
		}
		if err := cfg.Validate(); err != nil && !errors.Is(err, render3d.ErrInvalidFog) {
			t.Fatalf("Validate error does not wrap ErrInvalidFog: %v", err)
		}
		u := cfg.Uniforms()
		var buf [render3d.FogUniformSize]byte
		out := render3d.AppendFogUniforms(buf[:0], u)
		if len(out) != render3d.FogUniformSize {
			t.Fatalf("len = %d, want %d", len(out), render3d.FogUniformSize)
		}
		for i := range 12 {
			v := math.Float32frombits(binary.LittleEndian.Uint32(out[i*4:]))
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				t.Fatalf("packed float[%d] is non-finite: %v", i, v)
			}
		}
		cam := math3d.Vec3{Y: 2}
		pos := math3d.Vec3{Y: y, Z: -dist}
		fac := cfg.Factor(cam, pos)
		if math.IsNaN(float64(fac)) || math.IsInf(float64(fac), 0) || fac < 0 || fac > 1 {
			t.Fatalf("Factor = %v, want finite in [0, 1]", fac)
		}
		res := cfg.Apply(math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}, cam, pos)
		for i, c := range [...]float32{res.X, res.Y, res.Z} {
			if math.IsNaN(float64(c)) || math.IsInf(float64(c), 0) {
				t.Fatalf("Apply[%d] = %v, want finite", i, c)
			}
		}
	})
}

func BenchmarkAppendFogUniforms(b *testing.B) {
	fog := render3d.Fog{
		Mode:          render3d.FogExp2,
		Color:         math3d.Vec3{X: 0.6, Y: 0.7, Z: 0.8},
		Density:       0.04,
		HeightEnabled: true,
		HeightBase:    2,
		HeightFalloff: 0.2,
		HeightDensity: 0.05,
	}
	buf := make([]byte, 0, render3d.FogUniformSize)
	b.SetBytes(render3d.FogUniformSize)
	b.ReportAllocs()
	for b.Loop() {
		buf = render3d.AppendFogUniforms(buf[:0], fog.Uniforms())
	}
}

func BenchmarkFogFactor(b *testing.B) {
	fog := render3d.Fog{
		Mode:          render3d.FogExp,
		Color:         math3d.Vec3{X: 0.6, Y: 0.7, Z: 0.8},
		Density:       0.04,
		HeightEnabled: true,
		HeightBase:    2,
		HeightFalloff: 0.2,
		HeightDensity: 0.05,
	}
	cam := math3d.Vec3{Y: 5}
	pos := math3d.Vec3{X: 3, Y: 1, Z: -18}
	var sink float32
	b.ReportAllocs()
	for b.Loop() {
		sink += fog.Factor(cam, pos)
	}
	if sink < 0 {
		b.Fatal("unreachable")
	}
}

// fogTestPanel describes one flat emissive quad placed at a known world position facing +Z.
type fogTestPanel struct {
	center math3d.Vec3
	scale  math3d.Vec3
	color  math3d.Vec3
}

// fogPanelDraw returns the per-draw uniform for p: a black, unlit-by-default surface whose only radiance is its emissive color, so with an empty [render3d.LightRig] the lit color equals p.color exactly.
func fogPanelDraw(p fogTestPanel) render3d.PBRDrawUniform {
	model := math3d.Translate(p.center).Mul(math3d.Scale(p.scale))
	return render3d.NewPBRDrawUniform(model, render3d.PBRMaterial{Roughness: 1, Emissive: p.color})
}

// fogScene holds everything that draws panels through [render3d.NewLitPBRFogPipeline] on one device.
type fogScene struct {
	cam   *render3d.CameraBuffer
	light *render3d.GPULightBuffer
	fog   *render3d.GPUFogBuffer
	pipe  *gpu.Pipeline
	quad  *render3d.GPUMesh
	draws []*render3d.PBRDrawBuffer
}

// newFogScene creates the scene's GPU objects on dev, owned by cx. A non-nil fogBuf is reused instead of creating one from fog.
func newFogScene(t *testing.T, cx *core.Scope, dev *gpu.Device, fog render3d.Fog, fogBuf *render3d.GPUFogBuffer, rig render3d.LightRig, mesh render3d.Mesh, panels []fogTestPanel) *fogScene {
	t.Helper()
	var err error
	s := &fogScene{fog: fogBuf}
	if s.cam, err = render3d.NewCameraBuffer(cx, dev); err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	if s.light, err = render3d.NewLightBuffer(cx, dev, rig, render3d.LightBufferOptions{Label: "fog-test"}); err != nil {
		t.Fatalf("NewLightBuffer: %v", err)
	}
	if s.fog == nil {
		if s.fog, err = render3d.NewGPUFogBuffer(cx, dev, fog, render3d.FogBufferOptions{Label: "fog-test"}); err != nil {
			t.Fatalf("NewGPUFogBuffer: %v", err)
		}
	}
	var shared *gpu.BindGroupLayout
	for i, p := range panels {
		d, err := render3d.NewPBRDrawBuffer(cx, dev, shared, fogPanelDraw(p), false)
		if err != nil {
			t.Fatalf("NewPBRDrawBuffer %d: %v", i, err)
		}
		shared = d.Layout()
		s.draws = append(s.draws, d)
	}
	if s.pipe, err = render3d.NewLitPBRFogPipeline(cx, dev, s.cam.Layout(), s.light.Layout(), shared, s.fog.Layout(), gpu.FormatDepth24Plus); err != nil {
		t.Fatalf("NewLitPBRFogPipeline: %v", err)
	}
	if s.quad, err = render3d.UploadMesh(cx, dev, mesh, render3d.MeshUpload{Label: "fog-test"}); err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	return s
}

func (s *fogScene) record(pass *gpu.RenderPass, cam *scene3d.Camera) {
	s.cam.WriteCamera(cam)
	pass.SetPipeline(s.pipe)
	pass.SetBindGroup(render3d.CameraGroup, s.cam.BindGroup())
	s.light.Bind(pass)
	s.fog.Bind(pass)
	for _, d := range s.draws {
		pass.SetBindGroup(render3d.PBRDrawGroup, d.BindGroup())
		s.quad.Draw(pass)
	}
}

// fogRenderPanels draws panels with fog through NewLitPBRFogPipeline on the software GPU device and returns the image.
func fogRenderPanels(t *testing.T, w, h int, cam *scene3d.Camera, fog render3d.Fog, panels []fogTestPanel, clear core.Color) *image.RGBA {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	var cx *core.Scope // nil: the objects live as long as the software renderer
	scene := newFogScene(t, cx, dev, fog, nil, render3d.LightRig{}, fogMakeUnitQuadMesh(), panels)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "fog-test",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: clear}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	scene.record(pass, cam)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	return sr.Image()
}

func fogPanelPixel(t *testing.T, cam *scene3d.Camera, p math3d.Vec3, w, h int) (int, int) {
	t.Helper()
	s, _, vis := cam.WorldToScreen(p, float32(w), float32(h))
	if !vis {
		t.Fatalf("panel center %v not visible", p)
	}
	return int(math.Round(float64(s.X))), int(math.Round(float64(s.Y)))
}

func TestFogGPUDistanceModesMatchReference(t *testing.T) {
	const (
		w = 160
		h = 64
	)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 0, Z: 0}, math3d.DegToRad(50), float32(w)/float32(h), 0.1, 100)
	cam.LookAt(math3d.Vec3{Z: -10}, math3d.Vec3Up())

	// Four flat panels at exact set Z distances (4, 12, 20, 32), placed side-by-side in screen space so every panel's center projects cleanly onto a distinct pixel with no overlap.
	baseCol := math3d.Vec3{X: 0.9, Y: 0.18, Z: 0.12}
	fogCol := math3d.Vec3{X: 0.62, Y: 0.72, Z: 0.86}
	panels := []fogTestPanel{
		{center: math3d.Vec3{X: -2.0, Y: 0, Z: -4}, scale: math3d.Vec3{X: 0.45, Y: 1.0, Z: 1}, color: baseCol},
		{center: math3d.Vec3{X: -2.0, Y: 0, Z: -12}, scale: math3d.Vec3{X: 1.35, Y: 3.0, Z: 1}, color: baseCol},
		{center: math3d.Vec3{X: 3.3, Y: 0, Z: -20}, scale: math3d.Vec3{X: 2.25, Y: 5.0, Z: 1}, color: baseCol},
		{center: math3d.Vec3{X: 16.0, Y: 0, Z: -32}, scale: math3d.Vec3{X: 3.6, Y: 8.0, Z: 1}, color: baseCol},
	}

	modes := []struct {
		name   string
		fog    render3d.Fog
		golden string
	}{
		{name: "off", fog: render3d.Fog{Mode: render3d.FogOff, Color: fogCol, Start: 8, End: 24}},
		{name: "linear", fog: render3d.Fog{Mode: render3d.FogLinear, Color: fogCol, Start: 8, End: 24}, golden: "fog_linear_distances"},
		{name: "linear-max-opacity", fog: render3d.Fog{Mode: render3d.FogLinear, Color: fogCol, Start: 8, End: 24, MaxOpacity: 0.65}},
		{name: "exp", fog: render3d.Fog{Mode: render3d.FogExp, Color: fogCol, Density: 0.06}},
		{name: "exp2", fog: render3d.Fog{Mode: render3d.FogExp2, Color: fogCol, Density: 0.05}},
	}

	clearCol := core.RGB(0x121620)
	for _, tc := range modes {
		t.Run(tc.name, func(t *testing.T) {
			img := fogRenderPanels(t, w, h, cam, tc.fog, panels, clearCol)
			for _, p := range panels {
				px, py := fogPanelPixel(t, cam, p.center, w, h)
				fogAssertPixelNear(t, img, px, py, fogSRGB(tc.fog.Apply(p.color, cam.Pos, p.center)), 2)
			}
			if tc.golden != "" {
				fogAssertGolden(t, tc.golden, img)
			}
		})
	}
}

func TestFogGPUHeightAndCombinedMatchReference(t *testing.T) {
	const (
		w = 128
		h = 96
	)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 4, Z: 10}, math3d.DegToRad(50), float32(w)/float32(h), 0.1, 100)
	cam.LookAt(math3d.Vec3{X: 0, Y: 3, Z: -10}, math3d.Vec3Up())

	baseCol := math3d.Vec3{X: 0.85, Y: 0.45, Z: 0.15}
	fogCol := math3d.Vec3{X: 0.7, Y: 0.8, Z: 0.92}

	// Three panels at the same Z (-10) but three altitudes: below, at, and well above HeightBase = 3.
	panels := []fogTestPanel{
		{center: math3d.Vec3{X: -5, Y: -1, Z: -10}, scale: math3d.Vec3{X: 2, Y: 2, Z: 1}, color: baseCol},
		{center: math3d.Vec3{X: 0, Y: 3, Z: -10}, scale: math3d.Vec3{X: 2, Y: 2, Z: 1}, color: baseCol},
		{center: math3d.Vec3{X: 5, Y: 8, Z: -10}, scale: math3d.Vec3{X: 2, Y: 2, Z: 1}, color: baseCol},
	}

	heightFog := render3d.Fog{
		Mode:          render3d.FogOff,
		Color:         fogCol,
		HeightEnabled: true,
		HeightBase:    3,
		HeightFalloff: 0.35,
		HeightDensity: 0.06,
	}

	img := fogRenderPanels(t, w, h, cam, heightFog, panels, core.RGB(0x10141c))
	fogAssertGolden(t, "fog_height", img)

	samples := make([]color.RGBA, len(panels))
	for i, p := range panels {
		px, py := fogPanelPixel(t, cam, p.center, w, h)
		fogAssertPixelNear(t, img, px, py, fogSRGB(heightFog.Apply(p.color, cam.Pos, p.center)), 2)
		samples[i] = img.RGBAAt(px, py)
	}
	// Blue rises as fog thickens (fog blue 0.92 > panel blue 0.15): the low panel is foggier than the middle one, and the middle one foggier than the high one.
	if !(samples[0].B > samples[1].B && samples[1].B > samples[2].B) {
		t.Errorf("height fog blue ordering low=%d mid=%d high=%d, want low > mid > high", samples[0].B, samples[1].B, samples[2].B)
	}

	combinedFog := heightFog
	combinedFog.Mode = render3d.FogExp
	combinedFog.Density = 0.03
	combinedFog.MaxOpacity = 0.9
	imgCombined := fogRenderPanels(t, w, h, cam, combinedFog, panels, core.RGB(0x10141c))
	for _, p := range panels {
		px, py := fogPanelPixel(t, cam, p.center, w, h)
		fogAssertPixelNear(t, imgCombined, px, py, fogSRGB(combinedFog.Apply(p.color, cam.Pos, p.center)), 2)
	}
}

func TestFogGPUOrthographicMatchesReference(t *testing.T) {
	const (
		w = 96
		h = 64
	)
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{X: 0, Y: 0, Z: 0}, 8, float32(w)/float32(h), 0.1, 60)
	cam.LookAt(math3d.Vec3{Z: -10}, math3d.Vec3Up())
	camU := render3d.NewCameraUniform(cam)

	baseCol := math3d.Vec3{X: 0.9, Y: 0.2, Z: 0.2}
	fog := render3d.Fog{Mode: render3d.FogLinear, Color: math3d.Vec3{X: 0.5, Y: 0.7, Z: 0.9}, Start: 5, End: 25}
	panels := []fogTestPanel{
		{center: math3d.Vec3{X: -3, Y: 0, Z: -5}, scale: math3d.Vec3{X: 1.5, Y: 2, Z: 1}, color: baseCol},
		{center: math3d.Vec3{X: 0, Y: 0, Z: -15}, scale: math3d.Vec3{X: 1.5, Y: 2, Z: 1}, color: baseCol},
		{center: math3d.Vec3{X: 3, Y: 0, Z: -25}, scale: math3d.Vec3{X: 1.5, Y: 2, Z: 1}, color: baseCol},
	}

	img := fogRenderPanels(t, w, h, cam, fog, panels, core.RGB(0x10141c))
	for _, p := range panels {
		// Under an orthographic camera looking along -Z from z = 0, the ray toward p starts on the camera plane at (p.X, p.Y, 0).
		origin := render3d.FogCameraPos(camU, p.center)
		if want := (math3d.Vec3{X: p.center.X, Y: p.center.Y}); origin.Sub(want).Len() > 1e-4 {
			t.Errorf("FogCameraPos(ortho, %v) = %v, want %v", p.center, origin, want)
		}
		px, py := fogPanelPixel(t, cam, p.center, w, h)
		fogAssertPixelNear(t, img, px, py, fogSRGB(fog.Apply(p.color, origin, p.center)), 2)
	}

	persp := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 1, Y: 2, Z: 3}, math3d.DegToRad(50), 1, 0.1, 60)
	if got := render3d.FogCameraPos(render3d.NewCameraUniform(persp), math3d.Vec3{Z: -10}); got != persp.Pos {
		t.Errorf("FogCameraPos(perspective) = %v, want the eye %v", got, persp.Pos)
	}
}

// TestFogLitPBRMatchesShadePoint checks the fogged lit pipeline against the CPU lighting reference: the pixel must equal ShadePoint's lit color blended by Fog.Apply and encoded to sRGB.
func TestFogLitPBRMatchesShadePoint(t *testing.T) {
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	var cx *core.Scope // nil: the objects live as long as the software renderer

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 4}, math3d.DegToRad(50), 1, 0.1, 50)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	rig := render3d.LightRig{
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.2, Y: 0.25, Z: 0.3}, Intensity: 0.4},
		Directional: []render3d.DirectionalLight{
			{Direction: math3d.Vec3{X: -0.3, Y: -0.4, Z: -1}, Color: math3d.Vec3{X: 1, Y: 0.95, Z: 0.9}, Intensity: 1.6},
		},
	}
	mat := render3d.PBRMaterial{BaseColor: math3d.Vec3{X: 0.8, Y: 0.3, Z: 0.2}, Roughness: 0.6}
	fog := render3d.Fog{
		Mode:          render3d.FogLinear,
		Color:         math3d.Vec3{X: 0.5, Y: 0.6, Z: 0.8},
		Start:         2,
		End:           6,
		HeightEnabled: true,
		HeightFalloff: 0.3,
		HeightDensity: 0.05,
	}
	scene := newFogScene(t, cx, dev, fog, nil, rig, fogMakeUnitCubeMesh(), []fogTestPanel{{scale: math3d.Vec3One()}})
	scene.draws[0].Write(dev, render3d.NewPBRDrawUniform(math3d.Mat4Identity(), mat))

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	scene.record(pass, cam)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	// The cube's front face sits at z = 0.5 facing the camera, 3.5 units away.
	p := math3d.Vec3{Z: 0.5}
	n := math3d.Vec3{Z: 1}
	v := cam.Pos.Sub(p).Norm()
	lit := render3d.ShadePoint(p, n, v, mat, rig)
	want := fogSRGB(fog.Apply(lit, cam.Pos, p))
	fogAssertPixelNear(t, sr.Image(), size/2, size/2, want, 2)
	if unfogged := fogSRGB(lit); unfogged == want {
		t.Fatalf("fog left the lit color %v unchanged; the test does not exercise fog", unfogged)
	}
}

// TestFogMaterialPipelineMatchesEvaluateMaterial checks NewMaterialPipeline with a fog layout against EvaluateMaterial: the pixel must equal the material's linear radiance blended by Fog.Apply, then tone-mapped and encoded as MaterialWGSL does.
func TestFogMaterialPipelineMatchesEvaluateMaterial(t *testing.T) {
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()
	var cx *core.Scope // nil: the objects live as long as the software renderer

	camBuf, err := render3d.NewCameraBuffer(cx, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defaults, err := render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	fog := render3d.Fog{Mode: render3d.FogLinear, Color: math3d.Vec3{X: 0.35, Y: 0.45, Z: 0.7}, Start: 1, End: 5}
	fogBuf, err := render3d.NewGPUFogBuffer(cx, dev, fog, render3d.FogBufferOptions{Label: "material-fog"})
	if err != nil {
		t.Fatalf("NewGPUFogBuffer: %v", err)
	}
	pipe, err := render3d.NewMaterialPipeline(cx, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		FogLayout:   fogBuf.Layout(),
	})
	if err != nil {
		t.Fatalf("NewMaterialPipeline with fog: %v", err)
	}
	m := render3d.DefaultMaterial()
	m.BaseColorFactor = [4]float32{0.82, 0.18, 0.16, 1}
	m.MetallicFactor = 0
	m.RoughnessFactor = 0.85
	gm, err := render3d.NewGPUMaterial(cx, dev, defaults, m, render3d.MaterialUpload{Label: "fogged"})
	if err != nil {
		t.Fatalf("NewGPUMaterial: %v", err)
	}
	quad, err := render3d.UploadMesh(cx, dev, worldQuadMesh(math3d.Vec3{}, 2, 2), render3d.MeshUpload{Label: "quad"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3}, math3d.DegToRad(50), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3{}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pass.SetPipeline(pipe)
	pass.SetBindGroup(render3d.CameraGroup, camBuf.BindGroup())
	gm.Bind(pass)
	pass.SetBindGroup(render3d.MaterialFogGroup, fogBuf.BindGroup())
	quad.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	res := render3d.EvaluateMaterial(render3d.MaterialEvalInput{
		Material:    m,
		Normal:      math3d.Vec3{Z: 1},
		ViewDir:     math3d.Vec3{Z: 1},
		FrontFacing: true,
	})
	linear := math3d.Vec3{X: res.Linear.X, Y: res.Linear.Y, Z: res.Linear.Z}
	fogged := fog.Apply(linear, cam.Pos, math3d.Vec3{})
	// MaterialWGSL's output transform: Reinhard tone mapping, then a 1/2.2 gamma.
	tonemap := func(c float32) uint8 {
		c = max(c, 0)
		return uint8(math.Round(math.Pow(float64(c/(c+1)), 1/2.2) * 255))
	}
	want := color.RGBA{R: tonemap(fogged.X), G: tonemap(fogged.Y), B: tonemap(fogged.Z), A: 255}
	fogAssertPixelNear(t, sr.Image(), size/2, size/2, want, 3)
	if unfogged := srgbVec4ToRGBA(res.SRGB); unfogged == want {
		t.Fatalf("fog left the material color %v unchanged; the test does not exercise fog", unfogged)
	}

	if _, err := render3d.NewMaterialPipeline(cx, dev, camBuf.Layout(), defaults.Layout(), render3d.MaterialPipelineOptions{FogLayout: &gpu.BindGroupLayout{}}); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("NewMaterialPipeline(invalid fog layout) = %v, want ErrInvalidDescriptor", err)
	}
}

func TestFogDrawsInGPUViewProfiles(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}

	const size = 64
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 0}, math3d.DegToRad(50), 1, 0.1, 100)
	cam.LookAt(math3d.Vec3{Z: -10}, math3d.Vec3Up())

	panel := fogTestPanel{center: math3d.Vec3{Z: -10}, scale: math3d.Vec3{X: 3, Y: 3, Z: 1}, color: math3d.Vec3{X: 0.9, Y: 0.2, Z: 0.1}}
	fog1 := render3d.Fog{Mode: render3d.FogLinear, Color: math3d.Vec3{X: 0.4, Y: 0.6, Z: 0.9}, Start: 5, End: 15}
	fog2 := render3d.Fog{Mode: render3d.FogExp, Color: math3d.Vec3{X: 0.2, Y: 0.85, Z: 0.4}, Density: 0.12}

	for _, prof := range profiles {
		t.Run(prof.name, func(t *testing.T) {
			var (
				scene  *fogScene
				active = fog1
				update bool
			)
			opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, prof.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "fogview",
					Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						scene = newFogScene(t, cx, dev, active, nil, render3d.LightRig{}, fogMakeUnitQuadMesh(), []fogTestPanel{panel})
					},
					OnFrame: func(frame *gpu.Frame) {
						if update {
							if err := scene.fog.Update(frame.Device(), active); err != nil {
								t.Fatalf("GPUFogBuffer.Update: %v", err)
							}
							update = false
						}
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x10141c))
						pass.ClearDepth(1)
						scene.record(pass, cam)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			// Frame 1: linear fog halfway (d = 10 between 5 and 15).
			app.Advance(16 * time.Millisecond)
			img1, err := app.GPUReadPixels("id:fogview")
			if err != nil {
				t.Fatalf("GPUReadPixels frame 1: %v", err)
			}
			fogAssertPixelNear(t, img1, size/2, size/2, fogSRGB(fog1.Apply(panel.color, cam.Pos, panel.center)), 2)

			// Frame 2: switch to green exponential fog and check the pixel follows.
			app.Post(func() {
				active = fog2
				update = true
			})
			app.Settle()
			app.Advance(16 * time.Millisecond)
			img2, err := app.GPUReadPixels("id:fogview")
			if err != nil {
				t.Fatalf("GPUReadPixels frame 2: %v", err)
			}
			fogAssertPixelNear(t, img2, size/2, size/2, fogSRGB(fog2.Apply(panel.color, cam.Pos, panel.center)), 2)
		})
	}
}

func TestRetainedFogBufferSurvivesDeviceLoss(t *testing.T) {
	const size = 64
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 0}, math3d.DegToRad(50), 1, 0.1, 100)
	cam.LookAt(math3d.Vec3{Z: -10}, math3d.Vec3Up())

	panel := fogTestPanel{center: math3d.Vec3{Z: -10}, scale: math3d.Vec3{X: 3, Y: 3, Z: 1}, color: math3d.Vec3{X: 0.9, Y: 0.2, Z: 0.1}}
	fogBefore := render3d.Fog{Mode: render3d.FogLinear, Color: math3d.Vec3{X: 0.3, Y: 0.5, Z: 0.9}, Start: 5, End: 15}
	fogAfter := render3d.Fog{Mode: render3d.FogLinear, Color: math3d.Vec3{X: 0.2, Y: 0.9, Z: 0.3}, Start: 5, End: 15}

	var (
		scene  *fogScene
		fogBuf *render3d.GPUFogBuffer
		next   *render3d.Fog
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "fogloss",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				// Only the retained fog buffer is created once; NSGo restores it after a device loss, and everything else is rebuilt per device.
				if fogBuf == nil {
					var err error
					if fogBuf, err = render3d.NewGPUFogBuffer(cx, dev, fogBefore, render3d.FogBufferOptions{Label: "retained-fog", Retained: true}); err != nil {
						t.Fatalf("NewGPUFogBuffer: %v", err)
					}
				}
				scene = newFogScene(t, cx, dev, render3d.Fog{}, fogBuf, render3d.LightRig{}, fogMakeUnitQuadMesh(), []fogTestPanel{panel})
			},
			OnFrame: func(frame *gpu.Frame) {
				if next != nil {
					if err := fogBuf.Update(frame.Device(), *next); err != nil {
						t.Fatalf("GPUFogBuffer.Update after restore: %v", err)
					}
					next = nil
				}
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				scene.record(pass, cam)
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	app.Advance(16 * time.Millisecond)
	firstFogBuf := fogBuf
	img, err := app.GPUReadPixels("id:fogloss")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	fogAssertPixelNear(t, img, size/2, size/2, fogSRGB(fogBefore.Apply(panel.color, cam.Pos, panel.center)), 2)

	app.TriggerGPUDeviceLost("id:fogloss", nsgotest.GPULostContextLost, "context lost")
	if firstFogBuf.Valid() {
		t.Fatal("fog buffer still valid while device is lost")
	}

	app.RestoreGPUDevice("id:fogloss")
	if fogBuf != firstFogBuf || !firstFogBuf.Valid() {
		t.Fatalf("retained fog buffer not restored: same=%v valid=%v", fogBuf == firstFogBuf, firstFogBuf.Valid())
	}

	// Without an Update, the restored buffer still holds fogBefore.
	app.Advance(16 * time.Millisecond)
	img, err = app.GPUReadPixels("id:fogloss")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	fogAssertPixelNear(t, img, size/2, size/2, fogSRGB(fogBefore.Apply(panel.color, cam.Pos, panel.center)), 2)

	// An Update on the new device takes effect.
	app.Post(func() {
		f := fogAfter
		next = &f
	})
	app.Settle()
	app.Advance(16 * time.Millisecond)
	img, err = app.GPUReadPixels("id:fogloss")
	if err != nil {
		t.Fatalf("GPUReadPixels after update: %v", err)
	}
	fogAssertPixelNear(t, img, size/2, size/2, fogSRGB(fogAfter.Apply(panel.color, cam.Pos, panel.center)), 2)
}

func TestFogScopeDisposalAndLifecycle(t *testing.T) {
	var (
		fogBuf *render3d.GPUFogBuffer
		pipe   *gpu.Pipeline
		show   *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(dev *gpu.Device) {
					scene := newFogScene(t, cx, dev, render3d.Fog{}, nil, render3d.LightRig{}, fogMakeUnitQuadMesh(), []fogTestPanel{{scale: math3d.Vec3One()}})
					fogBuf, pipe = scene.fog, scene.pipe
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())

	if !fogBuf.Valid() || !pipe.Valid() {
		t.Fatal("fog GPU resources not valid while mounted")
	}
	if got := fogBuf.ByteSize(); got != render3d.FogUniformSize {
		t.Errorf("GPUFogBuffer.ByteSize = %d, want %d", got, render3d.FogUniformSize)
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if fogBuf.Valid() || pipe.Valid() {
		t.Error("fog GPU resources still valid after scope disposal")
	}

	if _, err := render3d.NewGPUFogBuffer(nil, nil, render3d.Fog{}, render3d.FogBufferOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewGPUFogBuffer(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewGPUFogBuffer(nil, nil, render3d.Fog{Mode: render3d.FogLinear, Start: 10, End: 5}, render3d.FogBufferOptions{}); !errors.Is(err, render3d.ErrInvalidFog) {
		t.Errorf("NewGPUFogBuffer(invalid fog) = %v, want ErrInvalidFog", err)
	}
	if _, err := render3d.NewLitPBRFogPipeline(nil, nil, nil, nil, nil, nil, gpu.FormatUndefined); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewLitPBRFogPipeline(nil dev) = %v, want ErrDeviceLost", err)
	}
	var nilBuf *render3d.GPUFogBuffer
	if nilBuf.Valid() || nilBuf.Buffer() != nil || nilBuf.Layout() != nil || nilBuf.BindGroup() != nil || nilBuf.ByteSize() != 0 || nilBuf.Close() != nil {
		t.Error("nil GPUFogBuffer accessors/Close failed")
	}
	nilBuf.Bind(nil)
	if err := nilBuf.Update(nil, render3d.Fog{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("nil GPUFogBuffer.Update = %v, want ErrDeviceLost", err)
	}
}
