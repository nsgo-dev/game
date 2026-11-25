package ai_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/nsgo/core"
)

func TestPerceptionNilLineOfSightIgnoresBudget(t *testing.T) {
	g := guard()
	sys := ai.PerceptionSystem{Budget: 2}
	sys.Add(g)
	var targets []ai.Target
	for i := range 10 {
		targets = append(targets, ai.Target{ID: ai.StimulusID(10 + i), Team: teamIntrude, Pos: pt(float32(10+5*i), 0)})
	}
	sys.Step(step, targets)
	for _, tg := range targets {
		if !g.Sees(tg.ID) {
			t.Fatalf("target %d not seen on the first step with a nil LineOfSight", tg.ID)
		}
	}
	if got, want := sys.Stats(), (ai.PerceptionStats{Candidates: 10}); got != want {
		t.Fatalf("stats = %+v, want %+v", got, want)
	}
}

func TestPerceptionNaNVelocityMakesNoEvents(t *testing.T) {
	g := guard()
	var sys ai.PerceptionSystem
	sys.Add(g)
	nan := core.Points(math.NaN())
	targets := []ai.Target{{ID: 10, Team: teamIntrude, Pos: pt(50, 0), Vel: core.Point{X: nan, Y: 1}}}
	sys.Step(step, targets)
	if len(g.Events()) != 1 || g.Events()[0].Kind != ai.EventGained {
		t.Fatalf("first step events = %+v", g.Events())
	}
	for s := range 3 {
		sys.Step(step, targets)
		if len(g.Events()) != 0 {
			t.Fatalf("step %d: a still target with a NaN velocity made events %+v", s, g.Events())
		}
	}
	if m, _ := g.Memory(10); m.Vel != (core.Point{}) {
		t.Fatalf("remembered velocity = %v, want zero", m.Vel)
	}
}

