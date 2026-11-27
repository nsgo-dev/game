package particles_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/particles"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

var sheet = &sprite.Atlas{
	Image: core.ImageSource{URI: "asset:particles.png"},
	Frames: map[string]sprite.Frame{
		"spark": {Name: "spark", X: 0, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
		"smoke": {Name: "smoke", X: 8, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		"ember": {Name: "ember", X: 24, Y: 0, W: 6, H: 6, OffsetX: 1, OffsetY: 1, Width: 8, Height: 8},
	},
}

func testColorCurve() particles.ColorCurve {
	return particles.ColorCurve{
		{T: 0, Color: core.RGBA(0xff8000, 1)},
		{T: 0.5, Color: core.RGBA(0xff0040, 0.8)},
		{T: 1, Color: core.RGBA(0x202020, 0)},
	}
}

func newConfiguredEmitter(seed uint64) *particles.Emitter {
	return &particles.Emitter{
		Atlas: sheet,
		Frames: []sprite.Frame{
			sheet.Frames["spark"],
			sheet.Frames["smoke"],
			sheet.Frames["ember"],
		},
		X:                  120,
		Y:                  80,
		Shape:              particles.Circle(15),
		Rate:               45,
		MaxParticles:       128,
		LifetimeMin:        300 * time.Millisecond,
		LifetimeMax:        900 * time.Millisecond,
		SpeedMin:           20,
		SpeedMax:           140,
		AngleMin:           -math.Pi,
		AngleMax:           math.Pi,
		GravityX:           10,
		GravityY:           98,
		InitialRotationMin: 0,
		InitialRotationMax: 2 * math.Pi,
		RotationSpeedMin:   -3,
		RotationSpeedMax:   3,
		SizeCurve: particles.FloatCurve{
			{T: 0, Value: 0.5},
			{T: 0.5, Value: 1.5},
			{T: 1, Value: 0},
		},
		AlphaCurve: particles.FloatCurve{
			{T: 0, Value: 1},
			{T: 1, Value: 0},
		},
		RotationCurve: particles.FloatCurve{
			{T: 0, Value: 0},
			{T: 1, Value: 0.5},
		},
		ColorCurve: testColorCurve(),
		Seed:       seed,
		Z:          2,
	}
}

func TestDeterminismUnderLoop(t *testing.T) {
	e1 := newConfiguredEmitter(42)
	e2 := newConfiguredEmitter(42)
	eDiff := newConfiguredEmitter(43)

	var tick *core.Signal[int]
	var loop *game.Loop
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		tick = core.NewSignal(cx, 0)
		loop = &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				e1.Update(dt)
				e2.Update(dt)
				eDiff.Update(dt)
			},
			Draw: func(float64) {
				tick.Set(tick.Get() + 1)
			},
		}
		loop.Run(cx)
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "e1", Style: core.Style{Width: core.Pt(300), Height: core.Pt(300)}, Draw: func(d *core.Drawing, _ core.Size) {
				_ = tick.Get()
				e1.Draw(d)
			}},
			core.Canvas{TestID: "e2", Style: core.Style{Width: core.Pt(300), Height: core.Pt(300)}, Draw: func(d *core.Drawing, _ core.Size) {
				_ = tick.Get()
				e2.Draw(d)
			}},
			core.Canvas{TestID: "diff", Style: core.Style{Width: core.Pt(300), Height: core.Pt(300)}, Draw: func(d *core.Drawing, _ core.Size) {
				_ = tick.Get()
				eDiff.Draw(d)
			}},
		}}
	})

	app.Advance(time.Second)

	if e1.Count() == 0 {
		t.Fatal("expected live particles after 1s")
	}
	assertParticlesIdentical(t, e1.Particles(), e2.Particles())

	ops1 := app.Find("e1").Ops
	ops2 := app.Find("e2").Ops
	opsDiff := app.Find("diff").Ops
	if len(ops1) == 0 {
		t.Fatal("expected draw ops from emitter")
	}
	if !reflect.DeepEqual(ops1, ops2) {
		t.Fatalf("same-seed emitters drew different ops:\ne1: %+v\ne2: %+v", ops1, ops2)
	}
	if reflect.DeepEqual(e1.Particles(), eDiff.Particles()) {
		t.Fatal("different seeds produced identical particles")
	}
	if reflect.DeepEqual(ops1, opsDiff) {
		t.Fatal("different seeds produced identical draw ops")
	}

	// Reset replays bit-for-bit identically.
	snapshot := append([]particles.Particle(nil), e1.Particles()...)
	e1.Reset()
	if e1.Count() != 0 {
		t.Fatalf("Count after Reset = %d, want 0", e1.Count())
	}
	step := time.Second / 60
	for range 60 {
		e1.Update(step)
	}
	assertParticlesIdentical(t, snapshot, e1.Particles())
}

