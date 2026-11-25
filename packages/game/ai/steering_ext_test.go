package ai_test

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/world3d"
	"github.com/nsgo-dev/nsgo/core"
)

func v3Len(v ai.Vec3) float64 {
	return math.Sqrt(float64(v.X)*float64(v.X) + float64(v.Y)*float64(v.Y) + float64(v.Z)*float64(v.Z))
}

func v3Dist(a, b ai.Vec3) float64 {
	return v3Len(ai.Vec3{X: a.X - b.X, Y: a.Y - b.Y, Z: a.Z - b.Z})
}

func v3DistXZ(a, b ai.Vec3) float64 {
	return math.Hypot(float64(a.X-b.X), float64(a.Z-b.Z))
}

func distPointToSegment(p, a, b core.Point) float64 {
	ax, ay := float64(a.X), float64(a.Y)
	bx, by := float64(b.X), float64(b.Y)
	px, py := float64(p.X), float64(p.Y)
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	if l2 <= 1e-18 {
		return math.Hypot(px-ax, py-ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / l2
	t = min(max(t, 0), 1)
	cx := ax + t*dx
	cy := ay + t*dy
	return math.Hypot(px-cx, py-cy)
}

func TestSteeringExtPursueEvadeOffsetTable(t *testing.T) {
	t.Run("Pursue", func(t *testing.T) {
		tests := []struct {
			name         string
			agent        ai.Agent
			target       ai.Agent
			maxLookAhead float32
			check        func(t *testing.T, f core.Point)
		}{
			{
				name:   "leads moving perpendicular target",
				agent:  ai.Agent{Pos: pt(0, 0), Vel: pt(0, 0), MaxSpeed: 100, MaxForce: 200},
				target: ai.Agent{Pos: pt(100, 0), Vel: pt(0, 100)},
				check: func(t *testing.T, f core.Point) {
					// Look-ahead = 100/(100+100) = 0.5s -> predicted (100, 50)
					if f.X <= 0 || f.Y <= 0 {
						t.Fatalf("Pursue force = %v, want positive X and positive Y lead", f)
					}
					seekDirect := ai.Seek(ai.Agent{Pos: pt(0, 0), MaxSpeed: 100, MaxForce: 200}, pt(100, 0))
					if f.Y <= seekDirect.Y+10 {
						t.Fatalf("Pursue did not lead target velocity: pursue=%v seek=%v", f, seekDirect)
					}
				},
			},
			{
				name:         "respects maxLookAhead cap",
				agent:        ai.Agent{Pos: pt(0, 0), Vel: pt(0, 0), MaxSpeed: 100, MaxForce: 500},
				target:       ai.Agent{Pos: pt(200, 0), Vel: pt(0, 100)},
				maxLookAhead: 0.2,
				check: func(t *testing.T, f core.Point) {
					// Natural lookAhead = 200/200 = 1.0s -> (200, 100); capped 0.2s -> (200, 20).
					uncapped := ai.Pursue(
						ai.Agent{Pos: pt(0, 0), Vel: pt(0, 0), MaxSpeed: 100, MaxForce: 500},
						ai.Agent{Pos: pt(200, 0), Vel: pt(0, 100)},
						0,
					)
					if f.Y >= uncapped.Y {
						t.Fatalf("capped Pursue Y (%v) should be less than uncapped (%v)", f.Y, uncapped.Y)
					}
					// Direction should match Seek toward (200, 20).
					want := ai.Seek(ai.Agent{Pos: pt(0, 0), MaxSpeed: 100, MaxForce: 500}, pt(200, 20))
					if ptDist(f, want) > 1e-3 {
						t.Fatalf("capped Pursue = %v, want %v", f, want)
					}
				},
			},
			{
				name:   "head-on approaching target seeks directly",
				agent:  ai.Agent{Pos: pt(0, 0), Vel: pt(50, 0), MaxSpeed: 100, MaxForce: 200},
				target: ai.Agent{Pos: pt(60, 0), Vel: pt(-80, 0)},
				check: func(t *testing.T, f core.Point) {
					want := ai.Seek(ai.Agent{Pos: pt(0, 0), Vel: pt(50, 0), MaxSpeed: 100, MaxForce: 200}, pt(60, 0))
					if ptDist(f, want) > 1e-4 {
						t.Fatalf("head-on Pursue = %v, want direct Seek %v", f, want)
					}
				},
			},
			{
				name:   "zero MaxSpeed returns zero",
				agent:  ai.Agent{Pos: pt(0, 0), MaxSpeed: 0, MaxForce: 100},
				target: ai.Agent{Pos: pt(10, 0), Vel: pt(5, 5)},
				check: func(t *testing.T, f core.Point) {
					if f != (core.Point{}) {
						t.Fatalf("got %v, want zero", f)
					}
				},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				f1 := ai.Pursue(tc.agent, tc.target, tc.maxLookAhead)
				f2 := ai.Pursue(tc.agent, tc.target, tc.maxLookAhead)
				if f1 != f2 {
					t.Fatalf("Pursue not deterministic: %v != %v", f1, f2)
				}
				tc.check(t, f1)
			})
		}
	})

	t.Run("Evade", func(t *testing.T) {
		tests := []struct {
			name         string
			agent        ai.Agent
			pursuer      ai.Agent
			panicRadius  float32
			maxLookAhead float32
			check        func(t *testing.T, f core.Point)
		}{
			{
				name:        "outside panicRadius returns zero",
				agent:       ai.Agent{Pos: pt(100, 0), MaxSpeed: 80, MaxForce: 200},
				pursuer:     ai.Agent{Pos: pt(0, 0), Vel: pt(50, 0)},
				panicRadius: 90,
				check: func(t *testing.T, f core.Point) {
					if f != (core.Point{}) {
						t.Fatalf("outside panicRadius got %v, want zero", f)
					}
				},
			},
			{
				name:        "evades predicted intercept point",
				agent:       ai.Agent{Pos: pt(0, 40), MaxSpeed: 80, MaxForce: 200},
				pursuer:     ai.Agent{Pos: pt(0, 0), Vel: pt(80, 0)},
				panicRadius: 100,
				check: func(t *testing.T, f core.Point) {
					// Pursuer predicted to +X -> agent should flee toward -X and +Y.
					if f.X >= -1 || f.Y <= 1 {
						t.Fatalf("Evade force = %v, want negative X and positive Y", f)
					}
				},
			},
			{
				name:         "capped maxLookAhead",
				agent:        ai.Agent{Pos: pt(0, 50), MaxSpeed: 50, MaxForce: 200},
				pursuer:      ai.Agent{Pos: pt(0, 0), Vel: pt(50, 0)},
				panicRadius:  0,
				maxLookAhead: 0.1,
				check: func(t *testing.T, f core.Point) {
					// Predicted pursuer = (5, 0).
					want := ai.Flee(ai.Agent{Pos: pt(0, 50), MaxSpeed: 50, MaxForce: 200}, pt(5, 0), 0)
					if ptDist(f, want) > 1e-3 {
						t.Fatalf("capped Evade = %v, want %v", f, want)
					}
				},
			},
			{
				name:        "coincident pursuer returns deterministic finite non-zero",
				agent:       ai.Agent{Pos: pt(10, 10), MaxSpeed: 50, MaxForce: 100},
				pursuer:     ai.Agent{Pos: pt(10, 10), Vel: pt(0, 0)},
				panicRadius: 50,
				check: func(t *testing.T, f core.Point) {
					if math.IsNaN(float64(f.X)) || math.IsNaN(float64(f.Y)) || ptLen(f) == 0 {
						t.Fatalf("coincident Evade = %v, want finite non-zero", f)
					}
				},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				f1 := ai.Evade(tc.agent, tc.pursuer, tc.panicRadius, tc.maxLookAhead)
				f2 := ai.Evade(tc.agent, tc.pursuer, tc.panicRadius, tc.maxLookAhead)
				if f1 != f2 {
					t.Fatalf("Evade not deterministic: %v != %v", f1, f2)
				}
				tc.check(t, f1)
			})
		}
	})

	t.Run("OffsetPursuit", func(t *testing.T) {
		// Leader moving along +Y: forward = (0, 1), left side = (-1, 0).
		// localOffset = (-10, 20) -> worldOffset = (0, -10) + (-20, 0) = (-20, -10).
		leader := ai.Agent{Pos: pt(50, 50), Vel: pt(0, 40)}
		follower := ai.Agent{Pos: pt(30, 0), MaxSpeed: 100, MaxForce: 300}
		f := ai.OffsetPursuit(follower, leader, pt(-10, 20), 10, 0.5)
		// World target is (30, 40) + leaderVel*lookAhead -> strictly +Y from follower at (30, 0).
		if math.Abs(float64(f.X)) > 1e-3 || f.Y <= 0 {
			t.Fatalf("OffsetPursuit force = %v, want purely +Y toward rotated offset", f)
		}

		// Simulate follower settling at offset behind stationary leader.
		stillLeader := ai.Agent{Pos: pt(100, 100), Vel: pt(0, 0)}
		a := ai.Agent{Pos: pt(0, 0), MaxSpeed: 120, MaxForce: 360}
		dt := time.Second / 60
		for range 900 {
			a.Step(ai.OffsetPursuit(a, stillLeader, pt(-20, 15), 60, 1.0), dt)
		}
		wantPos := pt(80, 115) // default fwd=(1,0), side=(0,1)
		if d := ptDist(a.Pos, wantPos); d > 0.5 {
			t.Fatalf("OffsetPursuit settled at %v, want %v (dist %v)", a.Pos, wantPos, d)
		}
	})
}

