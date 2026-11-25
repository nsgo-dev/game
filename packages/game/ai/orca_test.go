package ai_test

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math2d"
)

const orcaStep = time.Second / 60

func prefToward(pos, goal math2d.Vec2, maxSpeed, slowRadius float32) math2d.Vec2 {
	d := goal.Sub(pos)
	dist := d.Len()
	if dist < 1e-4 {
		return math2d.Vec2{}
	}
	speed := maxSpeed
	if slowRadius > 0 && dist < slowRadius {
		speed *= dist / slowRadius
	}
	return d.Scale(speed / dist)
}

func distPointToSeg2D(p, a, b math2d.Vec2) float64 {
	abx, aby := float64(b.X-a.X), float64(b.Y-a.Y)
	l2 := abx*abx + aby*aby
	if l2 <= 1e-18 {
		return math.Hypot(float64(p.X-a.X), float64(p.Y-a.Y))
	}
	t := (float64(p.X-a.X)*abx + float64(p.Y-a.Y)*aby) / l2
	t = min(max(t, 0), 1)
	cx := float64(a.X) + t*abx
	cy := float64(a.Y) + t*aby
	return math.Hypot(float64(p.X)-cx, float64(p.Y)-cy)
}

func TestORCAHeadOnPassWithoutOverlap(t *testing.T) {
	solver := ai.NewORCASolver(ai.ORCAConfig{TimeHorizon: 2.0})
	goals := []math2d.Vec2{
		{X: 10, Y: 0},
		{X: -10, Y: 0},
	}
	agents := []ai.ORCAAgent{
		{ID: 1, Pos: math2d.V2(-10, 0), Radius: 0.5, MaxSpeed: 4},
		{ID: 2, Pos: math2d.V2(10, 0), Radius: 0.5, MaxSpeed: 4},
	}

	minSep := math.Inf(1)
	maxLat := 0.0
	for step := range 600 {
		for i := range agents {
			agents[i].PrefVel = prefToward(agents[i].Pos, goals[i], agents[i].MaxSpeed, 0.5)
		}
		solver.Step(agents, nil, orcaStep)
		d := float64(agents[0].Pos.Dist(agents[1].Pos))
		if d < minSep {
			minSep = d
		}
		if d < float64(agents[0].Radius+agents[1].Radius)-1e-2 {
			t.Fatalf("step %d: head-on agents overlapped: dist=%.4f < %.4f (pos=%v, %v)",
				step, d, agents[0].Radius+agents[1].Radius, agents[0].Pos, agents[1].Pos)
		}
		if a := math.Abs(float64(agents[0].Pos.Y)); a > maxLat {
			maxLat = a
		}
		if agents[0].Pos.Dist(goals[0]) < 0.05 && agents[1].Pos.Dist(goals[1]) < 0.05 {
			break
		}
	}
	for i := range agents {
		if d := agents[i].Pos.Dist(goals[i]); d > 0.1 {
			t.Fatalf("agent %d did not reach goal: pos=%v goal=%v dist=%.3f", i, agents[i].Pos, goals[i], d)
		}
	}
	if maxLat < 0.3 {
		t.Fatalf("expected lateral pass-around deflection, got max |Y|=%.4f", maxLat)
	}
	t.Logf("head-on pass succeeded: minSep=%.4f (required >= 1.0), maxLat=%.4f", minSep, maxLat)
}

func TestORCACircleSwapWithoutOverlap(t *testing.T) {
	for _, count := range []int{16, 32} {
		t.Run(string(rune('0'+count/10))+string(rune('0'+count%10))+"_agents", func(t *testing.T) {
			solver := ai.NewORCASolver(ai.ORCAConfig{
				TimeHorizon:  2.5,
				MaxNeighbors: 16,
			})
			ringRadius := float64(count) * 0.65
			agents := make([]ai.ORCAAgent, count)
			goals := make([]math2d.Vec2, count)
			for i := range count {
				ang := float64(i) * (2 * math.Pi / float64(count))
				x := float32(ringRadius * math.Cos(ang))
				y := float32(ringRadius * math.Sin(ang))
				agents[i] = ai.ORCAAgent{
					ID:       uint64(i + 1),
					Pos:      math2d.V2(x, y),
					Radius:   0.5,
					MaxSpeed: 4.0,
				}
				goals[i] = math2d.V2(-x, -y)
			}

			minSep := math.Inf(1)
			const maxSteps = 60 * 30
			arrivedStep := -1
			for step := range maxSteps {
				allDone := true
				for i := range agents {
					agents[i].PrefVel = prefToward(agents[i].Pos, goals[i], agents[i].MaxSpeed, 0.5)
					if agents[i].Pos.Dist(goals[i]) > 0.15 {
						allDone = false
					}
				}
				if allDone {
					arrivedStep = step
					break
				}
				solver.Step(agents, nil, orcaStep)
				for i := range agents {
					if agents[i].Vel.Len() > agents[i].MaxSpeed+1e-3 {
						t.Fatalf("step %d: agent %d exceeded MaxSpeed: %.4f > %.4f", step, i, agents[i].Vel.Len(), agents[i].MaxSpeed)
					}
					for j := i + 1; j < len(agents); j++ {
						d := float64(agents[i].Pos.Dist(agents[j].Pos))
						if d < minSep {
							minSep = d
						}
						minAllowed := float64(agents[i].Radius+agents[j].Radius) - 0.03
						if d < minAllowed {
							t.Fatalf("step %d: agents %d and %d overlapped: dist=%.4f < %.4f", step, i, j, d, minAllowed)
						}
					}
				}
			}
			if arrivedStep < 0 {
				unfinished := 0
				for i := range agents {
					if agents[i].Pos.Dist(goals[i]) > 0.2 {
						unfinished++
					}
				}
				t.Fatalf("%d of %d agents failed to reach antipodal goals after %d steps", unfinished, count, maxSteps)
			}
			t.Logf("%d-agent circle swap completed in %d steps (minSep=%.4f)", count, arrivedStep, minSep)
		})
	}
}

