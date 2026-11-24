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

func toonSphereMesh(center math3d.Vec3, radius float32, stacks, slices int) render3d.Mesh {
	var m render3d.Mesh
	for iy := 0; iy <= stacks; iy++ {
		v := float32(iy) / float32(stacks)
		theta := float64(v) * math.Pi
		sinT, cosT := math.Sin(theta), math.Cos(theta)
		for ix := 0; ix <= slices; ix++ {
			u := float32(ix) / float32(slices)
			phi := float64(u) * 2 * math.Pi
			sinP, cosP := math.Sin(phi), math.Cos(phi)
			n := math3d.Vec3{
				X: float32(sinT * cosP),
				Y: float32(cosT),
				Z: float32(sinT * sinP),
			}
			m.Positions = append(m.Positions, center.Add(n.Scale(radius)))
			m.Normals = append(m.Normals, n)
			m.UVs = append(m.UVs, math3d.Vec2{X: u, Y: v})
		}
	}
	stride := uint32(slices + 1)
	for iy := uint32(0); iy < uint32(stacks); iy++ {
		for ix := uint32(0); ix < uint32(slices); ix++ {
			a := iy*stride + ix
			b := a + stride
			c := b + 1
			d := a + 1
			// CCW outward winding.
			m.Indices = append(m.Indices, a, c, b, a, d, c)
		}
	}
	return m
}

