package editor

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// componentGizmoHitRadius3D is how close in viewport points a pointer must be to pick a 3D component gizmo handle.
	componentGizmoHitRadius3D float64 = 10
	defaultGizmoPointRadius3D float32 = 5
	defaultGizmoLineWidth3D   float32 = 1.75
	minColliderExtent3D       float64 = 0.05
)

// Projection3D carries the 3D viewport's camera projection and world-to-screen drawing helpers
// for a single frame of size Width x Height in viewport points.
type Projection3D struct {
	Camera OrbitCamera3D
	Width  float32
	Height float32
	// ViewProj is the right-handed Y-up [0, 1] NDC view-projection matrix for Camera at Width x Height.
	ViewProj math3d.Mat4
	view     view3DCam
	hasView  bool
}

// Ray returns the world-space pick ray passing through viewport pixel (sx, sy) for a viewport of size w x h.
func (c OrbitCamera3D) Ray(sx, sy, w, h float32) math3d.Ray {
	orig, dir := c.ScreenRay(sx, sy, w, h)
	return math3d.Ray{
		Origin: math3d.Vec3{X: orig[0], Y: orig[1], Z: orig[2]},
		Dir:    math3d.Vec3{X: dir[0], Y: dir[1], Z: dir[2]},
	}
}

// Projection3D returns the camera projection and drawing helpers for a 3D viewport of size w x h.
// When w or h is non-positive, it falls back to the editor's current canvas or virtual viewport size.
func (e *Editor) Projection3D(w, h float32) Projection3D {
	cam := DefaultOrbitCamera3D()
	if e != nil {
		cam = e.Camera3D
		if w <= 0 || h <= 0 {
			w, h = e.CanvasSize()
			if w <= 0 || h <= 0 {
				w, h = e.ViewportSize()
			}
		}
	}
	if w <= 0 || h <= 0 {
		w, h = 640, 360
	}
	if e != nil && e.lookThroughCamera3D {
		if sc, _, ok := e.ActiveSceneCamera3D(w, h); ok && sc != nil {
			return Projection3D{
				Camera:   cam,
				Width:    w,
				Height:   h,
				ViewProj: sc.ViewProj(),
				view:     view3DCamFromSceneCamera(sc),
				hasView:  true,
			}
		}
	}
	return Projection3D{
		Camera:   cam,
		Width:    w,
		Height:   h,
		ViewProj: cam.ViewProj(w, h),
	}
}

// PickRay3D returns the world-space ray from the editor's 3D camera through viewport pixel (sx, sy).
func (e *Editor) PickRay3D(sx, sy, w, h float32) math3d.Ray {
	return e.Projection3D(w, h).ScreenRay(sx, sy)
}

// Project transforms a world-space 3D point into viewport pixel coordinates (sx, sy) and camera depth.
// visible is true when the point lies strictly between Near and Far in front of the camera.
func (p Projection3D) Project(world math3d.Vec3) (sx, sy, depth float32, visible bool) {
	if !isFiniteVec3(world) {
		return 0, 0, 0, false
	}
	if p.hasView {
		return p.view.project([3]float32{world.X, world.Y, world.Z}, p.Width, p.Height)
	}
	return p.Camera.Project([3]float32{world.X, world.Y, world.Z}, p.Width, p.Height)
}

// ProjectSegment clips a world-space 3D segment against the camera's near plane and projects both endpoints to viewport pixel coordinates.
func (p Projection3D) ProjectSegment(a, b math3d.Vec3) (x0, y0, x1, y1 float32, visible bool) {
	if !isFiniteVec3(a) || !isFiniteVec3(b) {
		return 0, 0, 0, 0, false
	}
	if p.hasView {
		return p.view.projectSegment([3]float32{a.X, a.Y, a.Z}, [3]float32{b.X, b.Y, b.Z}, p.Width, p.Height)
	}
	return p.Camera.projectSegment([3]float32{a.X, a.Y, a.Z}, [3]float32{b.X, b.Y, b.Z}, p.Width, p.Height)
}

// ScreenRay returns the world-space pick ray passing through viewport pixel (sx, sy).
func (p Projection3D) ScreenRay(sx, sy float32) math3d.Ray {
	if p.hasView {
		orig, dir := p.view.screenRay(sx, sy, p.Width, p.Height)
		return math3d.Ray{
			Origin: math3d.Vec3{X: orig[0], Y: orig[1], Z: orig[2]},
			Dir:    math3d.Vec3{X: dir[0], Y: dir[1], Z: dir[2]},
		}
	}
	return p.Camera.Ray(sx, sy, p.Width, p.Height)
}

// GizmoLength returns a camera-distance-scaled world length suitable for 3D handles placed at world.
func (p Projection3D) GizmoLength(world math3d.Vec3) float32 {
	if p.hasView {
		var dist float64
		if p.view.ortho {
			dist = p.view.halfH / math.Tan(p.view.fov*0.5)
		} else {
			dist = len64(sub64(vec3To64(world), p.view.eye))
		}
		l := float32(dist * 0.18)
		if !isFiniteFloat32(l) || l < 2 {
			l = 2
		}
		return l
	}
	dist, _, _, _ := p.Camera.effectiveParams()
	if !p.Camera.Orthographic {
		dist = len64(sub64([3]float64{float64(world.X), float64(world.Y), float64(world.Z)}, p.Camera.eye64()))
	}
	l := float32(dist * 0.18)
	if !isFiniteFloat32(l) || l < 2 {
		l = 2
	}
	return l
}

// DrawPoint draws a filled screen-space circular marker at the projection of world.
func (p Projection3D) DrawPoint(d *core.Drawing, world math3d.Vec3, radius float32, fill core.Color) {
	if d == nil || !isFiniteVec3(world) {
		return
	}
	sx, sy, _, vis := p.Project(world)
	if !vis || !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return
	}
	if !isFiniteFloat32(radius) || radius <= 0 {
		radius = defaultGizmoPointRadius3D
	}
	d.Save().SetFillColor(fill).BeginPath().Arc(sx, sy, radius, 0, float32(2*math.Pi), false).Fill().Restore()
}