func TestSteeringExtFlockingAndTacticalTable(t *testing.T) {
	t.Run("Alignment", func(t *testing.T) {
		a := ai.Agent{Pos: pt(0, 0), Vel: pt(0, 50), MaxSpeed: 100, MaxForce: 200}
		neighbors := []ai.Agent{
			a, // self should be skipped
			{Pos: pt(10, 0), Vel: pt(80, 0)},
			{Pos: pt(20, 0), Vel: pt(40, 0)},
			{Pos: pt(100, 0), Vel: pt(0, -100)}, // outside radius=50
		}
		f := ai.Alignment(a, neighbors, 50)
		// Average neighbor heading is +X -> desired (100, 0), steer = (100, -50).
		if math.Abs(float64(f.X)-100) > 1e-3 || math.Abs(float64(f.Y)+50) > 1e-3 {
			t.Fatalf("Alignment = %v, want (100, -50)", f)
		}
		// Distant only -> zero.
		if z := ai.Alignment(a, neighbors[3:], 50); z != (core.Point{}) {
			t.Fatalf("Alignment with no in-range neighbors = %v, want zero", z)
		}
	})

	t.Run("Cohesion", func(t *testing.T) {
		a := ai.Agent{Pos: pt(0, 0), Vel: pt(0, 0), MaxSpeed: 100, MaxForce: 200}
		neighbors := []ai.Agent{
			a,
			{Pos: pt(20, 10)},
			{Pos: pt(20, -10)},
			{Pos: pt(200, 200)}, // outside radius=40
		}
		f := ai.Cohesion(a, neighbors, 40)
		// Center of mass is (20, 0) -> Seek toward (20, 0) gives (100, 0).
		if math.Abs(float64(f.X)-100) > 1e-3 || math.Abs(float64(f.Y)) > 1e-3 {
			t.Fatalf("Cohesion = %v, want (100, 0)", f)
		}
	})

	t.Run("ObstacleAvoidance", func(t *testing.T) {
		tests := []struct {
			name      string
			agent     ai.Agent
			obstacles []ai.Obstacle
			maxAhead  float32
			check     func(t *testing.T, f core.Point)
		}{
			{
				name:      "obstacle dead ahead deflects laterally and brakes",
				agent:     ai.Agent{Pos: pt(0, 0), Vel: pt(60, 0), MaxSpeed: 60, MaxForce: 150, Radius: 8},
				obstacles: []ai.Obstacle{{Center: pt(30, 0), Radius: 10}},
				maxAhead:  60,
				check: func(t *testing.T, f core.Point) {
					if f.Y >= -10 || f.X >= 0 {
						t.Fatalf("dead-ahead avoidance = %v, want negative Y deflection and braking X", f)
					}
				},
			},
			{
				name:      "obstacle to left deflects right (-Y)",
				agent:     ai.Agent{Pos: pt(0, 0), Vel: pt(60, 0), MaxSpeed: 60, MaxForce: 150, Radius: 8},
				obstacles: []ai.Obstacle{{Center: pt(30, 6), Radius: 10}},
				maxAhead:  60,
				check: func(t *testing.T, f core.Point) {
					if f.Y >= -10 {
						t.Fatalf("left obstacle avoidance = %v, want negative Y", f)
					}
				},
			},
			{
				name:      "obstacle to right deflects left (+Y)",
				agent:     ai.Agent{Pos: pt(0, 0), Vel: pt(60, 0), MaxSpeed: 60, MaxForce: 150, Radius: 8},
				obstacles: []ai.Obstacle{{Center: pt(30, -6), Radius: 10}},
				maxAhead:  60,
				check: func(t *testing.T, f core.Point) {
					if f.Y <= 10 {
						t.Fatalf("right obstacle avoidance = %v, want positive Y", f)
					}
				},
			},
			{
				name:      "obstacle behind is ignored",
				agent:     ai.Agent{Pos: pt(0, 0), Vel: pt(60, 0), MaxSpeed: 60, MaxForce: 150, Radius: 8},
				obstacles: []ai.Obstacle{{Center: pt(-30, 0), Radius: 10}},
				maxAhead:  60,
				check: func(t *testing.T, f core.Point) {
					if f != (core.Point{}) {
						t.Fatalf("behind obstacle avoidance = %v, want zero", f)
					}
				},
			},
			{
				name:      "laterally clear obstacle is ignored",
				agent:     ai.Agent{Pos: pt(0, 0), Vel: pt(60, 0), MaxSpeed: 60, MaxForce: 150, Radius: 8},
				obstacles: []ai.Obstacle{{Center: pt(30, 25), Radius: 10}},
				maxAhead:  60,
				check: func(t *testing.T, f core.Point) {
					if f != (core.Point{}) {
						t.Fatalf("clear obstacle avoidance = %v, want zero", f)
					}
				},
			},
			{
				name:  "picks nearest threatening obstacle",
				agent: ai.Agent{Pos: pt(0, 0), Vel: pt(60, 0), MaxSpeed: 60, MaxForce: 150, Radius: 8},
				obstacles: []ai.Obstacle{
					{Center: pt(50, 5), Radius: 10},  // farther, on left -> would push -Y
					{Center: pt(25, -5), Radius: 10}, // nearer, on right -> pushes +Y
				},
				maxAhead: 80,
				check: func(t *testing.T, f core.Point) {
					if f.Y <= 10 {
						t.Fatalf("nearest obstacle avoidance = %v, want positive Y from closer right obstacle", f)
					}
				},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				f := ai.ObstacleAvoidance(tc.agent, tc.obstacles, tc.maxAhead)
				tc.check(t, f)
			})
		}
	})

	t.Run("WallAvoidance", func(t *testing.T) {
		wall := ai.Wall{A: pt(50, -100), B: pt(50, 100)}
		revWall := ai.Wall{A: pt(50, 100), B: pt(50, -100)}
		a := ai.Agent{Pos: pt(30, 0), Vel: pt(80, 0), MaxSpeed: 80, MaxForce: 200, Radius: 8}

		f1 := ai.WallAvoidance(a, []ai.Wall{wall}, 35)
		f2 := ai.WallAvoidance(a, []ai.Wall{revWall}, 35)
		if f1.X >= -10 || math.Abs(float64(f1.Y)) > 1e-3 {
			t.Fatalf("head-on WallAvoidance = %v, want strong -X repulsion", f1)
		}
		if ptDist(f1, f2) > 1e-4 {
			t.Fatalf("WallAvoidance depends on segment winding: %v != %v", f1, f2)
		}

		// Wall far away -> zero force.
		farWall := ai.Wall{A: pt(200, -100), B: pt(200, 100)}
		if z := ai.WallAvoidance(a, []ai.Wall{farWall}, 35); z != (core.Point{}) {
			t.Fatalf("distant WallAvoidance = %v, want zero", z)
		}

		// Sliding parallel near bottom wall keeps forward tangential velocity intact (zero X drag).
		bottomWall := ai.Wall{A: pt(-100, 0), B: pt(200, 0)}
		slider := ai.Agent{Pos: pt(0, 10), Vel: pt(80, 0), MaxSpeed: 80, MaxForce: 200, Radius: 8}
		fSlide := ai.WallAvoidance(slider, []ai.Wall{bottomWall}, 30)
		if fSlide.Y <= 0 || math.Abs(float64(fSlide.X)) > 1e-3 {
			t.Fatalf("parallel WallAvoidance = %v, want pure +Y push with zero X drag", fSlide)
		}
	})

	t.Run("Hide", func(t *testing.T) {
		pursuer := ai.Agent{Pos: pt(0, 0), Vel: pt(10, 0)}
		obstacles := []ai.Obstacle{
			{Center: pt(50, 0), Radius: 10},   // spot at (68, 0) with standoff=3, r=5
			{Center: pt(200, 50), Radius: 10}, // much farther
		}
		a := ai.Agent{Pos: pt(60, 20), MaxSpeed: 80, MaxForce: 240, Radius: 5}
		dt := time.Second / 60
		for range 600 {
			a.Step(ai.Hide(a, pursuer, obstacles, 3, 40), dt)
		}
		wantSpot := pt(68, 0)
		if d := ptDist(a.Pos, wantSpot); d > 0.5 {
			t.Fatalf("Hide settled at %v, want %v (dist %v)", a.Pos, wantSpot, d)
		}

		// No obstacles -> falls back to Evade.
		fNoObs := ai.Hide(a, pursuer, nil, 3, 20)
		fEvade := ai.Evade(a, pursuer, 0, 0)
		if fNoObs != fEvade {
			t.Fatalf("Hide fallback = %v, want Evade %v", fNoObs, fEvade)
		}
	})

	t.Run("Interpose", func(t *testing.T) {
		// Stationary targets: arrive at exact midpoint (50, 50).
		ta := ai.Agent{Pos: pt(0, 50)}
		tb := ai.Agent{Pos: pt(100, 50)}
		a := ai.Agent{Pos: pt(50, 0), MaxSpeed: 100, MaxForce: 300}
		dt := time.Second / 60
		for range 600 {
			a.Step(ai.Interpose(a, ta, tb, 50), dt)
		}
		if d := ptDist(a.Pos, pt(50, 50)); d > 0.5 {
			t.Fatalf("stationary Interpose settled at %v, want (50, 50)", a.Pos)
		}

		// Moving targets: leads midpoint along their velocity (+X).
		mA := ai.Agent{Pos: pt(0, 100), Vel: pt(60, 0)}
		mB := ai.Agent{Pos: pt(0, 120), Vel: pt(60, 0)}
		interceptor := ai.Agent{Pos: pt(0, 0), MaxSpeed: 110, MaxForce: 300}
		f := ai.Interpose(interceptor, mA, mB, 10)
		if f.X <= 10 || f.Y <= 10 {
			t.Fatalf("moving Interpose force = %v, want positive X lead and positive Y", f)
		}
	})

	t.Run("CombinePrioritized", func(t *testing.T) {
		// Priority 0 uses 60 of 100 budget; Priority 1 wants 80 -> truncated to 40; Priority 2 ignored.
		f := ai.CombinePrioritized(100,
			ai.WeightedForce{Force: pt(60, 0), Weight: 1},
			ai.WeightedForce{Force: pt(0, 80), Weight: 1},
			ai.WeightedForce{Force: pt(-50, 0), Weight: 1},
		)
		if math.Abs(float64(f.X)-60) > 1e-3 || math.Abs(float64(f.Y)-40) > 1e-3 {
			t.Fatalf("CombinePrioritized = %v, want (60, 40)", f)
		}

		// Priority 0 saturates full budget -> Priority 1 completely ignored.
		fSat := ai.CombinePrioritized(100,
			ai.WeightedForce{Force: pt(150, 0), Weight: 1},
			ai.WeightedForce{Force: pt(0, 100), Weight: 1},
		)
		if math.Abs(float64(fSat.X)-100) > 1e-3 || math.Abs(float64(fSat.Y)) > 1e-3 {
			t.Fatalf("saturated CombinePrioritized = %v, want (100, 0)", fSat)
		}

		// Zero weight entry consumes no budget.
		fSkip := ai.CombinePrioritized(100,
			ai.WeightedForce{Force: pt(200, 0), Weight: 0},
			ai.WeightedForce{Force: pt(0, 50), Weight: 1},
		)
		if math.Abs(float64(fSkip.X)) > 1e-3 || math.Abs(float64(fSkip.Y)-50) > 1e-3 {
			t.Fatalf("zero-weight CombinePrioritized = %v, want (0, 50)", fSkip)
		}
	})
}

