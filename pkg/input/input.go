// Package input turns touch regions, mouse buttons, relative mouse motion under a pointer lock, keyboard keys, game controller buttons and sticks, and device motion sensors into named actions and axes sampled once per game.Loop step, saves bindings in core.Storage across launches, and plays haptic and rumble feedback.
package input

import (
	"math"
	"slices"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gamepad"
)

// InputKind says what kind of physical input an Input watches.
type InputKind uint8

const (
	// InputNone is the zero Input, which is never active.
	InputNone InputKind = iota
	// InputTouchRegion is active while any touch finger lies inside a rectangle.
	InputTouchRegion
	// InputMouseButton is active while a mouse button is held down on the view.
	InputMouseButton
	// InputMouseRegion is active while a mouse button is held down inside a rectangle.
	InputMouseRegion
	// InputKey is active while a keyboard key, named as core.KeyEvent.Key names it, is held.
	InputKey
	// InputKeyCode is active while a keyboard key at a physical position, named as core.KeyEvent.Code names it, is held.
	InputKeyCode
	// InputGamepadButton is active while a button is pressed on a game controller the Actions reads.
	InputGamepadButton
)

// Input is one physical trigger bound to an action: a touch region, a mouse button, a mouse button in a region, a key, a key's physical position or a game controller button.
//
// Make one with TouchRegion, MouseButton, MouseRegion, Key, KeyCode or GamepadButton. Its zero value is never active.
type Input struct {
	Kind   InputKind
	Bounds core.Bounds
	Button core.MouseButton
	// Key is the key name of an InputKey, such as "a", "left", "space" or "shift".
	Key string
	// Code is the physical position of an InputKeyCode, such as "KeyW" or "ArrowLeft".
	Code string
	// PadButton is the controller button of an InputGamepadButton input.
	PadButton gamepad.Button
}

// TouchRegion returns an input that is active while any touch finger lies inside rect: from its origin, included, to its far edges, excluded, as core.Bounds.Contains tests.
func TouchRegion(rect core.Bounds) Input {
	return Input{Kind: InputTouchRegion, Bounds: rect}
}

// MouseButton returns an input that is active while button is held down on the view. core.MouseButtonNone is never active.
func MouseButton(button core.MouseButton) Input {
	return Input{Kind: InputMouseButton, Button: button}
}

// MouseRegion returns an input that is active while button is held down and the mouse lies inside rect, as core.Bounds.Contains tests. core.MouseButtonNone is never active.
func MouseRegion(rect core.Bounds, button core.MouseButton) Input {
	return Input{Kind: InputMouseRegion, Bounds: rect, Button: button}
}

type axisBinding struct {
	negative string
	positive string
}