// DrawLine draws a near-clipped world-space 3D segment from a to b.
func (p Projection3D) DrawLine(d *core.Drawing, a, b math3d.Vec3, color core.Color, width float32) {
	if d == nil || !isFiniteVec3(a) || !isFiniteVec3(b) {
		return
	}
	x0, y0, x1, y1, vis := p.ProjectSegment(a, b)
	if !vis || !isFiniteFloat32(x0) || !isFiniteFloat32(y0) || !isFiniteFloat32(x1) || !isFiniteFloat32(y1) {
		return
	}
	if !isFiniteFloat32(width) || width <= 0 {
		width = defaultGizmoLineWidth3D
	}
	d.Save().SetStrokeColor(color).SetLineWidth(width).BeginPath().MoveTo(x0, y0).LineTo(x1, y1).Stroke().Restore()
}

// DrawBox draws a wireframe 3D box centered at center with full extents size, transformed by mat
// (use math3d.Mat4Identity() when center and size are already in world space).
func (p Projection3D) DrawBox(d *core.Drawing, center, size math3d.Vec3, mat math3d.Mat4, color core.Color, width float32) {
	if d == nil || !isFiniteVec3(center) || !isFiniteVec3(size) {
		return
	}
	if mat == (math3d.Mat4{}) {
		mat = math3d.Mat4Identity()
	}
	if !isFiniteFloat32(width) || width <= 0 {
		width = defaultGizmoLineWidth3D
	}
	corners := boxCorners3D(center, size, mat)
	d.Save().SetStrokeColor(color).SetLineWidth(width).BeginPath()
	hasEdge := false
	for _, edge := range boxEdges3D {
		x0, y0, x1, y1, vis := p.ProjectSegment(corners[edge[0]], corners[edge[1]])
		if !vis || !isFiniteFloat32(x0) || !isFiniteFloat32(y0) || !isFiniteFloat32(x1) || !isFiniteFloat32(y1) {
			continue
		}
		d.MoveTo(x0, y0).LineTo(x1, y1)
		hasEdge = true
	}
	if hasEdge {
		d.Stroke()
	}
	d.Restore()
}

// GizmoHandleKind3D identifies the object-space geometry of a 3D gizmo handle.
type GizmoHandleKind3D int

const (
	// GizmoHandlePoint3D is a point handle at Position in object space.
	GizmoHandlePoint3D GizmoHandleKind3D = iota
	// GizmoHandleLine3D is a line segment handle from Start to End in object space.
	GizmoHandleLine3D
	// GizmoHandleBox3D is a wireframe box handle centered at Position with full extents Size in object space.
	GizmoHandleBox3D
)

// String returns a human-readable name for k.
func (k GizmoHandleKind3D) String() string {
	switch k {
	case GizmoHandlePoint3D:
		return "Point"
	case GizmoHandleLine3D:
		return "Line"
	case GizmoHandleBox3D:
		return "Box"
	default:
		return fmt.Sprintf("GizmoHandleKind3D(%d)", int(k))
	}
}

// GizmoHandle3D describes one visual or interactive 3D gizmo handle in object space.
type GizmoHandle3D struct {
	// ID identifies the handle within its ComponentGizmo3D and is passed to ComponentGizmo3D.Drag.
	// A handle with an empty ID or DisplayOnly=true is drawn as visual feedback and is not pickable.
	ID string
	// Kind selects whether the handle is a point, line segment, or wireframe box.
	Kind GizmoHandleKind3D
	// Position is the object-space location of a GizmoHandlePoint3D or center of a GizmoHandleBox3D.
	Position math3d.Vec3
	// Start and End are the object-space endpoints of a GizmoHandleLine3D.
	Start math3d.Vec3
	End   math3d.Vec3
	// Size holds the object-space full extents (width, height, depth) of a GizmoHandleBox3D.
	Size math3d.Vec3
	// Color is the handle's stroke/fill color; zero uses the editor's default accent color.
	Color core.Color
	// Radius is the screen-space marker radius in points for GizmoHandlePoint3D (defaults to 5 when <= 0).
	Radius float32
	// LineWidth is the stroke width in points for GizmoHandleLine3D and GizmoHandleBox3D (defaults to 1.75 when <= 0).
	LineWidth float32
	// DisplayOnly marks a visual-only primitive that draws over the 3D view without intercepting drags.
	DisplayOnly bool
	// Axis optionally records the handle's primary object-space direction for drag helpers (for example math3d.Vec3{X: 1}).
	Axis math3d.Vec3
	// Label is an optional short label drawn beside the handle while active.
	Label string
}

func (h GizmoHandle3D) referencePoint() math3d.Vec3 {
	switch h.Kind {
	case GizmoHandleLine3D:
		return h.Start.Add(h.End).Scale(0.5)
	default:
		return h.Position
	}
}

func (h GizmoHandle3D) effectiveColor(pal SpecPalette, active bool) core.Color {
	if active {
		return pal.Warning.Fg
	}
	var zero core.Color
	if h.Color != zero {
		return h.Color
	}
	return pal.Accent.Fg
}

// ComponentGizmo3D registers 3D viewport handles and drag editing for one component type.
// Handles returns object-space primitives (points, lines, boxes) for objectID when it carries
// Component. Drag applies a handle drag by writing component fields through Editor (such as
// SetComponentField or Editor.History); the editor previews each move live, rolls back cleanly
// on Escape or cancel, and commits the whole drag as a single undo step in Editor.History.
type ComponentGizmo3D struct {
	// Component is the component type name this gizmo handles (as keyed in ObjectDoc.Components), with no control characters.
	Component string
	// Handles returns the object-space handles and visual primitives for objectID.
	Handles func(ed *Editor, objectID string) []GizmoHandle3D
	// Drag applies a handle drag in object space. Because each move rolls back the previous
	// preview before calling Drag, ed.ComponentField reads the exact pre-drag field values, and
	// all edits made through Editor or Editor.History during the drag commit as one undo step.
	Drag func(ed *Editor, drag GizmoDrag3D) error
	// ShowUnselected, when true, also draws this gizmo's primitives on visible unselected objects
	// (only the selected object's non-DisplayOnly handles can be dragged).
	ShowUnselected bool
}

// PickedComponentGizmo3D identifies a picked or active 3D component gizmo handle on a scene object.
type PickedComponentGizmo3D struct {
	ObjectID  string
	Component string
	Handle    GizmoHandle3D
}

