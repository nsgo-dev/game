package ai_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/nsgo/core"
)

func dist2D(a, b core.Point) float64 {
	return math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y))
}

func speed2D(v core.Point) float64 {
	return math.Hypot(float64(v.X), float64(v.Y))
}

func TestNavAgentFollowsCorridorAndSlowsToArrive(t *testing.T) {
	// U-shaped corridor around wall [2,8] x [0,8].
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 2, 10),  // 0: left leg
		rectPoly(2, 8, 8, 10),  // 1: top bridge
		rectPoly(8, 0, 10, 10), // 2: right leg
	})

	agent := ai.NewNavAgent(mesh, pt(1, 1), ai.NavAgentConfig{
		MaxSpeed:         10,
		MaxAcceleration:  40,
		Radius:           0.3,
		ArrivalRadius:    2.5,
		StoppingDistance: 0.25,
	})
	if agent.State() != ai.NavAgentIdle || agent.State().String() != "idle" {
		t.Fatalf("initial state = %v, want idle", agent.State())
	}

	goal := pt(9, 1)
	agent.SetTarget(goal)
	if agent.State() != ai.NavAgentMoving {
		t.Fatalf("state after SetTarget = %v, want moving", agent.State())
	}
	if len(agent.Corridor()) != 3 {
		t.Fatalf("initial corridor = %v, want 3 polygons", agent.Corridor())
	}
	corner, ok := agent.NextCorner()
	if !ok || dist2D(corner, pt(2, 8)) > 1e-3 {
		t.Fatalf("initial NextCorner = (%v, %v), want (2, 8)", corner, ok)
	}
	if path := agent.Path(); len(path) != 4 {
		t.Fatalf("initial Path len = %d (%v), want 4", len(path), path)
	}

	const dt = time.Second / 60
	var maxCruiseSpeed float64
	var slowSpeed float64
	recordedSlow := false

	for step := 0; step < 600; step++ {
		agent.Step(dt)
		if !mesh.Contains(agent.Pos()) {
			t.Fatalf("step %d: agent left navmesh at %v", step, agent.Pos())
		}
		spd := speed2D(agent.Vel())
		if spd > maxCruiseSpeed {
			maxCruiseSpeed = spd
		}
		d := dist2D(agent.Pos(), goal)
		if len(agent.Corridor()) == 1 && d < 1.2 && d > 0.4 && !recordedSlow {
			slowSpeed = spd
			recordedSlow = true
		}
		if agent.State() == ai.NavAgentArrived {
			break
		}
	}

	if agent.State() != ai.NavAgentArrived {
		t.Fatalf("did not arrive: state=%v pos=%v dist=%.3f", agent.State(), agent.Pos(), dist2D(agent.Pos(), goal))
	}
	if dist2D(agent.Pos(), goal) > 0.25+1e-4 {
		t.Fatalf("arrived too far from goal: pos=%v dist=%.4f", agent.Pos(), dist2D(agent.Pos(), goal))
	}
	if speed2D(agent.Vel()) != 0 {
		t.Fatalf("expected zero velocity on arrival, got %v", agent.Vel())
	}
	if !recordedSlow || slowSpeed >= maxCruiseSpeed*0.85 {
		t.Fatalf("expected agent to slow inside ArrivalRadius (cruise=%.2f, nearGoal=%.2f, recorded=%v)", maxCruiseSpeed, slowSpeed, recordedSlow)
	}
	if agent.ReplanCount() != 1 {
		t.Fatalf("expected single initial plan, got %d replans", agent.ReplanCount())
	}
}

