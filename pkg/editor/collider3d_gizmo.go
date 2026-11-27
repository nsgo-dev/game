package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"time"

	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/world3d"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// minColliderSize3D is the smallest positive extent or radius a collider handle drag
	// or bounds fit will produce, keeping physics3d shapes non-degenerate.
	minColliderSize3D = 0.01
	// colliderHandleHalfSize is half the side of a 3D collider handle marker in canvas points.
	colliderHandleHalfSize float32 = 5
)

// ColliderHandle3D describes one draggable size handle of a 3D collider or trigger
// component in world space, edited through the 3D gizmo API in Gizmo3DCollider mode.
type ColliderHandle3D struct {
	// Component is "collider3d" or "trigger3d".
	Component string
	// Shape is "box", "sphere", "capsule", or "cylinder".
	Shape string
	// Field is the component field adjusted by dragging this handle: "width", "height", "depth", or "radius".
	Field string
	// Axis is the 3D gizmo axis (Axis3DX, Axis3DY, or Axis3DZ) mapped to this handle.
	Axis GizmoAxis3D
	// Sign is +1 for the positive local axis end and -1 for the negative end.
	Sign float32
	// Pos is the handle's world-space position on the collider surface.
	Pos [3]float32
	// Dir is the handle's world-space outward unit direction.
	Dir [3]float32
}

type collider3DFrame struct {
	comp       string
	shape      string
	resolved   map[string]any
	center     physics3d.Vec3
	rot        physics3d.Quat
	sx, sy, sz float32
}

// objectWorldTransform3D returns the composed 3D world transform of object o using
// sceneformat.ObjectLocalTransform3D and math3d.Transform.Mul so collider gizmos,
// wireframes, and BuildPhysics share the exact same world coordinates.
func (e *Editor) objectWorldTransform3D(o *sceneformat.ObjectDoc) math3d.Transform {
	if o == nil {
		return math3d.TransformIdentity()
	}
	if e.clipScene != nil {
		if bo := e.clipScene.Object(o.ID); bo != nil {
			var chain []*sceneformat.ObjectDoc
			for p := bo; p != nil; p = p.Parent {
				chain = append(chain, p.Doc)
			}
			world := math3d.TransformIdentity()
			for i := len(chain) - 1; i >= 0; i-- {
				world = world.Mul(sceneformat.ObjectLocalTransform3D(chain[i]))
			}
			return world
		}
	}
	path := objectPath(e.Doc, o.ID)
	if len(path) == 0 {
		return sceneformat.ObjectLocalTransform3D(o)
	}
	world := math3d.TransformIdentity()
	for _, p := range path {
		world = world.Mul(sceneformat.ObjectLocalTransform3D(p))
	}
	return world
}

func colliderEulerToQuat3D(rx, ry, rz float32) physics3d.Quat {
	if rx == 0 && ry == 0 && rz == 0 {
		return physics3d.QuatIdentity()
	}
	qx := math3d.QuatFromAxisAngle(math3d.Vec3{X: 1}, rx)
	qy := math3d.QuatFromAxisAngle(math3d.Vec3{Y: 1}, ry)
	qz := math3d.QuatFromAxisAngle(math3d.Vec3{Z: 1}, rz)
	q := qz.Mul(qy).Mul(qx).Norm()
	return physics3d.Quat{X: q.X, Y: q.Y, Z: q.Z, W: q.W}
}

