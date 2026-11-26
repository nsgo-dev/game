package editor_test

import (
	"bytes"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const placeW, placeH = 640, 360

var (
	crateModel = editor.ModelEntry{Path: "models/crate.glb"}
	boxModel   = editor.ModelEntry{Mesh: "box"}
)

// pixelOf returns where world point p falls on a viewport placeW by placeH.
func pixelOf(t *testing.T, ed *editor.Editor, p [3]float32) (float32, float32) {
	t.Helper()
	sx, sy, _, ok := ed.Camera3D.Project(p, placeW, placeH)
	if !ok {
		t.Fatalf("%v is not in view", p)
	}
	return sx, sy
}

// objectsWith returns the IDs of the scene's top-level objects that draw m, in document order.
func objectsWith(ed *editor.Editor, m editor.ModelEntry) []string {
	var ids []string
	for _, l := range ed.Doc.Layers {
		for _, o := range l.Objects {
			if o.Mesh == m.Mesh && o.Model == m.Path {
				ids = append(ids, o.ID)
			}
		}
	}
	return ids
}

func fileURL(p string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(p)}).String()
}

// The Place tool with a model selected adds it where the press's ray meets the floor, resting on it, as one step that Undo takes back byte for byte and Redo makes again.
func TestPlaceModelOnTheFloorUndoRedo(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	if err := ed.SelectModel(crateModel); err != nil {
		t.Fatal(err)
	}
	ed.SetTool(editor.ToolPlace)
	before := saveYAML(t, ed)
	steps := ed.History.Len()

	sx, sy := pixelOf(t, ed, [3]float32{2, 0, 0.3})
	ed.HandleViewport3DPress(sx, sy, placeW, placeH)
	ids := objectsWith(ed, crateModel)
	if len(ids) != 3 {
		t.Fatalf("crate objects after the press = %v, want the two of the level and a new one", ids)
	}
	id := ids[2]
	obj, _, _ := ed.FindObject(id)
	if ed.SelectedID != id || obj.Name != "crate" {
		t.Fatalf("placed %+v, selected %q", obj, ed.SelectedID)
	}
	assertNear32(t, "x", obj.X, 2, 1e-3)
	assertNear32(t, "z", obj.PosZ, 0.3, 1e-3)
	lo, hi := ed.ObjectAABB3D(obj)
	assertNear32(t, "bottom of the crate", lo[1], 0, 1e-3)
	if hi[1] <= 0.5 {
		t.Fatalf("crate box %v..%v, want it above the floor", lo, hi)
	}
	if ed.History.Len() != steps+1 {
		t.Fatalf("history grew by %d steps, want 1", ed.History.Len()-steps)
	}

	if !ed.Undo() || !bytes.Equal(saveYAML(t, ed), before) {
		t.Fatal("Undo did not put the scene back")
	}
	if !ed.Redo() {
		t.Fatal("Redo failed")
	}
	if got := objectsWith(ed, crateModel); !slices.Equal(got, ids) {
		t.Fatalf("crates after Redo = %v, want %v", got, ids)
	}
}

