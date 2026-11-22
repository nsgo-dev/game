package physics3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func approx(a, b, tol float32) bool {
	return float32(math.Abs(float64(a-b))) <= tol
}

func approxVec(a, b physics3d.Vec3, tol float32) bool {
	return approx(a.X, b.X, tol) && approx(a.Y, b.Y, tol) && approx(a.Z, b.Z, tol)
}

func runLoop(t *testing.T, rate int, d time.Duration, update func(dt time.Duration)) {
	t.Helper()
	l := &game.Loop{Rate: rate, Update: update}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})
	app.Advance(d)
}

func TestVec3Math(t *testing.T) {
	a := physics3d.Vec3{X: 1, Y: 2, Z: 3}
	b := physics3d.Vec3{X: 4, Y: -5, Z: 6}

	if got := a.Add(b); got != (physics3d.Vec3{X: 5, Y: -3, Z: 9}) {
		t.Fatalf("Add = %v", got)
	}
	if got := a.Sub(b); got != (physics3d.Vec3{X: -3, Y: 7, Z: -3}) {
		t.Fatalf("Sub = %v", got)
	}
	if got := a.Scale(2); got != (physics3d.Vec3{X: 2, Y: 4, Z: 6}) {
		t.Fatalf("Scale = %v", got)
	}
	if got := a.Dot(b); !approx(got, 12, 1e-6) {
		t.Fatalf("Dot = %v, want 12", got)
	}
	// Right-handed Y-up: X x Y == Z.
	x := physics3d.Vec3{X: 1}
	y := physics3d.Vec3{Y: 1}
	z := physics3d.Vec3{Z: 1}
	if got := x.Cross(y); got != z {
		t.Fatalf("X x Y = %v, want %v", got, z)
	}
	v := physics3d.Vec3{X: 3, Y: 4, Z: 0}
	if !approx(v.Len(), 5, 1e-6) || !approx(v.LenSq(), 25, 1e-6) {
		t.Fatalf("Len/LenSq = %v/%v", v.Len(), v.LenSq())
	}
	if got := v.Norm(); !approxVec(got, physics3d.Vec3{X: 0.6, Y: 0.8}, 1e-6) {
		t.Fatalf("Norm = %v", got)
	}
	if got := (physics3d.Vec3{}).Norm(); got != (physics3d.Vec3{}) {
		t.Fatalf("zero Norm = %v, want zero", got)
	}
	if got := (physics3d.Vec3{X: 1, Y: 2, Z: 3}).Dist(physics3d.Vec3{X: 4, Y: 6, Z: 3}); !approx(got, 5, 1e-6) {
		t.Fatalf("Dist = %v, want 5", got)
	}
}

