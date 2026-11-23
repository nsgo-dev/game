package input_test

import (
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

var desktop = nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop})

// watchKeys binds actions with bind and has the harness hear keys anywhere on the window.
func watchKeys(t *testing.T, watch []string, bind func(a *input.Actions)) *harness {
	t.Helper()
	return startHarness(t, watch, func(cx *core.Scope, a *input.Actions, _ *core.View) {
		bind(a)
		a.WatchKeys(cx)
	}, desktop)
}

// focusedKeys binds actions with bind and has the harness hear keys only through its focused view, as Attach wires it; presses counts the view's own OnPress.
func focusedKeys(t *testing.T, watch []string, presses *int, bind func(a *input.Actions)) *harness {
	t.Helper()
	h := startHarness(t, watch, func(_ *core.Scope, a *input.Actions, v *core.View) {
		bind(a)
		v.Focusable = func() bool { return true }
		v.OnPress = func(core.PressEvent) { *presses++ }
	}, desktop)
	h.app.Focus("id:game")
	return h
}

func TestHeldKeyHoldsPressedAcrossSteps(t *testing.T) {
	for _, path := range []string{"watched", "focused"} {
		t.Run(path, func(t *testing.T) {
			bind := func(a *input.Actions) { a.Bind("jump", input.Key("Space")) }
			var h *harness
			var presses int
			if path == "watched" {
				h = watchKeys(t, []string{"jump"}, bind)
			} else {
				h = focusedKeys(t, []string{"jump"}, &presses, bind)
			}
			h.step()
			expectState(t, "idle", h.last("jump"), stepState{})

			h.app.KeyDown("space")
			h.step()
			expectState(t, "first held step", h.last("jump"), stepState{pressed: true, justPressed: true})
			for range 4 {
				h.app.KeyDown("space") // auto-repeat
				h.step()
				expectState(t, "later held step", h.last("jump"), stepState{pressed: true})
			}

			h.app.KeyUp("space")
			h.step()
			expectState(t, "release step", h.last("jump"), stepState{justReleased: true})
			h.step()
			expectState(t, "after release", h.last("jump"), stepState{})
			if presses != 0 {
				t.Fatalf("the bound space pressed the view %d times, want it consumed", presses)
			}
		})
	}
}

func TestKeyTapBetweenStepsIsLatched(t *testing.T) {
	h := watchKeys(t, []string{"fire"}, func(a *input.Actions) { a.Bind("fire", input.Key("x")) })
	h.step()
	h.app.KeyDown("x")
	h.app.KeyUp("x")
	h.step()
	expectState(t, "tap step", h.last("fire"), stepState{pressed: true, justPressed: true})
	h.step()
	expectState(t, "after tap", h.last("fire"), stepState{justReleased: true})
}

func TestOppositeKeysDriveAxis(t *testing.T) {
	for _, path := range []string{"watched", "focused"} {
		t.Run(path, func(t *testing.T) {
			bind := func(a *input.Actions) {
				a.Bind("left", input.Key("left"), input.KeyCode("KeyA"))
				a.Bind("right", input.Key("right"), input.KeyCode("KeyD"))
				a.BindAxis("move_x", "left", "right")
			}
			var h *harness
			var presses int
			if path == "watched" {
				h = watchKeys(t, nil, bind)
			} else {
				h = focusedKeys(t, nil, &presses, bind)
			}
			axis := func(label string, want float64) {
				t.Helper()
				h.step()
				if got := h.actions.Axis("move_x"); got != want {
					t.Fatalf("%s: Axis = %v, want %v", label, got, want)
				}
			}
			axis("idle", 0)
			h.app.KeyDown("left")
			axis("left held", -1)
			h.app.KeyDown("right")
			axis("both held", 0)
			h.app.KeyUp("left")
			axis("right held", 1)
			h.app.KeyUp("right")
			axis("none held", 0)

			h.app.KeyCodeDown("KeyA")
			axis("KeyA held", -1)
			h.app.KeyCodeDown("KeyD")
			axis("KeyA and KeyD held", 0)
			h.app.KeyCodeUp("KeyA")
			axis("KeyD held", 1)
			h.app.KeyCodeUp("KeyD")
			axis("no position held", 0)
		})
	}
}

func TestKeyCodeFollowsThePositionNotTheLayout(t *testing.T) {
	h := startHarness(t, []string{"up", "named_w"}, func(cx *core.Scope, a *input.Actions, _ *core.View) {
		a.Bind("up", input.KeyCode("KeyW"))
		a.Bind("named_w", input.Key("w"))
		a.WatchKeys(cx)
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, KeyCodeNames: map[string]string{"KeyW": "z"}}))
	h.app.KeyCodeDown("KeyW")
	h.step()
	expectState(t, "KeyW by position", h.last("up"), stepState{pressed: true, justPressed: true})
	expectState(t, "w by name on AZERTY", h.last("named_w"), stepState{})
	h.app.KeyCodeUp("KeyW")
}

func TestKeysReleasedWhenWindowLosesFocus(t *testing.T) {
	for _, path := range []string{"watched", "focused"} {
		t.Run(path, func(t *testing.T) {
			// The keys are held by name: nsgotest's BlurWindow releases a key KeyCodeDown holds under its position as its name and without its position, so neither core.KeyCodesDown nor OnKeyUp hears it come up (reported as an NSGo issue).
			bind := func(a *input.Actions) {
				a.Bind("left", input.Key("left"))
				a.Bind("right", input.Key("right"))
				a.BindAxis("move_x", "left", "right")
			}
			var h *harness
			var presses int
			if path == "watched" {
				h = watchKeys(t, []string{"left", "right"}, bind)
			} else {
				h = focusedKeys(t, []string{"left", "right"}, &presses, bind)
			}
			h.app.KeyDown("left")
			h.app.KeyDown("right")
			h.step()
			expectState(t, "left before blur", h.last("left"), stepState{pressed: true, justPressed: true})
			expectState(t, "right before blur", h.last("right"), stepState{pressed: true, justPressed: true})

			h.app.BlurWindow()
			h.step()
			expectState(t, "left after blur", h.last("left"), stepState{justReleased: true})
			expectState(t, "right after blur", h.last("right"), stepState{justReleased: true})
			if got := h.actions.Axis("move_x"); got != 0 {
				t.Fatalf("Axis after blur = %v, want 0", got)
			}
			h.step()
			expectState(t, "left later", h.last("left"), stepState{})
		})
	}
}

