package scene

import (
	"time"

	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/nsgo/core"
)

// Controller defines a managed scene's lifecycle, update, view, and draw hooks.
//
// All fields are optional. When View is set, Manager.View mounts the returned node (such as a 3D gpu.View, an Overlay3D.View, or a native UI tree) in the scene's view scope and unmounts it when the scene exits. When View is nil, Manager.View renders the scene through a 2D core.Canvas using Draw or Scene. When Draw is nil and Scene is non-nil, Manager draws Scene automatically; when Update is nil and Scene is non-nil, Manager updates Scene automatically.
type Controller struct {
	// Scene is an optional Scene drawn and updated when Draw or Update is nil, and targeted by Manager.DispatchPress and Manager.DispatchTouch.
	Scene *Scene
	// Transparent, when true on a pushed overlay scene, lets Manager.Draw and Manager.View also show the underlying scene(s) first.
	Transparent bool
	// View optionally builds a native NSGo view tree for this scene, such as a 3D gpu.View or Overlay3D.View, when Manager.View mounts the scene.
	//
	// Its cx is the mounted row's scope, not the child scope Enter receives: when the scene leaves the Manager, Exit runs and Enter's scope is disposed first, and the row (with everything created under View's cx) is unmounted right after, within the same Switch, Pop, or transition step, so no frame or OnFrame callback runs in between. State that View's nodes read every frame should therefore be created in View or on the Manager's parent scope rather than under Enter's cx.
	View func(cx *core.Scope) core.Node
	// Enter runs once when the scene becomes active, with a child Scope owned by the Manager.
	// Loops, signals, effects, timers, and cleanups created under cx are automatically disposed when the scene exits.
	Enter func(cx *core.Scope)
	// Exit runs once when the scene leaves the Manager (by Switch, Pop, or disposing the Manager's Scope).
	Exit func()
	// Pause runs when another scene is pushed on top of this scene.
	Pause func()
	// Resume runs when the scene on top of this scene is popped.
	Resume func()
	// Update advances the scene by one fixed step dt while it is the active top scene.
	Update func(dt time.Duration)
	// Draw records the scene's frame into d; alpha is the interpolation fraction in [0, 1).
	Draw func(d *core.Drawing, alpha float64)
}

type managedScene struct {
	id     uint64
	ctrl   *Controller
	scope  *core.Scope
	exited bool
}

type activeTransition struct {
	tr       *Transition
	old      []*managedScene
	progress float64
	stop     func()
}

// Manager owns a stack of scenes under a core.Scope, driving their lifecycle hooks, child-scope disposal, view mounting for 2D and 3D scenes, shared input, and animated transitions.
//
// Audio needs no Manager support to outlive a switch: a sound.Mixer created on the Manager's parent scope (the cx passed to NewManager, or an ancestor of it) is not disposed when scenes exit, so its music keeps playing and its SFX groups stay live while scenes reach it through their own closures. A Mixer created under a scene's Enter scope is disposed with that scene.
type Manager struct {
	// Actions is an optional input action map shared across all 2D and 3D scenes managed by this Manager.
	//
	// When non-nil, Manager.View attaches Actions to its persistent stage view so touch, mouse, pointer-lock, and focused key input survive scene switches without re-binding. Set Actions before calling Manager.View: the stage is wired once when View builds it, so assigning Actions afterwards attaches nothing. Key events reach the stage only while it holds focus (focus it through HostOptions.Ref); to read keys regardless of focus, also call Actions.WatchKeys on the Manager's scope. Switch, Push, and Pop call Actions.Reset() by default (unless KeepInputOnSwitch is true) so transient press edges and held keys do not leak into the newly active scene while bindings, axes, and pointer lock remain intact.
	Actions *input.Actions
	// KeepInputOnSwitch, when true, skips calling Actions.Reset() on Switch, Push, and Pop so held keys and buttons stay continuously active across scene changes.
	KeepInputOnSwitch bool

	cx         *core.Scope
	rev        *core.Signal[uint64]
	entriesRev *core.Signal[uint64]
	nextID     uint64
	stack      []*managedScene
	trans      *activeTransition
}

// NewManager returns a Manager whose scenes run inside child scopes of cx.
// Disposing cx exits any active scenes and cancels any running transition.
func NewManager(cx *core.Scope) *Manager {
	m := &Manager{cx: cx}
	if cx != nil {
		cx.Run(func() {
			m.rev = core.NewSignal[uint64](cx, 0)
			m.entriesRev = core.NewSignal[uint64](cx, 0)
			cx.Cleanup(func() {
				m.disposeAll()
			})
		})
	}
	return m
}

