package physics3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics3d"
)

func TestSwingTwistAsymmetricConeAndTwistLimits(t *testing.T) {
	dt := time.Second / 60

	t.Run("AsymmetricSwingConeAllFourQuadrants", func(t *testing.T) {
		const (
			min1 = float32(-0.25)
			max1 = float32(0.90)
			min2 = float32(-0.35)
			max2 = float32(0.75)
		)

		runKick := func(angImpulse physics3d.Vec3) (minS1, maxS1, minS2, maxS2 float32) {
			w := physics3d.NewWorld()
			w.Gravity = physics3d.Vec3{}
			w.SolverIterations = 12

			anchor := w.AddBody(physics3d.NewStaticBody(
				physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}),
				physics3d.Vec3{},
			))
			rod := w.AddBody(physics3d.NewDynamicBody(
				physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.08, 1.0)),
				physics3d.Vec3{Y: 0.5},
				1.0,
			))
			rod.AngularDamping = 0.2

			st := physics3d.NewSwingTwistJoint(anchor, rod, physics3d.Vec3{}, physics3d.Vec3{Y: -0.5}, physics3d.Vec3{Y: 1})
			st.PlaneAxisA = physics3d.Vec3{X: 1}
			st.PlaneAxisB = physics3d.Vec3{X: 1}
			st.EnableSwingLimits = true
			st.SwingMin1 = min1
			st.SwingMax1 = max1
			st.SwingMin2 = min2
			st.SwingMax2 = max2
			w.AddJoint(st)

			rod.ApplyAngularImpulse(angImpulse)
			for range 120 {
				w.Step(dt)
				s1, s2 := st.SwingAngles()
				if s1 < minS1 {
					minS1 = s1
				}
				if s1 > maxS1 {
					maxS1 = s1
				}
				if s2 < minS2 {
					minS2 = s2
				}
				if s2 > maxS2 {
					maxS2 = s2
				}
				if s1 < min1-0.08 || s1 > max1+0.08 || s2 < min2-0.08 || s2 > max2+0.08 {
					t.Fatalf("swing (%v, %v) exceeded asymmetric bounds [%v,%v] x [%v,%v]", s1, s2, min1, max1, min2, max2)
				}
			}
			return minS1, maxS1, minS2, maxS2
		}

		// PlaneAxisA = +X (swing1 axis), Plane2A = (+Y) x (+X) = -Z (swing2 axis).
		_, pos1, _, _ := runKick(physics3d.Vec3{X: 3.0})
		neg1, _, _, _ := runKick(physics3d.Vec3{X: -3.0})
		_, _, _, pos2 := runKick(physics3d.Vec3{Z: -3.0})
		_, _, neg2, _ := runKick(physics3d.Vec3{Z: 3.0})

		if pos1 < 0.70 {
			t.Fatalf("positive swing1 reached only %v, want close to %v", pos1, max1)
		}
		if neg1 > -0.18 {
			t.Fatalf("negative swing1 reached only %v, want close to %v", neg1, min1)
		}
		if pos2 < 0.55 {
			t.Fatalf("positive swing2 reached only %v, want close to %v", pos2, max2)
		}
		if neg2 > -0.25 {
			t.Fatalf("negative swing2 reached only %v, want close to %v", neg2, min2)
		}
		// Asymmetry check: positive travel must clearly exceed negative travel magnitude.
		if pos1 <= -neg1+0.3 || pos2 <= -neg2+0.2 {
			t.Fatalf("expected asymmetric swing cone travel, got s1=[%v,%v] s2=[%v,%v]", neg1, pos1, neg2, pos2)
		}
	})

	t.Run("TwistLimitsPositiveAndNegative", func(t *testing.T) {
		const (
			minTw = float32(-0.30)
			maxTw = float32(0.65)
		)
		for _, spin := range []float32{4.0, -4.0} {
			w := physics3d.NewWorld()
			w.Gravity = physics3d.Vec3{}
			w.SolverIterations = 12

			anchor := w.AddBody(physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}), physics3d.Vec3{}))
			rod := w.AddBody(physics3d.NewDynamicBody(
				physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.1, 1.0)),
				physics3d.Vec3{Y: 0.5},
				1.0,
			))
			st := physics3d.NewSwingTwistJoint(anchor, rod, physics3d.Vec3{}, physics3d.Vec3{Y: -0.5}, physics3d.Vec3{Y: 1})
			st.EnableSwingLimits = true
			st.SwingMin1, st.SwingMax1 = -0.5, 0.5
			st.SwingMin2, st.SwingMax2 = -0.5, 0.5
			st.EnableTwistLimits = true
			st.MinTwist = minTw
			st.MaxTwist = maxTw
			w.AddJoint(st)

			// Apply sustained twist torque and a slight swing impulse simultaneously.
			rod.ApplyAngularImpulse(physics3d.Vec3{X: 0.5, Y: spin})
			var extreme float32
			for range 120 {
				rod.ApplyAngularImpulse(physics3d.Vec3{Y: spin * 0.05})
				w.Step(dt)
				tw := st.TwistAngle()
				if spin > 0 && tw > extreme {
					extreme = tw
				}
				if spin < 0 && tw < extreme {
					extreme = tw
				}
				if tw < minTw-0.08 || tw > maxTw+0.08 {
					t.Fatalf("twist angle %v exceeded [%v, %v] under spin=%v", tw, minTw, maxTw, spin)
				}
			}
			if spin > 0 && extreme < maxTw-0.15 {
				t.Fatalf("positive twist only reached %v, want near %v", extreme, maxTw)
			}
			if spin < 0 && extreme > minTw+0.15 {
				t.Fatalf("negative twist only reached %v, want near %v", extreme, minTw)
			}
		}
	})
}