func makeFlock2D(n int) []ai.Agent {
	rng := rand.New(rand.NewPCG(839, 2026))
	agents := make([]ai.Agent, n)
	for i := range agents {
		ang := rng.Float64() * 2 * math.Pi
		agents[i] = ai.Agent{
			Pos:      core.Point{X: core.Points(rng.Float64()*400 - 200), Y: core.Points(rng.Float64()*400 - 200)},
			Vel:      core.Point{X: core.Points(math.Cos(ang) * 40), Y: core.Points(math.Sin(ang) * 40)},
			MaxSpeed: 80,
			MaxForce: 180,
			Radius:   6,
		}
	}
	return agents
}

func stepFlock2D(agents []ai.Agent, forces []core.Point, grid *ai.NeighborGrid, dt time.Duration) {
	grid.Rebuild(agents)
	for i := range agents {
		neighbors := grid.Query(agents[i].Pos, 50)
		forces[i] = ai.CombinePrioritized(agents[i].MaxForce,
			ai.WeightedForce{Force: ai.Separation(agents[i], neighbors, 18), Weight: 1.6},
			ai.WeightedForce{Force: ai.Alignment(agents[i], neighbors, 45), Weight: 1.0},
			ai.WeightedForce{Force: ai.Cohesion(agents[i], neighbors, 50), Weight: 0.9},
		)
	}
	for i := range agents {
		agents[i].Step(forces[i], dt)
	}
}

