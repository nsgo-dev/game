package editor_test

import (
	"bytes"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func reloadedScene(t *testing.T, ed *editor.Editor) *sceneformat.SceneDoc {
	t.Helper()
	data, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	again, err := editor.New(nil, ed.Resolver)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	if err := again.OpenYAML(data); err != nil {
		t.Fatalf("OpenYAML: %v\n%s", err, data)
	}
	resaved, err := again.SaveYAML()
	if err != nil {
		t.Fatalf("resave SaveYAML: %v", err)
	}
	if !bytes.Equal(resaved, data) {
		t.Fatalf("reloaded scene saves differently:\n%s\nwant:\n%s", resaved, data)
	}
	return again.Doc
}

// #871: With no object selected, the inspector edits scene name, virtual size, background
// color and camera (x, y, zoom, rotation, bounds), each step updates the viewport live,
// saves and reloads byte-for-byte unchanged, and Undo restores each step cleanly.
func TestInspectorSceneSettingsEditsSaveReloadAndUndo(t *testing.T) {
	ed := newTestEditor(t)
	ed.SelectObject("")
	initialYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("initial SaveYAML: %v", err)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1200))

	if !app.Exists("id:inspector-scene") {
		t.Fatal("expected inspector-scene to be shown when no object is selected")
	}
	if app.Exists("id:inspector-empty") {
		t.Fatal("inspector-empty should not show while a scene is open")
	}

	// Edit scene name.
	app.Clear("id:inspector-scene-name")
	app.Type("id:inspector-scene-name", "Cavern")
	if ed.Doc.Name != "Cavern" {
		t.Fatalf("scene Name = %q, want Cavern", ed.Doc.Name)
	}

	// Edit virtual width and height.
	app.Clear("id:inspector-scene-width")
	app.Type("id:inspector-scene-width", "640")
	app.Submit("id:inspector-scene-width")
	app.Clear("id:inspector-scene-height")
	app.Type("id:inspector-scene-height", "360")
	app.Submit("id:inspector-scene-height")
	if ed.Doc.VirtualWidth != 640 || ed.Doc.VirtualHeight != 360 {
		t.Fatalf("virtual size = (%v, %v), want (640, 360)", ed.Doc.VirtualWidth, ed.Doc.VirtualHeight)
	}
	if vw, vh := ed.ViewportSize(); vw != 640 || vh != 360 {
		t.Fatalf("ViewportSize = (%v, %v), want (640, 360)", vw, vh)
	}

	// Edit background color via hex input.
	app.Clear("id:inspector-scene-bg")
	app.Type("id:inspector-scene-bg", "#223344")
	app.Submit("id:inspector-scene-bg")
	if ed.Doc.BackgroundColor != "#223344" {
		t.Fatalf("BackgroundColor = %q, want #223344", ed.Doc.BackgroundColor)
	}
	wantBG, err := core.ParseColor("#223344")
	if err != nil {
		t.Fatalf("ParseColor: %v", err)
	}
	if ed.Built == nil || !ed.Built.HasBackground || ed.Built.BackgroundColor != wantBG {
		t.Fatalf("Built background = (%v, %x), want (true, %x)", ed.Built.HasBackground, ed.Built.BackgroundColor, wantBG)
	}

	// Edit background color via NSGo's ColorWell / ColorPicker popup.
	app.Press("id:inspector-scene-bg-picker")
	if !app.Exists("id:inspector-scene-bg-color-picker") {
		t.Fatal("expected ColorPicker popup to open when pressing ColorWell")
	}
	app.Clear("Hex color")
	app.Type("Hex color", "334455")
	app.Submit("Hex color")
	if ed.Doc.BackgroundColor != "#334455" {
		t.Fatalf("BackgroundColor after ColorPicker = %q, want #334455", ed.Doc.BackgroundColor)
	}
	app.Key("escape")

	// Edit camera X, Y, Zoom, Rotation.
	app.Clear("id:inspector-camera-x")
	app.Type("id:inspector-camera-x", "400")
	app.Submit("id:inspector-camera-x")
	app.Clear("id:inspector-camera-y")
	app.Type("id:inspector-camera-y", "220")
	app.Submit("id:inspector-camera-y")
	app.Clear("id:inspector-camera-zoom")
	app.Type("id:inspector-camera-zoom", "2")
	app.Submit("id:inspector-camera-zoom")
	app.Clear("id:inspector-camera-rotation")
	app.Type("id:inspector-camera-rotation", "0.25")
	app.Submit("id:inspector-camera-rotation")

	if ed.Doc.Camera == nil || ed.Doc.Camera.X != 400 || ed.Doc.Camera.Y != 220 ||
		ed.Doc.Camera.EffectiveZoom() != 2 || !near(ed.Doc.Camera.Rotation, 0.25, 1e-5) {
		t.Fatalf("Camera = %+v, want (400, 220) zoom 2 rot 0.25", ed.Doc.Camera)
	}
	// Viewport follows camera edits live while View2DFollowsCamera is true.
	if v := ed.View2D(); v.X != 400 || v.Y != 220 || v.Zoom != 2 || !near(v.Rotation, 0.25, 1e-5) {
		t.Fatalf("View2D = %+v, want camera values", v)
	}

	// Enable camera bounds and edit its rectangle.
	if app.Exists("id:inspector-camera-bounds-x") {
		t.Fatal("bounds fields should be hidden while camera bounds are disabled")
	}
	app.Press("id:inspector-camera-bounds-enabled")
	if ed.Doc.Camera.Bounds == nil {
		t.Fatal("enabling bounds switch did not initialize Camera.Bounds")
	}
	if !app.Exists("id:inspector-camera-bounds-x") {
		t.Fatal("bounds fields did not appear after enabling camera bounds")
	}

	app.Clear("id:inspector-camera-bounds-x")
	app.Type("id:inspector-camera-bounds-x", "-50")
	app.Submit("id:inspector-camera-bounds-x")
	app.Clear("id:inspector-camera-bounds-y")
	app.Type("id:inspector-camera-bounds-y", "-25")
	app.Submit("id:inspector-camera-bounds-y")
	app.Clear("id:inspector-camera-bounds-width")
	app.Type("id:inspector-camera-bounds-width", "1280")
	app.Submit("id:inspector-camera-bounds-width")
	app.Clear("id:inspector-camera-bounds-height")
	app.Type("id:inspector-camera-bounds-height", "720")
	app.Submit("id:inspector-camera-bounds-height")

	wantBounds := sceneformat.BoundsDoc{X: -50, Y: -25, Width: 1280, Height: 720}
	if ed.Doc.Camera.Bounds == nil || *ed.Doc.Camera.Bounds != wantBounds {
		t.Fatalf("Bounds = %+v, want %+v", ed.Doc.Camera.Bounds, wantBounds)
	}

	// Save, reload, and verify byte-for-byte round-trip and every field!
	finalYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("final SaveYAML: %v", err)
	}
	reloaded := reloadedScene(t, ed)
	if reloaded.Name != "Cavern" || reloaded.VirtualWidth != 640 || reloaded.VirtualHeight != 360 || reloaded.BackgroundColor != "#334455" {
		t.Fatalf("reloaded scene settings = %+v", reloaded)
	}
	if reloaded.Camera == nil || reloaded.Camera.X != 400 || reloaded.Camera.Y != 220 ||
		reloaded.Camera.EffectiveZoom() != 2 || !near(reloaded.Camera.Rotation, 0.25, 1e-5) ||
		reloaded.Camera.Bounds == nil || *reloaded.Camera.Bounds != wantBounds {
		t.Fatalf("reloaded camera = %+v", reloaded.Camera)
	}

	// Undo every step back to the exact initial YAML byte-for-byte, then Redo back to final YAML.
	steps := ed.History.Index()
	if steps == 0 {
		t.Fatal("expected recorded history steps")
	}
	for range steps {
		app.Post(func() { ed.Undo() })
		app.Settle()
	}
	if ed.History.CanUndo() {
		t.Fatal("expected CanUndo false after undoing all steps")
	}
	undoneYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("undone SaveYAML: %v", err)
	}
	if !bytes.Equal(undoneYAML, initialYAML) {
		t.Fatalf("after undoing all scene settings edits, YAML differs:\ngot:\n%s\nwant:\n%s", undoneYAML, initialYAML)
	}
	if ed.Doc.Camera != nil {
		t.Fatalf("expected Camera nil after undoing all edits, got %+v", ed.Doc.Camera)
	}

	for range steps {
		app.Post(func() { ed.Redo() })
		app.Settle()
	}
	redoneYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("redone SaveYAML: %v", err)
	}
	if !bytes.Equal(redoneYAML, finalYAML) {
		t.Fatalf("after redoing all scene settings edits, YAML differs:\ngot:\n%s\nwant:\n%s", redoneYAML, finalYAML)
	}
}

