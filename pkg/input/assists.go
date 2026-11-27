package input

import (
	"math"
	"math/bits"
	"slices"
	"strings"
	"time"
)

const (
	// DefaultStepRate is the fixed update rate in steps per second assumed by
	// timed motor assists (MinPress, AcceptDelay, MashRate) when SetStepRate has
	// not been called.
	DefaultStepRate = 60

	// DefaultMashRate is the synthetic press rate in presses per second used for
	// MashToHold when an action enables mash assist without declaring a custom MashRate.
	DefaultMashRate = 10.0
)

// ActionAssist declares which player motor assists an action opts into.
//
// Menu or navigation actions leave Toggle and Mash false so global options such
// as a11y.Assist.HoldToToggle never latch them.
type ActionAssist struct {
	// Toggle reports whether the action may be latched as a toggle by player assists.
	Toggle bool
	// Mash reports whether the action is a rapid-press ("mash") mechanic that
	// MashToHold may satisfy with a hold or a single latched press.
	Mash bool
	// MashRate is the synthetic press rate in presses per second used when
	// MashToHold is active for this action. Zero or negative uses DefaultMashRate.
	MashRate float64
}

// Assists holds player-chosen motor accessibility assists evaluated during each
// fixed Actions.Step so live play and replays produce identical snapshots.
type Assists struct {
	// Toggle lists action names that latch pressed on the first accepted press
	// and release on the next accepted press.
	Toggle []string
	// MashToHold lists action names whose rapid-press mechanics count a single
	// hold (or latched press when also in Toggle) as repeated presses at the
	// action's declared MashRate (DefaultMashRate when unset).
	MashToHold []string
	// MinPress is the minimum continuous duration a binding must be held before
	// a press is accepted (slow keys). Zero or negative disables the filter.
	MinPress time.Duration
	// AcceptDelay is the cooldown period after an accepted press of an action
	// during which further presses of that action are ignored (bounce keys).
	// Zero or negative disables the cooldown.
	AcceptDelay time.Duration
}

// IsZero reports whether a enables no motor assists.
func (a Assists) IsZero() bool {
	return len(a.Toggle) == 0 && len(a.MashToHold) == 0 && a.MinPress <= 0 && a.AcceptDelay <= 0
}

type actionAssistState struct {
	// prevRawHeld tracks whether the raw physical/injected binding was held on
	// the previous step, so a release+repress between steps restarts MinPress.
	prevRawHeld bool
	// heldSteps counts consecutive fixed steps the raw binding has been held.
	heldSteps int
	// prevFilteredHeld tracks Stage 1 (MinPress) output on the previous step.
	prevFilteredHeld bool
	// acceptCooldown counts remaining steps during which new presses are ignored.
	acceptCooldown int
	// pressAccepted tracks whether the current continuous filtered hold passed
	// AcceptDelay so ongoing holds are never cut off by cooldown.
	pressAccepted bool
	// toggledOn tracks Stage 3 (Toggle) latched state.
	toggledOn bool
	// mashActive tracks whether Stage 4 (MashToHold) is currently holding/pulsing.
	mashActive bool
	// mashAccum accumulates fractional pulses per step at MashRate / stepRate.
	mashAccum float64
	// mashLastPulse records whether the immediately preceding step emitted a pulse,
	// guaranteeing at least one released step between pulses so JustPressed and
	// replay recordings observe every synthetic press.
	mashLastPulse bool
}

func (s actionAssistState) idle() bool {
	return !s.prevRawHeld &&
		s.heldSteps == 0 &&
		!s.prevFilteredHeld &&
		s.acceptCooldown == 0 &&
		!s.pressAccepted &&
		!s.toggledOn &&
		!s.mashActive &&
		!s.mashLastPulse
}

// SetStepRate sets the fixed step rate in steps per second used to convert timed
// motor assists (MinPress, AcceptDelay, MashRate) into fixed steps. Non-positive
// values restore DefaultStepRate (60).
func (a *Actions) SetStepRate(rate int) {
	if rate <= 0 {
		a.stepRate = 0
		return
	}
	a.stepRate = rate
}

