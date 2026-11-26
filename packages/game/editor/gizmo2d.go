package editor

import (
	"fmt"
	"math"
	"reflect"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// GizmoHandle2D names a part of the 2D transform gizmo drawn on the selected object, or the object itself, that a drag in the 2D viewport takes.
type GizmoHandle2D int

const (
	// Handle2DNone is no handle: a drag that starts away from the gizmo and every object.
	Handle2DNone GizmoHandle2D = iota
	// Handle2DBody is the object itself: a drag moves it freely, keeping the offset from where it was grabbed.
	Handle2DBody
	// Handle2DMove is the square at the object's origin: a drag moves it freely.
	Handle2DMove
	// Handle2DMoveX is the arrow along the object's own X axis: a drag moves it along that axis only.
	Handle2DMoveX
	// Handle2DMoveY is the arrow along the object's own Y axis: a drag moves it along that axis only.
	Handle2DMoveY
	// Handle2DRotate is the ring around the object: a drag turns it about its origin.
	Handle2DRotate
	// Handle2DScaleN is the middle of the outline's top edge, its local -Y side: a drag scales the object's Y about its origin.
	Handle2DScaleN
	// Handle2DScaleE is the middle of the outline's right edge, its local +X side: a drag scales the object's X about its origin.
	Handle2DScaleE
	// Handle2DScaleS is the middle of the outline's bottom edge, its local +Y side: a drag scales the object's Y about its origin.
	Handle2DScaleS
	// Handle2DScaleW is the middle of the outline's left edge, its local -X side: a drag scales the object's X about its origin.
	Handle2DScaleW
	// Handle2DScaleNE is the outline's top right corner: a drag scales the object's X and Y about its origin.
	Handle2DScaleNE
	// Handle2DScaleSE is the outline's bottom right corner: a drag scales the object's X and Y about its origin.
	Handle2DScaleSE
	// Handle2DScaleSW is the outline's bottom left corner: a drag scales the object's X and Y about its origin.
	Handle2DScaleSW
	// Handle2DScaleNW is the outline's top left corner: a drag scales the object's X and Y about its origin.
	Handle2DScaleNW
	// Handle2DLightRadius is the radius handle on a selected object with a light2d component: a drag adjusts light2d.radius.
	Handle2DLightRadius
	// Handle2DLightSpotAngle is either cone-edge handle on a selected spotlight (light2d kind "spot"): a drag adjusts light2d.angle.
	Handle2DLightSpotAngle
	// Handle2DLightSpotDirection is the aim handle along a selected spotlight's center ray: a drag adjusts light2d.direction.
	Handle2DLightSpotDirection
	// Handle2DOccluderVertex is a vertex handle on a selected polygon occluder2d: a drag moves that vertex in local space.
	Handle2DOccluderVertex
	// Handle2DOccluderEdgeMid is an edge-midpoint handle on a selected polygon occluder2d: a drag inserts a new vertex on that edge and moves it.
	Handle2DOccluderEdgeMid
	// Handle2DOccluderRectN is the top edge handle on a selected rect occluder2d: a drag resizes occluder2d.height.
	Handle2DOccluderRectN
	// Handle2DOccluderRectE is the right edge handle on a selected rect occluder2d: a drag resizes occluder2d.width.
	Handle2DOccluderRectE
	// Handle2DOccluderRectS is the bottom edge handle on a selected rect occluder2d: a drag resizes occluder2d.height.
	Handle2DOccluderRectS
	// Handle2DOccluderRectW is the left edge handle on a selected rect occluder2d: a drag resizes occluder2d.width.
	Handle2DOccluderRectW
	// Handle2DNavRegionVertex is a vertex handle on a selected navregion2d polygon: a drag moves that vertex in local space.
	Handle2DNavRegionVertex
	// Handle2DNavRegionEdgeMid is an edge-midpoint handle on a selected navregion2d polygon: a drag inserts a new vertex on that edge and moves it.
	Handle2DNavRegionEdgeMid
)

// String returns a human-readable label for the handle.
func (h GizmoHandle2D) String() string {
	switch h {
	case Handle2DBody:
		return "Body"
	case Handle2DMove:
		return "Move"
	case Handle2DMoveX:
		return "Move X"
	case Handle2DMoveY:
		return "Move Y"
	case Handle2DRotate:
		return "Rotate"
	case Handle2DScaleN:
		return "Scale N"
	case Handle2DScaleE:
		return "Scale E"
	case Handle2DScaleS:
		return "Scale S"
	case Handle2DScaleW:
		return "Scale W"
	case Handle2DScaleNE:
		return "Scale NE"
	case Handle2DScaleSE:
		return "Scale SE"
	case Handle2DScaleSW:
		return "Scale SW"
	case Handle2DScaleNW:
		return "Scale NW"
	case Handle2DLightRadius:
		return "Light Radius"
	case Handle2DLightSpotAngle:
		return "Spot Angle"
	case Handle2DLightSpotDirection:
		return "Spot Direction"
	case Handle2DOccluderVertex:
		return "Occluder Vertex"
	case Handle2DOccluderEdgeMid:
		return "Insert Occluder Vertex"
	case Handle2DOccluderRectN:
		return "Occluder Height N"
	case Handle2DOccluderRectE:
		return "Occluder Width E"
	case Handle2DOccluderRectS:
		return "Occluder Height S"
	case Handle2DOccluderRectW:
		return "Occluder Width W"
	case Handle2DNavRegionVertex:
		return "Nav Region Vertex"
	case Handle2DNavRegionEdgeMid:
		return "Insert Nav Region Vertex"
	default:
		return "None"
	}
}

// IsScale reports whether h is one of the corner or edge scale handles.
func (h GizmoHandle2D) IsScale() bool { return h >= Handle2DScaleN && h <= Handle2DScaleNW }

// IsLight reports whether h is one of the 2D light radius, spot-angle, or direction handles.
func (h GizmoHandle2D) IsLight() bool {
	return h >= Handle2DLightRadius && h <= Handle2DLightSpotDirection
}

// IsOccluder reports whether h is one of the 2D occluder polygon or rect shape handles.
func (h GizmoHandle2D) IsOccluder() bool {
	return h >= Handle2DOccluderVertex && h <= Handle2DOccluderRectW
}

// IsNavRegion reports whether h is one of the 2D navigation region polygon vertex or edge-midpoint handles.
func (h GizmoHandle2D) IsNavRegion() bool {
	return h == Handle2DNavRegionVertex || h == Handle2DNavRegionEdgeMid
}

// IsComponentHandle reports whether h edits a component (light2d, occluder2d, or navregion2d) rather than the object's 2D transform.
func (h GizmoHandle2D) IsComponentHandle() bool {
	return h.IsLight() || h.IsOccluder() || h.IsNavRegion()
}

// The 2D gizmo's sizes, in canvas points, which stay the same at any zoom.
const (
	// gizmoCenterHalf is half the side of the free-move square at the origin.
	gizmoCenterHalf float32 = 6
	// gizmoArrowLen is how far the axis arrows reach from the origin, and gizmoArrowStart where they start, past the square.
	gizmoArrowLen   float32 = 56
	gizmoArrowStart float32 = 12
	// gizmoArrowPick is how far from an arrow's line a press still takes it.
	gizmoArrowPick float32 = 6
	// gizmoRingMin is the rotation ring's smallest radius, and gizmoRingGap how far it stays outside the outline's farthest corner.
	gizmoRingMin float32 = 72
	gizmoRingGap float32 = 16
	// gizmoRingPick is how far from the ring a press still takes it.
	gizmoRingPick float32 = 6
	// gizmoScaleHalf is half the side of a scale handle's square.
	gizmoScaleHalf float32 = 5
	// gizmoScaleMinSide is the shortest outline side, on the canvas, that shows scale handles: below it they would cover the move square and each other, so zoom in to scale a small object with the handles.
	gizmoScaleMinSide float32 = 24
	// gizmoScaleMinReach is how far, on the canvas and along each axis it scales, a scale handle must sit from the origin to be offered: a handle on or next to the origin's line, as the top and left handles of a sprite whose origin is its top left corner are, could not say how far the pointer moved from the origin, and would scale wildly or not at all.
	gizmoScaleMinReach float32 = 2 * gizmoCenterHalf
	// minScale2D is the smallest scale magnitude a scale handle sets: below it the object would collapse to a point and its handles would vanish, leaving nothing to drag it back with.
	minScale2D = 1e-3
)

// transform2D is an object's 2D transform fields, its scale pointers kept nil where unset so Undo restores the file byte for byte.
type transform2D struct {
	X, Y, Rotation float32
	ScaleX, ScaleY *float32
}

func captureTransform2D(o *sceneformat.ObjectDoc) transform2D {
	return transform2D{X: o.X, Y: o.Y, Rotation: o.Rotation, ScaleX: cloneFloat32Ptr(o.ScaleX), ScaleY: cloneFloat32Ptr(o.ScaleY)}
}

func applyTransform2D(o *sceneformat.ObjectDoc, t transform2D) {
	o.X, o.Y, o.Rotation = t.X, t.Y, t.Rotation
	o.ScaleX, o.ScaleY = cloneFloat32Ptr(t.ScaleX), cloneFloat32Ptr(t.ScaleY)
}

func equalTransform2D(a, b transform2D) bool {
	return a.X == b.X && a.Y == b.Y && a.Rotation == b.Rotation && equalFloat32Ptr(a.ScaleX, b.ScaleX) && equalFloat32Ptr(a.ScaleY, b.ScaleY)
}

// drag2DTarget snapshots one object in a multi-object 2D viewport drag.
type drag2DTarget struct {
	id                   string
	space                sceneformat.Space
	parallaxX, parallaxY float32
	start                transform2D
	toParent             aff2
	startLayerPos        math2d.Vec2
	detSign              float64
}

// drag2DState is a 2D viewport drag in progress on objectID (or targets for a multi-selection),
// which the drag changes in place until EndDrag2D records it as one step or CancelDrag2D puts it back.
type drag2DState struct {
	handle               GizmoHandle2D
	objectID             string
	space                sceneformat.Space
	parallaxX, parallaxY float32
	start                transform2D
	// toParent maps the object's layer space to its parent's space, where its X, Y, rotation and scale are written: the identity for a top-level object.
	toParent aff2
	// grab is where the drag started, in the object's parent's space.
	grab math2d.Vec2
	// prevAngle is the pointer's angle about the origin at the last update, and angle how far it has turned in all, unwrapped, for the rotation ring.
	prevAngle, angle float64
	// Multi-selection drag state (populated when dragging >1 selected objects).
	targets     []drag2DTarget
	pivotCanvas math2d.Vec2
	grabCanvas  math2d.Vec2
	axisX       math2d.Vec2
	axisY       math2d.Vec2
	// Component-handle drag state (light2d radius/spot angle/direction and occluder2d rect/polygon handles).
	compName    string
	compBefore  map[string]any
	toLocal     aff2
	vertexIndex int
	// Override snapshot when dragging an instanced or inherited object.
	overrideInfo    ObjectOverrideInfo
	overrideOld     sceneformat.ObjectOverride
	overrideHadOld  bool
	overrideWasNil  bool
	overridePrevVer int
}

// gizmo2D is the selected object's gizmo as drawn on the canvas.
type gizmo2D struct {
	// origin is the object's origin on the canvas, and axisX and axisY its own unit X and Y axes there.
	origin, axisX, axisY math2d.Vec2
	// outline is the object's outline's corners, top left first, clockwise on screen for an unflipped object.
	outline [4]math2d.Vec2
	ring    float32
	// scale says, for each scale handle from Handle2DScaleN on, whether it is offered: the outline is large enough on the canvas, and the handle sits far enough from the origin along each axis it scales.
	scale [Handle2DScaleNW - Handle2DScaleN + 1]bool
}

// scaleShown reports whether the scale handle h is offered.
func (g gizmo2D) scaleShown(h GizmoHandle2D) bool {
	return h.IsScale() && g.scale[h-Handle2DScaleN]
}

// Gizmo2DShown reports whether the 2D viewport shows the transform gizmo: on the selected object(s), nested children included, in 2D, with the Select or Move tool, outside Play. Objects hidden or locked in the editor (SetObjectHidden, SetObjectLocked), or under one that is, have no gizmo.
func (e *Editor) Gizmo2DShown() bool {
	if e == nil || e.playing || e.ViewMode != ViewMode2D || (e.Tool != ToolSelect && e.Tool != ToolMove) {
		return false
	}
	if len(e.draggableSelectedIDs()) == 0 {
		return false
	}
	_, ok := e.gizmo2D()
	return ok
}

// gizmo2D lays out the selected object's (or multi-selection's) gizmo on the canvas from the scene as drawn.
func (e *Editor) gizmo2D() (gizmo2D, bool) {
	scene := e.ActiveScene()
	if scene == nil || e.SelectedID == "" {
		return gizmo2D{}, false
	}
	draggable := e.draggableSelectedIDs()
	if len(draggable) > 1 {
		return e.multiGizmo2D(scene, draggable)
	}
	targetID := e.SelectedID
	if len(draggable) == 1 {
		targetID = draggable[0]
	}
	bo := scene.Object(targetID)
	if bo == nil || bo.Doc == nil {
		return gizmo2D{}, false
	}
	space := sceneformat.SpaceWorld
	var px, py float32 = 1, 1
	if layer := scene.Layer(bo.Layer); layer != nil {
		if !layer.Visible {
			return gizmo2D{}, false
		}
		space = layer.Space
		px, py = layer.ParallaxX, layer.ParallaxY
	}
	toCanvas := e.layerAffineFor(space, px, py)
	m := toCanvas.Mul(bo.World)
	var g gizmo2D
	g.origin = m.TransformPoint(math2d.Vec2{})
	// The object's own axes are its parent's space turned by its rotation, before its own scale.
	parent := toCanvas
	if bo.Parent != nil {
		parent = toCanvas.Mul(bo.Parent.World)
	}
	turn := parent.Mul(math2d.Rotate(bo.Doc.Rotation))
	g.axisX = unitVec(turn.TransformVector(math2d.Vec2{X: 1}))
	g.axisY = unitVec(turn.TransformVector(math2d.Vec2{Y: 1}))
	rx, ry, rw, rh := objectOutline(bo)
	for i, c := range [4][2]float32{{rx, ry}, {rx + rw, ry}, {rx + rw, ry + rh}, {rx, ry + rh}} {
		g.outline[i] = m.TransformPoint(math2d.Vec2{X: c[0], Y: c[1]})
	}
	var far float32
	for _, c := range g.outline {
		far = max(far, vecLen(c.Sub(g.origin)))
	}
	g.ring = max(gizmoRingMin, far+gizmoRingGap)
	if vecLen(g.outline[1].Sub(g.outline[0])) >= gizmoScaleMinSide && vecLen(g.outline[3].Sub(g.outline[0])) >= gizmoScaleMinSide {
		for h := Handle2DScaleN; h <= Handle2DScaleNW; h++ {
			lx, ly := scaleHandleLocal(h, rx, ry, rw, rh)
			reachX := vecLen(m.TransformVector(math2d.Vec2{X: lx}))
			reachY := vecLen(m.TransformVector(math2d.Vec2{Y: ly}))
			sx, sy := h.scalesAxes()
			g.scale[h-Handle2DScaleN] = (!sx || reachX >= gizmoScaleMinReach) && (!sy || reachY >= gizmoScaleMinReach)
		}
	}
	if !isFiniteVec(g.origin) || !isFiniteFloat32(g.ring) {
		return gizmo2D{}, false
	}
	return g, true
}

// multiGizmo2D lays out a shared gizmo centered on the combined canvas bounding box of ids.
func (e *Editor) multiGizmo2D(scene *sceneformat.BuiltScene, ids []string) (gizmo2D, bool) {
	va := e.viewAffine()
	axisX := unitVec(va.TransformVector(math2d.Vec2{X: 1}))
	axisY := unitVec(va.TransformVector(math2d.Vec2{Y: 1}))

	hasAny := false
	var minU, maxU, minV, maxV float32
	for _, id := range ids {
		bo := scene.Object(id)
		if bo == nil || bo.Doc == nil {
			continue
		}
		space := sceneformat.SpaceWorld
		var px, py float32 = 1, 1
		if layer := scene.Layer(bo.Layer); layer != nil {
			if !layer.Visible {
				continue
			}
			space = layer.Space
			px, py = layer.ParallaxX, layer.ParallaxY
		}
		m := e.layerAffineFor(space, px, py).Mul(bo.World)
		rx, ry, rw, rh := objectOutline(bo)
		for _, c := range [4][2]float32{{rx, ry}, {rx + rw, ry}, {rx + rw, ry + rh}, {rx, ry + rh}} {
			p := m.TransformPoint(math2d.Vec2{X: c[0], Y: c[1]})
			u := p.X*axisX.X + p.Y*axisX.Y
			v := p.X*axisY.X + p.Y*axisY.Y
			if !hasAny {
				minU, maxU, minV, maxV = u, u, v, v
				hasAny = true
			} else {
				minU, maxU = min(minU, u), max(maxU, u)
				minV, maxV = min(minV, v), max(maxV, v)
			}
		}
	}
	if !hasAny {
		return gizmo2D{}, false
	}
	pt := func(u, v float32) math2d.Vec2 {
		return axisX.Scale(u).Add(axisY.Scale(v))
	}
	var g gizmo2D
	g.axisX = axisX
	g.axisY = axisY
	g.origin = pt((minU+maxU)/2, (minV+maxV)/2)
	g.outline = [4]math2d.Vec2{
		pt(minU, minV),
		pt(maxU, minV),
		pt(maxU, maxV),
		pt(minU, maxV),
	}
	var far float32
	for _, c := range g.outline {
		far = max(far, vecLen(c.Sub(g.origin)))
	}
	g.ring = max(gizmoRingMin, far+gizmoRingGap)
	w, hSide := maxU-minU, maxV-minV
	if w >= gizmoScaleMinSide && hSide >= gizmoScaleMinSide {
		reachX, reachY := w/2, hSide/2
		for h := Handle2DScaleN; h <= Handle2DScaleNW; h++ {
			sx, sy := h.scalesAxes()
			g.scale[h-Handle2DScaleN] = (!sx || reachX >= gizmoScaleMinReach) && (!sy || reachY >= gizmoScaleMinReach)
		}
	}
	if !isFiniteVec(g.origin) || !isFiniteFloat32(g.ring) {
		return gizmo2D{}, false
	}
	return g, true
}

// scalesAxes reports which of the object's axes the scale handle h scales: the edges one, the corners both.
func (h GizmoHandle2D) scalesAxes() (x, y bool) {
	x = h == Handle2DScaleE || h == Handle2DScaleW || h == Handle2DScaleNE || h == Handle2DScaleSE || h == Handle2DScaleSW || h == Handle2DScaleNW
	y = h == Handle2DScaleN || h == Handle2DScaleS || h == Handle2DScaleNE || h == Handle2DScaleSE || h == Handle2DScaleSW || h == Handle2DScaleNW
	return x, y
}

// scaleHandleLocal returns where the scale handle h sits in the object's own space, on the outline (rx, ry, rw, rh).
func scaleHandleLocal(h GizmoHandle2D, rx, ry, rw, rh float32) (float32, float32) {
	switch h {
	case Handle2DScaleN:
		return rx + rw/2, ry
	case Handle2DScaleE:
		return rx + rw, ry + rh/2
	case Handle2DScaleS:
		return rx + rw/2, ry + rh
	case Handle2DScaleW:
		return rx, ry + rh/2
	case Handle2DScaleNE:
		return rx + rw, ry
	case Handle2DScaleSE:
		return rx + rw, ry + rh
	case Handle2DScaleSW:
		return rx, ry + rh
	default:
		return rx, ry
	}
}

// scaleHandlePos returns where the scale handle h sits on the canvas.
func (g gizmo2D) scaleHandlePos(h GizmoHandle2D) math2d.Vec2 {
	o := g.outline
	mid := func(a, b math2d.Vec2) math2d.Vec2 { return math2d.Vec2{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2} }
	switch h {
	case Handle2DScaleN:
		return mid(o[0], o[1])
	case Handle2DScaleE:
		return mid(o[1], o[2])
	case Handle2DScaleS:
		return mid(o[2], o[3])
	case Handle2DScaleW:
		return mid(o[3], o[0])
	case Handle2DScaleNE:
		return o[1]
	case Handle2DScaleSE:
		return o[2]
	case Handle2DScaleSW:
		return o[3]
	default:
		return o[0]
	}
}

// pick returns the handle under the canvas point p, in the order a press takes them where they overlap: the move square, the scale handles offered, the axis arrows, then the ring.
func (g gizmo2D) pick(p math2d.Vec2) GizmoHandle2D {
	if abs32(p.X-g.origin.X) <= gizmoCenterHalf && abs32(p.Y-g.origin.Y) <= gizmoCenterHalf {
		return Handle2DMove
	}
	for h := Handle2DScaleN; h <= Handle2DScaleNW; h++ {
		if !g.scaleShown(h) {
			continue
		}
		c := g.scaleHandlePos(h)
		if abs32(p.X-c.X) <= gizmoScaleHalf && abs32(p.Y-c.Y) <= gizmoScaleHalf {
			return h
		}
	}
	for _, a := range [2]struct {
		h   GizmoHandle2D
		dir math2d.Vec2
	}{{Handle2DMoveX, g.axisX}, {Handle2DMoveY, g.axisY}} {
		d := p.Sub(g.origin)
		along := d.X*a.dir.X + d.Y*a.dir.Y
		across := d.X*a.dir.Y - d.Y*a.dir.X
		if along >= gizmoArrowStart && along <= gizmoArrowLen && abs32(across) <= gizmoArrowPick {
			return a.h
		}
	}
	if abs32(vecLen(p.Sub(g.origin))-g.ring) <= gizmoRingPick {
		return Handle2DRotate
	}
	return Handle2DNone
}

// PickGizmo2D returns the part of the selected object's 2D gizmo under the canvas point (x, y), Handle2DBody over the selected object away from its handles, or Handle2DNone. It is Handle2DNone whenever the gizmo is not shown (Gizmo2DShown).
func (e *Editor) PickGizmo2D(x, y float32) GizmoHandle2D {
	h, _ := e.pickGizmo2DWithIndex(x, y)
	return h
}

func (e *Editor) pickGizmo2DWithIndex(x, y float32) (GizmoHandle2D, int) {
	if !e.Gizmo2DShown() {
		return Handle2DNone, -1
	}
	g, _ := e.gizmo2D()
	p := math2d.Vec2{X: x, Y: y}
	if abs32(p.X-g.origin.X) <= gizmoCenterHalf && abs32(p.Y-g.origin.Y) <= gizmoCenterHalf {
		return Handle2DMove, -1
	}
	if e.SelectionCount() == 1 {
		if lh, idx := e.pickLightingHandle2D(x, y); lh != Handle2DNone {
			return lh, idx
		}
		if nh, idx := e.pickNavRegionHandle2D(x, y); nh != Handle2DNone {
			return nh, idx
		}
	}
	if h := g.pick(p); h != Handle2DNone {
		return h, -1
	}
	if hit := e.HitTest(x, y); hit != "" && e.IsSelected(hit) {
		return Handle2DBody, -1
	}
	return Handle2DNone, -1
}

// Dragging2D returns the handle a 2D viewport drag in progress holds, or Handle2DNone when none is.
func (e *Editor) Dragging2D() GizmoHandle2D {
	if e == nil {
		return Handle2DNone
	}
	return e.drag2D.handle
}

// BeginDrag2D starts a drag in the 2D viewport at the canvas point (x, y), where the pointer went down, with the Select or Move tool: on a handle of the selected object's gizmo (PickGizmo2D) it takes that handle, and on another object it selects that object and moves it. A nested child is dragged in its parent's space, so it follows the pointer under a turned or scaled parent. It returns the handle taken, Handle2DNone when the press is on nothing, another drag is under way, the tool or view mode is another, the scene plays, or the object is hidden or locked in the editor or under one that is. The drag changes the object as UpdateDrag2D says and is recorded by EndDrag2D as one undoable step.
func (e *Editor) BeginDrag2D(x, y float32) GizmoHandle2D {
	if e == nil || e.playing || e.HistoryPreviewActive() || e.ViewMode != ViewMode2D || (e.Tool != ToolSelect && e.Tool != ToolMove) || e.drag2D.handle != Handle2DNone || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return Handle2DNone
	}
	h, idx := e.pickGizmo2DWithIndex(x, y)
	if h.IsNavRegion() {
		return e.beginNavRegionDrag2D(h, idx, x, y)
	}
	if h.IsComponentHandle() {
		return e.beginLightingDrag2D(h, idx, x, y)
	}
	anchorID := ""
	if h == Handle2DNone {
		hit := e.HitTest(x, y)
		if hit == "" {
			return Handle2DNone
		}
		if obj, _ := e.FindResolvedObject(hit); obj == nil || e.viewportFrozen(hit) {
			return Handle2DNone
		}
		if !e.IsSelected(hit) {
			e.SelectObject(hit)
		}
		anchorID = hit
		h = Handle2DBody
	}
	draggable := e.draggableSelectedIDs()
	if len(draggable) > 1 {
		return e.beginMultiDrag2D(h, draggable, anchorID, x, y)
	}
	targetID := e.SelectedID
	if len(draggable) == 1 {
		targetID = draggable[0]
	}
	obj, layerName := e.FindResolvedObject(targetID)
	if obj == nil || e.viewportFrozen(targetID) {
		return Handle2DNone
	}
	info := e.ObjectOverrideInfo(targetID)
	var (
		oldOv     sceneformat.ObjectOverride
		hadOld    bool
		wasNilMap bool
		prevVer   int
	)
	if info.Origin != ObjectLocal {
		if e.BuildErr != nil {
			e.report(e.BuildErr)
			return Handle2DNone
		}
		var ok bool
		oldOv, hadOld, wasNilMap, ok = e.currentOverrideFor(e.Doc, info)
		if !ok {
			return Handle2DNone
		}
		prevVer = e.Doc.Version
	}
	// A nested child's transform is in its parent's space, so the drag is worked out there.
	path := e.resolvedObjectPath(targetID)
	toParent, ok := chain2(path[:max(len(path)-1, 0)]).invert()
	if !ok {
		return Handle2DNone
	}
	space, px, py := e.layerSpaceAndParallax(layerName)
	lx, ly := e.canvasToLayerFor(space, px, py, x, y)
	gx, gy := toParent.apply(float64(lx), float64(ly))
	st := drag2DState{
		handle:          h,
		objectID:        targetID,
		space:           space,
		parallaxX:       px,
		parallaxY:       py,
		start:           captureTransform2D(obj),
		toParent:        toParent,
		grab:            math2d.Vec2{X: float32(gx), Y: float32(gy)},
		overrideInfo:    info,
		overrideOld:     oldOv,
		overrideHadOld:  hadOld,
		overrideWasNil:  wasNilMap,
		overridePrevVer: prevVer,
	}
	st.prevAngle = math.Atan2(gy-float64(obj.Y), gx-float64(obj.X))
	e.drag2D = st
	e.Status = h.String() + " " + st.objectID
	e.notify()
	return h
}

