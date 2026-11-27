package input

import (
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
)

// DisconnectPolicy configures how Players responds when a player's assigned game controller disconnects mid-game and how that player recovers.
type DisconnectPolicy struct {
	// PauseOnDisconnect requests a pause (via OnPause) whenever at least one player's assigned controller disconnects without an active keyboard/pointer fallback, and requests resume once every disconnected player that caused the pause has recovered or been unassigned.
	PauseOnDisconnect bool
	// AllowReassignOnPress lets an unassigned connected controller claim the lowest-index player slot awaiting reconnect when a button is newly pressed on that controller.
	AllowReassignOnPress bool
	// FallbackToKeyboardTouch lets a player whose assigned controller disconnects continue playing via keyboard and pointer/touch without pausing. Players temporarily enables keyboard and pointer routing on that player until a controller reconnects or claims the slot, then restores the player's prior keyboard and pointer assignments.
	FallbackToKeyboardTouch bool
	// OnPause, when set, is called with true when PauseOnDisconnect pauses gameplay and with false once every disconnected required player has recovered or been unassigned. Pass game.Loop.SetPaused directly to pause and resume a Loop.
	OnPause func(paused bool)
	// OnDisconnect, when set, is called when a player's previously connected assigned controller disappears from the controller snapshot.
	OnDisconnect func(DisconnectEvent)
	// OnReconnect, when set, is called when a player awaiting reconnect regains its previous controller or is reassigned to another connected controller (automatically via AllowReassignOnPress or manually via AssignGamepad).
	OnReconnect func(ReconnectEvent)
}

// DisconnectEvent describes a player's assigned game controller disconnecting mid-game.
type DisconnectEvent struct {
	// PlayerIndex is the 0-based index of the affected player slot.
	PlayerIndex int
	// Player is the affected player slot.
	Player *Player
	// PreviousGamepadID is the controller ID that disconnected.
	PreviousGamepadID int
	// FallbackActive reports whether FallbackToKeyboardTouch enabled keyboard and pointer fallback for this player instead of pausing.
	FallbackActive bool
	// ShouldPause reports whether the policy requests pausing gameplay for this disconnect.
	ShouldPause bool
}

// ReconnectEvent describes a disconnected player recovering a game controller, either by reconnecting the same controller or by reassigning to another controller.
type ReconnectEvent struct {
	// PlayerIndex is the 0-based index of the recovered player slot.
	PlayerIndex int
	// Player is the recovered player slot.
	Player *Player
	// PreviousGamepadID is the controller ID that had disconnected.
	PreviousGamepadID int
	// NewGamepadID is the controller ID now assigned to the player.
	NewGamepadID int
	// Reassigned reports whether NewGamepadID differs from PreviousGamepadID.
	Reassigned bool
	// FallbackActive is false once controller control is restored.
	FallbackActive bool
	// ShouldPause reports whether Players still requires gameplay to remain paused after this reconnect (for example, because another player is still disconnected).
	ShouldPause bool
}

type padButtonState struct {
	id      int
	buttons uint32
}

// GamepadDisconnected reports whether p has an assigned controller that was previously connected and has since disconnected, awaiting reconnect or reassignment. Returns false if p is nil.
func (p *Player) GamepadDisconnected() bool {
	if p == nil {
		return false
	}
	return p.gpDisconnected
}

// FallbackActive reports whether p is currently using temporary keyboard and pointer fallback because its assigned controller disconnected under DisconnectPolicy.FallbackToKeyboardTouch. Returns false if p is nil.
func (p *Player) FallbackActive() bool {
	if p == nil {
		return false
	}
	return p.fallbackActive
}

// SetDisconnectPolicy configures how m handles mid-game controller disconnects, reconnections, press-to-reassign recovery, and single-player keyboard/touch fallback.
//
// Updating the policy while players are already disconnected immediately applies fallback and pause transitions to match the new policy.
func (m *Players) SetDisconnectPolicy(policy DisconnectPolicy) {
	m.policy = policy
	for _, p := range m.players {
		if !p.gpDisconnected {
			continue
		}
		if policy.FallbackToKeyboardTouch && !p.fallbackActive {
			m.activateFallback(p)
		} else if !policy.FallbackToKeyboardTouch && p.fallbackActive {
			m.deactivateFallback(p)
		}
	}
	m.syncDisconnectPause()
	m.syncWaitRAF()
}

// DisconnectPolicy returns the disconnect and reconnect policy currently configured on m.
func (m *Players) DisconnectPolicy() DisconnectPolicy {
	return m.policy
}

// DisconnectedPlayers returns the player slots currently awaiting controller reconnect, in ascending slot index order, or nil if no player is disconnected.
func (m *Players) DisconnectedPlayers() []*Player {
	var out []*Player
	for _, p := range m.players {
		if p.gpDisconnected {
			out = append(out, p)
		}
	}
	return out
}

