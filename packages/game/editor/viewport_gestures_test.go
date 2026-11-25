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
