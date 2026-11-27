package editor_test

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const viewportFrame = "id:editor-viewport-frame"

// start3DViewport mounts the editor without a GPU, so the canvas draws the whole wireframe view and frames cost no software rendering; navigation is the same either way.
func start3DViewport(t *testing.T, ed *editor.Editor, opts ...nsgotest.Option) *nsgotest.App {
	t.Helper()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return editor.EditorView(cx, ed) }, append([]nsgotest.Option{nsgotest.Size(1200, 720), nsgotest.NoGPU()}, opts...)...)
	if ed.ViewMode != editor.ViewMode3D {
		t.Fatalf("ViewMode = %v, want ViewMode3D", ed.ViewMode)
	}
	return app
}

// assertAt fails unless world point p projects to (x, y) on the canvas.
func assertAt(t *testing.T, ed *editor.Editor, label string, p [3]float32, x, y float32) {
	t.Helper()
	vw, vh := ed.CanvasSize()
	sx, sy, _, vis := ed.Camera3D.Project(p, vw, vh)
	if !vis || math.Abs(float64(sx-x)) > 0.5 || math.Abs(float64(sy-y)) > 0.5 {
		t.Fatalf("%s: %v is at (%v, %v) visible %v, want (%v, %v)", label, p, sx, sy, vis, x, y)
	}
}

// A middle-button drag, a drag with Shift held and a touchpad's two-finger scroll pan the view so what lies under the pointer follows it; a plain drag still orbits.
func TestViewport3DPansWithMiddleShiftAndTouchpad(t *testing.T) {
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "cube", Depth: 16})
	app := start3DViewport(t, ed)
	vw, vh := ed.CanvasSize()
	cx, cy := vw/2, vh/2

	target := ed.Camera3D.Target
	app.MouseDownAt(viewportFrame, pt(cx, cy), core.MouseButtonMiddle)
	app.HoverAt(viewportFrame, pt(cx+40, cy+25))
	app.MouseUpAt(viewportFrame, pt(cx+40, cy+25), core.MouseButtonMiddle)
	assertAt(t, ed, "after a middle drag of (40, 25)", target, cx+40, cy+25)
	if ed.History.Len() != 0 {
		t.Fatal("a pan went into the undo history")
	}

	target = ed.Camera3D.Target
	app.KeyDown("shift")
	app.DragAt(viewportFrame, pt(cx-200, cy), pt(-30, 50))
	app.KeyUp("shift")
	assertAt(t, ed, "after a Shift drag of (-30, 50)", target, cx-30, cy+50)

	target = ed.Camera3D.Target
	app.TouchpadScroll(viewportFrame, core.Point{}, core.Point{X: 12, Y: -8}, core.Point{X: 18, Y: -2})
	assertAt(t, ed, "after a touchpad scroll of (30, -10)", target, cx+30, cy-10)

	yaw := ed.Camera3D.Yaw
	app.DragAt(viewportFrame, pt(cx-200, cy), pt(60, 0))
	if ed.Camera3D.Yaw == yaw {
		t.Fatal("a plain drag did not orbit the view")
	}
}

// The wheel and a pinch zoom toward the target, which stays put.
func TestViewport3DZoomsWithWheelAndPinch(t *testing.T) {
	ed := newTest3DEditor(t)
	app := start3DViewport(t, ed)
	before := ed.Camera3D
	app.Wheel(viewportFrame, core.Point{Y: 40}, core.Modifiers{})
	if ed.Camera3D.Target != before.Target || ed.Camera3D.Distance == before.Distance {
		t.Fatalf("camera %+v after a wheel turn from %+v, want a zoom", ed.Camera3D, before)
	}
	before = ed.Camera3D
	app.Pinch(viewportFrame, 1, 1.5, 2)
	assertNear32(t, "distance after a pinch to 2", ed.Camera3D.Distance, before.Distance/2, 0.01)
	if ed.Camera3D.Target != before.Target {
		t.Fatal("a pinch moved the target")
	}
}

func near3(a, b [3]float32, tol float64) bool {
	for i := range 3 {
		if math.Abs(float64(a[i]-b[i])) > tol {
			return false
		}
	}
	return true
}

// Holding the right button flies the camera: the locked pointer turns it about its eye, W, A, S, D, Q and E move it each frame without switching the gizmo, the wheel changes the speed, and the camera stays where it flew to once the button comes up.
func TestViewport3DFliesWithTheRightButtonAndWASD(t *testing.T) {
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "cube", Depth: 16})
	ed.SetGizmoMode3D(editor.Gizmo3DRotate)
	ed.Camera3D.Orthographic = true
	app := start3DViewport(t, ed, nsgotest.Environment(core.Environment{CanLockPointer: true}))
	vw, vh := ed.CanvasSize()
	at := pt(vw/2, vh/2)

	app.MouseDownAt(viewportFrame, at, core.MouseButtonRight)
	if !ed.Flying3D() || ed.Camera3D.Orthographic {
		t.Fatalf("flying %v, orthographic %v after the right button went down; want a perspective flight", ed.Flying3D(), ed.Camera3D.Orthographic)
	}
	eye, yaw, pitch := ed.Camera3D.Eye(), ed.Camera3D.Yaw, ed.Camera3D.Pitch
	app.PointerDelta(100, -50)
	if !near3(ed.Camera3D.Eye(), eye, 1e-2) {
		t.Fatalf("looking moved the eye from %v to %v", eye, ed.Camera3D.Eye())
	}
	if ed.Camera3D.Yaw >= yaw || ed.Camera3D.Pitch >= pitch {
		t.Fatalf("yaw %v and pitch %v after the pointer moved right and up, from %v and %v; want the view turned right and up", ed.Camera3D.Yaw, ed.Camera3D.Pitch, yaw, pitch)
	}

	speed := ed.FlySpeed3D
	eye = ed.Camera3D.Eye()
	_, _, forward := ed.Camera3D.Basis()
	app.KeyDown("w")
	app.Advance(500 * time.Millisecond)
	app.KeyUp("w")
	moved := ed.Camera3D.Eye()
	var along, off float64
	for i := range 3 {
		along += float64(moved[i]-eye[i]) * float64(forward[i])
	}
	for i := range 3 {
		d := float64(moved[i]-eye[i]) - along*float64(forward[i])
		off += d * d
	}
	if along < float64(speed)*0.3 || along > float64(speed)*0.5+1e-3 || math.Sqrt(off) > 1e-2 {
		t.Fatalf("W held for 500ms at %v a second moved the eye %v forward and %v aside", speed, along, math.Sqrt(off))
	}
	if ed.GizmoMode != editor.Gizmo3DRotate {
		t.Fatalf("W while flying switched the gizmo to %v", ed.GizmoMode)
	}

	app.Wheel(viewportFrame, core.Point{Y: -200}, core.Modifiers{})
	assertNear32(t, "fly speed after a wheel turn up", ed.FlySpeed3D, speed*2, 1e-3)

	eye = ed.Camera3D.Eye()
	app.KeyDown("e")
	app.Advance(200 * time.Millisecond)
	app.KeyUp("e")
	if got := ed.Camera3D.Eye(); got[1] <= eye[1] || math.Abs(float64(got[0]-eye[0])) > 1e-2 {
		t.Fatalf("E moved the eye from %v to %v, want it straight up", eye, got)
	}

	app.MouseUpAt(viewportFrame, at, core.MouseButtonRight)
	if ed.Flying3D() {
		t.Fatal("still flying after the right button came up")
	}
	flown := ed.Camera3D
	app.Advance(100 * time.Millisecond)
	if ed.Camera3D != flown {
		t.Fatal("the camera moved after the flight ended")
	}
	app.Key("w")
	if ed.GizmoMode != editor.Gizmo3DTranslate {
		t.Fatalf("W after the flight left the gizmo at %v, want Translate", ed.GizmoMode)
	}
}

