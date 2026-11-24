package scene

import (
	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/nsgo/core"
)

// HostOptions configures the reactive view host returned by Manager.View.
type HostOptions struct {
	// Class sets Tailwind CSS classes on the outer host container view.
	Class string
	// Style sets layout and appearance on the outer host container view. When Width or Height are unset, the missing dimension defaults to 100%. When Position is unset, it defaults to core.Relative.
	Style core.Style
	// StyleFunc optionally supplies reactive styles for the outer host container view.
	StyleFunc func() core.Style
	// Ref optionally attaches a core.Ref to the persistent interactive stage view (where Manager.Actions is attached). Call Ref.Focus once the host is mounted so focused key input reaches Manager.Actions; the stage persists across 2D and 3D scene switches, so focus and pointer lock survive them.
	Ref *core.Ref
	// TestID optionally sets core.View.TestID on the outer host container view.
	TestID string
	// StageTestID optionally sets core.View.TestID on the inner stage view that receives attached input events and hosts mounted scenes.
	StageTestID string
	// TransitionTestID optionally sets core.View.TestID on the top transition overlay canvas.
	TransitionTestID string
	// Label optionally sets core.View.Label on the outer host container view.
	Label string
	// OnLayout optionally receives layout bounds changes of the outer host view.
	OnLayout func(core.Bounds)
	// Alpha optionally returns the interpolation fraction in [0, 1) for 2D scene redraws. Reading a reactive signal inside Alpha (such as a loop tick or interpolation signal) redraws mounted 2D canvases on every loop frame without causing mounted 3D gpu.View scenes to rebuild or commit.
	Alpha func() float64
	// Viewport optionally scales and clips 2D scenes rendered by the host through screen.Viewport. Transparent pushed 2D overlays treat a zero BarColor as core.Transparent so underlying 3D views remain visible in margin areas.
	Viewport *screen.Viewport
	// Children are optional persistent native views (such as a global HUD, audio controls, or debug overlay) mounted above all scenes and transitions.
	Children []core.Node
}

// View returns a reactive NSGo view host that mounts and switches between 2D canvas scenes (Controller.Draw / Controller.Scene) and 3D or native view scenes (Controller.View, such as gpu.View or Overlay3D.View).
//
// Each entered scene is keyed by a unique instance ID via core.Each so mounted views (including 3D gpu.View surfaces and GPU pipelines) stay alive across transition frames and across Push/Pop of 2D overlays, and unmount cleanly when the scene exits. The host owns no state of its own: everything it renders lives on the Manager, so it needs no scope beyond the one it is mounted in.
//
// When Manager.Actions is non-nil at the time View is called, View attaches it to the persistent stage view so touch, mouse, pointer-lock, and focused key input stay wired across 2D <-> 3D scene switches. Key events reach the stage only while it holds focus; focus it through HostOptions.Ref, or call Actions.WatchKeys to read keys regardless of focus.
//
// Across a boundary involving a Controller.View scene:
//   - Fade(d, color) (non-transparent) fades the outgoing scene under a colored overlay canvas over the first half, reveals the incoming scene at the midpoint, and fades the overlay out over the second half.
//   - Crossfade(d) (or Fade(d, 0)) crossfades the outgoing and incoming scene wrappers' Opacity from 1->0 and 0->1.
//   - Wipe(d, dir) uses exact path clipping between 2D scenes and falls back to smooth crossfade opacity when a Controller.View scene is involved.
//   - CustomTransition, or a constructor's Transition whose Draw the caller replaced, runs Transition.Draw directly between 2D scenes, or switches visibility at the midpoint while drawing custom overlay effects on top when a Controller.View scene is involved.
//   - A Transition with a nil Draw shows the incoming scene for its whole Duration, as Manager.Draw does.
func (m *Manager) View(opts HostOptions) core.Node {
	hostStyle := opts.Style
	if hostStyle.Position == 0 {
		hostStyle.Position = core.Relative
	}
	if hostStyle.Width == (core.Length{}) {
		hostStyle.Width = core.Pct(100)
	}
	if hostStyle.Height == (core.Length{}) {
		hostStyle.Height = core.Pct(100)
	}

	if m == nil {
		return core.View{
			Class:     opts.Class,
			Style:     hostStyle,
			StyleFunc: opts.StyleFunc,
			TestID:    opts.TestID,
			Label:     opts.Label,
			OnLayout:  opts.OnLayout,
			Children:  opts.Children,
		}
	}

	entriesNode := core.Each(
		func() []uint64 {
			if m.entriesRev != nil {
				_ = m.entriesRev.Get()
			}
			return m.liveSceneIDs()
		},
		func(id uint64) uint64 { return id },
		func(rowScope *core.Scope, item func() uint64, _ func() int) core.Node {
			id := item()
			ms := m.findManagedScene(id)
			if ms == nil || ms.ctrl == nil {
				return core.View{Style: hiddenFillStyle()}
			}
			return m.buildSceneEntryView(rowScope, ms, opts)
		},
	)

	transitionOverlay := core.Canvas{
		TestID: opts.TransitionTestID,
		Style: core.Style{
			Position:      core.Absolute,
			Top:           core.Pt(0),
			Left:          core.Pt(0),
			Right:         core.Pt(0),
			Bottom:        core.Pt(0),
			PointerEvents: core.PointerEventsNone,
		},
		Draw: func(d *core.Drawing, size core.Size) {
			if m.rev != nil {
				_ = m.rev.Get()
			}
			if m.trans == nil || m.trans.tr == nil {
				return
			}
			alpha := 0.0
			if opts.Alpha != nil {
				alpha = opts.Alpha()
			}
			m.drawHostTransitionOverlay(d, size, alpha, opts.Viewport)
		},
	}

	stage := core.View{
		TestID: opts.StageTestID,
		Ref:    opts.Ref,
		Style: core.Style{
			Position: core.Absolute,
			Top:      core.Pt(0),
			Left:     core.Pt(0),
			Right:    core.Pt(0),
			Bottom:   core.Pt(0),
		},
		Children: []core.Node{
			entriesNode,
			transitionOverlay,
		},
	}
	if m.Actions != nil {
		stage.Focusable = func() bool { return true }
		m.Actions.Attach(&stage)
	}

	children := make([]core.Node, 0, len(opts.Children)+1)
	children = append(children, stage)
	children = append(children, opts.Children...)

	return core.View{
		Class:     opts.Class,
		Style:     hostStyle,
		StyleFunc: opts.StyleFunc,
		TestID:    opts.TestID,
		Label:     opts.Label,
		OnLayout:  opts.OnLayout,
		Children:  children,
	}
}

