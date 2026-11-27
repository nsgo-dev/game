package input_test

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestAssistHoldToToggleLifecycleAndEdgeCases(t *testing.T) {
	a := input.NewActions()
	a.Bind("sprint", input.Key("shift"), input.GamepadButton(gamepad.ButtonLB))
	a.Bind("pause", input.Key("escape"))
	a.SetActionAssist("sprint", input.ActionAssist{Toggle: true})
	a.SetActionAssist("pause", input.ActionAssist{}) // menu action must never latch
	a.SetAssists(input.Assists{
		Toggle: []string{"sprint", "pause"},
	})

	// Press 1: latches sprint on.
	a.OnKeyDown(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	a.Step()
	if !a.Pressed("sprint") || !a.JustPressed("sprint") || a.JustReleased("sprint") {
		t.Fatalf("press 1: pressed=%v justPressed=%v justReleased=%v, want true/true/false",
			a.Pressed("sprint"), a.JustPressed("sprint"), a.JustReleased("sprint"))
	}
	if !a.ToggleActive("sprint") {
		t.Fatal("expected ToggleActive(sprint) == true")
	}

	// While holding Shift, pressing combined binding ButtonLB must NOT release the toggle.
	a.OnGamepads([]gamepad.Pad{{ID: 1, Buttons: [gamepad.ButtonCount]float32{gamepad.ButtonLB: 1}}})
	a.Step()
	if !a.Pressed("sprint") || a.JustPressed("sprint") {
		t.Fatalf("combined hold: pressed=%v justPressed=%v, want true/false", a.Pressed("sprint"), a.JustPressed("sprint"))
	}

	// Releasing both bindings keeps sprint latched on.
	a.OnKeyUp(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	a.OnGamepads([]gamepad.Pad{{ID: 1}})
	a.Step()
	if !a.Pressed("sprint") || a.JustPressed("sprint") || a.JustReleased("sprint") {
		t.Fatalf("after release: pressed=%v justPressed=%v justReleased=%v, want true/false/false",
			a.Pressed("sprint"), a.JustPressed("sprint"), a.JustReleased("sprint"))
	}

	// Press 2 via the other bound input (gamepad LB) releases the toggle.
	a.OnGamepads([]gamepad.Pad{{ID: 1, Buttons: [gamepad.ButtonCount]float32{gamepad.ButtonLB: 1}}})
	a.Step()
	if a.Pressed("sprint") || a.JustPressed("sprint") || !a.JustReleased("sprint") {
		t.Fatalf("press 2 (off): pressed=%v justPressed=%v justReleased=%v, want false/false/true",
			a.Pressed("sprint"), a.JustPressed("sprint"), a.JustReleased("sprint"))
	}
	if a.ToggleActive("sprint") {
		t.Fatal("expected ToggleActive(sprint) == false after second press")
	}
	a.OnGamepads([]gamepad.Pad{{ID: 1}})
	a.Step()
	if a.Pressed("sprint") || a.JustReleased("sprint") {
		t.Fatalf("idle after toggle off: pressed=%v justReleased=%v", a.Pressed("sprint"), a.JustReleased("sprint"))
	}

	// Repeated rapid taps between steps alternate ON -> OFF -> ON.
	a.OnKeyDown(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	a.OnKeyUp(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	a.Step()
	if !a.Pressed("sprint") || !a.JustPressed("sprint") {
		t.Fatal("rapid tap 1 did not toggle sprint on")
	}
	a.OnKeyDown(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	a.OnKeyUp(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	a.Step()
	if a.Pressed("sprint") || a.JustPressed("sprint") || !a.JustReleased("sprint") {
		t.Fatalf("rapid tap 2 did not toggle sprint off cleanly: pressed=%v jp=%v jr=%v",
			a.Pressed("sprint"), a.JustPressed("sprint"), a.JustReleased("sprint"))
	}

	// Undeclared menu action "pause" must NEVER latch even though listed in Assists.Toggle.
	a.OnKeyDown(core.KeyEvent{Key: "escape", Code: "Escape"})
	a.Step()
	if !a.Pressed("pause") || !a.JustPressed("pause") {
		t.Fatal("pause should still fire normally when pressed")
	}
	a.OnKeyUp(core.KeyEvent{Key: "escape", Code: "Escape"})
	a.Step()
	if a.Pressed("pause") || a.ToggleActive("pause") {
		t.Fatal("menu action pause must never stay latched by Toggle")
	}
}

func TestAssistToggleReleaseOnPauseAndReset(t *testing.T) {
	a := input.NewActions()
	a.Bind("sprint", input.Key("shift"))
	a.Bind("right", input.Key("d"))
	a.BindAxis("move_x", "", "right")
	a.SetActionAssist("sprint", input.ActionAssist{Toggle: true})
	a.SetActionAssist("right", input.ActionAssist{Toggle: true})
	a.SetAssists(input.Assists{Toggle: []string{"sprint", "right"}})

	// Latch both sprint and right on.
	a.OnKeyDown(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	a.OnKeyDown(core.KeyEvent{Key: "d", Code: "KeyD"})
	a.Step()
	a.OnKeyUp(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	a.OnKeyUp(core.KeyEvent{Key: "d", Code: "KeyD"})
	a.Step()
	if !a.Pressed("sprint") || a.Axis("move_x") != 1 {
		t.Fatalf("expected latched sprint and move_x=1, got sprint=%v axis=%v", a.Pressed("sprint"), a.Axis("move_x"))
	}

	// Pausing releases all latched toggles so unpausing emits JustReleased and stops moving.
	a.SetPaused(true)
	if a.ToggleActive("sprint") || a.ToggleActive("right") {
		t.Fatal("SetPaused(true) did not clear ToggleActive")
	}
	a.SetPaused(false)
	a.Step()
	if a.Pressed("sprint") || !a.JustReleased("sprint") {
		t.Fatalf("after unpause: sprint pressed=%v justReleased=%v, want false/true", a.Pressed("sprint"), a.JustReleased("sprint"))
	}
	if a.Axis("move_x") != 0 {
		t.Fatalf("Axis(move_x) after pause = %v, want 0", a.Axis("move_x"))
	}

	// Re-latch and verify focus loss / scene change Reset clears latched state immediately.
	a.OnKeyDown(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	a.Step()
	a.OnKeyUp(core.KeyEvent{Key: "shift", Code: "ShiftLeft"})
	a.Step()
	if !a.Pressed("sprint") {
		t.Fatal("expected sprint latched before Reset")
	}
	a.Reset()
	if a.Pressed("sprint") || a.ToggleActive("sprint") {
		t.Fatal("Reset did not clear latched sprint")
	}
	a.Step()
	if a.Pressed("sprint") || a.JustPressed("sprint") || a.JustReleased("sprint") {
		t.Fatalf("after Reset + Step: pressed=%v jp=%v jr=%v, want all false",
			a.Pressed("sprint"), a.JustPressed("sprint"), a.JustReleased("sprint"))
	}
}

func TestAssistMashToHoldAndPressOnce(t *testing.T) {
	t.Run("hold emits repeated presses at declared rate", func(t *testing.T) {
		a := input.NewActions()
		a.SetStepRate(60)
		a.Bind("struggle", input.Key("e"))
		// 10 Hz at 60 Hz => pulse every 6 steps: steps 0, 6, 12, 18, 24, 30, 36, 42, 48, 54 (10 pulses in 60 steps).
		a.SetActionAssist("struggle", input.ActionAssist{Mash: true, MashRate: 10})
		a.SetAssists(input.Assists{MashToHold: []string{"struggle"}})

		a.OnKeyDown(core.KeyEvent{Key: "e", Code: "KeyE"})
		var pulseSteps []int
		for step := range 60 {
			a.Step()
			if a.JustPressed("struggle") {
				if !a.Pressed("struggle") {
					t.Fatalf("step %d: JustPressed without Pressed", step)
				}
				pulseSteps = append(pulseSteps, step)
			}
		}
		want := []int{0, 6, 12, 18, 24, 30, 36, 42, 48, 54}
		if !slices.Equal(pulseSteps, want) {
			t.Fatalf("10 Hz pulse steps = %v, want %v", pulseSteps, want)
		}

		// Releasing stops mashing immediately.
		a.OnKeyUp(core.KeyEvent{Key: "e", Code: "KeyE"})
		for range 12 {
			a.Step()
			if a.Pressed("struggle") || a.JustPressed("struggle") {
				t.Fatal("mash continued after key release")
			}
		}
	})

	t.Run("press-once combines Toggle and MashToHold", func(t *testing.T) {
		a := input.NewActions()
		a.SetStepRate(60)
		a.Bind("struggle", input.Key("e"))
		a.SetActionAssist("struggle", input.ActionAssist{Toggle: true, Mash: true, MashRate: 15}) // every 4 steps
		a.SetAssists(input.Assists{
			Toggle:     []string{"struggle"},
			MashToHold: []string{"struggle"},
		})

		// Single tap (press and release before step 0) starts continuous mashing.
		a.OnKeyDown(core.KeyEvent{Key: "e", Code: "KeyE"})
		a.OnKeyUp(core.KeyEvent{Key: "e", Code: "KeyE"})
		var pulses []int
		for step := range 16 {
			a.Step()
			if a.JustPressed("struggle") {
				pulses = append(pulses, step)
			}
		}
		want := []int{0, 4, 8, 12}
		if !slices.Equal(pulses, want) {
			t.Fatalf("press-once mash pulses = %v, want %v", pulses, want)
		}

		// Second tap stops continuous mashing.
		a.OnKeyDown(core.KeyEvent{Key: "e", Code: "KeyE"})
		a.OnKeyUp(core.KeyEvent{Key: "e", Code: "KeyE"})
		for range 10 {
			a.Step()
			if a.Pressed("struggle") || a.JustPressed("struggle") {
				t.Fatal("press-once mash did not stop on second tap")
			}
		}
	})

	t.Run("high mash rate caps cleanly to alternate steps", func(t *testing.T) {
		a := input.NewActions()
		a.SetStepRate(60)
		a.Bind("rapid", input.Key("f"))
		a.SetActionAssist("rapid", input.ActionAssist{Mash: true, MashRate: 120})
		a.SetAssists(input.Assists{MashToHold: []string{"rapid"}})

		a.OnKeyDown(core.KeyEvent{Key: "f", Code: "KeyF"})
		pulses := 0
		for step := range 10 {
			a.Step()
			wantOn := step%2 == 0
			if a.Pressed("rapid") != wantOn || a.JustPressed("rapid") != wantOn {
				t.Fatalf("step %d: pressed=%v jp=%v, want %v", step, a.Pressed("rapid"), a.JustPressed("rapid"), wantOn)
			}
			if a.JustPressed("rapid") {
				pulses++
			}
		}
		if pulses != 5 {
			t.Fatalf("pulses = %d, want 5", pulses)
		}
	})
}

func TestAssistMinPressSlowKeys(t *testing.T) {
	a := input.NewActions()
	a.SetStepRate(60)
	a.Bind("jump", input.Key("space"))
	// 100ms at 60 Hz = exact 6 steps.
	a.SetAssists(input.Assists{MinPress: 100 * time.Millisecond})

	// Sub-step accidental tap is rejected.
	a.OnKeyDown(core.KeyEvent{Key: "space", Code: "Space"})
	a.OnKeyUp(core.KeyEvent{Key: "space", Code: "Space"})
	a.Step()
	if a.Pressed("jump") || a.JustPressed("jump") {
		t.Fatal("sub-step tap was accepted despite MinPress=100ms")
	}

	// Short hold (5 steps < 6 steps) is rejected.
	a.OnKeyDown(core.KeyEvent{Key: "space", Code: "Space"})
	for step := range 5 {
		a.Step()
		if a.Pressed("jump") || a.JustPressed("jump") {
			t.Fatalf("step %d: short hold accepted early", step)
		}
	}
	a.OnKeyUp(core.KeyEvent{Key: "space", Code: "Space"})
	a.Step()
	if a.Pressed("jump") || a.JustPressed("jump") {
		t.Fatal("released short hold fired")
	}

	// Full hold reaches 6 steps and fires on the 6th step (index 5).
	a.OnKeyDown(core.KeyEvent{Key: "space", Code: "Space"})
	for step := range 5 {
		a.Step()
		if a.Pressed("jump") || a.JustPressed("jump") {
			t.Fatalf("step %d: hold accepted before 6th step", step)
		}
	}
	a.Step() // 6th step
	if !a.Pressed("jump") || !a.JustPressed("jump") {
		t.Fatalf("6th step: pressed=%v justPressed=%v, want true/true", a.Pressed("jump"), a.JustPressed("jump"))
	}
	a.Step() // 7th step still held
	if !a.Pressed("jump") || a.JustPressed("jump") {
		t.Fatalf("7th step: pressed=%v justPressed=%v, want true/false", a.Pressed("jump"), a.JustPressed("jump"))
	}

	// Releasing and re-pressing between steps restarts MinPress hold timer.
	a.OnKeyUp(core.KeyEvent{Key: "space", Code: "Space"})
	a.OnKeyDown(core.KeyEvent{Key: "space", Code: "Space"})
	a.Step()
	if a.Pressed("jump") || !a.JustReleased("jump") {
		t.Fatalf("after mid-step release+repress: pressed=%v justReleased=%v, want false/true",
			a.Pressed("jump"), a.JustReleased("jump"))
	}
	// 5 more steps (total 6 since repress) accepts again.
	for range 4 {
		a.Step()
		if a.Pressed("jump") {
			t.Fatal("restarted hold accepted too early")
		}
	}
	a.Step()
	if !a.Pressed("jump") || !a.JustPressed("jump") {
		t.Fatal("restarted hold did not accept on 6th step")
	}
}

func TestAssistAcceptDelayBounceKeys(t *testing.T) {
	a := input.NewActions()
	a.SetStepRate(60)
	a.Bind("fire", input.Key("z"))
	// 500ms at 60 Hz = exact 30 steps.
	a.SetAssists(input.Assists{AcceptDelay: 500 * time.Millisecond})

	// Step 0: initial press accepted and held for 5 steps without interruption.
	a.OnKeyDown(core.KeyEvent{Key: "z", Code: "KeyZ"})
	a.Step()
	if !a.Pressed("fire") || !a.JustPressed("fire") {
		t.Fatal("step 0: initial press not accepted")
	}
	for step := 1; step <= 4; step++ {
		a.Step()
		if !a.Pressed("fire") || a.JustPressed("fire") {
			t.Fatalf("step %d: ongoing accepted hold interrupted", step)
		}
	}
	a.OnKeyUp(core.KeyEvent{Key: "z", Code: "KeyZ"})
	a.Step() // step 5: release
	if a.Pressed("fire") || !a.JustReleased("fire") {
		t.Fatal("step 5: expected release")
	}

	// Step 10: accidental tremor bounce is ignored (within 30-step delay).
	for step := 6; step <= 9; step++ {
		a.Step()
	}
	a.OnKeyDown(core.KeyEvent{Key: "z", Code: "KeyZ"})
	a.Step() // step 10
	if a.Pressed("fire") || a.JustPressed("fire") {
		t.Fatal("step 10: bounce press within AcceptDelay was not ignored")
	}
	a.OnKeyUp(core.KeyEvent{Key: "z", Code: "KeyZ"})

	// Step 29 (still inside 30-step delay): holding a rejected press must NOT fire
	// on step 29 nor spontaneously activate on step 30 without a fresh press.
	for step := 11; step <= 28; step++ {
		a.Step()
	}
	a.OnKeyDown(core.KeyEvent{Key: "z", Code: "KeyZ"})
	a.Step() // step 29
	if a.Pressed("fire") || a.JustPressed("fire") {
		t.Fatal("step 29: bounce press accepted 1 step too early")
	}
	a.Step() // step 30: still holding the rejected press
	if a.Pressed("fire") || a.JustPressed("fire") {
		t.Fatal("step 30: previously rejected hold spontaneously fired when cooldown expired")
	}

	// Fresh press at step 31 (after cooldown expired at step 30) IS accepted.
	a.OnKeyUp(core.KeyEvent{Key: "z", Code: "KeyZ"})
	a.Step() // release
	a.OnKeyDown(core.KeyEvent{Key: "z", Code: "KeyZ"})
	a.Step()
	if !a.Pressed("fire") || !a.JustPressed("fire") {
		t.Fatal("fresh press after AcceptDelay expired was not accepted")
	}
}

func TestAssistAcceptDelayProtectsToggleFromDoubleTap(t *testing.T) {
	a := input.NewActions()
	a.SetStepRate(60)
	a.Bind("aim", input.MouseButton(core.MouseButtonRight))
	a.SetActionAssist("aim", input.ActionAssist{Toggle: true})
	a.SetAssists(input.Assists{
		Toggle:      []string{"aim"},
		AcceptDelay: 500 * time.Millisecond, // 30 steps
	})

	// Step 0: right click toggles aim ON.
	a.OnContextMenu(core.ContextMenuEvent{Position: core.Point{X: 10, Y: 10}})
	a.Step()
	if !a.Pressed("aim") || !a.JustPressed("aim") {
		t.Fatal("step 0: aim did not toggle on")
	}

	// Step 5: accidental tremor right-click is ignored, keeping aim ON.
	for range 4 {
		a.Step()
	}
	a.OnContextMenu(core.ContextMenuEvent{Position: core.Point{X: 10, Y: 10}})
	a.Step() // step 5
	if !a.Pressed("aim") || a.JustReleased("aim") {
		t.Fatal("step 5: tremor bounce cancelled aim toggle")
	}

	// Step 30: intentional right-click toggles aim OFF.
	for range 24 {
		a.Step()
	}
	a.OnContextMenu(core.ContextMenuEvent{Position: core.Point{X: 10, Y: 10}})
	a.Step() // step 30
	if a.Pressed("aim") || !a.JustReleased("aim") {
		t.Fatalf("step 30: expected toggle off, got pressed=%v jr=%v", a.Pressed("aim"), a.JustReleased("aim"))
	}
}

func TestAssistPerPlayerAndStorageRoundTrip(t *testing.T) {
	players := input.NewPlayers(2)
	players.SetStepRate(60)
	for _, p := range players.All() {
		p.Actions().Bind("sprint", input.GamepadButton(gamepad.ButtonLB))
		p.SetActionAssist("sprint", input.ActionAssist{Toggle: true})
	}
	players.AssignGamepad(0, 1)
	players.AssignGamepad(1, 2)

	// Player 0 enables Hold-to-Toggle; Player 1 keeps standard hold.
	players.Player(0).SetAssists(input.Assists{
		Toggle:      []string{"sprint"},
		MinPress:    50 * time.Millisecond, // 3 steps
		AcceptDelay: 200 * time.Millisecond,
	})

	padsPressed := []gamepad.Pad{
		{ID: 1, Buttons: [gamepad.ButtonCount]float32{gamepad.ButtonLB: 1}},
		{ID: 2, Buttons: [gamepad.ButtonCount]float32{gamepad.ButtonLB: 1}},
	}
	players.OnGamepads(padsPressed)
	players.Step() // step 1: P1 fires immediately, P0 waits for MinPress (3 steps)
	if players.Player(0).Pressed("sprint") {
		t.Fatal("P0 fired before MinPress")
	}
	if !players.Player(1).Pressed("sprint") {
		t.Fatal("P1 should fire immediately with no assists")
	}
	players.Step() // step 2
	players.Step() // step 3: P0 latches ON
	if !players.Player(0).Pressed("sprint") || !players.Player(0).ToggleActive("sprint") {
		t.Fatal("P0 did not latch sprint on step 3")
	}

	// Release both pads: P0 stays toggled on, P1 releases.
	players.OnGamepads([]gamepad.Pad{{ID: 1}, {ID: 2}})
	players.Step()
	if !players.Player(0).Pressed("sprint") {
		t.Fatal("P0 toggle did not stay active after release")
	}
	if players.Player(1).Pressed("sprint") {
		t.Fatal("P1 should have released")
	}

	// Players.SetPaused(true) releases all toggles across players.
	players.SetPaused(true)
	players.SetPaused(false)
	players.Step()
	if players.Player(0).Pressed("sprint") || !players.Player(0).JustReleased("sprint") {
		t.Fatal("Players.SetPaused(true) did not release P0 toggle")
	}

	// Actions.Save / Load persists Assists cleanly.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return core.View{} })
	store := app.Storage()
	if err := players.Player(0).Actions().Save(store, "p0.controls"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded := input.NewActions()
	if ok, err := reloaded.Load(store, "p0.controls"); !ok || err != nil {
		t.Fatalf("Load = (%v, %v), want (true, nil)", ok, err)
	}
	got := reloaded.Assists()
	if !slices.Equal(got.Toggle, []string{"sprint"}) || got.MinPress != 50*time.Millisecond || got.AcceptDelay != 200*time.Millisecond {
		t.Fatalf("reloaded Assists = %+v, want Toggle=[sprint] MinPress=50ms AcceptDelay=200ms", got)
	}
}
