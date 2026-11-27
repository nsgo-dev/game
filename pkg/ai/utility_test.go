package ai_test

import (
	"bytes"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/random"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func almostEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestResponseCurves(t *testing.T) {
	// Zero value acts as identity clamped to [0, 1].
	var id ai.ResponseCurve
	if got := id.Eval(0.4); !almostEqual(got, 0.4, 1e-9) {
		t.Fatalf("identity(0.4) = %v, want 0.4", got)
	}
	if got := id.Eval(-2); got != 0 {
		t.Fatalf("identity(-2) = %v, want 0", got)
	}
	if got := id.Eval(3); got != 1 {
		t.Fatalf("identity(3) = %v, want 1", got)
	}
	if got := id.Eval(math.NaN()); got != 0 {
		t.Fatalf("identity(NaN) = %v, want 0", got)
	}

	// Linear: inverted y = 1 - x.
	lin := ai.LinearCurve(-1, 0, 1)
	if !almostEqual(lin.Eval(0), 1, 1e-9) || !almostEqual(lin.Eval(0.25), 0.75, 1e-9) || !almostEqual(lin.Eval(1), 0, 1e-9) {
		t.Fatalf("linear inverted unexpected values: %v %v %v", lin.Eval(0), lin.Eval(0.25), lin.Eval(1))
	}

	// Polynomial: quadratic y = x^2 and fractional power with negative shift.
	quad := ai.PolynomialCurve(1, 2, 0, 0)
	if !almostEqual(quad.Eval(0.5), 0.25, 1e-9) || !almostEqual(quad.Eval(1), 1, 1e-9) {
		t.Fatalf("quadratic unexpected values: %v %v", quad.Eval(0.5), quad.Eval(1))
	}
	sqrtShifted := ai.PolynomialCurve(1, 0.5, 0.5, 0)
	if got := sqrtShifted.Eval(0.2); got != 0 || math.IsNaN(got) {
		t.Fatalf("fractional power with negative base = %v, want 0", got)
	}
	if got := sqrtShifted.Eval(0.75); !almostEqual(got, 0.5, 1e-9) {
		t.Fatalf("sqrt(0.75-0.5) = %v, want 0.5", got)
	}

	// Logistic: S-curve centered at 0.5.
	logi := ai.LogisticCurve(1, 10, 0.5, 0)
	if !almostEqual(logi.Eval(0.5), 0.5, 1e-9) {
		t.Fatalf("logistic(0.5) = %v, want 0.5", logi.Eval(0.5))
	}
	if logi.Eval(0.1) >= 0.05 || logi.Eval(0.9) <= 0.95 {
		t.Fatalf("logistic ends not steep enough: 0.1->%v, 0.9->%v", logi.Eval(0.1), logi.Eval(0.9))
	}

	// Logit: symmetric inverse S-curve centered at (0.5, 0.5).
	logit := ai.LogitCurve(0.1, 0.5, 0.5)
	if !almostEqual(logit.Eval(0.5), 0.5, 1e-9) {
		t.Fatalf("logit(0.5) = %v, want 0.5", logit.Eval(0.5))
	}
	if logit.Eval(0) != 0 || logit.Eval(1) != 1 {
		t.Fatalf("logit endpoints = (%v, %v), want (0, 1)", logit.Eval(0), logit.Eval(1))
	}
	if !almostEqual(logit.Eval(0.1)+logit.Eval(0.9), 1.0, 1e-9) {
		t.Fatalf("logit not symmetric: f(0.1)=%v, f(0.9)=%v", logit.Eval(0.1), logit.Eval(0.9))
	}
	invLogit := ai.LogitCurve(-0.1, 0.5, 0.5)
	if invLogit.Eval(0) != 1 || invLogit.Eval(1) != 0 {
		t.Fatalf("inverted logit endpoints = (%v, %v), want (1, 0)", invLogit.Eval(0), invLogit.Eval(1))
	}

	// Step: threshold at 0.4.
	step := ai.StepCurve(0.4, 0.1, 0.9)
	if !almostEqual(step.Eval(0.39), 0.1, 1e-9) || !almostEqual(step.Eval(0.4), 0.9, 1e-9) {
		t.Fatalf("step unexpected values: %v %v", step.Eval(0.39), step.Eval(0.4))
	}

	// Resource curve (*resource.Curve satisfies ai.CurveEvaluator directly).
	resCurve, err := resource.ParseAs[*resource.Curve]([]byte(`type: curve
version: 1
name: urgency
keys:
  - time: 0
    value: 0
  - time: 0.5
    value: 0.8
  - time: 1
    value: 1
`))
	if err != nil {
		t.Fatalf("parse resource curve: %v", err)
	}
	rc := ai.ResourceCurve(resCurve)
	if !almostEqual(rc.Eval(0), 0, 1e-6) || !almostEqual(rc.Eval(0.5), 0.8, 1e-6) || !almostEqual(rc.Eval(0.75), 0.9, 1e-6) {
		t.Fatalf("resource curve eval unexpected: %v %v %v", rc.Eval(0), rc.Eval(0.5), rc.Eval(0.75))
	}

	// Custom curve function.
	custom := ai.CustomCurve(func(x float64) float64 { return math.Sin(x * math.Pi / 2) })
	if !almostEqual(custom.Eval(0.5), math.Sin(math.Pi/4), 1e-6) {
		t.Fatalf("custom curve(0.5) = %v, want %v", custom.Eval(0.5), math.Sin(math.Pi/4))
	}
}

