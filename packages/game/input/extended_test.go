package input_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func approx(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}

func TestVirtualStickFixedDeadZoneAndClamping(t *testing.T) {
	stick := &input.VirtualStick{
		Center:   pt(100, 100),
		Radius:   100,
		DeadZone: 0.2,
	}

	// Touch outside stick circle does not activate a fixed stick with no Zone.
	stick.OnTouch(core.TouchEvent{
		Phase:   core.TouchStart,
		Touches: []core.Touch{{ID: 1, Position: pt(250, 100), Changed: true}},
	})
	if stick.Active() {
		t.Fatal("fixed stick activated from touch outside circle")
	}

	// Touch inside dead zone (offset 15 on radius 100 -> norm 0.15 <= 0.2) yields 0.
	stick.OnTouch(core.TouchEvent{
		Phase:   core.TouchStart,
		Touches: []core.Touch{{ID: 1, Position: pt(115, 100), Changed: true}},
	})
	if !stick.Active() {
		t.Fatal("expected fixed stick active")
	}
	if stick.AxisX != 0 || stick.AxisY != 0 {
		t.Fatalf("inside dead zone: got (%v, %v), want (0, 0)", stick.AxisX, stick.AxisY)
	}
	if stick.BasePosition() != pt(100, 100) || stick.KnobPosition() != pt(115, 100) {
		t.Fatalf("positions: base=%v knob=%v", stick.BasePosition(), stick.KnobPosition())
	}

	// Move halfway past dead zone: offset 60 -> norm 0.6 -> (0.6-0.2)/0.8 = 0.5.
	stick.OnTouch(core.TouchEvent{
		Phase:   core.TouchMove,
		Touches: []core.Touch{{ID: 1, Position: pt(160, 100), Changed: true}},
	})
	if !approx(stick.AxisX, 0.5, 1e-4) || !approx(stick.AxisY, 0, 1e-4) {
		t.Fatalf("mid travel: got (%v, %v), want (0.5, 0)", stick.AxisX, stick.AxisY)
	}

	// Move beyond edge diagonally -> magnitude 1.0 and knob clamped to Radius.
	stick.OnTouch(core.TouchEvent{
		Phase:   core.TouchMove,
		Touches: []core.Touch{{ID: 1, Position: pt(300, 300), Changed: true}},
	})
	mag := math.Hypot(stick.AxisX, stick.AxisY)
	if !approx(mag, 1.0, 1e-4) {
		t.Fatalf("diagonal edge magnitude = %v, want 1.0 (axis=%v,%v)", mag, stick.AxisX, stick.AxisY)
	}
	knobDist := math.Hypot(float64(stick.KnobPosition().X-100), float64(stick.KnobPosition().Y-100))
	if !approx(knobDist, 100, 1e-2) {
		t.Fatalf("clamped knob distance = %v, want 100", knobDist)
	}

	// Unrelated second finger moving/ending must not disturb captured finger 1.
	stick.OnTouch(core.TouchEvent{
		Phase: core.TouchEnd,
		Touches: []core.Touch{
			{ID: 1, Position: pt(300, 300), Changed: false},
			{ID: 2, Position: pt(20, 20), Changed: true},
		},
	})
	if !stick.Active() {
		t.Fatal("unrelated finger lift released stick")
	}

	// Lift finger 1 -> resets to center and 0 output.
	stick.OnTouch(core.TouchEvent{
		Phase:   core.TouchEnd,
		Touches: []core.Touch{{ID: 1, Position: pt(300, 300), Changed: true}},
	})
	if stick.Active() || stick.AxisX != 0 || stick.AxisY != 0 {
		t.Fatalf("after release: active=%v axis=(%v,%v)", stick.Active(), stick.AxisX, stick.AxisY)
	}
	if stick.KnobPosition() != pt(100, 100) {
		t.Fatalf("after release knob = %v, want (100,100)", stick.KnobPosition())
	}
}

