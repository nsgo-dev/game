package scene

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/gpu"
)

// HUDAnchor names a region inside a HUD3D safe-area frame where a HUDSlot is placed.
type HUDAnchor uint8

const (
	// HUDFull stretches across the full HUD area inside safe-area insets and
	// padding while passing open regions through to the 3D view underneath.
	HUDFull HUDAnchor = iota
	// HUDTopLeft anchors content to the top-left corner.
	HUDTopLeft
	// HUDTopCenter anchors content horizontally centered along the top edge.
	HUDTopCenter
	// HUDTopRight anchors content to the top-right corner.
	HUDTopRight
	// HUDCenterLeft anchors content vertically centered along the left edge.
	HUDCenterLeft
	// HUDCenter anchors content centered both horizontally and vertically.
	HUDCenter
	// HUDCenterRight anchors content vertically centered along the right edge.
	HUDCenterRight
	// HUDBottomLeft anchors content to the bottom-left corner.
	HUDBottomLeft
	// HUDBottomCenter anchors content horizontally centered along the bottom edge.
	HUDBottomCenter
	// HUDBottomRight anchors content to the bottom-right corner.
	HUDBottomRight
)

// String returns the anchor's name.
func (a HUDAnchor) String() string {
	switch a {
	case HUDFull:
		return "full"
	case HUDTopLeft:
		return "top-left"
	case HUDTopCenter:
		return "top-center"
	case HUDTopRight:
		return "top-right"
	case HUDCenterLeft:
		return "center-left"
	case HUDCenter:
		return "center"
	case HUDCenterRight:
		return "center-right"
	case HUDBottomLeft:
		return "bottom-left"
	case HUDBottomCenter:
		return "bottom-center"
	case HUDBottomRight:
		return "bottom-right"
	default:
		return "unknown"
	}
}

// HUDSlot places one native NSGo view subtree (such as ui.Button, ui.Card,
// ui.Badge, ui.Progress, or core.View) inside a HUD3D overlay.
type HUDSlot struct {
	// Anchor chooses where the slot sits inside the safe-area HUD frame.
	// Its zero value is HUDFull.
	Anchor HUDAnchor
	// Node is the native NSGo view rendered in this slot.
	Node core.Node
	// PassThrough sets core.PointerEventsNone on the slot so mouse clicks,
	// drags, and touches pass straight through decorative HUD elements (such
	// as crosshairs or status readouts) to the 3D view underneath.
	PassThrough bool
	// Padding optionally insets the slot's content from its anchor edges.
	Padding core.Edges
	// TestID optionally sets core.View.TestID on the slot's anchor wrapper.
	TestID string
}

// HUD3D composes native NSGo views (such as ui.Button, ui.Card, ui.Badge,
// ui.Progress, ui.Switch, and core.Text) over a 3D gpu.View.
//
// The overlay layer uses core.PointerEventsBoxNone so interactive HUD controls
// receive mouse and touch input directly while open regions pass mouse and
// touch events straight through to the 3D view underneath. Safe-area insets
// (core.EnvSafeArea) are applied automatically by default so HUD controls stay
// clear of notches, status bars, and home indicators, and visibility can be
// toggled reactively without remounting HUD subtrees.
type HUD3D struct {
	// Children are native NSGo views mounted directly in the safe-area HUD
	// container in slice order.
	Children []core.Node
	// Slots are anchored native NSGo HUD elements positioned inside the
	// safe-area HUD container after Children.
	Slots []HUDSlot
	// Padding adds extra inner margins around the HUD content on top of any
	// safe-area insets.
	Padding core.Edges
	// IgnoreSafeArea disables automatic core.EnvSafeArea padding when true.
	// By default (false), HUD content automatically respects platform
	// safe-area insets.
	IgnoreSafeArea bool
	// Hidden hides the HUD layer (DisplayNone and PointerEventsNone) when true,
	// forwarding all mouse and touch input to the 3D view underneath.
	Hidden bool
	// Style optionally supplies additional layout styles for the inner HUD
	// container (for example Gap, Direction, AlignItems, JustifyContent, or
	// ZIndex on the outer overlay).
	Style core.Style
	// TestID optionally sets core.View.TestID on the outer HUD overlay view.
	TestID string
	// Label optionally sets core.View.Label on the outer HUD overlay view.
	Label string

	cx          *core.Scope
	hiddenSig   *core.Signal[bool]
	safeAreaSig *core.Signal[bool]
}

