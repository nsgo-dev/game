package physics3d

import "math"

// Continuous collision detection (#546). A dynamic body that moves more than half its smallest dimension in one step is swept in sub-steps of at most that size against static, kinematic and sleeping bodies, triangle meshes and heightfields. It stops at the first sub-step that overlaps one of them anew, or overlaps one it already touched at the start of the step clearly deeper than it did there, so the regular narrow phase resolves that contact instead of the body tunnelling through thin geometry. Contacts the body already has at the start, such as the ground under a body sliding or rolling fast along it, do not stop the sweep. Fast dynamic bodies are not swept against each other.

const maxSweepSteps = 32

// shapeMinExtent returns half the smallest dimension of s: the radius for spheres and capsules, the smallest half-extent for boxes, half the smallest width across a face for convex hulls, and the smaller of the radius and half the height for cylinders.
func shapeMinExtent(s Shape) float32 {
	switch s.Kind {
	case ShapeSphere:
		return s.Sphere.Radius
	case ShapeCapsule:
		return s.Capsule.Radius
	case ShapeBox:
		return min(s.Box.HalfExtents.X, s.Box.HalfExtents.Y, s.Box.HalfExtents.Z)
	case ShapeConvexHull:
		if s.Hull.geom == nil {
			return 0
		}
		return float32(s.Hull.geom.halfWidth)
	case ShapeCylinder:
		return min(s.Cylinder.Radius, s.Cylinder.Height()*0.5)
	default:
		return 0
	}
}

// sweepDepthSlop is how much deeper, as a fraction of the body's smallest extent, an overlap that already existed at the start of the step may get before the sweep treats it as blocking.
const sweepDepthSlop = 0.25

type sweepBodyCand struct {
	body   *Body
	shape  Shape
	bounds Box
}

// sweepFastBody moves b back to the first sub-step between from and b.Pos at which it overlaps a blocking collider it did not already overlap at from, or overlaps one it did clearly deeper.
func (w *World) sweepFastBody(b *Body, from Vec3, jointIgnored map[pairKey]struct{}) {
	ext := shapeMinExtent(b.Shape)
	if ext <= 0 {
		return
	}
	d := b.Pos.Sub(from)
	dist := d.Len()
	stepLen := ext * 0.5
	if dist <= stepLen {
		return
	}

	startShape := b.Shape.Transformed(from, b.Rot)
	endShape := b.Shape.Transformed(b.Pos, b.Rot)
	b0 := startShape.Bounds()
	b1 := endShape.Bounds()
	sweptBounds := BoxFromMinMax(
		Vec3{
			X: min(b0.Min().X, b1.Min().X),
			Y: min(b0.Min().Y, b1.Min().Y),
			Z: min(b0.Min().Z, b1.Min().Z),
		},
		Vec3{
			X: max(b0.Max().X, b1.Max().X),
			Y: max(b0.Max().Y, b1.Max().Y),
			Z: max(b0.Max().Z, b1.Max().Z),
		},
	)

	w.bp.ensureInit(w)
	var candBodies []sweepBodyCand
	var candTMs []*TriMeshCollider
	var candHFs []*HeightfieldCollider
	w.bp.queryAABB(sweptBounds, func(p *bpProxy) bool {
		switch p.kind {
		case bpKindBody:
			o := p.body
			if o == b || o.IsTrigger || !(o.Static || o.Kinematic || o.Sleeping) {
				return true
			}
			if !CanCollide(b, o) || w.isPairIgnored(b.ID, o.ID) {
				return true
			}
			if jointIgnored != nil {
				key, _ := canonicalPairKey(b.ID, o.ID)
				if _, ok := jointIgnored[key]; ok {
					return true
				}
			}
			if !aabbOverlap(sweptBounds, p.worldBounds) {
				return true
			}
			candBodies = append(candBodies, sweepBodyCand{body: o, shape: p.worldShape, bounds: p.worldBounds})
		case bpKindTriMesh:
			tm := p.triMesh
			if !canCollideMasks(b.Layer, b.Mask, tm.Layer, tm.Mask) || !aabbOverlap(sweptBounds, tm.Bounds) {
				return true
			}
			candTMs = append(candTMs, tm)
		case bpKindHeightfield:
			hf := p.heightfield
			if !canCollideMasks(b.Layer, b.Mask, hf.Layer, hf.Mask) || !aabbOverlap(sweptBounds, hf.Bounds) {
				return true
			}
			candHFs = append(candHFs, hf)
		}
		return true
	})

	var candVoxels []*VoxelCollider
	for _, vc := range w.voxels {
		if canCollideMasks(b.Layer, b.Mask, vc.Layer, vc.Mask) {
			candVoxels = append(candVoxels, vc)
		}
	}
	if len(candBodies) == 0 && len(candTMs) == 0 && len(candHFs) == 0 && len(candVoxels) == 0 {
		return
	}

	start := make(map[any]float32)
	sweepOverlapsCandidates(startShape, candBodies, candTMs, candHFs, candVoxels, func(key any, depth float32) bool {
		start[key] = depth
		return false
	})
	slop := ext * sweepDepthSlop
	k := min(int(math.Ceil(float64(dist/stepLen))), maxSweepSteps)
	for s := 1; s < k; s++ {
		p := from.Add(d.Scale(float32(s) / float32(k)))
		blocked := sweepOverlapsCandidates(b.Shape.Transformed(p, b.Rot), candBodies, candTMs, candHFs, candVoxels, func(key any, depth float32) bool {
			d0, ok := start[key]
			return !ok || depth > d0+slop
		})
		if blocked {
			b.Pos = p
			return
		}
	}
}

// sweepOverlapsCandidates calls visit for every candidate collider overlapped
// by shape, stopping and returning true as soon as visit returns true.
func sweepOverlapsCandidates(
	shape Shape,
	candBodies []sweepBodyCand,
	candTMs []*TriMeshCollider,
	candHFs []*HeightfieldCollider,
	candVoxels []*VoxelCollider,
	visit func(key any, depth float32) bool,
) bool {
	bounds := shape.Bounds()
	for i := range candBodies {
		cb := &candBodies[i]
		if !aabbOverlap(bounds, cb.bounds) {
			continue
		}
		if c := TestShapes(shape, cb.shape); c.Hit && visit(cb.body, c.Depth) {
			return true
		}
	}
	for _, tm := range candTMs {
		if !aabbOverlap(bounds, tm.Bounds) {
			continue
		}
		if c := tm.TestShape(shape); c.Hit && visit(tm, c.Depth) {
			return true
		}
	}
	for _, hf := range candHFs {
		if !aabbOverlap(bounds, hf.Bounds) {
			continue
		}
		if c := hf.TestShape(shape); c.Hit && visit(hf, c.Depth) {
			return true
		}
	}
	for _, vc := range candVoxels {
		if c := vc.TestShape(shape); c.Hit && visit(vc, c.Depth) {
			return true
		}
	}
	return false
}
