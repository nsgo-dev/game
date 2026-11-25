package physics_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
)

func TestRevoluteJointLimitsHoldUnderHeavyLoad(t *testing.T) {
	const dt = time.Second / 60

	var rw physics.RigidWorld
	rw.GravityY = 900
	rw.SolverIterations = 12

	bracket := physics.NewStaticRigidBox(100, 100, 12, 12)
	bar := physics.NewRigidBox(140, 100, 80, 14, 5)
	rw.AddBody(bracket, bar)

	hinge := physics.NewRevoluteJoint(bracket, bar, core.Point{X: 100, Y: 100})
	hinge.EnableLimit = true
	hinge.LowerAngle = -0.35
	hinge.UpperAngle = 0.40
	rw.AddJoint(hinge)

	hitUpper := false
	for range 120 {
		rw.Step(dt)
		ang := hinge.JointAngle()
		if ang < hinge.LowerAngle-0.02 || ang > hinge.UpperAngle+0.02 {
			t.Fatalf("limited revolute angle out of bounds under downward load: angle=%v bounds=[%v, %v]",
				ang, hinge.LowerAngle, hinge.UpperAngle)
		}
		pA := bracket.LocalToWorld(hinge.AnchorA)
		pB := bar.LocalToWorld(hinge.AnchorB)
		if err := float32(math.Hypot(float64(pB.X-pA.X), float64(pB.Y-pA.Y))); err > 0.5 {
			t.Fatalf("revolute pivot drifted under load: err=%v", err)
		}
		if ang >= hinge.UpperAngle-0.02 {
			hitUpper = true
		}
	}
	if !hitUpper {
		t.Fatalf("heavy bar never reached upper angle limit %v: final angle=%v", hinge.UpperAngle, hinge.JointAngle())
	}
	if tq := hinge.ReactionTorque(); tq >= -1 {
		t.Fatalf("expected negative opposing reaction torque at upper stop, got %v", tq)
	}

	// Reverse gravity upward and confirm lower limit holds under load.
	rw.GravityY = -900
	hitLower := false
	for range 120 {
		rw.Step(dt)
		ang := hinge.JointAngle()
		if ang < hinge.LowerAngle-0.02 || ang > hinge.UpperAngle+0.02 {
			t.Fatalf("limited revolute angle out of bounds under upward load: angle=%v bounds=[%v, %v]",
				ang, hinge.LowerAngle, hinge.UpperAngle)
		}
		if ang <= hinge.LowerAngle+0.02 {
			hitLower = true
		}
	}
	if !hitLower {
		t.Fatalf("heavy bar never reached lower angle limit %v: final angle=%v", hinge.LowerAngle, hinge.JointAngle())
	}
}

func TestRevoluteJointMotorReachesTargetSpeedAndRespectsLimit(t *testing.T) {
	const dt = time.Second / 60

	var rw physics.RigidWorld
	rw.SolverIterations = 12

	hub := physics.NewStaticRigidBox(100, 100, 10, 10)
	arm := physics.NewRigidBox(130, 100, 60, 10, 1)
	rw.AddBody(hub, arm)

	hinge := physics.NewRevoluteJoint(hub, arm, core.Point{X: 100, Y: 100})
	hinge.EnableMotor = true
	hinge.MotorSpeed = 4.0
	hinge.MaxMotorTorque = 20000
	rw.AddJoint(hinge)

	for range 60 {
		rw.Step(dt)
	}
	if speed := hinge.JointSpeed(); !near(speed, 4.0, 0.1) {
		t.Fatalf("revolute motor speed = %v, want ~4.0 rad/s", speed)
	}

	// Enable upper limit while motor continues pushing: limit must stop rotation.
	hinge.EnableLimit = true
	hinge.LowerAngle = -1.0
	hinge.UpperAngle = hinge.JointAngle() + 0.5
	for range 90 {
		rw.Step(dt)
		if ang := hinge.JointAngle(); ang > hinge.UpperAngle+0.02 {
			t.Fatalf("motor drove past upper limit: angle=%v upper=%v", ang, hinge.UpperAngle)
		}
	}
	if !near(hinge.JointAngle(), hinge.UpperAngle, 0.02) {
		t.Fatalf("motor did not reach upper limit: angle=%v upper=%v", hinge.JointAngle(), hinge.UpperAngle)
	}
}

