package physics3d_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics3d"
)

func TestSlowPushWakesSleepingBodyAndConservesMomentum(t *testing.T) {
	// Repro from #1713: a 1 kg cube settles and falls asleep on a frictionless
	// floor, then a 10 kg cube slides into it at 0.01, 0.1, and 1 m/s.
	// The sleeping cube must wake at any push speed, move forward, and conserve
	// total linear momentum within 1%.
	unitCube := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})

	for _, speed := range []float32{0.01, 0.1, 1.0} {
		t.Run("zero_damping_"+formatSpeed(speed), func(t *testing.T) {
			w := physics3d.NewWorld()
			floor := w.AddBody(physics3d.NewStaticBody(
				physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 100, Y: 0.5, Z: 20}}),
				physics3d.Vec3{Y: -0.5},
			))
			floor.Friction = 0

			targetDef := physics3d.NewDynamicBody(unitCube, physics3d.Vec3{X: 0, Y: 0.5}, 1)
			targetDef.Friction = 0
			targetDef.Restitution = 0
			targetDef.LinearDamping = 0
			targetDef.AngularDamping = 0
			target := w.AddBody(targetDef)

			for range 180 {
				w.Step(step60)
				if target.Sleeping {
					break
				}
			}
			if !target.Sleeping {
				t.Fatalf("1 kg cube did not fall asleep before push (vel=%v)", target.Vel)
			}

			// Place the 10 kg pusher close enough to make contact well within 10 s.
			// Keep AllowSleep=false on the pusher only when speed is below the
			// default sleep threshold (0.01 < 0.05) so we test momentum over the
			// full 10 s run without the pusher itself timing out to sleep.
			pusherDef := physics3d.NewDynamicBody(unitCube, physics3d.Vec3{X: -1.005, Y: 0.5}, 10)
			pusherDef.Friction = 0
			pusherDef.Restitution = 0
			pusherDef.LinearDamping = 0
			pusherDef.AngularDamping = 0
			pusherDef.Vel = physics3d.Vec3{X: speed}
			if speed < physics3d.DefaultSleepLinearThreshold {
				pusherDef.AllowSleep = false
			}
			pusher := w.AddBody(pusherDef)

			initialMomentum := float64(pusher.Mass) * float64(speed)
			for range 600 {
				w.Step(step60)
			}

			if target.Sleeping {
				t.Fatalf("speed=%v: 1 kg cube stayed asleep (pos.X=%v)", speed, target.Pos.X)
			}
			if target.Pos.X <= 0.01*speed {
				t.Fatalf("speed=%v: 1 kg cube barely moved (pos.X=%v)", speed, target.Pos.X)
			}
			finalMomentum := float64(pusher.Mass)*float64(pusher.Vel.X) + float64(target.Mass)*float64(target.Vel.X)
			relErr := math.Abs(finalMomentum-initialMomentum) / initialMomentum
			if relErr > 0.01 {
				t.Fatalf("speed=%v: momentum not conserved within 1%%: got %.6f, want %.6f (relErr=%.4f%%, pusherVel=%v, targetVel=%v)",
					speed, finalMomentum, initialMomentum, relErr*100, pusher.Vel.X, target.Vel.X)
			}
		})
	}

	// Also verify default settings (AllowSleep=true, DefaultSleepLinearThreshold)
	// at 0.01 m/s when contact happens before TimeToSleep elapses: even below
	// DefaultSleepLinearThreshold, an awake dynamic body touching a sleeping body
	// wakes it and conserves momentum within 1%.
	t.Run("default_settings_0.01mps_contact_wakes", func(t *testing.T) {
		w := physics3d.NewWorld()
		floor := w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 100, Y: 0.5, Z: 20}}),
			physics3d.Vec3{Y: -0.5},
		))
		floor.Friction = 0

		targetDef := physics3d.NewDynamicBody(unitCube, physics3d.Vec3{X: 0, Y: 0.5}, 1)
		targetDef.Friction = 0
		targetDef.LinearDamping = 0
		targetDef.AngularDamping = 0
		target := w.AddBody(targetDef)

		for range 180 {
			w.Step(step60)
			if target.Sleeping {
				break
			}
		}
		if !target.Sleeping {
			t.Fatal("1 kg cube did not fall asleep")
		}

		// Gap of 1 mm at 0.01 m/s reaches contact in 0.1 s (< 0.4 s TimeToSleep).
		pusherDef := physics3d.NewDynamicBody(unitCube, physics3d.Vec3{X: -1.001, Y: 0.5}, 10)
		pusherDef.Friction = 0
		pusherDef.LinearDamping = 0
		pusherDef.AngularDamping = 0
		pusherDef.Vel = physics3d.Vec3{X: 0.01}
		pusher := w.AddBody(pusherDef)

		initialMomentum := float64(pusher.Mass) * 0.01
		// Step 15 frames (0.25 s): contact occurs around frame 6 while both are awake.
		for range 15 {
			w.Step(step60)
		}
		if target.Sleeping {
			t.Fatal("0.01 m/s awake cube failed to wake sleeping 1 kg cube")
		}
		if target.Pos.X <= 0 {
			t.Fatalf("1 kg cube did not move: pos.X=%v", target.Pos.X)
		}
		finalMomentum := float64(pusher.Mass)*float64(pusher.Vel.X) + float64(target.Mass)*float64(target.Vel.X)
		relErr := math.Abs(finalMomentum-initialMomentum) / initialMomentum
		if relErr > 0.01 {
			t.Fatalf("0.01 m/s collision momentum relErr=%.4f%% > 1%% (got=%.6f want=%.6f)", relErr*100, finalMomentum, initialMomentum)
		}
	})
}

