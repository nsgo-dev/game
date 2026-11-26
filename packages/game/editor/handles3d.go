package editor

import (
	"fmt"
	"math"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

// Handle3DKind identifies an interactive 3D viewport handle for editing a selected light or 3D camera.
type Handle3DKind int

const (
	Handle3DNone Handle3DKind = iota
	// Handle3DLightDirection aims a directional or spot light along the dragged direction.
	Handle3DLightDirection
	// Handle3DLightRange edits a point or spot light's range radius/distance.
	Handle3DLightRange
	// Handle3DLightOuterCone edits a spot light's outer cone half-angle (degrees).
	Handle3DLightOuterCone
	// Handle3DLightInnerCone edits a spot light's inner cone half-angle (degrees).
	Handle3DLightInnerCone
	// Handle3DCameraFOV edits a 3D camera's vertical FOV (perspective, degrees) or OrthoHeight (orthographic).
	Handle3DCameraFOV
	// Handle3DCameraFar edits a 3D camera's far clip plane distance.
	Handle3DCameraFar
	// Handle3DProbeBoundsPosX edits the +X face of a selected reflection probe or light probe grid's bounds box.
	Handle3DProbeBoundsPosX
	// Handle3DProbeBoundsNegX edits the -X face of a selected reflection probe or light probe grid's bounds box.
	Handle3DProbeBoundsNegX
	// Handle3DProbeBoundsPosY edits the +Y face of a selected reflection probe or light probe grid's bounds box.
	Handle3DProbeBoundsPosY
	// Handle3DProbeBoundsNegY edits the -Y face of a selected reflection probe or light probe grid's bounds box.
	Handle3DProbeBoundsNegY
	// Handle3DProbeBoundsPosZ edits the +Z face of a selected reflection probe or light probe grid's bounds box.
	Handle3DProbeBoundsPosZ
	// Handle3DProbeBoundsNegZ edits the -Z face of a selected reflection probe or light probe grid's bounds box.
	Handle3DProbeBoundsNegZ
)

// String returns a human-readable label for the handle kind.
func (h Handle3DKind) String() string {
	switch h {
	case Handle3DLightDirection:
		return "Light Direction"
	case Handle3DLightRange:
		return "Light Range"
	case Handle3DLightOuterCone:
		return "Spot Outer Cone"
	case Handle3DLightInnerCone:
		return "Spot Inner Cone"
	case Handle3DCameraFOV:
		return "Camera FOV"
	case Handle3DCameraFar:
		return "Camera Far"
	case Handle3DProbeBoundsPosX:
		return "Probe Bounds +X"
	case Handle3DProbeBoundsNegX:
		return "Probe Bounds -X"
	case Handle3DProbeBoundsPosY:
		return "Probe Bounds +Y"
	case Handle3DProbeBoundsNegY:
		return "Probe Bounds -Y"
	case Handle3DProbeBoundsPosZ:
		return "Probe Bounds +Z"
	case Handle3DProbeBoundsNegZ:
		return "Probe Bounds -Z"
	default:
		return "None"
	}
}

func probeBoundsHandleAxis(kind Handle3DKind) (axis int, sign float64, ok bool) {
	switch kind {
	case Handle3DProbeBoundsPosX:
		return 0, +1, true
	case Handle3DProbeBoundsNegX:
		return 0, -1, true
	case Handle3DProbeBoundsPosY:
		return 1, +1, true
	case Handle3DProbeBoundsNegY:
		return 1, -1, true
	case Handle3DProbeBoundsPosZ:
		return 2, +1, true
	case Handle3DProbeBoundsNegZ:
		return 2, -1, true
	default:
		return 0, 0, false
	}
}

func probeLocalBox3D(o *sceneformat.ObjectDoc) (center, size [3]float64, isRefl bool, ok bool) {
	if o == nil {
		return center, size, false, false
	}
	if o.ReflectionProbe != nil {
		c := o.ReflectionProbe.EffectiveCenter()
		s := o.ReflectionProbe.EffectiveSize()
		return [3]float64{float64(c[0]), float64(c[1]), float64(c[2])},
			[3]float64{float64(s[0]), float64(s[1]), float64(s[2])}, true, true
	}
	if o.LightProbeGrid != nil {
		c := o.LightProbeGrid.EffectiveCenter()
		s := o.LightProbeGrid.EffectiveSize()
		return [3]float64{float64(c[0]), float64(c[1]), float64(c[2])},
			[3]float64{float64(s[0]), float64(s[1]), float64(s[2])}, false, true
	}
	return center, size, false, false
}

const (
	handle3DHitRadius float32 = 13
	icon3DHitRadius   float32 = 14
	handle3DRingSteps         = 36
)

// view3DCam is a unified projection/ray camera representing either the editor's OrbitCamera3D
// or the scene's active 3D camera when LookThroughCamera3D is enabled.
type view3DCam struct {
	eye     [3]float64
	right   [3]float64
	up      [3]float64
	forward [3]float64
	fov     float64
	halfH   float64
	near    float64
	far     float64
	ortho   bool
}

func (e *Editor) activeView3DCam(w, h float32) view3DCam {
	if e != nil && e.lookThroughCamera3D {
		if cam, _, ok := e.ActiveSceneCamera3D(w, h); ok && cam != nil {
			return view3DCamFromSceneCamera(cam)
		}
	}
	c := e.Camera3D
	_, fov, near, far := c.effectiveParams()
	right, up, fwd := c.basis64()
	return view3DCam{
		eye:     c.eye64(),
		right:   right,
		up:      up,
		forward: fwd,
		fov:     fov,
		halfH:   c.halfHeight64(),
		near:    near,
		far:     far,
		ortho:   c.Orthographic,
	}
}

func view3DCamFromSceneCamera(cam *scene3d.Camera) view3DCam {
	p := cam.Projection
	ortho := p.Kind == scene3d.ProjectionOrthographic
	fov := float64(p.FOVY)
	if fov <= 0.05 || fov >= math.Pi-0.05 || math.IsNaN(fov) || math.IsInf(fov, 0) {
		fov = math.Pi / 3
	}
	halfH := float64(p.OrthoHeight) * 0.5
	if halfH <= 0 || math.IsNaN(halfH) || math.IsInf(halfH, 0) {
		halfH = 5
	}
	near := float64(p.Near)
	if math.IsNaN(near) || math.IsInf(near, 0) || (!ortho && near <= 0) {
		near = 0.1
	}
	far := float64(p.Far)
	if math.IsNaN(far) || math.IsInf(far, 0) || far <= near {
		far = near + 1000
	}
	r := cam.Right()
	u := cam.Up()
	f := cam.Forward()
	return view3DCam{
		eye:     [3]float64{float64(cam.Pos.X), float64(cam.Pos.Y), float64(cam.Pos.Z)},
		right:   [3]float64{float64(r.X), float64(r.Y), float64(r.Z)},
		up:      [3]float64{float64(u.X), float64(u.Y), float64(u.Z)},
		forward: [3]float64{float64(f.X), float64(f.Y), float64(f.Z)},
		fov:     fov,
		halfH:   halfH,
		near:    near,
		far:     far,
		ortho:   ortho,
	}
}

func (v view3DCam) screen64(p [3]float64, w, h float32) (sx, sy float32) {
	aspect := float64(w) / float64(h)
	var ndcX, ndcY float64
	if v.ortho {
		ndcX = p[0] / (v.halfH * aspect)
		ndcY = p[1] / v.halfH
	} else {
		tanHalf := math.Tan(v.fov * 0.5)
		ndcX = p[0] / (p[2] * tanHalf * aspect)
		ndcY = p[1] / (p[2] * tanHalf)
	}
	return float32((ndcX + 1) * 0.5 * float64(w)), float32((1 - ndcY) * 0.5 * float64(h))
}

func (v view3DCam) project(world [3]float32, w, h float32) (sx, sy, depth float32, visible bool) {
	if w <= 0 || h <= 0 {
		return 0, 0, 0, false
	}
	p := toCamera64(vec64(world), v.eye, v.right, v.up, v.forward)
	if p[2] <= v.near || p[2] >= v.far {
		return 0, 0, float32(p[2]), false
	}
	sx, sy = v.screen64(p, w, h)
	return sx, sy, float32(p[2]), true
}

func (v view3DCam) projectSegment(a, b [3]float32, w, h float32) (x0, y0, x1, y1 float32, visible bool) {
	if w <= 0 || h <= 0 {
		return 0, 0, 0, 0, false
	}
	ca := toCamera64(vec64(a), v.eye, v.right, v.up, v.forward)
	cb := toCamera64(vec64(b), v.eye, v.right, v.up, v.forward)
	clipNear := v.near + 1e-3
	if (ca[2] <= clipNear && cb[2] <= clipNear) || (ca[2] >= v.far && cb[2] >= v.far) {
		return 0, 0, 0, 0, false
	}
	if ca[2] < clipNear {
		t := (clipNear - ca[2]) / (cb[2] - ca[2])
		ca = lerp64(ca, cb, t)
	} else if cb[2] < clipNear {
		t := (clipNear - cb[2]) / (ca[2] - cb[2])
		cb = lerp64(cb, ca, t)
	}
	x0, y0 = v.screen64(ca, w, h)
	x1, y1 = v.screen64(cb, w, h)
	return x0, y0, x1, y1, true
}

func (v view3DCam) screenRay(sx, sy, w, h float32) (origin, dir [3]float32) {
	if w <= 0 || h <= 0 {
		return vec32(v.eye), vec32(v.forward)
	}
	aspect := float64(w) / float64(h)
	ndcX := (float64(sx)/float64(w))*2 - 1
	ndcY := 1 - (float64(sy)/float64(h))*2
	if v.ortho {
		o := add64(v.eye, add64(scale64(v.right, ndcX*v.halfH*aspect), scale64(v.up, ndcY*v.halfH)))
		return vec32(o), vec32(v.forward)
	}
	tanHalf := math.Tan(v.fov * 0.5)
	d := normalize64(add64(
		add64(scale64(v.right, ndcX*tanHalf*aspect), scale64(v.up, ndcY*tanHalf)),
		v.forward,
	))
	return vec32(v.eye), vec32(d)
}

// objectWorldFrame3D returns an object's world position and orthonormal (right, up, forward=-Z) basis.
func (e *Editor) objectWorldFrame3D(o *sceneformat.ObjectDoc) (pos, right, up, fwd [3]float64) {
	posed := e.posed3D(o)
	world := e.parentWorld3D(o.ID).mul(local3(posed))
	pos = world.point([3]float64{0, 0, 0})
	r := normalize64(world.vector([3]float64{1, 0, 0}))
	u := normalize64(world.vector([3]float64{0, 1, 0}))
	f := normalize64(world.vector([3]float64{0, 0, -1}))
	if len64(f) < 1e-6 {
		f = [3]float64{0, 0, -1}
	}
	if len64(r) < 1e-6 {
		r = [3]float64{1, 0, 0}
	}
	u = normalize64(cross64(r, f))
	if len64(u) < 1e-6 {
		u = [3]float64{0, 1, 0}
	}
	r = normalize64(cross64(f, u))
	return pos, r, u, f
}

func (e *Editor) effectiveLightRange3D(pos [3]float32, l *sceneformat.LightDoc) float64 {
	if l != nil && l.Range > 0 {
		return float64(l.Range)
	}
	return float64(e.GizmoLength3D(pos)) * 1.4
}

func (e *Editor) effectiveDirHandleLen3D(pos [3]float32, l *sceneformat.LightDoc) float64 {
	g := float64(e.GizmoLength3D(pos)) * 1.4
	if l != nil && l.Kind == sceneformat.LightSpot && l.Range > 0 {
		return math.Min(float64(l.Range)*0.55, g)
	}
	return g
}

func (e *Editor) effectiveCameraPreviewDist3D(pos [3]float32, c *sceneformat.Camera3DDoc) float64 {
	near := float64(c.EffectiveNear())
	far := float64(c.EffectiveFar())
	ideal := math.Max(near*4, float64(e.GizmoLength3D(pos))*1.6)
	if ideal > far {
		ideal = far
	}
	if ideal <= near {
		ideal = near + 0.5
	}
	return ideal
}

// HandlePosition3D returns the world-space position of handle kind on the currently selected light
// or 3D camera object, or false when the selected object does not expose that handle.
func (e *Editor) HandlePosition3D(kind Handle3DKind) ([3]float32, bool) {
	if e == nil || kind == Handle3DNone {
		return [3]float32{}, false
	}
	obj, _ := e.SelectedObject()
	if obj == nil || e.viewportFrozen(obj.ID) {
		return [3]float32{}, false
	}
	pos64, right, up, fwd := e.objectWorldFrame3D(obj)
	pos := vec32(pos64)

	switch kind {
	case Handle3DLightDirection:
		if obj.Light == nil || (obj.Light.Kind != sceneformat.LightDirectional && obj.Light.Kind != sceneformat.LightSpot) {
			return [3]float32{}, false
		}
		dist := e.effectiveDirHandleLen3D(pos, obj.Light)
		return vec32(add64(pos64, scale64(fwd, dist))), true

	case Handle3DLightRange:
		if obj.Light == nil || (obj.Light.Kind != sceneformat.LightPoint && obj.Light.Kind != sceneformat.LightSpot) {
			return [3]float32{}, false
		}
		r := e.effectiveLightRange3D(pos, obj.Light)
		if obj.Light.Kind == sceneformat.LightPoint {
			return vec32(add64(pos64, scale64(right, r))), true
		}
		return vec32(add64(pos64, scale64(fwd, r))), true

	case Handle3DLightOuterCone:
		if obj.Light == nil || obj.Light.Kind != sceneformat.LightSpot {
			return [3]float32{}, false
		}
		coneLen := e.effectiveLightRange3D(pos, obj.Light)
		outerRad := float64(math3d.DegToRad(obj.Light.EffectiveOuterCone()))
		radius := coneLen * math.Tan(outerRad)
		center := add64(pos64, scale64(fwd, coneLen))
		return vec32(add64(center, scale64(right, radius))), true

	case Handle3DLightInnerCone:
		if obj.Light == nil || obj.Light.Kind != sceneformat.LightSpot {
			return [3]float32{}, false
		}
		coneLen := e.effectiveLightRange3D(pos, obj.Light)
		innerRad := float64(math3d.DegToRad(obj.Light.InnerCone))
		radius := coneLen * math.Tan(innerRad)
		center := add64(pos64, scale64(fwd, coneLen))
		return vec32(add64(center, scale64(up, radius))), true

	case Handle3DCameraFOV:
		if obj.Camera == nil {
			return [3]float32{}, false
		}
		d := e.effectiveCameraPreviewDist3D(pos, obj.Camera)
		var halfH float64
		if obj.Camera.EffectiveProjection() == sceneformat.ProjectionOrthographic {
			halfH = float64(obj.Camera.EffectiveOrthoHeight()) * 0.5
		} else {
			halfH = d * math.Tan(float64(math3d.DegToRad(obj.Camera.EffectiveFOV()))*0.5)
		}
		center := add64(pos64, scale64(fwd, d))
		return vec32(add64(center, scale64(up, halfH))), true

	case Handle3DCameraFar:
		if obj.Camera == nil {
			return [3]float32{}, false
		}
		far := float64(obj.Camera.EffectiveFar())
		return vec32(add64(pos64, scale64(fwd, far))), true

	case Handle3DProbeBoundsPosX, Handle3DProbeBoundsNegX,
		Handle3DProbeBoundsPosY, Handle3DProbeBoundsNegY,
		Handle3DProbeBoundsPosZ, Handle3DProbeBoundsNegZ:
		axis, sign, _ := probeBoundsHandleAxis(kind)
		center, size, _, has := probeLocalBox3D(obj)
		if !has {
			return [3]float32{}, false
		}
		posed := e.posed3D(obj)
		world := e.parentWorld3D(obj.ID).mul(local3(posed))
		pt := center
		pt[axis] += sign * size[axis] * 0.5
		return vec32(world.point(pt)), true
	}
	return [3]float32{}, false
}

// availableHandles3D returns the handle kinds exposed by the selected object in pick priority order.
func (e *Editor) availableHandles3D(obj *sceneformat.ObjectDoc) []Handle3DKind {
	if obj == nil {
		return nil
	}
	var kinds []Handle3DKind
	if obj.Light != nil {
		switch obj.Light.Kind {
		case sceneformat.LightDirectional:
			kinds = append(kinds, Handle3DLightDirection)
		case sceneformat.LightPoint:
			kinds = append(kinds, Handle3DLightRange)
		case sceneformat.LightSpot:
			kinds = append(kinds, Handle3DLightOuterCone, Handle3DLightInnerCone, Handle3DLightRange, Handle3DLightDirection)
		}
	}
	if obj.Camera != nil {
		kinds = append(kinds, Handle3DCameraFOV, Handle3DCameraFar)
	}
	if obj.ReflectionProbe != nil || obj.LightProbeGrid != nil {
		kinds = append(kinds,
			Handle3DProbeBoundsPosX, Handle3DProbeBoundsNegX,
			Handle3DProbeBoundsPosY, Handle3DProbeBoundsNegY,
			Handle3DProbeBoundsPosZ, Handle3DProbeBoundsNegZ,
		)
	}
	return kinds
}

// PickHandle3D hit-tests interactive light and 3D camera handles on the currently selected object.
func (e *Editor) PickHandle3D(sx, sy, w, h float32) Handle3DKind {
	if e == nil || e.playing || w <= 0 || h <= 0 || e.SelectionCount() != 1 {
		return Handle3DNone
	}
	if e.gridMap.tool != GridMapToolNone || (e.TerrainOpen() && e.terrainBrush.Mode != TerrainBrushNone) {
		return Handle3DNone
	}
	obj, _ := e.SelectedObject()
	if obj == nil || e.viewportFrozen(obj.ID) {
		return Handle3DNone
	}
	if e.LookThroughCamera3D() {
		if _, camID, ok := e.ActiveSceneCamera3D(w, h); ok && camID == obj.ID {
			return Handle3DNone
		}
	}
	vc := e.activeView3DCam(w, h)
	best := Handle3DNone
	bestDist := float64(handle3DHitRadius)
	for _, kind := range e.availableHandles3D(obj) {
		hp, ok := e.HandlePosition3D(kind)
		if !ok {
			continue
		}
		px, py, _, vis := vc.project(hp, w, h)
		if !vis {
			continue
		}
		d := math.Hypot(float64(sx-px), float64(sy-py))
		if d < bestDist {
			bestDist = d
			best = kind
		}
	}
	return best
}

// ActiveHandle3D returns the currently dragged light/camera handle kind, or Handle3DNone.
func (e *Editor) ActiveHandle3D() Handle3DKind {
	if e == nil || e.drag3D.kind != drag3DHandle {
		return Handle3DNone
	}
	return e.drag3D.handle
}

// DraggingHandle3D reports whether an interactive light/camera handle drag is in progress.
func (e *Editor) DraggingHandle3D() bool {
	return e != nil && e.drag3D.kind == drag3DHandle
}

// BeginHandleDrag3D picks an interactive light/camera handle at (sx, sy) and begins dragging it.
func (e *Editor) BeginHandleDrag3D(sx, sy, w, h float32) Handle3DKind {
	if e == nil || e.playing || e.HistoryPreviewActive() {
		return Handle3DNone
	}
	kind := e.PickHandle3D(sx, sy, w, h)
	if kind == Handle3DNone {
		return Handle3DNone
	}
	if !e.BeginHandleDragKind3D(kind, sx, sy, w, h) {
		return Handle3DNone
	}
	return kind
}

// BeginHandleDragKind3D starts dragging handle kind on the selected object from viewport pixel (sx, sy).
func (e *Editor) BeginHandleDragKind3D(kind Handle3DKind, sx, sy, w, h float32) bool {
	if e == nil || e.playing || kind == Handle3DNone || w <= 0 || h <= 0 {
		return false
	}
	obj, _ := e.SelectedObject()
	if obj == nil || e.viewportFrozen(obj.ID) || e.checkAgentTouchLock(obj.ID) != nil {
		return false
	}
	hp, ok := e.HandlePosition3D(kind)
	if !ok {
		return false
	}
	parentInv, ok := e.parentWorld3D(obj.ID).invert()
	if !ok {
		return false
	}
	pos64, right, up, fwd := e.objectWorldFrame3D(obj)
	pos32 := vec32(pos64)
	vc := e.activeView3DCam(w, h)
	orig32, dir32 := vc.screenRay(sx, sy, w, h)
	orig, dir := vec64(orig32), vec64(dir32)

	st := drag3DState{
		kind:                 drag3DHandle,
		handle:               kind,
		objectID:             obj.ID,
		startSX:              sx,
		startSY:              sy,
		lastSX:               sx,
		lastSY:               sy,
		vpW:                  w,
		vpH:                  h,
		gizmoLen:             e.GizmoLength3D(pos32),
		startTransform:       captureTransform3D(obj),
		startLight:           obj.Light.Clone(),
		startCamera:          obj.Camera.Clone(),
		startReflectionProbe: obj.ReflectionProbe.Clone(),
		startLightProbeGrid:  obj.LightProbeGrid.Clone(),
		startPos:             pos64,
		parentInv:            parentInv,
		handleRight:          right,
		handleUp:             up,
		handleFwd:            fwd,
	}

	switch kind {
	case Handle3DLightDirection:
		st.handleBaseLen = e.effectiveDirHandleLen3D(pos32, obj.Light)
		st.handlePlanePt = vec64(hp)
		st.handlePlaneN = vc.forward

	case Handle3DLightRange:
		axis := fwd
		if obj.Light.Kind == sceneformat.LightPoint {
			axis = right
		}
		st.handleAxis = axis
		st.handleBaseLen = e.effectiveLightRange3D(pos32, obj.Light)
		if t, ok := closestParamOnAxisLine64(pos64, axis, orig, dir); ok {
			st.startAxisT = t
			st.hasAxisT = true
		}

	case Handle3DLightOuterCone, Handle3DLightInnerCone:
		coneLen := e.effectiveLightRange3D(pos32, obj.Light)
		st.handleBaseLen = coneLen
		center := add64(pos64, scale64(fwd, coneLen))
		st.handlePlanePt = center
		axis := right
		if kind == Handle3DLightInnerCone {
			axis = up
		}
		st.handleAxis = axis
		if t, ok := closestParamOnAxisLine64(center, axis, orig, dir); ok {
			st.startAxisT = t
			st.hasAxisT = true
		}

	case Handle3DCameraFOV:
		d := e.effectiveCameraPreviewDist3D(pos32, obj.Camera)
		st.handleBaseLen = d
		center := add64(pos64, scale64(fwd, d))
		st.handlePlanePt = center
		st.handleAxis = up
		if t, ok := closestParamOnAxisLine64(center, up, orig, dir); ok {
			st.startAxisT = t
			st.hasAxisT = true
		}

	case Handle3DCameraFar:
		st.handleAxis = fwd
		st.handleBaseLen = float64(obj.Camera.EffectiveFar())
		if t, ok := closestParamOnAxisLine64(pos64, fwd, orig, dir); ok {
			st.startAxisT = t
			st.hasAxisT = true
		}

	case Handle3DProbeBoundsPosX, Handle3DProbeBoundsNegX,
		Handle3DProbeBoundsPosY, Handle3DProbeBoundsNegY,
		Handle3DProbeBoundsPosZ, Handle3DProbeBoundsNegZ:
		axis, sign, _ := probeBoundsHandleAxis(kind)
		center, size, isRefl, has := probeLocalBox3D(obj)
		if !has {
			return false
		}
		posed := e.posed3D(obj)
		world := e.parentWorld3D(obj.ID).mul(local3(posed))
		var localAxis [3]float64
		localAxis[axis] = 1
		worldVec := world.vector(localAxis)
		axisScale := len64(worldVec)
		worldAxis := localAxis
		if axisScale > 1e-6 {
			worldAxis = normalize64(worldVec)
		} else {
			axisScale = 1
		}
		faceLocal := center
		faceLocal[axis] += sign * size[axis] * 0.5
		faceWorld := world.point(faceLocal)
		st.handleAxis = worldAxis
		st.handlePlanePt = faceWorld
		st.handleBaseLen = math.Max(size[axis]*axisScale, 1)
		st.handleBoundsCenter = center
		st.handleBoundsSize = size
		st.handleBoundsAxis = axis
		st.handleBoundsSign = sign
		st.handleBoundsScale = axisScale
		st.handleBoundsIsRefl = isRefl
		if t, ok := closestParamOnAxisLine64(faceWorld, worldAxis, orig, dir); ok {
			st.startAxisT = t
			st.hasAxisT = true
		}
	}

	e.drag3D = st
	e.notify()
	return true
}

// UpdateHandleDrag3D updates the light or 3D camera live during an active handle drag.
func (e *Editor) UpdateHandleDrag3D(sx, sy float32) {
	if e == nil || e.playing || e.drag3D.kind != drag3DHandle {
		return
	}
	obj, _, _ := e.FindObject(e.drag3D.objectID)
	if obj == nil {
		e.drag3D = drag3DState{}
		return
	}
	st := &e.drag3D
	vc := e.activeView3DCam(st.vpW, st.vpH)
	orig32, dir32 := vc.screenRay(sx, sy, st.vpW, st.vpH)
	orig, dir := vec64(orig32), vec64(dir32)

	axisDelta := func(origin, axis [3]float64) float64 {
		if st.hasAxisT {
			if tCur, ok := closestParamOnAxisLine64(origin, axis, orig, dir); ok {
				return tCur - st.startAxisT
			}
		}
		p0 := vec32(origin)
		p1 := vec32(add64(origin, scale64(axis, math.Max(st.handleBaseLen, 1))))
		x0, y0, x1, y1, vis := vc.projectSegment(p0, p1, st.vpW, st.vpH)
		if !vis {
			return 0
		}
		ax, ay := float64(x1-x0), float64(y1-y0)
		pixLen := math.Hypot(ax, ay)
		if pixLen < 1e-3 {
			return 0
		}
		dsx, dsy := float64(sx-st.startSX), float64(sy-st.startSY)
		return ((dsx*ax + dsy*ay) / pixLen) * (math.Max(st.handleBaseLen, 1) / pixLen)
	}

	switch st.handle {
	case Handle3DLightDirection:
		hit, ok := intersectRayPlane64(orig, dir, st.handlePlanePt, st.handlePlaneN)
		if !ok {
			// Fallback: place target along ray at same distance from eye as handlePlanePt.
			d := len64(sub64(st.handlePlanePt, orig))
			hit = add64(orig, scale64(dir, d))
		}
		worldDir := normalize64(sub64(hit, st.startPos))
		if len64(worldDir) > 1e-5 {
			localDir := normalize64(st.parentInv.vector(worldDir))
			if len64(localDir) > 1e-5 {
				rx, ry := eulerFromForward64(localDir)
				obj.RotX = float32(rx)
				obj.RotY = float32(ry)
				obj.RotZ = 0
			}
		}

	case Handle3DLightRange:
		if obj.Light == nil || st.startLight == nil {
			return
		}
		delta := axisDelta(st.startPos, st.handleAxis)
		newRange := clamp64(st.handleBaseLen+delta, 0.1, 10000)
		obj.Light.Range = float32(math.Round(newRange*100) / 100)

	case Handle3DLightOuterCone:
		if obj.Light == nil || st.startLight == nil {
			return
		}
		coneLen := math.Max(st.handleBaseLen, 1e-3)
		startRad := coneLen * math.Tan(float64(math3d.DegToRad(st.startLight.EffectiveOuterCone())))
		delta := axisDelta(st.handlePlanePt, st.handleAxis)
		newRadius := math.Max(0.01, startRad+delta)
		deg := clamp64(math.Atan2(newRadius, coneLen)*180/math.Pi, 1, 89.5)
		deg = math.Round(deg*10) / 10
		outer := float32(deg)
		obj.Light.OuterCone = sceneformat.Float32(outer)
		maxInner := float32(math.Max(0, math.Round((deg-0.1)*10)/10))
		if maxInner >= outer {
			maxInner = 0
		}
		if obj.Light.InnerCone > maxInner {
			obj.Light.InnerCone = maxInner
		}

	case Handle3DLightInnerCone:
		if obj.Light == nil || st.startLight == nil {
			return
		}
		coneLen := math.Max(st.handleBaseLen, 1e-3)
		startRad := coneLen * math.Tan(float64(math3d.DegToRad(st.startLight.InnerCone)))
		delta := axisDelta(st.handlePlanePt, st.handleAxis)
		newRadius := math.Max(0, startRad+delta)
		outer := obj.Light.EffectiveOuterCone()
		maxDeg := math.Max(0, float64(outer)-0.1)
		deg := clamp64(math.Atan2(newRadius, coneLen)*180/math.Pi, 0, maxDeg)
		deg = math.Round(deg*10) / 10
		if float32(deg) >= outer {
			deg = math.Max(0, math.Floor(maxDeg*10)/10)
			if float32(deg) >= outer {
				deg = 0
			}
		}
		obj.Light.InnerCone = float32(deg)

	case Handle3DCameraFOV:
		if obj.Camera == nil || st.startCamera == nil {
			return
		}
		d := math.Max(st.handleBaseLen, 1e-3)
		delta := axisDelta(st.handlePlanePt, st.handleAxis)
		if obj.Camera.EffectiveProjection() == sceneformat.ProjectionOrthographic {
			startHalfH := float64(st.startCamera.EffectiveOrthoHeight()) * 0.5
			newH := clamp64((startHalfH+delta)*2, 0.1, 10000)
			obj.Camera.OrthoHeight = sceneformat.Float32(float32(math.Round(newH*100) / 100))
		} else {
			startHalfH := d * math.Tan(float64(math3d.DegToRad(st.startCamera.EffectiveFOV()))*0.5)
			newHalfH := math.Max(0.01, startHalfH+delta)
			deg := clamp64(2*math.Atan2(newHalfH, d)*180/math.Pi, 5, 170)
			obj.Camera.FOV = sceneformat.Float32(float32(math.Round(deg*10) / 10))
		}

	case Handle3DCameraFar:
		if obj.Camera == nil || st.startCamera == nil {
			return
		}
		delta := axisDelta(st.startPos, st.handleAxis)
		minFar := float64(obj.Camera.EffectiveNear()) + 0.1
		newFar := clamp64(st.handleBaseLen+delta, minFar, 100000)
		obj.Camera.Far = sceneformat.Float32(float32(math.Round(newFar*100) / 100))

	case Handle3DProbeBoundsPosX, Handle3DProbeBoundsNegX,
		Handle3DProbeBoundsPosY, Handle3DProbeBoundsNegY,
		Handle3DProbeBoundsPosZ, Handle3DProbeBoundsNegZ:
		deltaWorld := axisDelta(st.handlePlanePt, st.handleAxis)
		scale := st.handleBoundsScale
		if scale < 1e-6 {
			scale = 1
		}
		deltaLocal := deltaWorld / scale
		axis := st.handleBoundsAxis
		lo := st.handleBoundsCenter[axis] - st.handleBoundsSize[axis]*0.5
		hi := st.handleBoundsCenter[axis] + st.handleBoundsSize[axis]*0.5
		if st.handleBoundsSign > 0 {
			hi = math.Max(lo+0.1, hi+deltaLocal)
		} else {
			lo = math.Min(hi-0.1, lo+deltaLocal)
		}
		newSize := math.Max(0.1, math.Round((hi-lo)*100)/100)
		newCenter := math.Round(((lo+hi)*0.5)*100) / 100
		c3 := [3]float32{
			float32(st.handleBoundsCenter[0]),
			float32(st.handleBoundsCenter[1]),
			float32(st.handleBoundsCenter[2]),
		}
		s3 := [3]float32{
			float32(st.handleBoundsSize[0]),
			float32(st.handleBoundsSize[1]),
			float32(st.handleBoundsSize[2]),
		}
		c3[axis] = float32(newCenter)
		s3[axis] = float32(newSize)
		var cPtr *[3]float32
		if c3 != ([3]float32{}) {
			cPtr = &c3
		}
		if st.handleBoundsIsRefl {
			if obj.ReflectionProbe == nil || st.startReflectionProbe == nil {
				return
			}
			obj.ReflectionProbe.Center = cPtr
			obj.ReflectionProbe.Size = &s3
		} else {
			if obj.LightProbeGrid == nil || st.startLightProbeGrid == nil {
				return
			}
			obj.LightProbeGrid.Center = cPtr
			obj.LightProbeGrid.Size = &s3
		}
	}

	st.lastSX = sx
	st.lastSY = sy
	e.Rebuild()
	e.notify()
}

// EndHandleDrag3D commits an active light/camera handle drag as a single undoable History step.
func (e *Editor) EndHandleDrag3D() bool {
	if e == nil || e.drag3D.kind != drag3DHandle {
		return false
	}
	st := e.drag3D
	e.drag3D = drag3DState{}

	obj, _, _ := e.FindObject(st.objectID)
	if obj == nil {
		e.notify()
		return false
	}

	switch st.handle {
	case Handle3DLightDirection:
		after := captureTransform3D(obj)
		if equalTransform3D(st.startTransform, after) {
			e.notify()
			return false
		}
		applyTransform3D(obj, st.startTransform)
		e.History.Execute(&transform3DCmd{
			ed:     e,
			id:     st.objectID,
			label:  "Aim Light " + st.objectID,
			before: st.startTransform,
			after:  after,
		})
		return true

	case Handle3DLightRange, Handle3DLightOuterCone, Handle3DLightInnerCone:
		after := obj.Light.Clone()
		obj.Light = st.startLight.Clone()
		if err := e.SetObjectLight(st.objectID, after); err != nil {
			e.Rebuild()
			e.notify()
			return false
		}
		return true

	case Handle3DCameraFOV, Handle3DCameraFar:
		after := obj.Camera.Clone()
		obj.Camera = st.startCamera.Clone()
		if err := e.SetObjectCamera3D(st.objectID, after); err != nil {
			e.Rebuild()
			e.notify()
			return false
		}
		return true

	case Handle3DProbeBoundsPosX, Handle3DProbeBoundsNegX,
		Handle3DProbeBoundsPosY, Handle3DProbeBoundsNegY,
		Handle3DProbeBoundsPosZ, Handle3DProbeBoundsNegZ:
		if st.handleBoundsIsRefl {
			after := obj.ReflectionProbe.Clone()
			obj.ReflectionProbe = st.startReflectionProbe.Clone()
			if err := e.SetObjectReflectionProbe(st.objectID, after); err != nil {
				e.Rebuild()
				e.notify()
				return false
			}
			return true
		}
		after := obj.LightProbeGrid.Clone()
		obj.LightProbeGrid = st.startLightProbeGrid.Clone()
		if err := e.SetObjectLightProbeGrid(st.objectID, after); err != nil {
			e.Rebuild()
			e.notify()
			return false
		}
		return true
	}
	e.notify()
	return false
}

// CancelHandleDrag3D aborts any active light/camera/probe handle drag and restores the pre-drag values.
func (e *Editor) CancelHandleDrag3D() {
	if e == nil || e.drag3D.kind != drag3DHandle {
		return
	}
	st := e.drag3D
	e.drag3D = drag3DState{}
	if obj, _, _ := e.FindObject(st.objectID); obj != nil {
		applyTransform3D(obj, st.startTransform)
		obj.Light = st.startLight.Clone()
		obj.Camera = st.startCamera.Clone()
		obj.ReflectionProbe = st.startReflectionProbe.Clone()
		obj.LightProbeGrid = st.startLightProbeGrid.Clone()
		e.Rebuild()
	}
	e.notify()
}

// eulerFromForward64 computes (RotX, RotY) with RotZ=0 so rotateEulerXYZ([0, 0, -1], rx, ry, 0) equals unit vector d.
func eulerFromForward64(d [3]float64) (rx, ry float64) {
	d = normalize64(d)
	rx = math.Asin(clamp64(d[1], -1, 1))
	ry = math.Atan2(-d[0], -d[2])
	return rx, ry
}

// isLightOrCameraOnly3D reports whether o has a 3D light, camera, reflection probe, or light probe grid and draws no mesh, model or sprite.
func isLightOrCameraOnly3D(o *sceneformat.ObjectDoc) bool {
	if o == nil || (o.Light == nil && o.Camera == nil && o.ReflectionProbe == nil && o.LightProbeGrid == nil) {
		return false
	}
	return o.Mesh == "" && o.Model == "" && o.Sprite == nil && o.Scene == ""
}

// pickIcon3D checks whether viewport pixel (sx, sy) hits a visible light, 3D camera, or probe icon.
// Returns the flat 3D object index (Objects3D's) of the closest hit icon.
func (e *Editor) pickIcon3D(sx, sy, w, h float32) (int, bool) {
	if e == nil || e.Doc == nil || w <= 0 || h <= 0 {
		return -1, false
	}
	vc := e.activeView3DCam(w, h)
	activeCamID := ""
	if e.LookThroughCamera3D() {
		_, activeCamID, _ = e.ActiveSceneCamera3D(w, h)
	}
	bestIdx := -1
	bestDepth := float32(math.Inf(1))
	doc := e.resolvedDoc()
	if doc == nil {
		return -1, false
	}
	idx := 0
	for li := range doc.Layers {
		layer := &doc.Layers[li]
		visible := layer.IsVisible() && layer.EffectiveSpace() != sceneformat.SpaceScreen && !e.layerLocked[layer.Name]
		var walk func(objs []sceneformat.ObjectDoc, parent aff3, skip bool)
		walk = func(objs []sceneformat.ObjectDoc, parent aff3, skip bool) {
			for oi := range objs {
				o := &objs[oi]
				posed := e.posed3D(o)
				skipped := skip || e.hidden[o.ID] || e.locked[o.ID]
				if visible && !skipped && (posed.Light != nil || posed.Camera != nil || posed.ReflectionProbe != nil || posed.LightProbeGrid != nil) && o.ID != activeCamID {
					pos := vec32(parent.point([3]float64{float64(posed.X), float64(posed.Y), float64(posed.PosZ)}))
					if px, py, depth, vis := vc.project(pos, w, h); vis {
						if math.Hypot(float64(sx-px), float64(sy-py)) <= float64(icon3DHitRadius) && depth < bestDepth {
							bestDepth = depth
							bestIdx = idx
						}
					}
				}
				idx++
				walk(o.Children, parent.mul(local3(posed)), skipped)
			}
		}
		walk(layer.Objects, ident3, false)
	}
	return bestIdx, bestIdx >= 0
}

// drawSceneLightAndCameraIcons3D draws crisp 2D icons for every visible light and 3D camera in the scene.
func (e *Editor) drawSceneLightAndCameraIcons3D(d *core.Drawing, w, h float32) {
	if e == nil || e.Doc == nil {
		return
	}
	doc := e.resolvedDoc()
	if doc == nil {
		return
	}
	vc := e.activeView3DCam(w, h)
	activeCamID := ""
	if e.LookThroughCamera3D() {
		_, activeCamID, _ = e.ActiveSceneCamera3D(w, h)
	}
	for li := range doc.Layers {
		layer := &doc.Layers[li]
		if !layer.IsVisible() || layer.EffectiveSpace() == sceneformat.SpaceScreen {
			continue
		}
		var walk func(objs []sceneformat.ObjectDoc, parent aff3)
		walk = func(objs []sceneformat.ObjectDoc, parent aff3) {
			for oi := range objs {
				o := &objs[oi]
				if e.hidden[o.ID] {
					continue
				}
				posed := e.posed3D(o)
				world := parent.mul(local3(posed))
				selected := e.IsSelected(o.ID)
				if posed.Light != nil {
					pos := vec32(world.point([3]float64{0, 0, 0}))
					if px, py, _, vis := vc.project(pos, w, h); vis {
						drawLightIcon2D(d, px, py, posed.Light, selected)
					}
				}
				if posed.Camera != nil && o.ID != activeCamID {
					pos := vec32(world.point([3]float64{0, 0, 0}))
					if px, py, _, vis := vc.project(pos, w, h); vis {
						drawCameraIcon2D(d, px, py, posed.Camera, selected)
					}
				}
				if posed.ReflectionProbe != nil {
					pos := vec32(world.point([3]float64{0, 0, 0}))
					if px, py, _, vis := vc.project(pos, w, h); vis {
						drawReflectionProbeIcon2D(d, px, py, posed.ReflectionProbe, selected)
					}
				}
				if posed.LightProbeGrid != nil {
					pos := vec32(world.point([3]float64{0, 0, 0}))
					if px, py, _, vis := vc.project(pos, w, h); vis {
						drawLightProbeGridIcon2D(d, px, py, posed.LightProbeGrid, selected)
					}
				}
				walk(o.Children, world)
			}
		}
		walk(layer.Objects, ident3)
	}
}

func lightIconColor(l *sceneformat.LightDoc) core.Color {
	if l != nil && l.Color != "" {
		if c, err := core.ParseColor(l.Color); err == nil {
			return c
		}
	}
	return core.RGBA(0xFDE047, 1)
}

func drawLightIcon2D(d *core.Drawing, x, y float32, l *sceneformat.LightDoc, selected bool) {
	col := lightIconColor(l)
	ring := core.RGBA(0x0F172A, 0.85)
	if selected {
		ring = core.RGBA(0x38BDF8, 1)
	}
	// Backing disc for contrast over any sky or geometry.
	d.Save().SetFillColor(core.RGBA(0x0F172A, 0.72)).BeginPath().Arc(x, y, 10, 0, float32(2*math.Pi), false).Fill().Restore()
	d.Save().SetStrokeColor(ring).SetLineWidth(1.75).BeginPath().Arc(x, y, 10, 0, float32(2*math.Pi), false).Stroke().Restore()

	switch l.Kind {
	case sceneformat.LightDirectional:
		d.Save().SetFillColor(col).BeginPath().Arc(x, y, 4, 0, float32(2*math.Pi), false).Fill().Restore()
		d.Save().SetStrokeColor(col).SetLineWidth(1.5).BeginPath()
		for i := range 8 {
			a := float64(i) * math.Pi / 4
			cs, sn := float32(math.Cos(a)), float32(math.Sin(a))
			d.MoveTo(x+cs*5.5, y+sn*5.5).LineTo(x+cs*8.5, y+sn*8.5)
		}
		d.Stroke().Restore()
	case sceneformat.LightPoint:
		d.Save().SetFillColor(col).BeginPath().Arc(x, y, 4.5, 0, float32(2*math.Pi), false).Fill().Restore()
		d.Save().SetStrokeColor(col).SetLineWidth(1.25).BeginPath().
			MoveTo(x-7.5, y).LineTo(x-5.5, y).
			MoveTo(x+5.5, y).LineTo(x+7.5, y).
			MoveTo(x, y-7.5).LineTo(x, y-5.5).
			MoveTo(x, y+5.5).LineTo(x, y+7.5).
			Stroke().Restore()
	case sceneformat.LightSpot:
		d.Save().SetStrokeColor(col).SetLineWidth(1.5).BeginPath().
			MoveTo(x, y-5.5).
			LineTo(x-5.5, y+4.5).
			LineTo(x+5.5, y+4.5).
			ClosePath().
			Stroke().Restore()
		d.Save().SetFillColor(col).BeginPath().Arc(x, y-5.5, 2.2, 0, float32(2*math.Pi), false).Fill().Restore()
	}
}

func drawCameraIcon2D(d *core.Drawing, x, y float32, c *sceneformat.Camera3DDoc, selected bool) {
	col := core.RGBA(0xE2E8F0, 1)
	if c != nil && c.Current {
		col = core.RGBA(0x38BDF8, 1)
	}
	ring := core.RGBA(0x0F172A, 0.85)
	if selected {
		ring = core.RGBA(0x38BDF8, 1)
	}
	d.Save().SetFillColor(core.RGBA(0x0F172A, 0.72)).BeginPath().Arc(x, y, 10, 0, float32(2*math.Pi), false).Fill().Restore()
	d.Save().SetStrokeColor(ring).SetLineWidth(1.75).BeginPath().Arc(x, y, 10, 0, float32(2*math.Pi), false).Stroke().Restore()

	// Camera body + lens trapezoid.
	d.Save().SetFillColor(col).FillRect(x-6, y-3.5, 7.5, 7).Restore()
	d.Save().SetFillColor(col).BeginPath().
		MoveTo(x+1.5, y-1.8).
		LineTo(x+6.5, y-4).
		LineTo(x+6.5, y+4).
		LineTo(x+1.5, y+1.8).
		ClosePath().
		Fill().Restore()
}

func drawReflectionProbeIcon2D(d *core.Drawing, x, y float32, p *sceneformat.ReflectionProbeDoc, selected bool) {
	col := core.RGBA(0x22D3EE, 1)
	if p != nil && p.Cubemap == "" {
		col = core.RGBA(0x94A3B8, 1)
	}
	ring := core.RGBA(0x0F172A, 0.85)
	if selected {
		ring = core.RGBA(0x38BDF8, 1)
	}
	d.Save().SetFillColor(core.RGBA(0x0F172A, 0.72)).BeginPath().Arc(x, y, 10, 0, float32(2*math.Pi), false).Fill().Restore()
	d.Save().SetStrokeColor(ring).SetLineWidth(1.75).BeginPath().Arc(x, y, 10, 0, float32(2*math.Pi), false).Stroke().Restore()
	// Chrome sphere badge with specular highlight dot.
	d.Save().SetFillColor(col).BeginPath().Arc(x, y, 5.5, 0, float32(2*math.Pi), false).Fill().Restore()
	d.Save().SetFillColor(core.RGBA(0xFFFFFF, 0.9)).BeginPath().Arc(x-1.8, y-1.8, 1.8, 0, float32(2*math.Pi), false).Fill().Restore()
}

func drawLightProbeGridIcon2D(d *core.Drawing, x, y float32, g *sceneformat.LightProbeGridDoc, selected bool) {
	col := core.RGBA(0xF59E0B, 1)
	if g != nil && g.Data == "" {
		col = core.RGBA(0xCBD5E1, 1)
	}
	ring := core.RGBA(0x0F172A, 0.85)
	if selected {
		ring = core.RGBA(0x38BDF8, 1)
	}
	d.Save().SetFillColor(core.RGBA(0x0F172A, 0.72)).BeginPath().Arc(x, y, 10, 0, float32(2*math.Pi), false).Fill().Restore()
	d.Save().SetStrokeColor(ring).SetLineWidth(1.75).BeginPath().Arc(x, y, 10, 0, float32(2*math.Pi), false).Stroke().Restore()
	// 2x2 warm probe dot grid.
	for _, ox := range [2]float32{-3.2, 3.2} {
		for _, oy := range [2]float32{-3.2, 3.2} {
			d.Save().SetFillColor(col).BeginPath().Arc(x+ox, y+oy, 2, 0, float32(2*math.Pi), false).Fill().Restore()
		}
	}
}

// drawSceneHandles3D draws the wireframe helpers (direction arrow, range sphere, spot cones,
// camera frustum, reflection/light probe bounds) and interactive draggable handles for the
// currently selected light, 3D camera, reflection probe, or light probe grid.
func (e *Editor) drawSceneHandles3D(d *core.Drawing, w, h float32) {
	if e == nil || e.playing || e.SelectionCount() != 1 {
		return
	}
	obj, _ := e.SelectedObject()
	if obj == nil || e.viewportFrozen(obj.ID) {
		return
	}
	if obj.Light == nil && obj.Camera == nil && obj.ReflectionProbe == nil && obj.LightProbeGrid == nil {
		return
	}
	vc := e.activeView3DCam(w, h)
	activeHandle := e.ActiveHandle3D()
	pos64, right, up, fwd := e.objectWorldFrame3D(obj)
	pos := vec32(pos64)

	drawHandleKnob := func(kind Handle3DKind, worldPt [3]float32, col core.Color) {
		px, py, _, vis := vc.project(worldPt, w, h)
		if !vis {
			return
		}
		radius := float32(5.5)
		fill := col
		if activeHandle == kind {
			radius = 7
			fill = core.RGBA(0xFACC15, 1)
		}
		d.Save().SetFillColor(fill).BeginPath().Arc(px, py, radius, 0, float32(2*math.Pi), false).Fill().Restore()
		d.Save().SetStrokeColor(core.RGBA(0x0F172A, 0.9)).SetLineWidth(1.5).BeginPath().Arc(px, py, radius, 0, float32(2*math.Pi), false).Stroke().Restore()
	}

	drawCircle3D := func(center, uAxis, vAxis [3]float64, radius float64, col core.Color, lw float32) {
		if radius <= 1e-4 {
			return
		}
		d.Save().SetStrokeColor(col).SetLineWidth(lw).BeginPath()
		hasSeg := false
		ringPt := func(i int) [3]float32 {
			a := (2 * math.Pi * float64(i)) / float64(handle3DRingSteps)
			p := add64(center, add64(scale64(uAxis, math.Cos(a)*radius), scale64(vAxis, math.Sin(a)*radius)))
			return vec32(p)
		}
		prev := ringPt(0)
		for i := 1; i <= handle3DRingSteps; i++ {
			cur := ringPt(i % handle3DRingSteps)
			if x0, y0, x1, y1, vis := vc.projectSegment(prev, cur, w, h); vis {
				d.MoveTo(x0, y0).LineTo(x1, y1)
				hasSeg = true
			}
			prev = cur
		}
		if hasSeg {
			d.Stroke()
		}
		d.Restore()
	}

	if obj.Light != nil {
		col := lightIconColor(obj.Light)
		soft := col.Faded(0.65)
		switch obj.Light.Kind {
		case sceneformat.LightDirectional:
			if tip, ok := e.HandlePosition3D(Handle3DLightDirection); ok {
				if x0, y0, x1, y1, vis := vc.projectSegment(pos, tip, w, h); vis {
					d.Save().SetStrokeColor(col).SetLineWidth(2.25).BeginPath().MoveTo(x0, y0).LineTo(x1, y1).Stroke().Restore()
					drawArrowHead2D(d, x0, y0, x1, y1, col)
				}
				drawHandleKnob(Handle3DLightDirection, tip, col)
			}

		case sceneformat.LightPoint:
			r := e.effectiveLightRange3D(pos, obj.Light)
			drawCircle3D(pos64, [3]float64{1, 0, 0}, [3]float64{0, 1, 0}, r, soft, 1.5)
			drawCircle3D(pos64, [3]float64{1, 0, 0}, [3]float64{0, 0, 1}, r, soft, 1.5)
			drawCircle3D(pos64, [3]float64{0, 1, 0}, [3]float64{0, 0, 1}, r, soft, 1.5)
			if tip, ok := e.HandlePosition3D(Handle3DLightRange); ok {
				drawHandleKnob(Handle3DLightRange, tip, col)
			}

		case sceneformat.LightSpot:
			coneLen := e.effectiveLightRange3D(pos, obj.Light)
			center := add64(pos64, scale64(fwd, coneLen))
			outerRad := coneLen * math.Tan(float64(math3d.DegToRad(obj.Light.EffectiveOuterCone())))
			innerRad := coneLen * math.Tan(float64(math3d.DegToRad(obj.Light.InnerCone)))

			// Outer cone ring and 4 generator edges.
			drawCircle3D(center, right, up, outerRad, col, 1.75)
			d.Save().SetStrokeColor(soft).SetLineWidth(1.5).BeginPath()
			hasSeg := false
			for _, off := range [4][3]float64{
				scale64(right, outerRad),
				scale64(right, -outerRad),
				scale64(up, outerRad),
				scale64(up, -outerRad),
			} {
				rim := vec32(add64(center, off))
				if x0, y0, x1, y1, vis := vc.projectSegment(pos, rim, w, h); vis {
					d.MoveTo(x0, y0).LineTo(x1, y1)
					hasSeg = true
				}
			}
			if hasSeg {
				d.Stroke()
			}
			d.Restore()

			// Inner cone ring.
			if innerRad > 1e-3 {
				drawCircle3D(center, right, up, innerRad, core.RGBA(0xFDE68A, 0.75), 1.25)
			}
			// Direction axis shaft.
			if dirTip, ok := e.HandlePosition3D(Handle3DLightDirection); ok {
				if x0, y0, x1, y1, vis := vc.projectSegment(pos, dirTip, w, h); vis {
					d.Save().SetStrokeColor(col).SetLineWidth(2).BeginPath().MoveTo(x0, y0).LineTo(x1, y1).Stroke().Restore()
				}
				drawHandleKnob(Handle3DLightDirection, dirTip, col)
			}
			if rangeTip, ok := e.HandlePosition3D(Handle3DLightRange); ok {
				drawHandleKnob(Handle3DLightRange, rangeTip, col)
			}
			if outerTip, ok := e.HandlePosition3D(Handle3DLightOuterCone); ok {
				drawHandleKnob(Handle3DLightOuterCone, outerTip, core.RGBA(0x38BDF8, 1))
			}
			if innerTip, ok := e.HandlePosition3D(Handle3DLightInnerCone); ok {
				drawHandleKnob(Handle3DLightInnerCone, innerTip, core.RGBA(0xFDE68A, 1))
			}
		}
	}

	if obj.Camera != nil {
		if e.LookThroughCamera3D() {
			if _, camID, ok := e.ActiveSceneCamera3D(w, h); ok && camID == obj.ID {
				return
			}
		}
		aspect := float64(16.0 / 9.0)
		if w > 0 && h > 0 {
			aspect = float64(w) / float64(h)
		}
		near := float64(obj.Camera.EffectiveNear())
		previewDist := e.effectiveCameraPreviewDist3D(pos, obj.Camera)
		far := float64(obj.Camera.EffectiveFar())

		planeRect := func(dist float64) [4][3]float32 {
			var halfH float64
			if obj.Camera.EffectiveProjection() == sceneformat.ProjectionOrthographic {
				halfH = float64(obj.Camera.EffectiveOrthoHeight()) * 0.5
			} else {
				halfH = dist * math.Tan(float64(math3d.DegToRad(obj.Camera.EffectiveFOV()))*0.5)
			}
			halfW := halfH * aspect
			c := add64(pos64, scale64(fwd, dist))
			return [4][3]float32{
				vec32(add64(c, add64(scale64(right, -halfW), scale64(up, -halfH)))),
				vec32(add64(c, add64(scale64(right, +halfW), scale64(up, -halfH)))),
				vec32(add64(c, add64(scale64(right, +halfW), scale64(up, +halfH)))),
				vec32(add64(c, add64(scale64(right, -halfW), scale64(up, +halfH)))),
			}
		}

		nearR := planeRect(near)
		prevR := planeRect(previewDist)
		col := core.RGBA(0x38BDF8, 0.95)
		d.Save().SetStrokeColor(col).SetLineWidth(1.5).BeginPath()
		hasSeg := false
		seg := func(a, b [3]float32) {
			if x0, y0, x1, y1, vis := vc.projectSegment(a, b, w, h); vis {
				d.MoveTo(x0, y0).LineTo(x1, y1)
				hasSeg = true
			}
		}
		for i := range 4 {
			j := (i + 1) % 4
			seg(nearR[i], nearR[j])
			seg(prevR[i], prevR[j])
			seg(nearR[i], prevR[i])
		}
		// Up-triangle indicator above top edge of preview rect.
		topMid := vec32(scale64(add64(vec64(prevR[2]), vec64(prevR[3])), 0.5))
		topSpan := len64(sub64(vec64(prevR[2]), vec64(prevR[3])))
		upApex := vec32(add64(vec64(topMid), scale64(up, topSpan*0.18)))
		upL := vec32(add64(vec64(topMid), scale64(right, -topSpan*0.18)))
		upR := vec32(add64(vec64(topMid), scale64(right, topSpan*0.18)))
		seg(upL, upApex)
		seg(upR, upApex)
		if hasSeg {
			d.Stroke()
		}
		d.Restore()

		if far > previewDist+1e-2 {
			farR := planeRect(far)
			d.Save().SetStrokeColor(col.Faded(0.35)).SetLineWidth(1).BeginPath()
			hasFar := false
			for i := range 4 {
				j := (i + 1) % 4
				if x0, y0, x1, y1, vis := vc.projectSegment(farR[i], farR[j], w, h); vis {
					d.MoveTo(x0, y0).LineTo(x1, y1)
					hasFar = true
				}
				if x0, y0, x1, y1, vis := vc.projectSegment(prevR[i], farR[i], w, h); vis {
					d.MoveTo(x0, y0).LineTo(x1, y1)
					hasFar = true
				}
			}
			if hasFar {
				d.Stroke()
			}
			d.Restore()
		}

		if fovTip, ok := e.HandlePosition3D(Handle3DCameraFOV); ok {
			drawHandleKnob(Handle3DCameraFOV, fovTip, col)
		}
		if farTip, ok := e.HandlePosition3D(Handle3DCameraFar); ok {
			drawHandleKnob(Handle3DCameraFar, farTip, core.RGBA(0xA855F7, 1))
		}
	}

	if obj.ReflectionProbe != nil || obj.LightProbeGrid != nil {
		posed := e.posed3D(obj)
		world := e.parentWorld3D(obj.ID).mul(local3(posed))
		boxEdges := [12][2]int{
			{0, 1}, {1, 2}, {2, 3}, {3, 0},
			{4, 5}, {5, 6}, {6, 7}, {7, 4},
			{0, 4}, {1, 5}, {2, 6}, {3, 7},
		}
		drawWireBox := func(center, size [3]float64, col core.Color, lw float32) {
			var corners [8][3]float32
			for i, s := range boxSigns3D {
				p := [3]float64{
					center[0] + s[0]*size[0]*0.5,
					center[1] + s[1]*size[1]*0.5,
					center[2] + s[2]*size[2]*0.5,
				}
				corners[i] = vec32(world.point(p))
			}
			d.Save().SetStrokeColor(col).SetLineWidth(lw).BeginPath()
			hasSeg := false
			for _, edge := range boxEdges {
				if x0, y0, x1, y1, vis := vc.projectSegment(corners[edge[0]], corners[edge[1]], w, h); vis {
					d.MoveTo(x0, y0).LineTo(x1, y1)
					hasSeg = true
				}
			}
			if hasSeg {
				d.Stroke()
			}
			d.Restore()
		}

		if obj.ReflectionProbe != nil {
			c := obj.ReflectionProbe.EffectiveCenter()
			sz := obj.ReflectionProbe.EffectiveSize()
			c64 := [3]float64{float64(c[0]), float64(c[1]), float64(c[2])}
			s64 := [3]float64{float64(sz[0]), float64(sz[1]), float64(sz[2])}
			drawWireBox(c64, s64, core.RGBA(0x22D3EE, 0.95), 1.6)
			if b := float64(obj.ReflectionProbe.BlendDistance); b > 0 {
				inner := [3]float64{
					math.Max(0, s64[0]-2*b),
					math.Max(0, s64[1]-2*b),
					math.Max(0, s64[2]-2*b),
				}
				if inner[0] > 0 && inner[1] > 0 && inner[2] > 0 {
					drawWireBox(c64, inner, core.RGBA(0x22D3EE, 0.4), 1)
				}
			}
		}

		if obj.LightProbeGrid != nil {
			c := obj.LightProbeGrid.EffectiveCenter()
			sz := obj.LightProbeGrid.EffectiveSize()
			cnt := obj.LightProbeGrid.EffectiveCounts()
			c64 := [3]float64{float64(c[0]), float64(c[1]), float64(c[2])}
			s64 := [3]float64{float64(sz[0]), float64(sz[1]), float64(sz[2])}
			drawWireBox(c64, s64, core.RGBA(0xF59E0B, 0.95), 1.6)
			if cnt[0]*cnt[1]*cnt[2] <= 512 {
				sampleCoord := func(i, n int, mid, span float64) float64 {
					if n <= 1 {
						return mid
					}
					return (mid - span*0.5) + span*(float64(i)/float64(n-1))
				}
				dotCol := core.RGBA(0xFBBF24, 0.85)
				for iz := range cnt[2] {
					pz := sampleCoord(iz, cnt[2], c64[2], s64[2])
					for iy := range cnt[1] {
						py := sampleCoord(iy, cnt[1], c64[1], s64[1])
						for ix := range cnt[0] {
							px := sampleCoord(ix, cnt[0], c64[0], s64[0])
							wp := vec32(world.point([3]float64{px, py, pz}))
							if sx, sy, _, vis := vc.project(wp, w, h); vis {
								d.Save().SetFillColor(dotCol).BeginPath().Arc(sx, sy, 2.5, 0, float32(2*math.Pi), false).Fill().Restore()
							}
						}
					}
				}
			}
		}

		for _, hspec := range []struct {
			kind Handle3DKind
			col  core.Color
		}{
			{Handle3DProbeBoundsPosX, core.RGBA(0xEF4444, 1)},
			{Handle3DProbeBoundsNegX, core.RGBA(0xEF4444, 1)},
			{Handle3DProbeBoundsPosY, core.RGBA(0x22C55E, 1)},
			{Handle3DProbeBoundsNegY, core.RGBA(0x22C55E, 1)},
			{Handle3DProbeBoundsPosZ, core.RGBA(0x3B82F6, 1)},
			{Handle3DProbeBoundsNegZ, core.RGBA(0x3B82F6, 1)},
		} {
			if hp, ok := e.HandlePosition3D(hspec.kind); ok {
				drawHandleKnob(hspec.kind, hp, hspec.col)
			}
		}
	}
}

// cameraViewName3D returns the ViewName3D string when looking through the scene's active 3D camera.
func (e *Editor) cameraViewName3D() (string, bool) {
	if !e.LookThroughCamera3D() {
		return "", false
	}
	cam, id, ok := e.ActiveSceneCamera3D(16, 9)
	if !ok {
		return "", false
	}
	name := fmt.Sprintf("Camera (%s)", id)
	if cam != nil && cam.Projection.Kind == scene3d.ProjectionOrthographic {
		name += " · ortho"
	}
	return name, true
}