// Where the platform cannot lock the pointer, the pointer's movement with the right button held turns the flying camera.
func TestViewport3DFliesWithoutAPointerLock(t *testing.T) {
	ed := newTest3DEditor(t)
	app := start3DViewport(t, ed)
	vw, vh := ed.CanvasSize()
	app.MouseDownAt(viewportFrame, pt(vw/2, vh/2), core.MouseButtonRight)
	yaw := ed.Camera3D.Yaw
	app.HoverAt(viewportFrame, pt(vw/2+50, vh/2))
	assertNear32(t, "yaw after the pointer moved 50 right", ed.Camera3D.Yaw, yaw-50*0.004, 1e-5)
	app.MouseUpAt(viewportFrame, pt(vw/2+50, vh/2), core.MouseButtonRight)
	if ed.Flying3D() {
		t.Fatal("still flying after the right button came up")
	}
}

// F frames the selection, the number keys turn to the axis views, 5 switches the projection, and the bar, the axis widget, the Orbit buttons and Reset view do the same by pointer; the bar and the status bar name the view.
func TestViewport3DFocusAxisViewsAndProjection(t *testing.T) {
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "crate", X: 60, Y: 8, PosZ: -30, Depth: 16})
	app := start3DViewport(t, ed)
	text := func(sel string) string { t.Helper(); return app.Find(sel).Text }

	ed.SelectObject("crate")
	app.Settle()
	app.Key("f")
	crate, _, _ := ed.FindObject("crate")
	bmin, bmax := ed.ObjectAABB3D(crate)
	center := [3]float32{(bmin[0] + bmax[0]) / 2, (bmin[1] + bmax[1]) / 2, (bmin[2] + bmax[2]) / 2}
	if !near3(ed.Camera3D.Target, center, 1e-3) {
		t.Fatalf("F framed %v, want the crate's center %v", ed.Camera3D.Target, center)
	}
	if got := text("id:view3d-name"); got != "Perspective" {
		t.Fatalf("view name %q, want Perspective", got)
	}

	for _, tc := range []struct {
		key  string
		view editor.View3D
	}{
		{"7", editor.View3DTop},
		{"ctrl+7", editor.View3DBottom},
		{"1", editor.View3DFront},
		{"ctrl+1", editor.View3DBack},
		{"3", editor.View3DRight},
		{"ctrl+3", editor.View3DLeft},
	} {
		app.Key(tc.key)
		if v, ok := ed.Camera3D.AxisView(); !ok || v != tc.view {
			t.Fatalf("%s turned to %v (%v), want %v", tc.key, v, ok, tc.view)
		}
		if got := text("id:view3d-name"); got != tc.view.String() {
			t.Fatalf("after %s the bar names %q, want %q", tc.key, got, tc.view)
		}
		if !near3(ed.Camera3D.Target, center, 1e-3) {
			t.Fatalf("%s moved the target to %v", tc.key, ed.Camera3D.Target)
		}
	}
	app.Key("7")
	app.Key("5")
	if !ed.Orthographic3D() || text("id:view3d-name") != "Top · ortho" {
		t.Fatalf("after 5 orthographic %v, view %q; want Top · ortho", ed.Orthographic3D(), text("id:view3d-name"))
	}
	if got := text("id:editor-status-mode"); !strings.Contains(got, "Top · ortho · yaw 0° · pitch 90°") || !strings.Contains(got, " · Global") {
		t.Fatalf("status %q does not name the view and Global space", got)
	}
	app.Press("id:view3d-persp")
	if ed.Orthographic3D() {
		t.Fatal("Persp left the view orthographic")
	}
	app.Press("id:view3d-ortho")
	if !ed.Orthographic3D() {
		t.Fatal("Ortho left the view in perspective")
	}

	// Shading dropdown in the 3D viewport bar switches between Lit, Unlit and Wireframe.
	if got := text("id:view3d-shading-label"); got != "Lit" {
		t.Fatalf("initial shading label = %q, want Lit", got)
	}
	app.Press("id:view3d-shading")
	app.Press("id:view3d-shading-unlit")
	if ed.Shading3D != editor.Shading3DUnlit || text("id:view3d-shading-label") != "Unlit" {
		t.Fatalf("after Unlit: shading=%v label=%q", ed.Shading3D, text("id:view3d-shading-label"))
	}
	app.Press("id:view3d-shading")
	app.Press("id:view3d-shading-wireframe")
	if ed.Shading3D != editor.Shading3DWireframe || text("id:view3d-shading-label") != "Wireframe" {
		t.Fatalf("after Wireframe: shading=%v label=%q", ed.Shading3D, text("id:view3d-shading-label"))
	}
	app.Press("id:view3d-shading")
	app.Press("id:view3d-shading-lit")
	if ed.Shading3D != editor.Shading3DLit || text("id:view3d-shading-label") != "Lit" {
		t.Fatalf("after Lit: shading=%v label=%q", ed.Shading3D, text("id:view3d-shading-label"))
	}

	// Transform space toggle updates GizmoSpace3D and the status bar's " · Local" / " · Global" indicator.
	app.Press("id:view3d-space-local")
	if ed.GizmoSpace3D != editor.GizmoSpaceLocal || !strings.Contains(text("id:editor-status-mode"), " · Local") {
		t.Fatalf("after Local: space=%v status=%q", ed.GizmoSpace3D, text("id:editor-status-mode"))
	}
	app.Press("id:view3d-space-global")
	if ed.GizmoSpace3D != editor.GizmoSpaceGlobal || !strings.Contains(text("id:editor-status-mode"), " · Global") {
		t.Fatalf("after Global: space=%v status=%q", ed.GizmoSpace3D, text("id:editor-status-mode"))
	}

	// The orientation widget is 88 points in diameter with each axis end 28 points from its center,
	// and each axis end is also a real clickable, keyboard-reachable button.
	app.Key("1")
	app.PressAt("id:view3d-axes", pt(44+28, 44))
	if v, _ := ed.Camera3D.AxisView(); v != editor.View3DRight {
		t.Fatalf("pressing +X on the axis widget turned to %v, want Right", v)
	}
	app.PressAt("id:view3d-axes", pt(44, 44-28))
	if v, _ := ed.Camera3D.AxisView(); v != editor.View3DTop {
		t.Fatalf("pressing +Y on the axis widget turned to %v, want Top", v)
	}
	for _, tc := range []struct {
		id   string
		view editor.View3D
	}{
		{"id:view3d-axis-front", editor.View3DFront},
		{"id:view3d-axis-right", editor.View3DRight},
		{"id:view3d-axis-left", editor.View3DLeft},
		{"id:view3d-axis-top", editor.View3DTop},
		{"id:view3d-axis-bottom", editor.View3DBottom},
		{"id:view3d-axis-back", editor.View3DBack},
	} {
		app.Press(tc.id)
		if v, ok := ed.Camera3D.AxisView(); !ok || v != tc.view {
			t.Fatalf("pressing %s turned to %v (%v), want %v", tc.id, v, ok, tc.view)
		}
	}

	app.Key("1")
	app.Press("id:view3d-orbit-left")
	assertNear32(t, "yaw after Orbit left", ed.Camera3D.Yaw, -float32(math.Pi/12), 1e-5)
	app.Press("id:view3d-orbit-right")
	app.Press("id:view3d-orbit-right")
	assertNear32(t, "yaw after Orbit right twice", ed.Camera3D.Yaw, float32(math.Pi/12), 1e-5)
	if got := text("id:view3d-name"); got != "User · ortho" {
		t.Fatalf("an orbited orthographic view is named %q, want User · ortho", got)
	}

	app.Press("id:view3d-reset")
	want := editor.DefaultOrbitCamera3D()
	want.Orthographic = true
	if ed.Camera3D != want {
		t.Fatalf("Reset view left %+v, want %+v", ed.Camera3D, want)
	}
	ed.Camera3D.Target = [3]float32{500, 0, 0}
	app.Press("id:view3d-focus")
	if !near3(ed.Camera3D.Target, center, 1e-3) {
		t.Fatalf("Focus framed %v, want the crate's center %v", ed.Camera3D.Target, center)
	}
}

