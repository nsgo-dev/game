package physics_test

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
)

func buildTestWorld2D() (*physics.World, []*physics.Body, *[]string) {
	grid := physics.NewGrid(20, 14, 16, 16)
	grid.FillRect(0, 12, 20, 2, physics.TileSolid)
	grid.FillRect(0, 0, 1, 14, physics.TileSolid)
	grid.FillRect(19, 0, 1, 14, physics.TileSolid)
	grid.FillRect(6, 8, 6, 1, physics.TileOneWay)

	w := &physics.World{
		Tiles:    grid,
		GravityY: 600,
	}

	b1 := physics.NewBoxBody(40, 40, 14, 18)
	b1.ID = 1
	b1.Tag = "player"
	b1.VX = 75

	b2 := physics.NewCircleBody(180, 50, 8)
	b2.ID = 2
	b2.Tag = "orb"
	b2.VX = -55

	b3 := physics.NewBoxBody(110, 150, 16, 16)
	b3.ID = 3
	b3.Tag = "crate"
	b3.VX = 30

	w.Add(b1, b2, b3)

	var log []string
	trZone := &physics.Trigger{
		ID:    10,
		Tag:   "zone",
		Shape: physics.BoxShape(physics.Box{X: 90, Y: 100, W: 90, H: 92}),
		Area: physics.AreaSettings{
			GravityMode:     physics.AreaOverrideAdd,
			GravityY:        -200,
			FluidLinearDrag: 1.5,
		},
		OnEnter: func(b *physics.Body) { log = append(log, fmt.Sprintf("enter:zone:%d", b.ID)) },
		OnStay:  func(b *physics.Body) { log = append(log, fmt.Sprintf("stay:zone:%d", b.ID)) },
		OnExit:  func(b *physics.Body) { log = append(log, fmt.Sprintf("exit:zone:%d", b.ID)) },
	}
	trGoal := &physics.Trigger{
		ID:    11,
		Tag:   "goal",
		Shape: physics.CircleShape(physics.Circle{X: 140, Y: 170, R: 28}),
		OnEnter: func(b *physics.Body) {
			log = append(log, fmt.Sprintf("enter:goal:%d", b.ID))
		},
		OnExit: func(b *physics.Body) {
			log = append(log, fmt.Sprintf("exit:goal:%d", b.ID))
		},
	}
	w.AddTrigger(trZone, trGoal)

	return w, []*physics.Body{b1, b2, b3}, &log
}

func stepTestWorld2D(w *physics.World, step int) {
	const dt = time.Second / 60
	dtSec := float32(dt.Seconds())
	for i, b := range w.Bodies {
		if b.Grounded && (step+i*17)%45 == 0 {
			b.VY = -240
		}
		if b.OnRightWall {
			b.VX = -60
		} else if b.OnLeftWall {
			b.VX = 60
		}
		b.VY += float32(w.GravityY * dtSec)
		physics.MoveAndSlide(b, w.Tiles, dt)
	}
	w.StepTriggers()
}

func TestWorldSaveRestoreBitIdentical(t *testing.T) {
	w, bodies, log := buildTestWorld2D()
	for step := 0; step < 100; step++ {
		stepTestWorld2D(w, step)
	}

	snap := w.Save()
	snapChecksum := w.Checksum()

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal(WorldSnapshot): %v", err)
	}
	var decoded physics.WorldSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(WorldSnapshot): %v", err)
	}

	// Step original from 100 to 200.
	*log = nil
	for step := 100; step < 200; step++ {
		stepTestWorld2D(w, step)
	}
	wantChecksum := w.Checksum()
	wantEvents := append([]string(nil), (*log)...)
	wantBodies := make([]physics.Body, len(w.Bodies))
	for i, b := range w.Bodies {
		wantBodies[i] = *b
	}

	// Restore in place from JSON-round-tripped snapshot and verify pointers + callbacks preserved.
	if err := w.Restore(decoded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if w.Checksum() != snapChecksum {
		t.Fatalf("restored checksum = %#x, want %#x", w.Checksum(), snapChecksum)
	}
	for i, orig := range bodies {
		if w.Bodies[i] != orig {
			t.Fatalf("body[%d] pointer was not preserved across in-place Restore", i)
		}
	}

	*log = nil
	for step := 100; step < 200; step++ {
		stepTestWorld2D(w, step)
	}
	if w.Checksum() != wantChecksum {
		t.Fatalf("step 200 checksum after restore = %#x, want %#x", w.Checksum(), wantChecksum)
	}
	if len(*log) != len(wantEvents) {
		t.Fatalf("event count = %d, want %d", len(*log), len(wantEvents))
	}
	for i := range wantEvents {
		if (*log)[i] != wantEvents[i] {
			t.Fatalf("event[%d] = %q, want %q", i, (*log)[i], wantEvents[i])
		}
	}
	for i, wb := range wantBodies {
		gb := *w.Bodies[i]
		if gb.Shape != wb.Shape ||
			math.Float32bits(gb.VX) != math.Float32bits(wb.VX) ||
			math.Float32bits(gb.VY) != math.Float32bits(wb.VY) ||
			gb.Grounded != wb.Grounded || gb.OnLeftWall != wb.OnLeftWall || gb.OnRightWall != wb.OnRightWall {
			t.Fatalf("body[%d] mismatch:\ngot  %+v\nwant %+v", i, gb, wb)
		}
	}

	// Also restore into a brand new empty World.
	fresh := &physics.World{}
	if err := fresh.Restore(decoded); err != nil {
		t.Fatalf("fresh.Restore: %v", err)
	}
	if fresh.Checksum() != snapChecksum {
		t.Fatalf("fresh restored checksum = %#x, want %#x", fresh.Checksum(), snapChecksum)
	}
	for step := 100; step < 200; step++ {
		stepTestWorld2D(fresh, step)
	}
	if fresh.Checksum() != wantChecksum {
		t.Fatalf("fresh step 200 checksum = %#x, want %#x", fresh.Checksum(), wantChecksum)
	}
}