// NewHUD3D returns a visible HUD3D bound to cx with safe-area insets enabled.
// A nil cx defers reactive scope binding until Layer, View, or Wrap mounts the
// HUD in a scope.
func NewHUD3D(cx *core.Scope) *HUD3D {
	h := &HUD3D{}
	h.bindScope(cx)
	return h
}

func (h *HUD3D) bindScope(cx *core.Scope) {
	if h == nil || cx == nil || cx.Disposed() {
		return
	}
	if h.cx == cx && h.hiddenSig != nil && h.safeAreaSig != nil {
		// Sync any direct struct field edits made before mounting.
		if h.hiddenSig.GetUntracked() != h.Hidden {
			h.hiddenSig.Set(h.Hidden)
		}
		wantSafe := !h.IgnoreSafeArea
		if h.safeAreaSig.GetUntracked() != wantSafe {
			h.safeAreaSig.Set(wantSafe)
		}
		return
	}
	cx.Run(func() {
		h.cx = cx
		h.hiddenSig = core.NewSignal(cx, h.Hidden)
		h.safeAreaSig = core.NewSignal(cx, !h.IgnoreSafeArea)
	})
}

// Add appends one or more native NSGo view nodes to h.Children. Nil nodes are
// ignored.
func (h *HUD3D) Add(nodes ...core.Node) {
	if h == nil {
		return
	}
	for _, n := range nodes {
		if n != nil {
			h.Children = append(h.Children, n)
		}
	}
}

// AddAt appends node anchored at anchor inside the safe-area HUD frame.
func (h *HUD3D) AddAt(anchor HUDAnchor, node core.Node) {
	if h == nil || node == nil {
		return
	}
	h.Slots = append(h.Slots, HUDSlot{
		Anchor: anchor,
		Node:   node,
	})
}

// AddPassThrough appends a non-interactive decorative node anchored at anchor
// with core.PointerEventsNone so all mouse and touch input passes straight
// through it to the 3D view underneath.
func (h *HUD3D) AddPassThrough(anchor HUDAnchor, node core.Node) {
	if h == nil || node == nil {
		return
	}
	h.Slots = append(h.Slots, HUDSlot{
		Anchor:      anchor,
		Node:        node,
		PassThrough: true,
	})
}

// AddSlot appends slot to h.Slots when slot.Node is non-nil.
func (h *HUD3D) AddSlot(slot HUDSlot) {
	if h == nil || slot.Node == nil {
		return
	}
	h.Slots = append(h.Slots, slot)
}

// SetHidden hides or shows the HUD overlay reactively. While hidden, the HUD
// layer uses core.DisplayNone and core.PointerEventsNone so all mouse and
// touch events reach the 3D view underneath.
func (h *HUD3D) SetHidden(hidden bool) {
	if h == nil {
		return
	}
	h.Hidden = hidden
	if h.hiddenSig != nil && h.cx != nil && !h.cx.Disposed() {
		h.hiddenSig.Set(hidden)
	}
}

// SetVisible shows or hides the HUD overlay reactively. It is the inverse of
// SetHidden.
func (h *HUD3D) SetVisible(visible bool) {
	h.SetHidden(!visible)
}

// Visible reports whether the HUD overlay is currently visible. When called
// inside a reactive binding or StyleFunc, it subscribes to visibility changes.
func (h *HUD3D) Visible() bool {
	if h == nil {
		return false
	}
	if h.hiddenSig != nil && h.cx != nil && !h.cx.Disposed() {
		return !h.hiddenSig.Get()
	}
	return !h.Hidden
}

// Toggle flips HUD visibility and returns the new Visible state.
func (h *HUD3D) Toggle() bool {
	if h == nil {
		return false
	}
	next := h.Hidden
	h.SetHidden(!next)
	return next
}