func TestRagdollArmUnderLoadStaysWithinTwistAndSwingRange(t *testing.T) {
	const (
		minTwist  = float32(-0.40)
		maxTwist  = float32(0.50)
		swingMin1 = float32(-0.35)
		swingMax1 = float32(1.05)
		swingMin2 = float32(-0.45)
		swingMax2 = float32(0.85)
	)
	skeleton := []physics3d.SkeletonJoint{
		{Name: "pelvis", Parent: -1, LocalPos: physics3d.Vec3{}, Radius: 0.14, Length: 0.25, Mass: 10},
		{Name: "torso", Parent: 0, LocalPos: physics3d.Vec3{Y: 0.25}, Radius: 0.14, Length: 0.4, Mass: 12, SwingLimit: 0.4},
		{
			Name:      "upperArmL",
			Parent:    1,
			LocalPos:  physics3d.Vec3{X: -0.22, Y: 0.32},
			Radius:    0.06,
			Length:    0.35,
			Mass:      2.5,
			SwingMin1: swingMin1,
			SwingMax1: swingMax1,
			SwingMin2: swingMin2,
			SwingMax2: swingMax2,
			MinTwist:  minTwist,
			MaxTwist:  maxTwist,
		},
		{Name: "elbowL", Parent: 2, LocalPos: physics3d.Vec3{X: -0.35}, Radius: 0.05, Length: 0.30, Mass: 2.0, SwingLimit: 1.1},
	}

	w := physics3d.NewWorld()
	w.SolverIterations = 12
	rd := physics3d.BuildRagdoll(w, skeleton, physics3d.Vec3{Y: 3.0}, physics3d.QuatIdentity())

	// Pin pelvis and torso so gravity and external twisting torque load the shoulder directly.
	rd.Bodies[0].Static = true
	rd.Bodies[0].InvMass = 0
	rd.Bodies[0].InvInertia = 0
	rd.Bodies[1].Static = true
	rd.Bodies[1].InvMass = 0
	rd.Bodies[1].InvInertia = 0

	var shoulderJoint *physics3d.SwingTwistJoint
	for _, j := range rd.Joints {
		a, b := j.Bodies()
		if a == rd.Bodies[1] && b == rd.Bodies[2] {
			st, ok := j.(*physics3d.SwingTwistJoint)
			if !ok {
				t.Fatalf("expected *SwingTwistJoint for shoulder, got %T", j)
			}
			shoulderJoint = st
		}
	}
	if shoulderJoint == nil {
		t.Fatal("shoulder joint not found in Ragdoll.Joints")
	}

	dt := time.Second / 60
	arm := rd.Bodies[2]
	forearm := rd.Bodies[3]

	// Apply sustained twisting load around the arm's longitudinal axis plus downward gravity load.
	for step := range 180 {
		// Alternate twist torque directions so both MinTwist and MaxTwist are loaded.
		twistDir := float32(1)
		if step >= 90 {
			twistDir = -1
		}
		armAxisWorld := arm.Rot.RotateVec(physics3d.Vec3{X: -1})
		arm.ApplyAngularImpulse(armAxisWorld.Scale(twistDir * 0.08))
		forearm.ApplyImpulse(physics3d.Vec3{Y: -0.15, Z: 0.05})

		w.Step(dt)

		tw := shoulderJoint.TwistAngle()
		if tw < minTwist-0.09 || tw > maxTwist+0.09 {
			t.Fatalf("step %d: ragdoll arm twist %v rad outside [%v, %v]", step, tw, minTwist, maxTwist)
		}
		s1, s2 := shoulderJoint.SwingAngles()
		if s1 < swingMin1-0.12 || s1 > swingMax1+0.12 || s2 < swingMin2-0.12 || s2 > swingMax2+0.12 {
			t.Fatalf("step %d: ragdoll arm swing (%v, %v) outside [%v,%v] x [%v,%v]",
				step, s1, s2, swingMin1, swingMax1, swingMin2, swingMax2)
		}
	}
}

