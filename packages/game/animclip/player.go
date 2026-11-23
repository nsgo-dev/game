package animclip

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tween"
	"github.com/nsgo-dev/nsgo/core"
)

// Binder connects a clip track to what it drives: given the track's target and resolved property, it returns the function that writes the property's value. The value it receives is already converted for the property's kind and range (Property.Convert). An error fails NewPlayer, so a clip that names a missing object is refused before it plays.
type Binder func(target string, p Property) (set func(v float64), err error)

// Player plays one animation clip on a tween.Timeline: Play, Pause and Update run it on game.Loop's fixed steps, Seek scrubs it, and the clip's Loop wraps it. It writes nothing until the first Seek or Update.
//
// The player reads the clip once in NewPlayer; to play an edited or reloaded clip, make a new Player.
type Player struct {
	// OnEvent, when set, is called with the name of each clip event playback crosses, in time order. Seek does not call it.
	OnEvent func(name string)

	clip   *resource.Animation
	tl     *tween.Timeline
	length time.Duration
	loop   bool
}

// maxSeconds is the longest clip time a time.Duration holds.
const maxSeconds = float64(math.MaxInt64) / float64(time.Second)

// Seconds converts a clip time in seconds, as keys and events store it, to a time.Duration through KeyValue, so a key at 0.1 sits at exactly 100ms. Values beyond what a Duration holds saturate.
func Seconds(s float32) time.Duration {
	v := KeyValue(s) * float64(time.Second)
	switch {
	case math.IsNaN(v):
		return 0
	case v >= math.MaxInt64:
		return math.MaxInt64
	case v <= math.MinInt64:
		return math.MinInt64
	}
	return time.Duration(math.Round(v))
}

// NewPlayer checks clip against reg (Check), binds every track through bind, and returns a player at time 0. Each pair of neighboring keys becomes one tween from the first key's value to the second's, eased by the first key's interp; a track's value holds its first key before it and its last key after it, as resource.Animation.Sample does.
func NewPlayer(clip *resource.Animation, reg *sceneformat.ComponentRegistry, bind Binder) (*Player, error) {
	if err := Check(clip, reg); err != nil {
		return nil, err
	}
	if bind == nil {
		return nil, errors.New("animclip: nil binder")
	}
	if KeyValue(clip.Length) > maxSeconds {
		return nil, fmt.Errorf("animclip: clip length %v seconds is longer than a time.Duration holds", clip.Length)
	}
	length := Seconds(clip.Length)
	if length <= 0 {
		return nil, fmt.Errorf("animclip: clip length %v seconds is shorter than a nanosecond", clip.Length)
	}
	p := &Player{clip: clip, tl: tween.NewTimeline(), length: length}
	for i := range clip.Tracks {
		tr := &clip.Tracks[i]
		prop, err := Lookup(reg, tr.Property)
		if err != nil {
			return nil, err
		}
		set, err := bind(tr.Target, prop)
		if err != nil {
			return nil, fmt.Errorf("animclip: bind track %d (target %q property %q): %w", i, tr.Target, tr.Property, err)
		}
		if set == nil {
			return nil, fmt.Errorf("animclip: bind track %d (target %q property %q): binder returned no setter", i, tr.Target, tr.Property)
		}
		apply := func(v float64) { set(prop.Convert(v)) }
		keys := tr.Keys
		if len(keys) == 1 {
			v := KeyValue(keys[0].Value)
			p.tl.Add(Seconds(keys[0].Time), tween.New(v, v, 0, core.Linear, lerpLinear, apply))
			continue
		}
		for j := 0; j+1 < len(keys); j++ {
			a, b := keys[j], keys[j+1]
			start := Seconds(a.Time)
			p.tl.Add(start, tween.New(KeyValue(a.Value), KeyValue(b.Value), Seconds(b.Time)-start, core.Linear, lerpFor(a.Interp), apply))
		}
	}
	for _, ev := range clip.Events {
		name := ev.Name
		p.tl.Call(Seconds(ev.Time), func() {
			if p.OnEvent != nil {
				p.OnEvent(name)
			}
		})
	}
	// A cue at the clip's end makes the timeline as long as the clip, even when its last key comes earlier.
	p.tl.Call(length, func() {})
	p.SetLoop(clip.Loop)
	return p, nil
}

func lerpFor(interp resource.Interp) func(a, b, t float64) float64 {
	switch interp {
	case resource.InterpStep:
		return lerpStep
	case resource.InterpSmooth:
		return lerpSmooth
	default:
		return lerpLinear
	}
}

func lerpLinear(a, b, t float64) float64 { return a + (b-a)*t }

func lerpSmooth(a, b, t float64) float64 { return a + (b-a)*(t*t*(3-2*t)) }

func lerpStep(a, _, _ float64) float64 { return a }

// Clip returns the clip the player was made from.
func (p *Player) Clip() *resource.Animation { return p.clip }

// Length returns the clip's length.
func (p *Player) Length() time.Duration { return p.length }

// Time returns the playback position, from 0 to Length.
func (p *Player) Time() time.Duration { return p.tl.Time() }

// Playing reports whether the player is playing.
func (p *Player) Playing() bool { return p.tl.Playing() }

// Done reports whether a clip that does not loop has played to its end.
func (p *Player) Done() bool { return p.tl.Done() }

// Loop reports whether playback wraps to the start at the end: the clip's Loop unless SetLoop changed it.
func (p *Player) Loop() bool { return p.loop }

// SetLoop overrides the clip's Loop for this player.
func (p *Player) SetLoop(loop bool) {
	p.loop = loop
	p.tl.SetLoop(loop)
}

// Play starts or resumes playback from the current position. A clip that has played to its end stays there; use Restart.
func (p *Player) Play() { p.tl.Play() }

// Pause stops playback at the current position.
func (p *Player) Pause() { p.tl.Pause() }

// Restart writes the clip's start and plays from it.
func (p *Player) Restart() { p.tl.Restart() }

// Seek moves to at and writes every track's value there without calling OnEvent. A looping clip wraps at into [0, Length); one that does not loop clamps it to [0, Length].
func (p *Player) Seek(at time.Duration) {
	if p.loop {
		at %= p.length
		if at < 0 {
			at += p.length
		}
	}
	p.tl.Seek(at)
}

// Update advances a playing clip by dt, writes every track's value at the new position and calls OnEvent for each event crossed.
func (p *Player) Update(dt time.Duration) { p.tl.Update(dt) }
