package editor

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/fnv"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
)

// scene3DState is the open scene built in 3D with package scenemodel, which the 3D viewport draws on the GPU and picks by geometry. It is built on first use after the scene's look changes (an object's mesh, model, material, instanced scene or overrides, its layers, or the project's files) and otherwise kept: a transform, a hidden object, a clip's pose or a running game moves the built nodes in place (sync3D), so a gizmo drag rebuilds no mesh.
type scene3DState struct {
	// world holds the built node tree; built is what scenemodel.Build made in it, nil before the first build and after a failed one, whose error is err.
	world *scene3d.Scene
	built *scenemodel.Built
	err   error
	// gen counts the builds, so the GPU renderer knows when to upload the geometry again.
	gen uint64
	// stale says the scene may have changed its look since the last build (Rebuild sets it); doc, key, assetsRev and projectPath are what the last build was made from.
	stale       bool
	doc         *sceneformat.SceneDoc
	key         uint64
	assetsRev   uint64
	projectPath string
	// files loads models, material files and images from the open project's files.
	files *scenemodel.Assets
	// bounds holds the bounds of each object's own geometry in its node's space, before its transform; an object without geometry has none.
	bounds map[string]math3d.AABB
	// edges holds the distinct edges of each mesh of the build by MeshID, for the wireframe overlay, made on first use.
	edges map[uint32][][2]uint32
}

// scene3DAssets resolves what scenemodel.Build loads: the editor's Resolver when it can, and the open project's files otherwise.
type scene3DAssets struct {
	resolver sceneformat.AssetResolver
	files    *scenemodel.Assets
}

// errNoProjectFiles is why a model, material file or image cannot load without a project open and a Resolver that loads it.
var errNoProjectFiles = errors.New("editor: no project is open to load it from, and the Resolver does not load it")

// Model loads the glTF model at path through the Resolver when it is a scenemodel.ModelResolver, and from the project's files otherwise.
func (a scene3DAssets) Model(path string) (*gltf.Document, error) {
	if r, ok := a.resolver.(scenemodel.ModelResolver); ok {
		return r.Model(path)
	}
	if a.files == nil {
		return nil, errNoProjectFiles
	}
	return a.files.Model(path)
}

// Material loads the material file at path through the Resolver when it is a sceneformat.MaterialResolver, and from the project's files otherwise.
func (a scene3DAssets) Material(path string) (*resource.Material, error) {
	if r, ok := a.resolver.(sceneformat.MaterialResolver); ok {
		return r.Material(path)
	}
	if a.files == nil {
		return nil, errNoProjectFiles
	}
	return a.files.Material(path)
}

// Image loads the image at path through the Resolver when it is a scenemodel.ImageResolver, and from the project's files otherwise.
func (a scene3DAssets) Image(path string) (image.Image, error) {
	if r, ok := a.resolver.(scenemodel.ImageResolver); ok {
		return r.Image(path)
	}
	if a.files == nil {
		return nil, errNoProjectFiles
	}
	return a.files.Image(path)
}

// Scene3D returns the open scene built in 3D by scenemodel.Build, the geometry and materials the 3D viewport draws and picks by, with its nodes posed as the viewport shows them: by the open animation clip at the playhead, by the running game while playing, and with the objects hidden in the editor (SetObjectHidden) and screen-space layers invisible. It returns nil and the error when the scene does not build, such as when a model it names does not load.
//
// Models, material files and images load through the editor's Resolver when it implements scenemodel.ModelResolver, sceneformat.MaterialResolver or scenemodel.ImageResolver, and from the open project's files otherwise. A scene that instances or inherits other scene files is resolved first (sceneformat.Resolve), with the Resolver as its sceneformat.SceneResolver. The scene is built again only when what it looks like changes, not when an object moves, so the result stays the same between such edits and its nodes move in place.
func (e *Editor) Scene3D() (*scenemodel.Built, error) {
	if e == nil {
		return nil, errors.New("editor: nil editor")
	}
	s := e.scene3D()
	return s.built, s.err
}

// scene3D brings the 3D build up to date with the open scene, building it again when its look changed, and returns it with its nodes posed (sync3D).
func (e *Editor) scene3D() *scene3DState {
	s := &e.s3d
	if e.Doc == nil {
		// gen only grows, so a renderer still holding the last build's upload never takes the next build for it.
		*s = scene3DState{gen: s.gen, files: s.files, projectPath: s.projectPath, assetsRev: s.assetsRev}
		return s
	}
	if e.ProjectPath != s.projectPath || e.assetsRev != s.assetsRev {
		// Files the editor moved, installed or rescanned may be what the cached models and materials came from.
		s.files = nil
		s.stale = true
	}
	if s.files == nil && e.ProjectPath != "" {
		s.files = scenemodel.NewAssets(os.DirFS(filepath.Dir(e.ProjectPath)))
	}
	if s.stale || s.doc != e.Doc {
		key, comparable := lookKey3D(e.Doc)
		if !comparable || s.world == nil || s.err != nil || key != s.key || e.ProjectPath != s.projectPath || e.assetsRev != s.assetsRev {
			e.build3D(s)
		}
		s.doc, s.key, s.stale = e.Doc, key, false
		s.projectPath, s.assetsRev = e.ProjectPath, e.assetsRev
	}
	if s.built != nil {
		e.sync3D(s)
	}
	return s
}