func TestNavAgentChasesMovingTarget(t *testing.T) {
	// Arena with a central island obstacle [4,6] x [4,6] surrounded by 4 connected polygons.
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 4, 10),  // 0: left
		rectPoly(4, 6, 6, 10),  // 1: top
		rectPoly(6, 0, 10, 10), // 2: right
		rectPoly(4, 0, 6, 4),   // 3: bottom
	})

	chaser := ai.NewNavAgent(mesh, pt(1, 5), ai.NavAgentConfig{
		MaxSpeed:         12,
		MaxAcceleration:  60,
		Radius:           0.3,
		ArrivalRadius:    1.2,
		StoppingDistance: 0.4,
		ReplanInterval:   50 * time.Millisecond,
	})

	// Moving target walks clockwise around the island from right -> bottom -> left -> top -> right,
	// moving fast enough to round multiple polygons before the chaser intercepts it.
	waypoints := []core.Point{
		pt(8.5, 2.0),
		pt(5.0, 1.5),
		pt(1.5, 1.5),
		pt(1.5, 8.5),
		pt(8.5, 8.5),
	}
	targetPos := waypoints[0]
	wpIdx := 1
	const targetSpeed = 8.5
	const dt = time.Second / 60

	chaser.SetTarget(targetPos)

	caught := false
	for step := 0; step < 900; step++ {
		if wpIdx < len(waypoints) {
			toWP := pt(float32(waypoints[wpIdx].X-targetPos.X), float32(waypoints[wpIdx].Y-targetPos.Y))
			d := math.Hypot(float64(toWP.X), float64(toWP.Y))
			stepD := targetSpeed * dt.Seconds()
			if d <= stepD {
				targetPos = waypoints[wpIdx]
				wpIdx++
			} else {
				targetPos = pt(
					float32(float64(targetPos.X)+float64(toWP.X)*(stepD/d)),
					float32(float64(targetPos.Y)+float64(toWP.Y)*(stepD/d)),
				)
			}
		}

		chaser.SetTarget(targetPos)
		chaser.Step(dt)

		if !mesh.Contains(chaser.Pos()) {
			t.Fatalf("step %d: chaser left navmesh at %v", step, chaser.Pos())
		}
		if chaser.State() == ai.NavAgentArrived {
			caught = true
			break
		}
	}

	if !caught {
		t.Fatalf("chaser never caught moving target: state=%v pos=%v target=%v dist=%.3f",
			chaser.State(), chaser.Pos(), targetPos, dist2D(chaser.Pos(), targetPos))
	}
	if chaser.ReplanCount() < 2 {
		t.Fatalf("expected chaser to replan while pursuing moving target around obstacle, replans=%d", chaser.ReplanCount())
	}
}

func TestNavAgentPushedOffCorridorRepairsLocallyWithoutReplan(t *testing.T) {
	// Main hallway: 0 -> 1 -> 2 -> 3
	// 1-hop side alcove: 4 (adjacent to 1)
	// 2-hop side alcove: 5 (adjacent to 4 only)
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 10, 10),   // 0
		rectPoly(10, 0, 20, 10),  // 1
		rectPoly(20, 0, 30, 10),  // 2
		rectPoly(30, 0, 40, 10),  // 3
		rectPoly(10, 10, 20, 20), // 4: 1 hop from poly 1
		rectPoly(10, 20, 20, 30), // 5: 2 hops from poly 1 (via 4)
	})

	agent := ai.NewNavAgent(mesh, pt(5, 5), ai.NavAgentConfig{
		MaxSpeed:         20,
		MaxAcceleration:  80,
		Radius:           0.5,
		StoppingDistance: 0.5,
		MaxRepairDepth:   2,
	})
	goal := pt(35, 5)
	agent.SetTarget(goal)

	initialReplans := agent.ReplanCount()
	if initialReplans != 1 {
		t.Fatalf("initial ReplanCount = %d, want 1", initialReplans)
	}

	const dt = time.Second / 60
	// Advance into polygon 1.
	for range 45 {
		agent.Step(dt)
		if mesh.FindPolygon(agent.Pos()) == 1 {
			break
		}
	}
	if mesh.FindPolygon(agent.Pos()) != 1 {
		t.Fatalf("expected agent in poly 1, got poly %d at %v", mesh.FindPolygon(agent.Pos()), agent.Pos())
	}

	// Push agent sideways onto 1-hop adjacent alcove (poly 4).
	agent.SetPosition(pt(15, 15))
	if agent.ReplanCount() != initialReplans {
		t.Fatalf("1-hop push triggered full replan: got %d, want %d", agent.ReplanCount(), initialReplans)
	}
	if agent.LocalRepairCount() < 1 {
		t.Fatalf("expected LocalRepairCount >= 1 after 1-hop push, got %d", agent.LocalRepairCount())
	}
	corridor := agent.Corridor()
	if len(corridor) == 0 || corridor[0] != 4 {
		t.Fatalf("corridor after 1-hop push = %v, want prefix 4", corridor)
	}

	// Step several frames and confirm no replan occurred.
	for range 10 {
		agent.Step(dt)
	}
	if agent.ReplanCount() != initialReplans {
		t.Fatalf("stepping after 1-hop repair triggered full replan: got %d, want %d", agent.ReplanCount(), initialReplans)
	}

	// Push agent 2 hops sideways onto alcove (poly 5) — still within MaxRepairDepth=2!
	repairsBefore := agent.LocalRepairCount()
	agent.SetPosition(pt(15, 25))
	if agent.ReplanCount() != initialReplans {
		t.Fatalf("2-hop push triggered full replan: got %d, want %d", agent.ReplanCount(), initialReplans)
	}
	if agent.LocalRepairCount() <= repairsBefore {
		t.Fatalf("expected LocalRepairCount to increase after 2-hop push")
	}
	corridor = agent.Corridor()
	if len(corridor) < 3 || corridor[0] != 5 || corridor[1] != 4 {
		t.Fatalf("corridor after 2-hop push = %v, want prefix [5, 4, ...]", corridor)
	}

	// Agent should steer all the way back out of [5, 4] and reach the goal with ZERO additional full replans!
	for range 600 {
		agent.Step(dt)
		if agent.State() == ai.NavAgentArrived {
			break
		}
	}
	if agent.State() != ai.NavAgentArrived {
		t.Fatalf("agent failed to recover and arrive: state=%v pos=%v", agent.State(), agent.Pos())
	}
	if agent.ReplanCount() != initialReplans {
		t.Fatalf("expected ZERO extra full replans during local push recovery: got %d, want %d", agent.ReplanCount(), initialReplans)
	}
}