func toonBoxMesh(half math3d.Vec3) render3d.Mesh {
	type face struct {
		n, u, v    math3d.Vec3
		hn, hu, hv float32
	}
	faces := [...]face{
		{math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, math3d.Vec3{Y: 1}, half.X, half.Z, half.Y},
		{math3d.Vec3{X: -1}, math3d.Vec3{Z: 1}, math3d.Vec3{Y: 1}, half.X, half.Z, half.Y},
		{math3d.Vec3{Y: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: -1}, half.Y, half.X, half.Z},
		{math3d.Vec3{Y: -1}, math3d.Vec3{X: 1}, math3d.Vec3{Z: 1}, half.Y, half.X, half.Z},
		{math3d.Vec3{Z: 1}, math3d.Vec3{X: 1}, math3d.Vec3{Y: 1}, half.Z, half.X, half.Y},
		{math3d.Vec3{Z: -1}, math3d.Vec3{X: -1}, math3d.Vec3{Y: 1}, half.Z, half.X, half.Y},
	}
	var m render3d.Mesh
	for _, f := range faces {
		base := uint32(len(m.Positions))
		c := f.n.Scale(f.hn)
		u, v := f.u.Scale(f.hu), f.v.Scale(f.hv)
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

func toonPlaneMesh(half float32) render3d.Mesh {
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

func toonRampImage(steps []color.NRGBA) *image.NRGBA {
	w := len(steps)
	img := image.NewNRGBA(image.Rect(0, 0, w, 1))
	for x, c := range steps {
		img.SetNRGBA(x, 0, c)
	}
	return img
}

func toonDefaults(t *testing.T, cx *core.Scope, dev *gpu.Device, retained bool) *render3d.MaterialDefaults {
	t.Helper()
	d, err := render3d.NewMaterialDefaults(cx, dev, render3d.MaterialDefaultsOptions{Retained: retained})
	if err != nil {
		t.Fatalf("NewMaterialDefaults: %v", err)
	}
	return d
}

func toonRGBAFromLinear(c [4]float32) color.RGBA {
	toU8 := func(v float32) uint8 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint8(math.Round(float64(v) * 255))
	}
	return color.RGBA{R: toU8(c[0]), G: toU8(c[1]), B: toU8(c[2]), A: toU8(c[3])}
}

func toonNearColor(a, b color.RGBA, tol int) bool {
	diff := func(x, y uint8) int {
		d := int(x) - int(y)
		if d < 0 {
			return -d
		}
		return d
	}
	return diff(a.R, b.R) <= tol && diff(a.G, b.G) <= tol && diff(a.B, b.B) <= tol && diff(a.A, b.A) <= tol
}

func TestToonQuantizeBandAndSmoothness(t *testing.T) {
	// Hard steps (smoothness = 0) must produce exact discrete band levels across N·L.
	for _, bands := range []int{2, 3, 4, 5, 8} {
		seen := make(map[int]bool)
		for i := 0; i <= 200; i++ {
			ndl := float32(i) / 200.0
			got := render3d.ToonQuantizeBand(ndl, bands, 0)
			// Each discrete level must be k/(bands-1) for integer k in [0, bands-1].
			scaled := got * float32(bands-1)
			rounded := math.Round(float64(scaled))
			if math.Abs(float64(scaled)-rounded) > 1e-5 {
				t.Fatalf("bands=%d ndl=%g: got=%g not a discrete step of 1/%d", bands, ndl, got, bands-1)
			}
			seen[int(rounded)] = true
		}
		if len(seen) != bands {
			t.Fatalf("bands=%d: observed %d distinct levels, want %d (%v)", bands, len(seen), bands, seen)
		}
	}

	// Out-of-range ndl clamps cleanly to 0 and 1.
	if got := render3d.ToonQuantizeBand(-0.5, 3, 0); got != 0 {
		t.Errorf("ndl=-0.5: got %g, want 0", got)
	}
	if got := render3d.ToonQuantizeBand(1.5, 3, 0); got != 1 {
		t.Errorf("ndl=1.5: got %g, want 1", got)
	}

	// Smoothness > 0 must be monotonic, hit mid-level right at band boundary, and produce intermediate values.
	for _, smooth := range []float32{0.05, 0.2, 0.5} {
		prev := float32(-1)
		intermediates := 0
		for i := 0; i <= 300; i++ {
			ndl := float32(i) / 300.0
			got := render3d.ToonQuantizeBand(ndl, 3, smooth)
			if got < prev-1e-6 {
				t.Fatalf("smooth=%g: non-monotonic at ndl=%g (%g < %g)", smooth, ndl, got, prev)
			}
			if got > 0.05 && got < 0.45 {
				intermediates++
			}
			prev = got
		}
		if intermediates == 0 {
			t.Fatalf("smooth=%g: expected smooth transition values between band 0 and band 1", smooth)
		}
		// Right at threshold 1/3, smoothstep evaluates to 0.5 -> (1 - 1 + 0.5)/2 = 0.25.
		mid := render3d.ToonQuantizeBand(1.0/3.0, 3, smooth)
		if math.Abs(float64(mid-0.25)) > 1e-4 {
			t.Errorf("smooth=%g at ndl=1/3: got %g, want 0.25", smooth, mid)
		}
	}
}

func TestEvaluateToonDiffuseSpecularRimAndRamp(t *testing.T) {
	mat := render3d.ToonMaterial{
		BaseColorFactor:    [4]float32{0.8, 0.4, 0.2, 1},
		ShadowColor:        [3]float32{0.2, 0.25, 0.4},
		Bands:              3,
		BandSmoothness:     0,
		SpecularColor:      [3]float32{0.5, 0.5, 0.5},
		SpecularPower:      16,
		SpecularThreshold:  0.6,
		SpecularSmoothness: 0,
		RimColor:           [3]float32{0.3, 0.6, 0.9},
		RimThreshold:       0.7,
		RimSmoothness:      0,
	}

	// 1. Back-facing shadow band (N·L <= 0), facing camera (N·V = 1 -> no rim, no specular).
	shadow := render3d.EvaluateToon(mat, render3d.ToonEvalInput{
		Normal:     math3d.Vec3{Z: 1},
		ToLight:    math3d.Vec3{Z: -1},
		ViewDir:    math3d.Vec3{Z: 1},
		LightColor: math3d.Vec3{X: 1, Y: 1, Z: 1},
	})
	wantShadow := [4]float32{0.8 * 0.2, 0.4 * 0.25, 0.2 * 0.4, 1}
	for c := range 4 {
		if math.Abs(float64(shadow[c]-wantShadow[c])) > 1e-5 {
			t.Errorf("shadow[%d] = %g, want %g", c, shadow[c], wantShadow[c])
		}
	}

	// 2. Midtone band (N·L = 0.5 -> band 1 -> 0.5 mix between shadow and 1), view away from half-vector (no specular) and N·V = 1 (no rim).
	// N = (0, 0, 1), L = (sqrt(3)/2, 0, 0.5) -> N·L = 0.5, H = normalize(L + V) = normalize(0.866, 0, 1.5) -> N·H ≈ 0.866, 0.866^16 ≈ 0.10 < 0.6 -> no spec.
	mid := render3d.EvaluateToon(mat, render3d.ToonEvalInput{
		Normal:     math3d.Vec3{Z: 1},
		ToLight:    math3d.Vec3{X: float32(math.Sqrt(3) / 2), Z: 0.5},
		ViewDir:    math3d.Vec3{Z: 1},
		LightColor: math3d.Vec3{X: 1, Y: 1, Z: 1},
	})
	for c := range 3 {
		tint := mat.ShadowColor[c] + (1-mat.ShadowColor[c])*0.5
		want := mat.BaseColorFactor[c] * tint
		if math.Abs(float64(mid[c]-want)) > 1e-4 {
			t.Errorf("mid[%d] = %g, want %g", c, mid[c], want)
		}
	}

	// 3. Full lit + hard specular hit (N = L = V = +Z -> N·L = 1, N·H = 1 -> spec = 1, rim = 0).
	litSpec := render3d.EvaluateToon(mat, render3d.ToonEvalInput{
		Normal:     math3d.Vec3{Z: 1},
		ToLight:    math3d.Vec3{Z: 1},
		ViewDir:    math3d.Vec3{Z: 1},
		LightColor: math3d.Vec3{X: 1, Y: 1, Z: 1},
	})
	for c := range 3 {
		want := min(float32(1), mat.BaseColorFactor[c]+mat.SpecularColor[c])
		if math.Abs(float64(litSpec[c]-want)) > 1e-5 {
			t.Errorf("litSpec[%d] = %g, want %g", c, litSpec[c], want)
		}
	}

	// 4. Grazing Fresnel rim highlight (N = +X, V = +Z -> N·V = 0 -> fresnel = 1 >= 0.7, L = -X -> shadow band, no spec).
	rim := render3d.EvaluateToon(mat, render3d.ToonEvalInput{
		Normal:     math3d.Vec3{X: 1},
		ToLight:    math3d.Vec3{X: -1},
		ViewDir:    math3d.Vec3{Z: 1},
		LightColor: math3d.Vec3{X: 1, Y: 1, Z: 1},
	})
	for c := range 3 {
		want := min(float32(1), mat.BaseColorFactor[c]*mat.ShadowColor[c]+mat.RimColor[c])
		if math.Abs(float64(rim[c]-want)) > 1e-5 {
			t.Errorf("rim[%d] = %g, want %g", c, rim[c], want)
		}
	}

	// 5. Custom RampSample overrides analytical bands and ShadowColor.
	rampTint := [3]float32{0.9, 0.5, 0.1}
	baseTex := [4]float32{0.5, 1.0, 0.5, 0.75}
	rampOut := render3d.EvaluateToon(mat, render3d.ToonEvalInput{
		Normal:     math3d.Vec3{Z: 1},
		ToLight:    math3d.Vec3{Z: -1},
		ViewDir:    math3d.Vec3{Z: 1},
		LightColor: math3d.Vec3{X: 1, Y: 1, Z: 1},
		BaseSample: &baseTex,
		RampSample: &rampTint,
	})
	wantRamp := [4]float32{0.8 * 0.5 * 0.9, 0.4 * 1.0 * 0.5, 0.2 * 0.5 * 0.1, 0.75}
	for c := range 4 {
		if math.Abs(float64(rampOut[c]-wantRamp[c])) > 1e-5 {
			t.Errorf("ramp[%d] = %g, want %g", c, rampOut[c], wantRamp[c])
		}
	}
}

func TestToonMaterialValidate(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	closedTex, err := render3d.UploadTexture(nil, sr.Device(), toonRampImage([]color.NRGBA{{R: 255, G: 255, B: 255, A: 255}}), render3d.TextureUpload{})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}
	_ = closedTex.Close()
	closedSamp, err := render3d.NewGPUSampler(nil, sr.Device(), render3d.SamplerLinearClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler: %v", err)
	}
	_ = closedSamp.Close()
	defaults := toonDefaults(t, nil, sr.Device(), false)

	valid := render3d.DefaultToonMaterial()
	if err := valid.Validate(); err != nil {
		t.Fatalf("DefaultToonMaterial().Validate() = %v", err)
	}
	// Zero value is valid (Bands=0 -> default 3, SpecularPower=0 -> default 32).
	var zero render3d.ToonMaterial
	if err := zero.Validate(); err != nil {
		t.Fatalf("zero ToonMaterial.Validate() = %v", err)
	}
	if zero.EffectiveBands() != render3d.DefaultToonBands || zero.EffectiveSpecularPower() != render3d.DefaultToonSpecularPower {
		t.Fatalf("zero defaults = bands %d, power %g", zero.EffectiveBands(), zero.EffectiveSpecularPower())
	}

	cases := []struct {
		name string
		mut  func(*render3d.ToonMaterial)
	}{
		{"negative base color", func(m *render3d.ToonMaterial) { m.BaseColorFactor[0] = -0.1 }},
		{"nan base color", func(m *render3d.ToonMaterial) { m.BaseColorFactor[1] = nan }},
		{"alpha > 1", func(m *render3d.ToonMaterial) { m.BaseColorFactor[3] = 1.2 }},
		{"negative shadow", func(m *render3d.ToonMaterial) { m.ShadowColor[2] = -0.01 }},
		{"bands 1", func(m *render3d.ToonMaterial) { m.Bands = 1 }},
		{"bands negative", func(m *render3d.ToonMaterial) { m.Bands = -2 }},
		{"bands 9", func(m *render3d.ToonMaterial) { m.Bands = 9 }},
		{"band smoothness > 0.5", func(m *render3d.ToonMaterial) { m.BandSmoothness = 0.6 }},
		{"band smoothness < 0", func(m *render3d.ToonMaterial) { m.BandSmoothness = -0.1 }},
		{"negative specular power", func(m *render3d.ToonMaterial) { m.SpecularPower = -1 }},
		{"inf specular color", func(m *render3d.ToonMaterial) { m.SpecularColor[0] = inf }},
		{"specular threshold > 1", func(m *render3d.ToonMaterial) { m.SpecularThreshold = 1.1 }},
		{"specular smoothness > 0.5", func(m *render3d.ToonMaterial) { m.SpecularSmoothness = 0.7 }},
		{"negative rim color", func(m *render3d.ToonMaterial) { m.RimColor[1] = -0.5 }},
		{"rim threshold < 0", func(m *render3d.ToonMaterial) { m.RimThreshold = -0.1 }},
		{"rim smoothness > 0.5", func(m *render3d.ToonMaterial) { m.RimSmoothness = 0.51 }},
		{"negative outline width", func(m *render3d.ToonMaterial) { m.OutlineWidth = -0.01 }},
		{"outline alpha > 1", func(m *render3d.ToonMaterial) { m.OutlineColor[3] = 2 }},
		{"closed base texture", func(m *render3d.ToonMaterial) { m.BaseColorTexture = closedTex }},
		{"closed base sampler", func(m *render3d.ToonMaterial) { m.BaseColorSampler = closedSamp }},
		{"closed ramp texture", func(m *render3d.ToonMaterial) { m.RampTexture = closedTex }},
		{"closed ramp sampler", func(m *render3d.ToonMaterial) { m.RampSampler = closedSamp }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := valid
			tc.mut(&m)
			err := m.Validate()
			if !errors.Is(err, render3d.ErrInvalidToonMaterial) {
				t.Fatalf("Validate = %v, want ErrInvalidToonMaterial", err)
			}
			if _, err := render3d.NewGPUToonMaterial(nil, sr.Device(), defaults, m, render3d.ToonMaterialOptions{}); !errors.Is(err, render3d.ErrInvalidToonMaterial) {
				t.Fatalf("NewGPUToonMaterial = %v, want ErrInvalidToonMaterial", err)
			}
		})
	}

	// Nil device returns gpu.ErrDeviceLost.
	if _, err := render3d.NewGPUToonMaterial(nil, nil, defaults, valid, render3d.ToonMaterialOptions{}); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("NewGPUToonMaterial(nil dev) = %v, want gpu.ErrDeviceLost", err)
	}
	if _, err := render3d.NewGPUToonMaterial(nil, sr.Device(), nil, valid, render3d.ToonMaterialOptions{}); !errors.Is(err, render3d.ErrInvalidToonMaterial) {
		t.Fatalf("NewGPUToonMaterial(nil defaults) = %v, want ErrInvalidToonMaterial", err)
	}

}

