package render3d

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
)

// GizmoMode selects the transformation tool represented by a [Gizmo] (#287).
type GizmoMode uint8

const (
	GizmoTranslate GizmoMode = iota
	GizmoRotate
	GizmoScale
)

// GizmoAxis identifies a picked or active handle on a [Gizmo] (#287).
type GizmoAxis uint8

const (
	AxisNone GizmoAxis = iota
	AxisX
	AxisY
	AxisZ
	AxisXY
	AxisXZ
	AxisYZ
	AxisAll
)

// Standard 3D viewport gizmo colors.
var (
	gizmoColorX      = core.RGB(0xef4444)
	gizmoColorY      = core.RGB(0x22c55e)
	gizmoColorZ      = core.RGB(0x3b82f6)
	gizmoColorXY     = core.RGB(0xeab308)
	gizmoColorXZ     = core.RGB(0xd946ef)
	gizmoColorYZ     = core.RGB(0x06b6d4)
	gizmoColorAll    = core.RGB(0xffffff)
	gizmoColorActive = core.RGB(0xfacc15)
)

// GizmoSegment is one colored 3D wireframe edge belonging to a [GizmoAxis].
type GizmoSegment struct {
	A, B  math3d.Vec3
	Axis  GizmoAxis
	Color core.Color
}

// Gizmo generates 3D translate/rotate/scale wireframe handles, performs ray
// picking against axes and rings, computes constrained drag deltas with
// optional snapping, and renders into [*core.Drawing] (#287).
//
// The zero value is ready to use with sensible defaults.
type Gizmo struct {
	// RingSegments controls rotation ring tessellation (default 32).
	RingSegments int
	// PickThreshold is the ray-hit tolerance as a fraction of scale (default 0.12).
	PickThreshold float32
	// LineWidth is the stroke width in points for inactive axes (default 2.0).
	LineWidth float32
	// ActiveLineWidth is the stroke width in points for the active axis (default 3.5).
	ActiveLineWidth float32
	// DepthNO clips at the near plane of an OpenGL-style [-1, 1] depth projection, as [ProjectSegmentNO] does. By default handles clip for [0, 1] depth, matching [math3d.Perspective] and [math3d.Ortho].
	DepthNO bool
}

func (g Gizmo) ringSegments() int {
	if g.RingSegments >= 8 {
		return g.RingSegments
	}
	return 32
}

func (g Gizmo) pickThreshold() float32 {
	if g.PickThreshold > 0 {
		return g.PickThreshold
	}
	return 0.12
}

func (g Gizmo) lineWidth() float32 {
	if g.LineWidth > 0 {
		return g.LineWidth
	}
	return 2.0
}

func (g Gizmo) activeLineWidth() float32 {
	if g.ActiveLineWidth > 0 {
		return g.ActiveLineWidth
	}
	return 3.5
}

// Segments generates the 3D wireframe line segments for a gizmo at origin
// scaled by scale in mode. It allocates a new slice; use [Gizmo.AppendSegments] to reuse one.
func (g Gizmo) Segments(origin math3d.Vec3, scale float32, mode GizmoMode) []GizmoSegment {
	return g.AppendSegments(nil, origin, scale, mode)
}

// AppendSegments appends the segments [Gizmo.Segments] would return to dst and returns the extended slice, allocating nothing when dst has room.
func (g Gizmo) AppendSegments(dst []GizmoSegment, origin math3d.Vec3, scale float32, mode GizmoMode) []GizmoSegment {
	if scale <= 0 {
		scale = 1
	}
	switch mode {
	case GizmoRotate:
		return g.rotateSegments(dst, origin, scale)
	case GizmoScale:
		return g.scaleSegments(dst, origin, scale)
	default:
		return g.translateSegments(dst, origin, scale)
	}
}

// Planar translate handles are the squares [planarMin, planarMax]^2 * scale in each axis plane; drawing and picking share these bounds so a click lands where the handle is drawn (#546).
const (
	planarMin = 0.05
	planarMax = 0.32
)

// gizmoSegmentBuf is enough stack room for the default gizmo in every mode, so drawing and picking it allocate nothing.
const gizmoSegmentBuf = 3 * 32

