package editor_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func testResolver() sceneformat.AtlasMap {
	return sceneformat.AtlasMap{
		"tiles": &sprite.Atlas{
			Image: core.ImageSource{URI: "asset:tiles.png"},
			Frames: map[string]sprite.Frame{
				"1": {Name: "1", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
				"2": {Name: "2", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
				"3": {Name: "3", X: 32, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			},
		},
		"actors": &sprite.Atlas{
			Image: core.ImageSource{URI: "asset:actors.png"},
			Frames: map[string]sprite.Frame{
				"hero":  {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
				"slime": {Name: "slime", X: 16, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
			},
		},
	}
}

func testSceneDoc() *sceneformat.SceneDoc {
	return &sceneformat.SceneDoc{
		Version:         sceneformat.CurrentVersion,
		Name:            "Test Level",
		VirtualWidth:    160,
		VirtualHeight:   120,
		BackgroundColor: "#141824",
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "floor",
				Space: sceneformat.SpaceWorld,
				Order: 0,
				Tilemap: &sceneformat.TilemapDoc{
					TileWidth:  16,
					TileHeight: 16,
					Width:      4,
					Height:     4,
					Tileset:    "tiles",
					Data: []int{
						1, 1, 1, 1,
						1, 0, 0, 1,
						1, 0, 0, 1,
						1, 1, 1, 1,
					},
				},
			},
			{
				Name:  "actors",
				Space: sceneformat.SpaceWorld,
				Order: 10,
				Objects: []sceneformat.ObjectDoc{
					{
						ID:      "hero",
						Name:    "Hero",
						X:       40,
						Y:       40,
						OriginX: 8,
						OriginY: 8,
						Z:       2,
						Sprite:  &sceneformat.SpriteRef{Atlas: "actors", Frame: "hero"},
						Components: map[string]any{
							"physics": map[string]any{
								"solid":   true,
								"gravity": 9.8,
								"layer":   1,
								"kind":    "dynamic",
							},
						},
						Properties: map[string]any{
							"title":  "Knight",
							"hp":     10,
							"speed":  2.5,
							"active": true,
						},
					},
				},
			},
		},
	}
}

func newTestEditor(t *testing.T) *editor.Editor {
	t.Helper()
	ed, err := editor.New(testSceneDoc(), testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetActiveLayer("actors")
	return ed
}

// #161: Place, move, rotate, scale object -> save YAML -> reload -> assert equal!
func TestPlaceMoveRotateScaleSaveReload(t *testing.T) {
	ed := newTestEditor(t)
	ed.Prefabs["enemy_slime"] = editor.Prefab{
		Name:   "Slime",
		Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"},
		Z:      3,
		Properties: map[string]any{
			"hostile": true,
		},
	}

	id, err := ed.PlacePrefab("actors", "enemy_slime", 80, 60)
	if err != nil {
		t.Fatalf("PlacePrefab: %v", err)
	}
	if ed.SelectedID != id {
		t.Fatalf("SelectedID = %q, want %q", ed.SelectedID, id)
	}

	// Select via canvas click hit test.
	ed.SelectObject("")
	ed.SetTool(editor.ToolSelect)
	ed.HandleCanvasPress(84, 64)
	if ed.SelectedID != id {
		t.Fatalf("canvas click SelectedID = %q, want %q", ed.SelectedID, id)
	}

	if err := ed.MoveObject(id, 96, 72); err != nil {
		t.Fatalf("MoveObject: %v", err)
	}
	if err := ed.RotateObject(id, 0.75); err != nil {
		t.Fatalf("RotateObject: %v", err)
	}
	if err := ed.ScaleObject(id, 1.5, 2.0); err != nil {
		t.Fatalf("ScaleObject: %v", err)
	}

	saved, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}

	reloaded, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatalf("editor.New reloaded: %v", err)
	}
	if err := reloaded.OpenYAML(saved); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}

	if !reflect.DeepEqual(reloaded.Doc, ed.Doc) {
		t.Fatalf("reloaded SceneDoc mismatch:\ngot:  %#v\nwant: %#v", reloaded.Doc, ed.Doc)
	}

	gotObj, _, _ := reloaded.FindObject(id)
	if gotObj == nil {
		t.Fatalf("placed object %q missing after reload", id)
	}
	if gotObj.X != 96 || gotObj.Y != 72 || gotObj.Rotation != 0.75 ||
		gotObj.EffectiveScaleX() != 1.5 || gotObj.EffectiveScaleY() != 2.0 {
		t.Fatalf("unexpected reloaded transform: %+v", gotObj)
	}
}

// #162: Paint, erase, flood-fill tiles -> save -> build & draw -> verify recorded core.DrawOps!
func TestTilePaintEraseFloodFillAndDrawOps(t *testing.T) {
	ed := newTestEditor(t)

	// Initial floor grid (4x4):
	// 1 1 1 1
	// 1 0 0 1
	// 1 0 0 1
	// 1 1 1 1

	// Flood-fill interior 2x2 zeros with tile 2 in one command.
	if err := ed.FloodFillTile("floor", 1, 1, 2); err != nil {
		t.Fatalf("FloodFillTile: %v", err)
	}
	wantAfterFill := []int{
		1, 1, 1, 1,
		1, 2, 2, 1,
		1, 2, 2, 1,
		1, 1, 1, 1,
	}
	if got := ed.Doc.Layers[0].Tilemap.Data; !reflect.DeepEqual(got, wantAfterFill) {
		t.Fatalf("after flood fill = %v, want %v", got, wantAfterFill)
	}

	// Paint corner (0,0) with tile 3 and erase (3,3).
	if err := ed.PaintTile("floor", 0, 0, 3); err != nil {
		t.Fatalf("PaintTile: %v", err)
	}
	if err := ed.EraseTile("floor", 3, 3); err != nil {
		t.Fatalf("EraseTile: %v", err)
	}

	wantFinal := []int{
		3, 1, 1, 1,
		1, 2, 2, 1,
		1, 2, 2, 1,
		1, 1, 1, 0,
	}
	if got := ed.Doc.Layers[0].Tilemap.Data; !reflect.DeepEqual(got, wantFinal) {
		t.Fatalf("final tile data = %v, want %v", got, wantFinal)
	}

	// Save YAML, reload, build, and verify recorded DrawOps on a core.Canvas.
	raw, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	parsed, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	built, err := sceneformat.Build(parsed, testResolver())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "tile-scene",
			Style:  core.Style{Width: core.Pt(160), Height: core.Pt(120)},
			Draw: func(d *core.Drawing, _ core.Size) {
				built.Draw(d)
			},
		}
	})

	ops := app.Find("tile-scene").Ops
	// Count tiles drawn from tiles.png (15 non-zero tiles: 10 of tile 1, 4 of tile 2, 1 of tile 3).
	var tiles []gametest.ImageDraw
	for _, im := range gametest.ImageDraws(ops) {
		if im.Image.URI == "asset:tiles.png" {
			tiles = append(tiles, im)
		}
	}
	if len(tiles) != 15 {
		t.Fatalf("recorded tile image draws = %d, want 15", len(tiles))
	}
	// First tile (0,0) should be tile 3 (source X=32, dest X=0, Y=0).
	if tiles[0].SX != 32 || tiles[0].SY != 0 || tiles[0].Corners[0] != (core.Point{}) {
		t.Fatalf("tile (0,0) = %+v, want src X=32 at (0,0)", tiles[0])
	}
	// Tile (1,1) should be tile 2 (source X=16, dest X=16, Y=16).
	foundFillTile := false
	for _, im := range tiles {
		if im.Corners[0] == (core.Point{X: 16, Y: 16}) && im.SX == 16 && im.SY == 0 {
			foundFillTile = true
			break
		}
	}
	if !foundFillTile {
		t.Fatalf("expected flood-filled tile 2 at (16,16) in the drawn images: %+v", tiles)
	}
	// Tile (3,3) was erased and must not be drawn.
	for _, im := range tiles {
		if im.Corners[0] == (core.Point{X: 48, Y: 48}) {
			t.Fatalf("erased tile (3,3) should not be drawn: %+v", im)
		}
	}
}