func TestORCAStaticObstacleDoorwayNoClipping(t *testing.T) {
	// Test both orientations of each doorway wall segment to prove two-sided, winding-agnostic behavior.
	orientations := [][2]ai.ORCAObstacle{
		{
			{A: math2d.V2(-12, 0), B: math2d.V2(-1.4, 0)},
			{A: math2d.V2(1.4, 0), B: math2d.V2(12, 0)},
		},
		{
			{A: math2d.V2(-1.4, 0), B: math2d.V2(-12, 0)},
			{A: math2d.V2(12, 0), B: math2d.V2(1.4, 0)},
		},
	}

	for oi, obstacles := range orientations {
		solver := ai.NewORCASolver(ai.ORCAConfig{
			TimeHorizon:     2.0,
			TimeHorizonObst: 1.5,
		})
		// Four agents walking from above the doorway (y > 0) through the gap at x in [-1.4, 1.4] to y < 0.
		agents := []ai.ORCAAgent{
			{ID: 1, Pos: math2d.V2(-3.0, 5.0), Radius: 0.45, MaxSpeed: 4.0},
			{ID: 2, Pos: math2d.V2(3.0, 5.0), Radius: 0.45, MaxSpeed: 4.0},
			{ID: 3, Pos: math2d.V2(-1.0, 7.0), Radius: 0.45, MaxSpeed: 4.0},
			{ID: 4, Pos: math2d.V2(1.0, 7.0), Radius: 0.45, MaxSpeed: 4.0},
		}
		goals := []math2d.Vec2{
			math2d.V2(-2.0, -5.0),
			math2d.V2(2.0, -5.0),
			math2d.V2(-0.8, -6.5),
			math2d.V2(0.8, -6.5),
		}

		for step := range 60 * 15 {
			done := true
			for i := range agents {
				// Aim through doorway center until cleared, then direct to final goal.
				target := goals[i]
				if agents[i].Pos.Y > 0.3 {
					target = math2d.V2(0, -1.0)
				}
				agents[i].PrefVel = prefToward(agents[i].Pos, target, agents[i].MaxSpeed, 0.4)
				if agents[i].Pos.Dist(goals[i]) > 0.25 {
					done = false
				}
			}
			if done {
				break
			}
			solver.Step(agents, obstacles[:], orcaStep)
			for i, a := range agents {
				for _, obs := range obstacles {
					d := distPointToSeg2D(a.Pos, obs.A, obs.B)
					if d < float64(a.Radius)-0.03 {
						t.Fatalf("orientation %d step %d: agent %d clipped wall: dist=%.4f < radius=%.4f at %v",
							oi, step, i, d, a.Radius, a.Pos)
					}
				}
			}
		}
		for i := range agents {
			if d := agents[i].Pos.Dist(goals[i]); d > 0.35 {
				t.Fatalf("orientation %d: agent %d stalled at %v (dist %.3f from goal %v)", oi, i, agents[i].Pos, d, goals[i])
			}
		}
	}
}

