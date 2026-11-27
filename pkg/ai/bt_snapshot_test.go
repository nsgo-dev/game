package ai_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/random"
	"go.yaml.in/yaml/v3"
)

// customCounterNode is a custom SnapshotNode whose progress survives YAML & JSON round-trips.
type customCounterNode struct {
	kind    string
	target  int
	steps   int
	onTick  func(step int)
	failErr error
}

func (c *customCounterNode) Tick(time.Duration) ai.Status {
	c.steps++
	if c.onTick != nil {
		c.onTick(c.steps)
	}
	if c.steps >= c.target {
		c.steps = 0
		return ai.StatusSuccess
	}
	return ai.StatusRunning
}

func (c *customCounterNode) Reset() { c.steps = 0 }

func (c *customCounterNode) SnapshotKind() string { return c.kind }

func (c *customCounterNode) SnapshotState() (any, error) {
	return map[string]any{"steps": c.steps}, nil
}

func (c *customCounterNode) RestoreState(state any) error {
	if c.failErr != nil {
		return c.failErr
	}
	m, ok := state.(map[string]any)
	if !ok {
		return fmt.Errorf("customCounterNode: expected map, got %T", state)
	}
	var v int
	switch n := m["steps"].(type) {
	case int:
		v = n
	case float64:
		v = int(n)
	default:
		return fmt.Errorf("customCounterNode: invalid steps type %T", m["steps"])
	}
	if v < 0 || v >= c.target {
		return fmt.Errorf("customCounterNode: steps %d out of range [0, %d)", v, c.target)
	}
	c.steps = v
	return nil
}

// customWrapperNode implements both SnapshotNode and ChildNodes.
type customWrapperNode struct {
	child  ai.Node
	active bool
}

func (w *customWrapperNode) Tick(dt time.Duration) ai.Status {
	w.active = true
	st := w.child.Tick(dt)
	if st != ai.StatusRunning {
		w.active = false
	}
	return st
}

func (w *customWrapperNode) Reset() {
	w.active = false
	if w.child != nil {
		w.child.Reset()
	}
}

func (w *customWrapperNode) Children() []ai.Node         { return []ai.Node{w.child} }
func (w *customWrapperNode) SnapshotKind() string        { return "wrapper" }
func (w *customWrapperNode) SnapshotState() (any, error) { return w.active, nil }
func (w *customWrapperNode) RestoreState(state any) error {
	b, ok := state.(bool)
	if !ok {
		return fmt.Errorf("customWrapperNode: expected bool, got %T", state)
	}
	w.active = b
	return nil
}