func formatSpeed(v float32) string {
	switch v {
	case 0.01:
		return "0.01mps"
	case 0.1:
		return "0.1mps"
	case 1.0:
		return "1mps"
	default:
		return "custom"
	}
}

// buildCubePyramid3D builds a static floor and a 20-cube pyramid (rows of 6, 5, 4, 3, 2).
func buildCubePyramid3D() (*physics3d.World, []*physics3d.Body) {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 30, Y: 0.5, Z: 30}}),
		physics3d.Vec3{Y: -0.5},
	))
	unit := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	var cubes []*physics3d.Body
	y := float32(0.5)
	for _, n := range []int{6, 5, 4, 3, 2} {
		x0 := -float32(n-1) * 0.5
		for i := range n {
			b := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{X: x0 + float32(i), Y: y}, 1))
			cubes = append(cubes, b)
		}
		y += 1.0
	}
	return w, cubes
}

func countSleeping3D(bodies []*physics3d.Body) int {
	n := 0
	for _, b := range bodies {
		if b.Sleeping {
			n++
		}
	}
	return n
}

func TestPyramid20CubesSleepsAndWakesAsWholeIsland(t *testing.T) {
	w, cubes := buildCubePyramid3D()
	if len(cubes) != 20 {
		t.Fatalf("pyramid has %d cubes, want 20", len(cubes))
	}

	// Add an unrelated distant cube in its own island to ensure waking the
	// pyramid does not disturb distant sleeping islands.
	unit := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	distant := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{X: 20, Y: 0.5}, 1))

	// Give the top cube a tiny perturbation so the pyramid takes time to settle
	// while the bottom row is already nearly stationary.
	top := cubes[len(cubes)-1]
	top.Vel = physics3d.Vec3{X: 0.12}

	sleptStep := -1
	for step := range 600 {
		w.Step(step60)
		sleeping := countSleeping3D(cubes)
		if sleeping != 0 && sleeping != len(cubes) {
			t.Fatalf("step %d: partial pyramid sleep (%d of %d sleeping); island must sleep only as a whole",
				step, sleeping, len(cubes))
		}
		if sleeping == len(cubes) {
			sleptStep = step
			break
		}
	}
	if sleptStep < 0 {
		t.Fatalf("pyramid of 20 cubes did not fall asleep within 10 s (sleeping=%d)", countSleeping3D(cubes))
	}
	if !distant.Sleeping {
		t.Fatal("distant cube did not fall asleep")
	}

	// While asleep, the pyramid stays completely still (zero pose drift from
	// the moment it sleeps, and bit-identical checksum across sleeping steps).
	type pose struct {
		pos physics3d.Vec3
		rot physics3d.Quat
	}
	sleepingPoses := make([]pose, len(cubes))
	for i, b := range cubes {
		sleepingPoses[i] = pose{pos: b.Pos, rot: b.Rot}
	}
	w.Step(step60)
	before := w.Checksum()
	for range 30 {
		w.Step(step60)
	}
	if countSleeping3D(cubes) != len(cubes) {
		t.Fatalf("pyramid woke spontaneously (%d sleeping)", countSleeping3D(cubes))
	}
	for i, b := range cubes {
		if b.Pos != sleepingPoses[i].pos || b.Rot != sleepingPoses[i].rot || b.Vel != (physics3d.Vec3{}) || b.AngVel != (physics3d.Vec3{}) {
			t.Fatalf("sleeping cube %d drifted: pos=%v rot=%v vel=%v ang=%v", i, b.Pos, b.Rot, b.Vel, b.AngVel)
		}
	}
	if after := w.Checksum(); after != before {
		t.Fatalf("sleeping pyramid checksum drifted: %#x != %#x", after, before)
	}

	// Touch one corner cube with a slow awake cube (0.05 m/s) -> the entire
	// 20-cube pyramid must wake on the step they touch, while distant stays asleep.
	nudger := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{X: cubes[0].Pos.X - 1.001, Y: 0.5}, 1))
	nudger.Friction = 0
	nudger.Vel = physics3d.Vec3{X: 0.05}

	wokeWhole := false
	for step := range 30 {
		w.Step(step60)
		sleeping := countSleeping3D(cubes)
		if sleeping != 0 && sleeping != len(cubes) {
			t.Fatalf("wake step %d: partial pyramid wake (%d of %d still sleeping)", step, sleeping, len(cubes))
		}
		if sleeping == 0 {
			wokeWhole = true
			break
		}
	}
	if !wokeWhole {
		t.Fatal("touching one cube did not wake the 20-cube pyramid")
	}
	if !distant.Sleeping {
		t.Fatal("waking the pyramid also woke an unrelated distant sleeping cube")
	}
}

