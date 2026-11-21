package math2d

// Circle is a 2D circle defined by its Center and Radius.
//
// A circle with Radius <= 0 (or NaN) is empty and contains or overlaps nothing.
type Circle struct {
	Center Vec2
	Radius float32
}

// Contains reports whether p lies inside or on the boundary of c.
func (c Circle) Contains(p Vec2) bool {
	if !(c.Radius > 0) {
		return false
	}
	dx := float64(p.X) - float64(c.Center.X)
	dy := float64(p.Y) - float64(c.Center.Y)
	r := float64(c.Radius)
	return dx*dx+dy*dy <= r*r
}

// Overlaps reports whether c and o intersect (including tangent contact).
func (c Circle) Overlaps(o Circle) bool {
	if !(c.Radius > 0) || !(o.Radius > 0) {
		return false
	}
	dx := float64(c.Center.X) - float64(o.Center.X)
	dy := float64(c.Center.Y) - float64(o.Center.Y)
	sum := float64(c.Radius) + float64(o.Radius)
	return dx*dx+dy*dy <= sum*sum
}

// OverlapsRect reports whether c and non-empty rectangle r intersect
// (including boundary contact).
func (c Circle) OverlapsRect(r Rect) bool {
	if !(c.Radius > 0) || r.Empty() {
		return false
	}
	closest := r.ClosestPoint(c.Center)
	dx := float64(c.Center.X) - float64(closest.X)
	dy := float64(c.Center.Y) - float64(closest.Y)
	rad := float64(c.Radius)
	return dx*dx+dy*dy <= rad*rad
}

// Bounds returns the axis-aligned bounding Rect of c, or the zero Rect if Radius < 0
// or any component is NaN.
func (c Circle) Bounds() Rect {
	if !(c.Radius >= 0) || c.Center.X != c.Center.X || c.Center.Y != c.Center.Y {
		return Rect{}
	}
	d := 2 * c.Radius
	return Rect{
		X: c.Center.X - c.Radius,
		Y: c.Center.Y - c.Radius,
		W: d,
		H: d,
	}
}

// Translate returns c shifted by v.
func (c Circle) Translate(v Vec2) Circle {
	return Circle{
		Center: c.Center.Add(v),
		Radius: c.Radius,
	}
}

// ApproxEqual reports whether c and o have centers and radii within eps.
// It returns false if eps < 0 or any value is NaN.
func (c Circle) ApproxEqual(o Circle, eps float32) bool {
	return c.Center.ApproxEqual(o.Center, eps) && approxEqual32(c.Radius, o.Radius, eps)
}
