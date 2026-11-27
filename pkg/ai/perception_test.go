package ai_test

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/lights2d"
	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	teamGuards  ai.Team = 1
	teamIntrude ai.Team = 2
	step                = time.Second / 60
)

func guard() *ai.Perception {
	return &ai.Perception{
		Self:   1,
		Team:   teamGuards,
		Facing: pt(1, 0),
		Sight: ai.SightConfig{
			Range:           100,
			HalfAngle:       float32(math.Pi / 4),
			PeripheralRange: 20,
		},
		Hearing:     ai.HearingConfig{Range: 50},
		ForgetAfter: 2 * time.Second,
	}
}

func seen(p *ai.Perception, ids ...ai.StimulusID) []ai.StimulusID {
	var out []ai.StimulusID
	for _, id := range ids {
		if p.Sees(id) {
			out = append(out, id)
		}
	}
	return out
}

func TestPerceptionSightConeRangeAndOccluder(t *testing.T) {
	// A wall from x=30 to 40 and y=5 to 40 hides the area behind it from a guard at the origin.
	walls := []lights2d.Occluder{lights2d.RectOccluder(core.Bounds{Origin: pt(30, 5), Size: core.Size{Width: 10, Height: 35}})}
	targets := []ai.Target{
		{ID: 10, Team: teamIntrude, Pos: pt(50, 0)},    // ahead, in the open
		{ID: 11, Team: teamIntrude, Pos: pt(50, 60)},   // 50 degrees off the facing, outside the cone
		{ID: 12, Team: teamIntrude, Pos: pt(-10, 0)},   // behind, but within the peripheral range
		{ID: 13, Team: teamIntrude, Pos: pt(-50, 0)},   // behind and beyond the peripheral range
		{ID: 14, Team: teamIntrude, Pos: pt(150, 0)},   // ahead, beyond the range
		{ID: 15, Team: teamIntrude, Pos: pt(60, 15)},   // ahead, behind the wall
		{ID: 16, Team: teamIntrude, Pos: pt(100, 0)},   // exactly at the range
		{ID: 17, Team: teamGuards, Pos: pt(20, 0)},     // a friendly guard, ignored by default
		{ID: 1, Team: teamIntrude, Pos: pt(5, 0)},      // the guard's own ID, never sensed
		{ID: 18, Team: ai.NoTeam, Pos: pt(25, -1)},     // neutral, ignored by default
		{ID: 10, Team: teamIntrude, Pos: pt(-90, -90)}, // duplicate ID, ignored
	}
	run := func() ([]ai.StimulusID, []ai.Event) {
		g := guard()
		var sys ai.PerceptionSystem
		sys.LineOfSight = lights2d.LineOfSight(&walls)
		sys.Add(g)
		sys.Step(step, targets)
		return seen(g, 1, 10, 11, 12, 13, 14, 15, 16, 17, 18), append([]ai.Event(nil), g.Events()...)
	}
	got, events := run()
	if want := []ai.StimulusID{10, 12, 16}; !reflect.DeepEqual(got, want) {
		t.Fatalf("seen = %v, want %v", got, want)
	}
	if len(events) != 3 || events[0].Kind != ai.EventGained || events[0].Memory.ID != 10 || events[0].Memory.Pos != pt(50, 0) || events[0].Memory.Sense != ai.SenseSight {
		t.Fatalf("events = %+v", events)
	}
	for range 5 {
		again, againEvents := run()
		if !reflect.DeepEqual(again, got) || !reflect.DeepEqual(againEvents, events) {
			t.Fatalf("run differs: %v %+v, want %v %+v", again, againEvents, got, events)
		}
	}
}

