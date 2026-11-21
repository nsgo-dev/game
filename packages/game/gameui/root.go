package gameui

import (
	"math"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
)

// Root manages layout, focus navigation, pointer/touch interaction, and rendering for a game UI widget tree.
type Root struct {
	// Child is the root widget of the UI tree (typically a Panel, VBox, or HBox).
	Child Widget
	// Theme styles all widgets under this Root; nil uses DefaultTheme.
	Theme *Theme

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

func (r *Root) theme() Theme {
	return resolveTheme(r.Theme)
}

// Bounds returns the bounds from the last Layout or Draw call.
func (r *Root) Bounds() core.Bounds { return r.bounds }

// Layout computes exact rectangles for every widget in the tree inside bounds and refreshes the focusable widget list.
func (r *Root) Layout(bounds core.Bounds) {
	r.bounds = bounds
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
		r.Child.layout(bounds, &th)
	}
	r.syncFocusables()
}

// Draw lays out the tree into bounds and draws every visible widget into d.
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
	d.Save()
	r.drawWidget(d, r.Child, &th)
	d.Restore()
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
	case *Toggle:
		return v.Disabled
	case *Slider:
		return v.Disabled
	default:
		return false
	}
}

func (r *Root) syncFocusables() {
	r.focusables = r.focusables[:0]
	if r.Child != nil {
		collectFocusables(r.Child, &r.focusables)
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
// wrapping along the axis when reaching an edge, and falling back to linear order if layout has not run.
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
	cb := cur.Bounds()
	if cb.Size.Width <= 0 && cb.Size.Height <= 0 {
		if dy > 0 || (dy == 0 && dx > 0) {
			return r.FocusNext()
		}
		return r.FocusPrev()
	}

	dirX, dirY := float64(dx), float64(dy)
	norm := math.Hypot(dirX, dirY)
	dirX /= norm
	dirY /= norm

	cx := float64(cb.Origin.X) + float64(cb.Size.Width)/2
	cy := float64(cb.Origin.Y) + float64(cb.Size.Height)/2

	bestIdx := -1
	bestScore := math.Inf(1)

	wrapIdx := -1
	wrapPrimary := math.Inf(1) // most negative primary = farthest on opposite side
	wrapPerp := math.Inf(1)

	const eps = 0.5
	for i, cand := range r.focusables {
		if i == r.focusIdx {
			continue
		}
		nb := cand.Bounds()
		if nb.Size.Width <= 0 && nb.Size.Height <= 0 {
			continue
		}
		nx := float64(nb.Origin.X) + float64(nb.Size.Width)/2
		ny := float64(nb.Origin.Y) + float64(nb.Size.Height)/2
		vx := nx - cx
		vy := ny - cy

		primary := vx*dirX + vy*dirY
		perp := math.Abs(vx*(-dirY) + vy*dirX)

		if primary > eps {
			score := primary + perp*2.5
			if score < bestScore-1e-6 {
				bestScore = score
				bestIdx = i
			}
		} else if primary < -eps && math.Abs(primary) >= perp*0.5 {
			// Candidate lies along the opposite direction for edge wrap-around.
			if primary < wrapPrimary-1e-6 || (math.Abs(primary-wrapPrimary) <= 1e-6 && perp < wrapPerp) {
				wrapPrimary = primary
				wrapPerp = perp
				wrapIdx = i
			}
		}
	}

	if bestIdx >= 0 {
		return r.SetFocusIndex(bestIdx)
	}
	if wrapIdx >= 0 {
		return r.SetFocusIndex(wrapIdx)
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
	case *Toggle:
		if v.Disabled {
			return false
		}
		v.Checked = !v.Checked
		if v.OnChange != nil {
			v.OnChange(v.Checked)
		}
		return true
	default:
		return false
	}
}

// Adjust steps the currently focused adjustable widget by delta steps (e.g. -1 or +1).
// For a Slider, Value moves by delta * step within [Min, Max]. For a Toggle, delta > 0 checks it and delta < 0 unchecks it.
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

// HitTest returns the topmost enabled, focusable widget containing p, or nil.
func (r *Root) HitTest(p core.Point) Widget {
	r.syncFocusables()
	return r.hitTest(p)
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
	if w.focusable() && w.Bounds().Contains(p) {
		return w
	}
	return nil
}

// OnPointerMove updates hover state and smoothly drags any active Slider.
// Returns true if the pointer is over an interactive widget or dragging a Slider.
func (r *Root) OnPointerMove(p core.Point) bool {
	r.syncFocusables()
	if r.dragging != nil {
		r.hovered = r.dragging
		r.dragging.setFromX(float32(p.X))
		return true
	}
	hit := r.hitTest(p)
	r.hovered = hit
	return hit != nil
}

// OnPointerDown focuses and presses the interactive widget under p, beginning a drag if it is a Slider.
// Returns true if an interactive widget was hit.
func (r *Root) OnPointerDown(p core.Point) bool {
	r.syncFocusables()
	hit := r.hitTest(p)
	r.hovered = hit
	r.pressed = hit
	r.dragging = nil
	if hit == nil {
		return false
	}
	r.SetFocus(hit)
	if s, ok := hit.(*Slider); ok {
		r.dragging = s
		s.setFromX(float32(p.X))
	}
	return true
}

// OnPointerUp finishes any active press or Slider drag, activating a Button or Toggle when released inside it.
// Returns true if a press or drag was handled.
func (r *Root) OnPointerUp(p core.Point) bool {
	r.syncFocusables()
	wasPressed := r.pressed
	wasDragging := r.dragging
	r.pressed = nil
	r.dragging = nil

	hit := r.hitTest(p)
	r.hovered = hit

	if wasDragging != nil {
		wasDragging.setFromX(float32(p.X))
		return true
	}
	if wasPressed != nil && wasPressed == hit {
		activateWidget(wasPressed)
		return true
	}
	return wasPressed != nil
}

// OnPress handles a core.Canvas PressEvent directly (tapping/clicking a Button, Toggle, or Slider, or activating the focused widget on keyboard press).
func (r *Root) OnPress(ev core.PressEvent) bool {
	r.syncFocusables()
	if ev.Keyboard {
		return r.Activate()
	}
	hit := r.hitTest(ev.Position)
	if hit == nil {
		return false
	}
	r.SetFocus(hit)
	r.hovered = hit
	if s, ok := hit.(*Slider); ok {
		s.setFromX(float32(ev.Position.X))
		return true
	}
	return activateWidget(hit)
}
