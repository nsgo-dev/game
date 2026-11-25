package editor

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	maxOrbitPitch    float32 = float32(math.Pi/2) - 0.01
	minOrbitDistance float32 = 0.5
	maxOrbitDistance float32 = 50000
	gizmoHitRadius   float32 = 12
	gizmoRingSteps           = 32
)

// OrbitCamera3D is a pure-Go right-handed (Y-up) perspective orbit camera for the 3D scene view.
type OrbitCamera3D struct {
	Target   [3]float32
	Yaw      float32
	Pitch    float32
	Distance float32
	FOV      float32
	Near     float32
	Far      float32
}

// DefaultOrbitCamera3D returns a perspective orbit camera framed on the world origin.
func DefaultOrbitCamera3D() OrbitCamera3D {
	return OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.65,
		Pitch:    0.45,
		Distance: 240,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      10000,
	}
}

func (c OrbitCamera3D) effectiveParams() (dist, fov, near, far float64) {
	dist = float64(c.Distance)
	if dist <= 0 || math.IsNaN(dist) || math.IsInf(dist, 0) {
		dist = 240
	}
	fov = float64(c.FOV)
	if fov <= 0.05 || fov >= math.Pi-0.05 || math.IsNaN(fov) || math.IsInf(fov, 0) {
		fov = math.Pi / 3
	}
	near = float64(c.Near)
	if near <= 0 || math.IsNaN(near) || math.IsInf(near, 0) {
		near = 0.5
	}
	far = float64(c.Far)
	if far <= near || math.IsNaN(far) || math.IsInf(far, 0) {
		far = near + 10000
	}
	return dist, fov, near, far
}

// Eye returns the camera's world-space position derived from Target, Yaw, Pitch, and Distance.
func (c OrbitCamera3D) Eye() [3]float32 {
	eye := c.eye64()
	return [3]float32{float32(eye[0]), float32(eye[1]), float32(eye[2])}
}

func (c OrbitCamera3D) eye64() [3]float64 {
	dist, _, _, _ := c.effectiveParams()
	pitch := clamp64(float64(c.Pitch), -float64(maxOrbitPitch), float64(maxOrbitPitch))
	yaw := float64(c.Yaw)
	cosP := math.Cos(pitch)
	sinP := math.Sin(pitch)
	sinY := math.Sin(yaw)
	cosY := math.Cos(yaw)
	return [3]float64{
		float64(c.Target[0]) + dist*cosP*sinY,
		float64(c.Target[1]) + dist*sinP,
		float64(c.Target[2]) + dist*cosP*cosY,
	}
}

// Basis returns the camera's orthonormal right, up, and forward (eye-to-target) unit vectors in world space.
func (c OrbitCamera3D) Basis() (right, up, forward [3]float32) {
	r, u, f := c.basis64()
	return vec32(r), vec32(u), vec32(f)
}

func (c OrbitCamera3D) basis64() (right, up, forward [3]float64) {
	eye := c.eye64()
	target := vec64(c.Target)
	forward = normalize64(sub64(target, eye))
	if len64(forward) == 0 {
		forward = [3]float64{0, 0, -1}
	}
	worldUp := [3]float64{0, 1, 0}
	right = normalize64(cross64(forward, worldUp))
	if len64(right) == 0 {
		right = [3]float64{1, 0, 0}
	}
	up = normalize64(cross64(right, forward))
	return right, up, forward
}

// Orbit rotates the camera around Target by dyaw (azimuth) and dpitch (elevation) in radians.
func (c *OrbitCamera3D) Orbit(dyaw, dpitch float32) {
	if c == nil {
		return
	}
	c.Yaw += dyaw
	c.Pitch += dpitch
	if c.Pitch > maxOrbitPitch {
		c.Pitch = maxOrbitPitch
	}
	if c.Pitch < -maxOrbitPitch {
		c.Pitch = -maxOrbitPitch
	}
}

// Pan translates Target parallel to the camera image plane by dx along camera right and dy along camera up.
func (c *OrbitCamera3D) Pan(dx, dy float32) {
	if c == nil {
		return
	}
	right, up, _ := c.basis64()
	c.Target[0] += float32(right[0]*float64(dx) + up[0]*float64(dy))
	c.Target[1] += float32(right[1]*float64(dx) + up[1]*float64(dy))
	c.Target[2] += float32(right[2]*float64(dx) + up[2]*float64(dy))
}

// Zoom adjusts camera Distance by subtracting delta (positive zooms in, negative zooms out),
// clamped to a safe positive range.
func (c *OrbitCamera3D) Zoom(delta float32) {
	if c == nil {
		return
	}
	if c.Distance <= 0 {
		c.Distance = 240
	}
	c.Distance -= delta
	if c.Distance < minOrbitDistance {
		c.Distance = minOrbitDistance
	}
	if c.Distance > maxOrbitDistance {
		c.Distance = maxOrbitDistance
	}
}

// Project transforms a world-space 3D point into viewport pixel coordinates (sx, sy) and camera depth.
// visible is true when the point lies strictly between Near and Far in front of the camera.
func (c OrbitCamera3D) Project(world [3]float32, w, h float32) (sx, sy, depth float32, visible bool) {
	if w <= 0 || h <= 0 {
		return 0, 0, 0, false
	}
	_, fov, near, far := c.effectiveParams()
	eye := c.eye64()
	right, up, forward := c.basis64()
	rel := sub64(vec64(world), eye)

	cx := dot64(rel, right)
	cy := dot64(rel, up)
	cz := dot64(rel, forward)
	if cz <= near || cz >= far {
		return 0, 0, float32(cz), false
	}

	tanHalf := math.Tan(fov * 0.5)
	aspect := float64(w) / float64(h)
	ndcX := cx / (cz * tanHalf * aspect)
	ndcY := cy / (cz * tanHalf)
	sx = float32((ndcX + 1) * 0.5 * float64(w))
	sy = float32((1 - ndcY) * 0.5 * float64(h))
	return sx, sy, float32(cz), true
}

