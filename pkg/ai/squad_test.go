package ai_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/nsgo/core"
)

func assertPoolInvariant(t *testing.T, pool *ai.TokenPool) {
	t.Helper()
	total := pool.Available() + pool.HeldCount() + pool.CoolingDown()
	if total != pool.Capacity() {
		t.Fatalf("token pool conservation violated: avail=%d held=%d cool=%d cap=%d",
			pool.Available(), pool.HeldCount(), pool.CoolingDown(), pool.Capacity())
	}
	if pool.HeldCount() > pool.Capacity() {
		t.Fatalf("held count %d exceeds capacity %d", pool.HeldCount(), pool.Capacity())
	}
}

func TestTokenPoolTwoTokensSixEnemiesAndPreemption(t *testing.T) {
	pool := ai.NewTokenPool(ai.TokenPoolConfig{
		Capacity: 2,
		Cooldown: 300 * time.Millisecond,
	})
	dt := 100 * time.Millisecond

	// Six enemies request attack tokens with distinct priorities in scrambled order.
	requests := []struct {
		id   uint64
		prio float64
	}{
		{4, 1.0},
		{2, 5.0},
		{6, 2.0},
		{1, 3.0},
		{5, 6.0},
		{3, 4.0},
	}
	for _, r := range requests {
		pool.Request(r.id, r.prio)
	}
	pool.Step(dt)
	assertPoolInvariant(t, pool)

	if pool.HeldCount() != 2 {
		t.Fatalf("HeldCount = %d, want 2", pool.HeldCount())
	}
	if !pool.HasToken(5) || !pool.HasToken(2) {
		t.Fatalf("expected top-2 priority enemies 5 and 2 to hold tokens, got %+v", pool.Holders(nil))
	}
	if pool.PendingCount() != 4 {
		t.Fatalf("PendingCount = %d, want 4", pool.PendingCount())
	}

	// Higher-priority request from enemy 6 (now 9.0) preempts lowest-priority holder (enemy 2, prio 5.0).
	pool.Request(6, 9.0)
	pool.Step(dt)
	assertPoolInvariant(t, pool)

	if pool.HeldCount() != 2 {
		t.Fatalf("HeldCount after preemption = %d, want 2", pool.HeldCount())
	}
	if !pool.HasToken(5) || !pool.HasToken(6) {
		t.Fatalf("expected enemies 5 and 6 to hold tokens after preemption, got %+v", pool.Holders(nil))
	}
	if pool.HasToken(2) {
		t.Fatalf("enemy 2 should have been preempted")
	}
	// Preempted transfer does not enter cooldown.
	if pool.CoolingDown() != 0 {
		t.Fatalf("CoolingDown after preemption = %d, want 0", pool.CoolingDown())
	}

	// Enemy 6 finishes its attack and releases its token; cooldown delays re-acquisition.
	if !pool.Release(6) {
		t.Fatalf("Release(6) = false, want true")
	}
	assertPoolInvariant(t, pool)
	if pool.HeldCount() != 1 || pool.CoolingDown() != 1 {
		t.Fatalf("after release: held=%d cool=%d, want 1 and 1", pool.HeldCount(), pool.CoolingDown())
	}

	// Simulate 200ms (still cooling down for 100ms more): waiting enemies cannot take the cooling token.
	pool.Step(200 * time.Millisecond)
	assertPoolInvariant(t, pool)
	if pool.HeldCount() != 1 || pool.CoolingDown() != 1 {
		t.Fatalf("mid-cooldown: held=%d cool=%d, want 1 and 1", pool.HeldCount(), pool.CoolingDown())
	}

	// Advance remaining 100ms: token exits cooldown and goes to highest-priority waiting enemy (3, prio 4.0).
	pool.Step(100 * time.Millisecond)
	assertPoolInvariant(t, pool)
	if pool.HeldCount() != 2 || pool.CoolingDown() != 0 {
		t.Fatalf("post-cooldown: held=%d cool=%d, want 2 and 0", pool.HeldCount(), pool.CoolingDown())
	}
	if !pool.HasToken(5) || !pool.HasToken(3) {
		t.Fatalf("expected enemies 5 and 3 to hold tokens, got %+v", pool.Holders(nil))
	}

	// Enemy 5 dies -> Remove releases token into cooldown; enemy 1 gives up -> Remove cancels pending request.
	if !pool.Remove(5) {
		t.Fatalf("Remove(5) = false, want true")
	}
	if !pool.Remove(1) {
		t.Fatalf("Remove(1) = false, want true")
	}
	if pool.Remove(999) {
		t.Fatalf("Remove(999) = true, want false")
	}
	assertPoolInvariant(t, pool)

	// After cooldown elapses, only remaining pending enemy 4 receives the freed token (not dead enemy 1).
	pool.Step(300 * time.Millisecond)
	assertPoolInvariant(t, pool)
	if !pool.HasToken(3) || !pool.HasToken(4) || pool.HasToken(1) || pool.HasToken(5) {
		t.Fatalf("unexpected holders after removals: %+v", pool.Holders(nil))
	}
}

