package sprite

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// Sprite is a frame of an atlas placed in a drawing: moved, turned, scaled, flipped and faded about its origin.
//
// Its zero value draws nothing, as its scale and alpha are 0: NewSprite returns one at full size and opacity.
//
// A sprite has no tint: a Canvas has no way to recolor one image alone (no layer to composite it in), and a composite mode such as multiply would mix with everything drawn under it.
type Sprite struct {
	// Frame is the picture it draws now; a zero Frame draws nothing.
	Frame Frame
	// X and Y are where its origin goes in the drawing.
	X, Y float32
	// OriginX and OriginY are the point of the untrimmed frame, in its points from its top left, that sits at X, Y and that it turns and scales about.
	OriginX, OriginY float32
	// Rotation turns it about its origin, in radians, clockwise on the screen.
	Rotation float32
	// ScaleX and ScaleY scale it about its origin; a negative one mirrors it there.
	ScaleX, ScaleY float32
	// FlipX and FlipY mirror the picture inside its untrimmed frame, left to right and top to bottom, before it is placed, so the origin keeps its place in the frame's box.
	FlipX, FlipY bool
	// Alpha is its opacity, from 0, which draws nothing, to 1. It replaces the drawing's alpha for the sprite, as Drawing.SetAlpha does.
	Alpha float32
	// Z orders the sprites of a batch: a higher Z draws over a lower one, and sprites with the same Z draw in the batch's order.
	Z int
}

// NewSprite returns a sprite of f at full size and opacity, its origin at the frame's pivot (the top left when f has none).
func NewSprite(f Frame) Sprite {
	return Sprite{Frame: f, OriginX: f.PivotX, OriginY: f.PivotY, ScaleX: 1, ScaleY: 1, Alpha: 1}
}

// Batch draws sprites of one atlas into a drawing, in Z order.
type Batch struct {
	// Atlas is where the sprites' frames are.
	Atlas *Atlas
	// Sprites are the sprites it draws.
	Sprites []*Sprite

	order []*Sprite
}

// Draw draws the batch's sprites into d with one DrawImageFrom each, lowest Z first. It leaves d's state as it found it: a sprite that turns, mirrors or fades draws between Save and Restore.
func (b *Batch) Draw(d *core.Drawing) {
	b.order = append(b.order[:0], b.Sprites...)
	slices.SortStableFunc(b.order, func(x, y *Sprite) int { return cmp.Compare(x.Z, y.Z) })
	for _, s := range b.order {
		s.draw(d, b.Atlas.Image)
	}
	clear(b.order)
}

func (s *Sprite) draw(d *core.Drawing, img core.ImageSource) {
	f := s.Frame
	if f.W <= 0 || f.H <= 0 || s.Alpha <= 0 || s.ScaleX == 0 || s.ScaleY == 0 {
		return
	}
	// The matrix a, b, c, dd, e, ff takes a point of the untrimmed frame to the drawing: mirrored in the frame's box (a flip takes x to Width-x), moved so the origin is at 0, 0, scaled, turned and moved to X, Y.
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
	sin, cos := float32(0), float32(1)
	if s.Rotation != 0 {
		sn, cs := math.Sincos(float64(s.Rotation))
		sin, cos = float32(sn), float32(cs)
	}
	a, b := cos*sx, sin*sx
	c, dd := -sin*sy, cos*sy
	e := s.X + cos*u0 - sin*v0
	ff := s.Y + sin*u0 + cos*v0

	// Unturned and unmirrored, the frame's rectangle is only moved and scaled, and draws with no transform.
	plain := b == 0 && c == 0 && a > 0 && dd > 0
	saved := !plain || s.Alpha < 1
	if saved {
		d.Save()
	}
	if s.Alpha < 1 {
		d.SetAlpha(s.Alpha)
	}
	if plain {
		d.DrawImageFrom(img, f.X, f.Y, f.W, f.H, e+a*f.OffsetX, ff+dd*f.OffsetY, a*f.W, dd*f.H)
	} else {
		d.Transform(a, b, c, dd, e, ff).DrawImageFrom(img, f.X, f.Y, f.W, f.H, f.OffsetX, f.OffsetY, f.W, f.H)
	}
	if saved {
		d.Restore()
	}
}