func buildTestRigidWorld2D() *physics.RigidWorld {
	rw := &physics.RigidWorld{
		GravityY:         480,
		SolverIterations: 10,
	}

	floor := physics.NewStaticRigidBox(200, 300, 420, 24)
	floor.ID = 1
	leftWall := physics.NewStaticRigidBox(-10, 150, 20, 320)
	leftWall.ID = 2
	rightWall := physics.NewStaticRigidBox(410, 150, 20, 320)
	rightWall.ID = 3
	pivot := physics.NewStaticRigidBox(290, 80, 10, 10)
	pivot.ID = 4

	boxA := physics.NewRigidBox(120, 220, 32, 32, 2.0)
	boxA.ID = 5
	boxA.Angle = 0.25
	boxA.Restitution = 0.2
	boxA.Friction = 0.5
	boxA.LinearDamping = 0.05
	boxA.AngularDamping = 0.05

	boxB := physics.NewRigidBox(128, 175, 28, 28, 1.5)
	boxB.ID = 6
	boxB.Angle = -0.15
	boxB.Restitution = 0.25

	ball := physics.NewRigidCircle(70, 160, 14, 1.2)
	ball.ID = 7
	ball.VX = 140
	ball.Restitution = 0.6

	bob := physics.NewRigidCircle(340, 110, 12, 1.0)
	bob.ID = 8

	springBox := physics.NewRigidBox(210, 140, 24, 24, 1.0)
	springBox.ID = 9

	arm := physics.NewRigidBox(210, 190, 40, 12, 1.0)
	arm.ID = 10

	rw.AddBody(floor, leftWall, rightWall, pivot, boxA, boxB, ball, bob, springBox, arm)

	distJoint := physics.NewDistanceJoint(pivot, bob, core.Point{}, core.Point{}, 65)
	distJoint.Compliance = 0.001
	distJoint.Damping = 0.1
	springJoint := physics.NewSpringJoint(pivot, springBox, core.Point{}, core.Point{}, 90, 40, 3)
	revJoint := physics.NewRevoluteJoint(springBox, arm, core.Point{X: 210, Y: 165})
	rw.AddJoint(distJoint, springJoint, revJoint)

	water := &physics.Trigger{
		ID:    20,
		Tag:   "fluid",
		Shape: physics.BoxShape(physics.Box{X: 0, Y: 230, W: 400, H: 60}),
		Area: physics.AreaSettings{
			FluidDensity:     0.004,
			FluidLinearDrag:  2.0,
			FluidAngularDrag: 1.0,
			FluidVelX:        15,
		},
	}
	rw.AddTrigger(water)
	return rw
}