// SetSafeArea enables or disables automatic core.EnvSafeArea inset padding on
// the HUD overlay reactively.
func (h *HUD3D) SetSafeArea(enabled bool) {
	if h == nil {
		return
	}
	h.IgnoreSafeArea = !enabled
	if h.safeAreaSig != nil && h.cx != nil && !h.cx.Disposed() {
		h.safeAreaSig.Set(enabled)
	}
}

// SafeAreaEnabled reports whether automatic safe-area inset padding is enabled.
// When called inside a reactive binding, it subscribes to safe-area toggle
// changes.
func (h *HUD3D) SafeAreaEnabled() bool {
	if h == nil {
		return false
	}
	if h.safeAreaSig != nil && h.cx != nil && !h.cx.Disposed() {
		return h.safeAreaSig.Get()
	}
	return !h.IgnoreSafeArea
}

// Layer returns the absolutely positioned, pass-through native HUD overlay node
// covering its parent bounds. Mount it directly in gpu.ViewProps.Children,
// above a 2D Overlay3D canvas, or compose it automatically via HUD3D.View or
// HUD3D.Wrap.
func (h *HUD3D) Layer(cx *core.Scope) core.Node {
	if h == nil {
		return core.View{
			PointerEvents: core.PointerEventsNone,
			Style: core.Style{
				Position:      core.Absolute,
				Top:           core.Pt(0),
				Left:          core.Pt(0),
				Right:         core.Pt(0),
				Bottom:        core.Pt(0),
				PointerEvents: core.PointerEventsNone,
			},
		}
	}
	h.bindScope(cx)

	padding := h.Padding
	if padding == (core.Edges{}) {
		padding = h.Style.Padding
	}
	zIndex := h.Style.ZIndex

	innerStyle := h.Style
	innerStyle.Position = core.Relative
	innerStyle.Top = core.Length{}
	innerStyle.Left = core.Length{}
	innerStyle.Right = core.Length{}
	innerStyle.Bottom = core.Length{}
	innerStyle.Padding = core.Edges{}
	innerStyle.PaddingEnv = core.EnvEdges{}
	innerStyle.ZIndex = 0
	innerStyle.FlexGrow = 1
	innerStyle.FlexShrink = 1
	innerStyle.PointerEvents = core.PointerEventsBoxNone
	if innerStyle.Direction == 0 {
		innerStyle.Direction = core.Column
	}

	stageChildren := make([]core.Node, 0, len(h.Children)+len(h.Slots))
	for _, c := range h.Children {
		if c != nil {
			stageChildren = append(stageChildren, c)
		}
	}
	for _, slot := range h.Slots {
		if slot.Node == nil {
			continue
		}
		stageChildren = append(stageChildren, buildHUDSlot(slot))
	}

	return core.View{
		TestID:        h.TestID,
		Label:         h.Label,
		PointerEvents: core.PointerEventsBoxNone,
		StyleFunc: func() core.Style {
			hidden := h.Hidden
			if h.hiddenSig != nil {
				hidden = h.hiddenSig.Get()
			}
			safeArea := !h.IgnoreSafeArea
			if h.safeAreaSig != nil {
				safeArea = h.safeAreaSig.Get()
			}
			s := core.Style{
				Position:  core.Absolute,
				Top:       core.Pt(0),
				Left:      core.Pt(0),
				Right:     core.Pt(0),
				Bottom:    core.Pt(0),
				Direction: core.Column,
				ZIndex:    zIndex,
			}
			if hidden {
				s.Display = core.DisplayNone
				s.PointerEvents = core.PointerEventsNone
				return s
			}
			s.PointerEvents = core.PointerEventsBoxNone
			s.Padding = padding
			if safeArea {
				s.PaddingEnv = core.EnvAll(core.EnvSafeArea)
			}
			return s
		},
		Children: []core.Node{
			core.View{
				PointerEvents: core.PointerEventsBoxNone,
				Style:         innerStyle,
				Children:      stageChildren,
			},
		},
	}
}

// View returns a gpu.View configured by props with h's native HUD layer
// mounted on top of the 3D GPU surface (and above any existing props.Children
// such as a 2D overlay canvas).
func (h *HUD3D) View(cx *core.Scope, props gpu.ViewProps) core.Node {
	if h == nil {
		return gpu.View(cx, props)
	}
	merged := props
	children := make([]core.Node, 0, len(props.Children)+1)
	children = append(children, props.Children...)
	children = append(children, h.Layer(cx))
	merged.Children = children
	return gpu.View(cx, merged)
}

