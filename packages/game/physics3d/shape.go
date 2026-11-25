package physics3d

// Box is an axis-aligned bounding box defined by its center and half-extents.
type Box struct {
	Center      Vec3
	HalfExtents Vec3
}

// BoxFromMinMax returns the axis-aligned Box spanning [min, max].
func BoxFromMinMax(min, max Vec3) Box {
	return Box{
		Center:      min.Add(max).Scale(0.5),
		HalfExtents: max.Sub(min).Scale(0.5),
	}
}

// Min returns the minimum corner of b.
func (b Box) Min() Vec3 {
	return b.Center.Sub(b.HalfExtents)
}

// Max returns the maximum corner of b.
func (b Box) Max() Vec3 {
	return b.Center.Add(b.HalfExtents)
}

// Translated returns a copy of b shifted by offset.
func (b Box) Translated(offset Vec3) Box {
	return Box{Center: b.Center.Add(offset), HalfExtents: b.HalfExtents}
}

// Sphere is a ball defined by its center and radius.
type Sphere struct {
	Center Vec3
	Radius float32
}

// Translated returns a copy of s shifted by offset.
func (s Sphere) Translated(offset Vec3) Sphere {
	return Sphere{Center: s.Center.Add(offset), Radius: s.Radius}
}

// Capsule is a line segment from A to B swept by Radius.
type Capsule struct {
	A, B   Vec3
	Radius float32
}

// VerticalCapsule returns a Y-aligned capsule centered at center with the given
// radius and total height. If height is less than 2*radius, the capsule
// degenerates to a sphere of the given radius at center.
func VerticalCapsule(center Vec3, radius, height float32) Capsule {
	halfSeg := height*0.5 - radius
	if halfSeg < 0 {
		halfSeg = 0
	}
	return Capsule{
		A:      Vec3{X: center.X, Y: center.Y - halfSeg, Z: center.Z},
		B:      Vec3{X: center.X, Y: center.Y + halfSeg, Z: center.Z},
		Radius: radius,
	}
}

// Translated returns a copy of c shifted by offset.
func (c Capsule) Translated(offset Vec3) Capsule {
	return Capsule{A: c.A.Add(offset), B: c.B.Add(offset), Radius: c.Radius}
}

// ShapeKind identifies the active primitive inside a Shape.
type ShapeKind int

const (
	// ShapeBox selects Shape.Box.
	ShapeBox ShapeKind = iota
	// ShapeSphere selects Shape.Sphere.
	ShapeSphere
	// ShapeCapsule selects Shape.Capsule.
	ShapeCapsule
	// ShapeConvexHull selects Shape.Hull.
	ShapeConvexHull
	// ShapeCylinder selects Shape.Cylinder.
	ShapeCylinder
	// ShapeCompound selects Shape.Children.
	ShapeCompound
)

// Shape is a tagged wrapper holding one 3D collision primitive or a compound
// assembly of child shapes.
type Shape struct {
	Kind    ShapeKind
	Box     Box
	Sphere  Sphere
	Capsule Capsule
	// Hull is the convex hull of a ShapeConvexHull shape.
	Hull ConvexHull
	// Cylinder is the cylinder of a ShapeCylinder shape.
	Cylinder Cylinder
	// Rot orients a Box or a ConvexHull about its Center, making the box an oriented box. The zero value and identity leave them unrotated; spheres, capsules and cylinders ignore it.
	Rot Quat
	// Children holds the child colliders when Kind == ShapeCompound.
	Children []CompoundChild `json:"Children,omitempty"`
}

// BoxShape wraps b in a Shape.
func BoxShape(b Box) Shape {
	return Shape{Kind: ShapeBox, Box: b}
}

// SphereShape wraps s in a Shape.
func SphereShape(s Sphere) Shape {
	return Shape{Kind: ShapeSphere, Sphere: s}
}

// CapsuleShape wraps c in a Shape.
func CapsuleShape(c Capsule) Shape {
	return Shape{Kind: ShapeCapsule, Capsule: c}
}

// ConvexHullShape wraps h in a Shape.
func ConvexHullShape(h ConvexHull) Shape {
	return Shape{Kind: ShapeConvexHull, Hull: h}
}

