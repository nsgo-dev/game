package replay_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/replay"
)

// A Recorder wraps the live input and records each step's changes; a Player then plays the Recording back as the same ActionSource, so the game cannot tell the difference.
func ExampleRecorder() {
	live := input.NewActions()
	rec := replay.NewRecorder(live, 42, 60, []string{"jump"}, nil)
	for step := range 5 {
		if step == 2 {
			live.Trigger("jump")
		}
		rec.Step()
	}
	recording := rec.Recording()
	fmt.Println(recording.TotalSteps, len(recording.Events))

	p := replay.NewPlayer(recording)
	for !p.Done() {
		p.Step()
		if p.JustPressed("jump") {
			fmt.Println("jump on step", p.StepIndex()-1)
		}
	}
	// Output:
	// 5 2
	// jump on step 2
}

// Encode writes a Recording as versioned text that Decode reads back, so recordings can be checked in next to the tests that replay them.
func ExampleDecode() {
	rec := &replay.Recording{
		Version: replay.FormatVersion, Seed: 7, Rate: 60, TotalSteps: 3,
		Actions: []string{"fire"},
		Events:  []replay.StepEvent{{Step: 1, Press: []int{0}}, {Step: 2, Release: []int{0}}},
	}
	data, err := rec.Encode()
	if err != nil {
		fmt.Println(err)
		return
	}
	back, err := replay.Decode(data)
	fmt.Println(err, back.Seed, back.StepDuration(), len(back.Events))
	// Output:
	// <nil> 7 16.666666ms 2
}
