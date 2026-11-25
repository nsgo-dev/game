package physics

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
)

// MaxPolygonVertices is the most vertices a Polygon holds, as in Box2D.
const MaxPolygonVertices = 8

// polygonWeldTolerance is the distance under which NewPolygon merges two input points and drops a vertex lying on the line through its neighbours.
const polygonWeldTolerance = 1e-4

// Polygon is a convex polygon of 3 to MaxPolygonVertices vertices in world coordinates, optionally rounded by Radius: the shape is every point within Radius of the polygon, as Box2D's rounded polygons are. Rounding keeps stacked props and characters from catching on each other's corners.
//
// Vertices[:Count] wind with a positive signed area in NSGo's X-right, Y-down space, which is clockwise on screen, and the outward normal of the edge from vertex i to vertex i+1 is (dy, -dx) normalized. Build polygons with NewPolygon or NewBoxPolygon, which compute the convex hull and the winding; a Polygon written by hand must already be convex, wound that way and free of repeated vertices.
type Polygon struct {
	Vertices [MaxPolygonVertices]core.Point
	Count    int
	Radius   float32
}

// NewPolygon returns the convex hull of points rounded by radius. It merges points closer than 1e-4 and drops vertices that lie on an edge. It reports false when the hull has fewer than 3 or more than MaxPolygonVertices vertices, when a coordinate is not finite, or when radius is negative or not finite.
func NewPolygon(points []core.Point, radius float32) (Polygon, bool) {
	if !(radius >= 0) || math.IsInf(float64(radius), 0) {
		return Polygon{}, false
	}
	pts := make([]vec2, 0, len(points))
	for _, p := range points {
		x, y := float64(p.X), float64(p.Y)
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			return Polygon{}, false
		}
		pts = append(pts, vec2{x, y})
	}
	hull := convexHull(pts)
	if len(hull) < 3 || len(hull) > MaxPolygonVertices {
		return Polygon{}, false
	}
	poly := Polygon{Count: len(hull), Radius: radius}
	for i, v := range hull {
		poly.Vertices[i] = core.Point{X: core.Points(v.x), Y: core.Points(v.y)}
	}
	return poly, true
}

// NewBoxPolygon returns a w by h rectangle centered at (cx, cy) and rotated by angle radians (clockwise on screen), as a Polygon for ramps, wedges and rotated props.
func NewBoxPolygon(cx, cy, w, h, angle float32) Polygon {
	hw, hh := float64(w)*0.5, float64(h)*0.5
	s, c := math.Sincos(float64(angle))
	local := [4]vec2{{-hw, -hh}, {hw, -hh}, {hw, hh}, {-hw, hh}}
	p := Polygon{Count: 4}
	for i, l := range local {
		p.Vertices[i] = core.Point{
			X: core.Points(float64(cx) + l.x*c - l.y*s),
			Y: core.Points(float64(cy) + l.x*s + l.y*c),
		}
	}
	return p
}