// In an orthographic view a press picks what lies under it and a gizmo drag moves the object by the distance the pointer travels along the axis.
func TestViewport3DOrthographicPickAndGizmoDrag(t *testing.T) {
	ed := newTest3DEditor(t,
		sceneformat.ObjectDoc{ID: "cube", Depth: 16},
		sceneformat.ObjectDoc{ID: "far", X: 120, Y: 8, PosZ: -60, Depth: 16},
	)
	ed.Camera3D.Distance = 300
	ed.SetView3D(editor.View3DTop)
	ed.SetOrthographic3D(true)
	ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
	app := start3DViewport(t, ed)
	vw, vh := ed.CanvasSize()

	sx, sy, _, vis := ed.Camera3D.Project([3]float32{120, 8, -60}, vw, vh)
	if !vis {
		t.Fatal("far is not visible from the top")
	}
	app.PressAt("id:editor-viewport", pt(sx, sy))
	if ed.SelectedID != "far" {
		t.Fatalf("a press on far from the top selected %q", ed.SelectedID)
	}
	ed.SelectObject("cube")
	app.Settle()

	for _, tc := range []struct {
		axis editor.GizmoAxis3D
		dir  [3]float32
	}{
		{editor.Axis3DX, [3]float32{1, 0, 0}},
		{editor.Axis3DZ, [3]float32{0, 0, 1}},
	} {
		obj, _, _ := ed.FindObject("cube")
		origin := [3]float32{obj.X, obj.Y, obj.PosZ}
		gLen := ed.GizmoLength3D(origin)
		from := [3]float32{origin[0] + tc.dir[0]*gLen*0.6, origin[1], origin[2] + tc.dir[2]*gLen*0.6}
		to := [3]float32{from[0] + tc.dir[0]*25, from[1], from[2] + tc.dir[2]*25}
		x0, y0, _, _ := ed.Camera3D.Project(from, vw, vh)
		x1, y1, _, _ := ed.Camera3D.Project(to, vw, vh)
		if got := ed.PickGizmo3D(x0, y0, vw, vh); got != tc.axis {
			t.Fatalf("the gizmo's %v arm picks %v from the top", tc.axis, got)
		}
		app.DragAt(viewportFrame, pt(x0, y0), pt(x1-x0, y1-y0))
		obj, _, _ = ed.FindObject("cube")
		got := [3]float32{obj.X, obj.Y, obj.PosZ}
		want := [3]float32{origin[0] + tc.dir[0]*25, origin[1], origin[2] + tc.dir[2]*25}
		if !near3(got, want, 0.5) {
			t.Fatalf("a %v drag of 25 from the top moved the cube from %v to %v, want %v", tc.axis, origin, got, want)
		}
	}
}