func TestVirtualStickFloatingRecentersInZone(t *testing.T) {
	stick := &input.VirtualStick{
		Zone:     rect(0, 100, 200, 200),
		Center:   pt(80, 220),
		Radius:   50,
		DeadZone: 0.1,
		Floating: true,
	}

	// Touch outside Zone is ignored.
	stick.OnTouch(core.TouchEvent{
		Phase:   core.TouchStart,
		Touches: []core.Touch{{ID: 1, Position: pt(250, 200), Changed: true}},
	})
	if stick.Active() {
		t.Fatal("floating stick activated outside Zone")
	}

	// Touch inside Zone re-centers base to touch-down point (60, 160).
	stick.OnTouch(core.TouchEvent{
		Phase:   core.TouchStart,
		Touches: []core.Touch{{ID: 1, Position: pt(60, 160), Changed: true}},
	})
	if !stick.Active() {
		t.Fatal("expected floating stick active")
	}
	if stick.BasePosition() != pt(60, 160) || stick.KnobPosition() != pt(60, 160) {
		t.Fatalf("floating start: base=%v knob=%v, want (60,160)", stick.BasePosition(), stick.KnobPosition())
	}
	if stick.AxisX != 0 || stick.AxisY != 0 {
		t.Fatalf("floating initial axis = (%v, %v), want (0, 0)", stick.AxisX, stick.AxisY)
	}

	// Drag straight up by full radius (50) -> AxisY = -1.
	stick.OnTouch(core.TouchEvent{
		Phase:   core.TouchMove,
		Touches: []core.Touch{{ID: 1, Position: pt(60, 110), Changed: true}},
	})
	if !approx(stick.AxisX, 0, 1e-4) || !approx(stick.AxisY, -1, 1e-4) {
		t.Fatalf("floating drag up: got (%v, %v), want (0, -1)", stick.AxisX, stick.AxisY)
	}

	// Release returns base & knob to resting Center.
	stick.OnTouch(core.TouchEvent{
		Phase:   core.TouchEnd,
		Touches: []core.Touch{{ID: 1, Position: pt(60, 110), Changed: true}},
	})
	if stick.Active() || stick.BasePosition() != pt(80, 220) || stick.KnobPosition() != pt(80, 220) {
		t.Fatalf("after floating release: active=%v base=%v knob=%v", stick.Active(), stick.BasePosition(), stick.KnobPosition())
	}
}

