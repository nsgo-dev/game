package physics3d

import "math"

// Vec3 is a 3D vector in a right-handed, Y-up coordinate system.
type Vec3 struct {
	X, Y, Z float32
}

// Add returns v + b.
func (v Vec3) Add(b Vec3) Vec3 {
	return Vec3{X: v.X + b.X, Y: v.Y + b.Y, Z: v.Z + b.Z}
}

// Sub returns v - b.
func (v Vec3) Sub(b Vec3) Vec3 {
	return Vec3{X: v.X - b.X, Y: v.Y - b.Y, Z: v.Z - b.Z}
}

// Scale returns v multiplied by s.
func (v Vec3) Scale(s float32) Vec3 {
	return Vec3{X: v.X * s, Y: v.Y * s, Z: v.Z * s}
}

// Neg returns -v.
func (v Vec3) Neg() Vec3 {
	return Vec3{X: -v.X, Y: -v.Y, Z: -v.Z}
}

// Dot returns the dot product of v and b.
func (v Vec3) Dot(b Vec3) float32 {
	return v.X*b.X + v.Y*b.Y + v.Z*b.Z
}

// Cross returns the right-handed cross product v x b.
func (v Vec3) Cross(b Vec3) Vec3 {
	return Vec3{
		X: v.Y*b.Z - v.Z*b.Y,
		Y: v.Z*b.X - v.X*b.Z,
		Z: v.X*b.Y - v.Y*b.X,
	}
}

// LenSq returns the squared Euclidean length of v.
func (v Vec3) LenSq() float32 {
	return v.X*v.X + v.Y*v.Y + v.Z*v.Z
}

// Len returns the Euclidean length of v.
func (v Vec3) Len() float32 {
	return sqrt32(v.LenSq())
}

// Norm returns the unit vector in the direction of v, or the zero vector when
// v has zero length.
func (v Vec3) Norm() Vec3 {
	l := v.Len()
	if l == 0 {
		return Vec3{}
	}
	inv := 1 / l
	return Vec3{X: v.X * inv, Y: v.Y * inv, Z: v.Z * inv}
}

// DistSq returns the squared Euclidean distance between v and b.
func (v Vec3) DistSq(b Vec3) float32 {
	return v.Sub(b).LenSq()
}

// Dist returns the Euclidean distance between v and b.
func (v Vec3) Dist(b Vec3) float32 {
	return v.Sub(b).Len()
}

func sqrt32(x float32) float32 {
	if x <= 0 {
		return 0
	}
	return float32(math.Sqrt(float64(x)))
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func clamp32(x, lo, hi float32) float32 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}