// A model placed on another object's top rests on it, and on a turned surface with aligning on it turns its +Y axis to the surface's normal.
func TestPlaceModelOnGeometry(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	sx, sy := pixelOf(t, ed, [3]float32{-1, 1, 0})
	p, ok := ed.PlacementAt3D(sx, sy, placeW, placeH)
	if !ok || !p.OnSurface {
		t.Fatalf("placement on the crate = %+v, %v, want one on its surface", p, ok)
	}
	if p.Point[1] < 0.9 {
		t.Fatalf("placement on the crate %v is not on its top", p.Point)
	}
	assertNear32(t, "normal y", p.Normal[1], 1, 1e-3)

	// Snapping leaves a point on geometry where the ray hits it: the grid's spacing is wider than the crate, so a snapped point would hang over the floor.
	ed.PlaceSnap3D = true
	snapped, ok := ed.PlacementAt3D(sx, sy, placeW, placeH)
	if !ok || !snapped.OnSurface || snapped.Point != p.Point || snapped.Normal != p.Normal {
		t.Fatalf("placement on the crate with Snap = %+v, %v, want %+v", snapped, ok, p)
	}
	ed.PlaceSnap3D = false

	id, err := ed.PlaceModel3D(boxModel, p)
	if err != nil {
		t.Fatal(err)
	}
	obj, _, _ := ed.FindObject(id)
	lo, _ := ed.ObjectAABB3D(obj)
	assertNear32(t, "bottom of the box on the crate", lo[1], p.Point[1], 1e-3)

	// A big box turned about Z: its faces are tilted, and an aligned model follows the face it lands on.
	ed = newTest3DEditor(t, sceneformat.ObjectDoc{ID: "slab", Mesh: "box", RotZ: 0.5, ScaleX: sceneformat.Float32(200), ScaleY: sceneformat.Float32(40), ScaleZ: sceneformat.Float32(200)})
	p, ok = ed.PlacementAt3D(placeW/2, placeH/2, placeW, placeH)
	if !ok || !p.OnSurface {
		t.Fatalf("placement on the slab = %+v, %v", p, ok)
	}
	if p.Normal[1] > 0.95 {
		t.Fatalf("normal %v is not tilted", p.Normal)
	}
	ed.PlaceAlign3D = true
	// A primitive needs no project to load from.
	id, err = ed.PlaceModel3D(editor.ModelEntry{Mesh: "sphere"}, p)
	if err != nil {
		t.Fatal(err)
	}
	obj, _, _ = ed.FindObject(id)
	// Rz * Rx turns +Y to (-sin z cos x, cos z cos x, sin x).
	up := [3]float64{-math.Sin(float64(obj.RotZ)) * math.Cos(float64(obj.RotX)), math.Cos(float64(obj.RotZ)) * math.Cos(float64(obj.RotX)), math.Sin(float64(obj.RotX))}
	for i := range 3 {
		assertNear32(t, "aligned up", float32(up[i]), p.Normal[i], 1e-4)
	}
	// The sphere's center sits half its size off the surface, along the normal.
	for i := range 3 {
		assertNear32(t, "sphere center", []float32{obj.X, obj.Y, obj.PosZ}[i], p.Point[i]+p.Normal[i]*0.5, 1e-3)
	}
}

// Where the ray hits no geometry the model lands on the ground plane at PlaceHeight3D, snapped to the grid with PlaceSnap3D; a ray that never meets the plane places nothing.
func TestPlaceModelOnTheGroundPlane(t *testing.T) {
	ed := newTest3DEditor(t)
	if err := ed.SetPlaceOptions3D(2, false, false); err != nil {
		t.Fatal(err)
	}
	p, ok := ed.PlacementAt3D(placeW/2, placeH/2, placeW, placeH)
	if !ok || p.OnSurface || p.Point[1] != 2 || p.Normal != [3]float32{0, 1, 0} {
		t.Fatalf("placement = %+v, %v, want one on the ground plane at 2", p, ok)
	}
	id, err := ed.PlaceModelAt3D(boxModel, placeW/2, placeH/2, placeW, placeH)
	if err != nil {
		t.Fatal(err)
	}
	obj, _, _ := ed.FindObject(id)
	assertNear32(t, "box center", obj.Y, 2.5, 1e-4)

	if err := ed.SetPlaceOptions3D(0, true, false); err != nil {
		t.Fatal(err)
	}
	p, ok = ed.PlacementAt3D(placeW/2+37, placeH/2+23, placeW, placeH)
	if !ok {
		t.Fatal("no placement off the center")
	}
	for _, v := range []float32{p.Point[0], p.Point[2]} {
		if r := math.Mod(float64(v), 16); r != 0 {
			t.Fatalf("snapped point %v is not on the 16-unit grid", p.Point)
		}
	}
	if err := ed.SetPlaceOptions3D(float32(math.Inf(1)), false, false); err == nil {
		t.Fatal("an infinite ground height was accepted")
	}

	ed.SetView3D(editor.View3DFront)
	steps := ed.History.Len()
	if _, err := ed.PlaceModelAt3D(boxModel, placeW/2, placeH/2, placeW, placeH); err == nil || ed.History.Len() != steps {
		t.Fatalf("a level view placed on the horizon: err %v", err)
	}
}