func roundTripBTYAML(t *testing.T, snap ai.BehaviorTreeSnapshot) ai.BehaviorTreeSnapshot {
	t.Helper()
	b1, err := yaml.Marshal(snap)
	if err != nil {
		t.Fatalf("yaml.Marshal failed: %v", err)
	}
	var decoded ai.BehaviorTreeSnapshot
	if err := yaml.Unmarshal(b1, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}
	b2, err := yaml.Marshal(decoded)
	if err != nil {
		t.Fatalf("second yaml.Marshal failed: %v", err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("YAML round-trip changed bytes:\n--- first ---\n%s\n--- second ---\n%s", b1, b2)
	}
	return decoded
}

func roundTripBTJSON(t *testing.T, snap ai.BehaviorTreeSnapshot) ai.BehaviorTreeSnapshot {
	t.Helper()
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var decoded ai.BehaviorTreeSnapshot
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	return decoded
}

func TestBehaviorTreeSnapshotRestoreFullMidRunParity(t *testing.T) {
	type rig struct {
		bt         *ai.BehaviorTree
		bb         *ai.Blackboard
		rng        *random.Stream
		guardOK    bool
		retryFails int
		untilFails int
		log        []string
	}

	buildRig := func() *rig {
		r := &rig{
			bb:         ai.NewBlackboard(),
			rng:        random.New(2026),
			guardOK:    true,
			retryFails: 2,
			untilFails: 2,
		}
		r.bb.SetBool("alert", false)
		r.bb.SetBool("engaged", true)

		// High-priority observer-aborted alert branch.
		alertBranch := ai.BlackboardCondition(r.bb, "alert", ai.BBOpEq, ai.BBBool(true), ai.AbortBoth,
			ai.Sequence(
				ai.Wait(60*time.Millisecond),
				ai.Action(func(dt time.Duration) ai.Status {
					r.log = append(r.log, "alert-done")
					return ai.StatusSuccess
				}),
			),
		)

		// Main combat branch exercising every built-in composite, decorator, and custom node.
		combatBranch := ai.Guard(func() bool { return r.guardOK },
			ai.Service(75*time.Millisecond, func(elapsed time.Duration) {
				r.log = append(r.log, fmt.Sprintf("svc:%v", elapsed))
			},
				ai.Sequence(
					// 0: Cooldown around a quick action so it is cooling down when snapshotted later.
					ai.Succeeder(ai.Cooldown(120*time.Millisecond, ai.Action(func(dt time.Duration) ai.Status {
						r.log = append(r.log, "strike")
						return ai.StatusSuccess
					}))),
					// 1: Parallel(RequireAll) with Wait, Repeater+custom node, and TimeLimit+Retry.
					ai.Parallel(ai.RequireAll,
						ai.Wait(90*time.Millisecond),
						ai.Repeater(2, &customWrapperNode{
							child: &customCounterNode{
								kind:   "charge",
								target: 2,
								onTick: func(step int) { r.log = append(r.log, fmt.Sprintf("charge:%d", step)) },
							},
						}),
						ai.TimeLimit(250*time.Millisecond,
							ai.Retry(3, ai.Action(func(dt time.Duration) ai.Status {
								if r.retryFails > 0 {
									r.retryFails--
									r.log = append(r.log, "retry-fail")
									return ai.StatusFailure
								}
								r.log = append(r.log, "retry-ok")
								return ai.StatusSuccess
							})),
						),
					),
					// 2: RandomSelector + RandomSequence + WeightedSelector + Inverter/Failer/UntilFail.
					ai.RandomSelector(r.rng,
						ai.Inverter(ai.Succeeder(ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess }))), // always fails
						ai.Sequence(
							ai.Wait(40*time.Millisecond),
							ai.Action(func(time.Duration) ai.Status {
								r.log = append(r.log, "rsel-win")
								return ai.StatusSuccess
							}),
						),
						ai.Failer(ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess })), // always fails
					),
					ai.RandomSequence(r.rng,
						ai.Wait(30*time.Millisecond),
						ai.UntilFail(ai.Action(func(time.Duration) ai.Status {
							if r.untilFails > 0 {
								r.untilFails--
								r.log = append(r.log, "uf-run")
								return ai.StatusSuccess
							}
							r.log = append(r.log, "uf-done")
							return ai.StatusFailure
						})),
					),
					ai.WeightedSelector(r.rng,
						ai.WeightedNode{Node: ai.Sequence(ai.Wait(30*time.Millisecond), ai.Action(func(time.Duration) ai.Status {
							r.log = append(r.log, "w-a")
							return ai.StatusSuccess
						})), Weight: 2},
						ai.WeightedNode{Node: ai.Sequence(ai.Wait(30*time.Millisecond), ai.Action(func(time.Duration) ai.Status {
							r.log = append(r.log, "w-b")
							return ai.StatusSuccess
						})), Weight: 3},
					),
				),
			),
		)

		r.bt = ai.NewBehaviorTree(ai.Selector(alertBranch, combatBranch))
		return r
	}

	dt := 30 * time.Millisecond
	orig := buildRig()

	// Step 2 ticks so we are right in the middle of:
	// - Selector running child 1 (combatBranch)
	// - Guard running=true
	// - Service active with phase=30ms, sinceLast=30ms
	// - Sequence at index 1 (Parallel)
	// - Cooldown at child 0 cooling down (remaining=90ms after 30ms off-tick)
	// - Parallel active with Wait(60ms/90ms), Repeater(done=1, charge=0), TimeLimit(60ms/250ms)+Retry(failures=2)
	// - BlackboardCondition observed alert=false at rev 1
	for range 2 {
		if st := orig.bt.Tick(dt); st != ai.StatusRunning {
			t.Fatalf("pre-snapshot tick returned %v, want Running", st)
		}
	}

	snap, err := orig.bt.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}
	rngState := orig.rng.State()

	yamlSnap := roundTripBTYAML(t, snap)
	jsonSnap := roundTripBTJSON(t, snap)

	cloneYAML := buildRig()
	cloneYAML.retryFails = orig.retryFails
	cloneYAML.untilFails = orig.untilFails
	cloneYAML.rng.SetState(rngState)
	if err := cloneYAML.bt.Restore(yamlSnap); err != nil {
		t.Fatalf("Restore(yamlSnap) failed: %v", err)
	}

	cloneJSON := buildRig()
	cloneJSON.retryFails = orig.retryFails
	cloneJSON.untilFails = orig.untilFails
	cloneJSON.rng.SetState(rngState)
	if err := cloneJSON.bt.Restore(jsonSnap); err != nil {
		t.Fatalf("Restore(jsonSnap) failed: %v", err)
	}

	// Clear pre-snapshot logs and step all three trees identically through completion,
	// observer abort, and second pass.
	orig.log = nil
	cloneYAML.log = nil
	cloneJSON.log = nil

	for step := 1; step <= 25; step++ {
		if step == 14 {
			// Trigger BlackboardCondition AbortLower preemption mid-run!
			orig.bb.SetBool("alert", true)
			cloneYAML.bb.SetBool("alert", true)
			cloneJSON.bb.SetBool("alert", true)
		}
		if step == 15 {
			// Clear alert mid-alert-wait to trigger AbortSelf!
			orig.bb.SetBool("alert", false)
			cloneYAML.bb.SetBool("alert", false)
			cloneJSON.bb.SetBool("alert", false)
		}

		wantSt := orig.bt.Tick(dt)
		gotYAML := cloneYAML.bt.Tick(dt)
		gotJSON := cloneJSON.bt.Tick(dt)
		if gotYAML != wantSt || gotJSON != wantSt {
			t.Fatalf("step %d: status yaml=%v json=%v, want %v", step, gotYAML, gotJSON, wantSt)
		}
	}

	if !slices.Equal(cloneYAML.log, orig.log) {
		t.Fatalf("YAML restored log mismatch:\ngot:  %v\nwant: %v", cloneYAML.log, orig.log)
	}
	if !slices.Equal(cloneJSON.log, orig.log) {
		t.Fatalf("JSON restored log mismatch:\ngot:  %v\nwant: %v", cloneJSON.log, orig.log)
	}
}