func fillStyle() core.Style {
	return core.Style{
		Position: core.Absolute,
		Top:      core.Pt(0),
		Left:     core.Pt(0),
		Right:    core.Pt(0),
		Bottom:   core.Pt(0),
	}
}

func hiddenFillStyle() core.Style {
	st := fillStyle()
	st.Visibility = core.VisibilityHidden
	st.PointerEvents = core.PointerEventsNone
	return st
}

func (m *Manager) liveSceneIDs() []uint64 {
	n := len(m.stack)
	if m.trans != nil {
		n += len(m.trans.old)
	}
	if n == 0 {
		return nil
	}
	ids := make([]uint64, 0, n)
	if m.trans != nil {
		for _, ms := range m.trans.old {
			if ms != nil {
				ids = append(ids, ms.id)
			}
		}
	}
	for _, ms := range m.stack {
		if ms != nil {
			ids = append(ids, ms.id)
		}
	}
	return ids
}

func (m *Manager) findManagedScene(id uint64) *managedScene {
	for _, ms := range m.stack {
		if ms != nil && ms.id == id {
			return ms
		}
	}
	if m.trans != nil {
		for _, ms := range m.trans.old {
			if ms != nil && ms.id == id {
				return ms
			}
		}
	}
	return nil
}

func visibleStackStart(stack []*managedScene) int {
	if len(stack) == 0 {
		return 0
	}
	start := len(stack) - 1
	for start > 0 && stack[start].ctrl != nil && stack[start].ctrl.Transparent {
		start--
	}
	return start
}

func stackHasView(stack []*managedScene) bool {
	if len(stack) == 0 {
		return false
	}
	for i := visibleStackStart(stack); i < len(stack); i++ {
		if stack[i] != nil && stack[i].ctrl != nil && stack[i].ctrl.View != nil {
			return true
		}
	}
	return false
}

func (m *Manager) transitionHasView() bool {
	if m.trans == nil {
		return false
	}
	return stackHasView(m.trans.old) || stackHasView(m.stack)
}

