package physics_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics"
)

const step = time.Second / 60

// rampWorld builds a static triangular ramp falling to the right at slope 1/2 and a box resting on it.
func rampWorld(t testing.TB, friction float32) (*physics.RigidWorld, *physics.RigidBody) {
	t.Helper()
	ramp := physics.NewStaticRigidBody(physics.PolygonShape(mustPolygon(t, 0, 0, 50, 0, 200, 300, 200)))
	slope := math.Atan2(150, 300)
	nx, ny := math.Sin(slope), -math.Cos(slope)
	cx, cy := 60+nx*10.05, 80+ny*10.05
	box := physics.NewRigidBox(float32(cx), float32(cy), 20, 20, 1)
	box.Angle = float32(slope)
	box.Friction = 0.1
	ramp.Friction = friction
	w := &physics.RigidWorld{GravityY: 600}
	w.AddBody(ramp, box)
	return w, box
}

func TestRigidBoxSlidesDownPolygonRamp(t *testing.T) {
	w, box := rampWorld(t, 0.1)
	slope := float32(math.Atan2(150, 300))
	prevX := box.X
	var prevSpeed float64
	for i := range 90 {
		w.Step(step)
		if box.X < prevX-1e-3 {
			t.Fatalf("step %d: box moved back up the ramp from x=%v to %v", i, prevX, box.X)
		}
		prevX = box.X
		speed := math.Hypot(float64(box.VX), float64(box.VY))
		if i > 5 && speed < prevSpeed-1 {
			t.Fatalf("step %d: box caught on the ramp, speed %v after %v", i, speed, prevSpeed)
		}
		prevSpeed = speed
		if d := box.Angle - slope; d > 0.05 || d < -0.05 {
			t.Fatalf("step %d: box tipped to angle %v (slope %v)", i, box.Angle, slope)
		}
		// The box's center stays half its height above the surface y = 50 + x/2.
		dist := (float64(box.Y) - (50 + float64(box.X)/2)) * -math.Cos(float64(slope))
		if dist < 9 || dist > 11 {
			t.Fatalf("step %d: box center %v from the ramp, want ~10", i, dist)
		}
	}
	if box.X < 150 {
		t.Fatalf("box only reached x=%v", box.X)
	}
}

func TestRigidBoxHoldsOnRoughRamp(t *testing.T) {
	w, box := rampWorld(t, 10)
	box.Friction = 10
	startX := box.X
	for range 60 {
		w.Step(step)
	}
	if d := box.X - startX; d > 2 || d < -2 {
		t.Fatalf("box on a rough ramp slid %v", d)
	}
}

func TestRigidRampDeterministic(t *testing.T) {
	run := func() [3]float32 {
		w, box := rampWorld(t, 0.1)
		for range 120 {
			w.Step(step)
		}
		return [3]float32{box.X, box.Y, box.Angle}
	}
	first := run()
	for range 3 {
		if again := run(); again != first {
			t.Fatalf("runs differ: %v vs %v", again, first)
		}
	}
}

// chainFloor returns a static chain along y = 100 split into segments of length seg.
func chainFloor(seg float32) *physics.RigidBody {
	var xy []float32
	for x := float32(-100); x <= 2000; x += seg {
		xy = append(xy, x, 100)
	}
	return physics.NewStaticRigidBody(physics.ChainShape(pts(xy...), false))
}

func TestRigidCircleRollsAlongChainWithoutCatching(t *testing.T) {
	roll := func(floor *physics.RigidBody) []float32 {
		ball := physics.NewRigidCircle(20, 92, 8, 1)
		ball.VX = 200
		w := &physics.RigidWorld{GravityY: 600}
		w.AddBody(floor, ball)
		var xs []float32
		for i := range 180 {
			w.Step(step)
			if i > 10 && (ball.Y < 91.8 || ball.Y > 92.2 || ball.VY < -5) {
				t.Fatalf("step %d: ball hopped at x=%v y=%v vy=%v", i, ball.X, ball.Y, ball.VY)
			}
			xs = append(xs, ball.X)
		}
		if ball.AngularVel <= 0 {
			t.Fatalf("ball is not rolling: angular velocity %v", ball.AngularVel)
		}
		return xs
	}
	seamless := roll(chainFloor(2100))
	seamed := roll(chainFloor(10))
	for i := range seamless {
		if d := seamed[i] - seamless[i]; d > 0.5 || d < -0.5 {
			t.Fatalf("step %d: ball on seams at x=%v, on one segment at %v", i, seamed[i], seamless[i])
		}
	}
	if last := seamed[len(seamed)-1]; last < 300 {
		t.Fatalf("ball only rolled to x=%v", last)
	}
}