// #163: Edit typed properties via inspector -> save -> reload & verify applied!
func TestInspectorEditsTypedPropertiesAndSaves(t *testing.T) {
	ed := newTestEditor(t)
	ed.SelectObject("hero")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))

	// Verify inspector shows selected hero.
	if got := app.Find("id:inspector-object-id").Text; got != "hero" {
		t.Fatalf("inspector object id = %q, want hero", got)
	}

	// Toggle bool property "active" via native ui.Checkbox in the Inspector UI.
	app.Press("id:inspector-prop-active")
	// Edit string property "title" via native ui.Input in the Inspector UI.
	app.Clear("id:inspector-prop-title")
	app.Type("id:inspector-prop-title", "Paladin")

	// Edit int, float, and component fields via Inspector API (all going through History).
	app.Post(func() {
		if err := ed.SetProperty("hero", "hp", 25); err != nil {
			t.Errorf("SetProperty hp: %v", err)
		}
		if err := ed.SetProperty("hero", "speed", 4.25); err != nil {
			t.Errorf("SetProperty speed: %v", err)
		}
		if err := ed.SetComponentField("hero", "physics", "solid", false); err != nil {
			t.Errorf("SetComponentField solid: %v", err)
		}
		if err := ed.SetComponentField("hero", "physics", "gravity", 12.5); err != nil {
			t.Errorf("SetComponentField gravity: %v", err)
		}
		if err := ed.SetObjectAlpha("hero", 0.5); err != nil {
			t.Errorf("SetObjectAlpha: %v", err)
		}
		if err := ed.SetObjectZ("hero", 9); err != nil {
			t.Errorf("SetObjectZ: %v", err)
		}
	})
	app.Settle()

	raw, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	reloaded, err := sceneformat.Parse(raw)
	if err != nil {
		t.Fatalf("Parse reloaded: %v", err)
	}

	var hero *sceneformat.ObjectDoc
	for i := range reloaded.Layers {
		for j := range reloaded.Layers[i].Objects {
			if reloaded.Layers[i].Objects[j].ID == "hero" {
				hero = &reloaded.Layers[i].Objects[j]
			}
		}
	}
	if hero == nil {
		t.Fatal("hero missing after reload")
	}
	if hero.Properties["active"] != false {
		t.Fatalf("active = %#v, want false", hero.Properties["active"])
	}
	if hero.Properties["title"] != "Paladin" {
		t.Fatalf("title = %#v, want Paladin", hero.Properties["title"])
	}
	if hero.Properties["hp"] != 25 {
		t.Fatalf("hp = %#v, want 25", hero.Properties["hp"])
	}
	if hero.Properties["speed"] != 4.25 {
		t.Fatalf("speed = %#v, want 4.25", hero.Properties["speed"])
	}
	phys, _ := hero.Components["physics"].(map[string]any)
	if phys["solid"] != false || phys["gravity"] != 12.5 {
		t.Fatalf("physics component = %#v, want solid=false gravity=12.5", phys)
	}
	if hero.EffectiveAlpha() != 0.5 || hero.Z != 9 {
		t.Fatalf("alpha=%v z=%d, want 0.5 and 9", hero.EffectiveAlpha(), hero.Z)
	}
}