func (g Gizmo) translateSegments(segs []GizmoSegment, origin math3d.Vec3, scale float32) []GizmoSegment {
	axes := [3]struct {
		axis  GizmoAxis
		dir   math3d.Vec3
		perp1 math3d.Vec3
		perp2 math3d.Vec3
		color core.Color
	}{
		{AxisX, math3d.Vec3UnitX(), math3d.Vec3UnitY(), math3d.Vec3UnitZ(), gizmoColorX},
		{AxisY, math3d.Vec3UnitY(), math3d.Vec3UnitZ(), math3d.Vec3UnitX(), gizmoColorY},
		{AxisZ, math3d.Vec3UnitZ(), math3d.Vec3UnitX(), math3d.Vec3UnitY(), gizmoColorZ},
	}

	headBase := 0.82 * scale
	headRad := 0.08 * scale
	for _, a := range axes {
		tip := origin.Add(a.dir.Scale(scale))
		base := origin.Add(a.dir.Scale(headBase))
		segs = append(segs, GizmoSegment{A: origin, B: tip, Axis: a.axis, Color: a.color})
		segs = append(segs,
			GizmoSegment{A: tip, B: base.Add(a.perp1.Scale(headRad)), Axis: a.axis, Color: a.color},
			GizmoSegment{A: tip, B: base.Sub(a.perp1.Scale(headRad)), Axis: a.axis, Color: a.color},
			GizmoSegment{A: tip, B: base.Add(a.perp2.Scale(headRad)), Axis: a.axis, Color: a.color},
			GizmoSegment{A: tip, B: base.Sub(a.perp2.Scale(headRad)), Axis: a.axis, Color: a.color},
		)
	}

	// Planar translate handles: the outline of the square that pickTranslate tests.
	lo, hi := planarMin*scale, planarMax*scale
	planes := [3]struct {
		axis  GizmoAxis
		u, v  math3d.Vec3
		color core.Color
	}{
		{AxisXY, math3d.Vec3UnitX(), math3d.Vec3UnitY(), gizmoColorXY},
		{AxisXZ, math3d.Vec3UnitX(), math3d.Vec3UnitZ(), gizmoColorXZ},
		{AxisYZ, math3d.Vec3UnitY(), math3d.Vec3UnitZ(), gizmoColorYZ},
	}
	for _, pl := range planes {
		corner := func(a, b float32) math3d.Vec3 { return origin.Add(pl.u.Scale(a)).Add(pl.v.Scale(b)) }
		c0, c1, c2, c3 := corner(lo, lo), corner(hi, lo), corner(hi, hi), corner(lo, hi)
		segs = append(segs,
			GizmoSegment{A: c0, B: c1, Axis: pl.axis, Color: pl.color},
			GizmoSegment{A: c1, B: c2, Axis: pl.axis, Color: pl.color},
			GizmoSegment{A: c2, B: c3, Axis: pl.axis, Color: pl.color},
			GizmoSegment{A: c3, B: c0, Axis: pl.axis, Color: pl.color},
		)
	}
	return segs
}

func (g Gizmo) rotateSegments(segs []GizmoSegment, origin math3d.Vec3, scale float32) []GizmoSegment {
	n := g.ringSegments()
	step := (2 * math.Pi) / float64(n)
	for i := range n {
		a0 := float64(i) * step
		a1 := float64(i+1) * step
		c0, s0 := float32(math.Cos(a0))*scale, float32(math.Sin(a0))*scale
		c1, s1 := float32(math.Cos(a1))*scale, float32(math.Sin(a1))*scale

		// AxisX ring lies in YZ plane.
		segs = append(segs, GizmoSegment{
			A:     origin.Add(math3d.Vec3{Y: c0, Z: s0}),
			B:     origin.Add(math3d.Vec3{Y: c1, Z: s1}),
			Axis:  AxisX,
			Color: gizmoColorX,
		})
		// AxisY ring lies in XZ plane.
		segs = append(segs, GizmoSegment{
			A:     origin.Add(math3d.Vec3{X: c0, Z: s0}),
			B:     origin.Add(math3d.Vec3{X: c1, Z: s1}),
			Axis:  AxisY,
			Color: gizmoColorY,
		})
		// AxisZ ring lies in XY plane.
		segs = append(segs, GizmoSegment{
			A:     origin.Add(math3d.Vec3{X: c0, Y: s0}),
			B:     origin.Add(math3d.Vec3{X: c1, Y: s1}),
			Axis:  AxisZ,
			Color: gizmoColorZ,
		})
	}
	return segs
}

