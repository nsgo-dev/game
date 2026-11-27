package random

import (
	"reflect"
	"testing"
)

const defaultStepRuns = 4

// Mismatch describes the first run whose result differed from the baseline run.
type Mismatch[S any] struct {
	// Run is the 0-based index of the run that diverged from run 0.
	Run int
	// First is the result produced by run 0.
	First S
	// Other is the result produced by the diverging run.
	Other S
}

// CompareRuns executes run runs times from scratch and checks that every run
// equals the first. If equal is nil, reflect.DeepEqual is used. If runs < 2,
// CompareRuns runs twice.
//
// It returns a zero Mismatch and true when all runs agree, or the first mismatch
// and false when a run diverges.
func CompareRuns[S any](runs int, run func() S, equal func(a, b S) bool) (Mismatch[S], bool) {
	if runs < 2 {
		runs = 2
	}
	if equal == nil {
		equal = defaultEqual[S]
	}

	first := run()
	for i := 1; i < runs; i++ {
		other := run()
		if !equal(first, other) {
			return Mismatch[S]{Run: i, First: first, Other: other}, false
		}
	}
	return Mismatch[S]{}, true
}

// AssertDeterministic runs run runs times from scratch and fails t with both
// diverging states if any run produces a different result from the first.
func AssertDeterministic[S any](t testing.TB, runs int, run func() S) {
	t.Helper()
	if m, ok := CompareRuns(runs, run, nil); !ok {
		t.Fatalf("non-deterministic result on run %d:\nrun 0: %#v\nrun %d: %#v", m.Run, m.First, m.Run, m.Other)
	}
}

// CheckSteps creates a fresh simulation with makeSim and runs it for steps steps
// multiple times, returning the earliest 0-based step where two runs diverge
// along with both states. If equal is nil, reflect.DeepEqual is used.
//
// When all runs match on every step (or steps <= 0), CheckSteps returns
// (-1, zero, zero, true).
func CheckSteps[S any](steps int, makeSim func() func(step int) S, equal func(a, b S) bool) (firstDivergentStep int, a, b S, ok bool) {
	return CheckStepsRuns(defaultStepRuns, steps, makeSim, equal)
}

// CheckStepsRuns is like CheckSteps but executes runs independent runs from
// scratch (at least 2).
func CheckStepsRuns[S any](runs, steps int, makeSim func() func(step int) S, equal func(a, b S) bool) (firstDivergentStep int, a, b S, ok bool) {
	var zero S
	if steps <= 0 {
		return -1, zero, zero, true
	}
	if runs < 2 {
		runs = 2
	}
	if equal == nil {
		equal = defaultEqual[S]
	}

	// Record the baseline trajectory from run 0.
	baseSim := makeSim()
	base := make([]S, steps)
	for step := range steps {
		base[step] = baseSim(step)
	}

	bestStep := steps
	var bestA, bestB S
	diverged := false

	for r := 1; r < runs; r++ {
		sim := makeSim()
		for step := range bestStep {
			got := sim(step)
			if !equal(base[step], got) {
				bestStep = step
				bestA = base[step]
				bestB = got
				diverged = true
				break
			}
		}
		if diverged && bestStep == 0 {
			break
		}
	}

	if diverged {
		return bestStep, bestA, bestB, false
	}
	return -1, zero, zero, true
}

// AssertDeterministicSteps fails t if independent runs of makeSim diverge on
// any of the first steps steps, reporting the first divergent step and both states.
func AssertDeterministicSteps[S any](t testing.TB, steps int, makeSim func() func(step int) S, equal func(a, b S) bool) {
	t.Helper()
	if step, a, b, ok := CheckSteps(steps, makeSim, equal); !ok {
		t.Fatalf("simulation diverged at step %d:\nrun A: %#v\nrun B: %#v", step, a, b)
	}
}

func defaultEqual[S any](a, b S) bool {
	return reflect.DeepEqual(a, b)
}
