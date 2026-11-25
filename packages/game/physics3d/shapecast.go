package physics3d

import (
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/internal/detmath"
)

// ShapeCastOptions configures filtering for World.ShapeCastWith and World.ShapeCastAllWith.
type ShapeCastOptions struct {
	// LayerMask filters bodies and static colliders by their Layer bitmask.
	// Zero defaults to LayerAll.
	LayerMask LayerMask
	// IgnoreBody optionally excludes one body from the cast.
	IgnoreBody *Body
	// IgnoreBodyID optionally excludes the body with this ID (matching CharacterController.IgnoreBodyID).
	IgnoreBodyID int
	// IgnoreTriggers excludes bodies with IsTrigger == true when set.
	IgnoreTriggers bool
}

// ShapeCast sweeps shape placed at (origin, rot) along displacement delta against all bodies,
// triangle meshes, heightfields, and voxel colliders in w, returning the earliest hit.
// Ties break deterministically in the same order as RaycastFirst (Body, TriMesh, Heightfield,
// Voxel, then ascending ID).
func (w *World) ShapeCast(shape Shape, origin Vec3, rot Quat, delta Vec3) (RaycastHit, bool) {
	return w.ShapeCastLayer(shape, origin, rot, delta, LayerAll, nil)
}

// ShapeCastAll sweeps shape placed at (origin, rot) along displacement delta against all bodies,
// triangle meshes, heightfields, and voxel colliders in w, returning all hits (at most one
// earliest hit per body or collider) sorted by ascending T with deterministic tie-breaking.
func (w *World) ShapeCastAll(shape Shape, origin Vec3, rot Quat, delta Vec3) []RaycastHit {
	return w.ShapeCastAllLayer(shape, origin, rot, delta, LayerAll, nil)
}

// ShapeCastLayer sweeps shape placed at (origin, rot) along displacement delta against bodies
// and static colliders whose Layer matches mask, optionally ignoring ignore, and returns the
// earliest hit. Passing mask == 0 matches no colliders (matching RaycastFirstLayer).
func (w *World) ShapeCastLayer(shape Shape, origin Vec3, rot Quat, delta Vec3, mask LayerMask, ignore *Body) (RaycastHit, bool) {
	if w == nil || mask == 0 {
		return RaycastHit{}, false
	}
	return w.shapeCastFirst(shape, origin, rot, delta, mask, ignore, 0, false)
}

// ShapeCastAllLayer sweeps shape placed at (origin, rot) along displacement delta against bodies
// and static colliders whose Layer matches mask, optionally ignoring ignore, and returns all
// hits sorted by ascending T with deterministic tie-breaking. Passing mask == 0 returns nil.
func (w *World) ShapeCastAllLayer(shape Shape, origin Vec3, rot Quat, delta Vec3, mask LayerMask, ignore *Body) []RaycastHit {
	if w == nil || mask == 0 {
		return nil
	}
	return w.shapeCastAll(shape, origin, rot, delta, mask, ignore, 0, false)
}

// ShapeCastWith sweeps shape placed at (origin, rot) along displacement delta with filtering
// configured by opts, returning the earliest hit. A zero opts.LayerMask defaults to LayerAll.
func (w *World) ShapeCastWith(shape Shape, origin Vec3, rot Quat, delta Vec3, opts ShapeCastOptions) (RaycastHit, bool) {
	if w == nil {
		return RaycastHit{}, false
	}
	return w.shapeCastFirst(shape, origin, rot, delta, effectiveLayer(opts.LayerMask), opts.IgnoreBody, opts.IgnoreBodyID, opts.IgnoreTriggers)
}

// ShapeCastAllWith sweeps shape placed at (origin, rot) along displacement delta with filtering
// configured by opts, returning all hits sorted by ascending T. A zero opts.LayerMask defaults to LayerAll.
func (w *World) ShapeCastAllWith(shape Shape, origin Vec3, rot Quat, delta Vec3, opts ShapeCastOptions) []RaycastHit {
	if w == nil {
		return nil
	}
	return w.shapeCastAll(shape, origin, rot, delta, effectiveLayer(opts.LayerMask), opts.IgnoreBody, opts.IgnoreBodyID, opts.IgnoreTriggers)
}

// ShapeCastShape sweeps moving along displacement delta against stationary target and returns
// the exact time of impact, contact point, and outward unit normal pointing from target toward
// moving. Initial overlaps report T == 0 and Distance == 0 with the overlap contact from TestShapes.
func ShapeCastShape(moving Shape, delta Vec3, target Shape) (RaycastHit, bool) {
	if !shapeValid(moving) || !shapeValid(target) || !isFiniteVec(delta) {
		return RaycastHit{}, false
	}
	return sweepShapeVsShape3D(moving, delta, target)
}

func shapeValid(s Shape) bool {
	switch s.Kind {
	case ShapeSphere:
		return isFiniteVec(s.Sphere.Center) && isFinite32(s.Sphere.Radius) && s.Sphere.Radius > 0
	case ShapeBox:
		return isFiniteVec(s.Box.Center) && isFiniteVec(s.Box.HalfExtents) &&
			s.Box.HalfExtents.X > 0 && s.Box.HalfExtents.Y > 0 && s.Box.HalfExtents.Z > 0 &&
			isFiniteQuat(s.Rot)
	case ShapeCapsule:
		return isFiniteVec(s.Capsule.A) && isFiniteVec(s.Capsule.B) && isFinite32(s.Capsule.Radius) && s.Capsule.Radius > 0
	default:
		return false
	}
}

func (w *World) shapeCastFirst(shape Shape, origin Vec3, rot Quat, delta Vec3, mask LayerMask, ignore *Body, ignoreID int, ignoreTriggers bool) (RaycastHit, bool) {
	if !isFiniteVec(origin) || !isFiniteVec(delta) || !isFiniteQuat(rot) {
		return RaycastHit{}, false
	}
	ws := shape.Transformed(origin, rot)
	if !shapeValid(ws) {
		return RaycastHit{}, false
	}
	b0 := ws.Bounds()
	sweptBounds := unionBounds(b0, b0.Translated(delta))

	var best RaycastHit
	found := false
	consider := func(h RaycastHit) {
		if !found || compareRaycastHits(h, best) < 0 {
			best = h
			found = true
		}
	}

	for _, b := range w.bodies {
		if b == nil || b == ignore || (ignoreID != 0 && b.ID == ignoreID) {
			continue
		}
		if ignoreTriggers && b.IsTrigger {
			continue
		}
		if (effectiveLayer(b.Layer) & mask) == 0 {
			continue
		}
		target := b.WorldShape()
		if !shapeValid(target) || !aabbOverlap(sweptBounds, target.Bounds()) {
			continue
		}
		if h, ok := sweepShapeVsShape3D(ws, delta, target); ok {
			h.Body = b
			consider(h)
		}
	}

	for _, tm := range w.triMeshes {
		if tm == nil || (effectiveLayer(tm.Layer)&mask) == 0 {
			continue
		}
		if !aabbOverlap(sweptBounds, tm.Bounds) {
			continue
		}
		if h, ok := tm.ShapeCast(ws, delta); ok {
			h.TriMesh = tm
			consider(h)
		}
	}

	for _, hf := range w.heightfields {
		if hf == nil || (effectiveLayer(hf.Layer)&mask) == 0 {
			continue
		}
		if !aabbOverlap(sweptBounds, hf.Bounds) {
			continue
		}
		if h, ok := hf.ShapeCast(ws, delta); ok {
			h.Heightfield = hf
			consider(h)
		}
	}

	for _, vc := range w.voxels {
		if vc == nil || (effectiveLayer(vc.Layer)&mask) == 0 {
			continue
		}
		if h, ok := vc.shapeCastHit(ws, delta); ok {
			consider(h)
		}
	}

	return best, found
}

