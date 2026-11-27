package sceneformat

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"time"

	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/game/pkg/world3d"
)

// ModelResolver may optionally be implemented by an AssetResolver (such as *scenemodel.Assets)
// to load glTF models referenced by convex or trimesh 3D colliders.
// If the resolver instead implements FileResolver (such as *project.Resolver or FSAssets),
// BuildPhysics loads models from it automatically via gltf.LoadFS.
type ModelResolver interface {
	Model(path string) (*gltf.Document, error)
}

// TerrainResolver may optionally be implemented by an AssetResolver to load
// *world3d.Heightmap assets referenced by heightfield 3D colliders.
// If the resolver instead implements FileResolver (such as *project.Resolver or FSAssets),
// BuildPhysics loads *.terrain.yaml files from it automatically via world3d.LoadTerrainFS.
type TerrainResolver interface {
	Terrain(path string) (*world3d.Heightmap, error)
}

type physics3DAssetCache struct {
	resolver AssetResolver
	models   map[string]*gltf.Document
	terrains map[string]*world3d.Heightmap
}

func newPhysics3DAssetCache(resolver AssetResolver) *physics3DAssetCache {
	return &physics3DAssetCache{resolver: resolver}
}

func (c *physics3DAssetCache) model(p string) (*gltf.Document, error) {
	if c == nil || c.resolver == nil {
		return nil, fmt.Errorf("model %q requires a ModelResolver or FileResolver", p)
	}
	if doc, ok := c.models[p]; ok {
		return doc, nil
	}
	var doc *gltf.Document
	var err error
	if mr, ok := c.resolver.(ModelResolver); ok {
		doc, err = mr.Model(p)
	} else if fr, ok := c.resolver.(FileResolver); ok {
		doc, err = gltf.LoadFS(fileResolverFS{fr: fr}, p)
	} else {
		return nil, fmt.Errorf("model %q requires a ModelResolver or FileResolver", p)
	}
	if err != nil {
		return nil, fmt.Errorf("load model %q: %w", p, err)
	}
	if doc == nil {
		return nil, fmt.Errorf("resolver returned nil model for %q", p)
	}
	if c.models == nil {
		c.models = make(map[string]*gltf.Document)
	}
	c.models[p] = doc
	return doc, nil
}

func (c *physics3DAssetCache) terrain(p string) (*world3d.Heightmap, error) {
	if c == nil || c.resolver == nil {
		return nil, fmt.Errorf("terrain %q requires a TerrainResolver or FileResolver", p)
	}
	if hm, ok := c.terrains[p]; ok {
		return hm, nil
	}
	var hm *world3d.Heightmap
	var err error
	if tr, ok := c.resolver.(TerrainResolver); ok {
		hm, err = tr.Terrain(p)
	} else if fr, ok := c.resolver.(FileResolver); ok {
		hm, _, err = world3d.LoadTerrainFS(fileResolverFS{fr: fr}, p)
	} else {
		return nil, fmt.Errorf("terrain %q requires a TerrainResolver or FileResolver", p)
	}
	if err != nil {
		return nil, fmt.Errorf("load terrain %q: %w", p, err)
	}
	if hm == nil {
		return nil, fmt.Errorf("resolver returned nil terrain for %q", p)
	}
	if c.terrains == nil {
		c.terrains = make(map[string]*world3d.Heightmap)
	}
	c.terrains[p] = hm
	return hm, nil
}

// fileResolverFS adapts a sceneformat.FileResolver to an fs.FS so gltf.LoadFS and
// world3d.LoadTerrainFS can read project files through project.Resolver or FSAssets.
type fileResolverFS struct {
	fr FileResolver
}

func (f fileResolverFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	data, err := f.fr.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return &memFSFile{name: path.Base(name), r: bytes.NewReader(data), size: int64(len(data))}, nil
}

type memFSFile struct {
	name string
	r    *bytes.Reader
	size int64
}