func TestNeighborGridMatchesBruteForce(t *testing.T) {
	agents := makeFlock2D(150)
	grid := ai.NewNeighborGrid(32)
	grid.Rebuild(agents)

	for _, radius := range []float32{0, 15, 45, 500} {
		for _, probe := range []core.Point{pt(0, 0), pt(-120, 75), pt(180, -180)} {
			got := grid.Query(probe, radius)
			var want []ai.Agent
			for _, a := range agents {
				if ptDist(a.Pos, probe) <= float64(radius)+1e-5 {
					want = append(want, a)
				}
			}
			// Compare as position multisets (both should match in count and members).
			if len(got) != len(want) {
				t.Fatalf("radius=%v probe=%v: got %d neighbors, want %d", radius, probe, len(got), len(want))
			}
			for _, w := range want {
				if !slices.Contains(got, w) {
					t.Fatalf("radius=%v probe=%v: missing expected neighbor %+v", radius, probe, w)
				}
			}
		}
	}
}

func TestFlocking200AgentsZeroAllocAndDeterministic(t *testing.T) {
	const count = 200
	dt := time.Second / 60

	run := func(steps int) []ai.Agent {
		agents := makeFlock2D(count)
		forces := make([]core.Point, count)
		grid := ai.NewNeighborGrid(50)
		for range steps {
			stepFlock2D(agents, forces, grid, dt)
		}
		return agents
	}

	r1 := run(60)
	r2 := run(60)
	for i := range count {
		if r1[i] != r2[i] {
			t.Fatalf("agent %d diverged across runs: %+v != %+v", i, r1[i], r2[i])
		}
		if math.IsNaN(float64(r1[i].Pos.X)) || math.IsNaN(float64(r1[i].Pos.Y)) {
			t.Fatalf("agent %d position became NaN: %+v", i, r1[i])
		}
		if ptLen(r1[i].Vel) > float64(r1[i].MaxSpeed)+1e-3 {
			t.Fatalf("agent %d speed %v exceeded MaxSpeed %v", i, ptLen(r1[i].Vel), r1[i].MaxSpeed)
		}
	}

	// Verify 0 heap allocations per step after warm-up.
	agents := makeFlock2D(count)
	forces := make([]core.Point, count)
	grid := ai.NewNeighborGrid(50)
	for range 5 {
		stepFlock2D(agents, forces, grid, dt)
	}
	allocs := testing.AllocsPerRun(50, func() {
		stepFlock2D(agents, forces, grid, dt)
	})
	if allocs != 0 {
		t.Fatalf("200-agent flocking step allocated %.2f times/op, want 0", allocs)
	}
}