func TestORCALayersAndMasks(t *testing.T) {
	const (
		layerGround uint32 = 1 << 0
		layerAir    uint32 = 1 << 1
	)

	t.Run("disjoint layers ignore each other", func(t *testing.T) {
		solver := ai.NewORCASolver(ai.ORCAConfig{TimeHorizon: 2.0})
		agents := []ai.ORCAAgent{
			{ID: 1, Pos: math2d.V2(-2, 0), Vel: math2d.V2(4, 0), PrefVel: math2d.V2(4, 0), Radius: 0.5, MaxSpeed: 4, Layer: layerGround, Mask: layerGround},
			{ID: 2, Pos: math2d.V2(2, 0), Vel: math2d.V2(-4, 0), PrefVel: math2d.V2(-4, 0), Radius: 0.5, MaxSpeed: 4, Layer: layerAir, Mask: layerAir},
		}
		vels := solver.Solve(nil, agents, nil, orcaStep)
		if vels[0] != agents[0].PrefVel || vels[1] != agents[1].PrefVel {
			t.Fatalf("disjoint layers deflected: got %v, want %v and %v", vels, agents[0].PrefVel, agents[1].PrefVel)
		}
	})

	t.Run("asymmetric mask assigns full avoidance to seeing agent", func(t *testing.T) {
		solver := ai.NewORCASolver(ai.ORCAConfig{TimeHorizon: 2.0})
		// Ground avoids Air (mask includes layerAir), Air ignores Ground (mask is layerAir only).
		agents := []ai.ORCAAgent{
			{ID: 1, Pos: math2d.V2(-6, 0), Radius: 0.5, MaxSpeed: 4, Layer: layerGround, Mask: layerGround | layerAir},
			{ID: 2, Pos: math2d.V2(6, 0), Radius: 0.5, MaxSpeed: 4, Layer: layerAir, Mask: layerAir},
		}
		goals := []math2d.Vec2{math2d.V2(6, 0), math2d.V2(-6, 0)}

		for step := range 400 {
			for i := range agents {
				agents[i].PrefVel = prefToward(agents[i].Pos, goals[i], agents[i].MaxSpeed, 0.3)
			}
			solver.Step(agents, nil, orcaStep)
			// Air unit should travel strictly straight along Y == 0.
			if math.Abs(float64(agents[1].Pos.Y)) > 1e-4 {
				t.Fatalf("step %d: ignoring air agent deflected laterally to Y=%v", step, agents[1].Pos.Y)
			}
			// Ground unit must keep completely clear of Air unit without Air helping.
			if d := agents[0].Pos.Dist(agents[1].Pos); d < agents[0].Radius+agents[1].Radius-0.02 {
				t.Fatalf("step %d: ground unit failed to avoid non-cooperative air unit: dist=%.4f", step, d)
			}
		}
		if agents[0].Pos.Dist(goals[0]) > 0.2 || agents[1].Pos.Dist(goals[1]) > 0.2 {
			t.Fatalf("agents did not reach goals: %v, %v", agents[0].Pos, agents[1].Pos)
		}
	})

	t.Run("obstacle layer filtering", func(t *testing.T) {
		solver := ai.NewORCASolver(ai.ORCAConfig{TimeHorizonObst: 1.5})
		groundWall := []ai.ORCAObstacle{
			{A: math2d.V2(0, -5), B: math2d.V2(0, 5), Layer: layerGround},
		}
		ground := ai.ORCAAgent{Pos: math2d.V2(-1, 0), Vel: math2d.V2(4, 0), PrefVel: math2d.V2(4, 0), Radius: 0.5, MaxSpeed: 4, Layer: layerGround, Mask: layerGround}
		air := ai.ORCAAgent{Pos: math2d.V2(-1, 0), Vel: math2d.V2(4, 0), PrefVel: math2d.V2(4, 0), Radius: 0.5, MaxSpeed: 4, Layer: layerAir, Mask: layerAir}

		vGround := solver.SolveAgent(ground, nil, groundWall, orcaStep)
		vAir := solver.SolveAgent(air, nil, groundWall, orcaStep)
		if vGround.X >= 3.5 {
			t.Fatalf("ground agent ignored ground wall: v=%v", vGround)
		}
		if vAir != air.PrefVel {
			t.Fatalf("air agent was blocked by ground-only wall: v=%v", vAir)
		}
	})
}