func (e *Editor) beginMultiDrag2D(h GizmoHandle2D, ids []string, anchorID string, x, y float32) GizmoHandle2D {
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return Handle2DNone
	}
	g, ok := e.gizmo2D()
	if !ok {
		return Handle2DNone
	}
	targets := make([]drag2DTarget, 0, len(ids))
	for _, id := range ids {
		obj, li, _ := e.FindObject(id)
		if obj == nil || e.viewportFrozen(id) {
			continue
		}
		path := objectPath(e.Doc, id)
		parentChain := chain2(path[:max(len(path)-1, 0)])
		toParent, ok := parentChain.invert()
		if !ok {
			continue
		}
		space, px, py := e.layerSpaceAndParallax(e.Doc.Layers[li].Name)
		lx, ly := parentChain.apply(float64(obj.X), float64(obj.Y))
		detSign := 1.0
		if toParent.a*toParent.d-toParent.b*toParent.c < 0 {
			detSign = -1.0
		}
		targets = append(targets, drag2DTarget{
			id:            id,
			space:         space,
			parallaxX:     px,
			parallaxY:     py,
			start:         captureTransform2D(obj),
			toParent:      toParent,
			startLayerPos: math2d.Vec2{X: float32(lx), Y: float32(ly)},
			detSign:       detSign,
		})
	}
	if len(targets) == 0 {
		return Handle2DNone
	}
	if anchorID == "" {
		anchorID = e.SelectedID
	}
	st := drag2DState{
		handle:      h,
		objectID:    anchorID,
		targets:     targets,
		pivotCanvas: g.origin,
		grabCanvas:  math2d.Vec2{X: x, Y: y},
		axisX:       g.axisX,
		axisY:       g.axisY,
		prevAngle:   math.Atan2(float64(y-g.origin.Y), float64(x-g.origin.X)),
	}
	e.drag2D = st
	e.Status = fmt.Sprintf("%s %d objects", h.String(), len(targets))
	e.notify()
	return h
}

