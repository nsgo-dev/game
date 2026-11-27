package tilemap

import (
	"math"

	"github.com/nsgo-dev/game/pkg/physics"
	"github.com/nsgo-dev/nsgo/core"
)

// Compile-time checks that *Tilemap and *WorldStream satisfy physics.TileGrid and physics.TileGridOrigin directly.
var (
	_ physics.TileGrid       = (*Tilemap)(nil)
	_ physics.TileGridOrigin = (*Tilemap)(nil)
	_ physics.TileGrid       = (*WorldStream)(nil)
	_ physics.TileGridOrigin = (*WorldStream)(nil)
)

// PhysicsFlags maps tilemap TileFlags to physics.TileFlags.
// TileSolid (bit 0) and TileOneWay (bit 1) map directly to physics.TileSolid and physics.TileOneWay,
// and game-defined bits 2..31 are preserved verbatim.
func (f TileFlags) PhysicsFlags() physics.TileFlags {
	return physics.TileFlags(f)
}

// TileSize returns the width and height of one tile in world coordinates, satisfying physics.TileGrid.
func (m *Tilemap) TileSize() (width, height float32) {
	if m == nil {
		return 0, 0
	}
	return m.TileWidth, m.TileHeight
}

// GridSize returns the map's width and height in tile columns and rows, satisfying physics.TileGrid.
func (m *Tilemap) GridSize() (cols, rows int) {
	if m == nil {
		return 0, 0
	}
	return m.Width, m.Height
}

// GridOrigin returns the world-space origin of cell (0, 0), satisfying physics.TileGridOrigin.
func (m *Tilemap) GridOrigin() (x, y float32) {
	if m == nil {
		return 0, 0
	}
	return float32(m.Origin.X), float32(m.Origin.Y)
}

// TileFlagsAt returns the collision flags at (col, row) mapped to physics.TileFlags, satisfying physics.TileGrid.
func (m *Tilemap) TileFlagsAt(col, row int) physics.TileFlags {
	if m == nil {
		return 0
	}
	return m.FlagsAt(col, row).PhysicsFlags()
}

// TileFlagsAt returns the combined collision flags at uniform grid cell (col, row) relative to GridOrigin
// mapped to physics.TileFlags, satisfying physics.TileGrid.
func (s *WorldStream) TileFlagsAt(col, row int) physics.TileFlags {
	return s.GridFlagsAt(col, row).PhysicsFlags()
}

// ellipseSides is how many vertices approximate an ellipse object that is not a circle.
const ellipseSides = physics.MaxPolygonVertices

// PhysicsShape returns o's collision shape in map coordinates for static level geometry:
//
//   - a rectangle becomes a physics.Box, or a physics.Polygon when it is rotated;
//   - an ellipse becomes a physics.Circle when it is round and a physics.Polygon of 8 vertices inscribed in it otherwise;
//   - a polygon becomes a physics.Polygon when it is convex with at most physics.MaxPolygonVertices vertices, and a closed physics.Chain along its outline otherwise, which keeps concave terrain exact;
//   - a polyline becomes a physics.Segment when it has two points and an open physics.Chain when it has more.
//
// Rotation turns the object clockwise about (X, Y), as Tiled does. Points, tile objects and objects too small to collide (a rectangle without area, a polygon whose points all lie on one line, a polyline with fewer than two distinct points) report false. The shape ignores the object layer's offset; ObjectLayer.PhysicsBodies applies it.
func (o Object) PhysicsShape() (physics.Shape, bool) {
	rot := float64(o.Rotation) * math.Pi / 180
	sin, cos := math.Sincos(rot)
	place := func(lx, ly float64) core.Point {
		return core.Point{
			X: core.Points(float64(o.X) + lx*cos - ly*sin),
			Y: core.Points(float64(o.Y) + lx*sin + ly*cos),
		}
	}
	switch o.Shape {
	case ObjectRect:
		if o.Width <= 0 || o.Height <= 0 {
			return physics.Shape{}, false
		}
		if rot == 0 {
			return physics.BoxShape(physics.Box{X: o.X, Y: o.Y, W: o.Width, H: o.Height}), true
		}
		w, h := float64(o.Width), float64(o.Height)
		poly, ok := physics.NewPolygon([]core.Point{place(0, 0), place(w, 0), place(w, h), place(0, h)}, 0)
		return physics.PolygonShape(poly), ok
	case ObjectEllipse:
		if o.Width <= 0 || o.Height <= 0 {
			return physics.Shape{}, false
		}
		rx, ry := float64(o.Width)*0.5, float64(o.Height)*0.5
		if o.Width == o.Height {
			c := place(rx, ry)
			return physics.CircleShape(physics.Circle{X: float32(c.X), Y: float32(c.Y), R: float32(rx)}), true
		}
		pts := make([]core.Point, ellipseSides)
		for i := range pts {
			a := 2 * math.Pi * float64(i) / ellipseSides
			pts[i] = place(rx+rx*math.Cos(a), ry+ry*math.Sin(a))
		}
		poly, ok := physics.NewPolygon(pts, 0)
		return physics.PolygonShape(poly), ok
	case ObjectPolygon:
		pts := o.placedPoints(place)
		if len(pts) < 3 || flatOutline(pts) {
			return physics.Shape{}, false
		}
		if convexOutline(pts) {
			if poly, ok := physics.NewPolygon(pts, 0); ok {
				return physics.PolygonShape(poly), true
			}
		}
		s := physics.ChainShape(pts, true)
		return s, !s.Empty()
	case ObjectPolyline:
		pts := o.placedPoints(place)
		if len(pts) < 2 {
			return physics.Shape{}, false
		}
		if len(pts) == 2 {
			return physics.SegmentShape(physics.Segment{X1: float32(pts[0].X), Y1: float32(pts[0].Y), X2: float32(pts[1].X), Y2: float32(pts[1].Y)}), true
		}
		return physics.ChainShape(pts, false), true
	}
	return physics.Shape{}, false
}