// #164: Every edit type undone & redone restores the exact previous YAML byte-for-byte!
func TestUndoRedoRestoresExactYAMLForEveryEditType(t *testing.T) {
	ed := newTestEditor(t)

	mustYAML := func(step string) []byte {
		t.Helper()
		b, err := ed.SaveYAML()
		if err != nil {
			t.Fatalf("%s: SaveYAML: %v", step, err)
		}
		return b
	}

	type editStep struct {
		name string
		do   func() error
	}
	steps := []editStep{
		{"PlaceObject", func() error {
			_, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{
				ID:     "placed_slime",
				Name:   "Slime",
				X:      20,
				Y:      30,
				Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"},
			})
			return err
		}},
		{"MoveObject", func() error { return ed.MoveObject("placed_slime", 55, 65) }},
		{"RotateObject", func() error { return ed.RotateObject("placed_slime", 1.25) }},
		{"ScaleObject", func() error { return ed.ScaleObject("placed_slime", 2, 0.5) }},
		{"SetObjectAlpha", func() error { return ed.SetObjectAlpha("placed_slime", 0.6) }},
		{"SetObjectZ", func() error { return ed.SetObjectZ("placed_slime", 7) }},
		{"SetObjectName", func() error { return ed.SetObjectName("placed_slime", "Big Slime") }},
		{"SetProperty_new_map", func() error { return ed.SetProperty("placed_slime", "boss", true) }},
		{"SetProperty_existing", func() error { return ed.SetProperty("hero", "hp", 99) }},
		{"DeleteProperty", func() error { return ed.DeleteProperty("hero", "speed") }},
		{"SetComponentField_new", func() error { return ed.SetComponentField("placed_slime", "ai", "aggro", 120) }},
		{"SetComponentField_existing", func() error { return ed.SetComponentField("hero", "physics", "solid", false) }},
		{"PaintTile", func() error { return ed.PaintTile("floor", 0, 0, 2) }},
		{"EraseTile", func() error { return ed.EraseTile("floor", 3, 0) }},
		{"FloodFillTile", func() error { return ed.FloodFillTile("floor", 1, 1, 3) }},
		{"DropAtlasFrame", func() error {
			_, err := ed.DropAtlasFrame("actors", "slime", 100, 80)
			return err
		}},
		{"DeleteObject", func() error { return ed.DeleteObject("placed_slime") }},
	}

	snapshots := make([][]byte, 0, len(steps)+1)
	snapshots = append(snapshots, mustYAML("initial"))

	for _, st := range steps {
		if err := st.do(); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		snapshots = append(snapshots, mustYAML("after "+st.name))
	}

	// Undo every single edit in reverse order and assert byte-for-byte YAML equality at every step.
	for i := len(steps) - 1; i >= 0; i-- {
		if !ed.Undo() {
			t.Fatalf("Undo at step %d (%s) returned false", i, steps[i].name)
		}
		got := mustYAML("undo " + steps[i].name)
		if !bytes.Equal(got, snapshots[i]) {
			t.Fatalf("after undoing %s YAML differs!\ngot:\n%s\nwant:\n%s", steps[i].name, got, snapshots[i])
		}
	}
	if ed.History.CanUndo() {
		t.Fatal("CanUndo should be false at base state")
	}

	// Redo every single edit forward and assert byte-for-byte YAML equality at every step.
	for i := 0; i < len(steps); i++ {
		if !ed.Redo() {
			t.Fatalf("Redo at step %d (%s) returned false", i, steps[i].name)
		}
		got := mustYAML("redo " + steps[i].name)
		if !bytes.Equal(got, snapshots[i+1]) {
			t.Fatalf("after redoing %s YAML differs!\ngot:\n%s\nwant:\n%s", steps[i].name, got, snapshots[i+1])
		}
	}
	if ed.History.CanRedo() {
		t.Fatal("CanRedo should be false at tip")
	}

	// Undoing one step then executing a new edit truncates redo history.
	if !ed.Undo() {
		t.Fatal("expected Undo to succeed")
	}
	if !ed.History.CanRedo() {
		t.Fatal("expected CanRedo true after Undo")
	}
	if err := ed.MoveObject("hero", 12, 34); err != nil {
		t.Fatalf("MoveObject: %v", err)
	}
	if ed.History.CanRedo() {
		t.Fatal("CanRedo should be false after branching with a new command")
	}
}

