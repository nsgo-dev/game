package navbake3d

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"strings"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

// LayerAll selects every layer.
const LayerAll uint32 = ^uint32(0)

// Geometry is the level geometry a bake reads: world-space triangles, each on one or more layers that Settings.LayerMask selects from. Triangles wind counter-clockwise seen from their front, as glTF's and this module's meshes do; a triangle facing up is a floor, and every triangle is solid to the agents whatever way it faces. Triangles are surfaces, as in Recast, so a closed mesh tall enough to stand in is hollow; the collider shapes AddPhysicsWorld and AddGridMap add are filled instead.
//
// The Add methods read the level from the sources a 3D game builds it from: glTF models, scene3d scenes and sceneformat scene files, world3d terrain heightmaps and GridMaps, and physics3d static colliders.
type Geometry struct {
	verts  []math3d.Vec3 // three per triangle
	layers []uint32      // one per triangle
	// solids are the ranges of triangles, as [first, end) triangle indices, that are the surfaces of closed convex solids, such as collider boxes: a bake fills them, where it takes other triangles as surfaces only.
	solids [][2]int
}

// TriangleCount returns the number of triangles g holds.
func (g *Geometry) TriangleCount() int {
	if g == nil {
		return 0
	}
	return len(g.layers)
}

// AddTriangle adds the triangle (a, b, c) on layer, skipping a degenerate one or one with a non-finite coordinate. A layer of 0 counts as every layer.
func (g *Geometry) AddTriangle(a, b, c math3d.Vec3, layer uint32) {
	if !finiteVec(a) || !finiteVec(b) || !finiteVec(c) {
		return
	}
	if lenSq(cross(b.Sub(a), c.Sub(a))) <= 1e-12 {
		return
	}
	if layer == 0 {
		layer = LayerAll
	}
	g.verts = append(g.verts, a, b, c)
	g.layers = append(g.layers, layer)
}

func finiteVec(v math3d.Vec3) bool {
	return !math.IsNaN(float64(v.X)) && !math.IsNaN(float64(v.Y)) && !math.IsNaN(float64(v.Z)) &&
		!math.IsInf(float64(v.X), 0) && !math.IsInf(float64(v.Y), 0) && !math.IsInf(float64(v.Z), 0)
}

// AddMesh adds the indexed triangle list positions and indices, transformed by world, on layer. Nil indices take the positions three at a time. A world matrix that mirrors (negative determinant) flips the triangles' winding back, as glTF requires.
func (g *Geometry) AddMesh(positions []math3d.Vec3, indices []uint32, world math3d.Mat4, layer uint32) {
	flip := world.Det() < 0
	add := func(ia, ib, ic int) {
		if ia >= len(positions) || ib >= len(positions) || ic >= len(positions) {
			return
		}
		a := transformPoint(world, positions[ia])
		b := transformPoint(world, positions[ib])
		c := transformPoint(world, positions[ic])
		if flip {
			b, c = c, b
		}
		g.AddTriangle(a, b, c, layer)
	}
	if indices == nil {
		for i := 0; i+2 < len(positions); i += 3 {
			add(i, i+1, i+2)
		}
		return
	}
	for i := 0; i+2 < len(indices); i += 3 {
		add(int(indices[i]), int(indices[i+1]), int(indices[i+2]))
	}
}

// AddHeightmap adds the terrain h, triangulated as its own BuildMesh does, on layer.
func (g *Geometry) AddHeightmap(h *world3d.Heightmap, layer uint32) {
	if h == nil || h.Cols < 2 || h.Rows < 2 {
		return
	}
	m := h.BuildMesh(0, 0, h.Cols-1, h.Rows-1)
	g.AddMesh(m.Positions, m.Indices, math3d.Mat4Identity(), layer)
}

// AddGridMap adds the collider boxes of every occupied cell of gm, on layer, each baked as a solid. A tile without ColliderBoxes adds nothing, so a GridMap's floors and walls need collider boxes to be baked.
func (g *Geometry) AddGridMap(gm *world3d.GridMap, layer uint32) {
	if gm == nil {
		return
	}
	bounds := gm.Bounds()
	if bounds.Empty() {
		return
	}
	for _, b := range gm.CollisionBoxesIn(bounds) {
		g.addBox(b.Center(), b.HalfExtents(), math3d.QuatIdentity(), layer)
	}
}