// #871: A zero virtual width, non-positive height/zoom/bounds, or invalid background color
// is refused in Status and changes nothing (nor pushes to History).
func TestInspectorSceneSettingsRefusesInvalidValuesInStatus(t *testing.T) {
	ed := newTestEditor(t)
	if err := ed.SetCameraBounds(&sceneformat.BoundsDoc{X: 0, Y: 0, Width: 320, Height: 240}); err != nil {
		t.Fatalf("SetCameraBounds: %v", err)
	}
	ed.SelectObject("")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1200))

	beforeYAML, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	beforeHistory := ed.History.Len()

	cases := []struct {
		name     string
		selector string
		input    string
		wantSub  string
	}{
		{"zero virtual width", "id:inspector-scene-width", "0", "virtualWidth"},
		{"negative virtual width", "id:inspector-scene-width", "-10", "virtual size"},
		{"zero virtual height", "id:inspector-scene-height", "0", "virtualHeight"},
		{"invalid background", "id:inspector-scene-bg", "not-a-color", "backgroundColor"},
		{"zero camera zoom", "id:inspector-camera-zoom", "0", "camera zoom"},
		{"negative camera zoom", "id:inspector-camera-zoom", "-1", "camera zoom"},
		{"zero bounds width", "id:inspector-camera-bounds-width", "0", "camera bounds"},
		{"negative bounds height", "id:inspector-camera-bounds-height", "-5", "camera bounds"},
	}

	for _, tc := range cases {
		app.Clear(tc.selector)
		app.Type(tc.selector, tc.input)
		app.Submit(tc.selector)
		if !strings.Contains(ed.Status, tc.wantSub) {
			t.Fatalf("%s: Status = %q, want substring %q", tc.name, ed.Status, tc.wantSub)
		}
		if ed.History.Len() != beforeHistory {
			t.Fatalf("%s: refused edit pushed to history: len = %d, want %d", tc.name, ed.History.Len(), beforeHistory)
		}
		gotYAML, err := ed.SaveYAML()
		if err != nil {
			t.Fatalf("%s: SaveYAML: %v", tc.name, err)
		}
		if !bytes.Equal(gotYAML, beforeYAML) {
			t.Fatalf("%s: refused edit mutated scene:\ngot:\n%s\nwant:\n%s", tc.name, gotYAML, beforeYAML)
		}
	}
}