// Placing refuses a locked or screen-space active layer, a model that does not load and an entry that names nothing, and changes nothing.
func TestPlaceModelRefusals(t *testing.T) {
	ed := newTest3DEditor(t)
	ed.Doc.Layers = append(ed.Doc.Layers, sceneformat.LayerDoc{Name: "hud", Space: sceneformat.SpaceScreen, Order: 1})
	ed.Rebuild()
	p := editor.Placement3D{Normal: [3]float32{0, 1, 0}}
	ed.SetActiveLayer("hud")
	if _, err := ed.PlaceModel3D(boxModel, p); err == nil {
		t.Fatal("placed a model on a screen-space layer")
	}
	ed.SetActiveLayer("world")
	if err := ed.SetLayerLocked("world", true); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.PlaceModel3D(boxModel, p); err == nil {
		t.Fatal("placed a model on a locked layer")
	}
	if err := ed.SetLayerLocked("world", false); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.PlaceModel3D(crateModel, p); err == nil {
		t.Fatal("placed a model with no project to load it from")
	}
	if _, err := ed.PlaceModel3D(editor.ModelEntry{Mesh: "cube"}, p); err == nil {
		t.Fatal("placed a mesh that is not a built-in primitive")
	}
	if err := ed.SelectModel(editor.ModelEntry{Path: "models/crate.png"}); err == nil {
		t.Fatal("selected a file that is not a glTF model")
	}
	nan := float32(math.NaN())
	for _, in := range [][4]float32{{nan, placeH / 2, placeW, placeH}, {placeW / 2, nan, placeW, placeH}, {placeW / 2, placeH / 2, 0, placeH}, {placeW / 2, placeH / 2, placeW, nan}} {
		if p, ok := ed.PlacementAt3D(in[0], in[1], in[2], in[3]); ok {
			t.Errorf("PlacementAt3D%v = %+v, want none", in, p)
		}
	}
	for _, bad := range []editor.Placement3D{
		{Point: [3]float32{nan, 0, 0}, Normal: [3]float32{0, 1, 0}},
		{Normal: [3]float32{0, float32(math.Inf(1)), 0}},
	} {
		if _, err := ed.PlaceModel3D(boxModel, bad); err == nil {
			t.Errorf("placed a model at %+v", bad)
		}
	}
	if ed.History.Len() != 0 {
		t.Fatalf("refusals left %d steps in the history", ed.History.Len())
	}
}

// Models lists the primitives, then the project's glTF files and the outputs of its model targets, which ProjectModels lists for the inspector.
func TestModelsListsPrimitivesFilesAndTargets(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	if err := ed.EditProject("Add a model target", func(f *project.File) {
		f.Assets = &project.Assets{Models: []project.ModelTarget{{In: "art/tree.gltf", Out: "models/tree.glb"}}}
	}); err != nil {
		t.Fatal(err)
	}
	if got, want := ed.ProjectModels(), []string{"models/crate.glb", "models/tree.glb"}; !slices.Equal(got, want) {
		t.Fatalf("ProjectModels = %v, want %v", got, want)
	}
	var want []editor.ModelEntry
	for _, m := range sceneformat.MeshPrimitives() {
		want = append(want, editor.ModelEntry{Mesh: m})
	}
	want = append(want, crateModel, editor.ModelEntry{Path: "models/tree.glb", Output: true})
	if got := ed.Models(); !slices.Equal(got, want) {
		t.Fatalf("Models = %+v, want %+v", got, want)
	}
	if crateModel.Name() != "crate" || crateModel.Key() != "models/crate.glb" || boxModel.Key() != "mesh:box" {
		t.Fatal("entry names")
	}
}

