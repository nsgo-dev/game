package physics

import "math"

// Box is an axis-aligned rectangle whose top-left corner sits at X, Y with width W and height H.
// A box with W <= 0 or H <= 0 is empty and never overlaps anything.
type Box struct {
	X, Y, W, H float32
}

// Empty reports whether b has no positive area.
func (b Box) Empty() bool { return b.W <= 0 || b.H <= 0 }

// Left returns the box's left edge (X).
func (b Box) Left() float32 { return b.X }

// Top returns the box's top edge (Y).
func (b Box) Top() float32 { return b.Y }

// Right returns the box's right edge (X + W).
func (b Box) Right() float32 { return b.X + b.W }

// Bottom returns the box's bottom edge (Y + H).
func (b Box) Bottom() float32 { return b.Y + b.H }

// Center returns the midpoint of b.
func (b Box) Center() (x, y float32) {
	return b.X + float32(b.W*0.5), b.Y + float32(b.H*0.5)
}

// Contains reports whether (x, y) lies inside or on the boundary of b.
func (b Box) Contains(x, y float32) bool {
	return !b.Empty() && x >= b.X && x <= b.X+b.W && y >= b.Y && y <= b.Y+b.H
}

// Translated returns a copy of b shifted by (dx, dy).
func (b Box) Translated(dx, dy float32) Box {
	return Box{X: b.X + dx, Y: b.Y + dy, W: b.W, H: b.H}
}

// Circle is a disk centered at X, Y with radius R.
// A circle with R <= 0 is empty and never overlaps anything.
type Circle struct {
	X, Y, R float32
}

// Empty reports whether c has no positive radius.
func (c Circle) Empty() bool { return c.R <= 0 }

// Bounds returns the tightest axis-aligned Box enclosing c.
func (c Circle) Bounds() Box {
	if c.Empty() {
		return Box{}
	}
	return Box{X: c.X - c.R, Y: c.Y - c.R, W: c.R * 2, H: c.R * 2}
}

// Contains reports whether (x, y) lies inside or on the boundary of c.
func (c Circle) Contains(x, y float32) bool {
	if c.Empty() {
		return false
	}
	dx := float64(x) - float64(c.X)
	dy := float64(y) - float64(c.Y)
	r := float64(c.R)
	return float64(dx*dx)+float64(dy*dy) <= float64(r*r)
}

// Translated returns a copy of c shifted by (dx, dy).
func (c Circle) Translated(dx, dy float32) Circle {
	return Circle{X: c.X + dx, Y: c.Y + dy, R: c.R}
}

// ShapeKind identifies which geometry a Shape holds.
type ShapeKind uint8

const (
	// ShapeNone is the empty zero shape.
	ShapeNone ShapeKind = iota
	// ShapeBox holds an axis-aligned Box.
	ShapeBox
	// ShapeCircle holds a Circle.
	ShapeCircle
	// ShapePolygon holds a convex, optionally rounded Polygon.
	ShapePolygon
	// ShapeCapsule holds a Capsule.
	ShapeCapsule
	// ShapeSegment holds a Segment.
	ShapeSegment
	// ShapeChain holds a Chain.
	ShapeChain
)

// Shape holds one of Box, Circle, Polygon, Capsule, Segment or Chain, selected by Kind, so bodies, triggers and queries handle every shape uniformly. Shapes are comparable with ==; a chain compares by its pointer.
type Shape struct {
	Kind    ShapeKind
	Box     Box
	Circle  Circle
	Polygon Polygon
	Capsule Capsule
	Segment Segment
	Chain   *Chain
}

// BoxShape returns a Shape wrapping b.
func BoxShape(b Box) Shape { return Shape{Kind: ShapeBox, Box: b} }

// CircleShape returns a Shape wrapping c.
func CircleShape(c Circle) Shape { return Shape{Kind: ShapeCircle, Circle: c} }

// Empty reports whether s holds no usable geometry: a Box or Circle without positive area, a Polygon or Capsule that is empty, a Segment of zero length or a Chain without a segment. Segments and chains have no area but are not empty.
func (s Shape) Empty() bool {
	switch s.Kind {
	case ShapeBox:
		return s.Box.Empty()
	case ShapeCircle:
		return s.Circle.Empty()
	case ShapePolygon:
		return s.Polygon.Empty()
	case ShapeCapsule:
		return s.Capsule.Empty()
	case ShapeSegment:
		return s.Segment.Empty()
	case ShapeChain:
		return s.Chain.Empty()
	default:
		return true
	}
}

