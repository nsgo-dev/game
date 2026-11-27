package input

import (
	"cmp"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
)

// Player is one player's input slot in a Players group, with its own Actions and assigned physical devices.
//
// A nil *Player reports zero/false for all queries and -1 for Index.
type Player struct {
	players             *Players
	index               int
	actions             *Actions
	hasGamepad          bool
	gamepadID           int
	gpWasConnected      bool
	gpDisconnected      bool
	disconnectedPadID   int
	fallbackActive      bool
	preFallbackKeyboard bool
	preFallbackPointer  bool
	usesKeyboard        bool
	usesPointer         bool
}

// Index returns p's 0-based slot index in its Players group, or -1 if p is nil.
func (p *Player) Index() int {
	if p == nil {
		return -1
	}
	return p.index
}

// Actions returns p's Actions, where bindings, axes, virtual pads, and saved controls live. Returns nil if p is nil.
func (p *Player) Actions() *Actions {
	if p == nil {
		return nil
	}
	return p.actions
}

// Pressed reports whether action name is held for p during the current step.
func (p *Player) Pressed(name string) bool {
	if p == nil {
		return false
	}
	return p.actions.Pressed(name)
}

// JustPressed reports whether action name became pressed for p on the current step.
func (p *Player) JustPressed(name string) bool {
	if p == nil {
		return false
	}
	return p.actions.JustPressed(name)
}

// JustReleased reports whether action name was released for p on the current step.
func (p *Player) JustReleased(name string) bool {
	if p == nil {
		return false
	}
	return p.actions.JustReleased(name)
}

// Axis returns the value of axis name in [-1, 1] for p on the current step.
func (p *Player) Axis(name string) float64 {
	if p == nil {
		return 0
	}
	return p.actions.Axis(name)
}

// Delta returns the relative motion of pointer axis name for p on the current step.
func (p *Player) Delta(name string) float64 {
	if p == nil {
		return 0
	}
	return p.actions.Delta(name)
}

// GamepadID returns the controller ID assigned to p and true, or (0, false) when p has no assigned controller.
func (p *Player) GamepadID() (id int, ok bool) {
	if p == nil || !p.hasGamepad {
		return 0, false
	}
	return p.gamepadID, true
}

// Gamepad returns the latest connected controller state for p's assigned controller, or false when p has no assigned controller or that controller is not currently connected.
func (p *Player) Gamepad() (gamepad.Pad, bool) {
	if p == nil || !p.hasGamepad || p.players == nil {
		return gamepad.Pad{}, false
	}
	pads := p.players.padsForID(p.gamepadID)
	if len(pads) == 0 {
		return gamepad.Pad{}, false
	}
	return pads[0], true
}

// GamepadConnected reports whether p has an assigned controller that is present in the latest controller snapshot.
func (p *Player) GamepadConnected() bool {
	_, ok := p.Gamepad()
	return ok
}

// UsesKeyboard reports whether keyboard events from p's Players group are routed to p.
func (p *Player) UsesKeyboard() bool {
	if p == nil {
		return false
	}
	return p.usesKeyboard
}

// UsesPointer reports whether touch, mouse, and pointer-lock events from p's Players group are routed to p.
func (p *Player) UsesPointer() bool {
	if p == nil {
		return false
	}
	return p.usesPointer
}

// HasDevice reports whether at least one physical device (a controller, keyboard, or pointer) is assigned to p.
func (p *Player) HasDevice() bool {
	if p == nil {
		return false
	}
	return p.hasGamepad || p.usesKeyboard || p.usesPointer
}

// SetAssists sets the player-chosen motor assists applied to p's Actions during Step.
func (p *Player) SetAssists(assists Assists) {
	if p == nil {
		return
	}
	p.actions.SetAssists(assists)
}

// Assists returns a copy of p's active motor assists.
func (p *Player) Assists() Assists {
	if p == nil {
		return Assists{}
	}
	return p.actions.Assists()
}

// SetActionAssist declares which motor assists action name opts into for p.
func (p *Player) SetActionAssist(name string, spec ActionAssist) {
	if p == nil {
		return
	}
	p.actions.SetActionAssist(name, spec)
}

// ActionAssist returns the declared ActionAssist specification for name on p.
func (p *Player) ActionAssist(name string) ActionAssist {
	if p == nil {
		return ActionAssist{}
	}
	return p.actions.ActionAssist(name)
}