func TestBehaviorTreeSnapshotMidRandomWeightedUtilityAndToken(t *testing.T) {
	// Test mid-run RandomSelector, RandomSequence, WeightedSelector (with fallback after failure),
	// Parallel(RequireOne), WithAttackToken, and UtilitySelector.
	type env struct {
		tree       ai.Node
		rng        *random.Stream
		pool       *ai.TokenPool
		uRng       *random.Stream
		failFirst  bool
		failSecond bool
		log        []string
	}

	build := func() *env {
		e := &env{
			rng:        random.New(99),
			pool:       ai.NewTokenPool(ai.TokenPoolConfig{Capacity: 1}),
			uRng:       random.New(7),
			failFirst:  true,
			failSecond: true,
		}
		e.pool.Request(1, 1)
		e.pool.Step(0)
		reasoner := ai.NewReasoner(
			ai.UtilityAction[int]{
				Name:           "a",
				Weight:         1,
				Considerations: []ai.Consideration[int]{{Input: func(x int) float64 { return float64(x) / 10 }, Curve: ai.LinearCurve(1, 0, 0)}},
			},
			ai.UtilityAction[int]{
				Name:           "b",
				Weight:         1,
				Considerations: []ai.Consideration[int]{{Input: func(x int) float64 { return 1 - float64(x)/10 }, Curve: ai.LinearCurve(1, 0, 0)}},
			},
		)
		uSel := ai.UtilitySelector(reasoner, func() int { return 8 }, e.uRng, map[string]ai.Node{
			"a": ai.Wait(60 * time.Millisecond),
			"b": ai.Wait(60 * time.Millisecond),
		})

		wSel := ai.WeightedSelector(e.rng,
			ai.WeightedNode{Node: ai.Sequence(
				ai.Wait(40*time.Millisecond),
				ai.Action(func(time.Duration) ai.Status {
					if e.failFirst {
						e.failFirst = false
						e.log = append(e.log, "w0-fail")
						return ai.StatusFailure
					}
					e.log = append(e.log, "w0-ok")
					return ai.StatusSuccess
				}),
			), Weight: 5},
			ai.WeightedNode{Node: ai.Sequence(
				ai.Wait(40*time.Millisecond),
				ai.Action(func(time.Duration) ai.Status {
					if e.failSecond {
						e.failSecond = false
						e.log = append(e.log, "w1-fail")
						return ai.StatusFailure
					}
					e.log = append(e.log, "w1-ok")
					return ai.StatusSuccess
				}),
			), Weight: 5},
			ai.WeightedNode{Node: ai.Action(func(time.Duration) ai.Status {
				e.log = append(e.log, "w2-ok")
				return ai.StatusSuccess
			}), Weight: 1},
		)

		e.tree = ai.Parallel(ai.RequireOne,
			ai.WithAttackToken(e.pool, 1, nil, ai.Sequence(
				ai.RandomSequence(e.rng, ai.Wait(40*time.Millisecond), ai.Wait(40*time.Millisecond)),
				wSel,
			)),
			uSel,
		)
		return e
	}

	dt := 20 * time.Millisecond
	orig := build()
	// Tick 1: starts RandomSequence, WithAttackToken (running=true), UtilitySelector (active=0, running=0), Parallel(RequireOne).
	if st := orig.tree.Tick(dt); st != ai.StatusRunning {
		t.Fatalf("tick 1 = %v, want Running", st)
	}

	snap, err := ai.SnapshotBehaviorTree(orig.tree)
	if err != nil {
		t.Fatalf("SnapshotBehaviorTree failed: %v", err)
	}
	yamlSnap := roundTripBTYAML(t, snap)
	rngSt := orig.rng.State()
	uRngSt := orig.uRng.State()

	clone := build()
	clone.rng.SetState(rngSt)
	clone.uRng.SetState(uRngSt)
	if err := ai.RestoreBehaviorTree(clone.tree, yamlSnap); err != nil {
		t.Fatalf("RestoreBehaviorTree failed: %v", err)
	}

	for step := 2; step <= 12; step++ {
		want := orig.tree.Tick(dt)
		got := clone.tree.Tick(dt)
		if got != want {
			t.Fatalf("step %d: got %v, want %v", step, got, want)
		}
	}
	if !slices.Equal(clone.log, orig.log) {
		t.Fatalf("log mismatch:\ngot:  %v\nwant: %v", clone.log, orig.log)
	}
}