func assertParticlesIdentical(t *testing.T, a, b []particles.Particle) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("particle count %d != %d", len(a), len(b))
	}
	for i := range a {
		pa, pb := a[i], b[i]
		if pa.Frame != pb.Frame ||
			math.Float32bits(pa.X) != math.Float32bits(pb.X) ||
			math.Float32bits(pa.Y) != math.Float32bits(pb.Y) ||
			math.Float32bits(pa.VX) != math.Float32bits(pb.VX) ||
			math.Float32bits(pa.VY) != math.Float32bits(pb.VY) ||
			pa.Age != pb.Age ||
			pa.Lifetime != pb.Lifetime ||
			math.Float32bits(pa.Size) != math.Float32bits(pb.Size) ||
			math.Float32bits(pa.Alpha) != math.Float32bits(pb.Alpha) ||
			math.Float32bits(pa.Rotation) != math.Float32bits(pb.Rotation) ||
			pa.Color != pb.Color {
			t.Fatalf("particle %d differs:\na=%+v\nb=%+v", i, pa, pb)
		}
	}
}

func TestFloatCurve(t *testing.T) {
	var empty particles.FloatCurve
	for _, age := range []float32{-1, 0, 0.5, 1, 2} {
		if got := empty.At(age); got != 1 {
			t.Fatalf("empty.At(%v) = %v, want 1", age, got)
		}
	}

	single := particles.FloatCurve{{T: 0.5, Value: 3}}
	for _, age := range []float32{-1, 0, 0.5, 1, 2} {
		if got := single.At(age); got != 3 {
			t.Fatalf("single.At(%v) = %v, want 3", age, got)
		}
	}

	multi := particles.FloatCurve{
		{T: 0, Value: 0},
		{T: 0.5, Value: 2},
		{T: 1, Value: 1},
	}
	cases := []struct {
		t, want float32
	}{
		{-0.5, 0},
		{0, 0},
		{0.25, 1},
		{0.5, 2},
		{0.75, 1.5},
		{1, 1},
		{1.5, 1},
		{float32(math.NaN()), 0},
	}
	for _, tc := range cases {
		if got := multi.At(tc.t); got != tc.want {
			t.Fatalf("multi.At(%v) = %v, want %v", tc.t, got, tc.want)
		}
	}

	subrange := particles.FloatCurve{
		{T: 0.2, Value: 10},
		{T: 0.8, Value: 40},
	}
	if got := subrange.At(0); got != 10 {
		t.Fatalf("subrange.At(0) = %v, want 10", got)
	}
	if got := subrange.At(0.5); got != 25 {
		t.Fatalf("subrange.At(0.5) = %v, want 25", got)
	}
	if got := subrange.At(1); got != 40 {
		t.Fatalf("subrange.At(1) = %v, want 40", got)
	}

	step := particles.FloatCurve{
		{T: 0, Value: 0},
		{T: 0.5, Value: 0},
		{T: 0.5, Value: 1},
		{T: 1, Value: 1},
	}
	if got := step.At(0.25); got != 0 {
		t.Fatalf("step.At(0.25) = %v, want 0", got)
	}
	if got := step.At(0.75); got != 1 {
		t.Fatalf("step.At(0.75) = %v, want 1", got)
	}
}

