package math3d

import "math"

// Mat3 is a 3x3 float32 matrix stored in column-major order.
// Element at (row, col) is at index col*3 + row.
type Mat3 [9]float32

// Mat3Identity returns the 3x3 identity matrix.
func Mat3Identity() Mat3 {
	return Mat3{
		1, 0, 0,
		0, 1, 0,
		0, 0, 1,
	}
}

// Mat3FromCols builds a Mat3 from its three column vectors.
func Mat3FromCols(c0, c1, c2 Vec3) Mat3 {
	return Mat3{
		c0.X, c0.Y, c0.Z,
		c1.X, c1.Y, c1.Z,
		c2.X, c2.Y, c2.Z,
	}
}

// Mat3FromMat4 extracts the upper-left 3x3 submatrix of m.
func Mat3FromMat4(m Mat4) Mat3 {
	return Mat3{
		m[0], m[1], m[2],
		m[4], m[5], m[6],
		m[8], m[9], m[10],
	}
}

// NormalMatrix returns the inverse-transpose of the upper-left 3x3 of model,
// suitable for transforming surface normals under non-uniform scale.
// It returns false if the upper-left 3x3 is singular.
func NormalMatrix(model Mat4) (Mat3, bool) {
	inv, ok := Mat3FromMat4(model).Inverse()
	if !ok {
		return Mat3{}, false
	}
	return inv.Transpose(), true
}

// At returns the element at (row, col).
func (m Mat3) At(row, col int) float32 { return m[col*3+row] }

// Col returns column col (0..2) as a Vec3.
func (m Mat3) Col(col int) Vec3 {
	i := col * 3
	return Vec3{m[i], m[i+1], m[i+2]}
}

// Row returns row row (0..2) as a Vec3.
func (m Mat3) Row(row int) Vec3 {
	return Vec3{m[row], m[3+row], m[6+row]}
}

// Mul returns the matrix product m * b.
func (m Mat3) Mul(b Mat3) Mat3 {
	var out Mat3
	for c := range 3 {
		b0, b1, b2 := b[c*3], b[c*3+1], b[c*3+2]
		out[c*3+0] = m[0]*b0 + m[3]*b1 + m[6]*b2
		out[c*3+1] = m[1]*b0 + m[4]*b1 + m[7]*b2
		out[c*3+2] = m[2]*b0 + m[5]*b1 + m[8]*b2
	}
	return out
}

// MulVec3 returns the column-vector product m * v.
func (m Mat3) MulVec3(v Vec3) Vec3 {
	return Vec3{
		m[0]*v.X + m[3]*v.Y + m[6]*v.Z,
		m[1]*v.X + m[4]*v.Y + m[7]*v.Z,
		m[2]*v.X + m[5]*v.Y + m[8]*v.Z,
	}
}

// Transpose returns the transpose of m.
func (m Mat3) Transpose() Mat3 {
	return Mat3{
		m[0], m[3], m[6],
		m[1], m[4], m[7],
		m[2], m[5], m[8],
	}
}

// Det returns the determinant of m.
func (m Mat3) Det() float32 {
	a00, a10, a20 := float64(m[0]), float64(m[1]), float64(m[2])
	a01, a11, a21 := float64(m[3]), float64(m[4]), float64(m[5])
	a02, a12, a22 := float64(m[6]), float64(m[7]), float64(m[8])
	return float32(
		a00*(a11*a22-a12*a21) -
			a01*(a10*a22-a12*a20) +
			a02*(a10*a21-a11*a20),
	)
}

// Inverse returns the inverse of m and true, or the zero matrix and false if m is singular.
func (m Mat3) Inverse() (Mat3, bool) {
	a00, a10, a20 := float64(m[0]), float64(m[1]), float64(m[2])
	a01, a11, a21 := float64(m[3]), float64(m[4]), float64(m[5])
	a02, a12, a22 := float64(m[6]), float64(m[7]), float64(m[8])

	c00 := a11*a22 - a12*a21
	c01 := -(a10*a22 - a12*a20)
	c02 := a10*a21 - a11*a20

	det := a00*c00 + a01*c01 + a02*c02
	if math.Abs(det) < 1e-12 {
		return Mat3{}, false
	}

	c10 := -(a01*a22 - a02*a21)
	c11 := a00*a22 - a02*a20
	c12 := -(a00*a21 - a01*a20)

	c20 := a01*a12 - a02*a11
	c21 := -(a00*a12 - a02*a10)
	c22 := a00*a11 - a01*a10

	invDet := 1.0 / det
	return Mat3{
		float32(c00 * invDet), float32(c01 * invDet), float32(c02 * invDet),
		float32(c10 * invDet), float32(c11 * invDet), float32(c12 * invDet),
		float32(c20 * invDet), float32(c21 * invDet), float32(c22 * invDet),
	}, true
}

