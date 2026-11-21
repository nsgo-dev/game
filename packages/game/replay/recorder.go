package replay

import (
	"math"
	"slices"
)

var _ ActionSource = (*Recorder)(nil)

// Recorder wraps an ActionSource, implements ActionSource itself, and records watched actions and axes on every Step.
//
// Calling Step steps the wrapped source once and appends a StepEvent only when at least one watched action's pressed state or watched axis's value changed from the previous step. Pass a Recorder anywhere a game accepts ActionSource to record live play without changing the game's update loop.
type Recorder struct {
	src        ActionSource
	seed       uint64
	rate       int
	actions    []string
	axes       []string
	prevAction []bool
	prevAxis   []float64
	totalSteps int
	events     []StepEvent
}

// NewRecorder returns a Recorder that wraps src and records the named actions and axes with seed and fixed step rate (60 when rate <= 0).
//
// Empty or duplicate names in actions and axes are ignored, preserving first-seen order.
func NewRecorder(src ActionSource, seed uint64, rate int, actions, axes []string) *Recorder {
	if rate <= 0 {
		rate = defaultRate
	}
	act := uniqueNames(actions)
	ax := uniqueNames(axes)
	return &Recorder{
		src:        src,
		seed:       seed,
		rate:       rate,
		actions:    act,
		axes:       ax,
		prevAction: make([]bool, len(act)),
		prevAxis:   make([]float64, len(ax)),
	}
}

func uniqueNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
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
	return out
}

// Step steps the wrapped ActionSource once and records any watched actions or axes whose values changed on this step.
func (r *Recorder) Step() {
	if r.src != nil {
		r.src.Step()
	}
	stepIdx := r.totalSteps
	r.totalSteps++

	var press, release []int
	for i, name := range r.actions {
		now := r.src != nil && r.src.Pressed(name)
		if now != r.prevAction[i] {
			r.prevAction[i] = now
			if now {
				press = append(press, i)
			} else {
				release = append(release, i)
			}
		}
	}

	var axisChanges []AxisChange
	for i, name := range r.axes {
		var now float64
		if r.src != nil {
			now = r.src.Axis(name)
		}
		if now == 0 || math.IsNaN(now) || math.IsInf(now, 0) {
			now = 0
		}
		if now != r.prevAxis[i] {
			r.prevAxis[i] = now
			axisChanges = append(axisChanges, AxisChange{Index: i, Value: now})
		}
	}

	if len(press) > 0 || len(release) > 0 || len(axisChanges) > 0 {
		r.events = append(r.events, StepEvent{
			Step:    stepIdx,
			Press:   press,
			Release: release,
			Axes:    axisChanges,
		})
	}
}

// Pressed reports whether name is pressed on the wrapped ActionSource.
func (r *Recorder) Pressed(name string) bool {
	if r.src == nil {
		return false
	}
	return r.src.Pressed(name)
}

// JustPressed reports whether name became pressed on the wrapped ActionSource on the current step.
func (r *Recorder) JustPressed(name string) bool {
	if r.src == nil {
		return false
	}
	return r.src.JustPressed(name)
}

// JustReleased reports whether name was released on the wrapped ActionSource on the current step.
func (r *Recorder) JustReleased(name string) bool {
	if r.src == nil {
		return false
	}
	return r.src.JustReleased(name)
}

// Axis returns the value of name on the wrapped ActionSource.
func (r *Recorder) Axis(name string) float64 {
	if r.src == nil {
		return 0
	}
	return r.src.Axis(name)
}

// Seed returns the random seed stored in the recording.
func (r *Recorder) Seed() uint64 { return r.seed }

// Rate returns the fixed step rate stored in the recording.
func (r *Recorder) Rate() int { return r.rate }

// TotalSteps returns the number of steps recorded so far.
func (r *Recorder) TotalSteps() int { return r.totalSteps }

// Reset clears the recorded steps and events and begins a fresh recording with seed, keeping the wrapped source, rate and watched names.
func (r *Recorder) Reset(seed uint64) {
	r.seed = seed
	r.totalSteps = 0
	r.events = nil
	clear(r.prevAction)
	clear(r.prevAxis)
}

// Recording returns a deep copy of everything recorded so far. Continuing to step r afterwards does not affect the returned Recording.
func (r *Recorder) Recording() *Recording {
	rec := &Recording{
		Version:    FormatVersion,
		Seed:       r.seed,
		Rate:       r.rate,
		Actions:    slices.Clone(r.actions),
		Axes:       slices.Clone(r.axes),
		TotalSteps: r.totalSteps,
	}
	if len(r.events) > 0 {
		rec.Events = make([]StepEvent, len(r.events))
		for i, ev := range r.events {
			rec.Events[i] = StepEvent{
				Step:    ev.Step,
				Press:   slices.Clone(ev.Press),
				Release: slices.Clone(ev.Release),
				Axes:    slices.Clone(ev.Axes),
			}
		}
	}
	return rec
}
