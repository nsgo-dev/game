package math2d

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// Vec2 is a 2D vector or point in float32 coordinates.
type Vec2 struct {
	X, Y float32
}

// V2 returns Vec2{X: x, Y: y}.
func V2(x, y float32) Vec2 { return Vec2{X: x, Y: y} }

// Zero returns the zero vector (0, 0).
func Zero() Vec2 { return Vec2{} }

// One returns the vector (1, 1).
func One() Vec2 { return Vec2{X: 1, Y: 1} }

// UnitX returns the positive X unit vector (1, 0).
func UnitX() Vec2 { return Vec2{X: 1} }

// UnitY returns the positive Y unit vector (0, 1).
func UnitY() Vec2 { return Vec2{Y: 1} }

// FromAngle returns the unit vector at rad radians from +X toward +Y.
func FromAngle(rad float32) Vec2 {
	sn, cs := math.Sincos(float64(rad))
	return Vec2{X: float32(cs), Y: float32(sn)}
}

// FromPoint converts an NSGo core.Point to a Vec2 losslessly.
func FromPoint(p core.Point) Vec2 {
	return Vec2{X: float32(p.X), Y: float32(p.Y)}
}

// FromSize converts an NSGo core.Size to a Vec2 losslessly.
func FromSize(s core.Size) Vec2 {
	return Vec2{X: float32(s.Width), Y: float32(s.Height)}
}

// Point converts v to an NSGo core.Point losslessly.
func (v Vec2) Point() core.Point {
	return core.Point{X: core.Points(v.X), Y: core.Points(v.Y)}
}

// Size converts v to an NSGo core.Size losslessly.
func (v Vec2) Size() core.Size {
	return core.Size{Width: core.Points(v.X), Height: core.Points(v.Y)}
}

// Add returns v + o.
func (v Vec2) Add(o Vec2) Vec2 {
	return Vec2{X: v.X + o.X, Y: v.Y + o.Y}
}

// Sub returns v - o.
func (v Vec2) Sub(o Vec2) Vec2 {
	return Vec2{X: v.X - o.X, Y: v.Y - o.Y}
}

// Scale returns v multiplied by scalar s.
func (v Vec2) Scale(s float32) Vec2 {
	return Vec2{X: v.X * s, Y: v.Y * s}
}

// Mul returns the component-wise product of v and o.
func (v Vec2) Mul(o Vec2) Vec2 {
	return Vec2{X: v.X * o.X, Y: v.Y * o.Y}
}

// Div returns the component-wise quotient of v divided by o.
func (v Vec2) Div(o Vec2) Vec2 {
	return Vec2{X: v.X / o.X, Y: v.Y / o.Y}
}

// Neg returns -v.
func (v Vec2) Neg() Vec2 {
	return Vec2{X: -v.X, Y: -v.Y}
}

// Dot returns the dot product of v and o.
func (v Vec2) Dot(o Vec2) float32 {
	return v.X*o.X + v.Y*o.Y
}

// Cross returns the 2D scalar cross product (v.X*o.Y - v.Y*o.X).
func (v Vec2) Cross(o Vec2) float32 {
	return v.X*o.Y - v.Y*o.X
}

// Len returns the Euclidean length of v.
func (v Vec2) Len() float32 {
	x, y := float64(v.X), float64(v.Y)
	return float32(math.Sqrt(x*x + y*y))
}

// LenSq returns the squared length of v.
func (v Vec2) LenSq() float32 {
	return v.X*v.X + v.Y*v.Y
}

// Dist returns the Euclidean distance between v and o.
func (v Vec2) Dist(o Vec2) float32 {
	return v.Sub(o).Len()
}

// DistSq returns the squared distance between v and o.
func (v Vec2) DistSq(o Vec2) float32 {
	return v.Sub(o).LenSq()
}

// Normalize returns a unit vector in the direction of v.
// If v has zero length, NaN, or infinite length, it returns the zero vector.
func (v Vec2) Normalize() Vec2 {
	x, y := float64(v.X), float64(v.Y)
	l := math.Sqrt(x*x + y*y)
	if l == 0 || math.IsNaN(l) || math.IsInf(l, 0) {
		return Vec2{}
	}
	return Vec2{X: float32(x / l), Y: float32(y / l)}
}