func TestToonLightMatchesLightRigConventions(t *testing.T) {
	nan := float32(math.NaN())
	def := render3d.DefaultToonLight()
	if err := def.Validate(); err != nil {
		t.Fatalf("DefaultToonLight().Validate() = %v", err)
	}
	for name, mut := range map[string]func(*render3d.ToonLight){
		"zero direction":     func(l *render3d.ToonLight) { l.Key.Direction = math3d.Vec3{} },
		"nan direction":      func(l *render3d.ToonLight) { l.Key.Direction.X = nan },
		"negative intensity": func(l *render3d.ToonLight) { l.Key.Intensity = -1 },
		"negative color":     func(l *render3d.ToonLight) { l.Key.Color.Y = -0.1 },
		"negative ambient":   func(l *render3d.ToonLight) { l.Ambient.Intensity = -1 },
	} {
		l := def
		mut(&l)
		if err := l.Validate(); !errors.Is(err, render3d.ErrInvalidLight) {
			t.Errorf("%s: Validate = %v, want ErrInvalidLight", name, err)
		}
	}

	// Direction is the way the rays travel, as in DirectionalLight: light shining down reaches a surface from above.
	down := render3d.ToonLight{Key: render3d.DirectionalLight{Direction: math3d.Vec3{Y: -2}, Color: math3d.Vec3{X: 1, Y: 0.5, Z: 0.25}, Intensity: 2}}
	u := render3d.NewToonSceneUniform(math3d.Mat4Identity(), down)
	if !u.ToLight.ApproxEqual(math3d.Vec3{Y: 1}, 1e-6) {
		t.Errorf("ToLight = %v, want (0, 1, 0)", u.ToLight)
	}
	if u.LightColor != (math3d.Vec3{X: 2, Y: 1, Z: 0.5}) {
		t.Errorf("LightColor = %v, want Color × Intensity", u.LightColor)
	}
	off := down
	off.Key.Intensity = 0
	if c := render3d.NewToonSceneUniform(math3d.Mat4Identity(), off).LightColor; c != (math3d.Vec3{}) {
		t.Errorf("intensity 0 LightColor = %v, want off", c)
	}

	rig := render3d.LightRig{
		Ambient:     render3d.AmbientLight{Color: math3d.Vec3{X: 0.1, Y: 0.1, Z: 0.1}, Intensity: 1},
		Directional: []render3d.DirectionalLight{down.Key, {Direction: math3d.Vec3{X: 1}, Color: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 1}},
	}
	got, err := render3d.ToonLightFromRig(rig)
	if err != nil {
		t.Fatalf("ToonLightFromRig: %v", err)
	}
	if got.Key != down.Key || got.Ambient != rig.Ambient {
		t.Errorf("ToonLightFromRig = %+v, want the first directional light and the ambient fill", got)
	}
	if _, err := render3d.ToonLightFromRig(render3d.LightRig{}); !errors.Is(err, render3d.ErrInvalidLight) {
		t.Errorf("ToonLightFromRig(no directional) = %v, want ErrInvalidLight", err)
	}
}

func TestToonSmoothOutlineNormals(t *testing.T) {
	box := toonBoxMesh(math3d.Vec3{X: 1, Y: 1, Z: 1})
	smoothed := render3d.SmoothOutlineNormals(box)

	// Source normals must stay axis-aligned (unmutated).
	for i, n := range box.Normals {
		if math.Abs(float64(n.X))+math.Abs(float64(n.Y))+math.Abs(float64(n.Z)) > 1.001 {
			t.Fatalf("source box normal %d was mutated: %v", i, n)
		}
	}

	// Every corner of a unit cube meets 3 orthogonal faces, so smoothed normal must point along (±1, ±1, ±1)/sqrt(3).
	invSqrt3 := float32(1.0 / math.Sqrt(3))
	for i, p := range smoothed.Positions {
		n := smoothed.Normals[i]
		want := math3d.Vec3{
			X: float32(math.Copysign(float64(invSqrt3), float64(p.X))),
			Y: float32(math.Copysign(float64(invSqrt3), float64(p.Y))),
			Z: float32(math.Copysign(float64(invSqrt3), float64(p.Z))),
		}
		if !n.ApproxEqual(want, 1e-4) {
			t.Fatalf("vertex %d pos=%v: smoothed normal = %v, want %v", i, p, n, want)
		}
	}

	// Also works when source mesh has no normals (derives from face winding).
	boxNoNormals := box
	boxNoNormals.Normals = nil
	derived := render3d.SmoothOutlineNormals(boxNoNormals)
	if len(derived.Normals) != len(box.Positions) {
		t.Fatalf("derived normal count = %d, want %d", len(derived.Normals), len(box.Positions))
	}
	for i, n := range derived.Normals {
		if !n.ApproxEqual(smoothed.Normals[i], 1e-4) {
			t.Fatalf("derived normal %d = %v, want %v", i, n, smoothed.Normals[i])
		}
	}
}

