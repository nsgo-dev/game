package editor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/rooms"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// #884 Acceptance 1: Dragging the camera.bounds resize and move handles in the 2D viewport
// updates camera.bounds live, saves the new bounds (reloading byte-for-byte unchanged),
// cancels cleanly on Escape, and Undo/Redo restores them as a single step without stealing
// clicks on objects inside camera.bounds.
func TestViewportCameraBoundsDragSavesReloadsAndUndoes(t *testing.T) {
	ed := newFarEditor(t)
	origBounds := sceneformat.BoundsDoc{X: 1600, Y: 1000, Width: 640, Height: 360}
	if err := ed.SetCameraBounds(&origBounds); err != nil {
		t.Fatalf("SetCameraBounds: %v", err)
	}
	ed.History.Clear()
	ed.SelectObject("")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	// Zoom out slightly so all 8 handles of camera.bounds sit comfortably inside the canvas.
	app.Post(func() {
		ed.SetView2D(editor.View2D{X: 1920, Y: 1180, Zoom: 0.5})
	})
	app.Settle()

	// Verify all 8 resize handles are reported and drawn in viewport ops.
	seX, seY, ok := ed.CameraBoundsHandleCanvasPos(editor.RectHandleSE)
	if !ok {
		t.Fatal("expected CameraBoundsHandleCanvasPos(SE) ok")
	}
	foundHandleRect := false
	for _, op := range app.Find("id:editor-viewport").Ops {
		if op.Kind == core.DrawRect && mathAbs32(op.X+op.W/2-seX) < 1 && mathAbs32(op.Y+op.H/2-seY) < 1 {
			foundHandleRect = true
			break
		}
	}
	if !foundHandleRect {
		t.Fatalf("expected SE bounds handle FillRect around (%v, %v)", seX, seY)
	}

	// Drag SE corner handle (+40, +20) canvas pts at zoom 0.5 => (+80, +40) world units.
	app.MouseDownAt("id:editor-viewport-frame", core.Point{X: core.Points(seX), Y: core.Points(seY)}, core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(seX + 20), Y: core.Points(seY + 10)})
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(seX + 40), Y: core.Points(seY + 20)})
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: core.Points(seX + 40), Y: core.Points(seY + 20)}, core.MouseButtonLeft)

	wantAfterResize := sceneformat.BoundsDoc{X: 1600, Y: 1000, Width: 720, Height: 400}
	if ed.Doc.Camera.Bounds == nil || *ed.Doc.Camera.Bounds != wantAfterResize {
		t.Fatalf("bounds after SE drag = %+v, want %+v", ed.Doc.Camera.Bounds, wantAfterResize)
	}
	if ed.History.Len() != 1 || ed.History.UndoLabel() != "Resize camera bounds" {
		t.Fatalf("history = len %d label %q, want 1 / 'Resize camera bounds'", ed.History.Len(), ed.History.UndoLabel())
	}

	// Save and reload must preserve the new camera.bounds byte-for-byte.
	reloaded := reloadedScene(t, ed)
	if reloaded.Camera == nil || reloaded.Camera.Bounds == nil || *reloaded.Camera.Bounds != wantAfterResize {
		t.Fatalf("reloaded camera.bounds = %+v, want %+v", reloaded.Camera, wantAfterResize)
	}

	// Undo restores the original bounds; Redo restores the resized bounds.
	app.Press("id:toolbar-undo")
	if ed.Doc.Camera.Bounds == nil || *ed.Doc.Camera.Bounds != origBounds {
		t.Fatalf("bounds after Undo = %+v, want %+v", ed.Doc.Camera.Bounds, origBounds)
	}
	app.Press("id:toolbar-redo")
	if ed.Doc.Camera.Bounds == nil || *ed.Doc.Camera.Bounds != wantAfterResize {
		t.Fatalf("bounds after Redo = %+v, want %+v", ed.Doc.Camera.Bounds, wantAfterResize)
	}

	// Drag NW corner handle (-20, -10) canvas pts at zoom 0.5 => (-40, -20) world units.
	nwX, nwY, ok := ed.CameraBoundsHandleCanvasPos(editor.RectHandleNW)
	if !ok {
		t.Fatal("expected NW bounds handle")
	}
	app.MouseDownAt("id:editor-viewport-frame", core.Point{X: core.Points(nwX), Y: core.Points(nwY)}, core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(nwX - 20), Y: core.Points(nwY - 10)})
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: core.Points(nwX - 20), Y: core.Points(nwY - 10)}, core.MouseButtonLeft)

	wantAfterNW := sceneformat.BoundsDoc{X: 1560, Y: 980, Width: 760, Height: 420}
	if ed.Doc.Camera.Bounds == nil || *ed.Doc.Camera.Bounds != wantAfterNW {
		t.Fatalf("bounds after NW drag = %+v, want %+v", ed.Doc.Camera.Bounds, wantAfterNW)
	}

	// Drag the top border away from handles to move the entire bounds rectangle.
	topBorderX, topBorderY := ed.WorldToCanvas(1700, 980)
	app.MouseDownAt("id:editor-viewport-frame", core.Point{X: core.Points(topBorderX), Y: core.Points(topBorderY)}, core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(topBorderX + 30), Y: core.Points(topBorderY + 15)})
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: core.Points(topBorderX + 30), Y: core.Points(topBorderY + 15)}, core.MouseButtonLeft)

	wantAfterMove := sceneformat.BoundsDoc{X: 1620, Y: 1010, Width: 760, Height: 420}
	if ed.Doc.Camera.Bounds == nil || *ed.Doc.Camera.Bounds != wantAfterMove {
		t.Fatalf("bounds after border move = %+v, want %+v", ed.Doc.Camera.Bounds, wantAfterMove)
	}
	if ed.History.UndoLabel() != "Move camera bounds" {
		t.Fatalf("UndoLabel = %q, want 'Move camera bounds'", ed.History.UndoLabel())
	}

	// Pressing Escape mid-drag cancels the resize and restores pre-drag bounds without pushing history.
	histBeforeCancel := ed.History.Len()
	eX, eY, _ := ed.CameraBoundsHandleCanvasPos(editor.RectHandleE)
	app.MouseDownAt("id:editor-viewport-frame", core.Point{X: core.Points(eX), Y: core.Points(eY)}, core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(eX + 50), Y: core.Points(eY)})
	if ed.Doc.Camera.Bounds.Width == wantAfterMove.Width {
		t.Fatal("expected live bounds width update during E handle drag")
	}
	app.Key("escape")
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: core.Points(eX + 50), Y: core.Points(eY)}, core.MouseButtonLeft)
	if *ed.Doc.Camera.Bounds != wantAfterMove || ed.History.Len() != histBeforeCancel {
		t.Fatalf("after Escape cancel: bounds=%+v history=%d (want %+v / %d)", ed.Doc.Camera.Bounds, ed.History.Len(), wantAfterMove, histBeforeCancel)
	}

	// Place an object sitting inside camera.bounds and verify clicking it selects that object!
	var innerID string
	app.Post(func() {
		id, err := ed.PlaceObject("actors", sceneformat.ObjectDoc{ID: "inside_marker", X: 1800, Y: 1120})
		if err != nil {
			t.Errorf("PlaceObject: %v", err)
		}
		innerID = id
		ed.SelectObject("")
	})
	app.Settle()
	innerCX, innerCY := ed.WorldToCanvas(1800, 1120)
	app.PressAt("id:editor-viewport", core.Point{X: core.Points(innerCX), Y: core.Points(innerCY)})
	if ed.SelectedID != innerID {
		t.Fatalf("clicking object inside camera.bounds selected %q, want %q", ed.SelectedID, innerID)
	}
}