func (w *World) shapeCastAll(shape Shape, origin Vec3, rot Quat, delta Vec3, mask LayerMask, ignore *Body, ignoreID int, ignoreTriggers bool) []RaycastHit {
	if !isFiniteVec(origin) || !isFiniteVec(delta) || !isFiniteQuat(rot) {
		return nil
	}
	ws := shape.Transformed(origin, rot)
	if !shapeValid(ws) {
		return nil
	}
	b0 := ws.Bounds()
	sweptBounds := unionBounds(b0, b0.Translated(delta))

	var hits []RaycastHit
	for _, b := range w.bodies {
		if b == nil || b == ignore || (ignoreID != 0 && b.ID == ignoreID) {
			continue
		}
		if ignoreTriggers && b.IsTrigger {
			continue
		}
		if (effectiveLayer(b.Layer) & mask) == 0 {
			continue
		}
		target := b.WorldShape()
		if !shapeValid(target) || !aabbOverlap(sweptBounds, target.Bounds()) {
			continue
		}
		if h, ok := sweepShapeVsShape3D(ws, delta, target); ok {
			h.Body = b
			hits = append(hits, h)
		}
	}

	for _, tm := range w.triMeshes {
		if tm == nil || (effectiveLayer(tm.Layer)&mask) == 0 {
			continue
		}
		if !aabbOverlap(sweptBounds, tm.Bounds) {
			continue
		}
		if h, ok := tm.ShapeCast(ws, delta); ok {
			h.TriMesh = tm
			hits = append(hits, h)
		}
	}

	for _, hf := range w.heightfields {
		if hf == nil || (effectiveLayer(hf.Layer)&mask) == 0 {
			continue
		}
		if !aabbOverlap(sweptBounds, hf.Bounds) {
			continue
		}
		if h, ok := hf.ShapeCast(ws, delta); ok {
			h.Heightfield = hf
			hits = append(hits, h)
		}
	}

	for _, vc := range w.voxels {
		if vc == nil || (effectiveLayer(vc.Layer)&mask) == 0 {
			continue
		}
		if h, ok := vc.shapeCastHit(ws, delta); ok {
			hits = append(hits, h)
		}
	}

	slices.SortStableFunc(hits, compareRaycastHits)
	return hits
}

func (vc *VoxelCollider) shapeCastHit(shape Shape, delta Vec3) (RaycastHit, bool) {
	if !vc.valid() || !shapeValid(shape) || !isFiniteVec(delta) {
		return RaycastHit{}, false
	}
	// Report any strict initial penetration at T == 0 (consistent with body/mesh casts).
	if c := vc.TestShape(shape); c.Hit {
		return RaycastHit{
			Voxel:    vc,
			T:        0,
			Distance: 0,
			Point:    c.Point,
			Normal:   c.Normal,
		}, true
	}
	if delta.LenSq() <= 1e-14 {
		return RaycastHit{}, false
	}
	sh, ok := vc.Sweep(shape, delta)
	if !ok {
		return RaycastHit{}, false
	}
	return RaycastHit{
		Voxel:    vc,
		T:        sh.T,
		Distance: sh.Distance,
		Point:    sh.Point,
		Normal:   sh.Normal,
	}, true
}

// ShapeCast sweeps shape along displacement delta against tm using its BVH and returns
// the earliest hit.
func (tm *TriMeshCollider) ShapeCast(shape Shape, delta Vec3) (RaycastHit, bool) {
	if tm == nil || len(tm.nodes) == 0 || !shapeValid(shape) || !isFiniteVec(delta) {
		return RaycastHit{}, false
	}
	b0 := shape.Bounds()
	swept := unionBounds(b0, b0.Translated(delta))
	if !aabbOverlap(swept, tm.nodes[0].bounds) {
		return RaycastHit{}, false
	}

	dist := delta.Len()
	dir := Vec3{}
	if dist > 0 {
		dir = delta.Scale(1 / dist)
	}
	he := b0.HalfExtents

	nodeOverlapT := func(nodeBounds Box, maxDist float32) (float32, bool) {
		expanded := Box{Center: nodeBounds.Center, HalfExtents: nodeBounds.HalfExtents.Add(he)}
		if dist == 0 {
			if pointInsideBox(b0.Center, expanded.Min(), expanded.Max()) {
				return 0, true
			}
			return 0, false
		}
		tMin, _, hit := rayIntersectsBoxDist(b0.Center, dir, maxDist, expanded)
		return tMin, hit
	}

	var buf [64]int
	stack := append(buf[:0], 0)

	var best RaycastHit
	bestDist := dist
	bestTriIdx := -1
	found := false

	for len(stack) > 0 {
		node := &tm.nodes[stack[len(stack)-1]]
		stack = stack[:len(stack)-1]
		if node.triCount > 0 {
			for i := 0; i < node.triCount; i++ {
				triIdx := tm.triOrder[node.firstTri+i]
				tri := tm.Triangles[triIdx]
				if !aabbOverlap(swept, tri.Bounds()) {
					continue
				}
				if h, ok := sweepShapeVsTriangle3D(shape, delta, tri); ok {
					if !found || h.T < best.T || (h.T == best.T && triIdx < bestTriIdx) {
						best = h
						bestDist = h.Distance
						bestTriIdx = triIdx
						found = true
					}
				}
			}
			continue
		}

		tL, hitL := nodeOverlapT(tm.nodes[node.left].bounds, bestDist)
		tR, hitR := nodeOverlapT(tm.nodes[node.right].bounds, bestDist)

		if hitL && hitR {
			if tL <= tR {
				stack = append(stack, node.right, node.left)
			} else {
				stack = append(stack, node.left, node.right)
			}
		} else if hitL {
			stack = append(stack, node.left)
		} else if hitR {
			stack = append(stack, node.right)
		}
	}

	return best, found
}

