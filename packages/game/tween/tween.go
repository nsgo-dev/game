// Package tween animates values, timelines, and tilemap cameras deterministically on fixed game.Loop steps using NSGo easing curves.
package tween

import (
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// RepeatForever repeats a tween indefinitely when passed to Tween.Repeat.
const RepeatForever = -1

// Tween interpolates a value over a fixed duration using a core.Curve easing function, advancing deterministically via Update(dt) on game.Loop steps.
type Tween struct {
	dur        time.Duration
	delay      time.Duration
	repeat     int // 0 plays once, >0 additional repeats, <0 repeats forever
	yoyo       bool
	curve      core.Curve
	apply      func(t float64)
	onStart    func()
	started    bool
	onComplete []func()

	elapsed time.Duration
	done    bool
}

// New returns a tween that interpolates from from to to over d with curve, using lerp to compute each intermediate value and set to apply it.
//
// A zero curve uses core.Linear. A negative d is treated as 0.
func New[T any](from, to T, d time.Duration, curve core.Curve, lerp func(a, b T, t float64) T, set func(T)) *Tween {
	if d < 0 {
		d = 0
	}
	return &Tween{
		dur:   d,
		curve: curve,
		apply: func(t float64) {
			if set == nil {
				return
			}
			var v T
			switch {
			case t == 0:
				v = from
			case t == 1:
				v = to
			case lerp != nil:
				v = lerp(from, to, t)
			default:
				v = from
			}
			set(v)
		},
	}
}

// Float32 returns a tween over a float32 value from from to to over d with curve.
func Float32(from, to float32, d time.Duration, curve core.Curve, set func(float32)) *Tween {
	return New(from, to, d, curve, lerpFloat32, set)
}

// Float64 returns a tween over a float64 value from from to to over d with curve.
func Float64(from, to float64, d time.Duration, curve core.Curve, set func(float64)) *Tween {
	return New(from, to, d, curve, lerpFloat64, set)
}

// Point returns a tween over a core.Point from from to to over d with curve.
func Point(from, to core.Point, d time.Duration, curve core.Curve, set func(core.Point)) *Tween {
	return New(from, to, d, curve, lerpPoint, set)
}

// Color returns a tween over a core.Color from from to to over d with curve, interpolating sRGB and alpha channels.
func Color(from, to core.Color, d time.Duration, curve core.Curve, set func(core.Color)) *Tween {
	return New(from, to, d, curve, lerpColor, set)
}

// Delay waits d before the tween begins moving. Negative durations are treated as 0.
func (t *Tween) Delay(d time.Duration) *Tween {
	if d < 0 {
		d = 0
	}
	t.delay = d
	return t
}

// Repeat repeats the tween count additional times after its first pass (0 plays once; RepeatForever or any negative value repeats forever). When yoyo is true, alternating passes play back from to to from.
func (t *Tween) Repeat(count int, yoyo bool) *Tween {
	t.repeat = count
	t.yoyo = yoyo
	return t
}

// OnComplete registers fn to run once when Update finishes the tween. Multiple callbacks run in the order registered.
func (t *Tween) OnComplete(fn func()) *Tween {
	if fn != nil {
		t.onComplete = append(t.onComplete, fn)
	}
	return t
}

// Duration returns the tween's total duration including delay and repeats. For a tween that repeats forever, Duration returns one full cycle (delay plus one pass, or two passes when yoyo is enabled).
func (t *Tween) Duration() time.Duration {
	if t.repeat < 0 {
		if t.yoyo {
			return addDuration(t.delay, mulDuration(t.dur, 2))
		}
		return addDuration(t.delay, t.dur)
	}
	passes := t.repeat + 1
	return addDuration(t.delay, mulDuration(t.dur, passes))
}

// Done reports whether the tween has reached its end.
func (t *Tween) Done() bool { return t.done }

// Reset rewinds the tween to its start so it can play again from elapsed 0.
func (t *Tween) Reset() {
	t.elapsed = 0
	t.done = false
}

// Update advances the tween by dt, applies the value at the new time, and reports whether the tween has finished.
func (t *Tween) Update(dt time.Duration) bool {
	if t.done || dt <= 0 {
		return t.done
	}
	t.elapsed = addDuration(t.elapsed, dt)
	if t.elapsed < t.delay {
		return false
	}
	eased, finished := t.progress(t.elapsed)
	t.eval(eased)
	if finished {
		t.done = true
		t.fireComplete()
	}
	return t.done
}

// Seek moves the tween to timestamp at and applies the value it holds there without firing OnComplete callbacks.
func (t *Tween) Seek(at time.Duration) {
	if at < 0 {
		at = 0
	}
	t.elapsed = at
	eased, finished := t.progress(at)
	t.done = finished
	t.eval(eased)
}

// seekBefore puts the tween in its pre-start state for a timeline timestamp earlier than the track's start.
func (t *Tween) seekBefore() {
	t.elapsed = 0
	t.done = false
	if t.onStart != nil && !t.started {
		return
	}
	t.eval(0)
}

func (t *Tween) infinite() bool { return t.repeat < 0 }

func (t *Tween) eval(eased float64) {
	if !t.started {
		t.started = true
		if t.onStart != nil {
			t.onStart()
		}
	}
	if t.apply != nil {
		t.apply(eased)
	}
}

func (t *Tween) fireComplete() {
	for _, fn := range t.onComplete {
		fn()
	}
}

// progress returns the eased interpolation factor at elapsed time at and whether the tween is finished.
func (t *Tween) progress(at time.Duration) (eased float64, done bool) {
	if at < t.delay {
		return 0, false
	}
	active := at - t.delay
	if t.dur == 0 {
		if t.repeat < 0 {
			return 1, false
		}
		if t.yoyo && t.repeat%2 == 1 {
			return 0, true
		}
		return 1, true
	}
	if active == 0 {
		return 0, false
	}
	if t.repeat >= 0 {
		passes := t.repeat + 1
		totalActive := mulDuration(t.dur, passes)
		if active >= totalActive {
			if t.yoyo && (passes-1)%2 == 1 {
				return 0, true
			}
			return 1, true
		}
	}
	pass := int(active / t.dur)
	rem := active % t.dur
	u := float64(rem) / float64(t.dur)
	if t.yoyo && pass%2 == 1 {
		u = 1 - u
	}
	return t.curve.At(u), false
}

// Group steps a collection of active tweens together and removes each once it finishes. Its zero value is ready to use.
type Group struct {
	tweens []*Tween
}

// Add registers one or more non-nil tweens to be stepped by Update.
func (g *Group) Add(tweens ...*Tween) {
	for _, tw := range tweens {
		if tw != nil {
			g.tweens = append(g.tweens, tw)
		}
	}
}

// Remove deletes tw from the group if present.
func (g *Group) Remove(tw *Tween) {
	for i, cur := range g.tweens {
		if cur == tw {
			copy(g.tweens[i:], g.tweens[i+1:])
			g.tweens[len(g.tweens)-1] = nil
			g.tweens = g.tweens[:len(g.tweens)-1]
			return
		}
	}
}

// Clear removes all tweens from the group.
func (g *Group) Clear() {
	clear(g.tweens)
	g.tweens = g.tweens[:0]
}

// Len returns the number of active tweens currently in the group.
func (g *Group) Len() int { return len(g.tweens) }

// Update advances every active tween in the group by dt and removes any that finish.
func (g *Group) Update(dt time.Duration) {
	if len(g.tweens) == 0 {
		return
	}
	batch := make([]*Tween, len(g.tweens))
	copy(batch, g.tweens)
	for _, tw := range batch {
		if !g.contains(tw) {
			continue
		}
		if tw.Update(dt) {
			g.Remove(tw)
		}
	}
}

func (g *Group) contains(tw *Tween) bool {
	for _, cur := range g.tweens {
		if cur == tw {
			return true
		}
	}
	return false
}

func lerpFloat32(a, b float32, t float64) float32 {
	if t == 0 {
		return a
	}
	if t == 1 {
		return b
	}
	return float32(float64(a) + (float64(b)-float64(a))*t)
}

func lerpFloat64(a, b float64, t float64) float64 {
	if t == 0 {
		return a
	}
	if t == 1 {
		return b
	}
	return a + (b-a)*t
}

func lerpPoint(a, b core.Point, t float64) core.Point {
	if t == 0 {
		return a
	}
	if t == 1 {
		return b
	}
	return core.Point{
		X: core.Points(float64(a.X) + (float64(b.X)-float64(a.X))*t),
		Y: core.Points(float64(a.Y) + (float64(b.Y)-float64(a.Y))*t),
	}
}

func lerpColor(a, b core.Color, t float64) core.Color {
	if t == 0 {
		return a
	}
	if t == 1 {
		return b
	}
	ar, ag, ab, aa := a.Components()
	br, bg, bb, ba := b.Components()
	// A transparent endpoint adopts the other endpoint's RGB so fading in or out does not pass through black.
	if aa == 0 && ba > 0 {
		ar, ag, ab = br, bg, bb
	} else if ba == 0 && aa > 0 {
		br, bg, bb = ar, ag, ab
	}
	return packColor(
		ar+(br-ar)*t,
		ag+(bg-ag)*t,
		ab+(bb-ab)*t,
		aa+(ba-aa)*t,
	)
}

func packColor(r, g, b, a float64) core.Color {
	ch := func(v float64) uint32 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint32(math.Round(v * 255))
	}
	return core.Color(ch(r)<<24 | ch(g)<<16 | ch(b)<<8 | ch(a))
}

func addDuration(a, b time.Duration) time.Duration {
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

func mulDuration(d time.Duration, n int) time.Duration {
	if d <= 0 || n <= 0 {
		return 0
	}
	if d > math.MaxInt64/time.Duration(n) {
		return math.MaxInt64
	}
	return d * time.Duration(n)
}
