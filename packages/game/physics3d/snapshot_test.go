package physics3d_test

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics3d"
)

type flatVoxelGrid struct {
	size float32
}

func (g flatVoxelGrid) VoxelSize() float32 { return g.size }
func (g flatVoxelGrid) IsSolid(x, y, z int) bool {
	return y == -1 && x >= 6 && x <= 10 && z >= -2 && z <= 2
}

func buildTestWorld3D() (*physics3d.World, *[]string, *physics3d.Body, *physics3d.HingeJoint) {
	w := physics3d.NewWorld()

	// Static floor
	floor := w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 25, Y: 0.5, Z: 25}}),
		physics3d.Vec3{Y: -0.5},
	))
	floor.Tag = "floor"

	// Static TriMesh ramp
	verts := []physics3d.Vec3{
		{X: -6, Y: 0, Z: -2},
		{X: -2, Y: 0, Z: -2},
		{X: -2, Y: 0, Z: 2},
		{X: -6, Y: 1.5, Z: 2},
	}
	tm := physics3d.NewTriMeshCollider(verts, []uint32{0, 2, 1, 0, 3, 2})
	tm.Tag = "ramp"
	w.AddTriMesh(tm)

	// Static Heightfield patch
	heights := []float32{
		0.0, 0.2, 0.0,
		0.2, 0.4, 0.2,
		0.0, 0.2, 0.0,
	}
	hf := physics3d.NewHeightfieldCollider(3, 3, 2.0, physics3d.Vec3{X: -12, Y: 0, Z: -2}, heights)
	hf.Tag = "hill"
	w.AddHeightfield(hf)

	// Static VoxelCollider
	vc := physics3d.NewVoxelCollider(flatVoxelGrid{size: 1.0})
	vc.Tag = "voxels"
	w.AddVoxelCollider(vc)

	// Stacked boxes (bottom box settles and sleeps, upper box tilted slightly)
	b1 := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
		physics3d.Vec3{X: 0, Y: 0.52, Z: 0},
		2.0,
	))
	b1.Tag = "stack-bottom"

	b2 := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.45, Y: 0.45, Z: 0.45}}),
		physics3d.Vec3{X: 0.08, Y: 1.55, Z: 0},
		1.0,
	))
	b2.Tag = "stack-top"
	b2.Rot = physics3d.QuatFromAxisAngle(physics3d.Vec3{Z: 1}, 0.12)

	// Rolling sphere on the ramp
	sphere := w.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.4}),
		physics3d.Vec3{X: -4.5, Y: 2.2, Z: 0},
		1.5,
	))
	sphere.Tag = "roller"
	sphere.Restitution = 0.35

	// Capsule resting on voxel collider
	capBody := w.AddBody(physics3d.NewDynamicBody(
		physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.3, 1.2)),
		physics3d.Vec3{X: 8.0, Y: 1.5, Z: 0},
		1.2,
	))
	capBody.Tag = "capsule"

	// Kinematic mover (tests lastPos / lastRot / havePose private fields!)
	kin := physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.6, Y: 0.25, Z: 0.6}}),
		physics3d.Vec3{X: -2.0, Y: 0.5, Z: 4.0},
	)
	kin.Static = false
	kin.Kinematic = true
	kinBody := w.AddBody(kin)
	kinBody.Tag = "kinematic"

	// Box next to kinematic platform so moving kinematic wakes it after step 100
	sleeper := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.4, Z: 0.4}}),
		physics3d.Vec3{X: 0.5, Y: 0.41, Z: 4.0},
		1.0,
	))
	sleeper.Tag = "sleeper"

	// Trigger volume with AreaSettings (buoyancy + drag)
	trig := physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 2.5, Y: 1.5, Z: 2.5}}),
		physics3d.Vec3{X: -10, Y: 1.0, Z: 0},
	)
	trig.IsTrigger = true
	trig.Tag = "pool"
	trig.Area = physics3d.AreaSettings{
		FluidDensity:     3.5,
		FluidLinearDrag:  2.5,
		FluidAngularDrag: 1.5,
		FluidVelocity:    physics3d.Vec3{X: 1.2},
	}
	w.AddBody(trig)

	// Buoyant sphere dropping into pool
	floater := w.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
		physics3d.Vec3{X: -10, Y: 2.8, Z: 0},
		1.0,
	))
	floater.Tag = "floater"

	// IgnoreCollision pair
	w.IgnoreCollision(b1, sphere)

	// Joints: Hinge (motorized, tracks unwrapped angle), Distance, Ball, Slider, Fixed, SwingTwist, SixDOF, and a Breakable FixedJoint
	anchorH := w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.2, Y: 0.2, Z: 0.2}}),
		physics3d.Vec3{X: 3, Y: 3.5, Z: -3},
	))
	armH := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.6, Y: 0.15, Z: 0.15}}),
		physics3d.Vec3{X: 3.6, Y: 3.5, Z: -3},
		1.0,
	))
	hinge := physics3d.NewHingeJoint(anchorH, armH, physics3d.Vec3{}, physics3d.Vec3{X: -0.6}, physics3d.Vec3{Z: 1})
	hinge.MotorTargetVel = 3.5
	hinge.MotorMaxTorque = 12
	w.AddJoint(hinge)

	bob := w.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.3}),
		physics3d.Vec3{X: 4.5, Y: 2.5, Z: -3},
		1.0,
	))
	distJ := physics3d.NewDistanceJoint(armH, bob, physics3d.Vec3{X: 0.6}, physics3d.Vec3{}, 1.2, 1.2)
	distJ.Stiffness = 80
	distJ.Damping = 4
	w.AddJoint(distJ)

	ballB := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.2, Y: 0.4, Z: 0.2}}),
		physics3d.Vec3{X: 3, Y: 2.6, Z: -5},
		1.0,
	))
	ballJ := physics3d.NewBallJoint(nil, ballB, physics3d.Vec3{X: 3, Y: 3.2, Z: -5}, physics3d.Vec3{Y: 0.5})
	ballJ.ConeLimit = 0.6
	w.AddJoint(ballJ)

	sliderB := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.3, Z: 0.3}}),
		physics3d.Vec3{X: 0, Y: 3.0, Z: -5},
		1.0,
	))
	sliderJ := physics3d.NewSliderJoint(nil, sliderB, physics3d.Vec3{X: 0, Y: 3.0, Z: -5}, physics3d.Vec3{}, physics3d.Vec3{X: 1})
	sliderJ.EnableLimits = true
	sliderJ.MinTranslation = -1.0
	sliderJ.MaxTranslation = 1.0
	sliderJ.DriveTargetPos = 0.7
	sliderJ.DriveStiffness = 25
	sliderJ.DriveDamping = 3
	w.AddJoint(sliderJ)

	stB := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.2, Y: 0.4, Z: 0.2}}),
		physics3d.Vec3{X: -3, Y: 2.5, Z: -5},
		1.0,
	))
	stJ := physics3d.NewSwingTwistJoint(nil, stB, physics3d.Vec3{X: -3, Y: 3.1, Z: -5}, physics3d.Vec3{Y: 0.5}, physics3d.Vec3{Y: -1})
	stJ.EnableSwingLimits = true
	stJ.SwingMin1, stJ.SwingMax1 = -0.5, 0.5
	stJ.SwingMin2, stJ.SwingMax2 = -0.4, 0.4
	stJ.EnableTwistLimits = true
	stJ.MinTwist, stJ.MaxTwist = -0.5, 0.5
	w.AddJoint(stJ)

	sixB := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.25, Y: 0.25, Z: 0.25}}),
		physics3d.Vec3{X: -6, Y: 3.0, Z: -5},
		1.0,
	))
	sixJ := physics3d.NewSixDOFJoint(nil, sixB, physics3d.Vec3{X: -6, Y: 3.0, Z: -5}, physics3d.Vec3{})
	sixJ.Linear[0] = physics3d.AxisConfig{Mode: physics3d.AxisLimited, Min: -1, Max: 1, Drive: physics3d.DrivePosition, TargetPos: 0.5, Stiffness: 30, Damping: 4}
	sixJ.Linear[1] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
	sixJ.Linear[2] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
	sixJ.Angular[0] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
	sixJ.Angular[1] = physics3d.AxisConfig{Mode: physics3d.AxisFree, Drive: physics3d.DriveVelocity, TargetVel: 2.0, MaxForce: 5}
	sixJ.Angular[2] = physics3d.AxisConfig{Mode: physics3d.AxisLocked}
	w.AddJoint(sixJ)

	// Breakable fixed joint that will break when struck after step 100
	breakAnchor := w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.2, Y: 0.2, Z: 0.2}}),
		physics3d.Vec3{X: 5, Y: 3.5, Z: 3},
	))
	breakPend := w.AddBody(physics3d.NewDynamicBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.3, Z: 0.3}}),
		physics3d.Vec3{X: 5.8, Y: 3.5, Z: 3},
		2.0,
	))
	breakPend.Tag = "breakable"
	fixedJ := physics3d.NewFixedJoint(breakAnchor, breakPend, physics3d.Vec3{}, physics3d.Vec3{X: -0.8})
	fixedJ.BreakForce = 80
	w.AddJoint(fixedJ)

	var log []string
	w.OnContact = func(ev physics3d.ContactEvent) {
		log = append(log, fmt.Sprintf("contact:%d:%d:%d", ev.Kind, ev.BodyA.ID, ev.BodyB.ID))
	}
	w.OnTrigger = func(ev physics3d.ContactEvent) {
		log = append(log, fmt.Sprintf("trigger:%d:%d:%d", ev.Kind, ev.BodyA.ID, ev.BodyB.ID))
	}
	w.OnJointBreak = func(ev physics3d.JointBreakEvent) {
		log = append(log, fmt.Sprintf("break:%d:%d", ev.BodyA.ID, ev.BodyB.ID))
	}

	return w, &log, kinBody, hinge
}

