package physics3d_test

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics3d"
)

const step60 = time.Second / 60

func yaw45() physics3d.Quat {
	return physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, math.Pi/4)
}

func TestOrientedBoxCornersAreNotSolid(t *testing.T) {
	box := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}).Transformed(physics3d.Vec3{}, yaw45())
	// The AABB of a cube turned 45 degrees about Y reaches x = z = sqrt(2); the point (1.2, 0, 1.2) is inside that AABB but outside the cube.
	probe := physics3d.SphereShape(physics3d.Sphere{Center: physics3d.Vec3{X: 1.2, Z: 1.2}, Radius: 0.1})
	if c := physics3d.TestShapes(box, probe); c.Hit {
		t.Fatalf("sphere in the empty AABB corner hit the rotated box: %+v", c)
	}
	if c := physics3d.TestShapes(probe, box); c.Hit {
		t.Fatalf("reversed test hit: %+v", c)
	}
	// A sphere on the rotated face is hit with the face normal.
	face := physics3d.SphereShape(physics3d.Sphere{Center: physics3d.Vec3{X: 0.75, Z: 0.75}, Radius: 0.1})
	c := physics3d.TestShapes(face, box)
	if !c.Hit {
		t.Fatal("sphere touching the rotated face missed")
	}
	want := physics3d.Vec3{X: 1, Z: 1}.Norm()
	if !approxVec(c.Normal, want, 1e-4) {
		t.Fatalf("normal = %v, want %v", c.Normal, want)
	}
	if b := box.Bounds(); !approx(b.HalfExtents.X, float32(math.Sqrt2), 1e-5) {
		t.Fatalf("bounds half-extent X = %v, want sqrt(2)", b.HalfExtents.X)
	}
	if _, ok := physics3d.RaycastShape(physics3d.Ray{Origin: physics3d.Vec3{X: 1.25, Y: 5, Z: 1.25}, Dir: physics3d.Vec3{Y: -1}, MaxDist: 10}, box); ok {
		t.Fatal("ray through the empty AABB corner hit the rotated box")
	}
	hit, ok := physics3d.RaycastShape(physics3d.Ray{Origin: physics3d.Vec3{X: 3, Y: 0, Z: 0}, Dir: physics3d.Vec3{X: -1}, MaxDist: 10}, box)
	if !ok || !approx(hit.Distance, 3-float32(math.Sqrt2), 1e-4) {
		t.Fatalf("ray toward the rotated corner = %+v, %v; want distance %v", hit, ok, 3-math.Sqrt2)
	}
}

func TestOrientedBoxBoxSeparatingAxes(t *testing.T) {
	unit := physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}
	// Two cubes turned 45 degrees about Y, placed diagonally: their AABBs overlap, the cubes do not.
	a := unit
	b := physics3d.Box{Center: physics3d.Vec3{X: 1.6, Z: 1.6}, HalfExtents: unit.HalfExtents}
	if c := physics3d.TestOrientedBoxBox(a, yaw45(), b, yaw45()); c.Hit {
		t.Fatalf("diagonal rotated cubes reported overlap: %+v", c)
	}
	// Face-to-face along the rotated X axis, overlapping by 0.1.
	axis := yaw45().RotateVec(physics3d.Vec3{X: 1})
	b.Center = axis.Scale(1.9)
	c := physics3d.TestOrientedBoxBox(a, yaw45(), b, yaw45())
	if !c.Hit || !approx(c.Depth, 0.1, 1e-4) || !approxVec(c.Normal, axis.Neg(), 1e-4) {
		t.Fatalf("face contact = %+v, want depth 0.1 along %v", c, axis.Neg())
	}
	// A rotated cube resting flat on an axis-aligned floor gets a contact point under its center.
	floor := physics3d.Box{Center: physics3d.Vec3{Y: -1}, HalfExtents: physics3d.Vec3{X: 5, Y: 1, Z: 5}}
	cube := physics3d.Box{Center: physics3d.Vec3{X: 0.5, Y: 0.99, Z: -0.25}, HalfExtents: unit.HalfExtents}
	c = physics3d.TestOrientedBoxBox(cube, yaw45(), floor, physics3d.QuatIdentity())
	if !c.Hit || !approxVec(c.Normal, physics3d.Vec3{Y: 1}, 1e-5) {
		t.Fatalf("resting contact = %+v", c)
	}
	if !approx(c.Point.X, 0.5, 1e-4) || !approx(c.Point.Z, -0.25, 1e-4) {
		t.Fatalf("resting contact point = %v, want under (0.5, -0.25)", c.Point)
	}
}