func (f *memFSFile) Stat() (fs.FileInfo, error) {
	return memFSFileInfo{name: f.name, size: f.size}, nil
}
func (f *memFSFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *memFSFile) Close() error               { return nil }

type memFSFileInfo struct {
	name string
	size int64
}

func (fi memFSFileInfo) Name() string       { return fi.name }
func (fi memFSFileInfo) Size() int64        { return fi.size }
func (fi memFSFileInfo) Mode() fs.FileMode  { return 0o444 }
func (fi memFSFileInfo) ModTime() time.Time { return time.Time{} }
func (fi memFSFileInfo) IsDir() bool        { return false }
func (fi memFSFileInfo) Sys() any           { return nil }

// eulerXYZToQuat3D converts Euler angles (rx, ry, rz) in radians into a unit physics3d.Quat
// using the Rz * Ry * Rx order shared by ObjectLocalTransform3D and quatToEulerXYZ.
func eulerXYZToQuat3D(rx, ry, rz float32) physics3d.Quat {
	if rx == 0 && ry == 0 && rz == 0 {
		return physics3d.Quat{}
	}
	qx := math3d.QuatFromAxisAngle(math3d.Vec3{X: 1}, rx)
	qy := math3d.QuatFromAxisAngle(math3d.Vec3{Y: 1}, ry)
	qz := math3d.QuatFromAxisAngle(math3d.Vec3{Z: 1}, rz)
	q := qz.Mul(qy).Mul(qx).Norm()
	return physics3d.Quat{X: q.X, Y: q.Y, Z: q.Z, W: q.W}
}

func colliderLocalRot3D(resolved map[string]any) physics3d.Quat {
	rx, _ := resolved["rotX"].(float64)
	ry, _ := resolved["rotY"].(float64)
	rz, _ := resolved["rotZ"].(float64)
	return eulerXYZToQuat3D(float32(rx), float32(ry), float32(rz))
}

func rotateLocalVec3D(q physics3d.Quat, v physics3d.Vec3) physics3d.Vec3 {
	if q.IsIdentity() {
		return v
	}
	return q.RotateVec(v)
}

// extractObjectMesh3D collects triangle vertices and indices in object local space
// from o.Model (honoring o.ModelMesh and o.ModelNode identically to scenemodel.Build)
// or o.Mesh (matching scenemodel.PrimitiveMesh dimensions).
func extractObjectMesh3D(o *ObjectDoc, cache *physics3DAssetCache) ([]math3d.Vec3, []uint32, error) {
	if o == nil {
		return nil, nil, errors.New("nil object")
	}
	if o.Model != "" {
		doc, err := cache.model(o.Model)
		if err != nil {
			return nil, nil, err
		}
		return extractGLTFObjectMesh(doc, o.Model, o.ModelNode, o.ModelMesh)
	}
	if o.Mesh != "" {
		return primitiveCollisionMesh(o.Mesh)
	}
	return nil, nil, errors.New("requires the object to set model or mesh")
}