// Actions maps touch regions, mouse buttons, relative mouse motion under a pointer lock, keyboard keys and game controllers to named actions and axes, sampled once per fixed game.Loop step.
//
// Call Attach on the game's view (or forward events to OnTouch, OnPressDown, OnPressUp, OnMouseMove, OnMouseLeave, OnContextMenu, OnPointerDelta, OnKeyDown and OnKeyUp), call WatchKeys to hear the keys held anywhere on the window, call WatchGamepads to read game controllers, and call Step at the start of each Loop.Update. Between steps, Pressed, JustPressed, JustReleased and Axis report that step's snapshot. A quick tap that starts and ends between two steps is latched so it still registers for one step.
//
// The zero value is ready to use. Actions' methods are called on the engine's goroutine, as core.View's callbacks and Loop.Update are.
type Actions struct {
	// MapPoint, when set, transforms a touch or mouse position from the view's coordinates before testing TouchRegion and MouseRegion bounds, such as mapping a view to a letterboxed virtual canvas. Returning false ignores the point for region tests.
	MapPoint func(core.Point) (core.Point, bool)

	bindings   map[string][]Input
	axes       map[string]axisBinding
	motionAxes map[string]motionAxisBinding
	motion     *MotionInput
	motionSnap map[string]float64
	motionInit map[string]bool
	motionGap  uint64
	pads       []*VirtualPad

	// Relative mouse motion under the pointer lock: pending adds up between steps, step is the current snapshot.
	pointerAxes   map[string]pointerAxisBinding
	unlockPointer func()
	pendingDX     float64
	pendingDY     float64
	stepDX        float64
	stepDY        float64

	// Game controllers: the scope polled on each Step, the last state read, the selected controller and the stick axes bound with BindGamepadAxis.
	gpScope *core.Scope
	gpPads  []gamepad.Pad
	gpID    int
	gpOne   bool
	gpAxes  map[string]gamepadAxisBinding
	gpSnap  map[string]float64
	// gpMuted holds, per controller ID, a bit mask of the buttons held across Reset, which stay inactive until released.
	gpMuted map[int]uint32

	// Raw physical state as reported by events.
	touches     map[int]core.Point
	mousePos    core.Point
	hasMousePos bool
	pressDown   bool
	moveButton  core.MouseButton
	rightPulse  bool
	// keysDown holds the keys a focused view heard go down (OnKeyDown) and not yet come up, by physical position or, without one, by name; watchedKeys and watchedCodes are the keys held on the window as WatchKeys last read them.
	keysDown     map[string]heldKey
	watchedKeys  []string
	watchedCodes []string
	// ignoredKeys and ignoredCodes are the keys held on the window as Reset was called, which WatchKeys ignores until they come up.
	ignoredKeys  []string
	ignoredCodes []string

	// Per-action rising-edge latches and step snapshots.
	rawActive map[string]bool
	latched   map[string]bool
	curr      map[string]bool
	prev      map[string]bool
	padAxes   map[string]float64

	// Programmatically injected holds and axis contributions (e.g. tests, cutscenes, MCP automation).
	injectedHeld     map[string]bool
	injectedAxes     map[string]float64
	injectedAxisSnap map[string]float64
}

// NewActions returns an Actions with no bindings.
func NewActions() *Actions {
	return &Actions{}
}

// Bind binds name to inputs, replacing any previous inputs for name. Calling Bind with no inputs unbinds name. A key name is lowercased, as Key does, so Bindings returns it lowercased.
func (a *Actions) Bind(name string, inputs ...Input) {
	if len(inputs) == 0 {
		a.Unbind(name)
		return
	}
	if a.bindings == nil {
		a.bindings = make(map[string][]Input)
	}
	list := make([]Input, len(inputs))
	for i, in := range inputs {
		list[i] = normalizeInput(in)
	}
	a.bindings[name] = list
	if a.rawActive == nil {
		a.rawActive = make(map[string]bool)
	}
	a.rawActive[name] = a.activeNow(name)
	delete(a.latched, name)
}

// Unbind removes all inputs bound to name.
func (a *Actions) Unbind(name string) {
	delete(a.bindings, name)
	delete(a.rawActive, name)
	delete(a.latched, name)
}

// Bindings returns a copy of the inputs currently bound to name, or nil if name is unbound.
func (a *Actions) Bindings(name string) []Input {
	in := a.bindings[name]
	if len(in) == 0 {
		return nil
	}
	return slices.Clone(in)
}

// BindAxis binds axis name to two opposite actions: negative drives it toward -1 and positive toward +1. Calling BindAxis with two empty names unbinds the axis.
func (a *Actions) BindAxis(name, negative, positive string) {
	if negative == "" && positive == "" {
		a.UnbindAxis(name)
		return
	}
	if a.axes == nil {
		a.axes = make(map[string]axisBinding)
	}
	a.axes[name] = axisBinding{negative: negative, positive: positive}
}

// UnbindAxis removes the axis binding for name.
func (a *Actions) UnbindAxis(name string) {
	delete(a.axes, name)
}

