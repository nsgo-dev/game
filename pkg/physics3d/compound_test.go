package physics3d_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics3d"
)

// makeTableShape builds a table made of a box tabletop (child 0) and four box
// legs (children 1..4) in local space with leg bottoms at Y = 0 and tabletop
// spanning Y in [0.80, 0.90].
func makeTableShape() physics3d.Shape {
	leg := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.06, Y: 0.4, Z: 0.06}})
	top := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.75, Y: 0.05, Z: 0.35}})
	return physics3d.CompoundShape(
		physics3d.CompoundChild{Shape: top, Offset: physics3d.Vec3{Y: 0.85}, Mass: 3},
		physics3d.CompoundChild{Shape: leg, Offset: physics3d.Vec3{X: -0.69, Y: 0.4, Z: -0.29}, Mass: 1.5},
		physics3d.CompoundChild{Shape: leg, Offset: physics3d.Vec3{X: 0.69, Y: 0.4, Z: -0.29}, Mass: 1.5},
		physics3d.CompoundChild{Shape: leg, Offset: physics3d.Vec3{X: -0.69, Y: 0.4, Z: 0.29}, Mass: 1.5},
		physics3d.CompoundChild{Shape: leg, Offset: physics3d.Vec3{X: 0.69, Y: 0.4, Z: 0.29}, Mass: 1.5},
	)
}

func TestCompoundTableStandsTipsAndRestsOnSide(t *testing.T) {
	const dt = time.Second / 60
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 0.5, Z: 20}}),
		physics3d.Vec3{Y: -0.5},
	))

	table := w.AddBody(physics3d.NewDynamicBody(makeTableShape(), physics3d.Vec3{Y: 0.02}, 0))
	table.Friction = 0.7
	table.Restitution = 0.05

	if math.Abs(float64(table.Mass-9)) > 1e-4 {
		t.Fatalf("table mass = %v, want 9 (sum of children)", table.Mass)
	}
	if table.CenterOfMass.Y <= 0.5 || table.CenterOfMass.Y >= 0.6 {
		t.Fatalf("table CenterOfMass.Y = %v, expected near 0.55", table.CenterOfMass.Y)
	}

	// Phase 1: table stands upright on its four legs and falls asleep.
	for range 180 {
		w.Step(dt)
	}
	up := table.Rot.RotateVec(physics3d.Vec3{Y: 1})
	if up.Y < 0.99 {
		t.Fatalf("standing table tilted: up = %+v", up)
	}
	if abs32f(table.Pos.Y) > 0.02 {
		t.Fatalf("standing table Pos.Y = %v, want near 0", table.Pos.Y)
	}
	if !table.Sleeping {
		t.Fatalf("standing table did not sleep (vel=%+v angVel=%+v)", table.Vel, table.AngVel)
	}

	// Phase 2: push horizontally near the tabletop along +Z (shorter dimension) so it tips onto its side.
	topWorld := table.Pos.Add(table.Rot.RotateVec(physics3d.Vec3{Y: 0.88}))
	table.ApplyImpulseAtPoint(physics3d.Vec3{Z: 12}, topWorld)
	if table.Sleeping {
		t.Fatal("ApplyImpulseAtPoint did not wake table")
	}

	for range 420 {
		w.Step(dt)
	}

	upAfter := table.Rot.RotateVec(physics3d.Vec3{Y: 1})
	if abs32f(upAfter.Y) > 0.25 {
		t.Fatalf("tipped table should rest on its side (local +Y near horizontal), got up = %+v", upAfter)
	}
	bounds := table.WorldShape().Bounds()
	if bounds.Min().Y < -0.05 || bounds.Min().Y > 0.05 {
		t.Fatalf("tipped table bounds.Min().Y = %v, want near floor 0", bounds.Min().Y)
	}
	if !table.Sleeping {
		t.Fatalf("tipped table did not settle to sleep (vel=%+v angVel=%+v)", table.Vel, table.AngVel)
	}
}