func (e *Editor) editableCollider3DFrame(o *sceneformat.ObjectDoc) (collider3DFrame, bool) {
	if e == nil || o == nil || len(o.Components) == 0 {
		return collider3DFrame{}, false
	}
	reg := sceneformat.PhysicsComponentRegistry()
	var compName string
	var raw any
	if r, ok := o.Components[sceneformat.Collider3DComponentName]; ok {
		compName, raw = sceneformat.Collider3DComponentName, r
	} else if r, ok := o.Components[sceneformat.Trigger3DComponentName]; ok {
		compName, raw = sceneformat.Trigger3DComponentName, r
	} else {
		return collider3DFrame{}, false
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return collider3DFrame{}, false
	}
	resolved, err := reg.Resolve(compName, m)
	if err != nil {
		return collider3DFrame{}, false
	}
	shape, _ := resolved["shape"].(string)
	switch shape {
	case "box", "sphere", "capsule", "cylinder":
	default:
		return collider3DFrame{}, false
	}

	world := e.objectWorldTransform3D(o)
	sx := float32(math.Abs(float64(world.Scale.X)))
	sy := float32(math.Abs(float64(world.Scale.Y)))
	sz := float32(math.Abs(float64(world.Scale.Z)))
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	if sz == 0 {
		sz = 1
	}

	localCenter := physics3d.Vec3{
		X: float32(resolved["offsetX"].(float64)) * sx,
		Y: float32(resolved["offsetY"].(float64)) * sy,
		Z: float32(resolved["offsetZ"].(float64)) * sz,
	}
	worldPos := physics3d.Vec3{X: world.Pos.X, Y: world.Pos.Y, Z: world.Pos.Z}
	worldRot := physics3d.Quat{X: world.Rot.X, Y: world.Rot.Y, Z: world.Rot.Z, W: world.Rot.W}
	if worldRot.IsIdentity() {
		worldRot = physics3d.QuatIdentity()
	} else {
		worldRot = worldRot.Norm()
	}
	worldCenter := worldRot.RotateVec(localCenter).Add(worldPos)

	rx, _ := resolved["rotX"].(float64)
	ry, _ := resolved["rotY"].(float64)
	rz, _ := resolved["rotZ"].(float64)
	qLocal := colliderEulerToQuat3D(float32(rx), float32(ry), float32(rz))
	combinedRot := worldRot.Mul(qLocal).Norm()

	return collider3DFrame{
		comp:     compName,
		shape:    shape,
		resolved: resolved,
		center:   worldCenter,
		rot:      combinedRot,
		sx:       sx,
		sy:       sy,
		sz:       sz,
	}, true
}

// ColliderHandles3D returns the world-space size handles of object o's 3D collider
// (or trigger3d if it has no collider3d). Geometry-derived shapes (convex, trimesh,
// heightfield) and objects without a 3D collider return nil.
func (e *Editor) ColliderHandles3D(o *sceneformat.ObjectDoc) []ColliderHandle3D {
	frame, ok := e.editableCollider3DFrame(o)
	if !ok {
		return nil
	}
	ux := frame.rot.RotateVec(physics3d.Vec3{X: 1})
	uy := frame.rot.RotateVec(physics3d.Vec3{Y: 1})
	uz := frame.rot.RotateVec(physics3d.Vec3{Z: 1})

	makeHandle := func(field string, axis GizmoAxis3D, basis physics3d.Vec3, sign, reach float32) ColliderHandle3D {
		dir := basis.Scale(sign)
		pos := frame.center.Add(dir.Scale(reach))
		return ColliderHandle3D{
			Component: frame.comp,
			Shape:     frame.shape,
			Field:     field,
			Axis:      axis,
			Sign:      sign,
			Pos:       [3]float32{pos.X, pos.Y, pos.Z},
			Dir:       [3]float32{dir.X, dir.Y, dir.Z},
		}
	}

	switch frame.shape {
	case "box":
		hx := float32(frame.resolved["width"].(float64)) * frame.sx * 0.5
		hy := float32(frame.resolved["height"].(float64)) * frame.sy * 0.5
		hz := float32(frame.resolved["depth"].(float64)) * frame.sz * 0.5
		return []ColliderHandle3D{
			makeHandle("width", Axis3DX, ux, +1, hx),
			makeHandle("width", Axis3DX, ux, -1, hx),
			makeHandle("height", Axis3DY, uy, +1, hy),
			makeHandle("height", Axis3DY, uy, -1, hy),
			makeHandle("depth", Axis3DZ, uz, +1, hz),
			makeHandle("depth", Axis3DZ, uz, -1, hz),
		}
	case "sphere":
		r := float32(frame.resolved["radius"].(float64)) * max(frame.sx, max(frame.sy, frame.sz))
		return []ColliderHandle3D{
			makeHandle("radius", Axis3DX, ux, +1, r),
			makeHandle("radius", Axis3DX, ux, -1, r),
			makeHandle("radius", Axis3DY, uy, +1, r),
			makeHandle("radius", Axis3DY, uy, -1, r),
			makeHandle("radius", Axis3DZ, uz, +1, r),
			makeHandle("radius", Axis3DZ, uz, -1, r),
		}
	case "capsule":
		r := float32(frame.resolved["radius"].(float64)) * max(frame.sx, frame.sz)
		hy := max(float32(frame.resolved["height"].(float64))*frame.sy*0.5, r)
		return []ColliderHandle3D{
			makeHandle("radius", Axis3DX, ux, +1, r),
			makeHandle("radius", Axis3DX, ux, -1, r),
			makeHandle("height", Axis3DY, uy, +1, hy),
			makeHandle("height", Axis3DY, uy, -1, hy),
			makeHandle("radius", Axis3DZ, uz, +1, r),
			makeHandle("radius", Axis3DZ, uz, -1, r),
		}
	case "cylinder":
		r := float32(frame.resolved["radius"].(float64)) * max(frame.sx, frame.sz)
		hy := float32(frame.resolved["height"].(float64)) * frame.sy * 0.5
		return []ColliderHandle3D{
			makeHandle("radius", Axis3DX, ux, +1, r),
			makeHandle("radius", Axis3DX, ux, -1, r),
			makeHandle("height", Axis3DY, uy, +1, hy),
			makeHandle("height", Axis3DY, uy, -1, hy),
			makeHandle("radius", Axis3DZ, uz, +1, r),
			makeHandle("radius", Axis3DZ, uz, -1, r),
		}
	default:
		return nil
	}
}

