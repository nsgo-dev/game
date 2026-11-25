package ai_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/game/packages/game/statedump"
)

func TestBTInspectorReferenceTreeStatusesAndRunningPath(t *testing.T) {
	inRange := false
	hasPath := true
	swings := 0

	tree := ai.Named("root", ai.Selector(
		ai.Named("combat_guard", ai.Guard(func() bool { return inRange },
			ai.Named("attack_seq", ai.Sequence(
				ai.Named("in_range", ai.Condition(func() bool { return inRange })),
				ai.Named("swing_cd", ai.Cooldown(500*time.Millisecond,
					ai.Named("swing", ai.Action(func(time.Duration) ai.Status {
						swings++
						return ai.StatusSuccess
					})),
				)),
				ai.Named("recover", ai.Wait(200*time.Millisecond)),
			)),
		)),
		ai.Named("patrol_seq", ai.Sequence(
			ai.Named("has_path", ai.Condition(func() bool { return hasPath })),
			ai.Named("patrol", ai.Action(func(time.Duration) ai.Status {
				return ai.StatusRunning
			})),
		)),
	))

	ins := ai.NewBTInspector(tree)
	if ins.Len() != 10 {
		t.Fatalf("Len() = %d, want 10", ins.Len())
	}
	if ins.Root() != tree {
		t.Fatal("Root() did not return original root node")
	}

	// Before any tick, every node is Idle and inactive.
	traces := ins.Walk()
	for _, tr := range traces {
		if tr.Ticked || tr.Active || tr.StatusString() != "Idle" {
			t.Fatalf("before tick: node %s (%q) = %+v, want Idle/inactive", tr.Path, tr.Name, tr)
		}
	}
	if got := ins.RunningPath(); len(got) != 0 {
		t.Fatalf("before tick RunningPath() = %+v, want empty", got)
	}

	// Tick 1: player not in range -> combat_guard fails, patrol runs.
	if st := tree.Tick(100 * time.Millisecond); st != ai.StatusRunning {
		t.Fatalf("tick 1 status = %v, want Running", st)
	}
	traces = ins.Walk()
	byName := indexTracesByName(traces)
	assertNodeState(t, byName["root"], "root", 0, "selector", ai.StatusRunning, true, true)
	assertNodeState(t, byName["combat_guard"], "root/0", 1, "guard", ai.StatusFailure, true, false)
	assertNodeState(t, byName["attack_seq"], "root/0/0", 2, "sequence", 0, false, false)
	assertNodeState(t, byName["patrol_seq"], "root/1", 1, "sequence", ai.StatusRunning, true, true)
	assertNodeState(t, byName["has_path"], "root/1/0", 2, "condition", ai.StatusSuccess, true, false)
	assertNodeState(t, byName["patrol"], "root/1/1", 2, "action", ai.StatusRunning, true, true)

	assertRunningPathNames(t, ins.RunningPath(), []string{"root", "patrol_seq", "patrol"})

	// Tick 2: player enters range -> combat_guard preempts patrol_seq (resetting it to Idle)
	// and pauses on recover Wait.
	inRange = true
	if st := tree.Tick(100 * time.Millisecond); st != ai.StatusRunning {
		t.Fatalf("tick 2 status = %v, want Running", st)
	}
	if swings != 1 {
		t.Fatalf("swings = %d, want 1", swings)
	}
	traces = ins.Walk()
	byName = indexTracesByName(traces)
	assertNodeState(t, byName["root"], "root", 0, "selector", ai.StatusRunning, true, true)
	assertNodeState(t, byName["combat_guard"], "root/0", 1, "guard", ai.StatusRunning, true, true)
	assertNodeState(t, byName["attack_seq"], "root/0/0", 2, "sequence", ai.StatusRunning, true, true)
	assertNodeState(t, byName["in_range"], "root/0/0/0", 3, "condition", ai.StatusSuccess, true, false)
	assertNodeState(t, byName["swing_cd"], "root/0/0/1", 3, "cooldown", ai.StatusSuccess, true, false)
	assertNodeState(t, byName["swing"], "root/0/0/1/0", 4, "action", ai.StatusSuccess, true, false)
	assertNodeState(t, byName["recover"], "root/0/0/2", 3, "wait", ai.StatusRunning, true, true)
	// Preempted branch was Reset(), so it reports Idle.
	assertNodeState(t, byName["patrol_seq"], "root/1", 1, "sequence", 0, false, false)
	assertNodeState(t, byName["patrol"], "root/1/1", 2, "action", 0, false, false)

	assertRunningPathNames(t, ins.RunningPath(), []string{"root", "combat_guard", "attack_seq", "recover"})

	// Tick 3: recover Wait finishes -> whole tree returns Success, no active running path.
	if st := tree.Tick(100 * time.Millisecond); st != ai.StatusSuccess {
		t.Fatalf("tick 3 status = %v, want Success", st)
	}
	traces = ins.Walk()
	byName = indexTracesByName(traces)
	assertNodeState(t, byName["root"], "root", 0, "selector", ai.StatusSuccess, true, false)
	assertNodeState(t, byName["recover"], "root/0/0/2", 3, "wait", ai.StatusSuccess, true, false)
	if got := ins.RunningPath(); len(got) != 0 {
		t.Fatalf("after completion RunningPath() = %+v, want empty", got)
	}

	// Reset clears every node's trace back to Idle.
	ins.Reset()
	for _, tr := range ins.Walk() {
		if tr.Ticked || tr.Active {
			t.Fatalf("after Reset: node %q = %+v, want Idle/inactive", tr.Name, tr)
		}
	}
}

