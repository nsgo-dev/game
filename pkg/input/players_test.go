package input_test

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type mpHarness struct {
	app     *nsgotest.App
	players *input.Players
	shown   *core.Signal[bool]
	steps   int
	last    []padStep
}

func startMPHarness(t *testing.T, count int, setup func(cx *core.Scope, m *input.Players, v *core.View), opts ...nsgotest.Option) *mpHarness {
	t.Helper()
	h := &mpHarness{
		players: input.NewPlayers(count),
		last:    make([]padStep, count),
	}
	h.app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		h.shown = core.NewSignal(cx, true)
		v := core.View{
			TestID: "game",
			Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
		}
		if setup != nil {
			setup(cx, h.players, &v)
		}
		h.players.Attach(&v)
		loop := &game.Loop{
			Rate: 60,
			Update: func(time.Duration) {
				h.players.Step()
				h.steps++
				if len(h.last) != h.players.Len() {
					h.last = make([]padStep, h.players.Len())
				}
				for i, p := range h.players.All() {
					h.last[i] = padStep{
						jump:     p.Pressed("jump"),
						jumpJust: p.JustPressed("jump"),
						jumpUp:   p.JustReleased("jump"),
						fire:     p.Pressed("fire"),
						move:     p.Axis("move"),
						look:     p.Axis("look"),
					}
				}
			},
		}
		loop.Run(cx)
		return core.View{
			Children: []core.Node{
				v,
				core.Show{
					When: h.shown.Get,
					Then: func(child *core.Scope) core.Node {
						h.players.WatchGamepads(child)
						return core.View{}
					},
				},
			},
		}
	}, opts...)
	return h
}

func (h *mpHarness) step() []padStep {
	before := h.steps
	for h.steps == before {
		h.app.Advance(frameStep)
	}
	return slices.Clone(h.last)
}

