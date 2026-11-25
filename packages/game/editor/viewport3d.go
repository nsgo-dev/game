package editor

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// maxOrbitPitch is how far Orbit tilts the camera above or below the horizon, short of straight down or up so a drag never flips the view; the axis views (SetView) look straight down or up.
	maxOrbitPitch    float32 = float32(math.Pi/2) - 0.01
	minOrbitDistance float32 = 0.5
	maxOrbitDistance float32 = 50000
	gizmoHitRadius   float32 = 12
	gizmoRingSteps           = 32
)

// OrbitCamera3D is a pure-Go right-handed (Y-up) orbit camera for the 3D scene view: it looks at Target from Distance away, turned by Yaw about the vertical axis and tilted by Pitch, through a perspective projection or, with Orthographic, a parallel one.
type OrbitCamera3D struct {
	Target   [3]float32
	Yaw      float32
	Pitch    float32
	Distance float32
	FOV      float32
	Near     float32
	Far      float32
	// Orthographic projects the view in parallel instead of in perspective. The view is OrthoHeight high, the height the perspective view shows at Target, so switching keeps what lies at Target the same size; Distance still zooms it, and the eye moves back along the view so the clip planes take in the scene in front of Target and behind it.
	Orthographic bool
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

// OrthoHeight returns the world height the view shows at Target: the whole height of the orthographic view, and the height of the perspective view's slice through Target.
func (c OrbitCamera3D) OrthoHeight() float32 {
	return float32(c.halfHeight64() * 2)
}

func (c OrbitCamera3D) halfHeight64() float64 {
	dist, fov, _, _ := c.effectiveParams()
	return dist * math.Tan(fov*0.5)
}

// eyeDistance64 returns how far the eye sits from Target: Distance in perspective, and in orthographic halfway between the near and far planes whatever Distance is, so Target stays inside the clip range however far the view zooms out; in orthographic Distance only sets how much the view shows (OrthoHeight).
func (c OrbitCamera3D) eyeDistance64() float64 {
	dist, _, near, far := c.effectiveParams()
	if c.Orthographic {
		return (near + far) * 0.5
	}
	return dist
}

// pitch64 returns Pitch limited to straight up or down.
func (c OrbitCamera3D) pitch64() float64 {
	p := float64(c.Pitch)
	if math.IsNaN(p) {
		return 0
	}
	return clamp64(p, -math.Pi/2, math.Pi/2)
}

// Eye returns the camera's world-space position derived from Target, Yaw, Pitch and Distance, moved back along the view in orthographic (Orthographic).
func (c OrbitCamera3D) Eye() [3]float32 {
	eye := c.eye64()
	return [3]float32{float32(eye[0]), float32(eye[1]), float32(eye[2])}
}

func (c OrbitCamera3D) eye64() [3]float64 {
	dist := c.eyeDistance64()
	pitch := c.pitch64()
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

// basis64 derives the basis from the angles, not from the eye and Target, so it holds when the view looks straight down or up: right lies in the ground plane, turned by Yaw.
func (c OrbitCamera3D) basis64() (right, up, forward [3]float64) {
	pitch := c.pitch64()
	sinY, cosY := math.Sincos(float64(c.Yaw))
	sinP, cosP := math.Sincos(pitch)
	forward = [3]float64{-cosP * sinY, -sinP, -cosP * cosY}
	right = [3]float64{cosY, 0, -sinY}
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
	c.clampDistance()
}

// ZoomBy divides Distance by factor, so a factor of 2 shows the view twice as large, clamped as Zoom clamps it. A factor that is not positive and finite changes nothing.
func (c *OrbitCamera3D) ZoomBy(factor float32) {
	if c == nil || !(factor > 0) || math.IsInf(float64(factor), 0) {
		return
	}
	if c.Distance <= 0 {
		c.Distance = 240
	}
	c.Distance /= factor
	c.clampDistance()
}

func (c *OrbitCamera3D) clampDistance() {
	if c.Distance < minOrbitDistance || math.IsNaN(float64(c.Distance)) {
		c.Distance = minOrbitDistance
	}
	if c.Distance > maxOrbitDistance {
		c.Distance = maxOrbitDistance
	}
}

// worldPerPixel returns the world distance one pixel of a viewport h pixels high spans at Target.
func (c OrbitCamera3D) worldPerPixel(h float32) float64 {
	hh := float64(h)
	if hh <= 0 {
		hh = 360
	}
	return 2 * c.halfHeight64() / hh
}

// toCamera returns world point p in camera space: along right, up and forward from the eye.
func toCamera64(p, eye, right, up, forward [3]float64) [3]float64 {
	rel := sub64(p, eye)
	return [3]float64{dot64(rel, right), dot64(rel, up), dot64(rel, forward)}
}

// screen64 returns where camera-space point p falls on a viewport w by h pixels, dividing by its depth in perspective.
func (c OrbitCamera3D) screen64(p [3]float64, w, h float32) (sx, sy float32) {
	_, fov, _, _ := c.effectiveParams()
	aspect := float64(w) / float64(h)
	var ndcX, ndcY float64
	if c.Orthographic {
		halfH := c.halfHeight64()
		ndcX = p[0] / (halfH * aspect)
		ndcY = p[1] / halfH
	} else {
		tanHalf := math.Tan(fov * 0.5)
		ndcX = p[0] / (p[2] * tanHalf * aspect)
		ndcY = p[1] / (p[2] * tanHalf)
	}
	return float32((ndcX + 1) * 0.5 * float64(w)), float32((1 - ndcY) * 0.5 * float64(h))
}

// Project transforms a world-space 3D point into viewport pixel coordinates (sx, sy) and camera depth, in perspective or in orthographic (Orthographic).
// visible is true when the point lies strictly between Near and Far in front of the camera.
func (c OrbitCamera3D) Project(world [3]float32, w, h float32) (sx, sy, depth float32, visible bool) {
	if w <= 0 || h <= 0 {
		return 0, 0, 0, false
	}
	_, _, near, far := c.effectiveParams()
	right, up, forward := c.basis64()
	p := toCamera64(vec64(world), c.eye64(), right, up, forward)
	if p[2] <= near || p[2] >= far {
		return 0, 0, float32(p[2]), false
	}
	sx, sy = c.screen64(p, w, h)
	return sx, sy, float32(p[2]), true
}

// ScreenRay returns the world-space ray (origin, unit direction) passing through viewport pixel (sx, sy). In perspective every ray starts at the eye; in orthographic (Orthographic) every ray runs along the view's forward direction from the point of the eye's plane under the pixel.
func (c OrbitCamera3D) ScreenRay(sx, sy, w, h float32) (origin, dir [3]float32) {
	eye := c.eye64()
	right, up, forward := c.basis64()
	if w <= 0 || h <= 0 {
		return vec32(eye), vec32(forward)
	}
	_, fov, _, _ := c.effectiveParams()
	aspect := float64(w) / float64(h)
	ndcX := (float64(sx)/float64(w))*2 - 1
	ndcY := 1 - (float64(sy)/float64(h))*2
	if c.Orthographic {
		halfH := c.halfHeight64()
		o := add64(eye, add64(scale64(right, ndcX*halfH*aspect), scale64(up, ndcY*halfH)))
		return vec32(o), vec32(forward)
	}
	tanHalf := math.Tan(fov * 0.5)
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

// posed3D returns o as the open animation clip poses it at the playhead, or o itself when no clip is open or the clip's scene lacks it.
func (e *Editor) posed3D(o *sceneformat.ObjectDoc) *sceneformat.ObjectDoc {
	if e.clipScene != nil {
		if bo := e.clipScene.Object(o.ID); bo != nil && bo.Doc != nil {
			return bo.Doc
		}
	}
	return o
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

// ObjectCorners3D returns the 8 world-space corners of object o's oriented 3D bounding box: the bounds of the geometry it draws when the scene's 3D build (Scene3D) gives it a mesh or model, and otherwise a box the size of its sprite frame, or of a marker without one, as deep as its depth. A nested child's box is placed by the transforms of its ancestors too.
func (e *Editor) ObjectCorners3D(o *sceneformat.ObjectDoc) [8][3]float32 {
	if o == nil {
		return [8][3]float32{}
	}
	e.scene3D()
	return e.objectCorners3DUnder(o, e.parentWorld3D(o.ID))
}

// boxSigns3D are the corners of a unit box around its center, in the order ObjectCorners3D returns them.
var boxSigns3D = [8][3]float64{
	{-1, -1, -1},
	{+1, -1, -1},
	{+1, +1, -1},
	{-1, +1, -1},
	{-1, -1, +1},
	{+1, -1, +1},
	{+1, +1, +1},
	{-1, +1, +1},
}

// objectCorners3DUnder is ObjectCorners3D for an object whose parent's local space maps to world space by parent, so a walk down the tree composes each parent once instead of looking up every object's ancestors. It reads the 3D build as it stands, which the caller brings up to date (scene3D).
func (e *Editor) objectCorners3DUnder(o *sceneformat.ObjectDoc, parent aff3) [8][3]float32 {
	var corners [8][3]float32
	if b, ok := e.geometryBounds3D(o.ID); ok {
		local := local3(o)
		if parent != ident3 {
			local = parent.mul(local)
		}
		lo, hi := [3]float32{b.Min.X, b.Min.Y, b.Min.Z}, [3]float32{b.Max.X, b.Max.Y, b.Max.Z}
		for i, s := range boxSigns3D {
			var p [3]float64
			for axis := range 3 {
				p[axis] = float64(lo[axis])
				if s[axis] > 0 {
					p[axis] = float64(hi[axis])
				}
			}
			corners[i] = vec32(local.point(p))
		}
		return corners
	}
	bw, bh, bd := e.objectBaseExtents3D(o)
	hx := float64(bw) * 0.5 * math.Abs(float64(o.EffectiveScaleX()))
	hy := float64(bh) * 0.5 * math.Abs(float64(o.EffectiveScaleY()))
	hz := float64(bd) * 0.5 * math.Abs(float64(o.EffectiveScaleZ()))
	pos := [3]float64{float64(o.X), float64(o.Y), float64(o.PosZ)}
	rx, ry, rz := float64(o.RotX), float64(o.RotY), float64(o.RotZ)
	for i, s := range boxSigns3D {
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
	return aabbOfCorners3D(e.ObjectCorners3D(o))
}

// objectAABB3D is ObjectAABB3D reading the 3D build as it stands, which the caller brings up to date once (scene3D) before it measures many objects.
func (e *Editor) objectAABB3D(o *sceneformat.ObjectDoc) (boxMin, boxMax [3]float32) {
	return aabbOfCorners3D(e.objectCorners3DUnder(o, e.parentWorld3D(o.ID)))
}

// aabbOfCorners3D returns the axis-aligned box enclosing corners, with a minimal pickable thickness along degenerate axes.
func aabbOfCorners3D(corners [8][3]float32) (boxMin, boxMax [3]float32) {
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

// Pick3D casts the camera's ray (ScreenRay), in perspective or in orthographic, through viewport pixel (sx, sy) and returns the flat index (Objects3D's) of the closest visible scene object it hits, nested children included. An object that draws a mesh or model in the scene's 3D build (Scene3D) is hit on the triangles of its geometry, through the build's scene3d picking, and any other object on its world-space 3D AABB (ObjectAABB3D); an object an instanced scene adds is picked as its instance. A press that hits geometry picks the geometry's object even when the box of an object without geometry is nearer, since such boxes are a fixed size whatever the scene's scale and may enclose the camera or the whole level; the boxes are picked only where no geometry is hit. Objects hidden or locked in the editor (SetObjectHidden, SetObjectLocked), with their descendants, and the objects of hidden and locked layers (SetLayerVisible, SetLayerLocked) are passed over.
func (e *Editor) Pick3D(sx, sy, w, h float32) (objectIndex int, ok bool) {
	if e == nil || e.Doc == nil || w <= 0 || h <= 0 {
		return -1, false
	}
	if iconIdx, hit := e.pickIcon3D(sx, sy, w, h); hit {
		return iconIdx, true
	}
	vc := e.activeView3DCam(w, h)
	orig32, dir32 := vc.screenRay(sx, sy, w, h)
	orig := vec64(orig32)
	dir := vec64(dir32)
	s3 := e.scene3D()
	meshID, meshHit := e.pickGeometry3D(s3, orig, dir)

	meshIdx, boxIdx := -1, -1
	boxT := math.Inf(1)
	idx := 0
	for li := range e.Doc.Layers {
		layer := &e.Doc.Layers[li]
		// Screen-space layers hold HUD and menu objects at canvas coordinates,
		// so they are neither drawn as 3D wireframes nor pickable in 3D; the
		// flat index still advances for them to stay in step with Objects3D.
		visible := layer.IsVisible() && layer.EffectiveSpace() != sceneformat.SpaceScreen && !e.layerLocked[layer.Name]
		// Each level composes its parent's world transform once and hands it down, so picking stays linear in the number of objects.
		var walk func(objs []sceneformat.ObjectDoc, parent aff3, skip bool)
		walk = func(objs []sceneformat.ObjectDoc, parent aff3, skip bool) {
			for oi := range objs {
				o := &objs[oi]
				posed := e.posed3D(o)
				// A hidden or locked object takes its descendants with it.
				skipped := skip || e.hidden[o.ID] || e.locked[o.ID]
				if meshHit && o.ID == meshID {
					meshIdx = idx
				}
				if _, geometry := e.geometryBounds3D(o.ID); !meshHit && visible && !skipped && !geometry {
					bmin, bmax := aabbOfCorners3D(e.objectCorners3DUnder(posed, parent))
					if t, hit := intersectRayAABB64(orig, dir, vec64(bmin), vec64(bmax)); hit && t < boxT {
						boxT = t
						boxIdx = idx
					}
				}
				idx++
				walk(o.Children, parent.mul(local3(posed)), skipped)
			}
		}
		walk(layer.Objects, ident3, false)
	}
	if meshIdx >= 0 {
		return meshIdx, true
	}
	if boxIdx < 0 {
		return -1, false
	}
	return boxIdx, true
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
	return e.Select3DAtWith(sx, sy, w, h, false, false)
}

// Select3DAtWith picks the closest 3D object under (sx, sy) and applies selection with
// optional Shift (add) and Mod (toggle) modifiers.
func (e *Editor) Select3DAtWith(sx, sy, w, h float32, shift, mod bool) bool {
	if e == nil {
		return false
	}
	if id, ok := e.Pick3DObject(sx, sy, w, h); ok {
		switch {
		case mod:
			e.ToggleSelection(id)
		case shift:
			e.AddToSelection(id)
		default:
			e.SelectObject(id)
		}
		return true
	}
	if !shift && !mod {
		e.ClearSelection()
	}
	return false
}

// gizmo3DOrigin returns the world-space origin of the 3D gizmo: the selected object's position
// for a single selection, or the bounding-box center of all draggable selected objects for a
// multi-selection.
func (e *Editor) gizmo3DOrigin() ([3]float32, bool) {
	draggable := e.draggableSelectedIDs()
	if len(draggable) == 0 {
		return [3]float32{}, false
	}
	if len(draggable) == 1 {
		obj, _, _ := e.FindObject(draggable[0])
		if obj == nil {
			return [3]float32{}, false
		}
		return e.worldPosition3D(obj), true
	}
	hasAny := false
	var bmin, bmax [3]float32
	for _, id := range draggable {
		obj, _, _ := e.FindObject(id)
		if obj == nil {
			continue
		}
		p := e.worldPosition3D(obj)
		if !hasAny {
			bmin, bmax = p, p
			hasAny = true
		} else {
			for a := range 3 {
				bmin[a] = min(bmin[a], p[a])
				bmax[a] = max(bmax[a], p[a])
			}
		}
	}
	if !hasAny {
		return [3]float32{}, false
	}
	return [3]float32{
		(bmin[0] + bmax[0]) * 0.5,
		(bmin[1] + bmax[1]) * 0.5,
		(bmin[2] + bmax[2]) * 0.5,
	}, true
}

// GizmoLength3D returns the world-space arm length / ring radius for a 3D gizmo placed at pos.
func (e *Editor) GizmoLength3D(pos [3]float32) float32 {
	if e == nil {
		return 32
	}
	// In orthographic a gizmo's size on screen does not change with its distance from the eye, so it is sized by the view's zoom instead, as large as a perspective view shows it at Target.
	dist, _, _, _ := e.Camera3D.effectiveParams()
	if !e.Camera3D.Orthographic {
		dist = len64(sub64(vec64(pos), e.Camera3D.eye64()))
	}
	l := float32(dist * 0.18)
	if l < 2 {
		l = 2
	}
	return l
}

func (e *Editor) gizmo3DBlocked() bool {
	return e == nil || e.playing || e.gridMap.tool != GridMapToolNone || (e.TerrainOpen() && e.terrainBrush.Mode != TerrainBrushNone)
}

// PickGizmo3D hit-tests the active 3D gizmo handles on the currently selected object(s) in screen space
// through the shared 3D gizmo handle picking engine.
func (e *Editor) PickGizmo3D(sx, sy, w, h float32) GizmoAxis3D {
	if e.gizmo3DBlocked() || w <= 0 || h <= 0 {
		return Axis3DNone
	}
	if e.GizmoMode == Gizmo3DCollider {
		if e.SelectionCount() != 1 {
			return Axis3DNone
		}
		obj, _ := e.SelectedObject()
		if obj == nil || e.viewportFrozen(obj.ID) {
			return Axis3DNone
		}
		if hh, ok := e.pickColliderHandle3D(obj, sx, sy, w, h); ok {
			return hh.Axis
		}
		return Axis3DNone
	}
	origin, ok := e.gizmo3DOrigin()
	if !ok {
		return Axis3DNone
	}
	gizmoLen := e.GizmoLength3D(origin)
	handles := transformGizmoHandles3D(e.GizmoMode, gizmoLen, true)
	originMat := math3d.Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		origin[0], origin[1], origin[2], 1,
	}
	proj := e.Projection3D(w, h)
	idx, _, hit := pickGizmoHandles3D(proj, originMat, handles, sx, sy, float64(gizmoHitRadius))
	if !hit {
		return Axis3DNone
	}
	return gizmoAxisFromHandleID(handles[idx].ID)
}

type drag3DKind int

const (
	drag3DNone drag3DKind = iota
	drag3DGizmo
	drag3DHandle
	drag3DComponentGizmo
	drag3DPlugin
	drag3DOrbit
	drag3DPan
	drag3DTerrain
)

type drag3DTarget struct {
	id             string
	startTransform Transform3D
	startPos       [3]float64
	parentInv      aff3
}

type drag3DState struct {
	kind                 drag3DKind
	handle               Handle3DKind
	objectID             string
	targets              []drag3DTarget // non-empty when dragging >1 selected objects
	mode                 GizmoMode3D
	axis                 GizmoAxis3D
	startSX              float32
	startSY              float32
	lastSX               float32
	lastSY               float32
	vpW                  float32
	vpH                  float32
	gizmoLen             float32
	startTransform       Transform3D
	startLight           *sceneformat.LightDoc
	startCamera          *sceneformat.Camera3DDoc
	startReflectionProbe *sceneformat.ReflectionProbeDoc
	startLightProbeGrid  *sceneformat.LightProbeGridDoc
	// startPos is where the object's origin sat in world space as the drag began, and parentInv maps world space to its parent's space, the identity for a top-level object.
	startPos           [3]float64
	parentInv          aff3
	startAxisT         float64
	hasAxisT           bool
	prevRingVec        [3]float64
	hasRingVec         bool
	accumAngle         float64
	handleAxis         [3]float64
	handleRight        [3]float64
	handleUp           [3]float64
	handleFwd          [3]float64
	handlePlanePt      [3]float64
	handlePlaneN       [3]float64
	handleBaseLen      float64
	handleBoundsCenter [3]float64
	handleBoundsSize   [3]float64
	handleBoundsAxis   int
	handleBoundsSign   float64
	handleBoundsScale  float64
	handleBoundsIsRefl bool

	// Collider size-handle drag state (active when mode == Gizmo3DCollider).
	colliderComp         string
	colliderField        string
	colliderSign         float32
	colliderAxisDir      [3]float64
	colliderAxisScale    float64
	colliderIsRadius     bool
	colliderWasNil       bool
	colliderCompExisted  bool
	colliderFieldExisted bool
	colliderOldVal       any
	colliderStartVal     float64
	colliderMinVal       float64
	colliderMaxVal       float64
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
	if e.gizmo3DBlocked() {
		return Axis3DNone
	}
	if e.GizmoMode == Gizmo3DCollider {
		if w <= 0 || h <= 0 || e.SelectionCount() != 1 {
			return Axis3DNone
		}
		obj, _ := e.SelectedObject()
		if obj == nil || e.viewportFrozen(obj.ID) {
			return Axis3DNone
		}
		hh, ok := e.pickColliderHandle3D(obj, sx, sy, w, h)
		if !ok || !e.beginColliderDrag3D(obj, hh, sx, sy, w, h) {
			return Axis3DNone
		}
		return hh.Axis
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
	if e.gizmo3DBlocked() || axis == Axis3DNone || w <= 0 || h <= 0 || !isFiniteFloat32(sx) || !isFiniteFloat32(sy) || !isFiniteFloat32(w) || !isFiniteFloat32(h) {
		return false
	}
	if e.GizmoMode == Gizmo3DCollider {
		if e.SelectionCount() != 1 {
			return false
		}
		obj, _ := e.SelectedObject()
		if obj == nil || e.viewportFrozen(obj.ID) {
			return false
		}
		for _, hh := range e.ColliderHandles3D(obj) {
			if hh.Axis == axis && hh.Sign > 0 {
				return e.beginColliderDrag3D(obj, hh, sx, sy, w, h)
			}
		}
		return false
	}
	draggable := e.draggableSelectedIDs()
	if len(draggable) == 0 {
		return false
	}
	pos, ok := e.gizmo3DOrigin()
	if !ok {
		return false
	}
	gizmoLen := e.GizmoLength3D(pos)
	var st drag3DState
	if len(draggable) == 1 {
		obj, _, _ := e.FindObject(draggable[0])
		if obj == nil || e.viewportFrozen(obj.ID) {
			return false
		}
		parentInv, ok := e.parentWorld3D(obj.ID).invert()
		if !ok {
			// A parent scaled to zero leaves no space to drag the object in.
			return false
		}
		st = drag3DState{
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
	} else {
		targets := make([]drag3DTarget, 0, len(draggable))
		for _, id := range draggable {
			obj, _, _ := e.FindObject(id)
			if obj == nil || e.viewportFrozen(id) {
				continue
			}
			parentInv, ok := e.parentWorld3D(id).invert()
			if !ok {
				continue
			}
			targets = append(targets, drag3DTarget{
				id:             id,
				startTransform: captureTransform3D(obj),
				startPos:       vec64(e.worldPosition3D(obj)),
				parentInv:      parentInv,
			})
		}
		if len(targets) == 0 {
			return false
		}
		st = drag3DState{
			kind:     drag3DGizmo,
			objectID: e.SelectedID,
			targets:  targets,
			mode:     e.GizmoMode,
			axis:     axis,
			startSX:  sx,
			startSY:  sy,
			lastSX:   sx,
			lastSY:   sy,
			vpW:      w,
			vpH:      h,
			gizmoLen: gizmoLen,
			startPos: vec64(pos),
		}
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

// UpdateGizmoDrag3D updates the selected object's (or objects') 3D transform live during an active gizmo drag.
func (e *Editor) UpdateGizmoDrag3D(sx, sy float32) {
	if e == nil || e.playing || e.drag3D.kind != drag3DGizmo || !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return
	}
	if len(e.drag3D.targets) > 0 {
		e.updateMultiGizmoDrag3D(sx, sy)
		return
	}
	obj, _, _ := e.FindObject(e.drag3D.objectID)
	if obj == nil {
		e.drag3D = drag3DState{}
		return
	}

	st := &e.drag3D
	if st.mode == Gizmo3DCollider {
		e.updateColliderDrag3D(obj, st, sx, sy)
		return
	}
	w, h := st.vpW, st.vpH
	startPos := st.startPos
	axisDir := axisVector64(st.axis)
	orig32, dir32 := e.Camera3D.ScreenRay(sx, sy, w, h)
	orig := vec64(orig32)
	dir := vec64(dir32)

	switch st.mode {
	case Gizmo3DTranslate:
		delta := e.computeAxisDelta(st, startPos, axisDir, orig, dir, sx, sy)
		if math.IsNaN(delta) || math.IsInf(delta, 0) {
			return
		}
		if grid, pixel := e.positionSnapMode(); grid || pixel {
			axisIdx := 0
			switch st.axis {
			case Axis3DY:
				axisIdx = 1
			case Axis3DZ:
				axisIdx = 2
			}
			snappedCoord := e.SnapValue(startPos[axisIdx] + delta)
			if !math.IsNaN(snappedCoord) && !math.IsInf(snappedCoord, 0) {
				delta = snappedCoord - startPos[axisIdx]
			}
		}
		worldDelta := scale64(axisDir, delta)
		// The drag runs along a world axis; a nested child's position is in its parent's space.
		local := st.parentInv.vector(worldDelta)
		nx, ny, nz := st.startTransform.X+float32(local[0]), st.startTransform.Y+float32(local[1]), st.startTransform.PosZ+float32(local[2])
		if !isFiniteFloat32(nx) || !isFiniteFloat32(ny) || !isFiniteFloat32(nz) {
			return
		}
		obj.X, obj.Y, obj.PosZ = nx, ny, nz

	case Gizmo3DRotate:
		angle := e.computeRotationAngle(st, startPos, axisDir, orig, dir, sx, sy)
		if math.IsNaN(angle) || math.IsInf(angle, 0) {
			return
		}
		switch st.axis {
		case Axis3DX:
			rx := float32(e.SnapRotationRadians(float64(st.startTransform.RotX) + angle))
			if !isFiniteFloat32(rx) {
				return
			}
			obj.RotX = rx
			obj.RotY = st.startTransform.RotY
			obj.RotZ = st.startTransform.RotZ
		case Axis3DY:
			ry := float32(e.SnapRotationRadians(float64(st.startTransform.RotY) + angle))
			if !isFiniteFloat32(ry) {
				return
			}
			obj.RotX = st.startTransform.RotX
			obj.RotY = ry
			obj.RotZ = st.startTransform.RotZ
		case Axis3DZ:
			rz := float32(e.SnapRotationRadians(float64(st.startTransform.RotZ) + angle))
			if !isFiniteFloat32(rz) {
				return
			}
			obj.RotX = st.startTransform.RotX
			obj.RotY = st.startTransform.RotY
			obj.RotZ = rz
		}

	case Gizmo3DScale:
		delta := e.computeAxisDelta(st, startPos, axisDir, orig, dir, sx, sy)
		if math.IsNaN(delta) || math.IsInf(delta, 0) {
			return
		}
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
				if v := float32(e.SnapScaleFactor(base * factor)); isFiniteFloat32(v) {
					obj.ScaleX = sceneformat.Float32(v)
				}
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
				if v := float32(e.SnapScaleFactor(base * factor)); isFiniteFloat32(v) {
					obj.ScaleY = sceneformat.Float32(v)
				}
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
				if v := float32(e.SnapScaleFactor(base * factor)); isFiniteFloat32(v) {
					obj.ScaleZ = sceneformat.Float32(v)
				}
			}
		}
	}

	st.lastSX = sx
	st.lastSY = sy
	e.Rebuild()
	e.notify()
}

func (e *Editor) updateMultiGizmoDrag3D(sx, sy float32) {
	if !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return
	}
	st := &e.drag3D
	w, h := st.vpW, st.vpH
	startPos := st.startPos
	axisDir := axisVector64(st.axis)
	orig32, dir32 := e.Camera3D.ScreenRay(sx, sy, w, h)
	orig := vec64(orig32)
	dir := vec64(dir32)

	objs := make([]*sceneformat.ObjectDoc, len(st.targets))
	nexts := make([]Transform3D, len(st.targets))
	for i, t := range st.targets {
		obj, _, _ := e.FindObject(t.id)
		if obj == nil {
			e.drag3D = drag3DState{}
			return
		}
		objs[i] = obj
		nexts[i] = t.startTransform
	}

	switch st.mode {
	case Gizmo3DTranslate:
		delta := e.computeAxisDelta(st, startPos, axisDir, orig, dir, sx, sy)
		if math.IsNaN(delta) || math.IsInf(delta, 0) {
			return
		}
		if grid, pixel := e.positionSnapMode(); grid || pixel {
			axisIdx := 0
			switch st.axis {
			case Axis3DY:
				axisIdx = 1
			case Axis3DZ:
				axisIdx = 2
			}
			anchorPos := st.targets[0].startPos
			for _, t := range st.targets {
				if t.id == st.objectID {
					anchorPos = t.startPos
					break
				}
			}
			snappedCoord := e.SnapValue(anchorPos[axisIdx] + delta)
			if !math.IsNaN(snappedCoord) && !math.IsInf(snappedCoord, 0) {
				delta = snappedCoord - anchorPos[axisIdx]
			}
		}
		worldDelta := scale64(axisDir, delta)
		for i, t := range st.targets {
			local := t.parentInv.vector(worldDelta)
			nexts[i].X = t.startTransform.X + float32(local[0])
			nexts[i].Y = t.startTransform.Y + float32(local[1])
			nexts[i].PosZ = t.startTransform.PosZ + float32(local[2])
		}
	case Gizmo3DRotate:
		angle := e.computeRotationAngle(st, startPos, axisDir, orig, dir, sx, sy)
		if math.IsNaN(angle) || math.IsInf(angle, 0) {
			return
		}
		angle = e.SnapRotationRadians(angle)
		if math.IsNaN(angle) || math.IsInf(angle, 0) {
			return
		}
		cosA, sinA := math.Cos(angle), math.Sin(angle)
		for i, t := range st.targets {
			rel := sub64(t.startPos, startPos)
			kDotR := dot64(axisDir, rel)
			kCrossR := cross64(axisDir, rel)
			rotRel := add64(
				add64(scale64(rel, cosA), scale64(kCrossR, sinA)),
				scale64(axisDir, kDotR*(1-cosA)),
			)
			worldDelta := sub64(rotRel, rel)
			local := t.parentInv.vector(worldDelta)
			nexts[i].X = t.startTransform.X + float32(local[0])
			nexts[i].Y = t.startTransform.Y + float32(local[1])
			nexts[i].PosZ = t.startTransform.PosZ + float32(local[2])
			nexts[i].RotX = t.startTransform.RotX + float32(axisDir[0]*angle)
			nexts[i].RotY = t.startTransform.RotY + float32(axisDir[1]*angle)
			nexts[i].RotZ = t.startTransform.RotZ + float32(axisDir[2]*angle)
		}
	case Gizmo3DScale:
		delta := e.computeAxisDelta(st, startPos, axisDir, orig, dir, sx, sy)
		if math.IsNaN(delta) || math.IsInf(delta, 0) {
			return
		}
		denom := float64(st.gizmoLen)
		if denom <= 1e-4 {
			denom = 1
		}
		factor := 1.0 + delta/denom
		if e.scaleSnapActive() {
			factor = e.SnapScaleFactor(factor)
		}
		if math.IsNaN(factor) || math.IsInf(factor, 0) || math.Abs(factor) < minScale2D {
			return
		}
		for i, t := range st.targets {
			rel := sub64(t.startPos, startPos)
			proj := dot64(rel, axisDir)
			worldDelta := scale64(axisDir, proj*(factor-1))
			local := t.parentInv.vector(worldDelta)
			nexts[i].X = t.startTransform.X + float32(local[0])
			nexts[i].Y = t.startTransform.Y + float32(local[1])
			nexts[i].PosZ = t.startTransform.PosZ + float32(local[2])
			switch st.axis {
			case Axis3DX:
				base := 1.0
				if t.startTransform.ScaleX != nil {
					base = float64(*t.startTransform.ScaleX)
				}
				if base == 0 {
					base = 1
				}
				sv := float32(base * factor)
				if !isFiniteFloat32(sv) || math.Abs(float64(sv)) < minScale2D {
					return
				}
				nexts[i].ScaleX = sceneformat.Float32(sv)
			case Axis3DY:
				base := 1.0
				if t.startTransform.ScaleY != nil {
					base = float64(*t.startTransform.ScaleY)
				}
				if base == 0 {
					base = 1
				}
				sv := float32(base * factor)
				if !isFiniteFloat32(sv) || math.Abs(float64(sv)) < minScale2D {
					return
				}
				nexts[i].ScaleY = sceneformat.Float32(sv)
			case Axis3DZ:
				base := 1.0
				if t.startTransform.ScaleZ != nil {
					base = float64(*t.startTransform.ScaleZ)
				}
				if base == 0 {
					base = 1
				}
				sv := float32(base * factor)
				if !isFiniteFloat32(sv) || math.Abs(float64(sv)) < minScale2D {
					return
				}
				nexts[i].ScaleZ = sceneformat.Float32(sv)
			}
		}
	}

	for _, tr := range nexts {
		if !isFiniteFloat32(tr.X) || !isFiniteFloat32(tr.Y) || !isFiniteFloat32(tr.PosZ) ||
			!isFiniteFloat32(tr.RotX) || !isFiniteFloat32(tr.RotY) || !isFiniteFloat32(tr.RotZ) {
			return
		}
	}
	for i, obj := range objs {
		applyTransform3D(obj, nexts[i])
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
	if st.mode == Gizmo3DCollider {
		return e.endColliderDrag3D(st)
	}
	if len(st.targets) > 0 {
		type pending struct {
			id    string
			obj   *sceneformat.ObjectDoc
			start Transform3D
			after Transform3D
		}
		var changed []pending
		for _, t := range st.targets {
			obj, _, _ := e.FindObject(t.id)
			if obj == nil {
				continue
			}
			after := captureTransform3D(obj)
			if !equalTransform3D(t.startTransform, after) {
				changed = append(changed, pending{id: t.id, obj: obj, start: t.startTransform, after: after})
			}
		}
		if len(changed) == 0 {
			e.notify()
			return false
		}
		for _, p := range changed {
			applyTransform3D(p.obj, p.start)
		}
		for _, p := range changed {
			if e.checkTransformEdit(p.id, func(o *sceneformat.ObjectDoc) { applyTransform3D(o, p.after) }) != nil {
				e.Rebuild()
				e.notify()
				return false
			}
		}
		label := fmt.Sprintf("%s 3D %d objects (%s)", st.mode.String(), len(changed), st.axis.String())
		_ = e.History.Group(label, func() error {
			for _, p := range changed {
				e.History.Execute(&transform3DCmd{
					ed:     e,
					id:     p.id,
					label:  st.mode.String() + " 3D " + p.id + " (" + st.axis.String() + ")",
					before: p.start,
					after:  p.after,
				})
			}
			return nil
		})
		return true
	}

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
	if e.checkTransformEdit(st.objectID, func(o *sceneformat.ObjectDoc) { applyTransform3D(o, after) }) != nil {
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

// CancelGizmoDrag3D aborts any active 3D gizmo or light/camera handle drag and restores the object's pre-drag state.
func (e *Editor) CancelGizmoDrag3D() {
	if e == nil {
		return
	}
	if e.drag3D.kind == drag3DHandle {
		e.CancelHandleDrag3D()
		return
	}
	if e.drag3D.kind != drag3DGizmo {
		return
	}
	st := e.drag3D
	e.drag3D = drag3DState{}
	if st.mode == Gizmo3DCollider {
		if obj, _, _ := e.FindObject(st.objectID); obj != nil {
			restoreColliderDragState(obj, &st)
		}
		e.notify()
		return
	}
	if len(st.targets) > 0 {
		restored := false
		for _, t := range st.targets {
			if obj, _, _ := e.FindObject(t.id); obj != nil {
				applyTransform3D(obj, t.startTransform)
				restored = true
			}
		}
		if restored {
			e.Rebuild()
		}
		e.notify()
		return
	}
	if obj, _, _ := e.FindObject(st.objectID); obj != nil {
		applyTransform3D(obj, st.startTransform)
		e.Rebuild()
	}
	e.notify()
}

// HandleViewport3DPress handles a click/tap in the 3D viewport: with a terrain brush active it applies a dab, with an active 3D plugin tool it calls Press3D with the camera pick ray, with the Place tool and a model selected in the asset browser (SelectedModel) it places the model where the press lands (PlaceModelAt3D), otherwise it picks the closest 3D object or clears selection.
func (e *Editor) HandleViewport3DPress(sx, sy, w, h float32) {
	e.HandleViewport3DPressWith(sx, sy, w, h, false, false)
}

// HandleViewport3DPressWith handles a click/tap in the 3D viewport with optional Shift (add)
// and Mod (toggle) modifiers.
func (e *Editor) HandleViewport3DPressWith(sx, sy, w, h float32, shift, mod bool) {
	if e == nil || e.playing {
		return
	}
	if e.connPick != nil {
		if hit, ok := e.Pick3DObject(sx, sy, w, h); ok && hit != "" {
			e.finishConnectionPick(hit)
		}
		return
	}
	if e.TerrainOpen() && e.terrainBrush.Mode != TerrainBrushNone {
		if e.BeginTerrainStrokeAt(sx, sy, w, h) {
			e.EndTerrainStroke()
			return
		}
	}
	if e.Tool == ToolPlugin {
		if t := e.activePluginTool(); t != nil && t.supports3D() {
			if t.Press3D != nil {
				t.Press3D(e, e.PickRay3D(sx, sy, w, h))
			}
			return
		}
	}
	if e.Tool == ToolPlace && e.SelectedModel != nil {
		_, err := e.PlaceModelAt3D(*e.SelectedModel, sx, sy, w, h)
		e.report(err)
		return
	}
	if !shift && !mod {
		if e.PickHandle3D(sx, sy, w, h) != Handle3DNone {
			return
		}
		if _, hit := e.PickComponentGizmo3D(sx, sy, w, h); hit {
			return
		}
		if e.PickGizmo3D(sx, sy, w, h) != Axis3DNone {
			return
		}
	}
	e.Select3DAtWith(sx, sy, w, h, shift, mod)
}

// BeginViewport3DDrag starts a terrain brush stroke (if a terrain brush is active and hits the terrain and !pan), an active 3D plugin tool drag (if OnDragBegin3D claims it and !pan), a light/camera handle drag, a 3D component gizmo handle drag, a 3D transform gizmo drag, or a camera orbit/pan drag. It does nothing while the camera flies (BeginFly3D).
func (e *Editor) BeginViewport3DDrag(sx, sy, w, h float32, pan bool) {
	if e == nil || e.fly3D {
		return
	}
	if !pan && !e.playing && e.TerrainOpen() && e.terrainBrush.Mode != TerrainBrushNone {
		if e.BeginTerrainStrokeAt(sx, sy, w, h) {
			e.drag3D = drag3DState{
				kind:    drag3DTerrain,
				startSX: sx,
				startSY: sy,
				lastSX:  sx,
				lastSY:  sy,
				vpW:     w,
				vpH:     h,
			}
			return
		}
	}
	if !pan && !e.playing && e.Tool == ToolPlugin && e.BeginPluginDrag3D(sx, sy, w, h) {
		return
	}
	if !pan && !e.playing {
		if handle := e.BeginHandleDrag3D(sx, sy, w, h); handle != Handle3DNone {
			return
		}
		if _, ok := e.BeginComponentGizmoDrag3D(sx, sy, w, h); ok {
			return
		}
		if axis := e.BeginGizmoDrag3D(sx, sy, w, h); axis != Axis3DNone {
			return
		}
	}
	if e.lookThroughCamera3D {
		e.lookThroughCamera3D = false
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

// UpdateViewport3DDrag advances the active terrain brush stroke, 3D plugin tool drag, 3D handle drag, 3D component gizmo drag, 3D transform gizmo drag, or camera orbit/pan drag.
func (e *Editor) UpdateViewport3DDrag(sx, sy float32) {
	if e == nil {
		return
	}
	switch e.drag3D.kind {
	case drag3DTerrain:
		e.drag3D.lastSX = sx
		e.drag3D.lastSY = sy
		e.ContinueTerrainStrokeAt(sx, sy, e.drag3D.vpW, e.drag3D.vpH)
	case drag3DHandle:
		e.UpdateHandleDrag3D(sx, sy)
	case drag3DPlugin:
		e.UpdatePluginDrag3D(sx, sy)
	case drag3DComponentGizmo:
		e.UpdateComponentGizmoDrag3D(sx, sy)
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
		e.PanView3D(dx, dy, e.drag3D.vpH)
	}
}

// EndViewport3DDrag finishes any active terrain brush stroke, 3D plugin tool drag, 3D handle drag, 3D component gizmo drag, 3D transform gizmo drag, or camera drag.
func (e *Editor) EndViewport3DDrag() {
	if e == nil {
		return
	}
	switch e.drag3D.kind {
	case drag3DTerrain:
		e.drag3D = drag3DState{}
		e.EndTerrainStroke()
		return
	case drag3DHandle:
		e.EndHandleDrag3D()
		return
	case drag3DPlugin:
		e.EndPluginDrag3D(e.drag3D.lastSX, e.drag3D.lastSY)
		return
	case drag3DComponentGizmo:
		e.EndComponentGizmoDrag3D()
		return
	case drag3DGizmo:
		e.EndGizmoDrag3D()
		return
	}
	e.drag3D = drag3DState{}
}

// CancelViewport3DDrag aborts any active 3D drag (terrain stroke, 3D handle drag, 3D plugin tool drag, 3D component gizmo drag, 3D transform gizmo drag, or camera drag).
func (e *Editor) CancelViewport3DDrag() {
	if e == nil {
		return
	}
	switch e.drag3D.kind {
	case drag3DTerrain:
		e.drag3D = drag3DState{}
		e.CancelTerrainStroke()
		return
	case drag3DHandle:
		e.CancelHandleDrag3D()
		return
	case drag3DPlugin:
		e.CancelPluginDrag3D()
		return
	case drag3DComponentGizmo:
		e.CancelComponentGizmoDrag3D()
		return
	case drag3DGizmo:
		e.CancelGizmoDrag3D()
		return
	}
	e.drag3D = drag3DState{}
}

// HandleViewport3DWheel zooms the 3D orbit camera proportionally to deltaY, or while the camera flies (Flying3D) changes its fly speed (AdjustFlySpeed3D).
func (e *Editor) HandleViewport3DWheel(deltaY float32) {
	if e == nil || deltaY == 0 {
		return
	}
	if e.fly3D {
		e.AdjustFlySpeed3D(deltaY)
		return
	}
	e.lookThroughCamera3D = false
	dist, _, _, _ := e.Camera3D.effectiveParams()
	step := -deltaY * 0.002 * float32(dist)
	e.Camera3D.Zoom(step)
	e.notify()
}

// DrawViewport3D renders the 3D scene view onto d. Without the GPU view (Viewport3DOnGPU) it draws the whole view: the scene's background, the XZ ground grid, the world origin axes, a depth-sorted wireframe box for every visible object (the bounds of its geometry when it draws a mesh or model, ObjectCorners3D) and the active 3D gizmo. While the GPU view draws the scene's geometry under it, d is an overlay: it leaves the background clear and draws the grid, the axes, a box for each object that draws no geometry, the selected object's box as its selection outline, the edges of the geometry's triangles in Shading3DWireframe, and the gizmo.
func (e *Editor) DrawViewport3D(d *core.Drawing, size core.Size) {
	if e == nil || d == nil {
		return
	}
	w, h := float32(size.Width), float32(size.Height)
	if w <= 0 || h <= 0 {
		w, h = e.ViewportSize()
	}
	s3 := e.scene3D()
	overlay := e.gpu3D

	// 1. Background fill, which the GPU view draws when it draws the scene.
	if !overlay {
		d.Save().SetFillColor(e.background3D()).FillRect(0, 0, w, h).Restore()
	}

	// 2. 3D ground grid on XZ plane (Y = 0).
	e.drawGrid3D(d, w, h)

	// 3. World origin axes (X red, Y green, Z blue).
	e.drawOriginAxes3D(d, w, h)

	// 3b. Open terrain heightmap surface, painted layers, foliage density preview, and brush cursor.
	e.drawTerrain3D(d, w, h)

	// 4. Evaluate 3D LODs and occlusion buffer, then draw projected 3D wireframe bounding boxes for the visible scene objects.
	e.ensureViewport3DPlan(w, h)
	e.drawSceneObjects3D(d, w, h, overlay)

	// 4a. Active 3D GridMap floor, occupied cells, and hover/box-fill preview.
	e.drawGridMap3D(d, w, h)

	// 4b. Projected 3D occluder volumes (box and model occluders).
	e.drawOccluders3D(d, w, h)

	// 4c. Light and 3D camera icons.
	e.drawSceneLightAndCameraIcons3D(d, w, h)
	// 4b. Projected 3D physics collider and trigger debug wireframes.
	e.drawPhysicsDebug3D(d, w, h)

	// 4c. Projected 3D navigation mesh walkable polygons and edges.
	e.drawNavMeshOverlay3D(d, w, h)

	// 5. The edges of the geometry the GPU view draws, in wireframe shading.
	if overlay && e.Shading3D == Shading3DWireframe {
		e.drawWireframe3D(d, s3, w, h)
	}

	proj := e.Projection3D(w, h)

	// 6. Active 3D transform gizmo, light/camera handles, and component gizmo handles on the selected object (hidden while painting a GridMap or 3D Terrain brush).
	if !e.gizmo3DBlocked() {
		e.drawSceneHandles3D(d, w, h)
		e.drawGizmo3D(d, w, h)
		e.drawComponentGizmos3D(d, proj)
	}

	// 7. Active 3D plugin tool overlay.
	if !e.playing {
		if t := e.activePluginTool(); t != nil && t.Draw3D != nil {
			d.Save()
			t.Draw3D(e, d, proj)
			d.Restore()
		}
	}
}

// background3D returns the scene's background color, or the editor's dark default when it sets none or one that does not parse.
func (e *Editor) background3D() core.Color {
	bg := core.RGBA(0x141824, 1)
	if e.Doc != nil && e.Doc.BackgroundColor != "" {
		if parsed, err := core.ParseColor(e.Doc.BackgroundColor); err == nil {
			bg = parsed
		}
	}
	return bg
}

// maxWireframeEdges3D caps the triangle edges the wireframe overlay draws in one frame, so a dense model cannot stall the canvas.
const maxWireframeEdges3D = 1 << 18

// drawWireframe3D strokes the edges of the triangles of every visible part of the 3D build, at the posed nodes' transforms.
func (e *Editor) drawWireframe3D(d *core.Drawing, s *scene3DState, w, h float32) {
	if s.built == nil {
		return
	}
	d.Save().SetStrokeColor(core.RGBA(0xE2E8F0, 0.55)).SetLineWidth(1).BeginPath()
	drawn := 0
	var world [][3]float32
	for _, dr := range s.built.Drawables {
		if !dr.Node.EffectiveVisible() {
			continue
		}
		m := dr.Node.WorldMatrix()
		for pi := range dr.Parts {
			p := &dr.Parts[pi]
			world = world[:0]
			for _, v := range p.Mesh.Positions {
				q := m.TransformPoint(v)
				world = append(world, [3]float32{q.X, q.Y, q.Z})
			}
			for _, edge := range s.meshEdges3D(p.MeshID, p.Mesh) {
				if drawn == maxWireframeEdges3D {
					d.Stroke().Restore()
					return
				}
				if x0, y0, x1, y1, vis := e.Camera3D.projectSegment(world[edge[0]], world[edge[1]], w, h); vis {
					d.MoveTo(x0, y0).LineTo(x1, y1)
					drawn++
				}
			}
		}
	}
	if drawn > 0 {
		d.Stroke()
	}
	d.Restore()
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
	vc := e.activeView3DCam(w, h)

	d.Save().SetStrokeColor(core.RGBA(0x94A3B8, 0.22)).SetLineWidth(1).BeginPath()
	hasSegments := false
	for i := -half; i <= half; i++ {
		coord := float32(i) * step
		if x0, y0, x1, y1, vis := vc.projectSegment(
			[3]float32{coord, 0, -extent},
			[3]float32{coord, 0, extent},
			w, h,
		); vis {
			d.MoveTo(x0, y0).LineTo(x1, y1)
			hasSegments = true
		}
		if x0, y0, x1, y1, vis := vc.projectSegment(
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
	vc := e.activeView3DCam(w, h)

	axes := []struct {
		end   [3]float32
		color core.Color
	}{
		{[3]float32{axisLen, 0, 0}, core.RGBA(0xEF4444, 0.9)},
		{[3]float32{0, axisLen, 0}, core.RGBA(0x22C55E, 0.9)},
		{[3]float32{0, 0, axisLen}, core.RGBA(0x3B82F6, 0.9)},
	}
	for _, ax := range axes {
		x0, y0, x1, y1, vis := vc.projectSegment(origin, ax.end, w, h)
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

// drawSceneObjects3D strokes the boxes of the visible scene objects, back to front with the selected one last; over the GPU view (overlay) it leaves out the wireframe boxes of the unselected non-occluded objects whose geometry the GPU view draws, while still drawing their LOD badges and occluded dashed indicators.
func (e *Editor) drawSceneObjects3D(d *core.Drawing, w, h float32, overlay bool) {
	if e.Doc == nil {
		return
	}
	type drawable3D struct {
		obj       *sceneformat.ObjectDoc
		parent    aff3
		depth     float32
		selected  bool
		wireframe bool
	}
	vc := e.activeView3DCam(w, h)
	eye := vc.eye
	forward := vc.forward

	var items []drawable3D
	for li := range e.Doc.Layers {
		layer := &e.Doc.Layers[li]
		if !layer.IsVisible() || layer.EffectiveSpace() == sceneformat.SpaceScreen {
			continue
		}
		// Each level composes its parent's world transform once and hands it down, so drawing stays linear in the number of objects.
		var walk func(objs []sceneformat.ObjectDoc, parent aff3)
		walk = func(objs []sceneformat.ObjectDoc, parent aff3) {
			for oi := range objs {
				o := &objs[oi]
				// A hidden object is not drawn, nor are its descendants.
				if e.hidden[o.ID] {
					continue
				}
				children := o.Children
				// While an animation clip is open, draw each object as the clip poses it at the playhead, as the 2D viewport does through ActiveScene.
				o = e.posed3D(o)
				if isLightOrCameraOnly3D(o) {
					walk(children, parent.mul(local3(o)))
					continue
				}
				_, geometry := e.geometryBounds3D(o.ID)
				occluded := e.vp3d.occludedObjects[o.ID]
				_, hasLOD := e.vp3d.lodByObject[o.ID]
				selected := e.IsSelected(o.ID)
				wireframe := !(overlay && geometry && !selected) || occluded
				if !wireframe && !(e.ShowLODOverlay && hasLOD) {
					walk(children, parent.mul(local3(o)))
					continue
				}
				rel := sub64(parent.point([3]float64{float64(o.X), float64(o.Y), float64(o.PosZ)}), eye)
				depth := float32(dot64(rel, forward))
				items = append(items, drawable3D{
					obj:       o,
					parent:    parent,
					depth:     depth,
					selected:  selected,
					wireframe: wireframe,
				})
				walk(children, parent.mul(local3(o)))
			}
		}
		walk(layer.Objects, ident3)
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
		occluded := e.vp3d.occludedObjects[item.obj.ID]
		lod, hasLOD := e.vp3d.lodByObject[item.obj.ID]
		if item.wireframe {
			corners := e.objectCorners3DUnder(item.obj, item.parent)
			color := core.RGBA(0xCBD5E1, 0.8)
			lineWidth := float32(1.25)
			if occluded {
				color = core.RGBA(0x94A3B8, 0.35)
			} else if e.ShowLODOverlay && hasLOD {
				color = lodColor3D(lod)
			}
			if item.selected {
				color = core.RGBA(0x38BDF8, 1)
				lineWidth = 2.0
			}

			d.Save().SetStrokeColor(color).SetLineWidth(lineWidth)
			if occluded {
				d.SetLineDash(4, 4)
			}
			d.BeginPath()
			hasEdge := false
			for _, edge := range boxEdges3D {
				x0, y0, x1, y1, vis := vc.projectSegment(corners[edge[0]], corners[edge[1]], w, h)
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
		}

		if e.ShowLODOverlay && hasLOD && !occluded {
			e.drawObjectLODBadge3D(d, item.obj, lod, w, h)
		}

		if item.selected {
			if cx, cy, _, vis := vc.project(e.worldPosition3D(item.obj), w, h); vis {
				d.Save().SetFillColor(core.RGBA(0x38BDF8, 1)).BeginPath().Arc(cx, cy, 3, 0, float32(2*math.Pi), false).Fill().Restore()
			}
		}
	}
}

func (e *Editor) drawGizmo3D(d *core.Drawing, w, h float32) {
	if e.GizmoMode == Gizmo3DCollider {
		if e.SelectionCount() != 1 {
			return
		}
		obj, _ := e.SelectedObject()
		if obj == nil || e.viewportFrozen(obj.ID) {
			return
		}
		e.drawColliderGizmo3D(d, obj, w, h)
		return
	}
	origin, ok := e.gizmo3DOrigin()
	if !ok {
		return
	}
	gizmoLen := e.GizmoLength3D(origin)
	handles := transformGizmoHandles3D(e.GizmoMode, gizmoLen, false)
	originMat := math3d.Mat4{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		origin[0], origin[1], origin[2], 1,
	}
	drawGizmoHandles3D(d, e.Projection3D(w, h), originMat, handles, gizmoHandleIDFromAxis(e.ActiveGizmoAxis3D()))
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

// projectSegment clips a 3D world segment against the camera's near plane before projecting to 2D, in perspective or in orthographic.
func (c OrbitCamera3D) projectSegment(a, b [3]float32, w, h float32) (x0, y0, x1, y1 float32, visible bool) {
	if w <= 0 || h <= 0 {
		return 0, 0, 0, 0, false
	}
	_, _, near, far := c.effectiveParams()
	eye := c.eye64()
	right, up, forward := c.basis64()
	ca := toCamera64(vec64(a), eye, right, up, forward)
	cb := toCamera64(vec64(b), eye, right, up, forward)

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

	x0, y0 = c.screen64(ca, w, h)
	x1, y1 = c.screen64(cb, w, h)
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
