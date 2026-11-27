package input_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestOnDemandIdleLoopWakesOnTapAndConsecutiveKeys(t *testing.T) {
	a := input.NewActions()
	a.Bind("advance",
		input.TouchRegion(core.Bounds{Size: core.Size{Width: 400, Height: 300}}),
		input.Key("space"),
	)

	updates, draws, presses := 0, 0, 0
	var loop *game.Loop
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		a.WatchKeys(cx)
		loop = &game.Loop{
			Rate:     60,
			OnDemand: true,
			Idle:     func() bool { return true },
			Update: func(time.Duration) {
				updates++
				a.Step()
				if a.JustPressed("advance") {
					presses++
				}
			},
			Draw: func(float64) {
				draws++
			},
		}
		a.Wake = loop.MarkDirty
		loop.Run(cx)

		v := core.View{
			TestID: "game",
			Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
		}
		a.Attach(&v)
		return v
	}, nsgotest.Size(400, 300))

	// Idle at Rate 60 on a 60 Hz display: 0 updates and 0 draws over a full second.
	app.Advance(time.Second)
	if updates != 0 || draws != 0 || presses != 0 {
		t.Fatalf("after 1s idle: updates=%d draws=%d presses=%d; want 0, 0, 0", updates, draws, presses)
	}

	// Mouse hover without any button held must not wake an idle loop.
	app.HoverAt("game", core.Point{X: 100, Y: 100})
	app.HoverAt("game", core.Point{X: 120, Y: 120})
	app.Advance(time.Second)
	if updates != 0 || draws != 0 {
		t.Fatalf("after mouse hover while idle: updates=%d draws=%d; want 0, 0", updates, draws)
	}

	// A tap (PressAt sends TouchStart and TouchEnd before the next frame) leads to
	// exactly one Update and one Draw on the next frame.
	app.PressAt("game", core.Point{X: 100, Y: 100})
	app.Advance(frameStep)
	if updates != 1 || draws != 1 || presses != 1 {
		t.Fatalf("after tap: updates=%d draws=%d presses=%d; want 1, 1, 1", updates, draws, presses)
	}

	// Stays idle again afterwards.
	app.Advance(time.Second)
	if updates != 1 || draws != 1 {
		t.Fatalf("after 1s post-tap idle: updates=%d draws=%d; want 1, 1", updates, draws)
	}

	// Consecutive quick taps separated by idle time must each fire JustPressed.
	app.Key("space")
	app.Advance(frameStep)
	if updates != 2 || draws != 2 || presses != 2 {
		t.Fatalf("after first key tap: updates=%d draws=%d presses=%d; want 2, 2, 2", updates, draws, presses)
	}
	app.Advance(time.Second)
	app.Key("space")
	app.Advance(frameStep)
	if updates != 3 || draws != 3 || presses != 3 {
		t.Fatalf("after second key tap across idle gap: updates=%d draws=%d presses=%d; want 3, 3, 3", updates, draws, presses)
	}
}

func TestOnDemandIdleLoopWakesOnGamepadInput(t *testing.T) {
	a := input.NewActions()
	a.Bind("confirm", input.GamepadButton(gamepad.ButtonSouth))
	a.BindGamepadAxis("move", gamepad.AxisLeftX, input.GamepadAxisOptions{DeadZone: 0.2})

	updates, draws, confirms := 0, 0, 0
	var lastMove float64
	var loop *game.Loop
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		a.WatchGamepads(cx)
		loop = &game.Loop{
			Rate:     60,
			OnDemand: true,
			Idle:     func() bool { return true },
			Update: func(time.Duration) {
				updates++
				a.Step()
				if a.JustPressed("confirm") {
					confirms++
				}
				lastMove = a.Axis("move")
			},
			Draw: func(float64) { draws++ },
		}
		a.Wake = loop.MarkDirty
		loop.Run(cx)
		return core.View{}
	}, nsgotest.Environment(core.Environment{Gamepads: true}))

	// Connecting an idle controller and jittering inside the dead zone must not wake the loop.
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 0.05)
	app.Advance(time.Second)
	if updates != 0 || draws != 0 {
		t.Fatalf("idle controller with dead-zone jitter: updates=%d draws=%d; want 0, 0", updates, draws)
	}

	// Pressing South wakes the loop and registers JustPressed.
	app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	app.Advance(2 * frameStep)
	if confirms != 1 || draws == 0 {
		t.Fatalf("after South press: updates=%d draws=%d confirms=%d; want confirms=1 and draws>0", updates, draws, confirms)
	}
	beforeHold := draws
	app.Advance(time.Second)
	if draws != beforeHold {
		t.Fatalf("holding South without change drew %d extra frames", draws-beforeHold)
	}

	// Releasing South wakes once to clear the press, then returns to idle.
	app.SetGamepadButton(1, gamepad.ButtonSouth, 0)
	app.Advance(2 * frameStep)
	afterRelease := draws
	if afterRelease <= beforeHold {
		t.Fatalf("releasing South did not wake the loop: draws=%d", draws)
	}
	app.Advance(time.Second)
	if draws != afterRelease {
		t.Fatalf("after release idle drew %d extra frames", draws-afterRelease)
	}

	// Pushing the stick past the dead zone wakes the loop.
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 0.8)
	app.Advance(2 * frameStep)
	if lastMove <= 0 {
		t.Fatalf("stick deflection did not wake loop: move=%v", lastMove)
	}
}