func TestORCAPriorityAndStationaryHolding(t *testing.T) {
	t.Run("stationary agent holds ground while moving agent steers around", func(t *testing.T) {
		solver := ai.NewORCASolver(ai.ORCAConfig{TimeHorizon: 2.0})
		blockerPos := math2d.V2(0, 0)
		agents := []ai.ORCAAgent{
			{ID: 1, Pos: blockerPos, Radius: 0.6, MaxSpeed: 0, Stationary: true},
			{ID: 2, Pos: math2d.V2(-6, 0), Radius: 0.5, MaxSpeed: 4},
		}
		goal := math2d.V2(6, 0)

		for step := range 400 {
			agents[1].PrefVel = prefToward(agents[1].Pos, goal, agents[1].MaxSpeed, 0.3)
			solver.Step(agents, nil, orcaStep)
			if agents[0].Pos != blockerPos || agents[0].Vel != (math2d.Vec2{}) {
				t.Fatalf("step %d: stationary agent moved: pos=%v vel=%v", step, agents[0].Pos, agents[0].Vel)
			}
			if d := agents[0].Pos.Dist(agents[1].Pos); d < agents[0].Radius+agents[1].Radius-0.02 {
				t.Fatalf("step %d: moving agent clipped stationary agent: dist=%.4f", step, d)
			}
		}
		if d := agents[1].Pos.Dist(goal); d > 0.2 {
			t.Fatalf("moving agent failed to pass stationary agent and reach goal: pos=%v dist=%.3f", agents[1].Pos, d)
		}
	})

	t.Run("high priority yields less than low priority", func(t *testing.T) {
		solver := ai.NewORCASolver(ai.ORCAConfig{TimeHorizon: 2.0})
		agents := []ai.ORCAAgent{
			{ID: 1, Pos: math2d.V2(-6, 0), Radius: 0.5, MaxSpeed: 4, Priority: 10},
			{ID: 2, Pos: math2d.V2(6, 0), Radius: 0.5, MaxSpeed: 4, Priority: 1},
		}
		goals := []math2d.Vec2{math2d.V2(6, 0), math2d.V2(-6, 0)}
		maxLatHigh, maxLatLow := 0.0, 0.0

		for step := range 400 {
			for i := range agents {
				agents[i].PrefVel = prefToward(agents[i].Pos, goals[i], agents[i].MaxSpeed, 0.3)
			}
			solver.Step(agents, nil, orcaStep)
			if d := agents[0].Pos.Dist(agents[1].Pos); d < agents[0].Radius+agents[1].Radius-0.02 {
				t.Fatalf("step %d: priority agents overlapped: dist=%.4f", step, d)
			}
			maxLatHigh = max(maxLatHigh, math.Abs(float64(agents[0].Pos.Y)))
			maxLatLow = max(maxLatLow, math.Abs(float64(agents[1].Pos.Y)))
		}
		if maxLatHigh >= maxLatLow*0.35 {
			t.Fatalf("expected high-priority agent to yield much less: high=%.4f low=%.4f", maxLatHigh, maxLatLow)
		}
		if agents[0].Pos.Dist(goals[0]) > 0.2 || agents[1].Pos.Dist(goals[1]) > 0.2 {
			t.Fatalf("agents did not reach goals: %v, %v", agents[0].Pos, agents[1].Pos)
		}
	})

	t.Run("priority 1 holding vs priority 0 moving holds ground", func(t *testing.T) {
		solver := ai.NewORCASolver(ai.ORCAConfig{TimeHorizon: 2.0})
		holdPos := math2d.V2(0, 0)
		agents := []ai.ORCAAgent{
			{ID: 1, Pos: holdPos, Radius: 0.6, MaxSpeed: 4, Priority: 1},
			{ID: 2, Pos: math2d.V2(-6, 0), Radius: 0.5, MaxSpeed: 4, Priority: 0},
		}
		goal := math2d.V2(6, 0)
		for step := range 400 {
			agents[0].PrefVel = math2d.Vec2{}
			agents[1].PrefVel = prefToward(agents[1].Pos, goal, agents[1].MaxSpeed, 0.3)
			solver.Step(agents, nil, orcaStep)
			if agents[0].Pos.Dist(holdPos) > 1e-4 {
				t.Fatalf("step %d: holding unit (priority 1 vs 0) yielded to %v", step, agents[0].Pos)
			}
			if d := agents[0].Pos.Dist(agents[1].Pos); d < agents[0].Radius+agents[1].Radius-0.02 {
				t.Fatalf("step %d: moving unit overlapped holding unit: dist=%.4f", step, d)
			}
		}
		if d := agents[1].Pos.Dist(goal); d > 0.2 {
			t.Fatalf("moving unit failed to steer around holding unit: pos=%v", agents[1].Pos)
		}
	})
}