func TestBehaviorTreeYAMLBuiltSnapshotRestore(t *testing.T) {
	const src = `version: 1
name: guard
root:
  type: selector
  children:
    - type: sequence
      children:
        - type: wait
          duration: 60ms
        - type: cooldown
          duration: 100ms
          child:
            type: action
            action: ping
`
	doc, err := ai.ParseBehaviorTree([]byte(src))
	if err != nil {
		t.Fatalf("ParseBehaviorTree failed: %v", err)
	}

	build := func(pings *int) ai.Node {
		tree, err := ai.BuildBehaviorTree(doc, ai.BTBindings{
			Actions: map[string]func(ai.BTNodeContext) (ai.Node, error){
				"ping": func(ai.BTNodeContext) (ai.Node, error) {
					return ai.Action(func(time.Duration) ai.Status {
						*pings++
						return ai.StatusSuccess
					}), nil
				},
			},
		})
		if err != nil {
			t.Fatalf("BuildBehaviorTree failed: %v", err)
		}
		return tree
	}

	origPings, clonePings := 0, 0
	orig := build(&origPings)
	clone := build(&clonePings)

	dt := 20 * time.Millisecond
	// 2 ticks = 40ms into the 60ms wait; selector runningChild = 0!
	if orig.Tick(dt) != ai.StatusRunning || orig.Tick(dt) != ai.StatusRunning {
		t.Fatal("expected Running on first two ticks")
	}

	snap, err := ai.SnapshotBehaviorTree(orig)
	if err != nil {
		t.Fatalf("SnapshotBehaviorTree failed: %v", err)
	}
	decoded := roundTripBTYAML(t, snap)

	if err := ai.RestoreBehaviorTree(clone, decoded); err != nil {
		t.Fatalf("RestoreBehaviorTree failed: %v", err)
	}

	for step := range 10 {
		want := orig.Tick(dt)
		got := clone.Tick(dt)
		if got != want {
			t.Fatalf("step %d: got %v, want %v", step, got, want)
		}
	}
	if origPings != clonePings || origPings == 0 {
		t.Fatalf("pings mismatch: orig=%d clone=%d", origPings, clonePings)
	}
}

func TestBehaviorTreeSnapshotSharedDAGNode(t *testing.T) {
	shared := ai.Wait(100 * time.Millisecond)
	tree1 := ai.Sequence(shared, shared)
	tree1.Tick(40 * time.Millisecond)

	snap, err := ai.SnapshotBehaviorTree(tree1)
	if err != nil {
		t.Fatalf("SnapshotBehaviorTree on DAG failed: %v", err)
	}
	decoded := roundTripBTYAML(t, snap)

	shared2 := ai.Wait(100 * time.Millisecond)
	tree2 := ai.Sequence(shared2, shared2)
	if err := ai.RestoreBehaviorTree(tree2, decoded); err != nil {
		t.Fatalf("RestoreBehaviorTree on DAG failed: %v", err)
	}
	for step := range 6 {
		if got, want := tree2.Tick(30*time.Millisecond), tree1.Tick(30*time.Millisecond); got != want {
			t.Fatalf("step %d: got %v, want %v", step, got, want)
		}
	}
}

