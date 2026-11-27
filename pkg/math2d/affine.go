package math2d

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// Affine is a 2D 3×2 affine transform stored in the same column order as
// core.Drawing.Transform and core.Matrix3D.Affine (Canvas 2D / SVG matrix(a, b, c, d, e, f)):
//
//	[ A  C  Tx ] [ x ]   [ A*x + C*y + Tx ]
//	[ B  D  Ty ] [ y ] = [ B*x + D*y + Ty ]
//	[ 0  0  1  ] [ 1 ]   [ 1              ]
//
// The zero value maps every point to (0, 0); use Identity for the no-op transform.
type Affine struct {
	A, B   float32
	C, D   float32
	Tx, Ty float32
}

// Identity returns the identity affine transform.
func Identity() Affine {
	return Affine{A: 1, D: 1}
}

// Translate returns a pure translation transform by (x, y).
func Translate(x, y float32) Affine {
	return Affine{A: 1, D: 1, Tx: x, Ty: y}
}

// Scale returns a pure scaling transform by (sx, sy).
func Scale(sx, sy float32) Affine {
	return Affine{A: sx, D: sy}
}

// Rotate returns a rotation transform by rad radians from +X toward +Y.
func Rotate(rad float32) Affine {
	if rad == 0 {
		return Identity()
	}
	sn, cs := math.Sincos(float64(rad))
	s, c := float32(sn), float32(cs)
	return Affine{A: c, B: s, C: -s, D: c}
}

// RotateAround returns a rotation transform by rad radians about pivot (cx, cy).
func RotateAround(rad, cx, cy float32) Affine {
	if rad == 0 {
		return Identity()
	}
	sn, cs := math.Sincos(float64(rad))
	x, y := float64(cx), float64(cy)
	return Affine{
		A:  float32(cs),
		B:  float32(sn),
		C:  float32(-sn),
		D:  float32(cs),
		Tx: float32(x - cs*x + sn*y),
		Ty: float32(y - sn*x - cs*y),
	}
}

// TRS returns a transform that scales by scale, rotates by rot radians, and translates by pos:
// Translate(pos.X, pos.Y).Mul(Rotate(rot)).Mul(Scale(scale.X, scale.Y)).
func TRS(pos Vec2, rot float32, scale Vec2) Affine {
	if rot == 0 {
		return Affine{A: scale.X, D: scale.Y, Tx: pos.X, Ty: pos.Y}
	}
	sn, cs := math.Sincos(float64(rot))
	return Affine{
		A:  float32(cs * float64(scale.X)),
		B:  float32(sn * float64(scale.X)),
		C:  float32(-sn * float64(scale.Y)),
		D:  float32(cs * float64(scale.Y)),
		Tx: pos.X,
		Ty: pos.Y,
	}
}

// FromCore extracts a 2D Affine from an NSGo core.Matrix3D.
// It returns (Affine{}, false) if m is not a pure 2D affine transform.
func FromCore(m core.Matrix3D) (Affine, bool) {
	a, b, c, d, e, f, ok := m.Affine()
	if !ok {
		return Affine{}, false
	}
	return Affine{
		A:  float32(a),
		B:  float32(b),
		C:  float32(c),
		D:  float32(d),
		Tx: float32(e),
		Ty: float32(f),
	}, true
}

// ToCore converts m to an NSGo core.Matrix3D losslessly.
func (m Affine) ToCore() core.Matrix3D {
	return core.Matrix3D{
		float64(m.A), float64(m.B), 0, 0,
		float64(m.C), float64(m.D), 0, 0,
		0, 0, 1, 0,
		float64(m.Tx), float64(m.Ty), 0, 1,
	}
}

// Mul returns the matrix product m * other (other applied first, then m).
func (m Affine) Mul(other Affine) Affine {
	return Affine{
		A:  m.A*other.A + m.C*other.B,
		B:  m.B*other.A + m.D*other.B,
		C:  m.A*other.C + m.C*other.D,
		D:  m.B*other.C + m.D*other.D,
		Tx: m.A*other.Tx + m.C*other.Ty + m.Tx,
		Ty: m.B*other.Tx + m.D*other.Ty + m.Ty,
	}
}

// Invert returns the inverse of m, or (Affine{}, false) if m is singular,
// degenerate, or contains non-finite values.
func (m Affine) Invert() (Affine, bool) {
	a, b := float64(m.A), float64(m.B)
	c, d := float64(m.C), float64(m.D)
	tx, ty := float64(m.Tx), float64(m.Ty)
	det := a*d - b*c
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return Affine{}, false
	}
	invDet := 1 / det
	inv := Affine{
		A:  float32(d * invDet),
		B:  float32(-b * invDet),
		C:  float32(-c * invDet),
		D:  float32(a * invDet),
		Tx: float32((c*ty - d*tx) * invDet),
		Ty: float32((b*tx - a*ty) * invDet),
	}
	if !isFinite32(inv.A) || !isFinite32(inv.B) ||
		!isFinite32(inv.C) || !isFinite32(inv.D) ||
		!isFinite32(inv.Tx) || !isFinite32(inv.Ty) {
		return Affine{}, false
	}
	return inv, true
}

// TransformPoint applies m (including translation) to point p.
func (m Affine) TransformPoint(p Vec2) Vec2 {
	return Vec2{
		X: m.A*p.X + m.C*p.Y + m.Tx,
		Y: m.B*p.X + m.D*p.Y + m.Ty,
	}
}

// TransformVector applies only the linear part of m (ignoring translation) to vector v.
func (m Affine) TransformVector(v Vec2) Vec2 {
	return Vec2{
		X: m.A*v.X + m.C*v.Y,
		Y: m.B*v.X + m.D*v.Y,
	}
}

// TransformRect transforms the four corners of r by m and returns the tightest
// axis-aligned bounding Rect enclosing them.
// It returns the zero Rect if r is empty.
func (m Affine) TransformRect(r Rect) Rect {
	if r.Empty() {
		return Rect{}
	}
	p0 := m.TransformPoint(Vec2{X: r.X, Y: r.Y})
	p1 := m.TransformPoint(Vec2{X: r.X + r.W, Y: r.Y})
	p2 := m.TransformPoint(Vec2{X: r.X, Y: r.Y + r.H})
	p3 := m.TransformPoint(Vec2{X: r.X + r.W, Y: r.Y + r.H})
	minX := min(min(p0.X, p1.X), min(p2.X, p3.X))
	maxX := max(max(p0.X, p1.X), max(p2.X, p3.X))
	minY := min(min(p0.Y, p1.Y), min(p2.Y, p3.Y))
	maxY := max(max(p0.Y, p1.Y), max(p2.Y, p3.Y))
	return Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

// ApproxEqual reports whether each element of m and o differs by at most eps.
// It returns false if eps < 0 or any value is NaN.
func (m Affine) ApproxEqual(o Affine, eps float32) bool {
	return approxEqual32(m.A, o.A, eps) &&
		approxEqual32(m.B, o.B, eps) &&
		approxEqual32(m.C, o.C, eps) &&
		approxEqual32(m.D, o.D, eps) &&
		approxEqual32(m.Tx, o.Tx, eps) &&
		approxEqual32(m.Ty, o.Ty, eps)
}