func TestPerceptionAttitudeFilters(t *testing.T) {
	targets := []ai.Target{
		{ID: 10, Team: teamIntrude, Pos: pt(10, 0)},
		{ID: 11, Team: teamGuards, Pos: pt(20, 0)},
		{ID: 12, Team: ai.NoTeam, Pos: pt(30, 0)},
	}
	cases := []struct {
		detect   ai.AttitudeMask
		attitude ai.AttitudeFunc
		want     []ai.StimulusID
	}{
		{0, nil, []ai.StimulusID{10}},
		{ai.DetectFriendly, nil, []ai.StimulusID{11}},
		{ai.DetectNeutral | ai.DetectHostile, nil, []ai.StimulusID{10, 12}},
		{ai.DetectAll, nil, []ai.StimulusID{10, 11, 12}},
		{ai.DetectHostile, func(observer, other ai.Team) ai.Attitude { return ai.Hostile }, []ai.StimulusID{10, 11, 12}},
	}
	for i, tc := range cases {
		g := guard()
		g.Sight.Detect = tc.detect
		sys := ai.PerceptionSystem{Attitude: tc.attitude}
		sys.Add(g)
		sys.Step(step, targets)
		if got := seen(g, 10, 11, 12); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("case %d: seen %v, want %v", i, got, tc.want)
		}
	}
	if ai.DefaultAttitude(1, 1) != ai.Friendly || ai.DefaultAttitude(1, 2) != ai.Hostile || ai.DefaultAttitude(ai.NoTeam, ai.NoTeam) != ai.Neutral || ai.DefaultAttitude(1, ai.NoTeam) != ai.Neutral {
		t.Fatal("DefaultAttitude")
	}
	if ai.Hostile.String() != "hostile" || ai.SenseHearing.String() != "hearing" || ai.EventForgotten.String() != "forgotten" || ai.Attitude(9).String() != "attitude(9)" {
		t.Fatal("String")
	}
}

func TestPerceptionFacingAndPeripheralAngle(t *testing.T) {
	g := guard()
	g.Facing = pt(0, -3) // up the screen; the length does not matter
	g.Sight.PeripheralRange = 0
	var sys ai.PerceptionSystem
	sys.Add(g)
	targets := []ai.Target{{ID: 10, Team: teamIntrude, Pos: pt(0, -80)}, {ID: 11, Team: teamIntrude, Pos: pt(80, 0)}}
	sys.Step(step, targets)
	if got := seen(g, 10, 11); !reflect.DeepEqual(got, []ai.StimulusID{10}) {
		t.Fatalf("facing up: seen %v", got)
	}

	// A peripheral half-angle of 90 degrees covers the sides but not behind.
	g.Sight = ai.SightConfig{PeripheralRange: 30, PeripheralHalfAngle: float32(math.Pi / 2)}
	g.Facing = core.Point{}
	targets = []ai.Target{{ID: 20, Team: teamIntrude, Pos: pt(5, 25)}, {ID: 21, Team: teamIntrude, Pos: pt(-25, 0)}, {ID: 22, Team: teamIntrude, Pos: pt(25, 0)}}
	sys.Step(step, targets)
	if got := seen(g, 20, 21, 22); !reflect.DeepEqual(got, []ai.StimulusID{20, 22}) {
		t.Fatalf("peripheral: seen %v", got)
	}

	// A half-angle of π sees all around; the eye offset moves where range is measured from.
	g.Sight = ai.SightConfig{Range: 10, HalfAngle: math.Pi}
	g.EyeOffset = pt(0, -100)
	targets = []ai.Target{{ID: 30, Team: teamIntrude, Pos: pt(0, -95)}, {ID: 31, Team: teamIntrude, Pos: pt(0, 5)}}
	sys.Step(step, targets)
	if got := seen(g, 30, 31); !reflect.DeepEqual(got, []ai.StimulusID{30}) {
		t.Fatalf("eye offset: seen %v", got)
	}
}

