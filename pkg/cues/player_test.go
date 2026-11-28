package cues_test

import (
	"github.com/nsgo-dev/game/pkg/cues"
	"testing"
)

func TestPlayerSuppressesRollbackAndStopsActive(t *testing.T) {
	set, err := cues.Parse(sample())
	if err != nil {
		t.Fatal(err)
	}
	particles, sounds, stops := 0, 0, 0
	p, err := cues.NewPlayer(set, cues.Hooks{Particles: func(cues.Particles, cues.Event) { particles++ }, Sound: func(cues.Sound, cues.Event) { sounds++ }, Stop: func(cues.Cue, cues.Event) { stops++ }})
	if err != nil {
		t.Fatal(err)
	}
	event := cues.Event{Tag: "cue.hit.fire", Target: "hero", Instance: "cast-1", Kind: cues.EventExecuted, Step: 8}
	p.Dispatch(event)
	p.Dispatch(event)
	if particles != 1 || sounds != 1 {
		t.Fatalf("once handlers = %d/%d", particles, sounds)
	}
	p.Dispatch(cues.Event{Tag: "cue.status.burning", Target: "hero", Kind: cues.EventAdded, Step: 9})
	p.Dispatch(cues.Event{Tag: "cue.status.burning", Target: "hero", Kind: cues.EventAdded, Step: 10})
	p.Reconcile(nil, 11)
	if stops != 1 {
		t.Fatalf("stops = %d, want 1", stops)
	}
}

func TestReconcileStartsAndKeepsActive(t *testing.T) {
	set, err := cues.Parse(sample())
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	p, err := cues.NewPlayer(set, cues.Hooks{Animation: func(string, cues.Event) { starts++ }})
	if err != nil {
		t.Fatal(err)
	}
	active := []cues.Active{{Tag: "cue.status.burning", Target: "hero"}}
	p.Reconcile(active, 1)
	p.Reconcile(active, 2)
	if starts != 1 {
		t.Fatalf("starts = %d, want 1", starts)
	}
}
