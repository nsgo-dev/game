package physics3d

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
)

func vec3FromMath3d(v math3d.Vec3) Vec3 {
	return Vec3{X: v.X, Y: v.Y, Z: v.Z}
}

func quatFromMath3d(q math3d.Quat) Quat {
	return Quat{X: q.X, Y: q.Y, Z: q.Z, W: q.W}
}

func effectiveWorldMat4(m math3d.Mat4) math3d.Mat4 {
	if m == (math3d.Mat4{}) {
		return math3d.Mat4Identity()
	}
	return m
}

func mat4Det3x3(m math3d.Mat4) float32 {
	c0 := math3d.Vec3{X: m[0], Y: m[1], Z: m[2]}
	c1 := math3d.Vec3{X: m[4], Y: m[5], Z: m[6]}
	c2 := math3d.Vec3{X: m[8], Y: m[9], Z: m[10]}
	return c0.Dot(c1.Cross(c2))
}

func mat4AxisScales(m math3d.Mat4) (sx, sy, sz float32) {
	c0 := math3d.Vec3{X: m[0], Y: m[1], Z: m[2]}
	c1 := math3d.Vec3{X: m[4], Y: m[5], Z: m[6]}
	c2 := math3d.Vec3{X: m[8], Y: m[9], Z: m[10]}
	return c0.Len(), c1.Len(), c2.Len()
}

// appendGLTFPrimitiveTriangles appends world-transformed vertices and triangle
// indices for a single glTF primitive (indexed or non-indexed, supporting
// ModeTriangles, ModeTriangleStrip, and ModeTriangleFan). When flipWinding is
// true (negative determinant transform), triangle winding is reversed so face
// normals remain outward-pointing.
func appendGLTFPrimitiveTriangles(
	prim gltf.Primitive,
	world math3d.Mat4,
	flipWinding bool,
	verts []Vec3,
	indices []uint32,
) ([]Vec3, []uint32) {
	if len(prim.Positions) == 0 {
		return verts, indices
	}
	mode := prim.Mode
	if mode != gltf.ModeTriangles && mode != gltf.ModeTriangleStrip && mode != gltf.ModeTriangleFan {
		return verts, indices
	}

	baseVert := uint32(len(verts))
	for _, p := range prim.Positions {
		verts = append(verts, vec3FromMath3d(world.TransformPoint(p)))
	}

	numPos := uint32(len(prim.Positions))
	indexed := len(prim.Indices) > 0
	count := len(prim.Positions)
	if indexed {
		count = len(prim.Indices)
	}
	indexAt := func(k int) (uint32, bool) {
		if indexed {
			idx := prim.Indices[k]
			if idx >= numPos {
				return 0, false
			}
			return baseVert + idx, true
		}
		return baseVert + uint32(k), true
	}
	emitTri := func(a, b, c uint32) {
		if flipWinding {
			b, c = c, b
		}
		indices = append(indices, a, b, c)
	}

	switch mode {
	case gltf.ModeTriangles:
		for i := 0; i+2 < count; i += 3 {
			ia, okA := indexAt(i)
			ib, okB := indexAt(i + 1)
			ic, okC := indexAt(i + 2)
			if okA && okB && okC {
				emitTri(ia, ib, ic)
			}
		}
	case gltf.ModeTriangleStrip:
		for i := 0; i+2 < count; i++ {
			ia, okA := indexAt(i)
			ib, okB := indexAt(i + 1)
			ic, okC := indexAt(i + 2)
			if !okA || !okB || !okC {
				continue
			}
			if i%2 == 0 {
				emitTri(ia, ib, ic)
			} else {
				emitTri(ib, ia, ic)
			}
		}
	case gltf.ModeTriangleFan:
		if count < 3 {
			break
		}
		i0, ok0 := indexAt(0)
		if !ok0 {
			break
		}
		for i := 1; i+1 < count; i++ {
			ib, okB := indexAt(i)
			ic, okC := indexAt(i + 1)
			if okB && okC {
				emitTri(i0, ib, ic)
			}
		}
	}
	return verts, indices
}

