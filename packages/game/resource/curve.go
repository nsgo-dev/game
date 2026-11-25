package resource

import (
	"fmt"
	"math"
	"sort"
)

// Interp is how a key's value moves to the next key's.
type Interp string

const (
	// InterpLinear moves in a straight line; it is the default when a key leaves interp empty.
	InterpLinear Interp = "linear"
	// InterpStep holds the key's value until the next key.
	InterpStep Interp = "step"
	// InterpSmooth eases in and out with a smoothstep.
	InterpSmooth Interp = "smooth"
)

// Key is one keyed value on a Curve or a numeric animation Track.
type Key struct {
	// Time is where the key sits; keys must be in strictly increasing Time order.
	Time float32 `yaml:"time"`
	// Value is the value at Time.
	Value float32 `yaml:"value"`
	// Interp is how the value moves from this key to the next; empty is InterpLinear.
	Interp Interp `yaml:"interp,omitempty"`
}

// FrameKey is one discrete sprite frame key on an animation Track, holding an atlas frame name from Time until the next key without interpolation.
type FrameKey struct {
	// Time is where the frame starts, from 0 to the clip's Length; frame keys must be in strictly increasing Time order.
	Time float32 `yaml:"time"`
	// Frame is the atlas frame name shown from Time until the next key; it must not be empty.
	Frame string `yaml:"frame"`
}

func validateKeys(where string, keys []Key, minTime, maxTime float32) error {
	if len(keys) == 0 {
		return fmt.Errorf("resource: %s needs at least one key", where)
	}
	for i, k := range keys {
		if !isFinite(k.Time) || !isFinite(k.Value) {
			return fmt.Errorf("resource: %s key %d must have a finite time and value", where, i)
		}
		if k.Time < minTime || k.Time > maxTime {
			return fmt.Errorf("resource: %s key %d time %v must be in [%v, %v]", where, i, k.Time, minTime, maxTime)
		}
		if i > 0 && k.Time <= keys[i-1].Time {
			return fmt.Errorf("resource: %s key %d time %v must be after the previous key's %v", where, i, k.Time, keys[i-1].Time)
		}
		switch k.Interp {
		case "", InterpLinear, InterpStep, InterpSmooth:
		default:
			return fmt.Errorf("resource: %s key %d interp %q must be %q, %q or %q", where, i, k.Interp, InterpLinear, InterpStep, InterpSmooth)
		}
	}
	return nil
}

func validateFrameKeys(where string, frames []FrameKey, minTime, maxTime float32) error {
	if len(frames) == 0 {
		return fmt.Errorf("resource: %s needs at least one frame key", where)
	}
	for i, k := range frames {
		if !isFinite(k.Time) {
			return fmt.Errorf("resource: %s frame %d must have a finite time", where, i)
		}
		if k.Time < minTime || k.Time > maxTime {
			return fmt.Errorf("resource: %s frame %d time %v must be in [%v, %v]", where, i, k.Time, minTime, maxTime)
		}
		if i > 0 && k.Time <= frames[i-1].Time {
			return fmt.Errorf("resource: %s frame %d time %v must be after the previous frame's %v", where, i, k.Time, frames[i-1].Time)
		}
		if k.Frame == "" {
			return fmt.Errorf("resource: %s frame %d has an empty frame name", where, i)
		}
	}
	return nil
}

// evalFrameKeys returns the frame name held at t, holding the first frame before it and stepping discretely at each key's time without interpolation.
func evalFrameKeys(frames []FrameKey, t float32) string {
	n := len(frames)
	if n == 0 {
		return ""
	}
	if t <= frames[0].Time || math.IsNaN(float64(t)) {
		return frames[0].Frame
	}
	if t >= frames[n-1].Time {
		return frames[n-1].Frame
	}
	// The first key strictly after t; frames[i-1].Time <= t < frames[i].Time.
	i := sort.Search(n, func(i int) bool { return frames[i].Time > t })
	return frames[i-1].Frame
}