// CylinderShape wraps c in a Shape.
func CylinderShape(c Cylinder) Shape {
	return Shape{Kind: ShapeCylinder, Cylinder: c}
}

// Translated returns a copy of s shifted by offset.
func (s Shape) Translated(offset Vec3) Shape {
	switch s.Kind {
	case ShapeBox:
		return Shape{Kind: ShapeBox, Box: s.Box.Translated(offset), Rot: s.Rot}
	case ShapeSphere:
		return SphereShape(s.Sphere.Translated(offset))
	case ShapeCapsule:
		return CapsuleShape(s.Capsule.Translated(offset))
	case ShapeConvexHull:
		return Shape{Kind: ShapeConvexHull, Hull: s.Hull.Translated(offset), Rot: s.Rot}
	case ShapeCylinder:
		return CylinderShape(s.Cylinder.Translated(offset))
	case ShapeCompound:
		if len(s.Children) == 0 {
			return Shape{Kind: ShapeCompound}
		}
		children := make([]CompoundChild, len(s.Children))
		for i, c := range s.Children {
			c.Shape = c.WorldShape(offset, Quat{})
			c.Offset = Vec3{}
			c.Rot = Quat{}
			children[i] = c
		}
		return Shape{Kind: ShapeCompound, Children: children}
	default:
		return s
	}
}

// Transformed returns a copy of s rotated by rot around the local origin and translated by pos. For identity rotations, it is identical to Translated(pos). A Box or ConvexHull keeps its geometry and gains the rotation in Shape.Rot, so the result is exact rather than the AABB enclosing it; Bounds returns that AABB.
func (s Shape) Transformed(pos Vec3, rot Quat) Shape {
	if rot.IsIdentity() {
		return s.Translated(pos)
	}
	rot = rot.Norm()
	switch s.Kind {
	case ShapeBox:
		own, _ := s.orientedBox()
		combined := rot.Mul(own).Norm()
		if combined.IsIdentity() {
			combined = Quat{}
		}
		return Shape{Kind: ShapeBox, Box: Box{
			Center:      rot.RotateVec(s.Box.Center).Add(pos),
			HalfExtents: s.Box.HalfExtents,
		}, Rot: combined}
	case ShapeSphere:
		return SphereShape(Sphere{
			Center: rot.RotateVec(s.Sphere.Center).Add(pos),
			Radius: s.Sphere.Radius,
		})
	case ShapeCapsule:
		return CapsuleShape(Capsule{
			A:      rot.RotateVec(s.Capsule.A).Add(pos),
			B:      rot.RotateVec(s.Capsule.B).Add(pos),
			Radius: s.Capsule.Radius,
		})
	case ShapeConvexHull:
		combined := rot.Mul(normalizedQuat(s.Rot)).Norm()
		if combined.IsIdentity() {
			combined = Quat{}
		}
		h := s.Hull
		h.Center = rot.RotateVec(h.Center).Add(pos)
		return Shape{Kind: ShapeConvexHull, Hull: h, Rot: combined}
	case ShapeCylinder:
		return CylinderShape(Cylinder{
			A:      rot.RotateVec(s.Cylinder.A).Add(pos),
			B:      rot.RotateVec(s.Cylinder.B).Add(pos),
			Radius: s.Cylinder.Radius,
		})
	case ShapeCompound:
		if len(s.Children) == 0 {
			return Shape{Kind: ShapeCompound}
		}
		children := make([]CompoundChild, len(s.Children))
		for i, c := range s.Children {
			c.Shape = c.WorldShape(pos, rot)
			c.Offset = Vec3{}
			c.Rot = Quat{}
			children[i] = c
		}
		return Shape{Kind: ShapeCompound, Children: children}
	default:
		return s
	}
}