func (m *Manager) entryStyle(id uint64) core.Style {
	if m.rev != nil {
		_ = m.rev.Get()
	}

	if m.trans == nil || m.trans.tr == nil {
		idx := -1
		for i, ms := range m.stack {
			if ms != nil && ms.id == id {
				idx = i
				break
			}
		}
		if idx < 0 || idx < visibleStackStart(m.stack) {
			return hiddenFillStyle()
		}
		st := fillStyle()
		st.Visibility = core.VisibilityVisible
		if idx < len(m.stack)-1 {
			st.PointerEvents = core.PointerEventsNone
		} else {
			st.PointerEvents = core.PointerEventsAuto
		}
		return st
	}

	inOld, oldIdx := false, -1
	for i, ms := range m.trans.old {
		if ms != nil && ms.id == id {
			inOld, oldIdx = true, i
			break
		}
	}
	inNew, newIdx := false, -1
	if !inOld {
		for i, ms := range m.stack {
			if ms != nil && ms.id == id {
				inNew, newIdx = true, i
				break
			}
		}
	}
	if !inOld && !inNew {
		return hiddenFillStyle()
	}
	if inOld && oldIdx < visibleStackStart(m.trans.old) {
		return hiddenFillStyle()
	}
	if inNew && newIdx < visibleStackStart(m.stack) {
		return hiddenFillStyle()
	}

	tr := m.trans.tr
	p := clamp01(m.trans.progress)
	hasView := m.transitionHasView()

	switch tr.hostKind() {
	case transitionNone:
		// No Draw: show the incoming stack at once, as Manager.Draw draws only the new stack.
		if inOld {
			return hiddenFillStyle()
		}
		st := fillStyle()
		st.Visibility = core.VisibilityVisible
		if newIdx == len(m.stack)-1 {
			st.PointerEvents = core.PointerEventsAuto
		} else {
			st.PointerEvents = core.PointerEventsNone
		}
		return st

	case transitionFadeColor:
		if p < 0.5 {
			if inNew {
				return hiddenFillStyle()
			}
			st := fillStyle()
			st.Visibility = core.VisibilityVisible
			st.PointerEvents = core.PointerEventsNone
			return st
		}
		if inOld {
			return hiddenFillStyle()
		}
		st := fillStyle()
		st.Visibility = core.VisibilityVisible
		if newIdx == len(m.stack)-1 {
			st.PointerEvents = core.PointerEventsAuto
		} else {
			st.PointerEvents = core.PointerEventsNone
		}
		return st

	case transitionCrossfade:
		if inOld {
			a := float32(1 - p)
			if a <= 0 {
				return hiddenFillStyle()
			}
			st := fillStyle()
			st.Visibility = core.VisibilityVisible
			st.Opacity = core.Alpha(a)
			st.PointerEvents = core.PointerEventsNone
			return st
		}
		a := float32(p)
		if a <= 0 {
			return hiddenFillStyle()
		}
		st := fillStyle()
		st.Visibility = core.VisibilityVisible
		st.Opacity = core.Alpha(a)
		if newIdx == len(m.stack)-1 {
			st.PointerEvents = core.PointerEventsAuto
		} else {
			st.PointerEvents = core.PointerEventsNone
		}
		return st

	case transitionWipe:
		if !hasView {
			// Pure 2D wipe is rendered directly by transitionOverlay for exact clip parity.
			return hiddenFillStyle()
		}
		// View-backed wipe crossfades smoothly without resizing the underlying GPU surface.
		if inOld {
			a := float32(1 - p)
			if a <= 0 {
				return hiddenFillStyle()
			}
			st := fillStyle()
			st.Visibility = core.VisibilityVisible
			st.Opacity = core.Alpha(a)
			st.PointerEvents = core.PointerEventsNone
			return st
		}
		a := float32(p)
		if a <= 0 {
			return hiddenFillStyle()
		}
		st := fillStyle()
		st.Visibility = core.VisibilityVisible
		st.Opacity = core.Alpha(a)
		if newIdx == len(m.stack)-1 {
			st.PointerEvents = core.PointerEventsAuto
		} else {
			st.PointerEvents = core.PointerEventsNone
		}
		return st

	default: // transitionCustom
		if !hasView {
			// Pure 2D custom transition is rendered directly by transitionOverlay.
			return hiddenFillStyle()
		}
		if p < 0.5 {
			if inNew {
				return hiddenFillStyle()
			}
			st := fillStyle()
			st.Visibility = core.VisibilityVisible
			st.PointerEvents = core.PointerEventsNone
			return st
		}
		if inOld {
			return hiddenFillStyle()
		}
		st := fillStyle()
		st.Visibility = core.VisibilityVisible
		if newIdx == len(m.stack)-1 {
			st.PointerEvents = core.PointerEventsAuto
		} else {
			st.PointerEvents = core.PointerEventsNone
		}
		return st
	}
}

