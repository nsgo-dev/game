package ai_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/random"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// scriptedNode plays back a fixed sequence of statuses, resetting its cursor when Reset is called or after a non-Running status.
type scriptedNode struct {
	name     string
	statuses []ai.Status
	pos      int
	ticks    int
	resets   int
	log      *[]string
}

func (s *scriptedNode) Tick(time.Duration) ai.Status {
	s.ticks++
	if s.log != nil {
		*s.log = append(*s.log, s.name)
	}
	if len(s.statuses) == 0 {
		return ai.StatusFailure
	}
	st := s.statuses[min(s.pos, len(s.statuses)-1)]
	if st == ai.StatusRunning {
		if s.pos < len(s.statuses)-1 {
			s.pos++
		}
		return ai.StatusRunning
	}
	s.pos = 0
	return st
}

func (s *scriptedNode) Reset() {
	s.pos = 0
	s.resets++
	if s.log != nil {
		*s.log = append(*s.log, "reset:"+s.name)
	}
}

func TestBehaviorTreeTimeLimit(t *testing.T) {
	const dt = 50 * time.Millisecond

	tests := []struct {
		name         string
		limit        time.Duration
		childFactory func(log *[]string) (ai.Node, *trackedAction)
		dts          []time.Duration
		resetAfter   int // 1-based tick index after which to call Reset(), 0 for none
		wantStatuses []ai.Status
		wantResets   int
	}{
		{
			name:  "succeeds before limit",
			limit: 150 * time.Millisecond,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "work", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts:          []time.Duration{dt, dt},
			wantStatuses: []ai.Status{ai.StatusRunning, ai.StatusSuccess},
			wantResets:   0,
		},
		{
			name:  "succeeds on exact limit boundary",
			limit: 100 * time.Millisecond,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "exact", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts:          []time.Duration{dt, dt},
			wantStatuses: []ai.Status{ai.StatusRunning, ai.StatusSuccess},
			wantResets:   0,
		},
		{
			name:  "fails and resets running child when elapsed reaches limit then restarts cleanly",
			limit: 100 * time.Millisecond,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "slow", ticksNeeded: 5, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts:          []time.Duration{dt, dt, dt, dt},
			wantStatuses: []ai.Status{ai.StatusRunning, ai.StatusFailure, ai.StatusRunning, ai.StatusFailure},
			wantResets:   2,
		},
		{
			name:  "child failure clears timer before limit",
			limit: 150 * time.Millisecond,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "fail", ticksNeeded: 2, finalStatus: ai.StatusFailure, log: log}
				return ch, ch
			},
			dts:          []time.Duration{dt, dt, dt, dt},
			wantStatuses: []ai.Status{ai.StatusRunning, ai.StatusFailure, ai.StatusRunning, ai.StatusFailure},
			wantResets:   0,
		},
		{
			name:  "zero duration fails and resets running child immediately",
			limit: 0,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "slow", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts:          []time.Duration{dt},
			wantStatuses: []ai.Status{ai.StatusFailure},
			wantResets:   1,
		},
		{
			name:  "negative duration still passes synchronous success",
			limit: -10 * time.Millisecond,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "instant", ticksNeeded: 1, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts:          []time.Duration{dt},
			wantStatuses: []ai.Status{ai.StatusSuccess},
			wantResets:   0,
		},
		{
			name:  "non-positive dt does not advance or rewind elapsed timer",
			limit: 100 * time.Millisecond,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "slow", ticksNeeded: 10, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts:          []time.Duration{dt, 0, -dt, dt},
			wantStatuses: []ai.Status{ai.StatusRunning, ai.StatusRunning, ai.StatusRunning, ai.StatusFailure},
			wantResets:   1,
		},
		{
			name:  "reset mid-run clears elapsed time and resets child",
			limit: 100 * time.Millisecond,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "work", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts:          []time.Duration{dt, dt, dt},
			resetAfter:   1,
			wantStatuses: []ai.Status{ai.StatusRunning, ai.StatusRunning, ai.StatusSuccess},
			wantResets:   1,
		},
		{
			name:  "nil child returns Failure safely",
			limit: 100 * time.Millisecond,
			childFactory: func(*[]string) (ai.Node, *trackedAction) {
				return nil, nil
			},
			dts:          []time.Duration{dt},
			wantStatuses: []ai.Status{ai.StatusFailure},
			wantResets:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			child, tracked := tc.childFactory(&log)
			node := ai.TimeLimit(tc.limit, child)

			got := make([]ai.Status, 0, len(tc.dts))
			for i, stepDT := range tc.dts {
				got = append(got, node.Tick(stepDT))
				if tc.resetAfter > 0 && i+1 == tc.resetAfter {
					node.Reset()
				}
			}
			if !slices.Equal(got, tc.wantStatuses) {
				t.Fatalf("statuses = %v, want %v (log=%v)", got, tc.wantStatuses, log)
			}
			if tracked != nil && tracked.resets != tc.wantResets {
				t.Fatalf("child resets = %d, want %d", tracked.resets, tc.wantResets)
			}
		})
	}
}