func TestRotatedBodyCornerDoesNotCatchFallingSphere(t *testing.T) {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 0.5, Z: 20}}), physics3d.Vec3{Y: -0.5}))
	pillar := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 2, Z: 1}}), physics3d.Vec3{Y: 2})
	pillar.Rot = yaw45()
	w.AddBody(pillar)
	ball := w.AddBody(physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}), physics3d.Vec3{X: 1.2, Y: 6, Z: 1.2}, 1))
	for range 240 {
		w.Step(step60)
	}
	if ball.Pos.Y > 0.2 {
		t.Fatalf("ball stopped at y = %v on the pillar's empty AABB corner, want it on the floor", ball.Pos.Y)
	}
}

func TestContactFrictionMakesSphereRoll(t *testing.T) {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 50, Y: 0.5, Z: 50}}), physics3d.Vec3{Y: -0.5}))
	ball := w.AddBody(physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), physics3d.Vec3{Y: 0.5}, 1))
	ball.LinearDamping, ball.AngularDamping = 0, 0
	ball.Vel = physics3d.Vec3{X: 4}
	for range 60 {
		w.Step(step60)
	}
	// Rolling without slipping along +X spins about -Z with |ω| r = v.
	if ball.AngVel.Z >= 0 {
		t.Fatalf("ball sliding along +X spins %v, want negative Z (rolling)", ball.AngVel)
	}
	if slip := ball.Vel.X + ball.AngVel.Z*0.5; math.Abs(float64(slip)) > 0.05 {
		t.Fatalf("contact slip v + ωr = %v after 1 s, want rolling without slipping", slip)
	}
	if ball.Vel.X <= 0 {
		t.Fatalf("ball stopped (%v) instead of rolling on", ball.Vel)
	}
}

func TestBoxOnItsEdgeTipsOver(t *testing.T) {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 0.5, Z: 20}}), physics3d.Vec3{Y: -0.5}))
	tilt := physics3d.QuatFromAxisAngle(physics3d.Vec3{Z: 1}, 0.6) // past nothing: a cube on its edge tipped by 0.6 rad falls back onto a face
	box := physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), physics3d.Vec3{Y: 0.72}, 1)
	box.Rot = tilt
	b := w.AddBody(box)
	for range 300 {
		w.Step(step60)
	}
	up := b.Rot.RotateVec(physics3d.Vec3{Y: 1})
	right := b.Rot.RotateVec(physics3d.Vec3{X: 1})
	flat := max(math.Abs(float64(up.Y)), math.Abs(float64(right.Y)))
	if flat < 0.99 {
		t.Fatalf("box still tilted after 5 s (up %v, right %v), want it to tip onto a face", up, right)
	}
	if b.Pos.Y < 0.45 || b.Pos.Y > 0.6 {
		t.Fatalf("box rests at y = %v, want about 0.5", b.Pos.Y)
	}
}

func settle(w *physics3d.World, b *physics3d.Body) bool {
	for range 300 {
		w.Step(step60)
		if b.Sleeping {
			return true
		}
	}
	return false
}

func TestKinematicBodyWakesAndPushesSleepingBody(t *testing.T) {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 0.5, Z: 20}}), physics3d.Vec3{Y: -0.5}))
	box := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), physics3d.Vec3{Y: 0.5}, 1))
	if !settle(w, box) {
		t.Fatal("box never fell asleep")
	}
	pusher := w.AddBody(physics3d.Body{Shape: physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), Pos: physics3d.Vec3{X: -1.5, Y: 0.5}, Mass: 1, Kinematic: true})
	for range 60 {
		pusher.Pos.X += 0.05 // moved directly, with no velocity
		w.Step(step60)
	}
	if box.Pos.X < pusher.Pos.X+0.9 {
		t.Fatalf("kinematic pusher at x = %v passed through the box at x = %v", pusher.Pos.X, box.Pos.X)
	}
}