func TestVirtualPadMultiTouchAndActionsIntegration(t *testing.T) {
	pad := input.NewVirtualPad()
	stick := pad.AddStick(&input.VirtualStick{
		AxisXName: "move_x",
		AxisYName: "move_y",
		Zone:      rect(0, 0, 200, 300),
		Center:    pt(80, 200),
		Radius:    50,
		DeadZone:  0.1,
		Floating:  true,
	})
	jumpBtn := pad.AddButton(&input.VirtualButton{
		Center: pt(320, 220),
		Radius: 35,
		Label:  "JUMP",
		Action: "jump",
	})
	fireBtn := pad.AddButton(&input.VirtualButton{
		Bounds: rect(290, 100, 70, 50),
		Label:  "FIRE",
		Action: "fire",
	})

	actions := input.NewActions()
	actions.BindVirtualPad(pad)

	// Finger 1 grabs floating stick at (80, 200) and drags right to (130, 200).
	actions.OnTouch(core.TouchEvent{
		Phase:   core.TouchStart,
		Touches: []core.Touch{{ID: 1, Position: pt(80, 200), Changed: true}},
	})
	actions.OnTouch(core.TouchEvent{
		Phase:   core.TouchMove,
		Touches: []core.Touch{{ID: 1, Position: pt(130, 200), Changed: true}},
	})
	// Finger 2 presses circular jump button simultaneously.
	actions.OnTouch(core.TouchEvent{
		Phase: core.TouchStart,
		Touches: []core.Touch{
			{ID: 1, Position: pt(130, 200), Changed: false},
			{ID: 2, Position: pt(320, 220), Changed: true},
		},
	})
	// Finger 3 presses rectangular fire button simultaneously.
	actions.OnTouch(core.TouchEvent{
		Phase: core.TouchStart,
		Touches: []core.Touch{
			{ID: 1, Position: pt(130, 200), Changed: false},
			{ID: 2, Position: pt(320, 220), Changed: false},
			{ID: 3, Position: pt(310, 120), Changed: true},
		},
	})

	if !stick.Active() || !jumpBtn.Pressed() || !fireBtn.Pressed() {
		t.Fatalf("multi-touch state: stick=%v jump=%v fire=%v", stick.Active(), jumpBtn.Pressed(), fireBtn.Pressed())
	}
	if !approx(pad.Axis("move_x"), 1.0, 1e-4) || !pad.Pressed("jump") || !pad.Pressed("fire") {
		t.Fatalf("pad direct queries: axis=%v jump=%v fire=%v", pad.Axis("move_x"), pad.Pressed("jump"), pad.Pressed("fire"))
	}

	actions.Step()
	if !approx(actions.Axis("move_x"), 1.0, 1e-4) || !approx(actions.Axis("move_y"), 0, 1e-4) {
		t.Fatalf("actions stick axes: (%v, %v)", actions.Axis("move_x"), actions.Axis("move_y"))
	}
	if !actions.Pressed("jump") || !actions.JustPressed("jump") || !actions.Pressed("fire") || !actions.JustPressed("fire") {
		t.Fatalf("actions button state on step 1 failed")
	}

	// Dragging finger 1 (owned by stick) over the jump button after finger 2 lifts must NOT press jump.
	actions.OnTouch(core.TouchEvent{
		Phase: core.TouchEnd,
		Touches: []core.Touch{
			{ID: 1, Position: pt(130, 200), Changed: false},
			{ID: 2, Position: pt(320, 220), Changed: true},
			{ID: 3, Position: pt(310, 120), Changed: false},
		},
	})
	actions.OnTouch(core.TouchEvent{
		Phase: core.TouchMove,
		Touches: []core.Touch{
			{ID: 1, Position: pt(320, 220), Changed: true},
			{ID: 3, Position: pt(310, 120), Changed: false},
		},
	})
	actions.Step()
	if jumpBtn.Pressed() || actions.Pressed("jump") || !actions.JustReleased("jump") {
		t.Fatalf("stick finger stole jump button: btn=%v pressed=%v justReleased=%v", jumpBtn.Pressed(), actions.Pressed("jump"), actions.JustReleased("jump"))
	}
	if !stick.Active() || !fireBtn.Pressed() || !actions.Pressed("fire") {
		t.Fatalf("stick or fire unexpectedly released: stick=%v fire=%v", stick.Active(), fireBtn.Pressed())
	}

	// Quick tap on jump between two Step calls latches for one step.
	actions.OnTouch(core.TouchEvent{
		Phase:   core.TouchStart,
		Touches: []core.Touch{{ID: 4, Position: pt(320, 220), Changed: true}},
	})
	actions.OnTouch(core.TouchEvent{
		Phase:   core.TouchEnd,
		Touches: []core.Touch{{ID: 4, Position: pt(320, 220), Changed: true}},
	})
	actions.Step()
	if !actions.Pressed("jump") || !actions.JustPressed("jump") {
		t.Fatalf("quick tap on virtual button did not latch: pressed=%v justPressed=%v", actions.Pressed("jump"), actions.JustPressed("jump"))
	}
	actions.Step()
	if actions.Pressed("jump") || !actions.JustReleased("jump") {
		t.Fatalf("latched virtual button did not release: pressed=%v justReleased=%v", actions.Pressed("jump"), actions.JustReleased("jump"))
	}

	// Reset releases all virtual controls.
	actions.Reset()
	if stick.Active() || fireBtn.Pressed() || pad.Axis("move_x") != 0 {
		t.Fatalf("Reset did not clear virtual pad: stick=%v fire=%v axis=%v", stick.Active(), fireBtn.Pressed(), pad.Axis("move_x"))
	}
}

