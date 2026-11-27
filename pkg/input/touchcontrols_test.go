package input_test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestVirtualDPadQuantizesDirections(t *testing.T) {
	cases := []struct {
		name   string
		ways   int
		dx, dy core.Points
		want   input.DPadDirection
	}{
		{"dead zone", 4, 5, -5, 0},
		{"right", 4, 40, 0, input.DPadRight},
		{"up is screen up", 4, 0, -40, input.DPadUp},
		{"left", 4, -40, 0, input.DPadLeft},
		{"down", 4, 0, 40, input.DPadDown},
		{"4-way snaps a shallow diagonal to its axis", 4, 40, -15, input.DPadRight},
		{"4-way snaps a steep diagonal to its axis", 4, 15, -40, input.DPadUp},
		{"8-way keeps the axis near it", 8, 40, -10, input.DPadRight},
		{"8-way up-right", 8, 30, -30, input.DPadUp | input.DPadRight},
		{"8-way down-left", 8, -30, 30, input.DPadDown | input.DPadLeft},
		{"8-way up-left", 8, -30, -30, input.DPadUp | input.DPadLeft},
		{"8-way down-right", 8, 30, 30, input.DPadDown | input.DPadRight},
		{"past the edge still steers", 8, 200, 0, input.DPadRight},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &input.VirtualDPad{Center: pt(100, 100), Radius: 50, Ways: tc.ways, DeadZone: 0.25}
			d.OnTouch(touchAt(core.TouchStart, 1, 100, 100))
			if !d.Active() || d.Direction() != 0 {
				t.Fatalf("touch-down at the center: active=%v dir=%v, want active with no direction", d.Active(), d.Direction())
			}
			d.OnTouch(touchAt(core.TouchMove, 1, 100+tc.dx, 100+tc.dy))
			if got := d.Direction(); got != tc.want {
				t.Fatalf("direction at (%v, %v) = %04b, want %04b", tc.dx, tc.dy, got, tc.want)
			}
		})
	}
}

func TestVirtualDPadOneFingerRocksBetweenDirectionsThroughActions(t *testing.T) {
	pad := input.NewVirtualPad()
	dpad := &input.VirtualDPad{Center: pt(100, 300), Radius: 60, Ways: 4, Left: "left", Right: "right", Up: "jump", AxisXName: "move", AxisYName: "climb"}
	pad.AddControl(dpad)
	a := input.NewActions()
	a.BindAxis("steer", "left", "right")
	a.BindVirtualPad(pad)

	a.OnTouch(touchAt(core.TouchStart, 7, 60, 300))
	a.Step()
	if !a.Pressed("left") || a.Pressed("right") || a.Axis("move") != -1 || a.Axis("steer") != -1 {
		t.Fatalf("finger on the left arrow: left=%v right=%v move=%v steer=%v", a.Pressed("left"), a.Pressed("right"), a.Axis("move"), a.Axis("steer"))
	}

	// The same finger rocks onto the right arrow without lifting.
	a.OnTouch(touchAt(core.TouchMove, 7, 145, 302))
	a.Step()
	if a.Pressed("left") || !a.Pressed("right") || !a.JustPressed("right") || !a.JustReleased("left") || a.Axis("move") != 1 {
		t.Fatalf("after rocking right: left=%v right=%v justPressed=%v move=%v", a.Pressed("left"), a.Pressed("right"), a.JustPressed("right"), a.Axis("move"))
	}

	// Up reads positive on the Y axis, as a game controller's d-pad does.
	a.OnTouch(touchAt(core.TouchMove, 7, 100, 250))
	a.Step()
	if !a.Pressed("jump") || a.Axis("climb") != 1 {
		t.Fatalf("pushing up: jump=%v climb=%v, want pressed and +1", a.Pressed("jump"), a.Axis("climb"))
	}

	// A second finger cannot take the pad while the first owns it.
	a.OnTouch(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{{ID: 7, Position: pt(100, 250)}, {ID: 8, Position: pt(140, 300), Changed: true}}})
	a.Step()
	if a.Pressed("right") {
		t.Fatal("a second finger stole the d-pad from the first")
	}

	a.OnTouch(touchAt(core.TouchEnd, 7, 100, 250))
	a.Step()
	if dpad.Active() || a.Pressed("jump") || a.Axis("climb") != 0 {
		t.Fatalf("after lifting: active=%v jump=%v climb=%v", dpad.Active(), a.Pressed("jump"), a.Axis("climb"))
	}
	if names := a.ActionNames(); !strings.Contains(strings.Join(names, ","), "jump") {
		t.Fatalf("ActionNames = %v, want the d-pad's actions", names)
	}
}

