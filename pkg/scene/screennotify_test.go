package scene_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/scene"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

func bounds(x, y, w, h float32) core.Bounds {
	return core.Bounds{
		Origin: core.Point{X: core.Points(x), Y: core.Points(y)},
		Size:   core.Size{Width: core.Points(w), Height: core.Points(h)},
	}
}

// notifierLog collects the enter and exit events of one notifier in order.
type notifierLog struct {
	events []string
}

func (l *notifierLog) notifier(rect core.Bounds, margin float32) *scene.ScreenNotifier {
	return &scene.ScreenNotifier{
		Rect:    rect,
		Margin:  margin,
		OnEnter: func() { l.events = append(l.events, "enter") },
		OnExit:  func() { l.events = append(l.events, "exit") },
	}
}

func TestScreenNotifierEnterExitOrder(t *testing.T) {
	cam := &tilemap.Camera{X: 160, Y: 90, Zoom: 1, Width: 320, Height: 180}
	var log notifierLog
	n := log.notifier(bounds(500, 500, 16, 16), 0)

	// Starts off-screen: no events until the rectangle moves into the view.
	for range 3 {
		if n.Step(cam) {
			t.Fatal("off-screen rectangle reported on-screen")
		}
	}
	if len(log.events) != 0 {
		t.Fatalf("off-screen notifier fired %v", log.events)
	}

	// Moving inside the view fires enter exactly once and keeps the notifier on-screen.
	n.Rect = bounds(150, 80, 16, 16)
	if !n.Step(cam) {
		t.Fatal("rectangle inside the view reported off-screen")
	}
	for range 3 {
		n.Step(cam)
	}
	if len(log.events) != 1 || log.events[0] != "enter" {
		t.Fatalf("events = %v, want [enter]", log.events)
	}

	// Leaving the view fires exit exactly once.
	n.Rect = bounds(500, 500, 16, 16)
	if n.Step(cam) {
		t.Fatal("rectangle outside the view reported on-screen")
	}
	for range 3 {
		n.Step(cam)
	}
	if len(log.events) != 2 || log.events[1] != "exit" {
		t.Fatalf("events = %v, want [enter exit]", log.events)
	}
}

func TestScreenNotifierFirstStepInsideFiresEnter(t *testing.T) {
	cam := &tilemap.Camera{X: 160, Y: 90, Zoom: 1, Width: 320, Height: 180}
	var log notifierLog
	n := log.notifier(bounds(150, 80, 16, 16), 0)
	if !n.Step(cam) {
		t.Fatal("rectangle starting inside the view reported off-screen")
	}
	if len(log.events) != 1 || log.events[0] != "enter" {
		t.Fatalf("events = %v, want [enter]", log.events)
	}
}

func TestScreenNotifierMarginHysteresis(t *testing.T) {
	// View spans x in [0, 320), y in [0, 180).
	cam := &tilemap.Camera{X: 160, Y: 90, Zoom: 1, Width: 320, Height: 180}
	var log notifierLog
	n := log.notifier(bounds(300, 80, 16, 16), 32)

	if !n.Step(cam) || len(log.events) != 1 || log.events[0] != "enter" {
		t.Fatalf("on-screen rectangle did not enter: events = %v", log.events)
	}

	// On-screen and reaching 20 points past the edge: inside the 32-point margin, so no exit.
	n.Rect = bounds(320, 80, 20, 16)
	if !n.Step(cam) {
		t.Fatal("on-screen notifier exited inside its margin band")
	}

	// Fully 40 points past the edge is beyond the margin: exit fires.
	n.Rect = bounds(360, 80, 16, 16)
	if n.Step(cam) {
		t.Fatal("on-screen notifier past its margin band stayed on-screen")
	}
	if len(log.events) != 2 || log.events[1] != "exit" {
		t.Fatalf("events = %v, want [enter exit]", log.events)
	}

	// Off-screen 20 points past the edge, inside the margin band, does not re-enter: entering requires the view itself.
	n.Rect = bounds(340, 80, 16, 16)
	if n.Step(cam) {
		t.Fatal("off-screen notifier entered from inside its margin band")
	}

	// Overlapping the view edge enters again.
	n.Rect = bounds(319, 80, 4, 16)
	if !n.Step(cam) {
		t.Fatal("rectangle overlapping the view edge reported off-screen")
	}

	// Leaving past the margin fires the second exit.
	n.Rect = bounds(500, 500, 16, 16)
	if n.Step(cam) {
		t.Fatal("rectangle outside the view reported on-screen")
	}
	if len(log.events) != 4 || log.events[2] != "enter" || log.events[3] != "exit" {
		t.Fatalf("events = %v, want [enter exit enter exit]", log.events)
	}
}