func TestPerceptionNonFiniteInputs(t *testing.T) {
	nan := core.Points(math.NaN())
	inf := core.Points(math.Inf(1))
	f32nan := float32(math.NaN())
	f32inf := float32(math.Inf(1))
	// Target 10 is ahead beyond the peripheral range, target 11 is behind within it, and noise 20 is 10 away from the guard.
	cases := []struct {
		name                 string
		mod                  func(g *ai.Perception, ts []ai.Target, n *ai.Noise)
		ahead, behind, heard bool
		strength             float32 // the heard memory's strength; negative skips the check
	}{
		{"baseline", func(*ai.Perception, []ai.Target, *ai.Noise) {}, true, true, true, 0.8},
		{"nan pos", func(g *ai.Perception, _ []ai.Target, _ *ai.Noise) { g.Pos = core.Point{X: nan, Y: nan} }, false, false, false, -1},
		{"inf pos", func(g *ai.Perception, _ []ai.Target, _ *ai.Noise) { g.Pos = core.Point{X: inf} }, false, false, false, -1},
		{"nan facing faces +X", func(g *ai.Perception, _ []ai.Target, _ *ai.Noise) { g.Facing = core.Point{X: nan, Y: nan} }, true, true, true, 0.8},
		{"inf facing faces +X", func(g *ai.Perception, _ []ai.Target, _ *ai.Noise) { g.Facing = core.Point{X: -inf, Y: inf} }, true, true, true, 0.8},
		{"nan range", func(g *ai.Perception, _ []ai.Target, _ *ai.Noise) { g.Sight.Range = f32nan }, false, true, true, 0.8},
		{"inf range", func(g *ai.Perception, ts []ai.Target, _ *ai.Noise) { g.Sight.Range = f32inf; ts[0].Pos = pt(1e30, 0) }, true, true, true, 0.8},
		{"nan half angle", func(g *ai.Perception, _ []ai.Target, _ *ai.Noise) { g.Sight.HalfAngle = f32nan }, false, true, true, 0.8},
		{"nan peripheral range", func(g *ai.Perception, _ []ai.Target, _ *ai.Noise) { g.Sight.PeripheralRange = f32nan }, true, false, true, 0.8},
		{"nan peripheral half angle", func(g *ai.Perception, _ []ai.Target, _ *ai.Noise) { g.Sight.PeripheralHalfAngle = f32nan }, true, false, true, 0.8},
		{"negative peripheral half angle", func(g *ai.Perception, _ []ai.Target, _ *ai.Noise) { g.Sight.PeripheralHalfAngle = -1 }, true, false, true, 0.8},
		{"nan target pos", func(_ *ai.Perception, ts []ai.Target, _ *ai.Noise) { ts[0].Pos = core.Point{X: nan} }, false, true, true, 0.8},
		{"inf target pos", func(_ *ai.Perception, ts []ai.Target, _ *ai.Noise) { ts[0].Pos = core.Point{X: inf} }, false, true, true, 0.8},
		{"nan loudness", func(_ *ai.Perception, _ []ai.Target, n *ai.Noise) { n.Loudness = f32nan }, true, true, false, -1},
		{"negative loudness", func(_ *ai.Perception, _ []ai.Target, n *ai.Noise) { n.Loudness = -1 }, true, true, false, -1},
		{"inf loudness", func(_ *ai.Perception, _ []ai.Target, n *ai.Noise) { n.Loudness = f32inf }, true, true, true, 1},
		{"nan hearing range", func(g *ai.Perception, _ []ai.Target, _ *ai.Noise) { g.Hearing.Range = f32nan }, true, true, false, -1},
		{"inf hearing range", func(g *ai.Perception, _ []ai.Target, n *ai.Noise) { g.Hearing.Range = f32inf; n.Pos = pt(1e30, 0) }, true, true, true, 1},
		{"inf hearing range and zero loudness", func(g *ai.Perception, _ []ai.Target, n *ai.Noise) { g.Hearing.Range = f32inf; n.Loudness = 0 }, true, true, false, -1},
		{"nan noise pos", func(_ *ai.Perception, _ []ai.Target, n *ai.Noise) { n.Pos = core.Point{Y: nan} }, true, true, false, -1},
		{"inf noise pos", func(g *ai.Perception, _ []ai.Target, n *ai.Noise) {
			g.Hearing.Range = f32inf
			n.Pos = core.Point{X: inf}
		}, true, true, false, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := guard()
			targets := []ai.Target{{ID: 10, Team: teamIntrude, Pos: pt(50, 0)}, {ID: 11, Team: teamIntrude, Pos: pt(-10, 0)}}
			noise := ai.Noise{Source: 20, Team: teamIntrude, Pos: pt(10, 0), Loudness: 1}
			c.mod(g, targets, &noise)
			var sys ai.PerceptionSystem
			sys.Add(g)
			sys.PostNoise(noise)
			sys.Step(step, targets)
			m, heard := g.Memory(20)
			if g.Sees(10) != c.ahead || g.Sees(11) != c.behind || heard != c.heard {
				t.Fatalf("ahead %v behind %v heard %v, want %v %v %v", g.Sees(10), g.Sees(11), heard, c.ahead, c.behind, c.heard)
			}
			if c.strength >= 0 && math.Abs(float64(m.Strength-c.strength)) > 1e-6 {
				t.Fatalf("heard strength = %v, want %v", m.Strength, c.strength)
			}
			if want := btoi(c.ahead) + btoi(c.behind) + btoi(c.heard); len(g.Events()) != want {
				t.Fatalf("events = %+v, want %d", g.Events(), want)
			}
		})
	}
}

func TestPerceptionZeroAndNegativeStep(t *testing.T) {
	g := guard()
	var sys ai.PerceptionSystem
	sys.Add(g)
	sys.Step(step, []ai.Target{{ID: 10, Team: teamIntrude, Pos: pt(50, 0)}})
	for _, dt := range []time.Duration{0, -time.Hour} {
		sys.Step(dt, nil)
		m, ok := g.Memory(10)
		if !ok || m.Age != 0 || m.Strength != 1 || m.Visible {
			t.Fatalf("dt %v: memory %+v, %v", dt, m, ok)
		}
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
