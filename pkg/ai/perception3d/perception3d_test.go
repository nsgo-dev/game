package perception3d_test

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/ai/perception3d"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const (
	teamGuards  ai.Team = 1
	teamIntrude ai.Team = 2
	step                = time.Second / 60
)

func wallWorld() *physics3d.World {
	w := physics3d.NewWorld()
	wall := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 2, Y: 2, Z: 0.5}}), physics3d.Vec3{Z: -10})
	wall.Layer = layerWalls
	w.AddBody(wall)
	return w
}

const layerWalls physics3d.LayerMask = 1 << 1

func guard() *perception3d.Perception {
	return &perception3d.Perception{
		Self:        1,
		Team:        teamGuards,
		Forward:     math3d.Vec3{}, // -Z
		EyeOffset:   math3d.Vec3{Y: 1.5},
		Sight:       ai.SightConfig{Range: 30, HalfAngle: float32(math.Pi / 6), PeripheralRange: 2},
		Hearing:     ai.HearingConfig{Range: 10},
		ForgetAfter: time.Second,
	}
}

func TestSightConeOccluderAndHearing(t *testing.T) {
	g := guard()
	sys := perception3d.System{LineOfSight: perception3d.PhysicsLineOfSight(wallWorld(), layerWalls), Budget: 8}
	sys.Add(g)
	targets := []perception3d.Target{
		{ID: 10, Team: teamIntrude, Pos: math3d.Vec3{Y: 1.5, Z: -20}},          // behind the wall
		{ID: 11, Team: teamIntrude, Pos: math3d.Vec3{X: 5, Y: 1.5, Z: -20}},    // in the cone, clear of the wall
		{ID: 12, Team: teamIntrude, Pos: math3d.Vec3{Y: 1.5, Z: 10}},           // behind the agent
		{ID: 13, Team: teamIntrude, Pos: math3d.Vec3{X: 1, Y: 1.5, Z: 1}},      // peripheral
		{ID: 14, Team: teamIntrude, Pos: math3d.Vec3{Y: 25, Z: -5}},            // high above, outside the cone
		{ID: 15, Team: teamIntrude, Pos: math3d.Vec3{X: 20, Y: 1.5, Z: -0.01}}, // to the side
		{ID: 16, Team: teamGuards, Pos: math3d.Vec3{Y: 1.5, Z: -5}},            // a friend
		{ID: 1, Team: teamIntrude, Pos: math3d.Vec3{Y: 1.5, Z: -5}},            // itself
	}
	sys.Step(step, targets)
	var got []ai.StimulusID
	for _, tg := range targets {
		if g.Sees(tg.ID) {
			got = append(got, tg.ID)
		}
	}
	if !reflect.DeepEqual(got, []ai.StimulusID{11, 13}) {
		t.Fatalf("seen %v", got)
	}
	if ev := g.Events(); len(ev) != 2 || ev[0].Kind != ai.EventGained || ev[0].Memory.Pos != targets[1].Pos || ev[0].Memory.Sense != ai.SenseSight {
		t.Fatalf("events %+v", ev)
	}
	if st := sys.Stats(); st.Candidates != 3 || st.Checks != 3 || st.Deferred != 0 {
		t.Fatalf("stats %+v", st)
	}

	sys.PostNoise(perception3d.Noise{Source: 30, Team: teamIntrude, Pos: math3d.Vec3{X: 3, Y: 1.5}, Loudness: 1})
	sys.PostNoise(perception3d.Noise{Source: 31, Team: teamIntrude, Pos: math3d.Vec3{X: 30, Y: 1.5}, Loudness: 2})
	sys.Step(step, nil)
	m, ok := g.Memory(30)
	if !ok || m.Pos != (math3d.Vec3{X: 3, Y: 1.5}) || m.Sense != ai.SenseHearing || m.Vel != (math3d.Vec3{}) || math.Abs(float64(m.Strength)-0.7) > 1e-6 {
		t.Fatalf("heard memory %+v %v", m, ok)
	}
	if _, ok := g.Memory(31); ok {
		t.Fatal("heard a noise beyond range × loudness")
	}
	mems := g.Memories()
	if len(mems) != 3 || mems[0].ID != 11 || mems[0].Visible || mems[0].Age != step {
		t.Fatalf("memories %+v", mems)
	}

	// Every memory is forgotten a second after it was last sensed.
	forgotten := 0
	for range 61 {
		sys.Step(step, nil)
		for _, ev := range g.Events() {
			if ev.Kind == ai.EventForgotten {
				forgotten++
			}
		}
	}
	if forgotten != 3 || len(g.Memories()) != 0 {
		t.Fatalf("forgotten %d, memories %+v", forgotten, g.Memories())
	}
}

