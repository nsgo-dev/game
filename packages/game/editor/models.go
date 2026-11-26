package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/scenemodel"
	"github.com/nsgo-dev/nsgo/core"
)

// ModelEntry is one model the asset browser lists for placing in 3D (Models): a built-in primitive, which an object draws through sceneformat.ObjectDoc.Mesh, or a glTF file of the open project, which it draws through sceneformat.ObjectDoc.Model. Exactly one of Mesh and Path is set.
type ModelEntry struct {
	// Mesh names the built-in primitive, one of sceneformat.MeshPrimitives, or is empty for a file.
	Mesh string
	// Path is the slash-separated path of the .gltf or .glb file from the project root, such as "models/crate.glb", or is empty for a primitive.
	Path string
	// Output says Path is what a model target of game.yaml's assets section writes (project.ModelTarget.Out), which `game assets` builds and the asset browser lists before it has been built.
	Output bool
}

// Key identifies the entry among those Models returns: "mesh:" and the primitive's name, or the file's path.
func (m ModelEntry) Key() string {
	if m.Mesh != "" {
		return "mesh:" + m.Mesh
	}
	return m.Path
}

// Name is what the asset browser shows for the entry and what a placed object is named after: the primitive's name, or the file's name without its extension ("crate" for "models/crate.glb").
func (m ModelEntry) Name() string {
	if m.Mesh != "" {
		return m.Mesh
	}
	base := path.Base(m.Path)
	return strings.TrimSuffix(base, path.Ext(base))
}

// FileLabel is the concise filename label shown on 3D MODELS strip cards and tooltips: the file's basename ("fountain.glb") for glTF models, or the primitive's name ("box") for built-in primitives.
func (m ModelEntry) FileLabel() string {
	if m.Mesh != "" {
		return m.Mesh
	}
	return path.Base(m.Path)
}

// object returns the object that draws m, with no ID or transform.
func (m ModelEntry) object() sceneformat.ObjectDoc {
	return sceneformat.ObjectDoc{Name: m.Name(), Mesh: m.Mesh, Model: m.Path}
}

// valid reports whether m names exactly one primitive or model file of the kinds an object can draw.
func (m ModelEntry) valid() error {
	switch {
	case m.Mesh != "" && m.Path != "":
		return fmt.Errorf("editor: model entry names both mesh %q and file %q", m.Mesh, m.Path)
	case m.Mesh != "":
		if !slices.Contains(sceneformat.MeshPrimitives(), m.Mesh) {
			return fmt.Errorf("editor: mesh %q is not a built-in primitive (want one of %s)", m.Mesh, strings.Join(sceneformat.MeshPrimitives(), ", "))
		}
	case m.Path != "":
		if !fs.ValidPath(m.Path) || m.Path == "." {
			return fmt.Errorf("editor: model %q must be a slash-separated path relative to the project root", m.Path)
		}
		if !sceneformat.IsModelPath(m.Path) {
			return fmt.Errorf("editor: model %q must be a glTF file ending in .gltf or .glb", m.Path)
		}
	default:
		return errors.New("editor: model entry names neither a mesh nor a file")
	}
	return nil
}

// modelFileCache holds the open project's model files as they were last listed, with the project path and the file and asset revisions they were listed at.
type modelFileCache struct {
	key   string
	paths []string
}

// ProjectModels returns the slash-separated paths of the open project's glTF models, sorted: every .gltf and .glb file ProjectFiles lists, and the output of every model target of game.yaml's assets section (project.ModelTarget.Out), whether or not `game assets` has built it yet. It returns nil without a project. The files are listed again only when the editor changes which files the project holds or reloads its built assets (ReloadAssets), or after RefreshSearch.
func (e *Editor) ProjectModels() []string {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return nil
	}
	key := e.ProjectPath + "\x00" + strconv.FormatUint(e.filesRev, 10) + "\x00" + strconv.FormatUint(e.assetsRev, 10)
	if e.modelFiles.key != key {
		var paths []string
		if files, err := e.ProjectFiles(); err == nil {
			for _, f := range files {
				if !f.Dir && sceneformat.IsModelPath(f.Path) {
					paths = append(paths, f.Path)
				}
			}
		}
		e.modelFiles = modelFileCache{key: key, paths: paths}
	}
	out := slices.Clone(e.modelFiles.paths)
	out = append(out, e.modelOutputs()...)
	slices.Sort(out)
	return slices.Compact(out)
}

// modelOutputs returns the outputs of the open project's model targets that name glTF files.
func (e *Editor) modelOutputs() []string {
	if e.Project == nil || e.Project.Assets == nil {
		return nil
	}
	var out []string
	for _, t := range e.Project.Assets.Models {
		if t.Out != "" && fs.ValidPath(t.Out) && sceneformat.IsModelPath(t.Out) {
			out = append(out, t.Out)
		}
	}
	return out
}

// Models returns what the asset browser offers to place in 3D: the built-in primitives in sceneformat.MeshPrimitives order, then the open project's models (ProjectModels) by path, each output of a model target marked Output.
func (e *Editor) Models() []ModelEntry {
	if e == nil {
		return nil
	}
	prims := sceneformat.MeshPrimitives()
	out := make([]ModelEntry, 0, len(prims))
	for _, m := range prims {
		out = append(out, ModelEntry{Mesh: m})
	}
	outputs := e.modelOutputs()
	for _, p := range e.ProjectModels() {
		out = append(out, ModelEntry{Path: p, Output: slices.Contains(outputs, p)})
	}
	return out
}