// ToggleActive reports whether action name is currently latched on by a Toggle assist for p.
func (p *Player) ToggleActive(name string) bool {
	if p == nil {
		return false
	}
	return p.actions.ToggleActive(name)
}

// ReleaseToggles releases all latched toggles and active mash holds on p.
func (p *Player) ReleaseToggles() {
	if p == nil {
		return
	}
	p.actions.ReleaseToggles()
}

// Players coordinates local multiplayer input across several player slots, routing each connected game controller to at most one player and sharing keyboard or pointer events among the players assigned to them, sampled once per fixed game.Loop step.
//
// Each player owns its own Actions (accessed via Player.Actions), so several players can share the same gamepad bindings on separate controllers, use disjoint key sets on one keyboard, or combine keyboard, pointer, and game controllers.
//
// Call WatchGamepads, WatchKeys, and/or Attach on Players rather than on individual players' Actions, assign devices with AssignGamepad, AssignKeyboard, AssignPointer, or AssignUnassignedGamepads, and call Step once at the start of each game.Loop.Update step.
//
// The zero value is ready to use with no players; call NewPlayers or Add to create slots. Players' methods are called on the engine's goroutine, as core.View's callbacks and game.Loop.Update are.
type Players struct {
	players []*Player

	gpScope            *core.Scope
	gpWatchGen         uint64
	gpPads             []gamepad.Pad
	prevPadButtons     []padButtonState
	cancelWaitRAF      func()
	policy             DisconnectPolicy
	pausedByDisconnect bool
	pendingDisconnects []DisconnectEvent
	pendingReconnects  []ReconnectEvent

	keyWatchGen  uint64
	stopKeys     func()
	watchedKeys  []string
	watchedCodes []string

	stepRate int
}

// NewPlayers returns a Players group with count player slots (0 to count-1), each with an empty Actions and no assigned devices. A non-positive count returns an empty Players.
func NewPlayers(count int) *Players {
	m := &Players{}
	if count > 0 {
		m.players = make([]*Player, count)
		for i := range count {
			m.players[i] = &Player{
				players: m,
				index:   i,
				actions: NewActions(),
			}
		}
	}
	return m
}

// Add appends a new player slot with an empty Actions and no assigned devices, and returns it.
func (m *Players) Add() *Player {
	a := NewActions()
	if m.stepRate > 0 {
		a.SetStepRate(m.stepRate)
	}
	p := &Player{
		players: m,
		index:   len(m.players),
		actions: a,
	}
	m.players = append(m.players, p)
	return p
}

// Len returns the number of player slots in m.
func (m *Players) Len() int {
	return len(m.players)
}

// Player returns the player at slot index, or nil if index is out of range.
func (m *Players) Player(index int) *Player {
	if index < 0 || index >= len(m.players) {
		return nil
	}
	return m.players[index]
}

// All returns a copy of the player slots in index order, or nil when m has no players.
func (m *Players) All() []*Player {
	if len(m.players) == 0 {
		return nil
	}
	return slices.Clone(m.players)
}

// NextUnassigned returns the first player slot with no device assigned, or nil when every slot has a device.
func (m *Players) NextUnassigned() *Player {
	for _, p := range m.players {
		if !p.HasDevice() {
			return p
		}
	}
	return nil
}

// PlayerByGamepad returns the player currently assigned to controller padID, or nil if no player owns padID.
func (m *Players) PlayerByGamepad(padID int) *Player {
	for _, p := range m.players {
		if p.hasGamepad && p.gamepadID == padID {
			return p
		}
	}
	return nil
}