func TestNavAgentTargetLocalEndRepairAndTruncation(t *testing.T) {
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 10, 10),  // 0
		rectPoly(10, 0, 20, 10), // 1
		rectPoly(20, 0, 30, 10), // 2
		rectPoly(30, 0, 40, 10), // 3
	})

	agent := ai.NewNavAgent(mesh, pt(5, 5), ai.NavAgentConfig{
		MaxSpeed:         15,
		StoppingDistance: 0.5,
		MaxRepairDepth:   2,
	})
	agent.SetTarget(pt(25, 5)) // corridor [0, 1, 2]
	if agent.ReplanCount() != 1 {
		t.Fatalf("ReplanCount = %d, want 1", agent.ReplanCount())
	}

	// Target moves 1 polygon forward into poly 3 -> cheap local extension, no full replan!
	agent.SetTarget(pt(35, 5))
	if agent.ReplanCount() != 1 {
		t.Fatalf("adjacent target move triggered full replan: %d", agent.ReplanCount())
	}
	if got := agent.Corridor(); len(got) != 4 || got[3] != 3 {
		t.Fatalf("corridor after local extension = %v, want [0 1 2 3]", got)
	}

	// Target moves back into poly 1 -> cheap truncation, no full replan!
	agent.SetTarget(pt(15, 5))
	if agent.ReplanCount() != 1 {
		t.Fatalf("target move onto earlier corridor poly triggered full replan: %d", agent.ReplanCount())
	}
	if got := agent.Corridor(); len(got) != 2 || got[1] != 1 {
		t.Fatalf("corridor after truncation = %v, want [0 1]", got)
	}
}

func TestNavAgentUnreachableAndMeshRevisionRecovery(t *testing.T) {
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 10, 10),  // 0: island A
		rectPoly(20, 0, 30, 10), // 1: island B (disconnected)
	})

	agent := ai.NewNavAgent(mesh, pt(5, 5), ai.NavAgentConfig{
		MaxSpeed:         20,
		StoppingDistance: 0.5,
	})

	// Disconnected island reports NavAgentUnreachable.
	agent.SetTarget(pt(25, 5))
	if agent.State() != ai.NavAgentUnreachable || agent.State().String() != "unreachable" {
		t.Fatalf("disconnected target state = %v, want unreachable", agent.State())
	}

	// Off-mesh point also reports NavAgentUnreachable.
	agent.SetTarget(pt(15, 5))
	if agent.State() != ai.NavAgentUnreachable {
		t.Fatalf("off-mesh target state = %v, want unreachable", agent.State())
	}

	// Point back at disconnected island, then connect it via an off-mesh link (bumps Revision).
	agent.SetTarget(pt(25, 5))
	if agent.State() != ai.NavAgentUnreachable {
		t.Fatalf("expected unreachable before link, got %v", agent.State())
	}
	if !mesh.AddOffMeshLink(ai.NavOffMeshLink{
		Start: pt(9, 5),
		End:   pt(21, 5),
		ID:    7,
	}) {
		t.Fatal("AddOffMeshLink returned false")
	}

	const dt = time.Second / 60
	for range 300 {
		agent.Step(dt)
		if agent.State() == ai.NavAgentArrived {
			break
		}
	}
	if agent.State() != ai.NavAgentArrived {
		t.Fatalf("expected agent to recover and arrive after mesh revision bump, state=%v pos=%v", agent.State(), agent.Pos())
	}

	// ClearTarget returns to idle; dt<=0 is a no-op; nil mesh is safe.
	posBefore := agent.Pos()
	agent.Step(0)
	agent.Step(-dt)
	if agent.Pos() != posBefore {
		t.Fatal("non-positive dt modified position")
	}
	agent.ClearTarget()
	if agent.State() != ai.NavAgentIdle || agent.HasTarget() {
		t.Fatalf("after ClearTarget: state=%v hasTarget=%v", agent.State(), agent.HasTarget())
	}

	nilAgent := ai.NewNavAgent(nil, pt(0, 0), ai.NavAgentConfig{})
	nilAgent.SetTarget(pt(5, 5))
	nilAgent.Step(dt)
	if nilAgent.State() != ai.NavAgentUnreachable {
		t.Fatalf("nil mesh agent state = %v, want unreachable", nilAgent.State())
	}
}