func TestTokenPoolPreemptMarginAndTieBreaking(t *testing.T) {
	pool := ai.NewTokenPool(ai.TokenPoolConfig{
		Capacity:      2,
		PreemptMargin: 2.0,
	})

	// Equal-priority requests break ties by lowest AgentID.
	pool.Request(30, 5.0)
	pool.Request(10, 5.0)
	pool.Request(20, 5.0)
	pool.Step(16 * time.Millisecond)
	if !pool.HasToken(10) || !pool.HasToken(20) || pool.HasToken(30) {
		t.Fatalf("expected lowest IDs 10 and 20 to win equal-priority tie, got %+v", pool.Holders(nil))
	}

	// Priority 6.5 does not meet PreemptMargin 2.0 over 5.0.
	pool.Request(30, 6.5)
	pool.Step(16 * time.Millisecond)
	if pool.HasToken(30) {
		t.Fatalf("priority 6.5 should not preempt holder at 5.0 with margin 2.0")
	}

	// Priority 7.0 meets margin 2.0 and preempts highest-ID tied holder (20, keeping lower-ID 10).
	pool.Request(30, 7.0)
	pool.Step(16 * time.Millisecond)
	if !pool.HasToken(10) || !pool.HasToken(30) || pool.HasToken(20) {
		t.Fatalf("expected 30 to preempt higher-ID tied holder 20, got %+v", pool.Holders(nil))
	}

	// Refreshing holder priority via Request works without consuming a second token.
	pool.Request(10, 10.0)
	h, ok := pool.Holder(10)
	if !ok || h.Priority != 10.0 {
		t.Fatalf("Holder(10) = (%+v, %v), want priority 10.0", h, ok)
	}
	assertPoolInvariant(t, pool)

	// Infinite priority edge case: +Inf cannot preempt +Inf even with PreemptMargin > 0.
	pool.Request(10, math.Inf(1))
	pool.Request(30, math.Inf(1))
	pool.Request(40, math.Inf(1))
	pool.Step(16 * time.Millisecond)
	if pool.HasToken(40) {
		t.Fatalf("+Inf requester must not preempt +Inf holders")
	}
}

func TestTokenPoolMaxHoldDurationFullCooldownAndEdgeCases(t *testing.T) {
	pool := ai.NewTokenPool(ai.TokenPoolConfig{
		Capacity:        1,
		Cooldown:        200 * time.Millisecond,
		MaxHoldDuration: 300 * time.Millisecond,
	})

	// NaN priority is ignored.
	pool.Request(1, math.NaN())
	pool.Step(100 * time.Millisecond)
	if pool.HeldCount() != 0 || pool.PendingCount() != 0 {
		t.Fatalf("NaN request should be ignored")
	}

	pool.Request(1, 5.0)
	pool.Request(2, 4.0)
	pool.Step(100 * time.Millisecond)
	if !pool.HasToken(1) {
		t.Fatalf("agent 1 should hold token")
	}

	// Hold for 200ms (< 300ms): still held.
	pool.Step(200 * time.Millisecond)
	if !pool.HasToken(1) {
		t.Fatalf("agent 1 should still hold token at 200ms")
	}

	// Step another 100ms -> reaches 300ms MaxHoldDuration and enters FULL 200ms cooldown
	// (must NOT lose 100ms of cooldown on the same step).
	pool.Step(100 * time.Millisecond)
	assertPoolInvariant(t, pool)
	if pool.HasToken(1) || pool.HasToken(2) || pool.CoolingDown() != 1 {
		t.Fatalf("at expiry: has1=%v has2=%v cool=%d, want false, false, 1",
			pool.HasToken(1), pool.HasToken(2), pool.CoolingDown())
	}

	// After 100ms of cooldown, 100ms still remain: agent 2 must still wait.
	pool.Step(100 * time.Millisecond)
	if pool.HasToken(2) || pool.CoolingDown() != 1 {
		t.Fatalf("after 100ms of 200ms cooldown, token should still be cooling down")
	}

	// After the remaining 100ms, agent 2 acquires the token.
	pool.Step(100 * time.Millisecond)
	if !pool.HasToken(2) || pool.CoolingDown() != 0 {
		t.Fatalf("after full 200ms cooldown, agent 2 should hold token")
	}

	// Cancel withdraws pending request without releasing held token.
	pool.Request(3, 1.0)
	if !pool.Cancel(3) || pool.Cancel(3) {
		t.Fatalf("Cancel(3) behavior unexpected")
	}
	if !pool.HasToken(2) {
		t.Fatalf("Cancel(3) should not affect holder 2")
	}

	// Reset restores full availability.
	pool.Reset()
	if pool.Available() != 1 || pool.HeldCount() != 0 || pool.PendingCount() != 0 || pool.CoolingDown() != 0 {
		t.Fatalf("Reset did not clear pool state")
	}

	// Negative / zero capacity grants nothing.
	zeroPool := ai.NewTokenPool(ai.TokenPoolConfig{Capacity: -3, Cooldown: -time.Second, PreemptMargin: math.NaN(), MaxHoldDuration: -time.Second})
	if zeroPool.Capacity() != 0 || zeroPool.Config().Cooldown != 0 || zeroPool.Config().PreemptMargin != 0 || zeroPool.Config().MaxHoldDuration != 0 {
		t.Fatalf("config sanitization failed: %+v", zeroPool.Config())
	}
	zeroPool.Request(1, 10)
	zeroPool.Step(time.Second)
	if zeroPool.HeldCount() != 0 {
		t.Fatalf("zero-capacity pool should not grant tokens")
	}
}