// SetObjectModel names a project model undoably, clearing the node of the model it drew, and refuses an object that draws a primitive and a model that does not load.
func TestSetObjectModelUndoRedo(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	before := saveYAML(t, ed)
	if err := ed.SetObjectModel("lid", ""); err != nil {
		t.Fatal(err)
	}
	lid, _, _ := ed.FindObject("lid")
	if lid.Model != "" || lid.ModelNode != "" {
		t.Fatalf("lid after clearing its model = %+v", lid)
	}
	if !ed.Undo() || !bytes.Equal(saveYAML(t, ed), before) {
		t.Fatal("Undo did not put the lid's model and node back")
	}
	if !ed.Redo() || lid.Model != "" {
		t.Fatal("Redo did not clear the model again")
	}
	ed.Undo()
	if err := ed.SetObjectModel("ball", crateModel.Path); err == nil {
		t.Fatal("named a model on an object that draws a primitive")
	}
	if err := ed.SetObjectModel("lid", "models/missing.glb"); err == nil {
		t.Fatal("named a model that does not load")
	}
	if err := ed.SetObjectModel("lid", "models/crate.txt"); err == nil {
		t.Fatal("named a file that is not a glTF model")
	}

	// A version 2 scene already has models, so naming one leaves its version alone.
	if err := ed.SetObjectModel("lid", ""); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetObjectModel("lid", crateModel.Path); err != nil {
		t.Fatal(err)
	}
	if ed.Doc.Version != 2 {
		t.Fatalf("naming a model raised a version 2 scene to %d", ed.Doc.Version)
	}
}

// Naming a model in a version 1 scene raises it to version 2, the first that has models, and Undo puts the scene back byte for byte.
func TestSetObjectModelRaisesVersion1(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	if err := ed.OpenYAML([]byte("version: 1\nname: Old\nlayers:\n  - name: level\n    objects:\n      - id: post\n")); err != nil {
		t.Fatal(err)
	}
	before := saveYAML(t, ed)
	if err := ed.SetObjectModel("post", crateModel.Path); err != nil {
		t.Fatal(err)
	}
	if post, _, _ := ed.FindObject("post"); post.Model != crateModel.Path || ed.Doc.Version != 2 {
		t.Fatalf("post %+v in a version %d scene, want the crate in version 2", post, ed.Doc.Version)
	}
	if !bytes.Contains(saveYAML(t, ed), []byte("version: 2\n")) {
		t.Fatal("the saved scene is not version 2")
	}
	if !ed.Undo() || !bytes.Equal(saveYAML(t, ed), before) {
		t.Fatal("Undo did not put the version 1 scene back")
	}
}

