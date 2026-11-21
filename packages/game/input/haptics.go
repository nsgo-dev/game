package input

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

const defaultRumblePulseInterval = 50 * time.Millisecond

// RumbleStep is one segment of a rumble or haptic pattern.
// LowFreq and HighFreq are motor strengths in [0, 1]; setting both to 0 inserts a silent pause for Duration.
type RumbleStep struct {
	LowFreq  float32
	HighFreq float32
	Duration time.Duration
}

// Haptics plays tactile feedback through core.Scope.PlayHaptic on iOS and Android, runs timed
// rumble and multi-step patterns on Update(dt), and forwards motor updates to an optional
// GamepadRumble sink (a seam for gamepad rumble hardware).
//
// All output is silenced while Enabled is false or while the scope's window is in
// core.LifecycleBackground. Any active rumble stops automatically when disabled, backgrounded,
// or when the owning scope is disposed.
//
// Haptics' methods are called on the engine's goroutine, as core.Scope and game.Loop.Update are.
type Haptics struct {
	// Enabled controls whether any haptic or rumble feedback is emitted (true by default).
	// Set to false for accessibility or player preferences.
	Enabled bool

	// PulseInterval is how frequently mobile haptic pulses repeat during a continuous Rumble
	// (50 ms when <= 0).
	PulseInterval time.Duration

	// GamepadRumble, when set, receives motor strength updates for external gamepad rumble
	// backends (seam for nsgo#1049). Called with (lowFreq, highFreq, duration) in [0, 1]
	// when a rumble segment begins, and with (0, 0, 0) when rumble finishes, stops, is disabled,
	// or pauses in background.
	GamepadRumble func(lowFreq, highFreq float32, duration time.Duration)

	// OnHaptic, when set, is called each time a haptic is dispatched to core.Scope.PlayHaptic.
	// Useful for visual accessibility cues, telemetry, or tests.
	OnHaptic func(core.Haptic)

	cx         *core.Scope
	background bool

	steps     []RumbleStep
	stepIdx   int
	remaining time.Duration
	pulseAcc  time.Duration
	rumbling  bool
	motorOn   bool
}

// NewHaptics returns a Haptics bound to cx with Enabled set to true.
// It watches cx's lifecycle so feedback pauses cleanly in core.LifecycleBackground,
// and stops any active rumble when cx is disposed.
func NewHaptics(cx *core.Scope) *Haptics {
	h := &Haptics{
		Enabled: true,
		cx:      cx,
	}
	if cx != nil {
		cx.Effect(func() {
			bg := cx.Env().Lifecycle() == core.LifecycleBackground
			h.background = bg
			if bg {
				h.StopRumble()
			}
		})
		cx.Cleanup(h.StopRumble)
	}
	return h
}

// SetEnabled sets h.Enabled and immediately stops any active rumble when disabling.
func (h *Haptics) SetEnabled(enabled bool) {
	h.Enabled = enabled
	if !enabled {
		h.StopRumble()
	}
}

// Save writes h.Enabled into store under key. It fails with *core.StorageFullError when storage is full.
func (h *Haptics) Save(store core.Storage, key string) error {
	return store.SetItem(key, strconv.FormatBool(h.Enabled))
}

// Load restores h.Enabled from store under key and reports whether key was present.
// When key is absent or invalid, h.Enabled is left untouched.
func (h *Haptics) Load(store core.Storage, key string) (bool, error) {
	raw, ok := store.GetItem(key)
	if !ok {
		return false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("input: load haptics %q: %w", key, err)
	}
	h.SetEnabled(v)
	return true, nil
}

// Play emits kind through core.Scope.PlayHaptic when h is enabled and in foreground.
// Invalid haptic values are ignored.
func (h *Haptics) Play(kind core.Haptic) {
	if kind < core.HapticSelection || kind > core.HapticError {
		return
	}
	if !h.canEmit() {
		return
	}
	h.dispatchHaptic(kind)
}

// Selection plays a subtle selection-change tick (core.HapticSelection).
func (h *Haptics) Selection() { h.Play(core.HapticSelection) }

// Light plays a light impact haptic (core.HapticLight).
func (h *Haptics) Light() { h.Play(core.HapticLight) }

// Medium plays a medium impact haptic (core.HapticMedium).
func (h *Haptics) Medium() { h.Play(core.HapticMedium) }

// Heavy plays a heavy impact haptic (core.HapticHeavy).
func (h *Haptics) Heavy() { h.Play(core.HapticHeavy) }

// Soft plays a soft impact haptic (core.HapticSoft).
func (h *Haptics) Soft() { h.Play(core.HapticSoft) }

// Rigid plays a rigid impact haptic (core.HapticRigid).
func (h *Haptics) Rigid() { h.Play(core.HapticRigid) }

// Success plays a positive outcome haptic (core.HapticSuccess).
func (h *Haptics) Success() { h.Play(core.HapticSuccess) }

// Warning plays a warning outcome haptic (core.HapticWarning).
func (h *Haptics) Warning() { h.Play(core.HapticWarning) }