func TestToonUniformAppendDoesNotAllocate(t *testing.T) {
	mat := render3d.DefaultToonMaterial()
	scene := render3d.NewToonSceneUniform(math3d.Translate(math3d.Vec3{X: 1, Y: 2, Z: 3}), render3d.DefaultToonLight())

	mbuf := make([]byte, 0, render3d.ToonUniformSize)
	sbuf := make([]byte, 0, render3d.ToonSceneUniformSize)
	allocs := testing.AllocsPerRun(50, func() {
		mbuf = mat.AppendUniformData(mbuf[:0])
		sbuf = scene.AppendUniformData(sbuf[:0])
	})
	if allocs != 0 {
		t.Fatalf("AppendUniformData allocs = %v, want 0", allocs)
	}
	if len(mbuf) != render3d.ToonUniformSize {
		t.Fatalf("material uniform size = %d, want %d", len(mbuf), render3d.ToonUniformSize)
	}
	if len(sbuf) != render3d.ToonSceneUniformSize {
		t.Fatalf("scene uniform size = %d, want %d", len(sbuf), render3d.ToonSceneUniformSize)
	}
	// Spot-check packed bands field (float32 index 7 -> byte offset 28).
	bandsF := math.Float32frombits(binary.LittleEndian.Uint32(mbuf[28:32]))
	if bandsF != 3 {
		t.Errorf("packed bands = %g, want 3", bandsF)
	}
}

func BenchmarkToonMaterialAppendUniformData(b *testing.B) {
	mat := render3d.DefaultToonMaterial()
	buf := make([]byte, 0, render3d.ToonUniformSize)
	b.SetBytes(render3d.ToonUniformSize)
	b.ReportAllocs()
	for b.Loop() {
		buf = mat.AppendUniformData(buf[:0])
	}
}

func BenchmarkToonSceneAppendUniformData(b *testing.B) {
	u := render3d.NewToonSceneUniform(math3d.Translate(math3d.Vec3{X: 1, Y: 2, Z: 3}), render3d.DefaultToonLight())
	buf := make([]byte, 0, render3d.ToonSceneUniformSize)
	b.SetBytes(render3d.ToonSceneUniformSize)
	b.ReportAllocs()
	for b.Loop() {
		buf = u.AppendUniformData(buf[:0])
	}
}

func TestToonDrawsInGPUView(t *testing.T) {
	profiles := []struct {
		name string
		opts []nsgotest.Option
	}{
		{"full", nil},
		{"webgl2-gles3", []nsgotest.Option{nsgotest.NoGPUCompute()}},
	}
	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			const size = 96
			clearCol := color.RGBA{R: 20, G: 24, B: 36, A: 255}
			mat := render3d.ToonMaterial{
				BaseColorFactor: [4]float32{0.92, 0.32, 0.24, 1},
				ShadowColor:     [3]float32{0.2, 0.22, 0.36},
				Bands:           3,
				BandSmoothness:  0,
				SpecularColor:   [3]float32{0, 0, 0},
				RimColor:        [3]float32{0, 0, 0},
				OutlineColor:    [4]float32{0.04, 0.04, 0.06, 1},
				OutlineWidth:    0.08,
			}
			toLight := math3d.Vec3{X: 0.7, Y: 0.4, Z: 0.6}.Norm()
			light := render3d.ToonLight{Key: render3d.DirectionalLight{
				Direction: toLight.Scale(-1),
				Color:     math3d.Vec3{X: 1, Y: 1, Z: 1},
				Intensity: 1,
			}}

			var (
				camBuf   *render3d.CameraBuffer
				renderer *render3d.ToonRenderer
				sphere   *render3d.GPUMesh
				gpuMat   *render3d.GPUToonMaterial
				devRef   *gpu.Device
				frames   int
			)
			cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3.4}, math3d.DegToRad(45), 1, 0.1, 20)
			cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())

			opts := append([]nsgotest.Option{nsgotest.Size(size, size), nsgotest.ManualFrames()}, p.opts...)
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return gpu.View(cx, gpu.ViewProps{
					TestID:      "toon-view",
					Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
					DepthFormat: gpu.FormatDepth24Plus,
					OnReady: func(dev *gpu.Device) {
						devRef = dev
						var err error
						camBuf, err = render3d.NewCameraBuffer(cx, dev)
						if err != nil {
							t.Fatalf("NewCameraBuffer: %v", err)
						}
						renderer, err = render3d.NewToonRenderer(cx, dev, render3d.ToonRendererOptions{
							CameraLayout: camBuf.Layout(),
							DepthFormat:  gpu.FormatDepth24Plus,
						})
						if err != nil {
							t.Fatalf("NewToonRenderer: %v", err)
						}
						sphere, err = render3d.UploadMesh(cx, dev, toonSphereMesh(math3d.Vec3Zero(), 1.0, 20, 32), render3d.MeshUpload{Label: "sphere"})
						if err != nil {
							t.Fatalf("UploadMesh: %v", err)
						}
						gpuMat, err = renderer.NewMaterial(cx, dev, toonDefaults(t, cx, dev, false), mat, "hero")
						if err != nil {
							t.Fatalf("NewMaterial: %v", err)
						}
					},
					OnFrame: func(frame *gpu.Frame) {
						frames++
						camBuf.WriteCamera(cam)
						pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
						pass.ClearColor(core.RGB(0x141824))
						pass.ClearDepth(1)
						if err := renderer.Draw(devRef, pass, camBuf.BindGroup(), light, render3d.ToonDraw{
							Mesh:     sphere,
							Material: gpuMat,
						}); err != nil {
							t.Fatalf("ToonRenderer.Draw: %v", err)
						}
						pass.End()
						frame.Submit()
					},
				})
			}, opts...)

			app.Advance(16 * time.Millisecond)
			if frames == 0 {
				t.Fatal("OnFrame did not run")
			}
			img, err := app.GPUReadPixels("id:toon-view")
			if err != nil {
				t.Fatalf("GPUReadPixels: %v", err)
			}

			// Corner must remain background clear color.
			if got := img.RGBAAt(2, 2); !toonNearColor(got, clearCol, 2) {
				t.Fatalf("corner pixel = %v, want %v", got, clearCol)
			}

			// Compute expected 3 band colors via CPU EvaluateToon and verify all 3 bands plus crisp dark outline pixels appear on screen!
			evalBand := func(ndl float32) color.RGBA {
				// Construct unit normal with dot(N, L) == ndl.
				l := toLight
				// Pick perpendicular vector to L.
				perp := l.Cross(math3d.Vec3Up())
				if perp.LenSq() < 1e-4 {
					perp = l.Cross(math3d.Vec3Right())
				}
				perp = perp.Norm()
				sinA := float32(math.Sqrt(math.Max(0, 1-float64(ndl*ndl))))
				n := l.Scale(ndl).Add(perp.Scale(sinA))
				return toonRGBAFromLinear(render3d.EvaluateToon(mat, render3d.ToonEvalInput{
					Normal:     n,
					ToLight:    l,
					ViewDir:    math3d.Vec3{Z: 1},
					LightColor: math3d.Vec3{X: 1, Y: 1, Z: 1},
				}))
			}
			wantBand0 := evalBand(0.05) // shadow band
			wantBand1 := evalBand(0.50) // midtone band
			wantBand2 := evalBand(0.95) // lit band
			wantOutline := toonRGBAFromLinear(mat.OutlineColor)

			var countBand0, countBand1, countBand2, countOutline int
			for y := range size {
				for x := range size {
					c := img.RGBAAt(x, y)
					switch {
					case toonNearColor(c, wantOutline, 4):
						countOutline++
					case toonNearColor(c, wantBand0, 4):
						countBand0++
					case toonNearColor(c, wantBand1, 4):
						countBand1++
					case toonNearColor(c, wantBand2, 4):
						countBand2++
					}
				}
			}
			if countOutline < 80 {
				t.Errorf("outline pixel count = %d, want >= 80 crisp silhouette pixels", countOutline)
			}
			if countBand0 < 100 || countBand1 < 100 || countBand2 < 100 {
				t.Errorf("discrete band pixel counts = [%d, %d, %d], want >= 100 each (band0=%v band1=%v band2=%v)",
					countBand0, countBand1, countBand2, wantBand0, wantBand1, wantBand2)
			}
		})
	}
}

