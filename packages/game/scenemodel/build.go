package scenemodel

import (
	"cmp"
	"errors"
	"fmt"
	"image"
	"math"
	"slices"

	"github.com/nsgo-dev/game/packages/game/gltf"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/render3d"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/importsettings"
	"github.com/nsgo-dev/game/packages/game/world3d"
)

// AttachmentKey is the scene3d.Node attachment under which Build stores the *Drawable of each node that draws geometry, so code that holds a node, such as an editor's picking, can find what it draws with scene3d.AttachmentAs.
const AttachmentKey = "scenemodel.drawable"

// Material is the surface a Part draws with, in glTF 2.0's metallic-roughness terms.
type Material struct {
	// BaseColor is the linear RGBA base color, multiplied by BaseColorTexture's texels.
	BaseColor math3d.Vec4
	// Metallic and Roughness are the metalness and perceptual roughness, from 0 to 1.
	Metallic, Roughness float32
	// Emissive is the linear RGB light the surface gives off, its glTF emissive strength included.
	Emissive math3d.Vec3
	// BaseColorTexture is the sRGB base color image, or nil for none.
	BaseColorTexture *Texture
	// AlphaMode says how the base color's alpha is used: ignored, a cutout at AlphaCutoff, or blended over what is behind.
	AlphaMode render3d.AlphaMode
	// AlphaCutoff is the alpha below which a render3d.AlphaMask surface is discarded.
	AlphaCutoff float32
	// DoubleSided draws the back faces too, lit from the side the viewer sees.
	DoubleSided bool
}

// Texture is a decoded image a Material samples. Build returns one *Texture per image and sampler, which every material that uses them shares, so a renderer uploads each once.
type Texture struct {
	// Name says where the image came from, such as "models/crate.glb image 0" or "textures/stone.png", for debug labels.
	Name string
	// Image is the decoded image, whose color is sRGB.
	Image image.Image
	// Sampler is how the image is filtered and wrapped: the glTF texture's sampler, or linear filtering with repeat wrapping for a material file's texture.
	Sampler render3d.SamplerOptions
	// Mipmaps reports whether the sampler filters between mip levels, so the image needs a mip chain.
	Mipmaps bool
}

// Part is one piece of geometry a node draws with one material.
type Part struct {
	// Mesh is the triangle geometry in the space of the node that draws it.
	Mesh render3d.Mesh
	// MeshID identifies Mesh among the meshes of one Built, from 0 up: parts that draw the same glTF primitive or primitive shape share it, as render3d.DrawItem.MeshID expects.
	MeshID uint32
	// Material is the surface Mesh draws with.
	Material Material
	// MaterialID identifies Material among the materials of one Built, from 0 up, shared by the parts that use the same glTF material or material file.
	MaterialID uint32
	// LODs are the detail levels for this part, ordered from finest (index 0, matching Mesh and MeshID) to coarsest. Empty when the part has only one detail level.
	LODs []PartLOD

	// lodLevels is the prebuilt render3d.LODLevel slice for AppendDrawItems so frame submission does not allocate.
	lodLevels []render3d.LODLevel
}

// Drawable is a node that draws geometry: an object's node, or a node Build made for a node of the object's model.
type Drawable struct {
	// Node is the scene3d node the parts are drawn at; its LocalBounds hold the parts' bounds.
	Node *scene3d.Node
	// Object is the ID of the scene object the drawable belongs to.
	Object string
	// Parts are what the node draws, in the order the model lists them.
	Parts []Part
}

// Built is a scene's 3D node tree with the geometry, lights, cameras and environment each scene declares, from Build.
type Built struct {
	// Nodes3D holds the layer and object nodes, as sceneformat.BuildNodes3D builds them.
	sceneformat.Nodes3D
	// Drawables are the nodes that draw geometry, depth first in document order: an object's node, then the nodes made for its model's nodes or grid map instances, then its child objects.
	Drawables []*Drawable
	// GridMaps maps each object ID that named ObjectDoc.GridMap to its constructed runtime *world3d.GridMap, for collision queries, raycasting, or custom instancing.
	GridMaps map[string]*world3d.GridMap
	// Occluders are the occluders built from scene objects that set ObjectDoc.Occluder, in document order.
	Occluders []Occluder
	// LODHysteresis is the largest non-zero lodHysteresis declared by any model import sidecar in the built scene, or 0 when none sets one.
	LODHysteresis float32
	// Lights are the scene's 3D light objects in depth-first document order, linked to their scene3d nodes.
	Lights []*Light
	// Cameras are the scene's 3D camera objects in depth-first document order, linked to their scene3d nodes.
	Cameras []*Camera
	// ReflectionProbes are the scene's 3D reflection probe objects in depth-first document order, linked to their scene3d nodes and any loaded baked cubemaps.
	ReflectionProbes []*ReflectionProbe
	// LightProbeGrids are the scene's 3D light probe grid objects in depth-first document order, linked to their scene3d nodes and any loaded baked SH irradiance grids.
	LightProbeGrids []*LightProbeGrid
	// Environment holds the resolved sky, ambient/IBL lighting, fog, tone mapping and exposure from SceneDoc.Environment.
	Environment Environment
	// Meshes and Materials count the distinct MeshID and MaterialID values the parts use.
	Meshes, Materials int
}