// GizmoDrag3D describes an in-progress drag of a 3D component gizmo handle.
type GizmoDrag3D struct {
	// ObjectID is the ID of the object carrying Component.
	ObjectID string
	// Component is the component type name being edited.
	Component string
	// Handle is the picked handle in object space as it was when the drag began.
	Handle GizmoHandle3D
	// StartRay and Ray are the camera pick rays in world space at drag start and the current pointer position.
	StartRay math3d.Ray
	Ray      math3d.Ray
	// StartLocalRay and LocalRay are StartRay and Ray transformed into the object's local space.
	StartLocalRay math3d.Ray
	LocalRay      math3d.Ray
	// ObjectToWorld and WorldToObject transform between object space and world space at drag start.
	ObjectToWorld math3d.Mat4
	WorldToObject math3d.Mat4
	// StartPoint is the handle's object-space reference point at drag start (Position for point/box, midpoint for line).
	StartPoint math3d.Vec3
	// ScreenStart and ScreenCurrent are the viewport pixel coordinates at drag start and now.
	ScreenStart   [2]float32
	ScreenCurrent [2]float32
	// ViewportWidth and ViewportHeight are the 3D viewport's size in points.
	ViewportWidth  float32
	ViewportHeight float32
	// Camera is the editor's 3D camera for the drag.
	Camera  OrbitCamera3D
	view    view3DCam
	hasView bool
}

func isFiniteVec3(v math3d.Vec3) bool {
	return isFiniteFloat32(v.X) && isFiniteFloat32(v.Y) && isFiniteFloat32(v.Z)
}

// AxisDelta computes the signed displacement along object-space axis from the drag start to the
// current pointer ray, falling back to screen-space projection when the axis is nearly parallel
// to the view ray.
func (d GizmoDrag3D) AxisDelta(axis math3d.Vec3) (float32, bool) {
	if !isFiniteVec3(axis) || !isFiniteVec3(d.StartPoint) {
		return 0, false
	}
	a64 := normalize64(vec3To64(axis))
	if len64(a64) < 1e-9 {
		return 0, false
	}
	origin := vec3To64(d.StartPoint)
	if isFiniteVec3(d.StartLocalRay.Origin) && isFiniteVec3(d.StartLocalRay.Dir) && isFiniteVec3(d.LocalRay.Origin) && isFiniteVec3(d.LocalRay.Dir) {
		t0, ok0 := closestParamOnAxisLine64(origin, a64, vec3To64(d.StartLocalRay.Origin), vec3To64(d.StartLocalRay.Dir))
		t1, ok1 := closestParamOnAxisLine64(origin, a64, vec3To64(d.LocalRay.Origin), vec3To64(d.LocalRay.Dir))
		if ok0 && ok1 {
			delta := float32(t1 - t0)
			if isFiniteFloat32(delta) {
				return delta, true
			}
		}
	}
	if !isFiniteFloat32(d.ScreenStart[0]) || !isFiniteFloat32(d.ScreenStart[1]) || !isFiniteFloat32(d.ScreenCurrent[0]) || !isFiniteFloat32(d.ScreenCurrent[1]) {
		return 0, false
	}
	// Edge-on fallback: project screen delta onto the projected object-space axis.
	unitAxis := math3d.Vec3{X: float32(a64[0]), Y: float32(a64[1]), Z: float32(a64[2])}
	p0World := d.ObjectToWorld.TransformPoint(d.StartPoint)
	p1World := d.ObjectToWorld.TransformPoint(d.StartPoint.Add(unitAxis))
	var x0, y0, x1, y1 float32
	var vis bool
	if d.hasView {
		x0, y0, x1, y1, vis = d.view.projectSegment(
			[3]float32{p0World.X, p0World.Y, p0World.Z},
			[3]float32{p1World.X, p1World.Y, p1World.Z},
			d.ViewportWidth, d.ViewportHeight,
		)
	} else {
		x0, y0, x1, y1, vis = d.Camera.projectSegment(
			[3]float32{p0World.X, p0World.Y, p0World.Z},
			[3]float32{p1World.X, p1World.Y, p1World.Z},
			d.ViewportWidth, d.ViewportHeight,
		)
	}
	if !vis {
		return 0, false
	}
	ax, ay := float64(x1-x0), float64(y1-y0)
	pixLen := math.Hypot(ax, ay)
	if pixLen < 1e-3 {
		return 0, false
	}
	dsx := float64(d.ScreenCurrent[0] - d.ScreenStart[0])
	dsy := float64(d.ScreenCurrent[1] - d.ScreenStart[1])
	projPx := (dsx*ax + dsy*ay) / pixLen
	out := float32(projPx / pixLen)
	if !isFiniteFloat32(out) {
		return 0, false
	}
	return out, true
}

// PlanePoint intersects LocalRay with the object-space plane passing through planePoint with planeNormal.
func (d GizmoDrag3D) PlanePoint(planePoint, planeNormal math3d.Vec3) (math3d.Vec3, bool) {
	if !isFiniteVec3(planePoint) || !isFiniteVec3(planeNormal) || !isFiniteVec3(d.LocalRay.Origin) || !isFiniteVec3(d.LocalRay.Dir) {
		return math3d.Vec3{}, false
	}
	n := normalize64(vec3To64(planeNormal))
	if len64(n) < 1e-9 {
		return math3d.Vec3{}, false
	}
	hit, ok := intersectRayPlane64(vec3To64(d.LocalRay.Origin), vec3To64(d.LocalRay.Dir), vec3To64(planePoint), n)
	if !ok {
		return math3d.Vec3{}, false
	}
	out := math3d.Vec3{X: float32(hit[0]), Y: float32(hit[1]), Z: float32(hit[2])}
	if !isFiniteVec3(out) {
		return math3d.Vec3{}, false
	}
	return out, true
}

// PlaneDelta returns the object-space displacement between StartLocalRay and LocalRay intersected
// with the plane passing through StartPoint with planeNormal.
func (d GizmoDrag3D) PlaneDelta(planeNormal math3d.Vec3) (math3d.Vec3, bool) {
	if !isFiniteVec3(planeNormal) || !isFiniteVec3(d.StartPoint) || !isFiniteVec3(d.StartLocalRay.Origin) || !isFiniteVec3(d.StartLocalRay.Dir) || !isFiniteVec3(d.LocalRay.Origin) || !isFiniteVec3(d.LocalRay.Dir) {
		return math3d.Vec3{}, false
	}
	n := normalize64(vec3To64(planeNormal))
	if len64(n) < 1e-9 {
		return math3d.Vec3{}, false
	}
	p0 := vec3To64(d.StartPoint)
	h0, ok0 := intersectRayPlane64(vec3To64(d.StartLocalRay.Origin), vec3To64(d.StartLocalRay.Dir), p0, n)
	h1, ok1 := intersectRayPlane64(vec3To64(d.LocalRay.Origin), vec3To64(d.LocalRay.Dir), p0, n)
	if !ok0 || !ok1 {
		return math3d.Vec3{}, false
	}
	diff := sub64(h1, h0)
	out := math3d.Vec3{X: float32(diff[0]), Y: float32(diff[1]), Z: float32(diff[2])}
	if !isFiniteVec3(out) {
		return math3d.Vec3{}, false
	}
	return out, true
}

