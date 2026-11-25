package physics

import (
	"cmp"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// RigidWorld queries. They see each body at its current pose, a box turned by its Angle included, and all but QueryPoint see its Tiles, sharing the hit types and QueryMask of World's queries: QueryBodies selects bodies that are not sensors, QuerySensors sensors, and QueryTiles and QueryOneWay the tiles. A zero QueryMask means QueryAll and a zero LayerMask means LayerAll; a body matches when its effective Layer shares a bit with the LayerMask, which leaves the tiles alone (TileLayer and TileMask filter only what the tiles collide with).

// WorldShape returns b's shape at its current pose in world coordinates: a Box or Circle where (X, Y) puts it, a Box turned by a non-zero Angle as a Polygon, and a Polygon, Capsule, Segment or Chain as Step last placed it, or where its current pose puts it if it was moved since.
func (b *RigidBody) WorldShape() Shape {
	if b == nil {
		return Shape{}
	}
	switch b.Shape.Kind {
	case ShapeBox:
		w, h := b.Shape.Box.W, b.Shape.Box.H
		if b.Angle == 0 {
			return BoxShape(Box{X: b.X - float32(w*0.5), Y: b.Y - float32(h*0.5), W: w, H: h})
		}
		return PolygonShape(NewBoxPolygon(b.X, b.Y, w, h, b.Angle))
	case ShapeCircle:
		return CircleShape(Circle{X: b.X, Y: b.Y, R: b.Shape.Circle.R})
	}
	if b.hasLocal && b.Shape == b.synced {
		s, _ := b.poseShape()
		return s
	}
	return b.Shape
}

// queryMatches reports whether a query with mask and layerMask (already defaulted) sees b.
func queryMatches(b *RigidBody, mask QueryMask, layerMask LayerMask) bool {
	if b == nil || b.Shape.Empty() || effectiveLayer(b.Layer)&layerMask == 0 {
		return false
	}
	if b.Sensor {
		return mask&QuerySensors != 0
	}
	return mask&QueryBodies != 0
}

// QueryPoint returns the bodies of w matching mask and layerMask whose shape contains (x, y), in RigidWorld.Bodies order. Tiles are not bodies, so QueryTiles and QueryOneWay select nothing here; read a tile with TileFlagsAt.
func (w *RigidWorld) QueryPoint(x, y float32, mask QueryMask, layerMask LayerMask) []*RigidBody {
	if w == nil {
		return nil
	}
	if mask == 0 {
		mask = QueryAll
	}
	layerMask = effectiveLayer(layerMask)
	var hits []*RigidBody
	for _, b := range w.Bodies {
		if queryMatches(b, mask, layerMask) && b.WorldShape().Contains(x, y) {
			hits = append(hits, b)
		}
	}
	return hits
}

// QueryOverlap returns the bodies and tiles of w matching mask and layerMask that overlap shape, in deterministic order: bodies in RigidWorld.Bodies order, then tiles in row-major order. Each hit's Contact normal points from the hit toward shape.
func (w *RigidWorld) QueryOverlap(shape Shape, mask QueryMask, layerMask LayerMask) []OverlapHit {
	if w == nil || shape.Empty() {
		return nil
	}
	if mask == 0 {
		mask = QueryAll
	}
	layerMask = effectiveLayer(layerMask)
	var hits []OverlapHit
	for _, b := range w.Bodies {
		if !queryMatches(b, mask, layerMask) {
			continue
		}
		if c := TestShapes(shape, b.WorldShape()); c.Hit {
			hits = append(hits, OverlapHit{RigidBody: b, Contact: c})
		}
	}
	return appendTileOverlaps(hits, w.Tiles, shape, mask)
}

// Raycast casts the segment from from to to against the bodies and tiles of w matching mask and layerMask and returns every hit sorted by ascending T in [0, 1]. Equal distances keep bodies, in RigidWorld.Bodies order, before tiles, in the order the ray crosses them. A segment with an end that is not finite hits nothing.
func (w *RigidWorld) Raycast(from, to core.Point, mask QueryMask, layerMask LayerMask) []RaycastHit {
	if w == nil || !segmentFinite(from, to) {
		return nil
	}
	if mask == 0 {
		mask = QueryAll
	}
	layerMask = effectiveLayer(layerMask)
	fx, fy := float64(from.X), float64(from.Y)
	dx, dy := float64(to.X)-fx, float64(to.Y)-fy
	var hits []RaycastHit
	for _, b := range w.Bodies {
		if !queryMatches(b, mask, layerMask) {
			continue
		}
		if h, ok := raycastShape(fx, fy, dx, dy, b.WorldShape()); ok {
			h.RigidBody = b
			hits = append(hits, h)
		}
	}
	if w.Tiles != nil && mask&(QueryTiles|QueryOneWay) != 0 {
		hits = append(hits, raycastTiles(fx, fy, dx, dy, w.Tiles, mask, 1, false)...)
	}
	slices.SortStableFunc(hits, func(a, b RaycastHit) int {
		return cmp.Compare(a.T, b.T)
	})
	return hits
}

// RaycastFirst returns the nearest hit along the segment from from to to among the bodies and tiles of w matching mask and layerMask, with its point and surface normal. Of hits at the same distance, the body first in RigidWorld.Bodies wins, and a body wins over a tile. A segment with an end that is not finite hits nothing.
func (w *RigidWorld) RaycastFirst(from, to core.Point, mask QueryMask, layerMask LayerMask) (RaycastHit, bool) {
	if w == nil || !segmentFinite(from, to) {
		return RaycastHit{}, false
	}
	if mask == 0 {
		mask = QueryAll
	}
	layerMask = effectiveLayer(layerMask)
	fx, fy := float64(from.X), float64(from.Y)
	dx, dy := float64(to.X)-fx, float64(to.Y)-fy
	var best RaycastHit
	found := false
	for _, b := range w.Bodies {
		if !queryMatches(b, mask, layerMask) {
			continue
		}
		if h, ok := raycastShape(fx, fy, dx, dy, b.WorldShape()); ok && (!found || h.T < best.T) {
			h.RigidBody = b
			best, found = h, true
		}
	}
	if w.Tiles != nil && mask&(QueryTiles|QueryOneWay) != 0 {
		maxT := 1.0
		if found {
			maxT = float64(best.T)
		}
		if tiles := raycastTiles(fx, fy, dx, dy, w.Tiles, mask, maxT, true); len(tiles) > 0 && (!found || tiles[0].T < best.T) {
			best, found = tiles[0], true
		}
	}
	return best, found
}

// segmentFinite reports whether both ends of the segment from from to to are finite.
func segmentFinite(from, to core.Point) bool {
	return isFinite32(float32(from.X)) && isFinite32(float32(from.Y)) && isFinite32(float32(to.X)) && isFinite32(float32(to.Y))
}
