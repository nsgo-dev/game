package sprite

import (
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

type maskKind uint8

const (
	maskNone maskKind = iota
	maskRect
	maskCircle
	maskEllipse
	maskPolygon
)

type maskState uint8

const (
	maskUnmasked maskState = iota
	maskEmpty
	maskActive
)

// Mask clips drawing to a rectangle, circle, ellipse, or polygon in the drawing's coordinates.
//
// Its zero value leaves drawing unmasked. A mask whose shape has no area (a non-positive rectangle or radius, or a polygon of fewer than three points) clips everything drawn through it.
type Mask struct {
	kind   maskKind
	x, y   float32
	w, h   float32 // width/height for rect, radii rx/ry for ellipse, radius in w for circle
	points []core.Point
}

// RectMask returns a mask that clips drawing to the rectangle b.
func RectMask(b core.Bounds) Mask {
	return Mask{
		kind: maskRect,
		x:    float32(b.Origin.X),
		y:    float32(b.Origin.Y),
		w:    float32(b.Size.Width),
		h:    float32(b.Size.Height),
	}
}

// CircleMask returns a mask that clips drawing to the circle centered at (cx, cy) with radius r.
func CircleMask(cx, cy, r float32) Mask {
	return Mask{kind: maskCircle, x: cx, y: cy, w: r}
}

// EllipseMask returns a mask that clips drawing to the ellipse centered at (cx, cy) with radii rx and ry.
func EllipseMask(cx, cy, rx, ry float32) Mask {
	return Mask{kind: maskEllipse, x: cx, y: cy, w: rx, h: ry}
}

// PolygonMask returns a mask that clips drawing to the closed polygon formed by points.
func PolygonMask(points []core.Point) Mask {
	return Mask{kind: maskPolygon, points: slices.Clone(points)}
}

// DrawMasked clips d to mask, runs draw, and restores d's state. A zero Mask runs draw without clipping; a mask with no area skips draw.
func DrawMasked(d *core.Drawing, mask Mask, draw func(*core.Drawing)) {
	if draw == nil {
		return
	}
	switch mask.state() {
	case maskUnmasked:
		draw(d)
	case maskEmpty:
		return
	case maskActive:
		d.Save()
		mask.clip(d)
		draw(d)
		d.Restore()
	}
}

func (m Mask) state() maskState {
	switch m.kind {
	case maskNone:
		return maskUnmasked
	case maskRect, maskEllipse:
		if m.w <= 0 || m.h <= 0 {
			return maskEmpty
		}
		return maskActive
	case maskCircle:
		if m.w <= 0 {
			return maskEmpty
		}
		return maskActive
	case maskPolygon:
		if len(m.points) < 3 {
			return maskEmpty
		}
		return maskActive
	default:
		return maskEmpty
	}
}

// clip records the mask's path, clips d to it, and clears the current path so later drawing does not inherit it.
func (m Mask) clip(d *core.Drawing) {
	d.BeginPath()
	switch m.kind {
	case maskRect:
		d.Rect(m.x, m.y, m.w, m.h)
	case maskCircle:
		d.Arc(m.x, m.y, m.w, 0, 2*math.Pi, false)
	case maskEllipse:
		d.Ellipse(m.x, m.y, m.w, m.h)
	case maskPolygon:
		d.MoveTo(float32(m.points[0].X), float32(m.points[0].Y))
		for _, p := range m.points[1:] {
			d.LineTo(float32(p.X), float32(p.Y))
		}
		d.ClosePath()
	}
	d.Clip().BeginPath()
}
