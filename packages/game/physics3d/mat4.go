package physics3d

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// Mat4 is a 4x4 matrix stored in column-major order, matching OpenGL, WebGPU,
// and glTF conventions. Element (row, col) is stored at index col*4 + row, so
// the translation column occupies indices 12, 13, and 14.
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

// Mat4Mul returns the matrix product a * b (column-major).
func Mat4Mul(a, b Mat4) Mat4 {
	var out Mat4
	for col := 0; col < 4; col++ {
		c := col * 4
		b0, b1, b2, b3 := b[c], b[c+1], b[c+2], b[c+3]
		for row := 0; row < 4; row++ {
			out[c+row] = a[row]*b0 + a[4+row]*b1 + a[8+row]*b2 + a[12+row]*b3
		}
	}
	return out
}

// Mat4Perspective returns a right-handed perspective projection matrix mapping
// view-space Z in [-near, -far] to normalized device coordinates Z in [-1, 1].
// fovYRad is the vertical field of view in radians, aspect is width/height, and
// near and far are positive distances to the clipping planes (0 < near < far).
// Degenerate parameters return Mat4Identity().
func Mat4Perspective(fovYRad, aspect, near, far float32) Mat4 {
	if fovYRad <= 0 || fovYRad >= math.Pi || aspect <= 0 || near <= 0 || far <= near {
		return Mat4Identity()
	}
	t := float32(math.Tan(float64(fovYRad) * 0.5))
	if t <= 0 {
		return Mat4Identity()
	}
	f := 1 / t
	nf := 1 / (near - far)
	return Mat4{
		f / aspect, 0, 0, 0,
		0, f, 0, 0,
		0, 0, (far + near) * nf, -1,
		0, 0, 2 * far * near * nf, 0,
	}
}

// Mat4Ortho returns a right-handed orthographic projection matrix mapping
// [left, right] x [bottom, top] x [-near, -far] to normalized device
// coordinates [-1, 1]^3. Degenerate planes return Mat4Identity().
func Mat4Ortho(left, right, bottom, top, near, far float32) Mat4 {
	rl := right - left
	tb := top - bottom
	fn := far - near
	if rl == 0 || tb == 0 || fn == 0 {
		return Mat4Identity()
	}
	return Mat4{
		2 / rl, 0, 0, 0,
		0, 2 / tb, 0, 0,
		0, 0, -2 / fn, 0,
		-(right + left) / rl, -(top + bottom) / tb, -(far + near) / fn, 1,
	}
}

// Mat4LookAt returns a right-handed view matrix placing the camera at eye,
// looking toward target, with world up direction up.
func Mat4LookAt(eye, target, up Vec3) Mat4 {
	f := target.Sub(eye).Norm()
	if f.LenSq() == 0 {
		f = Vec3{Z: -1}
	}
	s := f.Cross(up).Norm()
	if s.LenSq() == 0 {
		fallback := Vec3{Y: 1}
		if abs32(f.Y) >= 0.9 {
			fallback = Vec3{Z: -1}
		}
		s = f.Cross(fallback).Norm()
	}
	u := s.Cross(f)
	return Mat4{
		s.X, u.X, -f.X, 0,
		s.Y, u.Y, -f.Y, 0,
		s.Z, u.Z, -f.Z, 0,
		-s.Dot(eye), -u.Dot(eye), f.Dot(eye), 1,
	}
}

type vec4 struct {
	x, y, z, w float32
}

func (m Mat4) mulPoint(p Vec3) vec4 {
	return vec4{
		x: m[0]*p.X + m[4]*p.Y + m[8]*p.Z + m[12],
		y: m[1]*p.X + m[5]*p.Y + m[9]*p.Z + m[13],
		z: m[2]*p.X + m[6]*p.Y + m[10]*p.Z + m[14],
		w: m[3]*p.X + m[7]*p.Y + m[11]*p.Z + m[15],
	}
}

func ndcToViewport(ndcX, ndcY float32, viewport core.Bounds) core.Point {
	sx := float32(viewport.Origin.X) + (ndcX*0.5+0.5)*float32(viewport.Size.Width)
	sy := float32(viewport.Origin.Y) + (0.5-ndcY*0.5)*float32(viewport.Size.Height)
	return core.Point{X: core.Points(sx), Y: core.Points(sy)}
}

func isFinite32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// Project transforms world-space point p by viewProj and maps normalized device coordinates to viewport screen coordinates (X left-to-right in [viewport.Origin.X, viewport.Origin.X+viewport.Size.Width], Y top-down in [viewport.Origin.Y, viewport.Origin.Y+viewport.Size.Height]).
//
// It returns the screen point, the normalized depth ndcZ, and ok == true when clip w > 0 and ndcZ is within [-1, 1], the depth range of this package's Mat4Perspective and Mat4Ortho. For a [0, 1] depth projection (math3d.Perspective, math3d.Ortho, a render3d camera) the caller must also require ndcZ >= 0: an orthographic [0, 1] camera has w = 1 everywhere, so a point behind it gets ndcZ in [-1, 0) and is still reported ok, as [DebugStyle.DepthZO] does for DrawContact.
func Project(viewProj Mat4, viewport core.Bounds, p Vec3) (core.Point, float32, bool) {
	if viewport.Size.Width <= 0 || viewport.Size.Height <= 0 {
		return core.Point{}, 0, false
	}
	c := viewProj.mulPoint(p)
	if c.w <= 1e-6 || !isFinite32(c.x) || !isFinite32(c.y) || !isFinite32(c.z) || !isFinite32(c.w) {
		return core.Point{}, 0, false
	}
	invW := 1 / c.w
	ndcX := c.x * invW
	ndcY := c.y * invW
	ndcZ := c.z * invW
	const clipEps = 1e-5
	if ndcZ < -1-clipEps || ndcZ > 1+clipEps {
		return core.Point{}, 0, false
	}
	if !isFinite32(ndcX) || !isFinite32(ndcY) || !isFinite32(ndcZ) {
		return core.Point{}, 0, false
	}
	return ndcToViewport(ndcX, ndcY, viewport), ndcZ, true
}