// placedPoints returns o's points rotated and moved into map coordinates, dropping each point equal to the one before it (and, for a polygon, a last point equal to the first).
func (o Object) placedPoints(place func(lx, ly float64) core.Point) []core.Point {
	out := make([]core.Point, 0, len(o.Points))
	for _, p := range o.Points {
		q := place(float64(p.X), float64(p.Y))
		if n := len(out); n > 0 && out[n-1] == q {
			continue
		}
		out = append(out, q)
	}
	if o.Shape == ObjectPolygon && len(out) > 1 && out[0] == out[len(out)-1] {
		out = out[:len(out)-1]
	}
	return out
}

// convexOutline reports whether the closed outline through pts is a simple convex polygon: it turns the same way at every vertex, ignoring straight runs, and turns once around in total, which rules out a self-intersecting star such as a pentagram, whose every turn has the same sign too.
func convexOutline(pts []core.Point) bool {
	sign := 0
	turning := 0.0
	n := len(pts)
	for i := range n {
		a, b, c := pts[i], pts[(i+1)%n], pts[(i+2)%n]
		ux, uy := float64(b.X)-float64(a.X), float64(b.Y)-float64(a.Y)
		vx, vy := float64(c.X)-float64(b.X), float64(c.Y)-float64(b.Y)
		cross := ux*vy - uy*vx
		switch {
		case cross > 0:
			if sign < 0 {
				return false
			}
			sign = 1
		case cross < 0:
			if sign > 0 {
				return false
			}
			sign = -1
		}
		turning += math.Atan2(cross, ux*vx+uy*vy)
	}
	return sign != 0 && math.Abs(math.Abs(turning)-2*math.Pi) < 1e-6
}

// flatOutline reports whether every point of pts lies on one line, so the outline encloses no area.
func flatOutline(pts []core.Point) bool {
	a := pts[0]
	for i := 1; i+1 < len(pts); i++ {
		b, c := pts[i], pts[i+1]
		ux, uy := float64(b.X)-float64(a.X), float64(b.Y)-float64(a.Y)
		vx, vy := float64(c.X)-float64(a.X), float64(c.Y)-float64(a.Y)
		if math.Abs(ux*vy-uy*vx) > 1e-9*(math.Hypot(ux, uy)*math.Hypot(vx, vy)) {
			return false
		}
	}
	return true
}

// PhysicsBodies returns a physics.Body for every object of l that has a PhysicsShape, in object order, moved by the layer's offset, for a physics.World's static level geometry. Each body's ID is the object's ID, its Tag the object's Type and its UserData the Object. Hidden objects are included, since collision layers are often hidden in Tiled. Use physics.NewStaticRigidBody(body.Shape) to collide them with a physics.RigidWorld. A nil layer, which Tilemap.ObjectLayer returns for a name the map does not have, has no bodies. Like the objects, the bodies are in the map's local coordinates and ignore Tilemap.Origin; translate their shapes by it for a map placed in a world.
func (l *ObjectLayer) PhysicsBodies() []*physics.Body {
	if l == nil {
		return nil
	}
	var out []*physics.Body
	for _, o := range l.Objects {
		s, ok := o.PhysicsShape()
		if !ok {
			continue
		}
		if l.OffsetX != 0 || l.OffsetY != 0 {
			s = s.Translated(l.OffsetX, l.OffsetY)
		}
		out = append(out, &physics.Body{ID: o.ID, Tag: o.Type, Shape: s, UserData: o})
	}
	return out
}
