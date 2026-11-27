package world3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/world3d"
)

func buildMultiLevelBridgeNavMesh() *world3d.NavMesh {
	ground := quadXZ(0, -2, 12, 2, 0)        // 0: ground under bridge
	eastLanding := quadXZ(12, -2, 16, 2, 0)  // 1: east ground landing
	northApproach := quadXZ(12, 2, 16, 6, 0) // 2: ramp base
	ramp := []math3d.Vec3{                   // 3: sloped ramp (Y: 0 -> 3)
		{X: 6, Y: 3, Z: 2},
		{X: 6, Y: 3, Z: 6},
		{X: 12, Y: 0, Z: 6},
		{X: 12, Y: 0, Z: 2},
	}
	upperLanding := quadXZ(2, 2, 6, 6, 3) // 4: ramp top landing
	bridgeDeck := quadXZ(2, -2, 10, 2, 3) // 5: upper bridge directly above poly 0
	island := quadXZ(20, -2, 24, 2, 3)    // 6: disconnected floating island

	nm := world3d.BuildNavMesh([][]math3d.Vec3{
		ground,
		eastLanding,
		northApproach,
		ramp,
		upperLanding,
		bridgeDeck,
		island,
	}, math3d.DegToRad(45))
	nm.VerticalTolerance = 1.2
	return nm
}

func TestNavMesh3DNearestPolygonAndFindCorridor(t *testing.T) {
	nm := buildMultiLevelBridgeNavMesh()

	idx, clamped, ok := nm.NearestPolygon(math3d.Vec3{X: 18, Y: 0, Z: 0})
	if !ok || idx != 1 {
		t.Fatalf("NearestPolygon(18,0,0) = (%d, %v, %v), want poly 1", idx, clamped, ok)
	}
	if !clamped.ApproxEqual(math3d.Vec3{X: 16, Y: 0, Z: 0}, 1e-3) {
		t.Fatalf("clamped = %v, want (16,0,0)", clamped)
	}

	corridor, ok := nm.FindCorridor(math3d.Vec3{X: 6, Y: 0, Z: 0}, math3d.Vec3{X: 6, Y: 3, Z: 0})
	if !ok || len(corridor) != 6 || corridor[0] != 0 || corridor[len(corridor)-1] != 5 {
		t.Fatalf("FindCorridor = (%v, %v), want 6-poly corridor from 0 to 5", corridor, ok)
	}
}

func TestNavAgent3DClimbsRampAndMultiLevelBridge(t *testing.T) {
	nm := buildMultiLevelBridgeNavMesh()

	// Start directly underneath the bridge at Y=0 and target the deck directly overhead at Y=3.
	agent := world3d.NewNavAgent(nm, math3d.Vec3{X: 6, Y: 0, Z: 0}, world3d.NavAgentConfig{
		MaxSpeed:         6,
		MaxAcceleration:  30,
		Radius:           0.35,
		ArrivalRadius:    1.5,
		StoppingDistance: 0.3,
	})
	if agent.State() != world3d.NavAgentIdle || agent.State().String() != "idle" {
		t.Fatalf("initial state = %v, want idle", agent.State())
	}

	goal := math3d.Vec3{X: 6, Y: 3, Z: 0}
	agent.SetTarget(goal)
	if agent.State() != world3d.NavAgentMoving {
		t.Fatalf("state after SetTarget = %v, want moving (must not falsely arrive underneath bridge)", agent.State())
	}
	if path := agent.Path(); len(path) < 4 {
		t.Fatalf("expected multi-segment 3D climb path, got %v", path)
	}

	const dt = time.Second / 60
	reachedRamp := false
	for step := 0; step < 900; step++ {
		agent.Step(dt)
		p := agent.Pos()
		polyIdx, ok := nm.FindPolygon(p)
		if !ok {
			t.Fatalf("step %d: agent left 3D navmesh at %v", step, p)
		}
		surfY := nm.Polys[polyIdx].HeightAt(p.X, p.Z)
		if !math3d.ApproxEqual(p.Y, surfY, 1e-2) {
			t.Fatalf("step %d: agent Y=%v disagrees with polygon %d HeightAt=%v", step, p.Y, polyIdx, surfY)
		}
		if polyIdx == 3 && p.Y > 0.5 && p.Y < 2.5 {
			reachedRamp = true
		}
		if agent.State() == world3d.NavAgentArrived {
			break
		}
	}

	if !reachedRamp {
		t.Fatal("agent never climbed intermediate heights on the sloped ramp")
	}
	if agent.State() != world3d.NavAgentArrived {
		t.Fatalf("agent did not arrive on bridge deck: state=%v pos=%v", agent.State(), agent.Pos())
	}
	if agent.Pos().Dist(goal) > 0.3+1e-3 {
		t.Fatalf("arrived too far from goal: pos=%v dist=%.4f", agent.Pos(), agent.Pos().Dist(goal))
	}
	if agent.Vel().Len() != 0 {
		t.Fatalf("expected zero velocity on arrival, got %v", agent.Vel())
	}
}