func TestCompoundMassCenterAndInertia(t *testing.T) {
	// Two equal 1x1x1 cubes (mass 2 each) at X = -2 and X = +2:
	// COM = (0, 0, 0), each box local I = (1/3)*m*(0.25+0.25) = m/6 = 1/3.
	// Parallel-axis shift along X by d = 2 adds m*d^2 = 8 to Iyy and Izz per cube.
	// Total: Ixx = 2/3, Iyy = 2/3 + 16 = 50/3, Izz = 50/3.
	cube := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	dumbbell := physics3d.CompoundShape(
		physics3d.CompoundChild{Shape: cube, Offset: physics3d.Vec3{X: -2}, Mass: 2},
		physics3d.CompoundChild{Shape: cube, Offset: physics3d.Vec3{X: 2}, Mass: 2},
		// Trigger child far away must NOT affect mass, COM, or inertia.
		physics3d.CompoundChild{Shape: physics3d.SphereShape(physics3d.Sphere{Radius: 3}), Offset: physics3d.Vec3{Y: 100}, Mass: 999, Trigger: true},
	)

	props := physics3d.ComputeCompoundMassProperties(dumbbell.Children, 0)
	if !approx32(props.Mass, 4, 1e-4) {
		t.Fatalf("Mass = %v, want 4", props.Mass)
	}
	if props.CenterOfMass.Len() > 1e-5 {
		t.Fatalf("CenterOfMass = %+v, want (0,0,0)", props.CenterOfMass)
	}
	wantIxx := float32(2.0 / 3.0)
	wantIyz := float32(50.0 / 3.0)
	if !approx32(props.Inertia.XX, wantIxx, 1e-4) || !approx32(props.Inertia.YY, wantIyz, 1e-3) || !approx32(props.Inertia.ZZ, wantIyz, 1e-3) {
		t.Fatalf("Inertia = %+v, want diag(%v, %v, %v)", props.Inertia, wantIxx, wantIyz, wantIyz)
	}
	if abs32f(props.Inertia.XY) > 1e-4 || abs32f(props.Inertia.XZ) > 1e-4 || abs32f(props.Inertia.YZ) > 1e-4 {
		t.Fatalf("off-diagonal inertia should be 0, got %+v", props.Inertia)
	}
	// Verify InvInertia * Inertia == I.
	prodX := props.InvInertia.MulVec(props.Inertia.MulVec(physics3d.Vec3{X: 1}))
	prodY := props.InvInertia.MulVec(props.Inertia.MulVec(physics3d.Vec3{Y: 1}))
	prodZ := props.InvInertia.MulVec(props.Inertia.MulVec(physics3d.Vec3{Z: 1}))
	if prodX.Sub(physics3d.Vec3{X: 1}).Len() > 1e-4 || prodY.Sub(physics3d.Vec3{Y: 1}).Len() > 1e-4 || prodZ.Sub(physics3d.Vec3{Z: 1}).Len() > 1e-4 {
		t.Fatalf("InvInertia * Inertia != I: %+v %+v %+v", prodX, prodY, prodZ)
	}

	// Density-based mass and mixed child shapes (convex hull, cylinder, capsule, rotated box).
	hull, err := physics3d.NewConvexHull([]physics3d.Vec3{
		{-0.5, -0.5, -0.5}, {0.5, -0.5, -0.5}, {0.5, 0.5, -0.5}, {-0.5, 0.5, -0.5},
		{-0.5, -0.5, 0.5}, {0.5, -0.5, 0.5}, {0.5, 0.5, 0.5}, {-0.5, 0.5, 0.5},
	}, 0)
	if err != nil {
		t.Fatalf("NewConvexHull: %v", err)
	}
	cyl := physics3d.CylinderShape(physics3d.VerticalCylinder(physics3d.Vec3{}, 0.3, 1.0))
	capS := physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.2, 0.8))
	mixed := physics3d.CompoundShape(
		physics3d.CompoundChild{Shape: physics3d.ConvexHullShape(hull), Offset: physics3d.Vec3{X: -1}, Density: 2},
		physics3d.CompoundChild{Shape: cyl, Offset: physics3d.Vec3{X: 1}, Rot: physics3d.QuatFromAxisAngle(physics3d.Vec3{Z: 1}, math.Pi/2), Density: 2},
		physics3d.CompoundChild{Shape: capS, Offset: physics3d.Vec3{Y: 1}, Density: 2},
	)
	mProps := physics3d.ComputeCompoundMassProperties(mixed.Children, 0)
	if mProps.Mass <= 0 || mProps.InvInertia.IsZero() || mProps.ScalarInvI <= 0 {
		t.Fatalf("mixed compound mass properties invalid: %+v", mProps)
	}

	// Scaling to explicit totalMass preserves relative mass ratios & COM.
	scaled := physics3d.ComputeCompoundMassProperties(mixed.Children, 20)
	if !approx32(scaled.Mass, 20, 1e-4) {
		t.Fatalf("scaled Mass = %v, want 20", scaled.Mass)
	}
	if scaled.CenterOfMass.Sub(mProps.CenterOfMass).Len() > 1e-5 {
		t.Fatalf("scaled COM %+v != density COM %+v", scaled.CenterOfMass, mProps.CenterOfMass)
	}

	// Free-space rotation of asymmetric compound conserves WorldCenterOfMass.
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}
	asym := physics3d.CompoundShape(
		physics3d.CompoundChild{Shape: cube, Offset: physics3d.Vec3{X: 0}, Mass: 1},
		physics3d.CompoundChild{Shape: cube, Offset: physics3d.Vec3{X: 3}, Mass: 3},
	)
	b := w.AddBody(physics3d.NewDynamicBody(asym, physics3d.Vec3{X: 1, Y: 2, Z: 3}, 0))
	b.LinearDamping = 0
	b.AngularDamping = 0
	b.AngVel = physics3d.Vec3{Z: 3.0}
	com0 := b.WorldCenterOfMass()
	if !approx32(b.CenterOfMass.X, 2.25, 1e-4) {
		t.Fatalf("asymmetric COM.X = %v, want 2.25", b.CenterOfMass.X)
	}
	for range 60 {
		w.Step(time.Second / 60)
	}
	com1 := b.WorldCenterOfMass()
	if com1.Sub(com0).Len() > 1e-4 {
		t.Fatalf("free-space rotation drifted WorldCenterOfMass: start=%+v end=%+v", com0, com1)
	}

	// RecomputeMassProperties updates after modifying children.
	b.Shape = physics3d.CompoundShape(
		physics3d.CompoundChild{Shape: cube, Offset: physics3d.Vec3{Y: 4}, Mass: 5},
	)
	b.Mass = 0
	b.RecomputeMassProperties()
	if !approx32(b.Mass, 5, 1e-4) || !approx32(b.CenterOfMass.Y, 4, 1e-4) {
		t.Fatalf("RecomputeMassProperties gave Mass=%v COM=%+v, want 5, (0,4,0)", b.Mass, b.CenterOfMass)
	}

	// Nested compound matches flattened mass properties.
	nested := physics3d.CompoundShape(
		physics3d.CompoundChild{
			Shape:  physics3d.CompoundShape(physics3d.CompoundChild{Shape: cube, Offset: physics3d.Vec3{X: 1}, Mass: 2}),
			Offset: physics3d.Vec3{X: 1},
		},
		physics3d.CompoundChild{Shape: cube, Offset: physics3d.Vec3{X: -2}, Mass: 2},
	)
	nProps := physics3d.ComputeCompoundMassProperties(nested.Children, 0)
	if !approx32(nProps.Mass, props.Mass, 1e-4) || nProps.CenterOfMass.Sub(props.CenterOfMass).Len() > 1e-4 ||
		!approx32(nProps.Inertia.YY, props.Inertia.YY, 1e-3) {
		t.Fatalf("nested props %+v != flat props %+v", nProps, props)
	}

	// LockRotation zeroes both InvInertia and InvInertiaTensor.
	lockedBody := physics3d.NewDynamicBody(dumbbell, physics3d.Vec3{}, 0)
	lockedBody.LockRotation = true
	lb := w.AddBody(lockedBody)
	if lb.InvInertia != 0 || !lb.InvInertiaTensor.IsZero() {
		t.Fatalf("LockRotation should zero inertia: InvInertia=%v Tensor=%+v", lb.InvInertia, lb.InvInertiaTensor)
	}
}