func TestVirtualPadDrawRecordsExpectedOps(t *testing.T) {
	pad := input.NewVirtualPad()
	stick := pad.AddStick(&input.VirtualStick{
		Center: pt(80, 120),
		Radius: 40,
	})
	pad.AddButton(&input.VirtualButton{
		Center: pt(280, 120),
		Radius: 30,
		Label:  "A",
		Action: "jump",
	})
	pad.AddButton(&input.VirtualButton{
		Bounds: rect(240, 20, 80, 40),
		Label:  "MENU",
		Action: "menu",
	})

	// Activate stick and circular button to exercise active/pressed fill colors.
	pad.OnTouch(core.TouchEvent{
		Phase: core.TouchStart,
		Touches: []core.Touch{
			{ID: 1, Position: pt(100, 120), Changed: true},
			{ID: 2, Position: pt(280, 120), Changed: true},
		},
	})
	if !stick.Active() || !pad.Pressed("jump") {
		t.Fatal("expected stick active and jump pressed before draw")
	}

	style := input.DefaultVirtualStyle()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "pad",
			Style:  core.Style{Width: core.Pt(400), Height: core.Pt(200)},
			Draw: func(d *core.Drawing, _ core.Size) {
				pad.Draw(d, style)
			},
		}
	})

	ops := app.Find("pad").Ops
	if len(ops) == 0 {
		t.Fatal("VirtualPad.Draw recorded no ops")
	}

	counts := map[core.DrawKind]int{}
	var labels []string
	var sawActiveKnob, sawPressedBtn bool
	for _, op := range ops {
		counts[op.Kind]++
		if op.Kind == core.DrawFillText {
			labels = append(labels, op.Text)
		}
		if op.Kind == core.DrawFillColor {
			if op.Color == style.KnobActiveFill {
				sawActiveKnob = true
			}
			if op.Color == style.ButtonPressedFill {
				sawPressedBtn = true
			}
		}
	}

	// Stick draws 2 arcs (base + knob), circular button draws 1 arc -> 3 arcs total.
	if counts[core.DrawArc] != 3 {
		t.Fatalf("DrawArc count = %d, want 3", counts[core.DrawArc])
	}
	if counts[core.DrawRoundRect] != 1 {
		t.Fatalf("DrawRoundRect count = %d, want 1", counts[core.DrawRoundRect])
	}
	if counts[core.DrawSave] == 0 || counts[core.DrawSave] != counts[core.DrawRestore] {
		t.Fatalf("unbalanced Save/Restore: save=%d restore=%d", counts[core.DrawSave], counts[core.DrawRestore])
	}
	if len(labels) != 2 || labels[0] != "A" || labels[1] != "MENU" {
		t.Fatalf("labels = %v, want [A MENU]", labels)
	}
	if !sawActiveKnob || !sawPressedBtn {
		t.Fatalf("active colors missing: knob=%v btn=%v", sawActiveKnob, sawPressedBtn)
	}
}

func TestGestureRecognizerTapDoubleTapLongPressViaLoop(t *testing.T) {
	actions := input.NewActions()
	gr := input.NewGestureRecognizer(actions)
	gr.BindGesture("tap", input.GestureTap)
	gr.BindGesture("dash", input.GestureDoubleTap)
	gr.BindGesture("charge", input.GestureLongPress)

	watch := []string{"tap", "dash", "charge"}
	history := make(map[string][]stepState, len(watch))
	steps := 0

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v := core.View{
			TestID: "game",
			Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
		}
		gr.Attach(&v)
		actions.Attach(&v)
		loop := &game.Loop{
			Rate: 60,
			Update: func(dt time.Duration) {
				gr.Update(dt)
				actions.Step()
				steps++
				for _, name := range watch {
					history[name] = append(history[name], stepState{
						pressed:      actions.Pressed(name),
						justPressed:  actions.JustPressed(name),
						justReleased: actions.JustReleased(name),
					})
				}
			},
		}
		loop.Run(cx)
		return v
	}, nsgotest.Size(400, 300))

	step := func() {
		before := steps
		for steps == before {
			app.Advance(frameStep)
		}
	}
	last := func(name string) stepState {
		h := history[name]
		return h[len(h)-1]
	}

	step()
	expectState(t, "idle tap", last("tap"), stepState{})

	// First tap triggers "tap", not "dash".
	app.PressAt("game", pt(100, 100))
	step()
	expectState(t, "first tap", last("tap"), stepState{pressed: true, justPressed: true})
	expectState(t, "first tap not dash", last("dash"), stepState{})

	// Advance 3 steps (~50ms) and tap again near (100, 100) -> triggers "dash" (DoubleTap).
	step()
	step()
	step()
	app.PressAt("game", pt(104, 102))
	step()
	expectState(t, "second tap", last("tap"), stepState{pressed: true, justPressed: true})
	expectState(t, "double tap triggers dash", last("dash"), stepState{pressed: true, justPressed: true})
	step()
	expectState(t, "dash clears next step", last("dash"), stepState{justReleased: true})

	// Long press: hold stationary finger across 500ms (~30 steps at 60Hz).
	finger := app.Touch("game", pt(200, 150))
	chargeFiredStep := -1
	for i := 0; i < 40; i++ {
		step()
		if last("charge").justPressed {
			if chargeFiredStep >= 0 {
				t.Fatalf("long press fired more than once (steps %d and %d)", chargeFiredStep, i)
			}
			chargeFiredStep = i
		}
	}
	if chargeFiredStep < 25 || chargeFiredStep > 35 {
		t.Fatalf("long press fired at step %d, want around 30 (~500ms)", chargeFiredStep)
	}

	// Lifting after long press must NOT trigger Tap.
	beforeLift := len(history["tap"])
	finger.Lift()
	step()
	step()
	for _, s := range history["tap"][beforeLift:] {
		if s.justPressed {
			t.Fatal("lifting after long press falsely triggered GestureTap")
		}
	}
}