func (g Gizmo) scaleSegments(segs []GizmoSegment, origin math3d.Vec3, scale float32) []GizmoSegment {
	axes := [3]struct {
		axis  GizmoAxis
		dir   math3d.Vec3
		color core.Color
	}{
		{AxisX, math3d.Vec3UnitX(), gizmoColorX},
		{AxisY, math3d.Vec3UnitY(), gizmoColorY},
		{AxisZ, math3d.Vec3UnitZ(), gizmoColorZ},
	}

	tipHalf := 0.065 * scale
	for _, a := range axes {
		tip := origin.Add(a.dir.Scale(scale))
		segs = append(segs, GizmoSegment{A: origin, B: tip, Axis: a.axis, Color: a.color})
		segs = appendCubeSegments(segs, tip, tipHalf, a.axis, a.color)
	}
	// Uniform scale cube handle at origin.
	segs = appendCubeSegments(segs, origin, 0.09*scale, AxisAll, gizmoColorAll)
	return segs
}

func appendCubeSegments(segs []GizmoSegment, center math3d.Vec3, half float32, axis GizmoAxis, color core.Color) []GizmoSegment {
	c := [8]math3d.Vec3{
		{X: center.X - half, Y: center.Y - half, Z: center.Z - half},
		{X: center.X + half, Y: center.Y - half, Z: center.Z - half},
		{X: center.X + half, Y: center.Y + half, Z: center.Z - half},
		{X: center.X - half, Y: center.Y + half, Z: center.Z - half},
		{X: center.X - half, Y: center.Y - half, Z: center.Z + half},
		{X: center.X + half, Y: center.Y - half, Z: center.Z + half},
		{X: center.X + half, Y: center.Y + half, Z: center.Z + half},
		{X: center.X - half, Y: center.Y + half, Z: center.Z + half},
	}
	edges := [12][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
		{4, 5}, {5, 6}, {6, 7}, {7, 4},
		{0, 4}, {1, 5}, {2, 6}, {3, 7},
	}
	for _, e := range edges {
		segs = append(segs, GizmoSegment{A: c[e[0]], B: c[e[1]], Axis: axis, Color: color})
	}
	return segs
}

// PickAxis tests ray against the gizmo handles at origin with size scale in
// mode and returns the picked [GizmoAxis] and the hit ray parameter t >= 0.
// If no handle lies within the pick threshold, it returns (AxisNone, 0).
func (g Gizmo) PickAxis(ray math3d.Ray, origin math3d.Vec3, scale float32, mode GizmoMode) (GizmoAxis, float32) {
	if ray.Dir.LenSq() == 0 {
		return AxisNone, 0
	}
	if scale <= 0 {
		scale = 1
	}
	nRay := math3d.Ray{Origin: ray.Origin, Dir: ray.Dir.Norm()}
	tol := g.pickThreshold() * scale

	switch mode {
	case GizmoRotate:
		return g.pickRotate(nRay, origin, scale, tol)
	case GizmoScale:
		return g.pickScale(nRay, origin, scale, tol)
	default:
		return g.pickTranslate(nRay, origin, scale, tol)
	}
}

