package physics_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/nsgo/core"
)

// boxPyramid builds a floor whose top is at y = 400 and a pyramid of 20 boxes of size 20 (rows of 6, 5, 4, 3 and 2) on it.
func boxPyramid() (*physics.RigidWorld, []*physics.RigidBody) {
	w := &physics.RigidWorld{GravityY: 600}
	w.AddBody(physics.NewStaticRigidBox(200, 410, 600, 20))
	const size = 20
	var boxes []*physics.RigidBody
	y := float32(400 - size/2)
	for _, n := range []int{6, 5, 4, 3, 2} {
		x0 := 200 - float32(n-1)*size*0.5
		for i := range n {
			b := physics.NewRigidBox(x0+float32(i)*size, y, size, size, 1)
			boxes = append(boxes, b)
			w.AddBody(b)
		}
		y -= size
	}
	return w, boxes
}

func allSleeping(bodies []*physics.RigidBody) bool {
	for _, b := range bodies {
		if !b.Sleeping() {
			return false
		}
	}
	return true
}

func TestRigidPyramidSleepsWithinThreeSeconds(t *testing.T) {
	w, boxes := boxPyramid()
	starts := make([]core.Point, len(boxes))
	for i, b := range boxes {
		starts[i] = core.Point{X: core.Points(b.X), Y: core.Points(b.Y)}
	}
	steps := 0
	for ; steps < 180 && !allSleeping(boxes); steps++ {
		w.Step(step)
	}
	if !allSleeping(boxes) {
		t.Fatalf("pyramid still awake after 3 s")
	}
	t.Logf("pyramid asleep after %d steps", steps)
	for i, b := range boxes {
		if math.Abs(float64(b.X)-float64(starts[i].X)) > 1 || math.Abs(float64(b.Y)-float64(starts[i].Y)) > 1 || math.Abs(float64(b.Angle)) > 0.02 {
			t.Fatalf("box %d settled at (%v, %v) angle %v, started at %v", i, b.X, b.Y, b.Angle, starts[i])
		}
		if b.VX != 0 || b.VY != 0 || b.AngularVel != 0 {
			t.Fatalf("sleeping box %d keeps velocity (%v, %v, %v)", i, b.VX, b.VY, b.AngularVel)
		}
	}
	// Asleep, the pyramid does not move at all.
	before := w.Checksum()
	for range 60 {
		w.Step(step)
	}
	if !allSleeping(boxes) {
		t.Fatal("pyramid woke without a disturbance")
	}
	if after := w.Checksum(); after != before {
		t.Fatalf("sleeping pyramid changed: checksum %x, was %x", after, before)
	}
}

func TestRigidSleepingIslandWakesOnImpulse(t *testing.T) {
	w, boxes := boxPyramid()
	for range 180 {
		w.Step(step)
	}
	if !allSleeping(boxes) {
		t.Fatal("pyramid did not sleep")
	}
	// An impulse on the top box wakes it at once and the rest of the pyramid, its island, on the next step.
	top := boxes[len(boxes)-1]
	top.ApplyImpulse(0, -50)
	if top.Sleeping() {
		t.Fatal("ApplyImpulse did not wake the body")
	}
	w.Step(step)
	for i, b := range boxes {
		if b.Sleeping() {
			t.Fatalf("box %d still asleep after its island was disturbed", i)
		}
	}
}

