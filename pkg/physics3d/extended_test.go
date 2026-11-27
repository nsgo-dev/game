package physics3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/physics3d"
)

func isFiniteVec(v physics3d.Vec3) bool {
	return !math.IsNaN(float64(v.X)) && !math.IsInf(float64(v.X), 0) &&
		!math.IsNaN(float64(v.Y)) && !math.IsInf(float64(v.Y), 0) &&
		!math.IsNaN(float64(v.Z)) && !math.IsInf(float64(v.Z), 0)
}

func isFiniteQuat(q physics3d.Quat) bool {
	return !math.IsNaN(float64(q.X)) && !math.IsInf(float64(q.X), 0) &&
		!math.IsNaN(float64(q.Y)) && !math.IsInf(float64(q.Y), 0) &&
		!math.IsNaN(float64(q.Z)) && !math.IsInf(float64(q.Z), 0) &&
		!math.IsNaN(float64(q.W)) && !math.IsInf(float64(q.W), 0)
}

func TestQuatMath(t *testing.T) {
	id := physics3d.QuatIdentity()
	v := physics3d.Vec3{X: 1, Y: 0, Z: 0}
	if got := id.RotateVec(v); !approxVec(got, v, 1e-6) {
		t.Fatalf("identity rotate = %v, want %v", got, v)
	}
	// Zero quaternion behaves as identity.
	if got := (physics3d.Quat{}).RotateVec(v); !approxVec(got, v, 1e-6) {
		t.Fatalf("zero quat rotate = %v, want %v", got, v)
	}

	// +90 deg around +Y maps +X -> -Z in right-handed Y-up coordinates.
	q90 := physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, float32(math.Pi/2))
	if got := q90.RotateVec(v); !approxVec(got, physics3d.Vec3{Z: -1}, 1e-5) {
		t.Fatalf("90 deg Y rotate (+X) = %v, want (0,0,-1)", got)
	}

	// Slerp halfway gives 45 degrees.
	q45 := id.Slerp(q90, 0.5)
	if ang := id.AngleTo(q45); !approx(ang, float32(math.Pi/4), 1e-4) {
		t.Fatalf("Slerp(0.5) angle = %v, want pi/4", ang)
	}

	// Conjugate inverts rotation.
	back := q90.Conjugate().RotateVec(q90.RotateVec(physics3d.Vec3{X: 2, Y: 3, Z: -1}))
	if !approxVec(back, physics3d.Vec3{X: 2, Y: 3, Z: -1}, 1e-5) {
		t.Fatalf("round-trip rotate = %v", back)
	}
}

func TestLayersFilteringCollisionsTriggersAndRaycasts(t *testing.T) {
	layerPlayer := physics3d.LayerBit(0)
	layerEnemy := physics3d.LayerBit(1)
	layerWorld := physics3d.LayerBit(2)
	layerTrigger := physics3d.LayerBit(3)

	if physics3d.LayerBit(32) != 0 {
		t.Fatalf("LayerBit(32) = %v, want 0", physics3d.LayerBit(32))
	}

	// Default zero Layer/Mask acts as LayerAll.
	a := &physics3d.Body{}
	b := &physics3d.Body{Layer: layerEnemy, Mask: layerPlayer | layerWorld}
	if !physics3d.CanCollide(a, b) {
		t.Fatal("zero layer/mask should default to LayerAll and collide")
	}

	// Asymmetric check: both sides must allow the other.
	p := &physics3d.Body{Layer: layerPlayer, Mask: layerWorld}
	e := &physics3d.Body{Layer: layerEnemy, Mask: layerPlayer | layerWorld}
	if physics3d.CanCollide(p, e) {
		t.Fatal("asymmetric mask must not collide when one side excludes the other")
	}

	// Physics step respects layer masks: non-colliding spheres pass through each other,
	// while colliding spheres bounce apart.
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}

	s1 := physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), physics3d.Vec3{X: -1}, 1)
	s1.Vel = physics3d.Vec3{X: 4}
	s1.Layer = layerPlayer
	s1.Mask = layerWorld
	s1.LinearDamping = 0
	b1 := w.AddBody(s1)

	s2 := physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), physics3d.Vec3{X: 1}, 1)
	s2.Vel = physics3d.Vec3{X: -4}
	s2.Layer = layerEnemy
	s2.Mask = layerWorld
	s2.LinearDamping = 0
	b2 := w.AddBody(s2)

	dt := time.Second / 60
	for range 60 {
		w.Step(dt)
	}
	if b1.Pos.X < 2.0 || b2.Pos.X > -2.0 {
		t.Fatalf("layer-filtered bodies collided instead of passing through: b1=%v b2=%v", b1.Pos, b2.Pos)
	}

	// RaycastLayer & OverlapLayer filter strictly by mask.
	w2 := physics3d.NewWorld()
	bWorld := w2.AddBody(physics3d.Body{
		Tag:    "world",
		Shape:  physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
		Pos:    physics3d.Vec3{X: 2},
		Static: true,
		Layer:  layerWorld,
	})
	bEnemy := w2.AddBody(physics3d.Body{
		Tag:    "enemy",
		Shape:  physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
		Pos:    physics3d.Vec3{X: 4},
		Static: true,
		Layer:  layerEnemy,
	})
	bTrig := w2.AddBody(physics3d.Body{
		Tag:       "trigger",
		Shape:     physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
		Pos:       physics3d.Vec3{X: 6},
		Static:    true,
		IsTrigger: true,
		Layer:     layerTrigger,
	})

	enemyHits := w2.RaycastLayer(physics3d.Vec3{}, physics3d.Vec3{X: 10}, layerEnemy)
	if len(enemyHits) != 1 || enemyHits[0].Body.ID != bEnemy.ID {
		t.Fatalf("RaycastLayer(enemy) = %+v, want only enemy", enemyHits)
	}
	firstTrig, ok := w2.RaycastFirstLayer(physics3d.Vec3{}, physics3d.Vec3{X: 10}, layerTrigger)
	if !ok || firstTrig.Body.ID != bTrig.ID {
		t.Fatalf("RaycastFirstLayer(trigger) = %+v, %v", firstTrig, ok)
	}
	if empty := w2.RaycastLayer(physics3d.Vec3{}, physics3d.Vec3{X: 10}, 0); len(empty) != 0 {
		t.Fatalf("RaycastLayer(0) = %d hits, want 0", len(empty))
	}

	ov := w2.OverlapLayer(physics3d.BoxShape(physics3d.BoxFromMinMax(
		physics3d.Vec3{X: 0, Y: -1, Z: -1},
		physics3d.Vec3{X: 8, Y: 1, Z: 1},
	)), layerWorld|layerTrigger)
	if len(ov) != 2 || ov[0].Body.ID != bWorld.ID || ov[1].Body.ID != bTrig.ID {
		t.Fatalf("OverlapLayer(world|trigger) got %d hits, want world & trigger", len(ov))
	}
}

