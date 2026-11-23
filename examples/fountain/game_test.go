package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const step = time.Second / StepRate

func snapshot(s *Scene) [][]particles.Particle {
	out := make([][]particles.Particle, len(s.Emitters))
	for i, e := range s.Emitters {
		out[i] = append([]particles.Particle(nil), e.Particles()...)
	}
	return out
}

func TestSceneHoldsTargetEveryStep(t *testing.T) {
	s := NewScene(DefaultSeed)
	for i := range 3 * StepRate {
		s.Step(step)
		if got := s.Count(); got != DefaultTarget {
			t.Fatalf("step %d: Count() = %d, want %d", i+1, got, DefaultTarget)
		}
	}
}

func TestSceneIsDeterministicForASeed(t *testing.T) {
	a, b := NewScene(DefaultSeed), NewScene(DefaultSeed)
	for range 5 * StepRate {
		a.Step(step)
		b.Step(step)
	}
	first := snapshot(a)
	if !reflect.DeepEqual(first, snapshot(b)) {
		t.Fatal("two scenes with the same seed diverged")
	}

	other := NewScene(DefaultSeed + 1)
	for range 5 * StepRate {
		other.Step(step)
	}
	if reflect.DeepEqual(first, snapshot(other)) {
		t.Fatal("scenes with different seeds produced identical particles")
	}

	a.Reset()
	for range 5 * StepRate {
		a.Step(step)
	}
	if !reflect.DeepEqual(first, snapshot(a)) {
		t.Fatal("Reset did not replay the scene")
	}
}

func TestSetTargetClampsAndSplits(t *testing.T) {
	s := NewScene(DefaultSeed)
	for _, tc := range []struct{ in, want int }{
		{0, MinTarget},
		{MaxTarget + 1, MaxTarget},
		{5003, 5003},
	} {
		s.SetTarget(tc.in)
		if s.Target != tc.want {
			t.Fatalf("SetTarget(%d): Target = %d, want %d", tc.in, s.Target, tc.want)
		}
		sum := 0
		for _, e := range s.Emitters {
			sum += e.MaxParticles
		}
		if sum != tc.want {
			t.Fatalf("SetTarget(%d): emitters hold %d, want %d", tc.in, sum, tc.want)
		}
	}
}

func TestViewDrawsEveryParticle(t *testing.T) {
	var c *Controller
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		c, node = NewView(cx, DefaultSeed)
		return node
	}, nsgotest.Size(VirtualWidth, VirtualHeight))

	app.Advance(2 * time.Second)
	if c.Scene.Steps < 2*StepRate {
		t.Fatalf("Steps = %d after 2s, want >= %d", c.Scene.Steps, 2*StepRate)
	}
	if got := c.Scene.Count(); got != DefaultTarget {
		t.Fatalf("Count() = %d, want %d", got, DefaultTarget)
	}

	visible := 0
	for _, e := range c.Scene.Emitters {
		for _, p := range e.Particles() {
			if p.Alpha > 0 && p.Size != 0 {
				visible++
			}
		}
	}
	images := 0
	for _, op := range app.Find("fountain-canvas").Ops {
		if op.Kind == core.DrawImage {
			if op.Image == nil || op.Image.Pixels == nil {
				t.Fatalf("DrawImage op without the procedural atlas: %+v", op)
			}
			images++
		}
	}
	if images != visible || visible < DefaultTarget*9/10 {
		t.Fatalf("drew %d images for %d visible of %d particles", images, visible, DefaultTarget)
	}
	if got := app.Find("hud-count").Text; got != "5000 particles" {
		t.Fatalf("hud-count = %q, want %q", got, "5000 particles")
	}
}

func TestKeysChangeTarget(t *testing.T) {
	var c *Controller
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		c, node = NewView(cx, DefaultSeed)
		return node
	}, nsgotest.Size(VirtualWidth, VirtualHeight))

	app.Key("up")
	app.Advance(time.Second)
	if c.Scene.Target != DefaultTarget+TargetStep || c.Scene.Count() != DefaultTarget+TargetStep {
		t.Fatalf("after up: Target = %d, Count() = %d, want %d", c.Scene.Target, c.Scene.Count(), DefaultTarget+TargetStep)
	}
	if got := app.Find("hud-count").Text; got != "6000 particles" {
		t.Fatalf("hud-count = %q, want %q", got, "6000 particles")
	}

	app.Key("down")
	app.Key("down")
	if c.Scene.Target != DefaultTarget-TargetStep {
		t.Fatalf("after down twice: Target = %d, want %d", c.Scene.Target, DefaultTarget-TargetStep)
	}
	// Particles above the new target live out their lifetimes, at most LifetimeMax.
	app.Advance(3 * time.Second)
	if got := c.Scene.Count(); got != DefaultTarget-TargetStep {
		t.Fatalf("Count() = %d after lowering the target, want %d", got, DefaultTarget-TargetStep)
	}

	app.Key("r")
	if c.Scene.Steps != 0 || c.Scene.Count() != 0 {
		t.Fatalf("after r: Steps = %d, Count() = %d, want 0 and 0", c.Scene.Steps, c.Scene.Count())
	}
}

// BenchmarkScene5000 measures one fixed step plus one frame's draw of the 5000-particle scene, the CPU side of what --nsgo-fps reports.
func BenchmarkScene5000(b *testing.B) {
	s := NewScene(DefaultSeed)
	for range 3 * StepRate {
		s.Step(step)
	}
	d := core.Drawing{Ops: make([]core.DrawOp, 0, 8*DefaultTarget)}
	size := core.Size{Width: VirtualWidth, Height: VirtualHeight}
	s.Draw(&d, size)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s.Step(step)
		d.Ops = d.Ops[:0]
		s.Draw(&d, size)
	}
	if s.Count() != DefaultTarget {
		b.Fatalf("Count() = %d, want %d", s.Count(), DefaultTarget)
	}
}
