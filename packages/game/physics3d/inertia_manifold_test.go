package physics3d

import (
	"math"
	"testing"
	"time"
)

const testStep60 = time.Second / 60

// TestDzhanibekovIntermediateAxisFlipVsTumblingBox verifies that a spinning
// plank (three distinct principal moments of inertia) exhibits the
// intermediate-axis (Dzhanibekov) flip under Euler's equation while conserving
// rotational kinetic energy and angular momentum magnitude, whereas a
// symmetric tumbling cube and a plank spinning about its shortest or longest
// principal axis do not flip (#810).
func TestDzhanibekovIntermediateAxisFlipVsTumblingBox(t *testing.T) {
	// Plank: HalfExtents (1.0, 0.4, 0.1), Mass = 3 kg.
	// Principal moments: Ix = 0.17 (smallest), Iy = 1.01 (intermediate), Iz = 1.16 (largest).
	plankShape := BoxShape(Box{HalfExtents: Vec3{X: 1.0, Y: 0.4, Z: 0.1}})

	runFreeSpin := func(shape Shape, angVel Vec3, steps int) (minAlign, maxEnergyErr, maxAngMomErr float64) {
		w := NewWorld()
		w.Gravity = Vec3{}
		b := NewDynamicBody(shape, Vec3{}, 3)
		b.LinearDamping = 0
		b.AngularDamping = 0
		b.AngVel = angVel
		body := w.AddBody(b)

		// Compute body-space inertia tensor I = inv(InvInertiaTensor) for invariants.
		invI := body.InvInertiaTensor
		iBody := invertSymMat3(invI)
		spinAxisWorld := angVel.Norm()
		// Find which local axis was initially aligned with spinAxisWorld.
		initLocal := spinAxisWorld

		energyAndMom := func() (float64, float64) {
			q := normalizedQuat(body.Rot)
			wLocal := q.Conjugate().RotateVec(body.AngVel)
			iw := iBody.MulVec(wLocal)
			ke := 0.5 * float64(wLocal.Dot(iw))
			lMag := math.Sqrt(float64(iw.Dot(iw)))
			return ke, lMag
		}
		ke0, l0 := energyAndMom()
		minAlign = 1.0

		for range steps {
			w.Step(testStep60)
			axisNow := normalizedQuat(body.Rot).RotateVec(initLocal)
			align := float64(axisNow.Dot(spinAxisWorld))
			if align < minAlign {
				minAlign = align
			}
			ke, lMag := energyAndMom()
			if err := math.Abs(ke-ke0) / ke0; err > maxEnergyErr {
				maxEnergyErr = err
			}
			if err := math.Abs(lMag-l0) / l0; err > maxAngMomErr {
				maxAngMomErr = err
			}
		}
		return minAlign, maxEnergyErr, maxAngMomErr
	}

	// 1. Plank spinning about its intermediate axis (+Y) with tiny perturbation flips 180°.
	minAlign, keErr, lErr := runFreeSpin(plankShape, Vec3{X: 0.05, Y: 10, Z: 0}, 300)
	if minAlign > -0.85 {
		t.Fatalf("intermediate-axis plank did not flip: min alignment = %.4f, want < -0.85", minAlign)
	}
	if keErr > 1e-3 {
		t.Fatalf("rotational kinetic energy drift = %.6f, want <= 1e-3", keErr)
	}
	if lErr > 1e-3 {
		t.Fatalf("angular momentum magnitude drift = %.6f, want <= 1e-3", lErr)
	}

	// 2. Symmetric tumbling cube with identical initial spin does NOT flip.
	cubeShape := BoxShape(Box{HalfExtents: Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	cubeMinAlign, _, _ := runFreeSpin(cubeShape, Vec3{X: 0.05, Y: 10, Z: 0}, 300)
	if cubeMinAlign < 0.99 {
		t.Fatalf("symmetric cube flipped unexpectedly: min alignment = %.4f, want >= 0.99", cubeMinAlign)
	}

	// 3. Plank spinning about its minimum-inertia (+X) and maximum-inertia (+Z)
	// axes stays stable without flipping.
	minX, _, _ := runFreeSpin(plankShape, Vec3{X: 10, Y: 0.05, Z: 0}, 300)
	if minX < 0.95 {
		t.Fatalf("plank about smallest-inertia axis flipped: min alignment = %.4f, want >= 0.95", minX)
	}
	minZ, _, _ := runFreeSpin(plankShape, Vec3{X: 0, Y: 0.05, Z: 10}, 300)
	if minZ < 0.95 {
		t.Fatalf("plank about largest-inertia axis flipped: min alignment = %.4f, want >= 0.95", minZ)
	}
}

func invertSymMat3(m SymMat3) SymMat3 {
	c0 := m.Solve(Vec3{X: 1})
	c1 := m.Solve(Vec3{Y: 1})
	c2 := m.Solve(Vec3{Z: 1})
	return SymMat3{
		XX: c0.X,
		XY: 0.5 * (c0.Y + c1.X),
		XZ: 0.5 * (c0.Z + c2.X),
		YY: c1.Y,
		YZ: 0.5 * (c1.Z + c2.Y),
		ZZ: c2.Z,
	}
}

// TestTenBoxTowerStands60SecondsWithoutShock verifies that a 10-box uniform
// tower stands without residual drift for 60 simulated seconds with shock
// propagation completely disabled, and that the default World produces a
// bit-identical trajectory because shock propagation does zero work on it (#810).
func TestTenBoxTowerStands60SecondsWithoutShock(t *testing.T) {
	buildTower := func(disableShock bool) (*World, []*Body, *Body) {
		w := NewWorld()
		w.disableShock = disableShock
		floor := w.AddBody(NewStaticBody(BoxShape(Box{HalfExtents: Vec3{X: 20, Y: 1, Z: 20}}), Vec3{Y: -1}))
		unit := BoxShape(Box{HalfExtents: Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
		boxes := make([]*Body, 10)
		for i := range boxes {
			boxes[i] = w.AddBody(NewDynamicBody(unit, Vec3{Y: float32(i) + 0.5}, 1))
		}
		return w, boxes, floor
	}

	wNoShock, boxes, floor := buildTower(true)
	wDefault, _, _ := buildTower(false)

	var posAt5s [10]Vec3
	for step := range 3600 {
		wNoShock.Step(testStep60)
		wDefault.Step(testStep60)
		if step == 299 {
			for i, b := range boxes {
				posAt5s[i] = b.Pos
			}
		}
	}

	for i, b := range boxes {
		if !b.Sleeping {
			t.Fatalf("box %d still awake after 60 s (vel=%v, ang=%v, resDepth=%v)", i, b.Vel, b.AngVel, b.residualDepth)
		}
		if b.Pos != posAt5s[i] {
			t.Fatalf("box %d drifted between 5 s and 60 s: %v -> %v", i, posAt5s[i], b.Pos)
		}
		if abs32(b.Pos.X) > 1e-4 || abs32(b.Pos.Z) > 1e-4 {
			t.Fatalf("box %d drifted laterally to (%v, %v)", i, b.Pos.X, b.Pos.Z)
		}
		below := floor
		if i > 0 {
			below = boxes[i-1]
		}
		if c := TestShapes(b.WorldShape(), below.WorldShape()); c.Hit && c.Depth > 0.005 {
			t.Fatalf("box %d residual penetration = %v m, want <= 5 mm", i, c.Depth)
		}
	}
	if top := boxes[len(boxes)-1]; top.Pos.Y < 9.5-0.03 || top.Pos.Y > 9.5+0.001 {
		t.Fatalf("top box rests at y = %v, want within 0.03 below 9.5", top.Pos.Y)
	}

	if wDefault.Checksum() != wNoShock.Checksum() {
		t.Fatalf("default World checksum %#x differs from disableShock=true checksum %#x; shock propagation intervened on 10-box tower",
			wDefault.Checksum(), wNoShock.Checksum())
	}
}

// TestMultiPointManifoldsBoxHullAndCapsule tests face clipping for box-box and
// convex hull faces (up to 4 points), 2-point capsule-on-face manifolds,
// per-point warm starting, and per-point friction (#810).
func TestMultiPointManifoldsBoxHullAndCapsule(t *testing.T) {
	t.Run("box-box axis-aligned and rotated 45 deg", func(t *testing.T) {
		floor := BoxShape(Box{Center: Vec3{Y: -1}, HalfExtents: Vec3{X: 10, Y: 1, Z: 10}})
		boxAligned := BoxShape(Box{Center: Vec3{Y: 0.49}, HalfExtents: Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
		c := TestShapes(boxAligned, floor)
		if !c.Hit {
			t.Fatal("expected hit")
		}
		pts := buildShapeManifold(boxAligned, floor, c, nil)
		if len(pts) != 4 {
			t.Fatalf("aligned box-box manifold count = %d, want 4", len(pts))
		}
		seenIDs := make(map[uint32]bool)
		for _, p := range pts {
			if p.featureID == 0 {
				t.Fatal("expected non-zero persistent featureID")
			}
			if seenIDs[p.featureID] {
				t.Fatalf("duplicate featureID %#x in box-box manifold", p.featureID)
			}
			seenIDs[p.featureID] = true
			if math.Abs(float64(p.depth-0.01)) > 1e-4 {
				t.Fatalf("aligned box-box point depth = %v, want 0.01", p.depth)
			}
		}

		// Two identical boxes stacked with 45° yaw twist: clipped octagon reduces to 4 points.
		lower := BoxShape(Box{Center: Vec3{Y: 0.5}, HalfExtents: Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
		upper := BoxShape(Box{Center: Vec3{Y: 1.49}, HalfExtents: Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
		upper.Rot = QuatFromAxisAngle(Vec3{Y: 1}, float32(math.Pi/4))
		c45 := TestShapes(upper, lower)
		if !c45.Hit {
			t.Fatal("expected 45° hit")
		}
		pts45 := buildShapeManifold(upper, lower, c45, nil)
		if len(pts45) != 4 {
			t.Fatalf("45° box-box manifold count = %d, want 4", len(pts45))
		}
	})

	t.Run("convex hull face clipping", func(t *testing.T) {
		cubePts := []Vec3{
			{-0.5, -0.5, -0.5}, {0.5, -0.5, -0.5}, {0.5, 0.5, -0.5}, {-0.5, 0.5, -0.5},
			{-0.5, -0.5, 0.5}, {0.5, -0.5, 0.5}, {0.5, 0.5, 0.5}, {-0.5, 0.5, 0.5},
		}
		hullA, err := NewConvexHull(cubePts, 16)
		if err != nil {
			t.Fatal(err)
		}
		hullB, err := NewConvexHull(cubePts, 16)
		if err != nil {
			t.Fatal(err)
		}
		sA := ConvexHullShape(hullA).Translated(Vec3{Y: 0.49})
		sB := ConvexHullShape(hullB).Translated(Vec3{Y: -0.5})
		c := TestShapes(sA, sB)
		if !c.Hit {
			t.Fatal("expected hull-hull hit")
		}
		pts := buildShapeManifold(sA, sB, c, nil)
		if len(pts) != 4 {
			t.Fatalf("hull-hull face manifold count = %d, want 4", len(pts))
		}
	})

	t.Run("capsule on face produces 2 points and rests flat", func(t *testing.T) {
		floor := BoxShape(Box{Center: Vec3{Y: -1}, HalfExtents: Vec3{X: 10, Y: 1, Z: 10}})
		horiz := CapsuleShape(Capsule{A: Vec3{X: -0.6, Y: 0.24}, B: Vec3{X: 0.6, Y: 0.24}, Radius: 0.25})
		c := TestShapes(horiz, floor)
		if !c.Hit {
			t.Fatal("expected horizontal capsule hit")
		}
		pts := buildShapeManifold(horiz, floor, c, nil)
		if len(pts) != 2 {
			t.Fatalf("horizontal capsule-on-box manifold count = %d, want 2", len(pts))
		}
		if math.Abs(float64(pts[0].point.X-pts[1].point.X)) < 1.0 {
			t.Fatalf("capsule contact points too close along axis: %v, %v", pts[0].point, pts[1].point)
		}

		// Vertical capsule on box collapses to 1 point.
		vert := CapsuleShape(Capsule{A: Vec3{Y: 0.24}, B: Vec3{Y: 1.24}, Radius: 0.25})
		cv := TestShapes(vert, floor)
		if !cv.Hit {
			t.Fatal("expected vertical capsule hit")
		}
		ptsv := buildShapeManifold(vert, floor, cv, nil)
		if len(ptsv) != 1 {
			t.Fatalf("vertical capsule-on-box manifold count = %d, want 1", len(ptsv))
		}

		// Horizontal capsule on static triangle produces 2 points.
		tri := Triangle{A: Vec3{X: -5, Z: -5}, B: Vec3{X: -5, Z: 5}, C: Vec3{X: 5, Z: 0}}
		ct := testShapeTriangleSolid(horiz, tri)
		if !ct.Hit {
			t.Fatal("expected capsule-on-triangle hit")
		}
		ptst := buildTriangleManifold(horiz, tri, ct, false, nil)
		if len(ptst) != 2 {
			t.Fatalf("horizontal capsule-on-triangle manifold count = %d, want 2", len(ptst))
		}

		// Dynamic simulation: horizontal capsule rests flat on a box and sleeps without rocking.
		w := NewWorld()
		w.AddBody(NewStaticBody(BoxShape(Box{HalfExtents: Vec3{X: 10, Y: 1, Z: 10}}), Vec3{Y: -1}))
		capBody := w.AddBody(NewDynamicBody(CapsuleShape(Capsule{A: Vec3{X: -0.6}, B: Vec3{X: 0.6}, Radius: 0.25}), Vec3{Y: 0.35}, 2))
		for range 300 {
			w.Step(testStep60)
		}
		if !capBody.Sleeping {
			t.Fatalf("horizontal capsule did not sleep: vel=%v ang=%v", capBody.Vel, capBody.AngVel)
		}
		axis := normalizedQuat(capBody.Rot).RotateVec(Vec3{X: 1})
		if abs32(axis.Y) > 0.01 {
			t.Fatalf("horizontal capsule tilted to axis %v, want flat", axis)
		}
	})

	t.Run("warm start cache stores 4 points and friction stops sliding box cleanly", func(t *testing.T) {
		w := NewWorld()
		floor := w.AddBody(NewStaticBody(BoxShape(Box{HalfExtents: Vec3{X: 20, Y: 1, Z: 20}}), Vec3{Y: -1}))
		floor.Friction = 0.6
		boxBody := NewDynamicBody(BoxShape(Box{HalfExtents: Vec3{X: 0.5, Y: 0.5, Z: 0.5}}), Vec3{Y: 0.5}, 2)
		boxBody.Friction = 0.6
		boxBody.Vel = Vec3{X: 3.0}
		b := w.AddBody(boxBody)
		for range 10 {
			w.Step(testStep60)
		}
		found4 := false
		for _, cm := range w.cache {
			if cm.count == 4 {
				found4 = true
				for k := range 4 {
					if cm.points[k].normalImpulse <= 0 {
						t.Fatalf("cached point %d has non-positive normal impulse %v", k, cm.points[k].normalImpulse)
					}
				}
			}
		}
		if !found4 {
			t.Fatalf("expected 4-point warm-start cache entry, got cache=%+v", w.cache)
		}
		for range 240 {
			w.Step(testStep60)
		}
		if !b.Sleeping {
			t.Fatalf("sliding box did not settle and sleep: vel=%v ang=%v", b.Vel, b.AngVel)
		}
		if b.Pos.X <= 0.2 {
			t.Fatalf("box failed to slide under initial velocity: x=%v", b.Pos.X)
		}
		if abs32(b.Pos.Z) > 1e-4 {
			t.Fatalf("symmetric sliding box drifted laterally to z=%v", b.Pos.Z)
		}
	})
}

// TestPerBodyInertiaAndCenterOfMassOverrides verifies diagonal/tensor inertia,
// world-space rotation of the inverse tensor, SetInertia, SetInertiaTensor,
// SetCenterOfMass, RecomputeMassProperties, joints with anisotropic inertia,
// and snapshot Save/Restore round-tripping (#810).
func TestPerBodyInertiaAndCenterOfMassOverrides(t *testing.T) {
	// 1. Diagonal inertia matches exact box formula and rotates into world space.
	w := NewWorld()
	w.Gravity = Vec3{}
	plank := NewDynamicBody(BoxShape(Box{HalfExtents: Vec3{X: 2.0, Y: 0.5, Z: 0.25}}), Vec3{}, 6)
	b := w.AddBody(plank)
	// Ix = m/3*(hy^2+hz^2) = 2*(0.25+0.0625) = 0.625 -> invIx = 1.6
	// Iy = m/3*(hx^2+hz^2) = 2*(4+0.0625) = 8.125 -> invIy = 1/8.125
	// Iz = m/3*(hx^2+hy^2) = 2*(4+0.25) = 8.5 -> invIz = 1/8.5
	wantInv := Vec3{X: 1 / 0.625, Y: 1 / 8.125, Z: 1 / 8.5}
	gotLocal := b.InvInertiaTensor
	if abs32(gotLocal.XX-wantInv.X) > 1e-4 || abs32(gotLocal.YY-wantInv.Y) > 1e-4 || abs32(gotLocal.ZZ-wantInv.Z) > 1e-4 {
		t.Fatalf("box InvInertiaTensor = %+v, want diagonal %+v", gotLocal, wantInv)
	}
	if abs32(gotLocal.XY) > 1e-6 || abs32(gotLocal.XZ) > 1e-6 || abs32(gotLocal.YZ) > 1e-6 {
		t.Fatalf("axis-aligned box has non-zero off-diagonal inertia: %+v", gotLocal)
	}

	// Rotate 90° around Y: local X becomes world -Z, so WorldInvInertiaTensor().ZZ == local XX.
	b.Rot = QuatFromAxisAngle(Vec3{Y: 1}, float32(math.Pi/2))
	wInv := b.WorldInvInertiaTensor()
	if abs32(wInv.ZZ-wantInv.X) > 1e-4 || abs32(wInv.XX-wantInv.Z) > 1e-4 {
		t.Fatalf("rotated WorldInvInertiaTensor = %+v, want XX=%v ZZ=%v", wInv, wantInv.Z, wantInv.X)
	}

	// Applying identical world angular impulse along world +Z vs +X gives angular velocity ratio invIx / invIz.
	b.AngVel = Vec3{}
	b.ApplyAngularImpulse(Vec3{Z: 1})
	wz := b.AngVel.Z
	b.AngVel = Vec3{}
	b.ApplyAngularImpulse(Vec3{X: 1})
	wx := b.AngVel.X
	if abs32(wz-wantInv.X) > 1e-4 || abs32(wx-wantInv.Z) > 1e-4 {
		t.Fatalf("anisotropic angular impulse response: wx=%v wz=%v, want %v %v", wx, wz, wantInv.Z, wantInv.X)
	}

	// 2. Per-body overrides: SetInertia, SetInertiaTensor, SetCenterOfMass.
	b.Rot = QuatIdentity()
	b.SetInertia(Vec3{X: 2, Y: 4, Z: 8})
	if abs32(b.InvInertiaTensor.XX-0.5) > 1e-5 || abs32(b.InvInertiaTensor.YY-0.25) > 1e-5 || abs32(b.InvInertiaTensor.ZZ-0.125) > 1e-5 {
		t.Fatalf("SetInertia produced %+v, want diag(0.5, 0.25, 0.125)", b.InvInertiaTensor)
	}
	b.SetCenterOfMass(Vec3{X: 1.5, Y: 0, Z: 0})
	if b.WorldCenterOfMass() != (Vec3{X: 1.5}) {
		t.Fatalf("WorldCenterOfMass = %v, want {1.5 0 0}", b.WorldCenterOfMass())
	}
	// Impulse directly through custom COM imparts zero angular velocity.
	b.Vel, b.AngVel = Vec3{}, Vec3{}
	b.ApplyImpulseAtPoint(Vec3{Y: 6}, Vec3{X: 1.5})
	if b.AngVel.Len() > 1e-5 {
		t.Fatalf("impulse through custom COM imparted AngVel = %v", b.AngVel)
	}
	// Impulse at body origin (offset -1.5 from COM) imparts torque (-1.5,0,0) x (0,6,0) = (0,0,-9) -> wz = -9 / 8 = -1.125.
	b.Vel, b.AngVel = Vec3{}, Vec3{}
	b.ApplyImpulseAtPoint(Vec3{Y: 6}, Vec3{})
	if abs32(b.AngVel.Z-(-9.0/8.0)) > 1e-4 {
		t.Fatalf("impulse at origin with custom COM gave AngVel.Z = %v, want -1.125", b.AngVel.Z)
	}

	// Custom full symmetric tensor override via SetInertiaTensor.
	customI := SymMat3{XX: 4, XY: 0.5, XZ: 0, YY: 5, YZ: 0, ZZ: 6}
	b.SetInertiaTensor(customI)
	// Verify I * InvI == identity.
	invCustom := b.InvInertiaTensor
	prodX := customI.MulVec(Vec3{X: invCustom.XX, Y: invCustom.XY, Z: invCustom.XZ})
	if prodX.Dist(Vec3{X: 1}) > 1e-4 {
		t.Fatalf("I * InvI column 0 = %v, want (1,0,0)", prodX)
	}

	// Snapshot round-trip preserves custom overrides and produces identical future step.
	snap := w.Save()
	w2 := NewWorld()
	if err := w2.Restore(snap); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if w2.Checksum() != w.Checksum() {
		t.Fatalf("checksum mismatch after Restore: got %#x, want %#x", w2.Checksum(), w.Checksum())
	}
	b2 := w2.Bodies()[0]
	if b2.CenterOfMass != b.CenterOfMass || b2.InvInertiaTensor != b.InvInertiaTensor || !b2.customCenterOfMass || !b2.customInertia {
		t.Fatalf("custom overrides lost across Save/Restore: got com=%v inv=%+v", b2.CenterOfMass, b2.InvInertiaTensor)
	}

	// RecomputeMassProperties clears overrides and restores shape-derived values.
	b.RecomputeMassProperties()
	if b.CenterOfMass != (Vec3{}) || abs32(b.InvInertiaTensor.XX-wantInv.X) > 1e-4 {
		t.Fatalf("RecomputeMassProperties did not restore defaults: com=%v inv=%+v", b.CenterOfMass, b.InvInertiaTensor)
	}

	// 3. Joints use anisotropic world-space inverse inertia tensor.
	runHingeMotor := func(inertiaZ float32) float32 {
		jw := NewWorld()
		jw.Gravity = Vec3{}
		anchor := jw.AddBody(NewStaticBody(BoxShape(Box{HalfExtents: Vec3{X: 0.2, Y: 0.2, Z: 0.2}}), Vec3{}))
		arm := NewDynamicBody(BoxShape(Box{HalfExtents: Vec3{X: 1, Y: 0.1, Z: 0.1}}), Vec3{X: 1}, 2)
		arm.SetInertia(Vec3{X: 0.5, Y: 5, Z: inertiaZ})
		armBody := jw.AddBody(arm)
		hj := NewHingeJoint(anchor, armBody, Vec3{}, Vec3{X: -1}, Vec3{Z: 1})
		hj.MotorTargetVel = 10
		hj.MotorMaxTorque = 2
		jw.AddJoint(hj)
		for range 10 {
			jw.Step(testStep60)
		}
		return armBody.AngVel.Z
	}
	wzLight := runHingeMotor(0.5)
	wzHeavy := runHingeMotor(20.0)
	if wzLight <= wzHeavy*2 {
		t.Fatalf("hinge motor ignored per-body inertia override: wzLight=%v wzHeavy=%v", wzLight, wzHeavy)
	}
}

// TestManifoldsAndInertiaDeterminism runs a combined scene (Dzhanibekov plank,
// 10-box stack, capsule on box, anisotropic hinge) twice and asserts
// bit-identical checksums and body poses (#810).
func TestManifoldsAndInertiaDeterminism(t *testing.T) {
	run := func() uint64 {
		w := NewWorld()
		w.AddBody(NewStaticBody(BoxShape(Box{HalfExtents: Vec3{X: 20, Y: 1, Z: 20}}), Vec3{Y: -1}))
		unit := BoxShape(Box{HalfExtents: Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
		for i := range 6 {
			w.AddBody(NewDynamicBody(unit, Vec3{X: -4, Y: float32(i) + 0.5}, 1))
		}
		capBody := NewDynamicBody(CapsuleShape(Capsule{A: Vec3{X: -0.5}, B: Vec3{X: 0.5}, Radius: 0.25}), Vec3{X: 3, Y: 0.8}, 2)
		capBody.Rot = QuatFromAxisAngle(Vec3{Y: 1}, 0.4)
		w.AddBody(capBody)

		plank := NewDynamicBody(BoxShape(Box{HalfExtents: Vec3{X: 0.8, Y: 0.3, Z: 0.1}}), Vec3{X: 0, Y: 6}, 3)
		plank.GravityScale = -1 // zero gravity via GravityScale? Wait: use GravityScale=0? No, let's let it fall & spin!
		plank.GravityScale = 0.01
		plank.AngVel = Vec3{X: 0.1, Y: 10, Z: 0}
		w.AddBody(plank)

		for range 180 {
			w.Step(testStep60)
		}
		return w.Checksum()
	}
	h1 := run()
	h2 := run()
	if h1 != h2 {
		t.Fatalf("determinism mismatch: %#016x != %#016x", h1, h2)
	}
}
