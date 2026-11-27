package physics_test

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics"
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
	for _, v := range []int{0, physics.SnapshotVersion + 1, 99} {
		if err := w.Restore(physics.WorldSnapshot{Version: v}); err == nil {
			t.Fatalf("expected error for unsupported WorldSnapshot version %d", v)
		}
	}
	// Version 1, 2 and 3 snapshots still restore cleanly.
	for _, oldVer := range []int{1, 2, 3} {
		if err := w.Restore(physics.WorldSnapshot{Version: oldVer}); err != nil {
			t.Fatalf("restoring a version %d WorldSnapshot: %v", oldVer, err)
		}
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
	for _, v := range []int{0, physics.SnapshotVersion + 1, 99} {
		if err := rw.Restore(physics.RigidWorldSnapshot{Version: v}); err == nil {
			t.Fatalf("expected error for unsupported RigidWorldSnapshot version %d", v)
		}
	}
	for _, oldVer := range []int{1, 2, 3} {
		if err := rw.Restore(physics.RigidWorldSnapshot{Version: oldVer}); err != nil {
			t.Fatalf("restoring a version %d RigidWorldSnapshot: %v", oldVer, err)
		}
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
		wantRigidGolden uint64 = 0xbdc9de9e5f0d60de
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

func roundTripWorldSnapshot(t *testing.T, snap physics.WorldSnapshot) physics.WorldSnapshot {
	t.Helper()
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal(WorldSnapshot): %v", err)
	}
	var decoded physics.WorldSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(WorldSnapshot): %v", err)
	}
	return decoded
}

func roundTripRigidWorldSnapshot(t *testing.T, snap physics.RigidWorldSnapshot) physics.RigidWorldSnapshot {
	t.Helper()
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal(RigidWorldSnapshot): %v", err)
	}
	var decoded physics.RigidWorldSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(RigidWorldSnapshot): %v", err)
	}
	return decoded
}

func TestWorldRestoreIdentityAfterRemoveAndAdd(t *testing.T) {
	type bodyExpect struct {
		x, vx float32
		label string
	}
	want := []bodyExpect{
		{x: 10, vx: 100, label: "A"},
		{x: 50, vx: 200, label: "B"},
		{x: 90, vx: 300, label: "C"},
	}

	for _, explicitIDs := range []bool{false, true} {
		mode := "defaultID0"
		if explicitIDs {
			mode = "explicitID"
		}
		for _, tc := range []struct {
			name      string
			removeIdx int // -1 for none
			addNew    bool
			readdSame bool
		}{
			{name: "removeFirst", removeIdx: 0},
			{name: "removeMiddle", removeIdx: 1},
			{name: "removeLast", removeIdx: 2},
			{name: "addOnly", removeIdx: -1, addNew: true},
			{name: "removeFirstAndAdd", removeIdx: 0, addNew: true},
			{name: "removeMiddleAndAdd", removeIdx: 1, addNew: true},
			{name: "removeAndReaddFirst", removeIdx: 0, readdSame: true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				w := &physics.World{GravityY: 400}
				orig := make([]*physics.Body, len(want))
				for i, e := range want {
					b := physics.NewBoxBody(e.x, 20, 12, 12)
					b.VX = e.vx
					b.Tag = e.label
					b.UserData = e.label
					if explicitIDs {
						b.ID = i + 1
					}
					orig[i] = b
				}
				w.Add(orig...)

				snap := roundTripWorldSnapshot(t, w.Save())
				wantChecksum := w.Checksum()

				// Mutate surviving bodies to confirm Restore reverts them.
				for _, b := range orig {
					b.Translate(500, 0)
					b.VX = -999
				}

				if tc.removeIdx >= 0 {
					w.Remove(orig[tc.removeIdx])
				}
				var added *physics.Body
				if tc.addNew {
					added = physics.NewBoxBody(777, 77, 10, 10)
					added.VX = 777
					added.UserData = "D"
					if explicitIDs {
						added.ID = 99
					}
					w.Add(added)
				}
				if tc.readdSame && tc.removeIdx >= 0 {
					w.Add(orig[tc.removeIdx])
				}

				if err := w.Restore(snap); err != nil {
					t.Fatalf("Restore: %v", err)
				}
				if got := w.Checksum(); got != wantChecksum {
					t.Fatalf("Checksum = %#016x, want %#016x", got, wantChecksum)
				}
				if len(w.Bodies) != len(want) {
					t.Fatalf("len(w.Bodies) = %d, want %d", len(w.Bodies), len(want))
				}

				for i, e := range want {
					got := w.Bodies[i]
					if got.Shape.Box.X != e.x || got.VX != e.vx || got.Tag != e.label {
						t.Fatalf("body[%d] state = (x=%v, vx=%v, tag=%q), want (x=%v, vx=%v, tag=%q)",
							i, got.Shape.Box.X, got.VX, got.Tag, e.x, e.vx, e.label)
					}
					wasRemoved := tc.removeIdx == i && !tc.readdSame
					if wasRemoved {
						if got == orig[i] {
							t.Fatalf("body[%d] (%s) unexpectedly reused removed pointer", i, e.label)
						}
						if got.UserData != nil {
							t.Fatalf("body[%d] (%s) recreated body UserData = %v, want nil", i, e.label, got.UserData)
						}
					} else {
						if got != orig[i] {
							t.Fatalf("body[%d] (%s) pointer was not preserved across Restore", i, e.label)
						}
						if got.UserData != e.label {
							t.Fatalf("body[%d] (%s) UserData = %v, want %q", i, e.label, got.UserData, e.label)
						}
					}
				}

				if added != nil {
					for i, b := range w.Bodies {
						if b == added {
							t.Fatalf("added body D remained in w.Bodies[%d] after Restore", i)
						}
					}
					if added.UserData != "D" || added.VX != 777 {
						t.Fatalf("detached body D was mutated by Restore: UserData=%v VX=%v", added.UserData, added.VX)
					}
				}
			})
		}
	}
}

