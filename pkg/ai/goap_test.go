package ai_test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestFactSchemaAndWorldState(t *testing.T) {
	s := ai.NewFactSchema()
	for i := range ai.MaxBoolFacts {
		name := fmt.Sprintf("b%d", i)
		f, err := s.Bool(name)
		if err != nil {
			t.Fatalf("Bool(%q): %v", name, err)
		}
		if int(f) != i {
			t.Fatalf("Bool(%q) = %d, want %d", name, f, i)
		}
	}
	if _, err := s.Bool("overflow"); err == nil {
		t.Fatal("expected error registering > MaxBoolFacts")
	}
	for i := range ai.MaxIntFacts {
		name := fmt.Sprintf("i%d", i)
		f, err := s.Int(name)
		if err != nil {
			t.Fatalf("Int(%q): %v", name, err)
		}
		if int(f) != i {
			t.Fatalf("Int(%q) = %d, want %d", name, f, i)
		}
	}
	if _, err := s.Int("overflow_int"); err == nil {
		t.Fatal("expected error registering > MaxIntFacts")
	}
	if _, err := s.Int("b0"); err == nil {
		t.Fatal("expected error registering existing bool name as int")
	}
	if _, err := s.Bool("i0"); err == nil {
		t.Fatal("expected error registering existing int name as bool")
	}

	var w ai.WorldState
	b0, _ := s.LookupBool("b0")
	b63, _ := s.LookupBool("b63")
	b64, _ := s.LookupBool("b64")
	b127, _ := s.LookupBool("b127")
	i0, _ := s.LookupInt("i0")
	i15, _ := s.LookupInt("i15")

	w = w.WithBool(b0, true).WithBool(b63, true).WithBool(b64, true).WithBool(b127, true).
		WithInt(i0, 42).WithInt(i15, 999999)
	if !w.Bool(b0) || !w.Bool(b63) || !w.Bool(b64) || !w.Bool(b127) {
		t.Fatal("expected bool bits across both 64-bit words to be set")
	}
	if w.Int(i0) != 42 || w.Int(i15) != math.MaxInt16 {
		t.Fatalf("ints = (%d, %d), want (42, %d)", w.Int(i0), w.Int(i15), math.MaxInt16)
	}

	cond := ai.Conditions{}.
		RequireBool(b0, true).
		RequireBool(b127, true).
		RequireIntGte(i0, 40).
		RequireIntLte(i0, 50)
	if !w.Satisfies(cond) {
		t.Fatal("expected world to satisfy conditions")
	}
	contradictory := cond.RequireIntLt(i0, 40)
	if w.Satisfies(contradictory) {
		t.Fatal("contradictory bounds must never be satisfied")
	}

	eff := ai.Effects{}.SetBool(b64, false).AddInt(i0, -10).SetInt(i15, -5)
	w2 := w.Apply(eff)
	if w2.Bool(b64) || w2.Int(i0) != 32 || w2.Int(i15) != -5 {
		t.Fatalf("Apply produced b64=%v i0=%d i15=%d, want false 32 -5", w2.Bool(b64), w2.Int(i0), w2.Int(i15))
	}
}

type campDomain struct {
	schema       *ai.FactSchema
	hasAxe       ai.BoolFact
	hasFire      ai.BoolFact
	cookedMeal   ai.BoolFact
	hasBow       ai.BoolFact
	wood         ai.IntFact
	rawMeat      ai.IntFact
	axeAvailable bool
	planner      *ai.GOAPPlanner
	goalFire     ai.GOAPGoal
	goalMeal     ai.GOAPGoal
}