func TestUnboundKeyIsNotConsumed(t *testing.T) {
	var presses int
	h := focusedKeys(t, []string{"jump"}, &presses, func(a *input.Actions) { a.Bind("jump", input.Key("x")) })
	h.app.Key("space")
	if presses != 1 {
		t.Fatalf("an unbound space pressed the view %d times, want 1", presses)
	}
	h.step()
	expectState(t, "jump after space", h.last("jump"), stepState{})
}

func TestResetReleasesHeldKeys(t *testing.T) {
	var presses int
	h := focusedKeys(t, []string{"jump"}, &presses, func(a *input.Actions) { a.Bind("jump", input.Key("space")) })
	h.app.KeyDown("space")
	h.step()
	expectState(t, "held", h.last("jump"), stepState{pressed: true, justPressed: true})
	h.app.Post(h.actions.Reset)
	h.app.Settle()
	h.step()
	expectState(t, "after Reset", h.last("jump"), stepState{})
	h.app.KeyUp("space")
	h.step()
	expectState(t, "after the key comes up", h.last("jump"), stepState{})
}

func TestOnKeyEventsWithoutAView(t *testing.T) {
	a := input.NewActions()
	a.Bind("left", input.Key("left"))
	a.Bind("right", input.KeyCode("ShiftRight"))
	a.BindAxis("x", "left", "right")

	if !a.OnKeyDown(core.KeyEvent{Key: "left", Code: "ArrowLeft"}) {
		t.Fatal("a bound key was not consumed")
	}
	if a.OnKeyDown(core.KeyEvent{Key: "q", Code: "KeyQ"}) {
		t.Fatal("an unbound key was consumed")
	}
	a.Step()
	if got := a.Axis("x"); got != -1 {
		t.Fatalf("Axis = %v, want -1", got)
	}
	// Both Shifts are held as one name but two positions; releasing the left one keeps ShiftRight held.
	a.OnKeyDown(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	a.OnKeyDown(core.KeyEvent{Key: "shift", Code: "ShiftRight"})
	a.OnKeyUp(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	a.OnKeyUp(core.KeyEvent{Key: "left", Code: "ArrowLeft"})
	a.Step()
	if got := a.Axis("x"); got != 1 {
		t.Fatalf("Axis with ShiftRight held = %v, want 1", got)
	}
	// A repeat of a key whose down the view never heard does not hold it.
	a.OnKeyDown(core.KeyEvent{Key: "left", Code: "ArrowLeft", Repeat: true})
	a.OnKeyUp(core.KeyEvent{Key: "shift", Code: "ShiftRight"})
	a.Step()
	if a.Pressed("left") || a.Axis("x") != 0 {
		t.Fatalf("after a stray repeat: left %v, Axis %v", a.Pressed("left"), a.Axis("x"))
	}
	// A key without a position comes up by name.
	a.OnKeyDown(core.KeyEvent{Key: "left"})
	a.OnKeyUp(core.KeyEvent{Key: "left", Code: "ArrowLeft"})
	a.Step()
	a.Step()
	if a.Pressed("left") {
		t.Fatal("a key without a position stayed held after it came up")
	}
	if input.Key("") != (input.Input{Kind: input.InputKey}) {
		t.Fatal("Key(\"\") is not an empty key input")
	}
	a.Bind("never", input.Key(""), input.KeyCode(""))
	a.OnKeyDown(core.KeyEvent{})
	a.Step()
	if a.Pressed("never") {
		t.Fatal("an empty key input became active")
	}
}

func TestKeyBindingsSaveAndLoad(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	a := input.NewActions()
	want := []input.Input{input.Key("Space"), input.KeyCode("KeyW"), input.MouseButton(core.MouseButtonLeft)}
	a.Bind("jump", want...)
	if err := a.Save(app.Storage(), "controls"); err != nil {
		t.Fatal(err)
	}
	b := input.NewActions()
	if ok, err := b.Load(app.Storage(), "controls"); !ok || err != nil {
		t.Fatalf("Load = (%v, %v)", ok, err)
	}
	if got := b.Bindings("jump"); !slices.Equal(got, want) {
		t.Fatalf("loaded %+v, want %+v", got, want)
	}

	a.Bind("broken", input.Input{Kind: input.InputKeyCode})
	if err := a.Save(app.Storage(), "broken"); err == nil {
		t.Fatal("saving a key_code input with no code succeeded")
	}
	for name, raw := range map[string]string{
		"key without key":       `{"version":1,"actions":{"jump":[{"kind":"key"}]}}`,
		"key_code without code": `{"version":1,"actions":{"jump":[{"kind":"key_code"}]}}`,
	} {
		if err := app.Storage().SetItem("bad", raw); err != nil {
			t.Fatal(err)
		}
		c := input.NewActions()
		if ok, err := c.Load(app.Storage(), "bad"); ok || err == nil {
			t.Fatalf("Load(%s) = (%v, %v), want an error", name, ok, err)
		}
	}
}
