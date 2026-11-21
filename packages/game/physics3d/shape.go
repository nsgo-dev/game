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
)

// Shape is a tagged wrapper holding one 3D collision primitive.
type Shape struct {
	Kind    ShapeKind
	Box     Box
	Sphere  Sphere
	Capsule Capsule
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

// Translated returns a copy of s shifted by offset.
func (s Shape) Translated(offset Vec3) Shape {
	switch s.Kind {
	case ShapeBox:
		return BoxShape(s.Box.Translated(offset))
	case ShapeSphere:
		return SphereShape(s.Sphere.Translated(offset))
	case ShapeCapsule:
		return CapsuleShape(s.Capsule.Translated(offset))
	default:
		return s
	}
}

// Transformed returns a copy of s rotated by rot around the local origin and
// translated by pos. For identity rotations, it is identical to Translated(pos).
// For axis-aligned Box shapes, a non-identity rotation computes the tight
// world-space AABB enclosing the rotated box.
func (s Shape) Transformed(pos Vec3, rot Quat) Shape {
	if rot.IsIdentity() {
		return s.Translated(pos)
	}
	switch s.Kind {
	case ShapeBox:
		ex := rot.RotateVec(Vec3{X: s.Box.HalfExtents.X})
		ey := rot.RotateVec(Vec3{Y: s.Box.HalfExtents.Y})
		ez := rot.RotateVec(Vec3{Z: s.Box.HalfExtents.Z})
		he := Vec3{
			X: abs32(ex.X) + abs32(ey.X) + abs32(ez.X),
			Y: abs32(ex.Y) + abs32(ey.Y) + abs32(ez.Y),
			Z: abs32(ex.Z) + abs32(ey.Z) + abs32(ez.Z),
		}
		return BoxShape(Box{
			Center:      rot.RotateVec(s.Box.Center).Add(pos),
			HalfExtents: he,
		})
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
	default:
		return s
	}
}

// Bounds returns a tight axis-aligned bounding Box enclosing s.
func (s Shape) Bounds() Box {
	switch s.Kind {
	case ShapeBox:
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
	default:
		return Box{}
	}
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

// TestShapes tests two shapes for overlap. The returned Contact normal points
// from b toward a.
func TestShapes(a, b Shape) Contact {
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
		point := b.Center.Add(normal.Scale(b.Radius - depth*0.5))
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

// TestCapsuleCapsule tests two capsules for overlap.
func TestCapsuleCapsule(a, b Capsule) Contact {
	pA, pB := closestPointsSegments(a.A, a.B, b.A, b.B)
	return TestSphereSphere(
		Sphere{Center: pA, Radius: a.Radius},
		Sphere{Center: pB, Radius: b.Radius},
	)
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
// The returned Contact normal points from b toward a.
func TestSphereCapsule(a Sphere, b Capsule) Contact {
	pB := closestPointOnSegment(b.A, b.B, a.Center)
	return TestSphereSphere(a, Sphere{Center: pB, Radius: b.Radius})
}

// TestBoxCapsule tests an axis-aligned box a against a capsule b.
// The returned Contact normal points from b toward a.
func TestBoxCapsule(a Box, b Capsule) Contact {
	if b.Radius <= 0 {
		return Contact{}
	}
	pB := closestOrDeepestPointSegmentBox(b.A, b.B, a)
	return TestBoxSphere(a, Sphere{Center: pB, Radius: b.Radius})
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
			denom := a*e - b*b
			if denom > eps {
				s = clamp32((b*f-c*e)/denom, 0, 1)
			} else {
				// Parallel segments: choose the midpoint of overlapping projections
				// onto segment 1 so symmetric parallel capsules have a centered contact.
				t0 := clamp32(-c/a, 0, 1)
				t1 := clamp32((b-c)/a, 0, 1)
				s = (t0 + t1) * 0.5
			}
			t = (b*s + f) / e
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
			qa += d.X * d.X
			qb += d.X * (segA.X - bMin.X)
		} else if pm.X > bMax.X {
			qa += d.X * d.X
			qb += d.X * (segA.X - bMax.X)
		}
		if pm.Y < bMin.Y {
			qa += d.Y * d.Y
			qb += d.Y * (segA.Y - bMin.Y)
		} else if pm.Y > bMax.Y {
			qa += d.Y * d.Y
			qb += d.Y * (segA.Y - bMax.Y)
		}
		if pm.Z < bMin.Z {
			qa += d.Z * d.Z
			qb += d.Z * (segA.Z - bMin.Z)
		} else if pm.Z > bMax.Z {
			qa += d.Z * d.Z
			qb += d.Z * (segA.Z - bMax.Z)
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
		v := c[0] + m[0]*t
		for i := 1; i < 6; i++ {
			if fi := c[i] + m[i]*t; fi < v {
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
		ds += dx * dx
	} else if p.X > bMax.X {
		dx := p.X - bMax.X
		ds += dx * dx
	}
	if p.Y < bMin.Y {
		dy := bMin.Y - p.Y
		ds += dy * dy
	} else if p.Y > bMax.Y {
		dy := p.Y - bMax.Y
		ds += dy * dy
	}
	if p.Z < bMin.Z {
		dz := bMin.Z - p.Z
		ds += dz * dz
	} else if p.Z > bMax.Z {
		dz := p.Z - bMax.Z
		ds += dz * dz
	}
	return ds
}