// StepRate returns the fixed step rate in steps per second used for timed motor
// assists (DefaultStepRate when unset).
func (a *Actions) StepRate() int {
	if a == nil || a.stepRate <= 0 {
		return DefaultStepRate
	}
	return a.stepRate
}

// SetActionAssist declares which motor assists action name opts into.
//
// Once any action on a declares an ActionAssist (including via project.File.BindInput),
// Toggle and MashToHold only affect actions that explicitly enable Toggle or Mash,
// ensuring undeclared menu or pause actions are never latched.
func (a *Actions) SetActionAssist(name string, spec ActionAssist) {
	if name == "" {
		return
	}
	if spec.MashRate < 0 || math.IsNaN(spec.MashRate) || math.IsInf(spec.MashRate, 0) {
		spec.MashRate = 0
	}
	if a.actionAssists == nil {
		a.actionAssists = make(map[string]ActionAssist)
	}
	a.actionAssists[name] = spec
	if st, ok := a.assistState[name]; ok {
		if !spec.Toggle && st.toggledOn {
			st.toggledOn = false
			st.mashActive = false
			st.mashAccum = 0
			st.mashLastPulse = false
			a.assistState[name] = st
		}
		if !spec.Mash && st.mashActive {
			st.mashActive = false
			st.mashAccum = 0
			st.mashLastPulse = false
			a.assistState[name] = st
		}
	}
}

// ActionAssist returns the declared ActionAssist specification for name, or the
// zero value if none has been declared.
func (a *Actions) ActionAssist(name string) ActionAssist {
	if a == nil {
		return ActionAssist{}
	}
	return a.actionAssists[name]
}