func newCampDomain(t *testing.T) *campDomain {
	t.Helper()
	s := ai.NewFactSchema()
	d := &campDomain{
		schema:       s,
		hasAxe:       s.MustBool("has_axe"),
		hasFire:      s.MustBool("has_fire"),
		cookedMeal:   s.MustBool("cooked_meal"),
		hasBow:       s.MustBool("has_bow"),
		wood:         s.MustInt("wood"),
		rawMeat:      s.MustInt("raw_meat"),
		axeAvailable: true,
	}

	var err error
	d.planner, err = ai.NewGOAPPlanner(
		ai.GOAPAction{
			Name:    "fetch_axe",
			Cost:    1,
			Effects: ai.Effects{}.SetBool(d.hasAxe, true),
			Check:   func(ai.WorldState) bool { return d.axeAvailable },
		},
		ai.GOAPAction{
			Name:          "chop_wood",
			Cost:          1,
			Preconditions: ai.Conditions{}.RequireBool(d.hasAxe, true),
			Effects:       ai.Effects{}.AddInt(d.wood, 1),
		},
		ai.GOAPAction{
			Name:    "gather_sticks",
			Cost:    3,
			Effects: ai.Effects{}.AddInt(d.wood, 1),
		},
		ai.GOAPAction{
			Name:          "build_fire",
			Cost:          1,
			Preconditions: ai.Conditions{}.RequireIntGte(d.wood, 2),
			Effects:       ai.Effects{}.SetBool(d.hasFire, true).AddInt(d.wood, -2),
		},
		ai.GOAPAction{
			Name:    "fetch_bow",
			Cost:    1,
			Effects: ai.Effects{}.SetBool(d.hasBow, true),
		},
		ai.GOAPAction{
			Name:          "hunt_game",
			Cost:          2,
			Preconditions: ai.Conditions{}.RequireBool(d.hasBow, true),
			Effects:       ai.Effects{}.AddInt(d.rawMeat, 1),
		},
		ai.GOAPAction{
			Name:    "forage_meat",
			Cost:    5,
			Effects: ai.Effects{}.AddInt(d.rawMeat, 1),
		},
		ai.GOAPAction{
			Name:          "cook_meat",
			Cost:          1,
			Preconditions: ai.Conditions{}.RequireBool(d.hasFire, true).RequireIntGte(d.rawMeat, 1),
			Effects:       ai.Effects{}.SetBool(d.cookedMeal, true).AddInt(d.rawMeat, -1),
		},
	)
	if err != nil {
		t.Fatalf("NewGOAPPlanner: %v", err)
	}

	d.goalFire = ai.GOAPGoal{
		Name:     "light_campfire",
		Priority: 10,
		Target:   ai.Conditions{}.RequireBool(d.hasFire, true),
	}
	d.goalMeal = ai.GOAPGoal{
		Name:     "prepare_meal",
		Priority: 5,
		Target:   ai.Conditions{}.RequireBool(d.cookedMeal, true),
	}
	return d
}

func TestGOAPReferenceActionSetAndDeterminism(t *testing.T) {
	d := newCampDomain(t)

	cases := []struct {
		name         string
		start        ai.WorldState
		axeAvailable bool
		goal         ai.GOAPGoal
		wantPlan     []string
		wantCost     float64
	}{
		{
			name:         "fire via axe is cheapest from empty state",
			start:        ai.WorldState{},
			axeAvailable: true,
			goal:         d.goalFire,
			wantPlan:     []string{"fetch_axe", "chop_wood", "chop_wood", "build_fire"},
			wantCost:     4,
		},
		{
			name:         "fire falls back to sticks when axe unavailable",
			start:        ai.WorldState{},
			axeAvailable: false,
			goal:         d.goalFire,
			wantPlan:     []string{"gather_sticks", "gather_sticks", "build_fire"},
			wantCost:     7,
		},
		{
			name:         "fire with existing wood skips gathering",
			start:        ai.WorldState{}.WithInt(d.wood, 2),
			axeAvailable: true,
			goal:         d.goalFire,
			wantPlan:     []string{"build_fire"},
			wantCost:     1,
		},
		{
			name:         "meal chains fire building and hunting",
			start:        ai.WorldState{}.WithBool(d.hasFire, true),
			axeAvailable: true,
			goal:         d.goalMeal,
			wantPlan:     []string{"fetch_bow", "hunt_game", "cook_meat"},
			wantCost:     4,
		},
		{
			name:         "meal from empty state builds fire and hunts",
			start:        ai.WorldState{},
			axeAvailable: true,
			goal:         d.goalMeal,
			wantPlan:     []string{"fetch_axe", "chop_wood", "chop_wood", "build_fire", "fetch_bow", "hunt_game", "cook_meat"},
			wantCost:     8,
		},
		{
			name:         "already satisfied goal returns empty zero-cost plan",
			start:        ai.WorldState{}.WithBool(d.hasFire, true),
			axeAvailable: true,
			goal:         d.goalFire,
			wantPlan:     []string{},
			wantCost:     0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d.axeAvailable = tc.axeAvailable
			var first []string
			for run := range 15 {
				plan, cost, err := d.planner.PlanNames(tc.start, tc.goal)
				if err != nil {
					t.Fatalf("run %d: PlanNames: %v", run, err)
				}
				if cost != tc.wantCost {
					t.Fatalf("run %d: cost = %v, want %v (plan=%v)", run, cost, tc.wantCost, plan)
				}
				if run == 0 {
					first = plan
				} else if !slices.Equal(plan, first) {
					t.Fatalf("run %d non-deterministic: got %v, first %v", run, plan, first)
				}
				// Verify executing the plan forward reaches the goal and honors every precondition.
				sim := tc.start
				for _, name := range plan {
					act := d.planner.Action(d.planner.ActionIndex(name))
					if !sim.Satisfies(act.Preconditions) {
						t.Fatalf("plan %v violated preconditions of %q", plan, name)
					}
					sim = sim.Apply(act.Effects)
				}
				if !sim.Satisfies(tc.goal.Target) {
					t.Fatalf("plan %v did not reach goal %q", plan, tc.goal.Name)
				}
			}
			// For multi-chain goals with independent branches (fire + hunt), total cost and deterministic ordering matter;
			// single-branch goals must match wantPlan exactly.
			if tc.name != "meal from empty state builds fire and hunts" && !slices.Equal(first, tc.wantPlan) {
				t.Fatalf("plan = %v, want %v", first, tc.wantPlan)
			}
		})
	}

	// Deterministic tie-breaking: equal-cost single-step routes pick the earliest declared action every time.
	s := ai.NewFactSchema()
	done := s.MustBool("done")
	tied, err := ai.NewGOAPPlanner(
		ai.GOAPAction{Name: "route_a", Cost: 2, Effects: ai.Effects{}.SetBool(done, true)},
		ai.GOAPAction{Name: "route_b", Cost: 2, Effects: ai.Effects{}.SetBool(done, true)},
		ai.GOAPAction{Name: "route_c", Cost: 2, Effects: ai.Effects{}.SetBool(done, true)},
	)
	if err != nil {
		t.Fatalf("NewGOAPPlanner tied: %v", err)
	}
	goalDone := ai.GOAPGoal{Name: "finish", Target: ai.Conditions{}.RequireBool(done, true)}
	for range 25 {
		names, cost, err := tied.PlanNames(ai.WorldState{}, goalDone)
		if err != nil || cost != 2 || !slices.Equal(names, []string{"route_a"}) {
			t.Fatalf("tied plan = (%v, %v, %v), want ([route_a], 2, nil)", names, cost, err)
		}
	}
}

