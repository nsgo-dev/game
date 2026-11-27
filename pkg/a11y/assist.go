package a11y

import (
	"math"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

const (
	// MinGameSpeed is the slowest allowed Assist.GameSpeed multiplier.
	MinGameSpeed = 0.25
	// MaxGameSpeed is the fastest allowed Assist.GameSpeed multiplier.
	MaxGameSpeed = 1.0
	// MinTextScale is the minimum allowed Assist.TextScale multiplier.
	MinTextScale float32 = 1.0
	// MaxTextScale is the maximum allowed Assist.TextScale multiplier.
	MaxTextScale float32 = 2.0
)

// AssistOptions is a plain snapshot of every assist option managed by Assist.
type AssistOptions struct {
	GameSpeed          float64
	ReduceMotion       bool
	Invincibility      bool
	AutoAim            bool
	HoldToToggle       bool
	HighContrast       bool
	CaptionsEnabled    bool
	SFXCaptionsEnabled bool
	ColorVision        ColorVision
	TextScale          float32
}

// Assist stores and persists a player's accessibility and gameplay-assist
// preferences in core.Storage under a key prefix. Every getter reads through
// core.Storage (and core.Env where appropriate), so calling a getter inside a
// reactive NSGo computation automatically subscribes to updates.
type Assist struct {
	cx    *core.Scope
	store core.Storage
	key   string
}

// NewAssist returns an Assist controller backed by cx's core.Storage under
// keys prefixed with key (defaulting to "a11y.assist" when empty).
func NewAssist(cx *core.Scope, key string) *Assist {
	if key == "" {
		key = "a11y.assist"
	}
	return &Assist{
		cx:    cx,
		store: cx.Storage(),
		key:   key,
	}
}

func (a *Assist) k(suffix string) string { return a.key + "." + suffix }

// GameSpeed returns the slow-motion multiplier in [0.25, 1.0], defaulting to 1.0.
func (a *Assist) GameSpeed() float64 {
	raw, ok := a.store.GetItem(a.k("gameSpeed"))
	if !ok {
		return MaxGameSpeed
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return MaxGameSpeed
	}
	return clampFloat64(v, MinGameSpeed, MaxGameSpeed)
}

// SetGameSpeed clamps v to [0.25, 1.0] and persists it.
func (a *Assist) SetGameSpeed(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		v = MaxGameSpeed
	}
	v = clampFloat64(v, MinGameSpeed, MaxGameSpeed)
	return a.store.SetItem(a.k("gameSpeed"), strconv.FormatFloat(v, 'f', -1, 64))
}

// ScaleDT scales a simulation step dt by GameSpeed. Non-positive durations
// return 0, and a speed of 1.0 returns dt unchanged.
func (a *Assist) ScaleDT(dt time.Duration) time.Duration {
	if dt <= 0 {
		return 0
	}
	speed := a.GameSpeed()
	if speed == 1.0 {
		return dt
	}
	return time.Duration(math.Round(float64(dt) * speed))
}

// ReduceMotion reports whether screen shake, flashes, and non-essential motion
// should be disabled. When unset in storage, it follows cx.Env().ReduceMotion().
func (a *Assist) ReduceMotion() bool {
	if raw, ok := a.store.GetItem(a.k("reduceMotion")); ok {
		if v, err := strconv.ParseBool(raw); err == nil {
			return v
		}
	}
	if a.cx != nil {
		return a.cx.Env().ReduceMotion()
	}
	return false
}

// SetReduceMotion persists an explicit reduced-motion preference.
func (a *Assist) SetReduceMotion(v bool) error {
	return a.setBool("reduceMotion", v)
}

// Invincibility reports whether player damage is disabled.
func (a *Assist) Invincibility() bool { return a.getBool("invincibility", false) }

// SetInvincibility persists the invincibility assist option.
func (a *Assist) SetInvincibility(v bool) error { return a.setBool("invincibility", v) }

// AutoAim reports whether aim assistance is enabled.
func (a *Assist) AutoAim() bool { return a.getBool("autoAim", false) }

// SetAutoAim persists the auto-aim assist option.
func (a *Assist) SetAutoAim(v bool) error { return a.setBool("autoAim", v) }

// HoldToToggle reports whether held inputs should act as toggles.
func (a *Assist) HoldToToggle() bool { return a.getBool("holdToToggle", false) }

// SetHoldToToggle persists the hold-to-toggle assist option.
func (a *Assist) SetHoldToToggle(v bool) error { return a.setBool("holdToToggle", v) }

// HighContrast reports whether high-contrast visuals are requested. When unset
// in storage, it follows cx.Env().IncreaseContrast().
func (a *Assist) HighContrast() bool {
	if raw, ok := a.store.GetItem(a.k("highContrast")); ok {
		if v, err := strconv.ParseBool(raw); err == nil {
			return v
		}
	}
	if a.cx != nil {
		return a.cx.Env().IncreaseContrast()
	}
	return false
}

// SetHighContrast persists an explicit high-contrast preference.
func (a *Assist) SetHighContrast(v bool) error { return a.setBool("highContrast", v) }