// NewTriMeshFromGLTFPrimitive builds a static TriMeshCollider from a single
// glTF primitive (indexed or non-indexed, supporting Triangles, TriangleStrip,
// and TriangleFan modes), transforming vertices by worldTransform (an all-zero
// matrix is treated as identity).
func NewTriMeshFromGLTFPrimitive(prim gltf.Primitive, worldTransform math3d.Mat4) *TriMeshCollider {
	world := effectiveWorldMat4(worldTransform)
	flip := mat4Det3x3(world) < 0
	verts, indices := appendGLTFPrimitiveTriangles(prim, world, flip, nil, nil)
	return NewTriMeshCollider(verts, indices)
}

// NewTriMeshFromGLTFMesh builds a single static TriMeshCollider combining all
// triangle primitives of mesh, transformed by worldTransform (an all-zero
// matrix is treated as identity).
func NewTriMeshFromGLTFMesh(mesh gltf.Mesh, worldTransform math3d.Mat4) *TriMeshCollider {
	world := effectiveWorldMat4(worldTransform)
	flip := mat4Det3x3(world) < 0
	var verts []Vec3
	var indices []uint32
	for _, prim := range mesh.Primitives {
		verts, indices = appendGLTFPrimitiveTriangles(prim, world, flip, verts, indices)
	}
	return NewTriMeshCollider(verts, indices)
}

// NewTriMeshFromGLTFNode builds a static TriMeshCollider from doc.Nodes[nodeIndex]
// using the node's full WorldMatrix transform. Returns nil if doc is nil,
// nodeIndex is out of range, or the node references no valid mesh.
func NewTriMeshFromGLTFNode(doc *gltf.Document, nodeIndex int) *TriMeshCollider {
	if doc == nil || nodeIndex < 0 || nodeIndex >= len(doc.Nodes) {
		return nil
	}
	node := doc.Nodes[nodeIndex]
	if node.Mesh < 0 || node.Mesh >= len(doc.Meshes) {
		return nil
	}
	tm := NewTriMeshFromGLTFMesh(doc.Meshes[node.Mesh], node.WorldMatrix)
	tm.Tag = node.Name
	return tm
}

func gltfMeshLocalBounds(mesh gltf.Mesh) (math3d.AABB, bool) {
	bounds := math3d.AABBEmpty()
	hasPos := false
	for _, prim := range mesh.Primitives {
		for _, p := range prim.Positions {
			bounds = bounds.ExpandPoint(p)
			hasPos = true
		}
		if !hasPos && !prim.Bounds.Empty() {
			bounds = bounds.Union(prim.Bounds)
			hasPos = true
		}
	}
	return bounds, hasPos && !bounds.Empty()
}

// GLTFColliderOptions configures how colliders are built from a glTF document.
type GLTFColliderOptions struct {
	// UseScene selects doc.Scenes[Scene] when true. When false, the document's
	// default scene (doc.Scene, falling back to scene 0 or all nodes) is used.
	UseScene bool
	// Scene is the scene index inspected when UseScene is true.
	Scene int

	// IncludeUnmarked builds static TriMeshColliders for ordinary mesh nodes
	// that carry neither a collider name suffix nor extras.collider metadata.
	// When false (default), only annotated collider nodes produce colliders.
	IncludeUnmarked bool

	// Friction is the default Coulomb friction coefficient for generated
	// colliders when not overridden by extras.collider (<= 0 defaults to 0.5).
	Friction float32
	// Restitution is the default bounciness in [0, 1] when not overridden.
	Restitution float32
	// Layer is the default collision layer mask when not overridden (0 -> LayerAll).
	Layer LayerMask
	// Mask is the default collision filter mask when not overridden (0 -> LayerAll).
	Mask LayerMask
	// TagPrefix is optionally prepended to generated collider tags.
	TagPrefix string
}

// GLTFColliders holds the static primitive bodies and triangle-mesh colliders
// extracted from a glTF document.
type GLTFColliders struct {
	Bodies    []*Body
	TriMeshes []*TriMeshCollider
}

// AddToWorld registers every body and triangle-mesh collider in c into w,
// updating c.Bodies to reference the stored *Body pointers returned by w.AddBody.
func (c *GLTFColliders) AddToWorld(w *World) {
	if c == nil || w == nil {
		return
	}
	for i, b := range c.Bodies {
		if b == nil {
			continue
		}
		c.Bodies[i] = w.AddBody(*b)
	}
	for _, tm := range c.TriMeshes {
		if tm == nil {
			continue
		}
		w.AddTriMesh(tm)
	}
}