// UpdateDrag2D moves the drag in progress to the canvas point (x, y) and changes the object to match, from how it was when the drag began:
//
//   - the body and the move square move it by the distance the pointer travelled in its parent's space (its layer's space for a top-level object), which under a rotated or zoomed view or camera is the world distance, and the X and Y arrows by that distance along the object's own axis;
//   - the ring turns it about its origin by the angle the pointer turned there;
//   - an edge handle scales its X or Y, and a corner both, about its origin, so the point grabbed follows the pointer; keepAspect (Shift) scales both by the same factor, keeping the aspect ratio.
//
// A scale that would be zero or close to it, or a scale or rotation that would shear a sprite in the object's tree, its ancestors taken into account (as ScaleObject and RotateObject refuse), is not taken: the object keeps its last valid transform and the status bar says why. It does nothing without a drag.
func (e *Editor) UpdateDrag2D(x, y float32, keepAspect bool) {
	if e == nil || e.drag2D.handle == Handle2DNone || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return
	}
	if e.drag2D.handle.IsNavRegion() {
		e.updateNavRegionDrag2D(x, y)
		return
	}
	if e.drag2D.handle.IsComponentHandle() {
		e.updateLightingDrag2D(x, y, keepAspect)
		return
	}
	if len(e.drag2D.targets) > 0 {
		e.updateMultiDrag2D(x, y, keepAspect)
		return
	}
	st := &e.drag2D
	obj, _ := e.FindResolvedObject(st.objectID)
	if obj == nil || e.playing {
		e.drag2D = drag2DState{}
		e.notify()
		return
	}
	lx, ly := e.canvasToLayerFor(st.space, st.parallaxX, st.parallaxY, x, y)
	px, py := st.toParent.apply(float64(lx), float64(ly))
	s := st.start
	dx, dy := px-float64(st.grab.X), py-float64(st.grab.Y)
	sin, cos := math.Sincos(float64(s.Rotation))
	next := s
	switch h := st.handle; {
	case h == Handle2DBody || h == Handle2DMove:
		nx, ny := e.SnapPosition2D(float64(s.X)+dx, float64(s.Y)+dy)
		next.X, next.Y = float32(nx), float32(ny)
	case h == Handle2DMoveX:
		along := dx*cos + dy*sin
		if s.Rotation == 0 {
			next.X = float32(e.SnapAxis2D(float64(s.X)+along, 0))
			next.Y = s.Y
		} else {
			along = e.SnapValue(along)
			next.X, next.Y = float32(float64(s.X)+along*cos), float32(float64(s.Y)+along*sin)
		}
	case h == Handle2DMoveY:
		along := -dx*sin + dy*cos
		if s.Rotation == 0 {
			next.X = s.X
			next.Y = float32(e.SnapAxis2D(float64(s.Y)+along, 1))
		} else {
			along = e.SnapValue(along)
			next.X, next.Y = float32(float64(s.X)-along*sin), float32(float64(s.Y)+along*cos)
		}
	case h == Handle2DRotate:
		ox, oy := px-float64(s.X), py-float64(s.Y)
		if math.Hypot(ox, oy) < 1e-6 {
			return
		}
		a := math.Atan2(oy, ox)
		step := math.Remainder(a-st.prevAngle, 2*math.Pi)
		st.angle += step
		st.prevAngle = a
		next.Rotation = float32(e.SnapRotationRadians(float64(s.Rotation) + st.angle))
		if !isFiniteFloat32(next.Rotation) {
			return
		}
		if err := e.checkTransformEdit(st.objectID, func(o *sceneformat.ObjectDoc) { o.Rotation = next.Rotation }); err != nil {
			e.Status = "Rotate refused: " + err.Error()
			e.notify()
			return
		}
	case h.IsScale():
		var snapScale func(float64) float64
		if e.scaleSnapActive() {
			snapScale = e.SnapScaleFactor
		}
		sx, sy, ok := scaleFromDragWithSnap(h, s, st.grab, px, py, keepAspect, snapScale)
		if !ok {
			e.Status = "Scale refused: the scale would be zero"
			e.notify()
			return
		}
		next.ScaleX, next.ScaleY = sx, sy
		if err := e.checkTransformEdit(st.objectID, func(o *sceneformat.ObjectDoc) { o.ScaleX, o.ScaleY = sx, sy }); err != nil {
			e.Status = "Scale refused: " + err.Error()
			e.notify()
			return
		}
	}
	if !isFiniteFloat32(next.X) || !isFiniteFloat32(next.Y) || !isFiniteFloat32(next.Rotation) {
		return
	}
	if st.overrideInfo.Origin != ObjectLocal {
		nextOv := cloneResolvedObjectOverride(st.overrideOld)
		switch h := st.handle; {
		case h == Handle2DBody || h == Handle2DMove || h == Handle2DMoveX || h == Handle2DMoveY:
			nextOv.X = sceneformat.Float32(next.X)
			nextOv.Y = sceneformat.Float32(next.Y)
		case h == Handle2DRotate:
			nextOv.Rotation = sceneformat.Float32(next.Rotation)
		case h.IsScale():
			sxAxis, syAxis := h.scalesAxes()
			if sxAxis || keepAspect {
				nextOv.ScaleX = cloneFloat32Ptr(next.ScaleX)
			}
			if syAxis || keepAspect {
				nextOv.ScaleY = cloneFloat32Ptr(next.ScaleY)
			}
		}
		if e.Doc.Version < 2 {
			e.Doc.Version = 2
		}
		setOverrideOnDoc(e.Doc, st.overrideInfo, nextOv, !isEmptyObjectOverride(nextOv), st.overrideWasNil)
		e.Rebuild()
		e.notify()
		return
	}
	applyTransform2D(obj, next)
	e.Rebuild()
	e.notify()
}