func (g Gizmo) pickTranslate(ray math3d.Ray, origin math3d.Vec3, scale, tol float32) (GizmoAxis, float32) {
	bestAxis := AxisNone
	bestDist := float32(math.Inf(1))
	bestT := float32(math.Inf(1))

	// Test primary axis shafts first.
	axes := [3]struct {
		axis GizmoAxis
		dir  math3d.Vec3
	}{
		{AxisX, math3d.Vec3UnitX()},
		{AxisY, math3d.Vec3UnitY()},
		{AxisZ, math3d.Vec3UnitZ()},
	}
	for _, a := range axes {
		d, t := raySegmentDistance(ray, origin, origin.Add(a.dir.Scale(scale)))
		if d <= tol && (d < bestDist-1e-5 || (math3d.ApproxEqual(d, bestDist, 1e-5) && t < bestT)) {
			bestAxis = a.axis
			bestDist = d
			bestT = t
		}
	}
	if bestAxis != AxisNone {
		return bestAxis, bestT
	}

	// Test the planar translate handles, the squares translateSegments draws.
	minP := planarMin * scale
	maxP := planarMax * scale
	planes := [3]struct {
		axis   GizmoAxis
		normal math3d.Vec3
		u, v   math3d.Vec3
	}{
		{AxisXY, math3d.Vec3UnitZ(), math3d.Vec3UnitX(), math3d.Vec3UnitY()},
		{AxisXZ, math3d.Vec3UnitY(), math3d.Vec3UnitX(), math3d.Vec3UnitZ()},
		{AxisYZ, math3d.Vec3UnitX(), math3d.Vec3UnitY(), math3d.Vec3UnitZ()},
	}
	for _, p := range planes {
		t, hit := ray.IntersectPlane(math3d.PlaneFromPointNormal(origin, p.normal))
		if !hit || t >= bestT {
			continue
		}
		local := ray.At(t).Sub(origin)
		lu := local.Dot(p.u)
		lv := local.Dot(p.v)
		if lu >= minP && lu <= maxP && lv >= minP && lv <= maxP {
			bestAxis = p.axis
			bestT = t
		}
	}
	if bestAxis == AxisNone {
		return AxisNone, 0
	}
	return bestAxis, bestT
}

func (g Gizmo) pickScale(ray math3d.Ray, origin math3d.Vec3, scale, tol float32) (GizmoAxis, float32) {
	// Center uniform-scale handle takes priority when ray hits close to origin.
	centerSphere := math3d.Sphere{Center: origin, Radius: 0.14 * scale}
	if t, hit := ray.IntersectSphere(centerSphere); hit {
		return AxisAll, t
	}

	bestAxis := AxisNone
	bestDist := float32(math.Inf(1))
	bestT := float32(math.Inf(1))

	axes := [3]struct {
		axis GizmoAxis
		dir  math3d.Vec3
	}{
		{AxisX, math3d.Vec3UnitX()},
		{AxisY, math3d.Vec3UnitY()},
		{AxisZ, math3d.Vec3UnitZ()},
	}
	for _, a := range axes {
		d, t := raySegmentDistance(ray, origin, origin.Add(a.dir.Scale(scale)))
		if d <= tol && (d < bestDist-1e-5 || (math3d.ApproxEqual(d, bestDist, 1e-5) && t < bestT)) {
			bestAxis = a.axis
			bestDist = d
			bestT = t
		}
	}
	if bestAxis == AxisNone {
		return AxisNone, 0
	}
	return bestAxis, bestT
}

func (g Gizmo) pickRotate(ray math3d.Ray, origin math3d.Vec3, scale, tol float32) (GizmoAxis, float32) {
	bestAxis := AxisNone
	bestDist := float32(math.Inf(1))
	bestT := float32(math.Inf(1))

	rings := [3]struct {
		axis   GizmoAxis
		normal math3d.Vec3
	}{
		{AxisX, math3d.Vec3UnitX()},
		{AxisY, math3d.Vec3UnitY()},
		{AxisZ, math3d.Vec3UnitZ()},
	}
	for _, r := range rings {
		t, hit := ray.IntersectPlane(math3d.PlaneFromPointNormal(origin, r.normal))
		if !hit {
			continue
		}
		p := ray.At(t)
		radialErr := float32(math.Abs(float64(p.Dist(origin) - scale)))
		if radialErr <= tol && (radialErr < bestDist-1e-5 || (math3d.ApproxEqual(radialErr, bestDist, 1e-5) && t < bestT)) {
			bestAxis = r.axis
			bestDist = radialErr
			bestT = t
		}
	}
	if bestAxis != AxisNone {
		return bestAxis, bestT
	}

	// Fallback for rays nearly parallel (edge-on) to a ring plane: test against
	// the tessellated ring segments directly.
	var buf [gizmoSegmentBuf]GizmoSegment
	for _, seg := range g.rotateSegments(buf[:0], origin, scale) {
		d, t := raySegmentDistance(ray, seg.A, seg.B)
		if d <= tol && (d < bestDist-1e-5 || (math3d.ApproxEqual(d, bestDist, 1e-5) && t < bestT)) {
			bestAxis = seg.Axis
			bestDist = d
			bestT = t
		}
	}
	if bestAxis == AxisNone {
		return AxisNone, 0
	}
	return bestAxis, bestT
}