// ShapeCast sweeps shape along displacement delta against hf and returns the earliest hit.
func (hf *HeightfieldCollider) ShapeCast(shape Shape, delta Vec3) (RaycastHit, bool) {
	if !hf.valid() || !shapeValid(shape) || !isFiniteVec(delta) {
		return RaycastHit{}, false
	}
	b0 := shape.Bounds()
	swept := unionBounds(b0, b0.Translated(delta))
	if !aabbOverlap(swept, hf.Bounds) {
		return RaycastHit{}, false
	}

	var best RaycastHit
	found := false

	hf.QueryTriangles(swept, func(tri Triangle) {
		if !aabbOverlap(swept, tri.Bounds()) {
			return
		}
		if h, ok := sweepShapeVsHeightfieldTriangle3D(shape, delta, tri); ok {
			if !found || h.T < best.T {
				best = h
				found = true
			}
		}
	})

	return best, found
}

// --- 3D narrowphase shape sweeps ---

func (v vec3d) neg() vec3d { return vec3d{-v.x, -v.y, -v.z} }
func (v vec3d) norm() vec3d {
	l := v.len()
	if l == 0 {
		return vec3d{}
	}
	return v.scale(1.0 / l)
}

func sweepShapeVsShape3D(moving Shape, delta Vec3, target Shape) (RaycastHit, bool) {
	// Initial overlap check at T == 0.
	if c := TestShapes(moving, target); c.Hit {
		return RaycastHit{
			T:        0,
			Distance: 0,
			Point:    c.Point,
			Normal:   c.Normal,
		}, true
	}
	dist := delta.Len()
	if dist <= 1e-12 {
		return RaycastHit{}, false
	}

	rotM, orientM := moving.orientedBox()
	rotT, orientT := target.orientedBox()

	switch moving.Kind {
	case ShapeSphere:
		switch target.Kind {
		case ShapeSphere:
			return sweepSphereVsSphere3D(moving.Sphere, delta, dist, target.Sphere)
		case ShapeBox:
			if orientT {
				f := newBoxFrame(target.Box.Center, rotT)
				localSphere := Sphere{Center: f.toLocal(moving.Sphere.Center), Radius: moving.Sphere.Radius}
				localDelta := f.inv.RotateVec(delta)
				h, ok := sweepSphereVsAABB3D(localSphere, localDelta, dist, Box{HalfExtents: target.Box.HalfExtents})
				if ok {
					h.Point = f.toWorld(h.Point)
					h.Normal = f.dirToWorld(h.Normal).Norm()
				}
				return h, ok
			}
			return sweepSphereVsAABB3D(moving.Sphere, delta, dist, target.Box)
		case ShapeCapsule:
			return sweepSphereVsCapsule3D(moving.Sphere, delta, dist, target.Capsule)
		}

	case ShapeBox:
		switch target.Kind {
		case ShapeSphere:
			// Symmetric to sweeping target sphere by -delta against moving box.
			h, ok := sweepShapeVsShape3D(target, delta.Neg(), moving)
			if !ok {
				return RaycastHit{}, false
			}
			n := h.Normal.Neg()
			return RaycastHit{
				T:        h.T,
				Distance: h.Distance,
				Point:    target.Sphere.Center.Add(n.Scale(target.Sphere.Radius)),
				Normal:   n,
			}, true
		case ShapeBox:
			if !orientM && !orientT {
				return sweepAABBVsAABB3D(moving.Box, delta, dist, target.Box)
			}
			return sweepOBBVsOBB3D(moving.Box, rotM, delta, dist, target.Box, rotT)
		case ShapeCapsule:
			// Symmetric to sweeping target capsule by -delta against moving box.
			h, ok := sweepCapsuleVsBox3D(target.Capsule, delta.Neg(), dist, moving.Box, rotM, orientM)
			if !ok {
				return RaycastHit{}, false
			}
			n := h.Normal.Neg()
			// At T, moving box shifted by +T*delta touches target capsule;
			// h.Point was on the static box frame, so adding T*delta maps it onto target capsule.
			return RaycastHit{
				T:        h.T,
				Distance: h.Distance,
				Point:    h.Point.Add(delta.Scale(h.T)),
				Normal:   n,
			}, true
		}

	case ShapeCapsule:
		switch target.Kind {
		case ShapeSphere:
			h, ok := sweepSphereVsCapsule3D(target.Sphere, delta.Neg(), dist, moving.Capsule)
			if !ok {
				return RaycastHit{}, false
			}
			n := h.Normal.Neg()
			return RaycastHit{
				T:        h.T,
				Distance: h.Distance,
				Point:    target.Sphere.Center.Add(n.Scale(target.Sphere.Radius)),
				Normal:   n,
			}, true
		case ShapeBox:
			return sweepCapsuleVsBox3D(moving.Capsule, delta, dist, target.Box, rotT, orientT)
		case ShapeCapsule:
			return sweepCapsuleVsCapsule3D(moving.Capsule, delta, dist, target.Capsule)
		}
	}

	return RaycastHit{}, false
}

// sweepPointVsSphere3D sweeps point p by d against exterior of sphere (c, r).
// Grazing tangents (disc <= 0) do not hit.
func sweepPointVsSphere3D(p, d, c vec3d, r float64) (t float64, normal vec3d, ok bool) {
	o := p.sub(c)
	qa := d.lenSq()
	if qa == 0 {
		return 0, vec3d{}, false
	}
	qb := o.dot(d)
	if qb >= 0 {
		return 0, vec3d{}, false
	}
	qc := o.lenSq() - float64(r*r)
	if qc <= 0 {
		return 0, o.norm(), true
	}
	disc := float64(qb*qb) - float64(qa*qc)
	if disc <= 0 {
		return 0, vec3d{}, false
	}
	t = (-qb - math.Sqrt(disc)) / qa
	if t < 0 || t > 1 {
		return 0, vec3d{}, false
	}
	n := o.add(d.scale(t)).norm()
	return t, n, true
}

func sweepSphereVsSphere3D(s Sphere, delta Vec3, dist float32, target Sphere) (RaycastHit, bool) {
	rSum := float64(s.Radius) + float64(target.Radius)
	t, n, ok := sweepPointVsSphere3D(toVec3d(s.Center), toVec3d(delta), toVec3d(target.Center), rSum)
	if !ok {
		return RaycastHit{}, false
	}
	nf := n.toVec3()
	return RaycastHit{
		T:        float32(t),
		Distance: float32(t) * dist,
		Point:    target.Center.Add(nf.Scale(target.Radius)),
		Normal:   nf,
	}, true
}

