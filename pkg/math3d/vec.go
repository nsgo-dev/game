package math3d

import "math"

// Scalar helpers.

// DegToRad converts degrees to radians.
func DegToRad(deg float32) float32 { return deg * (math.Pi / 180) }

// RadToDeg converts radians to degrees.
func RadToDeg(rad float32) float32 { return rad * (180 / math.Pi) }

// Clamp restricts v to the closed interval [lo, hi].
func Clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Lerp linearly interpolates between a (at t=0) and b (at t=1).
func Lerp(a, b, t float32) float32 { return a + (b-a)*t }

// ApproxEqual reports whether |a - b| <= eps.
func ApproxEqual(a, b, eps float32) bool {
	return float32(math.Abs(float64(a-b))) <= eps
}

func sqrtf(x float32) float32 { return float32(math.Sqrt(float64(x))) }

func absf(x float32) float32 { return float32(math.Abs(float64(x))) }

// Vec2 is a 2-component float32 vector.
type Vec2 struct {
	X, Y float32
}

// Vec2Zero returns the zero 2D vector (0, 0).
func Vec2Zero() Vec2 { return Vec2{} }

// Vec2One returns (1, 1).
func Vec2One() Vec2 { return Vec2{1, 1} }

// Vec2UnitX returns the +X unit vector (1, 0).
func Vec2UnitX() Vec2 { return Vec2{1, 0} }

// Vec2UnitY returns the +Y unit vector (0, 1).
func Vec2UnitY() Vec2 { return Vec2{0, 1} }

// Add returns v + b.
func (v Vec2) Add(b Vec2) Vec2 { return Vec2{v.X + b.X, v.Y + b.Y} }

// Sub returns v - b.
func (v Vec2) Sub(b Vec2) Vec2 { return Vec2{v.X - b.X, v.Y - b.Y} }

// Mul returns the component-wise product of v and b.
func (v Vec2) Mul(b Vec2) Vec2 { return Vec2{v.X * b.X, v.Y * b.Y} }

// Div returns the component-wise quotient v / b.
func (v Vec2) Div(b Vec2) Vec2 { return Vec2{v.X / b.X, v.Y / b.Y} }

// Scale returns v multiplied by scalar s.
func (v Vec2) Scale(s float32) Vec2 { return Vec2{v.X * s, v.Y * s} }

// Neg returns -v.
func (v Vec2) Neg() Vec2 { return Vec2{-v.X, -v.Y} }

// Dot returns the dot product v · b.
func (v Vec2) Dot(b Vec2) float32 { return v.X*b.X + v.Y*b.Y }

// LenSq returns the squared Euclidean length of v.
func (v Vec2) LenSq() float32 { return v.X*v.X + v.Y*v.Y }

// Len returns the Euclidean length of v.
func (v Vec2) Len() float32 { return sqrtf(v.LenSq()) }

// Norm returns the unit vector in the direction of v, or the zero vector if v has zero length.
func (v Vec2) Norm() Vec2 {
	l := v.Len()
	if l == 0 {
		return Vec2{}
	}
	inv := 1 / l
	return Vec2{v.X * inv, v.Y * inv}
}

// DistSq returns the squared Euclidean distance between v and b.
func (v Vec2) DistSq(b Vec2) float32 { return v.Sub(b).LenSq() }

// Dist returns the Euclidean distance between v and b.
func (v Vec2) Dist(b Vec2) float32 { return v.Sub(b).Len() }

// Lerp linearly interpolates from v (t=0) to b (t=1).
func (v Vec2) Lerp(b Vec2, t float32) Vec2 {
	return Vec2{Lerp(v.X, b.X, t), Lerp(v.Y, b.Y, t)}
}

// Min returns the component-wise minimum of v and b.
func (v Vec2) Min(b Vec2) Vec2 { return Vec2{min(v.X, b.X), min(v.Y, b.Y)} }

// Max returns the component-wise maximum of v and b.
func (v Vec2) Max(b Vec2) Vec2 { return Vec2{max(v.X, b.X), max(v.Y, b.Y)} }

// Clamp restricts each component of v to [lo, hi].
func (v Vec2) Clamp(lo, hi Vec2) Vec2 {
	return Vec2{Clamp(v.X, lo.X, hi.X), Clamp(v.Y, lo.Y, hi.Y)}
}

// ApproxEqual reports whether each component of v is within eps of b.
func (v Vec2) ApproxEqual(b Vec2, eps float32) bool {
	return ApproxEqual(v.X, b.X, eps) && ApproxEqual(v.Y, b.Y, eps)
}

// Vec3 promotes v to a Vec3 with the given z component.
func (v Vec2) Vec3(z float32) Vec3 { return Vec3{v.X, v.Y, z} }

// Vec3 is a 3-component float32 vector in right-handed Y-up space (+X right, +Y up, -Z forward).
type Vec3 struct {
	X, Y, Z float32
}

