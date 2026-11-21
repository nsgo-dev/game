package math3d

import "math"

// AABB is an axis-aligned bounding box defined by its minimum and maximum corners.
// An AABB is empty when Min.X > Max.X, Min.Y > Max.Y, or Min.Z > Max.Z.
// Use AABBEmpty to construct an identity box for incremental expansion.
type AABB struct {
	Min, Max Vec3
}

// AABBEmpty returns an empty AABB with Min at +Inf and Max at -Inf, ready to
// accumulate points with ExpandPoint or boxes with Union.
func AABBEmpty() AABB {
	posInf := float32(math.Inf(1))
	negInf := float32(math.Inf(-1))
	return AABB{
		Min: Vec3{posInf, posInf, posInf},
		Max: Vec3{negInf, negInf, negInf},
	}
}

// Empty reports whether b represents an empty volume (any Min component exceeds Max).
func (b AABB) Empty() bool {
	return b.Min.X > b.Max.X || b.Min.Y > b.Max.Y || b.Min.Z > b.Max.Z
}

// Center returns the center point of b, or Vec3Zero if b is empty.
func (b AABB) Center() Vec3 {
	if b.Empty() {
		return Vec3{}
	}
	return b.Min.Add(b.Max).Scale(0.5)
}

// Size returns the full width, height, and depth of b (Max - Min), or Vec3Zero if b is empty.
func (b AABB) Size() Vec3 {
	if b.Empty() {
		return Vec3{}
	}
	return b.Max.Sub(b.Min)
}

// HalfExtents returns half the size of b along each axis, or Vec3Zero if b is empty.
func (b AABB) HalfExtents() Vec3 {
	if b.Empty() {
		return Vec3{}
	}
	return b.Max.Sub(b.Min).Scale(0.5)
}

// ExpandPoint returns the smallest AABB containing both b and point p.
func (b AABB) ExpandPoint(p Vec3) AABB {
	if b.Empty() {
		return AABB{Min: p, Max: p}
	}
	return AABB{
		Min: b.Min.Min(p),
		Max: b.Max.Max(p),
	}
}

// Union returns the smallest AABB containing both b and other.
func (b AABB) Union(other AABB) AABB {
	if b.Empty() {
		return other
	}
	if other.Empty() {
		return b
	}
	return AABB{
		Min: b.Min.Min(other.Min),
		Max: b.Max.Max(other.Max),
	}
}

// Contains reports whether p lies inside or on the boundary of b.
func (b AABB) Contains(p Vec3) bool {
	if b.Empty() {
		return false
	}
	return p.X >= b.Min.X && p.X <= b.Max.X &&
		p.Y >= b.Min.Y && p.Y <= b.Max.Y &&
		p.Z >= b.Min.Z && p.Z <= b.Max.Z
}

// Intersects reports whether b overlaps or touches other.
func (b AABB) Intersects(other AABB) bool {
	if b.Empty() || other.Empty() {
		return false
	}
	return b.Min.X <= other.Max.X && b.Max.X >= other.Min.X &&
		b.Min.Y <= other.Max.Y && b.Max.Y >= other.Min.Y &&
		b.Min.Z <= other.Max.Z && b.Max.Z >= other.Min.Z
}