func TestRigidBoxSlidesAlongChainWithoutCatching(t *testing.T) {
	floor := chainFloor(10)
	floor.Friction = 0
	crate := physics.NewRigidBox(20, 90, 20, 20, 1)
	crate.Friction = 0
	crate.VX = 150
	w := &physics.RigidWorld{GravityY: 600}
	w.AddBody(floor, crate)
	for i := range 240 {
		w.Step(step)
		if crate.VX < 149 || crate.Angle > 0.01 || crate.Angle < -0.01 {
			t.Fatalf("step %d: crate caught on a seam at x=%v: vx=%v angle=%v", i, crate.X, crate.VX, crate.Angle)
		}
	}
}

func TestRigidCapsuleRestsOnGround(t *testing.T) {
	ground := physics.NewStaticRigidBox(200, 210, 400, 20)
	pill := physics.NewRigidBody(physics.CapsuleShape(physics.Capsule{X1: 80, Y1: 100, X2: 140, Y2: 100, R: 10}), 2)
	w := &physics.RigidWorld{GravityY: 600}
	w.AddBody(ground, pill)
	for range 180 {
		w.Step(step)
	}
	if !near(pill.Y, 190, 0.2) || math.Abs(float64(pill.Angle)) > 0.01 || math.Abs(float64(pill.VY)) > 1 {
		t.Fatalf("capsule at y=%v angle=%v vy=%v, want resting at 190 level", pill.Y, pill.Angle, pill.VY)
	}
	// Shape follows the body.
	c := pill.Shape.Capsule
	if !near((c.Y1+c.Y2)/2, pill.Y, 1e-3) || !near(c.X2-c.X1, 60, 1e-2) {
		t.Fatalf("capsule shape %+v not synced to body at (%v, %v)", c, pill.X, pill.Y)
	}
}

func TestRigidPolygonStackIsStable(t *testing.T) {
	ground := physics.NewStaticRigidBody(physics.PolygonShape(physics.NewBoxPolygon(200, 210, 400, 20, 0)))
	w := &physics.RigidWorld{GravityY: 600}
	w.AddBody(ground)
	var crates []*physics.RigidBody
	for i := range 4 {
		hexagon, ok := physics.NewPolygon(regularPolygonPoints(6, 12), 0.5)
		if !ok {
			t.Fatal("hexagon")
		}
		b := physics.NewRigidBody(physics.PolygonShape(hexagon.Translated(200, float32(188-22*i))), 1)
		crates = append(crates, b)
		w.AddBody(b)
	}
	for range 240 {
		w.Step(step)
	}
	ys := make([]float32, len(crates))
	for i, b := range crates {
		ys[i] = b.Y
	}
	for range 60 {
		w.Step(step)
	}
	for i, b := range crates {
		if math.Abs(float64(b.Y-ys[i])) > 0.2 || math.Abs(float64(b.X-200)) > 1 {
			t.Fatalf("crate %d drifted from y=%v to (%v, %v)", i, ys[i], b.X, b.Y)
		}
	}
}

func TestNewRigidBodyMassFromShape(t *testing.T) {
	tri := physics.PolygonShape(mustPolygon(t, 0, 0, 0, 6, 0, 0, 3))
	b := physics.NewRigidBody(tri, 2)
	if b.Static || !near(b.X, 2, 1e-5) || !near(b.Y, 1, 1e-5) || !near(b.Inertia, tri.MomentOfInertia(2), 1e-6) || !near(b.InvMass, 0.5, 1e-6) {
		t.Fatalf("triangle body = %+v", b)
	}
	box := physics.NewRigidBody(physics.BoxShape(physics.Box{X: 0, Y: 0, W: 4, H: 2}), 3)
	if want := physics.NewRigidBox(2, 1, 4, 2, 3); box.X != want.X || box.Y != want.Y || box.Inertia != want.Inertia || box.Shape != want.Shape {
		t.Fatalf("box body = %+v, want %+v", box, want)
	}
	for _, s := range []physics.Shape{
		physics.SegmentShape(physics.Segment{X2: 10}),
		physics.ChainShape(pts(0, 0, 10, 0), false),
	} {
		if b := physics.NewRigidBody(s, 5); !b.Static || b.InvMass != 0 {
			t.Fatalf("%v body not static: %+v", s.Kind, b)
		}
		// Even one marked dynamic by hand stays put.
		manual := &physics.RigidBody{Shape: s, Mass: 1}
		w := &physics.RigidWorld{GravityY: 100}
		w.AddBody(manual)
		w.Step(step)
		if !manual.Static || manual.Y != 0 {
			t.Fatalf("%v body moved: %+v", s.Kind, manual)
		}
	}
	if b := physics.NewRigidBody(tri, 0); !b.Static {
		t.Fatal("zero mass body is dynamic")
	}
	if b := physics.NewRigidBody(physics.Shape{}, 1); !b.Static {
		t.Fatal("empty shape body is dynamic")
	}
}

