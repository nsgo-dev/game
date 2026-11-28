package editor

import (
	"math"
	"slices"
	"time"

	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/game/pkg/l10n"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// viewport3DGestures is the 3D viewport's navigation in progress: a drag that orbits, pans, dollies or moves the gizmo, a middle-button camera drag, a right-button flight or dolly, a two-finger touch pan and a pinch.
type viewport3DGestures struct {
	// pressPos is where the finger or pointer went down: a pan event carries only its translation, so the down point is tracked from the frame's touch events.
	pressPos core.Point
	hasPress bool
	// leftMods records the modifiers held when MouseButtonLeft went down so primary drags combine OS event flags with live held keys.
	leftMods    core.Modifiers
	hasLeftMods bool
	// began says a pan that began with a press owns a 3D drag to finish; the touch that ends may be reported before the pan's end event.
	began bool
	// panAction is the resolved camera action for a primary drag started without a recorded press position.
	panAction Navigation3DAction
	lastPan   core.Point
	// middle is a middle-button camera drag under way, from middleLast.
	middle     bool
	middleLast core.Point
	// rightDolly is a right-button camera drag (such as Maya Alt+right-drag dolly) under way, from rightLast.
	rightDolly      bool
	rightDollyMoved bool
	rightLast       core.Point
	// fly is a right-button flight under way: unlock releases the pointer lock it took, nil where the platform cannot lock the pointer, and flyLast is where the pointer was, which its movement turns the view by without a lock.
	fly     bool
	unlock  func()
	flyLast core.Point
	// multi says two or more fingers are down, which pan the view; touchLast is their center.
	multi         bool
	touchPan      bool
	touchLast     core.Point
	pinchLast     float32
	suppressPress bool
	snapInvert    func() bool
}

// mouseEventModifiers combines the modifiers carried on a mouse button event with live held keys from cx.
func mouseEventModifiers(cx *core.Scope, mods core.Modifiers) core.Modifiers {
	if cx == nil {
		return mods
	}
	return core.Modifiers{
		Shift:    mods.Shift || core.Untracked(cx, func() bool { return core.KeyHeld(cx, "shift") }),
		Control:  mods.Control || core.Untracked(cx, func() bool { return core.KeyHeld(cx, "ctrl") }),
		Alt:      mods.Alt || core.Untracked(cx, func() bool { return core.KeyHeld(cx, "alt") }),
		Platform: mods.Platform || core.Untracked(cx, func() bool { return core.KeyHeld(cx, "mod") }),
	}
}

func (g *viewport3DGestures) primaryModifiers(shiftHeld, altHeld func() bool) core.Modifiers {
	var mods core.Modifiers
	if g.hasLeftMods {
		mods = g.leftMods
	}
	if shiftHeld != nil && shiftHeld() {
		mods.Shift = true
	}
	if altHeld != nil && altHeld() {
		mods.Alt = true
	}
	return mods
}

// wheel zooms the view by a mouse wheel's turn (routed through Navigation3D.Resolve) or a touchpad scroll with Control or Command held, pans it by a touchpad's scroll, and while flying changes the fly speed.
func (g *viewport3DGestures) wheel(ed *Editor, ev core.WheelEvent) {
	if ed.Flying3D() {
		ed.AdjustFlySpeed3D(float32(ev.Delta.Y))
		return
	}
	if ev.Touchpad {
		if !ev.Modifiers.Control && !ev.Modifiers.Platform {
			_, ch := ed.CanvasSize()
			ed.PanView3D(float32(ev.Delta.X), float32(ev.Delta.Y), ch)
			return
		}
		ed.HandleViewport3DWheel(float32(ev.Delta.Y))
		return
	}
	switch ed.Navigation3D().Resolve(Navigation3DInputWheel, ev.Modifiers) {
	case Navigation3DActionDolly:
		ed.HandleViewport3DWheel(float32(ev.Delta.Y))
	case Navigation3DActionPan:
		_, ch := ed.CanvasSize()
		ed.PanView3D(float32(ev.Delta.X), float32(ev.Delta.Y), ch)
	}
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

// mouseDown routes middle- and right-button presses through Navigation3D.Resolve (and records left-button modifiers for primary drags).
func (g *viewport3DGestures) mouseDown(cx *core.Scope, ed *Editor, frame *core.Ref, ev core.MouseButtonEvent) {
	mods := mouseEventModifiers(cx, ev.Modifiers)
	switch ev.Button {
	case core.MouseButtonLeft:
		g.suppressPress = false
		g.leftMods = mods
		g.hasLeftMods = true
	case core.MouseButtonMiddle:
		if g.fly || g.rightDolly || ed.PickingConnectionTarget() {
			return
		}
		action := ed.Navigation3D().Resolve(Navigation3DInputMiddleDrag, mods)
		switch action {
		case Navigation3DActionOrbit, Navigation3DActionPan, Navigation3DActionDolly:
			cw, ch := ed.CanvasSize()
			if ed.beginViewport3DDragAction(float32(ev.Position.X), float32(ev.Position.Y), cw, ch, action, false) {
				g.middle, g.middleLast = true, ev.Position
			}
		}
	case core.MouseButtonRight:
		if g.middle {
			return
		}
		g.rightDollyMoved = false
		action := ed.Navigation3D().Resolve(Navigation3DInputRightDrag, mods)
		switch action {
		case Navigation3DActionFly:
			if !ed.BeginFly3D() {
				return
			}
			g.fly, g.flyLast = true, ev.Position
			if cx != nil && cx.Env().CanLockPointer() {
				g.unlock = frame.LockPointer()
			}
		case Navigation3DActionOrbit, Navigation3DActionPan, Navigation3DActionDolly:
			if ed.PickingConnectionTarget() {
				return
			}
			cw, ch := ed.CanvasSize()
			if ed.beginViewport3DDragAction(float32(ev.Position.X), float32(ev.Position.Y), cw, ch, action, false) {
				g.rightDolly, g.rightLast = true, ev.Position
			}
		}
	}
}

// mouseMove follows a middle-button camera drag, a right-button dolly drag, and turns a flight by the pointer's movement where the pointer is not locked.
func (g *viewport3DGestures) mouseMove(ed *Editor, ev core.MouseEvent) {
	if g.middle {
		if ev.PressedButton != core.MouseButtonMiddle {
			g.endMiddle(ed)
		} else {
			ed.UpdateViewport3DDrag(float32(ev.Position.X), float32(ev.Position.Y))
			g.middleLast = ev.Position
		}
	}
	if g.rightDolly {
		if ev.PressedButton != core.MouseButtonRight {
			g.endRightDolly(ed)
		} else {
			if ev.Position != g.rightLast {
				g.rightDollyMoved = true
			}
			ed.UpdateViewport3DDrag(float32(ev.Position.X), float32(ev.Position.Y))
			g.rightLast = ev.Position
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

// mouseUp ends the camera drag or flight of the button that came up.
func (g *viewport3DGestures) mouseUp(ed *Editor, ev core.MouseButtonEvent) {
	switch ev.Button {
	case core.MouseButtonLeft:
		g.hasLeftMods = false
	case core.MouseButtonMiddle:
		if g.middle {
			ed.UpdateViewport3DDrag(float32(ev.Position.X), float32(ev.Position.Y))
			g.endMiddle(ed)
		}
	case core.MouseButtonRight:
		if g.rightDolly {
			if ev.Position != g.rightLast {
				g.rightDollyMoved = true
			}
			ed.UpdateViewport3DDrag(float32(ev.Position.X), float32(ev.Position.Y))
			g.endRightDolly(ed)
		}
		g.endFly(ed)
	}
}

func (g *viewport3DGestures) endMiddle(ed *Editor) {
	g.middle = false
	ed.EndViewport3DDrag()
}

func (g *viewport3DGestures) endRightDolly(ed *Editor) {
	if !g.rightDolly {
		return
	}
	g.rightDolly = false
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
			g.suppressPress = false
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
			g.hasLeftMods = false
			ed.CancelGizmoDrag3D()
			ed.CancelComponentGizmoDrag3D()
			ed.CancelPluginDrag3D()
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
		g.hasLeftMods = false
		ed.CancelGizmoDrag3D()
		ed.CancelComponentGizmoDrag3D()
		ed.CancelPluginDrag3D()
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

// pan follows a primary-button or one-finger drag according to Navigation3D.Resolve:
// when tools/gizmos are allowed (plain drag or Navigation3DActionNone), a hit on a terrain brush,
// 3D plugin tool, light/camera handle, or gizmo handle drags that target; otherwise it runs the
// resolved camera action (or does nothing for Navigation3DActionNone).
func (g *viewport3DGestures) pan(ed *Editor, ev core.PanEvent, shiftHeld, altHeld func() bool) {
	cw, ch := ed.CanvasSize()
	switch ev.Phase {
	case core.GestureBegan:
		g.lastPan = ev.Translation
		g.panAction = Navigation3DActionNone
		if g.middle || g.rightDolly || g.fly || g.multi || ed.Flying3D() {
			g.began = false
			return
		}
		if ed.PickingConnectionTarget() {
			g.began = false
			return
		}
		mods := g.primaryModifiers(shiftHeld, altHeld)
		action := ed.Navigation3D().Resolve(Navigation3DInputPrimaryDrag, mods)
		allowTools := action == Navigation3DActionNone || (!mods.Shift && !mods.Alt)
		g.began = g.hasPress
		if !g.began && allowTools && ((ed.TerrainOpen() && ed.TerrainBrush().Mode != TerrainBrushNone) || (ed.Tool == ToolPlugin && ed.activePluginToolSupports3D())) {
			g.pressPos = core.Point{
				X: ev.Position.X - ev.Translation.X,
				Y: ev.Position.Y - ev.Translation.Y,
			}
			g.hasPress = true
			g.began = true
		}
		if g.began {
			invert := g.snapInvert != nil && g.snapInvert()
			var started bool
			ed.withSnapInvert(invert, func() {
				started = ed.beginViewport3DDragAction(float32(g.pressPos.X), float32(g.pressPos.Y), cw, ch, action, allowTools)
			})
			g.began = started
			return
		}
		g.panAction = action
	case core.GestureChanged:
		if g.middle || g.rightDolly || g.fly || g.multi || ed.Flying3D() {
			g.lastPan = ev.Translation
			return
		}
		if g.began && g.hasPress {
			invert := g.snapInvert != nil && g.snapInvert()
			ed.withSnapInvert(invert, func() {
				ed.UpdateViewport3DDrag(float32(g.pressPos.X)+float32(ev.Translation.X), float32(g.pressPos.Y)+float32(ev.Translation.Y))
			})
			return
		}
		if ed.PickingConnectionTarget() || g.began {
			return
		}
		dx := float32(ev.Translation.X - g.lastPan.X)
		dy := float32(ev.Translation.Y - g.lastPan.Y)
		g.lastPan = ev.Translation
		switch g.panAction {
		case Navigation3DActionPan:
			ed.PanView3D(dx, dy, ch)
		case Navigation3DActionOrbit:
			ed.OrbitView3D(-dx*0.01, dy*0.01)
		case Navigation3DActionDolly:
			if delta := dx + dy; delta != 0 && isFiniteFloat32(delta) {
				ed.ZoomView3D(float32(math.Exp(float64(delta) * 0.005)))
			}
		}
	case core.GestureEnded:
		if g.began {
			if ed.PluginDragging3D() || ed.DraggingComponentGizmo3D() || ed.DraggingGizmo3D() || ed.ActiveHandle3D() != Handle3DNone {
				g.suppressPress = true
			}
			invert := g.snapInvert != nil && g.snapInvert()
			ed.withSnapInvert(invert, func() {
				ed.EndViewport3DDrag()
			})
			g.began = false
		}
		g.panAction = Navigation3DActionNone
		g.hasLeftMods = false
		g.lastPan = core.Point{}
	case core.GestureCancelled:
		if g.began {
			ed.CancelViewport3DDrag()
			g.began = false
		}
		g.panAction = Navigation3DActionNone
		g.hasLeftMods = false
		g.lastPan = core.Point{}
	}
}

// flyKeys3D are the keys that move the flying camera, which take them from the gizmo's shortcuts (W and E) while it flies.
var flyKeys3D = []string{"w", "a", "s", "d", "q", "e"}

// maxFlyStep3D caps the time one frame moves the flying camera for, so a stalled frame does not throw it across the scene.
const maxFlyStep3D = 100 * time.Millisecond

// flyView3D runs while the camera flies (Flying3D): it binds the movement keys, so W and E move the camera instead of switching the gizmo, and on every display frame moves it by the keys held, W and S forward and back, A and D left and right, Q and E down and up, Shift faster.
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

// axisWidgetSize3D is the diameter of the orientation widget in points, matching Viewport3D.dc.html,
// and axisWidgetReach3D is how far from its center each world axis end lies.
const (
	axisWidgetSize3D  float32 = 88
	axisWidgetReach3D float32 = 28
	axisMarkRadius3D  float32 = 9
)

// axisMark3D is one end of a world axis on the orientation widget, and the view pressing it turns to.
type axisMark3D struct {
	id       string
	view     View3D
	label    string
	ariaName string
	positive bool
	color    core.Color
	ink      core.Color
	// x and y are its place in the widget, and depth how far it points away from the viewer, from -1 (at the viewer) to 1.
	x, y, depth float32
}

// axisMarks3D returns the six axis ends of the orientation widget for camera c using the dark Spec palette.
func axisMarks3D(c OrbitCamera3D) []axisMark3D {
	return axisMarks3DFor(c, theme.Dark())
}

// axisMarks3DFor returns the six axis ends of the orientation widget for camera c using palette p, the farthest first, as they are drawn.
func axisMarks3DFor(c OrbitCamera3D, p theme.Palette) []axisMark3D {
	right, up, forward := c.basis64()
	ends := []struct {
		id       string
		dir      [3]float64
		view     View3D
		label    string
		ariaName string
		positive bool
		color    core.Color
		ink      core.Color
	}{
		{"view3d-axis-right", [3]float64{1, 0, 0}, View3DRight, "X", "View from +X (right)", true, p.Problem().Fg, p.Problem().OnFill},
		{"view3d-axis-left", [3]float64{-1, 0, 0}, View3DLeft, "", "View from -X (left)", false, p.Problem().Fg, p.Problem().OnFill},
		{"view3d-axis-top", [3]float64{0, 1, 0}, View3DTop, "Y", "View from +Y (top)", true, p.Added.Fg, p.Added.OnFill},
		{"view3d-axis-bottom", [3]float64{0, -1, 0}, View3DBottom, "", "View from -Y (bottom)", false, p.Added.Fg, p.Added.OnFill},
		{"view3d-axis-front", [3]float64{0, 0, 1}, View3DFront, "Z", "View from +Z (front)", true, p.Accent.Fg, p.Accent.OnFill},
		{"view3d-axis-back", [3]float64{0, 0, -1}, View3DBack, "", "View from -Z (back)", false, p.Accent.Fg, p.Accent.OnFill},
	}
	center := axisWidgetSize3D / 2
	marks := make([]axisMark3D, 0, len(ends))
	for _, end := range ends {
		marks = append(marks, axisMark3D{
			id:       end.id,
			view:     end.view,
			label:    end.label,
			ariaName: end.ariaName,
			positive: end.positive,
			color:    end.color,
			ink:      end.ink,
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

// drawAxisLines3D draws the three positive world-axis spokes from the orientation widget's center.
func drawAxisLines3D(d *core.Drawing, c OrbitCamera3D, p theme.Palette) {
	center := axisWidgetSize3D / 2
	for _, m := range axisMarks3DFor(c, p) {
		if !m.positive {
			continue
		}
		d.Save().SetStrokeColor(m.color).SetLineWidth(2).BeginPath().MoveTo(center, center).LineTo(m.x, m.y).Stroke().Restore()
	}
}

// drawAxisWidget3D draws the full orientation widget onto d for offscreen/canvas tests: a line from the center to each positive end, a Spec-colored lettered disc with WCAG AA ink at each positive end, and a raised bordered disc at each negative end, the nearer over the farther.
func drawAxisWidget3D(d *core.Drawing, c OrbitCamera3D, ink core.Color) {
	p := theme.Dark()
	center := axisWidgetSize3D / 2
	font := core.Font{Size: 10, Weight: 600}
	for _, m := range axisMarks3DFor(c, p) {
		if m.positive {
			labelInk := ink
			if labelInk == 0 {
				labelInk = m.ink
			}
			d.Save().SetStrokeColor(m.color).SetLineWidth(2).BeginPath().MoveTo(center, center).LineTo(m.x, m.y).Stroke().Restore()
			d.Save().SetFillColor(m.color).BeginPath().Arc(m.x, m.y, axisMarkRadius3D, 0, float32(2*math.Pi), false).Fill().Restore()
			d.Save().SetFillColor(labelInk).SetTextAlign(core.TextAlignCenter).SetTextBaseline(core.BaselineMiddle).FillText(m.label, m.x, m.y, font).Restore()
			continue
		}
		d.Save().SetFillColor(p.Raised).SetStrokeColor(m.color).SetLineWidth(1.5).BeginPath().Arc(m.x, m.y, 6, 0, float32(2*math.Pi), false).Fill().Stroke().Restore()
	}
}

func divider3D(p theme.Palette) core.Node {
	return core.View{
		Style: core.Style{
			Width:      core.Pt(1),
			Height:     core.Pt(16),
			Background: p.LineRaised,
			Margin:     core.Symmetric(0, 4),
		},
	}
}

func keycap3D(p theme.Palette, label string) core.Node {
	return core.View{
		Style: core.Style{
			Height:         core.Pt(18),
			Padding:        core.Symmetric(0, 5),
			CornerRadius:   4,
			Background:     p.Panel,
			BorderWidth:    1,
			BorderColor:    p.LineRaised,
			AlignItems:     core.AlignCenter,
			JustifyContent: core.JustifyCenter,
		},
		Children: []core.Node{
			core.Text{
				Content: label,
				Style: core.Style{
					FontFamily: core.FontMonospace,
					FontSize:   10.5,
					Color:      p.TextSecondary,
				},
			},
		},
	}
}

func helpGroup3D(p theme.Palette, keys []string, text string) core.Node {
	children := make([]core.Node, 0, len(keys)+1)
	for _, k := range keys {
		if k != "" {
			children = append(children, keycap3D(p, k))
		}
	}
	children = append(children, core.Text{
		Content: text,
		Style: core.Style{
			FontSize: 11.5,
			Color:    p.TextMuted,
		},
	})
	return core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        6,
		},
		Children: children,
	}
}

func localizedAxisAriaName3D(ed *Editor, v View3D, fallback string) string {
	switch v {
	case View3DRight:
		return ed.T("viewport3d.axisRight")
	case View3DLeft:
		return ed.T("viewport3d.axisLeft")
	case View3DTop:
		return ed.T("viewport3d.axisTop")
	case View3DBottom:
		return ed.T("viewport3d.axisBottom")
	case View3DFront:
		return ed.T("viewport3d.axisFront")
	case View3DBack:
		return ed.T("viewport3d.axisBack")
	default:
		return fallback
	}
}

func localizedViewName3D(ed *Editor) string {
	if ed == nil {
		return ""
	}
	if name, ok := ed.cameraViewName3D(); ok {
		return name
	}
	v, axis := ed.Camera3D.AxisView()
	axisName := func(v View3D) string {
		switch v {
		case View3DFront:
			return ed.T("viewport3d.viewFront")
		case View3DBack:
			return ed.T("viewport3d.viewBack")
		case View3DLeft:
			return ed.T("viewport3d.viewLeft")
		case View3DRight:
			return ed.T("viewport3d.viewRight")
		case View3DTop:
			return ed.T("viewport3d.viewTop")
		case View3DBottom:
			return ed.T("viewport3d.viewBottom")
		default:
			return v.String()
		}
	}
	switch {
	case axis && ed.Camera3D.Orthographic:
		return ed.T("viewport3d.viewOrtho", l10n.Param("view", axisName(v)))
	case axis:
		return axisName(v)
	case ed.Camera3D.Orthographic:
		return ed.T("viewport3d.viewUserOrtho")
	default:
		return ed.T("viewport3d.perspective")
	}
}

func localizedShading3D(ed *Editor, m Shading3D) string {
	switch m {
	case Shading3DUnlit:
		return ed.T("viewport3d.shadingUnlit")
	case Shading3DWireframe:
		return ed.T("viewport3d.shadingWireframe")
	default:
		return ed.T("viewport3d.shadingLit")
	}
}

// viewport3DOverlay renders the 3D viewport HUD strictly with theme.Palette tokens (Dark & Light),
// matching design/editor/Viewport3D.dc.html:
//   - Top-left: Persp/Ortho segmented control, Shading menu (Lit/Unlit/Wireframe), view name, Reset view, Camera/Env toggles, and the keycap help line below it.
//   - Top-right: 88×88 circular orientation widget with clickable, keyboard-focusable axis buttons (+X/-X/+Y/-Y/+Z/-Z) and Orbit 15° left/right buttons.
//   - Bottom-center: Local/Global space segmented control, Snap settings popover, and Focus button.
func viewport3DOverlay(cx *core.Scope, ed *Editor, rev *core.Signal[uint64]) []core.Node {
	p := ed.Palette(cx)
	focusRing := p.FocusRing(2)

	ortho := func() bool {
		_ = rev.Get()
		return ed.Camera3D.Orthographic
	}
	playing := func() bool {
		_ = rev.Get()
		return ed.Playing()
	}
	shortcutLabel := func(cmdID string) string {
		if sc := ed.EffectiveShortcut(cmdID); sc != "" {
			return cx.KeyLabel(sc)
		}
		return ""
	}
	segBtn := func(testID string, textFunc func() string, label string, active func() bool, disabled func() bool, onPress func()) core.Node {
		return base.Button{
			TestID:   testID,
			Label:    label,
			Selected: active,
			Disabled: disabled,
			StyleFunc: func() core.Style {
				on := active()
				isDisabled := disabled != nil && disabled()
				var bg core.Color
				if on {
					bg = p.LineRaised
				}
				var opacity core.Opacity
				if isDisabled {
					opacity = core.Alpha(0.45)
				}
				return core.Style{
					Height:         core.Pt(28),
					Padding:        core.Symmetric(0, 10),
					CornerRadius:   6,
					Background:     bg,
					Opacity:        opacity,
					AlignItems:     core.AlignCenter,
					JustifyContent: core.JustifyCenter,
					Hover:          &core.Style{Background: p.LineRaised},
					FocusVisible:   focusRing,
				}
			},
			OnPress: func(core.PressEvent) { onPress() },
			Children: []core.Node{
				core.Text{
					ContentFunc: textFunc,
					StyleFunc: func() core.Style {
						col := p.TextMuted
						if active() {
							col = p.TextPrimary
						}
						return core.Style{
							FontSize:   12.5,
							FontWeight: 500,
							Color:      col,
						}
					},
				},
			},
		}
	}
	staticText := func(s string) func() string { return func() string { return s } }
	iconBtn := func(testID string, icon func() *core.SVG, label string, fn func()) core.Node {
		return ui.Button{
			TestID:   testID,
			Icon:     icon,
			IconSize: 13,
			Side:     28,
			Label:    label,
			Size:     ui.SizeSmall,
			Variant:  ui.ButtonGhost,
			Tint:     func() core.Color { return p.TextMuted },
			Style: core.Style{
				CornerRadius: 6,
				FocusVisible: focusRing,
			},
			OnPress: fn,
		}
	}

	barStyle := core.Style{
		Direction:    core.Row,
		AlignItems:   core.AlignCenter,
		Gap:          4,
		Padding:      core.All(4),
		CornerRadius: 8,
		Background:   p.PillBg,
		BorderWidth:  1,
		BorderColor:  p.LineRaised,
	}

	shadingMenu := ui.Menu{
		TestID: "view3d-shading",
		Label:  ed.T("viewport3d.shadingMenuLabel"),
		Face: core.View{
			Style: core.Style{
				Direction:    core.Row,
				AlignItems:   core.AlignCenter,
				Gap:          4,
				Height:       core.Pt(28),
				Padding:      core.Symmetric(0, 8),
				CornerRadius: 6,
				Hover:        &core.Style{Background: p.LineRaised},
				FocusVisible: focusRing,
			},
			Children: []core.Node{
				core.Text{
					TestID: "view3d-shading-label",
					ContentFunc: func() string {
						_ = rev.Get()
						return localizedShading3D(ed, ed.Shading3D)
					},
					Style: core.Style{
						FontSize:   12.5,
						FontWeight: 500,
						Color:      p.TextSecondary,
					},
				},
				core.Icon{
					SVG: lucide.ChevronDown(),
					Style: core.Style{
						Width:  core.Pt(12),
						Height: core.Pt(12),
						Color:  p.TextMuted,
					},
				},
			},
		},
		Children: []core.Node{
			ui.MenuItem{
				TestID:   "view3d-shading-lit",
				Text:     localizedShading3D(ed, Shading3DLit),
				OnSelect: func() { ed.SetShading3D(Shading3DLit) },
			},
			ui.MenuItem{
				TestID:   "view3d-shading-unlit",
				Text:     localizedShading3D(ed, Shading3DUnlit),
				OnSelect: func() { ed.SetShading3D(Shading3DUnlit) },
			},
			ui.MenuItem{
				TestID:   "view3d-shading-wireframe",
				Text:     localizedShading3D(ed, Shading3DWireframe),
				OnSelect: func() { ed.SetShading3D(Shading3DWireframe) },
			},
		},
	}

	overlayRow3D := func(testID, text, label string, checked func() bool, onChange func(bool)) core.Node {
		return core.View{
			Style: core.Style{
				Direction:      core.Row,
				AlignItems:     core.AlignCenter,
				JustifyContent: core.JustifySpaceBetween,
				Height:         core.Pt(28),
				Gap:            12,
				Padding:        core.Symmetric(0, 6),
			},
			Children: []core.Node{
				core.Text{
					Content: text,
					Style: core.Style{
						FontSize: 12.5,
						Color:    p.TextSecondary,
					},
				},
				SpecSwitch{
					TestID:   testID,
					Label:    label,
					Size:     ui.SizeSmall,
					Checked:  checked,
					OnChange: onChange,
				},
			},
		}
	}

	overlays3DPopover := ui.Popover{
		TestID:    "view3d-overlays",
		Label:     ed.T("viewport3d.overlays3d"),
		Placement: core.PlaceTop,
		Align:     core.AlignCenter,
		Trigger: ui.Button{
			Icon:     lucide.Layers,
			IconSize: 13,
			Side:     28,
			Label:    ed.T("viewport3d.overlays3d"),
			Size:     ui.SizeSmall,
			Variant:  ui.ButtonGhost,
			Tint: func() core.Color {
				_ = rev.Get()
				if ed.ShowLODOverlay || ed.ShowOccluders || ed.ShowRenderStats || ed.ShowNavMesh3D {
					return p.Accent.Fg
				}
				return p.TextMuted
			},
			Style: core.Style{
				CornerRadius: 6,
				FocusVisible: focusRing,
			},
		},
		Style: core.Style{
			Width:        core.Pt(220),
			Padding:      core.All(8),
			Gap:          4,
			CornerRadius: 12,
			Background:   p.Raised,
			BorderWidth:  1,
			BorderColor:  p.LineRaised,
		},
		Children: []core.Node{
			core.Text{
				Content: ed.T("overlays.popoverTitle"),
				Style: core.Style{
					FontSize:      11,
					FontWeight:    600,
					LetterSpacing: 0.6,
					Color:         p.TextFaint,
					Padding:       core.Edges{Left: 6, Right: 6, Top: 2, Bottom: 4},
				},
			},
			overlayRow3D("viewport3d-toggle-lod", "LOD", "Toggle active LOD indicators in the 3D viewport", func() bool {
				_ = rev.Get()
				return ed.ShowLODOverlay
			}, ed.SetShowLODOverlay),
			overlayRow3D("viewport3d-toggle-occluders", "Occluders", "Toggle occluder volume visualization in the 3D viewport", func() bool {
				_ = rev.Get()
				return ed.ShowOccluders
			}, ed.SetShowOccluders),
			overlayRow3D("viewport3d-toggle-stats", "Render Stats", "Toggle RenderStats overlay in the 3D viewport", func() bool {
				_ = rev.Get()
				return ed.ShowRenderStats
			}, ed.SetShowRenderStats),
			overlayRow3D("viewport3d-toggle-navmesh", "3D NavMesh", "Toggle the 3D navigation mesh overlay in the viewport", func() bool {
				_ = rev.Get()
				return ed.ShowNavMesh3D
			}, ed.SetShowNavMesh3D),
		},
	}

	gridMapIconBtn := func(testID string, icon func() *core.SVG, label string, tool GridMapTool) core.Node {
		active := func() bool {
			_ = rev.Get()
			return ed.GridMapTool() == tool
		}
		return ui.Button{
			TestID:   testID,
			Icon:     icon,
			IconSize: 13,
			Side:     28,
			Label:    label,
			Size:     ui.SizeSmall,
			Variant:  ui.ButtonGhost,
			Disabled: playing,
			Color: func() core.Color {
				if active() {
					return p.LineRaised
				}
				return core.Transparent
			},
			Tint: func() core.Color {
				if active() {
					return p.TextPrimary
				}
				return p.TextMuted
			},
			Style: core.Style{
				CornerRadius: 6,
				FocusVisible: focusRing,
			},
			OnPress: func() {
				if ed.GridMapTool() == tool {
					ed.SetGridMapTool(GridMapToolNone)
				} else {
					ed.SetGridMapTool(tool)
				}
			},
		}
	}

	cameraDisabled := func() bool {
		_ = rev.Get()
		return !ed.LookThroughCamera3D() && !ed.HasSceneCamera3D()
	}

	type axesButtonKey struct {
		dark   bool
		yawQ   int
		pitchQ int
	}
	axesKey := core.NewMemo(cx, func() axesButtonKey {
		_ = rev.Get()
		return axesButtonKey{
			dark:   cx.Dark(),
			yawQ:   int(math.Round(float64(ed.Camera3D.Yaw) * 1000)),
			pitchQ: int(math.Round(float64(ed.Camera3D.Pitch) * 1000)),
		}
	})

	type focusKeyState struct {
		dark     bool
		focusKey string
		nav      Navigation3D
	}
	focusKeyMemo := core.NewMemo(cx, func() focusKeyState {
		_ = rev.Get()
		return focusKeyState{
			dark:     cx.Dark(),
			focusKey: shortcutLabel(CmdFrameSelection),
			nav:      ed.Navigation3D(),
		}
	})

	orthoKey := shortcutLabel(CmdView3DOrthographic)
	camKey := shortcutLabel(CmdView3DCamera)
	perspLabel := ed.T("viewport3d.perspective")
	orthoLabel := ed.T("viewport3d.orthographic")
	if orthoKey != "" {
		perspLabel = ed.T("viewport3d.perspectiveKey", l10n.Param("key", orthoKey))
		orthoLabel = ed.T("viewport3d.orthographicKey", l10n.Param("key", orthoKey))
	}
	camLabel := ed.T("viewport3d.lookThroughCamera")
	if camKey != "" {
		camLabel = ed.T("viewport3d.lookThroughCameraKey", l10n.Param("key", camKey))
	}

	return []core.Node{
		// Top-left stack: 3D viewport bar + keycap help line.
		core.View{
			Style: core.Style{
				Position:   core.Absolute,
				Left:       core.Pt(16),
				Top:        core.Pt(16),
				AlignItems: core.AlignStart,
				Gap:        8,
			},
			Children: []core.Node{
				core.View{
					TestID: "view3d-bar",
					Style:  barStyle,
					Children: []core.Node{
						core.View{
							Role:  core.RoleGroup,
							Label: ed.T("viewport3d.projection"),
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2},
							Children: []core.Node{
								segBtn("view3d-persp", staticText(ed.T("viewport3d.persp")), perspLabel, func() bool { return !ortho() }, nil, func() {
									ed.SetOrthographic3D(false)
								}),
								segBtn("view3d-ortho", staticText(ed.T("viewport3d.ortho")), orthoLabel, ortho, nil, func() {
									ed.SetOrthographic3D(true)
								}),
							},
						},
						divider3D(p),
						shadingMenu,
						divider3D(p),
						core.Text{
							TestID: "view3d-name",
							ContentFunc: func() string {
								_ = rev.Get()
								return localizedViewName3D(ed)
							},
							Style: core.Style{
								FontSize: 12.5,
								Color:    p.TextMuted,
								Padding:  core.Symmetric(0, 8),
							},
						},
						iconBtn("view3d-reset", lucide.House, ed.T("viewport3d.resetView"), ed.ResetView3D),
						divider3D(p),
						segBtn("view3d-camera", staticText(ed.T("viewport3d.camera")), camLabel, func() bool {
							_ = rev.Get()
							return ed.LookThroughCamera3D()
						}, cameraDisabled, func() {
							ed.report(ed.SetLookThroughCamera3D(!ed.LookThroughCamera3D()))
						}),
						segBtn("view3d-env", staticText(ed.T("viewport3d.env")), ed.T("viewport3d.envLabel"), func() bool {
							_ = rev.Get()
							return ed.EnvironmentPanelOpen()
						}, nil, func() {
							ed.SetEnvironmentPanelOpen(!ed.EnvironmentPanelOpen())
						}),
					},
				},
				core.Dynamic(func(cx *core.Scope) core.Node {
					fk := focusKeyMemo.Get()
					hp := theme.PaletteFor(fk.dark)
					orbitKeys := []string{ed.T("viewport3d.keyDrag")}
					panKeys := []string{ed.T("viewport3d.keyMiddle")}
					helpLabel := ed.T("viewport3d.helpDefault")
					switch fk.nav {
					case Navigation3DBlender:
						orbitKeys = []string{ed.T("viewport3d.keyMiddle")}
						panKeys = []string{"Shift", ed.T("viewport3d.keyMiddle")}
						helpLabel = ed.T("viewport3d.helpBlender")
					case Navigation3DMaya:
						orbitKeys = []string{"Alt", ed.T("viewport3d.keyDrag")}
						panKeys = []string{ed.T("viewport3d.keyMiddle")}
						helpLabel = ed.T("viewport3d.helpMaya")
					}
					if fk.focusKey != "" {
						helpLabel += ed.T("viewport3d.helpFocusSuffix", l10n.Param("key", fk.focusKey))
					}
					return core.View{
						TestID: "view3d-help",
						Label:  helpLabel,
						Style: core.Style{
							Direction:    core.Row,
							AlignItems:   core.AlignCenter,
							Gap:          12,
							Height:       core.Pt(28),
							Padding:      core.Symmetric(0, 12),
							CornerRadius: 8,
							Background:   hp.PillBg,
							BorderWidth:  1,
							BorderColor:  hp.LineRaised,
						},
						Children: []core.Node{
							helpGroup3D(hp, orbitKeys, ed.T("viewport3d.helpOrbit")),
							helpGroup3D(hp, panKeys, ed.T("viewport3d.helpPan")),
							helpGroup3D(hp, []string{ed.T("viewport3d.keyRight"), "WASD"}, ed.T("viewport3d.helpFly")),
							helpGroup3D(hp, []string{fk.focusKey}, ed.T("viewport3d.helpFocus")),
						},
					}
				}),
			},
		},

		// Top-right: circular orientation widget with clickable axis buttons + Orbit 15° buttons.
		core.View{
			TestID: "view3d-axes-panel",
			Style: core.Style{
				Position:   core.Absolute,
				Right:      core.Pt(16),
				Top:        core.Pt(16),
				AlignItems: core.AlignCenter,
				Gap:        8,
			},
			Children: []core.Node{
				core.View{
					Role:  core.RoleGroup,
					Label: ed.T("viewport3d.viewOrientation"),
					Style: core.Style{
						Width:        core.Pt(core.Points(axisWidgetSize3D)),
						Height:       core.Pt(core.Points(axisWidgetSize3D)),
						CornerRadius: core.Points(axisWidgetSize3D / 2),
						Background:   p.PillBg,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: []core.Node{
						core.Canvas{
							TestID: "view3d-axes",
							LabelFunc: func() string {
								_ = rev.Get()
								return ed.T("viewport3d.viewAxes", l10n.Param("view", localizedViewName3D(ed)))
							},
							Style: core.Style{
								Position: core.Absolute,
								Left:     core.Pt(0),
								Top:      core.Pt(0),
								Width:    core.Pt(core.Points(axisWidgetSize3D)),
								Height:   core.Pt(core.Points(axisWidgetSize3D)),
							},
							OnPress: func(ev core.PressEvent) {
								if v, ok := pickAxisMark3D(ed.Camera3D, float32(ev.Position.X), float32(ev.Position.Y)); ok {
									ed.SetView3D(v)
								}
							},
							Draw: func(d *core.Drawing, _ core.Size) {
								k := axesKey.Get()
								drawAxisLines3D(d, ed.Camera3D, theme.PaletteFor(k.dark))
							},
						},
						core.Dynamic(func(cx *core.Scope) core.Node {
							k := axesKey.Get()
							ap := theme.PaletteFor(k.dark)
							aRing := ap.FocusRing(2)
							marks := axisMarks3DFor(ed.Camera3D, ap)
							btns := make([]core.Node, 0, len(marks))
							for i, m := range marks {
								mark := m
								size := float32(12)
								slop := core.All(4)
								bg := ap.Raised
								borderW := core.Points(1.5)
								borderCol := mark.color
								if mark.positive {
									size = 18
									slop = core.All(5)
									bg = mark.color
									borderW = 0
									borderCol = 0
								}
								var children []core.Node
								if mark.positive {
									children = []core.Node{
										core.Text{
											Content: mark.label,
											Style: core.Style{
												FontSize:   10,
												FontWeight: 600,
												Color:      mark.ink,
											},
										},
									}
								}
								btns = append(btns, base.Button{
									TestID:  mark.id,
									Label:   localizedAxisAriaName3D(ed, mark.view, mark.ariaName),
									HitSlop: slop,
									Style: core.Style{
										Position:       core.Absolute,
										Left:           core.Pt(core.Points(mark.x - size/2)),
										Top:            core.Pt(core.Points(mark.y - size/2)),
										ZIndex:         i + 1,
										Width:          core.Pt(core.Points(size)),
										Height:         core.Pt(core.Points(size)),
										CornerRadius:   core.Points(size / 2),
										Background:     bg,
										BorderWidth:    borderW,
										BorderColor:    borderCol,
										AlignItems:     core.AlignCenter,
										JustifyContent: core.JustifyCenter,
										FocusVisible:   aRing,
									},
									OnPress: func(core.PressEvent) {
										ed.SetView3D(mark.view)
									},
									Children: children,
								})
							}
							return core.View{
								PointerEvents: core.PointerEventsBoxNone,
								Style: core.Style{
									Position: core.Absolute,
									Left:     core.Pt(0),
									Top:      core.Pt(0),
									Width:    core.Pt(core.Points(axisWidgetSize3D)),
									Height:   core.Pt(core.Points(axisWidgetSize3D)),
								},
								Children: btns,
							}
						}),
					},
				},
				core.View{
					Style: core.Style{
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          2,
						Padding:      core.All(3),
						CornerRadius: 8,
						Background:   p.PillBg,
						BorderWidth:  1,
						BorderColor:  p.LineRaised,
					},
					Children: []core.Node{
						iconBtn("view3d-orbit-left", lucide.RotateCcw, ed.T("viewport3d.orbitLeft"), func() { ed.OrbitView3D(-orbitStep3D, 0) }),
						iconBtn("view3d-orbit-right", lucide.RotateCw, ed.T("viewport3d.orbitRight"), func() { ed.OrbitView3D(orbitStep3D, 0) }),
					},
				},
			},
		},

		// Bottom-center floating pill: Local / Global space, Snap settings, Focus button.
		core.View{
			PointerEvents: core.PointerEventsBoxNone,
			Style: core.Style{
				Position:       core.Absolute,
				Left:           core.Pt(0),
				Right:          core.Pt(0),
				Bottom:         core.Pt(16),
				Direction:      core.Row,
				JustifyContent: core.JustifyCenter,
			},
			Children: []core.Node{
				core.View{
					TestID: "view3d-bottom-bar",
					Style:  barStyle,
					Children: []core.Node{
						core.View{
							Role:  core.RoleGroup,
							Label: ed.T("viewport3d.transformSpace"),
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2},
							Children: []core.Node{
								segBtn("view3d-space-local", func() string {
									_ = rev.Get()
									return ed.T("gizmo.spaceLocal")
								}, ed.T("viewport3d.localTransformSpace"), func() bool {
									_ = rev.Get()
									return ed.GizmoSpace3D == GizmoSpaceLocal
								}, playing, func() {
									ed.SetGizmoSpace3D(GizmoSpaceLocal)
								}),
								segBtn("view3d-space-global", func() string {
									_ = rev.Get()
									return ed.T("gizmo.spaceGlobal")
								}, ed.T("viewport3d.globalTransformSpace"), func() bool {
									_ = rev.Get()
									return ed.GizmoSpace3D == GizmoSpaceGlobal
								}, playing, func() {
									ed.SetGizmoSpace3D(GizmoSpaceGlobal)
								}),
							},
						},
						divider3D(p),
						snapSettingsPopover(cx, ed, rev, "view3d-snap-settings", playing),
						overlays3DPopover,
						divider3D(p),
						core.View{
							Role:  core.RoleGroup,
							Label: ed.T("viewport3d.gridmapGroup"),
							Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 2},
							Children: []core.Node{
								gridMapIconBtn("gridmap-tool-paint", lucide.Paintbrush, ed.T("viewport3d.gridmapPaint"), GridMapToolPaint),
								gridMapIconBtn("gridmap-tool-erase", lucide.Eraser, ed.T("viewport3d.gridmapErase"), GridMapToolErase),
								gridMapIconBtn("gridmap-tool-pick", lucide.Pipette, ed.T("viewport3d.gridmapPick"), GridMapToolPick),
								gridMapIconBtn("gridmap-tool-boxfill", lucide.SquareDashed, ed.T("viewport3d.gridmapBoxFill"), GridMapToolBoxFill),
							},
						},
						divider3D(p),
						core.Dynamic(func(cx *core.Scope) core.Node {
							fk := focusKeyMemo.Get()
							fp := theme.PaletteFor(fk.dark)
							fRing := fp.FocusRing(2)
							label := ed.T("viewport3d.frameSelection")
							if fk.focusKey != "" {
								label = ed.T("viewport3d.frameSelectionKey", l10n.Param("key", fk.focusKey))
							}
							btnChildren := []core.Node{
								core.Icon{
									SVG: lucide.Focus(),
									Style: core.Style{
										Width:  core.Pt(13),
										Height: core.Pt(13),
										Color:  fp.TextSecondary,
									},
								},
								core.Text{
									Content: ed.T("viewport3d.focus"),
									Style: core.Style{
										FontSize:   12.5,
										FontWeight: 500,
										Color:      fp.TextSecondary,
									},
								},
							}
							if fk.focusKey != "" {
								btnChildren = append(btnChildren, keycap3D(fp, fk.focusKey))
							}
							return base.Button{
								TestID:   "view3d-focus",
								Label:    label,
								Disabled: playing,
								StyleFunc: func() core.Style {
									var opacity core.Opacity
									if playing() {
										opacity = core.Alpha(0.45)
									}
									return core.Style{
										Direction:    core.Row,
										AlignItems:   core.AlignCenter,
										Gap:          6,
										Height:       core.Pt(28),
										Padding:      core.Symmetric(0, 10),
										CornerRadius: 6,
										Opacity:      opacity,
										Hover:        &core.Style{Background: fp.LineRaised},
										FocusVisible: fRing,
									}
								},
								OnPress:  func(core.PressEvent) { ed.FrameSelection() },
								Children: btnChildren,
							}
						}),
					},
				},
			},
		},

		// Contextual 3D Place options pill: Snap, Align, and Ground Height while the Place tool is active.
		core.Show{
			When: func() bool {
				_ = rev.Get()
				return !ed.Playing() && ed.Tool == ToolPlace && ed.GridMapTool() == GridMapToolNone
			},
			Then: func(cx *core.Scope) core.Node {
				pp := ed.Palette(cx)
				return core.View{
					TestID: "view3d-place-bar",
					Role:   core.RoleGroup,
					Label:  ed.T("viewport3d.placeOptionsLabel"),
					Style: core.Style{
						Position:     core.Absolute,
						Left:         core.Pt(16),
						Bottom:       core.Pt(60),
						Direction:    core.Row,
						AlignItems:   core.AlignCenter,
						Gap:          4,
						Padding:      core.All(4),
						CornerRadius: 8,
						Background:   pp.PillBg,
						BorderWidth:  1,
						BorderColor:  pp.LineRaised,
					},
					Children: []core.Node{
						core.Text{
							Content: ed.T("viewport3d.placeLabel"),
							Style: core.Style{
								FontSize: 11,
								Color:    pp.TextFaint,
								Padding:  core.Edges{Left: 4, Right: 2},
							},
						},
						segBtn("place3d-snap", staticText(ed.T("viewport3d.placeSnap")), ed.T("viewport3d.placeSnapLabel"), func() bool {
							_ = rev.Get()
							return ed.PlaceSnap3D
						}, playing, func() {
							ed.report(ed.SetPlaceOptions3D(ed.PlaceHeight3D, !ed.PlaceSnap3D, ed.PlaceAlign3D))
						}),
						segBtn("place3d-align", staticText(ed.T("viewport3d.placeAlign")), ed.T("viewport3d.placeAlignLabel"), func() bool {
							_ = rev.Get()
							return ed.PlaceAlign3D
						}, playing, func() {
							ed.report(ed.SetPlaceOptions3D(ed.PlaceHeight3D, ed.PlaceSnap3D, !ed.PlaceAlign3D))
						}),
						divider3D(pp),
						core.Text{
							Content: "Y",
							Style: core.Style{
								FontSize: 11,
								Color:    pp.TextMuted,
								Padding:  core.Edges{Left: 2, Right: 2},
							},
						},
						ui.NumberInput{
							TestID: "place3d-height",
							Label:  ed.T("viewport3d.groundHeightLabel"),
							Step:   0.5,
							Style:  core.Style{Width: core.Pt(68)},
							Value: func() float64 {
								_ = rev.Get()
								return float64(ed.PlaceHeight3D)
							},
							OnChange: func(v float64) {
								ed.report(ed.SetPlaceOptions3D(float32(v), ed.PlaceSnap3D, ed.PlaceAlign3D))
							},
						},
					},
				}
			},
		},
	}
}