// Wrap composes view3D and h's native HUD layer as siblings in a Z-stack
// core.View, placing the HUD above view3D.
//
// Use Wrap when view3D is already constructed (for example a gpu.View wrapped
// in an interactive core.View that handles OnMouseDown, OnMouseMove, and
// OnTouch for 3D camera orbit or picking) so touches and hovers on HUD controls
// never bubble into view3D while open HUD regions pass all mouse and touch
// events through to view3D.
func (h *HUD3D) Wrap(cx *core.Scope, view3D core.Node) core.Node {
	if h == nil {
		return view3D
	}
	children := make([]core.Node, 0, 2)
	if view3D != nil {
		children = append(children, view3D)
	}
	children = append(children, h.Layer(cx))
	return core.View{
		Style: core.Style{
			Position:   core.Relative,
			FlexGrow:   1,
			FlexShrink: 1,
		},
		Children: children,
	}
}

func absorbMouseButton(core.MouseButtonEvent) {}

func buildHUDSlot(slot HUDSlot) core.Node {
	pe := core.PointerEventsBoxNone
	if slot.PassThrough {
		pe = core.PointerEventsNone
	}
	style := hudAnchorStyle(slot.Anchor, slot.Padding)
	style.PointerEvents = pe

	content := slot.Node
	if !slot.PassThrough && slot.Anchor != HUDFull {
		// Wrap anchored interactive controls in a tight box that absorbs stray
		// mouse button presses (which ui.Button / base.Button do not register
		// separately from OnPress) so clicking a HUD button never starts a 3D
		// camera drag on an underlying viewport's OnMouseDown handler.
		// OnMouseDown does not add a keyboard Tab focus stop.
		content = core.View{
			OnMouseDown: absorbMouseButton,
			OnMouseUp:   absorbMouseButton,
			Children:    []core.Node{slot.Node},
		}
	}

	return core.View{
		TestID:        slot.TestID,
		PointerEvents: pe,
		Style:         style,
		Children:      []core.Node{content},
	}
}

func hudAnchorStyle(anchor HUDAnchor, padding core.Edges) core.Style {
	s := core.Style{
		Position:  core.Absolute,
		Direction: core.Column,
		Padding:   padding,
	}
	switch anchor {
	case HUDTopLeft:
		s.Top = core.Pt(0)
		s.Left = core.Pt(0)
	case HUDTopCenter:
		s.Top = core.Pt(0)
		s.Left = core.Pt(0)
		s.Right = core.Pt(0)
		s.AlignItems = core.AlignCenter
	case HUDTopRight:
		s.Top = core.Pt(0)
		s.Right = core.Pt(0)
	case HUDCenterLeft:
		s.Top = core.Pt(0)
		s.Bottom = core.Pt(0)
		s.Left = core.Pt(0)
		s.JustifyContent = core.JustifyCenter
		s.AlignItems = core.AlignStart
	case HUDCenter:
		s.Top = core.Pt(0)
		s.Bottom = core.Pt(0)
		s.Left = core.Pt(0)
		s.Right = core.Pt(0)
		s.JustifyContent = core.JustifyCenter
		s.AlignItems = core.AlignCenter
	case HUDCenterRight:
		s.Top = core.Pt(0)
		s.Bottom = core.Pt(0)
		s.Right = core.Pt(0)
		s.JustifyContent = core.JustifyCenter
		s.AlignItems = core.AlignEnd
	case HUDBottomLeft:
		s.Bottom = core.Pt(0)
		s.Left = core.Pt(0)
	case HUDBottomCenter:
		s.Bottom = core.Pt(0)
		s.Left = core.Pt(0)
		s.Right = core.Pt(0)
		s.AlignItems = core.AlignCenter
	case HUDBottomRight:
		s.Bottom = core.Pt(0)
		s.Right = core.Pt(0)
	default: // HUDFull
		s.Top = core.Pt(0)
		s.Left = core.Pt(0)
		s.Right = core.Pt(0)
		s.Bottom = core.Pt(0)
	}
	return s
}
