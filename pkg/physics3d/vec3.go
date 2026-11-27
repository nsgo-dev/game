package physics3d

import (
	"math"

	"github.com/nsgo-dev/game/internal/detmath"
)

// Vec3 is a 3D vector in a right-handed, Y-up coordinate system.
type Vec3 struct {
	X, Y, Z float32
}

// Add returns v + b.
func (v Vec3) Add(b Vec3) Vec3 {
	return Vec3{X: float32(v.X) + float32(b.X), Y: float32(v.Y) + float32(b.Y), Z: float32(v.Z) + float32(b.Z)}
}

// Sub returns v - b.
func (v Vec3) Sub(b Vec3) Vec3 {
	return Vec3{X: float32(v.X) - float32(b.X), Y: float32(v.Y) - float32(b.Y), Z: float32(v.Z) - float32(b.Z)}
}

// Scale returns v multiplied by s.
func (v Vec3) Scale(s float32) Vec3 {
	return Vec3{X: float32(v.X * s), Y: float32(v.Y * s), Z: float32(v.Z * s)}
}

// Neg returns -v.
func (v Vec3) Neg() Vec3 {
	return Vec3{X: -v.X, Y: -v.Y, Z: -v.Z}
}

// Dot returns the dot product of v and b.
func (v Vec3) Dot(b Vec3) float32 {
	return float32(float32(v.X*b.X)+float32(v.Y*b.Y)) + float32(v.Z*b.Z)
}

// Cross returns the right-handed cross product v x b.
func (v Vec3) Cross(b Vec3) Vec3 {
	return Vec3{
		X: float32(v.Y*b.Z) - float32(v.Z*b.Y),
		Y: float32(v.Z*b.X) - float32(v.X*b.Z),
		Z: float32(v.X*b.Y) - float32(v.Y*b.X),
	}
}