// #165: Enter Play mode, advance game.Loop mutating object positions, press Stop -> verify scene restored identically to pre-play state!
func TestPlayModeRunsLoopAndRestoresPrePlaySnapshotOnStop(t *testing.T) {
	ed := newTestEditor(t)
	// Make a pre-play edit so we also verify undo history survives Play/Stop intact.
	if err := ed.MoveObject("hero", 50, 45); err != nil {
		t.Fatalf("pre-play MoveObject: %v", err)
	}
	prePlayYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("pre-play SaveYAML: %v", err)
	}

	ed.OnPlayUpdate = func(scene *sceneformat.BuiltScene, dt time.Duration) {
		if hero := scene.Object("hero"); hero != nil {
			dx := float32(60.0 * dt.Seconds())
			hero.Doc.X += dx
			if hero.Sprite != nil {
				hero.Sprite.X = hero.Doc.X
			}
		}
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))

	// Press the Play button in the toolbar.
	app.Press("id:toolbar-play")
	if !ed.Playing() {
		t.Fatal("expected editor to be playing after pressing Play")
	}

	// Advance the game.Loop by 1 second (60 fixed steps -> hero moves +60 points).
	app.Advance(time.Second)

	if ed.PlaySteps() != 60 {
		t.Fatalf("PlaySteps = %d, want 60", ed.PlaySteps())
	}
	liveHero := ed.PlayScene().Object("hero")
	if liveHero == nil || math.Abs(float64(liveHero.Doc.X-110)) > 0.1 {
		t.Fatalf("live hero X = %v, want ~110 after 1s simulation", liveHero.Doc.X)
	}

	// Edits are rejected while playing, and SaveYAML still returns the clean pre-play snapshot.
	if err := ed.MoveObject("hero", 0, 0); err != editor.ErrPlaying {
		t.Fatalf("MoveObject during play err = %v, want ErrPlaying", err)
	}
	duringPlaySaved, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML during play: %v", err)
	}
	if !bytes.Equal(duringPlaySaved, prePlayYAML) {
		t.Fatalf("SaveYAML during play leaked runtime mutations")
	}

	// Press Stop button in the toolbar.
	app.Press("id:toolbar-play")
	if ed.Playing() {
		t.Fatal("expected editor to stop after pressing Stop")
	}

	// Verify scene restored identically byte-for-byte to pre-play YAML!
	postStopYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("post-stop SaveYAML: %v", err)
	}
	if !bytes.Equal(postStopYAML, prePlayYAML) {
		t.Fatalf("post-stop YAML differs from pre-play snapshot!\ngot:\n%s\nwant:\n%s", postStopYAML, prePlayYAML)
	}

	// Advancing further produces no more updates.
	stepsAtStop := ed.PlaySteps()
	app.Advance(time.Second)
	if ed.PlaySteps() != stepsAtStop {
		t.Fatalf("PlaySteps advanced after Stop: %d -> %d", stepsAtStop, ed.PlaySteps())
	}

	// Pre-play undo history still works cleanly on the restored SceneDoc!
	if !ed.Undo() {
		t.Fatal("expected Undo of pre-play MoveObject to succeed after Stop")
	}
	heroAfterUndo, _, _ := ed.FindObject("hero")
	if heroAfterUndo.X != 40 || heroAfterUndo.Y != 40 {
		t.Fatalf("hero after post-Stop Undo = (%v, %v), want (40, 40)", heroAfterUndo.X, heroAfterUndo.Y)
	}
}