func TestRigidSleepWakeTriggers(t *testing.T) {
	settle := func(t *testing.T) (*physics.RigidWorld, *physics.RigidBody, *physics.RigidBody) {
		t.Helper()
		w := &physics.RigidWorld{GravityY: 600}
		floor := physics.NewStaticRigidBox(100, 110, 400, 20)
		crate := physics.NewRigidBox(100, 90, 20, 20, 1)
		w.AddBody(floor, crate)
		for range 120 {
			w.Step(step)
		}
		if !crate.Sleeping() {
			t.Fatal("crate did not fall asleep on the floor")
		}
		return w, floor, crate
	}
	cases := []struct {
		name    string
		disturb func(w *physics.RigidWorld, floor, crate *physics.RigidBody)
	}{
		{"Wake", func(_ *physics.RigidWorld, _, crate *physics.RigidBody) { crate.Wake() }},
		{"ApplyForce", func(_ *physics.RigidWorld, _, crate *physics.RigidBody) { crate.ApplyForce(10, 0) }},
		{"ApplyTorque", func(_ *physics.RigidWorld, _, crate *physics.RigidBody) { crate.ApplyTorque(10) }},
		{"ApplyImpulseAtPoint", func(_ *physics.RigidWorld, _, crate *physics.RigidBody) {
			crate.ApplyImpulseAtPoint(5, 0, core.Point{X: 100, Y: 85})
		}},
		{"velocity set", func(_ *physics.RigidWorld, _, crate *physics.RigidBody) { crate.VX = 30 }},
		{"moved by hand", func(_ *physics.RigidWorld, _, crate *physics.RigidBody) { crate.X += 5 }},
		{"support removed", func(w *physics.RigidWorld, floor, _ *physics.RigidBody) { w.RemoveBody(floor) }},
		{"support moved", func(_ *physics.RigidWorld, floor, _ *physics.RigidBody) { floor.Y += 5 }},
		{"gravity scale", func(_ *physics.RigidWorld, _, crate *physics.RigidBody) { crate.SetGravityScale(-1) }},
		{"world gravity", func(w *physics.RigidWorld, _, _ *physics.RigidBody) { w.GravityX = 300 }},
		{"sleep disabled", func(w *physics.RigidWorld, _, _ *physics.RigidBody) { w.DisableSleep = true }},
		{"area added", func(w *physics.RigidWorld, _, _ *physics.RigidBody) {
			w.AddTrigger(&physics.Trigger{Shape: physics.BoxShape(physics.Box{X: 0, Y: 0, W: 200, H: 100}), Area: physics.AreaSettings{GravityMode: physics.AreaOverrideReplace, GravityY: -600}})
		}},
		{"awake body lands", func(w *physics.RigidWorld, _, _ *physics.RigidBody) {
			w.AddBody(physics.NewRigidCircle(100, 60, 6, 1))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, floor, crate := settle(t)
			c.disturb(w, floor, crate)
			woke := false
			for range 30 {
				w.Step(step)
				if !crate.Sleeping() {
					woke = true
					break
				}
			}
			if !woke {
				t.Fatal("crate stayed asleep")
			}
		})
	}
	t.Run("untouched stays asleep", func(t *testing.T) {
		w, _, crate := settle(t)
		// A body far away does not wake the crate.
		w.AddBody(physics.NewRigidCircle(300, -500, 6, 1))
		for range 30 {
			w.Step(step)
			if !crate.Sleeping() {
				t.Fatal("crate woke without a disturbance")
			}
		}
	})
}

func TestRigidJointLinksSleepIsland(t *testing.T) {
	w := &physics.RigidWorld{GravityY: 600}
	floor := physics.NewStaticRigidBox(100, 110, 400, 20)
	left := physics.NewRigidBox(60, 90, 20, 20, 1)
	right := physics.NewRigidBox(140, 90, 20, 20, 1)
	w.AddBody(floor, left, right)
	w.AddJoint(physics.NewDistanceJoint(left, right, core.Point{}, core.Point{}, 0))
	for range 120 {
		w.Step(step)
	}
	if !left.Sleeping() || !right.Sleeping() {
		t.Fatalf("jointed boxes did not sleep: %v %v", left.Sleeping(), right.Sleeping())
	}
	// Waking one end of the joint wakes the other, though the two never touch.
	left.ApplyImpulse(0, -20)
	w.Step(step)
	if right.Sleeping() {
		t.Fatal("joint did not wake its other body")
	}
}