// Bounds returns a tight axis-aligned bounding Box enclosing s.
func (s Shape) Bounds() Box {
	switch s.Kind {
	case ShapeBox:
		if r, ok := s.orientedBox(); ok {
			return orientedBoxBounds(s.Box, r)
		}
		return s.Box
	case ShapeSphere:
		r := s.Sphere.Radius
		return Box{Center: s.Sphere.Center, HalfExtents: Vec3{X: r, Y: r, Z: r}}
	case ShapeCapsule:
		minV := Vec3{
			X: min(s.Capsule.A.X, s.Capsule.B.X) - s.Capsule.Radius,
			Y: min(s.Capsule.A.Y, s.Capsule.B.Y) - s.Capsule.Radius,
			Z: min(s.Capsule.A.Z, s.Capsule.B.Z) - s.Capsule.Radius,
		}
		maxV := Vec3{
			X: max(s.Capsule.A.X, s.Capsule.B.X) + s.Capsule.Radius,
			Y: max(s.Capsule.A.Y, s.Capsule.B.Y) + s.Capsule.Radius,
			Z: max(s.Capsule.A.Z, s.Capsule.B.Z) + s.Capsule.Radius,
		}
		return BoxFromMinMax(minV, maxV)
	case ShapeConvexHull:
		if s.Hull.geom == nil {
			return Box{Center: s.Hull.Center}
		}
		return s.Hull.geom.bounds(s.Hull.Center, s.Rot)
	case ShapeCylinder:
		return s.Cylinder.bounds()
	case ShapeCompound:
		return compoundBounds(s.Children)
	default:
		return Box{}
	}
}

// isConvexKind reports whether s goes through the GJK and EPA narrow phase.
func isConvexKind(s Shape) bool {
	return s.Kind == ShapeConvexHull || s.Kind == ShapeCylinder
}

// Contact describes an overlap between two shapes A and B.
//
// When Hit is true, Normal is a unit vector pointing from B toward A (the
// direction to translate A by Depth to separate it from B), Depth is the
// positive penetration depth, and Point is a representative contact point in
// world space.
type Contact struct {
	Hit    bool
	Normal Vec3
	Depth  float32
	Point  Vec3
}

func (c Contact) flipped() Contact {
	if !c.Hit {
		return c
	}
	c.Normal = c.Normal.Neg()
	return c
}

// TestShapes tests two shapes for overlap. The returned Contact normal points from b toward a. Boxes oriented by Shape.Rot are tested exactly: two boxes with the separating axis theorem, and a box against a sphere or capsule in the box's own frame. Any pair with a convex hull or a cylinder is tested with GJK and EPA, and its Point is the middle of the area where the two shapes touch.
func TestShapes(a, b Shape) Contact {
	if a.Kind == ShapeCompound || b.Kind == ShapeCompound {
		return testCompoundShapes(a, b)
	}
	if isConvexKind(a) || isConvexKind(b) {
		return testConvex(toCvx(a), toCvx(b))
	}
	rotA, orientedA := a.orientedBox()
	rotB, orientedB := b.orientedBox()
	if a.Kind == ShapeBox && b.Kind == ShapeBox && (orientedA || orientedB) {
		return TestOrientedBoxBox(a.Box, rotA, b.Box, rotB)
	}
	if orientedA {
		f := newBoxFrame(a.Box.Center, rotA)
		return f.contactToWorld(TestShapes(BoxShape(Box{HalfExtents: a.Box.HalfExtents}), f.localShape(b)))
	}
	if orientedB {
		return TestShapes(b, a).flipped()
	}
	switch a.Kind {
	case ShapeBox:
		switch b.Kind {
		case ShapeBox:
			return TestBoxBox(a.Box, b.Box)
		case ShapeSphere:
			return TestBoxSphere(a.Box, b.Sphere)
		case ShapeCapsule:
			return TestBoxCapsule(a.Box, b.Capsule)
		}
	case ShapeSphere:
		switch b.Kind {
		case ShapeBox:
			return TestBoxSphere(b.Box, a.Sphere).flipped()
		case ShapeSphere:
			return TestSphereSphere(a.Sphere, b.Sphere)
		case ShapeCapsule:
			return TestSphereCapsule(a.Sphere, b.Capsule)
		}
	case ShapeCapsule:
		switch b.Kind {
		case ShapeBox:
			return TestBoxCapsule(b.Box, a.Capsule).flipped()
		case ShapeSphere:
			return TestSphereCapsule(b.Sphere, a.Capsule).flipped()
		case ShapeCapsule:
			return TestCapsuleCapsule(a.Capsule, b.Capsule)
		}
	}
	return Contact{}
}

