package postfx

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// shakeSource is ShakeEffect's program. Its sway is two sines a direction, at frequencies whose ratios repeat late, so the frame wanders rather than circles.
const shakeSource = `package main

// Amplitude is how far the frame moves at most along each direction, in its pixels.
var Amplitude float

// Time is the shake's clock in seconds.
var Time float

// Frequency is how many times a second the frame sways.
var Frequency float

// Phase starts each of the sway's four sines, in radians.
var Phase vec4

func Effect(pos vec2) vec4 {
	w := 6.2831853 * Frequency * Time
	o := vec2(0.6*sin(w+Phase.x)+0.4*sin(2.17*w+Phase.y), 0.6*sin(1.13*w+Phase.z)+0.4*sin(2.71*w+Phase.w))
	d := o * Amplitude
	if !(abs(d.x) < 1e30 && abs(d.y) < 1e30) {
		d = vec2(0)
	}
	size := InputSize()
	p := pos - d
	return Input(max(min(p, size-0.5), vec2(0.5)))
}
`

// ShakeEffect returns an effect that moves the whole frame about, as an explosion or a heavy landing does, reading the frame's edge where it moves off it. Its uniforms are:
//
//   - Amplitude, how far the frame moves at most along each direction, in the frame's pixels;
//   - Time, the shake's clock in seconds;
//   - Frequency, how many times a second the frame sways;
//   - Phase, four numbers or nodes in radians that start the sway's sines, which tell one shake from another.
//
// The frame moves by Amplitude times 0.6·sin(w+Phase[0]) + 0.4·sin(2.17w+Phase[1]) across and 0.6·sin(1.13w+Phase[2]) + 0.4·sin(2.71w+Phase[3]) down, with w = 2π·Frequency·Time. An offset that is not finite, or 1e30 pixels or more along either direction, as a NaN or infinite uniform gives, leaves the frame where it is. A Shake animates them on the frame clock. The effect has a Reference, so a chain of it draws a reference picture.
func ShakeEffect() Effect {
	return Effect{Name: "shake", Source: shakeSource, Reference: shakeReference}
}

// shakeReference is shakeSource on the CPU.
func shakeReference(in Input, u core.Uniforms, x, y float64) [4]float64 {
	ox, oy := ShakeOffset(Float(u, "Amplitude"), Float(u, "Time"), Float(u, "Frequency"), vector(u, "Phase", 4))
	w, h := in.Size()
	return in.At(math.Max(math.Min(x-ox, w-0.5), 0.5), math.Max(math.Min(y-oy, h-0.5), 0.5))
}

// ShakeOffset returns how far ShakeEffect moves the frame, in its pixels, at the uniforms given: (0, 0) when the offset is not finite or is 1e30 pixels or more along either direction, as on the GPU. It reads plain numbers, so it serves a ShakeEffect a game drives by hand; a Shake's uniforms follow animated nodes whose values only the platform knows.
func ShakeOffset(amplitude, seconds, frequency float64, phase [4]float64) (x, y float64) {
	w := 2 * math.Pi * frequency * seconds
	x = (0.6*math.Sin(w+phase[0]) + 0.4*math.Sin(2.17*w+phase[1])) * amplitude
	y = (0.6*math.Sin(1.13*w+phase[2]) + 0.4*math.Sin(2.71*w+phase[3])) * amplitude
	if !(math.Abs(x) < 1e30 && math.Abs(y) < 1e30) {
		return 0, 0
	}
	return x, y
}

// DefaultShakeFrequency is a Shake's Frequency when its config leaves it 0.
const DefaultShakeFrequency = 12

// ShakeConfig configures a Shake.
type ShakeConfig struct {
	// Seed picks each trigger's phases in turn, so the same seed shakes the same way in a replay; 0 is a seed of its own like any other, where tilemap.Camera's ShakeSeed takes 0 as 1.
	Seed uint64
	// Frequency is how many times a second the frame sways, DefaultShakeFrequency when 0 or less.
	Frequency float64
	// ReduceMotion, when set, is asked at each Trigger, and the shake does not run while it reports true: pass an a11y.Assist's ReduceMotion, which follows the player's choice and else the platform's. When nil, the platform's reduced motion setting is asked.
	ReduceMotion func() bool
}

// Shake is a screen shake a game triggers: ShakeEffect's Amplitude jumps to a peak and decays to 0 while its Time runs, on the platform's frame clock, with no commit per frame.
type Shake struct {
	cx        *core.Scope
	cfg       ShakeConfig
	rng       *rand.Rand
	amplitude *core.AnimatedValue
	time      *core.AnimatedValue
	phase     [4]*core.AnimatedValue
	run       *core.AnimationRun
}

// NewShake returns a shake for ShakeEffect, at rest, that lasts as long as cx's owner.
func NewShake(cx *core.Scope, cfg ShakeConfig) *Shake {
	if !(cfg.Frequency > 0) || math.IsInf(cfg.Frequency, 1) {
		cfg.Frequency = DefaultShakeFrequency
	}
	s := &Shake{
		cx:        cx,
		cfg:       cfg,
		rng:       rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b97f4a7c15)),
		amplitude: core.NewAnimatedValue(cx, 0),
		time:      core.NewAnimatedValue(cx, 0),
	}
	for i := range s.phase {
		s.phase[i] = core.NewAnimatedValue(cx, 0)
	}
	return s
}

// Uniforms returns ShakeEffect's uniforms, for its place in a View's Uniforms: Amplitude, Time and Phase follow the shake.
func (s *Shake) Uniforms() core.Uniforms {
	return core.Uniforms{
		"Amplitude": s.amplitude,
		"Time":      s.time,
		"Frequency": s.cfg.Frequency,
		"Phase":     []core.AnimatedNode{s.phase[0], s.phase[1], s.phase[2], s.phase[3]},
	}
}

// Amplitude returns the node ShakeEffect's Amplitude follows, 0 at rest.
func (s *Shake) Amplitude() core.AnimatedNode { return s.amplitude }

// Trigger shakes the frame by amplitude pixels at first, decaying to 0 over d, easing out, in place of a shake still running, with the next phases the seed gives. It reports whether the shake runs: it does not under reduced motion, nor for an amplitude or a duration of 0 or less, which leave a running shake alone and draw no phases. Stop, not Trigger(0, d), ends a running shake: unlike tilemap.Camera.Shake, where an intensity or a duration of 0 or less cancels.
func (s *Shake) Trigger(amplitude float64, d time.Duration) bool {
	if !(amplitude > 0) || math.IsInf(amplitude, 1) || d <= 0 || reduced(s.cx, s.cfg.ReduceMotion) {
		return false
	}
	s.Stop()
	for _, p := range s.phase {
		p.Set(s.rng.Float64() * 2 * math.Pi)
	}
	s.amplitude.Set(amplitude)
	s.run = s.cx.StartAnimation(core.Parallel(
		core.Timing(s.amplitude, 0, core.TimingConfig{Duration: d, Easing: core.BezierEasing(core.EaseOut)}),
		core.Timing(s.time, d.Seconds(), core.TimingConfig{Duration: d, Easing: core.BezierEasing(core.Linear)}),
	), nil)
	return true
}

// Stop ends a running shake at once, with the frame back in place.
func (s *Shake) Stop() {
	if s.run != nil {
		s.run.Stop()
		s.run = nil
	}
	s.amplitude.Set(0)
	s.time.Set(0)
}