func TestTriggerAndContactEventsStrictlyOnce(t *testing.T) {
	t.Run("TriggerEnterStayExitAndZeroImpulse", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}

		trig := physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}),
			physics3d.Vec3{X: 3},
		)
		trig.IsTrigger = true
		trigBody := w.AddBody(trig)

		mover := physics3d.NewDynamicBody(
			physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
			physics3d.Vec3{X: 0},
			1,
		)
		mover.Vel = physics3d.Vec3{X: 6}
		mover.LinearDamping = 0
		moverBody := w.AddBody(mover)

		var enters, stays, exits, solidEvents int
		w.OnTrigger = func(ev physics3d.ContactEvent) {
			if !ev.IsTrigger {
				t.Errorf("OnTrigger received non-trigger event: %+v", ev)
			}
			switch ev.Kind {
			case physics3d.ContactEnter:
				enters++
			case physics3d.ContactStay:
				stays++
			case physics3d.ContactExit:
				exits++
			}
		}
		w.OnContact = func(ev physics3d.ContactEvent) {
			solidEvents++
		}

		dt := time.Second / 60
		for range 90 {
			w.Step(dt)
		}

		if enters != 1 {
			t.Fatalf("trigger Enter count = %d, want strictly 1", enters)
		}
		if stays < 5 {
			t.Fatalf("trigger Stay count = %d, want >= 5", stays)
		}
		if exits != 1 {
			t.Fatalf("trigger Exit count = %d, want strictly 1", exits)
		}
		if solidEvents != 0 {
			t.Fatalf("OnContact fired %d times for trigger pair, want 0", solidEvents)
		}
		// Trigger must not alter velocity or trajectory (6 m/s * 1.5s = 9.0m).
		if !approx(moverBody.Vel.X, 6.0, 1e-4) || !approx(moverBody.Pos.X, 9.0, 1e-3) {
			t.Fatalf("trigger altered mover motion: pos=%v vel=%v", moverBody.Pos, moverBody.Vel)
		}

		// Re-entering and removing body inside trigger emits Exit strictly once.
		moverBody.Pos = physics3d.Vec3{X: 3}
		moverBody.Vel = physics3d.Vec3{}
		w.Step(dt)
		if enters != 2 {
			t.Fatalf("expected second Enter on re-entry, got %d", enters)
		}
		if len(w.ContactEvents()) != 1 || w.ContactEvents()[0].Kind != physics3d.ContactEnter {
			t.Fatalf("ContactEvents() = %+v, want single ContactEnter", w.ContactEvents())
		}

		w.RemoveBody(trigBody.ID)
		if exits != 2 {
			t.Fatalf("RemoveBody should emit trigger Exit immediately: exits=%d, want 2", exits)
		}
		// Next step must NOT emit a duplicate Exit.
		w.Step(dt)
		if exits != 2 {
			t.Fatalf("duplicate Exit emitted on Step after RemoveBody: exits=%d", exits)
		}
	})

	t.Run("SolidContactEnterStayWhileSleepingAndExitOnRemoval", func(t *testing.T) {
		w := physics3d.NewWorld()
		floor := w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 1, Z: 10}}),
			physics3d.Vec3{Y: -1},
		))
		box := w.AddBody(physics3d.NewDynamicBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
			physics3d.Vec3{Y: 1.0},
			1,
		))

		var enters, stays, exits int
		w.OnContact = func(ev physics3d.ContactEvent) {
			if ev.IsTrigger {
				t.Errorf("OnContact received trigger event: %+v", ev)
			}
			switch ev.Kind {
			case physics3d.ContactEnter:
				enters++
			case physics3d.ContactStay:
				stays++
			case physics3d.ContactExit:
				exits++
			}
		}

		dt := time.Second / 60
		for range 120 {
			w.Step(dt)
		}
		if !box.Sleeping {
			t.Fatalf("expected box to sleep on floor (vel=%v)", box.Vel)
		}
		if enters != 1 || exits != 0 || stays < 30 {
			t.Fatalf("resting contact lifecycle: enters=%d stays=%d exits=%d", enters, stays, exits)
		}

		w.RemoveBody(floor.ID)
		if exits != 1 {
			t.Fatalf("removing floor should emit ContactExit once, got exits=%d", exits)
		}
		w.Step(dt)
		if exits != 1 {
			t.Fatalf("duplicate ContactExit after floor removal: exits=%d", exits)
		}
	})
}