// TestBoxBox tests two axis-aligned boxes for overlap.
func TestBoxBox(a, b Box) Contact {
	d := a.Center.Sub(b.Center)
	ox := a.HalfExtents.X + b.HalfExtents.X - abs32(d.X)
	if ox <= 0 {
		return Contact{}
	}
	oy := a.HalfExtents.Y + b.HalfExtents.Y - abs32(d.Y)
	if oy <= 0 {
		return Contact{}
	}
	oz := a.HalfExtents.Z + b.HalfExtents.Z - abs32(d.Z)
	if oz <= 0 {
		return Contact{}
	}

	aMin, aMax := a.Min(), a.Max()
	bMin, bMax := b.Min(), b.Max()
	point := Vec3{
		X: (max(aMin.X, bMin.X) + min(aMax.X, bMax.X)) * 0.5,
		Y: (max(aMin.Y, bMin.Y) + min(aMax.Y, bMax.Y)) * 0.5,
		Z: (max(aMin.Z, bMin.Z) + min(aMax.Z, bMax.Z)) * 0.5,
	}

	// Pick the minimum-penetration axis deterministically.
	depth := ox
	var normal Vec3
	if d.X >= 0 {
		normal = Vec3{X: 1}
	} else {
		normal = Vec3{X: -1}
	}
	if oy < depth {
		depth = oy
		if d.Y >= 0 {
			normal = Vec3{Y: 1}
		} else {
			normal = Vec3{Y: -1}
		}
	}
	if oz < depth {
		depth = oz
		if d.Z >= 0 {
			normal = Vec3{Z: 1}
		} else {
			normal = Vec3{Z: -1}
		}
	}

	return Contact{
		Hit:    true,
		Normal: normal,
		Depth:  depth,
		Point:  point,
	}
}

// TestSphereSphere tests two spheres for overlap.
func TestSphereSphere(a, b Sphere) Contact {
	rSum := a.Radius + b.Radius
	if rSum <= 0 {
		return Contact{}
	}
	d := a.Center.Sub(b.Center)
	distSq := d.LenSq()
	if distSq >= rSum*rSum {
		return Contact{}
	}
	dist := sqrt32(distSq)
	if dist > 1e-6 {
		normal := d.Scale(1 / dist)
		depth := rSum - dist
		point := b.Center.Add(normal.Scale(b.Radius - float32(depth*0.5)))
		return Contact{
			Hit:    true,
			Normal: normal,
			Depth:  depth,
			Point:  point,
		}
	}
	return Contact{
		Hit:    true,
		Normal: Vec3{Y: 1},
		Depth:  rSum,
		Point:  a.Center,
	}
}

// TestCapsuleCapsule tests two capsules for overlap. When the two segments cross, the normal is perpendicular to both of them rather than an arbitrary axis.
func TestCapsuleCapsule(a, b Capsule) Contact {
	pA, pB := closestPointsSegments(a.A, a.B, b.A, b.B)
	c := TestSphereSphere(
		Sphere{Center: pA, Radius: a.Radius},
		Sphere{Center: pB, Radius: b.Radius},
	)
	if c.Hit && pA.Sub(pB).LenSq() <= 1e-12 {
		if n, ok := coincidentNormal(a.B.Sub(a.A), b.B.Sub(b.A), a.A.Add(a.B).Sub(b.A.Add(b.B))); ok {
			c.Normal = n
		}
	}
	return c
}

// coincidentNormal picks a contact normal for two segments (or a point and a segment) whose closest points coincide: perpendicular to both directions, falling back to perpendicular to the one non-degenerate direction. It is signed toward hint (from b toward a); with no usable hint it points to the upper half-space.
func coincidentNormal(dirA, dirB, hint Vec3) (Vec3, bool) {
	n := dirA.Cross(dirB)
	if n.LenSq() <= 1e-12 {
		d := dirA
		if d.LenSq() <= 1e-12 {
			d = dirB
		}
		if d.LenSq() <= 1e-12 {
			return Vec3{}, false
		}
		d = d.Norm()
		// Remove the axis component from the hint, or from +Y (then +X) when the hint runs along the axis.
		for _, cand := range [3]Vec3{hint, {Y: 1}, {X: 1}} {
			p := cand.Sub(d.Scale(cand.Dot(d)))
			if p.LenSq() > 1e-10 {
				return p.Norm(), true
			}
		}
		return Vec3{}, false
	}
	n = n.Norm()
	s := n.Dot(hint)
	if s < 0 || (s == 0 && (n.Y < 0 || (n.Y == 0 && (n.X < 0 || (n.X == 0 && n.Z < 0))))) {
		n = n.Neg()
	}
	return n, true
}