func TestColorCurve(t *testing.T) {
	var empty particles.ColorCurve
	if got := empty.At(0.5); got != core.RGB(0xffffff) {
		t.Fatalf("empty.At(0.5) = %#08x, want %#08x", uint32(got), uint32(core.RGB(0xffffff)))
	}

	// Channels are multiples of 4 so quarter steps are exact integers.
	c0 := core.Color(0x0064c8ff) // R=0,   G=100, B=200, A=255
	c1 := core.Color(0xc86400ff) // R=200, G=100, B=0,   A=255
	c2 := core.Color(0xc8000000) // R=200, G=0,   B=0,   A=0
	curve := particles.ColorCurve{
		{T: 0, Color: c0},
		{T: 0.5, Color: c1},
		{T: 1, Color: c2},
	}
	cases := []struct {
		t    float32
		want core.Color
	}{
		{-0.2, c0},
		{0, c0},
		{0.25, core.Color(0x646464ff)}, // R=100, G=100, B=100, A=255
		{0.5, c1},
		{0.75, core.Color(0xc8320080)}, // R=200, G=50, B=0, A=128 (255/2 rounded)
		{1, c2},
		{1.2, c2},
	}
	for _, tc := range cases {
		if got := curve.At(tc.t); got != tc.want {
			t.Fatalf("curve.At(%v) = %#08x, want %#08x", tc.t, uint32(got), uint32(tc.want))
		}
	}
}

func TestCurvesEvaluatedOverParticleLife(t *testing.T) {
	c0 := core.Color(0x000000ff) // alpha 1.0
	c1 := core.Color(0xc86400ff) // alpha 1.0
	e := &particles.Emitter{
		Atlas:              sheet,
		Frames:             []sprite.Frame{sheet.Frames["spark"]},
		LifetimeMin:        400 * time.Millisecond,
		LifetimeMax:        400 * time.Millisecond,
		InitialRotationMin: 1,
		InitialRotationMax: 1,
		RotationSpeedMin:   2, // +0.2 rad per 100ms
		RotationSpeedMax:   2,
		SizeCurve: particles.FloatCurve{
			{T: 0, Value: 1},
			{T: 0.5, Value: 3},
			{T: 1, Value: 1},
		},
		AlphaCurve: particles.FloatCurve{
			{T: 0, Value: 1},
			{T: 1, Value: 0},
		},
		RotationCurve: particles.FloatCurve{
			{T: 0, Value: 0},
			{T: 1, Value: 4},
		},
		ColorCurve: particles.ColorCurve{
			{T: 0, Color: c0},
			{T: 1, Color: c1},
		},
	}

	e.Burst(1)
	if e.Count() != 1 {
		t.Fatalf("Count = %d, want 1", e.Count())
	}

	checks := []struct {
		age      time.Duration
		size     float32
		alpha    float32
		rotation float32
		color    core.Color
	}{
		{0, 1, 1, 1 + 0 + 0, c0},
		{100 * time.Millisecond, 2, 0.75, 1 + 0.2 + 1, core.Color(0x321900ff)},
		{200 * time.Millisecond, 3, 0.5, 1 + 0.4 + 2, core.Color(0x643200ff)},
		{300 * time.Millisecond, 2, 0.25, 1 + 0.6 + 3, core.Color(0x964b00ff)},
	}
	for i, want := range checks {
		if i > 0 {
			e.Update(100 * time.Millisecond)
		}
		if e.Count() != 1 {
			t.Fatalf("at %v: Count = %d, want 1", want.age, e.Count())
		}
		p := e.Particles()[0]
		if p.Age != want.age {
			t.Fatalf("at %v: Age = %v", want.age, p.Age)
		}
		if !near(p.Size, want.size) {
			t.Fatalf("at %v: Size = %v, want %v", want.age, p.Size, want.size)
		}
		if !near(p.Alpha, want.alpha) {
			t.Fatalf("at %v: Alpha = %v, want %v", want.age, p.Alpha, want.alpha)
		}
		if !near(p.Rotation, want.rotation) {
			t.Fatalf("at %v: Rotation = %v, want %v", want.age, p.Rotation, want.rotation)
		}
		if p.Color != want.color {
			t.Fatalf("at %v: Color = %#08x, want %#08x", want.age, uint32(p.Color), uint32(want.color))
		}
	}

	// Final step reaches 400ms (t = 1.0) and expires the particle.
	e.Update(100 * time.Millisecond)
	if e.Count() != 0 {
		t.Fatalf("at lifetime: Count = %d, want 0", e.Count())
	}
}

