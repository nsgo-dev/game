//go:build !release

package gamedebug

import (
	"math"

	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
)

// ShapeOverlay draws collision shapes and sprite bounds as wireframes into a core.Drawing under whatever camera or viewport transform is already active on it.
//
// Stroke widths are automatically divided by the drawing's active scale so wireframes keep a steady thickness at any camera zoom.
type ShapeOverlay struct {
	// LineWidth is the desired stroke width in screen points; 1 when 0 or less.
	LineWidth float32
}

// DrawBox strokes the axis-aligned rectangle b in col under d's active transform, leaving d's state unchanged.
func (s *ShapeOverlay) DrawBox(d *core.Drawing, b core.Bounds, col core.Color) {
	if d == nil || b.Size.Width <= 0 || b.Size.Height <= 0 {
		return
	}
	lw := s.strokeWidth(d)
	d.Save().
		SetStrokeColor(col).
		SetLineWidth(lw).
		StrokeRect(float32(b.Origin.X), float32(b.Origin.Y), float32(b.Size.Width), float32(b.Size.Height)).
		Restore()
}

// DrawCircle strokes a circle centered at (x, y) with radius r in col under d's active transform, leaving d's state unchanged.
func (s *ShapeOverlay) DrawCircle(d *core.Drawing, x, y, r float32, col core.Color) {
	if d == nil || r <= 0 {
		return
	}
	lw := s.strokeWidth(d)
	d.Save().
		SetStrokeColor(col).
		SetLineWidth(lw).
		BeginPath().
		Arc(x, y, r, 0, 2*math.Pi, false).
		Stroke().
		Restore()
}

// DrawSpriteBounds strokes the untrimmed bounding box of sp in col, respecting its origin, scale, flip and rotation, under d's active transform.
func (s *ShapeOverlay) DrawSpriteBounds(d *core.Drawing, sp *sprite.Sprite, col core.Color) {
	if d == nil || sp == nil {
		return
	}
	drawSpriteBounds(d, sp, col, s.strokeWidth(d))
}

// DrawBatchBounds strokes the untrimmed bounding box of every visible sprite in batch in col under d's active transform.
func (s *ShapeOverlay) DrawBatchBounds(d *core.Drawing, batch *sprite.Batch, col core.Color) {
	if d == nil || batch == nil || len(batch.Sprites) == 0 {
		return
	}
	lw := s.strokeWidth(d)
	for _, sp := range batch.Sprites {
		drawSpriteBounds(d, sp, col, lw)
	}
}

func (s *ShapeOverlay) strokeWidth(d *core.Drawing) float32 {
	base := float32(1)
	if s != nil && s.LineWidth > 0 {
		base = s.LineWidth
	}
	scale := drawingScale(d)
	if scale <= 1e-6 {
		return base
	}
	return base / scale
}

func drawSpriteBounds(d *core.Drawing, sp *sprite.Sprite, col core.Color, lw float32) {
	if sp == nil {
		return
	}
	f := sp.Frame
	if f.W <= 0 || f.H <= 0 || sp.Alpha <= 0 || sp.ScaleX == 0 || sp.ScaleY == 0 {
		return
	}
	fw, fh := f.Width, f.Height
	if fw <= 0 {
		fw = f.W
	}
	if fh <= 0 {
		fh = f.H
	}

	// Match sprite.Sprite's placement matrix so the wireframe aligns with the untrimmed frame under every origin, flip, scale and rotation.
	sx, sy := sp.ScaleX, sp.ScaleY
	var tx, ty float32
	if sp.FlipX {
		sx, tx = -sx, fw
	}
	if sp.FlipY {
		sy, ty = -sy, fh
	}
	u0 := sp.ScaleX * (tx - sp.OriginX)
	v0 := sp.ScaleY * (ty - sp.OriginY)
	sin, cos := float32(0), float32(1)
	if sp.Rotation != 0 {
		sn, cs := math.Sincos(float64(sp.Rotation))
		sin, cos = float32(sn), float32(cs)
	}
	a, b := cos*sx, sin*sx
	c, dd := -sin*sy, cos*sy
	e := sp.X + cos*u0 - sin*v0
	ff := sp.Y + sin*u0 + cos*v0

	// Transform the four untrimmed corners into the active drawing space so stroke widths stay uniform even when ScaleX != ScaleY.
	x0, y0 := e, ff
	x1, y1 := a*fw+e, b*fw+ff
	x2, y2 := a*fw+c*fh+e, b*fw+dd*fh+ff
	x3, y3 := c*fh+e, dd*fh+ff

	d.Save().SetStrokeColor(col).SetLineWidth(lw)
	if b == 0 && c == 0 {
		minX, maxX := min(x0, x2), max(x0, x2)
		minY, maxY := min(y0, y2), max(y0, y2)
		d.StrokeRect(minX, minY, maxX-minX, maxY-minY)
	} else {
		d.BeginPath().
			MoveTo(x0, y0).
			LineTo(x1, y1).
			LineTo(x2, y2).
			LineTo(x3, y3).
			ClosePath().
			Stroke()
	}
	d.Restore()
}

// mat2 is the linear 2x2 part of a 2D affine transform, with columns (a, b) and (c, d).
type mat2 struct {
	a, b, c, d float32
}

// drawingScale replays the transform operations recorded on d so far and returns the geometric mean of its column lengths.
func drawingScale(d *core.Drawing) float32 {
	if d == nil || len(d.Ops) == 0 {
		return 1
	}
	cur := mat2{a: 1, d: 1}
	var stackBuf [16]mat2
	stack := stackBuf[:0]
	for _, op := range d.Ops {
		switch op.Kind {
		case core.DrawSave:
			stack = append(stack, cur)
		case core.DrawRestore:
			if n := len(stack); n > 0 {
				cur = stack[n-1]
				stack = stack[:n-1]
			}
		case core.DrawScale:
			cur.a *= op.X
			cur.b *= op.X
			cur.c *= op.Y
			cur.d *= op.Y
		case core.DrawRotate:
			sn, cs := math.Sincos(float64(op.A))
			sin, cos := float32(sn), float32(cs)
			cur = mat2{
				a: cur.a*cos + cur.c*sin,
				b: cur.b*cos + cur.d*sin,
				c: -cur.a*sin + cur.c*cos,
				d: -cur.b*sin + cur.d*cos,
			}
		case core.DrawTransform:
			a2, b2, c2, d2 := op.X1, op.Y1, op.X2, op.Y2
			cur = mat2{
				a: cur.a*a2 + cur.c*b2,
				b: cur.b*a2 + cur.d*b2,
				c: cur.a*c2 + cur.c*d2,
				d: cur.b*c2 + cur.d*d2,
			}
		case core.DrawSetTransform:
			cur = mat2{a: op.X1, b: op.Y1, c: op.X2, d: op.Y2}
		}
	}
	sx := math.Hypot(float64(cur.a), float64(cur.b))
	sy := math.Hypot(float64(cur.c), float64(cur.d))
	if sx <= 0 || sy <= 0 {
		return float32(max(sx, sy))
	}
	return float32(math.Sqrt(sx * sy))
}