func TestRemoveBodyInsideContactAndTriggerCallbacks(t *testing.T) {
	type evKey struct {
		kind      physics3d.ContactEventKind
		a, b      int
		isTrigger bool
	}
	toKeys := func(evs []physics3d.ContactEvent) []evKey {
		out := make([]evKey, len(evs))
		for i, ev := range evs {
			out[i] = evKey{kind: ev.Kind, a: ev.BodyA.ID, b: ev.BodyB.ID, isTrigger: ev.IsTrigger}
		}
		return out
	}
	assertEvents := func(t *testing.T, label string, got, want []evKey) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: got %+v, want %+v", label, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s[%d]: got %+v, want %+v\nfull got:  %+v\nfull want: %+v", label, i, got[i], want[i], got, want)
			}
		}
	}

	dt := time.Second / 60
	unitBox := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})

	t.Run("OnContactStayRemoveMiddleBody", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}

		ground := w.AddBody(physics3d.NewStaticBody(unitBox, physics3d.Vec3{Y: 0}))
		boxX := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: 0.9}, 1))
		boxY := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: 1.8}, 1))

		// Step 1: establish (1,2) and (2,3).
		w.Step(dt)
		assertEvents(t, "step1 ContactEvents", toKeys(w.ContactEvents()), []evKey{
			{physics3d.ContactEnter, ground.ID, boxX.ID, false},
			{physics3d.ContactEnter, boxX.ID, boxY.ID, false},
		})

		// Keep overlaps alive for step 2.
		boxX.Pos = physics3d.Vec3{Y: 0.9}
		boxY.Pos = physics3d.Vec3{Y: 1.8}

		var seen []evKey
		removed := false
		w.OnContact = func(ev physics3d.ContactEvent) {
			seen = append(seen, evKey{ev.Kind, ev.BodyA.ID, ev.BodyB.ID, ev.IsTrigger})
			if !removed && ev.Kind == physics3d.ContactStay && (ev.BodyA.ID == boxX.ID || ev.BodyB.ID == boxX.ID) {
				removed = true
				w.RemoveBody(boxX.ID)
			}
		}

		w.Step(dt)
		want := []evKey{
			{physics3d.ContactStay, ground.ID, boxX.ID, false},
			{physics3d.ContactExit, ground.ID, boxX.ID, false},
			{physics3d.ContactExit, boxX.ID, boxY.ID, false},
		}
		assertEvents(t, "step2 callbacks", seen, want)
		assertEvents(t, "step2 ContactEvents", toKeys(w.ContactEvents()), want)

		// Step 3: no stale Stay or duplicate Exit for removed boxX.
		seen = nil
		w.Step(dt)
		assertEvents(t, "step3 callbacks", seen, nil)
		assertEvents(t, "step3 ContactEvents", toKeys(w.ContactEvents()), nil)
	})

	t.Run("OnContactStayRemoveBodyA", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}

		// Body 1 touches both body 2 and body 3, so body 1 is BodyA in both pairs.
		ground := w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 5, Y: 0.5, Z: 5}}),
			physics3d.Vec3{Y: 0},
		))
		boxA := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{X: -2, Y: 0.9}, 1))
		boxB := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{X: 2, Y: 0.9}, 1))

		w.Step(dt)
		boxA.Pos = physics3d.Vec3{X: -2, Y: 0.9}
		boxB.Pos = physics3d.Vec3{X: 2, Y: 0.9}

		var seen []evKey
		removed := false
		w.OnContact = func(ev physics3d.ContactEvent) {
			seen = append(seen, evKey{ev.Kind, ev.BodyA.ID, ev.BodyB.ID, ev.IsTrigger})
			if !removed && ev.Kind == physics3d.ContactStay && ev.BodyA.ID == ground.ID {
				removed = true
				w.RemoveBody(ground.ID)
			}
		}

		w.Step(dt)
		want := []evKey{
			{physics3d.ContactStay, ground.ID, boxA.ID, false},
			{physics3d.ContactExit, ground.ID, boxA.ID, false},
			{physics3d.ContactExit, ground.ID, boxB.ID, false},
		}
		assertEvents(t, "callbacks", seen, want)
		assertEvents(t, "ContactEvents", toKeys(w.ContactEvents()), want)
	})

	t.Run("OnContactEnterSuppressesUndeliveredEnterWithoutOrphanExit", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}

		ground := w.AddBody(physics3d.NewStaticBody(unitBox, physics3d.Vec3{Y: 0}))
		boxX := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: 0.9}, 1))
		boxY := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: 1.8}, 1))

		var seen []evKey
		w.OnContact = func(ev physics3d.ContactEvent) {
			seen = append(seen, evKey{ev.Kind, ev.BodyA.ID, ev.BodyB.ID, ev.IsTrigger})
			if ev.Kind == physics3d.ContactEnter && ev.BodyA.ID == ground.ID && ev.BodyB.ID == boxX.ID {
				w.RemoveBody(boxX.ID)
			}
		}

		w.Step(dt)
		// (boxX, boxY) never received ContactEnter, so it must receive neither Enter nor an orphan Exit.
		want := []evKey{
			{physics3d.ContactEnter, ground.ID, boxX.ID, false},
			{physics3d.ContactExit, ground.ID, boxX.ID, false},
		}
		assertEvents(t, "callbacks", seen, want)
		assertEvents(t, "ContactEvents", toKeys(w.ContactEvents()), want)

		seen = nil
		w.Step(dt)
		assertEvents(t, "next step callbacks", seen, nil)
		_ = boxY
	})

	t.Run("OnContactEnterWithPreexistingOverlap", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}

		// Body 1 starts far away; bodies 2 and 3 overlap first.
		blocker := w.AddBody(physics3d.NewStaticBody(unitBox, physics3d.Vec3{Y: 10}))
		boxX := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: 0.9}, 1))
		boxY := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: 1.8}, 1))

		w.Step(dt)
		assertEvents(t, "step1", toKeys(w.ContactEvents()), []evKey{
			{physics3d.ContactEnter, boxX.ID, boxY.ID, false},
		})

		// Move blocker into contact with boxX so step 2 queues Enter(1,2) before Stay(2,3).
		blocker.Pos = physics3d.Vec3{Y: 0}
		boxX.Pos = physics3d.Vec3{Y: 0.9}
		boxY.Pos = physics3d.Vec3{Y: 1.8}

		var seen []evKey
		w.OnContact = func(ev physics3d.ContactEvent) {
			seen = append(seen, evKey{ev.Kind, ev.BodyA.ID, ev.BodyB.ID, ev.IsTrigger})
			if ev.Kind == physics3d.ContactEnter && ev.BodyA.ID == blocker.ID && ev.BodyB.ID == boxX.ID {
				w.RemoveBody(boxX.ID)
			}
		}

		w.Step(dt)
		want := []evKey{
			{physics3d.ContactEnter, blocker.ID, boxX.ID, false},
			{physics3d.ContactExit, blocker.ID, boxX.ID, false},
			{physics3d.ContactExit, boxX.ID, boxY.ID, false},
		}
		assertEvents(t, "step2 callbacks", seen, want)
		assertEvents(t, "step2 ContactEvents", toKeys(w.ContactEvents()), want)
	})

	t.Run("OnTriggerStayAndEnterRemoveBodyAAndB", func(t *testing.T) {
		// Trigger 1 (X=-0.6) and Trigger 3 (X=0.6) do not touch each other;
		// Mover 2 (X=0) overlaps both.
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}

		t1Def := physics3d.NewStaticBody(unitBox, physics3d.Vec3{X: -0.6})
		t1Def.IsTrigger = true
		t1 := w.AddBody(t1Def)

		mover := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{X: 0}, 1))

		t2Def := physics3d.NewStaticBody(unitBox, physics3d.Vec3{X: 0.6})
		t2Def.IsTrigger = true
		t2 := w.AddBody(t2Def)

		// Unrelated overlapping pair (4, 5) to verify removal of body 2 does
		// not suppress remaining queued events for unaffected pairs.
		t3Def := physics3d.NewStaticBody(unitBox, physics3d.Vec3{X: 10})
		t3Def.IsTrigger = true
		t3 := w.AddBody(t3Def)
		other := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{X: 10}, 1))

		// Step 1: Enter (t1, mover), (mover, t2), and (t3, other).
		w.Step(dt)

		var seen []evKey
		removed := false
		w.OnTrigger = func(ev physics3d.ContactEvent) {
			seen = append(seen, evKey{ev.Kind, ev.BodyA.ID, ev.BodyB.ID, ev.IsTrigger})
			if !removed && ev.Kind == physics3d.ContactStay && ev.BodyB.ID == mover.ID {
				removed = true
				w.RemoveBody(mover.ID)
			}
		}

		// Step 2: remove mover (BodyB of t1-mover, BodyA of mover-t2) inside Stay.
		// (mover, t2) gets Exit and its queued Stay is suppressed; unrelated (t3, other) still gets Stay.
		w.Step(dt)
		want := []evKey{
			{physics3d.ContactStay, t1.ID, mover.ID, true},
			{physics3d.ContactExit, t1.ID, mover.ID, true},
			{physics3d.ContactExit, mover.ID, t2.ID, true},
			{physics3d.ContactStay, t3.ID, other.ID, true},
		}
		assertEvents(t, "trigger Stay remove B callbacks", seen, want)
		assertEvents(t, "trigger Stay remove B ContactEvents", toKeys(w.ContactEvents()), want)

		// Step 3: only unrelated (t3, other) stays; no further events for mover.
		seen = nil
		w.Step(dt)
		wantStep3 := []evKey{
			{physics3d.ContactStay, t3.ID, other.ID, true},
		}
		assertEvents(t, "trigger step3 callbacks", seen, wantStep3)
		assertEvents(t, "trigger step3 ContactEvents", toKeys(w.ContactEvents()), wantStep3)

		// Now test removing Trigger BodyA inside OnTrigger Enter when two disjoint movers overlap it.
		w2 := physics3d.NewWorld()
		w2.Gravity = physics3d.Vec3{}
		wideBox := physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 2, Y: 0.5, Z: 0.5}})
		trigDef := physics3d.NewStaticBody(wideBox, physics3d.Vec3{})
		trigDef.IsTrigger = true
		trig := w2.AddBody(trigDef)
		m1 := w2.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{X: -1}, 1))
		m2 := w2.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{X: 1}, 1))

		seen = nil
		w2.OnTrigger = func(ev physics3d.ContactEvent) {
			seen = append(seen, evKey{ev.Kind, ev.BodyA.ID, ev.BodyB.ID, ev.IsTrigger})
			if ev.Kind == physics3d.ContactEnter && ev.BodyA.ID == trig.ID {
				w2.RemoveBody(trig.ID)
			}
		}

		w2.Step(dt)
		wantEnter := []evKey{
			{physics3d.ContactEnter, trig.ID, m1.ID, true},
			{physics3d.ContactExit, trig.ID, m1.ID, true},
		}
		assertEvents(t, "trigger Enter remove A callbacks", seen, wantEnter)
		assertEvents(t, "trigger Enter remove A ContactEvents", toKeys(w2.ContactEvents()), wantEnter)
		_ = m2
	})

	t.Run("MixedContactAndTriggerSuppressesQueuedTriggerStay", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}

		floor := w.AddBody(physics3d.NewStaticBody(unitBox, physics3d.Vec3{Y: 0}))
		box := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: 0.9}, 1))
		trigDef := physics3d.NewStaticBody(unitBox, physics3d.Vec3{Y: 1.0})
		trigDef.IsTrigger = true
		trig := w.AddBody(trigDef)

		w.Step(dt)
		box.Pos = physics3d.Vec3{Y: 0.9}

		var seen []evKey
		removed := false
		w.OnContact = func(ev physics3d.ContactEvent) {
			seen = append(seen, evKey{ev.Kind, ev.BodyA.ID, ev.BodyB.ID, ev.IsTrigger})
			if !removed && ev.Kind == physics3d.ContactStay && ev.BodyB.ID == box.ID {
				removed = true
				w.RemoveBody(box.ID)
			}
		}
		w.OnTrigger = func(ev physics3d.ContactEvent) {
			seen = append(seen, evKey{ev.Kind, ev.BodyA.ID, ev.BodyB.ID, ev.IsTrigger})
		}

		w.Step(dt)
		want := []evKey{
			{physics3d.ContactStay, floor.ID, box.ID, false},
			{physics3d.ContactExit, floor.ID, box.ID, false},
			{physics3d.ContactExit, box.ID, trig.ID, true},
		}
		assertEvents(t, "mixed callbacks", seen, want)
		assertEvents(t, "mixed ContactEvents", toKeys(w.ContactEvents()), want)
	})

	t.Run("SuppressesQueuedNaturalExitAndHandlesChainedRemoval", func(t *testing.T) {
		// Part 1: queued natural Exit for (2,3) is not delivered twice if body 2 is removed during Stay(1,2).
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{}
		b1 := w.AddBody(physics3d.NewStaticBody(unitBox, physics3d.Vec3{Y: 0}))
		b2 := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: 0.9}, 1))
		b3 := w.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: 1.8}, 1))
		w.Step(dt)

		b2.Pos = physics3d.Vec3{Y: 0.9}
		b3.Pos = physics3d.Vec3{Y: 10} // separates (2,3) naturally this step

		var seen []evKey
		removed := false
		w.OnContact = func(ev physics3d.ContactEvent) {
			seen = append(seen, evKey{ev.Kind, ev.BodyA.ID, ev.BodyB.ID, ev.IsTrigger})
			if !removed && ev.Kind == physics3d.ContactStay && ev.BodyB.ID == b2.ID {
				removed = true
				w.RemoveBody(b2.ID)
			}
		}
		w.Step(dt)
		want := []evKey{
			{physics3d.ContactStay, b1.ID, b2.ID, false},
			{physics3d.ContactExit, b1.ID, b2.ID, false},
			{physics3d.ContactExit, b2.ID, b3.ID, false},
		}
		assertEvents(t, "natural exit dedup callbacks", seen, want)
		assertEvents(t, "natural exit dedup ContactEvents", toKeys(w.ContactEvents()), want)

		// Part 2: chained removal inside an Exit callback (1-2, 2-3, 3-4).
		w2 := physics3d.NewWorld()
		w2.Gravity = physics3d.Vec3{}
		c1 := w2.AddBody(physics3d.NewStaticBody(unitBox, physics3d.Vec3{Y: 0}))
		c2 := w2.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: 0.9}, 1))
		c3 := w2.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: 1.8}, 1))
		c4 := w2.AddBody(physics3d.NewDynamicBody(unitBox, physics3d.Vec3{Y: 2.7}, 1))
		w2.Step(dt)

		c2.Pos = physics3d.Vec3{Y: 0.9}
		c3.Pos = physics3d.Vec3{Y: 1.8}
		c4.Pos = physics3d.Vec3{Y: 2.7}

		seen = nil
		removed2, removed3 := false, false
		w2.OnContact = func(ev physics3d.ContactEvent) {
			seen = append(seen, evKey{ev.Kind, ev.BodyA.ID, ev.BodyB.ID, ev.IsTrigger})
			if !removed2 && ev.Kind == physics3d.ContactStay && ev.BodyA.ID == c1.ID && ev.BodyB.ID == c2.ID {
				removed2 = true
				w2.RemoveBody(c2.ID)
			} else if !removed3 && ev.Kind == physics3d.ContactExit && ev.BodyA.ID == c1.ID && ev.BodyB.ID == c2.ID {
				removed3 = true
				w2.RemoveBody(c3.ID)
			}
		}
		w2.Step(dt)
		wantChained := []evKey{
			{physics3d.ContactStay, c1.ID, c2.ID, false},
			{physics3d.ContactExit, c1.ID, c2.ID, false},
			{physics3d.ContactExit, c2.ID, c3.ID, false},
			{physics3d.ContactExit, c3.ID, c4.ID, false},
		}
		assertEvents(t, "chained removal callbacks", seen, wantChained)
		assertEvents(t, "chained removal ContactEvents", toKeys(w2.ContactEvents()), wantChained)
	})
}