func TestPrismaticJointSlidesClampsAndMotorDrives(t *testing.T) {
	const dt = time.Second / 60

	var rw physics.RigidWorld
	rw.SolverIterations = 12

	rail := physics.NewStaticRigidBox(100, 100, 20, 20)
	carriage := physics.NewRigidBox(100, 100, 24, 16, 2)
	rw.AddBody(rail, carriage)

	// Tilted slide axis (2, 1) normalized.
	slider := physics.NewPrismaticJoint(rail, carriage, core.Point{X: 100, Y: 100}, core.Point{X: 2, Y: 1})
	slider.EnableLimit = true
	slider.LowerTranslation = -25
	slider.UpperTranslation = 60
	slider.EnableMotor = true
	slider.MotorSpeed = 90
	slider.MaxMotorForce = 8000
	rw.AddJoint(slider)

	reachedSpeed := false
	ax := 2.0 / math.Hypot(2, 1)
	ay := 1.0 / math.Hypot(2, 1)
	px, py := -ay, ax

	for range 90 {
		rw.Step(dt)
		// Check perpendicular alignment and locked orientation.
		dx := float64(carriage.X) - float64(rail.X)
		dy := float64(carriage.Y) - float64(rail.Y)
		perpErr := math.Abs(dx*px + dy*py)
		if perpErr > 0.5 {
			t.Fatalf("prismatic carriage drifted off slide axis: perpErr=%v", perpErr)
		}
		if math.Abs(float64(carriage.Angle)) > 0.02 {
			t.Fatalf("prismatic carriage rotated unexpectedly: angle=%v", carriage.Angle)
		}
		tr := slider.JointTranslation()
		if tr < slider.LowerTranslation-0.5 || tr > slider.UpperTranslation+0.5 {
			t.Fatalf("prismatic translation out of limits: tr=%v bounds=[%v, %v]",
				tr, slider.LowerTranslation, slider.UpperTranslation)
		}
		if near(slider.JointSpeed(), 90, 5) {
			reachedSpeed = true
		}
	}
	if !reachedSpeed {
		t.Fatalf("prismatic motor never reached target speed ~90")
	}
	if tr := slider.JointTranslation(); !near(tr, 60, 0.5) {
		t.Fatalf("prismatic carriage did not stop at upper limit 60: tr=%v", tr)
	}

	// Reverse motor to hit lower limit.
	slider.MotorSpeed = -120
	for range 120 {
		rw.Step(dt)
		tr := slider.JointTranslation()
		if tr < slider.LowerTranslation-0.5 || tr > slider.UpperTranslation+0.5 {
			t.Fatalf("prismatic translation out of limits on reverse: tr=%v", tr)
		}
	}
	if tr := slider.JointTranslation(); !near(tr, -25, 0.5) {
		t.Fatalf("prismatic carriage did not stop at lower limit -25: tr=%v", tr)
	}
}