// ViewPlaneDelta returns the object-space displacement across the camera-facing plane passing through StartPoint.
func (d GizmoDrag3D) ViewPlaneDelta() (math3d.Vec3, bool) {
	var fwd [3]float32
	if d.hasView {
		fwd = vec32(d.view.forward)
	} else {
		_, _, fwd = d.Camera.Basis()
	}
	localNormal := d.WorldToObject.TransformDir(math3d.Vec3{X: fwd[0], Y: fwd[1], Z: fwd[2]})
	return d.PlaneDelta(localNormal)
}

func vec3To64(v math3d.Vec3) [3]float64 {
	return [3]float64{float64(v.X), float64(v.Y), float64(v.Z)}
}

func boxCorners3D(center, size math3d.Vec3, mat math3d.Mat4) [8]math3d.Vec3 {
	hx, hy, hz := size.X*0.5, size.Y*0.5, size.Z*0.5
	var corners [8]math3d.Vec3
	for i, s := range boxSigns3D {
		local := math3d.Vec3{
			X: center.X + float32(s[0])*hx,
			Y: center.Y + float32(s[1])*hy,
			Z: center.Z + float32(s[2])*hz,
		}
		corners[i] = mat.TransformPoint(local)
	}
	return corners
}

// transformRay3D transforms worldRay into the coordinate frame mapped by mat, normalizing Dir.
func transformRay3D(mat math3d.Mat4, r math3d.Ray) math3d.Ray {
	o := mat.TransformPoint(r.Origin)
	d := mat.TransformDir(r.Dir)
	if l := d.Len(); l > 1e-12 {
		d = d.Scale(1 / l)
	}
	return math3d.Ray{Origin: o, Dir: d}
}

// hitTestGizmoHandle3D computes the minimum screen-space distance from (sx, sy) to handle h
// transformed into world space by mat. Returns hit=true when the distance is strictly below hitRadius.
func hitTestGizmoHandle3D(proj Projection3D, mat math3d.Mat4, h GizmoHandle3D, sx, sy float32, hitRadius float64) (float64, bool) {
	if h.DisplayOnly || h.ID == "" {
		return math.Inf(1), false
	}
	switch h.Kind {
	case GizmoHandlePoint3D:
		wp := mat.TransformPoint(h.Position)
		px, py, _, vis := proj.Project(wp)
		if !vis {
			return math.Inf(1), false
		}
		r := hitRadius
		if hr := float64(h.Radius) + 4; hr > r {
			r = hr
		}
		d := math.Hypot(float64(sx-px), float64(sy-py))
		return d, d < r

	case GizmoHandleLine3D:
		wa := mat.TransformPoint(h.Start)
		wb := mat.TransformPoint(h.End)
		x0, y0, x1, y1, vis := proj.ProjectSegment(wa, wb)
		if !vis {
			return math.Inf(1), false
		}
		d := pointToSegmentDist64(float64(sx), float64(sy), float64(x0), float64(y0), float64(x1), float64(y1))
		return d, d < hitRadius

	case GizmoHandleBox3D:
		corners := boxCorners3D(h.Position, h.Size, mat)
		best := math.Inf(1)
		for _, edge := range boxEdges3D {
			x0, y0, x1, y1, vis := proj.ProjectSegment(corners[edge[0]], corners[edge[1]])
			if !vis {
				continue
			}
			d := pointToSegmentDist64(float64(sx), float64(sy), float64(x0), float64(y0), float64(x1), float64(y1))
			if d < best {
				best = d
			}
		}
		return best, best < hitRadius
	default:
		return math.Inf(1), false
	}
}

// pickGizmoHandles3D finds the closest pickable handle in handles under (sx, sy) within hitRadius.
// Point handles take priority over overlapping line/box handles so small face/corner dots remain easy to grab.
func pickGizmoHandles3D(proj Projection3D, mat math3d.Mat4, handles []GizmoHandle3D, sx, sy float32, hitRadius float64) (int, float64, bool) {
	bestIdx := -1
	bestPrio := 99
	bestDist := hitRadius
	for i, h := range handles {
		d, hit := hitTestGizmoHandle3D(proj, mat, h, sx, sy, hitRadius)
		if !hit {
			continue
		}
		prio := 1
		if h.Kind == GizmoHandlePoint3D {
			prio = 0
		}
		if bestIdx < 0 || prio < bestPrio || (prio == bestPrio && d < bestDist) {
			bestIdx = i
			bestPrio = prio
			bestDist = d
		}
	}
	if bestIdx < 0 {
		return -1, math.Inf(1), false
	}
	return bestIdx, bestDist, true
}

// drawGizmoHandle3D renders a single 3D gizmo handle transformed by mat onto d through proj.
func drawGizmoHandle3D(d *core.Drawing, pal SpecPalette, proj Projection3D, mat math3d.Mat4, h GizmoHandle3D, active bool) {
	if d == nil {
		return
	}
	col := h.effectiveColor(pal, active)
	switch h.Kind {
	case GizmoHandlePoint3D:
		wp := mat.TransformPoint(h.Position)
		sx, sy, _, vis := proj.Project(wp)
		if !vis {
			return
		}
		r := h.Radius
		if r <= 0 {
			r = defaultGizmoPointRadius3D
		}
		if active {
			r += 1.5
		}
		d.Save()
		d.SetFillColor(col).BeginPath().Arc(sx, sy, r, 0, float32(2*math.Pi), false).Fill()
		d.SetStrokeColor(specAlpha(pal.Bg, 0.9)).SetLineWidth(1.25).BeginPath().Arc(sx, sy, r, 0, float32(2*math.Pi), false).Stroke()
		if active && h.Label != "" {
			d.SetFillColor(col).FillText(h.Label, sx+r+4, sy+3, core.Font{Size: 10})
		}
		d.Restore()

	case GizmoHandleLine3D:
		wa := mat.TransformPoint(h.Start)
		wb := mat.TransformPoint(h.End)
		x0, y0, x1, y1, vis := proj.ProjectSegment(wa, wb)
		if !vis {
			return
		}
		lw := h.LineWidth
		if lw <= 0 {
			lw = defaultGizmoLineWidth3D
		}
		if active {
			lw += 1.0
		}
		d.Save().SetStrokeColor(col).SetLineWidth(lw).BeginPath().MoveTo(x0, y0).LineTo(x1, y1).Stroke().Restore()

	case GizmoHandleBox3D:
		lw := h.LineWidth
		if lw <= 0 {
			lw = defaultGizmoLineWidth3D
		}
		if active {
			lw += 0.75
		}
		proj.DrawBox(d, h.Position, h.Size, mat, col, lw)
	}
}

