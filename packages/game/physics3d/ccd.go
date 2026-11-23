package physics3d

import "math"

// Continuous collision detection (#546). A dynamic body that moves more than half its smallest dimension in one step is swept in sub-steps of at most that size against static, kinematic and sleeping bodies, triangle meshes and heightfields. It stops at the first sub-step that overlaps one of them, so the regular narrow phase resolves that contact instead of the body tunnelling through thin geometry. Fast dynamic bodies are not swept against each other.

const maxSweepSteps = 32

// shapeMinExtent returns half the smallest dimension of s: the radius for spheres and capsules and the smallest half-extent for boxes.
func shapeMinExtent(s Shape) float32 {
	switch s.Kind {
	case ShapeSphere:
		return s.Sphere.Radius
	case ShapeCapsule:
		return s.Capsule.Radius
	case ShapeBox:
		return min(s.Box.HalfExtents.X, s.Box.HalfExtents.Y, s.Box.HalfExtents.Z)
	default:
		return 0
	}
}

// sweepFastBody moves b back to the first sub-step between from and b.Pos at which it overlaps a blocking collider.
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
	k := min(int(math.Ceil(float64(dist/stepLen))), maxSweepSteps)
	for s := 1; s < k; s++ {
		p := from.Add(d.Scale(float32(s) / float32(k)))
		if w.sweepBlocked(b, b.Shape.Transformed(p, b.Rot), jointIgnored) {
			b.Pos = p
			return
		}
	}
}

// sweepBlocked reports whether shape, standing in for b, overlaps a collider that does not move this step.
func (w *World) sweepBlocked(b *Body, shape Shape, jointIgnored map[pairKey]struct{}) bool {
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
		if aabbOverlap(bounds, os.Bounds()) && TestShapes(shape, os).Hit {
			return true
		}
	}
	for _, tm := range w.triMeshes {
		if canCollideMasks(b.Layer, b.Mask, tm.Layer, tm.Mask) && aabbOverlap(bounds, tm.Bounds) && tm.TestShape(shape).Hit {
			return true
		}
	}
	for _, hf := range w.heightfields {
		if canCollideMasks(b.Layer, b.Mask, hf.Layer, hf.Mask) && aabbOverlap(bounds, hf.Bounds) && hf.TestShape(shape).Hit {
			return true
		}
	}
	return false
}