func TestRigidWorldSaveRestoreBitIdentical(t *testing.T) {
	const dt = time.Second / 60
	rw := buildTestRigidWorld2D()

	for step := 0; step < 100; step++ {
		if step == 30 {
			rw.Bodies[4].ApplyForce(350, -120)
			rw.Bodies[4].ApplyTorque(900)
		}
		rw.Step(dt)
	}
	// Apply pending force/torque right before Save to ensure private accumulators round-trip.
	rw.Bodies[5].ApplyForce(-220, 80)
	rw.Bodies[5].ApplyTorque(-450)

	snap := rw.Save()
	snapChecksum := rw.Checksum()

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal(RigidWorldSnapshot): %v", err)
	}
	var decoded physics.RigidWorldSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(RigidWorldSnapshot): %v", err)
	}

	origPtrs := append([]*physics.RigidBody(nil), rw.Bodies...)

	// Step from 100 to 200.
	for step := 100; step < 200; step++ {
		if step == 140 {
			rw.Bodies[6].ApplyImpulse(45, -30)
		}
		rw.Step(dt)
	}
	wantChecksum := rw.Checksum()
	wantBodies := make([]physics.RigidBody, len(rw.Bodies))
	for i, b := range rw.Bodies {
		wantBodies[i] = *b
	}

	// Restore in place and re-step 100..200.
	if err := rw.Restore(decoded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if rw.Checksum() != snapChecksum {
		t.Fatalf("restored checksum = %#x, want %#x", rw.Checksum(), snapChecksum)
	}
	for i, p := range origPtrs {
		if rw.Bodies[i] != p {
			t.Fatalf("RigidBody[%d] pointer not preserved across Restore", i)
		}
	}
	for step := 100; step < 200; step++ {
		if step == 140 {
			rw.Bodies[6].ApplyImpulse(45, -30)
		}
		rw.Step(dt)
	}
	if rw.Checksum() != wantChecksum {
		t.Fatalf("step 200 checksum after restore = %#x, want %#x", rw.Checksum(), wantChecksum)
	}
	for i, wb := range wantBodies {
		gb := *rw.Bodies[i]
		if math.Float32bits(gb.X) != math.Float32bits(wb.X) ||
			math.Float32bits(gb.Y) != math.Float32bits(wb.Y) ||
			math.Float32bits(gb.VX) != math.Float32bits(wb.VX) ||
			math.Float32bits(gb.VY) != math.Float32bits(wb.VY) ||
			math.Float32bits(gb.Angle) != math.Float32bits(wb.Angle) ||
			math.Float32bits(gb.AngularVel) != math.Float32bits(wb.AngularVel) ||
			gb.Shape != wb.Shape {
			t.Fatalf("RigidBody[%d] diverged:\ngot  %+v\nwant %+v", i, gb, wb)
		}
	}

	// Restore into a brand-new RigidWorld from JSON and verify bit-identical trajectory.
	fresh := &physics.RigidWorld{}
	if err := fresh.Restore(decoded); err != nil {
		t.Fatalf("fresh.Restore: %v", err)
	}
	for step := 100; step < 200; step++ {
		if step == 140 {
			fresh.Bodies[6].ApplyImpulse(45, -30)
		}
		fresh.Step(dt)
	}
	if fresh.Checksum() != wantChecksum {
		t.Fatalf("fresh step 200 checksum = %#x, want %#x", fresh.Checksum(), wantChecksum)
	}
}

func TestSnapshotValidation(t *testing.T) {
	var nilWorld *physics.World
	if err := nilWorld.Restore(physics.WorldSnapshot{Version: physics.SnapshotVersion}); err == nil {
		t.Fatal("expected error restoring nil World")
	}
	w := &physics.World{}
	if err := w.Restore(physics.WorldSnapshot{Version: 99}); err == nil {
		t.Fatal("expected error for unsupported WorldSnapshot version")
	}
	if err := w.Restore(physics.WorldSnapshot{
		Version:       physics.SnapshotVersion,
		TriggerActive: []physics.WorldTriggerActiveSnapshot{{TriggerIndex: 5}},
	}); err == nil {
		t.Fatal("expected error for out-of-range trigger index")
	}

	var nilRigid *physics.RigidWorld
	if err := nilRigid.Restore(physics.RigidWorldSnapshot{Version: physics.SnapshotVersion}); err == nil {
		t.Fatal("expected error restoring nil RigidWorld")
	}
	rw := &physics.RigidWorld{}
	if err := rw.Restore(physics.RigidWorldSnapshot{Version: 99}); err == nil {
		t.Fatal("expected error for unsupported RigidWorldSnapshot version")
	}
	if err := rw.Restore(physics.RigidWorldSnapshot{
		Version: physics.SnapshotVersion,
		Joints:  []physics.RigidJointSnapshot{{Kind: "unknown"}},
	}); err == nil {
		t.Fatal("expected error for unknown joint kind")
	}
}