func sweepPointVsSegmentCylinder3D(p, d, segA, segB vec3d, r float64) (t float64, hitPt, normal vec3d, ok bool) {
	ab := segB.sub(segA)
	abLenSq := ab.lenSq()
	if abLenSq <= 1e-24 {
		return 0, vec3d{}, vec3d{}, false
	}
	abLen := math.Sqrt(abLenSq)
	abDir := ab.scale(1.0 / abLen)

	ao := p.sub(segA)
	dPerp := d.sub(abDir.scale(d.dot(abDir)))
	aoPerp := ao.sub(abDir.scale(ao.dot(abDir)))

	qa := dPerp.lenSq()
	if qa <= 1e-24 {
		return 0, vec3d{}, vec3d{}, false
	}
	qb := aoPerp.dot(dPerp)
	if qb >= 0 {
		return 0, vec3d{}, vec3d{}, false
	}
	qc := aoPerp.lenSq() - float64(r*r)
	var hitT float64
	if qc <= 0 {
		hitT = 0
	} else {
		disc := float64(qb*qb) - float64(qa*qc)
		if disc <= 0 {
			return 0, vec3d{}, vec3d{}, false
		}
		hitT = (-qb - math.Sqrt(disc)) / qa
		if hitT < 0 || hitT > 1 {
			return 0, vec3d{}, vec3d{}, false
		}
	}
	ptAtT := p.add(d.scale(hitT))
	h := ptAtT.sub(segA).dot(abDir)
	if h < 0 || h > abLen {
		return 0, vec3d{}, vec3d{}, false
	}
	axisPt := segA.add(abDir.scale(h))
	n := ptAtT.sub(axisPt).norm()
	return hitT, axisPt, n, true
}

func sweepSphereVsCapsule3D(s Sphere, delta Vec3, dist float32, cap Capsule) (RaycastHit, bool) {
	p := toVec3d(s.Center)
	d := toVec3d(delta)
	cA := toVec3d(cap.A)
	cB := toVec3d(cap.B)
	rSum := float64(s.Radius) + float64(cap.Radius)

	bestT := 2.0
	var bestAxisPt, bestN vec3d
	found := false

	consider := func(t float64, axisPt, n vec3d) {
		if t < 0 || t > 1 {
			return
		}
		if !found || t < bestT {
			bestT = t
			bestAxisPt = axisPt
			bestN = n
			found = true
		}
	}

	if t, axisPt, n, ok := sweepPointVsSegmentCylinder3D(p, d, cA, cB, rSum); ok {
		consider(t, axisPt, n)
	}
	if t, n, ok := sweepPointVsSphere3D(p, d, cA, rSum); ok {
		consider(t, cA, n)
	}
	if t, n, ok := sweepPointVsSphere3D(p, d, cB, rSum); ok {
		consider(t, cB, n)
	}

	if !found {
		return RaycastHit{}, false
	}
	nf := bestN.toVec3()
	return RaycastHit{
		T:        float32(bestT),
		Distance: float32(bestT) * dist,
		Point:    bestAxisPt.toVec3().Add(nf.Scale(cap.Radius)),
		Normal:   nf,
	}, true
}

// sweepSphereVsAABB3D computes the exact analytical TOI of sphere s translating by delta
// against static axis-aligned box b by testing the exact Minkowski sum:
// 6 offset face rectangles + 12 edge cylinders + 8 corner spheres.
func sweepSphereVsAABB3D(s Sphere, delta Vec3, dist float32, b Box) (RaycastHit, bool) {
	p := toVec3d(s.Center)
	d := toVec3d(delta)
	r := float64(s.Radius)
	bMin := toVec3d(b.Min())
	bMax := toVec3d(b.Max())

	bestT := 2.0
	var bestPt, bestN vec3d
	found := false

	consider := func(t float64, pt, n vec3d) {
		if t < 0 || t > 1 {
			return
		}
		if !found || t < bestT {
			bestT = t
			bestPt = pt
			bestN = n
			found = true
		}
	}

	// 1. Six face rectangles shifted outward by r.
	if d.x > 0 {
		t := (bMin.x - r - p.x) / d.x
		if t >= 0 && t <= 1 {
			hy, hz := p.y+float64(d.y*t), p.z+float64(d.z*t)
			if hy >= bMin.y && hy <= bMax.y && hz >= bMin.z && hz <= bMax.z {
				consider(t, vec3d{bMin.x, hy, hz}, vec3d{-1, 0, 0})
			}
		}
	} else if d.x < 0 {
		t := (bMax.x + r - p.x) / d.x
		if t >= 0 && t <= 1 {
			hy, hz := p.y+float64(d.y*t), p.z+float64(d.z*t)
			if hy >= bMin.y && hy <= bMax.y && hz >= bMin.z && hz <= bMax.z {
				consider(t, vec3d{bMax.x, hy, hz}, vec3d{1, 0, 0})
			}
		}
	}

	if d.y > 0 {
		t := (bMin.y - r - p.y) / d.y
		if t >= 0 && t <= 1 {
			hx, hz := p.x+float64(d.x*t), p.z+float64(d.z*t)
			if hx >= bMin.x && hx <= bMax.x && hz >= bMin.z && hz <= bMax.z {
				consider(t, vec3d{hx, bMin.y, hz}, vec3d{0, -1, 0})
			}
		}
	} else if d.y < 0 {
		t := (bMax.y + r - p.y) / d.y
		if t >= 0 && t <= 1 {
			hx, hz := p.x+float64(d.x*t), p.z+float64(d.z*t)
			if hx >= bMin.x && hx <= bMax.x && hz >= bMin.z && hz <= bMax.z {
				consider(t, vec3d{hx, bMax.y, hz}, vec3d{0, 1, 0})
			}
		}
	}

	if d.z > 0 {
		t := (bMin.z - r - p.z) / d.z
		if t >= 0 && t <= 1 {
			hx, hy := p.x+float64(d.x*t), p.y+float64(d.y*t)
			if hx >= bMin.x && hx <= bMax.x && hy >= bMin.y && hy <= bMax.y {
				consider(t, vec3d{hx, hy, bMin.z}, vec3d{0, 0, -1})
			}
		}
	} else if d.z < 0 {
		t := (bMax.z + r - p.z) / d.z
		if t >= 0 && t <= 1 {
			hx, hy := p.x+float64(d.x*t), p.y+float64(d.y*t)
			if hx >= bMin.x && hx <= bMax.x && hy >= bMin.y && hy <= bMax.y {
				consider(t, vec3d{hx, hy, bMax.z}, vec3d{0, 0, 1})
			}
		}
	}

	// 2. Twelve edge cylinders (each reduces to a 2D disk sweep + axis range check).
	xs := [2]float64{bMin.x, bMax.x}
	ys := [2]float64{bMin.y, bMax.y}
	zs := [2]float64{bMin.z, bMax.z}

	// 4 X-aligned edges.
	for _, ey := range ys {
		for _, ez := range zs {
			if t, ny, nz, ok := sweepPointVsDisk2D3D(p.y, p.z, d.y, d.z, ey, ez, r); ok {
				hx := p.x + float64(d.x*t)
				if hx >= bMin.x && hx <= bMax.x {
					consider(t, vec3d{hx, ey, ez}, vec3d{0, ny, nz})
				}
			}
		}
	}
	// 4 Y-aligned edges.
	for _, ex := range xs {
		for _, ez := range zs {
			if t, nx, nz, ok := sweepPointVsDisk2D3D(p.x, p.z, d.x, d.z, ex, ez, r); ok {
				hy := p.y + float64(d.y*t)
				if hy >= bMin.y && hy <= bMax.y {
					consider(t, vec3d{ex, hy, ez}, vec3d{nx, 0, nz})
				}
			}
		}
	}
	// 4 Z-aligned edges.
	for _, ex := range xs {
		for _, ey := range ys {
			if t, nx, ny, ok := sweepPointVsDisk2D3D(p.x, p.y, d.x, d.y, ex, ey, r); ok {
				hz := p.z + float64(d.z*t)
				if hz >= bMin.z && hz <= bMax.z {
					consider(t, vec3d{ex, ey, hz}, vec3d{nx, ny, 0})
				}
			}
		}
	}

	// 3. Eight corner spheres.
	for _, cx := range xs {
		for _, cy := range ys {
			for _, cz := range zs {
				corner := vec3d{cx, cy, cz}
				if t, n, ok := sweepPointVsSphere3D(p, d, corner, r); ok {
					consider(t, corner, n)
				}
			}
		}
	}

	if !found {
		return RaycastHit{}, false
	}
	return RaycastHit{
		T:        float32(bestT),
		Distance: float32(bestT) * dist,
		Point:    bestPt.toVec3(),
		Normal:   bestN.toVec3(),
	}, true
}