func TestWeldJointRigidAndSoftSpring(t *testing.T) {
	const dt = time.Second / 60

	// 1. Rigid weld holds a cantilevered bar horizontally under gravity.
	var rigidWorld physics.RigidWorld
	rigidWorld.GravityY = 600
	rigidWorld.SolverIterations = 12

	wall := physics.NewStaticRigidBox(100, 100, 16, 40)
	beam := physics.NewRigidBox(148, 100, 80, 16, 2)
	rigidWorld.AddBody(wall, beam)

	weld := physics.NewWeldJoint(wall, beam, core.Point{X: 108, Y: 100})
	rigidWorld.AddJoint(weld)

	for range 120 {
		rigidWorld.Step(dt)
		pA := wall.LocalToWorld(weld.AnchorA)
		pB := beam.LocalToWorld(weld.AnchorB)
		if err := float32(math.Hypot(float64(pB.X-pA.X), float64(pB.Y-pA.Y))); err > 0.5 {
			t.Fatalf("rigid weld anchor drifted: err=%v", err)
		}
		if math.Abs(float64(beam.Angle)) > 0.02 {
			t.Fatalf("rigid weld rotated under cantilever load: angle=%v", beam.Angle)
		}
	}
	fx, fy := weld.ReactionForce()
	if fy >= -500 {
		t.Fatalf("expected upward support reaction force on cantilevered weld, got (%v, %v)", fx, fy)
	}
	if tq := weld.ReactionTorque(); math.Abs(float64(tq)) < 1000 {
		t.Fatalf("expected substantial reaction torque on cantilevered weld, got %v", tq)
	}

	// 2. Soft weld (FrequencyHz + DampingRatio) flexes and oscillates softly under gravity.
	var softWorld physics.RigidWorld
	softWorld.GravityY = 600
	softWorld.SolverIterations = 12

	softWall := physics.NewStaticRigidBox(100, 100, 16, 40)
	softBeam := physics.NewRigidBox(148, 100, 80, 16, 2)
	softWorld.AddBody(softWall, softBeam)

	softWeld := physics.NewWeldJoint(softWall, softBeam, core.Point{X: 108, Y: 100})
	softWeld.FrequencyHz = 2.5
	softWeld.DampingRatio = 0.15
	softWorld.AddJoint(softWeld)

	maxAngle := float32(0)
	signChanges := 0
	prevAV := float32(0)
	for range 180 {
		softWorld.Step(dt)
		if math.IsNaN(float64(softBeam.Angle)) || math.IsInf(float64(softBeam.Angle), 0) {
			t.Fatalf("soft weld diverged")
		}
		if softBeam.Angle > maxAngle {
			maxAngle = softBeam.Angle
		}
		if prevAV > 0.05 && softBeam.AngularVel < -0.05 {
			signChanges++
		}
		if prevAV < -0.05 && softBeam.AngularVel > 0.05 {
			signChanges++
		}
		if math.Abs(float64(softBeam.AngularVel)) > 0.05 {
			prevAV = softBeam.AngularVel
		}
	}
	if maxAngle < 0.05 {
		t.Fatalf("soft weld did not flex noticeably: maxAngle=%v", maxAngle)
	}
	if signChanges < 2 {
		t.Fatalf("soft weld did not oscillate softly: angular velocity sign changes=%d", signChanges)
	}

	// 3. Soft weld with direct Stiffness and Damping also oscillates and remains stable.
	var directWorld physics.RigidWorld
	directWorld.SolverIterations = 12
	anchor := physics.NewStaticRigidBox(100, 100, 16, 16)
	block := physics.NewRigidBox(100, 100, 24, 24, 1)
	block.VY = 120
	directWorld.AddBody(anchor, block)
	directWeld := physics.NewWeldJoint(anchor, block, core.Point{X: 100, Y: 100})
	directWeld.Stiffness = 200
	directWeld.Damping = 4
	directWorld.AddJoint(directWeld)

	maxDisp := float32(0)
	for range 240 {
		directWorld.Step(dt)
		if d := float32(math.Abs(float64(block.Y - anchor.Y))); d > maxDisp {
			maxDisp = d
		}
	}
	if maxDisp < 3 {
		t.Fatalf("direct-stiffness soft weld did not deflect: maxDisp=%v", maxDisp)
	}
	if !near(block.Y, anchor.Y, 1.5) {
		t.Fatalf("direct-stiffness soft weld did not damp back toward rest pose: Y=%v want ~%v", block.Y, anchor.Y)
	}
}

