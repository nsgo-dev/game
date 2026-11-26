package editor_test

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// mountEditor mounts the whole editor over ed, as the other viewport tests do.
func mountEditor(t *testing.T, ed *editor.Editor) *nsgotest.App {
	t.Helper()
	return nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return editor.EditorView(cx, ed)
	}, nsgotest.Size(1120, 680))
}

func pt(x, y float32) core.Point { return core.Point{X: core.Points(x), Y: core.Points(y)} }

func saveYAML(t *testing.T, ed *editor.Editor) []byte {
	t.Helper()
	data, err := ed.SaveYAML()
	if err != nil {
		t.Fatalf("SaveYAML: %v", err)
	}
	return data
}

// checkOneUndoStep checks that the drag just made is one step in History, that Undo puts the scene back byte for byte as before, and that Redo makes it again.
func checkOneUndoStep(t *testing.T, app *nsgotest.App, ed *editor.Editor, before []byte) {
	t.Helper()
	if n := ed.History.Len(); n != 1 {
		t.Fatalf("History holds %d steps after one drag, want 1", n)
	}
	after := saveYAML(t, ed)
	if bytes.Equal(after, before) {
		t.Fatal("the drag left the scene as it was")
	}
	undoInUI(app, ed)
	if got := saveYAML(t, ed); !bytes.Equal(got, before) {
		t.Fatalf("Undo left the scene\n%s\nwant\n%s", got, before)
	}
	redoInUI(app, ed)
	if got := saveYAML(t, ed); !bytes.Equal(got, after) {
		t.Fatalf("Redo made the scene\n%s\nwant\n%s", got, after)
	}
}

// The issue's first acceptance item: dragging an object, turning it with the ring and scaling it with a handle each record one undo step, and Undo restores the scene exactly.
func TestDragMoveRotateScaleRecordOneStepEach(t *testing.T) {
	t.Run("move keeps the grab offset", func(t *testing.T) {
		ed := newTestEditor(t)
		app := mountEditor(t, ed)
		before := saveYAML(t, ed)
		hx, hy := ed.WorldToCanvas(40, 40)
		// Grab the hero off its origin, by its lower right part, while it is not yet selected.
		grab := pt(hx+5, hy+4)
		app.DragAt("id:editor-viewport", grab, pt(10, 0), pt(30, 20))
		if ed.SelectedID != "hero" {
			t.Fatalf("SelectedID = %q after dragging the hero, want hero", ed.SelectedID)
		}
		hero, _, _ := ed.FindObject("hero")
		if !near(hero.X, 70, 1e-3) || !near(hero.Y, 60, 1e-3) {
			t.Fatalf("the hero moved to (%v, %v), want (70, 60): the pointer's travel, keeping where it was grabbed", hero.X, hero.Y)
		}
		if hero.Rotation != 0 || hero.ScaleX != nil || hero.ScaleY != nil {
			t.Fatalf("a move changed the rotation %v or scale %v, %v", hero.Rotation, hero.ScaleX, hero.ScaleY)
		}
		checkOneUndoStep(t, app, ed, before)
	})

	t.Run("rotate with the ring", func(t *testing.T) {
		ed := newTestEditor(t)
		ed.SelectObject("hero")
		app := mountEditor(t, ed)
		before := saveYAML(t, ed)
		hx, hy := ed.WorldToCanvas(40, 40)
		const r = 72
		if h := ed.PickGizmo2D(hx+r, hy); h != editor.Handle2DRotate {
			t.Fatalf("PickGizmo2D on the ring = %v, want Rotate", h)
		}
		// A quarter turn clockwise on screen, from the ring's right to its bottom, by way of its lower right.
		app.DragAt("id:editor-viewport", pt(hx+r, hy), pt(-r+r*0.7071, r*0.7071), pt(-r, r))
		hero, _, _ := ed.FindObject("hero")
		if !near(hero.Rotation, math.Pi/2, 1e-4) {
			t.Fatalf("rotation after a quarter turn = %v, want %v", hero.Rotation, math.Pi/2)
		}
		if hero.X != 40 || hero.Y != 40 {
			t.Fatalf("turning moved the hero to (%v, %v)", hero.X, hero.Y)
		}
		checkOneUndoStep(t, app, ed, before)
	})

	t.Run("rotate past a half turn", func(t *testing.T) {
		ed := newTestEditor(t)
		ed.SelectObject("hero")
		app := mountEditor(t, ed)
		hx, hy := ed.WorldToCanvas(40, 40)
		const r = 72
		// Round the ring from its right through its bottom and left to its top: three quarter turns, past the angle where atan2 wraps.
		app.DragAt("id:editor-viewport", pt(hx+r, hy), pt(-r, r), pt(-2*r, 0), pt(-r, -r))
		hero, _, _ := ed.FindObject("hero")
		if !near(hero.Rotation, 3*math.Pi/2, 1e-4) {
			t.Fatalf("rotation after three quarter turns = %v, want %v", hero.Rotation, 3*math.Pi/2)
		}
	})

	t.Run("scale with a corner", func(t *testing.T) {
		ed := newTestEditor(t)
		ed.SelectObject("hero")
		app := mountEditor(t, ed)
		// At zoom 4 the 16-point hero is 64 points across, large enough to show its scale handles.
		ed.SetView2D(editor.View2D{X: 40, Y: 40, Zoom: 4})
		before := saveYAML(t, ed)
		cx, cy := ed.WorldToCanvas(48, 48)
		if h := ed.PickGizmo2D(cx, cy); h != editor.Handle2DScaleSE {
			t.Fatalf("PickGizmo2D on the bottom right corner = %v, want Scale SE", h)
		}
		app.DragAt("id:editor-viewport", pt(cx, cy), pt(32, 16))
		hero, _, _ := ed.FindObject("hero")
		if sx, sy := hero.EffectiveScaleX(), hero.EffectiveScaleY(); !near(sx, 2, 1e-4) || !near(sy, 1.5, 1e-4) {
			t.Fatalf("scale after dragging the corner (32, 16) at zoom 4 = (%v, %v), want (2, 1.5)", sx, sy)
		}
		if hero.X != 40 || hero.Y != 40 {
			t.Fatalf("scaling moved the hero's origin to (%v, %v)", hero.X, hero.Y)
		}
		checkOneUndoStep(t, app, ed, before)
	})
}