func sweepPointVsDisk2D3D(px, py, dx, dy, cx, cy, r float64) (t, nx, ny float64, ok bool) {
	ox, oy := px-cx, py-cy
	qa := float64(dx*dx) + float64(dy*dy)
	if qa == 0 {
		return 0, 0, 0, false
	}
	qb := float64(ox*dx) + float64(oy*dy)
	if qb >= 0 {
		return 0, 0, 0, false
	}
	qc := float64(float64(ox*ox)+float64(oy*oy)) - float64(r*r)
	if qc <= 0 {
		if l := detmath.Hypot(ox, oy); l > 0 {
			nx, ny = ox/l, oy/l
		}
		return 0, nx, ny, true
	}
	perp := math.Abs(float64(ox*dy) - float64(oy*dx))
	rDist := float64(r * math.Sqrt(qa))
	if perp >= rDist {
		return 0, 0, 0, false
	}
	disc := float64(rDist-perp) * float64(rDist+perp)
	if disc <= 0 {
		return 0, 0, 0, false
	}
	t = (-qb - math.Sqrt(disc)) / qa
	if t < 0 || t > 1 {
		return 0, 0, 0, false
	}
	hx, hy := ox+float64(dx*t), oy+float64(dy*t)
	if l := detmath.Hypot(hx, hy); l > 0 {
		nx, ny = hx/l, hy/l
	}
	return t, nx, ny, true
}

func sweepAABBVsAABB3D(a Box, delta Vec3, dist float32, b Box) (RaycastHit, bool) {
	c := toVec3d(a.Center)
	d := toVec3d(delta)
	bMin := toVec3d(b.Min())
	bMax := toVec3d(b.Max())
	ae := toVec3d(a.HalfExtents)
	eMin := vec3d{bMin.x - ae.x, bMin.y - ae.y, bMin.z - ae.z}
	eMax := vec3d{bMax.x + ae.x, bMax.y + ae.y, bMax.z + ae.z}

	tEnter := math.Inf(-1)
	tExit := math.Inf(1)
	var hitNormal Vec3

	slab := func(orig, vel, lo, hi float64, negN, posN Vec3) bool {
		if math.Abs(vel) <= 1e-12 {
			return orig > lo && orig < hi
		}
		inv := 1.0 / vel
		t1 := (lo - orig) * inv
		t2 := (hi - orig) * inv
		n := negN
		if t1 > t2 {
			t1, t2 = t2, t1
			n = posN
		}
		if t1 > tEnter {
			tEnter = t1
			hitNormal = n
		}
		if t2 < tExit {
			tExit = t2
		}
		return tEnter < tExit
	}

	if !slab(c.x, d.x, eMin.x, eMax.x, Vec3{X: -1}, Vec3{X: 1}) ||
		!slab(c.y, d.y, eMin.y, eMax.y, Vec3{Y: -1}, Vec3{Y: 1}) ||
		!slab(c.z, d.z, eMin.z, eMax.z, Vec3{Z: -1}, Vec3{Z: 1}) {
		return RaycastHit{}, false
	}
	if tEnter >= tExit || tExit <= 0 || tEnter < 0 || tEnter > 1 || hitNormal.LenSq() == 0 {
		return RaycastHit{}, false
	}

	aAt := a.Translated(delta.Scale(float32(tEnter)))
	aMin, aMax := aAt.Min(), aAt.Max()
	bMinF, bMaxF := b.Min(), b.Max()
	pt := Vec3{
		X: (max(aMin.X, bMinF.X) + min(aMax.X, bMaxF.X)) * 0.5,
		Y: (max(aMin.Y, bMinF.Y) + min(aMax.Y, bMaxF.Y)) * 0.5,
		Z: (max(aMin.Z, bMinF.Z) + min(aMax.Z, bMaxF.Z)) * 0.5,
	}
	if hitNormal.X < 0 {
		pt.X = bMinF.X
	} else if hitNormal.X > 0 {
		pt.X = bMaxF.X
	} else if hitNormal.Y < 0 {
		pt.Y = bMinF.Y
	} else if hitNormal.Y > 0 {
		pt.Y = bMaxF.Y
	} else if hitNormal.Z < 0 {
		pt.Z = bMinF.Z
	} else if hitNormal.Z > 0 {
		pt.Z = bMaxF.Z
	}

	return RaycastHit{
		T:        float32(tEnter),
		Distance: float32(tEnter) * dist,
		Point:    pt,
		Normal:   hitNormal,
	}, true
}