func TestTriMeshColliderBVHRaycastAndCollisions(t *testing.T) {
	// Build a 10x10 subdivided undulating floor mesh (200 triangles) plus a vertical wall (2 triangles).
	const gridN = 10
	verts := make([]physics3d.Vec3, 0, (gridN+1)*(gridN+1)+4)
	for gz := 0; gz <= gridN; gz++ {
		for gx := 0; gx <= gridN; gx++ {
			x := float32(gx) - 5
			z := float32(gz) - 5
			y := 0.4 * float32(math.Sin(float64(x)*0.5)) * float32(math.Cos(float64(z)*0.5))
			verts = append(verts, physics3d.Vec3{X: x, Y: y, Z: z})
		}
	}
	indices := make([]uint32, 0, gridN*gridN*6+6)
	for gz := 0; gz < gridN; gz++ {
		for gx := 0; gx < gridN; gx++ {
			i00 := uint32(gz*(gridN+1) + gx)
			i10 := i00 + 1
			i01 := i00 + uint32(gridN+1)
			i11 := i01 + 1
			indices = append(indices, i00, i11, i10, i00, i01, i11)
		}
	}
	// Vertical wall at X = 3.0 spanning Y in [-1, 4], Z in [-5, 5].
	wallBase := uint32(len(verts))
	verts = append(verts,
		physics3d.Vec3{X: 3, Y: -1, Z: -5},
		physics3d.Vec3{X: 3, Y: -1, Z: 5},
		physics3d.Vec3{X: 3, Y: 4, Z: 5},
		physics3d.Vec3{X: 3, Y: 4, Z: -5},
	)
	indices = append(indices, wallBase, wallBase+1, wallBase+2, wallBase, wallBase+2, wallBase+3)

	mesh := physics3d.NewTriMeshCollider(verts, indices)
	if len(mesh.Triangles) != gridN*gridN*2+2 {
		t.Fatalf("triangle count = %d, want %d", len(mesh.Triangles), gridN*gridN*2+2)
	}

	// Verify BVH Raycast matches exact brute-force triangle raycast across 64 rays.
	for ix := -4; ix <= 3; ix++ {
		for iz := -4; iz <= 3; iz++ {
			ray := physics3d.Ray{
				Origin:  physics3d.Vec3{X: float32(ix) + 0.37, Y: 5, Z: float32(iz) + 0.23},
				Dir:     physics3d.Vec3{X: 0.15, Y: -1, Z: -0.1},
				MaxDist: 15,
			}
			bvhHit, bvhOk := mesh.Raycast(ray)

			var bruteHit physics3d.RaycastHit
			bruteOk := false
			for _, tri := range mesh.Triangles {
				if h, ok := physics3d.RaycastTriangle(ray, tri); ok {
					if !bruteOk || h.Distance < bruteHit.Distance {
						bruteHit = h
						bruteOk = true
					}
				}
			}
			if bvhOk != bruteOk {
				t.Fatalf("ray (%d,%d) bvhOk=%v bruteOk=%v", ix, iz, bvhOk, bruteOk)
			}
			if bvhOk && (!approx(bvhHit.Distance, bruteHit.Distance, 1e-4) || !approxVec(bvhHit.Point, bruteHit.Point, 1e-4)) {
				t.Fatalf("ray (%d,%d) BVH hit %+v != brute %+v", ix, iz, bvhHit, bruteHit)
			}
		}
	}

	// Sphere, capsule, and box tests against mesh wall at X = 3.
	if c := mesh.TestShape(physics3d.SphereShape(physics3d.Sphere{Center: physics3d.Vec3{X: 2.7, Y: 2, Z: 0}, Radius: 0.5})); !c.Hit || !approx(c.Depth, 0.2, 1e-4) || !approxVec(c.Normal, physics3d.Vec3{X: -1}, 1e-4) {
		t.Fatalf("sphere vs mesh wall = %+v", c)
	}
	if c := mesh.TestShape(physics3d.BoxShape(physics3d.Box{Center: physics3d.Vec3{X: 2.6, Y: 2, Z: 0}, HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}})); !c.Hit || !approx(c.Depth, 0.1, 1e-4) {
		t.Fatalf("box vs mesh wall = %+v", c)
	}

	// World integration: dynamic sphere lands on mesh floor and CharacterController stops at mesh wall.
	w := physics3d.NewWorld()
	w.AddTriMesh(mesh)

	ball := w.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.4}),
		physics3d.Vec3{X: -1, Y: 2.5, Z: 0},
		1,
	))
	dt := time.Second / 60
	for range 120 {
		w.Step(dt)
	}
	// Ball should rest on the mesh surface above Y = -0.5 and never fall through.
	if ball.Pos.Y < -0.5 || ball.Pos.Y > 2.0 {
		t.Fatalf("ball fell through or exploded on TriMesh: pos=%v vel=%v", ball.Pos, ball.Vel)
	}

	// CharacterController walks across the undulating triangle mesh and stops at the X=3 mesh wall.
	cc := physics3d.NewCharacterController(physics3d.Vec3{X: 0, Y: 1.0, Z: 0}, 0.3, 1.8)
	for range 150 {
		cc.Move(w, physics3d.Vec3{X: 3.0}, dt)
	}
	if !cc.Grounded || !cc.OnWall || !approx(cc.Pos.X, 2.7, 0.03) {
		t.Fatalf("character vs TriMesh wall: pos=%v grounded=%v onWall=%v", cc.Pos, cc.Grounded, cc.OnWall)
	}

	// World.Raycast hits the TriMesh.
	if hit, ok := w.RaycastFirst(physics3d.Vec3{X: 0, Y: 2, Z: 0}, physics3d.Vec3{X: 6, Y: 2, Z: 0}); !ok || hit.TriMesh != mesh || !approx(hit.Point.X, 3.0, 1e-4) {
		t.Fatalf("World.RaycastFirst vs TriMesh = %+v, %v", hit, ok)
	}
}