// Build builds doc's layers and objects into a 3D node tree under parent, as sceneformat.BuildNodes3D does, and gives every object that names a look its geometry and material:
//
//   - ObjectDoc.Mesh, a built-in primitive (PrimitiveMesh), drawn on the object's node with the object's material, or with a white, non-metallic, fully rough surface when it names none;
//   - ObjectDoc.Model, a glTF model loaded through models: with ModelMesh, that mesh drawn on the object's node; with ModelNode, that node's mesh drawn on the object's node and each of its descendants as a child node with its own transform; and with neither, a child node for each root node of the model's default scene (or of its first scene, or every parentless node when it has none) and their descendants. The model's materials are used unless the object names a material file, which replaces them all.
//   - ObjectDoc.GridMap, a 3D grid map loaded through a GridMapResolver and its glTF mesh library through models, instanced with world3d.GridMap.BuildInstances as child nodes of the object's node sharing the tile meshes and materials, with the built *world3d.GridMap recorded in Built.GridMaps.
//
// A node Build makes for a glTF node is named after it, and is added after the object's child objects. Every node that draws gets its geometry's bounds as its LocalBounds and its *Drawable as the attachment AttachmentKey. A model draws in its bind pose with its meshes' default morph weights applied; skins, cameras, lights, animations and vertex colors are not used, nor are points and lines. A primitive without normals draws flat-shaded, and the base color texture's UV set and KHR_texture_transform are applied to the UVs.
//
// models may be nil when no object names a model or grid map. It must implement sceneformat.MaterialResolver when an object names a material file, ImageResolver when such a file names a texture, and GridMapResolver when an object names a grid map; Assets does all four. A material file is read when Build runs: build again to see it edited.
//
// Build returns an error and leaves parent unchanged when doc is invalid or still instances or inherits other scene files (pass it through sceneformat.Resolve first), when parent is nil, or when a model, node, mesh, grid map, material or image cannot be loaded or found.
func Build(doc *sceneformat.SceneDoc, parent *scene3d.Node, models ModelResolver) (*Built, error) {
	if parent == nil {
		return nil, scene3d.ErrNilNode
	}
	staging := scene3d.NewNode("")
	nodes, err := sceneformat.BuildNodes3D(doc, staging)
	if err != nil {
		return nil, err
	}
	env, err := buildEnvironment(doc.Environment, models)
	if err != nil {
		return nil, err
	}
	b := &builder{
		built:         &Built{Nodes3D: *nodes, GridMaps: make(map[string]*world3d.GridMap), Environment: env},
		models:        models,
		docs:          make(map[string]*gltf.Document),
		gridMaps:      make(map[string]*world3d.GridMapDoc),
		sidecars:      make(map[string]*importsettings.File),
		sidecarLoaded: make(map[string]bool),
		meshIDs:       make(map[meshKey]uint32),
		matIDs:        make(map[materialKey]uint32),
		gltfMats:      make(map[materialKey]Material),
		fileMats:      make(map[string]Material),
		textures:      make(map[textureKey]*Texture),
		fileTexts:     make(map[string]*Texture),
	}
	for li := range doc.Layers {
		l := &doc.Layers[li]
		var objErr error
		sceneformat.WalkObjects(l.Objects, func(o, _ *sceneformat.ObjectDoc) bool {
			if err := b.object(o, nodes.Objects[o.ID]); err != nil {
				objErr = fmt.Errorf("scenemodel: layer %q object %q: %w", l.Name, o.ID, err)
				return false
			}
			return true
		})
		if objErr != nil {
			return nil, objErr
		}
	}
	// Move the finished layer nodes under parent last, so a failure above leaves parent untouched.
	layers := staging.Children()
	for i, ln := range layers {
		if err := parent.AddChild(ln); err != nil {
			for _, moved := range layers[:i] {
				parent.RemoveChild(moved)
			}
			return nil, fmt.Errorf("scenemodel: layer %q: %w", ln.Name, err)
		}
	}
	b.built.Meshes, b.built.Materials = len(b.meshIDs), len(b.matIDs)
	return b.built, nil
}

// meshKey identifies a mesh's geometry: a primitive shape by name, or a glTF primitive by its document, mesh and primitive index and the UV set its base color texture reads.
type meshKey struct {
	primitive string
	doc       *gltf.Document
	mesh      int
	prim      int
	// uvFrom is the glTF material whose base color texture chose the UVs, or -1 for TEXCOORD_0.
	uvFrom int
	// weights are the morph weights applied, as written, or "".
	weights string
}

// materialKey identifies a material: a glTF material by document and index (-1 for the glTF default), a material file by path, or the primitives' default.
type materialKey struct {
	doc   *gltf.Document
	index int
	file  string
}

// textureKey identifies a glTF image sampled with one sampler.
type textureKey struct {
	doc     *gltf.Document
	image   int
	sampler int
}

// builder holds what one Build has loaded so far.
type builder struct {
	built         *Built
	models        ModelResolver
	docs          map[string]*gltf.Document
	gridMaps      map[string]*world3d.GridMapDoc
	sidecars      map[string]*importsettings.File
	sidecarLoaded map[string]bool
	meshIDs       map[meshKey]uint32
	matIDs        map[materialKey]uint32
	gltfMats      map[materialKey]Material
	fileMats      map[string]Material
	textures      map[textureKey]*Texture
	fileTexts     map[string]*Texture
	// shapes caches the geometry of each primitive shape.
	shapes map[string]render3d.Mesh
}