// sweepOBBVsOBB3D computes the exact analytical TOI between oriented box a (translating by delta)
// and static oriented box b using the 15-axis moving Separating Axis Theorem.
func sweepOBBVsOBB3D(a Box, rotA Quat, delta Vec3, dist float32, b Box, rotB Quat) (RaycastHit, bool) {
	rotA, rotB = normalizedQuat(rotA), normalizedQuat(rotB)
	axF, bxF := boxAxes(rotA), boxAxes(rotB)
	ax := [3]vec3d{toVec3d(axF[0]), toVec3d(axF[1]), toVec3d(axF[2])}
	bx := [3]vec3d{toVec3d(bxF[0]), toVec3d(bxF[1]), toVec3d(bxF[2])}
	ea := toVec3d(a.HalfExtents)
	eb := toVec3d(b.HalfExtents)
	rel := toVec3d(a.Center).sub(toVec3d(b.Center))
	vel := toVec3d(delta)

	project := func(axes [3]vec3d, e vec3d, l vec3d) float64 {
		return float64(float64(e.x*math.Abs(axes[0].dot(l)))+float64(e.y*math.Abs(axes[1].dot(l)))) + float64(e.z*math.Abs(axes[2].dot(l)))
	}

	tEnter := math.Inf(-1)
	tExit := math.Inf(1)
	var hitNormal vec3d
	hitEdge := -1

	testAxis := func(l vec3d, edge int) bool {
		ls := l.lenSq()
		if ls <= 1e-18 {
			return true
		}
		l = l.scale(1.0 / math.Sqrt(ls))
		rSum := project(ax, ea, l) + project(bx, eb, l)
		d0 := rel.dot(l)
		dv := vel.dot(l)
		if math.Abs(dv) <= 1e-12 {
			return d0 > -rSum && d0 < rSum
		}
		inv := 1.0 / dv
		t1 := (-rSum - d0) * inv
		t2 := (rSum - d0) * inv
		n := l.neg()
		if t1 > t2 {
			t1, t2 = t2, t1
			n = l
		}
		if t1 > tEnter {
			tEnter = t1
			hitNormal = n
			hitEdge = edge
		}
		if t2 < tExit {
			tExit = t2
		}
		return tEnter < tExit
	}

	for i := range 3 {
		if !testAxis(ax[i], -1) {
			return RaycastHit{}, false
		}
	}
	for i := range 3 {
		if !testAxis(bx[i], -1) {
			return RaycastHit{}, false
		}
	}
	for i := range 3 {
		for j := range 3 {
			if !testAxis(ax[i].cross(bx[j]), i*3+j) {
				return RaycastHit{}, false
			}
		}
	}

	if tEnter >= tExit || tExit <= 0 || tEnter < 0 || tEnter > 1 || hitNormal.lenSq() == 0 {
		return RaycastHit{}, false
	}

	nf := hitNormal.toVec3()
	aCenterAt := a.Center.Add(delta.Scale(float32(tEnter)))
	var pt Vec3
	if hitEdge >= 0 {
		i, j := hitEdge/3, hitEdge%3
		pa0, pa1 := supportEdge(aCenterAt, axF, a.HalfExtents, i, nf.Neg())
		pb0, pb1 := supportEdge(b.Center, bxF, b.HalfExtents, j, nf)
		ca, cb := closestPointsSegments(pa0, pa1, pb0, pb1)
		pt = ca.Add(cb).Scale(0.5)
	} else if cPt, ok := cornersInside(aCenterAt, axF, a.HalfExtents, b.Center, bxF, b.HalfExtents); ok {
		pt = cPt
	} else {
		// Crossed faces with no corner inside the other box: project midpoint onto B's support plane.
		bSupport := b.Center.Add(nf.Scale(
			float32(float32(b.HalfExtents.X*abs32(bxF[0].Dot(nf)))+
				float32(b.HalfExtents.Y*abs32(bxF[1].Dot(nf)))) +
				float32(b.HalfExtents.Z*abs32(bxF[2].Dot(nf))),
		))
		mid := aCenterAt.Add(b.Center).Scale(0.5)
		pt = mid.Add(nf.Scale(bSupport.Sub(mid).Dot(nf)))
	}

	return RaycastHit{
		T:        float32(tEnter),
		Distance: float32(tEnter) * dist,
		Point:    pt,
		Normal:   nf,
	}, true
}

// conservativeAdvance performs exact-distance Conservative Advancement for a translating
// convex shape A(t) = A + t*delta against a stationary convex shape B.
// distanceFn returns (distance, closestPointOnB, unitNormalFromBToA) at parameter t.
func conservativeAdvance(delta Vec3, dist float32, distanceFn func(t float32) (d float32, pB Vec3, n Vec3)) (RaycastHit, bool) {
	const (
		maxIter = 64
		hitTol  = float32(1e-5)
	)
	t := float32(0)
	for range maxIter {
		d, pB, n := distanceFn(t)
		approach := -delta.Dot(n)
		if d <= hitTol {
			if approach <= 1e-6*dist {
				return RaycastHit{}, false
			}
			return RaycastHit{
				T:        clamp32(t, 0, 1),
				Distance: clamp32(t, 0, 1) * dist,
				Point:    pB,
				Normal:   n,
			}, true
		}
		if approach <= 1e-7*dist {
			return RaycastHit{}, false
		}
		dt := d / approach
		t += dt
		if t > 1 {
			return RaycastHit{}, false
		}
	}
	// If the loop terminated near a shallow grazing contact, check final distance.
	d, pB, n := distanceFn(t)
	if d <= 1e-4 && -delta.Dot(n) > 1e-6*dist && t <= 1 {
		return RaycastHit{
			T:        clamp32(t, 0, 1),
			Distance: clamp32(t, 0, 1) * dist,
			Point:    pB,
			Normal:   n,
		}, true
	}
	return RaycastHit{}, false
}

func sweepCapsuleVsCapsule3D(a Capsule, delta Vec3, dist float32, b Capsule) (RaycastHit, bool) {
	rSum := a.Radius + b.Radius
	return conservativeAdvance(delta, dist, func(t float32) (float32, Vec3, Vec3) {
		off := delta.Scale(t)
		pA, pB := closestPointsSegments(a.A.Add(off), a.B.Add(off), b.A, b.B)
		diff := pA.Sub(pB)
		l := diff.Len()
		if l <= 1e-7 {
			return -rSum, pB, delta.Neg().Norm()
		}
		n := diff.Scale(1 / l)
		return l - rSum, pB.Add(n.Scale(b.Radius)), n
	})
}

