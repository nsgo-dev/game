// Package cover3d generates ai.CoverPoint values from the colliders of a physics3d world, projecting the vertical faces of 3D obstacles onto the XZ ground plane.
//
// It lives apart from pkg/ai so that ai does not depend on the 3D physics engine and physics3d does not depend on ai.
package cover3d

import (
	"math"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/physics3d"
)

// Options configures 3D collider-edge cover point generation projected onto the XZ ground plane.
//
// Generated ai.CoverPoint values store (X, Z) in Pos, the obstacle's floor Y in Elevation, and the horizontal XZ unit direction into the obstacle in Facing.
type Options struct {
	// Spacing is the horizontal distance between cover samples along each collider face (defaults to 1.0 when <= 0).
	Spacing float32
	// Standoff offsets each cover point outward from the collider face into open space (defaults to 0.5 when <= 0).
	Standoff float32
	// EdgeInset is the margin from face corners (defaults to Spacing * 0.5 when <= 0).
	EdgeInset float32
	// MinEdgeLength skips horizontal faces shorter than this length (<= 0 allows any positive face).
	MinEdgeLength float32
	// HalfCoverMinHeight is the minimum obstacle vertical height for ai.CoverHalf (defaults to 0.7 when <= 0). Shorter obstacles (such as floor slabs) do not generate cover.
	HalfCoverMinHeight float32
	// FullCoverMinHeight is the minimum obstacle vertical height for ai.CoverFull (defaults to 1.6 when <= 0).
	FullCoverMinHeight float32
	// LayerMask filters bodies considered as cover obstacles (0 defaults to physics3d.LayerAll).
	LayerMask physics3d.LayerMask
	// IncludeDynamic includes non-static bodies when true; by default only static level geometry generates cover.
	IncludeDynamic bool
	// StartID is the first ai.CoverPoint.ID assigned (defaults to 1 when 0).
	StartID uint64
}

// FromWorld generates deterministic XZ-plane cover points along the vertical faces of matching colliders in w, discarding points that overlap any other matching collider.
func FromWorld(w *physics3d.World, opts Options) []ai.CoverPoint {
	if w == nil {
		return nil
	}
	return FromBodies(w.Bodies(), opts)
}