type customRunningNode struct {
	status ai.Status
}

func (c *customRunningNode) Tick(time.Duration) ai.Status { return c.status }
func (c *customRunningNode) Reset()                       {}

func TestBTInspectorAllBuiltinKindsAndCustomNodes(t *testing.T) {
	t.Run("ParallelAndDecorators", func(t *testing.T) {
		rng := random.New(7)
		pool := ai.NewTokenPool(ai.TokenPoolConfig{Capacity: 2})
		pool.Request(1, 1)
		pool.Step(0)
		bb := ai.NewBlackboard()
		bb.SetBool("alert", true)

		reasoner := ai.NewReasoner(ai.UtilityAction[int]{
			Name:   "hold",
			Weight: 1,
			Considerations: []ai.Consideration[int]{{
				Name:  "always",
				Input: func(int) float64 { return 1 },
				Curve: ai.LinearCurve(1, 0, 0),
			}},
		})

		customBare := &customRunningNode{status: ai.StatusRunning}
		customNamed := &customRunningNode{status: ai.StatusRunning}

		tree := ai.Named("par", ai.Parallel(ai.RequireAll,
			ai.Named("inv", ai.Inverter(ai.Named("tl", ai.TimeLimit(time.Second, customBare)))),
			ai.Named("svc", ai.Service(50*time.Millisecond, func(time.Duration) {},
				ai.Named("tok", ai.WithAttackToken(pool, 1, nil,
					ai.Named("custom_named", customNamed),
				)),
			)),
			ai.Named("util", ai.UtilitySelector(reasoner, func() int { return 1 }, rng, map[string]ai.Node{
				"hold": ai.Named("wait_hold", ai.Wait(time.Second)),
			})),
			ai.Named("bb_dec", ai.BlackboardCondition(bb, "alert", ai.BBOpEq, ai.BBBool(true), ai.AbortSelf,
				ai.Named("rand_sel", ai.RandomSelector(rng,
					ai.Named("r_seq", ai.RandomSequence(rng,
						ai.Named("w_sel", ai.WeightedSelector(rng,
							ai.WeightedNode{Node: ai.Named("retry_n", ai.Retry(2, ai.Named("rep", ai.Repeater(3, ai.Named("uf", ai.UntilFail(ai.Named("succ", ai.Succeeder(ai.Named("fail", ai.Failer(ai.Wait(time.Second))))))))))), Weight: 1},
						)),
					)),
				)),
			)),
		))

		ins := ai.NewBTInspector(tree)
		if st := tree.Tick(16 * time.Millisecond); st != ai.StatusRunning {
			t.Fatalf("parallel status = %v, want Running", st)
		}
		traces := ins.Walk()
		byName := indexTracesByName(traces)

		wantKinds := map[string]string{
			"par":          "parallel",
			"inv":          "inverter",
			"tl":           "time_limit",
			"svc":          "service",
			"tok":          "attack_token",
			"custom_named": "custom",
			"util":         "utility_selector",
			"wait_hold":    "wait",
			"bb_dec":       "blackboard_condition",
			"rand_sel":     "random_selector",
			"r_seq":        "random_sequence",
			"w_sel":        "weighted_selector",
			"retry_n":      "retry",
			"rep":          "repeater",
			"uf":           "until_fail",
			"succ":         "succeeder",
			"fail":         "failer",
		}
		for name, wantKind := range wantKinds {
			tr, ok := byName[name]
			if !ok {
				t.Fatalf("missing node %q in trace", name)
			}
			if tr.Kind != wantKind {
				t.Fatalf("node %q kind = %q, want %q", name, tr.Kind, wantKind)
			}
			if !tr.Active || tr.LastStatus != ai.StatusRunning {
				t.Fatalf("node %q = %+v, want Active Running", name, tr)
			}
		}

		// Bare custom child under TimeLimit is also detected as Active Running.
		var sawBareCustom bool
		for _, tr := range traces {
			if tr.Kind == "custom" && tr.Name == "" && tr.Active && tr.LastStatus == ai.StatusRunning {
				sawBareCustom = true
				break
			}
		}
		if !sawBareCustom {
			t.Fatalf("bare custom node not marked Active Running in traces: %+v", traces)
		}
	})

	t.Run("NamedBlackboardObserverPreemptInSequence", func(t *testing.T) {
		bb := ai.NewBlackboard()
		bb.SetBool("ready", false)
		bb.SetBool("ammo", false)

		patrolTicks := 0
		shootTicks := 0

		tree := ai.Named("root", ai.Selector(
			ai.Named("combat", ai.Sequence(
				ai.Named("check_ready", ai.BlackboardCondition(bb, "ready", ai.BBOpEq, ai.BBBool(true), ai.AbortLower, nil)),
				ai.Named("check_ammo", ai.BlackboardCondition(bb, "ammo", ai.BBOpEq, ai.BBBool(true), ai.AbortLower, nil)),
				ai.Named("shoot", ai.Action(func(time.Duration) ai.Status {
					shootTicks++
					return ai.StatusRunning
				})),
			)),
			ai.Named("patrol", ai.Action(func(time.Duration) ai.Status {
				patrolTicks++
				return ai.StatusRunning
			})),
		))

		ins := ai.NewBTInspector(tree)
		tree.Tick(16 * time.Millisecond)
		assertRunningPathNames(t, ins.RunningPath(), []string{"root", "patrol"})

		// Changing only ammo while ready is still false must NOT preempt patrol
		// (tests unwrapNamed in sequenceNode.bbWantsPreempt).
		bb.SetBool("ammo", true)
		tree.Tick(16 * time.Millisecond)
		if shootTicks != 0 || patrolTicks != 2 {
			t.Fatalf("unexpected preemption: shootTicks=%d patrolTicks=%d, want 0 and 2", shootTicks, patrolTicks)
		}

		// Now ready becomes true -> combat preempts patrol!
		bb.SetBool("ready", true)
		tree.Tick(16 * time.Millisecond)
		if shootTicks != 1 {
			t.Fatalf("expected preemption when ready became true: shootTicks=%d", shootTicks)
		}
		assertRunningPathNames(t, ins.RunningPath(), []string{"root", "combat", "shoot"})
	})

	t.Run("BareCustomRootViaInspectorTick", func(t *testing.T) {
		root := &customRunningNode{status: ai.StatusRunning}
		ins := ai.NewBTInspector(root)
		if st := ins.Tick(16 * time.Millisecond); st != ai.StatusRunning {
			t.Fatalf("ins.Tick = %v, want Running", st)
		}
		rp := ins.RunningPath()
		if len(rp) != 1 || rp[0].Kind != "custom" || !rp[0].Active || rp[0].LastStatus != ai.StatusRunning {
			t.Fatalf("bare custom root RunningPath = %+v", rp)
		}
	})
}