func TestCameraBoundsDragFreezesCameraFollowingViewAndClampsMinSize(t *testing.T) {
	ed := newFarEditor(t)
	if err := ed.SetCameraBounds(&sceneformat.BoundsDoc{X: 1800, Y: 1050, Width: 320, Height: 240}); err != nil {
		t.Fatalf("SetCameraBounds: %v", err)
	}
	ed.History.Clear()
	ed.SelectObject("")
	ed.SetCanvasSize(800, 600)
	if !ed.View2DFollowsCamera() {
		t.Fatal("expected View2DFollowsCamera true")
	}

	// Drag E handle far left past W edge: must clamp width to >= 1 and remain valid for sceneformat.Validate.
	ex, ey, ok := ed.CameraBoundsHandleCanvasPos(editor.RectHandleE)
	if !ok {
		t.Fatal("expected E handle")
	}
	if h := ed.BeginCameraBoundsDrag(ex, ey); h != editor.RectHandleE {
		t.Fatalf("BeginCameraBoundsDrag = %v, want E", h)
	}
	ed.UpdateCameraBoundsDrag(ex-500, ey)
	if !ed.EndCameraBoundsDrag() {
		t.Fatal("EndCameraBoundsDrag returned false")
	}
	if ed.Doc.Camera.Bounds.Width != 1 {
		t.Fatalf("clamped bounds width = %v, want 1", ed.Doc.Camera.Bounds.Width)
	}
	if !ed.View2DFollowsCamera() {
		t.Fatal("expected View2DFollowsCamera to remain true after bounds drag")
	}
	_ = reloadedScene(t, ed)
}