// Each scene of a project opens at the camera it was left at: the editor keeps the views in .game/editor.yaml, out of version control, and starts a scene it has no view of at the default camera.
func TestViewport3DCameraPersistsPerScene(t *testing.T) {
	dir := writeEditorProject(t)
	room, err := os.ReadFile(filepath.Join(dir, "scenes", "room.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenes", "hall.yaml"), room, 0o644); err != nil {
		t.Fatal(err)
	}
	ed, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ed.WaitForProblems)
	if err := ed.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if ed.Camera3D != editor.DefaultOrbitCamera3D() {
		t.Fatalf("a scene without a saved view opened at %+v", ed.Camera3D)
	}
	roomCam := editor.OrbitCamera3D{Target: [3]float32{12, 0, -40}, Yaw: 0.61, Pitch: 0.49, Distance: 320, FOV: 1, Near: 0.5, Far: 9000, Orthographic: true}
	ed.Camera3D = roomCam

	if err := ed.OpenScene("scenes/hall.yaml"); err != nil {
		t.Fatal(err)
	}
	if ed.Camera3D != editor.DefaultOrbitCamera3D() {
		t.Fatalf("hall opened at room's camera %+v", ed.Camera3D)
	}
	state := filepath.Join(dir, filepath.FromSlash(editor.ViewStateFile))
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("leaving room wrote no view state: %v", err)
	}
	if ignore, err := os.ReadFile(filepath.Join(dir, ".game", ".gitignore")); err != nil || string(ignore) != "*\n" {
		t.Fatalf(".game/.gitignore = %q, %v; want it to ignore the folder", ignore, err)
	}
	hallCam := editor.DefaultOrbitCamera3D()
	hallCam.SetView(editor.View3DTop)
	ed.Camera3D = hallCam
	if err := ed.Save(); err != nil {
		t.Fatal(err)
	}

	if err := ed.OpenScene("scenes/room.yaml"); err != nil {
		t.Fatal(err)
	}
	if ed.Camera3D != roomCam {
		t.Fatalf("room opened again at %+v, want %+v", ed.Camera3D, roomCam)
	}

	// Another session of the editor opens the main scene at its view, and the other scene at its own.
	again, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.WaitForProblems)
	if err := again.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if again.Camera3D != roomCam {
		t.Fatalf("reopening the project showed room at %+v, want %+v", again.Camera3D, roomCam)
	}
	if err := again.OpenScene("scenes/hall.yaml"); err != nil {
		t.Fatal(err)
	}
	if again.Camera3D != hallCam {
		t.Fatalf("hall opened at %+v, want the view saved with it %+v", again.Camera3D, hallCam)
	}

	// Saving the views drops a scene whose file is gone.
	if err := os.Remove(filepath.Join(dir, "scenes", "room.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := again.SaveViewState(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "room.yaml") || !strings.Contains(string(data), "hall.yaml") {
		t.Fatalf("after room's file went the view state is\n%s", data)
	}

	// A damaged file costs only the views: the scene opens at the default camera and the next save writes the file afresh.
	if err := os.WriteFile(state, []byte("version: 1\nscenes: {oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(third.WaitForProblems)
	if err := openWithoutMainScene(third, dir); err != nil {
		t.Fatal(err)
	}
	if third.Camera3D != editor.DefaultOrbitCamera3D() || !strings.Contains(third.Status, "Could not restore the view") {
		t.Fatalf("a damaged view state opened hall at %+v with status %q", third.Camera3D, third.Status)
	}
	if err := third.SaveViewState(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(state); err != nil || !strings.HasPrefix(string(data), "version: 1\nscenes:\n  - path: scenes/hall.yaml\n") {
		t.Fatalf("the view state after a save is %q, %v", data, err)
	}
}

// openWithoutMainScene takes the main scene, whose file is gone, out of the project at dir, opens the project and then the hall scene.
func openWithoutMainScene(ed *editor.Editor, dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "game.yaml"))
	if err != nil {
		return err
	}
	bare := strings.Split(string(data), "autoload:")[0]
	if err := os.WriteFile(filepath.Join(dir, "game.yaml"), []byte(bare), 0o644); err != nil {
		return err
	}
	if err := ed.OpenProject(dir); err != nil {
		return err
	}
	return ed.OpenScene("scenes/hall.yaml")
}

// Closing the window records the open scene's view, though nothing else needs saving.
func TestClosingTheWindowSavesTheView(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("closes the headless model's window")
	}
	ed, dir := openTestProject(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680), nsgotest.Environment(core.Environment{OS: core.OSMacOS}))
	app.Post(func() {
		ed.SetViewMode(editor.ViewMode3D)
		ed.SetView3D(editor.View3DRight)
		ed.SetOrthographic3D(true)
	})
	app.Settle()
	want := ed.Camera3D
	if !app.CloseWindow() {
		t.Fatal("the window did not close")
	}
	again, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.WaitForProblems)
	if err := again.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if again.Camera3D != want {
		t.Fatalf("the scene opened at %+v after the window closed, want %+v", again.Camera3D, want)
	}
}

// A right-click that neither turns nor moves the camera leaves an orthographic view orthographic; a flight that moves it stays in perspective.
func TestViewport3DRightClickKeepsOrthographic(t *testing.T) {
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "cube", Depth: 16})
	ed.SetView3D(editor.View3DTop)
	ed.SetOrthographic3D(true)
	app := start3DViewport(t, ed, nsgotest.Environment(core.Environment{CanLockPointer: true}))
	vw, vh := ed.CanvasSize()
	at := pt(vw/2, vh/2)
	before := ed.Camera3D

	app.MouseDownAt(viewportFrame, at, core.MouseButtonRight)
	app.MouseUpAt(viewportFrame, at, core.MouseButtonRight)
	if ed.Flying3D() || ed.Camera3D != before {
		t.Fatalf("a right-click left flying %v and the camera %+v, want %+v", ed.Flying3D(), ed.Camera3D, before)
	}

	app.MouseDownAt(viewportFrame, at, core.MouseButtonRight)
	app.PointerDelta(40, 0)
	app.MouseUpAt(viewportFrame, at, core.MouseButtonRight)
	if ed.Camera3D.Orthographic {
		t.Fatal("a flight that turned the camera went back to orthographic")
	}

	// Asking for perspective during an unmoved flight keeps it after the flight.
	ed.SetOrthographic3D(true)
	if !ed.BeginFly3D() {
		t.Fatal("the flight did not start")
	}
	ed.SetOrthographic3D(false)
	ed.EndFly3D()
	if ed.Camera3D.Orthographic {
		t.Fatal("Persp during a flight was undone when the flight ended")
	}
}

// Jumping to another view or projection, or resetting it, during a gizmo drag cancels the drag and puts the object back, so no drag mixes rays from two cameras.
func TestViewport3DViewChangesCancelAGizmoDrag(t *testing.T) {
	const w, h float32 = 640, 360
	for name, change := range map[string]func(*editor.Editor){
		"axis view":    func(ed *editor.Editor) { ed.SetView3D(editor.View3DFront) },
		"orthographic": func(ed *editor.Editor) { ed.SetOrthographic3D(true) },
		"reset":        func(ed *editor.Editor) { ed.ResetView3D() },
	} {
		t.Run(name, func(t *testing.T) {
			ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "cube", Depth: 16})
			ed.Camera3D = editor.OrbitCamera3D{Yaw: 0.65, Pitch: 0.55, Distance: 200, FOV: float32(math.Pi / 3), Near: 0.5, Far: 5000}
			ed.SelectObject("cube")
			ed.SetGizmoMode3D(editor.Gizmo3DTranslate)
			gLen := ed.GizmoLength3D([3]float32{})
			sx, sy, _, _ := ed.Camera3D.Project([3]float32{gLen * 0.6, 0, 0}, w, h)
			if axis := ed.BeginGizmoDrag3D(sx, sy, w, h); axis != editor.Axis3DX {
				t.Fatalf("picked %v, want X", axis)
			}
			ed.UpdateGizmoDrag3D(sx+40, sy)
			change(ed)
			ed.UpdateGizmoDrag3D(sx+80, sy)
			if ed.EndGizmoDrag3D() {
				t.Fatal("the drag carried on after the view changed")
			}
			obj, _, _ := ed.FindObject("cube")
			if obj.X != 0 || obj.Y != 0 || obj.PosZ != 0 || ed.History.Len() != 0 {
				t.Fatalf("the cube is at (%v, %v, %v) with %d edits after the cancelled drag", obj.X, obj.Y, obj.PosZ, ed.History.Len())
			}
		})
	}
}

