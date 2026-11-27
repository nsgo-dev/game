package screen

import (
	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/save"
	"github.com/nsgo-dev/nsgo/core"
)

// DefaultLowPowerCap is the frame cap Video drops to while the system is constrained, when Video.LowPowerCap is 0.
const DefaultLowPowerCap = 30

// setFullScreen sends the window's full-screen request. Tests replace it because nsgotest's window request log has no entry for driver.WindowSetFullScreen and keeps no display ID, so the real request cannot be observed there; remove this seam once nsgotest records both (nsgo-dev/nsgo#2258).
var setFullScreen = func(cx *core.Scope, on bool, displayID string) {
	if displayID == "" {
		cx.SetFullScreen(on)
		return
	}
	cx.SetFullScreen(on, displayID)
}

// Video applies a player's video settings (save.DisplaySettings) to a scope's window and a Loop: windowed or full screen on the chosen display, a cap on how many frames Loop draws a second, and, with AutoDropLowPower, a lower cap while the system is in low-power mode or under serious thermal pressure. It ships no UI: a settings screen edits save.Settings, saves it, and applies it, and the game applies the saved settings at start with save.DefaultSettingsStore.LoadAndApply and Video.Apply as save.SettingsTargets.ApplyDisplay.
//
// Video asks the window to change full screen or display only when the settings change them, or on the first Apply when they differ from the window, so applying other settings does not undo a player leaving full screen with the system's own controls. On the web, a saved full-screen setting applied at start is deferred until the next user press (via Attach or ApplyPending) so the browser does not reject it outside a user gesture. While Run is active, Video also watches Env.FullScreen and updates its held settings (and calls OnWindowChanged) when the window enters or leaves full screen outside a Video request.
type Video struct {
	// Loop receives the frame cap in effect as its DrawRate; nil caps nothing.
	Loop *game.Loop
	// LowPowerCap is the frame cap while the system is constrained and AutoDropLowPower is on, DefaultLowPowerCap when 0 or less. A lower FrameCap stays in effect.
	LowPowerCap int
	// OnWindowChanged, when set, is called when the window enters or leaves full screen outside a Video request (such as the player pressing Escape or using browser or system controls). Video updates the settings returned by Settings before calling it.
	OnWindowChanged func(save.DisplaySettings)

	cx       *core.Scope
	stop     func()
	settings save.DisplaySettings
	// set says Apply has been called; sent says applyWindow has observed or sent a full-screen state, last holding it.
	set, sent   bool
	lastFull    bool
	lastDisplay string
	// pending says a web full-screen request from startup Apply is waiting for the next user press (ApplyPending).
	pending bool
	// windowSeen and windowFull track Env.FullScreen so Run's effect detects external transitions.
	windowSeen bool
	windowFull bool
	// awaitingFull says Video sent a full-screen request expecting desiredFull and has not yet seen Env.FullScreen reach it.
	awaitingFull bool
	desiredFull  bool
	// constrained holds whether the system is constrained while Run is active; a signal so that Dropped and FrameCap subscribe the computations reading them. Nil before the first Run.
	constrained *core.Signal[bool]
}

// Run starts applying v's settings on cx's window and watching the system's power and full-screen state, until the current owner is disposed or stop is called; either one releases the lowered cap and stops sending window requests. Settings applied before Run take effect when it starts. Calling Run again stops the earlier run first.
func (v *Video) Run(cx *core.Scope) (stop func()) {
	if v.stop != nil {
		v.stop()
	}
	v.cx = cx
	if v.constrained == nil {
		v.constrained = core.NewSignal(cx, false)
	}
	cx.Untrack(func() {
		v.windowFull = cx.Env().FullScreen()
		v.windowSeen = true
	})
	if v.set {
		v.applyWindow()
	}
	v.updateCap()
	stopPower := cx.Effect(func() {
		env := cx.Env()
		v.constrained.Set(Constrained(env.LowPowerMode(), env.ThermalState()))
		v.updateCap()
	})
	stopWindow := cx.Effect(func() {
		v.syncWindow(cx.Env().FullScreen())
	})
	stopped := false
	v.stop = func() {
		if stopped {
			return
		}
		stopped = true
		stopPower()
		stopWindow()
		v.cx = nil
		v.stop = nil
		// The player may have changed the window with the system's controls while stopped, so the next Run compares its settings with the window afresh.
		v.sent = false
		v.pending = false
		v.windowSeen = false
		v.awaitingFull = false
		v.constrained.Set(false)
		v.updateCap()
	}
	// Disposal of the owner releases the run the same way stop does.
	cx.Cleanup(v.stop)
	return v.stop
}