// object gives the object o, built at node n, its geometry, optional occluder, light and 3D camera.
func (b *builder) object(o *sceneformat.ObjectDoc, n *scene3d.Node) error {
	startDrawables := len(b.built.Drawables)
	if o.Light != nil {
		b.built.Lights = append(b.built.Lights, &Light{Node: n, Object: o.ID, Doc: *o.Light.Clone()})
	}
	if o.Camera != nil {
		b.built.Cameras = append(b.built.Cameras, &Camera{Node: n, Object: o.ID, Doc: *o.Camera.Clone()})
	}
	if err := b.buildReflectionProbe(o, n); err != nil {
		return err
	}
	if err := b.buildLightProbeGrid(o, n); err != nil {
		return err
	}
	switch {
	case o.Mesh != "":
		mesh, err := b.shape(o.Mesh)
		if err != nil {
			return err
		}
		mat, matID, err := b.primitiveMaterial(o.Material)
		if err != nil {
			return err
		}
		b.attach(o.ID, n, []Part{{Mesh: mesh, MeshID: b.meshID(meshKey{primitive: o.Mesh, uvFrom: -1}), Material: mat, MaterialID: matID}})
	case o.Model != "":
		if err := b.model(o, n); err != nil {
			return err
		}
	case o.GridMap != "":
		if err := b.gridMap(o, n); err != nil {
			return err
		}
	}
	b.buildObjectOccluders(o, n, startDrawables)
	return nil
}

// shape returns the geometry of the primitive shape name, building it once.
func (b *builder) shape(name string) (render3d.Mesh, error) {
	if m, ok := b.shapes[name]; ok {
		return m, nil
	}
	m, err := PrimitiveMesh(name)
	if err != nil {
		return render3d.Mesh{}, err
	}
	if b.shapes == nil {
		b.shapes = make(map[string]render3d.Mesh)
	}
	b.shapes[name] = m
	return m, nil
}

// defaultPrimitiveMaterial is the surface of a primitive shape that names no material file: white, non-metallic and fully rough.
var defaultPrimitiveMaterial = Material{BaseColor: math3d.Vec4{X: 1, Y: 1, Z: 1, W: 1}, Roughness: 1, AlphaMode: render3d.AlphaOpaque, AlphaCutoff: 0.5}

// primitiveMaterial returns the material of a primitive shape whose object names the material file path, or "" for none.
func (b *builder) primitiveMaterial(path string) (Material, uint32, error) {
	if path == "" {
		return defaultPrimitiveMaterial, b.materialID(materialKey{index: -1}), nil
	}
	return b.fileMaterial(path)
}

// meshID returns the ID of the geometry k, giving it the next one on first use.
func (b *builder) meshID(k meshKey) uint32 {
	if id, ok := b.meshIDs[k]; ok {
		return id
	}
	id := uint32(len(b.meshIDs))
	b.meshIDs[k] = id
	return id
}

// materialID returns the ID of the material k, giving it the next one on first use.
func (b *builder) materialID(k materialKey) uint32 {
	if id, ok := b.matIDs[k]; ok {
		return id
	}
	id := uint32(len(b.matIDs))
	b.matIDs[k] = id
	return id
}

// attach records parts as what node n of object id draws, when there are any.
func (b *builder) attach(id string, n *scene3d.Node, parts []Part) {
	if len(parts) == 0 {
		return
	}
	bounds := math3d.AABBEmpty()
	for i := range parts {
		bounds = bounds.Union(parts[i].Mesh.Bounds())
	}
	d := &Drawable{Node: n, Object: id, Parts: parts}
	n.SetLocalBounds(bounds)
	n.SetAttachment(AttachmentKey, d)
	b.built.Drawables = append(b.built.Drawables, d)
}

// errNoModelResolver is why Build cannot load a model without a ModelResolver.
var errNoModelResolver = errors.New("naming a model requires a ModelResolver")

// model gives the object o, built at node n, the geometry of its model, pairing any LOD levels declared in its import sidecar or by the _LOD1/_LOD2 node-name convention.
func (b *builder) model(o *sceneformat.ObjectDoc, n *scene3d.Node) error {
	doc, err := b.loadModel(o.Model)
	if err != nil {
		return err
	}
	sidecar, err := b.loadImportSettings(o.Model)
	if err != nil {
		return err
	}
	var sidecarLODs []importsettings.ModelLOD
	if sidecar != nil && sidecar.Model != nil {
		sidecarLODs = sidecar.Model.LODs
		if sidecar.Model.LODHysteresis > b.built.LODHysteresis {
			b.built.LODHysteresis = sidecar.Model.LODHysteresis
		}
	}
	g := gltfObject{builder: b, doc: doc, object: o.ID, override: o.Material, sidecarLODs: sidecarLODs}
	if len(sidecarLODs) > 0 && hasExplicitLODTargets(sidecarLODs) {
		return g.buildExplicitSidecarLODs(o, n, sidecarLODs)
	}
	switch {
	case o.ModelMesh != "":
		mi := FindMesh(doc, o.ModelMesh)
		if mi < 0 {
			return fmt.Errorf("model %q has no mesh %q", o.Model, o.ModelMesh)
		}
		parts, err := g.meshParts(mi, nil)
		if err != nil {
			return err
		}
		if coarseMeshes := findLODMeshes(doc, mi); len(coarseMeshes) > 0 {
			coarseParts := make([][]Part, len(coarseMeshes))
			for k, cmi := range coarseMeshes {
				cp, err := g.meshParts(cmi, nil)
				if err != nil {
					return err
				}
				coarseParts[k] = cp
			}
			ratios, dists := resolveThresholds(1+len(coarseMeshes), sidecarLODs)
			attachPartLODs(parts, coarseParts, ratios, dists)
		}
		b.attach(o.ID, n, parts)
		return nil
	case o.ModelNode != "":
		ni := FindNode(doc, o.ModelNode)
		if ni < 0 {
			return fmt.Errorf("model %q has no node %q", o.Model, o.ModelNode)
		}
		var primary map[int][]int
		if coarse := findSiblingLODsForNode(doc, ni); len(coarse) > 0 {
			primary = map[int][]int{ni: coarse}
		}
		return g.nodeWithConsumed(ni, n, false, nil, primary)
	}
	roots := sceneRoots(doc)
	primary, consumed := groupSiblingLODs(doc, roots)
	for _, root := range roots {
		if consumed[root] {
			continue
		}
		if err := g.nodeWithConsumed(root, n, true, consumed, primary); err != nil {
			return err
		}
	}
	return nil
}