// AssignGamepad assigns controller padID exclusively to player index, returning false if index is out of range.
//
// If another player already owns padID, that player is unassigned from padID first and its held controller inputs are released. If padID is not yet connected, the assignment is remembered and takes effect once padID appears in Step or OnGamepads. If player index was awaiting reconnect and padID is currently connected, AssignGamepad completes recovery, ends any active fallback, and emits OnReconnect.
func (m *Players) AssignGamepad(index, padID int) bool {
	p := m.Player(index)
	if p == nil {
		return false
	}
	if p.hasGamepad && p.gamepadID == padID {
		return true
	}
	if prev := m.PlayerByGamepad(padID); prev != nil && prev != p {
		m.clearPlayerGamepad(prev)
	}
	wasDisconnected := p.gpDisconnected
	prevID := p.disconnectedPadID
	pads := m.padsForID(padID)
	connected := len(pads) > 0

	p.hasGamepad = true
	p.gamepadID = padID
	p.gpWasConnected = connected
	p.gpDisconnected = false
	p.disconnectedPadID = 0
	if p.fallbackActive {
		m.deactivateFallback(p)
	}
	p.actions.SelectGamepad(padID)
	p.actions.OnGamepads(pads)

	if wasDisconnected && connected {
		m.pendingReconnects = append(m.pendingReconnects, ReconnectEvent{
			PlayerIndex:       p.index,
			Player:            p,
			PreviousGamepadID: prevID,
			NewGamepadID:      padID,
			Reassigned:        padID != prevID,
		})
	}
	m.flushDisconnectEvents()
	return true
}

// UnassignGamepad removes the controller assignment from player index and releases any inputs it held. Returns false if index is out of range or player index had no controller assigned.
func (m *Players) UnassignGamepad(index int) bool {
	p := m.Player(index)
	if p == nil || !p.hasGamepad {
		return false
	}
	m.clearPlayerGamepad(p)
	m.syncDisconnectPause()
	m.syncWaitRAF()
	return true
}

func (m *Players) clearPlayerGamepad(p *Player) {
	p.hasGamepad = false
	p.gamepadID = 0
	p.gpWasConnected = false
	p.gpDisconnected = false
	p.disconnectedPadID = 0
	if p.fallbackActive {
		m.deactivateFallback(p)
	}
	p.actions.SelectAnyGamepad()
	p.actions.OnGamepads(nil)
}

// AssignKeyboard routes keyboard events from m (WatchKeys, OnKeyDown, OnKeyUp, Attach) to player index, returning false if index is out of range.
//
// Multiple players may be assigned to the keyboard simultaneously so they can share one keyboard with disjoint key sets.
func (m *Players) AssignKeyboard(index int) bool {
	p := m.Player(index)
	if p == nil {
		return false
	}
	if p.fallbackActive {
		p.preFallbackKeyboard = true
	}
	if p.usesKeyboard {
		return true
	}
	p.usesKeyboard = true
	if m.stopKeys != nil {
		p.actions.setWatchedKeys(m.watchedKeys, m.watchedCodes)
	}
	return true
}

// UnassignKeyboard stops routing keyboard events from m to player index and releases any keys held on that player. Returns false if index is out of range or player index was not assigned to the keyboard.
func (m *Players) UnassignKeyboard(index int) bool {
	p := m.Player(index)
	if p == nil || !p.usesKeyboard {
		return false
	}
	if p.fallbackActive {
		p.preFallbackKeyboard = false
	}
	p.usesKeyboard = false
	p.actions.clearKeyboardInput()
	return true
}

// AssignPointer routes touch, mouse, and pointer-lock events from m (Attach and OnTouch/OnPressDown/OnMouseMove/OnPointerDelta) to player index, returning false if index is out of range.
//
// Multiple players may be assigned to pointer events, such as split-screen touch regions on one screen.
func (m *Players) AssignPointer(index int) bool {
	p := m.Player(index)
	if p == nil {
		return false
	}
	if p.fallbackActive {
		p.preFallbackPointer = true
	}
	p.usesPointer = true
	return true
}

// UnassignPointer stops routing touch, mouse, and pointer-lock events from m to player index and releases any touches, held mouse buttons, or pending pointer deltas on that player. Returns false if index is out of range or player index was not assigned to pointer events.
func (m *Players) UnassignPointer(index int) bool {
	p := m.Player(index)
	if p == nil || !p.usesPointer {
		return false
	}
	if p.fallbackActive {
		p.preFallbackPointer = false
	}
	p.usesPointer = false
	p.actions.clearPointerInput()
	return true
}

// Unassign removes all device assignments (controller, keyboard, and pointer) from player index and releases any inputs they held. Returns false if index is out of range.
func (m *Players) Unassign(index int) bool {
	p := m.Player(index)
	if p == nil {
		return false
	}
	p.fallbackActive = false
	p.preFallbackKeyboard = false
	p.preFallbackPointer = false
	if p.hasGamepad {
		m.clearPlayerGamepad(p)
	}
	if p.usesKeyboard {
		p.usesKeyboard = false
		p.actions.clearKeyboardInput()
	}
	if p.usesPointer {
		p.usesPointer = false
		p.actions.clearPointerInput()
	}
	m.syncDisconnectPause()
	m.syncWaitRAF()
	return true
}