func TestPerceptionLineOfSightFrom(t *testing.T) {
	g := guard()
	g.Pos = pt(10, 20)
	g.EyeOffset = pt(0, -5)
	var calls [][2]core.Point
	sys := ai.PerceptionSystem{LineOfSight: func(from, to core.Point) bool {
		calls = append(calls, [2]core.Point{from, to})
		return true
	}}
	sys.Add(g)
	sys.Step(step, []ai.Target{{ID: 10, Team: teamIntrude, Pos: pt(60, 15)}})
	if want := [][2]core.Point{{pt(10, 15), pt(60, 15)}}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestPerceptionEventsAndMemory(t *testing.T) {
	g := guard()
	var sys ai.PerceptionSystem
	sys.Add(g)
	kinds := func() []string {
		var out []string
		for _, ev := range g.Events() {
			out = append(out, fmt.Sprintf("%v %d", ev.Kind, ev.Memory.ID))
		}
		return out
	}
	target := ai.Target{ID: 10, Team: teamIntrude, Pos: pt(50, 0), Vel: pt(30, 0)}
	sys.Step(step, []ai.Target{target})
	if got := kinds(); !reflect.DeepEqual(got, []string{"gained 10"}) {
		t.Fatalf("step 1 events %v", got)
	}
	sys.Step(step, []ai.Target{target})
	if got := kinds(); len(got) != 0 {
		t.Fatalf("a still target made events %v", got)
	}
	target.Pos = pt(51, 0)
	sys.Step(step, []ai.Target{target})
	if got := kinds(); !reflect.DeepEqual(got, []string{"updated 10"}) {
		t.Fatalf("moved target events %v", got)
	}

	// The target leaves; the memory keeps its last known position and velocity and ages.
	sys.Step(step, nil)
	if got := kinds(); !reflect.DeepEqual(got, []string{"lost 10"}) {
		t.Fatalf("lost events %v", got)
	}
	m, ok := g.Memory(10)
	if !ok || m.Visible || m.Pos != pt(51, 0) || m.Vel != pt(30, 0) || m.Age != step || m.Sense != ai.SenseSight || m.Attitude != ai.Hostile || m.Team != teamIntrude {
		t.Fatalf("memory after losing sight = %+v", m)
	}
	if want := float32(1 - step.Seconds()/2); math.Abs(float64(m.Strength-want)) > 1e-6 {
		t.Fatalf("strength = %v, want %v", m.Strength, want)
	}

	// Seen again: updated, fresh again.
	sys.Step(step, []ai.Target{target})
	if got := kinds(); !reflect.DeepEqual(got, []string{"updated 10"}) {
		t.Fatalf("regained events %v", got)
	}
	if m, _ := g.Memory(10); m.Age != 0 || m.Strength != 1 || !m.Visible {
		t.Fatalf("regained memory = %+v", m)
	}

	if !g.Forget(10) || g.Forget(10) || len(g.Memories()) != 0 {
		t.Fatal("Forget")
	}
	sys.Step(step, []ai.Target{target})
	g.Clear()
	if len(g.Memories()) != 0 || len(g.Events()) != 0 {
		t.Fatal("Clear")
	}
}

func TestPerceptionHearingCreatesMemoryThatExpires(t *testing.T) {
	g := guard()
	g.Sight = ai.SightConfig{}
	var sys ai.PerceptionSystem
	sys.Add(g)

	sys.PostNoise(ai.Noise{Source: 20, Team: teamIntrude, Pos: pt(0, 40), Loudness: 1}) // within 50
	sys.PostNoise(ai.Noise{Source: 21, Team: teamIntrude, Pos: pt(0, 80), Loudness: 1}) // beyond 50
	sys.PostNoise(ai.Noise{Source: 22, Team: teamIntrude, Pos: pt(0, 80), Loudness: 2}) // loud enough to reach
	sys.PostNoise(ai.Noise{Source: 23, Team: teamGuards, Pos: pt(0, 10), Loudness: 1})  // friendly, ignored
	sys.PostNoise(ai.Noise{Source: 1, Team: teamIntrude, Pos: pt(0, 0), Loudness: 1})   // the guard's own noise
	sys.PostNoise(ai.Noise{Source: 24, Team: teamIntrude, Pos: pt(0, 1), Loudness: 0})  // silent
	sys.Step(step, nil)

	mems := g.Memories()
	if len(mems) != 2 || mems[0].ID != 20 || mems[1].ID != 22 {
		t.Fatalf("memories = %+v", mems)
	}
	m := mems[0]
	if m.Pos != pt(0, 40) || m.Sense != ai.SenseHearing || m.Visible || m.Vel != (core.Point{}) || m.Age != 0 || math.Abs(float64(m.Strength)-0.2) > 1e-6 {
		t.Fatalf("heard memory = %+v", m)
	}
	if ev := g.Events(); len(ev) != 2 || ev[0].Kind != ai.EventGained || ev[0].Memory.Sense != ai.SenseHearing {
		t.Fatalf("events = %+v", ev)
	}

	// Noises are consumed by the step that hears them.
	sys.Step(step, nil)
	if len(g.Events()) != 0 {
		t.Fatalf("noise heard twice: %+v", g.Events())
	}

	// A memory lasts ForgetAfter (2s) after it was last sensed and is forgotten on the step that reaches it.
	elapsed := step
	for elapsed+step < g.ForgetAfter {
		sys.Step(step, nil)
		elapsed += step
		if len(g.Events()) != 0 {
			t.Fatalf("event before expiry at %v: %+v", elapsed, g.Events())
		}
	}
	if m, ok := g.Memory(20); !ok || m.Strength != 0 || m.Age != elapsed {
		t.Fatalf("before expiry: %+v %v", m, ok)
	}
	sys.Step(step, nil)
	ev := g.Events()
	if len(ev) != 2 || ev[0].Kind != ai.EventForgotten || ev[0].Memory.ID != 20 || ev[0].Memory.Age != elapsed+step || ev[1].Memory.ID != 22 {
		t.Fatalf("expiry events = %+v", ev)
	}
	if len(g.Memories()) != 0 {
		t.Fatalf("memories after expiry = %+v", g.Memories())
	}
}

func TestPerceptionHearingRefreshesAndSightWins(t *testing.T) {
	g := guard()
	g.Hearing.Detect = ai.DetectAll
	var sys ai.PerceptionSystem
	sys.Add(g)

	// Seen and heard on the same step: sight keeps the exact position and velocity.
	sys.PostNoise(ai.Noise{Source: 10, Team: teamIntrude, Pos: pt(45, 3), Loudness: 1})
	sys.Step(step, []ai.Target{{ID: 10, Team: teamIntrude, Pos: pt(40, 0), Vel: pt(1, 0)}})
	if m, _ := g.Memory(10); m.Sense != ai.SenseSight || m.Pos != pt(40, 0) || len(g.Events()) != 1 {
		t.Fatalf("sight then noise: %+v %+v", m, g.Events())
	}

	// Out of sight, a noise moves the last known position and resets the age; a weaker noise keeps the stronger strength.
	sys.Step(step, nil)
	sys.PostNoise(ai.Noise{Source: 10, Team: teamIntrude, Pos: pt(-30, 0), Loudness: 1})
	sys.Step(step, nil)
	m, _ := g.Memory(10)
	if m.Sense != ai.SenseHearing || m.Pos != pt(-30, 0) || m.Vel != (core.Point{}) || m.Age != 0 || m.Visible {
		t.Fatalf("heard after lost: %+v", m)
	}
	if want := float32(1 - 2*step.Seconds()/2); math.Abs(float64(m.Strength-want)) > 1e-6 {
		t.Fatalf("strength = %v, want %v", m.Strength, want)
	}
	if ev := g.Events(); len(ev) != 1 || ev[0].Kind != ai.EventUpdated {
		t.Fatalf("events = %+v", ev)
	}

	// Anonymous noises share ID 0 and a neutral team.
	sys.PostNoise(ai.Noise{Team: ai.NoTeam, Pos: pt(0, 10), Loudness: 1})
	sys.Step(step, nil)
	if m, ok := g.Memory(0); !ok || m.Attitude != ai.Neutral {
		t.Fatalf("anonymous noise: %+v %v", m, ok)
	}
}

func TestPerceptionForgetAfterZero(t *testing.T) {
	g := guard()
	g.ForgetAfter = 0
	var sys ai.PerceptionSystem
	sys.Add(g)
	sys.Step(step, []ai.Target{{ID: 10, Team: teamIntrude, Pos: pt(10, 0)}})
	sys.Step(step, nil)
	ev := g.Events()
	if len(ev) != 2 || ev[0].Kind != ai.EventLost || ev[1].Kind != ai.EventForgotten || len(g.Memories()) != 0 {
		t.Fatalf("events = %+v", ev)
	}
}

// losRecorder identifies each agent by the X of its eye and each target by the Y of its position.
type losRecorder struct {
	calls [][2]int
	clear bool
}

func (r *losRecorder) check(from, to core.Point) bool {
	r.calls = append(r.calls, [2]int{int(from.X), int(to.Y)})
	return r.clear
}

func TestPerceptionBudgetRoundRobin(t *testing.T) {
	const agents, targets, budget = 7, 5, 4
	rec := &losRecorder{clear: true}
	sys := ai.PerceptionSystem{Budget: budget, LineOfSight: rec.check}
	var ps []*ai.Perception
	for i := range agents {
		p := &ai.Perception{Team: teamGuards, Pos: pt(float32(i), 0), Sight: ai.SightConfig{Range: 1000, HalfAngle: math.Pi}}
		ps = append(ps, p)
		sys.Add(p)
	}
	var ts []ai.Target
	for j := targets - 1; j >= 0; j-- { // posted out of ID order; served in ID order
		ts = append(ts, ai.Target{ID: ai.StimulusID(100 + j), Team: teamIntrude, Pos: pt(500, float32(j))})
	}

	// The fixed order: agents as added, then targets by ID.
	var order [][2]int
	for i := range agents {
		for j := range targets {
			order = append(order, [2]int{i, j})
		}
	}
	const steps = 40
	for s := range steps {
		rec.calls = rec.calls[:0]
		sys.Step(step, ts)
		st := sys.Stats()
		if len(rec.calls) > budget || st.Checks != len(rec.calls) || st.Candidates != agents*targets || st.Deferred != agents*targets-budget {
			t.Fatalf("step %d: %d calls, stats %+v", s, len(rec.calls), st)
		}
		for k, c := range rec.calls {
			if want := order[(s*budget+k)%len(order)]; c != want {
				t.Fatalf("step %d call %d = %v, want %v", s, k, c, want)
			}
		}
	}
	// After a full round every agent has seen every target.
	for i, p := range ps {
		if len(p.Memories()) != targets {
			t.Fatalf("agent %d remembers %d targets", i, len(p.Memories()))
		}
	}

	// Pairs not yet rechecked keep their last result, so a wall raised now hides targets only as the round reaches them.
	rec.clear = false
	sys.Step(step, ts)
	visible := 0
	for _, p := range ps {
		for _, m := range p.Memories() {
			if m.Visible {
				visible++
			}
		}
	}
	if visible != agents*targets-budget {
		t.Fatalf("visible after one budgeted step = %d", visible)
	}

	// A removed agent drops out of the round; one added later joins at the end.
	sys.Remove(ps[0])
	late := &ai.Perception{Pos: pt(50, 0), Sight: ai.SightConfig{Range: 1000, HalfAngle: math.Pi}}
	sys.Add(late)
	sys.Budget = 0
	rec.calls = rec.calls[:0]
	sys.Step(step, ts)
	if len(rec.calls) != agents*targets || sys.Stats().Deferred != 0 || rec.calls[len(rec.calls)-1] != [2]int{50, targets - 1} {
		t.Fatalf("unlimited budget calls = %v", rec.calls)
	}
	if sys.Len() != agents {
		t.Fatalf("Len = %d", sys.Len())
	}
}

func TestPerceptionSystemMembership(t *testing.T) {
	var a, b ai.PerceptionSystem
	p := guard()
	a.Add(p)
	a.Add(p)
	if a.Len() != 1 {
		t.Fatalf("Len after adding twice = %d", a.Len())
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("adding to a second system did not panic")
			}
		}()
		b.Add(p)
	}()
	if !a.Remove(p) || a.Remove(p) {
		t.Fatal("Remove")
	}
	b.Add(p)
	if b.Len() != 1 {
		t.Fatal("re-add after Remove")
	}
}