func TestTokenPoolDeterminismAcrossRequestOrders(t *testing.T) {
	run := func(order []uint64) [][]uint64 {
		pool := ai.NewTokenPool(ai.TokenPoolConfig{
			Capacity: 2,
			Cooldown: 100 * time.Millisecond,
		})
		prios := map[uint64]float64{
			1: 3.0, 2: 5.0, 3: 5.0, 4: 2.0, 5: 5.0, 6: 4.0,
		}
		var trace [][]uint64
		for step := range 12 {
			for _, id := range order {
				p := prios[id]
				if step == 4 && id == 6 {
					p = 10.0
				}
				if step == 7 && id == 4 {
					p = 12.0
				}
				pool.Request(id, p)
			}
			pool.Step(50 * time.Millisecond)
			if step == 2 {
				pool.Release(2)
			}
			if step == 6 {
				pool.Remove(6)
			}
			holders := pool.Holders(nil)
			ids := make([]uint64, len(holders))
			for i, h := range holders {
				ids[i] = h.AgentID
			}
			trace = append(trace, ids)
		}
		return trace
	}

	a := run([]uint64{1, 2, 3, 4, 5, 6})
	b := run([]uint64{6, 5, 4, 3, 2, 1})
	c := run([]uint64{3, 6, 1, 5, 2, 4})

	for step := range a {
		if !slices.Equal(a[step], b[step]) || !slices.Equal(a[step], c[step]) {
			t.Fatalf("step %d differed by submission order: a=%v b=%v c=%v", step, a[step], b[step], c[step])
		}
	}
}

func TestTokenManagerPerTargetAndRemoveAgent(t *testing.T) {
	mgr := ai.NewTokenManager(ai.TokenPoolConfig{Capacity: 1, Cooldown: 100 * time.Millisecond})
	mgr.ConfigureTarget(20, ai.TokenPoolConfig{Capacity: 2})

	mgr.Request(10, 1, 5)
	mgr.Request(10, 2, 8)
	mgr.Request(20, 1, 3)
	mgr.Request(20, 2, 4)
	mgr.Request(20, 3, 1)
	mgr.Step(16 * time.Millisecond)

	if !mgr.HasToken(10, 2) || mgr.HasToken(10, 1) {
		t.Fatalf("target 10: expected agent 2 to hold default 1-token pool")
	}
	if !mgr.HasToken(20, 1) || !mgr.HasToken(20, 2) || mgr.HasToken(20, 3) {
		t.Fatalf("target 20: expected agents 1 and 2 to hold 2-token pool")
	}

	// Agent 2 dies across all targets.
	mgr.RemoveAgent(2)
	if mgr.HasToken(10, 2) || mgr.HasToken(20, 2) {
		t.Fatalf("RemoveAgent(2) did not release tokens across all targets")
	}
	// Target 20 has 0 cooldown so agent 3 takes the freed token immediately on next step;
	// target 10 has 100ms cooldown so agent 1 waits.
	mgr.Step(50 * time.Millisecond)
	if !mgr.HasToken(20, 3) {
		t.Fatalf("target 20: expected agent 3 to acquire freed token")
	}
	if mgr.HasToken(10, 1) {
		t.Fatalf("target 10: expected cooldown to hold agent 1 back")
	}
	mgr.Step(50 * time.Millisecond)
	if !mgr.HasToken(10, 1) {
		t.Fatalf("target 10: expected agent 1 to acquire token after cooldown")
	}

	if !mgr.RemoveTarget(10) || mgr.Pool(10) != nil || mgr.HasToken(10, 1) {
		t.Fatalf("RemoveTarget(10) failed")
	}
	mgr.Reset()
	if mgr.Pool(20) != nil {
		t.Fatalf("Reset did not clear target pools")
	}
}