func TestBTInspectorYAMLNamesPreserved(t *testing.T) {
	const src = `version: 1
root:
  type: selector
  name: guard_ai
  children:
    - type: sequence
      name: attack_branch
      children:
        - type: condition
          name: sees_enemy
          condition: sees_enemy
        - type: action
          name: strike
          action: strike
    - type: wait
      name: idle_wait
      duration: 500ms
`
	doc, err := ai.ParseBehaviorTree([]byte(src))
	if err != nil {
		t.Fatalf("ParseBehaviorTree: %v", err)
	}
	sees := false
	tree, err := ai.BuildBehaviorTree(doc, ai.BTBindings{
		Conditions: map[string]func(ai.BTNodeContext) (func() bool, error){
			"sees_enemy": func(ai.BTNodeContext) (func() bool, error) {
				return func() bool { return sees }, nil
			},
		},
		Actions: map[string]func(ai.BTNodeContext) (ai.Node, error){
			"strike": func(ai.BTNodeContext) (ai.Node, error) {
				return ai.Action(func(time.Duration) ai.Status { return ai.StatusRunning }), nil
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildBehaviorTree: %v", err)
	}

	ins := ai.NewBTInspector(tree)
	tree.Tick(100 * time.Millisecond)
	assertRunningPathNames(t, ins.RunningPath(), []string{"guard_ai", "idle_wait"})

	sees = true
	tree.Tick(100 * time.Millisecond)
	assertRunningPathNames(t, ins.RunningPath(), []string{"guard_ai", "attack_branch", "strike"})
}

func TestBTInspectorZeroAllocsOnceWarmedUp(t *testing.T) {
	inRange := false
	tree := ai.Named("root", ai.Selector(
		ai.Named("attack", ai.Sequence(
			ai.Named("in_range", ai.Condition(func() bool { return inRange })),
			ai.Named("swing", ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess })),
		)),
		ai.Named("patrol", ai.Action(func(time.Duration) ai.Status { return ai.StatusRunning })),
	))

	ins := ai.NewBTInspector(tree)
	traceBuf := make([]ai.BTNodeTrace, 0, ins.Len())
	pathBuf := make([]ai.BTNodeTrace, 0, ins.Len())

	// Warm up.
	tree.Tick(16 * time.Millisecond)
	traceBuf = ins.WalkInto(traceBuf)
	pathBuf = ai.RunningPathInto(pathBuf, traceBuf)
	if len(pathBuf) != 2 {
		t.Fatalf("warm-up running path len = %d, want 2", len(pathBuf))
	}

	allocs := testing.AllocsPerRun(200, func() {
		tree.Tick(16 * time.Millisecond)
		traceBuf = ins.WalkInto(traceBuf)
		pathBuf = ai.RunningPathInto(pathBuf, traceBuf)
	})
	if allocs != 0 {
		t.Fatalf("Tick + WalkInto + RunningPathInto allocated %.2f times/run, want 0", allocs)
	}
}

func TestBTInspectorFormatDeterministicAndStatedumpDiff(t *testing.T) {
	buildRun := func(inRange bool) []byte {
		tree := ai.Named("root", ai.Selector(
			ai.Named("attack", ai.Sequence(
				ai.Named("in_range", ai.Condition(func() bool { return inRange })),
				ai.Named("swing", ai.Action(func(time.Duration) ai.Status { return ai.StatusRunning })),
			)),
			ai.Named("patrol", ai.Action(func(time.Duration) ai.Status { return ai.StatusRunning })),
		))
		ins := ai.NewBTInspector(tree)
		tree.Tick(16 * time.Millisecond)

		dump := &statedump.Dump{Version: statedump.FormatVersion, Seed: 42, Rate: 60, Steps: 1}
		if err := dump.SetValue("guard_bt", ins.Format()); err != nil {
			t.Fatalf("SetValue: %v", err)
		}
		data, err := statedump.Encode(dump)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if _, err := statedump.Parse(data); err != nil {
			t.Fatalf("Parse round-trip failed: %v", err)
		}
		return data
	}

	runA1 := buildRun(false)
	runA2 := buildRun(false)
	if diff := statedump.Diff(runA1, runA2); diff != "" {
		t.Fatalf("identical runs differed:\n%s", diff)
	}

	runB := buildRun(true)
	diff := statedump.Diff(runA1, runB)
	if diff == "" {
		t.Fatal("expected non-empty statedump diff when BT branch changed")
	}
	if !strings.Contains(diff, `action "patrol" = Running`) || !strings.Contains(diff, `action "swing" = Running`) {
		t.Fatalf("diff missing expected BT branch lines:\n%s", diff)
	}
}

func indexTracesByName(traces []ai.BTNodeTrace) map[string]ai.BTNodeTrace {
	m := make(map[string]ai.BTNodeTrace, len(traces))
	for _, tr := range traces {
		if tr.Name != "" {
			m[tr.Name] = tr
		}
	}
	return m
}

func assertNodeState(t *testing.T, tr ai.BTNodeTrace, path string, depth int, kind string, status ai.Status, ticked, active bool) {
	t.Helper()
	if tr.Path != path || tr.Depth != depth || tr.Kind != kind || tr.Ticked != ticked || tr.Active != active || (ticked && tr.LastStatus != status) {
		t.Fatalf("node %q = %+v, want path=%q depth=%d kind=%q status=%v ticked=%v active=%v",
			tr.Name, tr, path, depth, kind, status, ticked, active)
	}
}

func assertRunningPathNames(t *testing.T, path []ai.BTNodeTrace, want []string) {
	t.Helper()
	if len(path) != len(want) {
		t.Fatalf("running path len = %d (%+v), want %d (%v)", len(path), path, len(want), want)
	}
	for i, w := range want {
		if path[i].Name != w {
			t.Fatalf("running path[%d].Name = %q, want %q (full: %+v)", i, path[i].Name, w, path)
		}
	}
}
