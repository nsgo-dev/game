package bullets

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/mempool"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// Bullet is one live bullet of a Field. A field owns its bullets: a game reads them, for hit tests, and removes them with Field.RemoveFunc, but does not keep a pointer past the next Update, RemoveFunc or Clear, as the field reuses a removed bullet for a later one.
type Bullet struct {
	// X and Y are where the bullet is, in the field's coordinates.
	X, Y float32
	// Speed is how fast it moves, in points a second.
	Speed float32
	// Heading is the direction it moves, in radians clockwise from +X.
	Heading float32
	// Radius is its hit radius in points.
	Radius float32
	// Age is how long it has lived.
	Age time.Duration
	// Lifetime is how long it lives; 0 keeps it until it leaves the field's bounds or is removed.
	Lifetime time.Duration
	// Pattern is the name of the pattern that fired it.
	Pattern string
	// Shot is the index of the emitter's shot that fired it, from 0.
	Shot int
	// Sprite draws it; the field moves the sprite with the bullet.
	Sprite sprite.Sprite

	dirX, dirY  float32
	accel, turn float32
	face        bool
}

// Pos returns where the bullet is.
func (b *Bullet) Pos() math2d.Vec2 { return math2d.Vec2{X: b.X, Y: b.Y} }

// Velocity returns the bullet's velocity in points a second.
func (b *Bullet) Velocity() math2d.Vec2 {
	return math2d.Vec2{X: b.dirX * b.Speed, Y: b.dirY * b.Speed}
}

// Circle returns the bullet's hit circle.
func (b *Bullet) Circle() math2d.Circle {
	return math2d.Circle{Center: b.Pos(), Radius: b.Radius}
}

// setHeading points the bullet at h radians and turns its sprite with it when it faces its heading.
func (b *Bullet) setHeading(h float32) {
	b.Heading = h
	s, c := math.Sincos(float64(h))
	b.dirX, b.dirY = float32(c), float32(s)
	if b.face {
		b.Sprite.Rotation = h
	}
}

// advance moves the bullet on by dt: it curves, speeds up or slows down, then moves at its new velocity.
func (b *Bullet) advance(dt time.Duration) {
	sec := float32(dt.Seconds())
	if b.turn != 0 {
		b.setHeading(b.Heading + b.turn*sec)
	}
	if b.accel != 0 {
		b.Speed = max(0, b.Speed+b.accel*sec)
	}
	step := b.Speed * sec
	b.X += b.dirX * step
	b.Y += b.dirY * step
	b.Age += dt
	b.Sprite.X, b.Sprite.Y = b.X, b.Y
}

// Field holds a shooter's live bullets: it takes them from a pool, moves them, removes the ones that expire or leave its bounds and draws them through one sprite batch.
//
// A Field is not safe for concurrent use.
type Field struct {
	// Atlas holds the frames the bullets draw; emitters look their pattern's frame up in it.
	Atlas *sprite.Atlas
	// Bounds, when not empty, is the area bullets live in: Update removes a bullet once its hit circle lies wholly outside it.
	Bounds math2d.Rect
	// Limit, when above 0, is the most bullets the field holds: a shot spawns none past it.
	Limit int

	pool  *mempool.Pool[Bullet]
	live  []*Bullet
	batch sprite.Batch
}

// NewField returns a field drawing from atlas that keeps capacity bullets ready, so that it spawns up to capacity bullets at once without allocating.
func NewField(atlas *sprite.Atlas, capacity int) *Field {
	capacity = max(capacity, 0)
	return &Field{
		Atlas: atlas,
		pool:  mempool.NewPool[Bullet](capacity, nil),
		live:  make([]*Bullet, 0, capacity),
		batch: sprite.Batch{Sprites: make([]*sprite.Sprite, 0, capacity)},
	}
}

// Len returns the number of live bullets.
func (f *Field) Len() int { return len(f.live) }

// Bullets returns the live bullets, oldest first. The slice and the bullets are the field's: they are valid until the next Update, spawn, RemoveFunc or Clear.
func (f *Field) Bullets() []*Bullet { return f.live }

// Update moves every bullet on by dt, then removes the ones that have lived their lifetime or left the field's bounds, keeping the others in order.
func (f *Field) Update(dt time.Duration) {
	if dt <= 0 {
		return
	}
	cull := !f.Bounds.Empty()
	minX, minY := f.Bounds.X, f.Bounds.Y
	maxX, maxY := f.Bounds.X+f.Bounds.W, f.Bounds.Y+f.Bounds.H
	n := 0
	for _, b := range f.live {
		b.advance(dt)
		if (b.Lifetime > 0 && b.Age >= b.Lifetime) ||
			(cull && (b.X+b.Radius < minX || b.X-b.Radius > maxX || b.Y+b.Radius < minY || b.Y-b.Radius > maxY)) {
			f.pool.Release(b)
			continue
		}
		f.live[n] = b
		n++
	}
	clear(f.live[n:])
	f.live = f.live[:n]
}

// RemoveFunc removes the bullets for which remove returns true, such as those that hit the player, keeping the others in order, and returns how many it removed.
func (f *Field) RemoveFunc(remove func(b *Bullet) bool) int {
	n := 0
	for _, b := range f.live {
		if remove(b) {
			f.pool.Release(b)
			continue
		}
		f.live[n] = b
		n++
	}
	removed := len(f.live) - n
	clear(f.live[n:])
	f.live = f.live[:n]
	return removed
}

// Clear removes every bullet, keeping the field's memory for the next ones.
func (f *Field) Clear() {
	for _, b := range f.live {
		f.pool.Release(b)
	}
	clear(f.live)
	f.live = f.live[:0]
}

// spawn takes a bullet from the pool, or returns nil when the field holds Limit bullets.
func (f *Field) spawn() *Bullet {
	if f.Limit > 0 && len(f.live) >= f.Limit {
		return nil
	}
	b := f.pool.Acquire()
	f.live = append(f.live, b)
	return b
}

// Draw draws the bullets into d through the field's sprite batch, lowest Z first and oldest first for the same Z. It draws nothing when the field has no atlas.
func (f *Field) Draw(d *core.Drawing) {
	if f.Atlas == nil || len(f.live) == 0 {
		return
	}
	f.batch.Atlas = f.Atlas
	f.batch.Sprites = f.batch.Sprites[:0]
	for _, b := range f.live {
		f.batch.Sprites = append(f.batch.Sprites, &b.Sprite)
	}
	f.batch.Draw(d)
	clear(f.batch.Sprites)
}
