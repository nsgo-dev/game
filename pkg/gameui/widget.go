package gameui

import "github.com/nsgo-dev/nsgo/core"

// Align controls cross-axis placement in VBox and HBox, and child placement inside Panel.
type Align uint8

const (
	// AlignDefault inherits the container's Align, or stretches when unset on a container.
	AlignDefault Align = iota
	// AlignStretch expands the child to fill the available cross-axis size unless it sets a fixed size.
	AlignStretch
	// AlignStart aligns the child to the start (left in VBox, top in HBox).
	AlignStart
	// AlignCenter centers the child along the cross axis.
	AlignCenter
	// AlignEnd aligns the child to the end (right in VBox, bottom in HBox).
	AlignEnd
)

// Justify controls main-axis distribution in VBox and HBox when extra space remains and no child has Flex > 0.
type Justify uint8

const (
	// JustifyStart packs children at the start of the main axis.
	JustifyStart Justify = iota
	// JustifyCenter centers children as a group along the main axis.
	JustifyCenter
	// JustifyEnd packs children at the end of the main axis.
	JustifyEnd
	// JustifySpaceBetween spreads remaining space evenly between adjacent children.
	JustifySpaceBetween
)

// WidgetState describes the current interaction state of a widget during Draw or StateOf queries.
type WidgetState struct {
	Hovered  bool
	Pressed  bool
	Focused  bool
	Disabled bool
}

// layoutSpec carries sizing and visibility hints read by the box layouter.
type layoutSpec struct {
	Width     float32
	Height    float32
	MinWidth  float32
	MinHeight float32
	Flex      float32
	Align     Align
	Hidden    bool
}

// Widget is a node in a game UI layout and focus tree.
type Widget interface {
	// Bounds returns the widget's rectangle from the last Layout pass.
	Bounds() core.Bounds

	spec() layoutSpec
	measure(theme *Theme) core.Size
	layout(bounds core.Bounds, theme *Theme)
	draw(d *core.Drawing, theme *Theme, state WidgetState)
	focusable() bool
	childWidgets() []Widget
	setBounds(b core.Bounds)
}

// Container is a Widget that lays out child widgets.
// Panel, VBox, and HBox implement Container.
type Container interface {
	Widget
	// ChildWidgets returns the container's direct children in layout order.
	ChildWidgets() []Widget
}