// A pan or turn that is not finite changes nothing, and a camera that holds a value that is not finite is saved as the default camera, so the view state always loads again.
func TestViewport3DRefusesNonFiniteNavigation(t *testing.T) {
	nan := float32(math.NaN())
	ed := newTest3DEditor(t)
	before := ed.Camera3D
	ed.PanView3D(nan, 0, 360)
	ed.PanView3D(0, float32(math.Inf(-1)), 360)
	ed.OrbitView3D(nan, 0)
	if !ed.BeginFly3D() {
		t.Fatal("the flight did not start")
	}
	ed.FlyLook3D(nan, 0)
	ed.FlyMove3D(nan, 0, 0, false, time.Second)
	ed.AdjustFlySpeed3D(nan)
	ed.EndFly3D()
	if ed.Camera3D != before || math.IsNaN(float64(ed.FlySpeed3D)) {
		t.Fatalf("non-finite navigation left the camera %+v and fly speed %v, want %+v", ed.Camera3D, ed.FlySpeed3D, before)
	}

	ed, dir := openTestProject(t)
	ed.Camera3D.Target[0] = nan
	if err := ed.SaveViewState(); err != nil {
		t.Fatal(err)
	}
	again, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.WaitForProblems)
	if err := again.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if again.Camera3D != editor.DefaultOrbitCamera3D() || strings.Contains(again.Status, "Could not restore") {
		t.Fatalf("a saved NaN camera reopened at %+v with status %q", again.Camera3D, again.Status)
	}
}

// A view state a newer editor wrote is neither read nor written over, so an older editor never drops the views it keeps.
func TestViewStateFromANewerEditorIsLeftAlone(t *testing.T) {
	ed, dir := openTestProject(t)
	state := filepath.Join(dir, filepath.FromSlash(editor.ViewStateFile))
	if err := os.MkdirAll(filepath.Dir(state), 0o755); err != nil {
		t.Fatal(err)
	}
	newer := []byte("version: 2\nscenes:\n  - path: scenes/room.yaml\n    layout: {split: 0.5}\n")
	if err := os.WriteFile(state, newer, 0o644); err != nil {
		t.Fatal(err)
	}
	ed.Camera3D.Yaw = 1
	if err := ed.SaveViewState(); err == nil || !strings.Contains(err.Error(), "newer editor") {
		t.Fatalf("SaveViewState over a newer file = %v, want an error naming the newer editor", err)
	}
	again, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.WaitForProblems)
	if err := again.OpenProject(dir); err != nil {
		t.Fatal(err)
	}
	if again.Camera3D != editor.DefaultOrbitCamera3D() {
		t.Fatalf("a newer view state opened the scene at %+v, want the default camera", again.Camera3D)
	}
	if data, err := os.ReadFile(state); err != nil || !bytes.Equal(data, newer) {
		t.Fatalf("the newer view state is now %q, %v", data, err)
	}
}

// Opening a project without a main scene keeps the open scene open, so its view is saved in the project it was shown in first.
func TestOpeningAProjectWithoutAMainSceneSavesTheViewInTheFirst(t *testing.T) {
	ed, first := openTestProject(t)
	cam := editor.DefaultOrbitCamera3D()
	cam.SetView(editor.View3DRight)
	cam.Orthographic = true
	ed.Camera3D = cam

	second := writeEditorProject(t)
	data, err := os.ReadFile(filepath.Join(second, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "game.yaml"), []byte(strings.Split(string(data), "autoload:")[0]), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ed.OpenProject(second); err != nil {
		t.Fatal(err)
	}
	if ed.Camera3D != cam {
		t.Fatalf("the scene kept open under the second project moved to %+v", ed.Camera3D)
	}
	if _, err := os.Stat(filepath.Join(second, filepath.FromSlash(editor.ViewStateFile))); err == nil {
		t.Fatal("opening the second project wrote a view state there")
	}
	again, err := editor.New(nil, testResolver())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.WaitForProblems)
	if err := again.OpenProject(first); err != nil {
		t.Fatal(err)
	}
	if again.Camera3D != cam {
		t.Fatalf("the first project's scene reopened at %+v, want %+v", again.Camera3D, cam)
	}
}

// With a camera Distance of zero, as a view state or an app may set it, the orthographic gizmo is sized from the distance the view is drawn at.
func TestOrthographicGizmoLengthUsesTheDrawnDistance(t *testing.T) {
	ed := newTest3DEditor(t)
	ed.Camera3D.Orthographic = true
	ed.Camera3D.Distance = 240
	want := ed.GizmoLength3D([3]float32{})
	ed.Camera3D.Distance = 0
	if got := ed.GizmoLength3D([3]float32{}); got != want {
		t.Fatalf("GizmoLength3D at distance 0 = %v, want %v as at the drawn 240", got, want)
	}
}