func TestBehaviorTreeRestoreShapeMismatchAndValidation(t *testing.T) {
	base := func() ai.Node {
		return ai.Sequence(
			ai.Wait(100*time.Millisecond),
			ai.Cooldown(200*time.Millisecond, ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess })),
		)
	}

	orig := base()
	orig.Tick(40 * time.Millisecond)
	snap, err := ai.SnapshotBehaviorTree(orig)
	if err != nil {
		t.Fatalf("SnapshotBehaviorTree failed: %v", err)
	}

	mismatches := []struct {
		name    string
		target  ai.Node
		wantSub []string
	}{
		{
			name: "swapped root kind",
			target: ai.Selector(
				ai.Wait(100*time.Millisecond),
				ai.Cooldown(200*time.Millisecond, ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess })),
			),
			wantSub: []string{`"0"`, `"sequence"`, `"selector"`},
		},
		{
			name: "swapped child kind",
			target: ai.Sequence(
				ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess }),
				ai.Cooldown(200*time.Millisecond, ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess })),
			),
			wantSub: []string{`"0/0"`, `"wait"`, `"action"`},
		},
		{
			name: "extra child in target",
			target: ai.Sequence(
				ai.Wait(100*time.Millisecond),
				ai.Cooldown(200*time.Millisecond, ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess })),
				ai.Wait(50*time.Millisecond),
			),
			wantSub: []string{`"0/2"`, "missing"},
		},
		{
			name: "missing child in target",
			target: ai.Sequence(
				ai.Wait(100 * time.Millisecond),
			),
			wantSub: []string{`"0/1"`, "extra"},
		},
		{
			name: "parallel policy mismatch",
			target: ai.Parallel(ai.RequireOne,
				ai.Wait(50*time.Millisecond),
			),
			wantSub: []string{`"0"`},
		},
	}

	for _, tc := range mismatches {
		t.Run(tc.name, func(t *testing.T) {
			err := ai.RestoreBehaviorTree(tc.target, snap)
			if err == nil {
				t.Fatalf("RestoreBehaviorTree(%s) succeeded, want error", tc.name)
			}
			if !errors.Is(err, ai.ErrInvalidBTSnapshot) {
				t.Fatalf("err = %v, want ErrInvalidBTSnapshot", err)
			}
			for _, sub := range tc.wantSub {
				if !strings.Contains(err.Error(), sub) {
					t.Fatalf("err = %q, want substring %q", err.Error(), sub)
				}
			}
		})
	}

	// Parallel RequireAll vs RequireOne policy mismatch with matching structure.
	pAll := ai.Parallel(ai.RequireAll, ai.Wait(100*time.Millisecond))
	pAll.Tick(20 * time.Millisecond)
	pSnap, err := ai.SnapshotBehaviorTree(pAll)
	if err != nil {
		t.Fatalf("SnapshotBehaviorTree(parallel) failed: %v", err)
	}
	pOne := ai.Parallel(ai.RequireOne, ai.Wait(100*time.Millisecond))
	if err := ai.RestoreBehaviorTree(pOne, pSnap); !errors.Is(err, ai.ErrInvalidBTSnapshot) || !strings.Contains(err.Error(), "policy") {
		t.Fatalf("parallel policy mismatch err = %v, want ErrInvalidBTSnapshot mentioning policy", err)
	}

	// Custom node kind mismatch.
	cTreeA := ai.Sequence(&customCounterNode{kind: "patrol", target: 3})
	cTreeB := ai.Sequence(&customCounterNode{kind: "flee", target: 3})
	cSnap, err := ai.SnapshotBehaviorTree(cTreeA)
	if err != nil {
		t.Fatalf("SnapshotBehaviorTree(custom) failed: %v", err)
	}
	if err := ai.RestoreBehaviorTree(cTreeB, cSnap); !errors.Is(err, ai.ErrInvalidBTSnapshot) || !strings.Contains(err.Error(), `"0/0"`) {
		t.Fatalf("custom kind mismatch err = %v, want ErrInvalidBTSnapshot at 0/0", err)
	}

	// Unsupported version and nil root.
	badVer := snap
	badVer.Version = 99
	if err := ai.RestoreBehaviorTree(base(), badVer); !errors.Is(err, ai.ErrInvalidBTSnapshot) {
		t.Fatalf("bad version err = %v, want ErrInvalidBTSnapshot", err)
	}
	if _, err := ai.SnapshotBehaviorTree(nil); !errors.Is(err, ai.ErrInvalidBTSnapshot) {
		t.Fatalf("SnapshotBehaviorTree(nil) err = %v, want ErrInvalidBTSnapshot", err)
	}
	if err := ai.RestoreBehaviorTree(nil, snap); !errors.Is(err, ai.ErrInvalidBTSnapshot) {
		t.Fatalf("RestoreBehaviorTree(nil) err = %v, want ErrInvalidBTSnapshot", err)
	}
	var nilBT *ai.BehaviorTree
	if _, err := nilBT.Snapshot(); !errors.Is(err, ai.ErrInvalidBTSnapshot) {
		t.Fatalf("nilBT.Snapshot err = %v, want ErrInvalidBTSnapshot", err)
	}
	if err := nilBT.Restore(snap); !errors.Is(err, ai.ErrInvalidBTSnapshot) {
		t.Fatalf("nilBT.Restore err = %v, want ErrInvalidBTSnapshot", err)
	}
}