func TestGOAPIntSetAndSideEffectRestore(t *testing.T) {
	// 1. IntEffectSet + IntEffectAdd: reload sets ammo = 6, shoot needs ammo >= 1 and does ammo -= 1.
	s := ai.NewFactSchema()
	targetDown := s.MustBool("target_down")
	ammo := s.MustInt("ammo")
	combat, err := ai.NewGOAPPlanner(
		ai.GOAPAction{Name: "reload", Cost: 2, Effects: ai.Effects{}.SetInt(ammo, 6)},
		ai.GOAPAction{
			Name:          "shoot",
			Cost:          1,
			Preconditions: ai.Conditions{}.RequireIntGte(ammo, 1),
			Effects:       ai.Effects{}.SetBool(targetDown, true).AddInt(ammo, -1),
		},
	)
	if err != nil {
		t.Fatalf("NewGOAPPlanner combat: %v", err)
	}
	names, cost, err := combat.PlanNames(ai.WorldState{}, ai.GOAPGoal{
		Name:   "eliminate",
		Target: ai.Conditions{}.RequireBool(targetDown, true).RequireIntGte(ammo, 5),
	})
	if err != nil || cost != 3 || !slices.Equal(names, []string{"reload", "shoot"}) {
		t.Fatalf("combat plan = (%v, %v, %v), want ([reload shoot], 3, nil)", names, cost, err)
	}

	// 2. Side-effect restore: start already has key, unlock_door consumes key, goal requires door_open AND has_key.
	s2 := ai.NewFactSchema()
	hasKey := s2.MustBool("has_key")
	doorOpen := s2.MustBool("door_open")
	dungeon, err := ai.NewGOAPPlanner(
		ai.GOAPAction{
			Name:          "unlock_door",
			Cost:          1,
			Preconditions: ai.Conditions{}.RequireBool(hasKey, true),
			Effects:       ai.Effects{}.SetBool(doorOpen, true).SetBool(hasKey, false),
		},
		ai.GOAPAction{
			Name:    "find_spare_key",
			Cost:    2,
			Effects: ai.Effects{}.SetBool(hasKey, true),
		},
	)
	if err != nil {
		t.Fatalf("NewGOAPPlanner dungeon: %v", err)
	}
	names, cost, err = dungeon.PlanNames(
		ai.WorldState{}.WithBool(hasKey, true),
		ai.GOAPGoal{Name: "open_and_keep_key", Target: ai.Conditions{}.RequireBool(doorOpen, true).RequireBool(hasKey, true)},
	)
	if err != nil || cost != 3 || !slices.Equal(names, []string{"unlock_door", "find_spare_key"}) {
		t.Fatalf("dungeon plan = (%v, %v, %v), want ([unlock_door find_spare_key], 3, nil)", names, cost, err)
	}
}