// #166: Asset browser lists atlas frames; dragging/dropping an atlas frame into the scene creates a sprite object that draws in the viewport!
func TestAssetBrowserListsFramesAndDropDrawsInViewport(t *testing.T) {
	ed := newTestEditor(t)

	// Verify Assets() lists all frames across all atlases in deterministic sorted order.
	assets := ed.Assets()
	wantKeys := []string{"actors/hero", "actors/slime", "tiles/1", "tiles/2", "tiles/3"}
	gotKeys := make([]string, len(assets))
	for i, a := range assets {
		gotKeys[i] = a.Key()
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("Assets() = %v, want %v", gotKeys, wantKeys)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))

	// Verify Asset Browser renders entries in the UI tree when opened.
	openAssetBrowserInUI(app, ed)
	app.Find("id:asset-browser")
	app.Find("id:asset-actors-slime")

	// Select "actors/slime" in the Asset Browser (switches to Place tool), then click viewport at (104, 68).
	app.Press("id:asset-actors-slime")
	if ed.SelectedAsset == nil || ed.SelectedAsset.Key() != "actors/slime" {
		t.Fatalf("SelectedAsset = %+v, want actors/slime", ed.SelectedAsset)
	}
	if ed.Tool != editor.ToolPlace {
		t.Fatalf("Tool = %v, want ToolPlace", ed.Tool)
	}

	// The canvas fills its panel and shows the scene camera's view, so world (104, 68) is where WorldToCanvas puts it.
	px, py := ed.WorldToCanvas(104, 68)
	app.PressAt("id:editor-viewport", core.Point{X: core.Points(px), Y: core.Points(py)})

	// Verify a sprite object referencing actors/slime was created at (104, 68).
	droppedObj, _ := ed.SelectedObject()
	if droppedObj == nil || droppedObj.Sprite == nil {
		t.Fatalf("expected dropped sprite object to be selected, got %+v", droppedObj)
	}
	if droppedObj.Sprite.Atlas != "actors" || droppedObj.Sprite.Frame != "slime" ||
		math.Abs(float64(droppedObj.X-104)) > 1e-3 || math.Abs(float64(droppedObj.Y-68)) > 1e-3 {
		t.Fatalf("unexpected dropped object: %+v", droppedObj)
	}

	// Verify the dropped slime sprite actually draws in the viewport's recorded DrawOps!
	ops := app.Find("id:editor-viewport").Ops
	foundSlimeDraw := false
	for _, im := range gametest.ImageDraws(ops) {
		if im.Image.URI == "asset:actors.png" &&
			im.SX == 16 && im.SY == 0 &&
			math.Abs(float64(float32(im.Corners[0].X)-px)) < 1e-3 &&
			math.Abs(float64(float32(im.Corners[0].Y)-py)) < 1e-3 {
			foundSlimeDraw = true
			break
		}
	}
	if !foundSlimeDraw {
		t.Fatalf("expected dropped slime drawn at (%v,%v) in viewport ops: %+v", px, py, ops)
	}

	// Also verify direct DropAtlasFrame + Undo/Redo.
	undoInUI(app, ed)
	if obj, _, _ := ed.FindObject(droppedObj.ID); obj != nil {
		t.Fatalf("expected toolbar Undo to remove dropped object %q", droppedObj.ID)
	}
	redoInUI(app, ed)
	if obj, _, _ := ed.FindObject(droppedObj.ID); obj == nil {
		t.Fatalf("expected toolbar Redo to restore dropped object %q", droppedObj.ID)
	}
}