func TestCompoundBoundsCoversEveryChild(t *testing.T) {
	hull, err := physics3d.NewConvexHull(rockPoints(11, 24, 0.5), 16)
	if err != nil {
		t.Fatalf("NewConvexHull: %v", err)
	}
	shape := physics3d.CompoundShape(
		physics3d.CompoundChild{
			Shape:  physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.2, Z: 0.2}}),
			Offset: physics3d.Vec3{X: -2, Y: 0.5, Z: 0},
			Rot:    physics3d.QuatFromAxisAngle(physics3d.Vec3{Z: 1}, 0.6),
		},
		physics3d.CompoundChild{
			Shape:  physics3d.SphereShape(physics3d.Sphere{Radius: 0.6}),
			Offset: physics3d.Vec3{X: 2.5, Y: -1, Z: 1},
		},
		physics3d.CompoundChild{
			Shape:  physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.25, 1.4)),
			Offset: physics3d.Vec3{Y: 2.0, Z: -1.5},
			Rot:    physics3d.QuatFromAxisAngle(physics3d.Vec3{X: 1}, 0.8),
		},
		physics3d.CompoundChild{
			Shape:  physics3d.ConvexHullShape(hull),
			Offset: physics3d.Vec3{X: -1, Y: -2, Z: 2},
			Rot:    physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, 1.1),
		},
		physics3d.CompoundChild{
			Shape:  physics3d.CylinderShape(physics3d.VerticalCylinder(physics3d.Vec3{}, 0.4, 1.2)),
			Offset: physics3d.Vec3{X: 0, Y: 0, Z: 3},
			Rot:    physics3d.QuatFromAxisAngle(physics3d.Vec3{X: 1}, math.Pi/3),
		},
	)

	w := physics3d.NewWorld()
	b := w.AddBody(physics3d.NewStaticBody(shape, physics3d.Vec3{X: 3, Y: -1, Z: 2}))
	b.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{X: 1, Y: 2, Z: -1}.Norm(), 0.9)

	ws := b.WorldShape()
	total := ws.Bounds()
	tMin, tMax := total.Min(), total.Max()
	for i, child := range shape.Children {
		cb := child.WorldShape(b.Pos, b.Rot).Bounds()
		cMin, cMax := cb.Min(), cb.Max()
		if cMin.X < tMin.X-1e-4 || cMin.Y < tMin.Y-1e-4 || cMin.Z < tMin.Z-1e-4 ||
			cMax.X > tMax.X+1e-4 || cMax.Y > tMax.Y+1e-4 || cMax.Z > tMax.Z+1e-4 {
			t.Fatalf("child %d bounds [%+v, %+v] not contained in compound bounds [%+v, %+v]", i, cMin, cMax, tMin, tMax)
		}
	}
}

func TestCompoundRaycastAndOverlapChildIndex(t *testing.T) {
	const (
		layerBody    physics3d.LayerMask = 1 << 0
		layerHurtbox physics3d.LayerMask = 1 << 1
	)

	hull, err := physics3d.NewConvexHull([]physics3d.Vec3{
		{-0.3, -0.3, -0.3}, {0.3, -0.3, -0.3}, {0.3, 0.3, -0.3}, {-0.3, 0.3, -0.3},
		{-0.3, -0.3, 0.3}, {0.3, -0.3, 0.3}, {0.3, 0.3, 0.3}, {-0.3, 0.3, 0.3},
	}, 0)
	if err != nil {
		t.Fatalf("NewConvexHull: %v", err)
	}

	shape := physics3d.CompoundShape(
		physics3d.CompoundChild{
			Shape:  physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.5, Z: 0.4}}),
			Offset: physics3d.Vec3{Y: 0.5},
			Layer:  layerBody,
		},
		physics3d.CompoundChild{
			Shape:   physics3d.SphereShape(physics3d.Sphere{Radius: 0.3}),
			Offset:  physics3d.Vec3{Y: 1.5},
			Layer:   layerHurtbox,
			Trigger: true,
		},
		physics3d.CompoundChild{
			Shape:  physics3d.CylinderShape(physics3d.VerticalCylinder(physics3d.Vec3{}, 0.25, 0.8)),
			Offset: physics3d.Vec3{X: 1.5, Y: 0.5},
			Layer:  layerBody,
		},
		physics3d.CompoundChild{
			Shape:  physics3d.ConvexHullShape(hull),
			Offset: physics3d.Vec3{X: -1.5, Y: 0.5},
			Layer:  layerBody,
		},
	)

	// Standalone RaycastShape reports struck ChildIndex.
	if hit, ok := physics3d.RaycastShape(physics3d.Ray{
		Origin:  physics3d.Vec3{X: 0, Y: 1.5, Z: 5},
		Dir:     physics3d.Vec3{Z: -1},
		MaxDist: 10,
	}, shape); !ok || hit.ChildIndex != 1 {
		t.Fatalf("standalone RaycastShape head: ok=%v ChildIndex=%d, want true, 1", ok, hit.ChildIndex)
	}

	w := physics3d.NewWorld()
	comp := w.AddBody(physics3d.NewStaticBody(shape, physics3d.Vec3{X: 10}))
	plain := w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
		physics3d.Vec3{X: -10, Y: 0.5},
	))

	// Hit each child individually via World.RaycastFirst.
	cases := []struct {
		name      string
		from, to  physics3d.Vec3
		wantBody  *physics3d.Body
		wantChild int
	}{
		{"torso box", physics3d.Vec3{X: 10, Y: 0.5, Z: 5}, physics3d.Vec3{X: 10, Y: 0.5, Z: -5}, comp, 0},
		{"head sphere", physics3d.Vec3{X: 10, Y: 1.5, Z: 5}, physics3d.Vec3{X: 10, Y: 1.5, Z: -5}, comp, 1},
		{"cylinder arm", physics3d.Vec3{X: 11.5, Y: 0.5, Z: 5}, physics3d.Vec3{X: 11.5, Y: 0.5, Z: -5}, comp, 2},
		{"convex hull arm", physics3d.Vec3{X: 8.5, Y: 0.5, Z: 5}, physics3d.Vec3{X: 8.5, Y: 0.5, Z: -5}, comp, 3},
		{"plain box", physics3d.Vec3{X: -10, Y: 0.5, Z: 5}, physics3d.Vec3{X: -10, Y: 0.5, Z: -5}, plain, -1},
	}
	for _, tc := range cases {
		hit, ok := w.RaycastFirst(tc.from, tc.to)
		if !ok || hit.Body != tc.wantBody || hit.ChildIndex != tc.wantChild {
			t.Fatalf("%s: got ok=%v body=%v child=%d, want true, %v, %d", tc.name, ok, hit.Body, hit.ChildIndex, tc.wantBody, tc.wantChild)
		}
	}

	// Horizontal ray across all 3 Y=0.5 children returns all three sorted by distance (3, 0, 2).
	hits := w.Raycast(physics3d.Vec3{X: 5, Y: 0.5, Z: 0}, physics3d.Vec3{X: 15, Y: 0.5, Z: 0})
	if len(hits) != 3 || hits[0].ChildIndex != 3 || hits[1].ChildIndex != 0 || hits[2].ChildIndex != 2 {
		t.Fatalf("multi-child raycast = %+v, want child indices [3, 0, 2]", hits)
	}

	// Layer-filtered raycast sees only the hurtbox child when querying layerHurtbox.
	hurtHits := w.RaycastLayer(physics3d.Vec3{X: 10, Y: 3, Z: 0}, physics3d.Vec3{X: 10, Y: -1, Z: 0}, layerHurtbox)
	if len(hurtHits) != 1 || hurtHits[0].ChildIndex != 1 {
		t.Fatalf("RaycastLayer(layerHurtbox) = %+v, want 1 hit on child 1", hurtHits)
	}

	// OverlapLayer reports overlapping children in ascending ChildIndex order and filters by layer.
	allOverlaps := w.QueryOverlap(physics3d.BoxShape(physics3d.Box{
		Center:      physics3d.Vec3{X: 10, Y: 1.0, Z: 0},
		HalfExtents: physics3d.Vec3{X: 2.0, Y: 1.2, Z: 1.0},
	}))
	if len(allOverlaps) != 4 {
		t.Fatalf("QueryOverlap count = %d, want 4", len(allOverlaps))
	}
	for i, h := range allOverlaps {
		if h.Body != comp || h.ChildIndex != i {
			t.Fatalf("overlap[%d] = body %v child %d, want comp child %d", i, h.Body, h.ChildIndex, i)
		}
	}
	hurtOverlaps := w.OverlapLayer(physics3d.BoxShape(physics3d.Box{
		Center:      physics3d.Vec3{X: 10, Y: 1.0, Z: 0},
		HalfExtents: physics3d.Vec3{X: 2.0, Y: 1.2, Z: 1.0},
	}), layerHurtbox)
	if len(hurtOverlaps) != 1 || hurtOverlaps[0].ChildIndex != 1 {
		t.Fatalf("OverlapLayer(layerHurtbox) = %+v, want only child 1", hurtOverlaps)
	}
	plainOverlaps := w.QueryOverlap(physics3d.SphereShape(physics3d.Sphere{Center: physics3d.Vec3{X: -10, Y: 0.5}, Radius: 0.2}))
	if len(plainOverlaps) != 1 || plainOverlaps[0].Body != plain || plainOverlaps[0].ChildIndex != -1 {
		t.Fatalf("plain overlap = %+v, want plain with ChildIndex -1", plainOverlaps)
	}
}