func TestAllSixShapeOverlapPairs(t *testing.T) {
	t.Run("Box-Box", func(t *testing.T) {
		a := physics3d.Box{Center: physics3d.Vec3{X: 0, Y: 0.8, Z: 0}, HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}
		b := physics3d.BoxFromMinMax(physics3d.Vec3{X: -1, Y: -1, Z: -1}, physics3d.Vec3{X: 1, Y: 0.5, Z: 1})
		c := physics3d.TestBoxBox(a, b)
		if !c.Hit {
			t.Fatal("expected Box-Box hit")
		}
		if !approxVec(c.Normal, physics3d.Vec3{Y: 1}, 1e-5) || !approx(c.Depth, 0.2, 1e-5) {
			t.Fatalf("Box-Box contact = %+v, want normal (0,1,0) depth 0.2", c)
		}
		// Separating A along Normal*Depth clears the overlap.
		sep := a.Translated(c.Normal.Scale(c.Depth + 1e-4))
		if physics3d.TestBoxBox(sep, b).Hit {
			t.Fatal("expected separation to clear Box-Box overlap")
		}
		// Flush touching boxes do not report penetration.
		flush := physics3d.Box{Center: physics3d.Vec3{X: 0, Y: 1.0, Z: 0}, HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}
		if physics3d.TestBoxBox(flush, b).Hit {
			t.Fatal("expected flush boxes not to penetrate")
		}
	})

	t.Run("Sphere-Sphere", func(t *testing.T) {
		a := physics3d.Sphere{Center: physics3d.Vec3{X: 1.5, Y: 0, Z: 0}, Radius: 1}
		b := physics3d.Sphere{Center: physics3d.Vec3{X: 0, Y: 0, Z: 0}, Radius: 1}
		c := physics3d.TestSphereSphere(a, b)
		if !c.Hit || !approxVec(c.Normal, physics3d.Vec3{X: 1}, 1e-5) || !approx(c.Depth, 0.5, 1e-5) {
			t.Fatalf("Sphere-Sphere contact = %+v", c)
		}
		far := physics3d.Sphere{Center: physics3d.Vec3{X: 3, Y: 0, Z: 0}, Radius: 1}
		if physics3d.TestSphereSphere(far, b).Hit {
			t.Fatal("expected separated spheres not to overlap")
		}
	})

	t.Run("Capsule-Capsule", func(t *testing.T) {
		a := physics3d.Capsule{A: physics3d.Vec3{X: 0.6, Y: 0, Z: 0}, B: physics3d.Vec3{X: 0.6, Y: 2, Z: 0}, Radius: 0.5}
		b := physics3d.Capsule{A: physics3d.Vec3{X: 0, Y: 0, Z: 0}, B: physics3d.Vec3{X: 0, Y: 2, Z: 0}, Radius: 0.5}
		c := physics3d.TestCapsuleCapsule(a, b)
		if !c.Hit || !approxVec(c.Normal, physics3d.Vec3{X: 1}, 1e-5) || !approx(c.Depth, 0.4, 1e-5) {
			t.Fatalf("Capsule-Capsule contact = %+v", c)
		}
		// Skew capsules crossing near origin.
		skewA := physics3d.Capsule{A: physics3d.Vec3{X: -1, Y: 0.5, Z: 0}, B: physics3d.Vec3{X: 1, Y: 0.5, Z: 0}, Radius: 0.4}
		skewB := physics3d.Capsule{A: physics3d.Vec3{X: 0, Y: 0, Z: -1}, B: physics3d.Vec3{X: 0, Y: 0, Z: 1}, Radius: 0.4}
		cs := physics3d.TestCapsuleCapsule(skewA, skewB)
		if !cs.Hit || !approxVec(cs.Normal, physics3d.Vec3{Y: 1}, 1e-5) || !approx(cs.Depth, 0.3, 1e-5) {
			t.Fatalf("skew Capsule-Capsule contact = %+v", cs)
		}
	})

	t.Run("Box-Sphere", func(t *testing.T) {
		box := physics3d.Box{Center: physics3d.Vec3{}, HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}
		sph := physics3d.Sphere{Center: physics3d.Vec3{X: 0, Y: 1.3, Z: 0}, Radius: 0.5}
		// Normal points from B (sphere) toward A (box) => -Y.
		c := physics3d.TestBoxSphere(box, sph)
		if !c.Hit || !approxVec(c.Normal, physics3d.Vec3{Y: -1}, 1e-5) || !approx(c.Depth, 0.2, 1e-5) {
			t.Fatalf("Box-Sphere outside contact = %+v", c)
		}
		// Reversed order via TestShapes flips normal to +Y.
		cr := physics3d.TestShapes(physics3d.SphereShape(sph), physics3d.BoxShape(box))
		if !cr.Hit || !approxVec(cr.Normal, physics3d.Vec3{Y: 1}, 1e-5) || !approx(cr.Depth, 0.2, 1e-5) {
			t.Fatalf("Sphere-Box reversed contact = %+v", cr)
		}
		// Sphere center inside box near +Y face keeps normal continuous (-Y).
		inside := physics3d.Sphere{Center: physics3d.Vec3{X: 0, Y: 0.8, Z: 0}, Radius: 0.5}
		ci := physics3d.TestBoxSphere(box, inside)
		if !ci.Hit || !approxVec(ci.Normal, physics3d.Vec3{Y: -1}, 1e-5) || !approx(ci.Depth, 0.7, 1e-5) {
			t.Fatalf("Box-Sphere inside contact = %+v", ci)
		}
	})

	t.Run("Box-Capsule", func(t *testing.T) {
		floor := physics3d.BoxFromMinMax(physics3d.Vec3{X: -5, Y: -1, Z: -5}, physics3d.Vec3{X: 5, Y: 0, Z: 5})
		// Vertical capsule penetrating floor by 0.1 (bottom sphere center at Y=0.2, radius 0.3).
		cap := physics3d.Capsule{A: physics3d.Vec3{Y: 0.2}, B: physics3d.Vec3{Y: 1.2}, Radius: 0.3}
		c := physics3d.TestBoxCapsule(floor, cap)
		if !c.Hit || !approxVec(c.Normal, physics3d.Vec3{Y: -1}, 1e-5) || !approx(c.Depth, 0.1, 1e-5) {
			t.Fatalf("Box-Capsule contact = %+v", c)
		}
		// Deeply penetrating capsule (bottom segment endpoint inside box at Y=-0.1).
		deepCap := physics3d.Capsule{A: physics3d.Vec3{Y: -0.1}, B: physics3d.Vec3{Y: 0.9}, Radius: 0.3}
		cd := physics3d.TestBoxCapsule(floor, deepCap)
		if !cd.Hit || !approxVec(cd.Normal, physics3d.Vec3{Y: -1}, 1e-5) || !approx(cd.Depth, 0.4, 1e-5) {
			t.Fatalf("Box-Capsule deep contact = %+v", cd)
		}
		// TestShapes(Capsule, Box) flips normal to +Y.
		cf := physics3d.TestShapes(physics3d.CapsuleShape(cap), physics3d.BoxShape(floor))
		if !cf.Hit || !approxVec(cf.Normal, physics3d.Vec3{Y: 1}, 1e-5) || !approx(cf.Depth, 0.1, 1e-5) {
			t.Fatalf("Capsule-Box contact = %+v", cf)
		}
		// Tilted capsule grazing a box edge from outside.
		cube := physics3d.Box{Center: physics3d.Vec3{}, HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}
		tilted := physics3d.Capsule{A: physics3d.Vec3{X: 0.5, Y: 1.5, Z: 0}, B: physics3d.Vec3{X: 1.5, Y: 0.5, Z: 0}, Radius: 0.5}
		ct := physics3d.TestBoxCapsule(cube, tilted)
		if !ct.Hit || ct.Depth <= 0 {
			t.Fatalf("tilted Box-Capsule contact = %+v", ct)
		}
		if physics3d.TestBoxCapsule(cube, tilted.Translated(ct.Normal.Neg().Scale(ct.Depth+1e-3))).Hit {
			t.Fatal("separating tilted capsule by Depth did not resolve overlap")
		}
	})

	t.Run("Sphere-Capsule", func(t *testing.T) {
		sph := physics3d.Sphere{Center: physics3d.Vec3{X: 0.6, Y: 1.0, Z: 0}, Radius: 0.5}
		cap := physics3d.Capsule{A: physics3d.Vec3{Y: 0}, B: physics3d.Vec3{Y: 2}, Radius: 0.3}
		c := physics3d.TestSphereCapsule(sph, cap)
		if !c.Hit || !approxVec(c.Normal, physics3d.Vec3{X: 1}, 1e-5) || !approx(c.Depth, 0.2, 1e-5) {
			t.Fatalf("Sphere-Capsule contact = %+v", c)
		}
		cf := physics3d.TestShapes(physics3d.CapsuleShape(cap), physics3d.SphereShape(sph))
		if !cf.Hit || !approxVec(cf.Normal, physics3d.Vec3{X: -1}, 1e-5) || !approx(cf.Depth, 0.2, 1e-5) {
			t.Fatalf("Capsule-Sphere contact = %+v", cf)
		}
	})
}