func TestHingePositionDriveClosesDisplacedDoor(t *testing.T) {
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}
	w.SolverIterations = 12

	frame := w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.1, Y: 1.1, Z: 0.1}}),
		physics3d.Vec3{},
	))

	const doorHalfW = float32(0.5)
	door := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: doorHalfW, Y: 1.0, Z: 0.05}}),
		physics3d.Vec3{X: doorHalfW},
		4.0,
	))

	hinge := physics3d.NewHingeJoint(frame, door, physics3d.Vec3{}, physics3d.Vec3{X: -doorHalfW}, physics3d.Vec3{Y: 1})
	hinge.EnableLimits = true
	hinge.MinAngle = -0.05
	hinge.MaxAngle = float32(math.Pi / 2)
	hinge.DriveTargetAngle = 0
	hinge.DriveStiffness = 35
	hinge.DriveDamping = 10
	hinge.DriveMaxTorque = 50
	w.AddJoint(hinge)

	// Displace the door open to 70 degrees around +Y while keeping its hinge pivot at origin.
	openAngle := float32(70 * math.Pi / 180)
	door.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, openAngle)
	door.Pos = door.Rot.RotateVec(physics3d.Vec3{X: doorHalfW})
	// Also test waking from sleep when displaced from target.
	door.Sleeping = true

	if !approx(hinge.Angle(), openAngle, 0.02) {
		t.Fatalf("initial displaced door angle = %v, want %v", hinge.Angle(), openAngle)
	}

	dt := time.Second / 60
	var maxTorqueSeen float32
	for range 240 {
		w.Step(dt)
		if trq := hinge.ReactionTorque().Len(); trq > maxTorqueSeen {
			maxTorqueSeen = trq
		}
	}

	if ang := hinge.Angle(); !approx(ang, 0, 0.02) {
		t.Fatalf("position-driven door did not return to closed (0): got %v rad", ang)
	}
	if door.AngVel.Len() > 0.15 {
		t.Fatalf("door still oscillating at end of close: angVel=%v", door.AngVel)
	}
	if maxTorqueSeen < 1.0 {
		t.Fatalf("expected measurable hinge reaction torque during close, got %v", maxTorqueSeen)
	}
}

func TestSliderSpringAndPositionDrive(t *testing.T) {
	dt := time.Second / 60

	t.Run("ReturnsDisplacedCarriageToTarget", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}

		rail := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.1, Y: 0.1, Z: 0.1}}), physics3d.Vec3{}))
		carriage := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.25, Y: 0.25, Z: 0.25}}), physics3d.Vec3{X: 1.8}, 2.0))

		slider := physics3d.NewSliderJoint(rail, carriage, physics3d.Vec3{}, physics3d.Vec3{}, physics3d.Vec3{X: 1})
		slider.DriveTargetPos = 0.25
		slider.DriveStiffness = 45
		slider.DriveDamping = 14
		slider.DriveMaxForce = 80
		w.AddJoint(slider)

		for range 240 {
			w.Step(dt)
		}
		if tr := slider.Translation(); !approx(tr, 0.25, 0.02) {
			t.Fatalf("slider spring drive settled at %v, want 0.25", tr)
		}
		if carriage.Vel.Len() > 0.05 {
			t.Fatalf("slider carriage still moving: vel=%v", carriage.Vel)
		}
	})

	t.Run("VerticalSuspensionEquilibriumUnderGravity", func(t *testing.T) {
		w := physics3d.NewWorld()
		mount := w.AddBody(physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}), physics3d.Vec3{Y: 4}))
		mass := float32(2.0)
		wheel := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.3, Z: 0.3}}), physics3d.Vec3{Y: 4}, mass))

		k := float32(80)
		slider := physics3d.NewSliderJoint(mount, wheel, physics3d.Vec3{}, physics3d.Vec3{}, physics3d.Vec3{Y: 1})
		slider.DriveTargetPos = 0
		slider.DriveStiffness = k
		slider.DriveDamping = 8
		w.AddJoint(slider)

		for range 240 {
			w.Step(dt)
		}
		// Static equilibrium sag: -m*g/k = -2*9.81/80 = -0.24525 m (with small
		// symplectic-Euler damping offset -(c + dt*k)*g*dt/k ≈ -0.019 m).
		wantY := -mass * 9.81 / k
		if tr := slider.Translation(); !approx(tr, wantY, 0.03) {
			t.Fatalf("vertical slider suspension equilibrium = %v, want %v", tr, wantY)
		}
	})
}