func TestHeightfieldTerrainRaycastAndCharacterWalking(t *testing.T) {
	const (
		cols     = 33
		rows     = 33
		cellSize = float32(0.5)
	)
	origin := physics3d.Vec3{X: -8, Y: 0, Z: -8}
	heights := make([]float32, cols*rows)
	for cz := 0; cz < rows; cz++ {
		for cx := 0; cx < cols; cx++ {
			wx := origin.X + float32(cx)*cellSize
			wz := origin.Z + float32(cz)*cellSize
			// Smooth rolling hill: max slope ~0.35 (~19 deg, well within 45 deg walkable slope).
			heights[cz*cols+cx] = 1.2 * float32(math.Sin(float64(wx)*0.3)) * float32(math.Cos(float64(wz)*0.25))
		}
	}

	hf := physics3d.NewHeightfieldCollider(cols, rows, cellSize, origin, heights)

	// Out-of-bounds query returns false.
	if _, ok := hf.HeightAt(-10, 0); ok {
		t.Fatal("expected HeightAt outside bounds to return false")
	}

	// Exact vertex heights & barycentric interior raycast agreement.
	samples := [][2]float32{
		{0, 0},
		{1.23, -0.77},
		{-3.6, 2.15},
		{4.12, 3.88},
		{-2.25, -4.4},
	}
	for _, s := range samples {
		h, ok := hf.HeightAt(s[0], s[1])
		if !ok {
			t.Fatalf("HeightAt(%v,%v) out of bounds", s[0], s[1])
		}
		n := hf.NormalAt(s[0], s[1])
		if !approx(n.Len(), 1.0, 1e-4) || n.Y <= 0.5 {
			t.Fatalf("NormalAt(%v,%v) = %v, want unit upward normal", s[0], s[1], n)
		}
		// Vertical downward ray must hit exact HeightAt(x, z).
		ray := physics3d.Ray{
			Origin:  physics3d.Vec3{X: s[0], Y: 10, Z: s[1]},
			Dir:     physics3d.Vec3{Y: -1},
			MaxDist: 20,
		}
		hit, ok := hf.Raycast(ray)
		if !ok || !approx(hit.Point.Y, h, 1e-4) {
			t.Fatalf("vertical ray at (%v,%v): hit=%+v ok=%v, want Y=%v", s[0], s[1], hit, ok, h)
		}
		// Angled ray aimed directly at (x, h, z) hits the terrain accurately.
		target := physics3d.Vec3{X: s[0], Y: h, Z: s[1]}
		from := target.Add(physics3d.Vec3{X: -1.5, Y: 4.0, Z: -1.0})
		angled := physics3d.Ray{Origin: from, Dir: target.Sub(from), MaxDist: 10}
		aHit, aOk := hf.Raycast(angled)
		if !aOk {
			t.Fatalf("angled ray at (%v,%v) missed terrain", s[0], s[1])
		}
		hitH, ok := hf.HeightAt(aHit.Point.X, aHit.Point.Z)
		if !ok || !approx(aHit.Point.Y, hitH, 1e-3) {
			t.Fatalf("angled ray hit point %v not on terrain surface (h=%v)", aHit.Point, hitH)
		}
	}

	// CharacterController walks smoothly across undulating heightfield hills.
	w := physics3d.NewWorld()
	w.AddHeightfield(hf)

	startX, startZ := float32(-4.0), float32(0.0)
	startH, _ := hf.HeightAt(startX, startZ)
	cc := physics3d.NewCharacterController(physics3d.Vec3{X: startX, Y: startH + 0.05, Z: startZ}, 0.3, 1.8)

	dt := time.Second / 60
	// Settle onto terrain first.
	for range 15 {
		cc.Move(w, physics3d.Vec3{}, dt)
	}
	if !cc.Grounded {
		t.Fatalf("character failed to ground on heightfield: pos=%v", cc.Pos)
	}

	// Walk uphill, over crest, and downhill along +X for 3 seconds (covering ~9m).
	minY, maxY := cc.Pos.Y, cc.Pos.Y
	groundedFrames := 0
	const walkFrames = 180
	for range walkFrames {
		res := cc.Move(w, physics3d.Vec3{X: 3.0}, dt)
		if res.Grounded {
			groundedFrames++
		}
		if res.Pos.Y < minY {
			minY = res.Pos.Y
		}
		if res.Pos.Y > maxY {
			maxY = res.Pos.Y
		}
		terrainY, ok := hf.HeightAt(res.Pos.X, res.Pos.Z)
		if !ok {
			t.Fatalf("character left terrain bounds unexpectedly: pos=%v", res.Pos)
		}
		if absErr := float32(math.Abs(float64(res.Pos.Y - terrainY))); absErr > 0.12 {
			t.Fatalf("character feet diverged from heightfield: feetY=%v terrainY=%v at X=%v", res.Pos.Y, terrainY, res.Pos.X)
		}
	}
	if groundedFrames < walkFrames-5 {
		t.Fatalf("character lost ground contact too often on rolling hills: grounded %d/%d", groundedFrames, walkFrames)
	}
	if maxY-minY < 1.5 {
		t.Fatalf("character did not traverse full hill elevation range: minY=%v maxY=%v", minY, maxY)
	}
	if cc.Pos.X < 3.5 {
		t.Fatalf("character did not advance smoothly across heightfield: endX=%v", cc.Pos.X)
	}
}

