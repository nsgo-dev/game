package math3d

import "math"

// Quat is a quaternion stored as (X, Y, Z, W), matching glTF 2.0's [x, y, z, w]
// order. Unit quaternions represent right-handed 3D rotations; q and -q
// represent the same rotation.
type Quat struct {
	X, Y, Z, W float32
}

// QuatIdentity returns the identity rotation quaternion (0, 0, 0, 1).
func QuatIdentity() Quat { return Quat{0, 0, 0, 1} }

// QuatFromAxisAngle returns a unit quaternion representing a right-handed
// rotation of rad radians around axis. If axis has zero length, QuatIdentity
// is returned.
func QuatFromAxisAngle(axis Vec3, rad float32) Quat {
	l := axis.Len()
	if l == 0 {
		return QuatIdentity()
	}
	s, c := math.Sincos(float64(rad) * 0.5)
	scale := float32(s) / l
	return Quat{
		X: axis.X * scale,
		Y: axis.Y * scale,
		Z: axis.Z * scale,
		W: float32(c),
	}
}

// QuatFromEuler builds a unit quaternion from intrinsic YXZ Euler angles
// (yaw around +Y, then pitch around local +X, then roll around local +Z),
// equivalent to Ry(yaw) * Rx(pitch) * Rz(roll). All angles are in radians.
func QuatFromEuler(pitch, yaw, roll float32) Quat {
	sy, cy := math.Sincos(float64(yaw) * 0.5)
	sp, cp := math.Sincos(float64(pitch) * 0.5)
	sr, cr := math.Sincos(float64(roll) * 0.5)
	return Quat{
		X: float32(cy*sp*cr + sy*cp*sr),
		Y: float32(sy*cp*cr - cy*sp*sr),
		Z: float32(cy*cp*sr - sy*sp*cr),
		W: float32(cy*cp*cr + sy*sp*sr),
	}
}

// QuatFromEulerXYZ builds a unit quaternion from intrinsic XYZ Euler angles,
// equivalent to Rx(x) * Ry(y) * Rz(z). All angles are in radians.
func QuatFromEulerXYZ(x, y, z float32) Quat {
	sx, cx := math.Sincos(float64(x) * 0.5)
	sy, cy := math.Sincos(float64(y) * 0.5)
	sz, cz := math.Sincos(float64(z) * 0.5)
	return Quat{
		X: float32(sx*cy*cz + cx*sy*sz),
		Y: float32(cx*sy*cz - sx*cy*sz),
		Z: float32(cx*cy*sz + sx*sy*cz),
		W: float32(cx*cy*cz - sx*sy*sz),
	}
}

// ToEuler extracts intrinsic YXZ Euler angles (pitch, yaw, roll) in radians
// such that QuatFromEuler(pitch, yaw, roll) reproduces q (up to sign ±q).
// Near ±90° pitch it still separates yaw and roll; only an exact gimbal lock folds roll into yaw.
func (q Quat) ToEuler() (pitch, yaw, roll float32) {
	m := q.mat3f64()
	// M[row, col] = m[col*3 + row]
	// M12 = -sin(pitch)
	sinPitch := math.Max(-1, math.Min(1, -m[7]))
	pitch = float32(math.Asin(sinPitch))
	// The matrix is built in float64 from q, so atan2 stays accurate down to a far narrower gimbal band than the old float32 1e-6 cutoff, which zeroed roll within about 1.4e-3 rad of the pole (#546).
	if math.Hypot(m[6], m[8]) > 1e-12 {
		yaw = float32(math.Atan2(m[6], m[8]))
		roll = float32(math.Atan2(m[1], m[4]))
	} else {
		roll = 0
		yaw = float32(math.Atan2(-m[2], m[0]))
	}
	return pitch, yaw, roll
}