func extractGLTFObjectMesh(doc *gltf.Document, modelPath, modelNode, modelMesh string) ([]math3d.Vec3, []uint32, error) {
	var verts []math3d.Vec3
	var indices []uint32

	appendMesh := func(mi int, weights []float32, xform math3d.Mat4) {
		if mi < 0 || mi >= len(doc.Meshes) {
			return
		}
		gm := &doc.Meshes[mi]
		if len(weights) == 0 {
			weights = gm.Weights
		}
		flip := mat4Det3x3(xform) < 0
		for pi := range gm.Primitives {
			prim := &gm.Primitives[pi]
			if prim.Mode != gltf.ModeTriangles && prim.Mode != gltf.ModeTriangleStrip && prim.Mode != gltf.ModeTriangleFan {
				continue
			}
			pos := prim.Positions
			if len(prim.Targets) > 0 && hasNonZeroWeight(weights) {
				pos = gltf.EvaluateMorphPositions(*prim, weights, nil)
			}
			if len(pos) == 0 {
				continue
			}
			base := uint32(len(verts))
			for _, p := range pos {
				verts = append(verts, xform.TransformPoint(p))
			}
			numPos := uint32(len(pos))
			indexed := len(prim.Indices) > 0
			count := len(pos)
			if indexed {
				count = len(prim.Indices)
			}
			idxAt := func(k int) (uint32, bool) {
				if indexed {
					ix := prim.Indices[k]
					if ix >= numPos {
						return 0, false
					}
					return base + ix, true
				}
				return base + uint32(k), true
			}
			emit := func(a, b, c uint32) {
				if flip {
					b, c = c, b
				}
				indices = append(indices, a, b, c)
			}
			switch prim.Mode {
			case gltf.ModeTriangles:
				for i := 0; i+2 < count; i += 3 {
					ia, okA := idxAt(i)
					ib, okB := idxAt(i + 1)
					ic, okC := idxAt(i + 2)
					if okA && okB && okC {
						emit(ia, ib, ic)
					}
				}
			case gltf.ModeTriangleStrip:
				for i := 0; i+2 < count; i++ {
					ia, okA := idxAt(i)
					ib, okB := idxAt(i + 1)
					ic, okC := idxAt(i + 2)
					if !okA || !okB || !okC {
						continue
					}
					if i%2 == 0 {
						emit(ia, ib, ic)
					} else {
						emit(ib, ia, ic)
					}
				}
			case gltf.ModeTriangleFan:
				if count < 3 {
					break
				}
				i0, ok0 := idxAt(0)
				if !ok0 {
					break
				}
				for i := 1; i+1 < count; i++ {
					ib, okB := idxAt(i)
					ic, okC := idxAt(i + 1)
					if okB && okC {
						emit(i0, ib, ic)
					}
				}
			}
		}
	}

	inStack := make([]bool, len(doc.Nodes))
	var walkNode func(ni int, parent math3d.Mat4, applySelf bool)
	walkNode = func(ni int, parent math3d.Mat4, applySelf bool) {
		if ni < 0 || ni >= len(doc.Nodes) || inStack[ni] {
			return
		}
		inStack[ni] = true
		defer func() { inStack[ni] = false }()
		gn := &doc.Nodes[ni]
		cur := parent
		if applySelf {
			if gn.MatrixNotTRS {
				cur = parent.Mul(gn.LocalMatrix)
			} else {
				cur = parent.Mul(gn.Transform().ToMat4())
			}
		}
		if gn.Mesh >= 0 && gn.Mesh < len(doc.Meshes) {
			appendMesh(gn.Mesh, gn.Weights, cur)
		}
		for _, child := range gn.Children {
			walkNode(child, cur, true)
		}
	}

	switch {
	case modelMesh != "":
		mi := -1
		for i := range doc.Meshes {
			if doc.Meshes[i].Name == modelMesh {
				mi = i
				break
			}
		}
		if mi < 0 {
			return nil, nil, fmt.Errorf("model %q has no mesh %q", modelPath, modelMesh)
		}
		appendMesh(mi, nil, math3d.Mat4Identity())
	case modelNode != "":
		ni := -1
		for i := range doc.Nodes {
			if doc.Nodes[i].Name == modelNode {
				ni = i
				break
			}
		}
		if ni < 0 {
			return nil, nil, fmt.Errorf("model %q has no node %q", modelPath, modelNode)
		}
		walkNode(ni, math3d.Mat4Identity(), false)
	default:
		roots := gltfSceneRoots(doc)
		if len(roots) == 0 && len(doc.Nodes) == 0 {
			for mi := range doc.Meshes {
				appendMesh(mi, nil, math3d.Mat4Identity())
			}
		} else {
			for _, root := range roots {
				walkNode(root, math3d.Mat4Identity(), true)
			}
		}
	}

	if len(verts) == 0 || len(indices) < 3 {
		return nil, nil, fmt.Errorf("model %q contains no triangle geometry", modelPath)
	}
	return verts, indices, nil
}