func TestCompoundPerChildTriggerAndLayer(t *testing.T) {
	const (
		layerSolid   physics3d.LayerMask = 1 << 0
		layerHurtbox physics3d.LayerMask = 1 << 1
		layerBullet  physics3d.LayerMask = 1 << 2
	)
	const dt = time.Second / 60

	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}

	// Fighter body with solid torso (child 0) and trigger head hurtbox (child 1).
	fighterShape := physics3d.CompoundShape(
		physics3d.CompoundChild{
			Shape:  physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.5, Z: 0.4}}),
			Offset: physics3d.Vec3{Y: 0.5},
			Mass:   10,
			Layer:  layerSolid,
			Mask:   layerSolid,
		},
		physics3d.CompoundChild{
			Shape:   physics3d.SphereShape(physics3d.Sphere{Radius: 0.35}),
			Offset:  physics3d.Vec3{Y: 1.5},
			Trigger: true,
			Layer:   layerHurtbox,
			Mask:    layerBullet,
		},
	)
	fighter := w.AddBody(physics3d.NewDynamicBody(fighterShape, physics3d.Vec3{}, 0))
	fighter.LinearDamping = 0
	fighter.AngularDamping = 0

	// Bullet flies straight through the head hurtbox along +X.
	bullet := w.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}),
		physics3d.Vec3{X: -1.0, Y: 1.5},
		1.0,
	))
	bullet.LinearDamping = 0
	bullet.Layer = layerBullet
	bullet.Mask = layerHurtbox
	bullet.Vel = physics3d.Vec3{X: 6.0}

	var trigEnters, trigStays, trigExits int
	var solidContacts int
	w.OnTrigger = func(ev physics3d.ContactEvent) {
		if !ev.IsTrigger {
			t.Errorf("OnTrigger got IsTrigger=false")
		}
		// fighter.ID < bullet.ID so fighter is BodyA, bullet is BodyB.
		if ev.BodyA != fighter || ev.BodyB != bullet || ev.ChildIndexA != 1 || ev.ChildIndexB != -1 {
			t.Errorf("unexpected trigger event: A=%d(c=%d) B=%d(c=%d)", ev.BodyA.ID, ev.ChildIndexA, ev.BodyB.ID, ev.ChildIndexB)
		}
		switch ev.Kind {
		case physics3d.ContactEnter:
			trigEnters++
		case physics3d.ContactStay:
			trigStays++
		case physics3d.ContactExit:
			trigExits++
		}
	}
	w.OnContact = func(ev physics3d.ContactEvent) {
		solidContacts++
	}

	for range 40 {
		w.Step(dt)
	}

	if trigEnters != 1 || trigStays == 0 || trigExits != 1 {
		t.Fatalf("trigger lifecycle enter=%d stay=%d exit=%d, want 1, >0, 1", trigEnters, trigStays, trigExits)
	}
	if solidContacts != 0 {
		t.Fatalf("expected 0 solid contacts, got %d", solidContacts)
	}
	if fighter.Vel.Len() > 1e-5 || fighter.AngVel.Len() > 1e-5 {
		t.Fatalf("trigger child should not impart impulse to parent body: vel=%+v angVel=%+v", fighter.Vel, fighter.AngVel)
	}
	if bullet.Pos.X < 1.0 {
		t.Fatalf("bullet should pass freely through trigger child, got X=%v", bullet.Pos.X)
	}

	// CharacterController passes through the trigger child (head) when on layerHurtbox,
	// and is blocked by the solid child (torso) on layerSolid.
	cc := physics3d.NewCharacterController(physics3d.Vec3{X: -1.5, Y: 0}, 0.25, 1.0)
	cc.SetGravityScale(0)
	cc.Mask = layerSolid | layerHurtbox
	for range 30 {
		cc.Move(w, physics3d.Vec3{X: 3.0}, dt)
	}
	if !cc.OnWall || cc.Pos.X > -0.5 {
		t.Fatalf("CharacterController should be blocked by solid torso child, got Pos=%+v OnWall=%v", cc.Pos, cc.OnWall)
	}
	ccHigh := physics3d.NewCharacterController(physics3d.Vec3{X: -1.5, Y: 1.2}, 0.25, 0.6)
	ccHigh.SetGravityScale(0)
	ccHigh.Mask = layerSolid | layerHurtbox
	for range 60 {
		ccHigh.Move(w, physics3d.Vec3{X: 3.0}, dt)
	}
	if ccHigh.Pos.X < 1.0 {
		t.Fatalf("CharacterController should pass through trigger head child, got Pos=%+v", ccHigh.Pos)
	}
}

