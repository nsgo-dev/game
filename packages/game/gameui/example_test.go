package gameui_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/gameui"
	"github.com/nsgo-dev/nsgo/core"
)

// A Root lays out a widget tree, moves focus between the focusable widgets and activates the focused one, as a pause menu driven by a gamepad does.
func ExampleRoot() {
	volume := &gameui.Slider{Label: "Volume", Min: 0, Max: 10, Value: 5, Step: 1, Height: 40}
	resume := &gameui.Button{Text: "Resume", Height: 40, OnClick: func() { fmt.Println("resume clicked") }}
	menu := &gameui.VBox{
		Spacing: 10,
		Padding: core.All(20),
		Children: []gameui.Widget{
			&gameui.Label{Text: "Paused", Height: 30},
			resume,
			volume,
		},
	}
	root := gameui.NewRoot(menu)
	root.Layout(core.Bounds{Size: core.Size{Width: 320, Height: 240}})
	fmt.Println(resume.Bounds().Origin, volume.Bounds().Origin)

	root.FocusNext()
	fmt.Println(root.Focused() == resume)
	root.Activate()
	root.FocusNext()
	root.Adjust(1)
	fmt.Println(volume.Value, volume.Fraction())
	// Output:
	// {20 60} {20 110}
	// true
	// resume clicked
	// 6 0.6
}

// A SpatialNavigator moves focus to the nearest widget in the pressed direction, so a grid of buttons works with a d-pad.
func ExampleSpatialNavigator() {
	buttons := make([]*gameui.Button, 4)
	row := func(a, b int) gameui.Widget {
		return &gameui.HBox{Spacing: 10, Children: []gameui.Widget{buttons[a], buttons[b]}}
	}
	for i := range buttons {
		buttons[i] = &gameui.Button{Text: fmt.Sprint("slot ", i), Width: 100, Height: 40}
	}
	root := gameui.NewRoot(&gameui.VBox{Spacing: 10, Children: []gameui.Widget{row(0, 1), row(2, 3)}})
	root.Layout(core.Bounds{Size: core.Size{Width: 400, Height: 300}})

	nav := gameui.NewSpatialNavigator(root)
	nav.OnConfirm = func(w gameui.Widget) { fmt.Println("confirm", w.(*gameui.Button).Text) }
	nav.SetFocus(buttons[0])
	for _, a := range []gameui.NavAction{gameui.NavRight, gameui.NavDown, gameui.NavLeft, gameui.NavConfirm} {
		nav.HandleAction(a)
		fmt.Println(a, nav.Focused().(*gameui.Button).Text)
	}
	// Output:
	// NavRight slot 1
	// NavDown slot 3
	// NavLeft slot 2
	// confirm slot 2
	// NavConfirm slot 2
}

// A Viewport fits a fixed virtual resolution into the window, adding bars on the sides that do not fit, and maps window points back to virtual ones.
func ExampleViewport() {
	vp := gameui.NewViewport(320, 180, gameui.ScaleFit).WithWindowSize(1280, 1024)
	fmt.Println(vp.Scale(), vp.Rect())
	fmt.Println(len(vp.LetterboxBars()))
	p, inside := vp.WindowToVirtual(core.Point{X: 640, Y: 512})
	fmt.Println(p, inside)
	// Output:
	// 4 {{0 152} {1280 720}}
	// 2
	// {160 90} true
}

// A Toggle flips when activated and reports the change through OnChange.
func ExampleToggle() {
	sound := &gameui.Toggle{Label: "Sound", Checked: true, Height: 40, OnChange: func(on bool) { fmt.Println("sound:", on) }}
	root := gameui.NewRoot(&gameui.VBox{Children: []gameui.Widget{sound}})
	root.Layout(core.Bounds{Size: core.Size{Width: 200, Height: 100}})
	root.SetFocus(sound)
	root.Activate()
	fmt.Println(sound.Checked)
	// Output:
	// sound: false
	// false
}

// A ProgressBar shows how far a value is between Min and Max, such as a loading screen's progress.
func ExampleProgressBar() {
	bar := &gameui.ProgressBar{Min: 0, Max: 200, Value: 50}
	fmt.Println(bar.Fraction())
	// Output:
	// 0.25
}
