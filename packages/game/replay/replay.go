// Package replay records per-step game input to versioned YAML text files and plays it back deterministically for debugging and automated tests.
package replay

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/packages/game/loaderr"
)

// FormatVersion is the version written to every Recording and the only one Decode accepts.
//
// Version 2 is the YAML text format described in Encode; version 1 was a compact JSON form that is no longer read.
const FormatVersion = 2

const defaultRate = 60

// ActionSource supplies named actions and axes sampled once per game.Loop step.
//
// *input.Actions satisfies ActionSource for live play, *Recorder wraps an ActionSource to record it, and *Player satisfies ActionSource for playback, so a game reading input through ActionSource behaves identically in all three modes.
type ActionSource interface {
	Pressed(name string) bool
	JustPressed(name string) bool
	JustReleased(name string) bool
	Axis(name string) float64
	Step()
}

// Recording holds a game's random seed, fixed step rate, watched action and axis names, total step count and delta-encoded step events.
//
// Only steps where at least one watched action's pressed state or watched axis's value changed from the previous step appear in Events, keeping long recordings compact. Encode writes a Recording as YAML text with one line per event, and Decode reads that text back.
type Recording struct {
	// Version is the format version; it must equal FormatVersion.
	Version int
	// Seed is the random seed the recorded run started from.
	Seed uint64
	// Rate is the fixed step rate in steps per second.
	Rate int
	// Actions are the watched action names that StepEvent.Press and StepEvent.Release index into.
	Actions []string
	// Axes are the watched axis names that AxisChange.Index indexes into.
	Axes []string
	// TotalSteps is the number of recorded steps, including steps without events.
	TotalSteps int
	// Events are the steps where input changed, in strictly increasing Step order.
	Events []StepEvent
}

// StepEvent holds the action and axis changes that occurred on Step.
//
// Press and Release hold indices into Recording.Actions. Axes holds index-to-value updates into Recording.Axes.
type StepEvent struct {
	// Step is the zero-based step the changes take effect on.
	Step int
	// Press lists the actions that became pressed on Step.
	Press []int
	// Release lists the actions that were released on Step.
	Release []int
	// Axes lists the axes whose value changed on Step.
	Axes []AxisChange
}

// AxisChange records a new value for Recording.Axes[Index].
type AxisChange struct {
	// Index is the position of the axis in Recording.Axes.
	Index int
	// Value is the axis value from this step on.
	Value float64
}

// StepDuration returns the fixed duration of one step, 1/Rate seconds (1/60 s when Rate <= 0).
func (r *Recording) StepDuration() time.Duration {
	rate := defaultRate
	if r != nil && r.Rate > 0 {
		rate = r.Rate
	}
	return time.Second / time.Duration(rate)
}

