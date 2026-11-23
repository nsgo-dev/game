package rooms

import (
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

// Start puts the camera in the room holding target, clamped to it, with no transition, and reports whether a room holds target. Use it to spawn the player or after loading a save; when no room holds target the Director keeps its state.
func (d *Director) Start(target core.Point) bool {
	if d.Camera == nil {
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
	d.setBounds(r)
	d.snap(target)
	return true
}

// Update advances the Director by one fixed step of dt toward target, usually the player's position, and reports whether target entered a room on this step.
//
// While target stays in the current room it stays there, even where rooms overlap. When target leaves it for another room, Update clamps the camera to that room and starts its transition. When target is in no room, the camera keeps to the current one. Before the first room is found, Update behaves as Start.
func (d *Director) Update(target core.Point, dt time.Duration) bool {
	if d.Camera == nil {
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

// FadeAlpha returns the opacity of the fade's cover, from 0 to 1, or 0 when no fade is playing.
func (d *Director) FadeAlpha() float32 {
	if !d.active || d.tr.Kind != KindFade {
		return 0
	}
	p := d.Progress()
	x := 2 * p
	if d.cutDone {
		x = 2 * (1 - p)
	}
	return float32(d.curve.At(min(1, max(0, x))))
}

// DrawFade covers the view with the fade's color at FadeAlpha, and draws nothing when no fade is playing. Call it in view space, after the world and outside Camera.Apply, so the cover fills the Camera.Width by Camera.Height view.
func (d *Director) DrawFade(dr *core.Drawing) {
	a := d.FadeAlpha()
	if a <= 0 || dr == nil || d.Camera == nil || d.Camera.Width <= 0 || d.Camera.Height <= 0 {
		return
	}
	dr.Save()
	dr.SetAlpha(a)
	dr.SetFillColor(d.tr.FadeColor())
	dr.FillRect(0, 0, d.Camera.Width, d.Camera.Height)
	dr.Restore()
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

	// A fade interrupted by another room change keeps its cover where it is: in its first half the new fade continues from the same fraction, and in its second half it mirrors back to the same opacity.
	frac := 0.0
	if d.active && d.tr.Kind == KindFade && tr.Kind == KindFade {
		frac = d.Progress()
		if d.cutDone {
			frac = 1 - frac
		}
	}

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