func TestVirtualPadSlideBetweenButtons(t *testing.T) {
	newPad := func(slide bool) (*input.VirtualPad, *input.VirtualButton, *input.VirtualButton, *input.VirtualButton, *input.Actions) {
		pad := input.NewVirtualPad()
		pad.SlideBetweenButtons = slide
		left := pad.AddButton(&input.VirtualButton{Bounds: rect(0, 0, 50, 50), Action: "left"})
		right := pad.AddButton(&input.VirtualButton{Bounds: rect(50, 0, 50, 50), Action: "right"})
		mute := pad.AddButton(&input.VirtualButton{Bounds: rect(100, 0, 50, 50), Action: "mute", Mode: input.ButtonToggle})
		a := input.NewActions()
		a.BindVirtualPad(pad)
		return pad, left, right, mute, a
	}

	t.Run("off keeps the finger on its button", func(t *testing.T) {
		_, left, right, _, a := newPad(false)
		a.OnTouch(touchAt(core.TouchStart, 1, 25, 25))
		a.OnTouch(touchAt(core.TouchMove, 1, 75, 25))
		a.Step()
		if left.Pressed() || right.Pressed() || a.Pressed("right") {
			t.Fatalf("without SlideBetweenButtons: left=%v right=%v", left.Pressed(), right.Pressed())
		}
	})

	t.Run("on hands the finger over and back", func(t *testing.T) {
		pad, left, right, _, a := newPad(true)
		var presses int
		pad.OnPress = func() { presses++ }
		a.OnTouch(touchAt(core.TouchStart, 1, 25, 25))
		a.Step()
		if !a.Pressed("left") || presses != 1 {
			t.Fatalf("touch-down: left=%v presses=%d", a.Pressed("left"), presses)
		}
		a.OnTouch(touchAt(core.TouchMove, 1, 75, 25))
		a.Step()
		if left.Pressed() || !right.Pressed() || !a.JustPressed("right") || !a.JustReleased("left") || presses != 2 {
			t.Fatalf("slid right: left=%v right=%v justPressed=%v presses=%d", left.Pressed(), right.Pressed(), a.JustPressed("right"), presses)
		}
		a.OnTouch(touchAt(core.TouchMove, 1, 20, 25))
		a.Step()
		if !left.Pressed() || right.Pressed() {
			t.Fatalf("slid back: left=%v right=%v", left.Pressed(), right.Pressed())
		}
		a.OnTouch(touchAt(core.TouchEnd, 1, 20, 25))
		a.Step()
		if left.Pressed() || right.Pressed() || a.Pressed("left") {
			t.Fatalf("lifted: left=%v right=%v", left.Pressed(), right.Pressed())
		}
	})

	t.Run("a toggle is never entered by sliding", func(t *testing.T) {
		_, _, right, mute, a := newPad(true)
		a.OnTouch(touchAt(core.TouchStart, 1, 75, 25))
		a.OnTouch(touchAt(core.TouchMove, 1, 125, 25))
		a.Step()
		if mute.Pressed() || right.Pressed() {
			t.Fatalf("slid onto the toggle: mute=%v right=%v", mute.Pressed(), right.Pressed())
		}
	})
}

func TestVirtualButtonToggleAndTapModes(t *testing.T) {
	pad := input.NewVirtualPad()
	crouch := pad.AddButton(&input.VirtualButton{Center: pt(50, 50), Radius: 30, Action: "crouch", Mode: input.ButtonToggle})
	fire := pad.AddButton(&input.VirtualButton{Center: pt(200, 50), Radius: 30, Action: "fire", Mode: input.ButtonTap})
	a := input.NewActions()
	a.BindVirtualPad(pad)

	// Toggle: on at the first touch-down, still on after the finger lifts, off at the next touch-down.
	a.OnTouch(touchAt(core.TouchStart, 1, 50, 50))
	a.OnTouch(touchAt(core.TouchEnd, 1, 50, 50))
	a.Step()
	if !a.Pressed("crouch") || !crouch.Pressed() || crouch.Held() {
		t.Fatalf("toggle after one tap: action=%v pressed=%v held=%v", a.Pressed("crouch"), crouch.Pressed(), crouch.Held())
	}
	a.Step()
	if !a.Pressed("crouch") {
		t.Fatal("toggle released itself on the next step")
	}
	a.OnTouch(touchAt(core.TouchStart, 2, 50, 50))
	a.Step()
	if a.Pressed("crouch") || !a.JustReleased("crouch") {
		t.Fatalf("toggle after a second touch-down: pressed=%v justReleased=%v", a.Pressed("crouch"), a.JustReleased("crouch"))
	}
	a.OnTouch(touchAt(core.TouchEnd, 2, 50, 50))

	// Tap: pressed for exactly one step however long the finger stays.
	a.OnTouch(touchAt(core.TouchStart, 3, 200, 50))
	a.Step()
	if !a.Pressed("fire") || !a.JustPressed("fire") {
		t.Fatalf("tap step 1: pressed=%v justPressed=%v", a.Pressed("fire"), a.JustPressed("fire"))
	}
	a.Step()
	if a.Pressed("fire") || !fire.Held() {
		t.Fatalf("tap step 2 with the finger still down: pressed=%v held=%v", a.Pressed("fire"), fire.Held())
	}
	a.OnTouch(touchAt(core.TouchEnd, 3, 200, 50))
	a.OnTouch(touchAt(core.TouchStart, 4, 200, 50))
	a.OnTouch(touchAt(core.TouchEnd, 4, 200, 50))
	a.Step()
	if !a.JustPressed("fire") {
		t.Fatal("a second quick tap between steps did not fire")
	}

	// A cancel of every touch lifts fingers but leaves a toggle on; Reset turns it off.
	a.OnTouch(touchAt(core.TouchStart, 5, 50, 50))
	a.OnTouch(core.TouchEvent{Phase: core.TouchCancel})
	a.Step()
	if !a.Pressed("crouch") || crouch.Held() {
		t.Fatalf("after cancel: crouch=%v held=%v", a.Pressed("crouch"), crouch.Held())
	}
	a.Reset()
	a.Step()
	if a.Pressed("crouch") {
		t.Fatal("Reset left the toggle on")
	}
}

func TestVirtualStickUpPositive(t *testing.T) {
	for _, up := range []bool{false, true} {
		s := &input.VirtualStick{Center: pt(100, 100), Radius: 50, UpPositive: up}
		s.OnTouch(touchAt(core.TouchStart, 1, 100, 100))
		s.OnTouch(touchAt(core.TouchMove, 1, 100, 50))
		want := -1.0
		if up {
			want = 1
		}
		if !approx(s.AxisY, want, 1e-6) {
			t.Fatalf("UpPositive=%v: pushing up reads AxisY %v, want %v", up, s.AxisY, want)
		}
	}
}

