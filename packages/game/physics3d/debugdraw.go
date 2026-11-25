//go:build !release

package physics3d

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

func resolveDebugStyle(s DebugStyle) DebugStyle {
	def := DefaultDebugStyle()
	if s.BoxColor == 0 {
		s.BoxColor = def.BoxColor
	}
	if s.SphereColor == 0 {
		s.SphereColor = def.SphereColor
	}
	if s.CapsuleColor == 0 {
		s.CapsuleColor = def.CapsuleColor
	}
	if s.HullColor == 0 {
		s.HullColor = def.HullColor
	}
	if s.CylinderColor == 0 {
		s.CylinderColor = def.CylinderColor
	}
	if s.ContactColor == 0 {
		s.ContactColor = def.ContactColor
	}
	if s.NormalColor == 0 {
		s.NormalColor = def.NormalColor
	}
	if s.RayColor == 0 {
		s.RayColor = def.RayColor
	}
	if s.LineWidth <= 0 {
		s.LineWidth = def.LineWidth
	}
	if s.SphereSegments <= 0 {
		s.SphereSegments = def.SphereSegments
	}
	if s.NormalLength <= 0 {
		s.NormalLength = def.NormalLength
	}
	return s
}

// clipSegment4D clips the 3D segment [a, b] in homogeneous clip space against all six frustum planes (near, far, left, right, bottom, top) so segments crossing behind the camera clip cleanly to the near plane without inverting or producing NaN/Inf coordinates. The near plane is clip z >= 0 when depthZO is set ([0, 1] depth) and z >= -w otherwise ([-1, 1] depth).
func clipSegment4D(viewProj Mat4, viewport core.Bounds, a, b Vec3, depthZO bool) (core.Point, core.Point, bool) {
	if viewport.Size.Width <= 0 || viewport.Size.Height <= 0 {
		return core.Point{}, core.Point{}, false
	}
	ca := viewProj.mulPoint(a)
	cb := viewProj.mulPoint(b)
	if !isFinite32(ca.x) || !isFinite32(ca.y) || !isFinite32(ca.z) || !isFinite32(ca.w) ||
		!isFinite32(cb.x) || !isFinite32(cb.y) || !isFinite32(cb.z) || !isFinite32(cb.w) {
		return core.Point{}, core.Point{}, false
	}

	const eps = 1e-6
	t0 := float32(0)
	t1 := float32(1)

	clipPlane := func(da, db float32) bool {
		if da < -eps && db < -eps {
			return false
		}
		if da < 0 && db >= 0 {
			denom := da - db
			if denom == 0 {
				return false
			}
			t := da / denom
			if t > t0 {
				t0 = t
			}
		} else if da >= 0 && db < 0 {
			denom := da - db
			if denom == 0 {
				return false
			}
			t := da / denom
			if t < t1 {
				t1 = t
			}
		}
		return t0 <= t1
	}

	// Clip against near (z >= -w, or z >= 0 for [0, 1] depth), far (z <= w), left (x >= -w), right (x <= w), bottom (y >= -w), and top (y <= w).
	nearA, nearB := ca.z+ca.w, cb.z+cb.w
	if depthZO {
		nearA, nearB = ca.z, cb.z
	}
	if !clipPlane(nearA, nearB) ||
		!clipPlane(ca.w-ca.z, cb.w-cb.z) ||
		!clipPlane(ca.x+ca.w, cb.x+cb.w) ||
		!clipPlane(ca.w-ca.x, cb.w-cb.x) ||
		!clipPlane(ca.y+ca.w, cb.y+cb.w) ||
		!clipPlane(ca.w-ca.y, cb.w-cb.y) {
		return core.Point{}, core.Point{}, false
	}

	lerp4 := func(t float32) vec4 {
		return vec4{
			x: ca.x + (cb.x-ca.x)*t,
			y: ca.y + (cb.y-ca.y)*t,
			z: ca.z + (cb.z-ca.z)*t,
			w: ca.w + (cb.w-ca.w)*t,
		}
	}

	p0 := lerp4(t0)
	p1 := lerp4(t1)
	if p0.w <= 1e-6 || p1.w <= 1e-6 {
		return core.Point{}, core.Point{}, false
	}
	invW0 := 1 / p0.w
	invW1 := 1 / p1.w
	s0 := ndcToViewport(p0.x*invW0, p0.y*invW0, viewport)
	s1 := ndcToViewport(p1.x*invW1, p1.y*invW1, viewport)
	if !isFinite32(float32(s0.X)) || !isFinite32(float32(s0.Y)) ||
		!isFinite32(float32(s1.X)) || !isFinite32(float32(s1.Y)) {
		return core.Point{}, core.Point{}, false
	}
	return s0, s1, true
}