// Vec3Zero returns the zero 3D vector (0, 0, 0).
func Vec3Zero() Vec3 { return Vec3{} }

// Vec3One returns (1, 1, 1).
func Vec3One() Vec3 { return Vec3{1, 1, 1} }

// Vec3UnitX returns the +X unit vector (1, 0, 0).
func Vec3UnitX() Vec3 { return Vec3{1, 0, 0} }

// Vec3UnitY returns the +Y unit vector (0, 1, 0).
func Vec3UnitY() Vec3 { return Vec3{0, 1, 0} }

// Vec3UnitZ returns the +Z unit vector (0, 0, 1).
func Vec3UnitZ() Vec3 { return Vec3{0, 0, 1} }

// Vec3Right returns the right-handed +X axis (1, 0, 0).
func Vec3Right() Vec3 { return Vec3{1, 0, 0} }

// Vec3Up returns the right-handed +Y axis (0, 1, 0).
func Vec3Up() Vec3 { return Vec3{0, 1, 0} }

// Vec3Forward returns the right-handed forward -Z axis (0, 0, -1).
func Vec3Forward() Vec3 { return Vec3{0, 0, -1} }

// Add returns v + b.
func (v Vec3) Add(b Vec3) Vec3 { return Vec3{v.X + b.X, v.Y + b.Y, v.Z + b.Z} }

// Sub returns v - b.
func (v Vec3) Sub(b Vec3) Vec3 { return Vec3{v.X - b.X, v.Y - b.Y, v.Z - b.Z} }

// Mul returns the component-wise product of v and b.
func (v Vec3) Mul(b Vec3) Vec3 { return Vec3{v.X * b.X, v.Y * b.Y, v.Z * b.Z} }

// Div returns the component-wise quotient v / b.
func (v Vec3) Div(b Vec3) Vec3 { return Vec3{v.X / b.X, v.Y / b.Y, v.Z / b.Z} }

// Scale returns v multiplied by scalar s.
func (v Vec3) Scale(s float32) Vec3 { return Vec3{v.X * s, v.Y * s, v.Z * s} }

// Neg returns -v.
func (v Vec3) Neg() Vec3 { return Vec3{-v.X, -v.Y, -v.Z} }

// Dot returns the dot product v · b.
func (v Vec3) Dot(b Vec3) float32 { return v.X*b.X + v.Y*b.Y + v.Z*b.Z }

// Cross returns the right-handed cross product v × b.
func (v Vec3) Cross(b Vec3) Vec3 {
	return Vec3{
		v.Y*b.Z - v.Z*b.Y,
		v.Z*b.X - v.X*b.Z,
		v.X*b.Y - v.Y*b.X,
	}
}

// LenSq returns the squared Euclidean length of v.
func (v Vec3) LenSq() float32 { return v.X*v.X + v.Y*v.Y + v.Z*v.Z }

// Len returns the Euclidean length of v.
func (v Vec3) Len() float32 { return sqrtf(v.LenSq()) }

// Norm returns the unit vector in the direction of v, or the zero vector if v has zero length.
func (v Vec3) Norm() Vec3 {
	l := v.Len()
	if l == 0 {
		return Vec3{}
	}
	inv := 1 / l
	return Vec3{v.X * inv, v.Y * inv, v.Z * inv}
}

// DistSq returns the squared Euclidean distance between v and b.
func (v Vec3) DistSq(b Vec3) float32 { return v.Sub(b).LenSq() }

// Dist returns the Euclidean distance between v and b.
func (v Vec3) Dist(b Vec3) float32 { return v.Sub(b).Len() }

// Lerp linearly interpolates from v (t=0) to b (t=1).
func (v Vec3) Lerp(b Vec3, t float32) Vec3 {
	return Vec3{Lerp(v.X, b.X, t), Lerp(v.Y, b.Y, t), Lerp(v.Z, b.Z, t)}
}

// Min returns the component-wise minimum of v and b.
func (v Vec3) Min(b Vec3) Vec3 {
	return Vec3{min(v.X, b.X), min(v.Y, b.Y), min(v.Z, b.Z)}
}

// Max returns the component-wise maximum of v and b.
func (v Vec3) Max(b Vec3) Vec3 {
	return Vec3{max(v.X, b.X), max(v.Y, b.Y), max(v.Z, b.Z)}
}

// Clamp restricts each component of v to [lo, hi].
func (v Vec3) Clamp(lo, hi Vec3) Vec3 {
	return Vec3{Clamp(v.X, lo.X, hi.X), Clamp(v.Y, lo.Y, hi.Y), Clamp(v.Z, lo.Z, hi.Z)}
}

// ApproxEqual reports whether each component of v is within eps of b.
func (v Vec3) ApproxEqual(b Vec3, eps float32) bool {
	return ApproxEqual(v.X, b.X, eps) && ApproxEqual(v.Y, b.Y, eps) && ApproxEqual(v.Z, b.Z, eps)
}

