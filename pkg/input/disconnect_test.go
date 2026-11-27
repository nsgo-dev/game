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

type disconnectHarness struct {
	app     *nsgotest.App
	players *input.Players
	loop    *game.Loop
	shown   *core.Signal[bool]
	steps   int
	last    []padStep
}

func startDisconnectHarness(t *testing.T, count int, setup func(cx *core.Scope, m *input.Players, loop *game.Loop, v *core.View), opts ...nsgotest.Option) *disconnectHarness {
	t.Helper()
	h := &disconnectHarness{
		players: input.NewPlayers(count),
		last:    make([]padStep, count),
	}
	h.app = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		h.shown = core.NewSignal(cx, true)
		v := core.View{
			TestID: "game",
			Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
		}
		h.loop = &game.Loop{
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
		if setup != nil {
			setup(cx, h.players, h.loop, &v)
		}
		h.players.Attach(&v)
		h.loop.Run(cx)
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

func (h *disconnectHarness) step() []padStep {
	before := h.steps
	for range 20 {
		h.app.Advance(frameStep)
		if h.steps != before {
			return slices.Clone(h.last)
		}
	}
	return nil
}

func TestPlayersDisconnectEmitsEventPausesAndReconnectRestoresBindings(t *testing.T) {
	var disconnects []input.DisconnectEvent
	var reconnects []input.ReconnectEvent
	var pauseLog []bool

	h := startDisconnectHarness(t, 1, func(_ *core.Scope, m *input.Players, loop *game.Loop, _ *core.View) {
		bindPadDefaults(m.Player(0).Actions())
		m.AssignGamepad(0, 1)
		m.SetDisconnectPolicy(input.DisconnectPolicy{
			PauseOnDisconnect: true,
			OnPause: func(paused bool) {
				pauseLog = append(pauseLog, paused)
				loop.SetPaused(paused)
			},
			OnDisconnect: func(ev input.DisconnectEvent) {
				// Held inputs must already be released when OnDisconnect fires during Step.
				if ev.Player.Pressed("jump") || ev.Player.Axis("move") != 0 {
					t.Errorf("OnDisconnect saw stale held input: jump=%v move=%v", ev.Player.Pressed("jump"), ev.Player.Axis("move"))
				}
				disconnects = append(disconnects, ev)
			},
			OnReconnect: func(ev input.ReconnectEvent) {
				reconnects = append(reconnects, ev)
			},
		})
	}, nsgotest.Environment(core.Environment{Gamepads: true}))

	// Before pad 1 ever connects, stepping must not fire a disconnect event or pause.
	got := h.step()
	if got == nil || len(disconnects) != 0 || h.loop.Paused || h.players.PausedByDisconnect() {
		t.Fatalf("before initial connect: disconnects=%+v paused=%v", disconnects, h.loop.Paused)
	}

	// Connect pad 1 and hold jump + stick right.
	h.app.ConnectGamepad(1, "Xbox Wireless Controller")
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	h.app.SetGamepadAxis(1, gamepad.AxisLeftX, 1)
	got = h.step()
	if !got[0].jump || !got[0].jumpJust || got[0].move != 1 {
		t.Fatalf("P0 active input before disconnect: %+v", got[0])
	}
	if len(disconnects) != 0 || len(reconnects) != 0 {
		t.Fatalf("unexpected events on initial connect: d=%+v r=%+v", disconnects, reconnects)
	}

	// Disconnect pad 1 mid-game: releases held inputs, emits event, and pauses loop.
	h.app.DisconnectGamepad(1)
	got = h.step()
	if got == nil {
		t.Fatal("disconnect step did not run")
	}
	if got[0].jump || !got[0].jumpUp || got[0].move != 0 {
		t.Fatalf("P0 held inputs not cleared on disconnect: %+v", got[0])
	}
	if len(disconnects) != 1 {
		t.Fatalf("disconnect events = %d, want 1", len(disconnects))
	}
	ev := disconnects[0]
	if ev.PlayerIndex != 0 || ev.Player != h.players.Player(0) || ev.PreviousGamepadID != 1 || ev.FallbackActive || !ev.ShouldPause {
		t.Fatalf("unexpected DisconnectEvent: %+v", ev)
	}
	if !h.loop.Paused || !h.players.PausedByDisconnect() || !h.players.Player(0).GamepadDisconnected() {
		t.Fatalf("pause state after disconnect: loop.Paused=%v PausedByDisconnect=%v GamepadDisconnected=%v", h.loop.Paused, h.players.PausedByDisconnect(), h.players.Player(0).GamepadDisconnected())
	}
	if !slices.Equal(pauseLog, []bool{true}) {
		t.Fatalf("pauseLog = %v, want [true]", pauseLog)
	}
	if dp := h.players.DisconnectedPlayers(); len(dp) != 1 || dp[0] != h.players.Player(0) {
		t.Fatalf("DisconnectedPlayers = %+v, want [P0]", dp)
	}

	// While paused, advancing frames must not run game.Loop.Update.
	pausedSteps := h.steps
	for range 5 {
		h.app.Advance(frameStep)
	}
	if h.steps != pausedSteps {
		t.Fatalf("loop ran %d updates while paused for disconnect", h.steps-pausedSteps)
	}

	// Reconnect the same pad 1: automatically restores player 1's bindings, emits OnReconnect, and unpauses.
	h.app.ConnectGamepad(1, "Xbox Wireless Controller")
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	h.app.SetGamepadAxis(1, gamepad.AxisLeftX, -1)
	got = h.step()
	if got == nil {
		t.Fatal("loop did not resume after reconnecting pad 1")
	}
	if h.loop.Paused || h.players.PausedByDisconnect() || h.players.Player(0).GamepadDisconnected() {
		t.Fatalf("still paused/disconnected after reconnect: loop=%v paused=%v disc=%v", h.loop.Paused, h.players.PausedByDisconnect(), h.players.Player(0).GamepadDisconnected())
	}
	if !got[0].jump || !got[0].jumpJust || got[0].move != -1 {
		t.Fatalf("P0 bindings not restored after reconnect: %+v", got[0])
	}
	if len(reconnects) != 1 {
		t.Fatalf("reconnect events = %d, want 1", len(reconnects))
	}
	rev := reconnects[0]
	if rev.PlayerIndex != 0 || rev.Player != h.players.Player(0) || rev.PreviousGamepadID != 1 || rev.NewGamepadID != 1 || rev.Reassigned || rev.FallbackActive || rev.ShouldPause {
		t.Fatalf("unexpected ReconnectEvent: %+v", rev)
	}
	if !slices.Equal(pauseLog, []bool{true, false}) {
		t.Fatalf("pauseLog = %v, want [true false]", pauseLog)
	}
	if dp := h.players.DisconnectedPlayers(); dp != nil {
		t.Fatalf("DisconnectedPlayers after reconnect = %+v, want nil", dp)
	}
}

func TestPlayersReassignOnAnotherPadPressRestoresBindings(t *testing.T) {
	var disconnects []input.DisconnectEvent
	var reconnects []input.ReconnectEvent

	h := startDisconnectHarness(t, 1, func(_ *core.Scope, m *input.Players, loop *game.Loop, _ *core.View) {
		bindPadDefaults(m.Player(0).Actions())
		m.AssignGamepad(0, 1)
		m.SetDisconnectPolicy(input.DisconnectPolicy{
			PauseOnDisconnect:    true,
			AllowReassignOnPress: true,
			OnPause:              loop.SetPaused,
			OnDisconnect:         func(ev input.DisconnectEvent) { disconnects = append(disconnects, ev) },
			OnReconnect:          func(ev input.ReconnectEvent) { reconnects = append(reconnects, ev) },
		})
	}, nsgotest.Environment(core.Environment{Gamepads: true}))

	// Connect pad 1 (assigned to P0) and pad 2 (unassigned, already holding ButtonWest to verify resting buttons don't steal).
	h.app.ConnectGamepad(1, "Xbox Wireless Controller")
	h.app.ConnectGamepad(2, "DualSense Wireless Controller")
	h.app.SetGamepadButton(2, gamepad.ButtonWest, 1)
	h.step()

	// Disconnect pad 1 -> pauses; pad 2's already-held button must NOT steal P0's slot.
	h.app.DisconnectGamepad(1)
	h.step()
	if !h.loop.Paused || len(disconnects) != 1 || len(reconnects) != 0 {
		t.Fatalf("expected pause without steal: paused=%v d=%d r=%d", h.loop.Paused, len(disconnects), len(reconnects))
	}
	for range 3 {
		h.app.Advance(frameStep)
	}
	if !h.loop.Paused || len(reconnects) != 0 {
		t.Fatalf("already-held button on pad 2 stole P0 slot: paused=%v r=%+v", h.loop.Paused, reconnects)
	}

	// Pressing a new button on pad 2 claims P0's slot, restores bindings, emits OnReconnect(Reassigned: true), and resumes.
	h.app.SetGamepadButton(2, gamepad.ButtonStart, 1)
	got := h.step()
	if got == nil {
		t.Fatal("loop did not resume after pressing button on pad 2")
	}
	if h.loop.Paused || h.players.PausedByDisconnect() {
		t.Fatalf("still paused after pad 2 press: loop=%v paused=%v", h.loop.Paused, h.players.PausedByDisconnect())
	}
	if id, ok := h.players.Player(0).GamepadID(); !ok || id != 2 || !h.players.Player(0).GamepadConnected() {
		t.Fatalf("P0 not reassigned to pad 2: id=(%d,%v) connected=%v", id, ok, h.players.Player(0).GamepadConnected())
	}
	if len(reconnects) != 1 {
		t.Fatalf("reconnect events = %d, want 1", len(reconnects))
	}
	rev := reconnects[0]
	if rev.PlayerIndex != 0 || rev.PreviousGamepadID != 1 || rev.NewGamepadID != 2 || !rev.Reassigned || rev.ShouldPause {
		t.Fatalf("unexpected ReconnectEvent: %+v", rev)
	}

	// P0's original bindings now respond to pad 2.
	h.app.SetGamepadButton(2, gamepad.ButtonStart, 0)
	h.app.SetGamepadButton(2, gamepad.ButtonSouth, 1)
	h.app.SetGamepadAxis(2, gamepad.AxisLeftX, 1)
	got = h.step()
	if !got[0].jump || !got[0].jumpJust || got[0].move != 1 {
		t.Fatalf("P0 bindings on reassigned pad 2 failed: %+v", got[0])
	}

	// Disconnect pad 2 while AllowReassignOnPress is disabled -> pressing on newly connected pad 3 is ignored until pad 2 reconnects.
	policy := h.players.DisconnectPolicy()
	policy.AllowReassignOnPress = false
	h.players.SetDisconnectPolicy(policy)

	h.app.DisconnectGamepad(2)
	h.step()
	if !h.loop.Paused || len(disconnects) != 2 {
		t.Fatalf("second disconnect did not pause: paused=%v d=%d", h.loop.Paused, len(disconnects))
	}
	h.app.ConnectGamepad(3, "Pro Controller")
	h.app.SetGamepadButton(3, gamepad.ButtonSouth, 1)
	for range 3 {
		h.app.Advance(frameStep)
	}
	if !h.loop.Paused || len(reconnects) != 1 {
		t.Fatalf("pad 3 claimed slot while AllowReassignOnPress was false: paused=%v r=%+v", h.loop.Paused, reconnects)
	}
	h.app.ConnectGamepad(2, "DualSense Wireless Controller")
	got = h.step()
	if got == nil || h.loop.Paused || len(reconnects) != 2 || reconnects[1].Reassigned {
		t.Fatalf("pad 2 reconnect failed: paused=%v r=%+v", h.loop.Paused, reconnects)
	}
}

func TestPlayersFallbackToKeyboardAndTouchContinuesWithoutPausing(t *testing.T) {
	var disconnects []input.DisconnectEvent
	var reconnects []input.ReconnectEvent
	pauseCalls := 0

	h := startDisconnectHarness(t, 1, func(cx *core.Scope, m *input.Players, loop *game.Loop, _ *core.View) {
		a := m.Player(0).Actions()
		a.Bind("jump",
			input.GamepadButton(gamepad.ButtonSouth),
			input.Key("space"),
			input.TouchRegion(core.Bounds{Origin: core.Point{X: 200}, Size: core.Size{Width: 200, Height: 300}}),
		)
		a.Bind("fire", input.GamepadButton(gamepad.ButtonRT))
		a.Bind("left", input.GamepadButton(gamepad.ButtonDPadLeft), input.Key("left"))
		a.Bind("right", input.GamepadButton(gamepad.ButtonDPadRight), input.Key("right"))
		a.BindAxis("move", "left", "right")
		a.BindGamepadAxis("move", gamepad.AxisLeftX, input.GamepadAxisOptions{})

		m.AssignGamepad(0, 1)
		m.WatchKeys(cx)
		m.SetDisconnectPolicy(input.DisconnectPolicy{
			PauseOnDisconnect:       true, // Fallback overrides pause so play continues uninterrupted.
			AllowReassignOnPress:    true,
			FallbackToKeyboardTouch: true,
			OnPause: func(paused bool) {
				pauseCalls++
				loop.SetPaused(paused)
			},
			OnDisconnect: func(ev input.DisconnectEvent) { disconnects = append(disconnects, ev) },
			OnReconnect:  func(ev input.ReconnectEvent) { reconnects = append(reconnects, ev) },
		})
	}, nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, Gamepads: true}))

	h.app.ConnectGamepad(1, "Xbox Wireless Controller")
	h.step()

	// While pad 1 is connected, keyboard is NOT assigned to P0.
	h.app.KeyDown("space")
	got := h.step()
	if got[0].jump || h.players.Player(0).UsesKeyboard() || h.players.Player(0).UsesPointer() {
		t.Fatalf("keyboard/pointer active before disconnect: jump=%v kb=%v ptr=%v", got[0].jump, h.players.Player(0).UsesKeyboard(), h.players.Player(0).UsesPointer())
	}
	h.app.KeyUp("space")
	h.step()

	// Disconnect pad 1 while holding jump: fallback activates without pausing.
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	h.step()
	h.app.DisconnectGamepad(1)
	got = h.step()
	if got == nil {
		t.Fatal("step did not run in fallback mode")
	}
	if got[0].jump || !got[0].jumpUp {
		t.Fatalf("held gamepad jump not released on fallback disconnect: %+v", got[0])
	}
	if h.loop.Paused || h.players.PausedByDisconnect() || pauseCalls != 0 {
		t.Fatalf("fallback mode paused loop: loop.Paused=%v PausedByDisconnect=%v pauseCalls=%d", h.loop.Paused, h.players.PausedByDisconnect(), pauseCalls)
	}
	if len(disconnects) != 1 || !disconnects[0].FallbackActive || disconnects[0].ShouldPause {
		t.Fatalf("unexpected fallback DisconnectEvent: %+v", disconnects)
	}
	if !h.players.Player(0).FallbackActive() || !h.players.Player(0).UsesKeyboard() || !h.players.Player(0).UsesPointer() {
		t.Fatalf("fallback flags not active on P0: fallback=%v kb=%v ptr=%v", h.players.Player(0).FallbackActive(), h.players.Player(0).UsesKeyboard(), h.players.Player(0).UsesPointer())
	}

	// Keyboard drives P0 immediately without pausing.
	h.app.KeyDown("space")
	h.app.KeyDown("right")
	got = h.step()
	if !got[0].jump || !got[0].jumpJust || got[0].move != 1 {
		t.Fatalf("keyboard fallback failed: %+v", got[0])
	}
	h.app.KeyUp("space")
	h.app.KeyUp("right")
	h.step()

	// Touch drives P0 too.
	h.app.Post(func() {
		h.players.OnTouch(touchAt(core.TouchStart, 1, 250, 150))
	})
	h.app.Settle()
	got = h.step()
	if !got[0].jump || !got[0].jumpJust {
		t.Fatalf("touch fallback failed: %+v", got[0])
	}

	// Reconnect via pressing on another pad (pad 2): exits fallback cleanly, clears active fallback touch, restores prior keyboard/pointer flags, and hands control to pad 2.
	h.app.ConnectGamepad(2, "DualSense Wireless Controller")
	h.app.SetGamepadButton(2, gamepad.ButtonRT, 1)
	got = h.step()
	if len(reconnects) != 1 || !reconnects[0].Reassigned || reconnects[0].NewGamepadID != 2 || reconnects[0].FallbackActive || reconnects[0].ShouldPause {
		t.Fatalf("unexpected fallback ReconnectEvent: %+v", reconnects)
	}
	if h.players.Player(0).FallbackActive() || h.players.Player(0).UsesKeyboard() || h.players.Player(0).UsesPointer() {
		t.Fatalf("fallback did not restore prior keyboard/pointer assignments: fallback=%v kb=%v ptr=%v", h.players.Player(0).FallbackActive(), h.players.Player(0).UsesKeyboard(), h.players.Player(0).UsesPointer())
	}
	if got[0].jump {
		t.Fatalf("active fallback touch was not cleared on gamepad recovery: %+v", got[0])
	}
	if !got[0].fire {
		t.Fatalf("pad 2 input did not drive P0 after recovery: %+v", got[0])
	}
	if pauseCalls != 0 {
		t.Fatalf("OnPause called %d times in fallback mode, want 0", pauseCalls)
	}
}