func BenchmarkFlocking200Agents(b *testing.B) {
	const count = 200
	dt := time.Second / 60
	agents := makeFlock2D(count)
	forces := make([]core.Point, count)
	grid := ai.NewNeighborGrid(50)
	for range 5 {
		stepFlock2D(agents, forces, grid, dt)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		stepFlock2D(agents, forces, grid, dt)
	}
}

func TestCorridorPathFollowingKeepsRadiusClear(t *testing.T) {
	// L-shaped corridor of width 60:
	// Leg 1: x in [0, 200], y in [0, 60]
	// Leg 2: x in [140, 200], y in [60, 220]
	// Outer perimeter walls + inner corner walls.
	walls := []ai.Wall{
		{A: pt(0, 0), B: pt(200, 0)},      // bottom wall of leg 1
		{A: pt(200, 0), B: pt(200, 220)},  // right outer wall (leg 1 + leg 2)
		{A: pt(0, 60), B: pt(140, 60)},    // top wall of leg 1
		{A: pt(140, 60), B: pt(140, 220)}, // left wall of leg 2
	}

	// Deliberately place a waypoint slightly off-center near the inner corner so
	// FollowPath alone would cut the corner and violate wall clearance without
	// WallAvoidance + CombinePrioritized.
	path := []core.Point{
		pt(80, 30),
		pt(160, 48),
		pt(155, 80),
		pt(170, 190),
	}

	agent := ai.Agent{
		Pos:      pt(20, 30),
		Vel:      pt(40, 0),
		MaxSpeed: 100,
		MaxForce: 450,
		Radius:   10,
	}
	var st ai.PathFollowState
	dt := time.Second / 60
	minClearance := math.Inf(1)

	for step := range 600 {
		wallForce := ai.WallAvoidance(agent, walls, 36)
		pathForce := ai.FollowPath(agent, path, &st, 16, false)
		force := ai.CombinePrioritized(agent.MaxForce,
			ai.WeightedForce{Force: wallForce, Weight: 1.0},
			ai.WeightedForce{Force: pathForce, Weight: 1.0},
		)
		agent.Step(force, dt)

		for wi, w := range walls {
			d := distPointToSegment(agent.Pos, w.A, w.B)
			if d < minClearance {
				minClearance = d
			}
			if d < float64(agent.Radius) {
				t.Fatalf("step %d: agent pos %v violated wall %d clearance: dist=%.4f < radius=%.4f",
					step, agent.Pos, wi, d, agent.Radius)
			}
		}
		if st.Done {
			break
		}
	}

	if !st.Done {
		t.Fatalf("agent did not finish corridor path: pos=%v state=%+v", agent.Pos, st)
	}
	if minClearance < float64(agent.Radius) {
		t.Fatalf("minimum corridor clearance %.4f < agent radius %.4f", minClearance, agent.Radius)
	}
}