func TestRigidWorldRestoreIdentityAfterRemoveAndAdd(t *testing.T) {
	const dt = time.Second / 60
	type bodyExpect struct {
		x, vx float32
		label string
	}
	want := []bodyExpect{
		{x: 20, vx: 40, label: "A"},
		{x: 120, vx: -50, label: "B"},
		{x: 220, vx: 60, label: "C"},
	}

	for _, explicitIDs := range []bool{false, true} {
		mode := "defaultID0"
		if explicitIDs {
			mode = "explicitID"
		}
		for _, tc := range []struct {
			name      string
			removeIdx int // -1 for none
			addNew    bool
			readdSame bool
		}{
			{name: "removeFirst", removeIdx: 0},
			{name: "removeMiddle", removeIdx: 1},
			{name: "removeLast", removeIdx: 2},
			{name: "addOnly", removeIdx: -1, addNew: true},
			{name: "removeFirstAndAdd", removeIdx: 0, addNew: true},
			{name: "removeMiddleAndAdd", removeIdx: 1, addNew: true},
			{name: "removeAndReaddFirst", removeIdx: 0, readdSame: true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				rw := &physics.RigidWorld{GravityY: 300}
				orig := make([]*physics.RigidBody, len(want))
				for i, e := range want {
					b := physics.NewRigidBox(e.x, 40, 20, 20, 1.0)
					b.VX = e.vx
					b.Tag = e.label
					b.UserData = e.label
					if explicitIDs {
						b.ID = i + 1
					}
					orig[i] = b
				}
				rw.AddBody(orig...)

				for range 5 {
					rw.Step(dt)
				}
				savedPos := make([][4]float32, len(orig))
				for i, b := range orig {
					savedPos[i] = [4]float32{b.X, b.Y, b.VX, b.VY}
				}

				snap := roundTripRigidWorldSnapshot(t, rw.Save())
				wantChecksum := rw.Checksum()

				// Diverge simulation: remove/add bodies and step forward.
				if tc.removeIdx >= 0 {
					rw.RemoveBody(orig[tc.removeIdx])
				}
				var added *physics.RigidBody
				if tc.addNew {
					added = physics.NewRigidCircle(320, 40, 10, 1.0)
					added.VX = -120
					added.UserData = "D"
					if explicitIDs {
						added.ID = 99
					}
					rw.AddBody(added)
				}
				if tc.readdSame && tc.removeIdx >= 0 {
					rw.AddBody(orig[tc.removeIdx])
				}
				for range 10 {
					rw.Step(dt)
				}
				var addedXBefore float32
				if added != nil {
					addedXBefore = added.X
				}

				if err := rw.Restore(snap); err != nil {
					t.Fatalf("Restore: %v", err)
				}
				if got := rw.Checksum(); got != wantChecksum {
					t.Fatalf("Checksum = %#016x, want %#016x", got, wantChecksum)
				}
				if len(rw.Bodies) != len(want) {
					t.Fatalf("len(rw.Bodies) = %d, want %d", len(rw.Bodies), len(want))
				}

				for i, e := range want {
					got := rw.Bodies[i]
					if math.Float32bits(got.X) != math.Float32bits(savedPos[i][0]) ||
						math.Float32bits(got.Y) != math.Float32bits(savedPos[i][1]) ||
						math.Float32bits(got.VX) != math.Float32bits(savedPos[i][2]) ||
						math.Float32bits(got.VY) != math.Float32bits(savedPos[i][3]) ||
						got.Tag != e.label {
						t.Fatalf("body[%d] (%s) state mismatch: got (%v,%v,%v,%v,%q) want (%v,%v,%v,%v,%q)",
							i, e.label, got.X, got.Y, got.VX, got.VY, got.Tag,
							savedPos[i][0], savedPos[i][1], savedPos[i][2], savedPos[i][3], e.label)
					}
					wasRemoved := tc.removeIdx == i && !tc.readdSame
					if wasRemoved {
						if got == orig[i] {
							t.Fatalf("body[%d] (%s) unexpectedly reused removed pointer", i, e.label)
						}
						if got.UserData != nil {
							t.Fatalf("body[%d] (%s) recreated body UserData = %v, want nil", i, e.label, got.UserData)
						}
					} else {
						if got != orig[i] {
							t.Fatalf("body[%d] (%s) pointer was not preserved across Restore", i, e.label)
						}
						if got.UserData != e.label {
							t.Fatalf("body[%d] (%s) UserData = %v, want %q", i, e.label, got.UserData, e.label)
						}
					}
				}

				if added != nil {
					for i, b := range rw.Bodies {
						if b == added {
							t.Fatalf("added body D remained in rw.Bodies[%d] after Restore", i)
						}
					}
					if added.UserData != "D" || math.Float32bits(added.X) != math.Float32bits(addedXBefore) {
						t.Fatalf("detached body D was mutated by Restore: UserData=%v X=%v (want %v)", added.UserData, added.X, addedXBefore)
					}
				}
			})
		}
	}
}