func TestBehaviorTreeWithAttackTokenSixEnemies(t *testing.T) {
	pool := ai.NewTokenPool(ai.TokenPoolConfig{
		Capacity: 2,
		Cooldown: 100 * time.Millisecond,
	})

	type enemy struct {
		id       uint64
		prio     float64
		attacked int
		circled  int
		tree     ai.Node
	}

	enemies := make([]*enemy, 6)
	for i := range enemies {
		e := &enemy{
			id:   uint64(i + 1),
			prio: float64(i + 1),
		}
		e.tree = ai.Selector(
			ai.WithAttackToken(pool, e.id, func() float64 { return e.prio }, ai.Sequence(
				ai.Wait(100*time.Millisecond),
				ai.Action(func(time.Duration) ai.Status {
					e.attacked++
					return ai.StatusSuccess
				}),
			)),
			ai.Action(func(time.Duration) ai.Status {
				e.circled++
				return ai.StatusRunning
			}),
		)
		enemies[i] = e
	}

	dt := 50 * time.Millisecond
	maxSimultaneous := 0
	for step := range 25 {
		if step == 6 {
			// Boost enemy 1 to preempt a current attacker mid-windup.
			enemies[0].prio = 100
		}
		activeAttackers := 0
		for _, e := range enemies {
			e.tree.Tick(dt)
			if pool.HasToken(e.id) {
				activeAttackers++
			}
		}
		if activeAttackers > maxSimultaneous {
			maxSimultaneous = activeAttackers
		}
		if activeAttackers > 2 {
			t.Fatalf("step %d: %d enemies held attack tokens simultaneously, want <= 2", step, activeAttackers)
		}
		pool.Step(dt)
		assertPoolInvariant(t, pool)
	}

	if maxSimultaneous != 2 {
		t.Fatalf("max simultaneous attackers = %d, want 2", maxSimultaneous)
	}
	if enemies[0].attacked == 0 {
		t.Fatalf("boosted enemy 1 never completed an attack after preempting")
	}
	if enemies[1].circled == 0 {
		t.Fatalf("non-attacking enemy should have fallen back to circling")
	}

	// HasAttackToken condition node reflects pool state accurately.
	hasNode := ai.HasAttackToken(pool, 99)
	if hasNode.Tick(dt) != ai.StatusFailure {
		t.Fatalf("HasAttackToken for non-holder should fail")
	}

	// Resetting a tree cancels/releases its token.
	for _, e := range enemies {
		e.tree.Reset()
	}
	if pool.HeldCount() != 0 || pool.PendingCount() != 0 {
		t.Fatalf("after tree Reset: held=%d pending=%d, want 0 and 0", pool.HeldCount(), pool.PendingCount())
	}
}

func segmentsProperlyIntersect(a1, a2, b1, b2 core.Point) bool {
	orient := func(p, q, r core.Point) float64 {
		return float64(q.X-p.X)*float64(r.Y-p.Y) - float64(q.Y-p.Y)*float64(r.X-p.X)
	}
	const eps = 1e-4
	o1 := orient(a1, a2, b1)
	o2 := orient(a1, a2, b2)
	o3 := orient(b1, b2, a1)
	o4 := orient(b1, b2, a2)
	return ((o1 > eps && o2 < -eps) || (o1 < -eps && o2 > eps)) &&
		((o3 > eps && o4 < -eps) || (o3 < -eps && o4 > eps))
}