func TestPlayersTwoGamepadsDriveActionsSeparately(t *testing.T) {
	h := startMPHarness(t, 2, func(_ *core.Scope, m *input.Players, _ *core.View) {
		for _, p := range m.All() {
			bindPadDefaults(p.Actions())
		}
		m.AssignGamepad(0, 1)
		m.AssignGamepad(1, 2)
	}, nsgotest.Environment(core.Environment{Gamepads: true}))

	// Before either controller connects, assignments are remembered but not connected.
	if id, ok := h.players.Player(0).GamepadID(); !ok || id != 1 {
		t.Fatalf("P0 GamepadID = (%d, %v), want (1, true)", id, ok)
	}
	if h.players.Player(0).GamepadConnected() || h.players.Player(1).GamepadConnected() {
		t.Fatal("players report connected before any gamepad connects")
	}

	h.app.ConnectGamepad(1, "Xbox Wireless Controller")
	h.app.ConnectGamepad(2, "DualSense Wireless Controller")
	got := h.step()
	if got[0] != (padStep{}) || got[1] != (padStep{}) {
		t.Fatalf("idle controllers: %+v", got)
	}
	if !h.players.Player(0).GamepadConnected() || !h.players.Player(1).GamepadConnected() {
		t.Fatal("both players should report GamepadConnected after connect")
	}
	if gp, ok := h.players.Player(0).Gamepad(); !ok || gp.ID != 1 || gp.Name != "Xbox Wireless Controller" {
		t.Fatalf("P0 Gamepad() = (%+v, %v)", gp, ok)
	}
	if gp, ok := h.players.Player(1).Gamepad(); !ok || gp.ID != 2 || gp.Name != "DualSense Wireless Controller" {
		t.Fatalf("P1 Gamepad() = (%+v, %v)", gp, ok)
	}
	if h.players.PlayerByGamepad(1) != h.players.Player(0) || h.players.PlayerByGamepad(2) != h.players.Player(1) {
		t.Fatal("PlayerByGamepad returned wrong owners")
	}

	// Pad 1 drives only player 0.
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	h.app.SetGamepadAxis(1, gamepad.AxisLeftX, -0.575)
	got = h.step()
	if !got[0].jump || !got[0].jumpJust || got[0].jumpUp {
		t.Fatalf("P0 press: %+v", got[0])
	}
	expectAxis(t, "P0 move left", got[0].move, -0.5)
	if got[1] != (padStep{}) {
		t.Fatalf("P1 moved while only pad 1 acted: %+v", got[1])
	}

	// Pad 2 drives player 1 independently while pad 1 holds jump and moves left.
	h.app.SetGamepadButton(2, gamepad.ButtonSouth, 1)
	h.app.SetGamepadButton(2, gamepad.ButtonRT, 0.8)
	h.app.SetGamepadAxis(2, gamepad.AxisLeftX, 1)
	got = h.step()
	if !got[0].jump || got[0].jumpJust || got[0].fire {
		t.Fatalf("P0 hold while P1 presses: %+v", got[0])
	}
	expectAxis(t, "P0 move still left", got[0].move, -0.5)
	if !got[1].jump || !got[1].jumpJust || !got[1].fire {
		t.Fatalf("P1 press: %+v", got[1])
	}
	expectAxis(t, "P1 move right", got[1].move, 1)

	// Releasing pad 1 affects only player 0.
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 0)
	h.app.SetGamepadAxis(1, gamepad.AxisLeftX, 0)
	got = h.step()
	if got[0].jump || !got[0].jumpUp || got[0].move != 0 {
		t.Fatalf("P0 release: %+v", got[0])
	}
	if !got[1].jump || got[1].jumpJust || !got[1].fire || got[1].move != 1 {
		t.Fatalf("P1 disturbed by P0 release: %+v", got[1])
	}

	// Disconnecting pad 2 releases only player 1 while preserving its pad assignment; reconnecting restores it.
	h.app.DisconnectGamepad(2)
	got = h.step()
	if got[1].jump || !got[1].jumpUp || got[1].fire || got[1].move != 0 {
		t.Fatalf("P1 after disconnect: %+v", got[1])
	}
	if h.players.Player(1).GamepadConnected() {
		t.Fatal("P1 still reports connected after disconnect")
	}
	if id, ok := h.players.Player(1).GamepadID(); !ok || id != 2 {
		t.Fatalf("P1 GamepadID forgotten on disconnect: (%d, %v)", id, ok)
	}
	if !h.players.Player(0).GamepadConnected() {
		t.Fatal("P0 disconnected when pad 2 disconnected")
	}

	h.app.ConnectGamepad(2, "DualSense Wireless Controller")
	h.app.SetGamepadButton(2, gamepad.ButtonSouth, 1)
	got = h.step()
	if !got[1].jump || !got[1].jumpJust || !h.players.Player(1).GamepadConnected() {
		t.Fatalf("P1 after reconnect: %+v, connected=%v", got[1], h.players.Player(1).GamepadConnected())
	}
	if got[0].jump {
		t.Fatalf("P0 affected by P1 reconnect: %+v", got[0])
	}
}

func TestPlayersSplitKeyboardSeparateKeySets(t *testing.T) {
	for _, path := range []string{"watched", "focused"} {
		t.Run(path, func(t *testing.T) {
			var stopKeys func()
			var viewPresses int
			h := startMPHarness(t, 2, func(cx *core.Scope, m *input.Players, v *core.View) {
				// Player 0: WASD + Space
				p0 := m.Player(0).Actions()
				p0.Bind("left", input.KeyCode("KeyA"))
				p0.Bind("right", input.KeyCode("KeyD"))
				p0.Bind("jump", input.Key("space"))
				p0.BindAxis("move", "left", "right")

				// Player 1: Arrow keys + Enter
				p1 := m.Player(1).Actions()
				p1.Bind("left", input.Key("left"))
				p1.Bind("right", input.Key("right"))
				p1.Bind("jump", input.Key("enter"))
				p1.BindAxis("move", "left", "right")

				m.AssignKeyboard(0)
				m.AssignKeyboard(1)
				if path == "watched" {
					stopKeys = m.WatchKeys(cx)
				} else {
					v.Focusable = func() bool { return true }
					v.OnPress = func(core.PressEvent) { viewPresses++ }
				}
			}, desktop)
			if path == "focused" {
				h.app.Focus("id:game")
			}

			got := h.step()
			if got[0] != (padStep{}) || got[1] != (padStep{}) {
				t.Fatalf("idle: %+v", got)
			}

			// Both players move in opposite directions and jump at the same time on one keyboard.
			h.app.KeyCodeDown("KeyA")
			h.app.KeyDown("right")
			h.app.KeyDown("space")
			got = h.step()
			if got[0].move != -1 || !got[0].jump || !got[0].jumpJust {
				t.Fatalf("P0 split keyboard: %+v", got[0])
			}
			if got[1].move != 1 || got[1].jump {
				t.Fatalf("P1 split keyboard: %+v", got[1])
			}

			// Player 1 jumps while player 0 releases space.
			h.app.KeyUp("space")
			h.app.KeyDown("enter")
			got = h.step()
			if got[0].jump || !got[0].jumpUp || got[0].move != -1 {
				t.Fatalf("P0 after space up: %+v", got[0])
			}
			if !got[1].jump || !got[1].jumpJust || got[1].move != 1 {
				t.Fatalf("P1 enter down: %+v", got[1])
			}

			if path == "focused" && viewPresses != 0 {
				t.Fatalf("bound keys pressed focused view %d times, want 0", viewPresses)
			}

			// Releasing keys or stopping WatchKeys releases held inputs cleanly.
			if path == "watched" {
				h.app.Post(stopKeys)
				h.app.Settle()
				got = h.step()
				if got[0].move != 0 || got[1].move != 0 || got[1].jump {
					t.Fatalf("keys remained held after WatchKeys stop: %+v", got)
				}
			} else {
				h.app.KeyCodeUp("KeyA")
				h.app.KeyUp("right")
				h.app.KeyUp("enter")
				got = h.step()
				if got[0].move != 0 || got[1].move != 0 || got[1].jump || !got[1].jumpUp {
					t.Fatalf("after releasing keys: %+v", got)
				}
			}
		})
	}
}