const testLayoutYAML = `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/touchlayout.v1.json
version: 1
reference: {width: 800, height: 400}
maxScale: 2
idleAfter: 3s
idleAlpha: 0.4
controls:
  - id: move
    kind: dpad
    anchor: bottomLeft
    offset: {x: 20, "y": 30}
    size: {width: 160, height: 160}
    label: Move
    up: jump
    left: left
    right: right
    axisX: move
  - id: jump
    kind: button
    anchor: bottomRight
    offset: {x: 40, "y": 30}
    size: {width: 100, height: 100}
    label: Jump
    action: jump
    text: JUMP
  - id: look
    kind: stick
    anchor: right
    size: {width: 120, height: 120}
    axisX: look_x
    axisY: look_y
    deadZone: 0.1
  - id: tiny
    kind: button
    anchor: top
    offset: {x: 100}
    size: {width: 10, height: 10}
    action: pause
    icon: arrowUp
    mode: tap
    shape: rect
`

func TestTouchLayoutRoundTripIsLossless(t *testing.T) {
	l, err := input.ParseTouchLayout([]byte(testLayoutYAML))
	if err != nil {
		t.Fatal(err)
	}
	if l.IdleAfter != 3*time.Second || l.Controls[0].Up != "jump" || l.Controls[3].Offset.X != 100 {
		t.Fatalf("parsed layout = %+v", l)
	}
	out, err := input.EncodeTouchLayout(l)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != testLayoutYAML {
		t.Fatalf("encode changed the document:\n%s", out)
	}
	again, err := input.EncodeTouchLayout(l)
	if err != nil || !bytes.Equal(again, out) {
		t.Fatalf("a second encode differs: err=%v", err)
	}
}

func TestTouchLayoutRefusesBadDocuments(t *testing.T) {
	head := "version: 1\nreference: {width: 800, height: 400}\ncontrols:\n"
	button := "  - {id: a, kind: button, anchor: bottomLeft, size: {width: 50, height: 50}, action: jump"
	cases := []struct {
		name, doc, field string
	}{
		{"unknown root field", "version: 1\nreference: {width: 1, height: 1}\ncolour: red\n", ""},
		{"unknown control field", head + button + ", colour: red}\n", ""},
		{"version", "version: 2\nreference: {width: 1, height: 1}\n", "version"},
		{"missing version", "reference: {width: 1, height: 1}\n", "version"},
		{"reference", "version: 1\nreference: {width: 0, height: 1}\n", "reference"},
		{"idle alpha", "version: 1\nreference: {width: 1, height: 1}\nidleAlpha: 2\n", "idleAlpha"},
		{"anchor", head + "  - {id: a, kind: button, anchor: middle, size: {width: 50, height: 50}, action: jump}\n", "controls[0].anchor"},
		{"size", head + "  - {id: a, kind: button, anchor: top, size: {width: 50, height: -1}, action: jump}\n", "controls[0].size"},
		{"button without action", head + "  - {id: a, kind: button, anchor: top, size: {width: 50, height: 50}}\n", "controls[0].action"},
		{"button with ways", head + button + ", ways: 8}\n", "controls[0].ways"},
		{"button with options", head + button + ", options: {speed: fast}}\n", "controls[0].options"},
		{"stick with action", head + "  - {id: a, kind: stick, anchor: top, size: {width: 50, height: 50}, axisX: x, action: jump}\n", "controls[0].action"},
		{"dpad with nothing", head + "  - {id: a, kind: dpad, anchor: top, size: {width: 50, height: 50}}\n", "controls[0].up"},
		{"duplicate id", head + button + "}\n" + button + "}\n", "controls[1].id"},
		{"two documents", "version: 1\nreference: {width: 1, height: 1}\n---\nversion: 1\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := input.ParseTouchLayout([]byte(tc.doc))
			e, ok := loaderr.As(err)
			if !ok {
				t.Fatalf("err = %v, want a *loaderr.Error", err)
			}
			if tc.field != "" && e.Field != tc.field {
				t.Fatalf("field = %q, want %q (%v)", e.Field, tc.field, err)
			}
		})
	}
}