func (e *Editor) updateMultiDrag2D(x, y float32, keepAspect bool) {
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return
	}
	if e.playing {
		e.drag2D = drag2DState{}
		e.notify()
		return
	}
	st := &e.drag2D
	nexts := make([]transform2D, len(st.targets))
	objs := make([]*sceneformat.ObjectDoc, len(st.targets))
	for i, t := range st.targets {
		obj, _, _ := e.FindObject(t.id)
		if obj == nil {
			e.drag2D = drag2DState{}
			e.notify()
			return
		}
		objs[i] = obj
		nexts[i] = t.start
	}

	cur := math2d.Vec2{X: x, Y: y}
	switch h := st.handle; {
	case h == Handle2DBody || h == Handle2DMove || h == Handle2DMoveX || h == Handle2DMoveY:
		cd := cur.Sub(st.grabCanvas)
		switch h {
		case Handle2DMoveX:
			along := cd.X*st.axisX.X + cd.Y*st.axisX.Y
			cd = st.axisX.Scale(along)
		case Handle2DMoveY:
			along := cd.X*st.axisY.X + cd.Y*st.axisY.Y
			cd = st.axisY.Scale(along)
		}
		dstCanvas := st.grabCanvas.Add(cd)
		if grid, pixel := e.positionSnapMode(); grid || pixel {
			anchor := st.targets[0]
			for _, t := range st.targets {
				if t.id == st.objectID {
					anchor = t
					break
				}
			}
			glx, gly := e.canvasToLayerFor(anchor.space, anchor.parallaxX, anchor.parallaxY, st.grabCanvas.X, st.grabCanvas.Y)
			dlx, dly := e.canvasToLayerFor(anchor.space, anchor.parallaxX, anchor.parallaxY, dstCanvas.X, dstCanvas.Y)
			rawX := float64(anchor.startLayerPos.X) + float64(dlx-glx)
			rawY := float64(anchor.startLayerPos.Y) + float64(dly-gly)
			var snX, snY float64
			switch h {
			case Handle2DMoveX:
				snX = e.SnapAxis2D(rawX, 0)
				snY = rawY
			case Handle2DMoveY:
				snX = rawX
				snY = e.SnapAxis2D(rawY, 1)
			default:
				snX, snY = e.SnapPosition2D(rawX, rawY)
			}
			ldx := snX - float64(anchor.startLayerPos.X)
			ldy := snY - float64(anchor.startLayerPos.Y)
			for i, t := range st.targets {
				npx, npy := t.toParent.apply(float64(t.startLayerPos.X)+ldx, float64(t.startLayerPos.Y)+ldy)
				nexts[i].X = float32(npx)
				nexts[i].Y = float32(npy)
			}
		} else {
			for i, t := range st.targets {
				glx, gly := e.canvasToLayerFor(t.space, t.parallaxX, t.parallaxY, st.grabCanvas.X, st.grabCanvas.Y)
				dlx, dly := e.canvasToLayerFor(t.space, t.parallaxX, t.parallaxY, dstCanvas.X, dstCanvas.Y)
				gpx, gpy := t.toParent.apply(float64(glx), float64(gly))
				dpx, dpy := t.toParent.apply(float64(dlx), float64(dly))
				nexts[i].X = float32(float64(t.start.X) + (dpx - gpx))
				nexts[i].Y = float32(float64(t.start.Y) + (dpy - gpy))
			}
		}
	case h == Handle2DRotate:
		ox, oy := float64(x-st.pivotCanvas.X), float64(y-st.pivotCanvas.Y)
		dist := math.Hypot(ox, oy)
		if math.IsNaN(dist) || math.IsInf(dist, 0) || dist < 1e-6 {
			return
		}
		a := math.Atan2(oy, ox)
		step := math.Remainder(a-st.prevAngle, 2*math.Pi)
		if math.IsNaN(a) || math.IsInf(a, 0) || math.IsNaN(step) || math.IsInf(step, 0) {
			return
		}
		nextAngle := st.angle + step
		appliedAngle := e.SnapRotationRadians(nextAngle)
		sin, cos := math.Sincos(appliedAngle)
		for i, t := range st.targets {
			plx, ply := e.canvasToLayerFor(t.space, t.parallaxX, t.parallaxY, st.pivotCanvas.X, st.pivotCanvas.Y)
			rx := float64(t.startLayerPos.X) - float64(plx)
			ry := float64(t.startLayerPos.Y) - float64(ply)
			nlx := float64(plx) + rx*cos - ry*sin
			nly := float64(ply) + rx*sin + ry*cos
			npx, npy := t.toParent.apply(nlx, nly)
			nexts[i].X = float32(npx)
			nexts[i].Y = float32(npy)
			nexts[i].Rotation = float32(float64(t.start.Rotation) + t.detSign*appliedAngle)
			if !isFiniteFloat32(nexts[i].X) || !isFiniteFloat32(nexts[i].Y) || !isFiniteFloat32(nexts[i].Rotation) {
				return
			}
			if err := e.checkTransformEdit(t.id, func(o *sceneformat.ObjectDoc) { o.Rotation = nexts[i].Rotation }); err != nil {
				e.Status = "Rotate refused: " + err.Error()
				e.notify()
				return
			}
		}
		st.angle = nextAngle
		st.prevAngle = a
	case h.IsScale():
		// Compute scale factors along the gizmo's X and Y axes about pivotCanvas.
		proj := func(p math2d.Vec2) (float64, float64) {
			d := p.Sub(st.pivotCanvas)
			return float64(d.X*st.axisX.X + d.Y*st.axisX.Y), float64(d.X*st.axisY.X + d.Y*st.axisY.Y)
		}
		gx, gy := proj(st.grabCanvas)
		qx, qy := proj(cur)
		scaleX, scaleY := h.scalesAxes()
		const tiny = 1e-6
		fx, fy := 1.0, 1.0
		switch {
		case keepAspect && scaleX && scaleY:
			d := gx*gx + gy*gy
			if d < tiny {
				e.Status = "Scale refused: the scale would be zero"
				e.notify()
				return
			}
			fx = (qx*gx + qy*gy) / d
			fy = fx
		case scaleX:
			if math.Abs(gx) < tiny {
				e.Status = "Scale refused: the scale would be zero"
				e.notify()
				return
			}
			fx = qx / gx
			if scaleY {
				if math.Abs(gy) < tiny {
					e.Status = "Scale refused: the scale would be zero"
					e.notify()
					return
				}
				fy = qy / gy
			} else if keepAspect {
				fy = fx
			}
		case scaleY:
			if math.Abs(gy) < tiny {
				e.Status = "Scale refused: the scale would be zero"
				e.notify()
				return
			}
			fy = qy / gy
			if keepAspect {
				fx = fy
			}
		}
		if e.scaleSnapActive() {
			if scaleX || keepAspect {
				fx = e.SnapScaleFactor(fx)
			}
			if scaleY || keepAspect {
				if keepAspect && scaleX && scaleY {
					fy = fx
				} else {
					fy = e.SnapScaleFactor(fy)
				}
			}
		}
		if math.IsNaN(fx) || math.IsInf(fx, 0) || math.IsNaN(fy) || math.IsInf(fy, 0) ||
			(fx != 1 && math.Abs(fx) < minScale2D) || (fy != 1 && math.Abs(fy) < minScale2D) {
			e.Status = "Scale refused: the scale would be zero"
			e.notify()
			return
		}
		scaledPtr := func(base *float32, f float64) (*float32, bool) {
			if f == 1 {
				return cloneFloat32Ptr(base), true
			}
			b := 1.0
			if base != nil {
				b = float64(*base)
			}
			v := float32(b * f)
			if !isFiniteFloat32(v) || math.Abs(float64(v)) < minScale2D {
				return nil, false
			}
			return &v, true
		}
		for i, t := range st.targets {
			plx, ply := e.canvasToLayerFor(t.space, t.parallaxX, t.parallaxY, st.pivotCanvas.X, st.pivotCanvas.Y)
			rx := float64(t.startLayerPos.X) - float64(plx)
			ry := float64(t.startLayerPos.Y) - float64(ply)
			nlx := float64(plx) + rx*fx
			nly := float64(ply) + ry*fy
			npx, npy := t.toParent.apply(nlx, nly)
			nexts[i].X = float32(npx)
			nexts[i].Y = float32(npy)
			sxPtr, okX := scaledPtr(t.start.ScaleX, fx)
			syPtr, okY := scaledPtr(t.start.ScaleY, fy)
			if !okX || !okY {
				e.Status = "Scale refused: the scale would be zero"
				e.notify()
				return
			}
			nexts[i].ScaleX, nexts[i].ScaleY = sxPtr, syPtr
			if err := e.checkTransformEdit(t.id, func(o *sceneformat.ObjectDoc) { o.ScaleX, o.ScaleY = sxPtr, syPtr }); err != nil {
				e.Status = "Scale refused: " + err.Error()
				e.notify()
				return
			}
		}
	}
	for i := range nexts {
		if !isFiniteFloat32(nexts[i].X) || !isFiniteFloat32(nexts[i].Y) || !isFiniteFloat32(nexts[i].Rotation) {
			return
		}
	}
	for i, obj := range objs {
		applyTransform2D(obj, nexts[i])
	}
	e.Rebuild()
	e.notify()
}