func gltfSceneRoots(doc *gltf.Document) []int {
	si := doc.Scene
	if si < 0 || si >= len(doc.Scenes) {
		si = 0
	}
	if si < len(doc.Scenes) {
		return doc.Scenes[si].Roots
	}
	var roots []int
	for i := range doc.Nodes {
		if doc.Nodes[i].Parent < 0 {
			roots = append(roots, i)
		}
	}
	return roots
}

func hasNonZeroWeight(weights []float32) bool {
	for _, w := range weights {
		if w != 0 {
			return true
		}
	}
	return false
}

func mat4Det3x3(m math3d.Mat4) float32 {
	c0 := math3d.Vec3{X: m[0], Y: m[1], Z: m[2]}
	c1 := math3d.Vec3{X: m[4], Y: m[5], Z: m[6]}
	c2 := math3d.Vec3{X: m[8], Y: m[9], Z: m[10]}
	return c0.Dot(c1.Cross(c2))
}

// primitiveCollisionMesh returns local triangle geometry matching scenemodel.PrimitiveMesh:
// box 1x1x1, sphere diameter 1, plane 1x1 on XZ facing +Y, cylinder diameter 1 height 1,
// capsule diameter 1 total height 2.
func primitiveCollisionMesh(name string) ([]math3d.Vec3, []uint32, error) {
	switch name {
	case MeshBox:
		verts := []math3d.Vec3{
			{X: -0.5, Y: -0.5, Z: -0.5},
			{X: 0.5, Y: -0.5, Z: -0.5},
			{X: 0.5, Y: 0.5, Z: -0.5},
			{X: -0.5, Y: 0.5, Z: -0.5},
			{X: -0.5, Y: -0.5, Z: 0.5},
			{X: 0.5, Y: -0.5, Z: 0.5},
			{X: 0.5, Y: 0.5, Z: 0.5},
			{X: -0.5, Y: 0.5, Z: 0.5},
		}
		indices := []uint32{
			4, 5, 6, 4, 6, 7, // +Z
			1, 0, 3, 1, 3, 2, // -Z
			5, 1, 2, 5, 2, 6, // +X
			0, 4, 7, 0, 7, 3, // -X
			7, 6, 2, 7, 2, 3, // +Y
			0, 1, 5, 0, 5, 4, // -Y
		}
		return verts, indices, nil
	case MeshPlane:
		verts := []math3d.Vec3{
			{X: -0.5, Y: 0, Z: -0.5},
			{X: -0.5, Y: 0, Z: 0.5},
			{X: 0.5, Y: 0, Z: 0.5},
			{X: 0.5, Y: 0, Z: -0.5},
		}
		indices := []uint32{0, 1, 2, 0, 2, 3}
		return verts, indices, nil
	case MeshSphere:
		verts, idx := latLongCollisionMesh(12, 16, func(r int) (y, rad float32) {
			phi := math.Pi * float64(r) / 12
			return float32(0.5 * math.Cos(phi)), float32(0.5 * math.Sin(phi))
		})
		return verts, idx, nil
	case MeshCylinder:
		verts, idx := cylinderCollisionMesh(16, 0.5, 0.5)
		return verts, idx, nil
	case MeshCapsule:
		const capRings = 6
		rows := 2*capRings + 1
		verts, idx := latLongCollisionMesh(rows, 16, func(r int) (y, rad float32) {
			if r <= capRings {
				phi := (math.Pi / 2) * float64(r) / capRings
				return float32(0.5 + 0.5*math.Cos(phi)), float32(0.5 * math.Sin(phi))
			}
			k := r - capRings - 1
			phi := math.Pi/2 + (math.Pi/2)*float64(k)/capRings
			return float32(-0.5 + 0.5*math.Cos(phi)), float32(0.5 * math.Sin(phi))
		})
		return verts, idx, nil
	default:
		return nil, nil, fmt.Errorf("unknown primitive mesh %q", name)
	}
}