func TestNavAgentOffMeshLinkAndFlaggedEdgeCallbacks(t *testing.T) {
	// Island 0 -> shared edge (flagged door) -> Island 1 -> off-mesh jump -> Island 2
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 10, 10),  // 0
		rectPoly(10, 0, 20, 10), // 1
		rectPoly(30, 0, 40, 10), // 2 (gap 20..30)
	})
	const flagDoor uint32 = 1 << 4
	const flagJump uint32 = 1 << 5

	if !mesh.SetEdgeFlags(0, 1, flagDoor) {
		t.Fatal("SetEdgeFlags(0, 1) failed")
	}
	if !mesh.AddOffMeshLink(ai.NavOffMeshLink{
		Start: pt(19, 5),
		End:   pt(31, 5),
		Flags: flagJump,
		ID:    99,
	}) {
		t.Fatal("AddOffMeshLink failed")
	}

	var doorTicks, jumpTicks int
	var sawDoorEvent, sawJumpEvent ai.NavLinkEvent

	agent := ai.NewNavAgent(mesh, pt(2, 5), ai.NavAgentConfig{
		MaxSpeed:         20,
		MaxAcceleration:  100,
		StoppingDistance: 0.4,
		OnLink: func(a *ai.NavAgent, ev ai.NavLinkEvent, dt time.Duration) bool {
			if ev.OffMesh {
				sawJumpEvent = ev
				jumpTicks++
				frac := float64(jumpTicks) / 5.0
				if frac > 1 {
					frac = 1
				}
				nx := float64(ev.Start.X) + (float64(ev.End.X)-float64(ev.Start.X))*frac
				ny := float64(ev.Start.Y) + (float64(ev.End.Y)-float64(ev.Start.Y))*frac
				a.SetPosition(pt(float32(nx), float32(ny)))
				return jumpTicks >= 5
			}
			// Flagged shared edge (door): wait 2 steps before opening.
			sawDoorEvent = ev
			doorTicks++
			return doorTicks >= 2
		},
	})
	agent.SetTarget(pt(38, 5))

	const dt = time.Second / 60
	sawTraversing := false
	for range 400 {
		agent.Step(dt)
		if _, active := agent.TraversingLink(); active {
			sawTraversing = true
		}
		if agent.State() == ai.NavAgentArrived {
			break
		}
	}

	if agent.State() != ai.NavAgentArrived {
		t.Fatalf("agent did not arrive across door + off-mesh jump: state=%v pos=%v", agent.State(), agent.Pos())
	}
	if !sawTraversing {
		t.Fatal("expected TraversingLink() to be true during multi-step link traversal")
	}
	if doorTicks != 2 || sawDoorEvent.Flags != flagDoor || sawDoorEvent.OffMesh {
		t.Fatalf("door callback mismatch: ticks=%d ev=%+v", doorTicks, sawDoorEvent)
	}
	if jumpTicks != 5 || !sawJumpEvent.OffMesh || sawJumpEvent.LinkID != 99 || sawJumpEvent.Flags != (flagJump|ai.NavLinkOffMesh) {
		t.Fatalf("jump callback mismatch: ticks=%d ev=%+v", jumpTicks, sawJumpEvent)
	}
}