func TestToolbarToolsCanvasTilePaintAndSaveCallback(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetActiveLayer("floor")
	ed.SetActiveTileID(2)

	var savedBytes []byte
	ed.OnSave = func(data []byte) error {
		savedBytes = append([]byte(nil), data...)
		return nil
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))

	// Switch to Fill tool via command and click interior tile (1, 1), at world (24, 24). The tile palette that a tile tool shows narrows the viewport, so the canvas point is taken after the tool changes.
	runEditorCommand(t, app, ed, editor.CmdToolFillTile)
	if ed.Tool != editor.ToolFillTile {
		t.Fatalf("Tool = %v, want ToolFillTile", ed.Tool)
	}
	tx, ty := ed.WorldToCanvas(24, 24)
	tile := core.Point{X: core.Points(tx), Y: core.Points(ty)}
	app.PressAt("id:editor-viewport", tile)
	if got := ed.Doc.Layers[0].Tilemap.Data[1*4+1]; got != 2 {
		t.Fatalf("filled tile (1,1) = %d, want 2", got)
	}
	if got := ed.Doc.Layers[0].Tilemap.Data[2*4+2]; got != 2 {
		t.Fatalf("filled tile (2,2) = %d, want 2", got)
	}

	// Switch to Erase tool and click tile (1, 1).
	runEditorCommand(t, app, ed, editor.CmdToolEraseTile)
	app.PressAt("id:editor-viewport", tile)
	if got := ed.Doc.Layers[0].Tilemap.Data[1*4+1]; got != 0 {
		t.Fatalf("erased tile (1,1) = %d, want 0", got)
	}

	// Switch to Paint tool and click tile (1, 1) with tile 3.
	app.Post(func() { ed.SetActiveTileID(3) })
	app.Settle()
	runEditorCommand(t, app, ed, editor.CmdToolPaintTile)
	app.PressAt("id:editor-viewport", tile)
	if got := ed.Doc.Layers[0].Tilemap.Data[1*4+1]; got != 3 {
		t.Fatalf("painted tile (1,1) = %d, want 3", got)
	}

	// Save via File menu and verify OnSave received valid YAML matching the edits.
	saveInUI(app, ed)
	if len(savedBytes) == 0 {
		t.Fatal("expected OnSave to receive encoded YAML bytes")
	}
	parsed, err := sceneformat.Parse(savedBytes)
	if err != nil {
		t.Fatalf("Parse savedBytes: %v", err)
	}
	if got := parsed.Layers[0].Tilemap.Data[1*4+1]; got != 3 {
		t.Fatalf("saved YAML tile (1,1) = %d, want 3", got)
	}
}