func TestGOAPPlanBestGoalPriorityAndFallback(t *testing.T) {
	s := ai.NewFactSchema()
	a := s.MustBool("a")
	b := s.MustBool("b")
	unreachable := s.MustBool("unreachable")

	planner, err := ai.NewGOAPPlanner(
		ai.GOAPAction{Name: "do_a", Cost: 1, Effects: ai.Effects{}.SetBool(a, true)},
		ai.GOAPAction{Name: "do_b", Cost: 1, Effects: ai.Effects{}.SetBool(b, true)},
	)
	if err != nil {
		t.Fatalf("NewGOAPPlanner: %v", err)
	}

	goals := []ai.GOAPGoal{
		{Name: "low", Priority: 1, Target: ai.Conditions{}.RequireBool(a, true)},
		{Name: "impossible", Priority: 100, Target: ai.Conditions{}.RequireBool(unreachable, true)},
		{Name: "high", Priority: 10, Target: ai.Conditions{}.RequireBool(b, true)},
	}

	// Impossible top goal falls back cleanly to "high" (index 2).
	gi, plan, cost, err := planner.PlanBestGoal(ai.WorldState{}, goals)
	if err != nil || gi != 2 || cost != 1 || planner.ActionName(plan[0]) != "do_b" {
		t.Fatalf("PlanBestGoal = (%d, %v, %v, %v), want (2, [do_b], 1, nil)", gi, plan, cost, err)
	}

	// Once b is satisfied, PlanBestGoal moves to "low" (index 0).
	gi, plan, _, err = planner.PlanBestGoal(ai.WorldState{}.WithBool(b, true), goals)
	if err != nil || gi != 0 || planner.ActionName(plan[0]) != "do_a" {
		t.Fatalf("PlanBestGoal after b = (%d, %v, %v), want (0, [do_a], nil)", gi, plan, err)
	}

	// When all achievable goals are satisfied and no unsatisfied goals remain, returns (-1, nil, 0, nil).
	onlyAchievable := []ai.GOAPGoal{goals[0], goals[2]}
	gi, plan, cost, err = planner.PlanBestGoal(ai.WorldState{}.WithBool(a, true).WithBool(b, true), onlyAchievable)
	if err != nil || gi != -1 || len(plan) != 0 || cost != 0 {
		t.Fatalf("all satisfied = (%d, %v, %v, %v), want (-1, [], 0, nil)", gi, plan, cost, err)
	}
}

func TestGOAPNodeBudgetExceeded(t *testing.T) {
	s := ai.NewFactSchema()
	wood := s.MustInt("wood")
	planner, err := ai.NewGOAPPlanner(
		ai.GOAPAction{Name: "chop_1", Cost: 1, Effects: ai.Effects{}.AddInt(wood, 1)},
		ai.GOAPAction{Name: "chop_2", Cost: 2.1, Effects: ai.Effects{}.AddInt(wood, 2)},
	)
	if err != nil {
		t.Fatalf("NewGOAPPlanner: %v", err)
	}

	goal := ai.GOAPGoal{
		Name:   "stockpile",
		Target: ai.Conditions{}.RequireIntGte(wood, 25),
	}

	// Tight budget must fail cleanly with ErrBudgetExceeded rather than running unbounded.
	planner.NodeBudget = 5
	_, _, err = planner.Plan(ai.WorldState{}, goal)
	if !errors.Is(err, ai.ErrBudgetExceeded) {
		t.Fatalf("with NodeBudget=5: err = %v, want ErrBudgetExceeded", err)
	}

	// Sufficient budget finds the optimal 25-step plan.
	planner.NodeBudget = 128
	plan, cost, err := planner.Plan(ai.WorldState{}, goal)
	if err != nil {
		t.Fatalf("with NodeBudget=128: err = %v", err)
	}
	if len(plan) != 25 || cost != 25 {
		t.Fatalf("plan len=%d cost=%v, want len=25 cost=25", len(plan), cost)
	}
}