func TestORCA3DXZPlane(t *testing.T) {
	solver := ai.NewORCASolver(ai.ORCAConfig{TimeHorizon: 2.5})
	const count = 16
	const ringRadius = 10.0
	agents := make([]ai.ORCAAgent3D, count)
	goals := make([]ai.Vec3, count)
	for i := range count {
		ang := float64(i) * (2 * math.Pi / float64(count))
		x := float32(ringRadius * math.Cos(ang))
		z := float32(ringRadius * math.Sin(ang))
		y := float32(i) * 0.25 // distinct elevation per agent; StepXZ must preserve Pos.Y
		agents[i] = ai.ORCAAgent3D{
			ID:       uint64(i + 1),
			Pos:      ai.Vec3{X: x, Y: y, Z: z},
			Radius:   0.5,
			MaxSpeed: 4.0,
		}
		goals[i] = ai.Vec3{X: -x, Y: y, Z: -z}
	}

	for step := range 60 * 20 {
		done := true
		for i := range agents {
			dx := goals[i].X - agents[i].Pos.X
			dz := goals[i].Z - agents[i].Pos.Z
			d := float32(math.Hypot(float64(dx), float64(dz)))
			if d > 0.15 {
				done = false
			}
			if d > 1e-4 {
				sp := agents[i].MaxSpeed
				if d < 0.5 {
					sp *= d / 0.5
				}
				agents[i].PrefVel = ai.Vec3{X: dx * sp / d, Y: 0, Z: dz * sp / d}
			} else {
				agents[i].PrefVel = ai.Vec3{}
			}
		}
		if done {
			break
		}
		solver.StepXZ(agents, nil, orcaStep)
		for i := range agents {
			if agents[i].Vel.Y != 0 {
				t.Fatalf("step %d: agent %d Vel.Y = %v, want 0", step, i, agents[i].Vel.Y)
			}
			wantY := float32(i) * 0.25
			if agents[i].Pos.Y != wantY {
				t.Fatalf("step %d: agent %d Pos.Y = %v, want preserved %v", step, i, agents[i].Pos.Y, wantY)
			}
			for j := i + 1; j < len(agents); j++ {
				dx := float64(agents[i].Pos.X - agents[j].Pos.X)
				dz := float64(agents[i].Pos.Z - agents[j].Pos.Z)
				if d := math.Hypot(dx, dz); d < float64(agents[i].Radius+agents[j].Radius)-0.03 {
					t.Fatalf("step %d: XZ agents %d and %d overlapped: dist=%.4f", step, i, j, d)
				}
			}
		}
	}
	for i := range agents {
		dx := float64(goals[i].X - agents[i].Pos.X)
		dz := float64(goals[i].Z - agents[i].Pos.Z)
		if d := math.Hypot(dx, dz); d > 0.2 {
			t.Fatalf("XZ agent %d did not reach antipodal goal: dist=%.3f", i, d)
		}
	}
}

func TestORCAInfeasibleLP3Fallback(t *testing.T) {
	solver := ai.NewORCASolver(ai.ORCAConfig{TimeHorizon: 1.5, TimeHorizonObst: 1.0})
	// Box an agent in on all sides with incoming neighbors AND a hard wall on the left (x = -1).
	wall := []ai.ORCAObstacle{{A: math2d.V2(-1, -5), B: math2d.V2(-1, 5)}}
	agent := ai.ORCAAgent{
		ID: 1, Pos: math2d.V2(-0.4, 0), Vel: math2d.V2(0, 0), PrefVel: math2d.V2(-3, 0),
		Radius: 0.5, MaxSpeed: 4,
	}
	neighbors := []ai.ORCAAgent{
		{ID: 2, Pos: math2d.V2(0.7, 0), Vel: math2d.V2(-4, 0), Radius: 0.5, MaxSpeed: 4},
		{ID: 3, Pos: math2d.V2(-0.4, 1.1), Vel: math2d.V2(0, -4), Radius: 0.5, MaxSpeed: 4},
		{ID: 4, Pos: math2d.V2(-0.4, -1.1), Vel: math2d.V2(0, 4), Radius: 0.5, MaxSpeed: 4},
		{ID: 5, Pos: math2d.V2(0.5, 0.8), Vel: math2d.V2(-3, -3), Radius: 0.5, MaxSpeed: 4},
		{ID: 6, Pos: math2d.V2(0.5, -0.8), Vel: math2d.V2(-3, 3), Radius: 0.5, MaxSpeed: 4},
	}

	v := solver.SolveAgent(agent, neighbors, wall, orcaStep)
	if math.IsNaN(float64(v.X)) || math.IsNaN(float64(v.Y)) || math.IsInf(float64(v.X), 0) || math.IsInf(float64(v.Y), 0) {
		t.Fatalf("LP3 fallback produced non-finite velocity: %v", v)
	}
	if v.Len() > agent.MaxSpeed+1e-3 {
		t.Fatalf("LP3 fallback exceeded MaxSpeed: %.4f > %.4f", v.Len(), agent.MaxSpeed)
	}
	// Hard wall constraint must keep the agent from moving deeper into the wall (x = -1).
	if v.X < -0.5 {
		t.Fatalf("LP3 fallback violated hard obstacle wall constraint: v=%v", v)
	}
}

