package gameui

import (
	"slices"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// Root manages layout, focus navigation, pointer/touch interaction, and rendering for a game UI widget tree.
type Root struct {
	// Child is the root widget of the UI tree (typically a Panel, VBox, or HBox).
	Child Widget
	// Theme styles all widgets under this Root; nil uses DefaultTheme.
	Theme *Theme
	// Viewport optionally maps a fixed virtual resolution onto the physical window bounds,
	// applying scaling and letterboxing in Draw and mapping window pointer/touch input into virtual coordinates.
	Viewport *Viewport
	// FocusRing optionally overrides the focus ring style drawn around the focused widget.
	FocusRing *FocusRingStyle
	// TextScale multiplies every widget font size under this Root, including explicit per-widget and screen-file sizes; it is clamped to [MinTextScale, MaxTextScale], and 0 uses Theme.TextScale.
	TextScale float32

	bounds     core.Bounds
	focusables []Widget
	focusIdx   int
	focused    Widget
	hovered    Widget
	pressed    Widget
	dragging   *Slider
	hasFocus   bool
}

// NewRoot returns a Root wrapping child with the default theme.
func NewRoot(child Widget) *Root {
	return &Root{Child: child, focusIdx: -1}
}

// SetTextScale sets the root text scale and returns the normalized value.
func (r *Root) SetTextScale(scale float32) float32 {
	scale = clampTextScale(scale)
	r.TextScale = scale
	return scale
}

func (r *Root) theme() Theme {
	th := resolveTheme(r.Theme)
	if r.TextScale > 0 {
		th.TextScale = clampTextScale(r.TextScale)
	}
	if r.FocusRing != nil {
		th.FocusRing = r.FocusRing
	}
	return th
}

// Bounds returns the bounds from the last Layout or Draw call.
func (r *Root) Bounds() core.Bounds { return r.bounds }

// Layout computes exact rectangles for every widget in the tree inside bounds and refreshes the focusable widget list.
// When Viewport is set, bounds specifies the physical window rectangle and the widget tree is laid out in virtual coordinates.
func (r *Root) Layout(bounds core.Bounds) {
	r.bounds = bounds
	layoutBounds := bounds
	if r.Viewport != nil {
		if bounds.Size.Width > 0 && bounds.Size.Height > 0 {
			r.Viewport.SetWindowSize(bounds.Size)
		}
		layoutBounds = r.Viewport.VirtualBounds()
	}
	if r.Child == nil {
		r.focusables = r.focusables[:0]
		r.focusIdx = -1
		r.focused = nil
		r.hovered = nil
		r.pressed = nil
		r.dragging = nil
		return
	}
	th := r.theme()
	if r.Child.spec().Hidden {
		zeroTreeBounds(r.Child)
	} else {
		r.Child.layout(layoutBounds, &th)
	}
	r.syncFocusables()
}

// Draw lays out the tree into bounds and draws every visible widget into d.
// When Viewport is set, Draw renders letterbox bars and transforms d into the virtual resolution.
// Caller drawing state (transform, colors, stroke settings, alpha, text align/baseline, and current path) is preserved.
func (r *Root) Draw(d *core.Drawing, bounds core.Bounds) {
	if d == nil || r.Child == nil {
		return
	}
	r.Layout(bounds)
	if r.Child.spec().Hidden {
		return
	}
	th := r.theme()
	if r.Viewport != nil {
		d.Save()
		if bounds.Origin.X != 0 || bounds.Origin.Y != 0 {
			d.Translate(float32(bounds.Origin.X), float32(bounds.Origin.Y))
		}
		r.Viewport.Draw(d, func(d *core.Drawing, _ core.Bounds) {
			r.drawWidget(d, r.Child, &th)
		})
		d.Restore()
		return
	}
	d.Save()
	r.drawWidget(d, r.Child, &th)
	d.Restore()
}

// DrawWithViewport renders the UI tree using vp without permanently replacing r.Viewport.
func (r *Root) DrawWithViewport(d *core.Drawing, vp Viewport) {
	prev := r.Viewport
	r.Viewport = &vp
	r.Draw(d, core.Bounds{
		Size: core.Size{
			Width:  core.Points(vp.WindowWidth),
			Height: core.Points(vp.WindowHeight),
		},
	})
	r.Viewport = prev
}

func (r *Root) drawWidget(d *core.Drawing, w Widget, theme *Theme) {
	if w == nil || w.spec().Hidden {
		return
	}
	w.draw(d, theme, r.StateOf(w))
	for _, c := range w.childWidgets() {
		r.drawWidget(d, c, theme)
	}
}

// StateOf returns the current hover, pressed, focused, and disabled state of w.
func (r *Root) StateOf(w Widget) WidgetState {
	if w == nil {
		return WidgetState{}
	}
	disabled := isDisabled(w)
	return WidgetState{
		Hovered:  !disabled && r.hovered == w,
		Pressed:  !disabled && (r.pressed == w || (r.dragging != nil && Widget(r.dragging) == w)),
		Focused:  !disabled && r.hasFocus && r.focused == w,
		Disabled: disabled,
	}
}

func isDisabled(w Widget) bool {
	switch v := w.(type) {
	case *Button:
		return v.Disabled
	case *TouchButton:
		return v.Disabled
	case *Toggle:
		return v.Disabled
	case *Slider:
		return v.Disabled
	case *List:
		return v.Disabled
	default:
		return false
	}
}

type focusOrderWidget interface {
	focusOrder() int
}

type customHitTestWidget interface {
	hitTestPoint(p core.Point) bool
}

func widgetFocusOrder(w Widget) int {
	if fw, ok := w.(focusOrderWidget); ok {
		return fw.focusOrder()
	}
	return 0
}

func (r *Root) syncFocusables() {
	r.focusables = r.focusables[:0]
	if r.Child != nil {
		collectFocusables(r.Child, &r.focusables)
	}
	hasExplicitOrder := false
	for _, w := range r.focusables {
		if widgetFocusOrder(w) > 0 {
			hasExplicitOrder = true
			break
		}
	}
	if hasExplicitOrder {
		slices.SortStableFunc(r.focusables, func(a, b Widget) int {
			oa, ob := widgetFocusOrder(a), widgetFocusOrder(b)
			switch {
			case oa > 0 && ob > 0:
				return oa - ob
			case oa > 0:
				return -1
			case ob > 0:
				return 1
			default:
				return 0
			}
		})
	}
	if !r.hasFocus || r.focused == nil {
		r.focusIdx = -1
		r.focused = nil
		r.hasFocus = false
	} else {
		idx := -1
		for i, w := range r.focusables {
			if w == r.focused {
				idx = i
				break
			}
		}
		if idx >= 0 {
			r.focusIdx = idx
		} else {
			r.focusIdx = -1
			r.focused = nil
			r.hasFocus = false
		}
	}
	if r.hovered != nil && !r.isInFocusables(r.hovered) {
		r.hovered = nil
	}
	if r.pressed != nil && !r.isInFocusables(r.pressed) {
		r.pressed = nil
	}
	if r.dragging != nil && !r.isInFocusables(r.dragging) {
		r.dragging = nil
	}
}

func (r *Root) isInFocusables(target Widget) bool {
	for _, w := range r.focusables {
		if w == target {
			return true
		}
	}
	return false
}

func collectFocusables(w Widget, out *[]Widget) {
	if w == nil || w.spec().Hidden {
		return
	}
	if w.focusable() {
		*out = append(*out, w)
	}
	for _, c := range w.childWidgets() {
		collectFocusables(c, out)
	}
}

// FocusIndex returns the index of the focused widget in the current list of enabled, visible focusable widgets, or -1 if none is focused.
func (r *Root) FocusIndex() int {
	r.syncFocusables()
	return r.focusIdx
}

// Focused returns the currently focused widget, or nil if none is focused.
func (r *Root) Focused() Widget {
	r.syncFocusables()
	return r.focused
}

// Hovered returns the currently hovered interactive widget, or nil.
func (r *Root) Hovered() Widget {
	r.syncFocusables()
	return r.hovered
}

// Pressed returns the currently pressed interactive widget, or nil.
func (r *Root) Pressed() Widget {
	r.syncFocusables()
	return r.pressed
}

// ClearFocus removes keyboard/gamepad focus from the active widget.
func (r *Root) ClearFocus() {
	r.focusIdx = -1
	r.focused = nil
	r.hasFocus = false
}

// SetFocusIndex focuses the focusable widget at index, or clears focus when index < 0.
func (r *Root) SetFocusIndex(index int) bool {
	r.syncFocusables()
	if index < 0 {
		r.ClearFocus()
		return true
	}
	if index >= len(r.focusables) {
		return false
	}
	r.focusIdx = index
	r.focused = r.focusables[index]
	r.hasFocus = true
	return true
}

// SetFocus focuses w if it is visible and focusable in this tree.
func (r *Root) SetFocus(w Widget) bool {
	r.syncFocusables()
	if w == nil {
		r.ClearFocus()
		return true
	}
	for i, candidate := range r.focusables {
		if candidate == w {
			r.focusIdx = i
			r.focused = candidate
			r.hasFocus = true
			return true
		}
	}
	return false
}

// FocusNext moves focus to the next enabled, focusable widget, wrapping from the end to the beginning.
func (r *Root) FocusNext() bool {
	r.syncFocusables()
	if len(r.focusables) == 0 {
		r.ClearFocus()
		return false
	}
	next := 0
	if r.hasFocus && r.focusIdx >= 0 {
		next = (r.focusIdx + 1) % len(r.focusables)
	}
	return r.SetFocusIndex(next)
}

// FocusPrev moves focus to the previous enabled, focusable widget, wrapping from the beginning to the end.
func (r *Root) FocusPrev() bool {
	r.syncFocusables()
	if len(r.focusables) == 0 {
		r.ClearFocus()
		return false
	}
	prev := len(r.focusables) - 1
	if r.hasFocus && r.focusIdx >= 0 {
		prev = (r.focusIdx - 1 + len(r.focusables)) % len(r.focusables)
	}
	return r.SetFocusIndex(prev)
}

// MoveFocus moves focus geometrically in direction (dx, dy) (for arrow keys and gamepad D-pad/stick),
// checking explicit per-widget NavOverrides first, wrapping along the axis when reaching an edge,
// and falling back to linear order if layout has not run.
func (r *Root) MoveFocus(dx, dy int) bool {
	r.syncFocusables()
	if len(r.focusables) == 0 || (dx == 0 && dy == 0) {
		return false
	}
	if !r.hasFocus || r.focusIdx < 0 {
		if dy < 0 || (dy == 0 && dx < 0) {
			return r.SetFocusIndex(len(r.focusables) - 1)
		}
		return r.SetFocusIndex(0)
	}

	cur := r.focusables[r.focusIdx]
	if dir := dirToNavAction(dx, dy); dir != NavNone {
		if target := widgetNavOverrides(cur).For(dir); target != nil && target != cur && r.isInFocusables(target) {
			return r.SetFocus(target)
		}
	}

	cb := cur.Bounds()
	if cb.Size.Width <= 0 && cb.Size.Height <= 0 {
		if dy > 0 || (dy == 0 && dx > 0) {
			return r.FocusNext()
		}
		return r.FocusPrev()
	}

	nextIdx := spatialFindNext(r.focusables, r.focusIdx, dx, dy, true)
	if nextIdx >= 0 {
		return r.SetFocusIndex(nextIdx)
	}
	return false
}

// Activate triggers the currently focused widget (clicks a Button or flips a Toggle).
func (r *Root) Activate() bool {
	r.syncFocusables()
	if !r.hasFocus || r.focused == nil {
		return false
	}
	return activateWidget(r.focused)
}

func activateWidget(w Widget) bool {
	switch v := w.(type) {
	case *Button:
		if v.Disabled {
			return false
		}
		if v.OnClick != nil {
			v.OnClick()
		}
		return true
	case *TouchButton:
		if v.Disabled {
			return false
		}
		if v.OnClick != nil {
			v.OnClick()
		}
		return true
	case *Toggle:
		if v.Disabled {
			return false
		}
		v.Checked = !v.Checked
		if v.OnChange != nil {
			v.OnChange(v.Checked)
		}
		return true
	case *List:
		if v.Disabled || len(v.Items) == 0 {
			return false
		}
		idx := v.Selected
		if idx < 0 || idx >= len(v.Items) {
			idx = 0
		}
		return v.Select(idx)
	default:
		return false
	}
}

// Adjust steps the currently focused adjustable widget by delta steps (e.g. -1 or +1).
// For a Slider, Value moves by delta * step within [Min, Max]. For a Toggle, delta > 0 checks it and delta < 0 unchecks it.
// For a List, Selected steps by delta clamped to the available items.
func (r *Root) Adjust(delta float32) bool {
	r.syncFocusables()
	if !r.hasFocus || r.focused == nil || delta == 0 {
		return false
	}
	switch v := r.focused.(type) {
	case *Slider:
		if v.Disabled {
			return false
		}
		step := v.effectiveStep()
		v.SetValue(v.Value + delta*step)
		return true
	case *Toggle:
		if v.Disabled {
			return false
		}
		want := delta > 0
		if v.Checked != want {
			v.Checked = want
			if v.OnChange != nil {
				v.OnChange(want)
			}
		}
		return true
	case *List:
		if v.Disabled || len(v.Items) == 0 {
			return false
		}
		step := int(delta)
		if step == 0 {
			if delta > 0 {
				step = 1
			} else {
				step = -1
			}
		}
		return v.Select(v.Selected + step)
	default:
		return false
	}
}

// OnKeyDown handles keyboard navigation, activation, and slider adjustment.
// Returns true if the event was consumed by the UI.
func (r *Root) OnKeyDown(ev core.KeyEvent) bool {
	r.syncFocusables()
	key := strings.ToLower(strings.TrimSpace(ev.Key))
	switch key {
	case "tab":
		if ev.Modifiers.Shift {
			return r.FocusPrev()
		}
		return r.FocusNext()
	case "up", "arrowup":
		return r.MoveFocus(0, -1)
	case "down", "arrowdown":
		return r.MoveFocus(0, 1)
	case "left", "arrowleft":
		if _, ok := r.focused.(*Slider); ok && r.hasFocus {
			return r.Adjust(-1)
		}
		return r.MoveFocus(-1, 0)
	case "right", "arrowright":
		if _, ok := r.focused.(*Slider); ok && r.hasFocus {
			return r.Adjust(1)
		}
		return r.MoveFocus(1, 0)
	case "home":
		if s, ok := r.focused.(*Slider); ok && r.hasFocus && !s.Disabled {
			minV, _ := s.boundsRange()
			s.SetValue(minV)
			return true
		}
		if len(r.focusables) > 0 {
			return r.SetFocusIndex(0)
		}
		return false
	case "end":
		if s, ok := r.focused.(*Slider); ok && r.hasFocus && !s.Disabled {
			_, maxV := s.boundsRange()
			s.SetValue(maxV)
			return true
		}
		if len(r.focusables) > 0 {
			return r.SetFocusIndex(len(r.focusables) - 1)
		}
		return false
	case "enter", "return", "space", " ":
		return r.Activate()
	default:
		return false
	}
}

func (r *Root) mapPoint(p core.Point) (core.Point, bool) {
	vp, ok := p, true
	if r.Viewport != nil {
		vp, ok = r.Viewport.WindowToVirtual(p)
	}
	// The root's layout origin is a virtual-canvas offset, so subtract it after
	// converting the window point into virtual points. The converted point is
	// returned even when it falls outside the viewport, so an active drag keeps
	// tracking along the axis that stayed in bounds.
	return core.Point{X: vp.X - r.bounds.Origin.X, Y: vp.Y - r.bounds.Origin.Y}, ok
}

func clearListHover(w Widget) {
	if l, ok := w.(*List); ok {
		l.hoveredOne = 0
	}
}

func clearListPress(w Widget) {
	if l, ok := w.(*List); ok {
		l.pressedOne = 0
	}
}

// HitTest returns the topmost enabled, focusable widget containing p, or nil.
// When Viewport is set, p is mapped from window coordinates into virtual coordinates and points inside letterbox bars return nil.
func (r *Root) HitTest(p core.Point) Widget {
	r.syncFocusables()
	vp, ok := r.mapPoint(p)
	if !ok {
		return nil
	}
	return r.hitTest(vp)
}

func (r *Root) hitTest(p core.Point) Widget {
	if r.Child == nil {
		return nil
	}
	return hitTestWidget(r.Child, p)
}

func hitTestWidget(w Widget, p core.Point) Widget {
	if w == nil || w.spec().Hidden {
		return nil
	}
	children := w.childWidgets()
	for i := len(children) - 1; i >= 0; i-- {
		if hit := hitTestWidget(children[i], p); hit != nil {
			return hit
		}
	}
	if w.focusable() {
		if ht, ok := w.(customHitTestWidget); ok {
			if ht.hitTestPoint(p) {
				return w
			}
			return nil
		}
		if w.Bounds().Contains(p) {
			return w
		}
	}
	return nil
}

// OnPointerMove updates hover state and smoothly drags any active Slider.
// Returns true if the pointer is over an interactive widget or dragging a Slider.
func (r *Root) OnPointerMove(p core.Point) bool {
	r.syncFocusables()
	vp, ok := r.mapPoint(p)
	if r.dragging != nil {
		r.hovered = r.dragging
		r.dragging.setFromX(float32(vp.X))
		return true
	}
	if !ok {
		clearListHover(r.hovered)
		r.hovered = nil
		return false
	}
	hit := r.hitTest(vp)
	if r.hovered != hit {
		clearListHover(r.hovered)
	}
	r.hovered = hit
	if l, isList := hit.(*List); isList {
		if idx := l.ItemAt(vp); idx >= 0 {
			l.hoveredOne = idx + 1
		} else {
			l.hoveredOne = 0
		}
	}
	return hit != nil
}

// OnPointerDown focuses and presses the interactive widget under p, beginning a drag if it is a Slider.
// Returns true if an interactive widget was hit.
func (r *Root) OnPointerDown(p core.Point) bool {
	r.syncFocusables()
	vp, ok := r.mapPoint(p)
	clearListPress(r.pressed)
	if !ok {
		clearListHover(r.hovered)
		r.hovered = nil
		r.pressed = nil
		r.dragging = nil
		return false
	}
	hit := r.hitTest(vp)
	if r.hovered != hit {
		clearListHover(r.hovered)
	}
	r.hovered = hit
	r.pressed = hit
	r.dragging = nil
	if hit == nil {
		return false
	}
	r.SetFocus(hit)
	switch w := hit.(type) {
	case *Slider:
		r.dragging = w
		w.setFromX(float32(vp.X))
	case *List:
		if idx := w.ItemAt(vp); idx >= 0 {
			w.hoveredOne = idx + 1
			w.pressedOne = idx + 1
		}
	}
	return true
}

// OnPointerUp finishes any active press or Slider drag, activating a Button, Toggle, or List item when released inside it.
// Returns true if a press or drag was handled.
func (r *Root) OnPointerUp(p core.Point) bool {
	r.syncFocusables()
	vp, ok := r.mapPoint(p)
	wasPressed := r.pressed
	wasDragging := r.dragging
	var pressedListIdx int
	if l, isList := wasPressed.(*List); isList {
		pressedListIdx = l.pressedOne - 1
		l.pressedOne = 0
	}
	r.pressed = nil
	r.dragging = nil

	if !ok {
		clearListHover(r.hovered)
		r.hovered = nil
		if wasDragging != nil {
			wasDragging.setFromX(float32(vp.X))
			return true
		}
		return wasPressed != nil
	}

	hit := r.hitTest(vp)
	if r.hovered != hit {
		clearListHover(r.hovered)
	}
	r.hovered = hit
	if l, isList := hit.(*List); isList {
		if idx := l.ItemAt(vp); idx >= 0 {
			l.hoveredOne = idx + 1
		} else {
			l.hoveredOne = 0
		}
	}

	if wasDragging != nil {
		wasDragging.setFromX(float32(vp.X))
		return true
	}
	if wasPressed != nil && wasPressed == hit {
		if l, isList := wasPressed.(*List); isList {
			if idx := l.ItemAt(vp); idx >= 0 && (pressedListIdx < 0 || pressedListIdx == idx) {
				l.Select(idx)
				return true
			}
			return true
		}
		activateWidget(wasPressed)
		return true
	}
	return wasPressed != nil
}

// OnPress handles a core.Canvas PressEvent directly (tapping/clicking a Button, Toggle, Slider, or List, or activating the focused widget on keyboard press).
func (r *Root) OnPress(ev core.PressEvent) bool {
	r.syncFocusables()
	if ev.Keyboard {
		return r.Activate()
	}
	vp, ok := r.mapPoint(ev.Position)
	if !ok {
		return false
	}
	hit := r.hitTest(vp)
	if hit == nil {
		return false
	}
	r.SetFocus(hit)
	r.hovered = hit
	switch w := hit.(type) {
	case *Slider:
		w.setFromX(float32(vp.X))
		return true
	case *List:
		if idx := w.ItemAt(vp); idx >= 0 {
			return w.Select(idx)
		}
		return activateWidget(w)
	default:
		return activateWidget(hit)
	}
}