func TestSleepingBodyFallsWhenSupportLeaves(t *testing.T) {
	w := physics3d.NewWorld()
	platform := w.AddBody(physics3d.Body{Shape: physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 2, Y: 0.25, Z: 2}}), Pos: physics3d.Vec3{Y: -0.25}, Mass: 1, Kinematic: true})
	box := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), physics3d.Vec3{Y: 0.5}, 1))
	if !settle(w, box) {
		t.Fatal("box never fell asleep on the platform")
	}
	platform.Pos.Y = -10 // teleported away in one step
	for range 30 {
		w.Step(step60)
	}
	if box.Sleeping || box.Pos.Y > 0 {
		t.Fatalf("box hangs at y = %v (sleeping %v) after its support vanished", box.Pos.Y, box.Sleeping)
	}
}

func TestJointWakesSleepingPartner(t *testing.T) {
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}
	a := w.AddBody(physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.2}), physics3d.Vec3{}, 1))
	b := w.AddBody(physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.2}), physics3d.Vec3{X: 1}, 1))
	w.AddJoint(physics3d.NewDistanceJoint(a, b, physics3d.Vec3{}, physics3d.Vec3{}, 1, 1))
	if !settle(w, b) || !a.Sleeping {
		t.Fatal("pair never fell asleep")
	}
	a.ApplyImpulse(physics3d.Vec3{X: -3})
	for range 30 {
		w.Step(step60)
	}
	if b.Sleeping || b.Pos.X > 0.9 {
		t.Fatalf("jointed partner stayed asleep at x = %v while a moved to %v", b.Pos.X, a.Pos.X)
	}
}

func TestTriMeshBVHKeepsDeepTriangles(t *testing.T) {
	// Triangles at x = 2^i make a midpoint split peel off one triangle per level, which used to build a tree deeper than the fixed 64-entry traversal stack.
	const n = 100
	var verts []physics3d.Vec3
	var idx []uint32
	for i := range n {
		x := float32(math.Ldexp(1, i))
		w := x / 4
		base := uint32(len(verts))
		verts = append(verts, physics3d.Vec3{X: x}, physics3d.Vec3{X: x, Z: w}, physics3d.Vec3{X: x + w})
		idx = append(idx, base, base+1, base+2)
	}
	tm := physics3d.NewTriMeshCollider(verts, idx)
	visited := 0
	tm.QueryTriangles(tm.Bounds, func(physics3d.Triangle) { visited++ })
	if visited != n {
		t.Fatalf("QueryTriangles visited %d of %d triangles", visited, n)
	}
	for i := range 4 {
		x := float32(math.Ldexp(1, i))
		hit, ok := tm.Raycast(physics3d.Ray{Origin: physics3d.Vec3{X: x + x/16, Y: 1, Z: x / 16}, Dir: physics3d.Vec3{Y: -1}, MaxDist: 2})
		if !ok || !approx(hit.Distance, 1, 1e-5) {
			t.Fatalf("ray onto triangle %d = %+v, %v; want a hit at distance 1", i, hit, ok)
		}
	}
}

func TestFastSphereDoesNotTunnelThroughThinBox(t *testing.T) {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 5, Y: 0.1, Z: 5}}), physics3d.Vec3{}))
	ball := w.AddBody(physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}), physics3d.Vec3{Y: 1}, 1))
	ball.Vel = physics3d.Vec3{Y: -40}
	for range 60 {
		w.Step(step60)
	}
	if ball.Pos.Y < 0.1 {
		t.Fatalf("sphere at -40 m/s ended at y = %v, below the 0.2 m slab", ball.Pos.Y)
	}
}

func TestFastSphereDoesNotTunnelThroughTriMesh(t *testing.T) {
	w := physics3d.NewWorld()
	w.AddTriMesh(physics3d.NewTriMeshCollider([]physics3d.Vec3{{X: -5, Z: -5}, {X: -5, Z: 5}, {X: 5, Z: 5}, {X: 5, Z: -5}}, []uint32{0, 1, 2, 0, 2, 3}))
	ball := w.AddBody(physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}), physics3d.Vec3{Y: 1}, 1))
	ball.Vel = physics3d.Vec3{Y: -60}
	for range 60 {
		w.Step(step60)
	}
	if ball.Pos.Y < 0 {
		t.Fatalf("sphere at -60 m/s ended at y = %v, under the mesh", ball.Pos.Y)
	}
}

// slideOnFloor runs a frictionless body standing on a static floor at speed v along +X for one second and returns how far it travelled.
func slideOnFloor(t *testing.T, shape physics3d.Shape, restY, v float32) float32 {
	t.Helper()
	w := physics3d.NewWorld()
	floor := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 100, Y: 0.5, Z: 5}}), physics3d.Vec3{X: 90, Y: -0.5}))
	floor.Friction = 0
	body := w.AddBody(physics3d.NewDynamicBody(shape, physics3d.Vec3{Y: restY}, 1))
	body.Friction = 0
	body.Vel = physics3d.Vec3{X: v}
	for range 60 {
		w.Step(step60)
	}
	return body.Pos.X
}