func latLongCollisionMesh(rows, segs int, row func(r int) (y, rad float32)) ([]math3d.Vec3, []uint32) {
	var verts []math3d.Vec3
	var indices []uint32
	radii := make([]float32, rows+1)
	for r := 0; r <= rows; r++ {
		y, rad := row(r)
		if rad < 1e-6 {
			rad = 0
		}
		radii[r] = rad
		for i := 0; i < segs; i++ {
			a := 2 * math.Pi * float64(i) / float64(segs)
			s, c := float32(math.Sin(a)), float32(math.Cos(a))
			verts = append(verts, math3d.Vec3{X: rad * s, Y: y, Z: rad * c})
		}
	}
	for r := 0; r < rows; r++ {
		for i := 0; i < segs; i++ {
			next := (i + 1) % segs
			a := uint32(r*segs + i)
			d := uint32(r*segs + next)
			c := uint32((r+1)*segs + next)
			bl := uint32((r+1)*segs + i)
			if radii[r] != 0 {
				indices = append(indices, a, bl, d)
			}
			if radii[r+1] != 0 {
				indices = append(indices, d, bl, c)
			}
		}
	}
	return verts, indices
}

func cylinderCollisionMesh(segs int, radius, halfH float32) ([]math3d.Vec3, []uint32) {
	verts, indices := latLongCollisionMesh(1, segs, func(r int) (float32, float32) {
		if r == 0 {
			return halfH, radius
		}
		return -halfH, radius
	})
	topCenter := uint32(len(verts))
	verts = append(verts, math3d.Vec3{Y: halfH})
	botCenter := uint32(len(verts))
	verts = append(verts, math3d.Vec3{Y: -halfH})
	for i := 0; i < segs; i++ {
		next := (i + 1) % segs
		indices = append(indices, topCenter, uint32(i), uint32(next))
		indices = append(indices, botCenter, uint32(segs+next), uint32(segs+i))
	}
	return verts, indices
}

func buildConvexHull3D(o *ObjectDoc, compName string, resolved map[string]any, sx, sy, sz float32, center physics3d.Vec3, qLocal physics3d.Quat, cache *physics3DAssetCache) (physics3d.Shape, error) {
	rawVerts, rawIndices, err := extractObjectMesh3D(o, cache)
	if err != nil {
		return physics3d.Shape{}, fmt.Errorf("sceneformat: object %q component %q: convex collider: %w", o.ID, compName, err)
	}
	scaled := make([]math3d.Vec3, len(rawVerts))
	for i, v := range rawVerts {
		scaled[i] = math3d.Vec3{X: v.X * sx, Y: v.Y * sy, Z: v.Z * sz}
	}
	maxVerts, _ := resolved["maxVertices"].(int)
	hull, err := physics3d.ConvexHullFromMesh(scaled, rawIndices, maxVerts)
	if err != nil {
		return physics3d.Shape{}, fmt.Errorf("sceneformat: object %q component %q: convex collider: %w", o.ID, compName, err)
	}
	hull.Center = center
	return physics3d.Shape{Kind: physics3d.ShapeConvexHull, Hull: hull, Rot: qLocal}, nil
}