func TestSceneSettingsGoSettersValidationAndErrPlaying(t *testing.T) {
	ed := newTestEditor(t)
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	before := ed.History.Len()
	badCalls := []struct {
		name string
		err  error
	}{
		{"virtual size (0, 0)", ed.SetVirtualSize(0, 0)},
		{"virtual size (0, 120)", ed.SetVirtualSize(0, 120)},
		{"virtual size (160, 0)", ed.SetVirtualSize(160, 0)},
		{"virtual size (-1, 120)", ed.SetVirtualSize(-1, 120)},
		{"virtual size (NaN, 120)", ed.SetVirtualSize(nan, 120)},
		{"background invalid", ed.SetBackground("invalid-css")},
		{"camera position NaN", ed.SetCameraPosition(nan, 0)},
		{"camera zoom 0", ed.SetCameraZoom(0)},
		{"camera zoom -2", ed.SetCameraZoom(-2)},
		{"camera zoom Inf", ed.SetCameraZoom(inf)},
		{"camera rotation Inf", ed.SetCameraRotation(inf)},
		{"camera bounds zero width", ed.SetCameraBounds(&sceneformat.BoundsDoc{Width: 0, Height: 100})},
		{"camera bounds negative height", ed.SetCameraBounds(&sceneformat.BoundsDoc{Width: 100, Height: -1})},
		{"camera bounds NaN x", ed.SetCameraBounds(&sceneformat.BoundsDoc{X: nan, Width: 100, Height: 100})},
		{"SetCamera zero zoom", ed.SetCamera(&sceneformat.CameraDoc{Zoom: sceneformat.Float32(0)})},
	}
	for _, bc := range badCalls {
		if bc.err == nil {
			t.Errorf("%s: expected error, got nil", bc.name)
		}
	}
	if ed.History.Len() != before {
		t.Fatalf("refused setter calls recorded %d history entries", ed.History.Len()-before)
	}

	// Clearing background color works and is undoable.
	if err := ed.SetBackground(""); err != nil {
		t.Fatalf("SetBackground empty: %v", err)
	}
	if ed.Doc.BackgroundColor != "" || ed.Built.HasBackground {
		t.Fatalf("expected cleared background, got %q / HasBackground=%v", ed.Doc.BackgroundColor, ed.Built.HasBackground)
	}
	if !ed.Undo() || ed.Doc.BackgroundColor != "#141824" || !ed.Built.HasBackground {
		t.Fatalf("Undo cleared background failed: %q / %v", ed.Doc.BackgroundColor, ed.Built.HasBackground)
	}

	// SetCamera and ClearCameraBounds round-trip and undo cleanly.
	cam := &sceneformat.CameraDoc{
		X:        120,
		Y:        90,
		Zoom:     sceneformat.Float32(1.5),
		Rotation: 0.5,
		Bounds:   &sceneformat.BoundsDoc{X: 0, Y: 0, Width: 500, Height: 400},
	}
	if err := ed.SetCamera(cam); err != nil {
		t.Fatalf("SetCamera: %v", err)
	}
	if !reflect.DeepEqual(ed.Doc.Camera, cam) {
		t.Fatalf("Camera = %+v, want %+v", ed.Doc.Camera, cam)
	}
	if err := ed.ClearCameraBounds(); err != nil {
		t.Fatalf("ClearCameraBounds: %v", err)
	}
	if ed.Doc.Camera.Bounds != nil {
		t.Fatalf("expected nil Bounds after ClearCameraBounds, got %+v", ed.Doc.Camera.Bounds)
	}
	if !ed.Undo() || ed.Doc.Camera.Bounds == nil || *ed.Doc.Camera.Bounds != *cam.Bounds {
		t.Fatalf("Undo ClearCameraBounds = %+v, want %+v", ed.Doc.Camera.Bounds, cam.Bounds)
	}

	// While playing, all scene settings setters return ErrPlaying.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	for name, err := range map[string]error{
		"SetSceneName":      ed.SetSceneName("X"),
		"SetVirtualSize":    ed.SetVirtualSize(320, 180),
		"SetBackground":     ed.SetBackground("#000000"),
		"SetCamera":         ed.SetCamera(nil),
		"SetCameraPosition": ed.SetCameraPosition(10, 20),
		"SetCameraZoom":     ed.SetCameraZoom(2),
		"SetCameraRotation": ed.SetCameraRotation(1),
		"SetCameraBounds":   ed.SetCameraBounds(&sceneformat.BoundsDoc{Width: 100, Height: 100}),
		"ClearCameraBounds": ed.ClearCameraBounds(),
	} {
		if err != editor.ErrPlaying {
			t.Errorf("%s during Play = %v, want ErrPlaying", name, err)
		}
	}
	app.Post(func() { _ = ed.Stop() })
	app.Settle()
}