func newTestControls(t *testing.T, doc string) *input.TouchControls {
	t.Helper()
	l, err := input.ParseTouchLayout([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	tc, err := input.NewTouchControls(nil, l)
	if err != nil {
		t.Fatal(err)
	}
	return tc
}

func frameOf(t *testing.T, tc *input.TouchControls, id string) core.Bounds {
	t.Helper()
	for _, c := range tc.Controls() {
		if c.ID == id {
			return c.Frame
		}
	}
	t.Fatalf("no control %q", id)
	return core.Bounds{}
}

func TestTouchControlsPlaceInsideTheSafeAreaAndScale(t *testing.T) {
	tc := newTestControls(t, testLayoutYAML)

	// A screen twice the reference with a notch on the left and a home indicator at the bottom.
	safe := core.Edges{Left: 40, Right: 0, Top: 0, Bottom: 20}
	tc.Resize(core.Size{Width: 1640, Height: 820}, safe)
	// The safe area is 1600 by 800, exactly twice the reference.
	if got, want := frameOf(t, tc, "move"), rect(40+40, 800-60-320, 320, 320); got != want {
		t.Fatalf("move frame = %+v, want %+v", got, want)
	}
	if got, want := frameOf(t, tc, "jump"), rect(1640-80-200, 800-60-200, 200, 200); got != want {
		t.Fatalf("jump frame = %+v, want %+v", got, want)
	}
	if got, want := frameOf(t, tc, "look"), rect(1640-240, (800-240)/2, 240, 240); got != want {
		t.Fatalf("look frame = %+v, want %+v", got, want)
	}

	// A small screen shrinks the layout but never below MinTouchTarget.
	tc.Resize(core.Size{Width: 400, Height: 200}, core.Edges{})
	tiny := frameOf(t, tc, "tiny")
	if tiny.Size.Width != input.MinTouchTarget || tiny.Size.Height != input.MinTouchTarget {
		t.Fatalf("tiny size = %+v, want the %d-point minimum", tiny.Size, input.MinTouchTarget)
	}
	if got := frameOf(t, tc, "jump").Size.Width; got != 50 {
		t.Fatalf("jump width at half scale = %v, want 50", got)
	}

	// MaxScale caps the growth on a large screen.
	tc.Resize(core.Size{Width: 4000, Height: 2000}, core.Edges{})
	if got := frameOf(t, tc, "jump").Size.Width; got != 200 {
		t.Fatalf("jump width at MaxScale = %v, want 200", got)
	}

	// Controls placed past the safe area's edge are pulled back inside it.
	edge := newTestControls(t, "version: 1\nreference: {width: 100, height: 100}\ncontrols:\n  - {id: a, kind: button, anchor: topLeft, offset: {x: -50, \"y\": 500}, size: {width: 50, height: 50}, action: jump}\n")
	edge.Resize(core.Size{Width: 100, Height: 100}, core.Edges{Top: 10, Left: 5})
	// The safe area is 95 by 90, so the layout scales by 0.9.
	if got, want := frameOf(t, edge, "a"), rect(5, 55, 45, 45); got != want {
		t.Fatalf("clamped frame = %+v, want %+v", got, want)
	}

	// The built controls follow their frames.
	if b := tc.Button("jump"); b == nil || b.Radius != 100 || b.Center != pt(4000-80-100, 2000-60-100) {
		t.Fatalf("jump button = %+v", b)
	}
	if d := tc.DPad("move"); d == nil || d.Radius != 160 {
		t.Fatalf("move d-pad = %+v", d)
	}
	if s := tc.Stick("look"); s == nil || !s.UpPositive || s.Radius != 120 {
		t.Fatalf("look stick = %+v", s)
	}
}

func TestTouchControlsMirrored(t *testing.T) {
	tc := newTestControls(t, testLayoutYAML)
	tc.Resize(core.Size{Width: 830, Height: 400}, core.Edges{Left: 30})
	tc.SetMirrored(true)
	// The safe area runs from x 30 to 830, the reference's 800 wide.
	if got, want := frameOf(t, tc, "move"), rect(830-20-160, 400-30-160, 160, 160); got != want {
		t.Fatalf("mirrored move = %+v, want %+v", got, want)
	}
	if got, want := frameOf(t, tc, "jump"), rect(30+40, 400-30-100, 100, 100); got != want {
		t.Fatalf("mirrored jump = %+v, want %+v", got, want)
	}
	if got, want := frameOf(t, tc, "look"), rect(30, (400-120)/2, 120, 120); got != want {
		t.Fatalf("mirrored look = %+v, want %+v", got, want)
	}
	// A centered anchor's offset flips too: 100 right of center becomes 100 left.
	if got, want := frameOf(t, tc, "tiny").Origin.X, core.Points(30+(800-44)/2-100); got != want {
		t.Fatalf("mirrored tiny x = %v, want %v", got, want)
	}
	if !tc.Layout().Mirrored {
		t.Fatal("Layout does not record the mirroring")
	}
	// Directions keep their meaning: the d-pad's left arrow still presses left.
	a := input.NewActions()
	tc.BindActions(a)
	move := frameOf(t, tc, "move")
	a.OnTouch(touchAt(core.TouchStart, 1, move.Origin.X+10, move.Origin.Y+move.Size.Height/2))
	a.Step()
	if !a.Pressed("left") {
		t.Fatal("the mirrored d-pad's left arrow does not press left")
	}
}

func TestTouchControlsShowOnTouchAndHideOnKeysAndControllers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		idiom core.Idiom
		shown bool
	}{
		{"phone starts shown", core.IdiomPhone, true},
		{"desktop starts hidden", core.IdiomDesktop, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var controls *input.TouchControls
			var actions *input.Actions
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				l, err := input.ParseTouchLayout([]byte(testLayoutYAML))
				if err != nil {
					t.Fatal(err)
				}
				controls, err = input.NewTouchControls(cx, l)
				if err != nil {
					t.Fatal(err)
				}
				actions = input.NewActions()
				controls.BindActions(actions)
				v := core.View{TestID: "game", Style: core.Style{Width: core.Pct(100), Height: core.Pct(100)}}
				actions.Attach(&v)
				controls.Attach(&v)
				return v
			}, nsgotest.Size(800, 400), nsgotest.Idiom(tc.idiom))
			if controls.Visible() != tc.shown {
				t.Fatalf("initial Visible = %v, want %v", controls.Visible(), tc.shown)
			}

			// A touch anywhere shows the controls.
			finger := app.Touch("id:game", core.Point{X: 400, Y: 100})
			finger.Lift()
			if !controls.Visible() || controls.Alpha() != 1 {
				t.Fatalf("after a touch: visible=%v alpha=%v", controls.Visible(), controls.Alpha())
			}

			// A game controller's button hides them on the next Update.
			actions.OnGamepads([]gamepad.Pad{pad(1, map[gamepad.Button]float32{gamepad.ButtonSouth: 1}, [gamepad.AxisCount]float32{})})
			actions.Step()
			controls.Update(16 * time.Millisecond)
			if controls.Visible() || !controls.Tracker().LastDevice().IsGamepad() {
				t.Fatalf("after a controller press: visible=%v device=%v", controls.Visible(), controls.Tracker().LastDevice())
			}
			actions.OnGamepads(nil)
			actions.Step()
			controls.Update(16 * time.Millisecond)

			// Touch again, then a key the view hears hides them.
			finger = app.Touch("id:game", core.Point{X: 400, Y: 100})
			finger.Lift()
			if !controls.Visible() {
				t.Fatal("a touch after the controller did not show the controls")
			}
			actions.OnKeyDown(core.KeyEvent{Key: "a", Code: "KeyA"})
			actions.Step()
			controls.Update(16 * time.Millisecond)
			if controls.Visible() || controls.Alpha() != 0 {
				t.Fatalf("after a key: visible=%v alpha=%v", controls.Visible(), controls.Alpha())
			}
			// Keeping the key down does not hide them again after the next touch.
			finger = app.Touch("id:game", core.Point{X: 400, Y: 100})
			finger.Lift()
			actions.Step()
			controls.Update(16 * time.Millisecond)
			if !controls.Visible() {
				t.Fatal("a key still held hid the controls after a touch")
			}
		})
	}
}