// AssignUnassignedGamepads assigns each connected controller from the last read (Step or OnGamepads) that is not yet assigned to any player, in ascending controller ID order, to the next player slot that has no device assigned, and returns how many controllers were assigned.
func (m *Players) AssignUnassignedGamepads() int {
	assigned := 0
	for i := range m.gpPads {
		id := m.gpPads[i].ID
		if m.PlayerByGamepad(id) != nil {
			continue
		}
		slot := m.NextUnassigned()
		if slot == nil {
			break
		}
		m.AssignGamepad(slot.index, id)
		assigned++
	}
	return assigned
}

// WatchGamepads makes m poll connected controllers from cx's app once on every Step with gamepad.Pads and route each controller to the player assigned to its ID.
//
// Polling stops once cx is disposed, and while cx's core.Access refuses core.AccessGamepad no controller is read. Passing nil stops watching and releases every controller input across all players.
func (m *Players) WatchGamepads(cx *core.Scope) {
	if cx == nil {
		m.gpWatchGen++
		m.stopWaitRAF()
		m.gpScope = nil
		m.gpPads = m.gpPads[:0]
		m.prevPadButtons = m.prevPadButtons[:0]
		m.dispatchGamepads(true)
		return
	}
	if m.gpScope == cx {
		m.syncWaitRAF()
		return
	}
	m.gpWatchGen++
	gen := m.gpWatchGen
	m.stopWaitRAF()
	m.gpScope = cx
	cx.Cleanup(func() {
		if m.gpWatchGen != gen {
			return
		}
		m.gpWatchGen++
		m.stopWaitRAF()
	})
	m.syncWaitRAF()
}

// OnGamepads replaces the controller state m reads with pads, such as synthetic or recorded controllers in a unit test or replay, routes each controller to its assigned player, processes any disconnect, reconnect, or press-to-reassign policy transitions, and latches any rising edges so a quick press between steps registers on the next Step.
//
// While WatchGamepads has a live scope attached, the next Step replaces pads with the live controllers.
func (m *Players) OnGamepads(pads []gamepad.Pad) {
	m.setGamepads(pads)
	m.reconcileGamepads(true)
	m.flushDisconnectEvents()
}

// Gamepads returns a copy of the controller state m last read, sorted by controller ID.
func (m *Players) Gamepads() []gamepad.Pad {
	return slices.Clone(m.gpPads)
}

// UnassignedGamepads returns a copy of the controllers m last read that are not currently assigned to any player, sorted by controller ID.
func (m *Players) UnassignedGamepads() []gamepad.Pad {
	var out []gamepad.Pad
	for i := range m.gpPads {
		if m.PlayerByGamepad(m.gpPads[i].ID) == nil {
			out = append(out, m.gpPads[i])
		}
	}
	return out
}

// WatchKeys watches the keys held anywhere on cx's window once and routes them to every player assigned to the keyboard (AssignKeyboard), until cx is disposed, WatchKeys is called again, or stop is called, which releases watched keys on every player.
func (m *Players) WatchKeys(cx *core.Scope) (stop func()) {
	if m.stopKeys != nil {
		m.stopKeys()
	}
	if cx == nil {
		return func() {}
	}
	m.keyWatchGen++
	gen := m.keyWatchGen
	stopEffect := cx.Effect(func() {
		keys, codes := core.KeysDown(cx), core.KeyCodesDown(cx)
		cx.Untrack(func() { m.setWatchedKeys(keys, codes) })
	})
	cleanup := func() {
		if m.keyWatchGen != gen {
			return
		}
		m.keyWatchGen++
		m.stopKeys = nil
		stopEffect()
		m.setWatchedKeys(nil, nil)
	}
	m.stopKeys = cleanup
	cx.Cleanup(cleanup)
	return cleanup
}

// Step polls live controllers once (when WatchGamepads is attached), reconciles any mid-game controller disconnects, reconnects, or press-to-reassign transitions, routes controllers to their assigned players, steps every player's Actions, and delivers any DisconnectPolicy callbacks. Call it once at the start of each game.Loop.Update step.
func (m *Players) Step() {
	if m.readLiveGamepads() {
		m.reconcileGamepads(false)
	}
	for _, p := range m.players {
		p.actions.Step()
	}
	m.flushDisconnectEvents()
}

