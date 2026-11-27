package physics3d

// Ray is a directed ray starting at Origin, pointing along Dir, up to MaxDist
// units long. Dir need not be unit length; raycast functions normalize it.
// Rays with zero Dir or non-positive MaxDist report no hits.
type Ray struct {
	Origin  Vec3
	Dir     Vec3
	MaxDist float32
}

// RaycastHit describes where a ray intersects a shape, body, or static collider.
type RaycastHit struct {
	// Body is the hit body for World raycasts, or nil for standalone shape,
	// triangle-mesh, or heightfield hits.
	Body *Body
	// ChildIndex is the zero-based index of the struck child when Body or the
	// tested Shape is a ShapeCompound, or -1 for non-compound shapes and static
	// colliders.
	ChildIndex int
	// TriMesh is the hit triangle-mesh collider when the ray struck a static mesh.
	TriMesh *TriMeshCollider
	// Heightfield is the hit heightfield collider when the ray struck terrain.
	Heightfield *HeightfieldCollider
	// Voxel is the hit voxel collider when the ray struck voxel terrain.
	Voxel *VoxelCollider
	// T is the normalized hit fraction in [0, 1] along the ray's MaxDist
	// (or along the segment from -> to for World.Raycast).
	T float32
	// Distance is the Euclidean distance from the ray origin to Point.
	Distance float32
	// Point is the world-space intersection point.
	Point Vec3
	// Normal is the outward unit surface normal at Point. A ray that starts inside a box, sphere, capsule, convex hull or cylinder hits at T 0 with Point at the origin and Normal facing back along the ray (-Dir), since no surface was crossed.
	Normal Vec3
}

// RaycastShape tests ray against s.
func RaycastShape(ray Ray, s Shape) (RaycastHit, bool) {
	switch s.Kind {
	case ShapeBox:
		if r, ok := s.orientedBox(); ok {
			f := newBoxFrame(s.Box.Center, r)
			local := Ray{Origin: f.toLocal(ray.Origin), Dir: f.inv.RotateVec(ray.Dir), MaxDist: ray.MaxDist}
			hit, ok := RaycastBox(local, Box{HalfExtents: s.Box.HalfExtents})
			if ok {
				hit.Point = f.toWorld(hit.Point)
				hit.Normal = f.dirToWorld(hit.Normal)
			}
			return hit, ok
		}
		return RaycastBox(ray, s.Box)
	case ShapeSphere:
		return RaycastSphere(ray, s.Sphere)
	case ShapeCapsule:
		return RaycastCapsule(ray, s.Capsule)
	case ShapeConvexHull:
		return RaycastConvexHull(ray, s.Hull, s.Rot)
	case ShapeCylinder:
		return RaycastCylinder(ray, s.Cylinder)
	case ShapeCompound:
		return raycastCompound(ray, s.Children)
	default:
		return RaycastHit{}, false
	}
}

// RaycastBox tests ray against an axis-aligned box b.
func RaycastBox(ray Ray, b Box) (RaycastHit, bool) {
	if ray.MaxDist <= 0 {
		return RaycastHit{}, false
	}
	dir := ray.Dir.Norm()
	if dir.LenSq() == 0 {
		return RaycastHit{}, false
	}

	bMin, bMax := b.Min(), b.Max()
	tMin := float32(0)
	tMax := ray.MaxDist
	var hitNormal Vec3
	hitAxis := false

	slab := func(orig, d, lo, hi float32, negN, posN Vec3) bool {
		if abs32(d) < 1e-8 {
			return orig >= lo && orig <= hi
		}
		inv := 1 / d
		t1 := (lo - orig) * inv
		t2 := (hi - orig) * inv
		n := negN
		if t1 > t2 {
			t1, t2 = t2, t1
			n = posN
		}
		if t1 > tMin {
			tMin = t1
			hitNormal = n
			hitAxis = true
		}
		if t2 < tMax {
			tMax = t2
		}
		return tMin <= tMax
	}

	if !slab(ray.Origin.X, dir.X, bMin.X, bMax.X, Vec3{X: -1}, Vec3{X: 1}) ||
		!slab(ray.Origin.Y, dir.Y, bMin.Y, bMax.Y, Vec3{Y: -1}, Vec3{Y: 1}) ||
		!slab(ray.Origin.Z, dir.Z, bMin.Z, bMax.Z, Vec3{Z: -1}, Vec3{Z: 1}) {
		return RaycastHit{}, false
	}

	if !hitAxis {
		// Origin started inside or on the entry boundary of the box.
		hitNormal = dir.Neg()
	}

	return RaycastHit{
		ChildIndex: -1,
		T:          clamp32(tMin/ray.MaxDist, 0, 1),
		Distance:   tMin,
		Point:      ray.Origin.Add(dir.Scale(tMin)),
		Normal:     hitNormal,
	}, true
}

