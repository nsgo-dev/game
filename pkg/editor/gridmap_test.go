package editor_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/scene3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/scenemodel"
	"github.com/nsgo-dev/game/pkg/tooling/assetcheck"
	"github.com/nsgo-dev/game/pkg/world3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// makeTestLibraryGLB builds a real binary glTF 2.0 mesh library with two distinct
// named box tiles ("stone_floor" and "stone_pillar") so both the editor thumbnails
// and scenemodel.Build load full triangle geometry from disk.
func makeTestLibraryGLB(t *testing.T) []byte {
	t.Helper()
	box, err := scenemodel.PrimitiveMesh("box")
	if err != nil {
		t.Fatal(err)
	}
	var bin bytes.Buffer
	type view struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
		Target     int `json:"target,omitempty"`
	}
	var views []view
	addView := func(data []byte, target int) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		views = append(views, view{ByteOffset: bin.Len(), ByteLength: len(data), Target: target})
		bin.Write(data)
		return len(views) - 1
	}
	var accessors []map[string]any
	addAcc := func(a map[string]any) int {
		accessors = append(accessors, a)
		return len(accessors) - 1
	}
	floats := func(vs ...float32) []byte {
		out := make([]byte, 4*len(vs))
		for i, v := range vs {
			binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(v))
		}
		return out
	}
	addBoxMesh := func(sx, sy, sz float32) map[string]int {
		var pos, nrm []float32
		minP := [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
		maxP := [3]float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
		for i, p := range box.Positions {
			q := [3]float32{p.X * sx, p.Y * sy, p.Z * sz}
			for k := range 3 {
				minP[k], maxP[k] = min(minP[k], q[k]), max(maxP[k], q[k])
			}
			pos = append(pos, q[0], q[1], q[2])
			n := box.Normals[i]
			nrm = append(nrm, n.X, n.Y, n.Z)
		}
		idx := make([]byte, 2*len(box.Indices))
		for i, v := range box.Indices {
			binary.LittleEndian.PutUint16(idx[2*i:], uint16(v))
		}
		return map[string]int{
			"pos": addAcc(map[string]any{
				"bufferView":    addView(floats(pos...), 34962),
				"componentType": 5126,
				"count":         len(box.Positions),
				"type":          "VEC3",
				"min":           minP,
				"max":           maxP,
			}),
			"nrm": addAcc(map[string]any{
				"bufferView":    addView(floats(nrm...), 34962),
				"componentType": 5126,
				"count":         len(box.Normals),
				"type":          "VEC3",
			}),
			"idx": addAcc(map[string]any{
				"bufferView":    addView(idx, 34963),
				"componentType": 5123,
				"count":         len(box.Indices),
				"type":          "SCALAR",
			}),
		}
	}

	floor := addBoxMesh(2, 0.4, 2)
	pillar := addBoxMesh(0.8, 2, 0.8)
	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}

	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"nodes": []map[string]any{
			{"name": "stone_floor", "mesh": 0},
			{"name": "stone_pillar", "mesh": 1},
		},
		"meshes": []map[string]any{
			{
				"name": "stone_floor",
				"primitives": []map[string]any{{
					"attributes": map[string]int{"POSITION": floor["pos"], "NORMAL": floor["nrm"]},
					"indices":    floor["idx"],
					"material":   0,
				}},
			},
			{
				"name": "stone_pillar",
				"primitives": []map[string]any{{
					"attributes": map[string]int{"POSITION": pillar["pos"], "NORMAL": pillar["nrm"]},
					"indices":    pillar["idx"],
					"material":   1,
				}},
			},
		},
		"materials": []map[string]any{
			{"name": "FloorMat", "pbrMetallicRoughness": map[string]any{"baseColorFactor": []float32{0.55, 0.6, 0.65, 1}, "metallicFactor": 0, "roughnessFactor": 0.9}},
			{"name": "PillarMat", "pbrMetallicRoughness": map[string]any{"baseColorFactor": []float32{0.8, 0.55, 0.35, 1}, "metallicFactor": 0.1, "roughnessFactor": 0.7}},
		},
		"buffers":     []map[string]any{{"byteLength": bin.Len()}},
		"bufferViews": views,
		"accessors":   accessors,
	}
	jb, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for len(jb)%4 != 0 {
		jb = append(jb, ' ')
	}
	total := 12 + 8 + len(jb) + 8 + bin.Len()
	out := make([]byte, total)
	binary.LittleEndian.PutUint32(out[0:4], 0x46546C67)
	binary.LittleEndian.PutUint32(out[4:8], 2)
	binary.LittleEndian.PutUint32(out[8:12], uint32(total))
	binary.LittleEndian.PutUint32(out[12:16], uint32(len(jb)))
	binary.LittleEndian.PutUint32(out[16:20], 0x4E4F534A)
	copy(out[20:], jb)
	off := 20 + len(jb)
	binary.LittleEndian.PutUint32(out[off:off+4], uint32(bin.Len()))
	binary.LittleEndian.PutUint32(out[off+4:off+8], 0x004E4942)
	copy(out[off+8:], bin.Bytes())
	return out
}