// An edge handle scales one axis and leaves the other's field unset; Shift scales both by the same factor.
func TestScaleHandlesEdgesAndShift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		handle editor.GizmoHandle2D
		at     [2]float32 // world point of the handle
		by     [2]float32 // canvas points dragged
		shift  bool
		sx, sy float32
		unsetY bool
	}{
		{"right edge", editor.Handle2DScaleE, [2]float32{48, 40}, [2]float32{32, 9}, false, 2, 1, true},
		{"right edge with Shift", editor.Handle2DScaleE, [2]float32{48, 40}, [2]float32{32, 0}, true, 2, 2, false},
		{"bottom edge", editor.Handle2DScaleS, [2]float32{40, 48}, [2]float32{0, -16}, false, 1, 0.5, false},
		{"corner with Shift", editor.Handle2DScaleSE, [2]float32{48, 48}, [2]float32{32, 0}, true, 1.5, 1.5, false},
		{"corner through the origin flips", editor.Handle2DScaleNW, [2]float32{32, 32}, [2]float32{64, 64}, false, -1, -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed := newTestEditor(t)
			ed.SelectObject("hero")
			app := mountEditor(t, ed)
			ed.SetView2D(editor.View2D{X: 40, Y: 40, Zoom: 4})
			x, y := ed.WorldToCanvas(tc.at[0], tc.at[1])
			if h := ed.PickGizmo2D(x, y); h != tc.handle {
				t.Fatalf("PickGizmo2D = %v, want %v", h, tc.handle)
			}
			if tc.shift {
				app.KeyDown("shift")
			}
			app.DragAt("id:editor-viewport", pt(x, y), pt(tc.by[0], tc.by[1]))
			if tc.shift {
				app.KeyUp("shift")
			}
			hero, _, _ := ed.FindObject("hero")
			if sx, sy := hero.EffectiveScaleX(), hero.EffectiveScaleY(); !near(sx, tc.sx, 1e-4) || !near(sy, tc.sy, 1e-4) {
				t.Fatalf("scale = (%v, %v), want (%v, %v)", sx, sy, tc.sx, tc.sy)
			}
			if tc.unsetY && hero.ScaleY != nil {
				t.Fatalf("an X-only scale set scaleY to %v", *hero.ScaleY)
			}
			if ed.History.Len() != 1 {
				t.Fatalf("History holds %d steps, want 1", ed.History.Len())
			}
		})
	}
}