func TestRetainedToonDrawsAfterDeviceLoss(t *testing.T) {
	const size = 64
	mat := render3d.ToonMaterial{
		BaseColorFactor: [4]float32{0.2, 0.85, 0.55, 1},
		ShadowColor:     [3]float32{0.15, 0.25, 0.35},
		Bands:           3,
		OutlineColor:    [4]float32{0.05, 0.05, 0.05, 1},
		OutlineWidth:    0.08,
	}
	light := render3d.ToonLight{Key: render3d.DirectionalLight{
		Direction: math3d.Vec3{Z: -1},
		Color:     math3d.Vec3{X: 1, Y: 1, Z: 1},
		Intensity: 1,
	}}
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3.4}, math3d.DegToRad(45), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())

	var (
		camBuf   *render3d.CameraBuffer
		renderer *render3d.ToonRenderer
		sphere   *render3d.GPUMesh
		gpuMat   *render3d.GPUToonMaterial
		devRef   *gpu.Device
	)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return gpu.View(cx, gpu.ViewProps{
			TestID:      "toon-retained",
			Style:       core.Style{Width: core.Pt(size), Height: core.Pt(size)},
			DepthFormat: gpu.FormatDepth24Plus,
			OnReady: func(dev *gpu.Device) {
				devRef = dev
				var err error
				camBuf, err = render3d.NewCameraBuffer(cx, dev)
				if err != nil {
					t.Fatalf("NewCameraBuffer: %v", err)
				}
				if renderer == nil {
					renderer, err = render3d.NewToonRenderer(cx, dev, render3d.ToonRendererOptions{
						CameraLayout: camBuf.Layout(),
						DepthFormat:  gpu.FormatDepth24Plus,
						Retained:     true,
					})
					if err != nil {
						t.Fatalf("NewToonRenderer: %v", err)
					}
					sphere, err = render3d.UploadMesh(cx, dev, toonSphereMesh(math3d.Vec3Zero(), 1.0, 16, 24), render3d.MeshUpload{Label: "sphere", Retained: true})
					if err != nil {
						t.Fatalf("UploadMesh: %v", err)
					}
					gpuMat, err = renderer.NewMaterial(cx, dev, toonDefaults(t, cx, dev, true), mat, "retained-mat")
					if err != nil {
						t.Fatalf("NewMaterial: %v", err)
					}
				}
			},
			OnFrame: func(frame *gpu.Frame) {
				camBuf.WriteCamera(cam)
				pass := frame.BeginRenderPass(gpu.RenderPassDesc{})
				pass.ClearColor(core.RGB(0x10141c))
				pass.ClearDepth(1)
				if err := renderer.Draw(devRef, pass, camBuf.BindGroup(), light, render3d.ToonDraw{
					Mesh:     sphere,
					Material: gpuMat,
				}); err != nil {
					t.Fatalf("Draw: %v", err)
				}
				pass.End()
				frame.Submit()
			},
		})
	}, nsgotest.Size(size, size), nsgotest.ManualFrames())

	app.Advance(16 * time.Millisecond)
	before, err := app.GPUReadPixels("id:toon-retained")
	if err != nil {
		t.Fatalf("GPUReadPixels before loss: %v", err)
	}
	wantLit := toonRGBAFromLinear(mat.BaseColorFactor)
	if got := before.RGBAAt(size/2, size/2); !toonNearColor(got, wantLit, 4) {
		t.Fatalf("center before loss = %v, want %v", got, wantLit)
	}

	firstMat := gpuMat
	firstRenderer := renderer
	app.TriggerGPUDeviceLost("id:toon-retained", nsgotest.GPULostContextLost, "context lost")
	if firstMat.Valid() || firstRenderer.Valid() {
		t.Fatal("retained material/renderer still valid while device is lost")
	}
	app.RestoreGPUDevice("id:toon-retained")
	if !firstMat.Valid() || !firstRenderer.Valid() {
		t.Fatalf("retained resources not restored: mat=%v renderer=%v", firstMat.Valid(), firstRenderer.Valid())
	}

	// Update material color after restore using the new device handle to prove Update stays live after device loss!
	updated := mat
	updated.BaseColorFactor = [4]float32{0.3, 0.5, 0.95, 1}
	app.Post(func() {
		if err := firstMat.Update(devRef, updated); err != nil {
			t.Errorf("Update after device restore: %v", err)
		}
	})
	app.Settle()
	app.Advance(16 * time.Millisecond)

	after, err := app.GPUReadPixels("id:toon-retained")
	if err != nil {
		t.Fatalf("GPUReadPixels after restore: %v", err)
	}
	wantUpdated := toonRGBAFromLinear(updated.BaseColorFactor)
	if got := after.RGBAAt(size/2, size/2); !toonNearColor(got, wantUpdated, 4) {
		t.Fatalf("center after restore + Update = %v, want %v", got, wantUpdated)
	}
}

func TestToonMaterialScopeDisposalAndUpdateErrors(t *testing.T) {
	var (
		gpuMat   *render3d.GPUToonMaterial
		renderer *render3d.ToonRenderer
		show     *core.Signal[bool]
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
					renderer, err = render3d.NewToonRenderer(cx, dev, render3d.ToonRendererOptions{
						CameraLayout: camBuf.Layout(),
					})
					if err != nil {
						t.Fatalf("NewToonRenderer: %v", err)
					}
					gpuMat, err = renderer.NewMaterial(cx, dev, toonDefaults(t, cx, dev, false), render3d.DefaultToonMaterial(), "scoped")
					if err != nil {
						t.Fatalf("NewMaterial: %v", err)
					}
				},
			})
		}}
	}, nsgotest.Size(8, 8), nsgotest.ManualFrames())

	if !gpuMat.Valid() || !renderer.Valid() {
		t.Fatal("expected valid toon resources while mounted")
	}
	if gpuMat.ByteSize() < render3d.ToonUniformSize {
		t.Errorf("ByteSize = %d, want >= %d", gpuMat.ByteSize(), render3d.ToonUniformSize)
	}

	app.Post(func() { show.Set(false) })
	app.Settle()
	if gpuMat.Valid() || renderer.Valid() {
		t.Error("toon resources remained valid after scope disposal")
	}

	// Verify Update rejects texture swaps and nil/closed receivers.
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()
	tex, err := render3d.UploadTexture(nil, dev, toonRampImage([]color.NRGBA{{R: 255, G: 255, B: 255, A: 255}}), render3d.TextureUpload{})
	if err != nil {
		t.Fatalf("UploadTexture: %v", err)
	}
	defer func() { _ = tex.Close() }()
	m, err := render3d.NewGPUToonMaterial(nil, dev, toonDefaults(t, nil, dev, false), render3d.DefaultToonMaterial(), render3d.ToonMaterialOptions{})
	if err != nil {
		t.Fatalf("NewGPUToonMaterial: %v", err)
	}
	swapped := render3d.DefaultToonMaterial()
	swapped.BaseColorTexture = tex
	if err := m.Update(dev, swapped); !errors.Is(err, render3d.ErrInvalidToonMaterial) {
		t.Fatalf("Update texture swap = %v, want ErrInvalidToonMaterial", err)
	}
	if err := m.Update(nil, render3d.DefaultToonMaterial()); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("Update nil dev = %v, want gpu.ErrDeviceLost", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := m.Update(dev, render3d.DefaultToonMaterial()); !errors.Is(err, render3d.ErrInvalidToonMaterial) {
		t.Fatalf("Update closed mat = %v, want ErrInvalidToonMaterial", err)
	}
	var nilMat *render3d.GPUToonMaterial
	if nilMat.Valid() || nilMat.Close() != nil || nilMat.ByteSize() != 0 {
		t.Error("nil GPUToonMaterial must be safe and inactive")
	}
}

