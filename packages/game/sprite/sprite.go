package sprite

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

// Sprite is a frame of an atlas placed in a drawing: moved, turned, scaled, flipped, faded, tinted, blended and masked about its origin.
//
// Its zero value draws nothing, as its scale and alpha are 0: NewSprite returns one at full size and opacity. Nor does a sprite whose position, origin, rotation, scale or alpha is NaN or infinite.
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

// Batch draws sprites of one atlas into a drawing, in Z order. A batch with no Atlas draws nothing.
type Batch struct {
	// Atlas is where the sprites' frames are.
	Atlas *Atlas
	// Sprites are the sprites it draws.
	Sprites []*Sprite
	// Mask, if set, clips the whole batch to a shape in the drawing's coordinates.
	Mask *Mask

	order []*Sprite
	// rects, xforms and alphas are the arguments of the DrawAtlas that Draw is collecting, lone is its first sprite and faded whether its sprites have an Alpha below 1.
	rects  []core.Bounds
	xforms []core.RSXform
	alphas []float32
	lone   *Sprite
	faded  bool
}

// Draw draws the batch's sprites into d, lowest Z first, and leaves d's state as it found it.
//
// Consecutive sprites (in Z order) that are only moved, turned and scaled evenly, with the default blend, no tint and no mask of their own, draw with one Drawing.DrawAtlas. A run of sprites at full opacity keeps the drawing's alpha, as a lone such sprite does; a run of faded sprites draws between Save and Restore with the alpha set to 1, so that each sprite's Alpha replaces the drawing's, as Sprite.Alpha says. A sprite that is mirrored, scaled unevenly, tinted, blended or masked, and a run of one, draws with its own DrawImageFrom, or DrawTintedImageFrom when tinted, between Save and Restore when it turns, mirrors, fades, blends or clips.
func (b *Batch) Draw(d *core.Drawing) {
	if b.Atlas == nil {
		return
	}
	draw := func(d *core.Drawing) {
		b.order = append(b.order[:0], b.Sprites...)
		slices.SortStableFunc(b.order, func(x, y *Sprite) int { return cmp.Compare(x.Z, y.Z) })
		img := b.Atlas.Image
		for _, s := range b.order {
			if s.hidden() {
				continue
			}
			t, ok := s.atlasForm()
			if !ok {
				b.flush(d, img)
				s.draw(d, img)
				continue
			}
			faded := s.Alpha < 1
			if len(b.rects) > 0 && faded != b.faded {
				b.flush(d, img)
			}
			if len(b.rects) == 0 {
				b.lone, b.faded = s, faded
			}
			f := s.Frame
			b.rects = append(b.rects, core.Bounds{
				Origin: core.Point{X: core.Points(f.X), Y: core.Points(f.Y)},
				Size:   core.Size{Width: core.Points(f.W), Height: core.Points(f.H)},
			})
			b.xforms = append(b.xforms, t)
			b.alphas = append(b.alphas, s.Alpha)
		}
		b.flush(d, img)
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

// flush draws the run Draw has collected and empties it.
func (b *Batch) flush(d *core.Drawing, img core.ImageSource) {
	switch {
	case len(b.rects) == 1:
		b.lone.draw(d, img)
	case len(b.rects) > 1 && b.faded:
		d.Save().SetAlpha(1).DrawAtlas(img, b.rects, b.xforms, b.alphas).Restore()
	case len(b.rects) > 1:
		d.DrawAtlas(img, b.rects, b.xforms, nil)
	}
	b.rects, b.xforms, b.alphas, b.lone = b.rects[:0], b.xforms[:0], b.alphas[:0], nil
}

// tinted reports whether the sprite's Tint changes its picture: the zero color and opaque white leave it unchanged, so they draw untinted, and the zero color, though transparent black, is the one alpha-0 tint that still draws.
func (s *Sprite) tinted() bool { return s.Tint != 0 && s.Tint != untinted }

// hidden reports whether the sprite draws nothing: an empty frame, no opacity, a zero scale, a tint with no alpha, which multiplies every pixel's alpha to 0, a mask with no area, or a NaN or infinite placement or alpha, which DrawAtlas would drop and DrawImageFrom would draw nowhere.
func (s *Sprite) hidden() bool {
	f := s.Frame
	return f.W <= 0 || f.H <= 0 || s.Alpha <= 0 || s.ScaleX == 0 || s.ScaleY == 0 || s.tinted() && s.Tint&0xff == 0 || s.Mask != nil && s.Mask.state() == maskEmpty ||
		!finite(s.X) || !finite(s.Y) || !finite(s.OriginX) || !finite(s.OriginY) || !finite(s.Rotation) || !finite(s.ScaleX) || !finite(s.ScaleY) || !finite(s.Alpha)
}

// finite reports whether v is neither NaN nor infinite.
func finite(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }

// atlasForm returns the transform that places the sprite's trimmed rectangle for Drawing.DrawAtlas, and false when the sprite cannot draw there: DrawAtlas only moves, turns and scales evenly, and has no tint, blend or clip of a tile's own.
func (s *Sprite) atlasForm() (core.RSXform, bool) {
	if s.tinted() || s.Blend != core.CompositeSourceOver || s.Mask != nil && s.Mask.state() == maskActive {
		return core.RSXform{}, false
	}
	m := s.Transform()
	// An even scale and a turn, unmirrored, is the matrix [c -s; s c].
	if m.A != m.D || m.B != -m.C {
		return core.RSXform{}, false
	}
	f := s.Frame
	return core.RSXform{SCos: m.A, SSin: m.B, TX: m.Tx + m.A*f.OffsetX + m.C*f.OffsetY, TY: m.Ty + m.B*f.OffsetX + m.D*f.OffsetY}, true
}

func (s *Sprite) draw(d *core.Drawing, img core.ImageSource) {
	if s.hidden() {
		return
	}
	f := s.Frame
	tinted := s.tinted()
	masked := s.Mask != nil && s.Mask.state() == maskActive
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
