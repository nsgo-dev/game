package physics3d

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

const defaultSphereSegments = 24

// DebugStyle configures the colors, stroke width, and tessellation used by
// physics3d debug drawing functions. Zero-valued fields fall back to
// DefaultDebugStyle().
type DebugStyle struct {
	BoxColor       core.Color
	SphereColor    core.Color
	CapsuleColor   core.Color
	ContactColor   core.Color
	NormalColor    core.Color
	RayColor       core.Color
	LineWidth      float32
	SphereSegments int
	NormalLength   float32
}

// DefaultDebugStyle returns a high-contrast DebugStyle suitable for overlaying
// 3D collider wireframes on a game scene.
func DefaultDebugStyle() DebugStyle {
	return DebugStyle{
		BoxColor:       core.RGB(0x22C55E),
		SphereColor:    core.RGB(0x38BDF8),
		CapsuleColor:   core.RGB(0xFACC15),
		ContactColor:   core.RGB(0xEF4444),
		NormalColor:    core.RGB(0xF97316),
		RayColor:       core.RGB(0xE879F9),
		LineWidth:      1.5,
		SphereSegments: defaultSphereSegments,
		NormalLength:   0.5,
	}
}

// BoxEdges returns the 12 wireframe line segments of b in deterministic order
// (4 on the min-Z face, 4 on the max-Z face, and 4 connecting Z edges).
func BoxEdges(b Box) [12][2]Vec3 {
	mn := b.Min()
	mx := b.Max()
	c000 := Vec3{X: mn.X, Y: mn.Y, Z: mn.Z}
	c100 := Vec3{X: mx.X, Y: mn.Y, Z: mn.Z}
	c110 := Vec3{X: mx.X, Y: mx.Y, Z: mn.Z}
	c010 := Vec3{X: mn.X, Y: mx.Y, Z: mn.Z}
	c001 := Vec3{X: mn.X, Y: mn.Y, Z: mx.Z}
	c101 := Vec3{X: mx.X, Y: mn.Y, Z: mx.Z}
	c111 := Vec3{X: mx.X, Y: mx.Y, Z: mx.Z}
	c011 := Vec3{X: mn.X, Y: mx.Y, Z: mx.Z}
	return [12][2]Vec3{
		{c000, c100},
		{c100, c110},
		{c110, c010},
		{c010, c000},
		{c001, c101},
		{c101, c111},
		{c111, c011},
		{c011, c001},
		{c000, c001},
		{c100, c101},
		{c110, c111},
		{c010, c011},
	}
}

// SphereEdges returns wireframe line segments for three orthogonal great
// circles (XY, XZ, and YZ planes) of sphere s. Non-positive radii return nil;
// non-positive segments default to 24 (minimum 3).
func SphereEdges(s Sphere, segments int) [][2]Vec3 {
	if s.Radius <= 0 {
		return nil
	}
	if segments <= 0 {
		segments = defaultSphereSegments
	} else if segments < 3 {
		segments = 3
	}
	edges := make([][2]Vec3, 0, 3*segments)
	edges = appendCircleEdges(edges, s.Center, Vec3{X: 1}, Vec3{Y: 1}, s.Radius, segments)
	edges = appendCircleEdges(edges, s.Center, Vec3{X: 1}, Vec3{Z: 1}, s.Radius, segments)
	edges = appendCircleEdges(edges, s.Center, Vec3{Y: 1}, Vec3{Z: 1}, s.Radius, segments)
	return edges
}

// CapsuleEdges returns wireframe line segments for capsule c aligned with its
// axis (B - A): two end-cap rings at A and B, four longitudinal rails, and four
// hemisphere arcs capping both ends. Degenerate capsules (A == B) fall back to
// SphereEdges. Non-positive radii return nil; non-positive segments default to
// 24 (minimum 4).
func CapsuleEdges(c Capsule, segments int) [][2]Vec3 {
	if c.Radius <= 0 {
		return nil
	}
	if segments <= 0 {
		segments = defaultSphereSegments
	} else if segments < 4 {
		segments = 4
	}
	ab := c.B.Sub(c.A)
	axisLen := ab.Len()
	if axisLen <= 1e-6 {
		return SphereEdges(Sphere{Center: c.A, Radius: c.Radius}, segments)
	}
	axis := ab.Scale(1 / axisLen)
	u, v := orthoBasis(axis)
	r := c.Radius

	arcSteps := segments / 2
	if arcSteps < 2 {
		arcSteps = 2
	}
	edges := make([][2]Vec3, 0, 2*segments+4+4*arcSteps)

	// Two end-cap rings perpendicular to the capsule axis.
	edges = appendCircleEdges(edges, c.A, u, v, r, segments)
	edges = appendCircleEdges(edges, c.B, u, v, r, segments)

	// Four longitudinal rails connecting the end-cap rings.
	ur := u.Scale(r)
	vr := v.Scale(r)
	edges = append(edges,
		[2]Vec3{c.A.Add(ur), c.B.Add(ur)},
		[2]Vec3{c.A.Sub(ur), c.B.Sub(ur)},
		[2]Vec3{c.A.Add(vr), c.B.Add(vr)},
		[2]Vec3{c.A.Sub(vr), c.B.Sub(vr)},
	)

	// Four hemisphere arcs (two at cap B along +axis, two at cap A along -axis).
	negAxis := axis.Neg()
	edges = appendSemicircleEdges(edges, c.B, u, axis, r, arcSteps)
	edges = appendSemicircleEdges(edges, c.B, v, axis, r, arcSteps)
	edges = appendSemicircleEdges(edges, c.A, u, negAxis, r, arcSteps)
	edges = appendSemicircleEdges(edges, c.A, v, negAxis, r, arcSteps)
	return edges
}

func orthoBasis(axis Vec3) (u, v Vec3) {
	ref := Vec3{Y: 1}
	if abs32(axis.Y) >= 0.9 {
		ref = Vec3{Z: 1}
	}
	u = axis.Cross(ref).Norm()
	v = u.Cross(axis)
	return u, v
}

func appendCircleEdges(dst [][2]Vec3, center, u, v Vec3, radius float32, segments int) [][2]Vec3 {
	step := (2 * math.Pi) / float64(segments)
	prev := center.Add(u.Scale(radius))
	first := prev
	for i := 1; i <= segments; i++ {
		var cur Vec3
		if i == segments {
			cur = first
		} else {
			sn, cs := math.Sincos(step * float64(i))
			cur = center.Add(u.Scale(radius * float32(cs))).Add(v.Scale(radius * float32(sn)))
		}
		dst = append(dst, [2]Vec3{prev, cur})
		prev = cur
	}
	return dst
}

func appendSemicircleEdges(dst [][2]Vec3, center, u, pole Vec3, radius float32, steps int) [][2]Vec3 {
	step := math.Pi / float64(steps)
	prev := center.Add(u.Scale(radius))
	for i := 1; i <= steps; i++ {
		var cur Vec3
		if i == steps {
			cur = center.Sub(u.Scale(radius))
		} else {
			sn, cs := math.Sincos(step * float64(i))
			cur = center.Add(u.Scale(radius * float32(cs))).Add(pole.Scale(radius * float32(sn)))
		}
		dst = append(dst, [2]Vec3{prev, cur})
		prev = cur
	}
	return dst
}