// Error plays an error/failure outcome haptic (core.HapticError).
func (h *Haptics) Error() { h.Play(core.HapticError) }

// Rumble starts a timed rumble with lowFreq and highFreq motor strengths in [0, 1] for duration.
// Calling Rumble with duration <= 0 or both strengths <= 0 stops any active rumble.
// Step the rumble scheduler by calling Update(dt) from game.Loop.Update.
func (h *Haptics) Rumble(lowFreq, highFreq float32, duration time.Duration) {
	low := clampFloat32Unit(lowFreq)
	high := clampFloat32Unit(highFreq)
	if duration <= 0 || (low == 0 && high == 0) {
		h.StopRumble()
		return
	}
	h.PlayPattern(RumbleStep{LowFreq: low, HighFreq: high, Duration: duration})
}

// PlayPattern runs a sequence of timed RumbleSteps, replacing any active rumble.
// Call Update(dt) on each game.Loop step to advance the pattern.
func (h *Haptics) PlayPattern(steps ...RumbleStep) {
	if !h.canEmit() {
		h.StopRumble()
		return
	}
	filtered := make([]RumbleStep, 0, len(steps))
	for _, s := range steps {
		if s.Duration <= 0 {
			continue
		}
		filtered = append(filtered, RumbleStep{
			LowFreq:  clampFloat32Unit(s.LowFreq),
			HighFreq: clampFloat32Unit(s.HighFreq),
			Duration: s.Duration,
		})
	}
	if len(filtered) == 0 {
		h.StopRumble()
		return
	}
	h.steps = filtered
	h.stepIdx = 0
	h.rumbling = true
	h.beginStep(0)
}

// StopRumble immediately stops any active rumble or pattern and zeroes the gamepad motor sink.
func (h *Haptics) StopRumble() {
	h.steps = nil
	h.stepIdx = 0
	h.remaining = 0
	h.pulseAcc = 0
	h.rumbling = false
	if h.motorOn {
		h.motorOn = false
		if h.GamepadRumble != nil {
			h.GamepadRumble(0, 0, 0)
		}
	}
}

// Rumbling reports whether a rumble or pattern is currently playing.
func (h *Haptics) Rumbling() bool {
	return h.rumbling
}

// Update advances any active rumble or pattern by dt, emitting periodic mobile haptic pulses
// and ending or advancing steps as their durations expire. Call it from game.Loop.Update.
func (h *Haptics) Update(dt time.Duration) {
	if !h.rumbling {
		return
	}
	if !h.canEmit() {
		h.StopRumble()
		return
	}
	if dt <= 0 {
		return
	}
	interval := h.pulseInterval()
	remDT := dt
	for h.rumbling && remDT > 0 {
		if remDT >= h.remaining {
			remDT -= h.remaining
			h.stepIdx++
			if h.stepIdx >= len(h.steps) {
				h.StopRumble()
				return
			}
			h.beginStep(h.stepIdx)
			continue
		}
		h.remaining -= remDT
		step := h.steps[h.stepIdx]
		if step.LowFreq > 0 || step.HighFreq > 0 {
			h.pulseAcc += remDT
			if h.pulseAcc >= interval {
				h.pulseAcc %= interval
				h.dispatchHaptic(rumbleHaptic(step.LowFreq, step.HighFreq))
			}
		}
		remDT = 0
	}
}

func (h *Haptics) beginStep(idx int) {
	step := h.steps[idx]
	h.remaining = step.Duration
	h.pulseAcc = 0
	if step.LowFreq > 0 || step.HighFreq > 0 {
		h.motorOn = true
		if h.GamepadRumble != nil {
			h.GamepadRumble(step.LowFreq, step.HighFreq, step.Duration)
		}
		h.dispatchHaptic(rumbleHaptic(step.LowFreq, step.HighFreq))
		return
	}
	if h.motorOn {
		h.motorOn = false
		if h.GamepadRumble != nil {
			h.GamepadRumble(0, 0, 0)
		}
	}
}

func (h *Haptics) canEmit() bool {
	if !h.Enabled || h.background {
		return false
	}
	if h.cx != nil && h.cx.Disposed() {
		return false
	}
	return true
}

func (h *Haptics) dispatchHaptic(kind core.Haptic) {
	if h.cx != nil {
		h.cx.PlayHaptic(kind)
	}
	if h.OnHaptic != nil {
		h.OnHaptic(kind)
	}
}

func (h *Haptics) pulseInterval() time.Duration {
	if h.PulseInterval > 0 {
		return h.PulseInterval
	}
	return defaultRumblePulseInterval
}

func rumbleHaptic(lowFreq, highFreq float32) core.Haptic {
	strength := max(lowFreq, highFreq)
	switch {
	case strength >= 0.7:
		return core.HapticHeavy
	case strength >= 0.35:
		return core.HapticMedium
	default:
		return core.HapticLight
	}
}

func clampFloat32Unit(v float32) float32 {
	if math.IsNaN(float64(v)) || v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}