// ApproxEqual reports whether every element of m is within eps of b.
func (m Mat3) ApproxEqual(b Mat3, eps float32) bool {
	for i := range m {
		if !ApproxEqual(m[i], b[i], eps) {
			return false
		}
	}
	return true
}

// Mat4 is a 4x4 float32 matrix stored in column-major order, matching WGSL,
// Metal, GLSL, and glTF 2.0 directly. Element at (row, col) is at index col*4 + row.
type Mat4 [16]float32

// Mat4Identity returns the 4x4 identity matrix.
func Mat4Identity() Mat4 {
	return Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

// Mat4FromCols builds a Mat4 from its four column vectors.
func Mat4FromCols(c0, c1, c2, c3 Vec4) Mat4 {
	return Mat4{
		c0.X, c0.Y, c0.Z, c0.W,
		c1.X, c1.Y, c1.Z, c1.W,
		c2.X, c2.Y, c2.Z, c2.W,
		c3.X, c3.Y, c3.Z, c3.W,
	}
}

// At returns the element at (row, col).
func (m Mat4) At(row, col int) float32 { return m[col*4+row] }

// Col returns column col (0..3) as a Vec4.
func (m Mat4) Col(col int) Vec4 {
	i := col * 4
	return Vec4{m[i], m[i+1], m[i+2], m[i+3]}
}

// Row returns row row (0..3) as a Vec4.
func (m Mat4) Row(row int) Vec4 {
	return Vec4{m[row], m[4+row], m[8+row], m[12+row]}
}

// Mat3 extracts the upper-left 3x3 submatrix of m.
func (m Mat4) Mat3() Mat3 { return Mat3FromMat4(m) }

// Mul returns the matrix product m * b (applying b first, then m).
func (m Mat4) Mul(b Mat4) Mat4 {
	var out Mat4
	for c := range 4 {
		b0, b1, b2, b3 := b[c*4], b[c*4+1], b[c*4+2], b[c*4+3]
		out[c*4+0] = m[0]*b0 + m[4]*b1 + m[8]*b2 + m[12]*b3
		out[c*4+1] = m[1]*b0 + m[5]*b1 + m[9]*b2 + m[13]*b3
		out[c*4+2] = m[2]*b0 + m[6]*b1 + m[10]*b2 + m[14]*b3
		out[c*4+3] = m[3]*b0 + m[7]*b1 + m[11]*b2 + m[15]*b3
	}
	return out
}

// MulVec4 returns the column-vector product m * v.
func (m Mat4) MulVec4(v Vec4) Vec4 {
	return Vec4{
		m[0]*v.X + m[4]*v.Y + m[8]*v.Z + m[12]*v.W,
		m[1]*v.X + m[5]*v.Y + m[9]*v.Z + m[13]*v.W,
		m[2]*v.X + m[6]*v.Y + m[10]*v.Z + m[14]*v.W,
		m[3]*v.X + m[7]*v.Y + m[11]*v.Z + m[15]*v.W,
	}
}

// TransformPoint transforms a 3D point p as (p.X, p.Y, p.Z, 1), performing
// perspective division by w when w is neither 1 nor 0.
func (m Mat4) TransformPoint(p Vec3) Vec3 {
	x := m[0]*p.X + m[4]*p.Y + m[8]*p.Z + m[12]
	y := m[1]*p.X + m[5]*p.Y + m[9]*p.Z + m[13]
	z := m[2]*p.X + m[6]*p.Y + m[10]*p.Z + m[14]
	w := m[3]*p.X + m[7]*p.Y + m[11]*p.Z + m[15]
	if w != 1 && w != 0 {
		invW := 1 / w
		return Vec3{x * invW, y * invW, z * invW}
	}
	return Vec3{x, y, z}
}

// TransformDir transforms a 3D direction vector d as (d.X, d.Y, d.Z, 0),
// ignoring translation.
func (m Mat4) TransformDir(d Vec3) Vec3 {
	return Vec3{
		m[0]*d.X + m[4]*d.Y + m[8]*d.Z,
		m[1]*d.X + m[5]*d.Y + m[9]*d.Z,
		m[2]*d.X + m[6]*d.Y + m[10]*d.Z,
	}
}

// Transpose returns the transpose of m.
func (m Mat4) Transpose() Mat4 {
	return Mat4{
		m[0], m[4], m[8], m[12],
		m[1], m[5], m[9], m[13],
		m[2], m[6], m[10], m[14],
		m[3], m[7], m[11], m[15],
	}
}

// Det returns the determinant of m.
func (m Mat4) Det() float32 {
	a00, a10, a20, a30 := float64(m[0]), float64(m[1]), float64(m[2]), float64(m[3])
	a01, a11, a21, a31 := float64(m[4]), float64(m[5]), float64(m[6]), float64(m[7])
	a02, a12, a22, a32 := float64(m[8]), float64(m[9]), float64(m[10]), float64(m[11])
	a03, a13, a23, a33 := float64(m[12]), float64(m[13]), float64(m[14]), float64(m[15])

	s0 := a00*a11 - a01*a10
	s1 := a00*a12 - a02*a10
	s2 := a00*a13 - a03*a10
	s3 := a01*a12 - a02*a11
	s4 := a01*a13 - a03*a11
	s5 := a02*a13 - a03*a12

	c5 := a22*a33 - a23*a32
	c4 := a21*a33 - a23*a31
	c3 := a21*a32 - a22*a31
	c2 := a20*a33 - a23*a30
	c1 := a20*a32 - a22*a30
	c0 := a20*a31 - a21*a30

	return float32(s0*c5 - s1*c4 + s2*c3 + s3*c2 - s4*c1 + s5*c0)
}

// Inverse returns the inverse of m and true, or the zero matrix and false if m is singular.
func (m Mat4) Inverse() (Mat4, bool) {
	a00, a10, a20, a30 := float64(m[0]), float64(m[1]), float64(m[2]), float64(m[3])
	a01, a11, a21, a31 := float64(m[4]), float64(m[5]), float64(m[6]), float64(m[7])
	a02, a12, a22, a32 := float64(m[8]), float64(m[9]), float64(m[10]), float64(m[11])
	a03, a13, a23, a33 := float64(m[12]), float64(m[13]), float64(m[14]), float64(m[15])

	s0 := a00*a11 - a01*a10
	s1 := a00*a12 - a02*a10
	s2 := a00*a13 - a03*a10
	s3 := a01*a12 - a02*a11
	s4 := a01*a13 - a03*a11
	s5 := a02*a13 - a03*a12

	c5 := a22*a33 - a23*a32
	c4 := a21*a33 - a23*a31
	c3 := a21*a32 - a22*a31
	c2 := a20*a33 - a23*a30
	c1 := a20*a32 - a22*a30
	c0 := a20*a31 - a21*a30

	det := s0*c5 - s1*c4 + s2*c3 + s3*c2 - s4*c1 + s5*c0
	if math.Abs(det) < 1e-12 {
		return Mat4{}, false
	}
	invDet := 1.0 / det

	return Mat4{
		// Column 0
		float32((a11*c5 - a12*c4 + a13*c3) * invDet),
		float32((-a10*c5 + a12*c2 - a13*c1) * invDet),
		float32((a10*c4 - a11*c2 + a13*c0) * invDet),
		float32((-a10*c3 + a11*c1 - a12*c0) * invDet),
		// Column 1
		float32((-a01*c5 + a02*c4 - a03*c3) * invDet),
		float32((a00*c5 - a02*c2 + a03*c1) * invDet),
		float32((-a00*c4 + a01*c2 - a03*c0) * invDet),
		float32((a00*c3 - a01*c1 + a02*c0) * invDet),
		// Column 2
		float32((a31*s5 - a32*s4 + a33*s3) * invDet),
		float32((-a30*s5 + a32*s2 - a33*s1) * invDet),
		float32((a30*s4 - a31*s2 + a33*s0) * invDet),
		float32((-a30*s3 + a31*s1 - a32*s0) * invDet),
		// Column 3
		float32((-a21*s5 + a22*s4 - a23*s3) * invDet),
		float32((a20*s5 - a22*s2 + a23*s1) * invDet),
		float32((-a20*s4 + a21*s2 - a23*s0) * invDet),
		float32((a20*s3 - a21*s1 + a22*s0) * invDet),
	}, true
}

// ApproxEqual reports whether every element of m is within eps of b.
func (m Mat4) ApproxEqual(b Mat4, eps float32) bool {
	for i := range m {
		if !ApproxEqual(m[i], b[i], eps) {
			return false
		}
	}
	return true
}

// Translate returns a translation matrix by v.
func Translate(v Vec3) Mat4 {
	return Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		v.X, v.Y, v.Z, 1,
	}
}