func TestRestoreTriggersAndJointsIdentityAfterRemoveAndAdd(t *testing.T) {
	// 1. World triggers with default ID 0 keep their callbacks and UserData when an earlier trigger is removed.
	w := &physics.World{}
	b := physics.NewBoxBody(110, 10, 10, 10)
	w.Add(b)

	var events []string
	tr1 := &physics.Trigger{
		Tag:      "T1",
		Shape:    physics.BoxShape(physics.Box{X: 0, Y: 0, W: 40, H: 40}),
		UserData: "T1",
		OnEnter:  func(*physics.Body) { events = append(events, "T1") },
	}
	tr2 := &physics.Trigger{
		Tag:      "T2",
		Shape:    physics.BoxShape(physics.Box{X: 100, Y: 0, W: 40, H: 40}),
		UserData: "T2",
		OnEnter:  func(*physics.Body) { events = append(events, "T2") },
	}
	w.AddTrigger(tr1, tr2)

	wSnap := roundTripWorldSnapshot(t, w.Save())
	wChecksum := w.Checksum()

	w.RemoveTrigger(tr1)
	tr3 := &physics.Trigger{
		Tag:      "T3",
		Shape:    physics.BoxShape(physics.Box{X: 200, Y: 0, W: 40, H: 40}),
		UserData: "T3",
	}
	w.AddTrigger(tr3)

	if err := w.Restore(wSnap); err != nil {
		t.Fatalf("World.Restore: %v", err)
	}
	if w.Checksum() != wChecksum {
		t.Fatalf("World.Checksum = %#016x, want %#016x", w.Checksum(), wChecksum)
	}
	if len(w.Triggers) != 2 {
		t.Fatalf("len(w.Triggers) = %d, want 2", len(w.Triggers))
	}
	if w.Triggers[0] == tr1 || w.Triggers[0] == tr2 || w.Triggers[0].UserData != nil || w.Triggers[0].Tag != "T1" {
		t.Fatalf("w.Triggers[0] should be fresh T1, got ptr=%p tag=%q ud=%v", w.Triggers[0], w.Triggers[0].Tag, w.Triggers[0].UserData)
	}
	if w.Triggers[1] != tr2 || w.Triggers[1].UserData != "T2" || w.Triggers[1].Shape.Box.X != 100 {
		t.Fatalf("w.Triggers[1] should reuse tr2 with T2 state, got ptr=%p tag=%q ud=%v x=%v",
			w.Triggers[1], w.Triggers[1].Tag, w.Triggers[1].UserData, w.Triggers[1].Shape.Box.X)
	}
	w.StepTriggers()
	if len(events) != 1 || events[0] != "T2" {
		t.Fatalf("StepTriggers events = %v, want [T2]", events)
	}

	// 2. RigidWorld joints and triggers with default ID 0 preserve surviving handles after removal and addition.
	rw := &physics.RigidWorld{GravityY: 200}
	anchor := physics.NewStaticRigidBox(100, 20, 10, 10)
	bob1 := physics.NewRigidCircle(100, 80, 10, 1)
	bob2 := physics.NewRigidCircle(180, 80, 10, 1)
	rw.AddBody(anchor, bob1, bob2)

	j1 := physics.NewDistanceJoint(anchor, bob1, core.Point{}, core.Point{}, 60)
	j2 := physics.NewDistanceJoint(anchor, bob2, core.Point{}, core.Point{}, 100)
	rw.AddJoint(j1, j2)

	rt1 := &physics.Trigger{Tag: "R1", Shape: physics.BoxShape(physics.Box{X: 0, Y: 0, W: 50, H: 50}), UserData: "R1"}
	rt2 := &physics.Trigger{Tag: "R2", Shape: physics.BoxShape(physics.Box{X: 100, Y: 0, W: 50, H: 50}), UserData: "R2"}
	rw.AddTrigger(rt1, rt2)

	rwSnap := roundTripRigidWorldSnapshot(t, rw.Save())
	rwChecksum := rw.Checksum()

	rw.RemoveJoint(j1)
	j3 := physics.NewDistanceJoint(bob1, bob2, core.Point{}, core.Point{}, 80)
	rw.AddJoint(j3)
	rw.RemoveTrigger(rt1)

	if err := rw.Restore(rwSnap); err != nil {
		t.Fatalf("RigidWorld.Restore: %v", err)
	}
	if rw.Checksum() != rwChecksum {
		t.Fatalf("RigidWorld.Checksum = %#016x, want %#016x", rw.Checksum(), rwChecksum)
	}
	if len(rw.Joints) != 2 {
		t.Fatalf("len(rw.Joints) = %d, want 2", len(rw.Joints))
	}
	if rw.Joints[0] == j1 || rw.Joints[0] == j2 || rw.Joints[0] == j3 {
		t.Fatalf("rw.Joints[0] should be a fresh joint for removed j1")
	}
	if rw.Joints[1] != j2 {
		t.Fatalf("rw.Joints[1] did not reuse surviving j2 pointer")
	}
	if j2.Length != 100 || j2.B != bob2 {
		t.Fatalf("j2 state corrupted: Length=%v B=%p (want 100, %p)", j2.Length, j2.B, bob2)
	}
	if len(rw.Triggers) != 2 || rw.Triggers[0] == rt1 || rw.Triggers[0] == rt2 || rw.Triggers[1] != rt2 || rw.Triggers[1].UserData != "R2" {
		t.Fatalf("RigidWorld triggers not matched by identity after Restore")
	}
}

