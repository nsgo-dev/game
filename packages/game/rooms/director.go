package rooms

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// Director moves a tilemap.Camera between a Map's rooms, clamping it to the room the target is in and playing the room's transition on entry.
//
// The Director owns Camera.X, Camera.Y and Camera.Bounds: a game calls Update once per fixed step instead of Camera.Follow or Camera.Clamp, and keeps calling Camera.Update for shake. Map must not change while the Director uses it; call Start again after replacing it.
type Director struct {
	// Map is the room map the Director moves through.
	Map *Map
	// Camera is the camera the Director moves.
	Camera *tilemap.Camera
	// Smoothing is the Camera.Follow smoothing used outside transitions; <= 0 keeps the camera locked on the target.
	Smoothing float32

	room, prev *Room
	bounds     core.Bounds

	tr           Transition
	curve        core.Curve
	active       bool
	elapsed      time.Duration
	length       time.Duration
	fromX, fromY float32
	cutDone      bool

	// cover is what is left of a fade's cover after a room change into a slide or cut interrupted it; it keeps clearing on its own.
	cover fadingCover
}

// fadingCover is a fade's cover clearing along the second half of that fade: its opacity is curve.At(1 - elapsed/half).
type fadingCover struct {
	color   core.Color
	curve   core.Curve
	half    time.Duration
	elapsed time.Duration
}

func (c *fadingCover) alpha() float32 {
	if c.half <= 0 || c.elapsed >= c.half {
		return 0
	}
	return float32(c.curve.At(1 - float64(c.elapsed)/float64(c.half)))
}

// NewDirector returns a Director that moves cam through m's rooms.
func NewDirector(m *Map, cam *tilemap.Camera) *Director {
	return &Director{Map: m, Camera: cam}
}

// Room returns the room the camera is in or moving into, or nil before the target was first found in a room.
func (d *Director) Room() *Room { return d.room }

// Previous returns the room the camera last left, or nil before the first room change.
func (d *Director) Previous() *Room { return d.prev }

// Transitioning reports whether a slide or fade is playing.
func (d *Director) Transitioning() bool { return d.active }

// Transition returns the transition that is playing, or the last one played; its Kind is empty before the first room change.
func (d *Director) Transition() Transition { return d.tr }

// Progress returns how far the playing transition has run, from 0 to 1 in linear time, or 1 when none is playing.
func (d *Director) Progress() float64 {
	if !d.active || d.length <= 0 {
		return 1
	}
	return min(1, float64(d.elapsed)/float64(d.length))
}

// Start puts the camera in the room holding target, clamped to it, with no transition, and reports whether a room holds target. Use it to spawn the player or after loading a save; when no room holds target, or target has a non-finite coordinate, the Director keeps its state.
func (d *Director) Start(target core.Point) bool {
	if d.Camera == nil || !finitePoint(target) {
		return false
	}
	r := d.Map.RoomAt(target)
	if r == nil {
		return false
	}
	d.room, d.prev = r, nil
	d.tr = Transition{}
	d.active, d.cutDone = false, false
	d.elapsed, d.length = 0, 0
	d.cover = fadingCover{}
	d.setBounds(r)
	d.snap(target)
	return true
}

// Update advances the Director by one fixed step of dt toward target, usually the player's position, and reports whether target entered a room on this step.
//
// While target stays in the current room it stays there, even where rooms overlap. When target leaves it for another room, Update clamps the camera to that room and starts its transition. When target is in no room, the camera keeps to the current one. A target with a non-finite coordinate is ignored: the camera and any transition hold for that step. Before the first room is found, Update behaves as Start.
func (d *Director) Update(target core.Point, dt time.Duration) bool {
	if d.Camera == nil || !finitePoint(target) {
		return false
	}
	if d.room == nil {
		return d.Start(target)
	}
	entered := false
	if !d.room.Contains(target) {
		if r := d.Map.RoomAt(target); r != nil && r != d.room {
			d.enter(r, target)
			entered = true
		}
	}
	d.advance(target, dt)
	return entered
}

// FadeAlpha returns the opacity of the fade's cover, from 0 to 1, or 0 when no cover is showing. A fade interrupted by a room change keeps its cover's opacity: a new fade covers on from it, and a slide or cut lets it clear as the interrupted fade would have.
func (d *Director) FadeAlpha() float32 {
	if !d.active || d.tr.Kind != KindFade {
		return d.cover.alpha()
	}
	p := d.Progress()
	x := 2 * p
	if d.cutDone {
		x = 2 * (1 - p)
	}
	return float32(d.curve.At(min(1, max(0, x))))
}