// evalKeys returns the value of keys at t, holding the first and last values beyond the ends.
func evalKeys(keys []Key, t float32) float32 {
	n := len(keys)
	if n == 0 {
		return 0
	}
	if t <= keys[0].Time || math.IsNaN(float64(t)) {
		return keys[0].Value
	}
	if t >= keys[n-1].Time {
		return keys[n-1].Value
	}
	// The first key after t; t is strictly inside (keys[i-1].Time, keys[i].Time].
	i := sort.Search(n, func(i int) bool { return keys[i].Time > t })
	a, b := keys[i-1], keys[i]
	u := (t - a.Time) / (b.Time - a.Time)
	switch a.Interp {
	case InterpStep:
		return a.Value
	case InterpSmooth:
		u = u * u * (3 - 2*u)
	}
	return a.Value + (b.Value-a.Value)*u
}

// Curve is a value keyed over time, such as an easing, a particle's size over its life or damage over distance.
type Curve struct {
	Header `yaml:",inline"`
	// Name is an optional human-readable name.
	Name string `yaml:"name,omitempty"`
	// Keys are the curve's keys in strictly increasing Time order; there must be at least one.
	Keys []Key `yaml:"keys"`
}

// Kind returns KindCurve.
func (c *Curve) Kind() Kind { return KindCurve }

// Validate checks the header and that the keys are finite, ordered and use a known interp.
func (c *Curve) Validate() error {
	if c == nil {
		return fmt.Errorf("resource: nil curve")
	}
	if err := c.Header.validate(KindCurve); err != nil {
		return err
	}
	return validateKeys("curve", c.Keys, -math.MaxFloat32, math.MaxFloat32)
}

// Eval returns the curve's value at t, holding the first and last keys' values before and after them. Its method value fits tween.EaseFunc.
func (c *Curve) Eval(t float32) float32 {
	if c == nil {
		return 0
	}
	return evalKeys(c.Keys, t)
}

// Animation is a clip of property tracks keyed over its length, played on a scene object or node.
type Animation struct {
	Header `yaml:",inline"`
	// Name is an optional human-readable name.
	Name string `yaml:"name,omitempty"`
	// Length is the clip's length in seconds; it must be positive.
	Length float32 `yaml:"length"`
	// Loop plays the clip again from the start when it ends.
	Loop bool `yaml:"loop,omitempty"`
	// Tracks are the clip's keyed properties; each target and property pair appears once.
	Tracks []Track `yaml:"tracks,omitempty"`
	// Events are named cues on the clip's timeline in time order, such as a footstep sound, which a player reports as playback crosses them.
	Events []Event `yaml:"events,omitempty"`
}

// Event is one named cue on an Animation's timeline.
type Event struct {
	// Time is where the event sits, from 0 to the clip's Length; events must be in non-decreasing Time order.
	Time float32 `yaml:"time"`
	// Name says what happens, such as "footstep"; it must not be empty.
	Name string `yaml:"name"`
}

// Track keys one property of one target over an Animation's length: numeric values in Keys or discrete sprite frames in Frames.
type Track struct {
	// Target names the object the track drives, such as the ID of an object in a scene (animclip binds it that way); empty is the object the clip plays on.
	Target string `yaml:"target,omitempty"`
	// Property names the property, such as "x", "rotation", "alpha" or "sprite.frame".
	Property string `yaml:"property"`
	// Keys are the track's numeric keys, with times from 0 to the clip's Length. A track sets either Keys or Frames, not both.
	Keys []Key `yaml:"keys,omitempty"`
	// Frames are the track's discrete sprite frame keys, with times from 0 to the clip's Length, stepping without interpolation. A track sets either Keys or Frames, not both.
	Frames []FrameKey `yaml:"frames,omitempty"`
}

// IsFrameTrack reports whether tr holds discrete sprite frame keys rather than numeric keys.
func (tr *Track) IsFrameTrack() bool {
	return tr != nil && len(tr.Frames) > 0
}

// Kind returns KindAnimation.
func (a *Animation) Kind() Kind { return KindAnimation }