func TestPlayersMixedKeyboardAndGamepadIsolation(t *testing.T) {
	bindShared := func(a *input.Actions) {
		a.Bind("jump", input.Key("space"), input.GamepadButton(gamepad.ButtonSouth))
		a.Bind("left", input.Key("left"), input.GamepadButton(gamepad.ButtonDPadLeft))
		a.Bind("right", input.Key("right"), input.GamepadButton(gamepad.ButtonDPadRight))
		a.BindAxis("move", "left", "right")
		a.BindGamepadAxis("move", gamepad.AxisLeftX, input.GamepadAxisOptions{})
	}

	h := startMPHarness(t, 2, func(cx *core.Scope, m *input.Players, _ *core.View) {
		// Player 0: keyboard only. Player 1: gamepad 1 only. Player 2: hybrid keyboard + gamepad 2.
		p2 := m.Add()
		bindShared(m.Player(0).Actions())
		bindShared(m.Player(1).Actions())
		bindShared(p2.Actions())

		m.AssignKeyboard(0)
		m.AssignGamepad(1, 1)
		m.AssignKeyboard(2)
		m.AssignGamepad(2, 2)
		m.WatchKeys(cx)
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, Gamepads: true}))

	h.app.ConnectGamepad(1, "Xbox Wireless Controller")
	h.app.ConnectGamepad(2, "Pro Controller")
	h.step()

	// Keyboard drives P0 and P2 (hybrid), never P1 (gamepad-only), even though P1 also has Key("space") bound.
	h.app.KeyDown("space")
	h.app.KeyDown("left")
	got := h.step()
	if !got[0].jump || got[0].move != -1 {
		t.Fatalf("P0 keyboard: %+v", got[0])
	}
	if got[1].jump || got[1].move != 0 {
		t.Fatalf("P1 (gamepad-only) reacted to keyboard: %+v", got[1])
	}
	if !got[2].jump || got[2].move != -1 {
		t.Fatalf("P2 (hybrid) missed keyboard: %+v", got[2])
	}

	// Gamepad 1 drives ONLY P1, never P0 (keyboard-only) or P2 (pad 2).
	h.app.KeyUp("space")
	h.app.KeyUp("left")
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	h.app.SetGamepadAxis(1, gamepad.AxisLeftX, 1)
	got = h.step()
	if got[0].jump || got[0].move != 0 {
		t.Fatalf("P0 (keyboard-only) reacted to gamepad 1: %+v", got[0])
	}
	if !got[1].jump || got[1].move != 1 {
		t.Fatalf("P1 gamepad 1: %+v", got[1])
	}
	if got[2].jump || got[2].move != 0 {
		t.Fatalf("P2 (pad 2) reacted to gamepad 1: %+v", got[2])
	}

	// Gamepad 2 drives ONLY P2.
	h.app.SetGamepadButton(2, gamepad.ButtonSouth, 1)
	got = h.step()
	if got[0].jump || !got[1].jump || !got[2].jump {
		t.Fatalf("pad 2 isolation: %+v", got)
	}
}