func (m *Manager) disposeAll() {
	if m.trans != nil {
		t := m.trans
		m.trans = nil
		if t.stop != nil {
			t.stop()
		}
		for i := len(t.old) - 1; i >= 0; i-- {
			m.exitScene(t.old[i])
		}
	}
	for i := len(m.stack) - 1; i >= 0; i-- {
		m.exitScene(m.stack[i])
	}
	m.stack = nil
}

func (m *Manager) enterScene(ctrl *Controller) *managedScene {
	var child *core.Scope
	m.cx.Run(func() {
		child = m.cx.Child()
	})
	m.nextID++
	ms := &managedScene{id: m.nextID, ctrl: ctrl, scope: child}
	if ctrl.Enter != nil {
		child.Run(func() {
			ctrl.Enter(child)
		})
	}
	return ms
}

func (m *Manager) exitScene(ms *managedScene) {
	if ms == nil || ms.exited {
		return
	}
	ms.exited = true
	if ms.ctrl != nil && ms.ctrl.Exit != nil {
		if ms.scope != nil && !ms.scope.Disposed() {
			ms.scope.Run(func() {
				ms.ctrl.Exit()
			})
		} else {
			ms.ctrl.Exit()
		}
	}
	if ms.scope != nil && !ms.scope.Disposed() {
		ms.scope.Dispose()
	}
}

func (m *Manager) finishTransition() {
	if m.trans == nil {
		return
	}
	t := m.trans
	m.trans = nil
	if t.stop != nil {
		t.stop()
	}
	for i := len(t.old) - 1; i >= 0; i-- {
		m.exitScene(t.old[i])
	}
	m.bumpEntries()
}

func (m *Manager) bump() {
	if m.rev == nil || m.cx == nil || m.cx.Disposed() {
		return
	}
	m.rev.Set(m.rev.GetUntracked() + 1)
}

func (m *Manager) bumpEntries() {
	if m.entriesRev == nil || m.cx == nil || m.cx.Disposed() {
		return
	}
	m.entriesRev.Set(m.entriesRev.GetUntracked() + 1)
}

func (m *Manager) resetInput() {
	if m.Actions != nil && !m.KeepInputOnSwitch {
		m.Actions.Reset()
	}
}

// Invalidate marks the manager's 2D canvases dirty so they redraw on the next frame without rebuilding or unmounting mounted 3D views.
func (m *Manager) Invalidate() {
	if m == nil {
		return
	}
	m.bump()
}

// Switch replaces the current scene stack with next.
//
// When transition is nil or has a non-positive Duration, the old scenes exit and dispose their child scopes immediately before next enters.
// When transition has a positive Duration, next enters immediately while the old scenes remain alive to render until the transition completes on the frame clock, at which point the old scenes exit and dispose their child scopes.
func (m *Manager) Switch(next *Controller, transition *Transition) {
	if m == nil || m.cx == nil || m.cx.Disposed() {
		return
	}
	m.finishTransition()
	old := m.stack
	m.stack = nil
	m.resetInput()

	if next == nil {
		for i := len(old) - 1; i >= 0; i-- {
			m.exitScene(old[i])
		}
		m.bumpEntries()
		m.bump()
		return
	}

	if transition == nil || transition.Duration <= 0 {
		for i := len(old) - 1; i >= 0; i-- {
			m.exitScene(old[i])
		}
		m.stack = []*managedScene{m.enterScene(next)}
		m.bumpEntries()
		m.bump()
		return
	}

	m.stack = []*managedScene{m.enterScene(next)}
	at := &activeTransition{
		tr:       transition,
		old:      old,
		progress: 0,
	}
	m.trans = at
	m.cx.Run(func() {
		at.stop = m.cx.Animate(transition.Duration, core.Linear, func(raw float64) {
			if m.trans != at {
				return
			}
			if raw >= 1 {
				m.finishTransition()
				m.bump()
				return
			}
			at.progress = transition.Curve.At(raw)
			m.bump()
		})
	})
	m.bumpEntries()
	m.bump()
}

// Push pauses the current top scene (if any) and enters next on top of the stack in its own child Scope.
func (m *Manager) Push(next *Controller) {
	if m == nil || m.cx == nil || m.cx.Disposed() || next == nil {
		return
	}
	m.finishTransition()
	m.resetInput()
	if len(m.stack) > 0 {
		prev := m.stack[len(m.stack)-1]
		if prev.ctrl != nil && prev.ctrl.Pause != nil && !prev.scope.Disposed() {
			prev.scope.Run(func() {
				prev.ctrl.Pause()
			})
		}
	}
	ms := m.enterScene(next)
	m.stack = append(m.stack, ms)
	m.bumpEntries()
	m.bump()
}