func TestRigidPolygonShapeFollowsRotation(t *testing.T) {
	p := physics.PolygonShape(physics.NewBoxPolygon(50, 50, 20, 10, 0))
	b := physics.NewRigidBody(p, 1)
	b.AngularVel = math.Pi
	w := &physics.RigidWorld{}
	w.AddBody(b)
	for range 30 {
		w.Step(step)
	}
	// Half a second at pi rad/s is a quarter turn: the 20 wide box is now 20 tall.
	bb := b.Shape.Bounds()
	if !near(b.Angle, math.Pi/2, 1e-3) || !near(bb.W, 10, 1e-2) || !near(bb.H, 20, 1e-2) {
		t.Fatalf("angle %v bounds %+v", b.Angle, bb)
	}
	if cx, cy := b.Shape.Centroid(); !near(cx, b.X, 1e-3) || !near(cy, b.Y, 1e-3) {
		t.Fatalf("shape centroid (%v, %v) away from body (%v, %v)", cx, cy, b.X, b.Y)
	}

	// Assigning a new Shape re-anchors it at the body's pose.
	b.AngularVel = 0
	b.Shape = physics.CapsuleShape(physics.Capsule{X1: b.X - 5, Y1: b.Y, X2: b.X + 5, Y2: b.Y, R: 2})
	b.VX = 60
	w.Step(step)
	c := b.Shape.Capsule
	if !near(c.X1, b.X-5, 1e-3) || !near(c.X2, b.X+5, 1e-3) || !near(c.Y1, b.Y, 1e-3) {
		t.Fatalf("reassigned capsule %+v not at body (%v, %v)", c, b.X, b.Y)
	}
}

func TestRigidChainLoopContainsBall(t *testing.T) {
	room := physics.NewStaticRigidBody(physics.ChainShape(pts(0, 0, 200, 0, 200, 200, 0, 200), true))
	ball := physics.NewRigidCircle(100, 100, 10, 1)
	// Under one radius per step: without continuous collision a body whose center crosses a zero-thickness line in one step passes through it.
	ball.VX, ball.VY = 400, -300
	ball.Restitution = 0.8
	w := &physics.RigidWorld{GravityY: 600}
	w.AddBody(room, ball)
	for i := range 600 {
		w.Step(step)
		if ball.X < 0 || ball.X > 200 || ball.Y < 0 || ball.Y > 200 {
			t.Fatalf("step %d: ball escaped the room to (%v, %v)", i, ball.X, ball.Y)
		}
	}
}

func TestRigidQueriesSeeRotatedPolygons(t *testing.T) {
	// A World built from a RigidWorld's shapes sees each polygon at its pose.
	b := physics.NewRigidBody(physics.PolygonShape(physics.NewBoxPolygon(0, 0, 40, 4, 0)), 1)
	b.Angle = math.Pi / 2
	w := &physics.RigidWorld{}
	w.AddBody(b)
	b.VX = 1e-3
	w.Step(step)
	q := &physics.World{Bodies: []*physics.Body{{Shape: b.Shape}}}
	if len(q.QueryPoint(0, 15)) != 1 || len(q.QueryPoint(15, 0)) != 0 {
		t.Fatalf("rotated polygon not queried at its pose: %+v", b.Shape.Polygon)
	}
}

func BenchmarkRigidWorldPolygonPile(b *testing.B) {
	ground := physics.NewStaticRigidBody(physics.ChainShape(pts(-200, 300, 0, 280, 200, 300, 400, 280, 600, 300), false))
	b.ReportAllocs()
	for b.Loop() {
		w := &physics.RigidWorld{GravityY: 600}
		w.AddBody(ground)
		for i := range 20 {
			p := physics.NewBoxPolygon(float32(20*(i%10)), float32(200-30*(i/10)), 16, 12, float32(i)*0.3)
			p.Radius = 1
			w.AddBody(physics.NewRigidBody(physics.PolygonShape(p), 1))
		}
		for range 60 {
			w.Step(step)
		}
	}
}