// Apply validates s and applies it: it moves the window into or out of full screen on the chosen display when that changed, and sets Loop's DrawRate to the frame cap in effect. On the web, entering full screen on the initial Apply is deferred to the next user press (Attach or ApplyPending). Its signature matches save.SettingsTargets.ApplyDisplay; it always returns nil.
func (v *Video) Apply(s save.DisplaySettings) error {
	full := save.Settings{Display: s}
	full.Validate()
	v.settings = full.Display
	v.set = true
	if v.cx != nil {
		v.applyWindow()
	}
	v.updateCap()
	return nil
}

// ApplyPending sends a web full-screen request deferred from startup Apply. Call it inside a press, touch, or key handler (or wire a view with Attach) so the browser receives the request during a user gesture. It does nothing when no request is pending or Run is not active.
func (v *Video) ApplyPending() {
	if !v.pending || v.cx == nil {
		return
	}
	v.pending = false
	if !v.settings.Fullscreen {
		return
	}
	var displays []core.DisplayInfo
	var windowFull bool
	var os core.OS
	v.cx.Untrack(func() {
		env := v.cx.Env()
		displays = env.Displays()
		windowFull = env.FullScreen()
		os = env.OS()
	})
	display := DisplayID(v.settings, displays)
	if windowFull && (display == "" || os == core.OSWeb) {
		v.lastFull, v.lastDisplay, v.sent = true, display, true
		v.awaitingFull = false
		return
	}
	v.requestFullScreen(true, display, windowFull)
}

// PendingFullScreen reports whether a web full-screen request deferred from startup Apply is waiting for a user press.
func (v *Video) PendingFullScreen() bool { return v.pending }

// Attach wires v.ApplyPending onto view's press, touch, and key-down handlers, chaining any callbacks already set on view. On the web, attaching the game's input view sends a deferred startup full-screen request on the player's first click, tap, or key press.
func (v *Video) Attach(view *core.View) {
	if view == nil {
		return
	}
	if prev := view.OnPressDown; prev != nil {
		view.OnPressDown = func(ev core.PressEvent) { v.ApplyPending(); prev(ev) }
	} else {
		view.OnPressDown = func(core.PressEvent) { v.ApplyPending() }
	}
	if prev := view.OnPress; prev != nil {
		view.OnPress = func(ev core.PressEvent) { v.ApplyPending(); prev(ev) }
	} else {
		view.OnPress = func(core.PressEvent) { v.ApplyPending() }
	}
	if prev := view.OnTouchCapture; prev != nil {
		view.OnTouchCapture = func(ev core.TouchEvent) {
			if ev.Phase == core.TouchStart || ev.Phase == core.TouchEnd {
				v.ApplyPending()
			}
			prev(ev)
		}
	} else {
		view.OnTouchCapture = func(ev core.TouchEvent) {
			if ev.Phase == core.TouchStart || ev.Phase == core.TouchEnd {
				v.ApplyPending()
			}
		}
	}
	if prev := view.OnTouch; prev != nil {
		view.OnTouch = func(ev core.TouchEvent) {
			if ev.Phase == core.TouchStart || ev.Phase == core.TouchEnd {
				v.ApplyPending()
			}
			prev(ev)
		}
	} else {
		view.OnTouch = func(ev core.TouchEvent) {
			if ev.Phase == core.TouchStart || ev.Phase == core.TouchEnd {
				v.ApplyPending()
			}
		}
	}
	if prev := view.OnKeyDown; prev != nil {
		view.OnKeyDown = func(ev core.KeyEvent) bool {
			if !ev.Repeat {
				v.ApplyPending()
			}
			return prev(ev)
		}
	} else {
		view.OnKeyDown = func(ev core.KeyEvent) bool {
			if !ev.Repeat {
				v.ApplyPending()
			}
			return false
		}
	}
}

// Settings returns the settings last applied, updated when the window enters or leaves full screen outside a Video request.
func (v *Video) Settings() save.DisplaySettings { return v.settings }

// Dropped reports whether the frame cap is lowered because the system is constrained and AutoDropLowPower is on. Read inside a computation while Run is active, it subscribes to the system's power state; settings changes arrive through Apply, which the caller makes itself.
func (v *Video) Dropped() bool {
	// Read the signal first, so a computation subscribes even while AutoDropLowPower is off.
	constrained := v.constrained != nil && v.constrained.Get()
	return constrained && v.settings.AutoDropLowPower
}

