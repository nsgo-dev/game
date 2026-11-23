package sprite

import (
	"cmp"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

// Sprite is a frame of an atlas placed in a drawing: moved, turned, scaled, flipped, faded, tinted, blended and masked about its origin.
//
// Its zero value draws nothing, as its scale and alpha are 0: NewSprite returns one at full size and opacity.
//
// Tint recolors only the sprite's own pixels, leaving what is drawn under it unchanged; Blend sets how the sprite mixes with what is already drawn under it, as Drawing.SetComposite does.
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
	// Tint multiplies each of the picture's sRGB and alpha channels by the matching channel of a color before the sprite is composited, as Drawing.DrawTintedImageFrom does: core.RGB(0xff0000) keeps only its red, and a tint's alpha fades it on top of Alpha. Transparent pixels stay transparent, so the tint never reaches what is drawn under the sprite. The zero value, like opaque white, leaves the picture as it is, and any other tint with alpha 0 draws nothing. To fade a sprite out, lower Alpha rather than the tint's alpha: a tint that reaches transparent black is the zero value and draws the picture unchanged.
	Tint core.Color
	// Blend is how the sprite mixes with what is already in the drawing, as Drawing.SetComposite does; the zero value is CompositeSourceOver.
	Blend core.CompositeOp
	// Mask, if set, clips the sprite to a shape in the drawing's coordinates before drawing it.
	Mask *Mask
	// Z orders the sprites of a batch: a higher Z draws over a lower one, and sprites with the same Z draw in the batch's order.
	Z int
}

// NewSprite returns a sprite of f at full size and opacity, its origin at the frame's pivot (the top left when f has none).
func NewSprite(f Frame) Sprite {
	return Sprite{Frame: f, OriginX: f.PivotX, OriginY: f.PivotY, ScaleX: 1, ScaleY: 1, Alpha: 1}
}

// Pos returns the sprite's position (X, Y) in the drawing.
func (s Sprite) Pos() math2d.Vec2 { return math2d.Vec2{X: s.X, Y: s.Y} }

// Origin returns the sprite's origin (OriginX, OriginY) in its untrimmed frame.
func (s Sprite) Origin() math2d.Vec2 { return math2d.Vec2{X: s.OriginX, Y: s.OriginY} }

// Scale returns the sprite's scale (ScaleX, ScaleY).
func (s Sprite) Scale() math2d.Vec2 { return math2d.Vec2{X: s.ScaleX, Y: s.ScaleY} }

// Transform returns the 2D affine transform taking a point of the untrimmed frame to the drawing:
// mirrored in the frame's box when flipped, moved so the origin is at (0, 0), scaled, turned and moved to (X, Y).
func (s Sprite) Transform() math2d.Affine {
	var fx, tx float32 = 1, 0
	if s.FlipX {
		fx, tx = -1, s.Frame.Width
	}
	var fy, ty float32 = 1, 0
	if s.FlipY {
		fy, ty = -1, s.Frame.Height
	}
	local := math2d.Affine{A: fx, D: fy, Tx: tx - s.OriginX, Ty: ty - s.OriginY}
	return math2d.TRS(s.Pos(), s.Rotation, s.Scale()).Mul(local)
}

// Bounds returns the axis-aligned bounding rectangle of the sprite's untrimmed frame in the drawing,
// or the zero Rect when the sprite has an empty frame or zero scale.
func (s Sprite) Bounds() math2d.Rect {
	if s.Frame.W <= 0 || s.Frame.H <= 0 || s.ScaleX == 0 || s.ScaleY == 0 {
		return math2d.Rect{}
	}
	w, h := s.Frame.Width, s.Frame.Height
	if w == 0 && h == 0 {
		w, h = s.Frame.W, s.Frame.H
	}
	return s.Transform().TransformRect(math2d.Rect{W: w, H: h})
}

// Batch draws sprites of one atlas into a drawing, in Z order.
type Batch struct {
	// Atlas is where the sprites' frames are.
	Atlas *Atlas
	// Sprites are the sprites it draws.
	Sprites []*Sprite
	// Mask, if set, clips the whole batch to a shape in the drawing's coordinates.
	Mask *Mask

	order []*Sprite
}

// Draw draws the batch's sprites into d with one DrawImageFrom each, or one DrawTintedImageFrom for a tinted sprite, lowest Z first. It leaves d's state as it found it: a sprite that turns, mirrors, fades, blends or clips draws between Save and Restore.
func (b *Batch) Draw(d *core.Drawing) {
	draw := func(d *core.Drawing) {
		b.order = append(b.order[:0], b.Sprites...)
		slices.SortStableFunc(b.order, func(x, y *Sprite) int { return cmp.Compare(x.Z, y.Z) })
		for _, s := range b.order {
			s.draw(d, b.Atlas.Image)
		}
		clear(b.order)
	}
	if b.Mask != nil {
		DrawMasked(d, *b.Mask, draw)
		return
	}
	draw(d)
}

// untinted is opaque white, the tint that leaves a picture as it is.
var untinted = core.RGB(0xffffff)

func (s *Sprite) draw(d *core.Drawing, img core.ImageSource) {
	f := s.Frame
	if f.W <= 0 || f.H <= 0 || s.Alpha <= 0 || s.ScaleX == 0 || s.ScaleY == 0 {
		return
	}
	// The zero color and opaque white leave the picture unchanged, so they draw untinted. The zero color is also transparent black, so it is the one alpha-0 tint that still draws.
	tinted := s.Tint != 0 && s.Tint != untinted
	if tinted && s.Tint&0xff == 0 {
		// A tint with no alpha multiplies every pixel's alpha to 0.
		return
	}

	var masked bool
	if s.Mask != nil {
		switch s.Mask.state() {
		case maskEmpty:
			return
		case maskActive:
			masked = true
		}
	}
	m := s.Transform()

	// Unturned and unmirrored, the frame's rectangle is only moved and scaled, and draws with no transform.
	plain := m.B == 0 && m.C == 0 && m.A > 0 && m.D > 0
	blended := s.Blend != core.CompositeSourceOver
	saved := !plain || s.Alpha < 1 || blended || masked

	if saved {
		d.Save()
	}
	if masked {
		s.Mask.clip(d)
	}
	if blended {
		d.SetComposite(s.Blend)
	}
	if s.Alpha < 1 {
		d.SetAlpha(s.Alpha)
	}
	x, y, w, h := f.OffsetX, f.OffsetY, f.W, f.H
	if plain {
		x, y, w, h = m.Tx+m.A*f.OffsetX, m.Ty+m.D*f.OffsetY, m.A*f.W, m.D*f.H
	} else {
		d.Transform(m.A, m.B, m.C, m.D, m.Tx, m.Ty)
	}
	if tinted {
		d.DrawTintedImageFrom(img, s.Tint, f.X, f.Y, f.W, f.H, x, y, w, h)
	} else {
		d.DrawImageFrom(img, f.X, f.Y, f.W, f.H, x, y, w, h)
	}
	if saved {
		d.Restore()
	}
}