func (e *Editor) pickColliderHandle3D(o *sceneformat.ObjectDoc, sx, sy, w, h float32) (ColliderHandle3D, bool) {
	handles := e.ColliderHandles3D(o)
	if len(handles) == 0 {
		return ColliderHandle3D{}, false
	}
	var best ColliderHandle3D
	bestDist := float64(gizmoHitRadius)
	bestDepth := float32(math.Inf(1))
	found := false
	for _, hh := range handles {
		px, py, depth, vis := e.Camera3D.Project(hh.Pos, w, h)
		if !vis {
			continue
		}
		d := math.Hypot(float64(sx-px), float64(sy-py))
		if d < bestDist-1e-3 || (math.Abs(d-bestDist) <= 1e-3 && depth < bestDepth) {
			best = hh
			bestDist = d
			bestDepth = depth
			found = true
		}
	}
	return best, found
}

func (e *Editor) beginColliderDrag3D(obj *sceneformat.ObjectDoc, handle ColliderHandle3D, sx, sy, w, h float32) bool {
	frame, ok := e.editableCollider3DFrame(obj)
	if !ok {
		return false
	}
	rawComp, compExisted := obj.Components[handle.Component]
	compMap, _ := rawComp.(map[string]any)
	oldVal, fieldExisted := any(nil), false
	if compMap != nil {
		oldVal, fieldExisted = compMap[handle.Field]
	}
	startVal, _ := frame.resolved[handle.Field].(float64)

	var axisScale float64
	isRadius := handle.Field == "radius"
	switch {
	case handle.Field == "width":
		axisScale = float64(frame.sx)
	case handle.Field == "height":
		axisScale = float64(frame.sy)
	case handle.Field == "depth":
		axisScale = float64(frame.sz)
	case handle.Shape == "sphere":
		axisScale = float64(max(frame.sx, max(frame.sy, frame.sz)))
	default: // capsule or cylinder radius
		axisScale = float64(max(frame.sx, frame.sz))
	}
	if axisScale <= 1e-6 {
		axisScale = 1
	}

	minVal := minColliderSize3D
	maxVal := 0.0
	if handle.Shape == "capsule" {
		switch handle.Field {
		case "height":
			if r, ok := frame.resolved["radius"].(float64); ok && 2*r > minVal {
				minVal = 2 * r
			}
		case "radius":
			if hh, ok := frame.resolved["height"].(float64); ok && hh > 0 {
				maxVal = max(minColliderSize3D, hh*0.5)
			}
		}
	}

	st := drag3DState{
		kind:                 drag3DGizmo,
		objectID:             obj.ID,
		mode:                 Gizmo3DCollider,
		axis:                 handle.Axis,
		startSX:              sx,
		startSY:              sy,
		lastSX:               sx,
		lastSY:               sy,
		vpW:                  w,
		vpH:                  h,
		gizmoLen:             e.GizmoLength3D(handle.Pos),
		startPos:             vec64(handle.Pos),
		colliderComp:         handle.Component,
		colliderField:        handle.Field,
		colliderSign:         handle.Sign,
		colliderAxisDir:      vec64(handle.Dir),
		colliderAxisScale:    axisScale,
		colliderIsRadius:     isRadius,
		colliderWasNil:       obj.Components == nil,
		colliderCompExisted:  compExisted,
		colliderFieldExisted: fieldExisted,
		colliderOldVal:       cloneAny(oldVal),
		colliderStartVal:     startVal,
		colliderMinVal:       minVal,
		colliderMaxVal:       maxVal,
	}

	orig32, dir32 := e.Camera3D.ScreenRay(sx, sy, w, h)
	if t, ok := closestParamOnAxisLine64(st.startPos, st.colliderAxisDir, vec64(orig32), vec64(dir32)); ok {
		st.startAxisT = t
		st.hasAxisT = true
	}

	e.drag3D = st
	e.notify()
	return true
}

