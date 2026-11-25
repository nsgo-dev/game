package editor_test

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// farSceneDoc is a level wider than one screen: a 160x120 camera at the start of a 3000-point-wide stage, with an object 2000 points to the right of the camera's start.
func farSceneDoc() *sceneformat.SceneDoc {
	doc := testSceneDoc()
	doc.Camera = &sceneformat.CameraDoc{
		X:      80,
		Y:      60,
		Bounds: &sceneformat.BoundsDoc{X: 0, Y: 0, Width: 3000, Height: 120},
	}
	doc.Layers[1].Objects = append(doc.Layers[1].Objects, sceneformat.ObjectDoc{
		ID:      "far",
		Name:    "Far",
		X:       2080,
		Y:       60,
		OriginX: 8,
		OriginY: 8,
		Sprite:  &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"},
	})
	return doc
}

func newFarEditor(t *testing.T) *editor.Editor {
	t.Helper()
	ed, err := editor.New(farSceneDoc(), testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetActiveLayer("actors")
	return ed
}

func near(a, b, tol float32) bool { return math.Abs(float64(a-b)) <= float64(tol) }

func TestView2DFollowsTheSceneCameraUntilMoved(t *testing.T) {
	ed := newFarEditor(t)
	if !ed.View2DFollowsCamera() {
		t.Fatal("a new editor's 2D view does not follow the scene camera")
	}
	if v := ed.View2D(); v != (editor.View2D{X: 80, Y: 60, Zoom: 1}) {
		t.Fatalf("View2D = %+v, want the camera's (80, 60) at zoom 1", v)
	}
	// Before the canvas lays out it is the virtual size, so the view shows exactly what the camera does.
	for _, p := range [][2]float32{{0, 0}, {40, 40}, {2080, 60}, {-13, 77}} {
		cx, cy := ed.WorldToCanvas(p[0], p[1])
		sx, sy := ed.WorldToScreen(p[0], p[1])
		if !near(cx, sx, 1e-3) || !near(cy, sy, 1e-3) {
			t.Fatalf("WorldToCanvas%v = (%v, %v), want the camera's (%v, %v)", p, cx, cy, sx, sy)
		}
	}

	// A larger canvas centers the camera's frame.
	ed.SetCanvasSize(400, 300)
	if x, y := ed.ScreenToCanvas(0, 0); !near(x, 120, 1e-3) || !near(y, 90, 1e-3) {
		t.Fatalf("the frame's corner is at (%v, %v), want (120, 90)", x, y)
	}

	// A change to the camera moves a following view with it.
	ed.Doc.Camera = &sceneformat.CameraDoc{X: 300, Y: 60, Zoom: sceneformat.Float32(2)}
	if v := ed.View2D(); v.X != 300 || v.Y != 60 || v.Zoom != 2 {
		t.Fatalf("View2D after a camera edit = %+v, want (300, 60) at zoom 2", v)
	}
	camera := *ed.Doc.Camera
	history := ed.History.Len()

	if !ed.PanView2D(-100, 20) {
		t.Fatal("PanView2D refused a pan")
	}
	if ed.View2DFollowsCamera() {
		t.Fatal("the view still follows the camera after a pan")
	}
	// A drag of 100 canvas points to the left at zoom 2 moves the view 50 world units right.
	if v := ed.View2D(); !near(v.X, 350, 1e-3) || !near(v.Y, 50, 1e-3) || v.Zoom != 2 {
		t.Fatalf("View2D after the pan = %+v, want (350, 50) at zoom 2", v)
	}
	if !reflect.DeepEqual(*ed.Doc.Camera, camera) || ed.History.Len() != history {
		t.Fatalf("panning the view changed the scene: camera %+v, %d history entries", *ed.Doc.Camera, ed.History.Len())
	}

	ed.ResetView2D()
	if !ed.View2DFollowsCamera() || ed.View2D().X != 300 {
		t.Fatalf("ResetView2D left %+v", ed.View2D())
	}

	// Opening another scene shows it at its own camera's view.
	ed.PanView2D(10, 10)
	data, err := sceneformat.Encode(farSceneDoc())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := ed.OpenYAML(data); err != nil {
		t.Fatalf("OpenYAML: %v", err)
	}
	if !ed.View2DFollowsCamera() {
		t.Fatal("an opened scene kept the previous scene's view")
	}
}

func TestSetView2DRefusesBadValuesAndClampsZoom(t *testing.T) {
	ed := newFarEditor(t)
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	for _, v := range []editor.View2D{
		{X: nan, Zoom: 1}, {Y: inf, Zoom: 1}, {Zoom: 0}, {Zoom: -1}, {Zoom: nan}, {Zoom: 1, Rotation: inf},
	} {
		if ed.SetView2D(v) {
			t.Fatalf("SetView2D(%+v) was accepted", v)
		}
	}
	if !ed.View2DFollowsCamera() {
		t.Fatal("a refused view detached the view from the camera")
	}
	if !ed.SetView2D(editor.View2D{X: 1, Y: 2, Zoom: 1000}) || ed.View2D().Zoom != editor.MaxView2DZoom {
		t.Fatalf("zoom 1000 became %v, want %v", ed.View2D().Zoom, editor.MaxView2DZoom)
	}
	if ed.ZoomView2DAt(10, 10, 2) {
		t.Fatal("zooming in past the limit reported a change")
	}
	if !ed.SetView2D(editor.View2D{Zoom: 1e-6}) || ed.View2D().Zoom != editor.MinView2DZoom {
		t.Fatalf("zoom 1e-6 became %v, want %v", ed.View2D().Zoom, editor.MinView2DZoom)
	}
	if ed.ZoomView2DAt(10, 10, 0) || ed.ZoomView2DAt(10, 10, -2) || ed.ZoomView2DAt(nan, 10, 2) || ed.PanView2D(nan, 0) || ed.PanView2D(0, 0) {
		t.Fatal("a bad zoom factor or pan was accepted")
	}
}

// A scene camera zoomed past the view's limits gives a clamped view, so the first zoom or pan does not jump the other way.
func TestCameraViewZoomIsClamped(t *testing.T) {
	for _, tc := range []struct {
		camera, want float32
	}{
		{100, editor.MaxView2DZoom},
		{0.005, editor.MinView2DZoom},
	} {
		doc := farSceneDoc()
		doc.Camera.Zoom = &tc.camera
		ed, err := editor.New(doc, testResolver())
		if err != nil {
			t.Fatalf("editor.New: %v", err)
		}
		if z := ed.View2D().Zoom; z != tc.want {
			t.Fatalf("camera zoom %v: View2D().Zoom = %v, want %v", tc.camera, z, tc.want)
		}
		ed.ZoomView2DAt(10, 10, 1.1)
		if z := ed.View2D().Zoom; z < tc.want {
			t.Fatalf("camera zoom %v: zooming in lowered the zoom to %v", tc.camera, z)
		}
		ed.ResetView2D()
		ed.ZoomView2DAt(10, 10, 1/1.1)
		if z := ed.View2D().Zoom; z > tc.want {
			t.Fatalf("camera zoom %v: zooming out raised the zoom to %v", tc.camera, z)
		}
		ed.ResetView2D()
		if !ed.PanView2D(5, 5) || ed.View2D().Zoom != tc.want {
			t.Fatalf("camera zoom %v: a pan changed the zoom to %v", tc.camera, ed.View2D().Zoom)
		}
	}
}

// Play shows the player's frame, the scene's virtual screen, centered on the canvas at the camera's view whatever view the author left, and the view does not move until Stop brings the author's view back.
func TestPlayShowsTheCameraViewWhateverTheFreeView(t *testing.T) {
	ed := newFarEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
	cw, ch := ed.CanvasSize()
	vw, vh := ed.ViewportSize()

	ed.PanView2D(-2000, 0)
	ed.ZoomView2DAt(cw/2, ch/2, 4)
	free := ed.View2D()
	if ed.View2DFollowsCamera() {
		t.Fatal("after a pan and a zoom View2DFollowsCamera reports true")
	}

	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	if !ed.Playing() {
		t.Fatal("the scene is not playing")
	}
	if !ed.View2DFollowsCamera() {
		t.Fatal("while playing View2DFollowsCamera reports false, though the view is the scene camera's")
	}
	playCW, playCH := ed.CanvasSize()
	for _, c := range [][2]float32{{0, 0}, {vw, vh}} {
		x, y := ed.ScreenToCanvas(c[0], c[1])
		if wx, wy := (playCW-vw)/2+c[0], (playCH-vh)/2+c[1]; !near(x, wx, 1e-3) || !near(y, wy, 1e-3) {
			t.Fatalf("while playing the screen's corner (%v, %v) is at canvas (%v, %v), want (%v, %v)", c[0], c[1], x, y, wx, wy)
		}
	}
	playing := ed.View2D()
	app.Wheel("id:editor-viewport-frame", core.Point{Y: 40}, core.Modifiers{})
	app.TouchpadScroll("id:editor-viewport-frame", core.Point{}, core.Point{X: -30})
	if ed.PanView2D(10, 0) || ed.FrameAll2D() || ed.ResetView2D() || ed.View2D() != playing {
		t.Fatalf("the view moved while playing, from %+v to %+v", playing, ed.View2D())
	}

	app.Post(func() {
		if err := ed.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	app.Settle()
	if ed.View2D() != free || ed.View2DFollowsCamera() {
		t.Fatalf("after Stop the view is %+v (following the camera: %v), want the author's %+v", ed.View2D(), ed.View2DFollowsCamera(), free)
	}
}

// canvasBackgroundFilled reports whether d fills the rectangle (0, 0, w, h) with c before its first transform, so in the canvas's own coordinates.
func canvasBackgroundFilled(d *core.Drawing, w, h float32, c core.Color) bool {
	var fill core.Color
	var rect bool
	for _, op := range d.Ops {
		switch op.Kind {
		case core.DrawTransform:
			return false
		case core.DrawFillColor:
			fill = op.Color
		case core.DrawRect:
			rect = op.X == 0 && op.Y == 0 && op.W == w && op.H == h
		case core.DrawFill:
			if rect && fill == c {
				return true
			}
		}
	}
	return false
}

func TestEditModeFillsTheWholeCanvasWithTheSceneBackground(t *testing.T) {
	ed := newFarEditor(t)
	bg, err := core.ParseColor(ed.Doc.BackgroundColor)
	if err != nil {
		t.Fatalf("ParseColor(%q): %v", ed.Doc.BackgroundColor, err)
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
	cw, ch := ed.CanvasSize()
	size := core.Size{Width: core.Points(cw), Height: core.Points(ch)}
	// Pan to the far object so the camera's frame is off the canvas: the canvas is still the scene's background, not the editor's surface.
	ed.PanView2D(-2000, 0)
	if x, _ := ed.ScreenToCanvas(ed.Doc.VirtualWidth, 0); x >= 0 {
		t.Fatalf("the camera frame's right edge is at canvas x %v, want it off the canvas", x)
	}
	d := &core.Drawing{}
	ed.DrawViewport(d, size)
	if !canvasBackgroundFilled(d, cw, ch, bg) {
		t.Fatal("in edit mode the canvas is not filled with the scene's background before the scene is drawn")
	}

	app.Post(func() {
		if err := ed.Play(); err != nil {
			t.Errorf("Play: %v", err)
		}
	})
	app.Settle()
	if !ed.Playing() {
		t.Fatal("the scene is not playing")
	}
	d = &core.Drawing{}
	ed.DrawViewport(d, size)
	if canvasBackgroundFilled(d, cw, ch, bg) {
		t.Fatal("while playing the whole canvas is filled with the scene's background, though only the camera's frame is the game's")
	}
	app.Post(func() {
		if err := ed.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	app.Settle()
}

func TestZoomView2DAtKeepsTheWorldPointUnderThePointer(t *testing.T) {
	ed := newFarEditor(t)
	ed.SetCanvasSize(640, 480)
	for _, rot := range []float32{0, 0.7, -2} {
		for _, p := range [][2]float32{{0, 0}, {320, 240}, {611, 37}, {12.5, 470}} {
			if !ed.SetView2D(editor.View2D{X: 500, Y: -40, Zoom: 1.5, Rotation: rot}) {
				t.Fatal("SetView2D refused a view")
			}
			for _, f := range []float32{2, 0.5, 1.13, 7} {
				wx, wy := ed.CanvasToWorld(p[0], p[1])
				if !ed.ZoomView2DAt(p[0], p[1], f) {
					t.Fatalf("ZoomView2DAt(%v, %v) refused", p, f)
				}
				ax, ay := ed.CanvasToWorld(p[0], p[1])
				if !near(ax, wx, 1e-2) || !near(ay, wy, 1e-2) {
					t.Fatalf("rotation %v: zooming by %v at %v moved the world point under it from (%v, %v) to (%v, %v)", rot, f, p, wx, wy, ax, ay)
				}
				if got := ed.View2D().Rotation; got != rot {
					t.Fatalf("zooming turned the view to %v", got)
				}
			}
		}
	}
}

func TestCanvasAndWorldConversionsRoundTrip(t *testing.T) {
	ed := newFarEditor(t)
	ed.Doc.Camera.Rotation = 0.4
	ed.Doc.Camera.Zoom = sceneformat.Float32(1.5)
	ed.SetCanvasSize(500, 300)
	ed.SetView2D(editor.View2D{X: 1200, Y: 33, Zoom: 0.75, Rotation: -0.3})
	for _, p := range [][2]float32{{0, 0}, {250, 150}, {499, 1}, {-30, 400}} {
		wx, wy := ed.CanvasToWorld(p[0], p[1])
		if x, y := ed.WorldToCanvas(wx, wy); !near(x, p[0], 1e-2) || !near(y, p[1], 1e-2) {
			t.Fatalf("canvas %v -> world -> canvas = (%v, %v)", p, x, y)
		}
		sx, sy := ed.CanvasToScreen(p[0], p[1])
		if x, y := ed.ScreenToCanvas(sx, sy); !near(x, p[0], 1e-2) || !near(y, p[1], 1e-2) {
			t.Fatalf("canvas %v -> screen -> canvas = (%v, %v)", p, x, y)
		}
		if x, y := ed.ScreenToWorld(sx, sy); !near(x, wx, 1e-2) || !near(y, wy, 1e-2) {
			t.Fatalf("screen (%v, %v) is world (%v, %v), want (%v, %v)", sx, sy, x, y, wx, wy)
		}
	}
}

func TestHitTestPlaceAndPaintUseTheView(t *testing.T) {
	ed := newFarEditor(t)
	ed.SetCanvasSize(300, 200)
	ed.SetView2D(editor.View2D{X: 2000, Y: 60, Zoom: 2})

	// The far object, out of the camera's frame, is hit where the view shows it.
	fx, fy := ed.WorldToCanvas(2080, 60)
	if hit := ed.HitTest(fx, fy); hit != "far" {
		t.Fatalf("HitTest at the far object's canvas point = %q, want far", hit)
	}
	if hit := ed.HitTest(fx+20, fy); hit != "" {
		t.Fatalf("HitTest 10 world units right of the far object = %q, want none", hit)
	}

	// Place drops an object at the world point under the press.
	ed.SetTool(editor.ToolPlace)
	px, py := ed.WorldToCanvas(1990, 30)
	ed.HandleCanvasPress(px, py)
	placed, _ := ed.SelectedObject()
	if placed == nil || !near(placed.X, 1990, 1e-3) || !near(placed.Y, 30, 1e-3) {
		t.Fatalf("placed %+v, want at (1990, 30)", placed)
	}

	// Painting finds the tile under the press through the view.
	ed.SetView2D(editor.View2D{X: 32, Y: 32, Zoom: 4})
	ed.SetActiveLayer("floor")
	ed.SetActiveTileID(3)
	ed.SetTool(editor.ToolPaintTile)
	tx, ty := ed.WorldToCanvas(40, 24)
	ed.HandleCanvasPress(tx, ty)
	if got := ed.Doc.Layers[0].Tilemap.Data[1*4+2]; got != 3 {
		t.Fatalf("painted tile (2, 1) = %d, want 3", got)
	}

	// A screen-space layer is hit and placed on in the scene's screen, wherever the view shows its frame.
	ed.Doc.Layers = append(ed.Doc.Layers, sceneformat.LayerDoc{Name: "hud", Space: sceneformat.SpaceScreen, Order: 20})
	ed.Rebuild()
	ed.SetActiveLayer("hud")
	ed.SetTool(editor.ToolPlace)
	ed.SetView2D(editor.View2D{X: 500, Y: 60, Zoom: 0.5})
	hx, hy := ed.ScreenToCanvas(20, 10)
	ed.HandleCanvasPress(hx, hy)
	hud, layer := ed.SelectedObject()
	if layer != "hud" || hud == nil || !near(hud.X, 20, 1e-3) || !near(hud.Y, 10, 1e-3) {
		t.Fatalf("placed %+v on %q, want at screen (20, 10) on hud", hud, layer)
	}
	ed.SelectObject("")
	if hit := ed.HitTest(hx, hy); hit != hud.ID {
		t.Fatalf("HitTest at the hud object = %q, want %q", hit, hud.ID)
	}
}

func TestFrameSelectionAndFrameAll2D(t *testing.T) {
	ed := newFarEditor(t)
	ed.SetCanvasSize(400, 300)

	// Nothing selected frames everything: the stage from the hero to the far object and the camera's frame.
	if !ed.FrameSelection2D() {
		t.Fatal("FrameSelection2D with nothing selected framed nothing")
	}
	all := ed.View2D()
	for _, p := range [][2]float32{{0, 0}, {160, 120}, {2088, 68}, {32, 32}} {
		x, y := ed.WorldToCanvas(p[0], p[1])
		if x < 0 || x > 400 || y < 0 || y > 300 {
			t.Fatalf("framing all leaves world %v at canvas (%v, %v), outside the canvas", p, x, y)
		}
	}
	if !ed.FrameAll2D() || ed.View2D() != all {
		t.Fatalf("FrameAll2D = %+v, want %+v", ed.View2D(), all)
	}

	ed.ResetView2D()
	ed.SelectObject("far")
	if !ed.FrameSelection2D() {
		t.Fatal("FrameSelection2D framed nothing")
	}
	v := ed.View2D()
	// The 16x16 sprite centered on (2080, 60) is centered on the canvas, and the zoom is capped so it does not fill it.
	if !near(v.X, 2080, 1e-3) || !near(v.Y, 60, 1e-3) || v.Zoom != 4 {
		t.Fatalf("framed the selection at %+v, want (2080, 60) at zoom 4", v)
	}
	if hit := ed.HitTest(200, 150); hit != "far" {
		t.Fatalf("the canvas center hits %q, want far", hit)
	}

	empty, err := editor.New(&sceneformat.SceneDoc{Version: sceneformat.CurrentVersion}, nil)
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	// An empty scene still has the camera's frame to show.
	if !empty.FrameAll2D() {
		t.Fatal("FrameAll2D on an empty scene framed nothing")
	}
}

func TestViewportPansZoomsAndFramesThroughMountedUI(t *testing.T) {
	ed := newFarEditor(t)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))

	view := app.Find("id:editor-viewport").Bounds().Size
	if cw, ch := ed.CanvasSize(); cw != float32(view.Width) || ch != float32(view.Height) || cw <= 160 || ch <= 120 {
		t.Fatalf("CanvasSize = %v x %v, want the panel's %v x %v, larger than the scene's 160 x 120", cw, ch, view.Width, view.Height)
	}
	cw, ch := ed.CanvasSize()

	// The wheel zooms about the pointer, which nsgotest puts at the canvas's center.
	wx, wy := ed.CanvasToWorld(cw/2, ch/2)
	app.Wheel("id:editor-viewport-frame", core.Point{Y: 40}, core.Modifiers{})
	if z := ed.View2D().Zoom; z <= 1 {
		t.Fatalf("zoom after a wheel notch up = %v, want above 1", z)
	}
	if x, y := ed.CanvasToWorld(cw/2, ch/2); !near(x, wx, 1e-2) || !near(y, wy, 1e-2) {
		t.Fatalf("the wheel moved the world point under the pointer from (%v, %v) to (%v, %v)", wx, wy, x, y)
	}
	app.Pinch("id:editor-viewport-frame", 1, 1.5, 0.5)
	if x, y := ed.CanvasToWorld(cw/2, ch/2); !near(x, wx, 1e-2) || !near(y, wy, 1e-2) {
		t.Fatalf("the pinch moved the world point under it from (%v, %v) to (%v, %v)", wx, wy, x, y)
	}
	app.Key("mod+0")
	if !ed.View2DFollowsCamera() {
		t.Fatal("Mod+0 did not return to the camera's view")
	}

	// A touchpad scroll pans.
	before := ed.View2D()
	app.TouchpadScroll("id:editor-viewport-frame", core.Point{}, core.Point{X: -30, Y: 10})
	if v := ed.View2D(); !near(v.X, before.X+30, 1e-3) || !near(v.Y, before.Y-10, 1e-3) {
		t.Fatalf("a touchpad scroll moved the view from %+v to %+v", before, v)
	}

	// A middle-button drag pans.
	before = ed.View2D()
	start := core.Point{X: 200, Y: 100}
	app.MouseDownAt("id:editor-viewport-frame", start, core.MouseButtonMiddle)
	app.HoverAt("id:editor-viewport-frame", core.Point{X: 150, Y: 130})
	app.MouseUpAt("id:editor-viewport-frame", core.Point{X: 150, Y: 130}, core.MouseButtonMiddle)
	if v := ed.View2D(); !near(v.X, before.X+50, 1e-3) || !near(v.Y, before.Y-30, 1e-3) {
		t.Fatalf("a middle drag moved the view from %+v to %+v", before, v)
	}

	// A drag with Space held pans; without it, a drag does not.
	before = ed.View2D()
	app.Pan("id:editor-viewport-frame", core.Point{}, core.Point{X: -40, Y: 0})
	if ed.View2D() != before {
		t.Fatalf("a drag without Space moved the view from %+v to %+v", before, ed.View2D())
	}
	app.KeyDown("space")
	app.Pan("id:editor-viewport-frame", core.Point{}, core.Point{X: -40, Y: 0}, core.Point{X: -60, Y: 5})
	app.KeyUp("space")
	if v := ed.View2D(); !near(v.X, before.X+60, 1e-3) || !near(v.Y, before.Y-5, 1e-3) {
		t.Fatalf("a Space-drag moved the view from %+v to %+v", before, v)
	}

	// Navigating with Space held never edits: neither a Space+click nor a Space-drag with the Place tool places an object.
	ed.SetTool(editor.ToolPlace)
	objects, history := len(ed.Doc.Layers[1].Objects), ed.History.Len()
	app.KeyDown("space")
	app.PressAt("id:editor-viewport", core.Point{X: 120, Y: 80})
	app.Pan("id:editor-viewport-frame", core.Point{}, core.Point{X: 20, Y: 0})
	app.KeyUp("space")
	if n, h := len(ed.Doc.Layers[1].Objects), ed.History.Len(); n != objects || h != history {
		t.Fatalf("navigating with Space held left %d objects and %d history entries, want %d and %d", n, h, objects, history)
	}
	ed.SetTool(editor.ToolSelect)

	// A mouse wheel's sideways scroll pans sideways and does not zoom.
	before = ed.View2D()
	app.Wheel("id:editor-viewport-frame", core.Point{X: 40}, core.Modifiers{})
	if v := ed.View2D(); v.Zoom != before.Zoom || !near(v.X, before.X-40/v.Zoom, 1e-3) || v.Y != before.Y {
		t.Fatalf("a sideways wheel moved the view from %+v to %+v, want a pan of 40 points", before, v)
	}

	// F frames the selection, Home everything, and the tool bar does the same.
	ed.SelectObject("far")
	app.Key("f")
	if v := ed.View2D(); !near(v.X, 2080, 1e-3) || !near(v.Y, 60, 1e-3) {
		t.Fatalf("F framed %+v, want the far object", v)
	}
	app.Key("home")
	all := ed.View2D()
	app.Press("id:view-frame-selection")
	if v := ed.View2D(); !near(v.X, 2080, 1e-3) {
		t.Fatalf("the Frame button framed %+v, want the far object", v)
	}
	app.Press("id:view-frame-all")
	if ed.View2D() != all {
		t.Fatalf("the Frame All button framed %+v, want %+v", ed.View2D(), all)
	}
	app.Press("id:view-reset")
	if !ed.View2DFollowsCamera() {
		t.Fatal("the Camera View button did not return to the camera's view")
	}

	// The canvas outlines the camera's frame and its bounds.
	var frame, bounds bool
	fx, fy := ed.ScreenToCanvas(0, 0)
	bx, by := ed.WorldToCanvas(3000, 0)
	for _, op := range app.Find("id:editor-viewport").Ops {
		if op.Kind == core.DrawMoveTo && near(op.X, fx, 1e-3) && near(op.Y, fy, 1e-3) {
			frame = true
		}
		if op.Kind == core.DrawLineTo && near(op.X, bx, 1e-3) && near(op.Y, by, 1e-3) {
			bounds = true
		}
	}
	if !frame || !bounds {
		t.Fatalf("the canvas outlines the camera's frame %v and bounds %v, want both", frame, bounds)
	}
}

// The issue's acceptance: an object 2000 points to the right of the camera's start is reached by panning, selected and moved, and the saved scene's camera is unchanged.
func TestPanToAFarObjectSelectAndMoveKeepsTheSavedCamera(t *testing.T) {
	ed := newFarEditor(t)
	var saved []byte
	ed.OnSave = func(data []byte) error {
		saved = bytes.Clone(data)
		return nil
	}
	camera := *ed.Doc.Camera
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
	cw, _ := ed.CanvasSize()

	if x, _ := ed.WorldToCanvas(2080, 60); x < cw {
		t.Fatalf("the far object is already in view at canvas x %v of %v", x, cw)
	}
	// Pan right by touchpad scrolls until the far object is in view.
	for range 20 {
		if x, _ := ed.WorldToCanvas(2080, 60); x < cw-40 {
			break
		}
		app.TouchpadScroll("id:editor-viewport-frame", core.Point{}, core.Point{X: -200})
	}
	fx, fy := ed.WorldToCanvas(2080, 60)
	if fx < 0 || fx > cw {
		t.Fatalf("panning did not reach the far object: canvas x %v of %v", fx, cw)
	}

	app.Press("id:tool-select")
	app.PressAt("id:editor-viewport", core.Point{X: core.Points(fx), Y: core.Points(fy)})
	if ed.SelectedID != "far" {
		t.Fatalf("SelectedID = %q, want far", ed.SelectedID)
	}

	app.Press("id:tool-move")
	mx, my := ed.WorldToCanvas(2150, 90)
	app.PressAt("id:editor-viewport", core.Point{X: core.Points(mx), Y: core.Points(my)})
	far, _, _ := ed.FindObject("far")
	if !near(far.X, 2150, 1e-2) || !near(far.Y, 90, 1e-2) {
		t.Fatalf("the far object moved to (%v, %v), want (2150, 90)", far.X, far.Y)
	}

	app.Press("id:toolbar-save")
	doc, err := sceneformat.Parse(saved)
	if err != nil {
		t.Fatalf("Parse the saved scene: %v", err)
	}
	if doc.Camera == nil || !reflect.DeepEqual(*doc.Camera, camera) {
		t.Fatalf("saved camera = %+v, want the scene's %+v", doc.Camera, camera)
	}
	if !reflect.DeepEqual(*ed.Doc.Camera, camera) {
		t.Fatalf("the edited camera became %+v, want %+v", *ed.Doc.Camera, camera)
	}

	// The move is the one edit to undo; the view's pans are not edits.
	app.Press("id:toolbar-undo")
	if far, _, _ := ed.FindObject("far"); far.X != 2080 || far.Y != 60 {
		t.Fatalf("Undo left the far object at (%v, %v)", far.X, far.Y)
	}
	if ed.History.CanUndo() {
		t.Fatal("History holds more than the move")
	}
}

func FuzzZoomView2DAtKeepsThePointerFixed(f *testing.F) {
	f.Add(float32(0), float32(0), float32(1), float32(0), float32(10), float32(20), float32(2))
	f.Add(float32(2080), float32(60), float32(0.5), float32(1.2), float32(300), float32(5), float32(0.3))
	f.Fuzz(func(t *testing.T, vx, vy, zoom, rot, px, py, factor float32) {
		ed, err := editor.New(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		ed.SetCanvasSize(640, 360)
		bounded := func(v float32, limit float64) bool { return math.Abs(float64(v)) <= limit }
		if !bounded(vx, 1e5) || !bounded(vy, 1e5) || !bounded(rot, 10) || !bounded(px, 1e4) || !bounded(py, 1e4) ||
			!(zoom >= editor.MinView2DZoom && zoom <= editor.MaxView2DZoom) || !(factor >= 0.01 && factor <= 100) {
			t.Skip()
		}
		if !ed.SetView2D(editor.View2D{X: vx, Y: vy, Zoom: zoom, Rotation: rot}) {
			t.Fatalf("SetView2D refused a finite view")
		}
		wx, wy := ed.CanvasToWorld(px, py)
		if !ed.ZoomView2DAt(px, py, factor) {
			return
		}
		v := ed.View2D()
		if v.Zoom < editor.MinView2DZoom || v.Zoom > editor.MaxView2DZoom {
			t.Fatalf("zoom %v is out of range", v.Zoom)
		}
		ax, ay := ed.CanvasToWorld(px, py)
		// float32 keeps about seven digits of the larger of the view's position and the distance to the pointer.
		tol := float32(1e-3) * max(1, float32(math.Abs(float64(vx)))+float32(math.Abs(float64(vy)))+(float32(math.Abs(float64(px)))+float32(math.Abs(float64(py)))+640)/min(zoom, v.Zoom))
		if !near(ax, wx, tol) || !near(ay, wy, tol) {
			t.Fatalf("zoom %v by %v at (%v, %v) moved the world point under it from (%v, %v) to (%v, %v)", zoom, factor, px, py, wx, wy, ax, ay)
		}
	})
}

func BenchmarkCanvasToWorld(b *testing.B) {
	ed, err := editor.New(farSceneDoc(), testResolver())
	if err != nil {
		b.Fatal(err)
	}
	ed.SetCanvasSize(800, 600)
	ed.SetView2D(editor.View2D{X: 1000, Y: 60, Zoom: 1.5, Rotation: 0.2})
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		ed.CanvasToWorld(float32(i%800), float32(i%600))
	}
}