func TestBehaviorTreeRetry(t *testing.T) {
	const dt = 25 * time.Millisecond

	tests := []struct {
		name         string
		retries      int
		attempts     []ai.Status // per-tick outcomes of child (or nil for nil child)
		resetAfter   int
		wantStatuses []ai.Status
		wantTicks    int
		wantResets   int
	}{
		{
			name:         "succeeds on first try without retries",
			retries:      3,
			attempts:     []ai.Status{ai.StatusSuccess},
			wantStatuses: []ai.Status{ai.StatusSuccess},
			wantTicks:    1,
		},
		{
			name:         "retries failing child until success on final retry",
			retries:      2,
			attempts:     []ai.Status{ai.StatusFailure, ai.StatusFailure, ai.StatusSuccess},
			wantStatuses: []ai.Status{ai.StatusRunning, ai.StatusRunning, ai.StatusSuccess},
			wantTicks:    3,
		},
		{
			name:         "exhausts all retries after 1+n failures then restarts fresh",
			retries:      2,
			attempts:     []ai.Status{ai.StatusFailure, ai.StatusFailure, ai.StatusFailure, ai.StatusFailure, ai.StatusSuccess},
			wantStatuses: []ai.Status{ai.StatusRunning, ai.StatusRunning, ai.StatusFailure, ai.StatusRunning, ai.StatusSuccess},
			wantTicks:    5,
		},
		{
			name:         "running ticks do not consume retries",
			retries:      1,
			attempts:     []ai.Status{ai.StatusRunning, ai.StatusFailure, ai.StatusRunning, ai.StatusSuccess},
			wantStatuses: []ai.Status{ai.StatusRunning, ai.StatusRunning, ai.StatusRunning, ai.StatusSuccess},
			wantTicks:    4,
		},
		{
			name:         "zero retries fails immediately",
			retries:      0,
			attempts:     []ai.Status{ai.StatusFailure},
			wantStatuses: []ai.Status{ai.StatusFailure},
			wantTicks:    1,
		},
		{
			name:         "negative retries fails immediately",
			retries:      -3,
			attempts:     []ai.Status{ai.StatusFailure},
			wantStatuses: []ai.Status{ai.StatusFailure},
			wantTicks:    1,
		},
		{
			name:         "reset mid-run restores full retry budget and resets child",
			retries:      2,
			attempts:     []ai.Status{ai.StatusFailure, ai.StatusFailure, ai.StatusFailure, ai.StatusFailure},
			resetAfter:   1,
			wantStatuses: []ai.Status{ai.StatusRunning, ai.StatusRunning, ai.StatusRunning, ai.StatusFailure},
			wantTicks:    4,
			wantResets:   1,
		},
		{
			name:         "nil child returns Failure safely",
			retries:      3,
			attempts:     nil,
			wantStatuses: []ai.Status{ai.StatusFailure},
			wantTicks:    0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var child ai.Node
			var ticks, resets int
			if tc.attempts != nil {
				idx := 0
				child = &funcNode{
					tick: func(time.Duration) ai.Status {
						st := tc.attempts[min(idx, len(tc.attempts)-1)]
						idx++
						ticks++
						return st
					},
					reset: func() {
						resets++
					},
				}
			}
			node := ai.Retry(tc.retries, child)
			got := make([]ai.Status, 0, len(tc.wantStatuses))
			for i := range tc.wantStatuses {
				got = append(got, node.Tick(dt))
				if tc.resetAfter > 0 && i+1 == tc.resetAfter {
					node.Reset()
				}
			}
			if !slices.Equal(got, tc.wantStatuses) {
				t.Fatalf("statuses = %v, want %v", got, tc.wantStatuses)
			}
			if ticks != tc.wantTicks {
				t.Fatalf("child ticks = %d, want %d", ticks, tc.wantTicks)
			}
			if resets != tc.wantResets {
				t.Fatalf("child resets = %d, want %d", resets, tc.wantResets)
			}
		})
	}
}

type funcNode struct {
	tick  func(time.Duration) ai.Status
	reset func()
}

func (f *funcNode) Tick(dt time.Duration) ai.Status { return f.tick(dt) }
func (f *funcNode) Reset() {
	if f.reset != nil {
		f.reset()
	}
}