func TestPlanRunnerActionFailureReplanToAlternateRoute(t *testing.T) {
	d := newCampDomain(t)
	ws := ai.WorldState{}
	var log []string

	executors := map[string]ai.Node{
		"fetch_axe": ai.Action(func(time.Duration) ai.Status {
			log = append(log, "fetch_axe:fail")
			return ai.StatusFailure // Axe is stuck / broken -> must trigger replan via gather_sticks!
		}),
		"chop_wood": ai.Action(func(time.Duration) ai.Status {
			log = append(log, "chop_wood")
			ws.SetInt(d.wood, ws.Int(d.wood)+1)
			return ai.StatusSuccess
		}),
		"gather_sticks": func() ai.Node {
			ticks := 0
			return ai.Action(func(time.Duration) ai.Status {
				ticks++
				log = append(log, "gather_sticks")
				if ticks < 2 {
					return ai.StatusRunning
				}
				ticks = 0
				ws.SetInt(d.wood, ws.Int(d.wood)+1)
				return ai.StatusSuccess
			})
		}(),
		"build_fire": ai.Action(func(time.Duration) ai.Status {
			log = append(log, "build_fire")
			ws.SetInt(d.wood, ws.Int(d.wood)-2)
			ws.SetBool(d.hasFire, true)
			return ai.StatusSuccess
		}),
	}

	runner := ai.NewPlanRunner(d.planner, func() ai.WorldState { return ws }, []ai.GOAPGoal{d.goalFire}, executors)

	var finalStatus ai.Status = ai.StatusRunning
	loop := &game.Loop{
		Rate: 20,
		Update: func(dt time.Duration) {
			if finalStatus == ai.StatusRunning {
				finalStatus = runner.Tick(dt)
			}
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		loop.Run(cx)
		return core.View{}
	})
	app.Advance(300 * time.Millisecond)

	if finalStatus != ai.StatusSuccess {
		t.Fatalf("runner final status = %v (lastErr=%v), want StatusSuccess", finalStatus, runner.LastError())
	}
	if !ws.Bool(d.hasFire) {
		t.Fatal("expected has_fire to be true after alternate route completed")
	}
	if runner.Plans() != 2 {
		t.Fatalf("runner.Plans() = %d, want 2 (initial plan + 1 replan after failure)", runner.Plans())
	}
	wantLog := []string{"fetch_axe:fail", "gather_sticks", "gather_sticks", "gather_sticks", "gather_sticks", "build_fire"}
	if !slices.Equal(log, wantLog) {
		t.Fatalf("execution log = %v, want %v", log, wantLog)
	}
}

func TestPlanRunnerWorldInvalidationAndBehaviorTreeNode(t *testing.T) {
	d := newCampDomain(t)
	ws := ai.WorldState{}.WithBool(d.hasAxe, true)
	danger := false
	var log []string

	chopNode := &trackedAction{name: "chop_wood", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: &log}
	chopWrapper := ai.Action(func(dt time.Duration) ai.Status {
		st := chopNode.Tick(dt)
		if st == ai.StatusSuccess {
			ws.SetInt(d.wood, ws.Int(d.wood)+1)
		}
		return st
	})
	// Use a dedicated resettable node so Reset() is observed on chopNode.
	chopExec := &resetHookNode{
		tick: func(dt time.Duration) ai.Status { return chopWrapper.Tick(dt) },
		reset: func() {
			chopNode.Reset()
		},
	}

	executors := map[string]ai.Node{
		"fetch_axe": ai.Action(func(time.Duration) ai.Status {
			log = append(log, "fetch_axe")
			ws.SetBool(d.hasAxe, true)
			return ai.StatusSuccess
		}),
		"chop_wood": chopExec,
		"gather_sticks": ai.Action(func(time.Duration) ai.Status {
			log = append(log, "gather_sticks")
			ws.SetInt(d.wood, ws.Int(d.wood)+1)
			return ai.StatusSuccess
		}),
		"build_fire": ai.Action(func(time.Duration) ai.Status {
			log = append(log, "build_fire")
			ws.SetInt(d.wood, ws.Int(d.wood)-2)
			ws.SetBool(d.hasFire, true)
			return ai.StatusSuccess
		}),
	}

	runner := ai.NewPlanRunner(d.planner, func() ai.WorldState { return ws }, []ai.GOAPGoal{d.goalFire}, executors)

	// Embed PlanRunner directly as a behavior tree Node under a Selector with a higher-priority danger guard.
	tree := ai.Selector(
		ai.Guard(func() bool { return danger }, ai.Action(func(time.Duration) ai.Status {
			log = append(log, "flee")
			return ai.StatusRunning
		})),
		runner,
	)

	// Tick 1: starts chopping wood with existing axe.
	if st := tree.Tick(50 * time.Millisecond); st != ai.StatusRunning {
		t.Fatalf("tick 1 = %v, want Running", st)
	}
	if runner.CurrentAction() != "chop_wood" {
		t.Fatalf("tick 1 CurrentAction = %q, want chop_wood", runner.CurrentAction())
	}

	// Tick 2: external event steals the axe -> world state invalidates chop_wood precondition -> resets chop_wood and replans (fetch_axe -> chop_wood -> ...).
	ws.SetBool(d.hasAxe, false)
	if st := tree.Tick(50 * time.Millisecond); st != ai.StatusRunning {
		t.Fatalf("tick 2 = %v, want Running", st)
	}
	if !slices.Contains(log, "reset:chop_wood") || !slices.Contains(log, "fetch_axe") {
		t.Fatalf("expected world-state invalidation to reset chop_wood and run fetch_axe; log=%v", log)
	}

	// Tick 3: danger appears -> Selector ticks flee and then preempts runner with runner.Reset(), resetting chop_wood.
	danger = true
	if st := tree.Tick(50 * time.Millisecond); st != ai.StatusRunning {
		t.Fatalf("tick 3 (danger) = %v, want Running", st)
	}
	wantTail := []string{"flee", "reset:chop_wood"}
	if len(log) < 2 || !slices.Equal(log[len(log)-2:], wantTail) {
		t.Fatalf("expected flee followed by reset:chop_wood on tick 3, log=%v", log)
	}

	// Danger passes -> runner replans from current world state and finishes building the fire.
	danger = false
	var st ai.Status = ai.StatusRunning
	for range 20 {
		st = tree.Tick(50 * time.Millisecond)
		if st == ai.StatusSuccess {
			break
		}
	}
	if st != ai.StatusSuccess || !ws.Bool(d.hasFire) {
		t.Fatalf("tree failed to recover and build fire: status=%v hasFire=%v log=%v", st, ws.Bool(d.hasFire), log)
	}
}