func TestWheelJointTwoWheelCarDrivesOverUnevenGround(t *testing.T) {
	const dt = time.Second / 60

	var rw physics.RigidWorld
	rw.GravityY = 500
	rw.SolverIterations = 14

	// Uneven terrain: flat start -> gentle ramp up -> plateau -> ramp down -> bumpy flat finish.
	groundPieces := []*physics.RigidBody{
		physics.NewStaticRigidBox(80, 220, 240, 20),
		func() *physics.RigidBody {
			b := physics.NewStaticRigidBox(250, 206, 120, 20)
			b.Angle = -0.18
			return b
		}(),
		physics.NewStaticRigidBox(360, 194, 120, 20),
		func() *physics.RigidBody {
			b := physics.NewStaticRigidBox(470, 206, 120, 20)
			b.Angle = 0.18
			return b
		}(),
		physics.NewStaticRigidBox(900, 220, 780, 20),
		// Small speed bumps along the final flat stretch.
		physics.NewStaticRigidCircle(560, 214, 10),
		physics.NewStaticRigidCircle(615, 214, 10),
	}
	for _, g := range groundPieces {
		g.Friction = 1.2
		rw.AddBody(g)
	}

	chassis := physics.NewRigidBox(80, 178, 72, 18, 4)
	chassis.Friction = 0.6
	chassis.AngularDamping = 0.4
	rearWheel := physics.NewRigidCircle(54, 196, 12, 1)
	rearWheel.Friction = 1.4
	frontWheel := physics.NewRigidCircle(106, 196, 12, 1)
	frontWheel.Friction = 1.4
	rw.AddBody(chassis, rearWheel, frontWheel)

	rearSusp := physics.NewWheelJoint(chassis, rearWheel, core.Point{X: 54, Y: 196}, core.Point{X: 0, Y: 1})
	rearSusp.FrequencyHz = 4.5
	rearSusp.DampingRatio = 0.75
	rearSusp.EnableLimit = true
	rearSusp.LowerTranslation = -10
	rearSusp.UpperTranslation = 10
	rearSusp.EnableMotor = true
	rearSusp.MotorSpeed = 14
	rearSusp.MaxMotorTorque = 45000

	frontSusp := physics.NewWheelJoint(chassis, frontWheel, core.Point{X: 106, Y: 196}, core.Point{X: 0, Y: 1})
	frontSusp.FrequencyHz = 4.5
	frontSusp.DampingRatio = 0.75
	frontSusp.EnableLimit = true
	frontSusp.LowerTranslation = -10
	frontSusp.UpperTranslation = 10
	frontSusp.EnableMotor = true
	frontSusp.MotorSpeed = 14
	frontSusp.MaxMotorTorque = 45000

	rw.AddJoint(rearSusp, frontSusp)

	startX := chassis.X
	minTravel, maxTravel := float32(1e9), float32(-1e9)
	for step := range 360 {
		rw.Step(dt)
		if math.IsNaN(float64(chassis.X)) || math.IsNaN(float64(chassis.Y)) || math.IsNaN(float64(chassis.Angle)) {
			t.Fatalf("car state became NaN at step %d", step)
		}
		for _, wj := range []*physics.WheelJoint{rearSusp, frontSusp} {
			tr := wj.JointTranslation()
			if tr < wj.LowerTranslation-1.0 || tr > wj.UpperTranslation+1.0 {
				t.Fatalf("step %d: wheel suspension exceeded limits: tr=%v bounds=[%v, %v]",
					step, tr, wj.LowerTranslation, wj.UpperTranslation)
			}
			if tr < minTravel {
				minTravel = tr
			}
			if tr > maxTravel {
				maxTravel = tr
			}
		}
		if math.Abs(float64(chassis.Angle)) > 1.0 {
			t.Fatalf("step %d: car flipped over on uneven ground: angle=%v", step, chassis.Angle)
		}
	}

	if traveled := chassis.X - startX; traveled < 350 {
		t.Fatalf("car did not drive across uneven ground: traveled=%v px (chassis.X=%v)", traveled, chassis.X)
	}
	if maxTravel-minTravel < 1.0 {
		t.Fatalf("wheel suspension did not compress/extend over uneven ground: range=[%v, %v]", minTravel, maxTravel)
	}
}

func TestMouseJointDragsBodyTowardMovingTargetWithinMaxForce(t *testing.T) {
	const dt = time.Second / 60

	var rw physics.RigidWorld
	rw.GravityY = 300
	rw.SolverIterations = 12

	box := physics.NewRigidBox(100, 100, 28, 28, 2)
	rw.AddBody(box)

	const maxForce = float32(2500)
	mj := physics.NewMouseJoint(box, core.Point{X: 108, Y: 96}, maxForce)
	mj.FrequencyHz = 6
	mj.DampingRatio = 0.85
	rw.AddJoint(mj)

	target := core.Point{X: 108, Y: 96}
	for step := range 150 {
		if step < 60 {
			target.X += 3
			target.Y -= 1.5
			mj.SetTarget(target)
		}
		rw.Step(dt)
		fx, fy := mj.ReactionForce()
		mag := float32(math.Hypot(float64(fx), float64(fy)))
		if mag > maxForce*1.01 {
			t.Fatalf("step %d: MouseJoint exceeded MaxForce %v: got %v", step, maxForce, mag)
		}
	}

	p := box.LocalToWorld(mj.Anchor)
	dist := float32(math.Hypot(float64(p.X-target.X), float64(p.Y-target.Y)))
	if dist > 6.0 {
		t.Fatalf("MouseJoint did not pull body anchor to moving target: anchor=%+v target=%+v dist=%v", p, target, dist)
	}

	// Verify MaxForce clamp when target jumps very far away.
	mj.MaxForce = 400
	mj.SetTarget(core.Point{X: 2000, Y: 2000})
	rw.Step(dt)
	fx, fy := mj.ReactionForce()
	mag := float32(math.Hypot(float64(fx), float64(fy)))
	if mag > 400*1.01 || mag < 350 {
		t.Fatalf("expected MouseJoint force clamped near 400, got %v", mag)
	}
}