// boxFaces are the corners of each face of a unit box, counter-clockwise seen from outside, as signs of the half extents.
var boxFaces = [6][4][3]float32{
	{{-1, 1, -1}, {-1, 1, 1}, {1, 1, 1}, {1, 1, -1}},     // +Y
	{{-1, -1, -1}, {1, -1, -1}, {1, -1, 1}, {-1, -1, 1}}, // -Y
	{{1, -1, -1}, {1, 1, -1}, {1, 1, 1}, {1, -1, 1}},     // +X
	{{-1, -1, -1}, {-1, -1, 1}, {-1, 1, 1}, {-1, 1, -1}}, // -X
	{{-1, -1, 1}, {1, -1, 1}, {1, 1, 1}, {-1, 1, 1}},     // +Z
	{{-1, -1, -1}, {-1, 1, -1}, {1, 1, -1}, {1, -1, -1}}, // -Z
}

// beginSolid starts a closed convex solid whose triangles the following Add calls add, and endSolid ends it.
func (g *Geometry) beginSolid() int { return len(g.layers) }

func (g *Geometry) endSolid(first int) {
	if end := len(g.layers); end > first {
		g.solids = append(g.solids, [2]int{first, end})
	}
}

// addBox adds the 12 triangles of the solid box centred on c with half extents e, rotated by rot.
func (g *Geometry) addBox(c, e math3d.Vec3, rot math3d.Quat, layer uint32) {
	defer g.endSolid(g.beginSolid())
	for _, f := range boxFaces {
		var p [4]math3d.Vec3
		for i, s := range f {
			local := math3d.Vec3{X: s[0] * e.X, Y: s[1] * e.Y, Z: s[2] * e.Z}
			p[i] = rot.RotateVec3(local).Add(c)
		}
		g.AddTriangle(p[0], p[1], p[2], layer)
		g.AddTriangle(p[0], p[2], p[3], layer)
	}
}

// AddPhysicsWorld adds the static bodies, triangle meshes, heightfields and voxel colliders of w whose collision layers share a bit with mask. A collider's layer of 0 counts as every layer, as in physics3d. Boxes add their 12 triangles, and spheres and capsules a faceted approximation of their surface, each baked as a solid that fills the space inside it; every solid voxel of a voxel collider whose grid implements physics3d.VoxelBoundsGrid adds a solid box, and a grid that does not report its bounds adds nothing; dynamic and kinematic bodies and triggers are not level geometry and add nothing.
func (g *Geometry) AddPhysicsWorld(w *physics3d.World, mask uint32) {
	if w == nil {
		return
	}
	pick := func(layer physics3d.LayerMask) (uint32, bool) {
		l := uint32(layer)
		if l == 0 {
			l = LayerAll
		}
		return l, l&mask != 0
	}
	for _, b := range w.Bodies() {
		if !b.Static || b.IsTrigger {
			continue
		}
		layer, ok := pick(b.Layer)
		if !ok {
			continue
		}
		g.addShape(b.WorldShape(), layer)
	}
	for _, tm := range w.TriMeshes() {
		layer, ok := pick(tm.Layer)
		if !ok {
			continue
		}
		for _, t := range tm.Triangles {
			g.AddTriangle(vec(t.A), vec(t.B), vec(t.C), layer)
		}
	}
	for _, hf := range w.Heightfields() {
		layer, ok := pick(hf.Layer)
		if !ok || hf.Cols < 2 || hf.Rows < 2 {
			continue
		}
		hf.QueryTriangles(hf.Bounds, func(t physics3d.Triangle) {
			g.AddTriangle(vec(t.A), vec(t.B), vec(t.C), layer)
		})
	}
	for _, vc := range w.VoxelColliders() {
		layer, ok := pick(vc.Layer)
		if !ok || vc.Grid == nil {
			continue
		}
		// Only a grid that reports its bounds can be enumerated; physics3d queries any other one region by region.
		bg, ok := vc.Grid.(physics3d.VoxelBoundsGrid)
		if !ok {
			continue
		}
		lo, hi, ok := bg.VoxelBounds()
		if !ok {
			continue
		}
		bounds := physics3d.BoxFromMinMax(lo.Add(vc.Origin), hi.Add(vc.Origin))
		vc.QueryBlocks(bounds, func(_, _, _ int, b physics3d.Box) {
			g.addBox(vec(b.Center), vec(b.HalfExtents), math3d.QuatIdentity(), layer)
		})
	}
}