func TestORCADeterminismAndOrderIndependence(t *testing.T) {
	t.Run("shuffling neighbors and obstacles gives bit-identical velocity", func(t *testing.T) {
		solver := ai.NewORCASolver(ai.ORCAConfig{TimeHorizon: 2.0, MaxNeighbors: 10})
		rng := rand.New(rand.NewPCG(42, 99))

		agent := ai.ORCAAgent{
			ID: 100, Pos: math2d.V2(0, 0), Vel: math2d.V2(1.5, -0.5),
			PrefVel: math2d.V2(3.0, 1.0), Radius: 0.5, MaxSpeed: 4.0,
		}
		neighbors := make([]ai.ORCAAgent, 30)
		for i := range neighbors {
			neighbors[i] = ai.ORCAAgent{
				ID:       uint64(i + 1),
				Pos:      math2d.V2(float32(rng.Float64()*8-4), float32(rng.Float64()*8-4)),
				Vel:      math2d.V2(float32(rng.Float64()*6-3), float32(rng.Float64()*6-3)),
				Radius:   0.4 + float32(i%3)*0.1,
				MaxSpeed: 4.0,
				Priority: float32(i % 4),
			}
		}
		obstacles := []ai.ORCAObstacle{
			{A: math2d.V2(-3, -3), B: math2d.V2(-3, 3)},
			{A: math2d.V2(3, -3), B: math2d.V2(3, 3)},
			{A: math2d.V2(-3, 3), B: math2d.V2(3, 3)},
		}

		ref := solver.SolveAgent(agent, neighbors, obstacles, orcaStep)
		for trial := range 25 {
			nShuf := append([]ai.ORCAAgent(nil), neighbors...)
			rng.Shuffle(len(nShuf), func(i, j int) { nShuf[i], nShuf[j] = nShuf[j], nShuf[i] })
			oShuf := append([]ai.ORCAObstacle(nil), obstacles...)
			rng.Shuffle(len(oShuf), func(i, j int) { oShuf[i], oShuf[j] = oShuf[j], oShuf[i] })
			// Also flip obstacle endpoints randomly.
			for k := range oShuf {
				if rng.IntN(2) == 1 {
					oShuf[k].A, oShuf[k].B = oShuf[k].B, oShuf[k].A
				}
			}
			got := solver.SolveAgent(agent, nShuf, oShuf, orcaStep)
			if math.Float32bits(got.X) != math.Float32bits(ref.X) || math.Float32bits(got.Y) != math.Float32bits(ref.Y) {
				t.Fatalf("trial %d: shuffled SolveAgent = %v, want bit-identical %v", trial, got, ref)
			}
		}
	})

	t.Run("shuffling batch agents gives bit-identical velocities per ID", func(t *testing.T) {
		solver := ai.NewORCASolver(ai.ORCAConfig{TimeHorizon: 2.0, MaxNeighbors: 12})
		rng := rand.New(rand.NewPCG(7, 13))

		const n = 40
		base := make([]ai.ORCAAgent, n)
		for i := range n {
			base[i] = ai.ORCAAgent{
				ID:       uint64(i + 1),
				Pos:      math2d.V2(float32(rng.Float64()*12-6), float32(rng.Float64()*12-6)),
				Vel:      math2d.V2(float32(rng.Float64()*6-3), float32(rng.Float64()*6-3)),
				PrefVel:  math2d.V2(float32(rng.Float64()*6-3), float32(rng.Float64()*6-3)),
				Radius:   0.45,
				MaxSpeed: 4.0,
				Priority: float32(i % 3),
			}
		}
		refVels := solver.Solve(nil, base, nil, orcaStep)
		refByID := make(map[uint64]math2d.Vec2, n)
		for i, a := range base {
			refByID[a.ID] = refVels[i]
		}

		for trial := range 10 {
			perm := append([]ai.ORCAAgent(nil), base...)
			rng.Shuffle(len(perm), func(i, j int) { perm[i], perm[j] = perm[j], perm[i] })
			gotVels := solver.Solve(nil, perm, nil, orcaStep)
			for i, a := range perm {
				ref := refByID[a.ID]
				got := gotVels[i]
				if math.Float32bits(got.X) != math.Float32bits(ref.X) || math.Float32bits(got.Y) != math.Float32bits(ref.Y) {
					t.Fatalf("trial %d agent ID %d: got %v, want bit-identical %v", trial, a.ID, got, ref)
				}
			}
		}
	})

	t.Run("replay produces identical positions every step", func(t *testing.T) {
		run := func() [][]math2d.Vec2 {
			solver := ai.NewORCASolver(ai.ORCAConfig{TimeHorizon: 2.0})
			agents := make([]ai.ORCAAgent, 20)
			goals := make([]math2d.Vec2, 20)
			for i := range agents {
				ang := float64(i) * (2 * math.Pi / 20)
				x := float32(10 * math.Cos(ang))
				y := float32(10 * math.Sin(ang))
				agents[i] = ai.ORCAAgent{ID: uint64(i + 1), Pos: math2d.V2(x, y), Radius: 0.5, MaxSpeed: 4}
				goals[i] = math2d.V2(-x, -y)
			}
			trace := make([][]math2d.Vec2, 120)
			for step := range trace {
				for i := range agents {
					agents[i].PrefVel = prefToward(agents[i].Pos, goals[i], agents[i].MaxSpeed, 0.5)
				}
				solver.Step(agents, nil, orcaStep)
				snap := make([]math2d.Vec2, len(agents))
				for i := range agents {
					snap[i] = agents[i].Pos
				}
				trace[step] = snap
			}
			return trace
		}
		a, b := run(), run()
		for step := range a {
			for i := range a[step] {
				if math.Float32bits(a[step][i].X) != math.Float32bits(b[step][i].X) ||
					math.Float32bits(a[step][i].Y) != math.Float32bits(b[step][i].Y) {
					t.Fatalf("step %d agent %d diverged: %v vs %v", step, i, a[step][i], b[step][i])
				}
			}
		}
	})
}