func TestDistanceJointRopeAndRangeMode(t *testing.T) {
	const dt = time.Second / 60

	// 1. Rope mode (MaxOnly) is slack inside max length and catches taut at max length.
	var rw physics.RigidWorld
	rw.GravityY = 0
	rw.SolverIterations = 12

	pivot := physics.NewStaticRigidBox(100, 50, 8, 8)
	bob := physics.NewRigidCircle(100, 80, 10, 1) // starts 30 px below pivot (inside max 60)
	bob.VY = -40                                  // moving closer to pivot
	rw.AddBody(pivot, bob)

	rope := physics.NewRopeJoint(pivot, bob, core.Point{}, core.Point{}, 60)
	rw.AddJoint(rope)

	for range 15 {
		rw.Step(dt)
		fx, fy := rope.ReactionForce()
		if fx != 0 || fy != 0 {
			t.Fatalf("slack rope applied non-zero force: (%v, %v)", fx, fy)
		}
	}
	if bob.Y >= 72 {
		t.Fatalf("bob did not move freely closer inside slack rope: Y=%v", bob.Y)
	}

	// Enable gravity and let bob fall past 60 px: rope must catch taut and hold <= 60 px.
	rw.GravityY = 600
	bob.VX = 40
	caught := false
	for range 150 {
		rw.Step(dt)
		dist := float32(math.Hypot(float64(bob.X-pivot.X), float64(bob.Y-pivot.Y)))
		if dist > 60.5 {
			t.Fatalf("rope stretched beyond max length 60: dist=%v", dist)
		}
		fx, fy := rope.ReactionForce()
		if math.Hypot(float64(fx), float64(fy)) > 100 {
			caught = true
		}
	}
	if !caught {
		t.Fatalf("falling bob never pulled rope taut")
	}

	// 2. Range mode [MinLength, MaxLength]: slack in middle, enforces both ends.
	var rw2 physics.RigidWorld
	rw2.SolverIterations = 12
	p := physics.NewStaticRigidBox(100, 100, 8, 8)
	m := physics.NewRigidCircle(160, 100, 10, 1) // dist = 60 in [40, 80]
	rw2.AddBody(p, m)

	rangeJoint := physics.NewDistanceJoint(p, m, core.Point{}, core.Point{}, 0)
	rangeJoint.MinLength = 40
	rangeJoint.MaxLength = 80
	rw2.AddJoint(rangeJoint)

	m.VX = -200 // push inward toward MinLength=40
	for range 60 {
		rw2.Step(dt)
		dist := float32(math.Hypot(float64(m.X-p.X), float64(m.Y-p.Y)))
		if dist < 39.5 {
			t.Fatalf("range distance joint compressed below MinLength 40: dist=%v", dist)
		}
	}
	m.VX = 250 // push outward toward MaxLength=80
	for range 60 {
		rw2.Step(dt)
		dist := float32(math.Hypot(float64(m.X-p.X), float64(m.Y-p.Y)))
		if dist > 80.5 {
			t.Fatalf("range distance joint extended past MaxLength 80: dist=%v", dist)
		}
	}
}