func TestScreenNotifierZoomedCamera(t *testing.T) {
	// At zoom 2 the same 320x180 view sees a 160x90 world rectangle: x in [80, 240), y in [45, 135).
	cam := &tilemap.Camera{X: 160, Y: 90, Zoom: 2, Width: 320, Height: 180}
	var log notifierLog
	n := log.notifier(bounds(200, 24, 16, 16), 0) // y in [24, 40): above the zoomed view

	if n.Step(cam) {
		t.Fatal("rectangle above the zoomed view reported on-screen")
	}

	// Zooming out to 1 widens the view to x in [0, 320), y in [0, 180): the rectangle enters.
	cam.Zoom = 1
	if !n.Step(cam) {
		t.Fatal("rectangle inside the unzoomed view reported off-screen")
	}
	if len(log.events) != 1 || log.events[0] != "enter" {
		t.Fatalf("events = %v, want [enter]", log.events)
	}

	// Zooming back in to 2 shrinks the view again and the rectangle leaves.
	cam.Zoom = 2
	if n.Step(cam) {
		t.Fatal("rectangle above the zoomed view reported on-screen")
	}
	if len(log.events) != 2 || log.events[1] != "exit" {
		t.Fatalf("events = %v, want [enter exit]", log.events)
	}
}

func TestScreenNotifierRotatedCamera(t *testing.T) {
	// Unrotated, the 320x180 view at (160, 90) spans x in [0, 320), y in [0, 180).
	cam := &tilemap.Camera{X: 160, Y: 90, Zoom: 1, Width: 320, Height: 180}
	var log notifierLog
	n := log.notifier(bounds(200, -30, 16, 16), 0) // above the view at y in [-30, -14)

	if n.Step(cam) {
		t.Fatal("rectangle above the unrotated view reported on-screen")
	}

	// Rotating the view by 90 degrees swaps the axes of its covering rectangle, which then
	// spans x in [70, 250), y in [-70, 250) and reaches the rectangle above.
	cam.Rotation = 1.5708 // pi/2
	if !n.Step(cam) {
		t.Fatal("rectangle inside the rotated view's bounds reported off-screen")
	}
	if len(log.events) != 1 || log.events[0] != "enter" {
		t.Fatalf("events = %v, want [enter]", log.events)
	}
}

func TestScreenNotifierMultipleViews(t *testing.T) {
	left := &tilemap.Camera{X: 160, Y: 90, Zoom: 1, Width: 320, Height: 180}
	right := &tilemap.Camera{X: 560, Y: 90, Zoom: 1, Width: 320, Height: 180} // x in [400, 720)
	var log notifierLog
	n := log.notifier(bounds(500, 80, 16, 16), 0) // inside the right camera only

	if !n.Step(left, right) {
		t.Fatal("rectangle seen by the second view reported off-screen")
	}
	if len(log.events) != 1 || log.events[0] != "enter" {
		t.Fatalf("events = %v, want [enter]", log.events)
	}

	// Moving between the views, seen by neither, exits.
	n.Rect = bounds(340, 80, 16, 16)
	if n.Step(left, right) {
		t.Fatal("rectangle between the views reported on-screen")
	}

	// Nil cameras are skipped, and no views at all means on-screen.
	n.Rect = bounds(5000, 5000, 16, 16)
	if !n.Step(nil, nil) {
		t.Fatal("step with only nil cameras reported off-screen")
	}
	if !n.Step() {
		t.Fatal("step with no views reported off-screen")
	}
}

func TestScreenNotifierEmptyRectNeverEnters(t *testing.T) {
	cam := &tilemap.Camera{X: 160, Y: 90, Zoom: 1, Width: 320, Height: 180}
	var log notifierLog
	n := log.notifier(bounds(150, 80, 0, 16), 0)
	if n.Step(cam) {
		t.Fatal("zero-width rectangle reported on-screen")
	}
	if n.Step() {
		t.Fatal("zero-width rectangle reported on-screen with no views")
	}
	if len(log.events) != 0 {
		t.Fatalf("empty rectangle fired %v", log.events)
	}
}
