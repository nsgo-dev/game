package screen

import (
	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/save"
	"github.com/nsgo-dev/nsgo/core"
)

// DefaultLowPowerCap is the frame cap Video drops to while the system is constrained, when Video.LowPowerCap is 0.
const DefaultLowPowerCap = 30

// setFullScreen sends the window's full-screen request; tests replace it because nsgotest records neither the request's action nor its display ID.
var setFullScreen = func(cx *core.Scope, on bool, displayID string) {
	if displayID == "" {
		cx.SetFullScreen(on)
		return
	}
	cx.SetFullScreen(on, displayID)
}

// Video applies a player's video settings (save.DisplaySettings) to a scope's window and a Loop: windowed or borderless full screen on the chosen display, a cap on how many frames Loop draws a second, and, with AutoDropLowPower, a lower cap while the system is in low-power mode or under serious thermal pressure. It ships no UI: a settings screen edits save.Settings, saves it, and applies it, and the game applies the saved settings at start with save.DefaultSettingsStore.LoadAndApply and Video.Apply as save.SettingsTargets.ApplyDisplay.
//
// Video asks the window to change full screen or display only when the settings change them, or on the first Apply when they differ from the window, so applying other settings does not undo a player leaving full screen with the system's own controls.
type Video struct {
	// Loop receives the frame cap in effect as its DrawRate; nil caps nothing.
	Loop *game.Loop
	// LowPowerCap is the frame cap while the system is constrained and AutoDropLowPower is on, DefaultLowPowerCap when 0 or less. A lower FrameCap stays in effect.
	LowPowerCap int

	cx       *core.Scope
	stop     func()
	settings save.DisplaySettings
	// set says Apply has been called; sent says a full-screen state has been requested, last holding it.
	set, sent   bool
	lastFull    bool
	lastDisplay string
	constrained bool
}

// Run starts applying v's settings on cx's window and watching the system's power state, until cx is disposed or stop is called. Settings applied before Run take effect when it starts.
func (v *Video) Run(cx *core.Scope) (stop func()) {
	if v.stop != nil {
		v.stop()
	}
	v.cx = cx
	if v.set {
		v.applyWindow()
	}
	v.updateCap()
	stopEffect := cx.Effect(func() {
		env := cx.Env()
		v.constrained = Constrained(env.LowPowerMode(), env.ThermalState())
		v.updateCap()
	})
	v.stop = func() {
		stopEffect()
		v.cx = nil
		v.constrained = false
		v.stop = nil
		v.updateCap()
	}
	return v.stop
}

// Apply validates s and applies it: it moves the window into or out of full screen on the chosen display when that changed, and sets Loop's DrawRate to the frame cap in effect. Its signature matches save.SettingsTargets.ApplyDisplay; it always returns nil.
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

// Settings returns the settings last applied.
func (v *Video) Settings() save.DisplaySettings { return v.settings }

// Dropped reports whether the frame cap is lowered because the system is constrained and AutoDropLowPower is on.
func (v *Video) Dropped() bool { return v.constrained && v.settings.AutoDropLowPower }

// FrameCap returns the frame cap in effect, 0 for uncapped: the settings' FrameCap, lowered to LowPowerCap while Dropped.
func (v *Video) FrameCap() int {
	limit := v.settings.FrameCap
	if v.Dropped() {
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

// applyWindow requests the settings' full-screen state when it differs from the last one requested, or, before any request, from the window's.
func (v *Video) applyWindow() {
	// Apply may run inside a component or effect; reading the window here must not make it rerun on display changes.
	var displays []core.DisplayInfo
	var windowFull bool
	v.cx.Untrack(func() {
		displays = v.cx.Env().Displays()
		windowFull = v.cx.Env().FullScreen()
	})
	full := v.settings.Fullscreen
	display := ""
	if full {
		display = DisplayID(v.settings, displays)
	}
	if v.sent {
		if full == v.lastFull && display == v.lastDisplay {
			return
		}
	} else if !full && !windowFull {
		v.lastFull, v.lastDisplay, v.sent = false, "", true
		return
	}
	setFullScreen(v.cx, full, display)
	v.lastFull, v.lastDisplay, v.sent = full, display, true
}

func (v *Video) updateCap() {
	if v.Loop != nil {
		v.Loop.DrawRate = v.FrameCap()
	}
}