func TestToonRampTextureAndClipSpaceOutline(t *testing.T) {
	const size = 64
	sr := gpu.NewSoftwareRenderer(size, size, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()

	renderer, err := render3d.NewToonRenderer(nil, dev, render3d.ToonRendererOptions{
		CameraLayout: camBuf.Layout(),
		DepthFormat:  gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewToonRenderer: %v", err)
	}
	defer func() { _ = renderer.Close() }()

	// 2-step 1D cel ramp: left half deep magenta-purple, right half bright gold.
	rampImg := toonRampImage([]color.NRGBA{
		{R: 120, G: 30, B: 160, A: 255},
		{R: 250, G: 220, B: 60, A: 255},
	})
	rampTex, err := render3d.UploadTexture(nil, dev, rampImg, render3d.TextureUpload{Label: "ramp"})
	if err != nil {
		t.Fatalf("UploadTexture ramp: %v", err)
	}
	defer func() { _ = rampTex.Close() }()
	rampSamp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler ramp: %v", err)
	}
	defer func() { _ = rampSamp.Close() }()

	mat := render3d.ToonMaterial{
		BaseColorFactor:  [4]float32{1, 1, 1, 1},
		RampTexture:      rampTex,
		RampSampler:      rampSamp,
		OutlineColor:     [4]float32{0.02, 0.02, 0.04, 1},
		OutlineWidth:     0.05,
		OutlineClipSpace: true,
	}
	gpuMat, err := renderer.NewMaterial(nil, dev, toonDefaults(t, nil, dev, false), mat, "ramp-sphere")
	if err != nil {
		t.Fatalf("NewMaterial: %v", err)
	}
	defer func() { _ = gpuMat.Close() }()

	sphere, err := render3d.UploadMesh(nil, dev, toonSphereMesh(math3d.Vec3Zero(), 1.0, 18, 28), render3d.MeshUpload{Label: "sphere"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	defer func() { _ = sphere.Close() }()

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 3.4}, math3d.DegToRad(45), 1, 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	light := render3d.ToonLight{Key: render3d.DirectionalLight{
		Direction: math3d.Vec3{X: -1}, // shines toward -X: lights the right half, darkens the left half
		Color:     math3d.Vec3{X: 1, Y: 1, Z: 1},
		Intensity: 1,
	}}

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	if err := renderer.Draw(dev, pass, camBuf.BindGroup(), light, render3d.ToonDraw{Mesh: sphere, Material: gpuMat}); err != nil {
		t.Fatalf("Draw: %v", err)
	}
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	img := sr.Image()
	wantDark := color.RGBA{R: 120, G: 30, B: 160, A: 255}
	wantGold := color.RGBA{R: 250, G: 220, B: 60, A: 255}
	wantOutline := toonRGBAFromLinear(mat.OutlineColor)

	var darkPx, goldPx, outlinePx int
	for y := range size {
		for x := range size {
			c := img.RGBAAt(x, y)
			switch {
			case toonNearColor(c, wantDark, 4):
				darkPx++
			case toonNearColor(c, wantGold, 4):
				goldPx++
			case toonNearColor(c, wantOutline, 4):
				outlinePx++
			}
		}
	}
	if darkPx < 100 || goldPx < 100 {
		t.Fatalf("ramp band pixel counts: dark=%d gold=%d, want >= 100 each", darkPx, goldPx)
	}
	if outlinePx < 40 {
		t.Fatalf("clip-space outline pixel count = %d, want >= 40", outlinePx)
	}
}

func TestToonSceneMatchesReference(t *testing.T) {
	const (
		width  = 160
		height = 120
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()

	renderer, err := render3d.NewToonRenderer(nil, dev, render3d.ToonRendererOptions{
		Label:        "toon-ref",
		CameraLayout: camBuf.Layout(),
		DepthFormat:  gpu.FormatDepth24Plus,
	})
	if err != nil {
		t.Fatalf("NewToonRenderer: %v", err)
	}
	defer func() { _ = renderer.Close() }()

	// Geometry:
	// 1) Hero sphere at (-1.15, 0.85, 0.25): smooth 3-band coral-red cel shading with hard specular + Fresnel rim + ink outline.
	// 2) Companion box at (1.15, 0.65, -0.15): 4-band teal cel shading with smoothed inverted-hull outline normals.
	// 3) Mini ramp-shaded sphere at (0.1, 0.45, 1.35): custom 3-step artist cel ramp texture + ink outline.
	// 4) Ground plane at y = 0: soft studio floor without outline.
	sphereMesh, err := render3d.UploadMesh(nil, dev, toonSphereMesh(math3d.Vec3Zero(), 0.85, 24, 36), render3d.MeshUpload{Label: "hero-sphere"})
	if err != nil {
		t.Fatalf("UploadMesh sphere: %v", err)
	}
	boxCPU := toonBoxMesh(math3d.Vec3{X: 0.65, Y: 0.65, Z: 0.65})
	boxMesh, err := render3d.UploadMesh(nil, dev, boxCPU, render3d.MeshUpload{Label: "box"})
	if err != nil {
		t.Fatalf("UploadMesh box: %v", err)
	}
	boxOutlineMesh, err := render3d.UploadMesh(nil, dev, render3d.SmoothOutlineNormals(boxCPU), render3d.MeshUpload{Label: "box-outline"})
	if err != nil {
		t.Fatalf("UploadMesh box-outline: %v", err)
	}
	miniSphereMesh, err := render3d.UploadMesh(nil, dev, toonSphereMesh(math3d.Vec3Zero(), 0.45, 20, 28), render3d.MeshUpload{Label: "mini-sphere"})
	if err != nil {
		t.Fatalf("UploadMesh mini-sphere: %v", err)
	}
	floorMesh, err := render3d.UploadMesh(nil, dev, toonPlaneMesh(5.5), render3d.MeshUpload{Label: "floor"})
	if err != nil {
		t.Fatalf("UploadMesh floor: %v", err)
	}

	heroMatSpec := render3d.ToonMaterial{
		BaseColorFactor:    [4]float32{0.94, 0.34, 0.24, 1},
		ShadowColor:        [3]float32{0.24, 0.22, 0.40},
		Bands:              3,
		BandSmoothness:     0,
		SpecularColor:      [3]float32{0.9, 0.9, 0.85},
		SpecularPower:      36,
		SpecularThreshold:  0.55,
		SpecularSmoothness: 0.02,
		RimColor:           [3]float32{0.45, 0.72, 0.95},
		RimThreshold:       0.68,
		RimSmoothness:      0.03,
		OutlineColor:       [4]float32{0.05, 0.05, 0.08, 1},
		OutlineWidth:       0.045,
	}
	heroMat, err := renderer.NewMaterial(nil, dev, toonDefaults(t, nil, dev, false), heroMatSpec, "hero")
	if err != nil {
		t.Fatalf("NewMaterial hero: %v", err)
	}

	boxMatSpec := render3d.ToonMaterial{
		BaseColorFactor:    [4]float32{0.20, 0.76, 0.64, 1},
		ShadowColor:        [3]float32{0.18, 0.30, 0.42},
		Bands:              4,
		BandSmoothness:     0.03,
		SpecularColor:      [3]float32{0.35, 0.45, 0.45},
		SpecularPower:      24,
		SpecularThreshold:  0.65,
		SpecularSmoothness: 0.02,
		RimColor:           [3]float32{0.16, 0.32, 0.40},
		RimThreshold:       0.76,
		RimSmoothness:      0.04,
		OutlineColor:       [4]float32{0.04, 0.06, 0.09, 1},
		OutlineWidth:       0.055,
	}
	boxMat, err := renderer.NewMaterial(nil, dev, toonDefaults(t, nil, dev, false), boxMatSpec, "box")
	if err != nil {
		t.Fatalf("NewMaterial box: %v", err)
	}

	rampTex, err := render3d.UploadTexture(nil, dev, toonRampImage([]color.NRGBA{
		{R: 55, G: 48, B: 110, A: 255},
		{R: 185, G: 115, B: 90, A: 255},
		{R: 255, G: 230, B: 140, A: 255},
	}), render3d.TextureUpload{Label: "warm-ramp"})
	if err != nil {
		t.Fatalf("UploadTexture ramp: %v", err)
	}
	rampSamp, err := render3d.NewGPUSampler(nil, dev, render3d.SamplerNearestClamp())
	if err != nil {
		t.Fatalf("NewGPUSampler ramp: %v", err)
	}
	miniMatSpec := render3d.ToonMaterial{
		BaseColorFactor: [4]float32{1, 1, 1, 1},
		RampTexture:     rampTex,
		RampSampler:     rampSamp,
		OutlineColor:    [4]float32{0.06, 0.05, 0.09, 1},
		OutlineWidth:    0.035,
	}
	miniMat, err := renderer.NewMaterial(nil, dev, toonDefaults(t, nil, dev, false), miniMatSpec, "mini")
	if err != nil {
		t.Fatalf("NewMaterial mini: %v", err)
	}

	floorMatSpec := render3d.ToonMaterial{
		BaseColorFactor: [4]float32{0.58, 0.62, 0.70, 1},
		ShadowColor:     [3]float32{0.35, 0.38, 0.48},
		Bands:           3,
	}
	floorMat, err := renderer.NewMaterial(nil, dev, toonDefaults(t, nil, dev, false), floorMatSpec, "floor")
	if err != nil {
		t.Fatalf("NewMaterial floor: %v", err)
	}

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{X: 0.25, Y: 2.35, Z: 5.1}, math3d.DegToRad(44), float32(width)/float32(height), 0.1, 40)
	cam.LookAt(math3d.Vec3{Y: 0.65}, math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	light := render3d.ToonLight{
		Key: render3d.DirectionalLight{
			Direction: math3d.Vec3{X: 0.55, Y: 0.75, Z: 0.45}.Norm().Scale(-1),
			Color:     math3d.Vec3{X: 1, Y: 0.98, Z: 0.94},
			Intensity: 1,
		},
		Ambient: render3d.AmbientLight{Color: math3d.Vec3{X: 0.04, Y: 0.05, Z: 0.08}, Intensity: 1},
	}

	boxTransform := math3d.Translate(math3d.Vec3{X: 1.15, Y: 0.65, Z: -0.15}).Mul(math3d.RotateY(math3d.DegToRad(-28)))
	draws := []render3d.ToonDraw{
		{Mesh: floorMesh, Material: floorMat},
		{Mesh: sphereMesh, Material: heroMat, Transform: math3d.Translate(math3d.Vec3{X: -1.15, Y: 0.85, Z: 0.25})},
		{Mesh: boxMesh, OutlineMesh: boxOutlineMesh, Material: boxMat, Transform: boxTransform},
		{Mesh: miniSphereMesh, Material: miniMat, Transform: math3d.Translate(math3d.Vec3{X: 0.1, Y: 0.45, Z: 1.35})},
	}

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		Label:            "toon-scene-reference",
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x161b28)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	if err := renderer.Draw(dev, pass, camBuf.BindGroup(), light, draws...); err != nil {
		t.Fatalf("ToonRenderer.Draw: %v", err)
	}
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}

	got := sr.Image()

	// Verify crisp silhouette ink outline pixels and bright specular pixels are rendered in the scene.
	heroOutline := toonRGBAFromLinear(heroMatSpec.OutlineColor)
	var outlineCount, brightSpecCount int
	for y := range height {
		for x := range width {
			c := got.RGBAAt(x, y)
			if toonNearColor(c, heroOutline, 8) {
				outlineCount++
			}
			if c.R >= 245 && c.G >= 220 && c.B >= 200 {
				brightSpecCount++
			}
		}
	}
	if outlineCount < 150 {
		t.Errorf("outline pixel count = %d, want >= 150", outlineCount)
	}
	if brightSpecCount < 10 {
		t.Errorf("cel specular highlight pixel count = %d, want >= 10", brightSpecCount)
	}

	assertReference(t, "toon_scene", got)
}

func TestToonRendererDrawsSeveralTimesPerSubmit(t *testing.T) {
	const (
		width  = 128
		height = 64
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()

	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()
	renderer, err := render3d.NewToonRenderer(nil, dev, render3d.ToonRendererOptions{
		CameraLayout:    camBuf.Layout(),
		DepthFormat:     gpu.FormatDepth24Plus,
		InitialCapacity: 1,
	})
	if err != nil {
		t.Fatalf("NewToonRenderer: %v", err)
	}
	defer func() { _ = renderer.Close() }()
	mat := render3d.ToonMaterial{BaseColorFactor: [4]float32{1, 1, 1, 1}, ShadowColor: [3]float32{1, 1, 1}}
	gpuMat, err := renderer.NewMaterial(nil, dev, toonDefaults(t, nil, dev, false), mat, "white")
	if err != nil {
		t.Fatalf("NewMaterial: %v", err)
	}
	defer func() { _ = gpuMat.Close() }()
	sphere, err := render3d.UploadMesh(nil, dev, toonSphereMesh(math3d.Vec3Zero(), 0.8, 12, 18), render3d.MeshUpload{Label: "sphere"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	defer func() { _ = sphere.Close() }()

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 5}, math3d.DegToRad(45), float32(width)/float32(height), 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)
	light := render3d.DefaultToonLight()
	left := render3d.ToonDraw{Mesh: sphere, Material: gpuMat, Transform: math3d.Translate(math3d.Vec3{X: -1.5})}
	right := render3d.ToonDraw{Mesh: sphere, Material: gpuMat, Transform: math3d.Translate(math3d.Vec3{X: 1.5})}
	// Empty draws are skipped but still take a slot, so the second call outgrows the 32-slot buffer while the first call's draw still binds it.
	grow := append([]render3d.ToonDraw{right}, make([]render3d.ToonDraw, 40)...)

	clearCol := color.RGBA{R: 0x10, G: 0x14, B: 0x1c, A: 255}
	for _, tc := range []struct {
		name   string
		second []render3d.ToonDraw
	}{
		{"same buffer", []render3d.ToonDraw{right}},
		{"buffer grows", grow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			renderer.Begin()
			enc := dev.BeginEncoder()
			pass := enc.BeginRenderPass(gpu.RenderPassDesc{
				ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x10141c)}},
				DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
			})
			if err := renderer.Draw(dev, pass, camBuf.BindGroup(), light, left); err != nil {
				t.Fatalf("first Draw: %v", err)
			}
			if err := renderer.Draw(dev, pass, camBuf.BindGroup(), light, tc.second...); err != nil {
				t.Fatalf("second Draw: %v", err)
			}
			pass.End()
			dev.Submit(enc.Finish())
			sr.WaitIdle()
			if err := sr.LastError(); err != nil {
				t.Fatalf("software renderer: %v", err)
			}
			img := sr.Image()
			for _, x := range []int{41, 87} {
				if got := img.RGBAAt(x, height/2); toonNearColor(got, clearCol, 2) {
					t.Errorf("pixel (%d, %d) = %v, want a sphere", x, height/2, got)
				}
			}
		})
	}
}