func TestNavAgent3DChasesMovingTarget(t *testing.T) {
	nm := buildMultiLevelBridgeNavMesh()

	chaser := world3d.NewNavAgent(nm, math3d.Vec3{X: 2, Y: 0, Z: 0}, world3d.NavAgentConfig{
		MaxSpeed:         7,
		MaxAcceleration:  35,
		Radius:           0.35,
		ArrivalRadius:    1.2,
		StoppingDistance: 0.45,
		ReplanInterval:   50 * time.Millisecond,
	})

	// Moving target walks from the ramp base up the sloped ramp onto the upper bridge deck,
	// crossing multiple polygons so the chaser must replan and climb to intercept it.
	waypoints := []math3d.Vec3{
		{X: 14, Y: 0, Z: 4},
		{X: 12, Y: 0, Z: 4},
		{X: 6, Y: 3, Z: 4},
		{X: 4, Y: 3, Z: 4},
		{X: 4, Y: 3, Z: 0},
		{X: 9, Y: 3, Z: 0},
	}
	targetPos := waypoints[0]
	wpIdx := 1
	const targetSpeed float32 = 5.2
	const dt = time.Second / 60

	chaser.SetTarget(targetPos)

	caught := false
	for step := 0; step < 1200; step++ {
		if wpIdx < len(waypoints) {
			diff := waypoints[wpIdx].Sub(targetPos)
			d := diff.Len()
			stepD := targetSpeed * float32(dt.Seconds())
			if d <= stepD {
				targetPos = waypoints[wpIdx]
				wpIdx++
			} else {
				targetPos = targetPos.Add(diff.Scale(stepD / d))
			}
		}

		chaser.SetTarget(targetPos)
		chaser.Step(dt)

		if _, ok := nm.FindPolygon(chaser.Pos()); !ok {
			t.Fatalf("step %d: 3D chaser left navmesh at %v", step, chaser.Pos())
		}
		if chaser.State() == world3d.NavAgentArrived {
			caught = true
			break
		}
	}

	if !caught {
		t.Fatalf("3D chaser failed to catch moving target: state=%v pos=%v target=%v dist=%.3f",
			chaser.State(), chaser.Pos(), targetPos, chaser.Pos().Dist(targetPos))
	}
	if chaser.ReplanCount() < 2 {
		t.Fatalf("expected 3D chaser to replan while chasing moving target up ramp, replans=%d", chaser.ReplanCount())
	}
}