// Rotate and scale drags work in an orthographic plan, translate drags in an orthographic elevation, and the arm pointing at the eye neither moves nor makes the object jump.
func TestOrthographicGizmoRotateScaleAndElevations(t *testing.T) {
	const w, h float32 = 640, 360
	newEd := func(t *testing.T, view editor.View3D, mode editor.GizmoMode3D) *editor.Editor {
		t.Helper()
		ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "cube", Depth: 16})
		ed.Camera3D = editor.OrbitCamera3D{Distance: 200, FOV: float32(math.Pi / 3), Near: 0.5, Far: 5000, Orthographic: true}
		ed.Camera3D.SetView(view)
		ed.SelectObject("cube")
		ed.SetGizmoMode3D(mode)
		return ed
	}
	drag := func(t *testing.T, ed *editor.Editor, want editor.GizmoAxis3D, pts ...[3]float32) {
		t.Helper()
		x0, y0, _, vis := ed.Camera3D.Project(pts[0], w, h)
		if !vis {
			t.Fatalf("%v is not visible", pts[0])
		}
		if axis := ed.BeginGizmoDrag3D(x0, y0, w, h); axis != want {
			t.Fatalf("picked %v, want %v", axis, want)
		}
		for _, p := range pts[1:] {
			x, y, _, _ := ed.Camera3D.Project(p, w, h)
			ed.UpdateGizmoDrag3D(x, y)
		}
		if !ed.EndGizmoDrag3D() {
			t.Fatal("the drag did not commit")
		}
	}

	t.Run("rotate Y in Top", func(t *testing.T) {
		ed := newEd(t, editor.View3DTop, editor.Gizmo3DRotate)
		r := float64(ed.GizmoLength3D([3]float32{}))
		ring := func(a float64) [3]float32 { return [3]float32{float32(math.Cos(a) * r), 0, float32(-math.Sin(a) * r)} }
		drag(t, ed, editor.Axis3DY, ring(0.7), ring(0.7+math.Pi/8), ring(0.7+math.Pi/4))
		obj, _, _ := ed.FindObject("cube")
		assertNear32(t, "RotY", obj.RotY, float32(math.Pi/4), 0.02)
		assertNear32(t, "RotX", obj.RotX, 0, 0.02)
		assertNear32(t, "RotZ", obj.RotZ, 0, 0.02)
	})

	t.Run("scale X in Top", func(t *testing.T) {
		ed := newEd(t, editor.View3DTop, editor.Gizmo3DScale)
		l := ed.GizmoLength3D([3]float32{})
		drag(t, ed, editor.Axis3DX, [3]float32{l, 0, 0}, [3]float32{l * 1.5, 0, 0})
		obj, _, _ := ed.FindObject("cube")
		assertNear32(t, "ScaleX", obj.EffectiveScaleX(), 1.5, 0.02)
		assertNear32(t, "ScaleY", obj.EffectiveScaleY(), 1, 0.02)
		assertNear32(t, "ScaleZ", obj.EffectiveScaleZ(), 1, 0.02)
	})

	t.Run("translate X and Y in Front", func(t *testing.T) {
		for _, tc := range []struct {
			axis editor.GizmoAxis3D
			dir  [3]float32
		}{
			{editor.Axis3DX, [3]float32{1, 0, 0}},
			{editor.Axis3DY, [3]float32{0, 1, 0}},
		} {
			ed := newEd(t, editor.View3DFront, editor.Gizmo3DTranslate)
			l := ed.GizmoLength3D([3]float32{}) * 0.6
			from := [3]float32{tc.dir[0] * l, tc.dir[1] * l, 0}
			drag(t, ed, tc.axis, from, [3]float32{from[0] + tc.dir[0]*25, from[1] + tc.dir[1]*25, 0})
			obj, _, _ := ed.FindObject("cube")
			if got, want := [3]float32{obj.X, obj.Y, obj.PosZ}, [3]float32{tc.dir[0] * 25, tc.dir[1] * 25, 0}; !near3(got, want, 0.5) {
				t.Fatalf("a %v drag of 25 in Front moved the cube to %v, want %v", tc.axis, got, want)
			}
		}
	})

	t.Run("Y arm in Top", func(t *testing.T) {
		// From the top, clicking the gizmo center picks the screen-space center handle (Axis3DAll),
		// while dragging the Y arm edge-on (pointing at the eye) has no direction on screen to follow, so it moves nothing.
		ed := newEd(t, editor.View3DTop, editor.Gizmo3DTranslate)
		cx, cy, _, _ := ed.Camera3D.Project([3]float32{}, w, h)
		if axis := ed.PickGizmo3D(cx, cy, w, h); axis != editor.Axis3DAll {
			t.Fatalf("a press at the gizmo's center from the top picked %v, want Axis3DAll", axis)
		}
		if !ed.BeginGizmoDragAxis3D(editor.Axis3DY, cx, cy, w, h) {
			t.Fatal("BeginGizmoDragAxis3D(Axis3DY) returned false")
		}
		ed.UpdateGizmoDrag3D(cx+30, cy+20)
		ed.UpdateGizmoDrag3D(cx+60, cy+40)
		ed.EndGizmoDrag3D()
		obj, _, _ := ed.FindObject("cube")
		if obj.X != 0 || obj.Y != 0 || obj.PosZ != 0 {
			t.Fatalf("a drag on the Y arm, edge on from the top, moved the cube to (%v, %v, %v)", obj.X, obj.Y, obj.PosZ)
		}
	})
}

// Local space aligns the 3D transform gizmo with the selected object's orientation,
// while Global space aligns it with the world axes.
func TestViewport3DLocalGizmoSpaceOnRotatedObject(t *testing.T) {
	const w, h float32 = 640, 360
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{
		ID:    "pillar",
		Name:  "Pillar",
		RotY:  float32(math.Pi / 2), // local +X points along world -Z
		Depth: 16,
	})
	ed.Camera3D = editor.OrbitCamera3D{Distance: 200, FOV: float32(math.Pi / 3), Near: 0.5, Far: 5000, Orthographic: true}
	ed.Camera3D.SetView(editor.View3DTop)
	ed.SelectObject("pillar")
	ed.SetGizmoMode3D(editor.Gizmo3DTranslate)

	if err := ed.ExecuteCommand(editor.CmdGizmoSpace3D); err != nil {
		t.Fatalf("ExecuteCommand(CmdGizmoSpace3D): %v", err)
	}
	if ed.GizmoSpace3D != editor.GizmoSpaceLocal {
		t.Fatalf("GizmoSpace3D = %v, want Local", ed.GizmoSpace3D)
	}

	gLen := ed.GizmoLength3D([3]float32{0, 0, 0})
	// Local +X points along world -Z because RotY = +90°.
	from := [3]float32{0, 0, -gLen * 0.6}
	to := [3]float32{0, 0, -gLen*0.6 - 25}
	x0, y0, _, vis0 := ed.Camera3D.Project(from, w, h)
	x1, y1, _, vis1 := ed.Camera3D.Project(to, w, h)
	if !vis0 || !vis1 {
		t.Fatal("local X drag points are not visible")
	}
	if axis := ed.BeginGizmoDrag3D(x0, y0, w, h); axis != editor.Axis3DX {
		t.Fatalf("BeginGizmoDrag3D on rotated local +X picked %v, want X", axis)
	}
	ed.UpdateGizmoDrag3D(x1, y1)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("local X drag did not commit")
	}
	obj, _, _ := ed.FindObject("pillar")
	assertNear32(t, "local X drag world X", obj.X, 0, 0.5)
	assertNear32(t, "local X drag world PosZ", obj.PosZ, -25, 0.5)

	// Switching back to Global restores world +X dragging.
	if err := ed.ExecuteCommand(editor.CmdGizmoSpace3D); err != nil {
		t.Fatalf("ExecuteCommand(CmdGizmoSpace3D) toggle back: %v", err)
	}
	if ed.GizmoSpace3D != editor.GizmoSpaceGlobal {
		t.Fatalf("GizmoSpace3D = %v, want Global", ed.GizmoSpace3D)
	}
	origin := [3]float32{obj.X, obj.Y, obj.PosZ}
	gFrom := [3]float32{origin[0] + gLen*0.6, origin[1], origin[2]}
	gTo := [3]float32{gFrom[0] + 20, gFrom[1], gFrom[2]}
	gx0, gy0, _, _ := ed.Camera3D.Project(gFrom, w, h)
	gx1, gy1, _, _ := ed.Camera3D.Project(gTo, w, h)
	if axis := ed.BeginGizmoDrag3D(gx0, gy0, w, h); axis != editor.Axis3DX {
		t.Fatalf("BeginGizmoDrag3D on global +X picked %v, want X", axis)
	}
	ed.UpdateGizmoDrag3D(gx1, gy1)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("global X drag did not commit")
	}
	obj, _, _ = ed.FindObject("pillar")
	assertNear32(t, "global X drag world X", obj.X, 20, 0.5)
	assertNear32(t, "global X drag world PosZ", obj.PosZ, -25, 0.5)
}