func TestRaycastsAndWorldQueries(t *testing.T) {
	// Standalone shape raycasts.
	box := physics3d.Box{Center: physics3d.Vec3{X: 5}, HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}
	ray := physics3d.Ray{Origin: physics3d.Vec3{}, Dir: physics3d.Vec3{X: 1}, MaxDist: 10}
	if h, ok := physics3d.RaycastBox(ray, box); !ok || !approx(h.Distance, 4, 1e-5) || !approx(h.T, 0.4, 1e-5) || !approxVec(h.Normal, physics3d.Vec3{X: -1}, 1e-5) {
		t.Fatalf("RaycastBox = %+v, %v", h, ok)
	}

	sph := physics3d.Sphere{Center: physics3d.Vec3{X: 5}, Radius: 1}
	if h, ok := physics3d.RaycastSphere(ray, sph); !ok || !approx(h.Distance, 4, 1e-5) || !approxVec(h.Normal, physics3d.Vec3{X: -1}, 1e-5) {
		t.Fatalf("RaycastSphere = %+v, %v", h, ok)
	}

	cap := physics3d.Capsule{A: physics3d.Vec3{X: 5, Y: -1}, B: physics3d.Vec3{X: 5, Y: 1}, Radius: 0.5}
	// Hit cylinder body.
	if h, ok := physics3d.RaycastCapsule(ray, cap); !ok || !approx(h.Distance, 4.5, 1e-5) || !approxVec(h.Normal, physics3d.Vec3{X: -1}, 1e-5) {
		t.Fatalf("RaycastCapsule cylinder = %+v, %v", h, ok)
	}
	// Hit top hemisphere cap from above.
	downRay := physics3d.Ray{Origin: physics3d.Vec3{X: 5, Y: 5}, Dir: physics3d.Vec3{Y: -1}, MaxDist: 10}
	if h, ok := physics3d.RaycastCapsule(downRay, cap); !ok || !approx(h.Distance, 3.5, 1e-5) || !approxVec(h.Normal, physics3d.Vec3{Y: 1}, 1e-5) {
		t.Fatalf("RaycastCapsule top cap = %+v, %v", h, ok)
	}
	// Origins inside primitives report an immediate hit at T = 0.
	insideRay := physics3d.Ray{Origin: physics3d.Vec3{X: 5}, Dir: physics3d.Vec3{X: 1}, MaxDist: 5}
	if h, ok := physics3d.RaycastBox(insideRay, box); !ok || h.T != 0 {
		t.Fatalf("RaycastBox inside = %+v, %v", h, ok)
	}
	if h, ok := physics3d.RaycastSphere(insideRay, sph); !ok || h.T != 0 {
		t.Fatalf("RaycastSphere inside = %+v, %v", h, ok)
	}
	if h, ok := physics3d.RaycastCapsule(insideRay, cap); !ok || h.T != 0 {
		t.Fatalf("RaycastCapsule inside = %+v, %v", h, ok)
	}
	// Short MaxDist misses distant targets.
	shortRay := physics3d.Ray{Origin: physics3d.Vec3{}, Dir: physics3d.Vec3{X: 1}, MaxDist: 2}
	if _, ok := physics3d.RaycastBox(shortRay, box); ok {
		t.Fatal("expected short ray to miss box")
	}

	// Mixed world raycast sorted by ascending T in [0, 1].
	w := physics3d.NewWorld()
	bFar := w.AddBody(physics3d.Body{
		Tag:    "box-far",
		Shape:  physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
		Pos:    physics3d.Vec3{X: 8},
		Static: true,
	})
	bMid := w.AddBody(physics3d.Body{
		Tag:    "capsule-mid",
		Shape:  physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.5, 2)),
		Pos:    physics3d.Vec3{X: 5},
		Static: true,
	})
	bNear := w.AddBody(physics3d.Body{
		Tag:    "sphere-near",
		Shape:  physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
		Pos:    physics3d.Vec3{X: 2},
		Static: true,
	})

	hits := w.Raycast(physics3d.Vec3{}, physics3d.Vec3{X: 10})
	if len(hits) != 3 {
		t.Fatalf("World.Raycast got %d hits, want 3", len(hits))
	}
	wantIDs := []int{bNear.ID, bMid.ID, bFar.ID}
	wantTs := []float32{0.15, 0.45, 0.75}
	for i := range hits {
		if hits[i].Body.ID != wantIDs[i] || !approx(hits[i].T, wantTs[i], 1e-5) {
			t.Fatalf("hit[%d] = (ID=%d, T=%v), want (ID=%d, T=%v)", i, hits[i].Body.ID, hits[i].T, wantIDs[i], wantTs[i])
		}
		if hits[i].T < 0 || hits[i].T > 1 {
			t.Fatalf("hit[%d].T = %v out of [0,1]", i, hits[i].T)
		}
	}

	first, ok := w.RaycastFirst(physics3d.Vec3{}, physics3d.Vec3{X: 10})
	if !ok || first.Body.ID != bNear.ID || !approx(first.T, 0.15, 1e-5) {
		t.Fatalf("RaycastFirst = %+v, %v", first, ok)
	}

	// QueryOverlap returns overlapping bodies in deterministic insertion order.
	overlaps := w.QueryOverlap(physics3d.BoxShape(physics3d.BoxFromMinMax(
		physics3d.Vec3{X: 1, Y: -1, Z: -1},
		physics3d.Vec3{X: 6, Y: 1, Z: 1},
	)))
	if len(overlaps) != 2 || overlaps[0].Body.ID != bMid.ID || overlaps[1].Body.ID != bNear.ID {
		t.Fatalf("QueryOverlap returned %d hits (ids mismatch)", len(overlaps))
	}
}