// Scale returns a scaling matrix with diagonal (v.X, v.Y, v.Z, 1).
func Scale(v Vec3) Mat4 {
	return Mat4{
		v.X, 0, 0, 0,
		0, v.Y, 0, 0,
		0, 0, v.Z, 0,
		0, 0, 0, 1,
	}
}

// RotateX returns a right-handed rotation matrix around the +X axis by rad radians.
func RotateX(rad float32) Mat4 {
	s, c := math.Sincos(float64(rad))
	sf, cf := float32(s), float32(c)
	return Mat4{
		1, 0, 0, 0,
		0, cf, sf, 0,
		0, -sf, cf, 0,
		0, 0, 0, 1,
	}
}

// RotateY returns a right-handed rotation matrix around the +Y axis by rad radians.
func RotateY(rad float32) Mat4 {
	s, c := math.Sincos(float64(rad))
	sf, cf := float32(s), float32(c)
	return Mat4{
		cf, 0, -sf, 0,
		0, 1, 0, 0,
		sf, 0, cf, 0,
		0, 0, 0, 1,
	}
}

// RotateZ returns a right-handed rotation matrix around the +Z axis by rad radians.
func RotateZ(rad float32) Mat4 {
	s, c := math.Sincos(float64(rad))
	sf, cf := float32(s), float32(c)
	return Mat4{
		cf, sf, 0, 0,
		-sf, cf, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}
}