// Bounds returns the axis-aligned Box enclosing s, or a zero Box when s is empty. The bounds of a horizontal or vertical segment or chain have zero height or width.
func (s Shape) Bounds() Box {
	switch s.Kind {
	case ShapeBox:
		if s.Box.Empty() {
			return Box{}
		}
		return s.Box
	case ShapeCircle:
		return s.Circle.Bounds()
	case ShapePolygon:
		return s.Polygon.Bounds()
	case ShapeCapsule:
		return s.Capsule.Bounds()
	case ShapeSegment:
		return s.Segment.Bounds()
	case ShapeChain:
		return s.Chain.Bounds()
	default:
		return Box{}
	}
}

// Contains reports whether (x, y) lies inside or on the boundary of s. Segments and chains have no inside and contain no point.
func (s Shape) Contains(x, y float32) bool {
	switch s.Kind {
	case ShapeBox:
		return s.Box.Contains(x, y)
	case ShapeCircle:
		return s.Circle.Contains(x, y)
	case ShapePolygon:
		return s.Polygon.Contains(x, y)
	case ShapeCapsule:
		return s.Capsule.Contains(x, y)
	default:
		return false
	}
}

// Translated returns a copy of s shifted by (dx, dy). A chain is copied into a new Chain.
func (s Shape) Translated(dx, dy float32) Shape {
	switch s.Kind {
	case ShapeBox:
		s.Box = s.Box.Translated(dx, dy)
	case ShapeCircle:
		s.Circle = s.Circle.Translated(dx, dy)
	case ShapePolygon:
		s.Polygon = s.Polygon.Translated(dx, dy)
	case ShapeCapsule:
		s.Capsule = s.Capsule.Translated(dx, dy)
	case ShapeSegment:
		s.Segment = s.Segment.Translated(dx, dy)
	case ShapeChain:
		s.Chain = s.Chain.Translated(dx, dy)
	}
	return s
}

// Contact describes an overlap between two shapes A and B.
//
// When Hit is true, (NormalX, NormalY) is a unit vector pointing from B toward A:
// translating A by (NormalX*Depth, NormalY*Depth) separates A from B along the
// minimum translation axis. Shapes that only touch along an edge or point with
// zero penetration report Hit == false.
type Contact struct {
	Hit              bool
	NormalX, NormalY float32
	Depth            float32
	PointX, PointY   float32
}

// TestBoxBox tests whether boxes a and b overlap. The returned Contact normal points from b toward a.
// For concentric boxes where horizontal and vertical penetration depths are equal, the normal falls back to (0, -1).
func TestBoxBox(a, b Box) Contact {
	if a.Empty() || b.Empty() {
		return Contact{}
	}
	aMinX, aMaxX := float64(a.X), float64(a.X)+float64(a.W)
	aMinY, aMaxY := float64(a.Y), float64(a.Y)+float64(a.H)
	bMinX, bMaxX := float64(b.X), float64(b.X)+float64(b.W)
	bMinY, bMaxY := float64(b.Y), float64(b.Y)+float64(b.H)

	pushRight := bMaxX - aMinX
	pushLeft := aMaxX - bMinX
	if pushRight <= 0 || pushLeft <= 0 {
		return Contact{}
	}
	pushDown := bMaxY - aMinY
	pushUp := aMaxY - bMinY
	if pushDown <= 0 || pushUp <= 0 {
		return Contact{}
	}

	penX := pushLeft
	nx := float32(-1)
	if pushRight < pushLeft {
		penX = pushRight
		nx = 1
	}

	penY := pushUp
	ny := float32(-1)
	if pushDown < pushUp {
		penY = pushDown
		ny = 1
	}

	// Midpoint of the intersection rectangle is symmetric in a and b.
	ovLeft := max(aMinX, bMinX)
	ovRight := min(aMaxX, bMaxX)
	ovTop := max(aMinY, bMinY)
	ovBottom := min(aMaxY, bMaxY)
	px := float32((ovLeft + ovRight) * 0.5)
	py := float32((ovTop + ovBottom) * 0.5)

	if penX < penY {
		return Contact{
			Hit:     true,
			NormalX: nx,
			NormalY: 0,
			Depth:   float32(penX),
			PointX:  px,
			PointY:  py,
		}
	}
	return Contact{
		Hit:     true,
		NormalX: 0,
		NormalY: ny,
		Depth:   float32(penY),
		PointX:  px,
		PointY:  py,
	}
}