func TestCompoundDeterminismAndSaveRestore(t *testing.T) {
	const dt = time.Second / 60

	build := func() *physics3d.World {
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 15, Y: 0.5, Z: 15}}),
			physics3d.Vec3{Y: -0.5},
		))
		table := w.AddBody(physics3d.NewDynamicBody(makeTableShape(), physics3d.Vec3{Y: 0.1}, 0))
		table.Tag = "table"
		table.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{X: 1}, 0.25)

		hull, err := physics3d.NewConvexHull(rockPoints(9, 25, 0.35), 14)
		if err != nil {
			t.Fatalf("NewConvexHull: %v", err)
		}
		propShape := physics3d.CompoundShape(
			physics3d.CompoundChild{Shape: physics3d.ConvexHullShape(hull), Offset: physics3d.Vec3{X: -0.35}, Mass: 2},
			physics3d.CompoundChild{Shape: physics3d.CylinderShape(physics3d.VerticalCylinder(physics3d.Vec3{}, 0.2, 0.6)), Offset: physics3d.Vec3{X: 0.35}, Mass: 1.5},
			physics3d.CompoundChild{Shape: physics3d.SphereShape(physics3d.Sphere{Radius: 0.4}), Offset: physics3d.Vec3{Y: 0.5}, Trigger: true},
		)
		prop := w.AddBody(physics3d.NewDynamicBody(propShape, physics3d.Vec3{X: 0.1, Y: 1.8, Z: 0.05}, 0))
		prop.Tag = "prop"
		prop.AngVel = physics3d.Vec3{Y: 1.2, Z: -0.5}
		return w
	}

	// Run two independent worlds and assert identical checksums & states.
	w1 := build()
	w2 := build()
	for range 120 {
		w1.Step(dt)
		w2.Step(dt)
	}
	if w1.Checksum() != w2.Checksum() {
		t.Fatalf("non-deterministic compound simulation: %#x != %#x", w1.Checksum(), w2.Checksum())
	}

	// Save at step 60, JSON round-trip, restore both in-place and fresh, and re-step to 150.
	w := build()
	for range 60 {
		w.Step(dt)
	}
	snap := w.Save()
	snapChecksum := w.Checksum()

	// Mutating body's Children slice after Save must not corrupt snap.
	origChildOffset := w.Bodies()[1].Shape.Children[0].Offset
	w.Bodies()[1].Shape.Children[0].Offset.Y = 999
	if snap.Bodies[1].Shape.Children[0].Offset != origChildOffset {
		t.Fatal("Save aliased Body.Shape.Children slice")
	}
	w.Bodies()[1].Shape.Children[0].Offset = origChildOffset

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal(WorldSnapshot): %v", err)
	}

	for range 90 {
		w.Step(dt)
	}
	wantChecksum := w.Checksum()

	for _, mode := range []string{"in-place", "fresh"} {
		var decoded physics3d.WorldSnapshot
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s json.Unmarshal: %v", mode, err)
		}
		target := w
		if mode == "fresh" {
			target = physics3d.NewWorld()
		}
		if err := target.Restore(decoded); err != nil {
			t.Fatalf("%s Restore: %v", mode, err)
		}
		if got := target.Checksum(); got != snapChecksum {
			t.Fatalf("%s restored checksum = %#x, want %#x", mode, got, snapChecksum)
		}
		for range 90 {
			target.Step(dt)
		}
		if got := target.Checksum(); got != wantChecksum {
			t.Fatalf("%s checksum after re-step = %#x, want %#x", mode, got, wantChecksum)
		}
	}

	// JSON round-trip stability.
	var decoded physics3d.WorldSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	again, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("json.Marshal(decoded): %v", err)
	}
	if string(again) != string(raw) {
		t.Fatal("WorldSnapshot with compound shapes did not round-trip through JSON unchanged")
	}
}