func TestPlayersFallbackPreservesPreExistingKeyboardAssignment(t *testing.T) {
	m := input.NewPlayers(1)
	a := m.Player(0).Actions()
	a.Bind("jump", input.GamepadButton(gamepad.ButtonSouth), input.Key("space"))
	m.AssignGamepad(0, 1)
	m.AssignKeyboard(0) // Already hybrid keyboard + gamepad before disconnect.
	m.SetDisconnectPolicy(input.DisconnectPolicy{
		PauseOnDisconnect:       true,
		FallbackToKeyboardTouch: true,
	})

	m.OnGamepads([]gamepad.Pad{pad(1, nil, [4]float32{})})
	m.Step()
	m.OnGamepads(nil)
	m.Step()
	if !m.Player(0).FallbackActive() || !m.Player(0).UsesKeyboard() || !m.Player(0).UsesPointer() {
		t.Fatalf("expected fallback active with kb+ptr")
	}

	// Reconnect pad 1: keyboard stays enabled (it was pre-existing), pointer returns to false.
	m.OnGamepads([]gamepad.Pad{pad(1, nil, [4]float32{})})
	m.Step()
	if m.Player(0).FallbackActive() || !m.Player(0).UsesKeyboard() || m.Player(0).UsesPointer() {
		t.Fatalf("did not restore pre-existing keyboard/pointer flags: fallback=%v kb=%v ptr=%v", m.Player(0).FallbackActive(), m.Player(0).UsesKeyboard(), m.Player(0).UsesPointer())
	}
}