func (e *Editor) updateColliderDrag3D(obj *sceneformat.ObjectDoc, st *drag3DState, sx, sy float32) {
	orig32, dir32 := e.Camera3D.ScreenRay(sx, sy, st.vpW, st.vpH)
	worldDelta := e.computeAxisDelta(st, st.startPos, st.colliderAxisDir, vec64(orig32), vec64(dir32), sx, sy)
	if math.IsNaN(worldDelta) || math.IsInf(worldDelta, 0) {
		return
	}
	localDelta := worldDelta / st.colliderAxisScale
	var newVal float64
	if st.colliderIsRadius {
		newVal = st.colliderStartVal + localDelta
	} else {
		// Box width/height/depth and capsule/cylinder total height span both +/- sides around center.
		newVal = st.colliderStartVal + 2*localDelta
	}
	if grid, pixel := e.positionSnapMode(); grid || pixel {
		newVal = e.SnapValue(newVal)
	}
	minVal := st.colliderMinVal
	if minVal < minColliderSize3D {
		minVal = minColliderSize3D
	}
	if newVal < minVal {
		newVal = minVal
	}
	if st.colliderMaxVal > 0 && newVal > st.colliderMaxVal {
		newVal = st.colliderMaxVal
	}
	if obj.Components == nil {
		obj.Components = make(map[string]any)
	}
	m, ok := obj.Components[st.colliderComp].(map[string]any)
	if !ok || m == nil {
		m = make(map[string]any)
		obj.Components[st.colliderComp] = m
	}
	m[st.colliderField] = newVal
	st.lastSX = sx
	st.lastSY = sy
	e.notify()
}

func restoreColliderDragState(obj *sceneformat.ObjectDoc, st *drag3DState) {
	if obj == nil || obj.Components == nil {
		return
	}
	if !st.colliderCompExisted {
		delete(obj.Components, st.colliderComp)
		if st.colliderWasNil && len(obj.Components) == 0 {
			obj.Components = nil
		}
		return
	}
	m, ok := obj.Components[st.colliderComp].(map[string]any)
	if !ok || m == nil {
		return
	}
	if !st.colliderFieldExisted {
		delete(m, st.colliderField)
		return
	}
	m[st.colliderField] = cloneAny(st.colliderOldVal)
}

func (e *Editor) endColliderDrag3D(st drag3DState) bool {
	obj, _, _ := e.FindObject(st.objectID)
	if obj == nil {
		e.notify()
		return false
	}
	var afterVal any
	if m, ok := obj.Components[st.colliderComp].(map[string]any); ok {
		afterVal = m[st.colliderField]
	}
	if afterVal == nil || (st.colliderFieldExisted && reflect.DeepEqual(st.colliderOldVal, afterVal)) ||
		(!st.colliderFieldExisted && reflect.DeepEqual(st.colliderStartVal, afterVal)) {
		restoreColliderDragState(obj, &st)
		e.notify()
		return false
	}
	// Restore exact pre-drag state before committing as one History step so Undo is byte-for-byte.
	restoreColliderDragState(obj, &st)
	e.History.Execute(&setComponentFieldCmd{
		ed:          e,
		objectID:    st.objectID,
		component:   st.colliderComp,
		field:       st.colliderField,
		wasNilComps: st.colliderWasNil,
		compExisted: st.colliderCompExisted,
		existed:     st.colliderFieldExisted,
		oldVal:      cloneAny(st.colliderOldVal),
		newVal:      cloneAny(afterVal),
	})
	return true
}