func TestNavAgentGroupStaggeredReplanBudget(t *testing.T) {
	// L-shaped arena of 5 polygons.
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 10, 10),  // 0
		rectPoly(10, 0, 20, 10), // 1
		rectPoly(20, 0, 30, 10), // 2
		rectPoly(20, 10, 30, 20),
		rectPoly(20, 20, 30, 30),
	})

	const maxBudget = 3
	group := ai.NewNavAgentGroup(ai.NavAgentGroupConfig{
		MaxReplansPerStep: maxBudget,
		ReplanInterval:    100 * time.Millisecond,
	})

	const numAgents = 12
	for i := range numAgents {
		a := group.Add(mesh, pt(float32(1+(i%4)*2), float32(1+(i/4)*3)), ai.NavAgentConfig{
			MaxSpeed:         20,
			MaxAcceleration:  80,
			StoppingDistance: 0.5,
		})
		a.SetTarget(pt(25, 25))
	}

	// Because agents belong to a group, SetTarget must NOT have executed synchronous A* replans!
	for i, a := range group.Agents() {
		if a.ReplanCount() != 0 {
			t.Fatalf("agent %d replanned eagerly outside group.Step: %d", i, a.ReplanCount())
		}
	}

	const dt = time.Second / 60
	// First 4 steps must budget 3 + 3 + 3 + 3 = 12 replans strictly <= maxBudget per step.
	for step := 1; step <= 4; step++ {
		group.Step(dt)
		if got := group.LastStepReplans(); got != maxBudget {
			t.Fatalf("step %d: LastStepReplans = %d, want %d", step, got, maxBudget)
		}
	}

	// Jump all targets to a far polygon and verify MaxReplansPerStep is NEVER exceeded on any step.
	for step := 0; step < 300; step++ {
		if step == 20 {
			for _, a := range group.Agents() {
				a.SetTarget(pt(25, 28))
			}
		}
		group.Step(dt)
		if got := group.LastStepReplans(); got > maxBudget {
			t.Fatalf("step %d: LastStepReplans = %d exceeded MaxReplansPerStep=%d", step, got, maxBudget)
		}
	}

	arrived := 0
	for _, a := range group.Agents() {
		if a.State() == ai.NavAgentArrived {
			arrived++
		}
	}
	if arrived != numAgents {
		t.Fatalf("only %d/%d agents arrived", arrived, numAgents)
	}

	// Verify Remove cleanly detaches an agent.
	first := group.Agents()[0]
	if !group.Remove(first) || group.Len() != numAgents-1 {
		t.Fatalf("Remove failed: len=%d", group.Len())
	}
}

func TestNavAgentDeterministicReplayBitIdentical(t *testing.T) {
	runSim := func() [][2]uint32 {
		mesh := ai.BuildNavMesh([][]core.Point{
			rectPoly(0, 0, 10, 10),
			rectPoly(10, 0, 20, 10),
			rectPoly(20, 0, 30, 10),
			rectPoly(10, 10, 20, 20),
			rectPoly(20, 10, 30, 20),
		})
		group := ai.NewNavAgentGroup(ai.NavAgentGroupConfig{
			MaxReplansPerStep: 2,
			StaggerStride:     3,
			ReplanInterval:    50 * time.Millisecond,
		})
		for i := range 6 {
			a := group.Add(mesh, pt(float32(2+i), float32(2+i)), ai.NavAgentConfig{
				MaxSpeed:         14,
				MaxAcceleration:  56,
				StoppingDistance: 0.4,
			})
			a.SetTarget(pt(25, 15))
		}

		const dt = time.Second / 60
		var trace [][2]uint32
		for step := 0; step < 180; step++ {
			if step == 30 {
				// Push agent 0 off its corridor onto adjacent polygon 3.
				group.Agents()[0].SetPosition(pt(15, 15))
			}
			if step%15 == 0 {
				tx := float32(22 + (step/15)%6)
				ty := float32(12 + (step/30)%6)
				for _, a := range group.Agents() {
					a.SetTarget(pt(tx, ty))
				}
			}
			group.Step(dt)
			for _, a := range group.Agents() {
				p := a.Pos()
				trace = append(trace, [2]uint32{math.Float32bits(float32(p.X)), math.Float32bits(float32(p.Y))})
			}
		}
		return trace
	}

	run1 := runSim()
	run2 := runSim()
	if len(run1) != len(run2) {
		t.Fatalf("trace len mismatch: %d vs %d", len(run1), len(run2))
	}
	for i := range run1 {
		if run1[i] != run2[i] {
			t.Fatalf("replay diverged at sample %d: %x vs %x", i, run1[i], run2[i])
		}
	}
}