func TestPlayersGamepadExclusivityReassignUnassignAndAutoAssign(t *testing.T) {
	m := input.NewPlayers(3)
	for _, p := range m.All() {
		bindPadDefaults(p.Actions())
		p.Actions().Bind("kjump", input.Key("space"))
	}

	pads := []gamepad.Pad{
		pad(20, map[gamepad.Button]float32{gamepad.ButtonSouth: 1}, [4]float32{1, 0}),
		pad(10, nil, [4]float32{-1, 0}),
		pad(30, map[gamepad.Button]float32{gamepad.ButtonRT: 1}, [4]float32{}),
	}
	m.OnGamepads(pads)

	// Gamepads and UnassignedGamepads are sorted by ascending ID.
	if got := m.Gamepads(); len(got) != 3 || got[0].ID != 10 || got[1].ID != 20 || got[2].ID != 30 {
		t.Fatalf("Gamepads() IDs = %+v", got)
	}
	if got := m.UnassignedGamepads(); len(got) != 3 {
		t.Fatalf("UnassignedGamepads() len = %d, want 3", len(got))
	}

	// Slot 0 takes keyboard; AssignUnassignedGamepads skips slot 0 and assigns pads 10 and 20 to slots 1 and 2.
	m.AssignKeyboard(0)
	if n := m.AssignUnassignedGamepads(); n != 2 {
		t.Fatalf("AssignUnassignedGamepads() = %d, want 2", n)
	}
	if id, ok := m.Player(1).GamepadID(); !ok || id != 10 {
		t.Fatalf("P1 GamepadID = (%d, %v), want (10, true)", id, ok)
	}
	if id, ok := m.Player(2).GamepadID(); !ok || id != 20 {
		t.Fatalf("P2 GamepadID = (%d, %v), want (20, true)", id, ok)
	}
	if m.NextUnassigned() != nil {
		t.Fatalf("NextUnassigned() = %v, want nil", m.NextUnassigned())
	}
	if unassigned := m.UnassignedGamepads(); len(unassigned) != 1 || unassigned[0].ID != 30 {
		t.Fatalf("UnassignedGamepads() = %+v, want [30]", unassigned)
	}

	m.Step()
	if !m.Player(2).Pressed("jump") || m.Player(2).Axis("move") != 1 {
		t.Fatalf("P2 did not receive pad 20: jump=%v move=%v", m.Player(2).Pressed("jump"), m.Player(2).Axis("move"))
	}
	if m.Player(1).Axis("move") != -1 {
		t.Fatalf("P1 move = %v, want -1", m.Player(1).Axis("move"))
	}

	// Reassigning pad 20 from P2 to P0 is exclusive: P2 loses pad 20 and releases jump on the next step.
	if !m.AssignGamepad(0, 20) {
		t.Fatal("AssignGamepad(0, 20) returned false")
	}
	if _, ok := m.Player(2).GamepadID(); ok {
		t.Fatal("P2 still owns pad 20 after reassignment to P0")
	}
	if m.PlayerByGamepad(20) != m.Player(0) {
		t.Fatal("PlayerByGamepad(20) != P0")
	}
	m.Step()
	if m.Player(2).Pressed("jump") || !m.Player(2).JustReleased("jump") || m.Player(2).Axis("move") != 0 {
		t.Fatalf("P2 did not release pad 20 cleanly")
	}
	if !m.Player(0).Pressed("jump") || !m.Player(0).JustPressed("jump") {
		t.Fatalf("P0 did not acquire pad 20 cleanly")
	}

	// Idempotent AssignGamepad preserves held state without firing JustPressed again.
	if !m.AssignGamepad(0, 20) {
		t.Fatal("idempotent AssignGamepad returned false")
	}
	m.Step()
	if !m.Player(0).Pressed("jump") || m.Player(0).JustPressed("jump") {
		t.Fatal("idempotent AssignGamepad disrupted held state")
	}

	// Drop-in join pattern: unassigned pad 30 presses RT -> claims open slot P2.
	for _, gp := range m.UnassignedGamepads() {
		if gp.Pressed(gamepad.ButtonRT) {
			if slot := m.NextUnassigned(); slot != nil {
				m.AssignGamepad(slot.Index(), gp.ID)
			}
		}
	}
	m.Step()
	if id, ok := m.Player(2).GamepadID(); !ok || id != 30 || !m.Player(2).Pressed("fire") {
		t.Fatalf("drop-in join failed: id=(%d,%v) fire=%v", id, ok, m.Player(2).Pressed("fire"))
	}

	// UnassignKeyboard releases held keys on P0 without touching its gamepad.
	m.OnKeyDown(core.KeyEvent{Key: "space", Code: "Space"})
	m.Step()
	if !m.Player(0).Pressed("kjump") || !m.Player(0).Pressed("jump") {
		t.Fatal("P0 should have both keyboard and gamepad active")
	}
	if !m.UnassignKeyboard(0) || m.UnassignKeyboard(0) {
		t.Fatal("UnassignKeyboard return values unexpected")
	}
	m.Step()
	if m.Player(0).Pressed("kjump") || !m.Player(0).JustReleased("kjump") {
		t.Fatal("UnassignKeyboard did not release held key")
	}
	if !m.Player(0).Pressed("jump") {
		t.Fatal("UnassignKeyboard affected gamepad input")
	}

	// Unassign clears everything on P0.
	if !m.Unassign(0) || m.Player(0).HasDevice() {
		t.Fatal("Unassign(0) did not clear all devices")
	}
	m.Step()
	if m.Player(0).Pressed("jump") || !m.Player(0).JustReleased("jump") {
		t.Fatal("Unassign(0) did not release held gamepad button")
	}
}

