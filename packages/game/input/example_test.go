package input_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/nsgo/core"
)

func touchAt(phase core.TouchPhase, id int, x, y core.Points) core.TouchEvent {
	return core.TouchEvent{Phase: phase, Touches: []core.Touch{{ID: id, Position: core.Point{X: x, Y: y}, Changed: true}}}
}

// Actions turns touches into named actions and axes, sampled once per fixed step: here the left and right halves of a phone screen steer, and BindAxis reads them as one axis.
func ExampleActions() {
	a := input.NewActions()
	a.Bind("left", input.TouchRegion(core.Bounds{Size: core.Size{Width: 200, Height: 400}}))
	a.Bind("right", input.TouchRegion(core.Bounds{Origin: core.Point{X: 200}, Size: core.Size{Width: 200, Height: 400}}))
	a.BindAxis("steer", "left", "right")

	a.OnTouch(touchAt(core.TouchStart, 1, 300, 200))
	a.Step()
	fmt.Println(a.Pressed("right"), a.JustPressed("right"), a.Axis("steer"))
	a.Step()
	fmt.Println(a.Pressed("right"), a.JustPressed("right"))
	a.OnTouch(touchAt(core.TouchEnd, 1, 300, 200))
	a.Step()
	fmt.Println(a.Pressed("right"), a.JustReleased("right"), a.Axis("steer"))
	// Output:
	// true true 1
	// true false
	// false true 0
}

// A GestureRecognizer reads taps, swipes and pinches from touches as the game steps, and can trigger actions bound to them.
func ExampleGestureRecognizer() {
	a := input.NewActions()
	g := input.NewGestureRecognizer(a)
	g.BindGesture("dash", input.GestureSwipeRight)

	g.OnTouch(touchAt(core.TouchStart, 1, 50, 100))
	g.Update(time.Second / 60)
	g.OnTouch(touchAt(core.TouchMove, 1, 200, 100))
	g.OnTouch(touchAt(core.TouchEnd, 1, 200, 100))
	g.Update(time.Second / 60)
	a.Step()
	for _, e := range g.Events() {
		fmt.Println(e.Kind)
	}
	fmt.Println("dash:", a.JustPressed("dash"))
	// Output:
	// swipe_right
	// dash: true
}

// A PromptTable picks the button label to show for an action on the device the player last used, which a DeviceTracker follows.
func ExamplePromptTable() {
	prompts := input.NewPromptTable()
	prompts.SetMany("jump", map[input.DeviceKind]input.Prompt{
		input.DeviceKeyboard:           {Label: "[Space]", Glyph: "Space"},
		input.DeviceGamepadXbox:        {Label: "[A]", Glyph: "A"},
		input.DeviceGamepadPlayStation: {Label: "[Cross]", Glyph: "Cross"},
		input.DeviceTouch:              {Label: "Tap Jump"},
	})

	var tracker input.DeviceTracker
	tracker.OnKey(core.KeyEvent{})
	fmt.Println(prompts.PromptFor("jump", &tracker).Label)
	tracker.OnGamepadActivity("DualSense Wireless Controller")
	fmt.Println(tracker.LastDevice(), prompts.PromptFor("jump", &tracker).Label)
	// Output:
	// [Space]
	// playstation [Cross]
}

// DetectGamepadKind recognizes a controller family from the name the platform reports.
func ExampleDetectGamepadKind() {
	for _, name := range []string{"Xbox Wireless Controller", "Pro Controller", "USB Gamepad"} {
		fmt.Println(input.DetectGamepadKind(name))
	}
	// Output:
	// xbox
	// nintendo
	// gamepad
}

// A VirtualPad draws on-screen controls and turns touches on them into axes and button presses.
func ExampleVirtualPad() {
	pad := input.NewVirtualPad()
	pad.AddStick(&input.VirtualStick{Center: core.Point{X: 100, Y: 300}, Radius: 50, Name: "move"})
	pad.AddButton(&input.VirtualButton{Center: core.Point{X: 700, Y: 300}, Radius: 40, Action: "jump"})

	pad.OnTouch(touchAt(core.TouchStart, 1, 100, 300))
	pad.OnTouch(touchAt(core.TouchMove, 1, 125, 300))
	pad.OnTouch(touchAt(core.TouchStart, 2, 700, 300))
	fmt.Println(pad.Axis("move_x"), pad.Pressed("jump"))
	// Output:
	// 0.5 true
}
