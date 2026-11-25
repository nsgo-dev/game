package editor

import (
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// viewport3DGestures is the 3D viewport's navigation in progress: a drag that orbits, pans or moves the gizmo, a middle-button pan, a right-button flight, a two-finger touch pan and a pinch.
type viewport3DGestures struct {
	// pressPos is where the finger or pointer went down: a pan event carries only its translation, so the down point is tracked from the frame's touch events.
	pressPos core.Point
	hasPress bool
	// began says a pan that began with a press owns a 3D drag to finish; the touch that ends may be reported before the pan's end event.
	began   bool
	lastPan core.Point
	// middle is a middle-button pan under way, from middleLast.
	middle     bool
	middleLast core.Point
	// fly is a right-button flight under way: unlock releases the pointer lock it took, nil where the platform cannot lock the pointer, and flyLast is where the pointer was, which its movement turns the view by without a lock.
	fly     bool
	unlock  func()
	flyLast core.Point
	// multi says two or more fingers are down, which pan the view; touchLast is their center.
	multi     bool
	touchPan  bool
	touchLast core.Point
	pinchLast float32
}

// wheel zooms the view by a mouse wheel's turn or a touchpad scroll with Control or Command held, pans it by a touchpad's scroll, and while flying changes the fly speed.
func (g *viewport3DGestures) wheel(ed *Editor, ev core.WheelEvent) {
	if ed.Flying3D() {
		ed.AdjustFlySpeed3D(float32(ev.Delta.Y))
		return
	}
	if ev.Touchpad && !ev.Modifiers.Control && !ev.Modifiers.Platform {
		_, ch := ed.CanvasSize()
		ed.PanView3D(float32(ev.Delta.X), float32(ev.Delta.Y), ch)
		return
	}
	ed.HandleViewport3DWheel(float32(ev.Delta.Y))
}

// pinch zooms the view by how much the pinch scaled since its last report.
func (g *viewport3DGestures) pinch(ed *Editor, ev core.GestureEvent) {
	switch ev.Phase {
	case core.GestureBegan:
		g.pinchLast = ev.Value
	case core.GestureChanged, core.GestureEnded:
		if g.pinchLast > 0 && ev.Value > 0 {
			ed.ZoomView3D(ev.Value / g.pinchLast)
		}
		g.pinchLast = ev.Value
		if ev.Phase == core.GestureEnded {
			g.pinchLast = 0
		}
	default:
		g.pinchLast = 0
	}
}

// mouseDown starts a middle-button pan or, with the right button, a flight that locks the pointer to frame where the platform can.
func (g *viewport3DGestures) mouseDown(cx *core.Scope, ed *Editor, frame *core.Ref, ev core.MouseButtonEvent) {
	switch ev.Button {
	case core.MouseButtonMiddle:
		if g.fly || ed.PickingConnectionTarget() {
			return
		}
		cw, ch := ed.CanvasSize()
		ed.BeginViewport3DDrag(float32(ev.Position.X), float32(ev.Position.Y), cw, ch, true)
		g.middle, g.middleLast = true, ev.Position
	case core.MouseButtonRight:
		if g.middle || !ed.BeginFly3D() {
			return
		}
		g.fly, g.flyLast = true, ev.Position
		if cx.Env().CanLockPointer() {
			g.unlock = frame.LockPointer()
		}
	}
}

// mouseMove follows a middle-button pan, and turns a flight by the pointer's movement where the pointer is not locked.
func (g *viewport3DGestures) mouseMove(ed *Editor, ev core.MouseEvent) {
	if g.middle {
		if ev.PressedButton != core.MouseButtonMiddle {
			g.endMiddle(ed)
		} else {
			ed.UpdateViewport3DDrag(float32(ev.Position.X), float32(ev.Position.Y))
			g.middleLast = ev.Position
		}
	}
	if g.fly {
		if ev.PressedButton != core.MouseButtonRight || !ed.Flying3D() {
			g.endFly(ed)
			return
		}
		if g.unlock == nil {
			ed.FlyLook3D(float32(ev.Position.X-g.flyLast.X), float32(ev.Position.Y-g.flyLast.Y))
		}
		g.flyLast = ev.Position
	}
}

// mouseUp ends the pan or flight of the button that came up.
func (g *viewport3DGestures) mouseUp(ed *Editor, ev core.MouseButtonEvent) {
	switch ev.Button {
	case core.MouseButtonMiddle:
		if g.middle {
			ed.UpdateViewport3DDrag(float32(ev.Position.X), float32(ev.Position.Y))
			g.endMiddle(ed)
		}
	case core.MouseButtonRight:
		g.endFly(ed)
	}
}

func (g *viewport3DGestures) endMiddle(ed *Editor) {
	g.middle = false
	ed.EndViewport3DDrag()
}

// endFly ends the flight and releases the pointer it locked.
func (g *viewport3DGestures) endFly(ed *Editor) {
	if !g.fly {
		return
	}
	g.fly = false
	if g.unlock != nil {
		g.unlock()
		g.unlock = nil
	}
	ed.EndFly3D()
}

// pointerDelta turns a flight by the locked pointer's movement.
func (g *viewport3DGestures) pointerDelta(ed *Editor, dx, dy float64) {
	if g.fly {
		ed.FlyLook3D(float32(dx), float32(dy))
	}
}

// touch tracks where a finger or the pointer went down, for the drag a pan starts, and pans the view by the movement of the center of two fingers; a second finger puts back a gizmo drag the first one began and ends its orbit.
func (g *viewport3DGestures) touch(ed *Editor, ev core.TouchEvent) {
	switch ev.Phase {
	case core.TouchStart:
		if len(ev.Touches) == 1 {
			g.multi = false
		}
		for _, t := range ev.Touches {
			if t.Changed && len(ev.Touches) == 1 {
				g.pressPos = t.Position
				g.hasPress = true
			}
		}
	case core.TouchEnd, core.TouchCancel:
		g.hasPress = false
		if ev.Phase == core.TouchCancel {
			ed.CancelGizmoDrag3D()
		}
	}
	if len(ev.Touches) < 2 || ev.Phase == core.TouchEnd || ev.Phase == core.TouchCancel {
		g.touchPan = false
		if len(ev.Touches) <= 1 && ev.Phase != core.TouchMove {
			g.multi = false
		}
		return
	}
	if !g.multi {
		g.multi = true
		g.hasPress = false
		ed.CancelGizmoDrag3D()
		if g.began {
			ed.EndViewport3DDrag()
			g.began = false
		}
	}
	c := core.Point{
		X: (ev.Touches[0].Position.X + ev.Touches[1].Position.X) / 2,
		Y: (ev.Touches[0].Position.Y + ev.Touches[1].Position.Y) / 2,
	}
	if g.touchPan {
		_, ch := ed.CanvasSize()
		ed.PanView3D(float32(c.X-g.touchLast.X), float32(c.Y-g.touchLast.Y), ch)
	}
	g.touchPan = true
	g.touchLast = c
}

// pan follows a one-finger or primary-button drag: one that starts on a gizmo handle drags it, one with Shift held pans the view, and any other orbits it.
func (g *viewport3DGestures) pan(ed *Editor, ev core.PanEvent, shiftHeld func() bool) {
	cw, ch := ed.CanvasSize()
	switch ev.Phase {
	case core.GestureBegan:
		g.lastPan = ev.Translation
		if g.middle || g.fly || g.multi || ed.Flying3D() {
			g.began = false
			return
		}
		g.began = g.hasPress && !ed.PickingConnectionTarget()
		if !g.began && !ed.PickingConnectionTarget() && ed.TerrainOpen() && ed.TerrainBrush().Mode != TerrainBrushNone {
			g.pressPos = core.Point{
				X: ev.Position.X - ev.Translation.X,
				Y: ev.Position.Y - ev.Translation.Y,
			}
			g.hasPress = true
			g.began = true
		}
		if g.began {
			// A press that turns into a pan first picks a terrain brush hit or gizmo handle under the press point, and orbits otherwise, or pans with Shift held.
			ed.BeginViewport3DDrag(float32(g.pressPos.X), float32(g.pressPos.Y), cw, ch, shiftHeld())
		}
	case core.GestureChanged:
		if g.middle || g.fly || g.multi || ed.Flying3D() {
			g.lastPan = ev.Translation
			return
		}
		if g.began && g.hasPress {
			ed.UpdateViewport3DDrag(float32(g.pressPos.X)+float32(ev.Translation.X), float32(g.pressPos.Y)+float32(ev.Translation.Y))
			return
		}
		if ed.PickingConnectionTarget() || g.began {
			return
		}
		dx := float32(ev.Translation.X - g.lastPan.X)
		dy := float32(ev.Translation.Y - g.lastPan.Y)
		g.lastPan = ev.Translation
		if shiftHeld() {
			ed.PanView3D(dx, dy, ch)
			return
		}
		ed.OrbitView3D(-dx*0.01, dy*0.01)
	case core.GestureEnded, core.GestureCancelled:
		if g.began {
			ed.EndViewport3DDrag()
			g.began = false
		}
		g.lastPan = core.Point{}
	}
}

// flyKeys3D are the keys that move the flying camera, which take them from the gizmo's shortcuts (W and S) while it flies.
var flyKeys3D = []string{"w", "a", "s", "d", "q", "e"}

// maxFlyStep3D caps the time one frame moves the flying camera for, so a stalled frame does not throw it across the scene.
const maxFlyStep3D = 100 * time.Millisecond

// flyView3D runs while the camera flies (Flying3D): it binds the movement keys, so W and S move the camera instead of switching the gizmo, and on every display frame moves it by the keys held, W and S forward and back, A and D left and right, Q and E down and up, Shift faster.
func flyView3D(cx *core.Scope, ed *Editor) core.Node {
	for _, k := range flyKeys3D {
		cx.OnKeyWith(k, core.KeyOptions{When: core.KeyNotEditing}, func() {})
		cx.OnKeyWith("shift+"+k, core.KeyOptions{When: core.KeyNotEditing}, func() {})
	}
	axis := func(plus, minus string) float32 {
		var v float32
		if core.KeyHeld(cx, plus) {
			v++
		}
		if core.KeyHeld(cx, minus) {
			v--
		}
		return v
	}
	var last time.Time
	var loop func(at time.Time)
	loop = func(at time.Time) {
		if !last.IsZero() {
			ed.FlyMove3D(axis("d", "a"), axis("e", "q"), axis("w", "s"), core.KeyHeld(cx, "shift"), min(at.Sub(last), maxFlyStep3D))
		}
		last = at
		cx.RequestAnimationFrame(loop)
	}
	cx.RequestAnimationFrame(loop)
	return core.View{
		PointerEvents: core.PointerEventsNone,
		Style:         core.Style{Position: core.Absolute, Width: core.Pt(0), Height: core.Pt(0)},
	}
}

// axisWidgetSize3D is the side of the axis widget's square, in points, and axisWidgetReach3D how far from its center an axis's end lies.
const (
	axisWidgetSize3D  float32 = 76
	axisWidgetReach3D float32 = 26
	axisMarkRadius3D  float32 = 9
)

// axisMark3D is one end of a world axis on the axis widget, and the view pressing it turns to.
type axisMark3D struct {
	view     View3D
	label    string
	positive bool
	color    core.Color
	// x and y are its place in the widget, and depth how far it points away from the viewer, from -1 (at the viewer) to 1.
	x, y, depth float32
}

// axisMarks3D returns the six axis ends of the widget for camera c, the farthest first, as they are drawn.
func axisMarks3D(c OrbitCamera3D) []axisMark3D {
	right, up, forward := c.basis64()
	ends := []struct {
		dir      [3]float64
		view     View3D
		label    string
		positive bool
		color    core.Color
	}{
		{[3]float64{1, 0, 0}, View3DRight, "X", true, core.RGBA(0xEF4444, 1)},
		{[3]float64{-1, 0, 0}, View3DLeft, "", false, core.RGBA(0xEF4444, 1)},
		{[3]float64{0, 1, 0}, View3DTop, "Y", true, core.RGBA(0x22C55E, 1)},
		{[3]float64{0, -1, 0}, View3DBottom, "", false, core.RGBA(0x22C55E, 1)},
		{[3]float64{0, 0, 1}, View3DFront, "Z", true, core.RGBA(0x3B82F6, 1)},
		{[3]float64{0, 0, -1}, View3DBack, "", false, core.RGBA(0x3B82F6, 1)},
	}
	center := axisWidgetSize3D / 2
	marks := make([]axisMark3D, 0, len(ends))
	for _, end := range ends {
		marks = append(marks, axisMark3D{
			view:     end.view,
			label:    end.label,
			positive: end.positive,
			color:    end.color,
			x:        center + float32(dot64(end.dir, right))*axisWidgetReach3D,
			y:        center - float32(dot64(end.dir, up))*axisWidgetReach3D,
			depth:    float32(dot64(end.dir, forward)),
		})
	}
	slices.SortStableFunc(marks, func(a, b axisMark3D) int {
		switch {
		case a.depth > b.depth:
			return -1
		case a.depth < b.depth:
			return 1
		}
		return 0
	})
	return marks
}

// pickAxisMark3D returns the view of the axis end under the widget point (x, y), the one nearest the viewer where ends overlap, and false where there is none.
func pickAxisMark3D(c OrbitCamera3D, x, y float32) (View3D, bool) {
	marks := axisMarks3D(c)
	for i := len(marks) - 1; i >= 0; i-- {
		m := marks[i]
		if math.Hypot(float64(x-m.x), float64(y-m.y)) <= float64(axisMarkRadius3D)+2 {
			return m.view, true
		}
	}
	return 0, false
}

// drawAxisWidget3D draws the axis widget for camera c: a line from the center to each positive end, a lettered disc at each positive end and a plain one at each negative end, the nearer over the farther.
func drawAxisWidget3D(d *core.Drawing, c OrbitCamera3D, ink core.Color) {
	center := axisWidgetSize3D / 2
	font := core.Font{Size: 11, Weight: 700}
	for _, m := range axisMarks3D(c) {
		if m.positive {
			d.Save().SetStrokeColor(m.color).SetLineWidth(2).BeginPath().MoveTo(center, center).LineTo(m.x, m.y).Stroke().Restore()
			d.Save().SetFillColor(m.color).BeginPath().Arc(m.x, m.y, axisMarkRadius3D, 0, float32(2*math.Pi), false).Fill().Restore()
			d.Save().SetFillColor(ink).SetTextAlign(core.TextAlignCenter).SetTextBaseline(core.BaselineMiddle).FillText(m.label, m.x, m.y, font).Restore()
			continue
		}
		d.Save().SetFillColor(m.color.Faded(0.45)).BeginPath().Arc(m.x, m.y, axisMarkRadius3D-2, 0, float32(2*math.Pi), false).Fill().Restore()
	}
}

// viewport3DOverlay is what the 3D viewport shows over the scene: the bar with the projection, the view's name, Focus and Reset view; the axis widget with the Orbit buttons in the top right corner; and the controls' help line.
func viewport3DOverlay(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) []core.Node {
	colors := ui.ColorsOf(cx)
	ortho := func() bool {
		_ = rev.Get()
		return ed.Camera3D.Orthographic
	}
	panel := core.Style{
		Direction:    core.Row,
		AlignItems:   core.AlignCenter,
		Gap:          4,
		Padding:      core.Symmetric(2, 4),
		CornerRadius: 6,
		Background:   colors.Raised(),
	}
	bar := panel
	bar.Position, bar.Left, bar.Top = core.Absolute, core.Pt(8), core.Pt(8)
	axes := core.Style{Position: core.Absolute, Right: core.Pt(8), Top: core.Pt(8), AlignItems: core.AlignCenter, Gap: 4}
	help := core.Style{
		Position:     core.Absolute,
		Left:         core.Pt(8),
		Bottom:       core.Pt(8),
		Padding:      core.Symmetric(2, 6),
		CornerRadius: 4,
		FontSize:     11,
		Color:        colors.Muted(),
		Background:   colors.Raised(),
	}
	iconBtn := func(testID string, icon func() *core.SVG, label string, fn func()) core.Node {
		return ui.Button{TestID: testID, Icon: icon, Label: label, Size: ui.SizeSmall, Variant: ui.ButtonGhost, OnPress: fn}
	}
	return []core.Node{
		core.View{
			TestID: "view3d-bar",
			Style:  bar,
			Children: []core.Node{
				ui.ToolbarGroup{
					Label: "Projection",
					Children: []core.Node{
						ui.ToggleButton{TestID: "view3d-persp", Text: "Persp", Label: "Perspective (" + cx.KeyLabel("5") + ")", On: func() bool { return !ortho() }, OnChange: func(on bool) {
							if on {
								ed.SetOrthographic3D(false)
							}
						}},
						ui.ToggleButton{TestID: "view3d-ortho", Text: "Ortho", Label: "Orthographic (" + cx.KeyLabel("5") + ")", On: ortho, OnChange: func(on bool) {
							if on {
								ed.SetOrthographic3D(true)
							}
						}},
					},
				},
				core.Text{
					TestID: "view3d-name",
					ContentFunc: func() string {
						_ = rev.Get()
						return ed.ViewName3D()
					},
					Style: core.Style{FontSize: 12, Color: colors.Text(), Padding: core.Symmetric(0, 6)},
				},
				ui.Button{TestID: "view3d-focus", Icon: lucide.Focus, Text: "Focus", Label: "Frame the selection (" + cx.KeyLabel("f") + ")", Size: ui.SizeSmall, Variant: ui.ButtonGhost, OnPress: func() { ed.FrameSelection() }},
				iconBtn("view3d-reset", lucide.RefreshCcw, "Reset view", ed.ResetView3D),
			},
		},
		core.View{
			TestID: "view3d-axes-panel",
			Style:  axes,
			Children: []core.Node{
				core.Canvas{
					TestID: "view3d-axes",
					LabelFunc: func() string {
						_ = rev.Get()
						return "View axes: " + ed.ViewName3D()
					},
					Style: core.Style{Width: core.Pt(core.Points(axisWidgetSize3D)), Height: core.Pt(core.Points(axisWidgetSize3D))},
					OnPress: func(ev core.PressEvent) {
						if v, ok := pickAxisMark3D(ed.Camera3D, float32(ev.Position.X), float32(ev.Position.Y)); ok {
							ed.SetView3D(v)
						}
					},
					Draw: func(d *core.Drawing, _ core.Size) {
						_ = rev.Get()
						drawAxisWidget3D(d, ed.Camera3D, core.RGBA(0xFFFFFF, 1))
					},
				},
				core.View{
					Style: panel,
					Children: []core.Node{
						iconBtn("view3d-orbit-left", lucide.RotateCcw, "Orbit 15° left", func() { ed.OrbitView3D(-orbitStep3D, 0) }),
						iconBtn("view3d-orbit-right", lucide.RotateCw, "Orbit 15° right", func() { ed.OrbitView3D(orbitStep3D, 0) }),
					},
				},
			},
		},
		core.Text{
			TestID:  "view3d-help",
			Content: "Drag orbit · Middle pan · Right WASD fly · " + cx.KeyLabel("f") + " focus",
			Style:   help,
		},
	}
}