func TestActionCompensationFactorAndVeto(t *testing.T) {
	constVal := func(v float64) ai.Consideration[struct{}] {
		return ai.Consideration[struct{}]{
			Input: func(struct{}) float64 { return v },
			Curve: ai.LinearCurve(1, 0, 0),
		}
	}

	// N=1: modFactor = 0 -> score unchanged.
	a1 := ai.UtilityAction[struct{}]{
		Name:           "one",
		Weight:         1,
		Considerations: []ai.Consideration[struct{}]{constVal(0.8)},
	}
	if got := a1.Score(struct{}{}, 0); !almostEqual(got, 0.8, 1e-9) {
		t.Fatalf("N=1 score = %v, want 0.8", got)
	}

	// N=2: product = 0.64, modFactor = 0.5, makeUp = 0.36*0.5 = 0.18, final = 0.64 + 0.18*0.64 = 0.7552.
	a2 := ai.UtilityAction[struct{}]{
		Name:           "two",
		Weight:         1,
		Considerations: []ai.Consideration[struct{}]{constVal(0.8), constVal(0.8)},
	}
	if got := a2.Score(struct{}{}, 0); !almostEqual(got, 0.7552, 1e-9) {
		t.Fatalf("N=2 score = %v, want 0.7552", got)
	}

	// Weight scales compensated score; zero weight defaults to 1; negative disables.
	a2Weighted := a2
	a2Weighted.Weight = 1.5
	if got := a2Weighted.Score(struct{}{}, 0.1); !almostEqual(got, 0.7552*1.5+0.1, 1e-9) {
		t.Fatalf("weighted+momentum score = %v, want %v", got, 0.7552*1.5+0.1)
	}
	a2ZeroWeight := a2
	a2ZeroWeight.Weight = 0
	if got := a2ZeroWeight.Score(struct{}{}, 0); !almostEqual(got, 0.7552, 1e-9) {
		t.Fatalf("zero weight should default to 1: got %v", got)
	}
	a2Disabled := a2
	a2Disabled.Weight = -1
	if got := a2Disabled.Score(struct{}{}, 0.5); got != 0 {
		t.Fatalf("negative weight should score 0 even with momentum: got %v", got)
	}

	// Veto: a 0 consideration yields 0 even with momentum bonus.
	vetoed := ai.UtilityAction[struct{}]{
		Name:           "vetoed",
		Weight:         2,
		Considerations: []ai.Consideration[struct{}]{constVal(0.9), constVal(0)},
	}
	if got := vetoed.Score(struct{}{}, 0.5); got != 0 {
		t.Fatalf("vetoed action must score 0 even with momentum, got %v", got)
	}
}