// Validate checks the header, the length, every track and every event.
func (a *Animation) Validate() error {
	if a == nil {
		return fmt.Errorf("resource: nil animation")
	}
	if err := a.Header.validate(KindAnimation); err != nil {
		return err
	}
	if !isFinite(a.Length) || a.Length <= 0 {
		return fmt.Errorf("resource: animation length %v must be finite and positive", a.Length)
	}
	type pair struct{ target, property string }
	seen := make(map[pair]struct{}, len(a.Tracks))
	for i := range a.Tracks {
		tr := &a.Tracks[i]
		if tr.Property == "" {
			return fmt.Errorf("resource: animation track %d has an empty property", i)
		}
		p := pair{tr.Target, tr.Property}
		if _, dup := seen[p]; dup {
			return fmt.Errorf("resource: animation has two tracks for target %q property %q", tr.Target, tr.Property)
		}
		seen[p] = struct{}{}
		where := fmt.Sprintf("animation track %q %q", tr.Target, tr.Property)
		if len(tr.Keys) > 0 && len(tr.Frames) > 0 {
			return fmt.Errorf("resource: %s cannot have both keys and frames", where)
		}
		if len(tr.Frames) > 0 {
			if err := validateFrameKeys(where, tr.Frames, 0, a.Length); err != nil {
				return err
			}
		} else {
			if err := validateKeys(where, tr.Keys, 0, a.Length); err != nil {
				return err
			}
		}
	}
	for i, ev := range a.Events {
		if ev.Name == "" {
			return fmt.Errorf("resource: animation event %d has an empty name", i)
		}
		if !isFinite(ev.Time) || ev.Time < 0 || ev.Time > a.Length {
			return fmt.Errorf("resource: animation event %d time %v must be in [0, %v]", i, ev.Time, a.Length)
		}
		if i > 0 && ev.Time < a.Events[i-1].Time {
			return fmt.Errorf("resource: animation event %d time %v must not be before the previous event's %v", i, ev.Time, a.Events[i-1].Time)
		}
	}
	return nil
}

// ClipTime maps a play time t in seconds to a time within the clip: wrapped into [0, Length) when Loop is set, clamped to [0, Length] otherwise.
func (a *Animation) ClipTime(t float32) float32 {
	if a == nil || a.Length <= 0 || math.IsNaN(float64(t)) {
		return 0
	}
	if a.Loop {
		m := float32(math.Mod(float64(t), float64(a.Length)))
		if m < 0 {
			m += a.Length
		}
		return m
	}
	return min(max(t, 0), a.Length)
}

// Sample evaluates every numeric track at play time t (mapped through ClipTime) and calls set with each track's target, property and value, in track order. Discrete frame tracks are skipped; use SampleFrames for them.
func (a *Animation) Sample(t float32, set func(target, property string, value float32)) {
	if a == nil || set == nil {
		return
	}
	ct := a.ClipTime(t)
	for i := range a.Tracks {
		tr := &a.Tracks[i]
		if len(tr.Keys) == 0 {
			continue
		}
		set(tr.Target, tr.Property, evalKeys(tr.Keys, ct))
	}
}

// SampleFrames evaluates every discrete frame track at play time t (mapped through ClipTime) and calls set with each track's target, property and frame name, in track order.
func (a *Animation) SampleFrames(t float32, set func(target, property, frame string)) {
	if a == nil || set == nil {
		return
	}
	ct := a.ClipTime(t)
	for i := range a.Tracks {
		tr := &a.Tracks[i]
		if len(tr.Frames) == 0 {
			continue
		}
		set(tr.Target, tr.Property, evalFrameKeys(tr.Frames, ct))
	}
}

// Eval returns the numeric track's value at clip time t, holding the first and last keys' values before and after them.
func (tr *Track) Eval(t float32) float32 {
	if tr == nil {
		return 0
	}
	return evalKeys(tr.Keys, t)
}

// EvalFrame returns the discrete frame track's frame name at clip time t, holding the first frame before it and stepping at each key's time without interpolation.
func (tr *Track) EvalFrame(t float32) string {
	if tr == nil {
		return ""
	}
	return evalFrameKeys(tr.Frames, t)
}