// RaycastSphere tests ray against a sphere s.
func RaycastSphere(ray Ray, s Sphere) (RaycastHit, bool) {
	if ray.MaxDist <= 0 || s.Radius <= 0 {
		return RaycastHit{}, false
	}
	dir := ray.Dir.Norm()
	if dir.LenSq() == 0 {
		return RaycastHit{}, false
	}

	m := ray.Origin.Sub(s.Center)
	b := m.Dot(dir)
	c := m.Dot(m) - float32(s.Radius*s.Radius)
	if c <= 0 {
		// Origin is inside or on the sphere surface: report it like RaycastBox does, facing the ray (#546).
		return RaycastHit{
			ChildIndex: -1,
			T:          0,
			Distance:   0,
			Point:      ray.Origin,
			Normal:     dir.Neg(),
		}, true
	}
	if b > 0 {
		return RaycastHit{}, false
	}
	discr := float32(b*b) - c
	if discr < 0 {
		return RaycastHit{}, false
	}
	dist := -b - sqrt32(discr)
	if dist < 0 {
		dist = 0
	}
	if dist > ray.MaxDist {
		return RaycastHit{}, false
	}
	point := ray.Origin.Add(dir.Scale(dist))
	normal := point.Sub(s.Center).Norm()
	if normal.LenSq() == 0 {
		normal = dir.Neg()
	}
	return RaycastHit{
		ChildIndex: -1,
		T:          clamp32(dist/ray.MaxDist, 0, 1),
		Distance:   dist,
		Point:      point,
		Normal:     normal,
	}, true
}

// RaycastCapsule tests ray against a capsule c.
func RaycastCapsule(ray Ray, c Capsule) (RaycastHit, bool) {
	if ray.MaxDist <= 0 || c.Radius <= 0 {
		return RaycastHit{}, false
	}
	dir := ray.Dir.Norm()
	if dir.LenSq() == 0 {
		return RaycastHit{}, false
	}

	ab := c.B.Sub(c.A)
	abLenSq := ab.LenSq()
	if abLenSq <= 1e-12 {
		return RaycastSphere(ray, Sphere{Center: c.A, Radius: c.Radius})
	}

	// Check if ray origin starts inside the capsule.
	closest := closestPointOnSegment(c.A, c.B, ray.Origin)
	toOrigin := ray.Origin.Sub(closest)
	if toOrigin.LenSq() <= c.Radius*c.Radius {
		// Inside: report like RaycastBox and RaycastSphere, facing the ray (#546).
		return RaycastHit{
			ChildIndex: -1,
			T:          0,
			Distance:   0,
			Point:      ray.Origin,
			Normal:     dir.Neg(),
		}, true
	}

	abLen := sqrt32(abLenSq)
	abNorm := ab.Scale(1 / abLen)

	var best RaycastHit
	hit := false
	bestDist := ray.MaxDist

	// 1. Finite cylinder wall between A and B.
	ao := ray.Origin.Sub(c.A)
	dPerp := dir.Sub(abNorm.Scale(dir.Dot(abNorm)))
	aoPerp := ao.Sub(abNorm.Scale(ao.Dot(abNorm)))
	qa := dPerp.LenSq()
	qb := aoPerp.Dot(dPerp)
	qc := aoPerp.LenSq() - float32(c.Radius*c.Radius)
	if qa > 1e-12 {
		discr := float32(qb*qb) - float32(qa*qc)
		if discr >= 0 {
			dist := (-qb - sqrt32(discr)) / qa
			if dist >= 0 && dist <= bestDist {
				point := ray.Origin.Add(dir.Scale(dist))
				h := point.Sub(c.A).Dot(abNorm)
				if h >= 0 && h <= abLen {
					axisPt := c.A.Add(abNorm.Scale(h))
					normal := point.Sub(axisPt).Norm()
					best = RaycastHit{
						ChildIndex: -1,
						T:          clamp32(dist/ray.MaxDist, 0, 1),
						Distance:   dist,
						Point:      point,
						Normal:     normal,
					}
					bestDist = dist
					hit = true
				}
			}
		}
	}

	// 2. End-cap spheres at A and B. Since the ray starts outside the capsule,
	// the earliest entry into cylinder U sphere(A) U sphere(B) is exact.
	if hA, ok := RaycastSphere(ray, Sphere{Center: c.A, Radius: c.Radius}); ok && (!hit || hA.Distance < bestDist) {
		best = hA
		bestDist = hA.Distance
		hit = true
	}
	if hB, ok := RaycastSphere(ray, Sphere{Center: c.B, Radius: c.Radius}); ok && (!hit || hB.Distance < bestDist) {
		best = hB
		bestDist = hB.Distance
		hit = true
	}

	return best, hit
}