func TestToonRendererDrawReportsInvalidInputs(t *testing.T) {
	sr := gpu.NewSoftwareRenderer(8, 8, gpu.FormatUndefined)
	defer sr.Close()
	dev := sr.Device()
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()
	renderer, err := render3d.NewToonRenderer(nil, dev, render3d.ToonRendererOptions{CameraLayout: camBuf.Layout()})
	if err != nil {
		t.Fatalf("NewToonRenderer: %v", err)
	}
	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore}}})
	light := render3d.DefaultToonLight()
	if err := renderer.Draw(nil, pass, camBuf.BindGroup(), light); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("Draw(nil device) = %v, want gpu.ErrDeviceLost", err)
	}
	if err := renderer.Draw(dev, nil, camBuf.BindGroup(), light); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("Draw(nil pass) = %v, want gpu.ErrInvalidDescriptor", err)
	}
	if err := renderer.Draw(dev, pass, nil, light); !errors.Is(err, gpu.ErrInvalidDescriptor) {
		t.Errorf("Draw(nil camera group) = %v, want gpu.ErrInvalidDescriptor", err)
	}
	if err := renderer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := renderer.Draw(dev, pass, camBuf.BindGroup(), light); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("Draw(closed renderer) = %v, want gpu.ErrDeviceLost", err)
	}
	var nilRenderer *render3d.ToonRenderer
	if err := nilRenderer.Draw(dev, pass, camBuf.BindGroup(), light); !errors.Is(err, gpu.ErrDeviceLost) {
		t.Errorf("Draw(nil renderer) = %v, want gpu.ErrDeviceLost", err)
	}
	pass.End()
	enc.Finish()
}