func sweepCapsuleVsBox3D(cap Capsule, delta Vec3, dist float32, box Box, rot Quat, oriented bool) (RaycastHit, bool) {
	if oriented {
		f := newBoxFrame(box.Center, rot)
		localCap := Capsule{A: f.toLocal(cap.A), B: f.toLocal(cap.B), Radius: cap.Radius}
		localDelta := f.inv.RotateVec(delta)
		h, ok := sweepCapsuleVsAABB3D(localCap, localDelta, dist, Box{HalfExtents: box.HalfExtents})
		if ok {
			h.Point = f.toWorld(h.Point)
			h.Normal = f.dirToWorld(h.Normal).Norm()
		}
		return h, ok
	}
	return sweepCapsuleVsAABB3D(cap, delta, dist, box)
}

func sweepCapsuleVsAABB3D(cap Capsule, delta Vec3, dist float32, box Box) (RaycastHit, bool) {
	bMin, bMax := box.Min(), box.Max()
	return conservativeAdvance(delta, dist, func(t float32) (float32, Vec3, Vec3) {
		off := delta.Scale(t)
		segA := cap.A.Add(off)
		segB := cap.B.Add(off)
		pSeg := closestOrDeepestPointSegmentBox(segA, segB, box)
		pBox := Vec3{
			X: clamp32(pSeg.X, bMin.X, bMax.X),
			Y: clamp32(pSeg.Y, bMin.Y, bMax.Y),
			Z: clamp32(pSeg.Z, bMin.Z, bMax.Z),
		}
		diff := pSeg.Sub(pBox)
		l := diff.Len()
		if l <= 1e-7 {
			return -cap.Radius, pBox, delta.Neg().Norm()
		}
		n := diff.Scale(1 / l)
		return l - cap.Radius, pBox, n
	})
}

// --- 3D shape vs triangle sweeps ---

func sweepShapeVsHeightfieldTriangle3D(s Shape, delta Vec3, tri Triangle) (RaycastHit, bool) {
	if c := testShapeHeightfieldTriangle(s, tri); c.Hit {
		return RaycastHit{
			T:        0,
			Distance: 0,
			Point:    c.Point,
			Normal:   c.Normal,
		}, true
	}
	dist := delta.Len()
	if dist <= 1e-12 {
		return RaycastHit{}, false
	}
	return sweepShapeVsTriangleDisjoint3D(s, delta, dist, tri)
}

func sweepShapeVsTriangle3D(s Shape, delta Vec3, tri Triangle) (RaycastHit, bool) {
	if c := TestShapeTriangle(s, tri); c.Hit {
		return RaycastHit{
			T:        0,
			Distance: 0,
			Point:    c.Point,
			Normal:   c.Normal,
		}, true
	}
	dist := delta.Len()
	if dist <= 1e-12 {
		return RaycastHit{}, false
	}
	return sweepShapeVsTriangleDisjoint3D(s, delta, dist, tri)
}

func sweepShapeVsTriangleDisjoint3D(s Shape, delta Vec3, dist float32, tri Triangle) (RaycastHit, bool) {
	switch s.Kind {
	case ShapeSphere:
		return sweepSphereVsTriangle3D(s.Sphere, delta, dist, tri)
	case ShapeBox:
		if r, ok := s.orientedBox(); ok {
			f := newBoxFrame(s.Box.Center, r)
			localTri := Triangle{A: f.toLocal(tri.A), B: f.toLocal(tri.B), C: f.toLocal(tri.C)}
			localDelta := f.inv.RotateVec(delta)
			h, ok := sweepAABBVsTriangle3D(Box{HalfExtents: s.Box.HalfExtents}, localDelta, dist, localTri)
			if ok {
				h.Point = f.toWorld(h.Point)
				h.Normal = f.dirToWorld(h.Normal).Norm()
			}
			return h, ok
		}
		return sweepAABBVsTriangle3D(s.Box, delta, dist, tri)
	case ShapeCapsule:
		return sweepCapsuleVsTriangle3D(s.Capsule, delta, dist, tri)
	default:
		return RaycastHit{}, false
	}
}

func sweepSphereVsTriangle3D(s Sphere, delta Vec3, dist float32, tri Triangle) (RaycastHit, bool) {
	p := toVec3d(s.Center)
	d := toVec3d(delta)
	r := float64(s.Radius)
	a, b, c := toVec3d(tri.A), toVec3d(tri.B), toVec3d(tri.C)
	e1 := b.sub(a)
	e2 := c.sub(a)
	rawN := e1.cross(e2)
	nLen := rawN.len()
	if nLen <= 1e-12 {
		return RaycastHit{}, false
	}
	triN := rawN.scale(1.0 / nLen)

	bestT := 2.0
	var bestPt, bestN vec3d
	found := false

	consider := func(t float64, pt, n vec3d) {
		if t < 0 || t > 1 {
			return
		}
		if !found || t < bestT {
			bestT = t
			bestPt = pt
			bestN = n
			found = true
		}
	}

	// 1. Offset triangle planes (+triN and -triN).
	dn := d.dot(triN)
	if math.Abs(dn) > 1e-12 {
		planeN := triN
		if dn > 0 {
			planeN = triN.neg()
			dn = -dn
		}
		// Now dn < 0 (approaching planeN face from its +planeN side).
		distToPlane := p.sub(a).dot(planeN)
		t := (r - distToPlane) / dn
		if t >= 0 && t <= 1 {
			hitOnTri := p.add(d.scale(t)).sub(planeN.scale(r))
			if pointInTrianglePlane3Dd(hitOnTri, a, b, c, triN) {
				consider(t, hitOnTri, planeN)
			}
		}
	}

	// 2. Three edge cylinders of radius r.
	edges := [3][2]vec3d{{a, b}, {b, c}, {c, a}}
	for _, edge := range edges {
		if t, pt, n, ok := sweepPointVsSegmentCylinder3D(p, d, edge[0], edge[1], r); ok {
			consider(t, pt, n)
		}
	}

	// 3. Three vertex spheres of radius r.
	verts := [3]vec3d{a, b, c}
	for _, v := range verts {
		if t, n, ok := sweepPointVsSphere3D(p, d, v, r); ok {
			consider(t, v, n)
		}
	}

	if !found {
		return RaycastHit{}, false
	}
	return RaycastHit{
		T:        float32(bestT),
		Distance: float32(bestT) * dist,
		Point:    bestPt.toVec3(),
		Normal:   bestN.toVec3(),
	}, true
}

func pointInTrianglePlane3Dd(p, a, b, c, triN vec3d) bool {
	c0 := b.sub(a).cross(p.sub(a)).dot(triN)
	c1 := c.sub(b).cross(p.sub(b)).dot(triN)
	c2 := a.sub(c).cross(p.sub(c)).dot(triN)
	const tol = -1e-6
	return c0 >= tol && c1 >= tol && c2 >= tol
}