func TestBehaviorTreeRestoreAtomicityAndRollback(t *testing.T) {
	// Verify that an invalid built-in field OR a failing second custom SnapshotNode
	// leaves both earlier built-in nodes and earlier custom nodes unchanged.
	w := ai.Wait(100 * time.Millisecond)
	c1 := &customCounterNode{kind: "first", target: 5}
	c2 := &customCounterNode{kind: "second", target: 5}
	tree := ai.Sequence(w, c1, c2)

	// Advance tree: wait elapsed=40ms (still at index 0), and advance c1 separately? No, use Parallel!
	w = ai.Wait(100 * time.Millisecond)
	c1 = &customCounterNode{kind: "first", target: 5}
	c2 = &customCounterNode{kind: "second", target: 5}
	tree = ai.Parallel(ai.RequireAll, w, c1, c2)

	tree.Tick(40 * time.Millisecond) // w=40ms, c1=1, c2=1
	beforeSnap, err := ai.SnapshotBehaviorTree(tree)
	if err != nil {
		t.Fatalf("beforeSnap failed: %v", err)
	}

	// Build a valid snapshot at step 2 (w=80ms, c1=2, c2=2) on a separate tree.
	other := ai.Parallel(ai.RequireAll,
		ai.Wait(100*time.Millisecond),
		&customCounterNode{kind: "first", target: 5},
		&customCounterNode{kind: "second", target: 5},
	)
	other.Tick(40 * time.Millisecond)
	other.Tick(40 * time.Millisecond)
	validSnap, err := ai.SnapshotBehaviorTree(other)
	if err != nil {
		t.Fatalf("validSnap failed: %v", err)
	}

	// Case 1: Invalid built-in wait elapsed (>= duration) -> must not touch c1, c2, or w.
	corruptBuiltIn := validSnap
	corruptBuiltIn.Nodes = slices.Clone(validSnap.Nodes)
	corruptBuiltIn.Nodes[1].Elapsed = 100 * time.Millisecond
	if err := ai.RestoreBehaviorTree(tree, corruptBuiltIn); !errors.Is(err, ai.ErrInvalidBTSnapshot) {
		t.Fatalf("corrupt built-in err = %v, want ErrInvalidBTSnapshot", err)
	}
	afterSnap1, _ := ai.SnapshotBehaviorTree(tree)
	if !bytes.Equal(mustYAML(t, beforeSnap), mustYAML(t, afterSnap1)) {
		t.Fatalf("tree mutated after failed built-in restore:\nbefore:\n%s\nafter:\n%s", mustYAML(t, beforeSnap), mustYAML(t, afterSnap1))
	}

	// Case 2: Second custom node fails RestoreState -> c1 and built-in nodes must roll back.
	c2.failErr = errors.New("boom")
	if err := ai.RestoreBehaviorTree(tree, validSnap); !errors.Is(err, ai.ErrInvalidBTSnapshot) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("failing custom node err = %v, want ErrInvalidBTSnapshot wrapping boom", err)
	}
	c2.failErr = nil
	afterSnap2, _ := ai.SnapshotBehaviorTree(tree)
	if !bytes.Equal(mustYAML(t, beforeSnap), mustYAML(t, afterSnap2)) {
		t.Fatalf("tree mutated after failed custom node restore:\nbefore:\n%s\nafter:\n%s", mustYAML(t, beforeSnap), mustYAML(t, afterSnap2))
	}
}

type cyclicNode struct {
	child ai.Node
}

func (c *cyclicNode) Tick(time.Duration) ai.Status { return ai.StatusFailure }
func (c *cyclicNode) Reset()                       {}
func (c *cyclicNode) Children() []ai.Node          { return []ai.Node{c.child} }

func TestBehaviorTreeSnapshotCycleAndStatusText(t *testing.T) {
	c := &cyclicNode{}
	c.child = c
	if _, err := ai.SnapshotBehaviorTree(c); !errors.Is(err, ai.ErrInvalidBTSnapshot) || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cyclic snapshot err = %v, want ErrInvalidBTSnapshot mentioning cycle", err)
	}

	for _, st := range []ai.Status{ai.StatusSuccess, ai.StatusFailure, ai.StatusRunning} {
		b, err := st.MarshalText()
		if err != nil {
			t.Fatalf("Status(%v).MarshalText failed: %v", st, err)
		}
		var got ai.Status
		if err := got.UnmarshalText(b); err != nil {
			t.Fatalf("Status.UnmarshalText(%q) failed: %v", b, err)
		}
		if got != st {
			t.Fatalf("Status round-trip = %v, want %v", got, st)
		}
	}
	if _, err := ai.Status(99).MarshalText(); err == nil {
		t.Fatal("MarshalText accepted invalid status")
	}
	var bad ai.Status
	if err := bad.UnmarshalText([]byte("bogus")); err == nil {
		t.Fatal("UnmarshalText accepted bogus status")
	}
}

func mustYAML(t *testing.T, v any) []byte {
	t.Helper()
	b, err := yaml.Marshal(v)
	if err != nil {
		t.Fatalf("yaml.Marshal failed: %v", err)
	}
	return b
}

