package melee_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/pixelfx"
	"github.com/nsgo-dev/game/pkg/pixelfx/melee"
)

// Importing the package registers its effects; render the sword slash for a sprite sheet.
func ExampleSwordSlash() {
	e, _ := pixelfx.Lookup("melee/sword-slash")
	frames := pixelfx.Render(e, pixelfx.Options{}, 0)
	fmt.Println(e == melee.SwordSlash, e.Meta.Loop, len(frames.Pixels), frames.W, frames.H)
	// Output: true false 24 136 104
}

// A game with its own character reads the cleave's timing: when to wind up, freeze, shake and recover.
func ExampleHeavyCleave() {
	tm := pixelfx.ActTiming(melee.HeavyCleave)
	fmt.Printf("wind-up %.2f s, %d hit at %.3f s freezing %.2f s\n", tm.Windup, len(tm.Hits), tm.Hits[0].T, tm.Hits[0].Freeze)
	// Output: wind-up 0.42 s, 1 hit at 0.335 s freezing 0.20 s
}

// Or acts it out with the stock character and a straw target.
func ExampleSpinSlash_perform() {
	e := melee.SpinSlash
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
	fmt.Println(frames > 60, len(pf.Targets))
	// Output: true 1
}