func TestSixDOFPerAxisLockFreeLimitedSpringAndDrives(t *testing.T) {
	dt := time.Second / 60

	t.Run("LinearLockSpringLimitAndVelocityDrive", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.SolverIterations = 12
		base := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.2, Y: 0.2, Z: 0.2}}), physics3d.Vec3{}))
		mass := float32(1.5)
		plat := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.2, Z: 0.3}}), physics3d.Vec3{}, mass))
		plat.LinearDamping = 0

		j := physics3d.NewSixDOFJoint(base, plat, physics3d.Vec3{}, physics3d.Vec3{})
		// X: locked at 0
		j.Linear[0] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
		// Y: limited [-0.6, 0.6] with spring stiffness & damping resisting gravity
		j.Linear[1] = physics3d.AxisConfig{
			Mode:      physics3d.AxisLimited,
			Min:       -0.6,
			Max:       0.6,
			Stiffness: 60,
			Damping:   12,
			TargetPos: 0,
		}
		// Z: free with velocity drive toward +2.5 m/s
		j.Linear[2] = physics3d.AxisConfig{
			Mode:      physics3d.AxisFree,
			Drive:     physics3d.DriveVelocity,
			TargetVel: 2.5,
			MaxForce:  40,
		}
		// All 3 angular axes locked
		j.Angular[0] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
		j.Angular[1] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
		j.Angular[2] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
		w.AddJoint(j)

		// Apply sideways disturbance on locked X and angular impulse to confirm locks hold.
		plat.ApplyImpulse(physics3d.Vec3{X: 5})
		plat.ApplyAngularImpulse(physics3d.Vec3{X: 2, Y: -2, Z: 2})

		for range 180 {
			w.Step(dt)
		}

		lp := j.LinearPosition()
		if !approx(lp.X, 0, 0.02) {
			t.Fatalf("SixDOF locked X drifted to %v", lp.X)
		}
		wantY := -mass * 9.81 / 60
		if !approx(lp.Y, wantY, 0.04) {
			t.Fatalf("SixDOF spring Y = %v, want equilibrium ~%v", lp.Y, wantY)
		}
		if !approx(plat.Vel.Z, 2.5, 0.1) || lp.Z < 5.0 {
			t.Fatalf("SixDOF velocity-driven Z: vel=%v pos=%v", plat.Vel.Z, lp.Z)
		}
		if ang := base.Rot.AngleTo(plat.Rot); ang > 0.03 {
			t.Fatalf("SixDOF locked orientation drifted by %v rad", ang)
		}
	})

	t.Run("AngularSingleAxisPositionDriveAndLimits", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}

		anchor := w.AddBody(physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}), physics3d.Vec3{}))
		arm := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.1, Z: 0.1}}), physics3d.Vec3{X: 0.5}, 2.0))

		j := physics3d.NewSixDOFJoint(anchor, arm, physics3d.Vec3{}, physics3d.Vec3{X: -0.5})
		for k := range 3 {
			j.Linear[k] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
		}
		j.Angular[0] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
		j.Angular[1] = physics3d.AxisConfig{
			Mode:      physics3d.AxisLimited,
			Min:       -0.2,
			Max:       1.3,
			Drive:     physics3d.DrivePosition,
			TargetPos: 0.9,
			Stiffness: 40,
			Damping:   10,
			MaxForce:  60,
		}
		j.Angular[2] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
		w.AddJoint(j)

		for range 180 {
			w.Step(dt)
		}
		if got := j.AngularPosition().Y; !approx(got, 0.9, 0.04) {
			t.Fatalf("SixDOF single-axis angular position drive = %v rad, want 0.9", got)
		}

		// Target beyond Max=1.3 must be clamped by the angular limit.
		j.Angular[1].TargetPos = 2.2
		for range 120 {
			w.Step(dt)
		}
		if got := j.AngularPosition().Y; !approx(got, 1.3, 0.05) {
			t.Fatalf("SixDOF angular limit failed to clamp at 1.3: got %v rad", got)
		}
	})

	t.Run("AngularMultiAxisLockLimitedAndSpringDrive", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}
		w.SolverIterations = 12

		anchor := w.AddBody(physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}), physics3d.Vec3{}))
		body := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.3, Z: 0.3}}), physics3d.Vec3{}, 2.0))

		j := physics3d.NewSixDOFJoint(anchor, body, physics3d.Vec3{}, physics3d.Vec3{})
		for k := range 3 {
			j.Linear[k] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
		}
		// X locked, Y limited to [-0.4, 0.4], Z position-driven to +0.5 rad.
		j.Angular[0] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
		j.Angular[1] = physics3d.AxisConfig{Mode: physics3d.AxisLimited, Min: -0.4, Max: 0.4}
		j.Angular[2] = physics3d.AxisConfig{
			Mode:      physics3d.AxisFree,
			Drive:     physics3d.DrivePosition,
			TargetPos: 0.5,
			Stiffness: 35,
			Damping:   8,
		}
		w.AddJoint(j)

		body.ApplyAngularImpulse(physics3d.Vec3{X: 2.0, Y: 3.0})
		for range 180 {
			w.Step(dt)
			ap := j.AngularPosition()
			if ap.Y < -0.50 || ap.Y > 0.50 {
				t.Fatalf("SixDOF limited angular Y exceeded [-0.4, 0.4]: got %v", ap.Y)
			}
		}
		ap := j.AngularPosition()
		if !approx(ap.X, 0, 0.04) {
			t.Fatalf("SixDOF locked angular X drifted to %v", ap.X)
		}
		if !approx(ap.Z, 0.5, 0.05) {
			t.Fatalf("SixDOF driven angular Z = %v, want ~0.5", ap.Z)
		}
	})
}

