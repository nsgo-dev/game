package physics3d

// Oriented boxes (#546). A Box is axis-aligned on its own; a Shape whose Kind is ShapeBox may carry a rotation in Shape.Rot about the box's Center, which turns it into an oriented box. Narrow-phase tests, raycasts and debug drawing handle the orientation exactly, so the corners of a rotated body are no longer solid the way its tight world AABB was.

// orientedBox reports the unit rotation of a box shape and whether it differs from identity.
func (s Shape) orientedBox() (Quat, bool) {
	if s.Kind != ShapeBox || s.Rot.IsIdentity() {
		return QuatIdentity(), false
	}
	return s.Rot.Norm(), true
}

// boxFrame is the rigid frame of an oriented box: world = rot * local + origin.
type boxFrame struct {
	origin Vec3
	rot    Quat
	inv    Quat
}

func newBoxFrame(center Vec3, rot Quat) boxFrame {
	return boxFrame{origin: center, rot: rot, inv: rot.Conjugate()}
}

func (f boxFrame) toLocal(p Vec3) Vec3    { return f.inv.RotateVec(p.Sub(f.origin)) }
func (f boxFrame) toWorld(p Vec3) Vec3    { return f.rot.RotateVec(p).Add(f.origin) }
func (f boxFrame) dirToWorld(v Vec3) Vec3 { return f.rot.RotateVec(v) }

// localShape expresses s in f's frame.
func (f boxFrame) localShape(s Shape) Shape {
	switch s.Kind {
	case ShapeBox:
		r, _ := s.orientedBox()
		return Shape{Kind: ShapeBox, Box: Box{Center: f.toLocal(s.Box.Center), HalfExtents: s.Box.HalfExtents}, Rot: f.inv.Mul(r).Norm()}
	case ShapeSphere:
		return SphereShape(Sphere{Center: f.toLocal(s.Sphere.Center), Radius: s.Sphere.Radius})
	case ShapeCapsule:
		return CapsuleShape(Capsule{A: f.toLocal(s.Capsule.A), B: f.toLocal(s.Capsule.B), Radius: s.Capsule.Radius})
	case ShapeConvexHull:
		h := s.Hull
		h.Center = f.toLocal(h.Center)
		return Shape{Kind: ShapeConvexHull, Hull: h, Rot: f.inv.Mul(normalizedQuat(s.Rot)).Norm()}
	case ShapeCylinder:
		return CylinderShape(Cylinder{A: f.toLocal(s.Cylinder.A), B: f.toLocal(s.Cylinder.B), Radius: s.Cylinder.Radius})
	default:
		return s
	}
}

// contactToWorld maps a contact computed in f's frame back to world space.
func (f boxFrame) contactToWorld(c Contact) Contact {
	if !c.Hit {
		return c
	}
	c.Normal = f.dirToWorld(c.Normal)
	c.Point = f.toWorld(c.Point)
	return c
}

// boxAxes returns the world-space unit axes of an oriented box.
func boxAxes(rot Quat) [3]Vec3 {
	return [3]Vec3{rot.RotateVec(Vec3{X: 1}), rot.RotateVec(Vec3{Y: 1}), rot.RotateVec(Vec3{Z: 1})}
}

func vecComp(v Vec3, i int) float32 {
	switch i {
	case 0:
		return v.X
	case 1:
		return v.Y
	default:
		return v.Z
	}
}

// boxCorners returns the 8 world-space corners of a box with the given center, axes and half-extents.
func boxCorners(c Vec3, axes [3]Vec3, e Vec3) [8]Vec3 {
	var out [8]Vec3
	for i := range out {
		sx, sy, sz := float32(-1), float32(-1), float32(-1)
		if i&1 != 0 {
			sx = 1
		}
		if i&2 != 0 {
			sy = 1
		}
		if i&4 != 0 {
			sz = 1
		}
		out[i] = c.Add(axes[0].Scale(sx * e.X)).Add(axes[1].Scale(sy * e.Y)).Add(axes[2].Scale(sz * e.Z))
	}
	return out
}