func vec(v physics3d.Vec3) math3d.Vec3 { return math3d.Vec3{X: v.X, Y: v.Y, Z: v.Z} }

func quat(q physics3d.Quat) math3d.Quat {
	if q == (physics3d.Quat{}) {
		return math3d.QuatIdentity()
	}
	return math3d.Quat{X: q.X, Y: q.Y, Z: q.Z, W: q.W}
}

// roundSegments is how many facets a sphere's or capsule's circles get.
const roundSegments = 12

// stepSines are the sines of the multiples of 30 degrees, 360/roundSegments, written out rather than computed with math.Sin, whose result may differ between architectures, so a capsule bakes the same everywhere.
var stepSines = [roundSegments]float32{0, 0.5, sqrt3Half, 1, sqrt3Half, 0.5, 0, -0.5, -sqrt3Half, -1, -sqrt3Half, -0.5}

const sqrt3Half = 0.8660254037844386

// stepSin returns the sine of k times 30 degrees, and stepCos its cosine.
func stepSin(k int) float32 { return stepSines[((k%roundSegments)+roundSegments)%roundSegments] }

func stepCos(k int) float32 { return stepSin(k + roundSegments/4) }

// addShape adds the surface of a world-space physics3d shape.
func (g *Geometry) addShape(s physics3d.Shape, layer uint32) {
	switch s.Kind {
	case physics3d.ShapeBox:
		g.addBox(vec(s.Box.Center), vec(s.Box.HalfExtents), quat(s.Rot), layer)
	case physics3d.ShapeSphere:
		c := vec(s.Sphere.Center)
		g.addCapsule(c, c, s.Sphere.Radius, layer)
	case physics3d.ShapeCapsule:
		g.addCapsule(vec(s.Capsule.A), vec(s.Capsule.B), s.Capsule.Radius, layer)
	}
}

// addCapsule adds a solid faceted capsule from a to b with radius r: two hemispheres joined by a cylinder, a sphere when a equals b.
func (g *Geometry) addCapsule(a, b math3d.Vec3, r float32, layer uint32) {
	if !(r > 0) || math.IsInf(float64(r), 0) {
		return
	}
	defer g.endSolid(g.beginSolid())
	up := math3d.Vec3{Y: 1}
	if axis := b.Sub(a); lenSq(axis) > 1e-12 {
		up = norm(axis)
	}
	// u and v span the plane across the axis, so (u, v, up) is right-handed.
	ref := math3d.Vec3{X: 1}
	if math.Abs(float64(up.X)) > 0.9 {
		ref = math3d.Vec3{Z: 1}
	}
	v := norm(cross(up, ref))
	u := cross(v, up)
	// ring returns the circle of latitude phi steps of 30 degrees from the equator around center.
	ring := func(center math3d.Vec3, phi int) []math3d.Vec3 {
		row := make([]math3d.Vec3, roundSegments)
		for j := range row {
			dir := madd(madd(u.Scale(stepCos(j)*stepCos(phi)), v, stepSin(j)*stepCos(phi)), up, stepSin(phi))
			row[j] = madd(center, dir, r)
		}
		return row
	}
	// Latitude rings from the pole below a to the pole above b; the equator appears at both ends so the cylinder joins the hemispheres.
	const rings = roundSegments / 2
	var rows [][]math3d.Vec3
	for i := 0; i <= rings; i++ {
		phi := i - rings/2
		switch {
		case phi < 0:
			rows = append(rows, ring(a, phi))
		case phi > 0:
			rows = append(rows, ring(b, phi))
		default:
			rows = append(rows, ring(a, 0), ring(b, 0))
		}
	}
	for i := 0; i+1 < len(rows); i++ {
		for j := range roundSegments {
			j1 := (j + 1) % roundSegments
			p00, p01 := rows[i][j], rows[i][j1]
			p10, p11 := rows[i+1][j], rows[i+1][j1]
			g.AddTriangle(p00, p01, p11, layer)
			g.AddTriangle(p00, p11, p10, layer)
		}
	}
}