// drawGizmoHandles3D draws visual primitives (boxes and lines) first and point handles on top.
func drawGizmoHandles3D(d *core.Drawing, pal SpecPalette, proj Projection3D, mat math3d.Mat4, handles []GizmoHandle3D, activeID string) {
	for _, h := range handles {
		if h.Kind != GizmoHandlePoint3D {
			drawGizmoHandle3D(d, pal, proj, mat, h, activeID != "" && h.ID == activeID)
		}
	}
	for _, h := range handles {
		if h.Kind == GizmoHandlePoint3D {
			drawGizmoHandle3D(d, pal, proj, mat, h, activeID != "" && h.ID == activeID)
		}
	}
}

// --- Built-in 3D component gizmos built on the shared ComponentGizmo3D API ---

var builtinComponentGizmos3D = map[string]*ComponentGizmo3D{
	sceneformat.Collider3DComponentName: newPhysicsCollider3DGizmo(sceneformat.Collider3DComponentName, false),
	sceneformat.Trigger3DComponentName:  newPhysicsCollider3DGizmo(sceneformat.Trigger3DComponentName, true),
}

func newPhysicsCollider3DGizmo(component string, alwaysTrigger bool) *ComponentGizmo3D {
	readFloat := func(ed *Editor, objectID, field string, fallback float64) float64 {
		v, ok := ed.ComponentField(objectID, component, field)
		if !ok {
			return fallback
		}
		switch n := v.(type) {
		case float64:
			return n
		case float32:
			return float64(n)
		case int:
			return float64(n)
		default:
			return fallback
		}
	}
	readString := func(ed *Editor, objectID, field, fallback string) string {
		if v, ok := ed.ComponentField(objectID, component, field); ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
		return fallback
	}
	isTrigger := func(ed *Editor, objectID string) bool {
		if alwaysTrigger {
			return true
		}
		if v, ok := ed.ComponentField(objectID, component, "isTrigger"); ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
		return false
	}

	return &ComponentGizmo3D{
		Component: component,
		Handles: func(ed *Editor, objectID string) []GizmoHandle3D {
			// In Gizmo3DCollider mode the dedicated collider gizmo owns collider3d/trigger3d handles (including cylinder).
			if !ed.ShowPhysicsDebug || ed.GizmoMode == Gizmo3DCollider {
				return nil
			}
			shape := readString(ed, objectID, "shape", "box")
			cx := float32(readFloat(ed, objectID, "offsetX", 0))
			cy := float32(readFloat(ed, objectID, "offsetY", 0))
			cz := float32(readFloat(ed, objectID, "offsetZ", 0))
			center := math3d.Vec3{X: cx, Y: cy, Z: cz}
			spec := ed.activeSpecPalette()
			col := spec.Accent.Fg
			if isTrigger(ed, objectID) {
				col = spec.Warning.Fg
			}

			switch shape {
			case "sphere":
				r := float32(readFloat(ed, objectID, "radius", 0.5))
				return []GizmoHandle3D{
					{Kind: GizmoHandleLine3D, Start: center, End: center.Add(math3d.Vec3{X: r}), Color: col.Faded(0.65), DisplayOnly: true},
					{Kind: GizmoHandleLine3D, Start: center, End: center.Add(math3d.Vec3{Y: r}), Color: col.Faded(0.65), DisplayOnly: true},
					{Kind: GizmoHandleLine3D, Start: center, End: center.Add(math3d.Vec3{Z: r}), Color: col.Faded(0.65), DisplayOnly: true},
					{ID: "radius+x", Kind: GizmoHandlePoint3D, Position: center.Add(math3d.Vec3{X: r}), Axis: math3d.Vec3{X: 1}, Color: col, Label: "R"},
					{ID: "radius+y", Kind: GizmoHandlePoint3D, Position: center.Add(math3d.Vec3{Y: r}), Axis: math3d.Vec3{Y: 1}, Color: col, Label: "R"},
					{ID: "radius+z", Kind: GizmoHandlePoint3D, Position: center.Add(math3d.Vec3{Z: r}), Axis: math3d.Vec3{Z: 1}, Color: col, Label: "R"},
				}
			case "capsule":
				r := float32(readFloat(ed, objectID, "radius", 0.5))
				h := float32(readFloat(ed, objectID, "height", 1.0))
				halfH := max(h*0.5, r)
				return []GizmoHandle3D{
					{Kind: GizmoHandleLine3D, Start: center, End: center.Add(math3d.Vec3{X: r}), Color: col.Faded(0.65), DisplayOnly: true},
					{Kind: GizmoHandleLine3D, Start: center.Add(math3d.Vec3{Y: -halfH}), End: center.Add(math3d.Vec3{Y: halfH}), Color: col.Faded(0.65), DisplayOnly: true},
					{ID: "radius+x", Kind: GizmoHandlePoint3D, Position: center.Add(math3d.Vec3{X: r}), Axis: math3d.Vec3{X: 1}, Color: col, Label: "R"},
					{ID: "height+y", Kind: GizmoHandlePoint3D, Position: center.Add(math3d.Vec3{Y: halfH}), Axis: math3d.Vec3{Y: 1}, Color: col, Label: "H"},
					{ID: "height-y", Kind: GizmoHandlePoint3D, Position: center.Add(math3d.Vec3{Y: -halfH}), Axis: math3d.Vec3{Y: -1}, Color: col, Label: "H"},
				}
			default: // "box"
				w := float32(readFloat(ed, objectID, "width", 1.0))
				h := float32(readFloat(ed, objectID, "height", 1.0))
				d := float32(readFloat(ed, objectID, "depth", 1.0))
				hx, hy, hz := w*0.5, h*0.5, d*0.5
				return []GizmoHandle3D{
					{Kind: GizmoHandleBox3D, Position: center, Size: math3d.Vec3{X: w, Y: h, Z: d}, Color: col, DisplayOnly: true},
					{ID: "+x", Kind: GizmoHandlePoint3D, Position: center.Add(math3d.Vec3{X: hx}), Axis: math3d.Vec3{X: 1}, Color: col, Label: "+X"},
					{ID: "-x", Kind: GizmoHandlePoint3D, Position: center.Add(math3d.Vec3{X: -hx}), Axis: math3d.Vec3{X: -1}, Color: col, Label: "-X"},
					{ID: "+y", Kind: GizmoHandlePoint3D, Position: center.Add(math3d.Vec3{Y: hy}), Axis: math3d.Vec3{Y: 1}, Color: col, Label: "+Y"},
					{ID: "-y", Kind: GizmoHandlePoint3D, Position: center.Add(math3d.Vec3{Y: -hy}), Axis: math3d.Vec3{Y: -1}, Color: col, Label: "-Y"},
					{ID: "+z", Kind: GizmoHandlePoint3D, Position: center.Add(math3d.Vec3{Z: hz}), Axis: math3d.Vec3{Z: 1}, Color: col, Label: "+Z"},
					{ID: "-z", Kind: GizmoHandlePoint3D, Position: center.Add(math3d.Vec3{Z: -hz}), Axis: math3d.Vec3{Z: -1}, Color: col, Label: "-Z"},
				}
			}
		},
		Drag: func(ed *Editor, drag GizmoDrag3D) error {
			delta32, ok := drag.AxisDelta(drag.Handle.Axis)
			if !ok {
				return nil
			}
			delta := float64(delta32)
			snapExtent := func(v float64) float64 {
				if grid, pixel := ed.positionSnapMode(); grid || pixel {
					return ed.SnapValue(v)
				}
				return v
			}
			switch drag.Handle.ID {
			case "radius+x", "radius+y", "radius+z":
				r := readFloat(ed, drag.ObjectID, "radius", 0.5)
				next := max(minColliderExtent3D, snapExtent(r+delta))
				return ed.SetComponentField(drag.ObjectID, component, "radius", next)
			case "height+y", "height-y":
				h := readFloat(ed, drag.ObjectID, "height", 1.0)
				offY := readFloat(ed, drag.ObjectID, "offsetY", 0)
				r := readFloat(ed, drag.ObjectID, "radius", 0.5)
				minH := max(minColliderExtent3D, 2*r)
				nextH := max(minH, snapExtent(h+delta))
				actual := nextH - h
				sign := 1.0
				if drag.Handle.ID == "height-y" {
					sign = -1.0
				}
				if err := ed.SetComponentField(drag.ObjectID, component, "height", nextH); err != nil {
					return err
				}
				if actual != 0 {
					return ed.SetComponentField(drag.ObjectID, component, "offsetY", offY+sign*actual*0.5)
				}
				return nil
			case "+x", "-x", "+y", "-y", "+z", "-z":
				field, offField := "width", "offsetX"
				sign := 1.0
				switch drag.Handle.ID {
				case "-x":
					sign = -1.0
				case "+y":
					field, offField = "height", "offsetY"
				case "-y":
					field, offField, sign = "height", "offsetY", -1.0
				case "+z":
					field, offField = "depth", "offsetZ"
				case "-z":
					field, offField, sign = "depth", "offsetZ", -1.0
				}
				cur := readFloat(ed, drag.ObjectID, field, 1.0)
				off := readFloat(ed, drag.ObjectID, offField, 0)
				next := max(minColliderExtent3D, snapExtent(cur+delta))
				actual := next - cur
				if err := ed.SetComponentField(drag.ObjectID, component, field, next); err != nil {
					return err
				}
				if actual != 0 {
					return ed.SetComponentField(drag.ObjectID, component, offField, off+sign*actual*0.5)
				}
				return nil
			}
			return nil
		},
	}
}