func TestTouchControlsIdleFadeAndHaptics(t *testing.T) {
	tc := newTestControls(t, testLayoutYAML)
	tc.SetVisible(true)
	var ticks []core.Haptic
	tc.Haptics = input.NewHaptics(nil)
	tc.Haptics.OnHaptic = func(h core.Haptic) { ticks = append(ticks, h) }
	a := input.NewActions()
	tc.BindActions(a)
	tc.Resize(core.Size{Width: 800, Height: 400}, core.Edges{})

	tc.Update(2 * time.Second)
	if tc.Alpha() != 1 {
		t.Fatalf("alpha before IdleAfter = %v, want 1", tc.Alpha())
	}
	tc.Update(time.Second)
	for range 20 {
		tc.Update(50 * time.Millisecond)
	}
	if !approx(float64(tc.Alpha()), 0.4, 1e-6) {
		t.Fatalf("alpha once idle = %v, want IdleAlpha 0.4", tc.Alpha())
	}

	jump := frameOf(t, tc, "jump")
	a.OnTouch(touchAt(core.TouchStart, 1, jump.Origin.X+jump.Size.Width/2, jump.Origin.Y+jump.Size.Height/2))
	if tc.Alpha() != 1 || len(ticks) != 1 || ticks[0] != core.HapticSelection {
		t.Fatalf("on press: alpha=%v haptics=%v", tc.Alpha(), ticks)
	}
	// Holding the control keeps it awake past IdleAfter.
	tc.Update(5 * time.Second)
	if tc.Alpha() != 1 {
		t.Fatalf("alpha while held = %v, want 1", tc.Alpha())
	}
	a.Step()
	if !a.Pressed("jump") {
		t.Fatal("the jump button does not press jump through Actions")
	}
}

func TestTouchControlsSaveAndLoadAPlayersLayout(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} },
		nsgotest.Storage(map[string]string{"bad": "version: 1\nreference: {width: 1, height: 1}\ncontrols:\n  - {id: a, kind: hologram, anchor: top, size: {width: 1, height: 1}}\n"}))
	tc := newTestControls(t, testLayoutYAML)

	if found, err := tc.Load(app.Storage(), "touch"); found || err != nil {
		t.Fatalf("Load of an absent key = %v, %v", found, err)
	}
	edited := tc.Layout()
	edited.Controls[1].Size = input.TouchSize{Width: 140, Height: 140}
	edited.Controls[1].Offset.X = 10
	if err := tc.SetLayout(edited); err != nil {
		t.Fatal(err)
	}
	if err := tc.Save(app.Storage(), "touch"); err != nil {
		t.Fatal(err)
	}
	raw, _ := app.Storage().GetItem("touch")
	if !strings.HasPrefix(raw, input.TouchLayoutSchemaComment+"\nversion: 1\n") {
		t.Fatalf("saved layout is not the YAML document:\n%s", raw)
	}

	fresh := newTestControls(t, testLayoutYAML)
	if found, err := fresh.Load(app.Storage(), "touch"); !found || err != nil {
		t.Fatalf("Load = %v, %v", found, err)
	}
	if got := fresh.Layout().Controls[1]; got.Size.Width != 140 || got.Offset.X != 10 {
		t.Fatalf("loaded jump = %+v", got)
	}

	before := fresh.Layout()
	found, err := fresh.Load(app.Storage(), "bad")
	e, ok := loaderr.As(err)
	if found || !ok || e.File != "bad" || e.Field != "controls[0].kind" || e.Line != 4 {
		t.Fatalf("Load of an unknown kind = %v, %v", found, err)
	}
	if fresh.Layout().Controls[1].Size != before.Controls[1].Size {
		t.Fatal("a failed Load changed the layout")
	}
}

// ringControl is a control a plugin could register: a ring that presses its action while a finger is inside it and reports how far round the finger is on an axis.
type ringControl struct {
	action, axis string
	center       core.Point
	radius       float32
	finger       int
	angle        float64
}

func (r *ringControl) Claim(id int, p core.Point) bool {
	if r.finger != 0 || r.radius <= 0 || dist(p, r.center) > r.radius {
		return false
	}
	r.finger = id
	r.Move(id, p)
	return true
}

func (r *ringControl) Move(id int, p core.Point) {
	if id == r.finger {
		r.angle = float64(p.X-r.center.X) / float64(r.radius)
	}
}

func (r *ringControl) Release(id int) {
	if id == r.finger {
		r.Reset()
	}
}

func (r *ringControl) Reset()                { r.finger, r.angle = 0, 0 }
func (r *ringControl) ActionNames() []string { return []string{r.action} }
func (r *ringControl) ActionPressed(name string) bool {
	return name == r.action && r.finger != 0
}
func (r *ringControl) AxisValues(yield func(string, float64)) { yield(r.axis, r.angle) }
func (r *ringControl) Draw(d *core.Drawing, _ input.VirtualStyle) {
	d.BeginPath().Arc(float32(r.center.X), float32(r.center.Y), r.radius, 0, 6.3, false).Stroke()
}
func (r *ringControl) Place(frame core.Bounds, _ float32) {
	r.center = core.Point{X: frame.Origin.X + frame.Size.Width/2, Y: frame.Origin.Y + frame.Size.Height/2}
	r.radius = float32(min(frame.Size.Width, frame.Size.Height)) / 2
}

