package editor_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

func assertMultipleOf(t *testing.T, label string, got, step, eps float64) {
	t.Helper()
	q := math.Round(got / step)
	want := q * step
	if math.Abs(got-want) > eps {
		t.Fatalf("%s = %v, not a multiple of %v (nearest %v, diff %v)", label, got, step, want, math.Abs(got-want))
	}
}

func findObjectInDoc(doc *sceneformat.SceneDoc, id string) *sceneformat.ObjectDoc {
	for li := range doc.Layers {
		for oi := range doc.Layers[li].Objects {
			if doc.Layers[li].Objects[oi].ID == id {
				return &doc.Layers[li].Objects[oi]
			}
		}
	}
	return nil
}

// #869 Acceptance 1: With a 16-point grid on, placing and dragging an object leaves x and y at multiples of 16 in the saved file.
func TestSnapGridPlaceAndDragSavesMultiplesOf16(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetActiveLayer("actors")
	ed.SetGridSize(16)
	ed.SetSnapToGrid(true)
	app := mountEditor(t, ed)

	// 1. Place a new object at a non-multiple world coordinate (53, 71) -> snaps to (48, 64).
	app.Post(func() { ed.SetTool(editor.ToolPlace) })
	app.Settle()
	px, py := ed.WorldToCanvas(53, 71)
	app.PressAt("id:editor-viewport", pt(px, py))

	placedID := ed.SelectedID
	if placedID == "" || placedID == "hero" {
		t.Fatalf("Place tool did not create a new object (SelectedID=%q)", placedID)
	}
	placed, _, _ := ed.FindObject(placedID)
	if placed.X != 48 || placed.Y != 64 {
		t.Fatalf("placed object at (%v, %v), want (48, 64)", placed.X, placed.Y)
	}

	// 2. Drag the placed object by a non-multiple delta (+23, +39) -> (71, 103) snaps to (64, 96).
	app.Post(func() { ed.SetTool(editor.ToolSelect) })
	app.Settle()
	ox, oy := ed.WorldToCanvas(placed.X, placed.Y)
	app.DragAt("id:editor-viewport", pt(ox, oy), pt(23, 39))

	// 3. Also drag the existing hero by (+19, +11) from (40, 40) -> (59, 51) snaps to (64, 48).
	hx, hy := ed.WorldToCanvas(40, 40)
	app.DragAt("id:editor-viewport", pt(hx+2, hy+2), pt(19, 11))

	// Verify saved YAML parses back with exact multiples of 16.
	saved := saveYAML(t, ed)
	doc, err := sceneformat.Parse(saved)
	if err != nil {
		t.Fatalf("sceneformat.Parse saved YAML: %v", err)
	}
	gotPlaced := findObjectInDoc(doc, placedID)
	gotHero := findObjectInDoc(doc, "hero")
	if gotPlaced == nil || gotHero == nil {
		t.Fatalf("missing objects in saved scene: placed=%v hero=%v", gotPlaced, gotHero)
	}
	assertMultipleOf(t, "placed.X", float64(gotPlaced.X), 16, 1e-5)
	assertMultipleOf(t, "placed.Y", float64(gotPlaced.Y), 16, 1e-5)
	if gotPlaced.X != 64 || gotPlaced.Y != 96 {
		t.Fatalf("saved placed = (%v, %v), want (64, 96)", gotPlaced.X, gotPlaced.Y)
	}
	assertMultipleOf(t, "hero.X", float64(gotHero.X), 16, 1e-5)
	assertMultipleOf(t, "hero.Y", float64(gotHero.Y), 16, 1e-5)
	if gotHero.X != 64 || gotHero.Y != 48 {
		t.Fatalf("saved hero = (%v, %v), want (64, 48)", gotHero.X, gotHero.Y)
	}

	// Verify Move tool click-to-move also snaps to multiples of 16.
	app.Post(func() {
		ed.SelectObject("hero")
		ed.SetTool(editor.ToolMove)
	})
	app.Settle()
	mx, my := ed.WorldToCanvas(119, 89)
	app.PressAt("id:editor-viewport", pt(mx, my))
	hero, _, _ := ed.FindObject("hero")
	if hero.X != 112 || hero.Y != 96 {
		t.Fatalf("Move tool press left hero at (%v, %v), want (112, 96)", hero.X, hero.Y)
	}
}

