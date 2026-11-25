package moves_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/pixelfx"
	"github.com/nsgo-dev/game/packages/game/pixelfx/moves"
)

// A game with its own character reads the move's timing: when to wind up, freeze, shake and recover.
func ExampleGroundPound() {
	tm := pixelfx.ActTiming(moves.GroundPound)
	fmt.Printf("wind-up %.2f s, recovery %.2f s, %d hits, first at %.2f s freezing %.2f s\n", tm.Windup, tm.Recovery, len(tm.Hits), tm.Hits[0].T, tm.Hits[0].Freeze)
	// Output: wind-up 0.50 s, recovery 0.00 s, 3 hits, first at 0.62 s freezing 0.20 s
}

// Or acts it out with the stock character and a straw target.
func ExampleGroundPound_perform() {
	e := moves.GroundPound
	rig := pixelfx.NewRig()
	w, h, x, y := pixelfx.ActFrameSize(e, rig)
	frame := pixelfx.NewFrame(w, h, 0, 0)
	pf := pixelfx.Perform(e, pixelfx.Options{X: x, Y: y}, rig)
	frames := 0
	for !pf.Done() {
		pf.Update(1.0 / 60)
		frame.Clear()
		pf.Draw(frame, true, true)
		frames++
	}
	fmt.Println(frames > 4*60, len(pf.Targets))
	// Output: true 1
}