func TestSlotAssignerNonCrossingAndRingPriority(t *testing.T) {
	assigner := ai.NewSlotAssigner(
		ai.EngagementRing{Radius: 40, Slots: 4},
		ai.EngagementRing{Radius: 85, Slots: 6},
	)
	if assigner.TotalSlots() != 10 || len(assigner.Rings()) != 2 {
		t.Fatalf("unexpected ring setup: total=%d rings=%d", assigner.TotalSlots(), len(assigner.Rings()))
	}

	target := core.Point{X: 100, Y: 100}

	// 1. Six agents around target: inner ring (4 slots) must fill completely, remaining 2 on outer ring,
	// and zero straight-line agent->slot paths may cross.
	agents := []ai.SlotAgent{
		{ID: 6, Pos: core.Point{X: 190, Y: 110}},
		{ID: 1, Pos: core.Point{X: 20, Y: 90}},
		{ID: 4, Pos: core.Point{X: 95, Y: 20}},
		{ID: 2, Pos: core.Point{X: 115, Y: 195}},
		{ID: 5, Pos: core.Point{X: 170, Y: 170}},
		{ID: 3, Pos: core.Point{X: 35, Y: 165}},
	}
	assignments := assigner.Assign(nil, target, agents)
	if len(assignments) != len(agents) {
		t.Fatalf("got %d assignments, want %d", len(assignments), len(agents))
	}

	agentPos := make(map[uint64]core.Point, len(agents))
	for _, ag := range agents {
		agentPos[ag.ID] = ag.Pos
	}

	ring0Count, ring1Count := 0, 0
	for i, a := range assignments {
		if !a.Assigned {
			t.Fatalf("agent %d unassigned when slots available", a.AgentID)
		}
		if i > 0 && assignments[i-1].AgentID >= a.AgentID {
			t.Fatalf("assignments not sorted by ascending AgentID: %+v", assignments)
		}
		switch a.Ring {
		case 0:
			ring0Count++
		case 1:
			ring1Count++
		default:
			t.Fatalf("unexpected ring %d", a.Ring)
		}
	}
	if ring0Count != 4 || ring1Count != 2 {
		t.Fatalf("ring counts = (%d, %d), want (4, 2)", ring0Count, ring1Count)
	}

	for i := 0; i < len(assignments); i++ {
		for j := i + 1; j < len(assignments); j++ {
			a := assignments[i]
			b := assignments[j]
			if segmentsProperlyIntersect(agentPos[a.AgentID], a.Pos, agentPos[b.AgentID], b.Pos) {
				t.Fatalf("paths crossed: agent %d (%v->%v) and agent %d (%v->%v)",
					a.AgentID, agentPos[a.AgentID], a.Pos, b.AgentID, agentPos[b.AgentID], b.Pos)
			}
		}
	}

	// 2. More agents than slots (n > m): optimal m agents assigned without crossing, extras unassigned.
	small := ai.NewSlotAssigner(ai.EngagementRing{Radius: 30, Slots: 3})
	crowd := []ai.SlotAgent{
		{ID: 1, Pos: core.Point{X: 140, Y: 100}},
		{ID: 2, Pos: core.Point{X: 60, Y: 130}},
		{ID: 3, Pos: core.Point{X: 65, Y: 70}},
		{ID: 4, Pos: core.Point{X: 400, Y: 400}},
		{ID: 5, Pos: core.Point{X: -300, Y: -300}},
	}
	res := small.Assign(nil, target, crowd)
	assignedCount := 0
	crowdPos := make(map[uint64]core.Point, len(crowd))
	for _, c := range crowd {
		crowdPos[c.ID] = c.Pos
	}
	var active []ai.SlotAssignment
	for _, r := range res {
		if r.Assigned {
			assignedCount++
			active = append(active, r)
		} else if r.Ring != -1 || r.Slot != -1 {
			t.Fatalf("unassigned agent should have Ring=-1, Slot=-1, got %+v", r)
		}
	}
	if assignedCount != 3 {
		t.Fatalf("assigned %d of 5 agents to 3 slots, want 3", assignedCount)
	}
	for _, r := range res {
		if (r.AgentID == 4 || r.AgentID == 5) && r.Assigned {
			t.Fatalf("distant agent %d should remain unassigned when closer agents fill slots", r.AgentID)
		}
	}
	for i := 0; i < len(active); i++ {
		for j := i + 1; j < len(active); j++ {
			if segmentsProperlyIntersect(crowdPos[active[i].AgentID], active[i].Pos, crowdPos[active[j].AgentID], active[j].Pos) {
				t.Fatalf("n>m paths crossed between %+v and %+v", active[i], active[j])
			}
		}
	}
}

func TestSlotAssignerMovingTargetStabilityAndDeterminism(t *testing.T) {
	simulate := func(reverseInput bool) ([][]ai.SlotAssignment, int) {
		assigner := ai.NewSlotAssigner(
			ai.EngagementRing{Radius: 45, Slots: 4},
			ai.EngagementRing{Radius: 85, Slots: 4},
		)
		assigner.StayBias = 12.0

		// 6 agents initially arranged around (0, 0) with persistent steering state so they keep up
		// with the moving target (target ~90 u/s vs agent MaxSpeed 180 u/s).
		steering := []ai.Agent{
			{Pos: core.Point{X: 55, Y: 5}, MaxSpeed: 180, MaxForce: 600},
			{Pos: core.Point{X: -5, Y: 55}, MaxSpeed: 180, MaxForce: 600},
			{Pos: core.Point{X: -55, Y: -5}, MaxSpeed: 180, MaxForce: 600},
			{Pos: core.Point{X: 5, Y: -55}, MaxSpeed: 180, MaxForce: 600},
			{Pos: core.Point{X: 90, Y: 30}, MaxSpeed: 180, MaxForce: 600},
			{Pos: core.Point{X: -90, Y: -30}, MaxSpeed: 180, MaxForce: 600},
		}
		agents := make([]ai.SlotAgent, len(steering))
		for i := range steering {
			agents[i] = ai.SlotAgent{ID: uint64(i + 1), Pos: steering[i].Pos}
		}
		dt := 50 * time.Millisecond
		var history [][]ai.SlotAssignment
		var prev []ai.SlotAssignment
		swaps := 0
		var buf []ai.SlotAssignment

		for step := range 40 {
			// Target moves smoothly along a curved path (~90 u/s).
			angle := float64(step) * 0.08
			target := core.Point{
				X: core.Points(float64(step) * 4.5),
				Y: core.Points(math.Sin(angle) * 25.0),
			}

			input := slices.Clone(agents)
			if reverseInput && step%2 == 1 {
				slices.Reverse(input)
			}
			buf = assigner.Assign(buf, target, input)
			snap := slices.Clone(buf)
			history = append(history, snap)

			if step > 0 {
				for i := range snap {
					if snap[i].Ring != prev[i].Ring || snap[i].Slot != prev[i].Slot {
						swaps++
					}
				}
			}
			prev = snap

			// Advance each agent toward its assigned slot with persistent steering.
			for i := range steering {
				asgn, ok := assigner.Assignment(agents[i].ID)
				if !ok || !asgn.Assigned {
					continue
				}
				force := ai.Arrive(steering[i], asgn.Pos, 10)
				steering[i].Step(force, dt)
				agents[i].Pos = steering[i].Pos
			}
		}
		return history, swaps
	}

	runA, swapsA := simulate(false)
	runB, swapsB := simulate(true)

	if swapsA != swapsB {
		t.Fatalf("swap counts differed across runs: %d vs %d", swapsA, swapsB)
	}
	if swapsA > 0 {
		t.Fatalf("expected stable moving-target slot assignment with 0 swaps, got %d swaps", swapsA)
	}
	for step := range runA {
		if !slices.Equal(runA[step], runB[step]) {
			t.Fatalf("step %d assignment differed between runs:\nA: %+v\nB: %+v", step, runA[step], runB[step])
		}
	}
}