func TestGestureRecognizerSwipesAndPinch(t *testing.T) {
	actions := input.NewActions()
	gr := input.NewGestureRecognizer(actions)
	gr.BindGesture("jump", input.GestureSwipeUp)
	gr.BindGesture("crouch", input.GestureSwipeDown)
	gr.BindGesture("left", input.GestureSwipeLeft)
	gr.BindGesture("right", input.GestureSwipeRight)
	gr.BindGesture("zoom_in", input.GesturePinchOut)
	gr.BindGesture("zoom_out", input.GesturePinchIn)

	doSwipe := func(id int, from, to core.Point, steps int) {
		gr.OnTouch(core.TouchEvent{
			Phase:   core.TouchStart,
			Touches: []core.Touch{{ID: id, Position: from, Changed: true}},
		})
		for i := 0; i < steps; i++ {
			gr.Update(frameStep)
			actions.Step()
		}
		gr.OnTouch(core.TouchEvent{
			Phase:   core.TouchMove,
			Touches: []core.Touch{{ID: id, Position: to, Changed: true}},
		})
		gr.OnTouch(core.TouchEvent{
			Phase:   core.TouchEnd,
			Touches: []core.Touch{{ID: id, Position: to, Changed: true}},
		})
		gr.Update(frameStep)
		actions.Step()
	}

	cases := []struct {
		name   string
		from   core.Point
		to     core.Point
		action string
		kind   input.GestureKind
	}{
		{"up", pt(200, 200), pt(200, 110), "jump", input.GestureSwipeUp},
		{"down", pt(200, 110), pt(200, 200), "crouch", input.GestureSwipeDown},
		{"left", pt(200, 150), pt(110, 150), "left", input.GestureSwipeLeft},
		{"right", pt(110, 150), pt(200, 150), "right", input.GestureSwipeRight},
	}

	for i, tc := range cases {
		doSwipe(i+1, tc.from, tc.to, 3) // ~50ms, 90pt -> 1800 pt/s
		if !gr.Triggered(tc.kind) || !actions.JustPressed(tc.action) {
			t.Fatalf("swipe %s: triggered=%v actionJustPressed=%v", tc.name, gr.Triggered(tc.kind), actions.JustPressed(tc.action))
		}
		// Next step clears single-step pulse.
		gr.Update(frameStep)
		actions.Step()
		if gr.Triggered(tc.kind) || actions.Pressed(tc.action) {
			t.Fatalf("swipe %s did not clear on next step", tc.name)
		}
	}

	// Slow drag (90pt over 1.2s = 75 pt/s < 150 pt/s) must NOT trigger swipe.
	doSwipe(10, pt(200, 200), pt(200, 110), 72)
	if gr.Triggered(input.GestureSwipeUp) || actions.Pressed("jump") {
		t.Fatal("slow drag falsely triggered GestureSwipeUp")
	}

	// Two-finger pinch out (100pt -> 160pt -> scale 1.6) then pinch in (160pt -> 60pt -> scale 0.6).
	gr.OnTouch(core.TouchEvent{
		Phase: core.TouchStart,
		Touches: []core.Touch{
			{ID: 20, Position: pt(150, 150), Changed: true},
			{ID: 21, Position: pt(250, 150), Changed: true},
		},
	})
	gr.Update(frameStep)
	actions.Step()
	if !approx(gr.PinchScale(), 1.0, 1e-4) {
		t.Fatalf("initial pinch scale = %v, want 1.0", gr.PinchScale())
	}

	// Pinch out: spread apart to 160pt.
	gr.OnTouch(core.TouchEvent{
		Phase: core.TouchMove,
		Touches: []core.Touch{
			{ID: 20, Position: pt(120, 150), Changed: true},
			{ID: 21, Position: pt(280, 150), Changed: true},
		},
	})
	gr.Update(frameStep)
	actions.Step()
	if !gr.Triggered(input.GesturePinchOut) || !actions.JustPressed("zoom_in") {
		t.Fatalf("pinch out failed: triggered=%v zoom_in=%v", gr.Triggered(input.GesturePinchOut), actions.JustPressed("zoom_in"))
	}
	if !approx(gr.PinchScale(), 1.6, 1e-3) {
		t.Fatalf("pinch out scale = %v, want 1.6", gr.PinchScale())
	}

	// Pinch in: bring together to 60pt.
	gr.OnTouch(core.TouchEvent{
		Phase: core.TouchMove,
		Touches: []core.Touch{
			{ID: 20, Position: pt(170, 150), Changed: true},
			{ID: 21, Position: pt(230, 150), Changed: true},
		},
	})
	gr.Update(frameStep)
	actions.Step()
	if !gr.Triggered(input.GesturePinchIn) || !actions.JustPressed("zoom_out") {
		t.Fatalf("pinch in failed: triggered=%v zoom_out=%v", gr.Triggered(input.GesturePinchIn), actions.JustPressed("zoom_out"))
	}
	if !approx(gr.PinchScale(), 0.6, 1e-3) {
		t.Fatalf("pinch in scale = %v, want 0.6", gr.PinchScale())
	}

	// Lifting pinch fingers must NOT fire Tap or Swipe.
	gr.OnTouch(core.TouchEvent{
		Phase: core.TouchEnd,
		Touches: []core.Touch{
			{ID: 20, Position: pt(170, 150), Changed: true},
			{ID: 21, Position: pt(230, 150), Changed: true},
		},
	})
	gr.Update(frameStep)
	actions.Step()
	if len(gr.Events()) != 0 {
		t.Fatalf("lifting pinch fingers emitted spurious gestures: %+v", gr.Events())
	}
}