func TestPlayersMultiplayerSimultaneousDisconnectsAndOrderedRecovery(t *testing.T) {
	var disconnects []input.DisconnectEvent
	var reconnects []input.ReconnectEvent
	var pauseLog []bool

	h := startDisconnectHarness(t, 2, func(_ *core.Scope, m *input.Players, loop *game.Loop, _ *core.View) {
		bindPadDefaults(m.Player(0).Actions())
		bindPadDefaults(m.Player(1).Actions())
		m.AssignGamepad(0, 1)
		m.AssignGamepad(1, 2)
		m.SetDisconnectPolicy(input.DisconnectPolicy{
			PauseOnDisconnect:    true,
			AllowReassignOnPress: true,
			OnPause: func(paused bool) {
				pauseLog = append(pauseLog, paused)
				loop.SetPaused(paused)
			},
			OnDisconnect: func(ev input.DisconnectEvent) { disconnects = append(disconnects, ev) },
			OnReconnect:  func(ev input.ReconnectEvent) { reconnects = append(reconnects, ev) },
		})
	}, nsgotest.Environment(core.Environment{Gamepads: true}))

	h.app.ConnectGamepad(1, "Pad 1")
	h.app.ConnectGamepad(2, "Pad 2")
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	h.app.SetGamepadButton(2, gamepad.ButtonSouth, 1)
	got := h.step()
	if !got[0].jump || !got[1].jump {
		t.Fatalf("both players should be jumping: %+v", got)
	}

	// Disconnect both controllers at once: pauses once, clears both players' held inputs.
	h.app.DisconnectGamepad(1)
	h.app.DisconnectGamepad(2)
	got = h.step()
	if got[0].jump || !got[0].jumpUp || got[1].jump || !got[1].jumpUp {
		t.Fatalf("both players' held inputs should clear on simultaneous disconnect: %+v", got)
	}
	if len(disconnects) != 2 || !slices.Equal(pauseLog, []bool{true}) {
		t.Fatalf("simultaneous disconnect: d=%+v pauseLog=%v", disconnects, pauseLog)
	}
	if dp := h.players.DisconnectedPlayers(); len(dp) != 2 || dp[0].Index() != 0 || dp[1].Index() != 1 {
		t.Fatalf("DisconnectedPlayers = %+v", dp)
	}

	// Unassigned pad 3 presses a button: claims lowest disconnected slot (P0), game stays paused for P1.
	h.app.ConnectGamepad(3, "Pad 3")
	h.app.SetGamepadButton(3, gamepad.ButtonStart, 1)
	h.app.Advance(frameStep)
	if len(reconnects) != 1 || reconnects[0].PlayerIndex != 0 || reconnects[0].NewGamepadID != 3 || !reconnects[0].Reassigned || !reconnects[0].ShouldPause {
		t.Fatalf("partial recovery event = %+v", reconnects)
	}
	if !h.loop.Paused || !h.players.PausedByDisconnect() {
		t.Fatal("loop resumed while P1 was still disconnected")
	}

	// Pad 2 reconnects: recovers P1, game resumes.
	h.app.ConnectGamepad(2, "Pad 2")
	h.app.SetGamepadButton(2, gamepad.ButtonSouth, 1)
	got = h.step()
	if got == nil || h.loop.Paused || h.players.PausedByDisconnect() {
		t.Fatalf("loop did not resume after P1 reconnected: paused=%v", h.loop.Paused)
	}
	if len(reconnects) != 2 || reconnects[1].PlayerIndex != 1 || reconnects[1].NewGamepadID != 2 || reconnects[1].Reassigned || reconnects[1].ShouldPause {
		t.Fatalf("final recovery event = %+v", reconnects)
	}
	if !slices.Equal(pauseLog, []bool{true, false}) {
		t.Fatalf("pauseLog = %v, want [true false]", pauseLog)
	}
	if !got[1].jump {
		t.Fatalf("P1 did not respond after reconnect: %+v", got[1])
	}
}