func TestORCAZeroAllocationsAfterWarmup(t *testing.T) {
	solver := ai.NewORCASolver(ai.ORCAConfig{
		TimeHorizon:  2.0,
		NeighborDist: 8.0,
		MaxNeighbors: 16,
	})
	agents := make([]ai.ORCAAgent, 100)
	for i := range agents {
		agents[i] = ai.ORCAAgent{
			ID:       uint64(i + 1),
			Pos:      math2d.V2(float32(i%10)*2, float32(i/10)*2),
			Vel:      math2d.V2(1, 0),
			PrefVel:  math2d.V2(2, 0),
			Radius:   0.45,
			MaxSpeed: 4.0,
		}
	}
	obstacles := []ai.ORCAObstacle{
		{A: math2d.V2(-2, -2), B: math2d.V2(22, -2)},
		{A: math2d.V2(-2, 22), B: math2d.V2(22, 22)},
	}
	out := make([]math2d.Vec2, len(agents))
	// Warm up internal scratch buffers.
	out = solver.Solve(out, agents, obstacles, orcaStep)

	if allocs := testing.AllocsPerRun(20, func() {
		out = solver.Solve(out, agents, obstacles, orcaStep)
	}); allocs != 0 {
		t.Fatalf("Solve allocated %v times after warm-up", allocs)
	}

	if allocs := testing.AllocsPerRun(20, func() {
		_ = solver.SolveAgent(agents[0], agents[1:20], obstacles, orcaStep)
	}); allocs != 0 {
		t.Fatalf("SolveAgent allocated %v times after warm-up", allocs)
	}

	agents3D := make([]ai.ORCAAgent3D, 50)
	for i := range agents3D {
		agents3D[i] = ai.ORCAAgent3D{
			ID:       uint64(i + 1),
			Pos:      ai.Vec3{X: float32(i%10) * 2, Y: 1, Z: float32(i/10) * 2},
			PrefVel:  ai.Vec3{X: 2, Z: 0},
			Radius:   0.45,
			MaxSpeed: 4.0,
		}
	}
	out3D := make([]ai.Vec3, len(agents3D))
	out3D = solver.SolveXZ(out3D, agents3D, nil, orcaStep)
	if allocs := testing.AllocsPerRun(20, func() {
		out3D = solver.SolveXZ(out3D, agents3D, nil, orcaStep)
	}); allocs != 0 {
		t.Fatalf("SolveXZ allocated %v times after warm-up", allocs)
	}
}