func TestDisableSleepAndAllowSleep(t *testing.T) {
	unit := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})

	t.Run("WorldDisableSleepPreventsAndWakes", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 0.5, Z: 10}}),
			physics3d.Vec3{Y: -0.5},
		))
		b := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{Y: 0.5}, 1))
		w.DisableSleep = true
		for range 120 {
			w.Step(step60)
			if b.Sleeping {
				t.Fatal("body fell asleep with World.DisableSleep = true")
			}
		}

		// Re-enable sleep and let it fall asleep.
		w.DisableSleep = false
		for range 120 {
			w.Step(step60)
		}
		if !b.Sleeping {
			t.Fatal("body did not sleep after DisableSleep = false")
		}

		// Setting DisableSleep = true wakes sleeping bodies on the next Step.
		w.DisableSleep = true
		w.Step(step60)
		if b.Sleeping {
			t.Fatal("setting DisableSleep = true did not wake sleeping body")
		}
	})

	t.Run("BodyAllowSleepDefaultTrueAndKeepsIslandAwake", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 0.5, Z: 20}}),
			physics3d.Vec3{Y: -0.5},
		))

		// Default AllowSleep on both NewDynamicBody and zero-initialized Body literal is true.
		litBody := w.AddBody(physics3d.Body{Shape: unit, Pos: physics3d.Vec3{X: 10, Y: 0.5}, Mass: 1})
		if !litBody.AllowSleep {
			t.Fatal("zero-initialized Body literal should default AllowSleep to true")
		}

		// Stack of two cubes where bottom has AllowSleep=false: both must stay awake.
		bottomDef := physics3d.NewDynamicBody(unit, physics3d.Vec3{X: 0, Y: 0.5}, 1)
		if !bottomDef.AllowSleep {
			t.Fatal("NewDynamicBody should initialize AllowSleep to true")
		}
		bottomDef.AllowSleep = false
		bottom := w.AddBody(bottomDef)
		top := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{X: 0, Y: 1.5}, 1))

		// SetAllowSleep(false) on a zero-initialized Body literal before AddBody.
		litNoSleepDef := physics3d.Body{Shape: unit, Pos: physics3d.Vec3{X: -10, Y: 0.5}, Mass: 1}
		litNoSleepDef.SetAllowSleep(false)
		litNoSleep := w.AddBody(litNoSleepDef)

		for range 180 {
			w.Step(step60)
			if bottom.Sleeping || top.Sleeping {
				t.Fatalf("island with AllowSleep=false body slept: bottom=%v top=%v", bottom.Sleeping, top.Sleeping)
			}
			if litNoSleep.Sleeping {
				t.Fatal("body with SetAllowSleep(false) fell asleep")
			}
		}
		if !litBody.Sleeping {
			t.Fatal("separate island with default AllowSleep=true did not sleep")
		}

		// Allowing sleep on bottom lets the stack sleep together.
		bottom.AllowSleep = true
		for range 120 {
			w.Step(step60)
		}
		if !bottom.Sleeping || !top.Sleeping {
			t.Fatalf("stack did not sleep after AllowSleep=true: bottom=%v top=%v", bottom.Sleeping, top.Sleeping)
		}

		// Clearing AllowSleep on a sleeping body wakes its entire island on the next Step.
		bottom.AllowSleep = false
		w.Step(step60)
		if bottom.Sleeping || top.Sleeping {
			t.Fatalf("setting AllowSleep=false did not wake sleeping island: bottom=%v top=%v", bottom.Sleeping, top.Sleeping)
		}
	})
}