func TestPlayersDisconnectDeterministicOnGamepadsManualRecoveryAndPolicyUpdates(t *testing.T) {
	// Nil *Player and zero Players safety for new methods.
	var nilPlayer *input.Player
	if nilPlayer.GamepadDisconnected() || nilPlayer.FallbackActive() {
		t.Fatal("nil *Player disconnect methods returned true")
	}
	var zero input.Players
	if zero.DisconnectedPlayers() != nil || zero.PausedByDisconnect() {
		t.Fatal("zero Players disconnect state non-zero")
	}
	zero.PollGamepads()

	m := input.NewPlayers(2)
	bindPadDefaults(m.Player(0).Actions())
	bindPadDefaults(m.Player(1).Actions())
	m.AssignGamepad(0, 10)
	m.AssignGamepad(1, 20)

	var disconnects []input.DisconnectEvent
	var reconnects []input.ReconnectEvent
	var pauseLog []bool
	m.SetDisconnectPolicy(input.DisconnectPolicy{
		PauseOnDisconnect: true,
		OnPause:           func(p bool) { pauseLog = append(pauseLog, p) },
		OnDisconnect:      func(ev input.DisconnectEvent) { disconnects = append(disconnects, ev) },
		OnReconnect:       func(ev input.ReconnectEvent) { reconnects = append(reconnects, ev) },
	})

	// Connect both pads and hold jump on P0.
	m.OnGamepads([]gamepad.Pad{
		pad(10, map[gamepad.Button]float32{gamepad.ButtonSouth: 1}, [4]float32{1, 0}),
		pad(20, nil, [4]float32{}),
	})
	m.Step()
	if !m.Player(0).Pressed("jump") || m.Player(0).Axis("move") != 1 {
		t.Fatal("P0 did not register input")
	}

	// Pad 10 disconnects deterministically via OnGamepads; pad 30 is connected unassigned.
	m.OnGamepads([]gamepad.Pad{
		pad(20, nil, [4]float32{}),
		pad(30, nil, [4]float32{}),
	})
	m.Step()
	if m.Player(0).Pressed("jump") || !m.Player(0).JustReleased("jump") || m.Player(0).Axis("move") != 0 {
		t.Fatalf("P0 held inputs not cleared on deterministic disconnect")
	}
	if len(disconnects) != 1 || !m.PausedByDisconnect() || !slices.Equal(pauseLog, []bool{true}) {
		t.Fatalf("unexpected state after disconnect: d=%+v pause=%v", disconnects, pauseLog)
	}

	// Manual AssignGamepad to connected pad 30 recovers P0, emits OnReconnect, and unpauses.
	if !m.AssignGamepad(0, 30) {
		t.Fatal("AssignGamepad(0, 30) failed")
	}
	if len(reconnects) != 1 || !reconnects[0].Reassigned || reconnects[0].PreviousGamepadID != 10 || reconnects[0].NewGamepadID != 30 || reconnects[0].ShouldPause {
		t.Fatalf("manual AssignGamepad reconnect event = %+v", reconnects)
	}
	if m.PausedByDisconnect() || !slices.Equal(pauseLog, []bool{true, false}) {
		t.Fatalf("manual AssignGamepad did not unpause: pauseLog=%v", pauseLog)
	}

	// Disconnect pad 30, then UnassignGamepad(0) resolves the pause cleanly without emitting OnReconnect.
	m.OnGamepads([]gamepad.Pad{pad(20, nil, [4]float32{})})
	m.Step()
	if !m.PausedByDisconnect() || !slices.Equal(pauseLog, []bool{true, false, true}) {
		t.Fatalf("expected second pause: pauseLog=%v", pauseLog)
	}
	if !m.UnassignGamepad(0) {
		t.Fatal("UnassignGamepad(0) failed")
	}
	if m.PausedByDisconnect() || !slices.Equal(pauseLog, []bool{true, false, true, false}) || len(reconnects) != 1 {
		t.Fatalf("UnassignGamepad did not resolve pause cleanly: pauseLog=%v reconnects=%d", pauseLog, len(reconnects))
	}

	// Disconnect pad 20, then toggle FallbackToKeyboardTouch via SetDisconnectPolicy mid-disconnect: unpauses and activates fallback.
	m.OnGamepads(nil)
	m.Step()
	if !m.PausedByDisconnect() || !m.Player(1).GamepadDisconnected() {
		t.Fatal("P1 disconnect did not pause")
	}
	pol := m.DisconnectPolicy()
	pol.FallbackToKeyboardTouch = true
	m.SetDisconnectPolicy(pol)
	if m.PausedByDisconnect() || !m.Player(1).FallbackActive() {
		t.Fatalf("toggling fallback mid-disconnect failed: paused=%v fallback=%v", m.PausedByDisconnect(), m.Player(1).FallbackActive())
	}
	// Turning fallback back off while still disconnected re-pauses and restores keyboard/pointer flags.
	pol.FallbackToKeyboardTouch = false
	m.SetDisconnectPolicy(pol)
	if !m.PausedByDisconnect() || m.Player(1).FallbackActive() || m.Player(1).UsesKeyboard() || m.Player(1).UsesPointer() {
		t.Fatalf("disabling fallback mid-disconnect failed: paused=%v fallback=%v kb=%v ptr=%v", m.PausedByDisconnect(), m.Player(1).FallbackActive(), m.Player(1).UsesKeyboard(), m.Player(1).UsesPointer())
	}
	// Unassigning P1 clears disconnect state and unpauses.
	m.Unassign(1)
	if m.PausedByDisconnect() || m.Player(1).GamepadDisconnected() {
		t.Fatal("Unassign(1) did not clear disconnect pause")
	}
}