// StripModels returns the models offered in the 3D viewport's bottom MODELS strip: the open project's models first (in ProjectModels order, with model target outputs marked Output), followed by the built-in primitives in sceneformat.MeshPrimitives order.
func (e *Editor) StripModels() []ModelEntry {
	if e == nil {
		return nil
	}
	proj := e.ProjectModels()
	prims := sceneformat.MeshPrimitives()
	outputs := e.modelOutputs()
	out := make([]ModelEntry, 0, len(proj)+len(prims))
	for _, p := range proj {
		out = append(out, ModelEntry{Path: p, Output: slices.Contains(outputs, p)})
	}
	for _, m := range prims {
		out = append(out, ModelEntry{Mesh: m})
	}
	return out
}

// SelectModel makes m the model the Place tool adds in the 3D viewport (SelectedModel), or with an empty entry clears it. It refuses an entry that names neither or both of a primitive and a file, or one of a kind an object cannot draw.
func (e *Editor) SelectModel(m ModelEntry) error {
	if m == (ModelEntry{}) {
		if e.SelectedModel != nil {
			e.SelectedModel = nil
			e.Status = "Cleared the selected model"
			e.notify()
		}
		return nil
	}
	if err := m.valid(); err != nil {
		return err
	}
	e.SelectedModel = &m
	e.Status = "Selected model " + m.Key()
	e.notify()
	return nil
}

// Placement3D is where the Place tool puts a model in the 3D viewport (PlacementAt3D): a point on the geometry under the pointer, or on the ground plane where it hits none.
type Placement3D struct {
	// Point is the world-space point the model rests on.
	Point [3]float32
	// Normal is the outward unit normal of the surface at Point, straight up on the ground plane.
	Normal [3]float32
	// OnSurface says Point lies on the scene's geometry rather than on the ground plane.
	OnSurface bool
}

// PlacementAt3D returns where the Place tool puts a model for viewport pixel (sx, sy) of a viewport w by h: the nearest point where the camera's ray through the pixel (ScreenRay) hits the scene's geometry, passing over the objects hidden or locked in the editor as picking does, or where it crosses the ground plane, the horizontal plane at PlaceHeight3D, when it hits none. With PlaceSnap3D a point on the ground plane has its X and Z snapped to the 3D grid's spacing, which follows the camera's distance as the grid the viewport draws does; a point on the scene's geometry is not snapped, since the grid's spacing can far exceed the object under the pointer and a snapped point would leave its surface. It reports false when the ray hits no geometry and runs parallel to the ground plane or away from it.
func (e *Editor) PlacementAt3D(sx, sy, w, h float32) (Placement3D, bool) {
	if e == nil || e.Doc == nil || !(w > 0 && h > 0) || !isFiniteFloat32(sx) || !isFiniteFloat32(sy) {
		return Placement3D{}, false
	}
	orig32, dir32 := e.Camera3D.ScreenRay(sx, sy, w, h)
	orig, dir := vec64(orig32), vec64(dir32)
	var p Placement3D
	if hit, _, ok := e.rayGeometry3D(e.scene3D(), orig, dir); ok {
		n := normalize64(vec64([3]float32{hit.Normal.X, hit.Normal.Y, hit.Normal.Z}))
		if len64(n) == 0 {
			n = [3]float64{0, 1, 0}
		}
		p = Placement3D{Point: [3]float32{hit.Point.X, hit.Point.Y, hit.Point.Z}, Normal: vec32(n), OnSurface: true}
	} else {
		height := float64(e.PlaceHeight3D)
		if !isFiniteFloat32(e.PlaceHeight3D) {
			height = 0
		}
		if math.Abs(dir[1]) < 1e-9 {
			return Placement3D{}, false
		}
		t := (height - orig[1]) / dir[1]
		if t <= 0 || math.IsInf(t, 0) || math.IsNaN(t) {
			return Placement3D{}, false
		}
		at := add64(orig, scale64(dir, t))
		at[1] = height
		if e.PlaceSnap3D && !e.snapInvert {
			step, _ := e.gridStep3D()
			at[0], at[2] = float64(snapTo(float32(at[0]), step)), float64(snapTo(float32(at[2]), step))
		} else if !e.PlaceSnap3D {
			if grid, pixel := e.positionSnapMode(); grid {
				step := e.EffectiveGridSize()
				at[0], at[2] = float64(snapTo(float32(at[0]), step)), float64(snapTo(float32(at[2]), step))
			} else if pixel {
				at[0], at[2] = snapStep64(at[0], 1), snapStep64(at[2], 1)
			}
		}
		p = Placement3D{Point: vec32(at), Normal: [3]float32{0, 1, 0}}
	}
	return p, true
}

// snapTo rounds v to the nearest multiple of step.
func snapTo(v, step float32) float32 {
	if !(step > 0) {
		return v
	}
	return float32(math.Round(float64(v)/float64(step))) * step
}

// rayGeometry3D returns the nearest hit of the ray from orig along dir on the geometry of the 3D build s and the resolved scene object it belongs to, passing over the geometry of objects hidden or locked in the editor or under one. When multiple drawables tie at the same hit distance, the deepest object in the hierarchy wins.
func (e *Editor) rayGeometry3D(s *scene3DState, orig, dir [3]float64) (scene3d.RayHit, string, bool) {
	if s.world == nil {
		return scene3d.RayHit{}, "", false
	}
	ray := math3d.Ray{Origin: math3d.Vec3{X: float32(orig[0]), Y: float32(orig[1]), Z: float32(orig[2])}, Dir: math3d.Vec3{X: float32(dir[0]), Y: float32(dir[1]), Z: float32(dir[2])}}
	var (
		bestHit   scene3d.RayHit
		bestOwner string
		bestDepth int
		found     bool
	)
	const tieEps float32 = 1e-4
	for _, hit := range s.world.Raycast(ray, math.MaxUint32) {
		if found && hit.T > bestHit.T+tieEps {
			break
		}
		d, ok := scene3d.AttachmentAs[*scenemodel.Drawable](hit.Node, scenemodel.AttachmentKey)
		if !ok {
			continue
		}
		owner := e.docObjectFor(d.Object)
		if owner == "" || e.pickBlocked3D(owner) {
			continue
		}
		depth := len(e.resolvedObjectPath(owner))
		if !found || depth > bestDepth {
			bestHit, bestOwner, bestDepth, found = hit, owner, depth, true
		}
	}
	return bestHit, bestOwner, found
}