// PausedByDisconnect reports whether m has paused gameplay via DisconnectPolicy.PauseOnDisconnect because at least one disconnected player is awaiting a controller without fallback.
func (m *Players) PausedByDisconnect() bool {
	return m.pausedByDisconnect
}

// PollGamepads reads the live controllers once from the scope passed to WatchGamepads, processes any controller disconnect, reconnect, or press-to-reassign transitions, and updates each player's controller state without stepping Actions.
//
// When WatchGamepads has a live scope and a DisconnectPolicy is configured, Players automatically polls on display frames while any player is awaiting reconnect so a paused game.Loop resumes once a controller reconnects or presses a button. Call PollGamepads directly if you drive polling outside game.Loop.
func (m *Players) PollGamepads() {
	if !m.readLiveGamepads() {
		return
	}
	m.reconcileGamepads(true)
	m.flushDisconnectEvents()
}

func (m *Players) readLiveGamepads() bool {
	cx := m.gpScope
	if cx == nil {
		return false
	}
	if cx.Disposed() {
		m.gpWatchGen++
		m.stopWaitRAF()
		m.gpScope = nil
		m.gpPads = m.gpPads[:0]
		m.prevPadButtons = m.prevPadButtons[:0]
		m.dispatchGamepads(false)
		return false
	}
	m.setGamepads(core.Untracked(cx, func() []gamepad.Pad { return gamepad.Pads(cx) }))
	return true
}

func (m *Players) reconcileGamepads(syncActions bool) {
	anyDisconnected := false

	// Pass 1: detect newly disconnected assigned controllers.
	for _, p := range m.players {
		if !p.hasGamepad {
			continue
		}
		if m.hasPadID(p.gamepadID) {
			continue
		}
		if p.gpWasConnected {
			p.gpWasConnected = false
			p.gpDisconnected = true
			p.disconnectedPadID = p.gamepadID
			fallback := false
			if m.policy.FallbackToKeyboardTouch {
				m.activateFallback(p)
				fallback = true
			}
			m.pendingDisconnects = append(m.pendingDisconnects, DisconnectEvent{
				PlayerIndex:       p.index,
				Player:            p,
				PreviousGamepadID: p.disconnectedPadID,
				FallbackActive:    fallback,
				ShouldPause:       m.policy.PauseOnDisconnect && !fallback,
			})
		}
		if p.gpDisconnected {
			anyDisconnected = true
		}
	}

	// Pass 2: detect initial connections and same-controller reconnects.
	for _, p := range m.players {
		if !p.hasGamepad {
			continue
		}
		if !m.hasPadID(p.gamepadID) {
			continue
		}
		if p.gpDisconnected {
			prevID := p.disconnectedPadID
			p.gpDisconnected = false
			p.gpWasConnected = true
			p.disconnectedPadID = 0
			if p.fallbackActive {
				m.deactivateFallback(p)
			}
			m.pendingReconnects = append(m.pendingReconnects, ReconnectEvent{
				PlayerIndex:       p.index,
				Player:            p,
				PreviousGamepadID: prevID,
				NewGamepadID:      p.gamepadID,
				Reassigned:        prevID != p.gamepadID,
			})
		} else if !p.gpWasConnected {
			p.gpWasConnected = true
		}
	}

	// Pass 3: reassign disconnected slots when an unassigned controller newly presses any button.
	if anyDisconnected && m.policy.AllowReassignOnPress {
		for i := range m.gpPads {
			pad := &m.gpPads[i]
			if m.PlayerByGamepad(pad.ID) != nil {
				continue
			}
			nowMask := pressedButtons(pad)
			if nowMask == 0 {
				continue
			}
			prevMask := m.prevButtonsForID(pad.ID)
			if nowMask&^prevMask == 0 {
				continue
			}
			slot := m.nextDisconnectedPlayer()
			if slot == nil {
				break
			}
			prevID := slot.disconnectedPadID
			slot.gamepadID = pad.ID
			slot.gpDisconnected = false
			slot.gpWasConnected = true
			slot.disconnectedPadID = 0
			if slot.fallbackActive {
				m.deactivateFallback(slot)
			}
			slot.actions.SelectGamepad(pad.ID)
			m.pendingReconnects = append(m.pendingReconnects, ReconnectEvent{
				PlayerIndex:       slot.index,
				Player:            slot,
				PreviousGamepadID: prevID,
				NewGamepadID:      pad.ID,
				Reassigned:        prevID != pad.ID,
			})
		}
	}

	m.recordPadButtons()
	m.dispatchGamepads(syncActions)
}