func TestBudgetAndMembership(t *testing.T) {
	var calls int
	sys := perception3d.System{Budget: 2, LineOfSight: func(from, to math3d.Vec3) bool { calls++; return true }}
	var ps []*perception3d.Perception
	for i := range 3 {
		p := &perception3d.Perception{Pos: math3d.Vec3{X: float32(i)}, Sight: ai.SightConfig{Range: 100, HalfAngle: math.Pi}}
		sys.Add(p)
		sys.Add(p)
		ps = append(ps, p)
	}
	targets := []perception3d.Target{{ID: 5, Team: 2, Pos: math3d.Vec3{Z: -5}}, {ID: 6, Team: 2, Pos: math3d.Vec3{Z: 5}}}
	for s := range 3 {
		calls = 0
		sys.Step(step, targets)
		if calls != 2 || sys.Stats().Deferred != 4 {
			t.Fatalf("step %d: %d calls, stats %+v", s, calls, sys.Stats())
		}
	}
	for i, p := range ps {
		if len(p.Memories()) != 2 || len(p.AppendMemories(nil)) != 2 {
			t.Fatalf("agent %d after a full round: %+v", i, p.Memories())
		}
	}
	if sys.Len() != 3 || !sys.Remove(ps[0]) || sys.Remove(ps[0]) || sys.Len() != 2 {
		t.Fatal("Remove")
	}
	var other perception3d.System
	other.Add(ps[0])
	if other.Len() != 1 {
		t.Fatal("a removed perception did not join another system")
	}
	ps[0].Clear()
	if len(ps[0].Memories()) != 0 || len(ps[0].Events()) != 0 {
		t.Fatal("Clear")
	}
	if !ps[1].Forget(5) || ps[1].Forget(5) {
		t.Fatal("Forget")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("adding to a second system did not panic")
		}
	}()
	other.Add(ps[1])
}

func TestLoopDeterminism(t *testing.T) {
	w := wallWorld()
	run := func() []string {
		var log []string
		g := guard()
		g.Sight.Detect = ai.DetectAll
		sys := perception3d.System{LineOfSight: perception3d.PhysicsLineOfSight(w, 0), Budget: 1, Attitude: func(observer, other ai.Team) ai.Attitude { return ai.Hostile }}
		sys.Add(g)
		target := perception3d.Target{ID: 9, Team: teamGuards, Pos: math3d.Vec3{X: -15, Y: 1.5, Z: -20}, Vel: math3d.Vec3{X: 6}}
		frame := 0
		loop := &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				frame++
				target.Pos = target.Pos.Add(target.Vel.Scale(float32(dt.Seconds())))
				sys.Step(dt, []perception3d.Target{target, {ID: 8, Pos: math3d.Vec3{X: 1, Y: 1.5, Z: -25}}})
				for _, ev := range g.Events() {
					log = append(log, fmt.Sprintf("%d %v %d %v", frame, ev.Kind, ev.Memory.ID, ev.Memory.Pos))
				}
			},
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			loop.Run(cx)
			return core.View{}
		})
		app.Advance(6 * time.Second)
		return log
	}
	a, b := run(), run()
	if len(a) < 3 || !reflect.DeepEqual(a, b) {
		t.Fatalf("not deterministic or no events:\n%v\n%v", a, b)
	}
}

func TestNonFiniteInputsAndNilLineOfSight(t *testing.T) {
	nan := float32(math.NaN())
	g := guard()
	g.Forward = math3d.Vec3{X: nan, Y: nan, Z: nan} // falls back to -Z
	sys := perception3d.System{Budget: 1}
	sys.Add(g)
	targets := []perception3d.Target{
		{ID: 10, Team: teamIntrude, Pos: math3d.Vec3{Y: 1.5, Z: -20}, Vel: math3d.Vec3{X: nan}},
		{ID: 11, Team: teamIntrude, Pos: math3d.Vec3{X: 2, Y: 1.5, Z: -20}},
		{ID: 12, Team: teamIntrude, Pos: math3d.Vec3{X: nan, Y: 1.5, Z: -20}},
	}
	sys.PostNoise(perception3d.Noise{Source: 20, Team: teamIntrude, Pos: math3d.Vec3{Z: float32(math.Inf(-1))}, Loudness: 1})
	sys.Step(step, targets)
	if !g.Sees(10) || !g.Sees(11) || g.Sees(12) {
		t.Fatalf("seen 10 %v 11 %v 12 %v", g.Sees(10), g.Sees(11), g.Sees(12))
	}
	if _, heard := g.Memory(20); heard {
		t.Fatal("a noise at an infinite distance was heard")
	}
	if got, want := sys.Stats(), (ai.PerceptionStats{Candidates: 2}); got != want {
		t.Fatalf("stats = %+v, want %+v", got, want)
	}
	for s := range 3 {
		sys.Step(step, targets)
		if len(g.Events()) != 0 {
			t.Fatalf("step %d: events %+v", s, g.Events())
		}
	}
	if m, _ := g.Memory(10); m.Vel != (math3d.Vec3{}) {
		t.Fatalf("remembered velocity = %v, want zero", m.Vel)
	}
}

func TestCopyAndRemove(t *testing.T) {
	var sys perception3d.System
	g1 := guard()
	sys.Add(g1)
	target := []perception3d.Target{{ID: 10, Team: teamIntrude, Pos: math3d.Vec3{Y: 1.5, Z: -5}}}
	sys.Step(step, target)

	// A copy of an added Perception is a separate agent with its own memories.
	g2 := *g1
	sys.Add(&g2)
	if sys.Len() != 2 || !g2.Forget(10) || !g1.Sees(10) || len(g2.Events()) != 0 {
		t.Fatalf("copy: Len %d, original sees %v, copy events %+v", sys.Len(), g1.Sees(10), g2.Events())
	}

	// A removed agent keeps its memories but sees nothing and reports no events.
	sys.Remove(g1)
	if m, ok := g1.Memory(10); !ok || m.Visible || g1.Sees(10) || len(g1.Events()) != 0 {
		t.Fatalf("after Remove: memory %+v %v, events %+v", m, ok, g1.Events())
	}
	sys.Step(step, target)
	if !g2.Sees(10) || g1.Sees(10) {
		t.Fatalf("after a step: copy sees %v, removed original sees %v", g2.Sees(10), g1.Sees(10))
	}
}