// RotateAxis returns a right-handed rotation matrix around axis by rad radians.
// If axis has zero length, Mat4Identity is returned.
func RotateAxis(axis Vec3, rad float32) Mat4 {
	l := axis.Len()
	if l == 0 {
		return Mat4Identity()
	}
	inv := 1 / l
	x, y, z := axis.X*inv, axis.Y*inv, axis.Z*inv
	s, c := math.Sincos(float64(rad))
	sf, cf := float32(s), float32(c)
	t := 1 - cf
	return Mat4{
		t*x*x + cf, t*x*y + sf*z, t*x*z - sf*y, 0,
		t*x*y - sf*z, t*y*y + cf, t*y*z + sf*x, 0,
		t*x*z + sf*y, t*y*z - sf*x, t*z*z + cf, 0,
		0, 0, 0, 1,
	}
}

// LookAt returns a right-handed view matrix for a camera positioned at eye,
// looking toward target, with world up direction up. In the resulting view
// space, the camera sits at the origin looking down -Z with +X right and +Y up.
func LookAt(eye, target, up Vec3) Mat4 {
	f := target.Sub(eye)
	if f.LenSq() < 1e-12 {
		return Translate(eye.Neg())
	}
	f = f.Norm()
	r := f.Cross(up)
	if r.LenSq() < 1e-12 {
		// Forward is parallel to up; choose a deterministic fallback up axis.
		fallback := Vec3{0, 0, 1}
		if absf(f.Z) > 0.9 {
			fallback = Vec3{1, 0, 0}
		}
		r = f.Cross(fallback)
	}
	r = r.Norm()
	u := r.Cross(f)

	return Mat4{
		r.X, u.X, -f.X, 0,
		r.Y, u.Y, -f.Y, 0,
		r.Z, u.Z, -f.Z, 0,
		-r.Dot(eye), -u.Dot(eye), f.Dot(eye), 1,
	}
}

// Perspective returns a right-handed perspective projection matrix mapping
// view-space z in [-near, -far] to WebGPU/Metal NDC depth [0, 1].
// fovY is the vertical field of view in radians; aspect is width/height;
// near and far are positive distances from the camera (0 < near < far).
//
// This is equivalent to PerspectiveZO. Use PerspectiveNO for OpenGL [-1, 1] depth.
func Perspective(fovY, aspect, near, far float32) Mat4 {
	return PerspectiveZO(fovY, aspect, near, far)
}

// PerspectiveZO returns a right-handed perspective projection matrix for
// zero-to-one NDC depth [0, 1] (WebGPU, Metal, Vulkan, D3D).
func PerspectiveZO(fovY, aspect, near, far float32) Mat4 {
	f := float32(1.0 / math.Tan(float64(fovY)*0.5))
	invNF := 1 / (far - near)
	return Mat4{
		f / aspect, 0, 0, 0,
		0, f, 0, 0,
		0, 0, -far * invNF, -1,
		0, 0, -far * near * invNF, 0,
	}
}