func TestRigidDisableSleepKeepsBodiesAwake(t *testing.T) {
	w, boxes := boxPyramid()
	w.DisableSleep = true
	for range 240 {
		w.Step(step)
	}
	for i, b := range boxes {
		if b.Sleeping() {
			t.Fatalf("box %d slept with DisableSleep", i)
		}
	}
}

func TestRigidSleepThresholds(t *testing.T) {
	// A body sliding slowly on ice stays awake while it moves faster than the threshold.
	w := &physics.RigidWorld{GravityY: 600, SleepLinearThreshold: 1, TimeToSleep: 0.25}
	floor := physics.NewStaticRigidBox(500, 110, 2000, 20)
	floor.Friction = 0
	crate := physics.NewRigidBox(100, 90, 20, 20, 1)
	crate.Friction = 0
	crate.VX = 1.5
	w.AddBody(floor, crate)
	for range 120 {
		w.Step(step)
	}
	if crate.Sleeping() {
		t.Fatal("crate moving at 1.5 px/s slept under a 1 px/s threshold")
	}
	w.SleepLinearThreshold = 2
	for range 30 {
		w.Step(step)
	}
	if !crate.Sleeping() {
		t.Fatal("crate moving at 1.5 px/s did not sleep under a 2 px/s threshold within TimeToSleep")
	}
}

func TestRigidSleepSurvivesSaveRestore(t *testing.T) {
	w, _ := boxPyramid()
	for range 100 {
		w.Step(step)
	}
	raw, err := json.Marshal(w.Save())
	if err != nil {
		t.Fatal(err)
	}
	for range 120 {
		w.Step(step)
	}
	w.Bodies[len(w.Bodies)-1].ApplyImpulse(30, -40)
	for range 60 {
		w.Step(step)
	}
	want := w.Checksum()

	var snap physics.RigidWorldSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Contacts) == 0 {
		t.Fatal("snapshot has no warm-start contacts")
	}
	fresh := &physics.RigidWorld{}
	if err := fresh.Restore(snap); err != nil {
		t.Fatal(err)
	}
	for range 120 {
		fresh.Step(step)
	}
	if !allSleeping(fresh.Bodies[1:]) {
		t.Fatal("restored pyramid did not fall asleep as the original did")
	}
	fresh.Bodies[len(fresh.Bodies)-1].ApplyImpulse(30, -40)
	for range 60 {
		fresh.Step(step)
	}
	if got := fresh.Checksum(); got != want {
		t.Fatalf("restored checksum %#x, want %#x", got, want)
	}

	bad := snap
	bad.Contacts = []physics.RigidContactSnapshot{{BodyAIndex: 0, BodyBIndex: len(snap.Bodies)}}
	if err := fresh.Restore(bad); err == nil {
		t.Fatal("Restore accepted a contact with an out-of-range body")
	}
}

func TestRigidNonFiniteSleepSettingsMeanDefaults(t *testing.T) {
	nan, inf := float32(math.NaN()), float32(math.Inf(1))
	settings := []struct {
		name string
		set  func(w *physics.RigidWorld, v float32)
	}{
		{"SleepLinearThreshold", func(w *physics.RigidWorld, v float32) { w.SleepLinearThreshold = v }},
		{"SleepAngularThreshold", func(w *physics.RigidWorld, v float32) { w.SleepAngularThreshold = v }},
		{"TimeToSleep", func(w *physics.RigidWorld, v float32) { w.TimeToSleep = v }},
	}
	for _, s := range settings {
		for _, v := range []float32{nan, inf, -inf} {
			t.Run(s.name, func(t *testing.T) {
				// A falling ball never rests, and a crate on the floor sleeps within the default time.
				w := &physics.RigidWorld{GravityY: 600}
				s.set(w, v)
				floor := physics.NewStaticRigidBox(500, 110, 2000, 20)
				crate := physics.NewRigidBox(100, 90, 20, 20, 1)
				ball := physics.NewRigidCircle(600, -2000, 6, 1)
				w.AddBody(floor, crate, ball)
				for i := range 90 {
					w.Step(step)
					if ball.Sleeping() {
						t.Fatalf("%v = %v: falling ball slept at step %d", s.name, v, i)
					}
				}
				if !crate.Sleeping() {
					t.Fatalf("%v = %v: resting crate still awake after 1.5 s", s.name, v)
				}
			})
		}
	}
}