func setupGridMapProject(t *testing.T) (dir string, ed *editor.Editor) {
	t.Helper()
	dir = t.TempDir()
	for _, sub := range []string{"scenes", "levels", "models"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gameYAML := "version: 1\nid: dev.example.keep\nname: Keep\nappVersion: 0.1.0\nwindow:\n  width: 640\n  height: 360\nautoload:\n  mainScene: main\n  scenes:\n    - name: main\n      path: scenes/keep.scene.yaml\n"
	sceneYAML := `version: 2
name: Keep
layers:
  - name: world
    objects:
      - id: keep_grid
        gridMap: levels/keep.gridmap.yaml
`
	gridYAML := `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/gridmap.v1.json
version: 1
name: Keep
cellSize:
  - 2
  - 2
  - 2
meshLibrary: models/dungeon_tiles.glb
`
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(gameYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "keep.scene.yaml"), []byte(sceneYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "levels", "keep.gridmap.yaml"), []byte(gridYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "models", "dungeon_tiles.glb"), makeTestLibraryGLB(t), 0o644); err != nil {
		t.Fatal(err)
	}

	var err error
	ed, err = editor.New(nil, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.SetViewMode(editor.ViewMode3D)
	ed.SelectObject("keep_grid")
	ed.SetGridMapTool(editor.GridMapToolPaint)
	return dir, ed
}

// Acceptance #1: Painting in the editor writes a GridMap file that round-trips byte for byte
// and draws the same in the game (scenemodel.Build / BuildInstances).
func TestGridMapEditorPaintSaveRoundTripAndGameBuild(t *testing.T) {
	dir, ed := setupGridMapProject(t)

	pal := ed.GridMapPalette()
	if len(pal.Items) != 2 {
		t.Fatalf("palette items = %d, want 2 (msg=%q)", len(pal.Items), pal.Message)
	}
	if len(pal.Items[0].Thumbnail) == 0 || len(pal.Items[1].Thumbnail) == 0 {
		t.Fatal("expected non-empty 3D mesh thumbnails for library items")
	}

	// Box fill a 3x3 floor at Y=0 with stone_floor.
	if !ed.SetActiveGridMapTileByName("stone_floor") {
		t.Fatal("SetActiveGridMapTileByName(stone_floor) failed")
	}
	if err := ed.FillGridMapBox(world3d.GridCoord{X: 0, Y: 0, Z: 0}, world3d.GridCoord{X: 2, Y: 0, Z: 2}, ed.ActiveGridMapTile(), 0); err != nil {
		t.Fatalf("FillGridMapBox: %v", err)
	}

	// Raise floor height to Y=1, switch to stone_pillar, rotate to non-zero orientation, and paint two pillars.
	ed.StepGridMapFloor(1)
	if ed.GridMapFloor() != 1 {
		t.Fatalf("GridMapFloor = %d, want 1", ed.GridMapFloor())
	}
	if !ed.SetActiveGridMapTileByName("stone_pillar") {
		t.Fatal("SetActiveGridMapTileByName(stone_pillar) failed")
	}
	if err := ed.RotateGridMapOrientation(world3d.GridAxisY, 1); err != nil {
		t.Fatalf("RotateGridMapOrientation: %v", err)
	}
	if err := ed.PaintGridMapCell(world3d.GridCoord{X: 0, Y: 1, Z: 0}, ed.ActiveGridMapTile(), ed.GridMapOrientation()); err != nil {
		t.Fatalf("PaintGridMapCell 1: %v", err)
	}
	if err := ed.RotateGridMapOrientation(world3d.GridAxisX, 1); err != nil {
		t.Fatalf("RotateGridMapOrientation X: %v", err)
	}
	if err := ed.PaintGridMapCell(world3d.GridCoord{X: 2, Y: 1, Z: 2}, ed.ActiveGridMapTile(), ed.GridMapOrientation()); err != nil {
		t.Fatalf("PaintGridMapCell 2: %v", err)
	}

	if !ed.GridMapDirty() || !ed.HasUnsavedChanges() {
		t.Fatal("expected GridMapDirty and HasUnsavedChanges after painting")
	}
	if err := ed.Save(); err != nil {
		t.Fatalf("Editor.Save: %v", err)
	}
	if ed.GridMapDirty() {
		t.Fatal("GridMapDirty still true after Save")
	}

	// Verify saved file round-trips byte-for-byte through ParseGridMap + EncodeGridMap.
	diskBytes, err := os.ReadFile(filepath.Join(dir, "levels", "keep.gridmap.yaml"))
	if err != nil {
		t.Fatalf("ReadFile keep.gridmap.yaml: %v", err)
	}
	parsed, err := world3d.ParseGridMap(diskBytes)
	if err != nil {
		t.Fatalf("ParseGridMap: %v", err)
	}
	reencoded, err := world3d.EncodeGridMap(parsed)
	if err != nil {
		t.Fatalf("EncodeGridMap: %v", err)
	}
	if !bytes.Equal(diskBytes, reencoded) {
		t.Fatalf("saved .gridmap.yaml did not round-trip byte for byte:\n--- disk ---\n%s\n--- reencoded ---\n%s", diskBytes, reencoded)
	}

	// Verify game check passes cleanly on the saved project.
	report, err := assetcheck.Check(os.DirFS(dir))
	if err != nil {
		t.Fatalf("assetcheck.Check: %v", err)
	}
	if !report.OK() {
		t.Fatalf("assetcheck reported problems on saved project: %+v", report.Problems)
	}

	// Load the saved scene in the game with scenemodel.Build and assert identical cell transforms & draw items.
	sceneBytes, err := os.ReadFile(filepath.Join(dir, "scenes", "keep.scene.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sdoc, err := sceneformat.Parse(sceneBytes)
	if err != nil {
		t.Fatal(err)
	}
	gameScene := scene3d.NewScene()
	built, err := scenemodel.Build(sdoc, gameScene.Root(), scenemodel.NewAssets(os.DirFS(dir)))
	if err != nil {
		t.Fatalf("scenemodel.Build: %v", err)
	}
	gameGM := built.GridMaps["keep_grid"]
	edGM := ed.GridMap()
	if gameGM == nil || gameGM.Count() != edGM.Count() {
		t.Fatalf("game GridMap count = %v, want %d", gameGM, edGM.Count())
	}
	edBatches := edGM.BuildInstances(nil, nil)
	gameItems := built.AppendDrawItems(nil)
	var wantCount int
	for _, b := range edBatches {
		wantCount += len(b.Transforms)
	}
	if len(gameItems) != wantCount {
		t.Fatalf("game draw items = %d, want %d", len(gameItems), wantCount)
	}
	for _, b := range edBatches {
		for _, wantM := range b.Transforms {
			found := false
			for _, it := range gameItems {
				if it.Transform.ApproxEqual(wantM, 1e-4) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("editor cell transform %v missing from game draw items", wantM)
			}
		}
	}
}

// Acceptance #2: Paint, erase, box fill, and rotate are all undoable and redoable.
func TestGridMapPaintEraseBoxFillAndRotateUndoRedo(t *testing.T) {
	_, ed := setupGridMapProject(t)

	// Position camera above (1, 0, 1) looking down at floor Y=0.
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{2, 0, 2},
		Yaw:      0,
		Pitch:    1.2,
		Distance: 20,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      1000,
	}
	const w, h float32 = 640, 360
	projectCell := func(c world3d.GridCoord) (float32, float32) {
		center := ed.GridMap().CellTransform(c, 0)
		// Project the floor point at the center of cell c.
		pt := [3]float32{center[12], ed.GridMap().Origin.Y + float32(c.Y)*ed.GridMap().CellSize.Y, center[14]}
		sx, sy, _, vis := ed.Camera3D.Project(pt, w, h)
		if !vis {
			t.Fatalf("cell %v not visible", c)
		}
		return sx, sy
	}

	// 1. Paint stroke across (0,0,0) -> (2,0,0) commits as ONE undoable step.
	sx0, sy0 := projectCell(world3d.GridCoord{X: 0, Y: 0, Z: 0})
	sx2, sy2 := projectCell(world3d.GridCoord{X: 2, Y: 0, Z: 0})
	if !ed.BeginGridMapStroke(editor.GridMapToolPaint, sx0, sy0, w, h) {
		t.Fatal("BeginGridMapStroke paint failed")
	}
	ed.ContinueGridMapStroke(sx2, sy2)
	ed.EndGridMapStroke()
	if ed.GridMap().Count() != 3 {
		t.Fatalf("after paint stroke count = %d, want 3", ed.GridMap().Count())
	}

	// Undo removes entire paint stroke; Redo restores it.
	if !ed.Undo() || ed.GridMap().Count() != 0 {
		t.Fatalf("after undo paint count = %d, want 0", ed.GridMap().Count())
	}
	if ed.GridMapDirty() {
		t.Fatal("undoing back to initial state should clear GridMapDirty")
	}
	if !ed.Redo() || ed.GridMap().Count() != 3 {
		t.Fatalf("after redo paint count = %d, want 3", ed.GridMap().Count())
	}

	// 2. Erase stroke on (1,0,0) is undoable.
	sx1, sy1 := projectCell(world3d.GridCoord{X: 1, Y: 0, Z: 0})
	if !ed.BeginGridMapStroke(editor.GridMapToolErase, sx1, sy1, w, h) {
		t.Fatal("BeginGridMapStroke erase failed")
	}
	ed.EndGridMapStroke()
	if _, occupied := ed.GridMap().Cell(world3d.GridCoord{X: 1, Y: 0, Z: 0}); occupied {
		t.Fatal("cell (1,0,0) should be erased")
	}
	if !ed.Undo() {
		t.Fatal("Undo erase failed")
	}
	if _, occupied := ed.GridMap().Cell(world3d.GridCoord{X: 1, Y: 0, Z: 0}); !occupied {
		t.Fatal("Undo erase did not restore cell (1,0,0)")
	}

	// 3. Rotate orientation through X/Y/Z is undoable and redoable.
	if ed.GridMapOrientation() != 0 {
		t.Fatalf("initial orientation = %d, want 0", ed.GridMapOrientation())
	}
	if err := ed.RotateGridMapOrientation(world3d.GridAxisY, 1); err != nil {
		t.Fatal(err)
	}
	rot1 := ed.GridMapOrientation()
	if rot1 == 0 {
		t.Fatal("RotateGridMapOrientation Y did not change orientation")
	}
	if err := ed.RotateGridMapOrientation(world3d.GridAxisX, 1); err != nil {
		t.Fatal(err)
	}
	rot2 := ed.GridMapOrientation()
	if rot2 == rot1 {
		t.Fatal("RotateGridMapOrientation X did not change orientation")
	}
	if !ed.Undo() || ed.GridMapOrientation() != rot1 {
		t.Fatalf("Undo rotate 2: got %d, want %d", ed.GridMapOrientation(), rot1)
	}
	if !ed.Undo() || ed.GridMapOrientation() != 0 {
		t.Fatalf("Undo rotate 1: got %d, want 0", ed.GridMapOrientation())
	}
	if !ed.Redo() || ed.GridMapOrientation() != rot1 {
		t.Fatalf("Redo rotate 1: got %d, want %d", ed.GridMapOrientation(), rot1)
	}

	// 4. Rotating an occupied cell in place is also undoable.
	c0 := world3d.GridCoord{X: 0, Y: 0, Z: 0}
	if err := ed.RotateGridMapCell(c0, world3d.GridAxisZ, 1); err != nil {
		t.Fatal(err)
	}
	cellAfter, _ := ed.GridMap().Cell(c0)
	if cellAfter.Orientation == 0 {
		t.Fatal("RotateGridMapCell did not change cell orientation")
	}
	if !ed.Undo() {
		t.Fatal("Undo RotateGridMapCell failed")
	}
	cellRestored, _ := ed.GridMap().Cell(c0)
	if cellRestored.Orientation != 0 {
		t.Fatalf("Undo RotateGridMapCell restored orient %d, want 0", cellRestored.Orientation)
	}

	// 5. Box fill stroke via pointer drag previews bounds and commits as one undoable step.
	sxB, syB := projectCell(world3d.GridCoord{X: 1, Y: 0, Z: 2})
	if !ed.BeginGridMapStroke(editor.GridMapToolBoxFill, sx0, sy0, w, h) {
		t.Fatal("BeginGridMapStroke box fill failed")
	}
	ed.ContinueGridMapStroke(sxB, syB)
	minC, maxC, ok := ed.GridMapBoxPreview()
	if !ok || minC != (world3d.GridCoord{X: 0, Y: 0, Z: 0}) || maxC != (world3d.GridCoord{X: 1, Y: 0, Z: 2}) {
		t.Fatalf("GridMapBoxPreview = (%v, %v, %v), want (0,0,0)-(1,0,2)", minC, maxC, ok)
	}
	ed.EndGridMapStroke()
	if ed.GridMap().Count() != 7 { // (0,0,0)..(1,0,2) is 6 cells + existing (2,0,0) = 7
		t.Fatalf("after box fill count = %d, want 7", ed.GridMap().Count())
	}
	if !ed.Undo() || ed.GridMap().Count() != 3 {
		t.Fatalf("after undo box fill count = %d, want 3", ed.GridMap().Count())
	}

	// 6. Pick reads both item and orientation and switches Pick tool back to Paint.
	ed.SetActiveGridMapTileByName("stone_pillar")
	_ = ed.PaintGridMapCell(world3d.GridCoord{X: 3, Y: 0, Z: 3}, ed.ActiveGridMapTile(), 11)
	ed.SetActiveGridMapTileByName("stone_floor")
	_ = ed.SetGridMapOrientation(0)
	ed.SetGridMapTool(editor.GridMapToolPick)
	sxP, syP := projectCell(world3d.GridCoord{X: 3, Y: 0, Z: 3})
	if !ed.BeginGridMapStroke(editor.GridMapToolPick, sxP, syP, w, h) {
		t.Fatal("BeginGridMapStroke pick failed")
	}
	ed.EndGridMapStroke()
	pillarDef, _ := ed.GridMap().Library.ByName("stone_pillar")
	if ed.ActiveGridMapTile() != pillarDef.ID || ed.GridMapOrientation() != 11 {
		t.Fatalf("after pick: tile=%d orient=%d, want tile=%d orient=11", ed.ActiveGridMapTile(), ed.GridMapOrientation(), pillarDef.ID)
	}
	if ed.GridMapTool() != editor.GridMapToolPaint {
		t.Fatalf("after pick tool = %v, want Paint", ed.GridMapTool())
	}
}

// Mounted UI test: palette items + thumbnails, floor height keys (]/[), 24-orientation keys (x/y/z/q),
// undo shortcut (mod+z), hover cell preview, and 3D drawing.
func TestGridMapPaletteThumbnailsKeysPreviewAndMountedUI(t *testing.T) {
	_, ed := setupGridMapProject(t)
	ed.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{2, 0, 2},
		Yaw:      0.4,
		Pitch:    1.1,
		Distance: 20,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      1000,
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	// Palette panel and both item thumbnails are mounted in 3D GridMap mode.
	if app.Find("id:gridmap-palette") == nil {
		t.Fatal("gridmap-palette not mounted")
	}
	if app.Find("id:gridmap-palette-thumb-stone_floor") == nil || app.Find("id:gridmap-palette-thumb-stone_pillar") == nil {
		t.Fatal("expected palette thumbnails for stone_floor and stone_pillar")
	}

	// Selecting stone_pillar from the palette updates ActiveGridMapTile.
	app.Press("id:gridmap-palette-stone_pillar")
	pillarDef, _ := ed.GridMap().Library.ByName("stone_pillar")
	if ed.ActiveGridMapTile() != pillarDef.ID {
		t.Fatalf("ActiveGridMapTile = %d, want stone_pillar (%d)", ed.ActiveGridMapTile(), pillarDef.ID)
	}

	// Floor height keys ] and [ change GridMapFloor.
	app.Key("]")
	app.Key("]")
	if ed.GridMapFloor() != 2 {
		t.Fatalf("after ]] GridMapFloor = %d, want 2", ed.GridMapFloor())
	}
	app.Key("[")
	if ed.GridMapFloor() != 1 {
		t.Fatalf("after [ GridMapFloor = %d, want 1", ed.GridMapFloor())
	}

	// Rotation keys x, y, z, q rotate through orientations and undo restores them.
	app.Key("y")
	o1 := ed.GridMapOrientation()
	if o1 == 0 {
		t.Fatal("pressing 'y' did not rotate GridMap orientation")
	}
	app.Key("x")
	o2 := ed.GridMapOrientation()
	if o2 == o1 {
		t.Fatal("pressing 'x' did not rotate GridMap orientation")
	}
	app.Key("z")
	o3 := ed.GridMapOrientation()
	if o3 == o2 {
		t.Fatal("pressing 'z' did not rotate GridMap orientation")
	}
	// Cycle with 'q' 24 times returns to o3.
	for range world3d.GridOrientationCount {
		app.Key("q")
	}
	if ed.GridMapOrientation() != o3 {
		t.Fatalf("24 'q' steps landed on %d, want %d", ed.GridMapOrientation(), o3)
	}

	// Hover preview snaps to a cell on the active floor.
	cw, ch := ed.CanvasSize()
	ed.SetGridMapHover(cw*0.5, ch*0.5, cw, ch, false)
	hc, ok := ed.HoveredGridMapCell()
	if !ok || hc.Y != 1 {
		t.Fatalf("HoveredGridMapCell = (%v, %v), want ok with Y=1", hc, ok)
	}

	// DrawViewport3D renders floor grid, occupied cells, and hover preview cleanly.
	var drawing core.Drawing
	ed.DrawViewport3D(&drawing, core.Size{Width: core.Points(cw), Height: core.Points(ch)})
	if len(drawing.Ops) == 0 {
		t.Fatal("DrawViewport3D emitted no draw ops")
	}
}