// TestBoxSphere tests an axis-aligned box a against a sphere b.
// The returned Contact normal points from b toward a.
func TestBoxSphere(a Box, b Sphere) Contact {
	if b.Radius <= 0 {
		return Contact{}
	}
	aMin, aMax := a.Min(), a.Max()
	closest := Vec3{
		X: clamp32(b.Center.X, aMin.X, aMax.X),
		Y: clamp32(b.Center.Y, aMin.Y, aMax.Y),
		Z: clamp32(b.Center.Z, aMin.Z, aMax.Z),
	}
	d := closest.Sub(b.Center)
	distSq := d.LenSq()
	if distSq > 0 {
		if distSq >= b.Radius*b.Radius {
			return Contact{}
		}
		dist := sqrt32(distSq)
		return Contact{
			Hit:    true,
			Normal: d.Scale(1 / dist),
			Depth:  b.Radius - dist,
			Point:  closest,
		}
	}

	// Sphere center is inside or on the boundary of the box: push sphere out
	// through the nearest face, so Normal (B -> A) points inward from that face.
	dxPos := aMax.X - b.Center.X
	dxNeg := b.Center.X - aMin.X
	dyPos := aMax.Y - b.Center.Y
	dyNeg := b.Center.Y - aMin.Y
	dzPos := aMax.Z - b.Center.Z
	dzNeg := b.Center.Z - aMin.Z

	minFace := dxPos
	normal := Vec3{X: -1}
	point := Vec3{X: aMax.X, Y: b.Center.Y, Z: b.Center.Z}

	if dxNeg < minFace {
		minFace = dxNeg
		normal = Vec3{X: 1}
		point = Vec3{X: aMin.X, Y: b.Center.Y, Z: b.Center.Z}
	}
	if dyPos < minFace {
		minFace = dyPos
		normal = Vec3{Y: -1}
		point = Vec3{X: b.Center.X, Y: aMax.Y, Z: b.Center.Z}
	}
	if dyNeg < minFace {
		minFace = dyNeg
		normal = Vec3{Y: 1}
		point = Vec3{X: b.Center.X, Y: aMin.Y, Z: b.Center.Z}
	}
	if dzPos < minFace {
		minFace = dzPos
		normal = Vec3{Z: -1}
		point = Vec3{X: b.Center.X, Y: b.Center.Y, Z: aMax.Z}
	}
	if dzNeg < minFace {
		minFace = dzNeg
		normal = Vec3{Z: 1}
		point = Vec3{X: b.Center.X, Y: b.Center.Y, Z: aMin.Z}
	}

	// On an exact box edge or corner (multiple faces tied at distance 0), combine
	// the tied face normals so the normal stays continuous with the exterior Voronoi
	// region of that edge or corner.
	if minFace <= 1e-6 {
		var combined Vec3
		if dxPos <= 1e-6 {
			combined.X -= 1
		}
		if dxNeg <= 1e-6 {
			combined.X += 1
		}
		if dyPos <= 1e-6 {
			combined.Y -= 1
		}
		if dyNeg <= 1e-6 {
			combined.Y += 1
		}
		if dzPos <= 1e-6 {
			combined.Z -= 1
		}
		if dzNeg <= 1e-6 {
			combined.Z += 1
		}
		if combined.LenSq() > 0 {
			normal = combined.Norm()
		}
	}

	return Contact{
		Hit:    true,
		Normal: normal,
		Depth:  b.Radius + minFace,
		Point:  point,
	}
}

