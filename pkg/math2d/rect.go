package math2d

import "github.com/nsgo-dev/nsgo/core"

// Rect is an axis-aligned 2D rectangle given by its origin (X, Y) and size (W, H).
//
// A rectangle with W <= 0 or H <= 0 (or any NaN field) is empty.
// Containment uses half-open intervals [X, X+W) × [Y, Y+H), matching NSGo's core.Bounds.
type Rect struct {
	X, Y, W, H float32
}

// FromBounds converts an NSGo core.Bounds to a Rect losslessly.
func FromBounds(b core.Bounds) Rect {
	return Rect{
		X: float32(b.Origin.X),
		Y: float32(b.Origin.Y),
		W: float32(b.Size.Width),
		H: float32(b.Size.Height),
	}
}

// FromCenter returns a Rect centered at center with the given size.
func FromCenter(center, size Vec2) Rect {
	return Rect{
		X: center.X - size.X*0.5,
		Y: center.Y - size.Y*0.5,
		W: size.X,
		H: size.Y,
	}
}

// FromMinMax returns a Rect spanning from lo to hi.
// If lo has a component greater than hi, the resulting Rect has negative W or H;
// call Normalized to flip negative extents.
func FromMinMax(lo, hi Vec2) Rect {
	return Rect{
		X: lo.X,
		Y: lo.Y,
		W: hi.X - lo.X,
		H: hi.Y - lo.Y,
	}
}

// Bounds converts r to an NSGo core.Bounds losslessly.
func (r Rect) Bounds() core.Bounds {
	return core.Bounds{
		Origin: core.Point{X: core.Points(r.X), Y: core.Points(r.Y)},
		Size:   core.Size{Width: core.Points(r.W), Height: core.Points(r.H)},
	}
}

// Min returns the origin corner (X, Y) of r.
func (r Rect) Min() Vec2 { return Vec2{X: r.X, Y: r.Y} }

// Max returns the far corner (X+W, Y+H) of r.
func (r Rect) Max() Vec2 { return Vec2{X: r.X + r.W, Y: r.Y + r.H} }

// Center returns the midpoint of r.
func (r Rect) Center() Vec2 { return Vec2{X: r.X + r.W*0.5, Y: r.Y + r.H*0.5} }

// Size returns the dimensions (W, H) of r.
func (r Rect) Size() Vec2 { return Vec2{X: r.W, Y: r.H} }

// Normalized returns an equivalent Rect with non-negative W and H, shifting X or Y
// when W or H is negative.
func (r Rect) Normalized() Rect {
	if r.W < 0 {
		r.X += r.W
		r.W = -r.W
	}
	if r.H < 0 {
		r.Y += r.H
		r.H = -r.H
	}
	return r
}

// Empty reports whether r has zero or negative area or contains any NaN component.
func (r Rect) Empty() bool {
	return !(r.W > 0 && r.H > 0) || r.X != r.X || r.Y != r.Y
}

// Contains reports whether p lies within r's half-open bounds [X, X+W) × [Y, Y+H).
func (r Rect) Contains(p Vec2) bool {
	if r.Empty() {
		return false
	}
	return p.X >= r.X && p.Y >= r.Y && p.X < r.X+r.W && p.Y < r.Y+r.H
}

// ContainsRect reports whether non-empty rectangle o lies completely inside non-empty r.
// It returns false if either rectangle is empty.
func (r Rect) ContainsRect(o Rect) bool {
	if r.Empty() || o.Empty() {
		return false
	}
	return o.X >= r.X && o.Y >= r.Y && o.X+o.W <= r.X+r.W && o.Y+o.H <= r.Y+r.H
}

// Overlaps reports whether r and o have a non-empty intersection.
// Rectangles that touch only along an edge or corner do not overlap.
func (r Rect) Overlaps(o Rect) bool {
	if r.Empty() || o.Empty() {
		return false
	}
	return r.X < o.X+o.W && o.X < r.X+r.W && r.Y < o.Y+o.H && o.Y < r.Y+r.H
}

// Intersection returns the overlapping region of r and o, or (Rect{}, false) if they
// do not overlap or either is empty.
func (r Rect) Intersection(o Rect) (Rect, bool) {
	if r.Empty() || o.Empty() {
		return Rect{}, false
	}
	x0 := max(r.X, o.X)
	y0 := max(r.Y, o.Y)
	x1 := min(r.X+r.W, o.X+o.W)
	y1 := min(r.Y+r.H, o.Y+o.H)
	if !(x1 > x0 && y1 > y0) {
		return Rect{}, false
	}
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, true
}

// Union returns the smallest Rect containing both r and o.
// An empty rectangle acts as the identity element.
func (r Rect) Union(o Rect) Rect {
	if r.Empty() {
		if o.Empty() {
			return Rect{}
		}
		return o
	}
	if o.Empty() {
		return r
	}
	x0 := min(r.X, o.X)
	y0 := min(r.Y, o.Y)
	x1 := max(r.X+r.W, o.X+o.W)
	y1 := max(r.Y+r.H, o.Y+o.H)
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// Expand returns r expanded outward by margin on all four sides (negative shrinks).
func (r Rect) Expand(margin float32) Rect {
	return Rect{
		X: r.X - margin,
		Y: r.Y - margin,
		W: r.W + 2*margin,
		H: r.H + 2*margin,
	}
}

// Translate returns r shifted by v.
func (r Rect) Translate(v Vec2) Rect {
	return Rect{
		X: r.X + v.X,
		Y: r.Y + v.Y,
		W: r.W,
		H: r.H,
	}
}

// ClampPoint clamps p to the closed region spanned by r.
// Negative W or H is normalized before clamping.
func (r Rect) ClampPoint(p Vec2) Vec2 {
	rn := r.Normalized()
	return p.Clamp(rn.Min(), rn.Max())
}

// ClosestPoint returns the point in the closed rectangle r closest to p.
// For points inside r, it returns p; for points outside, it returns the nearest
// point on r's perimeter.
func (r Rect) ClosestPoint(p Vec2) Vec2 {
	return r.ClampPoint(p)
}

// ApproxEqual reports whether each field of r and o differs by at most eps.
// It returns false if eps < 0 or any value is NaN.
func (r Rect) ApproxEqual(o Rect, eps float32) bool {
	return approxEqual32(r.X, o.X, eps) &&
		approxEqual32(r.Y, o.Y, eps) &&
		approxEqual32(r.W, o.W, eps) &&
		approxEqual32(r.H, o.H, eps)
}