func TestRestoreIntoFreshWorldAdvancesInternalIDs(t *testing.T) {
	// Restoring into an empty world and adding new default-ID bodies must never
	// collide with the restored bodies' internal IDs.
	src := &physics.RigidWorld{}
	a := physics.NewRigidBox(10, 10, 10, 10, 1)
	b := physics.NewRigidBox(40, 10, 10, 10, 1)
	src.AddBody(a, b)
	snap1 := roundTripRigidWorldSnapshot(t, src.Save())

	fresh := &physics.RigidWorld{}
	if err := fresh.Restore(snap1); err != nil {
		t.Fatalf("fresh.Restore(snap1): %v", err)
	}
	fresh.Bodies[0].UserData = "A"
	fresh.Bodies[1].UserData = "B"
	bPtr := fresh.Bodies[1]

	// Add a third body C with ID 0; its internal ID must not collide with A or B.
	c := physics.NewRigidBox(80, 10, 10, 10, 1)
	c.UserData = "C"
	fresh.AddBody(c)
	snap2 := roundTripRigidWorldSnapshot(t, fresh.Save())

	// Remove A, then restore snap2: B and C must keep their exact pointers and state.
	fresh.RemoveBody(fresh.Bodies[0])
	if err := fresh.Restore(snap2); err != nil {
		t.Fatalf("fresh.Restore(snap2): %v", err)
	}
	if len(fresh.Bodies) != 3 {
		t.Fatalf("len(fresh.Bodies) = %d, want 3", len(fresh.Bodies))
	}
	if fresh.Bodies[0].UserData != nil || fresh.Bodies[0].X != 10 {
		t.Fatalf("fresh.Bodies[0] = (X=%v, UD=%v), want fresh A", fresh.Bodies[0].X, fresh.Bodies[0].UserData)
	}
	if fresh.Bodies[1] != bPtr || fresh.Bodies[1].UserData != "B" || fresh.Bodies[1].X != 40 {
		t.Fatalf("fresh.Bodies[1] did not preserve B: ptr=%p want=%p X=%v UD=%v",
			fresh.Bodies[1], bPtr, fresh.Bodies[1].X, fresh.Bodies[1].UserData)
	}
	if fresh.Bodies[2] != c || fresh.Bodies[2].UserData != "C" || fresh.Bodies[2].X != 80 {
		t.Fatalf("fresh.Bodies[2] did not preserve C: ptr=%p want=%p X=%v UD=%v",
			fresh.Bodies[2], c, fresh.Bodies[2].X, fresh.Bodies[2].UserData)
	}
}
