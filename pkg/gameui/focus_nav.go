package gameui

import (
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// ScanOptions configures one-switch focus scanning. Interval is the time between focus changes. Loops limits completed passes when positive, while a zero value scans continuously. Select is the action StepActions treats as the switch that activates the highlighted widget.
type ScanOptions struct {
	Enabled  bool
	Interval time.Duration
	Loops    int
	Select   NavAction
}

// NavAction identifies a logical UI navigation action from keyboard, gamepad, or virtual controls.
type NavAction uint8

const (
	// NavNone is the zero value representing no action.
	NavNone NavAction = iota
	// NavUp moves focus upward.
	NavUp
	// NavDown moves focus downward.
	NavDown
	// NavLeft moves focus left (or decrements a focused Slider when SliderSteps is enabled).
	NavLeft
	// NavRight moves focus right (or increments a focused Slider when SliderSteps is enabled).
	NavRight
	// NavConfirm activates the currently focused widget and invokes OnConfirm.
	NavConfirm
	// NavBack invokes OnBack (e.g. closing a submenu or canceling an action).
	NavBack
)

// String returns a human-readable name for a.
func (a NavAction) String() string {
	switch a {
	case NavUp:
		return "NavUp"
	case NavDown:
		return "NavDown"
	case NavLeft:
		return "NavLeft"
	case NavRight:
		return "NavRight"
	case NavConfirm:
		return "NavConfirm"
	case NavBack:
		return "NavBack"
	default:
		return "NavNone"
	}
}

// NavOverrides specifies explicit per-widget directional neighbors that take precedence over automatic 2D spatial lookup.
type NavOverrides struct {
	Up    Widget
	Down  Widget
	Left  Widget
	Right Widget
}

// For returns the explicit neighbor configured for dir, or nil if unset or non-directional.
func (o NavOverrides) For(dir NavAction) Widget {
	switch dir {
	case NavUp:
		return o.Up
	case NavDown:
		return o.Down
	case NavLeft:
		return o.Left
	case NavRight:
		return o.Right
	default:
		return nil
	}
}

// FocusRingStyle customizes the focus indicator drawn around the currently focused widget.
type FocusRingStyle struct {
	// Color overrides Theme.FocusedBorder when non-zero. Set core.Transparent to disable the default stroke.
	Color core.Color
	// Width overrides Theme.FocusWidth when > 0.
	Width float32
	// Outset expands the ring outward from the widget bounds by Outset points (negative insets it).
	Outset float32
	// CornerRadius overrides the corner radius (0 uses widget/theme default; < 0 uses square corners).
	CornerRadius float32
	// Dash sets an optional line dash pattern for the focus ring stroke.
	Dash []float32
	// Draw replaces the default stroke when non-nil, running inside Save/Restore.
	Draw func(d *core.Drawing, bounds core.Bounds, theme *Theme)
}

// DrawFocusRing renders a customizable focus ring around bounds using style and theme defaults.
func DrawFocusRing(d *core.Drawing, bounds core.Bounds, radius float32, style *FocusRingStyle, theme *Theme) {
	if d == nil {
		return
	}
	x, y := float32(bounds.Origin.X), float32(bounds.Origin.Y)
	w, h := float32(bounds.Size.Width), float32(bounds.Size.Height)
	if w <= 0 || h <= 0 {
		return
	}
	th := resolveTheme(theme)

	if style != nil && style.Draw != nil {
		ringBounds := bounds
		if style.Outset != 0 {
			ringBounds = expandBounds(bounds, style.Outset)
		}
		d.Save()
		style.Draw(d, ringBounds, &th)
		d.Restore()
		return
	}

	col := th.FocusedBorder
	lineW := th.FocusWidth
	outset := float32(0)
	r := radius
	var dash []float32

	if style != nil {
		if style.Color != 0 {
			col = style.Color
		}
		if style.Width > 0 {
			lineW = style.Width
		}
		outset = style.Outset
		if style.CornerRadius != 0 {
			r = resolveRadius(style.CornerRadius, radius)
		}
		dash = style.Dash
	}

	if col == 0 || col == core.Transparent || lineW <= 0 {
		return
	}
	if outset != 0 {
		x -= outset
		y -= outset
		w += 2 * outset
		h += 2 * outset
		if r > 0 {
			r = max(0, r+outset)
		}
	}
	if w <= 0 || h <= 0 {
		return
	}

	if len(dash) > 0 {
		d.Save()
		d.SetLineDash(dash...)
		d.SetStrokeColor(col).
			SetLineWidth(lineW).
			StrokePath(rectPath(x, y, w, h, r))
		d.Restore()
		return
	}
	d.SetStrokeColor(col).
		SetLineWidth(lineW).
		StrokePath(rectPath(x, y, w, h, r))
}

func expandBounds(b core.Bounds, outset float32) core.Bounds {
	return core.Bounds{
		Origin: core.Point{
			X: core.Points(float32(b.Origin.X) - outset),
			Y: core.Points(float32(b.Origin.Y) - outset),
		},
		Size: core.Size{
			Width:  core.Points(max(0, float32(b.Size.Width)+2*outset)),
			Height: core.Points(max(0, float32(b.Size.Height)+2*outset)),
		},
	}
}

// SpatialNavigator drives 2D spatial focus navigation, explicit neighbor overrides,
// action-based keyboard/gamepad input, and customizable focus ring rendering over a Root.
type SpatialNavigator struct {
	// Root is the UI root whose focusable widgets are navigated.
	Root *Root
	// WrapAround enables edge wrap-around when no widget exists further in the requested direction.
	WrapAround bool
	// SliderSteps causes NavLeft and NavRight to adjust a focused Slider by -1/+1 step
	// unless an explicit Left/Right neighbor override is configured for that Slider.
	SliderSteps bool
	// OnConfirm is invoked after NavConfirm activates the focused widget (or when Confirm is called).
	OnConfirm func(focused Widget)
	// OnBack is invoked when NavBack is triggered.
	OnBack func()
	// OnFocusChange is invoked whenever navigation moves focus from prev to next.
	OnFocusChange func(prev, next Widget)
	// FocusRing customizes the focus ring drawn by DrawFocusRing (or attached to Root).
	FocusRing *FocusRingStyle
	// Scan configures optional one-switch scanning. Use SetScanOptions to reset the scan clock after changing it.
	Scan ScanOptions

	overrides   map[Widget]NavOverrides
	scanElapsed time.Duration
	scanLoops   int
}

// FocusManager is an alias for SpatialNavigator.
type FocusManager = SpatialNavigator

// NewSpatialNavigator returns a SpatialNavigator attached to root with wrap-around and slider stepping enabled.
func NewSpatialNavigator(root *Root) *SpatialNavigator {
	return &SpatialNavigator{
		Root:        root,
		WrapAround:  true,
		SliderSteps: true,
	}
}

// SetScanOptions enables or disables one-switch scanning and resets its clock.
func (n *SpatialNavigator) SetScanOptions(opts ScanOptions) {
	if n == nil {
		return
	}
	if opts.Interval <= 0 {
		opts.Interval = 1200 * time.Millisecond
	}
	if opts.Select == NavNone {
		opts.Select = NavConfirm
	}
	if opts.Loops < 0 {
		opts.Loops = 0
	}
	n.Scan = opts
	n.scanElapsed = 0
	n.scanLoops = 0
}

// ScanSelect activates the currently highlighted widget when scanning is enabled. It is suitable for a single switch or eye-tracker action.
func (n *SpatialNavigator) ScanSelect() bool {
	if n == nil || !n.Scan.Enabled {
		return false
	}
	return n.Confirm()
}

// Step advances the scanner by fixed simulation time, moving focus through the root's focus order (wrapping from the last widget to the first) rather than by spatial direction, so rows, grids and columns all scan every widget. It returns true when focus moved.
func (n *SpatialNavigator) Step(dt time.Duration) bool {
	if n == nil || !n.Scan.Enabled || dt <= 0 || n.Root == nil {
		return false
	}
	r := n.Root
	r.syncFocusables()
	if len(r.focusables) == 0 {
		return false
	}
	n.scanElapsed += dt
	changed := false
	for n.scanElapsed >= n.Scan.Interval {
		n.scanElapsed -= n.Scan.Interval
		prev := r.focused
		if !r.FocusNext() {
			break
		}
		changed = true
		if prev != r.focused && n.OnFocusChange != nil {
			n.OnFocusChange(prev, r.focused)
		}
		if r.focusIdx == len(r.focusables)-1 {
			n.scanLoops++
			if n.Scan.Loops > 0 && n.scanLoops >= n.Scan.Loops {
				n.Scan.Enabled = false
				break
			}
		}
	}
	return changed
}

// SetOverrides registers explicit directional neighbor overrides for w on this navigator.
// Navigator overrides take precedence over widget-level Nav fields and automatic spatial search.
func (n *SpatialNavigator) SetOverrides(w Widget, ov NavOverrides) {
	if w == nil {
		return
	}
	if n.overrides == nil {
		n.overrides = make(map[Widget]NavOverrides)
	}
	n.overrides[w] = ov
}

// Overrides returns the navigator-level overrides registered for w (or zero value if none).
func (n *SpatialNavigator) Overrides(w Widget) NavOverrides {
	if n == nil || n.overrides == nil || w == nil {
		return NavOverrides{}
	}
	return n.overrides[w]
}

// ClearOverrides removes any navigator-level overrides registered for w.
func (n *SpatialNavigator) ClearOverrides(w Widget) {
	if n == nil || n.overrides == nil || w == nil {
		return
	}
	delete(n.overrides, w)
}

// Focused returns the currently focused widget in Root, or nil.
func (n *SpatialNavigator) Focused() Widget {
	if n == nil || n.Root == nil {
		return nil
	}
	return n.Root.Focused()
}

// SetFocus focuses w if it is visible and focusable in Root.
func (n *SpatialNavigator) SetFocus(w Widget) bool {
	if n == nil || n.Root == nil {
		return false
	}
	prev := n.Root.Focused()
	if !n.Root.SetFocus(w) {
		return false
	}
	next := n.Root.Focused()
	if prev != next && n.OnFocusChange != nil {
		n.OnFocusChange(prev, next)
	}
	return true
}

// ClearFocus clears focus on Root.
func (n *SpatialNavigator) ClearFocus() {
	if n == nil || n.Root == nil {
		return
	}
	prev := n.Root.Focused()
	n.Root.ClearFocus()
	if prev != nil && n.OnFocusChange != nil {
		n.OnFocusChange(prev, nil)
	}
}

func (n *SpatialNavigator) resolveOverride(cur Widget, dir NavAction) Widget {
	if cur == nil {
		return nil
	}
	if n != nil && n.overrides != nil {
		if target := n.overrides[cur].For(dir); target != nil {
			return target
		}
	}
	return widgetNavOverrides(cur).For(dir)
}

// Move navigates focus in the directional NavAction (NavUp, NavDown, NavLeft, NavRight).
// It checks explicit per-widget overrides first, then falls back to 2D spatial navigation.
func (n *SpatialNavigator) Move(dir NavAction) bool {
	if n == nil || n.Root == nil {
		return false
	}
	dx, dy, ok := navActionVector(dir)
	if !ok {
		return false
	}

	r := n.Root
	r.syncFocusables()
	if len(r.focusables) == 0 {
		return false
	}
	prev := r.focused

	if !r.hasFocus || r.focusIdx < 0 {
		idx := 0
		if dy < 0 || (dy == 0 && dx < 0) {
			idx = len(r.focusables) - 1
		}
		if r.SetFocusIndex(idx) {
			if prev != r.focused && n.OnFocusChange != nil {
				n.OnFocusChange(prev, r.focused)
			}
			return true
		}
		return false
	}

	cur := r.focusables[r.focusIdx]

	// 1. Explicit per-widget overrides take priority (following chains if an intermediate target is disabled/hidden).
	if target := n.resolveOverride(cur, dir); target != nil {
		if resolved := n.findEnabledOverride(target, dir); resolved != nil && resolved != cur {
			if r.SetFocus(resolved) {
				if prev != r.focused && n.OnFocusChange != nil {
					n.OnFocusChange(prev, r.focused)
				}
				return true
			}
		}
	}

	// 2. True 2D spatial navigation over laid-out widget centers and rects.
	cb := cur.Bounds()
	if cb.Size.Width <= 0 && cb.Size.Height <= 0 {
		var moved bool
		if dy > 0 || (dy == 0 && dx > 0) {
			moved = r.FocusNext()
		} else {
			moved = r.FocusPrev()
		}
		if moved && prev != r.focused && n.OnFocusChange != nil {
			n.OnFocusChange(prev, r.focused)
		}
		return moved
	}

	nextIdx := spatialFindNext(r.focusables, r.focusIdx, dx, dy, n.WrapAround)
	if nextIdx < 0 {
		return false
	}
	if r.SetFocusIndex(nextIdx) {
		if prev != r.focused && n.OnFocusChange != nil {
			n.OnFocusChange(prev, r.focused)
		}
		return true
	}
	return false
}

func (n *SpatialNavigator) findEnabledOverride(target Widget, dir NavAction) Widget {
	visited := 0
	for target != nil && visited < 16 {
		if n.Root.isInFocusables(target) {
			return target
		}
		target = n.resolveOverride(target, dir)
		visited++
	}
	return nil
}

// Confirm activates the currently focused widget and calls OnConfirm if configured.
func (n *SpatialNavigator) Confirm() bool {
	if n == nil || n.Root == nil {
		return false
	}
	n.Root.syncFocusables()
	focused := n.Root.focused
	activated := n.Root.Activate()
	if n.OnConfirm != nil && (activated || focused != nil) {
		n.OnConfirm(focused)
		return true
	}
	return activated
}

// Back triggers the OnBack callback if configured.
func (n *SpatialNavigator) Back() bool {
	if n == nil || n.OnBack == nil {
		return false
	}
	n.OnBack()
	return true
}

// HandleAction executes a single NavAction (directional move, slider step, confirm, or back).
func (n *SpatialNavigator) HandleAction(action NavAction) bool {
	if n == nil || n.Root == nil {
		return false
	}
	switch action {
	case NavUp, NavDown:
		return n.Move(action)
	case NavLeft:
		n.Root.syncFocusables()
		if n.SliderSteps && n.Root.hasFocus {
			if _, ok := n.Root.focused.(*Slider); ok && n.resolveOverride(n.Root.focused, NavLeft) == nil {
				return n.Root.Adjust(-1)
			}
		}
		return n.Move(NavLeft)
	case NavRight:
		n.Root.syncFocusables()
		if n.SliderSteps && n.Root.hasFocus {
			if _, ok := n.Root.focused.(*Slider); ok && n.resolveOverride(n.Root.focused, NavRight) == nil {
				return n.Root.Adjust(1)
			}
		}
		return n.Move(NavRight)
	case NavConfirm:
		return n.Confirm()
	case NavBack:
		return n.Back()
	default:
		return false
	}
}

// StepActions polls logical UI navigation actions using pressed (for example, wrapping input.Actions.JustPressed)
// and dispatches any triggered actions in deterministic order. Returns true if any action was handled.
func (n *SpatialNavigator) StepActions(pressed func(NavAction) bool) bool {
	if n == nil || pressed == nil {
		return false
	}
	order := [6]NavAction{NavUp, NavDown, NavLeft, NavRight, NavConfirm, NavBack}
	handled := false
	for _, act := range order {
		if pressed(act) {
			if n.HandleAction(act) {
				handled = true
			}
		}
	}
	if n.Scan.Enabled && n.Scan.Select != NavNone && n.Scan.Select != NavConfirm && pressed(n.Scan.Select) {
		if n.ScanSelect() {
			handled = true
		}
	}
	return handled
}

// DrawFocusRing draws a customizable focus ring around the currently focused widget in Root.
func (n *SpatialNavigator) DrawFocusRing(d *core.Drawing) {
	if n == nil || n.Root == nil || d == nil {
		return
	}
	focused := n.Root.Focused()
	if focused == nil {
		return
	}
	th := n.Root.theme()
	style := n.FocusRing
	if style == nil {
		style = n.Root.FocusRing
	}
	if style == nil {
		style = th.FocusRing
	}
	radius := resolveRadius(0, th.CornerRadius)
	d.Save()
	DrawFocusRing(d, focused.Bounds(), radius, style, &th)
	d.Restore()
}

func navActionVector(dir NavAction) (dx, dy int, ok bool) {
	switch dir {
	case NavUp:
		return 0, -1, true
	case NavDown:
		return 0, 1, true
	case NavLeft:
		return -1, 0, true
	case NavRight:
		return 1, 0, true
	default:
		return 0, 0, false
	}
}

func dirToNavAction(dx, dy int) NavAction {
	switch {
	case dx == 0 && dy < 0:
		return NavUp
	case dx == 0 && dy > 0:
		return NavDown
	case dx < 0 && dy == 0:
		return NavLeft
	case dx > 0 && dy == 0:
		return NavRight
	default:
		return NavNone
	}
}

// spatialFindNext finds the best focusable widget index in direction (dx, dy) from focusables[curIdx].
// Returns -1 when no candidate exists (and wrapAround is false or no opposite-side wrap target exists).
func spatialFindNext(focusables []Widget, curIdx int, dx, dy int, wrapAround bool) int {
	if curIdx < 0 || curIdx >= len(focusables) || (dx == 0 && dy == 0) {
		return -1
	}
	cur := focusables[curIdx]
	cb := cur.Bounds()
	if cb.Size.Width <= 0 && cb.Size.Height <= 0 {
		return -1
	}

	dirX, dirY := float64(dx), float64(dy)
	norm := math.Hypot(dirX, dirY)
	dirX /= norm
	dirY /= norm

	cx0, cy0 := float64(cb.Origin.X), float64(cb.Origin.Y)
	cx1, cy1 := cx0+float64(cb.Size.Width), cy0+float64(cb.Size.Height)
	cx := (cx0 + cx1) / 2
	cy := (cy0 + cy1) / 2

	bandIdx := -1
	bandScore := math.Inf(1)
	coneIdx := -1
	coneScore := math.Inf(1)
	fallbackIdx := -1
	fallbackScore := math.Inf(1)

	wrapIdx := -1
	wrapScore := math.Inf(1)

	const eps = 0.5
	for i, cand := range focusables {
		if i == curIdx {
			continue
		}
		nb := cand.Bounds()
		if nb.Size.Width <= 0 && nb.Size.Height <= 0 {
			continue
		}
		nx0, ny0 := float64(nb.Origin.X), float64(nb.Origin.Y)
		nx1, ny1 := nx0+float64(nb.Size.Width), ny0+float64(nb.Size.Height)
		nx := (nx0 + nx1) / 2
		ny := (ny0 + ny1) / 2

		vx := nx - cx
		vy := ny - cy
		primary := vx*dirX + vy*dirY
		perp := math.Abs(vx*(-dirY) + vy*dirX)

		// Compute cross-axis interval gap (0 when intervals overlap, e.g. same row/column or wide slider above buttons).
		var perpGap float64
		if math.Abs(dirX) >= math.Abs(dirY) {
			// Horizontal move: cross axis is Y.
			if ny1 < cy0 {
				perpGap = cy0 - ny1
			} else if ny0 > cy1 {
				perpGap = ny0 - cy1
			}
		} else {
			// Vertical move: cross axis is X.
			if nx1 < cx0 {
				perpGap = cx0 - nx1
			} else if nx0 > cx1 {
				perpGap = nx0 - cx1
			}
		}

		if primary > eps {
			score := primary + perpGap*3.0 + perp*0.5
			switch {
			case perpGap <= eps:
				// Tier 1: Candidate overlaps the current widget's cross-axis band (same row or column).
				if score < bandScore-1e-6 {
					bandScore = score
					bandIdx = i
				}
			case perp <= primary*2.5:
				// Tier 2: Candidate lies inside the directional cone when no same-band neighbor exists.
				if score < coneScore-1e-6 {
					coneScore = score
					coneIdx = i
				}
			default:
				// Tier 3: Wider half-plane fallback for sparse grids.
				if score < fallbackScore-1e-6 {
					fallbackScore = score
					fallbackIdx = i
				}
			}
		} else if wrapAround && primary < -eps && (perpGap <= eps || math.Abs(primary) >= perp*0.5) {
			// Opposite-side wrap candidate: prefer minimal perpendicular offset and farthest back (most negative primary).
			ws := primary + perpGap*4.0 + perp*2.0
			if ws < wrapScore-1e-6 {
				wrapScore = ws
				wrapIdx = i
			}
		}
	}

	if bandIdx >= 0 {
		return bandIdx
	}
	if coneIdx >= 0 {
		return coneIdx
	}
	if fallbackIdx >= 0 {
		return fallbackIdx
	}
	if wrapAround && wrapIdx >= 0 {
		return wrapIdx
	}
	return -1
}

func widgetNavOverrides(w Widget) NavOverrides {
	switch v := w.(type) {
	case *Button:
		return v.Nav
	case *TouchButton:
		return v.Nav
	case *Toggle:
		return v.Nav
	case *Slider:
		return v.Nav
	case *List:
		return v.Nav
	default:
		return NavOverrides{}
	}
}