func TestCameraTransformHitTestingAndFileRoundTrip(t *testing.T) {
	doc := testSceneDoc()
	doc.Camera = &sceneformat.CameraDoc{
		X:    80,
		Y:    60,
		Zoom: sceneformat.Float32(2),
	}
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}

	// Hero is at world (40, 40). With virtual 160x120, camera (80,60), zoom 2:
	// WorldToCanvas(40, 40) = (80 + (40-80)*2, 60 + (40-60)*2) = (0, 20).
	cx, cy := ed.WorldToCanvas(40, 40)
	if math.Abs(float64(cx-0)) > 1e-3 || math.Abs(float64(cy-20)) > 1e-3 {
		t.Fatalf("WorldToCanvas(40,40) = (%v, %v), want (0, 20)", cx, cy)
	}
	wx, wy := ed.CanvasToWorld(cx, cy)
	if math.Abs(float64(wx-40)) > 1e-3 || math.Abs(float64(wy-40)) > 1e-3 {
		t.Fatalf("CanvasToWorld(%v,%v) = (%v, %v), want (40, 40)", cx, cy, wx, wy)
	}
	if hit := ed.HitTest(cx, cy); hit != "hero" {
		t.Fatalf("HitTest under zoomed camera = %q, want hero", hit)
	}

	// SaveFile / OpenFile round-trip.
	path := t.TempDir() + "/level.scene.yaml"
	if err := ed.SaveFile(path); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	other, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := other.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if !reflect.DeepEqual(other.Doc, ed.Doc) {
		t.Fatalf("OpenFile doc mismatch:\ngot:  %#v\nwant: %#v", other.Doc, ed.Doc)
	}

	// A save writes through a temporary file renamed into place, leaving no
	// temporary behind in the target's directory.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("SaveFile left a temporary file %q", e.Name())
		}
	}

	// Opening YAML directly clears the save target: the document did not come
	// from the file that was open, so Save must not overwrite it.
	opened, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := opened.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if err := opened.OpenYAML([]byte("version: 1\nname: Other\n")); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}
	if opened.FilePath != "" {
		t.Fatalf("OpenYAML kept FilePath %q, want empty", opened.FilePath)
	}
}