func TestBehaviorTreeRandomSelector(t *testing.T) {
	const dt = 20 * time.Millisecond

	t.Run("edge cases and nil stream", func(t *testing.T) {
		tests := []struct {
			name     string
			stream   *random.Stream
			children func(log *[]string) []ai.Node
			want     []ai.Status
			wantLog  []string
		}{
			{
				name:     "empty returns Failure",
				stream:   random.New(1),
				children: func(*[]string) []ai.Node { return nil },
				want:     []ai.Status{ai.StatusFailure},
			},
			{
				name:   "all nil children return Failure",
				stream: random.New(1),
				children: func(*[]string) []ai.Node {
					return []ai.Node{nil, nil}
				},
				want: []ai.Status{ai.StatusFailure},
			},
			{
				name:   "nil stream falls back to declaration order",
				stream: nil,
				children: func(log *[]string) []ai.Node {
					return []ai.Node{
						&scriptedNode{name: "a", statuses: []ai.Status{ai.StatusFailure}, log: log},
						nil,
						&scriptedNode{name: "b", statuses: []ai.Status{ai.StatusRunning, ai.StatusSuccess}, log: log},
						&scriptedNode{name: "c", statuses: []ai.Status{ai.StatusSuccess}, log: log},
					}
				},
				want:    []ai.Status{ai.StatusRunning, ai.StatusSuccess},
				wantLog: []string{"a", "b", "b"},
			},
			{
				name:   "running child later fails and continues remaining shuffled order",
				stream: nil,
				children: func(log *[]string) []ai.Node {
					return []ai.Node{
						&scriptedNode{name: "a", statuses: []ai.Status{ai.StatusRunning, ai.StatusFailure}, log: log},
						&scriptedNode{name: "b", statuses: []ai.Status{ai.StatusSuccess}, log: log},
					}
				},
				want:    []ai.Status{ai.StatusRunning, ai.StatusSuccess},
				wantLog: []string{"a", "a", "b"},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				var log []string
				node := ai.RandomSelector(tc.stream, tc.children(&log)...)
				got := make([]ai.Status, 0, len(tc.want))
				for range tc.want {
					got = append(got, node.Tick(dt))
				}
				if !slices.Equal(got, tc.want) {
					t.Fatalf("statuses = %v, want %v", got, tc.want)
				}
				if !slices.Equal(log, tc.wantLog) {
					t.Fatalf("log = %v, want %v", log, tc.wantLog)
				}
			})
		}
	})

	t.Run("same seed reproduces exact order across runs and resets", func(t *testing.T) {
		runTrace := func(seed uint64) []string {
			var log []string
			rng := random.New(seed)
			// All children fail on each run so every child's shuffled position is recorded.
			children := []ai.Node{
				&scriptedNode{name: "a", statuses: []ai.Status{ai.StatusFailure}, log: &log},
				&scriptedNode{name: "b", statuses: []ai.Status{ai.StatusFailure}, log: &log},
				&scriptedNode{name: "c", statuses: []ai.Status{ai.StatusFailure}, log: &log},
				&scriptedNode{name: "d", statuses: []ai.Status{ai.StatusFailure}, log: &log},
			}
			sel := ai.RandomSelector(rng, children...)
			for range 8 {
				if st := sel.Tick(dt); st != ai.StatusFailure {
					t.Fatalf("got %v, want Failure", st)
				}
			}
			return log
		}

		trace1 := runTrace(42)
		trace2 := runTrace(42)
		if !slices.Equal(trace1, trace2) {
			t.Fatalf("same seed diverged:\n  run1: %v\n  run2: %v", trace1, trace2)
		}

		traceOther := runTrace(99)
		if slices.Equal(trace1, traceOther) {
			t.Fatalf("different seeds produced identical trace: %v", trace1)
		}

		// Resetting the same node + stream reproduces the exact same first-run trace.
		var reuseLog []string
		rng := random.New(42)
		sel := ai.RandomSelector(rng,
			&scriptedNode{name: "a", statuses: []ai.Status{ai.StatusFailure}, log: &reuseLog},
			&scriptedNode{name: "b", statuses: []ai.Status{ai.StatusFailure}, log: &reuseLog},
			&scriptedNode{name: "c", statuses: []ai.Status{ai.StatusFailure}, log: &reuseLog},
			&scriptedNode{name: "d", statuses: []ai.Status{ai.StatusFailure}, log: &reuseLog},
		)
		sel.Tick(dt)
		firstRun := slices.Clone(reuseLog)
		reuseLog = nil
		rng.Reset()
		sel.Reset()
		// Filter out reset markers from Reset() before ticking.
		reuseLog = nil
		sel.Tick(dt)
		if !slices.Equal(reuseLog, firstRun) {
			t.Fatalf("reused node after Reset = %v, want %v", reuseLog, firstRun)
		}
	})

	t.Run("preserves running child across ticks and resets mid-run", func(t *testing.T) {
		var log []string
		rng := random.New(7)
		children := []*scriptedNode{
			{name: "a", statuses: []ai.Status{ai.StatusRunning, ai.StatusRunning, ai.StatusSuccess}, log: &log},
			{name: "b", statuses: []ai.Status{ai.StatusRunning, ai.StatusRunning, ai.StatusSuccess}, log: &log},
			{name: "c", statuses: []ai.Status{ai.StatusRunning, ai.StatusRunning, ai.StatusSuccess}, log: &log},
		}
		sel := ai.RandomSelector(rng, children[0], children[1], children[2])
		if st := sel.Tick(dt); st != ai.StatusRunning {
			t.Fatalf("tick 1 = %v, want Running", st)
		}
		chosen := log[0]
		if st := sel.Tick(dt); st != ai.StatusRunning {
			t.Fatalf("tick 2 = %v, want Running", st)
		}
		if log[1] != chosen {
			t.Fatalf("tick 2 switched child from %q to %q", chosen, log[1])
		}
		// Interrupt mid-run.
		sel.Reset()
		for _, ch := range children {
			if ch.resets != 1 {
				t.Fatalf("child %s resets = %d, want 1", ch.name, ch.resets)
			}
		}
		// Next full run completes in 3 ticks on a single child.
		log = nil
		if sel.Tick(dt) != ai.StatusRunning || sel.Tick(dt) != ai.StatusRunning || sel.Tick(dt) != ai.StatusSuccess {
			t.Fatalf("expected 3-tick completion after Reset, log=%v", log)
		}
		if len(log) != 3 || log[0] != log[1] || log[1] != log[2] {
			t.Fatalf("expected single child across 3 ticks, got %v", log)
		}
	})
}