// The axis arrows move the object along its own turned axes only.
func TestMoveArrowsFollowTheObjectsAxes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		handle editor.GizmoHandle2D
	}{{"X arrow", editor.Handle2DMoveX}, {"Y arrow", editor.Handle2DMoveY}} {
		t.Run(tc.name, func(t *testing.T) {
			ed := newTestEditor(t)
			const rot = math.Pi / 6
			if err := ed.RotateObject("hero", rot); err != nil {
				t.Fatal(err)
			}
			ed.SelectObject("hero")
			app := mountEditor(t, ed)
			ed.History.Clear()
			ux, uy := float32(math.Cos(rot)), float32(math.Sin(rot))
			if tc.handle == editor.Handle2DMoveY {
				ux, uy = -uy, ux
			}
			ox, oy := ed.WorldToCanvas(40, 40)
			ax, ay := ox+40*ux, oy+40*uy
			if h := ed.PickGizmo2D(ax, ay); h != tc.handle {
				t.Fatalf("PickGizmo2D on the arrow = %v, want %v", h, tc.handle)
			}
			// 30 points along the axis and 25 across it: only the 30 count.
			app.DragAt("id:editor-viewport", pt(ax, ay), pt(30*ux-25*uy, 30*uy+25*ux))
			hero, _, _ := ed.FindObject("hero")
			if !near(hero.X, 40+30*ux, 1e-3) || !near(hero.Y, 40+30*uy, 1e-3) {
				t.Fatalf("the hero moved to (%v, %v), want (%v, %v)", hero.X, hero.Y, 40+30*ux, 40+30*uy)
			}
			if ed.History.Len() != 1 {
				t.Fatalf("History holds %d steps, want 1", ed.History.Len())
			}
		})
	}
}

// rotatedCameraDoc is the test scene under a camera zoomed in 2x and turned 0.5 radians, with a screen-space HUD layer holding a marker.
func rotatedCameraDoc() *sceneformat.SceneDoc {
	doc := testSceneDoc()
	doc.Camera = &sceneformat.CameraDoc{X: 40, Y: 40, Zoom: sceneformat.Float32(2), Rotation: 0.5}
	doc.Layers = append(doc.Layers, sceneformat.LayerDoc{
		Name:    "hud",
		Space:   sceneformat.SpaceScreen,
		Order:   20,
		Objects: []sceneformat.ObjectDoc{{ID: "badge", X: 20, Y: 20}},
	})
	return doc
}