func stepTestWorld3D(w *physics3d.World, kin *physics3d.Body, step int) {
	const dt = time.Second / 60
	if step >= 110 && step < 155 {
		kin.Pos.X += 0.05
	}
	if step == 130 {
		for _, b := range w.Bodies() {
			if b.Tag == "breakable" {
				b.ApplyImpulse(physics3d.Vec3{Y: -12})
			}
		}
	}
	w.Step(dt)
}

func TestWorldSaveRestoreBitIdentical(t *testing.T) {
	w, log, kin, hinge := buildTestWorld3D()

	for step := 0; step < 100; step++ {
		stepTestWorld3D(w, kin, step)
	}

	snap := w.Save()
	snapChecksum := w.Checksum()

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal(WorldSnapshot): %v", err)
	}
	var decoded physics3d.WorldSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(WorldSnapshot): %v", err)
	}

	// Step original from 100 to 200.
	*log = nil
	for step := 100; step < 200; step++ {
		stepTestWorld3D(w, kin, step)
	}
	wantChecksum := w.Checksum()
	wantEvents := append([]string(nil), (*log)...)
	wantHingeAngle := hinge.Angle()
	wantBodies := make([]physics3d.Body, len(w.Bodies()))
	for i, b := range w.Bodies() {
		wantBodies[i] = *b
	}
	wantContactEvents := append([]physics3d.ContactEvent(nil), w.ContactEvents()...)
	wantBreakEvents := append([]physics3d.JointBreakEvent(nil), w.JointBreakEvents()...)

	// Restore in place from JSON-decoded snapshot.
	if err := w.Restore(decoded); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if w.Checksum() != snapChecksum {
		t.Fatalf("restored checksum = %#x, want %#x", w.Checksum(), snapChecksum)
	}

	*log = nil
	for step := 100; step < 200; step++ {
		stepTestWorld3D(w, kin, step)
	}
	if w.Checksum() != wantChecksum {
		t.Fatalf("step 200 checksum after restore = %#x, want %#x", w.Checksum(), wantChecksum)
	}
	if math.Float32bits(hinge.Angle()) != math.Float32bits(wantHingeAngle) {
		t.Fatalf("hinge.Angle() = %v, want %v", hinge.Angle(), wantHingeAngle)
	}
	if len(*log) != len(wantEvents) {
		t.Fatalf("callback event count = %d, want %d", len(*log), len(wantEvents))
	}
	for i := range wantEvents {
		if (*log)[i] != wantEvents[i] {
			t.Fatalf("callback event[%d] = %q, want %q", i, (*log)[i], wantEvents[i])
		}
	}
	if len(w.ContactEvents()) != len(wantContactEvents) {
		t.Fatalf("ContactEvents len = %d, want %d", len(w.ContactEvents()), len(wantContactEvents))
	}
	if len(w.JointBreakEvents()) != len(wantBreakEvents) {
		t.Fatalf("JointBreakEvents len = %d, want %d", len(w.JointBreakEvents()), len(wantBreakEvents))
	}
	for i, wb := range wantBodies {
		gb := *w.Bodies()[i]
		if gb.Pos != wb.Pos || gb.Vel != wb.Vel || gb.Rot != wb.Rot || gb.AngVel != wb.AngVel || gb.Sleeping != wb.Sleeping {
			t.Fatalf("Body[%d] (%s) diverged:\ngot  %+v\nwant %+v", i, gb.Tag, gb, wb)
		}
	}

	// Restore into a fresh World (pre-registering VoxelCollider grid by ID).
	fresh := physics3d.NewWorld()
	vc := physics3d.NewVoxelCollider(flatVoxelGrid{size: 1.0})
	vc.ID = w.VoxelColliders()[0].ID
	fresh.AddVoxelCollider(vc)
	if err := fresh.Restore(decoded); err != nil {
		t.Fatalf("fresh.Restore: %v", err)
	}
	if fresh.Checksum() != snapChecksum {
		t.Fatalf("fresh restored checksum = %#x, want %#x", fresh.Checksum(), snapChecksum)
	}
	var freshKin *physics3d.Body
	for _, b := range fresh.Bodies() {
		if b.Tag == "kinematic" {
			freshKin = b
		}
	}
	for step := 100; step < 200; step++ {
		stepTestWorld3D(fresh, freshKin, step)
	}
	if fresh.Checksum() != wantChecksum {
		t.Fatalf("fresh step 200 checksum = %#x, want %#x", fresh.Checksum(), wantChecksum)
	}
}