func TestBehaviorTreeRandomSequence(t *testing.T) {
	const dt = 20 * time.Millisecond

	t.Run("edge cases and nil stream", func(t *testing.T) {
		tests := []struct {
			name     string
			stream   *random.Stream
			children func(log *[]string) []ai.Node
			want     []ai.Status
			wantLog  []string
		}{
			{
				name:     "empty returns Success",
				stream:   random.New(1),
				children: func(*[]string) []ai.Node { return nil },
				want:     []ai.Status{ai.StatusSuccess},
			},
			{
				name:   "nil child returns Failure",
				stream: nil,
				children: func(log *[]string) []ai.Node {
					return []ai.Node{
						&scriptedNode{name: "a", statuses: []ai.Status{ai.StatusSuccess}, log: log},
						nil,
					}
				},
				want:    []ai.Status{ai.StatusFailure},
				wantLog: []string{"a"},
			},
			{
				name:   "nil stream runs in declaration order and resumes running child",
				stream: nil,
				children: func(log *[]string) []ai.Node {
					return []ai.Node{
						&scriptedNode{name: "a", statuses: []ai.Status{ai.StatusSuccess}, log: log},
						&scriptedNode{name: "b", statuses: []ai.Status{ai.StatusRunning, ai.StatusSuccess}, log: log},
						&scriptedNode{name: "c", statuses: []ai.Status{ai.StatusSuccess}, log: log},
					}
				},
				want:    []ai.Status{ai.StatusRunning, ai.StatusSuccess},
				wantLog: []string{"a", "b", "b", "c"},
			},
			{
				name:   "failure aborts sequence and restarts from fresh order",
				stream: nil,
				children: func(log *[]string) []ai.Node {
					return []ai.Node{
						&scriptedNode{name: "a", statuses: []ai.Status{ai.StatusSuccess}, log: log},
						&scriptedNode{name: "b", statuses: []ai.Status{ai.StatusFailure}, log: log},
						&scriptedNode{name: "c", statuses: []ai.Status{ai.StatusSuccess}, log: log},
					}
				},
				want:    []ai.Status{ai.StatusFailure, ai.StatusFailure},
				wantLog: []string{"a", "b", "a", "b"},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				var log []string
				node := ai.RandomSequence(tc.stream, tc.children(&log)...)
				got := make([]ai.Status, 0, len(tc.want))
				for range tc.want {
					got = append(got, node.Tick(dt))
				}
				if !slices.Equal(got, tc.want) {
					t.Fatalf("statuses = %v, want %v", got, tc.want)
				}
				if !slices.Equal(log, tc.wantLog) {
					t.Fatalf("log = %v, want %v", log, tc.wantLog)
				}
			})
		}
	})

	t.Run("same seed reproduces exact shuffled order and preserves running state", func(t *testing.T) {
		runTrace := func(seed uint64) []string {
			var log []string
			rng := random.New(seed)
			seq := ai.RandomSequence(rng,
				&scriptedNode{name: "a", statuses: []ai.Status{ai.StatusSuccess}, log: &log},
				&scriptedNode{name: "b", statuses: []ai.Status{ai.StatusRunning, ai.StatusSuccess}, log: &log},
				&scriptedNode{name: "c", statuses: []ai.Status{ai.StatusSuccess}, log: &log},
				&scriptedNode{name: "d", statuses: []ai.Status{ai.StatusSuccess}, log: &log},
			)
			for range 6 {
				if st := seq.Tick(dt); st != ai.StatusRunning {
					t.Fatalf("expected Running on first tick of run, got %v", st)
				}
				if st := seq.Tick(dt); st != ai.StatusSuccess {
					t.Fatalf("expected Success on second tick of run, got %v", st)
				}
			}
			return log
		}

		t1 := runTrace(1234)
		t2 := runTrace(1234)
		if !slices.Equal(t1, t2) {
			t.Fatalf("same seed diverged:\n  run1: %v\n  run2: %v", t1, t2)
		}
		t3 := runTrace(5678)
		if slices.Equal(t1, t3) {
			t.Fatalf("different seeds produced identical sequence: %v", t1)
		}
		// Each run has 5 log entries: a, c, d once each + b twice (Running then Success).
		if len(t1) != 6*5 {
			t.Fatalf("len(trace) = %d, want 30", len(t1))
		}
	})

	t.Run("reset mid-run resets children and starts new order", func(t *testing.T) {
		var log []string
		rng := random.New(55)
		a := &trackedAction{name: "a", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: &log}
		b := &trackedAction{name: "b", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: &log}
		seq := ai.RandomSequence(rng, a, b)

		if st := seq.Tick(dt); st != ai.StatusRunning {
			t.Fatalf("tick 1 = %v, want Running", st)
		}
		seq.Reset()
		if a.resets != 1 || b.resets != 1 {
			t.Fatalf("resets = (%d, %d), want (1, 1)", a.resets, b.resets)
		}
		// After Reset, child 1 runs on ticks 1..2 (finishing on tick 2, which also starts child 2),
		// and child 2 finishes on tick 3 (3 sequence ticks, 4 leaf ticks total).
		log = nil
		for i := range 2 {
			if st := seq.Tick(dt); st != ai.StatusRunning {
				t.Fatalf("post-reset tick %d = %v, want Running", i+1, st)
			}
		}
		if st := seq.Tick(dt); st != ai.StatusSuccess {
			t.Fatalf("post-reset tick 3 = %v, want Success", st)
		}
		if len(log) != 4 {
			t.Fatalf("post-reset leaf ticks = %v (len %d), want 4", log, len(log))
		}
	})
}