func TestSleepThresholdsAndNonFiniteFallback(t *testing.T) {
	unit := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})

	t.Run("CustomLinearThresholdAndTimeToSleep", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.SleepLinearThreshold = 0.02
		w.TimeToSleep = 0.25
		floor := w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 50, Y: 0.5, Z: 10}}),
			physics3d.Vec3{Y: -0.5},
		))
		floor.Friction = 0

		crateDef := physics3d.NewDynamicBody(unit, physics3d.Vec3{Y: 0.5}, 1)
		crateDef.Friction = 0
		crateDef.LinearDamping = 0
		crateDef.AngularDamping = 0
		crateDef.Vel = physics3d.Vec3{X: 0.03}
		crate := w.AddBody(crateDef)

		for range 90 {
			w.Step(step60)
		}
		if crate.Sleeping {
			t.Fatal("crate moving at 0.03 m/s slept under a 0.02 m/s threshold")
		}

		w.SleepLinearThreshold = 0.05
		for range 20 {
			w.Step(step60)
		}
		if !crate.Sleeping {
			t.Fatal("crate moving at 0.03 m/s did not sleep under a 0.05 m/s threshold within 0.25 s")
		}
	})

	t.Run("NonFiniteSettingsUseDefaults", func(t *testing.T) {
		nan := float32(math.NaN())
		posInf := float32(math.Inf(1))
		negInf := float32(math.Inf(-1))
		setters := []struct {
			name string
			set  func(*physics3d.World, float32)
		}{
			{"SleepLinearThreshold", func(w *physics3d.World, v float32) { w.SleepLinearThreshold = v }},
			{"SleepAngularThreshold", func(w *physics3d.World, v float32) { w.SleepAngularThreshold = v }},
			{"TimeToSleep", func(w *physics3d.World, v float32) { w.TimeToSleep = v }},
		}
		for _, s := range setters {
			for _, v := range []float32{nan, posInf, negInf, -1} {
				w := physics3d.NewWorld()
				s.set(w, v)
				w.AddBody(physics3d.NewStaticBody(
					physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 0.5, Z: 20}}),
					physics3d.Vec3{Y: -0.5},
				))
				crate := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{Y: 0.5}, 1))
				falling := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{X: 10, Y: 500}, 1))
				for i := range 90 {
					w.Step(step60)
					if falling.Sleeping {
						t.Fatalf("%s=%v: falling body slept at step %d", s.name, v, i)
					}
				}
				if !crate.Sleeping {
					t.Fatalf("%s=%v: resting crate did not sleep", s.name, v)
				}
			}
		}
	})
}

