package world3d

import (
	"cmp"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
)

// GridColliderOptions configures static physics bodies created from a GridMap.
type GridColliderOptions struct {
	// DisableMerge emits one static body per cell ColliderBox instead of
	// greedily merging adjacent aligned boxes along X, then Z, then Y.
	DisableMerge bool
	// MergeTolerance sets the face-alignment tolerance for greedy box merging
	// (defaults to 1e-4 when <= 0).
	MergeTolerance float32
	// Friction is the Coulomb friction coefficient for created static bodies
	// (defaults to 0.5 when <= 0).
	Friction float32
	// Restitution is the bounciness in [0, 1] for created static bodies.
	Restitution float32
	// Layer is the collision layer bitmask (0 defaults to physics3d.LayerAll).
	Layer physics3d.LayerMask
	// Mask is the collision filter bitmask (0 defaults to physics3d.LayerAll).
	Mask physics3d.LayerMask
	// Tag is an optional label assigned to every created static body.
	Tag string
}

// ColliderBoxes returns all world-space collider AABBs from every occupied cell
// in g, in deterministic (Y, Z, X) cell order.
func (g *GridMap) ColliderBoxes() []math3d.AABB {
	if g == nil || g.count == 0 || g.Library == nil {
		return nil
	}
	coords := make([]GridCoord, 0, g.count)
	for _, ch := range g.chunks {
		for c := range ch.cells {
			coords = append(coords, c)
		}
	}
	slices.SortFunc(coords, func(a, b GridCoord) int {
		if c := cmp.Compare(a.Y, b.Y); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Z, b.Z); c != 0 {
			return c
		}
		return cmp.Compare(a.X, b.X)
	})

	var out []math3d.AABB
	for _, c := range coords {
		cell, ok := g.Cell(c)
		if !ok {
			continue
		}
		def, ok := g.Library.Get(cell.TileID)
		if !ok || len(def.ColliderBoxes) == 0 {
			continue
		}
		xform := g.CellTransform(c, cell.Orientation)
		for _, localBox := range def.ColliderBoxes {
			if localBox.Empty() {
				continue
			}
			worldBox := localBox.Transform(xform)
			if !worldBox.Empty() {
				out = append(out, worldBox)
			}
		}
	}
	return out
}

// MergedColliderBoxes extracts all world-space ColliderBoxes from g's occupied
// cells and greedily merges adjacent aligned boxes along X, then Z, then Y
// using physics3d.MergeBoxes. The union of the returned AABBs equals the union
// of g.ColliderBoxes() while reducing collider count and eliminating internal
// cell seams.
func (g *GridMap) MergedColliderBoxes() []math3d.AABB {
	return g.mergedColliderBoxes(0)
}

func (g *GridMap) mergedColliderBoxes(tol float32) []math3d.AABB {
	raw := g.ColliderBoxes()
	if len(raw) == 0 {
		return nil
	}
	pBoxes := make([]physics3d.Box, len(raw))
	for i, b := range raw {
		pBoxes[i] = physics3d.BoxFromMinMax(
			physics3d.Vec3{X: b.Min.X, Y: b.Min.Y, Z: b.Min.Z},
			physics3d.Vec3{X: b.Max.X, Y: b.Max.Y, Z: b.Max.Z},
		)
	}
	merged := physics3d.MergeBoxes(pBoxes, tol)
	out := make([]math3d.AABB, len(merged))
	for i, mb := range merged {
		mn, mx := mb.Min(), mb.Max()
		out[i] = math3d.AABB{
			Min: math3d.Vec3{X: mn.X, Y: mn.Y, Z: mn.Z},
			Max: math3d.Vec3{X: mx.X, Y: mx.Y, Z: mx.Z},
		}
	}
	return out
}

// BuildStaticBodies builds static physics3d.Body values for g's ColliderBoxes.
// Adjacent aligned boxes are greedily merged along X, then Z, then Y unless
// opts.DisableMerge is true.
func (g *GridMap) BuildStaticBodies(opts GridColliderOptions) []physics3d.Body {
	var boxes []math3d.AABB
	if opts.DisableMerge {
		boxes = g.ColliderBoxes()
	} else {
		boxes = g.mergedColliderBoxes(opts.MergeTolerance)
	}
	if len(boxes) == 0 {
		return nil
	}

	friction := opts.Friction
	if friction <= 0 {
		friction = 0.5
	}
	restitution := opts.Restitution
	if restitution < 0 {
		restitution = 0
	} else if restitution > 1 {
		restitution = 1
	}

	bodies := make([]physics3d.Body, len(boxes))
	for i, aabb := range boxes {
		center := aabb.Center()
		ext := aabb.HalfExtents()
		b := physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{
				HalfExtents: physics3d.Vec3{X: ext.X, Y: ext.Y, Z: ext.Z},
			}),
			physics3d.Vec3{X: center.X, Y: center.Y, Z: center.Z},
		)
		b.Friction = friction
		b.Restitution = restitution
		b.Layer = opts.Layer
		b.Mask = opts.Mask
		b.Tag = opts.Tag
		bodies[i] = b
	}
	return bodies
}

// AddCollidersToWorld creates static bodies in w for g's ColliderBoxes
// (greedily merged along X, then Z, then Y unless opts.DisableMerge is true)
// and returns the added body pointers in deterministic order.
func (g *GridMap) AddCollidersToWorld(w *physics3d.World, opts GridColliderOptions) []*physics3d.Body {
	if w == nil {
		return nil
	}
	bodies := g.BuildStaticBodies(opts)
	if len(bodies) == 0 {
		return nil
	}
	added := make([]*physics3d.Body, len(bodies))
	for i, b := range bodies {
		added[i] = w.AddBody(b)
	}
	return added
}

// Collider constructs a static physics3d.HeightfieldCollider matching h's grid
// dimensions, cell size, origin, and scaled vertex heights.
func (h *Heightmap) Collider() *physics3d.HeightfieldCollider {
	if !h.valid() {
		return physics3d.NewHeightfieldCollider(0, 0, 0, physics3d.Vec3{}, nil)
	}
	scaled := make([]float32, h.Cols*h.Rows)
	for i := range scaled {
		scaled[i] = h.Heights[i] * h.HeightScale
	}
	return physics3d.NewHeightfieldCollider(
		h.Cols,
		h.Rows,
		h.CellSize,
		physics3d.Vec3{X: h.Origin.X, Y: h.Origin.Y, Z: h.Origin.Z},
		scaled,
	)
}