func TestFastBodySlidingOnFloorIsNotHeldBack(t *testing.T) {
	sphere := physics3d.SphereShape(physics3d.Sphere{Radius: 0.5})
	box := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	for _, v := range []float32{20, 40} {
		if got := slideOnFloor(t, sphere, 0.5, v); got < 0.95*v {
			t.Errorf("sphere sliding at %v m/s travelled %v m in 1 s, want about %v", v, got, v)
		}
		if got := slideOnFloor(t, box, 0.5, v); got < 0.95*v {
			t.Errorf("box sliding at %v m/s travelled %v m in 1 s, want about %v", v, got, v)
		}
	}
}

func TestFastBodySlidingOnFloorStillStopsAtThinWall(t *testing.T) {
	w := physics3d.NewWorld()
	floor := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 100, Y: 0.5, Z: 5}}), physics3d.Vec3{X: 90, Y: -0.5}))
	floor.Friction = 0
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.05, Y: 2, Z: 5}}), physics3d.Vec3{X: 10, Y: 2}))
	ball := w.AddBody(physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.25}), physics3d.Vec3{Y: 0.25}, 1))
	ball.Friction = 0
	ball.Vel = physics3d.Vec3{X: 60}
	for range 60 {
		w.Step(step60)
	}
	if ball.Pos.X > 10 {
		t.Fatalf("sphere sliding at 60 m/s ended at x = %v, past the wall at x = 10", ball.Pos.X)
	}
}

// boxCapsuleSeparates checks the contract of Contact: moving the box by Normal*Depth (plus a hair) leaves no overlap.
func boxCapsuleSeparates(box physics3d.Box, c physics3d.Capsule) (physics3d.Contact, bool) {
	ct := physics3d.TestBoxCapsule(box, c)
	if !ct.Hit {
		return ct, true
	}
	moved := box.Translated(ct.Normal.Scale(ct.Depth + 1e-3))
	return ct, !physics3d.TestBoxCapsule(moved, c).Hit
}

func TestBoxCapsuleDeepPenetrationSeparates(t *testing.T) {
	rng := rand.New(rand.NewPCG(546, 1))
	f := func(lo, hi float64) float32 { return float32(lo + rng.Float64()*(hi-lo)) }
	failures := 0
	const trials = 2000
	for range trials {
		box := physics3d.Box{Center: physics3d.Vec3{X: f(-1, 1), Y: f(-1, 1), Z: f(-1, 1)}, HalfExtents: physics3d.Vec3{X: f(0.2, 2), Y: f(0.2, 2), Z: f(0.2, 2)}}
		mid := box.Center.Add(physics3d.Vec3{X: f(-0.5, 0.5) * box.HalfExtents.X, Y: f(-0.5, 0.5) * box.HalfExtents.Y, Z: f(-0.5, 0.5) * box.HalfExtents.Z})
		dir := physics3d.Vec3{X: f(-1, 1), Y: f(-1, 1), Z: f(-1, 1)}.Scale(f(0.1, 3))
		c := physics3d.Capsule{A: mid.Sub(dir), B: mid.Add(dir), Radius: f(0.05, 0.8)}
		if ct, ok := boxCapsuleSeparates(box, c); !ok {
			failures++
			if failures <= 3 {
				t.Errorf("box %+v capsule %+v: contact %+v does not separate", box, c, ct)
			}
		}
	}
	if failures > 0 {
		t.Fatalf("%d of %d deep box-capsule contacts do not separate", failures, trials)
	}
}