// ToggleableActions returns the sorted names of actions declared with Toggle: true.
func (a *Actions) ToggleableActions() []string {
	if a == nil || len(a.actionAssists) == 0 {
		return nil
	}
	var out []string
	for name, spec := range a.actionAssists {
		if spec.Toggle {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return out
}

// MashableActions returns the sorted names of actions declared with Mash: true.
func (a *Actions) MashableActions() []string {
	if a == nil || len(a.actionAssists) == 0 {
		return nil
	}
	var out []string
	for name, spec := range a.actionAssists {
		if spec.Mash {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return out
}

// SetAssists sets the player-chosen motor assists applied during Step.
// Negative durations are clamped to 0, and action lists are trimmed, deduplicated,
// and sorted. Removing an action from Toggle or MashToHold releases its latched
// toggle or mash state immediately.
func (a *Actions) SetAssists(assists Assists) {
	norm := normalizeAssists(assists)
	a.assists = norm

	if len(norm.Toggle) > 0 {
		a.toggleSet = make(map[string]bool, len(norm.Toggle))
		for _, name := range norm.Toggle {
			a.toggleSet[name] = true
		}
	} else {
		a.toggleSet = nil
	}

	if len(norm.MashToHold) > 0 {
		a.mashSet = make(map[string]bool, len(norm.MashToHold))
		for _, name := range norm.MashToHold {
			a.mashSet[name] = true
		}
	} else {
		a.mashSet = nil
	}

	// Clean up stale runtime state for assists that were turned off.
	for name, st := range a.assistState {
		if !a.isToggleEnabled(name) {
			st.toggledOn = false
		}
		if !a.isMashEnabled(name) {
			st.mashActive = false
			st.mashAccum = 0
			st.mashLastPulse = false
		}
		if norm.MinPress <= 0 {
			st.heldSteps = 0
		}
		if norm.AcceptDelay <= 0 {
			st.acceptCooldown = 0
		}
		if st.idle() {
			delete(a.assistState, name)
		} else {
			a.assistState[name] = st
		}
	}
}

// Assists returns a copy of the active player motor assists on a.
func (a *Actions) Assists() Assists {
	if a == nil {
		return Assists{}
	}
	return Assists{
		Toggle:      slices.Clone(a.assists.Toggle),
		MashToHold:  slices.Clone(a.assists.MashToHold),
		MinPress:    a.assists.MinPress,
		AcceptDelay: a.assists.AcceptDelay,
	}
}

// ToggleActive reports whether action name is currently latched on by a Toggle assist.
func (a *Actions) ToggleActive(name string) bool {
	if a == nil {
		return false
	}
	return a.assistState[name].toggledOn
}

// ReleaseToggles releases all latched Toggle states and active MashToHold holds
// without discarding bindings, assist configuration, or accept-delay cooldowns.
// On the next Step, previously latched actions report JustReleased.
func (a *Actions) ReleaseToggles() {
	if a == nil || len(a.assistState) == 0 {
		return
	}
	for name, st := range a.assistState {
		st.toggledOn = false
		st.mashActive = false
		st.mashAccum = 0
		st.mashLastPulse = false
		st.heldSteps = 0
		if st.idle() {
			delete(a.assistState, name)
		} else {
			a.assistState[name] = st
		}
	}
}

// SetPaused notifies a that the game loop paused or resumed. When paused is true,
// all latched toggles and in-flight mash or slow-key holds are released so no
// toggled action stays active across a pause menu. Suitable for loop.OnPause.
func (a *Actions) SetPaused(paused bool) {
	if a == nil {
		return
	}
	if paused {
		a.ReleaseToggles()
	}
}

func (a *Actions) hasAssists() bool {
	return !a.assists.IsZero()
}

func (a *Actions) isToggleEnabled(name string) bool {
	if !a.toggleSet[name] {
		return false
	}
	if len(a.actionAssists) == 0 {
		return true
	}
	return a.actionAssists[name].Toggle
}

func (a *Actions) isMashEnabled(name string) bool {
	if !a.mashSet[name] {
		return false
	}
	if len(a.actionAssists) == 0 {
		return true
	}
	return a.actionAssists[name].Mash
}

func (a *Actions) mashRateFor(name string) float64 {
	if spec, ok := a.actionAssists[name]; ok && spec.MashRate > 0 && !math.IsNaN(spec.MashRate) && !math.IsInf(spec.MashRate, 0) {
		return spec.MashRate
	}
	return DefaultMashRate
}

// durationToSteps converts d at rate steps/sec into whole fixed steps using exact
// rational ceiling arithmetic (ceil(d * rate / time.Second)) so exact multiples
// such as 100ms at 60 Hz equal 6 steps rather than suffering from 16.666ms truncation.
func durationToSteps(d time.Duration, rate int) int {
	if d <= 0 {
		return 0
	}
	if rate <= 0 {
		rate = DefaultStepRate
	}
	sec := uint64(time.Second)
	hi, lo := bits.Mul64(uint64(d), uint64(rate))
	// Add (sec - 1) for ceiling division.
	var carry uint64
	lo, carry = bits.Add64(lo, sec-1, 0)
	hi += carry
	if hi >= sec {
		return math.MaxInt
	}
	q, _ := bits.Div64(hi, lo, sec)
	if q > uint64(math.MaxInt) {
		return math.MaxInt
	}
	if q == 0 {
		return 1
	}
	return int(q)
}

func normalizeAssists(in Assists) Assists {
	out := Assists{
		Toggle:      normalizeActionList(in.Toggle),
		MashToHold:  normalizeActionList(in.MashToHold),
		MinPress:    in.MinPress,
		AcceptDelay: in.AcceptDelay,
	}
	if out.MinPress < 0 {
		out.MinPress = 0
	}
	if out.AcceptDelay < 0 {
		out.AcceptDelay = 0
	}
	return out
}

func normalizeActionList(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return out
}

// evaluateAssistAction evaluates the motor assist pipeline for a single action on
// the current fixed step, returning the effective pressed state and whether a fresh
// rising edge occurred on this step.
func (a *Actions) evaluateAssistAction(name string, rawHeld, rawLatched bool, minPressSteps, acceptDelaySteps, rate int) (outPressed, outRising bool) {
	isToggle := a.isToggleEnabled(name)
	isMash := a.isMashEnabled(name)

	// Fast path when no assists apply to this action and it has no lingering state.
	st, hadState := a.assistState[name]
	if !isToggle && !isMash && minPressSteps == 0 && acceptDelaySteps == 0 && !hadState {
		pressed := rawHeld || rawLatched
		return pressed, rawLatched
	}

	// Stage 1: MinPress (slow keys).
	var filteredHeld, filteredRising bool
	if minPressSteps > 0 {
		if rawHeld {
			if rawLatched && st.prevRawHeld {
				// Released and re-pressed between steps: restart continuous hold timer.
				st.heldSteps = 1
			} else {
				st.heldSteps++
			}
		} else {
			// A brief tap released before the step (rawLatched && !rawHeld) lasted less
			// than one step, so it never satisfies MinPress > 0.
			st.heldSteps = 0
		}
		filteredHeld = rawHeld && st.heldSteps >= minPressSteps
		if filteredHeld {
			if !st.prevFilteredHeld {
				filteredRising = true
			} else if rawLatched && st.prevRawHeld && minPressSteps == 1 {
				filteredRising = true
			}
		}
	} else {
		st.heldSteps = 0
		filteredHeld = rawHeld || rawLatched
		filteredRising = (filteredHeld && !st.prevFilteredHeld) || rawLatched
	}
	st.prevRawHeld = rawHeld
	st.prevFilteredHeld = filteredHeld

	// Stage 2: AcceptDelay (bounce keys / post-acceptance cooldown).
	if st.acceptCooldown > 0 {
		st.acceptCooldown--
	}
	var acceptedHeld, acceptedRising bool
	if acceptDelaySteps > 0 {
		if filteredRising {
			if st.acceptCooldown == 0 {
				acceptedRising = true
				st.pressAccepted = true
				st.acceptCooldown = acceptDelaySteps
			} else {
				// Ignore bounce press during cooldown; keep pressAccepted false so
				// holding an ignored bounce does not spontaneously fire later.
				st.pressAccepted = false
			}
		}
		if !filteredHeld {
			st.pressAccepted = false
		}
		acceptedHeld = filteredHeld && st.pressAccepted
	} else {
		st.pressAccepted = filteredHeld
		acceptedHeld = filteredHeld
		acceptedRising = filteredRising
	}

	// Stage 3: Toggle (hold-to-toggle).
	var holdActive, holdRising bool
	if isToggle {
		if acceptedRising {
			st.toggledOn = !st.toggledOn
			if st.toggledOn {
				holdRising = true
			}
		}
		holdActive = st.toggledOn
	} else {
		st.toggledOn = false
		holdActive = acceptedHeld
		holdRising = acceptedRising
	}

	// Stage 4: MashToHold (press-once / hold instead of rapid mashing).
	if isMash {
		if holdActive {
			if !st.mashActive {
				// Emit the first press immediately when the hold (or toggle latch) starts.
				st.mashActive = true
				st.mashAccum = 0
				st.mashLastPulse = true
				outPressed = true
				outRising = true
			} else {
				mashRate := a.mashRateFor(name)
				st.mashAccum += mashRate / float64(rate)
				if st.mashAccum >= 1.0-1e-9 && !st.mashLastPulse {
					st.mashAccum -= 1.0
					if st.mashAccum < 0 {
						st.mashAccum = 0
					}
					st.mashLastPulse = true
					outPressed = true
					outRising = true
				} else {
					// Cap accumulator at 1.0 so rates >= stepRate/2 alternate cleanly
					// ON/OFF every other step without building backlog.
					if st.mashAccum > 1.0 {
						st.mashAccum = 1.0
					}
					st.mashLastPulse = false
					outPressed = false
					outRising = false
				}
			}
		} else {
			st.mashActive = false
			st.mashAccum = 0
			st.mashLastPulse = false
			outPressed = false
			outRising = false
		}
	} else {
		st.mashActive = false
		st.mashAccum = 0
		st.mashLastPulse = false
		outPressed = holdActive
		outRising = holdRising
	}

	if st.idle() {
		delete(a.assistState, name)
	} else {
		if a.assistState == nil {
			a.assistState = make(map[string]actionAssistState)
		}
		a.assistState[name] = st
	}
	return outPressed, outRising
}