// TestSphereCapsule tests a sphere a against a capsule b.
// The returned Contact normal points from b toward a. A sphere centered on the capsule's segment is pushed out perpendicular to the segment.
func TestSphereCapsule(a Sphere, b Capsule) Contact {
	pB := closestPointOnSegment(b.A, b.B, a.Center)
	c := TestSphereSphere(a, Sphere{Center: pB, Radius: b.Radius})
	if c.Hit && a.Center.Sub(pB).LenSq() <= 1e-12 {
		if n, ok := coincidentNormal(b.B.Sub(b.A), Vec3{}, Vec3{}); ok {
			c.Normal = n
		}
	}
	return c
}

// TestBoxCapsule tests an axis-aligned box a against a capsule b.
// The returned Contact normal points from b toward a. When the capsule's segment passes through the box, the normal and depth come from the separating axis theorem over the box faces and the edges crossed with the segment, so moving a by Normal*Depth separates the whole capsule rather than one sphere on it.
func TestBoxCapsule(a Box, b Capsule) Contact {
	if b.Radius <= 0 {
		return Contact{}
	}
	pB := closestOrDeepestPointSegmentBox(b.A, b.B, a)
	c := TestBoxSphere(a, Sphere{Center: pB, Radius: b.Radius})
	if !c.Hit || pointBoxDistSq(pB, a.Min(), a.Max()) > 0 {
		return c
	}
	// The segment enters the box: one sphere at its deepest point understates how far the rest of the capsule reaches.
	seg := b.B.Sub(b.A)
	axes := [3]Vec3{{X: 1}, {Y: 1}, {Z: 1}}
	bestDepth := float32(-1)
	var bestN Vec3
	try := func(l Vec3, face bool) {
		ls := l.LenSq()
		if ls < 1e-10 {
			return
		}
		l = l.Scale(1 / sqrt32(ls))
		bc := a.Center.Dot(l)
		br := float32(float32(a.HalfExtents.X*abs32(l.X))+float32(a.HalfExtents.Y*abs32(l.Y))) + float32(a.HalfExtents.Z*abs32(l.Z))
		pa, pb := b.A.Dot(l), b.B.Dot(l)
		cMin, cMax := min(pa, pb)-b.Radius, max(pa, pb)+b.Radius
		// Box moving along +l must clear the capsule's top; along -l its bottom.
		up := cMax - (bc - br)
		down := (bc + br) - cMin
		depth, n := up, l
		if down < up {
			depth, n = down, l.Neg()
		}
		if !face {
			depth = float32(depth*1.05) + 1e-5 // prefer face axes on near ties, as the box-box test does
		}
		if bestDepth < 0 || depth < bestDepth {
			bestDepth, bestN = depth, n
		}
	}
	for _, ax := range axes {
		try(ax, true)
	}
	for _, ax := range axes {
		try(seg.Cross(ax), false)
	}
	if bestDepth <= 0 {
		return c
	}
	c.Normal = bestN
	c.Depth = bestDepth
	return c
}

func aabbOverlap(a, b Box) bool {
	return abs32(a.Center.X-b.Center.X) <= a.HalfExtents.X+b.HalfExtents.X &&
		abs32(a.Center.Y-b.Center.Y) <= a.HalfExtents.Y+b.HalfExtents.Y &&
		abs32(a.Center.Z-b.Center.Z) <= a.HalfExtents.Z+b.HalfExtents.Z
}

// closestPointOnSegment returns the closest point on segment [a, b] to p.
func closestPointOnSegment(a, b, p Vec3) Vec3 {
	ab := b.Sub(a)
	lenSq := ab.LenSq()
	if lenSq <= 1e-12 {
		return a
	}
	t := clamp32(p.Sub(a).Dot(ab)/lenSq, 0, 1)
	return a.Add(ab.Scale(t))
}