// #869 Acceptance 2: With a 15-degree step, a rotation drag saves a rotation that is a multiple of 15 degrees in radians.
func TestSnapRotationDragSavesMultiplesOf15Degrees(t *testing.T) {
	ed := newTestEditor(t)
	ed.SelectObject("hero")
	ed.SetRotationSnapDeg(15)
	ed.SetSnapRotation(true)
	app := mountEditor(t, ed)

	hx, hy := ed.WorldToCanvas(40, 40)
	const r = 72.0
	// Drag from 0° (hx+r, hy) to ~22° clockwise on screen -> snaps to 15° (π/12).
	ang22 := 22.0 * math.Pi / 180.0
	dx22 := float32(r*math.Cos(ang22) - r)
	dy22 := float32(r * math.Sin(ang22))
	app.DragAt("id:editor-viewport", pt(hx+float32(r), hy), pt(dx22, dy22))

	saved := saveYAML(t, ed)
	doc, err := sceneformat.Parse(saved)
	if err != nil {
		t.Fatalf("sceneformat.Parse saved YAML: %v", err)
	}
	gotHero := findObjectInDoc(doc, "hero")
	if gotHero == nil {
		t.Fatal("hero missing from saved scene")
	}
	stepRad := 15.0 * math.Pi / 180.0
	assertMultipleOf(t, "hero.Rotation", float64(gotHero.Rotation), stepRad, 1e-5)
	if math.Abs(float64(gotHero.Rotation)-stepRad) > 1e-5 {
		t.Fatalf("saved hero.Rotation = %v rad (%v°), want %v rad (15°)", gotHero.Rotation, float64(gotHero.Rotation)*180/math.Pi, stepRad)
	}
}

// Grid step defaults to the active layer's tile size when it has a tilemap, falls back to 8 without one, and respects explicit overrides and ResetGridSize.
func TestSnapGridDefaultsToActiveLayerTileSize(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Tiled",
		VirtualWidth:  320,
		VirtualHeight: 180,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "ground",
				Space: sceneformat.SpaceWorld,
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth:  16,
					TileHeight: 16,
					Width:      4,
					Height:     4,
					Tileset:    "tiles",
					Data:       make([]int, 16),
				},
			},
			{
				Name:  "wide",
				Space: sceneformat.SpaceWorld,
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth:  24,
					TileHeight: 12,
					Width:      2,
					Height:     2,
					Tileset:    "tiles",
					Data:       make([]int, 4),
				},
			},
			{
				Name:  "objects",
				Space: sceneformat.SpaceWorld,
			},
		},
	}
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if sx, sy := ed.EffectiveGridStep(); sx != 16 || sy != 16 {
		t.Fatalf("ground layer EffectiveGridStep = (%v, %v), want (16, 16)", sx, sy)
	}

	ed.SetActiveLayer("wide")
	if sx, sy := ed.EffectiveGridStep(); sx != 24 || sy != 12 {
		t.Fatalf("wide layer EffectiveGridStep = (%v, %v), want (24, 12)", sx, sy)
	}
	ed.SetSnapToGrid(true)
	if gx, gy := ed.SnapPosition2D(20, 10); gx != 24 || gy != 12 {
		t.Fatalf("non-square tile SnapPosition2D(20, 10) = (%v, %v), want (24, 12)", gx, gy)
	}

	ed.SetActiveLayer("objects")
	if sx, sy := ed.EffectiveGridStep(); sx != 8 || sy != 8 {
		t.Fatalf("layer without tilemap EffectiveGridStep = (%v, %v), want (8, 8)", sx, sy)
	}

	// Explicit override beats active layer tile size until ResetGridSize.
	ed.SetActiveLayer("ground")
	if !ed.SetGridSize(32) {
		t.Fatal("SetGridSize(32) failed")
	}
	if sx, sy := ed.EffectiveGridStep(); sx != 32 || sy != 32 {
		t.Fatalf("explicit GridSize EffectiveGridStep = (%v, %v), want (32, 32)", sx, sy)
	}
	ed.ResetGridSize()
	if sx, sy := ed.EffectiveGridStep(); sx != 16 || sy != 16 {
		t.Fatalf("after ResetGridSize EffectiveGridStep = (%v, %v), want (16, 16)", sx, sy)
	}
}