// Local rotation composes rotation matrices properly on an already-rotated object,
// multi-object local translate snaps along the local axis, and sheared/singular/NaN parents
// never produce skewed or non-finite gizmo axes.
func TestViewport3DLocalRotateEulerMultiSnapAndOrthonormalParent(t *testing.T) {
	const w, h float32 = 640, 360

	// 1. Local Z rotation on an object rotated +90° around Y:
	//    Local +Z points along world +X, so rotating +90° around Local Z must carry
	//    Local +Y (initially world +Y) to world +Z (not world -X as naive obj.RotZ += angle would!).
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{
		ID:    "pillar",
		Name:  "Pillar",
		RotY:  float32(math.Pi / 2),
		Depth: 16,
	})
	ed.Camera3D = editor.OrbitCamera3D{Distance: 200, FOV: float32(math.Pi / 3), Near: 0.5, Far: 5000, Orthographic: true}
	ed.Camera3D.SetView(editor.View3DRight) // look down +X (which is Local +Z)
	ed.SelectObject("pillar")
	ed.SetGizmoMode3D(editor.Gizmo3DRotate)
	ed.SetGizmoSpace3D(editor.GizmoSpaceLocal)

	r := float64(ed.GizmoLength3D([3]float32{}))
	// Local Z ring lies in the YZ world plane (around world +X = Local +Z).
	ringPt := func(a float64) [3]float32 {
		return [3]float32{0, float32(math.Cos(a) * r), float32(math.Sin(a) * r)}
	}
	p0 := ringPt(0)
	p1 := ringPt(math.Pi / 4)
	p2 := ringPt(math.Pi / 2)
	sx0, sy0, _, _ := ed.Camera3D.Project(p0, w, h)
	sx1, sy1, _, _ := ed.Camera3D.Project(p1, w, h)
	sx2, sy2, _, _ := ed.Camera3D.Project(p2, w, h)
	if !ed.BeginGizmoDragAxis3D(editor.Axis3DZ, sx0, sy0, w, h) {
		t.Fatal("BeginGizmoDragAxis3D(Axis3DZ) failed")
	}
	ed.UpdateGizmoDrag3D(sx1, sy1)
	ed.UpdateGizmoDrag3D(sx2, sy2)
	if !ed.EndGizmoDrag3D() {
		t.Fatal("local Z rotate did not commit")
	}
	obj, _, _ := ed.FindObject("pillar")
	// Apply Rz*Ry*Rx to local +Y = (0,1,0) and local +Z = (0,0,1).
	applyEuler := func(v [3]float64, rx, ry, rz float64) [3]float64 {
		x, y, z := v[0], v[1], v[2]
		sx, cx := math.Sincos(rx)
		y, z = y*cx-z*sx, y*sx+z*cx
		sy, cy := math.Sincos(ry)
		x, z = x*cy+z*sy, -x*sy+z*cy
		sz, cz := math.Sincos(rz)
		x, y = x*cz-y*sz, x*sz+y*cz
		return [3]float64{x, y, z}
	}
	localZAfter := applyEuler([3]float64{0, 0, 1}, float64(obj.RotX), float64(obj.RotY), float64(obj.RotZ))
	localYAfter := applyEuler([3]float64{0, 1, 0}, float64(obj.RotX), float64(obj.RotY), float64(obj.RotZ))
	// Local +Z must remain aligned with world +X (invariant rotation axis).
	assertNear32(t, "localZ.x", float32(localZAfter[0]), 1, 0.05)
	assertNear32(t, "localZ.y", float32(localZAfter[1]), 0, 0.05)
	assertNear32(t, "localZ.z", float32(localZAfter[2]), 0, 0.05)
	// Local +Y rotated +90° around world +X -> world +Z = (0, 0, 1).
	assertNear32(t, "localY.x", float32(localYAfter[0]), 0, 0.05)
	assertNear32(t, "localY.y", float32(localYAfter[1]), 0, 0.05)
	assertNear32(t, "localY.z", float32(localYAfter[2]), 1, 0.05)

	// 2. Multi-object Local translate with SnapToGrid snaps along the rotated local axis.
	edMulti := newTest3DEditor(t,
		sceneformat.ObjectDoc{ID: "a", Name: "A", X: 5, PosZ: 3, RotY: float32(math.Pi / 4), Depth: 16},
		sceneformat.ObjectDoc{ID: "b", Name: "B", X: 25, PosZ: 3, RotY: float32(math.Pi / 4), Depth: 16},
	)
	edMulti.Camera3D = editor.OrbitCamera3D{Distance: 200, FOV: float32(math.Pi / 3), Near: 0.5, Far: 5000, Orthographic: true}
	edMulti.Camera3D.SetView(editor.View3DTop)
	edMulti.SelectObjects([]string{"a", "b"})
	edMulti.SetGizmoMode3D(editor.Gizmo3DTranslate)
	edMulti.SetGizmoSpace3D(editor.GizmoSpaceLocal)
	edMulti.SetSnapToGrid(true)
	edMulti.GridSize = 16

	// Local +X for RotY = +45° is (cos45, 0, -sin45). Move ~18 units -> snaps to 16 units along local +X.
	invSqrt2 := float32(1 / math.Sqrt2)
	start := [3]float32{15, 0, 3}
	end := [3]float32{start[0] + 18*invSqrt2, 0, start[2] - 18*invSqrt2}
	msx0, msy0, _, _ := edMulti.Camera3D.Project(start, w, h)
	msx1, msy1, _, _ := edMulti.Camera3D.Project(end, w, h)
	if !edMulti.BeginGizmoDragAxis3D(editor.Axis3DX, msx0, msy0, w, h) {
		t.Fatal("multi BeginGizmoDragAxis3D failed")
	}
	edMulti.UpdateGizmoDrag3D(msx1, msy1)
	if !edMulti.EndGizmoDrag3D() {
		t.Fatal("multi local translate did not commit")
	}
	objA, _, _ := edMulti.FindObject("a")
	objB, _, _ := edMulti.FindObject("b")
	wantDX := 16 * invSqrt2
	wantDZ := -16 * invSqrt2
	assertNear32(t, "multi local snap A.X", objA.X-5, wantDX, 0.5)
	assertNear32(t, "multi local snap A.PosZ", objA.PosZ-3, wantDZ, 0.5)
	assertNear32(t, "multi local snap B.X", objB.X-25, wantDX, 0.5)
	assertNear32(t, "multi local snap B.PosZ", objB.PosZ-3, wantDZ, 0.5)

	// 3. Sheared/negative-scale parent + NaN rotation never produces NaN/Inf drag results.
	edParent := newTest3DEditor(t, sceneformat.ObjectDoc{
		ID:     "parent",
		Name:   "Parent",
		ScaleX: sceneformat.Float32(2),
		ScaleY: sceneformat.Float32(1),
		ScaleZ: sceneformat.Float32(-1),
		Children: []sceneformat.ObjectDoc{{
			ID:    "child",
			Name:  "Child",
			RotZ:  float32(math.Pi / 4),
			Depth: 16,
		}},
	})
	edParent.Camera3D = editor.OrbitCamera3D{Distance: 200, FOV: float32(math.Pi / 3), Near: 0.5, Far: 5000, Orthographic: true}
	edParent.Camera3D.SetView(editor.View3DFront)
	edParent.SelectObject("child")
	edParent.SetGizmoSpace3D(editor.GizmoSpaceLocal)
	edParent.SetGizmoMode3D(editor.Gizmo3DTranslate)
	child, _, _ := edParent.FindObject("child")
	child.RotX = float32(math.NaN())
	// Must fall back cleanly to finite world axes without panicking or writing NaN.
	if axis := edParent.PickGizmo3D(w/2+30, h/2, w, h); axis != editor.Axis3DNone && axis != editor.Axis3DX {
		t.Fatalf("PickGizmo3D with NaN RotX = %v", axis)
	}
}