func (e *Editor) drawColliderGizmo3D(d *core.Drawing, obj *sceneformat.ObjectDoc, w, h float32) {
	frame, ok := e.editableCollider3DFrame(obj)
	if !ok {
		return
	}
	// Ensure the edited collider wireframe is visible even when global ShowPhysicsDebug is off.
	if !e.ShowPhysicsDebug {
		world := e.objectWorldTransform3D(obj)
		for _, item := range sceneformat.ExtractPhysicsDebug3DAtWithResolver(obj, world, e.physicsResolver()) {
			e.drawPhysicsShape3D(d, item, w, h)
		}
	}

	handles := e.ColliderHandles3D(obj)
	if len(handles) == 0 {
		return
	}
	center := [3]float32{frame.center.X, frame.center.Y, frame.center.Z}
	activeAxis := e.ActiveGizmoAxis3D()
	activeSign := float32(0)
	if e.drag3D.kind == drag3DGizmo && e.drag3D.mode == Gizmo3DCollider {
		activeSign = e.drag3D.colliderSign
	}

	spec := e.activeSpecPalette()
	axisColor := func(ax GizmoAxis3D) core.Color {
		switch ax {
		case Axis3DX:
			return spec.AxisX
		case Axis3DY:
			return spec.AxisY
		case Axis3DZ:
			return spec.AxisZ
		default:
			return spec.Warning.Fg
		}
	}

	for _, hh := range handles {
		col := axisColor(hh.Axis)
		half := colliderHandleHalfSize
		active := hh.Axis == activeAxis && (activeSign == 0 || hh.Sign == activeSign)
		if active {
			col = spec.Warning.Fg
			half += 1.5
		}
		if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(center, hh.Pos, w, h); vis {
			d.Save().SetStrokeColor(col.Faded(0.55)).SetLineWidth(1.25).BeginPath().MoveTo(x0, y0).LineTo(x1, y1).Stroke().Restore()
		}
		if px, py, _, vis := e.Camera3D.Project(hh.Pos, w, h); vis {
			d.Save().SetFillColor(col).SetStrokeColor(specAlpha(spec.Bg, 0.95)).SetLineWidth(1.25).
				BeginPath().
				MoveTo(px, py-half).
				LineTo(px+half, py).
				LineTo(px, py+half).
				LineTo(px-half, py).
				ClosePath().
				Fill().Stroke().Restore()
		}
	}
}

