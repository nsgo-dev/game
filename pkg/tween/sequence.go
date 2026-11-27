package tween

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// To returns a tween that animates *value from whatever it holds when the step starts to
// to over d with ease. The start value is read lazily when the tween first runs, not when
// it is built, so earlier steps can leave the value anywhere. A zero ease uses linear.
//
// The built-in types (float32, float64, core.Point and core.Color) interpolate without a
// lerp; other types need one passed explicitly and hold their start value until snapping
// to the target at the end when lerp is omitted, matching New.
func To[T any](value *T, to T, d time.Duration, ease Ease, lerp ...func(a, b T, t float64) T) *Tween {
	if d < 0 {
		d = 0
	}
	lf := resolveLerp(lerp)
	var from T
	captured := false
	return &Tween{
		dur:      d,
		ease:     easeF64(ease),
		onRewind: func() { captured = false },
		apply: func(t float64) {
			if value == nil {
				return
			}
			if !captured {
				from = *value
				captured = true
			}
			var v T
			switch {
			case t == 0:
				v = from
			case t == 1:
				v = to
			case lf != nil:
				v = lf(from, to, t)
			default:
				v = from
			}
			*value = v
		},
	}
}

// By returns a tween like To that animates *value to its value at step start plus delta.
// The start value is read lazily when the tween first runs, so relative steps compose
// with the results of earlier steps. add combines the start value with delta and lerp
// interpolates; pass nil for both to use the defaults for float32, float64, core.Point
// and core.Color. For other types add is required; without it the tween holds the start
// value and snaps to delta at the end.
func By[T any](value *T, delta T, d time.Duration, ease Ease, add func(a, b T) T, lerp func(a, b T, t float64) T) *Tween {
	if d < 0 {
		d = 0
	}
	if add == nil {
		add = resolveAdd[T]()
	}
	lf := lerp
	if lf == nil {
		lf = resolveLerp[T](nil)
	}
	var from T
	captured := false
	return &Tween{
		dur:      d,
		ease:     easeF64(ease),
		onRewind: func() { captured = false },
		apply: func(t float64) {
			if value == nil {
				return
			}
			var to T
			if !captured {
				from = *value
				captured = true
			}
			if add != nil {
				to = add(from, delta)
			} else {
				to = delta
			}
			var v T
			switch {
			case t == 0:
				v = from
			case t == 1:
				v = to
			case lf != nil:
				v = lf(from, to, t)
			default:
				v = from
			}
			*value = v
		},
	}
}

// FromCurrent returns a tween like To over a value reached through get and set: the start
// is read from get when the step starts, and to is the target. A zero ease uses linear.
//
// The built-in types (float32, float64, core.Point and core.Color) interpolate without a
// lerp; other types need one passed explicitly and hold their start value until snapping
// to the target at the end when lerp is omitted, matching New.
func FromCurrent[T any](get func() T, set func(T), to T, d time.Duration, ease Ease, lerp ...func(a, b T, t float64) T) *Tween {
	if d < 0 {
		d = 0
	}
	lf := resolveLerp(lerp)
	var from T
	captured := false
	return &Tween{
		dur:      d,
		ease:     easeF64(ease),
		onRewind: func() { captured = false },
		apply: func(t float64) {
			if set == nil {
				return
			}
			if !captured {
				if get != nil {
					from = get()
				}
				captured = true
			}
			var v T
			switch {
			case t == 0:
				v = from
			case t == 1:
				v = to
			case lf != nil:
				v = lf(from, to, t)
			default:
				v = from
			}
			set(v)
		},
	}
}

// resolveLerp returns the explicit lerp, or the default interpolation for the built-in
// types when none is given. It returns nil for unknown types, which hold the start value.
func resolveLerp[T any](provided []func(a, b T, t float64) T) func(a, b T, t float64) T {
	if len(provided) > 0 && provided[0] != nil {
		return provided[0]
	}
	var zero T
	switch any(zero).(type) {
	case float32:
		return any(lerpFloat32).(func(a, b T, t float64) T)
	case float64:
		return any(lerpFloat64).(func(a, b T, t float64) T)
	case core.Point:
		return any(lerpPoint).(func(a, b T, t float64) T)
	case core.Color:
		return any(lerpColor).(func(a, b T, t float64) T)
	}
	return nil
}

// resolveAdd returns the default addition for the built-in types, or nil for others.
func resolveAdd[T any]() func(a, b T) T {
	var zero T
	switch any(zero).(type) {
	case float32:
		return any(func(a, b float32) float32 { return a + b }).(func(a, b T) T)
	case float64:
		return any(func(a, b float64) float64 { return a + b }).(func(a, b T) T)
	case core.Point:
		return any(func(a, b core.Point) core.Point { return core.Point{X: a.X + b.X, Y: a.Y + b.Y} }).(func(a, b T) T)
	case core.Color:
		return any(func(a, b core.Color) core.Color {
			ar, ag, ab, aa := a.Components()
			br, bg, bb, ba := b.Components()
			return packColor(ar+br, ag+bg, ab+bb, aa+ba)
		}).(func(a, b T) T)
	}
	return nil
}

// SequenceBuilder chains tween steps onto a Timeline without computing start times by
// hand: Then appends a step after the previous block, With runs a step in parallel with
// the last one, Wait inserts an interval and Call schedules a callback at the cursor.
type SequenceBuilder struct {
	tl        *Timeline
	cursor    time.Duration
	lastStart time.Duration
	hasLast   bool
	loops     int
}

// Sequence returns an empty SequenceBuilder ready to chain steps.
func Sequence() *SequenceBuilder {
	return &SequenceBuilder{tl: NewTimeline(), loops: 1}
}

// Then places step at the cursor, after every previous step, and advances the cursor to
// the step's end. Nil steps are ignored.
func (b *SequenceBuilder) Then(step *Tween) *SequenceBuilder {
	if step == nil {
		return b
	}
	b.tl.Add(b.cursor, step)
	b.lastStart = b.cursor
	b.hasLast = true
	b.cursor = addDuration(b.cursor, step.Duration())
	return b
}

// With places step so it runs in parallel with the last Then's step, starting at the
// same time. The cursor advances to the end of the longer of the parallel steps. Nil
// steps are ignored.
func (b *SequenceBuilder) With(step *Tween) *SequenceBuilder {
	if step == nil {
		return b
	}
	start := b.cursor
	if b.hasLast {
		start = b.lastStart
	}
	b.tl.Add(start, step)
	if end := addDuration(start, step.Duration()); end > b.cursor {
		b.cursor = end
	}
	return b
}

// Wait inserts an interval of d before the next step.
func (b *SequenceBuilder) Wait(d time.Duration) *SequenceBuilder {
	if d > 0 {
		b.cursor = addDuration(b.cursor, d)
	}
	return b
}

// Call schedules fn to run when playback reaches the cursor.
func (b *SequenceBuilder) Call(fn func()) *SequenceBuilder {
	b.tl.Call(b.cursor, fn)
	return b
}

// Loops sets how many runs the built timeline plays: n <= 0 wraps forever, n == 1 plays
// once and larger n wraps n-1 more times, like Timeline.SetLoops.
func (b *SequenceBuilder) Loops(n int) *SequenceBuilder {
	b.loops = n
	return b
}

// Build returns the timeline the builder assembled. Build may be called once; keep using
// the returned timeline and do not add steps to the builder afterwards.
func (b *SequenceBuilder) Build() *Timeline {
	b.tl.SetLoops(b.loops)
	return b.tl
}