func TestColorCurveAlphaAppliesWithoutAlphaCurve(t *testing.T) {
	e := &particles.Emitter{
		Atlas:       sheet,
		Frames:      []sprite.Frame{sheet.Frames["spark"]},
		LifetimeMin: time.Second,
		LifetimeMax: time.Second,
		ColorCurve: particles.ColorCurve{
			{T: 0, Color: core.Color(0xff0000ff)},
			{T: 1, Color: core.Color(0xff000000)},
		},
	}
	e.Burst(1)
	e.Update(500 * time.Millisecond)
	p := e.Particles()[0]
	wantAlpha := float32(0x80) / 255
	if !near(p.Alpha, wantAlpha) {
		t.Fatalf("Alpha = %v, want %v", p.Alpha, wantAlpha)
	}

	// Combining AlphaCurve (0.5) and ColorCurve alpha (~0.5) multiplies them.
	e.AlphaCurve = particles.FloatCurve{{T: 0, Value: 0.5}}
	e.Reset()
	e.Burst(1)
	e.Update(500 * time.Millisecond)
	p = e.Particles()[0]
	if !near(p.Alpha, 0.5*wantAlpha) {
		t.Fatalf("combined Alpha = %v, want %v", p.Alpha, 0.5*wantAlpha)
	}
}