func TestCompoundStaticCollidersCCDAndAreas(t *testing.T) {
	const dt = time.Second / 60

	// 1. Compound body settles on TriMesh, Heightfield, and VoxelCollider.
	{
		w := physics3d.NewWorld()
		tm := physics3d.NewTriMeshCollider([]physics3d.Vec3{
			{-5, 0, -5}, {5, 0, -5}, {5, 0, 5}, {-5, 0, 5},
		}, []uint32{0, 2, 1, 0, 3, 2})
		w.AddTriMesh(tm)

		table := w.AddBody(physics3d.NewDynamicBody(makeTableShape(), physics3d.Vec3{Y: 0.2}, 0))
		for range 120 {
			w.Step(dt)
		}
		if table.Pos.Y < -0.05 || table.Pos.Y > 0.1 {
			t.Fatalf("compound on TriMesh Pos.Y = %v, want near 0", table.Pos.Y)
		}
	}

	{
		w := physics3d.NewWorld()
		hf := physics3d.NewHeightfieldCollider(5, 5, 2.0, physics3d.Vec3{X: -4, Y: 0, Z: -4}, make([]float32, 25))
		w.AddHeightfield(hf)

		table := w.AddBody(physics3d.NewDynamicBody(makeTableShape(), physics3d.Vec3{Y: 0.2}, 0))
		for range 120 {
			w.Step(dt)
		}
		if table.Pos.Y < -0.05 || table.Pos.Y > 0.1 {
			t.Fatalf("compound on Heightfield Pos.Y = %v, want near 0", table.Pos.Y)
		}
	}

	{
		w := physics3d.NewWorld()
		vc := physics3d.NewVoxelCollider(flatVoxelGrid{size: 1.0})
		w.AddVoxelCollider(vc)

		table := w.AddBody(physics3d.NewDynamicBody(makeTableShape(), physics3d.Vec3{X: 8, Y: 0.2}, 0))
		for range 120 {
			w.Step(dt)
		}
		if table.Pos.Y < -0.05 || table.Pos.Y > 0.1 {
			t.Fatalf("compound on VoxelCollider Pos.Y = %v, want near 0", table.Pos.Y)
		}
	}

	// 2. CCD prevents a fast compound body from tunneling through a thin wall,
	//    and ignores trigger children during the sweep.
	{
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.05, Y: 5, Z: 5}}),
			physics3d.Vec3{X: 2},
		))
		fastShape := physics3d.CompoundShape(
			physics3d.CompoundChild{Shape: physics3d.SphereShape(physics3d.Sphere{Radius: 0.2}), Mass: 1},
			physics3d.CompoundChild{Shape: physics3d.SphereShape(physics3d.Sphere{Radius: 0.2}), Offset: physics3d.Vec3{X: 1.0}, Trigger: true},
		)
		fast := w.AddBody(physics3d.NewDynamicBody(fastShape, physics3d.Vec3{X: 0}, 0))
		fast.Vel = physics3d.Vec3{X: 120} // moves 2.0 units in 1/60s -> right across thin wall at X=2
		w.Step(dt)
		if fast.Pos.X > 2.0 {
			t.Fatalf("fast compound tunneled through thin wall: Pos.X = %v", fast.Pos.X)
		}
	}

	// 3. Fluid buoyancy uses the sum of solid children's volumes (ignoring trigger children).
	{
		w := physics3d.NewWorld()
		pool := physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 5, Z: 10}}),
			physics3d.Vec3{Y: -5},
		)
		pool.IsTrigger = true
		pool.Area = physics3d.AreaSettings{
			FluidDensity:     2.0,
			FluidLinearDrag:  4.0,
			FluidAngularDrag: 2.0,
		}
		w.AddBody(pool)

		// Two unit cubes (volume 1 + 1 = 2), total mass 2 -> density 1.0 < fluidDensity 2.0 -> floats!
		raftShape := physics3d.CompoundShape(
			physics3d.CompoundChild{Shape: physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), Offset: physics3d.Vec3{X: -0.6}, Mass: 1},
			physics3d.CompoundChild{Shape: physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), Offset: physics3d.Vec3{X: 0.6}, Mass: 1},
		)
		raft := w.AddBody(physics3d.NewDynamicBody(raftShape, physics3d.Vec3{Y: -0.2}, 0))
		for range 180 {
			w.Step(dt)
		}
		if raft.Pos.Y < -0.6 || raft.Pos.Y > 0.6 {
			t.Fatalf("buoyant compound raft should float near surface Y=0, got Y=%v", raft.Pos.Y)
		}
	}

	// 4. Cover generation expands static compound bodies into non-trigger children.
	{
		w := physics3d.NewWorld()
		barricade := physics3d.CompoundShape(
			physics3d.CompoundChild{Shape: physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1.5, Y: 0.9, Z: 0.25}}), Offset: physics3d.Vec3{Y: 0.9}},
			physics3d.CompoundChild{Shape: physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.25, Y: 0.9, Z: 1.5}}), Offset: physics3d.Vec3{X: 1.5, Y: 0.9, Z: 1.5}},
			physics3d.CompoundChild{Shape: physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 2, Y: 2, Z: 2}}), Offset: physics3d.Vec3{X: 10, Y: 2}, Trigger: true},
		)
		w.AddBody(physics3d.NewStaticBody(barricade, physics3d.Vec3{}))
		pts := w.GenerateCoverPoints(physics3d.CoverOptions{})
		if len(pts) == 0 {
			t.Fatal("expected cover points generated from static compound barricade")
		}
		for _, pt := range pts {
			if pt.Pos.X > 6 {
				t.Fatalf("trigger child should not generate cover points, got %+v", pt)
			}
		}
	}
}

func TestCompoundEdgeCases(t *testing.T) {
	// Empty compound is safe everywhere.
	empty := physics3d.CompoundShape()
	if empty.Kind != physics3d.ShapeCompound || len(empty.Children) != 0 {
		t.Fatalf("empty CompoundShape unexpected: %+v", empty)
	}
	if empty.Bounds() != (physics3d.Box{}) {
		t.Fatalf("empty bounds = %+v, want zero Box", empty.Bounds())
	}
	if _, ok := physics3d.RaycastShape(physics3d.Ray{Origin: physics3d.Vec3{Z: 5}, Dir: physics3d.Vec3{Z: -1}, MaxDist: 10}, empty); ok {
		t.Fatal("raycast against empty compound should miss")
	}
	if physics3d.TestShapes(empty, physics3d.SphereShape(physics3d.Sphere{Radius: 1})).Hit {
		t.Fatal("empty compound should not overlap sphere")
	}

	// CompoundShape copies caller slice so external mutation does not alias.
	children := []physics3d.CompoundChild{
		{Shape: physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), Offset: physics3d.Vec3{X: 1}},
	}
	s := physics3d.CompoundShape(children...)
	children[0].Offset.X = 99
	if s.Children[0].Offset.X != 1 {
		t.Fatal("CompoundShape aliased caller slice")
	}

	// AddBody sanitizes non-finite child offsets, rotations, mass, and density.
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	dirty := physics3d.CompoundShape(physics3d.CompoundChild{
		Shape:   physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
		Offset:  physics3d.Vec3{X: nan, Y: inf},
		Rot:     physics3d.Quat{X: nan, W: 1},
		Mass:    nan,
		Density: -5,
	})
	w := physics3d.NewWorld()
	b := w.AddBody(physics3d.NewDynamicBody(dirty, physics3d.Vec3{}, 2))
	c := b.Shape.Children[0]
	if c.Offset != (physics3d.Vec3{}) || c.Mass != 0 || c.Density != 0 || math.IsNaN(float64(c.Rot.X)) {
		t.Fatalf("dirty child was not sanitized: %+v", c)
	}

	// SymMat3 operations and singular matrix inverse.
	m := physics3d.SymMat3{XX: 2, YY: 3, ZZ: 4, XY: 0.5, XZ: -0.25, YZ: 0.75}
	inv, ok := m.Inverse()
	if !ok {
		t.Fatal("expected invertible SymMat3")
	}
	v := physics3d.Vec3{X: 1, Y: -2, Z: 3}
	back := inv.MulVec(m.MulVec(v))
	if back.Sub(v).Len() > 1e-4 {
		t.Fatalf("inv*(m*v) = %+v, want %+v", back, v)
	}
	if _, ok := (physics3d.SymMat3{}).Inverse(); ok {
		t.Fatal("zero SymMat3 should not be invertible")
	}
}