// Pixel snap snaps positions to whole points (1-unit increments); Scale snap snaps 2D scale handles to scale step increments.
func TestSnapPixelAndScale2D(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetSnapToPixel(true)
	app := mountEditor(t, ed)

	// Place at fractional coordinates (42.3, 55.7) -> snaps to (42, 56).
	app.Post(func() { ed.SetTool(editor.ToolPlace) })
	app.Settle()
	px, py := ed.WorldToCanvas(42.3, 55.7)
	app.PressAt("id:editor-viewport", pt(px, py))
	obj, _, _ := ed.FindObject(ed.SelectedID)
	if obj.X != 42 || obj.Y != 56 {
		t.Fatalf("pixel-snapped placed object = (%v, %v), want (42, 56)", obj.X, obj.Y)
	}

	// Scale snap with step 0.25 on 2D right-edge scale handle.
	app.Post(func() {
		ed.SetTool(editor.ToolSelect)
		ed.SelectObject("hero")
		ed.SetScaleSnapStep(0.25)
		ed.SetSnapScale(true)
		ed.SetView2D(editor.View2D{X: 40, Y: 40, Zoom: 4})
	})
	app.Settle()
	// Hero is 16x16 with origin (8,8), so right edge is at world X=48 (8 units from origin).
	// At zoom 4, 8 world units = 32 canvas pts. Drag right edge by +11 canvas pts -> raw factor (32+11)/32 = 1.34375 -> snaps to 1.25.
	ex, ey := ed.WorldToCanvas(48, 40)
	app.DragAt("id:editor-viewport", pt(ex, ey), pt(11, 0))
	hero, _, _ := ed.FindObject("hero")
	if hero.ScaleX == nil || math.Abs(float64(*hero.ScaleX)-1.25) > 1e-5 {
		t.Fatalf("scale-snapped hero.ScaleX = %v, want 1.25", hero.ScaleX)
	}
	if hero.ScaleY != nil {
		t.Fatalf("edge drag changed hero.ScaleY = %v, want nil", *hero.ScaleY)
	}
}

// Holding Ctrl/Cmd inverts snapping during a drag: disables snap when enabled, and temporarily enables snap when disabled.
func TestSnapModifierInvertsSnappingDuringDrag(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetGridSize(16)
	ed.SetSnapToGrid(true)
	app := mountEditor(t, ed)

	// 1. Grid snap ON + holding ctrl -> unsnapped move from (40, 40) by (+11, +13) -> (51, 53).
	hx, hy := ed.WorldToCanvas(40, 40)
	app.KeyDown("ctrl")
	app.DragAt("id:editor-viewport", pt(hx+2, hy+2), pt(11, 13))
	app.KeyUp("ctrl")
	hero, _, _ := ed.FindObject("hero")
	if math.Abs(float64(hero.X-51)) > 0.1 || math.Abs(float64(hero.Y-53)) > 0.1 {
		t.Fatalf("inverted drag (snap ON + ctrl) left hero at (%v, %v), want (51, 53)", hero.X, hero.Y)
	}

	// 2. Grid snap OFF + holding ctrl -> temporarily snaps to 16-pt grid: (51, 53) + (+10, +8) = (61, 61) -> snaps to (64, 64).
	app.Post(func() { ed.SetSnapToGrid(false) })
	app.Settle()
	hx, hy = ed.WorldToCanvas(hero.X, hero.Y)
	app.KeyDown("ctrl")
	app.DragAt("id:editor-viewport", pt(hx, hy), pt(10, 8))
	app.KeyUp("ctrl")
	hero, _, _ = ed.FindObject("hero")
	if hero.X != 64 || hero.Y != 64 {
		t.Fatalf("inverted drag (snap OFF + ctrl) left hero at (%v, %v), want (64, 64)", hero.X, hero.Y)
	}

	// 3. Rotation snap OFF + holding ctrl -> temporarily snaps rotation to 15°.
	app.Post(func() {
		ed.SetRotationSnapDeg(15)
		ed.SetSnapRotation(false)
	})
	app.Settle()
	hx, hy = ed.WorldToCanvas(hero.X, hero.Y)
	const r = 72.0
	ang20 := 20.0 * math.Pi / 180.0
	app.KeyDown("ctrl")
	app.DragAt("id:editor-viewport", pt(hx+float32(r), hy), pt(float32(r*math.Cos(ang20)-r), float32(r*math.Sin(ang20))))
	app.KeyUp("ctrl")
	hero, _, _ = ed.FindObject("hero")
	stepRad := 15.0 * math.Pi / 180.0
	assertMultipleOf(t, "inverted rotation", float64(hero.Rotation), stepRad, 1e-5)
}