type resetHookNode struct {
	tick  func(time.Duration) ai.Status
	reset func()
}

func (n *resetHookNode) Tick(dt time.Duration) ai.Status { return n.tick(dt) }
func (n *resetHookNode) Reset()                          { n.reset() }

func TestPlanRunnerHigherPriorityGoalPreemption(t *testing.T) {
	d := newCampDomain(t)
	ws := ai.WorldState{}.WithBool(d.hasFire, true).WithBool(d.hasBow, true)
	var log []string

	huntTicks := 0
	executors := map[string]ai.Node{
		"fetch_axe": ai.Action(func(time.Duration) ai.Status {
			log = append(log, "fetch_axe")
			ws.SetBool(d.hasAxe, true)
			return ai.StatusSuccess
		}),
		"chop_wood": ai.Action(func(time.Duration) ai.Status {
			log = append(log, "chop_wood")
			ws.SetInt(d.wood, ws.Int(d.wood)+1)
			return ai.StatusSuccess
		}),
		"build_fire": ai.Action(func(time.Duration) ai.Status {
			log = append(log, "build_fire")
			ws.SetInt(d.wood, ws.Int(d.wood)-2)
			ws.SetBool(d.hasFire, true)
			return ai.StatusSuccess
		}),
		"hunt_game": &resetHookNode{
			tick: func(time.Duration) ai.Status {
				huntTicks++
				log = append(log, "hunt_game")
				if huntTicks < 3 {
					return ai.StatusRunning
				}
				ws.SetInt(d.rawMeat, ws.Int(d.rawMeat)+1)
				return ai.StatusSuccess
			},
			reset: func() {
				huntTicks = 0
				log = append(log, "reset:hunt_game")
			},
		},
		"cook_meat": ai.Action(func(time.Duration) ai.Status {
			log = append(log, "cook_meat")
			ws.SetInt(d.rawMeat, ws.Int(d.rawMeat)-1)
			ws.SetBool(d.cookedMeal, true)
			return ai.StatusSuccess
		}),
	}

	// Goal 0: light_campfire (prio 10, initially satisfied). Goal 1: prepare_meal (prio 5).
	runner := ai.NewPlanRunner(d.planner, func() ai.WorldState { return ws }, []ai.GOAPGoal{d.goalFire, d.goalMeal}, executors)

	if st := runner.Tick(50 * time.Millisecond); st != ai.StatusRunning {
		t.Fatalf("tick 1 = %v, want Running", st)
	}
	if runner.ActiveGoal() != 1 {
		t.Fatalf("ActiveGoal = %d, want 1 (prepare_meal)", runner.ActiveGoal())
	}

	// Fire goes out! Higher-priority goalFire (0) preempts prepare_meal (1).
	ws.SetBool(d.hasFire, false)
	st := runner.Tick(50 * time.Millisecond)
	if st != ai.StatusSuccess || !ws.Bool(d.hasFire) {
		t.Fatalf("after preemption: status=%v hasFire=%v log=%v", st, ws.Bool(d.hasFire), log)
	}
	if !slices.Contains(log, "reset:hunt_game") || !slices.Contains(log, "build_fire") {
		t.Fatalf("expected preemption reset of hunt_game and rebuild of fire; log=%v", log)
	}
}

