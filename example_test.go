package game_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/nsgo/core"
)

// Loop runs fixed updates on display frames. Here a root scope with a manual clock stands in for the app, and each RunFrame is one display frame 1/60 s after the last. The first frame starts the loop's clock, so the 59 frame intervals that follow give 29 updates at 30 Hz while every frame draws.
func ExampleLoop() {
	clock := core.NewManualClock(time.Unix(0, 0))
	frameWanted := false
	root := core.NewRoot(core.RootOptions{Clock: clock, RequestFrame: func() { frameWanted = true }})
	defer root.Dispose()

	updates, draws := 0, 0
	loop := &game.Loop{
		Rate:   30,
		Update: func(dt time.Duration) { updates++ },
		Draw:   func(alpha float64) { draws++ },
	}
	stop := loop.Run(root)
	defer stop()

	for range 60 {
		clock.Advance(time.Second / 60)
		if frameWanted {
			frameWanted = false
			root.RunFrame()
		}
	}
	fmt.Println("updates:", updates, "draws:", draws)
	// Output:
	// updates: 29 draws: 60
}

// SetPaused stops updates while frames keep drawing, and Step queues single updates while paused, as a debugger's frame advance does.
func ExampleLoop_Step() {
	clock := core.NewManualClock(time.Unix(0, 0))
	frameWanted := false
	root := core.NewRoot(core.RootOptions{Clock: clock, RequestFrame: func() { frameWanted = true }})
	defer root.Dispose()

	updates := 0
	loop := &game.Loop{Update: func(time.Duration) { updates++ }, Draw: func(float64) {}}
	defer loop.Run(root)()
	frame := func() {
		clock.Advance(time.Second / 60)
		if frameWanted {
			frameWanted = false
			root.RunFrame()
		}
	}

	loop.SetPaused(true)
	for range 10 {
		frame()
	}
	fmt.Println("paused:", updates)
	loop.Step(3)
	frame()
	fmt.Println("after Step(3):", updates)
	// Output:
	// paused: 0
	// after Step(3): 3
}