// Multi-object selection moves as a whole: anchor snaps to grid and every peer moves by the exact same snapped delta (preserving relative offsets) in one undoable step.
func TestSnapMultiSelectionMovesAsAWhole(t *testing.T) {
	ed := newTestEditor(t)
	idB, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "peer", X: 50, Y: 45})
	if err != nil {
		t.Fatal(err)
	}
	// hero is at (40, 40), peer is at (50, 45) -> relative offset (+10, +5).
	ed.History.Clear()
	ed.SetGridSize(16)
	ed.SetSnapToGrid(true)
	ed.SelectObjects([]string{idB, "hero"})
	app := mountEditor(t, ed)

	before := saveYAML(t, ed)
	hx, hy := ed.WorldToCanvas(40, 40)
	// Drag hero by (+21, +19): (40+21, 40+19) = (61, 59) snaps to (64, 64), applied delta = (+24, +24).
	// peer should move from (50, 45) by (+24, +24) to (74, 69), preserving exact relative offset (+10, +5)!
	app.DragAt("id:editor-viewport", pt(hx, hy), pt(21, 19))

	hero, _, _ := ed.FindObject("hero")
	peer, _, _ := ed.FindObject(idB)
	if hero.X != 64 || hero.Y != 64 {
		t.Fatalf("primary hero = (%v, %v), want (64, 64)", hero.X, hero.Y)
	}
	if peer.X != 74 || peer.Y != 69 {
		t.Fatalf("peer = (%v, %v), want (74, 69) (preserving relative offset (10, 5))", peer.X, peer.Y)
	}
	checkOneUndoStep(t, app, ed, before)

	// Escape cancels an in-progress multi-object drag and restores every object.
	app.Post(func() {
		hx2, hy2 := ed.WorldToCanvas(hero.X, hero.Y)
		if h := ed.BeginDrag2D(hx2, hy2); h == editor.Handle2DNone {
			t.Error("BeginDrag2D failed")
		}
		ed.UpdateDrag2D(hx2+40, hy2+40, false)
	})
	app.Settle()
	app.Key("escape")
	hero, _, _ = ed.FindObject("hero")
	peer, _, _ = ed.FindObject(idB)
	if hero.X != 64 || hero.Y != 64 || peer.X != 74 || peer.Y != 69 {
		t.Fatalf("Escape did not restore multi-selection: hero=(%v,%v) peer=(%v,%v)", hero.X, hero.Y, peer.X, peer.Y)
	}
}