// #884 Acceptance 2 (round-trip): Opening an existing canonical *.rooms.yaml beside a scene
// and saving it produces byte-for-byte identical YAML and reloads identically with rooms.Parse.
func TestRoomsOpenRoundTripUnchangedAndProjectBrowser(t *testing.T) {
	rawCastle, err := os.ReadFile(filepath.Join("..", "rooms", "testdata", "castle.rooms.yaml"))
	if err != nil {
		t.Fatalf("read castle.rooms.yaml: %v", err)
	}

	ed := newTestEditor(t)
	if err := ed.OpenRoomsYAML("castle.rooms.yaml", rawCastle); err != nil {
		t.Fatalf("OpenRoomsYAML: %v", err)
	}
	if ed.RoomsDirty() {
		t.Fatal("freshly opened rooms map should not be dirty")
	}
	got, err := ed.SaveRoomsYAML()
	if err != nil {
		t.Fatalf("SaveRoomsYAML: %v", err)
	}
	if !bytes.Equal(got, rawCastle) {
		t.Fatalf("SaveRoomsYAML changed canonical castle.rooms.yaml:\ngot:\n%s\nwant:\n%s", got, rawCastle)
	}
	parsedGot, err := rooms.Parse(got)
	if err != nil {
		t.Fatalf("rooms.Parse(SaveRoomsYAML): %v", err)
	}
	parsedOrig, err := rooms.Parse(rawCastle)
	if err != nil {
		t.Fatalf("rooms.Parse(orig): %v", err)
	}
	if !reflect.DeepEqual(parsedGot, parsedOrig) {
		t.Fatalf("parsed rooms differ:\ngot:  %+v\nwant: %+v", parsedGot, parsedOrig)
	}

	// Open *.rooms.yaml from a project's file browser and save via Editor.Save().
	dir := writeEditorProject(t)
	roomsFile := filepath.Join(dir, "scenes", "castle.rooms.yaml")
	writeRoomTestFile(t, roomsFile, string(rawCastle))

	projEd, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := projEd.OpenProject(filepath.Join(dir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, projEd)
	}, nsgotest.Size(1280, 800))

	app.DoublePress("id:files-row-scenes/castle.rooms.yaml")
	app.Settle()
	if projEd.Rooms() == nil || len(projEd.Rooms().Rooms) != 2 {
		t.Fatalf("expected castle.rooms.yaml opened from file browser, got %+v", projEd.Rooms())
	}

	// Edit a room, verify HasUnsavedChanges, save via Save(), verify on-disk file parses cleanly and clears dirty.
	app.Post(func() {
		if err := projEd.SetRoomName("entrance", "Castle Gate"); err != nil {
			t.Errorf("SetRoomName: %v", err)
		}
	})
	app.Settle()
	if !projEd.RoomsDirty() || !projEd.HasUnsavedChanges() {
		t.Fatal("expected RoomsDirty and HasUnsavedChanges true after room edit")
	}
	app.Post(func() {
		if err := projEd.Save(); err != nil {
			t.Errorf("Save: %v", err)
		}
	})
	app.Settle()
	if projEd.RoomsDirty() || projEd.HasUnsavedChanges() {
		t.Fatal("expected clean state after Save")
	}
	onDisk, err := os.ReadFile(roomsFile)
	if err != nil {
		t.Fatalf("ReadFile roomsFile: %v", err)
	}
	reloadedMap, err := rooms.Parse(onDisk)
	if err != nil {
		t.Fatalf("rooms.Parse(onDisk): %v", err)
	}
	if r := reloadedMap.Room("entrance"); r == nil || r.Name != "Castle Gate" {
		t.Fatalf("on-disk room entrance = %+v, want Name='Castle Gate'", r)
	}
	reEncoded, err := rooms.Encode(reloadedMap)
	if err != nil || !bytes.Equal(reEncoded, onDisk) {
		t.Fatalf("on-disk rooms file does not round-trip unchanged: err=%v\ngot:\n%s\nwant:\n%s", err, reEncoded, onDisk)
	}
}

