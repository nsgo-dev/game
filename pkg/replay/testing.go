package replay

import (
	"reflect"
	"testing"
	"time"
)

// Run plays rec from start to finish against the simulation returned by makeSim and returns its end state.
//
// makeSim receives rec.Seed and a *Player as its ActionSource, and returns the per-step update function and a function returning the current state. Run calls step rec.TotalSteps times with rec.StepDuration(), matching game.Loop's fixed dt; step is expected to call src.Step() once per update, just as a live game.Loop.Update does with *input.Actions.
func Run[S any](t testing.TB, rec *Recording, makeSim func(seed uint64, src ActionSource) (step func(dt time.Duration), state func() S)) S {
	t.Helper()
	if rec == nil {
		t.Fatal("replay.Run: recording is nil")
	}
	if err := rec.Validate(); err != nil {
		t.Fatalf("replay.Run: invalid recording: %v", err)
	}
	if makeSim == nil {
		t.Fatal("replay.Run: makeSim is nil")
	}
	player := NewPlayer(rec)
	step, state := makeSim(rec.Seed, player)
	if step == nil {
		t.Fatal("replay.Run: makeSim returned nil step function")
	}
	if state == nil {
		t.Fatal("replay.Run: makeSim returned nil state function")
	}
	dt := rec.StepDuration()
	for range rec.TotalSteps {
		step(dt)
	}
	return state()
}

// AssertEndState plays rec with Run and fails t with a diagnostic if the end state does not match want according to equal (or reflect.DeepEqual when equal is nil). It returns the end state produced by the run.
func AssertEndState[S any](t testing.TB, rec *Recording, want S, equal func(a, b S) bool, makeSim func(seed uint64, src ActionSource) (step func(dt time.Duration), state func() S)) S {
	t.Helper()
	got := Run(t, rec, makeSim)
	var ok bool
	if equal != nil {
		ok = equal(got, want)
	} else {
		ok = reflect.DeepEqual(got, want)
	}
	if !ok {
		t.Fatalf("replay end state mismatch after %d steps (seed %d, rate %d):\n got: %+v\nwant: %+v",
			rec.TotalSteps, rec.Seed, rec.Rate, got, want)
	}
	return got
}