func TestSteering3DBehaviorsTable(t *testing.T) {
	t.Run("Full3D Seek Arrive Flee Pursue Evade OffsetPursuit", func(t *testing.T) {
		a := ai.Agent3D{Pos: ai.Vec3{X: 0, Y: 0, Z: 0}, MaxSpeed: 100, MaxForce: 200, Radius: 5}

		fSeek := ai.Seek3D(a, ai.Vec3{X: 0, Y: 50, Z: -50})
		if fSeek.Y <= 0 || fSeek.Z >= 0 {
			t.Fatalf("Seek3D = %v, want +Y and -Z", fSeek)
		}

		fArrive := ai.Arrive3D(ai.Agent3D{Pos: ai.Vec3{X: 80, Y: 0, Z: 0}, MaxSpeed: 60, MaxForce: 200}, ai.Vec3{X: 100, Y: 0, Z: 0}, 40)
		if math.Abs(float64(fArrive.X)-30) > 1e-3 {
			t.Fatalf("Arrive3D half slowRadius = %v, want X=30", fArrive)
		}

		if z := ai.Flee3D(ai.Agent3D{Pos: ai.Vec3{X: 60}, MaxSpeed: 50, MaxForce: 100}, ai.Vec3{}, 50); z != (ai.Vec3{}) {
			t.Fatalf("Flee3D outside panicRadius = %v, want zero", z)
		}

		target := ai.Agent3D{Pos: ai.Vec3{X: 100, Y: 0, Z: 0}, Vel: ai.Vec3{X: 0, Y: 80, Z: 0}}
		fPursue := ai.Pursue3D(a, target, 0.5)
		if fPursue.X <= 0 || fPursue.Y <= 0 {
			t.Fatalf("Pursue3D = %v, want +X and +Y lead", fPursue)
		}

		pursuer := ai.Agent3D{Pos: ai.Vec3{X: 0, Y: 0, Z: 0}, Vel: ai.Vec3{X: 80, Y: 0, Z: 0}}
		evader := ai.Agent3D{Pos: ai.Vec3{X: 0, Y: 30, Z: 0}, MaxSpeed: 80, MaxForce: 200}
		fEvade := ai.Evade3D(evader, pursuer, 100, 0.5)
		if fEvade.X >= -1 || fEvade.Y <= 1 {
			t.Fatalf("Evade3D = %v, want -X and +Y", fEvade)
		}

		// OffsetPursuit3D: leader moving along -Z (default forward): +X is right, +Z is trailing behind.
		leader := ai.Agent3D{Pos: ai.Vec3{X: 0, Y: 0, Z: -20}, Vel: ai.Vec3{X: 0, Y: 0, Z: -20}}
		follower := ai.Agent3D{Pos: ai.Vec3{X: 0, Y: 0, Z: 0}, MaxSpeed: 100, MaxForce: 300}
		fOff := ai.OffsetPursuit3D(follower, leader, ai.Vec3{X: 10, Y: 5, Z: 10}, 5, 0.5)
		if fOff.X <= 0 || fOff.Y <= 0 || fOff.Z >= 0 {
			t.Fatalf("OffsetPursuit3D = %v, want +X, +Y, -Z", fOff)
		}
	})

	t.Run("Full3D Flocking ObstacleAvoidance Hide Interpose CombinePrioritized3D", func(t *testing.T) {
		a := ai.Agent3D{Pos: ai.Vec3{}, Vel: ai.Vec3{Z: -60}, MaxSpeed: 60, MaxForce: 150, Radius: 6}
		neighbors := []ai.Agent3D{
			a,
			{Pos: ai.Vec3{X: 4, Y: 0, Z: 0}, Vel: ai.Vec3{X: 60, Y: 0, Z: 0}, Radius: 6},
		}
		if fSep := ai.Separation3D(a, neighbors, 20); fSep.X >= 0 {
			t.Fatalf("Separation3D = %v, want negative X", fSep)
		}
		if fAlign := ai.Alignment3D(a, neighbors, 30); fAlign.X <= 0 {
			t.Fatalf("Alignment3D = %v, want positive X", fAlign)
		}
		if fCoh := ai.Cohesion3D(a, neighbors, 30); fCoh.X <= 0 {
			t.Fatalf("Cohesion3D = %v, want positive X", fCoh)
		}

		obs := []ai.Obstacle3D{{Center: ai.Vec3{X: 0, Y: 3, Z: -30}, Radius: 10}}
		fObs := ai.ObstacleAvoidance3D(a, obs, 60)
		// Obstacle slightly above (+Y) -> deflect downward (-Y).
		if fObs.Y >= -5 {
			t.Fatalf("ObstacleAvoidance3D = %v, want negative Y deflection", fObs)
		}

		pursuer := ai.Agent3D{Pos: ai.Vec3{X: 0, Y: 0, Z: 0}}
		hideObs := []ai.Obstacle3D{{Center: ai.Vec3{X: 0, Y: 0, Z: -40}, Radius: 10}}
		hider := ai.Agent3D{Pos: ai.Vec3{X: 10, Y: 0, Z: -50}, MaxSpeed: 80, MaxForce: 200, Radius: 4}
		fHide := ai.Hide3D(hider, pursuer, hideObs, 2, 10)
		// Hiding spot at (0, 0, -56) -> steer toward -X, -Z.
		if fHide.X >= 0 || fHide.Z >= 0 {
			t.Fatalf("Hide3D = %v, want -X and -Z", fHide)
		}

		inter := ai.Agent3D{Pos: ai.Vec3{X: 0, Y: 0, Z: 0}, MaxSpeed: 100, MaxForce: 200}
		fInt := ai.Interpose3D(
			inter,
			ai.Agent3D{Pos: ai.Vec3{X: 0, Y: 0, Z: -100}, Vel: ai.Vec3{X: 40}},
			ai.Agent3D{Pos: ai.Vec3{X: 0, Y: 20, Z: -100}, Vel: ai.Vec3{X: 40}},
			10,
		)
		if fInt.X <= 0 || fInt.Y <= 0 || fInt.Z >= 0 {
			t.Fatalf("Interpose3D = %v, want +X, +Y, -Z", fInt)
		}

		fComb := ai.CombinePrioritized3D(100,
			ai.WeightedForce3D{Force: ai.Vec3{X: 60}, Weight: 1},
			ai.WeightedForce3D{Force: ai.Vec3{Z: 80}, Weight: 1},
			ai.WeightedForce3D{Force: ai.Vec3{Y: 50}, Weight: 1},
		)
		if math.Abs(float64(fComb.X)-60) > 1e-3 || math.Abs(float64(fComb.Z)-40) > 1e-3 || math.Abs(float64(fComb.Y)) > 1e-3 {
			t.Fatalf("CombinePrioritized3D = %v, want (60, 0, 40)", fComb)
		}
	})

	t.Run("PlanarXZ zeroes Y force and preserves Pos.Y", func(t *testing.T) {
		a := ai.Agent3D{Pos: ai.Vec3{X: 0, Y: 12, Z: 0}, MaxSpeed: 80, MaxForce: 200, Radius: 5, PlanarXZ: true}
		f := ai.Seek3D(a, ai.Vec3{X: 50, Y: 99, Z: -50})
		if f.Y != 0 || f.X <= 0 || f.Z >= 0 {
			t.Fatalf("PlanarXZ Seek3D = %v, want Y=0, +X, -Z", f)
		}
		a.Step(ai.Vec3{X: f.X, Y: 999, Z: f.Z}, time.Second/60)
		if a.Pos.Y != 12 || a.Vel.Y != 0 {
			t.Fatalf("PlanarXZ Step modified Y: pos=%v vel=%v", a.Pos, a.Vel)
		}
	})
}

