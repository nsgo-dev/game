package ai_test

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func ptDist(a, b core.Point) float64 {
	return math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y))
}

func ptLen(p core.Point) float64 {
	return math.Hypot(float64(p.X), float64(p.Y))
}

func TestSteeringSeekMovesTowardTarget(t *testing.T) {
	a := ai.Agent{
		Pos:      pt(0, 0),
		MaxSpeed: 100,
		MaxForce: 200,
	}
	target := pt(100, 0)
	dt := time.Second / 60

	for range 120 {
		f := ai.Seek(a, target)
		if ptLen(f) > float64(a.MaxForce)+1e-3 {
			t.Fatalf("seek force magnitude %v exceeds MaxForce %v", ptLen(f), a.MaxForce)
		}
		a.Step(f, dt)
		if ptLen(a.Vel) > float64(a.MaxSpeed)+1e-3 {
			t.Fatalf("speed %v exceeds MaxSpeed %v", ptLen(a.Vel), a.MaxSpeed)
		}
	}
	if a.Pos.X <= 80 {
		t.Fatalf("agent did not seek toward target: pos = %v", a.Pos)
	}
	if math.Abs(float64(a.Pos.Y)) > 1e-3 {
		t.Fatalf("agent drifted off axis: pos = %v", a.Pos)
	}
}

func TestSteeringFleeRespectsPanicRadius(t *testing.T) {
	threat := pt(0, 0)
	// Outside panic radius: zero force.
	far := ai.Agent{Pos: pt(60, 0), MaxSpeed: 50, MaxForce: 100}
	if f := ai.Flee(far, threat, 50); f != (core.Point{}) {
		t.Fatalf("outside panic radius gave force %v, want zero", f)
	}

	// Inside panic radius: accelerates away from threat.
	near := ai.Agent{Pos: pt(10, 0), MaxSpeed: 50, MaxForce: 100}
	dt := time.Second / 60
	for range 30 {
		near.Step(ai.Flee(near, threat, 50), dt)
	}
	if near.Pos.X <= 15 {
		t.Fatalf("agent did not flee from threat: pos = %v", near.Pos)
	}

	// Coincident with threat: deterministic non-zero force without NaN.
	onTop := ai.Agent{Pos: threat, MaxSpeed: 50, MaxForce: 100}
	f := ai.Flee(onTop, threat, 50)
	if math.IsNaN(float64(f.X)) || math.IsNaN(float64(f.Y)) || ptLen(f) == 0 {
		t.Fatalf("flee on top of threat gave %v, want deterministic non-zero force", f)
	}
}

func TestSteeringArriveSlowsAndStopsAtTarget(t *testing.T) {
	// Verify linear speed ramp inside slowRadius directly.
	probe := ai.Agent{Pos: pt(80, 0), Vel: pt(0, 0), MaxSpeed: 60, MaxForce: 200}
	halfForce := ai.Arrive(probe, pt(100, 0), 40) // d=20, slowRadius=40 -> desired speed = 30
	if math.Abs(float64(halfForce.X)-30) > 1e-3 || math.Abs(float64(halfForce.Y)) > 1e-3 {
		t.Fatalf("Arrive at half slowRadius = %v, want (30, 0)", halfForce)
	}

	// Simulate full approach and smooth stop at target.
	a := ai.Agent{
		Pos:      pt(0, 0),
		MaxSpeed: 60,
		MaxForce: 200,
	}
	target := pt(200, 0)
	dt := time.Second / 60

	var maxSpeedSeen float64
	for range 900 {
		f := ai.Arrive(a, target, 80)
		a.Step(f, dt)
		maxSpeedSeen = max(maxSpeedSeen, ptLen(a.Vel))
	}
	if maxSpeedSeen < 50 {
		t.Fatalf("agent never reached cruising speed: maxSpeed = %v", maxSpeedSeen)
	}
	if d := ptDist(a.Pos, target); d > 0.5 {
		t.Fatalf("agent did not settle at target: pos = %v (dist %v)", a.Pos, d)
	}
	if s := ptLen(a.Vel); s > 0.5 {
		t.Fatalf("agent did not come to rest: vel = %v (speed %v)", a.Vel, s)
	}
}

func TestSteeringWanderIsDeterministic(t *testing.T) {
	run := func() ai.Agent {
		rng := rand.New(rand.NewPCG(42, 99))
		a := ai.Agent{Pos: pt(0, 0), Vel: pt(10, 0), MaxSpeed: 40, MaxForce: 80}
		var st ai.WanderState
		dt := time.Second / 60
		for range 120 {
			f := ai.Wander(a, &st, 12, 24, 0.4, rng.Float32)
			a.Step(f, dt)
		}
		return a
	}

	r1 := run()
	r2 := run()
	if r1 != r2 {
		t.Fatalf("wander is not deterministic: run1=%+v run2=%+v", r1, r2)
	}
	if ptDist(r1.Pos, pt(0, 0)) < 10 {
		t.Fatalf("wander barely moved agent: pos=%v", r1.Pos)
	}
}