// 3D gizmo translate, rotate, and scale snap to grid/rotation/scale steps, and multi-object 3D translate moves the whole selection together.
func TestSnap3DGizmoTranslateRotateScale(t *testing.T) {
	const w, h float32 = 640, 360
	ed := newTest3DEditor(t,
		sceneformat.ObjectDoc{ID: "a", X: 0, Y: 0, PosZ: 0, Depth: 16},
		sceneformat.ObjectDoc{ID: "b", X: 5, Y: 3, PosZ: 7, Depth: 16},
	)
	ed.Camera3D = editor.OrbitCamera3D{Yaw: 0.65, Pitch: 0.55, Distance: 200, FOV: float32(math.Pi / 3), Near: 0.5, Far: 5000}
	ed.SetGridSize(16)
	ed.SetSnapToGrid(true)
	ed.SetRotationSnapDeg(15)
	ed.SetSnapRotation(true)
	ed.SetScaleSnapStep(0.25)
	ed.SetSnapScale(true)

	// 1. Multi-object 3D Translate along X: "a" (last = primary) snaps to multiple of 16, "b" moves by the same snapped delta.
	ed.SelectObjects([]string{"b", "a"})
	ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
	// Multi-selection 3D gizmo sits at the selection centroid ((0+5)/2, (0+3)/2, (0+7)/2) = (2.5, 1.5, 3.5).
	center := [3]float32{2.5, 1.5, 3.5}
	gLen := ed.GizmoLength3D(center)
	sx, sy, _, _ := ed.Camera3D.Project([3]float32{center[0] + gLen*0.6, center[1], center[2]}, w, h)
	tx, ty, _, _ := ed.Camera3D.Project([3]float32{center[0] + gLen*0.6 + 23, center[1], center[2]}, w, h)
	if axis := ed.BeginGizmoDrag3D(sx, sy, w, h); axis != editor.Axis3DX {
		t.Fatalf("picked %v, want Axis3DX", axis)
	}
	ed.UpdateGizmoDrag3D(tx, ty)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("EndGizmoDrag3D did not commit translate")
	}
	a, _, _ := ed.FindObject("a")
	b, _, _ := ed.FindObject("b")
	assertMultipleOf(t, "3D a.X", float64(a.X), 16, 1e-4)
	if a.X == 0 {
		t.Fatal("3D translate did not move a.X")
	}
	if math.Abs(float64((b.X-5)-a.X)) > 1e-4 || b.Y != 3 || b.PosZ != 7 {
		t.Fatalf("3D multi-selection peer b = (%v, %v, %v), want (%v, 3, 7)", b.X, b.Y, b.PosZ, 5+a.X)
	}

	// 2. 3D Rotate around Y with 15-degree step.
	ed.SelectObject("a")
	ed.SetGizmoMode3D(editor.Gizmo3DRotate)
	aPos := [3]float32{a.X, a.Y, a.PosZ}
	gLen = ed.GizmoLength3D(aPos)
	rx0, ry0, _, _ := ed.Camera3D.Project([3]float32{aPos[0] + gLen, aPos[1], aPos[2]}, w, h)
	ang := 23.0 * math.Pi / 180.0
	rx1, ry1, _, _ := ed.Camera3D.Project([3]float32{
		aPos[0] + gLen*float32(math.Cos(ang)),
		aPos[1],
		aPos[2] + gLen*float32(math.Sin(ang)),
	}, w, h)
	if !ed.BeginGizmoDragAxis3D(editor.Axis3DY, rx0, ry0, w, h) {
		t.Fatal("BeginGizmoDragAxis3D rotate Y failed")
	}
	ed.UpdateGizmoDrag3D(rx1, ry1)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("EndGizmoDrag3D did not commit rotate")
	}
	a, _, _ = ed.FindObject("a")
	stepRad := 15.0 * math.Pi / 180.0
	assertMultipleOf(t, "3D a.RotY", float64(a.RotY), stepRad, 1e-4)
	if a.RotY == 0 {
		t.Fatal("3D rotate did not change a.RotY")
	}

	// 3. 3D Scale along X with 0.25 step.
	ed.SetGizmoMode3D(editor.Gizmo3DScale)
	sx0, sy0, _, _ := ed.Camera3D.Project([3]float32{aPos[0] + gLen*0.6, aPos[1], aPos[2]}, w, h)
	sx1, sy1, _, _ := ed.Camera3D.Project([3]float32{aPos[0] + gLen*1.2, aPos[1], aPos[2]}, w, h)
	if !ed.BeginGizmoDragAxis3D(editor.Axis3DX, sx0, sy0, w, h) {
		t.Fatal("BeginGizmoDragAxis3D scale X failed")
	}
	ed.UpdateGizmoDrag3D(sx1, sy1)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("EndGizmoDrag3D did not commit scale")
	}
	a, _, _ = ed.FindObject("a")
	if a.ScaleX == nil {
		t.Fatal("3D scale left ScaleX nil")
	}
	assertMultipleOf(t, "3D a.ScaleX", float64(*a.ScaleX), 0.25, 1e-4)
}

// Optional 2D grid overlay draws outside tile tools when ShowGrid is on and follows the 2D view transform (pan, zoom, rotation).
func TestSnapGridOverlayDrawsAndFollowsView2D(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetTool(editor.ToolSelect) // outside tile tools
	ed.SetGridSize(16)

	countStrokes := func() int {
		var d core.Drawing
		ed.DrawViewport(&d, core.Size{Width: 320, Height: 180})
		n := 0
		for _, op := range d.Ops {
			if op.Kind == core.DrawStroke {
				n++
			}
		}
		return n
	}

	baseStrokes := countStrokes()
	ed.SetShowGrid(true)
	withGrid := countStrokes()
	if withGrid <= baseStrokes {
		t.Fatalf("ShowGrid did not add grid strokes outside tile tools (base=%d, withGrid=%d)", baseStrokes, withGrid)
	}

	// Panning, zooming, and rotating the 2D view still draws the grid overlay cleanly.
	ed.SetView2D(editor.View2D{X: 120, Y: -80, Zoom: 2, Rotation: 0.35})
	if afterTransform := countStrokes(); afterTransform <= baseStrokes {
		t.Fatalf("grid overlay disappeared after View2D transform (base=%d, after=%d)", baseStrokes, afterTransform)
	}
}

