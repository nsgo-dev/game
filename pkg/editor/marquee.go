package editor

import (
	"slices"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// MarqueeMode selects whether a marquee box matches objects it fully encloses or objects it touches.
type MarqueeMode int

const (
	// MarqueeEnclose selects objects whose 2D canvas bounding box lies completely inside the marquee.
	MarqueeEnclose MarqueeMode = iota
	// MarqueeTouch selects objects whose 2D canvas bounding box intersects (touches) the marquee.
	MarqueeTouch
)

// MarqueeRect is the normalized canvas-space rectangle of an active 2D marquee selection drag.
type MarqueeRect struct {
	X0, Y0, X1, Y1 float32
	Mode           MarqueeMode
}

type marqueeState struct {
	active bool
	startX float32
	startY float32
	curX   float32
	curY   float32
	add    bool
	toggle bool
	mode   MarqueeMode
}

// MarqueeActive reports whether a 2D viewport marquee box selection drag is in progress.
func (e *Editor) MarqueeActive() bool {
	return e != nil && e.marquee.active
}

// MarqueeRect returns the active marquee rectangle in normalized canvas coordinates (X0<=X1, Y0<=Y1)
// and true, or the zero rect and false when no marquee drag is active.
func (e *Editor) MarqueeRect() (MarqueeRect, bool) {
	if e == nil || !e.marquee.active {
		return MarqueeRect{}, false
	}
	return MarqueeRect{
		X0:   min(e.marquee.startX, e.marquee.curX),
		Y0:   min(e.marquee.startY, e.marquee.curY),
		X1:   max(e.marquee.startX, e.marquee.curX),
		Y1:   max(e.marquee.startY, e.marquee.curY),
		Mode: e.marquee.mode,
	}, true
}

// BeginMarquee2D starts a 2D marquee box selection drag at canvas point (x, y) in enclose mode,
// replacing the current selection when the drag ends.
func (e *Editor) BeginMarquee2D(x, y float32) bool {
	return e.BeginMarquee2DWith(x, y, false, false, MarqueeEnclose)
}

// BeginMarquee2DWith starts a 2D marquee box selection drag at canvas point (x, y).
// When add is true (Shift), matched objects are added to the existing selection;
// when toggle is true (Mod), matched objects are toggled in the existing selection;
// otherwise the marquee replaces the selection. mode chooses enclose vs touch matching.
func (e *Editor) BeginMarquee2DWith(x, y float32, add, toggle bool, mode MarqueeMode) bool {
	if e == nil || e.playing || e.HistoryPreviewActive() || e.ViewMode != ViewMode2D || e.Tool != ToolSelect || e.marquee.active || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return false
	}
	e.marquee = marqueeState{
		active: true,
		startX: x,
		startY: y,
		curX:   x,
		curY:   y,
		add:    add,
		toggle: toggle,
		mode:   mode,
	}
	e.notify()
	return true
}

// UpdateMarquee2D updates the live corner and matching mode of an active 2D marquee drag.
func (e *Editor) UpdateMarquee2D(x, y float32, mode MarqueeMode) {
	if e == nil || !e.marquee.active || !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return
	}
	e.marquee.curX = x
	e.marquee.curY = y
	e.marquee.mode = mode
	e.notify()
}

// EndMarquee2D finishes an active 2D marquee drag, applies the selection, and returns
// the resulting selected object IDs.
func (e *Editor) EndMarquee2D() []string {
	if e == nil || !e.marquee.active {
		return nil
	}
	st := e.marquee
	e.marquee = marqueeState{}
	revBefore := e.revision
	res := e.applyMarqueeSelection(st.startX, st.startY, st.curX, st.curY, st.add, st.toggle, st.mode)
	if e.revision == revBefore {
		e.notify()
	}
	return res
}

// CancelMarquee2D aborts an active 2D marquee drag without changing the selection.
func (e *Editor) CancelMarquee2D() bool {
	if e == nil || !e.marquee.active {
		return false
	}
	e.marquee = marqueeState{}
	e.notify()
	return true
}

// SelectMarquee2D selects visible, unlocked objects matching the canvas rectangle
// from (x0, y0) to (x1, y1) directly (without an interactive drag) and returns the
// resulting selected object IDs.
func (e *Editor) SelectMarquee2D(x0, y0, x1, y1 float32, add, toggle bool, mode MarqueeMode) []string {
	if e == nil || e.playing || e.ViewMode != ViewMode2D || !isFiniteFloat32(x0) || !isFiniteFloat32(y0) || !isFiniteFloat32(x1) || !isFiniteFloat32(y1) {
		return nil
	}
	return e.applyMarqueeSelection(x0, y0, x1, y1, add, toggle, mode)
}