func TestJointIslandLinksAndWakeInBox(t *testing.T) {
	unit := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 30, Y: 0.5, Z: 30}}),
		physics3d.Vec3{Y: -0.5},
	))
	left := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{X: -2, Y: 0.5}, 1))
	right := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{X: 2, Y: 0.5}, 1))
	distant := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{X: 15, Y: 0.5}, 1))
	w.AddJoint(physics3d.NewDistanceJoint(left, right, physics3d.Vec3{}, physics3d.Vec3{}, 4, 4))

	for range 120 {
		w.Step(step60)
	}
	if !left.Sleeping || !right.Sleeping || !distant.Sleeping {
		t.Fatalf("expected all bodies asleep: left=%v right=%v distant=%v", left.Sleeping, right.Sleeping, distant.Sleeping)
	}

	// WakeInBox touching only left must also wake jointed partner right, without waking distant.
	w.WakeInBox(physics3d.Box{Center: left.Pos, HalfExtents: physics3d.Vec3{X: 0.2, Y: 0.2, Z: 0.2}})
	if left.Sleeping || right.Sleeping {
		t.Fatalf("WakeInBox did not wake jointed island: left=%v right=%v", left.Sleeping, right.Sleeping)
	}
	if !distant.Sleeping {
		t.Fatal("WakeInBox woke unrelated distant body")
	}

	// Let them sleep again, then wake left with a slow velocity below 0.2 m/s.
	for range 120 {
		w.Step(step60)
	}
	if !left.Sleeping || !right.Sleeping {
		t.Fatalf("expected jointed pair to sleep again: left=%v right=%v", left.Sleeping, right.Sleeping)
	}
	left.Vel = physics3d.Vec3{X: -0.05}
	w.Step(step60)
	if left.Sleeping || right.Sleeping {
		t.Fatalf("slow velocity on left did not wake jointed right: left=%v right=%v", left.Sleeping, right.Sleeping)
	}
}