// FrameCap returns the frame cap in effect, 0 for uncapped: the settings' FrameCap, lowered to LowPowerCap while Dropped. It subscribes as Dropped does.
func (v *Video) FrameCap() int { return v.frameCap(v.Dropped()) }

func (v *Video) frameCap(dropped bool) int {
	limit := v.settings.FrameCap
	if dropped {
		drop := v.LowPowerCap
		if drop <= 0 {
			drop = DefaultLowPowerCap
		}
		if limit <= 0 || drop < limit {
			limit = drop
		}
	}
	return limit
}

// DisplayID returns the ID of the display s chooses among displays: DisplayID when it is connected, else the display at DisplayIndex, else "" for the window's current display.
func DisplayID(s save.DisplaySettings, displays []core.DisplayInfo) string {
	if s.DisplayID != "" {
		for _, d := range displays {
			if d.ID == s.DisplayID {
				return d.ID
			}
		}
	}
	if s.DisplayIndex >= 0 && s.DisplayIndex < len(displays) {
		return displays[s.DisplayIndex].ID
	}
	return ""
}

// Constrained reports whether a system in low-power mode lowPower with thermal pressure thermal should draw fewer frames: low-power mode on, or thermal pressure serious or critical.
func Constrained(lowPower core.LowPowerMode, thermal core.ThermalState) bool {
	return lowPower == core.LowPowerOn || thermal >= core.ThermalSerious
}

// CanFullScreen reports whether the platform in env can enter full screen: macOS, Windows, Linux, and non-phone web browsers return true; iOS, Android, and phone web browsers (such as iPhone Safari, until NSGo exposes a capability in nsgo-dev/nsgo#2690) return false. A nil env or an unset OS returns true.
func CanFullScreen(env *core.Env) bool {
	if env == nil {
		return true
	}
	switch env.OS() {
	case core.OSIOS, core.OSAndroid:
		return false
	case core.OSWeb:
		return env.Idiom() != core.IdiomPhone
	default:
		return true
	}
}

// applyWindow requests the settings' full-screen state when it differs from the last one requested, or, before any request, from the window's. On the web, entering full screen on the initial apply is deferred to ApplyPending so the browser does not reject it outside a user gesture.
func (v *Video) applyWindow() {
	// Apply may run inside a component or effect; reading the window here must not make it rerun on display changes.
	var displays []core.DisplayInfo
	var windowFull bool
	var os core.OS
	v.cx.Untrack(func() {
		env := v.cx.Env()
		displays = env.Displays()
		windowFull = env.FullScreen()
		os = env.OS()
	})
	full := v.settings.Fullscreen
	display := ""
	if full {
		display = DisplayID(v.settings, displays)
	}
	if !v.sent {
		if os == core.OSWeb && full && !windowFull {
			v.pending = true
			v.lastFull, v.lastDisplay, v.sent = false, "", true
			return
		}
		if full == windowFull && (display == "" || os == core.OSWeb) {
			v.pending = false
			v.lastFull, v.lastDisplay, v.sent = full, display, true
			return
		}
	} else {
		v.pending = false
		if full == v.lastFull && display == v.lastDisplay {
			return
		}
	}
	v.requestFullScreen(full, display, windowFull)
}

func (v *Video) requestFullScreen(full bool, display string, windowFull bool) {
	setFullScreen(v.cx, full, display)
	v.lastFull, v.lastDisplay, v.sent = full, display, true
	v.pending = false
	if full != windowFull {
		v.awaitingFull = true
		v.desiredFull = full
	} else {
		v.awaitingFull = false
	}
}

// syncWindow follows Env.FullScreen changes while Run is active, updating settings and notifying OnWindowChanged when the window changed outside a Video request.
func (v *Video) syncWindow(full bool) {
	if !v.windowSeen {
		v.windowSeen = true
		v.windowFull = full
		return
	}
	if full == v.windowFull {
		return
	}
	v.windowFull = full
	if v.awaitingFull && full == v.desiredFull {
		v.awaitingFull = false
		return
	}
	v.awaitingFull = false
	v.pending = false
	v.settings.Fullscreen = full
	if v.sent {
		v.lastFull = full
		v.lastDisplay = ""
	}
	if cb := v.OnWindowChanged; cb != nil {
		snap := v.settings
		v.cx.Untrack(func() { cb(snap) })
	}
}

// updateCap sets Loop's DrawRate without subscribing the running computation, which may be Run's own effect writing the signal it would read.
func (v *Video) updateCap() {
	if v.Loop == nil {
		return
	}
	dropped := v.settings.AutoDropLowPower && v.constrained != nil && v.constrained.GetUntracked()
	v.Loop.DrawRate = v.frameCap(dropped)
}