// scaleFromDrag returns the scale a drag of the scale handle h to the point (px, py) of the object's parent's space gives an object whose transform was s when it was grabbed at grab: each axis the handle moves scales by how much farther from the origin, along the object's own axis, the pointer is than the grab point was. It reports false for a scale whose magnitude would be below minScale2D or that is not finite. An axis the handle does not move keeps its pointer, nil included.
func scaleFromDrag(h GizmoHandle2D, s transform2D, grab math2d.Vec2, px, py float64, keepAspect bool) (sx, sy *float32, ok bool) {
	return scaleFromDragWithSnap(h, s, grab, px, py, keepAspect, nil)
}

func scaleFromDragWithSnap(h GizmoHandle2D, s transform2D, grab math2d.Vec2, px, py float64, keepAspect bool, snapFn func(float64) float64) (sx, sy *float32, ok bool) {
	sin, cos := math.Sincos(float64(s.Rotation))
	local := func(x, y float64) (float64, float64) {
		x, y = x-float64(s.X), y-float64(s.Y)
		return x*cos + y*sin, -x*sin + y*cos
	}
	gx, gy := local(float64(grab.X), float64(grab.Y))
	qx, qy := local(px, py)
	scaleX, scaleY := h.scalesAxes()
	// A grab point on the origin's line cannot say how far the pointer moved away from it.
	const tiny = 1e-6
	fx, fy := 1.0, 1.0
	switch {
	case keepAspect && scaleX && scaleY:
		d := gx*gx + gy*gy
		if d < tiny {
			return nil, nil, false
		}
		fx = (qx*gx + qy*gy) / d
		fy = fx
	case scaleX:
		if math.Abs(gx) < tiny {
			return nil, nil, false
		}
		fx = qx / gx
		if scaleY {
			if math.Abs(gy) < tiny {
				return nil, nil, false
			}
			fy = qy / gy
		} else if keepAspect {
			fy = fx
		}
	case scaleY:
		if math.Abs(gy) < tiny {
			return nil, nil, false
		}
		fy = qy / gy
		if keepAspect {
			fx = fy
		}
	}
	// When keepAspect and scale snapping are both active, snap the primary axis's final scale and derive the shared factor from it so aspect ratio stays exact.
	if snapFn != nil && keepAspect {
		primaryBase := float64(1)
		primaryF := fx
		if !scaleX && scaleY {
			if s.ScaleY != nil {
				primaryBase = float64(*s.ScaleY)
			}
			primaryF = fy
		} else if s.ScaleX != nil {
			primaryBase = float64(*s.ScaleX)
		}
		if math.Abs(primaryBase) >= minScale2D {
			snapped := snapFn(primaryBase * primaryF)
			fx = snapped / primaryBase
			fy = fx
		}
		snapFn = nil
	}
	// A factor of 1 keeps the axis's pointer, nil included, so an axis the drag leaves alone does not gain a scale field in the file.
	scaled := func(base *float32, f float64, moved bool) (*float32, bool) {
		if !moved || (f == 1 && snapFn == nil) {
			return cloneFloat32Ptr(base), true
		}
		b := float64(1)
		if base != nil {
			b = float64(*base)
		}
		val := b * f
		if snapFn != nil {
			val = snapFn(val)
		}
		v := float32(val)
		if !isFiniteFloat32(v) || math.Abs(float64(v)) < minScale2D {
			return nil, false
		}
		if base == nil && v == 1 {
			return nil, true
		}
		return &v, true
	}
	movedX := scaleX || keepAspect
	movedY := scaleY || keepAspect
	if sx, ok = scaled(s.ScaleX, fx, movedX); !ok {
		return nil, nil, false
	}
	if sy, ok = scaled(s.ScaleY, fy, movedY); !ok {
		return nil, nil, false
	}
	return sx, sy, true
}

