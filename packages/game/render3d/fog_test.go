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

func fogColorToRGBA(c [3]float32) color.RGBA {
	toU8 := func(v float32) uint8 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint8(math.Round(float64(v) * 255))
	}
	return color.RGBA{R: toU8(c[0]), G: toU8(c[1]), B: toU8(c[2]), A: 255}
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
	src := [3]float32{0.8, 0.2, 0.1}
	fogCol := [3]float32{0.6, 0.7, 0.9}

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
			for i := range 3 {
				wantC := src[i]*(1-tc.want) + fogCol[i]*tc.want
				fogApprox(t, "linear color", got[i], wantC, 1e-6)
			}
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
	fogCol := [3]float32{0.7, 0.75, 0.85}

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
		{"linear", render3d.Fog{Mode: render3d.FogLinear, Color: [3]float32{0.5, 0.6, 0.7}, Start: 10, End: 50}, true},
		{"exp", render3d.Fog{Mode: render3d.FogExp, Density: 0.04, MaxOpacity: 0.8}, true},
		{"exp2", render3d.Fog{Mode: render3d.FogExp2, Density: 0.02, MaxOpacity: 1}, true},
		{"height negative base", render3d.Fog{HeightEnabled: true, HeightBase: -25, HeightFalloff: 0.1, HeightDensity: 0.05}, true},
		{"unknown mode", render3d.Fog{Mode: render3d.FogMode(9)}, false},
		{"negative color", render3d.Fog{Color: [3]float32{-0.1, 0, 0}}, false},
		{"nan color", render3d.Fog{Color: [3]float32{0, nan, 0}}, false},
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

	// FogObject.Validate checks.
	if err := (render3d.FogObject{}).Validate(); err != nil {
		t.Errorf("zero FogObject.Validate = %v, want nil", err)
	}
	if err := (render3d.FogObject{Ambient: 1.5}).Validate(); !errors.Is(err, render3d.ErrInvalidFog) {
		t.Errorf("Ambient=1.5 Validate = %v, want ErrInvalidFog", err)
	}
	if err := (render3d.FogObject{LightDir: math3d.Vec3{X: nan}}).Validate(); !errors.Is(err, render3d.ErrInvalidFog) {
		t.Errorf("NaN LightDir Validate = %v, want ErrInvalidFog", err)
	}
}

func TestFogUniformLayoutAndZeroAlloc(t *testing.T) {
	if render3d.FogUniformSize != 48 {
		t.Fatalf("FogUniformSize = %d, want 48", render3d.FogUniformSize)
	}
	fog := render3d.Fog{
		Mode:          render3d.FogExp2,
		Color:         [3]float32{0.25, 0.5, 0.75},
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
			Color:         [3]float32{r, g, b},
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
		res := cfg.Apply([3]float32{0.5, 0.5, 0.5}, cam, pos)
		for i, c := range res {
			if math.IsNaN(float64(c)) || math.IsInf(float64(c), 0) {
				t.Fatalf("Apply[%d] = %v, want finite", i, c)
			}
		}
	})
}