func TestSteeringSeparationPushesApart(t *testing.T) {
	a := ai.Agent{Pos: pt(0, 0), MaxSpeed: 60, MaxForce: 120, Radius: 10}
	neighbor := ai.Agent{Pos: pt(5, 0), MaxSpeed: 60, MaxForce: 120, Radius: 10}

	// Single close neighbor on +X pushes strictly toward -X.
	f := ai.Separation(a, []ai.Agent{a, neighbor}, 20)
	if f.X >= 0 || math.Abs(float64(f.Y)) > 1e-4 {
		t.Fatalf("separation force = %v, want negative X and zero Y", f)
	}

	// Symmetric neighbors cancel to zero.
	left := ai.Agent{Pos: pt(-5, 0), Radius: 10}
	if sym := ai.Separation(a, []ai.Agent{left, neighbor}, 20); ptLen(sym) > 1e-4 {
		t.Fatalf("symmetric separation = %v, want zero", sym)
	}

	// desiredSep <= 0 falls back to combined radii (10 + 10 = 20).
	fRad := ai.Separation(a, []ai.Agent{neighbor}, 0)
	if fRad != f {
		t.Fatalf("radius fallback force = %v, want %v", fRad, f)
	}

	// Distant neighbor produces zero force.
	far := ai.Agent{Pos: pt(100, 0), Radius: 10}
	if fFar := ai.Separation(a, []ai.Agent{far}, 20); fFar != (core.Point{}) {
		t.Fatalf("distant neighbor force = %v, want zero", fFar)
	}
}

func TestSteeringFollowPathLinearAndLooping(t *testing.T) {
	path := []core.Point{pt(50, 0), pt(50, 50), pt(0, 50)}
	dt := time.Second / 60

	// Non-looping: visits waypoints in order and stops at final waypoint with Done=true.
	a := ai.Agent{Pos: pt(0, 0), MaxSpeed: 100, MaxForce: 300}
	var st ai.PathFollowState
	reached1 := false
	for range 720 {
		f := ai.FollowPath(a, path, &st, 12, false)
		a.Step(f, dt)
		if st.Index >= 1 {
			reached1 = true
		}
	}
	if !reached1 || !st.Done || st.Index != 2 {
		t.Fatalf("linear FollowPath state = %+v (reached1=%v), pos=%v", st, reached1, a.Pos)
	}
	if d := ptDist(a.Pos, path[2]); d > 1 {
		t.Fatalf("agent did not finish at final waypoint: pos=%v (dist %v)", a.Pos, d)
	}

	// Looping: wraps back to waypoint 0 after reaching the final waypoint.
	b := ai.Agent{Pos: pt(0, 0), MaxSpeed: 120, MaxForce: 400}
	var loopSt ai.PathFollowState
	sawWrap := false
	prevIdx := 0
	for range 360 {
		f := ai.FollowPath(b, path, &loopSt, 10, true)
		b.Step(f, dt)
		if prevIdx == 2 && loopSt.Index == 0 {
			sawWrap = true
		}
		prevIdx = loopSt.Index
	}
	if !sawWrap || loopSt.Done {
		t.Fatalf("looping FollowPath did not wrap cleanly: sawWrap=%v state=%+v", sawWrap, loopSt)
	}
}

func TestSteeringCombineAndLoopDeterminism(t *testing.T) {
	// Combine clamps weighted sum to maxForce.
	combined := ai.Combine(10,
		ai.WeightedForce{Force: pt(20, 0), Weight: 1},
		ai.WeightedForce{Force: pt(0, 20), Weight: 1},
	)
	if math.Abs(ptLen(combined)-10) > 1e-4 {
		t.Fatalf("Combine magnitude = %v, want 10", ptLen(combined))
	}

	// Run two independent game.Loop sessions via nsgotest and confirm bit-exact determinism.
	runLoop := func() ai.Agent {
		rng := rand.New(rand.NewPCG(7, 13))
		agent := ai.Agent{Pos: pt(0, 0), MaxSpeed: 80, MaxForce: 200, Radius: 8}
		var wst ai.WanderState
		neighbor := ai.Agent{Pos: pt(25, 10), Radius: 8}
		loop := &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				f := ai.Combine(agent.MaxForce,
					ai.WeightedForce{Force: ai.Seek(agent, pt(150, 0)), Weight: 1.0},
					ai.WeightedForce{Force: ai.Separation(agent, []ai.Agent{neighbor}, 20), Weight: 1.2},
					ai.WeightedForce{Force: ai.Wander(agent, &wst, 5, 10, 0.2, rng.Float32), Weight: 0.3},
				)
				agent.Step(f, dt)
			},
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			loop.Run(cx)
			return core.View{}
		})
		app.Advance(2 * time.Second)
		return agent
	}

	a1 := runLoop()
	a2 := runLoop()
	if a1 != a2 {
		t.Fatalf("steering across game.Loop steps is not deterministic: %+v != %+v", a1, a2)
	}
	if a1.Pos.X <= 50 {
		t.Fatalf("agent did not progress during loop run: pos = %v", a1.Pos)
	}
}