// Transform returns the tight axis-aligned bounding box enclosing b after
// transforming by m. For affine matrices this uses Arvo's exact O(1) method;
// for projective matrices all 8 corners are transformed.
func (b AABB) Transform(m Mat4) AABB {
	if b.Empty() {
		return AABBEmpty()
	}
	if m[3] == 0 && m[7] == 0 && m[11] == 0 && m[15] == 1 {
		var lo, hi [3]float32
		for r := range 3 {
			lo[r] = m[12+r]
			hi[r] = m[12+r]

			ax := m[r] * b.Min.X
			bx := m[r] * b.Max.X
			lo[r] += min(ax, bx)
			hi[r] += max(ax, bx)

			ay := m[4+r] * b.Min.Y
			by := m[4+r] * b.Max.Y
			lo[r] += min(ay, by)
			hi[r] += max(ay, by)

			az := m[8+r] * b.Min.Z
			bz := m[8+r] * b.Max.Z
			lo[r] += min(az, bz)
			hi[r] += max(az, bz)
		}
		return AABB{
			Min: Vec3{lo[0], lo[1], lo[2]},
			Max: Vec3{hi[0], hi[1], hi[2]},
		}
	}

	out := AABBEmpty()
	xs := [2]float32{b.Min.X, b.Max.X}
	ys := [2]float32{b.Min.Y, b.Max.Y}
	zs := [2]float32{b.Min.Z, b.Max.Z}
	for _, x := range xs {
		for _, y := range ys {
			for _, z := range zs {
				out = out.ExpandPoint(m.TransformPoint(Vec3{x, y, z}))
			}
		}
	}
	return out
}

// Sphere is a 3D bounding sphere defined by Center and Radius.
type Sphere struct {
	Center Vec3
	Radius float32
}

// Contains reports whether p lies inside or on the surface of s.
func (s Sphere) Contains(p Vec3) bool {
	if s.Radius < 0 {
		return false
	}
	return s.Center.DistSq(p) <= s.Radius*s.Radius
}

// IntersectsSphere reports whether s overlaps or touches other.
func (s Sphere) IntersectsSphere(other Sphere) bool {
	if s.Radius < 0 || other.Radius < 0 {
		return false
	}
	r := s.Radius + other.Radius
	return s.Center.DistSq(other.Center) <= r*r
}

// IntersectsAABB reports whether s overlaps or touches b.
func (s Sphere) IntersectsAABB(b AABB) bool {
	if s.Radius < 0 || b.Empty() {
		return false
	}
	closest := s.Center.Clamp(b.Min, b.Max)
	return s.Center.DistSq(closest) <= s.Radius*s.Radius
}

// Plane is an infinite plane satisfying Normal · p + D = 0.
// When Normal is unit-length, SignedDist returns the signed Euclidean distance
// from the plane (positive on the side Normal points toward).
type Plane struct {
	Normal Vec3
	D      float32
}

// PlaneFromPointNormal builds a normalized plane passing through point with
// the given outward normal.
func PlaneFromPointNormal(point, normal Vec3) Plane {
	n := normal.Norm()
	return Plane{Normal: n, D: -n.Dot(point)}
}

// SignedDist returns Normal · point + D (signed Euclidean distance when Normal is normalized).
func (p Plane) SignedDist(point Vec3) float32 {
	return p.Normal.Dot(point) + p.D
}

// Normalized returns an equivalent plane with a unit-length Normal.
// If Normal has zero length, p is returned unchanged.
func (p Plane) Normalized() Plane {
	l := p.Normal.Len()
	if l == 0 {
		return p
	}
	inv := 1 / l
	return Plane{
		Normal: p.Normal.Scale(inv),
		D:      p.D * inv,
	}
}

// Ray is a half-line P(t) = Origin + t * Dir for t >= 0.
type Ray struct {
	Origin, Dir Vec3
}

// At returns the point along the ray at parameter t: Origin + t * Dir.
func (r Ray) At(t float32) Vec3 {
	return r.Origin.Add(r.Dir.Scale(t))
}

// IntersectAABB tests r against b using the slab method.
// On a hit, tMin is the nearest non-negative entry parameter (0 when r.Origin
// lies inside b) and tMax is the exit parameter.
func (r Ray) IntersectAABB(b AABB) (tMin, tMax float32, hit bool) {
	if b.Empty() {
		return 0, 0, false
	}
	if r.Dir == (Vec3{}) {
		if b.Contains(r.Origin) {
			return 0, 0, true
		}
		return 0, 0, false
	}

	t0 := float32(0)
	t1 := float32(math.Inf(1))

	if !slabAxis(r.Origin.X, r.Dir.X, b.Min.X, b.Max.X, &t0, &t1) {
		return 0, 0, false
	}
	if !slabAxis(r.Origin.Y, r.Dir.Y, b.Min.Y, b.Max.Y, &t0, &t1) {
		return 0, 0, false
	}
	if !slabAxis(r.Origin.Z, r.Dir.Z, b.Min.Z, b.Max.Z, &t0, &t1) {
		return 0, 0, false
	}
	return t0, t1, true
}

