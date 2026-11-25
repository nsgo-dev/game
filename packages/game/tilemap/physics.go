package tilemap

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
)

// ellipseSides is how many vertices approximate an ellipse object that is not a circle.
const ellipseSides = physics.MaxPolygonVertices

// PhysicsShape returns o's collision shape in map coordinates for static level geometry:
//
//   - a rectangle becomes a physics.Box, or a physics.Polygon when it is rotated;
//   - an ellipse becomes a physics.Circle when it is round and a physics.Polygon of 8 vertices inscribed in it otherwise;
//   - a polygon becomes a physics.Polygon when it is convex with at most physics.MaxPolygonVertices vertices, and a closed physics.Chain along its outline otherwise, which keeps concave terrain exact;
//   - a polyline becomes a physics.Segment when it has two points and an open physics.Chain when it has more.
//
// Rotation turns the object clockwise about (X, Y), as Tiled does. Points, tile objects and objects too small to collide (a rectangle without area, a polyline with fewer than two distinct points) report false. The shape ignores the object layer's offset; ObjectLayer.PhysicsBodies applies it.
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
		if len(pts) < 3 {
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

// convexOutline reports whether the closed outline through pts turns the same way at every vertex, ignoring straight runs.
func convexOutline(pts []core.Point) bool {
	sign := 0
	n := len(pts)
	for i := range n {
		a, b, c := pts[i], pts[(i+1)%n], pts[(i+2)%n]
		cross := (float64(b.X)-float64(a.X))*(float64(c.Y)-float64(b.Y)) - (float64(b.Y)-float64(a.Y))*(float64(c.X)-float64(b.X))
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
	}
	return sign != 0
}

// PhysicsBodies returns a physics.Body for every object of l that has a PhysicsShape, in object order, moved by the layer's offset, for a physics.World's static level geometry. Each body's ID is the object's ID, its Tag the object's Type and its UserData the Object. Hidden objects are included, since collision layers are often hidden in Tiled. Use physics.NewStaticRigidBody(body.Shape) to collide them with a physics.RigidWorld.
func (l ObjectLayer) PhysicsBodies() []*physics.Body {
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