func TestDeviceTrackerAndPromptTableReactiveHUD(t *testing.T) {
	prompts := input.DefaultPromptTable()
	var tracker *input.DeviceTracker
	var changes []input.DeviceKind

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		tracker = input.NewDeviceTracker(cx, input.DeviceKeyboard)
		tracker.OnChange(func(d input.DeviceKind) {
			changes = append(changes, d)
		})

		v := core.View{
			TestID:    "game",
			Focusable: func() bool { return true },
			Style:     core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			Children: []core.Node{
				core.Text{
					TestID: "jump-prompt",
					ContentFunc: func() string {
						return prompts.PromptFor("jump", tracker).Label
					},
				},
			},
		}
		tracker.Attach(&v)
		return v
	}, nsgotest.Size(400, 300), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))

	expectPrompt := func(step string, wantDev input.DeviceKind, wantLabel string) {
		t.Helper()
		if got := tracker.LastDeviceUntracked(); got != wantDev {
			t.Fatalf("%s: LastDevice = %v, want %v", step, got, wantDev)
		}
		if got := app.Find("jump-prompt").Text; got != wantLabel {
			t.Fatalf("%s: HUD prompt = %q, want %q", step, got, wantLabel)
		}
	}

	// Initial keyboard state.
	expectPrompt("initial keyboard", input.DeviceKeyboard, "[Space]")

	// Touch switches to DeviceTouch even though nsgotest also synthesizes OnPressDown.
	f := app.Touch("game", pt(100, 100))
	expectPrompt("touch down", input.DeviceTouch, "Tap Jump")
	f.Lift()
	expectPrompt("touch lift stays touch", input.DeviceTouch, "Tap Jump")

	// Mouse move / press switches to DeviceMouse.
	app.HoverAt("game", pt(150, 150))
	expectPrompt("mouse hover", input.DeviceMouse, "Click")

	// Hardware keyboard switches back to DeviceKeyboard.
	app.Focus("game")
	app.Key("space")
	expectPrompt("keyboard press", input.DeviceKeyboard, "[Space]")

	// Gamepad brand detection and switching.
	app.Post(func() { tracker.OnGamepadActivity("Xbox Wireless Controller") })
	app.Settle()
	expectPrompt("xbox gamepad", input.DeviceGamepadXbox, "[A]")

	app.Post(func() { tracker.OnGamepadActivity("DualSense Wireless Controller") })
	app.Settle()
	expectPrompt("playstation gamepad", input.DeviceGamepadPlayStation, "[Cross]")

	app.Post(func() { tracker.OnGamepadActivity("Nintendo Switch Pro Controller") })
	app.Settle()
	expectPrompt("nintendo gamepad", input.DeviceGamepadNintendo, "[B]")

	app.Post(func() { tracker.OnGamepad(input.DeviceGamepadGeneric) })
	app.Settle()
	expectPrompt("generic gamepad", input.DeviceGamepadGeneric, "[South]")

	// Verify OnChange recorded every device transition without duplicates.
	wantChanges := []input.DeviceKind{
		input.DeviceTouch,
		input.DeviceMouse,
		input.DeviceKeyboard,
		input.DeviceGamepadXbox,
		input.DeviceGamepadPlayStation,
		input.DeviceGamepadNintendo,
		input.DeviceGamepadGeneric,
	}
	if len(changes) != len(wantChanges) {
		t.Fatalf("changes = %v, want %v", changes, wantChanges)
	}
	for i := range wantChanges {
		if changes[i] != wantChanges[i] {
			t.Fatalf("changes[%d] = %v, want %v", i, changes[i], wantChanges[i])
		}
	}
}