func slabAxis(origin, dir, bMin, bMax float32, t0, t1 *float32) bool {
	if dir == 0 {
		return origin >= bMin && origin <= bMax
	}
	invD := 1 / dir
	near := (bMin - origin) * invD
	far := (bMax - origin) * invD
	if near > far {
		near, far = far, near
	}
	if near > *t0 {
		*t0 = near
	}
	if far < *t1 {
		*t1 = far
	}
	return *t0 <= *t1
}

// IntersectSphere tests r against sphere s, returning the smallest non-negative
// parameter t where r.At(t) touches the surface of s.
// Works whether or not r.Dir is unit-length.
func (r Ray) IntersectSphere(s Sphere) (t float32, hit bool) {
	if s.Radius < 0 {
		return 0, false
	}
	a := r.Dir.LenSq()
	if a == 0 {
		return 0, false
	}
	oc := r.Origin.Sub(s.Center)
	halfB := oc.Dot(r.Dir)
	c := oc.LenSq() - s.Radius*s.Radius
	disc := halfB*halfB - a*c
	if disc < 0 {
		return 0, false
	}
	sqrtDisc := sqrtf(disc)
	t0 := (-halfB - sqrtDisc) / a
	if t0 >= 0 {
		return t0, true
	}
	t1 := (-halfB + sqrtDisc) / a
	if t1 >= 0 {
		return t1, true
	}
	return 0, false
}

// IntersectPlane tests r against plane p, returning the parameter t >= 0 of the
// intersection point, or false if the ray is parallel to or points away from p.
func (r Ray) IntersectPlane(p Plane) (t float32, hit bool) {
	denom := p.Normal.Dot(r.Dir)
	if absf(denom) < 1e-7 {
		return 0, false
	}
	t = -(p.Normal.Dot(r.Origin) + p.D) / denom
	if t < 0 {
		return 0, false
	}
	return t, true
}

// IntersectTriangle tests r against triangle (v0, v1, v2) using the
// Möller-Trumbore algorithm (double-sided).
// On a hit, t >= 0 is the ray parameter and (u, v) are barycentric coordinates
// such that r.At(t) == v0*(1-u-v) + v1*u + v2*v.
func (r Ray) IntersectTriangle(v0, v1, v2 Vec3) (t, u, v float32, hit bool) {
	const eps = 1e-7
	e1 := v1.Sub(v0)
	e2 := v2.Sub(v0)
	h := r.Dir.Cross(e2)
	det := e1.Dot(h)
	if absf(det) < eps {
		return 0, 0, 0, false
	}
	invDet := 1 / det
	s := r.Origin.Sub(v0)
	u = invDet * s.Dot(h)
	if u < 0 || u > 1 {
		return 0, 0, 0, false
	}
	q := s.Cross(e1)
	v = invDet * r.Dir.Dot(q)
	if v < 0 || u+v > 1 {
		return 0, 0, 0, false
	}
	t = invDet * e2.Dot(q)
	if t < 0 {
		return 0, 0, 0, false
	}
	return t, u, v, true
}

// Frustum plane indices.
const (
	FrustumLeft = iota
	FrustumRight
	FrustumBottom
	FrustumTop
	FrustumNear
	FrustumFar
)

// Frustum is a 6-plane view volume with normalized inward-pointing normals.
// A point lies inside the frustum when SignedDist >= 0 for all six planes.
type Frustum struct {
	Planes [6]Plane
}