// Snap preferences persist per project in .game/editor.yaml (ViewStateFile), never in scene files, and stay isolated between projects.
func TestSnapSettingsPersistPerProjectNotInScenes(t *testing.T) {
	ed, dir := openTestProject(t)
	sceneBefore := saveYAML(t, ed)

	want := editor.SnapSettings{
		Grid:            true,
		GridSize:        16,
		Pixel:           true,
		Rotation:        true,
		RotationStepDeg: 30,
		Scale:           true,
		ScaleStep:       0.5,
		ShowGrid:        true,
	}
	if !ed.SetSnapSettings(want) {
		t.Fatal("SetSnapSettings failed")
	}
	if ed.SceneDirty() {
		t.Fatal("changing snap preferences marked the scene dirty")
	}
	if got := saveYAML(t, ed); string(got) != string(sceneBefore) {
		t.Fatalf("changing snap preferences altered scene YAML:\n%s", got)
	}

	if err := ed.SaveViewState(); err != nil {
		t.Fatalf("SaveViewState: %v", err)
	}
	statePath := filepath.Join(dir, filepath.FromSlash(editor.ViewStateFile))
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read %s: %v", statePath, err)
	}
	if !strings.Contains(string(data), "snap:") || !strings.Contains(string(data), "gridSize: 16") {
		t.Fatalf("view state file missing snap settings:\n%s", data)
	}

	// Switching scenes within the same project keeps project-level snap preferences.
	if err := os.WriteFile(filepath.Join(dir, "scenes", "hall.yaml"), sceneBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenScene("scenes/hall.yaml"); err != nil {
		t.Fatal(err)
	}
	if got := ed.SnapSettings(); got != want {
		t.Fatalf("snap settings after switching scene = %+v, want %+v", got, want)
	}

	// Reopening the project in a fresh Editor restores the exact snap preferences.
	again, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.WaitForProblems)
	if err := again.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if got := again.SnapSettings(); got != want {
		t.Fatalf("reopened project snap settings = %+v, want %+v", got, want)
	}

	// Opening a different project with no saved snap settings resets to defaults.
	other := writeEditorProject(t)
	if err := again.OpenProject(other); err != nil {
		t.Fatal(err)
	}
	if got := again.SnapSettings(); got != editor.DefaultSnapSettings() {
		t.Fatalf("other project snap settings = %+v, want defaults %+v", got, editor.DefaultSnapSettings())
	}
}

// Toolbar snap popover toggles all settings, commands work via registry, and the 2D toolbar stays compact (<= 2 rows) at 1120x680.
func TestSnapToolbarPopoverAndCompactLayout(t *testing.T) {
	ed := newTestEditor(t)
	app := mountEditor(t, ed)

	// Verify 2D viewport bar Snap switch works and header stays 44 pt single-row at 1120x680.
	snapToggle := app.Find("id:viewport-bar-snap")
	if snapToggle == nil {
		t.Fatal("viewport-bar-snap control not found")
	}
	if h := app.Find("id:editor-toolbar").Bounds().Size.Height; math.Abs(float64(h-44)) > 1 {
		t.Fatalf("editor-toolbar height = %v, want 44", h)
	}

	// Open the Snap Settings popover directly in 2D and toggle every setting via UI.
	app.Press("id:view-snap-settings")
	app.Settle()
	app.Press("id:snap-show-grid")
	app.Press("id:snap-grid-toggle")
	app.Press("id:snap-pixel-toggle")
	app.Press("id:snap-rotation-toggle")
	app.Press("id:snap-scale-toggle")

	if !ed.ShowGrid || !ed.SnapToGrid || !ed.SnapToPixel || !ed.SnapRotation || !ed.SnapScale {
		t.Fatalf("2D popover toggles did not update editor: %+v", ed.SnapSettings())
	}

	// Switch to 3D where the Snap Settings popover also lives and verify it opens cleanly there too.
	app.Press("id:view-mode-3d")
	app.Settle()
	app.Press("id:view3d-snap-settings")
	app.Settle()
	if !ed.SnapSettingsPopoverOpen() {
		t.Fatal("expected 3D view3d-snap-settings button to open snap settings popover")
	}

	// Commands toggle each setting cleanly through ExecuteCommand.
	for _, id := range []string{
		editor.CmdToggleShowGrid,
		editor.CmdToggleSnapGrid,
		editor.CmdToggleSnapPixel,
		editor.CmdToggleSnapRotation,
		editor.CmdToggleSnapScale,
	} {
		if err := ed.ExecuteCommand(id); err != nil {
			t.Fatalf("ExecuteCommand(%q): %v", id, err)
		}
	}
	if ed.ShowGrid || ed.SnapToGrid || ed.SnapToPixel || ed.SnapRotation || ed.SnapScale {
		t.Fatalf("commands did not toggle off snap settings: %+v", ed.SnapSettings())
	}
}