// #871: While scene settings are shown in the 2D viewport, the camera start view frame,
// bounds and camera handle are drawn, and dragging the camera handle updates camera (X, Y)
// as a single undoable command (both when View2D follows camera and when panned/zoomed).
func TestViewportCameraHandleDragUpdatesCameraAndIsUndoable(t *testing.T) {
	ed := newFarEditor(t)
	ed.SelectObject("")

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))

	// Camera handle is active and drawn as an ellipse in the viewport ops.
	hx, hy, ok := ed.CameraHandleCanvasPos()
	if !ok {
		t.Fatal("expected CameraHandleCanvasPos ok when no object is selected")
	}
	foundHandle := false
	for _, op := range app.Find("id:editor-viewport").Ops {
		if op.Kind == core.DrawEllipse && near(op.X, hx, 1e-2) && near(op.Y, hy, 1e-2) {
			foundHandle = true
			break
		}
	}
	if !foundHandle {
		t.Fatalf("expected camera handle ellipse at (%v, %v) in viewport ops", hx, hy)
	}

	// Drag the camera handle (+60, +30) canvas points while View2DFollowsCamera is true (zoom 1).
	beforeCam := *ed.Doc.Camera
	start := core.Point{X: core.Points(hx), Y: core.Points(hy)}
	end := core.Point{X: core.Points(hx + 60), Y: core.Points(hy + 30)}
	app.MouseDownAt("id:editor-viewport-frame", start, core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(hx + 30), Y: core.Points(hy + 15)})
	app.HoverAt("id:editor-viewport-frame", end)
	app.MouseUpAt("id:editor-viewport-frame", end, core.MouseButtonLeft)

	if !near(ed.Doc.Camera.X, beforeCam.X+60, 1e-2) || !near(ed.Doc.Camera.Y, beforeCam.Y+30, 1e-2) {
		t.Fatalf("camera after drag = (%v, %v), want (%v, %v)", ed.Doc.Camera.X, ed.Doc.Camera.Y, beforeCam.X+60, beforeCam.Y+30)
	}
	if ed.History.Len() != 1 || ed.History.UndoLabel() != "Move camera" {
		t.Fatalf("history after camera drag: len=%d label=%q, want 1 / 'Move camera'", ed.History.Len(), ed.History.UndoLabel())
	}
	// Reload preserves dragged camera position byte-for-byte.
	if r := reloadedScene(t, ed); r.Camera == nil || !near(r.Camera.X, beforeCam.X+60, 1e-2) || !near(r.Camera.Y, beforeCam.Y+30, 1e-2) {
		t.Fatalf("reloaded camera after drag = %+v", r.Camera)
	}

	// Undo restores pre-drag camera position; Redo restores dragged position.
	undoInUI(app, ed)
	if !near(ed.Doc.Camera.X, beforeCam.X, 1e-2) || !near(ed.Doc.Camera.Y, beforeCam.Y, 1e-2) {
		t.Fatalf("camera after Undo = (%v, %v), want (%v, %v)", ed.Doc.Camera.X, ed.Doc.Camera.Y, beforeCam.X, beforeCam.Y)
	}
	redoInUI(app, ed)
	if !near(ed.Doc.Camera.X, beforeCam.X+60, 1e-2) || !near(ed.Doc.Camera.Y, beforeCam.Y+30, 1e-2) {
		t.Fatalf("camera after Redo = (%v, %v), want (%v, %v)", ed.Doc.Camera.X, ed.Doc.Camera.Y, beforeCam.X+60, beforeCam.Y+30)
	}

	// Also test dragging when View2D is panned/zoomed (zoom 2 -> +40 canvas pts = +20 world units).
	ed.SetView2D(editor.View2D{X: ed.Doc.Camera.X, Y: ed.Doc.Camera.Y, Zoom: 2})
	hx2, hy2, ok := ed.CameraHandleCanvasPos()
	if !ok {
		t.Fatal("expected camera handle under zoomed View2D")
	}
	camBeforeZoomDrag := *ed.Doc.Camera
	app.MouseDownAt("id:editor-viewport-frame", core.Point{X: core.Points(hx2), Y: core.Points(hy2)}, core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(hx2 + 40), Y: core.Points(hy2 - 20)})
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: core.Points(hx2 + 40), Y: core.Points(hy2 - 20)}, core.MouseButtonLeft)

	if !near(ed.Doc.Camera.X, camBeforeZoomDrag.X+20, 1e-2) || !near(ed.Doc.Camera.Y, camBeforeZoomDrag.Y-10, 1e-2) {
		t.Fatalf("camera after zoomed drag = (%v, %v), want (%v, %v)", ed.Doc.Camera.X, ed.Doc.Camera.Y, camBeforeZoomDrag.X+20, camBeforeZoomDrag.Y-10)
	}

	// Selecting an object switches the inspector to object properties and hides the camera handle.
	app.Post(func() { ed.SelectObject("hero") })
	app.Settle()
	if app.Exists("id:inspector-scene") || !app.Exists("id:inspector-object-id") {
		t.Fatal("expected object inspector when hero is selected")
	}
	if _, _, ok := ed.CameraHandleCanvasPos(); ok {
		t.Fatal("camera handle should not be active while an object is selected")
	}
	// Deselecting restores Scene Settings and the camera handle.
	app.Post(func() { ed.SelectObject("") })
	app.Settle()
	if !app.Exists("id:inspector-scene") {
		t.Fatal("expected inspector-scene after deselecting")
	}
	if _, _, ok := ed.CameraHandleCanvasPos(); !ok {
		t.Fatal("expected camera handle after deselecting")
	}
}