// A glTF file dropped from another app lands where the view points when it lies in the project, all of them as one step; a file outside the project or of another kind is refused.
func TestDropModelFiles(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	root := filepath.Dir(ed.ProjectPath)
	crate := fileURL(filepath.Join(root, "models", "crate.glb"))
	steps := ed.History.Len()
	ids, err := ed.DropModelFiles([]string{crate, crate}, placeW/2, placeH/2, placeW, placeH)
	if err != nil || len(ids) != 2 {
		t.Fatalf("DropModelFiles = %v, %v", ids, err)
	}
	if ed.History.Len() != steps+1 {
		t.Fatalf("two dropped files took %d steps, want 1", ed.History.Len()-steps)
	}
	ed.Undo()
	if len(objectsWith(ed, crateModel)) != 2 {
		t.Fatal("Undo did not take both dropped models back")
	}

	outside := filepath.Join(t.TempDir(), "rock.glb")
	data, err := os.ReadFile(filepath.Join(root, "models", "crate.glb"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, data, 0o644); err != nil {
		t.Fatal(err)
	}
	material := fileURL(filepath.Join(root, "materials", "stone.material.yaml"))
	for _, uris := range [][]string{{fileURL(outside)}, {material}, {crate, material}, {"blob:x#crate.glb"}} {
		if _, err := ed.DropModelFiles(uris, placeW/2, placeH/2, placeW, placeH); err == nil {
			t.Errorf("DropModelFiles(%v) placed them", uris)
		}
	}
	if len(objectsWith(ed, crateModel)) != 2 || !ed.History.CanRedo() {
		t.Fatal("a refused drop changed the scene")
	}

	// A file in a folder the asset browser leaves out is refused, as it is not a model the project lists.
	for _, dir := range []string{"_build", ".cache", "testdata"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		hidden := filepath.Join(root, dir, "rock.glb")
		if err := os.WriteFile(hidden, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ed.DropModelFiles([]string{fileURL(hidden)}, placeW/2, placeH/2, placeW, placeH); err == nil {
			t.Errorf("placed a dropped file from %s", dir)
		}
	}
	if len(objectsWith(ed, crateModel)) != 2 || !ed.History.CanRedo() {
		t.Fatal("a refused drop from a left-out folder changed the scene")
	}
}

// When a later file of a drop does not load, the ones placed before it are taken back and the history does not change.
func TestDropModelFilesRollsBack(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	root := filepath.Dir(ed.ProjectPath)
	before := saveYAML(t, ed)
	steps := ed.History.Len()
	uris := []string{fileURL(filepath.Join(root, "models", "crate.glb")), fileURL(filepath.Join(root, "models", "missing.glb"))}
	if ids, err := ed.DropModelFiles(uris, placeW/2, placeH/2, placeW, placeH); err == nil {
		t.Fatalf("a drop with a missing model placed %v", ids)
	}
	if !bytes.Equal(saveYAML(t, ed), before) || ed.History.Len() != steps {
		t.Fatalf("a refused drop left the scene changed or %d new steps", ed.History.Len()-steps)
	}
}

// In 2D, pressing a model in the asset browser selects it without turning on the Place tool, whose presses there add atlas frames, so a click on the canvas adds nothing.
func TestPressModelIn2DLeavesThePlaceToolOff(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	ed.SetViewMode(editor.ViewMode2D)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(1200, 900), nsgotest.NoGPU())
	before := saveYAML(t, ed)
	app.Press("id:model-mesh-box")
	if ed.SelectedModel == nil || *ed.SelectedModel != boxModel {
		t.Fatalf("selected model %+v, want the box", ed.SelectedModel)
	}
	if ed.Tool == editor.ToolPlace {
		t.Fatal("pressing a model in 2D turned on the Place tool")
	}
	app.Press("id:editor-viewport")
	if !bytes.Equal(saveYAML(t, ed), before) {
		t.Fatal("a click on the 2D canvas after pressing a model changed the scene")
	}
}

// In the editor's window: pressing a model in the asset browser picks it for the Place tool, a press on the 3D viewport places it, the toolbar's Undo and Redo take it back and make it again; dragging a primitive from the browser onto the viewport places it there, a file dropped from another app lands too, and the inspector's Model field picks one of the project's models.
func TestPlaceModelsInTheEditorWindow(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(1200, 900), nsgotest.NoGPU())
	app.Press("id:model-models-crate-glb")
	if ed.SelectedModel == nil || *ed.SelectedModel != crateModel || ed.Tool != editor.ToolPlace {
		t.Fatalf("after pressing the crate: model %+v, tool %v", ed.SelectedModel, ed.Tool)
	}
	if !app.Find("id:tool3d-place").Props.Pressed {
		t.Fatal("the 3D toolbar's Place is off with the Place tool on")
	}

	vw, vh := ed.CanvasSize()
	sx, sy, _, vis := ed.Camera3D.Project([3]float32{2, 0, 0.3}, vw, vh)
	if !vis {
		t.Fatal("the floor point is not in view")
	}
	app.PressAt("id:editor-viewport", pt(sx, sy))
	crates := objectsWith(ed, crateModel)
	if len(crates) != 3 {
		t.Fatalf("crates after the press = %v", crates)
	}
	app.Press("id:toolbar-undo")
	if len(objectsWith(ed, crateModel)) != 2 {
		t.Fatal("Undo did not take the placed crate back")
	}
	app.Press("id:toolbar-redo")
	if got := objectsWith(ed, crateModel); !slices.Equal(got, crates) {
		t.Fatalf("crates after Redo = %v, want %v", got, crates)
	}

	app.Press("id:tool3d-select")
	// The drop lands on the frame's middle, where the view points.
	want, ok := ed.PlacementAt3D(vw/2, vh/2, vw, vh)
	if !ok {
		t.Fatal("no placement at the viewport's middle")
	}
	app.DragTo("id:model-mesh-box", viewportFrame)
	boxes := objectsWith(ed, boxModel)
	if len(boxes) != 1 || ed.SelectedModel == nil || *ed.SelectedModel != boxModel {
		t.Fatalf("after dragging the box: boxes %v, selected model %+v", boxes, ed.SelectedModel)
	}
	box, _, _ := ed.FindObject(boxes[0])
	assertNear32(t, "dragged box x", box.X, want.Point[0], 1e-3)
	assertNear32(t, "dragged box z", box.PosZ, want.Point[2], 1e-3)

	app.DropFiles(viewportFrame, fileURL(filepath.Join(filepath.Dir(ed.ProjectPath), "models", "crate.glb")))
	if len(objectsWith(ed, crateModel)) != 4 {
		t.Fatal("a dropped crate file was not placed")
	}

	// The Model field picks a project model for an object that draws nothing yet, as one undoable step.
	app.Post(func() {
		if _, err := ed.PlaceObject("level", sceneformat.ObjectDoc{ID: "marker", X: 2}); err != nil {
			t.Error(err)
		}
	})
	app.Settle()
	app.Press("id:inspector-model")
	app.Press("role:option models/crate.glb")
	marker, _, _ := ed.FindObject("marker")
	if marker.Model != crateModel.Path {
		t.Fatalf("marker after picking the crate = %+v", marker)
	}
	app.Press("id:toolbar-undo")
	if marker, _, _ = ed.FindObject("marker"); marker.Model != "" {
		t.Fatalf("Undo left the marker's model %q", marker.Model)
	}
	ed.SelectObject("ball")
	app.Settle()
	if app.Enabled("id:inspector-model") {
		t.Fatal("the Model field is enabled on an object that draws a primitive")
	}
}