// Reset calls Reset on every player's Actions and clears m's last controller snapshot, keeping all player slots, bindings, and device assignments. Held keys and controller buttons stay inactive until released, matching Actions.Reset.
func (m *Players) Reset() {
	for _, p := range m.players {
		p.actions.Reset()
	}
	m.gpPads = m.gpPads[:0]
}

// SetStepRate sets the fixed step rate in steps per second on every player's Actions
// (and any players added later) for timed motor assists.
func (m *Players) SetStepRate(rate int) {
	if rate <= 0 {
		m.stepRate = 0
	} else {
		m.stepRate = rate
	}
	for _, p := range m.players {
		p.actions.SetStepRate(rate)
	}
}

// StepRate returns the fixed step rate in steps per second configured on m (DefaultStepRate when unset).
func (m *Players) StepRate() int {
	if m == nil || m.stepRate <= 0 {
		return DefaultStepRate
	}
	return m.stepRate
}

// ReleaseToggles releases all latched toggles and active mash holds on every player's Actions.
func (m *Players) ReleaseToggles() {
	for _, p := range m.players {
		p.actions.ReleaseToggles()
	}
}

// SetPaused forwards pause state to every player's Actions, releasing latched toggles when paused. Suitable for loop.OnPause.
func (m *Players) SetPaused(paused bool) {
	for _, p := range m.players {
		p.actions.SetPaused(paused)
	}
}

// Attach wires m's touch, press, mouse, context-menu, pointer-lock motion, and key handlers onto v, chaining any callbacks already set on v. Keyboard events are forwarded to players with AssignKeyboard; pointer and touch events are forwarded to players with AssignPointer.
func (m *Players) Attach(v *core.View) {
	if v == nil {
		return
	}
	if prev := v.OnTouch; prev != nil {
		v.OnTouch = func(ev core.TouchEvent) { m.OnTouch(ev); prev(ev) }
	} else {
		v.OnTouch = m.OnTouch
	}
	if prev := v.OnPressDown; prev != nil {
		v.OnPressDown = func(ev core.PressEvent) { m.OnPressDown(ev); prev(ev) }
	} else {
		v.OnPressDown = m.OnPressDown
	}
	if prev := v.OnPressUp; prev != nil {
		v.OnPressUp = func(ev core.PressEvent) { m.OnPressUp(ev); prev(ev) }
	} else {
		v.OnPressUp = m.OnPressUp
	}
	if prev := v.OnMouseMove; prev != nil {
		v.OnMouseMove = func(ev core.MouseEvent) { m.OnMouseMove(ev); prev(ev) }
	} else {
		v.OnMouseMove = m.OnMouseMove
	}
	if prev := v.OnMouseLeave; prev != nil {
		v.OnMouseLeave = func(ev core.MouseEvent) { m.OnMouseLeave(ev); prev(ev) }
	} else {
		v.OnMouseLeave = m.OnMouseLeave
	}
	if prev := v.OnContextMenu; prev != nil {
		v.OnContextMenu = func(ev core.ContextMenuEvent) { m.OnContextMenu(ev); prev(ev) }
	} else {
		v.OnContextMenu = m.OnContextMenu
	}
	if prev := v.OnPointerDelta; prev != nil {
		v.OnPointerDelta = func(dx, dy float64) { m.OnPointerDelta(dx, dy); prev(dx, dy) }
	} else {
		v.OnPointerDelta = m.OnPointerDelta
	}
	if prev := v.OnKeyDown; prev != nil {
		v.OnKeyDown = func(ev core.KeyEvent) bool { consumed := m.OnKeyDown(ev); return prev(ev) || consumed }
	} else {
		v.OnKeyDown = m.OnKeyDown
	}
	if prev := v.OnKeyUp; prev != nil {
		v.OnKeyUp = func(ev core.KeyEvent) { m.OnKeyUp(ev); prev(ev) }
	} else {
		v.OnKeyUp = m.OnKeyUp
	}
}

// OnKeyDown forwards ev to every player assigned to the keyboard and reports whether any of them consumed it.
func (m *Players) OnKeyDown(ev core.KeyEvent) bool {
	consumed := false
	for _, p := range m.players {
		if p.usesKeyboard && p.actions.OnKeyDown(ev) {
			consumed = true
		}
	}
	return consumed
}