func TestRigidBodyBoxStackSettlesDeterministically(t *testing.T) {
	runStack := func(t *testing.T) ([]physics3d.Vec3, []physics3d.Vec3, []bool) {
		t.Helper()
		w := physics3d.NewWorld()
		// Static floor with top surface at Y = 0.
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 1, Z: 20}}),
			physics3d.Vec3{Y: -1},
		))

		const stackCount = 4
		unitBox := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
		boxes := make([]*physics3d.Body, stackCount)
		for i := range stackCount {
			// Drop each 1x1x1 box with a small initial gap above the one below.
			y := float32(i)*1.15 + 0.65
			boxes[i] = w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: y}, 1.0))
		}

		runLoop(t, 60, 4*time.Second, func(dt time.Duration) {
			w.Step(dt)
		})

		pos := make([]physics3d.Vec3, stackCount)
		vel := make([]physics3d.Vec3, stackCount)
		sleeping := make([]bool, stackCount)
		for i, b := range boxes {
			pos[i] = b.Pos
			vel[i] = b.Vel
			sleeping[i] = b.Sleeping
		}

		// Verify zero penetration across floor and every adjacent box pair.
		floor := w.Bodies()[0]
		for i, b := range boxes {
			if c := physics3d.TestShapes(b.WorldShape(), floor.WorldShape()); c.Hit && c.Depth > 0.002 {
				t.Fatalf("box %d penetrates floor by %v", i, c.Depth)
			}
			if i > 0 {
				if c := physics3d.TestShapes(b.WorldShape(), boxes[i-1].WorldShape()); c.Hit && c.Depth > 0.002 {
					t.Fatalf("box %d penetrates box %d by %v", i, i-1, c.Depth)
				}
			}
		}

		return pos, vel, sleeping
	}

	pos1, vel1, sleep1 := runStack(t)
	pos2, vel2, sleep2 := runStack(t)

	for i := range pos1 {
		wantY := float32(i) + 0.5
		if !approx(pos1[i].Y, wantY, 0.01) {
			t.Fatalf("box %d settled Y = %v, want %v", i, pos1[i].Y, wantY)
		}
		if vel1[i].Len() > 0.02 {
			t.Fatalf("box %d residual speed = %v, want near zero", i, vel1[i].Len())
		}
		if !sleep1[i] {
			t.Fatalf("box %d did not settle to sleep", i)
		}
		// Bit-for-bit identical determinism across independent runs.
		if pos1[i] != pos2[i] || vel1[i] != vel2[i] || sleep1[i] != sleep2[i] {
			t.Fatalf("non-deterministic box %d: run1=(%v,%v,%v) run2=(%v,%v,%v)",
				i, pos1[i], vel1[i], sleep1[i], pos2[i], vel2[i], sleep2[i])
		}
	}
}