// DragTranslate computes the exact constrained 3D world-space translation delta
// along axis from startRay to currentRay relative to origin.
// When snap > 0, each active delta component is rounded to the nearest multiple
// of snap.
func (g Gizmo) DragTranslate(origin math3d.Vec3, axis GizmoAxis, startRay, currentRay math3d.Ray, snap float32) math3d.Vec3 {
	if startRay.Dir.LenSq() == 0 || currentRay.Dir.LenSq() == 0 {
		return math3d.Vec3Zero()
	}
	r0 := math3d.Ray{Origin: startRay.Origin, Dir: startRay.Dir.Norm()}
	r1 := math3d.Ray{Origin: currentRay.Origin, Dir: currentRay.Dir.Norm()}

	switch axis {
	case AxisX, AxisY, AxisZ:
		u := axisDirection(axis)
		s0, ok0 := closestParamOnLine(origin, u, r0)
		s1, ok1 := closestParamOnLine(origin, u, r1)
		if !ok0 || !ok1 {
			return math3d.Vec3Zero()
		}
		delta := snapFloat(s1-s0, snap)
		return u.Scale(delta)

	case AxisXY, AxisXZ, AxisYZ:
		n := planeNormalForAxis(axis)
		pl := math3d.PlaneFromPointNormal(origin, n)
		t0, ok0 := r0.IntersectPlane(pl)
		t1, ok1 := r1.IntersectPlane(pl)
		if !ok0 || !ok1 {
			return math3d.Vec3Zero()
		}
		delta := r1.At(t1).Sub(r0.At(t0))
		delta = delta.Sub(n.Scale(delta.Dot(n)))
		return snapVec3ForAxis(delta, axis, snap)

	case AxisAll:
		pl := math3d.PlaneFromPointNormal(origin, r0.Dir.Neg())
		t0, ok0 := r0.IntersectPlane(pl)
		t1, ok1 := r1.IntersectPlane(pl)
		if !ok0 || !ok1 {
			return math3d.Vec3Zero()
		}
		delta := r1.At(t1).Sub(r0.At(t0))
		return snapVec3ForAxis(delta, AxisAll, snap)

	default:
		return math3d.Vec3Zero()
	}
}

// DragRotate computes the signed right-handed rotation angle in radians around
// axis (AxisX, AxisY, or AxisZ) from startRay to currentRay relative to origin.
// When snap > 0, the angle is rounded to the nearest multiple of snap radians.
func (g Gizmo) DragRotate(origin math3d.Vec3, axis GizmoAxis, startRay, currentRay math3d.Ray, snap float32) float32 {
	if startRay.Dir.LenSq() == 0 || currentRay.Dir.LenSq() == 0 {
		return 0
	}
	u := axisDirection(axis)
	if u.LenSq() == 0 {
		return 0
	}
	r0 := math3d.Ray{Origin: startRay.Origin, Dir: startRay.Dir.Norm()}
	r1 := math3d.Ray{Origin: currentRay.Origin, Dir: currentRay.Dir.Norm()}

	pl := math3d.PlaneFromPointNormal(origin, u)
	t0, ok0 := r0.IntersectPlane(pl)
	t1, ok1 := r1.IntersectPlane(pl)
	if !ok0 || !ok1 {
		return 0
	}

	v0 := r0.At(t0).Sub(origin)
	v1 := r1.At(t1).Sub(origin)
	if v0.LenSq() < 1e-8 || v1.LenSq() < 1e-8 {
		return 0
	}
	v0 = v0.Norm()
	v1 = v1.Norm()

	sinA := u.Dot(v0.Cross(v1))
	cosA := v0.Dot(v1)
	angle := float32(math.Atan2(float64(sinA), float64(cosA)))
	return snapFloat(angle, snap)
}