// TestOrientedBoxBox tests two boxes, each rotated by its rotation about its own Center, with the separating axis theorem over the 15 face and edge axes. The returned Contact normal points from b toward a; Point is the average of the corners of either box that lie inside the other, so a box resting flat on another gets a point under its center and a tilted one gets its penetrating corner.
func TestOrientedBoxBox(a Box, rotA Quat, b Box, rotB Quat) Contact {
	rotA, rotB = normalizedQuat(rotA), normalizedQuat(rotB)
	ax, bx := boxAxes(rotA), boxAxes(rotB)
	ea, eb := a.HalfExtents, b.HalfExtents
	t := b.Center.Sub(a.Center)

	project := func(axes [3]Vec3, e Vec3, l Vec3) float32 {
		return float32(float32(e.X*abs32(axes[0].Dot(l)))+float32(e.Y*abs32(axes[1].Dot(l)))) + float32(e.Z*abs32(axes[2].Dot(l)))
	}

	bestDepth := float32(-1)
	var bestAxis Vec3
	bestEdge := -1
	test := func(l Vec3, edge int) bool {
		lenSq := l.LenSq()
		if lenSq < 1e-10 {
			return true // parallel edges: the face axes already cover this direction
		}
		l = l.Scale(1 / sqrt32(lenSq))
		overlap := project(ax, ea, l) + project(bx, eb, l) - abs32(t.Dot(l))
		if overlap <= 0 {
			return false
		}
		// Prefer face axes: an edge axis must be clearly shallower to win, which keeps resting contacts stable.
		if bestDepth < 0 || (edge < 0 && overlap < bestDepth) || (edge >= 0 && overlap < float32(bestDepth*0.95)-1e-5) {
			bestDepth, bestAxis, bestEdge = overlap, l, edge
		}
		return true
	}
	for i := range 3 {
		if !test(ax[i], -1) {
			return Contact{}
		}
	}
	for i := range 3 {
		if !test(bx[i], -1) {
			return Contact{}
		}
	}
	for i := range 3 {
		for j := range 3 {
			if !test(ax[i].Cross(bx[j]), i*3+j) {
				return Contact{}
			}
		}
	}

	// Normal from b toward a.
	n := bestAxis
	if n.Dot(t) > 0 {
		n = n.Neg()
	}

	point, ok := cornersInside(a.Center, ax, ea, b.Center, bx, eb)
	if !ok {
		if bestEdge >= 0 {
			i, j := bestEdge/3, bestEdge%3
			pa0, pa1 := supportEdge(a.Center, ax, ea, i, n.Neg())
			pb0, pb1 := supportEdge(b.Center, bx, eb, j, n)
			ca, cb := closestPointsSegments(pa0, pa1, pb0, pb1)
			point = ca.Add(cb).Scale(0.5)
		} else {
			point = a.Center.Add(b.Center).Scale(0.5)
		}
	}
	return Contact{Hit: true, Normal: n, Depth: bestDepth, Point: point}
}

// cornersInside averages the corners of each box that lie inside (or within a small tolerance of) the other one.
func cornersInside(ca Vec3, ax [3]Vec3, ea Vec3, cb Vec3, bx [3]Vec3, eb Vec3) (Vec3, bool) {
	tol := float32(1e-3 * (1 + max(ea.X, ea.Y, ea.Z, eb.X, eb.Y, eb.Z)))
	inside := func(p, c Vec3, axes [3]Vec3, e Vec3) bool {
		d := p.Sub(c)
		for k := range 3 {
			if abs32(d.Dot(axes[k])) > vecComp(e, k)+tol {
				return false
			}
		}
		return true
	}
	var sum Vec3
	n := 0
	for _, p := range boxCorners(ca, ax, ea) {
		if inside(p, cb, bx, eb) {
			sum = sum.Add(p)
			n++
		}
	}
	for _, p := range boxCorners(cb, bx, eb) {
		if inside(p, ca, ax, ea) {
			sum = sum.Add(p)
			n++
		}
	}
	if n == 0 {
		return Vec3{}, false
	}
	return sum.Scale(1 / float32(n)), true
}

// supportEdge returns the box edge parallel to axis i that lies furthest along dir.
func supportEdge(c Vec3, axes [3]Vec3, e Vec3, i int, dir Vec3) (Vec3, Vec3) {
	mid := c
	for k := range 3 {
		if k == i {
			continue
		}
		s := float32(1)
		if axes[k].Dot(dir) < 0 {
			s = -1
		}
		mid = mid.Add(axes[k].Scale(s * vecComp(e, k)))
	}
	half := axes[i].Scale(vecComp(e, i))
	return mid.Sub(half), mid.Add(half)
}

// orientedBoxBounds returns the tight world AABB of a box rotated by rot about its center.
func orientedBoxBounds(b Box, rot Quat) Box {
	ax := boxAxes(rot)
	e := b.HalfExtents
	he := Vec3{
		X: float32(float32(abs32(ax[0].X)*e.X)+float32(abs32(ax[1].X)*e.Y)) + float32(abs32(ax[2].X)*e.Z),
		Y: float32(float32(abs32(ax[0].Y)*e.X)+float32(abs32(ax[1].Y)*e.Y)) + float32(abs32(ax[2].Y)*e.Z),
		Z: float32(float32(abs32(ax[0].Z)*e.X)+float32(abs32(ax[1].Z)*e.Y)) + float32(abs32(ax[2].Z)*e.Z),
	}
	return Box{Center: b.Center, HalfExtents: he}
}

// OrientedBoxEdges returns the 12 world-space edges of b rotated by rot about its Center.
func OrientedBoxEdges(b Box, rot Quat) [12][2]Vec3 {
	edges := BoxEdges(Box{HalfExtents: b.HalfExtents})
	rot = normalizedQuat(rot)
	for i := range edges {
		for k := range 2 {
			edges[i][k] = rot.RotateVec(edges[i][k]).Add(b.Center)
		}
	}
	return edges
}