// build3D builds the open scene into a new world.
func (e *Editor) build3D(s *scene3DState) {
	s.gen++
	s.world, s.built, s.err, s.bounds, s.edges = nil, nil, nil, nil, nil
	doc := e.Doc
	if doc.RefersToScenes() {
		scenes, _ := e.Resolver.(sceneformat.SceneResolver)
		resolved, err := sceneformat.Resolve(doc, scenes)
		if err != nil {
			s.err = err
			return
		}
		doc = resolved
	}
	world := scene3d.NewScene()
	built, err := scenemodel.Build(doc, world.Root(), scene3DAssets{resolver: e.Resolver, files: s.files})
	if err != nil {
		s.err = err
		return
	}
	s.world, s.built = world, built
	s.bounds = make(map[string]math3d.AABB)
	soups := make(map[uint32][]math3d.Vec3)
	for _, d := range built.Drawables {
		obj := built.Objects[d.Object]
		if obj == nil {
			continue
		}
		// The drawable's geometry in the space of its object's node: a model's child node is placed by the transforms of the nodes between them.
		rel := math3d.Mat4Identity()
		for n := d.Node; n != nil && n != obj; n = n.Parent() {
			rel = n.Local.ToMat4().Mul(rel)
		}
		b, ok := s.bounds[d.Object]
		if !ok {
			b = math3d.AABBEmpty()
		}
		s.bounds[d.Object] = b.Union(d.Node.LocalBounds.Transform(rel))
		d.Node.SetCollider(&scene3d.MeshCollider{Triangles: drawableTriangles(d, soups)})
	}
}

// drawableTriangles returns the triangles of every part of d as a triangle soup in its node's space, sharing the soup of a one-part drawable's mesh through soups.
func drawableTriangles(d *scenemodel.Drawable, soups map[uint32][]math3d.Vec3) []math3d.Vec3 {
	if len(d.Parts) == 1 {
		p := &d.Parts[0]
		if soup, ok := soups[p.MeshID]; ok {
			return soup
		}
		soup := appendTriangles(nil, p.Mesh)
		soups[p.MeshID] = soup
		return soup
	}
	var soup []math3d.Vec3
	for i := range d.Parts {
		soup = appendTriangles(soup, d.Parts[i].Mesh)
	}
	return soup
}

// appendTriangles appends the triangles of m, three positions each, to dst.
func appendTriangles(dst []math3d.Vec3, m render3d.Mesh) []math3d.Vec3 {
	if len(m.Indices) == 0 {
		return append(dst, m.Positions[:len(m.Positions)/3*3]...)
	}
	for _, i := range m.Indices[:len(m.Indices)/3*3] {
		dst = append(dst, m.Positions[i])
	}
	return dst
}

// sync3D poses the built nodes as the viewport shows the scene: each object's transform as the open clip or the running game poses it, the objects hidden in the editor invisible with their descendants, and each layer visible when it is visible and in world space. It touches a node only when that changes something.
func (e *Editor) sync3D(s *scene3DState) {
	built := s.built
	for li := range e.Doc.Layers {
		l := &e.Doc.Layers[li]
		if ln := built.Layers[l.Name]; ln != nil {
			ln.Visible = l.IsVisible() && l.EffectiveSpace() != sceneformat.SpaceScreen
		}
		sceneformat.WalkObjects(l.Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			n := built.Objects[o.ID]
			if n == nil {
				return true
			}
			if t := sceneformat.ObjectLocalTransform3D(e.posed3D(o)); n.Local != t {
				n.SetLocal(t)
			}
			n.Visible = !e.hidden[o.ID]
			return true
		})
	}
}

// geometryBounds3D returns the bounds of the geometry object id draws itself, in its node's space before its transform, when the 3D build gives it any. It reads the build as it stands: the caller brings it up to date (scene3D).
func (e *Editor) geometryBounds3D(id string) (math3d.AABB, bool) {
	b, ok := e.s3d.bounds[id]
	return b, ok && !b.Empty()
}