func dist(a, b core.Point) float32 {
	return float32(math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y)))
}

func init() {
	input.RegisterTouchControlKind("test.ring", func(spec input.TouchControlSpec) (input.TouchControl, error) {
		axis := spec.Options["axis"]
		if axis == "" {
			return nil, loaderr.Errorf("options.axis", "name the axis the ring drives", "a ring needs options.axis")
		}
		for k := range spec.Options {
			if k != "axis" {
				return nil, errors.New("unknown option " + k)
			}
		}
		return &ringControl{action: spec.Action, axis: axis}, nil
	})
}

func TestTouchControlsBuildPluginKinds(t *testing.T) {
	doc := "version: 1\nreference: {width: 400, height: 400}\ncontrols:\n  - id: dial\n    kind: test.ring\n    anchor: center\n    size: {width: 200, height: 200}\n    action: spin\n    options: {axis: turn}\n"
	tc := newTestControls(t, doc)
	a := input.NewActions()
	tc.BindActions(a)
	tc.Resize(core.Size{Width: 400, Height: 400}, core.Edges{})

	a.OnTouch(touchAt(core.TouchStart, 3, 250, 200))
	a.Step()
	if !a.Pressed("spin") || !approx(a.Axis("turn"), 0.5, 1e-6) {
		t.Fatalf("plugin control: spin=%v turn=%v", a.Pressed("spin"), a.Axis("turn"))
	}
	if _, ok := tc.Control("dial").(*ringControl); !ok {
		t.Fatalf("Control(dial) = %T, want the plugin's control", tc.Control("dial"))
	}
	if kinds := fmt.Sprint(input.TouchControlKinds()); !strings.Contains(kinds, "test.ring") || !strings.Contains(kinds, "dpad") {
		t.Fatalf("TouchControlKinds = %s", kinds)
	}

	// A factory's located error names the control's field.
	l, err := input.ParseTouchLayout([]byte(strings.Replace(doc, "options: {axis: turn}", "options: {speed: fast}", 1)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = input.NewTouchControls(nil, l)
	if e, ok := loaderr.As(err); !ok || !strings.HasPrefix(e.Field, "controls[0]") {
		t.Fatalf("plugin factory error = %v", err)
	}
	l.Controls[0].Options = map[string]string{}
	_, err = input.NewTouchControls(nil, l)
	if e, ok := loaderr.As(err); !ok || e.Field != "controls[0].options.axis" {
		t.Fatalf("plugin factory located error = %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("registering a built-in kind twice did not panic")
		}
	}()
	input.RegisterTouchControlKind("dpad", func(input.TouchControlSpec) (input.TouchControl, error) { return nil, nil })
}

func TestTouchControlsDraw(t *testing.T) {
	tc := newTestControls(t, testLayoutYAML)
	var d core.Drawing
	tc.Draw(&d)
	if len(d.Ops) != 0 || len(d.Nodes) != 0 {
		t.Fatal("hidden controls drew")
	}
	tc.SetVisible(true)
	tc.Draw(&d)
	if len(d.Ops) == 0 {
		t.Fatal("visible controls drew nothing")
	}
	if len(d.Nodes) != 4 || d.Nodes[1].TestID != "touch-jump" || d.Nodes[1].Label != "Jump" || d.Nodes[1].Role != core.RoleButton || d.Nodes[0].Role != core.RoleGroup {
		t.Fatalf("canvas nodes = %+v", d.Nodes)
	}
}

// buttonRowYAML is three rectangular buttons side by side: hold, tap and toggle.
const buttonRowYAML = `version: 1
reference: {width: 300, height: 100}
controls:
  - {id: hold, kind: button, anchor: topLeft, size: {width: 60, height: 60}, label: Hold, action: run, shape: rect}
  - {id: tap, kind: button, anchor: topLeft, offset: {x: 60}, size: {width: 60, height: 60}, label: Tap, action: fire, mode: tap, shape: rect}
  - {id: toggle, kind: button, anchor: topLeft, offset: {x: 120}, size: {width: 60, height: 60}, label: Toggle, action: crouch, mode: toggle, shape: rect}
`

func TestTouchControlsButtonsSlideTapAndToggle(t *testing.T) {
	tc := newTestControls(t, buttonRowYAML)
	tc.Resize(core.Size{Width: 300, Height: 100}, core.Edges{})
	var ticks int
	tc.Haptics = input.NewHaptics(nil)
	tc.Haptics.OnHaptic = func(core.Haptic) { ticks++ }
	a := input.NewActions()
	tc.BindActions(a)

	a.OnTouch(touchAt(core.TouchStart, 1, 30, 30))
	a.Step()
	if !a.Pressed("run") || ticks != 1 {
		t.Fatalf("touch-down on hold: run=%v ticks=%d", a.Pressed("run"), ticks)
	}

	// Sliding onto the tap button hands the finger over and taps once.
	a.OnTouch(touchAt(core.TouchMove, 1, 90, 30))
	a.Step()
	if a.Pressed("run") || !a.JustPressed("fire") || ticks != 2 {
		t.Fatalf("slid onto tap: run=%v fire=%v ticks=%d", a.Pressed("run"), a.JustPressed("fire"), ticks)
	}
	a.Step()
	if a.Pressed("fire") || !tc.Button("tap").Held() {
		t.Fatalf("tap on the next step with the finger still down: fire=%v held=%v", a.Pressed("fire"), tc.Button("tap").Held())
	}

	// The toggle is never entered by sliding.
	a.OnTouch(touchAt(core.TouchMove, 1, 150, 30))
	a.Step()
	if a.Pressed("crouch") || tc.Button("tap").Held() || ticks != 2 {
		t.Fatalf("slid onto toggle: crouch=%v tap held=%v ticks=%d", a.Pressed("crouch"), tc.Button("tap").Held(), ticks)
	}
	a.OnTouch(touchAt(core.TouchEnd, 1, 150, 30))

	// A touch-down on the toggle turns it on until the next touch-down.
	a.OnTouch(touchAt(core.TouchStart, 2, 150, 30))
	a.OnTouch(touchAt(core.TouchEnd, 2, 150, 30))
	a.Step()
	if !a.Pressed("crouch") {
		t.Fatal("a touch-down on the toggle did not turn it on")
	}

	var d core.Drawing
	tc.Draw(&d)
	if len(d.Nodes) != 3 || d.Nodes[1].Role != core.RoleButton || !d.Nodes[2].Selected || d.Nodes[0].Selected {
		t.Fatalf("canvas nodes = %+v", d.Nodes)
	}
}

// stickPadYAML is a d-pad at the left and a stick at the right.
const stickPadYAML = `version: 1
reference: {width: 400, height: 200}
controls:
  - {id: move, kind: dpad, anchor: left, size: {width: 120, height: 120}, left: left, right: right, axisX: move}
  - {id: look, kind: stick, anchor: right, size: {width: 120, height: 120}, axisX: look_x, axisY: look_y}
`

func TestTouchControlsReleaseDPadAndStickOnCancelAndHide(t *testing.T) {
	tc := newTestControls(t, stickPadYAML)
	tc.Resize(core.Size{Width: 400, Height: 200}, core.Edges{})
	a := input.NewActions()
	tc.BindActions(a)
	move, look := frameOf(t, tc, "move"), frameOf(t, tc, "look")

	holdBoth := func() {
		t.Helper()
		a.OnTouch(touchAt(core.TouchStart, 1, move.Origin.X+move.Size.Width/2, move.Origin.Y+move.Size.Height/2))
		a.OnTouch(touchAt(core.TouchMove, 1, move.Origin.X+move.Size.Width-5, move.Origin.Y+move.Size.Height/2))
		a.OnTouch(core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{
			{ID: 1, Position: pt(move.Origin.X+move.Size.Width-5, move.Origin.Y+move.Size.Height/2)},
			{ID: 2, Position: pt(look.Origin.X+look.Size.Width/2, look.Origin.Y+look.Size.Height/2), Changed: true},
		}})
		a.OnTouch(core.TouchEvent{Phase: core.TouchMove, Touches: []core.Touch{{ID: 2, Position: pt(look.Origin.X+look.Size.Width/2, look.Origin.Y), Changed: true}}})
		a.Step()
		if !a.Pressed("right") || a.Axis("move") != 1 || a.Axis("look_y") <= 0.5 {
			t.Fatalf("holding both: right=%v move=%v look_y=%v", a.Pressed("right"), a.Axis("move"), a.Axis("look_y"))
		}
	}
	released := func(after string) {
		t.Helper()
		a.Step()
		if a.Pressed("right") || a.Axis("move") != 0 || a.Axis("look_y") != 0 || tc.DPad("move").Active() || tc.Stick("look").Active() {
			t.Fatalf("after %s: right=%v move=%v look_y=%v", after, a.Pressed("right"), a.Axis("move"), a.Axis("look_y"))
		}
	}

	// The system cancels every touch, as for an incoming call.
	holdBoth()
	a.OnTouch(core.TouchEvent{Phase: core.TouchCancel})
	released("a cancel")

	// A key hides the controls and lifts the fingers on them.
	tc.SetVisible(true)
	holdBoth()
	a.OnKeyDown(core.KeyEvent{Key: "a", Code: "KeyA"})
	a.Step()
	tc.Update(16 * time.Millisecond)
	if tc.Visible() {
		t.Fatal("a key did not hide the controls")
	}
	a.OnKeyUp(core.KeyEvent{Key: "a", Code: "KeyA"})
	released("a key")
}

func TestTouchControlsHideOnEveryKeyAfterATouch(t *testing.T) {
	// Bound to Actions but not attached to a view: the controls hear presses through the pad and keys through Update.
	tc := newTestControls(t, stickPadYAML)
	tc.Resize(core.Size{Width: 400, Height: 200}, core.Edges{})
	a := input.NewActions()
	tc.BindActions(a)
	move := frameOf(t, tc, "move")
	key := func() {
		a.OnKeyDown(core.KeyEvent{Key: "a", Code: "KeyA"})
		a.Step()
		tc.Update(16 * time.Millisecond)
		a.OnKeyUp(core.KeyEvent{Key: "a", Code: "KeyA"})
		a.Step()
		tc.Update(16 * time.Millisecond)
	}
	touch := func() {
		a.OnTouch(touchAt(core.TouchStart, 1, move.Origin.X+10, move.Origin.Y+move.Size.Height/2))
		a.OnTouch(touchAt(core.TouchEnd, 1, move.Origin.X+10, move.Origin.Y+move.Size.Height/2))
		a.Step()
		tc.Update(16 * time.Millisecond)
	}
	for i := range 2 {
		touch()
		if !tc.Visible() {
			t.Fatalf("round %d: a press did not show the controls", i)
		}
		key()
		if tc.Visible() {
			t.Fatalf("round %d: a key after a press did not hide the controls", i)
		}
	}
	// A game that shows the controls itself still has them hidden by the next key.
	tc.SetVisible(true)
	key()
	if tc.Visible() {
		t.Fatal("a key after SetVisible(true) did not hide the controls")
	}
}

func TestTouchControlsWakeAndFadeToZero(t *testing.T) {
	tc := newTestControls(t, "version: 1\nreference: {width: 400, height: 200}\nidleAfter: 1s\ncontrols:\n  - {id: jump, kind: button, anchor: bottomRight, size: {width: 80, height: 80}, label: Jump, action: jump}\n")
	tc.SetVisible(true)
	for range 40 {
		tc.Update(100 * time.Millisecond)
	}
	if tc.Alpha() != 0 {
		t.Fatalf("alpha once idle with idleAlpha 0 = %v, want 0", tc.Alpha())
	}
	// Faded out of sight, the controls still name themselves for screen readers and tests, since they still take touches.
	var d core.Drawing
	tc.Draw(&d)
	if len(d.Ops) != 0 || len(d.Nodes) != 1 || d.Nodes[0].TestID != "touch-jump" {
		t.Fatalf("faded draw: %d ops, nodes %+v", len(d.Ops), d.Nodes)
	}
	tc.Wake()
	if tc.Alpha() != 1 {
		t.Fatalf("alpha after Wake = %v, want 1", tc.Alpha())
	}
	tc.Update(900 * time.Millisecond)
	if tc.Alpha() != 1 {
		t.Fatalf("alpha before IdleAfter from Wake = %v, want 1", tc.Alpha())
	}
}

func TestTouchLayoutOptionsAreStrings(t *testing.T) {
	head := "version: 1\nreference: {width: 400, height: 400}\ncontrols:\n  - id: dial\n    kind: test.ring\n    anchor: center\n    size: {width: 200, height: 200}\n    options:\n"
	for _, bad := range []string{"      speed: 3\n", "      on: true\n", "      axis: ~\n", "      axis: [a]\n"} {
		_, err := input.ParseTouchLayout([]byte(head + bad))
		e, ok := loaderr.As(err)
		if !ok || !strings.HasPrefix(e.Field, "controls[0].options.") || e.Line != 9 {
			t.Fatalf("options %q: err = %v", bad, err)
		}
	}
	doc := input.TouchLayoutSchemaComment + "\n" + head + "      axis: turn\n      speed: \"3\"\n"
	l, err := input.ParseTouchLayout([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if l.Controls[0].Options["speed"] != "3" {
		t.Fatalf("options = %v", l.Controls[0].Options)
	}
	out, err := input.EncodeTouchLayout(l)
	if err != nil || string(out) != doc {
		t.Fatalf("a quoted number does not round-trip (%v):\n%s", err, out)
	}
}

// cooldownButton is a button a plugin could register: a VirtualButton underneath, so the pad treats it as one of its buttons, that reports itself as a switch.
type cooldownButton struct {
	b *input.VirtualButton
}

func (c *cooldownButton) Button() *input.VirtualButton               { return c.b }
func (c *cooldownButton) Claim(int, core.Point) bool                 { return false }
func (c *cooldownButton) Move(int, core.Point)                       {}
func (c *cooldownButton) Release(int)                                {}
func (c *cooldownButton) Reset()                                     { c.b.Reset() }
func (c *cooldownButton) ActionNames() []string                      { return []string{c.b.Action} }
func (c *cooldownButton) ActionPressed(name string) bool             { return name == c.b.Action && c.b.Pressed() }
func (c *cooldownButton) AxisValues(func(string, float64))           {}
func (c *cooldownButton) Draw(d *core.Drawing, s input.VirtualStyle) { c.b.Draw(d, s) }
func (c *cooldownButton) Place(frame core.Bounds, _ float32)         { c.b.Bounds = frame }
func (c *cooldownButton) Accessibility() (core.Role, bool)           { return core.RoleSwitch, c.b.Pressed() }

func init() {
	input.RegisterTouchControlKind("test.cooldown", func(spec input.TouchControlSpec) (input.TouchControl, error) {
		return &cooldownButton{b: &input.VirtualButton{Action: spec.Action, Mode: input.ButtonTap}}, nil
	})
}

func TestTouchControlsPluginButtonControl(t *testing.T) {
	tc := newTestControls(t, `version: 1
reference: {width: 300, height: 100}
controls:
  - {id: hold, kind: button, anchor: topLeft, size: {width: 60, height: 60}, action: run, shape: rect}
  - {id: blast, kind: test.cooldown, anchor: topLeft, offset: {x: 60}, size: {width: 60, height: 60}, label: Blast, action: blast}
`)
	tc.Resize(core.Size{Width: 300, Height: 100}, core.Edges{})
	a := input.NewActions()
	tc.BindActions(a)

	// A finger slides from the built-in button onto the plugin's, which taps once.
	a.OnTouch(touchAt(core.TouchStart, 1, 30, 30))
	a.Step()
	a.OnTouch(touchAt(core.TouchMove, 1, 90, 30))
	a.Step()
	if a.Pressed("run") || !a.JustPressed("blast") || tc.Button("blast") == nil {
		t.Fatalf("slid onto the plugin button: run=%v blast=%v", a.Pressed("run"), a.Pressed("blast"))
	}
	a.Step()
	if a.Pressed("blast") || !tc.Button("blast").Held() {
		t.Fatalf("plugin tap on the next step: blast=%v held=%v", a.Pressed("blast"), tc.Button("blast").Held())
	}
	var d core.Drawing
	tc.SetVisible(true)
	tc.Draw(&d)
	if d.Nodes[1].Role != core.RoleSwitch || d.Nodes[1].Label != "Blast" {
		t.Fatalf("plugin node = %+v", d.Nodes[1])
	}
	// A cancel lifts the finger from the plugin's button.
	a.OnTouch(core.TouchEvent{Phase: core.TouchCancel})
	a.Step()
	if tc.Button("blast").Held() {
		t.Fatal("a cancel left the plugin button held")
	}
}