func FuzzBoxCapsuleSeparation(f *testing.F) {
	f.Add(float32(0), float32(0), float32(0), float32(1), float32(1), float32(1), float32(-2), float32(0.1), float32(0), float32(2), float32(-0.1), float32(0.3), float32(0.4))
	f.Add(float32(0.5), float32(-0.2), float32(0), float32(0.3), float32(2), float32(0.5), float32(0.5), float32(-3), float32(0.1), float32(0.4), float32(3), float32(0), float32(0.2))
	f.Fuzz(func(t *testing.T, cx, cy, cz, hx, hy, hz, ax, ay, az, bx, by, bz, r float32) {
		for _, v := range []float32{cx, cy, cz, hx, hy, hz, ax, ay, az, bx, by, bz, r} {
			if math.IsNaN(float64(v)) || math.Abs(float64(v)) > 100 {
				t.Skip()
			}
		}
		if hx < 0.05 || hy < 0.05 || hz < 0.05 || r < 0.01 {
			t.Skip()
		}
		box := physics3d.Box{Center: physics3d.Vec3{X: cx, Y: cy, Z: cz}, HalfExtents: physics3d.Vec3{X: hx, Y: hy, Z: hz}}
		c := physics3d.Capsule{A: physics3d.Vec3{X: ax, Y: ay, Z: az}, B: physics3d.Vec3{X: bx, Y: by, Z: bz}, Radius: r}
		// The separation contract is exact when the segment passes through the box; shallow touches keep the closest-point sphere.
		ct := physics3d.TestBoxCapsule(box, c)
		if !ct.Hit {
			return
		}
		if !(isFiniteVec(ct.Normal) && ct.Depth > 0) {
			t.Fatalf("contact %+v is not finite and positive", ct)
		}
		if _, ok := boxCapsuleSeparates(box, c); !ok && segmentEntersBox(box, c) {
			t.Fatalf("box %+v capsule %+v: contact %+v does not separate", box, c, ct)
		}
	})
}

func segmentEntersBox(b physics3d.Box, c physics3d.Capsule) bool {
	_, ok := physics3d.RaycastBox(physics3d.Ray{Origin: c.A, Dir: c.B.Sub(c.A), MaxDist: c.B.Sub(c.A).Len()}, b)
	inside := func(p physics3d.Vec3) bool {
		mn, mx := b.Min(), b.Max()
		return p.X >= mn.X && p.X <= mx.X && p.Y >= mn.Y && p.Y <= mx.Y && p.Z >= mn.Z && p.Z <= mx.Z
	}
	return ok || inside(c.A)
}

func TestCoincidentCapsuleContactsUsePerpendicularNormals(t *testing.T) {
	// Two crossing capsules whose axes meet: the normal must be perpendicular to both axes (here ±Y), not an arbitrary axis.
	a := physics3d.Capsule{A: physics3d.Vec3{X: -1}, B: physics3d.Vec3{X: 1}, Radius: 0.2}
	b := physics3d.Capsule{A: physics3d.Vec3{Z: -1}, B: physics3d.Vec3{Z: 1}, Radius: 0.2}
	c := physics3d.TestCapsuleCapsule(a, b)
	if !c.Hit || math.Abs(float64(c.Normal.Y)) < 0.999 {
		t.Fatalf("crossing capsules normal = %+v, want ±Y", c)
	}
	// A sphere centered on a vertical capsule's axis is pushed out sideways, perpendicular to the axis.
	vert := physics3d.Capsule{A: physics3d.Vec3{Y: -1}, B: physics3d.Vec3{Y: 1}, Radius: 0.3}
	c = physics3d.TestSphereCapsule(physics3d.Sphere{Radius: 0.2}, vert)
	if !c.Hit || math.Abs(float64(c.Normal.Y)) > 1e-6 {
		t.Fatalf("sphere on vertical capsule axis normal = %+v, want perpendicular to Y", c)
	}
}

func TestAddBodySanitizesNonFiniteAndKeepsMasslessKinematic(t *testing.T) {
	w := physics3d.NewWorld()
	nan := float32(math.NaN())
	b := w.AddBody(physics3d.Body{Shape: physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), Mass: 1, Vel: physics3d.Vec3{X: nan}, AngVel: physics3d.Vec3{Y: float32(math.Inf(1))}})
	if !isFiniteVec(b.Vel) || !isFiniteVec(b.AngVel) {
		t.Fatalf("non-finite velocities were kept: %v %v", b.Vel, b.AngVel)
	}
	k := w.AddBody(physics3d.Body{Shape: physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), Pos: physics3d.Vec3{X: 5}, Kinematic: true})
	if k.Static || !k.Kinematic || k.InvMass != 0 {
		t.Fatalf("massless kinematic body became %+v", k)
	}
	for range 10 {
		w.Step(step60)
	}
	if !isFiniteVec(b.Pos) {
		t.Fatalf("body position went non-finite: %v", b.Pos)
	}
}