func TestORCAEdgeCases(t *testing.T) {
	// Zero-value solver works safely.
	var zero ai.ORCASolver
	if cfg := zero.Config(); cfg.TimeHorizon != ai.DefaultORCATimeHorizon || cfg.MaxNeighbors != ai.DefaultORCAMaxNeighbors {
		t.Fatalf("zero Config = %+v", cfg)
	}

	// Non-finite config values sanitize cleanly.
	nan, inf := float32(math.NaN()), float32(math.Inf(1))
	s := ai.NewORCASolver(ai.ORCAConfig{
		TimeHorizon:     nan,
		TimeHorizonObst: inf,
		NeighborDist:    nan,
		MaxNeighbors:    -5,
		CellSize:        inf,
	})
	if cfg := s.Config(); cfg.TimeHorizon != ai.DefaultORCATimeHorizon || cfg.TimeHorizonObst != ai.DefaultORCATimeHorizonObst || cfg.MaxNeighbors != ai.DefaultORCAMaxNeighbors {
		t.Fatalf("sanitized Config = %+v", cfg)
	}

	// Coincident agents separate cleanly without NaN.
	stacked := []ai.ORCAAgent{
		{ID: 1, Pos: math2d.V2(5, 5), Radius: 0.5, MaxSpeed: 4},
		{ID: 2, Pos: math2d.V2(5, 5), Radius: 0.5, MaxSpeed: 4},
		{ID: 3, Pos: math2d.V2(5, 5), Radius: 0.5, MaxSpeed: 4},
	}
	for range 120 {
		s.Step(stacked, nil, orcaStep)
	}
	for i := range stacked {
		for j := i + 1; j < len(stacked); j++ {
			if d := stacked[i].Pos.Dist(stacked[j].Pos); d < 0.8 || math.IsNaN(float64(d)) {
				t.Fatalf("stacked agents %d and %d did not separate: dist=%v", i, j, d)
			}
		}
	}

	// Zero dt leaves positions unchanged and returns zero velocities.
	vels := s.Solve(nil, stacked, nil, 0)
	for i, v := range vels {
		if v != (math2d.Vec2{}) {
			t.Fatalf("dt=0 returned non-zero vel[%d]=%v", i, v)
		}
	}

	// Degenerate zero-length obstacle acts as a static post without NaN.
	post := []ai.ORCAObstacle{{A: math2d.V2(2, 0), B: math2d.V2(2, 0)}}
	vPost := s.SolveAgent(ai.ORCAAgent{Pos: math2d.V2(0, 0), Vel: math2d.V2(4, 0), PrefVel: math2d.V2(4, 0), Radius: 0.5, MaxSpeed: 4}, nil, post, orcaStep)
	if math.IsNaN(float64(vPost.X)) || math.IsNaN(float64(vPost.Y)) {
		t.Fatalf("degenerate obstacle produced NaN: %v", vPost)
	}
}

func make1000Agents() ([]ai.ORCAAgent, []math2d.Vec2) {
	const n = 1000
	agents := make([]ai.ORCAAgent, n)
	goals := make([]math2d.Vec2, n)
	// Two dense 500-agent blocks marching past each other across a 40x40 arena.
	for i := range n {
		col := i % 25
		row := (i % 500) / 25
		var pos, goal math2d.Vec2
		if i < 500 {
			pos = math2d.V2(float32(col)*1.4, float32(row)*1.4)
			goal = math2d.V2(float32(col)*1.4+36, float32(row)*1.4)
		} else {
			pos = math2d.V2(float32(col)*1.4+36, float32(row)*1.4+0.35)
			goal = math2d.V2(float32(col)*1.4, float32(row)*1.4+0.35)
		}
		agents[i] = ai.ORCAAgent{
			ID:       uint64(i + 1),
			Pos:      pos,
			Radius:   0.4,
			MaxSpeed: 5.0,
		}
		goals[i] = goal
	}
	return agents, goals
}

func BenchmarkORCA1000Agents(b *testing.B) {
	solver := ai.NewORCASolver(ai.ORCAConfig{
		TimeHorizon:  2.0,
		NeighborDist: 6.0,
		MaxNeighbors: 16,
	})
	agents, goals := make1000Agents()
	// Advance until the two groups are actively interweaving in the center.
	for range 120 {
		for i := range agents {
			agents[i].PrefVel = prefToward(agents[i].Pos, goals[i], agents[i].MaxSpeed, 0.5)
		}
		solver.Step(agents, nil, orcaStep)
	}
	out := make([]math2d.Vec2, len(agents))
	out = solver.Solve(out, agents, nil, orcaStep)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		out = solver.Solve(out, agents, nil, orcaStep)
	}
}

func BenchmarkORCA1000AgentsXZ(b *testing.B) {
	solver := ai.NewORCASolver(ai.ORCAConfig{
		TimeHorizon:  2.0,
		NeighborDist: 6.0,
		MaxNeighbors: 16,
	})
	agents2D, goals2D := make1000Agents()
	agents := make([]ai.ORCAAgent3D, len(agents2D))
	for i, a := range agents2D {
		agents[i] = ai.ORCAAgent3D{
			ID:       a.ID,
			Pos:      ai.Vec3{X: a.Pos.X, Z: a.Pos.Y},
			Radius:   a.Radius,
			MaxSpeed: a.MaxSpeed,
		}
	}
	for range 120 {
		for i := range agents {
			p := math2d.V2(agents[i].Pos.X, agents[i].Pos.Z)
			pv := prefToward(p, goals2D[i], agents[i].MaxSpeed, 0.5)
			agents[i].PrefVel = ai.Vec3{X: pv.X, Z: pv.Y}
		}
		solver.StepXZ(agents, nil, orcaStep)
	}
	out := make([]ai.Vec3, len(agents))
	out = solver.SolveXZ(out, agents, nil, orcaStep)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		out = solver.SolveXZ(out, agents, nil, orcaStep)
	}
}