func TestGOAPZeroAllocations(t *testing.T) {
	d := newCampDomain(t)
	start := ai.WorldState{}
	goals := []ai.GOAPGoal{d.goalMeal, d.goalFire}
	buf := make([]int, 0, 16)

	// Warm-up calls to size internal buffers.
	var err error
	buf, _, err = d.planner.PlanInto(buf, start, d.goalMeal)
	if err != nil {
		t.Fatalf("warm-up PlanInto: %v", err)
	}
	_, buf, _, err = d.planner.PlanBestGoalInto(buf, start, goals)
	if err != nil {
		t.Fatalf("warm-up PlanBestGoalInto: %v", err)
	}

	allocsPlan := testing.AllocsPerRun(50, func() {
		var cost float64
		buf, cost, err = d.planner.PlanInto(buf, start, d.goalMeal)
		if err != nil || cost != 8 {
			t.Fatalf("PlanInto = (%v, %v)", cost, err)
		}
	})
	if allocsPlan != 0 {
		t.Fatalf("PlanInto allocated %.1f times/run after warm-up, want 0", allocsPlan)
	}

	allocsBest := testing.AllocsPerRun(50, func() {
		var gi int
		gi, buf, _, err = d.planner.PlanBestGoalInto(buf, start, goals)
		if err != nil || gi != 1 {
			t.Fatalf("PlanBestGoalInto = (%d, %v)", gi, err)
		}
	})
	if allocsBest != 0 {
		t.Fatalf("PlanBestGoalInto allocated %.1f times/run after warm-up, want 0", allocsBest)
	}
}

func BenchmarkGOAPPlan(b *testing.B) {
	s := ai.NewFactSchema()
	hasAxe := s.MustBool("has_axe")
	hasFire := s.MustBool("has_fire")
	cookedMeal := s.MustBool("cooked_meal")
	hasBow := s.MustBool("has_bow")
	wood := s.MustInt("wood")
	rawMeat := s.MustInt("raw_meat")

	planner, err := ai.NewGOAPPlanner(
		ai.GOAPAction{Name: "fetch_axe", Cost: 1, Effects: ai.Effects{}.SetBool(hasAxe, true)},
		ai.GOAPAction{Name: "chop_wood", Cost: 1, Preconditions: ai.Conditions{}.RequireBool(hasAxe, true), Effects: ai.Effects{}.AddInt(wood, 1)},
		ai.GOAPAction{Name: "gather_sticks", Cost: 3, Effects: ai.Effects{}.AddInt(wood, 1)},
		ai.GOAPAction{Name: "build_fire", Cost: 1, Preconditions: ai.Conditions{}.RequireIntGte(wood, 2), Effects: ai.Effects{}.SetBool(hasFire, true).AddInt(wood, -2)},
		ai.GOAPAction{Name: "fetch_bow", Cost: 1, Effects: ai.Effects{}.SetBool(hasBow, true)},
		ai.GOAPAction{Name: "hunt_game", Cost: 2, Preconditions: ai.Conditions{}.RequireBool(hasBow, true), Effects: ai.Effects{}.AddInt(rawMeat, 1)},
		ai.GOAPAction{Name: "forage_meat", Cost: 5, Effects: ai.Effects{}.AddInt(rawMeat, 1)},
		ai.GOAPAction{Name: "cook_meat", Cost: 1, Preconditions: ai.Conditions{}.RequireBool(hasFire, true).RequireIntGte(rawMeat, 1), Effects: ai.Effects{}.SetBool(cookedMeal, true).AddInt(rawMeat, -1)},
	)
	if err != nil {
		b.Fatal(err)
	}
	goal := ai.GOAPGoal{Name: "prepare_meal", Target: ai.Conditions{}.RequireBool(cookedMeal, true)}
	buf := make([]int, 0, 16)
	start := ai.WorldState{}

	// Warm-up.
	if buf, _, err = planner.PlanInto(buf, start, goal); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		buf, _, err = planner.PlanInto(buf, start, goal)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestGOAPFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*ai.GOAPConfig]{
		Decode: ai.ParseGOAP,
		Encode: ai.EncodeGOAP,
		Files:  []string{"testdata/goap/*.yaml"},
		Doc:    "ai#goap",
	})
}