// closestPointsSegments returns closest points (c1, c2) on segments [p1, q1]
// and [p2, q2], handling degenerate and parallel cases.
func closestPointsSegments(p1, q1, p2, q2 Vec3) (Vec3, Vec3) {
	const eps = 1e-12
	d1 := q1.Sub(p1)
	d2 := q2.Sub(p2)
	r := p1.Sub(p2)
	a := d1.LenSq()
	e := d2.LenSq()
	f := d2.Dot(r)

	var s, t float32
	if a <= eps && e <= eps {
		return p1, p2
	}
	if a <= eps {
		s = 0
		t = clamp32(f/e, 0, 1)
	} else {
		c := d1.Dot(r)
		if e <= eps {
			t = 0
			s = clamp32(-c/a, 0, 1)
		} else {
			b := d1.Dot(d2)
			denom := float32(a*e) - float32(b*b)
			if denom > eps {
				s = clamp32((float32(b*f)-float32(c*e))/denom, 0, 1)
			} else {
				// Parallel segments: choose the midpoint of overlapping projections
				// onto segment 1 so symmetric parallel capsules have a centered contact.
				t0 := clamp32(-c/a, 0, 1)
				t1 := clamp32((b-c)/a, 0, 1)
				s = (t0 + t1) * 0.5
			}
			t = (float32(b*s) + f) / e
			if t < 0 {
				t = 0
				s = clamp32(-c/a, 0, 1)
			} else if t > 1 {
				t = 1
				s = clamp32((b-c)/a, 0, 1)
			}
		}
	}
	return p1.Add(d1.Scale(s)), p2.Add(d2.Scale(t))
}

// closestOrDeepestPointSegmentBox returns the point on segment [segA, segB]
// closest to box (when the segment stays outside box) or deepest inside box
// (when the segment enters box).
func closestOrDeepestPointSegmentBox(segA, segB Vec3, box Box) Vec3 {
	d := segB.Sub(segA)
	if d.LenSq() <= 1e-12 {
		return segA
	}
	bMin, bMax := box.Min(), box.Max()

	// Clip segment [0, 1] against the 3 box slabs to see if it intersects the box.
	tEnter, tExit := float32(0), float32(1)
	if clipSlab(segA.X, d.X, bMin.X, bMax.X, &tEnter, &tExit) &&
		clipSlab(segA.Y, d.Y, bMin.Y, bMax.Y, &tEnter, &tExit) &&
		clipSlab(segA.Z, d.Z, bMin.Z, bMax.Z, &tEnter, &tExit) {
		return deepestPointInBox(segA, d, bMin, bMax, tEnter, tExit)
	}

	// Segment stays outside the box: distSq(S(t), box) is convex and piecewise
	// quadratic with breakpoints only where S(t) crosses the 6 slab planes.
	var ts [8]float32
	n := 2
	ts[0] = 0
	ts[1] = 1
	addBreak := func(orig, dir, plane float32) {
		if abs32(dir) <= 1e-8 {
			return
		}
		t := (plane - orig) / dir
		if t > 0 && t < 1 {
			ts[n] = t
			n++
		}
	}
	addBreak(segA.X, d.X, bMin.X)
	addBreak(segA.X, d.X, bMax.X)
	addBreak(segA.Y, d.Y, bMin.Y)
	addBreak(segA.Y, d.Y, bMax.Y)
	addBreak(segA.Z, d.Z, bMin.Z)
	addBreak(segA.Z, d.Z, bMax.Z)

	// Insertion sort the small breakpoint list.
	for i := 1; i < n; i++ {
		v := ts[i]
		j := i - 1
		for j >= 0 && ts[j] > v {
			ts[j+1] = ts[j]
			j--
		}
		ts[j+1] = v
	}

	bestT := float32(0)
	bestDistSq := pointBoxDistSq(segA, bMin, bMax)
	eval := func(t float32) {
		p := segA.Add(d.Scale(t))
		ds := pointBoxDistSq(p, bMin, bMax)
		if ds < bestDistSq {
			bestDistSq = ds
			bestT = t
		}
	}
	eval(1)

	for i := 0; i < n-1; i++ {
		t0, t1 := ts[i], ts[i+1]
		if t1-t0 <= 1e-7 {
			continue
		}
		tm := (t0 + t1) * 0.5
		pm := segA.Add(d.Scale(tm))

		var qa, qb float32
		if pm.X < bMin.X {
			qa += float32(d.X * d.X)
			qb += float32(d.X * (segA.X - bMin.X))
		} else if pm.X > bMax.X {
			qa += float32(d.X * d.X)
			qb += float32(d.X * (segA.X - bMax.X))
		}
		if pm.Y < bMin.Y {
			qa += float32(d.Y * d.Y)
			qb += float32(d.Y * (segA.Y - bMin.Y))
		} else if pm.Y > bMax.Y {
			qa += float32(d.Y * d.Y)
			qb += float32(d.Y * (segA.Y - bMax.Y))
		}
		if pm.Z < bMin.Z {
			qa += float32(d.Z * d.Z)
			qb += float32(d.Z * (segA.Z - bMin.Z))
		} else if pm.Z > bMax.Z {
			qa += float32(d.Z * d.Z)
			qb += float32(d.Z * (segA.Z - bMax.Z))
		}

		if qa > 1e-12 {
			eval(clamp32(-qb/qa, t0, t1))
		} else {
			// Distance is constant on [t0, t1]; prefer the midpoint for symmetry.
			p := segA.Add(d.Scale(tm))
			ds := pointBoxDistSq(p, bMin, bMax)
			if ds <= bestDistSq+1e-7 {
				bestDistSq = ds
				bestT = tm
			}
		}
	}

	return segA.Add(d.Scale(bestT))
}