func TestHingeAngleContinuesPastPi(t *testing.T) {
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}
	frame := w.AddBody(physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}), physics3d.Vec3{}))
	wheel := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.05, Z: 0.05}}), physics3d.Vec3{X: 0.5}, 1))
	hinge := physics3d.NewHingeJoint(frame, wheel, physics3d.Vec3{}, physics3d.Vec3{X: -0.5}, physics3d.Vec3{Y: 1})
	hinge.CollideConnected = false
	hinge.EnableLimits = true
	hinge.MinAngle = 0
	hinge.MaxAngle = 3 * math.Pi
	hinge.MotorTargetVel = 3
	hinge.MotorMaxTorque = 50
	w.AddJoint(hinge)
	prev := hinge.Angle()
	for range 240 {
		w.Step(step60)
		a := hinge.Angle()
		if a < prev-0.2 {
			t.Fatalf("hinge angle jumped back from %v to %v", prev, a)
		}
		prev = a
	}
	if prev < 2*math.Pi {
		t.Fatalf("motor turning for 4 s at 3 rad/s reached only %v rad", prev)
	}
	if prev > 3*math.Pi+0.1 {
		t.Fatalf("hinge passed its 3*pi limit: %v rad", prev)
	}
}

func TestRaycastFromInsideFacesTheRay(t *testing.T) {
	ray := physics3d.Ray{Origin: physics3d.Vec3{X: 0.2, Y: 0.1}, Dir: physics3d.Vec3{X: 1}, MaxDist: 5}
	want := physics3d.Vec3{X: -1}
	shapes := map[string]physics3d.Shape{
		"box":     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}),
		"sphere":  physics3d.SphereShape(physics3d.Sphere{Radius: 1}),
		"capsule": physics3d.CapsuleShape(physics3d.Capsule{A: physics3d.Vec3{Y: -1}, B: physics3d.Vec3{Y: 1}, Radius: 1}),
	}
	for name, s := range shapes {
		h, ok := physics3d.RaycastShape(ray, s)
		if !ok || h.T != 0 || h.Normal != want {
			t.Fatalf("%s: inside hit = %+v, %v; want T 0 and normal %v", name, h, ok, want)
		}
	}
}

func TestCharacterPushesDynamicBody(t *testing.T) {
	newWorld := func() (*physics3d.World, *physics3d.Body) {
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 0.5, Z: 20}}), physics3d.Vec3{Y: -0.5}))
		crate := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), physics3d.Vec3{X: 1.5, Y: 0.5}, 5))
		return w, crate
	}
	run := func(push float32) (crateX, charX float32) {
		w, crate := newWorld()
		cc := physics3d.NewCharacterController(physics3d.Vec3{}, 0.3, 1.8)
		cc.StepHeight = 0.2
		cc.PushForce = push
		for range 120 {
			cc.Move(w, physics3d.Vec3{X: 2}, step60)
			w.Step(step60)
		}
		return crate.Pos.X, cc.Pos.X
	}
	blockedCrate, blockedChar := run(0)
	if blockedCrate > 1.55 || blockedChar > 0.75 {
		t.Fatalf("without PushForce the crate moved to %v and the character reached %v", blockedCrate, blockedChar)
	}
	pushedCrate, pushedChar := run(400)
	if pushedCrate < 2 || pushedChar < blockedChar+0.4 {
		t.Fatalf("with PushForce the crate reached %v and the character %v (blocked: %v)", pushedCrate, pushedChar, blockedChar)
	}
}

func TestCharacterSteppingOntoLowCrateDoesNotShoveIt(t *testing.T) {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 0.5, Z: 20}}), physics3d.Vec3{Y: -0.5}))
	crate := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 0.1, Z: 1}}), physics3d.Vec3{X: 1.6, Y: 0.1}, 5))
	for range 30 {
		w.Step(step60)
	}
	startX := crate.Pos.X
	cc := physics3d.NewCharacterController(physics3d.Vec3{}, 0.3, 1.8)
	cc.StepHeight = 0.3
	cc.PushForce = 400
	stepped := false
	for range 40 {
		res := cc.Move(w, physics3d.Vec3{X: 2}, step60)
		stepped = stepped || res.SteppedUp
		w.Step(step60)
	}
	if !stepped {
		t.Fatalf("character never stepped onto the 0.2 m crate; it reached %v", cc.Pos)
	}
	if d := crate.Pos.X - startX; d > 0.05 {
		t.Fatalf("stepping onto the crate shoved it %v m", d)
	}
}