// CaptionsEnabled reports whether dialogue and music captions are enabled.
func (a *Assist) CaptionsEnabled() bool { return a.getBool("captionsEnabled", false) }

// SetCaptionsEnabled persists whether captions are enabled.
func (a *Assist) SetCaptionsEnabled(v bool) error { return a.setBool("captionsEnabled", v) }

// SFXCaptionsEnabled reports whether sound-effect captions are enabled.
func (a *Assist) SFXCaptionsEnabled() bool { return a.getBool("sfxCaptionsEnabled", false) }

// SetSFXCaptionsEnabled persists whether sound-effect captions are enabled.
func (a *Assist) SetSFXCaptionsEnabled(v bool) error { return a.setBool("sfxCaptionsEnabled", v) }

// ColorVision returns the selected color-vision mode, defaulting to VisionNormal.
func (a *Assist) ColorVision() ColorVision {
	raw, ok := a.store.GetItem(a.k("colorVision"))
	if !ok {
		return VisionNormal
	}
	if mode, ok := ParseColorVision(raw); ok {
		return mode
	}
	return VisionNormal
}

// SetColorVision persists the selected color-vision mode.
func (a *Assist) SetColorVision(v ColorVision) error {
	return a.store.SetItem(a.k("colorVision"), v.String())
}

// TextScale returns the UI/caption text scale multiplier in [1.0, 2.0]. When
// unset in storage, it adopts cx.Env().FontScale() clamped to [1.0, 2.0].
func (a *Assist) TextScale() float32 {
	raw, ok := a.store.GetItem(a.k("textScale"))
	if ok {
		if v, err := strconv.ParseFloat(raw, 32); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
			return clampFloat32(float32(v), MinTextScale, MaxTextScale)
		}
	}
	if a.cx != nil {
		if fs := a.cx.Env().FontScale(); fs > 0 {
			return clampFloat32(fs, MinTextScale, MaxTextScale)
		}
	}
	return MinTextScale
}

// SetTextScale clamps v to [1.0, 2.0] and persists it.
func (a *Assist) SetTextScale(v float32) error {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		v = MinTextScale
	}
	v = clampFloat32(v, MinTextScale, MaxTextScale)
	return a.store.SetItem(a.k("textScale"), strconv.FormatFloat(float64(v), 'f', -1, 32))
}

// Palette returns the active Palette for the player's current settings,
// preferring VisionHighContrast when HighContrast is enabled.
func (a *Assist) Palette() Palette {
	if a.HighContrast() {
		return PaletteFor(VisionHighContrast)
	}
	return PaletteFor(a.ColorVision())
}

// Snapshot returns all current assist option values.
func (a *Assist) Snapshot() AssistOptions {
	return AssistOptions{
		GameSpeed:          a.GameSpeed(),
		ReduceMotion:       a.ReduceMotion(),
		Invincibility:      a.Invincibility(),
		AutoAim:            a.AutoAim(),
		HoldToToggle:       a.HoldToToggle(),
		HighContrast:       a.HighContrast(),
		CaptionsEnabled:    a.CaptionsEnabled(),
		SFXCaptionsEnabled: a.SFXCaptionsEnabled(),
		ColorVision:        a.ColorVision(),
		TextScale:          a.TextScale(),
	}
}

// Apply persists every option in opts.
func (a *Assist) Apply(opts AssistOptions) error {
	if err := a.SetGameSpeed(opts.GameSpeed); err != nil {
		return err
	}
	if err := a.SetReduceMotion(opts.ReduceMotion); err != nil {
		return err
	}
	if err := a.SetInvincibility(opts.Invincibility); err != nil {
		return err
	}
	if err := a.SetAutoAim(opts.AutoAim); err != nil {
		return err
	}
	if err := a.SetHoldToToggle(opts.HoldToToggle); err != nil {
		return err
	}
	if err := a.SetHighContrast(opts.HighContrast); err != nil {
		return err
	}
	if err := a.SetCaptionsEnabled(opts.CaptionsEnabled); err != nil {
		return err
	}
	if err := a.SetSFXCaptionsEnabled(opts.SFXCaptionsEnabled); err != nil {
		return err
	}
	if err := a.SetColorVision(opts.ColorVision); err != nil {
		return err
	}
	return a.SetTextScale(opts.TextScale)
}

// Reset removes all stored assist preferences so defaults and OS environment
// settings take effect again.
func (a *Assist) Reset() {
	for _, suffix := range []string{
		"gameSpeed", "reduceMotion", "invincibility", "autoAim",
		"holdToToggle", "highContrast", "captionsEnabled",
		"sfxCaptionsEnabled", "colorVision", "textScale",
	} {
		a.store.RemoveItem(a.k(suffix))
	}
}

func (a *Assist) getBool(suffix string, def bool) bool {
	raw, ok := a.store.GetItem(a.k(suffix))
	if !ok {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return v
}

func (a *Assist) setBool(suffix string, v bool) error {
	return a.store.SetItem(a.k(suffix), strconv.FormatBool(v))
}

func clampFloat64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