func TestBreakableChainSeparatesAndReportsEvent(t *testing.T) {
	const dt = time.Second / 60

	var rw physics.RigidWorld
	rw.GravityY = 600
	rw.SolverIterations = 12

	ceiling := physics.NewStaticRigidBox(100, 20, 40, 10)
	link1 := physics.NewRigidBox(100, 50, 10, 40, 1)
	link2 := physics.NewRigidBox(100, 90, 10, 40, 1)
	weight := physics.NewRigidBox(100, 130, 24, 24, 1)
	rw.AddBody(ceiling, link1, link2, weight)

	// Under light load (total mass 3, weight = 1800), BreakForce 2500 should hold.
	jTop := physics.NewRevoluteJoint(ceiling, link1, core.Point{X: 100, Y: 30})
	jTop.BreakForce = 2500
	jMid := physics.NewRevoluteJoint(link1, link2, core.Point{X: 100, Y: 70})
	jMid.BreakForce = 5000
	jBot := physics.NewRevoluteJoint(link2, weight, core.Point{X: 100, Y: 110})
	jBot.BreakForce = 5000
	rw.AddJoint(jTop, jMid, jBot)

	var callbackEvents []physics.JointBreakEvent
	rw.OnJointBreak = func(ev physics.JointBreakEvent) {
		callbackEvents = append(callbackEvents, ev)
	}

	for range 30 {
		rw.Step(dt)
		if len(rw.JointBreakEvents()) != 0 {
			t.Fatalf("chain broke under safe load: %+v", rw.JointBreakEvents())
		}
	}
	if jTop.Broken() {
		t.Fatalf("top joint unexpectedly marked broken under safe load")
	}

	// Drop a heavy load onto the bottom of the chain so top link exceeds BreakForce 2500.
	weight.ApplyForce(0, 6000)
	var stepEvents []physics.JointBreakEvent
	for range 30 {
		weight.ApplyForce(0, 6000)
		rw.Step(dt)
		stepEvents = append(stepEvents, rw.JointBreakEvents()...)
		if jTop.Broken() {
			break
		}
	}

	if !jTop.Broken() {
		t.Fatalf("expected top chain joint to break past BreakForce")
	}
	if len(stepEvents) == 0 || len(callbackEvents) != len(stepEvents) {
		t.Fatalf("break event mismatch: stepEvents=%d callbackEvents=%d", len(stepEvents), len(callbackEvents))
	}
	ev := stepEvents[0]
	if ev.Joint != jTop || ev.BodyA != ceiling || ev.BodyB != link1 {
		t.Fatalf("unexpected JointBreakEvent target: %+v", ev)
	}
	if mag := float32(math.Hypot(float64(ev.ForceX), float64(ev.ForceY))); mag <= jTop.BreakForce {
		t.Fatalf("reported break force %v did not exceed BreakForce %v", mag, jTop.BreakForce)
	}
	for _, remaining := range rw.Joints {
		if remaining == jTop {
			t.Fatalf("broken joint was not removed from RigidWorld.Joints")
		}
	}

	// Next step with no new breaks should clear JointBreakEvents() and allow chain to fall freely.
	yAfterBreak := link1.Y
	for range 30 {
		rw.Step(dt)
	}
	if len(rw.JointBreakEvents()) != 0 {
		t.Fatalf("expected empty JointBreakEvents after break cleared, got %d", len(rw.JointBreakEvents()))
	}
	if link1.Y <= yAfterBreak+40 {
		t.Fatalf("detached chain did not fall freely after break: yAfterBreak=%v finalY=%v", yAfterBreak, link1.Y)
	}
}

func TestBreakableJointTorqueThreshold(t *testing.T) {
	const dt = time.Second / 60

	var rw physics.RigidWorld
	rw.GravityY = 800
	rw.SolverIterations = 12

	wall := physics.NewStaticRigidBox(100, 100, 16, 40)
	beam := physics.NewRigidBox(160, 100, 100, 14, 3)
	rw.AddBody(wall, beam)

	// Cantilever torque is roughly m*g*L/2 = 3*800*50 = 120000; threshold 20000 must break cleanly.
	weld := physics.NewWeldJoint(wall, beam, core.Point{X: 110, Y: 100})
	weld.BreakTorque = 20000
	rw.AddJoint(weld)

	rw.Step(dt)
	if !weld.Broken() {
		t.Fatalf("expected weld joint to break on BreakTorque, torque=%v", weld.ReactionTorque())
	}
	evs := rw.JointBreakEvents()
	if len(evs) != 1 || evs[0].Joint != weld || math.Abs(float64(evs[0].Torque)) <= float64(weld.BreakTorque) {
		t.Fatalf("unexpected torque break events: %+v", evs)
	}
}

