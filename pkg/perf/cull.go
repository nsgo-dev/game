package perf

import (
	"math"

	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// Intersects reports whether rectangles a and b overlap with positive area
// using half-open [Origin, Origin+Size) semantics consistent with
// core.Bounds.Contains. Empty or non-finite rectangles never intersect.
func Intersects(a, b core.Bounds) bool {
	if !(a.Size.Width > 0 && a.Size.Height > 0 && b.Size.Width > 0 && b.Size.Height > 0) {
		return false
	}
	return a.Origin.X < b.Origin.X+b.Size.Width &&
		a.Origin.X+a.Size.Width > b.Origin.X &&
		a.Origin.Y < b.Origin.Y+b.Size.Height &&
		a.Origin.Y+a.Size.Height > b.Origin.Y
}

// SpriteBounds computes the exact axis-aligned world-space bounding box of s's
// drawn pixels, accounting for frame trim offsets, origin, scale, flip, and
// rotation identically to sprite.Batch.Draw.
//
// It returns a zero core.Bounds when s is nil, has a zero-sized frame, or has
// zero ScaleX or ScaleY.
func SpriteBounds(s *sprite.Sprite) core.Bounds {
	if s == nil {
		return core.Bounds{}
	}
	f := s.Frame
	if f.W <= 0 || f.H <= 0 || s.ScaleX == 0 || s.ScaleY == 0 {
		return core.Bounds{}
	}

	sx, sy := s.ScaleX, s.ScaleY
	var tx, ty float32
	if s.FlipX {
		sx, tx = -sx, f.Width
	}
	if s.FlipY {
		sy, ty = -sy, f.Height
	}
	u0 := s.ScaleX * (tx - s.OriginX)
	v0 := s.ScaleY * (ty - s.OriginY)

	if s.Rotation == 0 {
		x0 := s.X + u0 + sx*f.OffsetX
		x1 := x0 + sx*f.W
		if x0 > x1 {
			x0, x1 = x1, x0
		}
		y0 := s.Y + v0 + sy*f.OffsetY
		y1 := y0 + sy*f.H
		if y0 > y1 {
			y0, y1 = y1, y0
		}
		return core.Bounds{
			Origin: core.Point{X: core.Points(x0), Y: core.Points(y0)},
			Size:   core.Size{Width: core.Points(x1 - x0), Height: core.Points(y1 - y0)},
		}
	}

	sn, cs := math.Sincos(float64(s.Rotation))
	sin, cos := float32(sn), float32(cs)
	a, b := cos*sx, sin*sx
	c, dd := -sin*sy, cos*sy
	e := s.X + cos*u0 - sin*v0
	ff := s.Y + sin*u0 + cos*v0

	hx := f.W * 0.5
	hy := f.H * 0.5
	cx := f.OffsetX + hx
	cy := f.OffsetY + hy
	wcx := a*cx + c*cy + e
	wcy := b*cx + dd*cy + ff
	whx := abs32(a)*hx + abs32(c)*hy
	why := abs32(b)*hx + abs32(dd)*hy
	return core.Bounds{
		Origin: core.Point{X: core.Points(wcx - whx), Y: core.Points(wcy - why)},
		Size:   core.Size{Width: core.Points(2 * whx), Height: core.Points(2 * why)},
	}
}

// CullSprites appends every sprite from src that is visible (Alpha > 0 and
// whose world-space [SpriteBounds] intersect view) to dst and returns the
// extended slice. Pass dst[:0] to reuse backing storage with 0 allocations.
func CullSprites(view core.Bounds, src []*sprite.Sprite, dst []*sprite.Sprite) []*sprite.Sprite {
	if !(view.Size.Width > 0 && view.Size.Height > 0) {
		return dst
	}
	for _, s := range src {
		if s == nil || s.Alpha <= 0 {
			continue
		}
		if Intersects(view, SpriteBounds(s)) {
			dst = append(dst, s)
		}
	}
	return dst
}

// CullBatch draws only the visible sprites from batch whose world-space bounds
// intersect view into d, preserving Z-order without mutating batch.Sprites.
//
// For zero-allocation repeated draws across frames, use [BatchCuller].
func CullBatch(batch *sprite.Batch, view core.Bounds, d *core.Drawing) {
	if batch == nil || batch.Atlas == nil || d == nil || len(batch.Sprites) == 0 {
		return
	}
	var c BatchCuller
	c.Draw(batch, view, d)
}

// BatchCuller draws only visible sprites from a [sprite.Batch] while reusing
// internal scratch slices across frames for zero steady-state allocations.
type BatchCuller struct {
	scratch sprite.Batch
}

// Draw draws every visible sprite in batch that intersects view into d in Z
// order without mutating batch.Sprites.
func (c *BatchCuller) Draw(batch *sprite.Batch, view core.Bounds, d *core.Drawing) {
	if batch == nil || batch.Atlas == nil || d == nil || len(batch.Sprites) == 0 {
		return
	}
	c.scratch.Atlas = batch.Atlas
	c.scratch.Sprites = CullSprites(view, batch.Sprites, c.scratch.Sprites[:0])
	if len(c.scratch.Sprites) > 0 {
		c.scratch.Draw(d)
	}
	clear(c.scratch.Sprites)
	c.scratch.Sprites = c.scratch.Sprites[:0]
	c.scratch.Atlas = nil
}

// Cull appends every item from items whose bounds (computed by boundsOf)
// intersect view to dst and returns the extended slice. Pass dst[:0] to reuse
// backing storage with 0 allocations.
func Cull[T any](view core.Bounds, items []T, boundsOf func(T) core.Bounds, dst []T) []T {
	if !(view.Size.Width > 0 && view.Size.Height > 0) || boundsOf == nil {
		return dst
	}
	for _, item := range items {
		if Intersects(view, boundsOf(item)) {
			dst = append(dst, item)
		}
	}
	return dst
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