func TestRigidMouseJointWakesAndHoldsBody(t *testing.T) {
	// A sleeping crate grabbed by a MouseJoint wakes, follows the target, and stays awake while held.
	w := &physics.RigidWorld{GravityY: 600}
	floor := physics.NewStaticRigidBox(500, 110, 2000, 20)
	crate := physics.NewRigidBox(100, 90, 20, 20, 1)
	w.AddBody(floor, crate)
	for range 120 {
		w.Step(step)
	}
	if !crate.Sleeping() {
		t.Fatal("crate did not sleep on the floor")
	}
	mouse := physics.NewMouseJoint(crate, core.Point{X: 100, Y: 90}, 5000)
	mouse.Target = core.Point{X: 100, Y: 40}
	w.Joints = append(w.Joints, mouse)
	for range 120 {
		w.Step(step)
		if crate.Sleeping() {
			t.Fatal("crate held by a mouse joint fell asleep")
		}
	}
	if crate.Y > 60 {
		t.Fatalf("crate at y=%v, want lifted toward the target at y=40", crate.Y)
	}
}

func TestRigidRemoveSupportSaveRestoreMatchesOriginal(t *testing.T) {
	// Removing a sleeping stack's static floor and saving before the next Step must produce bit-identical wake and fall behavior after Restore.
	w := &physics.RigidWorld{GravityY: 600}
	floor := physics.NewStaticRigidBox(100, 110, 400, 20)
	bottom := physics.NewRigidBox(100, 90, 20, 20, 1)
	top := physics.NewRigidBox(100, 70, 20, 20, 1)
	w.AddBody(floor, bottom, top)
	for range 120 {
		w.Step(step)
	}
	if !bottom.Sleeping() || !top.Sleeping() {
		t.Fatalf("stack did not sleep: bottom=%v top=%v", bottom.Sleeping(), top.Sleeping())
	}
	if !w.RemoveBody(floor) {
		t.Fatal("RemoveBody(floor) returned false")
	}
	raw, err := json.Marshal(w.Save())
	if err != nil {
		t.Fatal(err)
	}
	var snap physics.RigidWorldSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	fresh := &physics.RigidWorld{}
	if err := fresh.Restore(snap); err != nil {
		t.Fatal(err)
	}
	for range 30 {
		w.Step(step)
		fresh.Step(step)
	}
	if bottom.Sleeping() || top.Sleeping() {
		t.Fatalf("original stack stayed asleep: bottom=%v top=%v", bottom.Sleeping(), top.Sleeping())
	}
	if bottom.Y <= 110 || top.Y <= 90 {
		t.Fatalf("original stack did not fall: bottom.Y=%v top.Y=%v", bottom.Y, top.Y)
	}
	if fresh.Bodies[0].Sleeping() || fresh.Bodies[1].Sleeping() {
		t.Fatalf("restored stack stayed asleep: bottom=%v top=%v", fresh.Bodies[0].Sleeping(), fresh.Bodies[1].Sleeping())
	}
	if got, want := fresh.Checksum(), w.Checksum(); got != want {
		t.Fatalf("restored checksum %#x != original %#x", got, want)
	}
}