func TestCompoundShapeCast(t *testing.T) {
	const (
		layerSolid   physics3d.LayerMask = 1 << 0
		layerHurtbox physics3d.LayerMask = 1 << 1
		layerSensor  physics3d.LayerMask = 1 << 2
	)
	w := physics3d.NewWorld()

	// Target compound at X=5 with three children along X:
	// child 0: trigger sensor at local X=-2 (world X=3)
	// child 1: hurtbox sphere at local X=0 (world X=5)
	// child 2: solid box at local X=+2 (world X=7)
	targetShape := physics3d.CompoundShape(
		physics3d.CompoundChild{
			Shape:   physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
			Offset:  physics3d.Vec3{X: -2},
			Trigger: true,
			Layer:   layerSensor,
		},
		physics3d.CompoundChild{
			Shape:  physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
			Offset: physics3d.Vec3{X: 0},
			Layer:  layerHurtbox,
		},
		physics3d.CompoundChild{
			Shape:  physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
			Offset: physics3d.Vec3{X: 2},
			Layer:  layerSolid,
		},
	)
	comp := w.AddBody(physics3d.NewStaticBody(targetShape, physics3d.Vec3{X: 5}))
	plain := w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
		physics3d.Vec3{X: 10},
	))

	probe := physics3d.SphereShape(physics3d.Sphere{Radius: 0.25})
	origin := physics3d.Vec3{X: 0}
	delta := physics3d.Vec3{X: 12}

	// Default ShapeCast hits child 0 (trigger sensor) first.
	h, ok := w.ShapeCast(probe, origin, physics3d.QuatIdentity(), delta)
	if !ok || h.Body != comp || h.ChildIndex != 0 {
		t.Fatalf("ShapeCast = (%+v, %v), want comp child 0", h, ok)
	}

	// IgnoreTriggers skips child 0 and hits child 1 (hurtbox).
	hSolid, ok := w.ShapeCastWith(probe, origin, physics3d.QuatIdentity(), delta, physics3d.ShapeCastOptions{
		IgnoreTriggers: true,
	})
	if !ok || hSolid.Body != comp || hSolid.ChildIndex != 1 {
		t.Fatalf("ShapeCastWith(IgnoreTriggers) = (%+v, %v), want comp child 1", hSolid, ok)
	}

	// LayerMask filters by per-child layer.
	hSolidLayer, ok := w.ShapeCastLayer(probe, origin, physics3d.QuatIdentity(), delta, layerSolid, nil)
	if !ok || hSolidLayer.Body != comp || hSolidLayer.ChildIndex != 2 {
		t.Fatalf("ShapeCastLayer(layerSolid) = (%+v, %v), want comp child 2", hSolidLayer, ok)
	}

	// ShapeCastAll reports each matching child with ascending T and ChildIndex, plus plain with ChildIndex == -1.
	all := w.ShapeCastAll(probe, origin, physics3d.QuatIdentity(), delta)
	if len(all) != 4 {
		t.Fatalf("ShapeCastAll count = %d, want 4 (%+v)", len(all), all)
	}
	wantChildren := []int{0, 1, 2, -1}
	wantBodies := []*physics3d.Body{comp, comp, comp, plain}
	for i := range all {
		if all[i].Body != wantBodies[i] || all[i].ChildIndex != wantChildren[i] {
			t.Fatalf("ShapeCastAll[%d] = body %v child %d, want body %v child %d",
				i, all[i].Body, all[i].ChildIndex, wantBodies[i], wantChildren[i])
		}
	}

	// Sweeping a compound probe against both a compound target and static TriMesh/Heightfield/Voxel.
	movingComp := physics3d.CompoundShape(
		physics3d.CompoundChild{
			Shape:  physics3d.SphereShape(physics3d.Sphere{Radius: 0.25}),
			Offset: physics3d.Vec3{X: 0.5},
		},
		physics3d.CompoundChild{
			Shape:  physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.2, Y: 0.2, Z: 0.2}}),
			Offset: physics3d.Vec3{X: -0.5},
		},
	)
	sh, ok := physics3d.ShapeCastShape(movingComp, physics3d.Vec3{X: 10}, comp.WorldShape())
	if !ok || sh.ChildIndex != 0 || !approx32(sh.Distance, 1.75, 1e-2) {
		t.Fatalf("ShapeCastShape(compound, compound) = (%+v, %v), want child 0 dist ~1.75", sh, ok)
	}

	tm := physics3d.NewTriMeshCollider(
		[]physics3d.Vec3{{X: -5, Y: 0, Z: -5}, {X: 5, Y: 0, Z: -5}, {X: 5, Y: 0, Z: 5}, {X: -5, Y: 0, Z: 5}},
		[]uint32{0, 2, 1, 0, 3, 2},
	)
	tmHit, ok := tm.ShapeCast(movingComp.Translated(physics3d.Vec3{Y: 2}), physics3d.Vec3{Y: -4})
	if !ok || tmHit.ChildIndex != -1 || !approx32(tmHit.Distance, 1.75, 1e-2) {
		t.Fatalf("TriMesh.ShapeCast(compound) = (%+v, %v), want child -1 dist ~1.75", tmHit, ok)
	}

	hf := physics3d.NewHeightfieldCollider(3, 3, 2, physics3d.Vec3{X: -2, Y: 0, Z: -2}, make([]float32, 9))
	hfHit, ok := hf.ShapeCast(movingComp.Translated(physics3d.Vec3{Y: 2}), physics3d.Vec3{Y: -4})
	if !ok || hfHit.ChildIndex != -1 || !approx32(hfHit.Distance, 1.75, 1e-2) {
		t.Fatalf("Heightfield.ShapeCast(compound) = (%+v, %v), want child -1 dist ~1.75", hfHit, ok)
	}
}

