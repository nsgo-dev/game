package replay_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/replay"
	"github.com/nsgo-dev/nsgo/core"
)

type assistStepSnap struct {
	pressed      [4]bool
	justPressed  [4]bool
	justReleased [4]bool
	moveAxis     float64
}

func captureAssistSnap(src replay.ActionSource, names [4]string) assistStepSnap {
	var snap assistStepSnap
	for i, name := range names {
		snap.pressed[i] = src.Pressed(name)
		snap.justPressed[i] = src.JustPressed(name)
		snap.justReleased[i] = src.JustReleased(name)
	}
	snap.moveAxis = src.Axis("move_x")
	return snap
}

func TestReplayReproducesMotorAssistsExactly(t *testing.T) {
	actionNames := [4]string{"sprint", "struggle", "fire", "right"}
	actions := input.NewActions()
	actions.SetStepRate(60)
	actions.Bind("sprint", input.Key("shift"))
	actions.Bind("struggle", input.Key("e"))
	actions.Bind("fire", input.Key("z"))
	actions.Bind("right", input.Key("d"))
	actions.BindAxis("move_x", "", "right")

	actions.SetActionAssist("sprint", input.ActionAssist{Toggle: true})
	actions.SetActionAssist("struggle", input.ActionAssist{Toggle: true, Mash: true, MashRate: 10})
	actions.SetActionAssist("right", input.ActionAssist{Toggle: true})
	actions.SetAssists(input.Assists{
		Toggle:      []string{"sprint", "struggle", "right"},
		MashToHold:  []string{"struggle"},
		MinPress:    50 * time.Millisecond,  // 3 steps
		AcceptDelay: 250 * time.Millisecond, // 15 steps
	})

	rec := replay.NewRecorder(actions, 42, 60, actionNames[:], []string{"move_x"})
	var liveSnaps []assistStepSnap

	stepAndRecord := func(n int) {
		for range n {
			rec.Step()
			liveSnaps = append(liveSnaps, captureAssistSnap(rec, actionNames))
		}
	}

	// 1. Short accidental tap on fire (2 steps < MinPress=3) -> must not fire.
	actions.OnKeyDown(core.KeyEvent{Key: "z", Code: "KeyZ"})
	stepAndRecord(2)
	actions.OnKeyUp(core.KeyEvent{Key: "z", Code: "KeyZ"})
	stepAndRecord(2)

	// 2. Intentional press on sprint + right (held 3 steps to pass MinPress) -> latch ON.
	actions.OnKeyDown(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	actions.OnKeyDown(core.KeyEvent{Key: "d", Code: "KeyD"})
	stepAndRecord(3)
	actions.OnKeyUp(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	actions.OnKeyUp(core.KeyEvent{Key: "d", Code: "KeyD"})
	stepAndRecord(5)

	// 3. Bounce press on sprint within AcceptDelay -> ignored, sprint stays latched.
	actions.OnKeyDown(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	stepAndRecord(3)
	actions.OnKeyUp(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	stepAndRecord(2)

	// 4. Press-once on struggle (3 steps for MinPress) -> latches continuous 10 Hz mashing!
	actions.OnKeyDown(core.KeyEvent{Key: "e", Code: "KeyE"})
	stepAndRecord(3)
	actions.OnKeyUp(core.KeyEvent{Key: "e", Code: "KeyE"})
	stepAndRecord(24) // 4 synthetic pulses

	// 5. Pause releases all toggles (sprint, right, struggle).
	actions.SetPaused(true)
	actions.SetPaused(false)
	stepAndRecord(4)

	// 6. Fire twice separated by > AcceptDelay.
	actions.OnKeyDown(core.KeyEvent{Key: "z", Code: "KeyZ"})
	stepAndRecord(4)
	actions.OnKeyUp(core.KeyEvent{Key: "z", Code: "KeyZ"})
	stepAndRecord(16)
	actions.OnKeyDown(core.KeyEvent{Key: "z", Code: "KeyZ"})
	stepAndRecord(4)
	actions.OnKeyUp(core.KeyEvent{Key: "z", Code: "KeyZ"})
	stepAndRecord(3)

	// Encode to YAML and decode back to prove wire/text round-trip determinism.
	raw, err := rec.Recording().Encode()
	if err != nil {
		t.Fatalf("replay.Encode: %v", err)
	}
	decoded, err := replay.Decode(raw)
	if err != nil {
		t.Fatalf("replay.Decode: %v", err)
	}

	// Play back and assert every single step's Pressed, JustPressed, JustReleased, and Axis match identically.
	player := replay.NewPlayer(decoded)
	if player.TotalSteps() != len(liveSnaps) {
		t.Fatalf("TotalSteps = %d, want %d", player.TotalSteps(), len(liveSnaps))
	}

	mashPulses := 0
	for step, want := range liveSnaps {
		player.Step()
		got := captureAssistSnap(player, actionNames)
		if got != want {
			t.Fatalf("step %d mismatch:\ngot  %+v\nwant %+v", step, got, want)
		}
		if got.justPressed[1] {
			mashPulses++
		}
	}
	if mashPulses < 4 {
		t.Fatalf("expected at least 4 replayed mash pulses, got %d", mashPulses)
	}
}