func buildAllJointsWorld() (*physics.RigidWorld, *physics.MouseJoint) {
	rw := &physics.RigidWorld{
		GravityY:         420,
		SolverIterations: 12,
	}

	ground := physics.NewStaticRigidBox(250, 320, 520, 24)
	ground.ID = 1
	ground.Friction = 1.0

	anchor := physics.NewStaticRigidBox(80, 60, 10, 10)
	anchor.ID = 2
	ropeBob := physics.NewRigidCircle(115, 95, 10, 1)
	ropeBob.ID = 3

	revHub := physics.NewStaticRigidBox(160, 60, 10, 10)
	revHub.ID = 4
	revBar := physics.NewRigidBox(190, 60, 60, 10, 1.5)
	revBar.ID = 5

	rail := physics.NewStaticRigidBox(260, 60, 12, 12)
	rail.ID = 6
	slider := physics.NewRigidBox(260, 60, 20, 14, 1)
	slider.ID = 7

	weldWall := physics.NewStaticRigidBox(340, 60, 12, 24)
	weldWall.ID = 8
	weldArm := physics.NewRigidBox(375, 60, 56, 12, 1.2)
	weldArm.ID = 9

	chassis := physics.NewRigidBox(120, 270, 60, 16, 3)
	chassis.ID = 10
	wheelL := physics.NewRigidCircle(98, 286, 11, 1)
	wheelL.ID = 11
	wheelL.Friction = 1.2
	wheelR := physics.NewRigidCircle(142, 286, 11, 1)
	wheelR.ID = 12
	wheelR.Friction = 1.2

	dragged := physics.NewRigidBox(410, 180, 22, 22, 1.5)
	dragged.ID = 13

	breakAnchor := physics.NewStaticRigidBox(450, 40, 10, 10)
	breakAnchor.ID = 14
	breakWeight := physics.NewRigidBox(450, 80, 16, 16, 4)
	breakWeight.ID = 15

	rw.AddBody(
		ground, anchor, ropeBob, revHub, revBar, rail, slider,
		weldWall, weldArm, chassis, wheelL, wheelR, dragged, breakAnchor, breakWeight,
	)

	rope := physics.NewRopeJoint(anchor, ropeBob, core.Point{}, core.Point{}, 70)
	rev := physics.NewRevoluteJoint(revHub, revBar, core.Point{X: 160, Y: 60})
	rev.EnableLimit = true
	rev.LowerAngle = -0.5
	rev.UpperAngle = 0.6
	rev.EnableMotor = true
	rev.MotorSpeed = 3.0
	rev.MaxMotorTorque = 15000

	pris := physics.NewPrismaticJoint(rail, slider, core.Point{X: 260, Y: 60}, core.Point{X: 1, Y: 0.25})
	pris.EnableLimit = true
	pris.LowerTranslation = -15
	pris.UpperTranslation = 45
	pris.EnableMotor = true
	pris.MotorSpeed = 50
	pris.MaxMotorForce = 4000

	weld := physics.NewWeldJoint(weldWall, weldArm, core.Point{X: 346, Y: 60})
	weld.FrequencyHz = 3.5
	weld.DampingRatio = 0.25

	wL := physics.NewWheelJoint(chassis, wheelL, core.Point{X: 98, Y: 286}, core.Point{X: 0, Y: 1})
	wL.FrequencyHz = 4.5
	wL.DampingRatio = 0.7
	wL.EnableLimit = true
	wL.LowerTranslation = -8
	wL.UpperTranslation = 8
	wL.EnableMotor = true
	wL.MotorSpeed = 10
	wL.MaxMotorTorque = 25000

	wR := physics.NewWheelJoint(chassis, wheelR, core.Point{X: 142, Y: 286}, core.Point{X: 0, Y: 1})
	wR.FrequencyHz = 4.5
	wR.DampingRatio = 0.7
	wR.EnableLimit = true
	wR.LowerTranslation = -8
	wR.UpperTranslation = 8
	wR.EnableMotor = true
	wR.MotorSpeed = 10
	wR.MaxMotorTorque = 25000

	mouse := physics.NewMouseJoint(dragged, core.Point{X: 414, Y: 176}, 3000)

	breakable := physics.NewDistanceJoint(breakAnchor, breakWeight, core.Point{}, core.Point{}, 40)
	breakable.BreakForce = 2200

	rw.AddJoint(rope, rev, pris, weld, wL, wR, mouse, breakable)
	return rw, mouse
}

