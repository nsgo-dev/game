package physics_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/physics"
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

func TestRigidPointBlankShotStopsAtThinWall(t *testing.T) {
	// A ball fired at a wall it already touches, or nearly touches, has no contact with it yet and must still stop at its face.
	for _, gap := range []float32{0, 0.05, 0.2} {
		for _, bullet := range []bool{false, true} {
			w := &physics.RigidWorld{}
			wall := physics.NewStaticRigidBox(400, 100, 4, 200)
			ball := physics.NewRigidCircle(398-6-gap, 100, 6, 1)
			ball.Bullet = bullet
			ball.VX = 5000
			w.AddBody(wall, ball)
			for i := range 10 {
				w.Step(step)
				if ball.X > 398-6+0.5 {
					t.Fatalf("gap %v bullet %v: ball at x=%v after step %d, past the wall face at 392", gap, bullet, ball.X, i)
				}
			}
			if ball.VX > 1 {
				t.Fatalf("gap %v bullet %v: ball keeps VX=%v into the wall", gap, bullet, ball.VX)
			}
		}
	}
}

func TestRigidFastBodySlidesOverSeams(t *testing.T) {
	// Ground built from many static pieces, like a row of tiles or a flat chain, is as smooth to a swept body as one long box: the sweep never stops it at the seams between the pieces. (A crate much faster than these still catches on tile seams through the regular contacts, swept or not, as it did before continuous collision.)
	tiles := func() []*physics.RigidBody {
		var out []*physics.RigidBody
		for i := range 160 {
			b := physics.NewStaticRigidBox(float32(16+32*i), 110, 32, 20)
			b.Friction = 0
			out = append(out, b)
		}
		return out
	}
	chain := func() []*physics.RigidBody {
		pts := make([]core.Point, 400)
		for i := range pts {
			pts[i] = core.Point{X: core.Points(-100 + 15*i), Y: 100}
		}
		b := physics.NewStaticRigidBody(physics.ChainShape(pts, false))
		b.Friction = 0
		return []*physics.RigidBody{b}
	}
	crate := func() *physics.RigidBody { return physics.NewRigidBox(100, 90, 20, 20, 1) }
	ball := func() *physics.RigidBody { return physics.NewRigidCircle(100, 94, 6, 1) }
	cases := []struct {
		name   string
		ground func() []*physics.RigidBody
		body   func() *physics.RigidBody
		bullet bool
		speed  float32
	}{
		{"bullet crate on tiles", tiles, crate, true, 400},
		{"bullet ball on tiles", tiles, ball, true, 2000},
		{"fast ball on tiles", tiles, ball, false, 2000},
		{"bullet ball on chain", chain, ball, true, 400},
		{"fast ball on chain", chain, ball, false, 2000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &physics.RigidWorld{GravityY: 600}
			for _, g := range c.ground() {
				w.AddBody(g)
			}
			b := c.body()
			b.Friction = 0
			b.Bullet = c.bullet
			w.AddBody(b)
			for range 30 {
				w.Step(step)
			}
			restY := b.Y
			b.VX = c.speed
			start := b.X
			for i := range 60 {
				w.Step(step)
				if math.Abs(float64(b.Y-restY)) > 1 {
					t.Fatalf("body left the ground at step %d: y=%v, rested at %v", i, b.Y, restY)
				}
			}
			if got := b.X - start; math.Abs(float64(got-c.speed)) > 0.02*float64(c.speed) {
				t.Fatalf("body slid %v px in 1 s at %v px/s", got, c.speed)
			}
		})
	}
}

func TestRigidFastBodyStillStopsAtWallOnTiledFloor(t *testing.T) {
	// Ignoring the seams of the ground must not ignore a wall standing on it.
	w := &physics.RigidWorld{GravityY: 600}
	for i := range 40 {
		tile := physics.NewStaticRigidBox(float32(16+32*i), 110, 32, 20)
		tile.Friction = 0
		w.AddBody(tile)
	}
	wall := physics.NewStaticRigidBox(800, 60, 4, 80)
	crate := physics.NewRigidBox(100, 90, 20, 20, 1)
	crate.Friction = 0
	w.AddBody(wall, crate)
	for range 30 {
		w.Step(step)
	}
	crate.VX = 6000
	for i := range 60 {
		w.Step(step)
		if crate.X+10 > 798+0.5 {
			t.Fatalf("crate at x=%v after step %d, past the wall face at 798", crate.X, i)
		}
	}
}