func clipSlab(orig, dir, lo, hi float32, tEnter, tExit *float32) bool {
	if abs32(dir) <= 1e-8 {
		return orig >= lo && orig <= hi
	}
	inv := 1 / dir
	t0 := (lo - orig) * inv
	t1 := (hi - orig) * inv
	if t0 > t1 {
		t0, t1 = t1, t0
	}
	if t0 > *tEnter {
		*tEnter = t0
	}
	if t1 < *tExit {
		*tExit = t1
	}
	return *tEnter <= *tExit
}

// deepestPointInBox maximizes the minimum distance to the 6 box faces over
// t in [tEnter, tExit] for S(t) = segA + d*t.
func deepestPointInBox(segA, d, bMin, bMax Vec3, tEnter, tExit float32) Vec3 {
	// Six linear face-distance functions: f_i(t) = c[i] + m[i]*t.
	c := [6]float32{
		segA.X - bMin.X, bMax.X - segA.X,
		segA.Y - bMin.Y, bMax.Y - segA.Y,
		segA.Z - bMin.Z, bMax.Z - segA.Z,
	}
	m := [6]float32{d.X, -d.X, d.Y, -d.Y, d.Z, -d.Z}

	minFace := func(t float32) float32 {
		v := c[0] + float32(m[0]*t)
		for i := 1; i < 6; i++ {
			if fi := c[i] + float32(m[i]*t); fi < v {
				v = fi
			}
		}
		return v
	}

	var cands [20]float32
	nc := 0
	addCand := func(t float32) {
		if t >= tEnter && t <= tExit && nc < len(cands) {
			cands[nc] = t
			nc++
		}
	}
	addCand(tEnter)
	addCand(tExit)
	addCand((tEnter + tExit) * 0.5)
	for i := 0; i < 6; i++ {
		for j := i + 1; j < 6; j++ {
			dm := m[i] - m[j]
			if abs32(dm) > 1e-8 {
				addCand((c[j] - c[i]) / dm)
			}
		}
	}

	bestVal := float32(-1)
	bestLo, bestHi := tEnter, tEnter
	for i := 0; i < nc; i++ {
		t := cands[i]
		v := minFace(t)
		if v > bestVal+1e-6 {
			bestVal = v
			bestLo, bestHi = t, t
		} else if abs32(v-bestVal) <= 1e-6 {
			if t < bestLo {
				bestLo = t
			}
			if t > bestHi {
				bestHi = t
			}
		}
	}
	return segA.Add(d.Scale((bestLo + bestHi) * 0.5))
}

func pointBoxDistSq(p, bMin, bMax Vec3) float32 {
	var ds float32
	if p.X < bMin.X {
		dx := bMin.X - p.X
		ds += float32(dx * dx)
	} else if p.X > bMax.X {
		dx := p.X - bMax.X
		ds += float32(dx * dx)
	}
	if p.Y < bMin.Y {
		dy := bMin.Y - p.Y
		ds += float32(dy * dy)
	} else if p.Y > bMax.Y {
		dy := p.Y - bMax.Y
		ds += float32(dy * dy)
	}
	if p.Z < bMin.Z {
		dz := bMin.Z - p.Z
		ds += float32(dz * dz)
	} else if p.Z > bMax.Z {
		dz := p.Z - bMax.Z
		ds += float32(dz * dz)
	}
	return ds
}