// sweepAABBVsTriangle3D computes the exact analytical TOI between axis-aligned box b
// (translating by delta) and static triangle tri using 13-axis moving SAT.
func sweepAABBVsTriangle3D(b Box, delta Vec3, dist float32, tri Triangle) (RaycastHit, bool) {
	c := toVec3d(b.Center)
	e := toVec3d(b.HalfExtents)
	vel := toVec3d(delta)
	v0 := toVec3d(tri.A)
	v1 := toVec3d(tri.B)
	v2 := toVec3d(tri.C)

	e0 := v1.sub(v0)
	e1 := v2.sub(v1)
	e2 := v0.sub(v2)
	triN := e0.cross(v2.sub(v0))
	if triN.lenSq() <= 1e-18 {
		return RaycastHit{}, false
	}

	tEnter := math.Inf(-1)
	tExit := math.Inf(1)
	var hitNormal vec3d
	hitKind := 0 // 0=triN, 1=boxFace, 2=edgeCross
	hitBoxAxis := 0
	hitTriEdge := 0

	testAxis := func(l vec3d, kind, bAxis, tEdge int) bool {
		ls := l.lenSq()
		if ls <= 1e-18 {
			return true
		}
		l = l.scale(1.0 / math.Sqrt(ls))
		rBox := float64(float64(e.x*math.Abs(l.x))+float64(e.y*math.Abs(l.y))) + float64(e.z*math.Abs(l.z))
		cProj := c.dot(l)
		p0, p1, p2 := v0.dot(l), v1.dot(l), v2.dot(l)
		tMin := min(p0, min(p1, p2))
		tMax := max(p0, max(p1, p2))

		// Overlap condition: cProj + dv*t in [tMin - rBox, tMax + rBox].
		lo := tMin - rBox
		hi := tMax + rBox
		dv := vel.dot(l)
		if math.Abs(dv) <= 1e-12 {
			return cProj > lo && cProj < hi
		}
		inv := 1.0 / dv
		t1 := (lo - cProj) * inv
		t2 := (hi - cProj) * inv
		n := l.neg()
		if t1 > t2 {
			t1, t2 = t2, t1
			n = l
		}
		if t1 > tEnter {
			tEnter = t1
			hitNormal = n
			hitKind = kind
			hitBoxAxis = bAxis
			hitTriEdge = tEdge
		}
		if t2 < tExit {
			tExit = t2
		}
		return tEnter < tExit
	}

	if !testAxis(triN, 0, 0, 0) {
		return RaycastHit{}, false
	}
	if !testAxis(vec3d{1, 0, 0}, 1, 0, 0) ||
		!testAxis(vec3d{0, 1, 0}, 1, 1, 0) ||
		!testAxis(vec3d{0, 0, 1}, 1, 2, 0) {
		return RaycastHit{}, false
	}

	triEdges := [3]vec3d{e0, e1, e2}
	for ti, te := range triEdges {
		if !testAxis(vec3d{0, -te.z, te.y}, 2, 0, ti) ||
			!testAxis(vec3d{te.z, 0, -te.x}, 2, 1, ti) ||
			!testAxis(vec3d{-te.y, te.x, 0}, 2, 2, ti) {
			return RaycastHit{}, false
		}
	}

	if tEnter >= tExit || tExit <= 0 || tEnter < 0 || tEnter > 1 || hitNormal.lenSq() == 0 {
		return RaycastHit{}, false
	}

	nf := hitNormal.toVec3()
	bAt := b.Translated(delta.Scale(float32(tEnter)))
	var pt Vec3
	switch hitKind {
	case 2:
		ax := [3]Vec3{{X: 1}, {Y: 1}, {Z: 1}}
		pa0, pa1 := supportEdge(bAt.Center, ax, bAt.HalfExtents, hitBoxAxis, nf.Neg())
		edgePts := [3][2]Vec3{{tri.A, tri.B}, {tri.B, tri.C}, {tri.C, tri.A}}
		_, pb := closestPointsSegments(pa0, pa1, edgePts[hitTriEdge][0], edgePts[hitTriEdge][1])
		pt = pb
	default:
		// Compute support point of bAt in direction -nf and project onto triangle.
		var sx, sy, sz float32
		if nf.X > 1e-5 {
			sx = -bAt.HalfExtents.X
		} else if nf.X < -1e-5 {
			sx = bAt.HalfExtents.X
		}
		if nf.Y > 1e-5 {
			sy = -bAt.HalfExtents.Y
		} else if nf.Y < -1e-5 {
			sy = bAt.HalfExtents.Y
		}
		if nf.Z > 1e-5 {
			sz = -bAt.HalfExtents.Z
		} else if nf.Z < -1e-5 {
			sz = bAt.HalfExtents.Z
		}
		supportCorner := bAt.Center.Add(Vec3{X: sx, Y: sy, Z: sz})
		pt = closestPointOnTriangle(supportCorner, tri.A, tri.B, tri.C)
	}

	return RaycastHit{
		T:        float32(tEnter),
		Distance: float32(tEnter) * dist,
		Point:    pt,
		Normal:   nf,
	}, true
}

func sweepCapsuleVsTriangle3D(cap Capsule, delta Vec3, dist float32, tri Triangle) (RaycastHit, bool) {
	edges := [3][2]Vec3{{tri.A, tri.B}, {tri.B, tri.C}, {tri.C, tri.A}}
	return conservativeAdvance(delta, dist, func(t float32) (float32, Vec3, Vec3) {
		off := delta.Scale(t)
		segA := cap.A.Add(off)
		segB := cap.B.Add(off)

		// Closest points between segment [segA, segB] and triangle tri.
		qA := closestPointOnTriangle(segA, tri.A, tri.B, tri.C)
		bestSeg := segA
		bestTri := qA
		bestDistSq := segA.DistSq(qA)

		qB := closestPointOnTriangle(segB, tri.A, tri.B, tri.C)
		if ds := segB.DistSq(qB); ds < bestDistSq {
			bestDistSq = ds
			bestSeg = segB
			bestTri = qB
		}

		for _, edge := range edges {
			pS, pE := closestPointsSegments(segA, segB, edge[0], edge[1])
			if ds := pS.DistSq(pE); ds < bestDistSq {
				bestDistSq = ds
				bestSeg = pS
				bestTri = pE
			}
		}

		l := sqrt32(bestDistSq)
		if l <= 1e-7 {
			return -cap.Radius, bestTri, delta.Neg().Norm()
		}
		n := bestSeg.Sub(bestTri).Scale(1 / l)
		return l - cap.Radius, bestTri, n
	})
}