func TestPlayersPointerRoutingAndSplitTouch(t *testing.T) {
	m := input.NewPlayers(3)
	m.Player(0).Actions().Bind("act", input.TouchRegion(core.Bounds{Size: core.Size{Width: 200, Height: 300}}))
	m.Player(0).Actions().Bind("click", input.MouseButton(core.MouseButtonLeft))
	m.Player(0).Actions().BindPointerAxis("yaw", input.PointerX, input.PointerAxisOptions{Sensitivity: 0.5})

	m.Player(1).Actions().Bind("act", input.TouchRegion(core.Bounds{Origin: core.Point{X: 200}, Size: core.Size{Width: 200, Height: 300}}))
	m.Player(2).Actions().Bind("act", input.TouchRegion(core.Bounds{Size: core.Size{Width: 400, Height: 300}}))

	m.AssignPointer(0)
	m.AssignPointer(1)
	// Player 2 is NOT assigned pointer.

	var chainedTouch, chainedKey int
	v := core.View{
		OnTouch:   func(core.TouchEvent) { chainedTouch++ },
		OnKeyDown: func(core.KeyEvent) bool { chainedKey++; return false },
	}
	m.Attach(&v)

	v.OnTouch(touchAt(core.TouchStart, 1, 50, 100))
	v.OnTouch(touchAt(core.TouchStart, 2, 300, 100))
	v.OnPointerDelta(10, 0)
	v.OnKeyDown(core.KeyEvent{Key: "a"})
	m.Step()

	if !m.Player(0).Pressed("act") || !m.Player(1).Pressed("act") {
		t.Fatalf("split touch failed: P0=%v P1=%v", m.Player(0).Pressed("act"), m.Player(1).Pressed("act"))
	}
	if m.Player(2).Pressed("act") {
		t.Fatal("unassigned P2 received touch")
	}
	if got := m.Player(0).Delta("yaw"); got != 5 {
		t.Fatalf("P0 Delta(yaw) = %v, want 5", got)
	}
	if chainedTouch != 2 || chainedKey != 1 {
		t.Fatalf("Attach did not chain callbacks: touch=%d key=%d", chainedTouch, chainedKey)
	}

	// UnassignPointer releases active touches and clears pending pointer deltas.
	v.OnPointerDelta(20, 0)
	if !m.UnassignPointer(0) || m.UnassignPointer(0) {
		t.Fatal("UnassignPointer return values unexpected")
	}
	m.Step()
	if m.Player(0).Pressed("act") || !m.Player(0).JustReleased("act") || m.Player(0).Delta("yaw") != 0 {
		t.Fatalf("UnassignPointer did not clear P0 pointer state: act=%v up=%v yaw=%v", m.Player(0).Pressed("act"), m.Player(0).JustReleased("act"), m.Player(0).Delta("yaw"))
	}
	if !m.Player(1).Pressed("act") {
		t.Fatal("UnassignPointer(0) disturbed P1 touch")
	}
}