func (m *Players) flushDisconnectEvents() {
	if len(m.pendingDisconnects) == 0 && len(m.pendingReconnects) == 0 {
		m.syncDisconnectPause()
		m.syncWaitRAF()
		return
	}

	var disconnects []DisconnectEvent
	if len(m.pendingDisconnects) > 0 {
		disconnects = slices.Clone(m.pendingDisconnects)
		m.pendingDisconnects = m.pendingDisconnects[:0]
	}
	var reconnects []ReconnectEvent
	if len(m.pendingReconnects) > 0 {
		reconnects = slices.Clone(m.pendingReconnects)
		m.pendingReconnects = m.pendingReconnects[:0]
		stillPaused := m.wantDisconnectPause()
		for i := range reconnects {
			reconnects[i].ShouldPause = stillPaused
		}
	}

	for _, ev := range disconnects {
		if m.policy.OnDisconnect != nil {
			m.policy.OnDisconnect(ev)
		}
	}
	for _, ev := range reconnects {
		if m.policy.OnReconnect != nil {
			m.policy.OnReconnect(ev)
		}
	}

	m.syncDisconnectPause()
	m.syncWaitRAF()
}

func (m *Players) wantDisconnectPause() bool {
	if !m.policy.PauseOnDisconnect {
		return false
	}
	for _, p := range m.players {
		if p.gpDisconnected && !p.fallbackActive {
			return true
		}
	}
	return false
}

func (m *Players) syncDisconnectPause() {
	want := m.wantDisconnectPause()
	if want == m.pausedByDisconnect {
		return
	}
	m.pausedByDisconnect = want
	if m.policy.OnPause != nil {
		m.policy.OnPause(want)
	}
}

func (m *Players) hasDisconnectedPlayer() bool {
	for _, p := range m.players {
		if p.gpDisconnected {
			return true
		}
	}
	return false
}

func (m *Players) nextDisconnectedPlayer() *Player {
	for _, p := range m.players {
		if p.gpDisconnected {
			return p
		}
	}
	return nil
}

func (m *Players) needsDisconnectMonitor() bool {
	return m.pausedByDisconnect ||
		m.policy.PauseOnDisconnect ||
		m.policy.AllowReassignOnPress ||
		m.policy.FallbackToKeyboardTouch ||
		m.policy.OnReconnect != nil
}

func (m *Players) syncWaitRAF() {
	want := m.gpScope != nil && !m.gpScope.Disposed() && m.hasDisconnectedPlayer() && m.needsDisconnectMonitor()
	if !want {
		m.stopWaitRAF()
		return
	}
	if m.cancelWaitRAF != nil {
		return
	}
	cx := m.gpScope
	cx.Run(func() {
		m.cancelWaitRAF = cx.RequestAnimationFrame(m.waitRAFFrame)
	})
}

func (m *Players) stopWaitRAF() {
	if m.cancelWaitRAF != nil {
		cancel := m.cancelWaitRAF
		m.cancelWaitRAF = nil
		cancel()
	}
}

func (m *Players) waitRAFFrame(time.Time) {
	m.cancelWaitRAF = nil
	if m.gpScope == nil || m.gpScope.Disposed() || !m.hasDisconnectedPlayer() || !m.needsDisconnectMonitor() {
		return
	}
	m.PollGamepads()
	m.syncWaitRAF()
}

func (m *Players) activateFallback(p *Player) {
	if p.fallbackActive {
		return
	}
	p.fallbackActive = true
	p.preFallbackKeyboard = p.usesKeyboard
	p.preFallbackPointer = p.usesPointer
	if !p.usesKeyboard {
		p.usesKeyboard = true
		if m.stopKeys != nil {
			p.actions.setWatchedKeys(m.watchedKeys, m.watchedCodes)
		}
	}
	if !p.usesPointer {
		p.usesPointer = true
	}
}

func (m *Players) deactivateFallback(p *Player) {
	if !p.fallbackActive {
		return
	}
	restoreKeyboard := p.preFallbackKeyboard
	restorePointer := p.preFallbackPointer
	p.fallbackActive = false
	p.preFallbackKeyboard = false
	p.preFallbackPointer = false
	if !restoreKeyboard && p.usesKeyboard {
		p.usesKeyboard = false
		p.actions.clearKeyboardInput()
	}
	if !restorePointer && p.usesPointer {
		p.usesPointer = false
		p.actions.clearPointerInput()
	}
}

func (m *Players) hasPadID(id int) bool {
	for i := range m.gpPads {
		if m.gpPads[i].ID == id {
			return true
		}
		if m.gpPads[i].ID > id {
			return false
		}
	}
	return false
}

func (m *Players) prevButtonsForID(id int) uint32 {
	for i := range m.prevPadButtons {
		if m.prevPadButtons[i].id == id {
			return m.prevPadButtons[i].buttons
		}
		if m.prevPadButtons[i].id > id {
			return 0
		}
	}
	return 0
}

func (m *Players) recordPadButtons() {
	m.prevPadButtons = m.prevPadButtons[:0]
	for i := range m.gpPads {
		m.prevPadButtons = append(m.prevPadButtons, padButtonState{
			id:      m.gpPads[i].ID,
			buttons: pressedButtons(&m.gpPads[i]),
		})
	}
}