// Extreme zoom, tiny GridSize (1e-30), and non-finite inputs must never hang, panic, or emit non-finite coordinates.
func TestSnapOverlayAndStepExtremeZoomTinyStepAndNonFinite(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetShowGrid(true)
	ed.SetSnapToGrid(true)

	// Tiny GridSize with huge coordinate must not overflow snapStep64 to Inf/NaN.
	if !ed.SetGridSize(1e-30) {
		t.Fatal("SetGridSize(1e-30) failed")
	}
	big := 1e300
	if got := ed.SnapValue(big); math.IsNaN(got) || math.IsInf(got, 0) {
		t.Fatalf("SnapValue(%v) with tiny step = %v, want finite", big, got)
	}
	if got := ed.SnapValue(math.NaN()); !math.IsNaN(got) {
		t.Fatalf("SnapValue(NaN) = %v, want NaN", got)
	}
	if got := ed.SnapValue(math.Inf(1)); !math.IsInf(got, 1) {
		t.Fatalf("SnapValue(+Inf) = %v, want +Inf", got)
	}

	// Drawing the 2D snap grid overlay with tiny GridSize and extreme zoom-out must terminate quickly without panic.
	ed.SetView2D(editor.View2D{X: 0, Y: 0, Zoom: 1e-4})
	var draw core.Drawing
	ed.DrawViewport(&draw, core.Size{Width: core.Points(800), Height: core.Points(600)})

	// Very large canvas and very small step must stay bounded.
	ed.SetGridSize(1e-20)
	ed.SetView2D(editor.View2D{X: 1e6, Y: -1e6, Zoom: 1e-3})
	var draw2 core.Drawing
	ed.DrawViewport(&draw2, core.Size{Width: core.Points(2000), Height: core.Points(2000)})
}

// Resetting snap preferences back to defaults clears any previously saved snap block from .game/editor.yaml
// even when no scene cameras are open, and OpenDemo resets snap preferences to defaults.
func TestSnapResetClearsStaleViewStateAndOpenDemoResetsSnap(t *testing.T) {
	dir := writeEditorProject(t)
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}

	// Save non-default snap preferences while clearing ScenePath so len(cams) == 0.
	ed.ScenePath = ""
	ed.SetSnapToGrid(true)
	ed.SetGridSize(16)
	ed.SetShowGrid(true)
	if err := ed.SaveViewState(); err != nil {
		t.Fatalf("SaveViewState (non-default): %v", err)
	}
	vsPath := filepath.Join(dir, editor.ViewStateFile)
	data, err := os.ReadFile(vsPath)
	if err != nil {
		t.Fatalf("read ViewStateFile: %v", err)
	}
	if !strings.Contains(string(data), "snap:") {
		t.Fatalf("expected snap block in ViewStateFile:\n%s", data)
	}

	// Reset all snap settings back to defaults and save again with len(cams) == 0: stale snap block must be cleared!
	if !ed.SetSnapSettings(editor.DefaultSnapSettings()) {
		t.Fatal("SetSnapSettings(DefaultSnapSettings()) failed")
	}
	if err := ed.SaveViewState(); err != nil {
		t.Fatalf("SaveViewState (reset): %v", err)
	}
	dataAfter, err := os.ReadFile(vsPath)
	if err != nil {
		t.Fatalf("read ViewStateFile after reset: %v", err)
	}
	if strings.Contains(string(dataAfter), "snap:") {
		t.Fatalf("stale snap block remained in ViewStateFile after reset:\n%s", dataAfter)
	}

	// Opening the Demo Room after enabling snapping in a project must reset snap settings to defaults.
	ed.SetSnapToGrid(true)
	ed.SetGridSize(32)
	ed.SetSnapRotation(true)
	if err := ed.OpenDemo(); err != nil {
		t.Fatalf("OpenDemo: %v", err)
	}
	if got := ed.SnapSettings(); got != editor.DefaultSnapSettings() {
		t.Fatalf("SnapSettings after OpenDemo = %+v, want defaults %+v", got, editor.DefaultSnapSettings())
	}
}