// AxisBinding returns the negative and positive action names bound to axis name.
func (a *Actions) AxisBinding(name string) (negative, positive string, ok bool) {
	ax, ok := a.axes[name]
	return ax.negative, ax.positive, ok
}

// Trigger latches action name so it registers as pressed on the next Step call, even if name has no physical Input bindings.
func (a *Actions) Trigger(name string) {
	if name == "" {
		return
	}
	if a.latched == nil {
		a.latched = make(map[string]bool)
	}
	a.latched[name] = true
}

// Press holds action name pressed across subsequent Step calls until Release or Reset is called, even if name has no physical Input bindings.
// Like physical input, a Press followed by Release between two Step calls is latched so it still registers for one step.
func (a *Actions) Press(name string) {
	if name == "" {
		return
	}
	wasActive := a.rawActive[name] || a.injectedHeld[name]
	if a.injectedHeld == nil {
		a.injectedHeld = make(map[string]bool)
	}
	a.injectedHeld[name] = true
	if !wasActive {
		if a.latched == nil {
			a.latched = make(map[string]bool)
		}
		a.latched[name] = true
	}
	if a.rawActive == nil {
		a.rawActive = make(map[string]bool)
	}
	a.rawActive[name] = true
}

// Release ends an injected hold started by Press for action name.
func (a *Actions) Release(name string) {
	if name == "" {
		return
	}
	delete(a.injectedHeld, name)
	if a.rawActive != nil {
		a.rawActive[name] = a.activeNow(name)
	}
}

// SetAxis sets an injected analog contribution in [-1, 1] for axis name, sampled on each subsequent Step until ClearAxis or Reset is called.
// Non-finite (NaN) values are ignored; values outside [-1, 1] are clamped.
func (a *Actions) SetAxis(name string, value float64) {
	if name == "" || math.IsNaN(value) {
		return
	}
	if a.injectedAxes == nil {
		a.injectedAxes = make(map[string]float64)
	}
	a.injectedAxes[name] = clampAxis(value)
}

// ClearAxis removes an injected analog contribution previously set with SetAxis for axis name.
func (a *Actions) ClearAxis(name string) {
	delete(a.injectedAxes, name)
}