// loadModel loads the model at path through the ModelResolver once.
func (b *builder) loadModel(path string) (*gltf.Document, error) {
	if doc, ok := b.docs[path]; ok {
		return doc, nil
	}
	if b.models == nil {
		return nil, fmt.Errorf("model %q: %w", path, errNoModelResolver)
	}
	doc, err := b.models.Model(path)
	if err != nil {
		return nil, fmt.Errorf("model %q: %w", path, err)
	}
	if doc == nil {
		return nil, fmt.Errorf("model %q: the ModelResolver returned no document", path)
	}
	b.docs[path] = doc
	return doc, nil
}

// loadGridMap loads the grid map document at path through the resolver's GridMapResolver once.
func (b *builder) loadGridMap(path string) (*world3d.GridMapDoc, error) {
	if doc, ok := b.gridMaps[path]; ok {
		return doc, nil
	}
	gr, ok := b.models.(GridMapResolver)
	if !ok {
		return nil, fmt.Errorf("gridMap %q requires a ModelResolver that is also a GridMapResolver", path)
	}
	doc, err := gr.GridMap(path)
	if err != nil {
		return nil, fmt.Errorf("gridMap %q: %w", path, err)
	}
	if doc == nil {
		return nil, fmt.Errorf("gridMap %q: the GridMapResolver returned no document", path)
	}
	b.gridMaps[path] = doc
	return doc, nil
}

// gridMap loads the GridMap document and its glTF mesh library for object o, builds the runtime *world3d.GridMap, and instances its tiles under node n via world3d.GridMap.BuildInstances.
func (b *builder) gridMap(o *sceneformat.ObjectDoc, n *scene3d.Node) error {
	gdoc, err := b.loadGridMap(o.GridMap)
	if err != nil {
		return err
	}
	if gdoc.MeshLibrary == "" {
		gm, err := gdoc.Build(nil)
		if err != nil {
			return fmt.Errorf("gridMap %q: %w", o.GridMap, err)
		}
		b.built.GridMaps[o.ID] = gm
		return nil
	}
	gltfDoc, err := b.loadModel(gdoc.MeshLibrary)
	if err != nil {
		return fmt.Errorf("gridMap %q: %w", o.GridMap, err)
	}
	lib := meshLibraryFromGLTF(gltfDoc)
	gm, err := gdoc.Build(lib)
	if err != nil {
		return fmt.Errorf("gridMap %q: %w", o.GridMap, err)
	}
	b.built.GridMaps[o.ID] = gm
	if bounds := gm.Bounds(); !bounds.Empty() {
		n.SetLocalBounds(bounds)
	}

	batches := gm.BuildInstances(nil, nil)
	g := gltfObject{builder: b, doc: gltfDoc, object: o.ID, override: o.Material}
	for _, batch := range batches {
		mi := int(batch.MeshID)
		if mi < 0 || mi >= len(gltfDoc.Meshes) {
			return fmt.Errorf("gridMap %q: invalid mesh index %d", o.GridMap, mi)
		}
		parts, err := g.meshParts(mi, nil)
		if err != nil {
			return fmt.Errorf("gridMap %q: %w", o.GridMap, err)
		}
		if len(parts) == 0 {
			continue
		}
		// Sort instance transforms deterministically by position (Y, Z, X) then matrix entries so Built.Drawables is reproducible across runs.
		transforms := slices.Clone(batch.Transforms)
		slices.SortFunc(transforms, compareMat4)
		for idx, xform := range transforms {
			cn := scene3d.NewNode(fmt.Sprintf("%s_grid_%d_%d", o.ID, batch.MeshID, idx))
			cn.Local = math3d.Transform{
				Pos:   math3d.Vec3{X: xform[12], Y: xform[13], Z: xform[14]},
				Rot:   math3d.QuatFromMat4(xform),
				Scale: math3d.Vec3One(),
			}
			if err := n.AddChild(cn); err != nil {
				return fmt.Errorf("gridMap %q instance %d: %w", o.GridMap, idx, err)
			}
			b.attach(o.ID, cn, parts)
		}
	}
	return nil
}