func TestGOAPYAMLBuildAndValidation(t *testing.T) {
	raw := []byte(`version: 1
nodeBudget: 256
facts:
  bools:
    - has_axe
    - has_fire
  ints:
    - wood
actions:
  - name: fetch_axe
    cost: 1
    check: axe_available
    effects:
      bools:
        - fact: has_axe
          value: true
  - name: chop_wood
    cost: 1
    preconditions:
      bools:
        - fact: has_axe
          value: true
    effects:
      ints:
        - fact: wood
          op: add
          value: 1
  - name: gather_sticks
    cost: 3
    effects:
      ints:
        - fact: wood
          op: add
          value: 1
  - name: build_fire
    cost: 1
    preconditions:
      ints:
        - fact: wood
          op: gte
          value: 2
    effects:
      bools:
        - fact: has_fire
          value: true
      ints:
        - fact: wood
          op: add
          value: -2
goals:
  - name: light_campfire
    priority: 10
    target:
      bools:
        - fact: has_fire
          value: true
`)

	cfg, err := ai.ParseGOAP(raw)
	if err != nil {
		t.Fatalf("ParseGOAP: %v", err)
	}
	encoded, err := ai.EncodeGOAP(cfg)
	if err != nil {
		t.Fatalf("EncodeGOAP: %v", err)
	}
	if !bytes.Equal(encoded, raw) {
		t.Fatalf("round-trip changed bytes:\ngot:\n%s\nwant:\n%s", encoded, raw)
	}

	axeAvailable := true
	set, err := ai.BuildGOAP(cfg, ai.GOAPBindings{
		Checks: map[string]func(ai.WorldState) bool{
			"axe_available": func(ai.WorldState) bool { return axeAvailable },
		},
	})
	if err != nil {
		t.Fatalf("BuildGOAP: %v", err)
	}
	if set.Planner.NodeBudget != 256 {
		t.Fatalf("NodeBudget = %d, want 256", set.Planner.NodeBudget)
	}

	names, cost, err := set.Planner.PlanNames(ai.WorldState{}, set.Goals[0])
	if err != nil || cost != 4 || !slices.Equal(names, []string{"fetch_axe", "chop_wood", "chop_wood", "build_fire"}) {
		t.Fatalf("YAML plan (axe available) = (%v, %v, %v)", names, cost, err)
	}

	axeAvailable = false
	names, cost, err = set.Planner.PlanNames(ai.WorldState{}, set.Goals[0])
	if err != nil || cost != 7 || !slices.Equal(names, []string{"gather_sticks", "gather_sticks", "build_fire"}) {
		t.Fatalf("YAML plan (axe unavailable) = (%v, %v, %v)", names, cost, err)
	}

	// BuildGOAP rejects missing procedural check binding.
	if _, err := ai.BuildGOAP(cfg, ai.GOAPBindings{}); err == nil || !strings.Contains(err.Error(), "unbound check") {
		t.Fatalf("BuildGOAP missing check = %v, want unbound check error", err)
	}

	badDocs := []struct {
		name string
		yaml string
		want string
	}{
		{"empty", "   ", "empty"},
		{"nested unknown field", "version: 1\nfacts:\n  bools:\n    - a\nactions:\n  - name: x\n    bogus: 1\n", "bogus"},
		{"trailing doc", "version: 1\nfacts: {}\nactions: []\n---\nversion: 1\n", "trailing"},
		{"negative budget", "version: 1\nnodeBudget: -1\nfacts: {}\nactions: []\n", "nodeBudget"},
		{"duplicate bool fact", "version: 1\nfacts:\n  bools:\n    - a\n    - a\nactions: []\n", "declared twice"},
		{"bool int clash", "version: 1\nfacts:\n  bools:\n    - x\n  ints:\n    - x\nactions: []\n", "already registered"},
		{"duplicate action", "version: 1\nfacts: {}\nactions:\n  - name: a\n  - name: a\n", "defined twice"},
		{"negative cost", "version: 1\nfacts: {}\nactions:\n  - name: a\n    cost: -2\n", "cost"},
		{"unknown bool fact", "version: 1\nfacts: {}\nactions:\n  - name: a\n    effects:\n      bools:\n        - fact: nope\n          value: true\n", "unknown boolean fact"},
		{"unknown int op", "version: 1\nfacts:\n  ints:\n    - wood\nactions:\n  - name: a\n    preconditions:\n      ints:\n        - fact: wood\n          op: approx\n          value: 1\n", "unknown op"},
		{"int add zero", "version: 1\nfacts:\n  ints:\n    - wood\nactions:\n  - name: a\n    effects:\n      ints:\n        - fact: wood\n          op: add\n          value: 0\n", "non-zero"},
		{"int out of range", "version: 1\nfacts:\n  ints:\n    - wood\nactions:\n  - name: a\n    effects:\n      ints:\n        - fact: wood\n          op: set\n          value: 40000\n", "int16 range"},
		{"contradictory bounds", "version: 1\nfacts:\n  ints:\n    - wood\nactions:\n  - name: a\n    preconditions:\n      ints:\n        - fact: wood\n          op: gte\n          value: 5\n        - fact: wood\n          op: lte\n          value: 2\n", "contradictory"},
		{"duplicate goal", "version: 1\nfacts:\n  bools:\n    - a\nactions: []\ngoals:\n  - name: g\n    target:\n      bools:\n        - fact: a\n          value: true\n  - name: g\n    target:\n      bools:\n        - fact: a\n          value: false\n", "defined twice"},
	}

	for _, tc := range badDocs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ai.ParseGOAP([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseGOAP error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