// The bottom MODELS strip in 3D lists project models before primitives with drag tooltips,
// places a model undoably when dragged onto the 3D viewport, and opens the "Or ask for one" prompt;
// the 3D inspector renders MODEL, MATERIAL, COLLIDER, CHILDREN, and "Ask to add a component…".
func TestViewport3DModelsStripAndInspectorDetails(t *testing.T) {
	ed := editor.OpenCourtyard(t)
	if err := ed.SetComponentRegistry(sceneformat.PhysicsComponentRegistry()); err != nil {
		t.Fatal(err)
	}
	// Give crate a material file and a static convex hull collider, and add a group with children.
	if err := ed.SetObjectMaterial("crate", "materials/stone.material.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddComponent("crate", sceneformat.Collider3DComponentName); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentField("crate", sceneformat.Collider3DComponentName, "shape", "convex"); err != nil {
		t.Fatal(err)
	}
	if err := ed.AddComponent("crate", sceneformat.Body3DComponentName); err != nil {
		t.Fatal(err)
	}
	if err := ed.SetComponentField("crate", sceneformat.Body3DComponentName, "kind", "static"); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.PlaceObject("level", sceneformat.ObjectDoc{
		ID:   "gate",
		Name: "Gate",
		Children: []sceneformat.ObjectDoc{
			{ID: "pillar_l", Mesh: "box"},
			{ID: "pillar_r", Mesh: "box"},
			{ID: "lintel", Mesh: "box"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ed.PlaceObject("level", sceneformat.ObjectDoc{
		ID:   "benches",
		Name: "Benches",
		Children: []sceneformat.ObjectDoc{
			{ID: "bench_01", Model: "models/crate.glb"},
			{ID: "bench_02", Model: "models/crate.glb"},
			{ID: "bench_03", Model: "models/crate.glb"},
			{ID: "bench_04", Model: "models/crate.glb"},
			{ID: "bench_05", Model: "models/crate.glb"},
			{ID: "bench_06", Model: "models/crate.glb"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Verify summary APIs directly.
	ms, ok := ed.ObjectModelSummary3D("crate")
	if !ok || ms.Path != "models/crate.glb" || ms.Triangles <= 0 || ms.MaterialCount <= 0 {
		t.Fatalf("ObjectModelSummary3D(crate) = %+v, %v", ms, ok)
	}
	mats, ok := ed.ObjectMaterialSummary3D("crate")
	if !ok || mats.FileName != "stone.material.yaml" || !mats.HasValues {
		t.Fatalf("ObjectMaterialSummary3D(crate) = %+v, %v", mats, ok)
	}
	assertNear32(t, "stone roughness", mats.Roughness, 0.9, 1e-3)
	assertNear32(t, "stone metallic", mats.Metallic, 0, 1e-3)

	cs, ok := ed.ObjectColliderSummary3D("crate")
	if !ok || cs.Shape != "Convex hull" || cs.BodyKind != "static" || cs.Text() != "Convex hull · static" {
		t.Fatalf("ObjectColliderSummary3D(crate) = %+v, %v", cs, ok)
	}
	gs, ok := ed.ObjectChildrenSummary3D("gate")
	if !ok || gs.Count != 3 || gs.Summary != "pillar_l · pillar_r · lintel" {
		t.Fatalf("ObjectChildrenSummary3D(gate) = %+v, %v", gs, ok)
	}
	bs, ok := ed.ObjectChildrenSummary3D("benches")
	if !ok || bs.Count != 6 || bs.Summary != "bench_01 … bench_06 · models/crate.glb" {
		t.Fatalf("ObjectChildrenSummary3D(benches) = %+v, %v", bs, ok)
	}

	// StripModels lists project models before built-in primitives.
	strip := ed.StripModels()
	if len(strip) == 0 || strip[0].Path != "models/crate.glb" || strip[0].FileLabel() != "crate.glb" {
		t.Fatalf("StripModels = %+v, want project model crate.glb first", strip)
	}

	var asked []editor.Viewport3DAgentRequest
	ed.OnAskAgent3D = func(req editor.Viewport3DAgentRequest) {
		asked = append(asked, req)
	}

	ed.SelectObject("crate")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, nsgotest.Size(1440, 900), nsgotest.NoGPU())
	ed.WaitModelThumbnails()
	app.Settle()

	// Bottom MODELS strip is visible in 3D with title, hint, model card tooltip, and "Or ask for one".
	if got := app.Find("id:viewport3d-models-strip-title").Text; got != "MODELS" {
		t.Fatalf("strip title = %q, want MODELS", got)
	}
	if got := app.Find("id:viewport3d-models-strip-hint").Text; got != "drag in" {
		t.Fatalf("strip hint = %q, want drag in", got)
	}
	crateCard := app.Find("id:viewport3d-model-card-models-crate-glb")
	if crateCard.Props.Label != "Drag into the viewport to place crate.glb" {
		t.Fatalf("crate card label = %q", crateCard.Props.Label)
	}

	// Inspector shows MODEL, MATERIAL, COLLIDER, and Ask to add a component… for crate.
	if got := app.Find("id:inspector-model-path").Text; got != "models/crate.glb" {
		t.Fatalf("inspector-model-path = %q", got)
	}
	if got := app.Find("id:inspector-model-stats").Text; got != ms.StatsText() {
		t.Fatalf("inspector-model-stats = %q, want %q", got, ms.StatsText())
	}
	if got := app.Find("id:inspector-material3d-header").Text; got != "MATERIAL · stone.material.yaml" {
		t.Fatalf("inspector-material3d-header = %q", got)
	}
	if got := app.Find("id:inspector-material3d-roughness-value").Text; got != "0.90" {
		t.Fatalf("roughness text = %q, want 0.90", got)
	}
	if got := app.Find("id:inspector-material3d-metallic-value").Text; got != "0.00" {
		t.Fatalf("metallic text = %q, want 0.00", got)
	}
	if got := app.Find("id:inspector-collider3d-shape").Text; got != "Convex hull" {
		t.Fatalf("collider shape = %q, want Convex hull", got)
	}
	if got := app.Find("id:inspector-collider3d-kind").Text; got != "static" {
		t.Fatalf("collider kind = %q, want static", got)
	}

	// Dragging a model card from the bottom MODELS strip onto the 3D viewport places it undoably.
	beforeCount := len(objectsWith(ed, crateModel))
	app.DragTo("id:viewport3d-model-card-models-crate-glb", viewportFrame)
	afterDrag := objectsWith(ed, crateModel)
	if len(afterDrag) != beforeCount+1 {
		t.Fatalf("dragging from MODELS strip left %d crates, want %d", len(afterDrag), beforeCount+1)
	}
	app.Press("id:toolbar-undo")
	if len(objectsWith(ed, crateModel)) != beforeCount {
		t.Fatal("Undo did not remove model dragged from MODELS strip")
	}
	app.Press("id:toolbar-redo")
	if got := objectsWith(ed, crateModel); !slices.Equal(got, afterDrag) {
		t.Fatalf("Redo after MODELS strip drag = %v, want %v", got, afterDrag)
	}

	// "Or ask for one" opens the inline prompt with the expected placeholder and dispatches on submit.
	app.Press("id:viewport3d-ask-for-model")
	if !ed.Viewport3DAgentPromptOpen() || ed.Viewport3DAgentPromptKind() != editor.Viewport3DAgentPromptModel {
		t.Fatalf("after pressing Or ask for one: open=%v kind=%v", ed.Viewport3DAgentPromptOpen(), ed.Viewport3DAgentPromptKind())
	}
	if got := ed.Viewport3DAgentPromptPlaceholder(); got != "Describe a model… e.g. a low stone planter, 1 × 0.5 m, same stone as the walls" {
		t.Fatalf("model prompt placeholder = %q", got)
	}
	app.Type("id:viewport3d-agent-prompt-input", "low stone planter 1x0.5m")
	app.Press("id:viewport3d-agent-prompt-send")
	if len(asked) != 1 || asked[0].Kind != editor.Viewport3DAgentPromptModel || asked[0].Prompt != "low stone planter 1x0.5m" {
		t.Fatalf("asked = %+v", asked)
	}
	if ed.Viewport3DAgentPromptOpen() {
		t.Fatal("prompt bar stayed open after submit")
	}

	// Selecting the group shows CHILDREN and "Ask to add a component…" targets it.
	app.Post(func() { ed.SelectObject("gate") })
	app.Settle()
	if got := app.Find("id:inspector-children-summary").Text; got != "pillar_l · pillar_r · lintel" {
		t.Fatalf("inspector-children-summary = %q", got)
	}
	app.Press("id:inspector-ask-add-component")
	if got := ed.Viewport3DAgentPromptPlaceholder(); got != "Add a component to Gate… e.g. a trigger that plays splash.ogg" {
		t.Fatalf("component prompt placeholder = %q", got)
	}
	app.Type("id:viewport3d-agent-prompt-input", "trigger that plays splash.ogg")
	app.Press("id:viewport3d-agent-prompt-send")
	if len(asked) != 2 || asked[1].Kind != editor.Viewport3DAgentPromptComponent || asked[1].ObjectID != "gate" {
		t.Fatalf("asked after component prompt = %+v", asked)
	}

	// Switching to 2D hides the bottom 3D MODELS strip.
	app.Post(func() { ed.SetViewMode(editor.ViewMode2D) })
	app.Settle()
	if app.Exists("id:viewport3d-models-strip") {
		t.Fatal("viewport3d-models-strip is visible in 2D mode")
	}
}