// RemoveFromWorld removes every registered body and triangle-mesh collider in c from w.
func (c *GLTFColliders) RemoveFromWorld(w *World) {
	if c == nil || w == nil {
		return
	}
	for _, b := range c.Bodies {
		if b != nil && b.ID > 0 {
			w.RemoveBody(b.ID)
		}
	}
	for _, tm := range c.TriMeshes {
		if tm != nil && tm.ID > 0 {
			w.RemoveTriMesh(tm.ID)
		}
	}
}

func resolveColliderProps(nc gltf.NodeCollider, opts GLTFColliderOptions) (tag string, friction, restitution float32, layer, mask LayerMask) {
	tag = nc.Tag
	if opts.TagPrefix != "" {
		tag = opts.TagPrefix + tag
	}
	friction = opts.Friction
	if friction <= 0 {
		friction = 0.5
	}
	if nc.HasFriction {
		friction = nc.Friction
	}
	restitution = clamp32(opts.Restitution, 0, 1)
	if nc.HasRestitution {
		restitution = clamp32(nc.Restitution, 0, 1)
	}
	layer = opts.Layer
	if nc.HasLayer {
		layer = LayerMask(nc.Layer)
	}
	mask = opts.Mask
	if nc.HasMask {
		mask = LayerMask(nc.Mask)
	}
	return tag, friction, restitution, layer, mask
}

func buildBoxColliderBody(nc gltf.NodeCollider, localBounds math3d.AABB, hasBounds bool, mesh *gltf.Mesh, tag string, friction, restitution float32, layer, mask LayerMask) (*Body, *TriMeshCollider) {
	localCenter := math3d.Vec3{}
	localHalf := math3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}
	if hasBounds {
		localCenter = localBounds.Center()
		localHalf = localBounds.HalfExtents()
	}
	if nc.HasCenter {
		localCenter = nc.Center
	}
	if nc.HasHalfExtents {
		localHalf = nc.HalfExtents
	}
	if localHalf.X <= 0 && localHalf.Y <= 0 && localHalf.Z <= 0 {
		return nil, nil
	}

	world := effectiveWorldMat4(nc.WorldMatrix)
	worldCenter := world.TransformPoint(localCenter)

	if _, rot, scale, ok := world.Decompose(); ok {
		he := Vec3{
			X: abs32(localHalf.X * scale.X),
			Y: abs32(localHalf.Y * scale.Y),
			Z: abs32(localHalf.Z * scale.Z),
		}
		if he.X <= 0 && he.Y <= 0 && he.Z <= 0 {
			return nil, nil
		}
		b := NewStaticBody(BoxShape(Box{HalfExtents: he}), vec3FromMath3d(worldCenter))
		b.Rot = quatFromMath3d(rot).Norm()
		b.Tag = tag
		b.Friction = friction
		b.Restitution = restitution
		b.Layer = layer
		b.Mask = mask
		b.IsTrigger = nc.IsTrigger
		return &b, nil
	}

	// Sheared or non-TRS matrix: fall back to a transformed trimesh when mesh
	// geometry is available, otherwise an enclosing world AABB.
	if mesh != nil && !nc.IsTrigger {
		tm := NewTriMeshFromGLTFMesh(*mesh, world)
		if len(tm.Triangles) > 0 {
			tm.Tag = tag
			tm.Friction = friction
			tm.Restitution = restitution
			tm.Layer = layer
			tm.Mask = mask
			return nil, tm
		}
	}
	localBox := math3d.AABB{Min: localCenter.Sub(localHalf), Max: localCenter.Add(localHalf)}
	wb := localBox.Transform(world)
	b := NewStaticBody(BoxShape(Box{HalfExtents: vec3FromMath3d(wb.HalfExtents())}), vec3FromMath3d(wb.Center()))
	b.Tag = tag
	b.Friction = friction
	b.Restitution = restitution
	b.Layer = layer
	b.Mask = mask
	b.IsTrigger = nc.IsTrigger
	return &b, nil
}

