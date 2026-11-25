package editor

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

func gizmoTestEditor(t *testing.T) *Editor {
	t.Helper()
	doc := DefaultSceneDoc()
	doc.Layers[0].Objects = []sceneformat.ObjectDoc{{ID: "box", X: 100, Y: 50}}
	ed, err := New(doc, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ed.SetCanvasSize(640, 360)
	return ed
}

// A pan that the system cancels puts the drag back, and its begin starts the drag where the pointer went down, before the slop.
func TestViewportPanBeginsWhereThePointerWentDownAndCancels(t *testing.T) {
	ed := gizmoTestEditor(t)
	no := func() bool { return false }
	bx, by := ed.WorldToCanvas(100, 50)
	var g viewportGestures
	g.pan(ed, core.PanEvent{Phase: core.GestureBegan, Translation: core.Point{X: 12}, Position: core.Point{X: core.Points(bx + 12), Y: core.Points(by)}}, no, no)
	if ed.Dragging2D() != Handle2DBody || ed.SelectedID != "box" {
		t.Fatalf("a pan begun on the box holds %v with %q selected, want the box's body", ed.Dragging2D(), ed.SelectedID)
	}
	if o, _, _ := ed.FindObject("box"); o.X != 112 || o.Y != 50 {
		t.Fatalf("the box is at (%v, %v) as the pan begins, want (112, 50)", o.X, o.Y)
	}
	g.pan(ed, core.PanEvent{Phase: core.GestureChanged, Translation: core.Point{X: 30}, Position: core.Point{X: core.Points(bx + 30), Y: core.Points(by)}}, no, no)
	g.pan(ed, core.PanEvent{Phase: core.GestureCancelled}, no, no)
	if o, _, _ := ed.FindObject("box"); o.X != 100 || o.Y != 50 || ed.History.Len() != 0 || ed.Dragging2D() != Handle2DNone {
		t.Fatalf("after a cancelled pan the box is at (%v, %v) with %d steps", o.X, o.Y, ed.History.Len())
	}

	// A second finger cancels it too.
	g = viewportGestures{}
	g.pan(ed, core.PanEvent{Phase: core.GestureBegan, Translation: core.Point{X: 12}, Position: core.Point{X: core.Points(bx + 12), Y: core.Points(by)}}, no, no)
	g.touch(ed, core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{{ID: 1}, {ID: 2, Position: core.Point{X: 50}}}})
	if o, _, _ := ed.FindObject("box"); o.X != 100 || ed.Dragging2D() != Handle2DNone {
		t.Fatalf("a second finger left the box at %v, dragging %v", o.X, ed.Dragging2D())
	}
}

// A drag that changes nothing records nothing, and the drag's step is labeled by what it did.
func TestDrag2DRecordsNothingWhenUnchanged(t *testing.T) {
	ed := gizmoTestEditor(t)
	bx, by := ed.WorldToCanvas(100, 50)
	if ed.BeginDrag2D(bx, by) != Handle2DBody {
		t.Fatal("no drag on the box")
	}
	ed.UpdateDrag2D(bx+10, by, false)
	ed.UpdateDrag2D(bx, by, false)
	if ed.EndDrag2D() || ed.History.Len() != 0 {
		t.Fatal("a drag back to where it began recorded a step")
	}
	ed.BeginDrag2D(bx, by)
	ed.UpdateDrag2D(bx+10, by, false)
	if !ed.EndDrag2D() || ed.History.UndoLabel() != "Move box" {
		t.Fatalf("the drag's step is %q, want Move box", ed.History.UndoLabel())
	}
	if ed.BeginDrag2D(math.Float32frombits(0x7fc00000), by) != Handle2DNone {
		t.Fatal("a drag began at NaN")
	}
}

// scaleFromDrag keeps the grabbed point under the pointer: the scaled outline puts the grab point where the pointer is, along each axis the handle moves.
func FuzzScaleFromDragFollowsThePointer(f *testing.F) {
	f.Add(float32(0), float32(1), float32(1), float32(8), float32(8), float32(16), float32(4), false)
	f.Add(float32(0.7), float32(2), float32(0.5), float32(-8), float32(3), float32(-20), float32(9), true)
	f.Fuzz(func(t *testing.T, rot, sx0, sy0, gx, gy, px, py float32, keep bool) {
		bounded := func(v, lim float32) bool { return v >= -lim && v <= lim }
		if !bounded(rot, 10) || !bounded(gx, 1e3) || !bounded(gy, 1e3) || !bounded(px, 1e3) || !bounded(py, 1e3) ||
			!(math.Abs(float64(sx0)) >= 0.01 && math.Abs(float64(sx0)) <= 100) || !(math.Abs(float64(sy0)) >= 0.01 && math.Abs(float64(sy0)) <= 100) {
			t.Skip()
		}
		s := transform2D{X: 3, Y: -4, Rotation: rot, ScaleX: &sx0, ScaleY: &sy0}
		turn := math2d.Rotate(rot)
		toLayer := func(x, y float32) math2d.Vec2 {
			v := turn.TransformVector(math2d.Vec2{X: x, Y: y})
			return math2d.Vec2{X: v.X + s.X, Y: v.Y + s.Y}
		}
		grab := toLayer(gx, gy)
		p := toLayer(px, py)
		for _, h := range []GizmoHandle2D{Handle2DScaleE, Handle2DScaleS, Handle2DScaleSE} {
			sx, sy, ok := scaleFromDrag(h, s, grab, float64(p.X), float64(p.Y), keep)
			if !ok {
				continue
			}
			if sx == nil || sy == nil || *sx == 0 || *sy == 0 || !isFiniteFloat32(*sx) || !isFiniteFloat32(*sy) {
				t.Fatalf("%v gave scale %v, %v", h, sx, sy)
			}
			if keep {
				if r0, r1 := float64(sx0/sy0), float64(*sx / *sy); math.Abs(r0-r1) > 1e-3*math.Max(1, math.Abs(r0)) {
					t.Fatalf("%v with Shift changed the aspect ratio from %v to %v", h, r0, r1)
				}
				continue
			}
			// Off the origin's line, the grab point scaled by the new over the old scale lands on the pointer along the scaled axis.
			tol := float32(1e-2) * max(1, abs32(px), abs32(py))
			if h != Handle2DScaleS && abs32(gx) > 0.1 && !near32(gx**sx/sx0, px, tol) {
				t.Fatalf("%v put the grab point's x at %v, want the pointer's %v", h, gx**sx/sx0, px)
			}
			if h != Handle2DScaleE && abs32(gy) > 0.1 && !near32(gy**sy/sy0, py, tol) {
				t.Fatalf("%v put the grab point's y at %v, want the pointer's %v", h, gy**sy/sy0, py)
			}
			if h == Handle2DScaleE && *sy != sy0 || h == Handle2DScaleS && *sx != sx0 {
				t.Fatalf("%v scaled the other axis: (%v, %v) from (%v, %v)", h, *sx, *sy, sx0, sy0)
			}
		}
	})
}

func near32(a, b, tol float32) bool { return abs32(a-b) <= tol }