func TestSteering3DFollowsWorld3DNavMeshPath(t *testing.T) {
	// Build a real sloped 3D world3d.NavMesh: lower floor -> sloped ramp -> upper floor.
	nm := world3d.BuildNavMesh([][]math3d.Vec3{
		// Lower floor (y = 0), x in [0, 20], z in [0, 10]
		{{X: 0, Y: 0, Z: 0}, {X: 0, Y: 0, Z: 10}, {X: 20, Y: 0, Z: 10}, {X: 20, Y: 0, Z: 0}},
		// Sloped ramp (y from 0 to 4), x in [20, 40], z in [0, 10]
		{{X: 20, Y: 0, Z: 0}, {X: 20, Y: 0, Z: 10}, {X: 40, Y: 4, Z: 10}, {X: 40, Y: 4, Z: 0}},
		// Upper floor (y = 4), x in [40, 60], z in [0, 20]
		{{X: 40, Y: 4, Z: 0}, {X: 40, Y: 4, Z: 20}, {X: 60, Y: 4, Z: 20}, {X: 60, Y: 4, Z: 0}},
	}, -1)

	start := math3d.Vec3{X: 2, Y: 0, Z: 5}
	goal := math3d.Vec3{X: 55, Y: 4, Z: 16}
	rawPath, ok := nm.FindPath(start, goal)
	if !ok || len(rawPath) < 2 {
		t.Fatalf("world3d.NavMesh.FindPath failed: ok=%v path=%v", ok, rawPath)
	}

	// Zero-cost field-compatible conversion from math3d.Vec3 to ai.Vec3.
	path := make([]ai.Vec3, len(rawPath))
	for i, p := range rawPath {
		path[i] = ai.Vec3(p)
	}

	agent := ai.Agent3D{
		Pos:      ai.Vec3(start),
		MaxSpeed: 18,
		MaxForce: 60,
		Radius:   1.5,
		PlanarXZ: true,
	}
	var st ai.PathFollowState
	dt := time.Second / 60

	for range 600 {
		f := ai.FollowPath3D(agent, path, &st, 1.5, false)
		agent.Step(f, dt)
		// Snap grounded agent Y to the NavMesh polygon height underneath.
		if polyIdx, ok := nm.FindPolygon(math3d.Vec3(agent.Pos)); ok {
			agent.Pos.Y = nm.Polys[polyIdx].HeightAt(agent.Pos.X, agent.Pos.Z)
		}
		if st.Done {
			break
		}
	}

	if !st.Done {
		t.Fatalf("3D agent did not finish world3d.NavMesh path: pos=%v state=%+v", agent.Pos, st)
	}
	if d := v3DistXZ(agent.Pos, ai.Vec3(goal)); d > 2.0 {
		t.Fatalf("3D agent stopped too far from goal: pos=%v goal=%v distXZ=%.3f", agent.Pos, goal, d)
	}
	if math.Abs(float64(agent.Pos.Y-goal.Y)) > 0.25 {
		t.Fatalf("3D agent did not climb ramp to upper floor: y=%.3f want %.3f", agent.Pos.Y, goal.Y)
	}
}