// meshLibraryFromGLTF builds a *world3d.MeshLibrary from a parsed *gltf.Document using the same node/mesh naming and bounds rules as world3d.LoadMeshLibraryFromGLTF.
func meshLibraryFromGLTF(doc *gltf.Document) *world3d.MeshLibrary {
	lib := world3d.NewMeshLibrary()
	if doc == nil {
		return lib
	}
	addMesh := func(name string, meshIdx int) {
		if meshIdx < 0 || meshIdx >= len(doc.Meshes) {
			return
		}
		mesh := &doc.Meshes[meshIdx]
		if name == "" {
			name = mesh.Name
		}
		if name == "" {
			name = fmt.Sprintf("mesh_%d", meshIdx)
		}
		var matID uint32
		hasMat := false
		bounds := math3d.AABBEmpty()
		for i := range mesh.Primitives {
			prim := &mesh.Primitives[i]
			if !hasMat && prim.Material >= 0 {
				matID = uint32(prim.Material)
				hasMat = true
			}
			if !prim.Bounds.Empty() {
				bounds = bounds.Union(prim.Bounds)
			} else {
				for _, p := range prim.Positions {
					bounds = bounds.ExpandPoint(p)
				}
			}
		}
		var colliders []math3d.AABB
		if !bounds.Empty() {
			colliders = []math3d.AABB{bounds}
		}
		lib.Add(world3d.MeshTileDef{
			Name:          name,
			MeshID:        uint32(meshIdx),
			MaterialID:    matID,
			LocalBounds:   bounds,
			ColliderBoxes: colliders,
		})
	}

	if len(doc.Nodes) > 0 {
		for i := range doc.Nodes {
			node := &doc.Nodes[i]
			if node.Mesh < 0 || node.Mesh >= len(doc.Meshes) {
				continue
			}
			name := node.Name
			if name == "" {
				name = doc.Meshes[node.Mesh].Name
			}
			if name == "" {
				name = fmt.Sprintf("tile_%d", i)
			}
			addMesh(name, node.Mesh)
		}
	} else {
		for meshIdx := range doc.Meshes {
			addMesh(doc.Meshes[meshIdx].Name, meshIdx)
		}
	}
	return lib
}

func compareMat4(a, b math3d.Mat4) int {
	if c := cmp.Or(cmp.Compare(a[13], b[13]), cmp.Compare(a[14], b[14]), cmp.Compare(a[12], b[12])); c != 0 {
		return c
	}
	for i := range 12 {
		if c := cmp.Compare(a[i], b[i]); c != 0 {
			return c
		}
	}
	return 0
}

// FindNode returns the index of the first node of doc named name, or -1 when none is, as sceneformat.ObjectDoc.ModelNode looks it up.
func FindNode(doc *gltf.Document, name string) int {
	if doc == nil {
		return -1
	}
	for i := range doc.Nodes {
		if doc.Nodes[i].Name == name {
			return i
		}
	}
	return -1
}

// FindMesh returns the index of the first mesh of doc named name, or -1 when none is, as sceneformat.ObjectDoc.ModelMesh looks it up.
func FindMesh(doc *gltf.Document, name string) int {
	if doc == nil {
		return -1
	}
	for i := range doc.Meshes {
		if doc.Meshes[i].Name == name {
			return i
		}
	}
	return -1
}