func BenchmarkAppendFogUniforms(b *testing.B) {
	fog := render3d.Fog{
		Mode:          render3d.FogExp2,
		Color:         [3]float32{0.6, 0.7, 0.8},
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
		Color:         [3]float32{0.6, 0.7, 0.8},
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

// fogTestPanel describes one flat-colored vertical quad placed at a known world position facing +Z.
type fogTestPanel struct {
	center math3d.Vec3
	scale  math3d.Vec3
	color  [3]float32
}

// fogRenderPanels renders panels using FogPipeline + GPUFogBuffer + CameraBuffer on the software GPU device and returns the rendered image.
func fogRenderPanels(t *testing.T, w, h int, cam *scene3d.Camera, fog render3d.Fog, panels []fogTestPanel, clear core.Color) *image.RGBA {
	t.Helper()
	sr := gpu.NewSoftwareRenderer(w, h, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	fogBuf, err := render3d.NewGPUFogBuffer(nil, dev, fog, render3d.FogBufferOptions{Label: "test-fog"})
	if err != nil {
		t.Fatalf("NewGPUFogBuffer: %v", err)
	}
	t.Cleanup(func() { _ = fogBuf.Close() })

	pipe, err := render3d.NewFogPipeline(nil, dev, camBuf.Layout(), fogBuf.Layout(), render3d.FogPipelineOptions{
		Label:       "test-fog-pipe",
		DepthFormat: gpu.FormatDepth24Plus,
		CullMode:    gpu.CullBack,
	})
	if err != nil {
		t.Fatalf("NewFogPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipe.Close() })

	quad, err := render3d.UploadMesh(nil, dev, fogMakeUnitQuadMesh(), render3d.MeshUpload{Label: "panel"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	t.Cleanup(func() { _ = quad.Close() })

	objs := make([]*render3d.GPUFogObject, len(panels))
	for i, p := range panels {
		model := math3d.Translate(p.center).Mul(math3d.Scale(p.scale))
		obj, err := pipe.NewObjectBuffer(nil, dev, render3d.FogObject{
			Model:     model,
			BaseColor: [4]float32{p.color[0], p.color[1], p.color[2], 1},
			Ambient:   1, // unlit flat base color for exact fog formula verification
		}, render3d.FogBufferOptions{Label: "panel"})
		if err != nil {
			t.Fatalf("NewObjectBuffer %d: %v", i, err)
		}
		t.Cleanup(func() { _ = obj.Close() })
		objs[i] = obj
	}

	camBuf.WriteCamera(cam)
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label: "fog-test",
		ColorAttachments: []gpu.ColorAttachment{{
			LoadOp:     gpu.LoadClear,
			StoreOp:    gpu.StoreStore,
			ClearColor: clear,
		}},
		DepthStencil: &gpu.DepthStencilAttachment{
			DepthLoadOp:  gpu.LoadClear,
			DepthStoreOp: gpu.StoreDiscard,
			ClearDepth:   1,
		},
	})
	pipe.Bind(pass, camBuf.BindGroup(), fogBuf.BindGroup())
	for _, obj := range objs {
		obj.Bind(pass)
		quad.Draw(pass)
	}
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	return sr.Image()
}

func TestFogGPUDistanceModesMatchReference(t *testing.T) {
	const (
		w = 160
		h = 64
	)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0, Y: 0, Z: 0}, math3d.DegToRad(50), float32(w)/float32(h), 0.1, 100)
	cam.LookAt(math3d.Vec3{Z: -10}, math3d.Vec3Up())

	// Four flat panels at exact set Z distances (4, 12, 20, 32), placed side-by-side in screen space
	// so every panel's center projects cleanly onto a distinct pixel with no overlap.
	baseCol := [3]float32{0.9, 0.18, 0.12}
	fogCol := [3]float32{0.62, 0.72, 0.86}
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
		{
			name: "off",
			fog:  render3d.Fog{Mode: render3d.FogOff, Color: fogCol, Start: 8, End: 24},
		},
		{
			name:   "linear",
			fog:    render3d.Fog{Mode: render3d.FogLinear, Color: fogCol, Start: 8, End: 24},
			golden: "fog_linear_distances",
		},
		{
			name: "linear-max-opacity",
			fog:  render3d.Fog{Mode: render3d.FogLinear, Color: fogCol, Start: 8, End: 24, MaxOpacity: 0.65},
		},
		{
			name: "exp",
			fog:  render3d.Fog{Mode: render3d.FogExp, Color: fogCol, Density: 0.06},
		},
		{
			name: "exp2",
			fog:  render3d.Fog{Mode: render3d.FogExp2, Color: fogCol, Density: 0.05},
		},
	}

	clearCol := core.RGB(0x121620)
	for _, tc := range modes {
		t.Run(tc.name, func(t *testing.T) {
			img := fogRenderPanels(t, w, h, cam, tc.fog, panels, clearCol)
			for i, p := range panels {
				s, _, vis := cam.WorldToScreen(p.center, w, h)
				if !vis {
					t.Fatalf("panel %d center %v not visible", i, p.center)
				}
				px, py := int(math.Round(float64(s.X))), int(math.Round(float64(s.Y)))
				wantCol := fogColorToRGBA(tc.fog.Apply(p.color, cam.Pos, p.center))
				fogAssertPixelNear(t, img, px, py, wantCol, 2)
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

	baseCol := [3]float32{0.85, 0.45, 0.15}
	fogCol := [3]float32{0.7, 0.8, 0.92}

	// Three panels at the same horizontal Z (-10) but three distinct altitudes:
	// low (below HeightBase=3), mid (at HeightBase=3), high (well above HeightBase=3).
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
		s, _, vis := cam.WorldToScreen(p.center, w, h)
		if !vis {
			t.Fatalf("panel %d center not visible", i)
		}
		px, py := int(math.Round(float64(s.X))), int(math.Round(float64(s.Y)))
		wantCol := fogColorToRGBA(heightFog.Apply(p.color, cam.Pos, p.center))
		fogAssertPixelNear(t, img, px, py, wantCol, 2)
		samples[i] = img.RGBAAt(px, py)
	}
	// Blue channel increases monotonically as fog intensifies (fogCol.B=0.92 > baseCol.B=0.15):
	// low panel must be foggier than mid, and mid foggier than high.
	if !(samples[0].B > samples[1].B && samples[1].B > samples[2].B) {
		t.Errorf("height fog blue ordering low=%d mid=%d high=%d, want low > mid > high", samples[0].B, samples[1].B, samples[2].B)
	}

	// Combined distance + height fog.
	combinedFog := heightFog
	combinedFog.Mode = render3d.FogExp
	combinedFog.Density = 0.03
	combinedFog.MaxOpacity = 0.9
	imgCombined := fogRenderPanels(t, w, h, cam, combinedFog, panels, core.RGB(0x10141c))
	for i, p := range panels {
		s, _, vis := cam.WorldToScreen(p.center, w, h)
		if !vis {
			t.Fatalf("combined panel %d center not visible", i)
		}
		px, py := int(math.Round(float64(s.X))), int(math.Round(float64(s.Y)))
		wantCol := fogColorToRGBA(combinedFog.Apply(p.color, cam.Pos, p.center))
		fogAssertPixelNear(t, imgCombined, px, py, wantCol, 2)
	}
}

func TestFogGPUOrthographicMatchesReference(t *testing.T) {
	const (
		w = 96
		h = 64
	)
	cam := scene3d.NewOrthographicCamera(math3d.Vec3{X: 0, Y: 0, Z: 0}, 8, float32(w)/float32(h), 0.1, 60)
	cam.LookAt(math3d.Vec3{Z: -10}, math3d.Vec3Up())

	baseCol := [3]float32{0.9, 0.2, 0.2}
	fogCol := [3]float32{0.5, 0.7, 0.9}
	fog := render3d.Fog{
		Mode:  render3d.FogLinear,
		Color: fogCol,
		Start: 5,
		End:   25,
	}
	panels := []fogTestPanel{
		{center: math3d.Vec3{X: -3, Y: 0, Z: -5}, scale: math3d.Vec3{X: 1.5, Y: 2, Z: 1}, color: baseCol},
		{center: math3d.Vec3{X: 0, Y: 0, Z: -15}, scale: math3d.Vec3{X: 1.5, Y: 2, Z: 1}, color: baseCol},
		{center: math3d.Vec3{X: 3, Y: 0, Z: -25}, scale: math3d.Vec3{X: 1.5, Y: 2, Z: 1}, color: baseCol},
	}

	img := fogRenderPanels(t, w, h, cam, fog, panels, core.RGB(0x10141c))
	for i, p := range panels {
		s, _, vis := cam.WorldToScreen(p.center, w, h)
		if !vis {
			t.Fatalf("ortho panel %d not visible", i)
		}
		px, py := int(math.Round(float64(s.X))), int(math.Round(float64(s.Y)))
		// Under an orthographic camera looking along -Z from z=0, the ray starts on the camera plane at (p.X, p.Y, 0).
		rayOrigin := math3d.Vec3{X: p.center.X, Y: p.center.Y, Z: 0}
		wantCol := fogColorToRGBA(fog.Apply(p.color, rayOrigin, p.center))
		fogAssertPixelNear(t, img, px, py, wantCol, 2)
	}
}

func TestFogGPULitShadingAndMeshWithoutNormals(t *testing.T) {
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	t.Cleanup(sr.Close)
	dev := sr.Device()

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 4}, math3d.DegToRad(50), 1, 0.1, 50)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())

	fog := render3d.Fog{
		Mode:  render3d.FogLinear,
		Color: [3]float32{0.5, 0.6, 0.8},
		Start: 2,
		End:   10,
	}
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	t.Cleanup(func() { _ = camBuf.Close() })

	fogBuf, err := render3d.NewGPUFogBuffer(nil, dev, fog, render3d.FogBufferOptions{})
	if err != nil {
		t.Fatalf("NewGPUFogBuffer: %v", err)
	}
	t.Cleanup(func() { _ = fogBuf.Close() })

	pipe, err := render3d.NewFogPipeline(nil, dev, camBuf.Layout(), fogBuf.Layout(), render3d.FogPipelineOptions{
		DepthFormat: gpu.FormatDepth24Plus,
		CullMode:    gpu.CullBack,
	})
	if err != nil {
		t.Fatalf("NewFogPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipe.Close() })

	cube, err := render3d.UploadMesh(nil, dev, fogMakeUnitCubeMesh(), render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh cube: %v", err)
	}
	t.Cleanup(func() { _ = cube.Close() })

	// Front face of unit cube sits at z = +0.5 (distance 3.5 from cam at z = 4), normal = (0, 0, 1).
	// LightDir = (0, 0, 1), Ambient = 0.25 -> NdotL = 1 -> shade = 1.0 -> lit = baseColor.
	base := [3]float32{0.8, 0.3, 0.2}
	obj, err := pipe.NewObjectBuffer(nil, dev, render3d.FogObject{
		BaseColor: [4]float32{base[0], base[1], base[2], 1},
		LightDir:  math3d.Vec3{Z: 1},
		Ambient:   0.25,
	}, render3d.FogBufferOptions{})
	if err != nil {
		t.Fatalf("NewObjectBuffer: %v", err)
	}
	t.Cleanup(func() { _ = obj.Close() })

	camBuf.WriteCamera(cam)
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pipe.Bind(pass, camBuf.BindGroup(), fogBuf.BindGroup())
	obj.Bind(pass)
	cube.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	wantLit := fogColorToRGBA(fog.Apply(base, cam.Pos, math3d.Vec3{Z: 0.5}))
	fogAssertPixelNear(t, sr.Image(), size/2, size/2, wantLit, 2)

	// Also verify a mesh with NO normals does not produce NaN with directional lighting active.
	noNormMesh := render3d.Mesh{
		Positions: []math3d.Vec3{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}},
		Indices:   []uint32{0, 1, 2, 0, 2, 3},
	}
	bare, err := render3d.UploadMesh(nil, dev, noNormMesh, render3d.MeshUpload{})
	if err != nil {
		t.Fatalf("UploadMesh bare: %v", err)
	}
	t.Cleanup(func() { _ = bare.Close() })
	enc = dev.BeginEncoder()
	pass = enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	pipe.Bind(pass, camBuf.BindGroup(), fogBuf.BindGroup())
	obj.Bind(pass)
	bare.Draw(pass)
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	// With zero normals, shader falls back to ambient=0.25 without NaN.
	ambientLit := [3]float32{base[0] * 0.25, base[1] * 0.25, base[2] * 0.25}
	wantBare := fogColorToRGBA(fog.Apply(ambientLit, cam.Pos, math3d.Vec3{Z: 0}))
	fogAssertPixelNear(t, sr.Image(), size/2, size/2, wantBare, 2)
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

	baseCol := [3]float32{0.9, 0.2, 0.1}
	fogCol := [3]float32{0.4, 0.6, 0.9}
	fog1 := render3d.Fog{Mode: render3d.FogLinear, Color: fogCol, Start: 5, End: 15}
	fog2 := render3d.Fog{Mode: render3d.FogExp, Color: [3]float32{0.2, 0.85, 0.4}, Density: 0.12}

	for _, prof := range profiles {
		t.Run(prof.name, func(t *testing.T) {
			var (
				camBuf *render3d.CameraBuffer
				fogBuf *render3d.GPUFogBuffer
				pipe   *render3d.FogPipeline
				mesh   *render3d.GPUMesh
				obj    *render3d.GPUFogObject
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
						var err error
						if camBuf, err = render3d.NewCameraBuffer(cx, dev); err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
						if fogBuf, err = render3d.NewGPUFogBuffer(cx, dev, active, render3d.FogBufferOptions{Label: "view-fog"}); err != nil {
							t.Fatalf("NewGPUFogBuffer: %v", err)
						}
						if pipe, err = render3d.NewFogPipeline(cx, dev, camBuf.Layout(), fogBuf.Layout(), render3d.FogPipelineOptions{
							DepthFormat: gpu.FormatDepth24Plus,
							CullMode:    gpu.CullBack,
						}); err != nil {
							t.Fatalf("NewFogPipeline: %v", err)
						}
						if mesh, err = render3d.UploadMesh(cx, dev, fogMakeUnitQuadMesh(), render3d.MeshUpload{Label: "quad"}); err != nil {
							t.Fatalf("UploadMesh: %v", err)
						}
						if obj, err = pipe.NewObjectBuffer(cx, dev, render3d.FogObject{
							Model:     math3d.Translate(math3d.Vec3{Z: -10}).Mul(math3d.Scale(math3d.Vec3{X: 3, Y: 3, Z: 1})),
							BaseColor: [4]float32{baseCol[0], baseCol[1], baseCol[2], 1},
							Ambient:   1,
						}, render3d.FogBufferOptions{Label: "obj"}); err != nil {
							t.Fatalf("NewObjectBuffer: %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						if update {
							if err := fogBuf.Update(frame.Device(), active); err != nil {
								t.Fatalf("fogBuf.Update: %v", err)
							}
							update = false
						}
						camBuf.WriteCamera(cam)
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x10141c))
						pass.ClearDepth(1)
						pipe.Bind(pass, camBuf.BindGroup(), fogBuf.BindGroup())
						obj.Bind(pass)
						mesh.Draw(pass)
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			// Frame 1: linear fog at midpoint (d=10 between 5 and 15 => factor 0.5).
			app.Advance(16 * time.Millisecond)
			img1, err := app.GPUReadPixels("id:fogview")
			if err != nil {
				t.Fatalf("GPUReadPixels frame 1: %v", err)
			}
			want1 := fogColorToRGBA(fog1.Apply(baseCol, cam.Pos, math3d.Vec3{Z: -10}))
			fogAssertPixelNear(t, img1, size/2, size/2, want1, 2)

			// Frame 2: dynamically update fog to green exponential fog and verify pixels track it.
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
			want2 := fogColorToRGBA(fog2.Apply(baseCol, cam.Pos, math3d.Vec3{Z: -10}))
			fogAssertPixelNear(t, img2, size/2, size/2, want2, 2)
		})
	}
}

func TestRetainedFogBufferSurvivesDeviceLoss(t *testing.T) {
	const size = 64
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 0}, math3d.DegToRad(50), 1, 0.1, 100)
	cam.LookAt(math3d.Vec3{Z: -10}, math3d.Vec3Up())

	baseCol := [3]float32{0.9, 0.2, 0.1}
	fogBefore := render3d.Fog{Mode: render3d.FogLinear, Color: [3]float32{0.3, 0.5, 0.9}, Start: 5, End: 15}
	fogAfter := render3d.Fog{Mode: render3d.FogLinear, Color: [3]float32{0.2, 0.9, 0.3}, Start: 5, End: 15}

	var (
		camBuf *render3d.CameraBuffer
		fogBuf *render3d.GPUFogBuffer
		pipe   *render3d.FogPipeline
		mesh   *render3d.GPUMesh
		obj    *render3d.GPUFogObject
		next   *render3d.Fog
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "fogloss",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				var err error
				// CameraBuffer is not retained, so recreate it per device epoch.
				if camBuf, err = render3d.NewCameraBuffer(cx, dev); err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				// Retained fogBuf and mesh are created only once and automatically restored by NSGo.
				if fogBuf == nil {
					if fogBuf, err = render3d.NewGPUFogBuffer(cx, dev, fogBefore, render3d.FogBufferOptions{Label: "retained-fog", Retained: true}); err != nil {
						t.Fatalf("NewGPUFogBuffer: %v", err)
					}
				}
				if mesh == nil {
					if mesh, err = render3d.UploadMesh(cx, dev, fogMakeUnitQuadMesh(), render3d.MeshUpload{Label: "retained-quad", Retained: true}); err != nil {
						t.Fatalf("UploadMesh: %v", err)
					}
				}
				if pipe, err = render3d.NewFogPipeline(cx, dev, camBuf.Layout(), fogBuf.Layout(), render3d.FogPipelineOptions{
					DepthFormat: gpu.FormatDepth24Plus,
					CullMode:    gpu.CullBack,
				}); err != nil {
					t.Fatalf("NewFogPipeline: %v", err)
				}
				if obj, err = pipe.NewObjectBuffer(cx, dev, render3d.FogObject{
					Model:     math3d.Translate(math3d.Vec3{Z: -10}).Mul(math3d.Scale(math3d.Vec3{X: 3, Y: 3, Z: 1})),
					BaseColor: [4]float32{baseCol[0], baseCol[1], baseCol[2], 1},
					Ambient:   1,
				}, render3d.FogBufferOptions{Label: "obj"}); err != nil {
					t.Fatalf("NewObjectBuffer: %v", err)
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				if next != nil {
					if err := fogBuf.Update(frame.Device(), *next); err != nil {
						t.Fatalf("fogBuf.Update after restore: %v", err)
					}
					next = nil
				}
				camBuf.WriteCamera(cam)
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				pipe.Bind(pass, camBuf.BindGroup(), fogBuf.BindGroup())
				obj.Bind(pass)
				mesh.Draw(pass)
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
	fogAssertPixelNear(t, img, size/2, size/2, fogColorToRGBA(fogBefore.Apply(baseCol, cam.Pos, math3d.Vec3{Z: -10})), 2)

	app.TriggerGPUDeviceLost("id:fogloss", nsgotest.GPULostContextLost, "context lost")
	if firstFogBuf.Valid() {
		t.Fatal("fog buffer still valid while device is lost")
	}

	app.RestoreGPUDevice("id:fogloss")
	if fogBuf != firstFogBuf || !firstFogBuf.Valid() {
		t.Fatalf("retained fog buffer not restored: same=%v valid=%v", fogBuf == firstFogBuf, firstFogBuf.Valid())
	}

	// Render immediately after restore without calling Update: shadow data must still match fogBefore.
	app.Advance(16 * time.Millisecond)
	img, err = app.GPUReadPixels("id:fogloss")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	fogAssertPixelNear(t, img, size/2, size/2, fogColorToRGBA(fogBefore.Apply(baseCol, cam.Pos, math3d.Vec3{Z: -10})), 2)

	// Now update the restored retained fog buffer on the new device and verify the new fog renders.
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
	fogAssertPixelNear(t, img, size/2, size/2, fogColorToRGBA(fogAfter.Apply(baseCol, cam.Pos, math3d.Vec3{Z: -10})), 2)
}

func TestFogScopeDisposalAndLifecycle(t *testing.T) {
	var (
		fogBuf *render3d.GPUFogBuffer
		pipe   *render3d.FogPipeline
		obj    *render3d.GPUFogObject
		show   *core.Signal[bool]
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: func(cx *core.Scope) core.Node {
			return gpu.View(cx, gpu.ViewProps{
				Style: core.Style{Width: core.Pt(8), Height: core.Pt(8)},
				OnReady: func(dev *gpu.Device) {
					camBuf, err := render3d.NewCameraBuffer(cx, dev)
					if err != nil {
						t.Fatalf("NewCameraBuffer: %v", err)
					}
					fogBuf, err = render3d.NewGPUFogBuffer(cx, dev, render3d.Fog{}, render3d.FogBufferOptions{})
					if err != nil {
						t.Fatalf("NewGPUFogBuffer: %v", err)
					}
					pipe, err = render3d.NewFogPipeline(cx, dev, camBuf.Layout(), fogBuf.Layout(), render3d.FogPipelineOptions{})
					if err != nil {
						t.Fatalf("NewFogPipeline: %v", err)
					}
					obj, err = pipe.NewObjectBuffer(cx, dev, render3d.FogObject{}, render3d.FogBufferOptions{})
					if err != nil {
						t.Fatalf("NewObjectBuffer: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())

	if !fogBuf.Valid() || !pipe.Valid() || !obj.Valid() {
		t.Fatal("fog GPU resources not valid while mounted")
	}
	app.Post(func() { show.Set(false) })
	app.Settle()
	if fogBuf.Valid() || pipe.Valid() || obj.Valid() {
		t.Error("fog GPU resources still valid after scope disposal")
	}

	// Error and nil safety checks.
	if _, err := render3d.NewGPUFogBuffer(nil, nil, render3d.Fog{}, render3d.FogBufferOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewGPUFogBuffer(nil dev) = %v, want ErrDeviceLost", err)
	}
	if _, err := render3d.NewGPUFogBuffer(nil, nil, render3d.Fog{Mode: render3d.FogLinear, Start: 10, End: 5}, render3d.FogBufferOptions{}); !errors.Is(err, render3d.ErrInvalidFog) {
		t.Errorf("NewGPUFogBuffer(invalid fog) = %v, want ErrInvalidFog", err)
	}
	if _, err := render3d.NewFogPipeline(nil, nil, nil, nil, render3d.FogPipelineOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("NewFogPipeline(nil dev) = %v, want ErrDeviceLost", err)
	}
	var nilBuf *render3d.GPUFogBuffer
	if nilBuf.Valid() || nilBuf.Buffer() != nil || nilBuf.Layout() != nil || nilBuf.BindGroup() != nil || nilBuf.Close() != nil {
		t.Error("nil GPUFogBuffer accessors/Close failed")
	}
	if err := nilBuf.Update(nil, render3d.Fog{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("nil GPUFogBuffer.Update = %v, want ErrDeviceLost", err)
	}
	var nilPipe *render3d.FogPipeline
	if nilPipe.Valid() || nilPipe.Pipeline() != nil || nilPipe.ObjectLayout() != nil || nilPipe.Close() != nil {
		t.Error("nil FogPipeline accessors/Close failed")
	}
	var nilObj *render3d.GPUFogObject
	if nilObj.Valid() || nilObj.Buffer() != nil || nilObj.BindGroup() != nil || nilObj.Close() != nil {
		t.Error("nil GPUFogObject accessors/Close failed")
	}
	if err := nilObj.Update(nil, render3d.FogObject{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("nil GPUFogObject.Update = %v, want ErrDeviceLost", err)
	}
}