// ScreenRay returns the world-space ray (origin, unit direction) passing through viewport pixel (sx, sy).
func (c OrbitCamera3D) ScreenRay(sx, sy, w, h float32) (origin, dir [3]float32) {
	eye := c.eye64()
	if w <= 0 || h <= 0 {
		_, _, forward := c.basis64()
		return vec32(eye), vec32(forward)
	}
	_, fov, _, _ := c.effectiveParams()
	right, up, forward := c.basis64()
	tanHalf := math.Tan(fov * 0.5)
	aspect := float64(w) / float64(h)

	ndcX := (float64(sx)/float64(w))*2 - 1
	ndcY := 1 - (float64(sy)/float64(h))*2

	d := add64(
		add64(scale64(right, ndcX*tanHalf*aspect), scale64(up, ndcY*tanHalf)),
		forward,
	)
	d = normalize64(d)
	return vec32(eye), vec32(d)
}

// Object3DRef identifies a scene object by its flat 3D index, layer/object indices, and ID.
type Object3DRef struct {
	Index      int
	LayerIndex int
	// ObjectIndex is the object's index among its siblings: the children of its parent, or its layer's top-level objects.
	ObjectIndex int
	ID          string
	// ParentID is the ID of the object's parent, or empty for a top-level object.
	ParentID string
	Doc      *sceneformat.ObjectDoc
}

// Objects3D returns the objects of every layer, nested children included, depth first in document order with each parent before its children, with sequential flat indices (Pick3D's).
func (e *Editor) Objects3D() []Object3DRef {
	if e == nil || e.Doc == nil {
		return nil
	}
	var out []Object3DRef
	for li := range e.Doc.Layers {
		var walk func(objs []sceneformat.ObjectDoc, parent string)
		walk = func(objs []sceneformat.ObjectDoc, parent string) {
			for oi := range objs {
				o := &objs[oi]
				out = append(out, Object3DRef{
					Index:       len(out),
					LayerIndex:  li,
					ObjectIndex: oi,
					ID:          o.ID,
					ParentID:    parent,
					Doc:         o,
				})
				walk(o.Children, o.ID)
			}
		}
		walk(e.Doc.Layers[li].Objects, "")
	}
	return out
}

// parentWorld3D returns the 3D transform from the local space of the parent of the object id to world space, composed through its ancestors: the identity for a top-level object. While an animation clip is open the ancestors are posed as the clip poses them at the playhead, as the viewport draws them.
func (e *Editor) parentWorld3D(id string) aff3 {
	if e.clipScene != nil {
		if bo := e.clipScene.Object(id); bo != nil {
			var path []*sceneformat.ObjectDoc
			for p := bo.Parent; p != nil; p = p.Parent {
				path = append(path, p.Doc)
			}
			slices.Reverse(path)
			return chain3(path)
		}
	}
	path := objectPath(e.Doc, id)
	if len(path) < 2 {
		return ident3
	}
	return chain3(path[:len(path)-1])
}

// worldPosition3D returns where the origin of o, an object of the scene, sits in world space.
func (e *Editor) worldPosition3D(o *sceneformat.ObjectDoc) [3]float32 {
	return vec32(e.parentWorld3D(o.ID).point([3]float64{float64(o.X), float64(o.Y), float64(o.PosZ)}))
}

func (e *Editor) objectBaseExtents3D(o *sceneformat.ObjectDoc) (w, h, d float32) {
	w = defaultMarkerHalfSize * 2
	h = defaultMarkerHalfSize * 2
	if scene := e.ActiveScene(); scene != nil {
		if bo := scene.Object(o.ID); bo != nil && bo.Sprite != nil && bo.Sprite.Frame.Width > 0 && bo.Sprite.Frame.Height > 0 {
			w = bo.Sprite.Frame.Width
			h = bo.Sprite.Frame.Height
		}
	}
	if o.Depth > 0 {
		d = o.Depth
	} else {
		d = min(w, h)
	}
	return w, h, d
}

// ObjectCorners3D returns the 8 world-space corners of object o's oriented 3D bounding box. A nested child's box is placed by the transforms of its ancestors too.
func (e *Editor) ObjectCorners3D(o *sceneformat.ObjectDoc) [8][3]float32 {
	var corners [8][3]float32
	if o == nil {
		return corners
	}
	bw, bh, bd := e.objectBaseExtents3D(o)
	hx := float64(bw) * 0.5 * math.Abs(float64(o.EffectiveScaleX()))
	hy := float64(bh) * 0.5 * math.Abs(float64(o.EffectiveScaleY()))
	hz := float64(bd) * 0.5 * math.Abs(float64(o.EffectiveScaleZ()))

	signs := [8][3]float64{
		{-1, -1, -1},
		{+1, -1, -1},
		{+1, +1, -1},
		{-1, +1, -1},
		{-1, -1, +1},
		{+1, -1, +1},
		{+1, +1, +1},
		{-1, +1, +1},
	}
	pos := [3]float64{float64(o.X), float64(o.Y), float64(o.PosZ)}
	rx, ry, rz := float64(o.RotX), float64(o.RotY), float64(o.RotZ)
	parent := e.parentWorld3D(o.ID)
	for i, s := range signs {
		local := [3]float64{s[0] * hx, s[1] * hy, s[2] * hz}
		rot := rotateEulerXYZ64(local, rx, ry, rz)
		wpt := add64(pos, rot)
		if parent != ident3 {
			wpt = parent.point(wpt)
		}
		corners[i] = vec32(wpt)
	}
	return corners
}

// ObjectAABB3D returns the world-space axis-aligned bounding box (min, max) enclosing object o's 3D box.
func (e *Editor) ObjectAABB3D(o *sceneformat.ObjectDoc) (boxMin, boxMax [3]float32) {
	if o == nil {
		return boxMin, boxMax
	}
	corners := e.ObjectCorners3D(o)
	boxMin = corners[0]
	boxMax = corners[0]
	for i := 1; i < 8; i++ {
		for axis := 0; axis < 3; axis++ {
			if corners[i][axis] < boxMin[axis] {
				boxMin[axis] = corners[i][axis]
			}
			if corners[i][axis] > boxMax[axis] {
				boxMax[axis] = corners[i][axis]
			}
		}
	}
	// Guarantee a minimal pickable thickness along degenerate axes.
	const minHalf float32 = 0.5
	for axis := 0; axis < 3; axis++ {
		if boxMax[axis]-boxMin[axis] < minHalf*2 {
			mid := (boxMin[axis] + boxMax[axis]) * 0.5
			boxMin[axis] = mid - minHalf
			boxMax[axis] = mid + minHalf
		}
	}
	return boxMin, boxMax
}