// XY returns the X and Y components as a Vec2.
func (v Vec3) XY() Vec2 { return Vec2{v.X, v.Y} }

// Vec4 promotes v to a Vec4 with the given w component (use 1 for points, 0 for directions).
func (v Vec3) Vec4(w float32) Vec4 { return Vec4{v.X, v.Y, v.Z, w} }

// Vec4 is a 4-component float32 vector, commonly used for homogeneous coordinates and colors.
type Vec4 struct {
	X, Y, Z, W float32
}

// Vec4Zero returns the zero 4D vector (0, 0, 0, 0).
func Vec4Zero() Vec4 { return Vec4{} }

// Vec4One returns (1, 1, 1, 1).
func Vec4One() Vec4 { return Vec4{1, 1, 1, 1} }

// Add returns v + b.
func (v Vec4) Add(b Vec4) Vec4 { return Vec4{v.X + b.X, v.Y + b.Y, v.Z + b.Z, v.W + b.W} }

// Sub returns v - b.
func (v Vec4) Sub(b Vec4) Vec4 { return Vec4{v.X - b.X, v.Y - b.Y, v.Z - b.Z, v.W - b.W} }

// Mul returns the component-wise product of v and b.
func (v Vec4) Mul(b Vec4) Vec4 { return Vec4{v.X * b.X, v.Y * b.Y, v.Z * b.Z, v.W * b.W} }

// Div returns the component-wise quotient v / b.
func (v Vec4) Div(b Vec4) Vec4 { return Vec4{v.X / b.X, v.Y / b.Y, v.Z / b.Z, v.W / b.W} }

// Scale returns v multiplied by scalar s.
func (v Vec4) Scale(s float32) Vec4 { return Vec4{v.X * s, v.Y * s, v.Z * s, v.W * s} }

// Neg returns -v.
func (v Vec4) Neg() Vec4 { return Vec4{-v.X, -v.Y, -v.Z, -v.W} }

// Dot returns the dot product v · b.
func (v Vec4) Dot(b Vec4) float32 { return v.X*b.X + v.Y*b.Y + v.Z*b.Z + v.W*b.W }

// LenSq returns the squared Euclidean length of v.
func (v Vec4) LenSq() float32 { return v.X*v.X + v.Y*v.Y + v.Z*v.Z + v.W*v.W }

// Len returns the Euclidean length of v.
func (v Vec4) Len() float32 { return sqrtf(v.LenSq()) }

// Norm returns the unit vector in the direction of v, or the zero vector if v has zero length.
func (v Vec4) Norm() Vec4 {
	l := v.Len()
	if l == 0 {
		return Vec4{}
	}
	inv := 1 / l
	return Vec4{v.X * inv, v.Y * inv, v.Z * inv, v.W * inv}
}

// DistSq returns the squared Euclidean distance between v and b.
func (v Vec4) DistSq(b Vec4) float32 { return v.Sub(b).LenSq() }

// Dist returns the Euclidean distance between v and b.
func (v Vec4) Dist(b Vec4) float32 { return v.Sub(b).Len() }

// Lerp linearly interpolates from v (t=0) to b (t=1).
func (v Vec4) Lerp(b Vec4, t float32) Vec4 {
	return Vec4{Lerp(v.X, b.X, t), Lerp(v.Y, b.Y, t), Lerp(v.Z, b.Z, t), Lerp(v.W, b.W, t)}
}

// Min returns the component-wise minimum of v and b.
func (v Vec4) Min(b Vec4) Vec4 {
	return Vec4{min(v.X, b.X), min(v.Y, b.Y), min(v.Z, b.Z), min(v.W, b.W)}
}

// Max returns the component-wise maximum of v and b.
func (v Vec4) Max(b Vec4) Vec4 {
	return Vec4{max(v.X, b.X), max(v.Y, b.Y), max(v.Z, b.Z), max(v.W, b.W)}
}

// Clamp restricts each component of v to [lo, hi].
func (v Vec4) Clamp(lo, hi Vec4) Vec4 {
	return Vec4{
		Clamp(v.X, lo.X, hi.X),
		Clamp(v.Y, lo.Y, hi.Y),
		Clamp(v.Z, lo.Z, hi.Z),
		Clamp(v.W, lo.W, hi.W),
	}
}

// ApproxEqual reports whether each component of v is within eps of b.
func (v Vec4) ApproxEqual(b Vec4, eps float32) bool {
	return ApproxEqual(v.X, b.X, eps) &&
		ApproxEqual(v.Y, b.Y, eps) &&
		ApproxEqual(v.Z, b.Z, eps) &&
		ApproxEqual(v.W, b.W, eps)
}

// XYZ returns the X, Y, and Z components as a Vec3.
func (v Vec4) XYZ() Vec3 { return Vec3{v.X, v.Y, v.Z} }