type combatState struct {
	hp       float64 // 0..100
	ammo     float64 // 0..10
	potions  float64 // count
	enemyDst float64 // 0..200
	threat   float64 // 0..1
}

func newReferenceReasoner() *ai.Reasoner[combatState] {
	return ai.NewReasoner(
		ai.UtilityAction[combatState]{
			Name:   "flee",
			Weight: 1.3,
			Considerations: []ai.Consideration[combatState]{
				{
					Name:  "critical_hp",
					Input: func(s combatState) float64 { return s.hp },
					Min:   0, Max: 100,
					Curve: ai.LogisticCurve(1, -14, 0.25, 0),
				},
				{
					Name:  "high_threat",
					Input: func(s combatState) float64 { return s.threat },
					Curve: ai.StepCurve(0.5, 0, 1),
				},
			},
		},
		ai.UtilityAction[combatState]{
			Name:   "heal",
			Weight: 1.2,
			Considerations: []ai.Consideration[combatState]{
				{
					Name:  "low_hp",
					Input: func(s combatState) float64 { return s.hp },
					Min:   0, Max: 100,
					Curve: ai.PolynomialCurve(-1, 2, 0, 1),
				},
				{
					Name:  "has_potion",
					Input: func(s combatState) float64 { return s.potions },
					Curve: ai.StepCurve(1, 0, 1),
				},
			},
		},
		ai.UtilityAction[combatState]{
			Name:   "reload",
			Weight: 1.0,
			Considerations: []ai.Consideration[combatState]{
				{
					Name:  "empty_ammo",
					Input: func(s combatState) float64 { return s.ammo },
					Min:   0, Max: 10,
					Curve: ai.StepCurve(0.05, 1, 0),
				},
			},
		},
		ai.UtilityAction[combatState]{
			Name:   "attack",
			Weight: 1.1,
			Considerations: []ai.Consideration[combatState]{
				{
					Name:  "enemy_close",
					Input: func(s combatState) float64 { return s.enemyDst },
					Min:   0, Max: 200,
					Curve: ai.LinearCurve(-1, 0, 1),
				},
				{
					Name:  "has_ammo",
					Input: func(s combatState) float64 { return s.ammo },
					Min:   0, Max: 10,
					Curve: ai.StepCurve(0.05, 0, 1),
				},
			},
		},
		ai.UtilityAction[combatState]{
			Name:   "patrol",
			Weight: 0.2,
		},
	)
}

