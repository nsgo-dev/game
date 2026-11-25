package editor_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
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
	if got := text("id:editor-status-mode"); !strings.Contains(got, "Top · ortho · yaw 0° · pitch 90°") {
		t.Fatalf("status %q does not name the view", got)
	}
	app.Press("id:view3d-persp")
	if ed.Orthographic3D() {
		t.Fatal("Persp left the view orthographic")
	}
	app.Press("id:view3d-ortho")
	if !ed.Orthographic3D() {
		t.Fatal("Ortho left the view in perspective")
	}

	// The axis widget is 76 points square with each axis's end 26 points from its center; in the front view +X lies right of the center and +Y above it.
	app.Key("1")
	app.PressAt("id:view3d-axes", pt(38+26, 38))
	if v, _ := ed.Camera3D.AxisView(); v != editor.View3DRight {
		t.Fatalf("pressing +X on the axis widget turned to %v, want Right", v)
	}
	app.PressAt("id:view3d-axes", pt(38, 38-26))
	if v, _ := ed.Camera3D.AxisView(); v != editor.View3DTop {
		t.Fatalf("pressing +Y on the axis widget turned to %v, want Top", v)
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