// FrustumFromMat4 extracts the six normalized inward-pointing frustum planes
// from a view-projection matrix vp using the Gribb-Hartmann method, assuming
// WebGPU/Metal [0, 1] NDC depth (Perspective / PerspectiveZO / Ortho / OrthoZO).
//
// This is equivalent to FrustumFromMat4ZO. Use FrustumFromMat4NO for OpenGL
// [-1, 1] depth projections.
func FrustumFromMat4(vp Mat4) Frustum {
	return FrustumFromMat4ZO(vp)
}

// FrustumFromMat4ZO extracts frustum planes from a view-projection matrix using
// zero-to-one [0, 1] NDC depth.
func FrustumFromMat4ZO(vp Mat4) Frustum {
	r0 := vp.Row(0)
	r1 := vp.Row(1)
	r2 := vp.Row(2)
	r3 := vp.Row(3)
	return Frustum{
		Planes: [6]Plane{
			FrustumLeft:   planeFromVec4(r3.Add(r0)).Normalized(),
			FrustumRight:  planeFromVec4(r3.Sub(r0)).Normalized(),
			FrustumBottom: planeFromVec4(r3.Add(r1)).Normalized(),
			FrustumTop:    planeFromVec4(r3.Sub(r1)).Normalized(),
			FrustumNear:   planeFromVec4(r2).Normalized(),
			FrustumFar:    planeFromVec4(r3.Sub(r2)).Normalized(),
		},
	}
}

// FrustumFromMat4NO extracts frustum planes from a view-projection matrix using
// negative-one-to-one [-1, 1] NDC depth (PerspectiveNO / OrthoNO).
func FrustumFromMat4NO(vp Mat4) Frustum {
	r0 := vp.Row(0)
	r1 := vp.Row(1)
	r2 := vp.Row(2)
	r3 := vp.Row(3)
	return Frustum{
		Planes: [6]Plane{
			FrustumLeft:   planeFromVec4(r3.Add(r0)).Normalized(),
			FrustumRight:  planeFromVec4(r3.Sub(r0)).Normalized(),
			FrustumBottom: planeFromVec4(r3.Add(r1)).Normalized(),
			FrustumTop:    planeFromVec4(r3.Sub(r1)).Normalized(),
			FrustumNear:   planeFromVec4(r3.Add(r2)).Normalized(),
			FrustumFar:    planeFromVec4(r3.Sub(r2)).Normalized(),
		},
	}
}

func planeFromVec4(v Vec4) Plane {
	return Plane{Normal: Vec3{v.X, v.Y, v.Z}, D: v.W}
}

// ContainsPoint reports whether p lies inside or on the boundary of f.
func (f Frustum) ContainsPoint(p Vec3) bool {
	for _, plane := range f.Planes {
		if plane.SignedDist(p) < 0 {
			return false
		}
	}
	return true
}

// IntersectsSphere reports whether sphere s lies inside or intersects f.
func (f Frustum) IntersectsSphere(s Sphere) bool {
	if s.Radius < 0 {
		return false
	}
	for _, plane := range f.Planes {
		if plane.SignedDist(s.Center) < -s.Radius {
			return false
		}
	}
	return true
}

// IntersectsAABB reports whether box b lies inside or intersects f using the
// positive-vertex (p-vertex) test against each frustum plane.
func (f Frustum) IntersectsAABB(b AABB) bool {
	if b.Empty() {
		return false
	}
	for _, plane := range f.Planes {
		px := b.Min.X
		if plane.Normal.X >= 0 {
			px = b.Max.X
		}
		py := b.Min.Y
		if plane.Normal.Y >= 0 {
			py = b.Max.Y
		}
		pz := b.Min.Z
		if plane.Normal.Z >= 0 {
			pz = b.Max.Z
		}
		if plane.SignedDist(Vec3{px, py, pz}) < 0 {
			return false
		}
	}
	return true
}