func TestPerceptionBudgetChangingCandidates(t *testing.T) {
	const budget = 5
	rec := &losRecorder{clear: true}
	sys := ai.PerceptionSystem{Budget: budget, LineOfSight: rec.check}
	agent := func(x float32) *ai.Perception {
		return &ai.Perception{Team: teamGuards, Pos: pt(x, 0), Sight: ai.SightConfig{Range: 1000, HalfAngle: math.Pi}}
	}
	a0, a1, a2 := agent(0), agent(1), agent(2)
	sys.Add(a0)
	sys.Add(a1)
	sys.Add(a2)
	targets := func(ids ...int) []ai.Target {
		var ts []ai.Target
		for _, j := range ids {
			ts = append(ts, ai.Target{ID: ai.StimulusID(100 + j), Team: teamIntrude, Pos: pt(500, float32(j))})
		}
		return ts
	}
	run := func(name string, ts []ai.Target, want ...[2]int) {
		t.Helper()
		rec.calls = rec.calls[:0]
		sys.Step(step, ts)
		if !reflect.DeepEqual(rec.calls, want) {
			t.Fatalf("%s: calls %v, want %v", name, rec.calls, want)
		}
		if st := sys.Stats(); st.Checks != len(want) || st.Deferred != st.Candidates-len(want) {
			t.Fatalf("%s: stats %+v", name, st)
		}
	}

	run("first round", targets(0, 1, 2, 3), [2]int{0, 0}, [2]int{0, 1}, [2]int{0, 2}, [2]int{0, 3}, [2]int{1, 0})

	// The cursor's agent leaves: the round resumes at the first pair after the cursor, and wraps.
	sys.Remove(a1)
	run("cursor agent removed", targets(0, 1, 2, 3), [2]int{2, 0}, [2]int{2, 1}, [2]int{2, 2}, [2]int{2, 3}, [2]int{0, 0})

	// The cursor's target despawns: the round resumes at the agent's next target.
	run("cursor target gone", targets(1, 2, 3), [2]int{0, 1}, [2]int{0, 2}, [2]int{0, 3}, [2]int{2, 1}, [2]int{2, 2})

	// A new agent and a re-added one join at the end, in the order they were added.
	late := agent(5)
	sys.Add(late)
	sys.Add(a1)
	run("agents joined", targets(1, 2, 3), [2]int{2, 3}, [2]int{5, 1}, [2]int{5, 2}, [2]int{5, 3}, [2]int{1, 1})

	// With the set stable again, every pair is served within ceil(n/budget) steps.
	served := map[[2]int]bool{}
	for _, c := range rec.calls {
		served[c] = true
	}
	const pairs = 4 * 3
	for range (pairs+budget-1)/budget - 1 {
		rec.calls = rec.calls[:0]
		sys.Step(step, targets(1, 2, 3))
		for _, c := range rec.calls {
			served[c] = true
		}
	}
	if len(served) != pairs {
		t.Fatalf("served %d of %d pairs: %v", len(served), pairs, served)
	}
}