func TestSleepSettingsAndAllowSleepSnapshotRoundTrip(t *testing.T) {
	const dt = time.Second / 60
	unit := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})

	w := physics3d.NewWorld()
	w.DisableSleep = false
	w.SleepLinearThreshold = 0.08
	w.SleepAngularThreshold = 0.09
	w.TimeToSleep = 0.3
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 0.5, Z: 20}}),
		physics3d.Vec3{Y: -0.5},
	))
	sleeper := w.AddBody(physics3d.NewDynamicBody(unit, physics3d.Vec3{X: 0, Y: 0.5}, 1))
	sleeper.Tag = "sleeper"
	awakeDef := physics3d.NewDynamicBody(unit, physics3d.Vec3{X: 5, Y: 0.5}, 1)
	awakeDef.Tag = "awake"
	awakeDef.AllowSleep = false
	awake := w.AddBody(awakeDef)

	for range 60 {
		w.Step(dt)
	}
	if !sleeper.Sleeping || awake.Sleeping {
		t.Fatalf("before save: sleeper.Sleeping=%v awake.Sleeping=%v", sleeper.Sleeping, awake.Sleeping)
	}

	snap := w.Save()
	if snap.Version != physics3d.SnapshotVersion {
		t.Fatalf("snap.Version = %d, want %d", snap.Version, physics3d.SnapshotVersion)
	}
	snapChecksum := w.Checksum()

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded physics3d.WorldSnapshot
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	again, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("json.Marshal(decoded): %v", err)
	}
	if string(again) != string(raw) {
		t.Fatalf("WorldSnapshot did not round-trip unchanged through JSON")
	}

	// Step original further after waking sleeper via DisableSleep=true.
	w.DisableSleep = true
	for range 30 {
		w.Step(dt)
	}
	wantAfterDisable := w.Checksum()

	// Restore into a fresh world and verify settings, AllowSleep, and checksums.
	fresh := physics3d.NewWorld()
	if err := fresh.Restore(decoded); err != nil {
		t.Fatalf("fresh.Restore: %v", err)
	}
	if fresh.DisableSleep != false ||
		fresh.SleepLinearThreshold != 0.08 ||
		fresh.SleepAngularThreshold != 0.09 ||
		fresh.TimeToSleep != 0.3 {
		t.Fatalf("restored world sleep settings mismatch: %+v", fresh)
	}
	if fresh.Checksum() != snapChecksum {
		t.Fatalf("fresh checksum = %#x, want %#x", fresh.Checksum(), snapChecksum)
	}
	var freshSleeper, freshAwake *physics3d.Body
	for _, b := range fresh.Bodies() {
		switch b.Tag {
		case "sleeper":
			freshSleeper = b
		case "awake":
			freshAwake = b
		}
	}
	if !freshSleeper.AllowSleep || !freshSleeper.Sleeping {
		t.Fatalf("restored sleeper: AllowSleep=%v Sleeping=%v", freshSleeper.AllowSleep, freshSleeper.Sleeping)
	}
	if freshAwake.AllowSleep || freshAwake.Sleeping {
		t.Fatalf("restored awake: AllowSleep=%v Sleeping=%v", freshAwake.AllowSleep, freshAwake.Sleeping)
	}

	// Stepping fresh with DisableSleep=false keeps freshAwake awake and freshSleeper asleep.
	for range 30 {
		fresh.Step(dt)
	}
	if !freshSleeper.Sleeping || freshAwake.Sleeping {
		t.Fatalf("after stepping fresh: sleeper=%v awake=%v", freshSleeper.Sleeping, freshAwake.Sleeping)
	}

	// Restore again and verify identical checksum after toggling DisableSleep=true.
	if err := fresh.Restore(decoded); err != nil {
		t.Fatalf("second fresh.Restore: %v", err)
	}
	fresh.DisableSleep = true
	for range 30 {
		fresh.Step(dt)
	}
	if fresh.Checksum() != wantAfterDisable {
		t.Fatalf("after DisableSleep=true checksum = %#x, want %#x", fresh.Checksum(), wantAfterDisable)
	}

	// Legacy v2 snapshot (without AllowSleep / AllowSleepSet) defaults AllowSleep to true.
	v2 := decoded
	v2.Version = 2
	for i := range v2.Bodies {
		v2.Bodies[i].AllowSleep = false
		v2.Bodies[i].AllowSleepSet = false
	}
	legacy := physics3d.NewWorld()
	if err := legacy.Restore(v2); err != nil {
		t.Fatalf("legacy v2 Restore: %v", err)
	}
	for _, b := range legacy.Bodies() {
		if !b.AllowSleep {
			t.Fatalf("legacy v2 body %q restored with AllowSleep=false, want true", b.Tag)
		}
	}
}