// --- Component gizmo resolution, picking, drawing, and drag transaction state ---

type componentGizmoDrag3DState struct {
	active        bool
	objectID      string
	component     string
	gizmo         *ComponentGizmo3D
	handle        GizmoHandle3D
	label         string
	startRay      math3d.Ray
	startLocalRay math3d.Ray
	objToWorld    math3d.Mat4
	worldToObj    math3d.Mat4
	startPoint    math3d.Vec3
	startSX       float32
	startSY       float32
	lastSX        float32
	lastSY        float32
	vpW           float32
	vpH           float32
	previewCmds   []Command
}

// componentGizmo3D returns the active 3D component gizmo for component: the first active plugin
// in editor.plugins order that registers one, falling back to the editor's built-in 3D component gizmos.
func (e *Editor) componentGizmo3D(component string) *ComponentGizmo3D {
	if e == nil {
		return nil
	}
	if g := e.pluginComponentGizmo3D(component); g != nil {
		return g
	}
	return builtinComponentGizmos3D[component]
}

// objectWorldMat3D returns the 4x4 transform from object id's local space to world space,
// composing all ancestors and id's own 3D transform (as posed by any open animation clip).
func (e *Editor) objectWorldMat3D(id string) (math3d.Mat4, bool) {
	if e == nil || e.Doc == nil || id == "" {
		return math3d.Mat4Identity(), false
	}
	if e.clipScene != nil {
		if bo := e.clipScene.Object(id); bo != nil {
			var path []*sceneformat.ObjectDoc
			for cur := bo; cur != nil; cur = cur.Parent {
				if cur.Doc != nil {
					path = append(path, cur.Doc)
				}
			}
			m := math3d.Mat4Identity()
			for i := len(path) - 1; i >= 0; i-- {
				m = m.Mul(sceneformat.ObjectLocalTransform3D(path[i]).ToMat4())
			}
			return m, true
		}
	}
	path := e.resolvedObjectPath(id)
	if len(path) == 0 {
		return math3d.Mat4Identity(), false
	}
	m := math3d.Mat4Identity()
	for _, o := range path {
		m = m.Mul(sceneformat.ObjectLocalTransform3D(e.posed3D(o)).ToMat4())
	}
	return m, true
}