// PerspectiveNO returns a right-handed perspective projection matrix for
// negative-one-to-one NDC depth [-1, 1] (OpenGL, WebGL).
func PerspectiveNO(fovY, aspect, near, far float32) Mat4 {
	f := float32(1.0 / math.Tan(float64(fovY)*0.5))
	invNF := 1 / (far - near)
	return Mat4{
		f / aspect, 0, 0, 0,
		0, f, 0, 0,
		0, 0, -(far + near) * invNF, -1,
		0, 0, -2 * far * near * invNF, 0,
	}
}

// Ortho returns a right-handed orthographic projection matrix mapping
// view-space [left, right] x [bottom, top] x [-far, -near] to
// [-1, 1] x [-1, 1] x [0, 1] (WebGPU/Metal NDC depth [0, 1]).
//
// This is equivalent to OrthoZO. Use OrthoNO for OpenGL [-1, 1] depth.
func Ortho(left, right, bottom, top, near, far float32) Mat4 {
	return OrthoZO(left, right, bottom, top, near, far)
}

// OrthoZO returns a right-handed orthographic projection matrix for
// zero-to-one NDC depth [0, 1] (WebGPU, Metal, Vulkan, D3D).
func OrthoZO(left, right, bottom, top, near, far float32) Mat4 {
	invRL := 1 / (right - left)
	invTB := 1 / (top - bottom)
	invFN := 1 / (far - near)
	return Mat4{
		2 * invRL, 0, 0, 0,
		0, 2 * invTB, 0, 0,
		0, 0, -invFN, 0,
		-(right + left) * invRL, -(top + bottom) * invTB, -near * invFN, 1,
	}
}

// OrthoNO returns a right-handed orthographic projection matrix for
// negative-one-to-one NDC depth [-1, 1] (OpenGL, WebGL).
func OrthoNO(left, right, bottom, top, near, far float32) Mat4 {
	invRL := 1 / (right - left)
	invTB := 1 / (top - bottom)
	invFN := 1 / (far - near)
	return Mat4{
		2 * invRL, 0, 0, 0,
		0, 2 * invTB, 0, 0,
		0, 0, -2 * invFN, 0,
		-(right + left) * invRL, -(top + bottom) * invTB, -(far + near) * invFN, 1,
	}
}

// TRS builds a model matrix from translation pos, rotation rot, and scale
// in standard T * R * S order (scale first, then rotate, then translate).
func TRS(pos Vec3, rot Quat, scale Vec3) Mat4 {
	r := rot.ToMat4()
	r[0] *= scale.X
	r[1] *= scale.X
	r[2] *= scale.X
	r[4] *= scale.Y
	r[5] *= scale.Y
	r[6] *= scale.Y
	r[8] *= scale.Z
	r[9] *= scale.Z
	r[10] *= scale.Z
	r[12] = pos.X
	r[13] = pos.Y
	r[14] = pos.Z
	return r
}

// Decompose factors an affine TRS matrix into translation pos, unit rotation
// rot, and scale such that TRS(pos, rot, scale) reproduces m.
//
// If m has a reflection (negative upper-left 3x3 determinant), the sign is
// placed on scale.X so rot remains a proper right-handed rotation.
// It returns ok = false if m has projective terms or a zero scale axis.
func (m Mat4) Decompose() (pos Vec3, rot Quat, scale Vec3, ok bool) {
	if absf(m[3]) > 1e-5 || absf(m[7]) > 1e-5 || absf(m[11]) > 1e-5 || absf(m[15]-1) > 1e-5 {
		return Vec3{}, QuatIdentity(), Vec3{}, false
	}

	pos = Vec3{m[12], m[13], m[14]}
	c0 := Vec3{m[0], m[1], m[2]}
	c1 := Vec3{m[4], m[5], m[6]}
	c2 := Vec3{m[8], m[9], m[10]}

	sx := c0.Len()
	sy := c1.Len()
	sz := c2.Len()

	if sx < 1e-7 || sy < 1e-7 || sz < 1e-7 {
		return pos, QuatIdentity(), Vec3{sx, sy, sz}, false
	}

	if c0.Dot(c1.Cross(c2)) < 0 {
		sx = -sx
	}

	r0 := c0.Scale(1 / sx)
	r1 := c1.Scale(1 / sy)
	r2 := c2.Scale(1 / sz)

	rotMat := Mat4{
		r0.X, r0.Y, r0.Z, 0,
		r1.X, r1.Y, r1.Z, 0,
		r2.X, r2.Y, r2.Z, 0,
		0, 0, 0, 1,
	}
	return pos, QuatFromMat4(rotMat), Vec3{sx, sy, sz}, true
}
