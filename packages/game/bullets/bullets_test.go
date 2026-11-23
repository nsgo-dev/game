package bullets_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/bullets"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

const step = time.Second / 60

func testAtlas() *sprite.Atlas {
	return &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:bullets.png"},
		Frames: map[string]sprite.Frame{
			"orb":  {Name: "orb", X: 0, Y: 0, W: 8, H: 8, Width: 8, Height: 8},
			"dart": {Name: "dart", X: 8, Y: 0, W: 12, H: 4, Width: 12, Height: 4},
		},
	}
}

func mustEmitter(t testing.TB, f *bullets.Field, p bullets.Pattern, seed uint64) *bullets.Emitter {
	t.Helper()
	e, err := bullets.NewEmitter(f, &p, seed)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	return e
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func degrees(rad float32) float32 { return rad * 180 / math.Pi }

func TestRingSpacing(t *testing.T) {
	f := bullets.NewField(testAtlas(), 16)
	e := mustEmitter(t, f, bullets.Pattern{Name: "ring", Kind: bullets.Ring, Frame: "orb", Count: 4, Speed: 60, Angle: 10, Shots: 1}, 1)
	e.X, e.Y = 100, 50
	if n := e.Update(step); n != 4 {
		t.Fatalf("spawned %d, want 4", n)
	}
	wantHeadings := []float32{10, 100, -170, -80}
	for i, b := range f.Bullets() {
		if !near(degrees(b.Heading), wantHeadings[i]) {
			t.Errorf("bullet %d heading %v°, want %v°", i, degrees(b.Heading), wantHeadings[i])
		}
		// Fired at the start of the step, each bullet has moved one step's worth: 1 point at 60 points a second.
		if d := b.Pos().Sub(math2d.Vec2{X: 100, Y: 50}).Len(); !near(d, 1) {
			t.Errorf("bullet %d moved %v, want 1", i, d)
		}
		if b.Age != step || b.Pattern != "ring" || b.Shot != 0 {
			t.Errorf("bullet %d = age %v pattern %q shot %d", i, b.Age, b.Pattern, b.Shot)
		}
		if b.Sprite.X != b.X || b.Sprite.Y != b.Y || b.Sprite.OriginX != 4 || b.Sprite.OriginY != 4 {
			t.Errorf("bullet %d sprite at (%v, %v) origin (%v, %v)", i, b.Sprite.X, b.Sprite.Y, b.Sprite.OriginX, b.Sprite.OriginY)
		}
	}
	if !e.Done() || e.Update(time.Second) != 0 {
		t.Fatal("a one-shot pattern fired again")
	}
}

func TestSpiralTiming(t *testing.T) {
	f := bullets.NewField(nil, 0)
	p := bullets.Pattern{Name: "spiral", Kind: bullets.Spiral, Count: 2, Speed: 100, Spin: 30, Shots: 3, Delay: 100 * time.Millisecond, Interval: 50 * time.Millisecond}
	e := mustEmitter(t, f, p, 1)
	if n := e.Update(50 * time.Millisecond); n != 0 {
		t.Fatalf("fired %d bullets before the delay", n)
	}
	// The window [50ms, 210ms) holds the shots due at 100ms, 150ms and 200ms.
	if n := e.Update(160 * time.Millisecond); n != 6 {
		t.Fatalf("spawned %d, want 6", n)
	}
	if !e.Done() || e.Shots() != 3 || e.Elapsed() != 210*time.Millisecond {
		t.Fatalf("done %v shots %d elapsed %v", e.Done(), e.Shots(), e.Elapsed())
	}
	want := []struct {
		heading float32
		age     time.Duration
	}{
		{0, 110 * time.Millisecond}, {180, 110 * time.Millisecond},
		{30, 60 * time.Millisecond}, {-150, 60 * time.Millisecond},
		{60, 10 * time.Millisecond}, {-120, 10 * time.Millisecond},
	}
	for i, b := range f.Bullets() {
		if !near(degrees(b.Heading), want[i].heading) || b.Age != want[i].age || b.Shot != i/2 {
			t.Errorf("bullet %d: heading %v° age %v shot %d, want %v° %v", i, degrees(b.Heading), b.Age, b.Shot, want[i].heading, want[i].age)
		}
		if d := b.Pos().Len(); !near(d, float32(b.Age.Seconds())*100) {
			t.Errorf("bullet %d is %v from the emitter, want %v", i, d, b.Age.Seconds()*100)
		}
		if b.Sprite.Frame.W != 0 {
			t.Errorf("bullet %d of a frameless pattern has a frame", i)
		}
	}
}

func TestAimedBurst(t *testing.T) {
	f := bullets.NewField(testAtlas(), 8)
	p := bullets.Pattern{Name: "aimed", Kind: bullets.Aimed, Frame: "dart", Count: 3, Speed: 50, Spread: 30, Shots: 1, FaceHeading: true}
	e := mustEmitter(t, f, p, 1)
	e.X, e.Y = 10, 10
	e.TargetX, e.TargetY = 10, 200
	e.Update(step)
	for i, want := range []float32{75, 90, 105} {
		b := f.Bullets()[i]
		if !near(degrees(b.Heading), want) {
			t.Errorf("bullet %d heading %v°, want %v°", i, degrees(b.Heading), want)
		}
		if b.Sprite.Rotation != b.Heading {
			t.Errorf("bullet %d sprite rotation %v, want its heading %v", i, b.Sprite.Rotation, b.Heading)
		}
		if b.Sprite.Z != 0 || b.Sprite.OriginX != 6 || b.Sprite.OriginY != 2 {
			t.Errorf("bullet %d sprite z %d origin (%v, %v)", i, b.Sprite.Z, b.Sprite.OriginX, b.Sprite.OriginY)
		}
	}

	single := mustEmitter(t, f, bullets.Pattern{Name: "one", Kind: bullets.Aimed, Count: 1, Speed: 1, Angle: 5, Shots: 1}, 1)
	single.X, single.Y, single.TargetX, single.TargetY = 0, 0, -3, 0
	f.Clear()
	single.Update(step)
	if h := degrees(f.Bullets()[0].Heading); !near(h, -175) {
		t.Fatalf("single aimed bullet heading %v°, want -175°", h)
	}

	full := mustEmitter(t, f, bullets.Pattern{Name: "full", Kind: bullets.Aimed, Count: 4, Speed: 1, Spread: 360, Shots: 1}, 1)
	full.X, full.Y, full.TargetX, full.TargetY = 0, 0, 1, 0
	f.Clear()
	full.Update(step)
	for i, want := range []float32{180, -90, 0, 90} {
		if h := degrees(f.Bullets()[i].Heading); !near(h, want) && !near(h, want-360) {
			t.Errorf("full-circle aimed bullet %d heading %v°, want %v°", i, h, want)
		}
	}
}

func TestMotion(t *testing.T) {
	f := bullets.NewField(nil, 4)
	e := mustEmitter(t, f, bullets.Pattern{Name: "m", Kind: bullets.Ring, Count: 1, Speed: 10, Shots: 1, Accel: -20, Turn: 90}, 1)
	e.Update(time.Millisecond)
	f.Clear()
	e.Reset()
	if e.Done() || e.Shots() != 0 {
		t.Fatal("Reset did not start the emitter over")
	}
	e.Update(250 * time.Millisecond)
	b := f.Bullets()[0]
	if !near(b.Speed, 5) || !near(degrees(b.Heading), 22.5) {
		t.Fatalf("after 250ms speed %v heading %v°, want 5 and 22.5°", b.Speed, degrees(b.Heading))
	}
	for range 3 {
		f.Update(250 * time.Millisecond)
	}
	if b.Speed != 0 {
		t.Fatalf("speed %v after slowing past 0, want 0", b.Speed)
	}
	if v := b.Velocity(); v.X != 0 || v.Y != 0 {
		t.Fatalf("velocity %v of a stopped bullet", v)
	}
}

func TestFieldRemovesExpiredAndOutside(t *testing.T) {
	f := bullets.NewField(nil, 8)
	f.Bounds = math2d.Rect{X: 0, Y: 0, W: 100, H: 100}
	short := mustEmitter(t, f, bullets.Pattern{Name: "short", Kind: bullets.Ring, Count: 1, Speed: 0, Shots: 1, Lifetime: 6 * step}, 1)
	short.X, short.Y = 50, 50
	fast := mustEmitter(t, f, bullets.Pattern{Name: "fast", Kind: bullets.Ring, Count: 1, Speed: 300, Shots: 1, Radius: 5}, 1)
	fast.X, fast.Y = 90, 50
	still := mustEmitter(t, f, bullets.Pattern{Name: "still", Kind: bullets.Ring, Count: 1, Speed: 0, Shots: 1}, 1)
	still.X, still.Y = 20, 20
	for _, e := range []*bullets.Emitter{short, fast, still} {
		e.Update(step)
	}
	if f.Len() != 3 {
		t.Fatalf("len %d, want 3", f.Len())
	}
	// fast moves 5 points a step from x=95: its circle (radius 5) touches the bounds until x passes 105.
	for i := range 2 {
		f.Update(step)
		if f.Len() != 3 {
			t.Fatalf("len %d after %d updates, want 3", f.Len(), i+1)
		}
	}
	f.Update(step)
	if names := patternNames(f); !reflect.DeepEqual(names, []string{"short", "still"}) {
		t.Fatalf("after 3 updates = %v, want [short still]", names)
	}
	f.Update(step)
	if f.Len() != 2 {
		t.Fatalf("len %d before short's lifetime, want 2", f.Len())
	}
	f.Update(step)
	if names := patternNames(f); !reflect.DeepEqual(names, []string{"still"}) {
		t.Fatalf("after short's lifetime = %v, want [still]", names)
	}
}

func patternNames(f *bullets.Field) []string {
	var names []string
	for _, b := range f.Bullets() {
		names = append(names, b.Pattern)
	}
	return names
}

func TestRemoveFuncAndLimit(t *testing.T) {
	f := bullets.NewField(nil, 4)
	f.Limit = 6
	e := mustEmitter(t, f, bullets.Pattern{Name: "r", Kind: bullets.Ring, Count: 4, Speed: 30, Shots: 0, Interval: step, Radius: 2}, 1)
	if n := e.Update(step); n != 4 {
		t.Fatalf("spawned %d, want 4", n)
	}
	if n := e.Update(step); n != 2 || f.Len() != 6 {
		t.Fatalf("spawned %d (len %d) past the limit, want 2 (6)", n, f.Len())
	}
	player := math2d.Circle{Center: math2d.Vec2{X: 1, Y: 0}, Radius: 1}
	removed := f.RemoveFunc(func(b *bullets.Bullet) bool { return b.Circle().Overlaps(player) })
	if removed == 0 || f.Len() != 6-removed {
		t.Fatalf("removed %d, len %d", removed, f.Len())
	}
	for _, b := range f.Bullets() {
		if b.Circle().Overlaps(player) {
			t.Fatal("a hit bullet stayed")
		}
	}
	e.Stopped = true
	if !e.Done() || e.Update(time.Second) != 0 {
		t.Fatal("a stopped emitter fired")
	}
	f.Clear()
	if f.Len() != 0 {
		t.Fatalf("len %d after Clear", f.Len())
	}
}

func TestNewEmitterErrors(t *testing.T) {
	ok := bullets.Pattern{Name: "p", Kind: bullets.Ring, Frame: "orb", Count: 1, Shots: 1}
	withAtlas := bullets.NewField(testAtlas(), 0)
	if _, err := bullets.NewEmitter(withAtlas, &ok, 1); err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	missing := ok
	missing.Frame = "nope"
	bad := ok
	bad.Count = 0
	for name, c := range map[string]struct {
		f *bullets.Field
		p *bullets.Pattern
	}{
		"nil field":     {nil, &ok},
		"nil pattern":   {withAtlas, nil},
		"invalid":       {withAtlas, &bad},
		"missing frame": {withAtlas, &missing},
		"no atlas":      {bullets.NewField(nil, 0), &ok},
	} {
		if _, err := bullets.NewEmitter(c.f, c.p, 1); err == nil {
			t.Errorf("%s: NewEmitter succeeded", name)
		}
	}
}

// snapshot is what a run of a pattern leaves in its field, for comparing runs.
type snapshot struct {
	X, Y, Speed, Heading []float32
	Shots                []int
}

func runPattern(t testing.TB, p bullets.Pattern, seed uint64, steps int) snapshot {
	f := bullets.NewField(testAtlas(), 256)
	f.Bounds = math2d.Rect{X: -400, Y: -400, W: 800, H: 800}
	e := mustEmitter(t, f, p, seed)
	for i := range steps {
		e.TargetX, e.TargetY = float32(i), 300
		f.Update(step)
		e.Update(step)
	}
	var s snapshot
	for _, b := range f.Bullets() {
		s.X = append(s.X, b.X)
		s.Y = append(s.Y, b.Y)
		s.Speed = append(s.Speed, b.Speed)
		s.Heading = append(s.Heading, b.Heading)
		s.Shots = append(s.Shots, b.Shot)
	}
	return s
}

func TestSameSeedSameBullets(t *testing.T) {
	file, err := bullets.Parse(readFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range file.Patterns {
		t.Run(p.Name, func(t *testing.T) {
			random.AssertDeterministic(t, 3, func() snapshot { return runPattern(t, p, 42, 120) })
			if len(runPattern(t, p, 42, 120).X) == 0 {
				t.Fatal("the pattern spawned nothing")
			}
		})
	}
	vortex, _ := file.Pattern("vortex")
	if reflect.DeepEqual(runPattern(t, *vortex, 42, 120), runPattern(t, *vortex, 43, 120)) {
		t.Fatal("a jittered pattern spawned the same bullets for different seeds")
	}

	// Reset replays the same bullets as a fresh emitter.
	f := bullets.NewField(testAtlas(), 0)
	e := mustEmitter(t, f, *vortex, 7)
	first := headings(f, e)
	e.Reset()
	f.Clear()
	if second := headings(f, e); !reflect.DeepEqual(first, second) {
		t.Fatal("Reset did not replay the same bullets")
	}
}

func headings(f *bullets.Field, e *bullets.Emitter) []float32 {
	for range 30 {
		f.Update(step)
		e.Update(step)
	}
	var out []float32
	for _, b := range f.Bullets() {
		out = append(out, b.Heading)
	}
	return out
}

func TestDraw(t *testing.T) {
	atlas := testAtlas()
	f := bullets.NewField(atlas, 4)
	var d core.Drawing
	f.Draw(&d)
	if len(d.Ops) != 0 {
		t.Fatal("an empty field drew")
	}
	low := mustEmitter(t, f, bullets.Pattern{Name: "low", Kind: bullets.Ring, Frame: "orb", Count: 1, Speed: 0, Shots: 1, Z: 1}, 1)
	low.X, low.Y = 20, 30
	high := mustEmitter(t, f, bullets.Pattern{Name: "high", Kind: bullets.Ring, Frame: "orb", Count: 1, Speed: 0, Shots: 1, Z: 0}, 1)
	high.X, high.Y = 40, 50
	low.Update(step)
	high.Update(step)
	f.Draw(&d)

	var want core.Drawing
	want.DrawImageFrom(atlas.Image, 0, 0, 8, 8, 36, 46, 8, 8)
	want.DrawImageFrom(atlas.Image, 0, 0, 8, 8, 16, 26, 8, 8)
	if !reflect.DeepEqual(d.Ops, want.Ops) {
		t.Fatalf("ops = %#v\nwant %#v", d.Ops, want.Ops)
	}

	noAtlas := bullets.NewField(nil, 1)
	mustEmitter(t, noAtlas, bullets.Pattern{Name: "x", Kind: bullets.Ring, Count: 1, Shots: 1}, 1).Update(step)
	d.Ops = d.Ops[:0]
	noAtlas.Draw(&d)
	if len(d.Ops) != 0 {
		t.Fatal("a field without an atlas drew")
	}
}

func TestSteadyStateDoesNotAllocate(t *testing.T) {
	f, emitters, d := benchScene(t, 5000)
	simulate := testing.AllocsPerRun(20, func() {
		f.Update(step)
		for _, e := range emitters {
			e.Update(step)
		}
	})
	if simulate != 0 {
		t.Fatalf("moving and firing %d bullets allocated %v times a step", f.Len(), simulate)
	}
	// NSGo v0.2.0's Drawing.DrawImageFrom stores a pointer to its ImageSource argument in the op, which moves each copy to the heap: one allocation per sprite drawn, outside this package. Drawing must add none of its own.
	draw := testing.AllocsPerRun(20, func() {
		d.Ops = d.Ops[:0]
		f.Draw(d)
	})
	if draw > float64(len(d.Ops)) {
		t.Fatalf("drawing %d bullets allocated %v times for %d ops", f.Len(), draw, len(d.Ops))
	}
}