func TestCameraDragFromNilCameraAndCancel(t *testing.T) {
	ed := newTestEditor(t)
	if ed.Doc.Camera != nil {
		t.Fatalf("expected nil initial camera in testSceneDoc, got %+v", ed.Doc.Camera)
	}
	// newTestEditor selects layer "actors" via SetActiveLayer; clear layer/object
	// inspection to return to Scene Settings before dragging the camera handle.
	if ed.ShowingSceneSettings() {
		t.Fatal("expected ShowingSceneSettings false while a layer is selected for inspection")
	}
	ed.SelectObject("")
	if !ed.ShowingSceneSettings() {
		t.Fatal("expected ShowingSceneSettings true after SelectObject(\"\")")
	}
	ed.SetCanvasSize(400, 300)
	hx, hy, ok := ed.CameraHandleCanvasPos()
	if !ok {
		t.Fatal("expected camera handle when Camera is nil")
	}
	// Cancel restores nil camera without pushing to History.
	if !ed.BeginCameraDrag(hx, hy) {
		t.Fatal("BeginCameraDrag refused hit on handle")
	}
	ed.UpdateCameraDrag(hx+25, hy+15)
	if ed.Doc.Camera == nil || !near(ed.Doc.Camera.X, 105, 1e-2) || !near(ed.Doc.Camera.Y, 75, 1e-2) {
		t.Fatalf("live camera during drag = %+v, want (105, 75)", ed.Doc.Camera)
	}
	ed.CancelCameraDrag()
	if ed.Doc.Camera != nil || ed.History.Len() != 0 {
		t.Fatalf("CancelCameraDrag left Camera=%+v, history=%d", ed.Doc.Camera, ed.History.Len())
	}

	// Committing drag initializes Camera from default center (80, 60) + (25, 15) = (105, 75),
	// and Undo restores Camera back to nil!
	if !ed.BeginCameraDrag(hx, hy) {
		t.Fatal("BeginCameraDrag failed")
	}
	ed.UpdateCameraDrag(hx+25, hy+15)
	if !ed.EndCameraDrag() {
		t.Fatal("EndCameraDrag returned false after moving camera")
	}
	if ed.Doc.Camera == nil || !near(ed.Doc.Camera.X, 105, 1e-2) || !near(ed.Doc.Camera.Y, 75, 1e-2) {
		t.Fatalf("committed camera = %+v, want (105, 75)", ed.Doc.Camera)
	}
	if !ed.Undo() || ed.Doc.Camera != nil {
		t.Fatalf("Undo after drag from nil camera left %+v, want nil", ed.Doc.Camera)
	}
}