// #884 Acceptance 2 (viewport + tile-grid snapping): Create, select, move, resize with
// handles, cancel on Escape, and delete rooms in the 2D viewport with edges snapped to the
// scene's 16x16 tile grid; verify rooms.Parse loads the output and round-trips unchanged.
func TestRoomsViewportCreateMoveResizeSnapDeleteAndUndo(t *testing.T) {
	doc := &sceneformat.SceneDoc{
		Version:       sceneformat.CurrentVersion,
		Name:          "Grid World",
		VirtualWidth:  640,
		VirtualHeight: 360,
		Layers: []sceneformat.LayerDoc{
			{
				Name:  "tiles",
				Space: sceneformat.SpaceWorld,
				Tilemap: &sceneformat.TilemapDoc{
					Tileset:    "tiles",
					Width:      40,
					Height:     24,
					TileWidth:  16,
					TileHeight: 16,
					Data:       make([]int, 40*24),
				},
			},
		},
	}
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.NewRoomMap(""); err != nil {
		t.Fatalf("NewRoomMap: %v", err)
	}
	if gw, gh, ok := ed.RoomGridSize(); !ok || gw != 16 || gh != 16 {
		t.Fatalf("RoomGridSize = (%v, %v, %v), want (16, 16, true)", gw, gh, ok)
	}
	// Starter room snapped to 16x16 grid: 640x360 -> 640x368 (round(360/16)=23 -> 368).
	starter := ed.Rooms().Room("room_1")
	if starter == nil || starter.Width != 640 || starter.Height != 368 {
		t.Fatalf("starter room = %+v, want 640x368", starter)
	}
	// Resize starter room cleanly to (0, 0, 160, 112) and clear history to start fresh.
	if err := ed.SetRoomRect("room_1", 0, 0, 160, 112); err != nil {
		t.Fatalf("SetRoomRect: %v", err)
	}
	ed.History.Clear()

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	app.Post(func() {
		ed.SetView2D(editor.View2D{X: 240, Y: 140, Zoom: 1})
	})
	app.Settle()

	// Verify rooms draw their label in viewport ops.
	foundRoomText := false
	for _, op := range app.Find("id:editor-viewport").Ops {
		if op.Kind == core.DrawFillText && strings.Contains(op.Text, "room_1") {
			foundRoomText = true
			break
		}
	}
	if !foundRoomText {
		t.Fatal("expected room_1 label drawn in 2D viewport ops")
	}

	// Draw a second room by dragging in Draw Room mode with non-grid-aligned coordinates:
	// world (179, 19) -> (349, 131) should snap to (176, 16) -> (352, 128) => width 176, height 112!
	app.Post(func() { ed.SetRoomCreateMode(true) })
	app.Settle()
	x0, y0 := ed.WorldToCanvas(179, 19)
	x1, y1 := ed.WorldToCanvas(349, 131)
	app.MouseDownAt("id:editor-viewport-frame", core.Point{X: core.Points(x0), Y: core.Points(y0)}, core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(x1), Y: core.Points(y1)})
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: core.Points(x1), Y: core.Points(y1)}, core.MouseButtonLeft)

	if ed.RoomCreateMode() {
		t.Fatal("expected RoomCreateMode to turn off after drawing a room")
	}
	r2 := ed.Rooms().Room("room_2")
	if r2 == nil || r2.X != 176 || r2.Y != 16 || r2.Width != 176 || r2.Height != 112 {
		t.Fatalf("drawn room_2 = %+v, want (176, 16, 176, 112)", r2)
	}
	if ed.SelectedRoomID() != "room_2" {
		t.Fatalf("SelectedRoomID = %q, want room_2", ed.SelectedRoomID())
	}

	// Resize room_2 via its SE corner handle by (+23, +19) => snaps to (+16, +16) => 192x128.
	seX, seY, ok := ed.RoomHandleCanvasPos("room_2", editor.RectHandleSE)
	if !ok {
		t.Fatal("expected SE handle on room_2")
	}
	app.MouseDownAt("id:editor-viewport-frame", core.Point{X: core.Points(seX), Y: core.Points(seY)}, core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(seX + 23), Y: core.Points(seY + 19)})
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: core.Points(seX + 23), Y: core.Points(seY + 19)}, core.MouseButtonLeft)

	r2 = ed.Rooms().Room("room_2")
	if r2.X != 176 || r2.Y != 16 || r2.Width != 192 || r2.Height != 128 {
		t.Fatalf("resized room_2 = %+v, want (176, 16, 192, 128)", r2)
	}

	// Move room_2 by dragging its interior by (+25, +37) => snaps to (+32, +32) => (208, 48).
	cx, cy := ed.WorldToCanvas(250, 80)
	app.MouseDownAt("id:editor-viewport-frame", core.Point{X: core.Points(cx), Y: core.Points(cy)}, core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(cx + 25), Y: core.Points(cy + 37)})
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: core.Points(cx + 25), Y: core.Points(cy + 37)}, core.MouseButtonLeft)

	r2 = ed.Rooms().Room("room_2")
	if r2.X != 208 || r2.Y != 48 || r2.Width != 192 || r2.Height != 128 {
		t.Fatalf("moved room_2 = %+v, want (208, 48, 192, 128)", r2)
	}

	// Escape during a room handle drag cancels and restores pre-drag room with no history step.
	histBeforeEsc := ed.History.Len()
	wX, wY, _ := ed.RoomHandleCanvasPos("room_2", editor.RectHandleW)
	app.MouseDownAt("id:editor-viewport-frame", core.Point{X: core.Points(wX), Y: core.Points(wY)}, core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(wX - 48), Y: core.Points(wY)})
	app.Key("escape")
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: core.Points(wX - 48), Y: core.Points(wY)}, core.MouseButtonLeft)
	r2 = ed.Rooms().Room("room_2")
	if r2.X != 208 || r2.Width != 192 || ed.History.Len() != histBeforeEsc {
		t.Fatalf("after Escape cancel: room_2=%+v hist=%d (want X=208 W=192 hist=%d)", r2, ed.History.Len(), histBeforeEsc)
	}

	// Save YAML, parse with rooms.Parse, and assert byte-for-byte deterministic round-trip.
	savedYAML, err := ed.SaveRoomsYAML()
	if err != nil {
		t.Fatalf("SaveRoomsYAML: %v", err)
	}
	parsed, err := rooms.Parse(savedYAML)
	if err != nil {
		t.Fatalf("rooms.Parse: %v\n%s", err, savedYAML)
	}
	reencoded, err := rooms.Encode(parsed)
	if err != nil || !bytes.Equal(reencoded, savedYAML) {
		t.Fatalf("round-trip mismatch: err=%v\ngot:\n%s\nwant:\n%s", err, reencoded, savedYAML)
	}
	if len(parsed.Rooms) != 2 || parsed.Room("room_2").X != 208 || parsed.Room("room_2").Width != 192 {
		t.Fatalf("parsed rooms = %+v", parsed.Rooms)
	}

	// Delete room_2 via the inspector Delete button, verify Undo restores it!
	app.Press("id:inspector-room-delete")
	if ed.Rooms().Room("room_2") != nil || len(ed.Rooms().Rooms) != 1 {
		t.Fatalf("expected room_2 deleted, got %+v", ed.Rooms().Rooms)
	}
	app.Press("id:toolbar-undo")
	if r := ed.Rooms().Room("room_2"); r == nil || r.X != 208 || r.Width != 192 {
		t.Fatalf("expected Undo to restore room_2, got %+v", r)
	}
	if ed.SelectedRoomID() != "room_2" {
		t.Fatalf("expected Undo delete to restore SelectedRoomID=room_2, got %q", ed.SelectedRoomID())
	}

	// Delete via editor delete command (Delete/Backspace shortcut).
	app.Post(func() {
		if err := ed.ExecuteCommand(editor.CmdDelete); err != nil {
			t.Errorf("CmdDelete: %v", err)
		}
	})
	app.Settle()
	if ed.Rooms().Room("room_2") != nil {
		t.Fatal("expected CmdDelete to delete selected room_2")
	}

	// Disabling grid snap allows free (non-multiple-of-16) room drags.
	app.Post(func() {
		ed.Undo()
		ed.SetRoomSnapToGrid(false)
	})
	app.Settle()
	seX2, seY2, _ := ed.RoomHandleCanvasPos("room_2", editor.RectHandleSE)
	app.MouseDownAt("id:editor-viewport-frame", core.Point{X: core.Points(seX2), Y: core.Points(seY2)}, core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(seX2 + 7), Y: core.Points(seY2 + 5)})
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: core.Points(seX2 + 7), Y: core.Points(seY2 + 5)}, core.MouseButtonLeft)
	r2 = ed.Rooms().Room("room_2")
	if r2.Width != 199 || r2.Height != 133 {
		t.Fatalf("unsnapped room_2 = %+v, want 199x133", r2)
	}
}