func TestPromptTableFallbackChain(t *testing.T) {
	pt := input.NewPromptTable()
	pt.Set("dodge", input.DeviceKeyboard, input.Prompt{Label: "[Shift]", Icon: "key_shift", Glyph: "Shift"})
	pt.Set("dodge", input.DeviceGamepadGeneric, input.Prompt{Label: "[R1]", Icon: "gamepad_r1", Glyph: "R1"})
	pt.Set("heal", input.DeviceKeyboard, input.Prompt{Label: "[H]", Icon: "key_h", Glyph: "H"})

	// Exact Lookup returns false when PlayStation isn't explicitly set.
	if _, ok := pt.Lookup("dodge", input.DeviceGamepadPlayStation); ok {
		t.Fatal("Lookup unexpectedly fell back for unregistered PlayStation prompt")
	}

	// PlayStation -> GamepadGeneric when generic is registered.
	if got := pt.Prompt("dodge", input.DeviceGamepadPlayStation); got.Label != "[R1]" || got.Icon != "gamepad_r1" {
		t.Fatalf("PlayStation fallback to generic = %+v, want [R1]", got)
	}

	// PlayStation -> GamepadGeneric (miss) -> Keyboard when only keyboard is registered.
	if got := pt.Prompt("heal", input.DeviceGamepadPlayStation); got.Label != "[H]" || got.Glyph != "H" {
		t.Fatalf("PlayStation fallback to keyboard = %+v, want [H]", got)
	}

	// Unknown action falls back to the action name itself.
	if got := pt.Prompt("parry", input.DeviceGamepadXbox); got.Label != "parry" {
		t.Fatalf("unregistered action fallback = %+v, want Label=parry", got)
	}

	// Registering PlayStation overrides the fallback cleanly.
	pt.Set("dodge", input.DeviceGamepadPlayStation, input.Prompt{Label: "[Circle]", Icon: "ps_circle", Glyph: "Circle"})
	if got := pt.Prompt("dodge", input.DeviceGamepadPlayStation); got.Label != "[Circle]" {
		t.Fatalf("PlayStation explicit prompt = %+v, want [Circle]", got)
	}
}