func (e *Editor) componentGizmosEnabled3D() bool {
	if e.gizmo3DBlocked() || e.Doc == nil || e.SelectionCount() != 1 {
		return false
	}
	if e.Tool == ToolPlugin && e.activePluginToolSupports3D() {
		return false
	}
	return true
}

// ComponentGizmoHandles3D returns all 3D component gizmo handles registered for objectID's components.
func (e *Editor) ComponentGizmoHandles3D(objectID string) []PickedComponentGizmo3D {
	if e == nil || e.Doc == nil || objectID == "" {
		return nil
	}
	obj, _ := e.FindResolvedObject(objectID)
	if obj == nil || len(obj.Components) == 0 {
		return nil
	}
	var out []PickedComponentGizmo3D
	for _, compName := range sortedMapKeys(obj.Components) {
		g := e.componentGizmo3D(compName)
		if g == nil || g.Handles == nil {
			continue
		}
		for _, h := range g.Handles(e, objectID) {
			out = append(out, PickedComponentGizmo3D{
				ObjectID:  objectID,
				Component: compName,
				Handle:    h,
			})
		}
	}
	return out
}

// PickComponentGizmo3D hit-tests the selected object's 3D component gizmo handles at viewport pixel (sx, sy).
func (e *Editor) PickComponentGizmo3D(sx, sy, w, h float32) (PickedComponentGizmo3D, bool) {
	if !e.componentGizmosEnabled3D() || w <= 0 || h <= 0 || !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return PickedComponentGizmo3D{}, false
	}
	obj, _ := e.SelectedObject()
	if obj == nil || len(obj.Components) == 0 || e.viewportFrozen(obj.ID) {
		return PickedComponentGizmo3D{}, false
	}
	worldMat, ok := e.objectWorldMat3D(obj.ID)
	if !ok {
		return PickedComponentGizmo3D{}, false
	}
	proj := e.Projection3D(w, h)
	var best PickedComponentGizmo3D
	bestPrio := 99
	bestDist := componentGizmoHitRadius3D
	found := false

	for _, compName := range sortedMapKeys(obj.Components) {
		g := e.componentGizmo3D(compName)
		if g == nil || g.Handles == nil || g.Drag == nil {
			continue
		}
		handles := g.Handles(e, obj.ID)
		idx, dist, hit := pickGizmoHandles3D(proj, worldMat, handles, sx, sy, componentGizmoHitRadius3D)
		if !hit {
			continue
		}
		prio := 1
		if handles[idx].Kind == GizmoHandlePoint3D {
			prio = 0
		}
		if !found || prio < bestPrio || (prio == bestPrio && dist < bestDist) {
			found = true
			bestPrio = prio
			bestDist = dist
			best = PickedComponentGizmo3D{
				ObjectID:  obj.ID,
				Component: compName,
				Handle:    handles[idx],
			}
		}
	}
	return best, found
}

// DraggingComponentGizmo3D reports whether a 3D component gizmo handle drag is currently in progress.
func (e *Editor) DraggingComponentGizmo3D() bool {
	return e != nil && e.compGizmoDrag.active
}

// ActiveComponentGizmo3D returns the currently dragged 3D component gizmo handle, or false when idle.
func (e *Editor) ActiveComponentGizmo3D() (PickedComponentGizmo3D, bool) {
	if e == nil || !e.compGizmoDrag.active {
		return PickedComponentGizmo3D{}, false
	}
	return PickedComponentGizmo3D{
		ObjectID:  e.compGizmoDrag.objectID,
		Component: e.compGizmoDrag.component,
		Handle:    e.compGizmoDrag.handle,
	}, true
}

// BeginComponentGizmoDrag3D hit-tests the selected object's 3D component gizmo handles at (sx, sy)
// and begins dragging the hit handle. Edits made during the drag are previewed live and committed
// as one undo step in Editor.History on EndComponentGizmoDrag3D (or rolled back on CancelComponentGizmoDrag3D).
func (e *Editor) BeginComponentGizmoDrag3D(sx, sy, w, h float32) (PickedComponentGizmo3D, bool) {
	if e == nil || e.compGizmoDrag.active || !e.componentGizmosEnabled3D() {
		return PickedComponentGizmo3D{}, false
	}
	picked, ok := e.PickComponentGizmo3D(sx, sy, w, h)
	if !ok || e.checkAgentTouchLock(picked.ObjectID) != nil {
		return PickedComponentGizmo3D{}, false
	}
	g := e.componentGizmo3D(picked.Component)
	if g == nil || g.Drag == nil {
		return PickedComponentGizmo3D{}, false
	}
	objToWorld, ok := e.objectWorldMat3D(picked.ObjectID)
	if !ok {
		return PickedComponentGizmo3D{}, false
	}
	worldToObj, ok := objToWorld.Inverse()
	if !ok {
		return PickedComponentGizmo3D{}, false
	}
	startRay := e.PickRay3D(sx, sy, w, h)
	startLocalRay := transformRay3D(worldToObj, startRay)
	label := fmt.Sprintf("Edit %s (%s)", picked.Component, picked.Handle.ID)
	e.compGizmoDrag = componentGizmoDrag3DState{
		active:        true,
		objectID:      picked.ObjectID,
		component:     picked.Component,
		gizmo:         g,
		handle:        picked.Handle,
		label:         label,
		startRay:      startRay,
		startLocalRay: startLocalRay,
		objToWorld:    objToWorld,
		worldToObj:    worldToObj,
		startPoint:    picked.Handle.referencePoint(),
		startSX:       sx,
		startSY:       sy,
		lastSX:        sx,
		lastSY:        sy,
		vpW:           w,
		vpH:           h,
	}
	e.drag3D = drag3DState{
		kind:     drag3DComponentGizmo,
		objectID: picked.ObjectID,
		startSX:  sx,
		startSY:  sy,
		lastSX:   sx,
		lastSY:   sy,
		vpW:      w,
		vpH:      h,
	}
	e.Status = label
	e.notify()
	return picked, true
}