// FitCollider3DToBounds sizes objectID's 3D collider component (collider3d, or trigger3d
// if it has no collider3d; creating collider3d as a box if neither is present) to match
// the local-space bounds of the object's 3D mesh or glTF model geometry as a single undo step.
func (e *Editor) FitCollider3DToBounds(objectID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return fmt.Errorf("editor: object %q not found", objectID)
	}
	s := e.scene3D()
	bounds, ok := e.geometryBounds3D(objectID)
	if !ok {
		if s.err != nil {
			return fmt.Errorf("editor: fit collider bounds for %q: %w", objectID, s.err)
		}
		return fmt.Errorf("editor: object %q has no 3D model or mesh geometry to fit collider bounds", objectID)
	}

	compName := sceneformat.Collider3DComponentName
	if obj.Components != nil {
		if _, hasCol := obj.Components[sceneformat.Collider3DComponentName]; !hasCol {
			if _, hasTrig := obj.Components[sceneformat.Trigger3DComponentName]; hasTrig {
				compName = sceneformat.Trigger3DComponentName
			}
		}
	}

	var prevMap map[string]any
	compExisted := false
	if obj.Components != nil {
		if raw, exists := obj.Components[compName]; exists {
			compExisted = true
			m, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("editor: component %q on %q is not a map", compName, objectID)
			}
			prevMap = m
		}
	}

	physReg := sceneformat.PhysicsComponentRegistry()
	resolved, err := physReg.Resolve(compName, prevMap)
	if err != nil {
		return err
	}
	shape, _ := resolved["shape"].(string)
	if shape == "heightfield" {
		return fmt.Errorf("editor: heightfield collider on %q is sized by its terrain file, not model bounds", objectID)
	}

	nextMap := make(map[string]any, len(prevMap)+8)
	for k, v := range prevMap {
		nextMap[k] = cloneAny(v)
	}
	if !compExisted {
		nextMap["version"] = sceneformat.PhysicsComponentVersion
		nextMap["shape"] = "box"
	}

	center := bounds.Center()
	size := bounds.Size()
	clampDim := func(v float32) float64 {
		if v < minColliderSize3D {
			return minColliderSize3D
		}
		return float64(v)
	}
	setOrClearZero := func(key string, val float64) {
		if val == 0 {
			if _, had := prevMap[key]; had {
				nextMap[key] = 0.0
			} else {
				delete(nextMap, key)
			}
			return
		}
		nextMap[key] = val
	}

	switch shape {
	case "box":
		nextMap["width"] = clampDim(size.X)
		nextMap["height"] = clampDim(size.Y)
		nextMap["depth"] = clampDim(size.Z)
		setOrClearZero("offsetX", float64(center.X))
		setOrClearZero("offsetY", float64(center.Y))
		setOrClearZero("offsetZ", float64(center.Z))
		setOrClearZero("rotX", 0)
		setOrClearZero("rotY", 0)
		setOrClearZero("rotZ", 0)
	case "sphere":
		maxDim := max(size.X, max(size.Y, size.Z))
		nextMap["radius"] = clampDim(maxDim * 0.5)
		setOrClearZero("offsetX", float64(center.X))
		setOrClearZero("offsetY", float64(center.Y))
		setOrClearZero("offsetZ", float64(center.Z))
	case "capsule":
		r := clampDim(max(size.X, size.Z) * 0.5)
		h := clampDim(size.Y)
		if h < 2*r {
			h = 2 * r
		}
		nextMap["radius"] = r
		nextMap["height"] = h
		setOrClearZero("offsetX", float64(center.X))
		setOrClearZero("offsetY", float64(center.Y))
		setOrClearZero("offsetZ", float64(center.Z))
		setOrClearZero("rotX", 0)
		setOrClearZero("rotY", 0)
		setOrClearZero("rotZ", 0)
	case "cylinder":
		nextMap["radius"] = clampDim(max(size.X, size.Z) * 0.5)
		nextMap["height"] = clampDim(size.Y)
		setOrClearZero("offsetX", float64(center.X))
		setOrClearZero("offsetY", float64(center.Y))
		setOrClearZero("offsetZ", float64(center.Z))
		setOrClearZero("rotX", 0)
		setOrClearZero("rotY", 0)
		setOrClearZero("rotZ", 0)
	case "convex", "trimesh":
		setOrClearZero("offsetX", 0)
		setOrClearZero("offsetY", 0)
		setOrClearZero("offsetZ", 0)
		setOrClearZero("rotX", 0)
		setOrClearZero("rotY", 0)
		setOrClearZero("rotZ", 0)
	}

	reg := e.components
	if reg == nil {
		reg = builtinEditorComponents
	}
	if err := reg.CheckComponent(compName, nextMap); err != nil {
		var ce *sceneformat.ComponentError
		if errors.As(err, &ce) {
			ce.Object = objectID
		}
		return err
	}

	if compExisted && reflect.DeepEqual(prevMap, nextMap) {
		return nil
	}

	e.History.Execute(&setComponentMapCmd{
		ed:          e,
		objectID:    objectID,
		component:   compName,
		label:       "Fit " + compName + " to " + objectID + " bounds",
		wasNilComps: obj.Components == nil,
		compExisted: compExisted,
		oldMap:      cloneAny(prevMap),
		newMap:      cloneAny(nextMap),
	})
	return nil
}

type setComponentMapCmd struct {
	ed          *Editor
	objectID    string
	component   string
	label       string
	wasNilComps bool
	compExisted bool
	oldMap      any
	newMap      any
}

func (c *setComponentMapCmd) Label() string { return c.label }

func (c *setComponentMapCmd) Do() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil {
		return
	}
	if obj.Components == nil {
		obj.Components = make(map[string]any)
	}
	obj.Components[c.component] = cloneAny(c.newMap)
	c.ed.Status = c.label
}

func (c *setComponentMapCmd) Undo() {
	obj, _, _ := c.ed.FindObject(c.objectID)
	if obj == nil || obj.Components == nil {
		return
	}
	if !c.compExisted {
		delete(obj.Components, c.component)
		if c.wasNilComps && len(obj.Components) == 0 {
			obj.Components = nil
		}
		return
	}
	obj.Components[c.component] = cloneAny(c.oldMap)
}