// ClampLen returns v scaled down so its length does not exceed maxLen.
// If maxLen <= 0 or NaN, or v has NaN or infinite length, it returns the zero vector.
func (v Vec2) ClampLen(maxLen float32) Vec2 {
	if !(maxLen > 0) {
		return Vec2{}
	}
	x, y := float64(v.X), float64(v.Y)
	l := math.Sqrt(x*x + y*y)
	if math.IsNaN(l) || math.IsInf(l, 0) {
		return Vec2{}
	}
	if l <= float64(maxLen) {
		return v
	}
	s := float64(maxLen) / l
	return Vec2{X: float32(x * s), Y: float32(y * s)}
}

// Lerp linearly interpolates between v (at t=0) and o (at t=1).
func (v Vec2) Lerp(o Vec2, t float32) Vec2 {
	return Vec2{
		X: v.X + (o.X-v.X)*t,
		Y: v.Y + (o.Y-v.Y)*t,
	}
}

// MoveTowards moves v toward target by at most maxDelta without overshooting.
// If maxDelta <= 0 or NaN, or the distance is NaN, it returns v unchanged.
func (v Vec2) MoveTowards(target Vec2, maxDelta float32) Vec2 {
	if !(maxDelta > 0) {
		return v
	}
	dx := float64(target.X) - float64(v.X)
	dy := float64(target.Y) - float64(v.Y)
	dist := math.Sqrt(dx*dx + dy*dy)
	if math.IsNaN(dist) {
		return v
	}
	if dist == 0 || dist <= float64(maxDelta) {
		return target
	}
	if math.IsInf(dist, 0) {
		return v
	}
	s := float64(maxDelta) / dist
	return Vec2{
		X: float32(float64(v.X) + dx*s),
		Y: float32(float64(v.Y) + dy*s),
	}
}

// Angle returns the angle of v in radians in [-π, π] from +X toward +Y.
func (v Vec2) Angle() float32 {
	return float32(math.Atan2(float64(v.Y), float64(v.X)))
}

// Rotate returns v rotated by rad radians from +X toward +Y.
func (v Vec2) Rotate(rad float32) Vec2 {
	if rad == 0 {
		return v
	}
	sn, cs := math.Sincos(float64(rad))
	x, y := float64(v.X), float64(v.Y)
	return Vec2{
		X: float32(x*cs - y*sn),
		Y: float32(x*sn + y*cs),
	}
}

// Perp returns v rotated by +π/2 radians: (-v.Y, v.X).
func (v Vec2) Perp() Vec2 {
	return Vec2{X: -v.Y, Y: v.X}
}

// Reflect returns v reflected across a surface with unit normal n.
func (v Vec2) Reflect(normal Vec2) Vec2 {
	d := 2 * v.Dot(normal)
	return Vec2{X: v.X - d*normal.X, Y: v.Y - d*normal.Y}
}

// Project returns the orthogonal projection of v onto onto.
// If onto has zero length, NaN, or infinite length, it returns the zero vector.
func (v Vec2) Project(onto Vec2) Vec2 {
	ox, oy := float64(onto.X), float64(onto.Y)
	denom := ox*ox + oy*oy
	if denom == 0 || math.IsNaN(denom) || math.IsInf(denom, 0) {
		return Vec2{}
	}
	s := (float64(v.X)*ox + float64(v.Y)*oy) / denom
	if math.IsNaN(s) || math.IsInf(s, 0) {
		return Vec2{}
	}
	return Vec2{X: float32(ox * s), Y: float32(oy * s)}
}

// Min returns the component-wise minimum of v and o.
func (v Vec2) Min(o Vec2) Vec2 {
	return Vec2{X: min(v.X, o.X), Y: min(v.Y, o.Y)}
}

// Max returns the component-wise maximum of v and o.
func (v Vec2) Max(o Vec2) Vec2 {
	return Vec2{X: max(v.X, o.X), Y: max(v.Y, o.Y)}
}

// Clamp clamps each component of v to [lo, hi].
func (v Vec2) Clamp(lo, hi Vec2) Vec2 {
	return Vec2{
		X: min(max(v.X, lo.X), hi.X),
		Y: min(max(v.Y, lo.Y), hi.Y),
	}
}

// ApproxEqual reports whether each component of v and o differs by at most eps.
// It returns false if eps < 0 or any value is NaN.
func (v Vec2) ApproxEqual(o Vec2, eps float32) bool {
	return approxEqual32(v.X, o.X, eps) && approxEqual32(v.Y, o.Y, eps)
}

func approxEqual32(a, b, eps float32) bool {
	if !(eps >= 0) || a != a || b != b {
		return false
	}
	if a == b {
		return true
	}
	return math.Abs(float64(a)-float64(b)) <= float64(eps)
}

func isFinite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}