func TestAllJointTypesDeterminismAndSnapshotRoundTrip(t *testing.T) {
	const dt = time.Second / 60

	stepWorld := func(rw *physics.RigidWorld, mj *physics.MouseJoint, step int) {
		if mj != nil {
			mj.SetTarget(core.Point{
				X: core.Points(414 + float32(float32(step)*0.6)),
				Y: core.Points(176 - float32(float32(step%20)*0.5)),
			})
		}
		if step == 55 {
			// Apply downward yank on breakWeight (ID 15, index 14) to break the breakable joint.
			rw.Bodies[14].ApplyForce(0, 5000)
		}
		rw.Step(dt)
	}

	// 1. Two independent runs must be bit-identical.
	rw1, mj1 := buildAllJointsWorld()
	rw2, mj2 := buildAllJointsWorld()
	for step := 0; step < 120; step++ {
		stepWorld(rw1, mj1, step)
		stepWorld(rw2, mj2, step)
	}
	if rw1.Checksum() != rw2.Checksum() {
		t.Fatalf("non-deterministic simulation across runs: %#016x != %#016x", rw1.Checksum(), rw2.Checksum())
	}

	// 2. Save at step 56 (immediately after joint break event), JSON round-trip, restore, and re-step to 120.
	rw, mj := buildAllJointsWorld()
	callbackCount := 0
	rw.OnJointBreak = func(ev physics.JointBreakEvent) {
		callbackCount++
	}
	for step := 0; step <= 55; step++ {
		stepWorld(rw, mj, step)
	}
	if len(rw.JointBreakEvents()) != 1 || callbackCount != 1 {
		t.Fatalf("expected 1 joint break event at step 55, got events=%d callbacks=%d",
			len(rw.JointBreakEvents()), callbackCount)
	}

	snap := rw.Save()
	snapHash := rw.Checksum()
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded physics.RigidWorldSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Continue original world to step 120.
	for step := 56; step < 120; step++ {
		stepWorld(rw, mj, step)
	}
	wantHash := rw.Checksum()
	if wantHash != rw1.Checksum() {
		t.Fatalf("world hash diverged from reference run: %#016x != %#016x", wantHash, rw1.Checksum())
	}

	// Restore in place and verify bit-identical hash, preserved break event, and continuation.
	if err := rw.Restore(decoded); err != nil {
		t.Fatalf("in-place Restore: %v", err)
	}
	if rw.Checksum() != snapHash {
		t.Fatalf("restored hash = %#016x, want %#016x", rw.Checksum(), snapHash)
	}
	if len(rw.JointBreakEvents()) != 1 {
		t.Fatalf("restored JointBreakEvents len = %d, want 1", len(rw.JointBreakEvents()))
	}
	var restoredMouse *physics.MouseJoint
	for _, j := range rw.Joints {
		if m, ok := j.(*physics.MouseJoint); ok {
			restoredMouse = m
		}
	}
	if restoredMouse != mj {
		t.Fatalf("MouseJoint pointer not preserved across in-place Restore")
	}
	for step := 56; step < 120; step++ {
		stepWorld(rw, restoredMouse, step)
	}
	if rw.Checksum() != wantHash {
		t.Fatalf("in-place continuation hash = %#016x, want %#016x", rw.Checksum(), wantHash)
	}

	// Restore into a fresh RigidWorld and verify bit-identical continuation.
	fresh := &physics.RigidWorld{}
	if err := fresh.Restore(decoded); err != nil {
		t.Fatalf("fresh Restore: %v", err)
	}
	if fresh.Checksum() != snapHash {
		t.Fatalf("fresh restored hash = %#016x, want %#016x", fresh.Checksum(), snapHash)
	}
	var freshMouse *physics.MouseJoint
	for _, j := range fresh.Joints {
		if m, ok := j.(*physics.MouseJoint); ok {
			freshMouse = m
		}
	}
	for step := 56; step < 120; step++ {
		stepWorld(fresh, freshMouse, step)
	}
	if fresh.Checksum() != wantHash {
		t.Fatalf("fresh continuation hash = %#016x, want %#016x", fresh.Checksum(), wantHash)
	}
}