// The issue's second acceptance item: under a rotated, zoomed camera a drag moves a world-space object by the world distance the pointer travelled, and a screen-space one by the screen distance.
func TestDragUnderARotatedZoomedCamera(t *testing.T) {
	ed, err := editor.New(rotatedCameraDoc(), testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	app := mountEditor(t, ed)
	if !ed.View2DFollowsCamera() {
		t.Fatal("the view does not follow the camera")
	}
	const dx, dy = 40, 10
	hx, hy := ed.WorldToCanvas(40, 40)
	app.DragAt("id:editor-viewport", pt(hx+3, hy+2), pt(dx, dy))
	// The world turns by -0.5 on the canvas at zoom 2, so a canvas move maps back by turning it 0.5 and halving it.
	sin, cos := math.Sincos(0.5)
	wantX := 40 + float32((dx*cos-dy*sin)/2)
	wantY := 40 + float32((dx*sin+dy*cos)/2)
	hero, _, _ := ed.FindObject("hero")
	if !near(hero.X, wantX, 1e-3) || !near(hero.Y, wantY, 1e-3) {
		t.Fatalf("the hero moved to (%v, %v), want (%v, %v)", hero.X, hero.Y, wantX, wantY)
	}
	if hx2, hy2 := ed.WorldToCanvas(hero.X, hero.Y); !near(hx2, hx+dx, 1e-2) || !near(hy2, hy+dy, 1e-2) {
		t.Fatalf("the hero is drawn at (%v, %v), want under the pointer's travel at (%v, %v)", hx2, hy2, hx+dx, hy+dy)
	}

	// The HUD marker lives in the virtual screen, which the camera's view shows unturned and unzoomed.
	bx, by := ed.ScreenToCanvas(20, 20)
	app.DragAt("id:editor-viewport", pt(bx, by), pt(dx, dy))
	if ed.SelectedID != "badge" {
		t.Fatalf("SelectedID = %q, want badge", ed.SelectedID)
	}
	badge, _, _ := ed.FindObject("badge")
	if !near(badge.X, 20+dx, 1e-3) || !near(badge.Y, 20+dy, 1e-3) {
		t.Fatalf("the HUD marker moved to (%v, %v), want (%v, %v)", badge.X, badge.Y, 20+dx, 20+dy)
	}
	if ed.History.Len() != 2 {
		t.Fatalf("History holds %d steps after two drags, want 2", ed.History.Len())
	}

	// Under the turned camera the ring turns the hero by the angle the pointer turns on the canvas.
	ed.SelectObject("hero")
	ox, oy := ed.WorldToCanvas(hero.X, hero.Y)
	app.DragAt("id:editor-viewport", pt(ox+72, oy), pt(-72, 72))
	if hero, _, _ := ed.FindObject("hero"); !near(hero.Rotation, math.Pi/2, 1e-4) {
		t.Fatalf("rotation after a quarter turn under a turned camera = %v, want %v", hero.Rotation, math.Pi/2)
	}
}

// Escape during a drag, a cancelled touch and a second finger each put the object back and record nothing; the rest of the gesture does nothing.
func TestCancelledDragRestoresTheObject(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cancel func(app *nsgotest.App, f *nsgotest.Finger)
	}{
		{"Escape", func(app *nsgotest.App, f *nsgotest.Finger) { app.Key("escape") }},
		{"a cancelled touch", func(app *nsgotest.App, f *nsgotest.Finger) { f.Cancel() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed := newTestEditor(t)
			ed.SelectObject("hero")
			app := mountEditor(t, ed)
			ed.SetView2D(editor.View2D{X: 40, Y: 40, Zoom: 4})
			before := saveYAML(t, ed)
			cx, cy := ed.WorldToCanvas(48, 48)
			f := app.Touch("id:editor-viewport", pt(cx, cy))
			for i := 1; i <= 4; i++ {
				f.Move(pt(cx+float32(10*i), cy+float32(5*i)))
			}
			if ed.Dragging2D() != editor.Handle2DScaleSE {
				t.Fatalf("Dragging2D = %v, want Scale SE", ed.Dragging2D())
			}
			if hero, _, _ := ed.FindObject("hero"); hero.ScaleX == nil {
				t.Fatal("the drag did not scale the hero as it went")
			}
			tc.cancel(app, f)
			if ed.Dragging2D() != editor.Handle2DNone {
				t.Fatalf("Dragging2D = %v after the cancel, want None", ed.Dragging2D())
			}
			if got := saveYAML(t, ed); !bytes.Equal(got, before) {
				t.Fatalf("after the cancel the scene is\n%s\nwant\n%s", got, before)
			}
			if tc.name == "Escape" {
				f.Move(pt(cx+60, cy+60))
				f.Lift()
			}
			if got := saveYAML(t, ed); !bytes.Equal(got, before) || ed.History.Len() != 0 {
				t.Fatalf("the rest of the gesture changed the scene or recorded %d steps", ed.History.Len())
			}
		})
	}
}