func TestBehaviorTreeWeightedSelector(t *testing.T) {
	const dt = 20 * time.Millisecond

	t.Run("edge cases and nil stream fallback", func(t *testing.T) {
		tests := []struct {
			name    string
			stream  *random.Stream
			options func(log *[]string) []ai.WeightedNode
			want    []ai.Status
			wantLog []string
		}{
			{
				name:    "empty options returns Failure",
				stream:  random.New(1),
				options: func(*[]string) []ai.WeightedNode { return nil },
				want:    []ai.Status{ai.StatusFailure},
			},
			{
				name:   "zero negative NaN Inf weights and nil nodes are ignored",
				stream: random.New(1),
				options: func(log *[]string) []ai.WeightedNode {
					return []ai.WeightedNode{
						{Node: &scriptedNode{name: "zero", statuses: []ai.Status{ai.StatusSuccess}, log: log}, Weight: 0},
						{Node: &scriptedNode{name: "neg", statuses: []ai.Status{ai.StatusSuccess}, log: log}, Weight: -5},
						{Node: &scriptedNode{name: "nan", statuses: []ai.Status{ai.StatusSuccess}, log: log}, Weight: math.NaN()},
						{Node: &scriptedNode{name: "inf", statuses: []ai.Status{ai.StatusSuccess}, log: log}, Weight: math.Inf(1)},
						{Node: nil, Weight: 10},
					}
				},
				want: []ai.Status{ai.StatusFailure},
			},
			{
				name:   "only eligible positive option is chosen and invalid ones skipped",
				stream: random.New(1),
				options: func(log *[]string) []ai.WeightedNode {
					return []ai.WeightedNode{
						{Node: &scriptedNode{name: "zero", statuses: []ai.Status{ai.StatusSuccess}, log: log}, Weight: 0},
						{Node: &scriptedNode{name: "valid", statuses: []ai.Status{ai.StatusSuccess}, log: log}, Weight: 2.5},
						{Node: &scriptedNode{name: "neg", statuses: []ai.Status{ai.StatusSuccess}, log: log}, Weight: -1},
					}
				},
				want:    []ai.Status{ai.StatusSuccess},
				wantLog: []string{"valid"},
			},
			{
				name:   "nil stream tries descending weight order with declaration tie-break",
				stream: nil,
				options: func(log *[]string) []ai.WeightedNode {
					return []ai.WeightedNode{
						{Node: &scriptedNode{name: "low", statuses: []ai.Status{ai.StatusSuccess}, log: log}, Weight: 1},
						{Node: &scriptedNode{name: "high1", statuses: []ai.Status{ai.StatusFailure}, log: log}, Weight: 5},
						{Node: &scriptedNode{name: "high2", statuses: []ai.Status{ai.StatusRunning, ai.StatusFailure}, log: log}, Weight: 5},
						{Node: &scriptedNode{name: "mid", statuses: []ai.Status{ai.StatusSuccess}, log: log}, Weight: 3},
					}
				},
				want:    []ai.Status{ai.StatusRunning, ai.StatusSuccess},
				wantLog: []string{"high1", "high2", "high2", "mid"},
			},
			{
				name:   "extreme finite weights avoid overflow and underflow",
				stream: random.New(9),
				options: func(log *[]string) []ai.WeightedNode {
					return []ai.WeightedNode{
						{Node: &scriptedNode{name: "huge1", statuses: []ai.Status{ai.StatusFailure}, log: log}, Weight: 1e308},
						{Node: &scriptedNode{name: "huge2", statuses: []ai.Status{ai.StatusSuccess}, log: log}, Weight: 1e308},
					}
				},
				want: []ai.Status{ai.StatusSuccess},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				var log []string
				node := ai.WeightedSelector(tc.stream, tc.options(&log)...)
				got := make([]ai.Status, 0, len(tc.want))
				for range tc.want {
					got = append(got, node.Tick(dt))
				}
				if !slices.Equal(got, tc.want) {
					t.Fatalf("statuses = %v, want %v", got, tc.want)
				}
				if tc.wantLog != nil && !slices.Equal(log, tc.wantLog) {
					t.Fatalf("log = %v, want %v", log, tc.wantLog)
				}
			})
		}
	})

	t.Run("draws without replacement, preserves running child, and replays deterministically", func(t *testing.T) {
		runTrace := func(seed uint64) []string {
			var log []string
			rng := random.New(seed)
			sel := ai.WeightedSelector(rng,
				ai.WeightedNode{Node: &scriptedNode{name: "heavy", statuses: []ai.Status{ai.StatusRunning, ai.StatusFailure}, log: &log}, Weight: 7},
				ai.WeightedNode{Node: &scriptedNode{name: "mid", statuses: []ai.Status{ai.StatusFailure}, log: &log}, Weight: 2},
				ai.WeightedNode{Node: &scriptedNode{name: "light", statuses: []ai.Status{ai.StatusFailure}, log: &log}, Weight: 1},
			)
			for range 10 {
				for {
					st := sel.Tick(dt)
					if st == ai.StatusFailure {
						break
					}
				}
			}
			return log
		}

		t1 := runTrace(2026)
		t2 := runTrace(2026)
		if !slices.Equal(t1, t2) {
			t.Fatalf("same seed diverged:\n  run1: %v\n  run2: %v", t1, t2)
		}
		// Each run visits heavy twice (Running then Failure), mid once, light once = 4 entries per run.
		if len(t1) != 10*4 {
			t.Fatalf("len(trace) = %d, want 40", len(t1))
		}
		// Verify no child is drawn twice within a single run (heavy appears consecutively for Running+Failure).
		for run := range 10 {
			slice := t1[run*4 : (run+1)*4]
			counts := map[string]int{}
			for _, name := range slice {
				counts[name]++
			}
			if counts["heavy"] != 2 || counts["mid"] != 1 || counts["light"] != 1 {
				t.Fatalf("run %d did not draw without replacement: %v", run, slice)
			}
			// heavy's two ticks must be adjacent because Running child is preserved across ticks.
			firstHeavy := slices.Index(slice, "heavy")
			if firstHeavy < 0 || firstHeavy+1 >= len(slice) || slice[firstHeavy+1] != "heavy" {
				t.Fatalf("run %d did not keep running child across ticks: %v", run, slice)
			}
		}
	})

	t.Run("empirical distribution matches weights", func(t *testing.T) {
		rng := random.New(314159)
		counts := map[string]int{}
		makeLeaf := func(name string) ai.Node {
			return ai.Action(func(time.Duration) ai.Status {
				counts[name]++
				return ai.StatusSuccess
			})
		}
		sel := ai.WeightedSelector(rng,
			ai.WeightedNode{Node: makeLeaf("heavy"), Weight: 7},
			ai.WeightedNode{Node: makeLeaf("mid"), Weight: 2},
			ai.WeightedNode{Node: makeLeaf("light"), Weight: 1},
			ai.WeightedNode{Node: makeLeaf("zero"), Weight: 0},
		)
		const trials = 4000
		for range trials {
			if st := sel.Tick(dt); st != ai.StatusSuccess {
				t.Fatalf("got %v, want Success", st)
			}
		}
		if counts["zero"] != 0 {
			t.Fatalf("zero-weight option selected %d times, want 0", counts["zero"])
		}
		heavyRatio := float64(counts["heavy"]) / trials
		midRatio := float64(counts["mid"]) / trials
		lightRatio := float64(counts["light"]) / trials
		if math.Abs(heavyRatio-0.7) > 0.03 || math.Abs(midRatio-0.2) > 0.03 || math.Abs(lightRatio-0.1) > 0.03 {
			t.Fatalf("unexpected ratios: heavy=%.3f mid=%.3f light=%.3f", heavyRatio, midRatio, lightRatio)
		}
	})

	t.Run("reset mid-run resets children and restarts weights", func(t *testing.T) {
		var log []string
		rng := random.New(11)
		a := &trackedAction{name: "a", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: &log}
		b := &trackedAction{name: "b", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: &log}
		sel := ai.WeightedSelector(rng,
			ai.WeightedNode{Node: a, Weight: 1},
			ai.WeightedNode{Node: b, Weight: 1},
		)
		if st := sel.Tick(dt); st != ai.StatusRunning {
			t.Fatalf("tick 1 = %v, want Running", st)
		}
		sel.Reset()
		if a.resets != 1 || b.resets != 1 {
			t.Fatalf("resets = (%d, %d), want (1, 1)", a.resets, b.resets)
		}
	})
}