func TestReferenceAgentInputTableAndTieBreaking(t *testing.T) {
	r := newReferenceReasoner()

	cases := []struct {
		name  string
		state combatState
		want  string
	}{
		{
			name:  "critical hp under heavy threat flees",
			state: combatState{hp: 10, ammo: 5, potions: 0, enemyDst: 40, threat: 0.9},
			want:  "flee",
		},
		{
			name:  "low hp with potion and safe threat heals",
			state: combatState{hp: 25, ammo: 5, potions: 2, enemyDst: 120, threat: 0.2},
			want:  "heal",
		},
		{
			name:  "healthy with empty magazine reloads",
			state: combatState{hp: 90, ammo: 0, potions: 1, enemyDst: 50, threat: 0.3},
			want:  "reload",
		},
		{
			name:  "healthy with ammo and close enemy attacks",
			state: combatState{hp: 90, ammo: 8, potions: 1, enemyDst: 20, threat: 0.3},
			want:  "attack",
		},
		{
			name:  "healthy with no enemy in range patrols",
			state: combatState{hp: 100, ammo: 10, potions: 1, enemyDst: 200, threat: 0},
			want:  "patrol",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r.Reset()
			idx, score := r.Choose(tc.state, nil)
			if idx < 0 || score <= 0 {
				t.Fatalf("Choose returned (%d, %v), want positive score", idx, score)
			}
			if got := r.ActionName(idx); got != tc.want {
				scores := r.ScoreAll(tc.state, -1, nil)
				t.Fatalf("picked %q, want %q (scores=%v)", got, tc.want, scores)
			}
		})
	}

	// Deterministic tie-breaking: identical scores pick the earliest action in declaration order.
	tied := ai.NewReasoner(
		ai.UtilityAction[struct{}]{Name: "first", Weight: 0.5},
		ai.UtilityAction[struct{}]{Name: "second", Weight: 0.5},
		ai.UtilityAction[struct{}]{Name: "third", Weight: 0.5},
	)
	for range 20 {
		idx, _ := tied.Choose(struct{}{}, nil)
		if tied.ActionName(idx) != "first" {
			t.Fatalf("tie broke to %q, want %q", tied.ActionName(idx), "first")
		}
	}
}

func TestTopNWeightedRandomDeterminism(t *testing.T) {
	makeReasoner := func() *ai.Reasoner[combatState] {
		r := ai.NewReasoner(
			ai.UtilityAction[combatState]{Name: "a", Weight: 0.6},
			ai.UtilityAction[combatState]{Name: "b", Weight: 0.3},
			ai.UtilityAction[combatState]{Name: "c", Weight: 0.1},
			ai.UtilityAction[combatState]{
				Name:   "zero",
				Weight: 10,
				Considerations: []ai.Consideration[combatState]{
					{Input: func(combatState) float64 { return 0 }, Curve: ai.LinearCurve(1, 0, 0)},
				},
			},
		)
		r.TopN = 2
		return r
	}

	run := func(seed uint64) []string {
		r := makeReasoner()
		rng := random.New(seed)
		out := make([]string, 64)
		for i := range out {
			idx, _ := r.ChooseWithCurrent(combatState{}, -1, rng)
			out[i] = r.ActionName(idx)
		}
		return out
	}

	seq1 := run(42)
	seq2 := run(42)
	if !slices.Equal(seq1, seq2) {
		t.Fatalf("same seed produced different choices:\n%v\n%v", seq1, seq2)
	}

	// With TopN=2, only "a" and "b" may be chosen (never "c" or vetoed "zero"), and both appear.
	counts := map[string]int{}
	for _, name := range seq1 {
		counts[name]++
	}
	if counts["c"] != 0 || counts["zero"] != 0 {
		t.Fatalf("TopN=2 picked outside top 2: counts=%v", counts)
	}
	if counts["a"] == 0 || counts["b"] == 0 {
		t.Fatalf("TopN=2 expected both top actions to be sampled, got counts=%v", counts)
	}
	if counts["a"] <= counts["b"] {
		t.Fatalf("weighted sampling expected a (0.6) > b (0.3), got counts=%v", counts)
	}
}