// EndDrag2D finishes the drag in progress and records what it changed as one undoable step, which Undo takes back to the object as it was before the drag. It reports false, recording nothing, when no drag is under way or the drag changed nothing.
func (e *Editor) EndDrag2D() bool {
	if e == nil || e.drag2D.handle == Handle2DNone {
		return false
	}
	if e.drag2D.handle.IsComponentHandle() {
		return e.endLightingDrag2D()
	}
	st := e.drag2D
	e.drag2D = drag2DState{}
	defer e.resumeAutoSaveAfterInteraction()
	if len(st.targets) > 0 {
		verb := "Move"
		switch {
		case st.handle == Handle2DRotate:
			verb = "Rotate"
		case st.handle.IsScale():
			verb = "Scale"
		}
		type pending struct {
			id    string
			obj   *sceneformat.ObjectDoc
			start transform2D
			after transform2D
		}
		var changed []pending
		for _, t := range st.targets {
			obj, _, _ := e.FindObject(t.id)
			if obj == nil {
				continue
			}
			after := captureTransform2D(obj)
			if !equalTransform2D(t.start, after) {
				changed = append(changed, pending{id: t.id, obj: obj, start: t.start, after: after})
			}
		}
		if len(changed) == 0 {
			e.Status = fmt.Sprintf("Selected %d objects", len(st.targets))
			e.notify()
			return false
		}
		for _, p := range changed {
			applyTransform2D(p.obj, p.start)
		}
		label := fmt.Sprintf("%s %d objects", verb, len(changed))
		_ = e.History.Group(label, func() error {
			for _, p := range changed {
				e.History.Execute(&transform2DCmd{ed: e, id: p.id, label: verb + " " + p.id, before: p.start, after: p.after})
			}
			return nil
		})
		e.Status = label
		e.notify()
		return true
	}
	if st.overrideInfo.Origin != ObjectLocal {
		afterOv, hasAfter, _, ok := e.currentOverrideFor(e.Doc, st.overrideInfo)
		// Restore pre-drag state before recording through History.
		e.Doc.Version = st.overridePrevVer
		setOverrideOnDoc(e.Doc, st.overrideInfo, st.overrideOld, st.overrideHadOld, st.overrideWasNil)
		if !ok || (st.overrideHadOld == hasAfter && reflect.DeepEqual(st.overrideOld, afterOv)) {
			e.Rebuild()
			e.Status = "Selected " + st.objectID
			e.notify()
			return false
		}
		verb := "Move "
		switch {
		case st.handle == Handle2DRotate:
			verb = "Rotate "
		case st.handle.IsScale():
			verb = "Scale "
		}
		nextVer := st.overridePrevVer
		if hasAfter && nextVer < 2 {
			nextVer = 2
		}
		candidate := e.Doc.Clone()
		candidate.Version = nextVer
		setOverrideOnDoc(candidate, st.overrideInfo, afterOv, hasAfter, st.overrideWasNil)
		if _, err := e.checkCandidateScene(candidate); err != nil {
			e.Rebuild()
			e.report(err)
			return false
		}
		e.History.Execute(&objectOverrideCmd{
			ed:          e,
			info:        st.overrideInfo,
			label:       verb + st.objectID,
			oldOv:       st.overrideOld,
			hadOld:      st.overrideHadOld,
			wasNilMap:   st.overrideWasNil,
			newOv:       afterOv,
			hasNew:      hasAfter,
			prevVersion: st.overridePrevVer,
			nextVersion: nextVer,
		})
		return true
	}
	obj, _, _ := e.FindObject(st.objectID)
	if obj == nil {
		e.notify()
		return false
	}
	after := captureTransform2D(obj)
	if equalTransform2D(st.start, after) {
		e.Status = "Selected " + st.objectID
		e.notify()
		return false
	}
	// The object goes back to how it was before History applies the step, so Undo and Redo replay it exactly.
	applyTransform2D(obj, st.start)
	verb := "Move "
	switch {
	case st.handle == Handle2DRotate:
		verb = "Rotate "
	case st.handle.IsScale():
		verb = "Scale "
	}
	e.History.Execute(&transform2DCmd{ed: e, id: st.objectID, label: verb + st.objectID, before: st.start, after: after})
	return true
}