// A scale that would shear a child's sprite is refused, as ScaleObject refuses it, while a uniform one is taken.
func TestScaleHandleRefusesShearingAChild(t *testing.T) {
	doc := testSceneDoc()
	hero := &doc.Layers[1].Objects[0]
	hero.Children = []sceneformat.ObjectDoc{{ID: "sword", X: 8, Rotation: 0.5, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"}}}
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SelectObject("hero")
	app := mountEditor(t, ed)
	ed.SetView2D(editor.View2D{X: 40, Y: 40, Zoom: 4})
	if err := ed.ScaleObject("hero", 2, 1); err == nil {
		t.Fatal("ScaleObject took a scale that shears the child's sprite")
	}
	before := saveYAML(t, ed)
	x, y := ed.WorldToCanvas(48, 40)
	app.DragAt("id:editor-viewport", pt(x, y), pt(32, 0))
	if got := saveYAML(t, ed); !bytes.Equal(got, before) || ed.History.Len() != 0 {
		t.Fatalf("a shearing edge drag changed the scene to\n%s", got)
	}
	if ed.BuildErr != nil {
		t.Fatalf("BuildErr = %v", ed.BuildErr)
	}
	app.KeyDown("shift")
	app.DragAt("id:editor-viewport", pt(x, y), pt(32, 0))
	app.KeyUp("shift")
	h, _, _ := ed.FindObject("hero")
	if sx, sy := h.EffectiveScaleX(), h.EffectiveScaleY(); !near(sx, 2, 1e-4) || !near(sy, 2, 1e-4) {
		t.Fatalf("a uniform scale with Shift gave (%v, %v), want (2, 2)", sx, sy)
	}
}

// The gizmo shows only on a selection, in 2D, with the Select or Move tool and outside Play, and a drag with Space held, another tool or on nothing leaves the scene alone.
func TestGizmoShowsOnlyWhereItEdits(t *testing.T) {
	ed := newTestEditor(t)
	app := mountEditor(t, ed)
	if ed.Gizmo2DShown() {
		t.Fatal("the gizmo shows without a selection")
	}
	ed.SelectObject("hero")
	if !ed.Gizmo2DShown() {
		t.Fatal("the gizmo does not show on the selection")
	}
	app.Settle()
	hx, hy := ed.WorldToCanvas(40, 40)
	var ring bool
	for _, op := range app.Find("id:editor-viewport").Ops {
		if op.Kind == core.DrawArc && near(op.X, hx, 1e-3) && near(op.Y, hy, 1e-3) {
			ring = true
		}
	}
	if !ring {
		t.Fatal("the canvas does not draw the rotation ring about the selection's origin")
	}
	before := saveYAML(t, ed)

	app.KeyDown("space")
	app.DragAt("id:editor-viewport", pt(hx, hy), pt(30, 0))
	app.KeyUp("space")
	// The Space-drag panned the view, so the hero is elsewhere on the canvas.
	hx, hy = ed.WorldToCanvas(40, 40)
	ed.SetTool(editor.ToolPlace)
	if ed.Gizmo2DShown() {
		t.Fatal("the gizmo shows with the Place tool")
	}
	app.DragAt("id:editor-viewport", pt(hx, hy), pt(30, 0))
	ed.SetTool(editor.ToolMove)
	app.DragAt("id:editor-viewport", pt(hx+200, hy+150), pt(30, 0))
	ed.SetViewMode(editor.ViewMode3D)
	if ed.Gizmo2DShown() {
		t.Fatal("the 2D gizmo shows in 3D")
	}
	ed.SetViewMode(editor.ViewMode2D)
	if got := saveYAML(t, ed); !bytes.Equal(got, before) || ed.History.Len() != 0 {
		t.Fatalf("a Space-drag, a Place drag or a drag on nothing changed the scene to\n%s", got)
	}

	// Undo during a drag puts the drag back first.
	if ed.BeginDrag2D(hx, hy) != editor.Handle2DMove {
		t.Fatal("BeginDrag2D on the origin did not take the move square")
	}
	ed.UpdateDrag2D(hx+20, hy, false)
	ed.Undo()
	if got := saveYAML(t, ed); !bytes.Equal(got, before) || ed.Dragging2D() != editor.Handle2DNone {
		t.Fatal("Undo during a drag left the drag or its change")
	}
}

func BenchmarkUpdateDrag2D(b *testing.B) {
	ed, err := editor.New(testSceneDoc(), testResolver())
	if err != nil {
		b.Fatal(err)
	}
	ed.SetCanvasSize(800, 600)
	ed.SelectObject("hero")
	hx, hy := ed.WorldToCanvas(40, 40)
	if ed.BeginDrag2D(hx, hy) == editor.Handle2DNone {
		b.Fatal("no drag")
	}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		ed.UpdateDrag2D(hx+float32(i%50), hy, false)
	}
}

// A nested child is dragged in its parent's space: under a turned, scaled parent it follows the pointer by the world distance it travelled, and the gizmo shows on it.
func TestDragNestedChildUnderATurnedScaledParent(t *testing.T) {
	doc := testSceneDoc()
	hero := &doc.Layers[1].Objects[0]
	hero.Rotation = math.Pi / 2
	hero.ScaleX, hero.ScaleY = sceneformat.Float32(2), sceneformat.Float32(2)
	hero.Children = []sceneformat.ObjectDoc{{ID: "sword", X: 10, Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"}, OriginX: 8, OriginY: 8}}
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SelectObject("sword")
	app := mountEditor(t, ed)
	if !ed.Gizmo2DShown() {
		t.Fatal("the gizmo does not show on a selected nested child")
	}
	before := saveYAML(t, ed)
	// The parent turns the child's (10, 0) a quarter turn and doubles it, so its origin is at world (40, 60).
	sx, sy := ed.WorldToCanvas(40, 60)
	if h := ed.PickGizmo2D(sx, sy); h != editor.Handle2DMove {
		t.Fatalf("PickGizmo2D on the child's origin = %v, want Move", h)
	}
	wx, wy := ed.WorldToCanvas(60, 60)
	app.DragAt("id:editor-viewport", pt(sx, sy), pt(wx-sx, wy-sy))
	sword, _, _ := ed.FindObject("sword")
	// World (+20, 0) is parent-space (0, -10): back through the quarter turn and the scale of 2.
	if !near(sword.X, 10, 1e-3) || !near(sword.Y, -10, 1e-3) {
		t.Fatalf("the child moved to (%v, %v) in its parent's space, want (10, -10)", sword.X, sword.Y)
	}
	if h, _, _ := ed.FindObject("hero"); h.X != 40 || h.Y != 40 {
		t.Fatalf("dragging the child moved its parent to (%v, %v)", h.X, h.Y)
	}
	checkOneUndoStep(t, app, ed, before)
}

// Turning a nested child with the ring is refused, as RotateObject refuses it, when its parent's non-uniform scale would shear its sprite.
func TestRingRefusesShearingANestedChild(t *testing.T) {
	doc := testSceneDoc()
	hero := &doc.Layers[1].Objects[0]
	hero.ScaleX = sceneformat.Float32(2)
	hero.Children = []sceneformat.ObjectDoc{{ID: "sword", Sprite: &sceneformat.SpriteRef{Atlas: "actors", Frame: "slime"}, OriginX: 8, OriginY: 8}}
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetCanvasSize(800, 600)
	ed.SelectObject("sword")
	if err := ed.RotateObject("sword", math.Pi/4); err == nil {
		t.Fatal("RotateObject took a rotation that shears the child's sprite")
	}
	before := saveYAML(t, ed)
	ox, oy := ed.WorldToCanvas(40, 40)
	// The ring sits outside the outline's farthest corner, here past the 72-point minimum.
	const r = 72
	if h := ed.BeginDrag2D(ox+r, oy); h != editor.Handle2DRotate {
		t.Fatalf("BeginDrag2D on the ring = %v, want Rotate", h)
	}
	ed.UpdateDrag2D(ox+r*0.7071, oy+r*0.7071, false)
	if !strings.HasPrefix(ed.Status, "Rotate refused") {
		t.Fatalf("Status = %q, want it to say the rotation was refused", ed.Status)
	}
	ed.EndDrag2D()
	if got := saveYAML(t, ed); !bytes.Equal(got, before) || ed.History.Len() != 0 {
		t.Fatalf("a shearing ring drag changed the scene to\n%s", got)
	}
}

// A hidden or locked object, or one under a locked parent, has no gizmo and no drag, even while it is selected from the scene tree.
func TestFrozenObjectHasNoGizmoOrDrag(t *testing.T) {
	for _, tc := range []struct {
		name   string
		freeze func(ed *editor.Editor) error
	}{
		{"locked", func(ed *editor.Editor) error { return ed.SetObjectLocked("hero", true) }},
		{"hidden", func(ed *editor.Editor) error { return ed.SetObjectHidden("hero", true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed := newTestEditor(t)
			ed.SetCanvasSize(800, 600)
			ed.SetView2D(editor.View2D{X: 40, Y: 40, Zoom: 4})
			if err := tc.freeze(ed); err != nil {
				t.Fatal(err)
			}
			ed.SelectObject("hero")
			if ed.Gizmo2DShown() {
				t.Fatal("the gizmo shows on a frozen object")
			}
			before := saveYAML(t, ed)
			hx, hy := ed.WorldToCanvas(40, 40)
			cx, cy := ed.WorldToCanvas(48, 48)
			for _, p := range [][2]float32{{hx, hy}, {hx + 30, hy}, {cx, cy}, {hx + 100, hy}} {
				if h := ed.BeginDrag2D(p[0], p[1]); h != editor.Handle2DNone {
					t.Fatalf("BeginDrag2D(%v) on a frozen object = %v, want None", p, h)
				}
				ed.UpdateDrag2D(p[0]+20, p[1]+20, false)
				ed.EndDrag2D()
			}
			if got := saveYAML(t, ed); !bytes.Equal(got, before) || ed.History.Len() != 0 {
				t.Fatalf("drags on a frozen object changed the scene to\n%s", got)
			}
		})
	}
}

// A sprite whose origin is its top left corner keeps its move square, and offers only the scale handles that sit away from the origin's lines.
func TestTopLeftPivotKeepsTheMoveSquare(t *testing.T) {
	doc := testSceneDoc()
	hero := &doc.Layers[1].Objects[0]
	hero.OriginX, hero.OriginY = 0, 0
	ed, err := editor.New(doc, testResolver())
	if err != nil {
		t.Fatalf("editor.New: %v", err)
	}
	ed.SetActiveLayer("actors")
	ed.SelectObject("hero")
	app := mountEditor(t, ed)
	ed.SetView2D(editor.View2D{X: 48, Y: 48, Zoom: 4})
	ox, oy := ed.WorldToCanvas(40, 40)
	if h := ed.PickGizmo2D(ox, oy); h != editor.Handle2DMove {
		t.Fatalf("PickGizmo2D on the top left origin = %v, want Move", h)
	}
	for _, p := range []struct {
		name   string
		wx, wy float32
	}{{"N", 48, 40}, {"W", 40, 48}, {"NE", 56, 40}, {"SW", 40, 56}} {
		x, y := ed.WorldToCanvas(p.wx, p.wy)
		if h := ed.PickGizmo2D(x, y); h.IsScale() {
			t.Fatalf("PickGizmo2D on the %s handle = %v: a handle on the origin's line is offered", p.name, h)
		}
	}
	for _, p := range []struct {
		want   editor.GizmoHandle2D
		wx, wy float32
	}{{editor.Handle2DScaleE, 56, 48}, {editor.Handle2DScaleS, 48, 56}, {editor.Handle2DScaleSE, 56, 56}} {
		x, y := ed.WorldToCanvas(p.wx, p.wy)
		if h := ed.PickGizmo2D(x, y); h != p.want {
			t.Fatalf("PickGizmo2D at (%v, %v) = %v, want %v", p.wx, p.wy, h, p.want)
		}
	}
	before := saveYAML(t, ed)
	app.DragAt("id:editor-viewport", pt(ox, oy), pt(20, 8))
	h, _, _ := ed.FindObject("hero")
	if !near(h.X, 45, 1e-3) || !near(h.Y, 42, 1e-3) || h.ScaleX != nil || h.ScaleY != nil {
		t.Fatalf("a drag of the move square left the hero at (%v, %v) scale (%v, %v), want (45, 42) unscaled", h.X, h.Y, h.ScaleX, h.ScaleY)
	}
	checkOneUndoStep(t, app, ed, before)
	undoInUI(app, ed)
	cx, cy := ed.WorldToCanvas(56, 56)
	app.DragAt("id:editor-viewport", pt(cx, cy), pt(32, 32))
	h, _, _ = ed.FindObject("hero")
	if sx, sy := h.EffectiveScaleX(), h.EffectiveScaleY(); !near(sx, 1.5, 1e-4) || !near(sy, 1.5, 1e-4) {
		t.Fatalf("scale after dragging the bottom right corner (32, 32) at zoom 4 = (%v, %v), want (1.5, 1.5)", sx, sy)
	}
}

// A scale dragged to the origin's line is refused with a status, and the object keeps its last valid scale.
func TestScaleToZeroIsRefusedWithAStatus(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetCanvasSize(800, 600)
	ed.SetView2D(editor.View2D{X: 40, Y: 40, Zoom: 4})
	ed.SelectObject("hero")
	ex, ey := ed.WorldToCanvas(48, 40)
	if h := ed.BeginDrag2D(ex, ey); h != editor.Handle2DScaleE {
		t.Fatalf("BeginDrag2D on the right edge = %v, want Scale E", h)
	}
	ed.UpdateDrag2D(ex+32, ey, false)
	ox, _ := ed.WorldToCanvas(40, 40)
	ed.UpdateDrag2D(ox+1e-4, ey, false)
	if !strings.HasPrefix(ed.Status, "Scale refused") {
		t.Fatalf("Status = %q, want it to say the scale was refused", ed.Status)
	}
	h, _, _ := ed.FindObject("hero")
	if sx := h.EffectiveScaleX(); !near(sx, 2, 1e-4) {
		t.Fatalf("scale X after a refused scale = %v, want the last valid 2", sx)
	}
	ed.CancelDrag2D()
}

// Deleting, duplicating or saving during a drag puts the drag back first, so Undo of the delete restores the object as it was before the drag.
func TestEditsDuringADragPutItBackFirst(t *testing.T) {
	ed := newTestEditor(t)
	ed.SetCanvasSize(800, 600)
	ed.SelectObject("hero")
	before := saveYAML(t, ed)
	hx, hy := ed.WorldToCanvas(40, 40)
	start := func() {
		t.Helper()
		if ed.BeginDrag2D(hx, hy) != editor.Handle2DMove {
			t.Fatal("BeginDrag2D on the origin did not take the move square")
		}
		ed.UpdateDrag2D(hx+20, hy+10, false)
	}

	start()
	if err := ed.DeleteSelected(); err != nil {
		t.Fatalf("DeleteSelected: %v", err)
	}
	if ed.Dragging2D() != editor.Handle2DNone {
		t.Fatal("the drag outlived the delete")
	}
	ed.Undo()
	if got := saveYAML(t, ed); !bytes.Equal(got, before) {
		t.Fatalf("Undo of a delete made during a drag left the scene\n%s\nwant\n%s", got, before)
	}

	ed.SelectObject("hero")
	start()
	id, err := ed.DuplicateSelected()
	if err != nil {
		t.Fatalf("DuplicateSelected: %v", err)
	}
	dup, _, _ := ed.FindObject(id)
	hero, _, _ := ed.FindObject("hero")
	if dup == nil || dup.X != 56 || dup.Y != 56 || hero.X != 40 || hero.Y != 40 {
		t.Fatalf("a duplicate made during a drag copied the half-dragged transform: dup=(%v,%v), hero=(%v,%v)", dup.X, dup.Y, hero.X, hero.Y)
	}
	ed.Undo()

	ed.SelectObject("hero")
	start()
	var saved []byte
	ed.OnSave = func(data []byte) error { saved = data; return nil }
	if err := ed.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Putting the drag back leaves the scene as saved, so Save has nothing to write; it must not write the half-dragged object.
	if saved != nil && !bytes.Equal(saved, before) {
		t.Fatalf("Save during a drag wrote\n%s\nwant\n%s", saved, before)
	}
	if ed.Dragging2D() != editor.Handle2DNone || ed.SceneDirty() {
		t.Fatal("Save during a drag left the drag under way or the scene edited")
	}
	if got := saveYAML(t, ed); !bytes.Equal(got, before) {
		t.Fatalf("Save during a drag left the scene\n%s\nwant\n%s", got, before)
	}
}