func TestJointReactionForceAndTorqueReadout(t *testing.T) {
	dt := time.Second / 60
	const mass = float32(2.0)
	wantForceY := mass * 9.81

	type reactionJoint interface {
		physics3d.Joint3D
		ReactionForce() physics3d.Vec3
		ReactionTorque() physics3d.Vec3
	}

	cases := []struct {
		name  string
		build func(anchor, bob *physics3d.Body) reactionJoint
	}{
		{"DistanceJoint", func(a, b *physics3d.Body) reactionJoint {
			return physics3d.NewDistanceJoint(a, b, physics3d.Vec3{}, physics3d.Vec3{}, 1.0, 1.0)
		}},
		{"BallJoint", func(a, b *physics3d.Body) reactionJoint {
			return physics3d.NewBallJoint(a, b, physics3d.Vec3{}, physics3d.Vec3{Y: 1.0})
		}},
		{"HingeJoint", func(a, b *physics3d.Body) reactionJoint {
			return physics3d.NewHingeJoint(a, b, physics3d.Vec3{}, physics3d.Vec3{Y: 1.0}, physics3d.Vec3{Z: 1})
		}},
		{"SliderJoint", func(a, b *physics3d.Body) reactionJoint {
			return physics3d.NewSliderJoint(a, b, physics3d.Vec3{}, physics3d.Vec3{Y: 1.0}, physics3d.Vec3{X: 1})
		}},
		{"FixedJoint", func(a, b *physics3d.Body) reactionJoint {
			return physics3d.NewFixedJoint(a, b, physics3d.Vec3{}, physics3d.Vec3{Y: 1.0})
		}},
		{"SwingTwistJoint", func(a, b *physics3d.Body) reactionJoint {
			return physics3d.NewSwingTwistJoint(a, b, physics3d.Vec3{}, physics3d.Vec3{Y: 1.0}, physics3d.Vec3{Y: 1})
		}},
		{"SixDOFJoint", func(a, b *physics3d.Body) reactionJoint {
			j := physics3d.NewSixDOFJoint(a, b, physics3d.Vec3{}, physics3d.Vec3{Y: 1.0})
			for k := range 3 {
				j.Linear[k].Mode = physics3d.AxisLocked
				j.Angular[k].Mode = physics3d.AxisLocked
			}
			return j
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := physics3d.NewWorld()
			w.SolverIterations = 12
			anchor := w.AddBody(physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}), physics3d.Vec3{Y: 5}))
			bob := w.AddBody(physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.25}), physics3d.Vec3{Y: 4}, mass))
			bob.LinearDamping = 0
			j := tc.build(anchor, bob)
			w.AddJoint(j)

			for range 15 {
				w.Step(dt)
			}
			f := j.ReactionForce()
			if !approx(f.Y, wantForceY, 0.5) {
				t.Fatalf("%s ReactionForce().Y = %v N, want ~%v N (full=%v)", tc.name, f.Y, wantForceY, f)
			}
		})
	}

	// Cantilever beam held horizontally by a FixedJoint must report bending torque ~ m*g*L.
	t.Run("CantileverReactionTorque", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.SolverIterations = 16
		wall := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.1, Y: 1, Z: 1}}), physics3d.Vec3{}))
		beam := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.1, Z: 0.1}}), physics3d.Vec3{X: 0.5}, mass))
		beam.LinearDamping = 0
		beam.AngularDamping = 0
		fj := physics3d.NewFixedJoint(wall, beam, physics3d.Vec3{}, physics3d.Vec3{X: -0.5})
		w.AddJoint(fj)

		for range 15 {
			w.Step(dt)
		}
		f := fj.ReactionForce()
		trq := fj.ReactionTorque()
		if !approx(f.Y, wantForceY, 1.5) {
			t.Fatalf("cantilever ReactionForce().Y = %v N, want ~%v N", f.Y, wantForceY)
		}
		// m * g * 0.5m = 9.81 N*m around +Z.
		if trq.Z < 6.0 || trq.Z > 13.0 {
			t.Fatalf("cantilever ReactionTorque().Z = %v N*m, want ~9.81 N*m (full=%v)", trq.Z, trq)
		}
	})
}