func TestPerceptionCopyAndRemove(t *testing.T) {
	var sys ai.PerceptionSystem
	g1 := guard()
	sys.Add(g1)
	sys.Step(step, []ai.Target{{ID: 10, Team: teamIntrude, Pos: pt(50, 0)}})

	// A copy of an added Perception is a separate agent with its own memories.
	g2 := *g1
	sys.Add(&g2)
	if sys.Len() != 2 {
		t.Fatalf("Len after adding a copy = %d", sys.Len())
	}
	if !g2.Sees(10) || !g2.Forget(10) || !g1.Sees(10) {
		t.Fatal("the copy does not have its own memories")
	}
	if len(g2.Events()) != 0 {
		t.Fatalf("copy events = %+v", g2.Events())
	}
	sys.Step(step, []ai.Target{{ID: 11, Team: teamIntrude, Pos: pt(60, 0)}})
	ids := func(p *ai.Perception) []ai.StimulusID {
		var out []ai.StimulusID
		for _, m := range p.Memories() {
			out = append(out, m.ID)
		}
		return out
	}
	if got := ids(g1); !reflect.DeepEqual(got, []ai.StimulusID{10, 11}) {
		t.Fatalf("original remembers %v", got)
	}
	if got := ids(&g2); !reflect.DeepEqual(got, []ai.StimulusID{11}) {
		t.Fatalf("copy remembers %v", got)
	}
	if len(g1.Events()) != 2 || len(g2.Events()) != 1 || g2.Events()[0].Kind != ai.EventGained {
		t.Fatalf("events: original %+v, copy %+v", g1.Events(), g2.Events())
	}

	// A removed agent keeps its memories but sees nothing and reports no events.
	if !sys.Remove(g1) {
		t.Fatal("Remove")
	}
	m, ok := g1.Memory(11)
	if !ok || m.Visible || g1.Sees(11) || len(g1.Events()) != 0 {
		t.Fatalf("after Remove: memory %+v %v, sees %v, events %+v", m, ok, g1.Sees(11), g1.Events())
	}
	if !g2.Sees(11) {
		t.Fatal("removing the original changed the copy")
	}
	sys.Add(g1)
	sys.Step(step, []ai.Target{{ID: 11, Team: teamIntrude, Pos: pt(60, 0)}})
	if !g1.Sees(11) || len(g1.Events()) != 1 || g1.Events()[0].Kind != ai.EventUpdated {
		t.Fatalf("after re-adding: sees %v, events %+v", g1.Sees(11), g1.Events())
	}
}