// OnKeyUp forwards ev to every player assigned to the keyboard.
func (m *Players) OnKeyUp(ev core.KeyEvent) {
	for _, p := range m.players {
		if p.usesKeyboard {
			p.actions.OnKeyUp(ev)
		}
	}
}

// OnTouch forwards ev to every player assigned to pointer events.
func (m *Players) OnTouch(ev core.TouchEvent) {
	for _, p := range m.players {
		if p.usesPointer {
			p.actions.OnTouch(ev)
		}
	}
}

// OnPressDown forwards ev to every player assigned to pointer events.
func (m *Players) OnPressDown(ev core.PressEvent) {
	for _, p := range m.players {
		if p.usesPointer {
			p.actions.OnPressDown(ev)
		}
	}
}

// OnPressUp forwards ev to every player assigned to pointer events.
func (m *Players) OnPressUp(ev core.PressEvent) {
	for _, p := range m.players {
		if p.usesPointer {
			p.actions.OnPressUp(ev)
		}
	}
}

// OnMouseMove forwards ev to every player assigned to pointer events.
func (m *Players) OnMouseMove(ev core.MouseEvent) {
	for _, p := range m.players {
		if p.usesPointer {
			p.actions.OnMouseMove(ev)
		}
	}
}

// OnMouseLeave forwards ev to every player assigned to pointer events.
func (m *Players) OnMouseLeave(ev core.MouseEvent) {
	for _, p := range m.players {
		if p.usesPointer {
			p.actions.OnMouseLeave(ev)
		}
	}
}

// OnContextMenu forwards ev to every player assigned to pointer events.
func (m *Players) OnContextMenu(ev core.ContextMenuEvent) {
	for _, p := range m.players {
		if p.usesPointer {
			p.actions.OnContextMenu(ev)
		}
	}
}

// OnPointerDelta forwards relative pointer motion to every player assigned to pointer events.
func (m *Players) OnPointerDelta(dx, dy float64) {
	for _, p := range m.players {
		if p.usesPointer {
			p.actions.OnPointerDelta(dx, dy)
		}
	}
}

func (m *Players) setGamepads(pads []gamepad.Pad) {
	if len(pads) == 0 {
		m.gpPads = m.gpPads[:0]
		return
	}
	m.gpPads = append(m.gpPads[:0], pads...)
	slices.SortStableFunc(m.gpPads, func(x, y gamepad.Pad) int { return cmp.Compare(x.ID, y.ID) })
}

func (m *Players) dispatchGamepads(sync bool) {
	for _, p := range m.players {
		var pads []gamepad.Pad
		if p.hasGamepad {
			pads = m.padsForID(p.gamepadID)
		}
		p.actions.setGamepads(pads)
		if sync {
			p.actions.syncRaw()
		}
	}
}

func (m *Players) padsForID(id int) []gamepad.Pad {
	start := -1
	for i := range m.gpPads {
		if m.gpPads[i].ID == id {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			return m.gpPads[start:i]
		}
	}
	if start >= 0 {
		return m.gpPads[start:]
	}
	return nil
}

func (m *Players) setWatchedKeys(keys, codes []string) {
	m.watchedKeys = append(m.watchedKeys[:0], keys...)
	m.watchedCodes = append(m.watchedCodes[:0], codes...)
	for _, p := range m.players {
		if p.usesKeyboard {
			p.actions.setWatchedKeys(m.watchedKeys, m.watchedCodes)
		}
	}
}

func (a *Actions) clearKeyboardInput() {
	clear(a.keysDown)
	a.watchedKeys = a.watchedKeys[:0]
	a.watchedCodes = a.watchedCodes[:0]
	a.ignoredKeys = a.ignoredKeys[:0]
	a.ignoredCodes = a.ignoredCodes[:0]
	a.syncRaw()
}

func (a *Actions) clearPointerInput() {
	clear(a.touches)
	a.mousePos = core.Point{}
	a.hasMousePos = false
	a.pressDown = false
	a.moveButton = core.MouseButtonNone
	a.rightPulse = false
	a.pendingDX = 0
	a.pendingDY = 0
	for _, pad := range a.pads {
		pad.Reset()
	}
	a.syncRaw()
}