// Grid/pixel snapping applies to #1156 Gizmo3DCollider handles, #1170 built-in collider3d component gizmo handles,
// and #1174 pointer Paste in 2D.
func TestSnapAppliesToCollider3DGizmoComponentGizmoAndPointerPaste(t *testing.T) {
	const w, h float32 = 800, 600

	// 1. #1156 Gizmo3DCollider handle drag snaps box width to grid step 4.
	edCol := newTest3DEditor(t, sceneformat.ObjectDoc{
		ID:    "box",
		Name:  "Box",
		Depth: 10,
		Components: map[string]any{
			"collider3d": map[string]any{
				"version": 1,
				"shape":   "box",
				"width":   8.0,
				"height":  8.0,
				"depth":   8.0,
			},
		},
	})
	edCol.Camera3D = editor.OrbitCamera3D{
		Target:   [3]float32{0, 0, 0},
		Yaw:      0.5,
		Pitch:    0.35,
		Distance: 120,
		FOV:      float32(math.Pi / 3),
		Near:     0.5,
		Far:      5000,
	}
	edCol.SelectObject("box")
	edCol.SetGridSize(4)
	edCol.SetSnapToGrid(true)
	edCol.SetGizmoMode3D(editor.Gizmo3DCollider)

	proj := edCol.Projection3D(w, h)
	hx0, hy0, _, vis0 := proj.Project(math3d.Vec3{X: 4, Y: 0, Z: 0})
	hx1, hy1, _, vis1 := proj.Project(math3d.Vec3{X: 7.3, Y: 0, Z: 0})
	if !vis0 || !vis1 {
		t.Fatal("collider3d +X handle points not visible")
	}
	if axis := edCol.BeginGizmoDrag3D(hx0, hy0, w, h); axis != editor.Axis3DX {
		t.Fatalf("BeginGizmoDrag3D in Gizmo3DCollider = %v, want Axis3DX", axis)
	}
	edCol.UpdateGizmoDrag3D(hx1, hy1)
	if !edCol.EndGizmoDrag3D() {
		t.Fatal("EndGizmoDrag3D in Gizmo3DCollider returned false")
	}
	if gotW, ok := edCol.ComponentField("box", "collider3d", "width"); !ok {
		t.Fatal("collider3d width missing")
	} else {
		assertMultipleOf(t, "Gizmo3DCollider snapped width", gotW.(float64), 4, 1e-4)
	}

	// 2. #1170 built-in collider3d component gizmo (in Gizmo3DTranslate mode) snaps radius to grid step 4.
	edComp := newTest3DEditor(t, sceneformat.ObjectDoc{
		ID:    "sph",
		Name:  "Sphere",
		Depth: 10,
		Components: map[string]any{
			"collider3d": map[string]any{
				"version": 1,
				"shape":   "sphere",
				"radius":  8.0,
			},
		},
	})
	edComp.Camera3D = edCol.Camera3D
	edComp.ShowPhysicsDebug = true
	edComp.SelectObject("sph")
	edComp.SetGridSize(4)
	edComp.SetSnapToGrid(true)
	edComp.SetGizmoMode3D(editor.Gizmo3DTranslate)

	cProj := edComp.Projection3D(w, h)
	rx0, ry0, _, rvis0 := cProj.Project(math3d.Vec3{X: 8, Y: 0, Z: 0})
	rx1, ry1, _, rvis1 := cProj.Project(math3d.Vec3{X: 13.7, Y: 0, Z: 0})
	if !rvis0 || !rvis1 {
		t.Fatal("sphere component gizmo handle points not visible")
	}
	if _, ok := edComp.BeginComponentGizmoDrag3D(rx0, ry0, w, h); !ok {
		t.Fatal("BeginComponentGizmoDrag3D failed")
	}
	edComp.UpdateComponentGizmoDrag3D(rx1, ry1)
	if !edComp.EndComponentGizmoDrag3D() {
		t.Fatal("EndComponentGizmoDrag3D returned false")
	}
	if gotR, ok := edComp.ComponentField("sph", "collider3d", "radius"); !ok {
		t.Fatal("collider3d radius missing")
	} else {
		assertMultipleOf(t, "ComponentGizmo3D snapped radius", gotR.(float64), 4, 1e-4)
	}

	// 3. #1174 pointer Paste in 2D snaps the pasted object to the 16-pt grid.
	ed2D := newTestEditor(t)
	ed2D.SetGridSize(16)
	ed2D.SetSnapToGrid(true)
	ed2D.SelectObject("hero")
	if _, err := ed2D.CopySelected(); err != nil {
		t.Fatalf("CopySelected: %v", err)
	}
	px, py := ed2D.WorldToCanvas(53, 71)
	ed2D.SetViewportPointer2D(px, py)
	pasted, err := ed2D.Paste()
	if err != nil || len(pasted) != 1 {
		t.Fatalf("Paste = (%v, %v), want 1 object", pasted, err)
	}
	pObj, _, _ := ed2D.FindObject(pasted[0])
	if pObj == nil {
		t.Fatal("pasted object not found")
	}
	assertMultipleOf(t, "pasted X", float64(pObj.X), 16, 1e-4)
	assertMultipleOf(t, "pasted Y", float64(pObj.Y), 16, 1e-4)
	if pObj.X != 48 || pObj.Y != 64 {
		t.Fatalf("pasted at (%v, %v), want (48, 64)", pObj.X, pObj.Y)
	}
}