func TestBreakablePlankBridgeFallsApartAndReportsEvents(t *testing.T) {
	dt := time.Second / 60

	t.Run("PlankBridgeBreaksUnderHeavyBoulder", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.SolverIterations = 14

		const (
			numPlanks = 5
			plankLen  = float32(1.0)
			deckY     = float32(4.0)
		)
		leftBank := w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.25, Z: 1.0}}),
			physics3d.Vec3{X: -0.5, Y: deckY},
		))
		rightBank := w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.25, Z: 1.0}}),
			physics3d.Vec3{X: float32(numPlanks)*plankLen + 0.5, Y: deckY},
		))

		planks := make([]*physics3d.Body, numPlanks)
		for i := range numPlanks {
			p := physics3d.NewDynamicBody(
				physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: plankLen * 0.48, Y: 0.1, Z: 0.8}}),
				physics3d.Vec3{X: (float32(i) + 0.5) * plankLen, Y: deckY},
				1.0,
			)
			p.LinearDamping = 0.2
			p.AngularDamping = 0.2
			planks[i] = w.AddBody(p)
		}

		// Connect abutments and planks with breakable HingeJoints (BreakForce = 250 N,
		// comfortably above the flat bridge's ~120 N self-weight catenary tension
		// but well below a falling 30 kg boulder's load).
		const breakForce = float32(250)
		joints := make([]*physics3d.HingeJoint, 0, numPlanks+1)
		addBridgeJoint := func(a, b *physics3d.Body, ancA, ancB physics3d.Vec3) {
			hj := physics3d.NewHingeJoint(a, b, ancA, ancB, physics3d.Vec3{Z: 1})
			hj.BreakForce = breakForce
			w.AddJoint(hj)
			joints = append(joints, hj)
		}
		addBridgeJoint(leftBank, planks[0], physics3d.Vec3{X: 0.5}, physics3d.Vec3{X: -plankLen * 0.5})
		for i := 0; i < numPlanks-1; i++ {
			addBridgeJoint(planks[i], planks[i+1], physics3d.Vec3{X: plankLen * 0.5}, physics3d.Vec3{X: -plankLen * 0.5})
		}
		addBridgeJoint(planks[numPlanks-1], rightBank, physics3d.Vec3{X: plankLen * 0.5}, physics3d.Vec3{X: -0.5})

		var callbackEvents []physics3d.JointBreakEvent
		w.OnJointBreak = func(ev physics3d.JointBreakEvent) {
			callbackEvents = append(callbackEvents, ev)
		}

		// Phase 1: bridge supports its own weight for 1 second without breaking.
		for range 60 {
			w.Step(dt)
			if len(w.JointBreakEvents()) != 0 {
				t.Fatalf("bridge broke under its own weight: %+v", w.JointBreakEvents())
			}
		}
		if len(w.Joints()) != len(joints) {
			t.Fatalf("expected all %d bridge joints intact before boulder, got %d", len(joints), len(w.Joints()))
		}

		// Phase 2: drop a heavy boulder onto the center of the bridge.
		boulder := w.AddBody(physics3d.NewDynamicBody(
			physics3d.SphereShape(physics3d.Sphere{Radius: 0.45}),
			physics3d.Vec3{X: float32(numPlanks) * plankLen * 0.5, Y: deckY + 2.5},
			30.0,
		))
		_ = boulder

		var stepEvents []physics3d.JointBreakEvent
		for range 180 {
			w.Step(dt)
			stepEvents = append(stepEvents, w.JointBreakEvents()...)
		}

		if len(stepEvents) == 0 {
			t.Fatal("expected plank bridge joints to break under boulder load, got 0 break events")
		}
		if len(callbackEvents) != len(stepEvents) {
			t.Fatalf("OnJointBreak fired %d times != %d JointBreakEvents()", len(callbackEvents), len(stepEvents))
		}
		for i, ev := range stepEvents {
			if callbackEvents[i] != ev {
				t.Fatalf("event %d mismatch: callback=%+v step=%+v", i, callbackEvents[i], ev)
			}
			if ev.Force.Len() <= breakForce {
				t.Fatalf("reported break event %d force=%v N <= BreakForce %v N", i, ev.Force.Len(), breakForce)
			}
			hj, ok := ev.Joint.(*physics3d.HingeJoint)
			if !ok || !hj.Broken() {
				t.Fatalf("broken joint %d not marked Broken()", i)
			}
		}

		// Remaining active joints in world must equal total minus broken.
		if got, want := len(w.Joints()), len(joints)-len(stepEvents); got != want {
			t.Fatalf("remaining world joints = %d, want %d (broken=%d)", got, want, len(stepEvents))
		}

		// Center plank must have fallen well below the deck after the bridge gave way.
		if planks[numPlanks/2].Pos.Y > deckY-1.5 {
			t.Fatalf("bridge center plank did not fall after breaking: Y=%v (deckY=%v)", planks[numPlanks/2].Pos.Y, deckY)
		}
	})

	t.Run("BreakTorqueThreshold", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}

		wall := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.1, Y: 0.5, Z: 0.5}}), physics3d.Vec3{}))
		bar := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.1, Z: 0.1}}), physics3d.Vec3{X: 0.5}, 2.0))

		fj := physics3d.NewFixedJoint(wall, bar, physics3d.Vec3{}, physics3d.Vec3{X: -0.5})
		fj.BreakTorque = 15.0
		w.AddJoint(fj)

		// Snap the bar with a strong twisting angular impulse that exceeds BreakTorque.
		bar.ApplyAngularImpulse(physics3d.Vec3{Z: 5.0})
		w.Step(dt)

		evs := w.JointBreakEvents()
		if len(evs) != 1 || evs[0].Joint != fj || !fj.Broken() {
			t.Fatalf("expected FixedJoint to break on torque, events=%+v broken=%v", evs, fj.Broken())
		}
		if evs[0].Torque.Len() <= fj.BreakTorque {
			t.Fatalf("break torque %v <= threshold %v", evs[0].Torque.Len(), fj.BreakTorque)
		}
		if len(w.Joints()) != 0 {
			t.Fatalf("broken joint was not removed from world: len=%d", len(w.Joints()))
		}
	})
}