// AddGLTF adds the triangles of every mesh in doc's default scene (its first scene when it names none, or every node without a parent when it has no scenes), each at its node's world transform and then at world, on layer. Point and line primitives add nothing; skins and morph targets are left at their bind pose.
func (g *Geometry) AddGLTF(doc *gltf.Document, world math3d.Mat4, layer uint32) {
	if doc == nil {
		return
	}
	forEachGLTFMesh(doc, func(node *gltf.Node) {
		m := world.Mul(node.WorldMatrix)
		for _, prim := range doc.Meshes[node.Mesh].Primitives {
			g.AddMesh(prim.Positions, primitiveTriangles(prim), m, layer)
		}
	})
}

// forEachGLTFMesh calls fn for every node with a mesh under the roots AddGLTF bakes, parents before children.
func forEachGLTFMesh(doc *gltf.Document, fn func(node *gltf.Node)) {
	var roots []int
	switch {
	case doc.Scene >= 0 && doc.Scene < len(doc.Scenes):
		roots = doc.Scenes[doc.Scene].Roots
	case len(doc.Scenes) > 0:
		roots = doc.Scenes[0].Roots
	default:
		for i := range doc.Nodes {
			if doc.Nodes[i].Parent < 0 {
				roots = append(roots, i)
			}
		}
	}
	seen := make([]bool, len(doc.Nodes))
	var visit func(i int)
	visit = func(i int) {
		if i < 0 || i >= len(doc.Nodes) || seen[i] {
			return
		}
		seen[i] = true
		n := &doc.Nodes[i]
		if n.Mesh >= 0 && n.Mesh < len(doc.Meshes) {
			fn(n)
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	for _, r := range roots {
		visit(r)
	}
}

// primitiveTriangles returns prim's triangles as an index list, nil meaning the positions three at a time, expanding strips and fans; for points and lines it returns an empty list.
func primitiveTriangles(prim gltf.Primitive) []uint32 {
	idx := prim.Indices
	count := len(prim.Positions)
	if idx != nil {
		count = len(idx)
	}
	at := func(i int) uint32 {
		if idx != nil {
			return idx[i]
		}
		return uint32(i)
	}
	switch prim.Mode {
	case gltf.ModeTriangles:
		if idx == nil {
			return nil
		}
		return idx
	case gltf.ModeTriangleStrip:
		out := make([]uint32, 0, max(count-2, 0)*3)
		for i := 0; i+2 < count; i++ {
			if i%2 == 0 {
				out = append(out, at(i), at(i+1), at(i+2))
			} else {
				out = append(out, at(i+1), at(i), at(i+2))
			}
		}
		return out
	case gltf.ModeTriangleFan:
		out := make([]uint32, 0, max(count-2, 0)*3)
		for i := 1; i+1 < count; i++ {
			out = append(out, at(0), at(i), at(i+1))
		}
		return out
	default:
		return []uint32{}
	}
}

// NodeMesh returns the triangles of a scene3d node in its local space, three vertices per triangle, or nil when the node has no geometry to bake.
type NodeMesh func(n *scene3d.Node) ([]math3d.Vec3, error)

// AddScene adds the geometry of every enabled node of s, outside the subtree of a disabled node, whose Layer shares a bit with mask, transformed by the node's world matrix and on the node's Layer. mesh gives each node's triangles; nil reads a *scene3d.MeshCollider. A node mesh returns an error, it is returned with the node's name.
func (g *Geometry) AddScene(s *scene3d.Scene, mask uint32, mesh NodeMesh) error {
	if s == nil {
		return nil
	}
	if mesh == nil {
		mesh = ColliderMesh
	}
	return g.addNode(s.Root(), mask, mesh)
}

// addNode adds n and its descendants as AddScene does. It recurses itself rather than using scene3d's Walk, whose callback can only stop the whole traversal, so a disabled node skips its own subtree and nothing after it.
func (g *Geometry) addNode(n *scene3d.Node, mask uint32, mesh NodeMesh) error {
	if !n.Enabled {
		return nil
	}
	if n.Layer&mask != 0 {
		tris, err := mesh(n)
		if err != nil {
			return fmt.Errorf("navbake3d: node %q: %w", n.Name, err)
		}
		if len(tris) >= 3 {
			g.AddMesh(tris[:len(tris)-len(tris)%3], nil, n.WorldMatrix(), n.Layer)
		}
	}
	for _, c := range n.Children() {
		if err := g.addNode(c, mask, mesh); err != nil {
			return err
		}
	}
	return nil
}

// ColliderMesh is the NodeMesh that reads a node's *scene3d.MeshCollider.
func ColliderMesh(n *scene3d.Node) ([]math3d.Vec3, error) {
	if mc, ok := n.Collider.(*scene3d.MeshCollider); ok && mc != nil {
		return mc.Triangles, nil
	}
	return nil, nil
}

// GLTFTriangles returns the triangles AddGLTF would add from doc with an identity world matrix, three vertices per triangle.
func GLTFTriangles(doc *gltf.Document) []math3d.Vec3 {
	var g Geometry
	g.AddGLTF(doc, math3d.Mat4Identity(), LayerAll)
	return g.verts
}

// IsModelPath reports whether p names a glTF model, a .gltf or .glb file, as a scene object's model does (sceneformat.IsModelPath).
func IsModelPath(p string) bool {
	ext := strings.ToLower(path.Ext(p))
	return ext == ".gltf" || ext == ".glb"
}

// SceneMeshes returns the NodeMesh for nodes built by sceneformat.BuildNodes3D: an object whose Model names a glTF model in fsys, by its slash-separated path from the project root, bakes that model's triangles, each model loaded once; any other node bakes its *scene3d.MeshCollider.
func SceneMeshes(fsys fs.FS) NodeMesh {
	models := map[string][]math3d.Vec3{}
	return func(n *scene3d.Node) ([]math3d.Vec3, error) {
		if o, ok := n.UserData.(*sceneformat.ObjectDoc); ok && o != nil && IsModelPath(o.Model) {
			if tris, ok := models[o.Model]; ok {
				return tris, nil
			}
			if fsys == nil {
				return nil, errors.New("navbake3d: no file system to load models from")
			}
			doc, err := gltf.LoadFS(fsys, o.Model)
			if err != nil {
				return nil, err
			}
			tris := GLTFTriangles(doc)
			models[o.Model] = tris
			return tris, nil
		}
		return ColliderMesh(n)
	}
}

// AddSceneFile adds the geometry of a sceneformat scene: it resolves the scenes doc instances and inherits from fsys, builds the scene's 3D node tree and adds every object whose Model names a glTF model, as SceneMeshes loads it, on scene3d.LayerDefault.
func (g *Geometry) AddSceneFile(doc *sceneformat.SceneDoc, fsys fs.FS) error {
	resolved, err := sceneformat.Resolve(doc, sceneformat.FSScenes{FS: fsys})
	if err != nil {
		return err
	}
	s := scene3d.NewScene()
	if _, err := sceneformat.BuildNodes3D(resolved, s.Root()); err != nil {
		return err
	}
	return g.AddScene(s, LayerAll, SceneMeshes(fsys))
}

// bounds returns the bounding box of the triangles on a layer in mask and how many there are.
func (g *Geometry) bounds(mask uint32) (math3d.AABB, int) {
	b := math3d.AABBEmpty()
	n := 0
	for t, l := range g.layers {
		if l&mask == 0 {
			continue
		}
		n++
		for k := range 3 {
			b = b.ExpandPoint(g.verts[t*3+k])
		}
	}
	return b, n
}