func TestSceneSettingsNilEditorAndDragCancellationHooks(t *testing.T) {
	// Nil *Editor receiver must never panic on any scene settings setter.
	var nilEd *editor.Editor
	for name, err := range map[string]error{
		"SetSceneName":      nilEd.SetSceneName("X"),
		"SetVirtualSize":    nilEd.SetVirtualSize(320, 180),
		"SetBackground":     nilEd.SetBackground("#000000"),
		"SetCamera":         nilEd.SetCamera(nil),
		"SetCameraPosition": nilEd.SetCameraPosition(10, 20),
		"SetCameraZoom":     nilEd.SetCameraZoom(2),
		"SetCameraRotation": nilEd.SetCameraRotation(0.1),
		"SetCameraBounds":   nilEd.SetCameraBounds(&sceneformat.BoundsDoc{Width: 100, Height: 100}),
		"ClearCameraBounds": nilEd.ClearCameraBounds(),
	} {
		if err == nil {
			t.Errorf("nil Editor.%s returned nil error", name)
		}
	}

	// Escape during a live viewport camera drag cancels the drag and restores pre-drag camera.
	ed := newFarEditor(t)
	ed.SelectObject("")
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))

	hx, hy, ok := ed.CameraHandleCanvasPos()
	if !ok {
		t.Fatal("expected camera handle")
	}
	origCam := *ed.Doc.Camera
	app.MouseDownAt("id:editor-viewport-frame", core.Point{X: core.Points(hx), Y: core.Points(hy)}, core.MouseButtonLeft)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: core.Points(hx + 50), Y: core.Points(hy + 25)})
	if near(ed.Doc.Camera.X, origCam.X, 1e-2) {
		t.Fatal("expected camera X to move live during drag")
	}
	app.Key("escape")
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: core.Points(hx + 50), Y: core.Points(hy + 25)}, core.MouseButtonLeft)
	if !near(ed.Doc.Camera.X, origCam.X, 1e-2) || !near(ed.Doc.Camera.Y, origCam.Y, 1e-2) {
		t.Fatalf("camera after Escape cancel = (%v, %v), want (%v, %v)", ed.Doc.Camera.X, ed.Doc.Camera.Y, origCam.X, origCam.Y)
	}
	if ed.History.Len() != 0 {
		t.Fatalf("Escape-cancelled camera drag pushed %d history steps", ed.History.Len())
	}

	// Save and Undo cancel an active camera drag so half-dragged state never leaks.
	var savedBytes []byte
	ed.OnSave = func(data []byte) error {
		savedBytes = append([]byte(nil), data...)
		return nil
	}
	if !ed.BeginCameraDrag(hx, hy) {
		t.Fatal("BeginCameraDrag failed")
	}
	ed.UpdateCameraDrag(hx+40, hy+20)
	if err := ed.Save(); err != nil {
		t.Fatalf("Save during camera drag: %v", err)
	}
	if !near(ed.Doc.Camera.X, origCam.X, 1e-2) || !near(ed.Doc.Camera.Y, origCam.Y, 1e-2) {
		t.Fatalf("Save did not cancel camera drag: got (%v, %v), want (%v, %v)", ed.Doc.Camera.X, ed.Doc.Camera.Y, origCam.X, origCam.Y)
	}
	if len(savedBytes) != 0 {
		t.Fatal("Save rewrote an unedited scene after cancelling camera drag")
	}

	// Undo during a camera drag puts back the drag first, then undoes the prior edit.
	if err := ed.SetSceneName("BeforeDrag"); err != nil {
		t.Fatalf("SetSceneName: %v", err)
	}
	if !ed.BeginCameraDrag(hx, hy) {
		t.Fatal("BeginCameraDrag failed before Undo")
	}
	ed.UpdateCameraDrag(hx+40, hy+20)
	if !ed.Undo() {
		t.Fatal("Undo during camera drag returned false")
	}
	if !near(ed.Doc.Camera.X, origCam.X, 1e-2) || ed.Doc.Name == "BeforeDrag" {
		t.Fatalf("after Undo during drag: cam=(%v,%v) name=%q", ed.Doc.Camera.X, ed.Doc.Camera.Y, ed.Doc.Name)
	}
}