func TestSlotAssignerHysteresisEdgeCasesAndAllocs(t *testing.T) {
	// Hysteresis suppresses boundary jitter swaps.
	assigner := ai.NewSlotAssigner(ai.EngagementRing{Radius: 50, Slots: 2})
	assigner.StayBias = 15.0

	target := core.Point{X: 0, Y: 0}
	// Slot 0 is at (+50, 0), Slot 1 is at (-50, 0).
	// Agents sit slightly past x=0 on opposite sides, then target shifts slightly so distances barely invert.
	a1 := []ai.SlotAgent{
		{ID: 1, Pos: core.Point{X: 2, Y: 20}},
		{ID: 2, Pos: core.Point{X: -2, Y: -20}},
	}
	first := assigner.Assign(nil, target, a1)
	if first[0].Slot != 0 || first[1].Slot != 1 {
		t.Fatalf("initial slots = (%d, %d), want (0, 1)", first[0].Slot, first[1].Slot)
	}

	// Shift target slightly right by 4 units (difference < StayBias): agents keep their slots.
	second := assigner.Assign(nil, core.Point{X: 4, Y: 0}, a1)
	if second[0].Slot != 0 || second[1].Slot != 1 {
		t.Fatalf("expected StayBias to keep slots (0, 1), got (%d, %d)", second[0].Slot, second[1].Slot)
	}

	// Release clears stability memory; Reset clears all cached assignments.
	assigner.Release(1)
	if _, ok := assigner.Assignment(1); ok {
		t.Fatalf("Assignment(1) should be absent after Release")
	}
	assigner.Reset()
	if _, ok := assigner.Assignment(2); ok {
		t.Fatalf("Assignment(2) should be absent after Reset")
	}

	// Invalid rings filtered out; empty agents and zero slots handled cleanly.
	empty := ai.NewSlotAssigner(
		ai.EngagementRing{Radius: -10, Slots: 4},
		ai.EngagementRing{Radius: 20, Slots: 0},
		ai.EngagementRing{Radius: float32(math.NaN()), Slots: 2},
	)
	if empty.TotalSlots() != 0 {
		t.Fatalf("expected 0 valid slots, got %d", empty.TotalSlots())
	}
	none := empty.Assign(nil, target, []ai.SlotAgent{{ID: 1, Pos: core.Point{X: 1, Y: 2}}})
	if len(none) != 1 || none[0].Assigned || none[0].Ring != -1 || none[0].Slot != -1 {
		t.Fatalf("unexpected zero-slot assignment: %+v", none)
	}
	if len(empty.Assign(nil, target, nil)) != 0 {
		t.Fatalf("empty agents should yield empty assignments")
	}

	// Zero heap allocations after warm-up for both n <= m and n > m.
	warm := ai.NewSlotAssigner(
		ai.EngagementRing{Radius: 40, Slots: 4},
		ai.EngagementRing{Radius: 80, Slots: 4},
	)
	dst := make([]ai.SlotAssignment, 0, 10)
	six := []ai.SlotAgent{
		{ID: 1, Pos: core.Point{X: 40, Y: 0}},
		{ID: 2, Pos: core.Point{X: 0, Y: 40}},
		{ID: 3, Pos: core.Point{X: -40, Y: 0}},
		{ID: 4, Pos: core.Point{X: 0, Y: -40}},
		{ID: 5, Pos: core.Point{X: 80, Y: 0}},
		{ID: 6, Pos: core.Point{X: -80, Y: 0}},
	}
	dst = warm.Assign(dst, target, six)
	if allocs := testing.AllocsPerRun(50, func() {
		dst = warm.Assign(dst, target, six)
	}); allocs != 0 {
		t.Fatalf("SlotAssigner.Assign (n<=m) allocs = %v, want 0", allocs)
	}

	ten := append(slices.Clone(six),
		ai.SlotAgent{ID: 7, Pos: core.Point{X: 120, Y: 10}},
		ai.SlotAgent{ID: 8, Pos: core.Point{X: -120, Y: 10}},
		ai.SlotAgent{ID: 9, Pos: core.Point{X: 10, Y: 120}},
		ai.SlotAgent{ID: 10, Pos: core.Point{X: 10, Y: -120}},
	)
	dst = warm.Assign(dst, target, ten)
	if allocs := testing.AllocsPerRun(50, func() {
		dst = warm.Assign(dst, target, ten)
	}); allocs != 0 {
		t.Fatalf("SlotAssigner.Assign (n>m) allocs = %v, want 0", allocs)
	}
}

