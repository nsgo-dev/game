package physics_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
)

func TestRigidFastCircleStopsAtThinWall(t *testing.T) {
	// Each wall is a thin line through (400, 100) turned by angle; the ball must stay on its near side.
	walls := []struct {
		name  string
		angle float64
		body  func() *physics.RigidBody
	}{
		{"box", 0, func() *physics.RigidBody { return physics.NewStaticRigidBox(400, 100, 4, 200) }},
		{"rotated polygon", 0.2, func() *physics.RigidBody {
			return physics.NewStaticRigidBody(physics.PolygonShape(physics.NewBoxPolygon(400, 100, 4, 200, 0.2)))
		}},
		{"segment", 0, func() *physics.RigidBody {
			return physics.NewStaticRigidBody(physics.SegmentShape(physics.Segment{X1: 400, Y1: 0, X2: 400, Y2: 200}))
		}},
		{"chain", 0, func() *physics.RigidBody {
			return physics.NewStaticRigidBody(physics.ChainShape([]core.Point{{X: 400, Y: 0}, {X: 400, Y: 100}, {X: 400, Y: 200}}, false))
		}},
	}
	for _, wall := range walls {
		for _, speed := range []float32{2000, 5000, 20000} {
			t.Run(wall.name, func(t *testing.T) {
				w := &physics.RigidWorld{}
				ball := physics.NewRigidCircle(100, 100, 6, 1)
				ball.VX = speed
				w.AddBody(wall.body(), ball)
				sin, cos := math.Sincos(wall.angle)
				side := func() float64 { return (float64(ball.X)-400)*cos + (float64(ball.Y)-100)*sin }
				for i := range 60 {
					w.Step(step)
					if d := side(); d > -6 {
						t.Fatalf("speed %v: ball %v px past the wall's near face at step %d", speed, d+6, i)
					}
				}
			})
		}
	}
}

func TestRigidFastCircleBouncesOffThinWall(t *testing.T) {
	w := &physics.RigidWorld{}
	wall := physics.NewStaticRigidBox(400, 100, 4, 200)
	ball := physics.NewRigidCircle(100, 100, 6, 1)
	ball.VX = 5000
	ball.Restitution = 1
	w.AddBody(wall, ball)
	for range 4 {
		w.Step(step)
	}
	if ball.X > 400-2-6 || ball.X < 300 {
		t.Fatalf("ball at x=%v, want rebounding from the wall face at 392", ball.X)
	}
	if !near(ball.VX, -5000, 1) {
		t.Fatalf("elastic ball rebounded with VX=%v, want -5000", ball.VX)
	}
}

func TestRigidFastBodiesOfEveryShapeStopAtThinFloor(t *testing.T) {
	shapes := []struct {
		name string
		body func() *physics.RigidBody
	}{
		{"box", func() *physics.RigidBody { return physics.NewRigidBox(100, 0, 10, 10, 1) }},
		{"circle", func() *physics.RigidBody { return physics.NewRigidCircle(100, 0, 5, 1) }},
		{"polygon", func() *physics.RigidBody {
			return physics.NewRigidBody(physics.PolygonShape(physics.NewBoxPolygon(100, 0, 12, 6, 0.7)), 1)
		}},
		{"capsule", func() *physics.RigidBody {
			return physics.NewRigidBody(physics.CapsuleShape(physics.Capsule{X1: 94, Y1: 0, X2: 106, Y2: 0, R: 3}), 1)
		}},
	}
	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			w := &physics.RigidWorld{GravityY: 600}
			floor := physics.NewStaticRigidBox(100, 300, 400, 2)
			b := sh.body()
			b.VY = 6000
			b.AngularVel = 20
			w.AddBody(floor, b)
			for i := range 120 {
				w.Step(step)
				if b.Y > 300 {
					t.Fatalf("body fell through the floor at step %d: y=%v", i, b.Y)
				}
			}
			if b.Y < 280 {
				t.Fatalf("body rests at y=%v, want on the floor at y=299", b.Y)
			}
		})
	}
}

func TestRigidBulletIsSweptAtAnySpeed(t *testing.T) {
	// A box moving slower than its half thickness per step is not swept, so it ends some steps inside the wall; a Bullet is swept on every step and stops at the surface.
	for _, bullet := range []bool{false, true} {
		w := &physics.RigidWorld{}
		wall := physics.NewStaticRigidBox(200, 100, 2, 200)
		box := physics.NewRigidBox(150, 100, 20, 20, 1)
		box.Bullet = bullet
		box.VX = 500
		w.AddBody(wall, box)
		deepest := float32(0)
		for range 30 {
			w.Step(step)
			deepest = max(deepest, box.X+10-199)
		}
		if bullet && deepest > 0 {
			t.Fatalf("bullet box went %v px into the wall", deepest)
		}
		if !bullet && deepest <= 0 {
			t.Fatalf("plain box never entered the wall (deepest %v); the test no longer tells a bullet apart", deepest)
		}
	}
}

func TestRigidFastBodySlidesAlongFloor(t *testing.T) {
	// A body sliding fast along the ground it rests on is not stopped by it.
	w := &physics.RigidWorld{GravityY: 600}
	floor := physics.NewStaticRigidBox(5000, 110, 10000, 20)
	floor.Friction = 0
	crate := physics.NewRigidBox(100, 90, 20, 20, 1)
	crate.Friction = 0
	w.AddBody(floor, crate)
	for range 30 {
		w.Step(step)
	}
	crate.VX = 3000
	start := crate.X
	for range 60 {
		w.Step(step)
	}
	if got := crate.X - start; math.Abs(float64(got)-3000) > 60 {
		t.Fatalf("crate slid %v px in 1 s at 3000 px/s", got)
	}
	if math.Abs(float64(crate.Y)-90) > 1 {
		t.Fatalf("crate left the floor: y=%v", crate.Y)
	}
}

func TestRigidFastBodyIgnoresFilteredStatic(t *testing.T) {
	w := &physics.RigidWorld{}
	wall := physics.NewStaticRigidBox(400, 100, 4, 200)
	wall.Layer = physics.LayerBit(2)
	wall.Mask = physics.LayerBit(2)
	ball := physics.NewRigidCircle(100, 100, 6, 1)
	ball.Layer = physics.LayerBit(1)
	ball.Mask = physics.LayerBit(1)
	ball.VX = 5000
	w.AddBody(wall, ball)
	for range 6 {
		w.Step(step)
	}
	if ball.X < 400 {
		t.Fatalf("ball stopped at x=%v by a wall it does not collide with", ball.X)
	}
}