func TestNavAgent3DPushedOffCorridorRepairsLocallyWithoutReplan(t *testing.T) {
	// Hallway 0 -> 1 -> 2 -> 3 with 1-hop alcove 4 (off poly 1) and 2-hop alcove 5 (off poly 4).
	nm := world3d.BuildNavMesh([][]math3d.Vec3{
		quadXZ(0, 0, 5, 5, 0),   // 0
		quadXZ(5, 0, 10, 5, 0),  // 1
		quadXZ(10, 0, 15, 5, 0), // 2
		quadXZ(15, 0, 20, 5, 0), // 3
		quadXZ(5, 5, 10, 10, 0), // 4: 1 hop from poly 1
		quadXZ(5, 10, 10, 15, 0),
	}, math3d.DegToRad(45))

	agent := world3d.NewNavAgent(nm, math3d.Vec3{X: 2.5, Y: 0, Z: 2.5}, world3d.NavAgentConfig{
		MaxSpeed:         10,
		MaxAcceleration:  50,
		StoppingDistance: 0.35,
		MaxRepairDepth:   2,
	})
	goal := math3d.Vec3{X: 17.5, Y: 0, Z: 2.5}
	agent.SetTarget(goal)

	initialReplans := agent.ReplanCount()
	if initialReplans != 1 {
		t.Fatalf("initial ReplanCount = %d, want 1", initialReplans)
	}

	const dt = time.Second / 60
	for range 60 {
		agent.Step(dt)
		if p, ok := nm.FindPolygon(agent.Pos()); ok && p == 1 {
			break
		}
	}

	// Push 1 hop sideways onto poly 4.
	agent.SetPosition(math3d.Vec3{X: 7.5, Y: 0, Z: 7.5})
	if agent.ReplanCount() != initialReplans {
		t.Fatalf("1-hop 3D push triggered full replan: got %d, want %d", agent.ReplanCount(), initialReplans)
	}
	if agent.LocalRepairCount() < 1 {
		t.Fatalf("expected LocalRepairCount >= 1, got %d", agent.LocalRepairCount())
	}

	// Push 2 hops sideways onto poly 5.
	repairsBefore := agent.LocalRepairCount()
	agent.SetPosition(math3d.Vec3{X: 7.5, Y: 0, Z: 12.5})
	if agent.ReplanCount() != initialReplans {
		t.Fatalf("2-hop 3D push triggered full replan: got %d, want %d", agent.ReplanCount(), initialReplans)
	}
	if agent.LocalRepairCount() <= repairsBefore {
		t.Fatalf("expected LocalRepairCount to increase after 2-hop push")
	}

	for range 600 {
		agent.Step(dt)
		if agent.State() == world3d.NavAgentArrived {
			break
		}
	}
	if agent.State() != world3d.NavAgentArrived {
		t.Fatalf("3D agent failed to recover and arrive: state=%v pos=%v", agent.State(), agent.Pos())
	}
	if agent.ReplanCount() != initialReplans {
		t.Fatalf("expected ZERO extra full replans during 3D local recovery: got %d, want %d", agent.ReplanCount(), initialReplans)
	}
}

func TestNavAgent3DUnreachableAndOffMeshLinkCallbacks(t *testing.T) {
	nm := world3d.BuildNavMesh([][]math3d.Vec3{
		quadXZ(0, 0, 6, 6, 0),   // 0: ground start
		quadXZ(6, 0, 12, 6, 0),  // 1: ground jump pad
		quadXZ(18, 0, 24, 6, 4), // 2: elevated floating island (Y=4)
	}, math3d.DegToRad(45))

	const flagGate uint32 = 1 << 2
	const flagJumpPad uint32 = 1 << 3

	var gateTicks, jumpTicks int
	var sawJump world3d.NavLinkEvent

	agent := world3d.NewNavAgent(nm, math3d.Vec3{X: 2, Y: 0, Z: 3}, world3d.NavAgentConfig{
		MaxSpeed:         10,
		MaxAcceleration:  60,
		StoppingDistance: 0.35,
		OnLink: func(a *world3d.NavAgent, ev world3d.NavLinkEvent, dt time.Duration) bool {
			if ev.OffMesh {
				sawJump = ev
				jumpTicks++
				frac := float32(jumpTicks) / 6.0
				if frac > 1 {
					frac = 1
				}
				p := ev.Start.Lerp(ev.End, frac)
				// Add a parabolic arc in Y during the jump.
				p.Y += 2.0 * 4 * frac * (1 - frac)
				a.SetPosition(p)
				return jumpTicks >= 6
			}
			gateTicks++
			return gateTicks >= 2
		},
	})

	// Before adding the off-mesh link, the floating island is unreachable.
	goal := math3d.Vec3{X: 22, Y: 4, Z: 3}
	agent.SetTarget(goal)
	if agent.State() != world3d.NavAgentUnreachable || agent.State().String() != "unreachable" {
		t.Fatalf("expected unreachable before off-mesh link, got %v", agent.State())
	}

	// Flag the gate edge and add a 3D off-mesh jump pad (bumps mesh Revision).
	if !nm.SetEdgeFlags(0, 1, flagGate) {
		t.Fatal("SetEdgeFlags(0, 1) failed")
	}
	if !nm.AddOffMeshLink(world3d.NavOffMeshLink{
		Start: math3d.Vec3{X: 11, Y: 0, Z: 3},
		End:   math3d.Vec3{X: 19, Y: 4, Z: 3},
		Flags: flagJumpPad,
		ID:    42,
	}) {
		t.Fatal("AddOffMeshLink failed")
	}

	const dt = time.Second / 60
	for range 400 {
		agent.Step(dt)
		if agent.State() == world3d.NavAgentArrived {
			break
		}
	}

	if agent.State() != world3d.NavAgentArrived {
		t.Fatalf("3D agent did not arrive across gate + off-mesh jump: state=%v pos=%v", agent.State(), agent.Pos())
	}
	if gateTicks != 2 {
		t.Fatalf("gateTicks = %d, want 2", gateTicks)
	}
	if jumpTicks != 6 || !sawJump.OffMesh || sawJump.LinkID != 42 || sawJump.Flags != (flagJumpPad|world3d.NavLinkOffMesh) {
		t.Fatalf("jump event mismatch: ticks=%d ev=%+v", jumpTicks, sawJump)
	}
	if !math3d.ApproxEqual(agent.Pos().Y, 4, 1e-2) {
		t.Fatalf("expected final Y=4 on floating island, got %v", agent.Pos().Y)
	}
}