func TestCompoundNestedTriggerIgnoredInSolidStep(t *testing.T) {
	const dt = time.Second / 60
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{Y: -9.81}

	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 0.5, Z: 10}}),
		physics3d.Vec3{Y: -0.5},
	))

	// Nested compound: child 0 is itself a compound with a solid core box (halfY=0.5)
	// and a much larger trigger aura (radius=2.0) extending well below the core.
	inner := physics3d.CompoundShape(
		physics3d.CompoundChild{
			Shape: physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
		},
		physics3d.CompoundChild{
			Shape:   physics3d.SphereShape(physics3d.Sphere{Radius: 2.0}),
			Trigger: true,
		},
	)
	outer := physics3d.CompoundShape(physics3d.CompoundChild{Shape: inner})
	b := w.AddBody(physics3d.NewDynamicBody(outer, physics3d.Vec3{Y: 3.0}, 1))

	for range 120 {
		w.Step(dt)
	}
	// If the nested trigger sphere were treated as solid, b would rest around Y=2.0;
	// because it is skipped, b rests on its solid box around Y=0.5.
	if b.Pos.Y > 0.7 || b.Pos.Y < 0.3 {
		t.Fatalf("nested-trigger body settled at Y=%.3f, want ~0.5 (nested trigger must not block solid floor)", b.Pos.Y)
	}
}

func TestCompoundSnapshotV1LegacyChildIndexDefaults(t *testing.T) {
	// Simulate a pre-compound v1 JSON snapshot where child index fields were not written.
	legacyJSON := `{
		"version": 1,
		"gravity": {"X": 0, "Y": -9.81, "Z": 0},
		"solverIterations": 8,
		"nextID": 3,
		"nextColliderID": 1,
		"bodies": [
			{
				"id": 1,
				"shape": {"Kind": 0, "Box": {"Center": {"X": 0, "Y": 0, "Z": 0}, "HalfExtents": {"X": 5, "Y": 0.5, "Z": 5}}, "Rot": {"X": 0, "Y": 0, "Z": 0, "W": 1}},
				"pos": {"X": 0, "Y": -0.5, "Z": 0},
				"rot": {"X": 0, "Y": 0, "Z": 0, "W": 1},
				"static": true,
				"layer": 1,
				"mask": 4294967295
			},
			{
				"id": 2,
				"shape": {"Kind": 1, "Sphere": {"Center": {"X": 0, "Y": 0, "Z": 0}, "Radius": 0.5}},
				"pos": {"X": 0, "Y": 0.5, "Z": 0},
				"rot": {"X": 0, "Y": 0, "Z": 0, "W": 1},
				"mass": 1,
				"invMass": 1,
				"invInertia": 10,
				"layer": 1,
				"mask": 4294967295
			}
		],
		"warmStartCache": [
			{"bodyAID": 1, "bodyBID": 2, "normal": {"X": 0, "Y": 1, "Z": 0}, "normalImpulse": 0.16, "tangentImpulse": {"X": 0, "Y": 0, "Z": 0}}
		],
		"activeContacts": [
			{"keyA": 1, "keyB": 2, "bodyAID": 1, "bodyBID": 2, "point": {"X": 0, "Y": 0, "Z": 0}, "normal": {"X": 0, "Y": 1, "Z": 0}, "depth": 0.001}
		],
		"contactEvents": [
			{"kind": 0, "bodyAID": 1, "bodyBID": 2, "point": {"X": 0, "Y": 0, "Z": 0}, "normal": {"X": 0, "Y": 1, "Z": 0}, "depth": 0.001}
		]
	}`

	var snap physics3d.WorldSnapshot
	if err := json.Unmarshal([]byte(legacyJSON), &snap); err != nil {
		t.Fatalf("json.Unmarshal legacy v1 snapshot: %v", err)
	}
	if snap.WarmStartCache[0].ChildA != -1 || snap.WarmStartCache[0].ChildB != -1 {
		t.Fatalf("WarmStartCache child defaults = (%d, %d), want (-1, -1)",
			snap.WarmStartCache[0].ChildA, snap.WarmStartCache[0].ChildB)
	}
	if snap.ActiveContacts[0].KeyChildA != -1 || snap.ActiveContacts[0].KeyChildB != -1 ||
		snap.ActiveContacts[0].ChildIndexA != -1 || snap.ActiveContacts[0].ChildIndexB != -1 {
		t.Fatalf("ActiveContacts child defaults = %+v, want all -1", snap.ActiveContacts[0])
	}
	if snap.ContactEvents[0].ChildIndexA != -1 || snap.ContactEvents[0].ChildIndexB != -1 {
		t.Fatalf("ContactEvents child defaults = (%d, %d), want (-1, -1)",
			snap.ContactEvents[0].ChildIndexA, snap.ContactEvents[0].ChildIndexB)
	}

	w := physics3d.NewWorld()
	if err := w.Restore(snap); err != nil {
		t.Fatalf("Restore legacy v1 snapshot: %v", err)
	}
	evs := w.ContactEvents()
	if len(evs) != 1 || evs[0].ChildIndexA != -1 || evs[0].ChildIndexB != -1 {
		t.Fatalf("restored ContactEvents = %+v, want ChildIndex (-1, -1)", evs)
	}
}

func abs32f(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func approx32(got, want, tol float32) bool {
	return abs32f(got-want) <= tol
}