// ActionNames returns the sorted names of all actions bound, referenced by axes or virtual pads, or currently active or injected on a.
func (a *Actions) ActionNames() []string {
	if a == nil {
		return nil
	}
	seen := make(map[string]struct{})
	add := func(name string) {
		if name != "" {
			seen[name] = struct{}{}
		}
	}
	for name := range a.bindings {
		add(name)
	}
	for _, ax := range a.axes {
		add(ax.negative)
		add(ax.positive)
	}
	for _, pad := range a.pads {
		for _, btn := range pad.Buttons {
			if btn != nil {
				add(btn.Action)
			}
		}
	}
	for name, held := range a.injectedHeld {
		if held {
			add(name)
		}
	}
	for name, ok := range a.curr {
		if ok {
			add(name)
		}
	}
	for name, ok := range a.latched {
		if ok {
			add(name)
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// AxisNames returns the sorted names of all axes bound or injected on a.
func (a *Actions) AxisNames() []string {
	if a == nil {
		return nil
	}
	seen := make(map[string]struct{})
	add := func(name string) {
		if name != "" {
			seen[name] = struct{}{}
		}
	}
	for name := range a.axes {
		add(name)
	}
	for name := range a.motionAxes {
		add(name)
	}
	for name := range a.pointerAxes {
		add(name)
	}
	for name := range a.gpAxes {
		add(name)
	}
	for _, pad := range a.pads {
		for _, stick := range pad.Sticks {
			if stick != nil {
				add(stick.axisXKey())
				add(stick.axisYKey())
			}
		}
	}
	for name := range a.injectedAxes {
		add(name)
	}
	for name := range a.injectedAxisSnap {
		add(name)
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// BindVirtualPad connects pad to a so its virtual buttons drive their bound actions and its virtual sticks drive their named axes on each Step.
func (a *Actions) BindVirtualPad(pad *VirtualPad) {
	if pad == nil {
		return
	}
	if !slices.Contains(a.pads, pad) {
		a.pads = append(a.pads, pad)
	}
	pad.actions = a
	a.syncRaw()
}

// UnbindVirtualPad disconnects pad from a.
func (a *Actions) UnbindVirtualPad(pad *VirtualPad) {
	if pad == nil {
		return
	}
	if i := slices.Index(a.pads, pad); i >= 0 {
		a.pads = slices.Delete(a.pads, i, i+1)
	}
	if pad.actions == a {
		pad.actions = nil
	}
	a.syncRaw()
}

// Step samples the current input state into the step snapshot. Call it once at the start of each game.Loop.Update step.
func (a *Actions) Step() {
	a.pollGamepads()
	a.prev, a.curr = a.curr, a.prev
	clear(a.curr)
	clear(a.padAxes)
	clear(a.injectedAxisSnap)
	if (len(a.bindings) > 0 || len(a.latched) > 0 || len(a.pads) > 0 || len(a.injectedHeld) > 0) && a.curr == nil {
		a.curr = make(map[string]bool, len(a.bindings)+len(a.latched)+len(a.injectedHeld))
	}
	for name := range a.bindings {
		now := a.activeNow(name)
		if a.rawActive != nil {
			a.rawActive[name] = now
		}
		if now || a.latched[name] {
			a.curr[name] = true
		}
	}
	for _, pad := range a.pads {
		for _, btn := range pad.Buttons {
			if btn == nil || btn.Action == "" {
				continue
			}
			if _, bound := a.bindings[btn.Action]; bound {
				continue
			}
			now := a.padPressed(btn.Action)
			if a.rawActive != nil {
				a.rawActive[btn.Action] = now
			}
			if now || a.latched[btn.Action] {
				a.curr[btn.Action] = true
			}
		}
		for _, stick := range pad.Sticks {
			if stick == nil {
				continue
			}
			a.recordPadAxis(stick.axisXKey(), stick.AxisX)
			a.recordPadAxis(stick.axisYKey(), stick.AxisY)
		}
	}
	for name, held := range a.injectedHeld {
		if held {
			a.curr[name] = true
		}
	}
	for name, ok := range a.latched {
		if ok {
			a.curr[name] = true
		}
	}
	clear(a.latched)
	if len(a.injectedAxes) > 0 {
		if a.injectedAxisSnap == nil {
			a.injectedAxisSnap = make(map[string]float64, len(a.injectedAxes))
		}
		for name, v := range a.injectedAxes {
			a.injectedAxisSnap[name] = v
		}
	}
	a.stepMotion()
	a.stepPointer()
	a.stepGamepadAxes()
}

func (a *Actions) recordPadAxis(name string, v float64) {
	if name == "" || v == 0 {
		return
	}
	if a.padAxes == nil {
		a.padAxes = make(map[string]float64)
	}
	a.padAxes[name] = clampAxis(a.padAxes[name] + v)
}

// Reset clears active touches, held mouse buttons, held keys, the last game controller state read, pending latches, motion readings, relative mouse motion and the current step snapshot, keeping all action, axis and calibration settings and any pointer lock, so mouselook survives a scene change; a game controller button held across Reset stays inactive until it is released, as a held mouse button does. UnlockPointer releases the lock. Call it on focus loss, backgrounding or a scene change so no action stays stuck.
func (a *Actions) Reset() {
	clear(a.touches)
	a.mousePos = core.Point{}
	a.hasMousePos = false
	a.pressDown = false
	a.moveButton = core.MouseButtonNone
	a.rightPulse = false
	clear(a.keysDown)
	a.ignoreWatchedKeys()
	clear(a.rawActive)
	clear(a.latched)
	clear(a.curr)
	clear(a.prev)
	a.resetMotion()
	a.resetPointer()
	a.muteHeldButtons()
	a.gpPads = a.gpPads[:0]
	clear(a.gpSnap)
	clear(a.padAxes)
	clear(a.injectedHeld)
	clear(a.injectedAxes)
	clear(a.injectedAxisSnap)
	for _, pad := range a.pads {
		pad.Reset()
	}
}

// Pressed reports whether name is held during the current step.
func (a *Actions) Pressed(name string) bool {
	return a.curr[name]
}

// JustPressed reports whether name became pressed on the current step and was not pressed on the previous step.
func (a *Actions) JustPressed(name string) bool {
	return a.curr[name] && !a.prev[name]
}

// JustReleased reports whether name was pressed on the previous step and is no longer pressed on the current step.
func (a *Actions) JustReleased(name string) bool {
	return !a.curr[name] && a.prev[name]
}

// Axis returns the value of name in [-1, 1] for the current step.
//
// For an axis bound with BindAxis, its negative action contributes -1 and its positive action contributes +1.
// For an axis bound with BindMotionAxis, the calibrated and dead-zoned motion reading contributes a value in [-1, 1].
// For an axis bound with BindPointerAxis, the step's scaled relative mouse motion contributes, as Delta reads it.
// For an axis bound with BindGamepadAxis, the dead-zoned stick reading of the controllers a reads contributes a value in [-1, 1].
// An analog value from a VirtualPad stick bound to the same axis name is added too, as is any injected value set with SetAxis.
// When more than one is bound to the same axis name, their contributions are summed and clamped to [-1, 1].
// Otherwise Axis returns 1 when action name is pressed and 0 when it is not.
func (a *Actions) Axis(name string) float64 {
	var v float64
	hasAxis := false
	if ax, ok := a.axes[name]; ok {
		hasAxis = true
		if ax.negative != "" && a.Pressed(ax.negative) {
			v -= 1
		}
		if ax.positive != "" && a.Pressed(ax.positive) {
			v += 1
		}
	}
	if _, ok := a.motionAxes[name]; ok {
		hasAxis = true
		v += a.motionSnap[name]
	}
	if _, ok := a.pointerAxes[name]; ok {
		hasAxis = true
		v += a.Delta(name)
	}
	if _, ok := a.gpAxes[name]; ok {
		hasAxis = true
		v += a.gpSnap[name]
	}
	if pv, ok := a.padAxes[name]; ok {
		hasAxis = true
		v = clampAxis(v + pv)
	}
	if iv, ok := a.injectedAxisSnap[name]; ok {
		hasAxis = true
		v = clampAxis(v + iv)
	}
	if hasAxis {
		return clampUnit(v)
	}
	if a.Pressed(name) {
		return 1
	}
	return 0
}

// OnTouch records a touch event from a view's OnTouch callback.
func (a *Actions) OnTouch(ev core.TouchEvent) {
	switch ev.Phase {
	case core.TouchStart, core.TouchMove:
		if len(ev.Touches) > 0 && a.touches == nil {
			a.touches = make(map[int]core.Point, len(ev.Touches))
		}
		for _, t := range ev.Touches {
			a.touches[t.ID] = t.Position
		}
	case core.TouchEnd, core.TouchCancel:
		if len(ev.Touches) == 0 {
			if ev.Phase == core.TouchCancel {
				clear(a.touches)
			}
		} else {
			hasChanged := false
			for _, t := range ev.Touches {
				if t.Changed {
					hasChanged = true
					break
				}
			}
			for _, t := range ev.Touches {
				if t.Changed || !hasChanged {
					delete(a.touches, t.ID)
				} else {
					if a.touches == nil {
						a.touches = make(map[int]core.Point, len(ev.Touches))
					}
					a.touches[t.ID] = t.Position
				}
			}
		}
	}
	for _, pad := range a.pads {
		pad.handleTouch(ev, a.MapPoint)
	}
	a.syncRaw()
}

// OnPressDown records a primary pointer press from a view's OnPressDown callback. Keyboard presses and presses synthesized while a touch finger is active are ignored so touches only trigger TouchRegion bindings.
func (a *Actions) OnPressDown(ev core.PressEvent) {
	if ev.Keyboard || len(a.touches) > 0 {
		return
	}
	a.mousePos = ev.Position
	a.hasMousePos = true
	a.pressDown = true
	a.syncRaw()
}

// OnPressUp records a primary pointer release from a view's OnPressUp callback.
func (a *Actions) OnPressUp(ev core.PressEvent) {
	if !ev.Keyboard && len(a.touches) == 0 {
		a.mousePos = ev.Position
		a.hasMousePos = true
	}
	a.pressDown = false
	if a.moveButton == core.MouseButtonLeft {
		a.moveButton = core.MouseButtonNone
	}
	a.syncRaw()
}

// OnMouseMove records mouse movement and any held button from a view's OnMouseMove callback.
func (a *Actions) OnMouseMove(ev core.MouseEvent) {
	a.mousePos = ev.Position
	a.hasMousePos = true
	prev := a.moveButton
	a.moveButton = ev.PressedButton
	if prev == core.MouseButtonLeft && ev.PressedButton != core.MouseButtonLeft {
		a.pressDown = false
	}
	a.syncRaw()
}

// OnMouseLeave clears held mouse buttons when the pointer leaves the view.
func (a *Actions) OnMouseLeave(ev core.MouseEvent) {
	a.mousePos = ev.Position
	a.hasMousePos = false
	a.pressDown = false
	a.moveButton = core.MouseButtonNone
	a.syncRaw()
}

// OnContextMenu records a secondary (right) click from a view's OnContextMenu callback as a single-step pulse of core.MouseButtonRight at ev.Position.
func (a *Actions) OnContextMenu(ev core.ContextMenuEvent) {
	a.mousePos = ev.Position
	a.hasMousePos = true
	a.rightPulse = true
	a.syncRaw()
	a.rightPulse = false
	a.syncRaw()
}

// Attach wires a's touch, press, mouse, context-menu, pointer-lock motion and key handlers onto v, chaining any callbacks already set on v. v hears keys only while it has the keyboard focus; WatchKeys hears them anywhere on the window. A key held as v leaves the tree stays held until Reset, as OnKeyUp says.
func (a *Actions) Attach(v *core.View) {
	if v == nil {
		return
	}
	if prev := v.OnTouch; prev != nil {
		v.OnTouch = func(ev core.TouchEvent) { a.OnTouch(ev); prev(ev) }
	} else {
		v.OnTouch = a.OnTouch
	}
	if prev := v.OnPressDown; prev != nil {
		v.OnPressDown = func(ev core.PressEvent) { a.OnPressDown(ev); prev(ev) }
	} else {
		v.OnPressDown = a.OnPressDown
	}
	if prev := v.OnPressUp; prev != nil {
		v.OnPressUp = func(ev core.PressEvent) { a.OnPressUp(ev); prev(ev) }
	} else {
		v.OnPressUp = a.OnPressUp
	}
	if prev := v.OnMouseMove; prev != nil {
		v.OnMouseMove = func(ev core.MouseEvent) { a.OnMouseMove(ev); prev(ev) }
	} else {
		v.OnMouseMove = a.OnMouseMove
	}
	if prev := v.OnMouseLeave; prev != nil {
		v.OnMouseLeave = func(ev core.MouseEvent) { a.OnMouseLeave(ev); prev(ev) }
	} else {
		v.OnMouseLeave = a.OnMouseLeave
	}
	if prev := v.OnContextMenu; prev != nil {
		v.OnContextMenu = func(ev core.ContextMenuEvent) { a.OnContextMenu(ev); prev(ev) }
	} else {
		v.OnContextMenu = a.OnContextMenu
	}
	if prev := v.OnPointerDelta; prev != nil {
		v.OnPointerDelta = func(dx, dy float64) { a.OnPointerDelta(dx, dy); prev(dx, dy) }
	} else {
		v.OnPointerDelta = a.OnPointerDelta
	}
	if prev := v.OnKeyDown; prev != nil {
		v.OnKeyDown = func(ev core.KeyEvent) bool { consumed := a.OnKeyDown(ev); return prev(ev) || consumed }
	} else {
		v.OnKeyDown = a.OnKeyDown
	}
	if prev := v.OnKeyUp; prev != nil {
		v.OnKeyUp = func(ev core.KeyEvent) { a.OnKeyUp(ev); prev(ev) }
	} else {
		v.OnKeyUp = a.OnKeyUp
	}
}

// syncRaw updates rawActive for every bound action and latches any rising edge so a tap between two Step calls is not lost.
func (a *Actions) syncRaw() {
	if len(a.bindings) == 0 && len(a.pads) == 0 {
		return
	}
	if a.rawActive == nil {
		a.rawActive = make(map[string]bool, len(a.bindings))
	}
	for name := range a.bindings {
		now := a.activeNow(name)
		if now && !a.rawActive[name] {
			if a.latched == nil {
				a.latched = make(map[string]bool)
			}
			a.latched[name] = true
		}
		a.rawActive[name] = now
	}
	for _, pad := range a.pads {
		for _, btn := range pad.Buttons {
			if btn == nil || btn.Action == "" {
				continue
			}
			if _, bound := a.bindings[btn.Action]; bound {
				continue
			}
			now := a.padPressed(btn.Action)
			if now && !a.rawActive[btn.Action] {
				if a.latched == nil {
					a.latched = make(map[string]bool)
				}
				a.latched[btn.Action] = true
			}
			a.rawActive[btn.Action] = now
		}
	}
}

func (a *Actions) activeNow(name string) bool {
	if a.injectedHeld[name] {
		return true
	}
	for _, in := range a.bindings[name] {
		if a.inputActive(in) {
			return true
		}
	}
	return a.padPressed(name)
}

func (a *Actions) padPressed(name string) bool {
	for _, pad := range a.pads {
		if pad.Pressed(name) {
			return true
		}
	}
	return false
}

func clampAxis(v float64) float64 {
	if v < -1 {
		return -1
	}
	if v > 1 {
		return 1
	}
	return v
}

func (a *Actions) inputActive(in Input) bool {
	switch in.Kind {
	case InputTouchRegion:
		for _, pt := range a.touches {
			if p, ok := a.mapPoint(pt); ok && in.Bounds.Contains(p) {
				return true
			}
		}
	case InputMouseButton:
		return a.mouseButtonDown(in.Button)
	case InputGamepadButton:
		return a.gamepadButtonDown(in.PadButton)
	case InputMouseRegion:
		if !a.hasMousePos || !a.mouseButtonDown(in.Button) {
			return false
		}
		if p, ok := a.mapPoint(a.mousePos); ok && in.Bounds.Contains(p) {
			return true
		}
	case InputKey:
		return a.keyHeld(in.Key)
	case InputKeyCode:
		return a.keyCodeHeld(in.Code)
	}
	return false
}

func (a *Actions) mouseButtonDown(b core.MouseButton) bool {
	if b == core.MouseButtonNone {
		return false
	}
	if b == core.MouseButtonLeft {
		return a.pressDown || a.moveButton == core.MouseButtonLeft
	}
	if b == core.MouseButtonRight && a.rightPulse {
		return true
	}
	return a.moveButton == b
}

func (a *Actions) mapPoint(p core.Point) (core.Point, bool) {
	if a.MapPoint == nil {
		return p, true
	}
	return a.MapPoint(p)
}