func TestInspectorSceneSettingsNoOpCameraSubmitAndPickerClosesOnPlay(t *testing.T) {
	ed := newTestEditor(t)
	ed.SelectObject("")
	if ed.Doc.Camera != nil {
		t.Fatalf("expected nil initial Camera, got %+v", ed.Doc.Camera)
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 1200))

	// Submitting unchanged effective defaults for X, Y, Zoom, Rotation when Camera is nil must not dirty the scene.
	for _, sel := range []string{
		"id:inspector-camera-x",
		"id:inspector-camera-y",
		"id:inspector-camera-zoom",
		"id:inspector-camera-rotation",
	} {
		app.Submit(sel)
	}
	if ed.Doc.Camera != nil || ed.History.Len() != 0 {
		t.Fatalf("submitting unchanged camera defaults mutated scene: Camera=%+v history=%d", ed.Doc.Camera, ed.History.Len())
	}

	// Opening the background color picker and entering Play closes the popover and keeps it closed on Stop.
	app.Press("id:inspector-scene-bg-picker")
	if !app.Exists("id:inspector-scene-bg-color-picker") {
		t.Fatal("expected color picker popover to open")
	}
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	if app.Exists("id:inspector-scene-bg-color-picker") {
		t.Fatal("color picker popover stayed open during Play")
	}
	app.Post(func() { _ = ed.Stop() })
	app.Settle()
	if app.Exists("id:inspector-scene-bg-color-picker") {
		t.Fatal("color picker popover popped back open after Stop")
	}
}