// Spec Light theme, 60 Hz axis-button memoization, dynamic EffectiveShortcut keycaps,
// ScopeViewport3D on CmdGizmoSpace3D, menubar parity, and Play-mode guard all work together.
func TestViewport3DSpecLightThemeMemoizedAxesDynamicShortcutsAndPlayGuard(t *testing.T) {
	ed := newTest3DEditor(t, sceneformat.ObjectDoc{ID: "cube", Name: "Cube", Depth: 16})
	ed.SetAppearance(editor.AppearanceLight)

	// CmdGizmoSpace3D has ScopeViewport3D and is exposed in View > 3D Gizmo.
	var foundSpaceCmd bool
	for _, c := range ed.Commands() {
		if c.ID == editor.CmdGizmoSpace3D {
			foundSpaceCmd = true
			if c.Scope != editor.ScopeViewport3D {
				t.Fatalf("CmdGizmoSpace3D scope = %v, want ScopeViewport3D", c.Scope)
			}
		}
	}
	if !foundSpaceCmd {
		t.Fatal("CmdGizmoSpace3D missing from registry")
	}

	app := start3DViewport(t, ed, nsgotest.Environment(core.Environment{
		OS:     core.OSMacOS,
		Idiom:  core.IdiomDesktop,
		Dark:   false,
		Locale: "en-US",
	}))

	// 1. Light Spec theme: 3D HUD bars use LightSpecPalette().PillBg (not hardcoded dark #171A21).
	wantPillBg := editor.LightSpecPalette().PillBg
	if got := app.Find("id:view3d-bar").Style.Background; got != wantPillBg {
		t.Fatalf("Light theme view3d-bar background = 0x%08x, want LightSpecPalette().PillBg 0x%08x", uint32(got), uint32(wantPillBg))
	}
	if got := app.Find("id:view3d-bottom-bar").Style.Background; got != wantPillBg {
		t.Fatalf("Light theme view3d-bottom-bar background = 0x%08x, want LightSpecPalette().PillBg 0x%08x", uint32(got), uint32(wantPillBg))
	}

	// 2. Unrelated rev ticks must NOT remount view3d-axis-right (60 Hz focus stability).
	axisBefore := app.Find("id:view3d-axis-right")
	app.Post(func() {
		ed.SetSnapToGrid(!ed.SnapToGrid)
		ed.SetSnapToGrid(!ed.SnapToGrid)
	})
	app.Settle()
	axisAfter := app.Find("id:view3d-axis-right")
	if axisBefore.ID != axisAfter.ID {
		t.Fatalf("unrelated rev bump remounted view3d-axis-right: %v -> %v", axisBefore.ID, axisAfter.ID)
	}

	// 3. Rebinding CmdFrameSelection updates view3d-focus and view3d-help keycaps dynamically.
	app.Post(func() {
		if err := ed.RebindShortcut(editor.CmdFrameSelection, "g"); err != nil {
			t.Errorf("RebindShortcut(CmdFrameSelection, g): %v", err)
		}
	})
	app.Settle()
	helpLabel := app.Find("id:view3d-help").Props.Label
	focusLabel := app.Find("id:view3d-focus").Props.Label
	if !strings.Contains(helpLabel, "G focus") {
		t.Fatalf("view3d-help label after rebinding Frame to g = %q, want 'G focus'", helpLabel)
	}
	if !strings.Contains(focusLabel, "(G)") {
		t.Fatalf("view3d-focus label after rebinding Frame to g = %q, want '(G)'", focusLabel)
	}

	// 4. SetGizmoSpace3D is ignored during Play mode.
	ed.SetGizmoSpace3D(editor.GizmoSpaceGlobal)
	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
		ed.SetGizmoSpace3D(editor.GizmoSpaceLocal)
	})
	app.Settle()
	if ed.GizmoSpace3D != editor.GizmoSpaceGlobal {
		t.Fatalf("SetGizmoSpace3D mutated space during Play mode: got %v", ed.GizmoSpace3D)
	}
	app.Post(func() {
		_ = ed.Stop()
	})
	app.Settle()
}
