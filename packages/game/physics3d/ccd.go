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
	start := make(map[any]float32)
	w.sweepOverlaps(b, b.Shape.Transformed(from, b.Rot), jointIgnored, func(key any, depth float32) bool {
		start[key] = depth
		return false
	})
	slop := ext * sweepDepthSlop
	k := min(int(math.Ceil(float64(dist/stepLen))), maxSweepSteps)
	for s := 1; s < k; s++ {
		p := from.Add(d.Scale(float32(s) / float32(k)))
		blocked := w.sweepOverlaps(b, b.Shape.Transformed(p, b.Rot), jointIgnored, func(key any, depth float32) bool {
			d0, ok := start[key]
			return !ok || depth > d0+slop
		})
		if blocked {
			b.Pos = p
			return
		}
	}
}

// sweepOverlaps calls visit for every collider that does not move this step and that shape, standing in for b, overlaps, with a key identifying the collider and the overlap depth. It stops and returns true as soon as visit returns true.
func (w *World) sweepOverlaps(b *Body, shape Shape, jointIgnored map[pairKey]struct{}, visit func(key any, depth float32) bool) bool {
	bounds := shape.Bounds()
	for _, o := range w.bodies {
		if o == b || o.IsTrigger || !(o.Static || o.Kinematic || o.Sleeping) {
			continue
		}
		if !CanCollide(b, o) || w.isPairIgnored(b.ID, o.ID) {
			continue
		}
		if key, _ := canonicalPairKey(b.ID, o.ID); jointIgnored != nil {
			if _, ok := jointIgnored[key]; ok {
				continue
			}
		}
		os := o.WorldShape()
		if !aabbOverlap(bounds, os.Bounds()) {
			continue
		}
		if c := TestShapes(shape, os); c.Hit && visit(o, c.Depth) {
			return true
		}
	}
	for _, tm := range w.triMeshes {
		if !canCollideMasks(b.Layer, b.Mask, tm.Layer, tm.Mask) || !aabbOverlap(bounds, tm.Bounds) {
			continue
		}
		if c := tm.TestShape(shape); c.Hit && visit(tm, c.Depth) {
			return true
		}
	}
	for _, hf := range w.heightfields {
		if !canCollideMasks(b.Layer, b.Mask, hf.Layer, hf.Mask) || !aabbOverlap(bounds, hf.Bounds) {
			continue
		}
		if c := hf.TestShape(shape); c.Hit && visit(hf, c.Depth) {
			return true
		}
	}
	for _, vc := range w.voxels {
		if !canCollideMasks(b.Layer, b.Mask, vc.Layer, vc.Mask) {
			continue
		}
		if c := vc.TestShape(shape); c.Hit && visit(vc, c.Depth) {
			return true
		}
	}
	return false
}