// convexHull returns the convex hull of pts with positive signed area (Andrew's monotone chain), welding near-duplicate points and dropping collinear vertices.
func convexHull(pts []vec2) []vec2 {
	sorted := slices.Clone(pts)
	slices.SortFunc(sorted, func(a, b vec2) int {
		if c := cmp.Compare(a.x, b.x); c != 0 {
			return c
		}
		return cmp.Compare(a.y, b.y)
	})
	// Weld points closer than the tolerance to an already kept point.
	welded := sorted[:0]
	for _, p := range sorted {
		dup := false
		for _, q := range welded {
			if math.Hypot(p.x-q.x, p.y-q.y) < polygonWeldTolerance {
				dup = true
				break
			}
		}
		if !dup {
			welded = append(welded, p)
		}
	}
	if len(welded) < 3 {
		return nil
	}
	cross := func(o, a, b vec2) float64 { return (a.x-o.x)*(b.y-o.y) - (a.y-o.y)*(b.x-o.x) }
	hull := make([]vec2, 0, 2*len(welded))
	for _, p := range welded {
		for len(hull) >= 2 && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	lower := len(hull) + 1
	for i := len(welded) - 2; i >= 0; i-- {
		p := welded[i]
		for len(hull) >= lower && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	hull = hull[:len(hull)-1]
	// Drop vertices lying within the tolerance of the line through their neighbours.
	for changed := true; changed && len(hull) >= 3; {
		changed = false
		for i := range hull {
			prev := hull[(i+len(hull)-1)%len(hull)]
			next := hull[(i+1)%len(hull)]
			l := math.Hypot(next.x-prev.x, next.y-prev.y)
			if l == 0 || math.Abs(cross(prev, next, hull[i]))/l < polygonWeldTolerance {
				hull = slices.Delete(hull, i, i+1)
				changed = true
				break
			}
		}
	}
	if len(hull) < 3 {
		return nil
	}
	// The monotone chain winds counterclockwise in Y-up terms, which is a positive signed area with the formula this package uses.
	return hull
}

// Empty reports whether p holds no valid polygon: fewer than 3 or more than MaxPolygonVertices vertices, or a negative Radius.
func (p Polygon) Empty() bool {
	return p.Count < 3 || p.Count > MaxPolygonVertices || !(p.Radius >= 0)
}

// Bounds returns the tightest axis-aligned Box enclosing p and its rounding, or a zero Box when p is empty.
func (p Polygon) Bounds() Box {
	if p.Empty() {
		return Box{}
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, v := range p.Vertices[:p.Count] {
		minX, maxX = min(minX, float64(v.X)), max(maxX, float64(v.X))
		minY, maxY = min(minY, float64(v.Y)), max(maxY, float64(v.Y))
	}
	r := float64(p.Radius)
	return boxFromExtents(minX-r, minY-r, maxX+r, maxY+r)
}

// Contains reports whether (x, y) lies inside or on the boundary of p, including its rounding.
func (p Polygon) Contains(x, y float32) bool {
	if p.Empty() {
		return false
	}
	h := polygonHull(p)
	return h.contains(vec2{float64(x), float64(y)})
}

// Translated returns a copy of p shifted by (dx, dy).
func (p Polygon) Translated(dx, dy float32) Polygon {
	for i := range p.Count {
		p.Vertices[i].X += core.Points(dx)
		p.Vertices[i].Y += core.Points(dy)
	}
	return p
}

// Capsule is a segment from (X1, Y1) to (X2, Y2) swept by radius R: two half circles joined by a rectangle. Its round ends let characters slide over tile seams and step edges without snagging. A capsule with R <= 0 is empty; one with equal endpoints is a circle.
type Capsule struct {
	X1, Y1, X2, Y2, R float32
}

// Empty reports whether c has no positive radius.
func (c Capsule) Empty() bool { return !(c.R > 0) }

// Center returns the midpoint of c's segment.
func (c Capsule) Center() (x, y float32) {
	return float32((float64(c.X1) + float64(c.X2)) * 0.5), float32((float64(c.Y1) + float64(c.Y2)) * 0.5)
}

// Bounds returns the tightest axis-aligned Box enclosing c, or a zero Box when c is empty.
func (c Capsule) Bounds() Box {
	if c.Empty() {
		return Box{}
	}
	r := float64(c.R)
	return boxFromExtents(
		min(float64(c.X1), float64(c.X2))-r, min(float64(c.Y1), float64(c.Y2))-r,
		max(float64(c.X1), float64(c.X2))+r, max(float64(c.Y1), float64(c.Y2))+r)
}

// Contains reports whether (x, y) lies inside or on the boundary of c.
func (c Capsule) Contains(x, y float32) bool {
	if c.Empty() {
		return false
	}
	q := closestOnSegment(vec2{float64(c.X1), float64(c.Y1)}, vec2{float64(c.X2), float64(c.Y2)}, vec2{float64(x), float64(y)})
	dx, dy := float64(x)-q.x, float64(y)-q.y
	r := float64(c.R)
	return dx*dx+dy*dy <= r*r
}

// Translated returns a copy of c shifted by (dx, dy).
func (c Capsule) Translated(dx, dy float32) Capsule {
	return Capsule{X1: c.X1 + dx, Y1: c.Y1 + dy, X2: c.X2 + dx, Y2: c.Y2 + dy, R: c.R}
}

// Segment is a line segment from (X1, Y1) to (X2, Y2) with no thickness, for static ledges and walls. It collides with polygons, boxes, circles and capsules on both sides but never with another Segment or a Chain, as in Box2D. A segment of zero length is empty.
type Segment struct {
	X1, Y1, X2, Y2 float32
}

// Empty reports whether s has zero length.
func (s Segment) Empty() bool { return s.X1 == s.X2 && s.Y1 == s.Y2 }

// Bounds returns the axis-aligned Box spanning s; it has zero width or height when s is vertical or horizontal.
func (s Segment) Bounds() Box {
	if s.Empty() {
		return Box{}
	}
	return boxFromExtents(
		min(float64(s.X1), float64(s.X2)), min(float64(s.Y1), float64(s.Y2)),
		max(float64(s.X1), float64(s.X2)), max(float64(s.Y1), float64(s.Y2)))
}

// Translated returns a copy of s shifted by (dx, dy).
func (s Segment) Translated(dx, dy float32) Segment {
	return Segment{X1: s.X1 + dx, Y1: s.Y1 + dy, X2: s.X2 + dx, Y2: s.Y2 + dy}
}

// Chain is a static polyline through Points, closed back to the first point when Loop is true, for level terrain drawn as lines. Contacts at its interior vertices only use normals the neighbouring segments allow, so a body sliding or rolling along a flat run of segments never catches on the seams between them (Box2D's ghost collisions). Like Segment, it collides on both sides and never with another Chain or a Segment.
//
// An open chain needs at least 2 points and a loop at least 3; consecutive points must differ.
type Chain struct {
	Points []core.Point
	Loop   bool
}

// Empty reports whether c is nil or has too few points for one segment (two, or three for a loop).
func (c *Chain) Empty() bool {
	if c == nil {
		return true
	}
	if c.Loop {
		return len(c.Points) < 3
	}
	return len(c.Points) < 2
}

// SegmentCount returns the number of segments in c.
func (c *Chain) SegmentCount() int {
	if c.Empty() {
		return 0
	}
	if c.Loop {
		return len(c.Points)
	}
	return len(c.Points) - 1
}

// Segment returns segment i of c, from Points[i] to the next point.
func (c *Chain) Segment(i int) Segment {
	a := c.Points[i]
	b := c.Points[(i+1)%len(c.Points)]
	return Segment{X1: float32(a.X), Y1: float32(a.Y), X2: float32(b.X), Y2: float32(b.Y)}
}

// Bounds returns the axis-aligned Box spanning every point of c.
func (c *Chain) Bounds() Box {
	if c.Empty() {
		return Box{}
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range c.Points {
		minX, maxX = min(minX, float64(p.X)), max(maxX, float64(p.X))
		minY, maxY = min(minY, float64(p.Y)), max(maxY, float64(p.Y))
	}
	return boxFromExtents(minX, minY, maxX, maxY)
}

// Translated returns a new Chain with every point of c shifted by (dx, dy).
func (c *Chain) Translated(dx, dy float32) *Chain {
	if c == nil {
		return nil
	}
	out := &Chain{Points: make([]core.Point, len(c.Points)), Loop: c.Loop}
	for i, p := range c.Points {
		out.Points[i] = core.Point{X: p.X + core.Points(dx), Y: p.Y + core.Points(dy)}
	}
	return out
}

// boxFromExtents builds a Box from its minimum and maximum corners.
func boxFromExtents(minX, minY, maxX, maxY float64) Box {
	return Box{X: float32(minX), Y: float32(minY), W: float32(maxX - minX), H: float32(maxY - minY)}
}

// PolygonShape returns a Shape wrapping p.
func PolygonShape(p Polygon) Shape { return Shape{Kind: ShapePolygon, Polygon: p} }

// CapsuleShape returns a Shape wrapping c.
func CapsuleShape(c Capsule) Shape { return Shape{Kind: ShapeCapsule, Capsule: c} }

// SegmentShape returns a Shape wrapping s.
func SegmentShape(s Segment) Shape { return Shape{Kind: ShapeSegment, Segment: s} }

// ChainShape returns a Shape holding a Chain through a copy of points, closed when loop is true.
func ChainShape(points []core.Point, loop bool) Shape {
	return Shape{Kind: ShapeChain, Chain: &Chain{Points: slices.Clone(points), Loop: loop}}
}

// Area returns the area of s. Segments and chains have none.
func (s Shape) Area() float32 {
	switch s.Kind {
	case ShapeBox:
		if s.Box.Empty() {
			return 0
		}
		return float32(float64(s.Box.W) * float64(s.Box.H))
	case ShapeCircle:
		if s.Circle.Empty() {
			return 0
		}
		r := float64(s.Circle.R)
		return float32(math.Pi * r * r)
	case ShapePolygon, ShapeCapsule:
		area, _, _, _ := s.massProperties()
		return float32(area)
	default:
		return 0
	}
}

// Centroid returns the center of mass of s, taken as uniformly dense: the area's centroid for boxes, circles, polygons and capsules, and the midpoint of the line, weighted by length, for segments and chains. It returns (0, 0) when s is empty.
func (s Shape) Centroid() (x, y float32) {
	switch s.Kind {
	case ShapeBox:
		if s.Box.Empty() {
			return 0, 0
		}
		return s.Box.Center()
	case ShapeCircle:
		if s.Circle.Empty() {
			return 0, 0
		}
		return s.Circle.X, s.Circle.Y
	case ShapePolygon, ShapeCapsule:
		_, cx, cy, _ := s.massProperties()
		return float32(cx), float32(cy)
	case ShapeSegment:
		if s.Segment.Empty() {
			return 0, 0
		}
		return float32((float64(s.Segment.X1) + float64(s.Segment.X2)) * 0.5), float32((float64(s.Segment.Y1) + float64(s.Segment.Y2)) * 0.5)
	case ShapeChain:
		if s.Chain.Empty() {
			return 0, 0
		}
		var sumL, sx, sy float64
		for i := range s.Chain.SegmentCount() {
			seg := s.Chain.Segment(i)
			l := math.Hypot(float64(seg.X2)-float64(seg.X1), float64(seg.Y2)-float64(seg.Y1))
			sumL += l
			sx += l * (float64(seg.X1) + float64(seg.X2)) * 0.5
			sy += l * (float64(seg.Y1) + float64(seg.Y2)) * 0.5
		}
		if sumL == 0 {
			p := s.Chain.Points[0]
			return float32(p.X), float32(p.Y)
		}
		return float32(sx / sumL), float32(sy / sumL)
	default:
		return 0, 0
	}
}

// MomentOfInertia returns the rotational inertia of s about its Centroid for a body of the given mass spread evenly over its area, as RigidBody uses. Segments, chains and empty shapes return 0.
func (s Shape) MomentOfInertia(mass float32) float32 {
	m := float64(mass)
	if !(m > 0) {
		return 0
	}
	switch s.Kind {
	case ShapeBox:
		if s.Box.Empty() {
			return 0
		}
		w, h := float64(s.Box.W), float64(s.Box.H)
		return float32(m * (w*w + h*h) / 12.0)
	case ShapeCircle:
		if s.Circle.Empty() {
			return 0
		}
		r := float64(s.Circle.R)
		return float32(0.5 * m * r * r)
	case ShapePolygon, ShapeCapsule:
		area, _, _, unitInertia := s.massProperties()
		if area <= 0 {
			return 0
		}
		return float32(m * unitInertia / area)
	default:
		return 0
	}
}

// massProperties returns the area, centroid and the inertia about the centroid at unit density of a polygon or capsule.
func (s Shape) massProperties() (area, cx, cy, inertia float64) {
	switch s.Kind {
	case ShapeCapsule:
		c := s.Capsule
		if c.Empty() {
			return 0, 0, 0, 0
		}
		r := float64(c.R)
		rr := r * r
		length := math.Hypot(float64(c.X2)-float64(c.X1), float64(c.Y2)-float64(c.Y1))
		circleMass := math.Pi * rr
		boxMass := 2 * r * length
		// The two half circles, each offset by half the length, follow Box2D's b2ComputeCapsuleMass with the parallel-axis theorem applied twice.
		lc := 4 * r / (3 * math.Pi)
		h := 0.5 * length
		circleInertia := circleMass * (0.5*rr + h*h + 2*h*lc)
		boxInertia := boxMass * (4*rr + length*length) / 12
		x, y := c.Center()
		return circleMass + boxMass, float64(x), float64(y), circleInertia + boxInertia
	case ShapePolygon:
		p := s.Polygon
		if p.Empty() {
			return 0, 0, 0, 0
		}
		h := polygonHull(p)
		verts := h.v
		if h.r > 0 {
			// Rounded polygons are approximated by pushing each vertex out along its corner bisector, as Box2D does.
			const sqrt2 = 1.412
			for i := range h.count {
				prev := h.n[(i+h.count-1)%h.count]
				next := h.n[i]
				mid := vec2{prev.x + next.x, prev.y + next.y}
				if l := math.Hypot(mid.x, mid.y); l > 0 {
					mid = vec2{mid.x / l, mid.y / l}
				}
				verts[i] = vec2{h.v[i].x + sqrt2*h.r*mid.x, h.v[i].y + sqrt2*h.r*mid.y}
			}
		}
		// Triangle fan from the first vertex, relative to it for precision.
		origin := verts[0]
		var sumA, sumX, sumY, sumI float64
		for i := 1; i+1 < h.count; i++ {
			e1 := vec2{verts[i].x - origin.x, verts[i].y - origin.y}
			e2 := vec2{verts[i+1].x - origin.x, verts[i+1].y - origin.y}
			d := e1.x*e2.y - e1.y*e2.x
			triA := 0.5 * d
			sumA += triA
			sumX += triA * (e1.x + e2.x) / 3
			sumY += triA * (e1.y + e2.y) / 3
			intx2 := e1.x*e1.x + e2.x*e1.x + e2.x*e2.x
			inty2 := e1.y*e1.y + e2.y*e1.y + e2.y*e2.y
			sumI += (0.25 / 3.0 * d) * (intx2 + inty2)
		}
		if sumA <= 0 {
			return 0, 0, 0, 0
		}
		lcx, lcy := sumX/sumA, sumY/sumA
		// Inertia about the centroid by the parallel-axis theorem.
		inertia = sumI - sumA*(lcx*lcx+lcy*lcy)
		return sumA, origin.x + lcx, origin.y + lcy, inertia
	}
	return 0, 0, 0, 0
}

// transformed rotates s by angle about the origin and then moves it by (x, y). It handles polygons, capsules, segments and chains, whose geometry is free to rotate.
func (s Shape) transformed(x, y, angle float64) Shape {
	sn, cs := math.Sincos(angle)
	tf := func(px, py float64) (float64, float64) {
		return x + px*cs - py*sn, y + px*sn + py*cs
	}
	switch s.Kind {
	case ShapePolygon:
		for i := range s.Polygon.Count {
			v := s.Polygon.Vertices[i]
			wx, wy := tf(float64(v.X), float64(v.Y))
			s.Polygon.Vertices[i] = core.Point{X: core.Points(wx), Y: core.Points(wy)}
		}
	case ShapeCapsule:
		c := s.Capsule
		x1, y1 := tf(float64(c.X1), float64(c.Y1))
		x2, y2 := tf(float64(c.X2), float64(c.Y2))
		s.Capsule = Capsule{X1: float32(x1), Y1: float32(y1), X2: float32(x2), Y2: float32(y2), R: c.R}
	case ShapeSegment:
		g := s.Segment
		x1, y1 := tf(float64(g.X1), float64(g.Y1))
		x2, y2 := tf(float64(g.X2), float64(g.Y2))
		s.Segment = Segment{X1: float32(x1), Y1: float32(y1), X2: float32(x2), Y2: float32(y2)}
	case ShapeChain:
		if s.Chain == nil {
			return s
		}
		out := &Chain{Points: make([]core.Point, len(s.Chain.Points)), Loop: s.Chain.Loop}
		for i, p := range s.Chain.Points {
			wx, wy := tf(float64(p.X), float64(p.Y))
			out.Points[i] = core.Point{X: core.Points(wx), Y: core.Points(wy)}
		}
		s.Chain = out
	}
	return s
}

// toLocal is the inverse of transformed: it expresses world-space s relative to a body at (x, y) rotated by angle.
func (s Shape) toLocal(x, y, angle float64) Shape {
	return s.transformed(-x, -y, 0).transformed(0, 0, -angle)
}