func TestMomentumPreventsOscillation(t *testing.T) {
	type twin struct{ a, b float64 }

	build := func(momentum float64) *ai.Reasoner[twin] {
		r := ai.NewReasoner(
			ai.UtilityAction[twin]{
				Name: "left",
				Considerations: []ai.Consideration[twin]{
					{Input: func(s twin) float64 { return s.a }, Curve: ai.LinearCurve(1, 0, 0)},
				},
			},
			ai.UtilityAction[twin]{
				Name: "right",
				Considerations: []ai.Consideration[twin]{
					{Input: func(s twin) float64 { return s.b }, Curve: ai.LinearCurve(1, 0, 0)},
				},
			},
		)
		r.Momentum = momentum
		return r
	}

	// Inputs oscillate slightly between (0.52, 0.50) and (0.50, 0.52) every step.
	without := build(0)
	with := build(0.1)

	switchesWithout := 0
	switchesWith := 0
	prevWithout := -1
	prevWith := -1

	for step := range 20 {
		st := twin{a: 0.52, b: 0.50}
		if step%2 == 1 {
			st = twin{a: 0.50, b: 0.52}
		}
		i0, _ := without.Choose(st, nil)
		i1, _ := with.Choose(st, nil)
		if prevWithout >= 0 && i0 != prevWithout {
			switchesWithout++
		}
		if prevWith >= 0 && i1 != prevWith {
			switchesWith++
		}
		prevWithout = i0
		prevWith = i1
	}

	if switchesWithout != 19 {
		t.Fatalf("without momentum expected 19 switches, got %d", switchesWithout)
	}
	if switchesWith != 0 {
		t.Fatalf("with momentum expected 0 switches, got %d", switchesWith)
	}

	// When the rival clearly overcomes the momentum margin (0.80 > 0.50 + 0.10), the agent switches.
	idx, _ := with.Choose(twin{a: 0.50, b: 0.80}, nil)
	if with.ActionName(idx) != "right" {
		t.Fatalf("expected switch to right when score exceeds momentum, got %q", with.ActionName(idx))
	}

	// When the active action is vetoed (score 0), momentum must not keep it active.
	idx, _ = with.Choose(twin{a: 0.05, b: 0.0}, nil)
	if with.ActionName(idx) != "left" {
		t.Fatalf("vetoed active action should yield to left, got %q", with.ActionName(idx))
	}
}

func TestUtilitySelectorBehaviorTreeNode(t *testing.T) {
	state := combatState{hp: 90, ammo: 8, potions: 1, enemyDst: 20, threat: 0.2}
	r := newReferenceReasoner()
	r.Momentum = 0.1

	var log []string
	attackNode := &trackedAction{name: "attack", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: &log}
	fleeNode := &trackedAction{name: "flee", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: &log}
	patrolNode := &trackedAction{name: "patrol", ticksNeeded: 1, finalStatus: ai.StatusSuccess, log: &log}

	utilNode := ai.UtilitySelector(r, func() combatState { return state }, nil, map[string]ai.Node{
		"attack": attackNode,
		"flee":   fleeNode,
		"patrol": patrolNode,
	})

	step := 0
	loop := &game.Loop{
		Rate: 20,
		Update: func(dt time.Duration) {
			step++
			switch step {
			case 1, 2:
				// Attack starts and keeps running.
			case 3:
				// Sudden lethal danger preempts running attack -> attack must Reset, flee starts.
				state.hp = 5
				state.threat = 0.95
			case 4:
				// Flee finishes on its 2nd tick.
			}
			utilNode.Tick(dt)
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		loop.Run(cx)
		return core.View{}
	})
	app.Advance(200 * time.Millisecond)
	if step != 4 {
		t.Fatalf("ran %d steps, want 4", step)
	}

	want := []string{"attack", "attack", "reset:attack", "flee", "flee"}
	if !slices.Equal(log, want) {
		t.Fatalf("subtree log = %v, want %v", log, want)
	}

	// Reset clears running state and resets all child subtrees.
	utilNode.Reset()
	if attackNode.resets < 2 || fleeNode.resets < 1 {
		t.Fatalf("Reset did not reset subtrees: attack=%d flee=%d", attackNode.resets, fleeNode.resets)
	}
}

type benchAgent struct {
	inputs [6]float64
	active int
}