// Pick3D casts a perspective ray through viewport pixel (sx, sy) and intersects it against every
// visible scene object's 3D AABB, nested children included, returning the flat index (Objects3D's) of the closest hit object. Objects hidden or locked in the editor (SetObjectHidden, SetObjectLocked), with their descendants, are passed over.
func (e *Editor) Pick3D(sx, sy, w, h float32) (objectIndex int, ok bool) {
	if e == nil || e.Doc == nil || w <= 0 || h <= 0 {
		return -1, false
	}
	orig32, dir32 := e.Camera3D.ScreenRay(sx, sy, w, h)
	orig := vec64(orig32)
	dir := vec64(dir32)

	bestIdx := -1
	bestT := math.Inf(1)
	idx := 0
	for li := range e.Doc.Layers {
		layer := &e.Doc.Layers[li]
		// Screen-space layers hold HUD and menu objects at canvas coordinates,
		// so they are neither drawn as 3D wireframes nor pickable in 3D; the
		// flat index still advances for them to stay in step with Objects3D.
		visible := layer.IsVisible() && layer.EffectiveSpace() != sceneformat.SpaceScreen
		var walk func(objs []sceneformat.ObjectDoc, skip bool)
		walk = func(objs []sceneformat.ObjectDoc, skip bool) {
			for oi := range objs {
				o := &objs[oi]
				// A hidden or locked object takes its descendants with it.
				skipped := skip || e.hidden[o.ID] || e.locked[o.ID]
				if visible && !skipped {
					bmin, bmax := e.ObjectAABB3D(o)
					if t, hit := intersectRayAABB64(orig, dir, vec64(bmin), vec64(bmax)); hit && t < bestT {
						bestT = t
						bestIdx = idx
					}
				}
				idx++
				walk(o.Children, skipped)
			}
		}
		walk(layer.Objects, false)
	}
	if bestIdx < 0 {
		return -1, false
	}
	return bestIdx, true
}

// Pick3DObject returns the ID of the closest visible 3D object hit at viewport pixel (sx, sy).
func (e *Editor) Pick3DObject(sx, sy, w, h float32) (string, bool) {
	idx, ok := e.Pick3D(sx, sy, w, h)
	if !ok {
		return "", false
	}
	refs := e.Objects3D()
	if idx < 0 || idx >= len(refs) {
		return "", false
	}
	return refs[idx].ID, true
}

// Select3DAt picks the closest 3D object under (sx, sy) and selects it, or clears selection on empty space.
func (e *Editor) Select3DAt(sx, sy, w, h float32) bool {
	if e == nil {
		return false
	}
	if id, ok := e.Pick3DObject(sx, sy, w, h); ok {
		e.SelectObject(id)
		return true
	}
	e.SelectObject("")
	return false
}

// GizmoLength3D returns the world-space arm length / ring radius for a 3D gizmo placed at pos.
func (e *Editor) GizmoLength3D(pos [3]float32) float32 {
	if e == nil {
		return 32
	}
	dist := len64(sub64(vec64(pos), e.Camera3D.eye64()))
	l := float32(dist * 0.18)
	if l < 2 {
		l = 2
	}
	return l
}

// PickGizmo3D hit-tests the active 3D gizmo handles on the currently selected object in screen space.
func (e *Editor) PickGizmo3D(sx, sy, w, h float32) GizmoAxis3D {
	if e == nil || e.playing || w <= 0 || h <= 0 {
		return Axis3DNone
	}
	obj, _ := e.SelectedObject()
	if obj == nil {
		return Axis3DNone
	}
	origin := e.worldPosition3D(obj)
	gizmoLen := e.GizmoLength3D(origin)
	axes := [3]GizmoAxis3D{Axis3DX, Axis3DY, Axis3DZ}

	bestAxis := Axis3DNone
	bestDist := float64(gizmoHitRadius)

	switch e.GizmoMode {
	case Gizmo3DRotate:
		for _, axis := range axes {
			d := e.screenDistToGizmoRing(origin, gizmoLen, axis, sx, sy, w, h)
			if d < bestDist {
				bestDist = d
				bestAxis = axis
			}
		}
	default: // Translate and Scale share 3 axial handles.
		for _, axis := range axes {
			dir := axisVector32(axis)
			startPt := [3]float32{
				origin[0] + dir[0]*gizmoLen*0.08,
				origin[1] + dir[1]*gizmoLen*0.08,
				origin[2] + dir[2]*gizmoLen*0.08,
			}
			endPt := [3]float32{
				origin[0] + dir[0]*gizmoLen*1.12,
				origin[1] + dir[1]*gizmoLen*1.12,
				origin[2] + dir[2]*gizmoLen*1.12,
			}
			x0, y0, x1, y1, vis := e.Camera3D.projectSegment(startPt, endPt, w, h)
			if !vis {
				continue
			}
			d := pointToSegmentDist64(float64(sx), float64(sy), float64(x0), float64(y0), float64(x1), float64(y1))
			if d < bestDist {
				bestDist = d
				bestAxis = axis
			}
		}
	}
	return bestAxis
}

func (e *Editor) screenDistToGizmoRing(origin [3]float32, radius float32, axis GizmoAxis3D, sx, sy, w, h float32) float64 {
	u, v := ringBasis64(axis)
	orig := vec64(origin)
	r := float64(radius)
	best := math.Inf(1)

	ringPoint := func(i int) [3]float32 {
		a := (2 * math.Pi * float64(i)) / float64(gizmoRingSteps)
		ca, sa := math.Cos(a)*r, math.Sin(a)*r
		p := add64(orig, add64(scale64(u, ca), scale64(v, sa)))
		return vec32(p)
	}

	prev := ringPoint(0)
	for i := 1; i <= gizmoRingSteps; i++ {
		cur := ringPoint(i % gizmoRingSteps)
		if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(prev, cur, w, h); vis {
			d := pointToSegmentDist64(float64(sx), float64(sy), float64(x0), float64(y0), float64(x1), float64(y1))
			if d < best {
				best = d
			}
		}
		prev = cur
	}
	return best
}

type drag3DKind int

const (
	drag3DNone drag3DKind = iota
	drag3DGizmo
	drag3DOrbit
	drag3DPan
)