// Pop exits and disposes the top scene, resumes the underlying scene (if any), and returns the popped Controller (or nil if the stack was empty).
func (m *Manager) Pop() *Controller {
	if m == nil || m.cx == nil || m.cx.Disposed() {
		return nil
	}
	m.finishTransition()
	if len(m.stack) == 0 {
		return nil
	}
	m.resetInput()
	top := m.stack[len(m.stack)-1]
	m.stack[len(m.stack)-1] = nil
	m.stack = m.stack[:len(m.stack)-1]
	m.exitScene(top)

	if len(m.stack) > 0 {
		under := m.stack[len(m.stack)-1]
		if under.ctrl != nil && under.ctrl.Resume != nil && !under.scope.Disposed() {
			under.scope.Run(func() {
				under.ctrl.Resume()
			})
		}
	}
	m.bumpEntries()
	m.bump()
	return top.ctrl
}

// Current returns the active scene at the top of the stack, or nil when empty.
func (m *Manager) Current() *Controller {
	if m == nil || len(m.stack) == 0 {
		return nil
	}
	return m.stack[len(m.stack)-1].ctrl
}

// Depth returns the number of scenes currently on the stack.
func (m *Manager) Depth() int {
	if m == nil {
		return 0
	}
	return len(m.stack)
}

// Stack returns the active scene controllers from bottom to top of the stack, or nil when the stack is empty.
func (m *Manager) Stack() []*Controller {
	if m == nil || len(m.stack) == 0 {
		return nil
	}
	out := make([]*Controller, len(m.stack))
	for i, ms := range m.stack {
		out[i] = ms.ctrl
	}
	return out
}

// Transitioning reports whether a Switch transition is currently running.
func (m *Manager) Transitioning() bool {
	return m != nil && m.trans != nil
}

// TransitionProgress returns the active transition's eased progress in [0, 1], or 1 when no transition is running.
func (m *Manager) TransitionProgress() float64 {
	if m == nil || m.trans == nil {
		return 1
	}
	return m.trans.progress
}

// Update advances the top active scene by dt.
func (m *Manager) Update(dt time.Duration) {
	if m == nil || len(m.stack) == 0 {
		return
	}
	top := m.stack[len(m.stack)-1]
	if top.ctrl == nil || top.scope.Disposed() {
		return
	}
	if top.ctrl.Update != nil {
		top.scope.Run(func() {
			top.ctrl.Update(dt)
		})
	} else if top.ctrl.Scene != nil {
		top.scope.Run(func() {
			top.ctrl.Scene.Update(dt)
		})
	}
}

// Draw draws the active scene (or active transition) into d for a canvas of size.
//
// Reading from Draw inside a core.Canvas subscribes the canvas to scene changes and transition frames automatically.
func (m *Manager) Draw(d *core.Drawing, size core.Size, alpha float64) {
	if m == nil || d == nil {
		return
	}
	if m.rev != nil {
		_ = m.rev.Get()
	}
	if m.trans != nil {
		tr := m.trans
		drawOld := func(dst *core.Drawing) { drawSceneStack(tr.old, dst, alpha) }
		drawNew := func(dst *core.Drawing) { drawSceneStack(m.stack, dst, alpha) }
		if tr.tr.Draw != nil {
			tr.tr.Draw(d, size, tr.progress, drawOld, drawNew)
		} else {
			drawNew(d)
		}
		return
	}
	drawSceneStack(m.stack, d, alpha)
}

func drawSceneStack(stack []*managedScene, d *core.Drawing, alpha float64) {
	if len(stack) == 0 {
		return
	}
	start := len(stack) - 1
	for start > 0 && stack[start].ctrl != nil && stack[start].ctrl.Transparent {
		start--
	}
	for i := start; i < len(stack); i++ {
		ctrl := stack[i].ctrl
		if ctrl == nil {
			continue
		}
		if ctrl.Draw != nil {
			ctrl.Draw(d, alpha)
		} else if ctrl.Scene != nil {
			ctrl.Scene.Draw(d)
		}
	}
}

// DispatchPress forwards ev to the active scene's Scene.DispatchPress when present.
func (m *Manager) DispatchPress(ev core.PressEvent) bool {
	cur := m.Current()
	if cur == nil || cur.Scene == nil {
		return false
	}
	return cur.Scene.DispatchPress(ev)
}

// DispatchTouch forwards ev to the active scene's Scene.DispatchTouch when present.
func (m *Manager) DispatchTouch(ev core.TouchEvent) bool {
	cur := m.Current()
	if cur == nil || cur.Scene == nil {
		return false
	}
	return cur.Scene.DispatchTouch(ev)
}