func TestWorldSnapshotValidation(t *testing.T) {
	var nilWorld *physics3d.World
	if err := nilWorld.Restore(physics3d.WorldSnapshot{Version: physics3d.SnapshotVersion}); err == nil {
		t.Fatal("expected error restoring nil World")
	}
	w := physics3d.NewWorld()
	if err := w.Restore(physics3d.WorldSnapshot{Version: 99}); err == nil {
		t.Fatal("expected error for unsupported version")
	}
	if err := w.Restore(physics3d.WorldSnapshot{
		Version: physics3d.SnapshotVersion,
		Joints:  []physics3d.JointSnapshot{{Kind: "invalid"}},
	}); err == nil {
		t.Fatal("expected error for unknown joint kind")
	}
}

func TestGoldenTrajectory3D(t *testing.T) {
	// Cross-architecture golden trajectory hash (must match identically on amd64 and arm64).
	const wantGolden uint64 = 0xc4f17a20b03f1e68

	w, _, kin, _ := buildTestWorld3D()
	for step := 0; step < 200; step++ {
		stepTestWorld3D(w, kin, step)
	}
	got := w.Checksum()
	if got != wantGolden {
		t.Fatalf("physics3d.World golden checksum = %#016x, want %#016x", got, wantGolden)
	}
}