// CancelDrag2D ends the drag in progress, as Escape or a cancelled gesture does, putting the object back as it was before the drag and recording nothing. It reports false when no drag is under way.
func (e *Editor) CancelDrag2D() bool {
	if e == nil || e.drag2D.handle == Handle2DNone {
		return false
	}
	if e.drag2D.handle.IsComponentHandle() {
		return e.cancelLightingDrag2D()
	}
	st := e.drag2D
	e.drag2D = drag2DState{}
	defer e.resumeAutoSaveAfterInteraction()
	if len(st.targets) > 0 {
		restored := false
		for _, t := range st.targets {
			if obj, _, _ := e.FindObject(t.id); obj != nil {
				applyTransform2D(obj, t.start)
				restored = true
			}
		}
		if restored {
			e.Rebuild()
		}
		e.Status = "Cancelled " + st.handle.String()
		e.notify()
		return true
	}
	if st.overrideInfo.Origin != ObjectLocal {
		e.Doc.Version = st.overridePrevVer
		setOverrideOnDoc(e.Doc, st.overrideInfo, st.overrideOld, st.overrideHadOld, st.overrideWasNil)
		e.Rebuild()
		e.Status = "Cancelled " + st.handle.String()
		e.notify()
		return true
	}
	if obj, _, _ := e.FindObject(st.objectID); obj != nil {
		applyTransform2D(obj, st.start)
		e.Rebuild()
	}
	e.Status = "Cancelled " + st.handle.String()
	e.notify()
	return true
}