func (e *Editor) applyMarqueeSelection(x0, y0, x1, y1 float32, add, toggle bool, mode MarqueeMode) []string {
	hits := e.marqueeHitObjects(x0, y0, x1, y1, mode)
	switch {
	case toggle:
		if len(hits) == 0 {
			e.notify()
			return e.SelectedIDs()
		}
		next := e.SelectedIDs()
		primary := e.SelectedID
		for _, id := range hits {
			if idx := slices.Index(next, id); idx >= 0 {
				next = append(next[:idx], next[idx+1:]...)
			} else {
				next = append(next, id)
				primary = id
			}
		}
		if len(next) == 0 {
			e.ClearSelection()
		} else {
			if !slices.Contains(next, primary) {
				primary = next[len(next)-1]
			}
			e.SelectObjectsWithPrimary(next, primary)
		}
	case add:
		if len(hits) == 0 {
			e.notify()
			return e.SelectedIDs()
		}
		e.AddToSelection(hits...)
	default:
		if len(hits) == 0 {
			e.ClearSelection()
			e.notify()
		} else {
			e.SelectObjectsWithPrimary(hits, hits[0])
		}
	}
	return e.SelectedIDs()
}

// marqueeHitObjects returns the IDs of all visible, unlocked objects in e.Doc whose
// 2D canvas bounding box is enclosed by (or intersects, when mode == MarqueeTouch)
// the rectangle defined by canvas points (x0, y0) and (x1, y1).
func (e *Editor) marqueeHitObjects(x0, y0, x1, y1 float32, mode MarqueeMode) []string {
	scene := e.ActiveScene()
	if scene == nil || e.Doc == nil {
		return nil
	}
	minX, maxX := min(x0, x1), max(x0, x1)
	minY, maxY := min(y0, y1), max(y0, y1)

	var hits []string
	for _, layer := range scene.Layers {
		if !layer.Visible || e.layerLocked[layer.Name] {
			continue
		}
		toCanvas := e.layerAffineFor(layer.Space, layer.ParallaxX, layer.ParallaxY)
		var walk func(objs []*sceneformat.BuiltObject)
		walk = func(objs []*sceneformat.BuiltObject) {
			for _, bo := range objs {
				if e.hidden[bo.ID] || e.locked[bo.ID] {
					continue
				}
				// Only match objects that belong to the open document (or map instanced children to their root instance).
				targetID := ""
				for cur := bo; cur != nil; cur = cur.Parent {
					if obj, _, _ := e.FindObject(cur.ID); obj != nil {
						targetID = cur.ID
						break
					}
				}
				if targetID != "" && !slices.Contains(hits, targetID) {
					m := toCanvas.Mul(bo.World)
					rx, ry, rw, rh := objectOutline(bo)
					c0 := m.TransformPoint(math2d.Vec2{X: rx, Y: ry})
					c1 := m.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry})
					c2 := m.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry + rh})
					c3 := m.TransformPoint(math2d.Vec2{X: rx, Y: ry + rh})
					oMinX := min(min(c0.X, c1.X), min(c2.X, c3.X))
					oMaxX := max(max(c0.X, c1.X), max(c2.X, c3.X))
					oMinY := min(min(c0.Y, c1.Y), min(c2.Y, c3.Y))
					oMaxY := max(max(c0.Y, c1.Y), max(c2.Y, c3.Y))
					var matched bool
					if mode == MarqueeTouch {
						matched = oMaxX >= minX && oMinX <= maxX && oMaxY >= minY && oMinY <= maxY
					} else {
						matched = oMinX >= minX && oMaxX <= maxX && oMinY >= minY && oMaxY <= maxY
					}
					if matched {
						hits = append(hits, targetID)
					}
				}
				walk(bo.Children)
			}
		}
		walk(layer.Objects)
	}
	return hits
}

func (e *Editor) drawMarqueeOverlay(d *core.Drawing) {
	r, ok := e.MarqueeRect()
	if !ok {
		return
	}
	w, h := r.X1-r.X0, r.Y1-r.Y0
	if w <= 0 && h <= 0 {
		return
	}
	spec := e.SpecPalette(nil)
	d.Save()
	d.SetFillColor(ui.Alpha(spec.Accent.Fg, 0.14))
	d.FillRect(r.X0, r.Y0, w, h)
	d.SetStrokeColor(spec.Accent.Edge).SetLineWidth(1.5)
	if r.Mode == MarqueeTouch {
		d.SetLineDash(5, 3)
	}
	d.StrokeRect(r.X0, r.Y0, w, h)
	d.Restore()
}