func TestBehaviorTreeSnapshotMidRandomSelectorAndWeightedFallback(t *testing.T) {
	// Snapshot right in the middle of an active RandomSelector and an active WeightedSelector
	// (after one option failed and a second is currently Running).
	buildRandomSel := func() (ai.Node, *random.Stream, *[]string) {
		rng := random.New(12345)
		var log []string
		node := ai.RandomSelector(rng,
			ai.Sequence(ai.Wait(40*time.Millisecond), ai.Action(func(time.Duration) ai.Status {
				log = append(log, "r0-fail")
				return ai.StatusFailure
			})),
			ai.Sequence(ai.Wait(40*time.Millisecond), ai.Action(func(time.Duration) ai.Status {
				log = append(log, "r1-win")
				return ai.StatusSuccess
			})),
			ai.Sequence(ai.Wait(40*time.Millisecond), ai.Action(func(time.Duration) ai.Status {
				log = append(log, "r2-fail")
				return ai.StatusFailure
			})),
		)
		return node, rng, &log
	}

	dt := 20 * time.Millisecond
	rsOrig, rsRng, rsOrigLog := buildRandomSel()
	if st := rsOrig.Tick(dt); st != ai.StatusRunning {
		t.Fatalf("RandomSelector tick 1 = %v, want Running", st)
	}
	rsSnap, err := ai.SnapshotBehaviorTree(rsOrig)
	if err != nil {
		t.Fatalf("SnapshotBehaviorTree(RandomSelector) failed: %v", err)
	}
	rsDecoded := roundTripBTYAML(t, rsSnap)
	rsClone, rsCloneRng, rsCloneLog := buildRandomSel()
	rsCloneRng.SetState(rsRng.State())
	if err := ai.RestoreBehaviorTree(rsClone, rsDecoded); err != nil {
		t.Fatalf("RestoreBehaviorTree(RandomSelector) failed: %v", err)
	}
	for step := range 8 {
		if got, want := rsClone.Tick(dt), rsOrig.Tick(dt); got != want {
			t.Fatalf("RandomSelector step %d: got %v, want %v", step, got, want)
		}
	}
	if !slices.Equal(*rsCloneLog, *rsOrigLog) {
		t.Fatalf("RandomSelector log mismatch: got %v, want %v", *rsCloneLog, *rsOrigLog)
	}

	// WeightedSelector mid-run after option 0 fails synchronously and option 1/2 is running.
	buildWeighted := func() (ai.Node, *random.Stream, *[]string) {
		rng := random.New(77)
		var log []string
		node := ai.WeightedSelector(rng,
			ai.WeightedNode{Node: ai.Action(func(time.Duration) ai.Status {
				log = append(log, "opt0-fail")
				return ai.StatusFailure
			}), Weight: 10},
			ai.WeightedNode{Node: ai.Sequence(ai.Wait(40*time.Millisecond), ai.Action(func(time.Duration) ai.Status {
				log = append(log, "opt1-fail")
				return ai.StatusFailure
			})), Weight: 5},
			ai.WeightedNode{Node: ai.Sequence(ai.Wait(40*time.Millisecond), ai.Action(func(time.Duration) ai.Status {
				log = append(log, "opt2-win")
				return ai.StatusSuccess
			})), Weight: 2},
		)
		return node, rng, &log
	}

	wsOrig, wsRng, wsOrigLog := buildWeighted()
	if st := wsOrig.Tick(dt); st != ai.StatusRunning {
		t.Fatalf("WeightedSelector tick 1 = %v, want Running", st)
	}
	wsSnap, err := ai.SnapshotBehaviorTree(wsOrig)
	if err != nil {
		t.Fatalf("SnapshotBehaviorTree(WeightedSelector) failed: %v", err)
	}
	wsDecoded := roundTripBTYAML(t, wsSnap)
	wsClone, wsCloneRng, wsCloneLog := buildWeighted()
	wsCloneRng.SetState(wsRng.State())
	if err := ai.RestoreBehaviorTree(wsClone, wsDecoded); err != nil {
		t.Fatalf("RestoreBehaviorTree(WeightedSelector) failed: %v", err)
	}
	*wsOrigLog = nil
	*wsCloneLog = nil
	for step := range 8 {
		if got, want := wsClone.Tick(dt), wsOrig.Tick(dt); got != want {
			t.Fatalf("WeightedSelector step %d: got %v, want %v", step, got, want)
		}
	}
	if !slices.Equal(*wsCloneLog, *wsOrigLog) {
		t.Fatalf("WeightedSelector log mismatch: got %v, want %v", *wsCloneLog, *wsOrigLog)
	}
}