func TestBehaviorTreeService(t *testing.T) {
	tests := []struct {
		name           string
		interval       time.Duration
		nilFn          bool
		childFactory   func(log *[]string) (ai.Node, *trackedAction)
		dts            []time.Duration
		resetAfter     int
		wantStatuses   []ai.Status
		wantServiceDTs []time.Duration
		wantLog        []string
		wantResets     int
	}{
		{
			name:     "fires on entry before child and every exact interval while running",
			interval: 100 * time.Millisecond,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "chase", ticksNeeded: 5, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts: []time.Duration{
				50 * time.Millisecond,
				50 * time.Millisecond,
				50 * time.Millisecond,
				50 * time.Millisecond,
				50 * time.Millisecond,
			},
			wantStatuses: []ai.Status{
				ai.StatusRunning, ai.StatusRunning, ai.StatusRunning, ai.StatusRunning, ai.StatusSuccess,
			},
			wantServiceDTs: []time.Duration{
				50 * time.Millisecond,  // tick 1: entry
				100 * time.Millisecond, // tick 3: 100ms elapsed
				100 * time.Millisecond, // tick 5: 100ms elapsed
			},
			wantLog: []string{"svc", "chase", "chase", "svc", "chase", "chase", "svc", "chase"},
		},
		{
			name:     "preserves phase and conserves elapsed dt across non-divisor steps",
			interval: 100 * time.Millisecond,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "work", ticksNeeded: 6, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts: []time.Duration{
				60 * time.Millisecond, // tick 1: entry -> fn(60ms), phase=0
				60 * time.Millisecond, // tick 2: phase=60ms
				60 * time.Millisecond, // tick 3: phase=120ms -> fn(120ms), phase=20ms
				60 * time.Millisecond, // tick 4: phase=80ms
				60 * time.Millisecond, // tick 5: phase=140ms -> fn(120ms), phase=40ms
				60 * time.Millisecond, // tick 6: phase=100ms -> fn(60ms), phase=0
			},
			wantStatuses: []ai.Status{
				ai.StatusRunning, ai.StatusRunning, ai.StatusRunning, ai.StatusRunning, ai.StatusRunning, ai.StatusSuccess,
			},
			wantServiceDTs: []time.Duration{
				60 * time.Millisecond,
				120 * time.Millisecond,
				120 * time.Millisecond,
				60 * time.Millisecond,
			},
		},
		{
			name:     "zero interval fires every tick while active",
			interval: 0,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "work", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts:            []time.Duration{16 * time.Millisecond, 16 * time.Millisecond, 16 * time.Millisecond},
			wantStatuses:   []ai.Status{ai.StatusRunning, ai.StatusRunning, ai.StatusSuccess},
			wantServiceDTs: []time.Duration{16 * time.Millisecond, 16 * time.Millisecond, 16 * time.Millisecond},
		},
		{
			name:     "re-entry after completion runs service immediately again",
			interval: 500 * time.Millisecond,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "quick", ticksNeeded: 1, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts:            []time.Duration{50 * time.Millisecond, 50 * time.Millisecond},
			wantStatuses:   []ai.Status{ai.StatusSuccess, ai.StatusSuccess},
			wantServiceDTs: []time.Duration{50 * time.Millisecond, 50 * time.Millisecond},
		},
		{
			name:     "reset mid-run stops service and re-fires on next entry",
			interval: 200 * time.Millisecond,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "hold", ticksNeeded: 4, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts:            []time.Duration{50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond},
			resetAfter:     2,
			wantStatuses:   []ai.Status{ai.StatusRunning, ai.StatusRunning, ai.StatusRunning},
			wantServiceDTs: []time.Duration{50 * time.Millisecond, 50 * time.Millisecond},
			wantResets:     1,
		},
		{
			name:     "non-positive dt while active does not advance timer",
			interval: 100 * time.Millisecond,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "hold", ticksNeeded: 5, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts:            []time.Duration{50 * time.Millisecond, 0, -50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond},
			wantStatuses:   []ai.Status{ai.StatusRunning, ai.StatusRunning, ai.StatusRunning, ai.StatusRunning, ai.StatusSuccess},
			wantServiceDTs: []time.Duration{50 * time.Millisecond, 100 * time.Millisecond},
		},
		{
			name:     "nil callback ticks child normally",
			interval: 100 * time.Millisecond,
			nilFn:    true,
			childFactory: func(log *[]string) (ai.Node, *trackedAction) {
				ch := &trackedAction{name: "work", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: log}
				return ch, ch
			},
			dts:          []time.Duration{50 * time.Millisecond, 50 * time.Millisecond},
			wantStatuses: []ai.Status{ai.StatusRunning, ai.StatusSuccess},
		},
		{
			name:     "nil child returns Failure without running callback",
			interval: 100 * time.Millisecond,
			childFactory: func(*[]string) (ai.Node, *trackedAction) {
				return nil, nil
			},
			dts:          []time.Duration{50 * time.Millisecond},
			wantStatuses: []ai.Status{ai.StatusFailure},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			var serviceDTs []time.Duration
			child, tracked := tc.childFactory(&log)

			var fn func(time.Duration)
			if !tc.nilFn {
				fn = func(dt time.Duration) {
					serviceDTs = append(serviceDTs, dt)
					log = append(log, "svc")
				}
			}

			node := ai.Service(tc.interval, fn, child)
			got := make([]ai.Status, 0, len(tc.dts))
			for i, stepDT := range tc.dts {
				got = append(got, node.Tick(stepDT))
				if tc.resetAfter > 0 && i+1 == tc.resetAfter {
					node.Reset()
				}
			}

			if !slices.Equal(got, tc.wantStatuses) {
				t.Fatalf("statuses = %v, want %v", got, tc.wantStatuses)
			}
			if !slices.Equal(serviceDTs, tc.wantServiceDTs) {
				t.Fatalf("service dts = %v, want %v", serviceDTs, tc.wantServiceDTs)
			}
			if tc.wantLog != nil && !slices.Equal(log, tc.wantLog) {
				t.Fatalf("log = %v, want %v", log, tc.wantLog)
			}
			if tracked != nil && tracked.resets != tc.wantResets {
				t.Fatalf("child resets = %d, want %d", tracked.resets, tc.wantResets)
			}
		})
	}
}