func TestJoints3DChainDoorAndAllTypes(t *testing.T) {
	dt := time.Second / 60

	t.Run("MultiLinkHangingChainStableAt60Hz", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.SolverIterations = 12

		const (
			links   = 6
			linkLen = float32(0.6)
		)
		anchor := w.AddBody(physics3d.NewStaticBody(
			physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}),
			physics3d.Vec3{Y: 6},
		))

		bodies := make([]*physics3d.Body, links)
		prev := anchor
		for i := range links {
			// Start chain outstretched horizontally along +X so it swings down under gravity!
			pos := physics3d.Vec3{X: (float32(i) + 0.5) * linkLen, Y: 6}
			b := physics3d.NewDynamicBody(
				physics3d.CapsuleShape(physics3d.Capsule{
					A:      physics3d.Vec3{X: -linkLen * 0.35},
					B:      physics3d.Vec3{X: linkLen * 0.35},
					Radius: 0.08,
				}),
				pos,
				1.0,
			)
			b.LinearDamping = 0.2
			b.AngularDamping = 0.25
			bodies[i] = w.AddBody(b)

			var localA physics3d.Vec3
			if i == 0 {
				localA = physics3d.Vec3{}
			} else {
				localA = physics3d.Vec3{X: linkLen * 0.5}
			}
			localB := physics3d.Vec3{X: -linkLen * 0.5}
			w.AddJoint(physics3d.NewBallJoint(prev, bodies[i], localA, localB))
			prev = bodies[i]
		}

		// Simulate 4 seconds at fixed 1/60s step.
		for range 240 {
			w.Step(dt)
		}

		// Verify finite poses, chain swung downward below pivot, and tight joint attachment.
		prevPos := anchor.Pos
		prevRot := anchor.Rot
		for i, b := range bodies {
			if !isFiniteVec(b.Pos) || !isFiniteQuat(b.Rot) {
				t.Fatalf("chain link %d became non-finite: pos=%v rot=%v", i, b.Pos, b.Rot)
			}
			var localA physics3d.Vec3
			if i > 0 {
				localA = physics3d.Vec3{X: linkLen * 0.5}
			}
			worldA := prevPos.Add(prevRot.RotateVec(localA))
			worldB := b.Pos.Add(b.Rot.RotateVec(physics3d.Vec3{X: -linkLen * 0.5}))
			if err := worldA.Dist(worldB); err > 0.03 {
				t.Fatalf("chain link %d joint separation = %v (want <= 0.03)", i, err)
			}
			prevPos = b.Pos
			prevRot = b.Rot
		}
		// End of chain must have swung well below the anchor (Y = 6).
		if bodies[links-1].Pos.Y > 4.0 {
			t.Fatalf("hanging chain did not swing down under gravity: tailPos=%v", bodies[links-1].Pos)
		}
	})

	t.Run("DoorHingeWithLimitsAndMotor", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{} // focus purely on door hinge & motor

		frame := w.AddBody(physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.1, Y: 1.1, Z: 0.1}}),
			physics3d.Vec3{},
		))

		const doorHalfW = float32(0.5)
		doorBody := physics3d.NewDynamicBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: doorHalfW, Y: 1.0, Z: 0.05}}),
			physics3d.Vec3{X: doorHalfW, Y: 0, Z: 0},
			5.0,
		)
		door := w.AddBody(doorBody)

		hinge := physics3d.NewHingeJoint(
			frame,
			door,
			physics3d.Vec3{},
			physics3d.Vec3{X: -doorHalfW},
			physics3d.Vec3{Y: 1},
		)
		hinge.EnableLimits = true
		hinge.MinAngle = 0
		hinge.MaxAngle = float32(math.Pi / 2)
		hinge.MotorTargetVel = 2.5
		hinge.MotorMaxTorque = 25.0
		w.AddJoint(hinge)

		// Open door with motor for 1.5s; must reach and hold at pi/2 without overshooting.
		for range 90 {
			w.Step(dt)
			ang := hinge.Angle()
			if ang < -0.05 || ang > float32(math.Pi/2)+0.05 {
				t.Fatalf("door angle exceeded limits during open: %v rad", ang)
			}
		}
		openAng := hinge.Angle()
		if !approx(openAng, float32(math.Pi/2), 0.03) {
			t.Fatalf("motorized door did not reach MaxAngle pi/2: got %v rad", openAng)
		}
		// Hinge pivot on door must remain at origin.
		pivot := door.Pos.Add(door.Rot.RotateVec(physics3d.Vec3{X: -doorHalfW}))
		if pivot.Len() > 0.02 {
			t.Fatalf("door hinge pivot drifted from frame: %v", pivot)
		}

		// Reverse motor to close door smoothly back to MinAngle = 0.
		hinge.MotorTargetVel = -2.5
		for range 90 {
			w.Step(dt)
			ang := hinge.Angle()
			if ang < -0.05 || ang > float32(math.Pi/2)+0.05 {
				t.Fatalf("door angle exceeded limits during close: %v rad", ang)
			}
		}
		if closeAng := hinge.Angle(); !approx(closeAng, 0, 0.03) {
			t.Fatalf("motorized door did not close back to 0: got %v rad", closeAng)
		}
	})

	t.Run("DistanceJointLimitsAndSpring", func(t *testing.T) {
		w := physics3d.NewWorld()
		anchor := w.AddBody(physics3d.NewStaticBody(
			physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}),
			physics3d.Vec3{Y: 5},
		))
		bob := w.AddBody(physics3d.NewDynamicBody(
			physics3d.SphereShape(physics3d.Sphere{Radius: 0.25}),
			physics3d.Vec3{Y: 4.5},
			1.0,
		))
		// Rope with MinDistance=0, MaxDistance=2.0.
		rope := physics3d.NewDistanceJoint(anchor, bob, physics3d.Vec3{}, physics3d.Vec3{}, 0, 2.0)
		w.AddJoint(rope)

		for range 120 {
			w.Step(dt)
		}
		dist := anchor.Pos.Dist(bob.Pos)
		if dist > 2.03 || dist < 1.9 {
			t.Fatalf("rope bob distance = %v, want ~2.0", dist)
		}

		// Soft spring-damper DistanceJoint settles near equilibrium without exploding.
		w2 := physics3d.NewWorld()
		a2 := w2.AddBody(physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}), physics3d.Vec3{Y: 5}))
		b2 := w2.AddBody(physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.25}), physics3d.Vec3{Y: 4}, 1.0))
		spring := physics3d.NewDistanceJoint(a2, b2, physics3d.Vec3{}, physics3d.Vec3{}, 1.5, 1.5)
		spring.Stiffness = 50
		spring.Damping = 6
		w2.AddJoint(spring)
		for range 240 {
			w2.Step(dt)
		}
		// Static equilibrium extension: m*g/k = 9.81/50 ≈ 0.196 => dist ≈ 1.696.
		if d := a2.Pos.Dist(b2.Pos); !approx(d, 1.696, 0.08) {
			t.Fatalf("spring equilibrium distance = %v, want ~1.696", d)
		}
	})

	t.Run("BallJointConeSwingLimit", func(t *testing.T) {
		w := physics3d.NewWorld()
		anchor := w.AddBody(physics3d.NewStaticBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.1}), physics3d.Vec3{Y: 5}))
		rod := w.AddBody(physics3d.NewDynamicBody(
			physics3d.CapsuleShape(physics3d.VerticalCapsule(physics3d.Vec3{}, 0.1, 1.0)),
			physics3d.Vec3{Y: 4.5},
			1.0,
		))
		bj := physics3d.NewBallJoint(anchor, rod, physics3d.Vec3{}, physics3d.Vec3{Y: 0.5})
		bj.ConeLimit = 0.4 // ~23 deg max swing from vertical
		w.AddJoint(bj)

		// Kick rod sideways hard.
		rod.ApplyImpulse(physics3d.Vec3{X: 8})
		for range 120 {
			w.Step(dt)
			worldAxisB := rod.Rot.RotateVec(physics3d.Vec3{Y: 1})
			dot := float64(worldAxisB.Y)
			if dot > 1 {
				dot = 1
			}
			if dot < -1 {
				dot = -1
			}
			swing := float32(math.Acos(dot))
			if swing > bj.ConeLimit+0.08 {
				t.Fatalf("BallJoint swing angle %v exceeded ConeLimit %v", swing, bj.ConeLimit)
			}
		}
	})

	t.Run("SliderJointLimitsAndMotor", func(t *testing.T) {
		w := physics3d.NewWorld()
		w.Gravity = physics3d.Vec3{Y: -9.81} // Gravity perpendicular to X slide axis must be resisted!
		rail := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.1, Y: 0.1, Z: 0.1}}), physics3d.Vec3{}))
		carriage := w.AddBody(physics3d.NewDynamicBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.3, Y: 0.3, Z: 0.3}}),
			physics3d.Vec3{},
			2.0,
		))
		slider := physics3d.NewSliderJoint(rail, carriage, physics3d.Vec3{}, physics3d.Vec3{}, physics3d.Vec3{X: 1})
		slider.EnableLimits = true
		slider.MinTranslation = -1.0
		slider.MaxTranslation = 2.0
		slider.MotorTargetVel = 4.0
		slider.MotorMaxForce = 30.0
		w.AddJoint(slider)

		for range 90 {
			w.Step(dt)
			if absY := float32(math.Abs(float64(carriage.Pos.Y))); absY > 0.03 {
				t.Fatalf("slider sagged off axis under gravity: pos=%v", carriage.Pos)
			}
		}
		if tr := slider.Translation(); !approx(tr, 2.0, 0.03) {
			t.Fatalf("slider motor did not reach MaxTranslation 2.0: got %v", tr)
		}

		// Reverse motor to MinTranslation = -1.0.
		slider.MotorTargetVel = -4.0
		for range 90 {
			w.Step(dt)
		}
		if tr := slider.Translation(); !approx(tr, -1.0, 0.03) {
			t.Fatalf("slider motor did not reach MinTranslation -1.0: got %v", tr)
		}
	})

	t.Run("FixedJointLocksRelativePose", func(t *testing.T) {
		w := physics3d.NewWorld()
		bA := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.4, Z: 0.4}}), physics3d.Vec3{Y: 5}, 1))
		bB := w.AddBody(physics3d.NewDynamicBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.4, Z: 0.4}}), physics3d.Vec3{X: 1, Y: 5}, 1))
		w.AddJoint(physics3d.NewFixedJoint(bA, bB, physics3d.Vec3{X: 0.5}, physics3d.Vec3{X: -0.5}))
		bA.ApplyAngularImpulse(physics3d.Vec3{Z: 1.5})

		for range 60 {
			w.Step(dt)
		}
		worldAnchorA := bA.Pos.Add(bA.Rot.RotateVec(physics3d.Vec3{X: 0.5}))
		worldAnchorB := bB.Pos.Add(bB.Rot.RotateVec(physics3d.Vec3{X: -0.5}))
		if err := worldAnchorA.Dist(worldAnchorB); err > 0.02 {
			t.Fatalf("FixedJoint anchor drifted by %v", err)
		}
		if ang := bA.Rot.AngleTo(bB.Rot); ang > 0.02 {
			t.Fatalf("FixedJoint orientation drifted by %v rad", ang)
		}
	})
}