func TestPlayersWatchDisposalDoesNotEmitFalseDisconnect(t *testing.T) {
	var disconnects []input.DisconnectEvent
	h := startDisconnectHarness(t, 1, func(_ *core.Scope, m *input.Players, loop *game.Loop, _ *core.View) {
		bindPadDefaults(m.Player(0).Actions())
		m.AssignGamepad(0, 1)
		m.SetDisconnectPolicy(input.DisconnectPolicy{
			PauseOnDisconnect: true,
			OnPause:           loop.SetPaused,
			OnDisconnect:      func(ev input.DisconnectEvent) { disconnects = append(disconnects, ev) },
		})
	}, nsgotest.Environment(core.Environment{Gamepads: true}))

	h.app.ConnectGamepad(1, "Xbox Wireless Controller")
	h.app.SetGamepadButton(1, gamepad.ButtonSouth, 1)
	got := h.step()
	if !got[0].jump {
		t.Fatal("P0 should be jumping")
	}

	// Disposing the watching scope stops gamepad polling and releases inputs without emitting a mid-game disconnect event.
	h.app.Post(func() { h.shown.Set(false) })
	h.app.Settle()
	got = h.step()
	if got[0].jump || !got[0].jumpUp {
		t.Fatalf("held button not released on scope disposal: %+v", got[0])
	}
	if len(disconnects) != 0 || h.loop.Paused {
		t.Fatalf("scope disposal triggered false disconnect: d=%+v paused=%v", disconnects, h.loop.Paused)
	}
}