// DrawLine3D projects and strokes a 3D line segment [a, b] onto d using viewProj and viewport, clipping cleanly against the camera frustum and near plane. It leaves d's graphics state unchanged.
//
// The near plane is clip z >= -w, right for a [-1, 1] depth projection such as [Mat4Perspective] or [Mat4Ortho]. Use [DrawLine3DZO] for a [0, 1] projection such as math3d.Perspective or math3d.Ortho.
func DrawLine3D(d *core.Drawing, viewProj Mat4, viewport core.Bounds, a, b Vec3, col core.Color, lineWidth float32) {
	drawLine3D(d, viewProj, viewport, a, b, col, lineWidth, false)
}

// DrawLine3DZO is [DrawLine3D] for a [0, 1] depth projection such as math3d.Perspective, math3d.Ortho or a render3d camera: it clips at clip z >= 0, as render3d.ProjectSegment does. DrawLine3D used with a [0, 1] orthographic projection would draw segments behind the camera, because w is 1 there everywhere (#546).
func DrawLine3DZO(d *core.Drawing, viewProj Mat4, viewport core.Bounds, a, b Vec3, col core.Color, lineWidth float32) {
	drawLine3D(d, viewProj, viewport, a, b, col, lineWidth, true)
}

func drawLine3D(d *core.Drawing, viewProj Mat4, viewport core.Bounds, a, b Vec3, col core.Color, lineWidth float32, depthZO bool) {
	if d == nil {
		return
	}
	p0, p1, ok := clipSegment4D(viewProj, viewport, a, b, depthZO)
	if !ok {
		return
	}
	if lineWidth <= 0 {
		lineWidth = 1
	}
	d.Save()
	d.SetStrokeColor(col)
	d.SetLineWidth(lineWidth)
	d.BeginPath()
	d.MoveTo(float32(p0.X), float32(p0.Y))
	d.LineTo(float32(p1.X), float32(p1.Y))
	d.Stroke()
	d.Restore()
}

func drawEdges(d *core.Drawing, viewProj Mat4, viewport core.Bounds, edges [][2]Vec3, col core.Color, lineWidth float32, depthZO bool) {
	if d == nil || len(edges) == 0 {
		return
	}
	if lineWidth <= 0 {
		lineWidth = 1
	}
	started := false
	for _, e := range edges {
		p0, p1, ok := clipSegment4D(viewProj, viewport, e[0], e[1], depthZO)
		if !ok {
			continue
		}
		if !started {
			d.Save()
			d.SetStrokeColor(col)
			d.SetLineWidth(lineWidth)
			d.BeginPath()
			started = true
		}
		d.MoveTo(float32(p0.X), float32(p0.Y))
		d.LineTo(float32(p1.X), float32(p1.Y))
	}
	if started {
		d.Stroke()
		d.Restore()
	}
}

// DrawShape draws a 3D wireframe of s projected into viewport onto d.
func DrawShape(d *core.Drawing, viewProj Mat4, viewport core.Bounds, s Shape, style DebugStyle) {
	if d == nil {
		return
	}
	st := resolveDebugStyle(style)
	switch s.Kind {
	case ShapeBox:
		edges := BoxEdges(s.Box)
		if r, ok := s.orientedBox(); ok {
			edges = OrientedBoxEdges(s.Box, r)
		}
		drawEdges(d, viewProj, viewport, edges[:], st.BoxColor, st.LineWidth, st.DepthZO)
	case ShapeSphere:
		drawEdges(d, viewProj, viewport, SphereEdges(s.Sphere, st.SphereSegments), st.SphereColor, st.LineWidth, st.DepthZO)
	case ShapeCapsule:
		drawEdges(d, viewProj, viewport, CapsuleEdges(s.Capsule, st.SphereSegments), st.CapsuleColor, st.LineWidth, st.DepthZO)
	case ShapeConvexHull:
		drawEdges(d, viewProj, viewport, ConvexHullEdges(s.Hull, s.Rot), st.HullColor, st.LineWidth, st.DepthZO)
	case ShapeCylinder:
		drawEdges(d, viewProj, viewport, CylinderEdges(s.Cylinder, st.SphereSegments), st.CylinderColor, st.LineWidth, st.DepthZO)
	}
}