func TestShapes(t *testing.T) {
	const n = 1000
	const cx, cy float32 = 50, -30

	t.Run("Point", func(t *testing.T) {
		for _, shape := range []particles.Shape{{}, particles.Point()} {
			e := &particles.Emitter{
				X: cx, Y: cy,
				Shape:        shape,
				MaxParticles: n,
				LifetimeMin:  time.Second,
				Seed:         1,
			}
			e.Burst(n)
			if e.Count() != n {
				t.Fatalf("Count = %d, want %d", e.Count(), n)
			}
			for i, p := range e.Particles() {
				if p.X != cx || p.Y != cy {
					t.Fatalf("particle %d at (%v, %v), want (%v, %v)", i, p.X, p.Y, cx, cy)
				}
			}
		}
	})

	t.Run("Circle", func(t *testing.T) {
		const radius float32 = 25
		e := &particles.Emitter{
			X: cx, Y: cy,
			Shape:        particles.Circle(radius),
			MaxParticles: n,
			LifetimeMin:  time.Second,
			Seed:         7,
		}
		e.Burst(n)
		if e.Count() != n {
			t.Fatalf("Count = %d, want %d", e.Count(), n)
		}
		inner := 0
		for i, p := range e.Particles() {
			dx, dy := float64(p.X-cx), float64(p.Y-cy)
			dist := math.Hypot(dx, dy)
			if dist > float64(radius)+1e-4 {
				t.Fatalf("particle %d at distance %v > radius %v", i, dist, radius)
			}
			if dist < float64(radius)/2 {
				inner++
			}
		}
		// Uniform area distribution puts ~25% of particles inside radius/2 (unlike linear r which puts 50%).
		if inner < 180 || inner > 320 {
			t.Fatalf("inner half-radius count = %d of %d, expected ~250 for uniform area distribution", inner, n)
		}
	})

	t.Run("Ring", func(t *testing.T) {
		const radius float32 = 20
		e := &particles.Emitter{
			X: cx, Y: cy,
			Shape:        particles.Ring(radius),
			MaxParticles: n,
			LifetimeMin:  time.Second,
			Seed:         11,
		}
		e.Burst(n)
		if e.Count() != n {
			t.Fatalf("Count = %d, want %d", e.Count(), n)
		}
		for i, p := range e.Particles() {
			dist := math.Hypot(float64(p.X-cx), float64(p.Y-cy))
			if math.Abs(dist-float64(radius)) > 1e-4 {
				t.Fatalf("particle %d at distance %v, want %v", i, dist, radius)
			}
		}
	})

	t.Run("Rect", func(t *testing.T) {
		const w, h float32 = 40, 20
		e := &particles.Emitter{
			X: cx, Y: cy,
			Shape:        particles.Rect(w, h),
			MaxParticles: n,
			LifetimeMin:  time.Second,
			Seed:         13,
		}
		e.Burst(n)
		if e.Count() != n {
			t.Fatalf("Count = %d, want %d", e.Count(), n)
		}
		var q [4]int
		for i, p := range e.Particles() {
			dx, dy := p.X-cx, p.Y-cy
			if dx < -w/2 || dx > w/2 || dy < -h/2 || dy > h/2 {
				t.Fatalf("particle %d offset (%v, %v) outside [%v, %v] x [%v, %v]", i, dx, dy, -w/2, w/2, -h/2, h/2)
			}
			quad := 0
			if dx >= 0 {
				quad |= 1
			}
			if dy >= 0 {
				quad |= 2
			}
			q[quad]++
		}
		for i, count := range q {
			if count < 180 {
				t.Fatalf("rect quadrant %d has only %d particles of %d", i, count, n)
			}
		}
	})
}

func TestBurstAndExpiry(t *testing.T) {
	e := &particles.Emitter{
		Atlas:        sheet,
		Frames:       []sprite.Frame{sheet.Frames["spark"]},
		Rate:         0,
		MaxParticles: 10,
		LifetimeMin:  200 * time.Millisecond,
		LifetimeMax:  200 * time.Millisecond,
		Seed:         5,
	}

	e.Burst(0)
	e.Burst(-3)
	if e.Count() != 0 {
		t.Fatalf("Count after non-positive bursts = %d, want 0", e.Count())
	}

	e.Burst(6)
	if e.Count() != 6 {
		t.Fatalf("Count = %d, want 6", e.Count())
	}

	// Stepping with Rate == 0 must not emit additional particles.
	e.Update(50 * time.Millisecond)
	e.Update(50 * time.Millisecond)
	if e.Count() != 6 {
		t.Fatalf("Count after updates with Rate=0 = %d, want 6", e.Count())
	}

	// A second burst is capped at MaxParticles.
	e.Burst(10)
	if e.Count() != 10 {
		t.Fatalf("Count after capped burst = %d, want 10", e.Count())
	}

	// First 6 expire at 200ms; second 4 are at 100ms.
	e.Update(100 * time.Millisecond)
	if e.Count() != 4 {
		t.Fatalf("Count after first batch expires = %d, want 4", e.Count())
	}

	// Remaining 4 expire 100ms later.
	e.Update(100 * time.Millisecond)
	if e.Count() != 0 {
		t.Fatalf("Count after all expire = %d, want 0", e.Count())
	}

	// Default MaxParticles when 0 is 256.
	def := &particles.Emitter{LifetimeMin: time.Second}
	def.Burst(500)
	if def.Count() != 256 {
		t.Fatalf("default MaxParticles capped at %d, want 256", def.Count())
	}
}