func TestRagdollSkeletonKinematicDynamicAndBlended(t *testing.T) {
	skeleton := []physics3d.SkeletonJoint{
		{Name: "pelvis", Parent: -1, LocalPos: physics3d.Vec3{}, Radius: 0.14, Length: 0.25, Mass: 8},
		{Name: "torso", Parent: 0, LocalPos: physics3d.Vec3{Y: 0.25}, Radius: 0.14, Length: 0.4, Mass: 10, SwingLimit: 0.6},
		{Name: "head", Parent: 1, LocalPos: physics3d.Vec3{Y: 0.42}, Radius: 0.11, Length: 0.22, Mass: 4, SwingLimit: 0.5},
		{Name: "upperArmL", Parent: 1, LocalPos: physics3d.Vec3{X: -0.22, Y: 0.32}, Radius: 0.06, Length: 0.3, Mass: 2.5, SwingLimit: 1.2},
		{Name: "elbowL", Parent: 3, LocalPos: physics3d.Vec3{X: -0.3}, Radius: 0.05, Length: 0.28, Mass: 2.0, SwingLimit: 1.1},
		{Name: "thighL", Parent: 0, LocalPos: physics3d.Vec3{X: -0.1, Y: -0.05}, Radius: 0.08, Length: 0.42, Mass: 5.0, SwingLimit: 1.0},
		{Name: "kneeL", Parent: 5, LocalPos: physics3d.Vec3{Y: -0.42}, Radius: 0.07, Length: 0.4, Mass: 4.0, SwingLimit: 1.2},
	}

	w := physics3d.NewWorld()
	w.SolverIterations = 12
	// Static floor at Y = 0.
	w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 20, Y: 1, Z: 20}}),
		physics3d.Vec3{Y: -1},
	))

	rootPos := physics3d.Vec3{Y: 2.0}
	rootRot := physics3d.QuatIdentity()
	rd := physics3d.BuildRagdoll(w, skeleton, rootPos, rootRot)

	if len(rd.Bodies) != len(skeleton) {
		t.Fatalf("ragdoll bodies = %d, want %d", len(rd.Bodies), len(skeleton))
	}
	if len(rd.Joints) != len(skeleton)-1 {
		t.Fatalf("ragdoll joints = %d, want %d", len(rd.Joints), len(skeleton)-1)
	}
	// Verify both SwingTwistJoint and HingeJoint were generated (elbowL & kneeL => HingeJoint).
	var swingTwistCount, hingeCount int
	for _, j := range rd.Joints {
		switch j.(type) {
		case *physics3d.SwingTwistJoint:
			swingTwistCount++
		case *physics3d.HingeJoint:
			hingeCount++
		}
	}
	if swingTwistCount != 4 || hingeCount != 2 {
		t.Fatalf("ragdoll joint types: swingTwist=%d hinge=%d, want 4 swing-twist & 2 hinge", swingTwistCount, hingeCount)
	}

	// Capture initial animated rest pose.
	initPos, initRot := rd.WritePose()
	if !approxVec(initPos[0], rootPos, 1e-5) {
		t.Fatalf("initial pelvis pos = %v, want %v", initPos[0], rootPos)
	}
	if !approxVec(initPos[2], physics3d.Vec3{Y: 2.67}, 1e-4) {
		t.Fatalf("initial head pos = %v, want (0, 2.67, 0)", initPos[2])
	}

	dt := time.Second / 60

	// 1. Kinematic mode follows animated pose without falling under gravity.
	rd.SetMode(physics3d.RagdollKinematic)
	animPos := make([]physics3d.Vec3, len(initPos))
	animRot := make([]physics3d.Quat, len(initRot))
	yaw30 := physics3d.QuatFromAxisAngle(physics3d.Vec3{Y: 1}, float32(math.Pi/6))
	shift := physics3d.Vec3{X: 1.5, Y: 0.5, Z: -0.5}
	for i := range initPos {
		animPos[i] = yaw30.RotateVec(initPos[i].Sub(rootPos)).Add(rootPos).Add(shift)
		animRot[i] = yaw30.Mul(initRot[i]).Norm()
	}
	for range 30 {
		rd.SyncFromPose(animPos, animRot)
		w.Step(dt)
	}
	kinPos, kinRot := rd.WritePose()
	for i := range animPos {
		if !approxVec(kinPos[i], animPos[i], 1e-4) {
			t.Fatalf("kinematic bone %d pos = %v, want %v", i, kinPos[i], animPos[i])
		}
		if kinRot[i].AngleTo(animRot[i]) > 1e-4 {
			t.Fatalf("kinematic bone %d rot angle err = %v", i, kinRot[i].AngleTo(animRot[i]))
		}
	}

	// 2. Switch to RagdollDynamic on demand: ragdoll collapses onto floor under gravity.
	rd.SetMode(physics3d.RagdollDynamic)
	// Slight horizontal impulse to trigger natural collapse & rotation.
	rd.Bodies[2].ApplyImpulse(physics3d.Vec3{X: 3.0})
	for range 120 {
		w.Step(dt)
	}
	dynPos, dynRot := rd.WritePose()
	if dynPos[0].Y >= animPos[0].Y-0.8 {
		t.Fatalf("dynamic ragdoll did not fall under gravity: startY=%v endY=%v", animPos[0].Y, dynPos[0].Y)
	}
	for i, b := range rd.Bodies {
		if !isFiniteVec(b.Pos) || !isFiniteQuat(b.Rot) {
			t.Fatalf("dynamic ragdoll bone %d became non-finite", i)
		}
		if b.Pos.Y < -0.15 {
			t.Fatalf("dynamic ragdoll bone %d fell through floor: pos=%v", i, b.Pos)
		}
	}

	// 3. Animation-to-ragdoll blending (RagdollBlended):
	// BlendWeight=0.5 produces poses smoothly halfway between animPos and dynPos.
	rd.SetMode(physics3d.RagdollBlended)
	rd.BlendWeight = 0.5
	rd.SyncFromPose(animPos, animRot)
	blendPos, blendRot := rd.WritePose()
	for i := range animPos {
		wantPos := animPos[i].Add(dynPos[i]).Scale(0.5)
		if !approxVec(blendPos[i], wantPos, 0.02) {
			t.Fatalf("blended(0.5) bone %d pos = %v, want ~%v", i, blendPos[i], wantPos)
		}
		wantRot := animRot[i].Slerp(dynRot[i], 0.5)
		if blendRot[i].AngleTo(wantRot) > 0.02 {
			t.Fatalf("blended(0.5) bone %d rot err = %v rad", i, blendRot[i].AngleTo(wantRot))
		}
	}

	// BlendWeight=0 matches exact animated pose.
	rd.BlendWeight = 0
	rd.SyncFromPose(animPos, animRot)
	zeroPos, _ := rd.WritePose()
	for i := range animPos {
		if !approxVec(zeroPos[i], animPos[i], 1e-4) {
			t.Fatalf("blended(0.0) bone %d pos = %v, want %v", i, zeroPos[i], animPos[i])
		}
	}

	// Destroy cleans up all ragdoll bodies & joints (leaving only the static floor).
	rd.Destroy()
	if len(w.Bodies()) != 1 || len(w.Joints()) != 0 {
		t.Fatalf("after Destroy: bodies=%d joints=%d, want 1 floor and 0 joints", len(w.Bodies()), len(w.Joints()))
	}
}