// DragRotateQuat is a convenience wrapper around [Gizmo.DragRotate] returning
// the rotation delta as a unit [math3d.Quat].
func (g Gizmo) DragRotateQuat(origin math3d.Vec3, axis GizmoAxis, startRay, currentRay math3d.Ray, snap float32) math3d.Quat {
	u := axisDirection(axis)
	if u.LenSq() == 0 {
		return math3d.QuatIdentity()
	}
	return math3d.QuatFromAxisAngle(u, g.DragRotate(origin, axis, startRay, currentRay, snap))
}

// DragScale computes the constrained additive 3D scale delta along axis from
// startRay to currentRay relative to origin (zero when the rays match; apply
// as startScale.Add(delta)).
//
// For single axes (AxisX/AxisY/AxisZ) and planes (AxisXY/AxisXZ/AxisYZ), only
// the active components vary. For AxisAll, all three components receive the
// same uniform delta. When snap > 0, active components are rounded to the
// nearest multiple of snap.
func (g Gizmo) DragScale(origin math3d.Vec3, axis GizmoAxis, startRay, currentRay math3d.Ray, snap float32) math3d.Vec3 {
	if startRay.Dir.LenSq() == 0 || currentRay.Dir.LenSq() == 0 {
		return math3d.Vec3Zero()
	}
	switch axis {
	case AxisX, AxisY, AxisZ, AxisXY, AxisXZ, AxisYZ:
		return g.DragTranslate(origin, axis, startRay, currentRay, snap)
	case AxisAll:
		r0 := math3d.Ray{Origin: startRay.Origin, Dir: startRay.Dir.Norm()}
		r1 := math3d.Ray{Origin: currentRay.Origin, Dir: currentRay.Dir.Norm()}
		normal := r0.Dir.Neg()
		pl := math3d.PlaneFromPointNormal(origin, normal)
		t0, ok0 := r0.IntersectPlane(pl)
		t1, ok1 := r1.IntersectPlane(pl)
		if !ok0 || !ok1 {
			return math3d.Vec3Zero()
		}
		p0 := r0.At(t0)
		p1 := r1.At(t1)
		d0 := p0.Dist(origin)
		var uniform float32
		if d0 > 1e-4 {
			uniform = p1.Dist(origin) - d0
		} else {
			// Start ray hits right at origin: project motion onto a stable
			// view-plane right axis so dragging right increases uniform scale.
			up := math3d.Vec3Up()
			if math.Abs(float64(normal.Dot(up))) > 0.95 {
				up = math3d.Vec3UnitZ()
			}
			right := up.Cross(normal).Norm()
			uniform = p1.Sub(p0).Dot(right)
		}
		uniform = snapFloat(uniform, snap)
		return math3d.Vec3{X: uniform, Y: uniform, Z: uniform}
	default:
		return math3d.Vec3Zero()
	}
}

// DrawGizmo projects and draws a 3D gizmo into d using default [Gizmo] settings,
// clipping segments against the near plane and highlighting activeAxis on top.
func DrawGizmo(d *core.Drawing, viewProj math3d.Mat4, viewport core.Bounds, origin math3d.Vec3, scale float32, mode GizmoMode, activeAxis GizmoAxis) {
	(Gizmo{}).DrawGizmo(d, viewProj, viewport, origin, scale, mode, activeAxis)
}

// DrawGizmo projects and draws g's 3D wireframe handles into d with near-plane
// clipping, drawing activeAxis segments highlighted and thicker on top.
func (g Gizmo) DrawGizmo(d *core.Drawing, viewProj math3d.Mat4, viewport core.Bounds, origin math3d.Vec3, scale float32, mode GizmoMode, activeAxis GizmoAxis) {
	if d == nil || viewport.Size.Width <= 0 || viewport.Size.Height <= 0 {
		return
	}
	var buf [gizmoSegmentBuf]GizmoSegment
	segs := g.AppendSegments(buf[:0], origin, scale, mode)
	if len(segs) == 0 {
		return
	}

	d.Save()
	d.SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)

	drawPass := func(wantActive bool) {
		var (
			open     bool
			curColor core.Color
			curWidth float32
		)
		flush := func() {
			if open {
				d.Stroke()
				open = false
			}
		}
		for _, seg := range segs {
			isActive := activeAxis != AxisNone && (activeAxis == AxisAll || seg.Axis == activeAxis)
			if isActive != wantActive {
				continue
			}
			ax, ay, bx, by, ok := projectSegment(seg.A, seg.B, viewProj, viewport, g.DepthNO)
			if !ok {
				continue
			}
			col := seg.Color
			w := g.lineWidth()
			if isActive {
				col = gizmoColorActive
				w = g.activeLineWidth()
			}
			if !open || col != curColor || w != curWidth {
				flush()
				curColor = col
				curWidth = w
				d.SetStrokeColor(curColor).SetLineWidth(curWidth).BeginPath()
				open = true
			}
			d.MoveTo(ax, ay).LineTo(bx, by)
		}
		flush()
	}

	// Draw inactive axes first, then active axis on top for clear visibility.
	drawPass(false)
	drawPass(true)
	d.Restore()
}