func TestSquadRosterStateRolesAndOrders(t *testing.T) {
	sq := ai.NewSquad(7, 30, 10, 20, 10)
	if sq.ID() != 7 || sq.MemberCount() != 3 {
		t.Fatalf("ID=%d MemberCount=%d, want 7 and 3", sq.ID(), sq.MemberCount())
	}
	if !slices.Equal(sq.Members(nil), []uint64{10, 20, 30}) {
		t.Fatalf("Members = %v, want [10 20 30]", sq.Members(nil))
	}
	if sq.AddMember(20) {
		t.Fatalf("duplicate AddMember should return false")
	}
	if !sq.HasMember(10) || sq.HasMember(99) {
		t.Fatalf("HasMember mismatch")
	}

	// Shared SquadState typed accessors and deterministic Keys.
	st := sq.State()
	st.SetBool("alerted", true)
	st.SetInt("wave", 3)
	st.SetFloat("threat", 0.85)
	st.SetString("sector", "north")
	st.SetPoint("last_seen", core.Point{X: 120, Y: 45})
	st.SetID("primary_target", 42)
	if st.Len() != 6 || !st.Has("alerted") {
		t.Fatalf("SquadState Len=%d Has=%v", st.Len(), st.Has("alerted"))
	}
	if !slices.Equal(st.Keys(nil), []string{"alerted", "last_seen", "primary_target", "sector", "threat", "wave"}) {
		t.Fatalf("Keys not sorted: %v", st.Keys(nil))
	}
	if v, ok := st.Bool("alerted"); !ok || !v {
		t.Fatalf("Bool failed")
	}
	if v, ok := st.Int("wave"); !ok || v != 3 {
		t.Fatalf("Int failed")
	}
	if v, ok := st.Float("threat"); !ok || v != 0.85 {
		t.Fatalf("Float failed")
	}
	if v, ok := st.String("sector"); !ok || v != "north" {
		t.Fatalf("String failed")
	}
	if v, ok := st.Point("last_seen"); !ok || v != (core.Point{X: 120, Y: 45}) {
		t.Fatalf("Point failed")
	}
	if v, ok := st.ID("primary_target"); !ok || v != 42 {
		t.Fatalf("ID failed")
	}
	if _, ok := ai.SquadStateGet[int](st, "sector"); ok {
		t.Fatalf("mismatched SquadStateGet type should return false")
	}
	if _, ok := ai.SquadStateGet[int](nil, "wave"); ok {
		t.Fatalf("nil SquadStateGet should return false")
	}
	st.Delete("wave")
	if st.Has("wave") {
		t.Fatalf("Delete failed")
	}

	// Scored role assignment: non-greedy global optimum fills both roles even when agent 10 is best at flanker
	// and also the only eligible suppressor.
	assigned := sq.AssignRoles(
		ai.SquadRoleSpec{
			Name:  "flanker",
			Count: 1,
			Score: func(id uint64) float64 {
				switch id {
				case 10:
					return 10.0
				case 20:
					return 9.0
				default:
					return -5.0
				}
			},
		},
		ai.SquadRoleSpec{
			Name:  "suppressor",
			Count: 1,
			Score: func(id uint64) float64 {
				if id == 10 {
					return 8.0
				}
				return math.NaN() // 20 and 30 cannot suppress
			},
		},
		ai.SquadRoleSpec{
			Name:  "scout",
			Count: 1,
			Score: func(id uint64) float64 {
				if id == 30 {
					return -12.0 // negative finite score is still eligible
				}
				return math.Inf(-1)
			},
		},
	)
	if assigned != 3 {
		t.Fatalf("assigned = %d, want 3", assigned)
	}
	if sq.Role(10) != "suppressor" || sq.Role(20) != "flanker" || sq.Role(30) != "scout" {
		t.Fatalf("roles = (10:%q, 20:%q, 30:%q), want suppressor, flanker, scout",
			sq.Role(10), sq.Role(20), sq.Role(30))
	}
	if !sq.HasRole(20, "flanker") || sq.HasRole(20, "") {
		t.Fatalf("HasRole mismatch")
	}
	if !slices.Equal(sq.MembersWithRole(nil, "flanker"), []uint64{20}) {
		t.Fatalf("MembersWithRole(flanker) = %v", sq.MembersWithRole(nil, "flanker"))
	}

	// Extreme finite float64 scores do not overflow.
	extremeAssigned := sq.AssignRoles(
		ai.SquadRoleSpec{
			Name:  "alpha",
			Count: 1,
			Score: func(id uint64) float64 {
				if id == 10 {
					return 1e308
				}
				return -1e308
			},
		},
	)
	if extremeAssigned != 1 || sq.Role(10) != "alpha" {
		t.Fatalf("extreme score assignment failed: assigned=%d role(10)=%q", extremeAssigned, sq.Role(10))
	}

	// Manual SetRole, RemoveMember clearing role, ClearRoles.
	if !sq.SetRole(20, "medic") || sq.SetRole(999, "medic") {
		t.Fatalf("SetRole behavior unexpected")
	}
	if !sq.RemoveMember(20) || sq.Role(20) != "" {
		t.Fatalf("RemoveMember should clear role")
	}
	sq.ClearRoles()
	if sq.Role(10) != "" {
		t.Fatalf("ClearRoles failed")
	}
}