type drag3DState struct {
	kind           drag3DKind
	objectID       string
	mode           GizmoMode3D
	axis           GizmoAxis3D
	startSX        float32
	startSY        float32
	lastSX         float32
	lastSY         float32
	vpW            float32
	vpH            float32
	gizmoLen       float32
	startTransform Transform3D
	// startPos is where the object's origin sat in world space as the drag began, and parentInv maps world space to its parent's space, the identity for a top-level object.
	startPos    [3]float64
	parentInv   aff3
	startAxisT  float64
	hasAxisT    bool
	prevRingVec [3]float64
	hasRingVec  bool
	accumAngle  float64
}

// ActiveGizmoAxis3D returns the currently dragged 3D gizmo axis, or Axis3DNone.
func (e *Editor) ActiveGizmoAxis3D() GizmoAxis3D {
	if e == nil || e.drag3D.kind != drag3DGizmo {
		return Axis3DNone
	}
	return e.drag3D.axis
}

// DraggingGizmo3D reports whether a 3D gizmo drag is currently in progress.
func (e *Editor) DraggingGizmo3D() bool {
	return e != nil && e.drag3D.kind == drag3DGizmo
}

// BeginGizmoDrag3D attempts to pick a 3D gizmo handle on the selected object at (sx, sy) and start dragging it.
// Returns the picked GizmoAxis3D (Axis3DNone if no handle was hit).
func (e *Editor) BeginGizmoDrag3D(sx, sy, w, h float32) GizmoAxis3D {
	if e == nil || e.playing {
		return Axis3DNone
	}
	axis := e.PickGizmo3D(sx, sy, w, h)
	if axis == Axis3DNone {
		return Axis3DNone
	}
	if !e.BeginGizmoDragAxis3D(axis, sx, sy, w, h) {
		return Axis3DNone
	}
	return axis
}

// BeginGizmoDragAxis3D begins a 3D gizmo drag constrained to axis starting from viewport pixel (sx, sy).
func (e *Editor) BeginGizmoDragAxis3D(axis GizmoAxis3D, sx, sy, w, h float32) bool {
	if e == nil || e.playing || axis == Axis3DNone || w <= 0 || h <= 0 {
		return false
	}
	obj, _ := e.SelectedObject()
	if obj == nil {
		return false
	}
	parentInv, ok := e.parentWorld3D(obj.ID).invert()
	if !ok {
		// A parent scaled to zero leaves no space to drag the object in.
		return false
	}
	pos := e.worldPosition3D(obj)
	gizmoLen := e.GizmoLength3D(pos)
	st := drag3DState{
		kind:           drag3DGizmo,
		objectID:       obj.ID,
		mode:           e.GizmoMode,
		axis:           axis,
		startSX:        sx,
		startSY:        sy,
		lastSX:         sx,
		lastSY:         sy,
		vpW:            w,
		vpH:            h,
		gizmoLen:       gizmoLen,
		startTransform: captureTransform3D(obj),
		startPos:       vec64(pos),
		parentInv:      parentInv,
	}

	orig32, dir32 := e.Camera3D.ScreenRay(sx, sy, w, h)
	orig := vec64(orig32)
	dir := vec64(dir32)
	axisDir := axisVector64(axis)
	pos64 := vec64(pos)

	switch e.GizmoMode {
	case Gizmo3DRotate:
		if hit, ok := intersectRayPlane64(orig, dir, pos64, axisDir); ok {
			rv := sub64(hit, pos64)
			if len64(rv) > 1e-4 {
				st.prevRingVec = normalize64(rv)
				st.hasRingVec = true
			}
		}
	default:
		if t, ok := closestParamOnAxisLine64(pos64, axisDir, orig, dir); ok {
			st.startAxisT = t
			st.hasAxisT = true
		}
	}

	e.drag3D = st
	e.notify()
	return true
}

// UpdateGizmoDrag3D updates the selected object's 3D transform live during an active gizmo drag.
func (e *Editor) UpdateGizmoDrag3D(sx, sy float32) {
	if e == nil || e.playing || e.drag3D.kind != drag3DGizmo {
		return
	}
	obj, _, _ := e.FindObject(e.drag3D.objectID)
	if obj == nil {
		e.drag3D = drag3DState{}
		return
	}

	st := &e.drag3D
	w, h := st.vpW, st.vpH
	startPos := st.startPos
	axisDir := axisVector64(st.axis)
	orig32, dir32 := e.Camera3D.ScreenRay(sx, sy, w, h)
	orig := vec64(orig32)
	dir := vec64(dir32)

	switch st.mode {
	case Gizmo3DTranslate:
		delta := e.computeAxisDelta(st, startPos, axisDir, orig, dir, sx, sy)
		// The drag runs along a world axis; a nested child's position is in its parent's space.
		local := st.parentInv.vector(scale64(axisDir, delta))
		obj.X = st.startTransform.X + float32(local[0])
		obj.Y = st.startTransform.Y + float32(local[1])
		obj.PosZ = st.startTransform.PosZ + float32(local[2])

	case Gizmo3DRotate:
		angle := e.computeRotationAngle(st, startPos, axisDir, orig, dir, sx, sy)
		obj.RotX = st.startTransform.RotX + float32(axisDir[0]*angle)
		obj.RotY = st.startTransform.RotY + float32(axisDir[1]*angle)
		obj.RotZ = st.startTransform.RotZ + float32(axisDir[2]*angle)

	case Gizmo3DScale:
		delta := e.computeAxisDelta(st, startPos, axisDir, orig, dir, sx, sy)
		denom := float64(st.gizmoLen)
		if denom <= 1e-4 {
			denom = 1
		}
		factor := 1.0 + delta/denom
		if !math.IsNaN(factor) && !math.IsInf(factor, 0) {
			switch st.axis {
			case Axis3DX:
				base := float64(1)
				if st.startTransform.ScaleX != nil {
					base = float64(*st.startTransform.ScaleX)
				}
				if base == 0 {
					// A zero scale would stay zero under a multiplicative drag,
					// so start the drag from a unit scale instead.
					base = 1
				}
				obj.ScaleX = sceneformat.Float32(float32(base * factor))
			case Axis3DY:
				base := float64(1)
				if st.startTransform.ScaleY != nil {
					base = float64(*st.startTransform.ScaleY)
				}
				if base == 0 {
					// A zero scale would stay zero under a multiplicative drag,
					// so start the drag from a unit scale instead.
					base = 1
				}
				obj.ScaleY = sceneformat.Float32(float32(base * factor))
			case Axis3DZ:
				base := float64(1)
				if st.startTransform.ScaleZ != nil {
					base = float64(*st.startTransform.ScaleZ)
				}
				if base == 0 {
					// A zero scale would stay zero under a multiplicative drag,
					// so start the drag from a unit scale instead.
					base = 1
				}
				obj.ScaleZ = sceneformat.Float32(float32(base * factor))
			}
		}
	}

	st.lastSX = sx
	st.lastSY = sy
	e.Rebuild()
	e.notify()
}

