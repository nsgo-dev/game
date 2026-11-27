package editor

import (
	"cmp"
	"fmt"
	"math"
	"reflect"
	"slices"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/render3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	// maxOrbitPitch is how far Orbit tilts the camera above or below the horizon, short of straight down or up so a drag never flips the view; the axis views (SetView) look straight down or up.
	maxOrbitPitch    float32 = float32(math.Pi/2) - 0.01
	minOrbitDistance float32 = 0.5
	maxOrbitDistance float32 = 50000
	gizmoHitRadius   float32 = 12
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

// Objects3D returns the objects of every layer, nested, instanced and inherited children included, depth first in document order with each parent before its children, with sequential flat indices (Pick3D's).
func (e *Editor) Objects3D() []Object3DRef {
	doc := e.resolvedDoc()
	if doc == nil {
		return nil
	}
	var out []Object3DRef
	for li := range doc.Layers {
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
		walk(doc.Layers[li].Objects, "")
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
	path := e.resolvedObjectPath(id)
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
	hx := float64(bw) * 0.5
	hy := float64(bh) * 0.5
	hz := float64(bd) * 0.5
	world := local3(o)
	if parent != ident3 {
		world = parent.mul(world)
	}
	for i, s := range boxSigns3D {
		corners[i] = vec32(world.point([3]float64{s[0] * hx, s[1] * hy, s[2] * hz}))
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

// Pick3D casts the camera's ray (ScreenRay), in perspective or in orthographic, through viewport pixel (sx, sy) and returns the flat index (Objects3D's) of the deepest closest visible scene object it hits, nested and instanced children included. An object that draws a mesh or model in the scene's 3D build (Scene3D) is hit on the triangles of its geometry, through the build's scene3d picking, and any other object on its world-space 3D AABB (ObjectAABB3D); when a ray hits both an ancestor and one of its descendants without a nearer unrelated object between them, the deepest hit descendant wins. A press that hits geometry picks the geometry's object even when the box of an object without geometry is nearer, since such boxes are a fixed size whatever the scene's scale and may enclose the camera or the whole level; the boxes are picked only where no geometry is hit. Objects hidden or locked in the editor (SetObjectHidden, SetObjectLocked), with their descendants, and the objects of hidden and locked layers (SetLayerVisible, SetLayerLocked) are passed over.
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

	doc := e.resolvedDoc()
	if doc == nil {
		return -1, false
	}
	type boxHit3D struct {
		idx       int
		depth     int
		t         float64
		ancestors []int
	}
	meshIdx := -1
	var boxHits []boxHit3D
	idx := 0
	var ancStack []int
	for li := range doc.Layers {
		layer := &doc.Layers[li]
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
				curIdx := idx
				if meshHit && o.ID == meshID {
					meshIdx = curIdx
				}
				if _, geometry := e.geometryBounds3D(o.ID); !meshHit && visible && !skipped && !geometry {
					bmin, bmax := aabbOfCorners3D(e.objectCorners3DUnder(posed, parent))
					if t, hit := intersectRayAABB64(orig, dir, vec64(bmin), vec64(bmax)); hit {
						boxHits = append(boxHits, boxHit3D{
							idx:       curIdx,
							depth:     len(ancStack),
							t:         t,
							ancestors: slices.Clone(ancStack),
						})
					}
				}
				idx++
				ancStack = append(ancStack, curIdx)
				walk(o.Children, parent.mul(local3(posed)), skipped)
				ancStack = ancStack[:len(ancStack)-1]
			}
		}
		walk(layer.Objects, ident3, false)
	}
	if meshIdx >= 0 {
		return meshIdx, true
	}
	if len(boxHits) == 0 {
		return -1, false
	}
	const tieEps = 1e-6
	best := boxHits[0]
	for _, h := range boxHits[1:] {
		if h.t < best.t-tieEps || (math.Abs(h.t-best.t) <= tieEps && h.depth > best.depth) {
			best = h
		}
	}
	// Descend along hit ancestor-descendant chains so a press picks the deepest hit child
	// even when its parent's scaled or enclosing box was entered earlier along the ray,
	// unless a nearer unrelated object sits in front of the descendant.
	for {
		minUnrelatedT := math.Inf(1)
		for _, h := range boxHits {
			if h.idx == best.idx || slices.Contains(h.ancestors, best.idx) || slices.Contains(best.ancestors, h.idx) {
				continue
			}
			if h.t < minUnrelatedT {
				minUnrelatedT = h.t
			}
		}
		nextIdx := -1
		for i, h := range boxHits {
			if h.depth <= best.depth || !slices.Contains(h.ancestors, best.idx) || h.t > minUnrelatedT+tieEps {
				continue
			}
			if nextIdx < 0 ||
				h.t < boxHits[nextIdx].t-tieEps ||
				(math.Abs(h.t-boxHits[nextIdx].t) <= tieEps && h.depth > boxHits[nextIdx].depth) {
				nextIdx = i
			}
		}
		if nextIdx < 0 {
			break
		}
		best = boxHits[nextIdx]
	}
	return best.idx, true
}

// Pick3DObject returns the ID of the deepest closest visible 3D object hit at viewport pixel (sx, sy).
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

// resolveHierarchicalPick3D resolves the target object for a 3D viewport press hitting hitID:
//   - with alt held, selects the owning scene instance root for an instanced child or the immediate
//     parent for a locally nested child (or hitID itself when top-level);
//   - on a repeated plain press (alt == false) whose sole current selection already lies in hitID's
//     ancestor chain, walks up one level to that object's parent (reaching the instance root and
//     eventually cycling back to hitID from the top-level ancestor).
func (e *Editor) resolveHierarchicalPick3D(hitID string, alt bool) string {
	if e == nil || hitID == "" {
		return ""
	}
	if alt {
		if info := e.ObjectOverrideInfo(hitID); info.Origin == ObjectInstanced && info.InstanceID != "" {
			return info.InstanceID
		}
		path := e.resolvedObjectPath(hitID)
		if len(path) >= 2 {
			return path[len(path)-2].ID
		}
		return hitID
	}
	if e.SelectionCount() == 1 && e.SelectedID != "" {
		path := e.resolvedObjectPath(hitID)
		if len(path) >= 2 {
			for i := len(path) - 1; i >= 0; i-- {
				if path[i].ID == e.SelectedID {
					if i > 0 {
						return path[i-1].ID
					}
					return hitID
				}
			}
		}
	}
	return hitID
}

// isHierarchicalCyclePress3D reports whether a plain press at (sx, sy) hits a nested child whose
// ancestor chain already contains the sole selected object, so the press should step selection up
// the hierarchy instead of being absorbed by the gizmo sitting at the selected object's origin.
func (e *Editor) isHierarchicalCyclePress3D(sx, sy, w, h float32) bool {
	if e == nil || e.SelectionCount() != 1 || e.SelectedID == "" {
		return false
	}
	hit, ok := e.Pick3DObject(sx, sy, w, h)
	if !ok || hit == "" {
		return false
	}
	path := e.resolvedObjectPath(hit)
	if len(path) < 2 {
		return false
	}
	for _, anc := range path {
		if anc.ID == e.SelectedID {
			return true
		}
	}
	return false
}

// Select3DAt picks the deepest closest 3D object under (sx, sy) and selects it (stepping up to its
// parent or instance root on a second press when already selected), or clears selection on empty space.
func (e *Editor) Select3DAt(sx, sy, w, h float32) bool {
	return e.Select3DAtWithModifiers(sx, sy, w, h, false, false, false)
}

// Select3DAtWith picks the deepest closest 3D object under (sx, sy) and applies selection with
// optional Shift (add) and Mod (toggle) modifiers.
func (e *Editor) Select3DAtWith(sx, sy, w, h float32, shift, mod bool) bool {
	return e.Select3DAtWithModifiers(sx, sy, w, h, shift, mod, false)
}

// Select3DAtWithModifiers picks the deepest closest 3D object under (sx, sy) and applies selection
// with optional Shift (add), Mod (toggle), and Alt/Option (pick parent or owning scene instance root)
// modifiers. A repeated plain press on an already-selected nested child steps selection up to its
// parent (and instance root) before cycling back to the child.
func (e *Editor) Select3DAtWithModifiers(sx, sy, w, h float32, shift, mod, alt bool) bool {
	if e == nil {
		return false
	}
	if id, ok := e.Pick3DObject(sx, sy, w, h); ok {
		target := id
		if alt {
			target = e.resolveHierarchicalPick3D(id, true)
		} else if !shift && !mod {
			target = e.resolveHierarchicalPick3D(id, false)
		}
		switch {
		case mod:
			e.ToggleSelection(target)
		case shift:
			e.AddToSelection(target)
		default:
			e.SelectObject(target)
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
		obj, _ := e.FindResolvedObject(draggable[0])
		if obj == nil {
			return [3]float32{}, false
		}
		return e.worldPosition3D(obj), true
	}
	hasAny := false
	var bmin, bmax [3]float32
	for _, id := range draggable {
		obj, _ := e.FindResolvedObject(id)
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
	return e.Projection3D(0, 0).GizmoLength(math3d.Vec3{X: pos[0], Y: pos[1], Z: pos[2]})
}

func (e *Editor) gizmo3DBlocked() bool {
	return e == nil || e.playing || e.HistoryPreviewActive() || e.gridMap.tool != GridMapToolNone || (e.TerrainOpen() && e.terrainBrush.Mode != TerrainBrushNone)
}

type mat3 [3][3]float64

var identMat3 = mat3{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}

func isFinite3F64(v [3]float64) bool {
	return !math.IsNaN(v[0]) && !math.IsInf(v[0], 0) &&
		!math.IsNaN(v[1]) && !math.IsInf(v[1], 0) &&
		!math.IsNaN(v[2]) && !math.IsInf(v[2], 0)
}

// orthonormalizeBasis3 applies Gram-Schmidt to produce a right-handed orthonormal basis.
// Returns ok=false if any input is non-finite or the axes are degenerate/collinear.
func orthonormalizeBasis3(bx, by, bz [3]float64) (ox, oy, oz [3]float64, ok bool) {
	if !isFinite3F64(bx) || !isFinite3F64(by) || !isFinite3F64(bz) {
		return [3]float64{}, [3]float64{}, [3]float64{}, false
	}
	lxLen := len64(bx)
	if !(lxLen >= 1e-6) {
		return [3]float64{}, [3]float64{}, [3]float64{}, false
	}
	ox = scale64(bx, 1.0/lxLen)

	byProj := sub64(by, scale64(ox, dot64(by, ox)))
	lyLen := len64(byProj)
	if !(lyLen >= 1e-6) {
		return [3]float64{}, [3]float64{}, [3]float64{}, false
	}
	oy = scale64(byProj, 1.0/lyLen)

	// Right-handed Z axis; ensure input bz was not degenerate (zero-scale along Z).
	if !(len64(bz) >= 1e-6) {
		return [3]float64{}, [3]float64{}, [3]float64{}, false
	}
	cz := cross64(ox, oy)
	lzLen := len64(cz)
	if !(lzLen >= 1e-6) {
		return [3]float64{}, [3]float64{}, [3]float64{}, false
	}
	oz = scale64(cz, 1.0/lzLen)
	if !isFinite3F64(ox) || !isFinite3F64(oy) || !isFinite3F64(oz) {
		return [3]float64{}, [3]float64{}, [3]float64{}, false
	}
	return ox, oy, oz, true
}

func mat3FromBasis(ax, ay, az [3]float64) mat3 {
	return mat3{
		{ax[0], ay[0], az[0]},
		{ax[1], ay[1], az[1]},
		{ax[2], ay[2], az[2]},
	}
}

func mat3FromEulerXYZ(rx, ry, rz float64) mat3 {
	return mat3FromBasis(
		rotateEulerXYZ64([3]float64{1, 0, 0}, rx, ry, rz),
		rotateEulerXYZ64([3]float64{0, 1, 0}, rx, ry, rz),
		rotateEulerXYZ64([3]float64{0, 0, 1}, rx, ry, rz),
	)
}

// eulerXYZFromMat3 decomposes a rotation matrix matching Rz * Ry * Rx (rotateEulerXYZ64) back to (rx, ry, rz).
func eulerXYZFromMat3(r mat3) (rx, ry, rz float64) {
	s := -r[2][0]
	if s > 1 {
		s = 1
	} else if s < -1 {
		s = -1
	}
	ry = math.Asin(s)
	if math.Abs(math.Cos(ry)) > 1e-6 {
		rx = math.Atan2(r[2][1], r[2][2])
		rz = math.Atan2(r[1][0], r[0][0])
	} else {
		rx = math.Atan2(-r[1][2], r[1][1])
		rz = 0
	}
	clean := func(v float64) float64 {
		if math.Abs(v) < 1e-7 {
			return 0
		}
		return v
	}
	return clean(rx), clean(ry), clean(rz)
}

func mat3FromAxisAngle(axis [3]float64, angle float64) mat3 {
	if !isFinite3F64(axis) || math.IsNaN(angle) || math.IsInf(angle, 0) {
		return identMat3
	}
	al := len64(axis)
	if !(al >= 1e-9) {
		return identMat3
	}
	kx, ky, kz := axis[0]/al, axis[1]/al, axis[2]/al
	s, c := math.Sincos(angle)
	t := 1 - c
	return mat3{
		{t*kx*kx + c, t*kx*ky - s*kz, t*kx*kz + s*ky},
		{t*kx*ky + s*kz, t*ky*ky + c, t*ky*kz - s*kx},
		{t*kx*kz - s*ky, t*ky*kz + s*kx, t*kz*kz + c},
	}
}

func mat3Mul(a, b mat3) mat3 {
	var out mat3
	for r := range 3 {
		for c := range 3 {
			out[r][c] = a[r][0]*b[0][c] + a[r][1]*b[1][c] + a[r][2]*b[2][c]
		}
	}
	return out
}

func mat3Transpose(a mat3) mat3 {
	return mat3{
		{a[0][0], a[1][0], a[2][0]},
		{a[0][1], a[1][1], a[2][1]},
		{a[0][2], a[1][2], a[2][2]},
	}
}

func mat3IsIdentity(a mat3) bool {
	for r := range 3 {
		for c := range 3 {
			want := 0.0
			if r == c {
				want = 1.0
			}
			if math.Abs(a[r][c]-want) > 1e-6 {
				return false
			}
		}
	}
	return true
}

func parentRotationMat3(parent aff3) (mat3, bool) {
	bx := [3]float64{parent[0][0], parent[1][0], parent[2][0]}
	by := [3]float64{parent[0][1], parent[1][1], parent[2][1]}
	bz := [3]float64{parent[0][2], parent[1][2], parent[2][2]}
	ox, oy, oz, ok := orthonormalizeBasis3(bx, by, bz)
	if !ok {
		return identMat3, false
	}
	return mat3FromBasis(ox, oy, oz), true
}

// parentRotation3D returns the right-handed orthonormal parent world rotation for id.
// It uses Gram-Schmidt on parentWorld so non-uniform and mirrored parents are handled cleanly,
// and falls back to composing only finite ancestor Euler rotations if an ancestor has non-finite rotation.
func (e *Editor) parentRotation3D(id string, parentWorld aff3) mat3 {
	if r, ok := parentRotationMat3(parentWorld); ok {
		return r
	}
	path := e.objectChain3D(id)
	if len(path) < 2 {
		return identMat3
	}
	r := identMat3
	for _, o := range path[:len(path)-1] {
		rx, ry, rz := float64(o.RotX), float64(o.RotY), float64(o.RotZ)
		if !isFinite3F64([3]float64{rx, ry, rz}) {
			continue
		}
		r = mat3Mul(r, mat3FromEulerXYZ(rx, ry, rz))
	}
	return r
}

// composeEulerRotation3D rotates an object with start Euler angles (rx0, ry0, rz0) under parent
// rotation parentRot around a world-space unit axisDir by angle radians, returning updated XYZ Euler angles.
func composeEulerRotation3D(rx0, ry0, rz0 float64, parentRot mat3, axisDir [3]float64, angle float64) (float32, float32, float32, bool) {
	if !isFinite3F64(axisDir) || math.IsNaN(angle) || math.IsInf(angle, 0) {
		return 0, 0, 0, false
	}
	if angle == 0 {
		fx, fy, fz := float32(rx0), float32(ry0), float32(rz0)
		if !isFiniteFloat32(fx) || !isFiniteFloat32(fy) || !isFiniteFloat32(fz) {
			return 0, 0, 0, false
		}
		return fx, fy, fz, true
	}
	// Preserve exact unwrapped single-axis rotation when parent is unrotated and the drag axis
	// coincides with the sole active Euler axis.
	if mat3IsIdentity(parentRot) {
		switch {
		case math.Abs(axisDir[0]-1) < 1e-6 && math.Abs(axisDir[1]) < 1e-6 && math.Abs(axisDir[2]) < 1e-6 && ry0 == 0 && rz0 == 0:
			rx := float32(rx0 + angle)
			return rx, 0, 0, isFiniteFloat32(rx)
		case math.Abs(axisDir[1]-1) < 1e-6 && math.Abs(axisDir[0]) < 1e-6 && math.Abs(axisDir[2]) < 1e-6 && rx0 == 0 && rz0 == 0:
			ry := float32(ry0 + angle)
			return 0, ry, 0, isFiniteFloat32(ry)
		case math.Abs(axisDir[2]-1) < 1e-6 && math.Abs(axisDir[0]) < 1e-6 && math.Abs(axisDir[1]) < 1e-6 && rx0 == 0 && ry0 == 0:
			rz := float32(rz0 + angle)
			return 0, 0, rz, isFiniteFloat32(rz)
		}
	}
	rStart := mat3FromEulerXYZ(rx0, ry0, rz0)
	rDelta := mat3FromAxisAngle(axisDir, angle)
	rWorld := mat3Mul(rDelta, mat3Mul(parentRot, rStart))
	rLocal := mat3Mul(mat3Transpose(parentRot), rWorld)
	rx, ry, rz := eulerXYZFromMat3(rLocal)
	fx, fy, fz := float32(rx), float32(ry), float32(rz)
	if !isFiniteFloat32(fx) || !isFiniteFloat32(fy) || !isFiniteFloat32(fz) {
		return 0, 0, 0, false
	}
	return fx, fy, fz, true
}

// gizmo3DAxes returns the world-space right-handed orthonormal X, Y, Z axes for the 3D transform gizmo:
// world axes in GizmoSpaceGlobal, or the primary selected object's world orientation in GizmoSpaceLocal.
func (e *Editor) gizmo3DAxes() (ax, ay, az [3]float64) {
	ax = [3]float64{1, 0, 0}
	ay = [3]float64{0, 1, 0}
	az = [3]float64{0, 0, 1}
	if e == nil || e.GizmoSpace3D != GizmoSpaceLocal {
		return ax, ay, az
	}
	draggable := e.draggableSelectedIDs()
	if len(draggable) == 0 {
		return ax, ay, az
	}
	id := draggable[0]
	if slices.Contains(draggable, e.SelectedID) {
		id = e.SelectedID
	}
	obj, _ := e.FindResolvedObject(id)
	if obj == nil {
		return ax, ay, az
	}
	obj = e.posed3D(obj)
	rx, ry, rz := float64(obj.RotX), float64(obj.RotY), float64(obj.RotZ)
	if !isFinite3F64([3]float64{rx, ry, rz}) {
		return ax, ay, az
	}
	parent := e.parentWorld3D(obj.ID)
	bx := parent.vector(rotateEulerXYZ64(ax, rx, ry, rz))
	by := parent.vector(rotateEulerXYZ64(ay, rx, ry, rz))
	bz := parent.vector(rotateEulerXYZ64(az, rx, ry, rz))
	if lx, ly, lz, ok := orthonormalizeBasis3(bx, by, bz); ok {
		return lx, ly, lz
	}
	// If an ancestor had a non-finite rotation (while the selected object itself is finite),
	// fall back to composing finite ancestor rotations + self rotation.
	if !isFinite3F64(bx) || !isFinite3F64(by) || !isFinite3F64(bz) {
		rWorld := mat3Mul(e.parentRotation3D(obj.ID, parent), mat3FromEulerXYZ(rx, ry, rz))
		cx := [3]float64{rWorld[0][0], rWorld[1][0], rWorld[2][0]}
		cy := [3]float64{rWorld[0][1], rWorld[1][1], rWorld[2][1]}
		cz := [3]float64{rWorld[0][2], rWorld[1][2], rWorld[2][2]}
		if lx, ly, lz, ok := orthonormalizeBasis3(cx, cy, cz); ok {
			return lx, ly, lz
		}
	}
	return ax, ay, az
}

// GizmoOrientation3D returns the active 3D transform gizmo's world-space orientation quaternion:
// identity in Global space, or the primary draggable selected object's orthonormalized world
// orientation (including its ancestors) in Local space.
func (e *Editor) GizmoOrientation3D() math3d.Quat {
	return e.gizmo3DOrientation().toMath3D()
}

// PickGizmo3D hit-tests the active 3D gizmo handles on the currently selected object(s)
// using render3d.Gizmo in the active Global or Local coordinate frame.
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
	origV := math3d.Vec3{X: origin[0], Y: origin[1], Z: origin[2]}
	proj := e.Projection3D(w, h)
	gizmoLen := proj.GizmoLength(origV)
	orient := e.gizmo3DOrientation()
	worldRay := proj.ScreenRay(sx, sy)
	localRay := toGizmoRay3D(origV, orient, worldRay)
	var g render3d.Gizmo
	r3Axis, _ := g.PickAxis(localRay, math3d.Vec3Zero(), gizmoLen, render3DGizmoMode(e.GizmoMode))
	return gizmoAxisFromRender3D(r3Axis)
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
	drag3DDolly
	drag3DTerrain
)

type drag3DTarget struct {
	id              string
	startTransform  Transform3D
	startPos        [3]float64
	parentInv       aff3
	parentRot       mat3
	overrideInfo    ObjectOverrideInfo
	overrideOld     sceneformat.ObjectOverride
	overrideHadOld  bool
	overrideWasNil  bool
	overridePrevVer int
}

type drag3DState struct {
	kind                 drag3DKind
	handle               Handle3DKind
	objectID             string
	targets              []drag3DTarget // non-empty when dragging >1 selected objects
	mode                 GizmoMode3D
	space                GizmoSpace3D
	axis                 GizmoAxis3D
	orient               quat64
	startRay             math3d.Ray
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
	parentRot          mat3
	startAxisT         float64
	hasAxisT           bool
	prevRingVec        [3]float64
	hasRingVec         bool
	accumAngle         float64
	deltaText          string
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

	// Override snapshot when dragging an instanced or inherited 3D object.
	overrideInfo    ObjectOverrideInfo
	overrideOld     sceneformat.ObjectOverride
	overrideHadOld  bool
	overrideWasNil  bool
	overridePrevVer int
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

// GizmoDragDelta3D returns the formatted live drag delta shown in the status bar while a 3D transform gizmo drag is active, or empty when idle.
func (e *Editor) GizmoDragDelta3D() string {
	if e == nil || e.drag3D.kind != drag3DGizmo {
		return ""
	}
	return e.drag3D.deltaText
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
		if err := e.checkAgentTouchLock(obj.ID); err != nil {
			e.report(err)
			return false
		}
		for _, hh := range e.ColliderHandles3D(obj) {
			if hh.Axis == axis && hh.Sign > 0 {
				return e.beginColliderDrag3D(obj, hh, sx, sy, w, h)
			}
		}
		return false
	}
	if !validGizmoAxisForMode3D(e.GizmoMode, axis) {
		return false
	}
	draggable := e.draggableSelectedIDs()
	if len(draggable) == 0 {
		return false
	}
	for _, id := range draggable {
		if err := e.checkAgentTouchLock(id); err != nil {
			e.report(err)
			return false
		}
	}
	pos, ok := e.gizmo3DOrigin()
	if !ok {
		return false
	}
	origV := math3d.Vec3{X: pos[0], Y: pos[1], Z: pos[2]}
	proj := e.Projection3D(w, h)
	gizmoLen := proj.GizmoLength(origV)
	orient := e.gizmo3DOrientation()
	startRay := proj.ScreenRay(sx, sy)
	var st drag3DState
	if len(draggable) == 1 {
		obj, _ := e.FindResolvedObject(draggable[0])
		if obj == nil || e.viewportFrozen(obj.ID) {
			return false
		}
		info := e.ObjectOverrideInfo(obj.ID)
		var (
			oldOv     sceneformat.ObjectOverride
			hadOld    bool
			wasNilMap bool
			prevVer   int
		)
		if info.Origin != ObjectLocal {
			if e.BuildErr != nil {
				e.report(e.BuildErr)
				return false
			}
			var ok bool
			oldOv, hadOld, wasNilMap, ok = e.currentOverrideFor(e.Doc, info)
			if !ok {
				return false
			}
			prevVer = e.Doc.Version
		}
		parentWorld := e.parentWorld3D(obj.ID)
		parentInv, ok := parentWorld.invert()
		if !ok {
			// A parent scaled to zero leaves no space to drag the object in.
			return false
		}
		startTr := captureTransform3D(obj)
		parentRot := e.parentRotation3D(obj.ID, parentWorld)
		st = drag3DState{
			kind:            drag3DGizmo,
			objectID:        obj.ID,
			mode:            e.GizmoMode,
			space:           e.GizmoSpace3D,
			axis:            axis,
			orient:          orient,
			startRay:        startRay,
			startSX:         sx,
			startSY:         sy,
			lastSX:          sx,
			lastSY:          sy,
			vpW:             w,
			vpH:             h,
			gizmoLen:        gizmoLen,
			startTransform:  startTr,
			startPos:        vec64(pos),
			parentInv:       parentInv,
			parentRot:       parentRot,
			overrideInfo:    info,
			overrideOld:     oldOv,
			overrideHadOld:  hadOld,
			overrideWasNil:  wasNilMap,
			overridePrevVer: prevVer,
		}
	} else {
		targets := make([]drag3DTarget, 0, len(draggable))
		for _, id := range draggable {
			obj, _ := e.FindResolvedObject(id)
			if obj == nil || e.viewportFrozen(id) {
				continue
			}
			info := e.ObjectOverrideInfo(id)
			var (
				oldOv     sceneformat.ObjectOverride
				hadOld    bool
				wasNilMap bool
				prevVer   int
			)
			if info.Origin != ObjectLocal {
				if e.BuildErr != nil {
					e.report(e.BuildErr)
					return false
				}
				var ok bool
				oldOv, hadOld, wasNilMap, ok = e.currentOverrideFor(e.Doc, info)
				if !ok {
					continue
				}
				prevVer = e.Doc.Version
			}
			parentWorld := e.parentWorld3D(id)
			parentInv, ok := parentWorld.invert()
			if !ok {
				continue
			}
			startTr := captureTransform3D(obj)
			parentRot := e.parentRotation3D(id, parentWorld)
			targets = append(targets, drag3DTarget{
				id:              id,
				startTransform:  startTr,
				startPos:        vec64(e.worldPosition3D(obj)),
				parentInv:       parentInv,
				parentRot:       parentRot,
				overrideInfo:    info,
				overrideOld:     oldOv,
				overrideHadOld:  hadOld,
				overrideWasNil:  wasNilMap,
				overridePrevVer: prevVer,
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
			space:    e.GizmoSpace3D,
			axis:     axis,
			orient:   orient,
			startRay: startRay,
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

	r0 := toGizmoRay3D(origV, orient, startRay)
	switch e.GizmoMode {
	case Gizmo3DRotate:
		axisDir := axisVector64(axis)
		if hit, ok := intersectRayPlaneEps64(vec3To64(r0.Origin), vec3To64(r0.Dir), [3]float64{}, axisDir, 1e-3); ok {
			if len64(hit) > 1e-4 {
				st.prevRingVec = normalize64(hit)
				st.hasRingVec = true
			}
		}
		st.deltaText = formatRotateDelta3D(st.axis, st.space, 0)
	case Gizmo3DScale:
		st.deltaText = formatScaleDelta3D(st.axis, st.space, 1)
	default:
		st.deltaText = formatTranslateDelta3D(st.axis, st.space, [3]float64{})
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
	obj, _ := e.FindResolvedObject(e.drag3D.objectID)
	if obj == nil {
		e.drag3D = drag3DState{}
		return
	}

	st := &e.drag3D
	if st.mode == Gizmo3DCollider {
		if localObj, _, _ := e.FindObject(e.drag3D.objectID); localObj != nil {
			e.updateColliderDrag3D(localObj, st, sx, sy)
		}
		return
	}
	w, h := st.vpW, st.vpH
	origV := math3d.Vec3{X: float32(st.startPos[0]), Y: float32(st.startPos[1]), Z: float32(st.startPos[2])}
	proj := e.Projection3D(w, h)
	curRay := proj.ScreenRay(sx, sy)
	r0 := toGizmoRay3D(origV, st.orient, st.startRay)
	r1 := toGizmoRay3D(origV, st.orient, curRay)

	next := st.startTransform
	switch st.mode {
	case Gizmo3DTranslate:
		worldDelta, displayDelta, ok := e.computeTranslateDelta3D(st, st.startPos, r0, r1)
		if !ok {
			return
		}
		local := st.parentInv.vector(worldDelta)
		nx, ny, nz := st.startTransform.X+float32(local[0]), st.startTransform.Y+float32(local[1]), st.startTransform.PosZ+float32(local[2])
		if !isFiniteFloat32(nx) || !isFiniteFloat32(ny) || !isFiniteFloat32(nz) {
			return
		}
		next.X, next.Y, next.PosZ = nx, ny, nz
		st.deltaText = formatTranslateDelta3D(st.axis, st.space, displayDelta)

	case Gizmo3DRotate:
		uLocal := axisVector64(st.axis)
		angle := e.computeGizmoRotationAngle3D(st, uLocal, r1, sx)
		if math.IsNaN(angle) || math.IsInf(angle, 0) {
			return
		}
		rx0, ry0, rz0 := float64(st.startTransform.RotX), float64(st.startTransform.RotY), float64(st.startTransform.RotZ)
		if e.rotationSnapActive() {
			if mat3IsIdentity(st.parentRot) && st.space == GizmoSpaceGlobal {
				switch {
				case st.axis == Axis3DX && ry0 == 0 && rz0 == 0:
					angle = e.SnapRotationRadians(rx0+angle) - rx0
				case st.axis == Axis3DY && rx0 == 0 && rz0 == 0:
					angle = e.SnapRotationRadians(ry0+angle) - ry0
				case st.axis == Axis3DZ && rx0 == 0 && ry0 == 0:
					angle = e.SnapRotationRadians(rz0+angle) - rz0
				default:
					angle = e.SnapRotationRadians(angle)
				}
			} else {
				angle = e.SnapRotationRadians(angle)
			}
			if math.IsNaN(angle) || math.IsInf(angle, 0) {
				return
			}
		}
		uWorld := st.orient.rotate(uLocal)
		rx, ry, rz, ok := composeEulerRotation3D(rx0, ry0, rz0, st.parentRot, uWorld, angle)
		if !ok {
			return
		}
		next.RotX, next.RotY, next.RotZ = rx, ry, rz
		st.deltaText = formatRotateDelta3D(st.axis, st.space, angle)

	case Gizmo3DScale:
		factor, ok := e.computeScaleFactor3D(st, proj, r0, r1)
		if !ok {
			return
		}
		if st.axis == Axis3DAll {
			if e.scaleSnapActive() {
				factor = e.SnapScaleFactor(factor)
			}
			factor = clampMinScale3D(factor)
			if math.IsNaN(factor) || math.IsInf(factor, 0) {
				return
			}
			bx := effectiveScaleValue3D(st.startTransform.ScaleX)
			by := effectiveScaleValue3D(st.startTransform.ScaleY)
			bz := effectiveScaleValue3D(st.startTransform.ScaleZ)
			sxv := float32(clampMinScale3D(bx * factor))
			syv := float32(clampMinScale3D(by * factor))
			szv := float32(clampMinScale3D(bz * factor))
			if !isFiniteFloat32(sxv) || !isFiniteFloat32(syv) || !isFiniteFloat32(szv) {
				return
			}
			next.ScaleX = sceneformat.Float32(sxv)
			next.ScaleY = sceneformat.Float32(syv)
			next.ScaleZ = sceneformat.Float32(szv)
			st.deltaText = formatScaleDelta3D(st.axis, st.space, factor)
		} else {
			applyAxis := func(startPtr *float32) (*float32, float64, bool) {
				base := effectiveScaleValue3D(startPtr)
				target := base * factor
				if e.scaleSnapActive() {
					target = e.SnapScaleFactor(target)
				}
				target = clampMinScale3D(target)
				v := float32(target)
				if !isFiniteFloat32(v) {
					return nil, factor, false
				}
				return sceneformat.Float32(v), target / base, true
			}
			switch st.axis {
			case Axis3DX:
				if p, ratio, ok := applyAxis(st.startTransform.ScaleX); ok {
					next.ScaleX = p
					st.deltaText = formatScaleDelta3D(st.axis, st.space, ratio)
				}
			case Axis3DY:
				if p, ratio, ok := applyAxis(st.startTransform.ScaleY); ok {
					next.ScaleY = p
					st.deltaText = formatScaleDelta3D(st.axis, st.space, ratio)
				}
			case Axis3DZ:
				if p, ratio, ok := applyAxis(st.startTransform.ScaleZ); ok {
					next.ScaleZ = p
					st.deltaText = formatScaleDelta3D(st.axis, st.space, ratio)
				}
			}
		}
	}

	if st.overrideInfo.Origin != ObjectLocal {
		nextOv := applyTransform3DOverride(st.overrideOld, st.mode, st.axis, st.startTransform, next)
		if e.Doc.Version < 2 && !isEmptyObjectOverride(nextOv) {
			e.Doc.Version = 2
		}
		setOverrideOnDoc(e.Doc, st.overrideInfo, nextOv, !isEmptyObjectOverride(nextOv), st.overrideWasNil)
	} else {
		applyTransform3D(obj, next)
	}

	st.lastSX = sx
	st.lastSY = sy
	e.Rebuild()
	e.notify()
}

// applyTransform3DOverride merges the fields changed by a 3D gizmo drag from start to next into
// base without spuriously overriding unchanged coordinates. Position changes are checked across
// all modes so multi-object Rotate and Scale drags preserve orbital/radial centroid movement on
// instanced or inherited objects.
func applyTransform3DOverride(base sceneformat.ObjectOverride, mode GizmoMode3D, axis GizmoAxis3D, start, next Transform3D) sceneformat.ObjectOverride {
	nextOv := cloneResolvedObjectOverride(base)
	if next.X != start.X {
		nextOv.X = sceneformat.Float32(next.X)
	}
	if next.Y != start.Y {
		nextOv.Y = sceneformat.Float32(next.Y)
	}
	if next.PosZ != start.PosZ {
		nextOv.PosZ = sceneformat.Float32(next.PosZ)
	}
	switch mode {
	case Gizmo3DRotate:
		if next.RotX != start.RotX {
			nextOv.RotX = sceneformat.Float32(next.RotX)
		}
		if next.RotY != start.RotY {
			nextOv.RotY = sceneformat.Float32(next.RotY)
		}
		if next.RotZ != start.RotZ {
			nextOv.RotZ = sceneformat.Float32(next.RotZ)
		}
	case Gizmo3DScale:
		switch axis {
		case Axis3DX:
			if !equalFloat32Ptr(next.ScaleX, start.ScaleX) {
				nextOv.ScaleX = cloneFloat32Ptr(next.ScaleX)
			}
		case Axis3DY:
			if !equalFloat32Ptr(next.ScaleY, start.ScaleY) {
				nextOv.ScaleY = cloneFloat32Ptr(next.ScaleY)
			}
		case Axis3DZ:
			if !equalFloat32Ptr(next.ScaleZ, start.ScaleZ) {
				nextOv.ScaleZ = cloneFloat32Ptr(next.ScaleZ)
			}
		case Axis3DAll:
			if !equalFloat32Ptr(next.ScaleX, start.ScaleX) {
				nextOv.ScaleX = cloneFloat32Ptr(next.ScaleX)
			}
			if !equalFloat32Ptr(next.ScaleY, start.ScaleY) {
				nextOv.ScaleY = cloneFloat32Ptr(next.ScaleY)
			}
			if !equalFloat32Ptr(next.ScaleZ, start.ScaleZ) {
				nextOv.ScaleZ = cloneFloat32Ptr(next.ScaleZ)
			}
		}
	}
	return nextOv
}

func (e *Editor) updateMultiGizmoDrag3D(sx, sy float32) {
	if !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return
	}
	st := &e.drag3D
	w, h := st.vpW, st.vpH
	startPos := st.startPos
	origV := math3d.Vec3{X: float32(startPos[0]), Y: float32(startPos[1]), Z: float32(startPos[2])}
	proj := e.Projection3D(w, h)
	curRay := proj.ScreenRay(sx, sy)
	r0 := toGizmoRay3D(origV, st.orient, st.startRay)
	r1 := toGizmoRay3D(origV, st.orient, curRay)

	objs := make([]*sceneformat.ObjectDoc, len(st.targets))
	nexts := make([]Transform3D, len(st.targets))
	for i, t := range st.targets {
		obj, _ := e.FindResolvedObject(t.id)
		if obj == nil {
			e.drag3D = drag3DState{}
			return
		}
		objs[i] = obj
		nexts[i] = t.startTransform
	}

	switch st.mode {
	case Gizmo3DTranslate:
		anchorPos := st.targets[0].startPos
		for _, t := range st.targets {
			if t.id == st.objectID {
				anchorPos = t.startPos
				break
			}
		}
		worldDelta, displayDelta, ok := e.computeTranslateDelta3D(st, anchorPos, r0, r1)
		if !ok {
			return
		}
		for i, t := range st.targets {
			local := t.parentInv.vector(worldDelta)
			nexts[i].X = t.startTransform.X + float32(local[0])
			nexts[i].Y = t.startTransform.Y + float32(local[1])
			nexts[i].PosZ = t.startTransform.PosZ + float32(local[2])
		}
		st.deltaText = formatTranslateDelta3D(st.axis, st.space, displayDelta)
	case Gizmo3DRotate:
		uLocal := axisVector64(st.axis)
		angle := e.computeGizmoRotationAngle3D(st, uLocal, r1, sx)
		if math.IsNaN(angle) || math.IsInf(angle, 0) {
			return
		}
		angle = e.SnapRotationRadians(angle)
		if math.IsNaN(angle) || math.IsInf(angle, 0) {
			return
		}
		uWorld := st.orient.rotate(uLocal)
		qDeltaWorld := quat64FromAxisAngle(uWorld, angle)
		for i, t := range st.targets {
			rel := sub64(t.startPos, startPos)
			rotRel := qDeltaWorld.rotate(rel)
			worldDelta := sub64(rotRel, rel)
			local := t.parentInv.vector(worldDelta)
			nexts[i].X = t.startTransform.X + float32(local[0])
			nexts[i].Y = t.startTransform.Y + float32(local[1])
			nexts[i].PosZ = t.startTransform.PosZ + float32(local[2])
			rx, ry, rz, ok := composeEulerRotation3D(
				float64(t.startTransform.RotX),
				float64(t.startTransform.RotY),
				float64(t.startTransform.RotZ),
				t.parentRot,
				uWorld,
				angle,
			)
			if !ok {
				return
			}
			nexts[i].RotX = rx
			nexts[i].RotY = ry
			nexts[i].RotZ = rz
		}
		st.deltaText = formatRotateDelta3D(st.axis, st.space, angle)
	case Gizmo3DScale:
		factor, ok := e.computeScaleFactor3D(st, proj, r0, r1)
		if !ok {
			return
		}
		if e.scaleSnapActive() {
			factor = e.SnapScaleFactor(factor)
		}
		factor = clampMinScale3D(factor)
		if math.IsNaN(factor) || math.IsInf(factor, 0) {
			return
		}
		for i, t := range st.targets {
			rel := sub64(t.startPos, startPos)
			var worldDelta [3]float64
			if st.axis == Axis3DAll {
				worldDelta = scale64(rel, factor-1)
			} else {
				uWorld := st.orient.rotate(axisVector64(st.axis))
				projLen := dot64(rel, uWorld)
				worldDelta = scale64(uWorld, projLen*(factor-1))
			}
			local := t.parentInv.vector(worldDelta)
			nexts[i].X = t.startTransform.X + float32(local[0])
			nexts[i].Y = t.startTransform.Y + float32(local[1])
			nexts[i].PosZ = t.startTransform.PosZ + float32(local[2])
			scaleAxis := func(startPtr *float32) (*float32, bool) {
				base := effectiveScaleValue3D(startPtr)
				sv := float32(clampMinScale3D(base * factor))
				if !isFiniteFloat32(sv) {
					return nil, false
				}
				return sceneformat.Float32(sv), true
			}
			switch st.axis {
			case Axis3DX:
				p, ok := scaleAxis(t.startTransform.ScaleX)
				if !ok {
					return
				}
				nexts[i].ScaleX = p
			case Axis3DY:
				p, ok := scaleAxis(t.startTransform.ScaleY)
				if !ok {
					return
				}
				nexts[i].ScaleY = p
			case Axis3DZ:
				p, ok := scaleAxis(t.startTransform.ScaleZ)
				if !ok {
					return
				}
				nexts[i].ScaleZ = p
			case Axis3DAll:
				px, okX := scaleAxis(t.startTransform.ScaleX)
				py, okY := scaleAxis(t.startTransform.ScaleY)
				pz, okZ := scaleAxis(t.startTransform.ScaleZ)
				if !okX || !okY || !okZ {
					return
				}
				nexts[i].ScaleX, nexts[i].ScaleY, nexts[i].ScaleZ = px, py, pz
			}
		}
		st.deltaText = formatScaleDelta3D(st.axis, st.space, factor)
	}

	for _, tr := range nexts {
		if !isFiniteFloat32(tr.X) || !isFiniteFloat32(tr.Y) || !isFiniteFloat32(tr.PosZ) ||
			!isFiniteFloat32(tr.RotX) || !isFiniteFloat32(tr.RotY) || !isFiniteFloat32(tr.RotZ) {
			return
		}
	}
	for i, obj := range objs {
		t := st.targets[i]
		if t.overrideInfo.Origin != ObjectLocal {
			nextOv := applyTransform3DOverride(t.overrideOld, st.mode, st.axis, t.startTransform, nexts[i])
			if e.Doc.Version < 2 && !isEmptyObjectOverride(nextOv) {
				e.Doc.Version = 2
			}
			setOverrideOnDoc(e.Doc, t.overrideInfo, nextOv, !isEmptyObjectOverride(nextOv), t.overrideWasNil)
		} else {
			applyTransform3D(obj, nexts[i])
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

// EndGizmoDrag3D finishes an active 3D gizmo drag and commits a single undoable command to History.
// Returns true if a transform change was committed. A drag that would shear a descendant's sprite is dropped, restoring the pre-drag transform.
func (e *Editor) EndGizmoDrag3D() bool {
	if e == nil || e.drag3D.kind != drag3DGizmo {
		return false
	}
	st := e.drag3D
	e.drag3D = drag3DState{}
	defer e.resumeAutoSaveAfterInteraction()
	if st.mode == Gizmo3DCollider {
		return e.endColliderDrag3D(st)
	}
	if len(st.targets) > 0 {
		type pending struct {
			target   drag3DTarget
			obj      *sceneformat.ObjectDoc
			after    Transform3D
			afterOv  sceneformat.ObjectOverride
			hasAfter bool
			nextVer  int
		}
		var changed []pending
		for _, t := range st.targets {
			if t.overrideInfo.Origin != ObjectLocal {
				afterOv, hasAfter, _, ok := e.currentOverrideFor(e.Doc, t.overrideInfo)
				e.Doc.Version = t.overridePrevVer
				setOverrideOnDoc(e.Doc, t.overrideInfo, t.overrideOld, t.overrideHadOld, t.overrideWasNil)
				if ok && !(t.overrideHadOld == hasAfter && reflect.DeepEqual(t.overrideOld, afterOv)) {
					nextVer := t.overridePrevVer
					if hasAfter && nextVer < 2 {
						nextVer = 2
					}
					changed = append(changed, pending{target: t, afterOv: afterOv, hasAfter: hasAfter, nextVer: nextVer})
				}
				continue
			}
			obj, _, _ := e.FindObject(t.id)
			if obj == nil {
				continue
			}
			after := captureTransform3D(obj)
			if !equalTransform3D(t.startTransform, after) {
				changed = append(changed, pending{target: t, obj: obj, after: after})
			}
		}
		if len(changed) == 0 {
			e.Rebuild()
			e.notify()
			return false
		}
		for _, p := range changed {
			if p.target.overrideInfo.Origin == ObjectLocal {
				applyTransform3D(p.obj, p.target.startTransform)
			}
		}
		candidate := e.Doc.Clone()
		for _, p := range changed {
			if p.target.overrideInfo.Origin != ObjectLocal {
				if p.hasAfter && candidate.Version < p.nextVer {
					candidate.Version = p.nextVer
				}
				setOverrideOnDoc(candidate, p.target.overrideInfo, p.afterOv, p.hasAfter, p.target.overrideWasNil)
			} else if loc := locateObject(candidate, p.target.id); loc.obj != nil {
				applyTransform3D(loc.obj, p.after)
			}
		}
		if _, err := e.checkCandidateScene(candidate); err != nil {
			e.Rebuild()
			e.notify()
			return false
		}
		label := fmt.Sprintf("%s 3D %d objects (%s)", st.mode.String(), len(changed), st.axis.String())
		_ = e.History.Group(label, func() error {
			for _, p := range changed {
				if p.target.overrideInfo.Origin != ObjectLocal {
					e.History.Execute(&objectOverrideCmd{
						ed:          e,
						info:        p.target.overrideInfo,
						label:       st.mode.String() + " 3D " + p.target.id + " (" + st.axis.String() + ")",
						oldOv:       p.target.overrideOld,
						hadOld:      p.target.overrideHadOld,
						wasNilMap:   p.target.overrideWasNil,
						newOv:       p.afterOv,
						hasNew:      p.hasAfter,
						prevVersion: p.target.overridePrevVer,
						nextVersion: p.nextVer,
					})
					continue
				}
				e.History.Execute(&transform3DCmd{
					ed:     e,
					id:     p.target.id,
					label:  st.mode.String() + " 3D " + p.target.id + " (" + st.axis.String() + ")",
					before: p.target.startTransform,
					after:  p.after,
				})
			}
			return nil
		})
		return true
	}

	if st.overrideInfo.Origin != ObjectLocal {
		afterOv, hasAfter, _, ok := e.currentOverrideFor(e.Doc, st.overrideInfo)
		e.Doc.Version = st.overridePrevVer
		setOverrideOnDoc(e.Doc, st.overrideInfo, st.overrideOld, st.overrideHadOld, st.overrideWasNil)
		if !ok || (st.overrideHadOld == hasAfter && reflect.DeepEqual(st.overrideOld, afterOv)) {
			e.Rebuild()
			e.notify()
			return false
		}
		candidate := e.Doc.Clone()
		nextVer := st.overridePrevVer
		if hasAfter && nextVer < 2 {
			nextVer = 2
		}
		candidate.Version = nextVer
		setOverrideOnDoc(candidate, st.overrideInfo, afterOv, hasAfter, st.overrideWasNil)
		if _, err := e.checkCandidateScene(candidate); err != nil {
			e.Rebuild()
			e.notify()
			return false
		}
		e.History.Execute(&objectOverrideCmd{
			ed:          e,
			info:        st.overrideInfo,
			label:       st.mode.String() + " 3D " + st.objectID + " (" + st.axis.String() + ")",
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
	defer e.resumeAutoSaveAfterInteraction()
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
			if t.overrideInfo.Origin != ObjectLocal {
				e.Doc.Version = t.overridePrevVer
				setOverrideOnDoc(e.Doc, t.overrideInfo, t.overrideOld, t.overrideHadOld, t.overrideWasNil)
				restored = true
				continue
			}
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
	if st.overrideInfo.Origin != ObjectLocal {
		e.Doc.Version = st.overridePrevVer
		setOverrideOnDoc(e.Doc, st.overrideInfo, st.overrideOld, st.overrideHadOld, st.overrideWasNil)
		e.Rebuild()
		e.notify()
		return
	}
	if obj, _, _ := e.FindObject(st.objectID); obj != nil {
		applyTransform3D(obj, st.startTransform)
		e.Rebuild()
	}
	e.notify()
}

// HandleViewport3DPress handles a click/tap in the 3D viewport: with a terrain brush active it applies a dab, with an active 3D plugin tool it calls Press3D with the camera pick ray, with the Place tool and a model selected in the asset browser (SelectedModel) it places the model where the press lands (PlaceModelAt3D), otherwise it picks the deepest closest 3D object (stepping up to parent/instance root on a repeated press) or clears selection.
func (e *Editor) HandleViewport3DPress(sx, sy, w, h float32) {
	e.HandleViewport3DPressWithModifiers(sx, sy, w, h, false, false, false)
}

// HandleViewport3DPressWith handles a click/tap in the 3D viewport with optional Shift (add)
// and Mod (toggle) modifiers.
func (e *Editor) HandleViewport3DPressWith(sx, sy, w, h float32, shift, mod bool) {
	e.HandleViewport3DPressWithModifiers(sx, sy, w, h, shift, mod, false)
}

// HandleViewport3DPressWithModifiers handles a click/tap in the 3D viewport with optional Shift (add),
// Mod (toggle), and Alt/Option (pick parent or owning scene instance root) modifiers.
func (e *Editor) HandleViewport3DPressWithModifiers(sx, sy, w, h float32, shift, mod, alt bool) {
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
	if !shift && !mod && !alt {
		if e.PickHandle3D(sx, sy, w, h) != Handle3DNone {
			return
		}
		if _, hit := e.PickComponentGizmo3D(sx, sy, w, h); hit {
			return
		}
		if e.PickGizmo3D(sx, sy, w, h) != Axis3DNone && !e.isHierarchicalCyclePress3D(sx, sy, w, h) {
			return
		}
	}
	e.Select3DAtWithModifiers(sx, sy, w, h, shift, mod, alt)
}

// BeginViewport3DDrag starts a terrain brush stroke (if a terrain brush is active and hits the terrain and !pan), an active 3D plugin tool drag (if OnDragBegin3D claims it and !pan), a light/camera handle drag, a 3D component gizmo handle drag, a 3D transform gizmo drag, or a camera orbit/pan drag. It does nothing while the camera flies (BeginFly3D).
func (e *Editor) BeginViewport3DDrag(sx, sy, w, h float32, pan bool) {
	if pan {
		e.beginViewport3DDragAction(sx, sy, w, h, Navigation3DActionPan, false)
		return
	}
	e.beginViewport3DDragAction(sx, sy, w, h, Navigation3DActionOrbit, true)
}

// beginViewport3DDragAction starts a 3D viewport drag for action. When allowTools is true,
// active tools, handles, and gizmos under (sx, sy) claim the drag first; otherwise or on a miss,
// action determines whether a camera orbit, pan, or dolly drag starts (Navigation3DActionNone
// leaves drag3D idle). It reports whether any drag began.
func (e *Editor) beginViewport3DDragAction(sx, sy, w, h float32, action Navigation3DAction, allowTools bool) bool {
	if e == nil || e.fly3D {
		return false
	}
	if allowTools && !e.playing && e.TerrainOpen() && e.terrainBrush.Mode != TerrainBrushNone {
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
			return true
		}
	}
	if allowTools && !e.playing && e.Tool == ToolPlugin && e.BeginPluginDrag3D(sx, sy, w, h) {
		return true
	}
	if allowTools && !e.playing {
		if handle := e.BeginHandleDrag3D(sx, sy, w, h); handle != Handle3DNone {
			return true
		}
		if _, ok := e.BeginComponentGizmoDrag3D(sx, sy, w, h); ok {
			return true
		}
		if axis := e.BeginGizmoDrag3D(sx, sy, w, h); axis != Axis3DNone {
			return true
		}
	}
	var kind drag3DKind
	switch action {
	case Navigation3DActionOrbit:
		kind = drag3DOrbit
	case Navigation3DActionPan:
		kind = drag3DPan
	case Navigation3DActionDolly:
		kind = drag3DDolly
	default:
		return false
	}
	if e.lookThroughCamera3D {
		e.lookThroughCamera3D = false
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
	return true
}

// UpdateViewport3DDrag advances the active terrain brush stroke, 3D plugin tool drag, 3D handle drag, 3D component gizmo drag, 3D transform gizmo drag, or camera orbit/pan/dolly drag.
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
	case drag3DDolly:
		dx := sx - e.drag3D.lastSX
		dy := sy - e.drag3D.lastSY
		e.drag3D.lastSX = sx
		e.drag3D.lastSY = sy
		if delta := dx + dy; delta != 0 && isFiniteFloat32(delta) {
			e.ZoomView3D(float32(math.Exp(float64(delta) * 0.005)))
		}
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

	// 5b. Spec viewport overlays (Agent working, Added, Changed, Removed, Focused change, Problem)
	// sharing DrawObjectOverlay and the top-left legend toggles with the 2D viewport.
	e.drawSceneOverlays3D(d, w, h)

	proj := e.Projection3D(w, h)

	// 6. Active 3D transform gizmo, light/camera handles, and component gizmo handles on the selected object (hidden while painting a GridMap or 3D Terrain brush).
	if !e.gizmo3DBlocked() && e.OverlayLegendKindEnabled(OverlayLegendSelection) {
		e.drawSceneHandles3D(d, w, h)
		e.drawGizmo3D(d, w, h)
		e.drawComponentGizmos3D(d, proj)
	}

	// 6b. Live agent change and agent-working overlays projected onto the 3D viewport.
	e.drawAgentLiveOverlays3D(d, w, h)

	// 7. Active 3D plugin tool overlay.
	if !e.playing {
		if t := e.activePluginTool(); t != nil && t.Draw3D != nil {
			d.Save()
			t.Draw3D(e, d, proj)
			d.Restore()
		}
	}
}

// background3D returns the scene's background color, or the Spec viewport surround token when none is set.
func (e *Editor) background3D() core.Color {
	bg := e.activeSpecPalette().Viewport
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
	spec := e.SpecPalette(nil)
	d.Save().SetStrokeColor(ui.Alpha(spec.TextSecondary, 0.55)).SetLineWidth(1).BeginPath()
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
	spec := e.activeSpecPalette()

	if !e.gpu3D {
		corners := [4][3]float32{
			{-extent, 0, -extent},
			{extent, 0, -extent},
			{extent, 0, extent},
			{-extent, 0, extent},
		}
		var sx, sy [4]float32
		allVis := true
		for i, c := range corners {
			px, py, _, vis := vc.project(c, w, h)
			if !vis {
				allVis = false
				break
			}
			sx[i], sy[i] = px, py
		}
		if allVis {
			d.Save().SetFillColor(specAlpha(spec.Viewport3DGround, 0.55)).BeginPath().
				MoveTo(sx[0], sy[0]).
				LineTo(sx[1], sy[1]).
				LineTo(sx[2], sy[2]).
				LineTo(sx[3], sy[3]).
				ClosePath().Fill().Restore()
		}
	}

	d.Save().SetStrokeColor(spec.Viewport3DGrid).SetLineWidth(1).BeginPath()
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

	spec := e.SpecPalette(nil)
	axes := []struct {
		end   [3]float32
		color core.Color
	}{
		{[3]float32{axisLen, 0, 0}, specAlpha(spec.AxisX, 0.9)},
		{[3]float32{0, axisLen, 0}, specAlpha(spec.AxisY, 0.9)},
		{[3]float32{0, 0, axisLen}, specAlpha(spec.AxisZ, 0.9)},
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

	doc := e.resolvedDoc()
	if doc == nil {
		return
	}
	var items []drawable3D
	for li := range doc.Layers {
		layer := &doc.Layers[li]
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

	spec := e.SpecPalette(nil)
	selVisible := !e.playing && !e.HistoryPreviewActive() && e.OverlayKindVisible(OverlayKindSelection)
	canvas := core.Size{Width: core.Points(w), Height: core.Points(h)}

	for _, item := range items {
		occluded := e.vp3d.occludedObjects[item.obj.ID]
		lod, hasLOD := e.vp3d.lodByObject[item.obj.ID]
		if item.wireframe {
			corners := e.objectCorners3DUnder(item.obj, item.parent)
			color := ui.Alpha(spec.TextMuted, 0.75)
			lineWidth := float32(1.25)
			if occluded {
				color = ui.Alpha(spec.TextFaint, 0.35)
			} else if e.ShowLODOverlay && hasLOD {
				color = lodColor3D(spec, lod)
			}
			if item.selected && selVisible {
				color = spec.Accent.Edge
				lineWidth = overlaySelectionWidth
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

		if item.selected && selVisible {
			if bounds, vis := e.projectObject3DBounds(item.obj, item.parent, vc, w, h); vis {
				name := item.obj.Name
				if name == "" {
					name = item.obj.ID
				}
				DrawObjectOverlay(d, spec, ObjectOverlay{
					Kind:       OverlayKindSelection,
					Label:      name,
					Bounds:     bounds,
					CanvasSize: canvas,
				})
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
	origV := math3d.Vec3{X: origin[0], Y: origin[1], Z: origin[2]}
	proj := e.Projection3D(w, h)
	gizmoLen := proj.GizmoLength(origV)
	orient := e.gizmo3DOrientation()
	gizmoMat := math3d.TRS(origV, orient.toMath3D(), math3d.Vec3One())
	activeAxis := e.ActiveGizmoAxis3D()
	activeR3 := render3DGizmoAxis(activeAxis)
	spec := e.SpecPalette(nil)
	centerRingCol := ui.Alpha(spec.TextPrimary, 0.9)
	activeCol := spec.Warning.Fg

	var g render3d.Gizmo
	var buf [128]render3d.GizmoSegment
	segs := g.AppendSegments(buf[:0], math3d.Vec3Zero(), gizmoLen, render3DGizmoMode(e.GizmoMode))

	if e.GizmoMode == Gizmo3DTranslate {
		drawTranslatePlaneFills3D(d, proj, gizmoMat, gizmoLen, activeAxis, spec)
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
			isActive := activeR3 != render3d.AxisNone && (seg.Axis == activeR3 || (e.GizmoMode == Gizmo3DScale && activeR3 == render3d.AxisAll))
			if isActive != wantActive {
				continue
			}
			wa := gizmoMat.TransformPoint(seg.A)
			wb := gizmoMat.TransformPoint(seg.B)
			x0, y0, x1, y1, vis := proj.ProjectSegment(wa, wb)
			if !vis {
				continue
			}
			col := seg.Color
			switch seg.Axis {
			case render3d.AxisX:
				col = spec.AxisX
			case render3d.AxisY:
				col = spec.AxisY
			case render3d.AxisZ:
				col = spec.AxisZ
			}
			lw := float32(2.0)
			if isActive {
				col = activeCol
				lw = 3.25
			}
			if !open || col != curColor || lw != curWidth {
				flush()
				curColor = col
				curWidth = lw
				d.SetStrokeColor(curColor).SetLineWidth(curWidth).BeginPath()
				open = true
			}
			d.MoveTo(x0, y0).LineTo(x1, y1)
		}
		flush()
	}
	drawPass(false)
	drawPass(true)
	d.Restore()

	if e.GizmoMode == Gizmo3DTranslate {
		if cx, cy, _, vis := proj.Project(origV); vis {
			col := centerRingCol
			lw := float32(1.75)
			r := float32(6)
			if activeAxis == Axis3DAll {
				col = activeCol
				lw = 2.5
				r = 7
			}
			d.Save().SetStrokeColor(col).SetLineWidth(lw).BeginPath().Arc(cx, cy, r, 0, float32(2*math.Pi), false).Stroke().Restore()
		}
	}
}

func drawTranslatePlaneFills3D(d *core.Drawing, proj Projection3D, gizmoMat math3d.Mat4, gizmoLen float32, activeAxis GizmoAxis3D, spec SpecPalette) {
	const loFrac, hiFrac float32 = 0.05, 0.32
	lo, hi := loFrac*gizmoLen, hiFrac*gizmoLen
	fillAlpha := float32(0.22)
	activeAlpha := float32(0.42)
	if !spec.Dark {
		fillAlpha = 0.32
		activeAlpha = 0.45
	}
	planes := [3]struct {
		axis  GizmoAxis3D
		u, v  math3d.Vec3
		color core.Color
	}{
		{Axis3DXY, math3d.Vec3UnitX(), math3d.Vec3UnitY(), specAlpha(spec.Warning.Fg, fillAlpha)},
		{Axis3DXZ, math3d.Vec3UnitX(), math3d.Vec3UnitZ(), specAlpha(spec.Agent.Fg, fillAlpha)},
		{Axis3DYZ, math3d.Vec3UnitY(), math3d.Vec3UnitZ(), specAlpha(spec.Accent.Fg, fillAlpha)},
	}
	for _, pl := range planes {
		corner := func(a, b float32) (float32, float32, bool) {
			wp := gizmoMat.TransformPoint(pl.u.Scale(a).Add(pl.v.Scale(b)))
			x, y, _, vis := proj.Project(wp)
			return x, y, vis
		}
		x0, y0, v0 := corner(lo, lo)
		x1, y1, v1 := corner(hi, lo)
		x2, y2, v2 := corner(hi, hi)
		x3, y3, v3 := corner(lo, hi)
		if !v0 || !v1 || !v2 || !v3 {
			continue
		}
		col := pl.color
		if activeAxis == pl.axis {
			col = ui.Alpha(spec.Warning.Fg, activeAlpha)
		}
		d.Save().SetFillColor(col).BeginPath().
			MoveTo(x0, y0).LineTo(x1, y1).LineTo(x2, y2).LineTo(x3, y3).
			ClosePath().Fill().Restore()
	}
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
	return intersectRayPlaneEps64(rayOrig, rayDir, planePoint, planeNormal, 0.12)
}

func intersectRayPlaneEps64(rayOrig, rayDir, planePoint, planeNormal [3]float64, minDenom float64) ([3]float64, bool) {
	denom := dot64(rayDir, planeNormal)
	if math.Abs(denom) < minDenom {
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

// --- 3D transform gizmo quaternion, orientation, and render3d.Gizmo helpers (#892) ---

type quat64 struct {
	x, y, z, w float64
}

var quat64Identity = quat64{w: 1}

func (q quat64) norm() quat64 {
	l := math.Sqrt(q.x*q.x + q.y*q.y + q.z*q.z + q.w*q.w)
	if l < 1e-12 || math.IsNaN(l) || math.IsInf(l, 0) {
		return quat64Identity
	}
	inv := 1 / l
	return quat64{q.x * inv, q.y * inv, q.z * inv, q.w * inv}
}

func (q quat64) conj() quat64 {
	return quat64{-q.x, -q.y, -q.z, q.w}
}

func (q quat64) mul(b quat64) quat64 {
	return quat64{
		x: q.w*b.x + q.x*b.w + q.y*b.z - q.z*b.y,
		y: q.w*b.y - q.x*b.z + q.y*b.w + q.z*b.x,
		z: q.w*b.z + q.x*b.y - q.y*b.x + q.z*b.w,
		w: q.w*b.w - q.x*b.x - q.y*b.y - q.z*b.z,
	}
}

func (q quat64) rotate(v [3]float64) [3]float64 {
	n := q.norm()
	u := [3]float64{n.x, n.y, n.z}
	t := scale64(cross64(u, v), 2)
	return add64(add64(v, scale64(t, n.w)), cross64(u, t))
}

func (q quat64) toMath3D() math3d.Quat {
	n := q.norm()
	return math3d.Quat{X: float32(n.x), Y: float32(n.y), Z: float32(n.z), W: float32(n.w)}
}

func quat64FromAxisAngle(axis [3]float64, rad float64) quat64 {
	a := normalize64(axis)
	if len64(a) < 1e-12 {
		return quat64Identity
	}
	s, c := math.Sincos(rad * 0.5)
	return quat64{x: a[0] * s, y: a[1] * s, z: a[2] * s, w: c}
}

// quat64FromMat3 converts a right-handed orthonormal 3x3 rotation matrix to a unit quaternion.
func quat64FromMat3(r mat3) quat64 {
	tr := r[0][0] + r[1][1] + r[2][2]
	var q quat64
	switch {
	case tr > 0:
		s := math.Sqrt(tr+1.0) * 2
		q = quat64{
			x: (r[2][1] - r[1][2]) / s,
			y: (r[0][2] - r[2][0]) / s,
			z: (r[1][0] - r[0][1]) / s,
			w: 0.25 * s,
		}
	case r[0][0] > r[1][1] && r[0][0] > r[2][2]:
		s := math.Sqrt(1.0+r[0][0]-r[1][1]-r[2][2]) * 2
		q = quat64{
			x: 0.25 * s,
			y: (r[0][1] + r[1][0]) / s,
			z: (r[0][2] + r[2][0]) / s,
			w: (r[2][1] - r[1][2]) / s,
		}
	case r[1][1] > r[2][2]:
		s := math.Sqrt(1.0+r[1][1]-r[0][0]-r[2][2]) * 2
		q = quat64{
			x: (r[0][1] + r[1][0]) / s,
			y: 0.25 * s,
			z: (r[1][2] + r[2][1]) / s,
			w: (r[0][2] - r[2][0]) / s,
		}
	default:
		s := math.Sqrt(1.0+r[2][2]-r[0][0]-r[1][1]) * 2
		q = quat64{
			x: (r[0][2] + r[2][0]) / s,
			y: (r[1][2] + r[2][1]) / s,
			z: 0.25 * s,
			w: (r[1][0] - r[0][1]) / s,
		}
	}
	return q.norm()
}

func (e *Editor) objectChain3D(id string) []*sceneformat.ObjectDoc {
	if e.clipScene != nil {
		if bo := e.clipScene.Object(id); bo != nil {
			var path []*sceneformat.ObjectDoc
			for p := bo; p != nil; p = p.Parent {
				if p.Doc != nil {
					path = append(path, p.Doc)
				}
			}
			slices.Reverse(path)
			return path
		}
	}
	return e.resolvedObjectPath(id)
}

func (e *Editor) gizmo3DOrientation() quat64 {
	if e == nil || e.GizmoSpace3D != GizmoSpaceLocal {
		return quat64Identity
	}
	ax, ay, az := e.gizmo3DAxes()
	return quat64FromMat3(mat3FromBasis(ax, ay, az))
}

func render3DGizmoMode(m GizmoMode3D) render3d.GizmoMode {
	switch m {
	case Gizmo3DRotate:
		return render3d.GizmoRotate
	case Gizmo3DScale:
		return render3d.GizmoScale
	default:
		return render3d.GizmoTranslate
	}
}

func gizmoAxisFromRender3D(a render3d.GizmoAxis) GizmoAxis3D {
	switch a {
	case render3d.AxisX:
		return Axis3DX
	case render3d.AxisY:
		return Axis3DY
	case render3d.AxisZ:
		return Axis3DZ
	case render3d.AxisXY:
		return Axis3DXY
	case render3d.AxisXZ:
		return Axis3DXZ
	case render3d.AxisYZ:
		return Axis3DYZ
	case render3d.AxisAll:
		return Axis3DAll
	default:
		return Axis3DNone
	}
}

func render3DGizmoAxis(a GizmoAxis3D) render3d.GizmoAxis {
	switch a {
	case Axis3DX:
		return render3d.AxisX
	case Axis3DY:
		return render3d.AxisY
	case Axis3DZ:
		return render3d.AxisZ
	case Axis3DXY:
		return render3d.AxisXY
	case Axis3DXZ:
		return render3d.AxisXZ
	case Axis3DYZ:
		return render3d.AxisYZ
	case Axis3DAll:
		return render3d.AxisAll
	default:
		return render3d.AxisNone
	}
}

func validGizmoAxisForMode3D(mode GizmoMode3D, axis GizmoAxis3D) bool {
	switch mode {
	case Gizmo3DRotate:
		return axis == Axis3DX || axis == Axis3DY || axis == Axis3DZ
	case Gizmo3DScale:
		return axis == Axis3DX || axis == Axis3DY || axis == Axis3DZ || axis == Axis3DAll
	case Gizmo3DTranslate:
		return axis >= Axis3DX && axis <= Axis3DAll
	default:
		return false
	}
}

func toGizmoRay3D(origin math3d.Vec3, orient quat64, ray math3d.Ray) math3d.Ray {
	inv := orient.conj()
	relOrig := [3]float64{
		float64(ray.Origin.X - origin.X),
		float64(ray.Origin.Y - origin.Y),
		float64(ray.Origin.Z - origin.Z),
	}
	rotOrig := inv.rotate(relOrig)
	rotDir := inv.rotate([3]float64{float64(ray.Dir.X), float64(ray.Dir.Y), float64(ray.Dir.Z)})
	return math3d.Ray{
		Origin: math3d.Vec3{X: float32(rotOrig[0]), Y: float32(rotOrig[1]), Z: float32(rotOrig[2])},
		Dir:    math3d.Vec3{X: float32(rotDir[0]), Y: float32(rotDir[1]), Z: float32(rotDir[2])},
	}
}

const minGizmoPlaneCos3D = 0.02

func gizmoPlaneNormal3D(axis GizmoAxis3D) ([3]float64, bool) {
	switch axis {
	case Axis3DXY:
		return [3]float64{0, 0, 1}, true
	case Axis3DXZ:
		return [3]float64{0, 1, 0}, true
	case Axis3DYZ:
		return [3]float64{1, 0, 0}, true
	default:
		return [3]float64{}, false
	}
}

func maxGizmoJump3D(gizmoLen float32) float64 {
	return math.Max(1000, 200*float64(gizmoLen))
}

func clampMinScale3D(v float64) float64 {
	if math.Abs(v) < minScale2D {
		if v < 0 {
			return -minScale2D
		}
		return minScale2D
	}
	return v
}

func (e *Editor) computeTranslateDelta3D(st *drag3DState, anchorPos [3]float64, r0, r1 math3d.Ray) (worldDelta, displayDelta [3]float64, ok bool) {
	o0 := vec3To64(r0.Origin)
	d0 := normalize64(vec3To64(r0.Dir))
	o1 := vec3To64(r1.Origin)
	d1 := normalize64(vec3To64(r1.Dir))
	if !isFinite3F64(o0) || !isFinite3F64(o1) || len64(d0) < 1e-9 || len64(d1) < 1e-9 {
		return [3]float64{}, [3]float64{}, false
	}

	var gizmoDelta [3]float64
	switch st.axis {
	case Axis3DX, Axis3DY, Axis3DZ:
		u := axisVector64(st.axis)
		s0, ok0 := closestParamOnAxisLine64([3]float64{}, u, o0, d0)
		s1, ok1 := closestParamOnAxisLine64([3]float64{}, u, o1, d1)
		if !ok0 || !ok1 {
			return [3]float64{}, [3]float64{}, false
		}
		gizmoDelta = scale64(u, s1-s0)
	case Axis3DXY, Axis3DXZ, Axis3DYZ:
		n, _ := gizmoPlaneNormal3D(st.axis)
		p0, ok0 := intersectRayPlaneEps64(o0, d0, [3]float64{}, n, minGizmoPlaneCos3D)
		p1, ok1 := intersectRayPlaneEps64(o1, d1, [3]float64{}, n, minGizmoPlaneCos3D)
		if !ok0 || !ok1 {
			return [3]float64{}, [3]float64{}, false
		}
		diff := sub64(p1, p0)
		gizmoDelta = sub64(diff, scale64(n, dot64(diff, n)))
	case Axis3DAll:
		n := scale64(d0, -1)
		p0, ok0 := intersectRayPlaneEps64(o0, d0, [3]float64{}, n, minGizmoPlaneCos3D)
		p1, ok1 := intersectRayPlaneEps64(o1, d1, [3]float64{}, n, minGizmoPlaneCos3D)
		if !ok0 || !ok1 {
			return [3]float64{}, [3]float64{}, false
		}
		gizmoDelta = sub64(p1, p0)
	default:
		return [3]float64{}, [3]float64{}, false
	}

	if !isFinite3F64(gizmoDelta) || len64(gizmoDelta) > maxGizmoJump3D(st.gizmoLen) {
		return [3]float64{}, [3]float64{}, false
	}

	grid, pixel := e.positionSnapMode()
	if !grid && !pixel {
		worldDelta = st.orient.rotate(gizmoDelta)
		return worldDelta, gizmoDelta, true
	}
	// In Local mode on constrained axis/plane handles, snap displacements along the active local axes
	// so the object moves strictly along the drawn local handle in exact step increments.
	if st.space == GizmoSpaceLocal && st.axis != Axis3DAll {
		var step float64 = 1
		if grid {
			step = float64(e.EffectiveGridSize())
		}
		active := gizmoActiveComponents3D(st.axis)
		for c := range 3 {
			if active[c] {
				gizmoDelta[c] = snapStep64(gizmoDelta[c], step)
			} else {
				gizmoDelta[c] = 0
			}
		}
		worldDelta = st.orient.rotate(gizmoDelta)
		return worldDelta, gizmoDelta, true
	}
	// In Global mode (or screen-space center handle), snap active world coordinates to the grid.
	worldDelta = st.orient.rotate(gizmoDelta)
	active := gizmoActiveComponents3D(st.axis)
	for c := range 3 {
		if !active[c] {
			continue
		}
		snapped := e.SnapValue(anchorPos[c] + worldDelta[c])
		if math.IsNaN(snapped) || math.IsInf(snapped, 0) {
			return [3]float64{}, [3]float64{}, false
		}
		worldDelta[c] = snapped - anchorPos[c]
	}
	displayDelta = st.orient.conj().rotate(worldDelta)
	return worldDelta, displayDelta, true
}

func gizmoActiveComponents3D(axis GizmoAxis3D) [3]bool {
	switch axis {
	case Axis3DX:
		return [3]bool{true, false, false}
	case Axis3DY:
		return [3]bool{false, true, false}
	case Axis3DZ:
		return [3]bool{false, false, true}
	case Axis3DXY:
		return [3]bool{true, true, false}
	case Axis3DXZ:
		return [3]bool{true, false, true}
	case Axis3DYZ:
		return [3]bool{false, true, true}
	case Axis3DAll:
		return [3]bool{true, true, true}
	default:
		return [3]bool{}
	}
}

func (e *Editor) computeGizmoRotationAngle3D(st *drag3DState, uLocal [3]float64, curGizmoRay math3d.Ray, sx float32) float64 {
	if st.hasRingVec {
		if hit, ok := intersectRayPlaneEps64(vec3To64(curGizmoRay.Origin), vec3To64(curGizmoRay.Dir), [3]float64{}, uLocal, 1e-3); ok {
			if len64(hit) > 1e-4 {
				cur := normalize64(hit)
				sinA := dot64(cross64(st.prevRingVec, cur), uLocal)
				cosA := dot64(st.prevRingVec, cur)
				st.accumAngle += math.Atan2(sinA, cosA)
				st.prevRingVec = cur
				return st.accumAngle
			}
		}
		return st.accumAngle
	}
	return float64(sx-st.startSX) * 0.01
}

func (e *Editor) computeScaleFactor3D(st *drag3DState, proj Projection3D, r0, r1 math3d.Ray) (float64, bool) {
	denom := float64(st.gizmoLen)
	if denom <= 1e-4 {
		denom = 1
	}
	o0 := vec3To64(r0.Origin)
	d0 := normalize64(vec3To64(r0.Dir))
	o1 := vec3To64(r1.Origin)
	d1 := normalize64(vec3To64(r1.Dir))
	if !isFinite3F64(o0) || !isFinite3F64(o1) || len64(d0) < 1e-9 || len64(d1) < 1e-9 {
		return 1, false
	}
	maxJump := maxGizmoJump3D(st.gizmoLen)
	var delta float64
	if st.axis == Axis3DAll {
		n := scale64(d0, -1)
		p0, ok0 := intersectRayPlaneEps64(o0, d0, [3]float64{}, n, minGizmoPlaneCos3D)
		p1, ok1 := intersectRayPlaneEps64(o1, d1, [3]float64{}, n, minGizmoPlaneCos3D)
		if !ok0 || !ok1 {
			return 1, false
		}
		gizmoDelta := sub64(p1, p0)
		if !isFinite3F64(gizmoDelta) || len64(gizmoDelta) > maxJump {
			return 1, false
		}
		worldDelta := st.orient.rotate(gizmoDelta)
		var right, up [3]float64
		if proj.hasView {
			right, up = proj.view.right, proj.view.up
		} else {
			right, up, _ = proj.Camera.basis64()
		}
		delta = dot64(worldDelta, right) + dot64(worldDelta, up)
	} else {
		u := axisVector64(st.axis)
		s0, ok0 := closestParamOnAxisLine64([3]float64{}, u, o0, d0)
		s1, ok1 := closestParamOnAxisLine64([3]float64{}, u, o1, d1)
		if !ok0 || !ok1 {
			return 1, false
		}
		delta = s1 - s0
		if math.IsNaN(delta) || math.IsInf(delta, 0) || math.Abs(delta) > maxJump {
			return 1, false
		}
	}
	factor := 1.0 + delta/denom
	if math.IsNaN(factor) || math.IsInf(factor, 0) {
		return 1, false
	}
	return factor, true
}

func effectiveScaleValue3D(ptr *float32) float64 {
	if ptr == nil || *ptr == 0 {
		return 1
	}
	return float64(*ptr)
}

func cleanZero64(v float64) float64 {
	if math.Abs(v) < 1e-6 {
		return 0
	}
	return v
}

func formatTranslateDelta3D(axis GizmoAxis3D, space GizmoSpace3D, d [3]float64) string {
	return fmt.Sprintf("Translate %s (%s): dx %+.2f, dy %+.2f, dz %+.2f",
		axis.String(), space.String(), cleanZero64(d[0]), cleanZero64(d[1]), cleanZero64(d[2]))
}

func formatRotateDelta3D(axis GizmoAxis3D, space GizmoSpace3D, rad float64) string {
	deg := cleanZero64(rad * 180.0 / math.Pi)
	return fmt.Sprintf("Rotate %s (%s): %+.1f°", axis.String(), space.String(), deg)
}

func formatScaleDelta3D(axis GizmoAxis3D, space GizmoSpace3D, factor float64) string {
	return fmt.Sprintf("Scale %s (%s): %.2fx", axis.String(), space.String(), cleanZero64(factor))
}