func TestCorridor3DPathFollowingKeepsRadiusClear(t *testing.T) {
	walls := []ai.Wall3D{
		{A: ai.Vec3{X: 0, Z: 0}, B: ai.Vec3{X: 200, Z: 0}},
		{A: ai.Vec3{X: 200, Z: 0}, B: ai.Vec3{X: 200, Z: 200}},
		{A: ai.Vec3{X: 0, Z: 60}, B: ai.Vec3{X: 140, Z: 60}},
		{A: ai.Vec3{X: 140, Z: 60}, B: ai.Vec3{X: 140, Z: 200}},
	}
	path := []ai.Vec3{
		{X: 80, Z: 30},
		{X: 160, Z: 48},
		{X: 155, Z: 80},
		{X: 170, Z: 175},
	}
	agent := ai.Agent3D{
		Pos:      ai.Vec3{X: 20, Y: 2, Z: 30},
		Vel:      ai.Vec3{X: 40},
		MaxSpeed: 100,
		MaxForce: 450,
		Radius:   10,
		PlanarXZ: true,
	}
	var st ai.PathFollowState
	dt := time.Second / 60

	for step := range 600 {
		force := ai.CombinePrioritized3D(agent.MaxForce,
			ai.WeightedForce3D{Force: ai.WallAvoidance3D(agent, walls, 36), Weight: 1},
			ai.WeightedForce3D{Force: ai.FollowPath3D(agent, path, &st, 16, false), Weight: 1},
		)
		agent.Step(force, dt)
		for wi, w := range walls {
			d := distPointToSegment(
				core.Point{X: core.Points(agent.Pos.X), Y: core.Points(agent.Pos.Z)},
				core.Point{X: core.Points(w.A.X), Y: core.Points(w.A.Z)},
				core.Point{X: core.Points(w.B.X), Y: core.Points(w.B.Z)},
			)
			if d < float64(agent.Radius) {
				t.Fatalf("step %d: 3D agent %v violated wall %d: dist=%.4f < radius=%.4f",
					step, agent.Pos, wi, d, agent.Radius)
			}
		}
		if st.Done {
			break
		}
	}
	if !st.Done {
		t.Fatalf("3D agent did not finish corridor path: pos=%v state=%+v", agent.Pos, st)
	}
}

func makeFlock3D(n int, planarXZ bool) []ai.Agent3D {
	rng := rand.New(rand.NewPCG(839, 3030))
	agents := make([]ai.Agent3D, n)
	for i := range agents {
		ang := rng.Float64() * 2 * math.Pi
		y := float32(0)
		vy := float32(0)
		if !planarXZ {
			y = float32(rng.Float64()*200 - 100)
			vy = float32(rng.Float64()*40 - 20)
		}
		agents[i] = ai.Agent3D{
			Pos:      ai.Vec3{X: float32(rng.Float64()*400 - 200), Y: y, Z: float32(rng.Float64()*400 - 200)},
			Vel:      ai.Vec3{X: float32(math.Cos(ang) * 40), Y: vy, Z: float32(math.Sin(ang) * 40)},
			MaxSpeed: 80,
			MaxForce: 180,
			Radius:   6,
			PlanarXZ: planarXZ,
		}
	}
	return agents
}

func stepFlock3D(agents []ai.Agent3D, forces []ai.Vec3, grid *ai.NeighborGrid3D, dt time.Duration) {
	grid.Rebuild(agents)
	for i := range agents {
		neighbors := grid.Query(agents[i].Pos, 50)
		forces[i] = ai.CombinePrioritized3D(agents[i].MaxForce,
			ai.WeightedForce3D{Force: ai.Separation3D(agents[i], neighbors, 18), Weight: 1.6},
			ai.WeightedForce3D{Force: ai.Alignment3D(agents[i], neighbors, 45), Weight: 1.0},
			ai.WeightedForce3D{Force: ai.Cohesion3D(agents[i], neighbors, 50), Weight: 0.9},
		)
	}
	for i := range agents {
		agents[i].Step(forces[i], dt)
	}
}

func TestFlocking3D200AgentsZeroAllocAndDeterministic(t *testing.T) {
	const count = 200
	dt := time.Second / 60

	run := func() []ai.Agent3D {
		agents := makeFlock3D(count, true)
		forces := make([]ai.Vec3, count)
		grid := ai.NewNeighborGrid3D(50, true)
		for range 40 {
			stepFlock3D(agents, forces, grid, dt)
		}
		return agents
	}
	r1 := run()
	r2 := run()
	for i := range count {
		if r1[i] != r2[i] {
			t.Fatalf("3D agent %d diverged: %+v != %+v", i, r1[i], r2[i])
		}
	}

	agents := makeFlock3D(count, false)
	forces := make([]ai.Vec3, count)
	grid := ai.NewNeighborGrid3D(50, false)
	for range 5 {
		stepFlock3D(agents, forces, grid, dt)
	}
	allocs := testing.AllocsPerRun(50, func() {
		stepFlock3D(agents, forces, grid, dt)
	})
	if allocs != 0 {
		t.Fatalf("200-agent 3D flocking step allocated %.2f times/op, want 0", allocs)
	}
}

func BenchmarkFlocking200Agents3D(b *testing.B) {
	const count = 200
	dt := time.Second / 60
	agents := makeFlock3D(count, true)
	forces := make([]ai.Vec3, count)
	grid := ai.NewNeighborGrid3D(50, true)
	for range 5 {
		stepFlock3D(agents, forces, grid, dt)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		stepFlock3D(agents, forces, grid, dt)
	}
}