func buildSphereColliderBody(nc gltf.NodeCollider, localBounds math3d.AABB, hasBounds bool, tag string, friction, restitution float32, layer, mask LayerMask) *Body {
	localCenter := math3d.Vec3{}
	localRadius := float32(0.5)
	if hasBounds {
		localCenter = localBounds.Center()
		he := localBounds.HalfExtents()
		localRadius = max(he.X, max(he.Y, he.Z))
	}
	if nc.HasCenter {
		localCenter = nc.Center
	}
	if nc.HasRadius {
		localRadius = nc.Radius
	}
	if localRadius <= 0 {
		return nil
	}

	world := effectiveWorldMat4(nc.WorldMatrix)
	worldCenter := world.TransformPoint(localCenter)
	sx, sy, sz := mat4AxisScales(world)
	maxScale := max(sx, max(sy, sz))
	if maxScale <= 0 {
		return nil
	}

	b := NewStaticBody(SphereShape(Sphere{Radius: localRadius * maxScale}), vec3FromMath3d(worldCenter))
	b.Tag = tag
	b.Friction = friction
	b.Restitution = restitution
	b.Layer = layer
	b.Mask = mask
	b.IsTrigger = nc.IsTrigger
	return &b
}

func buildCapsuleColliderBody(nc gltf.NodeCollider, localBounds math3d.AABB, hasBounds bool, tag string, friction, restitution float32, layer, mask LayerMask) *Body {
	localCenter := math3d.Vec3{}
	if hasBounds {
		localCenter = localBounds.Center()
	}
	if nc.HasCenter {
		localCenter = nc.Center
	}

	world := effectiveWorldMat4(nc.WorldMatrix)
	worldCenter := world.TransformPoint(localCenter)
	sx, sy, sz := mat4AxisScales(world)
	rot := QuatIdentity()
	if _, q, _, ok := world.Decompose(); ok {
		rot = quatFromMath3d(q).Norm()
	}

	var capShape Capsule
	if nc.HasRadius || nc.HasHeight {
		// Explicit extras capsule is Y-aligned in node local space.
		localR := float32(0.5)
		localH := float32(2.0)
		if hasBounds {
			he := localBounds.HalfExtents()
			localR = max(he.X, he.Z)
			localH = he.Y * 2
		}
		if nc.HasRadius {
			localR = nc.Radius
		}
		if nc.HasHeight {
			localH = nc.Height
		}
		r := localR * max(sx, sz)
		h := localH * sy
		if r <= 0 {
			return nil
		}
		capShape = VerticalCapsule(Vec3{}, r, h)
	} else {
		// Mesh-derived capsule aligns along the longest scaled local axis
		// (preferring Y on ties).
		localHalf := math3d.Vec3{X: 0.5, Y: 1.0, Z: 0.5}
		if hasBounds {
			localHalf = localBounds.HalfExtents()
		}
		seX := localHalf.X * sx
		seY := localHalf.Y * sy
		seZ := localHalf.Z * sz

		axis := 1 // Y default
		if seX > seY && seX >= seZ {
			axis = 0
		} else if seZ > seY && seZ > seX {
			axis = 2
		}

		var halfLen, rad float32
		var dir Vec3
		switch axis {
		case 0:
			halfLen = seX
			rad = max(seY, seZ)
			dir = Vec3{X: 1}
		case 2:
			halfLen = seZ
			rad = max(seX, seY)
			dir = Vec3{Z: 1}
		default:
			halfLen = seY
			rad = max(seX, seZ)
			dir = Vec3{Y: 1}
		}
		if rad <= 0 {
			return nil
		}
		halfSeg := halfLen - rad
		if halfSeg < 0 {
			halfSeg = 0
		}
		capShape = Capsule{
			A:      dir.Scale(-halfSeg),
			B:      dir.Scale(halfSeg),
			Radius: rad,
		}
	}

	b := NewStaticBody(CapsuleShape(capShape), vec3FromMath3d(worldCenter))
	b.Rot = rot
	b.Tag = tag
	b.Friction = friction
	b.Restitution = restitution
	b.Layer = layer
	b.Mask = mask
	b.IsTrigger = nc.IsTrigger
	return &b
}

