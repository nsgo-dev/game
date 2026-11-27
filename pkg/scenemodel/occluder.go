package scenemodel

import (
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// Occluder is one occluder attached to a Built 3D scene (Built.Occluders),
// produced for any scene object that sets sceneformat.ObjectDoc.Occluder.
type Occluder struct {
	// Node is the scene3d node the occluder follows.
	Node *scene3d.Node
	// Object is the ID of the scene object that declared the occluder.
	Object string
	// Mode is sceneformat.OccluderModel or sceneformat.OccluderBox.
	Mode sceneformat.OccluderMode
	// LocalBounds is the occluder's bounding box in Node's local space.
	LocalBounds math3d.AABB
	// DoubleSided reports whether both front and back faces occlude.
	DoubleSided bool

	// base holds precomputed local-space triangle geometry and coplanar edge
	// flags (from render3d.NewBoxOccluder or render3d.OccluderFromMesh).
	base render3d.Occluder
}

// RenderOccluder returns a render3d.Occluder positioned at o.Node's current
// world matrix without copying vertex or index slices.
func (o Occluder) RenderOccluder() render3d.Occluder {
	occ := o.base
	if o.Node != nil {
		world := o.Node.WorldMatrix()
		occ.Transform = world
		occ.Bounds = o.LocalBounds.Transform(world)
	}
	return occ
}

// buildObjectOccluders creates Occluder entries for o (and the drawables built
// for o starting at startDrawables) when o.Occluder is set.
func (b *builder) buildObjectOccluders(o *sceneformat.ObjectDoc, n *scene3d.Node, startDrawables int) {
	if o == nil || o.Occluder == nil {
		return
	}
	switch o.Occluder.Mode {
	case sceneformat.OccluderBox:
		lo, hi := o.Occluder.LocalBounds()
		aabb := math3d.AABB{
			Min: math3d.Vec3{X: lo[0], Y: lo[1], Z: lo[2]},
			Max: math3d.Vec3{X: hi[0], Y: hi[1], Z: hi[2]},
		}
		base := render3d.NewBoxOccluder(aabb, math3d.Mat4Identity())
		base.DoubleSided = o.Occluder.DoubleSided
		b.built.Occluders = append(b.built.Occluders, Occluder{
			Node:        n,
			Object:      o.ID,
			Mode:        sceneformat.OccluderBox,
			LocalBounds: aabb,
			DoubleSided: o.Occluder.DoubleSided,
			base:        base,
		})
	case sceneformat.OccluderModel:
		for _, d := range b.built.Drawables[startDrawables:] {
			for pi := range d.Parts {
				p := &d.Parts[pi]
				if p.Material.AlphaMode == render3d.AlphaBlend {
					continue
				}
				mesh := p.Mesh
				if len(p.LODs) > 0 {
					mesh = p.LODs[len(p.LODs)-1].Mesh
				}
				base := render3d.OccluderFromMesh(mesh.Positions, mesh.Indices, math3d.Mat4Identity())
				if len(base.Indices) < 3 {
					continue
				}
				base.DoubleSided = o.Occluder.DoubleSided
				b.built.Occluders = append(b.built.Occluders, Occluder{
					Node:        d.Node,
					Object:      o.ID,
					Mode:        sceneformat.OccluderModel,
					LocalBounds: mesh.Bounds(),
					DoubleSided: o.Occluder.DoubleSided,
					base:        base,
				})
			}
		}
	}
}

// AppendOccluders appends a render3d.Occluder for every visible, enabled
// occluder in b.Occluders at its node's current world transform and returns the
// extended slice.
func (b *Built) AppendOccluders(dst []render3d.Occluder) []render3d.Occluder {
	if b == nil {
		return dst
	}
	for i := range b.Occluders {
		occ := &b.Occluders[i]
		if occ.Node == nil || !occ.Node.EffectiveVisible() || !occ.Node.EffectiveEnabled() {
			continue
		}
		dst = append(dst, occ.RenderOccluder())
	}
	return dst
}

// RasterizeOccluders clears buf, rasterizes every visible, enabled occluder in
// b for viewProj, builds the Hi-Z pyramid when at least one occluder was
// rasterized, and returns the number of occluders rasterized.
func (b *Built) RasterizeOccluders(buf *render3d.OcclusionBuffer, viewProj math3d.Mat4) int {
	if buf == nil {
		return 0
	}
	buf.Clear()
	if b == nil || len(b.Occluders) == 0 {
		return 0
	}
	count := 0
	for i := range b.Occluders {
		occ := &b.Occluders[i]
		if occ.Node == nil || !occ.Node.EffectiveVisible() || !occ.Node.EffectiveEnabled() {
			continue
		}
		buf.Rasterize(viewProj, occ.RenderOccluder())
		count++
	}
	if count > 0 {
		buf.BuildPyramid()
	}
	return count
}