// FromBodies generates deterministic XZ-plane cover points from the given 3D bodies: axis-aligned or oriented boxes, spheres, vertical capsules, and cylinders standing on an end (their axis within about 25 degrees of vertical). Convex hulls and cylinders lying on their side generate no cover.
func FromBodies(bodies []*physics3d.Body, opts Options) []ai.CoverPoint {
	if len(bodies) == 0 {
		return nil
	}
	spacing := opts.Spacing
	if !(spacing > 0) || math.IsInf(float64(spacing), 0) {
		spacing = 1.0
	}
	standoff := opts.Standoff
	if !(standoff > 0) || math.IsInf(float64(standoff), 0) {
		standoff = 0.5
	}
	inset := opts.EdgeInset
	if !(inset > 0) || math.IsInf(float64(inset), 0) {
		inset = spacing * 0.5
	}
	halfMin := opts.HalfCoverMinHeight
	if !(halfMin > 0) || math.IsInf(float64(halfMin), 0) {
		halfMin = 0.7
	}
	fullMin := opts.FullCoverMinHeight
	if !(fullMin > 0) || math.IsInf(float64(fullMin), 0) {
		fullMin = 1.6
	}
	if fullMin < halfMin {
		fullMin = halfMin
	}
	layerMask := opts.LayerMask
	if layerMask == 0 {
		layerMask = physics3d.LayerAll
	}

	bodyMatches := func(b *physics3d.Body) bool {
		if b == nil || b.IsTrigger {
			return false
		}
		if !opts.IncludeDynamic && !b.Static {
			return false
		}
		l := b.Layer
		if l == 0 {
			l = physics3d.LayerAll
		}
		return l&layerMask != 0
	}

	// Precompute world shapes for matching bodies once so overlap pruning is fast and deterministic.
	type worldBody struct {
		body  *physics3d.Body
		shape physics3d.Shape
	}
	active := make([]worldBody, 0, len(bodies))
	var appendShape func(b *physics3d.Body, s physics3d.Shape)
	appendShape = func(b *physics3d.Body, s physics3d.Shape) {
		if s.Kind == physics3d.ShapeCompound {
			for _, child := range s.Children {
				if child.Trigger || (child.EffectiveLayer(b.Layer)&layerMask) == 0 {
					continue
				}
				appendShape(b, child.LocalShape())
			}
			return
		}
		active = append(active, worldBody{body: b, shape: s})
	}
	for _, b := range bodies {
		if b == nil || b.IsTrigger {
			continue
		}
		if !opts.IncludeDynamic && !b.Static {
			continue
		}
		if b.Shape.Kind == physics3d.ShapeCompound {
			appendShape(b, b.WorldShape())
			continue
		}
		if bodyMatches(b) {
			active = append(active, worldBody{body: b, shape: b.WorldShape()})
		}
	}
	if len(active) == 0 {
		return nil
	}

	probeRadius := min(standoff*0.4, 0.2)
	if probeRadius < 0.05 {
		probeRadius = 0.05
	}
	isOpen3D := func(posXZ math2d.Vec2, elevY float32) bool {
		probe := physics3d.SphereShape(physics3d.Sphere{
			Center: physics3d.Vec3{X: posXZ.X, Y: elevY + halfMin*0.5, Z: posXZ.Y},
			Radius: probeRadius,
		})
		for i := range active {
			if physics3d.TestShapes(probe, active[i].shape).Hit {
				return false
			}
		}
		return true
	}

	nextID := opts.StartID
	if nextID == 0 {
		nextID = 1
	}

	var out []ai.CoverPoint
	for i := range active {
		b := active[i].body
		ws := active[i].shape
		bounds := ws.Bounds()
		minY := bounds.Min().Y
		height := bounds.Max().Y - minY
		if height < halfMin {
			continue
		}
		coverHeight := ai.CoverHalf
		if height >= fullMin {
			coverHeight = ai.CoverFull
		}
		var tags []string
		if b.Tag != "" {
			tags = []string{b.Tag}
		}

		switch ws.Kind {
		case physics3d.ShapeBox:
			rot := boxRotation(ws)
			axes := boxAxes(rot)
			he := ws.Box.HalfExtents
			// Inspect the 6 faces of the OBB in fixed order and keep near-vertical side faces.
			for axisIdx := range 3 {
				axis := axes[axisIdx]
				// Skip faces whose normal is mostly vertical (top/bottom).
				if abs32(axis.Y) > 0.35 {
					continue
				}
				horizAxis := -1
				bestHorizScore := float32(-1)
				for other := range 3 {
					if other == axisIdx {
						continue
					}
					// Pick the tangent axis lying closest to the horizontal XZ plane.
					score := 1 - abs32(axes[other].Y)
					if score > bestHorizScore {
						bestHorizScore = score
						horizAxis = other
					}
				}
				if horizAxis < 0 {
					continue
				}
				spanVec := axes[horizAxis].Scale(vecComp(he, horizAxis))
				halfNormal := axis.Scale(vecComp(he, axisIdx))

				for _, sign := range [2]float32{1, -1} {
					outNormal := axis.Scale(sign)
					facingXZ := math2d.Vec2{X: -outNormal.X, Y: -outNormal.Z}.Normalize()
					if facingXZ == (math2d.Vec2{}) {
						continue
					}
					faceCenter := ws.Box.Center.Add(halfNormal.Scale(sign))
					p0 := faceCenter.Sub(spanVec)
					p1 := faceCenter.Add(spanVec)
					seg := ai.CoverSegment{
						A:         math2d.V2(p0.X, p0.Z),
						B:         math2d.V2(p1.X, p1.Z),
						Facing:    facingXZ,
						Height:    coverHeight,
						Elevation: minY,
						Tags:      tags,
					}
					pts := ai.GenerateCoverFromSegments([]ai.CoverSegment{seg}, ai.ColliderCoverOptions{
						Spacing:       spacing,
						Standoff:      standoff,
						EdgeInset:     inset,
						MinEdgeLength: opts.MinEdgeLength,
						DefaultHeight: coverHeight,
						StartID:       nextID,
					})
					for _, pt := range pts {
						if !isOpen3D(pt.Pos, minY) {
							continue
						}
						pt.ID = nextID
						nextID++
						out = append(out, pt)
					}
				}
			}
		case physics3d.ShapeSphere, physics3d.ShapeCapsule, physics3d.ShapeCylinder:
			var cx, cz, rad float32
			switch ws.Kind {
			case physics3d.ShapeSphere:
				cx, cz, rad = ws.Sphere.Center.X, ws.Sphere.Center.Z, ws.Sphere.Radius
			case physics3d.ShapeCapsule:
				cx = (ws.Capsule.A.X + ws.Capsule.B.X) * 0.5
				cz = (ws.Capsule.A.Z + ws.Capsule.B.Z) * 0.5
				rad = ws.Capsule.Radius
			case physics3d.ShapeCylinder:
				// A pillar or a barrel standing on an end; one lying on its side is not round seen from above.
				c := ws.Cylinder
				if h := c.Height(); h <= 0 || abs32(c.B.Y-c.A.Y) < 0.9*h {
					continue
				}
				center := c.Center()
				cx, cz, rad = center.X, center.Z, c.Radius
			}
			perim := 2 * math.Pi * float64(rad)
			if rad <= 0 || (opts.MinEdgeLength > 0 && perim < float64(opts.MinEdgeLength)) {
				continue
			}
			count := max(4, int(math.Round(perim/float64(spacing))))
			sampleR := float64(rad) + float64(standoff)
			for k := range count {
				ang := 2 * math.Pi * float64(k) / float64(count)
				sn, cs := math.Sincos(ang)
				pos := math2d.Vec2{
					X: float32(float64(cx) + cs*sampleR),
					Y: float32(float64(cz) + sn*sampleR),
				}
				if !isOpen3D(pos, minY) {
					continue
				}
				pt := ai.CoverPoint{
					ID:        nextID,
					Pos:       pos,
					Facing:    math2d.Vec2{X: float32(-cs), Y: float32(-sn)},
					Height:    coverHeight,
					Elevation: minY,
				}
				if len(tags) > 0 {
					pt.Tags = append([]string(nil), tags...)
				}
				nextID++
				out = append(out, pt)
			}
		}
	}

	return out
}

// boxRotation reports the unit rotation of a box shape, as physics3d's narrow phase interprets Shape.Rot.
func boxRotation(s physics3d.Shape) physics3d.Quat {
	if s.Rot.IsIdentity() {
		return physics3d.QuatIdentity()
	}
	return s.Rot.Norm()
}

// boxAxes returns the world-space unit axes of an oriented box.
func boxAxes(rot physics3d.Quat) [3]physics3d.Vec3 {
	return [3]physics3d.Vec3{rot.RotateVec(physics3d.Vec3{X: 1}), rot.RotateVec(physics3d.Vec3{Y: 1}), rot.RotateVec(physics3d.Vec3{Z: 1})}
}

func vecComp(v physics3d.Vec3, i int) float32 {
	switch i {
	case 0:
		return v.X
	case 1:
		return v.Y
	default:
		return v.Z
	}
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