func (m *Manager) buildSceneEntryView(rowScope *core.Scope, ms *managedScene, opts HostOptions) core.Node {
	ctrl := ms.ctrl
	id := ms.id

	var children []core.Node
	if ctrl.View != nil {
		if node := ctrl.View(rowScope); node != nil {
			children = append(children, node)
		}
	}

	if ctrl.View == nil || ctrl.Draw != nil || ctrl.Scene != nil {
		canvasStyle := fillStyle()
		if ctrl.View != nil {
			canvasStyle.PointerEvents = core.PointerEventsNone
		}
		children = append(children, core.Canvas{
			Style: canvasStyle,
			Draw: func(d *core.Drawing, size core.Size) {
				if m.rev != nil {
					_ = m.rev.Get()
				}
				alpha := 0.0
				if opts.Alpha != nil {
					alpha = opts.Alpha()
				}
				if ms.exited || ctrl == nil {
					return
				}
				drawController2D(ctrl, d, size, alpha, opts.Viewport)
			},
		})
	}

	return core.View{
		StyleFunc: func() core.Style {
			return m.entryStyle(id)
		},
		Children: children,
	}
}

func drawController2D(ctrl *Controller, d *core.Drawing, size core.Size, alpha float64, vp *screen.Viewport) {
	if ctrl == nil || d == nil {
		return
	}
	if ctrl.Draw == nil && ctrl.Scene == nil {
		return
	}
	drawBody := func(dst *core.Drawing) {
		if ctrl.Draw != nil {
			ctrl.Draw(dst, alpha)
		} else if ctrl.Scene != nil {
			ctrl.Scene.Draw(dst)
		}
	}
	if vp != nil {
		useVP := *vp
		if ctrl.Transparent && useVP.BarColor == 0 {
			useVP.BarColor = core.Transparent
		}
		useVP.Draw(d, size, func(dst *core.Drawing, _ core.Size) {
			drawBody(dst)
		})
		return
	}
	drawBody(d)
}

func (m *Manager) drawHostTransitionOverlay(d *core.Drawing, size core.Size, alpha float64, vp *screen.Viewport) {
	tr := m.trans.tr
	p := clamp01(m.trans.progress)

	switch tr.hostKind() {
	case transitionNone:
		// The incoming stack is shown by entryStyle.

	case transitionFadeColor:
		var overlayAlpha float32
		if p < 0.5 {
			overlayAlpha = float32(p * 2)
		} else {
			overlayAlpha = float32((1 - p) * 2)
		}
		if overlayAlpha > 0 && size.Width > 0 && size.Height > 0 {
			d.Save()
			d.SetAlpha(overlayAlpha)
			d.SetFillColor(tr.fadeColor)
			d.FillRect(0, 0, float32(size.Width), float32(size.Height))
			d.Restore()
		}

	case transitionCrossfade:
		// Handled by wrapper Opacity on outgoing and incoming scenes.

	case transitionWipe:
		if !m.transitionHasView() && tr.Draw != nil {
			drawOld := func(dst *core.Drawing) { drawSceneStackWithViewport(m.trans.old, dst, size, alpha, vp) }
			drawNew := func(dst *core.Drawing) { drawSceneStackWithViewport(m.stack, dst, size, alpha, vp) }
			tr.Draw(d, size, p, drawOld, drawNew)
		}

	default: // transitionCustom
		if tr.Draw == nil {
			return
		}
		if !m.transitionHasView() {
			drawOld := func(dst *core.Drawing) { drawSceneStackWithViewport(m.trans.old, dst, size, alpha, vp) }
			drawNew := func(dst *core.Drawing) { drawSceneStackWithViewport(m.stack, dst, size, alpha, vp) }
			tr.Draw(d, size, p, drawOld, drawNew)
			return
		}
		noop := func(*core.Drawing) {}
		tr.Draw(d, size, p, noop, noop)
	}
}

func drawSceneStackWithViewport(stack []*managedScene, d *core.Drawing, size core.Size, alpha float64, vp *screen.Viewport) {
	if len(stack) == 0 {
		return
	}
	for i := visibleStackStart(stack); i < len(stack); i++ {
		if stack[i] == nil || stack[i].ctrl == nil {
			continue
		}
		drawController2D(stack[i].ctrl, d, size, alpha, vp)
	}
}