// modelAssets3D returns what the 3D build loads models, material files and images through: the editor's Resolver when it can, and the open project's files otherwise, sharing the 3D build's cache of them while it is current. It does not build the scene, so checking a model for SetObjectModel costs a 2D session no 3D build.
func (e *Editor) modelAssets3D() scene3DAssets {
	s := &e.s3d
	files := s.files
	if files == nil || s.projectPath != e.ProjectPath || s.assetsRev != e.assetsRev {
		files = nil
		if e.ProjectPath != "" {
			key := e.ProjectPath + "\x00" + strconv.FormatUint(e.assetsRev, 10)
			if e.modelStats3D.assetsKey != key || e.modelStats3D.assets == nil {
				e.modelStats3D.assetsKey = key
				e.modelStats3D.assets = scenemodel.NewAssets(newConfinedProjectDirFS(filepath.Dir(e.ProjectPath)))
			}
			files = e.modelStats3D.assets
		}
	}
	return scene3DAssets{models: e.ModelResolver, resolver: e.Resolver, files: files}
}

// buildModel3D builds m alone, drawn by one object at the origin with no transform, through the 3D build's assets (modelAssets3D), and returns the build and the bounds of its geometry in the object's space. It fails when m's model does not load.
func (e *Editor) buildModel3D(m ModelEntry) (*scenemodel.Built, math3d.AABB, error) {
	return buildModelWith3D(m, e.modelAssets3D())
}

// buildModelWith3D builds m alone as buildModel3D does, loading through assets.
func buildModelWith3D(m ModelEntry, assets scene3DAssets) (*scenemodel.Built, math3d.AABB, error) {
	if err := m.valid(); err != nil {
		return nil, math3d.AABB{}, err
	}
	obj := m.object()
	obj.ID = "model"
	doc := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers:  []sceneformat.LayerDoc{{Name: "model", Objects: []sceneformat.ObjectDoc{obj}}},
	}
	world := scene3d.NewScene()
	built, err := scenemodel.Build(doc, world.Root(), assets)
	if err != nil {
		return nil, math3d.AABB{}, err
	}
	bounds := math3d.AABBEmpty()
	for _, d := range built.Drawables {
		bounds = bounds.Union(d.Node.LocalBounds.Transform(d.Node.WorldMatrix()))
	}
	return built, bounds, nil
}

// alignUp3D returns the rotations about X and Z (applied X first, as sceneformat.ObjectDoc's are) that turn an object's +Y axis to the unit vector n.
func alignUp3D(n [3]float32) (rotX, rotZ float32) {
	nx, ny, nz := float64(n[0]), float64(n[1]), float64(n[2])
	return float32(math.Asin(clamp64(nz, -1, 1))), float32(math.Atan2(-nx, ny))
}

// PlaceModel3D adds an object that draws m to the active layer as one undoable step, resting on p: the object is placed so that the bounds of its geometry touch p from the side the surface faces (Placement3D.Normal), raised along the normal by how far the geometry reaches below its origin, so a unit box on the ground sits on it rather than half under it. With PlaceAlign3D and p on the scene's geometry, the object turns so that its +Y axis follows the surface's normal. The object is named after m (ModelEntry.Name), gets a new ID from it and is selected; placing a glTF model in a version 1 scene raises it to sceneformat.CurrentVersion, as PlaceObject does. It returns the new object's ID.
//
// It refuses while playing, when the active layer is locked (SetLayerLocked) or draws in screen space, and when m's model does not load.
func (e *Editor) PlaceModel3D(m ModelEntry, p Placement3D) (string, error) {
	if e.playing {
		return "", ErrPlaying
	}
	for _, v := range append(p.Point[:], p.Normal[:]...) {
		if !isFiniteFloat32(v) {
			return "", errors.New("editor: placement must be finite")
		}
	}
	li, err := e.resolveLayerIndex("")
	if err != nil {
		return "", err
	}
	layer := &e.Doc.Layers[li]
	if e.layerLocked[layer.Name] {
		return "", fmt.Errorf("editor: layer %q is locked", layer.Name)
	}
	if layer.EffectiveSpace() == sceneformat.SpaceScreen {
		return "", fmt.Errorf("editor: layer %q draws in screen space; place 3D models on a world layer", layer.Name)
	}
	_, bounds, err := e.buildModel3D(m)
	if err != nil {
		return "", fmt.Errorf("editor: load %s: %w", m.Key(), err)
	}
	obj := m.object()
	n := normalize64(vec64(p.Normal))
	if len64(n) == 0 {
		n = [3]float64{0, 1, 0}
	}
	if e.PlaceAlign3D && p.OnSurface {
		obj.RotX, obj.RotZ = alignUp3D(vec32(n))
	}
	// How far the geometry, turned as the object is, reaches below its origin along the normal.
	lift := 0.0
	if !bounds.Empty() {
		reach := math.Inf(1)
		for _, s := range boxSigns3D {
			corner := [3]float64{
				float64(bounds.Min.X) + float64(bounds.Max.X-bounds.Min.X)*(s[0]+1)/2,
				float64(bounds.Min.Y) + float64(bounds.Max.Y-bounds.Min.Y)*(s[1]+1)/2,
				float64(bounds.Min.Z) + float64(bounds.Max.Z-bounds.Min.Z)*(s[2]+1)/2,
			}
			reach = min(reach, dot64(rotateEulerXYZ64(corner, float64(obj.RotX), float64(obj.RotY), float64(obj.RotZ)), n))
		}
		lift = -reach
	}
	at := add64(vec64(p.Point), scale64(n, lift))
	obj.X, obj.Y, obj.PosZ = float32(at[0]), float32(at[1]), float32(at[2])
	return e.PlaceObject(layer.Name, obj)
}