// DrawFade covers the view with the fade's color at FadeAlpha, and draws nothing when no cover is showing. Call it in view space, after the world and outside Camera.Apply, so the cover fills the Camera.Width by Camera.Height view.
func (d *Director) DrawFade(dr *core.Drawing) {
	a := d.FadeAlpha()
	if a <= 0 || dr == nil || d.Camera == nil || d.Camera.Width <= 0 || d.Camera.Height <= 0 {
		return
	}
	dr.Save()
	dr.SetAlpha(a)
	dr.SetFillColor(d.coverColor())
	dr.FillRect(0, 0, d.Camera.Width, d.Camera.Height)
	dr.Restore()
}

// coverColor returns the color of the cover FadeAlpha measures.
func (d *Director) coverColor() core.Color {
	if d.active && d.tr.Kind == KindFade {
		return d.tr.FadeColor()
	}
	return d.cover.color
}

func (d *Director) setBounds(r *Room) {
	d.bounds = r.Bounds()
	d.Camera.Bounds = &d.bounds
}

// snap centers the camera on target and clamps it to the current room.
func (d *Director) snap(target core.Point) {
	d.Camera.X, d.Camera.Y = float32(target.X), float32(target.Y)
	d.Camera.Clamp()
}

// clamped returns where snap would put the camera, without moving it.
func (d *Director) clamped(target core.Point) (x, y float32) {
	c := tilemap.Camera{
		X: float32(target.X), Y: float32(target.Y),
		Zoom: d.Camera.Zoom, Rotation: d.Camera.Rotation,
		Width: d.Camera.Width, Height: d.Camera.Height,
		Bounds: d.Camera.Bounds,
	}
	c.Clamp()
	return c.X, c.Y
}

func (d *Director) enter(r *Room, target core.Point) {
	tr := d.Map.TransitionInto(r)
	length := tr.Length()

	// A cover showing when the room changes keeps its opacity. A new fade covers on from it: with the same easing, in its first half it continues from the same fraction and in its second half it mirrors back, and with another easing it starts where its own curve reaches that opacity. A slide or cut leaves the cover to clear as the interrupted fade would have.
	alpha := d.FadeAlpha()
	frac := 0.0
	var left fadingCover
	switch {
	case alpha <= 0:
	case tr.Kind == KindFade && length > 0:
		if d.active && d.tr.Kind == KindFade && d.tr.Easing == tr.Easing {
			frac = d.Progress()
			if d.cutDone {
				frac = 1 - frac
			}
		} else {
			frac = inverse(tr.Curve(), float64(alpha)) / 2
		}
	case d.active && d.tr.Kind == KindFade:
		left = fadingCover{color: d.tr.FadeColor(), curve: d.curve, half: d.length / 2}
		left.elapsed = time.Duration((1 - inverse(d.curve, float64(alpha))) * float64(left.half))
	default:
		left = d.cover
	}
	d.cover = left

	d.prev, d.room = d.room, r
	d.setBounds(r)
	d.tr, d.curve, d.length = tr, tr.Curve(), length
	d.cutDone = false
	d.fromX, d.fromY = d.Camera.X, d.Camera.Y
	if length <= 0 {
		d.active, d.elapsed = false, 0
		d.snap(target)
		return
	}
	d.active = true
	d.elapsed = time.Duration(frac * float64(length))
}

func (d *Director) advance(target core.Point, dt time.Duration) {
	if d.cover.half > 0 && dt > 0 {
		d.cover.elapsed = min(d.cover.half, d.cover.elapsed+dt)
		if d.cover.elapsed >= d.cover.half {
			d.cover = fadingCover{}
		}
	}
	if !d.active {
		d.Camera.Follow(target, d.Smoothing, dt)
		return
	}
	if dt > 0 {
		d.elapsed = min(d.length, d.elapsed+dt)
	}
	p := d.Progress()
	done := d.elapsed >= d.length
	switch d.tr.Kind {
	case KindSlide:
		toX, toY := d.clamped(target)
		if done {
			d.Camera.X, d.Camera.Y = toX, toY
		} else {
			e := d.curve.At(p)
			d.Camera.X = lerp(d.fromX, toX, e)
			d.Camera.Y = lerp(d.fromY, toY, e)
		}
	case KindFade:
		switch {
		case !d.cutDone && p >= 0.5:
			d.cutDone = true
			d.snap(target)
		case d.cutDone:
			d.Camera.Follow(target, d.Smoothing, dt)
		}
	}
	if done {
		d.active = false
	}
}

func lerp(a, b float32, t float64) float32 {
	return float32(float64(a) + (float64(b)-float64(a))*t)
}

// inverse returns the x in [0, 1] where the increasing curve c reaches y, found by bisection.
func inverse(c core.Curve, y float64) float64 {
	if y <= 0 {
		return 0
	}
	if y >= 1 {
		return 1
	}
	lo, hi := 0.0, 1.0
	for range 52 {
		mid := (lo + hi) / 2
		if c.At(mid) < y {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

func finitePoint(p core.Point) bool {
	x, y := float64(p.X), float64(p.Y)
	return !math.IsNaN(x) && !math.IsInf(x, 0) && !math.IsNaN(y) && !math.IsInf(y, 0)
}