func TestVelocityGravityAndDrawOps(t *testing.T) {
	e := &particles.Emitter{
		Atlas:              sheet,
		Frames:             []sprite.Frame{sheet.Frames["spark"]}, // 8x8, center at 4, 4
		X:                  10,
		Y:                  20,
		SpeedMin:           100,
		SpeedMax:           100,
		AngleMin:           0,
		AngleMax:           0,
		GravityX:           0,
		GravityY:           40,
		LifetimeMin:        time.Second,
		LifetimeMax:        time.Second,
		SizeCurve:          particles.FloatCurve{{T: 0, Value: 1}, {T: 1, Value: 3}},
		AlphaCurve:         particles.FloatCurve{{T: 0, Value: 1}, {T: 1, Value: 0}},
		InitialRotationMin: math.Pi / 2,
		InitialRotationMax: math.Pi / 2,
	}

	e.Burst(1)
	// Step 0.5s: VY = 0 + 40*0.5 = 20; X = 10 + 100*0.5 = 60; Y = 20 + 20*0.5 = 30.
	// At t = 0.5: Size = 2, Alpha = 0.5, Rotation = pi/2.
	e.Update(500 * time.Millisecond)

	p := e.Particles()[0]
	if !near(p.X, 60) || !near(p.Y, 30) || !near(p.VX, 100) || !near(p.VY, 20) {
		t.Fatalf("got pos=(%v,%v) vel=(%v,%v), want pos=(60,30) vel=(100,20)", p.X, p.Y, p.VX, p.VY)
	}

	expected := sprite.NewSprite(sheet.Frames["spark"])
	expected.X, expected.Y = 60, 30
	expected.OriginX, expected.OriginY = 4, 4
	expected.ScaleX, expected.ScaleY = 2, 2
	expected.Alpha = 0.5
	expected.Rotation = math.Pi / 2
	batch := &sprite.Batch{Atlas: sheet, Sprites: []*sprite.Sprite{&expected}}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "emitter", Style: core.Style{Width: core.Pt(200), Height: core.Pt(200)}, Draw: func(d *core.Drawing, _ core.Size) {
				e.Draw(d)
			}},
			core.Canvas{TestID: "batch", Style: core.Style{Width: core.Pt(200), Height: core.Pt(200)}, Draw: func(d *core.Drawing, _ core.Size) {
				batch.Draw(d)
			}},
		}}
	})

	got := app.Find("emitter").Ops
	want := app.Find("batch").Ops
	if len(got) == 0 {
		t.Fatal("emitter drew nothing")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("emitter ops != batch ops:\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestClearVsResetAndEmptyDraw(t *testing.T) {
	e := &particles.Emitter{
		Shape:       particles.Circle(10),
		LifetimeMin: time.Second,
		Seed:        99,
	}
	e.Burst(5)
	first := append([]particles.Particle(nil), e.Particles()...)

	// Clear keeps the PRNG advancing.
	e.Clear()
	if e.Count() != 0 {
		t.Fatalf("Count after Clear = %d, want 0", e.Count())
	}
	e.Burst(5)
	if reflect.DeepEqual(first, e.Particles()) {
		t.Fatal("Clear unexpectedly reset the PRNG")
	}

	// Reset replays from Seed.
	e.Reset()
	e.Burst(5)
	assertParticlesIdentical(t, first, e.Particles())

	// Draw with nil Atlas or zero particles does nothing and does not panic.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{TestID: "empty", Style: core.Style{Width: core.Pt(50), Height: core.Pt(50)}, Draw: func(d *core.Drawing, _ core.Size) {
			e.Draw(d) // Atlas is nil
			e.Atlas = sheet
			e.Clear()
			e.Draw(d) // 0 particles
		}}
	})
	if ops := app.Find("empty").Ops; len(ops) != 0 {
		t.Fatalf("empty/nil-atlas draw emitted %d ops, want 0", len(ops))
	}
}