func TestBehaviorTreeExtNestedCompositionAndReplay(t *testing.T) {
	runScenario := func(seed uint64) []string {
		var log []string
		rng := random.New(seed)
		danger := false
		serviceCount := 0

		flee := ai.Action(func(time.Duration) ai.Status {
			log = append(log, "flee")
			return ai.StatusSuccess
		})

		flakyAim := &scriptedNode{
			name:     "aim",
			statuses: []ai.Status{ai.StatusFailure, ai.StatusSuccess},
			log:      &log,
		}
		slowCharge := &trackedAction{
			name:        "charge",
			ticksNeeded: 5,
			finalStatus: ai.StatusSuccess,
			log:         &log,
		}
		quickJab := &trackedAction{
			name:        "jab",
			ticksNeeded: 1,
			finalStatus: ai.StatusSuccess,
			log:         &log,
		}

		// Service refreshes perception every 100ms while the combat branch runs;
		// Selector preemption resets the Service and its nested TimeLimit/Retry/WeightedSelector cleanly.
		combat := ai.Service(100*time.Millisecond, func(time.Duration) {
			serviceCount++
			log = append(log, "scan")
		}, ai.WeightedSelector(rng,
			ai.WeightedNode{
				Node:   ai.TimeLimit(100*time.Millisecond, slowCharge),
				Weight: 2,
			},
			ai.WeightedNode{
				Node:   ai.Sequence(ai.Retry(1, flakyAim), quickJab),
				Weight: 3,
			},
		))

		tree := ai.Selector(
			ai.Guard(func() bool { return danger }, flee),
			combat,
		)

		step := 0
		loop := &game.Loop{
			Rate: 20, // 50ms fixed steps
			Update: func(dt time.Duration) {
				step++
				danger = step == 4
				tree.Tick(dt)
			},
		}

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			loop.Run(cx)
			return core.View{}
		})
		app.Advance(500 * time.Millisecond) // 10 fixed steps
		if step != 10 {
			t.Fatalf("ran %d steps, want 10", step)
		}
		if serviceCount == 0 {
			t.Fatal("expected service to run at least once")
		}
		return log
	}

	run1 := runScenario(777)
	run2 := runScenario(777)
	if !slices.Equal(run1, run2) {
		t.Fatalf("nested scenario replay diverged:\n  run1: %v\n  run2: %v", run1, run2)
	}
}

func TestBehaviorTreeExtZeroAllocTick(t *testing.T) {
	const dt = 16 * time.Millisecond
	rng := random.New(42)

	okLeaf := ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess })
	failLeaf := ai.Action(func(time.Duration) ai.Status { return ai.StatusFailure })
	noop := func(time.Duration) {}

	nodes := map[string]ai.Node{
		"TimeLimit":        ai.TimeLimit(100*time.Millisecond, okLeaf),
		"Retry":            ai.Retry(2, failLeaf),
		"RandomSelector":   ai.RandomSelector(rng, failLeaf, failLeaf, okLeaf),
		"RandomSequence":   ai.RandomSequence(rng, okLeaf, okLeaf, okLeaf),
		"WeightedSelector": ai.WeightedSelector(rng, ai.WeightedNode{Node: failLeaf, Weight: 1}, ai.WeightedNode{Node: okLeaf, Weight: 3}),
		"Service":          ai.Service(50*time.Millisecond, noop, okLeaf),
	}

	for name, node := range nodes {
		t.Run(name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(100, func() {
				node.Tick(dt)
			})
			if allocs != 0 {
				t.Fatalf("%s.Tick allocated %.1f times/op, want 0", name, allocs)
			}
		})
	}
}