func TestTouchControlsAndGestureRecognizerWakeIdleLoop(t *testing.T) {
	t.Run("touch_controls", func(t *testing.T) {
		layout, err := input.ParseTouchLayout([]byte(`version: 1
reference: {width: 400, height: 300}
controls:
  - id: ok
    kind: button
    anchor: center
    offset: {x: 0, "y": 0}
    size: {width: 80, height: 80}
    label: OK
    action: ok
`))
		if err != nil {
			t.Fatal(err)
		}

		a := input.NewActions()
		updates, draws, presses := 0, 0, 0
		var tc *input.TouchControls
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			var err error
			tc, err = input.NewTouchControls(cx, layout)
			if err != nil {
				t.Fatal(err)
			}
			tc.Resize(core.Size{Width: 400, Height: 300}, core.Edges{})
			tc.BindActions(a)

			loop := &game.Loop{
				Rate:     60,
				OnDemand: true,
				Idle:     func() bool { return true },
				Update: func(time.Duration) {
					updates++
					a.Step()
					if a.JustPressed("ok") {
						presses++
					}
				},
				Draw: func(float64) { draws++ },
			}
			a.Wake = loop.MarkDirty
			loop.Run(cx)

			v := core.View{TestID: "game", Style: core.Style{Width: core.Pt(400), Height: core.Pt(300)}}
			a.Attach(&v)
			tc.Attach(&v)
			return v
		}, nsgotest.Size(400, 300))

		app.Advance(time.Second)
		if updates != 0 || draws != 0 {
			t.Fatalf("idle with TouchControls: updates=%d draws=%d; want 0, 0", updates, draws)
		}

		app.PressAt("game", core.Point{X: 200, Y: 150})
		app.Advance(frameStep)
		if updates != 1 || draws != 1 || presses != 1 {
			t.Fatalf("after tapping TouchControls button: updates=%d draws=%d presses=%d; want 1, 1, 1", updates, draws, presses)
		}

		// Standalone OnWake also fires when visibility or mirroring changes.
		standaloneWakes := 0
		app.Post(func() {
			tc.OnWake = func() { standaloneWakes++ }
			tc.SetMirrored(true)
			tc.SetVisible(false)
		})
		app.Settle()
		if standaloneWakes < 2 {
			t.Fatalf("TouchControls.OnWake calls = %d, want >= 2", standaloneWakes)
		}
	})

	t.Run("gesture_recognizer", func(t *testing.T) {
		g := input.NewGestureRecognizer(nil)
		updates, draws, taps := 0, 0, 0
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			loop := &game.Loop{
				Rate:     60,
				OnDemand: true,
				Idle:     func() bool { return true },
				Update: func(dt time.Duration) {
					updates++
					g.Update(dt)
					if g.Triggered(input.GestureTap) {
						taps++
					}
				},
				Draw: func(float64) { draws++ },
			}
			g.Wake = loop.MarkDirty
			loop.Run(cx)

			v := core.View{TestID: "stage", Style: core.Style{Width: core.Pt(400), Height: core.Pt(300)}}
			g.Attach(&v)
			return v
		}, nsgotest.Size(400, 300))

		app.Advance(time.Second)
		if updates != 0 || draws != 0 || taps != 0 {
			t.Fatalf("idle with GestureRecognizer: updates=%d draws=%d taps=%d; want 0, 0, 0", updates, draws, taps)
		}

		app.PressAt("stage", core.Point{X: 200, Y: 150})
		app.Advance(frameStep)
		if updates != 1 || draws != 1 || taps != 1 {
			t.Fatalf("after gesture tap: updates=%d draws=%d taps=%d; want 1, 1, 1", updates, draws, taps)
		}

		app.Advance(time.Second)
		if updates != 1 || draws != 1 {
			t.Fatalf("after 1s idle post-gesture: updates=%d draws=%d; want 1, 1", updates, draws)
		}
	})
}