func TestRigidBulletStopsAtThinDynamicWall(t *testing.T) {
	cases := []struct {
		name      string
		wallAngle float32
		wallVX    float32
		bulletVY  float32
		speed     float32
		rest      float32
	}{
		{"stationary_2000", 0, 0, 0, 2000, 0},
		{"stationary_5000", 0, 0, 0, 5000, 0},
		{"stationary_20000", 0, 0, 0, 20000, 0},
		{"elastic_5000", 0, 0, 0, 5000, 1},
		{"rotated_5000", 0.25, 0, 0, 5000, 0},
		{"oblique_5000", 0, 0, 1200, 5000, 0},
		{"moving_head_on_5000", 0, -600, 0, 5000, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &physics.RigidWorld{}
			wall := physics.NewRigidBox(400, 100, 4, 200, 10)
			wall.Angle = tc.wallAngle
			wall.VX = tc.wallVX
			wall.Restitution = tc.rest
			bullet := physics.NewRigidCircle(100, 100, 2, 1)
			bullet.Bullet = true
			bullet.VX = tc.speed
			bullet.VY = tc.bulletVY
			bullet.Restitution = tc.rest
			w.AddBody(wall, bullet)

			gotBegin := false
			w.OnContact = func(ev physics.ContactEvent) {
				if ev.Kind == physics.ContactBegin && ((ev.BodyA == wall && ev.BodyB == bullet) || (ev.BodyA == bullet && ev.BodyB == wall)) {
					gotBegin = true
				}
			}

			for i := range 20 {
				w.Step(step)
				// Transform bullet center into wall's local frame (+X is through the wall, Y is along its 200 px span).
				sinA, cosA := math.Sincos(float64(wall.Angle))
				dx := float64(bullet.X) - float64(wall.X)
				dy := float64(bullet.Y) - float64(wall.Y)
				localX := dx*cosA + dy*sinA
				localY := -dx*sinA + dy*cosA
				if math.Abs(localY) <= 100 && localX > 0 {
					t.Fatalf("bullet tunneled past dynamic wall at step %d: bullet=(%v,%v) wall=(%v,%v) angle=%v local=(%v,%v)",
						i, bullet.X, bullet.Y, wall.X, wall.Y, wall.Angle, localX, localY)
				}
			}
			if !gotBegin {
				t.Fatal("expected ContactBegin event for bullet vs dynamic wall")
			}
			if wall.VX <= tc.wallVX {
				t.Fatalf("expected bullet impact to transfer +X momentum to dynamic wall: wall.VX=%v, initial=%v", wall.VX, tc.wallVX)
			}
			if tc.rest == 1 && bullet.VX >= 0 {
				t.Fatalf("expected elastic bullet to rebound: bullet.VX=%v", bullet.VX)
			}
		})
	}
}

func TestRigidNonBulletStillPassesThinDynamicWall(t *testing.T) {
	w := &physics.RigidWorld{}
	wall := physics.NewRigidBox(400, 100, 4, 200, 10)
	ball := physics.NewRigidCircle(100, 100, 2, 1)
	ball.Bullet = false
	ball.VX = 20000
	w.AddBody(wall, ball)
	for range 4 {
		w.Step(step)
	}
	if ball.X <= wall.X+10 {
		t.Fatalf("non-bullet fast ball stopped at x=%v (wall x=%v); expected non-bullets to keep passing dynamic bodies", ball.X, wall.X)
	}
}

func TestRigidBulletStopsAtMovingKinematicPaddle(t *testing.T) {
	w := &physics.RigidWorld{}
	paddle := physics.NewRigidBox(400, 100, 6, 160, 1)
	paddle.Kinematic = true
	bullet := physics.NewRigidCircle(100, 100, 3, 1)
	bullet.Bullet = true
	bullet.VX = 6000
	w.AddBody(paddle, bullet)

	for i := range 10 {
		paddle.SetTargetPose(400-float32(i+1)*20, 100, 0)
		w.Step(step)
		if bullet.X > paddle.X {
			t.Fatalf("bullet tunneled past moving kinematic paddle at step %d: bullet.X=%v paddle.X=%v", i, bullet.X, paddle.X)
		}
	}
}

func TestRigidBulletNearMissDynamicBodyDoesNotStop(t *testing.T) {
	w := &physics.RigidWorld{}
	target := physics.NewRigidBox(400, 140, 20, 20, 10)
	bullet := physics.NewRigidCircle(100, 100, 4, 1)
	bullet.Bullet = true
	bullet.VX = 6000
	w.AddBody(target, bullet)

	for range 10 {
		w.Step(step)
	}
	if bullet.X < 900 {
		t.Fatalf("bullet flying clear of dynamic target stopped early at x=%v", bullet.X)
	}
}

func TestRigidBulletDynamicDeterminismAndSnapshot(t *testing.T) {
	run := func(saveStep int) uint64 {
		w := &physics.RigidWorld{}
		wall := physics.NewRigidBox(400, 100, 4, 200, 10)
		wall.VX = -200
		bullet := physics.NewRigidCircle(100, 100, 2, 1)
		bullet.Bullet = true
		bullet.VX = 5000
		bullet.Restitution = 0.6
		w.AddBody(wall, bullet)
		for i := range 15 {
			if i == saveStep {
				snap := w.Save()
				w = &physics.RigidWorld{}
				if err := w.Restore(snap); err != nil {
					t.Fatalf("Restore: %v", err)
				}
			}
			w.Step(step)
		}
		return w.Checksum()
	}
	want := run(-1)
	if got := run(2); got != want {
		t.Fatalf("restored checksum = %#x, want %#x", got, want)
	}
}

func BenchmarkRigidCCDFastBody(b *testing.B) {
	w := &physics.RigidWorld{}
	w.AddBody(physics.NewStaticRigidBox(500, 100, 4, 200))
	dyn := physics.NewRigidBox(300, 100, 4, 200, 10)
	bullet := physics.NewRigidCircle(0, 100, 2, 1)
	bullet.Bullet = true
	w.AddBody(dyn, bullet)
	// Warm scratch buffers.
	bullet.VX = 20000
	w.Step(step)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		bullet.X, bullet.Y = 0, 100
		bullet.VX, bullet.VY, bullet.AngularVel = 20000, 0, 0
		dyn.X, dyn.Y = 300, 100
		dyn.VX, dyn.VY, dyn.AngularVel = 0, 0, 0
		w.Step(step)
	}
}