// physicsAssetResolver bridges the editor's Resolver and the open project's files so
// sceneformat.BuildPhysics and 3D collider debug wireframes can load models, terrains,
// and physics materials consistently in both Play mode and edit mode.
type physicsAssetResolver struct {
	ed       *Editor
	resolver sceneformat.AssetResolver
	assets   scene3DAssets
	fsys     fs.FS
}

func (e *Editor) physicsResolver() sceneformat.AssetResolver {
	if e == nil {
		return nil
	}
	var fsys fs.FS
	if e.ProjectPath != "" {
		fsys = os.DirFS(filepath.Dir(e.ProjectPath))
	}
	assets := e.modelAssets3D()
	if e.Resolver == nil && fsys == nil && assets.files == nil && !e.TilesetOpen() {
		return nil
	}
	return physicsAssetResolver{
		ed:       e,
		resolver: e.Resolver,
		assets:   assets,
		fsys:     fsys,
	}
}

func (r physicsAssetResolver) Atlas(name string) (*sprite.Atlas, error) {
	if r.resolver == nil {
		return nil, fmt.Errorf("editor: atlas %q requires a Resolver", name)
	}
	return r.resolver.Atlas(name)
}

func (r physicsAssetResolver) PhysicsMaterial(path string) (*resource.PhysicsMaterial, error) {
	if pmr, ok := r.resolver.(sceneformat.PhysicsMaterialResolver); ok {
		return pmr.PhysicsMaterial(path)
	}
	if r.assets.files != nil {
		return r.assets.files.Library().PhysicsMaterial(path)
	}
	if r.fsys != nil {
		return resource.NewLibrary(r.fsys).PhysicsMaterial(path)
	}
	return nil, errNoProjectFiles
}

func (r physicsAssetResolver) Model(path string) (*gltf.Document, error) {
	if mr, ok := r.resolver.(sceneformat.ModelResolver); ok {
		return mr.Model(path)
	}
	if r.assets.files != nil {
		return r.assets.files.Model(path)
	}
	if fr, ok := r.resolver.(sceneformat.FileResolver); ok {
		return gltf.LoadFS(editorFileResolverFS{fr: fr}, path)
	}
	if r.fsys != nil {
		return gltf.LoadFS(r.fsys, path)
	}
	return nil, errNoProjectFiles
}

func (r physicsAssetResolver) Terrain(path string) (*world3d.Heightmap, error) {
	if tr, ok := r.resolver.(sceneformat.TerrainResolver); ok {
		return tr.Terrain(path)
	}
	if r.fsys != nil {
		hm, _, err := world3d.LoadTerrainFS(r.fsys, path)
		return hm, err
	}
	if fr, ok := r.resolver.(sceneformat.FileResolver); ok {
		hm, _, err := world3d.LoadTerrainFS(editorFileResolverFS{fr: fr}, path)
		return hm, err
	}
	return nil, errNoProjectFiles
}

func (r physicsAssetResolver) ReadFile(path string) ([]byte, error) {
	if data, ok, err := r.ed.liveTilesetYAML(path); ok {
		return data, err
	}
	if fr, ok := r.resolver.(sceneformat.FileResolver); ok {
		return fr.ReadFile(path)
	}
	if r.fsys != nil {
		return fs.ReadFile(r.fsys, path)
	}
	return nil, errNoProjectFiles
}

type editorFileResolverFS struct {
	fr sceneformat.FileResolver
}

func (f editorFileResolverFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	data, err := f.fr.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return &editorMemFile{name: path.Base(name), r: bytes.NewReader(data), size: int64(len(data))}, nil
}

type editorMemFile struct {
	name string
	r    *bytes.Reader
	size int64
}

func (f *editorMemFile) Stat() (fs.FileInfo, error) {
	return editorMemFileInfo{name: f.name, size: f.size}, nil
}
func (f *editorMemFile) Close() error               { return nil }
func (f *editorMemFile) Read(p []byte) (int, error) { return f.r.Read(p) }

type editorMemFileInfo struct {
	name string
	size int64
}

func (fi editorMemFileInfo) Name() string       { return fi.name }
func (fi editorMemFileInfo) Size() int64        { return fi.size }
func (fi editorMemFileInfo) Mode() fs.FileMode  { return 0o444 }
func (fi editorMemFileInfo) ModTime() time.Time { return time.Time{} }
func (fi editorMemFileInfo) IsDir() bool        { return false }
func (fi editorMemFileInfo) Sys() any           { return nil }