// TestCircleCircle tests whether circles a and b overlap. The returned Contact normal points from b toward a.
// Concentric circles use the deterministic fallback normal (0, -1).
func TestCircleCircle(a, b Circle) Contact {
	if a.Empty() || b.Empty() {
		return Contact{}
	}
	dx := float64(a.X) - float64(b.X)
	dy := float64(a.Y) - float64(b.Y)
	sumR := float64(a.R) + float64(b.R)
	distSq := float64(dx*dx) + float64(dy*dy)
	if distSq >= float64(sumR*sumR) {
		return Contact{}
	}
	if distSq == 0 {
		return Contact{
			Hit:     true,
			NormalX: 0,
			NormalY: -1,
			Depth:   float32(sumR),
			PointX:  a.X,
			PointY:  a.Y,
		}
	}
	dist := math.Sqrt(distSq)
	nx := dx / dist
	ny := dy / dist
	depth := sumR - dist
	// Midpoint between the two penetrating surfaces along the line of centers.
	midDist := float64(b.R) - float64(depth*0.5)
	return Contact{
		Hit:     true,
		NormalX: float32(nx),
		NormalY: float32(ny),
		Depth:   float32(depth),
		PointX:  float32(float64(b.X) + float64(nx*midDist)),
		PointY:  float32(float64(b.Y) + float64(ny*midDist)),
	}
}

// TestCircleBox tests whether circle a and box b overlap. The returned Contact normal points from b toward a.
func TestCircleBox(a Circle, b Box) Contact {
	if a.Empty() || b.Empty() {
		return Contact{}
	}
	minX, maxX := float64(b.X), float64(b.X)+float64(b.W)
	minY, maxY := float64(b.Y), float64(b.Y)+float64(b.H)
	cx, cy := float64(a.X), float64(a.Y)
	r := float64(a.R)

	// Closest point on b to the circle's center.
	px := min(max(cx, minX), maxX)
	py := min(max(cy, minY), maxY)
	dx := cx - px
	dy := cy - py
	distSq := float64(dx*dx) + float64(dy*dy)
	if distSq > 0 {
		if distSq >= r*r {
			return Contact{}
		}
		dist := math.Sqrt(distSq)
		return Contact{
			Hit:     true,
			NormalX: float32(dx / dist),
			NormalY: float32(dy / dist),
			Depth:   float32(r - dist),
			PointX:  float32(px),
			PointY:  float32(py),
		}
	}

	// Circle center lies inside or on the boundary of b: exit through the nearest face.
	dTop := cy - minY
	dBottom := maxY - cy
	dLeft := cx - minX
	dRight := maxX - cx

	best := dTop
	nx, ny := float32(0), float32(-1)
	cpx, cpy := cx, minY

	if dBottom < best {
		best = dBottom
		nx, ny = 0, 1
		cpx, cpy = cx, maxY
	}
	if dLeft < best {
		best = dLeft
		nx, ny = -1, 0
		cpx, cpy = minX, cy
	}
	if dRight < best {
		best = dRight
		nx, ny = 1, 0
		cpx, cpy = maxX, cy
	}

	return Contact{
		Hit:     true,
		NormalX: nx,
		NormalY: ny,
		Depth:   float32(best + r),
		PointX:  float32(cpx),
		PointY:  float32(cpy),
	}
}

// TestBoxCircle tests whether box a and circle b overlap. The returned Contact normal points from b toward a.
func TestBoxCircle(a Box, b Circle) Contact {
	c := TestCircleBox(b, a)
	if !c.Hit {
		return Contact{}
	}
	if c.NormalX != 0 {
		c.NormalX = -c.NormalX
	}
	if c.NormalY != 0 {
		c.NormalY = -c.NormalY
	}
	return c
}

// TestShapes tests whether shapes a and b overlap. The returned Contact normal points from b toward a, and translating a by (NormalX*Depth, NormalY*Depth) separates them.
//
// Pairs of boxes and circles use TestBoxBox, TestBoxCircle, TestCircleBox and TestCircleCircle. Every pair involving a Polygon, Capsule, Segment or Chain uses the separating axis test on the shapes' cores plus their rounding radii, reporting the contact point between the two surfaces. Segments and chains collide with every shape with area but never with each other; a chain reports its deepest segment contact, using only normals its neighbouring segments allow at interior vertices.
func TestShapes(a, b Shape) Contact {
	switch a.Kind {
	case ShapeBox:
		switch b.Kind {
		case ShapeBox:
			return TestBoxBox(a.Box, b.Box)
		case ShapeCircle:
			return TestBoxCircle(a.Box, b.Circle)
		}
	case ShapeCircle:
		switch b.Kind {
		case ShapeBox:
			return TestCircleBox(a.Circle, b.Box)
		case ShapeCircle:
			return TestCircleCircle(a.Circle, b.Circle)
		}
	}
	return testShapesConvex(a, b)
}
