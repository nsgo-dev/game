package sound

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/nsgo/audio"
)

// Rolloff is how an effect's volume falls from Near to Far in a Listener2D.
type Rolloff uint8

const (
	// RolloffLinear falls in a straight line from full volume at Near to silence at Far.
	RolloffLinear Rolloff = iota
	// RolloffInverse falls as Near divided by the distance, as sound does in the open, rescaled to reach silence at Far: steep close to Near and gentle towards Far. With a Near of 0 it falls as RolloffLinear.
	RolloffInverse
)

// Listener2D hears effects placed in a 2D world from where the camera looks, so an effect to the left of the camera plays panned left and an effect further away plays quieter. Its zero value hears every effect at full volume and centered.
//
// Distances are in world units, the same as the positions passed to Pan, Gain and Effect.PlayAt; the camera's zoom does not change them.
type Listener2D struct {
	// Position is where the listener is in the world, usually the 2D camera's center, as tilemap.Camera's X and Y.
	Position math2d.Vec2
	// Rotation turns the listener clockwise in the world, in radians, as tilemap.Camera.Rotation turns the camera, so left and right are the view's.
	Rotation float32
	// Near is the distance up to which an effect plays at full volume; a negative one or NaN counts as 0.
	Near float32
	// Far is the distance at and beyond which an effect is silent. A Far not greater than Near, infinite or NaN turns distance attenuation off, every effect playing at full volume.
	Far float32
	// Rolloff is how the volume falls between Near and Far.
	Rolloff Rolloff
	// PanWidth is how far to the side of the listener, across the view, an effect pans fully left or right, panning in proportion closer in; 0, less or NaN takes Far, and with distance attenuation off every effect plays centered. An effect straight above or below the listener plays centered at any distance.
	PanWidth float32
}

// Pan returns where an effect at pos plays from, -1 for fully left through 0 for centered to 1 for fully right, as audio.Voice.Pan takes it: its offset across the view divided by PanWidth, clamped. A pos with a NaN coordinate plays centered.
func (l Listener2D) Pan(pos math2d.Vec2) float32 {
	width := float64(l.PanWidth)
	if !(width > 0) {
		_, far, ok := l.span()
		if !ok {
			return 0
		}
		width = far
	}
	x, _ := l.toView(pos)
	p := x / width
	if math.IsNaN(p) {
		return 0
	}
	return float32(min(max(p, -1), 1))
}

// Gain returns the volume an effect at pos plays at, from 1 within Near to 0 at Far and beyond, falling by Rolloff between them. A pos with a NaN coordinate is silent, and one with an infinite coordinate too unless attenuation is off.
func (l Listener2D) Gain(pos math2d.Vec2) float32 {
	near, far, ok := l.span()
	if !ok {
		return 1
	}
	x, y := l.toView(pos)
	d := math.Hypot(x, y)
	switch {
	case math.IsNaN(d):
		return 0
	case d <= near:
		return 1
	case d >= far:
		return 0
	}
	var g float64
	if l.Rolloff == RolloffInverse && near > 0 {
		g = (near/d - near/far) / (1 - near/far)
	} else {
		g = (far - d) / (far - near)
	}
	return float32(min(max(g, 0), 1))
}

// Voice returns v with its volume scaled by Gain at pos and its pan set to Pan at pos, the voice an effect at pos plays with. Its volume is 0 where the effect is silent, which audio plays as full volume, so check Gain, or play through Effect.PlayAt, which plays nothing there.
func (l Listener2D) Voice(pos math2d.Vec2, v audio.Voice) audio.Voice {
	v.Volume = voiceVolume(v.Volume) * l.Gain(pos)
	v.Pan = l.Pan(pos)
	return v
}

// span returns Near and Far as float64, with ok false when attenuation is off.
func (l Listener2D) span() (near, far float64, ok bool) {
	near = float64(l.Near)
	if !(near > 0) {
		near = 0
	}
	far = float64(l.Far)
	if !(far > near) || math.IsInf(far, 1) {
		return 0, 0, false
	}
	return near, far, true
}

// toView returns pos's offset from the listener across (x, positive to the right) and down (y) the view, turned by Rotation as tilemap.Camera.WorldToView turns it.
func (l Listener2D) toView(pos math2d.Vec2) (x, y float64) {
	dx := float64(pos.X) - float64(l.Position.X)
	dy := float64(pos.Y) - float64(l.Position.Y)
	if l.Rotation == 0 {
		return dx, dy
	}
	sin, cos := math.Sincos(float64(l.Rotation))
	return dx*cos + dy*sin, -dx*sin + dy*cos
}

// PlayAt plays the effect at pos in the world as l hears it: panned by l.Pan and at v's volume times l.Gain, then the group's and the master volume, as Play does, and reports whether it played. It plays nothing where the effect is silent, beyond l's Far, and replaces v's pan. The pan and volume hold for the playback's whole length, as audio's playbacks cannot change their pan once started.
func (s *Effect) PlayAt(l Listener2D, pos math2d.Vec2, v audio.Voice) bool {
	v = l.Voice(pos, v)
	if !(v.Volume > 0) {
		return false
	}
	return s.Play(v)
}