// mat3f64 returns q's rotation matrix in float64, laid out like ToMat3.
func (q Quat) mat3f64() [9]float64 {
	x, y, z, w := float64(q.X), float64(q.Y), float64(q.Z), float64(q.W)
	n := math.Sqrt(x*x + y*y + z*z + w*w)
	if n == 0 {
		return [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	}
	x, y, z, w = x/n, y/n, z/n, w/n
	xx, yy, zz := x*x, y*y, z*z
	xy, xz, yz := x*y, x*z, y*z
	wx, wy, wz := w*x, w*y, w*z
	return [9]float64{
		1 - 2*(yy+zz), 2 * (xy + wz), 2 * (xz - wy),
		2 * (xy - wz), 1 - 2*(xx+zz), 2 * (yz + wx),
		2 * (xz + wy), 2 * (yz - wx), 1 - 2*(xx+yy),
	}
}

// ToEulerXYZ extracts intrinsic XYZ Euler angles (x, y, z) in radians
// such that QuatFromEulerXYZ(x, y, z) reproduces q (up to sign ±q).
func (q Quat) ToEulerXYZ() (x, y, z float32) {
	m := q.mat3f64()
	// M02 = sin(y)
	sinY := math.Max(-1, math.Min(1, m[6]))
	y = float32(math.Asin(sinY))
	if math.Hypot(m[7], m[8]) > 1e-12 {
		x = float32(math.Atan2(-m[7], m[8]))
		z = float32(math.Atan2(-m[3], m[0]))
	} else {
		z = 0
		x = float32(math.Atan2(m[5], m[4]))
	}
	return x, y, z
}

// QuatFromMat3 converts a pure 3x3 right-handed rotation matrix into a unit
// quaternion using Shepperd's method.
func QuatFromMat3(m Mat3) Quat {
	m00, m10, m20 := float64(m[0]), float64(m[1]), float64(m[2])
	m01, m11, m21 := float64(m[3]), float64(m[4]), float64(m[5])
	m02, m12, m22 := float64(m[6]), float64(m[7]), float64(m[8])

	trace := m00 + m11 + m22
	var x, y, z, w float64
	switch {
	case trace > 0:
		s := 2.0 * math.Sqrt(1.0+trace)
		inv := 1.0 / s
		w = 0.25 * s
		x = (m21 - m12) * inv
		y = (m02 - m20) * inv
		z = (m10 - m01) * inv
	case m00 > m11 && m00 > m22:
		s := 2.0 * math.Sqrt(1.0+m00-m11-m22)
		inv := 1.0 / s
		w = (m21 - m12) * inv
		x = 0.25 * s
		y = (m01 + m10) * inv
		z = (m02 + m20) * inv
	case m11 > m22:
		s := 2.0 * math.Sqrt(1.0+m11-m00-m22)
		inv := 1.0 / s
		w = (m02 - m20) * inv
		x = (m01 + m10) * inv
		y = 0.25 * s
		z = (m12 + m21) * inv
	default:
		s := 2.0 * math.Sqrt(1.0+m22-m00-m11)
		inv := 1.0 / s
		w = (m10 - m01) * inv
		x = (m02 + m20) * inv
		y = (m12 + m21) * inv
		z = 0.25 * s
	}
	return Quat{float32(x), float32(y), float32(z), float32(w)}.Norm()
}

// QuatFromMat4 converts the upper-left 3x3 rotation part of m into a unit
// quaternion. The upper-left 3x3 must be a pure rotation (orthonormal with
// determinant +1); use Decompose when scale is present.
func QuatFromMat4(m Mat4) Quat {
	return QuatFromMat3(Mat3FromMat4(m))
}

// ToMat3 converts q to a 3x3 column-major rotation matrix.
func (q Quat) ToMat3() Mat3 {
	n := q.Norm()
	x, y, z, w := n.X, n.Y, n.Z, n.W
	xx, yy, zz := x*x, y*y, z*z
	xy, xz, yz := x*y, x*z, y*z
	wx, wy, wz := w*x, w*y, w*z
	return Mat3{
		1 - 2*(yy+zz), 2 * (xy + wz), 2 * (xz - wy),
		2 * (xy - wz), 1 - 2*(xx+zz), 2 * (yz + wx),
		2 * (xz + wy), 2 * (yz - wx), 1 - 2*(xx+yy),
	}
}

// ToMat4 converts q to a 4x4 column-major rotation matrix.
func (q Quat) ToMat4() Mat4 {
	m := q.ToMat3()
	return Mat4{
		m[0], m[1], m[2], 0,
		m[3], m[4], m[5], 0,
		m[6], m[7], m[8], 0,
		0, 0, 0, 1,
	}
}

// Mul returns the Hamilton product q * b, composing rotation b followed by q.
func (q Quat) Mul(b Quat) Quat {
	return Quat{
		X: q.W*b.X + q.X*b.W + q.Y*b.Z - q.Z*b.Y,
		Y: q.W*b.Y - q.X*b.Z + q.Y*b.W + q.Z*b.X,
		Z: q.W*b.Z + q.X*b.Y - q.Y*b.X + q.Z*b.W,
		W: q.W*b.W - q.X*b.X - q.Y*b.Y - q.Z*b.Z,
	}
}

// Conj returns the conjugate of q: (-X, -Y, -Z, W).
// For unit quaternions, Conj equals Inverse.
func (q Quat) Conj() Quat { return Quat{-q.X, -q.Y, -q.Z, q.W} }

// Dot returns the 4D dot product of q and b.
func (q Quat) Dot(b Quat) float32 {
	return q.X*b.X + q.Y*b.Y + q.Z*b.Z + q.W*b.W
}

// LenSq returns the squared norm of q.
func (q Quat) LenSq() float32 {
	return q.X*q.X + q.Y*q.Y + q.Z*q.Z + q.W*q.W
}

// Len returns the norm of q.
func (q Quat) Len() float32 { return sqrtf(q.LenSq()) }

// Norm returns the normalized unit quaternion, or QuatIdentity if q has zero norm.
func (q Quat) Norm() Quat {
	l := q.Len()
	if l == 0 {
		return QuatIdentity()
	}
	inv := 1 / l
	return Quat{q.X * inv, q.Y * inv, q.Z * inv, q.W * inv}
}

// Inverse returns the multiplicative inverse of q, or QuatIdentity if q has zero norm.
func (q Quat) Inverse() Quat {
	lsq := q.LenSq()
	if lsq == 0 {
		return QuatIdentity()
	}
	inv := 1 / lsq
	return Quat{-q.X * inv, -q.Y * inv, -q.Z * inv, q.W * inv}
}

// RotateVec3 rotates v by the unit rotation q.
func (q Quat) RotateVec3(v Vec3) Vec3 {
	n := q.Norm()
	u := Vec3{n.X, n.Y, n.Z}
	t := u.Cross(v).Scale(2)
	return v.Add(t.Scale(n.W)).Add(u.Cross(t))
}

// Slerp performs shortest-path spherical linear interpolation from q (t=0) to b (t=1).
// If q·b < 0, b is negated so interpolation always takes the shorter arc.
func (q Quat) Slerp(b Quat, t float32) Quat {
	a := q.Norm()
	c := b.Norm()
	dot := a.Dot(c)
	if dot < 0 {
		c = Quat{-c.X, -c.Y, -c.Z, -c.W}
		dot = -dot
	}
	if dot > 0.9995 {
		return Quat{
			X: Lerp(a.X, c.X, t),
			Y: Lerp(a.Y, c.Y, t),
			Z: Lerp(a.Z, c.Z, t),
			W: Lerp(a.W, c.W, t),
		}.Norm()
	}
	if dot > 1 {
		dot = 1
	}
	theta := math.Acos(float64(dot))
	sinTheta := math.Sin(theta)
	s0 := float32(math.Sin((1-float64(t))*theta) / sinTheta)
	s1 := float32(math.Sin(float64(t)*theta) / sinTheta)
	return Quat{
		X: s0*a.X + s1*c.X,
		Y: s0*a.Y + s1*c.Y,
		Z: s0*a.Z + s1*c.Z,
		W: s0*a.W + s1*c.W,
	}.Norm()
}

// Nlerp performs shortest-path normalized linear interpolation from q (t=0) to b (t=1).
func (q Quat) Nlerp(b Quat, t float32) Quat {
	a := q.Norm()
	c := b.Norm()
	if a.Dot(c) < 0 {
		c = Quat{-c.X, -c.Y, -c.Z, -c.W}
	}
	return Quat{
		X: Lerp(a.X, c.X, t),
		Y: Lerp(a.Y, c.Y, t),
		Z: Lerp(a.Z, c.Z, t),
		W: Lerp(a.W, c.W, t),
	}.Norm()
}

// ApproxEqual reports whether q and b represent approximately the same rotation
// (accounting for the double-cover equivalence q == -q).
func (q Quat) ApproxEqual(b Quat, eps float32) bool {
	same := ApproxEqual(q.X, b.X, eps) &&
		ApproxEqual(q.Y, b.Y, eps) &&
		ApproxEqual(q.Z, b.Z, eps) &&
		ApproxEqual(q.W, b.W, eps)
	if same {
		return true
	}
	return ApproxEqual(q.X, -b.X, eps) &&
		ApproxEqual(q.Y, -b.Y, eps) &&
		ApproxEqual(q.Z, -b.Z, eps) &&
		ApproxEqual(q.W, -b.W, eps)
}

// LookRotation returns a unit quaternion that orients an object's local forward
// (-Z) along forward and its local up (+Y) toward up.
//
// For a camera at eye looking at target, Transform{Pos: eye, Rot: LookRotation(target.Sub(eye), up), Scale: Vec3One()}.ToMat4()
// is the inverse of LookAt(eye, target, up).
func LookRotation(forward, up Vec3) Quat {
	if forward.LenSq() < 1e-12 {
		return QuatIdentity()
	}
	f := forward.Norm()
	zAxis := f.Neg() // local +Z points opposite forward (-Z)
	xAxis := f.Cross(up)
	if xAxis.LenSq() < 1e-12 {
		fallback := Vec3{0, 0, 1}
		if absf(f.Z) > 0.9 {
			fallback = Vec3{1, 0, 0}
		}
		xAxis = f.Cross(fallback)
	}
	xAxis = xAxis.Norm()
	yAxis := zAxis.Cross(xAxis)
	return QuatFromMat3(Mat3FromCols(xAxis, yAxis, zAxis))
}

// Transform represents a 3D TRS (translation, rotation, scale) transform.
// Its zero value has zero scale and a zero quaternion; use TransformIdentity
// for a default identity transform.
type Transform struct {
	Pos   Vec3
	Rot   Quat
	Scale Vec3
}

// TransformIdentity returns the identity transform (Pos: 0, Rot: identity, Scale: 1).
func TransformIdentity() Transform {
	return Transform{
		Pos:   Vec3Zero(),
		Rot:   QuatIdentity(),
		Scale: Vec3One(),
	}
}

// ToMat4 converts t to a column-major 4x4 matrix in T * R * S order.
func (t Transform) ToMat4() Mat4 {
	return TRS(t.Pos, t.Rot, t.Scale)
}

// Mul composes parent and child transforms (parent * child), transforming
// child's local space into parent's space.
//
// Because a Transform cannot represent shear, this matches parent.ToMat4().Mul(child.ToMat4())
// exactly whenever parent.Scale is uniform (or child has no rotation). For
// hierarchies with non-uniform parent scale and rotated children, compose as Mat4.
func (t Transform) Mul(child Transform) Transform {
	return Transform{
		Pos:   t.Pos.Add(t.Rot.RotateVec3(t.Scale.Mul(child.Pos))),
		Rot:   t.Rot.Mul(child.Rot).Norm(),
		Scale: t.Scale.Mul(child.Scale),
	}
}

// Inverse returns the inverse of t. It is exact for rigid and uniform-scale
// transforms (and always satisfies t.Mul(t.Inverse()) == TransformIdentity when
// scale components are non-zero). For inverting non-uniform scale combined with
// rotation in matrix form, use t.ToMat4().Inverse().
func (t Transform) Inverse() Transform {
	invRot := t.Rot.Inverse()
	invScale := Vec3{
		safeInv(t.Scale.X),
		safeInv(t.Scale.Y),
		safeInv(t.Scale.Z),
	}
	invPos := invRot.RotateVec3(t.Pos.Neg()).Mul(invScale)
	return Transform{
		Pos:   invPos,
		Rot:   invRot,
		Scale: invScale,
	}
}

func safeInv(x float32) float32 {
	if x == 0 {
		return 0
	}
	return 1 / x
}

// TransformPoint applies t to point p (scale, rotate, then translate).
func (t Transform) TransformPoint(p Vec3) Vec3 {
	return t.Pos.Add(t.Rot.RotateVec3(t.Scale.Mul(p)))
}

// TransformDir applies t's scale and rotation to direction vector d (ignoring translation).
func (t Transform) TransformDir(d Vec3) Vec3 {
	return t.Rot.RotateVec3(t.Scale.Mul(d))
}

// Lerp interpolates from t (alpha=0) to b (alpha=1), using linear interpolation
// for position and scale and shortest-path Slerp for rotation.
func (t Transform) Lerp(b Transform, alpha float32) Transform {
	return Transform{
		Pos:   t.Pos.Lerp(b.Pos, alpha),
		Rot:   t.Rot.Slerp(b.Rot, alpha),
		Scale: t.Scale.Lerp(b.Scale, alpha),
	}
}

// ApproxEqual reports whether t and b are approximately equal within eps.
func (t Transform) ApproxEqual(b Transform, eps float32) bool {
	return t.Pos.ApproxEqual(b.Pos, eps) &&
		t.Rot.ApproxEqual(b.Rot, eps) &&
		t.Scale.ApproxEqual(b.Scale, eps)
}