func TestBehaviorTreeSnapshotPendingCooldownOffTickAndReset(t *testing.T) {
	type rig struct {
		bt      *ai.BehaviorTree
		inRange bool
		attacks int
	}

	build := func() *rig {
		r := &rig{}
		r.bt = ai.NewBehaviorTree(ai.Selector(
			ai.Guard(func() bool { return r.inRange }, ai.Sequence(
				ai.Cooldown(1500*time.Millisecond, ai.Action(func(time.Duration) ai.Status {
					r.attacks++
					return ai.StatusSuccess
				})),
				ai.Wait(300*time.Millisecond),
			)),
			ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess }),
		))
		return r
	}

	const dt = 20 * time.Millisecond
	orig := build()
	orig.inRange = true

	// Tick 1 (20ms): fires attack (1500ms cooldown starts) and enters Wait(300ms).
	if st := orig.bt.Tick(dt); st != ai.StatusRunning || orig.attacks != 1 {
		t.Fatalf("tick 1: st=%v attacks=%d, want Running, 1", st, orig.attacks)
	}
	// Ticks 2..11 (200ms total): out of range -> Guard resets Sequence on tick 2,
	// Cooldown is never ticked for 200ms, leaving 1300ms remaining.
	orig.inRange = false
	for range 10 {
		if st := orig.bt.Tick(dt); st != ai.StatusSuccess {
			t.Fatalf("off-tick fallback = %v, want Success", st)
		}
	}

	snap, err := orig.bt.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}
	foundCooldown := false
	for _, n := range snap.Nodes {
		if n.Kind == string(ai.BTNodeCooldown) {
			foundCooldown = true
			if n.Remaining != 1300*time.Millisecond {
				t.Fatalf("snapshot cooldown remaining = %v, want 1.3s", n.Remaining)
			}
		}
	}
	if !foundCooldown {
		t.Fatal("snapshot did not contain cooldown node")
	}

	yamlSnap := roundTripBTYAML(t, snap)
	jsonSnap := roundTripBTJSON(t, snap)

	// Restore into both a fresh tree (clock=0) and a warm tree (clock=5s) and verify
	// identical off-tick countdown and exact expiry step.
	fresh := build()
	if err := fresh.bt.Restore(yamlSnap); err != nil {
		t.Fatalf("fresh.Restore(yaml) failed: %v", err)
	}
	warm := build()
	warm.inRange = false
	for range 250 { // advance warm tree clock by 5s beforehand
		warm.bt.Tick(dt)
	}
	if err := warm.bt.Restore(jsonSnap); err != nil {
		t.Fatalf("warm.Restore(json) failed: %v", err)
	}

	// Re-snapshotting immediately after restore into fresh and warm trees must report 1300ms.
	for name, r := range map[string]*rig{"fresh": fresh, "warm": warm} {
		reSnap, err := r.bt.Snapshot()
		if err != nil {
			t.Fatalf("%s re-snapshot failed: %v", name, err)
		}
		if !bytes.Equal(mustYAML(t, snap), mustYAML(t, reSnap)) {
			t.Fatalf("%s re-snapshot YAML differs:\ngot:\n%s\nwant:\n%s", name, mustYAML(t, reSnap), mustYAML(t, snap))
		}
	}

	// Advance 64 ticks (1280ms) out of range: cooldown still has 20ms left on all three trees.
	for range 64 {
		orig.bt.Tick(dt)
		fresh.bt.Tick(dt)
		warm.bt.Tick(dt)
	}
	// On the 65th tick (1300ms elapsed since snapshot), bring player in range: all three attack together!
	orig.inRange, fresh.inRange, warm.inRange = true, true, true
	if st := orig.bt.Tick(dt); st != ai.StatusRunning || orig.attacks != 2 {
		t.Fatalf("orig at expiry: st=%v attacks=%d, want Running, 2", st, orig.attacks)
	}
	if st := fresh.bt.Tick(dt); st != ai.StatusRunning || fresh.attacks != 1 {
		t.Fatalf("fresh at expiry: st=%v attacks=%d, want Running, 1", st, fresh.attacks)
	}
	if st := warm.bt.Tick(dt); st != ai.StatusRunning || warm.attacks != 1 {
		t.Fatalf("warm at expiry: st=%v attacks=%d, want Running, 1", st, warm.attacks)
	}

	// Restoring an idle snapshot clears an active cooldown on the target tree.
	idleTree := build()
	idleSnap, err := idleTree.bt.Snapshot()
	if err != nil {
		t.Fatalf("idle snapshot failed: %v", err)
	}
	if err := fresh.bt.Restore(idleSnap); err != nil {
		t.Fatalf("restore idle snapshot failed: %v", err)
	}
	if st := fresh.bt.Tick(dt); st != ai.StatusRunning || fresh.attacks != 2 {
		t.Fatalf("after restoring idle snapshot: st=%v attacks=%d, want Running, 2", st, fresh.attacks)
	}
}

func TestBehaviorTreeCooldownThroughCustomChildNodes(t *testing.T) {
	inRange := true
	hits := 0
	wrapped := &customWrapperNode{
		child: ai.Cooldown(200*time.Millisecond, ai.Action(func(time.Duration) ai.Status {
			hits++
			return ai.StatusSuccess
		})),
	}
	tree := ai.Selector(
		ai.Guard(func() bool { return inRange }, wrapped),
		ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess }),
	)
	const dt = 50 * time.Millisecond
	tree.Tick(dt) // hits=1, cooldown 200ms
	inRange = false
	tree.Tick(dt) // 50ms off-tick
	tree.Tick(dt) // 100ms off-tick
	tree.Tick(dt) // 150ms off-tick
	inRange = true
	tree.Tick(dt) // 200ms -> cooldown expires, hits=2
	if hits != 2 {
		t.Fatalf("hits = %d, want 2", hits)
	}

	// ResetCooldowns also traverses custom ChildNodes and handles cyclic nodes safely.
	ai.ResetCooldowns(tree)
	tree.Tick(dt)
	if hits != 3 {
		t.Fatalf("hits after ResetCooldowns = %d, want 3", hits)
	}
	cyc := &cyclicNode{}
	cyc.child = cyc
	ai.ResetCooldowns(cyc)
}