// DrawBody draws the world-space collider wireframe of b onto d.
func DrawBody(d *core.Drawing, viewProj Mat4, viewport core.Bounds, b *Body, style DebugStyle) {
	if d == nil || b == nil {
		return
	}
	DrawShape(d, viewProj, viewport, b.WorldShape(), style)
}

// DrawCharacter draws the kinematic capsule wireframe of c onto d, along with
// its ground normal when grounded.
func DrawCharacter(d *core.Drawing, viewProj Mat4, viewport core.Bounds, c *CharacterController, style DebugStyle) {
	if d == nil || c == nil {
		return
	}
	st := resolveDebugStyle(style)
	DrawShape(d, viewProj, viewport, c.Shape(), st)
	if c.Grounded && c.GroundNormal.LenSq() > 0 {
		tip := c.Pos.Add(c.GroundNormal.Scale(st.NormalLength))
		drawLine3D(d, viewProj, viewport, c.Pos, tip, st.NormalColor, st.LineWidth, st.DepthZO)
	}
}

// DrawContact draws a contact point marker and its surface normal arrow onto d.
// Non-hitting contacts are ignored.
func DrawContact(d *core.Drawing, viewProj Mat4, viewport core.Bounds, c Contact, style DebugStyle) {
	if d == nil || !c.Hit {
		return
	}
	st := resolveDebugStyle(style)
	if pt, ndcZ, ok := Project(viewProj, viewport, c.Point); ok && (!st.DepthZO || ndcZ >= 0) && pointInBounds(viewport, pt) {
		r := st.LineWidth * 2
		if r < 3 {
			r = 3
		}
		d.Save()
		d.SetFillColor(st.ContactColor)
		d.BeginPath()
		d.Arc(float32(pt.X), float32(pt.Y), r, 0, float32(2*math.Pi), false)
		d.Fill()
		d.Restore()
	}
	if c.Normal.LenSq() > 0 {
		tip := c.Point.Add(c.Normal.Scale(st.NormalLength))
		drawLine3D(d, viewProj, viewport, c.Point, tip, st.NormalColor, st.LineWidth, st.DepthZO)
	}
}

func pointInBounds(b core.Bounds, p core.Point) bool {
	return p.X >= b.Origin.X && p.Y >= b.Origin.Y &&
		p.X <= b.Origin.X+b.Size.Width && p.Y <= b.Origin.Y+b.Size.Height
}

// DrawWorld draws the wireframes of every body in w in deterministic order,
// followed by contact markers and normals for any currently overlapping pairs.
func DrawWorld(d *core.Drawing, viewProj Mat4, viewport core.Bounds, w *World, style DebugStyle) {
	if d == nil || w == nil {
		return
	}
	st := resolveDebugStyle(style)
	bodies := w.Bodies()
	n := len(bodies)
	if n == 0 {
		return
	}
	shapes := make([]Shape, n)
	bounds := make([]Box, n)
	for i, b := range bodies {
		shapes[i] = b.WorldShape()
		bounds[i] = shapes[i].Bounds()
		DrawShape(d, viewProj, viewport, shapes[i], st)
	}
	for i := 0; i < n; i++ {
		a := bodies[i]
		for j := i + 1; j < n; j++ {
			b := bodies[j]
			if a.Static && b.Static {
				continue
			}
			if !aabbOverlap(bounds[i], bounds[j]) {
				continue
			}
			if c := TestShapes(shapes[i], shapes[j]); c.Hit {
				DrawContact(d, viewProj, viewport, c, st)
			}
		}
	}
}

// DrawRaycast draws a 3D ray segment onto d. When hitOK is true, the ray is
// drawn from ray.Origin to hit.Point and the hit point and outward normal are
// drawn as a contact; otherwise the full ray up to ray.MaxDist is drawn.
func DrawRaycast(d *core.Drawing, viewProj Mat4, viewport core.Bounds, ray Ray, hit RaycastHit, hitOK bool, style DebugStyle) {
	if d == nil || ray.MaxDist <= 0 {
		return
	}
	dir := ray.Dir.Norm()
	if dir.LenSq() == 0 {
		return
	}
	st := resolveDebugStyle(style)
	end := ray.Origin.Add(dir.Scale(ray.MaxDist))
	if hitOK {
		end = hit.Point
	}
	drawLine3D(d, viewProj, viewport, ray.Origin, end, st.RayColor, st.LineWidth, st.DepthZO)
	if hitOK {
		DrawContact(d, viewProj, viewport, Contact{
			Hit:    true,
			Point:  hit.Point,
			Normal: hit.Normal,
		}, st)
	}
}