func (e *Editor) computeAxisDelta(st *drag3DState, startPos, axisDir, rayOrig, rayDir [3]float64, sx, sy float32) float64 {
	if st.hasAxisT {
		if tCur, ok := closestParamOnAxisLine64(startPos, axisDir, rayOrig, rayDir); ok {
			return tCur - st.startAxisT
		}
	}
	// Fallback when axis is nearly collinear with the view ray: project screen delta onto projected axis.
	p0 := vec32(startPos)
	p1 := vec32(add64(startPos, scale64(axisDir, float64(st.gizmoLen))))
	x0, y0, x1, y1, vis := e.Camera3D.projectSegment(p0, p1, st.vpW, st.vpH)
	if !vis {
		return 0
	}
	ax, ay := float64(x1-x0), float64(y1-y0)
	pixLen := math.Hypot(ax, ay)
	if pixLen < 1e-3 {
		return 0
	}
	dsx, dsy := float64(sx-st.startSX), float64(sy-st.startSY)
	projPx := (dsx*ax + dsy*ay) / pixLen
	return projPx * (float64(st.gizmoLen) / pixLen)
}

func (e *Editor) computeRotationAngle(st *drag3DState, startPos, axisDir, rayOrig, rayDir [3]float64, sx, sy float32) float64 {
	if st.hasRingVec {
		if hit, ok := intersectRayPlane64(rayOrig, rayDir, startPos, axisDir); ok {
			rv := sub64(hit, startPos)
			if len64(rv) > 1e-4 {
				cur := normalize64(rv)
				sinA := dot64(cross64(st.prevRingVec, cur), axisDir)
				cosA := dot64(st.prevRingVec, cur)
				step := math.Atan2(sinA, cosA)
				st.accumAngle += step
				st.prevRingVec = cur
				return st.accumAngle
			}
		}
	}
	// Edge-on fallback: map horizontal screen delta to radians.
	return float64(sx-st.startSX) * 0.01
}

// EndGizmoDrag3D finishes an active 3D gizmo drag and commits a single undoable command to History.
// Returns true if a transform change was committed. A drag that would shear a descendant's sprite is dropped, restoring the pre-drag transform.
func (e *Editor) EndGizmoDrag3D() bool {
	if e == nil || e.drag3D.kind != drag3DGizmo {
		return false
	}
	st := e.drag3D
	e.drag3D = drag3DState{}

	obj, _, _ := e.FindObject(st.objectID)
	if obj == nil {
		e.notify()
		return false
	}
	after := captureTransform3D(obj)
	if equalTransform3D(st.startTransform, after) {
		e.notify()
		return false
	}
	// Restore pre-drag state before executing through History so Undo/Redo snapshots are exact.
	applyTransform3D(obj, st.startTransform)
	// A scale that would shear a descendant's sprite leaves a scene that cannot build, so the drag is dropped instead of committed.
	changed := *obj
	applyTransform3D(&changed, after)
	if sceneformat.CheckObjectTree(&changed) != nil {
		e.Rebuild()
		e.notify()
		return false
	}
	e.History.Execute(&transform3DCmd{
		ed:     e,
		id:     st.objectID,
		label:  st.mode.String() + " 3D " + st.objectID + " (" + st.axis.String() + ")",
		before: st.startTransform,
		after:  after,
	})
	return true
}

// CancelGizmoDrag3D aborts any active 3D gizmo drag and restores the object's pre-drag transform.
func (e *Editor) CancelGizmoDrag3D() {
	if e == nil || e.drag3D.kind != drag3DGizmo {
		return
	}
	st := e.drag3D
	e.drag3D = drag3DState{}
	if obj, _, _ := e.FindObject(st.objectID); obj != nil {
		applyTransform3D(obj, st.startTransform)
		e.Rebuild()
	}
	e.notify()
}

// HandleViewport3DPress handles a click/tap in the 3D viewport: picks the closest 3D object or clears selection.
func (e *Editor) HandleViewport3DPress(sx, sy, w, h float32) {
	if e == nil || e.playing {
		return
	}
	if e.PickGizmo3D(sx, sy, w, h) != Axis3DNone {
		return
	}
	e.Select3DAt(sx, sy, w, h)
}

// BeginViewport3DDrag starts either a 3D gizmo drag (if a handle is under the cursor and !pan)
// or a camera orbit/pan drag.
func (e *Editor) BeginViewport3DDrag(sx, sy, w, h float32, pan bool) {
	if e == nil {
		return
	}
	if !pan && !e.playing {
		if axis := e.BeginGizmoDrag3D(sx, sy, w, h); axis != Axis3DNone {
			return
		}
	}
	kind := drag3DOrbit
	if pan {
		kind = drag3DPan
	}
	e.drag3D = drag3DState{
		kind:    kind,
		startSX: sx,
		startSY: sy,
		lastSX:  sx,
		lastSY:  sy,
		vpW:     w,
		vpH:     h,
	}
}

// UpdateViewport3DDrag advances the active 3D gizmo drag or camera orbit/pan drag.
func (e *Editor) UpdateViewport3DDrag(sx, sy float32) {
	if e == nil {
		return
	}
	switch e.drag3D.kind {
	case drag3DGizmo:
		e.UpdateGizmoDrag3D(sx, sy)
	case drag3DOrbit:
		dx := sx - e.drag3D.lastSX
		dy := sy - e.drag3D.lastSY
		e.drag3D.lastSX = sx
		e.drag3D.lastSY = sy
		e.Camera3D.Orbit(-dx*0.01, dy*0.01)
		e.notify()
	case drag3DPan:
		dx := sx - e.drag3D.lastSX
		dy := sy - e.drag3D.lastSY
		e.drag3D.lastSX = sx
		e.drag3D.lastSY = sy
		dist, fov, _, _ := e.Camera3D.effectiveParams()
		h := float64(e.drag3D.vpH)
		if h <= 0 {
			h = 360
		}
		worldPerPx := float32((2 * dist * math.Tan(fov*0.5)) / h)
		e.Camera3D.Pan(-dx*worldPerPx, dy*worldPerPx)
		e.notify()
	}
}