func axisDirection(axis GizmoAxis) math3d.Vec3 {
	switch axis {
	case AxisX:
		return math3d.Vec3UnitX()
	case AxisY:
		return math3d.Vec3UnitY()
	case AxisZ:
		return math3d.Vec3UnitZ()
	default:
		return math3d.Vec3Zero()
	}
}

func planeNormalForAxis(axis GizmoAxis) math3d.Vec3 {
	switch axis {
	case AxisXY:
		return math3d.Vec3UnitZ()
	case AxisXZ:
		return math3d.Vec3UnitY()
	case AxisYZ:
		return math3d.Vec3UnitX()
	default:
		return math3d.Vec3Zero()
	}
}

func snapFloat(v, snap float32) float32 {
	if snap <= 0 {
		return v
	}
	return float32(math.Round(float64(v)/float64(snap))) * snap
}

func snapVec3ForAxis(v math3d.Vec3, axis GizmoAxis, snap float32) math3d.Vec3 {
	switch axis {
	case AxisXY:
		return math3d.Vec3{X: snapFloat(v.X, snap), Y: snapFloat(v.Y, snap), Z: 0}
	case AxisXZ:
		return math3d.Vec3{X: snapFloat(v.X, snap), Y: 0, Z: snapFloat(v.Z, snap)}
	case AxisYZ:
		return math3d.Vec3{X: 0, Y: snapFloat(v.Y, snap), Z: snapFloat(v.Z, snap)}
	case AxisAll:
		return math3d.Vec3{X: snapFloat(v.X, snap), Y: snapFloat(v.Y, snap), Z: snapFloat(v.Z, snap)}
	default:
		return v
	}
}

// closestParamOnLine returns the parameter s on line L(s) = lineOrigin + s*lineDir
// (|lineDir| == 1) closest to ray (|ray.Dir| == 1). Returns false when parallel.
func closestParamOnLine(lineOrigin, lineDir math3d.Vec3, ray math3d.Ray) (float32, bool) {
	w := ray.Origin.Sub(lineOrigin)
	b := ray.Dir.Dot(lineDir)
	denom := 1 - b*b
	if denom < 1e-6 {
		return 0, false
	}
	s := (w.Dot(lineDir) - b*w.Dot(ray.Dir)) / denom
	return s, true
}

// raySegmentDistance computes the shortest Euclidean distance between a unit-dir
// ray (t >= 0) and line segment [a, b], returning (distance, rayT).
func raySegmentDistance(ray math3d.Ray, a, b math3d.Vec3) (dist, rayT float32) {
	u := ray.Dir
	v := b.Sub(a)
	w0 := ray.Origin.Sub(a)

	bv := u.Dot(v)
	c := v.LenSq()
	d := u.Dot(w0)
	e := v.Dot(w0)

	var s, t float32
	if c < 1e-10 {
		s = 0
		t = max(0, -d)
	} else {
		denom := c - bv*bv
		if denom < 1e-7 {
			s = math3d.Clamp(-e/c, 0, 1)
			t = max(0, bv*s-d)
		} else {
			s = math3d.Clamp((e-bv*d)/denom, 0, 1)
			t = bv*s - d
			if t < 0 {
				t = 0
				s = math3d.Clamp(-e/c, 0, 1)
			}
		}
	}

	pRay := ray.At(t)
	pSeg := a.Add(v.Scale(s))
	return pRay.Dist(pSeg), t
}