func TestSixDOFSwingTwistBreakableDeterminism(t *testing.T) {
	type breakRecord struct {
		step   int
		aID    int
		bID    int
		force  physics3d.Vec3
		torque physics3d.Vec3
	}
	type simSnapshot struct {
		pos       []physics3d.Vec3
		rot       []physics3d.Quat
		vel       []physics3d.Vec3
		angVel    []physics3d.Vec3
		forces    []physics3d.Vec3
		torques   []physics3d.Vec3
		breaks    []breakRecord
		remJoints int
	}

	run := func() simSnapshot {
		w := physics3d.NewWorld()
		w.SolverIterations = 12
		dt := time.Second / 60

		// 1. Driven door hinge.
		frame := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.1, Y: 1, Z: 0.1}}), physics3d.Vec3{X: -4, Y: 2}))
		door := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 1, Z: 0.05}}), physics3d.Vec3{X: -3.5, Y: 2}, 3.0))
		hinge := physics3d.NewHingeJoint(frame, door, physics3d.Vec3{}, physics3d.Vec3{X: -0.5}, physics3d.Vec3{Y: 1})
		hinge.DriveTargetAngle = 0
		hinge.DriveStiffness = 30
		hinge.DriveDamping = 8
		w.AddJoint(hinge)
		door.ApplyAngularImpulse(physics3d.Vec3{Y: 2.0})

		// 2. 6DOF spring-suspended platform with velocity drive.
		base := w.AddBody(physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}), physics3d.Vec3{X: 4, Y: 4}))
		plat := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.2, Z: 0.3}}), physics3d.Vec3{X: 4, Y: 4}, 2.0))
		six := physics3d.NewSixDOFJoint(base, plat, physics3d.Vec3{}, physics3d.Vec3{})
		six.Linear[0].Mode = physics3d.AxisLocked
		six.Linear[1] = physics3d.AxisConfig{Mode: physics3d.AxisLimited, Min: -0.5, Max: 0.5, Stiffness: 50, Damping: 8}
		six.Linear[2] = physics3d.AxisConfig{Mode: physics3d.AxisFree, Drive: physics3d.DriveVelocity, TargetVel: 1.5, MaxForce: 20}
		six.Angular[0].Mode = physics3d.AxisLocked
		six.Angular[1] = physics3d.AxisConfig{Mode: physics3d.AxisLimited, Min: -0.5, Max: 0.5}
		six.Angular[2] = physics3d.AxisConfig{Mode: physics3d.AxisFree, Drive: physics3d.DrivePosition, TargetPos: 0.4, Stiffness: 25, Damping: 6}
		w.AddJoint(six)

		// 3. Ragdoll arm with SwingTwistJoint.
		skel := []physics3d.SkeletonJoint{
			{Name: "torso", Parent: -1, Radius: 0.12, Length: 0.3, Mass: 8},
			{Name: "upperArm", Parent: 0, LocalPos: physics3d.Vec3{X: 0.2, Y: 0.1}, Radius: 0.06, Length: 0.35, Mass: 2.0, SwingMin1: -0.3, SwingMax1: 0.9, SwingMin2: -0.4, SwingMax2: 0.7, MinTwist: -0.35, MaxTwist: 0.45},
		}
		rd := physics3d.BuildRagdoll(w, skel, physics3d.Vec3{Y: 6}, physics3d.QuatIdentity())
		rd.Bodies[0].Static = true
		rd.Bodies[0].InvMass = 0
		rd.Bodies[0].InvInertia = 0
		rd.Bodies[1].ApplyAngularImpulse(physics3d.Vec3{X: 1.5, Y: 1.0, Z: -0.8})

		// 4. Breakable 2-plank bridge with falling boulder.
		bankL := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.2, Z: 0.5}}), physics3d.Vec3{X: -1.3, Y: 1}))
		bankR := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.2, Z: 0.5}}), physics3d.Vec3{X: 1.3, Y: 1}))
		p1 := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.48, Y: 0.1, Z: 0.5}}), physics3d.Vec3{X: -0.5, Y: 1}, 1.0))
		p2 := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.48, Y: 0.1, Z: 0.5}}), physics3d.Vec3{X: 0.5, Y: 1}, 1.0))
		for _, pair := range []struct {
			a, b       *physics3d.Body
			ancA, ancB physics3d.Vec3
		}{
			{bankL, p1, physics3d.Vec3{X: 0.3}, physics3d.Vec3{X: -0.5}},
			{p1, p2, physics3d.Vec3{X: 0.5}, physics3d.Vec3{X: -0.5}},
			{p2, bankR, physics3d.Vec3{X: 0.5}, physics3d.Vec3{X: -0.3}},
		} {
			hj := physics3d.NewHingeJoint(pair.a, pair.b, pair.ancA, pair.ancB, physics3d.Vec3{Z: 1})
			hj.BreakForce = 80
			w.AddJoint(hj)
		}
		w.AddBody(physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.4}), physics3d.Vec3{Y: 3.0}, 20.0))

		var breaks []breakRecord
		for step := range 120 {
			w.Step(dt)
			for _, ev := range w.JointBreakEvents() {
				var aID, bID int
				if ev.BodyA != nil {
					aID = ev.BodyA.ID
				}
				if ev.BodyB != nil {
					bID = ev.BodyB.ID
				}
				breaks = append(breaks, breakRecord{
					step:   step,
					aID:    aID,
					bID:    bID,
					force:  ev.Force,
					torque: ev.Torque,
				})
			}
		}

		bodies := w.Bodies()
		snap := simSnapshot{
			pos:       make([]physics3d.Vec3, len(bodies)),
			rot:       make([]physics3d.Quat, len(bodies)),
			vel:       make([]physics3d.Vec3, len(bodies)),
			angVel:    make([]physics3d.Vec3, len(bodies)),
			forces:    []physics3d.Vec3{hinge.ReactionForce(), six.ReactionForce()},
			torques:   []physics3d.Vec3{hinge.ReactionTorque(), six.ReactionTorque()},
			breaks:    breaks,
			remJoints: len(w.Joints()),
		}
		for i, b := range bodies {
			snap.pos[i] = b.Pos
			snap.rot[i] = b.Rot
			snap.vel[i] = b.Vel
			snap.angVel[i] = b.AngVel
		}
		return snap
	}

	s1 := run()
	s2 := run()

	if len(s1.breaks) == 0 {
		t.Fatal("determinism scene expected at least one joint break event")
	}
	if s1.remJoints != s2.remJoints || len(s1.breaks) != len(s2.breaks) {
		t.Fatalf("joint break mismatch: run1=(rem=%d breaks=%d) run2=(rem=%d breaks=%d)",
			s1.remJoints, len(s1.breaks), s2.remJoints, len(s2.breaks))
	}
	vecBitsEq := func(a, b physics3d.Vec3) bool {
		return math.Float32bits(a.X) == math.Float32bits(b.X) &&
			math.Float32bits(a.Y) == math.Float32bits(b.Y) &&
			math.Float32bits(a.Z) == math.Float32bits(b.Z)
	}
	quatBitsEq := func(a, b physics3d.Quat) bool {
		return math.Float32bits(a.X) == math.Float32bits(b.X) &&
			math.Float32bits(a.Y) == math.Float32bits(b.Y) &&
			math.Float32bits(a.Z) == math.Float32bits(b.Z) &&
			math.Float32bits(a.W) == math.Float32bits(b.W)
	}
	for i := range s1.pos {
		if !vecBitsEq(s1.pos[i], s2.pos[i]) || !quatBitsEq(s1.rot[i], s2.rot[i]) ||
			!vecBitsEq(s1.vel[i], s2.vel[i]) || !vecBitsEq(s1.angVel[i], s2.angVel[i]) {
			t.Fatalf("body %d diverged between identical runs: run1=(%v,%v) run2=(%v,%v)",
				i, s1.pos[i], s1.rot[i], s2.pos[i], s2.rot[i])
		}
	}
	for i := range s1.forces {
		if !vecBitsEq(s1.forces[i], s2.forces[i]) || !vecBitsEq(s1.torques[i], s2.torques[i]) {
			t.Fatalf("reaction %d diverged: run1=(%v,%v) run2=(%v,%v)",
				i, s1.forces[i], s1.torques[i], s2.forces[i], s2.torques[i])
		}
	}
	for i := range s1.breaks {
		if s1.breaks[i].step != s2.breaks[i].step ||
			s1.breaks[i].aID != s2.breaks[i].aID ||
			s1.breaks[i].bID != s2.breaks[i].bID ||
			!vecBitsEq(s1.breaks[i].force, s2.breaks[i].force) ||
			!vecBitsEq(s1.breaks[i].torque, s2.breaks[i].torque) {
			t.Fatalf("break event %d diverged: run1=%+v run2=%+v", i, s1.breaks[i], s2.breaks[i])
		}
	}
}