// EndViewport3DDrag finishes any active 3D gizmo or camera drag.
func (e *Editor) EndViewport3DDrag() {
	if e == nil {
		return
	}
	if e.drag3D.kind == drag3DGizmo {
		e.EndGizmoDrag3D()
		return
	}
	e.drag3D = drag3DState{}
}

// HandleViewport3DWheel zooms the 3D orbit camera proportionally to deltaY.
func (e *Editor) HandleViewport3DWheel(deltaY float32) {
	if e == nil || deltaY == 0 {
		return
	}
	dist, _, _, _ := e.Camera3D.effectiveParams()
	step := -deltaY * 0.002 * float32(dist)
	e.Camera3D.Zoom(step)
	e.notify()
}

// DrawViewport3D renders the 3D scene view onto d: background, XZ ground grid, world origin axes,
// depth-sorted 3D wireframe bounding boxes for every visible scene object, and the active 3D gizmo.
func (e *Editor) DrawViewport3D(d *core.Drawing, size core.Size) {
	if e == nil || d == nil {
		return
	}
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 {
		w, h = e.ViewportSize()
	}

	// 1. Background fill.
	bg := core.RGBA(0x141824, 1)
	if e.Doc != nil && e.Doc.BackgroundColor != "" {
		if parsed, err := core.ParseColor(e.Doc.BackgroundColor); err == nil {
			bg = parsed
		}
	}
	d.Save().SetFillColor(bg).FillRect(0, 0, w, h).Restore()

	// 2. 3D ground grid on XZ plane (Y = 0).
	e.drawGrid3D(d, w, h)

	// 3. World origin axes (X red, Y green, Z blue).
	e.drawOriginAxes3D(d, w, h)

	// 4. Projected 3D wireframe bounding boxes for all visible scene objects.
	e.drawSceneObjects3D(d, w, h)

	// 5. Active 3D transform gizmo on the selected object.
	if !e.playing {
		e.drawGizmo3D(d, w, h)
	}
}

func (e *Editor) gridStep3D() (step float32, halfLines int) {
	dist, _, _, _ := e.Camera3D.effectiveParams()
	raw := dist / 10.0
	pow := math.Pow(2, math.Floor(math.Log2(math.Max(raw, 1))))
	step = float32(pow)
	if step < 1 {
		step = 1
	}
	return step, 8
}

func (e *Editor) drawGrid3D(d *core.Drawing, w, h float32) {
	step, half := e.gridStep3D()
	extent := step * float32(half)

	d.Save().SetStrokeColor(core.RGBA(0x94A3B8, 0.22)).SetLineWidth(1).BeginPath()
	hasSegments := false
	for i := -half; i <= half; i++ {
		coord := float32(i) * step
		if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(
			[3]float32{coord, 0, -extent},
			[3]float32{coord, 0, extent},
			w, h,
		); vis {
			d.MoveTo(x0, y0).LineTo(x1, y1)
			hasSegments = true
		}
		if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(
			[3]float32{-extent, 0, coord},
			[3]float32{extent, 0, coord},
			w, h,
		); vis {
			d.MoveTo(x0, y0).LineTo(x1, y1)
			hasSegments = true
		}
	}
	if hasSegments {
		d.Stroke()
	}
	d.Restore()
}

func (e *Editor) drawOriginAxes3D(d *core.Drawing, w, h float32) {
	step, _ := e.gridStep3D()
	axisLen := step * 3
	origin := [3]float32{0, 0, 0}

	axes := []struct {
		end   [3]float32
		color core.Color
	}{
		{[3]float32{axisLen, 0, 0}, core.RGBA(0xEF4444, 0.9)},
		{[3]float32{0, axisLen, 0}, core.RGBA(0x22C55E, 0.9)},
		{[3]float32{0, 0, axisLen}, core.RGBA(0x3B82F6, 0.9)},
	}
	for _, ax := range axes {
		x0, y0, x1, y1, vis := e.Camera3D.projectSegment(origin, ax.end, w, h)
		if !vis {
			continue
		}
		d.Save().SetStrokeColor(ax.color).SetLineWidth(2).BeginPath().MoveTo(x0, y0).LineTo(x1, y1).Stroke().Restore()
	}
}

var boxEdges3D = [12][2]int{
	{0, 1}, {1, 2}, {2, 3}, {3, 0}, // back face
	{4, 5}, {5, 6}, {6, 7}, {7, 4}, // front face
	{0, 4}, {1, 5}, {2, 6}, {3, 7}, // connecting edges
}

