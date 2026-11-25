package physics3d

import "math"

// Cylinder is a solid round cylinder whose axis runs from the center of its A cap to the center of its B cap, with flat caps of the given Radius (#808). A cylinder whose A and B coincide or whose Radius is not positive has no volume: collisions, raycasts and areas ignore it.
type Cylinder struct {
	A, B   Vec3
	Radius float32
}

// VerticalCylinder returns a Y-aligned cylinder centered at center with the given radius and total height, such as a barrel standing on its end.
func VerticalCylinder(center Vec3, radius, height float32) Cylinder {
	h := float32(max(height, 0) * 0.5)
	return Cylinder{
		A:      Vec3{X: center.X, Y: center.Y - h, Z: center.Z},
		B:      Vec3{X: center.X, Y: center.Y + h, Z: center.Z},
		Radius: radius,
	}
}

// Translated returns a copy of c shifted by offset.
func (c Cylinder) Translated(offset Vec3) Cylinder {
	return Cylinder{A: c.A.Add(offset), B: c.B.Add(offset), Radius: c.Radius}
}

// Center returns the midpoint of c's axis.
func (c Cylinder) Center() Vec3 {
	return c.A.Add(c.B).Scale(0.5)
}

// Height returns the length of c's axis.
func (c Cylinder) Height() float32 {
	return c.B.Sub(c.A).Len()
}

// bounds returns the tight AABB of c: along each world axis, the caps reach Radius times the sine of the angle between that axis and the cylinder's.
func (c Cylinder) bounds() Box {
	d := c.B.Sub(c.A)
	l := d.Len()
	var e Vec3
	if l <= 1e-12 {
		e = Vec3{X: c.Radius, Y: c.Radius, Z: c.Radius}
	} else {
		u := d.Scale(1 / l)
		r := c.Radius
		e = Vec3{
			X: r * sqrt32(max(0, 1-float32(u.X*u.X))),
			Y: r * sqrt32(max(0, 1-float32(u.Y*u.Y))),
			Z: r * sqrt32(max(0, 1-float32(u.Z*u.Z))),
		}
	}
	mn := Vec3{X: min(c.A.X, c.B.X) - e.X, Y: min(c.A.Y, c.B.Y) - e.Y, Z: min(c.A.Z, c.B.Z) - e.Z}
	mx := Vec3{X: max(c.A.X, c.B.X) + e.X, Y: max(c.A.Y, c.B.Y) + e.Y, Z: max(c.A.Z, c.B.Z) + e.Z}
	return BoxFromMinMax(mn, mx)
}

// RaycastCylinder tests ray against cylinder c, its side and both caps. A ray that starts inside c hits at T 0 with Normal facing back along the ray, as for the other shapes.
func RaycastCylinder(ray Ray, c Cylinder) (RaycastHit, bool) {
	if ray.MaxDist <= 0 || c.Radius <= 0 {
		return RaycastHit{}, false
	}
	dir := ray.Dir.Norm()
	if dir.LenSq() == 0 {
		return RaycastHit{}, false
	}
	a, b := toDV(c.A), toDV(c.B)
	axis := b.sub(a)
	h := axis.length()
	if h <= 1e-12 {
		return RaycastHit{}, false
	}
	u := axis.scale(1 / h)
	o, d := toDV(ray.Origin).sub(a), toDV(dir)
	r := float64(c.Radius)

	tEnter, tExit := 0.0, float64(ray.MaxDist)
	var n dv
	entered := false
	// Slab between the cap planes.
	ou, du := o.dot(u), d.dot(u)
	if math.Abs(du) < 1e-15 {
		if ou < 0 || ou > h {
			return RaycastHit{}, false
		}
	} else {
		t0, t1 := -ou/du, (h-ou)/du
		n0 := u.neg()
		if t0 > t1 {
			t0, t1, n0 = t1, t0, u
		}
		if t0 > tEnter {
			tEnter, n, entered = t0, n0, true
		}
		tExit = min(tExit, t1)
	}
	// Infinite cylinder around the axis.
	op, dp := o.sub(u.scale(ou)), d.sub(u.scale(du))
	qa, qb, qc := dp.lenSq(), op.dot(dp), op.lenSq()-float64(r*r)
	if qa < 1e-15 {
		if qc > 0 {
			return RaycastHit{}, false
		}
	} else {
		disc := float64(qb*qb) - float64(qa*qc)
		if disc < 0 {
			return RaycastHit{}, false
		}
		sq := math.Sqrt(disc)
		t0, t1 := (-qb-sq)/qa, (-qb+sq)/qa
		if t0 > tEnter {
			p := op.add(dp.scale(t0))
			tEnter, n, entered = t0, p.norm(), true
		}
		tExit = min(tExit, t1)
	}
	if tEnter > tExit {
		return RaycastHit{}, false
	}
	if !entered {
		return RaycastHit{Point: ray.Origin, Normal: dir.Neg()}, true
	}
	return RaycastHit{
		T:        clamp32(float32(tEnter)/ray.MaxDist, 0, 1),
		Distance: float32(tEnter),
		Point:    ray.Origin.Add(dir.Scale(float32(tEnter))),
		Normal:   n.vec3().Norm(),
	}, true
}

// RaycastConvexHull tests ray against hull h turned by rot about its Center. A ray that starts inside h hits at T 0 with Normal facing back along the ray, as for the other shapes.
func RaycastConvexHull(ray Ray, h ConvexHull, rot Quat) (RaycastHit, bool) {
	if ray.MaxDist <= 0 || h.geom == nil {
		return RaycastHit{}, false
	}
	dir := ray.Dir.Norm()
	if dir.LenSq() == 0 {
		return RaycastHit{}, false
	}
	f := cvx{center: toDV(h.Center), axes: rotAxes(rot)}
	o := f.local(toDV(ray.Origin).sub(f.center))
	d := f.local(toDV(dir))
	t, n, entered, ok := h.geom.raycast(o, d, float64(ray.MaxDist))
	if !ok {
		return RaycastHit{}, false
	}
	if !entered {
		return RaycastHit{Point: ray.Origin, Normal: dir.Neg()}, true
	}
	wn := f.axes[0].scale(n.x).add(f.axes[1].scale(n.y)).add(f.axes[2].scale(n.z))
	return RaycastHit{
		T:        clamp32(float32(t)/ray.MaxDist, 0, 1),
		Distance: float32(t),
		Point:    ray.Origin.Add(dir.Scale(float32(t))),
		Normal:   wn.vec3().Norm(),
	}, true
}

// CylinderEdges returns wireframe line segments for cylinder c: a ring around each cap and four lines along its side. Non-positive radii and zero-length axes return nil; non-positive segments default to 24 (minimum 4).
func CylinderEdges(c Cylinder, segments int) [][2]Vec3 {
	if c.Radius <= 0 {
		return nil
	}
	if segments <= 0 {
		segments = defaultSphereSegments
	} else if segments < 4 {
		segments = 4
	}
	ab := c.B.Sub(c.A)
	l := ab.Len()
	if l <= 1e-6 {
		return nil
	}
	u, v := orthoBasis(ab.Scale(1 / l))
	r := c.Radius
	edges := make([][2]Vec3, 0, 2*segments+4)
	edges = appendCircleEdges(edges, c.A, u, v, r, segments)
	edges = appendCircleEdges(edges, c.B, u, v, r, segments)
	for _, off := range [4]Vec3{u.Scale(r), u.Scale(-r), v.Scale(r), v.Scale(-r)} {
		edges = append(edges, [2]Vec3{c.A.Add(off), c.B.Add(off)})
	}
	return edges
}