func makeBenchSetup(topN int) (*ai.Reasoner[*benchAgent], []benchAgent) {
	actions := make([]ai.UtilityAction[*benchAgent], 50)
	for i := range actions {
		k0 := i % 6
		k1 := (i + 1) % 6
		k2 := (i + 2) % 6
		actions[i] = ai.UtilityAction[*benchAgent]{
			Name:   "a",
			Weight: 0.5 + float64(i%10)*0.1,
			Considerations: []ai.Consideration[*benchAgent]{
				{
					Input: func(a *benchAgent) float64 { return a.inputs[k0] },
					Curve: ai.LinearCurve(1, 0, 0),
				},
				{
					Input: func(a *benchAgent) float64 { return a.inputs[k1] },
					Curve: ai.PolynomialCurve(1, 2, 0, 0),
				},
				{
					Input: func(a *benchAgent) float64 { return a.inputs[k2] },
					Curve: ai.LogisticCurve(1, 8, 0.5, 0),
				},
			},
		}
	}
	r := ai.NewReasoner(actions...)
	r.TopN = topN
	r.Momentum = 0.05

	agents := make([]benchAgent, 500)
	for i := range agents {
		agents[i].active = -1
		for j := range agents[i].inputs {
			agents[i].inputs[j] = float64((i*7+j*13)%100) / 100.0
		}
	}
	return r, agents
}

func TestUtilityZeroAllocations(t *testing.T) {
	for _, tc := range []struct {
		name string
		topN int
		rng  *random.Stream
	}{
		{name: "best_action", topN: 1, rng: nil},
		{name: "top3_weighted", topN: 3, rng: random.New(7)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, agents := makeBenchSetup(tc.topN)
			// Warm-up step.
			for i := range agents {
				idx, _ := r.ChooseWithCurrent(&agents[i], agents[i].active, tc.rng)
				agents[i].active = idx
			}
			allocs := testing.AllocsPerRun(10, func() {
				for i := range agents {
					idx, _ := r.ChooseWithCurrent(&agents[i], agents[i].active, tc.rng)
					agents[i].active = idx
				}
			})
			if allocs != 0 {
				t.Fatalf("scoring 50 actions for 500 agents allocated %.1f times/step, want 0", allocs)
			}
		})
	}
}

func BenchmarkUtility50Actions500Agents(b *testing.B) {
	r, agents := makeBenchSetup(1)
	for i := range agents {
		idx, _ := r.ChooseWithCurrent(&agents[i], agents[i].active, nil)
		agents[i].active = idx
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i := range agents {
			idx, _ := r.ChooseWithCurrent(&agents[i], agents[i].active, nil)
			agents[i].active = idx
		}
	}
}

func BenchmarkUtility50Actions500AgentsTopN(b *testing.B) {
	r, agents := makeBenchSetup(3)
	rng := random.New(99)
	for i := range agents {
		idx, _ := r.ChooseWithCurrent(&agents[i], agents[i].active, rng)
		agents[i].active = idx
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i := range agents {
			idx, _ := r.ChooseWithCurrent(&agents[i], agents[i].active, rng)
			agents[i].active = idx
		}
	}
}

func TestUtilityFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*ai.UtilityConfig]{
		Decode: ai.ParseUtility,
		Encode: ai.EncodeUtility,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "ai",
	})
}