// LenSq returns the squared Euclidean length of v.
func (v Vec3) LenSq() float32 {
	return float32(float32(v.X*v.X)+float32(v.Y*v.Y)) + float32(v.Z*v.Z)
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

func lerpVec3(a, b Vec3, t float32) Vec3 {
	return Vec3{
		X: a.X + float32((b.X-a.X)*t),
		Y: a.Y + float32((b.Y-a.Y)*t),
		Z: a.Z + float32((b.Z-a.Z)*t),
	}
}

// orthonormalBasis returns two unit vectors (t1, t2) orthogonal to unit vector n
// such that (t1, t2, n) forms a right-handed basis.
func orthonormalBasis(n Vec3) (Vec3, Vec3) {
	var ref Vec3
	if abs32(n.X) < 0.9 {
		ref = Vec3{X: 1}
	} else {
		ref = Vec3{Y: 1}
	}
	t1 := n.Cross(ref).Norm()
	t2 := n.Cross(t1).Norm()
	return t1, t2
}

// Quat is a unit quaternion representing a 3D rotation (X, Y, Z imaginary; W real).
// The zero value Quat{} is treated as the identity rotation by Body and Quat helpers.
type Quat struct {
	X, Y, Z, W float32
}

// QuatIdentity returns the identity rotation quaternion.
func QuatIdentity() Quat {
	return Quat{W: 1}
}

// QuatFromAxisAngle returns the unit quaternion representing a right-handed
// rotation of angle radians around axis. If axis is zero, QuatIdentity is returned.
func QuatFromAxisAngle(axis Vec3, angle float32) Quat {
	n := axis.Norm()
	if n.LenSq() == 0 {
		return QuatIdentity()
	}
	half := float64(angle) * 0.5
	sd, cd := detmath.Sincos(half)
	s := float32(sd)
	c := float32(cd)
	return Quat{X: n.X * s, Y: n.Y * s, Z: n.Z * s, W: c}.Norm()
}

// QuatFromTo returns the shortest-arc unit quaternion rotating direction from to to.
func QuatFromTo(from, to Vec3) Quat {
	f := from.Norm()
	t := to.Norm()
	if f.LenSq() == 0 || t.LenSq() == 0 {
		return QuatIdentity()
	}
	dot := clamp32(f.Dot(t), -1, 1)
	if dot > 0.999999 {
		return QuatIdentity()
	}
	if dot < -0.999999 {
		axis, _ := orthonormalBasis(f)
		return Quat{X: axis.X, Y: axis.Y, Z: axis.Z, W: 0}
	}
	cross := f.Cross(t)
	return Quat{X: cross.X, Y: cross.Y, Z: cross.Z, W: 1 + dot}.Norm()
}

// LenSq returns the squared quaternion norm.
func (q Quat) LenSq() float32 {
	return float32(float32(float32(q.X*q.X)+float32(q.Y*q.Y))+float32(q.Z*q.Z)) + float32(q.W*q.W)
}

// IsIdentity reports whether q represents no rotation (including the zero Quat{}).
func (q Quat) IsIdentity() bool {
	return q.X == 0 && q.Y == 0 && q.Z == 0
}

// Norm returns the normalized unit quaternion, or QuatIdentity if q is zero.
func (q Quat) Norm() Quat {
	ls := q.LenSq()
	if ls <= 1e-12 {
		return QuatIdentity()
	}
	inv := 1 / sqrt32(ls)
	return Quat{X: q.X * inv, Y: q.Y * inv, Z: q.Z * inv, W: q.W * inv}
}

func normalizedQuat(q Quat) Quat {
	if q.IsIdentity() {
		return QuatIdentity()
	}
	return q.Norm()
}

// Conjugate returns the quaternion conjugate (inverse for unit quaternions).
func (q Quat) Conjugate() Quat {
	if q.LenSq() <= 1e-12 {
		return QuatIdentity()
	}
	return Quat{X: -q.X, Y: -q.Y, Z: -q.Z, W: q.W}
}

// Dot returns the 4D dot product of q and b.
func (q Quat) Dot(b Quat) float32 {
	return float32(float32(float32(q.X*b.X)+float32(q.Y*b.Y))+float32(q.Z*b.Z)) + float32(q.W*b.W)
}

// Mul returns the Hamilton product q * b (applying b then q).
func (q Quat) Mul(b Quat) Quat {
	if q.LenSq() <= 1e-12 {
		return normalizedQuat(b)
	}
	if b.LenSq() <= 1e-12 {
		return normalizedQuat(q)
	}
	return Quat{
		X: float32(float32(float32(q.W*b.X)+float32(q.X*b.W))+float32(q.Y*b.Z)) - float32(q.Z*b.Y),
		Y: float32(float32(float32(q.W*b.Y)-float32(q.X*b.Z))+float32(q.Y*b.W)) + float32(q.Z*b.X),
		Z: float32(float32(float32(q.W*b.Z)+float32(q.X*b.Y))-float32(q.Y*b.X)) + float32(q.Z*b.W),
		W: float32(float32(float32(q.W*b.W)-float32(q.X*b.X))-float32(q.Y*b.Y)) - float32(q.Z*b.Z),
	}
}

// RotateVec rotates v by the unit quaternion q.
func (q Quat) RotateVec(v Vec3) Vec3 {
	if q.IsIdentity() {
		return v
	}
	u := Vec3{X: q.X, Y: q.Y, Z: q.Z}
	t := u.Cross(v).Scale(2)
	return v.Add(t.Scale(q.W)).Add(u.Cross(t))
}

// Slerp performs shortest-arc spherical linear interpolation from q to b by t in [0, 1].
func (q Quat) Slerp(b Quat, t float32) Quat {
	qa := normalizedQuat(q)
	qb := normalizedQuat(b)
	t = clamp32(t, 0, 1)

	dot := qa.Dot(qb)
	if dot < 0 {
		dot = -dot
		qb = Quat{X: -qb.X, Y: -qb.Y, Z: -qb.Z, W: -qb.W}
	}
	if dot > 0.9995 {
		return Quat{
			X: qa.X + float32((qb.X-qa.X)*t),
			Y: qa.Y + float32((qb.Y-qa.Y)*t),
			Z: qa.Z + float32((qb.Z-qa.Z)*t),
			W: qa.W + float32((qb.W-qa.W)*t),
		}.Norm()
	}
	theta := detmath.Acos(float64(clamp32(dot, -1, 1)))
	sinTheta := detmath.Sin(theta)
	w0 := float32(detmath.Sin((1-float64(t))*theta) / sinTheta)
	w1 := float32(detmath.Sin(float64(t)*theta) / sinTheta)
	return Quat{
		X: float32(qa.X*w0) + float32(qb.X*w1),
		Y: float32(qa.Y*w0) + float32(qb.Y*w1),
		Z: float32(qa.Z*w0) + float32(qb.Z*w1),
		W: float32(qa.W*w0) + float32(qb.W*w1),
	}.Norm()
}

// ToAxisAngle decomposes q into a unit axis and an angle in [0, pi].
func (q Quat) ToAxisAngle() (Vec3, float32) {
	u := normalizedQuat(q)
	if u.W < 0 {
		u = Quat{X: -u.X, Y: -u.Y, Z: -u.Z, W: -u.W}
	}
	w := clamp32(u.W, -1, 1)
	angle := float32(2 * detmath.Acos(float64(w)))
	s := sqrt32(1 - float32(w*w))
	if s < 1e-6 {
		return Vec3{Y: 1}, 0
	}
	inv := 1 / s
	return Vec3{X: u.X * inv, Y: u.Y * inv, Z: u.Z * inv}, angle
}

// AngleTo returns the shortest angular distance in radians between q and b in [0, pi].
func (q Quat) AngleTo(b Quat) float32 {
	qa := normalizedQuat(q)
	qb := normalizedQuat(b)
	dot := abs32(clamp32(qa.Dot(qb), -1, 1))
	return float32(2 * detmath.Acos(float64(dot)))
}

// IntegrateQuat integrates world-space angular velocity angVel over dt seconds
// onto q using the exact exponential map.
func IntegrateQuat(q Quat, angVel Vec3, dt float32) Quat {
	u := normalizedQuat(q)
	wLen := angVel.Len()
	if wLen <= 1e-8 || dt == 0 {
		return u
	}
	dq := QuatFromAxisAngle(angVel.Scale(1/wLen), wLen*dt)
	return dq.Mul(u).Norm()
}