func TestRigidJointSleepWakeOnAnchorAndMutations(t *testing.T) {
	settleHanging := func(t *testing.T) (*physics.RigidWorld, *physics.RigidBody, *physics.RigidBody, physics.Joint) {
		t.Helper()
		w := &physics.RigidWorld{GravityY: 600}
		anchor := physics.NewStaticRigidBox(100, 40, 10, 10)
		bob := physics.NewRigidBox(100, 100, 20, 20, 1)
		bob.LinearDamping = 2
		j := physics.NewDistanceJoint(anchor, bob, core.Point{}, core.Point{}, 60)
		w.AddBody(anchor, bob)
		w.AddJoint(j)
		for range 120 {
			w.Step(step)
		}
		if !bob.Sleeping() {
			t.Fatal("hanging bob did not sleep")
		}
		return w, anchor, bob, j
	}

	t.Run("move static anchor", func(t *testing.T) {
		w, anchor, bob, _ := settleHanging(t)
		anchor.X += 60
		for range 30 {
			w.Step(step)
		}
		if bob.Sleeping() {
			t.Fatal("bob stayed asleep after its static joint anchor moved")
		}
		if bob.X <= 110 {
			t.Fatalf("bob did not follow moved anchor: bob.X=%v", bob.X)
		}
	})

	t.Run("remove static anchor", func(t *testing.T) {
		w, anchor, bob, _ := settleHanging(t)
		if !w.RemoveBody(anchor) {
			t.Fatal("RemoveBody(anchor) returned false")
		}
		if bob.Sleeping() {
			t.Fatal("RemoveBody(anchor) did not wake jointed bob immediately")
		}
	})

	t.Run("remove static anchor by slice edit", func(t *testing.T) {
		w, _, bob, _ := settleHanging(t)
		w.Bodies = []*physics.RigidBody{bob}
		w.Step(step)
		if bob.Sleeping() {
			t.Fatal("bob stayed asleep after static joint anchor left w.Bodies")
		}
	})

	t.Run("RemoveJoint", func(t *testing.T) {
		w, _, bob, j := settleHanging(t)
		startY := bob.Y
		if !w.RemoveJoint(j) {
			t.Fatal("RemoveJoint returned false")
		}
		if bob.Sleeping() {
			t.Fatal("RemoveJoint did not wake sleeping bob")
		}
		for range 20 {
			w.Step(step)
		}
		if bob.Y <= startY+10 {
			t.Fatalf("bob did not fall after RemoveJoint: startY=%v y=%v", startY, bob.Y)
		}
	})

	t.Run("AddJoint", func(t *testing.T) {
		w := &physics.RigidWorld{GravityY: 600}
		floor := physics.NewStaticRigidBox(200, 110, 600, 20)
		left := physics.NewRigidBox(100, 90, 20, 20, 1)
		right := physics.NewRigidBox(220, 90, 20, 20, 1)
		left.Friction = 0
		right.Friction = 0
		w.AddBody(floor, left, right)
		for range 120 {
			w.Step(step)
		}
		if !left.Sleeping() || !right.Sleeping() {
			t.Fatalf("crates did not sleep: left=%v right=%v", left.Sleeping(), right.Sleeping())
		}
		w.AddJoint(physics.NewSpringJoint(left, right, core.Point{}, core.Point{}, 40, 200, 5))
		if left.Sleeping() || right.Sleeping() {
			t.Fatalf("AddJoint did not wake sleeping crates: left=%v right=%v", left.Sleeping(), right.Sleeping())
		}
		for range 20 {
			w.Step(step)
		}
		if right.X-left.X >= 115 {
			t.Fatalf("spring joint did not pull woken crates together: dist=%v", right.X-left.X)
		}
	})
}

func TestRigidNaNVelocityDoesNotSleep(t *testing.T) {
	nan := float32(math.NaN())
	cases := []struct {
		name string
		set  func(b *physics.RigidBody)
	}{
		{"linear VX", func(b *physics.RigidBody) { b.VX = nan }},
		{"linear VY", func(b *physics.RigidBody) { b.VY = nan }},
		{"angular", func(b *physics.RigidBody) { b.AngularVel = nan }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &physics.RigidWorld{}
			b := physics.NewRigidBox(100, 100, 20, 20, 1)
			tc.set(b)
			w.AddBody(b)
			for i := range 90 {
				w.Step(step)
				if b.Sleeping() {
					t.Fatalf("%s = NaN slept at step %d", tc.name, i)
				}
			}
		})
	}
}