func TestRigidBodyBounceAndFriction(t *testing.T) {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 30, Y: 1, Z: 30}}),
		physics3d.Vec3{Y: -1},
	))

	// Bouncy sphere dropped from Y = 4.
	ballBody := physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), physics3d.Vec3{Y: 4}, 1)
	ballBody.Restitution = 0.8
	ballBody.LinearDamping = 0
	ball := w.AddBody(ballBody)

	// Sliding box launched horizontally along +X on the floor.
	sliderBody := physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
		physics3d.Vec3{X: 5, Y: 0.5, Z: 0},
		1,
	)
	sliderBody.Vel = physics3d.Vec3{X: 4}
	sliderBody.Friction = 0.6
	slider := w.AddBody(sliderBody)

	var maxReboundY float32
	hitFloor := false
	runLoop(t, 60, 2*time.Second, func(dt time.Duration) {
		w.Step(dt)
		if ball.Vel.Y > 0.5 {
			hitFloor = true
		}
		if hitFloor && ball.Pos.Y > maxReboundY {
			maxReboundY = ball.Pos.Y
		}
	})

	if !hitFloor || maxReboundY < 2.0 {
		t.Fatalf("bouncy ball did not rebound as expected: hitFloor=%v maxReboundY=%v", hitFloor, maxReboundY)
	}
	if slider.Pos.X <= 5.5 || slider.Vel.Len() > 0.05 {
		t.Fatalf("sliding box should slide and come to rest: pos=%v vel=%v", slider.Pos, slider.Vel)
	}

	// ApplyImpulse wakes a settled body and launches it.
	slider.ApplyImpulse(physics3d.Vec3{X: 3})
	if slider.Sleeping || slider.Vel.X < 2.5 {
		t.Fatalf("ApplyImpulse failed to wake slider: sleeping=%v vel=%v", slider.Sleeping, slider.Vel)
	}

	// RemoveBody removes the floor and wakes resting bodies above it.
	floorID := w.Bodies()[0].ID
	if !w.RemoveBody(floorID) {
		t.Fatal("RemoveBody returned false for existing floor")
	}
	startY := slider.Pos.Y
	runLoop(t, 60, 500*time.Millisecond, func(dt time.Duration) {
		w.Step(dt)
	})
	if slider.Pos.Y >= startY-0.5 {
		t.Fatalf("body did not fall after floor removal: startY=%v endY=%v", startY, slider.Pos.Y)
	}
}