func TestToonClipSpaceOutlineIsEvenOnWideViewport(t *testing.T) {
	const (
		width  = 160
		height = 60
	)
	sr := gpu.NewSoftwareRenderer(width, height, gpu.FormatDepth24Plus)
	defer sr.Close()
	dev := sr.Device()
	camBuf, err := render3d.NewCameraBuffer(nil, dev)
	if err != nil {
		t.Fatalf("NewCameraBuffer: %v", err)
	}
	defer func() { _ = camBuf.Close() }()
	renderer, err := render3d.NewToonRenderer(nil, dev, render3d.ToonRendererOptions{CameraLayout: camBuf.Layout(), DepthFormat: gpu.FormatDepth24Plus})
	if err != nil {
		t.Fatalf("NewToonRenderer: %v", err)
	}
	defer func() { _ = renderer.Close() }()
	mat := render3d.ToonMaterial{
		BaseColorFactor:  [4]float32{1, 1, 1, 1},
		ShadowColor:      [3]float32{1, 1, 1},
		OutlineColor:     [4]float32{0, 0, 0, 1},
		OutlineWidth:     0.2, // 0.2 × 60 / 2 = 6 pixels
		OutlineClipSpace: true,
	}
	gpuMat, err := renderer.NewMaterial(nil, dev, toonDefaults(t, nil, dev, false), mat, "outlined")
	if err != nil {
		t.Fatalf("NewMaterial: %v", err)
	}
	sphere, err := render3d.UploadMesh(nil, dev, toonSphereMesh(math3d.Vec3Zero(), 1, 24, 48), render3d.MeshUpload{Label: "sphere"})
	if err != nil {
		t.Fatalf("UploadMesh: %v", err)
	}
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3{Z: 6}, math3d.DegToRad(45), float32(width)/float32(height), 0.1, 20)
	cam.LookAt(math3d.Vec3Zero(), math3d.Vec3Up())
	camBuf.WriteCamera(cam)

	enc := dev.BeginEncoder()
	pass := enc.BeginRenderPass(gpu.RenderPassDesc{
		ColorAttachments: []gpu.ColorAttachment{{LoadOp: gpu.LoadClear, StoreOp: gpu.StoreStore, ClearColor: core.RGB(0x808080)}},
		DepthStencil:     &gpu.DepthStencilAttachment{DepthLoadOp: gpu.LoadClear, DepthStoreOp: gpu.StoreDiscard, ClearDepth: 1},
	})
	light := render3d.ToonLight{Key: render3d.DirectionalLight{Direction: math3d.Vec3{Z: -1}, Color: math3d.Vec3{X: 1, Y: 1, Z: 1}, Intensity: 1}}
	if err := renderer.Draw(dev, pass, camBuf.BindGroup(), light, render3d.ToonDraw{Mesh: sphere, Material: gpuMat}); err != nil {
		t.Fatalf("Draw: %v", err)
	}
	pass.End()
	dev.Submit(enc.Finish())
	sr.WaitIdle()
	if err := sr.LastError(); err != nil {
		t.Fatalf("software renderer: %v", err)
	}
	img := sr.Image()
	black := color.RGBA{A: 255}
	run := func(x, y, dx, dy int) int {
		for x >= 0 && y >= 0 && x < width && y < height && !toonNearColor(img.RGBAAt(x, y), black, 8) {
			x, y = x+dx, y+dy
		}
		n := 0
		for x >= 0 && y >= 0 && x < width && y < height && toonNearColor(img.RGBAAt(x, y), black, 8) {
			n++
			x, y = x+dx, y+dy
		}
		return n
	}
	horizontal := run(0, height/2, 1, 0)
	vertical := run(width/2, 0, 0, 1)
	if horizontal < 5 || horizontal > 7 || vertical < 5 || vertical > 7 {
		t.Fatalf("outline thickness = %d px horizontally, %d px vertically, want about 6 in both", horizontal, vertical)
	}
}