func TestSquadOrdersAndBehaviorTreeIntegration(t *testing.T) {
	sq := ai.NewSquad(1, 1, 2)
	sq.AssignRoles(
		ai.SquadRoleSpec{Name: "flanker", Count: 1, Score: func(id uint64) float64 {
			if id == 1 {
				return 10
			}
			return 1
		}},
		ai.SquadRoleSpec{Name: "suppressor", Count: 1, Score: func(id uint64) float64 {
			if id == 2 {
				return 10
			}
			return 1
		}},
	)

	var flankOrders, suppressOrders []ai.SquadOrder
	buildTree := func(id uint64) ai.Node {
		return ai.Selector(
			ai.Sequence(
				ai.SquadStateBool(sq, "combat_active"),
				ai.SquadHasOrder(sq, "pinch"),
				ai.SquadHasRole(sq, id, "flanker"),
				ai.SquadOrderAction(sq, func(ord ai.SquadOrder, _ time.Duration) ai.Status {
					flankOrders = append(flankOrders, ord)
					return ai.StatusSuccess
				}),
			),
			ai.Sequence(
				ai.SquadStateBool(sq, "combat_active"),
				ai.SquadHasOrder(sq, ""),
				ai.SquadHasRole(sq, id, "suppressor"),
				ai.SquadOrderAction(sq, func(ord ai.SquadOrder, _ time.Duration) ai.Status {
					suppressOrders = append(suppressOrders, ord)
					return ai.StatusSuccess
				}),
			),
		)
	}

	tree1 := buildTree(1)
	tree2 := buildTree(2)
	dt := 16 * time.Millisecond

	// Before combat_active and order, both trees fail.
	if tree1.Tick(dt) != ai.StatusFailure || tree2.Tick(dt) != ai.StatusFailure {
		t.Fatalf("trees should fail without active state and order")
	}

	sq.State().SetBool("combat_active", true)
	ord1 := sq.SetOrder(ai.SquadOrder{
		Kind:     "pinch",
		TargetID: 99,
		Position: core.Point{X: 200, Y: 100},
	})
	if ord1.Revision != 1 {
		t.Fatalf("first order Revision = %d, want 1", ord1.Revision)
	}

	if tree1.Tick(dt) != ai.StatusSuccess || tree2.Tick(dt) != ai.StatusSuccess {
		t.Fatalf("both members should execute their role branches on pinch order")
	}
	if len(flankOrders) != 1 || flankOrders[0] != ord1 {
		t.Fatalf("flanker received %+v, want %+v", flankOrders, ord1)
	}
	if len(suppressOrders) != 1 || suppressOrders[0] != ord1 {
		t.Fatalf("suppressor received %+v, want %+v", suppressOrders, ord1)
	}

	// Re-issuing same order kind increments Revision.
	ord2 := sq.SetOrder(ai.SquadOrder{Kind: "pinch", TargetID: 99, Position: core.Point{X: 250, Y: 120}})
	if ord2.Revision != 2 {
		t.Fatalf("second order Revision = %d, want 2", ord2.Revision)
	}

	// Clearing the order increments Revision and causes order-guarded BT branches to fail.
	sq.ClearOrder()
	if sq.Order().Kind != "" || sq.Order().Revision != 3 {
		t.Fatalf("ClearOrder produced %+v, want empty kind and revision 3", sq.Order())
	}
	if tree1.Tick(dt) != ai.StatusFailure || tree2.Tick(dt) != ai.StatusFailure {
		t.Fatalf("trees should fail after ClearOrder")
	}
}
