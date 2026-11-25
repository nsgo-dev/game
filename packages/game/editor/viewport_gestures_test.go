package editor

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

// The wheel handler sorts a scroll into a pan or a zoom by its source and modifiers. nsgotest's TouchpadScroll takes no modifiers, so the Control and Command cases are driven here rather than through the mounted viewport.
func TestViewportWheelPansOrZooms(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ev       core.WheelEvent
		pan      bool
		zoomedIn bool
	}{
		{"touchpad scroll pans", core.WheelEvent{Touchpad: true, Delta: core.Point{X: 30, Y: 20}}, true, false},
		{"touchpad scroll with Control zooms", core.WheelEvent{Touchpad: true, Delta: core.Point{Y: 40}, Modifiers: core.Modifiers{Control: true}}, false, true},
		{"touchpad scroll with Command zooms", core.WheelEvent{Touchpad: true, Delta: core.Point{Y: 40}, Modifiers: core.Modifiers{Platform: true}}, false, true},
		{"mouse wheel zooms", core.WheelEvent{Delta: core.Point{Y: 40}}, false, true},
		{"mouse wheel sideways pans", core.WheelEvent{Delta: core.Point{X: 40}}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed, err := New(nil, nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			ed.SetCanvasSize(640, 480)
			tc.ev.Position = core.Point{X: 320, Y: 240}
			before := ed.View2D()
			var g viewportGestures
			g.wheel(ed, tc.ev)
			v := ed.View2D()
			if tc.pan {
				wantX, wantY := before.X-float32(tc.ev.Delta.X)/v.Zoom, before.Y-float32(tc.ev.Delta.Y)/v.Zoom
				if v.Zoom != before.Zoom || v.X != wantX || v.Y != wantY {
					t.Fatalf("view %+v after %+v, want a pan to (%v, %v) at zoom %v", v, before, wantX, wantY, before.Zoom)
				}
			}
			if tc.zoomedIn && v.Zoom <= before.Zoom {
				t.Fatalf("zoom %v after %v, want it raised", v.Zoom, before.Zoom)
			}
		})
	}
}

func twoFingers(phase core.TouchPhase, a, b core.Point) core.TouchEvent {
	return core.TouchEvent{Phase: phase, Touches: []core.Touch{{ID: 1, Position: a}, {ID: 2, Position: b}}}
}

// Two fingers pan the 2D view by the movement of their center; a third finger, a lifted finger or a cancel ends the pan, and the next two-finger event starts a new one rather than jumping by the distance moved in between.
func TestViewportTwoFingerTouchPans(t *testing.T) {
	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ed.SetCanvasSize(640, 480)
	var g viewportGestures
	start := ed.View2D()
	panned := func(dx, dy float32) View2D {
		v := ed.View2D()
		return View2D{X: v.X - dx/v.Zoom, Y: v.Y - dy/v.Zoom, Zoom: v.Zoom, Rotation: v.Rotation}
	}

	g.touch(ed, twoFingers(core.TouchStart, core.Point{X: 100, Y: 100}, core.Point{X: 200, Y: 100}))
	if ed.View2D() != start {
		t.Fatalf("two fingers going down moved the view from %+v to %+v", start, ed.View2D())
	}
	want := panned(30, -10)
	g.touch(ed, twoFingers(core.TouchMove, core.Point{X: 120, Y: 90}, core.Point{X: 240, Y: 90}))
	if got := ed.View2D(); got != want {
		t.Fatalf("after the fingers' center moved (30, -10) the view is %+v, want %+v", got, want)
	}

	for _, tc := range []struct {
		name string
		ev   core.TouchEvent
	}{
		{"a third finger", core.TouchEvent{Phase: core.TouchStart, Touches: []core.Touch{{ID: 1, Position: core.Point{X: 120, Y: 90}}, {ID: 2, Position: core.Point{X: 240, Y: 90}}, {ID: 3, Position: core.Point{X: 300, Y: 300}}}}},
		{"a lifted finger", twoFingers(core.TouchEnd, core.Point{X: 120, Y: 90}, core.Point{X: 240, Y: 90})},
		{"a cancel", twoFingers(core.TouchCancel, core.Point{X: 120, Y: 90}, core.Point{X: 240, Y: 90})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g.touch(ed, twoFingers(core.TouchMove, core.Point{X: 120, Y: 90}, core.Point{X: 240, Y: 90}))
			g.touch(ed, tc.ev)
			if g.touchPan {
				t.Fatal("the pan is still on")
			}
			before := ed.View2D()
			// The next two-finger event, far from the last center, starts a new pan and does not move the view.
			g.touch(ed, twoFingers(core.TouchMove, core.Point{X: 400, Y: 400}, core.Point{X: 500, Y: 400}))
			if got := ed.View2D(); got != before {
				t.Fatalf("the first two-finger event after %s moved the view from %+v to %+v", tc.name, before, got)
			}
		})
	}
}

// A pinch zooms by its scale since its last report, so a change with no began before it, or one after a cancel, has nothing to compare with and leaves the view alone.
func TestViewportPinchNeedsABeginning(t *testing.T) {
	ed, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ed.SetCanvasSize(640, 480)
	at := core.Point{X: 320, Y: 240}
	var g viewportGestures
	start := ed.View2D()

	g.pinch(ed, core.GestureEvent{Phase: core.GestureChanged, Value: 2, Position: at})
	if ed.View2D() != start {
		t.Fatalf("a pinch change with no began moved the view from %+v to %+v", start, ed.View2D())
	}
	g = viewportGestures{}

	g.pinch(ed, core.GestureEvent{Phase: core.GestureBegan, Value: 1, Position: at})
	g.pinch(ed, core.GestureEvent{Phase: core.GestureChanged, Value: 2, Position: at})
	zoomed := ed.View2D()
	if !(zoomed.Zoom > start.Zoom) {
		t.Fatalf("a pinch from 1 to 2 left the zoom at %v, from %v", zoomed.Zoom, start.Zoom)
	}
	g.pinch(ed, core.GestureEvent{Phase: core.GestureCancelled, Position: at})
	if g.pinchLast != 0 {
		t.Fatalf("after a cancel the pinch's last scale is %v, want 0", g.pinchLast)
	}
	g.pinch(ed, core.GestureEvent{Phase: core.GestureChanged, Value: 4, Position: at})
	if ed.View2D() != zoomed {
		t.Fatalf("a pinch change after a cancel moved the view from %+v to %+v", zoomed, ed.View2D())
	}
}