func TestPerceptionPhysicsLineOfSight(t *testing.T) {
	const layerWalls physics.LayerMask = 1 << 1
	wall := physics.NewBoxBody(30, -20, 10, 40)
	wall.Layer = layerWalls
	intruder := physics.NewBoxBody(55, -5, 10, 10)
	intruder.Layer = 1 << 2
	w := &physics.World{Bodies: []*physics.Body{wall, intruder}}

	g := guard()
	sys := ai.PerceptionSystem{LineOfSight: ai.PhysicsLineOfSight(w, physics.QueryBodies, layerWalls)}
	sys.Add(g)
	targets := []ai.Target{{ID: 10, Team: teamIntrude, Pos: pt(60, 0)}, {ID: 11, Team: teamIntrude, Pos: pt(60, 50)}}
	sys.Step(step, targets)
	if got := seen(g, 10, 11); !reflect.DeepEqual(got, []ai.StimulusID{11}) {
		t.Fatalf("seen %v", got)
	}

	// With every layer blocking, the ray to 11 hits nothing and the ray to 10 hits the wall.
	sys.LineOfSight = ai.PhysicsLineOfSight(w, 0, 0)
	sys.Step(step, targets)
	if got := seen(g, 10, 11); !reflect.DeepEqual(got, []ai.StimulusID{11}) {
		t.Fatalf("all layers: seen %v", got)
	}
}