// PlaceModelAt3D places m where the Place tool puts it for viewport pixel (sx, sy) of a viewport w by h (PlacementAt3D, PlaceModel3D), and returns the new object's ID. It fails when the pixel's ray hits neither the scene's geometry nor the ground plane.
func (e *Editor) PlaceModelAt3D(m ModelEntry, sx, sy, w, h float32) (string, error) {
	if e.playing {
		return "", ErrPlaying
	}
	p, ok := e.PlacementAt3D(sx, sy, w, h)
	if !ok {
		return "", errors.New("editor: nothing to place the model on under the pointer; turn the view toward the ground")
	}
	return e.PlaceModel3D(m, p)
}

// DropModelFiles places the glTF files a drop from another app names (core.View.OnFileDrop's URLs) in the 3D viewport, as the Place tool would at viewport pixel (sx, sy) of a viewport w by h, all in one undoable step, and returns the new objects' IDs. Only .gltf and .glb files inside the open project's folder, outside the folders ProjectFiles leaves out, are placed, by their paths from its root; it fails without placing anything when a URL names no such file, such as a file outside the project, a file of another kind or a web drop, which names no path.
func (e *Editor) DropModelFiles(uris []string, sx, sy, w, h float32) ([]string, error) {
	if e.playing {
		return nil, ErrPlaying
	}
	if e.Project == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	if len(uris) == 0 {
		return nil, nil
	}
	entries := make([]ModelEntry, 0, len(uris))
	for _, uri := range uris {
		rel, err := e.projectFileOfURL(uri)
		if err != nil {
			return nil, err
		}
		if !sceneformat.IsModelPath(rel) {
			return nil, fmt.Errorf("editor: %s is not a glTF model (.gltf or .glb)", rel)
		}
		entries = append(entries, ModelEntry{Path: rel})
	}
	p, ok := e.PlacementAt3D(sx, sy, w, h)
	if !ok {
		return nil, errors.New("editor: nothing to place the model on under the drop; turn the view toward the ground")
	}
	var ids []string
	label := "Place " + entries[0].Name()
	if len(entries) > 1 {
		label = fmt.Sprintf("Place %d models", len(entries))
	}
	err := e.History.Group(label, func() error {
		for _, m := range entries {
			id, err := e.PlaceModel3D(m, p)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// projectFileOfURL returns the slash-separated path, from the open project's root, of the local file a dropped file's URL names, or an error when it names no local file inside the project or one in a folder ProjectFiles leaves out, so a dropped model is one the asset browser lists.
func (e *Editor) projectFileOfURL(uri string) (string, error) {
	f, err := core.FileFromURL(uri)
	if err != nil {
		return "", fmt.Errorf("editor: dropped file: %w", err)
	}
	if f.Path == "" {
		return "", fmt.Errorf("editor: dropped file %q has no path on disk; add it to the project and place it from the asset browser", f.Name)
	}
	root := filepath.Dir(e.ProjectPath)
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	file := f.Path
	if r, err := filepath.EvalSymlinks(file); err == nil {
		file = r
	}
	rel, err := filepath.Rel(root, file)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("editor: dropped file %s lies outside the project; copy it into the project first", f.Path)
	}
	rel = filepath.ToSlash(rel)
	dirs := strings.Split(rel, "/")
	for _, d := range dirs[:len(dirs)-1] {
		if skippedProjectDir(d) {
			return "", fmt.Errorf("editor: dropped file %s lies in %s, a folder the project does not list (names starting with \".\" or \"_\", and testdata); move it to another folder of the project first", rel, d)
		}
	}
	return rel, nil
}

// SetObjectModel names the glTF model object id draws in 3D, by its slash-separated path from the project root, such as "models/crate.glb", or with an empty path names none, as one undoable step. A named model must load through the 3D build's assets (the Resolver when it is a scenemodel.ModelResolver, the project's files otherwise). Changing the model clears the object's modelNode and modelMesh, which name parts of the model it drew; naming a model in a version 1 scene raises it to version 2, the first that has models, which Undo restores. It refuses a model on an object that draws a built-in primitive, as SetObjectMesh refuses the reverse.
func (e *Editor) SetObjectModel(id, model string) error {
	if e.playing {
		return ErrPlaying
	}
	obj, _, _ := e.FindObject(id)
	if obj == nil {
		if info := e.ObjectOverrideInfo(id); info.Origin != ObjectLocal {
			return e.editObjectOverride(id, "Set model of "+id, modelsVersion, func(ov *sceneformat.ObjectOverride, resolved, _ *sceneformat.ObjectDoc) error {
				if resolved.Model == model {
					return nil
				}
				if model != "" {
					m := ModelEntry{Path: model}
					if err := m.valid(); err != nil {
						return err
					}
					if resolved.Mesh != "" {
						return fmt.Errorf("editor: object %q draws mesh %q; it cannot also draw model %q", id, resolved.Mesh, model)
					}
					if _, _, err := e.buildModel3D(m); err != nil {
						return fmt.Errorf("editor: load model %q: %w", model, err)
					}
				} else if resolved.Mesh == "" && resolved.Occluder != nil && resolved.Occluder.Mode == sceneformat.OccluderModel {
					return fmt.Errorf("editor: object %q has a model occluder; clear or switch occluder to box before removing model", id)
				}
				ov.Model = sceneformat.String(model)
				ov.ModelNode = nil
				ov.ModelMesh = nil
				return nil
			})
		}
		return fmt.Errorf("editor: object %q not found", id)
	}
	if obj.Model == model {
		return nil
	}
	if model != "" {
		m := ModelEntry{Path: model}
		if err := m.valid(); err != nil {
			return err
		}
		if obj.Mesh != "" {
			return fmt.Errorf("editor: object %q draws mesh %q; it cannot also draw model %q", id, obj.Mesh, model)
		}
		if _, _, err := e.buildModel3D(m); err != nil {
			return fmt.Errorf("editor: load model %q: %w", model, err)
		}
	} else if obj.Mesh == "" && obj.Occluder != nil && obj.Occluder.Mode == sceneformat.OccluderModel {
		return fmt.Errorf("editor: object %q has a model occluder; clear or switch occluder to box before removing model", id)
	}
	e.History.Execute(&modelObjectCmd{
		ed:          e,
		id:          id,
		old:         objectModel{model: obj.Model, node: obj.ModelNode, mesh: obj.ModelMesh},
		new:         objectModel{model: model},
		prevVersion: e.Doc.Version,
	})
	return nil
}

// objectModel is what an object's model fields hold.
type objectModel struct{ model, node, mesh string }

// modelsVersion is the first scene format version whose objects can draw glTF models (sceneformat.ObjectDoc.Model).
const modelsVersion = 2

// modelObjectCmd sets an object's model, with its modelNode and modelMesh, and raises a version 1 scene to modelsVersion.
type modelObjectCmd struct {
	ed          *Editor
	id          string
	old, new    objectModel
	prevVersion int
}

func (c *modelObjectCmd) Label() string { return "Set model of " + c.id }

func (c *modelObjectCmd) Do() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Model, obj.ModelNode, obj.ModelMesh = c.new.model, c.new.node, c.new.mesh
		if c.new.model != "" && c.ed.Doc.Version < modelsVersion {
			c.ed.Doc.Version = modelsVersion
		}
		c.ed.Status = c.Label()
	}
}

func (c *modelObjectCmd) Undo() {
	if obj, _, _ := c.ed.FindObject(c.id); obj != nil {
		obj.Model, obj.ModelNode, obj.ModelMesh = c.old.model, c.old.node, c.old.mesh
		c.ed.Doc.Version = c.prevVersion
	}
}

// ObjectModelSummary3D summarizes the 3D geometry drawn by an object for the 3D inspector's MODEL card.
type ObjectModelSummary3D struct {
	// Path is the project-relative glTF model path (e.g. "models/fountain.glb"), or empty for a built-in primitive.
	Path string
	// Mesh is the built-in primitive name (e.g. "box"), or empty for a glTF model file.
	Mesh string
	// Triangles is the total triangle count across all drawables belonging to the object.
	Triangles int
	// MaterialCount is the number of distinct materials used by the object's drawables.
	MaterialCount int
}

// Entry returns the ModelEntry for rendering the object's offscreen thumbnail.
func (s ObjectModelSummary3D) Entry() ModelEntry {
	return ModelEntry{Mesh: s.Mesh, Path: s.Path}
}

// StatsText formats triangle and material counts concisely (for example, "1,284 tris · 2 materials").
func (s ObjectModelSummary3D) StatsText() string {
	triWord := "tris"
	if s.Triangles == 1 {
		triWord = "tri"
	}
	matWord := "materials"
	if s.MaterialCount == 1 {
		matWord = "material"
	}
	return fmt.Sprintf("%s %s · %s %s", formatThousands(s.Triangles), triWord, formatThousands(s.MaterialCount), matWord)
}

// formatThousands formats an integer with comma thousands separators (e.g. 1284 -> "1,284").
func formatThousands(n int) string {
	neg := n < 0
	var mag uint64
	if neg {
		mag = uint64(-(n + 1)) + 1
	} else {
		mag = uint64(n)
	}
	s := strconv.FormatUint(mag, 10)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	rem := len(s) % 3
	if rem > 0 {
		b.WriteString(s[:rem])
	}
	for i := rem; i < len(s); i += 3 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

type cachedModelStats3D struct {
	tris      int
	matCount  int
	roughness float32
	metallic  float32
	hasMat    bool
	ok        bool
}

type modelStatsCache3D struct {
	assetsKey string
	assets    *scenemodel.Assets
	statsKey  string
	entries   map[string]cachedModelStats3D
}

func (e *Editor) standaloneModelStats3D(entry ModelEntry) cachedModelStats3D {
	cacheKey := e.ProjectPath + "\x00" + strconv.FormatUint(e.assetsRev, 10)
	if e.modelStats3D.statsKey != cacheKey || e.modelStats3D.entries == nil {
		e.modelStats3D.statsKey = cacheKey
		e.modelStats3D.entries = make(map[string]cachedModelStats3D)
	}
	mk := entry.Key()
	if cached, ok := e.modelStats3D.entries[mk]; ok {
		return cached
	}
	var res cachedModelStats3D
	if built, _, err := e.buildModel3D(entry); err == nil && built != nil {
		res.tris, res.matCount = drawablesStats3D(built.Drawables, "")
		res.ok = true
		for _, d := range built.Drawables {
			if len(d.Parts) > 0 {
				res.roughness = clampUnit32(d.Parts[0].Material.Roughness)
				res.metallic = clampUnit32(d.Parts[0].Material.Metallic)
				res.hasMat = true
				break
			}
		}
	}
	e.modelStats3D.entries[mk] = res
	return res
}

// ObjectModelSummary3D returns the model path or primitive name, total triangle count, and distinct material count for object id, or false when id has no 3D model or mesh.
func (e *Editor) ObjectModelSummary3D(id string) (ObjectModelSummary3D, bool) {
	if e == nil || id == "" {
		return ObjectModelSummary3D{}, false
	}
	obj, _ := e.FindResolvedObject(id)
	if obj == nil || (obj.Model == "" && obj.Mesh == "") {
		return ObjectModelSummary3D{}, false
	}
	summary := ObjectModelSummary3D{
		Path: obj.Model,
		Mesh: obj.Mesh,
	}
	tris, matCount, found := e.objectDrawableStats3D(id)
	if !found {
		if st := e.standaloneModelStats3D(summary.Entry()); st.ok {
			tris, matCount = st.tris, st.matCount
		}
	}
	summary.Triangles = tris
	summary.MaterialCount = matCount
	return summary, true
}

func (e *Editor) objectDrawableStats3D(id string) (tris, matCount int, found bool) {
	s := e.scene3D()
	if s == nil || s.built == nil {
		return 0, 0, false
	}
	t, m := drawablesStats3D(s.built.Drawables, id)
	if t == 0 && m == 0 {
		return 0, 0, false
	}
	return t, m, true
}

func drawablesStats3D(drawables []*scenemodel.Drawable, objectID string) (tris, matCount int) {
	mats := make(map[uint32]struct{})
	for _, d := range drawables {
		if objectID != "" && d.Object != objectID {
			continue
		}
		for _, part := range d.Parts {
			if n := len(part.Mesh.Indices); n > 0 {
				tris += n / 3
			} else {
				tris += len(part.Mesh.Positions) / 3
			}
			mats[part.MaterialID] = struct{}{}
		}
	}
	return tris, len(mats)
}

// ObjectMaterialSummary3D summarizes the 3D surface material (material file name, roughness, and metallic) for the 3D inspector's MATERIAL section.
type ObjectMaterialSummary3D struct {
	// Path is the project-relative *.material.yaml path, or empty when the object uses the model's embedded glTF material or default primitive surface.
	Path string
	// FileName is the basename of Path (e.g. "stone.material.yaml"), or empty when no external material file is assigned.
	FileName string
	// Roughness is the PBR roughness in [0, 1].
	Roughness float32
	// Metallic is the PBR metallic factor in [0, 1].
	Metallic float32
	// HasValues reports whether Roughness and Metallic were resolved from a material file or built 3D drawable.
	HasValues bool
}

// ObjectMaterialSummary3D returns the material file and roughness/metallic summary for object id, or false when id has neither a material nor 3D model/mesh geometry.
func (e *Editor) ObjectMaterialSummary3D(id string) (ObjectMaterialSummary3D, bool) {
	if e == nil || id == "" {
		return ObjectMaterialSummary3D{}, false
	}
	obj, _ := e.FindResolvedObject(id)
	if obj == nil || (obj.Material == "" && obj.Model == "" && obj.Mesh == "") {
		return ObjectMaterialSummary3D{}, false
	}
	if obj.Material != "" {
		out := ObjectMaterialSummary3D{
			Path:     obj.Material,
			FileName: path.Base(obj.Material),
		}
		if e.material != nil && e.material.relPath == obj.Material {
			out.Roughness = clampUnit32(e.material.mat.EffectiveRoughness())
			out.Metallic = clampUnit32(e.material.mat.Metallic)
			out.HasValues = true
			return out, true
		}
		if mat, err := e.modelAssets3D().Material(obj.Material); err == nil && mat != nil {
			out.Roughness = clampUnit32(mat.EffectiveRoughness())
			out.Metallic = clampUnit32(mat.Metallic)
			out.HasValues = true
			return out, true
		}
		return out, true
	}
	// Fall back to the first built drawable's embedded material (from glTF or primitive default).
	if s := e.scene3D(); s != nil && s.built != nil {
		for _, d := range s.built.Drawables {
			if d.Object == id && len(d.Parts) > 0 {
				return ObjectMaterialSummary3D{
					Roughness: clampUnit32(d.Parts[0].Material.Roughness),
					Metallic:  clampUnit32(d.Parts[0].Material.Metallic),
					HasValues: true,
				}, true
			}
		}
	}
	if st := e.standaloneModelStats3D(ModelEntry{Mesh: obj.Mesh, Path: obj.Model}); st.hasMat {
		return ObjectMaterialSummary3D{
			Roughness: st.roughness,
			Metallic:  st.metallic,
			HasValues: true,
		}, true
	}
	return ObjectMaterialSummary3D{}, false
}

func clampUnit32(v float32) float32 {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ObjectColliderSummary3D summarizes an object's 3D collider and physics body kind for the 3D inspector's COLLIDER section.
type ObjectColliderSummary3D struct {
	// ShapeKey is the canonical component shape value (e.g. "convex", "box", "sphere", "capsule", "cylinder", "trimesh", "heightfield").
	ShapeKey string
	// Shape is the localized human-readable shape name (e.g. "Convex hull", "Box").
	Shape string
	// BodyKind is the effective 3D body kind ("static", "dynamic", "kinematic", or "trigger").
	BodyKind string
	// IsTrigger reports whether the collider behaves as a non-solid trigger sensor.
	IsTrigger bool
}

// Text formats the collider summary as "<Shape> · <BodyKind>" (for example, "Convex hull · static").
func (s ObjectColliderSummary3D) Text() string {
	return s.Shape + " · " + s.BodyKind
}

// ObjectColliderSummary3D returns the 3D collider shape and body kind summary for object id, or false when id has no collider3d or trigger3d component.
func (e *Editor) ObjectColliderSummary3D(id string) (ObjectColliderSummary3D, bool) {
	if e == nil || id == "" {
		return ObjectColliderSummary3D{}, false
	}
	obj, _ := e.FindResolvedObject(id)
	if obj == nil || len(obj.Components) == 0 {
		return ObjectColliderSummary3D{}, false
	}
	var comp map[string]any
	isTrigger := false
	if raw, ok := obj.Components[sceneformat.Collider3DComponentName]; ok {
		comp, _ = raw.(map[string]any)
		if b, ok := comp["isTrigger"].(bool); ok && b {
			isTrigger = true
		}
	} else if raw, ok := obj.Components[sceneformat.Trigger3DComponentName]; ok {
		comp, _ = raw.(map[string]any)
		isTrigger = true
	} else {
		return ObjectColliderSummary3D{}, false
	}
	shapeKey := "box"
	if s, ok := comp["shape"].(string); ok && s != "" {
		shapeKey = s
	}
	bodyKind := "static"
	if rawBody, ok := obj.Components[sceneformat.Body3DComponentName]; ok {
		if bm, ok := rawBody.(map[string]any); ok {
			if k, ok := bm["kind"].(string); ok && k != "" {
				bodyKind = k
			}
		}
	}
	if isTrigger || bodyKind == "trigger" {
		bodyKind = "trigger"
		isTrigger = true
	}
	return ObjectColliderSummary3D{
		ShapeKey:  shapeKey,
		Shape:     e.colliderShapeLabel3D(shapeKey),
		BodyKind:  bodyKind,
		IsTrigger: isTrigger,
	}, true
}

func (e *Editor) colliderShapeLabel3D(shapeKey string) string {
	switch shapeKey {
	case "convex":
		return e.T("inspector.collider.convex")
	case "box":
		return e.T("inspector.collider.box")
	case "sphere":
		return e.T("inspector.collider.sphere")
	case "capsule":
		return e.T("inspector.collider.capsule")
	case "cylinder":
		return e.T("inspector.collider.cylinder")
	case "trimesh":
		return e.T("inspector.collider.trimesh")
	case "heightfield":
		return e.T("inspector.collider.heightfield")
	default:
		return shapeKey
	}
}

// ObjectChildrenSummary3D summarizes the direct children of a group object for the 3D inspector's CHILDREN section.
type ObjectChildrenSummary3D struct {
	// Count is the number of direct children.
	Count int
	// IDs are the child IDs (or names when an ID is empty) in document order.
	IDs []string
	// SharedRef is the shared prefab, model, or scene path when every child references the same one.
	SharedRef string
	// Summary is the formatted single-line summary (e.g. "wall_n · wall_e · wall_w · wall_s_l · wall_s_r" or "bench_01 … bench_06 · prefabs/bench.prefab.yaml").
	Summary string
}

// ObjectChildrenSummary3D returns a summary of object id's direct children, or false when id has no children.
func (e *Editor) ObjectChildrenSummary3D(id string) (ObjectChildrenSummary3D, bool) {
	if e == nil || id == "" {
		return ObjectChildrenSummary3D{}, false
	}
	obj, _ := e.FindResolvedObject(id)
	if obj == nil || len(obj.Children) == 0 {
		return ObjectChildrenSummary3D{}, false
	}
	ids := make([]string, 0, len(obj.Children))
	sharedRef := ""
	sharedValid := true
	for i := range obj.Children {
		ch := &obj.Children[i]
		label := ch.ID
		if label == "" {
			label = ch.Name
		}
		ids = append(ids, label)
		ref := ch.Prefab
		if ref == "" {
			ref = ch.Scene
		}
		if ref == "" {
			ref = ch.Model
		}
		if i == 0 {
			sharedRef = ref
		} else if ref != sharedRef {
			sharedValid = false
		}
	}
	if !sharedValid {
		sharedRef = ""
	}
	var text string
	if len(ids) <= 5 {
		text = strings.Join(ids, " · ")
	} else {
		text = ids[0] + " … " + ids[len(ids)-1]
	}
	if sharedRef != "" {
		text += " · " + sharedRef
	}
	return ObjectChildrenSummary3D{
		Count:     len(ids),
		IDs:       ids,
		SharedRef: sharedRef,
		Summary:   text,
	}, true
}

// Viewport3DAgentPromptKind identifies what the 3D viewport's inline agent prompt is asking for.
type Viewport3DAgentPromptKind int

const (
	// Viewport3DAgentPromptNone means the 3D inline agent prompt is closed.
	Viewport3DAgentPromptNone Viewport3DAgentPromptKind = iota
	// Viewport3DAgentPromptModel asks the agent for a new 3D model ("Or ask for one").
	Viewport3DAgentPromptModel
	// Viewport3DAgentPromptComponent asks the agent to add a component to the selected object ("Ask to add a component…").
	Viewport3DAgentPromptComponent
)

// Viewport3DAgentRequest is dispatched when the author submits a prompt from the 3D MODELS strip ("Or ask for one")
// or from the 3D inspector ("Ask to add a component…").
type Viewport3DAgentRequest struct {
	Kind     Viewport3DAgentPromptKind
	ObjectID string
	Prompt   string
}

type viewport3DAgentPromptState struct {
	kind     Viewport3DAgentPromptKind
	objectID string
}

// resetViewport3DAgentPrompt clears the 3D inline agent prompt (and, on project/demo switch, the last request and standalone stats cache).
func (e *Editor) resetViewport3DAgentPrompt(clearLast bool) {
	if e == nil {
		return
	}
	e.agentPrompt3D = viewport3DAgentPromptState{}
	if clearLast {
		e.lastAgentReq3D = nil
		e.modelStats3D = modelStatsCache3D{}
	}
}

// OpenModelAgentPrompt opens the 3D viewport's inline agent prompt to describe and request a new model ("Or ask for one").
func (e *Editor) OpenModelAgentPrompt() {
	if e == nil || e.playing || e.MergeConflictOpen() {
		return
	}
	e.agentPrompt3D = viewport3DAgentPromptState{kind: Viewport3DAgentPromptModel}
	e.Status = e.T("models.askForOneHint")
	e.notify()
}

// OpenComponentAgentPrompt opens the 3D viewport's inline agent prompt to add a component to objectID ("Ask to add a component…").
func (e *Editor) OpenComponentAgentPrompt(objectID string) {
	if e == nil || e.playing || e.MergeConflictOpen() {
		return
	}
	if objectID == "" {
		objectID = e.SelectedID
	}
	e.agentPrompt3D = viewport3DAgentPromptState{kind: Viewport3DAgentPromptComponent, objectID: objectID}
	e.Status = e.Viewport3DAgentPromptPlaceholder()
	e.notify()
}

// CloseViewport3DAgentPrompt closes the 3D inline agent prompt.
func (e *Editor) CloseViewport3DAgentPrompt() {
	if e == nil || e.agentPrompt3D.kind == Viewport3DAgentPromptNone {
		return
	}
	e.agentPrompt3D = viewport3DAgentPromptState{}
	e.notify()
}

// Viewport3DAgentPromptOpen reports whether the 3D inline agent prompt is currently open.
func (e *Editor) Viewport3DAgentPromptOpen() bool {
	return e != nil && e.agentPrompt3D.kind != Viewport3DAgentPromptNone
}

// Viewport3DAgentPromptKind returns the active 3D inline agent prompt kind.
func (e *Editor) Viewport3DAgentPromptKind() Viewport3DAgentPromptKind {
	if e == nil {
		return Viewport3DAgentPromptNone
	}
	return e.agentPrompt3D.kind
}

// Viewport3DAgentPromptObjectID returns the target object ID for a component prompt, or "".
func (e *Editor) Viewport3DAgentPromptObjectID() string {
	if e == nil {
		return ""
	}
	return e.agentPrompt3D.objectID
}

// Viewport3DAgentPromptPlaceholder returns the contextual placeholder text for the open 3D inline agent prompt.
func (e *Editor) Viewport3DAgentPromptPlaceholder() string {
	if e == nil {
		return ""
	}
	switch e.agentPrompt3D.kind {
	case Viewport3DAgentPromptModel:
		return e.T("models.askForOneHint")
	case Viewport3DAgentPromptComponent:
		name := e.agentPrompt3D.objectID
		if o, _ := e.FindResolvedObject(e.agentPrompt3D.objectID); o != nil && o.Name != "" {
			name = o.Name
		}
		if name == "" {
			name = "selection"
		}
		return e.T("inspector.askAddComponentHint", l10n.Param("name", name))
	default:
		return ""
	}
}

// SubmitViewport3DAgentPrompt dispatches text as a Viewport3DAgentRequest to OnAskAgent3D (falling back to OnAssetAskAgent when configured), updates the status bar, and closes the inline prompt.
func (e *Editor) SubmitViewport3DAgentPrompt(text string) (Viewport3DAgentRequest, error) {
	if e == nil {
		return Viewport3DAgentRequest{}, errors.New("editor: nil editor")
	}
	if e.playing {
		return Viewport3DAgentRequest{}, ErrPlaying
	}
	if e.MergeConflictOpen() {
		return Viewport3DAgentRequest{}, errors.New("editor: resolve merge conflicts before submitting a 3D agent prompt")
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Viewport3DAgentRequest{}, errors.New("editor: prompt must not be empty")
	}
	kind := e.agentPrompt3D.kind
	if kind == Viewport3DAgentPromptNone {
		kind = Viewport3DAgentPromptModel
	}
	req := Viewport3DAgentRequest{
		Kind:     kind,
		ObjectID: e.agentPrompt3D.objectID,
		Prompt:   trimmed,
	}
	reqCopy := req
	e.lastAgentReq3D = &reqCopy
	e.agentPrompt3D = viewport3DAgentPromptState{}
	e.Status = "Asked agent: " + trimmed
	if e.OnAskAgent3D != nil {
		e.OnAskAgent3D(req)
	} else if e.OnAssetAskAgent != nil {
		e.OnAssetAskAgent(req.ObjectID, req.Prompt)
	}
	e.notify()
	return req, nil
}

// LastViewport3DAgentRequest returns the most recent request submitted from the 3D MODELS strip or 3D inspector agent prompt.
func (e *Editor) LastViewport3DAgentRequest() (Viewport3DAgentRequest, bool) {
	if e == nil || e.lastAgentReq3D == nil {
		return Viewport3DAgentRequest{}, false
	}
	return *e.lastAgentReq3D, true
}