func TestGoldenTrajectory2D(t *testing.T) {
	// Cross-architecture golden trajectory hashes (must match identically on amd64 and arm64).
	const (
		wantWorldGolden uint64 = 0x1798e96d103baf55
		wantRigidGolden uint64 = 0xa61a95947c7d0dfc
	)

	w, _, _ := buildTestWorld2D()
	for step := 0; step < 200; step++ {
		stepTestWorld2D(w, step)
	}
	gotWorld := w.Checksum()
	if gotWorld != wantWorldGolden {
		t.Fatalf("physics.World golden checksum = %#016x, want %#016x", gotWorld, wantWorldGolden)
	}

	const dt = time.Second / 60
	rw := buildTestRigidWorld2D()
	for step := 0; step < 200; step++ {
		if step == 30 {
			rw.Bodies[4].ApplyForce(350, -120)
			rw.Bodies[4].ApplyTorque(900)
		}
		if step == 120 {
			rw.Bodies[6].ApplyImpulse(45, -30)
		}
		rw.Step(dt)
	}
	gotRigid := rw.Checksum()
	if gotRigid != wantRigidGolden {
		t.Fatalf("physics.RigidWorld golden checksum = %#016x, want %#016x", gotRigid, wantRigidGolden)
	}
}

func buildConvexRigidWorld2D() *physics.RigidWorld {
	rw := &physics.RigidWorld{GravityY: 500}
	floor := physics.NewStaticRigidBody(physics.ChainShape([]core.Point{{X: 0, Y: 200}, {X: 120, Y: 220}, {X: 240, Y: 200}, {X: 360, Y: 230}}, false))
	floor.ID = 1
	ramp := physics.NewStaticRigidBody(physics.SegmentShape(physics.Segment{X1: 250, Y1: 120, X2: 340, Y2: 170}))
	ramp.ID = 2
	hex, ok := physics.NewPolygon([]core.Point{{X: 40, Y: 40}, {X: 60, Y: 30}, {X: 80, Y: 40}, {X: 80, Y: 60}, {X: 60, Y: 70}, {X: 40, Y: 60}}, 1)
	if !ok {
		panic("NewPolygon rejected the hexagon")
	}
	poly := physics.NewRigidBody(physics.PolygonShape(hex), 2)
	poly.ID = 3
	poly.VX = 40
	poly.AngularVel = 1.5
	capsule := physics.NewRigidBody(physics.CapsuleShape(physics.Capsule{X1: 150, Y1: 60, X2: 190, Y2: 70, R: 8}), 1.5)
	capsule.ID = 4
	capsule.VX = -30
	tilted := physics.NewRigidBody(physics.PolygonShape(physics.NewBoxPolygon(290, 60, 24, 14, 0.4)), 1)
	tilted.ID = 5
	rw.AddBody(floor, ramp, poly, capsule, tilted)
	return rw
}

func TestRigidWorldSaveRestoreConvexShapes(t *testing.T) {
	const dt = time.Second / 60
	rw := buildConvexRigidWorld2D()
	for range 90 {
		rw.Step(dt)
	}
	raw, err := json.Marshal(rw.Save())
	if err != nil {
		t.Fatalf("json.Marshal(RigidWorldSnapshot): %v", err)
	}
	snapChecksum := rw.Checksum()
	for range 120 {
		rw.Step(dt)
	}
	want := rw.Checksum()

	for _, name := range []string{"in place", "fresh"} {
		var decoded physics.RigidWorldSnapshot
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("json.Unmarshal(RigidWorldSnapshot): %v", err)
		}
		target := rw
		if name == "fresh" {
			target = &physics.RigidWorld{}
		}
		if err := target.Restore(decoded); err != nil {
			t.Fatalf("%s Restore: %v", name, err)
		}
		if got := target.Checksum(); got != snapChecksum {
			t.Fatalf("%s restored checksum = %#x, want %#x", name, got, snapChecksum)
		}
		for range 120 {
			target.Step(dt)
		}
		if got := target.Checksum(); got != want {
			t.Fatalf("%s checksum after re-stepping = %#x, want %#x", name, got, want)
		}
	}
}

func TestRigidWorldSaveDoesNotShareChainPoints(t *testing.T) {
	rw := buildConvexRigidWorld2D()
	snap := rw.Save()
	rw.Bodies[0].Shape.Chain.Points[0].Y = 999
	if got := snap.Bodies[0].Shape.Chain.Points[0].Y; got == 999 {
		t.Fatal("snapshot chain shares points with the live body")
	}
}