func TestCharacterControllerStairsSlopesAndWalls(t *testing.T) {
	t.Run("ClimbsReferenceStaircase", func(t *testing.T) {
		w := physics3d.NewWorld()
		// Flat floor at Y = 0.
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -5, Y: -1, Z: -5}, physics3d.Vec3{X: 10, Y: 0, Z: 5})),
			physics3d.Vec3{},
		))
		// 4-step staircase: each step rises 0.25 (<= StepHeight 0.35), tread depth 0.6.
		for i := range 4 {
			xMin := 1.0 + float32(i)*0.6
			yMax := float32(i+1) * 0.25
			w.AddBody(physics3d.NewStaticBody(
				physics3d.BoxShape(physics3d.BoxFromMinMax(
					physics3d.Vec3{X: xMin, Y: 0, Z: -2},
					physics3d.Vec3{X: 10, Y: yMax, Z: 2},
				)),
				physics3d.Vec3{},
			))
		}

		cc := physics3d.NewCharacterController(physics3d.Vec3{X: 0, Y: 0, Z: 0}, 0.3, 1.8)
		cc.StepHeight = 0.35

		stepUps := 0
		runLoop(t, 60, 2*time.Second, func(dt time.Duration) {
			res := cc.Move(w, physics3d.Vec3{X: 2.5}, dt)
			if res.SteppedUp {
				stepUps++
			}
		})

		if stepUps < 4 {
			t.Fatalf("expected at least 4 step-ups, got %d (pos=%v)", stepUps, cc.Pos)
		}
		if !approx(cc.Pos.Y, 1.0, 0.02) || !cc.Grounded {
			t.Fatalf("controller did not reach top of 4-step staircase: pos=%v grounded=%v", cc.Pos, cc.Grounded)
		}
		if cc.Pos.X < 3.5 {
			t.Fatalf("controller did not advance across staircase: pos=%v", cc.Pos)
		}
	})

	t.Run("RespectsHeadroomOnStairs", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -5, Y: -1, Z: -5}, physics3d.Vec3{X: 5, Y: 0, Z: 5})),
			physics3d.Vec3{},
		))
		// Step of height 0.25.
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: 1, Y: 0, Z: -2}, physics3d.Vec3{X: 4, Y: 0.25, Z: 2})),
			physics3d.Vec3{},
		))
		// Low ceiling at Y = 1.85 leaves only 0.05 headroom for a 1.80-tall controller.
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -5, Y: 1.85, Z: -5}, physics3d.Vec3{X: 5, Y: 3, Z: 5})),
			physics3d.Vec3{},
		))

		cc := physics3d.NewCharacterController(physics3d.Vec3{X: 0, Y: 0, Z: 0}, 0.3, 1.8)
		cc.StepHeight = 0.35

		runLoop(t, 60, time.Second, func(dt time.Duration) {
			cc.Move(w, physics3d.Vec3{X: 2.5}, dt)
		})

		if cc.Pos.Y > 0.05 || cc.Pos.X > 0.75 {
			t.Fatalf("controller climbed stair despite low ceiling: pos=%v", cc.Pos)
		}
	})

	t.Run("SlidesSmoothlyUpAndDown30DegreeRamp", func(t *testing.T) {
		w := physics3d.NewWorld()
		// Flat floor at Y = 0.
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -5, Y: -1, Z: -5}, physics3d.Vec3{X: 12, Y: 0, Z: 5})),
			physics3d.Vec3{},
		))
		// 30-degree smooth ramp formed by a wide-radius tilted capsule along Z = 0.
		// Its top surface passes through (1, 0, 0) and rises at exactly 30 degrees.
		const (
			angle30 = float64(30 * math.Pi / 180)
			rampR   = float32(1.0)
			rampLen = float32(8.0)
		)
		cos30 := float32(math.Cos(angle30))
		sin30 := float32(math.Sin(angle30))
		rampA := physics3d.Vec3{X: 1.0, Y: -rampR / cos30, Z: 0}
		rampB := rampA.Add(physics3d.Vec3{X: rampLen * cos30, Y: rampLen * sin30, Z: 0})
		w.AddBody(physics3d.NewStaticBody(
			physics3d.CapsuleShape(physics3d.Capsule{A: rampA, B: rampB, Radius: rampR}),
			physics3d.Vec3{},
		))

		cc := physics3d.NewCharacterController(physics3d.Vec3{X: 0, Y: 0, Z: 0}, 0.3, 1.8)

		// Walk up the 30-degree ramp.
		runLoop(t, 60, 2*time.Second, func(dt time.Duration) {
			cc.Move(w, physics3d.Vec3{X: 2.5}, dt)
		})
		peakPos := cc.Pos
		if peakPos.X < 3.5 || peakPos.Y < 1.2 || !cc.Grounded {
			t.Fatalf("controller failed to climb 30-degree ramp: pos=%v grounded=%v normal=%v", peakPos, cc.Grounded, cc.GroundNormal)
		}
		if !approx(cc.GroundNormal.Y, cos30, 0.05) {
			t.Fatalf("30-degree ramp ground normal Y = %v, want ~%v", cc.GroundNormal.Y, cos30)
		}

		// Walk smoothly back down the 30-degree ramp without detaching.
		allGrounded := true
		runLoop(t, 60, time.Second, func(dt time.Duration) {
			res := cc.Move(w, physics3d.Vec3{X: -2.0}, dt)
			if !res.Grounded {
				allGrounded = false
			}
		})
		if !allGrounded {
			t.Fatalf("controller detached while walking down 30-degree ramp (pos=%v)", cc.Pos)
		}
		if cc.Pos.Y >= peakPos.Y-0.5 {
			t.Fatalf("controller did not descend 30-degree ramp: startY=%v endY=%v", peakPos.Y, cc.Pos.Y)
		}
	})

	t.Run("CannotClimb60DegreeSteepSlopeAndSlidesDown", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -5, Y: -1, Z: -5}, physics3d.Vec3{X: 12, Y: 0, Z: 5})),
			physics3d.Vec3{},
		))
		const (
			angle60 = float64(60 * math.Pi / 180)
			rampR   = float32(1.0)
			rampLen = float32(6.0)
		)
		cos60 := float32(math.Cos(angle60))
		sin60 := float32(math.Sin(angle60))
		rampA := physics3d.Vec3{X: 1.5, Y: -rampR / cos60, Z: 0}
		rampB := rampA.Add(physics3d.Vec3{X: rampLen * cos60, Y: rampLen * sin60, Z: 0})
		w.AddBody(physics3d.NewStaticBody(
			physics3d.CapsuleShape(physics3d.Capsule{A: rampA, B: rampB, Radius: rampR}),
			physics3d.Vec3{},
		))

		// 1. Walking into the 60-degree slope from flat ground cannot climb it.
		cc := physics3d.NewCharacterController(physics3d.Vec3{X: 0, Y: 0, Z: 0}, 0.3, 1.8)
		hitWall := false
		runLoop(t, 60, 1500*time.Millisecond, func(dt time.Duration) {
			res := cc.Move(w, physics3d.Vec3{X: 3.0}, dt)
			if res.OnWall {
				hitWall = true
			}
		})
		if cc.Pos.Y > 0.05 {
			t.Fatalf("controller climbed 60-degree steep slope: pos=%v", cc.Pos)
		}
		if !hitWall {
			t.Fatalf("expected OnWall when pushing into 60-degree steep slope (pos=%v)", cc.Pos)
		}

		// 2. Dropped directly onto the 60-degree slope slides down toward the floor.
		// At X=3.0, surface of 60-degree slope is at Y = (3.0 - 1.5)*tan(60°) ≈ 2.60.
		slider := physics3d.NewCharacterController(physics3d.Vec3{X: 2.5, Y: 3.2, Z: 0}, 0.3, 1.8)
		runLoop(t, 60, 1500*time.Millisecond, func(dt time.Duration) {
			slider.Move(w, physics3d.Vec3{X: 1.0}, dt)
		})
		if slider.Pos.Y > 0.2 || slider.Pos.X >= 2.0 {
			t.Fatalf("controller did not slide down 60-degree slope: pos=%v grounded=%v", slider.Pos, slider.Grounded)
		}
	})

	t.Run("StopsAndSlidesCleanlyAtVerticalWall", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -5, Y: -1, Z: -5}, physics3d.Vec3{X: 10, Y: 0, Z: 10})),
			physics3d.Vec3{},
		))
		wall := w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: 2.0, Y: 0, Z: -5}, physics3d.Vec3{X: 3.0, Y: 4.0, Z: 10})),
			physics3d.Vec3{},
		))

		// Head-on into vertical wall.
		cc := physics3d.NewCharacterController(physics3d.Vec3{X: 0, Y: 0, Z: 0}, 0.3, 1.8)
		runLoop(t, 60, time.Second, func(dt time.Duration) {
			cc.Move(w, physics3d.Vec3{X: 4.0}, dt)
		})
		// Wall face is at X = 2.0, capsule radius is 0.3 => max X is 1.7.
		if !approx(cc.Pos.X, 1.7, 0.01) || !cc.OnWall || !cc.Grounded {
			t.Fatalf("head-on wall stop failed: pos=%v onWall=%v grounded=%v", cc.Pos, cc.OnWall, cc.Grounded)
		}
		if c := physics3d.TestShapes(cc.Shape(), wall.WorldShape()); c.Hit && c.Depth > 1e-3 {
			t.Fatalf("controller penetrated wall by %v", c.Depth)
		}

		// Diagonal walk into vertical wall slides cleanly along +Z without penetrating X.
		runLoop(t, 60, time.Second, func(dt time.Duration) {
			cc.Move(w, physics3d.Vec3{X: 3.0, Z: 3.0}, dt)
		})
		if !approx(cc.Pos.X, 1.7, 0.01) || cc.Pos.Z < 2.5 || !cc.OnWall {
			t.Fatalf("tangential wall slide failed: pos=%v onWall=%v", cc.Pos, cc.OnWall)
		}
		if c := physics3d.TestShapes(cc.Shape(), wall.WorldShape()); c.Hit && c.Depth > 1e-3 {
			t.Fatalf("controller penetrated wall during slide by %v", c.Depth)
		}
	})

	t.Run("JumpAndCeilingCollision", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -5, Y: -1, Z: -5}, physics3d.Vec3{X: 5, Y: 0, Z: 5})),
			physics3d.Vec3{},
		))
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: -5, Y: 2.3, Z: -5}, physics3d.Vec3{X: 5, Y: 3.3, Z: 5})),
			physics3d.Vec3{},
		))

		cc := physics3d.NewCharacterController(physics3d.Vec3{X: 0, Y: 0, Z: 0}, 0.3, 1.8)
		hitCeiling := false
		jumped := false
		runLoop(t, 60, time.Second, func(dt time.Duration) {
			wish := physics3d.Vec3{}
			if !jumped {
				wish.Y = 6.0
				jumped = true
			}
			res := cc.Move(w, wish, dt)
			if res.OnCeiling {
				hitCeiling = true
			}
			if cc.Pos.Y+cc.Height > 2.32 {
				t.Errorf("controller penetrated ceiling: top=%v", cc.Pos.Y+cc.Height)
			}
		})
		if !hitCeiling {
			t.Fatal("expected controller to hit low ceiling during jump")
		}
		if !cc.Grounded || !approx(cc.Pos.Y, 0, 0.02) {
			t.Fatalf("controller did not land back on floor: pos=%v grounded=%v", cc.Pos, cc.Grounded)
		}
	})
}

func TestBoxInertiaAndLockRotation(t *testing.T) {
	// #546: box inertia used the sphere's 0.4 coefficient (1.8× too high for
	// a cube), and InvInertia == 0 could not lock a body's rotation.
	w := physics3d.NewWorld()
	box := w.AddBody(physics3d.Body{
		Mass:  2,
		Shape: physics3d.Shape{Kind: physics3d.ShapeBox, Box: physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}},
	})
	// (1/3) m Σe² = 2; the isotropic inverse is 0.5.
	if math.Abs(float64(box.InvInertia-0.5)) > 1e-5 {
		t.Fatalf("box InvInertia = %v, want 0.5", box.InvInertia)
	}
	locked := w.AddBody(physics3d.Body{
		Mass:         2,
		LockRotation: true,
		Shape:        physics3d.Shape{Kind: physics3d.ShapeBox, Box: physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}},
	})
	if locked.InvInertia != 0 {
		t.Fatalf("locked InvInertia = %v, want 0", locked.InvInertia)
	}
	locked.ApplyAngularImpulse(physics3d.Vec3{Y: 1})
	if locked.AngVel != (physics3d.Vec3{}) {
		t.Fatalf("locked body gained angular velocity %+v", locked.AngVel)
	}
}