// BuildGLTFColliders extracts static collider bodies and TriMeshColliders from
// doc according to glTF collider naming conventions (-col, -colonly, -boxcol,
// -boxcolonly, -spherecol, -spherecolonly, -capsulecol, -capsulecolonly,
// -convcol, -convcolonly) and node extras.collider metadata.
//
// Node transforms are read from Node.WorldMatrix (computed automatically by
// gltf.Load / gltf.LoadFS and refreshed by doc.ComputeWorldMatrices).
func BuildGLTFColliders(doc *gltf.Document, opts GLTFColliderOptions) (*GLTFColliders, error) {
	out := &GLTFColliders{}
	if doc == nil {
		return out, nil
	}

	var annotated []gltf.NodeCollider
	var err error
	if opts.UseScene {
		if opts.Scene < 0 || opts.Scene >= len(doc.Scenes) {
			return nil, fmt.Errorf("physics3d: gltf scene index %d out of range for %d scenes", opts.Scene, len(doc.Scenes))
		}
		annotated, err = doc.SceneColliderNodes(opts.Scene)
	} else {
		annotated, err = doc.ColliderNodes()
	}
	if err != nil {
		return nil, err
	}

	annotatedSet := make(map[int]struct{}, len(annotated))
	for _, nc := range annotated {
		annotatedSet[nc.NodeIndex] = struct{}{}
		tag, friction, restitution, layer, mask := resolveColliderProps(nc, opts)

		var mesh *gltf.Mesh
		var localBounds math3d.AABB
		hasBounds := false
		if nc.MeshIndex >= 0 && nc.MeshIndex < len(doc.Meshes) {
			mesh = &doc.Meshes[nc.MeshIndex]
			localBounds, hasBounds = gltfMeshLocalBounds(*mesh)
		}

		switch nc.Kind {
		case gltf.ColliderTriMesh, gltf.ColliderConvex:
			if mesh == nil {
				continue
			}
			tm := NewTriMeshFromGLTFMesh(*mesh, nc.WorldMatrix)
			if len(tm.Triangles) == 0 {
				continue
			}
			tm.Tag = tag
			tm.Friction = friction
			tm.Restitution = restitution
			tm.Layer = layer
			tm.Mask = mask
			out.TriMeshes = append(out.TriMeshes, tm)

		case gltf.ColliderBox:
			b, tm := buildBoxColliderBody(nc, localBounds, hasBounds, mesh, tag, friction, restitution, layer, mask)
			if b != nil {
				out.Bodies = append(out.Bodies, b)
			} else if tm != nil {
				out.TriMeshes = append(out.TriMeshes, tm)
			}

		case gltf.ColliderSphere:
			if b := buildSphereColliderBody(nc, localBounds, hasBounds, tag, friction, restitution, layer, mask); b != nil {
				out.Bodies = append(out.Bodies, b)
			}

		case gltf.ColliderCapsule:
			if b := buildCapsuleColliderBody(nc, localBounds, hasBounds, tag, friction, restitution, layer, mask); b != nil {
				out.Bodies = append(out.Bodies, b)
			}
		}
	}

	if opts.IncludeUnmarked {
		var drawNodes []int
		if opts.UseScene {
			drawNodes = doc.SceneDrawableNodes(opts.Scene)
		} else {
			drawNodes = doc.DrawableNodes()
		}
		for _, ni := range drawNodes {
			if _, already := annotatedSet[ni]; already {
				continue
			}
			node := doc.Nodes[ni]
			if node.Mesh < 0 || node.Mesh >= len(doc.Meshes) {
				continue
			}
			tm := NewTriMeshFromGLTFMesh(doc.Meshes[node.Mesh], node.WorldMatrix)
			if len(tm.Triangles) == 0 {
				continue
			}
			tag := node.Name
			if opts.TagPrefix != "" {
				tag = opts.TagPrefix + tag
			}
			friction := opts.Friction
			if friction <= 0 {
				friction = 0.5
			}
			tm.Tag = tag
			tm.Friction = friction
			tm.Restitution = clamp32(opts.Restitution, 0, 1)
			tm.Layer = opts.Layer
			tm.Mask = opts.Mask
			out.TriMeshes = append(out.TriMeshes, tm)
		}
	}

	return out, nil
}

// AddGLTFColliders builds static colliders from doc and adds them to w.
func (w *World) AddGLTFColliders(doc *gltf.Document, opts GLTFColliderOptions) (*GLTFColliders, error) {
	col, err := BuildGLTFColliders(doc, opts)
	if err != nil {
		return nil, err
	}
	col.AddToWorld(w)
	return col, nil
}