func buildTriMesh3D(o *ObjectDoc, compName string, resolved map[string]any, world math3d.Transform, cache *physics3DAssetCache) (*physics3d.TriMeshCollider, error) {
	rawVerts, rawIndices, err := extractObjectMesh3D(o, cache)
	if err != nil {
		return nil, fmt.Errorf("sceneformat: object %q component %q: trimesh collider: %w", o.ID, compName, err)
	}
	sx := world.Scale.X
	sy := world.Scale.Y
	sz := world.Scale.Z
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	if sz == 0 {
		sz = 1
	}
	asx := float32(math.Abs(float64(sx)))
	asy := float32(math.Abs(float64(sy)))
	asz := float32(math.Abs(float64(sz)))

	center := physics3d.Vec3{
		X: float32(resolved["offsetX"].(float64)) * asx,
		Y: float32(resolved["offsetY"].(float64)) * asy,
		Z: float32(resolved["offsetZ"].(float64)) * asz,
	}
	qLocal := colliderLocalRot3D(resolved)
	worldPos := physics3d.Vec3{X: world.Pos.X, Y: world.Pos.Y, Z: world.Pos.Z}
	worldRot := physics3d.Quat{X: world.Rot.X, Y: world.Rot.Y, Z: world.Rot.Z, W: world.Rot.W}

	verts := make([]physics3d.Vec3, len(rawVerts))
	for i, v := range rawVerts {
		p := physics3d.Vec3{X: v.X * sx, Y: v.Y * sy, Z: v.Z * sz}
		p = rotateLocalVec3D(qLocal, p).Add(center)
		if !worldRot.IsIdentity() {
			p = worldRot.RotateVec(p)
		}
		verts[i] = p.Add(worldPos)
	}

	indices := rawIndices
	if sx*sy*sz < 0 {
		indices = make([]uint32, len(rawIndices))
		copy(indices, rawIndices)
		for i := 0; i+2 < len(indices); i += 3 {
			indices[i+1], indices[i+2] = indices[i+2], indices[i+1]
		}
	}

	tm := physics3d.NewTriMeshCollider(verts, indices)
	if len(tm.Triangles) == 0 {
		return nil, fmt.Errorf("sceneformat: object %q component %q: trimesh collider produced no non-degenerate triangles", o.ID, compName)
	}
	tm.Tag = o.ID
	return tm, nil
}

func buildHeightfield3D(o *ObjectDoc, compName string, resolved map[string]any, world math3d.Transform, cache *physics3DAssetCache) (*physics3d.HeightfieldCollider, error) {
	terrainPath, _ := resolved["terrain"].(string)
	if terrainPath == "" {
		return nil, &ComponentError{
			Object:    o.ID,
			Component: compName,
			Field:     "terrain",
			Msg:       "heightfield shape requires a non-empty terrain path",
		}
	}
	hm, err := cache.terrain(terrainPath)
	if err != nil {
		return nil, fmt.Errorf("sceneformat: object %q component %q: heightfield collider: %w", o.ID, compName, err)
	}
	base := hm.Collider()
	if base.Cols < 2 || base.Rows < 2 || base.CellSize <= 0 || len(base.Heights) < base.Cols*base.Rows {
		return nil, fmt.Errorf("sceneformat: object %q component %q: terrain %q produced an invalid heightfield", o.ID, compName, terrainPath)
	}

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
	xzScale := max(sx, sz)
	cellSize := base.CellSize * xzScale
	heights := make([]float32, len(base.Heights))
	for i, h := range base.Heights {
		heights[i] = h * sy
	}
	off := physics3d.Vec3{
		X: float32(resolved["offsetX"].(float64)) * sx,
		Y: float32(resolved["offsetY"].(float64)) * sy,
		Z: float32(resolved["offsetZ"].(float64)) * sz,
	}
	worldRot := physics3d.Quat{X: world.Rot.X, Y: world.Rot.Y, Z: world.Rot.Z, W: world.Rot.W}
	if !worldRot.IsIdentity() {
		off = worldRot.RotateVec(off)
	}
	origin := physics3d.Vec3{
		X: base.Origin.X*xzScale + world.Pos.X + off.X,
		Y: base.Origin.Y*sy + world.Pos.Y + off.Y,
		Z: base.Origin.Z*xzScale + world.Pos.Z + off.Z,
	}
	hf := physics3d.NewHeightfieldCollider(base.Cols, base.Rows, cellSize, origin, heights)
	hf.Tag = o.ID
	return hf, nil
}

var _ io.Reader = (*memFSFile)(nil)