// Validate checks that r uses the supported format version, has a valid rate and step count, unique non-empty action and axis names, and strictly increasing in-bounds step events. Its errors are *loaderr.Error values that name the field at fault in the YAML text Encode writes, such as "steps[3].press[0]", and how to fix it.
func (r *Recording) Validate() error {
	if r == nil {
		return loaderr.Errorf("", "pass a recording", "recording is nil")
	}
	if r.Version != FormatVersion {
		return &loaderr.Error{Field: "version", Problem: fmt.Sprintf("unsupported version %d", r.Version), Expected: fmt.Sprint(FormatVersion), Fix: fixRecordAgain}
	}
	if r.Rate <= 0 {
		return loaderr.Errorf("rate", "set rate to the fixed steps per second the recording ran at, above 0", "invalid rate %d", r.Rate)
	}
	if r.TotalSteps < 0 {
		return loaderr.Errorf("total_steps", "set total_steps to the number of steps recorded, 0 or more", "invalid total_steps %d", r.TotalSteps)
	}
	if err := validateNames("actions", "action", r.Actions); err != nil {
		return err
	}
	if err := validateNames("axes", "axis", r.Axes); err != nil {
		return err
	}

	prevStep := -1
	for i, ev := range r.Events {
		at := loaderr.Path("steps", i)
		if ev.Step < 0 || ev.Step >= r.TotalSteps {
			return loaderr.Errorf(loaderr.Join(at, "step"), "keep each step below total_steps, or raise total_steps", "event %d step %d out of range [0, %d)", i, ev.Step, r.TotalSteps)
		}
		if ev.Step <= prevStep {
			return loaderr.Errorf(loaderr.Join(at, "step"), "list steps in increasing order and merge two entries for the same step into one", "event %d step %d is not after previous step %d", i, ev.Step, prevStep)
		}
		prevStep = ev.Step

		var seenAction map[int]struct{}
		if len(ev.Press)+len(ev.Release) > 1 {
			seenAction = make(map[int]struct{}, len(ev.Press)+len(ev.Release))
		}
		for _, list := range []struct {
			key, verb string
			idx       []int
		}{{"press", "press", ev.Press}, {"release", "release", ev.Release}} {
			for j, idx := range list.idx {
				field := loaderr.Join(at, list.key, j)
				if idx < 0 || idx >= len(r.Actions) {
					return loaderr.Errorf(field, "use the index of a name in actions", "step %d %s index %d out of range [0, %d)", ev.Step, list.verb, idx, len(r.Actions))
				}
				if seenAction != nil {
					if _, dup := seenAction[idx]; dup {
						return loaderr.Errorf(field, "list each action once per step, in press or in release", "step %d lists action %q more than once", ev.Step, r.Actions[idx])
					}
					seenAction[idx] = struct{}{}
				}
			}
		}

		var seenAxis map[int]struct{}
		if len(ev.Axes) > 1 {
			seenAxis = make(map[int]struct{}, len(ev.Axes))
		}
		for _, ch := range ev.Axes {
			if ch.Index < 0 || ch.Index >= len(r.Axes) {
				return loaderr.Errorf(loaderr.Join(at, "axes"), "use the index of a name in axes", "step %d axis index %d out of range [0, %d)", ev.Step, ch.Index, len(r.Axes))
			}
			field := loaderr.Join(at, "axes", r.Axes[ch.Index])
			if math.IsNaN(ch.Value) || math.IsInf(ch.Value, 0) {
				return loaderr.Errorf(field, "use a finite axis value", "step %d axis %q has a non-finite value", ev.Step, r.Axes[ch.Index])
			}
			if seenAxis != nil {
				if _, dup := seenAxis[ch.Index]; dup {
					return loaderr.Errorf(field, "set each axis once per step", "step %d lists axis %q more than once", ev.Step, r.Axes[ch.Index])
				}
				seenAxis[ch.Index] = struct{}{}
			}
		}
	}
	return nil
}

// fixRecordAgain is the fix for a recording this build cannot read.
const fixRecordAgain = "record the replay again with this build, or correct the text as docs/en/replay.md describes it"

func validateNames(field, kind string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	for i, name := range names {
		if name == "" {
			return loaderr.Errorf(loaderr.Path(field, i), "give the "+kind+" a non-empty name", "%s %d has an empty name", kind, i)
		}
		if _, dup := seen[name]; dup {
			return loaderr.Errorf(loaderr.Path(field, i), "list each "+kind+" name once", "duplicate %s name %q", kind, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// Clone returns an independent deep copy of r, or nil if r is nil.
func (r *Recording) Clone() *Recording {
	if r == nil {
		return nil
	}
	out := &Recording{
		Version:    r.Version,
		Seed:       r.Seed,
		Rate:       r.Rate,
		Actions:    slices.Clone(r.Actions),
		Axes:       slices.Clone(r.Axes),
		TotalSteps: r.TotalSteps,
	}
	if len(r.Events) > 0 {
		out.Events = make([]StepEvent, len(r.Events))
		for i, ev := range r.Events {
			out.Events[i] = StepEvent{
				Step:    ev.Step,
				Press:   slices.Clone(ev.Press),
				Release: slices.Clone(ev.Release),
				Axes:    slices.Clone(ev.Axes),
			}
		}
	}
	return out
}