func TestNavAgent3DGroupStaggeredReplanBudget(t *testing.T) {
	nm := buildMultiLevelBridgeNavMesh()

	const maxBudget = 2
	group := world3d.NewNavAgentGroup(world3d.NavAgentGroupConfig{
		MaxReplansPerStep: maxBudget,
		ReplanInterval:    100 * time.Millisecond,
	})

	const numAgents = 10
	for i := range numAgents {
		a := group.Add(nm, math3d.Vec3{X: float32(1 + i), Y: 0, Z: 0}, world3d.NavAgentConfig{
			MaxSpeed:         8,
			MaxAcceleration:  40,
			StoppingDistance: 0.4,
		})
		a.SetTarget(math3d.Vec3{X: 6, Y: 3, Z: 0})
	}

	const dt = time.Second / 60
	for step := 1; step <= 5; step++ {
		group.Step(dt)
		if got := group.LastStepReplans(); got != maxBudget {
			t.Fatalf("step %d: LastStepReplans = %d, want %d", step, got, maxBudget)
		}
	}

	for step := 0; step < 600; step++ {
		group.Step(dt)
		if got := group.LastStepReplans(); got > maxBudget {
			t.Fatalf("step %d: LastStepReplans = %d exceeded MaxReplansPerStep=%d", step, got, maxBudget)
		}
	}

	arrived := 0
	for _, a := range group.Agents() {
		if a.State() == world3d.NavAgentArrived {
			arrived++
		}
	}
	if arrived != numAgents {
		t.Fatalf("only %d/%d 3D agents arrived on bridge", arrived, numAgents)
	}
}

func TestNavAgent3DDeterministicReplayBitIdentical(t *testing.T) {
	runSim := func() [][3]uint32 {
		nm := buildMultiLevelBridgeNavMesh()
		group := world3d.NewNavAgentGroup(world3d.NavAgentGroupConfig{
			MaxReplansPerStep: 2,
			StaggerStride:     3,
			ReplanInterval:    50 * time.Millisecond,
		})
		for i := range 6 {
			a := group.Add(nm, math3d.Vec3{X: float32(2 + i), Y: 0, Z: -1 + float32(i%3)}, world3d.NavAgentConfig{
				MaxSpeed:         6,
				MaxAcceleration:  30,
				StoppingDistance: 0.35,
			})
			a.SetTarget(math3d.Vec3{X: 6, Y: 3, Z: 0})
		}

		const dt = time.Second / 60
		var trace [][3]uint32
		for step := 0; step < 240; step++ {
			if step == 40 {
				// Push agent 0 onto adjacent east landing.
				group.Agents()[0].SetPosition(math3d.Vec3{X: 13.5, Y: 0, Z: 0.5})
			}
			if step%20 == 0 {
				tx := float32(3 + (step/20)%6)
				for _, a := range group.Agents() {
					a.SetTarget(math3d.Vec3{X: tx, Y: 3, Z: 0})
				}
			}
			group.Step(dt)
			for _, a := range group.Agents() {
				p := a.Pos()
				trace = append(trace, [3]uint32{
					math.Float32bits(p.X),
					math.Float32bits(p.Y),
					math.Float32bits(p.Z),
				})
			}
		}
		return trace
	}

	run1 := runSim()
	run2 := runSim()
	if len(run1) != len(run2) {
		t.Fatalf("3D trace len mismatch: %d vs %d", len(run1), len(run2))
	}
	for i := range run1 {
		if run1[i] != run2[i] {
			t.Fatalf("3D replay diverged at sample %d: %x vs %x", i, run1[i], run2[i])
		}
	}
}