// transform2DCmd sets an object's 2D transform, as a 2D viewport drag ends.
type transform2DCmd struct {
	ed            *Editor
	id            string
	label         string
	before, after transform2D
}

func (c *transform2DCmd) Label() string { return c.label }

func (c *transform2DCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		applyTransform2D(obj, c.after)
		c.ed.Status = c.label
	}
}

func (c *transform2DCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		applyTransform2D(obj, c.before)
	}
}

// Gizmo colors: the axes as most editors color them, X red and Y green.
var (
	gizmoColorX      = core.RGBA(0xEF4444, 1)
	gizmoColorY      = core.RGBA(0x22C55E, 1)
	gizmoColorRing   = core.RGBA(0x38BDF8, 0.8)
	gizmoColorCenter = core.RGBA(0xF8FAFC, 1)
	gizmoColorActive = core.RGBA(0xFBBF24, 1)
)

// drawGizmo2D draws the selected object's transform gizmo: the rotation ring, the X and Y arrows, the move square and, on an outline large enough, the scale handles. The handle a drag holds is drawn highlighted.
func (e *Editor) drawGizmo2D(d *core.Drawing) {
	if !e.Gizmo2DShown() {
		return
	}
	g, _ := e.gizmo2D()
	active := e.drag2D.handle
	pick := func(h GizmoHandle2D, c core.Color) core.Color {
		if h == active {
			return gizmoColorActive
		}
		return c
	}
	d.Save()
	d.SetLineWidth(1.5)
	d.SetStrokeColor(pick(Handle2DRotate, gizmoColorRing))
	d.BeginPath().Arc(g.origin.X, g.origin.Y, g.ring, 0, 2*math.Pi, false).Stroke()
	for _, a := range [2]struct {
		h   GizmoHandle2D
		dir math2d.Vec2
		c   core.Color
	}{{Handle2DMoveX, g.axisX, gizmoColorX}, {Handle2DMoveY, g.axisY, gizmoColorY}} {
		c := pick(a.h, a.c)
		from := g.origin.Add(a.dir.Scale(gizmoArrowStart))
		tip := g.origin.Add(a.dir.Scale(gizmoArrowLen))
		side := math2d.Vec2{X: -a.dir.Y, Y: a.dir.X}
		back := tip.Sub(a.dir.Scale(10))
		d.SetStrokeColor(c).SetLineWidth(2)
		d.BeginPath().MoveTo(from.X, from.Y).LineTo(back.X, back.Y).Stroke()
		l, r := back.Add(side.Scale(5)), back.Sub(side.Scale(5))
		d.SetFillColor(c)
		d.BeginPath().MoveTo(tip.X, tip.Y).LineTo(l.X, l.Y).LineTo(r.X, r.Y).ClosePath().Fill()
	}
	d.SetFillColor(pick(Handle2DMove, gizmoColorCenter))
	d.FillRect(g.origin.X-gizmoCenterHalf, g.origin.Y-gizmoCenterHalf, 2*gizmoCenterHalf, 2*gizmoCenterHalf)
	d.SetLineWidth(1.5)
	for h := Handle2DScaleN; h <= Handle2DScaleNW; h++ {
		if g.scaleShown(h) {
			c := g.scaleHandlePos(h)
			d.SetFillColor(pick(h, gizmoColorCenter))
			d.FillRect(c.X-gizmoScaleHalf, c.Y-gizmoScaleHalf, 2*gizmoScaleHalf, 2*gizmoScaleHalf)
			d.SetStrokeColor(core.RGBA(0x38BDF8, 1))
			d.StrokeRect(c.X-gizmoScaleHalf, c.Y-gizmoScaleHalf, 2*gizmoScaleHalf, 2*gizmoScaleHalf)
		}
	}
	d.Restore()
	if e.SelectionCount() == 1 {
		e.drawSelectedLightingHandles2D(d)
		e.drawSelectedNavRegionHandles2D(d)
	}
}

func unitVec(v math2d.Vec2) math2d.Vec2 {
	l := vecLen(v)
	if l == 0 || !isFiniteFloat32(l) {
		return math2d.Vec2{X: 1}
	}
	return math2d.Vec2{X: v.X / l, Y: v.Y / l}
}

func vecLen(v math2d.Vec2) float32 { return float32(math.Hypot(float64(v.X), float64(v.Y))) }

func isFiniteVec(v math2d.Vec2) bool { return isFiniteFloat32(v.X) && isFiniteFloat32(v.Y) }

func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }
