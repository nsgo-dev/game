package input_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type padStep struct {
	jump, jumpJust, jumpUp bool
	fire                   bool
	move, look             float64
}

type padHarness struct {
	app     *nsgotest.App
	actions *input.Actions
	shown   *core.Signal[bool]
	steps   int
	last    padStep
}

// startPadHarness runs a game.Loop that steps actions and reads the controllers through WatchGamepads, called in a child scope that is mounted while shown is true.
func startPadHarness(t *testing.T, setup func(a *input.Actions)) *padHarness {
	t.Helper()
	h := &padHarness{actions: input.NewActions()}
	setup(h.actions)
	h.app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		h.shown = core.NewSignal(cx, true)
		loop := &game.Loop{
			Rate: 60,
			Update: func(time.Duration) {
				a := h.actions
				a.Step()
				h.steps++
				h.last = padStep{
					jump:     a.Pressed("jump"),
					jumpJust: a.JustPressed("jump"),
					jumpUp:   a.JustReleased("jump"),
					fire:     a.Pressed("fire"),
					move:     a.Axis("move"),
					look:     a.Axis("look"),
				}
			},
		}
		loop.Run(cx)
		return core.Show{
			When: h.shown.Get,
			Then: func(child *core.Scope) core.Node {
				h.actions.WatchGamepads(child)
				return core.View{}
			},
		}
	}, nsgotest.Environment(core.Environment{Gamepads: true}))
	return h
}

func (h *padHarness) step() padStep {
	before := h.steps
	for h.steps == before {
		h.app.Advance(frameStep)
	}
	return h.last
}

func bindPadDefaults(a *input.Actions) {
	a.Bind("jump", input.GamepadButton(gamepad.ButtonSouth))
	a.Bind("fire", input.GamepadButton(gamepad.ButtonRT))
	a.Bind("left", input.GamepadButton(gamepad.ButtonDPadLeft))
	a.Bind("right", input.GamepadButton(gamepad.ButtonDPadRight))
	a.BindAxis("move", "left", "right")
	a.BindGamepadAxis("move", gamepad.AxisLeftX, input.GamepadAxisOptions{})
	a.BindGamepadAxis("look", gamepad.AxisRightY, input.GamepadAxisOptions{DeadZone: 0.2, Axial: true, Invert: true})
}

func expectAxis(t *testing.T, label string, got, want float64) {
	t.Helper()
	if !approx(got, want, 1e-6) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func TestGamepadButtonsDriveActionsAndSticksDriveAxisWithDeadZone(t *testing.T) {
	h := startPadHarness(t, bindPadDefaults)

	if s := h.step(); s != (padStep{}) {
		t.Fatalf("no controller: %+v, want zero", s)
	}

	h.app.ConnectGamepad(1, "Xbox Wireless Controller")
	if s := h.step(); s != (padStep{}) {
		t.Fatalf("idle controller: %+v, want zero", s)
	}

	// A face button drives an action with the usual edges.
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	if s := h.step(); !s.jump || !s.jumpJust || s.jumpUp {
		t.Fatalf("press: %+v", s)
	}
	if s := h.step(); !s.jump || s.jumpJust {
		t.Fatalf("hold: %+v", s)
	}
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 0)
	if s := h.step(); s.jump || !s.jumpUp {
		t.Fatalf("release: %+v", s)
	}

	// An analog trigger counts as pressed from 0.5.
	h.app.SetGamepadButton(1, gamepad.ButtonRT, 0.4)
	if s := h.step(); s.fire {
		t.Fatal("trigger at 0.4 pressed")
	}
	h.app.SetGamepadButton(1, gamepad.ButtonRT, 0.6)
	if s := h.step(); !s.fire {
		t.Fatal("trigger at 0.6 not pressed")
	}

	// The default radial dead zone swallows small deflections and rescales the rest.
	h.app.SetGamepadAxis(1, gamepad.AxisLeftX, 0.1)
	expectAxis(t, "move inside dead zone", h.step().move, 0)
	h.app.SetGamepadAxis(1, gamepad.AxisLeftX, -0.575)
	expectAxis(t, "move half left", h.step().move, -0.5)
	h.app.SetGamepadAxis(1, gamepad.AxisLeftX, 0.6)
	h.app.SetGamepadAxis(1, gamepad.AxisLeftY, 0.8)
	expectAxis(t, "move on full diagonal", h.step().move, 0.6)
	// Radial: a small X with a large Y still moves, keeping the stick's direction.
	h.app.SetGamepadAxis(1, gamepad.AxisLeftX, 0.1)
	mag := math.Hypot(0.1, 0.8)
	expectAxis(t, "move small x with large y", h.step().move, float64(float32(0.1))/mag*(mag-0.15)/0.85)

	// D-pad actions and the stick sum into the same axis, clamped.
	h.app.SetGamepadAxis(1, gamepad.AxisLeftY, 0)
	h.app.SetGamepadAxis(1, gamepad.AxisLeftX, 0.575)
	h.app.SetGamepadButton(1, gamepad.ButtonDPadRight, 1)
	expectAxis(t, "dpad plus stick", h.step().move, 1)
	h.app.SetGamepadButton(1, gamepad.ButtonDPadRight, 0)
	h.app.SetGamepadButton(1, gamepad.ButtonDPadLeft, 1)
	expectAxis(t, "dpad left plus stick right", h.step().move, -0.5)
	h.app.SetGamepadButton(1, gamepad.ButtonDPadLeft, 0)

	// An axial, inverted dead zone ignores the other axis of the stick.
	h.app.SetGamepadAxis(1, gamepad.AxisRightX, 0.9)
	h.app.SetGamepadAxis(1, gamepad.AxisRightY, 0.15)
	expectAxis(t, "look inside axial dead zone", h.step().look, 0)
	h.app.SetGamepadAxis(1, gamepad.AxisRightY, -0.6)
	expectAxis(t, "look up inverted", h.step().look, 0.5)

	// Disconnecting releases everything the controller held.
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	if s := h.step(); !s.jump {
		t.Fatal("jump not held before disconnect")
	}
	h.app.DisconnectGamepad(1)
	if s := h.step(); s.jump || !s.jumpUp || s.fire || s.move != 0 || s.look != 0 {
		t.Fatalf("after disconnect: %+v", s)
	}
}

func TestGamepadSelectionAndSeveralControllers(t *testing.T) {
	h := startPadHarness(t, bindPadDefaults)
	h.app.ConnectGamepad(1, "DualSense")
	h.app.ConnectGamepad(2, "Pro Controller")

	// Any controller presses a button; the stick farthest from center wins.
	h.app.SetGamepadButton(2, gamepad.ButtonSouth, 1)
	h.app.SetGamepadAxis(1, gamepad.AxisLeftX, -0.32)
	h.app.SetGamepadAxis(2, gamepad.AxisLeftX, 0.575)
	s := h.step()
	if !s.jump {
		t.Fatal("button on controller 2 not seen")
	}
	expectAxis(t, "farthest stick", s.move, 0.5)
	if pads := h.actions.Gamepads(); len(pads) != 2 || pads[0].ID != 1 || pads[1].ID != 2 {
		t.Fatalf("Gamepads() = %+v, want controllers 1 and 2", pads)
	}

	h.app.Post(func() { h.actions.SelectGamepad(1) })
	h.app.Settle()
	s = h.step()
	if s.jump || !s.jumpUp {
		t.Fatalf("controller 2 still drives jump after SelectGamepad(1): %+v", s)
	}
	expectAxis(t, "selected stick", s.move, -0.2)
	if id, ok := h.actions.SelectedGamepad(); !ok || id != 1 {
		t.Fatalf("SelectedGamepad() = (%d, %v), want (1, true)", id, ok)
	}

	h.app.Post(func() { h.actions.SelectAnyGamepad() })
	h.app.Settle()
	if s := h.step(); !s.jump {
		t.Fatal("SelectAnyGamepad did not restore controller 2")
	}
	if _, ok := h.actions.SelectedGamepad(); ok {
		t.Fatal("SelectedGamepad reports a selection after SelectAnyGamepad")
	}

	// Disposing the watching scope stops reading and releases the controllers.
	h.app.Post(func() { h.shown.Set(false) })
	h.app.Settle()
	if s := h.step(); s.jump || s.move != 0 {
		t.Fatalf("controllers still read after the watching scope was disposed: %+v", s)
	}
}

func TestGamepadButtonHeldAcrossResetStaysInactiveUntilReleased(t *testing.T) {
	h := startPadHarness(t, bindPadDefaults)
	h.app.ConnectGamepad(1, "Xbox Wireless Controller")
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	if s := h.step(); !s.jumpJust {
		t.Fatalf("press: %+v", s)
	}

	// A scene change resets the actions while South is still held: the new scene must not see a fresh press.
	h.app.Post(h.actions.Reset)
	h.app.Settle()
	for range 2 {
		if s := h.step(); s.jump || s.jumpJust {
			t.Fatalf("button held across Reset registered: %+v", s)
		}
	}
	// Another button pressed after Reset is not affected.
	h.app.SetGamepadButton(1, gamepad.ButtonRT, 1)
	if s := h.step(); !s.fire || s.jump {
		t.Fatalf("fresh trigger after Reset: %+v", s)
	}

	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 0)
	if s := h.step(); s.jump || s.jumpUp {
		t.Fatalf("release of a muted button: %+v", s)
	}
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	if s := h.step(); !s.jump || !s.jumpJust {
		t.Fatalf("press after release: %+v", s)
	}
}

func TestWatchGamepadsNilReleasesControllerInputs(t *testing.T) {
	h := startPadHarness(t, bindPadDefaults)
	h.app.ConnectGamepad(1, "DualSense")
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	h.app.SetGamepadAxis(1, gamepad.AxisLeftX, 1)
	if s := h.step(); !s.jump || s.move != 1 {
		t.Fatalf("before detaching: %+v", s)
	}

	h.app.Post(func() { h.actions.WatchGamepads(nil) })
	h.app.Settle()
	if s := h.step(); s.jump || !s.jumpUp || s.move != 0 {
		t.Fatalf("after WatchGamepads(nil): %+v", s)
	}
	if pads := h.actions.Gamepads(); len(pads) != 0 {
		t.Fatalf("Gamepads() after WatchGamepads(nil) = %+v", pads)
	}
}

// denyGamepads is a core.Access that refuses only core.AccessGamepad.
type denyGamepads struct{}

func (denyGamepads) Allows(req core.AccessRequest) bool { return req.Permission != core.AccessGamepad }
func (denyGamepads) WebHosts() []string                 { return nil }

func TestWatchGamepadsReadsNothingWhenAccessIsRefused(t *testing.T) {
	a := input.NewActions()
	bindPadDefaults(a)
	var last padStep
	steps := 0
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.ProvideAccess(denyGamepads{})
		a.WatchGamepads(cx)
		loop := &game.Loop{
			Rate: 60,
			Update: func(time.Duration) {
				a.Step()
				steps++
				last = padStep{jump: a.Pressed("jump"), move: a.Axis("move")}
			},
		}
		loop.Run(cx)
		return core.View{}
	}, nsgotest.Environment(core.Environment{Gamepads: true}))
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	app.SetGamepadAxis(1, gamepad.AxisLeftX, 1)
	for before := steps; steps < before+2; {
		app.Advance(frameStep)
	}
	if last.jump || last.move != 0 {
		t.Fatalf("controller read although core.AccessGamepad is refused: %+v", last)
	}
	if pads := a.Gamepads(); len(pads) != 0 {
		t.Fatalf("Gamepads() = %+v, want none", pads)
	}
}

func TestGamepadsSortByIDWithExtremeIDs(t *testing.T) {
	a := input.NewActions()
	a.BindGamepadAxis("x", gamepad.AxisLeftX, input.GamepadAxisOptions{DeadZone: -1})
	// Both read the same distance from center, so the axis keeps the first controller in ID order.
	a.OnGamepads([]gamepad.Pad{
		pad(math.MaxInt, nil, [4]float32{0.5, 0}),
		pad(math.MinInt, nil, [4]float32{-0.5, 0}),
	})
	a.Step()
	if pads := a.Gamepads(); len(pads) != 2 || pads[0].ID != math.MinInt || pads[1].ID != math.MaxInt {
		t.Fatalf("Gamepads() IDs = %v, want [MinInt MaxInt]", []int{pads[0].ID, pads[1].ID})
	}
	expectAxis(t, "tie keeps the lowest ID", a.Axis("x"), -0.5)
}

func pad(id int, buttons map[gamepad.Button]float32, axes [gamepad.AxisCount]float32) gamepad.Pad {
	p := gamepad.Pad{ID: id, Axes: axes}
	for b, v := range buttons {
		p.Buttons[b] = v
	}
	return p
}

func TestOnGamepadsLatchesAQuickPressBetweenSteps(t *testing.T) {
	a := input.NewActions()
	a.Bind("jump", input.GamepadButton(gamepad.ButtonSouth))
	a.Step()

	a.OnGamepads([]gamepad.Pad{pad(7, map[gamepad.Button]float32{gamepad.ButtonSouth: 1}, [4]float32{})})
	a.OnGamepads([]gamepad.Pad{pad(7, nil, [4]float32{})})
	a.Step()
	if !a.Pressed("jump") || !a.JustPressed("jump") {
		t.Fatal("quick press between steps was lost")
	}
	a.Step()
	if a.Pressed("jump") || !a.JustReleased("jump") {
		t.Fatal("quick press did not release on the next step")
	}

	// Reset drops the controller state; a fresh press after it registers again.
	a.OnGamepads([]gamepad.Pad{pad(7, map[gamepad.Button]float32{gamepad.ButtonSouth: 1}, [4]float32{})})
	a.Reset()
	if got := a.Gamepads(); len(got) != 0 {
		t.Fatalf("Gamepads() after Reset = %+v", got)
	}
	a.Step()
	if a.Pressed("jump") {
		t.Fatal("jump pressed after Reset")
	}

	// A button outside the standard layout is never active.
	a.Bind("bad", input.GamepadButton(gamepad.ButtonCount))
	full := pad(7, nil, [4]float32{})
	for i := range full.Buttons {
		full.Buttons[i] = 1
	}
	a.OnGamepads([]gamepad.Pad{full})
	a.Step()
	if a.Pressed("bad") {
		t.Fatal("out-of-range button is active")
	}
}

func TestGamepadAxisOptions(t *testing.T) {
	nan := float32(math.NaN())
	cases := []struct {
		name string
		axis gamepad.Axis
		opts input.GamepadAxisOptions
		axes [4]float32
		want float64
	}{
		{"default dead zone", gamepad.AxisLeftX, input.GamepadAxisOptions{}, [4]float32{0.15, 0}, 0},
		{"dead zone off", gamepad.AxisLeftX, input.GamepadAxisOptions{DeadZone: -1}, [4]float32{0.1, 0}, float64(float32(0.1))},
		{"custom radial", gamepad.AxisLeftY, input.GamepadAxisOptions{DeadZone: 0.5}, [4]float32{0, 0.75}, 0.5},
		{"radial corner beyond unit circle", gamepad.AxisLeftX, input.GamepadAxisOptions{}, [4]float32{1, 1}, 1 / math.Sqrt2},
		{"axial corner", gamepad.AxisLeftX, input.GamepadAxisOptions{Axial: true}, [4]float32{1, 1}, 1},
		{"sensitivity clamps", gamepad.AxisRightX, input.GamepadAxisOptions{DeadZone: -1, Sensitivity: 3}, [4]float32{0, 0, 0.5, 0}, 1},
		{"invert", gamepad.AxisRightY, input.GamepadAxisOptions{DeadZone: -1, Invert: true}, [4]float32{0, 0, 0, 0.5}, -0.5},
		{"dead zone of one reads zero", gamepad.AxisLeftX, input.GamepadAxisOptions{DeadZone: 1}, [4]float32{1, 0}, 0},
		{"NaN reading", gamepad.AxisLeftX, input.GamepadAxisOptions{}, [4]float32{nan, nan}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := input.NewActions()
			a.BindGamepadAxis("x", tc.axis, tc.opts)
			a.OnGamepads([]gamepad.Pad{pad(0, nil, tc.axes)})
			a.Step()
			expectAxis(t, "Axis", a.Axis("x"), tc.want)
		})
	}

	a := input.NewActions()
	a.BindGamepadAxis("x", gamepad.AxisRightY, input.GamepadAxisOptions{DeadZone: 0.3, Invert: true})
	if ax, opts, ok := a.GamepadAxisBinding("x"); !ok || ax != gamepad.AxisRightY || opts != (input.GamepadAxisOptions{DeadZone: 0.3, Invert: true}) {
		t.Fatalf("GamepadAxisBinding = (%v, %+v, %v)", ax, opts, ok)
	}
	a.BindGamepadAxis("x", gamepad.AxisCount, input.GamepadAxisOptions{})
	if _, _, ok := a.GamepadAxisBinding("x"); ok {
		t.Fatal("binding an out-of-range axis did not unbind")
	}
	a.BindGamepadAxis("x", gamepad.AxisLeftX, input.GamepadAxisOptions{})
	a.UnbindGamepadAxis("x")
	a.OnGamepads([]gamepad.Pad{pad(0, nil, [4]float32{1, 0})})
	a.Step()
	if _, _, ok := a.GamepadAxisBinding("x"); ok || a.Axis("x") != 0 {
		t.Fatal("UnbindGamepadAxis left the axis bound")
	}
}

func TestGamepadBindingsSaveAndLoad(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	store := app.Storage()

	a := input.NewActions()
	a.Bind("jump", input.GamepadButton(gamepad.ButtonSouth), input.MouseButton(core.MouseButtonLeft))
	a.Bind("pause", input.GamepadButton(gamepad.ButtonStart))
	a.Bind("menu", input.GamepadButton(gamepad.ButtonHome))
	a.BindGamepadAxis("move_x", gamepad.AxisLeftX, input.GamepadAxisOptions{})
	a.BindGamepadAxis("look_y", gamepad.AxisRightY, input.GamepadAxisOptions{DeadZone: -1, Axial: true, Sensitivity: 2, Invert: true})
	if err := a.Save(store, "controls"); err != nil {
		t.Fatal(err)
	}
	first, _ := store.GetItem("controls")

	b := input.NewActions()
	if ok, err := b.Load(store, "controls"); !ok || err != nil {
		t.Fatalf("Load = (%v, %v)", ok, err)
	}
	for _, name := range []string{"jump", "pause", "menu"} {
		if !slices.Equal(a.Bindings(name), b.Bindings(name)) {
			t.Fatalf("%s: loaded %+v, want %+v", name, b.Bindings(name), a.Bindings(name))
		}
	}
	for _, name := range []string{"move_x", "look_y"} {
		wa, wo, _ := a.GamepadAxisBinding(name)
		ga, gotOpts, ok := b.GamepadAxisBinding(name)
		if !ok || ga != wa || gotOpts != wo {
			t.Fatalf("%s: loaded (%v, %+v, %v), want (%v, %+v)", name, ga, gotOpts, ok, wa, wo)
		}
	}
	// Saving what was loaded writes the same text.
	if err := b.Save(store, "controls"); err != nil {
		t.Fatal(err)
	}
	if second, _ := store.GetItem("controls"); second != first {
		t.Fatalf("round trip changed the saved text:\n%s\n%s", first, second)
	}

	invalid := map[string]string{
		"unknown gamepad button":     `{"version":1,"actions":{"jump":[{"kind":"gamepad_button","button":"turbo"}]}}`,
		"unknown gamepad axis":       `{"version":1,"gamepad_axes":{"move":{"axis":"left_z"}}}`,
		"unknown gamepad axis field": `{"version":1,"gamepad_axes":{"m":{"axis":"left_x","bogus":1}}}`,
		"misspelled dead zone":       `{"version":1,"gamepad_axes":{"m":{"axis":"left_x","deadzone":0.3}}}`,
		"unknown top-level field":    `{"version":1,"gamepad_axes":{},"keyboard":{}}`,
		"trailing data":              `{"version":1}{}`,
	}
	for name, raw := range invalid {
		if err := store.SetItem("bad", raw); err != nil {
			t.Fatal(err)
		}
		c := input.NewActions()
		c.Bind("jump", input.GamepadButton(gamepad.ButtonEast))
		if ok, err := c.Load(store, "bad"); ok || err == nil {
			t.Fatalf("%s: Load = (%v, %v), want an error", name, ok, err)
		}
		if !slices.Equal(c.Bindings("jump"), []input.Input{input.GamepadButton(gamepad.ButtonEast)}) {
			t.Fatalf("%s: failed Load changed bindings", name)
		}
	}
}

func TestGamepadAxisNonFiniteOptionsStillSave(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	store := app.Storage()
	nan, inf := math.NaN(), math.Inf(1)

	a := input.NewActions()
	a.BindGamepadAxis("nan", gamepad.AxisLeftX, input.GamepadAxisOptions{DeadZone: nan, Sensitivity: nan})
	a.BindGamepadAxis("inf", gamepad.AxisLeftY, input.GamepadAxisOptions{DeadZone: inf, Sensitivity: -inf})
	a.BindGamepadAxis("off", gamepad.AxisRightX, input.GamepadAxisOptions{DeadZone: -inf, Sensitivity: inf})
	want := map[string]input.GamepadAxisOptions{
		"nan": {},
		"inf": {DeadZone: 1},
		"off": {DeadZone: -1},
	}
	pads := []gamepad.Pad{pad(0, nil, [4]float32{0.1, 0.9, 0.1, 0})}
	a.OnGamepads(pads)
	a.Step()
	before := map[string]float64{}
	for name, opts := range want {
		if _, got, _ := a.GamepadAxisBinding(name); got != opts {
			t.Fatalf("%s: options %+v, want %+v", name, got, opts)
		}
		before[name] = a.Axis(name)
	}
	if err := a.Save(store, "controls"); err != nil {
		t.Fatalf("Save with non-finite options: %v", err)
	}
	b := input.NewActions()
	if ok, err := b.Load(store, "controls"); !ok || err != nil {
		t.Fatalf("Load = (%v, %v)", ok, err)
	}
	b.OnGamepads(pads)
	b.Step()
	for name := range want {
		expectAxis(t, name+" after round trip", b.Axis(name), before[name])
	}
}

func FuzzGamepadAxis(f *testing.F) {
	f.Add(float32(0.5), float32(0.5), 0.0, 0.0, false, false)
	f.Add(float32(1), float32(-1), 0.3, 2.0, true, true)
	f.Add(float32(0.1), float32(0), 0.15, 1.0, false, false)
	f.Fuzz(func(t *testing.T, x, y float32, deadZone, sensitivity float64, axial, invert bool) {
		a := input.NewActions()
		opts := input.GamepadAxisOptions{DeadZone: deadZone, Axial: axial, Sensitivity: sensitivity, Invert: invert}
		a.BindGamepadAxis("x", gamepad.AxisLeftX, opts)
		a.BindGamepadAxis("y", gamepad.AxisLeftY, opts)
		a.OnGamepads([]gamepad.Pad{pad(0, nil, [4]float32{x, y})})
		a.Step()
		vx, vy := a.Axis("x"), a.Axis("y")
		for _, v := range []float64{vx, vy} {
			if math.IsNaN(v) || v < -1 || v > 1 {
				t.Fatalf("axis value %v outside [-1, 1] for x=%v y=%v opts=%+v", v, x, y, opts)
			}
		}
		// Inside a radial dead zone, the whole stick reads zero.
		dz := deadZone
		if dz == 0 {
			dz = input.DefaultGamepadDeadZone
		}
		fx, fy := float64(x), float64(y)
		if !axial && dz > 0 && dz < 1 && !math.IsNaN(fx) && !math.IsNaN(fy) && math.Hypot(fx, fy) <= dz && (vx != 0 || vy != 0) {
			t.Fatalf("stick (%v, %v) inside dead zone %v read (%v, %v)", x, y, dz, vx, vy)
		}
	})
}

func BenchmarkActionsStepGamepads(b *testing.B) {
	a := input.NewActions()
	bindPadDefaults(a)
	pads := []gamepad.Pad{
		pad(1, map[gamepad.Button]float32{gamepad.ButtonSouth: 1}, [4]float32{0.5, -0.3, 0.1, 0.9}),
		pad(2, map[gamepad.Button]float32{gamepad.ButtonRT: 0.7}, [4]float32{-0.2, 0.4, 0, 0}),
	}
	b.ReportAllocs()
	for b.Loop() {
		a.OnGamepads(pads)
		a.Step()
		_ = a.Axis("move")
	}
}