// sceneRoots returns the root nodes a whole model draws: those of its default scene, or of its first scene when it names no default, or every node without a parent when it has no scenes.
func sceneRoots(doc *gltf.Document) []int {
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

// gltfObject builds the glTF nodes of one object's model.
type gltfObject struct {
	*builder
	doc    *gltf.Document
	object string
	// override is the material file that replaces the model's materials, or "".
	override string
	// sidecarLODs holds threshold-only LOD entries from the model's *.import.yaml sidecar, if any.
	sidecarLODs []importsettings.ModelLOD
}

// nodeWithConsumed builds glTF node ni: under n as a new child node with the glTF node's transform when child is true, and on n itself otherwise. Any coarser _LOD sibling nodes listed in primary[ni] are attached as Part.LODs instead of being emitted as separate nodes, and nodes in consumed are skipped.
func (g *gltfObject) nodeWithConsumed(ni int, n *scene3d.Node, child bool, consumed map[int]bool, primary map[int][]int) error {
	gn := &g.doc.Nodes[ni]
	at := n
	if child {
		if gn.MatrixNotTRS {
			return fmt.Errorf("model node %d (%q) has a sheared matrix, which a scene3d node cannot hold", ni, gn.Name)
		}
		at = scene3d.NewNode(gn.Name)
		at.Local = gn.Transform()
		if err := n.AddChild(at); err != nil {
			return fmt.Errorf("model node %d (%q): %w", ni, gn.Name, err)
		}
	}
	if gn.Mesh >= 0 && gn.Mesh < len(g.doc.Meshes) {
		parts, err := g.meshParts(gn.Mesh, gn.Weights)
		if err != nil {
			return fmt.Errorf("model node %d (%q): %w", ni, gn.Name, err)
		}
		if coarseNodes := primary[ni]; len(coarseNodes) > 0 {
			coarseParts := make([][]Part, len(coarseNodes))
			for k, cni := range coarseNodes {
				cn := &g.doc.Nodes[cni]
				cp, err := g.meshParts(cn.Mesh, cn.Weights)
				if err != nil {
					return fmt.Errorf("model node %d (%q) LOD %d (%q): %w", ni, gn.Name, k+1, cn.Name, err)
				}
				coarseParts[k] = cp
			}
			ratios, dists := resolveThresholds(1+len(coarseNodes), g.sidecarLODs)
			attachPartLODs(parts, coarseParts, ratios, dists)
		}
		g.attach(g.object, at, parts)
	}
	childPrimary, childConsumed := groupSiblingLODs(g.doc, gn.Children)
	for _, c := range gn.Children {
		if consumed[c] || childConsumed[c] {
			continue
		}
		if err := g.nodeWithConsumed(c, at, true, consumed, childPrimary); err != nil {
			return err
		}
	}
	return nil
}

// meshParts returns a part for each triangle primitive of mesh mi, with morph weights, when non-empty, in place of the mesh's own.
func (g *gltfObject) meshParts(mi int, weights []float32) ([]Part, error) {
	gm := &g.doc.Meshes[mi]
	if len(weights) == 0 {
		weights = gm.Weights
	}
	var parts []Part
	for pi := range gm.Primitives {
		prim := &gm.Primitives[pi]
		if prim.Mode != gltf.ModeTriangles && prim.Mode != gltf.ModeTriangleStrip && prim.Mode != gltf.ModeTriangleFan {
			continue
		}
		var mat Material
		var matID uint32
		var err error
		if g.override != "" {
			mat, matID, err = g.fileMaterial(g.override)
		} else {
			mat, matID, err = g.gltfMaterial(prim.Material)
		}
		if err != nil {
			return nil, fmt.Errorf("mesh %d (%q) primitive %d: %w", mi, gm.Name, pi, err)
		}
		// The base color texture may read another UV set or transform it; a material file's texture reads TEXCOORD_0.
		var uvRef *gltf.TextureRef
		uvFrom := -1
		if g.override == "" && prim.Material >= 0 && prim.Material < len(g.doc.Materials) {
			if ref := g.doc.Materials[prim.Material].BaseColorTexture; ref != nil && (ref.EffectiveTexCoord() != 0 || ref.Transform != nil) {
				uvRef, uvFrom = ref, prim.Material
			}
		}
		key := meshKey{doc: g.doc, mesh: mi, prim: pi, uvFrom: uvFrom}
		if len(prim.Targets) > 0 && anyNonZero(weights) {
			key.weights = fmt.Sprint(weights)
		}
		mesh, err := g.primitiveMesh(prim, uvRef, weights)
		if err != nil {
			return nil, fmt.Errorf("mesh %d (%q) primitive %d: %w", mi, gm.Name, pi, err)
		}
		parts = append(parts, Part{Mesh: mesh, MeshID: g.meshID(key), Material: mat, MaterialID: matID})
	}
	return parts, nil
}

// anyNonZero reports whether any weight is not zero.
func anyNonZero(weights []float32) bool {
	for _, w := range weights {
		if w != 0 {
			return true
		}
	}
	return false
}

// primitiveMesh converts a glTF triangle primitive into a triangle list, with UVs from uvRef's UV set and transform when it is not nil, and morph weights applied.
func (g *gltfObject) primitiveMesh(prim *gltf.Primitive, uvRef *gltf.TextureRef, weights []float32) (render3d.Mesh, error) {
	m := render3d.Mesh{Positions: prim.Positions, Normals: prim.Normals, UVs: prim.TexCoords0}
	if len(prim.Targets) > 0 && anyNonZero(weights) {
		m.Positions = gltf.EvaluateMorphPositions(*prim, weights, nil)
		if len(prim.Normals) > 0 {
			m.Normals = gltf.EvaluateMorphNormals(*prim, weights, nil)
		}
	}
	if uvRef != nil {
		uvs, err := g.doc.TransformTexCoords(*prim, uvRef, nil)
		if err != nil {
			return render3d.Mesh{}, err
		}
		m.UVs = uvs
	}
	indices, err := triangleList(prim.Mode, prim.Indices, len(m.Positions))
	if err != nil {
		return render3d.Mesh{}, err
	}
	m.Indices = indices
	if len(m.Normals) == 0 {
		m = flatShaded(m)
	}
	if err := m.Validate(); err != nil {
		return render3d.Mesh{}, err
	}
	return m, nil
}

// triangleList returns the indices of a triangle list drawing the triangles of mode, with indices or, when it has none, its count vertices in order. A list keeps its indices, nil when it has none; a strip or fan is unrolled, with every other strip triangle's first two vertices swapped to keep its winding.
func triangleList(mode gltf.PrimitiveMode, indices []uint32, count int) ([]uint32, error) {
	if mode == gltf.ModeTriangles {
		return indices, nil
	}
	at := func(i int) uint32 { return uint32(i) }
	n := count
	if len(indices) > 0 {
		at = func(i int) uint32 { return indices[i] }
		n = len(indices)
	}
	if n < 3 {
		return nil, fmt.Errorf("a triangle strip or fan needs 3 vertices, not %d", n)
	}
	out := make([]uint32, 0, 3*(n-2))
	for i := 0; i+2 < n; i++ {
		switch {
		case mode == gltf.ModeTriangleFan:
			out = append(out, at(0), at(i+1), at(i+2))
		case i%2 == 0:
			out = append(out, at(i), at(i+1), at(i+2))
		default:
			out = append(out, at(i+1), at(i), at(i+2))
		}
	}
	return out, nil
}

// flatShaded returns m with one vertex per triangle corner and each triangle's face normal, as glTF says to draw a primitive without normals.
func flatShaded(m render3d.Mesh) render3d.Mesh {
	idx := m.Indices
	if len(idx) == 0 {
		idx = make([]uint32, len(m.Positions)-len(m.Positions)%3)
		for i := range idx {
			idx[i] = uint32(i)
		}
	}
	out := render3d.Mesh{
		Positions: make([]math3d.Vec3, len(idx)),
		Normals:   make([]math3d.Vec3, len(idx)),
	}
	if len(m.UVs) == len(m.Positions) {
		out.UVs = make([]math3d.Vec2, len(idx))
	}
	for t := 0; t+2 < len(idx); t += 3 {
		var p [3]math3d.Vec3
		for k := range 3 {
			v := idx[t+k]
			if int(v) >= len(m.Positions) {
				return m // Validate reports the bad index.
			}
			p[k] = m.Positions[v]
			out.Positions[t+k] = p[k]
			if out.UVs != nil {
				out.UVs[t+k] = m.UVs[v]
			}
		}
		n := p[1].Sub(p[0]).Cross(p[2].Sub(p[0]))
		if l := n.Len(); l > 0 && !math.IsInf(float64(l), 0) {
			n = n.Scale(1 / l)
		} else {
			n = math3d.Vec3{Y: 1}
		}
		out.Normals[t], out.Normals[t+1], out.Normals[t+2] = n, n, n
	}
	return out
}

// gltfMaterial returns the glTF material mi of the model, or glTF's default material for -1.
func (g *gltfObject) gltfMaterial(mi int) (Material, uint32, error) {
	if mi < 0 || mi >= len(g.doc.Materials) {
		mi = -1
	}
	key := materialKey{doc: g.doc, index: mi}
	if m, ok := g.gltfMats[key]; ok {
		return m, g.materialID(key), nil
	}
	m := Material{BaseColor: math3d.Vec4{X: 1, Y: 1, Z: 1, W: 1}, Metallic: 1, Roughness: 1, AlphaMode: render3d.AlphaOpaque, AlphaCutoff: 0.5}
	if mi >= 0 {
		gm := &g.doc.Materials[mi]
		mode, err := render3d.AlphaModeFromGLTF(gm.AlphaMode)
		if err != nil {
			return Material{}, 0, err
		}
		strength, err := gm.EmissiveStrength()
		if err != nil {
			return Material{}, 0, err
		}
		m = Material{
			BaseColor:   gm.BaseColorFactor,
			Metallic:    gm.MetallicFactor,
			Roughness:   gm.RoughnessFactor,
			Emissive:    gm.EmissiveFactor.Scale(strength),
			AlphaMode:   mode,
			AlphaCutoff: gm.AlphaCutoff,
			DoubleSided: gm.DoubleSided,
		}
		if gm.BaseColorTexture != nil {
			tex, err := g.gltfTexture(gm.BaseColorTexture.TextureIndex)
			if err != nil {
				return Material{}, 0, fmt.Errorf("material %d (%q) base color texture: %w", mi, gm.Name, err)
			}
			m.BaseColorTexture = tex
		}
	}
	g.gltfMats[key] = m
	return m, g.materialID(key), nil
}

// gltfTexture returns the decoded image and sampler of the model's texture ti, or nil for a texture without an image.
func (g *gltfObject) gltfTexture(ti int) (*Texture, error) {
	if ti < 0 || ti >= len(g.doc.Textures) {
		return nil, fmt.Errorf("texture %d is out of range", ti)
	}
	t := &g.doc.Textures[ti]
	if t.Source < 0 {
		return nil, nil
	}
	key := textureKey{doc: g.doc, image: t.Source, sampler: t.Sampler}
	if tex, ok := g.textures[key]; ok {
		return tex, nil
	}
	var s gltf.Sampler
	if t.Sampler >= 0 && t.Sampler < len(g.doc.Samplers) {
		s = g.doc.Samplers[t.Sampler]
	}
	opts, mips, err := render3d.SamplerOptionsFromGLTF(s)
	if err != nil {
		return nil, err
	}
	img, err := g.doc.DecodeImage(t.Source)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("image %d", t.Source)
	if n := g.doc.Images[t.Source].Name; n != "" {
		name = fmt.Sprintf("image %d (%s)", t.Source, n)
	}
	tex := &Texture{Name: name, Image: img, Sampler: opts, Mipmaps: mips}
	g.textures[key] = tex
	return tex, nil
}

// fileMaterial returns the material of the material file at path, loaded through the ModelResolver's sceneformat.MaterialResolver.
func (b *builder) fileMaterial(path string) (Material, uint32, error) {
	key := materialKey{file: path, index: -1}
	if m, ok := b.fileMats[path]; ok {
		return m, b.materialID(key), nil
	}
	mr, ok := b.models.(sceneformat.MaterialResolver)
	if !ok {
		return Material{}, 0, fmt.Errorf("material %q requires a ModelResolver that is also a sceneformat.MaterialResolver", path)
	}
	rm, err := mr.Material(path)
	if err != nil {
		return Material{}, 0, fmt.Errorf("material %q: %w", path, err)
	}
	m, err := b.materialFromFile(rm)
	if err != nil {
		return Material{}, 0, fmt.Errorf("material %q: %w", path, err)
	}
	b.fileMats[path] = m
	return m, b.materialID(key), nil
}

// MaterialFromFile converts a material file into a Material without its texture: the base color and emissive CSS colors converted from sRGB to linear light, the base color's alpha times the opacity, render3d.AlphaBlend when that alpha is below 1, the metalness, roughness and double-sidedness as written. Its 2D blend mode does not apply in 3D, and a 3D surface is always lit, so Unlit is not used either.
func MaterialFromFile(rm *resource.Material) Material {
	r, g, bl, a := rm.Color().Components()
	m := Material{
		BaseColor:   math3d.Vec4{X: srgbToLinear(r), Y: srgbToLinear(g), Z: srgbToLinear(bl), W: float32(a) * rm.EffectiveOpacity()},
		Metallic:    rm.Metallic,
		Roughness:   rm.EffectiveRoughness(),
		AlphaMode:   render3d.AlphaOpaque,
		AlphaCutoff: 0.5,
		DoubleSided: rm.DoubleSided,
	}
	if m.BaseColor.W < 1 {
		m.AlphaMode = render3d.AlphaBlend
	}
	if c, ok := rm.EmissiveColor(); ok {
		er, eg, eb, _ := c.Components()
		m.Emissive = math3d.Vec3{X: srgbToLinear(er), Y: srgbToLinear(eg), Z: srgbToLinear(eb)}
	}
	return m
}

// materialFromFile converts the material file rm, loading its texture through the ModelResolver's ImageResolver.
func (b *builder) materialFromFile(rm *resource.Material) (Material, error) {
	if rm == nil {
		return Material{}, errors.New("the MaterialResolver returned no material")
	}
	m := MaterialFromFile(rm)
	if rm.Texture == "" {
		return m, nil
	}
	if tex, ok := b.fileTexts[rm.Texture]; ok {
		m.BaseColorTexture = tex
		return m, nil
	}
	ir, ok := b.models.(ImageResolver)
	if !ok {
		return Material{}, fmt.Errorf("texture %q requires a ModelResolver that is also an ImageResolver", rm.Texture)
	}
	img, err := ir.Image(rm.Texture)
	if err != nil {
		return Material{}, fmt.Errorf("texture %q: %w", rm.Texture, err)
	}
	opts := render3d.SamplerLinearRepeat()
	tex := &Texture{Name: rm.Texture, Image: img, Sampler: opts, Mipmaps: true}
	b.fileTexts[rm.Texture] = tex
	m.BaseColorTexture = tex
	return m, nil
}

// srgbToLinear converts one sRGB channel from 0 to 1 into linear light.
func srgbToLinear(c float64) float32 {
	return render3d.SRGBToLinearChannel(float32(c))
}

// AppendDrawItems appends a render3d.DrawItem for every part of every drawable whose node is visible and enabled, in Drawables order, and returns the extended slice for render3d.Queue.Build. An item's ID holds the drawable's index in Drawables in its high 32 bits and the part's index in its low 32 bits (DrawItemID); its PipelineID is DrawPipeline of the part's material; its Transform is the node's world matrix and WorldBounds the part's bounds under it.
func (b *Built) AppendDrawItems(dst []render3d.DrawItem) []render3d.DrawItem {
	if b == nil {
		return dst
	}
	for di, d := range b.Drawables {
		if !d.Node.EffectiveVisible() || !d.Node.EffectiveEnabled() {
			continue
		}
		world := d.Node.WorldMatrix()
		for pi := range d.Parts {
			p := &d.Parts[pi]
			tris := uint32(len(p.Mesh.Indices) / 3)
			if len(p.Mesh.Indices) == 0 {
				tris = uint32(len(p.Mesh.Positions) / 3)
			}
			dst = append(dst, render3d.DrawItem{
				ID:          DrawItemID(di, pi),
				MeshID:      p.MeshID,
				MaterialID:  p.MaterialID,
				PipelineID:  DrawPipeline(p.Material),
				Blend:       p.Material.AlphaMode.BlendMode(),
				Transform:   world,
				WorldBounds: p.Mesh.Bounds().Transform(world),
				Triangles:   tris,
				Vertices:    uint32(len(p.Mesh.Positions)),
				LODs:        p.lodLevels,
			})
		}
	}
	return dst
}

// DrawItemID returns the render3d.DrawItem ID AppendDrawItems gives part pi of drawable di.
func DrawItemID(di, pi int) uint64 { return uint64(di)<<32 | uint64(uint32(pi)) }

// Part returns the drawable and part a render3d.DrawItem ID from AppendDrawItems names, and false when it names none.
func (b *Built) Part(id uint64) (*Drawable, *Part, bool) {
	if b == nil {
		return nil, nil, false
	}
	di, pi := int(id>>32), int(uint32(id))
	if di >= len(b.Drawables) || pi >= len(b.Drawables[di].Parts) {
		return nil, nil, false
	}
	d := b.Drawables[di]
	return d, &d.Parts[pi], true
}

// DrawPipeline returns the render3d.DrawItem PipelineID of a part drawn with m: 0 for a single-sided opaque or cutout surface, 1 for a double-sided one, 2 for a single-sided blended surface and 3 for a double-sided one. Renderer draws each with its own pipeline.
func DrawPipeline(m Material) uint32 {
	var id uint32
	if m.DoubleSided {
		id = 1
	}
	if m.AlphaMode == render3d.AlphaBlend {
		id += 2
	}
	return id
}