// #884: Room inspector edits id, name, bounds and transition (inherit/cut/slide/fade with
// duration, easing, fade color), refuses invalid values in Status without mutating History,
// and undoes back to exact original YAML bytes.
func TestInspectorRoomEditsIDNameTransitionValidationAndUndo(t *testing.T) {
	rawCastle, err := os.ReadFile(filepath.Join("..", "rooms", "testdata", "castle.rooms.yaml"))
	if err != nil {
		t.Fatalf("read castle.rooms.yaml: %v", err)
	}

	ed := newTestEditor(t)
	ed.SelectObject("")
	if err := ed.OpenRoomsYAML("castle.rooms.yaml", rawCastle); err != nil {
		t.Fatalf("OpenRoomsYAML: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1200))

	// Scene Settings shows the ROOMS section with clickable room rows.
	if !app.Exists("id:inspector-rooms-section") || !app.Exists("id:inspector-rooms-item-entrance") {
		t.Fatal("expected inspector-rooms-section and entrance row in Scene Settings")
	}
	app.Press("id:inspector-rooms-item-entrance")
	if !app.Exists("id:inspector-room") || ed.SelectedRoomID() != "entrance" {
		t.Fatalf("expected room inspector for entrance, SelectedRoomID=%q", ed.SelectedRoomID())
	}

	// Edit ID, Name, and Bounds.
	app.Clear("id:inspector-room-id")
	app.Type("id:inspector-room-id", "foyer")
	app.Submit("id:inspector-room-id")
	if ed.SelectedRoomID() != "foyer" || ed.Rooms().Room("foyer") == nil {
		t.Fatalf("room rename failed: SelectedRoomID=%q rooms=%+v", ed.SelectedRoomID(), ed.Rooms().Rooms)
	}

	app.Clear("id:inspector-room-name")
	app.Type("id:inspector-room-name", "Grand Foyer")
	if ed.Rooms().Room("foyer").Name != "Grand Foyer" {
		t.Fatalf("room Name = %q, want Grand Foyer", ed.Rooms().Room("foyer").Name)
	}

	// Edit Transition: Fade with duration + color, then Slide, then Cut, then Inherit.
	app.Press("id:inspector-room-transition-fade")
	if tr := ed.Rooms().Room("foyer").Transition; tr == nil || tr.Kind != rooms.KindFade {
		t.Fatalf("transition after Fade = %+v", tr)
	}
	app.Clear("id:inspector-room-transition-duration")
	app.Type("id:inspector-room-transition-duration", "450ms")
	app.Submit("id:inspector-room-transition-duration")
	app.Clear("id:inspector-room-transition-color")
	app.Type("id:inspector-room-transition-color", "#112233")
	app.Submit("id:inspector-room-transition-color")
	if tr := ed.Rooms().Room("foyer").Transition; tr == nil || tr.Duration != rooms.Duration(450*time.Millisecond) || tr.Color != "#112233" {
		t.Fatalf("fade transition = %+v", tr)
	}

	// Refused invalid edits do not mutate the room or push to History.
	beforeHist := ed.History.Len()
	beforeYAML, err := ed.SaveRoomsYAML()
	if err != nil {
		t.Fatalf("SaveRoomsYAML: %v", err)
	}

	// Duplicate ID ("hall")
	app.Clear("id:inspector-room-id")
	app.Type("id:inspector-room-id", "hall")
	app.Submit("id:inspector-room-id")
	if !strings.Contains(ed.Status, "duplicate") {
		t.Fatalf("expected duplicate ID error in Status, got %q", ed.Status)
	}
	// Bare duration without unit
	app.Clear("id:inspector-room-transition-duration")
	app.Type("id:inspector-room-transition-duration", "400")
	app.Submit("id:inspector-room-transition-duration")
	if !strings.Contains(ed.Status, "duration") {
		t.Fatalf("expected invalid duration error in Status, got %q", ed.Status)
	}
	// Invalid fade color
	app.Clear("id:inspector-room-transition-color")
	app.Type("id:inspector-room-transition-color", "not-a-color")
	app.Submit("id:inspector-room-transition-color")
	if !strings.Contains(ed.Status, "color") {
		t.Fatalf("expected invalid color error in Status, got %q", ed.Status)
	}
	if ed.History.Len() != beforeHist {
		t.Fatalf("refused edits pushed to History: %d vs %d", ed.History.Len(), beforeHist)
	}
	afterRefusedYAML, _ := ed.SaveRoomsYAML()
	if !bytes.Equal(afterRefusedYAML, beforeYAML) {
		t.Fatalf("refused edits mutated room map:\ngot:\n%s\nwant:\n%s", afterRefusedYAML, beforeYAML)
	}

	// Switch Fade -> Slide (clears Color automatically) -> Cut (clears Duration/Easing) -> Inherit (nil).
	app.Press("id:inspector-room-transition-slide")
	if tr := ed.Rooms().Room("foyer").Transition; tr == nil || tr.Kind != rooms.KindSlide || tr.Color != "" || tr.Duration != rooms.Duration(450*time.Millisecond) {
		t.Fatalf("slide transition = %+v", tr)
	}
	app.Press("id:inspector-room-transition-cut")
	if tr := ed.Rooms().Room("foyer").Transition; tr == nil || tr.Kind != rooms.KindCut || tr.Duration != 0 || tr.Easing != "" {
		t.Fatalf("cut transition = %+v", tr)
	}
	app.Press("id:inspector-room-transition-inherit")
	if tr := ed.Rooms().Room("foyer").Transition; tr != nil {
		t.Fatalf("inherit transition = %+v, want nil", tr)
	}

	// Undo every step back to byte-for-byte original castle.rooms.yaml!
	steps := ed.History.Index()
	for range steps {
		app.Post(func() { ed.Undo() })
		app.Settle()
	}
	undoneYAML, err := ed.SaveRoomsYAML()
	if err != nil {
		t.Fatalf("SaveRoomsYAML after undo: %v", err)
	}
	if !bytes.Equal(undoneYAML, rawCastle) {
		t.Fatalf("after undoing all room edits, YAML differs:\ngot:\n%s\nwant:\n%s", undoneYAML, rawCastle)
	}

	// Pressing Back returns to Scene Settings inspector.
	app.Press("id:inspector-room-back")
	if !app.Exists("id:inspector-scene") || ed.SelectedRoomID() != "" {
		t.Fatalf("expected Scene Settings after pressing Back, SelectedRoomID=%q", ed.SelectedRoomID())
	}

	// Play mode refuses all room edits with ErrPlaying.
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	for name, err := range map[string]error{
		"AddRoom":              func() error { _, e := ed.AddRoom(rooms.Room{ID: "x", Width: 10, Height: 10}); return e }(),
		"DeleteRoom":           ed.DeleteRoom("entrance"),
		"SetRoomRect":          ed.SetRoomRect("entrance", 10, 10, 100, 100),
		"SetRoomID":            ed.SetRoomID("entrance", "start"),
		"SetRoomName":          ed.SetRoomName("entrance", "Start"),
		"SetRoomTransition":    ed.SetRoomTransition("entrance", &rooms.Transition{Kind: rooms.KindCut}),
		"SetRoomMapTransition": ed.SetRoomMapTransition(&rooms.Transition{Kind: rooms.KindCut}),
		"SaveRooms":            ed.SaveRooms(),
	} {
		if err != editor.ErrPlaying {
			t.Errorf("%s during Play = %v, want ErrPlaying", name, err)
		}
	}
	app.Post(func() { _ = ed.Stop() })
	app.Settle()
}

func mathAbs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func TestRoomMapUndoDoesNotCorruptSwitchedMap(t *testing.T) {
	ed := newFarEditor(t)
	if err := ed.NewRoomMap(""); err != nil {
		t.Fatalf("NewRoomMap A: %v", err)
	}
	if _, err := ed.AddRoom(rooms.Room{ID: "alpha_extra", X: 320, Y: 0, Width: 320, Height: 180}); err != nil {
		t.Fatalf("AddRoom on map A: %v", err)
	}
	// Switch to a completely different room map B.
	mapBYAML := []byte("version: 1\nrooms:\n  - id: beta_only\n    x: 0\n    y: 0\n    width: 640\n    height: 360\n")
	if err := ed.OpenRoomsYAML("beta.rooms.yaml", mapBYAML); err != nil {
		t.Fatalf("OpenRoomsYAML B: %v", err)
	}

	// Undoing/redoing the history entry recorded on map A must not corrupt map B.
	ed.Undo()
	if got := ed.Rooms(); got == nil || len(got.Rooms) != 1 || got.Rooms[0].ID != "beta_only" {
		t.Fatalf("Undo after switching room maps corrupted map B: %+v", got)
	}
	ed.Redo()
	if got := ed.Rooms(); got == nil || len(got.Rooms) != 1 || got.Rooms[0].ID != "beta_only" {
		t.Fatalf("Redo after switching room maps corrupted map B: %+v", got)
	}
}

func TestNewRoomMapFromInspectorAssignsDefaultPathAndSaveErrorsOnUntitled(t *testing.T) {
	dir := writeEditorProject(t)

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(filepath.Join(dir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	ed.SelectObject("")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	// Clicking "New Room Map" inside a project assigns a default path beside the scene so Save works.
	app.Press("id:inspector-rooms-new")
	wantPath := filepath.Join(dir, "scenes", "room.rooms.yaml")
	if ed.RoomsPath() != wantPath {
		t.Fatalf("RoomsPath after New Room Map = %q, want %q", ed.RoomsPath(), wantPath)
	}
	if !ed.RoomsDirty() || !ed.HasUnsavedChanges() {
		t.Fatal("new room map should be dirty before Save")
	}
	app.Post(func() {
		if err := ed.Save(); err != nil {
			t.Errorf("Save() for inspector-created room map: %v", err)
		}
	})
	app.Settle()
	if ed.RoomsDirty() {
		t.Fatal("expected RoomsDirty() == false after Save")
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected saved room map at %s: %v", wantPath, err)
	}

	// An untitled room map (NewRoomMap("")) with a dirty scene must still report ErrNoRoomsFile on Save().
	app.Post(func() {
		if err := ed.NewRoomMap(""); err != nil {
			t.Errorf("NewRoomMap(\"\"): %v", err)
		}
		if err := ed.SetCameraPosition(10, 20); err != nil {
			t.Errorf("SetCameraPosition: %v", err)
		}
	})
	app.Settle()
	var saveErr error
	app.Post(func() { saveErr = ed.Save() })
	app.Settle()
	if !errors.Is(saveErr, editor.ErrNoRoomsFile) {
		t.Fatalf("Save() with dirty scene + untitled dirty room map = %v, want ErrNoRoomsFile", saveErr)
	}
	if !ed.RoomsDirty() {
		t.Fatal("untitled room map should remain dirty after ErrNoRoomsFile")
	}
}

func TestRoomInspectorDraftResetsAcrossRoomsWithIdenticalTransition(t *testing.T) {
	ed := newFarEditor(t)
	raw := []byte("version: 1\nrooms:\n  - id: room_a\n    x: 0\n    y: 0\n    width: 320\n    height: 180\n    transition:\n      kind: fade\n      duration: 400ms\n      color: \"#000000\"\n  - id: room_b\n    x: 320\n    y: 0\n    width: 320\n    height: 180\n    transition:\n      kind: fade\n      duration: 400ms\n      color: \"#000000\"\n")
	if err := ed.OpenRoomsYAML("twin.rooms.yaml", raw); err != nil {
		t.Fatalf("OpenRoomsYAML: %v", err)
	}
	ed.SelectRoom("room_a")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	// Type uncommitted drafts into room_a without submitting.
	app.Clear("id:inspector-room-transition-duration")
	app.Type("id:inspector-room-transition-duration", "999ms")
	app.Clear("id:inspector-room-transition-color")
	app.Type("id:inspector-room-transition-color", "#abcdef")

	// Switch to room_b, which has the exact same committed duration/color as room_a.
	app.Post(func() { ed.SelectRoom("room_b") })
	app.Settle()

	if got := app.Find("id:inspector-room-transition-duration").Text; got != "400ms" {
		t.Fatalf("duration draft leaked from room_a to room_b: got %q, want 400ms", got)
	}
	if got := app.Find("id:inspector-room-transition-color").Text; got != "#000000" {
		t.Fatalf("color draft leaked from room_a to room_b: got %q, want #000000", got)
	}
	if tr := ed.Rooms().Room("room_b").Transition; tr.Duration != rooms.Duration(400*time.Millisecond) || tr.Color != "#000000" {
		t.Fatalf("room_b transition mutated unexpectedly: %+v", tr)
	}
}

func TestOpenAndSaveRoomsRejectsProjectSymlinkEscape(t *testing.T) {
	projDir := writeEditorProject(t)
	outsideDir := t.TempDir()

	outsideFile := filepath.Join(outsideDir, "secret.rooms.yaml")
	outsideContent := "version: 1\nrooms:\n  - id: secret\n    x: 0\n    y: 0\n    width: 320\n    height: 180\n"
	writeRoomTestFile(t, outsideFile, outsideContent)

	linkFile := filepath.Join(projDir, "escape.rooms.yaml")
	if err := os.Symlink(outsideFile, linkFile); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	linkDir := filepath.Join(projDir, "escape_dir")
	if err := os.Symlink(outsideDir, linkDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	ed, err := editor.New(nil, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := ed.OpenProject(filepath.Join(projDir, "game.yaml")); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if err := ed.OpenRoomsProjectFile("escape.rooms.yaml"); err == nil {
		t.Fatal("expected OpenRoomsProjectFile to refuse symlink escaping project root")
	}
	if err := ed.OpenRoomsProjectFile("escape_dir/secret.rooms.yaml"); err == nil {
		t.Fatal("expected OpenRoomsProjectFile to refuse symlinked directory escaping project root")
	}

	if err := ed.NewRoomMap(""); err != nil {
		t.Fatalf("NewRoomMap: %v", err)
	}
	if err := ed.SaveRoomsFile(linkFile); err == nil {
		t.Fatal("expected SaveRoomsFile to refuse writing through symlink escaping project root")
	}
	if err := ed.SaveRoomsFile(filepath.Join(linkDir, "new.rooms.yaml")); err == nil {
		t.Fatal("expected SaveRoomsFile to refuse writing through symlinked directory escaping project root")
	}
	gotOutside, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("ReadFile outsideFile: %v", err)
	}
	if string(gotOutside) != outsideContent {
		t.Fatal("outside symlink target was modified")
	}
}

func TestRoomsSceneSectionDefaultTransitionAndZeroDragCreateClick(t *testing.T) {
	ed := newFarEditor(t)
	if err := ed.NewRoomMap(""); err != nil {
		t.Fatalf("NewRoomMap: %v", err)
	}
	ed.SelectObject("")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1280, 800))

	// Edit map-level default transition in Scene Settings -> ROOMS section.
	app.Press("id:inspector-rooms-transition-fade")
	if tr := ed.Rooms().Transition; tr == nil || tr.Kind != rooms.KindFade {
		t.Fatalf("map default transition after Fade = %+v", tr)
	}
	app.Clear("id:inspector-rooms-transition-duration")
	app.Type("id:inspector-rooms-transition-duration", "300ms")
	app.Submit("id:inspector-rooms-transition-duration")
	app.Clear("id:inspector-rooms-transition-color")
	app.Type("id:inspector-rooms-transition-color", "#223344")
	app.Submit("id:inspector-rooms-transition-color")
	if tr := ed.Rooms().Transition; tr == nil || tr.Duration != rooms.Duration(300*time.Millisecond) || tr.Color != "#223344" {
		t.Fatalf("map default fade transition = %+v", tr)
	}
	app.Press("id:inspector-rooms-transition-none")
	if tr := ed.Rooms().Transition; tr != nil {
		t.Fatalf("map default transition after None = %+v, want nil", tr)
	}

	// Zero-drag click in Draw Room mode must not fall through and select underlying scene objects.
	ed.SetRoomCreateMode(true)
	ed.SelectObject("")
	var heroX, heroY float32
	foundHero := false
	for _, layer := range ed.Doc.Layers {
		if len(layer.Objects) > 0 {
			heroX, heroY = layer.Objects[0].X, layer.Objects[0].Y
			foundHero = true
			break
		}
	}
	if !foundHero {
		t.Fatal("expected at least one scene object in newFarEditor")
	}
	hx, hy := ed.WorldToCanvas(heroX, heroY)
	// First verify a normal click outside Draw Room mode WOULD select the object, then verify Draw Room mode blocks it.
	app.Post(func() {
		ed.HandleCanvasPress(hx, hy)
	})
	app.Settle()
	if ed.SelectedID != "" {
		t.Fatalf("zero-drag click in Draw Room mode selected object %q, want empty", ed.SelectedID)
	}
}

func writeRoomTestFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", p, err)
	}
}