func (e *Editor) drawSceneObjects3D(d *core.Drawing, w, h float32) {
	if e.Doc == nil {
		return
	}
	type drawable3D struct {
		obj      *sceneformat.ObjectDoc
		depth    float32
		selected bool
	}
	eye := e.Camera3D.eye64()
	_, _, forward := e.Camera3D.basis64()

	var items []drawable3D
	for li := range e.Doc.Layers {
		layer := &e.Doc.Layers[li]
		if !layer.IsVisible() || layer.EffectiveSpace() == sceneformat.SpaceScreen {
			continue
		}
		var walk func(objs []sceneformat.ObjectDoc)
		walk = func(objs []sceneformat.ObjectDoc) {
			for oi := range objs {
				o := &objs[oi]
				// A hidden object is not drawn, nor are its descendants.
				if e.hidden[o.ID] {
					continue
				}
				children := o.Children
				// While an animation clip is open, draw each object as the clip poses it at the playhead, as the 2D viewport does through ActiveScene.
				if e.clipScene != nil {
					if bo := e.clipScene.Object(o.ID); bo != nil && bo.Doc != nil {
						o = bo.Doc
					}
				}
				rel := sub64(vec64(e.worldPosition3D(o)), eye)
				depth := float32(dot64(rel, forward))
				items = append(items, drawable3D{
					obj:      o,
					depth:    depth,
					selected: o.ID == e.SelectedID,
				})
				walk(children)
			}
		}
		walk(layer.Objects)
	}

	// Sort back-to-front so nearer objects and the selected object draw last.
	slices.SortStableFunc(items, func(a, b drawable3D) int {
		if a.selected != b.selected {
			if a.selected {
				return 1
			}
			return -1
		}
		return cmp.Compare(b.depth, a.depth)
	})

	for _, item := range items {
		corners := e.ObjectCorners3D(item.obj)
		color := core.RGBA(0xCBD5E1, 0.8)
		lineWidth := float32(1.25)
		if item.selected {
			color = core.RGBA(0x38BDF8, 1)
			lineWidth = 2.0
		}

		d.Save().SetStrokeColor(color).SetLineWidth(lineWidth).BeginPath()
		hasEdge := false
		for _, edge := range boxEdges3D {
			x0, y0, x1, y1, vis := e.Camera3D.projectSegment(corners[edge[0]], corners[edge[1]], w, h)
			if !vis {
				continue
			}
			d.MoveTo(x0, y0).LineTo(x1, y1)
			hasEdge = true
		}
		if hasEdge {
			d.Stroke()
		}
		d.Restore()

		if item.selected {
			if cx, cy, _, vis := e.Camera3D.Project(e.worldPosition3D(item.obj), w, h); vis {
				d.Save().SetFillColor(core.RGBA(0x38BDF8, 1)).BeginPath().Arc(cx, cy, 3, 0, float32(2*math.Pi), false).Fill().Restore()
			}
		}
	}
}

func (e *Editor) drawGizmo3D(d *core.Drawing, w, h float32) {
	obj, _ := e.SelectedObject()
	if obj == nil || pathFlagged(e.hidden, objectPath(e.Doc, obj.ID)) {
		return
	}
	origin := e.worldPosition3D(obj)
	gizmoLen := e.GizmoLength3D(origin)
	activeAxis := e.ActiveGizmoAxis3D()

	axes := []struct {
		axis  GizmoAxis3D
		color core.Color
	}{
		{Axis3DX, core.RGBA(0xEF4444, 1)},
		{Axis3DY, core.RGBA(0x22C55E, 1)},
		{Axis3DZ, core.RGBA(0x3B82F6, 1)},
	}

	switch e.GizmoMode {
	case Gizmo3DRotate:
		for _, ax := range axes {
			col := ax.color
			lw := float32(2.0)
			if ax.axis == activeAxis {
				col = core.RGBA(0xFACC15, 1)
				lw = 3.0
			}
			e.drawGizmoRing3D(d, origin, gizmoLen, ax.axis, col, lw, w, h)
		}

	default: // Translate (arrows) or Scale (cube caps)
		for _, ax := range axes {
			col := ax.color
			lw := float32(2.5)
			if ax.axis == activeAxis {
				col = core.RGBA(0xFACC15, 1)
				lw = 3.5
			}
			dir := axisVector32(ax.axis)
			endPt := [3]float32{
				origin[0] + dir[0]*gizmoLen,
				origin[1] + dir[1]*gizmoLen,
				origin[2] + dir[2]*gizmoLen,
			}
			x0, y0, x1, y1, vis := e.Camera3D.projectSegment(origin, endPt, w, h)
			if !vis {
				continue
			}
			d.Save().SetStrokeColor(col).SetLineWidth(lw).BeginPath().MoveTo(x0, y0).LineTo(x1, y1).Stroke().Restore()

			if e.GizmoMode == Gizmo3DScale {
				d.Save().SetFillColor(col).FillRect(x1-4, y1-4, 8, 8).Restore()
			} else {
				drawArrowHead2D(d, x0, y0, x1, y1, col)
			}
		}
	}
}