func TestUtilityYAMLBuildAndValidation(t *testing.T) {
	raw := []byte(`version: 1
momentum: 0.1
actions:
  - name: attack
    weight: 1.2
    considerations:
      - name: target_close
        input: target_distance
        max: 200
        curve:
          kind: linear
          slope: -1
          shiftY: 1
  - name: flee
    considerations:
      - name: danger
        input: threat_level
        curve:
          kind: resource
          file: curves/flee.curve.yaml
`)

	cfg, err := ai.ParseUtility(raw)
	if err != nil {
		t.Fatalf("ParseUtility: %v", err)
	}
	encoded, err := ai.EncodeUtility(cfg)
	if err != nil {
		t.Fatalf("EncodeUtility: %v", err)
	}
	if !bytes.Equal(encoded, raw) {
		t.Fatalf("round-trip changed bytes:\ngot:\n%s\nwant:\n%s", encoded, raw)
	}

	fleeCurve, err := resource.ParseAs[*resource.Curve]([]byte(`type: curve
version: 1
keys:
  - time: 0
    value: 0
  - time: 1
    value: 1
`))
	if err != nil {
		t.Fatalf("parse resource curve: %v", err)
	}

	loadCalls := 0
	r, err := ai.BuildReasoner(cfg, ai.UtilityBindings[combatState]{
		Inputs: map[string]func(combatState) float64{
			"target_distance": func(s combatState) float64 { return s.enemyDst },
			"threat_level":    func(s combatState) float64 { return s.threat },
		},
		LoadCurve: func(path string) (ai.CurveEvaluator, error) {
			loadCalls++
			if path != "curves/flee.curve.yaml" {
				return nil, errors.New("not found")
			}
			return fleeCurve, nil
		},
	})
	if err != nil {
		t.Fatalf("BuildReasoner: %v", err)
	}
	if loadCalls != 1 {
		t.Fatalf("LoadCurve calls = %d, want 1", loadCalls)
	}

	idx, _ := r.Choose(combatState{enemyDst: 180, threat: 0.9}, nil)
	if r.ActionName(idx) != "flee" {
		t.Fatalf("expected flee, got %q", r.ActionName(idx))
	}
	idx, _ = r.Choose(combatState{enemyDst: 10, threat: 0.1}, nil)
	if r.ActionName(idx) != "attack" {
		t.Fatalf("expected attack, got %q", r.ActionName(idx))
	}

	// Validation error cases.
	badDocs := []struct {
		name string
		yaml string
		want string
	}{
		{"empty", "   ", "empty"},
		{"nested unknown field", "version: 1\nactions:\n  - name: a\n    bogus: 1\n", "bogus"},
		{"trailing doc", "version: 1\nactions: []\n---\nversion: 1\n", "trailing"},
		{"duplicate action", "version: 1\nactions:\n  - name: a\n  - name: a\n", "twice"},
		{"negative weight", "version: 1\nactions:\n  - name: a\n    weight: -1\n", "weight"},
		{"bad range", "version: 1\nactions:\n  - name: a\n    considerations:\n      - input: x\n        min: 10\n        max: 5\n        curve:\n          kind: linear\n          slope: 1\n", "greater than min"},
		{"unknown curve", "version: 1\nactions:\n  - name: a\n    considerations:\n      - input: x\n        curve:\n          kind: spline\n", "unknown curve kind"},
		{"resource missing file", "version: 1\nactions:\n  - name: a\n    considerations:\n      - input: x\n        curve:\n          kind: resource\n", "requires file"},
		{"linear with file", "version: 1\nactions:\n  - name: a\n    considerations:\n      - input: x\n        curve:\n          kind: linear\n          file: foo.yaml\n", "cannot specify file"},
		{"linear with exponent", "version: 1\nactions:\n  - name: a\n    considerations:\n      - input: x\n        curve:\n          kind: linear\n          exponent: 2\n", "exponent"},
	}
	for _, tc := range badDocs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ai.ParseUtility([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseUtility error = %v, want substring %q", err, tc.want)
			}
		})
	}

	// BuildReasoner rejects missing input or missing LoadCurve.
	if _, err := ai.BuildReasoner(cfg, ai.UtilityBindings[combatState]{}); err == nil || !strings.Contains(err.Error(), "unbound input") {
		t.Fatalf("BuildReasoner missing input = %v", err)
	}
	if _, err := ai.BuildReasoner(cfg, ai.UtilityBindings[combatState]{
		Inputs: map[string]func(combatState) float64{
			"target_distance": func(s combatState) float64 { return s.enemyDst },
			"threat_level":    func(s combatState) float64 { return s.threat },
		},
	}); err == nil || !strings.Contains(err.Error(), "LoadCurve") {
		t.Fatalf("BuildReasoner missing LoadCurve = %v", err)
	}
}