func TestSteadyStateUpdateZeroAllocs(t *testing.T) {
	e := newConfiguredEmitter(123)
	step := time.Second / 60
	// Warm up past initial capacity allocation.
	for range 120 {
		e.Update(step)
	}
	allocs := testing.AllocsPerRun(200, func() {
		e.Update(step)
	})
	if allocs != 0 {
		t.Fatalf("steady-state Update allocated %v times per step, want 0", allocs)
	}
}

func near(a, b float32) bool {
	return math.Abs(float64(a-b)) < 1e-4
}

func TestColorCurveTintsParticleSprites(t *testing.T) {
	e := &particles.Emitter{
		Atlas:       sheet,
		Frames:      []sprite.Frame{sheet.Frames["spark"]},
		X:           40,
		Y:           40,
		LifetimeMin: time.Second,
		LifetimeMax: time.Second,
		ColorCurve: particles.ColorCurve{
			{T: 0, Color: core.RGBA(0xff0000, 1)},
			{T: 1, Color: core.RGBA(0x0000ff, 0)},
		},
	}
	e.Burst(1)
	e.Update(500 * time.Millisecond)

	p := e.Particles()[0]
	// Halfway: R and B are each 255/2 rounded, and the curve's alpha 0.5 goes to Alpha, not the tint.
	if p.Color != core.Color(0x800080_80) || !near(p.Alpha, float32(0x80)/255) {
		t.Fatalf("Color = %#08x, Alpha = %v, want 0x80008080, %v", uint32(p.Color), p.Alpha, float32(0x80)/255)
	}

	expected := sprite.NewSprite(sheet.Frames["spark"])
	expected.X, expected.Y = p.X, p.Y
	expected.OriginX, expected.OriginY = 4, 4
	expected.Alpha = p.Alpha
	expected.Tint = core.RGB(0x800080)
	batch := &sprite.Batch{Atlas: sheet, Sprites: []*sprite.Sprite{&expected}}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{TestID: "emitter", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) { e.Draw(d) }},
			core.Canvas{TestID: "batch", Style: core.Style{Width: core.Pt(100), Height: core.Pt(100)}, Draw: func(d *core.Drawing, _ core.Size) { batch.Draw(d) }},
		}}
	})
	got, want := app.Find("emitter").Ops, app.Find("batch").Ops
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("emitter ops != batch ops:\ngot:  %+v\nwant: %+v", got, want)
	}
	var tinted bool
	for _, op := range got {
		tinted = tinted || op.Kind == core.DrawImage && op.Tinted && op.Color == core.RGB(0x800080)
	}
	if !tinted {
		t.Fatalf("the particle drew untinted: %+v", got)
	}
}

// BenchmarkEmitter5000UpdateDraw measures one fixed step and one frame's draw of 5000 live rotating, fading particles through the emitter's sprite.Batch. Its ColorCurve tints every particle, so it measures the path where each particle keeps its own image draw rather than joining a DrawAtlas run.
func BenchmarkEmitter5000UpdateDraw(b *testing.B) {
	const count = 5000
	e := newConfiguredEmitter(7)
	e.Rate = 0
	e.MaxParticles = count
	e.LifetimeMin, e.LifetimeMax = time.Hour, time.Hour
	e.Burst(count)
	step := time.Second / 60
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 8*count)}
	e.Update(step)
	e.Draw(&d)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		e.Update(step)
		// Particles live an hour of simulated time, so a long -benchtime would otherwise see all 5000 expire on the same step.
		if n := e.Count(); n < count {
			e.Burst(count - n)
		}
		d.Ops = d.Ops[:0]
		e.Draw(&d)
	}
	if e.Count() != count {
		b.Fatalf("Count() = %d, want %d", e.Count(), count)
	}
}