func (e *Editor) drawGizmoRing3D(d *core.Drawing, origin [3]float32, radius float32, axis GizmoAxis3D, color core.Color, lw, w, h float32) {
	u, v := ringBasis64(axis)
	orig := vec64(origin)
	r := float64(radius)

	ringPoint := func(i int) [3]float32 {
		a := (2 * math.Pi * float64(i)) / float64(gizmoRingSteps)
		ca, sa := math.Cos(a)*r, math.Sin(a)*r
		return vec32(add64(orig, add64(scale64(u, ca), scale64(v, sa))))
	}

	d.Save().SetStrokeColor(color).SetLineWidth(lw).BeginPath()
	hasSeg := false
	prev := ringPoint(0)
	for i := 1; i <= gizmoRingSteps; i++ {
		cur := ringPoint(i % gizmoRingSteps)
		if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(prev, cur, w, h); vis {
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

func drawArrowHead2D(d *core.Drawing, x0, y0, x1, y1 float32, color core.Color) {
	dx, dy := float64(x1-x0), float64(y1-y0)
	length := math.Hypot(dx, dy)
	if length < 1e-3 {
		return
	}
	ux, uy := float32(dx/length), float32(dy/length)
	px, py := -uy, ux
	const headLen float32 = 9
	const headHalfW float32 = 4

	tipX, tipY := x1+ux*3, y1+uy*3
	baseX, baseY := tipX-ux*headLen, tipY-uy*headLen
	leftX, leftY := baseX+px*headHalfW, baseY+py*headHalfW
	rightX, rightY := baseX-px*headHalfW, baseY-py*headHalfW

	d.Save().SetFillColor(color).BeginPath().
		MoveTo(tipX, tipY).
		LineTo(leftX, leftY).
		LineTo(rightX, rightY).
		ClosePath().
		Fill().Restore()
}

// projectSegment clips a 3D world segment against the camera's near plane before projecting to 2D.
func (c OrbitCamera3D) projectSegment(a, b [3]float32, w, h float32) (x0, y0, x1, y1 float32, visible bool) {
	if w <= 0 || h <= 0 {
		return 0, 0, 0, 0, false
	}
	_, fov, near, far := c.effectiveParams()
	eye := c.eye64()
	right, up, forward := c.basis64()

	ra := sub64(vec64(a), eye)
	rb := sub64(vec64(b), eye)
	ca := [3]float64{dot64(ra, right), dot64(ra, up), dot64(ra, forward)}
	cb := [3]float64{dot64(rb, right), dot64(rb, up), dot64(rb, forward)}

	clipNear := near + 1e-3
	if (ca[2] <= clipNear && cb[2] <= clipNear) || (ca[2] >= far && cb[2] >= far) {
		return 0, 0, 0, 0, false
	}
	if ca[2] < clipNear {
		t := (clipNear - ca[2]) / (cb[2] - ca[2])
		ca = lerp64(ca, cb, t)
	} else if cb[2] < clipNear {
		t := (clipNear - cb[2]) / (ca[2] - cb[2])
		cb = lerp64(cb, ca, t)
	}

	tanHalf := math.Tan(fov * 0.5)
	aspect := float64(w) / float64(h)

	proj := func(p [3]float64) (float32, float32) {
		ndcX := p[0] / (p[2] * tanHalf * aspect)
		ndcY := p[1] / (p[2] * tanHalf)
		return float32((ndcX + 1) * 0.5 * float64(w)), float32((1 - ndcY) * 0.5 * float64(h))
	}
	x0, y0 = proj(ca)
	x1, y1 = proj(cb)
	return x0, y0, x1, y1, true
}

// --- Pure-Go 3D math helpers ---

func axisVector32(axis GizmoAxis3D) [3]float32 {
	switch axis {
	case Axis3DX:
		return [3]float32{1, 0, 0}
	case Axis3DY:
		return [3]float32{0, 1, 0}
	case Axis3DZ:
		return [3]float32{0, 0, 1}
	default:
		return [3]float32{0, 0, 0}
	}
}

func axisVector64(axis GizmoAxis3D) [3]float64 {
	v := axisVector32(axis)
	return vec64(v)
}

func ringBasis64(axis GizmoAxis3D) (u, v [3]float64) {
	switch axis {
	case Axis3DX:
		return [3]float64{0, 1, 0}, [3]float64{0, 0, 1}
	case Axis3DY:
		return [3]float64{1, 0, 0}, [3]float64{0, 0, -1}
	case Axis3DZ:
		return [3]float64{1, 0, 0}, [3]float64{0, 1, 0}
	default:
		return [3]float64{1, 0, 0}, [3]float64{0, 1, 0}
	}
}

func vec64(v [3]float32) [3]float64 {
	return [3]float64{float64(v[0]), float64(v[1]), float64(v[2])}
}

func vec32(v [3]float64) [3]float32 {
	return [3]float32{float32(v[0]), float32(v[1]), float32(v[2])}
}

func add64(a, b [3]float64) [3]float64 {
	return [3]float64{a[0] + b[0], a[1] + b[1], a[2] + b[2]}
}

func sub64(a, b [3]float64) [3]float64 {
	return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

func scale64(a [3]float64, s float64) [3]float64 {
	return [3]float64{a[0] * s, a[1] * s, a[2] * s}
}

func lerp64(a, b [3]float64, t float64) [3]float64 {
	return [3]float64{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
	}
}

func dot64(a, b [3]float64) float64 {
	return a[0]*b[0] + a[1]*b[1] + a[2]*b[2]
}

func cross64(a, b [3]float64) [3]float64 {
	return [3]float64{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func len64(a [3]float64) float64 {
	return math.Sqrt(dot64(a, a))
}

func normalize64(a [3]float64) [3]float64 {
	l := len64(a)
	if l < 1e-12 {
		return [3]float64{}
	}
	return scale64(a, 1/l)
}

func clamp64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func rotateEulerXYZ64(p [3]float64, rx, ry, rz float64) [3]float64 {
	x, y, z := p[0], p[1], p[2]
	if rx != 0 {
		s, c := math.Sincos(rx)
		y, z = y*c-z*s, y*s+z*c
	}
	if ry != 0 {
		s, c := math.Sincos(ry)
		x, z = x*c+z*s, -x*s+z*c
	}
	if rz != 0 {
		s, c := math.Sincos(rz)
		x, y = x*c-y*s, x*s+y*c
	}
	return [3]float64{x, y, z}
}

func intersectRayAABB64(orig, dir, boxMin, boxMax [3]float64) (float64, bool) {
	tMin := 0.0
	tMax := math.Inf(1)
	for axis := 0; axis < 3; axis++ {
		if math.Abs(dir[axis]) < 1e-9 {
			if orig[axis] < boxMin[axis] || orig[axis] > boxMax[axis] {
				return 0, false
			}
			continue
		}
		invD := 1.0 / dir[axis]
		t0 := (boxMin[axis] - orig[axis]) * invD
		t1 := (boxMax[axis] - orig[axis]) * invD
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		if t0 > tMin {
			tMin = t0
		}
		if t1 < tMax {
			tMax = t1
		}
		if tMax < tMin {
			return 0, false
		}
	}
	return tMin, true
}

func closestParamOnAxisLine64(lineOrigin, lineDir, rayOrig, rayDir [3]float64) (float64, bool) {
	w0 := sub64(lineOrigin, rayOrig)
	a := dot64(lineDir, lineDir)
	b := dot64(lineDir, rayDir)
	c := dot64(rayDir, rayDir)
	d := dot64(lineDir, w0)
	e := dot64(rayDir, w0)
	denom := a*c - b*b
	if math.Abs(denom) < 1e-4 {
		return 0, false
	}
	return (b*e - c*d) / denom, true
}

func intersectRayPlane64(rayOrig, rayDir, planePoint, planeNormal [3]float64) ([3]float64, bool) {
	denom := dot64(rayDir, planeNormal)
	if math.Abs(denom) < 0.12 {
		return [3]float64{}, false
	}
	s := dot64(sub64(planePoint, rayOrig), planeNormal) / denom
	if s <= 0 {
		return [3]float64{}, false
	}
	return add64(rayOrig, scale64(rayDir, s)), true
}

func pointToSegmentDist64(px, py, x0, y0, x1, y1 float64) float64 {
	dx, dy := x1-x0, y1-y0
	l2 := dx*dx + dy*dy
	if l2 < 1e-9 {
		return math.Hypot(px-x0, py-y0)
	}
	t := ((px-x0)*dx + (py-y0)*dy) / l2
	t = clamp64(t, 0, 1)
	projX := x0 + t*dx
	projY := y0 + t*dy
	return math.Hypot(px-projX, py-projY)
}