func TestPlayersResetWatchDisposalAccessDeniedAndEdgeCases(t *testing.T) {
	// Zero value and out-of-range / nil *Player safety.
	var zero input.Players
	if zero.Len() != 0 || zero.All() != nil || zero.NextUnassigned() != nil || zero.Player(0) != nil || zero.Player(-1) != nil {
		t.Fatal("zero Players invariants failed")
	}
	var nilPlayer *input.Player
	if nilPlayer.Index() != -1 || nilPlayer.Actions() != nil || nilPlayer.Pressed("x") || nilPlayer.JustPressed("x") || nilPlayer.JustReleased("x") || nilPlayer.Axis("x") != 0 || nilPlayer.Delta("x") != 0 || nilPlayer.GamepadConnected() || nilPlayer.UsesKeyboard() || nilPlayer.UsesPointer() || nilPlayer.HasDevice() {
		t.Fatal("nil *Player methods did not return zero values")
	}
	if _, ok := nilPlayer.GamepadID(); ok {
		t.Fatal("nil *Player GamepadID returned ok")
	}
	if _, ok := nilPlayer.Gamepad(); ok {
		t.Fatal("nil *Player Gamepad returned ok")
	}
	if zero.AssignGamepad(0, 1) || zero.UnassignGamepad(0) || zero.AssignKeyboard(0) || zero.UnassignKeyboard(0) || zero.AssignPointer(0) || zero.UnassignPointer(0) || zero.Unassign(0) {
		t.Fatal("out-of-range assignments returned true")
	}
	if input.NewPlayers(-3).Len() != 0 {
		t.Fatal("negative count NewPlayers had non-zero length")
	}
	zero.Step()
	zero.Reset()
	zero.Attach(nil)

	// Quick tap latching via OnGamepads per player.
	m := input.NewPlayers(2)
	m.Player(0).Actions().Bind("jump", input.GamepadButton(gamepad.ButtonSouth))
	m.Player(1).Actions().Bind("jump", input.GamepadButton(gamepad.ButtonSouth))
	m.AssignGamepad(0, 1)
	m.AssignGamepad(1, 2)
	m.Step()
	m.OnGamepads([]gamepad.Pad{
		pad(1, map[gamepad.Button]float32{gamepad.ButtonSouth: 1}, [4]float32{}),
		pad(2, nil, [4]float32{}),
	})
	m.OnGamepads([]gamepad.Pad{pad(1, nil, [4]float32{}), pad(2, nil, [4]float32{})})
	m.Step()
	if !m.Player(0).Pressed("jump") || !m.Player(0).JustPressed("jump") || m.Player(1).Pressed("jump") {
		t.Fatalf("per-player quick tap latch failed: P0=(%v,%v) P1=%v", m.Player(0).Pressed("jump"), m.Player(0).JustPressed("jump"), m.Player(1).Pressed("jump"))
	}

	// Reset mutes held gamepad buttons and watched keys until released; scope disposal stops WatchGamepads.
	h := startMPHarness(t, 2, func(cx *core.Scope, m *input.Players, _ *core.View) {
		m.Player(0).Actions().Bind("jump", input.GamepadButton(gamepad.ButtonSouth), input.Key("space"))
		m.Player(1).Actions().Bind("jump", input.GamepadButton(gamepad.ButtonSouth))
		m.AssignGamepad(0, 1)
		m.AssignKeyboard(0)
		m.AssignGamepad(1, 2)
		m.WatchKeys(cx)
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, Gamepads: true}))

	h.app.ConnectGamepad(1, "Xbox Wireless Controller")
	h.app.ConnectGamepad(2, "DualSense")
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	h.app.SetGamepadButton(2, gamepad.ButtonSouth, 1)
	h.app.KeyDown("space")
	got := h.step()
	if !got[0].jump || !got[1].jump {
		t.Fatalf("before reset: %+v", got)
	}

	h.app.Post(h.players.Reset)
	h.app.Settle()
	for range 2 {
		got = h.step()
		if got[0].jump || got[1].jump {
			t.Fatalf("held inputs fired after Reset: %+v", got)
		}
	}

	// Releasing and pressing again works.
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 0)
	h.app.KeyUp("space")
	h.step()
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	got = h.step()
	if !got[0].jump || !got[0].jumpJust || got[1].jump {
		t.Fatalf("after releasing P0 only: %+v", got)
	}

	// Disposing the watching scope stops reading gamepads and releases held inputs.
	h.app.Post(func() { h.shown.Set(false) })
	h.app.Settle()
	got = h.step()
	if got[0].jump || !got[0].jumpUp {
		t.Fatalf("after scope disposal: %+v", got[0])
	}

	// AccessGamepad denied reads no controllers.
	denied := input.NewPlayers(2)
	bindPadDefaults(denied.Player(0).Actions())
	denied.AssignGamepad(0, 1)
	steps := 0
	var p0Jump bool
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.ProvideAccess(denyGamepads{})
		denied.WatchGamepads(cx)
		loop := &game.Loop{
			Rate: 60,
			Update: func(time.Duration) {
				denied.Step()
				steps++
				p0Jump = denied.Player(0).Pressed("jump")
			},
		}
		loop.Run(cx)
		return core.View{}
	}, nsgotest.Environment(core.Environment{Gamepads: true}))
	app.ConnectGamepad(1, "Xbox Wireless Controller")
	app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	for before := steps; steps < before+2; {
		app.Advance(frameStep)
	}
	if p0Jump || len(denied.Gamepads()) != 0 {
		t.Fatalf("controllers read despite AccessGamepad refusal: jump=%v pads=%+v", p0Jump, denied.Gamepads())
	}

	// Per-player Save/Load round-trips independently.
	store := app.Storage()
	p0 := input.NewPlayers(2)
	p0.Player(0).Actions().Bind("jump", input.Key("space"))
	p0.Player(1).Actions().Bind("jump", input.GamepadButton(gamepad.ButtonSouth))
	if err := p0.Player(0).Actions().Save(store, "p0"); err != nil {
		t.Fatal(err)
	}
	if err := p0.Player(1).Actions().Save(store, "p1"); err != nil {
		t.Fatal(err)
	}
	restored := input.NewPlayers(2)
	if ok, err := restored.Player(0).Actions().Load(store, "p0"); !ok || err != nil {
		t.Fatalf("Load p0 = (%v, %v)", ok, err)
	}
	if ok, err := restored.Player(1).Actions().Load(store, "p1"); !ok || err != nil {
		t.Fatalf("Load p1 = (%v, %v)", ok, err)
	}
	if !slices.Equal(restored.Player(0).Actions().Bindings("jump"), []input.Input{input.Key("space")}) {
		t.Fatalf("restored P0 bindings = %+v", restored.Player(0).Actions().Bindings("jump"))
	}
	if !slices.Equal(restored.Player(1).Actions().Bindings("jump"), []input.Input{input.GamepadButton(gamepad.ButtonSouth)}) {
		t.Fatalf("restored P1 bindings = %+v", restored.Player(1).Actions().Bindings("jump"))
	}
}

func BenchmarkPlayersStepGamepads(b *testing.B) {
	m := input.NewPlayers(4)
	for i, p := range m.All() {
		bindPadDefaults(p.Actions())
		m.AssignGamepad(i, i+1)
	}
	pads := []gamepad.Pad{
		pad(1, map[gamepad.Button]float32{gamepad.ButtonSouth: 1}, [4]float32{0.5, -0.3, 0.1, 0.9}),
		pad(2, map[gamepad.Button]float32{gamepad.ButtonRT: 0.7}, [4]float32{-0.2, 0.4, 0, 0}),
		pad(3, nil, [4]float32{0.8, 0, 0, 0}),
		pad(4, map[gamepad.Button]float32{gamepad.ButtonDPadLeft: 1}, [4]float32{}),
	}
	// Warm up backing slices before measuring steady-state allocations.
	m.OnGamepads(pads)
	m.Step()

	b.ReportAllocs()
	for b.Loop() {
		m.OnGamepads(pads)
		m.Step()
		_ = m.Player(0).Axis("move")
		_ = m.Player(1).Pressed("fire")
	}
}