func TestPerceptionLoopDeterminism(t *testing.T) {
	walls := []lights2d.Occluder{lights2d.RectOccluder(core.Bounds{Origin: pt(80, -40), Size: core.Size{Width: 10, Height: 80}})}
	run := func() []string {
		var log []string
		sys := ai.PerceptionSystem{LineOfSight: lights2d.LineOfSight(&walls), Budget: 3}
		var guards []*ai.Perception
		for i := range 4 {
			g := guard()
			g.Self = ai.StimulusID(i + 1)
			g.Pos = pt(float32(i*30), float32(i*10-15))
			sys.Add(g)
			guards = append(guards, g)
		}
		intruder := ai.Target{ID: 100, Team: teamIntrude, Pos: pt(200, 0), Vel: pt(-60, 15)}
		frame := 0
		loop := &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				frame++
				intruder.Pos.X += intruder.Vel.X * core.Points(dt.Seconds())
				intruder.Pos.Y += intruder.Vel.Y * core.Points(dt.Seconds())
				if frame%20 == 0 && frame < 120 {
					sys.PostNoise(ai.Noise{Source: 100, Team: teamIntrude, Pos: intruder.Pos, Loudness: 1.5})
				}
				for _, g := range guards {
					g.Facing = pt(float32(math.Cos(float64(frame)/30)), float32(math.Sin(float64(frame)/30)))
				}
				sys.Step(dt, []ai.Target{intruder})
				for gi, g := range guards {
					for _, ev := range g.Events() {
						log = append(log, fmt.Sprintf("%d g%d %v %v %v %.3f", frame, gi, ev.Kind, ev.Memory.Sense, ev.Memory.Pos, ev.Memory.Strength))
					}
				}
			},
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			loop.Run(cx)
			return core.View{}
		})
		app.Advance(5 * time.Second)
		return log
	}
	a, b := run(), run()
	if len(a) == 0 {
		t.Fatal("no perception events across the run")
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("perception across game.Loop steps is not deterministic:\n%v\n%v", a, b)
	}
	kinds := map[string]bool{}
	for _, line := range a {
		for _, k := range []string{"gained", "updated", "lost", "forgotten", "hearing"} {
			if strings.Contains(line, " "+k+" ") {
				kinds[k] = true
			}
		}
	}
	if len(kinds) != 5 {
		t.Fatalf("run covered only %v", kinds)
	}
}

func BenchmarkPerceptionSystemStep(b *testing.B) {
	walls := []lights2d.Occluder{lights2d.RectOccluder(core.Bounds{Origin: pt(400, 0), Size: core.Size{Width: 20, Height: 600}})}
	sys := ai.PerceptionSystem{LineOfSight: lights2d.LineOfSight(&walls), Budget: 64}
	for i := range 300 {
		sys.Add(&ai.Perception{
			Team:        teamGuards,
			Pos:         pt(float32(i%20)*40, float32(i/20)*40),
			Facing:      pt(1, float32(i%3-1)),
			Sight:       ai.SightConfig{Range: 300, HalfAngle: 1, PeripheralRange: 40},
			Hearing:     ai.HearingConfig{Range: 200},
			ForgetAfter: 3 * time.Second,
		})
	}
	targets := make([]ai.Target, 100)
	for i := range targets {
		targets[i] = ai.Target{ID: ai.StimulusID(1000 + i), Team: teamIntrude, Pos: pt(float32(i*8), float32(i*6))}
	}
	b.ReportAllocs()
	for b.Loop() {
		sys.PostNoise(ai.Noise{Source: 1000, Team: teamIntrude, Pos: pt(300, 300), Loudness: 1})
		sys.Step(step, targets)
	}
}