// pickGeometry3D returns the object of the open scene whose geometry the ray from orig along dir hits first, passing over geometry that belongs to an object hidden or locked in the editor or under one. An object that only an instanced scene adds is reported as the nearest of its ancestors that the open scene holds.
func (e *Editor) pickGeometry3D(s *scene3DState, orig, dir [3]float64) (id string, ok bool) {
	if s.world == nil {
		return "", false
	}
	ray := math3d.Ray{Origin: math3d.Vec3{X: float32(orig[0]), Y: float32(orig[1]), Z: float32(orig[2])}, Dir: math3d.Vec3{X: float32(dir[0]), Y: float32(dir[1]), Z: float32(dir[2])}}
	for _, hit := range s.world.Raycast(ray, math.MaxUint32) {
		d, found := scene3d.AttachmentAs[*scenemodel.Drawable](hit.Node, scenemodel.AttachmentKey)
		if !found {
			continue
		}
		owner := e.docObjectFor(d.Object)
		if owner == "" || e.pickBlocked3D(owner) {
			continue
		}
		return owner, true
	}
	return "", false
}

// docObjectFor returns id when the open scene holds an object with it, and otherwise the nearest ancestor the scene holds of the instanced object id names ("enemy_1/body" belongs to "enemy_1"), or "" for none.
func (e *Editor) docObjectFor(id string) string {
	for id != "" {
		if obj, _, _ := e.FindObject(id); obj != nil {
			return id
		}
		i := strings.LastIndexByte(id, '/')
		if i < 0 {
			return ""
		}
		id = id[:i]
	}
	return ""
}

// pickBlocked3D reports whether the object id, or one of its ancestors, is hidden or locked in the editor, or its layer is locked, so a press passes through it.
func (e *Editor) pickBlocked3D(id string) bool {
	if _, li, _ := e.FindObject(id); li >= 0 && e.layerLocked[e.Doc.Layers[li].Name] {
		return true
	}
	for _, o := range objectPath(e.Doc, id) {
		if e.hidden[o.ID] || e.locked[o.ID] {
			return true
		}
	}
	return false
}

// meshEdges3D returns the distinct edges of the triangles of m, as pairs of indices into its positions, made once per mesh of the build.
func (s *scene3DState) meshEdges3D(id uint32, m render3d.Mesh) [][2]uint32 {
	if edges, ok := s.edges[id]; ok {
		return edges
	}
	index := func(i int) uint32 {
		if len(m.Indices) == 0 {
			return uint32(i)
		}
		return m.Indices[i]
	}
	count := len(m.Indices)
	if count == 0 {
		count = len(m.Positions)
	}
	seen := make(map[[2]math3d.Vec3]bool)
	var edges [][2]uint32
	for t := 0; t+2 < count; t += 3 {
		tri := [3]uint32{index(t), index(t + 1), index(t + 2)}
		for k := range 3 {
			a, b := tri[k], tri[(k+1)%3]
			// Two vertices at the same place, as a flat-shaded or UV-seamed mesh splits them, make one edge.
			pa, pb := m.Positions[a], m.Positions[b]
			if pa == pb {
				continue
			}
			if lessVec3(pb, pa) {
				pa, pb = pb, pa
			}
			key := [2]math3d.Vec3{pa, pb}
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, [2]uint32{a, b})
		}
	}
	if s.edges == nil {
		s.edges = make(map[uint32][][2]uint32)
	}
	s.edges[id] = edges
	return edges
}

// lessVec3 orders positions by X, then Y, then Z, so an edge's two ends make one key whichever way a triangle walks it.
func lessVec3(a, b math3d.Vec3) bool {
	if a.X != b.X {
		return a.X < b.X
	}
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.Z < b.Z
}

// lookKey3D returns a hash of everything in doc that decides what its 3D build draws, leaving out the transforms that sync3D applies in place: the scene it inherits and its overrides, its layers, and each object's place in the tree, look (mesh, model, modelNode, modelMesh, material), instanced scene and overrides. comparable is false when an override holds a value JSON cannot encode, so the key cannot tell whether it changed.
func lookKey3D(doc *sceneformat.SceneDoc) (key uint64, comparable bool) {
	h := fnv.New64a()
	comparable = true
	writeJSON := func(v any) {
		data, err := json.Marshal(v)
		if err != nil {
			comparable = false
			return
		}
		h.Write(data)
	}
	str := func(s string) {
		var n [8]byte
		binary.LittleEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	str(doc.Inherits)
	// JSON orders map keys and follows pointers, so equal overrides hash equally.
	writeJSON(doc.Overrides)
	writeJSON(doc.LayerOverrides)
	for li := range doc.Layers {
		l := &doc.Layers[li]
		str("layer")
		str(l.Name)
		var walk func(objs []sceneformat.ObjectDoc)
		walk = func(objs []sceneformat.ObjectDoc) {
			str("[")
			for oi := range objs {
				o := &objs[oi]
				str(o.ID)
				str(o.Mesh)
				str(o.Model)
				str(o.ModelNode)
				str(o.ModelMesh)
				str(o.Material)
				str(o.Scene)
				writeJSON(o.Overrides)
				walk(o.Children)
			}
			str("]")
		}
		walk(l.Objects)
	}
	return h.Sum64(), comparable
}
