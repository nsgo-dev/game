package vehicle_test

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/vehicle"
)

func newSyntheticRace(t *testing.T, laps int) (*vehicle.Race, []*physics3d.Body, *physics3d.World) {
	t.Helper()
	w := physics3d.NewWorld()
	var cps []*physics3d.Body
	for i := range 3 {
		cps = append(cps, vehicle.AddCheckpoint(w, box(1, 1, 1), physics3d.Vec3{X: float32(10 * i)}))
	}
	r, err := vehicle.NewRace(laps, cps...)
	if err != nil {
		t.Fatal(err)
	}
	return r, cps, w
}

func enter(r *vehicle.Race, body, cp *physics3d.Body) {
	a, b := body, cp
	if b.ID < a.ID {
		a, b = b, a
	}
	r.Observe(physics3d.ContactEvent{Kind: physics3d.ContactEnter, BodyA: a, BodyB: b, IsTrigger: true})
}

func TestNewRaceValidates(t *testing.T) {
	w := physics3d.NewWorld()
	trig := vehicle.AddCheckpoint(w, box(1, 1, 1), physics3d.Vec3{})
	other := vehicle.AddCheckpoint(w, box(1, 1, 1), physics3d.Vec3{X: 5})
	solid := w.AddBody(physics3d.NewStaticBody(box(1, 1, 1), physics3d.Vec3{}))
	if !trig.IsTrigger || !trig.Static {
		t.Fatalf("AddCheckpoint made %+v", trig)
	}
	for name, f := range map[string]func() error{
		"zero laps":   func() error { _, err := vehicle.NewRace(0, trig, other); return err },
		"one cp":      func() error { _, err := vehicle.NewRace(1, trig); return err },
		"nil cp":      func() error { _, err := vehicle.NewRace(1, trig, nil); return err },
		"non-trigger": func() error { _, err := vehicle.NewRace(1, trig, solid); return err },
		"duplicate":   func() error { _, err := vehicle.NewRace(1, trig, other, trig); return err },
	} {
		if f() == nil {
			t.Errorf("%s: NewRace accepted it", name)
		}
	}
}

func TestRaceCountsLapsInOrder(t *testing.T) {
	r, cps, w := newSyntheticRace(t, 2)
	car := w.AddBody(physics3d.NewDynamicBody(box(1, 1, 1), physics3d.Vec3{}, 1))
	var events []vehicle.LapEvent
	r.OnLap = func(ev vehicle.LapEvent) { events = append(events, ev) }
	rc := r.AddRacer(car, "a")

	tick := func(n int) {
		for range n {
			r.Tick()
		}
	}
	// Re-entering the start line before passing the others is not a lap, and skipping checkpoint 1 does not count checkpoint 2.
	tick(5)
	enter(r, car, cps[0])
	enter(r, car, cps[2])
	if rc.Laps != 0 || rc.Next != 1 {
		t.Fatalf("out-of-order checkpoints counted: %+v", rc)
	}
	tick(5)
	enter(r, car, cps[1])
	tick(5)
	enter(r, car, cps[2])
	if rc.Next != 0 {
		t.Fatalf("Next = %d after the last checkpoint, want 0", rc.Next)
	}
	tick(5)
	enter(r, car, cps[0])
	if rc.Laps != 1 || rc.LastLap != 20 || rc.BestLap != 20 || rc.LapStart != 20 || rc.Next != 1 || rc.Finished {
		t.Fatalf("after lap 1: %+v", rc)
	}
	tick(3)
	enter(r, car, cps[1])
	enter(r, car, cps[2])
	tick(3)
	enter(r, car, cps[0])
	if !rc.Finished || rc.FinishStep != 26 || rc.Laps != 2 || rc.LastLap != 6 || rc.BestLap != 6 {
		t.Fatalf("after lap 2: %+v", rc)
	}
	want := []vehicle.LapEvent{
		{Racer: rc, Lap: 1, Steps: 20, Best: true},
		{Racer: rc, Lap: 2, Steps: 6, Best: true, Finished: true},
	}
	if len(events) != len(want) {
		t.Fatalf("events = %+v", events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("event %d = %+v, want %+v", i, events[i], want[i])
		}
	}
	// A finished racer stops counting.
	enter(r, car, cps[1])
	if rc.Next != 1 {
		t.Fatal("finished racer kept advancing")
	}
}

func TestRaceIgnoresOtherEvents(t *testing.T) {
	r, cps, w := newSyntheticRace(t, 1)
	car := w.AddBody(physics3d.NewDynamicBody(box(1, 1, 1), physics3d.Vec3{}, 1))
	other := w.AddBody(physics3d.NewDynamicBody(box(1, 1, 1), physics3d.Vec3{}, 1))
	rc := r.AddRacer(car, "a")
	r.Observe(physics3d.ContactEvent{Kind: physics3d.ContactStay, BodyA: cps[1], BodyB: car, IsTrigger: true})
	r.Observe(physics3d.ContactEvent{Kind: physics3d.ContactExit, BodyA: cps[1], BodyB: car, IsTrigger: true})
	r.Observe(physics3d.ContactEvent{Kind: physics3d.ContactEnter, BodyA: cps[1], BodyB: car})
	enter(r, other, cps[1])
	enter(r, car, other)
	if rc.Next != 1 {
		t.Fatalf("ignored events advanced the racer: %+v", rc)
	}
	enter(r, car, cps[1])
	if rc.Next != 2 {
		t.Fatal("ContactEnter on the next checkpoint was not counted")
	}
}

func TestRaceStandings(t *testing.T) {
	r, cps, w := newSyntheticRace(t, 1)
	body := func() *physics3d.Body {
		return w.AddBody(physics3d.NewDynamicBody(box(1, 1, 1), physics3d.Vec3{}, 1))
	}
	slow, fast, mid, idle, tie := r.AddRacer(body(), "slow"), r.AddRacer(body(), "fast"), r.AddRacer(body(), "mid"), r.AddRacer(body(), "idle"), r.AddRacer(body(), "tie")
	lap := func(rc *vehicle.Racer) {
		enter(r, rc.Body, cps[1])
		enter(r, rc.Body, cps[2])
		enter(r, rc.Body, cps[0])
	}
	r.Tick()
	lap(fast)
	r.Tick()
	lap(slow)
	enter(r, mid.Body, cps[1])
	enter(r, mid.Body, cps[2])
	got := r.Standings()
	want := []*vehicle.Racer{fast, slow, mid, idle, tie}
	for i := range want {
		if got[i] != want[i] {
			names := ""
			for _, rc := range got {
				names += rc.Name + " "
			}
			t.Fatalf("standings = %s", names)
		}
	}
	if len(r.Racers()) != 5 || r.Racers()[0] != slow {
		t.Fatal("Racers is not in add order")
	}
}

// TestRaceThroughWorldTriggers checks that World.OnTrigger delivers checkpoint events to Race.Observe for a car driving through gates.
func TestRaceThroughWorldTriggers(t *testing.T) {
	tt := newTestTrack(1)
	d := newDriver(tt.car, tt.waypoints)
	seen := map[int]bool{}
	for i := 0; i < 20*rate && tt.racer.Laps == 0; i++ {
		tt.step(d)
		seen[tt.racer.Next] = true
	}
	for _, n := range []int{0, 1, 2, 3} {
		if !seen[n] {
			t.Fatalf("racer never headed for checkpoint %d", n)
		}
	}
	if tt.racer.Laps != 1 {
		t.Fatalf("no lap in 20 s: %+v", tt.racer)
	}
}