// UpdateComponentGizmoDrag3D updates an active 3D component gizmo handle drag to viewport pixel (sx, sy).
func (e *Editor) UpdateComponentGizmoDrag3D(sx, sy float32) {
	if e == nil || !e.compGizmoDrag.active || !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return
	}
	if e.playing {
		e.CancelComponentGizmoDrag3D()
		return
	}
	st := &e.compGizmoDrag
	if obj, _ := e.FindResolvedObject(st.objectID); obj == nil {
		e.CancelComponentGizmoDrag3D()
		return
	}
	if st.gizmo == nil || st.gizmo.Drag == nil {
		e.CancelComponentGizmoDrag3D()
		return
	}

	proj := e.Projection3D(st.vpW, st.vpH)
	ray := proj.ScreenRay(sx, sy)
	localRay := transformRay3D(st.worldToObj, ray)
	drag := GizmoDrag3D{
		ObjectID:       st.objectID,
		Component:      st.component,
		Handle:         st.handle,
		StartRay:       st.startRay,
		Ray:            ray,
		StartLocalRay:  st.startLocalRay,
		LocalRay:       localRay,
		ObjectToWorld:  st.objToWorld,
		WorldToObject:  st.worldToObj,
		StartPoint:     st.startPoint,
		ScreenStart:    [2]float32{st.startSX, st.startSY},
		ScreenCurrent:  [2]float32{sx, sy},
		ViewportWidth:  st.vpW,
		ViewportHeight: st.vpH,
		Camera:         e.Camera3D,
		view:           proj.view,
		hasView:        proj.hasView,
	}

	// Roll back the previous preview frame so Drag always sees the clean pre-drag state.
	prev := st.previewCmds
	if len(prev) > 0 {
		(&groupCmd{cmds: prev}).rollback(len(prev))
		st.previewCmds = nil
	}

	grp := e.History.beginPreview(st.label)
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				partial := e.History.endPreview(grp)
				if len(partial) > 0 {
					(&groupCmd{cmds: partial}).rollback(len(partial))
				}
				if st.active && len(prev) > 0 {
					_ = (&groupCmd{cmds: prev}).do()
					st.previewCmds = prev
				}
				panic(r)
			}
		}()
		err = st.gizmo.Drag(e, drag)
	}()
	cmds := e.History.endPreview(grp)

	if !st.active {
		if len(cmds) > 0 {
			(&groupCmd{cmds: cmds}).rollback(len(cmds))
			e.afterMutate()
		}
		return
	}

	if err != nil {
		if len(cmds) > 0 {
			(&groupCmd{cmds: cmds}).rollback(len(cmds))
		}
		if len(prev) > 0 {
			_ = (&groupCmd{cmds: prev}).do()
			st.previewCmds = prev
		}
		e.report(err)
		e.afterMutate()
		return
	}

	st.previewCmds = cmds
	st.lastSX = sx
	st.lastSY = sy
	if e.drag3D.kind == drag3DComponentGizmo {
		e.drag3D.lastSX = sx
		e.drag3D.lastSY = sy
	}
	if len(cmds) == 0 && len(prev) > 0 {
		// Reverted cleanly back to pre-drag values; notify since no History command executed this frame.
		e.afterMutate()
	}
}

// EndComponentGizmoDrag3D finishes an active 3D component gizmo drag and commits all edits made
// during the drag as one undoable step in Editor.History. Returns true when a step was committed.
func (e *Editor) EndComponentGizmoDrag3D() bool {
	if e == nil || !e.compGizmoDrag.active {
		return false
	}
	st := e.compGizmoDrag
	e.compGizmoDrag = componentGizmoDrag3DState{}
	if e.drag3D.kind == drag3DComponentGizmo {
		e.drag3D = drag3DState{}
	}
	if len(st.previewCmds) == 0 {
		e.notify()
		return false
	}
	e.Status = st.label
	return e.History.commitPreview(st.label, st.previewCmds)
}

// CancelComponentGizmoDrag3D aborts an active 3D component gizmo drag, rolling back all previewed
// edits and recording nothing in Editor.History.
func (e *Editor) CancelComponentGizmoDrag3D() bool {
	if e == nil || !e.compGizmoDrag.active {
		return false
	}
	st := e.compGizmoDrag
	e.compGizmoDrag = componentGizmoDrag3DState{}
	if e.drag3D.kind == drag3DComponentGizmo {
		e.drag3D = drag3DState{}
	}
	if len(st.previewCmds) > 0 {
		(&groupCmd{cmds: st.previewCmds}).rollback(len(st.previewCmds))
		e.afterMutate()
		return true
	}
	e.notify()
	return true
}

func (e *Editor) drawComponentGizmos3D(d *core.Drawing, proj Projection3D) {
	if e.gizmo3DBlocked() || d == nil || e.Doc == nil {
		return
	}
	drawForObject := func(obj *sceneformat.ObjectDoc, worldMat math3d.Mat4, selected bool) {
		if obj == nil || len(obj.Components) == 0 {
			return
		}
		for _, compName := range sortedMapKeys(obj.Components) {
			g := e.componentGizmo3D(compName)
			if g == nil || g.Handles == nil {
				continue
			}
			if !selected && !g.ShowUnselected {
				continue
			}
			handles := g.Handles(e, obj.ID)
			if len(handles) == 0 {
				continue
			}
			activeID := ""
			if selected && e.compGizmoDrag.active && e.compGizmoDrag.objectID == obj.ID && e.compGizmoDrag.component == compName {
				activeID = e.compGizmoDrag.handle.ID
			}
			drawGizmoHandles3D(d, e.activeSpecPalette(), proj, worldMat, handles, activeID)
		}
	}

	var selectedObj *sceneformat.ObjectDoc
	var selectedMat math3d.Mat4
	doc := e.resolvedDoc()
	if doc == nil {
		return
	}
	for li := range doc.Layers {
		layer := &doc.Layers[li]
		if !layer.IsVisible() || layer.EffectiveSpace() == sceneformat.SpaceScreen {
			continue
		}
		var walk func(objs []sceneformat.ObjectDoc, parentMat math3d.Mat4)
		walk = func(objs []sceneformat.ObjectDoc, parentMat math3d.Mat4) {
			for oi := range objs {
				o := &objs[oi]
				if e.hidden[o.ID] {
					continue
				}
				posed := e.posed3D(o)
				worldMat := parentMat.Mul(sceneformat.ObjectLocalTransform3D(posed).ToMat4())
				if o.ID == e.SelectedID {
					selectedObj = posed
					selectedMat = worldMat
				} else {
					drawForObject(posed, worldMat, false)
				}
				if len(o.Children) > 0 {
					walk(o.Children, worldMat)
				}
			}
		}
		walk(layer.Objects, math3d.Mat4Identity())
	}
	if selectedObj != nil && !e.viewportFrozen(selectedObj.ID) {
		drawForObject(selectedObj, selectedMat, true)
	}
}
